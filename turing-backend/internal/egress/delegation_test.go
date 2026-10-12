package egress

import (
	"math"
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

// The budget is the most each of count results may keep for all of them to
// fit one join frame, and DelegationResultsFit is exactly that bound, with no
// arithmetic that a huge setting could overflow into a pass.
func TestDelegationResultsFitIsTheBudgetAndCannotOverflow(t *testing.T) {
	for count := 1; count <= 70; count++ {
		budget := DelegationResultBudget(count)
		fits := count*(budget+DelegationResultHeaderBytes)+DelegationResultsIntroBytes <= DelegationResultsFrameMaxBytes
		over := count*(budget+1+DelegationResultHeaderBytes)+DelegationResultsIntroBytes > DelegationResultsFrameMaxBytes
		if budget >= 1 && (!fits || !over) {
			t.Fatalf("budget(%d) = %d is not the largest that fits", count, budget)
		}
		if DelegationResultsFit(count, budget) != (budget >= 1) || DelegationResultsFit(count, budget+1) {
			t.Fatalf("fit(%d, %d) disagrees with the budget", count, budget)
		}
	}
	for _, test := range []struct{ count, cap int }{
		{3, math.MaxInt}, {math.MaxInt, 1}, {math.MaxInt, math.MaxInt}, {0, 8192}, {3, 0}, {-1, 8192},
	} {
		if DelegationResultsFit(test.count, test.cap) {
			t.Fatalf("DelegationResultsFit(%d, %d) = true, want false", test.count, test.cap)
		}
	}
	if DelegationResultBudget(0) > 0 || DelegationResultBudget(math.MaxInt) > 0 {
		t.Fatal("a budget for no results, or for more than can fit, must be empty")
	}
}
