package services

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	modelpricing "codeswitch/resources/model-pricing"

	"github.com/daodao97/xgo/xdb"
	"github.com/daodao97/xgo/xrequest"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// warnedServiceTiers 去重容器:首次见到未知 service_tier 时告警,之后静默。
var warnedServiceTiers sync.Map

// warnUnknownTier 在首次遇到未知 service_tier 值时打印一次警告。
// 同值的后续请求静默,不同未知 tier 分别告警一次。
func warnUnknownTier(tier string) {
	if tier == "" {
		return
	}
	if _, loaded := warnedServiceTiers.LoadOrStore(tier, struct{}{}); loaded {
		return
	}
	fmt.Printf("⚠️  unknown service_tier=%q,保留原值入库,按 default 档计费\n", tier)
}

// LastUsedProvider 最后使用的供应商信息
// @author sm
type LastUsedProvider struct {
	Platform     string `json:"platform"`      // claude/codex/gemini
	ProviderName string `json:"provider_name"` // 供应商名称
	UpdatedAt    int64  `json:"updated_at"`    // 更新时间（毫秒）
}

type ProviderRelayService struct {
	providerService     *ProviderService
	geminiService       *GeminiService
	blacklistService    *BlacklistService
	notificationService *NotificationService
	appSettings         *AppSettingsService // 应用设置服务（用于获取轮询开关状态）
	server              *http.Server
	serverMu            sync.Mutex // 保护 server：Start/Stop 均可从前端 RPC 触发
	addr                string
	// extraAddrs 额外监听地址（wsl_auto 模式下的 WSL 宿主机网段）。
	// 同一个 http.Server 可以 Serve 多个 listener，Shutdown 会一并关闭。
	extraAddrs []string
	// boundAddrs 本次启动实际绑定成功的地址。监听地址在启动时就已冻结，
	// 之后改设置不会重绑，所以任何"这个地址能不能连"的判断都必须以它为准，
	// 不能拿磁盘上的设置去推断。
	boundAddrs  []string
	lastUsed    map[string]*LastUsedProvider // 各平台最后使用的供应商
	lastUsedMu  sync.RWMutex                 // 保护 lastUsed 的锁
	rrMu        sync.Mutex                   // 轮询状态锁
	rrLastStart map[string]string            // 轮询状态：key="platform:level" → value=上次起始 Provider Name
	// endpointCooldowns 多地址供应商的地址冷却状态（进程内，issue #27）
	endpointCooldowns *endpointCooldownStore
	// concurrency 按供应商并发配额（进程内，issue #21）
	concurrency *concurrencyLimiter
	// captureRequests 抓包模式开关（进程内状态，重启即关，issue #5）
	captureRequests atomic.Bool
	// captureClearGen "清除抓包数据"的代次。采集时记在 requestLog 上，落库前
	// 不一致即置空：清除动作之后才结束的在途长流请求，不得把已被用户删除的
	// 那批抓包内容重新写回
	captureClearGen atomic.Int64
	// captureWriteMu 让"清除/删除会话"与"落库提交"线性化：写侧以读锁包住
	// 代次校验 + INSERT 提交，清除以写锁包住代次推进 + UPDATE。
	// 消除"校验通过后、提交完成前恰好发生清除"的写回窗口
	captureWriteMu sync.RWMutex
	// captureSessionID 当前录制会话 id（0=无）。会话生命周期见 capturesession.go
	captureSessionID atomic.Int64
	// captureDeletedSessions 已删除会话的墓碑（captureWriteMu 保护）：
	// 在途长流请求落库时若其会话已被单独删除，捕获内容自我置空。
	// 会话只会由当前进程产生新行，进程生命期的墓碑即完备
	captureDeletedSessions map[int64]struct{}
	// captureRecoverOnce 每进程一次的遗留会话恢复（Start 可被前端重复触发）
	captureRecoverOnce sync.Once
}

// errClientAbort 表示客户端中断连接，不应计入 provider 失败次数
var errClientAbort = errors.New("client aborted, skip failure count")

// errUpstreamStreamAborted 表示上游返回 2xx 后中途断流。
// 此时响应头与部分内容已经写给客户端，不能再降级到其它供应商（会写出两段响应），
// 但必须计入供应商失败，否则坏供应商永远不会被拉黑。
var errUpstreamStreamAborted = errors.New("upstream stream aborted after response started")

// errUpstreamClientError 表示上游以"请求本身有问题"为由拒绝（400/413/422 等）。
// 换供应商同样会失败，因此不计入供应商失败次数，避免一个坏请求把所有供应商拉黑。
var errUpstreamClientError = errors.New("upstream rejected the request payload")

// relayHTTPClient 转发共用的 HTTP 客户端。
// xrequest 的默认路径每次调用都会新建 http.Client 与 http.Transport，
// 连接完全无法复用，用后的空闲连接与其读写协程会长期滞留；这里改为共享连接池。
// 超时设为与原实现一致的 32 小时（适配超大型项目分析），实际的提前中止依靠
// 请求 context —— 客户端断开时立刻释放上游连接。
var relayHTTPClient = &http.Client{
	Timeout: 32 * time.Hour,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	},
}

// relayHTTPClientInsecure 与 relayHTTPClient 参数完全一致，仅跳过上游 TLS 证书验证，
// 供显式开启 insecureSkipVerify 的供应商使用（自签名证书/企业代理场景）。
// 独立实例：两种验证策略不能共用同一个 Transport 的连接池。
var relayHTTPClientInsecure = &http.Client{
	Timeout: 32 * time.Hour,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
	},
}

// warnedInsecureProviders 去重容器：开启跳验的供应商进程内首次使用时告警一次。
var warnedInsecureProviders sync.Map

// warnInsecureProviderOnce 首次对该供应商使用不验证 TLS 的客户端时打印警告，
// 作为跳验生效的审计痕迹。
func warnInsecureProviderOnce(name string) {
	if _, loaded := warnedInsecureProviders.LoadOrStore(name, struct{}{}); loaded {
		return
	}
	fmt.Printf("⚠️  Provider %s 已开启跳过 TLS 证书验证（insecureSkipVerify），存在中间人风险\n", name)
}

// relayClientFor 按供应商的 insecureSkipVerify 选择共享转发客户端。
// 返回的是共享实例，严禁在其上调 xrequest 的 SetTimeout（会写回 client，产生数据竞争）。
func relayClientFor(insecure bool, providerName string) *http.Client {
	if !insecure {
		return relayHTTPClient
	}
	warnInsecureProviderOnce(providerName)
	return relayHTTPClientInsecure
}

// deleteHeaderFold 按 HTTP 头大小写不敏感的语义删除。
// cloneHeaders 拿到的是 Go 规范化后的键名（如 X-Api-Key），
// 用小写字面量 delete 删不掉，必须逐个比对。
func deleteHeaderFold(headers map[string]string, names ...string) {
	for _, name := range names {
		for key := range headers {
			if strings.EqualFold(key, name) {
				delete(headers, key)
			}
		}
	}
}

// getHeaderFold 大小写不敏感地取头部值。
func getHeaderFold(headers map[string]string, name string) string {
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return value
		}
	}
	return ""
}

// setHeaderCanonical 以规范化键名写入头部，并先清掉同名的其它大小写形式。
// xrequest 是 req.Header[k] = []string{v} 直接赋值不做规范化，
// 若不先清理，客户端的 X-Api-Key 与注入的 x-api-key 会作为两个条目同时发到上游。
func setHeaderCanonical(headers map[string]string, name string, value string) {
	deleteHeaderFold(headers, name)
	headers[http.CanonicalHeaderKey(name)] = value
}

// sanitizeUpstreamHeaders 清理透传给上游的客户端请求头。
//
// 必须在注入供应商凭据之前调用：它会按大小写不敏感的方式删掉所有认证类头，
// 放到注入之后调用会把刚写入的供应商凭据一并删除。
//
// 具体清理三类：
//  1. 认证类头必须清空，否则用户本机的真实 API Key 会随请求一起发给每个第三方供应商；
//  2. Accept-Encoding 必须交回 Go 协商，透传客户端的值会让 Go 不再自动解压，
//     SSE 解析与 usage 提取拿到的是压缩字节，计费恒为 0；
//  3. 逐跳头（Connection/TE 等）不应跨代理转发。
func sanitizeUpstreamHeaders(headers map[string]string) {
	deleteHeaderFold(headers,
		"authorization", "proxy-authorization", "x-api-key", "api-key", "x-goog-api-key",
		"accept-encoding", "connection", "keep-alive", "transfer-encoding", "te", "upgrade")
}

// credentialQueryParams 查询串中承载凭据的参数名（小写比较）。
var credentialQueryParams = map[string]bool{
	"key":          true,
	"api_key":      true,
	"apikey":       true,
	"access_token": true,
	"token":        true,
}

// stripCredentialQueryParams 从原始查询串里删掉凭据类参数，其余参数保持原样与原顺序
// （alt=sse 这类必须保留，且不能因重新编码改变值）。
func stripCredentialQueryParams(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	parts := strings.Split(rawQuery, "&")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		name := part
		if eq := strings.Index(part, "="); eq >= 0 {
			name = part[:eq]
		}
		if credentialQueryParams[strings.ToLower(name)] {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, "&")
}

// maskSensitiveQuery 把 URL 查询串里的凭据类参数替换掉再进日志。
// Gemini 端点常见 ?key=<API Key>，原样打印会把密钥写进控制台与日志文件。
func maskSensitiveQuery(rawURL string) string {
	qIdx := strings.Index(rawURL, "?")
	if qIdx < 0 {
		return rawURL
	}
	path, rawQuery := rawURL[:qIdx], rawURL[qIdx+1:]
	parts := strings.Split(rawQuery, "&")
	for i, part := range parts {
		eq := strings.Index(part, "=")
		if eq <= 0 {
			continue
		}
		if credentialQueryParams[strings.ToLower(part[:eq])] {
			parts[i] = part[:eq] + "=***"
		}
	}
	return path + "?" + strings.Join(parts, "&")
}

// checkNonStreamTruncated 校验非流式响应是否被上游截断。
//
// xrequest 读取非流式响应体时丢弃了 io.ReadAll 的错误（xrequest/response.go 的
// `body, _ := io.ReadAll(...)`），上游中途断连会被当作完整响应，
// 于是半死的供应商在非流式请求上永远被判成功、失败计数被清零、永远不会被拉黑。
// 上游声明了 Content-Length 时可以用它与实际写给客户端的字节数比对；
// 分块传输（无 Content-Length）或内容被压缩时无从校验，返回 nil 维持原行为。
func checkNonStreamTruncated(resp *xrequest.Response, written int64) error {
	if resp == nil || resp.RawResponse == nil {
		return nil
	}
	declared := resp.RawResponse.ContentLength
	if declared <= 0 {
		return nil // 分块传输、未声明长度或空响应体
	}
	// 上游若做了内容压缩，解压后长度与 Content-Length 不可比
	if resp.RawResponse.Header.Get("Content-Encoding") != "" {
		return nil
	}
	if written < declared {
		return fmt.Errorf("响应被截断: 实际 %d 字节，上游声明 %d 字节", written, declared)
	}
	return nil
}

// respondNoEligibleProviders 初筛后无可用供应商的 404 终态。
// 把"为什么被跳过"按原因拆开讲清并给排查指引：白名单不匹配、临时拉黑与
// 未启用是三种完全不同的处置方式，混在一个计数里用户无从下手（issue #29）。
// 多种原因并存时全部列出，不做"选一个当代表"的省略
func respondNoEligibleProviders(c *gin.Context, requestedModel string, skippedModel, skippedBlacklist, skippedInvalid int) {
	var reasons, hints []string
	if skippedModel > 0 {
		reasons = append(reasons, fmt.Sprintf("%d 个供应商的模型白名单/映射不包含该模型", skippedModel))
		hints = append(hints, "在主页打开对应供应商，确认\"支持的模型\"包含该模型或留空（留空=支持所有模型），\"模型映射\"的目标模型名必须在白名单内")
	}
	if skippedBlacklist > 0 {
		reasons = append(reasons, fmt.Sprintf("%d 个正被临时拉黑", skippedBlacklist))
		hints = append(hints, "被拉黑的供应商可等待自动恢复，或到黑名单页手动解除")
	}
	if skippedInvalid > 0 {
		reasons = append(reasons, fmt.Sprintf("%d 个配置校验失败（详见控制台日志）", skippedInvalid))
		hints = append(hints, "配置校验失败的常见原因是模型映射的目标不在白名单内")
	}

	var msg string
	if len(reasons) == 0 {
		msg = "当前平台没有已启用的供应商。请在主页添加供应商并确认其已启用、API 地址与密钥已填写"
	} else {
		head := "没有可用的供应商"
		if requestedModel != "" {
			head = fmt.Sprintf("没有可用的供应商支持模型 '%s'", requestedModel)
		}
		msg = fmt.Sprintf("%s：%s。排查：%s", head, strings.Join(reasons, "；"), strings.Join(hints, "；"))
	}
	c.JSON(http.StatusNotFound, gin.H{"error": msg})
}

// respondAllProvidersFailed 统一输出"所有供应商都失败"的终态响应。
//
// 只有当**每一次**失败都是上游判定请求内容有问题（400/413/422 等）时才回 4xx：
// 这类请求换供应商也不可能成功，回 502 会让 SDK 按服务端故障自动重试，
// 一个坏请求被反复重发，每次都完整扫一遍全部供应商、白耗上游配额。
//
// 反之只要有任何一次是真的供应商故障（超时、5xx、限流），就必须维持 502 让 SDK 退避重试——
// 只看最后一个错误会误判：降级链末尾往往是最挑剔的备用供应商，
// 它回的 400 会把前面那个"临时过载、稍后可用"的供应商掩盖掉。
func respondAllProvidersFailed(c *gin.Context, lastError error, allClientErrors bool, payload gin.H) {
	status := http.StatusBadGateway
	if allClientErrors && errors.Is(lastError, errUpstreamClientError) {
		status = http.StatusBadRequest
		payload["type"] = "error"
		payload["error"] = map[string]string{
			"type":    "invalid_request_error",
			"message": lastError.Error(),
		}
	}
	c.JSON(status, payload)
}

// respondAllBusy 纯并发满载终态：503 + Retry-After，带稳定机器码
// provider_concurrency_exhausted。按平台给各自协议兼容的错误结构；
// 不用 502（那表示已联系上游失败）也不用 504（不是上游超时）。
func respondAllBusy(c *gin.Context, kind string) {
	c.Header("Retry-After", "1")
	msg := "所有可用供应商均已达到并发上限，请稍后重试"
	switch {
	case kind == "codex":
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"type":    "server_error",
				"code":    "provider_concurrency_exhausted",
				"message": msg,
			},
		})
	case kind == "gemini":
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error": gin.H{
				"code":    http.StatusServiceUnavailable,
				"status":  "UNAVAILABLE",
				"message": msg,
				// Google 风格 ErrorInfo：机器码走结构化字段，客户端不必解析文案
				"details": []gin.H{{
					"@type":  "type.googleapis.com/google.rpc.ErrorInfo",
					"reason": "provider_concurrency_exhausted",
				}},
			},
		})
	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "overloaded_error",
				"code":    "provider_concurrency_exhausted",
				"message": msg,
			},
			"message": msg,
		})
	}
}

// isClientSideUpstreamStatus 判定上游 4xx 是否属于"请求内容本身有问题"。
// 这类失败换供应商也一样，不应计入供应商失败次数；
// 401/403/404/408/429 仍属供应商侧问题（密钥失效、路径配错、限流），保持计入。
func isClientSideUpstreamStatus(status int) bool {
	switch status {
	case http.StatusBadRequest,
		http.StatusRequestEntityTooLarge,
		http.StatusUnsupportedMediaType,
		http.StatusUnprocessableEntity:
		return true
	}
	return false
}

// NewProviderRelayService 构造代理服务。addrs 首个为主监听地址，
// 其余为额外监听地址（wsl_auto 模式下需要同时覆盖回环与 WSL 宿主机网段）。
func NewProviderRelayService(providerService *ProviderService, geminiService *GeminiService, blacklistService *BlacklistService, notificationService *NotificationService, appSettings *AppSettingsService, addrs ...string) *ProviderRelayService {
	addr := ""
	var extraAddrs []string
	if len(addrs) > 0 {
		addr = addrs[0]
		extraAddrs = append(extraAddrs, addrs[1:]...)
	}
	if addr == "" {
		addr = "127.0.0.1:18100" // 【安全修复】仅监听本地回环地址，防止 API Key 暴露到局域网
	}

	// 【修复】数据库初始化已移至 main.go 的 InitDatabase()
	// 此处不再调用 xdb.Inits()、ensureRequestLogTable()、ensureBlacklistTables()

	return &ProviderRelayService{
		providerService:     providerService,
		geminiService:       geminiService,
		blacklistService:    blacklistService,
		notificationService: notificationService,
		appSettings:         appSettings,
		addr:                addr,
		extraAddrs:          extraAddrs,
		lastUsed: map[string]*LastUsedProvider{
			"claude": nil,
			"codex":  nil,
			"gemini": nil,
		},
		rrLastStart:            make(map[string]string),
		endpointCooldowns:      newEndpointCooldownStore(),
		concurrency:            newConcurrencyLimiter(),
		captureDeletedSessions: make(map[int64]struct{}),
	}
}

// setLastUsedProvider 记录最后使用的供应商
// @author sm
func (prs *ProviderRelayService) setLastUsedProvider(platform, providerName string) {
	prs.lastUsedMu.Lock()
	defer prs.lastUsedMu.Unlock()
	prs.lastUsed[platform] = &LastUsedProvider{
		Platform:     platform,
		ProviderName: providerName,
		UpdatedAt:    time.Now().UnixMilli(),
	}
}

// GetLastUsedProvider 获取指定平台最后使用的供应商
// @author sm
func (prs *ProviderRelayService) GetLastUsedProvider(platform string) *LastUsedProvider {
	prs.lastUsedMu.RLock()
	defer prs.lastUsedMu.RUnlock()
	return prs.lastUsed[platform]
}

// GetAllLastUsedProviders 获取所有平台最后使用的供应商
// @author sm
func (prs *ProviderRelayService) GetAllLastUsedProviders() map[string]*LastUsedProvider {
	prs.lastUsedMu.RLock()
	defer prs.lastUsedMu.RUnlock()
	result := make(map[string]*LastUsedProvider)
	for k, v := range prs.lastUsed {
		result[k] = v
	}
	return result
}

// isRoundRobinSettingEnabled 检查轮询设置是否启用（纯读取 AppSettings，不受 Fixed Mode 影响）
// 用于在 Fixed Mode 分支内也支持轮询排序
func (prs *ProviderRelayService) isRoundRobinSettingEnabled() bool {
	if prs.appSettings == nil {
		return false
	}
	settings, err := prs.appSettings.GetAppSettings()
	if err != nil {
		return false
	}
	return settings.EnableRoundRobin
}

// isRoundRobinEnabled 检查轮询功能是否启用（仅在降级模式下使用）
// 条件：1. 应用设置开关启用 2. 拉黑模式关闭（Fixed Mode 走单独分支处理轮询）
func (prs *ProviderRelayService) isRoundRobinEnabled() bool {
	// Fixed Mode 分支内有独立的轮询处理逻辑，此处返回 false 走降级模式
	if prs.blacklistService.ShouldUseFixedMode() {
		return false
	}
	return prs.isRoundRobinSettingEnabled()
}

// roundRobinOrder 对同 Level 的 providers 进行轮询排序
// 算法：基于 name 追踪，将上次起始 provider 移到末尾，实现轮询效果
// 参数：
//   - platform: 平台标识（claude/codex/gemini/custom:xxx）
//   - level: 当前 Level
//   - providers: 同 Level 的 providers 列表（已过滤、按用户排序）
//
// 返回：轮询排序后的 providers 列表（新切片，不修改原切片）
func (prs *ProviderRelayService) roundRobinOrder(platform string, level int, providers []Provider) []Provider {
	if len(providers) <= 1 {
		return providers
	}

	// 构建 key: "platform:level"
	key := fmt.Sprintf("%s:%d", platform, level)

	prs.rrMu.Lock()
	defer prs.rrMu.Unlock()

	lastStart := prs.rrLastStart[key]

	// 记录本次起始 provider 名称（更新状态）
	prs.rrLastStart[key] = providers[0].Name

	// 如果没有历史记录，返回原顺序
	if lastStart == "" {
		return providers
	}

	// 查找上次起始 provider 在当前列表中的位置
	lastIdx := -1
	for i, p := range providers {
		if p.Name == lastStart {
			lastIdx = i
			break
		}
	}

	// 上次起始 provider 不在当前列表（可能被禁用/黑名单），返回原顺序
	if lastIdx == -1 {
		return providers
	}

	// 构建轮询顺序：从 lastIdx+1 开始，环形遍历
	result := make([]Provider, len(providers))
	for i := 0; i < len(providers); i++ {
		idx := (lastIdx + 1 + i) % len(providers)
		result[i] = providers[idx]
	}

	// 更新本次起始 provider 名称
	prs.rrLastStart[key] = result[0].Name

	return result
}

// roundRobinOrderGemini 对 Gemini providers 进行轮询排序（复用相同逻辑）
func (prs *ProviderRelayService) roundRobinOrderGemini(level int, providers []GeminiProvider) []GeminiProvider {
	if len(providers) <= 1 {
		return providers
	}

	// 构建 key: "gemini:level"
	key := fmt.Sprintf("gemini:%d", level)

	prs.rrMu.Lock()
	defer prs.rrMu.Unlock()

	lastStart := prs.rrLastStart[key]

	// 记录本次起始 provider 名称
	prs.rrLastStart[key] = providers[0].Name

	// 如果没有历史记录，返回原顺序
	if lastStart == "" {
		return providers
	}

	// 查找上次起始 provider 在当前列表中的位置
	lastIdx := -1
	for i, p := range providers {
		if p.Name == lastStart {
			lastIdx = i
			break
		}
	}

	// 上次起始 provider 不在当前列表，返回原顺序
	if lastIdx == -1 {
		return providers
	}

	// 构建轮询顺序
	result := make([]GeminiProvider, len(providers))
	for i := 0; i < len(providers); i++ {
		idx := (lastIdx + 1 + i) % len(providers)
		result[i] = providers[idx]
	}

	// 更新本次起始 provider 名称
	prs.rrLastStart[key] = result[0].Name

	return result
}

func (prs *ProviderRelayService) Start() error {
	// 本服务注册为 Wails 服务后 Start/Stop 也可被前端 RPC 调用，
	// 重复 Start 会覆盖 prs.server 引用，旧监听器与 Serve 协程再也关不掉
	prs.serverMu.Lock()
	alreadyRunning := prs.server != nil
	prs.serverMu.Unlock()
	if alreadyRunning {
		return fmt.Errorf("provider relay 已在 %s 上运行", prs.addr)
	}

	// 启动前验证配置
	if warnings := prs.validateConfig(); len(warnings) > 0 {
		fmt.Println("======== Provider 配置验证警告 ========")
		for _, warn := range warnings {
			fmt.Printf("⚠️  %s\n", warn)
		}
		fmt.Println("========================================")
	}

	router := gin.Default()
	prs.registerRoutes(router)

	listener, err := net.Listen("tcp", prs.addr)
	if err != nil {
		return fmt.Errorf("listen provider relay on %s: %w", prs.addr, err)
	}

	server := &http.Server{
		Addr:    prs.addr,
		Handler: router,
	}
	prs.serverMu.Lock()
	prs.server = server
	prs.serverMu.Unlock()

	listeners := []net.Listener{listener}
	// 额外地址失败不影响主地址：WSL 网卡可能尚未就绪或该 IP 已变化，
	// 此时主地址（回环）仍应正常服务，只告警
	for _, extra := range prs.extraAddrs {
		extraListener, extraErr := net.Listen("tcp", extra)
		if extraErr != nil {
			fmt.Printf("[WARN] 额外监听地址 %s 绑定失败（不影响主地址）: %v\n", extra, extraErr)
			continue
		}
		listeners = append(listeners, extraListener)
	}

	bound := make([]string, 0, len(listeners))
	for _, l := range listeners {
		fmt.Printf("provider relay server listening on %s\n", l.Addr().String())
		bound = append(bound, l.Addr().String())
		go func(ln net.Listener) {
			if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
				fmt.Printf("provider relay server error: %v\n", err)
			}
		}(l)
	}

	prs.serverMu.Lock()
	prs.boundAddrs = bound
	prs.serverMu.Unlock()
	return nil
}

// BoundAddresses 返回本次启动实际绑定成功的监听地址。
// 监听地址在启动时冻结，改了网络设置也要重启应用才生效，
// 所以 UI 展示与"WSL 能不能连到"的判断都应以此为准。
func (prs *ProviderRelayService) BoundAddresses() []string {
	prs.serverMu.Lock()
	defer prs.serverMu.Unlock()
	return append([]string(nil), prs.boundAddrs...)
}

// GetRequestCapture 读取抓包模式开关
func (prs *ProviderRelayService) GetRequestCapture() bool {
	return prs.captureRequests.Load()
}

// stripStaleCapture 落库前的校验：采集发生在请求开始，长流请求可能在
// "清空全部"（代次不一致）或"删除所属会话"（墓碑命中）之后才结束，
// 两种情况都说明这批内容已被用户要求删除，置空且摘除会话关联。
// 采集快照化后合法捕获行必有非零会话 id，携带内容却无会话的行只能是
// 竞态残迹，一并置空（否则会混进 0 号旧数据桶）。
// 调用方需持有 captureWriteMu 读锁（墓碑 map 由该锁保护）
func (prs *ProviderRelayService) stripStaleCapture(requestLog *ReqeustLog) {
	_, deleted := prs.captureDeletedSessions[requestLog.CaptureSessionID]
	orphan := requestLog.CaptureSessionID == 0 && requestLogHasCapture(requestLog)
	if requestLog.captureGen != prs.captureClearGen.Load() || deleted || orphan {
		requestLog.RequestHeaders = ""
		requestLog.RequestBody = ""
		requestLog.BodyTruncated = false
		requestLog.BodyBytes = 0
		requestLog.CaptureSessionID = 0
	}
}

// requestLogInsertSQL 两条写入路径共用的 19 列 INSERT，避免列清单分叉
const requestLogInsertSQL = `
	INSERT INTO request_log (
		platform, model, provider, http_code,
		input_tokens, output_tokens, cache_create_tokens, cache_read_tokens,
		reasoning_tokens, is_stream, duration_sec,
		ephemeral_5m_tokens, ephemeral_1h_tokens, service_tier,
		request_headers, request_body, body_truncated, body_bytes, capture_session_id
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`

func requestLogInsertArgs(requestLog *ReqeustLog) []interface{} {
	return []interface{}{
		requestLog.Platform, requestLog.Model, requestLog.Provider, requestLog.HttpCode,
		requestLog.InputTokens, requestLog.OutputTokens, requestLog.CacheCreateTokens,
		requestLog.CacheReadTokens, requestLog.ReasoningTokens,
		boolToInt(requestLog.IsStream), requestLog.DurationSec,
		requestLog.Ephemeral5mTokens, requestLog.Ephemeral1hTokens, requestLog.ServiceTier,
		requestLog.RequestHeaders, requestLog.RequestBody,
		boolToInt(requestLog.BodyTruncated), requestLog.BodyBytes, requestLog.CaptureSessionID,
	}
}

func requestLogHasCapture(requestLog *ReqeustLog) bool {
	return requestLog.RequestHeaders != "" || requestLog.RequestBody != "" ||
		requestLog.BodyTruncated || requestLog.BodyBytes != 0
}

// writeRequestLog 落库统一入口，调用方需已持有 captureWriteMu 读锁。
// 携带抓包内容的行同步直写——提交在读锁内完成，与清除的写锁真正线性化
// （批量队列的 ExecBatchCtx 超时后任务仍会执行，"返回"不等于"已提交"，
// 不能作为栅栏边界）；普通行保持批量队列路径，不受清除语义约束。
// 抓包是低频调试态，直写不构成写入热点
func (prs *ProviderRelayService) writeRequestLog(requestLog *ReqeustLog) error {
	if requestLogHasCapture(requestLog) {
		db, err := xdb.DB("default")
		if err != nil {
			return err
		}
		_, err = db.Exec(requestLogInsertSQL, requestLogInsertArgs(requestLog)...)
		return err
	}
	if GlobalDBQueueLogs == nil {
		return fmt.Errorf("队列未初始化")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return GlobalDBQueueLogs.ExecBatchCtx(ctx, requestLogInsertSQL, requestLogInsertArgs(requestLog)...)
}

// validateConfig 验证所有 provider 的配置
// 返回警告列表（非阻塞性错误）
func (prs *ProviderRelayService) validateConfig() []string {
	warnings := make([]string, 0)

	for _, kind := range []string{"claude", "codex"} {
		providers, err := prs.providerService.LoadProviders(kind)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("[%s] 加载配置失败: %v", kind, err))
			continue
		}

		enabledCount := 0
		for _, p := range providers {
			if !p.Enabled {
				continue
			}
			enabledCount++

			// 验证每个启用的 provider
			if errs := p.ValidateConfiguration(); len(errs) > 0 {
				for _, errMsg := range errs {
					warnings = append(warnings, fmt.Sprintf("[%s/%s] %s", kind, p.Name, errMsg))
				}
			}

			// 检查是否配置了模型白名单或映射
			if (p.SupportedModels == nil || len(p.SupportedModels) == 0) &&
				(p.ModelMapping == nil || len(p.ModelMapping) == 0) {
				warnings = append(warnings, fmt.Sprintf(
					"[%s/%s] 未配置 supportedModels 或 modelMapping，将假设支持所有模型（可能导致降级失败）",
					kind, p.Name))
			}

			// 检查是否只配置了映射但没有白名单
			if len(p.ModelMapping) > 0 && len(p.SupportedModels) == 0 {
				warnings = append(warnings, fmt.Sprintf(
					"[%s/%s] 配置了 modelMapping 但未配置 supportedModels，映射目标将不做校验，请确认目标模型在供应商处可用",
					kind, p.Name))
			}
		}

		if enabledCount == 0 {
			warnings = append(warnings, fmt.Sprintf("[%s] 没有启用的 provider", kind))
		}
	}

	// Gemini：同样在启动期暴露白名单/映射配置错误，
	// 否则要等真实流量打到该 provider 才会在日志里刷出来
	if prs.geminiService != nil {
		for _, p := range prs.geminiService.GetProviders() {
			if !p.Enabled {
				continue
			}
			if errs := p.ValidateConfiguration(); len(errs) > 0 {
				for _, errMsg := range errs {
					warnings = append(warnings, fmt.Sprintf("[gemini/%s] %s", p.Name, errMsg))
				}
			}
		}
	}

	return warnings
}

func (prs *ProviderRelayService) Stop() error {
	prs.serverMu.Lock()
	server := prs.server
	prs.server = nil
	// 清掉绑定地址：停掉之后再对外报告"正在监听 xxx"会误导 UI 与 WSL 可达性判断
	prs.boundAddrs = nil
	prs.serverMu.Unlock()

	if server == nil {
		// 代理本就未运行；若录制开关还开着（异常路径），同样封存会话
		prs.closeActiveCaptureSession()
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := server.Shutdown(ctx)
	if err != nil {
		// 优雅关闭超时（长流式请求会一直占着连接）：强制关闭监听与全部连接，
		// 否则 Serve 协程与在途连接会活过 OnShutdown
		fmt.Printf("[WARN] 代理优雅关闭超时，强制关闭: %v\n", err)
		if closeErr := server.Close(); closeErr != nil {
			fmt.Printf("[WARN] 强制关闭代理失败: %v\n", closeErr)
		}
	}
	// 代理停了就不再有新流量，正常封存录制中的会话（区别于崩溃后的"已中断"）
	prs.closeActiveCaptureSession()
	return err
}

func (prs *ProviderRelayService) Addr() string {
	return prs.addr
}

func (prs *ProviderRelayService) registerRoutes(router gin.IRouter) {
	router.POST("/v1/messages", prs.proxyHandler("claude", "/v1/messages"))
	router.POST("/responses", prs.proxyHandler("codex", "/responses"))

	// /v1/models 端点（OpenAI-compatible API）
	// 支持 Claude 和 Codex 平台
	router.GET("/v1/models", prs.modelsHandler("claude"))

	// Gemini API 端点（使用专门的路径前缀避免与 Claude 冲突）
	router.POST("/gemini/v1beta/*any", prs.geminiProxyHandler("/v1beta"))
	router.POST("/gemini/v1/*any", prs.geminiProxyHandler("/v1"))

	// 自定义 CLI 工具端点（路由格式: /custom/:toolId/v1/messages）
	// toolId 用于区分不同的 CLI 工具，对应 provider kind 为 "custom:{toolId}"
	router.POST("/custom/:toolId/v1/messages", prs.customCliProxyHandler())
	// 兼容别名：ai-sdk 系 Anthropic 客户端（如 opencode 的 @ai-sdk/anthropic）
	// 按 `${baseURL}/messages` 拼 URL，而注入的 baseURL 不带 /v1。
	// handler 不读实际请求路径（上游端点固定 /v1/messages），别名零逻辑分叉
	router.POST("/custom/:toolId/messages", prs.customCliProxyHandler())

	// 自定义 CLI 工具的 /v1/models 端点
	router.GET("/custom/:toolId/v1/models", prs.customModelsHandler())
}

func (prs *ProviderRelayService) proxyHandler(kind string, endpoint string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var bodyBytes []byte
		if c.Request.Body != nil {
			data, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
				return
			}
			bodyBytes = data
			c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		// 空 body 或非法 JSON 一定会被所有上游拒绝，提前挡掉：
		// 否则每个供应商都要挨一次 4xx，还会白耗一轮降级
		if !gjson.ValidBytes(bodyBytes) {
			c.JSON(http.StatusBadRequest, gin.H{
				"type":    "error",
				"error":   map[string]string{"type": "invalid_request_error", "message": "request body must be valid JSON"},
				"message": "request body must be valid JSON",
			})
			return
		}

		isStream := gjson.GetBytes(bodyBytes, "stream").Bool()
		requestedModel := gjson.GetBytes(bodyBytes, "model").String()

		// 如果未指定模型，记录警告但不拦截
		if requestedModel == "" {
			fmt.Printf("[WARN] 请求未指定模型名，无法执行模型智能降级\n")
		}

		// (providers, 配置代数) 配对装载：容量热更新以更高代数为准，
		// 分两步读取会让旧配置带上新代数、降容被来回覆盖
		providers, configGen, err := prs.providerService.LoadProvidersWithGen(kind)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load providers"})
			return
		}

		active := make([]Provider, 0, len(providers))
		skippedCount := 0
		skippedModel, skippedBlacklist, skippedInvalid := 0, 0, 0
		for _, provider := range providers {
			// 基础过滤：enabled、URL、APIKey
			if !provider.Enabled || provider.APIURL == "" || provider.APIKey == "" {
				continue
			}

			// 配置验证：失败则自动跳过
			if errs := provider.ValidateConfiguration(); len(errs) > 0 {
				fmt.Printf("[WARN] Provider %s 配置验证失败，已自动跳过: %v\n", provider.Name, errs)
				skippedCount++
				skippedInvalid++
				continue
			}

			// 核心过滤：只保留支持请求模型的 provider
			if requestedModel != "" && !provider.IsModelSupported(requestedModel) {
				fmt.Printf("[INFO] Provider %s 不支持模型 %s，已跳过\n", provider.Name, requestedModel)
				skippedCount++
				skippedModel++
				continue
			}

			// 黑名单检查：跳过已拉黑的 provider
			if isBlacklisted, until := prs.blacklistService.IsBlacklisted(kind, provider.Name); isBlacklisted {
				fmt.Printf("⛔ Provider %s 已拉黑，过期时间: %v\n", provider.Name, until.Format("15:04:05"))
				skippedCount++
				skippedBlacklist++
				continue
			}

			active = append(active, provider)
		}

		if len(active) == 0 {
			respondNoEligibleProviders(c, requestedModel, skippedModel, skippedBlacklist, skippedInvalid)
			return
		}

		fmt.Printf("[INFO] 找到 %d 个可用的 provider（已过滤 %d 个）：", len(active), skippedCount)
		for _, p := range active {
			fmt.Printf("%s ", p.Name)
		}
		fmt.Println()

		// 按 Level 分组
		levelGroups := make(map[int][]Provider)
		for _, provider := range active {
			level := provider.Level
			if level <= 0 {
				level = 1 // 未配置或零值时默认为 Level 1
			}
			levelGroups[level] = append(levelGroups[level], provider)
		}

		// 获取所有 level 并升序排序
		levels := make([]int, 0, len(levelGroups))
		for level := range levelGroups {
			levels = append(levels, level)
		}
		sort.Ints(levels)

		fmt.Printf("[INFO] 共 %d 个 Level 分组：%v\n", len(levels), levels)

		query := flattenQuery(c.Request.URL.Query())
		clientHeaders := cloneHeaders(c.Request.Header)

		// 获取拉黑功能开关状态
		blacklistEnabled := prs.blacklistService.ShouldUseFixedMode()

		// 【拉黑模式】：同 Provider 重试直到被拉黑，然后切换到下一个 Provider
		// 设计目标：Claude Code 单次请求最多重试 3 次，但拉黑阈值可能是 5
		// 通过内部重试机制，在单次请求中累积足够失败次数触发拉黑
		if blacklistEnabled {
			// 缓存轮询设置（单次请求级别，避免重复读取配置文件）
			roundRobinSettingEnabled := prs.isRoundRobinSettingEnabled()
			if roundRobinSettingEnabled {
				fmt.Printf("[INFO] 🔒 拉黑模式 + 轮询负载均衡\n")
			} else {
				fmt.Printf("[INFO] 🔒 拉黑模式（顺序调度）\n")
			}

			// 获取重试配置
			retryConfig := prs.blacklistService.GetRetryConfig()
			maxRetryPerProvider := retryConfig.FailureThreshold
			retryWaitSeconds := retryConfig.RetryWaitSeconds
			fmt.Printf("[INFO] 重试配置: 每 Provider 最多 %d 次重试，间隔 %d 秒\n",
				maxRetryPerProvider, retryWaitSeconds)

			var lastError error
			// 只要有过一次真正的供应商故障，终态就必须维持 502 让 SDK 退避重试
			sawNonClientError := false
			var lastProvider string
			totalAttempts := 0

			busyWaitDeadline := time.Time{}
			enteredBusyWait := false
			defer func() {
				if enteredBusyWait {
					prs.concurrency.leaveWaitPhase()
				}
			}()
			busySkipped := 0
			// 已实际尝试过的供应商：等待阶段重扫不再碰它（失败已计、重试预算不重置）
			attemptedProviders := map[string]bool{}
			// 因并发满被跳过、尚未真实尝试的候选
			busyPending := map[string]concurrencyBusyRef{}
			for {
				busySkipped = 0
				// 每 pass 重建：上一轮候选可能已被拉黑或删除，残留会让容量门控恒真
				busyPending = map[string]concurrencyBusyRef{}
				// 遍历所有 Level 和 Provider
				for _, level := range levels {
					providersInLevel := levelGroups[level]

					// 如果启用轮询，对同 Level 的 providers 进行轮询排序
					if roundRobinSettingEnabled {
						providersInLevel = prs.roundRobinOrder(kind, level, providersInLevel)
					}

					fmt.Printf("[INFO] === 尝试 Level %d（%d 个 provider）===\n", level, len(providersInLevel))

					for _, provider := range providersInLevel {
						if attemptedProviders[strconv.FormatInt(provider.ID, 10)] {
							continue
						}
						// 检查是否已被拉黑（跳过已拉黑的 provider）
						if blacklisted, until := prs.blacklistService.IsBlacklisted(kind, provider.Name); blacklisted {
							fmt.Printf("[INFO] ⏭️ 跳过已拉黑的 Provider: %s (解禁时间: %v)\n", provider.Name, until)
							continue
						}

						// 获取实际模型名
						effectiveModel := provider.GetEffectiveModel(requestedModel)
						currentBodyBytes := bodyBytes
						if effectiveModel != requestedModel && requestedModel != "" {
							fmt.Printf("[INFO] Provider %s 映射模型: %s -> %s\n", provider.Name, requestedModel, effectiveModel)
							modifiedBody, err := ReplaceModelInRequestBody(bodyBytes, effectiveModel)
							if err != nil {
								fmt.Printf("[ERROR] 模型映射失败: %v，跳过此 Provider\n", err)
								continue
							}
							currentBodyBytes = modifiedBody
						}

						// 获取有效端点
						effectiveEndpoint := provider.GetEffectiveEndpoint(endpoint)

						// 同 Provider 内重试循环
						for retryCount := 0; retryCount < maxRetryPerProvider; retryCount++ {
							totalAttempts++

							// 再次检查是否已被拉黑（重试过程中可能被拉黑）
							if blacklisted, _ := prs.blacklistService.IsBlacklisted(kind, provider.Name); blacklisted {
								fmt.Printf("[INFO] 🚫 Provider %s 已被拉黑，切换到下一个\n", provider.Name)
								break
							}

							fmt.Printf("[INFO] [拉黑模式] Provider: %s (Level %d) | 重试 %d/%d | Model: %s\n",
								provider.Name, level, retryCount+1, maxRetryPerProvider, effectiveModel)

							startTime := time.Now()
							ok, err := prs.forwardRequest(c, kind, provider, effectiveEndpoint, query, clientHeaders, currentBodyBytes, isStream, effectiveModel, configGen)
							duration := time.Since(startTime)

							if ok {
								fmt.Printf("[INFO] ✓ 成功: %s | 重试 %d 次 | 耗时: %.2fs\n",
									provider.Name, retryCount+1, duration.Seconds())
								if err := prs.blacklistService.RecordSuccess(kind, provider.Name); err != nil {
									fmt.Printf("[WARN] 清零失败计数失败: %v\n", err)
								}
								prs.setLastUsedProvider(kind, provider.Name)
								return
							}

							// 并发满载：不算尝试、不计失败，换下一个供应商
							if errors.Is(err, errProviderBusy) {
								totalAttempts--
								// 已真实失败过的供应商重试遇忙不再进等待候选：
								// 下一 pass 必然跳过它，等它只会把失败聚合错改成 503
								if pk := strconv.FormatInt(provider.ID, 10); !attemptedProviders[pk] {
									busySkipped++
									busyPending[pk] = concurrencyBusyRef{Key: pk, Limit: provider.MaxConcurrency, Gen: configGen}
								}
								fmt.Printf("[INFO] Provider %s 并发已满，跳过\n", provider.Name)
								break
							}
							// 实际尝试过：等待阶段重扫不再碰它
							attemptedProviders[strconv.FormatInt(provider.ID, 10)] = true
							delete(busyPending, strconv.FormatInt(provider.ID, 10))

							// 失败处理
							lastError = err
							lastProvider = provider.Name

							errorMsg := "未知错误"
							if err != nil {
								errorMsg = err.Error()
							}
							fmt.Printf("[WARN] ✗ 失败: %s | 重试 %d/%d | 错误: %s | 耗时: %.2fs\n",
								provider.Name, retryCount+1, maxRetryPerProvider, errorMsg, duration.Seconds())

							// 客户端请求被拒绝（不支持的格式/功能）：直接返回 400，不重试不拉黑
							if errors.Is(err, ErrClientRequestRejected) {
								fmt.Printf("[INFO] 🚫 客户端请求被拒绝: %s\n", errorMsg)
								c.JSON(http.StatusBadRequest, gin.H{
									"type":    "error",
									"error":   map[string]string{"type": "invalid_request_error", "message": errorMsg},
									"message": errorMsg,
								})
								return
							}

							// 客户端中断不计入失败次数，直接返回
							if errors.Is(err, errClientAbort) {
								fmt.Printf("[INFO] 客户端中断，停止重试\n")
								return
							}

							// 上游 2xx 后中途断流：响应已部分写出，不能再换供应商（会写出两段响应），
							// 但必须计入失败，否则半死的供应商永远不会被拉黑
							if errors.Is(err, errUpstreamStreamAborted) {
								if err := prs.blacklistService.RecordFailure(kind, provider.Name); err != nil {
									fmt.Printf("[ERROR] 记录失败到黑名单失败: %v\n", err)
								}
								return
							}

							// 上游判定"请求内容本身有问题"：换供应商也一样失败，
							// 不计入失败次数（否则一个坏请求能把全部供应商拉黑），直接换下一个供应商
							if errors.Is(err, errUpstreamClientError) {
								fmt.Printf("[INFO] 上游拒绝请求内容，不计供应商失败，切换到下一个: %s\n", errorMsg)
								break
							}

							sawNonClientError = true

							// 记录失败次数（可能触发拉黑）
							if err := prs.blacklistService.RecordFailure(kind, provider.Name); err != nil {
								fmt.Printf("[ERROR] 记录失败到黑名单失败: %v\n", err)
							}

							// 检查是否刚被拉黑
							if blacklisted, _ := prs.blacklistService.IsBlacklisted(kind, provider.Name); blacklisted {
								fmt.Printf("[INFO] 🚫 Provider %s 达到失败阈值，已被拉黑，切换到下一个\n", provider.Name)
								break
							}

							// 多地址池已在本次请求内整轮试过：不再按拉黑阈值原地重试
							// （那会放大成 阈值×地址数 次网络发送），失败已计一次，
							// 直接切下一供应商
							if errors.Is(err, errEndpointPoolExhausted) {
								fmt.Printf("[INFO] Provider %s 地址池耗尽，切换下一供应商\n", provider.Name)
								break
							}

							// 等待后重试（除非是最后一次）；等待期间客户端可能已经离开，
							// 此时继续重试只是白烧上游额度
							if retryCount < maxRetryPerProvider-1 {
								fmt.Printf("[INFO] ⏳ 等待 %d 秒后重试...\n", retryWaitSeconds)
								select {
								case <-time.After(time.Duration(retryWaitSeconds) * time.Second):
								case <-c.Request.Context().Done():
									fmt.Printf("[INFO] 等待重试期间客户端已断开，停止尝试\n")
									return
								}
							}
						}

						if c.Request.Context().Err() != nil {
							fmt.Printf("[INFO] 客户端已断开，停止尝试后续 Provider\n")
							return
						}
					}
				}

				// 一整遍下来只要还有因并发满被跳过的供应商，就进入有界等待
				if busySkipped == 0 {
					break
				}
				if busyWaitDeadline.IsZero() {
					busyWaitDeadline = time.Now().Add(prs.concurrency.waitBudget)
					if !prs.concurrency.enterWaitPhase() {
						respondAllBusy(c, kind)
						return
					}
					enteredBusyWait = true
				}
				// 唤醒以"忙候选真的有空位"为门控：本轮实际尝试供应商的正常释放
				// 也会触发全局信号，不加门控直接重扫会形成自唤醒重试风暴
				woke := false
				for {
					capSignal := prs.concurrency.releaseSignal()
					if prs.concurrency.anyCapacity(kind, busyPending) {
						woke = true
						break
					}
					if !prs.concurrency.waitForRelease(c.Request.Context(), busyWaitDeadline, capSignal) {
						break
					}
				}
				if !woke {
					respondAllBusy(c, kind)
					return
				}
				// 容量门控可能被"释放后立刻又被占走"的候选反复触发，
				// 重扫前硬校验总预算与客户端 context，防止空转越过 deadline
				if c.Request.Context().Err() != nil || time.Now().After(busyWaitDeadline) {
					respondAllBusy(c, kind)
					return
				}
				fmt.Printf("[INFO] 并发配额有释放，重扫供应商\n")
			}

			// 所有 Provider 都失败或被拉黑
			fmt.Printf("[ERROR] 💥 拉黑模式：所有 Provider 都失败或被拉黑（共尝试 %d 次）\n", totalAttempts)

			errorMsg := "未知错误"
			if lastError != nil {
				errorMsg = lastError.Error()
			}
			respondAllProvidersFailed(c, lastError, !sawNonClientError, gin.H{
				"error":         fmt.Sprintf("所有 Provider 都失败或被拉黑，最后尝试: %s - %s", lastProvider, errorMsg),
				"lastProvider":  lastProvider,
				"totalAttempts": totalAttempts,
				"mode":          "blacklist_retry",
				"hint":          "拉黑模式已开启，同 Provider 重试到拉黑再切换。如需立即降级请关闭拉黑功能",
			})
			return
		}

		// 【降级模式】：拉黑功能关闭，失败自动尝试下一个 provider
		roundRobinEnabled := prs.isRoundRobinEnabled()
		if roundRobinEnabled {
			fmt.Printf("[INFO] 🔄 降级模式 + 轮询负载均衡\n")
		} else {
			fmt.Printf("[INFO] 🔄 降级模式（顺序降级）\n")
		}

		var lastError error
		// 只要有过一次真正的供应商故障，终态就必须维持 502 让 SDK 退避重试
		sawNonClientError := false
		var lastProvider string
		var lastDuration time.Duration
		totalAttempts := 0

		busyWaitDeadline := time.Time{}
		enteredBusyWait := false
		defer func() {
			if enteredBusyWait {
				prs.concurrency.leaveWaitPhase()
			}
		}()
		busySkipped := 0
		// 已实际尝试过的供应商：等待阶段重扫不再碰它（失败已计、重试预算不重置）
		attemptedProviders := map[string]bool{}
		// 因并发满被跳过、尚未真实尝试的候选
		busyPending := map[string]concurrencyBusyRef{}
		for {
			busySkipped = 0
			// 每 pass 重建：上一轮候选可能已被拉黑或删除，残留会让容量门控恒真
			busyPending = map[string]concurrencyBusyRef{}
			for _, level := range levels {
				providersInLevel := levelGroups[level]

				// 如果启用轮询，对同 Level 的 providers 进行轮询排序
				if roundRobinEnabled {
					providersInLevel = prs.roundRobinOrder(kind, level, providersInLevel)
				}

				fmt.Printf("[INFO] === 尝试 Level %d（%d 个 provider）===\n", level, len(providersInLevel))

				for i, provider := range providersInLevel {
					if attemptedProviders[strconv.FormatInt(provider.ID, 10)] {
						continue
					}
					totalAttempts++

					// 获取实际应该使用的模型名
					effectiveModel := provider.GetEffectiveModel(requestedModel)

					// 如果需要映射，修改请求体
					currentBodyBytes := bodyBytes
					if effectiveModel != requestedModel && requestedModel != "" {
						fmt.Printf("[INFO] Provider %s 映射模型: %s -> %s\n", provider.Name, requestedModel, effectiveModel)

						modifiedBody, err := ReplaceModelInRequestBody(bodyBytes, effectiveModel)
						if err != nil {
							fmt.Printf("[ERROR] 替换模型名失败: %v\n", err)
							// 映射失败不应阻止尝试其他 provider
							continue
						}
						currentBodyBytes = modifiedBody
					}

					fmt.Printf("[INFO]   [%d/%d] Provider: %s | Model: %s\n", i+1, len(providersInLevel), provider.Name, effectiveModel)

					// 尝试发送请求
					// 获取有效的端点（用户配置优先）
					effectiveEndpoint := provider.GetEffectiveEndpoint(endpoint)
					startTime := time.Now()
					ok, err := prs.forwardRequest(c, kind, provider, effectiveEndpoint, query, clientHeaders, currentBodyBytes, isStream, effectiveModel, configGen)
					duration := time.Since(startTime)

					if ok {
						fmt.Printf("[INFO]   ✓ Level %d 成功: %s | 耗时: %.2fs\n", level, provider.Name, duration.Seconds())

						// 成功：清零连续失败计数
						if err := prs.blacklistService.RecordSuccess(kind, provider.Name); err != nil {
							fmt.Printf("[WARN] 清零失败计数失败: %v\n", err)
						}

						// 记录最后使用的供应商
						prs.setLastUsedProvider(kind, provider.Name)

						return // 成功，立即返回
					}

					// 并发满载：不算尝试、不计失败，换下一个供应商
					if errors.Is(err, errProviderBusy) {
						totalAttempts--
						busySkipped++
						pk := strconv.FormatInt(provider.ID, 10)
						busyPending[pk] = concurrencyBusyRef{Key: pk, Limit: provider.MaxConcurrency, Gen: configGen}
						fmt.Printf("[INFO] Provider %s 并发已满，跳过\n", provider.Name)
						continue
					}
					// 实际尝试过：等待阶段重扫不再碰它
					attemptedProviders[strconv.FormatInt(provider.ID, 10)] = true
					delete(busyPending, strconv.FormatInt(provider.ID, 10))

					// 失败：记录错误并尝试下一个
					lastError = err
					lastProvider = provider.Name
					lastDuration = duration

					errorMsg := "未知错误"
					if err != nil {
						errorMsg = err.Error()
					}
					fmt.Printf("[WARN]   ✗ Level %d 失败: %s | 错误: %s | 耗时: %.2fs\n",
						level, provider.Name, errorMsg, duration.Seconds())

					// 客户端请求被拒绝（不支持的格式/功能）：直接返回 400，不重试不拉黑
					if errors.Is(err, ErrClientRequestRejected) {
						fmt.Printf("[INFO] 🚫 客户端请求被拒绝: %s\n", errorMsg)
						c.JSON(http.StatusBadRequest, gin.H{
							"type":    "error",
							"error":   map[string]string{"type": "invalid_request_error", "message": errorMsg},
							"message": errorMsg,
						})
						return
					}

					// 客户端中断不计入失败次数，且没必要再换供应商
					if errors.Is(err, errClientAbort) {
						fmt.Printf("[INFO] 客户端中断，跳过失败计数: %s\n", provider.Name)
						return
					}

					// 上游 2xx 后中途断流：响应已部分写出，不能再降级，但必须计入失败
					if errors.Is(err, errUpstreamStreamAborted) {
						if err := prs.blacklistService.RecordFailure(kind, provider.Name); err != nil {
							fmt.Printf("[ERROR] 记录失败到黑名单失败: %v\n", err)
						}
						return
					}

					// 上游判定"请求内容本身有问题"：不计入供应商失败，继续尝试下一个
					if errors.Is(err, errUpstreamClientError) {
						fmt.Printf("[INFO] 上游拒绝请求内容，不计供应商失败: %s\n", errorMsg)
					} else {
						sawNonClientError = true
						if err := prs.blacklistService.RecordFailure(kind, provider.Name); err != nil {
							fmt.Printf("[ERROR] 记录失败到黑名单失败: %v\n", err)
						}
					}

					if c.Request.Context().Err() != nil {
						fmt.Printf("[INFO] 客户端已断开，停止尝试后续 Provider\n")
						return
					}

					// 发送切换通知：检查是否有下一个可用的 provider
					if prs.notificationService != nil {
						nextProvider := ""
						// 先查找同级别的下一个
						if i+1 < len(providersInLevel) {
							nextProvider = providersInLevel[i+1].Name
						} else {
							// 查找下一个 level 的第一个 provider
							for _, nextLevel := range levels {
								if nextLevel > level && len(levelGroups[nextLevel]) > 0 {
									nextProvider = levelGroups[nextLevel][0].Name
									break
								}
							}
						}
						if nextProvider != "" {
							prs.notificationService.NotifyProviderSwitch(SwitchNotification{
								FromProvider: provider.Name,
								ToProvider:   nextProvider,
								Reason:       errorMsg,
								Platform:     kind,
							})
						}
					}
				}

				fmt.Printf("[WARN] Level %d 的所有 %d 个 provider 均失败，尝试下一 Level\n", level, len(providersInLevel))
			}

			// 一整遍下来只要还有因并发满被跳过的供应商，就进入有界等待
			if busySkipped == 0 {
				break
			}
			if busyWaitDeadline.IsZero() {
				busyWaitDeadline = time.Now().Add(prs.concurrency.waitBudget)
				if !prs.concurrency.enterWaitPhase() {
					respondAllBusy(c, kind)
					return
				}
				enteredBusyWait = true
			}
			// 唤醒以"忙候选真的有空位"为门控：本轮实际尝试供应商的正常释放
			// 也会触发全局信号，不加门控直接重扫会形成自唤醒重试风暴
			woke := false
			for {
				capSignal := prs.concurrency.releaseSignal()
				if prs.concurrency.anyCapacity(kind, busyPending) {
					woke = true
					break
				}
				if !prs.concurrency.waitForRelease(c.Request.Context(), busyWaitDeadline, capSignal) {
					break
				}
			}
			if !woke {
				respondAllBusy(c, kind)
				return
			}
			// 容量门控可能被"释放后立刻又被占走"的候选反复触发，
			// 重扫前硬校验总预算与客户端 context，防止空转越过 deadline
			if c.Request.Context().Err() != nil || time.Now().After(busyWaitDeadline) {
				respondAllBusy(c, kind)
				return
			}
			fmt.Printf("[INFO] 并发配额有释放，重扫供应商\n")
		}

		// 所有 provider 都失败，返回 502
		errorMsg := "未知错误"
		if lastError != nil {
			errorMsg = lastError.Error()
		}
		fmt.Printf("[ERROR] 所有 %d 个 provider 均失败，最后尝试: %s | 错误: %s\n",
			totalAttempts, lastProvider, errorMsg)

		respondAllProvidersFailed(c, lastError, !sawNonClientError, gin.H{
			"error":          fmt.Sprintf("所有 %d 个 provider 均失败，最后错误: %s", totalAttempts, errorMsg),
			"last_provider":  lastProvider,
			"last_duration":  fmt.Sprintf("%.2fs", lastDuration.Seconds()),
			"total_attempts": totalAttempts,
		})
	}
}

func (prs *ProviderRelayService) forwardRequest(
	c *gin.Context,
	kind string,
	provider Provider,
	endpoint string,
	query map[string]string,
	clientHeaders map[string]string,
	bodyBytes []byte,
	isStream bool,
	model string,
	configGen int64,
) (bool, error) {
	headers := cloneMap(clientHeaders)

	// ========== 协议转换检测 ==========
	upstreamProtocol := provider.ResolveUpstreamProtocol(endpoint)
	var sseConverter *OpenAIToAnthropicSSEConverter
	var convertInfo ConvertInfo

	// codex 走的是 OpenAI Responses 协议，请求体不是 Anthropic Messages 格式。
	// 若供应商被误配成 openai_chat，套用 Anthropic→OpenAI 转换只会产出无意义的请求体，
	// 这里直接按原样转发并告警，避免静默损坏请求。
	if upstreamProtocol == UpstreamProtocolOpenAIChat && kind == "codex" {
		fmt.Printf("[协议转换] Provider %s 被配置为 OpenAI Chat，但 codex 使用 Responses 协议，跳过请求体转换\n", provider.Name)
		upstreamProtocol = UpstreamProtocolAnthropic
	}

	// 如果上游是 OpenAI Chat，需要转换请求体
	if upstreamProtocol == UpstreamProtocolOpenAIChat {
		fmt.Printf("[协议转换] Provider %s 使用 OpenAI Chat 协议\n", provider.Name)

		// 转换请求体
		opts := DefaultConvertOptions()
		convertedBody, info, err := ConvertAnthropicToOpenAI(bodyBytes, opts)
		if err != nil {
			// 客户端请求被拒绝（不支持的功能）
			return false, err
		}
		bodyBytes = convertedBody
		convertInfo = info

		// 打印转换信息
		if len(info.DroppedMetadataKeys) > 0 {
			fmt.Printf("[协议转换] 丢弃 metadata keys: %v\n", info.DroppedMetadataKeys)
		}
		if len(info.DroppedFields) > 0 {
			fmt.Printf("[协议转换] 丢弃顶层字段: %v\n", info.DroppedFields)
		}
		if info.MappedUser != "" {
			fmt.Printf("[协议转换] metadata.user_id -> user: %s\n", info.MappedUser)
		}

		// 创建 SSE 转换器（用于响应处理）
		sseConverter = NewOpenAIToAnthropicSSEConverter(model)
	}
	_ = convertInfo // 避免未使用警告

	// 先清掉客户端自带的凭据与压缩协商，再注入本代理的供应商凭据
	sanitizeUpstreamHeaders(headers)

	// 请求清理（头部）：在注入供应商凭据之前执行，用户配置的黑名单删不到中继写入的认证头
	if provider.RequestSanitizeEnabled {
		headers = sanitizeHeaders(headers, provider.SanitizeConfig)
	}

	// 根据认证方式设置请求头（默认 Bearer，与 v2.2.x 保持一致）
	authType := strings.ToLower(strings.TrimSpace(provider.ConnectivityAuthType))
	switch authType {
	case "x-api-key":
		// 仅当用户显式选择 x-api-key 时使用（Anthropic 官方 API）
		setHeaderCanonical(headers, "x-api-key", provider.APIKey)
		// 只有 Anthropic 协议的 Anthropic 类平台才注入 anthropic-version，
		// codex 的 /responses 是 OpenAI Responses 协议，注入该头没有意义
		if upstreamProtocol == UpstreamProtocolAnthropic && kind != "codex" {
			setHeaderCanonical(headers, "anthropic-version", "2023-06-01")
		}
	case "", "bearer":
		// 默认使用 Bearer token（兼容所有第三方中转）
		setHeaderCanonical(headers, "authorization", fmt.Sprintf("Bearer %s", provider.APIKey))
	default:
		// 自定义 Header 名
		headerName := strings.TrimSpace(provider.ConnectivityAuthType)
		if headerName == "" || strings.EqualFold(headerName, "custom") {
			headerName = "Authorization"
		}
		setHeaderCanonical(headers, headerName, provider.APIKey)
	}

	// OpenAI 协议时移除 Anthropic 专用头
	if upstreamProtocol == UpstreamProtocolOpenAIChat {
		deleteHeaderFold(headers, "anthropic-version", "anthropic-beta", "x-api-key")
		// 确保使用 Bearer 认证（上一步可能把 x-api-key 型凭据删掉了）
		if getHeaderFold(headers, "authorization") == "" {
			setHeaderCanonical(headers, "authorization", fmt.Sprintf("Bearer %s", provider.APIKey))
		}
	}

	if getHeaderFold(headers, "accept") == "" {
		setHeaderCanonical(headers, "accept", "application/json")
	}

	// 请求清理（请求体）：在协议转换之后、发送之前执行，作用于实际出站 body
	// （openai_chat 路径的转换器本身按白名单重建 body，这里只是兜底）
	if provider.RequestSanitizeEnabled {
		if cleaned, removed := sanitizeRequestBody(bodyBytes, provider.SanitizeConfig); len(removed) > 0 {
			fmt.Printf("[Sanitize] Provider %s: 移除请求体字段 %v\n", provider.Name, removed)
			bodyBytes = cleaned
		}
	}

	// 并发配额：在本地校验/协议转换之后获取——满载时不能把本应确定
	// 返回的 400 客户端错误变成"忙"。占用覆盖地址池遍历与 SSE 转发全程
	// （本函数同步转发到流结束才返回），defer 释放即为流结束时机。
	concurrencyProviderKey := strconv.FormatInt(provider.ID, 10)
	if !prs.concurrency.TryAcquire(kind, concurrencyProviderKey, provider.MaxConcurrency, configGen) {
		return false, errProviderBusy
	}
	defer prs.concurrency.Release(kind, concurrencyProviderKey)

	requestLog := &ReqeustLog{
		Platform: kind,
		Provider: provider.Name,
		Model:    model,
		IsStream: isStream,
	}
	// 抓包模式：录制终态出站 headers/body（映射/清理/认证注入均已完成，
	// 即实际进 transport 前的应用层形态）。地址池内各地址仅 URL 不同，采集一次。
	// 代次先于开关与内容读取：清除/关停若与采集竞态，只会让本行被误清（安全方向）
	// 抓包状态一次性快照（读锁内）：开关/会话/代次分开裸读会在与关闭、
	// 清除的竞态下拼出错位组合
	if enabled, sessionID, gen := prs.captureSnapshot(); enabled {
		requestLog.captureGen = gen
		requestLog.CaptureSessionID = sessionID
		requestLog.RequestHeaders = maskCaptureHeaders(headers, provider.ConnectivityAuthType, provider.APIKey)
		requestLog.RequestBody, requestLog.BodyTruncated, requestLog.BodyBytes = redactCaptureBody(bodyBytes, provider.APIKey)
	}
	start := time.Now()
	defer func() {
		requestLog.DurationSec = time.Since(start).Seconds()
		// 若请求过程中发生 rename,把旧名兑换成新名再落库
		requestLog.Provider = ResolveProviderAlias(requestLog.Platform, requestLog.Provider)
		// 读锁覆盖"代次校验 + 提交"全程,与清除的写锁互斥,堵死校验后提交前的清除窗口
		prs.captureWriteMu.RLock()
		defer prs.captureWriteMu.RUnlock()
		prs.stripStaleCapture(requestLog)

		if err := prs.writeRequestLog(requestLog); err != nil {
			fmt.Printf("写入 request_log 失败: %v\n", err)
		}
	}()

	// ========== 地址池遍历（issue #27）==========
	// 单地址供应商：行为与旧实现完全一致（含 HTTP 层 1 次自动重试）。
	// 多地址供应商：同一请求内每个地址至多试一次，仅传输层失败/408/421/429/5xx
	// 且响应未提交时切下一地址；全部失败返回 errEndpointPoolExhausted，
	// 调用方记一次供应商失败后立即换供应商。整个池遍历共用上面这一条 requestLog。
	pool := provider.EndpointPool()
	if len(pool) == 0 {
		return false, fmt.Errorf("provider %s 没有可用的 API 地址", provider.Name)
	}
	multiAddress := len(pool) > 1
	if multiAddress {
		pool = prs.endpointCooldowns.Order(kind, provider.ID, pool)
	}

	var lastErr error
	primaryKey := normalizeURL(provider.APIURL)
	for i, addr := range pool {
		if i > 0 {
			// 上一地址的失败状态码不能残留进本次尝试的日志
			requestLog.HttpCode = 0
			// SSE 转换器有状态，跨地址复用会串流，换新
			if sseConverter != nil {
				sseConverter = NewOpenAIToAnthropicSSEConverter(model)
			}
			fmt.Printf("[INFO] Provider %s 地址兜底: 改试 %s\n", provider.Name, addr)
		}

		ok, err := prs.forwardToAddress(c, kind, provider, joinURL(addr, endpoint), query, headers, bodyBytes, isStream, sseConverter, requestLog, !multiAddress)
		if ok {
			if multiAddress {
				prs.endpointCooldowns.MarkSuccess(kind, provider.ID, addr)
				// 冷却重排后备用地址可能排在首位，不能拿下标判断主备身份
				if normalizeURL(addr) != primaryKey {
					fmt.Printf("[WARN] Provider %s 主地址失败或冷却中，备用地址 %s 接管本次请求\n", provider.Name, addr)
				}
			}
			return true, nil
		}

		lastErr = err
		if !multiAddress {
			return false, err
		}
		if !addressSwitchableError(err) || c.Writer.Written() {
			return false, err
		}
		prs.endpointCooldowns.MarkFailure(kind, provider.ID, addr, retryAfterOf(err))
		fmt.Printf("[WARN] Provider %s 地址 %s 失败，冷却后改试下一地址: %v\n", provider.Name, addr, err)
	}
	return false, fmt.Errorf("%w: %v", errEndpointPoolExhausted, lastErr)
}

// forwardToAddress 向单个地址发一次请求并转发响应。
// singleAddress=true 时保留 HTTP 层 1 次自动重试（旧行为）；
// 多地址路径关闭隐藏重试，重试预算统一由地址池承担。
func (prs *ProviderRelayService) forwardToAddress(
	c *gin.Context,
	kind string,
	provider Provider,
	targetURL string,
	query map[string]string,
	headers map[string]string,
	bodyBytes []byte,
	isStream bool,
	sseConverter *OpenAIToAnthropicSSEConverter,
	requestLog *ReqeustLog,
	singleAddress bool,
) (bool, error) {
	// 绑定客户端 context：客户端取消（用户 Ctrl-C / CLI 超时断开）时立即释放上游连接，
	// 否则处理协程与上游请求会一直挂到 32 小时超时，上游还在持续产出并计费。
	// 超时与连接池由共享客户端统一提供，不再每请求新建 Transport。
	req := xrequest.New().
		SetClient(relayClientFor(provider.InsecureSkipVerify, provider.Name)).
		WithContext(c.Request.Context()).
		SetHeaders(headers).
		SetQueryParams(query)
	if singleAddress {
		req = req.SetRetry(1, 500*time.Millisecond)
	}

	reqBody := bytes.NewReader(bodyBytes)
	req = req.SetBody(reqBody)

	resp, err := req.Post(targetURL)

	// 无论成功失败，先尝试记录 HttpCode
	if resp != nil {
		requestLog.HttpCode = resp.StatusCode()
	}

	if err != nil {
		// 客户端已断开：不是供应商故障，不计入失败次数
		if c.Request.Context().Err() != nil || errors.Is(err, context.Canceled) {
			fmt.Printf("[INFO] Provider %s 请求期间客户端已断开，不计入供应商失败\n", provider.Name)
			return false, fmt.Errorf("%w: %v", errClientAbort, err)
		}
		// 尝试从响应体提取供应商原始错误信息
		if resp != nil {
			if upstreamBody := extractUpstreamError(resp); upstreamBody != "" {
				return false, newUpstreamStatusError(resp, resp.StatusCode(),
					fmt.Sprintf("upstream status %d: %s", resp.StatusCode(), upstreamBody))
			}
		}
		return false, err
	}

	if resp == nil {
		return false, fmt.Errorf("empty response")
	}

	status := requestLog.HttpCode

	if resp.Error() != nil {
		// 客户端已断开：不是供应商故障，不计入失败次数
		if c.Request.Context().Err() != nil {
			fmt.Printf("[INFO] Provider %s 响应期间客户端已断开，不计入供应商失败\n", provider.Name)
			return false, fmt.Errorf("%w: %v", errClientAbort, resp.Error())
		}
		// 优先使用 extractUpstreamError 提取完整错误（覆盖 SSE 空 body 场景）
		errMsg := strings.TrimSpace(resp.Error().Error())
		if errMsg == "" {
			if upstreamBody := extractUpstreamError(resp); upstreamBody != "" {
				errMsg = upstreamBody
			}
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("upstream status %d", status)
		}
		if isClientSideUpstreamStatus(status) {
			return false, fmt.Errorf("%w: upstream status %d: %s", errUpstreamClientError, status, errMsg)
		}
		return false, newUpstreamStatusError(resp, status, fmt.Sprintf("upstream status %d: %s", status, errMsg))
	}

	// 状态码为 0 且无错误：当作成功处理
	if status == 0 {
		fmt.Printf("[WARN] Provider %s 返回状态码 0，但无错误，当作成功处理\n", provider.Name)
		return prs.relayResponseToClient(c, kind, provider, resp, sseConverter, isStream, requestLog)
	}

	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		return prs.relayResponseToClient(c, kind, provider, resp, sseConverter, isStream, requestLog)
	}

	// 尝试从响应体提取供应商原始错误信息
	upstreamBody := extractUpstreamError(resp)
	detail := fmt.Sprintf("upstream status %d", status)
	if upstreamBody != "" {
		detail = fmt.Sprintf("upstream status %d: %s", status, upstreamBody)
	}
	// 请求内容本身被拒绝：换供应商也一样，不计入供应商失败次数
	if isClientSideUpstreamStatus(status) {
		return false, fmt.Errorf("%w: %s", errUpstreamClientError, detail)
	}
	return false, newUpstreamStatusError(resp, status, detail)
}

// newUpstreamStatusError 构造带状态码的上游失败；429 时顺带解析 Retry-After
// 供地址冷却使用
func newUpstreamStatusError(resp *xrequest.Response, status int, detail string) *upstreamStatusError {
	e := &upstreamStatusError{status: status, detail: detail}
	if status == http.StatusTooManyRequests && resp != nil && resp.RawResponse != nil {
		e.retryAfter = parseRetryAfter(resp.RawResponse.Header.Get("Retry-After"), time.Now())
	}
	return e
}

// relayResponseToClient 把上游 2xx 响应转发给客户端并区分三种收尾情况：
//   - 完整转发成功；
//   - 客户端主动断开（不计供应商失败）；
//   - 上游中途断流（响应已部分写出，不能再降级，但必须计供应商失败，
//     否则半死的供应商每次都被判成功、失败计数被清零而永远不会被拉黑）。
func (prs *ProviderRelayService) relayResponseToClient(
	c *gin.Context,
	kind string,
	provider Provider,
	resp *xrequest.Response,
	sseConverter *OpenAIToAnthropicSSEConverter,
	isStream bool,
	requestLog *ReqeustLog,
) (bool, error) {
	var copyErr error
	if sseConverter != nil && isStream {
		// 使用协议转换 Hook
		_, copyErr = resp.ToHttpResponseWriter(c.Writer, protocolConvertHook(sseConverter, kind, requestLog))
		// 上游未发 [DONE] 就断开时补齐终止事件序列，否则客户端一直等 message_stop，
		// 且 message_delta 里已捕获的 usage 也会随之丢失。
		// 只在响应确实已经开始写出时才补：一个字节都没写出去的失败要留给降级重试，
		// 否则会给客户端伪造一条"完整但空"的消息，用户看到空回答还没有任何报错。
		if c.Writer.Written() {
			if tail := sseConverter.FinalizeIfUnterminated(); tail != "" {
				parseEventPayload(tail, ClaudeCodeParseTokenUsageFromResponse, requestLog)
				if _, writeErr := c.Writer.Write([]byte(tail)); writeErr == nil {
					c.Writer.Flush()
				}
			}
		}
	} else {
		var written int64
		written, copyErr = resp.ToHttpResponseWriter(c.Writer, ReqeustLogHook(c, kind, requestLog))
		// 非流式路径 xrequest 内部是 `body, _ := io.ReadAll(...)`，上游中途断流的读错误被丢弃，
		// 截断的响应会被当成完整响应返回 nil，坏供应商反而被记成功、永远不会被拉黑。
		// 上游声明了 Content-Length 时用它兜底校验实际写出的字节数。
		if copyErr == nil {
			if truncErr := checkNonStreamTruncated(resp, written); truncErr != nil {
				copyErr = truncErr
			}
		}
	}

	if copyErr == nil {
		return true, nil
	}

	if c.Request.Context().Err() != nil || errors.Is(copyErr, context.Canceled) {
		fmt.Printf("[INFO] Provider %s 转发过程中客户端断开，不计入供应商失败\n", provider.Name)
		return false, fmt.Errorf("%w: %v", errClientAbort, copyErr)
	}

	// 一个字节都没写给客户端（例如 xrequest 在 Peek 阶段就读失败，此时响应头还没发出）：
	// 仍可安全降级到下一个供应商，按普通失败上报
	if !c.Writer.Written() {
		fmt.Printf("[WARN] Provider %s 响应读取失败且尚未写出任何内容，可降级: %v\n", provider.Name, copyErr)
		return false, fmt.Errorf("upstream read failed before response started: %w", copyErr)
	}

	fmt.Printf("[WARN] Provider %s 上游中途断流（响应已部分写出，无法降级）: %v\n", provider.Name, copyErr)
	return false, fmt.Errorf("%w: %v", errUpstreamStreamAborted, copyErr)
}

// extractUpstreamError 从供应商响应中提取原始错误信息（最多 512 字节）
func extractUpstreamError(resp *xrequest.Response) string {
	if resp == nil {
		return ""
	}
	// 错误响应不会再走 ToHttpResponseWriter（那里才有 Body.Close），
	// 这里必须自己关闭，否则每次失败都泄漏一条上游连接
	defer func() {
		if resp.RawResponse != nil && resp.RawResponse.Body != nil {
			_ = resp.RawResponse.Body.Close()
		}
	}()
	// 优先尝试 String()（会自动解压 gzip 等）
	body := resp.String()
	// SSE 流式响应时 String() 返回空，回退到直接读取 RawResponse.Body（带超时防御）
	if body == "" && resp.RawResponse != nil && resp.RawResponse.Body != nil {
		done := make(chan []byte, 1)
		go func() {
			raw, err := io.ReadAll(io.LimitReader(resp.RawResponse.Body, 512))
			if err == nil {
				done <- raw
			} else {
				done <- nil
			}
		}()
		select {
		case raw := <-done:
			if raw != nil {
				body = string(raw)
			}
		case <-time.After(500 * time.Millisecond):
			// 超时放弃，关闭 Body 中断后台读取，避免 goroutine 泄漏
			resp.RawResponse.Body.Close()
		}
	}
	if body == "" {
		return ""
	}
	// 截断过长的错误信息
	if len(body) > 512 {
		body = body[:512] + "..."
	}
	return body
}

func cloneHeaders(header http.Header) map[string]string {
	cloned := make(map[string]string, len(header))
	for key, values := range header {
		if len(values) > 0 {
			cloned[key] = values[len(values)-1]
		}
	}
	return cloned
}

func cloneMap(m map[string]string) map[string]string {
	cloned := make(map[string]string, len(m))
	for k, v := range m {
		cloned[k] = v
	}
	return cloned
}

func flattenQuery(values map[string][]string) map[string]string {
	query := make(map[string]string, len(values))
	for key, items := range values {
		if len(items) > 0 {
			query[key] = items[len(items)-1]
		}
	}
	return query
}

func joinURL(base string, endpoint string) string {
	base = strings.TrimSuffix(base, "/")
	endpoint = "/" + strings.TrimPrefix(endpoint, "/")
	return base + endpoint
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func requestLogColumnExists(db *sql.DB, column string) (bool, error) {
	var count int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('request_log') WHERE name = ?", column,
	).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func ensureRequestLogColumn(db *sql.DB, column string, definition string) error {
	exists, err := requestLogColumnExists(db, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	alter := fmt.Sprintf("ALTER TABLE request_log ADD COLUMN %s %s", column, definition)
	_, err = db.Exec(alter)
	return err
}

// ensureRequestLogCreatedAt 单独处理 created_at 列的迁移。
// SQLite 不允许 ALTER TABLE ADD COLUMN 带 CURRENT_TIMESTAMP 这类非常量默认值
// （报 "Cannot add a column with non-constant default"）。
// 建表时就没有该列的旧库若走通用迁移会直接失败，进而让 InitDatabase 返回错误、应用无法启动。
// 这里改为：加不带默认值的列 → 回填历史行 → 用触发器为后续 INSERT 补时间戳
// （INSERT 语句不显式写 created_at，没有触发器就会留下 NULL，按时间统计的用量与成本全废）。
func ensureRequestLogCreatedAt(db *sql.DB) error {
	exists, err := requestLogColumnExists(db, "created_at")
	if err != nil {
		return err
	}
	if !exists {
		if _, err := db.Exec("ALTER TABLE request_log ADD COLUMN created_at DATETIME"); err != nil {
			return err
		}
	}
	if _, err := db.Exec("UPDATE request_log SET created_at = CURRENT_TIMESTAMP WHERE created_at IS NULL"); err != nil {
		return err
	}
	// 新建库的 created_at 自带 DEFAULT CURRENT_TIMESTAMP，触发器条件不会命中；
	// 迁移补出来的列没有默认值，全靠该触发器兜底。
	_, err = db.Exec(`CREATE TRIGGER IF NOT EXISTS request_log_created_at_default
		AFTER INSERT ON request_log FOR EACH ROW WHEN NEW.created_at IS NULL
		BEGIN
			UPDATE request_log SET created_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
		END`)
	return err
}

func ensureRequestLogTable() error {
	db, err := xdb.DB("default")
	if err != nil {
		return err
	}
	return ensureRequestLogTableWithDB(db)
}

func ensureRequestLogTableWithDB(db *sql.DB) error {
	const createTableSQL = `CREATE TABLE IF NOT EXISTS request_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		platform TEXT,
		model TEXT,
		provider TEXT,
		http_code INTEGER,
		input_tokens INTEGER,
		output_tokens INTEGER,
		cache_create_tokens INTEGER,
		cache_read_tokens INTEGER,
		reasoning_tokens INTEGER,
		is_stream INTEGER DEFAULT 0,
		duration_sec REAL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`

	if _, err := db.Exec(createTableSQL); err != nil {
		return err
	}

	// 历史新增列按声明顺序补齐,旧库也能顺利升级。新增列只需在末尾追加一行。
	// created_at 的默认值是非常量，不能走通用 ALTER 路径，单独迁移
	if err := ensureRequestLogCreatedAt(db); err != nil {
		return err
	}

	migrations := []struct {
		column     string
		definition string
	}{
		{"is_stream", "INTEGER DEFAULT 0"},
		{"duration_sec", "REAL DEFAULT 0"},
		{"ephemeral_5m_tokens", "INTEGER DEFAULT 0"},
		{"ephemeral_1h_tokens", "INTEGER DEFAULT 0"},
		{"service_tier", "TEXT DEFAULT ''"},
		{"request_headers", "TEXT DEFAULT ''"},
		{"request_body", "TEXT DEFAULT ''"},
		{"body_truncated", "INTEGER DEFAULT 0"},
		{"body_bytes", "INTEGER DEFAULT 0"},
		{"capture_session_id", "INTEGER DEFAULT 0"},
	}
	for _, m := range migrations {
		if err := ensureRequestLogColumn(db, m.column, m.definition); err != nil {
			return err
		}
	}

	// 抓包会话表与索引（依赖 capture_session_id 列已就位）
	return ensureCaptureSessionTable(db)
}

// protocolConvertHook 协议转换 Hook：将 OpenAI SSE 转换为 Anthropic SSE，并提取 usage
// 注意：xrequest 的 hook 是逐行回调（每次收到一行 SSE 数据）
func protocolConvertHook(converter *OpenAIToAnthropicSSEConverter, kind string, usage *ReqeustLog) func(data []byte) (bool, []byte) {
	return func(data []byte) (bool, []byte) {
		// xrequest 逐行回调，直接传给 ProcessLine
		line := string(data)
		converted := converter.ProcessLine(line)

		// 如果没有输出，返回 flush=false 丢弃该行（避免写出空行）
		if converted == "" {
			return false, nil
		}

		// 从转换后的 Anthropic SSE 中提取 usage（使用现有解析器）
		parseEventPayload(converted, ClaudeCodeParseTokenUsageFromResponse, usage)

		// 返回转换后的数据
		return true, []byte(converted)
	}
}

func ReqeustLogHook(c *gin.Context, kind string, usage *ReqeustLog) func(data []byte) (bool, []byte) { // SSE 钩子：累计字节和解析 token 用量
	return func(data []byte) (bool, []byte) {
		payload := strings.TrimSpace(string(data))

		parserFn := ClaudeCodeParseTokenUsageFromResponse
		switch kind {
		case "codex":
			parserFn = CodexParseTokenUsageFromResponse
		case "gemini":
			parserFn = GeminiParseTokenUsageFromResponse
		}
		parseEventPayload(payload, parserFn, usage)

		return true, data
	}
}

func parseEventPayload(payload string, parser func(string, *ReqeustLog), usage *ReqeustLog) {
	hasData := false
	for _, line := range strings.Split(payload, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data:") {
			hasData = true
			// SSE 规范允许 "data:xxx" 和 "data: xxx",统一剥掉 "data:" 再 trim 空格
			parser(strings.TrimSpace(strings.TrimPrefix(line, "data:")), usage)
		}
	}
	// 非流式响应(无 data: 前缀)直接把 payload 当完整 JSON 喂给 parser
	if !hasData {
		if body := strings.TrimSpace(payload); body != "" {
			parser(body, usage)
		}
	}
}

type ReqeustLog struct {
	ID                int64  `json:"id"`
	Platform          string `json:"platform"` // claude、codex 或 gemini
	Model             string `json:"model"`
	Provider          string `json:"provider"` // provider name
	HttpCode          int    `json:"http_code"`
	InputTokens       int    `json:"input_tokens"`
	OutputTokens      int    `json:"output_tokens"`
	CacheCreateTokens int    `json:"cache_create_tokens"`
	// Ephemeral5mTokens/Ephemeral1hTokens 分别对应 cache_creation.ephemeral_5m/1h_input_tokens。
	// 为 0 时按 CacheCreateTokens 全量当 5m 计费(旧数据兼容)。
	Ephemeral5mTokens int     `json:"ephemeral_5m_tokens"`
	Ephemeral1hTokens int     `json:"ephemeral_1h_tokens"`
	CacheReadTokens   int     `json:"cache_read_tokens"`
	ReasoningTokens   int     `json:"reasoning_tokens"`
	IsStream          bool    `json:"is_stream"`
	DurationSec       float64 `json:"duration_sec"`
	CreatedAt         string  `json:"created_at"`
	// ServiceTier 上游实际分配的档位(default/priority/flex 等),空=未区分。
	ServiceTier     string  `json:"service_tier"`
	InputCost       float64 `json:"input_cost"`
	OutputCost      float64 `json:"output_cost"`
	ReasoningCost   float64 `json:"reasoning_cost"`
	CacheCreateCost float64 `json:"cache_create_cost"`
	CacheReadCost   float64 `json:"cache_read_cost"`
	Ephemeral5mCost float64 `json:"ephemeral_5m_cost"`
	Ephemeral1hCost float64 `json:"ephemeral_1h_cost"`
	TotalCost       float64 `json:"total_cost"`
	HasPricing      bool    `json:"has_pricing"`
	// HasCapture 列表查询计算列：该行是否录有抓包数据（前端据此显示"查看详情"）
	HasCapture bool `json:"has_capture"`

	// ========== 抓包字段（issue #5）==========
	// 列表接口不返回大字段（json:"-"），详情走 RequestLogDetail DTO
	RequestHeaders string `json:"-"`
	RequestBody    string `json:"-"`
	BodyTruncated  bool   `json:"-"`
	BodyBytes      int    `json:"-"`
	// CaptureSessionID 所属抓包会话（0=非会话行/旧数据），见 capturesession.go
	CaptureSessionID int64 `json:"-"`
	// captureGen 采集时的清除代次（stripStaleCapture 用，不落库不序列化）
	captureGen int64
}

// claude code usage parser
// 覆盖三种场景:
//  1. SSE message_start.message.usage (input/cache 一次性)
//  2. SSE message_delta.usage (output/cache cumulative,按事件累积上报同一请求的最终值)
//  3. 非流式根级 usage (单次完整 snapshot)
//
// 对每个字段取 max,既兼容 message_delta 的累计语义,也兼容多事件重复出现的字段,避免重复计费。
// 参考 https://docs.anthropic.com/en/api/messages-streaming
func ClaudeCodeParseTokenUsageFromResponse(data string, usage *ReqeustLog) {
	collectAnthropicUsage(data, "message.usage", usage)
	collectAnthropicUsage(data, "usage", usage)
	clampCacheEphemerals(usage)
}

// collectAnthropicUsage 从指定前缀(message.usage 或 usage)提取 Anthropic 字段,取 max 避免 += 累计导致的翻倍。
func collectAnthropicUsage(data, prefix string, usage *ReqeustLog) {
	maxIntInto(&usage.InputTokens, int(gjson.Get(data, prefix+".input_tokens").Int()))
	maxIntInto(&usage.OutputTokens, int(gjson.Get(data, prefix+".output_tokens").Int()))
	maxIntInto(&usage.CacheCreateTokens, int(gjson.Get(data, prefix+".cache_creation_input_tokens").Int()))
	maxIntInto(&usage.CacheReadTokens, int(gjson.Get(data, prefix+".cache_read_input_tokens").Int()))
	maxIntInto(&usage.Ephemeral5mTokens, int(gjson.Get(data, prefix+".cache_creation.ephemeral_5m_input_tokens").Int()))
	maxIntInto(&usage.Ephemeral1hTokens, int(gjson.Get(data, prefix+".cache_creation.ephemeral_1h_input_tokens").Int()))
	if rawTier := gjson.Get(data, prefix+".service_tier").String(); strings.TrimSpace(rawTier) != "" {
		usage.ServiceTier = string(modelpricing.NormalizeObservedServiceTier(rawTier, warnUnknownTier))
	}
}

// maxIntInto 把 candidate 大于 *dst 时写回,用于流式 cumulative 字段合并。
func maxIntInto(dst *int, candidate int) {
	if candidate > *dst {
		*dst = candidate
	}
}

// codex usage parser(OpenAI Responses API)
func CodexParseTokenUsageFromResponse(data string, usage *ReqeustLog) {
	// 流式事件把 Response 包在 response 字段里(response.completed);
	// 非流式 /responses 直接返回 Response 对象,usage 在根级——两处都要看,
	// 否则非流式请求的 token 与成本全部记 0。
	usageResult := gjson.Get(data, "response.usage")
	if !usageResult.Exists() {
		usageResult = gjson.Get(data, "usage")
	}
	if usageResult.Exists() {
		inputTokens := int(usageResult.Get("input_tokens").Int())
		outputTokens := int(usageResult.Get("output_tokens").Int())
		cacheReadTokens := int(usageResult.Get("input_tokens_details.cached_tokens").Int())
		reasoningTokens := int(usageResult.Get("output_tokens_details.reasoning_tokens").Int())
		if cacheReadTokens > inputTokens {
			cacheReadTokens = inputTokens
		}
		if reasoningTokens > outputTokens {
			reasoningTokens = outputTokens
		}
		// Responses usage.input_tokens 含 cached_tokens;下游把两者分开计价,这里先拆成未缓存输入+缓存读取。
		usage.InputTokens = inputTokens - cacheReadTokens
		// output_tokens 已包含 reasoning_tokens,而计费引擎是 OutputCost + ReasoningCost 相加,
		// 不拆开会把推理 token 计两次。
		usage.OutputTokens = outputTokens - reasoningTokens
		usage.CacheReadTokens = cacheReadTokens
		usage.ReasoningTokens = reasoningTokens
	}
	// service_tier 可能在 response.service_tier 或 response.usage.service_tier,两路径都尝试
	for _, path := range []string{"response.service_tier", "response.usage.service_tier", "service_tier", "usage.service_tier"} {
		if rawTier := gjson.Get(data, path).String(); strings.TrimSpace(rawTier) != "" {
			usage.ServiceTier = string(modelpricing.NormalizeObservedServiceTier(rawTier, warnUnknownTier))
			break
		}
	}
}

// clampCacheEphemerals 兜底 Anthropic ephemeral 拆分的异常情况:
// 若 5m+1h > total,打印一次警告并截断到 total(保留 5m 优先级,1h 截掉超出部分)。
// 若 split 非零但 total 为 0,把 total 回填为 split 之和,避免 total 被漏传导致 create cost 计 0。
func clampCacheEphemerals(usage *ReqeustLog) {
	if usage == nil {
		return
	}
	split := usage.Ephemeral5mTokens + usage.Ephemeral1hTokens
	if split == 0 {
		return
	}
	if usage.CacheCreateTokens == 0 {
		usage.CacheCreateTokens = split
		return
	}
	if split > usage.CacheCreateTokens {
		fmt.Printf("⚠️  ephemeral split(%d)>total(%d),截断 1h=%d 到可用额度\n",
			split, usage.CacheCreateTokens, usage.Ephemeral1hTokens)
		overflow := split - usage.CacheCreateTokens
		if usage.Ephemeral1hTokens >= overflow {
			usage.Ephemeral1hTokens -= overflow
			return
		}
		// 1h 截到 0 还不够,再从 5m 截剩余
		remaining := overflow - usage.Ephemeral1hTokens
		usage.Ephemeral1hTokens = 0
		if usage.Ephemeral5mTokens >= remaining {
			usage.Ephemeral5mTokens -= remaining
		} else {
			usage.Ephemeral5mTokens = 0
		}
	}
}

// gemini usage parser (流式响应专用)
// Gemini SSE 流中每个 chunk 都会携带完整的 usageMetadata，需取最大值而非累加
func GeminiParseTokenUsageFromResponse(data string, usage *ReqeustLog) {
	usageResult := gjson.Get(data, "usageMetadata")
	if !usageResult.Exists() {
		return
	}
	mergeGeminiUsageMetadata(usageResult, usage)
}

// mergeGeminiUsageMetadata 合并 Gemini usageMetadata 到 ReqeustLog（取最大值去重）
// Gemini 流式响应特点：每个 chunk 包含截止当前的累计用量，因此取最大值即可
func mergeGeminiUsageMetadata(usage gjson.Result, reqLog *ReqeustLog) {
	if !usage.Exists() || reqLog == nil {
		return
	}

	promptTokens := int(usage.Get("promptTokenCount").Int())
	if usage.Get("promptTokenCount").Exists() || usage.Get("cachedContentTokenCount").Exists() {
		cacheReadTokens := int(usage.Get("cachedContentTokenCount").Int())
		if cacheReadTokens > promptTokens {
			cacheReadTokens = promptTokens
		}
		reqLog.InputTokens = promptTokens - cacheReadTokens
		reqLog.CacheReadTokens = cacheReadTokens
	}
	if v := usage.Get("candidatesTokenCount"); v.Exists() {
		reqLog.OutputTokens = int(v.Int())
	}
	// thinking/reasoning tokens (thoughtsTokenCount)
	// 参考: https://ai.google.dev/gemini-api/docs/thinking
	if v := usage.Get("thoughtsTokenCount"); v.Exists() {
		reqLog.ReasoningTokens = int(v.Int())
	}

	// 若仅提供 totalTokenCount，按 total - input 估算输出 token。
	// totalTokenCount 含 thoughtsTokenCount，直接相减会把思考 token 也算进输出，
	// 与单独入库的 ReasoningTokens 重复计费，因此要先扣掉。
	total := usage.Get("totalTokenCount").Int()
	if total > 0 && reqLog.OutputTokens == 0 && promptTokens > 0 && promptTokens < int(total) {
		if derived := int(total) - promptTokens - reqLog.ReasoningTokens; derived > 0 {
			reqLog.OutputTokens = derived
		}
	}
}

// geminiClientAbortMsg 标记 Gemini 转发中"客户端主动断开"的错误信息,
// 调用方据此跳过 RecordFailure(与 Claude/Codex 的 errClientAbort 语义对齐)。
const geminiClientAbortMsg = "client aborted"

// geminiClientErrorPrefix 标记 Gemini 转发中"上游判定请求内容本身有问题"的错误信息。
// Gemini 转发返回的是字符串而不是 error，用前缀传递该分类：
// 调用方据此跳过 RecordFailure 与同供应商重试（与 errUpstreamClientError 语义对齐）。
const geminiClientErrorPrefix = "client request rejected: "

// isGeminiClientError 判断 Gemini 转发的错误信息是否属于客户端请求问题。
func isGeminiClientError(errMsg string) bool {
	return strings.HasPrefix(errMsg, geminiClientErrorPrefix)
}

// streamGeminiResponseWithHook 流式传输 Gemini 响应并通过 Hook 提取 token 用量
// 【修复】维护跨 chunk 缓冲，确保完整 SSE 事件解析
// Gemini SSE 格式: "data: {json}\n\n" 或 "data: [DONE]\n\n"
func streamGeminiResponseWithHook(body io.Reader, writer io.Writer, requestLog *ReqeustLog) error {
	buf := make([]byte, 8192)   // 增大缓冲区减少系统调用
	var lineBuf strings.Builder // 跨 chunk 行缓冲

	for {
		n, err := body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			// 写入客户端（优先保证数据传输）；写客户端失败=客户端主动断开,
			// 用 errClientAbort 标记,避免被当作供应商故障计入拉黑
			if _, writeErr := writer.Write(chunk); writeErr != nil {
				return fmt.Errorf("%w: %v", errClientAbort, writeErr)
			}
			// 如果是 http.Flusher，立即刷新
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
			// 解析 SSE 数据提取 token 用量（使用缓冲处理跨 chunk 情况）
			parseGeminiSSEWithBuffer(string(chunk), &lineBuf, requestLog)
		}
		if err != nil {
			// 处理缓冲区残留数据
			if lineBuf.Len() > 0 {
				parseGeminiSSELine(lineBuf.String(), requestLog)
				lineBuf.Reset()
			}
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// parseGeminiSSEWithBuffer 使用缓冲处理跨 chunk 的 SSE 事件
// 【修复】解决 JSON 被 TCP 分割到多个 chunk 导致解析失败的问题
func parseGeminiSSEWithBuffer(chunk string, lineBuf *strings.Builder, requestLog *ReqeustLog) {
	// 将当前 chunk 追加到缓冲
	lineBuf.WriteString(chunk)
	content := lineBuf.String()

	// 按双换行符分割完整的 SSE 事件
	// SSE 格式: "data: {...}\n\n" 或 "data: {...}\r\n\r\n"
	for {
		// 查找事件分隔符（双换行）
		idx := strings.Index(content, "\n\n")
		if idx == -1 {
			// 尝试 \r\n\r\n 分隔符
			idx = strings.Index(content, "\r\n\r\n")
			if idx == -1 {
				break // 没有完整事件，等待更多数据
			}
			idx += 4 // \r\n\r\n 长度
		} else {
			idx += 2 // \n\n 长度
		}

		// 提取完整事件
		event := content[:idx]
		content = content[idx:]

		// 解析事件中的 data 行
		parseGeminiSSELine(event, requestLog)
	}

	// 更新缓冲区为未处理的残留数据
	lineBuf.Reset()
	lineBuf.WriteString(content)
}

// parseGeminiSSELine 解析单个 SSE 事件提取 usageMetadata
// 【优化】只在包含 usageMetadata 时才调用 gjson 解析
func parseGeminiSSELine(event string, requestLog *ReqeustLog) {
	lines := strings.Split(event, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" || data == "" {
			continue
		}
		// 【优化】快速检查是否包含 usageMetadata，避免无效解析
		if !strings.Contains(data, "usageMetadata") {
			continue
		}
		GeminiParseTokenUsageFromResponse(data, requestLog)
	}
}

// ReplaceModelInRequestBody 替换请求体中的模型名
// 使用 gjson + sjson 实现高性能 JSON 操作，避免完整反序列化
func ReplaceModelInRequestBody(bodyBytes []byte, newModel string) ([]byte, error) {
	// 检查请求体中是否存在 model 字段
	result := gjson.GetBytes(bodyBytes, "model")
	if !result.Exists() {
		return bodyBytes, fmt.Errorf("请求体中未找到 model 字段")
	}

	// 使用 sjson.SetBytes 替换模型名（高性能操作）
	modified, err := sjson.SetBytes(bodyBytes, "model", newModel)
	if err != nil {
		return bodyBytes, fmt.Errorf("替换模型名失败: %w", err)
	}

	return modified, nil
}

// geminiProxyHandler 处理 Gemini API 请求（支持 Level 分组降级和黑名单）
func (prs *ProviderRelayService) geminiProxyHandler(apiVersion string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 获取完整路径（例如 /v1beta/models/gemini-2.5-pro:generateContent）
		fullPath := c.Param("any")
		endpoint := apiVersion + fullPath

		// 保留查询参数（如 ?alt=sse），但必须剔除客户端自带的凭据参数：
		// Gemini REST 支持 ?key=<API Key>，原样转发会把用户本机的真实 Key 发给
		// 降级链上每一个第三方供应商，上游还可能优先用它认证计费。
		// 供应商凭据统一走 x-goog-api-key 请求头注入。
		query := stripCredentialQueryParams(c.Request.URL.RawQuery)
		if query != "" {
			endpoint = endpoint + "?" + query
		}

		// 查询串里可能带 key=<API Key>，日志要脱敏后再打印
		fmt.Printf("[Gemini] 收到请求: %s\n", maskSensitiveQuery(endpoint))

		// 读取请求体
		var bodyBytes []byte
		if c.Request.Body != nil {
			data, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
				return
			}
			bodyBytes = data
			c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		// 判断是否为流式请求
		isStream := strings.Contains(endpoint, ":streamGenerateContent") || strings.Contains(query, "alt=sse")

		// 从 endpoint 提取请求模型名（Gemini 的模型在 URL 路径而非请求体中）
		requestedModel := extractGeminiModelFromEndpoint(endpoint)

		// 加载 Gemini providers（与配置代数配对）
		providers, geminiGen := prs.geminiService.providersWithGen()
		if len(providers) == 0 {
			respondNoEligibleProviders(c, requestedModel, 0, 0, 0)
			return
		}

		// 1. 过滤可用的 providers（启用 + BaseURL 配置 + 配置合法 + 支持请求模型 + 未被拉黑）
		var activeProviders []GeminiProvider
		skippedModel, skippedBlacklist, skippedInvalid := 0, 0, 0
		for _, p := range providers {
			if !p.Enabled || p.BaseURL == "" {
				continue
			}
			// 配置验证：失败自动跳过（与 Claude/Codex 行为一致）
			if errs := p.ValidateConfiguration(); len(errs) > 0 {
				fmt.Printf("[Gemini] ⚠️ Provider %s 配置验证失败，已自动跳过: %v\n", p.Name, errs)
				skippedInvalid++
				continue
			}
			if requestedModel != "" {
				// 模型白名单过滤：不支持请求模型的 provider 直接跳过
				if !p.IsModelSupported(requestedModel) {
					fmt.Printf("[Gemini] ℹ️ Provider %s 不支持模型 %s，已跳过\n", p.Name, requestedModel)
					skippedModel++
					continue
				}
				// 白名单非空时，最终转发的 effective model 必须仍在白名单内。
				// 不能只在"映射改变了模型名"时才查——恒等通配符映射
				// （gemini-* -> gemini-*）会让白名单外的模型原样通过初筛
				if len(p.SupportedModels) > 0 {
					if effective := p.GetEffectiveModel(requestedModel); !modelInWhitelist(p.SupportedModels, effective) {
						fmt.Printf("[Gemini] ⚠️ Provider %s 映射结果 %s 不在白名单中，已跳过\n", p.Name, effective)
						skippedModel++
						continue
					}
				}
			}
			// 检查黑名单
			if isBlacklisted, until := prs.blacklistService.IsBlacklisted("gemini", p.Name); isBlacklisted {
				fmt.Printf("[Gemini] ⛔ Provider %s 已拉黑，过期时间: %v\n", p.Name, until.Format("15:04:05"))
				skippedBlacklist++
				continue
			}
			// Level 默认值处理
			if p.Level <= 0 {
				p.Level = 1
			}
			activeProviders = append(activeProviders, p)
		}

		if len(activeProviders) == 0 {
			respondNoEligibleProviders(c, requestedModel, skippedModel, skippedBlacklist, skippedInvalid)
			return
		}

		// 2. 按 Level 分组
		levelGroups := make(map[int][]GeminiProvider)
		for _, p := range activeProviders {
			levelGroups[p.Level] = append(levelGroups[p.Level], p)
		}

		// 获取排序后的 Level 列表
		var sortedLevels []int
		for level := range levelGroups {
			sortedLevels = append(sortedLevels, level)
		}
		sort.Ints(sortedLevels)

		fmt.Printf("[Gemini] 共 %d 个 Level 分组: %v\n", len(sortedLevels), sortedLevels)

		// 请求日志
		requestLog := &ReqeustLog{
			Platform:     "gemini",
			IsStream:     isStream,
			InputTokens:  0,
			OutputTokens: 0,
		}
		start := time.Now()

		// 保存日志的 defer
		defer func() {
			// Provider 为空说明没有任何一次真实转发（如纯并发忙），
			// 不落一条 Provider=""、HttpCode=0 的无效记录
			if requestLog.Provider == "" {
				return
			}
			requestLog.DurationSec = time.Since(start).Seconds()
			// 若请求过程中发生 rename,把旧名兑换成新名再落库
			requestLog.Provider = ResolveProviderAlias(requestLog.Platform, requestLog.Provider)
			// 读锁覆盖"代次校验 + 提交"全程,与清除的写锁互斥,堵死校验后提交前的清除窗口
			prs.captureWriteMu.RLock()
			defer prs.captureWriteMu.RUnlock()
			prs.stripStaleCapture(requestLog)
			if err := prs.writeRequestLog(requestLog); err != nil {
				fmt.Printf("[Gemini] 写入 request_log 失败: %v\n", err)
			}
		}()

		// 获取拉黑功能开关状态
		blacklistEnabled := prs.blacklistService.ShouldUseFixedMode()

		// 【拉黑模式】：同 Provider 重试直到被拉黑，然后切换到下一个 Provider
		if blacklistEnabled {
			// 缓存轮询设置（单次请求级别，避免重复读取配置文件）
			roundRobinSettingEnabled := prs.isRoundRobinSettingEnabled()
			if roundRobinSettingEnabled {
				fmt.Printf("[Gemini] 🔒 拉黑模式 + 轮询负载均衡\n")
			} else {
				fmt.Printf("[Gemini] 🔒 拉黑模式（顺序调度）\n")
			}

			// 获取重试配置
			retryConfig := prs.blacklistService.GetRetryConfig()
			maxRetryPerProvider := retryConfig.FailureThreshold
			retryWaitSeconds := retryConfig.RetryWaitSeconds
			fmt.Printf("[Gemini] 重试配置: 每 Provider 最多 %d 次重试，间隔 %d 秒\n",
				maxRetryPerProvider, retryWaitSeconds)

			var lastError string
			// 只要有过一次真正的供应商故障，终态就必须维持 502 让 SDK 退避重试
			sawNonClientError := false
			var lastProvider string
			totalAttempts := 0

			busyWaitDeadline := time.Time{}
			enteredBusyWait := false
			defer func() {
				if enteredBusyWait {
					prs.concurrency.leaveWaitPhase()
				}
			}()
			busySkipped := 0
			// 已实际尝试过的供应商：等待阶段重扫不再碰它（失败已计、重试预算不重置）
			attemptedProviders := map[string]bool{}
			// 因并发满被跳过、尚未真实尝试的候选
			busyPending := map[string]concurrencyBusyRef{}
			for {
				busySkipped = 0
				// 每 pass 重建：上一轮候选可能已被拉黑或删除，残留会让容量门控恒真
				busyPending = map[string]concurrencyBusyRef{}
				// 遍历所有 Level 和 Provider
				for _, level := range sortedLevels {
					providersInLevel := levelGroups[level]

					// 如果启用轮询，对同 Level 的 providers 进行轮询排序
					if roundRobinSettingEnabled {
						providersInLevel = prs.roundRobinOrderGemini(level, providersInLevel)
					}

					fmt.Printf("[Gemini] === 尝试 Level %d（%d 个 provider）===\n", level, len(providersInLevel))

					for _, provider := range providersInLevel {
						if attemptedProviders[provider.ID] {
							continue
						}
						// 检查是否已被拉黑（跳过已拉黑的 provider）
						if blacklisted, until := prs.blacklistService.IsBlacklisted("gemini", provider.Name); blacklisted {
							fmt.Printf("[Gemini] ⏭️ 跳过已拉黑的 Provider: %s (解禁时间: %v)\n", provider.Name, until)
							continue
						}

						// 模型映射：Gemini 的模型在 URL 路径里，映射即按 provider 重写路径段。
						// 必须用局部变量，绝不能改写外层 endpoint——否则 A 失败降级后
						// B 会拿到 A 的映射结果
						providerEndpoint := endpoint
						if requestedModel != "" {
							if effectiveModel := provider.GetEffectiveModel(requestedModel); effectiveModel != requestedModel {
								providerEndpoint = rewriteGeminiModelInEndpoint(endpoint, requestedModel, effectiveModel)
								fmt.Printf("[Gemini] Provider %s 映射模型: %s -> %s\n", provider.Name, requestedModel, effectiveModel)
							}
						}

						// 同 Provider 内重试循环
						for retryCount := 0; retryCount < maxRetryPerProvider; retryCount++ {
							// 再次检查是否已被拉黑（重试过程中可能被拉黑）。
							// 必须在占用配额之前检查：占用后 break 会永久泄漏配额
							if blacklisted, _ := prs.blacklistService.IsBlacklisted("gemini", provider.Name); blacklisted {
								fmt.Printf("[Gemini] 🚫 Provider %s 已被拉黑，切换到下一个\n", provider.Name)
								break
							}

							// 并发配额：每次尝试独立占用，重试间隙让出；
							// 满载不算尝试、不计失败，换下一个供应商
							if !prs.concurrency.TryAcquire("gemini", provider.ID, provider.MaxConcurrency, geminiGen) {
								fmt.Printf("[Gemini] Provider %s 并发已满，跳过\n", provider.Name)
								// 已真实失败过的供应商重试遇忙不再进等待候选：
								// 下一 pass 必然跳过它，等它只会把失败聚合错改成 503
								if !attemptedProviders[provider.ID] {
									busySkipped++
									busyPending[provider.ID] = concurrencyBusyRef{Key: provider.ID, Limit: provider.MaxConcurrency, Gen: geminiGen}
								}
								break
							}
							totalAttempts++

							// 预填日志：成功占用配额后才把本次请求归属到该供应商，
							// 忙跳过的供应商不得留名在请求日志里
							requestLog.Provider = provider.Name
							requestLog.Model = provider.Model

							fmt.Printf("[Gemini] [拉黑模式] Provider: %s (Level %d) | 重试 %d/%d\n",
								provider.Name, level, retryCount+1, maxRetryPerProvider)

							ok, errMsg, responseWritten := prs.forwardGeminiRequest(c, &provider, providerEndpoint, bodyBytes, isStream, requestLog)
							prs.concurrency.Release("gemini", provider.ID)
							// 实际尝试过：等待阶段重扫不再碰它
							attemptedProviders[provider.ID] = true
							delete(busyPending, provider.ID)
							if ok {
								fmt.Printf("[Gemini] ✓ 成功: %s | 重试 %d 次\n", provider.Name, retryCount+1)
								_ = prs.blacklistService.RecordSuccess("gemini", provider.Name)
								prs.setLastUsedProvider("gemini", provider.Name)
								return
							}

							// 【关键修复】如果响应已写入客户端，不能重试或降级，直接返回
							if responseWritten {
								if errMsg == geminiClientAbortMsg {
									fmt.Printf("[Gemini] ℹ️ 客户端中断: %s | 不计入供应商失败\n", provider.Name)
									return
								}
								fmt.Printf("[Gemini] ⚠️ 响应已部分写入，无法重试: %s | 错误: %s\n", provider.Name, errMsg)
								_ = prs.blacklistService.RecordFailure("gemini", provider.Name)
								return
							}

							// 客户端已取消:停止全部尝试,不计供应商失败
							if errMsg == geminiClientAbortMsg {
								fmt.Printf("[Gemini] ℹ️ 客户端取消请求,停止尝试\n")
								return
							}

							// 失败处理
							lastError = errMsg
							lastProvider = provider.Name

							fmt.Printf("[Gemini] ✗ 失败: %s | 重试 %d/%d | 错误: %s\n",
								provider.Name, retryCount+1, maxRetryPerProvider, errMsg)

							// 上游判定请求内容本身有问题：不计供应商失败，也别拿同一个坏请求重试，
							// 直接换下一个供应商
							if isGeminiClientError(errMsg) {
								fmt.Printf("[Gemini] 上游拒绝请求内容，不计供应商失败，切换到下一个\n")
								break
							}

							sawNonClientError = true

							// 记录失败次数（可能触发拉黑）
							_ = prs.blacklistService.RecordFailure("gemini", provider.Name)

							// 检查是否刚被拉黑
							if blacklisted, _ := prs.blacklistService.IsBlacklisted("gemini", provider.Name); blacklisted {
								fmt.Printf("[Gemini] 🚫 Provider %s 达到失败阈值，已被拉黑，切换到下一个\n", provider.Name)
								break
							}

							// 等待后重试（除非是最后一次）；等待期间客户端可能已经离开
							if retryCount < maxRetryPerProvider-1 {
								fmt.Printf("[Gemini] ⏳ 等待 %d 秒后重试...\n", retryWaitSeconds)
								select {
								case <-time.After(time.Duration(retryWaitSeconds) * time.Second):
								case <-c.Request.Context().Done():
									fmt.Printf("[Gemini] 等待重试期间客户端已断开，停止尝试\n")
									return
								}
							}
						}
					}
				}

				// 一整遍下来只要还有因并发满被跳过的供应商，就进入有界等待
				if busySkipped == 0 {
					break
				}
				if busyWaitDeadline.IsZero() {
					busyWaitDeadline = time.Now().Add(prs.concurrency.waitBudget)
					if !prs.concurrency.enterWaitPhase() {
						respondAllBusy(c, "gemini")
						return
					}
					enteredBusyWait = true
				}
				// 唤醒以"忙候选真的有空位"为门控：本轮实际尝试供应商的正常释放
				// 也会触发全局信号，不加门控直接重扫会形成自唤醒重试风暴
				woke := false
				for {
					capSignal := prs.concurrency.releaseSignal()
					if prs.concurrency.anyCapacity("gemini", busyPending) {
						woke = true
						break
					}
					if !prs.concurrency.waitForRelease(c.Request.Context(), busyWaitDeadline, capSignal) {
						break
					}
				}
				if !woke {
					respondAllBusy(c, "gemini")
					return
				}
				// 容量门控可能被"释放后立刻又被占走"的候选反复触发，
				// 重扫前硬校验总预算与客户端 context，防止空转越过 deadline
				if c.Request.Context().Err() != nil || time.Now().After(busyWaitDeadline) {
					respondAllBusy(c, "gemini")
					return
				}
				fmt.Printf("[Gemini] 并发配额有释放，重扫供应商\n")
			}

			// 所有 Provider 都失败或被拉黑
			fmt.Printf("[Gemini] 💥 拉黑模式：所有 Provider 都失败或被拉黑（共尝试 %d 次）\n", totalAttempts)

			// 请求内容被拒时必须回 4xx：回 502 会让 SDK 按服务端故障自动重试，
			// 一个不可能成功的坏请求被反复重发，每次都扫一遍全部供应商
			terminalStatus := http.StatusBadGateway
			if !sawNonClientError && isGeminiClientError(lastError) {
				terminalStatus = http.StatusBadRequest
			}
			if requestLog.HttpCode == 0 {
				requestLog.HttpCode = terminalStatus
			}
			c.JSON(terminalStatus, gin.H{
				"error":         fmt.Sprintf("所有 Provider 都失败或被拉黑，最后尝试: %s - %s", lastProvider, lastError),
				"lastProvider":  lastProvider,
				"totalAttempts": totalAttempts,
				"mode":          "blacklist_retry",
				"hint":          "拉黑模式已开启，同 Provider 重试到拉黑再切换。如需立即降级请关闭拉黑功能",
			})
			return
		}

		// 【降级模式】：按 Level 顺序尝试所有 provider
		roundRobinEnabled := prs.isRoundRobinEnabled()
		if roundRobinEnabled {
			fmt.Printf("[Gemini] 🔄 降级模式 + 轮询负载均衡\n")
		} else {
			fmt.Printf("[Gemini] 🔄 降级模式（顺序降级）\n")
		}

		var lastError string
		// 只要有过一次真正的供应商故障，终态就必须维持 502 让 SDK 退避重试
		sawNonClientError := false
		busyWaitDeadline := time.Time{}
		enteredBusyWait := false
		defer func() {
			if enteredBusyWait {
				prs.concurrency.leaveWaitPhase()
			}
		}()
		busySkipped := 0
		// 已实际尝试过的供应商：等待阶段重扫不再碰它（失败已计、重试预算不重置）
		attemptedProviders := map[string]bool{}
		// 因并发满被跳过、尚未真实尝试的候选
		busyPending := map[string]concurrencyBusyRef{}
		for {
			busySkipped = 0
			// 每 pass 重建：上一轮候选可能已被拉黑或删除，残留会让容量门控恒真
			busyPending = map[string]concurrencyBusyRef{}
			for _, level := range sortedLevels {
				providersInLevel := levelGroups[level]

				// 如果启用轮询，对同 Level 的 providers 进行轮询排序
				if roundRobinEnabled {
					providersInLevel = prs.roundRobinOrderGemini(level, providersInLevel)
				}

				fmt.Printf("[Gemini] === 尝试 Level %d（%d 个 provider）===\n", level, len(providersInLevel))

				for idx, provider := range providersInLevel {
					if attemptedProviders[provider.ID] {
						continue
					}
					fmt.Printf("[Gemini]   [%d/%d] Provider: %s\n", idx+1, len(providersInLevel), provider.Name)

					// 并发配额：满载不算尝试、不计失败，换下一个供应商
					if !prs.concurrency.TryAcquire("gemini", provider.ID, provider.MaxConcurrency, geminiGen) {
						fmt.Printf("[Gemini] Provider %s 并发已满，跳过\n", provider.Name)
						busySkipped++
						busyPending[provider.ID] = concurrencyBusyRef{Key: provider.ID, Limit: provider.MaxConcurrency, Gen: geminiGen}
						continue
					}

					// 预填日志：成功占用配额后才归属到该供应商，失败也能落库
					requestLog.Provider = provider.Name
					requestLog.Model = provider.Model

					// 模型映射：按 provider 用局部变量重写路径段，绝不改写外层 endpoint
					providerEndpoint := endpoint
					if requestedModel != "" {
						if effectiveModel := provider.GetEffectiveModel(requestedModel); effectiveModel != requestedModel {
							providerEndpoint = rewriteGeminiModelInEndpoint(endpoint, requestedModel, effectiveModel)
							fmt.Printf("[Gemini] Provider %s 映射模型: %s -> %s\n", provider.Name, requestedModel, effectiveModel)
						}
					}

					ok, errMsg, responseWritten := prs.forwardGeminiRequest(c, &provider, providerEndpoint, bodyBytes, isStream, requestLog)
					prs.concurrency.Release("gemini", provider.ID)
					// 实际尝试过：等待阶段重扫不再碰它
					attemptedProviders[provider.ID] = true
					delete(busyPending, provider.ID)
					if ok {
						_ = prs.blacklistService.RecordSuccess("gemini", provider.Name)
						// 记录最后使用的供应商
						prs.setLastUsedProvider("gemini", provider.Name)
						fmt.Printf("[Gemini] ✓ 请求完成 | Provider: %s | 总耗时: %.2fs\n", provider.Name, time.Since(start).Seconds())
						return // 成功，退出
					}

					// 【关键修复】如果响应已写入客户端，不能降级到其他 provider，直接返回
					if responseWritten {
						if errMsg == geminiClientAbortMsg {
							fmt.Printf("[Gemini] ℹ️ 客户端中断: %s | 不计入供应商失败\n", provider.Name)
							return
						}
						fmt.Printf("[Gemini] ⚠️ 响应已部分写入，无法降级: %s | 错误: %s\n", provider.Name, errMsg)
						_ = prs.blacklistService.RecordFailure("gemini", provider.Name)
						return
					}

					// 客户端已取消:停止全部尝试,不计供应商失败
					if errMsg == geminiClientAbortMsg {
						fmt.Printf("[Gemini] ℹ️ 客户端取消请求,停止尝试\n")
						return
					}

					// 失败，记录并继续
					lastError = errMsg
					// 上游判定请求内容本身有问题：换供应商也一样，不计供应商失败
					if isGeminiClientError(errMsg) {
						fmt.Printf("[Gemini] 上游拒绝请求内容，不计供应商失败\n")
						continue
					}
					sawNonClientError = true
					_ = prs.blacklistService.RecordFailure("gemini", provider.Name)
				}

				fmt.Printf("[Gemini] Level %d 的所有 %d 个 provider 均失败，尝试下一 Level\n", level, len(providersInLevel))
			}

			// 一整遍下来只要还有因并发满被跳过的供应商，就进入有界等待
			if busySkipped == 0 {
				break
			}
			if busyWaitDeadline.IsZero() {
				busyWaitDeadline = time.Now().Add(prs.concurrency.waitBudget)
				if !prs.concurrency.enterWaitPhase() {
					respondAllBusy(c, "gemini")
					return
				}
				enteredBusyWait = true
			}
			// 唤醒以"忙候选真的有空位"为门控：本轮实际尝试供应商的正常释放
			// 也会触发全局信号，不加门控直接重扫会形成自唤醒重试风暴
			woke := false
			for {
				capSignal := prs.concurrency.releaseSignal()
				if prs.concurrency.anyCapacity("gemini", busyPending) {
					woke = true
					break
				}
				if !prs.concurrency.waitForRelease(c.Request.Context(), busyWaitDeadline, capSignal) {
					break
				}
			}
			if !woke {
				respondAllBusy(c, "gemini")
				return
			}
			// 容量门控可能被"释放后立刻又被占走"的候选反复触发，
			// 重扫前硬校验总预算与客户端 context，防止空转越过 deadline
			if c.Request.Context().Err() != nil || time.Now().After(busyWaitDeadline) {
				respondAllBusy(c, "gemini")
				return
			}
			fmt.Printf("[Gemini] 并发配额有释放，重扫供应商\n")
		}

		// 所有 Level 都失败
		terminalStatus := http.StatusBadGateway
		if !sawNonClientError && isGeminiClientError(lastError) {
			terminalStatus = http.StatusBadRequest
		}
		if requestLog.HttpCode == 0 {
			requestLog.HttpCode = terminalStatus
		}
		c.JSON(terminalStatus, gin.H{
			"error":   "all gemini providers failed",
			"details": lastError,
		})
		fmt.Printf("[Gemini] ✗ 所有 provider 均失败 | 最后错误: %s\n", lastError)
	}
}

// geminiModelSpan 定位 endpoint 路径中 "models/<model>" 段里模型名的 [start,end) 下标。
// 未找到返回 (-1,-1)。只认路径段边界（"/models/" 或串首 "models/"），
// 避免 "notmodels/" 之类子串误匹配；查询串里的 "/models/" 不算路径；
// 模型名终止于 ':'、'?'、'#' 或串尾。'/' 不作终止符——模型映射目标
// 可以带斜杠（如 vendor/gemini-x），在 '/' 截断会让请求日志只记到 "vendor"。
func geminiModelSpan(endpoint string) (int, int) {
	pathEnd := len(endpoint)
	if q := strings.IndexByte(endpoint, '?'); q >= 0 {
		pathEnd = q
	}
	path := endpoint[:pathEnd]

	var start int
	if strings.HasPrefix(path, "models/") {
		start = len("models/")
	} else if idx := strings.Index(path, "/models/"); idx >= 0 {
		start = idx + len("/models/")
	} else {
		return -1, -1
	}

	end := start
	for end < pathEnd {
		c := endpoint[end]
		if c == ':' || c == '#' {
			break
		}
		end++
	}
	if end == start {
		return -1, -1
	}
	return start, end
}

// extractGeminiModelFromEndpoint 从 Gemini API endpoint 中提取模型名
// 例如 "/v1beta/models/gemini-2.5-pro:generateContent?alt=sse" -> "gemini-2.5-pro"
func extractGeminiModelFromEndpoint(endpoint string) string {
	start, end := geminiModelSpan(endpoint)
	if start == -1 {
		return ""
	}
	return strings.TrimSpace(endpoint[start:end])
}

// rewriteGeminiModelInEndpoint 把 endpoint 路径中的模型名从 from 替换为 to。
// Gemini 的模型在 URL 路径而非请求体里，模型映射只能改写路径段。
// 仅当 models/ 段正好等于 from 时才替换，否则原样返回（与提取共用同一
// 边界解析，提取成功即重写必然命中同一区间）。
func rewriteGeminiModelInEndpoint(endpoint, from, to string) string {
	if from == "" || to == "" || from == to {
		return endpoint
	}
	start, end := geminiModelSpan(endpoint)
	if start == -1 || endpoint[start:end] != from {
		return endpoint
	}
	return endpoint[:start] + to + endpoint[end:]
}

// forwardGeminiRequest 转发 Gemini 请求到指定 provider
// 返回 (成功, 错误信息, 是否已写入响应)
// 【重要】当 responseWritten=true 时，调用方不得重试或降级，因为响应头/数据已发送给客户端
func (prs *ProviderRelayService) forwardGeminiRequest(
	c *gin.Context,
	provider *GeminiProvider,
	endpoint string,
	bodyBytes []byte,
	isStream bool,
	requestLog *ReqeustLog,
) (success bool, errMsg string, responseWritten bool) {
	providerStart := time.Now()

	// 构建目标 URL
	targetURL := strings.TrimSuffix(provider.BaseURL, "/") + endpoint

	// 预先填充日志，保证失败也能记录 provider 和模型
	requestLog.Provider = provider.Name
	// 【修复】每次尝试开始前重置 HttpCode，避免重试时沿用上一次的状态码
	requestLog.HttpCode = 0
	// 抓包字段同步重置：多次尝试复用同一 requestLog，必须在任何提前返回之前清掉
	// 上一家的残留，否则本家构造请求失败时会落下"新 Provider + 旧请求内容"的错配
	requestLog.RequestHeaders = ""
	requestLog.RequestBody = ""
	requestLog.BodyTruncated = false
	requestLog.BodyBytes = 0
	requestLog.CaptureSessionID = 0
	// 优先从 endpoint 提取模型名（如 gemini-2.5-pro），否则回退到 provider.Model
	if extractedModel := extractGeminiModelFromEndpoint(endpoint); extractedModel != "" {
		requestLog.Model = extractedModel
	} else {
		requestLog.Model = provider.Model
	}

	// 创建 HTTP 请求（绑定客户端 context:客户端取消时立即释放上游连接,
	// 配合 32h 长超时不至于让被放弃的请求占用资源）
	req, err := http.NewRequestWithContext(c.Request.Context(), "POST", targetURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return false, fmt.Sprintf("创建请求失败: %v", err), false
	}

	// 复制请求头
	for key, values := range c.Request.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	// 清掉客户端自带凭据（否则用户本机 Key 会发给第三方供应商）与 Accept-Encoding
	// （透传后 Go 不再自动解压，流式 usageMetadata 解析拿到压缩字节，计费恒为 0）
	for _, name := range []string{
		"Authorization", "Proxy-Authorization", "X-Api-Key", "Api-Key", "X-Goog-Api-Key",
		"Accept-Encoding", "Connection", "Keep-Alive", "Te", "Upgrade",
	} {
		req.Header.Del(name)
	}

	// 设置 API Key
	if provider.APIKey != "" {
		req.Header.Set("x-goog-api-key", provider.APIKey)
	}

	// 抓包模式：字段已在本次尝试开头统一重置，这里按开关采集终态出站请求
	// （x-goog-api-key 注入完成、进入 transport 之前的应用层形态）。
	// 状态一次性快照（读锁内），避免与关闭/清除竞态拼出错位组合
	if enabled, sessionID, gen := prs.captureSnapshot(); enabled {
		requestLog.captureGen = gen
		requestLog.CaptureSessionID = sessionID
		flat := make(map[string]string, len(req.Header))
		for k, vs := range req.Header {
			// 多值头合并保留（transport 会全部发送，丢值会让详情失真）
			if len(vs) > 0 {
				flat[k] = strings.Join(vs, ", ")
			}
		}
		requestLog.RequestHeaders = maskCaptureHeaders(flat, "", provider.APIKey)
		requestLog.RequestBody, requestLog.BodyTruncated, requestLog.BodyBytes = redactCaptureBody(bodyBytes, provider.APIKey)
	}

	// 发送请求（与 Claude/Codex 转发共用连接池，避免每请求新建 Transport；
	// 超时同为 32 小时以适配长推理/长输出任务，提前中止依靠请求 context）
	resp, err := relayClientFor(provider.InsecureSkipVerify, provider.Name).Do(req)
	providerDuration := time.Since(providerStart).Seconds()

	if err != nil {
		// 客户端取消(context 已终止)不是供应商故障:立即止损,不计失败
		if c.Request.Context().Err() != nil {
			fmt.Printf("[Gemini]   ℹ️ 客户端已取消请求: %s | 耗时: %.2fs\n", provider.Name, providerDuration)
			return false, geminiClientAbortMsg, false
		}
		fmt.Printf("[Gemini]   ✗ 失败: %s | 错误: %v | 耗时: %.2fs\n", provider.Name, err, providerDuration)
		return false, fmt.Sprintf("请求失败: %v", err), false
	}
	defer resp.Body.Close()

	// 先记录上游状态码，失败场景也能落库
	requestLog.HttpCode = resp.StatusCode

	// 检查响应状态
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// 上游错误体大小不可控（可能是整页 HTML），限长读取后再进日志与错误串
		errorBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		fmt.Printf("[Gemini]   ✗ 失败: %s | HTTP %d | 耗时: %.2fs\n", provider.Name, resp.StatusCode, providerDuration)
		msg := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, string(errorBody))
		// 请求内容本身被拒（400/413/422 等）：换供应商也一样失败，
		// 加前缀让调用方跳过失败计数，避免一个坏请求把所有 Gemini 供应商拉黑
		if isClientSideUpstreamStatus(resp.StatusCode) {
			msg = geminiClientErrorPrefix + msg
		}
		return false, msg, false
	}

	fmt.Printf("[Gemini]   ✓ 连接成功: %s | HTTP %d | 耗时: %.2fs\n", provider.Name, resp.StatusCode, providerDuration)

	// 处理响应
	if isStream {
		// 流式模式：先写 header 再流式传输
		for key, values := range resp.Header {
			for _, value := range values {
				c.Header(key, value)
			}
		}
		c.Status(resp.StatusCode)
		c.Writer.Flush()
		// 【重要】从 Flush() 开始，响应头已写入客户端，任何失败都不能重试
		copyErr := streamGeminiResponseWithHook(resp.Body, c.Writer, requestLog)
		if copyErr != nil {
			// 客户端主动断开（如用户取消）不是供应商故障。
			// 取消发生在等待上游下一个 chunk 时（最常见时序）不会有写失败，
			// 而是请求 context 让 Body.Read 返回 context canceled，必须一并识别，
			// 否则用户每次取消都会给健康供应商记一次失败，连续取消即被拉黑。
			if errors.Is(copyErr, errClientAbort) ||
				errors.Is(copyErr, context.Canceled) ||
				c.Request.Context().Err() != nil {
				fmt.Printf("[Gemini]   ℹ️ 客户端中断流式连接: %s\n", provider.Name)
				return false, geminiClientAbortMsg, true
			}
			fmt.Printf("[Gemini]   ⚠️ 流式传输中断: %s | 错误: %v\n", provider.Name, copyErr)
			// 流式传输中断：已写入部分响应，客户端会收到不完整数据
			return false, fmt.Sprintf("流式传输中断: %v", copyErr), true
		}
	} else {
		// 非流式模式：先读完 body 再写 header（允许读取失败时重试）
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			fmt.Printf("[Gemini]   ⚠️ 读取响应失败: %s | 错误: %v\n", provider.Name, readErr)
			// 【修复】此时 header 尚未写入客户端，可以重试/降级
			return false, fmt.Sprintf("读取响应失败: %v", readErr), false
		}
		// 解析 Gemini 用量数据
		parseGeminiUsageMetadata(body, requestLog)
		// 读取成功后再写 header 和 body
		for key, values := range resp.Header {
			for _, value := range values {
				c.Header(key, value)
			}
		}
		c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}

	return true, "", true
}

// parseGeminiUsageMetadata 从 Gemini 非流式响应中提取用量，填充 request_log
// 复用 mergeGeminiUsageMetadata 统一解析逻辑
func parseGeminiUsageMetadata(body []byte, reqLog *ReqeustLog) {
	if len(body) == 0 || reqLog == nil {
		return
	}
	usage := gjson.GetBytes(body, "usageMetadata")
	if !usage.Exists() {
		return
	}
	mergeGeminiUsageMetadata(usage, reqLog)
}

// customCliProxyHandler 处理自定义 CLI 工具的 API 请求
// 路由格式: /custom/:toolId/v1/messages
// toolId 用于区分不同的 CLI 工具，对应 provider kind 为 "custom:{toolId}"
func (prs *ProviderRelayService) customCliProxyHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从 URL 参数提取 toolId
		toolId := c.Param("toolId")
		if toolId == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "toolId is required"})
			return
		}

		// 构建 provider kind（格式: "custom:{toolId}"）
		kind := "custom:" + toolId
		endpoint := "/v1/messages"

		fmt.Printf("[CustomCLI] 收到请求: toolId=%s, kind=%s\n", toolId, kind)

		// 读取请求体
		var bodyBytes []byte
		if c.Request.Body != nil {
			data, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
				return
			}
			bodyBytes = data
			c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		// 空 body 或非法 JSON 一定会被所有上游拒绝，提前挡掉
		if !gjson.ValidBytes(bodyBytes) {
			c.JSON(http.StatusBadRequest, gin.H{
				"type":    "error",
				"error":   map[string]string{"type": "invalid_request_error", "message": "request body must be valid JSON"},
				"message": "request body must be valid JSON",
			})
			return
		}

		isStream := gjson.GetBytes(bodyBytes, "stream").Bool()
		requestedModel := gjson.GetBytes(bodyBytes, "model").String()

		if requestedModel == "" {
			fmt.Printf("[CustomCLI][WARN] 请求未指定模型名，无法执行模型智能降级\n")
		}

		// 加载该 CLI 工具的 providers
		// (providers, 配置代数) 配对装载
		providers, configGen, err := prs.providerService.LoadProvidersWithGen(kind)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to load providers for %s: %v", kind, err)})
			return
		}

		// 过滤可用的 providers
		active := make([]Provider, 0, len(providers))
		skippedCount := 0
		skippedModel, skippedBlacklist, skippedInvalid := 0, 0, 0
		for _, provider := range providers {
			if !provider.Enabled || provider.APIURL == "" || provider.APIKey == "" {
				continue
			}

			if errs := provider.ValidateConfiguration(); len(errs) > 0 {
				fmt.Printf("[CustomCLI][WARN] Provider %s 配置验证失败，已自动跳过: %v\n", provider.Name, errs)
				skippedCount++
				skippedInvalid++
				continue
			}

			if requestedModel != "" && !provider.IsModelSupported(requestedModel) {
				fmt.Printf("[CustomCLI][INFO] Provider %s 不支持模型 %s，已跳过\n", provider.Name, requestedModel)
				skippedCount++
				skippedModel++
				continue
			}

			// 黑名单检查
			if isBlacklisted, until := prs.blacklistService.IsBlacklisted(kind, provider.Name); isBlacklisted {
				fmt.Printf("[CustomCLI] ⛔ Provider %s 已拉黑，过期时间: %v\n", provider.Name, until.Format("15:04:05"))
				skippedCount++
				skippedBlacklist++
				continue
			}

			active = append(active, provider)
		}

		if len(active) == 0 {
			respondNoEligibleProviders(c, requestedModel, skippedModel, skippedBlacklist, skippedInvalid)
			return
		}

		fmt.Printf("[CustomCLI][INFO] 找到 %d 个可用的 provider（已过滤 %d 个）：", len(active), skippedCount)
		for _, p := range active {
			fmt.Printf("%s ", p.Name)
		}
		fmt.Println()

		// 按 Level 分组
		levelGroups := make(map[int][]Provider)
		for _, provider := range active {
			level := provider.Level
			if level <= 0 {
				level = 1
			}
			levelGroups[level] = append(levelGroups[level], provider)
		}

		levels := make([]int, 0, len(levelGroups))
		for level := range levelGroups {
			levels = append(levels, level)
		}
		sort.Ints(levels)

		fmt.Printf("[CustomCLI][INFO] 共 %d 个 Level 分组：%v\n", len(levels), levels)

		query := flattenQuery(c.Request.URL.Query())
		clientHeaders := cloneHeaders(c.Request.Header)

		// 获取拉黑功能开关状态
		blacklistEnabled := prs.blacklistService.ShouldUseFixedMode()

		// 【拉黑模式】：同 Provider 重试直到被拉黑，然后切换到下一个 Provider
		if blacklistEnabled {
			// 缓存轮询设置（单次请求级别，避免重复读取配置文件）
			roundRobinSettingEnabled := prs.isRoundRobinSettingEnabled()
			if roundRobinSettingEnabled {
				fmt.Printf("[CustomCLI][INFO] 🔒 拉黑模式 + 轮询负载均衡\n")
			} else {
				fmt.Printf("[CustomCLI][INFO] 🔒 拉黑模式（顺序调度）\n")
			}

			// 获取重试配置
			retryConfig := prs.blacklistService.GetRetryConfig()
			maxRetryPerProvider := retryConfig.FailureThreshold
			retryWaitSeconds := retryConfig.RetryWaitSeconds
			fmt.Printf("[CustomCLI][INFO] 重试配置: 每 Provider 最多 %d 次重试，间隔 %d 秒\n",
				maxRetryPerProvider, retryWaitSeconds)

			var lastError error
			// 只要有过一次真正的供应商故障，终态就必须维持 502 让 SDK 退避重试
			sawNonClientError := false
			var lastProvider string
			totalAttempts := 0

			busyWaitDeadline := time.Time{}
			enteredBusyWait := false
			defer func() {
				if enteredBusyWait {
					prs.concurrency.leaveWaitPhase()
				}
			}()
			busySkipped := 0
			// 已实际尝试过的供应商：等待阶段重扫不再碰它（失败已计、重试预算不重置）
			attemptedProviders := map[string]bool{}
			// 因并发满被跳过、尚未真实尝试的候选
			busyPending := map[string]concurrencyBusyRef{}
			for {
				busySkipped = 0
				// 每 pass 重建：上一轮候选可能已被拉黑或删除，残留会让容量门控恒真
				busyPending = map[string]concurrencyBusyRef{}
				// 遍历所有 Level 和 Provider
				for _, level := range levels {
					providersInLevel := levelGroups[level]

					// 如果启用轮询，对同 Level 的 providers 进行轮询排序
					if roundRobinSettingEnabled {
						providersInLevel = prs.roundRobinOrder(kind, level, providersInLevel)
					}

					fmt.Printf("[CustomCLI][INFO] === 尝试 Level %d（%d 个 provider）===\n", level, len(providersInLevel))

					for _, provider := range providersInLevel {
						if attemptedProviders[strconv.FormatInt(provider.ID, 10)] {
							continue
						}
						// 检查是否已被拉黑（跳过已拉黑的 provider）
						if blacklisted, until := prs.blacklistService.IsBlacklisted(kind, provider.Name); blacklisted {
							fmt.Printf("[CustomCLI][INFO] ⏭️ 跳过已拉黑的 Provider: %s (解禁时间: %v)\n", provider.Name, until)
							continue
						}

						// 获取实际模型名
						effectiveModel := provider.GetEffectiveModel(requestedModel)
						currentBodyBytes := bodyBytes
						if effectiveModel != requestedModel && requestedModel != "" {
							fmt.Printf("[CustomCLI][INFO] Provider %s 映射模型: %s -> %s\n", provider.Name, requestedModel, effectiveModel)
							modifiedBody, err := ReplaceModelInRequestBody(bodyBytes, effectiveModel)
							if err != nil {
								fmt.Printf("[CustomCLI][ERROR] 模型映射失败: %v，跳过此 Provider\n", err)
								continue
							}
							currentBodyBytes = modifiedBody
						}

						// 获取有效端点
						effectiveEndpoint := provider.GetEffectiveEndpoint(endpoint)

						// 同 Provider 内重试循环
						for retryCount := 0; retryCount < maxRetryPerProvider; retryCount++ {
							totalAttempts++

							// 再次检查是否已被拉黑（重试过程中可能被拉黑）
							if blacklisted, _ := prs.blacklistService.IsBlacklisted(kind, provider.Name); blacklisted {
								fmt.Printf("[CustomCLI][INFO] 🚫 Provider %s 已被拉黑，切换到下一个\n", provider.Name)
								break
							}

							fmt.Printf("[CustomCLI][INFO] [拉黑模式] Provider: %s (Level %d) | 重试 %d/%d | Model: %s\n",
								provider.Name, level, retryCount+1, maxRetryPerProvider, effectiveModel)

							startTime := time.Now()
							ok, err := prs.forwardRequest(c, kind, provider, effectiveEndpoint, query, clientHeaders, currentBodyBytes, isStream, effectiveModel, configGen)
							duration := time.Since(startTime)

							if ok {
								fmt.Printf("[CustomCLI][INFO] ✓ 成功: %s | 重试 %d 次 | 耗时: %.2fs\n",
									provider.Name, retryCount+1, duration.Seconds())
								if err := prs.blacklistService.RecordSuccess(kind, provider.Name); err != nil {
									fmt.Printf("[CustomCLI][WARN] 清零失败计数失败: %v\n", err)
								}
								prs.setLastUsedProvider(kind, provider.Name)
								return
							}

							// 并发满载：不算尝试、不计失败，换下一个供应商
							if errors.Is(err, errProviderBusy) {
								totalAttempts--
								// 已真实失败过的供应商重试遇忙不再进等待候选：
								// 下一 pass 必然跳过它，等它只会把失败聚合错改成 503
								if pk := strconv.FormatInt(provider.ID, 10); !attemptedProviders[pk] {
									busySkipped++
									busyPending[pk] = concurrencyBusyRef{Key: pk, Limit: provider.MaxConcurrency, Gen: configGen}
								}
								fmt.Printf("[CustomCLI][INFO] Provider %s 并发已满，跳过\n", provider.Name)
								break
							}
							// 实际尝试过：等待阶段重扫不再碰它
							attemptedProviders[strconv.FormatInt(provider.ID, 10)] = true
							delete(busyPending, strconv.FormatInt(provider.ID, 10))

							// 失败处理
							lastError = err
							lastProvider = provider.Name

							errorMsg := "未知错误"
							if err != nil {
								errorMsg = err.Error()
							}
							fmt.Printf("[CustomCLI][WARN] ✗ 失败: %s | 重试 %d/%d | 错误: %s | 耗时: %.2fs\n",
								provider.Name, retryCount+1, maxRetryPerProvider, errorMsg, duration.Seconds())

							// 客户端请求被拒绝（协议转换不支持的格式/功能）：直接返回 400，不重试不拉黑。
							// 与 claude/codex 路径保持一致，否则客户端自身的问题会被算成供应商故障
							if errors.Is(err, ErrClientRequestRejected) {
								fmt.Printf("[CustomCLI][INFO] 🚫 客户端请求被拒绝: %s\n", errorMsg)
								c.JSON(http.StatusBadRequest, gin.H{
									"type":    "error",
									"error":   map[string]string{"type": "invalid_request_error", "message": errorMsg},
									"message": errorMsg,
								})
								return
							}

							// 客户端中断不计入失败次数，直接返回
							if errors.Is(err, errClientAbort) {
								fmt.Printf("[CustomCLI][INFO] 客户端中断，停止重试\n")
								return
							}

							// 上游 2xx 后中途断流：响应已部分写出，不能再换供应商，但必须计入失败
							if errors.Is(err, errUpstreamStreamAborted) {
								if err := prs.blacklistService.RecordFailure(kind, provider.Name); err != nil {
									fmt.Printf("[CustomCLI][ERROR] 记录失败到黑名单失败: %v\n", err)
								}
								return
							}

							// 上游判定"请求内容本身有问题"：不计供应商失败，直接换下一个供应商
							if errors.Is(err, errUpstreamClientError) {
								fmt.Printf("[CustomCLI][INFO] 上游拒绝请求内容，不计供应商失败，切换到下一个: %s\n", errorMsg)
								break
							}

							sawNonClientError = true

							// 记录失败次数（可能触发拉黑）
							if err := prs.blacklistService.RecordFailure(kind, provider.Name); err != nil {
								fmt.Printf("[CustomCLI][ERROR] 记录失败到黑名单失败: %v\n", err)
							}

							// 检查是否刚被拉黑
							if blacklisted, _ := prs.blacklistService.IsBlacklisted(kind, provider.Name); blacklisted {
								fmt.Printf("[CustomCLI][INFO] 🚫 Provider %s 达到失败阈值，已被拉黑，切换到下一个\n", provider.Name)
								break
							}

							// 多地址池已整轮试过：失败已计一次，直接切下一供应商
							if errors.Is(err, errEndpointPoolExhausted) {
								fmt.Printf("[CustomCLI][INFO] Provider %s 地址池耗尽，切换下一供应商\n", provider.Name)
								break
							}

							// 等待后重试（除非是最后一次）；等待期间客户端可能已经离开
							if retryCount < maxRetryPerProvider-1 {
								fmt.Printf("[CustomCLI][INFO] ⏳ 等待 %d 秒后重试...\n", retryWaitSeconds)
								select {
								case <-time.After(time.Duration(retryWaitSeconds) * time.Second):
								case <-c.Request.Context().Done():
									fmt.Printf("[CustomCLI][INFO] 等待重试期间客户端已断开，停止尝试\n")
									return
								}
							}
						}

						if c.Request.Context().Err() != nil {
							fmt.Printf("[CustomCLI][INFO] 客户端已断开，停止尝试后续 Provider\n")
							return
						}
					}
				}

				// 一整遍下来只要还有因并发满被跳过的供应商，就进入有界等待
				if busySkipped == 0 {
					break
				}
				if busyWaitDeadline.IsZero() {
					busyWaitDeadline = time.Now().Add(prs.concurrency.waitBudget)
					if !prs.concurrency.enterWaitPhase() {
						respondAllBusy(c, kind)
						return
					}
					enteredBusyWait = true
				}
				// 唤醒以"忙候选真的有空位"为门控：本轮实际尝试供应商的正常释放
				// 也会触发全局信号，不加门控直接重扫会形成自唤醒重试风暴
				woke := false
				for {
					capSignal := prs.concurrency.releaseSignal()
					if prs.concurrency.anyCapacity(kind, busyPending) {
						woke = true
						break
					}
					if !prs.concurrency.waitForRelease(c.Request.Context(), busyWaitDeadline, capSignal) {
						break
					}
				}
				if !woke {
					respondAllBusy(c, kind)
					return
				}
				// 容量门控可能被"释放后立刻又被占走"的候选反复触发，
				// 重扫前硬校验总预算与客户端 context，防止空转越过 deadline
				if c.Request.Context().Err() != nil || time.Now().After(busyWaitDeadline) {
					respondAllBusy(c, kind)
					return
				}
				fmt.Printf("[INFO] 并发配额有释放，重扫供应商\n")
			}

			// 所有 Provider 都失败或被拉黑
			fmt.Printf("[CustomCLI][ERROR] 💥 拉黑模式：所有 Provider 都失败或被拉黑（共尝试 %d 次）\n", totalAttempts)

			errorMsg := "未知错误"
			if lastError != nil {
				errorMsg = lastError.Error()
			}
			respondAllProvidersFailed(c, lastError, !sawNonClientError, gin.H{
				"error":         fmt.Sprintf("所有 Provider 都失败或被拉黑，最后尝试: %s - %s", lastProvider, errorMsg),
				"lastProvider":  lastProvider,
				"totalAttempts": totalAttempts,
				"mode":          "blacklist_retry",
				"hint":          "拉黑模式已开启，同 Provider 重试到拉黑再切换。如需立即降级请关闭拉黑功能",
			})
			return
		}

		// 【降级模式】：失败自动尝试下一个 provider
		roundRobinEnabled := prs.isRoundRobinEnabled()
		if roundRobinEnabled {
			fmt.Printf("[CustomCLI][INFO] 🔄 降级模式 + 轮询负载均衡\n")
		} else {
			fmt.Printf("[CustomCLI][INFO] 🔄 降级模式（顺序降级）\n")
		}

		var lastError error
		// 只要有过一次真正的供应商故障，终态就必须维持 502 让 SDK 退避重试
		sawNonClientError := false
		var lastProvider string
		var lastDuration time.Duration
		totalAttempts := 0

		busyWaitDeadline := time.Time{}
		enteredBusyWait := false
		defer func() {
			if enteredBusyWait {
				prs.concurrency.leaveWaitPhase()
			}
		}()
		busySkipped := 0
		// 已实际尝试过的供应商：等待阶段重扫不再碰它（失败已计、重试预算不重置）
		attemptedProviders := map[string]bool{}
		// 因并发满被跳过、尚未真实尝试的候选
		busyPending := map[string]concurrencyBusyRef{}
		for {
			busySkipped = 0
			// 每 pass 重建：上一轮候选可能已被拉黑或删除，残留会让容量门控恒真
			busyPending = map[string]concurrencyBusyRef{}
			for _, level := range levels {
				providersInLevel := levelGroups[level]

				// 如果启用轮询，对同 Level 的 providers 进行轮询排序
				if roundRobinEnabled {
					providersInLevel = prs.roundRobinOrder(kind, level, providersInLevel)
				}

				fmt.Printf("[CustomCLI][INFO] === 尝试 Level %d（%d 个 provider）===\n", level, len(providersInLevel))

				for i, provider := range providersInLevel {
					if attemptedProviders[strconv.FormatInt(provider.ID, 10)] {
						continue
					}
					totalAttempts++

					effectiveModel := provider.GetEffectiveModel(requestedModel)
					currentBodyBytes := bodyBytes
					if effectiveModel != requestedModel && requestedModel != "" {
						fmt.Printf("[CustomCLI][INFO] Provider %s 映射模型: %s -> %s\n", provider.Name, requestedModel, effectiveModel)
						modifiedBody, err := ReplaceModelInRequestBody(bodyBytes, effectiveModel)
						if err != nil {
							fmt.Printf("[CustomCLI][ERROR] 替换模型名失败: %v\n", err)
							continue
						}
						currentBodyBytes = modifiedBody
					}

					fmt.Printf("[CustomCLI][INFO]   [%d/%d] Provider: %s | Model: %s\n", i+1, len(providersInLevel), provider.Name, effectiveModel)
					// 获取有效的端点（用户配置优先）
					effectiveEndpoint := provider.GetEffectiveEndpoint(endpoint)

					startTime := time.Now()
					ok, err := prs.forwardRequest(c, kind, provider, effectiveEndpoint, query, clientHeaders, currentBodyBytes, isStream, effectiveModel, configGen)
					duration := time.Since(startTime)

					if ok {
						fmt.Printf("[CustomCLI][INFO]   ✓ Level %d 成功: %s | 耗时: %.2fs\n", level, provider.Name, duration.Seconds())
						if err := prs.blacklistService.RecordSuccess(kind, provider.Name); err != nil {
							fmt.Printf("[CustomCLI][WARN] 清零失败计数失败: %v\n", err)
						}
						prs.setLastUsedProvider(kind, provider.Name)
						return
					}

					// 并发满载：不算尝试、不计失败，换下一个供应商
					if errors.Is(err, errProviderBusy) {
						totalAttempts--
						busySkipped++
						pk := strconv.FormatInt(provider.ID, 10)
						busyPending[pk] = concurrencyBusyRef{Key: pk, Limit: provider.MaxConcurrency, Gen: configGen}
						fmt.Printf("[CustomCLI][INFO] Provider %s 并发已满，跳过\n", provider.Name)
						continue
					}
					// 实际尝试过：等待阶段重扫不再碰它
					attemptedProviders[strconv.FormatInt(provider.ID, 10)] = true
					delete(busyPending, strconv.FormatInt(provider.ID, 10))

					lastError = err
					lastProvider = provider.Name
					lastDuration = duration

					errorMsg := "未知错误"
					if err != nil {
						errorMsg = err.Error()
					}
					fmt.Printf("[CustomCLI][WARN]   ✗ Level %d 失败: %s | 错误: %s | 耗时: %.2fs\n",
						level, provider.Name, errorMsg, duration.Seconds())

					// 客户端请求被拒绝（协议转换不支持的格式/功能）：直接返回 400，不重试不拉黑
					if errors.Is(err, ErrClientRequestRejected) {
						fmt.Printf("[CustomCLI][INFO] 🚫 客户端请求被拒绝: %s\n", errorMsg)
						c.JSON(http.StatusBadRequest, gin.H{
							"type":    "error",
							"error":   map[string]string{"type": "invalid_request_error", "message": errorMsg},
							"message": errorMsg,
						})
						return
					}

					if errors.Is(err, errClientAbort) {
						fmt.Printf("[CustomCLI][INFO] 客户端中断，跳过失败计数: %s\n", provider.Name)
						return
					}

					// 上游 2xx 后中途断流：响应已部分写出，不能再降级，但必须计入失败
					if errors.Is(err, errUpstreamStreamAborted) {
						if err := prs.blacklistService.RecordFailure(kind, provider.Name); err != nil {
							fmt.Printf("[CustomCLI][ERROR] 记录失败到黑名单失败: %v\n", err)
						}
						return
					}

					// 上游判定"请求内容本身有问题"：不计入供应商失败，继续尝试下一个
					if errors.Is(err, errUpstreamClientError) {
						fmt.Printf("[CustomCLI][INFO] 上游拒绝请求内容，不计供应商失败: %s\n", errorMsg)
					} else {
						sawNonClientError = true
						if err := prs.blacklistService.RecordFailure(kind, provider.Name); err != nil {
							fmt.Printf("[CustomCLI][ERROR] 记录失败到黑名单失败: %v\n", err)
						}
					}

					if c.Request.Context().Err() != nil {
						fmt.Printf("[CustomCLI][INFO] 客户端已断开，停止尝试后续 Provider\n")
						return
					}

					// 发送切换通知
					if prs.notificationService != nil {
						nextProvider := ""
						if i+1 < len(providersInLevel) {
							nextProvider = providersInLevel[i+1].Name
						} else {
							for _, nextLevel := range levels {
								if nextLevel > level && len(levelGroups[nextLevel]) > 0 {
									nextProvider = levelGroups[nextLevel][0].Name
									break
								}
							}
						}
						if nextProvider != "" {
							prs.notificationService.NotifyProviderSwitch(SwitchNotification{
								FromProvider: provider.Name,
								ToProvider:   nextProvider,
								Reason:       errorMsg,
								Platform:     kind,
							})
						}
					}
				}

				fmt.Printf("[CustomCLI][WARN] Level %d 的所有 %d 个 provider 均失败，尝试下一 Level\n", level, len(providersInLevel))
			}

			// 一整遍下来只要还有因并发满被跳过的供应商，就进入有界等待
			if busySkipped == 0 {
				break
			}
			if busyWaitDeadline.IsZero() {
				busyWaitDeadline = time.Now().Add(prs.concurrency.waitBudget)
				if !prs.concurrency.enterWaitPhase() {
					respondAllBusy(c, kind)
					return
				}
				enteredBusyWait = true
			}
			// 唤醒以"忙候选真的有空位"为门控：本轮实际尝试供应商的正常释放
			// 也会触发全局信号，不加门控直接重扫会形成自唤醒重试风暴
			woke := false
			for {
				capSignal := prs.concurrency.releaseSignal()
				if prs.concurrency.anyCapacity(kind, busyPending) {
					woke = true
					break
				}
				if !prs.concurrency.waitForRelease(c.Request.Context(), busyWaitDeadline, capSignal) {
					break
				}
			}
			if !woke {
				respondAllBusy(c, kind)
				return
			}
			// 容量门控可能被"释放后立刻又被占走"的候选反复触发，
			// 重扫前硬校验总预算与客户端 context，防止空转越过 deadline
			if c.Request.Context().Err() != nil || time.Now().After(busyWaitDeadline) {
				respondAllBusy(c, kind)
				return
			}
			fmt.Printf("[INFO] 并发配额有释放，重扫供应商\n")
		}

		// 所有 provider 都失败
		errorMsg := "未知错误"
		if lastError != nil {
			errorMsg = lastError.Error()
		}
		fmt.Printf("[CustomCLI][ERROR] 所有 %d 个 provider 均失败，最后尝试: %s | 错误: %s\n",
			totalAttempts, lastProvider, errorMsg)

		respondAllProvidersFailed(c, lastError, !sawNonClientError, gin.H{
			"error":          fmt.Sprintf("所有 %d 个 provider 均失败，最后错误: %s", totalAttempts, errorMsg),
			"last_provider":  lastProvider,
			"last_duration":  fmt.Sprintf("%.2fs", lastDuration.Seconds()),
			"total_attempts": totalAttempts,
		})
	}
}

// forwardModelsRequest 共享的 /v1/models 请求转发逻辑
// 返回 (selectedProvider, error)
func (prs *ProviderRelayService) forwardModelsRequest(
	c *gin.Context,
	kind string,
	logPrefix string,
) error {
	fmt.Printf("[%s] 收到 /v1/models 请求, kind=%s\n", logPrefix, kind)

	// 加载 providers
	providers, err := prs.providerService.LoadProviders(kind)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load providers"})
		return fmt.Errorf("failed to load providers: %w", err)
	}

	// 过滤可用的 providers（启用 + URL + APIKey）
	var activeProviders []Provider
	for _, provider := range providers {
		if !provider.Enabled || provider.APIURL == "" || provider.APIKey == "" {
			continue
		}

		// 黑名单检查：跳过已拉黑的 provider
		if isBlacklisted, until := prs.blacklistService.IsBlacklisted(kind, provider.Name); isBlacklisted {
			fmt.Printf("[%s] ⛔ Provider %s 已拉黑，过期时间: %v\n", logPrefix, provider.Name, until.Format("15:04:05"))
			continue
		}

		activeProviders = append(activeProviders, provider)
	}

	if len(activeProviders) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no providers available"})
		return fmt.Errorf("no providers available")
	}

	// 按 Level 分组并排序
	levelGroups := make(map[int][]Provider)
	for _, provider := range activeProviders {
		level := provider.Level
		if level <= 0 {
			level = 1
		}
		levelGroups[level] = append(levelGroups[level], provider)
	}

	levels := make([]int, 0, len(levelGroups))
	for level := range levelGroups {
		levels = append(levels, level)
	}
	sort.Ints(levels)

	// 展平候选（Level 升序、组内保持用户顺序），逐个尝试直到成功——
	// 与聊天转发一致的多 Provider 容错，避免首个供应商临时故障导致整体失败
	ordered := make([]Provider, 0, len(activeProviders))
	for _, level := range levels {
		ordered = append(ordered, levelGroups[level]...)
	}

	// 复用共享连接池；client 级 Timeout 32h 不适合模型列表这类短请求，
	// 这里用 30s 的轻量包装挂到同一个 Transport 上，按供应商选择验证策略
	modelsClient := &http.Client{Timeout: 30 * time.Second, Transport: relayHTTPClient.Transport}
	modelsClientInsecure := &http.Client{Timeout: 30 * time.Second, Transport: relayHTTPClientInsecure.Transport}
	var lastErr error
	for i := range ordered {
		selectedProvider := &ordered[i]
		client := modelsClient
		if selectedProvider.InsecureSkipVerify {
			warnInsecureProviderOnce(selectedProvider.Name)
			client = modelsClientInsecure
		}
		fmt.Printf("[%s] 使用 Provider: %s | URL: %s\n", logPrefix, selectedProvider.Name, selectedProvider.APIURL)

		// 地址池：与聊天转发同语义——多地址供应商按冷却排序逐地址尝试，
		// 传输失败与 408/421/429/5xx 切下一地址，凭据类 4xx 直接换供应商
		pool := selectedProvider.EndpointPool()
		multiAddress := len(pool) > 1
		if multiAddress {
			pool = prs.endpointCooldowns.Order(kind, selectedProvider.ID, pool)
		}

	addrLoop:
		for _, addr := range pool {
			// 构建目标 URL（拼接地址和 /v1/models）
			targetURL := joinURL(addr, "/v1/models")

			// 绑定客户端 context：客户端断开后不得继续遍历地址与供应商
			req, err := http.NewRequestWithContext(c.Request.Context(), "GET", targetURL, nil)
			if err != nil {
				lastErr = fmt.Errorf("failed to create request: %w", err)
				continue
			}

			// 复制客户端请求头
			for key, values := range c.Request.Header {
				for _, value := range values {
					req.Header.Add(key, value)
				}
			}
			// 清掉客户端自带凭据，避免用户本机 Key 被转发给第三方供应商
			for _, name := range []string{
				"Authorization", "Proxy-Authorization", "X-Api-Key", "Api-Key", "X-Goog-Api-Key",
				"Accept-Encoding", "Connection", "Keep-Alive", "Te", "Upgrade",
			} {
				req.Header.Del(name)
			}

			// 请求清理（头部）：与聊天转发同规则，在注入供应商凭据之前作用于透传的客户端头
			if selectedProvider.RequestSanitizeEnabled {
				sanitizeHTTPHeaders(req.Header, selectedProvider.SanitizeConfig)
			}

			// 根据认证方式设置请求头（默认 Bearer，与 v2.2.x 保持一致）
			authType := strings.ToLower(strings.TrimSpace(selectedProvider.ConnectivityAuthType))
			switch authType {
			case "x-api-key":
				req.Header.Set("x-api-key", selectedProvider.APIKey)
				req.Header.Set("anthropic-version", "2023-06-01")
			case "", "bearer":
				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", selectedProvider.APIKey))
			default:
				headerName := strings.TrimSpace(selectedProvider.ConnectivityAuthType)
				if headerName == "" || strings.EqualFold(headerName, "custom") {
					headerName = "Authorization"
				}
				req.Header.Set(headerName, selectedProvider.APIKey)
			}

			// 设置默认 Accept 头
			if req.Header.Get("Accept") == "" {
				req.Header.Set("Accept", "application/json")
			}

			resp, err := client.Do(req)
			if err != nil {
				// 客户端已断开：立即停止全部尝试，不冷却地址、不换供应商
				if c.Request.Context().Err() != nil {
					fmt.Printf("[%s] 客户端已断开，停止尝试\n", logPrefix)
					return fmt.Errorf("client aborted: %w", err)
				}
				fmt.Printf("[%s] ✗ 请求失败: %s (%s) | 错误: %v | 尝试下一个\n", logPrefix, selectedProvider.Name, addr, err)
				lastErr = fmt.Errorf("request failed: %w", err)
				if multiAddress {
					prs.endpointCooldowns.MarkFailure(kind, selectedProvider.ID, addr, defaultEndpointCooldown)
				}
				continue // 传输层失败：可切下一地址
			}

			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr != nil {
				// 取消可能发生在响应头之后：错误从 body 读取冒出，
				// 同样必须即刻终止，不冷却地址、不换供应商
				if c.Request.Context().Err() != nil {
					fmt.Printf("[%s] 客户端已断开，停止尝试\n", logPrefix)
					return fmt.Errorf("client aborted: %w", readErr)
				}
				fmt.Printf("[%s] ✗ 读取响应失败: %s (%s) | 错误: %v | 尝试下一个\n", logPrefix, selectedProvider.Name, addr, readErr)
				lastErr = fmt.Errorf("failed to read response: %w", readErr)
				if multiAddress {
					prs.endpointCooldowns.MarkFailure(kind, selectedProvider.ID, addr, defaultEndpointCooldown)
				}
				continue
			}

			if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				fmt.Printf("[%s] ✗ HTTP %d: %s (%s) | 尝试下一个\n", logPrefix, resp.StatusCode, selectedProvider.Name, addr)
				lastErr = fmt.Errorf("provider %s HTTP %d", selectedProvider.Name, resp.StatusCode)
				switchable := resp.StatusCode == http.StatusRequestTimeout ||
					resp.StatusCode == http.StatusMisdirectedRequest ||
					resp.StatusCode == http.StatusTooManyRequests ||
					resp.StatusCode >= 500
				if multiAddress && switchable {
					cooldown := defaultEndpointCooldown
					if resp.StatusCode == http.StatusTooManyRequests {
						if d := parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()); d > 0 {
							cooldown = d
						}
					}
					prs.endpointCooldowns.MarkFailure(kind, selectedProvider.ID, addr, cooldown)
					continue
				}
				break addrLoop // 凭据/请求类错误：换地址无意义，直接换供应商
			}

			if multiAddress {
				prs.endpointCooldowns.MarkSuccess(kind, selectedProvider.ID, addr)
			}

			// 复制响应头
			for key, values := range resp.Header {
				for _, value := range values {
					c.Header(key, value)
				}
			}

			fmt.Printf("[%s] ✓ 成功: %s (%s) | HTTP %d\n", logPrefix, selectedProvider.Name, addr, resp.StatusCode)
			c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
			return nil
		}
	}

	fmt.Printf("[%s] ✗ 所有 %d 个 provider 均失败 | 最后错误: %v\n", logPrefix, len(ordered), lastErr)
	c.JSON(http.StatusBadGateway, gin.H{
		"error":   "all providers failed for /v1/models",
		"details": fmt.Sprintf("%v", lastErr),
	})
	return fmt.Errorf("all providers failed: %v", lastErr)
}

// modelsHandler 处理 /v1/models 请求（OpenAI-compatible API）
// 将请求转发到第一个可用的 provider 并注入 API Key
func (prs *ProviderRelayService) modelsHandler(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		_ = prs.forwardModelsRequest(c, kind, "Models")
	}
}

// customModelsHandler 处理自定义 CLI 工具的 /v1/models 请求
// 路由格式: /custom/:toolId/v1/models
func (prs *ProviderRelayService) customModelsHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从 URL 参数提取 toolId
		toolId := c.Param("toolId")
		if toolId == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "toolId is required"})
			return
		}

		// 构建 provider kind（格式: "custom:{toolId}"）
		kind := "custom:" + toolId

		_ = prs.forwardModelsRequest(c, kind, "CustomModels")
	}
}

// ========== 请求清理（Request Sanitizer，黑名单模式，按供应商开启） ==========

// 内置默认黑名单：供应商对应维度未配置（nil）时使用；
// 显式配置为空数组表示该维度什么都不删。
var (
	defaultBlockedBodyFields = []string{"prompt_caching"}
	defaultBlockedHeaders    []string
	defaultBlockedBetaValues = []string{
		"prompt-caching-scope-2026-01-05",
		"redact-thinking-2026-02-12",
	}
)

// resolveBlocklist 把自定义列表（nil 时退回默认列表）转成查找集合。
// fold 为 true 时按小写归一（用于大小写不敏感的请求头名）。
func resolveBlocklist(custom, def []string, fold bool) map[string]bool {
	src := custom
	if src == nil {
		src = def
	}
	m := make(map[string]bool, len(src))
	for _, v := range src {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if fold {
			v = strings.ToLower(v)
		}
		m[v] = true
	}
	return m
}

// cfg 三个访问器把指针三态展开成切片语义：
// nil 指针 → 返回 nil（调用方回退内置默认）；指向空数组 → 返回非 nil 空切片（什么都不删）。
func cfgBodyFields(cfg *SanitizeConfig) []string {
	if cfg == nil || cfg.BlockedBodyFields == nil {
		return nil
	}
	return derefList(cfg.BlockedBodyFields)
}

func cfgHeaders(cfg *SanitizeConfig) []string {
	if cfg == nil || cfg.BlockedHeaders == nil {
		return nil
	}
	return derefList(cfg.BlockedHeaders)
}

func cfgBetaValues(cfg *SanitizeConfig) []string {
	if cfg == nil || cfg.BlockedBetaValues == nil {
		return nil
	}
	return derefList(cfg.BlockedBetaValues)
}

// derefList 解引用并保证返回非 nil 切片，避免"指向 nil 切片的指针"退化回默认列表。
func derefList(p *[]string) []string {
	if *p == nil {
		return []string{}
	}
	return *p
}

// sanitizeRequestBody 移除请求体顶层黑名单字段，返回清理后的 body 与被移除的键。
// 单趟重建：一次解析、一次序列化；顶层键序可能变化，JSON 语义不受影响。
// 顶层存在重复键的畸形 body 原样放行——map 解析会静默吞并重复键，
// 宁可不清理也不能改写非目标数据。
func sanitizeRequestBody(bodyBytes []byte, cfg *SanitizeConfig) ([]byte, []string) {
	blocked := resolveBlocklist(cfgBodyFields(cfg), defaultBlockedBodyFields, false)
	if len(blocked) == 0 {
		return bodyBytes, nil
	}

	root := gjson.ParseBytes(bodyBytes)
	if !root.IsObject() {
		return bodyBytes, nil
	}
	// 快速路径：统计顶层键出现次数，没有命中黑名单就不动 body
	hasBlocked := false
	keyCount := 0
	root.ForEach(func(key, _ gjson.Result) bool {
		keyCount++
		if blocked[key.String()] {
			hasBlocked = true
		}
		return true
	})
	if !hasBlocked {
		return bodyBytes, nil
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(bodyBytes, &fields); err != nil {
		return bodyBytes, nil
	}
	if len(fields) != keyCount {
		fmt.Printf("[Sanitize] 请求体顶层存在重复键，跳过清理以免改写非目标数据\n")
		return bodyBytes, nil
	}

	var removed []string
	for k := range fields {
		if blocked[k] {
			removed = append(removed, k)
			delete(fields, k)
		}
	}
	cleaned, err := json.Marshal(fields)
	if err != nil {
		return bodyBytes, nil
	}
	sort.Strings(removed)
	return cleaned, removed
}

// sanitizeHeaders 移除黑名单请求头并清理 anthropic-beta 中不支持的值。
// 必须在注入供应商凭据之前调用，用户配置的黑名单才碰不到中继写入的认证头。
func sanitizeHeaders(headers map[string]string, cfg *SanitizeConfig) map[string]string {
	blockedHeader := resolveBlocklist(cfgHeaders(cfg), defaultBlockedHeaders, true)
	blockedBeta := resolveBlocklist(cfgBetaValues(cfg), defaultBlockedBetaValues, false)

	cleaned := make(map[string]string, len(headers))
	for k, v := range headers {
		lower := strings.ToLower(k)
		if blockedHeader[lower] {
			continue
		}
		if lower == "anthropic-beta" {
			v = cleanAnthropicBeta(v, blockedBeta)
			if v == "" {
				continue
			}
		}
		cleaned[k] = v
	}
	return cleaned
}

// sanitizeHTTPHeaders 是 sanitizeHeaders 的 http.Header 版本，供 models 转发路径使用。
func sanitizeHTTPHeaders(h http.Header, cfg *SanitizeConfig) {
	blockedHeader := resolveBlocklist(cfgHeaders(cfg), defaultBlockedHeaders, true)
	blockedBeta := resolveBlocklist(cfgBetaValues(cfg), defaultBlockedBetaValues, false)

	for _, k := range headerKeys(h) {
		lower := strings.ToLower(k)
		if blockedHeader[lower] {
			h.Del(k)
			continue
		}
		if lower == "anthropic-beta" {
			// 逐个清理同名头的每个值，不能用 Get/Set（只取第一个值、覆盖其余合法值）
			kept := make([]string, 0, len(h.Values(k)))
			for _, v := range h.Values(k) {
				if cleaned := cleanAnthropicBeta(v, blockedBeta); cleaned != "" {
					kept = append(kept, cleaned)
				}
			}
			if len(kept) == 0 {
				h.Del(k)
			} else {
				h[k] = kept
			}
		}
	}
}

// headerKeys 先收集键再遍历，避免边遍历边删除。
func headerKeys(h http.Header) []string {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	return keys
}

// cleanAnthropicBeta 从 anthropic-beta 头的逗号分隔值中剔除黑名单项。
func cleanAnthropicBeta(value string, blocked map[string]bool) string {
	parts := strings.Split(value, ",")
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" || blocked[trimmed] {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	return strings.Join(filtered, ", ")
}
