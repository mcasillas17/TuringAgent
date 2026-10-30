package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// DelegationWaitingConsent is a task's displayed state while its run waits,
// unclaimable, for the user's consent to tools that leave the machine.
const DelegationWaitingConsent = "waiting_consent"

const (
	egressConsentPending  = "pending"
	egressConsentGranted  = "granted"
	egressConsentDeclined = "declined"
)

// ErrDelegationEgressNotPending is a consent decision for a task that is not
// waiting for one: it needed none, was already decided, or stopped waiting.
var ErrDelegationEgressNotPending = errors.New("the task is not waiting for your consent")

// DelegationEgress is what a task waiting for consent would run with if the
// user allowed it: the tools it already has, the ones it waits on, and the
// skill and memory bindings a decision over that whole set must carry.
type DelegationEgress struct {
	DelegationID   string
	ChildSessionID string
	ChildRunID     string
	Model          string
	// EgressTools are the tools waiting on consent, and SelectedTools those
	// with the job's frozen set, sorted: the set a decision freezes.
	EgressTools   []string
	SelectedTools []string
	// Required says declining ends the task instead of running it without
	// EgressTools.
	Required                  bool
	SkillSnapshotFingerprint  string
	MemorySnapshotFingerprint string
}

// DelegationEgressResult is what a consent decision did: the events to
// publish, and the continuation a declined task's end queued, if it was the
// last of its turn's tasks.
type DelegationEgressResult struct {
	Events            []Event
	ContinuationRunID string
}

// DelegationsWaitingForConsent counts the tasks held for the user's consent:
// queued, their consent still pending. The queue bound is all that ends one
// nobody answers.
func (r *Repository) DelegationsWaitingForConsent(ctx context.Context) (int, error) {
	var waiting int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM delegations d JOIN agent_runs run ON run.id = d.child_run_id
		WHERE d.egress_consent = ? AND run.status = ?`, egressConsentPending, lifecycleQueued).Scan(&waiting)
	return waiting, err
}

// DelegationEgressContext reads a task waiting for consent.
func (r *Repository) DelegationEgressContext(ctx context.Context, delegationID string) (DelegationEgress, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return DelegationEgress{}, err
	}
	defer func() { _ = tx.Rollback() }()
	egress, _, err := pendingDelegationEgressTx(ctx, tx, delegationID)
	if err != nil {
		return DelegationEgress{}, err
	}
	return egress, tx.Commit()
}

// pendingDelegationEgressTx reads a task waiting for consent and its job's
// raw payload. A delegation that does not exist, or whose conversation is
// being deleted, is not found; one that is not waiting is not pending.
func pendingDelegationEgressTx(ctx context.Context, tx *sql.Tx, delegationID string) (DelegationEgress, map[string]json.RawMessage, error) {
	var egress DelegationEgress
	var toolsJSON, consent, status, payloadJSON string
	err := tx.QueryRowContext(ctx, `
		SELECT d.id, d.child_session_id, d.child_run_id, child.model_name,
			d.egress_tools_json, d.egress_consent, d.egress_required, child.status, job.payload_json
		FROM delegations d
		JOIN sessions parent ON parent.id = d.parent_session_id AND parent.deletion_state = 'active'
		JOIN agent_runs child ON child.id = d.child_run_id
		JOIN jobs job ON job.run_id = child.id
		WHERE d.id = ?`, delegationID).Scan(&egress.DelegationID, &egress.ChildSessionID,
		&egress.ChildRunID, &egress.Model, &toolsJSON, &consent, &egress.Required, &status, &payloadJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return DelegationEgress{}, nil, ErrDelegationNotFound
	}
	if err != nil {
		return DelegationEgress{}, nil, err
	}
	if consent != egressConsentPending || status != lifecycleQueued {
		return DelegationEgress{}, nil, ErrDelegationEgressNotPending
	}
	if err := json.Unmarshal([]byte(toolsJSON), &egress.EgressTools); err != nil {
		return DelegationEgress{}, nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payloadJSON), &raw); err != nil {
		return DelegationEgress{}, nil, err
	}
	var payload queuedJobPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return DelegationEgress{}, nil, err
	}
	egress.SelectedTools = append(slices.Clone(payload.SelectedTools), egress.EgressTools...)
	slices.Sort(egress.SelectedTools)
	egress.SelectedTools = slices.Compact(egress.SelectedTools)
	if egress.SkillSnapshotFingerprint, err = skillSnapshotFingerprint(payload.Skills); err != nil {
		return DelegationEgress{}, nil, err
	}
	// A specialist's pinned persona and profile are withheld, so the only
	// thing the selected set changes is whether a memory tool is in it.
	if egress.MemorySnapshotFingerprint, err = backendegress.MemorySnapshotFingerprint(
		MemoryEgressSnapshot{}.Preimage(egress.SelectedTools)); err != nil {
		return DelegationEgress{}, nil, err
	}
	return egress, raw, nil
}

// GrantDelegationEgress records the user's consent for one task. In one
// transaction it reads the task again, refuses a decision that does not
// describe it as it now stands, binds the decision to the task's run, widens
// the job's frozen set to the granted tools, and lets a worker claim it. The
// task's own conversation says where its tool calls may go, and its turn's
// stream is told the task is queued.
func (r *Repository) GrantDelegationEgress(ctx context.Context, delegationID string, pending *PendingEgressDecision) (DelegationEgressResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return DelegationEgressResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	egress, raw, err := pendingDelegationEgressTx(ctx, tx, delegationID)
	if err != nil {
		return DelegationEgressResult{}, err
	}
	decision, err := normalizePendingEgressDecision(pending)
	if err != nil {
		return DelegationEgressResult{}, err
	}
	// A specialist runs on a local model, so only its tools may leave the
	// machine, and only through the destinations the decision names.
	if decision.Provider != backendegress.LocalProvider || decision.Model != egress.Model ||
		decision.RecallApplicable || decision.MemoryProfileApplicable ||
		!slices.Equal(decision.SelectedTools, egress.SelectedTools) {
		return DelegationEgressResult{}, ErrEgressDecisionInvalid
	}
	if decision.SkillSnapshotFingerprint != egress.SkillSnapshotFingerprint {
		return DelegationEgressResult{}, ErrEgressSkillSnapshotChanged
	}
	if decision.MemorySnapshotFingerprint != egress.MemorySnapshotFingerprint {
		return DelegationEgressResult{}, ErrEgressMemorySnapshotChanged
	}
	stored, err := insertRunEgressDecisionTx(ctx, tx, egress.ChildRunID, decision)
	if err != nil {
		return DelegationEgressResult{}, err
	}
	preimage := MemoryEgressSnapshot{}.Preimage(egress.SelectedTools)
	for key, value := range map[string]any{
		"selectedTools":             egress.SelectedTools,
		"requestedTools":            egress.SelectedTools,
		"egressDecision":            stored,
		"pinnedPersona":             pinnedPersonaSnapshot(preimage),
		"pinnedProfile":             pinnedProfileSnapshot(preimage),
		"memorySnapshotFingerprint": egress.MemorySnapshotFingerprint,
	} {
		if raw[key], err = json.Marshal(value); err != nil {
			return DelegationEgressResult{}, err
		}
	}
	payloadJSON, err := json.Marshal(raw)
	if err != nil {
		return DelegationEgressResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET payload_json = ? WHERE run_id = ? AND status = 'pending'`,
		string(payloadJSON), egress.ChildRunID); err != nil {
		return DelegationEgressResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE delegations SET egress_consent = ? WHERE id = ?`,
		egressConsentGranted, delegationID); err != nil {
		return DelegationEgressResult{}, err
	}
	at := now()
	var childTraceID string
	if err := tx.QueryRowContext(ctx, `SELECT trace_id FROM agent_runs WHERE id = ?`, egress.ChildRunID).Scan(&childTraceID); err != nil {
		return DelegationEgressResult{}, err
	}
	// Worded prospectively, like a consented send's: nothing has left the
	// machine yet, and a task that never calls the tool sends nothing.
	notice, err := recordEgressConsentTx(ctx, tx, egress.ChildSessionID, egress.ChildRunID, childTraceID, stored,
		"Tool calls may go to ", "", "Data leaves your machine if this task calls one of those tools", at)
	if err != nil {
		return DelegationEgressResult{}, err
	}
	reported, err := reportDelegationTx(ctx, tx, egress.ChildRunID, at)
	if err != nil {
		return DelegationEgressResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return DelegationEgressResult{}, err
	}
	return DelegationEgressResult{Events: append([]Event{notice}, reported...)}, nil
}

// DeclineDelegationEgress records that the user declined one task's tools. A
// task that requires them is ended without running, which reports and joins
// like any other end; any other task runs without them.
func (r *Repository) DeclineDelegationEgress(ctx context.Context, delegationID string) (DelegationEgressResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return DelegationEgressResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	egress, _, err := pendingDelegationEgressTx(ctx, tx, delegationID)
	if err != nil {
		return DelegationEgressResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE delegations SET egress_consent = ? WHERE id = ?`,
		egressConsentDeclined, delegationID); err != nil {
		return DelegationEgressResult{}, err
	}
	var result DelegationEgressResult
	if egress.Required {
		transition, err := cancelRunTx(ctx, tx, CancelRunInput{
			RunID: egress.ChildRunID, resolveVersionInTx: true,
			Cancellation: runoutcome.EgressConsentDeclinedCancellation(),
		})
		if err != nil {
			return DelegationEgressResult{}, err
		}
		result = DelegationEgressResult{Events: transition.Events, ContinuationRunID: transition.ContinuationRunID}
	} else if result.Events, err = reportDelegationTx(ctx, tx, egress.ChildRunID, now()); err != nil {
		return DelegationEgressResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return DelegationEgressResult{}, err
	}
	return result, nil
}
