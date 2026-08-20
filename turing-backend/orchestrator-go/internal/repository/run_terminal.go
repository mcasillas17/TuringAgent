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
	raw                *rawTerminalReport
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

	raw *rawTerminalReport
}

// CancelRunInput is a normalized terminal cancellation.
type CancelRunInput struct {
	RunID                string
	AssistantMessageID   string
	ExpectedStateVersion int64
	Cancellation         runoutcome.Cancellation

	resolveVersionInTx bool
	raw                *rawTerminalReport
}

// rawTerminalReport is the temporary raw ingestion the seven compatibility
// adapters carry, and the only way to reach a transition without an expected
// version.
//
// Its fields are unexported, so it can only be built inside this package: no
// caller outside the repository can construct one, and no new caller inside it
// should. It exists so ChatService, RuntimeService, and their tests keep
// compiling across this commit while their typed-ingestion tests are still
// waiting to be written. It is removed with the adapters.
type rawTerminalReport struct {
	code        string
	message     string
	payloadJSON string
	// appendEvent distinguishes the bare methods, which historically wrote no
	// terminal event, from the WithEvent variants that returned one. Both now
	// append exactly one terminal event, because a lifecycle change with no
	// projection is the bug this task exists to remove; the flag only decides
	// whether the caller is handed the events back.
	returnEvents bool
}

// terminalExpectation resolves how a terminal command names the version it
// expects. A raw adapter has none to give, so it resolves the row's own
// version inside the guarded transaction rather than reading it first and
// racing itself.
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
	identity.sha256 = row.contentSHA256
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
	transactionLocal := input.raw != nil || input.resolveVersionInTx
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
	if input.raw != nil {
		// A caller that has not been converted yet owns its own payload, and
		// that untyped shape is exactly what the next task's tests have to be
		// able to observe. The canonical state is merged into it either way.
		payload, err = rawPayloadMap(input.raw.payloadJSON)
		if err != nil {
			return RunTransitionResult{}, err
		}
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
	return applyRawTerminalCompatibility(ctx, tx, input.raw, input.RunID, result)
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
	transactionLocal := input.raw != nil || input.resolveVersionInTx
	expected, err := terminalExpectation(input.ExpectedStateVersion, transactionLocal)
	if err != nil {
		return RunTransitionResult{}, err
	}
	row, err := readRunRow(ctx, tx, input.RunID)
	if err != nil {
		return RunTransitionResult{}, err
	}
	content := terminalContentIdentity(row, input.AssistantMessageID, "", false)
	code, message := input.Failure.Code(), ""
	if input.raw != nil {
		code, message = input.raw.code, input.raw.message
	}
	extraSet := `error_code = ?,
			error_message = ?,
			execution_active = 0,
			execution_exit_acknowledged_at = COALESCE(execution_exit_acknowledged_at, ?),
			execution_state = 'exited',
			execution_lease_expires_at = NULL,
			execution_lease_expires_at_ns = NULL`
	extraArgs := []any{code, nullableText(message), transitionTime}
	if input.leaveExecutionUntouched {
		extraSet = `error_code = ?, error_message = ?`
		extraArgs = []any{code, nullableText(message)}
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
	failurePayload, err := terminalFailurePayload(input.RunID, input.Failure, input.raw)
	if err != nil {
		return RunTransitionResult{}, err
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
		eventPayload:     failurePayload,
	}, func(ctx context.Context, state RunState, at string) ([]Event, error) {
		events, err := failPendingApprovalLifecycleTx(ctx, tx, input.RunID, input.Failure.Reason(), code, at)
		if err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'failed', finished_at = ?, error_code = ?, error_message = ?, lease_owner = NULL, lease_expires_at = NULL, lease_expires_at_ns = NULL WHERE run_id = ? AND status IN ('pending','in_progress')`, at, code, nullableText(message), input.RunID); err != nil {
			return nil, err
		}
		return events, nil
	})
	if err != nil {
		return RunTransitionResult{}, err
	}
	return applyRawTerminalCompatibility(ctx, tx, input.raw, input.RunID, result)
}

// terminalFailurePayload builds the public failure projection. The canonical
// path publishes the normalized code and the run state; the raw adapters
// publish exactly the payload their caller passed, because their untyped
// ingestion is what the next task's tests have to be able to see.
func terminalFailurePayload(runID string, failure runoutcome.Failure, raw *rawTerminalReport) (map[string]any, error) {
	if raw != nil {
		return rawPayloadMap(raw.payloadJSON)
	}
	return map[string]any{
		"runId": runID,
		"code":  failure.Code(),
		// Deprecated and always false: whether the system retries is internal
		// dispatch policy, never a promise to the user that repeating is safe.
		"retryable": false,
	}, nil
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
	transactionLocal := input.raw != nil || input.resolveVersionInTx
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
	if input.raw != nil {
		storedReason = input.raw.message
		payload, err = rawPayloadMap(input.raw.payloadJSON)
		if err != nil {
			return RunTransitionResult{}, err
		}
	}
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
	return applyRawTerminalCompatibility(ctx, tx, input.raw, input.RunID, result)
}

// applyRawTerminalCompatibility hands a raw adapter's caller the event list it
// used to get. A bare method historically returned none, and its caller
// therefore publishes none; the event is still durable, so a reopened session
// sees the terminal state either way.
func applyRawTerminalCompatibility(
	ctx context.Context,
	tx *sql.Tx,
	raw *rawTerminalReport,
	runID string,
	result RunTransitionResult,
) (RunTransitionResult, error) {
	if raw == nil || raw.returnEvents {
		return result, nil
	}
	result.Events = nil
	return result, nil
}

// ---------------------------------------------------------------------------
// Temporary raw-signature adapters. All seven are removed in the task that
// converts ChatService and RuntimeService to typed reports; no new caller may
// use them. Each one delegates the terminal mutation to the canonical writer
// above, and none of them carries an expected version, so each resolves the
// row's own version inside the canonical writer's transaction.
// ---------------------------------------------------------------------------

// CompleteRun is a temporary raw adapter. Use CompleteRunCanonical.
func (r *Repository) CompleteRun(ctx context.Context, runID string, assistantMessageID string, content string) error {
	_, err := r.CompleteRunCanonical(ctx, CompleteRunInput{
		RunID:              runID,
		AssistantMessageID: assistantMessageID,
		Content:            content,
		raw:                &rawTerminalReport{},
	})
	return err
}

// CompleteRunWithEvent is a temporary raw adapter. Use CompleteRunCanonical.
func (r *Repository) CompleteRunWithEvent(ctx context.Context, runID string, assistantMessageID string, content string, payloadJSON string, usage *RunTokenUsage) ([]Event, error) {
	result, err := r.CompleteRunCanonical(ctx, CompleteRunInput{
		RunID:              runID,
		AssistantMessageID: assistantMessageID,
		Content:            content,
		Usage:              usage,
		raw:                &rawTerminalReport{payloadJSON: payloadJSON, returnEvents: true},
	})
	if err != nil {
		return nil, err
	}
	return result.Events, nil
}

// FailRun is a temporary raw adapter. Use FailRunCanonical.
func (r *Repository) FailRun(ctx context.Context, runID string, code string, message string) error {
	_, err := r.FailRunCanonical(ctx, FailRunInput{
		RunID:   runID,
		Failure: rawReportedFailure(code),
		raw:     &rawTerminalReport{code: code, message: message},
	})
	return err
}

// FailRunWithEvent is a temporary raw adapter. Use FailRunCanonical.
func (r *Repository) FailRunWithEvent(ctx context.Context, runID string, code string, message string, payloadJSON string) ([]Event, error) {
	return r.failRunRaw(ctx, runID, code, message, payloadJSON, false)
}

// FailRunWithEventPreservingExecution is a temporary raw adapter. Use
// FailRunCanonical with PreserveExecution.
func (r *Repository) FailRunWithEventPreservingExecution(ctx context.Context, runID string, code string, message string, payloadJSON string) ([]Event, error) {
	return r.failRunRaw(ctx, runID, code, message, payloadJSON, true)
}

func (r *Repository) failRunRaw(ctx context.Context, runID string, code string, message string, payloadJSON string, preserveExecution bool) ([]Event, error) {
	result, err := r.FailRunCanonical(ctx, FailRunInput{
		RunID:             runID,
		Failure:           rawReportedFailure(code),
		PreserveExecution: preserveExecution,
		raw:               &rawTerminalReport{code: code, message: message, payloadJSON: payloadJSON, returnEvents: true},
	})
	if err != nil {
		return nil, err
	}
	return result.Events, nil
}

// CancelRun is a temporary raw adapter. Use CancelRunCanonical.
func (r *Repository) CancelRun(ctx context.Context, runID string, reason string) error {
	_, err := r.CancelRunCanonical(ctx, CancelRunInput{
		RunID:        runID,
		Cancellation: runoutcome.AbandonedCancellation(),
		raw:          &rawTerminalReport{message: reason},
	})
	return err
}

// CancelRunWithEvent is a temporary raw adapter. Use CancelRunCanonical.
func (r *Repository) CancelRunWithEvent(ctx context.Context, runID string, reason string, payloadJSON string) ([]Event, error) {
	result, err := r.CancelRunCanonical(ctx, CancelRunInput{
		RunID:        runID,
		Cancellation: runoutcome.AbandonedCancellation(),
		raw:          &rawTerminalReport{message: reason, payloadJSON: payloadJSON, returnEvents: true},
	})
	if err != nil {
		return nil, err
	}
	return result.Events, nil
}

// rawReportedFailure normalizes a bare code from a caller that has not been
// converted yet. It has no origin to offer, so it reports the orchestrator
// itself and lets the normalizer decide; an unrecognized pair fails closed to
// an internal failure rather than guessing a provider or tool story.
func rawReportedFailure(code string) runoutcome.Failure {
	for _, origin := range []runoutcome.Origin{
		runoutcome.OriginDispatch,
		runoutcome.OriginRecovery,
		runoutcome.OriginProviderTransport,
		runoutcome.OriginProviderProtocol,
		runoutcome.OriginProviderConfiguration,
		runoutcome.OriginProviderOutputGuard,
		runoutcome.OriginExternalProvider,
		runoutcome.OriginToolExecution,
		runoutcome.OriginToolGuard,
		runoutcome.OriginToolInfrastructure,
		runoutcome.OriginToolPolicy,
		runoutcome.OriginApprovalTransport,
		runoutcome.OriginApprovalExpiry,
		runoutcome.OriginAutomationPolicy,
		runoutcome.OriginContextAssembly,
		runoutcome.OriginWorkerRuntime,
		runoutcome.OriginClientLifecycle,
	} {
		failure := runoutcome.NormalizeFailure(origin, code, runoutcome.RetryClassNever)
		if failure.Code() == code && failure.Reason() != runoutcome.ReasonNone && failure.Reason() != runoutcome.ReasonAbandoned {
			return failure
		}
	}
	return runoutcome.NormalizeFailure(runoutcome.OriginOrchestratorInternal, "", runoutcome.RetryClassNever)
}
