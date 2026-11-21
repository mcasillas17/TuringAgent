package repository

import (
	"context"
	"errors"
)

// errUnattendedSend refuses a send that asks to be unattended: an unattended
// run is queued only in the transaction that writes its automation_runs row.
var errUnattendedSend = errors.New("an unattended run is queued only beside its automation_runs row")

// RunUnattended is the one definition of unattended work (TUR-027): a run
// nobody asked for when it runs. Today that is a run with an automation_runs
// row, which only a scheduled automation fire writes. PRO-001c adds the other
// half, a run in a session of kind 'watch', when that kind exists.
//
// It is what jobs.unattended is written from, once, at insert, and what the
// enqueue and migration tests compare that flag with. It is not a gate: the
// claim reads the flag, and unattendedGrantCheck and the memory service keep
// their own primary-key probe of automation_runs.
func (r *Repository) RunUnattended(ctx context.Context, runID string) (bool, error) {
	var unattended bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM automation_runs a WHERE a.run_id = r.id)
		FROM agent_runs r
		WHERE r.id = ?`, runID).Scan(&unattended)
	return unattended, err
}
