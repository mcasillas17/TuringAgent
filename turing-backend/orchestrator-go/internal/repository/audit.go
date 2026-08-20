package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

// maxAuditPayloadReadBytes bounds how much of a stored payload_json a read
// ever returns. payload_json is untrusted stored data — nothing enforces its
// shape or size once it lands in audit_logs — so a reader must not be able to
// make a single row hand back an unbounded amount of it.
const maxAuditPayloadReadBytes = 16 * 1024

// maxAuditMetadataReadBytes bounds how many bytes a read returns for each
// structural metadata column (id, correlation_id, actor_type, actor_id,
// action, target). Like payload_json these columns are untrusted stored data,
// so one hostile row must not be able to make a single read hand back an
// unbounded amount of any of them. The service layer enforces tighter,
// field-specific bounds on top of this.
const maxAuditMetadataReadBytes = 512

// auditPayloadBoundedProjectionSQL is the SQL expression that decides whether
// a row's payload_json is small enough to return. It reads substr(...,
// maxAuditPayloadReadBytes+1) rather than length(CAST(payload_json AS BLOB))
// so a query only ever inspects the first maxAuditPayloadReadBytes+1 bytes of
// the column: the bytes returned are bounded either way, but length()ing the
// whole column made the *scan* cost scale with however large a stored payload
// happened to be, unbounded by this constant.
const auditPayloadBoundedProjectionSQL = `CASE WHEN length(substr(CAST(payload_json AS BLOB), 1, ?)) <= ? THEN payload_json END`

// auditMetadataBoundedProjectionSQL builds the bounded-prefix projection for a
// single structural metadata column. column is always a fixed code constant
// from this file — never client input — so interpolating it is not an injection
// vector. Like the payload projection it reads
// substr(CAST(col AS BLOB), 1, maxAuditMetadataReadBytes+1) rather than
// length()ing the whole column, so the per-row scan cost stays bounded no
// matter how large a stored value is. The length is COALESCEd to 0 because
// substr of a zero-length blob is NULL in SQLite: without it a non-NULL empty
// string would fail the comparison and collapse to NULL, erasing the
// empty-vs-NULL distinction the service relies on. A required column projects
// COALESCE(..., ”) so an over-bound value collapses to an empty string; an
// optional column projects NULL when over-bound while a NULL column stays NULL.
// Both placeholders are bound with maxAuditMetadataReadBytes+1 then
// maxAuditMetadataReadBytes.
func auditMetadataBoundedProjectionSQL(column string, required bool) string {
	bounded := "CASE WHEN COALESCE(length(substr(CAST(" + column + " AS BLOB), 1, ?)), 0) <= ? THEN " + column + " END"
	if required {
		return "COALESCE(" + bounded + ", '')"
	}
	return bounded
}

// auditActionApprovalPrefixSQL classifies, as a 0/1 int, whether a row's
// stored action begins with the literal "approval." — reading only the first 9
// bytes (the length of "approval.") of the action column, never length()ing or
// otherwise scanning the whole value, so this bit costs no more scan than the
// bounded metadata projections beside it. It exists so the service's
// omit-target rule can fail closed for an oversized approval.* action: the
// bounded action projection collapses an over-maxAuditMetadataReadBytes action
// to ”, which would otherwise erase the approval. prefix and leak the target
// (the approval JWT jti). The comparison is NULL-safe via COALESCE(..., 0) so a
// NULL action classifies false, and "approval." is a fixed 9-byte SQL literal,
// never client input, so nothing is interpolated.
const auditActionApprovalPrefixSQL = `COALESCE(substr(CAST(action AS BLOB), 1, 9) = CAST('approval.' AS BLOB), 0)`

// maxAuditRecordsLimit is the repository's hard ceiling on AuditQuery.Limit,
// matching the public service contract's page-size cap. It exists so that
// Limit+1 (the over-fetch used to detect another page) can never overflow:
// the caller-supplied Limit is rejected before it's used in any arithmetic or
// allocation.
const maxAuditRecordsLimit = 100

// AuditOrder selects ListAuditRecords' sort direction. It is a closed Go enum
// rather than a client-supplied string so the SQL direction keyword is always
// chosen from a fixed two-branch switch in this file, never interpolated from
// request text.
type AuditOrder int

const (
	AuditOrderDescending AuditOrder = iota
	AuditOrderAscending
)

var (
	// ErrAuditInvalidOrder reports an AuditOrder outside the enum above. The
	// service layer owns request validation; this is a second, cheaper gate
	// against a caller passing a raw int that isn't one of the two constants.
	ErrAuditInvalidOrder = errors.New("invalid audit order")
	// ErrAuditInvalidLimit reports a Limit that is non-positive or above
	// maxAuditRecordsLimit. The repository refuses to guess a default — the
	// service owns defaults — and refuses to silently clamp an over-large
	// Limit, because Limit+1 (the over-fetch below) must never be computed
	// from an unbounded value: an int as large as math.MaxInt would overflow
	// Limit+1 to a negative number, which then panics as a negative slice
	// capacity.
	ErrAuditInvalidLimit = errors.New("invalid audit limit")
)

// AuditCursor anchors keyset pagination at the last row of a previous page.
// Both fields are required to break ties between rows that share a
// CreatedAt; RowID is SQLite's rowid, which 0009 preserved specifically so
// audit reads could order by it.
type AuditCursor struct {
	CreatedAt string
	RowID     int64
}

// AuditQuery describes one ListAuditRecords read. Every field the client
// could influence is an explicit optional (sql.NullString) so an unset
// filter cannot be confused with an intentional empty-string match.
type AuditQuery struct {
	CorrelationID  sql.NullString
	Action         sql.NullString
	CreatedAtStart sql.NullString // inclusive
	CreatedAtEnd   sql.NullString // exclusive
	Order          AuditOrder
	After          *AuditCursor
	Limit          int
}

// AuditRecord is one bounded audit row. PayloadJSON is invalid whenever the
// stored payload either does not exist or exceeds maxAuditPayloadReadBytes;
// PayloadPresent and PayloadScrubbed are what let a caller tell those two
// "no JSON" cases apart from each other and from an ordinary redaction.
//
// ActionHasApprovalPrefix reports whether the *original* stored action began
// with "approval." — computed in-query from only its first 9 bytes, so it
// survives the Action column being bounded to ” for an oversized action. It
// is an internal disclosure-classification bit, not part of the public read
// contract: the service uses it to keep failing closed on the approval.* target
// (the approval JWT jti) even when the bounded Action can no longer reveal the
// prefix.
type AuditRecord struct {
	RowID                   int64
	AuditID                 string
	CorrelationID           sql.NullString
	ActorType               string
	ActorID                 sql.NullString
	Action                  string
	Target                  sql.NullString
	CreatedAt               string
	PayloadPresent          bool
	PayloadScrubbed         bool
	PayloadJSON             sql.NullString
	ActionHasApprovalPrefix bool
}

func (r *Repository) RecordAudit(ctx context.Context, correlationID string, actorType string, actorID string, action string, target string, payloadJSON string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := recordAuditTx(ctx, tx, correlationID, actorType, actorID, action, target, payloadJSON); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordAuditForExistingRun inserts only while correlationID still names a
// stored run. The existence check and insert share one SQLite statement, so a
// concurrent session deletion either scrubs this row afterward or wins first
// and prevents the row from being recreated.
func (r *Repository) RecordAuditForExistingRun(ctx context.Context, correlationID string, actorType string, actorID string, action string, target string, payloadJSON string) (bool, error) {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO audit_logs (
			id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at
		)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?
		WHERE EXISTS (
			SELECT 1
			FROM agent_runs
			JOIN sessions ON sessions.id = agent_runs.session_id
			WHERE agent_runs.id = ? AND sessions.deletion_state = 'active'
		)
	`,
		ids.New("audit"),
		correlationID,
		actorType,
		nullableText(actorID),
		action,
		nullableText(target),
		nullableText(payloadJSON),
		now(),
		correlationID,
	)
	if err != nil {
		return false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return inserted == 1, nil
}

// recordAuditTx is the transactional half, for callers that must record an
// action atomically with the change it describes — DeleteSession scrubs audit
// and deletes a session in one transaction, and its own audit row has to
// either land with that or not at all.
func recordAuditTx(ctx context.Context, tx *sql.Tx, correlationID string, actorType string, actorID string, action string, target string, payloadJSON string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_logs (id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ids.New("audit"), nullableText(correlationID), actorType, nullableText(actorID), action, nullableText(target), nullableText(payloadJSON), now())
	return err
}

// ListAuditRecords runs the one query behind every audit read: a single
// SELECT over audit_logs, filtered by exact-match correlation/action and a
// start-inclusive/end-exclusive created_at window, ordered by
// (created_at, rowid) in query.Order, and keyset-paginated from query.After.
//
// It fetches at most query.Limit+1 rows so a caller can tell whether another
// page exists without a second round trip; the extra row is not special —
// it's still a full AuditRecord, and dropping it (or not) is the service's
// job, not this method's.
func (r *Repository) ListAuditRecords(ctx context.Context, query AuditQuery) ([]AuditRecord, error) {
	if query.Limit <= 0 || query.Limit > maxAuditRecordsLimit {
		return nil, ErrAuditInvalidLimit
	}

	var direction string
	switch query.Order {
	case AuditOrderDescending:
		direction = "DESC"
	case AuditOrderAscending:
		direction = "ASC"
	default:
		return nil, ErrAuditInvalidOrder
	}

	var b strings.Builder
	args := make([]any, 0, 24)
	// Each structural metadata column is projected through a bounded-prefix
	// CASE so one hostile row cannot make this read hand back an unbounded
	// amount of any column. Required columns (id, actor_type, action) collapse
	// to '' when over-bound; optional columns (correlation_id, actor_id,
	// target) collapse to NULL. Column names are fixed code constants, never
	// input. The created_at, presence, scrubbed, and payload projections are
	// unchanged. auditActionApprovalPrefixSQL is appended last: it reads the
	// original action's first 9 bytes only, so the approval.* disclosure
	// classification survives the bounded action column above collapsing an
	// oversized action to ''. It uses fixed SQL literals (no placeholders), so
	// it does not disturb the placeholder ordering.
	b.WriteString(`SELECT rowid, ` +
		auditMetadataBoundedProjectionSQL("id", true) + `, ` +
		auditMetadataBoundedProjectionSQL("correlation_id", false) + `, ` +
		auditMetadataBoundedProjectionSQL("actor_type", true) + `, ` +
		auditMetadataBoundedProjectionSQL("actor_id", false) + `, ` +
		auditMetadataBoundedProjectionSQL("action", true) + `, ` +
		auditMetadataBoundedProjectionSQL("target", false) + `, created_at,
		payload_json IS NOT NULL,
		COALESCE(payload_json = ?, 0),
		` + auditPayloadBoundedProjectionSQL + `,
		` + auditActionApprovalPrefixSQL + `
		FROM audit_logs WHERE 1 = 1`)
	// The metadata-bound placeholders come first, in the exact column order
	// above, each as (maxAuditMetadataReadBytes+1, maxAuditMetadataReadBytes);
	// then the scrubbed comparison and the payload bound.
	args = append(args,
		maxAuditMetadataReadBytes+1, maxAuditMetadataReadBytes, // id
		maxAuditMetadataReadBytes+1, maxAuditMetadataReadBytes, // correlation_id
		maxAuditMetadataReadBytes+1, maxAuditMetadataReadBytes, // actor_type
		maxAuditMetadataReadBytes+1, maxAuditMetadataReadBytes, // actor_id
		maxAuditMetadataReadBytes+1, maxAuditMetadataReadBytes, // action
		maxAuditMetadataReadBytes+1, maxAuditMetadataReadBytes, // target
		scrubbedAuditPayload,
		maxAuditPayloadReadBytes+1, maxAuditPayloadReadBytes,
	)

	if query.CorrelationID.Valid {
		b.WriteString(" AND correlation_id = ?")
		args = append(args, query.CorrelationID.String)
	}
	if query.Action.Valid {
		b.WriteString(" AND action = ?")
		args = append(args, query.Action.String)
	}
	if query.CreatedAtStart.Valid {
		b.WriteString(" AND created_at >= ?")
		args = append(args, query.CreatedAtStart.String)
	}
	if query.CreatedAtEnd.Valid {
		b.WriteString(" AND created_at < ?")
		args = append(args, query.CreatedAtEnd.String)
	}
	if query.After != nil {
		switch query.Order {
		case AuditOrderDescending:
			b.WriteString(" AND (created_at < ? OR (created_at = ? AND rowid < ?))")
		case AuditOrderAscending:
			b.WriteString(" AND (created_at > ? OR (created_at = ? AND rowid > ?))")
		}
		args = append(args, query.After.CreatedAt, query.After.CreatedAt, query.After.RowID)
	}

	b.WriteString(" ORDER BY created_at " + direction + ", rowid " + direction + " LIMIT ?")
	args = append(args, query.Limit+1)

	rows, err := r.db.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	records := make([]AuditRecord, 0, query.Limit+1)
	for rows.Next() {
		var record AuditRecord
		if err := rows.Scan(
			&record.RowID,
			&record.AuditID,
			&record.CorrelationID,
			&record.ActorType,
			&record.ActorID,
			&record.Action,
			&record.Target,
			&record.CreatedAt,
			&record.PayloadPresent,
			&record.PayloadScrubbed,
			&record.PayloadJSON,
			&record.ActionHasApprovalPrefix,
		); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}
