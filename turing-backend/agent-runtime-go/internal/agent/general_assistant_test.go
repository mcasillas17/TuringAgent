package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
)

func TestExecuteRejectsNilJob(t *testing.T) {
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, nil)

	err := assistant.Execute(context.Background(), nil, discardUpdate)

	if err == nil || err.Error() != "job is required" {
		t.Fatalf("Execute error = %v, want job is required", err)
	}
}

func TestExecuteEmitsRunFailedForMissingProvider(t *testing.T) {
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, nil)

	updates := collectUpdates(t, assistant, testJob())

	failed := updates[len(updates)-1].GetRunFailed()
	if failed == nil || failed.Code != "model_provider_unavailable" {
		t.Fatalf("terminal update = %+v", updates[len(updates)-1])
	}
	if updates[0].GetEvent().GetType() != turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_STARTED {
		t.Fatalf("first update = %+v, want message started", updates[0])
	}
}

func TestExecuteCachesSuccessfulToolDiscovery(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "delta", Text: "first"}},
		{{Type: "delta", Text: "second"}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.time"}},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client},
	)

	collectUpdates(t, assistant, testJob())
	collectUpdates(t, assistant, testJob())

	if got := client.listCalls.Load(); got != 1 {
		t.Fatalf("ListTools calls = %d, want 1 cached discovery", got)
	}
	for index, request := range provider.requests {
		if len(request.Tools) != 1 || request.Tools[0].Name != "system.time" {
			t.Fatalf("request %d tools = %+v", index, request.Tools)
		}
	}
}

func TestExecuteRetriesFailedToolDiscovery(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{{{Type: "delta", Text: "recovered"}}}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.time"}},
		listErrors:  []error{errors.New("temporarily unavailable")},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client},
	)

	first := collectUpdates(t, assistant, testJob())
	failed := first[len(first)-1].GetRunFailed()
	if failed == nil || failed.Code != "tool_discovery_failed" || !failed.Retryable {
		t.Fatalf("first terminal update = %+v, want retryable tool_discovery_failed", first[len(first)-1])
	}
	second := collectUpdates(t, assistant, testJob())
	if second[len(second)-1].GetRunCompleted() == nil {
		t.Fatalf("second terminal update = %+v, want completed retry", second[len(second)-1])
	}
	if got := client.listCalls.Load(); got != 2 {
		t.Fatalf("ListTools calls = %d, want failed attempt and retry", got)
	}
}

func TestExecuteSharesConcurrentToolDiscovery(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var enteredOnce sync.Once
	client := &assistantTestToolLister{
		listFunc: func(ctx context.Context) ([]map[string]any, error) {
			enteredOnce.Do(func() { close(entered) })
			select {
			case <-release:
				return []map[string]any{{"name": "system.time"}}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	provider := &completionProvider{}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client},
	)
	errs := make(chan error, 2)
	go func() { errs <- assistant.Execute(context.Background(), testJob(), discardUpdate) }()
	<-entered
	go func() { errs <- assistant.Execute(context.Background(), testJob(), discardUpdate) }()
	close(release)

	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}
	}
	if got := client.listCalls.Load(); got != 1 {
		t.Fatalf("concurrent ListTools calls = %d, want 1", got)
	}
	if got := provider.calls.Load(); got != 2 {
		t.Fatalf("provider calls = %d, want 2 completed runs", got)
	}
}

func TestExecuteSharesConcurrentToolDiscoveryFailure(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var attempts atomic.Int32
	client := &assistantTestToolLister{
		listFunc: func(context.Context) ([]map[string]any, error) {
			if attempts.Add(1) == 1 {
				close(entered)
				<-release
				return nil, errors.New("discovery unavailable")
			}
			return []map[string]any{{"name": "system.time"}}, nil
		},
	}
	provider := &completionProvider{}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client},
	)

	type result struct {
		err     error
		updates []*turingv1.RuntimeUpdate
	}
	execute := func(ctx context.Context) result {
		var updates []*turingv1.RuntimeUpdate
		err := assistant.Execute(ctx, testJob(), func(update *turingv1.RuntimeUpdate) error {
			updates = append(updates, update)
			return nil
		})
		return result{err: err, updates: updates}
	}

	results := make(chan result, 2)
	go func() { results <- execute(context.Background()) }()
	<-entered
	waiting := make(chan struct{})
	go func() {
		results <- execute(&doneSignalingContext{Context: context.Background(), called: waiting})
	}()
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("second Execute did not join the discovery flight")
	}
	close(release)

	for range 2 {
		got := <-results
		if got.err != nil {
			t.Fatalf("Execute returned error: %v", got.err)
		}
		failed := got.updates[len(got.updates)-1].GetRunFailed()
		if failed == nil || failed.Code != "tool_discovery_failed" {
			t.Fatalf("terminal update = %+v, want tool_discovery_failed", got.updates[len(got.updates)-1])
		}
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("concurrent ListTools attempts = %d, want 1 shared failure", got)
	}

	if err := assistant.Execute(context.Background(), testJob(), discardUpdate); err != nil {
		t.Fatalf("subsequent Execute error: %v", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("ListTools attempts after separate retry = %d, want 2", got)
	}
}

func TestExecuteChecksContextWhileWaitingForToolDiscovery(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	client := &assistantTestToolLister{
		listFunc: func(ctx context.Context) ([]map[string]any, error) {
			close(entered)
			select {
			case <-release:
				return nil, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: &completionProvider{}},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client},
	)
	firstDone := make(chan error, 1)
	go func() { firstDone <- assistant.Execute(context.Background(), testJob(), discardUpdate) }()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan struct{})
	waiterDone := make(chan error, 1)
	go func() {
		waiterDone <- assistant.Execute(&doneSignalingContext{Context: ctx, called: waiting}, testJob(), discardUpdate)
	}()
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("second Execute did not begin waiting for tool discovery")
	}
	cancel()

	select {
	case err := <-waiterDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("waiting Execute error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("waiting Execute did not return its context cancellation")
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Execute returned error: %v", err)
	}
	if got := client.listCalls.Load(); got != 1 {
		t.Fatalf("ListTools calls = %d, want canceled waiter not to rediscover", got)
	}
}

func TestExecuteRetriesAfterCanceledToolDiscovery(t *testing.T) {
	entered := make(chan struct{})
	var attempt atomic.Int32
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.time"}},
		listFunc: func(ctx context.Context) ([]map[string]any, error) {
			if attempt.Add(1) == 1 {
				close(entered)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return []map[string]any{{"name": "system.time"}}, nil
		},
	}
	provider := &completionProvider{}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client},
	)
	ctx, cancel := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() { firstDone <- assistant.Execute(ctx, testJob(), discardUpdate) }()
	<-entered
	cancel()

	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled discovery error = %v, want context.Canceled", err)
	}
	if err := assistant.Execute(context.Background(), testJob(), discardUpdate); err != nil {
		t.Fatalf("retry Execute error: %v", err)
	}
	if got := client.listCalls.Load(); got != 2 {
		t.Fatalf("ListTools calls = %d, want canceled attempt and retry", got)
	}
	if got := provider.calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want only successful retry", got)
	}
}

func TestExecuteHealthyWaiterRetriesCanceledLeaderToolDiscovery(t *testing.T) {
	firstEntered := make(chan struct{})
	var attempts atomic.Int32
	client := &assistantTestToolLister{
		listFunc: func(ctx context.Context) ([]map[string]any, error) {
			if attempts.Add(1) == 1 {
				close(firstEntered)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return []map[string]any{{"name": "system.time"}}, nil
		},
	}
	provider := &completionProvider{}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client},
	)

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderDone := make(chan error, 1)
	go func() { leaderDone <- assistant.Execute(leaderCtx, testJob(), discardUpdate) }()
	<-firstEntered

	waiting := make(chan struct{})
	waiterCtx := &doneSignalingContext{Context: context.Background(), called: waiting}
	waiterDone := make(chan error, 1)
	go func() { waiterDone <- assistant.Execute(waiterCtx, testJob(), discardUpdate) }()
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("healthy Execute did not begin waiting for the leader's discovery")
	}
	cancelLeader()

	select {
	case err := <-leaderDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("leader Execute error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("leader Execute did not return after cancellation")
	}
	select {
	case err := <-waiterDone:
		if err != nil {
			t.Fatalf("healthy waiter Execute error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("healthy waiter did not retry discovery and complete")
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("ListTools attempts = %d, want canceled leader attempt and one retry", got)
	}
	if got := provider.calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want healthy waiter to complete once", got)
	}
}

func TestExecuteRunsModelChosenTool(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "provider_call_1", Name: "system.time", Arguments: map[string]any{"zone": "UTC"}}}}},
		{{Type: "delta", Text: "12:00"}, {Type: "completed", FinishReason: "stop"}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.time", "inputSchema": map[string]any{"type": "object"}}},
		result:      map[string]any{"time": "12:00"},
	}
	runner := &tools.Runner{PostBeacon: allowToolCall}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: runner},
	)

	updates := collectUpdates(t, assistant, testJob())

	if len(client.calls) != 1 || client.calls[0].name != "system.time" {
		t.Fatalf("tool calls = %+v, want one system.time call", client.calls)
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider requests = %d, want tool turn and final turn", len(provider.requests))
	}
	secondMessages := provider.requests[1].Messages
	if len(secondMessages) != 3 {
		t.Fatalf("second request messages = %+v, want user, assistant call, and tool result", secondMessages)
	}
	callMessage := secondMessages[1]
	if callMessage.Role != "assistant" || len(callMessage.ToolCalls) != 1 ||
		callMessage.ToolCalls[0].ID == "" || callMessage.ToolCalls[0].ID == "provider_call_1" {
		t.Fatalf("assistant tool-call message = %+v", callMessage)
	}
	if resultMessage := secondMessages[2]; resultMessage.Role != "tool" ||
		resultMessage.ToolCallID != callMessage.ToolCalls[0].ID ||
		resultMessage.Name != "system.time" || resultMessage.Content != `{"time":"12:00"}` {
		t.Fatalf("tool result message = %+v", resultMessage)
	}
	if completed := updates[len(updates)-1].GetRunCompleted(); completed == nil || completed.Content != "12:00" {
		t.Fatalf("terminal update = %+v, want final model content", updates[len(updates)-1])
	}
}

func TestExecuteRunnerBeaconsOwnKnownToolLifecycle(t *testing.T) {
	var beacons []*turingv1.ToolCallBeacon
	runner := &tools.Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		beacons = append(beacons, beacon)
		return allowToolCall(context.Background(), beacon)
	}}
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "provider_call_1", Name: "system.time"}}}},
		{{Type: "delta", Text: "done"}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.time"}},
		result:      map[string]any{"ok": true},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: runner},
	)

	updates := collectUpdates(t, assistant, testJob())

	var assistantLifecycle []turingv1.TuringEventType
	for _, eventType := range eventTypes(updates) {
		switch eventType {
		case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED,
			turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED:
			assistantLifecycle = append(assistantLifecycle, eventType)
		}
	}
	if len(assistantLifecycle) != 0 {
		t.Fatalf("assistant lifecycle events = %v, want beacon ownership", assistantLifecycle)
	}
	gotLifecycle := lifecycleFromBeacons(beacons)
	runtimeID := beacons[0].GetToolCallId()
	wantLifecycle := []beaconLifecycleEvent{
		{eventType: "tool.call.started", toolCallID: runtimeID},
		{eventType: "tool.call.completed", toolCallID: runtimeID},
	}
	if fmt.Sprint(gotLifecycle) != fmt.Sprint(wantLifecycle) {
		t.Fatalf("beacon lifecycle = %+v, want %+v", gotLifecycle, wantLifecycle)
	}
}

func TestExecuteDenialProducesDeniedBeaconWithoutAssistantFailure(t *testing.T) {
	var beacons []*turingv1.ToolCallBeacon
	runner := &tools.Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		beacons = append(beacons, beacon)
		if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
			return &turingv1.ToolPolicyDecision{
				Decision:   turingv1.ToolPolicyDecision_DECISION_DENY,
				ToolCallId: beacon.GetToolCallId(),
				Reason:     "blocked",
			}, nil
		}

		return &turingv1.ToolPolicyDecision{}, nil
	}}
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "provider_denied", Name: "system.time"}}}},
		{{Type: "delta", Text: "recovered"}},
	}}
	client := &assistantTestToolLister{definitions: []map[string]any{{"name": "system.time"}}}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: runner},
	)

	updates := collectUpdates(t, assistant, testJob())

	for _, update := range updates {
		if event := update.GetEvent(); event != nil &&
			event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED {
			t.Fatalf("assistant emitted failed event for denied runner call: %+v", event)
		}
	}
	gotLifecycle := lifecycleFromBeacons(beacons)
	runtimeID := beacons[0].GetToolCallId()
	wantLifecycle := []beaconLifecycleEvent{
		{eventType: "tool.call.started", toolCallID: runtimeID},
		{eventType: "tool.call.denied", toolCallID: runtimeID},
	}
	if fmt.Sprint(gotLifecycle) != fmt.Sprint(wantLifecycle) {
		t.Fatalf("beacon lifecycle = %+v, want %+v", gotLifecycle, wantLifecycle)
	}
}

func TestExecuteStopsSilentlyWhenApprovalAlreadyTerminalizedRun(t *testing.T) {
	var beacons []*turingv1.ToolCallBeacon
	runner := &tools.Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			beacons = append(beacons, beacon)
			return &turingv1.ToolPolicyDecision{
				Decision:   turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
				ApprovalId: "approval_1",
				ToolCallId: beacon.GetToolCallId(),
			}, nil
		},
		WaitApproval: func(context.Context, string) (string, error) {
			return "", agentTerminalRunError{message: "approval denied"}
		},
	}
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "provider_call", Name: "system.time"}}}},
		{{Type: "delta", Text: "must not run"}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.time"}},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: runner},
	)
	var updates []*turingv1.RuntimeUpdate

	err := assistant.Execute(context.Background(), testJob(), func(update *turingv1.RuntimeUpdate) error {
		updates = append(updates, update)
		return nil
	})

	if err != nil {
		t.Fatalf("Execute error = %v, want nil for externally terminalized run", err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want no model follow-up", len(provider.requests))
	}
	if len(client.calls) != 0 {
		t.Fatalf("MCP calls = %d, want 0", len(client.calls))
	}
	if len(beacons) != 1 || beacons[0].GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
		t.Fatalf("beacons = %+v, want only before beacon", beacons)
	}
	for _, update := range updates {
		if update.GetRunFailed() != nil || update.GetRunCompleted() != nil {
			t.Fatalf("runtime emitted terminal update after external terminalization: %+v", update)
		}
		if event := update.GetEvent(); event != nil &&
			(event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED ||
				event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED) {
			t.Fatalf("runtime emitted tool recovery event after external terminalization: %+v", event)
		}
	}
}

func TestExecuteRunsMultipleToolCallsSequentiallyAndPreservesTurnContent(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{
			{Type: "delta", Text: "Checking. "},
			{Type: "tool_call", ToolCalls: []llm.ToolCall{
				{ID: "call_a", Name: "system.first", Arguments: map[string]any{"order": 1}},
				{ID: "call_b", Name: "system.second", Arguments: map[string]any{"order": 2}},
			}},
		},
		{{Type: "delta", Text: "Done."}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.first"}, {"name": "system.second"}},
		callFunc: func(_ context.Context, name string, _ map[string]any) (map[string]any, error) {
			return map[string]any{"name": name}, nil
		},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}},
	)

	updates := collectUpdates(t, assistant, testJob())

	if len(client.calls) != 2 || client.calls[0].name != "system.first" || client.calls[1].name != "system.second" {
		t.Fatalf("tool execution order = %+v", client.calls)
	}
	threaded := provider.requests[1].Messages
	if got := threaded[1].Content; got != "Checking. " {
		t.Fatalf("assistant tool turn content = %q", got)
	}
	firstRuntimeID := threaded[1].ToolCalls[0].ID
	secondRuntimeID := threaded[1].ToolCalls[1].ID
	if firstRuntimeID == "" || firstRuntimeID == "call_a" ||
		secondRuntimeID == "" || secondRuntimeID == "call_b" ||
		firstRuntimeID == secondRuntimeID ||
		threaded[2].Role != "tool" || threaded[2].ToolCallID != firstRuntimeID ||
		threaded[3].Role != "tool" || threaded[3].ToolCallID != secondRuntimeID {
		t.Fatalf("threaded tool results = %+v", threaded)
	}
	completed := updates[len(updates)-1].GetRunCompleted()
	if completed == nil || completed.Content != "Checking. Done." {
		t.Fatalf("run completion = %+v, want all streamed content", completed)
	}
	messageCompleted := updates[len(updates)-2].GetEvent()
	if messageCompleted == nil || messageCompleted.Payload.AsMap()["content"] != completed.Content {
		t.Fatalf("message completion = %+v, want content %q", messageCompleted, completed.Content)
	}
	types := eventTypes(updates)
	wantTypes := []turingv1.TuringEventType{
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_STARTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED,
	}
	if fmt.Sprint(types) != fmt.Sprint(wantTypes) {
		t.Fatalf("event types = %v, want %v", types, wantTypes)
	}
}

func TestExecuteStopsAtMaximumToolIterations(t *testing.T) {
	provider := &loopingToolProvider{}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.repeat"}},
		result:      map[string]any{"ok": true},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}},
	)

	updates := collectUpdates(t, assistant, testJob())

	if got := provider.calls; got != maxToolIterations {
		t.Fatalf("provider calls = %d, want %d", got, maxToolIterations)
	}
	if got := len(client.calls); got != maxToolIterations {
		t.Fatalf("runner/client calls = %d, want %d", got, maxToolIterations)
	}
	var step *turingv1.TuringEvent
	for _, update := range updates {
		if event := update.GetEvent(); event != nil && event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP {
			step = event
		}
	}
	if step == nil || step.Payload.AsMap()["maxToolIterations"] != float64(maxToolIterations) {
		t.Fatalf("max-iteration step = %+v", step)
	}
	completed := updates[len(updates)-1].GetRunCompleted()
	if completed == nil || completed.Content != "12345" {
		t.Fatalf("run completion = %+v, want all visible streamed content", completed)
	}
}

func TestExecuteRejectsEntireToolBatchThatExceedsRunLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		calls int
	}{
		{name: "configured limit", limit: 2, calls: 3},
		{name: "safe default", limit: 0, calls: 11},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requested := make([]llm.ToolCall, test.calls)
			definitions := make([]map[string]any, test.calls)
			for index := range requested {
				name := fmt.Sprintf("system.tool_%d", index)
				requested[index] = llm.ToolCall{ID: "provider_reused", Name: name}
				definitions[index] = map[string]any{"name": name}
			}
			provider := &queuedProvider{responses: [][]llm.StreamEvent{{
				{Type: "tool_call", ToolCalls: requested},
			}}}
			client := &assistantTestToolLister{definitions: definitions}
			assistant := NewGeneralAssistant(
				map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
				fakeMessageClient{},
				&GeneralAssistantTools{
					SystemMCP:          client,
					Runner:             &tools.Runner{PostBeacon: allowToolCall},
					MaxToolCallsPerRun: test.limit,
				},
			)

			updates := collectUpdates(t, assistant, testJob())

			if len(client.calls) != 0 {
				t.Fatalf("MCP calls = %d, want no execution from oversized batch", len(client.calls))
			}
			if len(provider.requests) != 1 {
				t.Fatalf("provider requests = %d, want no follow-up model request", len(provider.requests))
			}
			failed := updates[len(updates)-1].GetRunFailed()
			if failed == nil || failed.Code != "tool_call_limit_exceeded" || failed.Retryable {
				t.Fatalf("terminal update = %+v, want non-retryable tool_call_limit_exceeded", updates[len(updates)-1])
			}
		})
	}
}

func TestExecuteCountsToolCallsCumulativelyIncludingUnknownCalls(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{
			{ID: "provider_1", Name: "unknown.first"},
			{ID: "provider_2", Name: "unknown.second"},
		}}},
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{
			{ID: "provider_3", Name: "system.first"},
			{ID: "provider_4", Name: "system.second"},
		}}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.first"}, {"name": "system.second"}},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{
			SystemMCP:          client,
			Runner:             &tools.Runner{PostBeacon: allowToolCall},
			MaxToolCallsPerRun: 3,
		},
	)

	updates := collectUpdates(t, assistant, testJob())

	if len(client.calls) != 0 {
		t.Fatalf("MCP calls = %d, want none from cumulative over-limit batch", len(client.calls))
	}
	if len(provider.requests) != 2 {
		t.Fatalf("provider requests = %d, want two model rounds", len(provider.requests))
	}
	failed := updates[len(updates)-1].GetRunFailed()
	if failed == nil || failed.Code != "tool_call_limit_exceeded" || failed.Retryable {
		t.Fatalf("terminal update = %+v, want non-retryable tool_call_limit_exceeded", updates[len(updates)-1])
	}
}

func TestExecuteAllowsExactToolCallLimit(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{
			{ID: "provider_1", Name: "system.first"},
			{ID: "provider_2", Name: "system.second"},
		}}},
		{{Type: "delta", Text: "done"}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.first"}, {"name": "system.second"}},
		result:      map[string]any{"ok": true},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{
			SystemMCP:          client,
			Runner:             &tools.Runner{PostBeacon: allowToolCall},
			MaxToolCallsPerRun: 2,
		},
	)

	updates := collectUpdates(t, assistant, testJob())

	if len(client.calls) != 2 {
		t.Fatalf("MCP calls = %d, want exact boundary executed", len(client.calls))
	}
	if updates[len(updates)-1].GetRunCompleted() == nil {
		t.Fatalf("terminal update = %+v, want completed run", updates[len(updates)-1])
	}
}

func TestExecuteReturnsCommittedSideEffectErrorWithoutModelRetry(t *testing.T) {
	reportErr := errors.New("completed beacon failed")
	runner := &tools.Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
			return nil, reportErr
		}
		return allowToolCall(context.Background(), beacon)
	}}
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "provider_call", Name: "system.write"}}}},
		{{Type: "delta", Text: "must not run"}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.write"}},
		result:      map[string]any{"ok": true},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: runner},
	)
	var updates []*turingv1.RuntimeUpdate

	err := assistant.Execute(context.Background(), testJob(), func(update *turingv1.RuntimeUpdate) error {
		updates = append(updates, update)
		return nil
	})

	if !errors.Is(err, reportErr) || !tools.SideEffectWasCommitted(err) {
		t.Fatalf("Execute error = %T %v, want committed-side-effect reporting error", err, err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("MCP calls = %d, want one committed call", len(client.calls))
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want no model retry", len(provider.requests))
	}
	for _, update := range updates {
		if update.GetRunFailed() != nil || update.GetRunCompleted() != nil {
			t.Fatalf("assistant emitted terminal runtime update: %+v", update)
		}
	}
}

func TestExecuteReturnsToolResultReportingErrorWithoutModelRetry(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "provider_call", Name: "system.read"}}}},
		{{Type: "delta", Text: "must not run"}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.read"}},
		result:      map[string]any{"bad": make(chan int)},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}},
	)
	var updates []*turingv1.RuntimeUpdate

	err := assistant.Execute(context.Background(), testJob(), func(update *turingv1.RuntimeUpdate) error {
		updates = append(updates, update)
		return nil
	})

	var reportingErr ToolResultReportingError
	if !errors.As(err, &reportingErr) || !strings.Contains(err.Error(), "json: unsupported type") {
		t.Fatalf("Execute error = %T %v, want ToolResultReportingError", err, err)
	}
	if len(client.calls) != 1 {
		t.Fatalf("MCP calls = %d, want one successful call", len(client.calls))
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want no model retry", len(provider.requests))
	}
	for _, update := range updates {
		if update.GetRunFailed() != nil || update.GetRunCompleted() != nil {
			t.Fatalf("assistant emitted terminal runtime update: %+v", update)
		}
	}
}

func TestExecuteSurfacesRecoverableToolErrorsToModel(t *testing.T) {
	tests := []struct {
		name        string
		definitions []map[string]any
		runner      *tools.Runner
		result      map[string]any
		callErr     error
		wantError   string
		wantCalls   int
		wantOwned   bool
	}{
		{
			name:      "unknown tool",
			runner:    &tools.Runner{PostBeacon: allowToolCall},
			wantError: "unknown_tool",
			wantOwned: true,
		},
		{
			name:        "nil runner",
			definitions: []map[string]any{{"name": "system.requested"}},
			wantError:   "tool_runner_unavailable",
			wantOwned:   true,
		},
		{
			name:        "runner denied",
			definitions: []map[string]any{{"name": "system.requested"}},
			runner: &tools.Runner{PostBeacon: func(_ context.Context, _ *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
				return &turingv1.ToolPolicyDecision{
					Decision: turingv1.ToolPolicyDecision_DECISION_DENY,
					Reason:   "blocked",
				}, nil
			}},
			wantError: "tool denied: blocked",
		},
		{
			name:        "runner error",
			definitions: []map[string]any{{"name": "system.requested"}},
			runner: &tools.Runner{PostBeacon: func(_ context.Context, _ *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
				return nil, errors.New("policy unavailable")
			}},
			wantError: "policy unavailable",
			wantOwned: true,
		},
		{
			name:        "MCP error",
			definitions: []map[string]any{{"name": "system.requested"}},
			runner:      &tools.Runner{PostBeacon: allowToolCall},
			callErr:     errors.New("MCP failed"),
			wantError:   "MCP failed",
			wantCalls:   1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &queuedProvider{responses: [][]llm.StreamEvent{
				{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "call_recover", Name: "system.requested"}}}},
				{{Type: "delta", Text: "recovered"}},
			}}
			client := &assistantTestToolLister{definitions: test.definitions, result: test.result}
			if test.callErr != nil {
				client.callFunc = func(context.Context, string, map[string]any) (map[string]any, error) {
					return nil, test.callErr
				}
			}
			assistant := NewGeneralAssistant(
				map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
				fakeMessageClient{},
				&GeneralAssistantTools{SystemMCP: client, Runner: test.runner},
			)

			updates := collectUpdates(t, assistant, testJob())

			types := eventTypes(updates)
			if test.wantOwned {
				if len(types) < 3 ||
					types[1] != turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED ||
					types[2] != turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED {
					t.Fatalf("event types = %v, want message started then tool started/failed", types)
				}
			} else {
				for _, eventType := range types {
					if eventType == turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED ||
						eventType == turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED ||
						eventType == turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED {
						t.Fatalf("event types = %v, want runner beacon ownership", types)
					}
				}
			}
			resultMessage := provider.requests[1].Messages[2]
			var result map[string]string
			if err := json.Unmarshal([]byte(resultMessage.Content), &result); err != nil {
				t.Fatalf("tool error result is not JSON: %q: %v", resultMessage.Content, err)
			}
			if !strings.Contains(result["error"], test.wantError) {
				t.Fatalf("tool error = %q, want substring %q", result["error"], test.wantError)
			}
			if completed := updates[len(updates)-1].GetRunCompleted(); completed == nil || completed.Content != "recovered" {
				t.Fatalf("terminal update = %+v", updates[len(updates)-1])
			}
			if len(client.calls) != test.wantCalls {
				t.Fatalf("MCP calls = %d, want %d", len(client.calls), test.wantCalls)
			}
		})
	}
}

func TestExecutePassesJobAndRegistryMetadataToRunner(t *testing.T) {
	var before *turingv1.ToolCallBeacon
	runner := &tools.Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
			before = beacon
		}
		return allowToolCall(context.Background(), beacon)
	}}
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
			ID: "provider_id", Name: "files.read", Arguments: map[string]any{"path": "note.txt"},
		}}}},
		{{Type: "delta", Text: "done"}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "files.read"}},
		result:      map[string]any{"content": "hello"},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{FilesMCP: client, Runner: runner},
	)

	collectUpdates(t, assistant, testJob())

	if before == nil ||
		before.ToolCallId == "" ||
		before.ToolCallId == "provider_id" ||
		before.AgentId != turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT ||
		before.RunId != "run_1" ||
		before.TraceId != "trace_1" ||
		before.ServerName != "files" ||
		before.ToolName != "files.read" {
		t.Fatalf("runner before beacon = %+v", before)
	}
	if len(client.calls) != 1 || client.calls[0].args["path"] != "note.txt" {
		t.Fatalf("MCP calls = %+v", client.calls)
	}
}

func TestExecuteRegeneratesDuplicateProviderToolCallIDsAndLinksResults(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{
			{ID: "duplicate", Name: "system.first"},
			{ID: "duplicate", Name: "system.second"},
		}}},
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{
			{ID: "duplicate", Name: "system.third"},
		}}},
		{{Type: "delta", Text: "done"}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{
			{"name": "system.first"},
			{"name": "system.second"},
			{"name": "system.third"},
		},
		result: map[string]any{"ok": true},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}},
	)

	collectUpdates(t, assistant, testJob())

	firstTurn := provider.requests[1].Messages
	firstID := firstTurn[1].ToolCalls[0].ID
	secondID := firstTurn[1].ToolCalls[1].ID
	if firstID == "" || firstID == "duplicate" || secondID == "" || secondID == "duplicate" || secondID == firstID {
		t.Fatalf("same-turn IDs = %q, %q; want fresh unique runtime IDs", firstID, secondID)
	}
	if firstTurn[2].ToolCallID != firstID || firstTurn[3].ToolCallID != secondID {
		t.Fatalf("first-turn tool result linkage = %+v", firstTurn)
	}

	secondTurn := provider.requests[2].Messages
	thirdID := secondTurn[4].ToolCalls[0].ID
	if thirdID == "" || thirdID == "duplicate" || thirdID == firstID || thirdID == secondID {
		t.Fatalf("cross-turn IDs = %q, %q, %q; want unique third ID", firstID, secondID, thirdID)
	}
	if secondTurn[5].ToolCallID != thirdID {
		t.Fatalf("third tool result ID = %q, want %q", secondTurn[5].ToolCallID, thirdID)
	}
}

func TestExecuteGeneratesUniqueToolCallIDsAcrossNewAssistantsConcurrently(t *testing.T) {
	const runCount = 64
	type result struct {
		id  string
		err error
	}
	results := make(chan result, runCount)
	for range runCount {
		go func() {
			provider := &queuedProvider{responses: [][]llm.StreamEvent{
				{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "provider_reused", Name: "system.time"}}}},
				{{Type: "delta", Text: "done"}},
			}}
			client := &assistantTestToolLister{
				definitions: []map[string]any{{"name": "system.time"}},
				result:      map[string]any{"ok": true},
			}
			assistant := NewGeneralAssistant(
				map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
				fakeMessageClient{},
				&GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}},
			)
			err := assistant.Execute(context.Background(), testJob(), discardUpdate)
			id := ""
			if len(provider.requests) == 2 {
				id = provider.requests[1].Messages[1].ToolCalls[0].ID
			}
			results <- result{id: id, err: err}
		}()
	}

	seen := make(map[string]struct{}, runCount)
	for range runCount {
		got := <-results
		if got.err != nil {
			t.Fatalf("Execute returned error: %v", got.err)
		}
		if got.id == "" || got.id == "provider_reused" {
			t.Fatalf("normalized ID = %q, want fresh runtime ID", got.id)
		}
		if _, exists := seen[got.id]; exists {
			t.Fatalf("duplicate runtime tool-call ID %q", got.id)
		}
		seen[got.id] = struct{}{}
	}
}

func TestExecuteGeneratesUniqueIDsForEmptyProviderToolCallIDs(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{{Name: "system.first"}}}},
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{{Name: "system.second"}}}},
		{{Type: "delta", Text: "done"}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.first"}, {"name": "system.second"}},
		result:      map[string]any{"ok": true},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}},
	)

	collectUpdates(t, assistant, testJob())

	firstID := provider.requests[1].Messages[1].ToolCalls[0].ID
	secondID := provider.requests[2].Messages[3].ToolCalls[0].ID
	if firstID == "" || secondID == "" || firstID == secondID {
		t.Fatalf("normalized IDs = %q, %q; want unique nonempty IDs", firstID, secondID)
	}
	if provider.requests[1].Messages[2].ToolCallID != firstID || provider.requests[2].Messages[4].ToolCallID != secondID {
		t.Fatalf("tool result linkage does not match assistant call IDs: %+v", provider.requests[2].Messages)
	}
}

func TestExecuteRewritesEveryProviderToolCallID(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{
			{ID: "runtime_call_1", Name: "system.first"},
			{Name: "system.second"},
		}}},
		{{Type: "delta", Text: "done"}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.first"}, {"name": "system.second"}},
		result:      map[string]any{"ok": true},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}},
	)

	collectUpdates(t, assistant, testJob())

	calls := provider.requests[1].Messages[1].ToolCalls
	if calls[0].ID == "" || calls[0].ID == "runtime_call_1" {
		t.Fatalf("first runtime ID = %q, want fresh ID", calls[0].ID)
	}
	if calls[1].ID == "" || calls[1].ID == calls[0].ID {
		t.Fatalf("runtime IDs = %q, %q; want unique", calls[0].ID, calls[1].ID)
	}
}

func TestExecuteReturnsWhenContextIsCanceledWhileWaitingForModelEvent(t *testing.T) {
	provider := &blockingProvider{
		entered: make(chan struct{}),
		events:  make(chan llm.StreamEvent),
	}
	defer close(provider.events)
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- assistant.Execute(ctx, testJob(), discardUpdate) }()
	<-provider.entered
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Execute error = %v, want context.Canceled", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Execute did not observe cancellation while waiting for a model event")
	}
}

func TestExecuteDoesNotStartAnotherModelTurnAfterToolCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "call_cancel", Name: "system.cancel"}}}},
		{{Type: "delta", Text: "must not run"}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.cancel"}},
		callFunc: func(context.Context, string, map[string]any) (map[string]any, error) {
			cancel()
			return nil, context.Canceled
		},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}},
	)

	err := assistant.Execute(ctx, testJob(), discardUpdate)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context.Canceled", err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want no model turn after cancellation", len(provider.requests))
	}
}

func TestExecuteStopsMultipleToolCallsImmediatelyOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	var runnerCalls atomic.Int32
	var mcpCalls atomic.Int32
	provider := &queuedProvider{responses: [][]llm.StreamEvent{{
		{Type: "tool_call", ToolCalls: []llm.ToolCall{
			{ID: "call_first", Name: "system.first"},
			{ID: "call_second", Name: "system.second"},
		}},
	}}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.first"}, {"name": "system.second"}},
		callFunc: func(ctx context.Context, _ string, _ map[string]any) (map[string]any, error) {
			switch mcpCalls.Add(1) {
			case 1:
				close(firstStarted)
				<-ctx.Done()
				return nil, ctx.Err()
			default:
				close(secondStarted)
				return map[string]any{"unexpected": true}, nil
			}
		},
	}
	runner := &tools.Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
			runnerCalls.Add(1)
		}
		return allowToolCall(context.Background(), beacon)
	}}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: runner},
	)
	var updates []*turingv1.RuntimeUpdate
	done := make(chan error, 1)
	go func() {
		done <- assistant.Execute(ctx, testJob(), func(update *turingv1.RuntimeUpdate) error {
			updates = append(updates, update)
			return nil
		})
	}()
	<-firstStarted
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Execute error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Execute did not return after the first tool call observed cancellation")
	}
	if got := runnerCalls.Load(); got != 1 {
		t.Fatalf("runner calls = %d, want 1", got)
	}
	if got := mcpCalls.Load(); got != 1 {
		t.Fatalf("MCP calls = %d, want 1", got)
	}
	select {
	case <-secondStarted:
		t.Fatal("second tool call started after cancellation")
	default:
	}
	for _, update := range updates {
		if event := update.GetEvent(); event != nil &&
			(event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED ||
				event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED) {
			t.Fatalf("canceled tool call emitted recovery event %s", event.Type)
		}
	}
}

func TestExecutePropagatesAssistantOwnedToolLifecycleEmitErrors(t *testing.T) {
	emitErr := errors.New("emit failed")
	tests := []turingv1.TuringEventType{
		turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED,
	}
	for _, target := range tests {
		t.Run(target.String(), func(t *testing.T) {
			provider := &queuedProvider{responses: [][]llm.StreamEvent{
				{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "call_emit", Name: "system.requested"}}}},
			}}
			client := &assistantTestToolLister{}
			assistant := NewGeneralAssistant(
				map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
				fakeMessageClient{},
				&GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}},
			)

			err := assistant.Execute(context.Background(), testJob(), func(update *turingv1.RuntimeUpdate) error {
				if event := update.GetEvent(); event != nil && event.Type == target {
					return emitErr
				}
				return nil
			})

			if !errors.Is(err, emitErr) {
				t.Fatalf("Execute error = %v, want emit error", err)
			}
		})
	}
}

func TestExecutePropagatesMessageEmitErrors(t *testing.T) {
	emitErr := errors.New("emit failed")
	targets := []turingv1.TuringEventType{
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_STARTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED,
	}
	for _, target := range targets {
		t.Run(target.String(), func(t *testing.T) {
			provider := &scriptedProvider{events: []llm.StreamEvent{{Type: "delta", Text: "hello"}}}
			assistant := NewGeneralAssistant(
				map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
				fakeMessageClient{},
				nil,
			)

			err := assistant.Execute(context.Background(), testJob(), func(update *turingv1.RuntimeUpdate) error {
				if event := update.GetEvent(); event != nil && event.Type == target {
					return emitErr
				}
				return nil
			})

			if !errors.Is(err, emitErr) {
				t.Fatalf("Execute error = %v, want emit error", err)
			}
		})
	}
	t.Run("run completed", func(t *testing.T) {
		assistant := NewGeneralAssistant(
			map[turingv1.ModelProvider]llm.Provider{
				turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: &scriptedProvider{},
			},
			fakeMessageClient{},
			nil,
		)
		err := assistant.Execute(context.Background(), testJob(), func(update *turingv1.RuntimeUpdate) error {
			if update.GetRunCompleted() != nil {
				return emitErr
			}
			return nil
		})
		if !errors.Is(err, emitErr) {
			t.Fatalf("Execute error = %v, want emit error", err)
		}
	})
}

func TestExecuteEmitsRunFailedWhenMessageFetchFails(t *testing.T) {
	fetchErr := errors.New("messages unavailable")
	assistant := NewGeneralAssistant(nil, fakeMessageClient{err: fetchErr}, nil)

	updates := collectUpdates(t, assistant, testJob())

	failed := updates[len(updates)-1].GetRunFailed()
	if failed == nil || failed.Code != "message_fetch_failed" || failed.Message != fetchErr.Error() || !failed.Retryable {
		t.Fatalf("terminal update = %+v, want retryable message_fetch_failed", updates[len(updates)-1])
	}
}

func TestExecuteEmitsRunFailedWhenProviderStreamStartFails(t *testing.T) {
	streamErr := errors.New("stream unavailable")
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: errorProvider{err: streamErr},
		},
		fakeMessageClient{},
		nil,
	)

	updates := collectUpdates(t, assistant, testJob())

	failed := updates[len(updates)-1].GetRunFailed()
	if failed == nil || failed.Code != "model_stream_failed" || failed.Message != streamErr.Error() || !failed.Retryable {
		t.Fatalf("terminal update = %+v, want retryable model_stream_failed", updates[len(updates)-1])
	}
}

func TestExecutePreservesDebugToolPath(t *testing.T) {
	provider := &scriptedProvider{}
	client := &assistantTestToolLister{result: map[string]any{"time": "12:00"}}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}},
	)
	job := testJob()
	job.UserText = "/tool system.time"

	updates := collectUpdates(t, assistant, job)

	if len(provider.requests) != 0 {
		t.Fatalf("provider requests = %d, want debug path to bypass model", len(provider.requests))
	}
	if len(client.calls) != 1 || client.calls[0].name != "system.time" {
		t.Fatalf("debug tool calls = %+v", client.calls)
	}
	if completed := updates[len(updates)-1].GetRunCompleted(); completed == nil || completed.Content != `{"time":"12:00"}` {
		t.Fatalf("terminal update = %+v", updates[len(updates)-1])
	}
}

func TestGeneralAssistantStreamsDeltasAndCompletesRun(t *testing.T) {
	provider := &scriptedProvider{events: []llm.StreamEvent{{Type: "delta", Text: "Hel"}, {Type: "delta", Text: "lo"}, {Type: "completed", FinishReason: "stop"}}}
	assistant := NewGeneralAssistant(map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider}, fakeMessageClient{messages: []llm.ChatMessage{{Role: "system", Content: "Be helpful"}}}, nil)
	updates := collectUpdates(t, assistant, testJob())

	if updates[0].GetEvent().Type != turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_STARTED {
		t.Fatalf("first update = %+v, want message.started", updates[0])
	}
	if updates[1].GetEvent().Type != turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA || updates[1].GetEvent().Payload.AsMap()["delta"] != "Hel" {
		t.Fatalf("second update = %+v, want first delta", updates[1])
	}
	if updates[3].GetEvent().Type != turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED {
		t.Fatalf("message completion update = %+v", updates[3])
	}
	if completed := updates[4].GetRunCompleted(); completed == nil || completed.Content != "Hello" || completed.AssistantMessageId != "msg_assistant" {
		t.Fatalf("terminal update = %+v, want run_completed content", updates[4])
	}
	if len(provider.requests) != 1 || len(provider.requests[0].Messages) != 2 || provider.requests[0].Messages[1].Content != "hi" {
		t.Fatalf("provider requests = %+v", provider.requests)
	}
	if provider.requests[0].Tools == nil || len(provider.requests[0].Tools) != 0 {
		t.Fatalf("nil toolset request tools = %#v, want usable empty definitions", provider.requests[0].Tools)
	}
}

func TestGeneralAssistantClassifiesStreamedProviderErrors(t *testing.T) {
	tests := []struct {
		code      string
		retryable bool
	}{
		{code: "model_unavailable", retryable: true},
		{code: "model_stream_error", retryable: true},
		{code: "model_bad_chunk", retryable: false},
		{code: "model_error", retryable: false},
		{code: "", retryable: false},
	}
	for _, test := range tests {
		name := test.code
		if name == "" {
			name = "empty defaults to model_error"
		}
		t.Run(name, func(t *testing.T) {
			provider := &scriptedProvider{events: []llm.StreamEvent{{
				Type: "error", Code: test.code, Message: "provider error",
			}}}
			assistant := NewGeneralAssistant(
				map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
				fakeMessageClient{},
				nil,
			)

			updates := collectUpdates(t, assistant, testJob())

			failed := updates[len(updates)-1].GetRunFailed()
			wantCode := test.code
			if wantCode == "" {
				wantCode = "model_error"
			}
			if failed == nil || failed.Code != wantCode || failed.Message != "provider error" || failed.Retryable != test.retryable {
				t.Fatalf("last update = %+v, want code %q retryable=%t", updates[len(updates)-1], wantCode, test.retryable)
			}
		})
	}
}

func collectUpdates(t *testing.T, assistant *GeneralAssistant, job *turingv1.AgentJob) []*turingv1.RuntimeUpdate {
	t.Helper()
	var updates []*turingv1.RuntimeUpdate
	if err := assistant.Execute(context.Background(), job, func(update *turingv1.RuntimeUpdate) error {
		updates = append(updates, update)
		return nil
	}); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	return updates
}

func testJob() *turingv1.AgentJob {
	return &turingv1.AgentJob{
		JobId:              "job_1",
		RunId:              "run_1",
		SessionId:          "sess_1",
		UserMessageId:      "msg_user",
		AssistantMessageId: "msg_assistant",
		TraceId:            "trace_1",
		ModelProvider:      turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:              "llama3.2",
		UserText:           "hi",
	}
}

type scriptedProvider struct {
	events   []llm.StreamEvent
	requests []llm.ChatRequest
}

func (p *scriptedProvider) ID() string { return "ollama" }

func (p *scriptedProvider) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	p.requests = append(p.requests, req)
	out := make(chan llm.StreamEvent, len(p.events))
	go func() {
		defer close(out)
		for _, event := range p.events {
			out <- event
		}
	}()
	return out, nil
}

type fakeMessageClient struct {
	messages []llm.ChatMessage
	err      error
}

func (c fakeMessageClient) FetchMessages(ctx context.Context, sessionID string) ([]llm.ChatMessage, error) {
	return c.messages, c.err
}

type queuedProvider struct {
	responses [][]llm.StreamEvent
	requests  []llm.ChatRequest
}

func (p *queuedProvider) ID() string { return "queued" }

func (p *queuedProvider) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	p.requests = append(p.requests, req)
	index := len(p.requests) - 1
	out := make(chan llm.StreamEvent, len(p.responses[index]))
	for _, event := range p.responses[index] {
		out <- event
	}
	close(out)
	return out, nil
}

type assistantTestToolCall struct {
	name string
	args map[string]any
}

type assistantTestToolLister struct {
	definitions []map[string]any
	result      map[string]any
	calls       []assistantTestToolCall
	listErrors  []error
	listFunc    func(context.Context) ([]map[string]any, error)
	callFunc    func(context.Context, string, map[string]any) (map[string]any, error)
	listCalls   atomic.Int32
}

func (c *assistantTestToolLister) ListTools(ctx context.Context) ([]map[string]any, error) {
	c.listCalls.Add(1)
	if c.listFunc != nil {
		return c.listFunc(ctx)
	}
	if len(c.listErrors) > 0 {
		err := c.listErrors[0]
		c.listErrors = c.listErrors[1:]
		return nil, err
	}
	return c.definitions, nil
}

func (c *assistantTestToolLister) CallTool(ctx context.Context, name string, args map[string]any, _ ...string) (map[string]any, error) {
	c.calls = append(c.calls, assistantTestToolCall{name: name, args: args})
	if c.callFunc != nil {
		return c.callFunc(ctx, name, args)
	}
	return c.result, nil
}

func allowToolCall(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
	return &turingv1.ToolPolicyDecision{
		Decision:   turingv1.ToolPolicyDecision_DECISION_ALLOW,
		ToolCallId: beacon.GetToolCallId(),
	}, nil
}

type completionProvider struct {
	calls atomic.Int32
}

func (p *completionProvider) ID() string { return "completion" }

func (p *completionProvider) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	p.calls.Add(1)
	out := make(chan llm.StreamEvent, 1)
	out <- llm.StreamEvent{Type: "completed"}
	close(out)
	return out, nil
}

func discardUpdate(*turingv1.RuntimeUpdate) error { return nil }

type doneSignalingContext struct {
	context.Context
	called chan struct{}
	once   sync.Once
}

func (c *doneSignalingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.called) })
	return c.Context.Done()
}

func eventTypes(updates []*turingv1.RuntimeUpdate) []turingv1.TuringEventType {
	var result []turingv1.TuringEventType
	for _, update := range updates {
		if event := update.GetEvent(); event != nil {
			result = append(result, event.Type)
		}
	}
	return result
}

type beaconLifecycleEvent struct {
	eventType  string
	toolCallID string
}

func lifecycleFromBeacons(beacons []*turingv1.ToolCallBeacon) []beaconLifecycleEvent {
	result := make([]beaconLifecycleEvent, 0, len(beacons))
	for _, beacon := range beacons {
		eventType := ""
		switch beacon.GetPhase() {
		case turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE:
			eventType = "tool.call.started"
		case turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER:
			switch beacon.GetStatus() {
			case turingv1.ToolCallStatus_TOOL_CALL_STATUS_COMPLETED:
				eventType = "tool.call.completed"
			case turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED:
				eventType = "tool.call.failed"
			case turingv1.ToolCallStatus_TOOL_CALL_STATUS_DENIED:
				eventType = "tool.call.denied"
			}
		}
		if eventType != "" {
			result = append(result, beaconLifecycleEvent{
				eventType: eventType, toolCallID: beacon.GetToolCallId(),
			})
		}
	}
	return result
}

type loopingToolProvider struct {
	calls int
}

func (p *loopingToolProvider) ID() string { return "looping" }

func (p *loopingToolProvider) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	p.calls++
	out := make(chan llm.StreamEvent, 2)
	out <- llm.StreamEvent{Type: "delta", Text: fmt.Sprint(p.calls)}
	out <- llm.StreamEvent{Type: "tool_call", ToolCalls: []llm.ToolCall{{
		ID: fmt.Sprintf("call_%d", p.calls), Name: "system.repeat",
	}}}
	close(out)
	return out, nil
}

type blockingProvider struct {
	entered chan struct{}
	events  chan llm.StreamEvent
}

func (p *blockingProvider) ID() string { return "blocking" }

func (p *blockingProvider) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	close(p.entered)
	return p.events, nil
}

type errorProvider struct {
	err error
}

func (p errorProvider) ID() string { return "error" }

func (p errorProvider) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	return nil, p.err
}

type agentTerminalRunError struct {
	message string
}

func (e agentTerminalRunError) Error() string     { return e.message }
func (e agentTerminalRunError) RunTerminal() bool { return true }
