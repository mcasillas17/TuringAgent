package audit

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func openAuditTestDB(t *testing.T) *db.DB {
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

func TestRecordStoresCanonicalPayload(t *testing.T) {
	database := openAuditTestDB(t)
	service := New(repository.New(database))

	if err := service.Record(context.Background(), "run_1", "client", "user_1", "approval.approved", "appr_1", map[string]any{"b": float64(2), "a": "one"}); err != nil {
		t.Fatal(err)
	}

	var payloadJSON string
	if err := database.QueryRowContext(context.Background(), `SELECT payload_json FROM audit_logs WHERE action = 'approval.approved'`).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	if payloadJSON != `{"a":"one","b":2}` {
		t.Fatalf("payload_json = %s", payloadJSON)
	}
}

func TestRecordRejectsUnsafeDynamicPayload(t *testing.T) {
	database := openAuditTestDB(t)
	service := New(repository.New(database))

	err := service.Record(context.Background(), "run_1", "runtime", "worker_1", "tool.call.started", "call_1", map[string]any{"bad": math.NaN()})
	if err == nil {
		t.Fatal("Record succeeded with NaN payload, want error")
	}
}

// TestListAuditEntriesContract is the Task 1 compile-time contract lock: it
// exists so the generated public types keep the exact optional/enum/message
// shape the service is written against, independent of any service behavior.
// It deliberately does not call the service — but it is not a no-op either.
// Constructing the request and reading each field back proves the optional
// correlation/action fields are pointer-typed (so "unset" and "empty" stay
// distinguishable), that AuditOrder is the expected enum, and that PageRequest
// carries the limit. A proto regression that flattened these to plain strings
// or dropped a field would fail to compile or fail these assertions.
func TestListAuditEntriesContract(t *testing.T) {
	correlationID := "run_1"
	action := "tool.call.before"
	req := &turingv1.ListAuditEntriesRequest{
		CorrelationId: &correlationID,
		Action:        &action,
		Order:         turingv1.AuditOrder_AUDIT_ORDER_DESCENDING,
		Page:          &turingv1.PageRequest{Limit: 25},
	}

	if req.CorrelationId == nil || *req.CorrelationId != "run_1" {
		t.Fatalf("correlation_id = %v, want a pointer retaining %q", req.CorrelationId, "run_1")
	}
	if req.Action == nil || *req.Action != "tool.call.before" {
		t.Fatalf("action = %v, want a pointer retaining %q", req.Action, "tool.call.before")
	}
	if req.Order != turingv1.AuditOrder_AUDIT_ORDER_DESCENDING {
		t.Fatalf("order = %v, want AUDIT_ORDER_DESCENDING", req.Order)
	}
	if req.Page == nil || req.Page.GetLimit() != 25 {
		t.Fatalf("page = %v, want limit 25", req.Page)
	}
}

// insertAuditRow writes a row directly so a service test can pin an exact
// created_at and an arbitrary (even malformed) payload_json — neither of which
// Record can produce, because Record stamps now() and only ever marshals a
// valid JSON object.
func insertAuditRow(t *testing.T, database *db.DB, id, correlationID, actorType, actorID, action, target, payloadJSON, createdAt string) {
	t.Helper()
	_, err := database.ExecContext(context.Background(), `
		INSERT INTO audit_logs (id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, nullOrText(correlationID), actorType, nullOrText(actorID), action, nullOrText(target), nullOrText(payloadJSON), createdAt)
	if err != nil {
		t.Fatalf("insert audit row %s: %v", id, err)
	}
}

func nullOrText(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func newAuditServer(t *testing.T) (*Server, *db.DB) {
	t.Helper()
	database := openAuditTestDB(t)
	return New(repository.New(database)), database
}

func listEntries(t *testing.T, service *Server, req *turingv1.ListAuditEntriesRequest) *turingv1.ListAuditEntriesResponse {
	t.Helper()
	resp, err := service.ListAuditEntries(context.Background(), req)
	if err != nil {
		t.Fatalf("ListAuditEntries: %v", err)
	}
	return resp
}

func requireCode(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if status.Code(err) != want {
		t.Fatalf("status code = %v (err=%v), want %v", status.Code(err), err, want)
	}
}

func canonicalTime(sec int) string {
	return fmt.Sprintf("2026-05-01T00:00:%02d.000000000Z", sec)
}

// setPayloadFields is the closed set of payload field names a projected
// AuditPayload can carry. presentPayloadFields reports which of them a given
// payload actually set, so a test can assert exact allowlist membership: any
// field outside the documented rule for an action shows up here and fails.
func presentPayloadFields(p *turingv1.AuditPayload) map[string]bool {
	set := map[string]bool{}
	if p.ToolName != nil {
		set["tool_name"] = true
	}
	if p.ServerName != nil {
		set["server_name"] = true
	}
	if p.Phase != nil {
		set["phase"] = true
	}
	if p.Status != nil {
		set["status"] = true
	}
	if p.Reason != nil {
		set["reason"] = true
	}
	if p.DurationMs != nil {
		set["duration_ms"] = true
	}
	if p.ErrorCode != nil {
		set["error_code"] = true
	}
	if p.Provider != nil {
		set["provider"] = true
	}
	if p.DisplayName != nil {
		set["display_name"] = true
	}
	if p.Unattended != nil {
		set["unattended"] = true
	}
	if p.AutomationId != nil {
		set["automation_id"] = true
	}
	if p.AutomationName != nil {
		set["automation_name"] = true
	}
	if p.Method != nil {
		set["method"] = true
	}
	if p.RequestId != nil {
		set["request_id"] = true
	}
	if p.DeletedRuns != nil {
		set["deleted_runs"] = true
	}
	if p.DeletedMessages != nil {
		set["deleted_messages"] = true
	}
	if p.DecisionComment != nil {
		set["decision_comment"] = true
	}
	if p.DecisionCommentTruncated != nil {
		set["decision_comment_truncated"] = true
	}
	if p.DenialReason != nil {
		set["denial_reason"] = true
	}
	if p.DenialReasonTruncated != nil {
		set["denial_reason_truncated"] = true
	}
	return set
}

func sameStringSet(got map[string]bool, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for _, key := range want {
		if !got[key] {
			return false
		}
	}
	return true
}

func TestListAuditEntriesReturnsEveryCurrentActionUnderExplicitPolicy(t *testing.T) {
	service, database := newAuditServer(t)

	rows := []struct {
		id      string
		action  string
		payload string
	}{
		{"a_tcb", "tool.call.before", `{"phase":"before","toolCallId":"tc1","agentId":"ag1","serverName":"files","toolName":"files.update","runId":"r1","traceId":"tr1","args":{"path":"/x"},"modelToolCallId":"m1","status":"pending","reason":"needs-approval","durationMs":12,"error":{"code":"E_DENIED","message":"blocked"}}`},
		{"a_tca", "tool.call.after", `{"phase":"after","serverName":"files","toolName":"files.update","runId":"r1","traceId":"tr1","args":{"path":"/x"},"resultSummary":"wrote 3 lines","modelToolCallId":"m1","status":"ok","durationMs":34,"error":{"code":"","message":""}}`},
		{"a_ar", "approval.requested", `{"toolName":"files.update","unattended":true,"automationId":"auto1","automationName":"Nightly"}`},
		{"a_aa", "approval.approved", `{"toolName":"files.update"}`},
		{"a_ad", "approval.denied", `{"toolName":"files.update"}`},
		{"a_ae", "approval.expired", `{"toolName":"files.update"}`},
		{"a_ac", "approval.consumed", `{"toolName":"files.update"}`},
		{"a_atb", "automation.tool.blocked", `{"toolName":"files.delete","serverName":"files","automationId":"auto2","automationName":"Cleanup"}`},
		{"a_ic", "integration.connected", `{"provider":"github","displayName":"GitHub"}`},
		{"a_ir", "integration.revoked", `{"provider":"github","displayName":"GitHub"}`},
		{"a_id", "integration.deleted", `{"provider":"github","displayName":"GitHub"}`},
		{"a_auth", "auth.failed", `{"method":"bearer","requestId":"req1","userAgent":"curl/8","peer":"1.2.3.4:5555"}`},
		{"a_sd", "session.deleted", `{"runs":3,"messages":42}`},
		// session.routed / session.unrouted are direct recordAuditTx writes with
		// no reviewed field rule, so they are default-deny: retrievable, PRESENT,
		// but every payload field (agentId/agent/endpoint/model) must be dropped.
		// The fixtures carry sentinel values so a leak is caught by name below.
		{"a_sr", "session.routed", `{"agentId":"SENTINEL_AGENTID","agent":"SENTINEL_AGENT","endpoint":"SENTINEL_ENDPOINT","model":"SENTINEL_MODEL"}`},
		{"a_su", "session.unrouted", `{"agentId":"SENTINEL_AGENTID","agent":"SENTINEL_AGENT","endpoint":"SENTINEL_ENDPOINT","model":"SENTINEL_MODEL"}`},
	}
	for i, row := range rows {
		insertAuditRow(t, database, row.id, "run_all", "runtime", "actor_1", row.action, "", row.payload, canonicalTime(i))
	}

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		Order: turingv1.AuditOrder_AUDIT_ORDER_ASCENDING,
		Page:  &turingv1.PageRequest{Limit: 100},
	})
	byAction := map[string]*turingv1.AuditEntry{}
	for _, entry := range resp.Entries {
		byAction[entry.Action] = entry
	}

	check := func(action string, wantFields []string, verify func(p *turingv1.AuditPayload)) {
		entry, ok := byAction[action]
		if !ok {
			t.Fatalf("action %s missing from response", action)
		}
		if entry.Payload.State != turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_PRESENT {
			t.Fatalf("%s payload state = %v, want PRESENT", action, entry.Payload.State)
		}
		if got := presentPayloadFields(entry.Payload); !sameStringSet(got, wantFields) {
			t.Fatalf("%s projected fields = %v, want exactly %v", action, got, wantFields)
		}
		verify(entry.Payload)
	}

	check("tool.call.before", []string{"tool_name", "server_name", "phase", "status", "reason", "duration_ms", "error_code"}, func(p *turingv1.AuditPayload) {
		if p.GetToolName() != "files.update" || p.GetServerName() != "files" || p.GetPhase() != "before" || p.GetStatus() != "pending" || p.GetReason() != "needs-approval" || p.GetDurationMs() != 12 || p.GetErrorCode() != "E_DENIED" {
			t.Fatalf("tool.call.before values wrong: %+v", p)
		}
	})
	check("tool.call.after", []string{"tool_name", "server_name", "phase", "status", "duration_ms"}, func(p *turingv1.AuditPayload) {
		// error.code was empty, so error_code must be omitted, not "".
		if p.GetToolName() != "files.update" || p.GetStatus() != "ok" || p.GetDurationMs() != 34 {
			t.Fatalf("tool.call.after values wrong: %+v", p)
		}
	})
	check("approval.requested", []string{"tool_name", "unattended", "automation_id", "automation_name"}, func(p *turingv1.AuditPayload) {
		if p.GetToolName() != "files.update" || !p.GetUnattended() || p.GetAutomationId() != "auto1" || p.GetAutomationName() != "Nightly" {
			t.Fatalf("approval.requested values wrong: %+v", p)
		}
	})
	for _, action := range []string{"approval.approved", "approval.denied", "approval.expired", "approval.consumed"} {
		check(action, []string{"tool_name"}, func(p *turingv1.AuditPayload) {
			if p.GetToolName() != "files.update" {
				t.Fatalf("%s tool_name = %q", action, p.GetToolName())
			}
		})
	}
	check("automation.tool.blocked", []string{"tool_name", "server_name", "automation_id", "automation_name"}, func(p *turingv1.AuditPayload) {
		if p.GetToolName() != "files.delete" || p.GetServerName() != "files" || p.GetAutomationId() != "auto2" || p.GetAutomationName() != "Cleanup" {
			t.Fatalf("automation.tool.blocked values wrong: %+v", p)
		}
	})
	for _, action := range []string{"integration.connected", "integration.revoked", "integration.deleted"} {
		check(action, []string{"provider", "display_name"}, func(p *turingv1.AuditPayload) {
			if p.GetProvider() != "github" || p.GetDisplayName() != "GitHub" {
				t.Fatalf("%s values wrong: %+v", action, p)
			}
		})
	}
	check("auth.failed", []string{"method", "request_id"}, func(p *turingv1.AuditPayload) {
		if p.GetMethod() != "bearer" || p.GetRequestId() != "req1" {
			t.Fatalf("auth.failed values wrong: %+v", p)
		}
	})
	check("session.deleted", []string{"deleted_runs", "deleted_messages"}, func(p *turingv1.AuditPayload) {
		if p.GetDeletedRuns() != 3 || p.GetDeletedMessages() != 42 {
			t.Fatalf("session.deleted values wrong: %+v", p)
		}
	})
	// Direct-write routing actions have no reviewed field rule: PRESENT with no
	// projected fields at all.
	for _, action := range []string{"session.routed", "session.unrouted"} {
		check(action, nil, func(p *turingv1.AuditPayload) {})
	}

	// Belt and suspenders: nothing from the routing payloads may reach the wire,
	// even though none of agentId/agent/endpoint/model has a proto field to
	// carry it. Marshal the whole response and prove every sentinel is absent.
	raw, err := protojson.Marshal(resp)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	for _, sentinel := range []string{"SENTINEL_AGENTID", "SENTINEL_AGENT", "SENTINEL_ENDPOINT", "SENTINEL_MODEL"} {
		if strings.Contains(string(raw), sentinel) {
			t.Fatalf("routing payload leaked sentinel %q into the response: %s", sentinel, raw)
		}
	}
}

// TUR-002 writes the human's approval comment / denial reason into the audit
// payload; TUR-013 is the read path that has to make it user-readable without
// widening anything else. These cases pin the exact contract: only the two
// human decision actions project rationale, presence distinguishes "the human
// typed nothing" from "no human field was ever recorded", the truncation flag
// mirrors the stored bool exactly, and the read API never repairs a stored
// value it dislikes — it omits it.
func TestListAuditEntriesProjectsApprovalDecisionRationale(t *testing.T) {
	service, database := newAuditServer(t)

	atBound := strings.Repeat("c", maxAuditDecisionRationaleBytes)
	overBound := strings.Repeat("c", maxAuditDecisionRationaleBytes+1)

	rows := []struct {
		id         string
		action     string
		payload    string
		wantFields []string
		verify     func(t *testing.T, p *turingv1.AuditPayload)
	}{
		{
			id:         "d_comment",
			action:     "approval.approved",
			payload:    `{"toolName":"files.update","comment":"looked at the diff, fine","commentTruncated":true}`,
			wantFields: []string{"tool_name", "decision_comment", "decision_comment_truncated"},
			verify: func(t *testing.T, p *turingv1.AuditPayload) {
				if p.GetDecisionComment() != "looked at the diff, fine" {
					t.Fatalf("decision_comment = %q", p.GetDecisionComment())
				}
				if !p.GetDecisionCommentTruncated() {
					t.Fatal("decision_comment_truncated = false, want true")
				}
			},
		},
		{
			// The human approved without typing anything. TUR-002 stores an
			// explicit empty string; it must stay a present empty value, never
			// collapse into "no rationale recorded".
			id:         "d_comment_empty",
			action:     "approval.approved",
			payload:    `{"toolName":"files.update","comment":""}`,
			wantFields: []string{"tool_name", "decision_comment"},
			verify: func(t *testing.T, p *turingv1.AuditPayload) {
				if p.DecisionComment == nil {
					t.Fatal("decision_comment is nil, want a present empty string")
				}
				if *p.DecisionComment != "" {
					t.Fatalf("decision_comment = %q, want empty", *p.DecisionComment)
				}
			},
		},
		{
			// Unattended automation grant: no human field was recorded at all,
			// so nothing may be projected — not even an empty rationale.
			id:         "d_unattended",
			action:     "approval.approved",
			payload:    `{"toolName":"files.update","unattended":true,"automationId":"auto1","automationName":"Nightly"}`,
			wantFields: []string{"tool_name", "unattended", "automation_id", "automation_name"},
			verify: func(t *testing.T, p *turingv1.AuditPayload) {
				if p.DecisionComment != nil {
					t.Fatalf("unattended approval projected decision_comment = %q", *p.DecisionComment)
				}
				if p.DecisionCommentTruncated != nil {
					t.Fatal("unattended approval projected decision_comment_truncated")
				}
			},
		},
		{
			id:         "d_comment_false_flag",
			action:     "approval.approved",
			payload:    `{"toolName":"files.update","comment":"short","commentTruncated":false}`,
			wantFields: []string{"tool_name", "decision_comment", "decision_comment_truncated"},
			verify: func(t *testing.T, p *turingv1.AuditPayload) {
				if p.DecisionCommentTruncated == nil || *p.DecisionCommentTruncated {
					t.Fatalf("decision_comment_truncated = %v, want a present false", p.DecisionCommentTruncated)
				}
			},
		},
		{
			id:         "d_reason",
			action:     "approval.denied",
			payload:    `{"toolName":"files.update","reason":"path is outside the sandbox","reasonTruncated":true}`,
			wantFields: []string{"tool_name", "denial_reason", "denial_reason_truncated"},
			verify: func(t *testing.T, p *turingv1.AuditPayload) {
				if p.GetDenialReason() != "path is outside the sandbox" {
					t.Fatalf("denial_reason = %q", p.GetDenialReason())
				}
				if !p.GetDenialReasonTruncated() {
					t.Fatal("denial_reason_truncated = false, want true")
				}
				// A denial reason must never be mistaken for the tool-policy
				// `reason` field the tool.call.* rule projects.
				if p.Reason != nil {
					t.Fatalf("approval.denied projected the tool-policy reason field = %q", *p.Reason)
				}
			},
		},
		{
			id:         "d_reason_empty",
			action:     "approval.denied",
			payload:    `{"toolName":"files.update","reason":""}`,
			wantFields: []string{"tool_name", "denial_reason"},
			verify: func(t *testing.T, p *turingv1.AuditPayload) {
				if p.DenialReason == nil || *p.DenialReason != "" {
					t.Fatalf("denial_reason = %v, want a present empty string", p.DenialReason)
				}
			},
		},
		{
			id:         "d_reason_absent",
			action:     "approval.denied",
			payload:    `{"toolName":"files.update"}`,
			wantFields: []string{"tool_name"},
			verify:     func(t *testing.T, p *turingv1.AuditPayload) {},
		},
		{
			// Newline, carriage return, and tab are how a person formats a
			// sentence they typed. They are preserved verbatim, unlike every
			// other control character.
			id:         "d_whitespace",
			action:     "approval.approved",
			payload:    `{"toolName":"files.update","comment":"line one\nline two\tindented\r\n"}`,
			wantFields: []string{"tool_name", "decision_comment"},
			verify: func(t *testing.T, p *turingv1.AuditPayload) {
				if p.GetDecisionComment() != "line one\nline two\tindented\r\n" {
					t.Fatalf("decision_comment = %q, want the newlines/tab preserved", p.GetDecisionComment())
				}
			},
		},
		{
			id:         "d_bound_exact",
			action:     "approval.denied",
			payload:    `{"toolName":"files.update","reason":"` + atBound + `"}`,
			wantFields: []string{"tool_name", "denial_reason"},
			verify: func(t *testing.T, p *turingv1.AuditPayload) {
				if p.GetDenialReason() != atBound {
					t.Fatalf("denial_reason at the exact bound was not projected verbatim (len=%d)", len(p.GetDenialReason()))
				}
			},
		},
		{
			// One byte over the writer's own bound: omitted, never truncated
			// here — truncation is the writer's job and it flags it.
			id:         "d_bound_over",
			action:     "approval.approved",
			payload:    `{"toolName":"files.update","comment":"` + overBound + `"}`,
			wantFields: []string{"tool_name"},
			verify:     func(t *testing.T, p *turingv1.AuditPayload) {},
		},
		{
			id:         "d_wrong_type_number",
			action:     "approval.approved",
			payload:    `{"toolName":"files.update","comment":123}`,
			wantFields: []string{"tool_name"},
			verify:     func(t *testing.T, p *turingv1.AuditPayload) {},
		},
		{
			id:         "d_wrong_type_object",
			action:     "approval.denied",
			payload:    `{"toolName":"files.update","reason":{"text":"nope"}}`,
			wantFields: []string{"tool_name"},
			verify:     func(t *testing.T, p *turingv1.AuditPayload) {},
		},
		{
			id:         "d_flag_wrong_type",
			action:     "approval.approved",
			payload:    `{"toolName":"files.update","comment":"ok","commentTruncated":"true"}`,
			wantFields: []string{"tool_name", "decision_comment"},
			verify:     func(t *testing.T, p *turingv1.AuditPayload) {},
		},
		{
			id:         "d_control_nul",
			action:     "approval.approved",
			payload:    `{"toolName":"files.update","comment":"a\u0000b"}`,
			wantFields: []string{"tool_name"},
			verify:     func(t *testing.T, p *turingv1.AuditPayload) {},
		},
		{
			id:         "d_control_bell",
			action:     "approval.denied",
			payload:    `{"toolName":"files.update","reason":"a\u0007b"}`,
			wantFields: []string{"tool_name"},
			verify:     func(t *testing.T, p *turingv1.AuditPayload) {},
		},
		{
			id:         "d_control_escape",
			action:     "approval.approved",
			payload:    `{"toolName":"files.update","comment":"a\u001b[31mred"}`,
			wantFields: []string{"tool_name"},
			verify:     func(t *testing.T, p *turingv1.AuditPayload) {},
		},
		{
			// U+0085 NEL is a C1 control, not ordinary text whitespace.
			id:         "d_control_c1",
			action:     "approval.denied",
			payload:    `{"toolName":"files.update","reason":"a\u0085b"}`,
			wantFields: []string{"tool_name"},
			verify:     func(t *testing.T, p *turingv1.AuditPayload) {},
		},
		{
			// The approve path stores `comment`; a stray `reason` on it is not
			// a denial reason and must not be projected as one — nor as the
			// tool-policy `reason` field.
			id:         "d_cross_key_approved",
			action:     "approval.approved",
			payload:    `{"toolName":"files.update","reason":"CROSS_KEY","reasonTruncated":true}`,
			wantFields: []string{"tool_name"},
			verify:     func(t *testing.T, p *turingv1.AuditPayload) {},
		},
		{
			id:         "d_cross_key_denied",
			action:     "approval.denied",
			payload:    `{"toolName":"files.update","comment":"CROSS_KEY","commentTruncated":true}`,
			wantFields: []string{"tool_name"},
			verify:     func(t *testing.T, p *turingv1.AuditPayload) {},
		},
		{
			id:         "d_other_requested",
			action:     "approval.requested",
			payload:    `{"toolName":"files.update","comment":"CROSS_ACTION","commentTruncated":true,"reason":"CROSS_ACTION","reasonTruncated":true}`,
			wantFields: []string{"tool_name"},
			verify:     func(t *testing.T, p *turingv1.AuditPayload) {},
		},
		{
			id:         "d_other_expired",
			action:     "approval.expired",
			payload:    `{"toolName":"files.update","comment":"CROSS_ACTION","reason":"CROSS_ACTION"}`,
			wantFields: []string{"tool_name"},
			verify:     func(t *testing.T, p *turingv1.AuditPayload) {},
		},
		{
			id:         "d_other_consumed",
			action:     "approval.consumed",
			payload:    `{"toolName":"files.update","comment":"CROSS_ACTION","reason":"CROSS_ACTION"}`,
			wantFields: []string{"tool_name"},
			verify:     func(t *testing.T, p *turingv1.AuditPayload) {},
		},
		{
			// A non-approval action that happens to carry the same keys is
			// still governed by its own rule: tool.call.* keeps projecting the
			// tool-policy `reason`, and never the human rationale fields.
			id:         "d_tool_call",
			action:     "tool.call.before",
			payload:    `{"toolName":"files.update","reason":"needs-approval","comment":"CROSS_ACTION","commentTruncated":true,"reasonTruncated":true}`,
			wantFields: []string{"tool_name", "reason"},
			verify: func(t *testing.T, p *turingv1.AuditPayload) {
				if p.GetReason() != "needs-approval" {
					t.Fatalf("tool.call.before reason = %q", p.GetReason())
				}
			},
		},
	}

	for i, row := range rows {
		insertAuditRow(t, database, row.id, "run_rationale", "client", "user_1", row.action, "appr_"+row.id, row.payload, canonicalTime(i))
	}

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		Order: turingv1.AuditOrder_AUDIT_ORDER_ASCENDING,
		Page:  &turingv1.PageRequest{Limit: 100},
	})
	byID := map[string]*turingv1.AuditEntry{}
	for _, entry := range resp.Entries {
		byID[entry.AuditId] = entry
	}
	if len(byID) != len(rows) {
		t.Fatalf("returned %d distinct entries, want %d", len(byID), len(rows))
	}

	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			entry, ok := byID[row.id]
			if !ok {
				t.Fatalf("entry %s missing from response", row.id)
			}
			if entry.Payload.State != turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_PRESENT {
				t.Fatalf("payload state = %v, want PRESENT", entry.Payload.State)
			}
			if got := presentPayloadFields(entry.Payload); !sameStringSet(got, row.wantFields) {
				t.Fatalf("projected fields = %v, want exactly %v", got, row.wantFields)
			}
			row.verify(t, entry.Payload)
		})
	}

	// Nothing a rejected or cross-keyed rationale carried may reach the wire
	// under any field name, and no approval row may leak its target (the JTI).
	raw, err := protojson.Marshal(resp)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	for _, sentinel := range []string{"CROSS_KEY", "CROSS_ACTION", overBound, "\\u001b", "\\u0000", "\\u0007", "\\u0085", "appr_d_comment"} {
		if strings.Contains(string(raw), sentinel) {
			t.Fatalf("response leaked %q: %s", sentinel, raw)
		}
	}
}

// The human rationale is the one field this API deliberately discloses in the
// user's own words, so it has to arrive through its typed field and nothing
// else: not as raw payload JSON, and never alongside the approval id that is
// also the approval JWT's `jti`.
func TestListAuditEntriesReturnsRationaleOnlyInTypedFieldsAndNeverTheApprovalJTI(t *testing.T) {
	service, database := newAuditServer(t)

	const jti = "appr_01JJTISENTINEL"
	insertAuditRow(t, database, "j_ok", "run_jti", "client", "user_1", "approval.approved", jti,
		`{"toolName":"files.update","comment":"RATIONALE_SENTINEL","commentTruncated":true}`, canonicalTime(0))
	insertAuditRow(t, database, "j_deny", "run_jti", "client", "user_1", "approval.denied", jti+"_deny",
		`{"toolName":"files.update","reason":"DENIAL_SENTINEL"}`, canonicalTime(1))

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		Order: turingv1.AuditOrder_AUDIT_ORDER_ASCENDING,
		Page:  &turingv1.PageRequest{Limit: 100},
	})
	raw, err := protojson.Marshal(resp)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	body := string(raw)

	for _, entry := range resp.Entries {
		if entry.Target != nil {
			t.Fatalf("approval entry %s returned target %q, want omitted", entry.AuditId, *entry.Target)
		}
	}
	if strings.Contains(body, jti) {
		t.Fatalf("response leaked the approval JTI: %s", body)
	}
	if !strings.Contains(body, `"decisionComment":"RATIONALE_SENTINEL"`) {
		t.Fatalf("decisionComment missing from the wire form: %s", body)
	}
	if !strings.Contains(body, `"decisionCommentTruncated":true`) {
		t.Fatalf("decisionCommentTruncated missing from the wire form: %s", body)
	}
	if !strings.Contains(body, `"denialReason":"DENIAL_SENTINEL"`) {
		t.Fatalf("denialReason missing from the wire form: %s", body)
	}
	// The stored JSON keys themselves never appear: the value travels in a
	// typed field, not as a passed-through payload object.
	for _, storedKey := range []string{`"comment"`, `"commentTruncated"`, `"reason"`, `"reasonTruncated"`, "payloadJson"} {
		if strings.Contains(body, storedKey) {
			t.Fatalf("response carried the stored payload key %s: %s", storedKey, body)
		}
	}
}

// The rationale reader is reached through encoding/json in production, which
// can never hand it invalid UTF-8. These cases drive it directly so the shape
// rules that path cannot exercise — invalid UTF-8, a non-string value, an
// absent key — are proven rather than assumed, alongside the ones it can.
func TestAuditHumanRationaleAcceptsTypedTextAndRejectsUnsafeShapes(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  *string
	}{
		{"absent", nil, nil},
		{"empty is present", "", stringPtr("")},
		{"plain text", "looks fine", stringPtr("looks fine")},
		{"newline tab carriage return", "a\nb\tc\r\n", stringPtr("a\nb\tc\r\n")},
		{"unicode text", "está bien ✅", stringPtr("está bien ✅")},
		{"at the byte bound", strings.Repeat("x", maxAuditDecisionRationaleBytes), stringPtr(strings.Repeat("x", maxAuditDecisionRationaleBytes))},
		{"one byte over the bound", strings.Repeat("x", maxAuditDecisionRationaleBytes+1), nil},
		{"invalid utf-8", "ok\xffbad", nil},
		{"nul", "a\x00b", nil},
		{"escape", "a\x1bb", nil},
		{"delete", "a\x7fb", nil},
		{"c1 control", "a\u0085b", nil},
		{"bool", true, nil},
		{"number", json.Number("1"), nil},
		{"object", map[string]any{"text": "nope"}, nil},
		{"array", []any{"nope"}, nil},
		{"null", nil, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			object := map[string]any{}
			if tc.name != "absent" {
				object["comment"] = tc.value
			}
			got := auditHumanRationale(object, "comment")
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("got %q, want omitted", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("got omitted, want %q", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Fatalf("got %q, want %q", *got, *tc.want)
			}
		})
	}
}

func stringPtr(value string) *string { return &value }

func TestListAuditEntriesDistinguishesAbsentPresentAndScrubbedPayloads(t *testing.T) {
	service, database := newAuditServer(t)

	oversized := "{" + strings.Repeat(" ", 16*1024+1) + "}"
	insertAuditRow(t, database, "p_absent", "run_state", "runtime", "", "payload.absent", "", "", canonicalTime(0))
	insertAuditRow(t, database, "p_scrubbed", "run_state", "runtime", "", "session.deleted", "", `{"scrubbed":true}`, canonicalTime(1))
	insertAuditRow(t, database, "p_present", "run_state", "runtime", "", "tool.call.before", "", `{"toolName":"files.update"}`, canonicalTime(2))
	insertAuditRow(t, database, "p_malformed", "run_state", "runtime", "", "tool.call.before", "", `{"toolName":`, canonicalTime(3))
	insertAuditRow(t, database, "p_nonobject", "run_state", "runtime", "", "tool.call.before", "", `["toolName","files.update"]`, canonicalTime(4))
	insertAuditRow(t, database, "p_oversized", "run_state", "runtime", "", "tool.call.before", "", oversized, canonicalTime(5))
	// One valid object followed by a trailing JSON token. parseAuditObject
	// requires EOF after the object, so this is treated as malformed: PRESENT
	// metadata only, never the fields of the leading object.
	insertAuditRow(t, database, "p_trailing", "run_state", "runtime", "", "tool.call.before", "", `{"toolName":"files.update"} 123`, canonicalTime(6))

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		CorrelationId: strptr("run_state"),
		Order:         turingv1.AuditOrder_AUDIT_ORDER_ASCENDING,
		Page:          &turingv1.PageRequest{Limit: 100},
	})
	byID := map[string]*turingv1.AuditEntry{}
	for _, entry := range resp.Entries {
		byID[entry.AuditId] = entry
	}

	if got := byID["p_absent"].Payload.State; got != turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_ABSENT {
		t.Fatalf("absent state = %v, want ABSENT", got)
	}
	if got := byID["p_scrubbed"].Payload.State; got != turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_SCRUBBED {
		t.Fatalf("scrubbed state = %v, want SCRUBBED", got)
	}
	if len(presentPayloadFields(byID["p_scrubbed"].Payload)) != 0 {
		t.Fatalf("scrubbed payload leaked fields: %v", presentPayloadFields(byID["p_scrubbed"].Payload))
	}
	present := byID["p_present"].Payload
	if present.State != turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_PRESENT || present.GetToolName() != "files.update" {
		t.Fatalf("present payload = %+v, want PRESENT with tool_name", present)
	}
	for _, id := range []string{"p_malformed", "p_nonobject", "p_oversized", "p_trailing"} {
		payload := byID[id].Payload
		if payload.State != turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_PRESENT {
			t.Fatalf("%s state = %v, want PRESENT (metadata-only, never an RPC failure)", id, payload.State)
		}
		if len(presentPayloadFields(payload)) != 0 {
			t.Fatalf("%s exposed payload fields from an unparseable/oversized body: %v", id, presentPayloadFields(payload))
		}
	}
}

func TestUnknownAuditActionReturnsMetadataAndPresentStateWithoutPayloadFields(t *testing.T) {
	service, database := newAuditServer(t)

	insertAuditRow(t, database, "u1", "run_unknown", "runtime", "worker", "memory.updated", "mem_1", `{"toolName":"files.update","secret":"top"}`, canonicalTime(0))

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		CorrelationId: strptr("run_unknown"),
		Page:          &turingv1.PageRequest{Limit: 10},
	})
	if len(resp.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(resp.Entries))
	}
	entry := resp.Entries[0]
	if entry.Action != "memory.updated" || entry.GetCorrelationId() != "run_unknown" || entry.GetActorId() != "worker" || entry.GetTarget() != "mem_1" {
		t.Fatalf("metadata wrong for unknown action: %+v", entry)
	}
	if entry.Payload.State != turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_PRESENT {
		t.Fatalf("unknown action state = %v, want PRESENT", entry.Payload.State)
	}
	if len(presentPayloadFields(entry.Payload)) != 0 {
		t.Fatalf("unknown action leaked payload fields (default-deny broken): %v", presentPayloadFields(entry.Payload))
	}
}

func TestListAuditEntriesNeverReturnsSecretsOrUnboundedToolContent(t *testing.T) {
	service, database := newAuditServer(t)

	longTool := strings.Repeat("A", 5000)
	sentinels := []string{
		"SENTINEL_ARGS", "SENTINEL_RESULT", "SENTINEL_ERRMSG", "SENTINEL_APPROVAL_TOKEN",
		"SENTINEL_APPROVAL_JTI", "SENTINEL_JTI", "SENTINEL_AUTHORIZATION", "SENTINEL_APIKEY",
		"SENTINEL_CREDENTIAL", "SENTINEL_PASSWORD", "SENTINEL_SECRET", "SENTINEL_UNKNOWN", longTool,
	}
	payload := fmt.Sprintf(`{
		"toolName": %q,
		"serverName": "files",
		"args": {"path": "SENTINEL_ARGS", "nested": {"deep": "SENTINEL_UNKNOWN"}},
		"resultSummary": "SENTINEL_RESULT",
		"error": {"code": "E_OK", "message": "SENTINEL_ERRMSG"},
		"approvalToken": "SENTINEL_APPROVAL_TOKEN",
		"approvalJti": "SENTINEL_APPROVAL_JTI",
		"jti": "SENTINEL_JTI",
		"authorization": "SENTINEL_AUTHORIZATION",
		"apiKey": "SENTINEL_APIKEY",
		"credential": "SENTINEL_CREDENTIAL",
		"password": "SENTINEL_PASSWORD",
		"secret": "SENTINEL_SECRET"
	}`, longTool)
	insertAuditRow(t, database, "s1", "run_secret", "runtime", "", "tool.call.after", "", payload, canonicalTime(0))

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		CorrelationId: strptr("run_secret"),
		Page:          &turingv1.PageRequest{Limit: 10},
	})
	raw, err := protojson.Marshal(resp)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	for _, sentinel := range sentinels {
		if strings.Contains(string(raw), sentinel) {
			t.Fatalf("response leaked sentinel %q: %s", sentinel, raw)
		}
	}
	// The overlong tool_name must be omitted entirely, never truncated into
	// acceptance.
	if resp.Entries[0].Payload.ToolName != nil {
		t.Fatalf("tool_name = %q, want omitted because it exceeds the byte bound", resp.Entries[0].Payload.GetToolName())
	}
	// error.code was safe and in-bounds, so it should still come through.
	if resp.Entries[0].Payload.GetErrorCode() != "E_OK" {
		t.Fatalf("error_code = %q, want E_OK (only error.code is allowed, never error.message)", resp.Entries[0].Payload.GetErrorCode())
	}
}

// Every approval.* audit row is written with the approval id as its target
// (service/approvals/service.go), and that same approval id is the `jti` claim
// of the short-lived approval JWT handed to mcp-files. The public contract says
// a JTI is never returned, so target has to be dropped for the whole
// approval.* family — including approval actions nobody has written yet, which
// is why the rule is a prefix and not a five-way list.
func TestListAuditEntriesNeverReturnsApprovalTargetJTI(t *testing.T) {
	service, database := newAuditServer(t)

	const sentinelJTI = "appr_SENTINEL-JTI-6b31f0-do-not-leak"
	approvalActions := []string{
		"approval.requested", "approval.approved", "approval.denied",
		"approval.expired", "approval.consumed",
	}
	for i, action := range approvalActions {
		insertAuditRow(t, database, fmt.Sprintf("jti_%d", i), "run_jti", "runtime", "actor_jti",
			action, sentinelJTI, `{"toolName":"files.update"}`, canonicalTime(i))
	}
	// A future approval action nobody has reviewed yet: the prefix rule must
	// already cover it.
	insertAuditRow(t, database, "jti_future", "run_jti", "runtime", "actor_jti",
		"approval.rationale.recorded", sentinelJTI, `{"toolName":"files.update"}`, canonicalTime(len(approvalActions)))
	// A non-approval action whose target is an ordinary, safe identifier: it
	// must keep mapping, or this fix would have quietly blanked every target.
	insertAuditRow(t, database, "jti_tool", "run_jti", "runtime", "actor_jti",
		"tool.call.before", "call_safe_1", `{"toolName":"files.update"}`, canonicalTime(len(approvalActions)+1))

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		CorrelationId: strptr("run_jti"),
		Order:         turingv1.AuditOrder_AUDIT_ORDER_ASCENDING,
		Page:          &turingv1.PageRequest{Limit: 100},
	})
	byID := map[string]*turingv1.AuditEntry{}
	for _, entry := range resp.Entries {
		byID[entry.AuditId] = entry
	}
	if len(byID) != len(approvalActions)+2 {
		t.Fatalf("got %d entries, want %d", len(byID), len(approvalActions)+2)
	}

	for i, action := range approvalActions {
		entry := byID[fmt.Sprintf("jti_%d", i)]
		if entry == nil {
			t.Fatalf("%s row missing from the response", action)
		}
		if entry.Target != nil {
			t.Fatalf("%s target = %q, want omitted (it is the approval JWT jti)", action, entry.GetTarget())
		}
		// Everything else that is safe still has to come through, or the
		// omission would have cost the row its evidentiary value.
		if entry.Action != action || entry.GetCorrelationId() != "run_jti" || entry.GetActorId() != "actor_jti" {
			t.Fatalf("%s row lost safe metadata: %+v", action, entry)
		}
		if entry.Payload.GetToolName() != "files.update" {
			t.Fatalf("%s tool_name = %q, want files.update", action, entry.Payload.GetToolName())
		}
	}
	if got := byID["jti_future"]; got == nil || got.Target != nil {
		t.Fatalf("future approval action target = %v, want omitted under the approval. prefix rule", got)
	}
	if got := byID["jti_tool"]; got == nil || got.GetTarget() != "call_safe_1" {
		t.Fatalf("non-approval target = %v, want the safe stored value to still map", got)
	}

	raw, err := protojson.Marshal(resp)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	if strings.Contains(string(raw), sentinelJTI) {
		t.Fatalf("response leaked the approval jti sentinel: %s", raw)
	}
}

// app.go's persistAuthFailure stores the peer address as actor_id. The
// documented contract says the recorded user agent and peer address are never
// returned, so actor_id has to be dropped for auth.failed specifically — while
// the method (target) and the allowlisted method/requestId payload stay, which
// is what makes the row useful at all.
func TestListAuditEntriesNeverReturnsAuthFailedPeerActorID(t *testing.T) {
	service, database := newAuditServer(t)

	const sentinelPeer = "203.0.113.77:65432-SENTINEL-PEER-do-not-leak"
	const sentinelUserAgent = "SENTINEL-USER-AGENT-do-not-leak/1.0"
	const method = "/turing.v1.HealthService/Check"

	insertAuditRow(t, database, "auth_1", "run_auth", "client", sentinelPeer, "auth.failed", method,
		fmt.Sprintf(`{"method":%q,"requestId":"req_auth_1","userAgent":%q,"peer":%q}`, method, sentinelUserAgent, sentinelPeer),
		canonicalTime(0))
	// A different action recorded with the same actor_id value: actor_id is not
	// sensitive in general, only on auth.failed, so this one must still map.
	insertAuditRow(t, database, "auth_other", "run_auth", "client", "automation_42", "automation.tool.blocked", "call_2",
		`{"toolName":"files.delete"}`, canonicalTime(1))

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		CorrelationId: strptr("run_auth"),
		Order:         turingv1.AuditOrder_AUDIT_ORDER_ASCENDING,
		Page:          &turingv1.PageRequest{Limit: 100},
	})
	if len(resp.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(resp.Entries))
	}
	authEntry, otherEntry := resp.Entries[0], resp.Entries[1]

	if authEntry.ActorId != nil {
		t.Fatalf("auth.failed actor_id = %q, want omitted (it is the peer address)", authEntry.GetActorId())
	}
	if authEntry.GetTarget() != method {
		t.Fatalf("auth.failed target = %q, want the method %q", authEntry.GetTarget(), method)
	}
	if authEntry.ActorType != "client" || authEntry.GetCorrelationId() != "run_auth" {
		t.Fatalf("auth.failed row lost safe metadata: %+v", authEntry)
	}
	if authEntry.Payload.GetMethod() != method || authEntry.Payload.GetRequestId() != "req_auth_1" {
		t.Fatalf("auth.failed payload = %+v, want the allowlisted method and request id", authEntry.Payload)
	}
	if got := otherEntry.GetActorId(); got != "automation_42" {
		t.Fatalf("non-auth.failed actor_id = %q, want it to still map", got)
	}

	raw, err := protojson.Marshal(resp)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	for _, sentinel := range []string{sentinelPeer, sentinelUserAgent} {
		if strings.Contains(string(raw), sentinel) {
			t.Fatalf("response leaked auth.failed sentinel %q: %s", sentinel, raw)
		}
	}
}

func TestListAuditEntriesUsesStableFilterBoundCursors(t *testing.T) {
	service, database := newAuditServer(t)

	ids := []string{"c1", "c2", "c3", "c4", "c5"}
	for _, id := range ids {
		// All five share one timestamp, so only the rowid tie-breaker keeps
		// pagination stable across pages.
		insertAuditRow(t, database, id, "run_page", "runtime", "", "paged.action", "", "", canonicalTime(0))
	}

	paginate := func(order turingv1.AuditOrder) []string {
		var seen []string
		cursor := ""
		for page := 0; page < 10; page++ {
			req := &turingv1.ListAuditEntriesRequest{
				CorrelationId: strptr("run_page"),
				Order:         order,
				Page:          &turingv1.PageRequest{Limit: 2, Cursor: cursor},
			}
			resp := listEntries(t, service, req)
			for _, entry := range resp.Entries {
				seen = append(seen, entry.AuditId)
			}
			if resp.Page.GetNextCursor() == "" {
				return seen
			}
			if len(resp.Entries) != 2 {
				t.Fatalf("page with a next cursor returned %d entries, want the full page of 2", len(resp.Entries))
			}
			cursor = resp.Page.GetNextCursor()
		}
		t.Fatalf("pagination did not terminate")
		return nil
	}

	asc := paginate(turingv1.AuditOrder_AUDIT_ORDER_ASCENDING)
	if !equalStrings(asc, []string{"c1", "c2", "c3", "c4", "c5"}) {
		t.Fatalf("ascending stitched pages = %v, want every row once in order", asc)
	}
	desc := paginate(turingv1.AuditOrder_AUDIT_ORDER_DESCENDING)
	if !equalStrings(desc, []string{"c5", "c4", "c3", "c2", "c1"}) {
		t.Fatalf("descending stitched pages = %v, want every row once in reverse order", desc)
	}

	// A single last page (no over-fetch) must carry no next cursor.
	only := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		CorrelationId: strptr("run_page"),
		Page:          &turingv1.PageRequest{Limit: 100},
	})
	if only.Page.GetNextCursor() != "" {
		t.Fatalf("full result set returned a next cursor %q, want empty", only.Page.GetNextCursor())
	}
}

func TestListAuditEntriesRejectsTamperedOrReusedCursors(t *testing.T) {
	service, database := newAuditServer(t)

	for i := 0; i < 3; i++ {
		insertAuditRow(t, database, fmt.Sprintf("t%d", i), "run_cursor", "runtime", "", "tool.call.before", "", `{"toolName":"files.update"}`, canonicalTime(i))
	}

	baseReq := func() *turingv1.ListAuditEntriesRequest {
		return &turingv1.ListAuditEntriesRequest{
			CorrelationId: strptr("run_cursor"),
			Action:        strptr("tool.call.before"),
			Order:         turingv1.AuditOrder_AUDIT_ORDER_ASCENDING,
			Page:          &turingv1.PageRequest{Limit: 2},
		}
	}
	first := listEntries(t, service, baseReq())
	validCursor := first.Page.GetNextCursor()
	if validCursor == "" {
		t.Fatalf("expected a next cursor to tamper with")
	}

	// The unmodified cursor with the same filters must be accepted.
	reuse := baseReq()
	reuse.Page.Cursor = validCursor
	if _, err := service.ListAuditEntries(context.Background(), reuse); err != nil {
		t.Fatalf("valid cursor rejected: %v", err)
	}

	malformedCases := map[string]string{
		"not base64": "not*base64*", // '*' is outside the raw-url alphabet
		// A syntactically valid raw-URL-base64 string (length 2052, divisible by
		// 4, all 'A' == zero bits) that is longer than the 2048 cap. base64
		// would decode it happily, so the only thing that can reject it is the
		// encoded-length guard — which is exactly what this case proves.
		"overlong valid base64": strings.Repeat("A", 2052),
		"malformed json":        base64.RawURLEncoding.EncodeToString([]byte("{not json")),
		"non object":            base64.RawURLEncoding.EncodeToString([]byte(`123`)),
		// A valid, known-field cursor object followed by a trailing JSON token.
		// base64, DisallowUnknownFields, and the object decode all pass, so only
		// the require-EOF check after the object can reject these — proving that
		// check is reached, not shadowed by an earlier guard.
		"trailing number token": appendCursorTrailer(t, validCursor, " 123"),
		"trailing object token": appendCursorTrailer(t, validCursor, "{}"),
		"unknown field":         tamperCursor(t, validCursor, func(m map[string]any) { m["extra"] = "x" }),
		"bad version":           tamperCursor(t, validCursor, func(m map[string]any) { m["v"] = 2 }),
		"zero row id":           tamperCursor(t, validCursor, func(m map[string]any) { m["rowID"] = 0 }),
		"negative row id":       tamperCursor(t, validCursor, func(m map[string]any) { m["rowID"] = -1 }),
		"bad timestamp":         tamperCursor(t, validCursor, func(m map[string]any) { m["createdAt"] = "2026-05-01T00:00:00Z" }),
		"garbage time":          tamperCursor(t, validCursor, func(m map[string]any) { m["createdAt"] = "nope" }),
		// The anchor itself is now authenticated: swapping in a different but
		// individually valid rowID (still positive) or a different but still
		// canonical createdAt, while retaining the original MAC, must be rejected.
		// The filter fingerprint alone never covered these, so before the MAC was
		// added both of these mutations were silently accepted.
		"mutated positive row id": tamperCursor(t, validCursor, func(m map[string]any) { m["rowID"] = 987654321 }),
		"mutated canonical created at": tamperCursor(t, validCursor, func(m map[string]any) {
			m["createdAt"] = "2026-05-01T00:00:09.000000000Z"
		}),
		"bad fp shape": tamperCursor(t, validCursor, func(m map[string]any) { m["fingerprint"] = "abc" }),
		// Exactly 64 characters but not lowercase hex: an uppercase 'A' and a
		// stray 'g' each exercise the per-character lowercase-hex shape guard,
		// which the length-only "bad fp shape" case above cannot reach.
		"uppercase hex fingerprint": tamperCursor(t, validCursor, func(m map[string]any) {
			m["fingerprint"] = strings.Repeat("0", 63) + "A"
		}),
		"non hex fingerprint": tamperCursor(t, validCursor, func(m map[string]any) {
			m["fingerprint"] = strings.Repeat("0", 63) + "g"
		}),
		"wrong fingerprint": tamperCursor(t, validCursor, func(m map[string]any) {
			m["fingerprint"] = strings.Repeat("0", 64)
		}),
		// The MAC has the same 64-lowercase-hex shape as the fingerprint: a
		// too-short value fails the shape guard, an uppercase value fails the
		// lowercase-hex guard, and a well-shaped but wrong MAC fails the
		// constant-time comparison against the server's key.
		"bad mac shape": tamperCursor(t, validCursor, func(m map[string]any) { m["mac"] = "abc" }),
		"uppercase mac": tamperCursor(t, validCursor, func(m map[string]any) {
			m["mac"] = strings.Repeat("0", 63) + "A"
		}),
		"wrong mac": tamperCursor(t, validCursor, func(m map[string]any) {
			m["mac"] = strings.Repeat("0", 64)
		}),
	}
	for name, cursor := range malformedCases {
		t.Run(name, func(t *testing.T) {
			req := baseReq()
			req.Page.Cursor = cursor
			_, err := service.ListAuditEntries(context.Background(), req)
			requireCode(t, err, codes.InvalidArgument)
		})
	}

	// Reusing a valid cursor under different filters/order must be rejected by
	// the fingerprint binding, not silently honored.
	reuseCases := map[string]func(*turingv1.ListAuditEntriesRequest){
		"changed action":      func(r *turingv1.ListAuditEntriesRequest) { r.Action = strptr("tool.call.after") },
		"changed correlation": func(r *turingv1.ListAuditEntriesRequest) { r.CorrelationId = strptr("run_other") },
		"changed order":       func(r *turingv1.ListAuditEntriesRequest) { r.Order = turingv1.AuditOrder_AUDIT_ORDER_DESCENDING },
		"added time filter": func(r *turingv1.ListAuditEntriesRequest) {
			r.CreatedAtStart = timestamppb.New(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
		},
	}
	for name, mutate := range reuseCases {
		t.Run(name, func(t *testing.T) {
			req := baseReq()
			req.Page.Cursor = validCursor
			mutate(req)
			_, err := service.ListAuditEntries(context.Background(), req)
			requireCode(t, err, codes.InvalidArgument)
		})
	}
}

// TestListAuditEntriesCursorMACIsBoundToServerSecret proves the cursor MAC key
// is derived from the configured secret: a cursor minted under one secret is
// accepted by a freshly reconstructed server over the same repo only when it is
// given the same secret, and rejected under a different secret. This is what
// lets pagination survive a restart when a stable secret (the app wires the
// server-side TURING_APPROVAL_JWT_SECRET) is wired, while keeping cursors
// unforgeable across differently-keyed deployments.
func TestListAuditEntriesCursorMACIsBoundToServerSecret(t *testing.T) {
	database := openAuditTestDB(t)
	repo := repository.New(database)
	for i := 0; i < 3; i++ {
		insertAuditRow(t, database, fmt.Sprintf("sec%d", i), "run_secret", "runtime", "", "tool.call.before", "", `{"toolName":"files.update"}`, canonicalTime(i))
	}

	baseReq := func() *turingv1.ListAuditEntriesRequest {
		return &turingv1.ListAuditEntriesRequest{
			CorrelationId: strptr("run_secret"),
			Action:        strptr("tool.call.before"),
			Order:         turingv1.AuditOrder_AUDIT_ORDER_ASCENDING,
			Page:          &turingv1.PageRequest{Limit: 2},
		}
	}

	minted := New(repo, "secret-A")
	first := listEntries(t, minted, baseReq())
	cursor := first.Page.GetNextCursor()
	if cursor == "" {
		t.Fatalf("expected a next cursor to test key binding")
	}

	// Same repo, same secret, brand-new server: the derived key matches, so the
	// cursor is accepted across the reconstruction.
	sameSecret := New(repo, "secret-A")
	reuse := baseReq()
	reuse.Page.Cursor = cursor
	if _, err := sameSecret.ListAuditEntries(context.Background(), reuse); err != nil {
		t.Fatalf("cursor rejected by a reconstructed server with the same secret: %v", err)
	}

	// Same repo, different secret: the derived key differs, so the MAC fails.
	otherSecret := New(repo, "secret-B")
	req := baseReq()
	req.Page.Cursor = cursor
	_, err := otherSecret.ListAuditEntries(context.Background(), req)
	requireCode(t, err, codes.InvalidArgument)
}

// TestNewPanicsOnMultipleCursorSecrets locks the constructor contract: at most
// one cursor secret may be passed, and a misuse is a programming error that
// must panic rather than silently pick one.
func TestNewPanicsOnMultipleCursorSecrets(t *testing.T) {
	database := openAuditTestDB(t)
	repo := repository.New(database)
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("New did not panic when given two cursor secrets")
		}
	}()
	_ = New(repo, "a", "b")
}

func TestListAuditEntriesRejectsInvalidLimitsCursorsFiltersAndTimes(t *testing.T) {
	service, _ := newAuditServer(t)

	if _, err := service.ListAuditEntries(context.Background(), nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("nil request code = %v, want InvalidArgument", status.Code(err))
	}

	validTime := timestamppb.New(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	cases := map[string]*turingv1.ListAuditEntriesRequest{
		"negative limit":       {Page: &turingv1.PageRequest{Limit: -1}},
		"over max limit":       {Page: &turingv1.PageRequest{Limit: 101}},
		"empty correlation":    {CorrelationId: strptr("")},
		"blank correlation":    {CorrelationId: strptr("   ")},
		"control correlation":  {CorrelationId: strptr("run\x001")},
		"newline correlation":  {CorrelationId: strptr("run\n1")},
		"overlong correlation": {CorrelationId: strptr(strings.Repeat("a", 257))},
		"empty action":         {Action: strptr("")},
		"overlong action":      {Action: strptr(strings.Repeat("a", 129))},
		"invalid start":        {CreatedAtStart: &timestamppb.Timestamp{Nanos: 2_000_000_000}},
		"invalid end":          {CreatedAtEnd: &timestamppb.Timestamp{Nanos: 2_000_000_000}},
		"start equals end":     {CreatedAtStart: validTime, CreatedAtEnd: validTime},
		"start after end": {
			CreatedAtStart: timestamppb.New(time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)),
			CreatedAtEnd:   validTime,
		},
		"unknown order": {Order: turingv1.AuditOrder(99)},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := service.ListAuditEntries(context.Background(), req)
			requireCode(t, err, codes.InvalidArgument)
		})
	}
}

func TestListAuditEntriesReturnsInternalWhenRepositoryFails(t *testing.T) {
	database := openAuditTestDB(t)
	service := New(repository.New(database))
	// Closing the database after construction makes the one query fail; the
	// service must answer a generic Internal, never surface a driver error or
	// row contents.
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	_, err := service.ListAuditEntries(context.Background(), &turingv1.ListAuditEntriesRequest{
		Page: &turingv1.PageRequest{Limit: 10},
	})
	requireCode(t, err, codes.Internal)
	if status.Convert(err).Message() != "list audit entries failed" {
		t.Fatalf("message = %q, want generic 'list audit entries failed'", status.Convert(err).Message())
	}
}

// TestListAuditEntriesDefaultsToFiftyWhenPageUnsetOrLimitZero proves the
// documented default page size (50) independently for both ways a client can
// decline to pick one: no Page at all, and Page with Limit 0. Seeding 51 rows
// makes the boundary observable — a page of exactly 50 plus a next cursor — so
// this fails loudly if the default is ever changed to another number or if
// Limit 0 stops meaning "use the default".
func TestListAuditEntriesDefaultsToFiftyWhenPageUnsetOrLimitZero(t *testing.T) {
	service, database := newAuditServer(t)
	for i := 0; i < 51; i++ {
		insertAuditRow(t, database, fmt.Sprintf("d%02d", i), "run_default", "runtime", "", "paged.action", "", "", canonicalTime(i))
	}

	cases := map[string]*turingv1.PageRequest{
		"page unset": nil,
		"limit zero": {Limit: 0},
	}
	for name, page := range cases {
		t.Run(name, func(t *testing.T) {
			resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
				CorrelationId: strptr("run_default"),
				Order:         turingv1.AuditOrder_AUDIT_ORDER_ASCENDING,
				Page:          page,
			})
			if len(resp.Entries) != 50 {
				t.Fatalf("%s returned %d entries, want exactly the default page of 50", name, len(resp.Entries))
			}
			if resp.Page.GetNextCursor() == "" {
				t.Fatalf("%s returned no next cursor, want one because 51 seeded rows exceed the default page", name)
			}
		})
	}
}

// TestListAuditEntriesTimeWindowIsStartInclusiveEndExclusive seeds rows before,
// exactly on the start, inside, exactly on the end, and after a [start, end)
// window and proves only start-inclusive..end-exclusive rows come back. Because
// the window is applied by the repository query, a correct result also proves
// the resolved start/end values actually reached the repository intact.
func TestListAuditEntriesTimeWindowIsStartInclusiveEndExclusive(t *testing.T) {
	service, database := newAuditServer(t)

	seed := []struct {
		id  string
		sec int
	}{
		{"before", 5},
		{"at_start", 10},
		{"inside", 15},
		{"at_end", 20},
		{"after", 25},
	}
	for _, s := range seed {
		insertAuditRow(t, database, s.id, "run_time", "runtime", "", "paged.action", "", "", canonicalTime(s.sec))
	}

	start := time.Date(2026, 5, 1, 0, 0, 10, 0, time.UTC)
	end := time.Date(2026, 5, 1, 0, 0, 20, 0, time.UTC)
	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		CorrelationId:  strptr("run_time"),
		CreatedAtStart: timestamppb.New(start),
		CreatedAtEnd:   timestamppb.New(end),
		Order:          turingv1.AuditOrder_AUDIT_ORDER_ASCENDING,
		Page:           &turingv1.PageRequest{Limit: 100},
	})

	var got []string
	for _, entry := range resp.Entries {
		got = append(got, entry.AuditId)
	}
	// at_start is included (>= start), at_end is excluded (< end), before/after
	// fall outside the window entirely.
	if !equalStrings(got, []string{"at_start", "inside"}) {
		t.Fatalf("windowed ids = %v, want exactly [at_start inside] (start inclusive, end exclusive)", got)
	}
}

// TestListAuditEntriesReturnsStoredCreatedAtInstant proves the projected
// AuditEntry.CreatedAt is the exact stored instant, sub-second digits and all —
// not a re-stamped or truncated value.
func TestListAuditEntriesReturnsStoredCreatedAtInstant(t *testing.T) {
	service, database := newAuditServer(t)

	stored := "2026-05-01T12:34:56.123456789Z"
	insertAuditRow(t, database, "ts1", "run_ts", "runtime", "", "paged.action", "", "", stored)

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		CorrelationId: strptr("run_ts"),
		Page:          &turingv1.PageRequest{Limit: 10},
	})
	if len(resp.Entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(resp.Entries))
	}
	want := time.Date(2026, 5, 1, 12, 34, 56, 123456789, time.UTC)
	got := resp.Entries[0].GetCreatedAt().AsTime()
	if !got.Equal(want) {
		t.Fatalf("created_at = %s, want the stored instant %s", got, want)
	}
}

// TestListAuditEntriesReturnsInternalForUnparseableStoredCreatedAt feeds a row
// whose stored created_at cannot be parsed and proves the read fails closed:
// a generic codes.Internal with the fixed message, and no echo of the offending
// stored value.
func TestListAuditEntriesReturnsInternalForUnparseableStoredCreatedAt(t *testing.T) {
	service, database := newAuditServer(t)

	garbage := "not-a-timestamp"
	insertAuditRow(t, database, "bad_ts", "run_badts", "runtime", "", "paged.action", "", "", garbage)

	_, err := service.ListAuditEntries(context.Background(), &turingv1.ListAuditEntriesRequest{
		CorrelationId: strptr("run_badts"),
		Page:          &turingv1.PageRequest{Limit: 10},
	})
	requireCode(t, err, codes.Internal)
	msg := status.Convert(err).Message()
	if msg != "list audit entries failed" {
		t.Fatalf("message = %q, want generic 'list audit entries failed'", msg)
	}
	if strings.Contains(msg, garbage) {
		t.Fatalf("message leaked the stored created_at value %q: %q", garbage, msg)
	}
}

// TestListAuditEntriesMapsNullAndEmptyMetadataDistinctly proves the optional
// metadata mapping preserves presence semantics: a SQL NULL correlation_id /
// actor_id / target maps to an absent proto optional (nil, Has == false), while
// a stored non-NULL empty string maps to a present empty string (non-nil, "").
// The two must not collapse into the same thing.
func TestListAuditEntriesMapsNullAndEmptyMetadataDistinctly(t *testing.T) {
	service, database := newAuditServer(t)

	// nullOrText turns "" into a SQL NULL, so this row stores NULL for
	// correlation_id, actor_id, and target.
	insertAuditRow(t, database, "null_row", "", "runtime", "", "paged.action", "", "", canonicalTime(0))

	// Non-NULL empty strings can't be expressed through the helper, so insert
	// them directly. The schema allows '' for these nullable TEXT columns;
	// actor_type is constrained, so it keeps a real value.
	if _, err := database.ExecContext(context.Background(), `
		INSERT INTO audit_logs (id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at)
		VALUES (?, '', 'runtime', '', 'paged.action', '', NULL, ?)`,
		"empty_row", canonicalTime(1)); err != nil {
		t.Fatalf("insert empty_row: %v", err)
	}

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		Order: turingv1.AuditOrder_AUDIT_ORDER_ASCENDING,
		Page:  &turingv1.PageRequest{Limit: 100},
	})
	byID := map[string]*turingv1.AuditEntry{}
	for _, entry := range resp.Entries {
		byID[entry.AuditId] = entry
	}

	nullEntry, ok := byID["null_row"]
	if !ok {
		t.Fatalf("null_row missing from response")
	}
	if nullEntry.CorrelationId != nil {
		t.Fatalf("NULL correlation_id mapped to a %q pointer, want absent (nil)", nullEntry.GetCorrelationId())
	}
	if nullEntry.ActorId != nil {
		t.Fatalf("NULL actor_id mapped to a %q pointer, want absent (nil)", nullEntry.GetActorId())
	}
	if nullEntry.Target != nil {
		t.Fatalf("NULL target mapped to a %q pointer, want absent (nil)", nullEntry.GetTarget())
	}

	emptyEntry, ok := byID["empty_row"]
	if !ok {
		t.Fatalf("empty_row missing from response")
	}
	if emptyEntry.CorrelationId == nil || *emptyEntry.CorrelationId != "" {
		t.Fatalf("non-NULL empty correlation_id mapped to %v, want a present empty string", emptyEntry.CorrelationId)
	}
	if emptyEntry.ActorId == nil || *emptyEntry.ActorId != "" {
		t.Fatalf("non-NULL empty actor_id mapped to %v, want a present empty string", emptyEntry.ActorId)
	}
	if emptyEntry.Target == nil || *emptyEntry.Target != "" {
		t.Fatalf("non-NULL empty target mapped to %v, want a present empty string", emptyEntry.Target)
	}
}

// TestListAuditEntriesRejectsInvalidUTF8Filters proves the correlation and
// action filters are rejected specifically by UTF-8 validation — asserting the
// exact per-field message, so the failure can't be attributed to the empty,
// blank, length, or control-character checks that surround it.
func TestListAuditEntriesRejectsInvalidUTF8Filters(t *testing.T) {
	service, _ := newAuditServer(t)

	cases := map[string]struct {
		req  *turingv1.ListAuditEntriesRequest
		want string
	}{
		"correlation invalid utf8": {
			req:  &turingv1.ListAuditEntriesRequest{CorrelationId: strptr("run_\xff")},
			want: "correlation_id must be valid UTF-8",
		},
		"action invalid utf8": {
			req:  &turingv1.ListAuditEntriesRequest{Action: strptr("tool_\xff")},
			want: "action must be valid UTF-8",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := service.ListAuditEntries(context.Background(), tc.req)
			requireCode(t, err, codes.InvalidArgument)
			if msg := status.Convert(err).Message(); msg != tc.want {
				t.Fatalf("message = %q, want %q (must fail specifically on UTF-8 validation)", msg, tc.want)
			}
		})
	}
}

// TestListAuditEntriesEnforcesStrictNumericAndBoolTyping proves the payload
// readers never coerce: a numeric field is projected only for an exact integral
// json.Number in range (fractional, exponent, string, and negative shapes are
// dropped for both durations and counts), and a bool field is projected only
// for an exact JSON bool (a string is dropped, while both true and false pass).
func TestListAuditEntriesEnforcesStrictNumericAndBoolTyping(t *testing.T) {
	service, database := newAuditServer(t)

	seed := []struct {
		id      string
		action  string
		payload string
	}{
		// tool.call.before -> duration_ms via the strict integral reader.
		{"n_frac", "tool.call.before", `{"durationMs":1.5}`},
		{"n_exp", "tool.call.before", `{"durationMs":1e3}`},
		{"n_str", "tool.call.before", `{"durationMs":"12"}`},
		{"n_neg", "tool.call.before", `{"durationMs":-5}`},
		{"n_ok", "tool.call.before", `{"durationMs":12}`},
		{"n_zero", "tool.call.before", `{"durationMs":0}`},
		// approval.requested -> unattended via the strict bool reader.
		{"b_str", "approval.requested", `{"unattended":"true"}`},
		{"b_true", "approval.requested", `{"unattended":true}`},
		{"b_false", "approval.requested", `{"unattended":false}`},
		// session.deleted -> deleted_runs / deleted_messages, same integral rule.
		{"c_frac", "session.deleted", `{"runs":1.5,"messages":2.5}`},
		{"c_exp", "session.deleted", `{"runs":1e3,"messages":2e3}`},
		{"c_str", "session.deleted", `{"runs":"3","messages":"4"}`},
		{"c_neg", "session.deleted", `{"runs":-1,"messages":-2}`},
		{"c_ok", "session.deleted", `{"runs":3,"messages":42}`},
	}
	for i, s := range seed {
		insertAuditRow(t, database, s.id, "run_strict", "runtime", "", s.action, "", s.payload, canonicalTime(i))
	}

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		CorrelationId: strptr("run_strict"),
		Order:         turingv1.AuditOrder_AUDIT_ORDER_ASCENDING,
		Page:          &turingv1.PageRequest{Limit: 100},
	})
	byID := map[string]*turingv1.AuditEntry{}
	for _, entry := range resp.Entries {
		byID[entry.AuditId] = entry
	}

	// Every non-integral / out-of-domain duration shape is dropped, not coerced.
	for _, id := range []string{"n_frac", "n_exp", "n_str", "n_neg"} {
		if p := byID[id].Payload; p.DurationMs != nil {
			t.Fatalf("%s: durationMs coerced to %d, want omitted (no coercion)", id, p.GetDurationMs())
		}
	}
	// Exact integral json.Number is projected, including zero.
	if p := byID["n_ok"].Payload; p.DurationMs == nil || p.GetDurationMs() != 12 {
		t.Fatalf("n_ok: duration_ms = %v, want 12", p.DurationMs)
	}
	if p := byID["n_zero"].Payload; p.DurationMs == nil || p.GetDurationMs() != 0 {
		t.Fatalf("n_zero: duration_ms = %v, want 0", p.DurationMs)
	}

	// A JSON string is never coerced into a bool; an exact bool passes either way.
	if p := byID["b_str"].Payload; p.Unattended != nil {
		t.Fatalf("b_str: string \"true\" coerced to bool %v, want omitted", p.GetUnattended())
	}
	if p := byID["b_true"].Payload; p.Unattended == nil || !p.GetUnattended() {
		t.Fatalf("b_true: unattended = %v, want true", p.Unattended)
	}
	if p := byID["b_false"].Payload; p.Unattended == nil || p.GetUnattended() {
		t.Fatalf("b_false: unattended = %v, want false", p.Unattended)
	}

	// Counts share the integral rule: every invalid shape drops both fields.
	for _, id := range []string{"c_frac", "c_exp", "c_str", "c_neg"} {
		if p := byID[id].Payload; p.DeletedRuns != nil || p.DeletedMessages != nil {
			t.Fatalf("%s: counts coerced (runs=%v messages=%v), want both omitted", id, p.DeletedRuns, p.DeletedMessages)
		}
	}
	if p := byID["c_ok"].Payload; p.GetDeletedRuns() != 3 || p.GetDeletedMessages() != 42 {
		t.Fatalf("c_ok: counts = (%d,%d), want (3,42)", p.GetDeletedRuns(), p.GetDeletedMessages())
	}
}

// TestListAuditEntriesMatchesScrubbedTombstoneExactly proves the SCRUBBED state
// is reserved for the exact deletion tombstone byte string. A semantically
// equal but differently-spaced object, and the false variant, are ordinary
// PRESENT rows projected to metadata only — never mistaken for a scrub.
func TestListAuditEntriesMatchesScrubbedTombstoneExactly(t *testing.T) {
	service, database := newAuditServer(t)

	insertAuditRow(t, database, "exact", "run_tomb", "runtime", "", "session.deleted", "", `{"scrubbed":true}`, canonicalTime(0))
	insertAuditRow(t, database, "spaced", "run_tomb", "runtime", "", "session.deleted", "", `{"scrubbed": true}`, canonicalTime(1))
	insertAuditRow(t, database, "false_variant", "run_tomb", "runtime", "", "session.deleted", "", `{"scrubbed":false}`, canonicalTime(2))

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		CorrelationId: strptr("run_tomb"),
		Order:         turingv1.AuditOrder_AUDIT_ORDER_ASCENDING,
		Page:          &turingv1.PageRequest{Limit: 100},
	})
	byID := map[string]*turingv1.AuditEntry{}
	for _, entry := range resp.Entries {
		byID[entry.AuditId] = entry
	}

	if got := byID["exact"].Payload.State; got != turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_SCRUBBED {
		t.Fatalf("exact tombstone state = %v, want SCRUBBED", got)
	}
	if n := len(presentPayloadFields(byID["exact"].Payload)); n != 0 {
		t.Fatalf("scrubbed tombstone leaked %d payload fields", n)
	}
	for _, id := range []string{"spaced", "false_variant"} {
		p := byID[id].Payload
		if p.State != turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_PRESENT {
			t.Fatalf("%s state = %v, want PRESENT (only the exact tombstone byte string scrubs)", id, p.State)
		}
		if n := len(presentPayloadFields(p)); n != 0 {
			t.Fatalf("%s leaked %d payload fields, want metadata-only PRESENT", id, n)
		}
	}
}

// TestListAuditEntriesTypeGuardsAllowedStringAndNestedErrorFields proves the
// projection's shape guards on allowed fields: a numeric value for a string
// field (toolName) is omitted rather than coerced, and a nested error that is
// not itself a JSON object (a string or an array) yields no error_code. In both
// cases the row still returns PRESENT and a correctly-typed sibling field is
// still projected, so one bad field never fails the RPC or poisons the rest.
func TestListAuditEntriesTypeGuardsAllowedStringAndNestedErrorFields(t *testing.T) {
	service, database := newAuditServer(t)

	seed := []struct {
		id      string
		action  string
		payload string
	}{
		// Numeric toolName is dropped by the string type guard; serverName maps.
		{"g_tool_numeric", "tool.call.before", `{"toolName":123,"serverName":"files"}`},
		// error as a bare string is not an object: error.code is unreadable, so
		// error_code is omitted while the sibling toolName still projects.
		{"g_err_string", "tool.call.after", `{"error":"boom","toolName":"files.update"}`},
		// error as an array is likewise not an object: same omission, no failure.
		{"g_err_array", "tool.call.after", `{"error":["boom"],"toolName":"files.update"}`},
	}
	for i, s := range seed {
		insertAuditRow(t, database, s.id, "run_guard", "runtime", "", s.action, "", s.payload, canonicalTime(i))
	}

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		CorrelationId: strptr("run_guard"),
		Order:         turingv1.AuditOrder_AUDIT_ORDER_ASCENDING,
		Page:          &turingv1.PageRequest{Limit: 100},
	})
	byID := map[string]*turingv1.AuditEntry{}
	for _, entry := range resp.Entries {
		byID[entry.AuditId] = entry
	}

	tool := byID["g_tool_numeric"].Payload
	if tool.State != turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_PRESENT {
		t.Fatalf("g_tool_numeric state = %v, want PRESENT", tool.State)
	}
	if tool.ToolName != nil {
		t.Fatalf("g_tool_numeric: tool_name = %q, want omitted (numeric value must not coerce to string)", tool.GetToolName())
	}
	if tool.GetServerName() != "files" {
		t.Fatalf("g_tool_numeric: server_name = %q, want \"files\" (valid sibling must still map)", tool.GetServerName())
	}

	for _, id := range []string{"g_err_string", "g_err_array"} {
		p := byID[id].Payload
		if p.State != turingv1.AuditPayloadState_AUDIT_PAYLOAD_STATE_PRESENT {
			t.Fatalf("%s state = %v, want PRESENT (a non-object error must not fail the RPC)", id, p.State)
		}
		if p.ErrorCode != nil {
			t.Fatalf("%s: error_code = %q, want omitted (error is not a JSON object)", id, p.GetErrorCode())
		}
		if p.GetToolName() != "files.update" {
			t.Fatalf("%s: tool_name = %q, want \"files.update\" (valid sibling must still map)", id, p.GetToolName())
		}
	}
}

// TestListAuditEntriesOmitsApprovalTargetEvenWhenActionOverReadBound is the
// round-3 regression. An approval.* action longer than the repository's
// 512-byte metadata read bound collapses the bounded Action column to ”,
// which the service then redacts to "[redacted]". The action-prefix
// omit-target rule must not be fooled by that emptied action: the repository
// derives ActionHasApprovalPrefix from the *original* action's first 9 bytes,
// so the row's target (the approval JWT jti) is still dropped. Before the fix
// auditDisclosureFor keyed only on the emptied Action, so strings.HasPrefix
// missed the approval. family and leaked the jti as target.
func TestListAuditEntriesOmitsApprovalTargetEvenWhenActionOverReadBound(t *testing.T) {
	service, database := newAuditServer(t)

	const jti = "appr_01JOVERSIZED-do-not-leak"
	// Over the 512-byte read bound: the bounded projection collapses Action to
	// '', but the approval. prefix is still in the first 9 bytes.
	oversizedApproval := "approval.approved" + strings.Repeat("a", 512)

	insertAuditRow(t, database, "over_appr", "run_over_jti", "runtime", "actor_jti",
		oversizedApproval, jti, `{"toolName":"files.update"}`, canonicalTime(0))

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		CorrelationId: strptr("run_over_jti"),
		Page:          &turingv1.PageRequest{Limit: 10},
	})
	if len(resp.Entries) != 1 {
		t.Fatalf("got %d entries, want 1 (the over-read-bound row must still return)", len(resp.Entries))
	}
	entry := resp.Entries[0]
	// The oversized action is a required field over the read bound, so it
	// redacts rather than surfacing as a bare empty string.
	if entry.Action != redactedAuditMetadata {
		t.Fatalf("action = %q, want %q (over-read-bound required field redacts)", entry.Action, redactedAuditMetadata)
	}
	// The target is the approval jti and must be dropped even though the action
	// that would otherwise key the rule reads back empty.
	if entry.Target != nil {
		t.Fatalf("target = %q, want omitted (an over-bound approval action must still drop its jti target)", entry.GetTarget())
	}
	raw, err := protojson.Marshal(resp)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	if strings.Contains(string(raw), jti) {
		t.Fatalf("response leaked the approval jti: %s", raw)
	}
}

func strptr(value string) *string {
	return &value
}

// TestListAuditEntriesProjectsStructuralMetadataSafely proves the service
// treats stored structural metadata (id, correlation_id, actor_type, actor_id,
// action, target) as untrusted: invalid UTF-8, control characters, and values
// over the field-specific byte bound must never reach the wire. Required
// fields that fail collapse to the fixed "[redacted]" literal; optional fields
// that fail are omitted. A single bad field never fails the whole page, and the
// whole response must still protojson.Marshal (which itself rejects invalid
// UTF-8 in a proto3 string). actor_type is exercised separately in
// TestMapAuditEntryRedactsUnsafeActorType because a DB CHECK forbids storing a
// bad actor_type at all.
func TestListAuditEntriesProjectsStructuralMetadataSafely(t *testing.T) {
	service, database := newAuditServer(t)

	// 300 bytes: over the 256/128 service bounds but under the repository's
	// 512-byte read bound, so it reaches the service intact and the service's
	// own field bounds are what must reject it.
	oversized := strings.Repeat("z", 300)

	// Row 0: fully safe baseline — every field survives unchanged, payload maps.
	insertAuditRow(t, database, "audit_safe", "corr_safe", "runtime", "actor_safe", "tool.call.before", "target_safe", `{"toolName":"files.update"}`, canonicalTime(0))
	// Row 1: invalid UTF-8 in a required (action) and an optional (target).
	insertAuditRow(t, database, "audit_utf8", "corr_utf8", "runtime", "actor_utf8", "\xff\xfe", "\xff\xfe", "", canonicalTime(1))
	// Row 2: control characters — newline in a required (action), newline/tab in
	// optionals (correlation_id/actor_id); a clean target must still survive.
	insertAuditRow(t, database, "audit_ctl", "corr\nctl", "runtime", "actor\tid", "act\nion", "target_ctl", "", canonicalTime(2))
	// Row 3: oversized required must redact, oversized optionals must drop, and a
	// within-bound optional (target) must still survive.
	insertAuditRow(t, database, oversized, oversized, "runtime", oversized, oversized, "target_ok", "", canonicalTime(3))

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		CreatedAtStart: timestamppb.New(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
		CreatedAtEnd:   timestamppb.New(time.Date(2026, 5, 1, 0, 1, 0, 0, time.UTC)),
		Order:          turingv1.AuditOrder_AUDIT_ORDER_ASCENDING,
		Page:           &turingv1.PageRequest{Limit: 100},
	})

	raw, err := protojson.Marshal(resp)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v (a projected metadata value must never be invalid UTF-8)", err)
	}
	if strings.Contains(string(raw), oversized) {
		t.Fatalf("response leaked an over-bound structural value: %s", raw)
	}
	if !strings.Contains(string(raw), "[redacted]") {
		t.Fatalf("response never contained the redaction literal, want required over-bound/unsafe fields redacted: %s", raw)
	}
	if len(resp.Entries) != 4 {
		t.Fatalf("got %d entries, want 4", len(resp.Entries))
	}

	safe := resp.Entries[0]
	if safe.AuditId != "audit_safe" || safe.ActorType != "runtime" || safe.Action != "tool.call.before" {
		t.Fatalf("safe row required = (%q,%q,%q), want unchanged", safe.AuditId, safe.ActorType, safe.Action)
	}
	if safe.GetCorrelationId() != "corr_safe" || safe.GetActorId() != "actor_safe" || safe.GetTarget() != "target_safe" {
		t.Fatalf("safe row optionals = (%q,%q,%q), want unchanged", safe.GetCorrelationId(), safe.GetActorId(), safe.GetTarget())
	}
	if safe.Payload.GetToolName() != "files.update" {
		t.Fatalf("safe row tool_name = %q, want files.update (existing writer metadata must stay unchanged)", safe.Payload.GetToolName())
	}

	utf8Row := resp.Entries[1]
	if utf8Row.Action != "[redacted]" {
		t.Fatalf("invalid-UTF-8 action = %q, want [redacted]", utf8Row.Action)
	}
	if utf8Row.Target != nil {
		t.Fatalf("invalid-UTF-8 target = %q, want omitted", utf8Row.GetTarget())
	}
	if utf8Row.AuditId != "audit_utf8" || utf8Row.GetCorrelationId() != "corr_utf8" || utf8Row.GetActorId() != "actor_utf8" || utf8Row.ActorType != "runtime" {
		t.Fatalf("invalid-UTF-8 row clobbered a safe sibling field: %+v", utf8Row)
	}

	ctlRow := resp.Entries[2]
	if ctlRow.Action != "[redacted]" {
		t.Fatalf("control-char action = %q, want [redacted]", ctlRow.Action)
	}
	if ctlRow.CorrelationId != nil {
		t.Fatalf("control-char correlation_id = %q, want omitted", ctlRow.GetCorrelationId())
	}
	if ctlRow.ActorId != nil {
		t.Fatalf("control-char actor_id = %q, want omitted", ctlRow.GetActorId())
	}
	if ctlRow.AuditId != "audit_ctl" || ctlRow.ActorType != "runtime" || ctlRow.GetTarget() != "target_ctl" {
		t.Fatalf("control-char row clobbered a safe sibling field: %+v", ctlRow)
	}

	overRow := resp.Entries[3]
	if overRow.AuditId != "[redacted]" || overRow.Action != "[redacted]" {
		t.Fatalf("oversized required = (%q,%q), want both [redacted]", overRow.AuditId, overRow.Action)
	}
	if overRow.ActorType != "runtime" {
		t.Fatalf("within-bound actor_type = %q, want runtime", overRow.ActorType)
	}
	if overRow.CorrelationId != nil || overRow.ActorId != nil {
		t.Fatalf("oversized optionals not omitted: correlation=%v actor_id=%v", overRow.CorrelationId, overRow.ActorId)
	}
	if overRow.GetTarget() != "target_ok" {
		t.Fatalf("within-bound target = %q, want target_ok (only over-bound fields drop)", overRow.GetTarget())
	}
}

// TestMapAuditEntryRedactsUnsafeActorType exercises actor_type's structural
// projection directly, because the audit_logs CHECK constraint forbids storing
// an invalid or over-long actor_type through the database at all. Both an
// invalid-UTF-8 value and one over the 32-byte bound must collapse to the fixed
// "[redacted]" literal, while a clean value is preserved.
func TestMapAuditEntryRedactsUnsafeActorType(t *testing.T) {
	cases := map[string]struct {
		actorType string
		want      string
	}{
		"invalid utf8": {"\xff\xfe", "[redacted]"},
		"over bound":   {strings.Repeat("a", 33), "[redacted]"},
		"control char": {"run\ntime", "[redacted]"},
		"clean":        {"runtime", "runtime"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			entry, err := mapAuditEntry(&repository.AuditRecord{
				AuditID:   "audit_x",
				ActorType: tc.actorType,
				Action:    "tool.call.before",
				CreatedAt: "2026-05-01T00:00:00.000000000Z",
			})
			if err != nil {
				t.Fatalf("mapAuditEntry: %v", err)
			}
			if entry.ActorType != tc.want {
				t.Fatalf("actor_type = %q, want %q", entry.ActorType, tc.want)
			}
		})
	}
}

// TestListAuditEntriesRedactsRequiredMetadataOverRepositoryReadBound is the
// Task 3 boundary regression. When a required structural field exceeds the
// repository's maxAuditMetadataReadBytes read bound, the bounded SQL projection
// collapses that column to ” (empty) before the service ever sees it. That
// empty must project as the fixed "[redacted]" literal on the public wire — a
// bare empty string would be indistinguishable from a genuine value and hide
// that the field was actually redacted. audit_id and action are free-form TEXT
// so they can overflow through the schema; actor_type is CHECK-constrained and
// cannot, so it is exercised directly in the map-level tests. This drives the
// row through the real repository, so it also proves the repository still
// returns the over-bound row rather than dropping it.
func TestListAuditEntriesRedactsRequiredMetadataOverRepositoryReadBound(t *testing.T) {
	service, database := newAuditServer(t)

	// 600 bytes: over the repository's 512-byte read bound, so the SQL
	// projection collapses these required columns to '' at the DB layer.
	oversized := strings.Repeat("q", 600)

	insertAuditRow(t, database, oversized, "corr_over", "runtime", "actor_over", oversized, "target_over", `{"toolName":"files.update"}`, canonicalTime(0))

	// The repository must still return the over-bound row, with its required
	// columns collapsed to '' — the empty is the safe overflow marker.
	records, err := repository.New(database).ListAuditRecords(context.Background(), repository.AuditQuery{
		CorrelationID: sql.NullString{String: "corr_over", Valid: true},
		Order:         repository.AuditOrderAscending,
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("ListAuditRecords: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("repository returned %d records, want 1 (the over-bound row must still return)", len(records))
	}
	if records[0].AuditID != "" || records[0].Action != "" {
		t.Fatalf("repository over-bound required = (id=%q, action=%q), want both collapsed to empty", records[0].AuditID, records[0].Action)
	}

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		CorrelationId: strptr("corr_over"),
		Page:          &turingv1.PageRequest{Limit: 10},
	})
	if len(resp.Entries) != 1 {
		t.Fatalf("got %d entries, want 1 (the over-read-bound row must still return)", len(resp.Entries))
	}
	entry := resp.Entries[0]

	// The within-bound optionals prove the row itself came through, so an empty
	// required field is a redaction — not a missing row.
	if entry.GetCorrelationId() != "corr_over" || entry.GetActorId() != "actor_over" || entry.GetTarget() != "target_over" {
		t.Fatalf("optionals = (%q,%q,%q), want the row's within-bound fields intact", entry.GetCorrelationId(), entry.GetActorId(), entry.GetTarget())
	}
	if entry.AuditId != redactedAuditMetadata {
		t.Fatalf("audit_id = %q, want %q (an over-read-bound required field must redact, never emerge empty)", entry.AuditId, redactedAuditMetadata)
	}
	if entry.Action != redactedAuditMetadata {
		t.Fatalf("action = %q, want %q (an over-read-bound required field must redact, never emerge empty)", entry.Action, redactedAuditMetadata)
	}

	raw, err := protojson.Marshal(resp)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	if strings.Contains(string(raw), oversized) {
		t.Fatalf("response leaked an over-read-bound structural value: %s", raw)
	}
}

// TestMapAuditEntryRedactsEmptyRequiredMetadata is the focused map-level proof
// that a genuinely empty required structural field maps to the fixed
// "[redacted]" literal, not a bare empty string. Required writers never emit an
// empty audit_id, actor_type, or action intentionally, so treating empty as
// redactable loses no genuine value and turns the repository's overflow-to-empty
// into a safe redaction marker with no extra public state.
func TestMapAuditEntryRedactsEmptyRequiredMetadata(t *testing.T) {
	entry, err := mapAuditEntry(&repository.AuditRecord{
		AuditID:   "",
		ActorType: "",
		Action:    "",
		CreatedAt: "2026-05-01T00:00:00.000000000Z",
	})
	if err != nil {
		t.Fatalf("mapAuditEntry: %v", err)
	}
	if entry.AuditId != redactedAuditMetadata {
		t.Fatalf("empty audit_id = %q, want %q", entry.AuditId, redactedAuditMetadata)
	}
	if entry.ActorType != redactedAuditMetadata {
		t.Fatalf("empty actor_type = %q, want %q", entry.ActorType, redactedAuditMetadata)
	}
	if entry.Action != redactedAuditMetadata {
		t.Fatalf("empty action = %q, want %q", entry.Action, redactedAuditMetadata)
	}
}

// TestListAuditEntriesOmitsPayloadStringsWithControlCharacters proves the
// action-allowlisted payload string reader rejects control characters too, not
// only invalid UTF-8, over-length, and empty. A display value may carry Unicode
// and spaces but not controls; a bad value is omitted while a safe sibling in
// the same payload still maps.
func TestListAuditEntriesOmitsPayloadStringsWithControlCharacters(t *testing.T) {
	service, database := newAuditServer(t)

	// toolName holds a newline (a control char); serverName is a clean sibling.
	insertAuditRow(t, database, "p_ctl", "run_pctl", "runtime", "", "tool.call.before", "", `{"toolName":"bad\ntool","serverName":"files"}`, canonicalTime(0))

	resp := listEntries(t, service, &turingv1.ListAuditEntriesRequest{
		CorrelationId: strptr("run_pctl"),
		Page:          &turingv1.PageRequest{Limit: 10},
	})
	p := resp.Entries[0].Payload
	if p.ToolName != nil {
		t.Fatalf("tool_name = %q, want omitted (a control character must drop the value)", p.GetToolName())
	}
	if p.GetServerName() != "files" {
		t.Fatalf("server_name = %q, want files (a safe sibling must still map)", p.GetServerName())
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func tamperCursor(t *testing.T, cursor string, mutate func(m map[string]any)) string {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal cursor: %v", err)
	}
	mutate(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal cursor: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(out)
}

// appendCursorTrailer decodes a valid cursor's base64, appends raw trailer bytes
// after the first JSON object, and re-encodes. The result base64-decodes cleanly
// and its first value is still a valid, known-field auditCursorPayload, so decode
// gets past base64, DisallowUnknownFields, and the object decode itself — the only
// thing left that can reject it is the require-EOF check on the trailing token.
func appendCursorTrailer(t *testing.T, cursor, trailer string) string {
	t.Helper()
	data, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		t.Fatalf("decode cursor: %v", err)
	}
	data = append(data, []byte(trailer)...)
	return base64.RawURLEncoding.EncodeToString(data)
}
