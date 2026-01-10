package services

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestConfigBackup_Export_SanitizesAndFilters(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	configDir := filepath.Join(home, ".code-switch")
	if err := os.MkdirAll(filepath.Join(configDir, "updates"), 0o755); err != nil {
		t.Fatalf("mkdir updates: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(configDir, "providers"), 0o755); err != nil {
		t.Fatalf("mkdir providers: %v", err)
	}

	claudeProviders := providerEnvelope{
		Providers: []Provider{
			{ID: 1, Name: "p1", APIKey: "secret"},
		},
	}
	writeJSON(t, filepath.Join(configDir, "claude-code.json"), claudeProviders)

	geminiProviders := []GeminiProvider{
		{ID: "g1", Name: "gemini", APIKey: "secret", EnvConfig: map[string]string{"GEMINI_API_KEY": "secret", "GOOGLE_GEMINI_BASE_URL": "https://example.com"}},
	}
	writeJSON(t, filepath.Join(configDir, "gemini-providers.json"), geminiProviders)

	mcp := mcpStorePayload{
		Servers: map[string]rawMCPServer{
			"reftools": {
				Type: "http",
				Env:  map[string]string{"REFTOOLS_API_KEY": "secret", "PATH": "/usr/bin"},
				URL:  "https://api.ref.tools/mcp?apiKey={apiKey}",
			},
		},
	}
	writeJSON(t, filepath.Join(configDir, "mcp.json"), mcp)

	if err := os.WriteFile(filepath.Join(configDir, "update-state.json"), []byte(`{"x":1}`), 0o644); err != nil {
		t.Fatalf("write update-state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "updates", "big.bin"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write updates file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "app.db"), []byte("db"), 0o600); err != nil {
		t.Fatalf("write app.db: %v", err)
	}

	out := filepath.Join(home, "export.zip")
	svc := NewConfigBackupService(nil, nil, nil)
	res, err := svc.ExportConfig(out, ConfigBackupExportOptions{IncludeSecrets: false, IncludeDatabase: false})
	if err != nil {
		t.Fatalf("ExportConfig: %v", err)
	}
	if res.FileCount == 0 {
		t.Fatalf("expected files exported")
	}

	entries := readZipEntries(t, out)
	if _, ok := entries["app.db"]; ok {
		t.Fatalf("expected app.db excluded")
	}
	if _, ok := entries["update-state.json"]; ok {
		t.Fatalf("expected update-state.json excluded")
	}
	if _, ok := entries["updates/big.bin"]; ok {
		t.Fatalf("expected updates/ excluded")
	}

	var exportedClaude providerEnvelope
	if err := json.Unmarshal(entries["claude-code.json"], &exportedClaude); err != nil {
		t.Fatalf("unmarshal claude-code.json: %v", err)
	}
	if len(exportedClaude.Providers) != 1 || exportedClaude.Providers[0].APIKey != "" {
		t.Fatalf("expected provider apiKey sanitized")
	}

	var exportedGemini []GeminiProvider
	if err := json.Unmarshal(entries["gemini-providers.json"], &exportedGemini); err != nil {
		t.Fatalf("unmarshal gemini-providers.json: %v", err)
	}
	if len(exportedGemini) != 1 || exportedGemini[0].APIKey != "" {
		t.Fatalf("expected gemini apiKey sanitized")
	}
	if exportedGemini[0].EnvConfig["GEMINI_API_KEY"] != "" {
		t.Fatalf("expected gemini env secret sanitized")
	}
	if exportedGemini[0].EnvConfig["GOOGLE_GEMINI_BASE_URL"] == "" {
		t.Fatalf("expected gemini non-secret env preserved")
	}

	var exportedMCP mcpStorePayload
	if err := json.Unmarshal(entries["mcp.json"], &exportedMCP); err != nil {
		t.Fatalf("unmarshal mcp.json: %v", err)
	}
	if exportedMCP.Servers["reftools"].Env["REFTOOLS_API_KEY"] != "" {
		t.Fatalf("expected mcp env secret sanitized")
	}
	if exportedMCP.Servers["reftools"].Env["PATH"] == "" {
		t.Fatalf("expected mcp non-secret env preserved")
	}

	var manifest ConfigBackupManifest
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatalf("unmarshal manifest.json: %v", err)
	}
	if manifest.SchemaVersion != configBackupSchemaVersion {
		t.Fatalf("unexpected schema version: %d", manifest.SchemaVersion)
	}
	if manifest.IncludeSecrets {
		t.Fatalf("expected include_secrets=false")
	}
	if manifest.IncludeDatabase {
		t.Fatalf("expected include_database=false")
	}
}

func TestConfigBackup_Import_PreserveSecrets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	configDir := filepath.Join(home, ".code-switch")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}

	claudeProviders := providerEnvelope{
		Providers: []Provider{
			{ID: 1, Name: "p1", APIKey: "keep"},
		},
	}
	writeJSON(t, filepath.Join(configDir, "claude-code.json"), claudeProviders)

	out := filepath.Join(home, "export.zip")
	svc := NewConfigBackupService(nil, nil, nil)
	if _, err := svc.ExportConfig(out, ConfigBackupExportOptions{IncludeSecrets: false, IncludeDatabase: false}); err != nil {
		t.Fatalf("ExportConfig: %v", err)
	}

	// 导入（保留本机密钥）
	importRes, err := svc.ImportConfig(out, ConfigBackupImportOptions{ImportDatabase: false, PreserveExistingSecrets: true})
	if err != nil {
		t.Fatalf("ImportConfig: %v", err)
	}
	if importRes.ImportedFiles == 0 {
		t.Fatalf("expected imported files")
	}
	if importRes.BackupsCreated == 0 {
		t.Fatalf("expected backup created")
	}

	var after providerEnvelope
	data, err := os.ReadFile(filepath.Join(configDir, "claude-code.json"))
	if err != nil {
		t.Fatalf("read claude-code.json: %v", err)
	}
	if err := json.Unmarshal(data, &after); err != nil {
		t.Fatalf("unmarshal claude-code.json: %v", err)
	}
	if after.Providers[0].APIKey != "keep" {
		t.Fatalf("expected apiKey preserved, got %q", after.Providers[0].APIKey)
	}
}

func TestConfigBackup_Import_SkipDatabase(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	configDir := filepath.Join(home, ".code-switch")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "app.db"), []byte("db"), 0o600); err != nil {
		t.Fatalf("write app.db: %v", err)
	}

	out := filepath.Join(home, "export.zip")
	svc := NewConfigBackupService(nil, nil, nil)
	if _, err := svc.ExportConfig(out, ConfigBackupExportOptions{IncludeSecrets: true, IncludeDatabase: true}); err != nil {
		t.Fatalf("ExportConfig: %v", err)
	}

	// 清空本机 db，验证导入时能跳过
	if err := os.Remove(filepath.Join(configDir, "app.db")); err != nil {
		t.Fatalf("remove app.db: %v", err)
	}

	if _, err := svc.ImportConfig(out, ConfigBackupImportOptions{ImportDatabase: false, PreserveExistingSecrets: true}); err != nil {
		t.Fatalf("ImportConfig: %v", err)
	}
	if _, err := os.Stat(filepath.Join(configDir, "app.db")); err == nil {
		t.Fatalf("expected app.db not imported when ImportDatabase=false")
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func readZipEntries(t *testing.T, zipPath string) map[string][]byte {
	t.Helper()
	f, err := os.Open(zipPath)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		t.Fatalf("stat zip: %v", err)
	}
	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		t.Fatalf("new reader: %v", err)
	}
	out := make(map[string][]byte, len(zr.File))
	for _, entry := range zr.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		rc, err := entry.Open()
		if err != nil {
			t.Fatalf("open entry %s: %v", entry.Name, err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read entry %s: %v", entry.Name, err)
		}
		out[entry.Name] = data
	}
	return out
}
