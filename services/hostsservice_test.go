package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHostsServiceApplyCleanupIdempotent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	hostsPath := filepath.Join(tmpHome, "hosts")
	if err := os.WriteFile(hostsPath, []byte("127.0.0.1 localhost\n"), 0o644); err != nil {
		t.Fatalf("write hosts: %v", err)
	}

	hs := &HostsService{
		hostsPath:        hostsPath,
		privilegeService: &PrivilegeService{},
	}

	backupDir := filepath.Join(tmpHome, ".code-switch", "backups", "hosts")

	// Cleanup on a clean file should be a no-op and should not create backups.
	if err := hs.Cleanup(); err != nil {
		t.Fatalf("cleanup (no marker): %v", err)
	}
	if _, err := os.Stat(backupDir); err == nil {
		t.Fatalf("expected no backups to be created on noop cleanup")
	}

	// Apply once should change the file and create a backup.
	if err := hs.Apply([]string{"api.openai.com"}, true, true); err != nil {
		t.Fatalf("apply: %v", err)
	}
	afterFirstApply, err := os.ReadFile(hostsPath)
	if err != nil {
		t.Fatalf("read after apply: %v", err)
	}
	contentFirst := string(afterFirstApply)
	if !strings.Contains(contentFirst, hostsMarkerStart) || !strings.Contains(contentFirst, hostsMarkerEnd) {
		t.Fatalf("expected managed block markers to be present after apply")
	}
	if !strings.Contains(contentFirst, "127.0.0.1 api.openai.com") {
		t.Fatalf("expected ipv4 entry to be present")
	}
	if !strings.Contains(contentFirst, "::1 api.openai.com") {
		t.Fatalf("expected ipv6 entry to be present")
	}

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 backup after first apply, got %d", len(entries))
	}

	// Apply same config again should be idempotent: no file change and no new backup.
	if err := hs.Apply([]string{"api.openai.com"}, true, true); err != nil {
		t.Fatalf("apply again: %v", err)
	}
	afterSecondApply, err := os.ReadFile(hostsPath)
	if err != nil {
		t.Fatalf("read after second apply: %v", err)
	}
	if string(afterSecondApply) != contentFirst {
		t.Fatalf("expected hosts file to remain unchanged on idempotent apply")
	}
	entries, err = os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup dir after second apply: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected no new backup on idempotent apply, got %d", len(entries))
	}

	// Cleanup should remove the managed block and create a backup.
	if err := hs.Cleanup(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	afterCleanup, err := os.ReadFile(hostsPath)
	if err != nil {
		t.Fatalf("read after cleanup: %v", err)
	}
	contentAfterCleanup := string(afterCleanup)
	if strings.Contains(contentAfterCleanup, hostsMarkerStart) || strings.Contains(contentAfterCleanup, hostsMarkerEnd) {
		t.Fatalf("expected managed block markers to be removed after cleanup")
	}
	entries, err = os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup dir after cleanup: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 backups after cleanup, got %d", len(entries))
	}

	// Cleanup again should be a no-op and not create new backups.
	if err := hs.Cleanup(); err != nil {
		t.Fatalf("cleanup again: %v", err)
	}
	entries, err = os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup dir after second cleanup: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected no new backup on noop cleanup, got %d", len(entries))
	}
}

func TestHostsServicePreservesCRLFLineEndings(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	hostsPath := filepath.Join(tmpHome, "hosts")
	original := "127.0.0.1 localhost\r\n"
	if err := os.WriteFile(hostsPath, []byte(original), 0o644); err != nil {
		t.Fatalf("write hosts: %v", err)
	}

	hs := &HostsService{
		hostsPath:        hostsPath,
		privilegeService: &PrivilegeService{},
	}

	if err := hs.Apply([]string{"api.openai.com"}, true, false); err != nil {
		t.Fatalf("apply: %v", err)
	}

	after, err := os.ReadFile(hostsPath)
	if err != nil {
		t.Fatalf("read after apply: %v", err)
	}
	if !strings.Contains(string(after), "\r\n") {
		t.Fatalf("expected CRLF line endings to be preserved")
	}
}
