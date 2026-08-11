package worker

import (
	"context"
	"errors"
	"fmt"
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
	WorkerID                 string
	AgentID                  turingv1.AgentId
	MaxConcurrentRuns        int
	HeartbeatInterval        time.Duration
	DisconnectCleanupTimeout time.Duration
	DecisionTombstoneTTL     time.Duration
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
	cancel             context.CancelCauseFunc
	done               chan struct{}
	mu                 sync.Mutex
	stop               bool
	terminalReportOnce sync.Once
}

const (
	terminalUpdateSendTimeout   = 5 * time.Second
	defaultHeartbeatInterval    = 30 * time.Second
	defaultDecisionTombstoneTTL = 100 * time.Millisecond
	maxConcurrentRuns           = 128
)

var errOutboundWriterStopped = errors.New("runtime outbound writer stopped")
var errShutdownRequested = errors.New("runtime shutdown requested")
var errRuntimeDisconnected = errors.New("runtime stream disconnected")

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
	return &Worker{
		options: options, client: client, executor: executor, active: map[string]*activeRun{},
		approvals: map[string]string{}, toolCalls: map[string]string{}, decisions: map[string][]*decisionWaiter{},
		generations: map[string]uint64{},
	}
}

func (w *Worker) Run(ctx context.Context) error {
	if w.client == nil {
		return errors.New("runtime client is required")
	}
	if w.executor == nil {
		return errors.New("executor is required")
	}
	if w.options.MaxConcurrentRuns < 1 || w.options.MaxConcurrentRuns > maxConcurrentRuns {
		return errors.New("max concurrent runs must be between 1 and 128")
	}
	if ctx == nil {
		ctx = context.Background()
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
	if err := w.send(streamCtx, stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: w.options.WorkerID, AgentId: w.options.AgentID, MaxConcurrentRuns: int32(w.options.MaxConcurrentRuns)}}}); err != nil {
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
			if err := w.send(streamCtx, stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Heartbeat{Heartbeat: &turingv1.RuntimeHeartbeat{
				WorkerId: w.options.WorkerID,
			}}}); err != nil {
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
			return w.cancelRun(ctx, stream, value.RunCancelled.GetRunId())
		}
	case *turingv1.RuntimeCommand_ShutdownRequested:
		return errShutdownRequested
	case *turingv1.RuntimeCommand_ToolPolicyDecision:
		w.deliverDecision(value.ToolPolicyDecision)
	case *turingv1.RuntimeCommand_ApprovalUpdated:
		if value.ApprovalUpdated != nil && (value.ApprovalUpdated.Status == "denied" || value.ApprovalUpdated.Status == "expired") {
			return w.cancelApprovalRun(ctx, stream, value.ApprovalUpdated.ApprovalId, value.ApprovalUpdated.Status)
		}
	}
	return nil
}

func (w *Worker) startRun(parent context.Context, stream RuntimeStream, job *turingv1.AgentJob) {
	runCtx, cancel := context.WithCancelCause(parent)
	entry := &activeRun{cancel: cancel, done: make(chan struct{})}
	w.mu.Lock()
	if _, exists := w.active[job.GetRunId()]; exists || len(w.active) >= w.options.MaxConcurrentRuns {
		w.mu.Unlock()
		cancel(context.Canceled)
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
			return w.send(runCtx, stream, update)
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
			w.sendTerminalOnce(entry, runCtx, stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{RunId: job.GetRunId(), Code: "runtime_error", Message: err.Error(), Retryable: false}}})
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
	if !entry.markStopping() {
		return nil
	}
	entry.cancel(cause)
	go func() {
		<-entry.done
		w.deleteActive(runID)
		w.sendTerminalOnce(entry, context.Background(), stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCancelledAck{RunCancelledAck: &turingv1.RuntimeCancelledAck{RunId: runID}}})
	}()
	return nil
}

func (w *Worker) activeRun(runID string) *activeRun {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.active[runID]
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
	writer := w.writer
	w.writerMu.Unlock()
	if writer != nil {
		writer.stop(nil)
		<-writer.exited
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
	w.sendTerminalOrReport(ctx, stream, update)
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
	if err := w.send(ctx, stream, &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_ToolBeacon{ToolBeacon: beacon}}); err != nil {
		if outboundSendStarted(err) {
			sent = true
			return nil, sentBeaconError{err: err}
		}
		return nil, err
	}
	sent = true
	select {
	case decision := <-waiter.decision:
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
	waiter := waiters[0]
	tombstone := waiter.tombstone
	if len(waiters) == 1 {
		delete(w.decisions, decision.ToolCallId)
		delete(w.generations, decision.ToolCallId)
	} else {
		w.decisions[decision.ToolCallId] = waiters[1:]
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
			// Responses carry no phase or generation. Preserve this slot so a
			// delayed response cannot be delivered to a later same-ID request.
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

func (r *activeRun) claimTerminalReport() bool {
	claimed := false
	r.terminalReportOnce.Do(func() {
		claimed = true
	})
	return claimed
}
