package runtime

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// The measured half of telemetry ends here: whatever the worker reported has
// to land on the run, and whatever it did not report has to stay NULL.

func TestRunCompletedStoresReportedTokenUsage(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "count me")

	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
			RunId:              enqueued.RunID,
			AssistantMessageId: enqueued.AssistantMessageID,
			Content:            "done",
			TokenUsage:         &turingv1.RunTokenUsage{InputTokens: proto64(910), OutputTokens: proto64(42)},
		}}})
	if err != nil {
		t.Fatal(err)
	}

	input, output := readRunTokens(t, h, enqueued.RunID)
	if input != int64(910) || output != int64(42) {
		t.Fatalf("stored tokens = %v / %v, want 910 / 42", input, output)
	}
}

// A worker that reports nothing must leave the columns NULL. This is the path
// every run takes on an Ollama that does not report usage, and a zero written
// here would become a measured-looking "0 tokens" on the client.
func TestRunCompletedLeavesTokensNullWhenNothingWasReported(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "silent")

	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
			RunId:              enqueued.RunID,
			AssistantMessageId: enqueued.AssistantMessageID,
			Content:            "done",
		}}})
	if err != nil {
		t.Fatal(err)
	}

	input, output := readRunTokens(t, h, enqueued.RunID)
	if input != nil || output != nil {
		t.Fatalf("stored tokens = %v / %v, want NULL for a silent provider", input, output)
	}
}

// Half a report is kept as half a report. Filling the missing side in with a
// zero would be the estimate this design exists to refuse.
func TestRunCompletedStoresAPartialTokenReportWithoutCompletingIt(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "partial")

	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
			RunId:              enqueued.RunID,
			AssistantMessageId: enqueued.AssistantMessageID,
			Content:            "done",
			TokenUsage:         &turingv1.RunTokenUsage{OutputTokens: proto64(7)},
		}}})
	if err != nil {
		t.Fatal(err)
	}

	input, output := readRunTokens(t, h, enqueued.RunID)
	if input != nil {
		t.Fatalf("input tokens = %v, want NULL", input)
	}
	if output != int64(7) {
		t.Fatalf("output tokens = %v, want 7", output)
	}
}

// An empty RunTokenUsage message carries no numbers, so it is silence with an
// envelope around it and must be stored as such.
func TestRunCompletedTreatsAnEmptyUsageMessageAsSilence(t *testing.T) {
	h := newHarness(t)
	enqueued := h.createRunningRunResult(t, "empty envelope")

	err := h.service.applyUpdate(context.Background(), &turingv1.RuntimeUpdate{
		Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
			RunId:              enqueued.RunID,
			AssistantMessageId: enqueued.AssistantMessageID,
			Content:            "done",
			TokenUsage:         &turingv1.RunTokenUsage{},
		}}})
	if err != nil {
		t.Fatal(err)
	}

	input, output := readRunTokens(t, h, enqueued.RunID)
	if input != nil || output != nil {
		t.Fatalf("stored tokens = %v / %v, want NULL", input, output)
	}
}

func readRunTokens(t *testing.T, h *harness, runID string) (any, any) {
	t.Helper()
	var input, output any
	if err := h.database.QueryRowContext(context.Background(),
		`SELECT input_tokens, output_tokens FROM agent_runs WHERE id = ?`, runID).Scan(&input, &output); err != nil {
		t.Fatalf("read tokens: %v", err)
	}
	return input, output
}

func proto64(value int64) *int64 { return &value }
