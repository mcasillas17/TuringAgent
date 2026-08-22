package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

type MCPServerTier string

const (
	MCPServerTierBundled        MCPServerTier = "bundled"
	MCPServerTierLocalContainer MCPServerTier = "local_container"
	MCPServerTierRemoteURL      MCPServerTier = "remote_url"
)

var (
	ErrMCPServerNotFound         = errors.New("MCP server not found")
	ErrMCPServerBundled          = errors.New("bundled MCP server cannot be imported")
	ErrMCPServerImportSuppressed = errors.New("MCP server import is suppressed after deletion")
	ErrMCPServerNameTaken        = errors.New("MCP server name is already registered")
	ErrMCPToolNameCollision      = errors.New("MCP tool name collides with another server")
	ErrMCPToolNotFound           = errors.New("MCP tool not found")
)

type MCPServerRecord struct {
	ID          string
	Name        string
	Transport   string
	URL         string
	SealedToken []byte
	Tier        MCPServerTier
	Enabled     bool
	Status      string
	StatusError string
	CreatedAt   string
}

type MCPServerTool struct {
	Name       string
	Policy     string
	SchemaJSON string
	Enabled    bool
	Present    bool
}

type ImportedMCPServer struct {
	Name        string
	URL         string
	SealedToken []byte
	Tier        MCPServerTier
	// Tools is an optional static tools snapshot — already fully
	// validated and policy-defaulted by the caller — that ImportMCPServer
	// reconciles atomically with the server row itself. A nil slice
	// means the mcp.json entry carried no "tools" key at all; a non-nil
	// empty slice means an explicit `"tools": []`; both leave a legacy
	// placeholder's carried tools withdrawn (see ImportMCPServer) with
	// nothing reconfirmed. RegisterMCPServer ignores this field: direct
	// registration never accepts a tools snapshot.
	Tools []MCPServerTool
}

// MCPImportResult reports what ImportMCPServer did: Created is false when an
// entry of that name already existed and the row was left untouched.
type MCPImportResult struct {
	Server  MCPServerRecord
	Created bool
}

type RemoteMCPServerEgress struct {
	ServerName   string `json:"serverName"`
	Endpoint     string `json:"endpoint"`
	EndpointHost string `json:"endpointHost"`
}

type MCPImportIssue struct {
	Name   string
	Reason string
}

// ImportMCPServer registers a server discovered from an mcp.json import,
// reconciling its optional Tools snapshot in the very same transaction as
// the server row itself. It is create-only: if a row of that name already
// exists (and is not bundled) with a real, non-empty URL, it is left
// completely untouched — Created is reported false and Tools is not even
// inspected — so a reimport never disturbs an operator's enablement,
// endpoint, sealed token, liveness, or tool policies. Bundled and
// tombstoned names are refused so the caller can decide how to surface
// that as unsupported without needing a separate lookup call.
//
// The one narrow exception is a legacy placeholder from migration 0016: a
// non-bundled row with url == "", seeded disabled so a pre-registry
// runtime's tool policy and schema survived until an operator imports a
// real endpoint. That row is adopted in place (its id is preserved, and
// only url, sealed_token, and tier are updated) rather than skipped
// forever, and Created is reported true so the caller classifies it as
// imported. Because that endpoint was never verified, adopting it always
// withdraws every tool it carried (present=0, enabled=0) before
// reconfirming whichever tools this call's Tools snapshot supplies — which
// may be none, if the reimported entry carried no "tools" key at all or an
// explicit empty one. A tool that survives keeps whatever policy an
// operator had already edited onto it; only its presence/enabled/schema
// state is touched.
//
// Reconciling Tools is folded into the same tx as the row mutation (via
// replaceServerToolsTx, the same helper ReplaceMCPServerTools itself
// uses): a bundled-namespace or inter-server tool-name collision rolls
// back the row insert/adoption too, so a corrected reimport sees no row
// (new name) or the placeholder exactly as it was (adoption) rather than a
// partial row it could only ever skip.
func (r *Repository) ImportMCPServer(ctx context.Context, input ImportedMCPServer) (MCPImportResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return MCPImportResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var suppressed int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM mcp_import_tombstones WHERE name = ?
	`, input.Name).Scan(&suppressed); err != nil {
		return MCPImportResult{}, err
	}
	if suppressed != 0 {
		return MCPImportResult{}, ErrMCPServerImportSuppressed
	}

	var existingID, existingTier, existingURL string
	err = tx.QueryRowContext(ctx, `SELECT id, tier, url FROM mcp_servers WHERE name = ?`, input.Name).
		Scan(&existingID, &existingTier, &existingURL)
	switch {
	case err == nil && MCPServerTier(existingTier) == MCPServerTierBundled:
		return MCPImportResult{}, ErrMCPServerBundled
	case err == nil && existingURL != "":
		record, ferr := mcpServerByName(ctx, tx, input.Name)
		if ferr != nil {
			return MCPImportResult{}, ferr
		}
		if cerr := tx.Commit(); cerr != nil {
			return MCPImportResult{}, cerr
		}
		return MCPImportResult{Server: record, Created: false}, nil
	case err == nil:
		// Legacy placeholder: adopt it in place. Only url, sealed_token,
		// and tier change directly; enabled and mcp_server_status are
		// left exactly as they were. Tools are reconciled below, after
		// the update, via the same withdraw-then-reconfirm helper live
		// discovery uses.
		if _, uerr := tx.ExecContext(ctx, `
			UPDATE mcp_servers
			SET url = ?, sealed_token = ?, tier = ?
			WHERE id = ?
		`, input.URL, nullableBytes(input.SealedToken), string(input.Tier), existingID); uerr != nil {
			return MCPImportResult{}, uerr
		}
		record, ferr := mcpServerByName(ctx, tx, input.Name)
		if ferr != nil {
			return MCPImportResult{}, ferr
		}
		if terr := replaceServerToolsTx(ctx, tx, record, input.Tools); terr != nil {
			return MCPImportResult{}, terr
		}
		if cerr := tx.Commit(); cerr != nil {
			return MCPImportResult{}, cerr
		}
		return MCPImportResult{Server: record, Created: true}, nil
	case !errors.Is(err, sql.ErrNoRows):
		return MCPImportResult{}, err
	}

	serverID := ids.New("mcp")
	createdAt := now()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO mcp_servers (
			id, name, transport, url, sealed_token, tier, enabled, created_at
		) VALUES (?, ?, 'http', ?, ?, ?, 0, ?)
	`, serverID, input.Name, input.URL, nullableBytes(input.SealedToken), string(input.Tier), createdAt); err != nil {
		return MCPImportResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO mcp_server_status (mcp_server_id, status)
		VALUES (?, 'unknown')
	`, serverID); err != nil {
		return MCPImportResult{}, err
	}
	record, err := mcpServerByName(ctx, tx, input.Name)
	if err != nil {
		return MCPImportResult{}, err
	}
	if err := replaceServerToolsTx(ctx, tx, record, input.Tools); err != nil {
		return MCPImportResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return MCPImportResult{}, err
	}
	return MCPImportResult{Server: record, Created: true}, nil
}

// RegisterMCPServer explicitly (re)registers a server: it atomically clears
// any matching import tombstone and inserts a new disabled row. Any existing
// name is refused (bundled names return ErrMCPServerBundled; anything else
// returns ErrMCPServerNameTaken), and a refusal never clears the tombstone —
// the existing-name check runs, and fails, before the tombstone delete.
func (r *Repository) RegisterMCPServer(ctx context.Context, input ImportedMCPServer) (MCPServerRecord, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return MCPServerRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var existingTier string
	err = tx.QueryRowContext(ctx, `SELECT tier FROM mcp_servers WHERE name = ?`, input.Name).Scan(&existingTier)
	switch {
	case err == nil && MCPServerTier(existingTier) == MCPServerTierBundled:
		return MCPServerRecord{}, ErrMCPServerBundled
	case err == nil:
		return MCPServerRecord{}, ErrMCPServerNameTaken
	case !errors.Is(err, sql.ErrNoRows):
		return MCPServerRecord{}, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_import_tombstones WHERE name = ?`, input.Name); err != nil {
		return MCPServerRecord{}, err
	}

	serverID := ids.New("mcp")
	createdAt := now()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO mcp_servers (
			id, name, transport, url, sealed_token, tier, enabled, created_at
		) VALUES (?, ?, 'http', ?, ?, ?, 0, ?)
	`, serverID, input.Name, input.URL, nullableBytes(input.SealedToken), string(input.Tier), createdAt); err != nil {
		return MCPServerRecord{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO mcp_server_status (mcp_server_id, status)
		VALUES (?, 'unknown')
	`, serverID); err != nil {
		return MCPServerRecord{}, err
	}
	record, err := mcpServerByName(ctx, tx, input.Name)
	if err != nil {
		return MCPServerRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return MCPServerRecord{}, err
	}
	return record, nil
}

// ReplaceMCPServerToken replaces a non-bundled server's sealed credential in
// place and returns the updated record. Passing empty bytes clears the
// column to SQL NULL rather than storing a zero-length blob.
//
// A prior Up/Down liveness observation was made using the credential this
// call is replacing (or clearing), so it says nothing about whether the
// new one — or the absence of one — actually works: in the same
// transaction as the sealed_token update, liveness is reset to unknown
// with an empty status message. A failure resetting liveness rolls back
// the token change too, rather than leaving a rotated token paired with a
// stale liveness reading from the credential it just replaced.
func (r *Repository) ReplaceMCPServerToken(ctx context.Context, serverID string, sealedToken []byte) (MCPServerRecord, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return MCPServerRecord{}, err
	}
	defer func() { _ = tx.Rollback() }()

	server, err := mcpServerByID(ctx, tx, serverID)
	if errors.Is(err, sql.ErrNoRows) {
		return MCPServerRecord{}, ErrMCPServerNotFound
	}
	if err != nil {
		return MCPServerRecord{}, err
	}
	if server.Tier == MCPServerTierBundled {
		return MCPServerRecord{}, ErrMCPServerBundled
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE mcp_servers SET sealed_token = ? WHERE id = ?
	`, nullableBytes(sealedToken), serverID); err != nil {
		return MCPServerRecord{}, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE mcp_server_status SET status = 'unknown', error = '', checked_at = NULL WHERE mcp_server_id = ?
	`, serverID)
	if err != nil {
		return MCPServerRecord{}, err
	}
	if err := expectOneRow(result, "MCP server status not found"); err != nil {
		return MCPServerRecord{}, err
	}
	record, err := mcpServerByID(ctx, tx, serverID)
	if err != nil {
		return MCPServerRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return MCPServerRecord{}, err
	}
	return record, nil
}

// GetMCPServerByName looks up a server's current disposition by name so a
// caller (such as an import routine) can decide whether it is genuinely new
// before doing more expensive work, without mutating anything.
func (r *Repository) GetMCPServerByName(ctx context.Context, name string) (MCPServerRecord, error) {
	record, err := mcpServerByName(ctx, r.db, name)
	if errors.Is(err, sql.ErrNoRows) {
		return MCPServerRecord{}, ErrMCPServerNotFound
	}
	return record, err
}

// MCPServerTombstoned reports whether name was deleted and remains
// suppressed from reimport.
func (r *Repository) MCPServerTombstoned(ctx context.Context, name string) (bool, error) {
	var count int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM mcp_import_tombstones WHERE name = ?
	`, name).Scan(&count)
	return count != 0, err
}

func (r *Repository) ListMCPServers(ctx context.Context) ([]MCPServerRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.id, s.name, s.transport, s.url, s.sealed_token, s.tier, s.enabled,
			COALESCE(st.status, 'unknown'), COALESCE(st.error, ''), s.created_at
		FROM mcp_servers s
		LEFT JOIN mcp_server_status st ON st.mcp_server_id = s.id
		ORDER BY s.name
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	servers := make([]MCPServerRecord, 0)
	for rows.Next() {
		record, err := scanMCPServer(rows)
		if err != nil {
			return nil, err
		}
		servers = append(servers, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return servers, nil
}

func (r *Repository) GetMCPServer(ctx context.Context, serverID string) (MCPServerRecord, error) {
	server, err := mcpServerByID(ctx, r.db, serverID)
	if errors.Is(err, sql.ErrNoRows) {
		return MCPServerRecord{}, ErrMCPServerNotFound
	}
	return server, err
}

func (r *Repository) MCPToolAvailable(ctx context.Context, serverName string, toolName string) (bool, error) {
	var available bool
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(s.enabled = 1 AND (
			(t.id IS NULL AND s.tier = 'bundled') OR (
				t.policy != 'disabled' AND (
					s.tier = 'bundled' OR t.present = 1
				)
			)
		), 0)
		FROM mcp_servers s
		LEFT JOIN tools t ON t.mcp_server_id = s.id AND t.tool_name = ?
		WHERE s.name = ?
	`, toolName, serverName).Scan(&available)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	return available, err
}

func (r *Repository) MCPDispatchActive(
	ctx context.Context,
	serverID string,
	runID string,
	toolName string,
	expectedPolicy string,
) (bool, error) {
	var active bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM mcp_servers server
			JOIN tools tool ON tool.mcp_server_id = server.id
			JOIN agent_runs run ON run.id = ?
			JOIN sessions session ON session.id = run.session_id
			WHERE server.id = ?
				AND tool.tool_name = ?
				AND tool.policy = ?
				AND server.enabled = 1
				AND tool.present = 1
				AND tool.enabled = 1
				AND run.execution_active = 1
				AND run.status = 'running'
				AND session.deletion_state = 'active'
		)
	`, runID, serverID, toolName, expectedPolicy).Scan(&active)
	return active, err
}

func (r *Repository) SetMCPServerEnabled(ctx context.Context, serverID string, enabled bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	defer func() { _ = tx.Rollback() }()
	server, err := mcpServerByID(ctx, tx, serverID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMCPServerNotFound
	}
	if err != nil {
		return err
	}
	value := 0
	if enabled {
		value = 1
	}
	result, err := tx.ExecContext(ctx, `UPDATE mcp_servers SET enabled = ? WHERE id = ?`, value, serverID)
	if err != nil {
		return err
	}
	if err := expectOneRow(result, "MCP server not found"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE tools
		SET enabled = CASE WHEN ? = 1 AND present = 1 AND policy != 'disabled' THEN 1 ELSE 0 END
		WHERE mcp_server_id = ?
	`, value, server.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) SetMCPServerStatus(ctx context.Context, serverID string, statusValue string, message string) error {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO mcp_server_status (mcp_server_id, status, error, checked_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(mcp_server_id) DO UPDATE SET
			status = excluded.status,
			error = excluded.error,
			checked_at = excluded.checked_at
	`, serverID, statusValue, message, now())
	if err != nil {
		return err
	}
	if err := expectOneRow(result, "MCP server not found"); err != nil {
		return err
	}
	return nil
}

func (r *Repository) SetMCPToolPolicy(ctx context.Context, serverID string, toolName string, policy string) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE tools
		SET policy = ?,
			present = CASE
				WHEN EXISTS (
					SELECT 1 FROM mcp_servers
					WHERE id = tools.mcp_server_id AND tier = 'bundled'
				) THEN 1
				ELSE present
			END,
			enabled = CASE
				WHEN ? = 'disabled' THEN 0
				WHEN present = 0 AND NOT EXISTS (
					SELECT 1 FROM mcp_servers
					WHERE id = tools.mcp_server_id AND tier = 'bundled'
				) THEN 0
				WHEN EXISTS (
					SELECT 1 FROM mcp_servers
					WHERE id = tools.mcp_server_id AND enabled = 1
				) THEN 1
				ELSE 0
			END
		WHERE mcp_server_id = ? AND tool_name = ?
	`, policy, policy, serverID, toolName)
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

func (r *Repository) DeleteMCPServer(ctx context.Context, serverID string) error {
	server, err := r.GetMCPServer(ctx, serverID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMCPServerNotFound
	}
	if err != nil {
		return err
	}
	if server.Tier == MCPServerTierBundled {
		return ErrMCPServerBundled
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO mcp_import_tombstones (name, deleted_at)
		VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET deleted_at = excluded.deleted_at
	`, server.Name, now()); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM mcp_servers WHERE id = ?`, serverID)
	if err != nil {
		return err
	}
	if err := expectOneRow(result, "MCP server not found"); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) ReplaceMCPImportIssues(ctx context.Context, issues map[string]string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_import_issues`); err != nil {
		return err
	}
	reportedAt := now()
	for name, reason := range issues {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO mcp_import_issues (name, reason, reported_at)
			VALUES (?, ?, ?)
		`, name, reason, reportedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) ListMCPImportIssues(ctx context.Context) ([]MCPImportIssue, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT name, reason
		FROM mcp_import_issues
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	issues := make([]MCPImportIssue, 0)
	for rows.Next() {
		var issue MCPImportIssue
		if err := rows.Scan(&issue.Name, &issue.Reason); err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return issues, nil
}

func (r *Repository) ReplaceMCPServerTools(ctx context.Context, serverID string, tools []MCPServerTool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	server, err := mcpServerByID(ctx, tx, serverID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMCPServerNotFound
	}
	if err != nil {
		return err
	}
	if err := replaceServerToolsTx(ctx, tx, server, tools); err != nil {
		return err
	}
	return tx.Commit()
}

// replaceServerToolsTx is the one shared implementation of "withdraw every
// tool currently attributed to server, then reconfirm exactly the tools
// supplied, refusing a name collision with another server's present tool":
// ReplaceMCPServerTools uses it for live discovery (opening and committing
// its own transaction around it), and ImportMCPServer folds it into its
// own already-open transaction so a static mcp.json snapshot's tool
// reconciliation rolls back together with the server row mutation it
// belongs to. Both callers see identical collision/upsert behavior because
// there is exactly one implementation of it.
func replaceServerToolsTx(ctx context.Context, tx *sql.Tx, server MCPServerRecord, tools []MCPServerTool) error {
	if _, err := tx.ExecContext(ctx, `UPDATE tools SET present = 0, enabled = 0 WHERE mcp_server_id = ?`, server.ID); err != nil {
		return err
	}
	discoveredAt := now()
	for _, tool := range tools {
		var owner string
		err := tx.QueryRowContext(ctx, `
			SELECT server_name
			FROM tools
			WHERE tool_name = ? AND server_name != ? AND present = 1
			LIMIT 1
		`, tool.Name, server.Name).Scan(&owner)
		if err == nil {
			return fmt.Errorf("%w: %s is already owned by %s", ErrMCPToolNameCollision, tool.Name, owner)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		enabled := 0
		if server.Enabled {
			enabled = 1
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tools (
				id, server_name, tool_name, policy, schema_json, enabled, discovered_at, mcp_server_id, present
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
			ON CONFLICT(server_name, tool_name) DO UPDATE SET
				schema_json = excluded.schema_json,
				enabled = CASE WHEN tools.policy = 'disabled' THEN 0 ELSE excluded.enabled END,
				discovered_at = excluded.discovered_at,
				mcp_server_id = excluded.mcp_server_id,
				present = 1
		`, ids.New("tool"), server.Name, tool.Name, tool.Policy, tool.SchemaJSON, enabled, discoveredAt, server.ID); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListMCPServerTools(ctx context.Context, serverID string) ([]MCPServerTool, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT tool_name, policy, schema_json, enabled, present
		FROM tools
		WHERE mcp_server_id = ?
		ORDER BY tool_name
	`, serverID)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tools, nil
}

func (r *Repository) RemoteMCPServersForTools(ctx context.Context, selectedTools []string) ([]RemoteMCPServerEgress, error) {
	destinations := make([]RemoteMCPServerEgress, 0)
	seen := make(map[string]struct{})
	for _, selected := range selectedTools {
		serverName, toolName, ok := strings.Cut(selected, "/")
		if !ok || serverName == "" || toolName == "" {
			return nil, fmt.Errorf("selected tool %q must use server/tool", selected)
		}
		var endpoint string
		err := r.db.QueryRowContext(ctx, `
			SELECT s.url
			FROM mcp_servers s
			JOIN tools t ON t.mcp_server_id = s.id
			WHERE s.name = ? AND t.tool_name = ?
				AND s.tier = 'remote_url' AND s.enabled = 1 AND t.enabled = 1
		`, serverName, toolName).Scan(&endpoint)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if _, exists := seen[serverName]; exists {
			continue
		}
		parsed, err := backendegress.ParseKeyedEndpoint(endpoint)
		if err != nil {
			return nil, fmt.Errorf("remote MCP server %q endpoint is insecure", serverName)
		}
		seen[serverName] = struct{}{}
		destinations = append(destinations, RemoteMCPServerEgress{
			ServerName:   serverName,
			Endpoint:     parsed.Canonical,
			EndpointHost: parsed.Host,
		})
	}
	slices.SortFunc(destinations, func(left, right RemoteMCPServerEgress) int {
		return strings.Compare(left.ServerName, right.ServerName)
	})
	return destinations, nil
}

type mcpServerScanner interface {
	Scan(...any) error
}

type mcpServerQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func mcpServerByID(ctx context.Context, q mcpServerQuerier, serverID string) (MCPServerRecord, error) {
	return scanMCPServer(q.QueryRowContext(ctx, `
		SELECT s.id, s.name, s.transport, s.url, s.sealed_token, s.tier, s.enabled,
			COALESCE(st.status, 'unknown'), COALESCE(st.error, ''), s.created_at
		FROM mcp_servers s
		LEFT JOIN mcp_server_status st ON st.mcp_server_id = s.id
		WHERE s.id = ?
	`, serverID))
}

func mcpServerByName(ctx context.Context, q mcpServerQuerier, name string) (MCPServerRecord, error) {
	return scanMCPServer(q.QueryRowContext(ctx, `
		SELECT s.id, s.name, s.transport, s.url, s.sealed_token, s.tier, s.enabled,
			COALESCE(st.status, 'unknown'), COALESCE(st.error, ''), s.created_at
		FROM mcp_servers s
		LEFT JOIN mcp_server_status st ON st.mcp_server_id = s.id
		WHERE s.name = ?
	`, name))
}

func scanMCPServer(scanner mcpServerScanner) (MCPServerRecord, error) {
	var record MCPServerRecord
	var sealedToken []byte
	if err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.Transport,
		&record.URL,
		&sealedToken,
		&record.Tier,
		&record.Enabled,
		&record.Status,
		&record.StatusError,
		&record.CreatedAt,
	); err != nil {
		return MCPServerRecord{}, err
	}
	record.SealedToken = append([]byte(nil), sealedToken...)
	return record, nil
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
