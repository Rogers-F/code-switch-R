package services

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/daodao97/xgo/xdb"
	_ "modernc.org/sqlite"
)

// escapeSQLiteURIPath 转义 SQLite file: URI 路径中会改变 URI 语义的字符。
// 只转义 '?'、'#'、'%'，其余（含盘符冒号、中文、空格）保持原样，
// 避免过度编码后驱动反而找不到文件。
func escapeSQLiteURIPath(path string) string {
	var b strings.Builder
	b.Grow(len(path))
	for i := 0; i < len(path); i++ {
		switch path[i] {
		case '?':
			b.WriteString("%3f")
		case '#':
			b.WriteString("%23")
		case '%':
			b.WriteString("%25")
		default:
			b.WriteByte(path[i])
		}
	}
	return b.String()
}

// InitDatabase 初始化数据库连接（必须在所有服务构造之前调用）
// 【修复】解决数据库初始化时序问题：
// 1. 确保配置目录存在
// 2. 初始化 xdb 连接池（PRAGMA 经 DSN 下发到每条连接）
// 3. 校验 PRAGMA 已生效
// 4. 确保表结构存在
// 5. 预热连接池
func InitDatabase() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取用户目录失败: %w", err)
	}

	// 1. 确保配置目录存在（SQLite 不会自动创建父目录）。
	// 0700：抓包全量模式会把明文 API Key 与完整请求/响应写进 app.db，
	// 目录与库文件都收敛为仅属主可读写（POSIX 位在 Windows 上为尽力而为）
	configDir := filepath.Join(home, ".code-switch")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	// 已存在的旧目录一并收紧
	if err := os.Chmod(configDir, 0700); err != nil {
		fmt.Printf("[DB] 收紧配置目录权限失败（不影响运行）: %v\n", err)
	}

	// 2. 初始化 xdb 连接池
	// 【修复】busy_timeout 是连接级属性，db.Exec 只对连接池中当时取出的那一条连接生效，
	// 其余连接仍为 0，并发写会立即返回 SQLITE_BUSY。modernc.org/sqlite 只在 DSN 带 file:
	// 前缀时保留查询参数，并对每条新建连接自动执行 _pragma 指定的 PRAGMA，
	// 因此必须把 PRAGMA 写进 DSN（journal_mode=WAL 是库级持久属性，一并放入保证首连即生效）。
	// 旧 DSN 的 cache=shared&mode=rwc 无 file: 前缀时本就被驱动整体丢弃，此处不再保留。
	// 路径要按 URI 转义：家目录里出现 '#' 会被当作 fragment 截断、'?' 会被当作查询串起点，
	// 结果是静默打开另一个数据库文件
	dbFile := escapeSQLiteURIPath(filepath.ToSlash(filepath.Join(configDir, "app.db")))
	dsn := "file:" + dbFile + "?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)"
	if err := xdb.Inits([]xdb.Config{
		{
			Name:   "default",
			Driver: "sqlite",
			DSN:    dsn,
		},
	}); err != nil {
		return fmt.Errorf("初始化数据库失败: %w", err)
	}

	// 3. 校验 PRAGMA 已生效（这次查询会真正建立连接、创建库文件）
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}
	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		return fmt.Errorf("读取 journal_mode 失败: %w", err)
	}
	fmt.Printf("✅ SQLite PRAGMA 已设置: journal_mode=%s, busy_timeout=30000ms\n", journalMode)

	// 库文件收敛为 0600：抓包全量模式的明文内容都落在这里。
	// 必须在首次查询之后——sql.Open 是惰性的，文件到此才真正存在
	if err := os.Chmod(filepath.Join(configDir, "app.db"), 0600); err != nil {
		fmt.Printf("[DB] 收紧库文件权限失败（不影响运行）: %v\n", err)
	}

	// 4. 确保表结构存在
	if err := ensureRequestLogTable(); err != nil {
		return fmt.Errorf("初始化 request_log 表失败: %w", err)
	}
	if err := ensureBlacklistTables(); err != nil {
		return fmt.Errorf("初始化黑名单表失败: %w", err)
	}
	if err := ensureProviderAliasTable(); err != nil {
		return fmt.Errorf("初始化 provider_alias 表失败: %w", err)
	}

	// 5. 预热连接池：强制建立数据库连接，避免首次写入时失败
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM request_log").Scan(&count); err != nil {
		fmt.Printf("⚠️  连接池预热查询失败: %v\n", err)
	} else {
		fmt.Printf("✅ 数据库连接已预热（request_log 记录数: %d）\n", count)
	}

	return nil
}

// ensureBlacklistTables 确保黑名单相关表存在
func ensureBlacklistTables() error {
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	// 1. 创建 app_settings 表
	const createAppSettingsSQL = `CREATE TABLE IF NOT EXISTS app_settings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		key TEXT UNIQUE NOT NULL,
		value TEXT
	)`
	if _, err := db.Exec(createAppSettingsSQL); err != nil {
		return fmt.Errorf("创建 app_settings 表失败: %w", err)
	}

	// 2. 创建 provider_blacklist 表
	const createBlacklistSQL = `CREATE TABLE IF NOT EXISTS provider_blacklist (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		platform TEXT NOT NULL,
		provider_name TEXT NOT NULL,
		failure_count INTEGER DEFAULT 0,
		blacklisted_at DATETIME,
		blacklisted_until DATETIME,
		last_failure_at DATETIME,
		blacklist_level INTEGER DEFAULT 0,
		last_recovered_at DATETIME,
		last_degrade_hour INTEGER DEFAULT 0,
		last_failure_window_start DATETIME,
		auto_recovered INTEGER DEFAULT 0,
		UNIQUE(platform, provider_name)
	)`
	if _, err := db.Exec(createBlacklistSQL); err != nil {
		return fmt.Errorf("创建 provider_blacklist 表失败: %w", err)
	}

	// 2.1 旧库补列：分级拉黑引入的四列对已存在的旧表不会由 CREATE TABLE IF NOT EXISTS 补上，
	// 必须按 pragma_table_info 检测后 ALTER 补齐，否则旧库升级后黑名单 SQL 全部报 no such column
	blacklistMigrations := []struct {
		column     string
		definition string
	}{
		{"blacklist_level", "INTEGER DEFAULT 0"},
		{"last_recovered_at", "DATETIME"},
		{"last_degrade_hour", "INTEGER DEFAULT 0"},
		{"last_failure_window_start", "DATETIME"},
	}
	for _, m := range blacklistMigrations {
		if err := ensureBlacklistColumn(db, m.column, m.definition); err != nil {
			return fmt.Errorf("补齐 provider_blacklist 列 %s 失败: %w", m.column, err)
		}
	}

	// 3. 确保 app_settings 中有默认的黑名单配置
	defaultSettings := []struct {
		key   string
		value string
	}{
		{"enable_blacklist", "false"},
		{"blacklist_failure_threshold", "3"},
		{"blacklist_duration_minutes", "30"},
	}

	for _, s := range defaultSettings {
		_, err := db.Exec(`
			INSERT OR IGNORE INTO app_settings (key, value) VALUES (?, ?)
		`, s.key, s.value)
		if err != nil {
			return fmt.Errorf("插入默认设置 %s 失败: %w", s.key, err)
		}
	}

	return nil
}

// ensureBlacklistColumn 检测 provider_blacklist 是否缺列,缺则 ALTER 补齐(旧库升级用)
func ensureBlacklistColumn(db *sql.DB, column string, definition string) error {
	query := fmt.Sprintf("SELECT COUNT(*) FROM pragma_table_info('provider_blacklist') WHERE name = '%s'", column)
	var count int
	if err := db.QueryRow(query).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		alter := fmt.Sprintf("ALTER TABLE provider_blacklist ADD COLUMN %s %s", column, definition)
		if _, err := db.Exec(alter); err != nil {
			return err
		}
	}
	return nil
}

// ensureProviderAliasTable 创建 provider_alias 表,用于 rename 后 48h 内承接旧名 in-flight 写入。
func ensureProviderAliasTable() error {
	db, err := xdb.DB("default")
	if err != nil {
		return fmt.Errorf("获取数据库连接失败: %w", err)
	}

	const createSQL = `CREATE TABLE IF NOT EXISTS provider_alias (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		platform TEXT NOT NULL,
		provider_id INTEGER NOT NULL,
		alias_name TEXT NOT NULL COLLATE NOCASE,
		canonical_name TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME NOT NULL,
		UNIQUE(platform, alias_name)
	)`
	if _, err := db.Exec(createSQL); err != nil {
		return fmt.Errorf("创建 provider_alias 表失败: %w", err)
	}

	const createIndexSQL = `
		CREATE INDEX IF NOT EXISTS idx_provider_alias_pid ON provider_alias(platform, provider_id);
		CREATE INDEX IF NOT EXISTS idx_provider_alias_expires ON provider_alias(expires_at);
	`
	if _, err := db.Exec(createIndexSQL); err != nil {
		return fmt.Errorf("创建 provider_alias 索引失败: %w", err)
	}

	return nil
}
