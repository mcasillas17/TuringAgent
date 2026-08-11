package runtime

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
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
