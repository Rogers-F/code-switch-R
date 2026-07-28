package services

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/daodao97/xgo/xdb"
	_ "modernc.org/sqlite"
)

// setupDatabaseFixTestHome 把 HOME 指到临时目录,防止测试污染真实用户配置
func setupDatabaseFixTestHome(t *testing.T) string {
	t.Helper()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	// Windows 的 os.UserHomeDir() 读的是 USERPROFILE,只设 HOME 会写到真实用户配置目录
	t.Setenv("USERPROFILE", tmpHome)
	return tmpHome
}

// TestInitDatabaseBusyTimeoutOnAllConnections 验证 busy_timeout 经 DSN 下发到连接池中的每条连接。
// 旧实现只在池中当时取出的那一条连接上 Exec PRAGMA,其余连接 busy_timeout=0,
// 并发写竞争时会立即返回 SQLITE_BUSY。
func TestInitDatabaseBusyTimeoutOnAllConnections(t *testing.T) {
	setupDatabaseFixTestHome(t)

	if err := InitDatabase(); err != nil {
		t.Fatalf("InitDatabase: %v", err)
	}
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("获取数据库失败: %v", err)
	}
	// 测试结束关闭 DB 句柄,确保 Windows 下 t.TempDir() 能删除 app.db
	t.Cleanup(func() { _ = db.Close() })

	// 同时持有多条连接再逐条检查,持有期间连接不会互相复用,
	// 因此其中必然包含预热之外新建的连接
	ctx := context.Background()
	const connCount = 3
	conns := make([]*sql.Conn, 0, connCount)
	t.Cleanup(func() {
		for _, c := range conns {
			_ = c.Close()
		}
	})
	for i := 0; i < connCount; i++ {
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("获取连接 %d 失败: %v", i, err)
		}
		conns = append(conns, conn)
	}

	for i, conn := range conns {
		var timeoutMs int
		if err := conn.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&timeoutMs); err != nil {
			t.Fatalf("连接 %d 读取 busy_timeout 失败: %v", i, err)
		}
		if timeoutMs != 30000 {
			t.Fatalf("连接 %d busy_timeout=%d, 期望 30000(PRAGMA 未随 DSN 下发到全部连接)", i, timeoutMs)
		}
	}
}

// TestEnsureBlacklistTablesMigratesOldSchema 验证分级拉黑之前的旧库升级后自动补齐四列。
// CREATE TABLE IF NOT EXISTS 对已存在的旧表是 no-op,缺列时黑名单 SQL 会报 no such column。
func TestEnsureBlacklistTablesMigratesOldSchema(t *testing.T) {
	tmpHome := setupDatabaseFixTestHome(t)

	configDir := filepath.Join(tmpHome, ".code-switch")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("创建配置目录失败: %v", err)
	}
	dbPath := filepath.Join(configDir, "app.db")
	if err := xdb.Inits([]xdb.Config{{Name: "default", Driver: "sqlite", DSN: dbPath}}); err != nil {
		t.Fatalf("初始化 xdb 失败: %v", err)
	}
	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("获取数据库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// 模拟分级拉黑引入之前的旧表结构(无 blacklist_level 等四列)
	const oldSchema = `CREATE TABLE provider_blacklist (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		platform TEXT NOT NULL,
		provider_name TEXT NOT NULL,
		failure_count INTEGER DEFAULT 0,
		blacklisted_at DATETIME,
		blacklisted_until DATETIME,
		last_failure_at DATETIME,
		auto_recovered INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(platform, provider_name)
	)`
	if _, err := db.Exec(oldSchema); err != nil {
		t.Fatalf("创建旧表失败: %v", err)
	}

	if err := ensureBlacklistTables(); err != nil {
		t.Fatalf("ensureBlacklistTables: %v", err)
	}

	migratedColumns := []string{
		"blacklist_level",
		"last_recovered_at",
		"last_degrade_hour",
		"last_failure_window_start",
	}
	for _, col := range migratedColumns {
		var count int
		query := fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('provider_blacklist') WHERE name = '%s'", col)
		if err := db.QueryRow(query).Scan(&count); err != nil {
			t.Fatalf("检查列 %s 失败: %v", col, err)
		}
		if count != 1 {
			t.Fatalf("旧库升级后缺列 %s 未补齐", col)
		}
	}

	// 补列后典型的黑名单写入不应再报 no such column
	if _, err := db.Exec(`INSERT INTO provider_blacklist (platform, provider_name, blacklist_level) VALUES ('claude', 'p1', 2)`); err != nil {
		t.Fatalf("写入含新列的记录失败: %v", err)
	}
}
