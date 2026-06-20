package services

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newQueueTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", "file:queue-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.Exec(`CREATE TABLE events (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return db
}

func TestDBWriteQueueExecWritesRow(t *testing.T) {
	db := newQueueTestDB(t)
	queue := NewDBWriteQueue(db, 10, false)
	t.Cleanup(func() { _ = queue.Shutdown(time.Second) })

	if err := queue.Exec(`INSERT INTO events (name) VALUES (?)`, "one"); err != nil {
		t.Fatalf("exec insert: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE name = ?`, "one").Scan(&count); err != nil {
		t.Fatalf("count row: %v", err)
	}
	if count != 1 {
		t.Fatalf("count=%d want 1", count)
	}
}

func TestDBWriteQueueBatchReturnsErrorToAllTasks(t *testing.T) {
	db := newQueueTestDB(t)
	queue := NewDBWriteQueue(db, 10, true)
	t.Cleanup(func() { _ = queue.Shutdown(time.Second) })

	err := queue.ExecBatch(`INSERT INTO missing_table (name) VALUES (?)`, "bad")
	if err == nil {
		t.Fatal("expected batch error")
	}
	if !strings.Contains(err.Error(), "批量提交失败") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDBWriteQueueRejectsAfterShutdown(t *testing.T) {
	db := newQueueTestDB(t)
	queue := NewDBWriteQueue(db, 10, false)

	if err := queue.Shutdown(time.Second); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	err := queue.Exec(`INSERT INTO events (name) VALUES (?)`, "after")
	if err == nil {
		t.Fatal("expected closed queue error")
	}
	if !strings.Contains(err.Error(), "写入队列已关闭") {
		t.Fatalf("unexpected error: %v", err)
	}
}
