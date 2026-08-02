package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daodao97/xgo/xdb"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// ========== 抓包会话 ==========
//
// 一次"开启抓包 → 关闭抓包"是一个会话（capture_session 表一行），
// 期间捕获的 request_log 行以 capture_session_id 关联。会话数据跨重启保留，
// 但录制开关本身仍是进程内状态（重启即关，见 SetRequestCapture）。
// 会话状态（当前会话 id、已删除会话墓碑）由 ProviderRelayService 独占管理，
// 所有变更遵循"事务提交在前、内存状态变更在后"：SQL 失败时内存不动。

// captureRowPredicate 判断 request_log 行是否录有抓包内容的统一谓词。
// 会话行的 capture_session_id 恒非 0；旧版抓包数据（迁移前）落在 0 上，
// 与普通日志行共享 0 值，因此 0 号伪会话的任何查询都必须叠加本谓词
const captureRowPredicate = `(request_headers != '' OR request_body != '' OR body_truncated != 0 OR body_bytes != 0)`

// captureStripSet 清除抓包内容的统一 SET 子句（同时摘除会话关联）
const captureStripSet = `request_headers = '', request_body = '', body_truncated = 0, body_bytes = 0, capture_session_id = 0`

// CaptureSessionInfo 会话列表项。Legacy=true 表示 0 号伪会话（迁移前旧数据）
type CaptureSessionInfo struct {
	ID           int64  `json:"id"`
	StartedAt    string `json:"started_at"`
	EndedAt      string `json:"ended_at"`
	Interrupted  bool   `json:"interrupted"`
	Legacy       bool   `json:"legacy"`
	Active       bool   `json:"active"`
	RequestCount int64  `json:"request_count"`
}

// CaptureSessionLogRow 会话内请求的轻量行（不携带 headers/body 大字段）
type CaptureSessionLogRow struct {
	ID            int64   `json:"id"`
	CreatedAt     string  `json:"created_at"`
	Platform      string  `json:"platform"`
	Provider      string  `json:"provider"`
	Model         string  `json:"model"`
	HttpCode      int     `json:"http_code"`
	IsStream      bool    `json:"is_stream"`
	DurationSec   float64 `json:"duration_sec"`
	BodyBytes     int     `json:"body_bytes"`
	BodyTruncated bool    `json:"body_truncated"`
}

// CaptureExportResult 导出结果
type CaptureExportResult struct {
	Path     string `json:"path"`
	Count    int    `json:"count"`
	Canceled bool   `json:"canceled"`
}

// ensureCaptureSessionTable 建表 + 会话相关迁移（capture_session_id 列在
// request_log 的迁移清单里补，此处只管会话表与索引）
func ensureCaptureSessionTable(db *sql.DB) error {
	const createSQL = `CREATE TABLE IF NOT EXISTS capture_session (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		started_at DATETIME,
		ended_at DATETIME,
		interrupted INTEGER DEFAULT 0
	)`
	if _, err := db.Exec(createSQL); err != nil {
		return fmt.Errorf("创建 capture_session 表失败: %w", err)
	}
	// 部分索引只覆盖非 0 会话行；查询侧必须显式带 capture_session_id != 0
	// 才能命中（参数化谓词下 SQLite 不会自动推导可用性）
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_request_log_capture_session
		ON request_log(capture_session_id) WHERE capture_session_id != 0`); err != nil {
		return fmt.Errorf("创建抓包会话索引失败: %w", err)
	}
	return nil
}

// recoverStaleCaptureSessions 把上个进程遗留的"未关闭"会话标记为已中断。
// 结束时间取该会话最后一条捕获的时间，没有则取开始时间。
// 只在进程首次触碰会话状态时执行一次（Start 可被前端重复触发，不能挂在那里）
func (prs *ProviderRelayService) recoverStaleCaptureSessions() {
	prs.captureRecoverOnce.Do(func() {
		db, err := xdb.DB("default")
		if err != nil {
			fmt.Printf("[Capture] 恢复遗留会话失败(db): %v\n", err)
			return
		}
		if _, err := db.Exec(`UPDATE capture_session SET interrupted = 1,
			ended_at = COALESCE(
				(SELECT MAX(created_at) FROM request_log
				 WHERE capture_session_id = capture_session.id AND capture_session_id != 0),
				started_at)
			WHERE ended_at IS NULL`); err != nil {
			fmt.Printf("[Capture] 恢复遗留会话失败: %v\n", err)
		}
	})
}

// SetRequestCapture 设置抓包模式开关。录制开关为进程内状态、重启即关
// （调试态功能，不持久化可避免用户遗忘后长期落盘敏感请求内容）；
// 会话数据落库保留。开启即建会话，关闭即封会话。
// 顺序约束：开启时先提交会话行再置位开关，否则竞态下捕获行会落到 0 号
// 伪会话里；关闭时先摘开关再封会话
func (prs *ProviderRelayService) SetRequestCapture(enabled bool) error {
	prs.captureWriteMu.Lock()
	defer prs.captureWriteMu.Unlock()

	if enabled == prs.captureRequests.Load() {
		return nil
	}
	db, err := xdb.DB("default")
	if err != nil {
		return err
	}

	if enabled {
		prs.recoverStaleCaptureSessions()
		res, err := db.Exec(`INSERT INTO capture_session (started_at) VALUES (?)`, captureNowUTC())
		if err != nil {
			return fmt.Errorf("创建抓包会话失败: %w", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("读取会话 ID 失败: %w", err)
		}
		prs.captureSessionID.Store(id)
		prs.captureRequests.Store(true)
		fmt.Printf("[Capture] 抓包模式已开启（会话 #%d）：后续转发的出站请求头/正文（脱敏后）将写入请求日志\n", id)
		return nil
	}

	// 关闭：SQL 提交在前、内存状态变更在后（提交失败时录制保持开启并报错，
	// 不留下"开关已关但会话未封"的错位状态）。写锁隔离了并发采集，
	// 不存在"封了会话还有新行进来"的窗口
	sessionID := prs.captureSessionID.Load()
	if sessionID != 0 {
		if _, err := db.Exec(`UPDATE capture_session SET ended_at = ?, interrupted = 0 WHERE id = ?`,
			captureNowUTC(), sessionID); err != nil {
			return fmt.Errorf("结束抓包会话失败: %w", err)
		}
	}
	prs.captureRequests.Store(false)
	prs.captureSessionID.Store(0)
	fmt.Printf("[Capture] 抓包模式已关闭（历史抓包数据保留，可在抓包页删除或导出）\n")
	return nil
}

// closeActiveCaptureSession 优雅关停时封存活动会话（interrupted=0）。
// 由 relay Stop 调用；失败仅打日志，不阻塞退出
func (prs *ProviderRelayService) closeActiveCaptureSession() {
	prs.captureWriteMu.Lock()
	defer prs.captureWriteMu.Unlock()
	if !prs.captureRequests.Load() {
		return
	}
	sessionID := prs.captureSessionID.Load()
	if sessionID != 0 {
		if db, err := xdb.DB("default"); err == nil {
			if _, err := db.Exec(`UPDATE capture_session SET ended_at = ?, interrupted = 0 WHERE id = ?`,
				captureNowUTC(), sessionID); err != nil {
				fmt.Printf("[Capture] 关停时结束会话失败: %v\n", err)
			}
		}
	}
	prs.captureRequests.Store(false)
	prs.captureSessionID.Store(0)
}

// captureSnapshot 在读锁内一次性快照采集所需状态。
// 三个字段若在锁外分别读取，与关闭/清除竞态时可能拼出
// "开关已开 + 会话已清零"的组合，把敏感内容写进 0 号旧数据桶
func (prs *ProviderRelayService) captureSnapshot() (enabled bool, sessionID int64, gen int64) {
	prs.captureWriteMu.RLock()
	defer prs.captureWriteMu.RUnlock()
	return prs.captureRequests.Load(), prs.captureSessionID.Load(), prs.captureClearGen.Load()
}

// ListCaptureSessions 列出全部会话（新会话在前），含 0 号伪会话（仅当存在旧数据）
func (prs *ProviderRelayService) ListCaptureSessions() ([]CaptureSessionInfo, error) {
	prs.recoverStaleCaptureSessions()
	db, err := xdb.DB("default")
	if err != nil {
		return nil, err
	}
	// LEFT JOIN：刚开启、还没有任何捕获的会话也必须出现在列表里
	rows, err := db.Query(`SELECT s.id, COALESCE(s.started_at, ''), COALESCE(s.ended_at, ''),
			s.interrupted, COUNT(r.id)
		FROM capture_session s
		LEFT JOIN request_log r ON r.capture_session_id = s.id AND r.capture_session_id != 0
		GROUP BY s.id ORDER BY s.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	activeID := prs.captureSessionID.Load()
	capturing := prs.captureRequests.Load()
	sessions := make([]CaptureSessionInfo, 0, 16)
	for rows.Next() {
		var s CaptureSessionInfo
		var interrupted int
		if err := rows.Scan(&s.ID, &s.StartedAt, &s.EndedAt, &interrupted, &s.RequestCount); err != nil {
			return nil, err
		}
		s.Interrupted = interrupted != 0
		s.Active = capturing && s.ID == activeID
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 0 号伪会话：迁移前的旧抓包数据。必须叠加抓包谓词，否则会把普通日志当抓包
	var legacyCount int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM request_log
		WHERE capture_session_id = 0 AND ` + captureRowPredicate).Scan(&legacyCount); err == nil && legacyCount > 0 {
		sessions = append(sessions, CaptureSessionInfo{ID: 0, Legacy: true, RequestCount: legacyCount})
	}
	return sessions, nil
}

// GetCaptureSessionLogs 读取会话内的轻量请求行。
// sinceID>0：增量模式，返回 id > sinceID 的新行（升序），供录制中的会话轮询追加；
// 否则：初始/翻页模式，返回 id < beforeID（beforeID<=0 视为不设上界）的最新行（降序）。
// limit 兜底 200、上限 500
func (prs *ProviderRelayService) GetCaptureSessionLogs(sessionID int64, sinceID int64, beforeID int64, limit int) ([]CaptureSessionLogRow, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	db, err := xdb.DB("default")
	if err != nil {
		return nil, err
	}

	where := `capture_session_id = ? AND capture_session_id != 0`
	if sessionID == 0 {
		// 0 号伪会话必须叠加抓包谓词（与普通日志共享 0 值）
		where = `capture_session_id = 0 AND ` + captureRowPredicate
	}
	args := []interface{}{}
	if sessionID != 0 {
		args = append(args, sessionID)
	}
	order := `ORDER BY id DESC`
	if sinceID > 0 {
		where += ` AND id > ?`
		args = append(args, sinceID)
		order = `ORDER BY id ASC`
	} else if beforeID > 0 {
		where += ` AND id < ?`
		args = append(args, beforeID)
	}
	args = append(args, limit)

	rows, err := db.Query(`SELECT id, COALESCE(created_at, ''), COALESCE(platform, ''), COALESCE(provider, ''), COALESCE(model, ''),
			COALESCE(http_code, 0), COALESCE(is_stream, 0), COALESCE(duration_sec, 0), COALESCE(body_bytes, 0), COALESCE(body_truncated, 0)
		FROM request_log WHERE `+where+` `+order+` LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]CaptureSessionLogRow, 0, limit)
	for rows.Next() {
		var r CaptureSessionLogRow
		var isStream, truncated int
		if err := rows.Scan(&r.ID, &r.CreatedAt, &r.Platform, &r.Provider, &r.Model,
			&r.HttpCode, &isStream, &r.DurationSec, &r.BodyBytes, &truncated); err != nil {
			return nil, err
		}
		r.IsStream = isStream != 0
		r.BodyTruncated = truncated != 0
		result = append(result, r)
	}
	return result, rows.Err()
}

// DeleteCaptureSession 删除单个会话：清除其捕获内容（保留统计行本身）并移除
// 会话元数据。删除的是活动会话时原地轮换出新会话（录制不中断、白纸重来）。
// 事务提交在前、内存状态（墓碑/活动会话 id）变更在后；回滚时内存不动。
// 墓碑兜住在途长流请求：采集发生在请求开始，落库时校验会话已删则自我置空
func (prs *ProviderRelayService) DeleteCaptureSession(sessionID int64) (int64, error) {
	prs.captureWriteMu.Lock()
	defer prs.captureWriteMu.Unlock()

	db, err := xdb.DB("default")
	if err != nil {
		return 0, err
	}

	if sessionID == 0 {
		// 0 号伪会话：只清旧数据行，无会话元数据、无在途写入（旧数据不会再产生）
		res, err := db.Exec(`UPDATE request_log SET ` + captureStripSet +
			` WHERE capture_session_id = 0 AND ` + captureRowPredicate)
		if err != nil {
			return 0, err
		}
		affected, _ := res.RowsAffected()
		return affected, nil
	}

	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// 显式带 capture_session_id != 0 才能命中部分索引（参数化谓词不被自动推导）
	res, err := tx.Exec(`UPDATE request_log SET `+captureStripSet+
		` WHERE capture_session_id = ? AND capture_session_id != 0`, sessionID)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	if _, err := tx.Exec(`DELETE FROM capture_session WHERE id = ?`, sessionID); err != nil {
		return 0, err
	}

	rotated := int64(0)
	if prs.captureRequests.Load() && prs.captureSessionID.Load() == sessionID {
		r, err := tx.Exec(`INSERT INTO capture_session (started_at) VALUES (?)`, captureNowUTC())
		if err != nil {
			return 0, err
		}
		if rotated, err = r.LastInsertId(); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}

	// 提交成功后才变更内存状态
	prs.captureDeletedSessions[sessionID] = struct{}{}
	if rotated != 0 {
		prs.captureSessionID.Store(rotated)
	} else if prs.captureSessionID.Load() == sessionID {
		prs.captureSessionID.Store(0)
	}
	return affected, nil
}

// ClearCapturedRequests 清空全部抓包数据：所有会话 + 0 号旧数据的捕获内容
// 一并清除（保留统计行本身），会话元数据整表删除；录制中则轮换出新会话。
// 全局清除以代次推进兜在途行（任何旧代次行落库时自我置空），返回清理行数
func (prs *ProviderRelayService) ClearCapturedRequests() (int64, error) {
	prs.captureWriteMu.Lock()
	defer prs.captureWriteMu.Unlock()

	db, err := xdb.DB("default")
	if err != nil {
		return 0, err
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`UPDATE request_log SET ` + captureStripSet + ` WHERE ` + captureRowPredicate)
	if err != nil {
		return 0, err
	}
	affected, _ := res.RowsAffected()
	if _, err := tx.Exec(`DELETE FROM capture_session`); err != nil {
		return 0, err
	}
	rotated := int64(0)
	if prs.captureRequests.Load() {
		r, err := tx.Exec(`INSERT INTO capture_session (started_at) VALUES (?)`, captureNowUTC())
		if err != nil {
			return 0, err
		}
		if rotated, err = r.LastInsertId(); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}

	// 提交成功后才推进代次并轮换：写侧以读锁包住"代次校验 + 提交"，
	// 在途行要么在本次 UPDATE 前已完整提交（被清掉），要么在写锁释放后
	// 校验（读到新代次而自我置空）
	prs.captureClearGen.Add(1)
	prs.captureSessionID.Store(rotated)
	return affected, nil
}

// captureExportEntry 导出文件中的单条请求
type captureExportEntry struct {
	ID             int64           `json:"id"`
	CreatedAt      string          `json:"created_at"`
	Platform       string          `json:"platform"`
	Provider       string          `json:"provider"`
	Model          string          `json:"model"`
	HttpCode       int             `json:"http_code"`
	IsStream       bool            `json:"is_stream"`
	DurationSec    float64         `json:"duration_sec"`
	RequestHeaders json.RawMessage `json:"request_headers,omitempty"`
	RequestBody    string          `json:"request_body,omitempty"`
	BodyTruncated  bool            `json:"body_truncated"`
	BodyBytes      int             `json:"body_bytes"`
}

// ExportCaptureSessionWithDialog 弹系统保存对话框并导出指定会话的全部捕获内容。
// 录制中的会话同样可导出（导出已提交的部分）。
// 内容在采集时已做认证脱敏，但正文仍可能包含敏感的提示词内容（前端文案有提示）。
// 单会话可达数百 MB（64KB/条 × 数千条），因此不整载进内存：
// 对话框确认后开只读事务逐行流式写入目标目录内的临时文件，成功后原子替换
func (prs *ProviderRelayService) ExportCaptureSessionWithDialog(sessionID int64) (CaptureExportResult, error) {
	db, err := xdb.DB("default")
	if err != nil {
		return CaptureExportResult{}, err
	}

	// 会话元数据先取好：无效会话不弹对话框
	var meta CaptureSessionInfo
	if sessionID == 0 {
		var legacyCount int64
		if err := db.QueryRow(`SELECT COUNT(*) FROM request_log
			WHERE capture_session_id = 0 AND ` + captureRowPredicate).Scan(&legacyCount); err != nil {
			return CaptureExportResult{}, err
		}
		if legacyCount == 0 {
			return CaptureExportResult{}, fmt.Errorf("没有可导出的旧抓包数据")
		}
		meta = CaptureSessionInfo{ID: 0, Legacy: true, RequestCount: legacyCount}
	} else {
		var interrupted int
		err := db.QueryRow(`SELECT id, COALESCE(started_at, ''), COALESCE(ended_at, ''), interrupted
			FROM capture_session WHERE id = ?`, sessionID).
			Scan(&meta.ID, &meta.StartedAt, &meta.EndedAt, &interrupted)
		if err == sql.ErrNoRows {
			return CaptureExportResult{}, fmt.Errorf("会话不存在或已被删除")
		}
		if err != nil {
			return CaptureExportResult{}, err
		}
		meta.Interrupted = interrupted != 0
		meta.Active = prs.captureRequests.Load() && prs.captureSessionID.Load() == sessionID
	}

	dialog := application.SaveFileDialog().
		SetFilename(fmt.Sprintf("capture-session-%d-%s.json", sessionID, time.Now().Format("20060102-150405"))).
		AddFilter("JSON 抓包数据 (*.json)", "*.json").
		CanCreateDirectories(true)
	if app := application.Get(); app != nil {
		if w := app.Window.Current(); w != nil {
			dialog.AttachToWindow(w)
		}
	}
	path, err := dialog.PromptForSingleSelection()
	if err != nil {
		return CaptureExportResult{}, fmt.Errorf("打开保存对话框失败: %w", err)
	}
	if strings.TrimSpace(path) == "" {
		return CaptureExportResult{Canceled: true}, nil
	}

	count, err := prs.streamCaptureExport(db, sessionID, meta, path)
	if err != nil {
		return CaptureExportResult{}, err
	}
	return CaptureExportResult{Path: path, Count: count}, nil
}

// streamCaptureExport 单个只读事务内快照读取会话行，流式写临时文件后原子替换。
// 任何失败都会清掉临时文件，不留半成品
func (prs *ProviderRelayService) streamCaptureExport(db *sql.DB, sessionID int64, meta CaptureSessionInfo, destPath string) (count int, err error) {
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("创建目录失败: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".capture-export-*")
	if err != nil {
		return 0, fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmpPath)
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		return 0, fmt.Errorf("设置权限失败: %w", err)
	}

	// 只读事务：会话元数据、行数与行数据取自同一快照——
	// 元数据若沿用对话框弹出前的读取，与录制/删除并发时会与行数据错位
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	where := `capture_session_id = ? AND capture_session_id != 0`
	args := []interface{}{sessionID}
	if sessionID == 0 {
		where = `capture_session_id = 0 AND ` + captureRowPredicate
		args = nil
	}

	if sessionID != 0 {
		var interrupted int
		err = tx.QueryRow(`SELECT id, COALESCE(started_at, ''), COALESCE(ended_at, ''), interrupted
			FROM capture_session WHERE id = ?`, sessionID).
			Scan(&meta.ID, &meta.StartedAt, &meta.EndedAt, &interrupted)
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("会话不存在或已被删除")
		}
		if err != nil {
			return 0, err
		}
		meta.Interrupted = interrupted != 0
		meta.Active = prs.captureRequests.Load() && prs.captureSessionID.Load() == sessionID
	}
	if err = tx.QueryRow(`SELECT COUNT(*) FROM request_log WHERE `+where, args...).Scan(&meta.RequestCount); err != nil {
		return 0, err
	}

	rows, err := tx.Query(`SELECT id, COALESCE(created_at, ''), COALESCE(platform, ''), COALESCE(provider, ''), COALESCE(model, ''),
			COALESCE(http_code, 0), COALESCE(is_stream, 0), COALESCE(duration_sec, 0),
			COALESCE(request_headers, ''), COALESCE(request_body, ''), COALESCE(body_truncated, 0), COALESCE(body_bytes, 0)
		FROM request_log WHERE `+where+` ORDER BY id ASC`, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	write := func(s string) {
		if err == nil {
			_, err = tmp.WriteString(s)
		}
	}
	sessionJSON, merr := json.Marshal(meta)
	if merr != nil {
		return 0, merr
	}
	write("{\n\"session\": " + string(sessionJSON) + ",\n\"requests\": [")

	enc := json.NewEncoder(tmp)
	first := true
	for rows.Next() {
		var e captureExportEntry
		var headers string
		var isStream, truncated int
		if scanErr := rows.Scan(&e.ID, &e.CreatedAt, &e.Platform, &e.Provider, &e.Model,
			&e.HttpCode, &isStream, &e.DurationSec, &headers, &e.RequestBody, &truncated, &e.BodyBytes); scanErr != nil {
			return 0, scanErr
		}
		e.IsStream = isStream != 0
		e.BodyTruncated = truncated != 0
		if headers != "" && json.Valid([]byte(headers)) {
			e.RequestHeaders = json.RawMessage(headers)
		}
		if first {
			write("\n")
			first = false
		} else {
			write(",\n")
		}
		if err == nil {
			err = enc.Encode(&e) // Encode 自带换行，与手写分隔符配合成 JSON Lines 风格的数组体
		}
		if err != nil {
			return 0, fmt.Errorf("写入导出文件失败: %w", err)
		}
		count++
	}
	if rerr := rows.Err(); rerr != nil {
		return 0, rerr
	}
	write(fmt.Sprintf("],\n\"meta\": {\"exported_at\": %q, \"count\": %d}\n}\n",
		time.Now().UTC().Format(time.RFC3339), count))
	if err != nil {
		return 0, fmt.Errorf("写入导出文件失败: %w", err)
	}

	if err = tmp.Sync(); err != nil {
		return 0, fmt.Errorf("同步导出文件失败: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return 0, fmt.Errorf("关闭导出文件失败: %w", err)
	}
	if err = atomicRename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return 0, err
	}
	return count, nil
}

// captureNowUTC 会话时间戳统一 UTC 文本，与 request_log.created_at
// （DEFAULT CURRENT_TIMESTAMP，UTC）同口径，前端统一转本地展示
func captureNowUTC() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}
