package services

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	hostsMarkerStart = "# === Code-Switch MITM Start ==="
	hostsMarkerEnd   = "# === Code-Switch MITM End ==="
)

// HostsEntry represents a single hosts entry
type HostsEntry struct {
	IP     string `json:"ip"`
	Domain string `json:"domain"`
}

// HostsService manages system hosts file modifications
type HostsService struct {
	hostsPath        string
	mu               sync.Mutex
	privilegeService *PrivilegeService
}

// NewHostsService creates a new hosts service instance
func NewHostsService() (*HostsService, error) {
	hostsPath := getHostsPath()
	privSvc, err := NewPrivilegeService()
	if err != nil {
		return nil, fmt.Errorf("failed to create privilege service: %w", err)
	}

	return &HostsService{
		hostsPath:        hostsPath,
		privilegeService: privSvc,
	}, nil
}

// getHostsPath returns the platform-specific hosts file path
func getHostsPath() string {
	switch runtime.GOOS {
	case "windows":
		systemRoot := os.Getenv("SystemRoot")
		if systemRoot == "" {
			systemRoot = "C:\\Windows"
		}
		return filepath.Join(systemRoot, "System32", "drivers", "etc", "hosts")
	default: // linux, darwin
		return "/etc/hosts"
	}
}

// getLineEnding returns the platform-specific line ending
func getLineEnding() string {
	if runtime.GOOS == "windows" {
		return "\r\n"
	}
	return "\n"
}

// Apply adds hosts entries for domains
func (h *HostsService) Apply(domains []string, ipv4 bool, ipv6 bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Read current hosts file
	original, err := h.readHosts()
	if err != nil {
		return fmt.Errorf("failed to read hosts file: %w", err)
	}

	// Remove old managed block
	content := h.removeManagedBlock(original)

	// Build new entries
	var entries []string
	eol := detectLineEnding(original)
	if eol == "" {
		eol = getLineEnding()
	}

	for _, domain := range domains {
		if ipv4 {
			entries = append(entries, fmt.Sprintf("127.0.0.1 %s", domain))
		}
		if ipv6 {
			entries = append(entries, fmt.Sprintf("::1 %s", domain))
		}
	}

	// Inject new managed block
	if len(entries) > 0 {
		managedBlock := hostsMarkerStart + eol
		for _, entry := range entries {
			managedBlock += entry + eol
		}
		managedBlock += hostsMarkerEnd + eol

		// Append to content
		if !strings.HasSuffix(content, eol) && len(content) > 0 {
			content += eol
		}
		content += managedBlock
	}

	// No changes, skip backup and write (avoids unnecessary privilege prompt)
	if content == original {
		return nil
	}

	// Create backup (only when content will change)
	if err := h.createBackup(original); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	// Write back
	if err := h.writeHosts(content); err != nil {
		return fmt.Errorf("failed to write hosts file: %w", err)
	}

	return nil
}

// Cleanup removes all managed hosts entries
func (h *HostsService) Cleanup() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Read current hosts file
	original, err := h.readHosts()
	if err != nil {
		return fmt.Errorf("failed to read hosts file: %w", err)
	}

	// No managed block, no-op (avoid unnecessary privilege prompt)
	if !strings.Contains(original, hostsMarkerStart) {
		return nil
	}

	// Remove managed block
	content := h.removeManagedBlock(original)

	// No changes, skip backup and write
	if content == original {
		return nil
	}

	// Create backup (only when content will change)
	if err := h.createBackup(original); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	// Write back
	if err := h.writeHosts(content); err != nil {
		return fmt.Errorf("failed to write hosts file: %w", err)
	}

	return nil
}

// CheckStatus checks if a domain is currently managed
func (h *HostsService) CheckStatus(domain string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	content, err := h.readHosts()
	if err != nil {
		return false, fmt.Errorf("failed to read hosts file: %w", err)
	}

	// Extract managed block
	managedBlock := h.extractManagedBlock(content)
	if managedBlock == "" {
		return false, nil
	}

	// Check if domain is in managed block
	lines := splitHostLines(managedBlock)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, domain) {
			return true, nil
		}
	}

	return false, nil
}

// GetManagedDomains returns all currently managed domains
func (h *HostsService) GetManagedDomains() ([]HostsEntry, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	content, err := h.readHosts()
	if err != nil {
		return nil, fmt.Errorf("failed to read hosts file: %w", err)
	}

	// Extract managed block
	managedBlock := h.extractManagedBlock(content)
	if managedBlock == "" {
		return []HostsEntry{}, nil
	}

	// Parse entries
	var entries []HostsEntry
	lines := splitHostLines(managedBlock)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 2 {
			entries = append(entries, HostsEntry{
				IP:     parts[0],
				Domain: parts[1],
			})
		}
	}

	return entries, nil
}

// Helper methods

// readHosts reads the entire hosts file
func (h *HostsService) readHosts() (string, error) {
	data, err := os.ReadFile(h.hostsPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// writeHosts writes content to hosts file (requires privilege)
func (h *HostsService) writeHosts(content string) error {
	// 1) Fast-path: if file is writable, do an atomic replace without elevation.
	// This avoids privilege prompt for tests and user-writable environments.
	if err := atomicWriteHostsFile(h.hostsPath, []byte(content)); err == nil {
		return nil
	} else if !looksLikePermissionError(err) {
		// Not a permission issue: fail fast (avoid confusing privilege prompt for invalid paths)
		return err
	}

	// Create temporary file with new content
	tmpDir := os.TempDir()
	tmpFile := filepath.Join(tmpDir, fmt.Sprintf("hosts-temp-%d", time.Now().UnixNano()))

	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	defer os.Remove(tmpFile)

	// Use elevated privileges to copy temp file to hosts location
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		// Use copy command on Windows
		cmd = "cmd.exe"
		args = []string{"/c", "copy", "/Y", tmpFile, h.hostsPath}
	case "darwin", "linux":
		// Use cp command on Unix-like systems
		cmd = "cp"
		args = []string{"-f", tmpFile, h.hostsPath}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}

	// Execute with elevated privileges
	_, err := h.privilegeService.RunElevated(cmd, args)
	if err != nil {
		return fmt.Errorf("failed to write hosts file with elevated privileges: %w", err)
	}

	return nil
}

// removeManagedBlock removes the managed block from content
func (h *HostsService) removeManagedBlock(content string) string {
	lines := splitHostLines(content)
	eol := detectLineEnding(content)
	if eol == "" {
		eol = getLineEnding()
	}
	var result []string
	inManagedBlock := false

	for _, line := range lines {
		if strings.Contains(line, hostsMarkerStart) {
			inManagedBlock = true
			continue
		}
		if strings.Contains(line, hostsMarkerEnd) {
			inManagedBlock = false
			continue
		}
		if !inManagedBlock {
			result = append(result, line)
		}
	}

	return strings.Join(result, eol)
}

// extractManagedBlock extracts the managed block from content
func (h *HostsService) extractManagedBlock(content string) string {
	lines := splitHostLines(content)
	var result []string
	inManagedBlock := false

	for _, line := range lines {
		if strings.Contains(line, hostsMarkerStart) {
			inManagedBlock = true
			continue
		}
		if strings.Contains(line, hostsMarkerEnd) {
			inManagedBlock = false
			break
		}
		if inManagedBlock {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// createBackup creates a backup of the current hosts file
func (h *HostsService) createBackup(content string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	backupDir := filepath.Join(homeDir, ".code-switch", "backups", "hosts")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return err
	}

	// Keep only last 5 backups
	h.cleanupOldBackups(backupDir, 5)

	// Create new backup
	// 使用纳秒级时间戳，避免并发/快速调用导致备份文件名碰撞
	timestamp := time.Now().Format("20060102-150405.000000000")
	backupPath := filepath.Join(backupDir, fmt.Sprintf("hosts.%s.bak", timestamp))
	return os.WriteFile(backupPath, []byte(content), 0600)
}

func detectLineEnding(content string) string {
	if strings.Contains(content, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func splitHostLines(content string) []string {
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines
}

func looksLikePermissionError(err error) bool {
	if err == nil {
		return false
	}
	if os.IsPermission(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "operation not permitted") || strings.Contains(msg, "permission denied")
}

func atomicWriteHostsFile(path string, data []byte) error {
	tmpPath := fmt.Sprintf("%s.tmp.%d", path, time.Now().UnixNano())
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return err
	}

	// Windows: rename 目标存在时会失败，需要先删除
	if runtime.GOOS == "windows" {
		if _, err := os.Stat(path); err == nil {
			if err := os.Remove(path); err != nil {
				_ = os.Remove(tmpPath)
				return err
			}
		}
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

// cleanupOldBackups removes old backup files, keeping only the most recent N
func (h *HostsService) cleanupOldBackups(backupDir string, keep int) {
	files, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}

	// Filter and sort backups
	var backups []os.DirEntry
	for _, f := range files {
		if !f.IsDir() && strings.HasPrefix(f.Name(), "hosts.") {
			backups = append(backups, f)
		}
	}

	if len(backups) <= keep {
		return
	}

	// Remove oldest files
	for i := 0; i < len(backups)-keep; i++ {
		os.Remove(filepath.Join(backupDir, backups[i].Name()))
	}
}

// Wails exported methods

// ApplyHostsEntries applies hosts entries (exported for Wails)
func (h *HostsService) ApplyHostsEntries(domains []string, ipv4 bool, ipv6 bool) error {
	return h.Apply(domains, ipv4, ipv6)
}

// CleanupHostsEntries removes all managed entries (exported for Wails)
func (h *HostsService) CleanupHostsEntries() error {
	return h.Cleanup()
}

// CheckHostsStatus checks if a domain is managed (exported for Wails)
func (h *HostsService) CheckHostsStatus(domain string) (bool, error) {
	return h.CheckStatus(domain)
}

// GetManagedHostsDomains returns managed domains (exported for Wails)
func (h *HostsService) GetManagedHostsDomains() ([]HostsEntry, error) {
	return h.GetManagedDomains()
}

// GetHostsFilePath returns the hosts file path (exported for Wails)
func (h *HostsService) GetHostsFilePath() string {
	return h.hostsPath
}

// ReadHostsFile reads the entire hosts file content (exported for Wails)
func (h *HostsService) ReadHostsFile() (string, error) {
	file, err := os.Open(h.hostsPath)
	if err != nil {
		return "", fmt.Errorf("failed to open hosts file: %w", err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("failed to read hosts file: %w", err)
	}

	return strings.Join(lines, "\n"), nil
}
