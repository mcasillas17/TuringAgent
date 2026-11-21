package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// The owner's quiet switch for unattended work (TUR-028a), stated once.
//
// unattended_quiet is the switch the owner sets, written compare-and-set on
// its revision. unattended_quiet_drain is the fence that turning it on writes
// in the same transaction: while either is up, no unattended job is claimed,
// no automation fires, and an unattended run that would go back to the queue
// is cancelled instead (releaseJobToPendingTx). The drain cancels the queued
// unattended jobs in bounded batches and clears its key when none is left, so
// turning quiet off never lets work queued before it start.
const (
	unattendedQuietSettingKey      = "unattended_quiet"
	unattendedQuietDrainSettingKey = "unattended_quiet_drain"
)

const (
	quietSettingOn  = "true"
	quietSettingOff = "false"
)

// UnattendedQuiet is the owner's quiet switch as stored.
type UnattendedQuiet struct {
	// Quiet is true while no unattended work starts. A stored value that
	// cannot be read counts as on, so it fails closed.
	Quiet bool
	// Revision is what a write must name. A missing row is revision 0.
	Revision int64
	// Unavailable is true when the stored value cannot be read. Its revision
	// is still readable, so the write that repairs it names it like any other.
	Unavailable bool
}

// ErrUnattendedQuietStale refuses a quiet write that named a revision other
// than the current one. It wrote nothing; the setting it was refused against
// is returned beside it.
var ErrUnattendedQuietStale = errors.New("the quiet setting changed since it was read")

var errUnattendedQuietRevisionExhausted = errors.New("the quiet setting has no next revision")

// GetUnattendedQuiet reads the owner's quiet switch.
func (r *Repository) GetUnattendedQuiet(ctx context.Context) (UnattendedQuiet, error) {
	return readUnattendedQuietTx(ctx, r.db)
}

// SetUnattendedQuiet sets the owner's quiet switch, compare-and-set on the
// revision the caller read, and reports whether it wrote. The revision is
// compared first: a stale one is refused with ErrUnattendedQuietStale, writes
// nothing, and the current setting is returned. With the current revision,
// the value already held writes nothing either. A change writes the next
// revision and a content-free unattended.quiet.changed audit record, and
// turning quiet on also raises the drain, all in one transaction, so a failed
// audit write refuses the change.
func (r *Repository) SetUnattendedQuiet(ctx context.Context, quiet bool, revision int64) (UnattendedQuiet, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return UnattendedQuiet{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	current, err := readUnattendedQuietTx(ctx, tx)
	if err != nil {
		return UnattendedQuiet{}, false, err
	}
	if revision != current.Revision {
		return current, false, ErrUnattendedQuietStale
	}
	if !current.Unavailable && current.Quiet == quiet {
		return current, false, tx.Commit()
	}
	if current.Revision == math.MaxInt64 {
		return current, false, errUnattendedQuietRevisionExhausted
	}
	next := UnattendedQuiet{Quiet: quiet, Revision: current.Revision + 1}
	value := quietSettingOff
	if quiet {
		value = quietSettingOn
	}
	at := now()
	// The write is the compare: it lands only on the revision read above.
	result, err := tx.ExecContext(ctx, `
		INSERT INTO settings (key, value_json, revision, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, revision = excluded.revision,
			updated_at = excluded.updated_at
		WHERE settings.revision = ?
	`, unattendedQuietSettingKey, value, next.Revision, at, current.Revision)
	if err != nil {
		return UnattendedQuiet{}, false, err
	}
	if err := expectOneRowErr(result, ErrUnattendedQuietStale); err != nil {
		return current, false, err
	}
	if quiet {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO settings (key, value_json, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at
		`, unattendedQuietDrainSettingKey, quietSettingOn, at); err != nil {
			return UnattendedQuiet{}, false, err
		}
	}
	payload, err := json.Marshal(map[string]any{"quiet": quiet, "revision": next.Revision})
	if err != nil {
		return UnattendedQuiet{}, false, err
	}
	if err := recordAuditTx(ctx, tx, "", "client", "", "unattended.quiet.changed",
		unattendedQuietSettingKey, string(payload)); err != nil {
		return UnattendedQuiet{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return UnattendedQuiet{}, false, err
	}
	return next, true, nil
}

func readUnattendedQuietTx(ctx context.Context, q rowQuerier) (UnattendedQuiet, error) {
	var value string
	var revision int64
	err := q.QueryRowContext(ctx,
		`SELECT value_json, revision FROM settings WHERE key = ?`, unattendedQuietSettingKey).Scan(&value, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return UnattendedQuiet{}, nil
	}
	if err != nil {
		return UnattendedQuiet{}, err
	}
	switch value {
	case quietSettingOn:
		return UnattendedQuiet{Quiet: true, Revision: revision}, nil
	case quietSettingOff:
		return UnattendedQuiet{Revision: revision}, nil
	default:
		return UnattendedQuiet{Quiet: true, Revision: revision, Unavailable: true}, nil
	}
}

// unattendedWorkFencedTx reports whether new unattended work is fenced: the
// owner's quiet switch is on, or the drain turning it on started has not
// finished. It is one primary-key read of both keys, made inside the
// transaction that would start or return the work, so SQLite serializes it
// with the switch's own transaction. A quiet value that cannot be read, or a
// drain key holding anything at all, fences: it fails closed.
func unattendedWorkFencedTx(ctx context.Context, q rowQuerier) (bool, error) {
	var quiet string
	var draining int
	if err := q.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(CASE WHEN key = ? THEN value_json END), ?),
			COUNT(CASE WHEN key = ? THEN 1 END)
		FROM settings
		WHERE key IN (?, ?)
	`, unattendedQuietSettingKey, quietSettingOff, unattendedQuietDrainSettingKey,
		unattendedQuietSettingKey, unattendedQuietDrainSettingKey).Scan(&quiet, &draining); err != nil {
		return false, err
	}
	return quiet != quietSettingOff || draining > 0, nil
}

// MaxQuietDrainBatch is the most queued unattended jobs one drain transaction
// cancels, so draining a long backlog never holds the single database
// connection for long and attended work is claimed between batches.
const MaxQuietDrainBatch = 100

// quietDrainQuery selects at most a batch of queued unattended jobs' runs. It
// has no order: every one of them is cancelled, and walking the pending jobs
// unsorted lets it stop at its batch rather than sort the whole queue.
const quietDrainQuery = `
	SELECT j.run_id
	FROM jobs j
	JOIN agent_runs r ON r.id = j.run_id
	WHERE j.status = 'pending' AND j.unattended = 1 AND r.status = 'queued'
	LIMIT ?`

// QuietDrainBatch is what one drain transaction did.
type QuietDrainBatch struct {
	// Cancelled are the runs this batch cancelled owner_quiet.
	Cancelled []string
	// Events are every event the cancellations appended.
	Events []Event
	// Done is true when no drain is pending any more: this batch found nothing
	// left and cleared the drain, or none was pending.
	Done bool
}

// DrainUnattendedQuiet runs one batch of the drain turning quiet on began:
// in one transaction it cancels at most MaxQuietDrainBatch queued unattended
// jobs' runs owner_quiet, through the same transition every other
// cancellation takes, and the batch that finds none left clears the drain.
// The drain key is durable and nothing else marks progress, so a drain cut
// short resumes on the next maintenance tick. Nothing unattended is enqueued
// or sent back to the queue while it runs, so it cancels every one.
func (r *Repository) DrainUnattendedQuiet(ctx context.Context) (QuietDrainBatch, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return QuietDrainBatch{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var draining int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM settings WHERE key = ?`, unattendedQuietDrainSettingKey).Scan(&draining); err != nil {
		return QuietDrainBatch{}, err
	}
	if draining == 0 {
		return QuietDrainBatch{Done: true}, tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, quietDrainQuery, MaxQuietDrainBatch)
	if err != nil {
		return QuietDrainBatch{}, err
	}
	var runIDs []string
	for rows.Next() {
		var runID string
		if err := rows.Scan(&runID); err != nil {
			return QuietDrainBatch{}, errors.Join(err, rows.Close())
		}
		runIDs = append(runIDs, runID)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return QuietDrainBatch{}, err
	}
	var batch QuietDrainBatch
	for _, runID := range runIDs {
		result, err := cancelRunTx(ctx, tx, CancelRunInput{
			RunID: runID, Cancellation: runoutcome.OwnerQuietCancellation(), resolveVersionInTx: true,
		})
		if err != nil {
			return QuietDrainBatch{}, err
		}
		batch.Cancelled = append(batch.Cancelled, runID)
		batch.Events = append(batch.Events, result.Events...)
	}
	// PRO-001 extends the batch here: it also deletes at most
	// MaxQuietDrainBatch watch_pending_fires rows, and the drain is finished
	// only when both are empty.
	if len(runIDs) == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, unattendedQuietDrainSettingKey); err != nil {
			return QuietDrainBatch{}, err
		}
		batch.Done = true
	}
	if err := tx.Commit(); err != nil {
		return QuietDrainBatch{}, err
	}
	return batch, nil
}
