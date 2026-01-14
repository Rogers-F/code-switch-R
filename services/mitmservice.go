package services

import (
	"container/list"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const defaultMITMCertCacheCapacity = 200

type mitmCertCacheEntry struct {
	domain string
	cert   *tls.Certificate
}

// mitmCertLRUCache is a small bounded LRU cache for generated server certificates.
// It is NOT concurrency-safe; callers must synchronize access.
type mitmCertLRUCache struct {
	capacity int
	ll       *list.List
	byDomain map[string]*list.Element
}

func newMITMCertLRUCache(capacity int) *mitmCertLRUCache {
	if capacity <= 0 {
		capacity = defaultMITMCertCacheCapacity
	}
	return &mitmCertLRUCache{
		capacity: capacity,
		ll:       list.New(),
		byDomain: make(map[string]*list.Element, capacity),
	}
}

func (c *mitmCertLRUCache) Get(domain string) (*tls.Certificate, bool) {
	el, ok := c.byDomain[domain]
	if !ok {
		return nil, false
	}
	c.ll.MoveToFront(el)
	entry, ok := el.Value.(mitmCertCacheEntry)
	if !ok {
		return nil, false
	}
	return entry.cert, entry.cert != nil
}

func (c *mitmCertLRUCache) Put(domain string, cert *tls.Certificate) {
	if domain == "" || cert == nil {
		return
	}
	if el, ok := c.byDomain[domain]; ok {
		el.Value = mitmCertCacheEntry{domain: domain, cert: cert}
		c.ll.MoveToFront(el)
		return
	}

	el := c.ll.PushFront(mitmCertCacheEntry{domain: domain, cert: cert})
	c.byDomain[domain] = el

	for c.ll.Len() > c.capacity {
		back := c.ll.Back()
		if back == nil {
			break
		}
		entry, ok := back.Value.(mitmCertCacheEntry)
		if ok {
			delete(c.byDomain, entry.domain)
		}
		c.ll.Remove(back)
	}
}

func (c *mitmCertLRUCache) Len() int {
	if c == nil || c.ll == nil {
		return 0
	}
	return c.ll.Len()
}

// MITMService handles HTTPS interception and forwarding
type MITMService struct {
	// Certificate management
	certDir   string
	caCert    *x509.Certificate
	caKey     *rsa.PrivateKey
	certCache *mitmCertLRUCache // domain -> cert cache (bounded)
	certMu    sync.RWMutex

	// Server
	server   *http.Server
	listener net.Listener
	port     int
	running  bool
	runMu    sync.RWMutex

	// Configuration
	targetHost string // PoC: fixed target (e.g., api.anthropic.com)
	ruleEngine *MITMRuleEngine

	// Logging
	logChan chan MITMLogEntry
}

// MITMLogEntry represents a single MITM request log
type MITMLogEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	Domain     string    `json:"domain"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	Target     string    `json:"target"`
	StatusCode int       `json:"statusCode"`
	Latency    int64     `json:"latency"` // milliseconds
	Error      string    `json:"error,omitempty"`
}

// NewMITMService creates a new MITM service instance
func NewMITMService(ruleService *RuleService, providerService *ProviderService) (*MITMService, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home dir: %w", err)
	}

	certDir := filepath.Join(homeDir, ".code-switch", "certs")
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create cert directory: %w", err)
	}

	svc := &MITMService{
		certDir:    certDir,
		certCache:  newMITMCertLRUCache(defaultMITMCertCacheCapacity),
		port:       443, // Default HTTPS port for MITM interception
		targetHost: "rule-based",
		logChan:    make(chan MITMLogEntry, 100),
	}

	if ruleService != nil && providerService != nil {
		svc.ruleEngine = NewMITMRuleEngine(ruleService, providerService)
	}

	// Ensure CA exists
	if err := svc.ensureCA(); err != nil {
		return nil, fmt.Errorf("failed to ensure CA: %w", err)
	}

	return svc, nil
}

// ensureCA loads or generates Root CA
func (m *MITMService) ensureCA() error {
	caCertPath := filepath.Join(m.certDir, "ca.crt")
	caKeyPath := filepath.Join(m.certDir, "ca.key")

	// Try to load existing CA
	if _, err := os.Stat(caCertPath); err == nil {
		if _, err := os.Stat(caKeyPath); err == nil {
			return m.loadCA(caCertPath, caKeyPath)
		}
	}

	// Generate new CA
	log.Println("[MITM] Generating new Root CA...")
	return m.generateCA(caCertPath, caKeyPath)
}

// loadCA loads CA from disk
func (m *MITMService) loadCA(certPath, keyPath string) error {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("failed to read CA cert: %w", err)
	}

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("failed to read CA key: %w", err)
	}

	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return fmt.Errorf("failed to decode CA cert PEM")
	}

	m.caCert, err = x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse CA cert: %w", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return fmt.Errorf("failed to decode CA key PEM")
	}

	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse CA key: %w", err)
	}

	m.caKey = key
	log.Println("[MITM] Loaded existing Root CA from disk")
	return nil
}

// generateCA generates a new Root CA
func (m *MITMService) generateCA(certPath, keyPath string) error {
	// Generate private key
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate CA key: %w", err)
	}

	// Create certificate template
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "Code-Switch MITM CA",
			Organization: []string{"Code-Switch"},
		},
		NotBefore:             time.Now().Add(-24 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// Self-sign the certificate
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("failed to create CA cert: %w", err)
	}

	// Parse the certificate
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return fmt.Errorf("failed to parse CA cert: %w", err)
	}

	// Save to disk
	certOut, err := os.Create(certPath)
	if err != nil {
		return fmt.Errorf("failed to create CA cert file: %w", err)
	}
	defer certOut.Close()

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return fmt.Errorf("failed to encode CA cert: %w", err)
	}

	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create CA key file: %w", err)
	}
	defer keyOut.Close()

	keyBytes := x509.MarshalPKCS1PrivateKey(key)
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyBytes}); err != nil {
		return fmt.Errorf("failed to encode CA key: %w", err)
	}

	m.caCert = cert
	m.caKey = key

	log.Println("[MITM] Generated and saved new Root CA")
	return nil
}

// generateServerCert generates a certificate for a specific domain
func (m *MITMService) generateServerCert(domain string) (*tls.Certificate, error) {
	m.certMu.Lock()
	defer m.certMu.Unlock()

	// Check cache first (Get will refresh LRU position)
	if m.certCache != nil {
		if cert, ok := m.certCache.Get(domain); ok {
			return cert, nil
		}
	}

	log.Printf("[MITM] Generating server certificate for %s...\n", domain)

	// Generate key pair
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}

	// Create certificate template
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   domain,
			Organization: []string{"Code-Switch MITM"},
		},
		NotBefore:   time.Now().Add(-24 * time.Hour),
		NotAfter:    time.Now().Add(365 * 24 * time.Hour), // 1 year
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    []string{domain},
	}

	// Sign with CA
	certDER, err := x509.CreateCertificate(rand.Reader, template, m.caCert, &key.PublicKey, m.caKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create certificate: %w", err)
	}

	// Create tls.Certificate
	cert := &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}

	// Cache it
	if m.certCache != nil {
		m.certCache.Put(domain, cert)
	}

	return cert, nil
}

// getCertificate is the callback for tls.Config.GetCertificate
func (m *MITMService) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	domain := hello.ServerName
	if domain == "" {
		// 兜底：无 SNI 的客户端也允许完成握手（证书名可能不匹配，但不直接失败）
		domain = "localhost"
	}

	return m.generateServerCert(domain)
}

// Start starts the MITM server
func (m *MITMService) Start() error {
	m.runMu.Lock()
	defer m.runMu.Unlock()

	if m.running {
		return fmt.Errorf("MITM server already running")
	}

	// Create TLS config with dynamic certificate
	tlsConfig := &tls.Config{
		GetCertificate: m.getCertificate,
		MinVersion:     tls.VersionTLS12,
	}

	// Create listener
	addr := fmt.Sprintf(":%d", m.port)
	listener, err := tls.Listen("tcp", addr, tlsConfig)
	if err != nil {
		// 低端口（<1024）在多数系统上需要管理员权限
		if m.port < 1024 {
			return fmt.Errorf("failed to start listener on port %d (administrator privileges required): %w", m.port, err)
		}
		return fmt.Errorf("failed to start listener: %w", err)
	}

	m.listener = listener

	// Create HTTP server
	m.server = &http.Server{
		Handler:      m.createHandler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start server in goroutine
	go func() {
		log.Printf("[MITM] Server started on port %d\n", m.port)
		if err := m.server.Serve(m.listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[MITM] Server error: %v\n", err)
		}
	}()

	m.running = true
	return nil
}

// Stop stops the MITM server
func (m *MITMService) Stop() error {
	m.runMu.Lock()
	defer m.runMu.Unlock()

	if !m.running {
		return fmt.Errorf("MITM server not running")
	}

	if m.server != nil {
		if err := m.server.Close(); err != nil {
			return fmt.Errorf("failed to stop server: %w", err)
		}
	}

	m.running = false
	log.Println("[MITM] Server stopped")
	return nil
}

// GetStatus returns the current status
func (m *MITMService) GetStatus() map[string]interface{} {
	m.runMu.RLock()
	defer m.runMu.RUnlock()

	return map[string]interface{}{
		"running": m.running,
		"port":    m.port,
		"target":  m.targetHost,
	}
}

// GetCACertPath returns the path to the CA certificate for installation
func (m *MITMService) GetCACertPath() string {
	return filepath.Join(m.certDir, "ca.crt")
}

// createHandler creates the HTTP handler for proxying
func (m *MITMService) createHandler() http.Handler {
	if m.ruleEngine != nil {
		return m.ruleEngine.CreateRuleBasedProxy(m.logChan)
	}

	director := func(req *http.Request) {
		// Set target
		req.URL.Scheme = "https"
		req.URL.Host = m.targetHost
		req.Host = m.targetHost

		// Log request start
		log.Printf("[MITM] %s %s -> %s\n", req.Method, req.URL.Path, m.targetHost)
	}

	// Create reverse proxy
	proxy := &httputil.ReverseProxy{
		Director: director,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // PoC: skip verification
			},
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			DisableCompression:    false,
			DisableKeepAlives:     false,
			ResponseHeaderTimeout: 30 * time.Second,
		},
		ModifyResponse: func(resp *http.Response) error {
			// Log response
			log.Printf("[MITM] <- %d %s\n", resp.StatusCode, resp.Request.URL.Path)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("[MITM] Proxy error: %v\n", err)
			w.WriteHeader(http.StatusBadGateway)
			w.Write([]byte(fmt.Sprintf(`{"error": "Proxy error", "details": "%s"}`, err.Error())))
		},
		FlushInterval: 100 * time.Millisecond, // Support streaming
	}

	// Wrap with logging
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		domain := r.Host

		// Create response wrapper to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Handle request
		proxy.ServeHTTP(rw, r)

		// Log entry
		entry := MITMLogEntry{
			Timestamp:  start,
			Domain:     domain,
			Method:     r.Method,
			Path:       r.URL.Path,
			Target:     m.targetHost,
			StatusCode: rw.statusCode,
			Latency:    time.Since(start).Milliseconds(),
		}

		// Send to log channel (non-blocking)
		select {
		case m.logChan <- entry:
		default:
			// Channel full, skip
		}
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	return rw.ResponseWriter.Write(b)
}

func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// GetLogs returns recent logs (for frontend)
func (m *MITMService) GetLogs() []MITMLogEntry {
	logs := []MITMLogEntry{}
	for {
		select {
		case entry := <-m.logChan:
			logs = append(logs, entry)
		default:
			return logs
		}
	}
}

// Wails bindings

// StartMITM starts the MITM server (exported for Wails)
func (m *MITMService) StartMITM() error {
	return m.Start()
}

// StopMITM stops the MITM server (exported for Wails)
func (m *MITMService) StopMITM() error {
	return m.Stop()
}

// GetMITMStatus returns the current status (exported for Wails)
func (m *MITMService) GetMITMStatus() map[string]interface{} {
	return m.GetStatus()
}

// GetMITMCACertPath returns the CA cert path (exported for Wails)
func (m *MITMService) GetMITMCACertPath() string {
	return m.GetCACertPath()
}

// GetMITMLogs returns recent logs (exported for Wails)
func (m *MITMService) GetMITMLogs() []MITMLogEntry {
	return m.GetLogs()
}

// SetMITMPort sets the listening port (exported for Wails)
func (m *MITMService) SetMITMPort(port int) error {
	m.runMu.Lock()
	defer m.runMu.Unlock()

	if m.running {
		return fmt.Errorf("cannot change port while server is running")
	}

	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port: %d", port)
	}

	m.port = port
	return nil
}

// SetMITMTarget sets the target host (exported for Wails)
func (m *MITMService) SetMITMTarget(target string) error {
	m.runMu.Lock()
	defer m.runMu.Unlock()

	if m.running {
		return fmt.Errorf("cannot change target while server is running")
	}

	m.targetHost = target
	return nil
}
