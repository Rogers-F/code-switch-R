package services

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// setupSkillTestHome 把 HOME/USERPROFILE 指向临时目录，避免污染真实用户配置
func setupSkillTestHome(t *testing.T) string {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)
	return tmpHome
}

// writeSkillSource 生成一个包含 SKILL.md 的技能源目录
func writeSkillSource(t *testing.T, dir, description string, extraFiles map[string]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	skillMD := "---\nname: demo\ndescription: " + description + "\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMD), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range extraFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// 重装升级应整目录替换旧版本，且不残留 .new/.old 旁路目录
func TestSkillInstallUpgradeReplacesAndCleansUp(t *testing.T) {
	tmpHome := setupSkillTestHome(t)
	ss := NewSkillService()

	src1 := filepath.Join(t.TempDir(), "demo-v1")
	writeSkillSource(t, src1, "v1", map[string]string{"old-only.txt": "v1"})
	if err := ss.installFromPathEx("demo", src1, skillPlatformClaude, skillLocationUser); err != nil {
		t.Fatalf("首次安装失败: %v", err)
	}

	src2 := filepath.Join(t.TempDir(), "demo-v2")
	writeSkillSource(t, src2, "v2", nil)
	if err := ss.installFromPathEx("demo", src2, skillPlatformClaude, skillLocationUser); err != nil {
		t.Fatalf("升级安装失败: %v", err)
	}

	target := filepath.Join(tmpHome, ".claude", "skills", "demo")
	data, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "description: v2") {
		t.Fatalf("升级后 SKILL.md 仍是旧版本: %q", string(data))
	}
	if _, err := os.Stat(filepath.Join(target, "old-only.txt")); !os.IsNotExist(err) {
		t.Fatal("旧版本独有文件未被清除")
	}
	for _, suffix := range []string{".new", ".old"} {
		if _, err := os.Stat(target + suffix); !os.IsNotExist(err) {
			t.Fatalf("残留旁路目录 %s", target+suffix)
		}
	}
}

// 替换旧目录失败时必须保留旧版本可用，不能出现"旧的已删、新的没装上"
func TestSkillInstallFailureKeepsOldVersion(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("依赖 Windows 上打开的文件句柄阻止目录改名")
	}

	tmpHome := setupSkillTestHome(t)
	ss := NewSkillService()

	src1 := filepath.Join(t.TempDir(), "demo-v1")
	writeSkillSource(t, src1, "v1", nil)
	if err := ss.installFromPathEx("demo", src1, skillPlatformClaude, skillLocationUser); err != nil {
		t.Fatalf("首次安装失败: %v", err)
	}

	target := filepath.Join(tmpHome, ".claude", "skills", "demo")

	// 打开旧版本中的文件并保持句柄，使 os.Rename(target, target+".old") 失败
	f, err := os.Open(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	src2 := filepath.Join(t.TempDir(), "demo-v2")
	writeSkillSource(t, src2, "v2", nil)
	if err := ss.installFromPathEx("demo", src2, skillPlatformClaude, skillLocationUser); err == nil {
		t.Fatal("目录被锁定时安装应失败")
	}

	// 旧版本必须原样保留
	data, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatalf("旧版本被破坏: %v", err)
	}
	if !strings.Contains(string(data), "description: v1") {
		t.Fatalf("旧版本内容被破坏: %q", string(data))
	}
	// 失败路径不应残留旁路目录
	if _, err := os.Stat(target + ".new"); !os.IsNotExist(err) {
		t.Fatal("失败后残留 .new 目录")
	}
}
