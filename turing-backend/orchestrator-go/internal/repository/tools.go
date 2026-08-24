package repository

import (
	"context"
	"database/sql"
	"errors"

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
		if tool.ServerName == "skills" || tool.ServerName == "integrations" {
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
	// The same registry-wide aggregate budget replaceServerToolsTx
	// enforces for every other tool-reconciliation path (ImportMCPServer,
	// RegisterMCPServer's placeholder adoption, ReplaceMCPServerTools)
	// applies here too: before this check, UpsertTools — the
	// bundled/skills/legacy path the runtime uses to publish worker tool
	// capabilities — was the one write path that could grow the
	// registry's aggregate tool byte total (see MaxMCPRegistryToolBytes)
	// without limit, even though a ListMcpServers response sums every
	// server's tools together regardless of which path populated them,
	// present or withdrawn. Checked after the withdrawal and every
	// replacement row above have already run — not computed beforehand
	// from a present-only baseline plus the incoming tools' own Go-side
	// byte count — so this one query measures exactly the table's real
	// resulting state, the same way replaceServerToolsTx's own budget
	// check does. A third-party server's own tools (populated entirely
	// separately, via replaceServerToolsTx) are unaffected by the
	// withdrawal above and so are still counted here — the two paths
	// share one registry-wide budget, not two independently-budgeted
	// halves. A refusal returns before tx.Commit, so the deferred
	// Rollback above discards the withdrawal and every replacement row
	// together: a refused snapshot never leaves the bundled/skills/legacy
	// tools it was about to replace withdrawn with nothing reconfirmed.
	totalBytes, err := aggregateAllToolBytes(ctx, tx)
	if err != nil {
		return err
	}
	if totalBytes > MaxMCPRegistryToolBytes {
		return ErrMCPRegistryToolBudgetExceeded
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

// PseudoServerToolAvailable is policy-only. A missing row is available so a
// newly discovered pseudo-server tool can bootstrap into the registry.
// Deliberately do not add present/enabled checks: UpsertTools derives those
// bits from the report this predicate gates.
func (r *Repository) PseudoServerToolAvailable(ctx context.Context, serverName, toolName string) (bool, error) {
	var policy string
	err := r.db.QueryRowContext(ctx, `
		SELECT policy FROM tools
		WHERE server_name = ? AND tool_name = ? AND mcp_server_id IS NULL
	`, serverName, toolName).Scan(&policy)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return policy != "disabled", nil
}

func (r *Repository) PseudoServerToolPolicy(ctx context.Context, serverName, toolName string) (string, bool, error) {
	var policy string
	err := r.db.QueryRowContext(ctx, `
		SELECT policy FROM tools
		WHERE server_name = ? AND tool_name = ? AND mcp_server_id IS NULL
	`, serverName, toolName).Scan(&policy)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return policy, err == nil, err
}

func (r *Repository) SetToolPolicyByName(ctx context.Context, serverName, toolName, policy string) error {
	// The server-backed branches mirror SetMCPToolPolicy exactly. Dropping the
	// present clauses would let this public RPC resurrect a tool the server no
	// longer exports (present = 0 after its last discovery prune).
	result, err := r.db.ExecContext(ctx, `
		UPDATE tools
		SET policy = ?,
			present = CASE
				WHEN mcp_server_id IS NOT NULL AND EXISTS (
					SELECT 1 FROM mcp_servers
					WHERE id = tools.mcp_server_id AND tier = 'bundled'
				) THEN 1
				ELSE present
			END,
			enabled = CASE
				WHEN ? = 'disabled' THEN 0
				WHEN mcp_server_id IS NULL THEN 1
				WHEN present = 0 AND NOT EXISTS (
					SELECT 1 FROM mcp_servers
					WHERE id = tools.mcp_server_id AND tier = 'bundled'
				) THEN 0
				WHEN EXISTS (SELECT 1 FROM mcp_servers WHERE id = tools.mcp_server_id AND enabled = 1) THEN 1
				ELSE 0
			END
		WHERE server_name = ? AND tool_name = ?
	`, policy, policy, serverName, toolName)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrMCPToolNotFound
	}
	return nil
}

func (r *Repository) ListPseudoServerTools(ctx context.Context, serverName string) ([]MCPServerTool, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT tool_name, policy, schema_json, enabled, present
		FROM tools WHERE server_name = ? AND mcp_server_id IS NULL
		ORDER BY tool_name
	`, serverName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	tools := make([]MCPServerTool, 0)
	for rows.Next() {
		var tool MCPServerTool
		if err := rows.Scan(&tool.Name, &tool.Policy, &tool.SchemaJSON, &tool.Enabled, &tool.Present); err != nil {
			return nil, err
		}
		tools = append(tools, tool)
	}
	return tools, rows.Err()
}

func (r *Repository) GetToolByName(ctx context.Context, serverName, toolName string) (MCPServerTool, error) {
	var tool MCPServerTool
	err := r.db.QueryRowContext(ctx, `
		SELECT tool_name, policy, schema_json, enabled, present
		FROM tools WHERE server_name = ? AND tool_name = ?
	`, serverName, toolName).Scan(&tool.Name, &tool.Policy, &tool.SchemaJSON, &tool.Enabled, &tool.Present)
	if errors.Is(err, sql.ErrNoRows) {
		return MCPServerTool{}, ErrMCPToolNotFound
	}
	return tool, err
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
