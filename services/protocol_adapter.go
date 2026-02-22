package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

// ========== 错误定义 ==========

// ErrClientRequestRejected 客户端请求被拒绝（不支持的格式/功能）
// 该错误会导致直接返回 400，不触发 provider 切换和拉黑
var ErrClientRequestRejected = errors.New("client request rejected")

// NewClientRequestRejectedError 创建带原因的客户端请求拒绝错误
func NewClientRequestRejectedError(reason string) error {
	return fmt.Errorf("%w: %s", ErrClientRequestRejected, reason)
}

// ========== 请求转换选项和结果 ==========

// ConvertOptions 请求转换选项
type ConvertOptions struct {
	IncludeUsage bool // 是否注入 stream_options.include_usage
}

// DefaultConvertOptions 默认转换选项
func DefaultConvertOptions() ConvertOptions {
	return ConvertOptions{
		IncludeUsage: true,
	}
}

// ConvertInfo 请求转换结果信息
type ConvertInfo struct {
	DroppedMetadataKeys []string // 被丢弃的 metadata 键
	MappedUser          string   // 映射到 OpenAI user 字段的值
	InjectedStreamOpts  bool     // 是否注入了 stream_options
	DroppedFields       []string // 被丢弃的顶层字段
}

// ========== 请求转换：Anthropic → OpenAI ==========

// ConvertAnthropicToOpenAI 将 Anthropic Messages 请求转换为 OpenAI Chat Completions 请求
// 第一期限制：
// - 只支持文本内容（不支持 tools、多模态）
// - 只支持 stream=true
func ConvertAnthropicToOpenAI(body []byte, opts ConvertOptions) ([]byte, ConvertInfo, error) {
	info := ConvertInfo{}

	// 解析 Anthropic 请求
	parsed := gjson.ParseBytes(body)

	// ========== 前置校验 ==========

	// 检查 stream（第一期只支持流式）
	streamVal := parsed.Get("stream")
	if !streamVal.Exists() || !streamVal.Bool() {
		return nil, info, NewClientRequestRejectedError("第一期仅支持 stream=true 的请求")
	}

	// 检查 tools（第一期不支持）
	if parsed.Get("tools").Exists() && parsed.Get("tools").IsArray() && len(parsed.Get("tools").Array()) > 0 {
		return nil, info, NewClientRequestRejectedError("第一期不支持 tools 功能")
	}
	if parsed.Get("tool_choice").Exists() {
		return nil, info, NewClientRequestRejectedError("第一期不支持 tool_choice 功能")
	}

	// ========== 构建 OpenAI 请求 ==========

	openAIReq := make(map[string]interface{})

	// model（直接使用，已经过 ModelMapping 处理）
	if model := parsed.Get("model").String(); model != "" {
		openAIReq["model"] = model
	}

	// max_tokens
	if maxTokens := parsed.Get("max_tokens"); maxTokens.Exists() {
		openAIReq["max_tokens"] = maxTokens.Int()
	}

	// stream
	openAIReq["stream"] = true

	// stream_options（用于获取 usage）
	if opts.IncludeUsage {
		openAIReq["stream_options"] = map[string]interface{}{
			"include_usage": true,
		}
		info.InjectedStreamOpts = true
	}

	// temperature
	if temp := parsed.Get("temperature"); temp.Exists() {
		openAIReq["temperature"] = temp.Float()
	}

	// top_p
	if topP := parsed.Get("top_p"); topP.Exists() {
		openAIReq["top_p"] = topP.Float()
	}

	// stop_sequences → stop
	if stopSeqs := parsed.Get("stop_sequences"); stopSeqs.Exists() && stopSeqs.IsArray() {
		stops := make([]string, 0)
		for _, s := range stopSeqs.Array() {
			stops = append(stops, s.String())
		}
		if len(stops) > 0 {
			openAIReq["stop"] = stops
		}
	}

	// metadata.user_id → user
	if userId := parsed.Get("metadata.user_id"); userId.Exists() && userId.String() != "" {
		openAIReq["user"] = userId.String()
		info.MappedUser = userId.String()
	}

	// 记录被丢弃的 metadata 键
	if metadata := parsed.Get("metadata"); metadata.Exists() && metadata.IsObject() {
		metadata.ForEach(func(key, value gjson.Result) bool {
			if key.String() != "user_id" {
				info.DroppedMetadataKeys = append(info.DroppedMetadataKeys, key.String())
			}
			return true
		})
	}

	// 记录被丢弃的顶层字段
	droppedTopLevel := []string{"betas", "anthropic_version"}
	for _, field := range droppedTopLevel {
		if parsed.Get(field).Exists() {
			info.DroppedFields = append(info.DroppedFields, field)
		}
	}

	// ========== 转换 messages ==========

	messages := make([]map[string]interface{}, 0)

	// system → 转为第一条 system 消息
	if system := parsed.Get("system"); system.Exists() {
		systemText, err := extractTextContent(system)
		if err != nil {
			return nil, info, err
		}
		if systemText != "" {
			messages = append(messages, map[string]interface{}{
				"role":    "system",
				"content": systemText,
			})
		}
	}

	// messages[]
	if msgArray := parsed.Get("messages"); msgArray.Exists() && msgArray.IsArray() {
		for i, msg := range msgArray.Array() {
			role := msg.Get("role").String()

			// 校验 role（第一期只支持 user/assistant）
			if role != "user" && role != "assistant" {
				return nil, info, NewClientRequestRejectedError(
					fmt.Sprintf("messages[%d].role='%s' 不支持，第一期仅支持 user/assistant", i, role))
			}

			content := msg.Get("content")
			contentText, err := extractTextContent(content)
			if err != nil {
				return nil, info, fmt.Errorf("messages[%d].content: %w", i, err)
			}

			messages = append(messages, map[string]interface{}{
				"role":    role,
				"content": contentText,
			})
		}
	}

	openAIReq["messages"] = messages

	// 序列化
	result, err := json.Marshal(openAIReq)
	if err != nil {
		return nil, info, fmt.Errorf("序列化 OpenAI 请求失败: %w", err)
	}

	return result, info, nil
}

// extractTextContent 从 Anthropic content 字段提取纯文本
// content 可能是 string 或 [{type:"text",text:"..."},...] 数组
func extractTextContent(content gjson.Result) (string, error) {
	if !content.Exists() {
		return "", nil
	}

	// 字符串形式
	if content.Type == gjson.String {
		return content.String(), nil
	}

	// 数组形式
	if content.IsArray() {
		var texts []string
		for i, block := range content.Array() {
			blockType := block.Get("type").String()
			if blockType != "text" {
				return "", NewClientRequestRejectedError(
					fmt.Sprintf("content[%d].type='%s' 不支持，第一期仅支持 text 类型", i, blockType))
			}
			texts = append(texts, block.Get("text").String())
		}
		return strings.Join(texts, "\n"), nil
	}

	return "", NewClientRequestRejectedError("content 格式无效，必须是 string 或 text block 数组")
}

// ========== SSE 转换状态机：OpenAI → Anthropic ==========

// OpenAIToAnthropicSSEConverter OpenAI SSE 到 Anthropic SSE 的转换器
// 设计为支持逐行输入（适配 xrequest 的 hook 行为）
type OpenAIToAnthropicSSEConverter struct {
	messageID           string // Anthropic message ID
	model               string // 模型名（用于 message_start）
	startedMessage      bool   // 是否已输出 message_start
	startedContentBlock bool   // 是否已输出 content_block_start
	stopped             bool   // 是否已输出 message_stop
	finishReason        string // 捕获的 finish_reason
	inputTokens         int64  // 捕获的 input tokens
	outputTokens        int64  // 捕获的 output tokens
	usageCaptured       bool   // 是否已捕获 usage
}

// NewOpenAIToAnthropicSSEConverter 创建新的 SSE 转换器
func NewOpenAIToAnthropicSSEConverter(model string) *OpenAIToAnthropicSSEConverter {
	return &OpenAIToAnthropicSSEConverter{
		messageID: "msg_" + uuid.New().String()[:24],
		model:     model,
	}
}

// ProcessLine 处理单行输入（xrequest 的 hook 是逐行回调）
// 返回转换后的 Anthropic SSE 事件（可能为空，表示无输出）
func (c *OpenAIToAnthropicSSEConverter) ProcessLine(line string) string {
	if c.stopped {
		return "" // 已结束，忽略后续数据
	}

	line = strings.TrimSpace(line)

	// 跳过空行和非 data: 行
	if line == "" || !strings.HasPrefix(line, "data:") {
		return ""
	}

	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

	// 检查 [DONE]
	if data == "[DONE]" {
		return c.outputStopEvents()
	}

	// 解析 JSON
	parsed := gjson.Parse(data)

	// 提取 id 和 model（如果有）
	if id := parsed.Get("id").String(); id != "" && c.messageID == "" {
		c.messageID = id
	}
	if model := parsed.Get("model").String(); model != "" && c.model == "" {
		c.model = model
	}

	// 提取 usage（stream_options.include_usage 开启时）
	if usage := parsed.Get("usage"); usage.Exists() && !c.usageCaptured {
		c.inputTokens = usage.Get("prompt_tokens").Int()
		c.outputTokens = usage.Get("completion_tokens").Int()
		c.usageCaptured = true
	}

	// 处理 choices[0]
	choice := parsed.Get("choices.0")
	if !choice.Exists() {
		return "" // 无 choices，跳过
	}

	// 提取 finish_reason
	if fr := choice.Get("finish_reason"); fr.Exists() && fr.String() != "" {
		c.finishReason = fr.String()
	}
	if fr := choice.Get("delta.finish_reason"); fr.Exists() && fr.String() != "" {
		c.finishReason = fr.String()
	}

	// 提取 content delta
	contentDelta := choice.Get("delta.content").String()
	// 兼容：有些上游用 message.content
	if contentDelta == "" {
		contentDelta = choice.Get("message.content").String()
	}

	if contentDelta != "" {
		return c.outputContentDelta(contentDelta)
	}

	return ""
}

// outputContentDelta 输出内容增量事件
func (c *OpenAIToAnthropicSSEConverter) outputContentDelta(text string) string {
	var output strings.Builder

	// 首次输出：先发送 message_start 和 content_block_start
	if !c.startedMessage {
		output.WriteString(c.outputMessageStart())
		c.startedMessage = true
	}
	if !c.startedContentBlock {
		output.WriteString(c.outputContentBlockStart())
		c.startedContentBlock = true
	}

	// 输出 content_block_delta
	delta := map[string]interface{}{
		"type":  "content_block_delta",
		"index": 0,
		"delta": map[string]interface{}{
			"type": "text_delta",
			"text": text,
		},
	}
	deltaJSON, _ := json.Marshal(delta)
	output.WriteString(fmt.Sprintf("data: %s\n\n", string(deltaJSON)))

	return output.String()
}

// outputMessageStart 输出 message_start 事件
func (c *OpenAIToAnthropicSSEConverter) outputMessageStart() string {
	msg := map[string]interface{}{
		"type": "message_start",
		"message": map[string]interface{}{
			"id":            c.messageID,
			"type":          "message",
			"role":          "assistant",
			"model":         c.model,
			"content":       []interface{}{},
			"stop_reason":   nil,
			"stop_sequence": nil,
			"usage": map[string]interface{}{
				"input_tokens":  0,
				"output_tokens": 0,
			},
		},
	}
	msgJSON, _ := json.Marshal(msg)
	return fmt.Sprintf("data: %s\n\n", string(msgJSON))
}

// outputContentBlockStart 输出 content_block_start 事件
func (c *OpenAIToAnthropicSSEConverter) outputContentBlockStart() string {
	block := map[string]interface{}{
		"type":  "content_block_start",
		"index": 0,
		"content_block": map[string]interface{}{
			"type": "text",
			"text": "",
		},
	}
	blockJSON, _ := json.Marshal(block)
	return fmt.Sprintf("data: %s\n\n", string(blockJSON))
}

// outputStopEvents 输出停止事件序列
func (c *OpenAIToAnthropicSSEConverter) outputStopEvents() string {
	if c.stopped {
		return ""
	}
	c.stopped = true

	var output strings.Builder

	// 确保已发送 start 事件
	if !c.startedMessage {
		output.WriteString(c.outputMessageStart())
		c.startedMessage = true
	}
	if !c.startedContentBlock {
		output.WriteString(c.outputContentBlockStart())
		c.startedContentBlock = true
	}

	// content_block_stop
	blockStop := map[string]interface{}{
		"type":  "content_block_stop",
		"index": 0,
	}
	blockStopJSON, _ := json.Marshal(blockStop)
	output.WriteString(fmt.Sprintf("data: %s\n\n", string(blockStopJSON)))

	// message_delta（包含 stop_reason 和 usage）
	stopReason := c.mapFinishReason(c.finishReason)
	msgDelta := map[string]interface{}{
		"type": "message_delta",
		"delta": map[string]interface{}{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]interface{}{
			"input_tokens":  c.inputTokens,
			"output_tokens": c.outputTokens,
		},
	}
	msgDeltaJSON, _ := json.Marshal(msgDelta)
	output.WriteString(fmt.Sprintf("data: %s\n\n", string(msgDeltaJSON)))

	// message_stop
	msgStop := map[string]interface{}{
		"type": "message_stop",
	}
	msgStopJSON, _ := json.Marshal(msgStop)
	output.WriteString(fmt.Sprintf("data: %s\n\n", string(msgStopJSON)))

	return output.String()
}

// mapFinishReason 映射 OpenAI finish_reason 到 Anthropic stop_reason
func (c *OpenAIToAnthropicSSEConverter) mapFinishReason(finishReason string) string {
	switch finishReason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "content_filter":
		return "end_turn"
	case "tool_calls", "function_call":
		return "tool_use"
	default:
		return "end_turn"
	}
}

// GetUsage 获取捕获的 usage 信息
func (c *OpenAIToAnthropicSSEConverter) GetUsage() (inputTokens, outputTokens int64) {
	return c.inputTokens, c.outputTokens
}

// ========== 辅助函数 ==========

// GenerateAnthropicMessageID 生成 Anthropic 风格的 message ID
func GenerateAnthropicMessageID() string {
	return fmt.Sprintf("msg_%s", uuid.New().String()[:24])
}

// GenerateAnthropicTimestamp 生成 Anthropic 风格的时间戳
func GenerateAnthropicTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}
