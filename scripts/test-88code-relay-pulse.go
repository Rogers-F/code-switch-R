//go:build ignore

// +build ignore

package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 模拟 relay-pulse 的探测逻辑测试 88code
// Author: Half open flowers

func main() {
	apiKey := "88_784022b235595e84936fa42596b41bcad3dc24a79ced14a5d0d4489937ba36e8"

	fmt.Println("================================================================")
	fmt.Println("模拟 relay-pulse 探测逻辑测试 88code")
	fmt.Println("================================================================")

	tests := []struct {
		name           string
		url            string
		headers        map[string]string
		body           string
		successContains string
	}{
		{
			name: "原配置: m.88code.org + x-api-key + /v1/messages",
			url:  "https://m.88code.org/api/v1/messages",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
			},
			body:            `{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`,
			successContains: "content", // Anthropic 响应格式
		},
		{
			name: "relay-pulse 示例: api.88code.com + Bearer + /v1/chat/completions",
			url:  "https://api.88code.com/v1/chat/completions",
			headers: map[string]string{
				"Content-Type":  "application/json",
				"Authorization": "Bearer " + apiKey,
			},
			body:            `{"model":"claude-3-opus","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`,
			successContains: "choices", // OpenAI 响应格式
		},
		{
			name: "测试: m.88code.org + Bearer + /v1/messages",
			url:  "https://m.88code.org/api/v1/messages",
			headers: map[string]string{
				"Content-Type":      "application/json",
				"Authorization":     "Bearer " + apiKey,
				"anthropic-version": "2023-06-01",
			},
			body:            `{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`,
			successContains: "content",
		},
		{
			name: "测试: m.88code.org + x-api-key + 无 anthropic-version",
			url:  "https://m.88code.org/api/v1/messages",
			headers: map[string]string{
				"Content-Type": "application/json",
				"x-api-key":    apiKey,
			},
			body:            `{"model":"claude-haiku-4-5-20251001","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`,
			successContains: "content",
		},
	}

	client := &http.Client{Timeout: 30 * time.Second}

	for _, test := range tests {
		fmt.Printf("\n--- %s ---\n", test.name)
		fmt.Printf("URL: %s\n", test.url)

		req, _ := http.NewRequest("POST", test.url, bytes.NewBufferString(test.body))
		for k, v := range test.headers {
			req.Header.Set(k, v)
		}

		start := time.Now()
		resp, err := client.Do(req)
		latency := time.Since(start)

		if err != nil {
			fmt.Printf("❌ 网络错误: %v\n", err)
			continue
		}
		defer resp.Body.Close()

		bodyBytes, _ := io.ReadAll(resp.Body)
		bodyStr := string(bodyBytes)

		// relay-pulse 的判定逻辑
		status := "🔴 红色(失败)"
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			// 内容校验
			if strings.Contains(bodyStr, test.successContains) {
				status = "🟢 绿色(成功)"
			} else {
				status = "🔴 红色(内容不匹配)"
			}
		} else if resp.StatusCode == 429 {
			status = "🟡 黄色(限流)"
		}

		fmt.Printf("HTTP: %d | 延迟: %v\n", resp.StatusCode, latency.Round(time.Millisecond))
		fmt.Printf("状态: %s\n", status)

		// 显示响应片段
		snippet := bodyStr
		if len(snippet) > 300 {
			snippet = snippet[:300] + "..."
		}
		fmt.Printf("响应: %s\n", snippet)

		// 检查是否包含预期内容
		if strings.Contains(bodyStr, test.successContains) {
			fmt.Printf("✅ 包含预期内容 %q\n", test.successContains)
		} else {
			fmt.Printf("❌ 不包含预期内容 %q\n", test.successContains)
		}
	}
}
