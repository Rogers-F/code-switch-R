package services

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// MITMRuleEngine handles rule-based routing for MITM proxy
type MITMRuleEngine struct {
	ruleService     *RuleService
	providerService *ProviderService
	transport       http.RoundTripper
}

func parseTargetProviderSpec(spec string) (platform string, providerKey string, hasProviderKey bool, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", false, fmt.Errorf("target provider is empty")
	}

	// 支持 custom:{toolId}（platform 本身含 ":"），以及 custom:{toolId}:{providerId}
	if strings.HasPrefix(spec, "custom:") {
		firstColon := strings.Index(spec, ":")
		lastColon := strings.LastIndex(spec, ":")
		if firstColon == lastColon {
			// 只有一个 ":"，表示只有 platform（custom:{toolId}），不指定具体 provider
			return spec, "", false, nil
		}
		platform = spec[:lastColon]
		providerKey = strings.TrimSpace(spec[lastColon+1:])
		if providerKey == "" {
			return "", "", false, fmt.Errorf("invalid target provider format: %s", spec)
		}
		return platform, providerKey, true, nil
	}

	parts := strings.SplitN(spec, ":", 2)
	platform = strings.TrimSpace(parts[0])
	if platform == "" {
		return "", "", false, fmt.Errorf("invalid target provider format: %s", spec)
	}

	if len(parts) == 2 {
		providerKey = strings.TrimSpace(parts[1])
		if providerKey != "" {
			return platform, providerKey, true, nil
		}
	}

	return platform, "", false, nil
}

// NewMITMRuleEngine creates a new rule engine
func NewMITMRuleEngine(ruleService *RuleService, providerService *ProviderService) *MITMRuleEngine {
	return &MITMRuleEngine{
		ruleService:     ruleService,
		providerService: providerService,
	}
}

// SetTransport allows overriding the default HTTP transport (useful for smoke tests).
func (e *MITMRuleEngine) SetTransport(transport http.RoundTripper) {
	e.transport = transport
}

// MatchRule finds a matching rule for the given host
func (e *MITMRuleEngine) MatchRule(host string) (*MITMRule, error) {
	// Remove port from host if present
	if colonIdx := strings.LastIndex(host, ":"); colonIdx > 0 {
		// Check if it's not an IPv6 address
		if !strings.HasPrefix(host, "[") {
			host = host[:colonIdx]
		}
	}

	// Find rule by exact host match
	rule, err := e.ruleService.FindByHost(host)
	if err != nil {
		return nil, fmt.Errorf("failed to find rule: %w", err)
	}

	return rule, nil
}

// GetTargetProvider retrieves the provider configuration for a rule
func (e *MITMRuleEngine) GetTargetProvider(rule *MITMRule) (*Provider, error) {
	if rule == nil {
		return nil, fmt.Errorf("rule is nil")
	}

	platform, providerKey, hasProviderKey, err := parseTargetProviderSpec(rule.TargetProvider)
	if err != nil {
		return nil, err
	}

	// Load providers for the platform
	providers, err := e.providerService.LoadProviders(platform)
	if err != nil {
		return nil, fmt.Errorf("failed to load providers for %s: %w", platform, err)
	}

	// If specific ID provided, find by ID
	if hasProviderKey {
		for i := range providers {
			if providers[i].Name == providerKey || fmt.Sprintf("%d", providers[i].ID) == providerKey {
				return &providers[i], nil
			}
		}
		return nil, fmt.Errorf("provider not found: %s", rule.TargetProvider)
	}

	// Otherwise, return first enabled provider
	for i := range providers {
		if providers[i].Enabled {
			return &providers[i], nil
		}
	}

	return nil, fmt.Errorf("no enabled providers found for %s", platform)
}

// ApplyModelMapping applies model transformations based on rule
func (e *MITMRuleEngine) ApplyModelMapping(body []byte, rule *MITMRule) ([]byte, error) {
	if len(rule.ModelMappings) == 0 {
		return body, nil
	}

	// Parse JSON body
	bodyStr := string(body)
	model := gjson.Get(bodyStr, "model").String()
	if model == "" {
		return body, nil // No model field to transform
	}

	// Try to match model against mappings
	for _, mapping := range rule.ModelMappings {
		if e.matchModelPattern(model, mapping.SourceModel) {
			// Apply transformation
			newBodyStr, err := sjson.Set(bodyStr, "model", mapping.TargetModel)
			if err != nil {
				return nil, fmt.Errorf("failed to set model: %w", err)
			}
			log.Printf("[MITM] Model mapping: %s -> %s\n", model, mapping.TargetModel)
			return []byte(newBodyStr), nil
		}
	}

	return body, nil
}

// matchModelPattern checks if model matches pattern (supports * wildcard)
func (e *MITMRuleEngine) matchModelPattern(model, pattern string) bool {
	// Exact match
	if model == pattern {
		return true
	}

	// Wildcard match
	if strings.Contains(pattern, "*") {
		prefix := strings.Split(pattern, "*")[0]
		return strings.HasPrefix(model, prefix)
	}

	return false
}

// ApplyPathRewrite applies path rewriting based on rule
func (e *MITMRuleEngine) ApplyPathRewrite(path string, rule *MITMRule) string {
	if rule.PathRewrite == "" {
		return path
	}

	// Simple replacement format: "source->target"
	parts := strings.SplitN(rule.PathRewrite, "->", 2)
	if len(parts) != 2 {
		return path
	}

	source := strings.TrimSpace(parts[0])
	target := strings.TrimSpace(parts[1])

	if strings.Contains(path, source) {
		newPath := strings.Replace(path, source, target, 1)
		log.Printf("[MITM] Path rewrite: %s -> %s\n", path, newPath)
		return newPath
	}

	return path
}

func (e *MITMRuleEngine) defaultAuthTypeForSource(sourceHost string) string {
	host := strings.ToLower(strings.TrimSpace(sourceHost))
	switch {
	case strings.Contains(host, "anthropic.com"):
		return "x-api-key"
	case strings.Contains(host, "openai.com"):
		return "bearer"
	default:
		return "bearer"
	}
}

// BuildAuthHeaders builds authentication headers for the target provider
func (e *MITMRuleEngine) BuildAuthHeaders(sourceHost string, provider *Provider) map[string]string {
	headers := make(map[string]string)

	// Determine auth type
	authType := strings.ToLower(strings.TrimSpace(provider.ConnectivityAuthType))
	if authType == "" {
		authType = e.defaultAuthTypeForSource(sourceHost)
	}

	switch authType {
	case "bearer":
		headers["Authorization"] = fmt.Sprintf("Bearer %s", provider.APIKey)
	case "x-api-key":
		headers["x-api-key"] = provider.APIKey
	default:
		// Custom header name
		headers[provider.ConnectivityAuthType] = provider.APIKey
	}

	// Add extra headers
	for k, v := range provider.ExtraHeaders {
		if _, exists := headers[k]; !exists {
			headers[k] = v
		}
	}

	// Apply override headers
	for k, v := range provider.OverrideHeaders {
		headers[k] = v
	}

	return headers
}

// CreateRuleBasedProxy creates a reverse proxy with rule-based routing
func (e *MITMRuleEngine) CreateRuleBasedProxy(logChan chan MITMLogEntry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// 匹配优先级：SNI > Host（去端口）
		domain := strings.TrimSpace(r.Host)
		if r.TLS != nil && strings.TrimSpace(r.TLS.ServerName) != "" {
			domain = strings.TrimSpace(r.TLS.ServerName)
		}

		// Remove port from domain
		if colonIdx := strings.LastIndex(domain, ":"); colonIdx > 0 {
			if !strings.HasPrefix(domain, "[") {
				domain = domain[:colonIdx]
			}
		}

		// Match rule
		rule, err := e.MatchRule(domain)
		if err != nil {
			log.Printf("[MITM] Error matching rule for %s: %v\n", domain, err)
			http.Error(w, fmt.Sprintf(`{"error": "Rule matching error", "details": "%s"}`, err.Error()), http.StatusInternalServerError)
			return
		}

		if rule == nil {
			log.Printf("[MITM] No rule found for %s\n", domain)
			http.Error(w, fmt.Sprintf(`{"error": "No rule configured", "domain": "%s"}`, domain), http.StatusBadGateway)
			return
		}

		// Get target provider
		provider, err := e.GetTargetProvider(rule)
		if err != nil {
			log.Printf("[MITM] Error getting provider for rule %s: %v\n", rule.Name, err)
			http.Error(w, fmt.Sprintf(`{"error": "Provider error", "details": "%s"}`, err.Error()), http.StatusBadGateway)
			return
		}

		// Read and process body
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("[MITM] Error reading body: %v\n", err)
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}
		r.Body.Close()

		// Apply model mapping
		if len(bodyBytes) > 0 && strings.Contains(r.Header.Get("Content-Type"), "application/json") {
			bodyBytes, err = e.ApplyModelMapping(bodyBytes, rule)
			if err != nil {
				log.Printf("[MITM] Error applying model mapping: %v\n", err)
				http.Error(w, "Failed to apply model mapping", http.StatusInternalServerError)
				return
			}
		}

		// Apply path rewrite
		originalPath := r.URL.Path
		rewrittenPath := e.ApplyPathRewrite(r.URL.Path, rule)

		// Parse target URL
		targetURL, err := url.Parse(strings.TrimSpace(provider.APIURL))
		if err != nil {
			log.Printf("[MITM] Invalid provider URL %s: %v\n", provider.APIURL, err)
			http.Error(w, "Invalid provider URL", http.StatusBadGateway)
			return
		}
		if targetURL.Scheme == "" || targetURL.Host == "" {
			log.Printf("[MITM] Invalid provider URL (missing scheme/host) %s\n", provider.APIURL)
			http.Error(w, "Invalid provider URL", http.StatusBadGateway)
			return
		}

		// Override endpoint if specified
		effectivePath := rewrittenPath
		if strings.TrimSpace(provider.APIEndpoint) != "" {
			effectivePath = strings.TrimSpace(provider.APIEndpoint)
			if !strings.HasPrefix(effectivePath, "/") {
				effectivePath = "/" + effectivePath
			}
		} else if effectivePath == "" {
			effectivePath = "/"
		}

		// Support provider.APIURL with a path prefix (e.g. https://example.com/openai)
		upstreamPath := effectivePath
		basePath := strings.TrimSuffix(targetURL.Path, "/")
		if basePath != "" && basePath != "/" {
			// Avoid duplicating /v1/... when basePath already included
			if upstreamPath != basePath && !strings.HasPrefix(upstreamPath, basePath+"/") {
				upstreamPath = basePath + upstreamPath
			}
		}

		// Create reverse proxy director
		director := func(req *http.Request) {
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
			req.Host = targetURL.Host
			req.URL.Path = upstreamPath

			// Set auth headers
			authHeaders := e.BuildAuthHeaders(domain, provider)
			for k, v := range authHeaders {
				req.Header.Set(k, v)
			}

			// Strip headers
			for _, h := range provider.StripHeaders {
				req.Header.Del(h)
			}

			// Restore body
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			req.ContentLength = int64(len(bodyBytes))
			if len(bodyBytes) > 0 {
				req.Header.Set("Content-Length", fmt.Sprintf("%d", len(bodyBytes)))
			}

			log.Printf("[MITM] Routing %s %s -> %s%s (rule: %s)\n",
				req.Method, originalPath, targetURL.Host, upstreamPath, rule.Name)
		}

		// Create proxy
		transport := e.transport
		if transport == nil {
			transport = &http.Transport{
				TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, // TODO: Make configurable
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				DisableCompression:    false,
				DisableKeepAlives:     false,
				ResponseHeaderTimeout: 30 * time.Second,
			}
		}
		proxy := &httputil.ReverseProxy{
			Director: director,
			Transport: transport,
			ModifyResponse: func(resp *http.Response) error {
				log.Printf("[MITM] <- %d %s\n", resp.StatusCode, resp.Request.URL.Path)
				return nil
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				log.Printf("[MITM] Proxy error: %v\n", err)

				// Log error
				entry := MITMLogEntry{
					Timestamp:  start,
					Domain:     domain,
					Method:     r.Method,
					Path:       originalPath,
					Target:     fmt.Sprintf("%s (rule: %s)", targetURL.Host, rule.Name),
					StatusCode: http.StatusBadGateway,
					Latency:    time.Since(start).Milliseconds(),
					Error:      err.Error(),
				}

				select {
				case logChan <- entry:
				default:
				}

				w.WriteHeader(http.StatusBadGateway)
				json.NewEncoder(w).Encode(map[string]interface{}{
					"error":   "Proxy error",
					"details": err.Error(),
					"rule":    rule.Name,
				})
			},
			FlushInterval: 100 * time.Millisecond,
		}

		// Wrap response writer to capture status
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Serve
		proxy.ServeHTTP(rw, r)

		// Log successful request
		entry := MITMLogEntry{
			Timestamp:  start,
			Domain:     domain,
			Method:     r.Method,
			Path:       originalPath,
			Target:     fmt.Sprintf("%s (rule: %s)", targetURL.Host, rule.Name),
			StatusCode: rw.statusCode,
			Latency:    time.Since(start).Milliseconds(),
		}

		select {
		case logChan <- entry:
		default:
		}
	})
}
