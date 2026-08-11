package approvals

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type approvalHarness struct {
	repo     *repository.Repository
	database *db.DB
	bus      *events.Bus
	service  *Server
	conn     *grpc.ClientConn
}

func newApprovalHarness(t *testing.T) *approvalHarness {
	t.Helper()
	database := openApprovalTestDB(t)
	repo := repository.New(database)
	bus := events.NewBus(8)
	service := New(repo, bus, "approval-secret")
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	turingv1.RegisterApprovalServiceServer(grpcServer, service)
	go func() {
		_ = grpcServer.Serve(lis)
	}()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	// NewClient starts the channel IDLE; DialContext connected eagerly in the
	// background. Connect() restores that so handshake latency does not land
	// inside a test's deadline.
	conn.Connect()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = conn.Close()
	})
	return &approvalHarness{repo: repo, database: database, bus: bus, service: service, conn: conn}
}

func openApprovalTestDB(t *testing.T) *db.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name())
	sqlDB, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=memory&cache=shared&_foreign_keys=on", name))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	database := &db.DB{DB: sqlDB}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return database
}

func TestRecoverExpirationConflictReturnsCurrentReadError(t *testing.T) {
	expirationErr := fmt.Errorf("expiration conflict")
	currentErr := fmt.Errorf("current approval unavailable")

	_, err := recoverExpirationConflict(context.Background(), expirationErr, func(context.Context) (repository.ApprovalRecord, error) {
		return repository.ApprovalRecord{}, currentErr
	})

	if !errors.Is(err, currentErr) || errors.Is(err, expirationErr) {
		t.Fatalf("recoverExpirationConflict error = %v, want current read error", err)
	}
}

func (h *approvalHarness) createRunningToolCall(t *testing.T) repository.EnqueueUserMessageResult {
	t.Helper()
	session, err := h.repo.CreateSession(context.Background(), "Approvals")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "needs approval", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkRunRunning(context.Background(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.RecordToolCallBefore(context.Background(), repository.ToolCallRecord{ToolCallID: "call_1", RunID: enqueued.RunID}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:placeholder"); err != nil {
		t.Fatal(err)
	}
	return enqueued
}

func TestCreateApprovalForToolPersistsEventAndAudit(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)

	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt", "content": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if approvalID == "" {
		t.Fatal("approvalID is empty")
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "waiting_approval" {
		t.Fatalf("run status = %q, want waiting_approval", run.Status)
	}
	events, _, err := h.repo.ReplayEvents(context.Background(), enqueued.SessionID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	var requested repository.Event
	for _, event := range events {
		if event.Type == "approval.requested" {
			requested = event
		}
	}
	if requested.EventID == "" {
		t.Fatal("approval.requested event was not persisted")
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(requested.PayloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["approvalId"] != approvalID || payload["toolName"] != "files.update" || payload["argsSummary"] == "" {
		t.Fatalf("approval.requested payload = %+v", payload)
	}
	var auditAction string
	if err := h.database.QueryRowContext(context.Background(), `SELECT action FROM audit_logs WHERE target = ?`, approvalID).Scan(&auditAction); err != nil {
		t.Fatal(err)
	}
	if auditAction != "approval.requested" {
		t.Fatalf("audit action = %q", auditAction)
	}
}

func TestDefaultApprovalTTLAlignsExpiryAndJWT(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	started := time.Now()
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, approval.ExpiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if ttl := expiresAt.Sub(started); ttl < 64*time.Second || ttl > 66*time.Second {
		t.Fatalf("approval TTL = %v, want configured default near 65s", ttl)
	}
	if expiresAt.Nanosecond() != 0 {
		t.Fatalf("approval expiry = %s, want whole-second precision shared with JWT", approval.ExpiresAt)
	}
	if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	approved, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(approved.ApprovalToken, ".")
	if len(parts) != 3 {
		t.Fatalf("approval token = %q, want JWT", approved.ApprovalToken)
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["exp"] != float64(expiresAt.Unix()) {
		t.Fatalf("JWT exp = %#v, want approval expiry %d", payload["exp"], expiresAt.Unix())
	}
}

func TestApprovalLifecycleEventsIncludePersistedCorrelationIDs(t *testing.T) {
	createApproval := func(t *testing.T, h *approvalHarness) (repository.EnqueueUserMessageResult, string) {
		t.Helper()
		session, err := h.repo.CreateSession(context.Background(), "Correlation")
		if err != nil {
			t.Fatal(err)
		}
		enqueued, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
			SessionID: session.SessionID, Content: "needs approval", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.repo.MarkRunRunning(context.Background(), enqueued.RunID); err != nil {
			t.Fatal(err)
		}
		if err := h.repo.RecordToolCallBefore(context.Background(), repository.ToolCallRecord{
			ToolCallID: "call_correlation", RunID: enqueued.RunID, ModelToolCallID: "provider_call_1",
		}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:correlation"); err != nil {
			t.Fatal(err)
		}
		approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_correlation", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
		if err != nil {
			t.Fatal(err)
		}
		return enqueued, approvalID
	}
	assertCorrelation := func(t *testing.T, h *approvalHarness, enqueued repository.EnqueueUserMessageResult, eventType, approvalID string) {
		t.Helper()
		events, _, err := h.repo.ReplayEvents(context.Background(), enqueued.SessionID, 0, 20)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			if event.Type != eventType {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
				t.Fatal(err)
			}
			if payload["approvalId"] != approvalID || payload["toolCallId"] != "call_correlation" ||
				payload["runId"] != enqueued.RunID || payload["traceId"] != enqueued.TraceID ||
				payload["modelToolCallId"] != "provider_call_1" {
				t.Fatalf("%s payload = %+v", eventType, payload)
			}
			return
		}
		t.Fatalf("%s event was not persisted", eventType)
	}

	t.Run("requested", func(t *testing.T) {
		h := newApprovalHarness(t)
		enqueued, approvalID := createApproval(t, h)
		assertCorrelation(t, h, enqueued, "approval.requested", approvalID)
	})
	t.Run("approved", func(t *testing.T) {
		h := newApprovalHarness(t)
		enqueued, approvalID := createApproval(t, h)
		if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
			t.Fatal(err)
		}
		assertCorrelation(t, h, enqueued, "approval.approved", approvalID)
	})
	t.Run("denied", func(t *testing.T) {
		h := newApprovalHarness(t)
		enqueued, approvalID := createApproval(t, h)
		if _, err := h.service.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{ApprovalId: approvalID}); err != nil {
			t.Fatal(err)
		}
		assertCorrelation(t, h, enqueued, "approval.denied", approvalID)
	})
	t.Run("expired", func(t *testing.T) {
		h := newApprovalHarness(t)
		enqueued, approvalID := createApproval(t, h)
		if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), approvalID); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.GetApprovalForRuntime(context.Background(), &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID}); err != nil {
			t.Fatal(err)
		}
		assertCorrelation(t, h, enqueued, "approval.expired", approvalID)
	})
	t.Run("consumed", func(t *testing.T) {
		h := newApprovalHarness(t)
		enqueued, approvalID := createApproval(t, h)
		if _, err := h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.service.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{ApprovalId: approvalID}); err != nil {
			t.Fatal(err)
		}
		assertCorrelation(t, h, enqueued, "approval.consumed", approvalID)
	})
}

func TestCreateApprovalForToolReusesExistingToolCallApproval(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)

	first, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("second approval = %q, want %q", second, first)
	}
	var count int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM approvals WHERE tool_call_id = 'call_1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("approval count = %d, want 1", count)
	}
}

func TestCreateApprovalForToolRejectsExistingApprovalForDifferentRun(t *testing.T) {
	h := newApprovalHarness(t)
	first := h.createRunningToolCall(t)
	firstID, err := h.service.CreateApprovalForTool(context.Background(), first.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	secondSession, err := h.repo.CreateSession(context.Background(), "Second")
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.repo.EnqueueUserMessage(context.Background(), repository.EnqueueUserMessageInput{
		SessionID: secondSession.SessionID, Content: "second", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.MarkRunRunning(context.Background(), second.RunID); err != nil {
		t.Fatal(err)
	}

	secondID, err := h.service.CreateApprovalForTool(context.Background(), second.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err == nil {
		t.Fatalf("CreateApprovalForTool reused cross-run approval %q as %q", firstID, secondID)
	}
}

func TestApproveApprovalReturnsStatusAndToken(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	client := turingv1.NewApprovalServiceClient(h.conn)

	resp, err := client.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != turingv1.ApprovalStatus_APPROVAL_STATUS_APPROVED {
		t.Fatalf("ApproveApproval status = %s", resp.Status)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(approval.ApprovalToken, ".") {
		t.Fatalf("approval token was not signed: %q", approval.ApprovalToken)
	}
	var auditAction string
	if err := h.database.QueryRowContext(context.Background(), `SELECT action FROM audit_logs WHERE action = 'approval.approved' AND target = ?`, approvalID).Scan(&auditAction); err != nil {
		t.Fatal(err)
	}
}

func TestApproveApprovalNotifiesRuntimeWithToken(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	notifier := &recordingApprovalNotifier{}
	h.service.SetNotifier(notifier)

	_, err = h.service.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID})
	if err != nil {
		t.Fatal(err)
	}
	if got := notifier.snapshot(); got.runID != enqueued.RunID || got.approvalID != approvalID || got.status != "approved" || !strings.Contains(got.approvalToken, ".") {
		t.Fatalf("approval notification = %+v", got)
	}
}

func TestGetApprovalForRuntimeReturnsApprovedTokenAndConsumeConsumesOnce(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	client := turingv1.NewApprovalServiceClient(h.conn)
	if _, err := client.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}

	runtimeState, err := client.GetApprovalForRuntime(context.Background(), &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID})
	if err != nil {
		t.Fatal(err)
	}
	if runtimeState.Status != turingv1.ApprovalStatus_APPROVAL_STATUS_APPROVED || !strings.Contains(runtimeState.ApprovalToken, ".") {
		t.Fatalf("runtime approval state = %+v", runtimeState)
	}
	consumed, err := client.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{ApprovalId: approvalID})
	if err != nil {
		t.Fatal(err)
	}
	if consumed.Status != turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED {
		t.Fatalf("consume status = %s", consumed.Status)
	}
	again, err := client.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{ApprovalId: approvalID})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("second ConsumeApproval error = %v, want FailedPrecondition", err)
	}
	if again != nil {
		t.Fatalf("second ConsumeApproval response = %+v, want nil", again)
	}
}

func TestConsumeExpiredApprovalPublishesToolFailureBeforeRunFailure(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	client := turingv1.NewApprovalServiceClient(h.conn)
	if _, err := client.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), approvalID); err != nil {
		t.Fatal(err)
	}
	published, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
	defer unsubscribe()

	if response, err := client.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{ApprovalId: approvalID}); status.Code(err) != codes.FailedPrecondition || response != nil {
		t.Fatalf("ConsumeApproval response/error = %+v/%v, want nil/FailedPrecondition", response, err)
	}
	var publishedTypes []string
	for len(publishedTypes) < 3 {
		select {
		case event := <-published:
			if event.RunID == enqueued.RunID && (event.Type == "approval.expired" || event.Type == "tool.call.failed" || event.Type == "agent.run.failed") {
				publishedTypes = append(publishedTypes, event.Type)
			}
		case <-time.After(time.Second):
			t.Fatalf("published consume-expiry lifecycle = %v", publishedTypes)
		}
	}
	if want := []string{"approval.expired", "tool.call.failed", "agent.run.failed"}; !reflect.DeepEqual(publishedTypes, want) {
		t.Fatalf("published consume-expiry lifecycle = %v, want %v", publishedTypes, want)
	}
}

func TestGetApprovalForRuntimeLazilyExpiresOnceAndTerminalizesRunAndJob(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), approvalID); err != nil {
		t.Fatal(err)
	}
	notifier := &recordingApprovalNotifier{}
	h.service.SetNotifier(notifier)
	published, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
	defer unsubscribe()

	start := make(chan struct{})
	states := make(chan *turingv1.RuntimeApprovalState, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			state, callErr := h.service.GetApprovalForRuntime(context.Background(), &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID})
			states <- state
			errs <- callErr
		}()
	}
	close(start)
	for range 2 {
		if callErr := <-errs; callErr != nil {
			t.Fatalf("GetApprovalForRuntime error: %v", callErr)
		}
		state := <-states
		if state.GetStatus() != turingv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED {
			t.Fatalf("runtime approval state = %+v, want expired", state)
		}
	}

	second, err := h.service.GetApprovalForRuntime(context.Background(), &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID})
	if err != nil {
		t.Fatalf("second GetApprovalForRuntime error: %v", err)
	}
	if second.GetStatus() != turingv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED {
		t.Fatalf("second runtime approval state = %+v, want expired", second)
	}

	var runStatus, runCode string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, error_code FROM agent_runs WHERE id = ?`, enqueued.RunID).Scan(&runStatus, &runCode); err != nil {
		t.Fatal(err)
	}
	if runStatus != "failed" || runCode != "approval_expired" {
		t.Fatalf("run status/code = %q/%q, want failed/approval_expired", runStatus, runCode)
	}
	var jobStatus, jobCode string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, error_code FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&jobStatus, &jobCode); err != nil {
		t.Fatal(err)
	}
	if jobStatus != "failed" || jobCode != "approval_expired" {
		t.Fatalf("job status/code = %q/%q, want failed/approval_expired", jobStatus, jobCode)
	}
	var toolCallStatus string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM tool_calls WHERE id = 'call_1'`).Scan(&toolCallStatus); err != nil {
		t.Fatal(err)
	}
	if toolCallStatus != "failed" {
		t.Fatalf("expired tool call status = %q, want failed", toolCallStatus)
	}
	var eventCount, terminalEventCount, auditCount int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'approval.expired'`, enqueued.RunID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'agent.run.failed'`, enqueued.RunID).Scan(&terminalEventCount); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM audit_logs WHERE action = 'approval.expired' AND target = ?`, approvalID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 || terminalEventCount != 1 || auditCount != 1 {
		t.Fatalf("expiration event/terminal-event/audit counts = %d/%d/%d, want 1/1/1", eventCount, terminalEventCount, auditCount)
	}
	if got := notifier.snapshot(); got.count != 1 || got.runID != enqueued.RunID || got.approvalID != approvalID || got.status != "expired" || got.approvalToken != "" {
		t.Fatalf("expiration notification = %+v, want one expired notification", got)
	}
	var publishedTypes []string
	for len(publishedTypes) < 3 {
		select {
		case event := <-published:
			if event.RunID == enqueued.RunID && (event.Type == "approval.expired" || event.Type == "tool.call.failed" || event.Type == "agent.run.failed") {
				publishedTypes = append(publishedTypes, event.Type)
			}
		case <-time.After(time.Second):
			t.Fatalf("published expiration lifecycle = %v", publishedTypes)
		}
	}
	if want := []string{"approval.expired", "tool.call.failed", "agent.run.failed"}; !reflect.DeepEqual(publishedTypes, want) {
		t.Fatalf("published expiration lifecycle = %v, want %v", publishedTypes, want)
	}
	select {
	case event := <-published:
		t.Fatalf("duplicate expiration event published: %+v", event)
	default:
	}
}

func TestGetApprovalForRuntimeExpiresApprovedTokenBeforeReturningIt(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	client := turingv1.NewApprovalServiceClient(h.conn)
	if _, err := client.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), approvalID); err != nil {
		t.Fatal(err)
	}

	state, err := h.service.GetApprovalForRuntime(context.Background(), &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID})
	if err != nil {
		t.Fatal(err)
	}
	if state.GetStatus() != turingv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED || state.GetApprovalToken() != "" {
		t.Fatalf("runtime approval state = %+v, want expired with no token", state)
	}
	var statusValue string
	var token sql.NullString
	if err := h.database.QueryRowContext(context.Background(), `SELECT status, approval_token FROM approvals WHERE id = ?`, approvalID).Scan(&statusValue, &token); err != nil {
		t.Fatal(err)
	}
	if statusValue != "expired" || token.Valid {
		t.Fatalf("stored approval status/token = %q/%q, want expired/NULL", statusValue, token.String)
	}
}

func TestGetApprovalForRuntimeKeepsExpirationAtomicAndAncillaryFailuresNonFatal(t *testing.T) {
	notifierErr := status.Error(codes.Unavailable, "notify expiration failed: not pending")
	tests := []struct {
		name      string
		setup     func(t *testing.T, h *approvalHarness) error
		wantError string
		wantState turingv1.ApprovalStatus
	}{
		{
			name: "append event",
			setup: func(t *testing.T, h *approvalHarness) error {
				_, err := h.database.ExecContext(context.Background(), `
					CREATE TRIGGER fail_expiration_event
					BEFORE INSERT ON events
					WHEN NEW.type = 'approval.expired'
					BEGIN
						SELECT RAISE(ABORT, 'append expiration event failed');
					END
				`)
				return err
			},
			wantError: "append expiration event failed",
		},
		{
			name: "audit",
			setup: func(t *testing.T, h *approvalHarness) error {
				_, err := h.database.ExecContext(context.Background(), `
					CREATE TRIGGER fail_expiration_audit
					BEFORE INSERT ON audit_logs
					WHEN NEW.action = 'approval.expired'
					BEGIN
						SELECT RAISE(ABORT, 'record expiration audit failed');
					END
				`)
				return err
			},
			wantState: turingv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED,
		},
		{
			name: "notifier",
			setup: func(t *testing.T, h *approvalHarness) error {
				h.service.SetNotifier(failingApprovalNotifier{err: notifierErr})
				return nil
			},
			wantState: turingv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newApprovalHarness(t)
			enqueued := h.createRunningToolCall(t)
			approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), approvalID); err != nil {
				t.Fatal(err)
			}
			if err := tt.setup(t, h); err != nil {
				t.Fatal(err)
			}

			state, err := h.service.GetApprovalForRuntime(context.Background(), &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID})
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) {
					t.Fatalf("GetApprovalForRuntime state/error = %+v/%v, want error containing %q", state, err, tt.wantError)
				}
			} else if err != nil {
				t.Fatalf("GetApprovalForRuntime reported ancillary failure: %v", err)
			} else if state.GetStatus() != tt.wantState {
				t.Fatalf("GetApprovalForRuntime state = %+v, want %s", state, tt.wantState)
			}
			current, err := h.repo.GetApproval(context.Background(), approvalID)
			if err != nil {
				t.Fatal(err)
			}
			wantStatus := "expired"
			if tt.wantError != "" {
				wantStatus = "pending"
			}
			if current.Status != wantStatus {
				t.Fatalf("approval status = %q, want %s", current.Status, wantStatus)
			}
		})
	}
}

type failingApprovalNotifier struct {
	err error
}

func (n failingApprovalNotifier) NotifyApprovalUpdated(context.Context, string, string, string, string) error {
	return n.err
}

type recordingApprovalNotifier struct {
	mu            sync.Mutex
	count         int
	runID         string
	approvalID    string
	status        string
	approvalToken string
	contextErr    error
}

func (n *recordingApprovalNotifier) NotifyApprovalUpdated(ctx context.Context, runID string, approvalID string, status string, approvalToken string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.count++
	n.runID = runID
	n.approvalID = approvalID
	n.status = status
	n.approvalToken = approvalToken
	n.contextErr = ctx.Err()
	return nil
}

type approvalNotificationSnapshot struct {
	count         int
	runID         string
	approvalID    string
	status        string
	approvalToken string
	contextErr    error
}

func (n *recordingApprovalNotifier) snapshot() approvalNotificationSnapshot {
	n.mu.Lock()
	defer n.mu.Unlock()
	return approvalNotificationSnapshot{
		count:         n.count,
		runID:         n.runID,
		approvalID:    n.approvalID,
		status:        n.status,
		approvalToken: n.approvalToken,
		contextErr:    n.contextErr,
	}
}

func TestPostCommitApprovalWorkUsesIndependentContext(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	notifier := &recordingApprovalNotifier{}
	h.service.SetNotifier(notifier)

	h.service.finishPostCommit(approval, "client", "approval.approved", "approved", "token")

	got := notifier.snapshot()
	if got.count != 1 || got.contextErr != nil || got.status != "approved" {
		t.Fatalf("post-commit notification = %+v, want independent live context", got)
	}
}

func TestDenyApprovalAtExpiryBoundaryTakesExpirationPath(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `
		UPDATE approvals
		SET expires_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ?
	`, approvalID); err != nil {
		t.Fatal(err)
	}

	response, err := h.service.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{ApprovalId: approvalID, Reason: "too late"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("DenyApproval boundary response/error = %+v/%v, want FailedPrecondition", response, err)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "expired" {
		t.Fatalf("approval status = %q, want expired", approval.Status)
	}
	var expiredEvents, deniedEvents int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'approval.expired'`, enqueued.RunID).Scan(&expiredEvents); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'approval.denied'`, enqueued.RunID).Scan(&deniedEvents); err != nil {
		t.Fatal(err)
	}
	if expiredEvents != 1 || deniedEvents != 0 {
		t.Fatalf("expiry/denial event counts = %d/%d, want 1/0", expiredEvents, deniedEvents)
	}
}

func TestDenyApprovalReturnsDeniedStatus(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	client := turingv1.NewApprovalServiceClient(h.conn)

	resp, err := client.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{ApprovalId: approvalID, Reason: "no"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != turingv1.ApprovalStatus_APPROVAL_STATUS_DENIED {
		t.Fatalf("DenyApproval status = %s", resp.Status)
	}
	run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" {
		t.Fatalf("run status = %q, want failed", run.Status)
	}
}

func TestDenyApprovalPublishesCommittedTerminalRunEventOnlyOnce(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	published, unsubscribe := h.bus.Subscribe(enqueued.SessionID)
	defer unsubscribe()
	client := turingv1.NewApprovalServiceClient(h.conn)

	if _, err := client.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	var publishedTypes []string
	for len(publishedTypes) < 3 {
		select {
		case event := <-published:
			if event.RunID == enqueued.RunID && (event.Type == "approval.denied" || event.Type == "tool.call.denied" || event.Type == "agent.run.failed") {
				publishedTypes = append(publishedTypes, event.Type)
				if event.Type == "tool.call.denied" {
					var payload map[string]any
					if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
						t.Fatal(err)
					}
					want := map[string]any{
						"toolCallId": "call_1",
						"toolName":   "files.update",
						"serverName": "files",
						"error":      "User denied approval",
					}
					if !reflect.DeepEqual(payload, want) {
						t.Fatalf("tool.call.denied payload = %#v, want %#v", payload, want)
					}
				}
			}
		case <-time.After(time.Second):
			t.Fatalf("published lifecycle events = %v", publishedTypes)
		}
	}
	if want := []string{"approval.denied", "tool.call.denied", "agent.run.failed"}; !reflect.DeepEqual(publishedTypes, want) {
		t.Fatalf("published lifecycle events = %v, want %v", publishedTypes, want)
	}

	if _, err := client.DenyApproval(context.Background(), &turingv1.DenyApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatalf("repeated denial: %v", err)
	}
	select {
	case event := <-published:
		t.Fatalf("repeated denial published duplicate event: %+v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestApprovalTerminalizationRollsBackWhenRequiredApprovalEventAppendFails(t *testing.T) {
	tests := []struct {
		name        string
		eventType   string
		terminalize func(context.Context, *approvalHarness, string) error
	}{
		{
			name:      "denial",
			eventType: "approval.denied",
			terminalize: func(ctx context.Context, h *approvalHarness, approvalID string) error {
				_, err := h.service.DenyApproval(ctx, &turingv1.DenyApprovalRequest{ApprovalId: approvalID})
				return err
			},
		},
		{
			name:      "expiry",
			eventType: "approval.expired",
			terminalize: func(ctx context.Context, h *approvalHarness, approvalID string) error {
				if _, err := h.database.ExecContext(ctx, `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), approvalID); err != nil {
					return err
				}
				_, err := h.service.GetApprovalForRuntime(ctx, &turingv1.GetApprovalForRuntimeRequest{ApprovalId: approvalID})
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newApprovalHarness(t)
			enqueued := h.createRunningToolCall(t)
			approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := h.database.ExecContext(context.Background(), fmt.Sprintf(`
				CREATE TRIGGER fail_%s_event
				BEFORE INSERT ON events
				WHEN NEW.type = '%s'
				BEGIN
					SELECT RAISE(ABORT, 'append approval event failed');
				END
			`, test.name, test.eventType)); err != nil {
				t.Fatal(err)
			}
			err = test.terminalize(context.Background(), h, approvalID)
			if err == nil || !strings.Contains(err.Error(), "append approval event failed") {
				t.Fatalf("terminalization error = %v, want approval event append failure", err)
			}
			approval, err := h.repo.GetApproval(context.Background(), approvalID)
			if err != nil {
				t.Fatal(err)
			}
			run, err := h.repo.GetRun(context.Background(), enqueued.RunID)
			if err != nil {
				t.Fatal(err)
			}
			if approval.Status != "pending" || run.Status != "waiting_approval" {
				t.Fatalf("required event failure leaked terminal state: approval=%q run=%q", approval.Status, run.Status)
			}
		})
	}
}

func TestApproveExpiredApprovalFailsPrecondition(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), approvalID); err != nil {
		t.Fatal(err)
	}
	client := turingv1.NewApprovalServiceClient(h.conn)

	_, err = client.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("ApproveApproval error = %v, want FailedPrecondition", err)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "expired" {
		t.Fatalf("approval status = %q, want expired", approval.Status)
	}
}

func TestReapproveExpiredApprovedApprovalRevokesToken(t *testing.T) {
	h := newApprovalHarness(t)
	enqueued := h.createRunningToolCall(t)
	approvalID, err := h.service.CreateApprovalForTool(context.Background(), enqueued.RunID, "call_1", "general_assistant", "files.update", map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatal(err)
	}
	client := turingv1.NewApprovalServiceClient(h.conn)
	if _, err := client.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(context.Background(), `UPDATE approvals SET expires_at = ? WHERE id = ?`, time.Now().Add(-time.Minute).Format(time.RFC3339Nano), approvalID); err != nil {
		t.Fatal(err)
	}

	_, err = client.ApproveApproval(context.Background(), &turingv1.ApproveApprovalRequest{ApprovalId: approvalID})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("reapprove expired approval error = %v, want FailedPrecondition", err)
	}
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	if approval.Status != "expired" || approval.ApprovalToken != "" {
		t.Fatalf("approval after reapprove = %+v, want expired with revoked token", approval)
	}
}
