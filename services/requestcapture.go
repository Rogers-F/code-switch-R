package services

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"sync/atomic"
	"unicode/utf8"
)

// ========== 抓包采集（全量不脱敏）==========
//
// 语义：录制"终态供应商尝试、进入 HTTP transport 之前"的应用层出站请求
// （URL / 请求头 / 请求体，映射与认证注入后的最终形态）与上游响应（响应头 /
// 响应体）。录制开关为进程内状态、重启即关（调试态功能）。
//
// 【安全告警】不做任何脱敏：明文 API Key、完整提示词与响应内容都会原样落库。
// 这是用户显式选择的调试模式，导出文件切勿分享。数据库删除仅为逻辑删除，
// 磁盘回收依赖 ClearCapturedRequests 的 VACUUM。

const (
	// captureFieldLimit 单个字段（请求体/响应体）落库上限。超出即截断并置标记，
	// 但计数与转发不受影响。真实提示词/响应远小于此，该上限只是 OOM 兜底
	captureFieldLimit = 50 * 1024 * 1024
	// captureInflightBudget 进程内在途“响应缓冲累积”的总预算。多路大响应并发时，
	// 累计占用触顶后新增响应内容不再暂存（置 budget_skipped），仍照常转发。
	// 注意：这是响应流累积的软上限（主要 OOM 风险），不计入已在内存中的请求体
	// 与落库前的字符串副本，因此不是进程总内存的硬保证
	captureInflightBudget = 128 * 1024 * 1024
	// captureDetailPreviewLimit 明细接口每字段返回的预览上限。整段 50MiB 直接
	// 塞进 <pre> 会冻死 webview，完整内容只能走导出
	captureDetailPreviewLimit = 256 * 1024
)

// rawRequestHeaders 序列化出站请求头（键排序 JSON 对象），不脱敏。
// 请求头由本程序构造，体量受 HTTP 实践约束（KB 级），不设截断
func rawRequestHeaders(headers map[string]string) string {
	if len(headers) == 0 {
		return ""
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make([][2]string, 0, len(keys))
	for _, k := range keys {
		ordered = append(ordered, [2]string{k, headers[k]})
	}
	// 用有序切片保证键序稳定；json.Marshal(map) 也排序，但显式更清晰
	obj := make(map[string]string, len(headers))
	for _, kv := range ordered {
		obj[kv[0]] = kv[1]
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return ""
	}
	return string(data)
}

// rawResponseHeaders 序列化上游响应头（JSON 对象，值为数组），保留重复头
// （尤其 Set-Cookie）。响应头体量受 Go http.Transport 的 MaxResponseHeaderBytes
// 约束（默认 10MB），不再单独截断
func rawResponseHeaders(header http.Header) string {
	if len(header) == 0 {
		return ""
	}
	obj := make(map[string][]string, len(header))
	for k, v := range header {
		vs := make([]string, len(v))
		copy(vs, v)
		obj[k] = vs
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return ""
	}
	return string(data)
}

// rawHTTPHeaders 与 rawResponseHeaders 同构：把 http.Header 序列化为 JSON 对象
// （值为数组，保留多值），供 Gemini 请求头采集复用（不逗号合并）
func rawHTTPHeaders(header http.Header) string {
	return rawResponseHeaders(header)
}

// rawCaptureBody 把请求体原样转为存储文本（不脱敏），按 captureFieldLimit 截断。
// 返回 (存储文本, 是否截断, 原始字节数)。请求体已完整在内存，截断只影响落库量
func rawCaptureBody(body []byte) (string, bool, int) {
	total := len(body)
	if total == 0 {
		return "", false, 0
	}
	if total <= captureFieldLimit {
		return string(body), false, total
	}
	return string(truncateUTF8(string(body[:captureFieldLimit]), captureFieldLimit)), true, total
}

// captureBuffer 累积上游响应体，带每字段硬上限与进程级在途预算。
// 只在录制请求上创建；append 在转发协程内单线程调用（无需加锁）。
// 预算随缓冲增长增量占用、请求结束时整体释放
type captureBuffer struct {
	inflight      *atomic.Int64 // 指向 ProviderRelayService.captureInflightBytes
	buf           []byte
	total         int  // 上游实际产出的字节数（用于 response_bytes，即使未全存）
	reserved      int  // 已向全局预算占用的字节数
	truncated     bool // 触及 captureFieldLimit
	budgetSkipped bool // 触及全局预算，停止暂存
}

func newCaptureBuffer(inflight *atomic.Int64) *captureBuffer {
	return &captureBuffer{inflight: inflight}
}

// append 记录一段响应字节。始终累加 total；仅在未截断、未触预算时尝试暂存
func (cb *captureBuffer) append(p []byte) {
	if cb == nil || len(p) == 0 {
		return
	}
	cb.total += len(p)
	if cb.truncated || cb.budgetSkipped {
		return
	}
	room := captureFieldLimit - len(cb.buf)
	if room <= 0 {
		cb.truncated = true
		return
	}
	want := len(p)
	if want > room {
		want = room
		cb.truncated = true
	}
	// 增量占用全局预算；触顶则停止暂存（已存部分保留）
	if cb.inflight != nil {
		if cb.inflight.Add(int64(want)) > captureInflightBudget {
			cb.inflight.Add(int64(-want))
			cb.budgetSkipped = true
			return
		}
		cb.reserved += want
	}
	cb.buf = append(cb.buf, p[:want]...)
}

// release 归还占用的预算。请求结束时调用一次
func (cb *captureBuffer) release() {
	if cb == nil || cb.inflight == nil || cb.reserved == 0 {
		return
	}
	cb.inflight.Add(int64(-cb.reserved))
	cb.reserved = 0
}

// markTruncated 强制标记为截断（非"触及上限"，而是读取被中断，如 SSE 错误体
// 读取超时）。使详情/导出如实呈现"内容不完整"，不把空/残缺当成完整响应
func (cb *captureBuffer) markTruncated() {
	if cb != nil {
		cb.truncated = true
	}
}

// captureTeeReader 包装上游响应体：转发读取的同时把原始字节喂给 captureBuffer。
// 用于 Claude/Codex/custom 路径——xrequest 的逐行 hook 会剥掉行尾、跳过空行，
// 无法还原原始 SSE，必须在字节流层 tee
type captureTeeReader struct {
	src io.ReadCloser
	cb  *captureBuffer
}

func newCaptureTeeReader(src io.ReadCloser, cb *captureBuffer) *captureTeeReader {
	return &captureTeeReader{src: src, cb: cb}
}

func (t *captureTeeReader) Read(p []byte) (int, error) {
	n, err := t.src.Read(p)
	if n > 0 {
		t.cb.append(p[:n])
	}
	return n, err
}

func (t *captureTeeReader) Close() error {
	return t.src.Close()
}

// capturePreview 截取字段预览（明细接口用），返回 (预览文本, 是否截断)。
// 按 UTF-8 边界安全截断
func capturePreview(s string) (string, bool) {
	if len(s) <= captureDetailPreviewLimit {
		return s, false
	}
	return truncateUTF8(s, captureDetailPreviewLimit), true
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
