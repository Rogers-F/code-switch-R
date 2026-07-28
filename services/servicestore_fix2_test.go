package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNewSuiStoreClosesDBOnCreateTableFailure 建表失败时不能泄漏已打开的 sql.DB 连接/文件句柄。
// 做法：预先在目标路径写入非法内容，让 sql.Open 之后的第一次真实 I/O(CREATE TABLE)
// 必然失败，从而触发该错误分支；随后校验目标文件可被删除——
// 若 db 连接未关闭，Windows 下仍持有句柄的文件通常无法被删除/覆盖(共享冲突)。
func TestNewSuiStoreClosesDBOnCreateTableFailure(t *testing.T) {
	tmpHome := t.TempDir()

	// os.UserConfigDir() 各平台读的变量不同：Windows 读 AppData、
	// macOS 读 $HOME/Library/Application Support、Linux 读 XDG_CONFIG_HOME 或 $HOME/.config。
	// 三套都设置，并且不自己拼路径——直接问 getSafeDBPath 要真实路径，
	// 这样三个平台走同一条逻辑，也不会有任何一条路径碰到真实用户目录。
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("AppData", filepath.Join(tmpHome, "AppData", "Roaming"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpHome, ".config"))

	dbPath, err := getSafeDBPath()
	if err != nil {
		t.Fatalf("获取数据库路径失败: %v", err)
	}
	// 兜底确认隔离生效，避免误写真实用户配置目录
	if !strings.HasPrefix(filepath.Clean(dbPath), filepath.Clean(tmpHome)) {
		t.Fatalf("测试环境未隔离: getSafeDBPath() = %q，不在临时目录 %q 之下", dbPath, tmpHome)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("创建数据库目录失败: %v", err)
	}

	// 写入非法内容，使其不是合法的 sqlite 文件，CREATE TABLE 必然报错
	if err := os.WriteFile(dbPath, []byte("not a sqlite database file"), 0o644); err != nil {
		t.Fatalf("写入损坏的数据库文件失败: %v", err)
	}

	if _, err := NewSuiStore(); err == nil {
		t.Fatal("对损坏的数据库文件调用 NewSuiStore 应当返回错误")
	}

	// 若错误路径上 db 未关闭，连接仍持有该文件句柄，此时删除该文件在 Windows 下会失败
	if err := os.Remove(dbPath); err != nil {
		t.Fatalf("NewSuiStore 失败后无法删除数据库文件，可能是 db 句柄未关闭导致的泄漏: %v", err)
	}
}
