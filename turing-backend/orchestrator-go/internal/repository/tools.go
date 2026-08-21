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
	if _, err := tx.ExecContext(ctx, `
		UPDATE tools
		SET present = 0, enabled = 0
		WHERE mcp_server_id IS NULL OR mcp_server_id IN (
			SELECT id FROM mcp_servers WHERE tier = 'bundled'
		)
	`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE mcp_server_status
		SET status = 'down', error = '', checked_at = ?
		WHERE mcp_server_id IN (
			SELECT id FROM mcp_servers WHERE tier = 'bundled'
		)
	`, discoveredAt); err != nil {
		return err
	}
	for _, tool := range tools {
		if tool.ServerName == "skills" {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO tools (
					id, server_name, tool_name, policy, schema_json, enabled, discovered_at, mcp_server_id, present
				) VALUES (?, ?, ?, ?, ?, 1, ?, NULL, 1)
				ON CONFLICT(server_name, tool_name) DO UPDATE SET
					schema_json = excluded.schema_json,
					enabled = CASE WHEN tools.policy = 'disabled' THEN 0 ELSE 1 END,
					discovered_at = excluded.discovered_at,
					mcp_server_id = NULL,
					present = 1
			`, ids.New("tool"), tool.ServerName, tool.ToolName, tool.Policy, tool.SchemaJSON, discoveredAt)
			if err != nil {
				return err
			}
			continue
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO tools (
				id, server_name, tool_name, policy, schema_json, enabled, discovered_at, mcp_server_id, present
			)
			SELECT ?, ?, ?, ?, ?, CASE WHEN ? = 'disabled' THEN 0 ELSE 1 END, ?, id, 1
			FROM mcp_servers
			WHERE name = ? AND enabled = 1
			ON CONFLICT(server_name, tool_name) DO UPDATE SET
				schema_json = excluded.schema_json,
				enabled = CASE WHEN tools.policy = 'disabled' THEN 0 ELSE 1 END,
				discovered_at = excluded.discovered_at,
				mcp_server_id = excluded.mcp_server_id,
				present = 1
		`, ids.New("tool"), tool.ServerName, tool.ToolName, tool.Policy, tool.SchemaJSON, tool.Policy, discoveredAt, tool.ServerName)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE mcp_server_status
			SET status = 'up', error = '', checked_at = ?
			WHERE mcp_server_id IN (
				SELECT id FROM mcp_servers WHERE tier = 'bundled' AND name = ?
			)
		`, discoveredAt, tool.ServerName); err != nil {
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
