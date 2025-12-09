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
	// 使用 map[string]any 宽容解析，避免 env 中非字符串值导致整体解析失败
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return status, nil
	}
	env, _ := payload["env"].(map[string]any)
	if env == nil {
		return status, nil
	}
	// 将 env 值转为字符串进行比较（nil 时返回空字符串）
	baseURLVal := anyToString(env["ANTHROPIC_BASE_URL"])
	baseURL := css.baseURL()
	// 只检查 base_url 是否指向本地代理，因为：
	// 1. base_url 是决定代理是否生效的关键字段
	// 2. auth_token 可能被 Claude CLI 覆盖，但不影响代理功能
	// 去除尾随斜杠以避免用户手动编辑时的小差异导致状态误判
	enabled := strings.EqualFold(
		strings.TrimSuffix(strings.TrimSpace(baseURLVal), "/"),
		strings.TrimSuffix(strings.TrimSpace(baseURL), "/"),
	)
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

// anyToString 将 any 类型安全转换为字符串，nil 返回空字符串
func anyToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
