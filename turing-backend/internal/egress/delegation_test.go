package egress

import (
	"strings"
	"testing"
)

// The orchestrator's caps on specialist results are sized against 64 KiB, so a
// join's framing has to admit exactly that much rather than the default.
func TestDelegationResultsFramingIsBoundedAt64KiB(t *testing.T) {
	framing := DelegationResultsFraming()
	if framing.MaxBytes != 64*1024 || DelegationResultsFrameMaxBytes != 64*1024 {
		t.Fatalf("MaxBytes = %d, constant = %d, want both %d", framing.MaxBytes, DelegationResultsFrameMaxBytes, 64*1024)
	}
	if err := framing.validate(); err != nil {
		t.Fatalf("framing is invalid: %v", err)
	}
}

// A brief at both argument caps, in four-byte runes, is framed whole: the
// specialist never receives a cut-down task.
func TestADelegationBriefAtItsCapsIsFramedWhole(t *testing.T) {
	task := strings.Repeat("🔬", DelegationBriefMaxTaskBytes/4)
	context := strings.Repeat("📎", DelegationBriefMaxContextBytes/4)
	brief, err := FrameDelegationBrief(task, context)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"BEGIN TURING_RETRIEVED_DELEGATION_BRIEF_", "Task:\n" + task, "Context:\n" + context} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief does not contain %.60q", want)
		}
	}
	if strings.Contains(brief, "[Result truncated") {
		t.Fatal("a brief within its caps was truncated")
	}
}

// A brief with no context names no context section.
func TestABriefWithoutContextHasOnlyItsTask(t *testing.T) {
	brief, err := FrameDelegationBrief("Gather the notes", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(brief, "Task:\nGather the notes\nEND ") || strings.Contains(brief, "Context:") {
		t.Fatalf("brief = %q, want the task alone", brief)
	}
}
