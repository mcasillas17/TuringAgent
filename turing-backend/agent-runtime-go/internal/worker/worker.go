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

type Executor interface {
	Execute(ctx context.Context, job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error
}

type BeaconPosterSetter interface {
	SetToolBeaconPoster(func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error))
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
	options     Options
	client      RuntimeClient
	executor    Executor
	mu          sync.Mutex
	active      map[string]*activeRun
	approvals   map[string]string
	toolCalls   map[string]string
	decisionMu  sync.Mutex
	decisions   map[string][]*decisionWaiter
	generations map[string]uint64
	writerMu    sync.Mutex
	writer      *outboundWriter
	fatalMu     sync.Mutex
	fatal       chan error
}

type decisionWaiter struct {
	decision   chan *turingv1.ToolPolicyDecision
	phase      turingv1.ToolCallPhase
	generation uint64
	tombstone  bool
	expiresAt  time.Time
}

type activeRun struct {
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
)

var errOutboundWriterStopped = errors.New("runtime outbound writer stopped")
var errShutdownRequested = errors.New("runtime shutdown requested")
var errRuntimeDisconnected = errors.New("runtime stream disconnected")

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
		approvals: map[string]string{}, toolCalls: map[string]string{}, decisions: map[string][]*decisionWaiter{},
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
		}
	}
	return nil
}

func (w *Worker) startRun(parent context.Context, stream RuntimeStream, job *turingv1.AgentJob) {
	runCtx, cancel := context.WithCancelCause(parent)
	entry := &activeRun{
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
	if entry == nil {
		return w.send(ctx, stream, update)
	}
	if entry.outboundPaused() {
		return nil
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
	if err := w.sendRunUpdate(ctx, w.activeRun(beacon.GetRunId()), stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: beacon}}); err != nil {
		if outboundSendStarted(err) {
			sent = true
			return nil, sentBeaconError{err: err}
		}
		return nil, err
	}
	sent = true
	select {
	case decision := <-waiter.decision:
		// The decision is the orchestrator's reply to this exact beacon, so any
		// version it carries is the state tool and model work continues from.
		w.acceptCommandVersion(beacon.GetRunId(), decision.GetRunStateVersion())
		if decision.GetDecision() == turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED && decision.GetApprovalId() != "" && beacon.GetRunId() != "" {
			w.mu.Lock()
			w.approvals[decision.GetApprovalId()] = beacon.GetRunId()
			w.mu.Unlock()
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
		if runID := w.toolCalls[decision.GetToolCallId()]; runID != "" {
			w.approvals[decision.GetApprovalId()] = runID
		}
		w.mu.Unlock()
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

// outboundPaused reports whether this run's narration is currently withheld.
func (r *activeRun) outboundPaused() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.paused
}
