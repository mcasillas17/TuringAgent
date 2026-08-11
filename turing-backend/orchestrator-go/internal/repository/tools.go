package repository

import (
	"context"
	"database/sql"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

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

	if _, err := tx.ExecContext(ctx, `UPDATE tools SET enabled = 0 WHERE enabled = 1`); err != nil {
		return err
	}
	discoveredAt := now()
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

func (r *Repository) GetToolPolicy(ctx context.Context, serverName string, toolName string) (string, bool, error) {
	var policy string
	err := r.db.QueryRowContext(ctx, `
		SELECT policy
		FROM tools
		WHERE server_name = ? AND tool_name = ? AND enabled = 1
	`, serverName, toolName).Scan(&policy)
	if err == nil {
		return policy, true, nil
	}
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return "", false, err
}
