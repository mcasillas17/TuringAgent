package runtime

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// enforceSelection stamps a queued or running job the way a delegated
// specialist's job is written: its tools are frozen and enforced.
func enforceSelection(t *testing.T, h *harness, jobID string, selected string) {
	t.Helper()
	if _, err := h.database.ExecContext(context.Background(), `
		UPDATE jobs SET payload_json = json_set(payload_json,
			'$.enforceSelectedTools', json('true'), '$.selectedTools', json(?))
		WHERE id = ?`, selected, jobID); err != nil {
		t.Fatal(err)
	}
}

func selectionBeacon(t *testing.T, run repository.EnqueueUserMessageResult, callID, server, tool string, args map[string]any) *turingv1.ToolCallBeacon {
	t.Helper()
	structArgs, err := structpb.NewStruct(args)
	if err != nil {
		t.Fatal(err)
	}
	return &turingv1.ToolCallBeacon{
		RunId: run.RunID, TraceId: run.TraceID, ToolCallId: callID,
		AgentId:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		ServerName: server, ToolName: tool,
		Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: structArgs,
	}
}

// Safe built-in reads take their decision from the orchestrator too, so an
// enforced job cannot reach one the runtime should never have offered.
func TestABeaconOutsideTheEnforcedSetIsDeniedEvenWhenSafe(t *testing.T) {
	for _, tool := range []struct{ server, name string }{
		{"files", "files.read"}, {"system", "system.time"}, {"skills", "skills_list"}, {"skills", "skill_view"},
	} {
		t.Run(tool.name, func(t *testing.T) {
			h := newHarness(t)
			run := h.createRunningRunResult(t, "specialist")
			enforceSelection(t, h, run.JobID, `["memory/memory.search"]`)

			decision, err := h.service.handleToolBeacon(context.Background(),
				selectionBeacon(t, run, "call_out", tool.server, tool.name, map[string]any{"path": "notes.md"}))
			if err != nil {
				t.Fatal(err)
			}
			if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY || decision.GetReason() != "tool_not_selected" {
				t.Fatalf("decision = %v %q, want deny tool_not_selected", decision.GetDecision(), decision.GetReason())
			}
		})
	}
}

// The selection is checked before the policy, so an out-of-set tool that would
// need approval never creates one.
func TestAnOutOfSetToolNeedingApprovalCreatesNoApproval(t *testing.T) {
	h := newHarness(t)
	run := h.createRunningRunResult(t, "specialist")
	enforceSelection(t, h, run.JobID, `[]`)

	decision, err := h.service.handleToolBeacon(context.Background(),
		selectionBeacon(t, run, "call_update", "files", "files.update", map[string]any{"path": "notes.md", "content": "x"}))
	if err != nil {
		t.Fatal(err)
	}
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY || decision.GetReason() != "tool_not_selected" {
		t.Fatalf("decision = %v %q, want deny tool_not_selected", decision.GetDecision(), decision.GetReason())
	}
	if _, err := h.repo.GetApprovalByToolCall(context.Background(), run.RunID, "call_update"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("approval lookup = %v, want none created", err)
	}
}

func TestABeaconInsideTheEnforcedSetKeepsItsPolicyDecision(t *testing.T) {
	h := newHarness(t)
	run := h.createRunningRunResult(t, "specialist")
	enforceSelection(t, h, run.JobID, `["files/files.read","system/system.time"]`)

	for _, beacon := range []*turingv1.ToolCallBeacon{
		selectionBeacon(t, run, "call_read", "files", "files.read", map[string]any{"path": "notes.md"}),
		selectionBeacon(t, run, "call_time", "system", "system.time", map[string]any{}),
	} {
		decision, err := h.service.handleToolBeacon(context.Background(), beacon)
		if err != nil {
			t.Fatal(err)
		}
		if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
			t.Fatalf("%s decision = %v %q, want allow", beacon.GetToolName(), decision.GetDecision(), decision.GetReason())
		}
	}
}

// A job that does not enforce its set — every turn today — is decided by
// policy alone, exactly as before.
func TestAnUnenforcedJobKeepsTodaysDecision(t *testing.T) {
	h := newHarness(t)
	run := h.createRunningRunResult(t, "ordinary turn")

	decision, err := h.service.handleToolBeacon(context.Background(),
		selectionBeacon(t, run, "call_time", "system", "system.time", map[string]any{}))
	if err != nil {
		t.Fatal(err)
	}
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_ALLOW {
		t.Fatalf("decision = %v %q, want allow", decision.GetDecision(), decision.GetReason())
	}
}

// The selection is checked ahead of the worker's own toolset, so an
// out-of-set call is reported as not selected even from a worker that does not
// serve the tool; an in-set tool the worker lacks is still unknown_tool.
func TestTheSelectionIsCheckedBeforeTheWorkersToolset(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner := registerWorkerCapabilities(t, h, "worker-selection", "registration-selection", teamCapabilities(1, "common"))
	run := h.createRunningRunResult(t, "specialist")
	enforceSelection(t, h, run.JobID, `["files/files.update"]`)
	record, err := h.repo.GetRun(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct{ tool, want string }{
		{"files.read", "tool_not_selected"},
		{"files.update", "unknown_tool"},
	} {
		decision, err := h.service.handleToolBefore(ctx,
			selectionBeacon(t, run, "call_"+test.tool, "files", test.tool, map[string]any{"path": "notes.md", "content": "x"}),
			record, "worker-selection", owner)
		if err != nil {
			t.Fatal(err)
		}
		if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY || decision.GetReason() != test.want {
			t.Fatalf("%s decision = %v %q, want deny %s", test.tool, decision.GetDecision(), decision.GetReason(), test.want)
		}
	}
}
