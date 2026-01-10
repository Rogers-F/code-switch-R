package services

import (
	"sync"
	"time"
)

// RequestDetailMode 记录详情的模式
type RequestDetailMode string

const (
	RequestDetailModeOff      RequestDetailMode = "off"       // 关闭
	RequestDetailModeFailOnly RequestDetailMode = "fail_only" // 仅失败请求
	RequestDetailModeAll      RequestDetailMode = "all"       // 全部请求
)

// RequestDetail 请求详情（内存缓存，不持久化）
type RequestDetail struct {
	SequenceID      int64             `json:"sequence_id"`      // 唯一序号（对应 request_log.id）
	Platform        string            `json:"platform"`         // 平台：claude/codex/gemini
	Provider        string            `json:"provider"`         // 供应商名称
	Model           string            `json:"model"`            // 模型名
	RequestURL      string            `json:"request_url"`      // 目标 URL
	RequestBody     string            `json:"request_body"`     // 请求体（可能截断）
	ResponseBody    string            `json:"response_body"`    // 响应体（可能截断/聚合）
	Headers         map[string]string `json:"headers"`          // 请求头（已脱敏）
	ResponseHeaders map[string]string `json:"response_headers"` // 响应头
	HttpCode        int               `json:"http_code"`        // HTTP 状态码
	Timestamp       time.Time         `json:"timestamp"`        // 请求时间
	DurationMs      int64             `json:"duration_ms"`      // 耗时（毫秒）
	Truncated       bool              `json:"truncated"`        // 是否被截断
	RequestSize     int               `json:"request_size"`     // 原始请求体大小
	ResponseSize    int               `json:"response_size"`    // 原始响应体大小
}

// RequestDetailCache 请求详情缓存服务
// 使用环形缓冲区存储最近 N 条请求的详情
type RequestDetailCache struct {
	mu         sync.RWMutex
	mode       RequestDetailMode
	buffer     []*RequestDetail // 环形缓冲区
	capacity   int              // 缓冲区容量
	head       int              // 写入位置
	count      int              // 当前数量
	seqToIndex map[int64]int    // sequence_id -> buffer index 映射
}

const (
	DefaultCacheCapacity  = 100        // 默认缓存 N 条
	MaxRequestBodySize    = 300 * 1024 // 请求体最大 300KB
	MaxResponseBodySize   = 300 * 1024 // 响应体最大 300KB
	MaxStreamResponseSize = 500 * 1024 // 流式响应最大 500KB
)

// NewRequestDetailCache 创建请求详情缓存服务
func NewRequestDetailCache(capacity int) *RequestDetailCache {
	if capacity <= 0 {
		capacity = DefaultCacheCapacity
	}
	return &RequestDetailCache{
		mode:       RequestDetailModeOff,
		buffer:     make([]*RequestDetail, capacity),
		capacity:   capacity,
		seqToIndex: make(map[int64]int),
	}
}

// SetMode 设置记录模式
func (c *RequestDetailCache) SetMode(mode RequestDetailMode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = mode
}

// GetMode 获取当前记录模式
func (c *RequestDetailCache) GetMode() RequestDetailMode {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mode
}

// ShouldRecord 判断是否应该记录该请求
func (c *RequestDetailCache) ShouldRecord(httpCode int) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	switch c.mode {
	case RequestDetailModeOff:
		return false
	case RequestDetailModeFailOnly:
		// 非 2xx 状态码都算失败
		return httpCode < 200 || httpCode >= 300
	case RequestDetailModeAll:
		return true
	default:
		return false
	}
}

// Store 存储请求详情
func (c *RequestDetailCache) Store(detail *RequestDetail) {
	if detail == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// 检查模式（双重检查）
	if c.mode == RequestDetailModeOff {
		return
	}

	// 如果旧位置有数据，删除映射
	if old := c.buffer[c.head]; old != nil {
		delete(c.seqToIndex, old.SequenceID)
	}

	// 存储新数据
	c.buffer[c.head] = detail
	c.seqToIndex[detail.SequenceID] = c.head

	// 更新指针
	c.head = (c.head + 1) % c.capacity
	if c.count < c.capacity {
		c.count++
	}
}

// Get 根据 sequence_id 获取详情
func (c *RequestDetailCache) Get(sequenceID int64) *RequestDetail {
	c.mu.RLock()
	defer c.mu.RUnlock()

	idx, ok := c.seqToIndex[sequenceID]
	if !ok {
		return nil
	}

	return c.buffer[idx]
}

// GetRecent 获取最近 N 条详情（按时间倒序）
func (c *RequestDetailCache) GetRecent(limit int) []*RequestDetail {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if limit <= 0 || c.count == 0 {
		return []*RequestDetail{}
	}

	if limit > c.count {
		limit = c.count
	}

	result := make([]*RequestDetail, 0, limit)

	// 从 head-1 开始倒序读取
	for i := 0; i < limit; i++ {
		idx := (c.head - 1 - i + c.capacity) % c.capacity
		if c.buffer[idx] != nil {
			result = append(result, c.buffer[idx])
		}
	}

	return result
}

// GetAvailableIDs 获取当前缓存中所有可用的 sequence_id 列表
func (c *RequestDetailCache) GetAvailableIDs() []int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	ids := make([]int64, 0, len(c.seqToIndex))
	for id := range c.seqToIndex {
		ids = append(ids, id)
	}
	return ids
}

// Clear 清空缓存
func (c *RequestDetailCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.buffer = make([]*RequestDetail, c.capacity)
	c.seqToIndex = make(map[int64]int)
	c.head = 0
	c.count = 0
}

// Stats 返回缓存统计信息
type CacheStats struct {
	Mode     RequestDetailMode `json:"mode"`
	Capacity int               `json:"capacity"`
	Count    int               `json:"count"`
}

func (c *RequestDetailCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CacheStats{
		Mode:     c.mode,
		Capacity: c.capacity,
		Count:    c.count,
	}
}

// TruncateBody 截断过大的内容
func TruncateBody(body string, maxSize int) (string, bool) {
	if len(body) <= maxSize {
		return body, false
	}
	return body[:maxSize], true
}

// SanitizeHeaders 脱敏请求头
func SanitizeHeaders(headers map[string]string) map[string]string {
	sanitized := make(map[string]string, len(headers))
	// 使用小写形式进行比较，兼容各种大小写形式的 header key
	sensitiveKeys := map[string]bool{
		"authorization":  true,
		"x-api-key":      true,
		"x-goog-api-key": true,
		"api-key":        true,
		"bearer":         true,
	}

	for k, v := range headers {
		lowerKey := toLower(k)
		if sensitiveKeys[lowerKey] {
			// 脱敏：只显示前 8 个字符
			if len(v) > 12 {
				sanitized[k] = v[:8] + "****" + v[len(v)-4:]
			} else if len(v) > 4 {
				sanitized[k] = v[:4] + "****"
			} else {
				sanitized[k] = "****"
			}
		} else {
			sanitized[k] = v
		}
	}
	return sanitized
}

// toLower 简单的小写转换（避免导入 strings）
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// GlobalRequestDetailCache 全局请求详情缓存实例
var GlobalRequestDetailCache *RequestDetailCache

// InitRequestDetailCache 初始化全局缓存
func InitRequestDetailCache() {
	GlobalRequestDetailCache = NewRequestDetailCache(DefaultCacheCapacity)
}
