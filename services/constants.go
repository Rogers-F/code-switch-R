// Package services provides centralized constants for API versions and defaults.
package services

import "os"

// API 版本常量 - 集中管理便于更新
const (
	// DefaultAnthropicAPIVersion is the default Anthropic API version.
	// This version is used for health checks and connectivity tests.
	DefaultAnthropicAPIVersion = "2023-06-01"
)

// GetAnthropicAPIVersion returns the Anthropic API version to use.
// It supports override via the ANTHROPIC_API_VERSION environment variable.
func GetAnthropicAPIVersion() string {
	if v := os.Getenv("ANTHROPIC_API_VERSION"); v != "" {
		return v
	}
	return DefaultAnthropicAPIVersion
}
