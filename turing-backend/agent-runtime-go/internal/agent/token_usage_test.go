package agent

import (
	"math"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
)

// What a run costs is the sum of its model turns, not the last one. A run that
// calls a tool asks the model twice, and reporting only the final turn would
// understate exactly the runs that cost the most.
func TestExecuteSumsReportedTokensAcrossModelTurns(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{
			{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "system.repeat"}}},
			{Type: "completed", Usage: &llm.TokenUsage{InputTokens: llm.TokenCount(100), OutputTokens: llm.TokenCount(20)}},
		},
		{
			{Type: "delta", Text: "done"},
			{Type: "completed", Usage: &llm.TokenUsage{InputTokens: llm.TokenCount(140), OutputTokens: llm.TokenCount(5)}},
		},
	}}
	assistant := newTokenUsageAssistant(provider)

	usage := completedTokenUsage(t, collectUpdates(t, assistant, testJob()))

	if usage.GetInputTokens() != 240 || usage.GetOutputTokens() != 25 {
		t.Fatalf("token usage = %d in / %d out, want 240 / 25", usage.GetInputTokens(), usage.GetOutputTokens())
	}
}

// The provenance case. Nothing may fill this in, because whatever filled it in
// would be a guess wearing a measurement's clothes.
func TestExecuteReportsNoTokenUsageWhenProviderReportsNone(t *testing.T) {
	provider := &scriptedProvider{events: []llm.StreamEvent{
		{Type: "delta", Text: "hello"},
		{Type: "completed"},
	}}
	assistant := newTokenUsageAssistant(provider)

	updates := collectUpdates(t, assistant, testJob())

	completed := lastRunCompleted(t, updates)
	if completed.GetTokenUsage() != nil {
		t.Fatalf("token usage = %v, want absent for a silent provider", completed.GetTokenUsage())
	}
}

// One turn reporting and another not is the mixed case a proxy produces. The
// counts that exist are kept; the ones that do not are not invented.
func TestExecuteKeepsReportedTurnsWhenAnotherTurnIsSilent(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{
			{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "call_1", Name: "system.repeat"}}},
			{Type: "completed"},
		},
		{
			{Type: "delta", Text: "done"},
			{Type: "completed", Usage: &llm.TokenUsage{OutputTokens: llm.TokenCount(9)}},
		},
	}}
	assistant := newTokenUsageAssistant(provider)

	usage := completedTokenUsage(t, collectUpdates(t, assistant, testJob()))

	if usage.InputTokens != nil {
		t.Fatalf("input tokens = %d, want absent", usage.GetInputTokens())
	}
	if usage.GetOutputTokens() != 9 {
		t.Fatalf("output tokens = %d, want 9", usage.GetOutputTokens())
	}
}

func TestRunTokenAccumulatorReportsNothingUntilAProviderDoes(t *testing.T) {
	var accumulator runTokenAccumulator
	if accumulator.reported() != nil {
		t.Fatal("a fresh accumulator reported usage")
	}
	accumulator.add(nil)
	if accumulator.reported() != nil {
		t.Fatal("a nil report became a usage report")
	}
}

// A zero the provider actually stated is a measurement, and must survive as
// one. Collapsing it to "unreported" would erase a real observation.
func TestRunTokenAccumulatorKeepsAReportedZero(t *testing.T) {
	var accumulator runTokenAccumulator
	accumulator.add(&llm.TokenUsage{InputTokens: llm.TokenCount(0), OutputTokens: llm.TokenCount(0)})

	usage := accumulator.reported()
	if usage == nil || usage.InputTokens == nil || usage.OutputTokens == nil {
		t.Fatalf("usage = %v, want a reported zero", usage)
	}
	if usage.GetInputTokens() != 0 || usage.GetOutputTokens() != 0 {
		t.Fatalf("usage = %d / %d, want 0 / 0", usage.GetInputTokens(), usage.GetOutputTokens())
	}
}

// A sum that would wrap reports nothing rather than a negative token count.
func TestRunTokenAccumulatorDiscardsAnOverflowingSum(t *testing.T) {
	var accumulator runTokenAccumulator
	accumulator.add(&llm.TokenUsage{InputTokens: llm.TokenCount(math.MaxInt64 - 1), OutputTokens: llm.TokenCount(1)})
	accumulator.add(&llm.TokenUsage{InputTokens: llm.TokenCount(2), OutputTokens: llm.TokenCount(1)})

	if usage := accumulator.reported(); usage != nil {
		t.Fatalf("usage = %v, want nothing after an overflow", usage)
	}
}

func newTokenUsageAssistant(provider llm.Provider) *GeneralAssistant {
	return NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{
			SystemMCP: &assistantTestToolLister{
				definitions: []map[string]any{{"name": "system.repeat"}},
				result:      map[string]any{"ok": true},
			},
			Runner: &tools.Runner{PostBeacon: allowToolCall},
		},
	)
}

func completedTokenUsage(t *testing.T, updates []*turingv1.RuntimeUpdate) *turingv1.RunTokenUsage {
	t.Helper()
	usage := lastRunCompleted(t, updates).GetTokenUsage()
	if usage == nil {
		t.Fatal("run completed without token usage")
	}
	return usage
}

func lastRunCompleted(t *testing.T, updates []*turingv1.RuntimeUpdate) *turingv1.RuntimeRunCompleted {
	t.Helper()
	for index := len(updates) - 1; index >= 0; index-- {
		if completed := updates[index].GetRunCompleted(); completed != nil {
			return completed
		}
	}
	t.Fatalf("no run_completed update in %d updates", len(updates))
	return nil
}
