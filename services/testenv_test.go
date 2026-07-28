package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// assertHomeIsolated 确认 os.UserHomeDir() 已被重定向到测试临时目录。
// 各平台读取的环境变量不同(Windows 用 USERPROFILE,类 Unix 用 HOME),
// 漏设其中一个会让测试直接读写真实用户配置目录并覆盖用户数据。
func assertHomeIsolated(t *testing.T, tmpHome string) {
	t.Helper()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("获取用户家目录失败: %v", err)
	}

	want, err := filepath.EvalSymlinks(tmpHome)
	if err != nil {
		want = tmpHome
	}
	got, err := filepath.EvalSymlinks(home)
	if err != nil {
		got = home
	}

	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(want)) {
		t.Fatalf("测试环境未隔离: os.UserHomeDir() = %q, 期望 %q。"+
			"继续执行会覆盖真实用户配置,请同时设置 HOME 与 USERPROFILE", home, tmpHome)
	}
}
