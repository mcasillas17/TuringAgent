package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/mcpregistry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

// TUR-026d, at registered-MCP dispatch: an authenticated direct
// CallRegisteredMcpTool from the runtime identity, with no BEFORE beacon, for
// an active scheduled automation's run (no enforced selection).

const ceilingWorkerID = "worker-ceiling"

var (
	vendorLookup = repository.AutomationTool{ServerName: "vendor", ToolName: "vendor.lookup"}
	vendorWrite  = repository.AutomationTool{ServerName: "vendor", ToolName: "vendor.write"}
)

type ceilingHarness struct {
	app        *App
	registry   turingv1.McpRegistryServiceClient
	runtimeCtx context.Context
	publicCtx  context.Context
	events     turingv1.EventServiceClient
	serverID   string
	// requests counts every request the registered server receives.
	requests atomic.Int32
}

func newCeilingHarness(t *testing.T, tier repository.MCPServerTier) *ceilingHarness {
	t.Helper()
	h := &ceilingHarness{app: newTestApp(t)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		h.requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	ctx := context.Background()
	registered, err := h.app.Repository.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: server.URL + "/mcp", Tier: tier,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.app.Repository.SetMCPServerEnabled(ctx, registered.Server.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := h.app.Repository.ReplaceMCPServerTools(ctx, registered.Server.ID, []repository.MCPServerTool{
		{Name: "vendor.lookup", Policy: "safe", SchemaJSON: `{"type":"object"}`},
		{Name: "vendor.write", Policy: "approval_required", SchemaJSON: `{"type":"object"}`},
	}); err != nil {
		t.Fatal(err)
	}
	h.serverID = registered.Server.ID
	h.registry = turingv1.NewMcpRegistryServiceClient(newBufconnClient(t, h.app.InternalServer))
	h.events = turingv1.NewEventServiceClient(newBufconnClient(t, h.app.PublicServer))
	h.runtimeCtx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer internal")
	h.publicCtx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer client")
	return h
}

// runningAutomation fires a scheduled automation with the given allowlist and
// delivers its run to a worker, so the run is running with an active
// execution, as a worker's direct call finds it.
func (h *ceilingHarness) runningAutomation(t *testing.T, allowed ...repository.AutomationTool) (repository.Automation, repository.AutomationFire, repository.Job) {
	t.Helper()
	ctx := context.Background()
	repo := h.app.Repository
	automation, err := repo.CreateAutomation(ctx, repository.AutomationInput{
		Name: "Nightly sync", Prompt: "Sync the vendor.",
		Schedule: repository.Schedule{Kind: repository.ScheduleInterval, Interval: time.Hour},
		Enabled:  true, AllowedTools: allowed,
	})
	if err != nil {
		t.Fatal(err)
	}
	due, err := time.Parse(time.RFC3339Nano, automation.NextDueAt)
	if err != nil {
		t.Fatal(err)
	}
	fire, found, err := repo.ClaimDueAutomation(ctx, due, repository.AutomationRunDefaults{
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil || !found {
		t.Fatalf("claim due automation = found %v, %v", found, err)
	}
	job, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", ceilingWorkerID, 0, time.Hour, nil, nil)
	if err != nil || job.RunID != fire.RunID {
		t.Fatalf("claim = %+v, %v; want the automation's run", job, err)
	}
	assignment := repository.Assignment{JobID: job.JobID, RunID: job.RunID, WorkerID: ceilingWorkerID, AttemptID: job.AssignmentAttemptID}
	if err := repo.BeginAssignmentSend(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkAssignmentDelivered(ctx, assignment); err != nil {
		t.Fatal(err)
	}
	return automation, fire, job
}

// grantedWrite records a vendor.write call, creates its approval and grants
// it the way an automation's run is granted one, then resumes the run as the
// worker would.
func (h *ceilingHarness) grantedWrite(t *testing.T, fire repository.AutomationFire, job repository.Job, toolCallID string) string {
	t.Helper()
	ctx := context.Background()
	repo := h.app.Repository
	args := map[string]any{"path": "x"}
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, repository.ToolCallRecord{ToolCallID: toolCallID, RunID: fire.RunID},
		"general_assistant", "vendor", "vendor.write", string(encoded), approvalpreview.Hash(string(encoded))); err != nil {
		t.Fatal(err)
	}
	approvalID, err := h.app.ApprovalService.CreateApprovalForTool(ctx, fire.RunID, toolCallID, "general_assistant", "vendor.write", args)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.app.ApprovalService.GrantUnattendedApproval(ctx, approvalID, "vendor", "vendor.write"); err != nil {
		t.Fatalf("GrantUnattendedApproval: %v", err)
	}
	waiting, err := repo.GetRunState(ctx, fire.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ResumeApprovedRun(ctx, repository.ResumeApprovedRunInput{
		RunID: fire.RunID, ApprovalID: approvalID, WorkerID: ceilingWorkerID,
		AssignmentAttemptID: job.AssignmentAttemptID, ExpectedStateVersion: waiting.StateVersion,
	}); err != nil {
		t.Fatalf("ResumeApprovedRun: %v", err)
	}
	return approvalID
}

func (h *ceilingHarness) call(runID string, approvalID string, tool string) error {
	args, err := structpb.NewStruct(map[string]any{"path": "x"})
	if err != nil {
		return err
	}
	_, err = h.registry.CallRegisteredMcpTool(h.runtimeCtx, &turingv1.CallRegisteredMcpToolRequest{
		ServerId: h.serverID, RunId: runID, ApprovalId: approvalID, ToolName: tool, Args: args,
	})
	return err
}

type blockedRow struct {
	actorType, actorID, target, payload string
}

func (h *ceilingHarness) blockedRows(t *testing.T, runID string) []blockedRow {
	t.Helper()
	rows, err := h.app.database.QueryContext(context.Background(), `
		SELECT actor_type, COALESCE(actor_id, ''), COALESCE(target, ''), COALESCE(payload_json, '')
		FROM audit_logs WHERE correlation_id = ? AND action = 'automation.tool.blocked' ORDER BY rowid`, runID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var found []blockedRow
	for rows.Next() {
		var row blockedRow
		if err := rows.Scan(&row.actorType, &row.actorID, &row.target, &row.payload); err != nil {
			t.Fatal(err)
		}
		found = append(found, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return found
}

// assertFailedVisibly checks the run failed under the automation-policy origin
// with code, its execution left active and uncertain until the worker
// acknowledges, and exactly one automation.tool.blocked row with no
// arguments.
func (h *ceilingHarness) assertFailedVisibly(t *testing.T, automation repository.Automation, runID string, tool string, code string) {
	t.Helper()
	var runStatus, errorCode, outcome, executionState string
	var executionActive int
	var exitedAt sql.NullString
	if err := h.app.database.QueryRowContext(context.Background(), `
		SELECT status, COALESCE(error_code, ''), outcome_reason, execution_active, execution_state, execution_exit_acknowledged_at
		FROM agent_runs WHERE id = ?`, runID).Scan(&runStatus, &errorCode, &outcome, &executionActive, &executionState, &exitedAt); err != nil {
		t.Fatal(err)
	}
	if runStatus != "failed" || errorCode != code || outcome != "policy_denied" {
		t.Fatalf("run = %s / %q / %s, want failed / %s / policy_denied", runStatus, errorCode, outcome, code)
	}
	if executionActive != 1 || executionState != "uncertain" || exitedAt.Valid {
		t.Fatalf("execution = active %d, %q, exited %v; want still active and uncertain until the worker acknowledges", executionActive, executionState, exitedAt)
	}
	rows := h.blockedRows(t, runID)
	if len(rows) != 1 {
		t.Fatalf("automation.tool.blocked rows = %d, want 1", len(rows))
	}
	want := `{"automationId":"` + automation.AutomationID + `","automationName":"Nightly sync","code":"` + code +
		`","serverName":"vendor","toolName":"` + tool + `"}`
	if row := rows[0]; row.actorType != "automation" || row.actorID != automation.AutomationID || row.target != "" || row.payload != want {
		t.Fatalf("automation.tool.blocked row = %+v, want automation actor, no target and payload %s", row, want)
	}
}

func assertPermissionDenied(t *testing.T, err error, code string) {
	t.Helper()
	if status.Code(err) != codes.PermissionDenied || !strings.Contains(status.Convert(err).Message(), code) {
		t.Fatalf("CallRegisteredMcpTool err = %v, want PermissionDenied naming %s", err, code)
	}
}

func (h *ceilingHarness) approvalState(t *testing.T, approvalID string) (string, bool) {
	t.Helper()
	var approvalStatus string
	var token sql.NullString
	if err := h.app.database.QueryRowContext(context.Background(),
		`SELECT status, approval_token FROM approvals WHERE id = ?`, approvalID).Scan(&approvalStatus, &token); err != nil {
		t.Fatal(err)
	}
	return approvalStatus, token.Valid && token.String != ""
}

// (1) and (2): an unallowlisted safe tool on a local_container server. The
// call is refused with PermissionDenied and reaches no server; the run fails
// with its execution preserved and one audit row; a session subscribed before
// the call receives agent.run.failed without resubscribing. A second refused
// call on the failed run writes, publishes and changes nothing.
func TestADirectCallToAnUnlistedSafeToolFailsTheRunVisibly(t *testing.T) {
	h := newCeilingHarness(t, repository.MCPServerTierLocalContainer)
	automation, fire, _ := h.runningAutomation(t, repository.AutomationTool{ServerName: "files", ToolName: "files.read"})
	streamCtx, cancel := context.WithCancel(h.publicCtx)
	defer cancel()
	stream, err := h.events.SubscribeSessionEvents(streamCtx, &turingv1.SubscribeSessionEventsRequest{SessionId: fire.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	received := make(chan *turingv1.TuringEvent, 64)
	go func() {
		for {
			event, err := stream.Recv()
			if err != nil {
				close(received)
				return
			}
			received <- event
		}
	}()
	waitForSubscriber(t, h.app, fire.SessionID)

	assertPermissionDenied(t, h.call(fire.RunID, "", "vendor.lookup"), repository.AutomationToolNotAllowlistedCode)
	if got := h.requests.Load(); got != 0 {
		t.Fatalf("vendor requests = %d, want none", got)
	}
	h.assertFailedVisibly(t, automation, fire.RunID, "vendor.lookup", repository.AutomationToolNotAllowlistedCode)
	waitForStreamEvent(t, received, turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_FAILED)
	drainStream(received)

	before, err := h.app.Repository.GetRunState(context.Background(), fire.RunID)
	if err != nil {
		t.Fatal(err)
	}
	assertPermissionDenied(t, h.call(fire.RunID, "", "vendor.lookup"), repository.AutomationToolNotAllowlistedCode)
	if got := h.requests.Load(); got != 0 {
		t.Fatalf("vendor requests = %d, want none", got)
	}
	if rows := h.blockedRows(t, fire.RunID); len(rows) != 1 {
		t.Fatalf("automation.tool.blocked rows after a second refusal = %d, want 1", len(rows))
	}
	after, err := h.app.Repository.GetRunState(context.Background(), fire.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if after.StateVersion != before.StateVersion {
		t.Fatalf("state version = %d after a second refusal, want %d", after.StateVersion, before.StateVersion)
	}
	select {
	case event, open := <-received:
		if open {
			t.Fatalf("a second refusal published %s", event.GetType())
		}
	case <-time.After(200 * time.Millisecond):
	}
}

// drainStream reads whatever is already on its way, until the stream is quiet.
func drainStream(received <-chan *turingv1.TuringEvent) {
	for {
		select {
		case _, open := <-received:
			if !open {
				return
			}
		case <-time.After(200 * time.Millisecond):
			return
		}
	}
}

// (3) Revoked before the call starts: the automation is narrowed, or
// disabled, after the call's approval was granted and before the call. It is
// refused at entry; the approval is never consumed and ends expired with its
// token cleared by the run's failure; nothing is sent; the run fails with
// its row.
func TestADirectCallRevokedBeforeItStartsNeverSpendsItsApproval(t *testing.T) {
	for _, revocation := range []struct {
		name   string
		revoke func(t *testing.T, h *ceilingHarness, automation repository.Automation)
	}{
		{name: "narrowed", revoke: narrowCeilingAutomation},
		{name: "disabled", revoke: disableCeilingAutomation},
	} {
		t.Run(revocation.name, func(t *testing.T) {
			h := newCeilingHarness(t, repository.MCPServerTierLocalContainer)
			automation, fire, job := h.runningAutomation(t, vendorWrite, vendorLookup)
			approvalID := h.grantedWrite(t, fire, job, "call_revoked_before")
			revocation.revoke(t, h, automation)

			assertPermissionDenied(t, h.call(fire.RunID, approvalID, "vendor.write"), repository.AutomationGrantRevokedCode)
			if got := h.requests.Load(); got != 0 {
				t.Fatalf("vendor requests = %d, want none", got)
			}
			if approvalStatus, hasToken := h.approvalState(t, approvalID); approvalStatus != "expired" || hasToken {
				t.Fatalf("approval = %s with token %v, want expired with its token cleared", approvalStatus, hasToken)
			}
			h.assertFailedVisibly(t, automation, fire.RunID, "vendor.write", repository.AutomationGrantRevokedCode)
		})
	}
}

// (4) An unallowlisted tool on a remote_url server sends no request and fails
// the run with one audit row.
func TestADirectCallToAnUnlistedRemoteToolSendsNothing(t *testing.T) {
	h := newCeilingHarness(t, repository.MCPServerTierRemoteURL)
	automation, fire, _ := h.runningAutomation(t, repository.AutomationTool{ServerName: "files", ToolName: "files.read"})

	assertPermissionDenied(t, h.call(fire.RunID, "", "vendor.lookup"), repository.AutomationToolNotAllowlistedCode)
	if got := h.requests.Load(); got != 0 {
		t.Fatalf("remote requests = %d, want none", got)
	}
	h.assertFailedVisibly(t, automation, fire.RunID, "vendor.lookup", repository.AutomationToolNotAllowlistedCode)
}

// (5) Revoked during the call: with a barrier after the entry check passes
// and before the call's approval is consumed, the automation is disabled, or
// its allowlist narrowed. The call is refused at dispatch naming
// automation_grant_revoked, its approval is spent, nothing is sent, and
// exactly one audit row is written.
func TestADirectCallRevokedDuringTheCallIsRefusedAtDispatch(t *testing.T) {
	for _, revocation := range []struct {
		name   string
		revoke func(t *testing.T, h *ceilingHarness, automation repository.Automation)
	}{
		{name: "disabled", revoke: disableCeilingAutomation},
		{name: "narrowed", revoke: narrowCeilingAutomation},
	} {
		t.Run(revocation.name, func(t *testing.T) {
			h := newCeilingHarness(t, repository.MCPServerTierLocalContainer)
			automation, fire, job := h.runningAutomation(t, vendorWrite, vendorLookup)
			approvalID := h.grantedWrite(t, fire, job, "call_revoked_during")
			barrier := &revokingEnforcer{inner: h.app.ApprovalService, revoke: func() { revocation.revoke(t, h, automation) }}
			h.app.MCPRegistryService.SetApprovalEnforcer(barrier)

			assertPermissionDenied(t, h.call(fire.RunID, approvalID, "vendor.write"), repository.AutomationGrantRevokedCode)
			if !barrier.reached.Load() {
				t.Fatal("the call never reached the barrier; it was refused at entry")
			}
			if got := h.requests.Load(); got != 0 {
				t.Fatalf("vendor requests = %d, want none", got)
			}
			if approvalStatus, _ := h.approvalState(t, approvalID); approvalStatus != "consumed" {
				t.Fatalf("approval = %s, want it spent", approvalStatus)
			}
			h.assertFailedVisibly(t, automation, fire.RunID, "vendor.write", repository.AutomationGrantRevokedCode)
		})
	}
}

func narrowCeilingAutomation(t *testing.T, h *ceilingHarness, automation repository.Automation) {
	if _, err := h.app.Repository.UpdateAutomation(context.Background(), automation.AutomationID, repository.AutomationInput{
		Name: automation.Name, Prompt: automation.Prompt, Schedule: automation.Schedule,
		AllowedTools: []repository.AutomationTool{vendorLookup},
	}); err != nil {
		t.Error(err)
	}
}

func disableCeilingAutomation(t *testing.T, h *ceilingHarness, automation repository.Automation) {
	if _, err := h.app.Repository.SetAutomationEnabled(context.Background(), automation.AutomationID, false); err != nil {
		t.Error(err)
	}
}

// revokingEnforcer is the barrier between the entry check and the approval's
// consumption: it revokes, then consumes through the real approval service.
type revokingEnforcer struct {
	inner   mcpregistry.ApprovalEnforcer
	revoke  func()
	reached atomic.Bool
}

func (e *revokingEnforcer) ConsumeApprovalForThirdParty(ctx context.Context, approvalID string, runID string, serverName string, serverID string, toolName string, args map[string]any) error {
	e.reached.Store(true)
	e.revoke()
	return e.inner.ConsumeApprovalForThirdParty(ctx, approvalID, runID, serverName, serverID, toolName, args)
}

func waitForSubscriber(t *testing.T, app *App, sessionID string) {
	t.Helper()
	waitFor(t, "the session's subscriber", func() bool { return app.EventBus.SessionSubscriberCount(sessionID) > 0 })
}

func waitForStreamEvent(t *testing.T, received <-chan *turingv1.TuringEvent, eventType turingv1.TuringEventType) *turingv1.TuringEvent {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case event, open := <-received:
			if !open {
				t.Fatalf("the subscription ended before %s arrived", eventType)
			}
			if event.GetType() == eventType {
				return event
			}
		case <-deadline:
			t.Fatalf("no %s arrived on the session's subscription", eventType)
		}
	}
}
