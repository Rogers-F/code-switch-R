package services

import (
	"fmt"
)

// RequestDetailService 请求详情服务（供前端调用）
type RequestDetailService struct{}

// NewRequestDetailService 创建请求详情服务
func NewRequestDetailService() *RequestDetailService {
	// 确保全局缓存已初始化
	if GlobalRequestDetailCache == nil {
		InitRequestDetailCache()
	}
	return &RequestDetailService{}
}

// SetRecordMode 设置记录模式
// mode: "off" | "fail_only" | "all"
func (s *RequestDetailService) SetRecordMode(mode string) error {
	if GlobalRequestDetailCache == nil {
		return fmt.Errorf("request detail cache not initialized")
	}
	
	var m RequestDetailMode
	switch mode {
	case "off":
		m = RequestDetailModeOff
	case "fail_only":
		m = RequestDetailModeFailOnly
	case "all":
		m = RequestDetailModeAll
	default:
		return fmt.Errorf("invalid mode: %s, expected: off, fail_only, all", mode)
	}
	
	GlobalRequestDetailCache.SetMode(m)
	fmt.Printf("[RequestDetail] 记录模式已设置为: %s\n", mode)
	return nil
}

// GetRecordMode 获取当前记录模式
func (s *RequestDetailService) GetRecordMode() string {
	if GlobalRequestDetailCache == nil {
		return "off"
	}
	return string(GlobalRequestDetailCache.GetMode())
}

// GetDetail 根据序号获取请求详情
func (s *RequestDetailService) GetDetail(sequenceID int64) *RequestDetail {
	if GlobalRequestDetailCache == nil {
		return nil
	}
	return GlobalRequestDetailCache.Get(sequenceID)
}

// GetRecentDetails 获取最近的请求详情列表
func (s *RequestDetailService) GetRecentDetails(limit int) []*RequestDetail {
	if GlobalRequestDetailCache == nil {
		return []*RequestDetail{}
	}
	if limit <= 0 {
		limit = 20
	}
	return GlobalRequestDetailCache.GetRecent(limit)
}

// GetAvailableIDs 获取当前缓存中所有可用的 sequence_id
func (s *RequestDetailService) GetAvailableIDs() []int64 {
	if GlobalRequestDetailCache == nil {
		return []int64{}
	}
	return GlobalRequestDetailCache.GetAvailableIDs()
}

// GetCacheStats 获取缓存统计信息
func (s *RequestDetailService) GetCacheStats() CacheStats {
	if GlobalRequestDetailCache == nil {
		return CacheStats{Mode: RequestDetailModeOff}
	}
	return GlobalRequestDetailCache.Stats()
}

// ClearCache 清空缓存
func (s *RequestDetailService) ClearCache() {
	if GlobalRequestDetailCache != nil {
		GlobalRequestDetailCache.Clear()
		fmt.Println("[RequestDetail] 缓存已清空")
	}
}
