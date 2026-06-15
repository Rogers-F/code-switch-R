package services

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daodao97/xgo/xdb"
	_ "modernc.org/sqlite"
)

// setupRenameTestEnv 把 HOME 指到临时目录并初始化独立的 app.db,
// 同时初始化 request_log / provider_blacklist / health_check_history / provider_alias 表。
func setupRenameTestEnv(t *testing.T) string {
	t.Helper()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	configDir := filepath.Join(tmpHome, ".code-switch")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("创建配置目录失败: %v", err)
	}

	dbPath := filepath.Join(configDir, "app.db?cache=shared&mode=rwc")
	if err := xdb.Inits([]xdb.Config{{Name: "default", Driver: "sqlite", DSN: dbPath}}); err != nil {
		t.Fatalf("初始化 xdb 失败: %v", err)
	}

	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("获取数据库失败: %v", err)
	}
	_, _ = db.Exec("PRAGMA busy_timeout = 30000")

	// 测试结束关闭 DB 句柄，确保 Windows 下 t.TempDir() 能删除 app.db（句柄占用会导致 RemoveAll 失败）
	t.Cleanup(func() {
		if d, e := xdb.DB("default"); e == nil && d != nil {
			_ = d.Close()
		}
	})

	schemas := []string{
		`CREATE TABLE IF NOT EXISTS request_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			platform TEXT, model TEXT, provider TEXT,
			http_code INTEGER, input_tokens INTEGER, output_tokens INTEGER,
			cache_create_tokens INTEGER, cache_read_tokens INTEGER,
			reasoning_tokens INTEGER, is_stream INTEGER DEFAULT 0,
			duration_sec REAL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS provider_blacklist (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			platform TEXT NOT NULL, provider_name TEXT NOT NULL,
			failure_count INTEGER DEFAULT 0,
			blacklisted_at DATETIME, blacklisted_until DATETIME,
			last_failure_at DATETIME, blacklist_level INTEGER DEFAULT 0,
			last_recovered_at DATETIME, last_degrade_hour INTEGER DEFAULT 0,
			last_failure_window_start DATETIME, auto_recovered INTEGER DEFAULT 0,
			UNIQUE(platform, provider_name)
		)`,
		`CREATE TABLE IF NOT EXISTS health_check_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_id INTEGER NOT NULL, provider_name TEXT NOT NULL,
			platform TEXT NOT NULL, model TEXT, endpoint TEXT,
			status TEXT NOT NULL, latency_ms INTEGER, error_message TEXT,
			checked_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS provider_alias (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			platform TEXT NOT NULL, provider_id INTEGER NOT NULL,
			alias_name TEXT NOT NULL COLLATE NOCASE,
			canonical_name TEXT NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME NOT NULL,
			UNIQUE(platform, alias_name)
		)`,
	}
	for _, s := range schemas {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("建表失败: %v", err)
		}
	}

	return tmpHome
}

// saveProviderFixture 写入一组 provider 到 claude-code.json 作为初始状态。
func saveProviderFixture(t *testing.T, ps *ProviderService, providers []Provider) {
	t.Helper()
	// 直接写文件绕过 SaveProviders 的 name 不可改校验
	path, err := providerFilePath("claude")
	if err != nil {
		t.Fatalf("获取路径失败: %v", err)
	}
	data, _ := serializeProviders(providers)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写 fixture 失败: %v", err)
	}
}

func seedRequestLog(t *testing.T, platform, providerName string, count int) {
	t.Helper()
	db, _ := xdb.DB("default")
	for i := 0; i < count; i++ {
		_, err := db.Exec(
			`INSERT INTO request_log (platform, model, provider, http_code) VALUES (?, ?, ?, 200)`,
			platform, "test-model", providerName,
		)
		if err != nil {
			t.Fatalf("seed request_log 失败: %v", err)
		}
	}
}

func seedBlacklist(t *testing.T, platform, providerName string) {
	t.Helper()
	db, _ := xdb.DB("default")
	_, err := db.Exec(
		`INSERT INTO provider_blacklist (platform, provider_name, failure_count) VALUES (?, ?, 3)`,
		platform, providerName,
	)
	if err != nil {
		t.Fatalf("seed blacklist 失败: %v", err)
	}
}

func seedHealthCheck(t *testing.T, platform string, providerID int64, providerName string) {
	t.Helper()
	db, _ := xdb.DB("default")
	_, err := db.Exec(
		`INSERT INTO health_check_history (provider_id, provider_name, platform, status) VALUES (?, ?, ?, 'ok')`,
		providerID, providerName, platform,
	)
	if err != nil {
		t.Fatalf("seed health_check 失败: %v", err)
	}
}

func countRows(t *testing.T, query string, args ...interface{}) int {
	t.Helper()
	db, _ := xdb.DB("default")
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil && err != sql.ErrNoRows {
		t.Fatalf("查询失败: %v (sql=%s)", err, query)
	}
	return n
}

// TestRenameProvider_HappyPath 基础 rename + 历史数据迁移。
func TestRenameProvider_HappyPath(t *testing.T) {
	setupRenameTestEnv(t)

	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{
		{ID: 1, Name: "OldName", APIURL: "https://a.com", APIKey: "k"},
	})

	seedRequestLog(t, "claude", "OldName", 5)
	seedBlacklist(t, "claude", "OldName")
	seedHealthCheck(t, "claude", 1, "OldName")

	if err := ps.RenameProvider("claude", 1, "NewName"); err != nil {
		t.Fatalf("RenameProvider 失败: %v", err)
	}

	// 验证配置文件
	providers, err := ps.LoadProviders("claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(providers) != 1 || providers[0].Name != "NewName" {
		t.Errorf("JSON 应更新为 NewName,实际 %+v", providers)
	}

	// 验证 DB 历史数据
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE provider = ? AND platform = ?`, "NewName", "claude"); n != 5 {
		t.Errorf("request_log 应 5 条 NewName,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM request_log WHERE provider = ?`, "OldName"); n != 0 {
		t.Errorf("request_log 不应还有 OldName,实际 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM provider_blacklist WHERE provider_name = ?`, "NewName"); n != 1 {
		t.Errorf("provider_blacklist 应改名,实际 NewName 条数 %d", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM health_check_history WHERE provider_name = ?`, "NewName"); n != 1 {
		t.Errorf("health_check_history 应改名,实际 %d", n)
	}

	// 验证 alias 已写入
	if n := countRows(t, `SELECT COUNT(*) FROM provider_alias WHERE alias_name = ? AND canonical_name = ?`, "OldName", "NewName"); n != 1 {
		t.Errorf("alias 应有 1 条 OldName->NewName,实际 %d", n)
	}
}

// TestRenameProvider_EmptyName 拒绝空名字。
func TestRenameProvider_EmptyName(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "X", APIURL: "u"}})

	if err := ps.RenameProvider("claude", 1, "  "); err == nil {
		t.Error("空名字应拒绝")
	}
}

// TestRenameProvider_SameName 拒绝新旧相同。
func TestRenameProvider_SameName(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "X", APIURL: "u"}})

	if err := ps.RenameProvider("claude", 1, "x"); err == nil {
		t.Error("新旧同名(大小写不同)应拒绝")
	}
}

// TestRenameProvider_CurrentConflict 拒绝和同 kind 其它 provider 冲突。
func TestRenameProvider_CurrentConflict(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{
		{ID: 1, Name: "A", APIURL: "u"},
		{ID: 2, Name: "B", APIURL: "u"},
	})

	if err := ps.RenameProvider("claude", 1, "B"); err == nil {
		t.Error("冲突名字应拒绝")
	}
}

// TestRenameProvider_ChainedBlocked 48h 内同 provider 禁止再次 rename。
func TestRenameProvider_ChainedBlocked(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "A", APIURL: "u"}})

	if err := ps.RenameProvider("claude", 1, "B"); err != nil {
		t.Fatalf("首次 rename 应成功: %v", err)
	}
	if err := ps.RenameProvider("claude", 1, "C"); err == nil {
		t.Error("48h 内再次 rename 应拒绝")
	}
}

// TestRenameProvider_AliasOccupied 新名字被其它 provider 的未过期 alias 占用时拒绝。
func TestRenameProvider_AliasOccupied(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{
		{ID: 1, Name: "A", APIURL: "u"},
		{ID: 2, Name: "X", APIURL: "u"},
	})

	// A -> B,产生 alias A
	if err := ps.RenameProvider("claude", 1, "B"); err != nil {
		t.Fatalf("A->B 失败: %v", err)
	}
	// 此时 X 想改为 A,但 alias 还占着 A
	if err := ps.RenameProvider("claude", 2, "A"); err == nil {
		t.Error("新名 A 被未过期 alias 占用,应拒绝")
	}
}

// TestRenameProvider_TTLCleanup 过期 alias 不应阻塞新 rename。
func TestRenameProvider_TTLCleanup(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{
		{ID: 1, Name: "A", APIURL: "u"},
	})
	if err := ps.RenameProvider("claude", 1, "B"); err != nil {
		t.Fatalf("rename 失败: %v", err)
	}

	// 手动把 alias 和 provider_id 相关记录改为已过期
	db, _ := xdb.DB("default")
	past := time.Now().Add(-1 * time.Hour).UTC().Format("2006-01-02 15:04:05")
	if _, err := db.Exec(`UPDATE provider_alias SET expires_at = ?`, past); err != nil {
		t.Fatalf("手动过期失败: %v", err)
	}

	// 现在 rename B -> C 应该过(链式约束看未过期,过期不算)
	if err := ps.RenameProvider("claude", 1, "C"); err != nil {
		t.Errorf("过期 alias 不应阻塞:%v", err)
	}

	// 过期 alias 应该已经被清理
	if n := countRows(t, `SELECT COUNT(*) FROM provider_alias WHERE alias_name = 'A'`); n != 0 {
		t.Errorf("过期 alias 应被清理,实际仍有 %d 条", n)
	}
}

// TestRenameProvider_NotFound id 不存在时报错。
func TestRenameProvider_NotFound(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "A", APIURL: "u"}})

	if err := ps.RenameProvider("claude", 999, "B"); err == nil {
		t.Error("id 不存在应报错")
	}
}

// TestRenameProvider_RollbackOnTxFail DB 事务失败时,配置文件应回滚回旧名。
// 通过在事务执行前 DROP 目标表来制造 tx 失败。
func TestRenameProvider_RollbackOnTxFail(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "OldName", APIURL: "u"}})

	// 故意破坏表结构:DROP provider_blacklist,让 doRenameTx 的 UPDATE 失败
	db, _ := xdb.DB("default")
	if _, err := db.Exec(`DROP TABLE provider_blacklist`); err != nil {
		t.Fatalf("drop 失败: %v", err)
	}

	err := ps.RenameProvider("claude", 1, "NewName")
	if err == nil {
		t.Fatal("期望 rename 失败,实际成功")
	}

	// 验证配置文件被回滚回 OldName
	providers, lerr := ps.LoadProviders("claude")
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(providers) != 1 || providers[0].Name != "OldName" {
		t.Errorf("配置文件应回滚为 OldName,实际 %+v", providers)
	}
}

// TestSaveProviders_RejectsAliasReuse 验证新建/保存 provider 时,
// 不能使用 48h 内仍活动的 alias 名,防止历史数据被 alias resolver 静默归并。
func TestSaveProviders_RejectsAliasReuse(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{
		{ID: 1, Name: "OldName", APIURL: "https://a.com"},
	})

	// A->B 产生 alias OldName->NewName
	if err := ps.RenameProvider("claude", 1, "NewName"); err != nil {
		t.Fatalf("rename 失败: %v", err)
	}

	// 用户尝试新增 id=2 命名为 OldName,应该被拒绝
	providers, _ := ps.LoadProviders("claude")
	providers = append(providers, Provider{ID: 2, Name: "OldName", APIURL: "https://b.com"})
	err := ps.SaveProviders("claude", providers)
	if err == nil {
		t.Fatal("新建 provider 复用活动 alias 名应该被拒绝")
	}
}

// TestSaveProviders_AliasReuseCaseInsensitive 验证 alias 占用的大小写不敏感,
// 锁住 alias_name 列的 COLLATE NOCASE 契约,防止未来改回 case-sensitive 产生回归。
func TestSaveProviders_AliasReuseCaseInsensitive(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{
		{ID: 1, Name: "OldName", APIURL: "https://a.com"},
	})

	if err := ps.RenameProvider("claude", 1, "NewName"); err != nil {
		t.Fatalf("rename 失败: %v", err)
	}

	// 使用不同大小写的同名("oldname" vs "OldName")仍应被拒绝
	providers, _ := ps.LoadProviders("claude")
	providers = append(providers, Provider{ID: 2, Name: "oldname", APIURL: "https://b.com"})
	if err := ps.SaveProviders("claude", providers); err == nil {
		t.Fatal("大小写不同的同名 alias 也应被拒绝(COLLATE NOCASE)")
	}
}

// TestResolveProviderAlias rename 后用旧名查 canonical。
func TestResolveProviderAlias(t *testing.T) {
	setupRenameTestEnv(t)
	ps := NewProviderService()
	saveProviderFixture(t, ps, []Provider{{ID: 1, Name: "A", APIURL: "u"}})

	if err := ps.RenameProvider("claude", 1, "B"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if got := ResolveProviderAlias("claude", "A"); got != "B" {
		t.Errorf("A 应该被解析为 B,实际 %q", got)
	}
	if got := ResolveProviderAlias("claude", "B"); got != "B" {
		t.Errorf("canonical 输入应原样返回,实际 %q", got)
	}
	if got := ResolveProviderAlias("claude", "Unknown"); got != "Unknown" {
		t.Errorf("未注册 name 应原样返回,实际 %q", got)
	}
}
