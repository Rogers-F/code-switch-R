package services

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSwitchNotifyToggleRespected 复现用户反馈：关闭"切换/拉黑系统通知"后
// isEnabled 必须返回 false（通知不再发出），且状态跨重启保持
func TestSwitchNotifyToggleRespected(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	as := NewAppSettingsService(nil)
	settings, err := as.GetAppSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.EnableSwitchNotify {
		t.Fatal("默认应为开启")
	}

	settings.EnableSwitchNotify = false
	if _, err := as.SaveAppSettings(settings); err != nil {
		t.Fatal(err)
	}

	ns := NewNotificationService(as)
	if ns.isEnabled() {
		t.Fatal("开关已关闭，isEnabled 仍返回 true")
	}

	// 磁盘上的文件必须真实含有 false（防止序列化把 false 丢掉）
	data, err := os.ReadFile(filepath.Join(tmpHome, ".code-switch", "app.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsStr(string(data), `"enable_switch_notify": false`) && !containsStr(string(data), `"enable_switch_notify":false`) {
		t.Fatalf("设置文件缺少关闭状态: %s", string(data))
	}

	// 重新构造服务（模拟重启）后仍应关闭
	as2 := NewAppSettingsService(nil)
	ns2 := NewNotificationService(as2)
	if ns2.isEnabled() {
		t.Fatal("重启后开关状态丢失")
	}
}

// TestSwitchNotifyFailClosedOnBrokenSettings 设置文件损坏（读取报错）时
// 通知必须 fail-closed：不能退回"默认开启"，否则界面显示已关闭（本地缓存）
// 而通知仍持续弹出
func TestSwitchNotifyFailClosedOnBrokenSettings(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	dir := filepath.Join(tmpHome, ".code-switch")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// 非法 JSON：模拟历史版本写坏/类型不匹配的设置文件
	if err := os.WriteFile(filepath.Join(dir, "app.json"), []byte(`{"enable_switch_notify": fal`), 0o644); err != nil {
		t.Fatal(err)
	}

	as := NewAppSettingsService(nil)
	if _, err := as.GetAppSettings(); err == nil {
		t.Fatal("损坏文件应返回读取错误（前置条件）")
	}
	ns := NewNotificationService(as)
	if ns.isEnabled() {
		t.Fatal("设置读取失败时必须 fail-closed，不得继续发通知")
	}
}
