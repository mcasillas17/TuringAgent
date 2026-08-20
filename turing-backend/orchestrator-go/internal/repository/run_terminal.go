package repository

import (
	"context"
	"database/sql"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// This file owns terminalization. Completion, failure, and cancellation share
// the guarded transition core but not their identity requirements, so each has
// its own wrapper: only a success report carries content to persist, only a
// failure carries a normalized failure, and only a cancellation may claim the
// run was abandoned.

// CompleteRunInput is an explicit successful terminal report.
//
// Content is persisted exactly as given. Empty or whitespace-only content is a
// legitimate success — it commits completed/completed_no_content rather than
// being rewritten into apologetic filler — so there is no "is this good enough"
// check anywhere below.
type CompleteRunInput struct {
	RunID                string
	AssistantMessageID   string
	Content              string
	ExpectedStateVersion int64
	Usage                *RunTokenUsage

	resolveVersionInTx bool
}

// FailRunInput is a normalized terminal failure. The failure is a typed value
// rather than a code and a sentence, because the sentence was where provider
// and tool text used to reach public responses.
type FailRunInput struct {
	RunID                string
	AssistantMessageID   string
	ExpectedStateVersion int64
	Failure              runoutcome.Failure
	// PreserveExecution keeps an unacknowledged worker's execution containment
	// intact while the run terminalizes, so reconciliation still fences it.
	PreserveExecution bool

	// leaveExecutionUntouched writes no execution column at all. The approval
	// terminalization path needs it: a worker that is still holding an
	// authorization has not exited, and marking it exited would release the
	// session's capacity to the next run while the old one is still inside a
	// tool call. Unexported, because it is the narrow exception rather than an
	// option a caller should weigh.
	leaveExecutionUntouched bool

	// allowedFrom narrows the source lifecycles for a writer that owns a
	// narrower rule than "any nonterminal run". The approval paths use it: an
	// approval decision may only terminalize the run that decision belongs to,
	// in the phase it was made for. Unexported, so no caller outside this
	// package can widen its own guard.
	allowedFrom []string
	// resolveVersionInTx marks a repository-internal writer that is already
	// inside the transaction it is guarding — recovery, retry exhaustion, and
	// approval terminalization all decide to terminalize from the same row they
	// are about to update. They resolve the expectation under the guard rather
	// than reading a version first and racing themselves. Unexported for the
	// same reason raw is: no caller outside this package can skip carrying an
	// expected version.
	resolveVersionInTx bool
}

// CancelRunInput is a normalized terminal cancellation.
type CancelRunInput struct {
	RunID                string
	AssistantMessageID   string
	ExpectedStateVersion int64
	Cancellation         runoutcome.Cancellation

	resolveVersionInTx bool
}

// terminalExpectation resolves how a terminal command names the version it
// expects.
//
// transactionLocal comes from the unexported input fields only, never from the
// expectation itself: a public caller supplying zero is refused here rather
// than being promoted onto the resolve-in-transaction path.
func terminalExpectation(expected int64, transactionLocal bool) (int64, error) {
	if transactionLocal {
		return unresolvedStateVersion, nil
	}
	if expected < 1 {
		return 0, ErrRunStateVersionInvalid
	}
	return expected, nil
}

// terminalContentIdentity decides the content a terminal transition commits.
//
// A success report owns the bytes and persists them. A failure or cancellation
// owns nothing: whatever the run already produced stays exactly as it is, and
// its identity is read from the row rather than recomputed from a report that
// never carried content.
func terminalContentIdentity(row runRow, assistantMessageID string, content string, write bool) runTerminalContent {
	identity := runTerminalContent{assistantMessageID: assistantMessageID}
	if write && assistantMessageID != "" {
		identity.content = content
		identity.write = true
		identity.sha256 = runoutcome.ContentSHA256(content)
		identity.hasDisplayable = runoutcome.HasDisplayableContent(content)
		return identity
	}
	// Both halves of the identity are read from the same bytes. Taking the hash
	// from the stored column while computing displayability from the message
	// would let a terminal outcome describe content the message does not hold.
	identity.sha256 = runoutcome.ContentSHA256(row.messageContent)
	identity.hasDisplayable = runoutcome.HasDisplayableContent(row.messageContent)
	return identity
}

// CompleteRunCanonical commits an explicit successful terminal report.
func (r *Repository) CompleteRunCanonical(ctx context.Context, input CompleteRunInput) (RunTransitionResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RunTransitionResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := completeRunTx(ctx, tx, input)
	if err != nil {
		return RunTransitionResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return RunTransitionResult{}, err
	}
	return result, nil
}

func completeRunTx(ctx context.Context, tx *sql.Tx, input CompleteRunInput) (RunTransitionResult, error) {
	transactionLocal := input.resolveVersionInTx
	expected, err := terminalExpectation(input.ExpectedStateVersion, transactionLocal)
	if err != nil {
		return RunTransitionResult{}, err
	}
	row, err := readRunRow(ctx, tx, input.RunID)
	if err != nil {
		return RunTransitionResult{}, err
	}
	content := terminalContentIdentity(row, input.AssistantMessageID, input.Content, true)
	reason := runoutcome.ReasonNone
	if !content.hasDisplayable {
		reason = runoutcome.ReasonCompletedNoContent
	}
	var inputTokens, outputTokens sql.NullInt64
	if input.Usage != nil {
		inputTokens = nonNegativeNullInt64(input.Usage.InputTokens)
		outputTokens = nonNegativeNullInt64(input.Usage.OutputTokens)
	}
	payload := map[string]any{"runId": input.RunID}
	if input.AssistantMessageID != "" {
		payload["assistantMessageId"] = input.AssistantMessageID
	}
	transition := runTransition{
		runID:            input.RunID,
		expectedVersion:  expected,
		transactionLocal: transactionLocal,
		allowedFrom:      []string{lifecycleRunning, lifecycleWaitingApproval, lifecycleRecovering},
		to:               lifecycleCompleted,
		reason:           reason,
		terminal:         &content,
		rejection:        ErrRunNotCompletable,
		extraSet: `input_tokens = ?,
			output_tokens = ?,
			execution_active = 0,
			execution_exit_acknowledged_at = COALESCE(execution_exit_acknowledged_at, ?),
			execution_state = 'exited',
			execution_lease_expires_at = NULL,
			execution_lease_expires_at_ns = NULL`,
		eventType:    "agent.run.completed",
		eventPayload: payload,
	}
	transition.extraArgs = []any{inputTokens, outputTokens, transitionTime}
	result, err := applyRunTransitionTx(ctx, tx, transition, func(ctx context.Context, state RunState, at string) ([]Event, error) {
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'completed', finished_at = ?, lease_owner = NULL, lease_expires_at = NULL, lease_expires_at_ns = NULL WHERE run_id = ? AND status IN ('pending','in_progress')`, at, input.RunID); err != nil {
			return nil, err
		}
		// A tool call still open when the run reports success did not succeed;
		// it was cut off. The category says that without inventing a story
		// about why.
		events, err := failPendingApprovalLifecycleTx(ctx, tx, input.RunID, runoutcome.ReasonToolFailure, "tool_call_failed", at)
		if err != nil {
			return nil, err
		}
		if input.AssistantMessageID != "" && state.HasDisplayableContent {
			messageEvent, err := appendRunMessageCompletedTx(ctx, tx, row, input.AssistantMessageID, input.Content, at)
			if err != nil {
				return nil, err
			}
			events = append(events, messageEvent)
		}
		return events, nil
	})
	if err != nil {
		return RunTransitionResult{}, err
	}
	return result, nil
}

func appendRunMessageCompletedTx(ctx context.Context, tx *sql.Tx, row runRow, assistantMessageID string, content string, at string) (Event, error) {
	payloadJSON, err := marshalEventPayload(map[string]any{
		"messageId": assistantMessageID,
		"content":   content,
	})
	if err != nil {
		return Event{}, err
	}
	return appendRunEventTx(ctx, tx, row.sessionID, row.runID, row.traceID, "message.completed", payloadJSON, at)
}

// FailRunCanonical commits a normalized terminal failure.
func (r *Repository) FailRunCanonical(ctx context.Context, input FailRunInput) (RunTransitionResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RunTransitionResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := failRunTx(ctx, tx, input)
	if err != nil {
		return RunTransitionResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return RunTransitionResult{}, err
	}
	return result, nil
}

func failRunTx(ctx context.Context, tx *sql.Tx, input FailRunInput) (RunTransitionResult, error) {
	transactionLocal := input.resolveVersionInTx
	expected, err := terminalExpectation(input.ExpectedStateVersion, transactionLocal)
	if err != nil {
		return RunTransitionResult{}, err
	}
	row, err := readRunRow(ctx, tx, input.RunID)
	if err != nil {
		return RunTransitionResult{}, err
	}
	content := terminalContentIdentity(row, input.AssistantMessageID, "", false)
	// The normalized code is the only diagnostic that survives. error_message
	// stays NULL for every failure this package writes: it was the column a
	// provider's or a tool's sentence used to reach a client through.
	code := input.Failure.Code()
	extraSet := `error_code = ?,
			error_message = ?,
			execution_active = 0,
			execution_exit_acknowledged_at = COALESCE(execution_exit_acknowledged_at, ?),
			execution_state = 'exited',
			execution_lease_expires_at = NULL,
			execution_lease_expires_at_ns = NULL`
	extraArgs := []any{code, nullableText(""), transitionTime}
	if input.leaveExecutionUntouched {
		extraSet = `error_code = ?, error_message = ?`
		extraArgs = []any{code, nullableText("")}
	} else if input.PreserveExecution {
		extraSet = `error_code = ?,
			error_message = ?,
			execution_exit_acknowledged_at = CASE
				WHEN execution_active = 1 THEN execution_exit_acknowledged_at
				ELSE COALESCE(execution_exit_acknowledged_at, ?)
			END,
			execution_state = CASE WHEN execution_active = 1 THEN 'uncertain' ELSE 'exited' END,
			execution_lease_expires_at = CASE WHEN execution_active = 1 THEN execution_lease_expires_at ELSE NULL END,
			execution_lease_expires_at_ns = CASE WHEN execution_active = 1 THEN execution_lease_expires_at_ns ELSE NULL END`
	}
	allowedFrom := input.allowedFrom
	if len(allowedFrom) == 0 {
		allowedFrom = []string{lifecycleQueued, lifecycleRunning, lifecycleWaitingApproval, lifecycleRecovering}
	}
	result, err := applyRunTransitionTx(ctx, tx, runTransition{
		runID:            input.RunID,
		expectedVersion:  expected,
		transactionLocal: transactionLocal,
		allowedFrom:      allowedFrom,
		to:               lifecycleFailed,
		reason:           input.Failure.Reason(),
		terminal:         &content,
		rejection:        ErrRunNotFailable,
		extraSet:         extraSet,
		extraArgs:        extraArgs,
		eventType:        "agent.run.failed",
		eventPayload:     terminalFailurePayload(input.RunID, input.Failure),
	}, func(ctx context.Context, state RunState, at string) ([]Event, error) {
		events, err := failPendingApprovalLifecycleTx(ctx, tx, input.RunID, input.Failure.Reason(), code, at)
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'failed', finished_at = ?, error_code = ?, error_message = NULL, lease_owner = NULL, lease_expires_at = NULL, lease_expires_at_ns = NULL WHERE run_id = ? AND status IN ('pending','in_progress')`, at, code, input.RunID); err != nil {
			return nil, err
		}
		return events, nil
	})
	if err != nil {
		return RunTransitionResult{}, err
	}
	return result, nil
}

// terminalFailurePayload builds the public failure projection: the normalized
// code and the run state the repository merges into every lifecycle event.
// There is no message key, because there is no message to put in it.
func terminalFailurePayload(runID string, failure runoutcome.Failure) map[string]any {
	return map[string]any{
		"runId": runID,
		"code":  failure.Code(),
		// Deprecated and always false: whether the system retries is internal
		// dispatch policy, never a promise to the user that repeating is safe.
		"retryable": false,
	}
}

// CancelRunCanonical commits a normalized terminal cancellation.
//
// Cancellation deliberately leaves execution containment alone. A cancelled run
// whose worker has not acknowledged exit is still in flight, and reconciliation
// has to keep seeing it.
func (r *Repository) CancelRunCanonical(ctx context.Context, input CancelRunInput) (RunTransitionResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RunTransitionResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := cancelRunTx(ctx, tx, input)
	if err != nil {
		return RunTransitionResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return RunTransitionResult{}, err
	}
	return result, nil
}

func cancelRunTx(ctx context.Context, tx *sql.Tx, input CancelRunInput) (RunTransitionResult, error) {
	transactionLocal := input.resolveVersionInTx
	expected, err := terminalExpectation(input.ExpectedStateVersion, transactionLocal)
	if err != nil {
		return RunTransitionResult{}, err
	}
	row, err := readRunRow(ctx, tx, input.RunID)
	if err != nil {
		return RunTransitionResult{}, err
	}
	content := terminalContentIdentity(row, input.AssistantMessageID, "", false)
	storedReason := input.Cancellation.Code()
	payload := map[string]any{"runId": input.RunID, "reason": string(input.Cancellation.Reason())}
	result, err := applyRunTransitionTx(ctx, tx, runTransition{
		runID:            input.RunID,
		expectedVersion:  expected,
		transactionLocal: transactionLocal,
		allowedFrom:      []string{lifecycleQueued, lifecycleRunning, lifecycleWaitingApproval, lifecycleRecovering},
		to:               lifecycleCancelled,
		reason:           input.Cancellation.Reason(),
		terminal:         &content,
		rejection:        ErrRunNotCancellable,
		extraSet:         `cancellation_reason = ?`,
		extraArgs:        []any{storedReason},
		eventType:        "agent.run.cancelled",
		eventPayload:     payload,
	}, func(ctx context.Context, state RunState, at string) ([]Event, error) {
		events, err := failPendingApprovalLifecycleTx(ctx, tx, input.RunID, input.Cancellation.Reason(), "cancelled", at)
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'cancelled', finished_at = ?, error_code = 'cancelled', error_message = ?, lease_owner = NULL, lease_expires_at = NULL, lease_expires_at_ns = NULL WHERE run_id = ? AND status IN ('pending','in_progress')`, at, nullableText(storedReason), input.RunID); err != nil {
			return nil, err
		}
		return events, nil
	})
	if err != nil {
		return RunTransitionResult{}, err
	}
	return result, nil
}
