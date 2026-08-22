package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/memory"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
)

type budgetCapturingProvider struct {
	window    int
	output    int
	responses [][]llm.StreamEvent
	requests  []llm.ChatRequest
	estimates []int
	onRequest func(int)
}

func (r *admissionAwareRecaller) PrepareRecall(
	_ context.Context,
	sessionID string,
	userText string,
) func(context.Context, []llm.ChatMessage) (llm.ChatMessage, bool) {
	return func(ctx context.Context, inContext []llm.ChatMessage) (llm.ChatMessage, bool) {
		return r.Recall(ctx, sessionID, userText, inContext)
	}
}

type admissionAwareRecaller struct {
	target string
	calls  int
}

func (r *oscillatingRecaller) PrepareRecall(
	_ context.Context,
	sessionID string,
	userText string,
) func(context.Context, []llm.ChatMessage) (llm.ChatMessage, bool) {
	return func(ctx context.Context, inContext []llm.ChatMessage) (llm.ChatMessage, bool) {
		return r.Recall(ctx, sessionID, userText, inContext)
	}
}

type oscillatingRecaller struct {
	calls int
}

type preparedCountingRecaller struct {
	prepareCalls int
	directCalls  int
	rankCalls    int
}

type cancelingPreparedRecaller struct {
	cancel context.CancelFunc
}

func (r cancelingPreparedRecaller) PrepareRecall(
	context.Context,
	string,
	string,
) func(context.Context, []llm.ChatMessage) (llm.ChatMessage, bool) {
	return func(context.Context, []llm.ChatMessage) (llm.ChatMessage, bool) {
		r.cancel()
		return llm.ChatMessage{}, false
	}
}

type staticRecallSearcher struct {
	excerpts []memory.Excerpt
}

func (s staticRecallSearcher) SearchMessages(
	context.Context,
	string,
	string,
	string,
	int,
) ([]memory.Excerpt, error) {
	return s.excerpts, nil
}

func (r *preparedCountingRecaller) Recall(
	_ context.Context,
	_ string,
	_ string,
	_ []llm.ChatMessage,
) (llm.ChatMessage, bool) {
	r.directCalls++
	return llm.ChatMessage{Role: "system", Content: "direct recall"}, true
}

func (r *preparedCountingRecaller) PrepareRecall(
	_ context.Context,
	_ string,
	_ string,
) func(context.Context, []llm.ChatMessage) (llm.ChatMessage, bool) {
	r.prepareCalls++
	return func(_ context.Context, _ []llm.ChatMessage) (llm.ChatMessage, bool) {
		r.rankCalls++
		return llm.ChatMessage{Role: "system", Content: "prepared recall"}, true
	}
}

func (r *oscillatingRecaller) Recall(
	_ context.Context,
	_ string,
	_ string,
	_ []llm.ChatMessage,
) (llm.ChatMessage, bool) {
	r.calls++
	content := "small recall"
	if r.calls%2 == 1 {
		content = strings.Repeat("large recall ", 30)
	}
	return llm.ChatMessage{Role: "system", Content: content}, true
}

func (r *admissionAwareRecaller) Recall(
	_ context.Context,
	_ string,
	_ string,
	inContext []llm.ChatMessage,
) (llm.ChatMessage, bool) {
	r.calls++
	for _, message := range inContext {
		if (message.Role == "user" || message.Role == "assistant") &&
			strings.Contains(message.Content, r.target) {
			return llm.ChatMessage{}, false
		}
	}
	return llm.ChatMessage{
		Role:    "system",
		Content: "recalled omitted current-session material: " + r.target,
	}, true
}

func (p *budgetCapturingProvider) ID() string { return "budget-capturing" }

func (p *budgetCapturingProvider) ContextWindowTokens() int { return p.window }

func (p *budgetCapturingProvider) MaxOutputTokens() int { return p.output }

func (p *budgetCapturingProvider) EstimateRequestTokens(req llm.ChatRequest) (int, error) {
	body, err := json.Marshal(req)
	return len(body), err
}

func (p *budgetCapturingProvider) StreamChat(_ context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	if p.onRequest != nil {
		p.onRequest(len(p.requests))
	}
	p.requests = append(p.requests, req)
	estimate, err := llm.EstimateRequestTokens(p, req)
	if err != nil {
		return nil, err
	}
	p.estimates = append(p.estimates, estimate)
	index := len(p.requests) - 1
	events := []llm.StreamEvent{{Type: "completed", FinishReason: "stop"}}
	if index < len(p.responses) {
		// Terminated the way the wire protocol terminates every turn, for the
		// same reason scriptedProvider does it: a fixture that describes an
		// ordinary tool turn must not accidentally describe a cut-off stream,
		// which the agent now refuses to execute tools from.
		events = withTerminalEvent(p.responses[index])
	}
	out := make(chan llm.StreamEvent, len(events))
	for _, event := range events {
		out <- event
	}
	close(out)
	return out, nil
}

func TestExecuteLongSessionStaysWithinBudgetAndPreservesRecall(t *testing.T) {
	provider := &budgetCapturingProvider{window: 900}
	recaller := &fakeRecaller{
		block: llm.ChatMessage{Role: "system", Content: "recalled material that must survive before old history"},
		ok:    true,
	}

	var history []llm.ChatMessage
	for index := 0; index < 8; index++ {
		history = append(history,
			llm.ChatMessage{Role: "user", Content: strings.Repeat("old question ", 20)},
			llm.ChatMessage{Role: "assistant", Content: strings.Repeat("old answer ", 20)},
		)
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{messages: history},
		&GeneralAssistantTools{Recall: recaller},
	)

	updates := collectUpdates(t, assistant, testJob())

	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	if provider.estimates[0] > provider.window {
		t.Fatalf("request estimate = %d, configured window = %d", provider.estimates[0], provider.window)
	}
	if !containsMessageContent(provider.requests[0].Messages, "recalled material") {
		t.Fatalf("recall did not reach provider: %#v", provider.requests[0].Messages)
	}
	if notes := runStepNotes(updates); !containsString(notes, recallNotice) {
		t.Fatalf("run notes = %q, want recall attribution", notes)
	}
}

func TestExecuteBoundsInjectedSkillIndexToProviderContext(t *testing.T) {
	provider := &budgetCapturingProvider{window: 4096, output: 512}
	job := testJob()
	for index := 0; index < 1000; index++ {
		job.Skills = append(job.Skills, &turingv1.SkillSnapshot{
			SkillId:     fmt.Sprintf("category/skill-%04d", index),
			Name:        fmt.Sprintf("Skill %04d", index),
			Category:    "category",
			Description: strings.Repeat("bounded description ", 20),
		})
	}
	files := &assistantTestToolLister{definitions: []map[string]any{{
		"name":        "files.large",
		"description": strings.Repeat("large file tool schema ", 100),
	}}}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{FilesMCP: files},
	)

	updates := collectUpdates(t, assistant, job)

	if failure := findRunFailed(updates); failure != nil {
		t.Fatalf("bounded skill index failed the run: %#v", failure)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	if provider.estimates[0]+provider.output > provider.window {
		t.Fatalf(
			"request estimate %d + output reserve %d exceeds context window %d",
			provider.estimates[0],
			provider.output,
			provider.window,
		)
	}
	if !containsMessageContent(provider.requests[0].Messages, "skills omitted") ||
		!containsMessageContent(provider.requests[0].Messages, "skills_list") {
		t.Fatalf("bounded skill index did not disclose truncation: %#v", provider.requests[0].Messages)
	}
	if !containsToolDefinition(provider.requests[0].Tools, "skills_list") ||
		!containsToolDefinition(provider.requests[0].Tools, "skill_view") {
		t.Fatalf("bounded skill index lost its access tools: %#v", provider.requests[0].Tools)
	}
	for _, update := range updates {
		event := update.GetEvent()
		if event == nil || event.Type != turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP {
			continue
		}
		payload := event.GetPayload().AsMap()
		if payload["reason"] == "context_budget" && payload["skillIndexOmitted"] == true {
			return
		}
	}
	t.Fatalf("updates did not durably disclose the partial enabled skill index: %#v", updates)
}

func TestExecuteDisclosesOmittedEnabledSkillIndex(t *testing.T) {
	provider := &budgetCapturingProvider{window: 1200, output: 100}
	job := testJob()
	job.Skills = []*turingv1.SkillSnapshot{{
		SkillId:     "writing/tone",
		Name:        "Tone",
		Category:    "writing",
		Description: "Keep responses concise.",
	}}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{},
	)

	updates := collectUpdates(t, assistant, job)

	if failure := findRunFailed(updates); failure != nil {
		t.Fatalf("run failed instead of omitting the skill index: %#v", failure)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	if containsMessageContent(provider.requests[0].Messages, "The user enabled the following local skills") {
		t.Fatalf("skill index unexpectedly fit the constrained request: %#v", provider.requests[0].Messages)
	}
	if containsToolDefinition(provider.requests[0].Tools, skillsListToolName) ||
		containsToolDefinition(provider.requests[0].Tools, skillViewToolName) {
		t.Fatalf("skill access tools reached a request without their index: %#v", provider.requests[0].Tools)
	}
	for _, update := range updates {
		event := update.GetEvent()
		if event == nil || event.Type != turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP {
			continue
		}
		payload := event.GetPayload().AsMap()
		if payload["reason"] == "context_budget" && payload["skillIndexOmitted"] == true {
			if note, _ := payload["note"].(string); !strings.Contains(note, "enabled skill metadata") {
				t.Fatalf("context omission note = %q, want enabled skill metadata", note)
			}
			return
		}
	}
	t.Fatalf("updates did not disclose the omitted enabled skill index: %#v", updates)
}

func TestExecuteDoesNotReportSkillIndexOmissionForLegacyOnlySkills(t *testing.T) {
	provider := &budgetCapturingProvider{window: 4096, output: 100}
	job := testJob()
	job.Skills = []*turingv1.SkillSnapshot{{
		Name:         "Legacy tone",
		Instructions: "Keep responses concise.",
	}}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{},
	)

	updates := collectUpdates(t, assistant, job)

	if len(provider.requests) != 1 ||
		!containsMessageContent(provider.requests[0].Messages, "Keep responses concise.") {
		t.Fatalf("legacy skill did not reach the provider: %#v", provider.requests)
	}
	for _, update := range updates {
		event := update.GetEvent()
		if event == nil || event.Type != turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP {
			continue
		}
		if event.GetPayload().AsMap()["skillIndexOmitted"] == true {
			t.Fatalf("legacy-only skills produced a false index omission: %#v", update)
		}
	}
}

func TestBuildBudgetedContextWithRecallDoesNotReturnOmittedRecall(t *testing.T) {
	provider := &budgetCapturingProvider{window: 700, output: 100}
	assistant := NewGeneralAssistant(nil, nil, nil)

	budgeted, recallMessage, err := assistant.buildBudgetedContextWithRecall(
		context.Background(),
		provider,
		testJob(),
		nil,
		false,
		nil,
		nil,
		nil,
		[]llm.ChatMessage{{Role: "user", Content: "current question"}},
		nil,
		func(context.Context, []llm.ChatMessage) (llm.ChatMessage, bool) {
			return llm.ChatMessage{
				Role:    "system",
				Content: strings.Repeat("oversized recall ", 100),
			}, true
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if budgeted.RecallUsed {
		t.Fatal("oversized recall unexpectedly reached the budgeted request")
	}
	if recallMessage != nil {
		t.Fatalf("omitted recall returned for later preflight: %#v", recallMessage)
	}
}

func TestExecuteRebudgetsSkillIndexBeforeToolPreflight(t *testing.T) {
	provider := &budgetCapturingProvider{
		window: 4096,
		output: 512,
		responses: [][]llm.StreamEvent{
			{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
				ID: "call_1", Name: "system.read", Arguments: map[string]any{},
			}}}},
			{{Type: "completed", FinishReason: "stop"}},
		},
	}
	job := testJob()
	for index := 0; index < 100; index++ {
		job.Skills = append(job.Skills, &turingv1.SkillSnapshot{
			SkillId:     fmt.Sprintf("category/skill-%03d", index),
			Name:        fmt.Sprintf("Skill %03d", index),
			Category:    "category",
			Description: strings.Repeat("bounded description ", 10),
		})
	}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{
			"name":        "system.read",
			"description": strings.Repeat("large system tool schema ", 80),
		}},
		result: map[string]any{"ok": true},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{
			SystemMCP: client,
			Runner:    &tools.Runner{PostBeacon: allowToolCall},
		},
	)

	updates := collectUpdates(t, assistant, job)

	if failure := findRunFailed(updates); failure != nil {
		t.Fatalf("run failed instead of re-budgeting the skill index: %#v", failure)
	}
	if len(client.calls) != 1 || len(provider.requests) != 2 {
		t.Fatalf("tool calls/provider requests = %d/%d, want 1/2", len(client.calls), len(provider.requests))
	}
	if !containsMessageContent(provider.requests[0].Messages, "The user enabled the following local skills") {
		t.Fatal("initial request did not include the skill index")
	}
	if provider.estimates[1]+provider.output > provider.window {
		t.Fatalf(
			"second request estimate %d + output reserve %d exceeds window %d",
			provider.estimates[1],
			provider.output,
			provider.window,
		)
	}
}

func TestExecuteRecallsCurrentSessionHistoryOmittedByBudget(t *testing.T) {
	provider := &budgetCapturingProvider{window: 900}
	target := "important current-session fact to preserve"
	recaller := &admissionAwareRecaller{target: target}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{messages: []llm.ChatMessage{
			{Role: "user", Content: target},
			{Role: "assistant", Content: strings.Repeat("large old answer ", 60)},
			{Role: "user", Content: "recent question"},
			{Role: "assistant", Content: "recent answer"},
		}},
		&GeneralAssistantTools{Recall: recaller},
	)

	updates := collectUpdates(t, assistant, testJob())

	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	if !containsMessageContent(provider.requests[0].Messages, target) {
		t.Fatalf("budget omitted target from both history and recall: %#v", provider.requests[0].Messages)
	}
	if recaller.calls == 0 {
		t.Fatal("recall was not evaluated against admitted history")
	}
	if !containsString(runStepNotes(updates), recallNotice) {
		t.Fatalf("run notes = %q, want recall attribution", runStepNotes(updates))
	}
}

func TestExecuteCurrentTurnIDDoesNotSuppressPagedOlderDuplicate(t *testing.T) {
	job := testJob()
	job.UserText = "repeat deployment detail"
	provider := &budgetCapturingProvider{window: 2048}
	recaller := memory.NewRecaller(staticRecallSearcher{excerpts: []memory.Excerpt{{
		MessageID: "msg_older_duplicate",
		SessionID: job.GetSessionId(),
		Role:      "user",
		Content:   job.GetUserText(),
		CreatedAt: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	}}})
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{Recall: recaller},
	)

	updates := collectUpdates(t, assistant, job)

	if len(provider.requests) != 1 ||
		!containsMessageContent(provider.requests[0].Messages, "EARLIER conversations") {
		t.Fatalf("older duplicate was suppressed by current turn: %#v", provider.requests)
	}
	if !containsString(runStepNotes(updates), recallNotice) {
		t.Fatalf("run notes = %q, want recall attribution", runStepNotes(updates))
	}
}

func TestExecuteBoundsOscillatingRecallConvergence(t *testing.T) {
	provider := &budgetCapturingProvider{window: 900}
	recaller := &oscillatingRecaller{}
	var history []llm.ChatMessage
	for index := range 25 {
		history = append(history,
			llm.ChatMessage{Role: "user", Content: fmt.Sprintf("question %02d %s", index, strings.Repeat("q", 20))},
			llm.ChatMessage{Role: "assistant", Content: fmt.Sprintf("answer %02d %s", index, strings.Repeat("a", 20))},
		)
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{messages: history},
		&GeneralAssistantTools{Recall: recaller},
	)

	updates := collectUpdates(t, assistant, testJob())

	if recaller.calls > 4 {
		t.Fatalf("recall calls = %d, want at most 3 convergence passes plus fallback", recaller.calls)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want eventual model dispatch", len(provider.requests))
	}
	if failure := findRunFailed(updates); failure != nil {
		t.Fatalf("oscillating recall failed the run: %#v", failure)
	}
}

func TestExecutePropagatesCancellationDuringRecallConvergence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := &budgetCapturingProvider{window: 2048}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{Recall: cancelingPreparedRecaller{cancel: cancel}},
	)

	var updates []*turingv1.RuntimeUpdate
	err := assistant.Execute(ctx, testJob(), func(update *turingv1.RuntimeUpdate) error {
		updates = append(updates, update)
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context.Canceled", err)
	}
	if failure := findRunFailed(updates); failure != nil {
		t.Fatalf("cancellation emitted run failure: %#v", failure)
	}
	if len(provider.requests) != 0 {
		t.Fatalf("provider requests = %d, want none after cancellation", len(provider.requests))
	}
}

func TestExecutePreparesRecallSearchOnceAcrossToolIterations(t *testing.T) {
	provider := &budgetCapturingProvider{
		window: 2048,
		responses: [][]llm.StreamEvent{
			{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
				ID: "call_1", Name: "system.read", Arguments: map[string]any{},
			}}}},
			{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
				ID: "call_2", Name: "system.read", Arguments: map[string]any{},
			}}}},
			{{Type: "delta", Text: "done"}, {Type: "completed", FinishReason: "stop"}},
		},
	}
	recaller := &preparedCountingRecaller{}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.read"}},
		result:      map[string]any{"ok": true},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{
			Recall:    recaller,
			SystemMCP: client,
			Runner:    &tools.Runner{PostBeacon: allowToolCall},
		},
	)

	updates := collectUpdates(t, assistant, testJob())

	if len(provider.requests) != 3 {
		t.Fatalf("provider requests = %d, want 3", len(provider.requests))
	}
	if recaller.prepareCalls != 1 || recaller.directCalls != 0 {
		t.Fatalf("prepare/direct recall calls = %d/%d, want 1/0", recaller.prepareCalls, recaller.directCalls)
	}
	if recaller.rankCalls < 3 {
		t.Fatalf("rank calls = %d, want context-dependent ranking for each dispatch", recaller.rankCalls)
	}
	if failure := findRunFailed(updates); failure != nil {
		t.Fatalf("prepared recall run failed: %#v", failure)
	}
}

func TestExecuteNoticesWhenRecallCannotFitContextWindow(t *testing.T) {
	job := testJob()
	provider := &budgetCapturingProvider{}
	live := []llm.ChatMessage{{Role: "user", Content: job.GetUserText()}}
	provider.window = estimateRequest(t, provider, job.GetModel(), live, nil)
	recaller := &fakeRecaller{
		block: llm.ChatMessage{Role: "system", Content: strings.Repeat("recalled material ", 30)},
		ok:    true,
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{Recall: recaller},
	)

	updates := collectUpdates(t, assistant, job)

	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want 1", len(provider.requests))
	}
	if containsMessageContent(provider.requests[0].Messages, "recalled material") {
		t.Fatalf("recall reached constrained provider: %#v", provider.requests[0].Messages)
	}
	notes := runStepNotes(updates)
	if containsString(notes, recallNotice) {
		t.Fatalf("run notes = %q, claimed omitted recall was used", notes)
	}
	if !containsStringFragment(notes, "recalled material") {
		t.Fatalf("run notes = %q, want durable recall-omission notice", notes)
	}
}

func TestExecuteRebudgetsAfterToolResultWithoutSplittingProtocol(t *testing.T) {
	provider := &budgetCapturingProvider{
		window: 1200,
		responses: [][]llm.StreamEvent{
			{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
				ID: "call_1", Name: "system.read", Arguments: map[string]any{},
			}}}},
			{{Type: "delta", Text: "done"}, {Type: "completed", FinishReason: "stop"}},
		},
	}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{
			"name":        "system.read",
			"inputSchema": map[string]any{"type": "object"},
		}},
		result: map[string]any{"content": strings.Repeat("result ", 70)},
	}
	history := []llm.ChatMessage{
		{Role: "user", Content: strings.Repeat("old question ", 25)},
		{Role: "assistant", Content: strings.Repeat("old answer ", 25)},
		{Role: "user", Content: "recent question"},
		{Role: "assistant", Content: "recent answer"},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{messages: history},
		&GeneralAssistantTools{
			SystemMCP: client,
			Runner:    &tools.Runner{PostBeacon: allowToolCall},
		},
	)

	updates := collectUpdates(t, assistant, testJob())

	if len(provider.requests) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(provider.requests))
	}
	for index, estimate := range provider.estimates {
		if estimate > provider.window {
			t.Fatalf("request %d estimate = %d, window = %d", index, estimate, provider.window)
		}
	}
	second := provider.requests[1].Messages
	if len(second) < 3 {
		t.Fatalf("second request messages = %#v, want user/call/result protocol", second)
	}
	call := second[len(second)-2]
	result := second[len(second)-1]
	if call.Role != "assistant" || len(call.ToolCalls) != 1 || call.ToolCalls[0].ID != "call_1" {
		t.Fatalf("assistant tool call was split or changed: %#v", call)
	}
	if result.Role != "tool" || result.ToolCallID != "call_1" {
		t.Fatalf("tool result was split or changed: %#v", result)
	}
	if !containsStringFragment(runStepNotes(updates), "older conversation") {
		t.Fatalf("run notes = %q, want history omission notice", runStepNotes(updates))
	}
}

func TestExecuteCompactsOversizedToolResultWithoutSplittingProtocol(t *testing.T) {
	provider := &budgetCapturingProvider{
		window: 700,
		responses: [][]llm.StreamEvent{
			{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
				ID: "call_1", Name: "system.read", Arguments: map[string]any{},
			}}}},
		},
	}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.read"}},
		result:      map[string]any{"content": strings.Repeat("oversized result ", 100)},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{
			SystemMCP: client,
			Runner:    &tools.Runner{PostBeacon: allowToolCall},
		},
	)

	updates := collectUpdates(t, assistant, testJob())

	if len(provider.requests) != 2 {
		t.Fatalf("provider requests = %d, want compacted second dispatch", len(provider.requests))
	}
	second := provider.requests[1].Messages
	call := second[len(second)-2]
	result := second[len(second)-1]
	if call.Role != "assistant" || len(call.ToolCalls) != 1 || call.ToolCalls[0].ID != "call_1" {
		t.Fatalf("tool call protocol changed: %#v", call)
	}
	if result.Role != "tool" || result.ToolCallID != "call_1" {
		t.Fatalf("tool result protocol changed: %#v", result)
	}
	if strings.Contains(result.Content, "oversized result oversized result") ||
		!strings.Contains(result.Content, "omitted") {
		t.Fatalf("tool result was not replaced by an explicit omission marker: %q", result.Content)
	}
	if !containsStringFragment(runStepNotes(updates), "tool result") {
		t.Fatalf("run notes = %q, want tool-result omission notice", runStepNotes(updates))
	}
}

func TestExecuteRejectsUnfitToolProtocolBeforeSideEffect(t *testing.T) {
	provider := &budgetCapturingProvider{
		output: 100,
		responses: [][]llm.StreamEvent{
			{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
				ID: "call_1", Name: "files.create", Arguments: map[string]any{"path": "note.txt"},
			}}}},
		},
	}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "files.create"}},
		result:      map[string]any{"ok": true},
	}
	emptyMarkerMessages := []llm.ChatMessage{
		{Role: "user", Content: testJob().GetUserText()},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "call_1", Name: "files.create", Arguments: map[string]any{"path": "note.txt"},
		}}},
		{
			Role:       "tool",
			Name:       "files.create",
			ToolCallID: "call_1",
			Content:    compactedToolResultForBytes(0),
		},
	}
	toolDefinitions := []llm.ToolDefinition{{
		Name:       "files.create",
		Parameters: map[string]any{"type": "object"},
	}}
	provider.window = estimateRequest(
		t,
		provider,
		testJob().GetModel(),
		emptyMarkerMessages,
		toolDefinitions,
	) + provider.output
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{
			FilesMCP: client,
			Runner: &tools.Runner{
				PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
					return approvalToolCall(beacon), nil
				},
				WaitApproval:   func(context.Context, string) (string, error) { return "token", nil },
				ResumeApproved: allowAgentResume,
			},
		},
	)

	updates := collectUpdates(t, assistant, testJob())

	if len(client.calls) != 0 {
		t.Fatalf("side-effecting tool calls = %d, want none before protocol fits", len(client.calls))
	}
	failure := findRunFailed(updates)
	if failure == nil || failure.GetCode() != "context_budget_exceeded" {
		t.Fatalf("failure = %#v, want context_budget_exceeded", failure)
	}
}

func TestExecuteCompletesAfterEscapeHeavyResultAtPreflightBoundary(t *testing.T) {
	result := make(map[string]any, 12)
	for index := range 12 {
		result[fmt.Sprintf("k%d", index)] = "v"
	}
	provider := &budgetCapturingProvider{
		output: 100,
		responses: [][]llm.StreamEvent{
			{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
				ID: "call_1", Name: "files.create", Arguments: map[string]any{"path": "note.txt"},
			}}}},
			{{Type: "delta", Text: "done"}, {Type: "completed", FinishReason: "stop"}},
		},
	}
	toolDefinitions := []llm.ToolDefinition{{
		Name:       "files.create",
		Parameters: map[string]any{"type": "object"},
	}}
	prospectiveMessages := []llm.ChatMessage{
		{Role: "user", Content: testJob().GetUserText()},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "call_1", Name: "files.create", Arguments: map[string]any{"path": "note.txt"},
		}}},
		{
			Role:       "tool",
			Name:       "files.create",
			ToolCallID: "call_1",
			Content:    compactedToolResultForBytes(maxToolResultBytes),
		},
	}
	provider.window = estimateRequest(
		t,
		provider,
		testJob().GetModel(),
		prospectiveMessages,
		toolDefinitions,
	) + provider.output
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "files.create"}},
		result:      result,
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{
			FilesMCP: client,
			Runner: &tools.Runner{
				PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
					return approvalToolCall(beacon), nil
				},
				WaitApproval:   func(context.Context, string) (string, error) { return "token", nil },
				ResumeApproved: allowAgentResume,
			},
		},
	)

	updates := collectUpdates(t, assistant, testJob())

	if len(client.calls) != 1 || len(provider.requests) != 2 {
		t.Fatalf("tool calls/provider requests = %d/%d, want 1/2", len(client.calls), len(provider.requests))
	}
	if failure := findRunFailed(updates); failure != nil {
		t.Fatalf("run failed after permitted side effect: %#v", failure)
	}
}

func TestExecuteEmitsEachChangedOmissionSetBeforeItsDispatch(t *testing.T) {
	provider := &budgetCapturingProvider{
		window: 950,
		output: 100,
		responses: [][]llm.StreamEvent{
			{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
				ID: "call_1", Name: "system.read", Arguments: map[string]any{},
			}}}},
			{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
				ID: "call_2", Name: "system.read", Arguments: map[string]any{},
			}}}},
			{{Type: "delta", Text: "done"}, {Type: "completed", FinishReason: "stop"}},
		},
	}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.read"}},
		result:      map[string]any{"content": strings.Repeat("tool result ", 28)},
	}
	history := []llm.ChatMessage{
		{Role: "user", Content: strings.Repeat("oldest question ", 12)},
		{Role: "assistant", Content: strings.Repeat("oldest answer ", 12)},
		{Role: "user", Content: strings.Repeat("middle question ", 12)},
		{Role: "assistant", Content: strings.Repeat("middle answer ", 12)},
		{Role: "user", Content: "recent question"},
		{Role: "assistant", Content: "recent answer"},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{messages: history},
		&GeneralAssistantTools{
			SystemMCP: client,
			Runner:    &tools.Runner{PostBeacon: allowToolCall},
		},
	)

	var updates []*turingv1.RuntimeUpdate
	var updatesBeforeRequest []int
	provider.onRequest = func(int) {
		updatesBeforeRequest = append(updatesBeforeRequest, len(updates))
	}
	if err := assistant.Execute(context.Background(), testJob(), func(update *turingv1.RuntimeUpdate) error {
		updates = append(updates, update)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if len(provider.requests) != 3 || len(updatesBeforeRequest) != 3 {
		t.Fatalf("requests/update markers = %d/%d, want 3/3", len(provider.requests), len(updatesBeforeRequest))
	}
	type omissionSet struct {
		history         int
		recall          bool
		skillIndex      bool
		toolDefinitions int
		results         int
	}
	var emitted []omissionSet
	var eventIndices []int
	for index, update := range updates {
		event := update.GetEvent()
		if event == nil || event.Type != turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP {
			continue
		}
		payload := event.GetPayload().AsMap()
		if payload["reason"] != "context_budget" {
			continue
		}
		emitted = append(emitted, omissionSet{
			history:         int(payload["historyMessagesOmitted"].(float64)),
			recall:          payload["recallOmitted"].(bool),
			skillIndex:      payload["skillIndexOmitted"].(bool),
			toolDefinitions: int(payload["toolDefinitionsOmitted"].(float64)),
			results:         int(payload["toolResultsOmitted"].(float64)),
		})
		eventIndices = append(eventIndices, index)
	}
	if len(emitted) < 2 {
		t.Fatalf("omission events = %#v, want changed sets across tool rounds", emitted)
	}
	for index := 1; index < len(emitted); index++ {
		if emitted[index] == emitted[index-1] {
			t.Fatalf("duplicate unchanged omission set emitted: %#v", emitted)
		}
	}
	for index, eventIndex := range eventIndices {
		if index >= len(updatesBeforeRequest) || eventIndex >= updatesBeforeRequest[index] {
			t.Fatalf(
				"omission event %d at update %d did not precede request marker %v",
				index,
				eventIndex,
				updatesBeforeRequest,
			)
		}
	}
	var last omissionSet
	emittedIndex := 0
	totalToolDefinitions := len(client.definitions) + 2
	for _, request := range provider.requests {
		current := omissionSet{toolDefinitions: totalToolDefinitions - len(request.Tools)}
		for _, message := range request.Messages {
			if strings.HasPrefix(message.Content, `{"contextBudget":{"omitted":true`) {
				current.results++
			}
		}
		for _, message := range history {
			if !containsMessageContent(request.Messages, message.Content) {
				current.history++
			}
		}
		if emittedIndex == 0 || current != last {
			if emittedIndex >= len(emitted) || emitted[emittedIndex] != current {
				t.Fatalf("notice sets = %#v, request omission set = %#v", emitted, current)
			}
			emittedIndex++
			last = current
		}
	}
	if emittedIndex != len(emitted) {
		t.Fatalf("notice sets = %#v, only %d matched request omissions", emitted, emittedIndex)
	}
}

func TestExecuteNoticesWhenOllamaReachesConfiguredOutputLimit(t *testing.T) {
	provider := &scriptedProvider{events: []llm.StreamEvent{
		{Type: "delta", Text: "partial answer"},
		{Type: "completed", FinishReason: "length"},
	}}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{},
	)

	updates := collectUpdates(t, assistant, testJob())

	noticeIndex, completionIndex := -1, -1
	for index, update := range updates {
		if completed := update.GetRunCompleted(); completed != nil {
			completionIndex = index
		}

		event := update.GetEvent()
		if event == nil || event.Type != turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP {
			continue
		}
		payload := event.GetPayload().AsMap()
		if payload["reason"] != "model_output_limit" {
			continue
		}
		noticeIndex = index
		if payload["maxOutputTokens"] != float64(llm.DefaultMaxOutputTokens) {
			t.Fatalf("maxOutputTokens = %#v, want %d", payload["maxOutputTokens"], llm.DefaultMaxOutputTokens)
		}
		note, _ := payload["note"].(string)
		if !strings.Contains(note, "OLLAMA_MAX_OUTPUT_TOKENS") {
			t.Fatalf("notice = %q, want Ollama configuration guidance", note)
		}
	}

	if noticeIndex < 0 || completionIndex < 0 || noticeIndex >= completionIndex {
		t.Fatalf("notice/completion indices = %d/%d, want notice before successful completion", noticeIndex, completionIndex)
	}
}

func TestExecuteNoticesRealOpenAILengthStopWithPendingToolFragment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w,
			"data: "+`{"choices":[{"index":0,"delta":{"content":"partial","tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"files_create","arguments":"{\"path\":\""}}]}}]}`+"\n\n"+
				"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`+"\n\n",
		)
	}))
	t.Cleanup(server.Close)
	provider, err := llm.NewOpenAICompatibleWithLimits(server.URL, "", server.Client(), 4096, 512)
	if err != nil {
		t.Fatal(err)
	}
	job := testJob()
	job.ModelProvider = turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE
	authorizeDirectRemoteJob(job, server.URL)
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{},
	)

	updates := collectUpdates(t, assistant, job)

	if failure := findRunFailed(updates); failure != nil {
		t.Fatalf("OpenAI length stop failed the run: %#v", failure)
	}
	var notice map[string]any
	for _, update := range updates {
		event := update.GetEvent()
		if event != nil && event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP &&
			event.GetPayload().AsMap()["reason"] == "model_output_limit" {
			notice = event.GetPayload().AsMap()
		}
	}

	if notice == nil || notice["setting"] != "OPENAI_MAX_OUTPUT_TOKENS" {
		t.Fatalf("output-limit notice = %#v, want OpenAI setting", notice)
	}
}

func TestExecuteDoesNotRunIDAndNameOnlyOpenAIToolCallAtLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w,
			"data: "+`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"files_create","arguments":""}}]}}]}`+"\n\n"+
				"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`+"\n\n",
		)
	}))
	t.Cleanup(server.Close)
	provider, err := llm.NewOpenAICompatibleWithLimits(server.URL, "", server.Client(), 4096, 512)
	if err != nil {
		t.Fatal(err)
	}
	job := testJob()
	job.ModelProvider = turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE
	authorizeDirectRemoteJob(job, server.URL)
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "files.create"}},
		result:      map[string]any{"ok": true},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{
			FilesMCP: client,
			Runner:   &tools.Runner{PostBeacon: allowToolCall},
		},
	)

	updates := collectUpdates(t, assistant, job)

	if len(client.calls) != 0 {
		t.Fatalf("MCP calls = %d, want no truncated-call side effect", len(client.calls))
	}
	if failure := findRunFailed(updates); failure != nil {
		t.Fatalf("length stop failed instead of completing with notice: %#v", failure)
	}
	if !containsStringFragment(runStepNotes(updates), "output limit") {
		t.Fatalf("run notes = %q, want output-limit notice", runStepNotes(updates))
	}
}

func TestExecuteDoesNotRunSparseOpenAIToolCallsAtLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w,
			"data: "+`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","type":"function","function":{"name":"first","arguments":"{}"}},{"index":2,"id":"call_2","type":"function","function":{"name":"third","arguments":"{}"}}]}}]}`+"\n\n"+
				"data: "+`{"choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`+"\n\n",
		)
	}))
	t.Cleanup(server.Close)
	provider, err := llm.NewOpenAICompatibleWithLimits(server.URL, "", server.Client(), 4096, 512)
	if err != nil {
		t.Fatal(err)
	}
	job := testJob()
	job.ModelProvider = turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE
	authorizeDirectRemoteJob(job, server.URL)
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "first"}, {"name": "third"}},
		result:      map[string]any{"ok": true},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{
			SystemMCP: client,
			Runner:    &tools.Runner{PostBeacon: allowToolCall},
		},
	)

	updates := collectUpdates(t, assistant, job)

	if len(client.calls) != 0 {
		t.Fatalf("MCP calls = %d, want no sparse-index side effects", len(client.calls))
	}
	failure := findRunFailed(updates)
	if failure == nil || failure.GetCode() != "model_bad_chunk" {
		t.Fatalf("failure = %#v, want model_bad_chunk", failure)
	}
}

func TestExecuteNoticesLengthLimitedToolTurnBeforeRunningTool(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{
			{Type: "tool_call", ToolCalls: []llm.ToolCall{{
				ID: "call_1", Name: "system.read", Arguments: map[string]any{},
			}}},
			{Type: "completed", FinishReason: "length"},
		},
		{
			{Type: "delta", Text: "done"},
			{Type: "completed", FinishReason: "stop"},
		},
	}}
	noticeSeen := false
	toolExecutedAfterNotice := false
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.read"}},
		callFunc: func(context.Context, string, map[string]any) (map[string]any, error) {
			toolExecutedAfterNotice = noticeSeen
			return map[string]any{"ok": true}, nil
		},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		&GeneralAssistantTools{
			SystemMCP: client,
			Runner:    &tools.Runner{PostBeacon: allowToolCall},
		},
	)

	var updates []*turingv1.RuntimeUpdate
	if err := assistant.Execute(context.Background(), testJob(), func(update *turingv1.RuntimeUpdate) error {
		updates = append(updates, update)
		event := update.GetEvent()
		if event == nil {
			return nil
		}
		payload := event.GetPayload().AsMap()
		if event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP &&
			payload["reason"] == "model_output_limit" {
			noticeSeen = true
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !noticeSeen || !toolExecutedAfterNotice {
		t.Fatalf("noticeSeen/toolExecutedAfterNotice = %v/%v; updates=%#v", noticeSeen, toolExecutedAfterNotice, updates)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsStringFragment(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

func containsToolDefinition(definitions []llm.ToolDefinition, name string) bool {
	for _, definition := range definitions {
		if definition.Name == name {
			return true
		}
	}
	return false
}
