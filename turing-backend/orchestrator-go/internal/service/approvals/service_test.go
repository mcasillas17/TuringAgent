package approvals

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
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
	_, err = client.ConsumeApproval(context.Background(), &turingv1.ConsumeApprovalRequest{ApprovalId: approvalID})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("second ConsumeApproval error = %v, want FailedPrecondition", err)
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
	var eventCount, auditCount int
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE run_id = ? AND type = 'approval.expired'`, enqueued.RunID).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if err := h.database.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM audit_logs WHERE action = 'approval.expired' AND target = ?`, approvalID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 || auditCount != 1 {
		t.Fatalf("expiration event/audit counts = %d/%d, want 1/1", eventCount, auditCount)
	}
	if got := notifier.snapshot(); got.count != 1 || got.runID != enqueued.RunID || got.approvalID != approvalID || got.status != "expired" || got.approvalToken != "" {
		t.Fatalf("expiration notification = %+v, want one expired notification", got)
	}
	select {
	case event := <-published:
		if event.Type != "approval.expired" {
			t.Fatalf("published event = %+v, want approval.expired", event)
		}
	default:
		t.Fatal("approval.expired was not published")
	}
	select {
	case event := <-published:
		t.Fatalf("duplicate event published: %+v", event)
	default:
	}
}

func TestGetApprovalForRuntimeReturnsPostCommitExpirationErrors(t *testing.T) {
	notifierErr := status.Error(codes.Unavailable, "notify expiration failed: not pending")
	tests := []struct {
		name      string
		setup     func(t *testing.T, h *approvalHarness) error
		wantError string
		wantCause error
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
			wantError: "record expiration audit failed",
		},
		{
			name: "notifier",
			setup: func(t *testing.T, h *approvalHarness) error {
				h.service.SetNotifier(failingApprovalNotifier{err: notifierErr})
				return nil
			},
			wantError: "notify expiration failed: not pending",
			wantCause: notifierErr,
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
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("GetApprovalForRuntime state/error = %+v/%v, want error containing %q", state, err, tt.wantError)
			}
			if tt.wantCause != nil && !errors.Is(err, tt.wantCause) {
				t.Fatalf("GetApprovalForRuntime error = %v, want wrapped cause %v", err, tt.wantCause)
			}
			if tt.wantCause != nil && status.Code(err) != status.Code(tt.wantCause) {
				t.Fatalf("GetApprovalForRuntime status code = %s, want %s", status.Code(err), status.Code(tt.wantCause))
			}
			current, err := h.repo.GetApproval(context.Background(), approvalID)
			if err != nil {
				t.Fatal(err)
			}
			if current.Status != "expired" {
				t.Fatalf("approval status = %q, want expired", current.Status)
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
}

func (n *recordingApprovalNotifier) NotifyApprovalUpdated(ctx context.Context, runID string, approvalID string, status string, approvalToken string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.count++
	n.runID = runID
	n.approvalID = approvalID
	n.status = status
	n.approvalToken = approvalToken
	return nil
}

type approvalNotificationSnapshot struct {
	count         int
	runID         string
	approvalID    string
	status        string
	approvalToken string
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
