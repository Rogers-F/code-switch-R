package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	modelpricing "codeswitch/resources/model-pricing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ModelSyncService 从 llm-metadata(basellm.github.io,镜像 llm-metadata.pages.dev)
// 同步各厂商模型目录与价格,校验后原子落盘并热重建定价快照。
// 生效数据优先级:本地已验证缓存 > 内置种子;两者都经同一套解析/转换逻辑。
//
// 并发模型:所有触发源(启动延迟、1h 调度、SyncNow)汇入 runSync,mutex+running
// 单飞;状态/目录的读写全部在 s.mu 内;定价重建走 pricing 自身的原子快照。

const (
	modelSyncDirName   = "llm-metadata"
	modelSyncStateFile = "state.json"

	modelSyncInterval     = 24 * time.Hour
	modelSyncJitterMax    = 30 * time.Minute
	modelSyncStartupDelay = 10 * time.Second
	modelSyncTick         = time.Hour

	modelSyncBatchTimeout   = 90 * time.Second
	modelSyncRequestTimeout = 15 * time.Second
	modelSyncMaxBody        = 3 << 20 // 3MiB
	modelSyncMaxConcurrent  = 3

	modelSyncFailBackoffBase = time.Hour
	modelSyncFailBackoffCap  = 24 * time.Hour

	// 模型数骤降隔离:低于 30 天内高水位一半即拒收该厂商批次。
	modelSyncHighWaterWindow = 30 * 24 * time.Hour
	// 已知模型基础价上涨超过该倍数(新旧均为正)时隔离该厂商批次。
	modelSyncMaxPriceJump = 10.0
)

// modelSyncBaseURLs 主源在前,镜像在后;测试可注入替换。
var modelSyncBaseURLs = []string{
	"https://basellm.github.io/llm-metadata",
	"https://llm-metadata.pages.dev",
}

// providerCanaryPatterns 每厂商必须存在的标志性模型族,防止拿错/污染数据。
var providerCanaryPatterns = map[string]*regexp.Regexp{
	"anthropic":  regexp.MustCompile(`^claude-`),
	"openai":     regexp.MustCompile(`^gpt-\d`),
	"google":     regexp.MustCompile(`^gemini-`),
	"deepseek":   regexp.MustCompile(`^deepseek-`),
	"alibaba":    regexp.MustCompile(`^(qwen|qvq|qwq)`),
	"moonshotai": regexp.MustCompile(`^(kimi-|moonshot-)`),
	"zhipuai":    regexp.MustCompile(`^glm-`),
}

// providerFamilyGuards 解析器依赖的家族:上一份有效目录存在而新批次整族消失时隔离。
var providerFamilyGuards = map[string][]*regexp.Regexp{
	"anthropic": {regexp.MustCompile(`haiku`)},
	"openai":    {regexp.MustCompile(`^gpt-\d+(\.\d+)*$`)},
	"google":    {regexp.MustCompile(`-pro(-preview)?$`), regexp.MustCompile(`-flash(-preview)?$`)},
}

type providerSyncState struct {
	ETagByOrigin map[string]string     `json:"etagByOrigin,omitempty"`
	SHA256       string                `json:"sha256,omitempty"`
	FetchedAt    time.Time             `json:"fetchedAt,omitempty"`
	CheckedAt    time.Time             `json:"checkedAt,omitempty"`
	ModelCount   int                   `json:"modelCount,omitempty"`
	HighWater    int                   `json:"highWater,omitempty"`
	HighWaterAt  time.Time             `json:"highWaterAt,omitempty"`
	NextDue      time.Time             `json:"nextDue,omitempty"`
	FailStreak   int                   `json:"failStreak,omitempty"`
	LastError    string                `json:"lastError,omitempty"`
	LastPrices   map[string][2]float64 `json:"lastPrices,omitempty"`
}

type modelSyncState struct {
	Generation  int64                         `json:"generation"`
	LastSuccess time.Time                     `json:"lastSuccess,omitempty"`
	Providers   map[string]*providerSyncState `json:"providers"`
}

func newModelSyncState() *modelSyncState {
	return &modelSyncState{Providers: make(map[string]*providerSyncState)}
}

// ModelSyncProviderStatus 单厂商同步状态(前端展示)。
type ModelSyncProviderStatus struct {
	Provider   string    `json:"provider"`
	Source     string    `json:"source"` // remote=已同步缓存 seed=内置种子
	ModelCount int       `json:"modelCount"`
	FetchedAt  time.Time `json:"fetchedAt"`
	CheckedAt  time.Time `json:"checkedAt"`
	NextDue    time.Time `json:"nextDue"`
	LastError  string    `json:"lastError,omitempty"`
}

// ModelSyncStatus 同步总体状态(前端展示)。
type ModelSyncStatus struct {
	AutoSyncEnabled bool                      `json:"autoSyncEnabled"`
	Running         bool                      `json:"running"`
	Generation      int64                     `json:"generation"`
	LastSuccess     time.Time                 `json:"lastSuccess"`
	Providers       []ModelSyncProviderStatus `json:"providers"`
	Pricing         modelpricing.RebuildStats `json:"pricing"`
	DefaultModels   DefaultModels             `json:"defaultModels"`
}

// ModelSyncService 见文件头说明。
type ModelSyncService struct {
	appSettings *AppSettingsService
	policy      *DefaultModelPolicy
	pricing     *modelpricing.Service
	app         *application.App

	baseURLs   []string
	httpClient *http.Client
	dir        string

	mu       sync.Mutex
	state    *modelSyncState
	catalogs map[string]*modelpricing.RemoteCatalog
	sources  map[string]string
	seed     map[string]*modelpricing.RemoteCatalog
	running   bool
	restoring bool // RestoreBuiltinPricing 预约标志:置位期间 runSync 拒绝启动
	stopped   bool

	rootCtx   context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup // 调度器 goroutine
	syncWG    sync.WaitGroup // 在途同步批次(含 SyncNow 直接触发)
	startOnce sync.Once
	stopOnce  sync.Once
}

// NewModelSyncService 创建服务:载入本地缓存(哈希校验,失败回种子)并完成首次定价重建。
func NewModelSyncService(appSettings *AppSettingsService, policy *DefaultModelPolicy) *ModelSyncService {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	pricing, pricingErr := modelpricing.DefaultService()
	if pricingErr != nil {
		log.Printf("[ModelSync] 定价服务初始化失败: %v", pricingErr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &ModelSyncService{
		appSettings: appSettings,
		policy:      policy,
		pricing:     pricing,
		baseURLs:    append([]string(nil), modelSyncBaseURLs...),
		httpClient:  &http.Client{Timeout: 0}, // 超时由 per-request context 控制
		dir:         filepath.Join(home, appSettingsDir, modelSyncDirName),
		state:       newModelSyncState(),
		catalogs:    make(map[string]*modelpricing.RemoteCatalog),
		sources:     make(map[string]string),
		seed:        modelpricing.EmbeddedSeedCatalogs(),
		rootCtx:     ctx,
		cancel:      cancel,
	}
	s.loadState()
	s.loadLocalCatalogs()
	s.rebuildPricing()
	if policy != nil {
		policy.SetSource(s)
	}
	return s
}

// SetApp 注入 Wails App 引用,用于向前端广播同步事件。
func (s *ModelSyncService) SetApp(app *application.App) {
	s.mu.Lock()
	s.app = app
	s.mu.Unlock()
}

// Catalogs 返回当前生效目录(浅拷贝;目录对象建成后不可变)。
func (s *ModelSyncService) Catalogs() map[string]*modelpricing.RemoteCatalog {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]*modelpricing.RemoteCatalog, len(s.catalogs))
	for k, v := range s.catalogs {
		out[k] = v
	}
	return out
}

// Start 启动后台调度:延迟首查,此后每小时检查到期厂商。
func (s *ModelSyncService) Start() {
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			select {
			case <-time.After(modelSyncStartupDelay):
			case <-s.rootCtx.Done():
				return
			}
			s.maybeAutoSync()
			ticker := time.NewTicker(modelSyncTick)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					s.maybeAutoSync()
				case <-s.rootCtx.Done():
					return
				}
			}
		}()
	})
}

// Stop 取消在途请求并等待后台退出(接入 app.OnShutdown)。
// 同时等待 SyncNow 触发的在途批次,避免关停后仍有落盘/事件广播。
func (s *ModelSyncService) Stop() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.stopped = true
		s.mu.Unlock()
		s.cancel()
		s.wg.Wait()
		s.syncWG.Wait()
	})
}

// SyncNow 手动全量同步(无视到期时间);已有批次在跑时返回当前状态不重入。
func (s *ModelSyncService) SyncNow() ModelSyncStatus {
	return s.runSync(modelpricing.RemoteProviderIDs)
}

// GetSyncStatus 返回当前同步状态。
func (s *ModelSyncService) GetSyncStatus() ModelSyncStatus {
	s.mu.Lock()
	status := s.coreStatusLocked()
	s.mu.Unlock()
	return s.decorateStatus(status)
}

// GetDefaultModels 返回各平台当前解析出的默认/探测模型。
func (s *ModelSyncService) GetDefaultModels() DefaultModels {
	if s.policy == nil {
		return DefaultModels{}
	}
	return s.policy.DefaultModels()
}

// RestoreBuiltinPricing 恢复内置数据:关闭自动同步(可再开启)、清空缓存与状态、
// 立即以"嵌入 base + 内置种子"重建生效快照。
// restoring 预约标志保证与同步批次互斥,不存在"已关自动同步但恢复被拒"的部分完成。
func (s *ModelSyncService) RestoreBuiltinPricing() (ModelSyncStatus, error) {
	s.mu.Lock()
	if s.running || s.restoring {
		status := s.coreStatusLocked()
		s.mu.Unlock()
		return s.decorateStatus(status), fmt.Errorf("同步正在进行,请稍后再试")
	}
	s.restoring = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.restoring = false
		s.mu.Unlock()
	}()

	if s.appSettings != nil {
		// 只改自动同步字段的原子读改写,不经 SaveAppSettings 的自启动副作用,
		// 避免自启动 OS 操作失败连带造成"已关自动同步但缓存未恢复"的部分完成
		if _, err := s.appSettings.mutateAppSettings(func(settings *AppSettings) {
			settings.AutoSyncModels = false
		}); err != nil {
			return s.GetSyncStatus(), fmt.Errorf("关闭自动同步失败: %w", err)
		}
	}

	s.mu.Lock()
	for _, id := range modelpricing.RemoteProviderIDs {
		_ = os.Remove(filepath.Join(s.dir, id+".json"))
	}
	_ = os.Remove(filepath.Join(s.dir, modelSyncStateFile))
	s.state = newModelSyncState()
	s.catalogs = make(map[string]*modelpricing.RemoteCatalog, len(s.seed))
	for id, catalog := range s.seed {
		s.catalogs[id] = catalog
		s.sources[id] = "seed"
		// 用种子价格作为 10x 跳变校验基线,后续首轮远程同步即受保护
		s.state.Providers[id] = &providerSyncState{LastPrices: extractBasePrices(catalog)}
	}
	s.mu.Unlock()

	s.rebuildPricing()
	s.emitUpdated()
	return s.GetSyncStatus(), nil
}

// —— 内部实现 ——

func (s *ModelSyncService) autoSyncEnabled() bool {
	if s.appSettings == nil {
		return true
	}
	settings, err := s.appSettings.GetAppSettings()
	if err != nil {
		return true
	}
	return settings.AutoSyncModels
}

func (s *ModelSyncService) maybeAutoSync() {
	if !s.autoSyncEnabled() {
		return
	}
	now := time.Now()
	s.mu.Lock()
	due := make([]string, 0, len(modelpricing.RemoteProviderIDs))
	for _, id := range modelpricing.RemoteProviderIDs {
		st := s.state.Providers[id]
		if st == nil || st.NextDue.IsZero() || !st.NextDue.After(now) {
			due = append(due, id)
		}
	}
	s.mu.Unlock()
	if len(due) == 0 {
		return
	}
	s.runSync(due)
}

type providerOutcome struct {
	id          string
	catalog     *modelpricing.RemoteCatalog
	body        []byte
	etag        string
	origin      string
	notModified bool
	err         error
}

func (s *ModelSyncService) runSync(targets []string) ModelSyncStatus {
	s.mu.Lock()
	if s.running || s.stopped || s.restoring {
		status := s.coreStatusLocked()
		s.mu.Unlock()
		return s.decorateStatus(status)
	}
	s.running = true
	s.syncWG.Add(1)
	etags := make(map[string]map[string]string, len(targets))
	prevCatalogs := make(map[string]*modelpricing.RemoteCatalog, len(targets))
	prevStates := make(map[string]providerSyncState, len(targets))
	for _, id := range targets {
		if st := s.state.Providers[id]; st != nil {
			etags[id] = st.ETagByOrigin
			prevStates[id] = *st
		}
		prevCatalogs[id] = s.catalogs[id]
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		s.syncWG.Done()
	}()

	ctx, cancel := context.WithTimeout(s.rootCtx, modelSyncBatchTimeout)
	defer cancel()

	sem := make(chan struct{}, modelSyncMaxConcurrent)
	outcomes := make([]providerOutcome, len(targets))
	var wg sync.WaitGroup
	for i, id := range targets {
		wg.Add(1)
		go func(idx int, providerID string) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				outcomes[idx] = providerOutcome{id: providerID, err: ctx.Err()}
				return
			}
			outcome := s.fetchProvider(ctx, providerID, etags[providerID])
			if outcome.err == nil && !outcome.notModified {
				prev := prevStates[providerID]
				catalog, err := s.validateBatch(providerID, outcome.body, &prev, prevCatalogs[providerID])
				if err != nil {
					outcome.err = err
				} else {
					outcome.catalog = catalog
				}
			}
			outcomes[idx] = outcome
		}(i, id)
	}
	wg.Wait()

	s.applyOutcomes(outcomes)
	return s.GetSyncStatus()
}

func (s *ModelSyncService) fetchProvider(ctx context.Context, providerID string, etagByOrigin map[string]string) providerOutcome {
	outcome := providerOutcome{id: providerID}
	var lastErr error
	for _, base := range s.baseURLs {
		if ctx.Err() != nil {
			outcome.err = ctx.Err()
			return outcome
		}
		etag := ""
		if etagByOrigin != nil {
			etag = etagByOrigin[base]
		}
		body, respETag, notModified, err := s.fetchFromOrigin(ctx, base, providerID, etag)
		if err != nil {
			lastErr = err
			continue
		}
		outcome.origin = base
		if notModified {
			outcome.notModified = true
			return outcome
		}
		outcome.body = body
		outcome.etag = respETag
		return outcome
	}
	outcome.err = lastErr
	if outcome.err == nil {
		outcome.err = fmt.Errorf("所有源均不可用")
	}
	return outcome
}

func (s *ModelSyncService) fetchFromOrigin(ctx context.Context, base, providerID, etag string) (body []byte, respETag string, notModified bool, err error) {
	url := base + "/api/providers/" + providerID + ".json"
	for attempt := 0; attempt < 2; attempt++ {
		reqCtx, cancel := context.WithTimeout(ctx, modelSyncRequestTimeout)
		body, respETag, notModified, err = s.doFetch(reqCtx, url, etag)
		cancel()
		if err == nil {
			return body, respETag, notModified, nil
		}
		// 仅对可恢复错误(429/5xx/网络错误)做一次短退避重试
		var httpErr *syncHTTPError
		retriable := true
		if ok := asSyncHTTPError(err, &httpErr); ok {
			retriable = httpErr.status == http.StatusTooManyRequests || httpErr.status >= 500
		}
		if !retriable || attempt == 1 || ctx.Err() != nil {
			return nil, "", false, err
		}
		select {
		case <-time.After(time.Duration(attempt+1) * 300 * time.Millisecond):
		case <-ctx.Done():
			return nil, "", false, ctx.Err()
		}
	}
	return nil, "", false, err
}

type syncHTTPError struct {
	status int
	url    string
}

func (e *syncHTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.status, e.url)
}

func asSyncHTTPError(err error, target **syncHTTPError) bool {
	if e, ok := err.(*syncHTTPError); ok {
		*target = e
		return true
	}
	return false
}

func (s *ModelSyncService) doFetch(ctx context.Context, url, etag string) ([]byte, string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", false, err
	}
	req.Header.Set("Accept", "application/json")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, "", false, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotModified:
		return nil, "", true, nil
	case resp.StatusCode == http.StatusOK:
		body, err := io.ReadAll(io.LimitReader(resp.Body, modelSyncMaxBody+1))
		if err != nil {
			return nil, "", false, err
		}
		if len(body) > modelSyncMaxBody {
			return nil, "", false, fmt.Errorf("响应超过大小上限 %d 字节: %s", modelSyncMaxBody, url)
		}
		return body, resp.Header.Get("ETag"), false, nil
	default:
		return nil, "", false, &syncHTTPError{status: resp.StatusCode, url: url}
	}
}

// validateBatch 解析并执行批次级校验:canary、高水位缩量、已知模型价格跳变、家族回归。
func (s *ModelSyncService) validateBatch(providerID string, body []byte, prev *providerSyncState, prevCatalog *modelpricing.RemoteCatalog) (*modelpricing.RemoteCatalog, error) {
	catalog, err := modelpricing.ParseRemoteCatalog(providerID, body)
	if err != nil {
		return nil, err
	}

	if pattern := providerCanaryPatterns[providerID]; pattern != nil {
		found := false
		for id, model := range catalog.Models {
			if pattern.MatchString(id) && model.Cost != nil &&
				((model.Cost.Input != nil && *model.Cost.Input > 0) || (model.Cost.Output != nil && *model.Cost.Output > 0)) {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%s 批次缺少标志性模型族(canary),疑似数据异常", providerID)
		}
	}

	newCount := len(catalog.Models)
	if prev != nil && prev.HighWater > 0 && time.Since(prev.HighWaterAt) <= modelSyncHighWaterWindow {
		floor := int(math.Ceil(float64(prev.HighWater) * 0.5))
		if newCount < floor {
			return nil, fmt.Errorf("%s 模型数骤降(%d < 30 天高水位 %d 的一半),批次已隔离", providerID, newCount, prev.HighWater)
		}
	}

	if prev != nil && len(prev.LastPrices) > 0 {
		for id, model := range catalog.Models {
			old, ok := prev.LastPrices[id]
			if !ok || model.Cost == nil {
				continue
			}
			if model.Cost.Input != nil && old[0] > 0 && *model.Cost.Input > 0 && *model.Cost.Input > old[0]*modelSyncMaxPriceJump {
				return nil, fmt.Errorf("%s/%s input 价格跳变超 %.0f 倍(%.4f -> %.4f),批次已隔离", providerID, id, modelSyncMaxPriceJump, old[0], *model.Cost.Input)
			}
			if model.Cost.Output != nil && old[1] > 0 && *model.Cost.Output > 0 && *model.Cost.Output > old[1]*modelSyncMaxPriceJump {
				return nil, fmt.Errorf("%s/%s output 价格跳变超 %.0f 倍(%.4f -> %.4f),批次已隔离", providerID, id, modelSyncMaxPriceJump, old[1], *model.Cost.Output)
			}
		}
	}

	if prevCatalog != nil {
		for _, guard := range providerFamilyGuards[providerID] {
			if catalogHasFamily(prevCatalog, guard) && !catalogHasFamily(catalog, guard) {
				return nil, fmt.Errorf("%s 批次缺失既有模型家族 %s,疑似回归,批次已隔离", providerID, guard.String())
			}
		}
	}

	return catalog, nil
}

func catalogHasFamily(catalog *modelpricing.RemoteCatalog, pattern *regexp.Regexp) bool {
	for id := range catalog.Models {
		if pattern.MatchString(id) {
			return true
		}
	}
	return false
}

// applyOutcomes 落盘、更新状态,有变更时重建定价快照并广播。
// 磁盘写入全部放在锁外(慢盘/杀软扫描时不阻塞状态读取),锁内只发布内存状态。
func (s *ModelSyncService) applyOutcomes(outcomes []providerOutcome) {
	now := time.Now()

	// 第一阶段(无锁):写各厂商缓存文件
	writeErrs := make([]error, len(outcomes))
	for i := range outcomes {
		o := &outcomes[i]
		if o.id == "" || o.err != nil || o.notModified {
			continue
		}
		writeErrs[i] = atomicWriteFile(filepath.Join(s.dir, o.id+".json"), o.body, 0o644)
	}

	changed := false
	allOK := true

	// 第二阶段(锁内):发布内存状态
	s.mu.Lock()
	for i, outcome := range outcomes {
		if outcome.id == "" {
			continue
		}
		st := s.state.Providers[outcome.id]
		if st == nil {
			st = &providerSyncState{}
			s.state.Providers[outcome.id] = st
		}
		st.CheckedAt = now

		switch {
		case outcome.err != nil:
			allOK = false
			st.FailStreak++
			st.LastError = outcome.err.Error()
			st.NextDue = now.Add(failBackoff(st.FailStreak))
			log.Printf("[ModelSync] %s 同步失败: %v", outcome.id, outcome.err)

		case outcome.notModified:
			st.FailStreak = 0
			st.LastError = ""
			st.NextDue = now.Add(nextSyncDelay())

		case writeErrs[i] != nil:
			allOK = false
			st.FailStreak++
			st.LastError = fmt.Sprintf("写入缓存失败: %v", writeErrs[i])
			st.NextDue = now.Add(modelSyncFailBackoffBase)
			log.Printf("[ModelSync] %s 写入缓存失败: %v", outcome.id, writeErrs[i])

		default:
			sum := sha256.Sum256(outcome.body)
			st.SHA256 = hex.EncodeToString(sum[:])
			if st.ETagByOrigin == nil {
				st.ETagByOrigin = make(map[string]string, 2)
			}
			st.ETagByOrigin[outcome.origin] = outcome.etag
			st.FetchedAt = now
			st.ModelCount = len(outcome.catalog.Models)
			if st.ModelCount > st.HighWater || now.Sub(st.HighWaterAt) > modelSyncHighWaterWindow {
				st.HighWater = st.ModelCount
				st.HighWaterAt = now
			}
			st.LastPrices = extractBasePrices(outcome.catalog)
			st.FailStreak = 0
			st.LastError = ""
			st.NextDue = now.Add(nextSyncDelay())

			s.catalogs[outcome.id] = outcome.catalog
			s.sources[outcome.id] = "remote"
			changed = true
			log.Printf("[ModelSync] %s 同步成功: %d 个模型", outcome.id, st.ModelCount)
		}
	}
	if changed {
		s.state.Generation++
	}
	if allOK && len(outcomes) > 0 {
		s.state.LastSuccess = now
	}
	stateData, marshalErr := json.MarshalIndent(s.state, "", "  ")
	s.mu.Unlock()

	// 第三阶段(无锁):写状态文件、重建定价、广播
	// runSync 单飞保证同一时刻只有一个批次走到这里,状态文件不会交错写
	if marshalErr != nil {
		log.Printf("[ModelSync] 序列化状态失败: %v", marshalErr)
	} else if err := atomicWriteFile(s.statePath(), stateData, 0o644); err != nil {
		log.Printf("[ModelSync] 写入状态失败: %v", err)
	}
	if changed {
		s.rebuildPricing()
	}
	s.emitUpdated()
}

// failBackoff 失败退避:1h 起步指数增长,封顶 24h;显式限制指数防位移溢出。
func failBackoff(streak int) time.Duration {
	shift := streak - 1
	if shift < 0 {
		shift = 0
	}
	if shift > 5 {
		shift = 5
	}
	backoff := modelSyncFailBackoffBase << shift
	if backoff > modelSyncFailBackoffCap {
		backoff = modelSyncFailBackoffCap
	}
	return backoff
}

// nextSyncDelay 24h ± 随机 jitter,打散各客户端请求时刻。
func nextSyncDelay() time.Duration {
	return modelSyncInterval + time.Duration(rand.Int63n(int64(modelSyncJitterMax)))
}

func extractBasePrices(catalog *modelpricing.RemoteCatalog) map[string][2]float64 {
	out := make(map[string][2]float64, len(catalog.Models))
	for id, model := range catalog.Models {
		if model.Cost == nil {
			continue
		}
		var in, outPrice float64
		if model.Cost.Input != nil {
			in = *model.Cost.Input
		}
		if model.Cost.Output != nil {
			outPrice = *model.Cost.Output
		}
		out[id] = [2]float64{in, outPrice}
	}
	return out
}

func (s *ModelSyncService) rebuildPricing() {
	if s.pricing == nil {
		return
	}
	stats, err := s.pricing.Rebuild(modelpricing.ConvertCatalogs(s.Catalogs()))
	if err != nil {
		log.Printf("[ModelSync] 定价快照重建失败(保留旧快照): %v", err)
		return
	}
	log.Printf("[ModelSync] 定价快照已更新: 共 %d 条,新增 %d,更新 %d,保留本地扩展 %d,冲突拒收 %d",
		stats.TotalModels, stats.RemoteAdded, stats.RemoteUpdated, stats.KeptComplex, stats.DroppedConflict)
}

func (s *ModelSyncService) emitUpdated() {
	s.mu.Lock()
	app := s.app
	s.mu.Unlock()
	if app != nil {
		app.Event.Emit("model-sync:updated", nil)
	}
}

// coreStatusLocked 只读取 s.mu 保护的内存状态。
// 【禁止】在此调用 policy/pricing/appSettings:policy 会经 Catalogs() 重入 s.mu 造成死锁,
// 外部信息统一由锁外的 decorateStatus 补齐。
func (s *ModelSyncService) coreStatusLocked() ModelSyncStatus {
	status := ModelSyncStatus{
		Running:     s.running,
		Generation:  s.state.Generation,
		LastSuccess: s.state.LastSuccess,
	}
	for _, id := range modelpricing.RemoteProviderIDs {
		entry := ModelSyncProviderStatus{Provider: id, Source: s.sources[id]}
		if entry.Source == "" {
			entry.Source = "seed"
		}
		if catalog := s.catalogs[id]; catalog != nil {
			entry.ModelCount = len(catalog.Models)
		}
		if st := s.state.Providers[id]; st != nil {
			entry.FetchedAt = st.FetchedAt
			entry.CheckedAt = st.CheckedAt
			entry.NextDue = st.NextDue
			entry.LastError = st.LastError
		}
		status.Providers = append(status.Providers, entry)
	}
	return status
}

// decorateStatus 在锁外补齐外部信息(设置开关、定价统计、默认模型解析)。
func (s *ModelSyncService) decorateStatus(status ModelSyncStatus) ModelSyncStatus {
	status.AutoSyncEnabled = s.autoSyncEnabled()
	if s.pricing != nil {
		status.Pricing = s.pricing.Stats()
	}
	if s.policy != nil {
		status.DefaultModels = s.policy.DefaultModels()
	}
	return status
}

func (s *ModelSyncService) statePath() string {
	return filepath.Join(s.dir, modelSyncStateFile)
}

func (s *ModelSyncService) loadState() {
	data, err := os.ReadFile(s.statePath())
	if err != nil {
		return
	}
	var state modelSyncState
	if err := json.Unmarshal(data, &state); err != nil {
		log.Printf("[ModelSync] 状态文件损坏,重置: %v", err)
		return
	}
	if state.Providers == nil {
		state.Providers = make(map[string]*providerSyncState)
	}
	s.state = &state
}

// loadLocalCatalogs 逐厂商载入本地缓存:哈希与状态一致且可解析才生效,
// 否则清除条件请求缓存(下轮强制无条件拉取,含文件被删的情况)并回退内置种子。
func (s *ModelSyncService) loadLocalCatalogs() {
	for _, id := range modelpricing.RemoteProviderIDs {
		st := s.state.Providers[id]
		path := filepath.Join(s.dir, id+".json")

		loaded := false
		if data, err := os.ReadFile(path); err == nil && st != nil && st.SHA256 != "" {
			sum := sha256.Sum256(data)
			if hex.EncodeToString(sum[:]) == st.SHA256 {
				if catalog, perr := modelpricing.ParseRemoteCatalog(id, data); perr == nil {
					s.catalogs[id] = catalog
					s.sources[id] = "remote"
					loaded = true
				}
			}
		}

		if !loaded {
			// 本地 body 不可用(缺失/被改/不可解析):必须废弃条件请求凭据,
			// 否则服务端持续 304 会让应用永远拿不到 body、卡在种子数据上
			if st != nil && (st.SHA256 != "" || len(st.ETagByOrigin) > 0) {
				st.ETagByOrigin = nil
				st.SHA256 = ""
				st.NextDue = time.Time{}
				log.Printf("[ModelSync] %s 本地缓存缺失或校验失败,已废弃缓存凭据,回退内置种子", id)
			}
			if seedCatalog, ok := s.seed[id]; ok {
				s.catalogs[id] = seedCatalog
				s.sources[id] = "seed"
			}
		}

		// LastPrices 基线:为空时用当前生效目录(远程缓存或种子)初始化,
		// 让 10x 价格跳变校验从首轮远程同步就生效
		if catalog := s.catalogs[id]; catalog != nil {
			if st == nil {
				st = &providerSyncState{}
				s.state.Providers[id] = st
			}
			if len(st.LastPrices) == 0 {
				st.LastPrices = extractBasePrices(catalog)
			}
		}
	}
}
