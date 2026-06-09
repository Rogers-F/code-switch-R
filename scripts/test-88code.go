//go:build ignore

// +build ignore

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 测试 88code 连通性 - 全面测试端点和认证方式组合
// Author: Half open flowers

// 测试结果
type TestResult struct {
	Endpoint   string
	AuthMethod string
	Model      string
	StatusCode int
	Latency    time.Duration
	Response   string
	Success    bool
}

func main() {
	// 88code 配置
	baseURL := "https://m.88code.org/api"
	apiKey := "88_784022b235595e84936fa42596b41bcad3dc24a79ced14a5d0d4489937ba36e8"

	client := &http.Client{Timeout: 15 * time.Second}

	fmt.Println("=" + strings.Repeat("=", 60))
	fmt.Println("88code 连通性全面测试")
	fmt.Println("=" + strings.Repeat("=", 60))
	fmt.Printf("Base URL: %s\n", baseURL)
	fmt.Printf("API Key: %s...%s\n\n", apiKey[:10], apiKey[len(apiKey)-4:])

	// 测试端点列表
	endpoints := []string{
		"/v1/messages",
		"/v1/chat/completions",
		"/messages",
		"/chat/completions",
	}

	// 不带 /api 前缀的端点
	noApiEndpoints := []string{
		"/v1/messages",
		"/v1/chat/completions",
	}

	// 测试模型
	models := []string{
		"claude-haiku-4-5-20251001",
		"claude-3-5-haiku-20241022",
		"claude-sonnet-4-20250514",
	}

	var results []TestResult

	// ============ 测试 1: 带 /api 前缀的端点 ============
	fmt.Println("\n" + strings.Repeat("-", 60))
	fmt.Println("第一部分：测试 Base URL = " + baseURL)
	fmt.Println(strings.Repeat("-", 60))

	for _, endpoint := range endpoints {
		url := baseURL + endpoint

		// 测试 x-api-key 认证 (Anthropic 风格)
		for _, model := range models {
			result := testWithXAPIKey(client, url, apiKey, model)
			results = append(results, result)
			printResult(result)
		}

		// 测试 Bearer 认证 (OpenAI 风格)
		for _, model := range models {
			result := testWithBearer(client, url, apiKey, model)
			results = append(results, result)
			printResult(result)
		}

		fmt.Println()
	}

	// ============ 测试 2: 不带 /api 前缀的端点 ============
	baseURLNoAPI := "https://m.88code.org"
	fmt.Println("\n" + strings.Repeat("-", 60))
	fmt.Println("第二部分：测试 Base URL = " + baseURLNoAPI)
	fmt.Println(strings.Repeat("-", 60))

	for _, endpoint := range noApiEndpoints {
		url := baseURLNoAPI + endpoint

		for _, model := range models {
			result := testWithXAPIKey(client, url, apiKey, model)
			results = append(results, result)
			printResult(result)
		}

		for _, model := range models {
			result := testWithBearer(client, url, apiKey, model)
			results = append(results, result)
			printResult(result)
		}

		fmt.Println()
	}

	// ============ 汇总结果 ============
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("测试汇总")
	fmt.Println(strings.Repeat("=", 60))

	successResults := []TestResult{}
	for _, r := range results {
		if r.Success {
			successResults = append(successResults, r)
		}
	}

	if len(successResults) > 0 {
		fmt.Printf("\n✅ 成功的组合 (%d 个):\n", len(successResults))
		for _, r := range successResults {
			fmt.Printf("   端点: %-30s | 认证: %-15s | 模型: %s | 延迟: %v\n",
				r.Endpoint, r.AuthMethod, r.Model, r.Latency)
		}

		// 推荐配置
		best := successResults[0]
		for _, r := range successResults {
			if r.Latency < best.Latency {
				best = r
			}
		}
		fmt.Println("\n📌 推荐配置（延迟最低）:")
		fmt.Printf("   端点: %s\n", best.Endpoint)
		fmt.Printf("   认证方式: %s\n", best.AuthMethod)
		fmt.Printf("   测试模型: %s\n", best.Model)
		fmt.Printf("   响应延迟: %v\n", best.Latency)
	} else {
		fmt.Println("\n❌ 没有找到成功的组合")
		fmt.Println("\n可能的原因:")
		fmt.Println("  1. API Key 无效或过期")
		fmt.Println("  2. IP 被限制访问")
		fmt.Println("  3. 服务器暂时不可用")
		fmt.Println("  4. 需要其他认证方式")
	}

	// 显示所有失败结果的错误信息
	fmt.Println("\n" + strings.Repeat("-", 60))
	fmt.Println("失败响应详情（按状态码分组）:")
	fmt.Println(strings.Repeat("-", 60))

	statusGroups := make(map[int][]TestResult)
	for _, r := range results {
		if !r.Success {
			statusGroups[r.StatusCode] = append(statusGroups[r.StatusCode], r)
		}
	}

	for code, group := range statusGroups {
		fmt.Printf("\nHTTP %d (%d 个):\n", code, len(group))
		// 只显示第一个作为示例
		if len(group) > 0 {
			r := group[0]
			resp := r.Response
			if len(resp) > 200 {
				resp = resp[:200] + "..."
			}
			fmt.Printf("  示例: %s + %s\n", r.Endpoint, r.AuthMethod)
			fmt.Printf("  响应: %s\n", resp)
		}
	}
}

func testWithXAPIKey(client *http.Client, url, apiKey, model string) TestResult {
	reqBody := map[string]interface{}{
		"model":      model,
		"max_tokens": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return TestResult{Endpoint: url, AuthMethod: "x-api-key", Model: model, Response: err.Error()}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		return TestResult{Endpoint: url, AuthMethod: "x-api-key", Model: model, Latency: latency, Response: err.Error()}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	success := resp.StatusCode >= 200 && resp.StatusCode < 300

	return TestResult{
		Endpoint:   url,
		AuthMethod: "x-api-key",
		Model:      model,
		StatusCode: resp.StatusCode,
		Latency:    latency,
		Response:   string(respBody),
		Success:    success,
	}
}

func testWithBearer(client *http.Client, url, apiKey, model string) TestResult {
	reqBody := map[string]interface{}{
		"model":      model,
		"max_tokens": 1,
		"messages": []map[string]string{
			{"role": "user", "content": "hi"},
		},
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return TestResult{Endpoint: url, AuthMethod: "Bearer", Model: model, Response: err.Error()}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		return TestResult{Endpoint: url, AuthMethod: "Bearer", Model: model, Latency: latency, Response: err.Error()}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	success := resp.StatusCode >= 200 && resp.StatusCode < 300

	return TestResult{
		Endpoint:   url,
		AuthMethod: "Bearer",
		Model:      model,
		StatusCode: resp.StatusCode,
		Latency:    latency,
		Response:   string(respBody),
		Success:    success,
	}
}

func printResult(r TestResult) {
	icon := "❌"
	if r.Success {
		icon = "✅"
	} else if r.StatusCode == 401 || r.StatusCode == 403 {
		icon = "🔐"
	} else if r.StatusCode == 404 {
		icon = "🚫"
	} else if r.StatusCode == 400 {
		icon = "⚠️"
	}

	fmt.Printf("  %s [%-10s] %-50s HTTP %d (%v)\n",
		icon, r.AuthMethod, r.Endpoint, r.StatusCode, r.Latency.Round(time.Millisecond))
}
