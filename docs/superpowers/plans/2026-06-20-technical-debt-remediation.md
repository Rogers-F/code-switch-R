# Technical Debt Remediation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the highest-risk test, build, release, startup, path-boundary, and coverage issues found in the 2026-06-20 audit.

**Architecture:** Fix the dangerous data path first, then make the build and release path reproducible, then tighten runtime boundaries. Each task is independently testable and should be committed separately.

**Tech Stack:** Go, Vue, TypeScript, Wails, SQLite, GitHub Actions, npm.

---

## File Structure

- Modify `requestlog_mock_test.go`: isolate the request-log seed test from the real user database.
- Modify `frontend/package.json`: declare runtime and package-manager constraints.
- Keep `frontend/package-lock.json`: use npm as the single frontend lock source.
- Delete `frontend/pnpm-lock.yaml`: remove the unused second lock source after confirming npm is the chosen manager.
- Modify `.github/workflows/release.yml`: use reproducible installs, pin the desktop build tool, and add quality gates.
- Modify `services/providerrelay.go`: make relay startup return bind errors synchronously.
- Modify `main.go`: call relay startup synchronously and log shutdown errors.
- Modify `services/providerservice.go`: validate safe custom tool identifiers before file-path construction.
- Modify `services/customcliservice.go`: reuse the same identifier validation for provider storage paths.
- Add `services/provider_path_test.go`: cover unsafe custom provider kind values.
- Modify `services/healthcheckservice.go`: make batch health-check timeout respect per-provider timeout.
- Add `services/healthcheck_timeout_test.go`: cover timeout calculation.
- Modify `services/updateservice.go`: parse and validate `Content-Range` strictly.
- Add `services/updateservice_range_test.go`: cover valid and invalid range responses.
- Add `services/dbqueue_test.go`: cover write queue success, batch failure, and shutdown behavior.
- Modify `.gitignore`: block local binaries, temporary results, local databases, and malformed scratch files.

## Task 1: Isolate Request-Log Seed Test

**Files:**
- Modify: `requestlog_mock_test.go`

- [ ] **Step 1: Replace real-home initialization with a test helper**

Change the top of `requestlog_mock_test.go` so it no longer has package-level database initialization. Keep these imports:

```go
import (
	"database/sql"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daodao97/xgo/xdb"
	_ "modernc.org/sqlite"
)
```

Add this helper below `const timeLayout = "2006-01-02 15:04:05"`:

```go
func setupRequestLogSeedTestDB(t *testing.T) *sql.DB {
	t.Helper()

	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	configDir := filepath.Join(tmpHome, ".code-switch")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("create test config dir: %v", err)
	}

	dbPath := filepath.Join(configDir, "app.db?cache=shared&mode=rwc")
	if err := xdb.Inits([]xdb.Config{{Name: "default", Driver: "sqlite", DSN: dbPath}}); err != nil {
		t.Fatalf("init test database: %v", err)
	}

	db, err := xdb.DB("default")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}

	schema := `CREATE TABLE IF NOT EXISTS request_log (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		platform TEXT,
		model TEXT,
		provider TEXT,
		http_code INTEGER,
		input_tokens INTEGER,
		output_tokens INTEGER,
		cache_create_tokens INTEGER,
		cache_read_tokens INTEGER,
		reasoning_tokens INTEGER,
		is_stream INTEGER DEFAULT 0,
		duration_sec REAL DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create request_log table: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	return db
}
```

- [ ] **Step 2: Update the seed test to use the helper**

Change the start of `TestSeedMockRequestLogs` to:

```go
func TestSeedMockRequestLogs(t *testing.T) {
	db := setupRequestLogSeedTestDB(t)
	if _, err := db.Exec("DELETE FROM request_log"); err != nil {
		t.Fatalf("clear test rows: %v", err)
	}
	if err := SeedMockRequestLogs(16); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
```

- [ ] **Step 3: Replace sensitive sample names in seed data**

Use neutral platform and model names in `SeedMockRequestLogs`:

```go
platModels := map[string][]string{
	"tool_a": {
		"model-a-fast",
		"model-a-large",
		"model-a-small",
	},
	"tool_b": {
		"model-b-fast",
		"model-b-large",
	},
}
providers := map[string][]string{
	"tool_a": {"provider-a", "provider-b", "provider-c"},
	"tool_b": {"provider-a"},
}
```

- [ ] **Step 4: Verify only the root package test**

Run:

```powershell
go test -timeout 60s .
```

Expected: pass, and no write to the real `.code-switch/app.db`.

- [ ] **Step 5: Commit**

```powershell
git add requestlog_mock_test.go
git commit -m "fix: isolate request log seed test"
```

## Task 2: Make Frontend Tooling Reproducible

**Files:**
- Modify: `frontend/package.json`
- Delete: `frontend/pnpm-lock.yaml`

- [ ] **Step 1: Declare runtime and package manager**

Add these fields to `frontend/package.json` after `"type": "module"`:

```json
  "packageManager": "npm@10.8.2",
  "engines": {
    "node": ">=22.12.0 <23",
    "npm": ">=10.8.0 <11"
  },
```

- [ ] **Step 2: Confirm npm is the single package manager**

Run:

```powershell
cd frontend
npm ci
npx vue-tsc --noEmit
npm run build
```

Expected: install uses `package-lock.json`; type check passes; build passes under Node 22.12 or newer.

- [ ] **Step 3: Remove the unused lock file**

After confirming npm is the selected manager:

```powershell
git rm frontend/pnpm-lock.yaml
```

- [ ] **Step 4: Commit**

```powershell
git add frontend/package.json frontend/package-lock.json
git commit -m "chore: pin frontend runtime"
```

## Task 3: Add Release Quality Gates

**Files:**
- Modify: `.github/workflows/release.yml`

- [ ] **Step 1: Replace non-reproducible frontend install**

In all release jobs, replace:

```yaml
      - name: Install frontend dependencies
        run: cd frontend && npm install
```

with:

```yaml
      - name: Install frontend dependencies
        run: cd frontend && npm ci
```

- [ ] **Step 2: Pin the desktop build tool**

In all release jobs, replace:

```yaml
      - name: Install Wails
        run: go install github.com/wailsapp/wails/v3/cmd/wails3@latest
```

with:

```yaml
      - name: Install Wails
        run: go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha.38
```

- [ ] **Step 3: Add backend and frontend gates before packaging**

Add these steps after frontend dependency installation in each build job:

```yaml
      - name: Run backend tests
        run: go test -timeout 60s ./...

      - name: Run backend vet
        run: go vet ./...

      - name: Run frontend typecheck
        run: cd frontend && npx vue-tsc --noEmit
```

- [ ] **Step 4: Verify workflow syntax locally where possible**

Run:

```powershell
git diff -- .github/workflows/release.yml
```

Expected: every build job uses pinned tool install, `npm ci`, and three quality gates.

- [ ] **Step 5: Commit**

```powershell
git add .github/workflows/release.yml
git commit -m "ci: add release quality gates"
```

## Task 4: Return Local Relay Startup Errors

**Files:**
- Modify: `services/providerrelay.go`
- Modify: `main.go`
- Test: `services/providerrelay_start_test.go`

- [ ] **Step 1: Add a failing startup test**

Create `services/providerrelay_start_test.go`:

```go
package services

import (
	"net"
	"strings"
	"testing"
)

func TestProviderRelayStartReturnsBindError(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	defer listener.Close()

	relay := &ProviderRelayService{
		providerService: NewProviderService(),
		addr:            listener.Addr().String(),
		rrLastStart:     make(map[string]string),
	}

	err = relay.Start()
	if err == nil {
		t.Fatal("expected bind error")
	}
	if !strings.Contains(err.Error(), "listen") && !strings.Contains(err.Error(), "bind") {
		t.Fatalf("expected bind/listen error, got %v", err)
	}
}
```

- [ ] **Step 2: Run the failing test**

Run:

```powershell
go test -timeout 60s ./services -run TestProviderRelayStartReturnsBindError -count=1
```

Expected before implementation: fail because `Start()` returns nil.

- [ ] **Step 3: Implement synchronous bind**

In `services/providerrelay.go`, add `"net"` to imports and replace `Start()` with:

```go
func (prs *ProviderRelayService) Start() error {
	if warnings := prs.validateConfig(); len(warnings) > 0 {
		fmt.Println("======== Provider 配置验证警告 ========")
		for _, warn := range warnings {
			fmt.Printf("⚠️  %s\n", warn)
		}
		fmt.Println("========================================")
	}

	router := gin.Default()
	prs.registerRoutes(router)

	listener, err := net.Listen("tcp", prs.addr)
	if err != nil {
		return fmt.Errorf("listen provider relay on %s: %w", prs.addr, err)
	}

	prs.server = &http.Server{
		Addr:    prs.addr,
		Handler: router,
	}

	fmt.Printf("provider relay server listening on %s\n", listener.Addr().String())

	go func() {
		if err := prs.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Printf("provider relay server error: %v\n", err)
		}
	}()
	return nil
}
```

- [ ] **Step 4: Call startup synchronously**

In `main.go`, replace the outer goroutine:

```go
	go func() {
		if err := providerRelay.Start(); err != nil {
			log.Printf("provider relay start error: %v", err)
		}
	}()
```

with:

```go
	if err := providerRelay.Start(); err != nil {
		log.Fatalf("provider relay start error: %v", err)
	}
```

Also replace:

```go
		_ = providerRelay.Stop()
```

with:

```go
		if err := providerRelay.Stop(); err != nil {
			log.Printf("provider relay stop error: %v", err)
		}
```

- [ ] **Step 5: Verify**

Run:

```powershell
go test -timeout 60s ./services -run TestProviderRelayStartReturnsBindError -count=1
go test -timeout 60s ./services
```

Expected: both pass.

- [ ] **Step 6: Commit**

```powershell
git add main.go services/providerrelay.go services/providerrelay_start_test.go
git commit -m "fix: report relay startup failures"
```

## Task 5: Validate Custom Tool Identifiers

**Files:**
- Modify: `services/providerservice.go`
- Modify: `services/customcliservice.go`
- Test: `services/provider_path_test.go`

- [ ] **Step 1: Add failing path-safety tests**

Create `services/provider_path_test.go`:

```go
package services

import "testing"

func TestProviderFilePathRejectsUnsafeCustomKind(t *testing.T) {
	tests := []string{
		"custom:../outside",
		"custom:..\\outside",
		"custom:/absolute",
		"custom:a/b",
		"custom:a:b",
		"custom:",
	}

	for _, kind := range tests {
		t.Run(kind, func(t *testing.T) {
			tmpHome := t.TempDir()
			t.Setenv("HOME", tmpHome)
			t.Setenv("USERPROFILE", tmpHome)

			if _, err := providerFilePath(kind); err == nil {
				t.Fatalf("expected unsafe kind %q to fail", kind)
			}
		})
	}
}

func TestProviderFilePathAcceptsSafeCustomKind(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	if _, err := providerFilePath("custom:tool_01-safe"); err != nil {
		t.Fatalf("expected safe custom kind to pass: %v", err)
	}
}
```

- [ ] **Step 2: Run failing tests**

Run:

```powershell
go test -timeout 60s ./services -run TestProviderFilePath -count=1
```

Expected before implementation: unsafe cases fail to reject.

- [ ] **Step 3: Add shared identifier validation**

In `services/providerservice.go`, add `regexp` to imports and add:

```go
var safeCustomToolIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func validateCustomToolID(toolID string) error {
	if !safeCustomToolIDPattern.MatchString(toolID) {
		return fmt.Errorf("invalid custom tool id: %s", toolID)
	}
	return nil
}
```

Inside `providerFilePath`, immediately after `toolId := strings.TrimPrefix(kind, "custom:")`, replace the empty check with:

```go
			if err := validateCustomToolID(toolId); err != nil {
				return "", err
			}
```

- [ ] **Step 4: Make custom provider path construction return errors**

In `services/customcliservice.go`, change:

```go
func (s *CustomCliService) getProvidersPath(toolId string) string {
	return filepath.Join(s.getProvidersDir(), toolId+".json")
}
```

to:

```go
func (s *CustomCliService) getProvidersPath(toolId string) (string, error) {
	if err := validateCustomToolID(toolId); err != nil {
		return "", err
	}
	return filepath.Join(s.getProvidersDir(), toolId+".json"), nil
}
```

Update the delete call:

```go
	providersPath, err := s.getProvidersPath(id)
	if err != nil {
		return err
	}
	_ = os.Remove(providersPath)
```

- [ ] **Step 5: Verify**

Run:

```powershell
go test -timeout 60s ./services -run TestProviderFilePath -count=1
go test -timeout 60s ./services
```

Expected: pass.

- [ ] **Step 6: Commit**

```powershell
git add services/providerservice.go services/customcliservice.go services/provider_path_test.go
git commit -m "fix: validate custom provider paths"
```

## Task 6: Respect Per-Provider Health-Check Timeout

**Files:**
- Modify: `services/healthcheckservice.go`
- Test: `services/healthcheck_timeout_test.go`

- [ ] **Step 1: Add timeout calculation tests**

Create `services/healthcheck_timeout_test.go`:

```go
package services

import (
	"testing"
	"time"
)

func TestBatchCheckTimeoutUsesLargestEnabledProviderTimeout(t *testing.T) {
	providers := []Provider{
		{AvailabilityMonitorEnabled: true, AvailabilityConfig: &AvailabilityConfig{Timeout: 45000}},
		{AvailabilityMonitorEnabled: true, AvailabilityConfig: &AvailabilityConfig{Timeout: 15000}},
		{AvailabilityMonitorEnabled: false, AvailabilityConfig: &AvailabilityConfig{Timeout: 120000}},
	}

	got := batchCheckTimeout(providers)
	want := 50 * time.Second
	if got != want {
		t.Fatalf("batchCheckTimeout=%s want %s", got, want)
	}
}

func TestBatchCheckTimeoutUsesDefaultWhenNoEnabledProviders(t *testing.T) {
	got := batchCheckTimeout([]Provider{{AvailabilityMonitorEnabled: false}})
	want := time.Duration(DefaultTimeoutMs)*time.Millisecond + 5*time.Second
	if got != want {
		t.Fatalf("batchCheckTimeout=%s want %s", got, want)
	}
}
```

- [ ] **Step 2: Run failing tests**

Run:

```powershell
go test -timeout 60s ./services -run TestBatchCheckTimeout -count=1
```

Expected before implementation: fail because helper does not exist.

- [ ] **Step 3: Add helper and use it**

In `services/healthcheckservice.go`, add:

```go
func batchCheckTimeout(providers []Provider) time.Duration {
	maxTimeout := DefaultTimeoutMs
	for i := range providers {
		if !providers[i].AvailabilityMonitorEnabled {
			continue
		}
		timeout := DefaultTimeoutMs
		if providers[i].AvailabilityConfig != nil && providers[i].AvailabilityConfig.Timeout > 0 {
			timeout = providers[i].AvailabilityConfig.Timeout
		}
		if timeout > maxTimeout {
			maxTimeout = timeout
		}
	}
	return time.Duration(maxTimeout)*time.Millisecond + 5*time.Second
}
```

In `checkAllProviders`, replace:

```go
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
```

with:

```go
	ctx, cancel := context.WithTimeout(context.Background(), batchCheckTimeout(providers))
```

- [ ] **Step 4: Verify**

Run:

```powershell
go test -timeout 60s ./services -run TestBatchCheckTimeout -count=1
go test -timeout 60s ./services
```

Expected: pass.

- [ ] **Step 5: Commit**

```powershell
git add services/healthcheckservice.go services/healthcheck_timeout_test.go
git commit -m "fix: respect health check timeouts"
```

## Task 7: Validate Download Range Responses

**Files:**
- Modify: `services/updateservice.go`
- Test: `services/updateservice_range_test.go`

- [ ] **Step 1: Add parser tests**

Create `services/updateservice_range_test.go`:

```go
package services

import "testing"

func TestParseContentRange(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		wantStart int64
		wantEnd   int64
		wantTotal int64
		wantOK    bool
	}{
		{name: "valid", value: "bytes 10-99/200", wantStart: 10, wantEnd: 99, wantTotal: 200, wantOK: true},
		{name: "missing total", value: "bytes 10-99/*", wantOK: false},
		{name: "wrong unit", value: "items 10-99/200", wantOK: false},
		{name: "end before start", value: "bytes 99-10/200", wantOK: false},
		{name: "end beyond total", value: "bytes 10-200/200", wantOK: false},
		{name: "malformed", value: "bad", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, total, ok := parseContentRange(tt.value)
			if ok != tt.wantOK {
				t.Fatalf("ok=%v want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if start != tt.wantStart || end != tt.wantEnd || total != tt.wantTotal {
				t.Fatalf("got %d-%d/%d want %d-%d/%d", start, end, total, tt.wantStart, tt.wantEnd, tt.wantTotal)
			}
		})
	}
}
```

- [ ] **Step 2: Run failing tests**

Run:

```powershell
go test -timeout 60s ./services -run TestParseContentRange -count=1
```

Expected before implementation: fail because helper does not exist.

- [ ] **Step 3: Add strict parser**

In `services/updateservice.go`, add:

```go
func parseContentRange(value string) (int64, int64, int64, bool) {
	var unit string
	var start int64
	var end int64
	var total int64
	n, err := fmt.Sscanf(value, "%s %d-%d/%d", &unit, &start, &end, &total)
	if err != nil || n != 4 || unit != "bytes" {
		return 0, 0, 0, false
	}
	if start < 0 || end < start || total <= 0 || end >= total {
		return 0, 0, 0, false
	}
	return start, end, total, true
}
```

- [ ] **Step 4: Use parser in download path**

Replace the current `Content-Range` parsing block with:

```go
		contentRange := resp.Header.Get("Content-Range")
		if contentRange != "" {
			rangeStart, _, rangeTotal, ok := parseContentRange(contentRange)
			if !ok || rangeStart != startByte || rangeTotal != info.Size {
				_ = os.Remove(tempPath)
				file, err = os.Create(tempPath)
				startByte = 0
			} else {
				file, err = os.OpenFile(tempPath, os.O_APPEND|os.O_WRONLY, 0o644)
			}
		} else {
			_ = os.Remove(tempPath)
			file, err = os.Create(tempPath)
			startByte = 0
		}
```

- [ ] **Step 5: Verify**

Run:

```powershell
go test -timeout 60s ./services -run TestParseContentRange -count=1
go test -timeout 60s ./services
```

Expected: pass.

- [ ] **Step 6: Commit**

```powershell
git add services/updateservice.go services/updateservice_range_test.go
git commit -m "fix: validate download ranges"
```

## Task 8: Add Write Queue Regression Tests

**Files:**
- Add: `services/dbqueue_test.go`

- [ ] **Step 1: Add queue tests**

Create `services/dbqueue_test.go`:

```go
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
```

- [ ] **Step 2: Verify**

Run:

```powershell
go test -timeout 60s ./services -run TestDBWriteQueue -count=1
go test -timeout 60s ./services
```

Expected: pass.

- [ ] **Step 3: Commit**

```powershell
git add services/dbqueue_test.go
git commit -m "test: cover database write queue"
```

## Task 9: Clean Ignore Rules

**Files:**
- Modify: `.gitignore`

- [ ] **Step 1: Expand ignore rules**

Replace `.gitignore` with:

```gitignore
frontend/node_modules/
frontend/dist/
node_modules/
dist/
build/
bin/
coverage/

.DS_Store
Thumbs.db
.task/
.cache/
.pytest_cache/
.mypy_cache/
.ruff_cache/

*.exe
*.log
*.db
*.sqlite
*.sqlite3

test-results-*.json
multi-provider-test-*.json
test_*.go.bak
test_wsl_*.go
test_wsl_*.sh

nul
%s
currentTextareaRef
temp.txt

.env
.env.*
*.pem
*.key
*.p12
id_rsa*
.ace-tool/
```

- [ ] **Step 2: Review currently untracked files before deletion**

Run:

```powershell
git status --short
```

Expected: intentional source files remain visible; local binaries and scratch outputs are ignored.

- [ ] **Step 3: Remove local scratch files only after explicit approval**

Use native PowerShell cleanup only after confirming the exact paths. Do not remove files in this task without a separate approval step.

- [ ] **Step 4: Commit ignore rules**

```powershell
git add .gitignore
git commit -m "chore: ignore local build artifacts"
```

## Task 10: Full Verification

**Files:**
- No direct file edits.

- [ ] **Step 1: Run backend checks**

Run:

```powershell
go test -timeout 60s ./...
go vet ./...
```

Expected: both pass. Confirm the root package test no longer touches the real user database.

- [ ] **Step 2: Run frontend checks under pinned Node**

Run:

```powershell
cd frontend
npm ci
npx vue-tsc --noEmit
npm run build
```

Expected: all pass under Node 22.12 or newer.

- [ ] **Step 3: Inspect final diff**

Run:

```powershell
git status --short
git diff --stat HEAD
```

Expected: only intentional source, test, workflow, and ignore-rule changes are present.

- [ ] **Step 4: Commit verification note if needed**

If no extra changes are needed, do not create an empty commit. If verification requires a small documentation update, commit it:

```powershell
git add <verified-doc-file>
git commit -m "docs: record remediation verification"
```

## Self-Review

- Spec coverage: P0 real-data test risk is Task 1; frontend reproducibility is Task 2; release gates are Task 3; relay startup is Task 4; custom path boundary is Task 5; health-check timeout is Task 6; download range parsing is Task 7; queue coverage is Task 8; worktree hygiene is Task 9; full verification is Task 10.
- Placeholder scan: no deferred implementation markers are used.
- Type consistency: helper and test names are consistent across tasks.
