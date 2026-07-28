package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupUpdateTestHome 将家目录隔离到测试临时目录，返回更新数据目录路径
func setupUpdateTestHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("USERPROFILE", tmp)
	assertHomeIsolated(t, tmp)
	dataDir := filepath.Join(tmp, ".code-switch", "update")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("创建数据目录失败: %v", err)
	}
	return dataDir
}

// TestNewUpdateServicePolicyOverride 构建期注入的更新策略应覆盖运行时检测；非法值回退运行时检测
func TestNewUpdateServicePolicyOverride(t *testing.T) {
	setupUpdateTestHome(t)

	if got := NewUpdateService("v1.0.0", "installer").GetState().Policy; got != string(PolicyInstaller) {
		t.Fatalf("policy = %q, 期望 %q（注入值应直接采用）", got, PolicyInstaller)
	}

	// 非法注入值与未注入应得到相同的运行时检测结果
	auto := NewUpdateService("v1.0.0").GetState().Policy
	if got := NewUpdateService("v1.0.0", "bogus").GetState().Policy; got != auto {
		t.Fatalf("非法注入值 policy = %q, 期望回退运行时检测结果 %q", got, auto)
	}
}

// writePendingApply 写入待应用标记，返回 pending 文件路径
func writePendingApply(t *testing.T, dataDir string, pending *PendingApply) string {
	t.Helper()
	data, err := json.MarshalIndent(pending, "", "  ")
	if err != nil {
		t.Fatalf("序列化 pending 失败: %v", err)
	}
	pendingPath := filepath.Join(dataDir, "pending_apply.json")
	if err := os.WriteFile(pendingPath, data, 0644); err != nil {
		t.Fatalf("写入 pending 失败: %v", err)
	}
	return pendingPath
}

// makeAppDirAndArchive 构造模拟 darwin 解压产物：.app 目录 + 原 zip 文件，返回二者路径与 zip 的 SHA256
func makeAppDirAndArchive(t *testing.T, dataDir string) (string, string, string) {
	t.Helper()
	appDir := filepath.Join(dataDir, "downloads", "extracted", "CodeSwitch.app")
	if err := os.MkdirAll(appDir, 0755); err != nil {
		t.Fatalf("创建 .app 目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "binary"), []byte("app-bytes"), 0644); err != nil {
		t.Fatalf("写入 .app 内容失败: %v", err)
	}
	archivePath := filepath.Join(dataDir, "downloads", "CodeSwitch.zip")
	content := []byte("zip-bytes")
	if err := os.WriteFile(archivePath, content, 0644); err != nil {
		t.Fatalf("写入 zip 失败: %v", err)
	}
	sum := sha256.Sum256(content)
	return appDir, archivePath, hex.EncodeToString(sum[:])
}

// TestCheckPendingApplyAppDirRestore 待应用目标为 .app 目录时，应对原 zip 校验并恢复 ready 状态
func TestCheckPendingApplyAppDirRestore(t *testing.T) {
	dataDir := setupUpdateTestHome(t)
	appDir, archivePath, sha := makeAppDirAndArchive(t, dataDir)

	writePendingApply(t, dataDir, &PendingApply{
		TargetVersion: "v9.9.9",
		Method:        "swap",
		FilePath:      appDir,
		ArchivePath:   archivePath,
		FileSHA256:    sha,
		StartedAt:     time.Now(),
	})

	us := NewUpdateService("v1.0.0")
	if got := us.GetState().State; got != StateReady {
		t.Fatalf("state = %q, 期望 %q（zip 校验通过应恢复 ready）", got, StateReady)
	}
	if us.downloadState == nil || us.downloadState.ArchivePath != archivePath {
		t.Fatalf("恢复后 downloadState.ArchivePath 未保留原 zip 路径")
	}
}

// TestCheckPendingApplyAppDirCleanup 校验失败时 .app 目录应被整体清理（os.Remove 对非空目录无效）
func TestCheckPendingApplyAppDirCleanup(t *testing.T) {
	dataDir := setupUpdateTestHome(t)
	appDir, archivePath, _ := makeAppDirAndArchive(t, dataDir)

	pendingPath := writePendingApply(t, dataDir, &PendingApply{
		TargetVersion: "v9.9.9",
		Method:        "swap",
		FilePath:      appDir,
		ArchivePath:   archivePath,
		FileSHA256:    "deadbeef", // 与 zip 实际哈希不符
		StartedAt:     time.Now(),
	})

	us := NewUpdateService("v1.0.0")
	if got := us.GetState().State; got != StateIdle {
		t.Fatalf("state = %q, 期望 %q", got, StateIdle)
	}
	if _, err := os.Stat(appDir); !os.IsNotExist(err) {
		t.Fatalf(".app 目录未被清理: %v", err)
	}
	if _, err := os.Stat(archivePath); !os.IsNotExist(err) {
		t.Fatalf("原 zip 未被清理: %v", err)
	}
	if _, err := os.Stat(pendingPath); !os.IsNotExist(err) {
		t.Fatalf("pending 标记未被清理: %v", err)
	}
}

// TestDownloadStallWatchdog 链路停滞（对端保持连接但不再发数据）时下载应被看门狗中断并进入 error 态
func TestDownloadStallWatchdog(t *testing.T) {
	setupUpdateTestHome(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// 保持连接但不再发送任何数据，直到请求被取消
		<-r.Context().Done()
	}))
	defer srv.Close()

	origPrefixes := allowedURLPrefixes
	allowedURLPrefixes = append([]string{srv.URL}, origPrefixes...)
	defer func() { allowedURLPrefixes = origPrefixes }()

	origTimeout := downloadStallTimeout
	downloadStallTimeout = 500 * time.Millisecond
	defer func() { downloadStallTimeout = origTimeout }()

	dataDir := filepath.Join(t.TempDir(), "update")
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("创建数据目录失败: %v", err)
	}
	us := &UpdateService{
		state:          StateDownloading,
		currentVersion: "v1.0.0",
		dataDir:        dataDir,
	}
	info := &UpdateInfo{
		Version:     "v9.9.9",
		DownloadURL: srv.URL + "/CodeSwitch.exe",
		SHA256:      "deadbeef",
		Size:        100,
	}

	done := make(chan struct{})
	go func() {
		us.doDownload(context.Background(), info)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("doDownload 未返回：停滞看门狗未生效")
	}

	snap := us.GetState()
	if snap.State != StateError {
		t.Fatalf("state = %q, 期望 %q", snap.State, StateError)
	}
	if !strings.Contains(snap.Error, "stalled") {
		t.Fatalf("错误信息 = %q, 期望包含 stalled", snap.Error)
	}
}

// TestLaunchLinuxUpdaterUsesAppImagePath AppImage 形态下替换目标必须取 APPIMAGE 环境变量而非 os.Executable()
func TestLaunchLinuxUpdaterUsesAppImagePath(t *testing.T) {
	setupUpdateTestHome(t)
	const appImagePath = "/opt/apps/CodeSwitch.AppImage"
	t.Setenv("APPIMAGE", appImagePath)

	// 只校验脚本内容，不调用 launchLinuxUpdater：后者会真的 exec 一个后台脚本，
	// 该脚本会等待本进程退出后覆写可执行文件，在 CI 上还会遗留孤儿进程
	script := buildLinuxUpdateScript(appImagePath, "/tmp/CodeSwitch-new.AppImage", os.Getpid())

	if !strings.Contains(script, appImagePath) {
		t.Fatalf("脚本未使用 APPIMAGE 路径作为替换目标:\n%s", script)
	}
	exePath, err := os.Executable()
	if err == nil && strings.Contains(script, shSingleQuote(exePath)) {
		t.Fatalf("脚本仍以 os.Executable() 为替换目标:\n%s", script)
	}
}
