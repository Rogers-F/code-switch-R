package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	claudeSettingsDir      = ".claude"
	claudeSettingsFileName = "settings.json"
	claudeBackupFileName   = "cc-studio.back.settings.json"
	claudeAuthTokenValue   = "code-switch"
)

type ClaudeProxyStatus struct {
	Enabled bool   `json:"enabled"`
	BaseURL string `json:"base_url"`
}

type ClaudeSettingsService struct {
	relayAddr string
}

func NewClaudeSettingsService(relayAddr string) *ClaudeSettingsService {
	return &ClaudeSettingsService{relayAddr: relayAddr}
}

func (css *ClaudeSettingsService) ProxyStatus() (ClaudeProxyStatus, error) {
	status := ClaudeProxyStatus{Enabled: false, BaseURL: css.baseURL()}
	settingsPath, _, err := css.paths()
	if err != nil {
		return status, err
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return status, nil
		}
		return status, err
	}
	var payload claudeSettingsFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return status, nil
	}
	baseURL := css.baseURL()
	enabled := strings.EqualFold(payload.Env["ANTHROPIC_AUTH_TOKEN"], claudeAuthTokenValue) &&
		strings.EqualFold(payload.Env["ANTHROPIC_BASE_URL"], baseURL)
	status.Enabled = enabled
	return status, nil
}

func (css *ClaudeSettingsService) EnableProxy() error {
	settingsPath, backupPath, err := css.paths()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return err
	}

	// 读取现有配置（最小侵入模式：保留用户的其他配置）
	var existingData map[string]interface{}
	if _, statErr := os.Stat(settingsPath); statErr == nil {
		content, readErr := os.ReadFile(settingsPath)
		if readErr != nil {
			return readErr
		}
		// 创建备份
		if err := os.WriteFile(backupPath, content, 0o600); err != nil {
			return err
		}
		// 解析现有配置（仅当文件非空时）
		if len(content) > 0 {
			if err := json.Unmarshal(content, &existingData); err != nil {
				// JSON 解析失败，使用空配置继续（备份已保存）
				fmt.Printf("[警告] settings.json 格式无效，已备份到 %s，将使用空配置: %v\n", backupPath, err)
				existingData = make(map[string]interface{})
			}
		}
		if existingData == nil {
			existingData = make(map[string]interface{})
		}
	} else if errors.Is(statErr, os.ErrNotExist) {
		// 文件不存在，使用空配置
		existingData = make(map[string]interface{})
	} else {
		// 其他 stat 错误（权限等），返回错误避免意外覆盖
		return fmt.Errorf("无法读取 settings.json: %w", statErr)
	}

	// 仅更新代理相关字段，保留其他配置（如 model, alwaysThinkingEnabled, enabledPlugins）
	env, ok := existingData["env"].(map[string]interface{})
	if !ok {
		env = make(map[string]interface{})
	}
	env["ANTHROPIC_AUTH_TOKEN"] = claudeAuthTokenValue
	env["ANTHROPIC_BASE_URL"] = css.baseURL()
	existingData["env"] = env

	// 原子写入
	return AtomicWriteJSON(settingsPath, existingData)
}

func (css *ClaudeSettingsService) DisableProxy() error {
	settingsPath, backupPath, err := css.paths()
	if err != nil {
		return err
	}
	if err := os.Remove(settingsPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(backupPath); err == nil {
		if err := os.Rename(backupPath, settingsPath); err != nil {
			return err
		}
	} else if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return nil
}

func (css *ClaudeSettingsService) paths() (settingsPath string, backupPath string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(home, claudeSettingsDir)
	return filepath.Join(dir, claudeSettingsFileName), filepath.Join(dir, claudeBackupFileName), nil
}

func (css *ClaudeSettingsService) baseURL() string {
	addr := strings.TrimSpace(css.relayAddr)
	if addr == "" {
		addr = ":18100"
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	host := addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	return host
}

type claudeSettingsFile struct {
	Env map[string]string `json:"env"`
}
