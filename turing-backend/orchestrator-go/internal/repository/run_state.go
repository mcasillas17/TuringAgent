package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/persisttime"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runcorrelation"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// The durable public lifecycle vocabulary. They are named constants because
// they appear in guarded SQL predicates, in the allowed-transition tables
// below, and in event projections; a typo in any one of those places would
// silently widen or narrow a guard.
const (
	lifecycleQueued          = "queued"
	lifecycleRunning         = "running"
	lifecycleWaitingApproval = "waiting_approval"
	lifecycleRecovering      = "recovering"
	lifecycleCompleted       = "completed"
	lifecycleFailed          = "failed"
	lifecycleCancelled       = "cancelled"
)

// runStateChangedEventType is the projection a transition uses when it has no
// lifecycle event of its own. Entering recovery is the case that matters: it
// used to be invisible, so a reopened session showed a run as running while
// nobody owned it.
const runStateChangedEventType = "agent.run.state_changed"

// approvalIdentityPayloadKey names the approval whose authorization triggered a
// lifecycle event. It is already the key every approval event publishes, and it
// is an ID a client is given anyway — the approval it was asked to decide — so
// carrying it on the resume's own projection tells a watcher which of a run's
// outstanding authorizations was acted on without widening what a public
// payload may contain.
//
// The worker and the assignment attempt that also identify a resume stay out of
// it. They are row and dispatch guards, and no client has any business knowing
// which process is executing a run.
const approvalIdentityPayloadKey = "approvalId"

// approvalIdentityPayloadPath is the same key as a JSON path, bound as a query
// argument rather than concatenated into SQL.
const approvalIdentityPayloadPath = "$." + approvalIdentityPayloadKey

// runStateVersionPayloadPath locates the committed version inside the canonical
// snapshot every lifecycle event carries, which is what identifies the single
// event a given transition wrote.
const runStateVersionPayloadPath = "$.runState.stateVersion"

// unresolvedStateVersion marks a transaction-local transition whose caller
// carries no expected version. It is never a stored or public value — zero is
// protobuf absence — and it is only reachable through the ...InTx constructors
// below, which resolve the real expectation from the row they are about to
// guard, inside the same transaction as the guarded update. No exported input
// reaches it, so a public caller that omits a version is refused rather than
// silently promoted to an unguarded write.
const unresolvedStateVersion int64 = 0

var (
	// ErrRunStateVersionExhausted reports that a run has no representable next
	// version. The message is value-free on purpose: it is returned to callers
	// and logged, and a version number is a row value.
	ErrRunStateVersionExhausted = errors.New("run state version exhausted")
	// ErrRunStateVersionInvalid rejects a version outside the stored range.
	// Zero is the protobuf absence value and can never be a real expectation.
	ErrRunStateVersionInvalid = errors.New("invalid run state version")
	// ErrRunTransitionConflict reports that a command lost to another writer:
	// the row is not where the command expected it, or an otherwise-matching
	// repeat carries a different identity. It never says which, because the
	// difference is row content.
	ErrRunTransitionConflict = errors.New("run transition conflict")
	// ErrRunTransitionUnsupported rejects a transition this state machine does
	// not define — an unknown target lifecycle, or an outcome reason the
	// normative matrix does not allow for it.
	ErrRunTransitionUnsupported = errors.New("unsupported run transition")
)

// RunState is the canonical durable answer to "what happened to this run".
//
// ContentSHA256 is internal identity for duplicate terminal reports. It is
// never projected into a public payload or protobuf message; the exported
// field exists so writers in this package can compare it, not so readers can
// publish it.
type RunState struct {
	RunID                 string
	UserMessageID         string
	AssistantMessageID    string
	Lifecycle             string
	OutcomeReason         string
	StateVersion          int64
	StateUpdatedAt        string
	FinishedAt            sql.NullString
	HasDisplayableContent bool
	ContentSHA256         string
}

// RunTransitionResult is what a guarded transition committed. Duplicate marks
// the write-free replay path: the state is real and current, but this call did
// not produce it and appended no events.
type RunTransitionResult struct {
	State     RunState
	Events    []Event
	Duplicate bool
}

// allowedOutcomeReasons is the normative lifecycle/reason matrix. A nonterminal
// lifecycle carries no outcome, a completed run carries either nothing to say
// or the fact that it produced no displayable content, and each terminal
// failure reason has to be one this product can actually justify.
var allowedOutcomeReasons = map[string][]runoutcome.Reason{
	lifecycleQueued:          {runoutcome.ReasonNone},
	lifecycleRunning:         {runoutcome.ReasonNone},
	lifecycleWaitingApproval: {runoutcome.ReasonNone},
	lifecycleRecovering:      {runoutcome.ReasonNone},
	lifecycleCompleted:       {runoutcome.ReasonNone, runoutcome.ReasonCompletedNoContent},
	lifecycleFailed: {
		runoutcome.ReasonExpired,
		runoutcome.ReasonContextLimit,
		runoutcome.ReasonProviderFailure,
		runoutcome.ReasonToolFailure,
		runoutcome.ReasonPolicyDenied,
		runoutcome.ReasonRetriesExhausted,
		runoutcome.ReasonRecoveryInterrupted,
		runoutcome.ReasonSideEffectUncertain,
		runoutcome.ReasonApprovalDeliveryFailed,
		runoutcome.ReasonInternalFailure,
	},
	lifecycleCancelled: {runoutcome.ReasonUserCancelled, runoutcome.ReasonAbandoned},
}

func isTerminalLifecycle(lifecycle string) bool {
	switch lifecycle {
	case lifecycleCompleted, lifecycleFailed, lifecycleCancelled:
		return true
	default:
		return false
	}
}

// runTransitionIdentity is the trigger identity a command must still match to
// be the owner of the transition it is asking for. An empty field is not
// checked, because not every trigger has an owner to prove: lease recovery
// requeues a run precisely because no worker can vouch for it.
type runTransitionIdentity struct {
	workerID            string
	assignmentAttemptID string
	// approvalID names the approval a decision belongs to. It is checked
	// against the approvals this run actually owns, because a decision on
	// somebody else's approval must not move this run.
	approvalID string
}

// runTerminalContent is the content identity a terminal transition commits.
// write is the difference between a success report, which persists the exact
// bytes it was given, and a failure or cancellation, which must leave whatever
// the run already produced exactly as it is.
type runTerminalContent struct {
	assistantMessageID string
	content            string
	write              bool
	sha256             string
	hasDisplayable     bool
}

// runTransition is one guarded public lifecycle transition expressed as data,
// so every writer in this package is validated by the same code rather than by
// its own hand-written UPDATE predicate.
type runTransition struct {
	runID           string
	expectedVersion int64
	// transactionLocal marks a writer that is already inside the transaction it
	// is guarding and therefore resolves its own expectation from the row.
	// Without it, an absent expected version is an error rather than a silent
	// "whatever the row says": zero is protobuf absence, and a public caller
	// that forgot to carry a version must not be handed an unguarded write.
	//
	// It is set only by the ...InTx constructors in this file, never derived
	// from a version a caller supplied. Deriving it would mean the one input a
	// public caller fully controls could switch off the guard.
	transactionLocal bool
	allowedFrom      []string
	to               string
	reason           runoutcome.Reason
	identity         runTransitionIdentity
	terminal         *runTerminalContent
	// clearsOwnership marks a transition that releases the run's worker and
	// attempt. Its duplicate check cannot compare the identity the command
	// carried against the row, because the row no longer has one; it requires
	// the absence the transition itself produced instead.
	clearsOwnership bool
	// requiresAuthorizedApproval demands that the named approval still
	// authorizes something. It is opt-in because most transitions that name an
	// approval are the ones deciding it — a request is recorded while the
	// approval is pending by definition, and demanding an answer there would
	// refuse the very transition that asks the question.
	requiresAuthorizedApproval bool
	// durableApprovalIdentity makes the named approval part of what the
	// duplicate rule compares, by reading it back off the event this transition
	// commits.
	//
	// It exists because the row records no such thing. Two approvals on one run
	// produce two Readys that agree on the run, the worker, the attempt, the
	// expected version and the resulting lifecycle, so from the row alone the
	// second is indistinguishable from the first one repeating — and answering
	// it as a replay hands a worker an acceptance for an authorization that
	// resumed nothing. The event is where the difference is durable, which also
	// means a process that never saw the first Ready reaches the same verdict.
	durableApprovalIdentity bool
	// rejection is the sentinel this writer owes a caller when the row is not in
	// a lifecycle it accepts — already terminal, or simply not there yet.
	// Terminal writers keep their existing sentinels because callers outside
	// this package already map them onto gRPC status codes.
	rejection error
	// extraSet carries the execution, lease, and diagnostic columns a specific
	// writer owns. It is appended to the one guarded UPDATE so a transition can
	// never half-commit its execution state.
	//
	// An argument may be transitionTime, which the core replaces with the
	// canonical timestamp it computed. A writer cannot compute that itself —
	// the monotonic value is only known once the prior row is read under the
	// guard — and passing its own clock reading would put a second, slightly
	// different instant on the same transition.
	extraSet  string
	extraArgs []any
	// eventType is the single canonical projection. Empty means the transition
	// has no lifecycle event of its own and projects agent.run.state_changed.
	eventType    string
	eventPayload map[string]any
}

// transitionTime is the placeholder a writer puts in extraArgs where the
// canonical transition timestamp belongs.
type transitionTimePlaceholder struct{}

var transitionTime = transitionTimePlaceholder{}

func resolveTransitionArgs(args []any, at string) []any {
	resolved := make([]any, len(args))
	for index, arg := range args {
		if _, placeholder := arg.(transitionTimePlaceholder); placeholder {
			resolved[index] = at
			continue
		}
		resolved[index] = arg
	}
	return resolved
}

// runRow is the pre-transition snapshot every guarded transition reads. It
// carries the linked assistant message because correlation, content identity,
// and displayability are all decided from it.
type runRow struct {
	runID              string
	sessionID          string
	traceID            string
	userMessageID      string
	assistantMessageID string
	lifecycle          string
	outcomeReason      string
	stateVersion       int64
	stateUpdatedAt     string
	finishedAt         sql.NullString
	contentSHA256      string
	workerID           string
	attemptID          string
	messageID          string
	messageSessionID   string
	messageRunID       string
	messageRole        string
	messageContent     string
}

type runStateQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// runRowQuery reads a run with the assistant message it owns.
//
// The join cannot fan out: schema 0017 carries a partial unique index on
// messages(run_id) where run_id is not null and role is 'assistant', which is
// exactly this join's predicate, so a run matches at most one assistant row.
// Without that index the guarded transitions below would be reading one row of
// several and deciding content identity from whichever the planner returned.
const runRowQuery = `
	SELECT r.id, r.session_id, r.trace_id, r.user_message_id, COALESCE(r.assistant_message_id, ''),
		r.status, r.outcome_reason, r.state_version, r.state_updated_at, r.finished_at,
		r.assistant_content_sha256, COALESCE(r.worker_id, ''), COALESCE(r.execution_attempt_id, ''),
		COALESCE(m.id, ''), COALESCE(m.session_id, ''), COALESCE(m.run_id, ''),
		COALESCE(m.role, ''), COALESCE(m.content, '')
	FROM agent_runs r
	LEFT JOIN messages m ON m.run_id = r.id AND m.role = 'assistant'
	WHERE r.id = ?
`

func readRunRow(ctx context.Context, q runStateQuerier, runID string) (runRow, error) {
	var row runRow
	err := q.QueryRowContext(ctx, runRowQuery, runID).Scan(
		&row.runID, &row.sessionID, &row.traceID, &row.userMessageID, &row.assistantMessageID,
		&row.lifecycle, &row.outcomeReason, &row.stateVersion, &row.stateUpdatedAt, &row.finishedAt,
		&row.contentSHA256, &row.workerID, &row.attemptID,
		&row.messageID, &row.messageSessionID, &row.messageRunID, &row.messageRole, &row.messageContent,
	)
	return row, err
}

// runCorrelationLink projects a run row onto the one link shape the shared
// validator understands. The join is by the message's own run ID, so both
// directions of the circular link are read rather than assumed.
func runCorrelationLink(ctx context.Context, q runStateQuerier, runID string) (runcorrelation.Link, error) {
	row, err := readRunRow(ctx, q, runID)
	if err != nil {
		return runcorrelation.Link{}, err
	}
	return row.link(), nil
}

func (row runRow) link() runcorrelation.Link {
	return runcorrelation.Link{
		RunID:                 row.runID,
		RunSessionID:          row.sessionID,
		RunAssistantMessageID: row.assistantMessageID,
		MessageID:             row.messageID,
		MessageSessionID:      row.messageSessionID,
		MessageRunID:          row.messageRunID,
		MessageRole:           row.messageRole,
	}
}

// validateRunCorrelationLink is the one call site name this package uses for
// the shared rule, so a future writer cannot quietly introduce a second,
// slightly different notion of "these belong together".
func validateRunCorrelationLink(link runcorrelation.Link) error {
	return runcorrelation.Validate(link)
}

// state projects a row onto the canonical durable state. Displayability is
// computed from the linked message rather than stored, because the message is
// the only authority on what a client will actually render.
func (row runRow) state() RunState {
	return RunState{
		RunID:                 row.runID,
		UserMessageID:         row.userMessageID,
		AssistantMessageID:    row.assistantMessageID,
		Lifecycle:             row.lifecycle,
		OutcomeReason:         row.outcomeReason,
		StateVersion:          row.stateVersion,
		StateUpdatedAt:        row.stateUpdatedAt,
		FinishedAt:            row.finishedAt,
		HasDisplayableContent: runoutcome.HasDisplayableContent(row.messageContent),
		ContentSHA256:         row.contentSHA256,
	}
}

// GetRunState reads the canonical state of one run.
func (r *Repository) GetRunState(ctx context.Context, runID string) (RunState, error) {
	row, err := readRunRow(ctx, r.db, runID)
	if err != nil {
		return RunState{}, err
	}
	return row.state(), nil
}

// currentRunStateVersionTx reads the version a run's durable state holds right
// now, inside the transaction that is about to project it.
//
// It exists for the projections that are appended BEFORE the transition they
// explain — the give-up notices — which have no committed result to name yet.
// Reading here rather than before the transaction is the whole point: an
// outside read could be overtaken by another writer, and the notice would name
// a version the log had already moved past.
func currentRunStateVersionTx(ctx context.Context, tx *sql.Tx, runID string) (int64, error) {
	var version int64
	if err := tx.QueryRowContext(ctx, `SELECT state_version FROM agent_runs WHERE id = ?`, runID).Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

// runStateSnapshot is the public projection carried by every lifecycle event.
// The content digest and every execution detail are deliberately absent: the
// digest is internal duplicate identity, and a worker ID is not a client's
// business.
//
// assistantMessageId is present only when the run actually owns one. A run
// migrated from legacy history, or one whose message did not survive, has no
// assistant identity to give, and publishing an empty string would hand a
// client something that reads as an ID and names nothing. The run-outcomes
// migration already omits the key in exactly that case; this is the same rule
// on the live writer, so a client cannot tell the two origins apart.
func runStateSnapshot(state RunState) map[string]any {
	snapshot := map[string]any{
		"runId":                 state.RunID,
		"userMessageId":         state.UserMessageID,
		"lifecycle":             state.Lifecycle,
		"outcomeReason":         state.OutcomeReason,
		"stateVersion":          state.StateVersion,
		"stateUpdatedAt":        state.StateUpdatedAt,
		"hasDisplayableContent": state.HasDisplayableContent,
	}
	if state.AssistantMessageID != "" {
		snapshot["assistantMessageId"] = state.AssistantMessageID
	}
	if state.FinishedAt.Valid && state.FinishedAt.String != "" {
		snapshot["finishedAt"] = state.FinishedAt.String
	}
	return snapshot
}

// marshalRunStatePayload merges a writer's own payload with the canonical
// snapshot. The snapshot is assigned last so no writer key can shadow it.
func marshalRunStatePayload(payload map[string]any, state RunState) (string, error) {
	merged := make(map[string]any, len(payload)+1)
	for key, value := range payload {
		merged[key] = value
	}
	merged["runState"] = runStateSnapshot(state)
	encoded, err := json.Marshal(merged)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// applyRunTransitionTx is the single guarded lifecycle transition.
//
// Everything a transition has to get right lives here exactly once: the
// allowed lifecycle/reason matrix, the run/message correlation, the trigger
// identity that fences a stale owner, the expected version, the write-free
// duplicate rule, the monotonic timestamp, the guarded UPDATE, and the one
// canonical event. Writers supply only what is genuinely theirs — their
// execution columns, their subsidiary rows, and their event payload.
//
// subsidiary runs after the row is committed and before the canonical event, so
// the projection a client reads is appended after everything it describes.
func applyRunTransitionTx(
	ctx context.Context,
	tx *sql.Tx,
	transition runTransition,
	subsidiary func(context.Context, RunState, string) ([]Event, error),
) (RunTransitionResult, error) {
	allowedReasons, known := allowedOutcomeReasons[transition.to]
	if !known || !slices.Contains(allowedReasons, transition.reason) {
		return RunTransitionResult{}, ErrRunTransitionUnsupported
	}
	if !transition.transactionLocal && transition.expectedVersion < 1 {
		return RunTransitionResult{}, ErrRunStateVersionInvalid
	}

	row, err := readRunRow(ctx, tx, transition.runID)
	if err != nil {
		return RunTransitionResult{}, err
	}
	// A run whose assistant link is not complete and mutually consistent cannot
	// be transitioned: its state would be published against a message that does
	// not claim it. Absence is refused on the same terms as a mismatch, because
	// half a link proves ownership no better than a contradictory one. The
	// neutral fallback that lets a legacy row survive without a usable link is
	// a rule about immutable history, read-only by construction; a writer that
	// borrowed it would commit new state on top of the same missing proof.
	if err := validateRunCorrelationLink(row.link()); err != nil {
		return RunTransitionResult{}, err
	}

	expected := transition.expectedVersion
	if transition.transactionLocal {
		// The transaction-local path: the version this command would have
		// carried is derived from the row it is about to guard, inside this
		// transaction, so there is no read-then-write window to lose.
		expected = row.stateVersion
		if !slices.Contains(transition.allowedFrom, row.lifecycle) {
			expected = row.stateVersion - 1
		}
	}

	// Ownership of a named approval is established BEFORE anything else can
	// answer this command, including the write-free duplicate below. A
	// duplicate is recognized from the row alone, so a command naming an
	// approval this run never owned would otherwise be told "yes, that is
	// already done" and handed this run's state — the strongest possible answer
	// to a decision about something else entirely.
	//
	// Whether the approval still authorizes anything is checked in the same
	// place and for the same reason: a transition that requires an
	// authorization must not be able to reach the replay path on a question
	// nobody answered, or on an answer of no.
	if transition.identity.approvalID != "" {
		approvalStatus, err := runApprovalStatusTx(ctx, tx, transition.runID, transition.identity.approvalID)
		if err != nil {
			return RunTransitionResult{}, err
		}
		if approvalStatus == "" {
			return RunTransitionResult{}, ErrRunTransitionConflict
		}
		if transition.requiresAuthorizedApproval && !approvalAuthorizesResume(approvalStatus) {
			return RunTransitionResult{}, ErrRunTransitionConflict
		}
	}

	// The assistant message a terminal report names is checked here, before the
	// duplicate and before the lifecycle, for the same reason approval
	// ownership is. A report about a different run is not a late report and not
	// a replay: answering it with "already done" or "this run cannot fail"
	// would tell a caller its own run is finished when that run is still going.
	// Failure and cancellation write no content, so this is the only place
	// their identity is ever checked.
	if content := transition.terminal; content != nil &&
		content.assistantMessageID != "" && content.assistantMessageID != row.assistantMessageID {
		return RunTransitionResult{}, ErrRunTransitionConflict
	}

	if isRunTransitionDuplicate(row, transition, expected) {
		// The row says this transition already happened. For a transition whose
		// trigger the row does not record, that is only half an answer: what is
		// left is whether THIS trigger is the one that committed the version the
		// row is sitting at, which only the event it wrote can say.
		committed, err := transitionCommittedThisTriggerTx(ctx, tx, transition, row.stateVersion)
		if err != nil {
			return RunTransitionResult{}, err
		}
		if !committed {
			// Somebody else's transition put the row here. This one lost, which
			// is a conflict rather than a replay — and it is refused before the
			// lifecycle check below could give it any other answer.
			return RunTransitionResult{}, ErrRunTransitionConflict
		}
		return RunTransitionResult{State: row.state(), Duplicate: true}, nil
	}
	if !slices.Contains(transition.allowedFrom, row.lifecycle) {
		// A refused command answers with its own writer's sentinel wherever it
		// has one, terminal source or not. Callers already branch on those
		// sentinels and the gRPC boundary maps them onto FailedPrecondition; a
		// generic conflict in their place turns a precondition a caller can act
		// on into an unknown internal error. Version and identity conflicts
		// below keep the conflict sentinel, because those are lost races rather
		// than refused preconditions.
		if transition.rejection != nil {
			return RunTransitionResult{}, transition.rejection
		}
		return RunTransitionResult{}, ErrRunTransitionConflict
	}
	if expected < 1 {
		return RunTransitionResult{}, ErrRunStateVersionInvalid
	}
	if row.stateVersion != expected {
		return RunTransitionResult{}, ErrRunTransitionConflict
	}
	if !matchesTransitionIdentity(row, transition.identity) {
		return RunTransitionResult{}, ErrRunTransitionConflict
	}
	if row.stateVersion == math.MaxInt64 {
		return RunTransitionResult{}, ErrRunStateVersionExhausted
	}
	transitionAt, err := persisttime.NextStateTime(time.Now(), row.stateUpdatedAt)
	if err != nil {
		return RunTransitionResult{}, err
	}
	nextVersion := row.stateVersion + 1

	content := transition.terminal
	if content != nil && content.write && content.assistantMessageID != "" {
		result, err := tx.ExecContext(ctx,
			`UPDATE messages SET content = ? WHERE id = ? AND run_id = ? AND role = 'assistant'`,
			content.content, content.assistantMessageID, transition.runID)
		if err != nil {
			return RunTransitionResult{}, err
		}
		if err := expectOneRow(result, "assistant message not found"); err != nil {
			return RunTransitionResult{}, err
		}
	}

	update := `UPDATE agent_runs SET status = ?, outcome_reason = ?, state_version = ?, state_updated_at = ?`
	args := []any{transition.to, string(transition.reason), nextVersion, transitionAt}
	if isTerminalLifecycle(transition.to) {
		update += `, finished_at = ?`
		args = append(args, transitionAt)
	}
	if content != nil {
		update += `, assistant_content_sha256 = ?`
		args = append(args, content.sha256)
	}
	if transition.extraSet != "" {
		update += `, ` + transition.extraSet
		args = append(args, resolveTransitionArgs(transition.extraArgs, transitionAt)...)
	}
	update += ` WHERE id = ? AND status = ? AND state_version = ?`
	args = append(args, transition.runID, row.lifecycle, row.stateVersion)
	result, err := tx.ExecContext(ctx, update, args...)
	if err != nil {
		return RunTransitionResult{}, err
	}
	if err := expectOneRowErr(result, ErrRunTransitionConflict); err != nil {
		return RunTransitionResult{}, err
	}

	committed := row.state()
	committed.Lifecycle = transition.to
	committed.OutcomeReason = string(transition.reason)
	committed.StateVersion = nextVersion
	committed.StateUpdatedAt = transitionAt
	if isTerminalLifecycle(transition.to) {
		committed.FinishedAt = sql.NullString{String: transitionAt, Valid: true}
	}
	if content != nil {
		committed.ContentSHA256 = content.sha256
		committed.HasDisplayableContent = content.hasDisplayable
	}

	var events []Event
	if subsidiary != nil {
		events, err = subsidiary(ctx, committed, transitionAt)
		if err != nil {
			return RunTransitionResult{}, err
		}
	}
	payloadJSON, err := marshalRunStatePayload(transition.eventPayload, committed)
	if err != nil {
		return RunTransitionResult{}, err
	}
	event, err := appendRunEventTx(ctx, tx, row.sessionID, transition.runID, row.traceID,
		transitionEventType(transition), payloadJSON, transitionAt)
	if err != nil {
		return RunTransitionResult{}, err
	}
	return RunTransitionResult{State: committed, Events: append(events, event)}, nil
}

// transitionEventType is the single canonical projection a transition appends.
// A writer with no lifecycle event of its own projects the shared state change.
func transitionEventType(transition runTransition) string {
	if transition.eventType != "" {
		return transition.eventType
	}
	return runStateChangedEventType
}

// isRunTransitionDuplicate recognizes the one repeat that is free: the row is
// already exactly where this command wanted to put it, one version on from the
// version it expected, and every identity the command carries still matches.
//
// A terminal repeat additionally has to match the content identity, so a second
// completion carrying different bytes is a conflict rather than a replay.
func isRunTransitionDuplicate(row runRow, transition runTransition, expected int64) bool {
	if expected < 1 || expected == math.MaxInt64 {
		return false
	}
	if row.stateVersion != expected+1 {
		return false
	}
	if row.lifecycle != transition.to || row.outcomeReason != string(transition.reason) {
		return false
	}
	if transition.clearsOwnership {
		if row.workerID != "" || row.attemptID != "" {
			return false
		}
	} else if !matchesTransitionIdentity(row, transition.identity) {
		return false
	}
	if content := transition.terminal; content != nil {
		// The named assistant message is already proven to be this run's own
		// by the identity gate above, so what is left to compare is the
		// content the row committed.
		if content.sha256 != row.contentSHA256 {
			return false
		}
		if content.hasDisplayable != runoutcome.HasDisplayableContent(row.messageContent) {
			return false
		}
	}
	return true
}

// transitionCommittedThisTriggerTx answers whether the version a row already
// sits at was committed by this exact trigger.
//
// It reads the transition's own event, because that is the only durable record
// of a trigger the run row does not keep. Matching on the resulting version and
// the event type identifies exactly one event — every transition moves the
// version by one and writes one projection — so a single matching row is proof
// and anything else is not. Absence fails closed: an unexplained version is not
// evidence that this command produced it.
//
// A transition that carries no durable trigger keeps the row-only rule it
// always had. An event written before this rule existed carries no trigger
// either, so a resume replayed across an upgrade is refused rather than
// replayed — the honest answer, since nothing durable says which approval put
// that run where it is.
func transitionCommittedThisTriggerTx(ctx context.Context, tx *sql.Tx, transition runTransition, version int64) (bool, error) {
	if !transition.durableApprovalIdentity {
		return true, nil
	}
	if transition.identity.approvalID == "" {
		return false, nil
	}
	var matches int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM events
		WHERE run_id = ? AND type = ?
			AND json_extract(payload_json, ?) = ?
			AND json_extract(payload_json, ?) = ?
	`,
		transition.runID, transitionEventType(transition),
		runStateVersionPayloadPath, version,
		approvalIdentityPayloadPath, transition.identity.approvalID,
	).Scan(&matches); err != nil {
		return false, err
	}
	return matches == 1, nil
}

// runApprovalStatusTx reports the durable status of an approval this run owns.
//
// An empty status means the run does not own it, which is the answer that
// matters most: a decision carried by a command is only that command's
// authority to move the run if the run is the one the decision was made about.
// Empty is unambiguous because the column is NOT NULL under a CHECK constraint
// that admits only the four decided words and pending.
func runApprovalStatusTx(ctx context.Context, tx *sql.Tx, runID string, approvalID string) (string, error) {
	var status string
	err := tx.QueryRowContext(ctx,
		`SELECT status FROM approvals WHERE id = ? AND run_id = ?`, approvalID, runID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return status, nil
}

// approvalAuthorizesResume reports whether an approval's durable status still
// stands as permission to make the call it was asked about.
//
// approved is a decision nobody has spent yet. consumed is the same decision
// after mcp-files redeemed its token, which is what a replayed resume meets
// once the authorized call has already run — the authorization is used up, not
// withdrawn, so it must answer the replay rather than fence it. Pending is a
// question nobody answered; denied and expired are an answer of no.
func approvalAuthorizesResume(status string) bool {
	return status == "approved" || status == "consumed"
}

// matchesTransitionIdentity checks the trigger identity against what the row
// currently records. An identity field the command did not supply is not
// checked: a lease recovery requeue has no owner to prove, and demanding one
// would strand exactly the runs recovery exists to rescue.
func matchesTransitionIdentity(row runRow, identity runTransitionIdentity) bool {
	if identity.workerID != "" && identity.workerID != row.workerID {
		return false
	}
	if identity.assignmentAttemptID != "" && identity.assignmentAttemptID != row.attemptID {
		return false
	}
	return true
}

// runInTransition runs one guarded transition in its own short transaction.
// The subsidiary callback receives the transaction so a writer can commit its
// own rows inside the same boundary as the state change.
func (r *Repository) runInTransition(
	ctx context.Context,
	transition runTransition,
	subsidiary func(context.Context, *sql.Tx, RunState, string) ([]Event, error),
) (RunTransitionResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RunTransitionResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := applyRunTransitionTx(ctx, tx, transition, func(ctx context.Context, state RunState, at string) ([]Event, error) {
		if subsidiary == nil {
			return nil, nil
		}
		return subsidiary(ctx, tx, state, at)
	})
	if err != nil {
		return RunTransitionResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return RunTransitionResult{}, err
	}
	return result, nil
}

// FenceRunOwnershipInput fences a run whose worker ownership became uncertain.
type FenceRunOwnershipInput struct {
	RunID                string
	ExpectedStateVersion int64
	WorkerID             string
	AssignmentAttemptID  string
}

// FenceRunOwnership moves a running or waiting-approval run to recovering.
//
// This is the transition the old code did not have. Losing a worker used to
// leave the row saying running, so a reopened session and a live stream both
// reported forward progress nobody was making.
func (r *Repository) FenceRunOwnership(ctx context.Context, input FenceRunOwnershipInput) (RunTransitionResult, error) {
	return r.runInTransition(ctx, fenceOwnershipTransition(input.RunID, input.ExpectedStateVersion, runTransitionIdentity{
		workerID:            input.WorkerID,
		assignmentAttemptID: input.AssignmentAttemptID,
	}, "uncertain"), nil)
}

func fenceOwnershipTransition(runID string, expectedVersion int64, identity runTransitionIdentity, executionState string) runTransition {
	return runTransition{
		runID:           runID,
		expectedVersion: expectedVersion,
		allowedFrom:     []string{lifecycleRunning, lifecycleWaitingApproval},
		to:              lifecycleRecovering,
		reason:          runoutcome.ReasonNone,
		identity:        identity,
		extraSet:        `execution_state = ?`,
		extraArgs:       []any{executionState},
	}
}

// fenceOwnershipTransitionInTx is the same fence for a writer that is already
// inside the transaction it is guarding. It takes no version at all: the
// expectation is resolved from the row under the guard, and there is no
// argument a caller could pass to reach this path from outside the package.
func fenceOwnershipTransitionInTx(runID string, identity runTransitionIdentity, executionState string) runTransition {
	transition := fenceOwnershipTransition(runID, unresolvedStateVersion, identity, executionState)
	transition.transactionLocal = true
	return transition
}

// ResumeRecoveringRunInput proves that the same still-owned attempt is alive
// and returns the run to running.
//
// There is deliberately no approval here. Recovering-to-running is the generic
// proof that a worker is back, and the approval handshake has its own
// transition — ResumeApprovedRun — which guards the approval twice over and
// commits it into the event so a replay can be told apart. Naming an approval
// on this one would have looked like the same guarantee while providing none of
// it: nothing durable would record which authorization moved the run.
type ResumeRecoveringRunInput struct {
	RunID                string
	ExpectedStateVersion int64
	WorkerID             string
	AssignmentAttemptID  string
}

// ResumeRecoveringRun returns a recovering run to running.
//
// Only the attempt that still owns the run may do this, which is why worker and
// assignment attempt are part of the transition identity rather than advisory
// arguments.
func (r *Repository) ResumeRecoveringRun(ctx context.Context, input ResumeRecoveringRunInput) (RunTransitionResult, error) {
	return r.runInTransition(ctx, runTransition{
		runID:           input.RunID,
		expectedVersion: input.ExpectedStateVersion,
		allowedFrom:     []string{lifecycleRecovering},
		to:              lifecycleRunning,
		reason:          runoutcome.ReasonNone,
		identity: runTransitionIdentity{
			workerID:            input.WorkerID,
			assignmentAttemptID: input.AssignmentAttemptID,
		},
		extraSet:  `execution_state = 'delivered'`,
		extraArgs: nil,
	}, nil)
}

// RequeueRecoveringRunInput returns a recovering run to the queue.
type RequeueRecoveringRunInput struct {
	RunID                string
	ExpectedStateVersion int64
	AssignmentAttemptID  string
}

// RequeueRecoveringRun sends a recovering run back to the queue for another
// worker. A run whose worker is merely gone passes through recovering first, so
// the interval where nobody owned it is durable rather than erased; only a
// release this transaction can prove takes the direct edge below.
func (r *Repository) RequeueRecoveringRun(ctx context.Context, input RequeueRecoveringRunInput) (RunTransitionResult, error) {
	return r.runInTransition(ctx, requeueRecoveringTransition(input.RunID, input.ExpectedStateVersion, runTransitionIdentity{
		assignmentAttemptID: input.AssignmentAttemptID,
	}), nil)
}

// requeueTransition is the shared body of every transition that puts a run back
// on the queue. The target, the outcome, and the execution columns it clears
// are identical no matter which state the run is coming from; only the source
// lifecycle differs, and that difference is exactly what separates a proven
// release from a recovery.
func requeueTransition(runID string, expectedVersion int64, allowedFrom []string, identity runTransitionIdentity) runTransition {
	return runTransition{
		runID:           runID,
		expectedVersion: expectedVersion,
		allowedFrom:     allowedFrom,
		to:              lifecycleQueued,
		reason:          runoutcome.ReasonNone,
		identity:        identity,
		clearsOwnership: true,
		extraSet: `started_at = NULL,
			worker_id = NULL,
			execution_active = 0,
			execution_exit_acknowledged_at = NULL,
			execution_attempt_id = NULL,
			execution_state = 'none',
			execution_lease_expires_at = NULL,
			execution_lease_expires_at_ns = NULL`,
	}
}

func requeueRecoveringTransition(runID string, expectedVersion int64, identity runTransitionIdentity) runTransition {
	return requeueTransition(runID, expectedVersion, []string{lifecycleRecovering}, identity)
}

// requeueRecoveringTransitionInTx is the requeue for a writer already inside
// the transaction it is guarding, on the same terms as the fence above.
func requeueRecoveringTransitionInTx(runID string, identity runTransitionIdentity) runTransition {
	transition := requeueRecoveringTransition(runID, unresolvedStateVersion, identity)
	transition.transactionLocal = true
	return transition
}

// releaseRunningTransition is the direct running-to-queued edge, and it is
// narrow on purpose.
//
// Recovering exists to publish the interval where nobody could say whether a
// run was still executing. A release this transaction can prove has no such
// interval: the assignment either never reached a worker, or the authenticated
// attempt that owned it handed it back. Publishing recovering for those would
// describe an executor that does not exist, and committing two versions would
// make a client reconcile through a state the run was never in.
//
// The proof is the caller's to supply, and the guards it must pass are the
// caller's whole authority: the exact version the release was computed against,
// and the worker and attempt the row still records. Anything short of that is
// uncertainty and belongs on the recovering path.
func releaseRunningTransition(runID string, expectedVersion int64, identity runTransitionIdentity) runTransition {
	return requeueTransition(runID, expectedVersion, []string{lifecycleRunning}, identity)
}

// releaseRunningTransitionInTx is the same direct edge for a writer already
// inside the transaction it is guarding.
//
// Recovering joins the accepted sources here and only here: this constructor
// serves the pre-delivery writers, whose proof is that the assignment command
// never left the orchestrator. That fact does not stop being true because an
// earlier reconciliation already fenced the run, and forcing such a run through
// recovering a second time would publish a phase it is already in.
func releaseRunningTransitionInTx(runID string, identity runTransitionIdentity) runTransition {
	transition := requeueTransition(runID, unresolvedStateVersion,
		[]string{lifecycleRunning, lifecycleRecovering}, identity)
	transition.transactionLocal = true
	return transition
}
