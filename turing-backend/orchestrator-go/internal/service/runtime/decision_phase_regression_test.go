package runtime

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestSendBeaconDecisionEchoesBeaconPhase(t *testing.T) {
	h := newHarness(t)
	commands := make(chan workerCommand, 1)
	connected := &worker{
		commands:    commands,
		done:        make(chan struct{}),
		assignments: map[string]assignment{},
	}
	beacon := &turingv1.ToolCallBeacon{
		ToolCallId: "call_phase",
		Phase:      turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER,
	}

	if err := h.service.sendBeaconDecision(context.Background(), connected, beacon, &turingv1.ToolPolicyDecision{
		Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
		ToolCallId: beacon.ToolCallId,
	}); err != nil {
		t.Fatal(err)
	}

	command := (<-commands).command
	if phase := command.GetToolPolicyDecision().GetPhase(); phase != beacon.Phase {
		t.Fatalf("decision phase = %s, want %s", phase, beacon.Phase)
	}
}
