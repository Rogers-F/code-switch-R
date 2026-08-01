package services

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"
)

// ========== 抓包模式（issue #5）==========
//
// 语义：录制"终态供应商尝试、进入 HTTP transport 之前"的应用层出站
// 请求头与正文（映射/清理/认证注入后的最终形态）。开关为进程内状态，
// 重启即关——这是调试态功能，不持久化避免用户遗忘后长期落敏感数据。
// 脱敏承诺的边界：已知认证信息（认证头与配置的 API Key 值、JSON 正文
// 中的敏感键）会被打码；正文其余内容原样保存，仍可能包含敏感信息。

const (
	// captureHeadersLimit 请求头序列化后的存储上限
	captureHeadersLimit = 8 * 1024
	// captureBodyLimit 请求正文的存储上限
	captureBodyLimit = 64 * 1024
	// captureParseLimit JSON 树式脱敏的解析上限。超大正文整树解析 + 重序列化
	// 会造成数倍内存放大，超过该阈值退化为"密钥值替换 + 截断"的降级路径
	captureParseLimit = 2 * 1024 * 1024
	// captureMaskValue 打码占位
	captureMaskValue = "***"
)

// captureSensitiveHeaders 固定敏感头名（小写）；自定义认证头名由调用方传入
var captureSensitiveHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"x-api-key":           true,
	"api-key":             true,
	"x-goog-api-key":      true,
	"cookie":              true,
}

// captureSensitiveSegments 正文敏感键的分段词表（小写）
var captureSensitiveSegments = map[string]bool{
	"key": true, "apikey": true, "token": true, "secret": true,
	"password": true, "passwd": true, "credential": true, "credentials": true,
	"auth": true, "authorization": true, "bearer": true, "cookie": true, "session": true,
}

// captureKeySegments 把键名切成小写分段：非字母数字是边界，camelCase 的
// 小写→大写转折也是边界（accessToken → access/token）
func captureKeySegments(name string) []string {
	segments := []string{}
	seg := strings.Builder{}
	prevLower := false
	for _, r := range name {
		isUpper := r >= 'A' && r <= 'Z'
		isLower := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !isUpper && !isLower {
			if seg.Len() > 0 {
				segments = append(segments, seg.String())
				seg.Reset()
			}
			prevLower = false
			continue
		}
		if isUpper && prevLower && seg.Len() > 0 {
			segments = append(segments, seg.String())
			seg.Reset()
		}
		if isUpper {
			r += 'a' - 'A'
		}
		seg.WriteRune(r)
		prevLower = isLower
	}
	if seg.Len() > 0 {
		segments = append(segments, seg.String())
	}
	return segments
}

// captureQuantityQualifiers "tokens" 前缀为这些分段时是数量语义（max_tokens、
// output_tokens、cache_read_tokens…），不按凭据处理；数字开头的分段
// （ephemeral_5m_tokens 的 "5m"）同样视为数量限定词
var captureQuantityQualifiers = map[string]bool{
	"max": true, "min": true, "num": true, "count": true, "total": true,
	"budget": true, "input": true, "output": true, "prompt": true,
	"completion": true, "thinking": true, "reasoning": true,
	"cache": true, "cached": true, "read": true, "write": true, "creation": true,
}

// captureDurationSegment 判断分段是否是时长/容量形状（5m/1h/200k…）：
// 全数字加至多一个单位字母。不能放宽成"数字开头"，否则 2fa_tokens
// 这类认证键会被误判为数量语义
func captureDurationSegment(s string) bool {
	if s == "" {
		return false
	}
	last := s[len(s)-1]
	digits := s
	switch last {
	case 's', 'm', 'h', 'd', 'k':
		digits = s[:len(s)-1]
	}
	if digits == "" {
		return false
	}
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return false
		}
	}
	return true
}

// captureSensitiveKey 判断正文键名是否敏感。与导出侧 sensitiveKeyName 的差异：
//  1. 额外切分 camelCase（accessToken/clientSecret 才能命中）；
//  2. "tokens" 仅在前一分段是数量限定词或时长形状时豁免（max_tokens/
//     ephemeral_5m_tokens 是数量，accessTokens/refresh_tokens/2fa_tokens
//     仍是凭据）；无限定词默认按凭据打码。
func captureSensitiveKey(name string) bool {
	segments := captureKeySegments(name)
	for i, s := range segments {
		if s == "tokens" {
			if i > 0 {
				prev := segments[i-1]
				if captureQuantityQualifiers[prev] || captureDurationSegment(prev) {
					continue
				}
			}
			return true
		}
		if captureSensitiveSegments[s] {
			return true
		}
		if strings.HasSuffix(s, "s") && captureSensitiveSegments[strings.TrimSuffix(s, "s")] {
			return true
		}
		// "api"+"key" 连段（api_key / api-key / apiKey 已被切开）
		if s == "api" && i+1 < len(segments) && captureSensitiveSegments[segments[i+1]] {
			return true
		}
	}
	return false
}

// maskCaptureHeaders 序列化出站请求头（键排序 JSON），打码敏感头。
// authHeaderName 是供应商配置的认证方式（可能是任意自定义头名），
// apiKey 是该供应商密钥——任何头的值包含它都会被整体打码，
// 兜住"密钥被写进非常规头"的场景。
func maskCaptureHeaders(headers map[string]string, authHeaderName, apiKey string) string {
	if len(headers) == 0 {
		return ""
	}
	customAuth := strings.ToLower(strings.TrimSpace(authHeaderName))

	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	masked := make(map[string]string, len(headers))
	for _, k := range keys {
		v := headers[k]
		lower := strings.ToLower(k)
		switch {
		case captureSensitiveHeaders[lower]:
			v = captureMaskValue
		case customAuth != "" && lower == customAuth:
			v = captureMaskValue
		case apiKey != "" && strings.Contains(v, apiKey):
			v = captureMaskValue
		}
		// 单值预限长：总量超限时不能直接截断序列化字节（会产出非法 JSON）
		if len(v) > 2*1024 {
			v = truncateUTF8(v, 2*1024) + "…"
		}
		masked[k] = v
	}

	data, err := json.Marshal(masked)
	if err != nil {
		return ""
	}
	if len(data) > captureHeadersLimit {
		// 极端场景（海量头）：宁可放弃明细也要保证字段是合法 JSON
		return `{"_capture":"headers exceed size limit"}`
	}
	return string(data)
}

// redactCaptureBody 脱敏并限长正文。返回 (存储文本, 是否截断, 原始字节数)。
// 合法 JSON：递归打码敏感键（captureSensitiveKey，含 camelCase）并按值替换
// 已知 API Key；数字经 UseNumber 无损透传，避免大整数被 float64 改值。
// 非 JSON 或超过解析阈值：仅按值替换已知 Key。截断信息走元数据列，
// 不往原文里塞标记。
func redactCaptureBody(body []byte, apiKey string) (string, bool, int) {
	originalBytes := len(body)
	if originalBytes == 0 {
		return "", false, 0
	}

	text := ""
	truncated := false
	if originalBytes <= captureParseLimit {
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.UseNumber()
		var tree any
		if dec.Decode(&tree) == nil && !dec.More() {
			redactCaptureValue(&tree, false, apiKey)
			if data, err := json.Marshal(tree); err == nil {
				text = string(data)
				// 树遍历只处理值,密钥出现在字段名里时兜底按值替换
				if apiKey != "" {
					text = strings.ReplaceAll(text, apiKey, captureMaskValue)
				}
			}
		}
	}
	if text == "" {
		// 降级路径（非 JSON 或超过解析阈值）：只在有界前缀上工作，避免为超大
		// 正文再造整份字符串。前缀多留 len(apiKey)，让跨截断边界的密钥出现
		// 尽量完整落入前缀被整体替换
		prefix := body
		if len(prefix) > captureBodyLimit+len(apiKey) {
			prefix = prefix[:captureBodyLimit+len(apiKey)]
			truncated = true // 替换可能让文本缩回限额内，但内容已经丢了
		}
		text = string(prefix)
		if apiKey != "" {
			text = strings.ReplaceAll(text, apiKey, captureMaskValue)
		}
	}

	if len(text) > captureBodyLimit {
		// 截断产物是子串引用，底层仍攥着完整分配；克隆彻底放掉
		text = strings.Clone(truncateUTF8(text, captureBodyLimit))
		truncated = true
	}
	if truncated && apiKey != "" {
		// 前面的替换会左移后续内容,截断边界处未被整体替换的密钥残段可能
		// 被挪进最终文本;残段只可能出现在末尾(完整出现均已替换),按后缀刮除
		text = scrubKeyFragmentSuffix(text, apiKey)
	}
	return text, truncated, originalBytes
}

// scrubKeyFragmentSuffix 刮掉文本末尾与密钥前缀重合的残段，
// 保证截断永远不会持久化半截密钥
func scrubKeyFragmentSuffix(text, apiKey string) string {
	max := len(apiKey) - 1
	if max > len(text) {
		max = len(text)
	}
	for j := max; j >= 1; j-- {
		if strings.HasSuffix(text, apiKey[:j]) {
			return text[:len(text)-j]
		}
	}
	return text
}

// redactCaptureValue 递归脱敏 JSON 树：敏感键下的标量（字符串/数字/布尔）
// 一律打码，敏感父键的子树进入敏感上下文；任何字符串值包含已知 Key 也打码
func redactCaptureValue(v *any, sensitiveCtx bool, apiKey string) {
	switch val := (*v).(type) {
	case string:
		if val == "" {
			return
		}
		if sensitiveCtx || (apiKey != "" && strings.Contains(val, apiKey)) {
			*v = captureMaskValue
		}
	case json.Number, bool, float64:
		// 非字符串标量也可能是凭据（如纯数字密码）
		if sensitiveCtx {
			*v = captureMaskValue
		}
	case map[string]any:
		for k := range val {
			child := val[k]
			childSensitive := sensitiveCtx || captureSensitiveKey(k)
			redactCaptureValue(&child, childSensitive, apiKey)
			val[k] = child
		}
	case []any:
		for i := range val {
			child := val[i]
			redactCaptureValue(&child, sensitiveCtx, apiKey)
			val[i] = child
		}
	}
}

// truncateUTF8 在不打断多字节序列的前提下截断到至多 limit 字节
func truncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := s[:limit]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}
