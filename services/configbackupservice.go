package services

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	configBackupSchemaVersion = 1
	configBackupManifestName  = "manifest.json"
)

type ConfigBackupExportOptions struct {
	IncludeSecrets  bool `json:"include_secrets"`
	IncludeDatabase bool `json:"include_database"`
}

type ConfigBackupExportFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type ConfigBackupManifest struct {
	SchemaVersion   int                      `json:"schema_version"`
	App             string                   `json:"app"`
	ExportedAt      string                   `json:"exported_at"`
	IncludeSecrets  bool                     `json:"include_secrets"`
	IncludeDatabase bool                     `json:"include_database"`
	Files           []ConfigBackupExportFile `json:"files"`
}

type ConfigBackupExportResult struct {
	Path      string               `json:"path"`
	FileCount int                  `json:"file_count"`
	Manifest  ConfigBackupManifest `json:"manifest"`
}

type ConfigBackupImportOptions struct {
	ImportDatabase          bool `json:"import_database"`
	PreserveExistingSecrets bool `json:"preserve_existing_secrets"`
}

type ConfigBackupImportResult struct {
	ImportedFiles  int      `json:"imported_files"`
	SkippedFiles   int      `json:"skipped_files"`
	BackupsCreated int      `json:"backups_created"`
	Warnings       []string `json:"warnings,omitempty"`
}

type ConfigBackupService struct {
	mcpService    *MCPService
	geminiService *GeminiService
	promptService *PromptService
}

func NewConfigBackupService(ms *MCPService, gs *GeminiService, ps *PromptService) *ConfigBackupService {
	return &ConfigBackupService{
		mcpService:    ms,
		geminiService: gs,
		promptService: ps,
	}
}

func (s *ConfigBackupService) Start() error { return nil }
func (s *ConfigBackupService) Stop() error  { return nil }

func (s *ConfigBackupService) GetDefaultExportPath() (string, error) {
	home, err := getUserHomeDir()
	if err != nil {
		return "", err
	}

	filename := fmt.Sprintf("code-switch-config-%s.zip", time.Now().Format("20060102-150405"))
	candidates := []string{
		filepath.Join(home, "Desktop"),
		filepath.Join(home, "Downloads"),
		home,
	}
	for _, dir := range candidates {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return filepath.Join(dir, filename), nil
		}
	}
	return filepath.Join(home, filename), nil
}

func (s *ConfigBackupService) ExportConfig(destPath string, opt ConfigBackupExportOptions) (ConfigBackupExportResult, error) {
	destPath = strings.TrimSpace(destPath)
	if destPath == "" {
		var err error
		destPath, err = s.GetDefaultExportPath()
		if err != nil {
			return ConfigBackupExportResult{}, err
		}
	}
	destPath, err := expandUserPath(destPath)
	if err != nil {
		return ConfigBackupExportResult{}, err
	}

	if fi, statErr := os.Stat(destPath); statErr == nil && fi.IsDir() {
		defaultPath, err := s.GetDefaultExportPath()
		if err != nil {
			return ConfigBackupExportResult{}, err
		}
		destPath = filepath.Join(destPath, filepath.Base(defaultPath))
	}
	if _, err := os.Stat(destPath); err == nil {
		return ConfigBackupExportResult{}, fmt.Errorf("导出文件已存在：%s", destPath)
	}

	configDir, err := getCodeSwitchConfigDir()
	if err != nil {
		return ConfigBackupExportResult{}, err
	}
	if _, err := os.Stat(configDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ConfigBackupExportResult{}, fmt.Errorf("配置目录不存在：%s", configDir)
		}
		return ConfigBackupExportResult{}, err
	}

	files, err := collectConfigBackupFiles(configDir, opt.IncludeDatabase)
	if err != nil {
		return ConfigBackupExportResult{}, err
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return ConfigBackupExportResult{}, err
	}

	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return ConfigBackupExportResult{}, err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	defer zw.Close()

	manifest := ConfigBackupManifest{
		SchemaVersion:   configBackupSchemaVersion,
		App:             "code-switch-R",
		ExportedAt:      time.Now().UTC().Format(time.RFC3339Nano),
		IncludeSecrets:  opt.IncludeSecrets,
		IncludeDatabase: opt.IncludeDatabase,
		Files:           make([]ConfigBackupExportFile, 0, len(files)),
	}

	for _, rel := range files {
		fullPath := filepath.Join(configDir, rel)

		data, err := os.ReadFile(fullPath)
		if err != nil {
			return ConfigBackupExportResult{}, fmt.Errorf("读取文件失败 %s: %w", rel, err)
		}

		if !opt.IncludeSecrets {
			if sanitized, ok, err := sanitizeBackupFile(rel, data); err != nil {
				return ConfigBackupExportResult{}, err
			} else if ok {
				data = sanitized
			}
		}

		entryName := filepath.ToSlash(rel)
		w, err := zw.Create(entryName)
		if err != nil {
			return ConfigBackupExportResult{}, err
		}
		if _, err := w.Write(data); err != nil {
			return ConfigBackupExportResult{}, err
		}

		sum := sha256.Sum256(data)
		manifest.Files = append(manifest.Files, ConfigBackupExportFile{
			Path:   entryName,
			Size:   int64(len(data)),
			SHA256: hex.EncodeToString(sum[:]),
		})
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return ConfigBackupExportResult{}, err
	}
	mw, err := zw.Create(configBackupManifestName)
	if err != nil {
		return ConfigBackupExportResult{}, err
	}
	if _, err := mw.Write(manifestBytes); err != nil {
		return ConfigBackupExportResult{}, err
	}

	return ConfigBackupExportResult{
		Path:      destPath,
		FileCount: len(files),
		Manifest:  manifest,
	}, nil
}

func (s *ConfigBackupService) ImportConfig(srcPath string, opt ConfigBackupImportOptions) (ConfigBackupImportResult, error) {
	srcPath = strings.TrimSpace(srcPath)
	if srcPath == "" {
		return ConfigBackupImportResult{}, fmt.Errorf("导入路径为空")
	}
	srcPath, err := expandUserPath(srcPath)
	if err != nil {
		return ConfigBackupImportResult{}, err
	}
	if _, err := os.Stat(srcPath); err != nil {
		return ConfigBackupImportResult{}, err
	}

	configDir, err := getCodeSwitchConfigDir()
	if err != nil {
		return ConfigBackupImportResult{}, err
	}
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		return ConfigBackupImportResult{}, err
	}

	zf, err := os.Open(srcPath)
	if err != nil {
		return ConfigBackupImportResult{}, err
	}
	defer zf.Close()
	fi, err := zf.Stat()
	if err != nil {
		return ConfigBackupImportResult{}, err
	}
	zr, err := zip.NewReader(zf, fi.Size())
	if err != nil {
		return ConfigBackupImportResult{}, err
	}

	manifest, err := readBackupManifest(zr)
	if err != nil {
		return ConfigBackupImportResult{}, err
	}
	if manifest.SchemaVersion != configBackupSchemaVersion {
		return ConfigBackupImportResult{}, fmt.Errorf("不支持的备份格式版本：%d", manifest.SchemaVersion)
	}

	result := ConfigBackupImportResult{
		Warnings: make([]string, 0),
	}

	changed := map[string]bool{}
	for _, zentry := range zr.File {
		if zentry.FileInfo().IsDir() {
			continue
		}
		if zentry.Name == configBackupManifestName {
			continue
		}

		rel, ok := sanitizeZipEntryName(zentry.Name)
		if !ok {
			result.SkippedFiles++
			result.Warnings = append(result.Warnings, fmt.Sprintf("跳过非法路径：%s", zentry.Name))
			continue
		}

		if shouldSkipBackupRelPath(rel, opt.ImportDatabase) {
			result.SkippedFiles++
			continue
		}

		rc, err := zentry.Open()
		if err != nil {
			return result, err
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return result, readErr
		}

		dest := filepath.Join(configDir, filepath.FromSlash(rel))

		if opt.PreserveExistingSecrets {
			if merged, ok, err := mergePreserveSecrets(rel, dest, data); err != nil {
				return result, err
			} else if ok {
				data = merged
			}
		}

		if backupPath, err := CreateBackup(dest); err != nil {
			return result, err
		} else if backupPath != "" {
			result.BackupsCreated++
		}

		perm := os.FileMode(0o644)
		if isDatabaseFile(rel) {
			perm = 0o600
		}
		if err := atomicWriteFile(dest, data, perm); err != nil {
			return result, fmt.Errorf("写入失败 %s: %w", rel, err)
		}

		result.ImportedFiles++
		changed[filepath.ToSlash(rel)] = true
	}

	// Import 后做必要的 in-memory/sync 刷新，避免 UI 仍显示旧状态
	if changed["gemini-providers.json"] && s.geminiService != nil {
		if err := s.geminiService.ReloadProvidersFromDisk(); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Gemini 配置重载失败: %v", err))
		}
	}
	if changed["prompts.json"] && s.promptService != nil {
		if err := s.promptService.ReloadFromDisk(); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Prompts 配置重载失败: %v", err))
		}
	}
	if changed["mcp.json"] && s.mcpService != nil {
		servers, err := s.mcpService.ListServers()
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("MCP 配置读取失败: %v", err))
		} else if err := s.mcpService.SaveServers(servers); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("MCP 配置同步失败: %v", err))
		}
	}
	return result, nil
}

func getCodeSwitchConfigDir() (string, error) {
	home, err := getUserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".code-switch"), nil
}

func expandUserPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("路径为空")
	}
	home, err := getUserHomeDir()
	if err != nil {
		return "", err
	}
	if p == "~" {
		p = home
	} else if strings.HasPrefix(p, "~"+string(os.PathSeparator)) {
		p = filepath.Join(home, strings.TrimPrefix(p, "~"+string(os.PathSeparator)))
	} else if strings.HasPrefix(p, "~/") {
		// 兼容用户输入 unix 风格
		p = filepath.Join(home, strings.TrimPrefix(p, "~/"))
	}
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("请提供绝对路径：%s", p)
	}
	return p, nil
}

func collectConfigBackupFiles(configDir string, includeDatabase bool) ([]string, error) {
	files := make([]string, 0, 32)
	err := filepath.WalkDir(configDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		rel, err := filepath.Rel(configDir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			if rel == "." {
				return nil
			}
			if strings.HasPrefix(rel, "updates/") || rel == "updates" {
				return filepath.SkipDir
			}
			// 备份文件夹不导出（避免递归叠加）
			if strings.HasPrefix(rel, "backup/") || rel == "backup" {
				return filepath.SkipDir
			}
			return nil
		}

		// 只处理常规文件
		if !d.Type().IsRegular() {
			return nil
		}

		if shouldSkipBackupRelPath(rel, includeDatabase) {
			return nil
		}

		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	// 保证 manifest 输出稳定
	sort.Strings(files)
	return files, nil
}

func shouldSkipBackupRelPath(rel string, includeDatabase bool) bool {
	base := path.Base(rel)

	// 明确排除：更新状态/更新目录（避免导入后触发异常更新行为）
	if base == "update-state.json" {
		return true
	}

	// 排除：首次运行/迁移/临时标记
	if strings.HasPrefix(base, ".import_") || strings.HasPrefix(base, ".migrated-") || strings.HasPrefix(base, ".migrated-from-") {
		return true
	}

	// 排除：临时/备份文件（避免污染导入结果）
	if strings.Contains(base, ".tmp") || strings.Contains(base, ".backup") || strings.Contains(base, ".bak.") || strings.HasSuffix(base, ".bak") {
		return true
	}

	if !includeDatabase && isDatabaseFile(rel) {
		return true
	}

	// 排除：updates 目录（双保险）
	if strings.HasPrefix(rel, "updates/") || rel == "updates" {
		return true
	}

	return false
}

func isDatabaseFile(rel string) bool {
	switch path.Base(rel) {
	case "app.db", "app.db-wal", "app.db-shm":
		return true
	default:
		return false
	}
}

func sanitizeZipEntryName(name string) (string, bool) {
	clean := path.Clean(strings.TrimSpace(name))
	clean = strings.TrimPrefix(clean, "./")
	if clean == "." || clean == "" {
		return "", false
	}
	if strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "\\") {
		return "", false
	}
	if strings.Contains(clean, "..") {
		// 避免任何形式的路径穿越
		parts := strings.Split(clean, "/")
		for _, p := range parts {
			if p == ".." {
				return "", false
			}
		}
	}
	return clean, true
}

func readBackupManifest(zr *zip.Reader) (ConfigBackupManifest, error) {
	for _, f := range zr.File {
		if f.Name != configBackupManifestName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return ConfigBackupManifest{}, err
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return ConfigBackupManifest{}, readErr
		}
		var manifest ConfigBackupManifest
		if err := json.Unmarshal(data, &manifest); err != nil {
			return ConfigBackupManifest{}, err
		}
		return manifest, nil
	}
	return ConfigBackupManifest{}, fmt.Errorf("备份文件缺少 %s", configBackupManifestName)
}

func sanitizeBackupFile(rel string, data []byte) ([]byte, bool, error) {
	switch {
	case rel == "claude-code.json" || rel == "codex.json" || strings.HasPrefix(rel, "providers/"):
		return sanitizeProviderEnvelope(data)
	case rel == "gemini-providers.json":
		return sanitizeGeminiProviders(data)
	case rel == "mcp.json":
		return sanitizeMCPStore(data)
	default:
		return nil, false, nil
	}
}

func sanitizeProviderEnvelope(data []byte) ([]byte, bool, error) {
	if len(data) == 0 {
		return data, false, nil
	}
	var env providerEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, false, nil
	}
	changed := false
	for i := range env.Providers {
		if strings.TrimSpace(env.Providers[i].APIKey) != "" {
			env.Providers[i].APIKey = ""
			changed = true
		}
	}
	if !changed {
		return data, false, nil
	}
	out, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func sanitizeGeminiProviders(data []byte) ([]byte, bool, error) {
	if len(data) == 0 {
		return data, false, nil
	}
	var providers []GeminiProvider
	if err := json.Unmarshal(data, &providers); err != nil {
		return nil, false, nil
	}
	changed := false
	for i := range providers {
		if strings.TrimSpace(providers[i].APIKey) != "" {
			providers[i].APIKey = ""
			changed = true
		}
		if providers[i].EnvConfig != nil {
			for k, v := range providers[i].EnvConfig {
				if looksLikeSecretKey(k) && strings.TrimSpace(v) != "" {
					providers[i].EnvConfig[k] = ""
					changed = true
				}
			}
		}
	}
	if !changed {
		return data, false, nil
	}
	out, err := json.MarshalIndent(providers, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func sanitizeMCPStore(data []byte) ([]byte, bool, error) {
	if len(data) == 0 {
		return data, false, nil
	}

	payload, ok, err := decodeMCPStore(data)
	if err != nil || !ok {
		return nil, false, nil
	}

	changed := false
	for name, srv := range payload.Servers {
		if srv.Env == nil {
			continue
		}
		newEnv := make(map[string]string, len(srv.Env))
		for k, v := range srv.Env {
			if looksLikeSecretKey(k) && strings.TrimSpace(v) != "" {
				newEnv[k] = ""
				changed = true
			} else {
				newEnv[k] = v
			}
		}
		srv.Env = newEnv
		payload.Servers[name] = srv
	}
	if !changed {
		return data, false, nil
	}
	out, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func looksLikeSecretKey(key string) bool {
	u := strings.ToUpper(strings.TrimSpace(key))
	if u == "" {
		return false
	}
	return strings.Contains(u, "KEY") || strings.Contains(u, "TOKEN") || strings.Contains(u, "SECRET") || strings.Contains(u, "PASSWORD") || strings.Contains(u, "AUTH")
}

func mergePreserveSecrets(rel, destPath string, imported []byte) ([]byte, bool, error) {
	existing, err := os.ReadFile(destPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}

	switch {
	case rel == "claude-code.json" || rel == "codex.json" || strings.HasPrefix(rel, "providers/"):
		return mergeProviderSecrets(existing, imported)
	case rel == "gemini-providers.json":
		return mergeGeminiSecrets(existing, imported)
	case rel == "mcp.json":
		return mergeMCPSecrets(existing, imported)
	default:
		return nil, false, nil
	}
}

func mergeProviderSecrets(existing, imported []byte) ([]byte, bool, error) {
	if len(existing) == 0 || len(imported) == 0 {
		return nil, false, nil
	}

	var ex providerEnvelope
	if err := json.Unmarshal(existing, &ex); err != nil {
		return nil, false, nil
	}
	var im providerEnvelope
	if err := json.Unmarshal(imported, &im); err != nil {
		return nil, false, nil
	}

	byID := make(map[int64]string, len(ex.Providers))
	byName := make(map[string]string, len(ex.Providers))
	for _, p := range ex.Providers {
		if strings.TrimSpace(p.APIKey) == "" {
			continue
		}
		byID[p.ID] = p.APIKey
		byName[p.Name] = p.APIKey
	}

	changed := false
	for i := range im.Providers {
		if strings.TrimSpace(im.Providers[i].APIKey) != "" {
			continue
		}
		if v := byID[im.Providers[i].ID]; strings.TrimSpace(v) != "" {
			im.Providers[i].APIKey = v
			changed = true
			continue
		}
		if v := byName[im.Providers[i].Name]; strings.TrimSpace(v) != "" {
			im.Providers[i].APIKey = v
			changed = true
		}
	}

	if !changed {
		return nil, false, nil
	}
	out, err := json.MarshalIndent(im, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func mergeGeminiSecrets(existing, imported []byte) ([]byte, bool, error) {
	if len(existing) == 0 || len(imported) == 0 {
		return nil, false, nil
	}

	var ex []GeminiProvider
	if err := json.Unmarshal(existing, &ex); err != nil {
		return nil, false, nil
	}
	var im []GeminiProvider
	if err := json.Unmarshal(imported, &im); err != nil {
		return nil, false, nil
	}

	byID := make(map[string]GeminiProvider, len(ex))
	for _, p := range ex {
		byID[p.ID] = p
	}

	changed := false
	for i := range im {
		existingProvider, ok := byID[im[i].ID]
		if !ok {
			continue
		}

		if strings.TrimSpace(im[i].APIKey) == "" && strings.TrimSpace(existingProvider.APIKey) != "" {
			im[i].APIKey = existingProvider.APIKey
			changed = true
		}

		if existingProvider.EnvConfig != nil {
			if im[i].EnvConfig == nil {
				im[i].EnvConfig = map[string]string{}
			}
			for k, v := range existingProvider.EnvConfig {
				if !looksLikeSecretKey(k) || strings.TrimSpace(v) == "" {
					continue
				}
				if curr, ok := im[i].EnvConfig[k]; !ok || strings.TrimSpace(curr) == "" {
					im[i].EnvConfig[k] = v
					changed = true
				}
			}
		}
	}

	if !changed {
		return nil, false, nil
	}
	out, err := json.MarshalIndent(im, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func mergeMCPSecrets(existing, imported []byte) ([]byte, bool, error) {
	if len(existing) == 0 || len(imported) == 0 {
		return nil, false, nil
	}

	exPayload, ok, err := decodeMCPStore(existing)
	if err != nil || !ok {
		return nil, false, nil
	}
	imPayload, ok, err := decodeMCPStore(imported)
	if err != nil || !ok {
		return nil, false, nil
	}

	changed := false
	for name, imServer := range imPayload.Servers {
		exServer, ok := exPayload.Servers[name]
		if !ok || exServer.Env == nil {
			continue
		}
		if imServer.Env == nil {
			imServer.Env = map[string]string{}
		}
		for k, v := range exServer.Env {
			if !looksLikeSecretKey(k) || strings.TrimSpace(v) == "" {
				continue
			}
			if curr, ok := imServer.Env[k]; !ok || strings.TrimSpace(curr) == "" {
				imServer.Env[k] = v
				changed = true
			}
		}
		imPayload.Servers[name] = imServer
	}

	if !changed {
		return nil, false, nil
	}
	out, err := json.MarshalIndent(imPayload, "", "  ")
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func decodeMCPStore(data []byte) (mcpStorePayload, bool, error) {
	var payload mcpStorePayload
	if err := json.Unmarshal(data, &payload); err == nil && payload.Servers != nil {
		return payload, true, nil
	}
	// legacy flat format: map[string]rawMCPServer
	var legacy map[string]rawMCPServer
	if err := json.Unmarshal(data, &legacy); err != nil {
		return mcpStorePayload{}, false, nil
	}
	return mcpStorePayload{Servers: legacy}, true, nil
}
