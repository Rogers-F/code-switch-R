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

// withFixedLocal 把 time.Local 固定为东八区,结束后还原,
// 保证时区相关断言与运行机器的系统时区无关
func withFixedLocal(t *testing.T) *time.Location {
	t.Helper()

	old := time.Local
	loc := time.FixedZone("UTC+8", 8*3600)
	time.Local = loc
	t.Cleanup(func() { time.Local = old })
	return loc
}

// setupLogFixTestDB 初始化独立临时库并建含全部列的 request_log 表
func setupLogFixTestDB(t *testing.T) *sql.DB {
	t.Helper()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	// Windows 的 os.UserHomeDir() 读的是 USERPROFILE,只设 HOME 会写到真实用户配置目录
	t.Setenv("USERPROFILE", tmpHome)

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

	const schema = `CREATE TABLE IF NOT EXISTS request_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		platform TEXT, model TEXT, provider TEXT,
		http_code INTEGER, input_tokens INTEGER, output_tokens INTEGER,
		cache_create_tokens INTEGER, cache_read_tokens INTEGER,
		reasoning_tokens INTEGER, is_stream INTEGER DEFAULT 0,
		duration_sec REAL DEFAULT 0,
		ephemeral_5m_tokens INTEGER DEFAULT 0,
		ephemeral_1h_tokens INTEGER DEFAULT 0,
		service_tier TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("创建 request_log 表失败: %v", err)
	}
	return db
}

func insertLogFixRecord(t *testing.T, db *sql.DB, createdAt string) {
	t.Helper()

	const insertSQL = `INSERT INTO request_log
		(platform, model, provider, http_code, input_tokens, output_tokens,
		 cache_create_tokens, cache_read_tokens, reasoning_tokens, created_at)
		VALUES ('claude', 'claude-haiku-4-5', 'p1', 200, 1000000, 0, 0, 0, 0, ?)`
	if _, err := db.Exec(insertSQL, createdAt); err != nil {
		t.Fatalf("插入记录失败: %v", err)
	}
}

// TestParseTimeInputZoneHandling 前端传入的裸时间串是本地墙钟时间,
// 必须按本地时区解析;带时区的串按串内时区解析
func TestParseTimeInputZoneHandling(t *testing.T) {
	loc := withFixedLocal(t)

	cases := []struct {
		name  string
		input string
		want  int64
	}{
		{
			name:  "裸时间串按本地时区解析",
			input: "2026-07-28 10:00:00",
			want:  time.Date(2026, 7, 28, 10, 0, 0, 0, loc).Unix(),
		},
		{
			name:  "带时区串按自身时区解析",
			input: "2026-07-28T10:00:00+02:00",
			want:  time.Date(2026, 7, 28, 10, 0, 0, 0, time.FixedZone("", 2*3600)).Unix(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseTimeInput(tc.input)
			if err != nil {
				t.Fatalf("parseTimeInput(%q): %v", tc.input, err)
			}
			if got.Unix() != tc.want {
				t.Fatalf("parseTimeInput(%q).Unix()=%d, want %d(裸时间串被误按 UTC 解析)", tc.input, got.Unix(), tc.want)
			}
		})
	}
}

// TestCostSinceUsesUTCBoundary created_at 落库为 UTC 文本,查询边界须转 UTC。
// 旧实现双重时区偏移:本地串先被按 UTC 解析,再以本地墙钟格式化比较,
// 东八区下"今日 00:00 起"的成本在下午 4 点前恒为 0。
func TestCostSinceUsesUTCBoundary(t *testing.T) {
	withFixedLocal(t)
	db := setupLogFixTestDB(t)

	// 记录 1 小时前产生,落库为 UTC 文本(模拟 DEFAULT CURRENT_TIMESTAMP)
	insertLogFixRecord(t, db, time.Now().UTC().Add(-time.Hour).Format(timeLayout))

	ls := NewLogService()
	// 查询窗口下界:本地 2 小时前的裸时间串(模拟托盘前端传参)
	start := time.Now().In(time.Local).Add(-2 * time.Hour).Format(timeLayout)
	cost, err := ls.CostSince(start, "")
	if err != nil {
		t.Fatalf("CostSince: %v", err)
	}
	if cost <= 0 {
		t.Fatalf("窗口内记录成本=%v, 期望 > 0(时区偏移导致记录被查询边界排除)", cost)
	}
}

// TestHeatmapStatsUsesUTCBoundary 热力图 SQL 预滤边界须转 UTC,
// 旧实现用本地墙钟串比较,东八区下窗口最旧的 8 小时记录被误排除
func TestHeatmapStatsUsesUTCBoundary(t *testing.T) {
	withFixedLocal(t)
	db := setupLogFixTestDB(t)

	// 记录 20 小时前产生,在 1 天(24 小时)窗口内;
	// 旧实现下东八区窗口下界被推后 8 小时,该记录被 SQL 预滤丢弃
	insertLogFixRecord(t, db, time.Now().UTC().Add(-20*time.Hour).Format(timeLayout))

	ls := NewLogService()
	stats, err := ls.HeatmapStats(1)
	if err != nil {
		t.Fatalf("HeatmapStats: %v", err)
	}
	var total int64
	for _, s := range stats {
		total += s.TotalRequests
	}
	if total != 1 {
		t.Fatalf("热力图统计到 %d 条请求, 期望 1", total)
	}
}
