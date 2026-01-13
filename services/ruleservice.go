package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/daodao97/xgo/xdb"
	"github.com/google/uuid"
)

// ModelMapping represents a model transformation rule
type ModelMapping struct {
	SourceModel string `json:"sourceModel"` // Original model name pattern (supports *)
	TargetModel string `json:"targetModel"` // Mapped model name
}

// MITMRule represents a routing rule for MITM proxy
type MITMRule struct {
	ID              string          `json:"id"`              // UUID
	Name            string          `json:"name"`            // Display name
	Enabled         bool            `json:"enabled"`         // Rule status
	SourceHost      string          `json:"sourceHost"`      // Domain to intercept (e.g., api.openai.com)
	TargetProvider  string          `json:"targetProvider"`  // Target provider ID
	ModelMappings   []ModelMapping  `json:"modelMappings"`   // Model transformation rules
	PathRewrite     string          `json:"pathRewrite"`     // Path rewrite pattern (optional)
	Priority        int             `json:"priority"`        // Execution priority (higher = first)
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

// RuleService manages MITM routing rules
type RuleService struct {
	db *sql.DB
}

// NewRuleService creates a new rule service instance
func NewRuleService() (*RuleService, error) {
	db, err := xdb.DB("default")
	if err != nil || db == nil {
		return nil, fmt.Errorf("database not initialized: %w", err)
	}

	svc := &RuleService{db: db}
	if err := svc.initTable(); err != nil {
		return nil, fmt.Errorf("failed to init table: %w", err)
	}

	return svc, nil
}

// initTable creates the rules table if it doesn't exist
func (r *RuleService) initTable() error {
	schema := `
	CREATE TABLE IF NOT EXISTS mitm_rules (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		source_host TEXT NOT NULL UNIQUE,
		target_provider TEXT NOT NULL,
		model_mappings TEXT NOT NULL DEFAULT '[]',
		path_rewrite TEXT DEFAULT '',
		priority INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_mitm_rules_enabled ON mitm_rules(enabled);
	CREATE INDEX IF NOT EXISTS idx_mitm_rules_priority ON mitm_rules(priority DESC);
	CREATE INDEX IF NOT EXISTS idx_mitm_rules_source_host ON mitm_rules(source_host);
	`

	if _, err := r.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	return nil
}

// Create creates a new rule
func (r *RuleService) Create(rule *MITMRule) error {
	if rule.ID == "" {
		rule.ID = uuid.New().String()
	}

	now := time.Now()
	rule.CreatedAt = now
	rule.UpdatedAt = now

	// Marshal model mappings to JSON
	mappingsJSON, err := json.Marshal(rule.ModelMappings)
	if err != nil {
		return fmt.Errorf("failed to marshal model mappings: %w", err)
	}

	query := `
		INSERT INTO mitm_rules (id, name, enabled, source_host, target_provider, model_mappings, path_rewrite, priority, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = r.db.Exec(query,
		rule.ID,
		rule.Name,
		boolToInt(rule.Enabled),
		rule.SourceHost,
		rule.TargetProvider,
		string(mappingsJSON),
		rule.PathRewrite,
		rule.Priority,
		now.Unix(),
		now.Unix(),
	)

	if err != nil {
		return fmt.Errorf("failed to insert rule: %w", err)
	}

	return nil
}

// Update updates an existing rule
func (r *RuleService) Update(rule *MITMRule) error {
	rule.UpdatedAt = time.Now()

	// Marshal model mappings to JSON
	mappingsJSON, err := json.Marshal(rule.ModelMappings)
	if err != nil {
		return fmt.Errorf("failed to marshal model mappings: %w", err)
	}

	query := `
		UPDATE mitm_rules
		SET name = ?, enabled = ?, source_host = ?, target_provider = ?, model_mappings = ?, path_rewrite = ?, priority = ?, updated_at = ?
		WHERE id = ?
	`

	result, err := r.db.Exec(query,
		rule.Name,
		boolToInt(rule.Enabled),
		rule.SourceHost,
		rule.TargetProvider,
		string(mappingsJSON),
		rule.PathRewrite,
		rule.Priority,
		rule.UpdatedAt.Unix(),
		rule.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update rule: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("rule not found: %s", rule.ID)
	}

	return nil
}

// Delete deletes a rule by ID
func (r *RuleService) Delete(id string) error {
	query := `DELETE FROM mitm_rules WHERE id = ?`
	result, err := r.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete rule: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("rule not found: %s", id)
	}

	return nil
}

// Get retrieves a single rule by ID
func (r *RuleService) Get(id string) (*MITMRule, error) {
	query := `
		SELECT id, name, enabled, source_host, target_provider, model_mappings, path_rewrite, priority, created_at, updated_at
		FROM mitm_rules
		WHERE id = ?
	`

	var rule MITMRule
	var enabled int
	var mappingsJSON string
	var createdAt, updatedAt int64

	err := r.db.QueryRow(query, id).Scan(
		&rule.ID,
		&rule.Name,
		&enabled,
		&rule.SourceHost,
		&rule.TargetProvider,
		&mappingsJSON,
		&rule.PathRewrite,
		&rule.Priority,
		&createdAt,
		&updatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("rule not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query rule: %w", err)
	}

	rule.Enabled = enabled != 0
	rule.CreatedAt = time.Unix(createdAt, 0)
	rule.UpdatedAt = time.Unix(updatedAt, 0)

	// Unmarshal model mappings
	if err := json.Unmarshal([]byte(mappingsJSON), &rule.ModelMappings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal model mappings: %w", err)
	}

	return &rule, nil
}

// List retrieves all rules, ordered by priority (descending) then created_at
func (r *RuleService) List() ([]*MITMRule, error) {
	query := `
		SELECT id, name, enabled, source_host, target_provider, model_mappings, path_rewrite, priority, created_at, updated_at
		FROM mitm_rules
		ORDER BY priority DESC, created_at ASC
	`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query rules: %w", err)
	}
	defer rows.Close()

	var rules []*MITMRule
	for rows.Next() {
		var rule MITMRule
		var enabled int
		var mappingsJSON string
		var createdAt, updatedAt int64

		err := rows.Scan(
			&rule.ID,
			&rule.Name,
			&enabled,
			&rule.SourceHost,
			&rule.TargetProvider,
			&mappingsJSON,
			&rule.PathRewrite,
			&rule.Priority,
			&createdAt,
			&updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan rule: %w", err)
		}

		rule.Enabled = enabled != 0
		rule.CreatedAt = time.Unix(createdAt, 0)
		rule.UpdatedAt = time.Unix(updatedAt, 0)

		// Unmarshal model mappings
		if err := json.Unmarshal([]byte(mappingsJSON), &rule.ModelMappings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal model mappings: %w", err)
		}

		rules = append(rules, &rule)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return rules, nil
}

// ListEnabled retrieves only enabled rules, ordered by priority
func (r *RuleService) ListEnabled() ([]*MITMRule, error) {
	query := `
		SELECT id, name, enabled, source_host, target_provider, model_mappings, path_rewrite, priority, created_at, updated_at
		FROM mitm_rules
		WHERE enabled = 1
		ORDER BY priority DESC, created_at ASC
	`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query enabled rules: %w", err)
	}
	defer rows.Close()

	var rules []*MITMRule
	for rows.Next() {
		var rule MITMRule
		var enabled int
		var mappingsJSON string
		var createdAt, updatedAt int64

		err := rows.Scan(
			&rule.ID,
			&rule.Name,
			&enabled,
			&rule.SourceHost,
			&rule.TargetProvider,
			&mappingsJSON,
			&rule.PathRewrite,
			&rule.Priority,
			&createdAt,
			&updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan rule: %w", err)
		}

		rule.Enabled = enabled != 0
		rule.CreatedAt = time.Unix(createdAt, 0)
		rule.UpdatedAt = time.Unix(updatedAt, 0)

		// Unmarshal model mappings
		if err := json.Unmarshal([]byte(mappingsJSON), &rule.ModelMappings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal model mappings: %w", err)
		}

		rules = append(rules, &rule)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return rules, nil
}

// FindByHost retrieves a rule by source host (exact match)
func (r *RuleService) FindByHost(host string) (*MITMRule, error) {
	query := `
		SELECT id, name, enabled, source_host, target_provider, model_mappings, path_rewrite, priority, created_at, updated_at
		FROM mitm_rules
		WHERE source_host = ? AND enabled = 1
		ORDER BY priority DESC
		LIMIT 1
	`

	var rule MITMRule
	var enabled int
	var mappingsJSON string
	var createdAt, updatedAt int64

	err := r.db.QueryRow(query, host).Scan(
		&rule.ID,
		&rule.Name,
		&enabled,
		&rule.SourceHost,
		&rule.TargetProvider,
		&mappingsJSON,
		&rule.PathRewrite,
		&rule.Priority,
		&createdAt,
		&updatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, nil // Not found is not an error
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query rule by host: %w", err)
	}

	rule.Enabled = enabled != 0
	rule.CreatedAt = time.Unix(createdAt, 0)
	rule.UpdatedAt = time.Unix(updatedAt, 0)

	// Unmarshal model mappings
	if err := json.Unmarshal([]byte(mappingsJSON), &rule.ModelMappings); err != nil {
		return nil, fmt.Errorf("failed to unmarshal model mappings: %w", err)
	}

	return &rule, nil
}

// ToggleEnabled toggles the enabled status of a rule
func (r *RuleService) ToggleEnabled(id string) error {
	query := `
		UPDATE mitm_rules
		SET enabled = NOT enabled, updated_at = ?
		WHERE id = ?
	`

	result, err := r.db.Exec(query, time.Now().Unix(), id)
	if err != nil {
		return fmt.Errorf("failed to toggle rule: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("rule not found: %s", id)
	}

	return nil
}

// Wails exported methods

// CreateRule creates a new MITM rule (exported for Wails)
func (r *RuleService) CreateRule(rule *MITMRule) error {
	return r.Create(rule)
}

// UpdateRule updates an existing rule (exported for Wails)
func (r *RuleService) UpdateRule(rule *MITMRule) error {
	return r.Update(rule)
}

// DeleteRule deletes a rule by ID (exported for Wails)
func (r *RuleService) DeleteRule(id string) error {
	return r.Delete(id)
}

// GetRule retrieves a single rule (exported for Wails)
func (r *RuleService) GetRule(id string) (*MITMRule, error) {
	return r.Get(id)
}

// ListRules retrieves all rules (exported for Wails)
func (r *RuleService) ListRules() ([]*MITMRule, error) {
	return r.List()
}

// ToggleRuleEnabled toggles rule status (exported for Wails)
func (r *RuleService) ToggleRuleEnabled(id string) error {
	return r.ToggleEnabled(id)
}
