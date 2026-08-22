package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// emptyAssistantContentSHA256 is the content identity of the empty assistant
// placeholder every enqueue creates. A run starts life with no output, and the
// digest has to say so exactly rather than be left absent, because a later
// duplicate terminal report is judged against it.
//
// It is a constant, and an unexported one: this value is the fixed point
// duplicate detection compares against, so nothing — in this package or any
// other — may rebind it at runtime. The literal is the lowercase SHA-256 of
// zero bytes; a test in this package pins it to runoutcome.ContentSHA256("")
// so the two can never drift apart.
const emptyAssistantContentSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

type EnqueueUserMessageInput struct {
	SessionID                      string
	Content                        string
	ContentType                    string
	AgentID                        string
	ModelProvider                  string
	Model                          string
	RequestedModel                 string
	ExecutionModel                 string
	IdempotencyKey                 string
	RequestedTools                 []string
	SelectedTools                  []string
	RequiredContextTokens          int
	MinimumWorkerMaxConcurrentRuns int
	EgressDecision                 *PendingEgressDecision
	ValidateRouting                func(context.Context, RoutingRequirements) error
}

type RoutingRequirements struct {
	AgentID                        string
	ModelProvider                  string
	Model                          string
	RequestedTools                 []string
	SelectedTools                  []string
	RequiredContextTokens          int
	MinimumWorkerMaxConcurrentRuns int
	ExternalAgent                  bool
	ExternalAgentCredentialRef     string
}

type RoutingModelCapability struct {
	Provider         string
	Model            string
	MaxContextTokens int
}

type WorkerRoutingCapabilities struct {
	Models                      []RoutingModelCapability
	Tools                       []string
	MaxConcurrentRuns           int
	ExternalAgentCredentialRefs []string
	RemoteEgressDecisionVersion int
}

type PendingRoutingWork struct {
	RunID        string
	Requirements RoutingRequirements
}

type PendingRoutingCursor struct {
	CreatedAtNanos int64
	JobID          string
}

type EnqueueUserMessageResult struct {
	SessionID           string
	UserMessageID       string
	AssistantMessageID  string
	RunID               string
	JobID               string
	TraceID             string
	Status              string
	SessionUpdatedEvent Event
	QueuedEvent         Event
	// Replayed is true when an existing idempotent enqueue was returned. Its
	// events already reached the durable log and must not be published again.
	Replayed bool
	// RoutingEvents carries the notices written in the same transaction as the
	// run — today, the one saying this message is leaving the machine. They are
	// returned rather than published here because the repository does not own
	// the event bus, and a caller that forgot to publish them would leave a
	// subscriber with no record of the egress.
	RoutingEvents []Event
}

type Job struct {
	JobID                          string
	RunID                          string
	SessionID                      string
	UserMessageID                  string
	AssistantMessageID             string
	AgentID                        string
	TraceID                        string
	ModelProvider                  string
	Model                          string
	UserText                       string
	RequestedTools                 []string
	RequiredContextTokens          int
	MinimumWorkerMaxConcurrentRuns int
	Attempt                        int
	AssignmentAttemptID            string
	// ExpectedStateVersion is the run's version at the moment this claim
	// committed queued -> running. The worker echoes it on later reports, so a
	// report computed against a state the run has already left is refused
	// rather than applied.
	ExpectedStateVersion int64
	Skills               []SkillSnapshot
	// ExternalAgent is nil for the local assistant, which is the default and
	// the common case.
	ExternalAgent  *ExternalAgentTarget
	EgressDecision *RunEgressDecision
	SelectedTools  []string
	StartedEvent   Event
}

type queuedJobPayload struct {
	UserText                       string               `json:"userText"`
	RequestedTools                 []string             `json:"requestedTools"`
	RequiredContextTokens          int                  `json:"requiredContextTokens"`
	MinimumWorkerMaxConcurrentRuns int                  `json:"minimumWorkerMaxConcurrentRuns"`
	Skills                         []SkillSnapshot      `json:"skills"`
	ExternalAgent                  *ExternalAgentTarget `json:"externalAgent"`
	EgressDecision                 *RunEgressDecision   `json:"egressDecision"`
	SelectedTools                  []string             `json:"selectedTools"`
}

type Assignment struct {
	JobID     string
	RunID     string
	WorkerID  string
	AttemptID string
}

var ErrAssignmentFenced = errors.New("assignment attempt is fenced")
var ErrIdempotencyConflict = errors.New("idempotency key was already used for a different request")

type sendMessageIdempotencyRecord struct {
	SessionID           string
	RequestFingerprint  string
	UserMessageID       string
	AssistantMessageID  string
	RunID               string
	JobID               string
	TraceID             string
	QueuedEventSequence int64
	CreatedAt           string
}

func (record sendMessageIdempotencyRecord) result() EnqueueUserMessageResult {
	return EnqueueUserMessageResult{
		SessionID:          record.SessionID,
		UserMessageID:      record.UserMessageID,
		AssistantMessageID: record.AssistantMessageID,
		RunID:              record.RunID,
		JobID:              record.JobID,
		TraceID:            record.TraceID,
		Status:             "queued",
		QueuedEvent: Event{
			SessionID: record.SessionID,
			RunID:     sql.NullString{String: record.RunID, Valid: true},
			TraceID:   record.TraceID,
			Sequence:  record.QueuedEventSequence,
			Type:      "agent.run.queued",
			CreatedAt: record.CreatedAt,
		},
		Replayed: true,
	}
}

func enqueueRequestFingerprint(input EnqueueUserMessageInput) (string, error) {
	type egressFingerprint struct {
		Version                   int                     `json:"version"`
		Provider                  string                  `json:"provider"`
		Model                     string                  `json:"model"`
		RequestDigest             string                  `json:"request_digest"`
		ExternalAgentID           string                  `json:"external_agent_id"`
		ExternalCredentialRefHash string                  `json:"external_credential_ref_hash"`
		Endpoint                  string                  `json:"endpoint"`
		EndpointHost              string                  `json:"endpoint_host"`
		DataCategories            []string                `json:"data_categories"`
		SelectedTools             []string                `json:"selected_tools"`
		SkillSnapshotFingerprint  string                  `json:"skill_snapshot_fingerprint"`
		RecallApplicable          bool                    `json:"recall_applicable"`
		MemoryProfileApplicable   bool                    `json:"memory_profile_applicable"`
		RemoteMCPServers          []RemoteMCPServerEgress `json:"remote_mcp_servers"`
	}
	var egressDecision *egressFingerprint
	if input.EgressDecision != nil {
		egressDecision = &egressFingerprint{
			Version:                   input.EgressDecision.Version,
			Provider:                  input.EgressDecision.Provider,
			Model:                     input.EgressDecision.Model,
			RequestDigest:             input.EgressDecision.RequestDigest,
			ExternalAgentID:           input.EgressDecision.ExternalAgentID,
			ExternalCredentialRefHash: input.EgressDecision.ExternalCredentialRefHash,
			Endpoint:                  input.EgressDecision.Endpoint,
			EndpointHost:              input.EgressDecision.EndpointHost,
			DataCategories:            append([]string(nil), input.EgressDecision.DataCategories...),
			SelectedTools:             append([]string(nil), input.EgressDecision.SelectedTools...),
			SkillSnapshotFingerprint:  input.EgressDecision.SkillSnapshotFingerprint,
			RecallApplicable:          input.EgressDecision.RecallApplicable,
			MemoryProfileApplicable:   input.EgressDecision.MemoryProfileApplicable,
			RemoteMCPServers:          append([]RemoteMCPServerEgress(nil), input.EgressDecision.RemoteMCPServers...),
		}
	}
	version := 4
	requestedModel := input.RequestedModel
	if egressDecision == nil {
		version = 2
		requestedModel = ""
	}
	canonical, err := json.Marshal(struct {
		Version                        int                `json:"version"`
		SessionID                      string             `json:"session_id"`
		Content                        string             `json:"content"`
		ContentType                    string             `json:"content_type"`
		AgentID                        string             `json:"agent_id"`
		ModelProvider                  string             `json:"model_provider"`
		Model                          string             `json:"model"`
		RequestedModel                 string             `json:"requested_model,omitempty"`
		RequestedTools                 []string           `json:"requested_tools"`
		RequiredContextTokens          int                `json:"required_context_tokens"`
		MinimumWorkerMaxConcurrentRuns int                `json:"minimum_worker_max_concurrent_runs"`
		EgressDecision                 *egressFingerprint `json:"egress_decision,omitempty"`
	}{
		Version:                        version,
		SessionID:                      input.SessionID,
		Content:                        input.Content,
		ContentType:                    input.ContentType,
		AgentID:                        input.AgentID,
		ModelProvider:                  input.ModelProvider,
		Model:                          input.Model,
		RequestedModel:                 requestedModel,
		RequestedTools:                 input.RequestedTools,
		RequiredContextTokens:          input.RequiredContextTokens,
		MinimumWorkerMaxConcurrentRuns: input.MinimumWorkerMaxConcurrentRuns,
		EgressDecision:                 egressDecision,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func findSendMessageIdempotencyTx(ctx context.Context, tx *sql.Tx, idempotencyKey string) (sendMessageIdempotencyRecord, bool, error) {
	var record sendMessageIdempotencyRecord
	err := tx.QueryRowContext(ctx, `
		SELECT
			session_id,
			request_fingerprint,
			user_message_id,
			assistant_message_id,
			run_id,
			job_id,
			trace_id,
			queued_event_sequence,
			created_at
		FROM send_message_idempotency
		WHERE idempotency_key = ?
	`, idempotencyKey).Scan(
		&record.SessionID,
		&record.RequestFingerprint,
		&record.UserMessageID,
		&record.AssistantMessageID,
		&record.RunID,
		&record.JobID,
		&record.TraceID,
		&record.QueuedEventSequence,
		&record.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return sendMessageIdempotencyRecord{}, false, nil
	}
	if err != nil {
		return sendMessageIdempotencyRecord{}, false, err
	}
	return record, true, nil
}

// RetriesExhaustedCode marks a run that was terminally failed only after its
// retry budget was spent. It lets an operator distinguish "gave up after
// retries" from a run that failed on its first attempt.
const RetriesExhaustedCode = "retries_exhausted"

// RetryDecision reports how a retryable run failure was resolved: either the
// job was requeued for another attempt, or (once attempts were exhausted) the
// run was terminally failed and the terminal lifecycle events were emitted.
type RetryDecision struct {
	Requeued bool
	Events   []Event
}

// RetryableRunFailureInput is one report that a run failed in a way another
// attempt could survive.
//
// Worker and assignment attempt are the difference between the two shapes this
// repository accepts. Supplying both is a claim that the caller knows exactly
// who was holding the run when it was released — an authenticated attempt
// handing its own run back — and that claim is checked against the row before
// anything is written. Supplying neither is an honest statement that nobody can
// vouch for the run, which is what recovering exists to publish. Half an
// identity is neither, and is refused rather than quietly demoted to the weaker
// path.
type RetryableRunFailureInput struct {
	RunID string
	// ExpectedStateVersion is the version the release was computed against. It
	// is required for a confirmed release and unused otherwise: a caller with no
	// owner to name has nothing to prove the run has not moved on, so the
	// uncertain path resolves the expectation from the row inside its own
	// transaction instead.
	ExpectedStateVersion int64
	WorkerID             string
	AssignmentAttemptID  string
	Failure              runoutcome.Failure
	MaxAttempts          int
}

// confirmedRelease reports whether this input names the owner it is releasing.
func (input RetryableRunFailureInput) confirmedRelease() bool {
	return input.WorkerID != "" && input.AssignmentAttemptID != ""
}

// validate refuses an input that is neither a confirmed release nor an explicit
// uncertainty.
func (input RetryableRunFailureInput) validate() error {
	if (input.WorkerID == "") != (input.AssignmentAttemptID == "") {
		return ErrRetryIdentityIncomplete
	}
	if input.confirmedRelease() && input.ExpectedStateVersion < 1 {
		return ErrRunStateVersionInvalid
	}
	return nil
}

// identity projects the input onto the trigger identity a guarded transition
// fences on.
func (input RetryableRunFailureInput) identity() runTransitionIdentity {
	return runTransitionIdentity{
		workerID:            input.WorkerID,
		assignmentAttemptID: input.AssignmentAttemptID,
	}
}

// ErrRetryIdentityIncomplete rejects a retryable failure that names half an
// owner. A worker without its attempt, or an attempt without its worker, proves
// nothing about who was holding the run, and silently treating it as an
// uncertain recovery would hide a caller that meant to prove something.
var ErrRetryIdentityIncomplete = errors.New("incomplete retry assignment identity")

// RequeueOrFailRetryableRun handles a retryable run failure (for example a
// worker rejecting an assignment because it was busy). While the job still has
// attempts left it is requeued for another worker to claim; once the attempt
// budget is spent — or the run is no longer in a requeueable state — the run is
// terminally failed with a distinguishable code so the message does not bounce
// forever. input.MaxAttempts caps the total number of attempts.
//
// A confirmed release requeues the run in one transaction, one version, and one
// queued projection. An uncertain report commits the two real transitions the
// run actually went through. A confirmed release that fails any of its guards
// is fenced without a write; it never falls back to the uncertain path, because
// a caller that claimed to know the owner and was wrong has not discovered
// uncertainty — it has been overtaken.
func (r *Repository) RequeueOrFailRetryableRun(ctx context.Context, input RetryableRunFailureInput) (RetryDecision, error) {
	if err := input.validate(); err != nil {
		return RetryDecision{}, err
	}
	maxAttempts := input.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = defaultAssignmentMaxAttempts
	}
	runID := input.RunID
	// The caller normalizes before it gets here, so the only thing this reads
	// is the outcome that normalization produced. worker_busy and
	// worker_unavailable carry outcome none, which is what keeps a requeue from
	// terminalizing a run that is going to run.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RetryDecision{}, err
	}
	defer func() { _ = tx.Rollback() }()

	var runStatus, sessionID, traceID, rowWorkerID, rowAttemptID string
	var stateVersion int64
	if err := tx.QueryRowContext(ctx, `
		SELECT status, session_id, trace_id, COALESCE(worker_id, ''), COALESCE(execution_attempt_id, ''), state_version
		FROM agent_runs
		WHERE id = ?
	`, runID).Scan(&runStatus, &sessionID, &traceID, &rowWorkerID, &rowAttemptID, &stateVersion); err != nil {
		return RetryDecision{}, err
	}
	if input.confirmedRelease() {
		// Every one of these is a claim about a run somebody else may be
		// holding, or about a state the run has already left. Applying one
		// would requeue work that is still executing, so the report is refused
		// before it can touch the row, the job, or the log.
		if runStatus != lifecycleRunning ||
			stateVersion != input.ExpectedStateVersion ||
			rowWorkerID != input.WorkerID ||
			rowAttemptID != input.AssignmentAttemptID {
			return RetryDecision{}, ErrAssignmentFenced
		}
	}
	attempt, jobID, jobErr := inProgressRetryJobTx(ctx, tx, runID, input)
	if jobErr != nil && !errors.Is(jobErr, sql.ErrNoRows) {
		return RetryDecision{}, jobErr
	}
	if input.confirmedRelease() && jobErr != nil {
		// The owner checked out against the run row but owns no in-progress
		// job, so the assignment it is releasing is not the one the queue
		// holds. That is the same fence as a stale attempt.
		return RetryDecision{}, ErrAssignmentFenced
	}
	// A run already fenced into recovering is still requeueable: recovering is
	// what a lost worker looks like, and it is the state a retry starts from. A
	// confirmed release has already proven a narrower thing than that.
	requeueable := input.confirmedRelease() ||
		((runStatus == lifecycleRunning || runStatus == lifecycleRecovering) && jobErr == nil)

	if requeueable && attempt < maxAttempts {
		requeued, requeueEvents, err := requeueRetryableRunTx(ctx, tx, jobID, input)
		if err != nil {
			return RetryDecision{}, err
		}
		// attempt is the one that just failed, so the attempt the user is about to
		// wait through is attempt+1 — matching the counter the requeue has just
		// incremented.
		notice, err := newRetryNotice(runoutcome.NoticeDispatchRetry, attempt+1, maxAttempts)
		if err != nil {
			return RetryDecision{}, err
		}
		// The notice lands after every projection the requeue committed, so it
		// is anchored to the version the requeue just committed — the version a
		// client reading the notice will find on the run.
		noticeEvent, err := appendStepNoticeTx(ctx, tx, sessionID, runID, traceID, notice, requeued.StateVersion, now())
		if err != nil {
			return RetryDecision{}, err
		}
		if err := tx.Commit(); err != nil {
			return RetryDecision{}, err
		}
		return RetryDecision{Requeued: true, Events: append(requeueEvents, noticeEvent)}, nil
	}

	failure := input.Failure
	var events []Event
	if requeueable && attempt >= maxAttempts {
		// Giving up is its own outcome, reported under its own normalized code,
		// rather than the transient condition that triggered the last attempt.
		failure = runoutcome.NormalizeFailure(runoutcome.OriginDispatch, RetriesExhaustedCode, runoutcome.RetryClassNever)
		// The client renders a terminal failure card for agent.run.failed, but
		// that card explains the failure, not that retries were attempted and
		// exhausted; this notice carries the attempt count. Ordered before the
		// terminal events so the explanation precedes the failure it explains.
		notice, err := newRetryNotice(runoutcome.NoticeRecoveryExhausted, attempt, maxAttempts)
		if err != nil {
			return RetryDecision{}, err
		}
		// This notice precedes the terminal transition, so it is anchored to the
		// version the run still holds here. Naming the version the failure is
		// about to commit would claim a state that does not exist yet at this
		// point in the log, and the read is inside this transaction so no other
		// writer can move the row between the two.
		currentVersion, err := currentRunStateVersionTx(ctx, tx, runID)
		if err != nil {
			return RetryDecision{}, err
		}
		noticeEvent, err := appendStepNoticeTx(ctx, tx, sessionID, runID, traceID, notice, currentVersion, now())
		if err != nil {
			return RetryDecision{}, err
		}
		events = append(events, noticeEvent)
	}
	if failure.Reason() == runoutcome.ReasonNone {
		// A nonterminal dispatch condition on a run that cannot be requeued has
		// no honest terminal reason of its own; the run failed for want of a
		// worker, which is a dispatch failure, not a worker's verdict.
		failure = runoutcome.NormalizeFailure(runoutcome.OriginRecovery, "worker_unavailable", runoutcome.RetryClassNever)
	}
	terminal, err := failRunTx(ctx, tx, FailRunInput{RunID: runID, Failure: failure, resolveVersionInTx: true})
	if err != nil {
		return RetryDecision{}, err
	}
	events = append(events, terminal.Events...)
	if err := tx.Commit(); err != nil {
		return RetryDecision{}, err
	}
	return RetryDecision{Events: events}, nil
}

// inProgressRetryJobTx reads the in-progress job a retryable failure is about.
//
// A confirmed release must name the exact assignment the queue holds — the
// worker that leased it and the attempt it was dispatched under — because the
// whole authority of the direct edge is that this transaction can see who was
// holding the run. An uncertain report has no such names and takes whatever
// in-progress job the run has, which is the only thing it can honestly ask for.
func inProgressRetryJobTx(ctx context.Context, tx *sql.Tx, runID string, input RetryableRunFailureInput) (int, string, error) {
	var attempt int
	var jobID string
	err := tx.QueryRowContext(ctx, `
		SELECT id, attempt
		FROM jobs
		WHERE run_id = ?
			AND status = 'in_progress'
			AND (? = '' OR COALESCE(assignment_attempt_id, '') = ?)
			AND (? = '' OR COALESCE(lease_owner, '') = ?)
	`, runID,
		input.AssignmentAttemptID, input.AssignmentAttemptID,
		input.WorkerID, input.WorkerID,
	).Scan(&jobID, &attempt)
	return attempt, jobID, err
}

// requeueRetryableRunTx commits the requeue half of a retryable failure on the
// path the caller's proof entitles it to.
//
// The two are not variants of one write. A confirmed release moves the run
// straight back to the queue under the version and owner it named; an uncertain
// report commits the interval where nobody owned the run and then the requeue
// out of it. Choosing between them here, from the same input the guards above
// checked, is what keeps a claimed release from silently degrading into the
// weaker path when its proof does not hold.
func requeueRetryableRunTx(ctx context.Context, tx *sql.Tx, jobID string, input RetryableRunFailureInput) (RunState, []Event, error) {
	if !input.confirmedRelease() {
		return requeueRunThroughRecoveryTx(ctx, tx, input.RunID, "", runTransitionIdentity{}, true)
	}
	// The job half is the same release the uncertain path performs, so it runs
	// through the same helper. Naming the exact job and attempt keeps this
	// path's fence: a row that no longer matches means a newer attempt owns the
	// run, which the helper reports as ErrAssignmentFenced. The worker guard the
	// direct edge also requires was already applied when this transaction read
	// the job under lease_owner. Only the run transition below is specific to a
	// confirmed release.
	if err := releaseJobToPendingTx(ctx, tx, input.RunID, jobID, input.AssignmentAttemptID, true); err != nil {
		return RunState{}, nil, err
	}
	released, err := applyRunTransitionTx(ctx, tx,
		releaseRunningTransition(input.RunID, input.ExpectedStateVersion, input.identity()), nil)
	if err != nil {
		if errors.Is(err, ErrRunTransitionConflict) {
			return RunState{}, nil, ErrAssignmentFenced
		}
		return RunState{}, nil, err
	}
	return released.State, released.Events, nil
}

// newRetryNotice builds a bounded, allowlisted retry projection or fails
// closed. A notice that cannot be expressed in the closed vocabulary is not
// downgraded to prose; it is refused.
func newRetryNotice(category runoutcome.NoticeCategory, attempt int, maxAttempts int) (runoutcome.StepNotice, error) {
	if attempt < 1 || maxAttempts < 1 || attempt > maxAttempts || maxAttempts > runoutcome.MaxNoticeAttempts {
		return runoutcome.StepNotice{}, runoutcome.ErrUnsupportedNotice
	}
	return runoutcome.NewStepNotice(category, int32(attempt), int32(maxAttempts))
}

// requeueRunThroughRecoveryTx returns an active run and its in-progress job to
// the queue.
//
// The run passes through recovering on the way, and that is the point: the
// interval where nobody owned the run is a real phase of its life, and
// rewriting running straight back to queued erased it. Both transitions commit
// here, in this one transaction, each with its own version and its own
// projection — so a client reconciling on version never sees a number that
// never existed, and never sees the run claim forward progress it was not
// making.
//
// jobID and identity.assignmentAttemptID are optional guards. Retry after a
// worker rejection knows neither and requeues whatever in-progress job the run
// has; assignment reconciliation knows both and must not requeue a job a newer
// attempt already owns.
func requeueRunThroughRecoveryTx(
	ctx context.Context,
	tx *sql.Tx,
	runID string,
	jobID string,
	identity runTransitionIdentity,
	incrementAttempt bool,
) (RunState, []Event, error) {
	if err := releaseJobToPendingTx(ctx, tx, runID, jobID, identity.assignmentAttemptID, incrementAttempt); err != nil {
		return RunState{}, nil, err
	}
	return requeueRunLifecycleTx(ctx, tx, runID, identity, "uncertain")
}

// releaseJobToPendingTx hands a run's in-progress job back to the queue.
//
// jobID and attemptID are optional guards. Retry after an anonymous worker
// rejection knows neither and requeues whatever in-progress job the run has;
// assignment reconciliation knows both and must not requeue a job a newer
// attempt already owns.
func releaseJobToPendingTx(
	ctx context.Context,
	tx *sql.Tx,
	runID string,
	jobID string,
	attemptID string,
	incrementAttempt bool,
) error {
	attemptIncrement := 0
	if incrementAttempt {
		attemptIncrement = 1
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'pending',
			lease_owner = NULL,
			lease_expires_at = NULL,
			lease_expires_at_ns = NULL,
			picked_up_at = NULL,
			assignment_attempt_id = NULL,
			attempt = attempt + ?
		WHERE run_id = ?
			AND status = 'in_progress'
			AND (? = '' OR id = ?)
			AND (? = '' OR COALESCE(assignment_attempt_id, '') = ?)
	`, attemptIncrement, runID, jobID, jobID, attemptID, attemptID)
	if err != nil {
		return err
	}
	// A caller that supplied a job or attempt guard is reconciling a specific
	// assignment, so a row that no longer matches means a newer attempt owns
	// the run — that is a fence, not a missing job.
	noMatch := errors.New("in-progress job not found for retry requeue")
	if jobID != "" || attemptID != "" {
		noMatch = ErrAssignmentFenced
	}
	return expectOneRowErr(result, noMatch)
}

// requeueRunLifecycleTx commits the lifecycle half of an uncertain requeue:
// fence first if the run still claims to be owned, then requeue. It returns the
// state the requeue committed, so a projection appended after it in this
// transaction can name the version the run actually holds rather than reading
// it again.
func requeueRunLifecycleTx(
	ctx context.Context,
	tx *sql.Tx,
	runID string,
	identity runTransitionIdentity,
	fenceExecutionState string,
) (RunState, []Event, error) {
	var events []Event
	fenced, err := applyRunTransitionTx(ctx, tx,
		fenceOwnershipTransitionInTx(runID, identity, fenceExecutionState), nil)
	if err != nil {
		return RunState{}, nil, err
	}
	events = append(events, fenced.Events...)
	requeued, err := applyRunTransitionTx(ctx, tx,
		requeueRecoveringTransitionInTx(runID, identity), nil)
	if err != nil {
		return RunState{}, nil, err
	}
	return requeued.State, append(events, requeued.Events...), nil
}

// requeueUnsentRunLifecycleTx commits the lifecycle half of a requeue whose
// assignment command provably never reached a worker.
//
// It is a separate helper from the uncertain one above because the two are
// answering different questions, not sharing a mechanism. Its callers have
// already proven — by the execution state, the job, and the attempt they
// guarded on — that no executor exists, so the run goes straight back to the
// queue in one transition. Publishing recovering here would announce a phase
// the run was never in, and the second version would make a client reconcile
// through a state that never happened.
func requeueUnsentRunLifecycleTx(
	ctx context.Context,
	tx *sql.Tx,
	runID string,
	identity runTransitionIdentity,
) (RunState, []Event, error) {
	released, err := applyRunTransitionTx(ctx, tx, releaseRunningTransitionInTx(runID, identity), nil)
	if err != nil {
		return RunState{}, nil, err
	}
	return released.State, released.Events, nil
}

func (r *Repository) EnqueueUserMessage(ctx context.Context, input EnqueueUserMessageInput) (EnqueueUserMessageResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	input = normalizeEnqueueUserMessageInput(input)
	if err := requireActiveSessionTx(ctx, tx, input.SessionID); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	fingerprint := ""
	if input.IdempotencyKey != "" {
		fingerprint, err = enqueueRequestFingerprint(input)
		if err != nil {
			return EnqueueUserMessageResult{}, err
		}
		record, found, err := findSendMessageIdempotencyTx(ctx, tx, input.IdempotencyKey)
		if err != nil {
			return EnqueueUserMessageResult{}, err
		}
		if found {
			if record.RequestFingerprint != fingerprint {
				return EnqueueUserMessageResult{}, ErrIdempotencyConflict
			}
			if err := tx.Commit(); err != nil {
				return EnqueueUserMessageResult{}, err
			}
			return record.result(), nil
		}
	}
	result, err := r.enqueueUserMessageTx(ctx, tx, input)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if input.IdempotencyKey != "" {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO send_message_idempotency (
				idempotency_key, session_id, request_fingerprint, user_message_id,
				assistant_message_id, run_id, job_id, trace_id, queued_event_sequence,
				created_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			input.IdempotencyKey,
			input.SessionID,
			fingerprint,
			result.UserMessageID,
			result.AssistantMessageID,
			result.RunID,
			result.JobID,
			result.TraceID,
			result.QueuedEvent.Sequence,
			result.QueuedEvent.CreatedAt,
		); err != nil {
			return EnqueueUserMessageResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	return result, nil
}

type resolvedEnqueueRoute struct {
	requirements      RoutingRequirements
	routedAgent       ExternalAgent
	externalTarget    *ExternalAgentTarget
	externalAgentName sql.NullString
	externalAgentHost sql.NullString
}

func resolveEnqueueRouteTx(ctx context.Context, tx *sql.Tx, input EnqueueUserMessageInput) (resolvedEnqueueRoute, error) {
	model := input.Model
	if input.ExecutionModel != "" {
		model = input.ExecutionModel
	}
	resolved := resolvedEnqueueRoute{requirements: RoutingRequirements{
		AgentID:                        input.AgentID,
		ModelProvider:                  input.ModelProvider,
		Model:                          model,
		RequestedTools:                 append([]string(nil), input.RequestedTools...),
		SelectedTools:                  append([]string(nil), input.SelectedTools...),
		RequiredContextTokens:          input.RequiredContextTokens,
		MinimumWorkerMaxConcurrentRuns: input.MinimumWorkerMaxConcurrentRuns,
	}}
	routedAgent, routed, err := sessionExternalAgentTx(ctx, tx, input.SessionID)
	if err != nil {
		return resolvedEnqueueRoute{}, err
	}
	if !routed {
		return resolved, nil
	}
	resolved.requirements.ModelProvider = "openai_compatible"
	resolved.requirements.Model = routedAgent.Model
	resolved.requirements.ExternalAgent = true
	resolved.requirements.ExternalAgentCredentialRef = routedAgent.CredentialRef
	resolved.routedAgent = routedAgent
	canonicalEndpoint, err := backendegress.ParseKeyedEndpoint(routedAgent.BaseURL)
	if err != nil {
		return resolvedEnqueueRoute{}, ErrExternalAgentBaseURLInsecure
	}
	resolved.externalTarget = &ExternalAgentTarget{
		AgentID:       routedAgent.AgentID,
		DisplayName:   routedAgent.DisplayName,
		BaseURL:       canonicalEndpoint.Canonical,
		CredentialRef: routedAgent.CredentialRef,
	}
	resolved.externalAgentName = sql.NullString{String: routedAgent.DisplayName, Valid: true}
	resolved.externalAgentHost = sql.NullString{String: canonicalEndpoint.Host, Valid: true}
	return resolved, nil
}

// enqueueUserMessageTx is the whole of "a message arrives and a run is
// queued", minus the transaction around it. It is separate so the scheduler
// can claim a due automation and queue its run in ONE transaction: a crash
// between advancing the schedule and creating the run would otherwise either
// lose a run or fire the same one twice.
func (r *Repository) enqueueUserMessageTx(ctx context.Context, tx *sql.Tx, input EnqueueUserMessageInput) (EnqueueUserMessageResult, error) {
	input = normalizeEnqueueUserMessageInput(input)
	if err := requireActiveSessionTx(ctx, tx, input.SessionID); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	// Resolve the effective destination before writing anything. A conversation
	// routed to an external agent overrides request provider/model fields, and
	// routing validation must evaluate that same frozen destination.
	resolvedRoute, err := resolveEnqueueRouteTx(ctx, tx, input)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if input.ValidateRouting != nil {
		err := input.ValidateRouting(ctx, resolvedRoute.requirements)
		if err != nil {
			return EnqueueUserMessageResult{}, err
		}
	}
	modelProvider, model := resolvedRoute.requirements.ModelProvider, resolvedRoute.requirements.Model
	egressDecision, err := normalizePendingEgressDecision(input.EgressDecision)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if modelProvider == "openai_compatible" && egressDecision == nil {
		return EnqueueUserMessageResult{}, ErrRemoteEgressConsentRequired
	}
	if modelProvider != "openai_compatible" && egressDecision != nil &&
		len(egressDecision.RemoteMCPServers) == 0 {
		return EnqueueUserMessageResult{}, ErrLocalEgressDecisionForbidden
	}
	if egressDecision != nil &&
		(egressDecision.Provider != modelProvider || egressDecision.Model != model) {
		return EnqueueUserMessageResult{}, ErrEgressDecisionInvalid
	}
	if egressDecision != nil {
		if resolvedRoute.requirements.ExternalAgent {
			if egressDecision.ExternalAgentID != resolvedRoute.routedAgent.AgentID ||
				egressDecision.ExternalCredentialRefHash != backendegress.HashCredentialReference(resolvedRoute.routedAgent.CredentialRef) ||
				egressDecision.Endpoint != resolvedRoute.externalTarget.BaseURL {
				return EnqueueUserMessageResult{}, ErrEgressDecisionInvalid
			}
		} else if egressDecision.ExternalAgentID != "" ||
			egressDecision.ExternalCredentialRefHash != "" {
			return EnqueueUserMessageResult{}, ErrEgressDecisionInvalid
		}
	}

	created := time.Now().UTC()
	userMessageID := ids.New("msg")
	assistantMessageID := ids.New("msg")
	runID := ids.New("run")
	jobID := ids.New("job")
	traceID := ids.New("trace")
	var next int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM messages WHERE session_id = ?`, input.SessionID).Scan(&next); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	var latestCreatedAt string
	latestQuery := `SELECT created_at FROM messages WHERE session_id = ? ORDER BY ` + sqliteTimestampNanos("created_at") + ` DESC, id DESC LIMIT 1`
	err = tx.QueryRowContext(ctx, latestQuery, input.SessionID).Scan(&latestCreatedAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return EnqueueUserMessageResult{}, err
	}
	var timestampAnchors []time.Time
	if latestCreatedAt != "" {
		latest, parseErr := time.Parse(time.RFC3339Nano, latestCreatedAt)
		if parseErr != nil {
			return EnqueueUserMessageResult{}, parseErr
		}
		timestampAnchors = append(timestampAnchors, latest)
	}
	created, err = nextSessionActivityTimeTx(ctx, tx, input.SessionID, created, timestampAnchors...)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	createdAt := FormatTimestamp(created)
	assistantCreatedAt := FormatTimestamp(created.Add(time.Nanosecond))
	derivedTitle := DeriveSessionTitle(input.Content)
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at) VALUES (?, ?, 'user', ?, ?, ?, ?)`, userMessageID, input.SessionID, input.Content, input.ContentType, next, createdAt); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at) VALUES (?, ?, ?, 'assistant', '', ?, ?, ?)`, assistantMessageID, input.SessionID, runID, input.ContentType, next+1, assistantCreatedAt); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	// Recorded on the run itself, not left to be derived later from
	// session_external_agent. That table says where the conversation points
	// NOW; re-pointing or deleting an agent afterwards would silently rewrite
	// the history of where earlier messages were actually sent, and "what left
	// this machine" is exactly the record that must not be rewritable.
	//
	// The host rather than the base URL: a URL can carry a path, a query, and
	// from a careless paste a credential. Only the recipient is worth keeping.
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_runs (id, session_id, user_message_id, assistant_message_id, agent_id, trace_id, status, model_provider, model_name, external_agent_name, external_agent_host, created_at, state_version, state_updated_at, outcome_reason, assistant_content_sha256) VALUES (?, ?, ?, ?, ?, ?, 'queued', ?, ?, ?, ?, ?, 1, ?, 'none', ?)`, runID, input.SessionID, userMessageID, assistantMessageID, input.AgentID, traceID, modelProvider, model, resolvedRoute.externalAgentName, resolvedRoute.externalAgentHost, createdAt, createdAt, emptyAssistantContentSHA256); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	// Enqueue is the only writer that creates the circular run/message link, so
	// it is the only place that can prove both directions agree. The rows are
	// read back and validated here — after both exist and before the job and
	// the queued event depend on them — rather than trusted because this
	// function wrote them: a half-written link would otherwise become the
	// history every later reader joins on.
	queuedRow, err := readRunRow(ctx, tx, runID)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if err := validateRunCorrelationLink(queuedRow.link()); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	var storedEgressDecision *RunEgressDecision
	if egressDecision != nil {
		decision, insertErr := insertRunEgressDecisionTx(ctx, tx, runID, egressDecision)
		if insertErr != nil {
			return EnqueueUserMessageResult{}, insertErr
		}
		storedEgressDecision = &decision
	}
	// Name the conversation after the first thing said in it, and mark the
	// session as touched so the client's most-recent-first list actually
	// reflects activity rather than creation order.
	//
	// The untitled check lives in the statement rather than in a read followed
	// by a write. SetMaxOpenConns(1) serialises writers today, so a read-modify
	// -write would also be correct — but it would be correct only because of a
	// pool setting made elsewhere for another reason. This form does not care.
	if _, err := tx.ExecContext(ctx, `
		UPDATE sessions
		SET title = CASE
				WHEN title_origin = 'unset' AND ? <> ''
					THEN ?
				ELSE title
			END,
			title_origin = CASE
				WHEN title_origin = 'unset' AND ? <> ''
					THEN 'derived'
				ELSE title_origin
			END,
			updated_at = ?
		WHERE id = ?
	`, derivedTitle, derivedTitle, derivedTitle, createdAt, input.SessionID); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	var sessionTitle, sessionStatus string
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(title, ''), status FROM sessions WHERE id = ?`,
		input.SessionID,
	).Scan(&sessionTitle, &sessionStatus); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	sessionUpdatedPayload, err := json.Marshal(map[string]string{
		"title":     sessionTitle,
		"status":    sessionStatus,
		"updatedAt": createdAt,
	})
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	sessionUpdatedEvent, err := appendSessionEventTx(ctx, tx, input.SessionID, traceID, "session.updated", string(sessionUpdatedPayload), createdAt)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	// Frozen into the payload rather than read when the job is claimed: a skill
	// edited, disabled, or re-granted while the job waits must not change the
	// metadata and content this run was offered. It is the same reason userText
	// is stored here.
	skillSnapshots, err := r.enabledSkillSnapshotsTx(ctx, tx)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if egressDecision != nil {
		actualSkillFingerprint, fingerprintErr := skillSnapshotFingerprint(skillSnapshots)
		if fingerprintErr != nil {
			return EnqueueUserMessageResult{}, fingerprintErr
		}
		if actualSkillFingerprint != egressDecision.SkillSnapshotFingerprint {
			return EnqueueUserMessageResult{}, ErrEgressDecisionInvalid
		}
	}
	jobPayload, err := json.Marshal(map[string]any{
		"userText":                       input.Content,
		"sessionId":                      input.SessionID,
		"userMessageId":                  userMessageID,
		"assistantMessageId":             assistantMessageID,
		"traceId":                        traceID,
		"modelProvider":                  modelProvider,
		"model":                          model,
		"requestedTools":                 input.RequestedTools,
		"requiredContextTokens":          input.RequiredContextTokens,
		"minimumWorkerMaxConcurrentRuns": input.MinimumWorkerMaxConcurrentRuns,
		"skills":                         skillSnapshots,
		// Frozen for the same reason the skills are: re-pointing or deleting
		// the agent while this job waits must not redirect a message the user
		// already sent, and must not send it to a company they did not pick.
		"externalAgent":  resolvedRoute.externalTarget,
		"egressDecision": storedEgressDecision,
		"selectedTools":  egressDecisionSelectedTools(egressDecision, input.SelectedTools),
	})
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO jobs (id, run_id, agent_id, status, payload_json, created_at, created_at_ns) VALUES (?, ?, ?, 'pending', ?, ?, ?)`, jobID, runID, input.AgentID, string(jobPayload), createdAt, created.UnixNano()); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	queuedPayload, err := marshalRunStatePayload(map[string]any{
		"runId":   runID,
		"jobId":   jobID,
		"status":  "queued",
		"agentId": input.AgentID,
	}, queuedRow.state())
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	queuedEvent, err := appendRunEventTx(ctx, tx, input.SessionID, runID, traceID, "agent.run.queued", queuedPayload, createdAt)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	// Written into the transcript, in the same transaction that accepts the
	// message, because a conversation that leaves this machine has to say so
	// where it happens rather than only in a settings screen the user is not
	// looking at. Ordered after the queued event so it lands inside the run
	// the sender is already streaming.
	//
	// Worded prospectively, and that is not a stylistic choice. Nothing has
	// left the machine at this point — the job is only queued — and several
	// designed paths mean it never will: a missing API key or an unwired
	// runtime fails the run with external_agent_unavailable, a debug tool
	// command is answered locally before a provider is ever chosen, and a
	// cancelled or unclaimed run never reaches a vendor at all. "Sent … left
	// your machine" would be a false statement in the transcript on every one
	// of those, which this project ranks as the worst kind of defect. The notice
	// below stays conditional on the run actually reaching the provider.
	var routingEvents []Event
	if storedEgressDecision != nil {
		destinations := make([]string, 0, 1+len(storedEgressDecision.RemoteMCPServers))
		if storedEgressDecision.EndpointHost != "" {
			destinations = append(destinations, storedEgressDecision.EndpointHost)
		}
		for _, remoteMCP := range storedEgressDecision.RemoteMCPServers {
			if !slices.Contains(destinations, remoteMCP.EndpointHost) {
				destinations = append(destinations, remoteMCP.EndpointHost)
			}
		}
		destination := strings.Join(destinations, ", ")
		displayCategories := make([]string, len(storedEgressDecision.DataCategories))
		for index, category := range storedEgressDecision.DataCategories {
			displayCategories[index] = egressCategoryLabel(category)
		}
		notice, err := appendRunNoticeTx(ctx, tx, input.SessionID, runID, traceID,
			"Sending to "+destination+" — disclosed data categories: "+
				strings.Join(displayCategories, ", ")+
				". Data leaves your machine if this run reaches a remote destination",
			map[string]any{
				"provider":        storedEgressDecision.Provider,
				"endpoint":        destination,
				"model":           storedEgressDecision.Model,
				"dataCategories":  storedEgressDecision.DataCategories,
				"decisionVersion": storedEgressDecision.Version,
			}, createdAt)
		if err != nil {
			return EnqueueUserMessageResult{}, err
		}
		routingEvents = append(routingEvents, notice)
		if err := recordAuditTx(ctx, tx, runID, "client", "", "egress.consent.recorded", runID,
			auditPayload(map[string]any{
				"provider":         storedEgressDecision.Provider,
				"endpointHost":     destination,
				"dataCategories":   storedEgressDecision.DataCategories,
				"decisionVersion":  storedEgressDecision.Version,
				"consentGrantedAt": storedEgressDecision.ConsentGrantedAt,
			})); err != nil {
			return EnqueueUserMessageResult{}, err
		}
	}
	return EnqueueUserMessageResult{SessionID: input.SessionID, UserMessageID: userMessageID, AssistantMessageID: assistantMessageID, RunID: runID, JobID: jobID, TraceID: traceID, Status: "queued", SessionUpdatedEvent: sessionUpdatedEvent, QueuedEvent: queuedEvent, RoutingEvents: routingEvents}, nil
}

func egressCategoryLabel(category string) string {
	switch category {
	case "EGRESS_DATA_CATEGORY_CURRENT_MESSAGE":
		return "Current message"
	case "EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY":
		return "Conversation history"
	case "EGRESS_DATA_CATEGORY_CROSS_SESSION_RECALL":
		return "Cross-session recall"
	case "EGRESS_DATA_CATEGORY_MEMORY_PROFILE":
		return "Memory and profile"
	case "EGRESS_DATA_CATEGORY_SKILL_CONTENT":
		return "Enabled skill content"
	case "EGRESS_DATA_CATEGORY_TOOL_SCHEMAS":
		return "Tool schemas"
	case "EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS":
		return "Tool arguments"
	case "EGRESS_DATA_CATEGORY_TOOL_RESULTS":
		return "Tool results"
	case "EGRESS_DATA_CATEGORY_ATTACHMENTS":
		return "Attachments"
	default:
		return "Unknown data category"
	}
}

func normalizeEnqueueUserMessageInput(input EnqueueUserMessageInput) EnqueueUserMessageInput {
	if input.ContentType == "" {
		input.ContentType = "text"
	}
	input.RequestedTools = append([]string{}, input.RequestedTools...)
	sort.Strings(input.RequestedTools)
	input.RequestedTools = slices.Compact(input.RequestedTools)
	input.SelectedTools = append([]string{}, input.SelectedTools...)
	sort.Strings(input.SelectedTools)
	input.SelectedTools = slices.Compact(input.SelectedTools)
	input.EgressDecision = clonePendingEgressDecision(input.EgressDecision)
	return input
}

func (r *Repository) ClaimNextJob(ctx context.Context, agentID string, leaseOwner string) (Job, error) {
	return r.ClaimNextJobWithLimit(ctx, agentID, leaseOwner, 0, 5*time.Minute)
}

func (r *Repository) ClaimNextJobWithLimit(ctx context.Context, agentID string, leaseOwner string, globalLimit int, leaseDuration time.Duration) (Job, error) {
	return r.ClaimNextCompatibleJobWithLimit(ctx, agentID, leaseOwner, globalLimit, leaseDuration, nil, nil)
}

func (r *Repository) ClaimNextCompatibleJobWithLimit(
	ctx context.Context,
	agentID string,
	leaseOwner string,
	globalLimit int,
	leaseDuration time.Duration,
	capabilities *WorkerRoutingCapabilities,
	compatible func(RoutingRequirements) bool,
) (Job, error) {
	pickedUpAt := now()
	if leaseDuration <= 0 {
		leaseDuration = 5 * time.Minute
	}
	leaseExpires := time.Now().UTC().Add(leaseDuration)
	leaseExpiresAt := FormatTimestamp(leaseExpires)
	leaseExpiresAtNanos := leaseExpires.UnixNano()
	assignmentAttemptID := ids.New("attempt")
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Job{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if globalLimit > 0 {
		var active int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM agent_runs
			WHERE agent_id = ?
				AND execution_active = 1
		`, agentID).Scan(&active); err != nil {
			return Job{}, err
		}
		if active >= globalLimit {
			return Job{}, nil
		}
	}
	routingFilter, routingArgs := claimRoutingFilterSQL(capabilities)
	claimQuery := `
		SELECT
			j.id,
			j.run_id,
			r.session_id,
			r.user_message_id,
			COALESCE(r.assistant_message_id, ''),
			j.agent_id,
			r.trace_id,
			r.model_provider,
			r.model_name,
			j.payload_json,
			j.attempt
		FROM jobs j
		JOIN agent_runs r ON r.id = j.run_id
		WHERE j.agent_id = ? AND j.status = 'pending' AND r.status = 'queued'
			-- Turn order is anchored on the user message that initiated each
			-- run, because that is the one message every run is guaranteed to
			-- have. Anchoring on the assistant message instead would drop a run
			-- whose assistant link is missing or unusable out of this subquery
			-- entirely, so a run that is still active on paper would stop
			-- counting as a blocker and later work in the same conversation
			-- would be dispatched around it.
			AND NOT EXISTS (
				SELECT 1
				FROM agent_runs earlier
				JOIN messages earlier_user ON earlier_user.id = earlier.user_message_id
				JOIN messages candidate_user ON candidate_user.id = r.user_message_id
				WHERE earlier.session_id = r.session_id
					AND earlier_user.sequence < candidate_user.sequence
					AND (
						earlier.execution_active = 1
						OR
						earlier.status NOT IN ('completed', 'failed', 'cancelled')
						OR EXISTS (
							SELECT 1
							FROM jobs earlier_job
							WHERE earlier_job.run_id = earlier.id
								AND earlier_job.status NOT IN ('completed', 'failed', 'cancelled')
						)
					)
			)` + routingFilter + `
		ORDER BY j.created_at_ns, j.id
		LIMIT 1
	`
	queryArgs := []any{agentID}
	queryArgs = append(queryArgs, routingArgs...)
	rows, err := tx.QueryContext(ctx, claimQuery, queryArgs...)
	if err != nil {
		return Job{}, err
	}
	defer func() { _ = rows.Close() }()

	var job Job
	for rows.Next() {
		var candidate Job
		var payloadJSON string
		if err := rows.Scan(
			&candidate.JobID,
			&candidate.RunID,
			&candidate.SessionID,
			&candidate.UserMessageID,
			&candidate.AssistantMessageID,
			&candidate.AgentID,
			&candidate.TraceID,
			&candidate.ModelProvider,
			&candidate.Model,
			&payloadJSON,
			&candidate.Attempt,
		); err != nil {
			return Job{}, err
		}
		var payload queuedJobPayload
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return Job{}, err
		}
		candidate.UserText = payload.UserText
		candidate.RequestedTools = payload.RequestedTools
		candidate.RequiredContextTokens = payload.RequiredContextTokens
		candidate.MinimumWorkerMaxConcurrentRuns = payload.MinimumWorkerMaxConcurrentRuns
		// Absent for jobs enqueued before skills existed, which decodes to nil —
		// the same as a conversation with none attached.
		candidate.Skills = payload.Skills
		// Absent for every job enqueued before routing existed, and for every
		// conversation that was never routed away. nil means the local assistant,
		// which is the only default that keeps the transcript on this machine.
		candidate.ExternalAgent = payload.ExternalAgent
		candidate.EgressDecision = payload.EgressDecision
		candidate.SelectedTools = payload.SelectedTools
		externalAgentCredentialRef := ""
		if candidate.ExternalAgent != nil {
			externalAgentCredentialRef = candidate.ExternalAgent.CredentialRef
		}
		if compatible != nil && !compatible(RoutingRequirements{
			AgentID:                        candidate.AgentID,
			ModelProvider:                  candidate.ModelProvider,
			Model:                          candidate.Model,
			RequestedTools:                 candidate.RequestedTools,
			SelectedTools:                  candidate.SelectedTools,
			RequiredContextTokens:          candidate.RequiredContextTokens,
			MinimumWorkerMaxConcurrentRuns: candidate.MinimumWorkerMaxConcurrentRuns,
			ExternalAgent:                  candidate.ExternalAgent != nil,
			ExternalAgentCredentialRef:     externalAgentCredentialRef,
		}) {
			continue
		}
		job = candidate
		break
	}
	if err := rows.Err(); err != nil {
		return Job{}, err
	}
	if err := rows.Close(); err != nil {
		return Job{}, err
	}
	if job.JobID == "" {
		return Job{}, nil
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'in_progress', lease_owner = ?, lease_expires_at = ?, lease_expires_at_ns = ?, picked_up_at = ?, assignment_attempt_id = ?
		WHERE id = ? AND status = 'pending'
	`, leaseOwner, leaseExpiresAt, leaseExpiresAtNanos, pickedUpAt, assignmentAttemptID, job.JobID)
	if err != nil {
		return Job{}, err
	}
	if err := expectOneRow(result, "pending job not found for claim"); err != nil {
		return Job{}, err
	}
	// agent.run.started IS the lifecycle event for queued -> running, so the
	// claim commits the version and the projection together instead of writing
	// the status here and announcing it separately.
	started, err := applyRunTransitionTx(ctx, tx, runTransition{
		runID:            job.RunID,
		expectedVersion:  unresolvedStateVersion,
		transactionLocal: true,
		allowedFrom:      []string{lifecycleQueued},
		to:               lifecycleRunning,
		reason:           runoutcome.ReasonNone,
		extraSet: `started_at = COALESCE(started_at, ?),
			worker_id = ?,
			execution_active = 1,
			execution_exit_acknowledged_at = NULL,
			execution_attempt_id = ?,
			execution_state = 'pending_send',
			execution_lease_expires_at = ?,
			execution_lease_expires_at_ns = ?`,
		extraArgs: []any{pickedUpAt, leaseOwner, assignmentAttemptID, leaseExpiresAt, leaseExpiresAtNanos},
		eventType: "agent.run.started",
		eventPayload: map[string]any{
			"runId":               job.RunID,
			"jobId":               job.JobID,
			"status":              "running",
			"agentId":             agentID,
			"attempt":             job.Attempt,
			"assignmentAttemptId": assignmentAttemptID,
		},
	}, nil)
	if err != nil {
		if errors.Is(err, ErrRunTransitionConflict) {
			return Job{}, errors.New("run is not queued")
		}
		return Job{}, err
	}
	// A duplicate here would mean the run was already running while its job was
	// still pending. The claim query and the job update rule that out today, so
	// this is a guard rather than a handled case — but indexing an empty slice
	// would turn a future inconsistency into a panic instead of a conflict.
	if started.Duplicate || len(started.Events) != 1 {
		return Job{}, errors.New("run is not queued")
	}
	job.AssignmentAttemptID = assignmentAttemptID
	job.ExpectedStateVersion = started.State.StateVersion
	job.StartedEvent = started.Events[0]
	if err := tx.Commit(); err != nil {
		return Job{}, err
	}
	return job, nil
}

func claimRoutingFilterSQL(capabilities *WorkerRoutingCapabilities) (string, []any) {
	if capabilities == nil {
		return "", nil
	}
	const externalAgentType = `json_type(j.payload_json, '$.externalAgent')`
	var destinations []string
	var args []any
	egressAware := capabilities.RemoteEgressDecisionVersion >= RunEgressDecisionVersion
	if egressAware && len(capabilities.ExternalAgentCredentialRefs) > 0 {
		placeholders := make([]string, len(capabilities.ExternalAgentCredentialRefs))
		for index, credentialRef := range capabilities.ExternalAgentCredentialRefs {
			placeholders[index] = "?"
			args = append(args, credentialRef)
		}
		destinations = append(destinations, `(
			`+externalAgentType+` = 'object'
			AND json_extract(j.payload_json, '$.externalAgent.credentialRef') IN (`+strings.Join(placeholders, ", ")+`)
			AND COALESCE(CAST(json_extract(j.payload_json, '$.requiredContextTokens') AS INTEGER), 0) <= 0
		)`)
	}
	for _, model := range capabilities.Models {
		if model.Provider == "openai_compatible" && !egressAware {
			continue
		}
		destinations = append(destinations, `(
			COALESCE(`+externalAgentType+`, 'null') <> 'object'
			AND r.model_provider = ?
			AND r.model_name = ?
			AND COALESCE(CAST(json_extract(j.payload_json, '$.requiredContextTokens') AS INTEGER), 0) <= ?
		)`)
		args = append(args, model.Provider, model.Model, model.MaxContextTokens)
	}
	if len(destinations) == 0 {
		destinations = append(destinations, "0")
	}
	filter := `
		AND (` + strings.Join(destinations, " OR ") + `)
		AND MAX(
			COALESCE(CAST(json_extract(j.payload_json, '$.minimumWorkerMaxConcurrentRuns') AS INTEGER), 0),
			1
		) <= ?`
	args = append(args, capabilities.MaxConcurrentRuns)
	if len(capabilities.Tools) == 0 {
		filter += `
		AND NOT EXISTS (
			SELECT 1
			FROM json_each(COALESCE(json_extract(j.payload_json, '$.requestedTools'), json('[]')))
		)
		AND NOT EXISTS (
			SELECT 1
			FROM json_each(COALESCE(json_extract(j.payload_json, '$.selectedTools'), json('[]')))
		)`
		return filter, args
	}
	placeholders := make([]string, len(capabilities.Tools))
	for index, tool := range capabilities.Tools {
		placeholders[index] = "?"
		args = append(args, tool)
	}
	filter += `
		AND NOT EXISTS (
			SELECT 1
			FROM json_each(COALESCE(json_extract(j.payload_json, '$.requestedTools'), json('[]'))) requested_tool
			WHERE requested_tool.value NOT IN (` + strings.Join(placeholders, ", ") + `)
		)
		AND NOT EXISTS (
			SELECT 1
			FROM json_each(COALESCE(json_extract(j.payload_json, '$.selectedTools'), json('[]'))) selected_tool
			WHERE selected_tool.value NOT IN (` + strings.Join(placeholders, ", ") + `)
		)`
	for _, tool := range capabilities.Tools {
		args = append(args, tool)
	}
	return filter, args
}

func (r *Repository) ListPendingRoutingWorkPage(
	ctx context.Context,
	after PendingRoutingCursor,
	limit int,
) ([]PendingRoutingWork, PendingRoutingCursor, error) {
	if limit < 1 {
		return nil, after, fmt.Errorf("pending routing page limit must be positive")
	}
	query := `
		SELECT j.id, j.created_at_ns, j.run_id, j.agent_id, r.model_provider, r.model_name, j.payload_json
		FROM jobs j
		JOIN agent_runs r ON r.id = j.run_id
		WHERE j.status = 'pending' AND r.status = 'queued'`
	args := make([]any, 0, 3)
	if after.JobID != "" {
		query += `
			AND (j.created_at_ns > ? OR (j.created_at_ns = ? AND j.id > ?))`
		args = append(args, after.CreatedAtNanos, after.CreatedAtNanos, after.JobID)
	}
	query += `
		ORDER BY j.created_at_ns, j.id
		LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, after, err
	}
	defer func() { _ = rows.Close() }()

	var work []PendingRoutingWork
	next := after
	for rows.Next() {
		var item PendingRoutingWork
		var payloadJSON string
		if err := rows.Scan(
			&next.JobID,
			&next.CreatedAtNanos,
			&item.RunID,
			&item.Requirements.AgentID,
			&item.Requirements.ModelProvider,
			&item.Requirements.Model,
			&payloadJSON,
		); err != nil {
			return nil, after, err
		}
		var payload queuedJobPayload
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			return nil, after, err
		}
		item.Requirements.RequestedTools = append([]string(nil), payload.RequestedTools...)
		item.Requirements.SelectedTools = append([]string(nil), payload.SelectedTools...)
		item.Requirements.RequiredContextTokens = payload.RequiredContextTokens
		item.Requirements.MinimumWorkerMaxConcurrentRuns = payload.MinimumWorkerMaxConcurrentRuns
		item.Requirements.ExternalAgent = payload.ExternalAgent != nil
		if payload.ExternalAgent != nil {
			item.Requirements.ExternalAgentCredentialRef = payload.ExternalAgent.CredentialRef
		}
		work = append(work, item)
	}
	if err := rows.Err(); err != nil {
		return nil, after, err
	}
	return work, next, nil
}

func (r *Repository) RequeueClaimedJob(ctx context.Context, jobID string, runID string) error {
	return r.requeueAssignment(ctx, Assignment{JobID: jobID, RunID: runID}, "", true)
}

func (r *Repository) AbortPendingAssignment(ctx context.Context, assignment Assignment) error {
	return r.requeueAssignment(ctx, assignment, "pending_send", false)
}

// AbortUnsentAssignment is valid only after the delivery caller has proved no
// stream.Send goroutine was started. The stored "sending" state fences the
// assignment identity; by itself it does not prove the command was unsent.
func (r *Repository) AbortUnsentAssignment(ctx context.Context, assignment Assignment) error {
	return r.requeueAssignment(ctx, assignment, "sending", false)
}

func (r *Repository) requeueAssignment(
	ctx context.Context,
	assignment Assignment,
	requiredExecutionState string,
	incrementAttempt bool,
) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var attemptID, executionState string
	var workerID sql.NullString
	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(execution_attempt_id, ''), execution_state, worker_id
		FROM agent_runs
		WHERE id = ?
	`, assignment.RunID).Scan(&attemptID, &executionState, &workerID)
	if err != nil {
		return err
	}
	if assignment.AttemptID != "" && attemptID != assignment.AttemptID {
		return ErrAssignmentFenced
	}
	if assignment.WorkerID != "" && (!workerID.Valid || workerID.String != assignment.WorkerID) {
		return ErrAssignmentFenced
	}
	if requiredExecutionState != "" && executionState != requiredExecutionState {
		return ErrAssignmentFenced
	}
	if requiredExecutionState == "" && (executionState == "sending" || executionState == "uncertain") {
		return ErrAssignmentFenced
	}
	attemptIncrement := 0
	if incrementAttempt {
		attemptIncrement = 1
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE jobs
		SET status = 'pending',
			lease_owner = NULL,
			lease_expires_at = NULL,
			lease_expires_at_ns = NULL,
			picked_up_at = NULL,
			assignment_attempt_id = NULL,
			attempt = attempt + ?
		WHERE id = ? AND run_id = ? AND status = 'in_progress'
	`, attemptIncrement, assignment.JobID, assignment.RunID)
	if err != nil {
		return err
	}
	if err := expectOneRowErr(result, ErrAssignmentFenced); err != nil {
		return err
	}
	// Both the job and the run move here, and this signature returns only an
	// error, so its three callers publish nothing live: a client that is
	// watching sees the requeue when it replays, not when it happens. Widening
	// these three signatures is the streaming task's work, and doing it here
	// would change public shapes that task has to change anyway. What this
	// boundary owes today is that the events exist, in order, on commit.
	//
	// An assignment aborted before send provably never reached a worker. The
	// pending-send path proves that from the stored state. AbortUnsentAssignment
	// supplies the other proof at its sole call site: sendTracked reported that
	// no stream.Send goroutine was started; its "sending" guard here only fences
	// the same assignment. There is no executor to be uncertain about, so the
	// run goes straight back to the queue rather than announcing a recovery it
	// is not having.
	//
	// RequeueClaimedJob pins no delivery state, worker, or attempt, so
	// transaction-observed pending_send is the only state that proves no send
	// for that API. Once its assignment has been delivered — or fenced, or
	// landed in any state past the point of no send — there may be an executor
	// holding the run, and the honest edge is the uncertain one that publishes
	// recovering before queued.
	if requiredExecutionState == "" && executionState != "pending_send" {
		if _, _, err := requeueRunLifecycleTx(
			ctx, tx, assignment.RunID, assignmentIdentity(assignment), "uncertain",
		); err != nil {
			return mapRequeueConflict(err)
		}
		return tx.Commit()
	}
	if _, _, err := requeueUnsentRunLifecycleTx(ctx, tx, assignment.RunID, assignmentIdentity(assignment)); err != nil {
		if errors.Is(err, ErrRunTransitionConflict) {
			return ErrAssignmentFenced
		}
		return err
	}
	return tx.Commit()
}

func (r *Repository) BeginAssignmentSend(ctx context.Context, assignment Assignment) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_state = 'sending'
		WHERE id = ?
			AND worker_id = ?
			AND execution_attempt_id = ?
			AND execution_active = 1
			AND execution_state = 'pending_send'
			AND status = 'running'
			AND EXISTS (
				SELECT 1
				FROM jobs j
				WHERE j.id = ?
					AND j.run_id = agent_runs.id
					AND j.status = 'in_progress'
					AND j.lease_owner = ?
					AND j.assignment_attempt_id = ?
			)
	`, assignment.RunID, assignment.WorkerID, assignment.AttemptID, assignment.JobID, assignment.WorkerID, assignment.AttemptID)
	if err != nil {
		return err
	}
	return expectOneRowErr(result, ErrAssignmentFenced)
}

func (r *Repository) MarkAssignmentDelivered(ctx context.Context, assignment Assignment) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_state = 'delivered'
		WHERE id = ?
			AND worker_id = ?
			AND execution_attempt_id = ?
			AND execution_active = 1
			AND execution_state = 'sending'
	`, assignment.RunID, assignment.WorkerID, assignment.AttemptID)
	if err != nil {
		return err
	}
	return expectOneRowErr(result, ErrAssignmentFenced)
}

func (r *Repository) MarkAssignmentDeliveryUncertain(ctx context.Context, assignment Assignment) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_state = 'uncertain'
		WHERE id = ?
			AND worker_id = ?
			AND execution_attempt_id = ?
			AND execution_active = 1
			AND execution_state IN ('sending', 'delivered')
	`, assignment.RunID, assignment.WorkerID, assignment.AttemptID)
	if err != nil {
		return err
	}
	return expectOneRowErr(result, ErrAssignmentFenced)
}
