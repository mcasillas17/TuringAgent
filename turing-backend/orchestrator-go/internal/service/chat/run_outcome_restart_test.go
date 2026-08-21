package chat

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/sessions"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

// restartHarness owns a database file rather than a process's memory, because
// the promise this whole feature exists for is what a user sees after the app
// was closed. Every read below goes through a public gRPC service built on a
// reopened file; nothing inspects a repository struct the seeding process
// still holds.
type restartHarness struct {
	path     string
	database *db.DB
	repo     *repository.Repository
}

func newRestartHarness(t *testing.T) *restartHarness {
	t.Helper()
	path := filepath.Join(t.TempDir(), "turing.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return &restartHarness{path: path, database: database, repo: repository.New(database)}
}

// restart closes the seeded database and reopens the same file, so what the
// tests below read is what actually survived on disk.
func (h *restartHarness) restart(t *testing.T) *restartHarness {
	t.Helper()
	if err := h.database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}
	reopened, err := db.Open(h.path)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	return &restartHarness{path: h.path, database: reopened, repo: repository.New(reopened)}
}

type seededRun struct {
	sessionID          string
	runID              string
	userMessageID      string
	assistantMessageID string
	stateVersion       int64
}

func (h *restartHarness) seedQueuedRun(t *testing.T, title string) seededRun {
	t.Helper()
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, title)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "what happened?", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	state, err := h.repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	return seededRun{
		sessionID:          session.SessionID,
		runID:              enqueued.RunID,
		userMessageID:      enqueued.UserMessageID,
		assistantMessageID: enqueued.AssistantMessageID,
		stateVersion:       state.StateVersion,
	}
}

func (h *restartHarness) markRunning(t *testing.T, run *seededRun) {
	t.Helper()
	if err := h.repo.MarkRunRunning(context.Background(), run.runID); err != nil {
		t.Fatalf("MarkRunRunning: %v", err)
	}
	run.stateVersion = h.currentVersion(t, run.runID)
}

func (h *restartHarness) currentVersion(t *testing.T, runID string) int64 {
	t.Helper()
	state, err := h.repo.GetRunState(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	return state.StateVersion
}

// publicServices serves the two public read surfaces a reopened client uses,
// built fresh over whichever database file the harness currently holds. The
// tests call them over gRPC rather than calling the services in process, so
// what they assert is what actually crosses the wire.
func publicServices(t *testing.T, h *restartHarness) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	turingv1.RegisterSessionServiceServer(server, sessions.New(h.repo, config.Config{
		OllamaModel: "llama3.2",
	}, &restartCapabilities{}))
	turingv1.RegisterEventServiceServer(server, events.NewServer(h.repo, events.NewBus(8)))
	go func() {
		_ = server.Serve(listener)
	}()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial reopened services: %v", err)
	}
	conn.Connect()
	t.Cleanup(func() {
		server.Stop()
		_ = conn.Close()
	})
	return conn
}

// restartCapabilities is the smallest routing source SessionService needs. The
// reopened reads under test are history and events, neither of which consults
// it beyond building a capability response nobody asserts on.
type restartCapabilities struct{}

func (*restartCapabilities) ProviderCapabilities() map[turingv1.ModelProvider][]*turingv1.ModelCapability {
	return map[turingv1.ModelProvider][]*turingv1.ModelCapability{}
}

func (*restartCapabilities) AgentAvailable(turingv1.AgentId) bool { return true }

func (*restartCapabilities) RoutableDefaultModel(_ string, configured string) string {
	return configured
}

func (*restartCapabilities) LiveToolNames() []string { return nil }

func listMessagesOverGRPC(t *testing.T, h *restartHarness, sessionID string) []*turingv1.Message {
	t.Helper()
	response, err := turingv1.NewSessionServiceClient(publicServices(t, h)).ListMessages(
		context.Background(), &turingv1.ListMessagesRequest{SessionId: sessionID, Limit: 50})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	return response.GetMessages()
}

func listEventsOverGRPC(t *testing.T, h *restartHarness, sessionID string) []*turingv1.TuringEvent {
	t.Helper()
	response, err := turingv1.NewEventServiceClient(publicServices(t, h)).ListEvents(
		context.Background(), &turingv1.ListEventsRequest{SessionId: sessionID, Limit: 500})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	return response.GetEvents()
}

// reopenedMessageState reads the run state the way a reopened client does:
// through SessionService history on a freshly built service over the reopened
// file.
func reopenedMessageState(t *testing.T, h *restartHarness, run seededRun) *turingv1.RunState {
	t.Helper()
	messages := listMessagesOverGRPC(t, h, run.sessionID)
	for _, message := range messages {
		if message.GetMessageId() == run.assistantMessageID {
			return message.GetRunState()
		}
	}
	t.Fatalf("assistant message %s missing from reopened history", run.assistantMessageID)
	return nil
}

// reopenedEventStates reads every run state the public event replay carries for
// one run, in sequence order.
func reopenedEventStates(t *testing.T, h *restartHarness, run seededRun) []*turingv1.RunState {
	t.Helper()
	states := make([]*turingv1.RunState, 0, 8)
	for _, event := range listEventsOverGRPC(t, h, run.sessionID) {
		if event.GetRunId() != run.runID || event.GetRunState() == nil {
			continue
		}
		states = append(states, event.GetRunState())
	}
	return states
}

func latestEventState(t *testing.T, h *restartHarness, run seededRun) *turingv1.RunState {
	t.Helper()
	states := reopenedEventStates(t, h, run)
	if len(states) == 0 {
		return nil
	}
	return states[len(states)-1]
}

// TestEveryLifecycleAndTerminalReasonRoundTripsAfterDatabaseRestart is the
// table this task exists for: every public lifecycle, and every terminal reason
// the normative matrix allows, has to survive the file being closed and
// reopened and still reach a client through public gRPC.
func TestEveryLifecycleAndTerminalReasonRoundTripsAfterDatabaseRestart(t *testing.T) {
	tests := []struct {
		name          string
		seed          func(*testing.T, *restartHarness, *seededRun)
		lifecycle     turingv1.RunLifecycle
		reason        turingv1.RunOutcomeReason
		finished      bool
		hasContent    bool
		wantNoProduce bool
	}{
		{
			name:      "queued",
			seed:      func(*testing.T, *restartHarness, *seededRun) {},
			lifecycle: turingv1.RunLifecycle_RUN_LIFECYCLE_QUEUED,
			reason:    turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_NONE,
		},
		{
			name: "running",
			seed: func(t *testing.T, h *restartHarness, run *seededRun) {
				h.markRunning(t, run)
			},
			lifecycle: turingv1.RunLifecycle_RUN_LIFECYCLE_RUNNING,
			reason:    turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_NONE,
		},
		{
			name: "waiting approval",
			seed: func(t *testing.T, h *restartHarness, run *seededRun) {
				h.markRunning(t, run)
				h.awaitApproval(t, run)
			},
			lifecycle: turingv1.RunLifecycle_RUN_LIFECYCLE_WAITING_APPROVAL,
			reason:    turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_NONE,
		},
		{
			name: "recovering",
			seed: func(t *testing.T, h *restartHarness, run *seededRun) {
				h.markRunning(t, run)
				h.fenceOwnership(t, run)
			},
			lifecycle: turingv1.RunLifecycle_RUN_LIFECYCLE_RECOVERING,
			reason:    turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_NONE,
		},
		{
			name: "completed with content",
			seed: func(t *testing.T, h *restartHarness, run *seededRun) {
				h.markRunning(t, run)
				h.complete(t, run, "it finished")
			},
			lifecycle:  turingv1.RunLifecycle_RUN_LIFECYCLE_COMPLETED,
			reason:     turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_NONE,
			finished:   true,
			hasContent: true,
		},
		{
			name: "completed without content",
			seed: func(t *testing.T, h *restartHarness, run *seededRun) {
				h.markRunning(t, run)
				h.complete(t, run, "   ")
			},
			lifecycle: turingv1.RunLifecycle_RUN_LIFECYCLE_COMPLETED,
			reason:    turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_COMPLETED_NO_CONTENT,
			finished:  true,
		},
		{
			name: "cancelled abandoned",
			seed: func(t *testing.T, h *restartHarness, run *seededRun) {
				h.markRunning(t, run)
				h.cancel(t, run, runoutcome.AbandonedCancellation())
			},
			lifecycle: turingv1.RunLifecycle_RUN_LIFECYCLE_CANCELLED,
			reason:    turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_ABANDONED,
			finished:  true,
		},
		{
			// The reservation row. No current path writes user_cancelled — the
			// assertion below proves that — but a future explicit cancel-intent
			// RPC will, and a projection that could not carry it would silently
			// downgrade an honest claim of intent when that lands.
			name: "cancelled user cancelled reservation",
			seed: func(t *testing.T, h *restartHarness, run *seededRun) {
				h.markRunning(t, run)
				h.cancel(t, run, runoutcome.AbandonedCancellation())
				h.seedReservedOutcome(t, run, "cancelled", string(runoutcome.ReasonUserCancelled))
			},
			lifecycle:     turingv1.RunLifecycle_RUN_LIFECYCLE_CANCELLED,
			reason:        turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_USER_CANCELLED,
			finished:      true,
			wantNoProduce: true,
		},
	}
	for _, failure := range failedReasonInventory() {
		tests = append(tests, struct {
			name          string
			seed          func(*testing.T, *restartHarness, *seededRun)
			lifecycle     turingv1.RunLifecycle
			reason        turingv1.RunOutcomeReason
			finished      bool
			hasContent    bool
			wantNoProduce bool
		}{
			name: "failed " + failure.name,
			seed: func(t *testing.T, h *restartHarness, run *seededRun) {
				h.markRunning(t, run)
				h.fail(t, run, runoutcome.NormalizeFailure(failure.origin, failure.code, runoutcome.RetryClassNever))
			},
			lifecycle: turingv1.RunLifecycle_RUN_LIFECYCLE_FAILED,
			reason:    failure.public,
			finished:  true,
		})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			seeder := newRestartHarness(t)
			run := seeder.seedQueuedRun(t, test.name)
			test.seed(t, seeder, &run)
			committed := seeder.currentVersion(t, run.runID)
			reopened := seeder.restart(t)

			history := reopenedMessageState(t, reopened, run)
			if history == nil {
				t.Fatal("reopened history carries no run state")
			}
			assertRunStateShape(t, history, run, committed, test.lifecycle, test.reason, test.finished, test.hasContent)

			replayed := latestEventState(t, reopened, run)
			if replayed == nil {
				t.Fatal("reopened event replay carries no run state")
			}
			assertRunStateShape(t, replayed, run, committed, test.lifecycle, test.reason, test.finished, test.hasContent)
			if !proto.Equal(history, replayed) {
				t.Fatalf("history state %+v and replayed state %+v disagree", history, replayed)
			}
			if test.wantNoProduce {
				assertNoUserCancelledProducer(t)
			}
		})
	}
}

type failedReason struct {
	name   string
	origin runoutcome.Origin
	code   string
	public turingv1.RunOutcomeReason
}

// failedReasonInventory is every failure outcome the normative matrix allows a
// failed run to carry, each reached through a real typed producer pair rather
// than by writing the reason string into the row.
func failedReasonInventory() []failedReason {
	return []failedReason{
		{"expired", runoutcome.OriginApprovalExpiry, "approval_expired", turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_EXPIRED},
		{"context limit", runoutcome.OriginContextAssembly, "context_budget_exceeded", turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_CONTEXT_LIMIT},
		{"provider failure", runoutcome.OriginProviderTransport, "model_timeout", turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_PROVIDER_FAILURE},
		{"tool failure", runoutcome.OriginToolExecution, "tool_call_failed", turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_TOOL_FAILURE},
		{"policy denied", runoutcome.OriginAutomationPolicy, "automation_approval_failed", turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_POLICY_DENIED},
		{"retries exhausted", runoutcome.OriginDispatch, "retries_exhausted", turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_RETRIES_EXHAUSTED},
		{"recovery interrupted", runoutcome.OriginRecovery, "job_timeout", turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_RECOVERY_INTERRUPTED},
		{"side effect uncertain", runoutcome.OriginRecovery, "side_effect_uncertain", turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_SIDE_EFFECT_UNCERTAIN},
		{"approval delivery failed", runoutcome.OriginApprovalTransport, "approval_delivery_failed", turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_APPROVAL_DELIVERY_FAILED},
		{"internal failure", runoutcome.OriginWorkerRuntime, "runtime_error", turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_INTERNAL_FAILURE},
	}
}

func assertRunStateShape(
	t *testing.T,
	state *turingv1.RunState,
	run seededRun,
	version int64,
	lifecycle turingv1.RunLifecycle,
	reason turingv1.RunOutcomeReason,
	finished bool,
	hasContent bool,
) {
	t.Helper()
	if state.GetRunId() != run.runID || state.GetUserMessageId() != run.userMessageID ||
		state.GetAssistantMessageId() != run.assistantMessageID {
		t.Fatalf("run state identity = %+v, want the seeded run %s", state, run.runID)
	}
	if state.GetLifecycle() != lifecycle || state.GetOutcomeReason() != reason {
		t.Fatalf("outcome = %v/%v, want %v/%v", state.GetLifecycle(), state.GetOutcomeReason(), lifecycle, reason)
	}
	if state.GetStateVersion() != version {
		t.Fatalf("state version = %d, want the committed %d", state.GetStateVersion(), version)
	}
	if state.GetStateUpdatedAt() == nil {
		t.Fatal("run state carries no state_updated_at")
	}
	if finished != (state.GetFinishedAt() != nil) {
		t.Fatalf("finished_at = %v, want present=%v", state.GetFinishedAt(), finished)
	}
	if state.GetHasDisplayableContent() != hasContent {
		t.Fatalf("has_displayable_content = %v, want %v", state.GetHasDisplayableContent(), hasContent)
	}
}

// TestCompletedWithoutContentRoundTripsWithoutAssistantText proves the silent
// success is legible as one: the run completed, the reason says there was
// nothing to show, and no filler was invented to fill the message.
func TestCompletedWithoutContentRoundTripsWithoutAssistantText(t *testing.T) {
	seeder := newRestartHarness(t)
	run := seeder.seedQueuedRun(t, "Silent success")
	seeder.markRunning(t, &run)
	seeder.complete(t, &run, "")
	committed := seeder.currentVersion(t, run.runID)
	reopened := seeder.restart(t)

	var assistant *turingv1.Message
	for _, message := range listMessagesOverGRPC(t, reopened, run.sessionID) {
		if message.GetMessageId() == run.assistantMessageID {
			assistant = message
		}
	}
	if assistant == nil {
		t.Fatal("reopened history lost the assistant message")
	}
	if strings.TrimSpace(assistant.GetContent()) != "" {
		t.Fatalf("assistant content = %q, want nothing invented", assistant.GetContent())
	}
	state := assistant.GetRunState()
	if state == nil {
		t.Fatal("a completed run with no content carries no run state")
	}
	assertRunStateShape(t, state, run, committed,
		turingv1.RunLifecycle_RUN_LIFECYCLE_COMPLETED,
		turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_COMPLETED_NO_CONTENT, true, false)
	replayed := latestEventState(t, reopened, run)
	if replayed == nil {
		t.Fatal("the completion event carries no run state")
	}
	if !proto.Equal(state, replayed) {
		t.Fatalf("history state %+v and completion event state %+v disagree", state, replayed)
	}
}

// TestRecoveringAndWaitingStatesRoundTripAfterRestart pins the two nonterminal
// phases a reopened session used to report as running: a run whose worker is
// unproven, and one waiting on a human.
func TestRecoveringAndWaitingStatesRoundTripAfterRestart(t *testing.T) {
	tests := []struct {
		name      string
		seed      func(*testing.T, *restartHarness, *seededRun)
		lifecycle turingv1.RunLifecycle
	}{
		{
			name: "recovering",
			seed: func(t *testing.T, h *restartHarness, run *seededRun) {
				h.markRunning(t, run)
				h.fenceOwnership(t, run)
			},
			lifecycle: turingv1.RunLifecycle_RUN_LIFECYCLE_RECOVERING,
		},
		{
			name: "waiting approval",
			seed: func(t *testing.T, h *restartHarness, run *seededRun) {
				h.markRunning(t, run)
				h.awaitApproval(t, run)
			},
			lifecycle: turingv1.RunLifecycle_RUN_LIFECYCLE_WAITING_APPROVAL,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			seeder := newRestartHarness(t)
			run := seeder.seedQueuedRun(t, test.name)
			test.seed(t, seeder, &run)
			committed := seeder.currentVersion(t, run.runID)
			reopened := seeder.restart(t)

			state := reopenedMessageState(t, reopened, run)
			if state == nil {
				t.Fatalf("%s run carries no reopened state", test.name)
			}
			assertRunStateShape(t, state, run, committed, test.lifecycle,
				turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_NONE, false, false)
			if state.GetFinishedAt() != nil {
				t.Fatalf("a %s run reports finished_at %v", test.name, state.GetFinishedAt())
			}
			// The lifecycle event that committed the phase has to say the same
			// thing: a live watcher and a reopened one must not disagree about
			// whether anybody is executing this run.
			replayed := latestEventState(t, reopened, run)
			if replayed == nil {
				t.Fatalf("the %s transition published no run state", test.name)
			}
			if !proto.Equal(state, replayed) {
				t.Fatalf("history state %+v and event state %+v disagree", state, replayed)
			}
		})
	}
}

// TestTerminalRowWithoutTerminalEventStillReopensFromCanonicalState is the
// point of making the run row canonical: history reads the row, not a replay of
// events that may have been pruned or never written.
func TestTerminalRowWithoutTerminalEventStillReopensFromCanonicalState(t *testing.T) {
	seeder := newRestartHarness(t)
	run := seeder.seedQueuedRun(t, "Row without event")
	seeder.markRunning(t, &run)
	seeder.fail(t, &run, runoutcome.NormalizeFailure(runoutcome.OriginProviderTransport, "model_timeout", runoutcome.RetryClassNever))
	committed := seeder.currentVersion(t, run.runID)
	if _, err := seeder.database.ExecContext(context.Background(),
		`DELETE FROM events WHERE run_id = ? AND type = 'agent.run.failed'`, run.runID); err != nil {
		t.Fatalf("delete terminal event: %v", err)
	}
	reopened := seeder.restart(t)

	state := reopenedMessageState(t, reopened, run)
	if state == nil {
		t.Fatal("a terminal run with no terminal event carries no state")
	}
	assertRunStateShape(t, state, run, committed,
		turingv1.RunLifecycle_RUN_LIFECYCLE_FAILED,
		turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_PROVIDER_FAILURE, true, false)
	// The events that remain still carry their own committed snapshots, and
	// none of them claims the run is finished — which is exactly why the
	// terminal answer has to come from the row rather than from a replay.
	replayed := reopenedEventStates(t, reopened, run)
	if len(replayed) == 0 {
		t.Fatal("the surviving lifecycle events carry no run state")
	}
	for _, published := range replayed {
		if published.GetLifecycle() == turingv1.RunLifecycle_RUN_LIFECYCLE_FAILED {
			t.Fatalf("a terminal state survived the deleted event: %+v", published)
		}
	}
}

// TestChatLiveAndReopenedHistoryCarryIdenticalRunState is the disagreement this
// task removes: the state a live ChatStream reports for a terminal run and the
// state a reopened session reads for the same run must be the same value.
func TestChatLiveAndReopenedHistoryCarryIdenticalRunState(t *testing.T) {
	seeder := newRestartHarness(t)
	h := newHarnessWithDatabase(t, seeder.database)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	session := h.createSession(t)
	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId: session, Content: "live and reopened",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	first, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv queued: %v", err)
	}
	queued := first.GetRunQueued()
	if queued == nil {
		t.Fatalf("first event = %T, want run_queued", first.Event)
	}
	run := seededRun{sessionID: session, runID: queued.GetRunId()}
	runRow, err := seeder.repo.GetRun(context.Background(), run.runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	run.assistantMessageID = runRow.AssistantMessageID
	running := seeder.awaitLifecycle(t, run.runID, "running")
	run.userMessageID = running.UserMessageID
	run.stateVersion = running.StateVersion
	seeder.complete(t, &run, "live answer")

	var liveState *turingv1.RunState
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		if completed := event.GetRunCompleted(); completed != nil {
			liveState = completed.GetRunState()
			break
		}
	}
	if liveState == nil {
		t.Fatal("the live completion carried no run state")
	}

	reopened := seeder.restart(t)
	historyState := reopenedMessageState(t, reopened, run)
	if historyState == nil {
		t.Fatal("reopened history carries no run state")
	}
	if !proto.Equal(liveState, historyState) {
		t.Fatalf("live state %+v and reopened state %+v disagree", liveState, historyState)
	}
}

// awaitLifecycle waits for a run a real dispatcher is moving to reach a phase,
// so a test that drives the rest of the lifecycle itself cannot race the claim.
func (h *restartHarness) awaitLifecycle(t *testing.T, runID string, lifecycle string) repository.RunState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		state, err := h.repo.GetRunState(context.Background(), runID)
		if err != nil {
			t.Fatalf("GetRunState: %v", err)
		}
		if state.Lifecycle == lifecycle {
			return state
		}
		if time.Now().After(deadline) {
			t.Fatalf("run lifecycle = %q, want %q", state.Lifecycle, lifecycle)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestSendMessageIdempotentReplayKeepsOriginalRunAndStateIdentity pins what a
// retried send is allowed to say: the same run, and the same state identity —
// never a second run, and never a state a client would reconcile as newer.
func TestSendMessageIdempotentReplayKeepsOriginalRunAndStateIdentity(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	session := h.createSession(t)
	request := &turingv1.SendMessageRequest{
		SessionId: session, Content: "retry me", IdempotencyKey: "key_identity",
	}
	first := firstQueuedEvent(t, h, request)
	second := firstQueuedEvent(t, h, request)
	if first.GetRunId() != second.GetRunId() {
		t.Fatalf("replayed run id = %q, want the original %q", second.GetRunId(), first.GetRunId())
	}
	firstState, secondState := first.GetRunState(), second.GetRunState()
	if firstState == nil || secondState == nil {
		t.Fatalf("queued run states = %+v / %+v, want both present", firstState, secondState)
	}
	if !proto.Equal(firstState, secondState) {
		t.Fatalf("replayed state %+v is not the original %+v", secondState, firstState)
	}
	if firstState.GetLifecycle() != turingv1.RunLifecycle_RUN_LIFECYCLE_QUEUED || firstState.GetStateVersion() != 1 {
		t.Fatalf("queued state = %+v, want the queued transition this event reports", firstState)
	}
}

func firstQueuedEvent(t *testing.T, h *harness, request *turingv1.SendMessageRequest) *turingv1.RunQueued {
	t.Helper()
	ctx, cancel := context.WithTimeout(h.clientContext(), 5*time.Second)
	stream, err := h.chatClient.SendMessage(ctx, request)
	if err != nil {
		cancel()
		t.Fatalf("SendMessage: %v", err)
	}
	t.Cleanup(cancel)
	event, err := stream.Recv()
	if err != nil {
		t.Fatalf("recv queued: %v", err)
	}
	queued := event.GetRunQueued()
	if queued == nil {
		t.Fatalf("first event = %T, want run_queued", event.Event)
	}
	return queued
}

// TestChatDirectRunQueuedCarriesVersionOneRunState covers the one lifecycle
// event the initiating client never receives from the bus: its own queued
// event is already marked sent, so without the direct send it would learn the
// run's first version only when something else moved it.
func TestChatDirectRunQueuedCarriesVersionOneRunState(t *testing.T) {
	h := newHarness(t)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	session := h.createSession(t)
	queued := firstQueuedEvent(t, h, &turingv1.SendMessageRequest{SessionId: session, Content: "queued state"})
	state := queued.GetRunState()
	if state == nil {
		t.Fatal("the direct queued event carries no run state")
	}
	if state.GetRunId() != queued.GetRunId() {
		t.Fatalf("queued state run id = %q, want %q", state.GetRunId(), queued.GetRunId())
	}
	if state.GetLifecycle() != turingv1.RunLifecycle_RUN_LIFECYCLE_QUEUED ||
		state.GetOutcomeReason() != turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_NONE {
		t.Fatalf("queued outcome = %v/%v", state.GetLifecycle(), state.GetOutcomeReason())
	}
	if state.GetStateVersion() != 1 {
		t.Fatalf("queued state version = %d, want the run's first version", state.GetStateVersion())
	}
	if state.GetFinishedAt() != nil {
		t.Fatalf("a queued run reports finished_at %v", state.GetFinishedAt())
	}
}

// TestChatAndEventTypeMappersMapRunStateChangedToTwentyThree pins the durable
// type onto its allocated public value in both mappers. Both normalize
// underscores to dots before switching, so the case literal they need is the
// dotted one even though the durable type keeps its underscore.
func TestChatAndEventTypeMappersMapRunStateChangedToTwentyThree(t *testing.T) {
	if got := mapEventType("agent.run.state_changed"); got != turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED {
		t.Fatalf("chat.mapEventType = %v, want AGENT_RUN_STATE_CHANGED", got)
	}
	if int32(turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED) != 23 {
		t.Fatalf("state-changed event type = %d, want the allocated 23",
			turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED)
	}

	seeder := newRestartHarness(t)
	run := seeder.seedQueuedRun(t, "State changed type")
	seeder.markRunning(t, &run)
	seeder.fenceOwnership(t, &run)
	// Both transitions the seed made — the direct queued-to-running start and
	// the ownership fence — project the shared state-changed event, so the
	// replay carries two of them and the newest is the fence.
	var stateChanges []*turingv1.RunState
	for _, event := range listEventsOverGRPC(t, seeder, run.sessionID) {
		if event.GetType() != turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED {
			continue
		}
		stateChanges = append(stateChanges, event.GetRunState())
	}
	if len(stateChanges) == 0 {
		t.Fatal("EventService replay never mapped the durable state-changed event onto its type")
	}
	newest := stateChanges[len(stateChanges)-1]
	if newest.GetLifecycle() != turingv1.RunLifecycle_RUN_LIFECYCLE_RECOVERING {
		t.Fatalf("newest state-changed event state = %+v, want recovering", newest)
	}
}

// TestRunStateChangedIsNotTerminal keeps a lifecycle projection from ending a
// client's stream. Entering recovery is news, not an ending.
func TestRunStateChangedIsNotTerminal(t *testing.T) {
	if isTerminalEvent("agent.run.state_changed") {
		t.Fatal("agent.run.state_changed is treated as terminal")
	}
	mapped := mapChatEvent(events.Event{
		SessionID:   "sess_1",
		RunID:       "run_1",
		TraceID:     "trace_1",
		Sequence:    9,
		Type:        "agent.run.state_changed",
		CreatedAt:   "2026-08-20T00:00:00Z",
		PayloadJSON: `{"runState":{"runId":"run_1","userMessageId":"msg_user","assistantMessageId":"msg_assistant","lifecycle":"recovering","outcomeReason":"none","stateVersion":3,"stateUpdatedAt":"2026-08-20T00:00:00.000000000Z","hasDisplayableContent":false}}`,
	})
	changed := mapped.GetRunStateChanged()
	if changed == nil {
		t.Fatalf("mapped event = %T, want run_state_changed", mapped.Event)
	}
	state := changed.GetRunState()
	if state.GetLifecycle() != turingv1.RunLifecycle_RUN_LIFECYCLE_RECOVERING || state.GetStateVersion() != 3 {
		t.Fatalf("state-changed state = %+v, want recovering at version 3", state)
	}
}

// TestChatPersistedEventCarriesRunStateForApprovalAndStateChanged covers the
// arm that carries every event without a dedicated union: an approval that
// moved the run, and the shared state-changed projection, must reach a client
// with the same snapshot the dedicated unions carry.
func TestChatPersistedEventCarriesRunStateForApprovalAndStateChanged(t *testing.T) {
	seeder := newRestartHarness(t)
	run := seeder.seedQueuedRun(t, "Persisted arm")
	seeder.markRunning(t, &run)
	seeder.awaitApproval(t, &run)
	waitingVersion := seeder.currentVersion(t, run.runID)
	seeder.fenceOwnership(t, &run)
	recoveringVersion := seeder.currentVersion(t, run.runID)

	rows, _, err := seeder.repo.ReplayEvents(context.Background(), run.sessionID, 0, 100)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}
	var sawApproval, sawStateChanged bool
	for _, row := range rows {
		if !row.RunID.Valid || row.RunID.String != run.runID {
			continue
		}
		switch row.Type {
		case "approval.requested":
			sawApproval = true
			mapped := mapChatEvent(busEventFromRepository(row))
			persisted := mapped.GetPersistedEvent()
			if persisted == nil {
				t.Fatalf("approval event mapped to %T, want persisted_event", mapped.Event)
			}
			state := persisted.GetRunState()
			if state.GetLifecycle() != turingv1.RunLifecycle_RUN_LIFECYCLE_WAITING_APPROVAL ||
				state.GetStateVersion() != waitingVersion {
				t.Fatalf("approval persisted state = %+v, want waiting approval at %d", state, waitingVersion)
			}
		case "agent.run.state_changed":
			built := persistedEvent(busEventFromRepository(row), events.Decode(row.Type, row.PayloadJSON))
			state := built.GetRunState()
			if state == nil {
				t.Fatal("a state-changed event reached the persisted arm without its snapshot")
			}
			if built.GetType() != turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED {
				t.Fatalf("persisted state-changed type = %v", built.GetType())
			}
			// The seed makes two of these — the direct start and the fence.
			// Only the fence is asserted against its committed version; the
			// start proves the same arm carries a nonterminal snapshot too.
			if state.GetStateVersion() != recoveringVersion {
				continue
			}
			sawStateChanged = true
			if state.GetLifecycle() != turingv1.RunLifecycle_RUN_LIFECYCLE_RECOVERING {
				t.Fatalf("state-changed persisted state = %+v, want recovering at %d", state, recoveringVersion)
			}
		}
	}
	if !sawApproval || !sawStateChanged {
		t.Fatalf("seeded events approval=%v state_changed=%v, want both", sawApproval, sawStateChanged)
	}
}

// TestAmbiguousCancellationStaysAbandonedAcrossLiveReplayAndReopen is the
// honesty check. The one signal this product has covers a deliberate stop and a
// dropped connection alike, so every surface has to say abandoned — the live
// stream, the replayed event, and the reopened history.
func TestAmbiguousCancellationStaysAbandonedAcrossLiveReplayAndReopen(t *testing.T) {
	seeder := newRestartHarness(t)
	h := newHarnessWithDatabase(t, seeder.database)
	worker := connectChatTestWorker(t, h, defaultChatWorkerCapabilities(false))
	defer func() { _ = worker.CloseSend() }()
	session := h.createSession(t)
	ctx, cancel := context.WithCancel(h.clientContext())
	stream, err := h.chatClient.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId: session, Content: "abandon me",
	})
	if err != nil {
		cancel()
		t.Fatalf("SendMessage: %v", err)
	}
	event, err := stream.Recv()
	if err != nil {
		cancel()
		t.Fatalf("recv queued: %v", err)
	}
	runID := event.GetRunQueued().GetRunId()
	cancel()

	deadline := time.Now().Add(5 * time.Second)
	var state repository.RunState
	for {
		state, err = seeder.repo.GetRunState(context.Background(), runID)
		if err != nil {
			t.Fatalf("GetRunState: %v", err)
		}
		if state.Lifecycle == "cancelled" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run lifecycle = %q, want the disconnect to terminalize it", state.Lifecycle)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if state.OutcomeReason != string(runoutcome.ReasonAbandoned) {
		t.Fatalf("outcome reason = %q, want abandoned", state.OutcomeReason)
	}

	runRow, err := seeder.repo.GetRun(context.Background(), runID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	seeded := seededRun{
		sessionID:          session,
		runID:              runID,
		userMessageID:      state.UserMessageID,
		assistantMessageID: runRow.AssistantMessageID,
	}
	replayed := latestEventState(t, seeder, seeded)
	if replayed.GetOutcomeReason() != turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_ABANDONED {
		t.Fatalf("replayed outcome = %v, want abandoned", replayed.GetOutcomeReason())
	}

	// A row the migration never reached is the remaining shape: the transport's
	// own word for what happened, stored before any of this existed. The read
	// boundary answers it with the fixed generic value rather than promoting it
	// to a claim about what the user wanted.
	legacy := mapChatEvent(events.Event{
		SessionID: session, RunID: runID, TraceID: "trace_legacy", Sequence: 999,
		Type:        "agent.run.cancelled",
		PayloadJSON: `{"runId":"` + runID + `","reason":"client_cancelled","message":"stream gone"}`,
	})
	unmigrated := legacy.GetRunCancelled()
	if unmigrated == nil {
		t.Fatalf("unmigrated cancellation mapped to %T, want run_cancelled", legacy.Event)
	}
	if unmigrated.GetReason() != legacyCancellationReason {
		t.Fatalf("unmigrated cancellation reason = %q, want the fixed generic value", unmigrated.GetReason())
	}
	if unmigrated.GetRunState() != nil {
		t.Fatalf("a row with no snapshot produced a state %+v", unmigrated.GetRunState())
	}

	reopened := seeder.restart(t)
	history := reopenedMessageState(t, reopened, seeded)
	if history.GetOutcomeReason() != turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_ABANDONED {
		t.Fatalf("reopened outcome = %v, want abandoned", history.GetOutcomeReason())
	}
	if history.GetLifecycle() != turingv1.RunLifecycle_RUN_LIFECYCLE_CANCELLED {
		t.Fatalf("reopened lifecycle = %v, want cancelled", history.GetLifecycle())
	}
	// Nothing the session can serve claims intent anywhere: not a payload, not
	// a projected outcome.
	for _, event := range listEventsOverGRPC(t, reopened, session) {
		if event.GetRunState().GetOutcomeReason() == turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_USER_CANCELLED {
			t.Fatalf("public event %v claims the user cancelled", event.GetType())
		}
		for key, value := range event.GetPayload().AsMap() {
			if text, ok := value.(string); ok && strings.Contains(text, "user_cancelled") {
				t.Fatalf("public payload key %q claims the user cancelled", key)
			}
		}
	}
	assertNoUserCancelledProducer(t)
}

// orchestratorRoot is this module's own source tree: internal packages and the
// binary that wires them. The scan below walks all of it rather than the one
// package under test, because "no current path produces this" is only worth
// asserting if it covers every path.
func orchestratorRoot() string { return filepath.Join("..", "..", "..") }

// userCancelledVocabularyFiles are the files allowed to name the reserved
// outcome at all: the vocabulary that defines it, the two matrices that list
// which reasons a cancelled run may hold, and the migration's stored-value
// guard. None of them writes it onto a run.
func userCancelledVocabularyFiles() map[string]string {
	root := orchestratorRoot()
	return map[string]string{
		filepath.Join(root, "internal", "runoutcome", "outcome.go"):             "the closed vocabulary constant",
		filepath.Join(root, "internal", "repository", "run_state.go"):           "the writer's lifecycle/reason matrix",
		filepath.Join(root, "internal", "service", "runstate", "projection.go"): "the reader's lifecycle/reason matrix",
		filepath.Join(root, "internal", "db", "run_outcomes_migration.go"):      "the migration's stored-value guard",
	}
}

// assertNoUserCancelledProducer reads the orchestrator's own source and refuses
// any use of the reserved outcome outside the vocabulary and matrices above.
// The projection has to carry it, because a future explicit cancel-intent RPC
// will mean it — but nothing today may claim a user meant to stop a run when
// all this product observed was a socket closing.
func assertNoUserCancelledProducer(t *testing.T) {
	t.Helper()
	allowed := userCancelledVocabularyFiles()
	err := filepath.WalkDir(orchestratorRoot(), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(body), "user_cancelled") && !strings.Contains(string(body), "ReasonUserCancelled") {
			return nil
		}
		if _, vocabulary := allowed[path]; !vocabulary {
			t.Errorf("%s names the reserved user-cancelled outcome, which no current path may produce", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan orchestrator source: %v", err)
	}
	for file, role := range allowed {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s (%s): %v", file, role, err)
		}
		if !strings.Contains(string(body), "user_cancelled") && !strings.Contains(string(body), "ReasonUserCancelled") {
			t.Errorf("%s no longer names the reserved outcome it exists to hold (%s)", file, role)
		}
	}
	assertNoUserCancelledConstructor(t)
}

// assertNoUserCancelledConstructor proves the vocabulary file itself exposes no
// way to build the reserved cancellation. A constant nobody can turn into a
// value is a reservation; a constructor would be a producer.
func assertNoUserCancelledConstructor(t *testing.T) {
	t.Helper()
	path := filepath.Join(orchestratorRoot(), "internal", "runoutcome", "outcome.go")
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			identifier, ok := node.(*ast.Ident)
			if ok && identifier.Name == "ReasonUserCancelled" {
				t.Errorf("%s builds the reserved user-cancelled outcome in %s",
					fileSet.Position(identifier.Pos()), function.Name.Name)
			}
			literal, ok := node.(*ast.BasicLit)
			if ok && literal.Kind == token.STRING {
				value, err := strconv.Unquote(literal.Value)
				if err == nil && value == "user_cancelled" {
					t.Errorf("%s builds the reserved user-cancelled outcome in %s",
						fileSet.Position(literal.Pos()), function.Name.Name)
				}
			}
			return true
		})
	}
}

func (h *restartHarness) awaitApproval(t *testing.T, run *seededRun) {
	t.Helper()
	if _, _, err := h.repo.CreateApprovalWithEvent(context.Background(), run.runID, "", "general_assistant",
		"system.shell", `{"command":"rm -rf /"}`, "hash_await",
		time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("CreateApprovalWithEvent: %v", err)
	}
	run.stateVersion = h.currentVersion(t, run.runID)
}

func (h *restartHarness) fenceOwnership(t *testing.T, run *seededRun) {
	t.Helper()
	if _, err := h.repo.FenceRunOwnership(context.Background(), repository.FenceRunOwnershipInput{
		RunID: run.runID, ExpectedStateVersion: run.stateVersion,
	}); err != nil {
		t.Fatalf("FenceRunOwnership: %v", err)
	}
	run.stateVersion = h.currentVersion(t, run.runID)
}

func (h *restartHarness) complete(t *testing.T, run *seededRun, content string) {
	t.Helper()
	if _, err := h.repo.CompleteRunCanonical(context.Background(), repository.CompleteRunInput{
		RunID:                run.runID,
		AssistantMessageID:   run.assistantMessageID,
		Content:              content,
		ExpectedStateVersion: run.stateVersion,
	}); err != nil {
		t.Fatalf("CompleteRunCanonical: %v", err)
	}
	run.stateVersion = h.currentVersion(t, run.runID)
}

func (h *restartHarness) fail(t *testing.T, run *seededRun, failure runoutcome.Failure) {
	t.Helper()
	if _, err := h.repo.FailRunCanonical(context.Background(), repository.FailRunInput{
		RunID:                run.runID,
		AssistantMessageID:   run.assistantMessageID,
		ExpectedStateVersion: run.stateVersion,
		Failure:              failure,
	}); err != nil {
		t.Fatalf("FailRunCanonical: %v", err)
	}
	run.stateVersion = h.currentVersion(t, run.runID)
}

func (h *restartHarness) cancel(t *testing.T, run *seededRun, cancellation runoutcome.Cancellation) {
	t.Helper()
	if _, err := h.repo.CancelRunCanonical(context.Background(), repository.CancelRunInput{
		RunID:                run.runID,
		AssistantMessageID:   run.assistantMessageID,
		ExpectedStateVersion: run.stateVersion,
		Cancellation:         cancellation,
	}); err != nil {
		t.Fatalf("CancelRunCanonical: %v", err)
	}
	run.stateVersion = h.currentVersion(t, run.runID)
}

// seedReservedOutcome writes an outcome no current writer produces directly
// into the row and its terminal event, which is the only way to prove the read
// path can carry a reservation. It is a test fixture rather than a repository
// API on purpose: giving production code a way to write this value is exactly
// what the honesty rule forbids.
func (h *restartHarness) seedReservedOutcome(t *testing.T, run *seededRun, lifecycle string, reason string) {
	t.Helper()
	ctx := context.Background()
	if _, err := h.database.ExecContext(ctx,
		`UPDATE agent_runs SET status = ?, outcome_reason = ? WHERE id = ?`, lifecycle, reason, run.runID); err != nil {
		t.Fatalf("seed reserved outcome: %v", err)
	}
	rows, err := h.database.QueryContext(ctx,
		`SELECT id, payload_json FROM events WHERE run_id = ? AND payload_json LIKE '%runState%'`, run.runID)
	if err != nil {
		t.Fatalf("read seeded events: %v", err)
	}
	defer func() { _ = rows.Close() }()
	updates := map[string]string{}
	for rows.Next() {
		var id, payloadJSON string
		if err := rows.Scan(&id, &payloadJSON); err != nil {
			t.Fatalf("scan seeded event: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
			t.Fatalf("decode seeded payload: %v", err)
		}
		snapshot, ok := payload["runState"].(map[string]any)
		if !ok {
			continue
		}
		snapshot["lifecycle"] = lifecycle
		snapshot["outcomeReason"] = reason
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("encode seeded payload: %v", err)
		}
		updates[id] = string(encoded)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate seeded events: %v", err)
	}
	for id, payloadJSON := range updates {
		if _, err := h.database.ExecContext(ctx,
			`UPDATE events SET payload_json = ? WHERE id = ?`, payloadJSON, id); err != nil {
			t.Fatalf("rewrite seeded event: %v", err)
		}
	}
}

// chatRawDiagnostics is what an unmigrated row could still be holding. None of
// it may reach a ChatStream event, whatever arm maps it.
var chatRawDiagnostics = []string{
	"connection refused by ollama at 127.0.0.1:11434",
	"/Users/someone/secrets/private.key",
	`{"command":"rm -rf /Users/someone"}`,
	"denied because this would email the whole company",
	"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.token",
}

// TestChatServiceSanitizesMalformedLegacyFailureEvents pins the arm that used
// to hand a client the JSON parser's own sentence. An unreadable payload is a
// fact about this server, not a diagnostic anybody may render.
func TestChatServiceSanitizesMalformedLegacyFailureEvents(t *testing.T) {
	for _, eventType := range []string{
		"agent.run.failed", "agent.run.cancelled", "agent.run.step", "agent.run.state_changed",
		"approval.denied", "approval.expired", "tool.call.failed", "tool.call.denied", "message.delta",
	} {
		t.Run(eventType, func(t *testing.T) {
			mapped := mapChatEvent(events.Event{
				SessionID: "sess_1", RunID: "run_1", TraceID: "trace_1", Sequence: 4,
				Type:        eventType,
				PayloadJSON: `{"code":"model_error","message":"connection refused by ollama at 127.0.0.1:11434"`,
			})
			assertChatEventCarriesNoRawDiagnostics(t, mapped)
			if message := mapped.GetRunFailed().GetMessage(); message != "" {
				t.Fatalf("unparseable payload produced the message %q", message)
			}
			if eventType == "message.delta" && mapped.GetTokenDelta() == nil {
				t.Fatalf("an unparseable delta mapped to %T, want the event type it actually is", mapped.Event)
			}
			if persisted := mapped.GetPersistedEvent(); persisted != nil {
				if len(persisted.GetPayload().GetFields()) != 0 {
					t.Fatalf("unparseable payload produced fields %s", persisted.GetPayload())
				}
				if persisted.GetRunState() != nil {
					t.Fatalf("unparseable payload produced a run state %+v", persisted.GetRunState())
				}
			}
		})
	}
}

// TestChatPublicFailureEventsNeverExposeRawDiagnostics is the same rule for a
// row that parses but was never migrated. The legacy code, message and reason
// fields are answered with fixed generic values; a new client reads RunState.
func TestChatPublicFailureEventsNeverExposeRawDiagnostics(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		payload   string
		assertFn  func(*testing.T, *turingv1.ChatStreamEvent)
	}{
		{
			name:      "run failed",
			eventType: "agent.run.failed",
			payload: `{"runId":"run_1","code":"model_error","retryable":true,` +
				`"message":"connection refused by ollama at 127.0.0.1:11434"}`,
			assertFn: func(t *testing.T, got *turingv1.ChatStreamEvent) {
				t.Helper()
				failed := got.GetRunFailed()
				if failed == nil {
					t.Fatalf("event = %T, want run_failed", got.Event)
				}
				if failed.GetRunId() != "run_1" {
					t.Fatalf("run_failed run id = %q", failed.GetRunId())
				}
				if failed.GetMessage() != "" {
					t.Fatalf("run_failed message = %q, want nothing", failed.GetMessage())
				}
				if failed.GetCode() == "model_error" {
					t.Fatalf("run_failed code = %q, want a fixed generic value", failed.GetCode())
				}
				retryable := failed.ProtoReflect().Descriptor().Fields().ByNumber(4)
				if retryable == nil || failed.ProtoReflect().Get(retryable).Bool() {
					t.Fatalf("deprecated retryable = %v, want a fixed false", failed)
				}
			},
		},
		{
			name:      "run cancelled",
			eventType: "agent.run.cancelled",
			payload:   `{"runId":"run_1","reason":"denied because this would email the whole company"}`,
			assertFn: func(t *testing.T, got *turingv1.ChatStreamEvent) {
				t.Helper()
				cancelled := got.GetRunCancelled()
				if cancelled == nil {
					t.Fatalf("event = %T, want run_cancelled", got.Event)
				}
				if cancelled.GetRunId() != "run_1" {
					t.Fatalf("run_cancelled run id = %q", cancelled.GetRunId())
				}
				if strings.Contains(cancelled.GetReason(), "denied because") {
					t.Fatalf("run_cancelled reason = %q, want a fixed generic value", cancelled.GetReason())
				}
			},
		},
		{
			name:      "run step retry",
			eventType: "agent.run.step",
			payload:   `{"note":"retrying after connection refused by ollama at 127.0.0.1:11434","attempt":2,"maxAttempts":3}`,
			assertFn: func(t *testing.T, got *turingv1.ChatStreamEvent) {
				t.Helper()
				persisted := got.GetPersistedEvent()
				if persisted == nil {
					t.Fatalf("event = %T, want persisted_event", got.Event)
				}
				if got := persisted.GetPayload().GetFields()["category"].GetStringValue(); got != "dispatch_retry" {
					t.Fatalf("category = %q, want dispatch_retry", got)
				}
			},
		},
		{
			name:      "approval denied",
			eventType: "approval.denied",
			payload: `{"approvalId":"appr_1","toolName":"system.shell","runId":"run_1",` +
				`"message":"denied because this would email the whole company",` +
				`"approvalToken":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.token"}`,
			assertFn: func(t *testing.T, got *turingv1.ChatStreamEvent) {
				t.Helper()
				persisted := got.GetPersistedEvent()
				if persisted == nil {
					t.Fatalf("event = %T, want persisted_event", got.Event)
				}
				if got := persisted.GetPayload().GetFields()["approvalId"].GetStringValue(); got != "appr_1" {
					t.Fatalf("approvalId = %q, want the identity the contract already promised", got)
				}
			},
		},
		{
			name:      "tool call failed",
			eventType: "tool.call.failed",
			payload: `{"toolCallId":"call_1","toolName":"files.read","serverName":"files",` +
				`"error":"/Users/someone/secrets/private.key","args":{"command":"rm -rf /Users/someone"}}`,
			assertFn: func(t *testing.T, got *turingv1.ChatStreamEvent) {
				t.Helper()
				persisted := got.GetPersistedEvent()
				if persisted == nil {
					t.Fatalf("event = %T, want persisted_event", got.Event)
				}
				if got := persisted.GetPayload().GetFields()["toolCallId"].GetStringValue(); got != "call_1" {
					t.Fatalf("toolCallId = %q, want the tool call identity", got)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mapped := mapChatEvent(events.Event{
				SessionID: "sess_1", RunID: "run_1", TraceID: "trace_1", Sequence: 6,
				Type: test.eventType, PayloadJSON: test.payload,
			})
			assertChatEventCarriesNoRawDiagnostics(t, mapped)
			test.assertFn(t, mapped)
		})
	}
}

// assertChatEventCarriesNoRawDiagnostics reads the values rather than the
// message's rendered form. protobuf's own String() elides anything it thinks
// is a secret, so a poison check against a rendering would pass for exactly
// the values that matter most.
func assertChatEventCarriesNoRawDiagnostics(t *testing.T, event *turingv1.ChatStreamEvent) {
	t.Helper()
	texts := []string{
		event.GetRunFailed().GetCode(),
		event.GetRunFailed().GetMessage(),
		event.GetRunCancelled().GetReason(),
		event.GetMessageCompleted().GetContent(),
		event.GetTokenDelta().GetDelta(),
		event.GetApprovalRequested().GetArgsSummary(),
	}
	for _, text := range texts {
		for _, poison := range chatRawDiagnostics {
			if text != "" && strings.Contains(text, poison) {
				t.Fatalf("chat event republished %q", poison)
			}
		}
		for _, parserText := range []string{"unexpected end of JSON", "unexpected EOF", "invalid character", "cannot unmarshal"} {
			if text != "" && strings.Contains(text, parserText) {
				t.Fatalf("chat event carries parser text %q", parserText)
			}
		}
	}
	if persisted := event.GetPersistedEvent(); persisted != nil {
		assertChatPayloadCarriesNoRawDiagnostics(t, persisted.GetPayload().AsMap())
	}
}

func assertChatPayloadCarriesNoRawDiagnostics(t *testing.T, value any) {
	t.Helper()
	switch typed := value.(type) {
	case string:
		for _, poison := range chatRawDiagnostics {
			if strings.Contains(typed, poison) {
				t.Fatalf("chat payload republished %q", poison)
			}
		}
	case map[string]any:
		for key, nested := range typed {
			for _, forbidden := range []string{"message", "error", "reason", "note", "args", "approvalToken", "detail"} {
				if key == forbidden {
					t.Fatalf("chat payload carries the diagnostic key %q", key)
				}
			}
			assertChatPayloadCarriesNoRawDiagnostics(t, nested)
		}
	case []any:
		for _, nested := range typed {
			assertChatPayloadCarriesNoRawDiagnostics(t, nested)
		}
	}
}
