package runtime

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/protobuf/proto"
)

func safeBeforeBeacon(enqueued repository.EnqueueUserMessageResult, toolCallID string) *turingv1.ToolCallBeacon {
	return &turingv1.ToolCallBeacon{
		RunId: enqueued.RunID, TraceId: enqueued.TraceID, ToolCallId: toolCallID,
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "system", ToolName: "system.time",
		Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE,
	}
}

func TestSafeBeforeRollsBackWhenStartedEventCannotPersist(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "started event atomicity")
	beacon := safeBeforeBeacon(enqueued, "call_started_atomic")
	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_started_event
		BEFORE INSERT ON events
		WHEN NEW.type = 'tool.call.started'
		BEGIN
			SELECT RAISE(ABORT, 'tool started event unavailable');
		END;
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.handleToolBeacon(context.Background(), beacon); err == nil {
		t.Fatal("BEFORE succeeded despite required tool.call.started event failure")
	}
	var toolStatus string
	err := h.database.QueryRowContext(context.Background(), `SELECT status FROM tool_calls WHERE id = ?`, beacon.ToolCallId).Scan(&toolStatus)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("tool call after required event failure = %q / %v, want no row", toolStatus, err)
	}
	if _, err := h.database.ExecContext(context.Background(), `DROP TRIGGER fail_started_event`); err != nil {
		t.Fatal(err)
	}
	decision, err := h.service.handleToolBeacon(context.Background(), beacon)
	if err != nil || decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("BEFORE retry = %+v / %v, want allow", decision, err)
	}
	assertStartedEventCount(t, h, enqueued.RunID, beacon.ToolCallId, 1)
}

func TestDeniedBeforeRollsBackWhenDeniedEventCannotPersist(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "denied event atomicity")
	beacon := safeBeforeBeacon(enqueued, "call_denied_atomic")
	beacon.ToolName = "unknown.tool"
	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_denied_event
		BEFORE INSERT ON events
		WHEN NEW.type = 'tool.call.denied'
		BEGIN
			SELECT RAISE(ABORT, 'tool denied event unavailable');
		END;
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.handleToolBeacon(context.Background(), beacon); err == nil {
		t.Fatal("denied BEFORE succeeded despite required tool.call.denied event failure")
	}
	var toolStatus string
	err := h.database.QueryRowContext(context.Background(), `SELECT status FROM tool_calls WHERE id = ?`, beacon.ToolCallId).Scan(&toolStatus)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("denied tool call after required event failure = %q / %v, want no row", toolStatus, err)
	}
	if _, err := h.database.ExecContext(context.Background(), `DROP TRIGGER fail_denied_event`); err != nil {
		t.Fatal(err)
	}
	decision, err := h.service.handleToolBeacon(context.Background(), beacon)
	if err != nil || decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY {
		t.Fatalf("denied BEFORE retry = %+v / %v, want deny", decision, err)
	}
	var eventCount int
	if err := h.database.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM events
		WHERE run_id = ? AND type = 'tool.call.denied' AND payload_json LIKE ?
	`, enqueued.RunID, `%"toolCallId":"`+beacon.ToolCallId+`"%`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("tool.call.denied count = %d, want 1", eventCount)
	}
}

func TestSafeBeforeIgnoresAncillaryAuditFailureAndRetriesIdempotently(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "audit ancillary")
	beacon := safeBeforeBeacon(enqueued, "call_audit_ancillary")
	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_before_audit
		BEFORE INSERT ON audit_logs
		WHEN NEW.action = 'tool.call.before'
		BEGIN
			SELECT RAISE(ABORT, 'tool before audit unavailable');
		END;
	`); err != nil {
		t.Fatal(err)
	}
	decision, err := h.service.handleToolBeacon(context.Background(), beacon)
	if err != nil || decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("BEFORE with audit failure = %+v / %v, want committed allow", decision, err)
	}
	if _, err := h.database.ExecContext(context.Background(), `DROP TRIGGER fail_before_audit`); err != nil {
		t.Fatal(err)
	}
	decision, err = h.service.handleToolBeacon(context.Background(), beacon)
	if err != nil || decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("duplicate BEFORE after audit failure = %+v / %v, want allow", decision, err)
	}
	assertStartedEventCount(t, h, enqueued.RunID, beacon.ToolCallId, 1)
}

func TestDeniedBeforeIgnoresAncillaryAuditFailure(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "denied audit ancillary")
	beacon := safeBeforeBeacon(enqueued, "call_denied_audit")
	beacon.ToolName = "unknown.tool"
	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_denied_audit
		BEFORE INSERT ON audit_logs
		WHEN NEW.action = 'tool.call.before'
		BEGIN
			SELECT RAISE(ABORT, 'denied tool audit unavailable');
		END;
	`); err != nil {
		t.Fatal(err)
	}
	decision, err := h.service.handleToolBeacon(context.Background(), beacon)
	if err != nil || decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY {
		t.Fatalf("denied BEFORE with audit failure = %+v / %v, want committed deny", decision, err)
	}
}

func TestToolAfterIgnoresAncillaryAuditFailure(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "after audit ancillary")
	before := safeBeforeBeacon(enqueued, "call_after_audit")
	if decision, err := h.service.handleToolBeacon(context.Background(), before); err != nil ||
		decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("BEFORE = %+v / %v, want allow", decision, err)
	}
	if _, err := h.database.ExecContext(context.Background(), `
		CREATE TRIGGER fail_after_audit
		BEFORE INSERT ON audit_logs
		WHEN NEW.action = 'tool.call.after'
		BEGIN
			SELECT RAISE(ABORT, 'tool after audit unavailable');
		END;
	`); err != nil {
		t.Fatal(err)
	}
	after := proto.Clone(before).(*turingv1.ToolCallBeacon)
	after.Phase = turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER
	after.Status = turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED
	after.ResultSummary = "done"
	decision, err := h.service.handleToolBeacon(context.Background(), after)
	if err != nil || decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("AFTER with audit failure = %+v / %v, want committed allow", decision, err)
	}
	var status string
	if err := h.database.QueryRowContext(context.Background(), `SELECT status FROM tool_calls WHERE id = ?`, before.ToolCallId).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "completed" {
		t.Fatalf("tool status = %q, want completed", status)
	}
}

func TestSafeBeforeRepairsMissingStartedEventIdempotently(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "legacy started repair")
	beacon := safeBeforeBeacon(enqueued, "call_started_repair")
	argsJSON, argsHash, err := canonicalArgs(beaconArgs(beacon))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.RecordToolCallBeforeNew(context.Background(), repository.ToolCallRecord{
		ToolCallID: beacon.ToolCallId, RunID: beacon.RunId, Status: "allowed",
	}, "general_assistant", beaconServerName(beacon), beacon.ToolName, argsJSON, argsHash); err != nil {
		t.Fatal(err)
	}
	decision, err := h.service.handleToolBeacon(context.Background(), beacon)
	if err != nil || decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("legacy BEFORE repair = %+v / %v, want allow", decision, err)
	}
	assertStartedEventCount(t, h, enqueued.RunID, beacon.ToolCallId, 1)
}

func assertStartedEventCount(t *testing.T, h *harness, runID string, toolCallID string, want int) {
	t.Helper()
	var count int
	if err := h.database.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM events
		WHERE run_id = ? AND type = 'tool.call.started' AND payload_json LIKE ?
	`, runID, `%"toolCallId":"`+toolCallID+`"%`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("tool.call.started count = %d, want %d", count, want)
	}
}
