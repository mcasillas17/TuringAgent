package worker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
)

type RuntimeStream interface {
	Send(*turingv1.RuntimeUpdate) error
	Recv() (*turingv1.RuntimeCommand, error)
}

type RuntimeClient interface {
	ConnectWorker(ctx context.Context) (RuntimeStream, error)
}

type ToolRegistryInvalidator interface {
	InvalidateToolRegistry()
}

type Executor interface {
	Execute(ctx context.Context, job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error
}

type BeaconPosterSetter interface {
	SetToolBeaconPoster(func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error))
}

// ApprovalResumerSetter is how the resume handshake reaches the code that
// actually runs approved tools. It is a separate optional interface for the
// same reason the beacon poster is: an executor that does not run tools has
// nothing to resume.
type ApprovalResumerSetter interface {
	SetApprovalResumer(func(context.Context, tools.ApprovalResume) error)
}

type Options struct {
	WorkerID                    string
	AgentID                     turingv1.AgentId
	MaxConcurrentRuns           int
	HeartbeatInterval           time.Duration
	DisconnectCleanupTimeout    time.Duration
	DecisionTombstoneTTL        time.Duration
	UpdateSendTimeout           time.Duration
	Models                      []*turingv1.ModelCapability
	ExternalAgentCredentialRefs []string
	// RemoteEgressDecisionVersion is opt-in: only an executor that validates a
	// frozen RunEgressDecision before provider I/O may advertise it.
	RemoteEgressDecisionVersion int32
	// SupportsExternalAgents mirrors the coarse legacy wire field. Exact
	// routing authorization comes only from ExternalAgentCredentialRefs.
	SupportsExternalAgents bool
	NewRegistrationID      func() (string, error)

	// DiscoverTools enumerates the tools this worker can execute, reported to the
	// orchestrator on every connect. The orchestrator persists the snapshot as
	// its registry and derives policy from it, so a reconnect must re-report
	// rather than leave a stale one behind.
	//
	// Optional: without it the worker reports UNSPECIFIED and the orchestrator
	// applies its compatibility defaults.
	DiscoverTools func(context.Context) ([]*turingv1.DiscoveredTool, error)
}

type Worker struct {
	options  Options
	client   RuntimeClient
	executor Executor
	mu       sync.Mutex
	active   map[string]*activeRun
	// terminalAttempts remembers, for a bounded and recent set of runs, the
	// assignment attempt that already claimed the run's terminal report.
	terminalAttempts map[string]terminalAttempt
	// terminalOrder is the insertion order of terminalAttempts, so the oldest
	// entry is the one evicted when the bound is reached.
	terminalOrder []string
	approvals     map[string]string
	resumes       map[string]*approvalResume
	// unclaimedResumes is the insertion order of resume slots opened by a
	// decision that arrived before the requirement naming it, so the oldest is
	// dropped first if one is never claimed.
	unclaimedResumes []string
	toolCalls        map[string]string
	decisionMu       sync.Mutex
	decisions        map[string][]*decisionWaiter
	generations      map[string]uint64
	writerMu         sync.Mutex
	writer           *outboundWriter
	fatalMu          sync.Mutex
	fatal            chan error
	refreshMu        sync.Mutex
	refreshRunning   bool
	refreshPending   *turingv1.RuntimeMcpRegistryChanged
}

type decisionWaiter struct {
	decision   chan *turingv1.ToolPolicyDecision
	phase      turingv1.ToolCallPhase
	generation uint64
	tombstone  bool
	expiresAt  time.Time
}

// approvalResume is one approval's half-finished lifecycle handshake.
//
// It exists from the moment the worker learns which run an approval belongs to,
// which is earlier than the moment anything waits on it: the decision command
// and the acceptance command both arrive on the command loop and would
// otherwise have nowhere to land while the executor is still between them.
//
// decided is closed when the approved decision command arrives; deadline is the
// instant the wait stops, recorded so it can be read without inferring it from
// elapsed time. Both are guarded by the worker's own lock.
//
// The resume's fate — accepted or abandoned — is a separate, narrower
// decision guarded by its own lock (outcomeMu) rather than the worker's. A
// delivered acceptance and a failing path giving up on the wait can arrive on
// different goroutines at effectively the same instant, and the only way to
// make "acceptance wins" and "abandonment cannot be revived" both true is to
// make the record-or-claim decision a single critical section: whichever side
// reaches outcomeMu first determines the outcome, and the other observes it
// rather than racing it again.
type approvalResume struct {
	runID      string
	decided    chan struct{}
	decideOnce sync.Once
	deadline   time.Time

	// outcomeMu guards outcome, accepted, and readyStarted. It is deliberately
	// its own lock, not the worker's w.mu: deliverResumeAcceptance and
	// resumeApproval's failing paths reach this decision from call sites that
	// each already touch other locks (w.mu, the entry's) on their own way
	// here, just never while holding outcomeMu itself. Giving the resume's
	// outcome a lock that nothing else ever acquires — always taken and
	// released on its own, never nested inside another lock — is what keeps
	// this decision from ever needing a lock order with anything else in the
	// worker.
	outcomeMu sync.Mutex
	outcome   resumeOutcome
	accepted  *turingv1.RuntimeApprovalResumeAccepted
	// readyStarted records that resumeApproval has begun attempting this
	// resume's Ready send. It is set exactly once, by beginReadySend,
	// immediately before resumeApproval attempts to send the Ready — never
	// before the decision has arrived, because beginReadySend is only ever
	// called after resumeApproval's decision select returns. It means only
	// that the local attempt was initiated, not that any bytes reached the
	// transport — the send below can still fail. recordAccepted refuses to
	// commit an acceptance until this is true: the orchestrator cannot have
	// produced a durable acceptance for a Ready this worker had not yet
	// attempted to send, so a stray or premature delivery arriving before
	// this point can never be recorded, no matter what races it here. That
	// is what makes the decision select's own failing branch unable to ever
	// observe a recorded acceptance — the invariant is enforced locally
	// rather than merely assumed.
	readyStarted bool
	// signal is closed exactly once, the instant an acceptance is recorded, so
	// a blocked wait can wake up rather than poll for one. It carries no
	// payload and settles nothing by itself — outcome and accepted, read under
	// outcomeMu, are the only source of truth once it fires. Closing it here
	// needs no sync.Once of its own: recordAccepted is the only place that
	// closes it, and it only ever reaches that close once, on the single call
	// that wins the transition out of resumeOutcomePending under outcomeMu.
	signal chan struct{}
}

// resumeOutcome is the one durable fact a pending resume ever settles into.
type resumeOutcome uint8

const (
	// resumeOutcomePending is the initial state: neither an acceptance nor an
	// abandonment has been committed yet, and either is still possible.
	resumeOutcomePending resumeOutcome = iota
	// resumeOutcomeAccepted means a durable acceptance was recorded before
	// anything abandoned this resume. It is final: nothing moves out of it.
	resumeOutcomeAccepted
	// resumeOutcomeAbandoned means a failing path claimed this resume before
	// any acceptance was recorded. It is also final: a later acceptance is
	// dropped rather than recorded, because reviving a resume whose failure
	// may already be in flight — a terminal report sent, a stream about to
	// drop — would contradict an outcome already being acted on.
	resumeOutcomeAbandoned
)

type activeRun struct {
	// runID is carried on the entry because the terminal-report claim is made
	// through the entry, and what has to be remembered afterwards is which run
	// and attempt made it.
	runID  string
	cancel context.CancelCauseFunc
	done   chan struct{}
	mu     sync.Mutex
	stop   bool
	// attemptID is the assignment this run belongs to. It is what tells a
	// same-attempt refresh from a fenced predecessor's command: only the
	// attempt that still owns the run may move its version.
	attemptID string
	// version is the highest state version the orchestrator has committed for
	// this assignment. It is echoed on terminal reports so the orchestrator can
	// refuse anything computed against a state it has already left.
	version int64
	// paused withholds this run's narration after an update failed to reach the
	// orchestrator.
	paused                bool
	terminalReportClaimed bool
}

const (
	terminalUpdateSendTimeout   = 5 * time.Second
	defaultUpdateSendTimeout    = 5 * time.Second
	defaultHeartbeatInterval    = 30 * time.Second
	defaultDecisionTombstoneTTL = 100 * time.Millisecond
	maxConcurrentRuns           = 128
	// maxRememberedTerminalAttempts bounds the terminal-report memory.
	//
	// The window it has to cover is one round trip: the orchestrator proves
	// ownership and sends a same-attempt refresh while this worker is reporting
	// how the run ended. Only runs that terminalized inside that window can
	// have a refresh still in flight, and a worker runs at most
	// MaxConcurrentRuns of them at a time — so a few hundred entries is orders
	// of magnitude more than the race can produce, while still being a fixed
	// ceiling rather than a promise that something else will clean up.
	maxRememberedTerminalAttempts = 256
	// maxUnclaimedApprovalDecisions bounds the decisions a worker remembers for
	// approvals it has not yet been told the run of. Only a decision queued
	// ahead of its own policy decision can land here, and the gap between them
	// is one command, so a few dozen is far more than the ordering can produce
	// while still being a ceiling rather than a hope.
	maxUnclaimedApprovalDecisions = 64
)

// terminalAttempt is the assignment whose terminal report a run already
// committed to sending.
type terminalAttempt struct {
	attemptID string
}

var errOutboundWriterStopped = errors.New("runtime outbound writer stopped")

// errRunNarrationPaused is returned to a caller that needs an answer from the
// orchestrator on a run whose narration is withheld. It names the condition and
// nothing about the run, the tool, or the arguments, because it travels back
// through the tool runner and out to callers that must not learn any of that.
var errRunNarrationPaused = errors.New("run narration is paused")
var errShutdownRequested = errors.New("runtime shutdown requested")
var errRuntimeDisconnected = errors.New("runtime stream disconnected")

// errApprovalResumeUnacknowledged drops a stream whose approval resume was
// never answered. It names the condition only: the run, the approval, and the
// tool are all things this error travels too far to carry.
var errApprovalResumeUnacknowledged = errors.New("runtime approval resume was not acknowledged")

// ErrInvalidConfig marks the validation failures Run reports before it touches
// the network. They cannot succeed on retry, so a caller looping over Run must
// stop rather than spin on them forever.
var ErrInvalidConfig = errors.New("invalid worker configuration")

type outboundWriter struct {
	stream RuntimeStream
	queue  chan *outboundRequest
	done   chan struct{}
	exited chan struct{}
	once   sync.Once
	mu     sync.Mutex
	err    error
}

type outboundRequest struct {
	ctx     context.Context
	update  *turingv1.RuntimeUpdate
	result  chan error
	started chan struct{}
	mu      sync.Mutex
	state   outboundRequestState
	err     error
}

type outboundRequestState uint8

const (
	outboundRequestQueued outboundRequestState = iota
	outboundRequestStarted
	outboundRequestAbandoned
)

type outboundSendStartedError struct {
	err error
}

func (e outboundSendStartedError) Error() string     { return e.err.Error() }
func (e outboundSendStartedError) Unwrap() error     { return e.err }
func (e outboundSendStartedError) SendStarted() bool { return true }

type outboundSendState interface {
	SendStarted() bool
}

func outboundSendStarted(err error) bool {
	var state outboundSendState
	return errors.As(err, &state) && state.SendStarted()
}

func newOutboundWriter(stream RuntimeStream) *outboundWriter {
	writer := &outboundWriter{
		stream: stream,
		queue:  make(chan *outboundRequest, 64),
		done:   make(chan struct{}),
		exited: make(chan struct{}),
	}
	go writer.run()
	return writer
}

func (w *outboundWriter) run() {
	defer close(w.exited)
	for {
		select {
		case <-w.done:
			return
		case request := <-w.queue:
			select {
			case <-w.done:
				return
			default:
			}
			if err := request.begin(); err != nil {
				request.complete(err)
				continue
			}
			err := w.stream.Send(request.update)
			request.complete(err)
			if err != nil {
				w.stop(err)
				return
			}
		}
	}
}

func (w *outboundWriter) send(ctx context.Context, update *turingv1.RuntimeUpdate) error {
	if ctx == nil {
		ctx = context.Background()
	}
	request := &outboundRequest{ctx: ctx, update: update, result: make(chan error, 1), started: make(chan struct{})}
	select {
	case <-w.done:
		return w.error()
	default:
	}
	select {
	case w.queue <- request:
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		return w.error()
	}
	select {
	case err := <-request.result:
		return request.classify(err)
	case <-ctx.Done():
		return request.cancel(ctx.Err())
	case <-w.done:
		return request.cancel(w.error())
	}
}

func (w *outboundWriter) stop(err error) {
	if err == nil {
		err = errOutboundWriterStopped
	}
	w.once.Do(func() {
		w.mu.Lock()
		w.err = err
		w.mu.Unlock()
		close(w.done)
	})
}

func (w *outboundWriter) error() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err == nil {
		return errOutboundWriterStopped
	}
	return w.err
}

func (r *outboundRequest) complete(err error) {
	select {
	case r.result <- err:
	default:
	}
}

func (r *outboundRequest) begin() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == outboundRequestAbandoned {
		if r.err != nil {
			return r.err
		}
		return errOutboundWriterStopped
	}
	if err := r.ctx.Err(); err != nil {
		r.state = outboundRequestAbandoned
		return err
	}
	r.state = outboundRequestStarted
	close(r.started)
	return nil
}

func (r *outboundRequest) sendStarted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state == outboundRequestStarted
}

func (r *outboundRequest) classify(err error) error {
	if err != nil && r.sendStarted() {
		return outboundSendStartedError{err: err}
	}
	return err
}

func (r *outboundRequest) cancel(err error) error {
	r.mu.Lock()
	if r.state == outboundRequestQueued {
		r.state = outboundRequestAbandoned
		r.err = err
		r.mu.Unlock()
		return err
	}
	started := r.state == outboundRequestStarted
	r.mu.Unlock()
	if started && err != nil {
		return outboundSendStartedError{err: err}
	}
	return err
}

func New(options Options, client RuntimeClient, executor Executor) *Worker {
	if options.WorkerID == "" {
		options.WorkerID = "worker-general-go"
	}
	if options.AgentID == turingv1.AgentId_AGENT_ID_UNSPECIFIED {
		options.AgentID = turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT
	}
	if options.MaxConcurrentRuns <= 0 {
		options.MaxConcurrentRuns = 1
	}
	if options.HeartbeatInterval <= 0 {
		options.HeartbeatInterval = defaultHeartbeatInterval
	}
	if options.DecisionTombstoneTTL <= 0 {
		options.DecisionTombstoneTTL = defaultDecisionTombstoneTTL
	}
	if options.UpdateSendTimeout <= 0 {
		options.UpdateSendTimeout = defaultUpdateSendTimeout
	}
	if options.NewRegistrationID == nil {
		options.NewRegistrationID = newRegistrationID
	}
	options.Models = cloneModelCapabilities(options.Models)
	options.ExternalAgentCredentialRefs = cloneCredentialRefs(options.ExternalAgentCredentialRefs)
	return &Worker{
		options: options, client: client, executor: executor, active: map[string]*activeRun{},
		terminalAttempts: map[string]terminalAttempt{},
		approvals:        map[string]string{}, resumes: map[string]*approvalResume{},
		toolCalls: map[string]string{}, decisions: map[string][]*decisionWaiter{},
		generations: map[string]uint64{},
	}
}

func newRegistrationID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	return "registration_" + hex.EncodeToString(random[:]), nil
}

func cloneModelCapabilities(models []*turingv1.ModelCapability) []*turingv1.ModelCapability {
	cloned := make([]*turingv1.ModelCapability, 0, len(models))
	for _, model := range models {
		if model == nil {
			cloned = append(cloned, nil)
			continue
		}
		cloned = append(cloned, &turingv1.ModelCapability{
			Provider:         model.GetProvider(),
			Model:            model.GetModel(),
			MaxContextTokens: model.GetMaxContextTokens(),
		})
	}
	return cloned
}

func cloneCredentialRefs(refs []string) []string {
	cloned := append([]string(nil), refs...)
	sort.Strings(cloned)
	return cloned
}

func (w *Worker) Run(ctx context.Context) error {
	if w.client == nil {
		return fmt.Errorf("%w: runtime client is required", ErrInvalidConfig)
	}
	if w.executor == nil {
		return fmt.Errorf("%w: executor is required", ErrInvalidConfig)
	}
	if w.options.MaxConcurrentRuns < 1 || w.options.MaxConcurrentRuns > maxConcurrentRuns {
		return fmt.Errorf("%w: max concurrent runs must be between 1 and 128", ErrInvalidConfig)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := w.waitForPriorRuns(ctx); err != nil {
		return fmt.Errorf("wait for prior active runs: %w", err)
	}
	modernRegistration := len(w.options.Models) > 0 ||
		len(w.options.ExternalAgentCredentialRefs) > 0 ||
		w.options.SupportsExternalAgents
	registrationID := ""
	if modernRegistration {
		var err error
		registrationID, err = w.options.NewRegistrationID()
		if err != nil {
			return fmt.Errorf("create worker registration identity: %w", err)
		}
		if registrationID == "" {
			return fmt.Errorf("%w: registration ID is required", ErrInvalidConfig)
		}
	}
	streamCtx, cancelStream := context.WithCancelCause(ctx)
	runCtx, cancelRuns := context.WithCancelCause(context.WithoutCancel(ctx))
	stream, err := w.client.ConnectWorker(streamCtx)
	if err != nil {
		cancelRuns(errRuntimeDisconnected)
		cancelStream(errRuntimeDisconnected)
		return err
	}
	w.startOutboundWriter(stream)
	defer func() {
		entries := w.cancelActiveRuns(errRuntimeDisconnected)
		cancelRuns(errRuntimeDisconnected)
		w.waitForActiveRuns(entries, w.disconnectCleanupTimeout())
		cancelStream(errRuntimeDisconnected)
		w.stopOutboundWriter()
	}()
	fatal := make(chan error, 1)
	w.setFatalChannel(fatal)
	defer w.setFatalChannel(nil)
	if setter, ok := w.executor.(BeaconPosterSetter); ok {
		setter.SetToolBeaconPoster(func(ctx context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			return w.postToolBeacon(ctx, stream, beacon)
		})
	}
	if setter, ok := w.executor.(ApprovalResumerSetter); ok {
		setter.SetApprovalResumer(func(ctx context.Context, resume tools.ApprovalResume) error {
			return w.resumeApproval(ctx, stream, resume)
		})
	}
	ready := &turingv1.RuntimeWorkerReady{
		WorkerId:          w.options.WorkerID,
		AgentId:           w.options.AgentID,
		MaxConcurrentRuns: int32(w.options.MaxConcurrentRuns),
		RegistrationId:    registrationID,
	}
	if w.options.DiscoverTools != nil {
		discovered, err := w.options.DiscoverTools(streamCtx)
		if err != nil {
			// Report the failure rather than registering a snapshot we do not
			// have: the orchestrator rejects a FAILED worker, and the caller's
			// reconnect loop retries — which is what should happen while an MCP
			// server is briefly unreachable. Claiming an empty tool set instead
			// would look authoritative and wipe the orchestrator's registry.
			ready.ToolDiscoveryStatus = turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_FAILED
		} else {
			ready.Tools = discovered
			ready.ToolDiscoveryStatus = turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_COMPLETE
		}
	} else if modernRegistration {
		// A modern capability snapshot is authoritative. Mark an absent discovery
		// callback as a known-empty tool set so older orchestrators do not apply
		// their legacy compatibility tools to this worker.
		ready.ToolDiscoveryStatus = turingv1.ToolDiscoveryStatus_TOOL_DISCOVERY_STATUS_COMPLETE
	}
	if modernRegistration {
		ready.Capabilities = &turingv1.WorkerCapabilities{
			Models:                      cloneModelCapabilities(w.options.Models),
			AgentIds:                    []turingv1.AgentId{w.options.AgentID},
			Tools:                       ready.GetTools(),
			MaxConcurrentRuns:           int32(w.options.MaxConcurrentRuns),
			SupportsExternalAgents:      w.options.SupportsExternalAgents || len(w.options.ExternalAgentCredentialRefs) > 0,
			ExternalAgentCredentialRefs: cloneCredentialRefs(w.options.ExternalAgentCredentialRefs),
			RemoteEgressDecisionVersion: w.options.RemoteEgressDecisionVersion,
		}
	}
	if err := w.send(streamCtx, stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: ready}}); err != nil {
		return err
	}
	type receiveResult struct {
		command *turingv1.RuntimeCommand
		err     error
	}
	received := make(chan receiveResult, 1)
	heartbeat := time.NewTicker(w.options.HeartbeatInterval)
	defer heartbeat.Stop()
	stopReceive := make(chan struct{})
	defer close(stopReceive)
	go func() {
		for {
			command, err := stream.Recv()
			select {
			case received <- receiveResult{command: command, err: err}:
			case <-stopReceive:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-fatal:
			return err
		case <-heartbeat.C:
			sendCtx, cancel := context.WithTimeout(streamCtx, w.options.UpdateSendTimeout)
			err := w.send(sendCtx, stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Heartbeat{Heartbeat: &turingv1.RuntimeHeartbeat{
				WorkerId: w.options.WorkerID,
			}}})
			cancel()
			if err != nil {
				return err
			}
		case result := <-received:
			if result.err != nil {
				return result.err
			}
			if err := w.handleCommand(runCtx, stream, result.command); err != nil {
				if errors.Is(err, errShutdownRequested) {
					return nil
				}

				return err
			}
		}
	}
}

func (w *Worker) cancelActiveRuns(cause error) map[string]*activeRun {
	if cause == nil {
		cause = errRuntimeDisconnected
	}
	w.mu.Lock()
	entries := make(map[string]*activeRun, len(w.active))
	for runID, entry := range w.active {
		entries[runID] = entry
		entry.markStopping()
	}
	w.mu.Unlock()
	for _, entry := range entries {
		entry.cancel(cause)
	}
	return entries
}

func (w *Worker) waitForActiveRuns(entries map[string]*activeRun, timeout time.Duration) {
	if len(entries) == 0 {
		return
	}
	if timeout <= 0 {
		timeout = terminalUpdateSendTimeout
	}
	exited := make(chan string, len(entries))
	for runID, entry := range entries {
		go func(runID string, entry *activeRun) {
			<-entry.done
			w.deleteActiveEntry(runID, entry)
			exited <- runID
		}(runID, entry)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for len(entries) > 0 {
		select {
		case runID := <-exited:
			delete(entries, runID)
		case <-timer.C:
			return
		}
	}
}

func (w *Worker) waitForPriorRuns(ctx context.Context) error {
	for {
		var (
			runID string
			entry *activeRun
		)
		w.mu.Lock()
		for runID, entry = range w.active {
			break
		}
		w.mu.Unlock()
		if entry == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-entry.done:
			w.deleteActiveEntry(runID, entry)
		}
	}
}

func (w *Worker) disconnectCleanupTimeout() time.Duration {
	if w.options.DisconnectCleanupTimeout > 0 {
		return w.options.DisconnectCleanupTimeout
	}
	return terminalUpdateSendTimeout
}

func (w *Worker) handleCommand(ctx context.Context, stream RuntimeStream, cmd *turingv1.RuntimeCommand) error {
	if cmd == nil {
		return errors.New("runtime command is required")
	}
	switch value := cmd.GetCommand().(type) {
	case *turingv1.RuntimeCommand_RunAssigned:
		if value.RunAssigned != nil {
			w.startRun(ctx, stream, value.RunAssigned)
		}
	case *turingv1.RuntimeCommand_RunCancelled:
		if value.RunCancelled != nil {
			if !w.acceptCommandVersion(value.RunCancelled.GetRunId(), value.RunCancelled.GetStateVersion()) {
				// Computed against a state this worker has already been told to
				// leave. Acting on it would cancel a run the orchestrator has
				// since moved forward.
				return nil
			}
			return w.cancelRun(ctx, stream, value.RunCancelled.GetRunId())
		}
	case *turingv1.RuntimeCommand_ShutdownRequested:
		return errShutdownRequested
	case *turingv1.RuntimeCommand_ToolPolicyDecision:
		w.deliverDecision(value.ToolPolicyDecision)
	case *turingv1.RuntimeCommand_ApprovalUpdated:
		if value.ApprovalUpdated != nil {
			w.acceptCommandVersion(w.runForApproval(value.ApprovalUpdated.GetApprovalId()), value.ApprovalUpdated.GetStateVersion())
			if value.ApprovalUpdated.Status == "denied" || value.ApprovalUpdated.Status == "expired" {
				return w.cancelApprovalRun(ctx, stream, value.ApprovalUpdated.ApprovalId, value.ApprovalUpdated.Status)
			}
			if value.ApprovalUpdated.Status == "approved" {
				// The decision this worker was waiting to be told about. It is
				// what permits asking for a resume, and nothing more: the run
				// is still waiting-approval until the orchestrator says
				// otherwise.
				w.markApprovalDecided(value.ApprovalUpdated.GetApprovalId())
			}
		}
	case *turingv1.RuntimeCommand_ApprovalResumeAccepted:
		w.deliverResumeAcceptance(value.ApprovalResumeAccepted)
	case *turingv1.RuntimeCommand_McpRegistryChanged:
		w.scheduleMCPRegistryRefresh(ctx, stream, value.McpRegistryChanged)
		return nil
	}
	return nil
}

func (w *Worker) scheduleMCPRegistryRefresh(
	ctx context.Context,
	stream RuntimeStream,
	command *turingv1.RuntimeMcpRegistryChanged,
) {
	if command == nil {
		w.reportFatal(errors.New("MCP registry refresh command is required"))
		return
	}
	w.refreshMu.Lock()
	w.refreshPending = &turingv1.RuntimeMcpRegistryChanged{
		RegistrationId: command.GetRegistrationId(),
	}
	if w.refreshRunning {
		w.refreshMu.Unlock()
		return
	}
	w.refreshRunning = true
	w.refreshMu.Unlock()

	go func() {
		for {
			w.refreshMu.Lock()
			pending := w.refreshPending
			w.refreshPending = nil
			if pending == nil {
				w.refreshRunning = false
				w.refreshMu.Unlock()
				return
			}
			w.refreshMu.Unlock()
			update, err := w.refreshMCPRegistry(ctx, pending)
			if err != nil {
				w.refreshMu.Lock()
				w.refreshRunning = false
				w.refreshMu.Unlock()
				w.reportFatal(err)
				return
			}
			sendCtx, cancel := context.WithTimeout(ctx, w.options.UpdateSendTimeout)
			err = w.send(sendCtx, stream, update)
			cancel()
			if err != nil {
				w.refreshMu.Lock()
				w.refreshRunning = false
				w.refreshMu.Unlock()
				w.reportFatal(err)
				return
			}
		}
	}()
}

func (w *Worker) refreshMCPRegistry(
	ctx context.Context,
	command *turingv1.RuntimeMcpRegistryChanged,
) (*turingv1.RuntimeUpdate, error) {
	if command == nil || command.GetRegistrationId() == "" {
		return nil, errors.New("MCP registry refresh registration_id is required")
	}
	if invalidator, ok := w.executor.(ToolRegistryInvalidator); ok {
		invalidator.InvalidateToolRegistry()
	}
	if w.options.DiscoverTools == nil {
		return nil, errors.New("MCP registry refresh requires tool discovery")
	}
	discovered, err := w.options.DiscoverTools(ctx)
	if err != nil {
		return nil, fmt.Errorf("refresh MCP registry tools: %w", err)
	}
	return &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_WorkerCapabilitiesUpdated{
			WorkerCapabilitiesUpdated: &turingv1.RuntimeWorkerCapabilitiesUpdated{
				WorkerId:       w.options.WorkerID,
				RegistrationId: command.GetRegistrationId(),
				Capabilities: &turingv1.WorkerCapabilities{
					Models:                      cloneModelCapabilities(w.options.Models),
					AgentIds:                    []turingv1.AgentId{w.options.AgentID},
					Tools:                       discovered,
					MaxConcurrentRuns:           int32(w.options.MaxConcurrentRuns),
					SupportsExternalAgents:      w.options.SupportsExternalAgents || len(w.options.ExternalAgentCredentialRefs) > 0,
					ExternalAgentCredentialRefs: cloneCredentialRefs(w.options.ExternalAgentCredentialRefs),
					RemoteEgressDecisionVersion: w.options.RemoteEgressDecisionVersion,
				},
			},
		},
	}, nil
}

func (w *Worker) startRun(parent context.Context, stream RuntimeStream, job *turingv1.AgentJob) {
	runCtx, cancel := context.WithCancelCause(parent)
	entry := &activeRun{
		runID:     job.GetRunId(),
		cancel:    cancel,
		done:      make(chan struct{}),
		attemptID: job.GetAssignmentAttemptId(),
		version:   job.GetExpectedStateVersion(),
	}
	w.mu.Lock()
	if existing, exists := w.active[job.GetRunId()]; exists {
		// Already running it. A redelivered RunAssigned is never a second
		// executor: acking would terminally fail a healthy in-flight run, which
		// then keeps executing and reports a completion the orchestrator
		// rejects.
		//
		// The same shape also carries a same-attempt refresh, which is how the
		// orchestrator hands back the version it committed while this run's
		// ownership was in doubt. That one is applied — to this exact attempt
		// only — and releases any updates held since the loss.
		w.mu.Unlock()
		cancel(context.Canceled)
		existing.refreshAssignment(job.GetAssignmentAttemptId(), job.GetExpectedStateVersion())
		return
	}
	if w.terminalAttemptReportedLocked(job.GetRunId(), job.GetAssignmentAttemptId()) {
		// The same refresh, arriving after this attempt's executor already
		// exited. There is no entry left to apply it to, and starting one would
		// run the whole job a second time, commit its side effects again, and
		// produce a second terminal report the orchestrator has to refuse.
		// Checked after the active-entry branch above so a live run's refresh
		// keeps taking exactly the path it always did.
		w.mu.Unlock()
		cancel(context.Canceled)
		return
	}
	if len(w.active) >= w.options.MaxConcurrentRuns {
		w.mu.Unlock()
		cancel(context.Canceled)
		// Say so rather than dropping it. Silence is indistinguishable from
		// "accepted and running", and the orchestrator renews its assignment on
		// every heartbeat, so a dropped one never expires — the run would hang
		// forever. Retryable because the worker was busy, not because the run is
		// broken. Reachable after a reconnect, when a run still draining from the
		// previous stream can outlive DisconnectCleanupTimeout and hold the slot.
		sendCtx, sendCancel := context.WithTimeout(parent, w.options.UpdateSendTimeout)
		defer sendCancel()
		_ = w.send(sendCtx, stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{
			RunFailed: &turingv1.RuntimeRunFailed{
				RunId:                job.GetRunId(),
				Code:                 "worker_busy",
				FailureOrigin:        turingv1.FailureOrigin_FAILURE_ORIGIN_DISPATCH,
				AutomaticRetryClass:  turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_SAME_RUN_TRANSIENT,
				ExpectedStateVersion: job.GetExpectedStateVersion(),
			},
		}})
		return
	}
	w.active[job.GetRunId()] = entry
	// A run that is starting again under a new attempt has nothing to remember:
	// the entry could only ever suppress an attempt nobody will mention again.
	w.forgetTerminalAttemptLocked(job.GetRunId())
	w.mu.Unlock()
	go func() {
		defer close(entry.done)
		var terminal *turingv1.RuntimeUpdate
		err := w.executor.Execute(runCtx, job, func(update *turingv1.RuntimeUpdate) error {
			if isDerivedMessageCompleted(update) {
				return nil
			}
			if isTerminalRunUpdate(update) {
				terminal = update
				return nil
			}
			sendCtx, cancel := context.WithTimeout(runCtx, w.options.UpdateSendTimeout)
			defer cancel()
			return w.sendRunUpdate(sendCtx, entry, stream, update)
		})
		if entry.isStopping() {
			return
		}
		// Remembered BEFORE the run leaves the active map, and before any
		// terminal report is claimed or sent. Between the delete and the send
		// the run is in neither place, and a same-attempt refresh landing in
		// that window — the recv loop runs concurrently with this goroutine —
		// would look like a brand new assignment. This is also why it is
		// unconditional: an executor that exited without reporting anything is
		// just as finished as one that reported, and starting its attempt again
		// would re-run the whole job either way.
		w.rememberTerminalAttempt(entry)
		w.deleteActive(job.GetRunId())
		if terminal != nil {
			w.sendTerminalOnce(entry, runCtx, stream, terminal)
			return
		}

		if runWasTerminalized(err) {
			w.sendTerminalOnce(entry, runCtx, stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: job.GetRunId()}}})
			return
		}

		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(runCtx.Err(), context.Canceled) {
			// The executor's error text stays here. It names Go types, provider
			// prose, and tool output, none of which the orchestrator may
			// persist or return; what it needs is that this failure came from
			// the worker's own runtime and must not be retried.
			log.Printf("run %s executor failed: %v", job.GetRunId(), err)
			w.sendTerminalOnce(entry, runCtx, stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{
				RunFailed: &turingv1.RuntimeRunFailed{
					RunId:               job.GetRunId(),
					Code:                "runtime_error",
					FailureOrigin:       turingv1.FailureOrigin_FAILURE_ORIGIN_WORKER_RUNTIME,
					AutomaticRetryClass: turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_NEVER,
				},
			}})
		}
	}()
}

func isDerivedMessageCompleted(update *turingv1.RuntimeUpdate) bool {
	event := update.GetEvent()
	// RuntimeService derives message.completed from run_completed.
	return event != nil && event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED
}

func isTerminalRunUpdate(update *turingv1.RuntimeUpdate) bool {
	return update != nil && (update.GetRunCompleted() != nil || update.GetRunFailed() != nil)
}

type terminalRunState interface {
	RunTerminal() bool
}

func runWasTerminalized(err error) bool {
	var terminal terminalRunState
	return errors.As(err, &terminal) && terminal.RunTerminal()
}

func (w *Worker) cancelRun(ctx context.Context, stream RuntimeStream, runID string) error {
	return w.cancelRunWithCause(ctx, stream, runID, context.Canceled)
}

func (w *Worker) cancelRunWithCause(ctx context.Context, stream RuntimeStream, runID string, cause error) error {
	entry := w.activeRun(runID)
	if entry == nil {
		return nil
	}
	if cause == nil {
		cause = context.Canceled
	}
	started, ownsTerminalReport := entry.beginCancellation()
	if !started {
		return nil
	}
	if ownsTerminalReport {
		w.rememberTerminalAttempt(entry)
	}
	entry.cancel(cause)
	go func() {
		<-entry.done
		w.deleteActive(runID)
		if ownsTerminalReport {
			ack := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: runID}}}
			stampObservedVersion(entry, ack)
			w.sendTerminalOrReport(context.Background(), stream, ack)
		}
	}()
	return nil
}

// rememberTerminalAttempt records that this assignment has claimed the run's
// terminal report, so a redelivered or refreshed assignment for the SAME
// attempt is recognized as what it is rather than started again.
//
// The memory is bounded by count and evicted oldest-first. Nothing here expires
// on a timer: a timer would be another goroutine to leak, and an entry that
// outlives its race is harmless — the only assignment it can suppress is one
// naming an attempt that has already reported.
func (w *Worker) rememberTerminalAttempt(entry *activeRun) {
	if entry == nil {
		return
	}
	// Read before the worker lock is taken, never under it: entry.mu is only
	// ever acquired outside w.mu, and keeping it that way is what stops the two
	// from ever being ordered both ways.
	runID, attemptID := entry.identity()
	if runID == "" || attemptID == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, exists := w.terminalAttempts[runID]; !exists {
		w.terminalOrder = append(w.terminalOrder, runID)
	}
	w.terminalAttempts[runID] = terminalAttempt{attemptID: attemptID}
	w.evictTerminalAttemptsLocked()
}

func (w *Worker) evictTerminalAttemptsLocked() {
	for len(w.terminalAttempts) > maxRememberedTerminalAttempts && len(w.terminalOrder) > 0 {
		oldest := w.terminalOrder[0]
		w.terminalOrder = w.terminalOrder[1:]
		delete(w.terminalAttempts, oldest)
	}
}

// forgetTerminalAttemptLocked drops the memory for a run a new attempt is about
// to execute, so a run re-dispatched many times does not hold an entry naming an
// attempt nobody will ever mention again.
func (w *Worker) forgetTerminalAttemptLocked(runID string) {
	if _, exists := w.terminalAttempts[runID]; !exists {
		return
	}
	delete(w.terminalAttempts, runID)
	for index, remembered := range w.terminalOrder {
		if remembered == runID {
			w.terminalOrder = append(w.terminalOrder[:index], w.terminalOrder[index+1:]...)
			return
		}
	}
}

// terminalAttemptReportedLocked reports whether this exact assignment attempt
// has already claimed the run's terminal report.
//
// Both identities must be present and equal. An orchestrator that sends no
// attempt ID cannot be told a redelivery from a re-dispatch, and refusing there
// would strand a run that legitimately needs to run again.
func (w *Worker) terminalAttemptReportedLocked(runID string, attemptID string) bool {
	if runID == "" || attemptID == "" {
		return false
	}
	remembered, exists := w.terminalAttempts[runID]
	return exists && remembered.attemptID == attemptID
}

func (w *Worker) activeRun(runID string) *activeRun {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active[runID]
}

func (w *Worker) runForApproval(approvalID string) string {
	if approvalID == "" {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.approvals[approvalID]
}

// rememberApproval records which run an approval belongs to and opens the
// resume slot for it.
//
// The slot is opened here, when the requirement is learned, rather than when
// something waits on it. The decision and the acceptance both arrive on the
// command loop, and the executor is not yet blocked on either; without a place
// for them to land, a command that arrived a moment early would simply be lost
// and the run would wait out its deadline for something it had already been
// sent.
func (w *Worker) rememberApproval(approvalID string, runID string) {
	if approvalID == "" || runID == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.approvals[approvalID] = runID
	pending, exists := w.resumes[approvalID]
	if !exists {
		w.resumes[approvalID] = newApprovalResume(runID)
		return
	}
	if pending.runID == "" {
		// A decision that arrived before this requirement did. It is this run's
		// after all, and now that the slot has an owner it is cleaned up with
		// the run rather than by the orphan bound.
		pending.runID = runID
		w.claimUnclaimedResumeLocked(approvalID)
	}
}

func newApprovalResume(runID string) *approvalResume {
	return &approvalResume{
		runID:   runID,
		decided: make(chan struct{}),
		signal:  make(chan struct{}),
	}
}

// recordAccepted stores a durable acceptance for later claiming, unless a
// failing path has already reached outcomeMu first and abandoned this resume
// — once abandoned, no later acceptance can revive it — or the Ready send this
// acceptance would answer has not even been attempted yet. It reports whether
// the acceptance was recorded.
//
// This is the "record" half of the single linearizable commit point: whether
// this call or a concurrent abandonOrClaim acquires outcomeMu first is what
// decides the resume's fate, and each of them only ever acts on what it
// observes after acquiring it. The readyStarted check is a local invariant on
// top of that commit point, not a race with it: readyStarted can only ever be
// true after beginReadySend runs, which is only ever after the decision
// arrived, so an acceptance offered any earlier is refused here regardless of
// what outcome is otherwise in play.
func (p *approvalResume) recordAccepted(accepted *turingv1.RuntimeApprovalResumeAccepted) bool {
	p.outcomeMu.Lock()
	defer p.outcomeMu.Unlock()
	if !p.readyStarted || p.outcome != resumeOutcomePending {
		return false
	}
	p.outcome = resumeOutcomeAccepted
	p.accepted = accepted
	close(p.signal)
	return true
}

// beginReadySend records, under outcomeMu, that resumeApproval is initiating
// this resume's Ready send attempt locally — not that any bytes have reached
// the transport, only that the attempt is beginning. Called exactly once,
// from resumeApproval's sole call site, immediately before the send is
// attempted and never before the decision select returns: by that point
// nothing else can have reached outcomeMu first, so the resume is always
// still resumeOutcomePending here and the assignment is unconditional. It is
// what makes recordAccepted's readyStarted gate an honest local proof rather
// than an assumption: an acceptance cannot be durably recorded for this
// resume before this call has run, so nothing racing the decision select's
// own failing branch can ever produce a recorded acceptance for it to
// observe.
func (p *approvalResume) beginReadySend() {
	p.outcomeMu.Lock()
	defer p.outcomeMu.Unlock()
	p.readyStarted = true
}

// abandonOrClaim is the resume's single commit point for giving up. It is the
// "abandon" half of the same critical section recordAccepted uses: whichever
// of the two acquires outcomeMu first settles the resume's fate.
//
// If an acceptance was already recorded, it is claimed here — the acceptance
// wins regardless of what triggered this call, because the orchestrator
// cannot have produced it without having received and committed the Ready it
// answers. Otherwise the resume is marked abandoned so that a delivery
// arriving a moment later, racing this same call for the lock, observes the
// abandonment instead of reviving a resume whose failure may already be in
// flight.
func (p *approvalResume) abandonOrClaim() (*turingv1.RuntimeApprovalResumeAccepted, bool) {
	p.outcomeMu.Lock()
	defer p.outcomeMu.Unlock()
	switch p.outcome {
	case resumeOutcomeAccepted:
		return p.accepted, true
	case resumeOutcomePending:
		p.outcome = resumeOutcomeAbandoned
	}
	return nil, false
}

// markApprovalDecided records that the approved decision command arrived.
//
// This is the only thing that permits asking for a resume. A token this worker
// polled for is evidence about the approval, not about the run, and acting on
// it would mean continuing a run the orchestrator never agreed to restart.
//
// The decision can legitimately arrive before the requirement that explains it:
// an automation's own grant is queued ahead of the policy decision naming the
// approval, and both travel the same ordered command channel. So an unknown
// approval opens a slot rather than being dropped — bounded, because a slot
// nobody ever claims has no run to be cleaned up with.
func (w *Worker) markApprovalDecided(approvalID string) {
	if approvalID == "" {
		return
	}
	w.mu.Lock()
	pending, exists := w.resumes[approvalID]
	if !exists {
		pending = newApprovalResume("")
		w.resumes[approvalID] = pending
		w.unclaimedResumes = append(w.unclaimedResumes, approvalID)
		w.evictUnclaimedResumesLocked()
	}
	w.mu.Unlock()
	pending.decideOnce.Do(func() { close(pending.decided) })
}

func (w *Worker) claimUnclaimedResumeLocked(approvalID string) {
	for index, unclaimed := range w.unclaimedResumes {
		if unclaimed == approvalID {
			w.unclaimedResumes = append(w.unclaimedResumes[:index], w.unclaimedResumes[index+1:]...)
			return
		}
	}
}

func (w *Worker) evictUnclaimedResumesLocked() {
	for len(w.unclaimedResumes) > maxUnclaimedApprovalDecisions {
		oldest := w.unclaimedResumes[0]
		w.unclaimedResumes = w.unclaimedResumes[1:]
		if pending, exists := w.resumes[oldest]; exists && pending.runID == "" {
			delete(w.resumes, oldest)
		}
	}
}

// beginApprovalResume claims the pending resume for an approval and records the
// instant its wait ends.
func (w *Worker) beginApprovalResume(approvalID string, runID string, deadline time.Time) (*approvalResume, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	pending, exists := w.resumes[approvalID]
	if !exists {
		return nil, ""
	}
	if pending.runID == "" {
		return nil, ""
	}
	if runID != "" && pending.runID != runID {
		return nil, ""
	}
	pending.deadline = deadline
	return pending, pending.runID
}

// deliverResumeAcceptance releases a paused run on the orchestrator's durable
// acceptance, and only on one that names this exact run, approval, and live
// attempt at a version this assignment has not already moved past.
//
// A mismatched acceptance is dropped rather than acted on, and dropping it
// changes nothing else — in particular it does not restart the wait, because
// then any stream could hold this run paused indefinitely by sending
// acceptances that were never about it. Every check here, including the
// version check, is deliberately non-mutating: this function only decides
// whether pending.recordAccepted gets a chance to commit the outcome, never
// adopts anything itself. That is what keeps a duplicate, late, or
// already-abandoned acceptance — one recordAccepted goes on to refuse — from
// having moved the run's observed version before it was dropped;
// completeAcceptedResume is the only place that adopts a version, and it only
// runs for an outcome recordAccepted or abandonOrClaim actually claimed.
func (w *Worker) deliverResumeAcceptance(accepted *turingv1.RuntimeApprovalResumeAccepted) {
	if accepted == nil || accepted.GetApprovalId() == "" {
		return
	}
	w.mu.Lock()
	pending := w.resumes[accepted.GetApprovalId()]
	pendingRunID := ""
	if pending != nil {
		pendingRunID = pending.runID
	}
	w.mu.Unlock()
	if pending == nil || pendingRunID == "" || pendingRunID != accepted.GetRunId() {
		return
	}
	entry := w.activeRun(pendingRunID)
	if entry == nil {
		return
	}
	_, attemptID := entry.identity()
	if accepted.GetAssignmentAttemptId() != "" && accepted.GetAssignmentAttemptId() != attemptID {
		return
	}
	// versionAcceptable only validates: it never moves the run's version.
	// Adoption happens exactly once, in completeAcceptedResume, and only for
	// an outcome recordAccepted actually commits below — so a duplicate,
	// late, or already-abandoned acceptance that fails to record leaves the
	// run's observed version exactly where it was, rather than having
	// dragged it forward on its way to being dropped.
	if version := accepted.GetStateVersion(); version > 0 && !entry.versionAcceptable(version) {
		return
	}
	pending.recordAccepted(accepted)
}

// resumeApproval is the worker's half of the approval handshake.
//
// It waits for the decision command, tells the orchestrator this attempt is
// restored and paused, and then holds the executor until the orchestrator says
// it has durably committed running again. Everything after this point — the
// approved tool call and the model work that follows it — happens only on that
// acceptance.
func (w *Worker) resumeApproval(ctx context.Context, stream RuntimeStream, resume tools.ApprovalResume) error {
	if resume.ApprovalID == "" {
		return errors.New("approval resume requires an approval")
	}
	waitCtx := ctx
	if !resume.Deadline.IsZero() {
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithDeadline(ctx, resume.Deadline)
		defer cancel()
	}
	deadline, _ := waitCtx.Deadline()
	pending, runID := w.beginApprovalResume(resume.ApprovalID, resume.RunID, deadline)
	if pending == nil {
		return errors.New("approval resume names an approval this worker is not holding")
	}
	entry := w.activeRun(runID)
	if entry == nil {
		return errors.New("approval resume names a run this worker is no longer running")
	}
	select {
	case <-pending.decided:
	case <-waitCtx.Done():
		// Still waiting-approval, and this attempt still owns the run, so the
		// worker can name what actually went wrong. There is no claim branch
		// here: beginReadySend has not run yet at this point in the call —
		// it only ever runs after this select returns — so recordAccepted's
		// readyStarted gate guarantees no acceptance can have been committed
		// for this resume no matter what raced this deadline. The decision
		// gate is authoritative and this failure is unconditional.
		return w.failApprovalResume(ctx, stream, entry, runID, false, waitCtx.Err())
	}
	_, attemptID := entry.identity()
	// beginReadySend marks, under the resume's own lock, that recordAccepted
	// may now commit an acceptance for this resume. It runs immediately
	// before the send begins so that a send error below — the only case
	// where the Ready's fate becomes unknowable from here — still leaves an
	// acceptance racing it eligible to win: readyStarted is set regardless of
	// how the send below turns out, so "the send started" is true from this
	// worker's own point of view the instant it is attempted, not only once
	// it is confirmed to have reached the wire.
	pending.beginReadySend()
	// Bounded by what is LEFT of the approval budget, never by a fresh one.
	// The send is part of the same wait the user already sat through, so a
	// timeout of its own would let a resume outlive the deadline everything
	// else was told about — and it is also what makes a decision arriving in
	// the same instant the budget expires deterministic. Both arms of the wait
	// above are ready then, and select chooses between them at random; because
	// a spent budget cannot start a send either, both orders reach the same
	// answer instead of flipping a coin between failing the run and dropping
	// the stream.
	sendCtx, cancelSend := context.WithTimeout(waitCtx, w.options.UpdateSendTimeout)
	err := w.sendRunUpdateReportingPause(sendCtx, entry, stream, &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_ApprovalResumeReady{ApprovalResumeReady: &turingv1.RuntimeApprovalResumeReady{
			RunId:                runID,
			ApprovalId:           resume.ApprovalID,
			ExpectedStateVersion: entry.expectedVersion(),
			AssignmentAttemptId:  attemptID,
		}},
	})
	cancelSend()
	if err != nil {
		// A send error only means the Ready's fate is unknowable from HERE —
		// it does not mean the orchestrator never saw it. abandonOrClaim is
		// the single commit point: if an acceptance is already recorded, that
		// is durable proof the Ready was received and acted on regardless of
		// what this process's own send call returned, and it outranks the send
		// error — the resume is done, and the run's outbound narration (which
		// sendRunUpdateReportingPause just paused on this same error) is
		// refreshed rather than left muted for a run the orchestrator is
		// still hearing from. Otherwise this call is what abandons the
		// resume, atomically with respect to a delivery racing it for the
		// same decision.
		if accepted, ok := pending.abandonOrClaim(); ok {
			return w.completeAcceptedResume(entry, accepted)
		}
		// Only a send that BEGAN can have been received. Until then the Ready
		// is still sitting in this process — abandoned in the writer's queue,
		// or refused because the run's narration is withheld — so the row is
		// still waiting approval and this attempt still owns it, and the honest
		// answer is the transport failure rather than dropping every other run
		// on this stream. Once the bytes are in flight none of that is knowable
		// any more, and the stream goes so the ownership fence can decide.
		return w.failApprovalResume(ctx, stream, entry, runID, outboundSendStarted(err), err)
	}
	// The Ready is away. An acceptance recorded while the send was still in
	// flight already closed pending.signal, so the select below claims it
	// immediately rather than blocking — a channel that is already closed is
	// always the ready case, which is what makes a separate non-blocking peek
	// before this select redundant rather than merely equivalent.
	select {
	case <-pending.signal:
	case <-waitCtx.Done():
	}
	// One call settles it regardless of which case fired: signal only ever
	// closes after an acceptance is recorded under outcomeMu, so if it fired
	// first abandonOrClaim is guaranteed to observe and claim that acceptance;
	// if the deadline fired first with nothing recorded yet, this is the same
	// call that atomically abandons the resume, closing the window a delivery
	// racing it for outcomeMu would otherwise slip through.
	if accepted, ok := pending.abandonOrClaim(); ok {
		return w.completeAcceptedResume(entry, accepted)
	}
	return w.failApprovalResume(ctx, stream, entry, runID, true, waitCtx.Err())
}

// completeAcceptedResume is what every winning path through resumeApproval
// returns through: it adopts the accepted version and unpauses outbound
// narration, and only then reports success.
//
// It does this unconditionally, even when sendRunUpdateReportingPause never
// paused anything: the point is that a Ready send error must never leave a
// durably-resumed run muted just because the acceptance that saved it happens
// to be observed after the pause was already set.
//
// entry is never nil here: every call site obtains it from w.activeRun and
// already returns early when that lookup fails, before this is ever reached.
func (w *Worker) completeAcceptedResume(entry *activeRun, accepted *turingv1.RuntimeApprovalResumeAccepted) error {
	entry.unpauseAfterAcceptedResume(accepted.GetStateVersion())
	return nil
}

// failApprovalResume ends a resume that cannot complete, without ever leaving
// the paused executor holding its worker slot.
//
// readySent decides what may be said. Before the Ready is in flight the row is
// still waiting approval and this attempt still owns it, so a typed
// approval-delivery failure at the version this worker knows is both true and
// useful. Once the Ready may have been received the orchestrator may already
// have committed running, and a terminal report computed against the older
// version would be refused anyway — so the stream is dropped instead and the
// required ownership fence moves the run to recovering.
//
// started decides whether that is still this resume's call to make. It is false
// when something else has already begun terminalizing the run — the
// orchestrator cancelled it, or a sibling authorization was refused — and that
// something owns the outcome and is already acknowledging it. There is nothing
// left in doubt for a fence to settle, and dropping the stream would take down
// every other run this worker is executing to re-establish a state the
// orchestrator itself just dictated.
func (w *Worker) failApprovalResume(
	ctx context.Context,
	stream RuntimeStream,
	entry *activeRun,
	runID string,
	readySent bool,
	cause error,
) error {
	started, ownsTerminalReport := entry.beginCancellation()
	if started && ownsTerminalReport {
		// Remembered on BOTH paths, and before the entry is released below.
		// Claiming the terminal report is what ends this attempt, whether or
		// not anything is said about it: the executor will not report again and
		// the run is about to leave the active map. A same-attempt refresh
		// arriving after that — the orchestrator handing back the version it
		// committed while ownership was in doubt — would otherwise look like a
		// brand new assignment and run the whole job a second time.
		w.rememberTerminalAttempt(entry)
	}
	if started && ownsTerminalReport && !readySent {
		update := &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{
			RunId:               runID,
			Code:                "approval_delivery_failed",
			FailureOrigin:       turingv1.FailureOrigin_FAILURE_ORIGIN_APPROVAL_TRANSPORT,
			AutomaticRetryClass: turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_NEVER,
		}}}
		stampObservedVersion(entry, update)
		w.sendTerminalOrReport(ctx, stream, update)
	}
	if started && readySent {
		w.reportFatal(errApprovalResumeUnacknowledged)
	}
	if started {
		resumeErr := approvalResumeError{err: cause}
		entry.cancel(resumeErr)
		// The slot is released once the executor has actually unwound, exactly
		// as a cancellation releases it. Waiting here would deadlock: this runs
		// inside the executor being cancelled.
		go func() {
			<-entry.done
			w.deleteActive(runID)
		}()
	}
	return approvalResumeError{err: cause}
}

// approvalResumeError ends the run rather than the tool call. The tool never
// ran, so there is nothing to report about it, and the run cannot continue
// against a resume nobody confirmed.
type approvalResumeError struct{ err error }

func (e approvalResumeError) Error() string {
	if e.err == nil {
		return "approval resume was not acknowledged"
	}
	return "approval resume was not acknowledged: " + e.err.Error()
}

func (e approvalResumeError) Unwrap() error     { return e.err }
func (e approvalResumeError) RunTerminal() bool { return true }

// acceptCommandVersion records the state version an orchestrator command
// carries, and reports whether the command is current.
//
// A command with no version is from an orchestrator that does not send one and
// is obeyed as before; a command naming an older version lost to something this
// worker has already been told about, and is refused.
func (w *Worker) acceptCommandVersion(runID string, version int64) bool {
	if runID == "" || version <= 0 {
		return true
	}
	entry := w.activeRun(runID)
	if entry == nil {
		return true
	}
	return entry.acceptVersion(version)
}

func (w *Worker) cancelApprovalRun(ctx context.Context, stream RuntimeStream, approvalID string, approvalStatus string) error {
	w.mu.Lock()
	runID := w.approvals[approvalID]
	w.mu.Unlock()
	if runID == "" {
		return nil
	}
	return w.cancelRunWithCause(ctx, stream, runID, tools.TerminalApprovalError{Status: approvalStatus})
}

func (w *Worker) deleteActive(runID string) {
	w.deleteActiveEntry(runID, nil)
}

func (w *Worker) deleteActiveEntry(runID string, expected *activeRun) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if expected != nil && w.active[runID] != expected {
		return
	}
	if _, exists := w.active[runID]; !exists {
		return
	}
	delete(w.active, runID)
	for approvalID, approvalRunID := range w.approvals {
		if approvalRunID == runID {
			delete(w.approvals, approvalID)
			delete(w.resumes, approvalID)
		}
	}
	for approvalID, pending := range w.resumes {
		if pending.runID == runID {
			delete(w.resumes, approvalID)
		}
	}
	for toolCallID, toolCallRunID := range w.toolCalls {
		if toolCallRunID == runID {
			delete(w.toolCalls, toolCallID)
		}
	}
}

func (w *Worker) startOutboundWriter(stream RuntimeStream) {
	w.writerMu.Lock()
	defer w.writerMu.Unlock()
	if w.writer != nil {
		return
	}
	w.writer = newOutboundWriter(stream)
}

func (w *Worker) stopOutboundWriter() {
	w.writerMu.Lock()
	defer w.writerMu.Unlock()
	writer := w.writer
	if writer != nil {
		writer.stop(nil)
		<-writer.exited
		if w.writer == writer {
			w.writer = nil
		}
	}
}

func (w *Worker) send(ctx context.Context, _ RuntimeStream, update *turingv1.RuntimeUpdate) error {
	if update == nil {
		return fmt.Errorf("runtime update is required")
	}
	w.writerMu.Lock()
	writer := w.writer
	w.writerMu.Unlock()
	if writer == nil {
		return errors.New("runtime outbound writer is not initialized")
	}
	return writer.send(ctx, update)
}

func (w *Worker) sendTerminalUpdate(ctx context.Context, stream RuntimeStream, update *turingv1.RuntimeUpdate) error {
	reportCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), terminalUpdateSendTimeout)
	defer cancel()
	return w.send(reportCtx, stream, update)
}

func (w *Worker) sendTerminalOrReport(ctx context.Context, stream RuntimeStream, update *turingv1.RuntimeUpdate) {
	if err := w.sendTerminalUpdate(ctx, stream, update); err != nil {
		w.reportFatal(err)
	}
}

func (w *Worker) sendTerminalOnce(entry *activeRun, ctx context.Context, stream RuntimeStream, update *turingv1.RuntimeUpdate) {
	if entry == nil || !entry.claimTerminalReport() {
		return
	}
	stampObservedVersion(entry, update)
	w.sendTerminalOrReport(ctx, stream, update)
}

// sendRunUpdate sends one update on behalf of a run, and stops that run
// narrating itself once an update fails to arrive.
//
// A failed send is the worker's only evidence that the orchestrator stopped
// hearing it, and the orchestrator fences a run it cannot hear from. Anything
// this run says afterwards describes a state nobody committed, so it is
// withheld until a same-attempt refresh says what the current state is.
//
// Withheld updates are dropped rather than queued, and that is deliberate: they
// were computed against a state the orchestrator has by then already left, so
// replaying them after the refresh would publish a story about a version that
// no longer exists. Nothing durable is lost — narration is advisory, and the
// run's outcome travels on the terminal report, which is still sent and is
// fenced by the version this assignment last accepted.
func (w *Worker) sendRunUpdate(ctx context.Context, entry *activeRun, stream RuntimeStream, update *turingv1.RuntimeUpdate) error {
	err := w.sendRunUpdateReportingPause(ctx, entry, stream, update)
	if errors.Is(err, errRunNarrationPaused) {
		// Narration is advisory, so a withheld one is not the executor's
		// problem and is reported as the no-op it is.
		return nil
	}
	return err
}

// sendRunUpdateReportingPause is the same send, for callers that cannot treat a
// withheld update as done.
//
// Narration is one-way and may be dropped; a tool beacon is a question, and
// dropping it silently leaves the asker waiting for an answer that was never
// requested. Those callers need the pause reported rather than absorbed.
func (w *Worker) sendRunUpdateReportingPause(ctx context.Context, entry *activeRun, stream RuntimeStream, update *turingv1.RuntimeUpdate) error {
	if entry == nil {
		return w.send(ctx, stream, update)
	}
	if entry.outboundPaused() {
		return errRunNarrationPaused
	}
	if err := w.send(ctx, stream, update); err != nil {
		entry.pauseOutbound()
		return err
	}
	return nil
}

// stampObservedVersion writes the version this assignment last accepted onto a
// terminal report.
//
// The worker is authoritative here rather than the executor: the executor knows
// what it produced, and only the worker knows which committed state that work
// was computed against.
func stampObservedVersion(entry *activeRun, update *turingv1.RuntimeUpdate) {
	version := entry.expectedVersion()
	if version <= 0 || update == nil {
		return
	}
	switch {
	case update.GetRunCompleted() != nil:
		update.GetRunCompleted().ExpectedStateVersion = version
	case update.GetRunFailed() != nil:
		update.GetRunFailed().ExpectedStateVersion = version
	case update.GetRunCancelledAck() != nil:
		update.GetRunCancelledAck().ObservedStateVersion = version
	}
}

func (w *Worker) setFatalChannel(ch chan error) {
	w.fatalMu.Lock()
	defer w.fatalMu.Unlock()
	w.fatal = ch
}

func (w *Worker) reportFatal(err error) {
	if err == nil {
		return
	}
	w.fatalMu.Lock()
	fatal := w.fatal
	w.fatalMu.Unlock()
	if fatal == nil {
		return
	}
	select {
	case fatal <- err:
	default:
	}
}

func (w *Worker) postToolBeacon(ctx context.Context, stream RuntimeStream, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
	if beacon == nil || beacon.ToolCallId == "" {
		return nil, errors.New("tool beacon with tool_call_id is required")
	}
	sent := false
	w.decisionMu.Lock()
	w.reapDecisionTombstonesLocked(time.Now())
	generation := w.generations[beacon.ToolCallId] + 1
	w.generations[beacon.ToolCallId] = generation
	waiter := &decisionWaiter{
		decision:   make(chan *turingv1.ToolPolicyDecision, 1),
		phase:      beacon.Phase,
		generation: generation,
	}
	w.decisions[beacon.ToolCallId] = append(w.decisions[beacon.ToolCallId], waiter)
	w.decisionMu.Unlock()
	defer func() {
		w.retireDecisionWaiter(beacon.ToolCallId, waiter, sent)
		w.mu.Lock()
		delete(w.toolCalls, beacon.ToolCallId)
		w.mu.Unlock()
	}()
	w.mu.Lock()
	w.toolCalls[beacon.ToolCallId] = beacon.RunId
	w.mu.Unlock()
	if err := w.sendRunUpdateReportingPause(ctx, w.activeRun(beacon.GetRunId()), stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: beacon}}); err != nil {
		if outboundSendStarted(err) {
			sent = true
			return nil, sentBeaconError{err: err}
		}
		// A paused run leaves here too, and deliberately as a plain error: the
		// beacon was never posted, so the runner must not report an after-beacon
		// for it, and the executor fails out through the reporting path that
		// already exists rather than blocking on a decision nobody will send.
		return nil, err
	}
	sent = true
	select {
	case decision := <-waiter.decision:
		// The decision is the orchestrator's reply to this exact beacon, so any
		// version it carries is the state tool and model work continues from.
		w.acceptCommandVersion(beacon.GetRunId(), decision.GetRunStateVersion())
		if decision.GetDecision() == turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED && decision.GetApprovalId() != "" && beacon.GetRunId() != "" {
			w.rememberApproval(decision.GetApprovalId(), beacon.GetRunId())
		}
		return decision, nil
	case <-ctx.Done():
		return nil, sentBeaconError{err: ctx.Err()}
	}
}

type sentBeaconError struct {
	err error
}

func (e sentBeaconError) Error() string { return e.err.Error() }
func (e sentBeaconError) Unwrap() error { return e.err }
func (e sentBeaconError) BeaconPosted() bool {
	return true
}

func (w *Worker) deliverDecision(decision *turingv1.ToolPolicyDecision) {
	if decision == nil || decision.ToolCallId == "" {
		return
	}
	w.decisionMu.Lock()
	w.reapDecisionTombstonesLocked(time.Now())
	waiters := w.decisions[decision.ToolCallId]
	if len(waiters) == 0 {
		w.decisionMu.Unlock()
		return
	}
	match := -1
	for index, waiter := range waiters {
		if waiter.phase == decision.GetPhase() {
			match = index
			break
		}
	}
	if match < 0 {
		w.decisionMu.Unlock()
		return
	}
	waiter := waiters[match]
	tombstone := waiter.tombstone
	if len(waiters) == 1 {
		delete(w.decisions, decision.ToolCallId)
		delete(w.generations, decision.ToolCallId)
	} else {
		w.decisions[decision.ToolCallId] = append(waiters[:match], waiters[match+1:]...)
	}
	w.decisionMu.Unlock()
	if tombstone {
		return
	}
	if decision.GetDecision() == turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED && decision.GetApprovalId() != "" {
		w.mu.Lock()
		runID := w.toolCalls[decision.GetToolCallId()]
		w.mu.Unlock()
		w.rememberApproval(decision.GetApprovalId(), runID)
	}
	select {
	case waiter.decision <- decision:
	default:
	}
}

func (w *Worker) retireDecisionWaiter(toolCallID string, target *decisionWaiter, sent bool) {
	w.decisionMu.Lock()
	w.reapDecisionTombstonesLocked(time.Now())
	waiters := w.decisions[toolCallID]
	for index, waiter := range waiters {
		if waiter != target {
			continue
		}
		if sent {
			// Preserve this generation so a delayed same-phase response cannot
			// be delivered to a later request that reuses the tool call ID.
			waiter.tombstone = true
			waiter.expiresAt = time.Now().Add(w.options.DecisionTombstoneTTL)
			w.decisionMu.Unlock()
			time.AfterFunc(w.options.DecisionTombstoneTTL, w.reapDecisionTombstones)
			return
		}
		waiters = append(waiters[:index], waiters[index+1:]...)
		if len(waiters) == 0 {
			delete(w.decisions, toolCallID)
			delete(w.generations, toolCallID)
		} else {
			w.decisions[toolCallID] = waiters
		}
		w.decisionMu.Unlock()
		return
	}
	w.decisionMu.Unlock()
}

func (w *Worker) reapDecisionTombstones() {
	w.decisionMu.Lock()
	defer w.decisionMu.Unlock()
	w.reapDecisionTombstonesLocked(time.Now())
}

func (w *Worker) reapDecisionTombstonesLocked(now time.Time) {
	for toolCallID, waiters := range w.decisions {
		kept := waiters[:0]
		for _, waiter := range waiters {
			if waiter.tombstone && !waiter.expiresAt.After(now) {
				continue
			}
			kept = append(kept, waiter)
		}
		if len(kept) == 0 {
			delete(w.decisions, toolCallID)
			delete(w.generations, toolCallID)
			continue
		}
		w.decisions[toolCallID] = kept
	}
}

func (r *activeRun) markStopping() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stop {
		return false
	}
	r.stop = true
	return true
}

func (r *activeRun) isStopping() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stop
}

func (r *activeRun) beginCancellation() (bool, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stop {
		return false, false
	}
	r.stop = true
	if r.terminalReportClaimed {
		return true, false
	}
	r.terminalReportClaimed = true
	return true, true
}

func (r *activeRun) claimTerminalReport() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stop || r.terminalReportClaimed {
		return false
	}
	r.terminalReportClaimed = true
	return true
}

// identity is the run and assignment this entry belongs to. Both are written
// once, at construction, and this reads them under the entry's own lock so no
// caller has to know that.
func (r *activeRun) identity() (string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runID, r.attemptID
}

// expectedVersion is the highest state version this assignment has been told
// about.
func (r *activeRun) expectedVersion() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.version
}

// acceptVersion moves this assignment forward to a version the orchestrator
// committed. It only ever moves forward: a command carrying an older version
// was computed against a state this worker has already been told to leave, and
// following it back would make the next report stale.
func (r *activeRun) acceptVersion(version int64) bool {
	if version <= 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if version < r.version {
		return false
	}
	r.version = version
	return true
}

// versionAcceptable reports whether version could be adopted as a forward
// move for this assignment, using the same forward-only rule as acceptVersion
// — but without moving anything. deliverResumeAcceptance uses this instead of
// acceptVersion to validate an acceptance before recordAccepted decides
// whether the resume is still open to it: a duplicate, late, or
// already-abandoned acceptance must leave the version untouched, and only
// completeAcceptedResume — reached once recordAccepted or abandonOrClaim has
// actually claimed the outcome — is allowed to adopt it, via
// unpauseAfterAcceptedResume.
func (r *activeRun) versionAcceptable(version int64) bool {
	if version <= 0 {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return version >= r.version
}

// pauseOutbound stops this run narrating itself after an update failed to reach
// the orchestrator.
func (r *activeRun) pauseOutbound() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paused = true
}

// refreshAssignment applies a same-attempt assignment refresh: it carries the
// committed version forward and releases any held updates. A refresh naming a
// different attempt is a fenced predecessor's command and changes nothing.
func (r *activeRun) refreshAssignment(attemptID string, version int64) bool {
	r.mu.Lock()
	if attemptID == "" || r.attemptID == "" || attemptID != r.attemptID {
		r.mu.Unlock()
		return false
	}
	if version > 0 && version >= r.version {
		r.version = version
	}
	r.paused = false
	r.mu.Unlock()
	return true
}

// unpauseAfterAcceptedResume clears this run's outbound pause and adopts the
// orchestrator's accepted version, without re-validating the attempt the way
// refreshAssignment does for an incoming wire refresh.
//
// That re-validation is deliberately skipped here: an accepted resume only
// ever reaches this call after deliverResumeAcceptance has already matched it
// to this exact run, attempt, and version — including the case where neither
// the accepted message nor this entry carries an attempt ID at all, which
// deliverResumeAcceptance treats as a match. Routing through refreshAssignment
// instead would silently refuse to unpause exactly that case, because it
// requires a non-empty attempt ID on both sides to prove the wire message it
// is built for is naming the same attempt — a proof this call does not need
// to redo.
func (r *activeRun) unpauseAfterAcceptedResume(version int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if version > 0 && version >= r.version {
		r.version = version
	}
	r.paused = false
}

// outboundPaused reports whether this run's narration is currently withheld.
func (r *activeRun) outboundPaused() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paused
}
