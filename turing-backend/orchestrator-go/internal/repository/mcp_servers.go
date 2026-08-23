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

// MaxNonBundledMCPServers bounds how many non-bundled (third-party) MCP
// server rows the registry may hold at once. TuringAgent's own bundled
// servers ("system", "files", "skills") never count toward it: they are
// fixed in number, seeded by migrations rather than an operator's own
// import/registration action, and are exactly the rows an ImportMCPServer/
// RegisterMCPServer call already refuses to create or collide with
// regardless of this cap. Without a bound, an mcp.json import or repeated
// direct registrations could grow the registry — and therefore
// ListMcpServers' response — without limit; 256 comfortably covers any
// realistic third-party deployment while keeping that response bounded.
// Enforced only against a genuinely new row (see ImportMCPServer and
// RegisterMCPServer's own fresh-insert branches): adopting an existing
// legacy migration-0016 placeholder in place is an UPDATE, not an INSERT,
// so it never needs — and is never refused by — this cap.
const MaxNonBundledMCPServers = 256

var (
	ErrMCPServerNotFound         = errors.New("MCP server not found")
	ErrMCPServerBundled          = errors.New("bundled MCP server cannot be imported")
	ErrMCPServerImportSuppressed = errors.New("MCP server import is suppressed after deletion")
	ErrMCPServerNameTaken        = errors.New("MCP server name is already registered")
	ErrMCPToolNameCollision      = errors.New("MCP tool name collides with another server")
	ErrMCPToolNotFound           = errors.New("MCP tool not found")
	// ErrMCPServerRegistryFull is returned by ImportMCPServer and
	// RegisterMCPServer when creating the requested row would grow the
	// registry's non-bundled server count beyond MaxNonBundledMCPServers.
	// It is checked, and the row inserted, inside the same transaction as
	// every other disposition decision those two methods make, so two
	// concurrent callers racing to fill the last slot can never both
	// succeed: database.SetMaxOpenConns(1) (see internal/db) serializes
	// every transaction against this single-connection database, so the
	// count this check reads can never go stale before the INSERT that
	// follows it commits.
	ErrMCPServerRegistryFull = errors.New("MCP server registry is full")
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

	if full, cerr := nonBundledMCPServerRegistryFullTx(ctx, tx); cerr != nil {
		return MCPImportResult{}, cerr
	} else if full {
		return MCPImportResult{}, ErrMCPServerRegistryFull
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

	if full, cerr := nonBundledMCPServerRegistryFullTx(ctx, tx); cerr != nil {
		return MCPRegisterResult{}, cerr
	} else if full {
		return MCPRegisterResult{}, ErrMCPServerRegistryFull
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

// mcpRowsQuerier is satisfied by both *sql.Tx and *db.DB (via the
// embedded *sql.DB): listMCPServersRows/listMCPServerToolsRows/
// listMCPImportIssuesRows run against whichever one a caller has on hand
// — a plain call outside any transaction (the public ListMCPServers/
// ListMCPServerTools/ListMCPImportIssues methods below), or
// MCPRegistrySnapshot's own single read transaction — sharing one
// implementation of each query rather than two near-identical copies that
// could silently compute the same rows differently.
type mcpRowsQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func listMCPServersRows(ctx context.Context, q mcpRowsQuerier) ([]MCPServerRecord, error) {
	rows, err := q.QueryContext(ctx, `
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

func (r *Repository) ListMCPServers(ctx context.Context) ([]MCPServerRecord, error) {
	return listMCPServersRows(ctx, r.db)
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

// DeleteMCPServer refuses a bundled server, tombstones the name so a
// later mcp.json import cannot silently resurrect it, and removes the row
// — the tier check, the tombstone insert, and the delete itself all
// inside one transaction, so nothing between "read the row to check its
// tier" and "actually delete it" can observe (or need to tolerate) a
// different snapshot of that row. It returns the exact record that was
// deleted, read from inside that same transaction, so a caller (the
// service's DeleteMcpServer, for its post-commit notify/audit) never
// needs a separate pre-read of a row this call is about to remove —
// eliminating the race window a pre-read-then-delete pattern would
// otherwise leave between that read and the transaction that actually
// deletes it. A refusal (not found, bundled) returns the zero-value
// record alongside the named error.
func (r *Repository) DeleteMCPServer(ctx context.Context, serverID string) (MCPServerRecord, error) {
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
		INSERT INTO mcp_import_tombstones (name, deleted_at)
		VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET deleted_at = excluded.deleted_at
	`, server.Name, now()); err != nil {
		return MCPServerRecord{}, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM mcp_servers WHERE id = ?`, serverID)
	if err != nil {
		return MCPServerRecord{}, err
	}
	if err := expectOneRow(result, "MCP server not found"); err != nil {
		return MCPServerRecord{}, err
	}
	if err := tx.Commit(); err != nil {
		return MCPServerRecord{}, err
	}
	return server, nil
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

func listMCPImportIssuesRows(ctx context.Context, q mcpRowsQuerier) ([]MCPImportIssue, error) {
	rows, err := q.QueryContext(ctx, `
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

func (r *Repository) ListMCPImportIssues(ctx context.Context) ([]MCPImportIssue, error) {
	return listMCPImportIssuesRows(ctx, r.db)
}

// MCPServerWithTools pairs a server row with every tool row
// ListMCPServerTools would return for it (present and withdrawn alike),
// captured together by MCPRegistrySnapshot. Tools is empty whenever the
// snapshot's own OverBudget is true (see MCPRegistrySnapshot's own doc
// comment) — never partially populated for some servers and not others.
type MCPServerWithTools struct {
	Server MCPServerRecord
	Tools  []MCPServerTool
}

// MCPRegistrySnapshot is the complete, point-in-time registry state
// ListMcpServers needs to build its response: every server (bundled and
// non-bundled), each paired with its own tools (unless OverBudget — see
// below), and every recorded import issue.
type MCPRegistrySnapshot struct {
	Servers []MCPServerWithTools
	Issues  []MCPImportIssue
	// OverBudget is true when the registry-wide aggregate tool-byte total
	// (see aggregateAllToolBytes/MaxMCPRegistryToolBytes) already exceeds
	// budget at read time. Every tool-reconciliation write path already
	// refuses to create such a state, so this should be unreachable in
	// practice — but a database that somehow already carries one (e.g. a
	// legacy state predating one of those guards, or a future regression
	// that reintroduces an unguarded write path) must not make the
	// registry unmanageable: Servers and Issues are still fully
	// populated (an operator retains enough to identify and delete an
	// offending server), only every server's own Tools is left empty,
	// so a caller (ListMcpServers) never attempts to read, let alone
	// marshal and send, a schema-heavy result sized against an unbounded
	// aggregate.
	OverBudget bool
}

// MCPRegistrySnapshot reads the complete server+tools+issues registry
// state from a single SQLite read transaction, so a caller (ListMcpServers)
// can never observe a mix of before-and-after state for a concurrent tool
// reconciliation (replaceServerToolsTx, UpsertTools) or server
// insert/delete. Before this, ListMcpServers read its aggregate budget
// guard (see below), the server list, each server's own tools, and the
// import issues as separate, independently-acquired queries: nothing held
// the connection across them, so a write commuting between any two of
// those queries could make the guard's decision (computed against an
// earlier state) disagree with the rows actually returned moments later
// (a later, larger state) — the exact "coherent snapshot" gap this method
// closes. The database's single connection (db.Open's SetMaxOpenConns(1))
// is what makes one transaction sufficient to close it: only one *sql.Tx
// can be open at a time, so every read below runs against exactly the
// same, unchanging view a concurrent writer cannot alter until this
// transaction ends (it must wait for the very same connection).
//
// The registry-wide aggregate tool-byte total (see
// aggregateAllToolBytes/MaxMCPRegistryToolBytes, which counts every row,
// present and withdrawn) is checked first, inside this same transaction,
// before any tool row is read: every tool-reconciliation write path
// already refuses to create an aggregate over that budget, so this
// should be unreachable in practice, but a database that somehow already
// carries one is degraded rather than refused outright — see
// MCPRegistrySnapshot's own OverBudget field for exactly what that means
// and why: the registry must stay usable enough to recover from, not
// merely safe. Every server row and every import issue is still read
// (and the whole read still commits as one coherent snapshot); only the
// per-server tool read is skipped while OverBudget, so this never
// attempts to read, let alone build a response sized against, an
// over-budget aggregate of tool schemas specifically.
func (r *Repository) MCPRegistrySnapshot(ctx context.Context) (MCPRegistrySnapshot, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return MCPRegistrySnapshot{}, err
	}
	defer func() { _ = tx.Rollback() }()

	totalBytes, err := aggregateAllToolBytes(ctx, tx)
	if err != nil {
		return MCPRegistrySnapshot{}, err
	}
	overBudget := totalBytes > MaxMCPRegistryToolBytes

	if r.mcpRegistrySnapshotBarrier != nil {
		r.mcpRegistrySnapshotBarrier()
	}

	servers, err := listMCPServersRows(ctx, tx)
	if err != nil {
		return MCPRegistrySnapshot{}, err
	}
	snapshot := MCPRegistrySnapshot{Servers: make([]MCPServerWithTools, 0, len(servers)), OverBudget: overBudget}
	for _, server := range servers {
		var tools []MCPServerTool
		if !overBudget {
			tools, err = listMCPServerToolsRows(ctx, tx, server.ID)
			if err != nil {
				return MCPRegistrySnapshot{}, err
			}
		}
		snapshot.Servers = append(snapshot.Servers, MCPServerWithTools{Server: server, Tools: tools})
	}
	issues, err := listMCPImportIssuesRows(ctx, tx)
	if err != nil {
		return MCPRegistrySnapshot{}, err
	}
	snapshot.Issues = issues

	if err := tx.Commit(); err != nil {
		return MCPRegistrySnapshot{}, err
	}
	return snapshot, nil
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

// MaxMCPRegistryToolBytes bounds the aggregate raw (tool_name +
// schema_json byte length) total across *every* row the `tools` table
// holds for the entire registry — every server combined, present and
// withdrawn (present=0) rows alike, not any one server's own snapshot and
// not only its currently-present tools — enforced transactionally inside
// replaceServerToolsTx: the one function every tool-reconciliation path
// already shares (a live rediscovery via ReplaceMCPServerTools, a static
// mcp.json snapshot via ImportMCPServer, and placeholder adoption's own
// tool withdrawal). Before this, only a single server's own tools were
// bounded (maxMCPToolBytes, 4MiB, enforced in the mcpregistry package
// against both a live tools/list response and a static snapshot) — but
// with up to MaxNonBundledMCPServers (256) non-bundled servers each
// independently allowed a nearly-4MiB snapshot, a single ListMcpServers
// response summing all of them could vastly exceed the 4MiB gRPC message
// cap (maxGRPCMessageSize, in internal/app) that response itself is bound
// by.
//
// Counting every row, not just present=1 ones, is deliberate and load-
// bearing, not an overcount: ListMCPServerTools — and therefore
// ListMcpServers' own per-server descriptor, via toolDescriptor — returns
// every row attributed to a server regardless of its present flag, since a
// withdrawn tool's policy is intentionally preserved (never deleted) so an
// operator's edits survive a tool's temporary disappearance. A withdrawn
// row's tool_name and schema_json are marshaled into the wire response
// exactly like a present one's (only its own Present field differs), so a
// budget that excluded those rows would silently undercount what
// ListMcpServers actually sends: a vendor (or a malicious one) that keeps
// rediscovering under a fresh, disjoint set of tool names every cycle would
// leave every previous cycle's tools behind, forever withdrawn but never
// deleted, and — counted only by present=1 — the aggregate would appear to
// stay flat no matter how large the table, and therefore the response,
// actually grew. Counting every row instead makes that same withdrawn
// history spend the same budget a present tool would, so unbounded growth
// through repeated rediscovery is refused once the real total — exactly
// what a client's response would carry — reaches the cap, the same way a
// single server accumulating too many present tools already was. Preserved
// (never deleted) absent rows are exactly the point: an operator's edited
// policy for a tool that comes and goes must survive that churn, right up
// until the registry-wide total actually fills the budget.
//
// 256KiB is conservative enough that — combined with every other bounded
// field a ListMcpServersResponse carries (up to 256 non-bundled plus a
// handful of bundled server descriptors, each capped at
// maxMCPServerURLBytes=2048 for its own url and 512 bytes for its status
// message, plus up to 256 Unsupported entries at similar per-field caps)
// — the real marshaled response stays comfortably under
// maxGRPCMessageSize even in the worst case. That worst case was measured
// directly (not merely estimated), and the margin below reflects a
// correction: an earlier version of this budget (1MiB) was sized against
// "many minimal tools" — many small, distinct McpToolDescriptor messages,
// which maximizes protobuf's fixed *per-message* framing overhead
// relative to each tool's own tiny payload, and measured to about a 1.55x
// expansion on the wire. That is not the true worst case. A single tool
// whose schema is one large array of minimal JSON scalars — e.g.
// `{"type":"object","x":[0,0,0,...]}` — is worse: each array element
// converts to a google.protobuf.Value carrying a fixed 8-byte double
// (via structpb.NewStruct), which costs roughly 9-11 wire bytes per
// element against as few as 2 raw JSON bytes ("0,"), measuring to about a
// 5.5x expansion — enough that a single tool consuming the *original*
// 1MiB budget alone, in that one adversarial shape, marshaled to about
// 5.5MiB by *itself* — already past the 4MiB cap before any server
// descriptor, Unsupported entry, or any other tool was even added. (A
// full worst-case response built around that same 1MiB-budget tool —
// 259 server descriptors each at their own per-field caps, plus 256
// maximally-sized Unsupported entries — marshaled to about 6.3MiB
// overall.) 256KiB against that same worst-measured (number-array) shape
// marshals a full worst-case
// ListMcpServersResponse (259 server descriptors each at their own
// per-field caps, with the entire tool budget dumped onto one server in
// that shape, plus 256 maximally-sized Unsupported entries) to about
// 2.16MiB — leaving roughly 46% margin under the 4MiB cap, deliberately
// wider than the original (and mistaken) 39% margin given this constant's
// history of a missed worst case. See docs/mcp-security-and-integration.md
// for the fuller accounting and
// TestReplaceServerToolsTxAggregateBudgetExactBoundaryAcrossMultipleServers
// and the mcpregistry package's own worst-case marshal-size test for the
// boundary and wire-size proofs, respectively.
//
// The count is computed with LENGTH(CAST(... AS BLOB)), not plain
// LENGTH(...): SQLite's LENGTH() on a TEXT value counts *characters*, not
// bytes, so a multi-byte UTF-8 tool name or schema would otherwise be
// undercounted relative to Go's own len() (which this same budget check
// also applies to the replacement tools passed in), silently allowing more
// real bytes through the cap than intended.
const MaxMCPRegistryToolBytes = 256 * 1024

// ErrMCPRegistryToolBudgetExceeded is returned by replaceServerToolsTx —
// and therefore by ReplaceMCPServerTools, ImportMCPServer, and
// RegisterMCPServer's placeholder-adoption branch — when accepting a
// server's replacement tool snapshot would push the registry's aggregate
// all-rows tool byte total (see MaxMCPRegistryToolBytes) over budget. It
// is also returned by UpsertTools for the same reason. MCPRegistrySnapshot
// never returns it: an aggregate already over budget at read time is
// instead reported via its own OverBudget field (see MCPRegistrySnapshot's
// doc comment), degraded rather than refused outright, so the registry
// stays usable enough to recover from. The caller's transaction rolls
// back entirely — including the withdrawal update replaceServerToolsTx
// already issued for this server — so a refused reconciliation never
// leaves a server with no tools where it used to have some, nor (for
// ImportMCPServer) a bare row with no tools at all.
var ErrMCPRegistryToolBudgetExceeded = errors.New("MCP registry aggregate tool budget exceeded")

// toolBudgetQueryRow is satisfied by *sql.Tx: aggregateAllToolBytes always
// runs against a live transaction mid-reconciliation (replaceServerToolsTx,
// UpsertTools) or a snapshot read transaction (MCPRegistrySnapshot), never
// against a bare, non-transactional connection — every caller needs the
// count to reflect exactly the rows its own transaction just wrote, or
// exactly the rows its own read transaction is otherwise consistently
// observing.
type toolBudgetQueryRow interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// aggregateAllToolBytes returns the registry-wide total of every tool
// row's raw (tool_name + schema_json) byte length — present and withdrawn
// (present=0) rows alike. This is the one query replaceServerToolsTx,
// UpsertTools, and MCPRegistrySnapshot all need to enforce (or, for the
// last, merely read) MaxMCPRegistryToolBytes, so all three share this
// single implementation rather than three copies that could silently
// drift from computing the same total differently. See
// MaxMCPRegistryToolBytes's own comment for why every row counts, not only
// present=1 ones.
func aggregateAllToolBytes(ctx context.Context, q toolBudgetQueryRow) (int64, error) {
	var total int64
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(LENGTH(CAST(tool_name AS BLOB)) + LENGTH(CAST(schema_json AS BLOB))), 0)
		FROM tools
	`).Scan(&total)
	return total, err
}

// MaxThirdPartyMCPRegistryToolBytes reserves half of the registry-wide
// aggregate (MaxMCPRegistryToolBytes) exclusively for non-bundled
// ("third-party": local-container and remote-url tier) servers,
// enforced — in addition to, and checked before, the unchanged full
// aggregate check — inside replaceServerToolsTx whenever the server
// being reconciled is not bundled. UpsertTools — the bundled/skills/
// legacy path the runtime uses to publish "system", "files", "skills",
// and any other bundled server's own tool capabilities during
// ConnectWorker — is unaffected by this narrower cap and continues to
// enforce only the full aggregate, exactly as before.
//
// Without this, a third-party import or live rediscovery could grow its
// own share of the aggregate arbitrarily close to the full
// MaxMCPRegistryToolBytes cap — replaceServerToolsTx's own prior budget
// check never distinguished a third-party server's bytes from a bundled
// one's — leaving little or no headroom for UpsertTools' own, entirely
// separate, next call to publish (or grow) TuringAgent's own bundled
// tool schemas: a worker connecting after third-party servers had
// already filled the aggregate to (or near) its cap would have its own
// ConnectWorker registration — and therefore every bundled tool the
// runtime depends on ("system", "files", "skills") — refused by the very
// same aggregate guard, through no fault of its own. Reserving half the
// aggregate exclusively for third-party servers guarantees the other
// half is always available for UpsertTools regardless of how many
// third-party servers exist or how large their own tool snapshots are.
//
// 128KiB was not merely assumed: it is measured against the actual
// combined byte total of every tool TuringAgent's own bundled servers
// register today ("system"'s 4 tools, "files"'s 5, and "skills"'s 2 —
// mirrored byte-for-byte real schemas, not estimates). That measurement —
// with a generous safety margin, not just scraping by — is
// internal/service/runtime's own
// TestFirstPartyBundledToolSchemasFitWithinReservedHeadroom; a second,
// separate test in that same package,
// TestConnectWorkerSucceedsWhenThirdPartyToolsFillExactlyTheReservedSubBudget,
// proves the reservation end-to-end at its exact boundary: with the
// third-party sub-budget filled to precisely its own cap, a worker's
// ConnectWorker registration of those same real bundled schemas still
// succeeds, and one more third-party byte on top is refused. See also
// MaxMCPRegistryToolBytes's own comment and
// docs/mcp-security-and-integration.md for the full aggregate's own
// worst-case wire-size accounting, which this narrower cap does not
// change: a third-party server's own tools were always counted in, and
// bounded by, that same full aggregate, and still are — this is a cap
// *within* it, not a replacement for it.
const MaxThirdPartyMCPRegistryToolBytes = MaxMCPRegistryToolBytes / 2

// ErrMCPThirdPartyToolBudgetExceeded is returned by replaceServerToolsTx
// when accepting a non-bundled ("third-party") server's replacement tool
// snapshot would push the *third-party-only* share of the registry's
// aggregate tool byte total (see MaxThirdPartyMCPRegistryToolBytes) over
// its own, narrower budget — distinct from ErrMCPRegistryToolBudgetExceeded,
// which reports the full aggregate (every server, bundled and
// non-bundled combined) exceeding MaxMCPRegistryToolBytes. Checked first
// (see replaceServerToolsTx): a third-party overage is always also,
// trivially, within the still-wider full-aggregate budget, never the
// reverse, so a third-party caller always sees the more specific,
// narrower reason when both would otherwise apply. Like
// ErrMCPRegistryToolBudgetExceeded, the caller's transaction rolls back
// entirely.
var ErrMCPThirdPartyToolBudgetExceeded = errors.New("MCP third-party tool budget exceeded")

// aggregateThirdPartyToolBytes returns the registry-wide total of every
// tool row's raw (tool_name + schema_json) byte length attributed to a
// non-bundled ("third-party") server — present and withdrawn rows
// alike, the same inclusion rule aggregateAllToolBytes applies to the
// full aggregate (see MaxMCPRegistryToolBytes's own comment for why). A
// tool row with mcp_server_id NULL (the "skills" rows UpsertTools
// writes, which are not attributed to any mcp_servers row at all) is
// never a third-party row and is excluded the same way a bundled
// server's own rows are, via the join this subquery requires.
func aggregateThirdPartyToolBytes(ctx context.Context, q toolBudgetQueryRow) (int64, error) {
	var total int64
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(LENGTH(CAST(tool_name AS BLOB)) + LENGTH(CAST(schema_json AS BLOB))), 0)
		FROM tools
		WHERE mcp_server_id IN (
			SELECT id FROM mcp_servers WHERE tier != 'bundled'
		)
	`).Scan(&total)
	return total, err
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
// implementation of it — and, since the aggregate byte budget below is
// enforced here too, identical budget enforcement as well.
//
// The budget is checked *after* the withdrawal and every replacement
// upsert below have already run — not computed beforehand from a
// present-only baseline plus the incoming tools' own Go-side byte count —
// so the one query below measures exactly the table's real resulting
// state, the same state a concurrent ListMcpServers read would see: every
// row, present or withdrawn, counts (see MaxMCPRegistryToolBytes). A
// violation rolls back the whole transaction, undoing the withdrawal and
// every upsert together, via the caller's own deferred tx.Rollback.
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
	// Checked before the full aggregate below, not after: when a
	// non-bundled server's own contribution pushes both the third-party
	// share and the full aggregate over budget at once, the more
	// specific, narrower reason wins — a third-party caller is never
	// told the generic full-aggregate reason when the real, more
	// actionable cause is its own tier's own share of it. server is
	// always non-bundled here in practice — every current caller
	// (ImportMCPServer, RegisterMCPServer's placeholder adoption,
	// ReplaceMCPServerTools) only ever reconciles a non-bundled row's
	// tools this way (see this function's own doc comment); UpsertTools
	// is the bundled/skills path and never calls this function. The
	// explicit tier check is kept anyway, rather than assuming that
	// invariant unconditionally, so a future caller that ever did pass a
	// bundled server through here would not be narrowed by a cap that
	// must only ever apply to third-party ones.
	if server.Tier != MCPServerTierBundled {
		thirdPartyBytes, err := aggregateThirdPartyToolBytes(ctx, tx)
		if err != nil {
			return err
		}
		if thirdPartyBytes > MaxThirdPartyMCPRegistryToolBytes {
			return ErrMCPThirdPartyToolBudgetExceeded
		}
	}
	totalBytes, err := aggregateAllToolBytes(ctx, tx)
	if err != nil {
		return err
	}
	if totalBytes > MaxMCPRegistryToolBytes {
		return ErrMCPRegistryToolBudgetExceeded
	}
	return nil
}

func listMCPServerToolsRows(ctx context.Context, q mcpRowsQuerier, serverID string) ([]MCPServerTool, error) {
	rows, err := q.QueryContext(ctx, `
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

func (r *Repository) ListMCPServerTools(ctx context.Context, serverID string) ([]MCPServerTool, error) {
	return listMCPServerToolsRows(ctx, r.db, serverID)
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

// nonBundledMCPServerRegistryFullTx reports whether the registry already
// holds MaxNonBundledMCPServers non-bundled rows, run inside tx so the
// count it reads and the row insert its caller makes immediately
// afterward (ImportMCPServer/RegisterMCPServer's own fresh-insert
// branches only — never their placeholder-adoption branches, which never
// call this) can never be interleaved by a concurrent transaction: this
// database's single connection (see internal/db, SetMaxOpenConns(1))
// means only one BeginTx can hold the connection at a time, so no other
// transaction's INSERT can land between this SELECT and that INSERT.
func nonBundledMCPServerRegistryFullTx(ctx context.Context, tx *sql.Tx) (bool, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM mcp_servers WHERE tier != ?
	`, string(MCPServerTierBundled)).Scan(&count); err != nil {
		return false, err
	}
	return count >= MaxNonBundledMCPServers, nil
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
