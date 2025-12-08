package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

// GitHubRelease GitHub Release 结构
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

func main() {
	client := &http.Client{}

	releaseURL := "https://api.github.com/repos/SimonUTD/code-switch-R/releases/latest"

	req, err := http.NewRequest("GET", releaseURL, nil)
	if err != nil {
		log.Fatal("创建请求失败:", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "CodeSwitch-Test")

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("请求失败:", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Fatalf("GitHub API 返回错误状态码: %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		log.Fatal("解析响应失败:", err)
	}

	fmt.Printf("✅ 最新版本: %s\n", release.TagName)
	fmt.Printf("📝 发布说明:\n%s\n\n", release.Body)
	fmt.Printf("📦 可用的安装包:\n")
	for _, asset := range release.Assets {
		fmt.Printf("  - %s (%d bytes)\n    %s\n", asset.Name, asset.Size, asset.BrowserDownloadURL)
	}

	// 检查必需的文件是否存在
	requiredFiles := []string{
		"CodeSwitch-amd64-installer.exe", // Windows安装器
		"CodeSwitch.exe",                 // Windows便携版
		"codeswitch-macos-arm64.zip",     // macOS ARM
		"codeswitch-macos-amd64.zip",     // macOS Intel
	}

	fmt.Printf("\n🔍 检查必需文件:\n")
	for _, required := range requiredFiles {
		found := false
		for _, asset := range release.Assets {
			if asset.Name == required {
				found = true
				break
			}
		}
		if found {
			fmt.Printf("  ✅ %s\n", required)
		} else {
			fmt.Printf("  ❌ %s (缺失)\n", required)
		}
	}
}
