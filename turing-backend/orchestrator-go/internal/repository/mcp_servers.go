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
	// nothing reconfirmed. RegisterMCPServer refuses this field with
	// ErrMCPServerToolsNotAllowed rather than silently ignoring it:
	// direct registration never accepts a tools snapshot.
	Tools []MCPServerTool
}

// MCPImportResult reports what ImportMCPServer did: Created is false when an
// entry of that name already existed and the row was left untouched.
type MCPImportResult struct {
	Server  MCPServerRecord
	Created bool
}

// MCPRegisterResult reports what RegisterMCPServer did: Adopted is true when
// the call reused an existing migration-0016 (or otherwise legacy)
// placeholder row in place (see adoptMCPServerPlaceholder), false when it
// inserted a genuinely new row. Computing this inside RegisterMCPServer's
// own transaction — rather than a separate pre-read the service layer would
// otherwise need before calling it — is what makes it race-safe: nothing
// between that read and the mutation can change which branch actually ran.
type MCPRegisterResult struct {
	Server  MCPServerRecord
	Adopted bool
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
// url, sealed_token, and tier are updated, and enabled is forced to 0 in
// that same UPDATE — the same way RegisterMCPServer's own
// adoptMCPServerPlaceholder branch forces it — rather than assumed already
// disabled by construction) rather than skipped forever, and Created is
// reported true so the caller classifies it as imported. Because that
// endpoint was never verified, adopting it always withdraws every tool it
// carried (present=0, enabled=0) before reconfirming whichever tools this
// call's Tools snapshot supplies — which may be none, if the reimported
// entry carried no "tools" key at all or an explicit empty one. A tool
// that survives keeps whatever policy an operator had already edited onto
// it; only its presence/enabled/schema state is touched. Liveness is reset
// to unknown/empty in the same transaction, for the same reason: whatever
// status a placeholder's url=="" row happened to carry says nothing about
// the real endpoint now replacing it.
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
		// Legacy placeholder: adopt it in place. url, sealed_token, and
		// tier change directly; enabled is forced to 0 explicitly in
		// this same UPDATE — not merely left alone on the assumption a
		// migration-0016 placeholder is always still disabled — the
		// same way adoptMCPServerPlaceholder (RegisterMCPServer's own
		// adoption branch) already forces it, so neither adoption path
		// can ever leave a row enabled if that assumption ever stops
		// holding (e.g. a future path that flips a placeholder enabled
		// before it is adopted). This also matters for the tools
		// reconciled below: replaceServerToolsTx seeds a reconfirmed
		// tool's enabled bit from this same record's Enabled field, so
		// a stale, still-enabled read here would silently activate a
		// static snapshot's tools before the adopted endpoint's first
		// live contact. The placeholder's endpoint was never verified —
		// its liveness reading was seeded a priori by migration 0016,
		// not observed — so it says nothing about whether the endpoint
		// this call adopts actually works: liveness is reset to unknown
		// with an empty status message in the same transaction as the
		// row update, the same way ReplaceMCPServerToken resets
		// liveness alongside a rotated credential. A failure resetting
		// it rolls back the whole adoption, rather than leaving an
		// adopted endpoint paired with a stale liveness reading from
		// before it existed. Tools are reconciled below, after the
		// update, via the same withdraw-then-reconfirm helper live
		// discovery uses.
		if _, uerr := tx.ExecContext(ctx, `
			UPDATE mcp_servers
			SET url = ?, sealed_token = ?, tier = ?, enabled = 0
			WHERE id = ?
		`, input.URL, nullableBytes(input.SealedToken), string(input.Tier), existingID); uerr != nil {
			return MCPImportResult{}, uerr
		}
		statusResult, uerr := tx.ExecContext(ctx, `
			UPDATE mcp_server_status SET status = 'unknown', error = '', checked_at = NULL WHERE mcp_server_id = ?
		`, existingID)
		if uerr != nil {
			return MCPImportResult{}, uerr
		}
		if uerr := expectOneRow(statusResult, "MCP server status not found"); uerr != nil {
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

// ErrMCPServerToolsNotAllowed is returned by RegisterMCPServer when the
// caller's ImportedMCPServer carries a Tools snapshot (nil vs. non-nil,
// same "present" semantics ImportedMCPServer.Tools documents for
// ImportMCPServer): direct registration never accepts one — a new,
// direct registration always starts with no tools and gets them from live
// discovery on first enable, the same way RegisterMcpServer's own service
// comment describes — so this guards the one field ImportMCPServer's own
// validated-snapshot handling depends on from ever reaching this
// unvalidated path, regardless of what a future or misbehaving caller
// passes in.
var ErrMCPServerToolsNotAllowed = errors.New("direct MCP server registration does not accept a tools snapshot")

// RegisterMCPServer explicitly (re)registers a server: depending on what it
// finds for that name, it either inserts a new disabled row (clearing any
// matching import tombstone atomically) or adopts an existing legacy
// placeholder row in place — see MCPRegisterResult.Adopted and
// adoptMCPServerPlaceholder below for the latter. Any other existing name is
// refused (bundled names return ErrMCPServerBundled; a real,
// already-configured row — non-empty url — returns ErrMCPServerNameTaken),
// and a refusal never clears the tombstone — the existing-name check runs,
// and fails, before the tombstone delete. A Tools snapshot on input is
// refused with ErrMCPServerToolsNotAllowed before any of that, so a caller
// cannot bypass the service layer's own tool validation (name/schema shape,
// bundled-namespace and inter-server collision checks) by handing tools
// straight to the repository.
//
// The one exception to "any existing name is refused" is a migration-0016
// (or otherwise legacy) placeholder: a disabled, non-bundled row with
// url=="". A mobile operator has no way to edit backend files the way file
// reimport is edited, so without this exception that placeholder can only
// ever be adopted by a file reimport, stranding them. Naming that exact
// server and supplying a real endpoint through this explicit RPC is treated
// as the operator's consent to adopt it: the row is updated in place (same
// id), forced disabled regardless of what it carried, and its liveness
// reset to unknown/empty — the placeholder's endpoint was never verified,
// so any liveness reading it carried says nothing about the one this call
// adopts, the same reasoning ReplaceMCPServerToken and ImportMCPServer's own
// placeholder adoption already apply. Every tool the placeholder carried is
// withdrawn (present=0, enabled=0) via the same replaceServerToolsTx helper
// live discovery and ImportMCPServer both use, called here with a nil
// snapshot (this RPC never accepts Tools) so nothing is reconfirmed —
// preserving whatever policy an operator had already edited onto a carried
// tool, since only its presence/enabled state is touched. That preserved
// policy intentionally stays on the withdrawn row: it is not reset to a
// default, and it reapplies only if the newly adopted endpoint later
// reconfirms that exact tool name (present=1) through live discovery. This
// is the deliberate migration-recovery contract migration 0016 exists for
// — fail-closed (present=0 keeps the tool unusable until reconfirmed) while
// not discarding an operator's prior edit — not an oversight to "fix" by
// resetting it.
//
// Which branch ran is reported back via MCPRegisterResult.Adopted, computed
// inside this same transaction rather than from a separate pre-read, so it
// can never race a concurrent registration/import deciding differently.
func (r *Repository) RegisterMCPServer(ctx context.Context, input ImportedMCPServer) (MCPRegisterResult, error) {
	if input.Tools != nil {
		return MCPRegisterResult{}, ErrMCPServerToolsNotAllowed
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return MCPRegisterResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var existingID, existingTier, existingURL string
	err = tx.QueryRowContext(ctx, `SELECT id, tier, url FROM mcp_servers WHERE name = ?`, input.Name).
		Scan(&existingID, &existingTier, &existingURL)
	switch {
	case err == nil && MCPServerTier(existingTier) == MCPServerTierBundled:
		return MCPRegisterResult{}, ErrMCPServerBundled
	case err == nil && existingURL != "":
		return MCPRegisterResult{}, ErrMCPServerNameTaken
	case err == nil:
		return r.adoptMCPServerPlaceholder(ctx, tx, existingID, input)
	case !errors.Is(err, sql.ErrNoRows):
		return MCPRegisterResult{}, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_import_tombstones WHERE name = ?`, input.Name); err != nil {
		return MCPRegisterResult{}, err
	}

	serverID := ids.New("mcp")
	createdAt := now()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO mcp_servers (
			id, name, transport, url, sealed_token, tier, enabled, created_at
		) VALUES (?, ?, 'http', ?, ?, ?, 0, ?)
	`, serverID, input.Name, input.URL, nullableBytes(input.SealedToken), string(input.Tier), createdAt); err != nil {
		return MCPRegisterResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO mcp_server_status (mcp_server_id, status)
		VALUES (?, 'unknown')
	`, serverID); err != nil {
		return MCPRegisterResult{}, err
	}
	record, err := mcpServerByName(ctx, tx, input.Name)
	if err != nil {
		return MCPRegisterResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return MCPRegisterResult{}, err
	}
	return MCPRegisterResult{Server: record, Adopted: false}, nil
}

// adoptMCPServerPlaceholder is RegisterMCPServer's placeholder-adoption
// branch: it updates the existing url-empty row's url/sealed_token/tier in
// place, forces it disabled (regardless of what it carried — an explicit
// registration always starts disabled, the same as a brand-new row),
// resets its liveness to unknown/empty (that reading predates the endpoint
// this call adopts, so it says nothing about it), withdraws every tool the
// placeholder carried without reconfirming any (RegisterMCPServer never
// accepts a Tools snapshot, so there is nothing to reconfirm), and returns
// the adopted record with Adopted set to true. tx is committed on success;
// the caller's deferred rollback is a harmless no-op afterward.
//
// Unlike the fresh-insert branch above, this never touches
// mcp_import_tombstones: a name reaching this branch is, by construction,
// not tombstoned. A tombstoned name has no mcp_servers row at all — the row
// is deleted (see DeleteMCPServer) exactly when the tombstone is written —
// so a row existing here (the SELECT above found one) and that same name
// being tombstoned are mutually exclusive states application invariants
// never let coexist. There is no tombstone-clearing step to add here, not
// because it was forgotten but because there is nothing for it to clear.
func (r *Repository) adoptMCPServerPlaceholder(ctx context.Context, tx *sql.Tx, existingID string, input ImportedMCPServer) (MCPRegisterResult, error) {
	if _, err := tx.ExecContext(ctx, `
		UPDATE mcp_servers
		SET url = ?, sealed_token = ?, tier = ?, enabled = 0
		WHERE id = ?
	`, input.URL, nullableBytes(input.SealedToken), string(input.Tier), existingID); err != nil {
		return MCPRegisterResult{}, err
	}
	statusResult, err := tx.ExecContext(ctx, `
		UPDATE mcp_server_status SET status = 'unknown', error = '', checked_at = NULL WHERE mcp_server_id = ?
	`, existingID)
	if err != nil {
		return MCPRegisterResult{}, err
	}
	if err := expectOneRow(statusResult, "MCP server status not found"); err != nil {
		return MCPRegisterResult{}, err
	}
	record, err := mcpServerByID(ctx, tx, existingID)
	if err != nil {
		return MCPRegisterResult{}, err
	}
	if err := replaceServerToolsTx(ctx, tx, record, nil); err != nil {
		return MCPRegisterResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return MCPRegisterResult{}, err
	}
	return MCPRegisterResult{Server: record, Adopted: true}, nil
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
// its own transaction around it), ImportMCPServer folds it into its own
// already-open transaction so a static mcp.json snapshot's tool
// reconciliation rolls back together with the server row mutation it
// belongs to, and adoptMCPServerPlaceholder folds it in the same way — with
// a nil tools snapshot, since RegisterMCPServer never accepts one — so a
// legacy placeholder's carried tools are withdrawn (not reconfirmed) in the
// same transaction as the row it withdraws them from. All three callers see
// identical collision/upsert behavior because there is exactly one
// implementation of it.
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
