package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// approvalIdentityKeys is the only vocabulary an approval failure projection is
// allowed to publish beside its category. It is the same list the 0011
// migration rewrites legacy rows onto, so a client cannot tell a migrated
// approval.denied from one this build just wrote.
var approvalIdentityKeys = map[string]bool{
	"approvalId":      true,
	"toolCallId":      true,
	"toolName":        true,
	"runId":           true,
	"traceId":         true,
	"modelToolCallId": true,
}

// approvalFailureCategories maps the two terminal approval event types onto the
// category each one owes. The category is read off the event type the server
// chose, never off a failure code a tool or provider influenced — the same rule
// approvalEventCategory applies to migrated rows.
var approvalFailureCategories = map[string]string{
	"approval.denied":  "policy_denied",
	"approval.expired": "expired",
}

// assertApprovalFailureProjection pins the safe shape of a newly written
// approval terminalization payload: the identity a client already contracts on,
// the allowlisted category its event type owes, and nothing else. A message, a
// reason, or a human rationale in this payload is the exact leak the migration
// existed to scrub, so their absence is asserted on the raw keys rather than
// inferred.
func assertApprovalFailureProjection(t *testing.T, event Event, want map[string]any) {
	t.Helper()
	category, terminal := approvalFailureCategories[event.Type]
	if !terminal {
		t.Fatalf("event type = %q, want an approval terminalization type", event.Type)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		t.Fatalf("decode %s payload %q: %v", event.Type, event.PayloadJSON, err)
	}
	for key := range payload {
		if key == "category" || approvalIdentityKeys[key] {
			continue
		}
		t.Fatalf("%s payload published %q, which is neither intended identity nor the category: %s",
			event.Type, key, event.PayloadJSON)
	}
	if payload["category"] != category {
		t.Fatalf("%s category = %v, want %q", event.Type, payload["category"], category)
	}
	expected := make(map[string]any, len(want)+1)
	for key, value := range want {
		expected[key] = value
	}
	expected["category"] = category
	if !reflect.DeepEqual(payload, expected) {
		t.Fatalf("%s payload = %#v, want %#v", event.Type, payload, expected)
	}
}

// assertNoRationaleInEvents proves the human sentence a user typed never
// reaches any durable public payload, whichever event carried it before.
func assertNoRationaleInEvents(t *testing.T, database interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, runID string, rationale string) {
	t.Helper()
	ctx := context.Background()
	rows, err := database.QueryContext(ctx, `SELECT type, payload_json FROM events WHERE run_id = ?`, runID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var eventType, payload string
		if err := rows.Scan(&eventType, &payload); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(payload, rationale) {
			t.Fatalf("%s payload leaked the approval rationale: %s", eventType, payload)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

// approvalWaitingRun leaves a run parked in waiting_approval with one pending
// approval over a recorded tool call, which is the state every terminalization
// path below starts from.
func approvalWaitingRun(t *testing.T, repo *Repository, title string, worker string, toolCallID string, modelToolCallID string, expiresAt string) (EnqueueUserMessageResult, ApprovalRecord, Job) {
	t.Helper()
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, title)
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "decide for me", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNextJob(ctx, "general_assistant", worker)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: toolCallID, RunID: enqueued.RunID, ModelToolCallID: modelToolCallID,
		Status: "approval_required",
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:test"); err != nil {
		t.Fatal(err)
	}
	approval, err := repo.CreateApproval(ctx, enqueued.RunID, toolCallID, "general_assistant",
		"files.update", `{"path":"note.txt"}`, "sha256:test", expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	return enqueued, approval, claimed
}

// A user's denial is a policy outcome, and that is all the public event says.
// The sentence the user typed stays in the approval row and in the governed
// audit projection, which is where TUR-002 put it on purpose.
func TestDeniedApprovalProjectsPolicyDeniedCategoryWithoutRationale(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	const rationale = "I did not want that file touched"
	enqueued, approval, _ := approvalWaitingRun(t, repo, "Deny projection", "worker-deny",
		"call_deny_projection", "model_deny_projection", "2099-01-01T00:00:00Z")

	transition, err := repo.DenyApprovalWithEvent(ctx, approval.ApprovalID,
		sql.NullString{String: rationale, Valid: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	assertApprovalFailureProjection(t, transition.ApprovalEvent, map[string]any{
		"approvalId":      approval.ApprovalID,
		"toolCallId":      "call_deny_projection",
		"toolName":        "files.update",
		"runId":           enqueued.RunID,
		"traceId":         enqueued.TraceID,
		"modelToolCallId": "model_deny_projection",
	})
	assertNoRationaleInEvents(t, database, enqueued.RunID, rationale)

	// The governed storage keeps exactly what the public payload refuses.
	var storedReason sql.NullString
	if err := database.QueryRowContext(ctx,
		`SELECT denial_reason FROM approvals WHERE id = ?`, approval.ApprovalID).Scan(&storedReason); err != nil {
		t.Fatal(err)
	}
	if !storedReason.Valid || storedReason.String != rationale {
		t.Fatalf("stored denial reason = %+v, want the governed rationale preserved", storedReason)
	}
}

// An approval that runs out of time is expired, not denied, and the category
// says so — the run's own outcome is published in agent.run.failed's RunState,
// not smuggled into the approval projection.
func TestExpiredApprovalProjectsExpiredCategory(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, approval, _ := approvalWaitingRun(t, repo, "Expire projection", "worker-expire",
		"call_expire_projection", "model_expire_projection", "2099-01-01T00:00:00Z")

	transition, err := repo.ExpireApprovalWithEvent(ctx, approval.ApprovalID, "")
	if err != nil {
		t.Fatal(err)
	}
	assertApprovalFailureProjection(t, transition.ApprovalEvent, map[string]any{
		"approvalId":      approval.ApprovalID,
		"toolCallId":      "call_expire_projection",
		"toolName":        "files.update",
		"runId":           enqueued.RunID,
		"traceId":         enqueued.TraceID,
		"modelToolCallId": "model_expire_projection",
	})
}

// A decision that cannot be delivered still terminalizes the approval under
// approval.denied. The run fails for approval_delivery_failed, but the event
// type the server chose is the one that decides the public category.
func TestUndeliverableApprovalDecisionProjectsPolicyDeniedCategory(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, approval, _ := approvalWaitingRun(t, repo, "Delivery failure projection", "worker-undeliverable",
		"call_delivery_projection", "model_delivery_projection", "2099-01-01T00:00:00Z")

	transition, err := repo.FailApprovalDeliveryWithEvent(ctx, approval.ApprovalID, "")
	if err != nil {
		t.Fatal(err)
	}
	assertApprovalFailureProjection(t, transition.ApprovalEvent, map[string]any{
		"approvalId":      approval.ApprovalID,
		"toolCallId":      "call_delivery_projection",
		"toolName":        "files.update",
		"runId":           enqueued.RunID,
		"traceId":         enqueued.TraceID,
		"modelToolCallId": "model_delivery_projection",
	})
}

// Losing the worker while a decision is pending closes the approval through
// reconciliation rather than through a decision RPC. It is a different writer
// and it owes the same shape.
func TestReconciledPendingApprovalProjectsPolicyDeniedCategory(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	enqueued, approval, claimed := approvalWaitingRun(t, repo, "Reconciled approval projection", "worker-lost-decision",
		"call_reconcile_projection", "model_reconcile_projection", "2099-01-01T00:00:00Z")

	reconciliation, err := repo.ReconcileAssignment(ctx, Assignment{
		JobID: claimed.JobID, RunID: claimed.RunID, WorkerID: "worker-lost-decision",
		AttemptID: claimed.AssignmentAttemptID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(reconciliation.Events) == 0 || reconciliation.Events[0].Type != "approval.denied" {
		t.Fatalf("reconciliation events = %+v, want approval.denied first", reconciliation.Events)
	}
	assertApprovalFailureProjection(t, reconciliation.Events[0], map[string]any{
		"approvalId":      approval.ApprovalID,
		"toolCallId":      "call_reconcile_projection",
		"toolName":        "files.update",
		"runId":           enqueued.RunID,
		"traceId":         enqueued.TraceID,
		"modelToolCallId": "model_reconcile_projection",
	})
}

// A run that terminalizes with a pending approval underneath it revokes that
// approval as expired. The run's own failure reason belongs to the run's
// RunState; the approval event publishes the category its type owes.
func TestTerminalRunRevokesPendingApprovalWithExpiredCategory(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	enqueued, approval, _ := approvalWaitingRun(t, repo, "Terminal revocation projection", "worker-terminal-revoke",
		"call_revoke_projection", "model_revoke_projection", "2099-01-01T00:00:00Z")

	state, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FailRunCanonical(ctx, FailRunInput{
		RunID:                enqueued.RunID,
		ExpectedStateVersion: state.StateVersion,
		Failure:              providerFailureForTest(),
	}); err != nil {
		t.Fatal(err)
	}
	revocation := onlyEventOfType(t, database, enqueued.RunID, "approval.expired")
	assertApprovalFailureProjection(t, revocation, map[string]any{
		"approvalId":      approval.ApprovalID,
		"toolCallId":      "call_revoke_projection",
		"toolName":        "files.update",
		"modelToolCallId": "model_revoke_projection",
	})
}

// An approved authorization abandoned by a timed-out job is revoked as expired
// too. Its run fails for side_effect_uncertain, which is a fact about the run,
// not about the approval — publishing it here would give one event type two
// different vocabularies.
func TestStaleApprovedAuthorizationProjectsExpiredCategory(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	enqueued, approval, claimed := approvalWaitingRun(t, repo, "Stale authorization projection", "worker-stale-auth",
		"call_stale_projection", "model_stale_projection", "2099-01-01T00:00:00Z")
	if _, err := repo.ApproveApprovalWithEvent(ctx, approval.ApprovalID, "approved-token", sql.NullString{}, ""); err != nil {
		t.Fatal(err)
	}
	cutoff := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	expired := cutoff.Add(-time.Second)
	if _, err := database.ExecContext(ctx, `
		UPDATE agent_runs
		SET execution_lease_expires_at = ?, execution_lease_expires_at_ns = ?
		WHERE id = ?
	`, FormatTimestamp(expired), expired.UnixNano(), enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if claimed.JobID == "" {
		t.Fatal("claimed job id is empty")
	}

	events, err := repo.RecoverStaleAssignments(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[0].Type != "approval.expired" {
		t.Fatalf("recovery events = %+v, want approval.expired first", events)
	}
	assertApprovalFailureProjection(t, events[0], map[string]any{
		"approvalId":      approval.ApprovalID,
		"toolCallId":      "call_stale_projection",
		"toolName":        "files.update",
		"modelToolCallId": "model_stale_projection",
	})
}

// The nonfailure half of the approval lifecycle keeps its existing governed
// shape. A request and an approval are not failures, so inventing a failure
// category for them would put a word in the vocabulary that describes nothing.
func TestRequestedAndApprovedApprovalEventsCarryNoFailureCategory(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Nonfailure projection")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "decide for me", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-nonfailure"); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: "call_nonfailure_projection", RunID: enqueued.RunID,
		ModelToolCallID: "model_nonfailure_projection", Status: "approval_required",
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:test"); err != nil {
		t.Fatal(err)
	}
	// CreateApprovalWithEvent rather than CreateApproval: approval.requested is
	// the event a client sees first, and it is the one most likely to be
	// assumed harmless and left unasserted.
	approval, requested, err := repo.CreateApprovalWithEvent(ctx, enqueued.RunID, "call_nonfailure_projection",
		"general_assistant", "files.update", `{"path":"note.txt"}`, "sha256:test", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	approvedTransition, err := repo.ApproveApprovalWithEvent(ctx, approval.ApprovalID, "token", sql.NullString{}, "")
	if err != nil {
		t.Fatal(err)
	}
	consumed, err := repo.ConsumeApprovalWithEvent(ctx, approval.ApprovalID, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []Event{requested, approvedTransition.ApprovalEvent, consumed.ApprovalEvent} {
		var payload map[string]any
		if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
			t.Fatal(err)
		}
		if _, present := payload["category"]; present {
			t.Fatalf("%s payload gained a failure category: %s", event.Type, event.PayloadJSON)
		}
	}
}

// onlyEventOfType reads exactly one durable event of a type for a run. It reads
// the stored row rather than a returned slice so a writer cannot pass by
// returning a payload it never persisted.
func onlyEventOfType(t *testing.T, database interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, runID string, eventType string) Event {
	t.Helper()
	ctx := context.Background()
	rows, err := database.QueryContext(ctx,
		`SELECT id, type, payload_json FROM events WHERE run_id = ? AND type = ? ORDER BY sequence`, runID, eventType)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var found []Event
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.EventID, &event.Type, &event.PayloadJSON); err != nil {
			t.Fatal(err)
		}
		found = append(found, event)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("got %d %s events for %s, want exactly 1", len(found), eventType, runID)
	}
	return found[0]
}

// Every approval terminalization this build can write has to reach the durable
// log through one of the two approval event helpers, because those are where
// the category rule lives. A writer that appends approval.denied or
// approval.expired directly through the generic run-event append is exactly how
// an uncategorized payload shipped before, and the source is the only place
// that mistake is visible before it reaches a database.
func TestNoRepositoryWriterAppendsApprovalEventsDirectly(t *testing.T) {
	fileSet := token.NewFileSet()
	packages, err := parser.ParseDir(fileSet, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, parsed := range packages {
		for name, file := range parsed.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				call, isCall := node.(*ast.CallExpr)
				if !isCall {
					return true
				}
				function, isIdent := call.Fun.(*ast.Ident)
				if !isIdent || function.Name != "appendRunEventTx" || len(call.Args) < 6 {
					return true
				}
				literal, isLiteral := call.Args[5].(*ast.BasicLit)
				if !isLiteral || literal.Kind != token.STRING {
					return true
				}
				eventType, unquoteErr := strconv.Unquote(literal.Value)
				if unquoteErr != nil {
					t.Fatalf("%s: %v", name, unquoteErr)
				}
				if _, terminal := approvalFailureCategories[eventType]; terminal {
					t.Fatalf("%s:%d appends %s directly instead of through the approval event helpers, "+
						"so its payload bypasses the category rule",
						name, fileSet.Position(literal.Pos()).Line, eventType)
				}
				return true
			})
		}
	}
}

// approvalFailureCategory is the one rule that decides a public category, and
// it decides it from the event type the server chose. A failing tool, a
// provider, or a user's typed sentence never influences it.
func TestApprovalFailureCategoryIsReadOffTheServerChosenEventType(t *testing.T) {
	for _, testCase := range []struct {
		eventType string
		want      runoutcome.Reason
		failure   bool
	}{
		{eventType: "approval.denied", want: runoutcome.ReasonPolicyDenied, failure: true},
		{eventType: "approval.expired", want: runoutcome.ReasonExpired, failure: true},
		{eventType: "approval.requested"},
		{eventType: "approval.approved"},
		{eventType: "approval.consumed"},
		{eventType: "agent.run.failed"},
	} {
		category, failure := approvalFailureCategory(testCase.eventType)
		if failure != testCase.failure || category != testCase.want {
			t.Fatalf("approvalFailureCategory(%q) = (%q, %v), want (%q, %v)",
				testCase.eventType, category, failure, testCase.want, testCase.failure)
		}
	}
}

// An approval need not be attached to a tool call: tool_call_id is nullable and
// CreateApproval stores NULL for an empty one. The migration drops identity
// keys it finds empty rather than publishing a blank, so a live writer that
// emitted "toolCallId": "" would produce an event a client could tell apart
// from the rewritten one on the very key it joins on.
func TestApprovalFailureOmitsAnAbsentToolCallIDRatherThanPublishingBlank(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Detached approval projection")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "decide for me", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-detached"); err != nil {
		t.Fatal(err)
	}
	approval, err := repo.CreateApproval(ctx, enqueued.RunID, "", "general_assistant",
		"files.update", `{"path":"note.txt"}`, "sha256:test", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.DenyApprovalWithEvent(ctx, approval.ApprovalID, sql.NullString{}, ""); err != nil {
		t.Fatal(err)
	}
	assertApprovalFailureProjection(t, onlyEventOfType(t, database, enqueued.RunID, "approval.denied"), map[string]any{
		"approvalId": approval.ApprovalID,
		"toolName":   "files.update",
		"runId":      enqueued.RunID,
		"traceId":    enqueued.TraceID,
	})
}
