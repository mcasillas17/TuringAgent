package repository

import (
	"context"
	"database/sql"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

const toolRegistryInitializedKey = "tool_registry_initialized"

type DiscoveredTool struct {
	ServerName string
	ToolName   string
	SchemaJSON string
	Policy     string
}

// UpsertTools replaces the enabled tool snapshot while retaining rows for
// tools that disappeared. Existing policies remain authoritative.
func (r *Repository) UpsertTools(ctx context.Context, tools []DiscoveredTool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	discoveredAt := now()
	if _, err := tx.ExecContext(ctx, `UPDATE tools SET enabled = 0 WHERE enabled = 1`); err != nil {
		return err
	}
	for _, tool := range tools {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tools (id, server_name, tool_name, policy, schema_json, enabled, discovered_at)
			VALUES (?, ?, ?, ?, ?, 1, ?)
			ON CONFLICT(server_name, tool_name) DO UPDATE SET
				schema_json = excluded.schema_json,
				enabled = 1,
				discovered_at = excluded.discovered_at
		`, ids.New("tool"), tool.ServerName, tool.ToolName, tool.Policy, tool.SchemaJSON, discoveredAt); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO settings (key, value_json, updated_at)
		VALUES (?, 'true', ?)
		ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at
	`, toolRegistryInitializedKey, discoveredAt); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) ListEnabledTools(ctx context.Context) ([]DiscoveredTool, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT server_name, tool_name, policy, schema_json
		FROM tools
		WHERE enabled = 1
		ORDER BY server_name, tool_name
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	tools := make([]DiscoveredTool, 0)
	for rows.Next() {
		var tool DiscoveredTool
		if err := rows.Scan(&tool.ServerName, &tool.ToolName, &tool.Policy, &tool.SchemaJSON); err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tools, nil
}

func (r *Repository) GetToolPolicy(ctx context.Context, serverName string, toolName string) (string, bool, bool, error) {
	var policy string
	var enabled int
	err := r.db.QueryRowContext(ctx, `
		SELECT policy, enabled
		FROM tools
		WHERE server_name = ? AND tool_name = ?
	`, serverName, toolName).Scan(&policy, &enabled)
	if err == nil {
		return policy, enabled == 1, true, nil
	}
	if err == sql.ErrNoRows {
		return "", false, false, nil
	}
	return "", false, false, err
}

func (r *Repository) ToolRegistryInitialized(ctx context.Context) (bool, error) {
	var initialized bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM settings WHERE key = ? AND value_json = 'true'
		)
	`, toolRegistryInitializedKey).Scan(&initialized)
	return initialized, err
}
