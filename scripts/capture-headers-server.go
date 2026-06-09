//go:build ignore

// +build ignore

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
)

// 本地 HTTP 服务器，捕获请求的所有 headers
// 启动后，将 Claude Code 的 API URL 指向 http://localhost:18888/v1/messages
// Author: Half open flowers

func main() {
	port := ":18888"

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("\n" + strings.Repeat("=", 70))
		fmt.Printf("收到请求: %s %s\n", r.Method, r.URL.Path)
		fmt.Println(strings.Repeat("=", 70))

		// 打印所有 headers（按字母排序）
		fmt.Println("\n📋 请求 Headers:")
		var keys []string
		for k := range r.Header {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			values := r.Header[k]
			for _, v := range values {
				// 脱敏 API Key
				displayValue := v
				if strings.Contains(strings.ToLower(k), "key") || strings.Contains(strings.ToLower(k), "authorization") {
					if len(v) > 20 {
						displayValue = v[:10] + "..." + v[len(v)-4:]
					}
				}
				fmt.Printf("  %s: %s\n", k, displayValue)
			}
		}

		// 打印请求体
		if r.Body != nil {
			body, _ := io.ReadAll(r.Body)
			if len(body) > 0 {
				fmt.Println("\n📦 请求体:")
				// 格式化 JSON
				var prettyJSON map[string]interface{}
				if json.Unmarshal(body, &prettyJSON) == nil {
					formatted, _ := json.MarshalIndent(prettyJSON, "  ", "  ")
					fmt.Printf("  %s\n", string(formatted))
				} else {
					fmt.Printf("  %s\n", string(body))
				}
			}
		}

		fmt.Println(strings.Repeat("-", 70))

		// 返回一个模拟的成功响应
		w.Header().Set("Content-Type", "application/json")
		response := map[string]interface{}{
			"id":   "msg_capture_test",
			"type": "message",
			"role": "assistant",
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": "Headers captured! Check server console.",
				},
			},
			"model":         "claude-haiku-4-5-20251001",
			"stop_reason":   "end_turn",
			"stop_sequence": nil,
			"usage": map[string]int{
				"input_tokens":  10,
				"output_tokens": 5,
			},
		}
		json.NewEncoder(w).Encode(response)
	})

	fmt.Printf("🚀 Header 捕获服务器启动在 http://localhost%s\n", port)
	fmt.Println("📌 使用方法:")
	fmt.Println("   1. 在 Code-Switch 中添加一个测试供应商")
	fmt.Println("   2. API URL 设置为: http://localhost:18888")
	fmt.Println("   3. 启用该供应商")
	fmt.Println("   4. 使用 Claude Code 发送请求")
	fmt.Println("   5. 在这里查看捕获的 Headers")
	fmt.Println("\n按 Ctrl+C 停止服务器...")

	log.Fatal(http.ListenAndServe(port, nil))
}
