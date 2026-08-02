package services

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
)

// RecoverAndLog 供 defer 直接调用的 panic 兜底：GUI 构建下后台协程或原生
// 回调里的未捕获 panic 会让进程无提示消失（用户视角即"闪退"）。
// 恢复后把现场写进控制台日志与 ~/.code-switch/crash.log，便于事后排查。
// 注意必须以 `defer services.RecoverAndLog("xxx")` 形式使用——recover 只在
// 被 defer 的函数体内直接调用才生效。
func RecoverAndLog(label string) {
	r := recover()
	if r == nil {
		return
	}
	stack := debug.Stack()
	fmt.Printf("🚨 [panic-recovered] %s: %v\n%s\n", label, r, stack)
	appendCrashLog(label, r, stack)
}

// SafeGo 启动带 panic 兜底的 goroutine，替代裸 `go fn()`
func SafeGo(label string, fn func()) {
	go func() {
		defer RecoverAndLog(label)
		fn()
	}()
}

// RecoverFromPanicValue 供调用方在自定义 recover 分支里复用统一的记录逻辑
// （调用方已拿到 recover() 的返回值，还需要在恢复后做状态收敛时用这个变体）
func RecoverFromPanicValue(label string, r any) {
	if r == nil {
		return
	}
	stack := debug.Stack()
	fmt.Printf("🚨 [panic-recovered] %s: %v\n%s\n", label, r, stack)
	appendCrashLog(label, r, stack)
}

// crashLogMu 序列化 crash.log 的追加与轮转：多个协程可能同时 panic
var crashLogMu sync.Mutex

// crashLogMaxBytes 单文件上限。超限时整体挪到 .old（只保留一代），
// 防止反复 panic 的场景无界膨胀
const crashLogMaxBytes = 1 << 20

// appendCrashLog 追加 panic 现场到 crash.log。控制台环形日志随进程消失，
// 落盘一份才能覆盖"用户重启后来报告闪退"的场景。尽力而为，失败静默
func appendCrashLog(label string, r any, stack []byte) {
	crashLogMu.Lock()
	defer crashLogMu.Unlock()

	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, appSettingsDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	path := filepath.Join(dir, "crash.log")
	if info, err := os.Stat(path); err == nil && info.Size() > crashLogMaxBytes {
		_ = os.Remove(path + ".old")
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	// 历史文件可能以旧权限创建过，统一收敛（POSIX 位在 Windows 上只是尽力而为）
	_ = f.Chmod(0600)
	fmt.Fprintf(f, "==== %s [%s] %v\n%s\n", time.Now().Format("2006-01-02 15:04:05"), label, r, stack)
}
