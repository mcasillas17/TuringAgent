package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestExecuteRejectsNilJob(t *testing.T) {
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, nil)

	err := assistant.Execute(context.Background(), nil, discardUpdate)

	if err == nil || err.Error() != "job is required" {
		t.Fatalf("Execute error = %v, want job is required", err)
	}
}

func TestExecuteAddsFallbackWhenIterationCapHasNoVisibleContent(t *testing.T) {
	provider := &silentLoopingToolProvider{}
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

	const fallback = "Tool iteration limit reached before a final response."
	if got := provider.calls; got != maxToolIterations {
		t.Fatalf("provider calls = %d, want %d", got, maxToolIterations)
	}
	if got := eventTypes(updates); fmt.Sprint(got) != fmt.Sprint([]turingv1.TuringEventType{
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_STARTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP,
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED,
	}) {
		t.Fatalf("event types = %v, want started, cap step, fallback delta, completed", got)
	}
	delta := updates[len(updates)-3].GetEvent()
	if delta == nil || delta.Payload.AsMap()["delta"] != fallback {
		t.Fatalf("fallback delta = %+v, want %q", delta, fallback)
	}
	completed := updates[len(updates)-1].GetRunCompleted()
	if completed == nil || completed.Content != fallback {
		t.Fatalf("run completion = %+v, want fallback content", completed)
	}
}

func TestExecuteReplacesWhitespaceOnlyIterationCapContentWithFallback(t *testing.T) {
	provider := &whitespaceLoopingToolProvider{}
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

	completed := updates[len(updates)-1].GetRunCompleted()
	if completed == nil || completed.Content != toolIterationFallback {
		t.Fatalf("run completion = %+v, want fallback %q", completed, toolIterationFallback)
	}
	fallbackDelta := updates[len(updates)-3].GetEvent()
	if fallbackDelta == nil || fallbackDelta.GetPayload().AsMap()["delta"] != toolIterationFallback {
		t.Fatalf("fallback delta = %+v, want %q", fallbackDelta, toolIterationFallback)
	}
}

func TestExecuteAddsFallbackWhenFinalModelTurnHasNoVisibleContent(t *testing.T) {
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: &completionProvider{},
		},
		fakeMessageClient{},
		nil,
	)

	updates := collectUpdates(t, assistant, testJob())

	const fallback = "The model returned an empty response."
	if got := eventTypes(updates); fmt.Sprint(got) != fmt.Sprint([]turingv1.TuringEventType{
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_STARTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED,
	}) {
		t.Fatalf("event types = %v, want started, fallback delta, completed", got)
	}
	if got := updates[1].GetEvent().GetPayload().AsMap()["delta"]; got != fallback {
		t.Fatalf("fallback delta = %#v, want %q", got, fallback)
	}
	if completed := updates[len(updates)-1].GetRunCompleted(); completed == nil || completed.Content != fallback {
		t.Fatalf("run completion = %+v, want fallback content", completed)
	}
}

func TestExecuteReplacesWhitespaceOnlyFinalModelContentWithFallback(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{{
		{Type: "delta", Text: " \n\t"},
		{Type: "completed", FinishReason: "stop"},
	}}}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		nil,
	)

	updates := collectUpdates(t, assistant, testJob())

	if completed := updates[len(updates)-1].GetRunCompleted(); completed == nil || completed.Content != emptyFinalFallback {
		t.Fatalf("run completion = %+v, want fallback %q", completed, emptyFinalFallback)
	}
	fallbackDelta := updates[len(updates)-3].GetEvent()
	if fallbackDelta == nil || fallbackDelta.GetPayload().AsMap()["delta"] != emptyFinalFallback {
		t.Fatalf("fallback delta = %+v, want %q", fallbackDelta, emptyFinalFallback)
	}
}

func TestExecuteRejectsModelOutputBeyondAggregateByteLimit(t *testing.T) {
	provider := &scriptedProvider{events: []llm.StreamEvent{
		{Type: "delta", Text: strings.Repeat("a", maxModelOutputBytes)},
		{Type: "delta", Text: "b"},
	}}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider,
		},
		fakeMessageClient{},
		nil,
	)

	updates := collectUpdates(t, assistant, testJob())

	failed := updates[len(updates)-1].GetRunFailed()
	if failed == nil || failed.Code != "model_output_limit_exceeded" || failed.Retryable {
		t.Fatalf("terminal update = %+v, want non-retryable model_output_limit_exceeded", updates[len(updates)-1])
	}
	if got := eventTypes(updates); !reflect.DeepEqual(got, []turingv1.TuringEventType{
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_STARTED,
		turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA,
	}) {
		t.Fatalf("event types = %v, want started and only the in-limit delta", got)
	}
}

func TestMaximumModelOutputCompletionUpdatesFitGRPCTransport(t *testing.T) {
	const maxGRPCMessageBytes = 4 * 1024 * 1024
	job := testJob()
	job.RunId = "run_" + strings.Repeat("r", 26)
	job.SessionId = "sess_" + strings.Repeat("s", 26)
	job.AssistantMessageId = "msg_" + strings.Repeat("m", 26)
	job.TraceId = "trace_" + strings.Repeat("t", 26)

	var updates []*turingv1.RuntimeUpdate
	err := completeRun(func(update *turingv1.RuntimeUpdate) error {
		updates = append(updates, update)
		return nil
	}, job, strings.Repeat("x", maxModelOutputBytes))
	if err != nil {
		t.Fatalf("completeRun returned error: %v", err)
	}
	if len(updates) != 2 {
		t.Fatalf("completion updates = %d, want message.completed and run_completed", len(updates))
	}
	for index, update := range updates {
		encoded, err := proto.Marshal(update)
		if err != nil {
			t.Fatalf("marshal completion update %d: %v", index, err)
		}
		if len(encoded) >= maxGRPCMessageBytes {
			t.Errorf(
				"completion update %d serialized bytes = %d, want below %d-byte gRPC transport limit",
				index,
				len(encoded),
				maxGRPCMessageBytes,
			)
		}
	}
}

func TestExecuteEnforcesAggregateSuccessfulToolResultLimit(t *testing.T) {
	const limit = 4 * 1024 * 1024
	for _, test := range []struct {
		name       string
		resultSize int
		wantFailed bool
	}{
		{name: "exact boundary", resultSize: limit},
		{name: "overflow", resultSize: limit + 1, wantFailed: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := []llm.ToolCall{{ID: "provider_call", Name: "files.create"}}
			if test.wantFailed {
				calls = append(calls, llm.ToolCall{ID: "must_not_run", Name: "files.create"})
			}
			provider := &queuedProvider{responses: [][]llm.StreamEvent{
				{{Type: "tool_call", ToolCalls: calls}},
				{{Type: "delta", Text: "done"}},
			}}
			client := &assistantTestToolLister{
				definitions: []map[string]any{{"name": "files.create"}},
				result:      toolResultWithSerializedSize(t, test.resultSize),
			}
			runner := &tools.Runner{
				PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
					return approvalToolCall(beacon), nil
				},
				WaitApproval: func(context.Context, string) (string, error) { return "token", nil },
			}
			assistant := NewGeneralAssistant(
				map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
				fakeMessageClient{},
				&GeneralAssistantTools{FilesMCP: client, Runner: runner},
			)

			updates := collectUpdates(t, assistant, testJob())

			if test.wantFailed {
				failed := updates[len(updates)-1].GetRunFailed()
				if failed == nil || failed.Code != "tool_result_limit_exceeded" || failed.Retryable {
					t.Fatalf("terminal update = %+v, want non-retryable tool_result_limit_exceeded", updates[len(updates)-1])
				}
				if len(provider.requests) != 1 {
					t.Fatalf("provider requests = %d, want no follow-up model call", len(provider.requests))
				}
				if len(client.calls) != 1 {
					t.Fatalf("side-effecting MCP calls = %d, want only completed overflow call", len(client.calls))
				}
				return
			}
			if completed := updates[len(updates)-1].GetRunCompleted(); completed == nil || completed.Content != "done" {
				t.Fatalf("terminal update = %+v, want completed boundary result", updates[len(updates)-1])
			}
			if got := len(provider.requests[1].Messages[2].Content); got != limit {
				t.Fatalf("serialized tool result bytes = %d, want %d", got, limit)
			}
		})
	}
}

func TestExecuteCountsAggregateErrorToolResultsAcrossCalls(t *testing.T) {
	const limit = 4 * 1024 * 1024
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{
			{ID: "first", Name: "system.first"},
			{ID: "second", Name: "system.second"},
		}}},
		{{Type: "delta", Text: "must not continue"}},
	}}
	call := 0
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.first"}, {"name": "system.second"}},
		callFunc: func(context.Context, string, map[string]any) (map[string]any, error) {
			call++
			size := limit / 2
			if call == 2 {
				size++
			}
			return nil, errors.New(strings.Repeat("e", size-len(`{"error":""}`)))
		},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}},
	)

	updates := collectUpdates(t, assistant, testJob())

	failed := updates[len(updates)-1].GetRunFailed()
	if failed == nil || failed.Code != "tool_result_limit_exceeded" || failed.Retryable {
		t.Fatalf("terminal update = %+v, want aggregate error-result limit failure", updates[len(updates)-1])
	}
	if len(client.calls) != 2 || len(provider.requests) != 1 {
		t.Fatalf("calls: MCP=%d model=%d, want 2 and 1", len(client.calls), len(provider.requests))
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

func TestExecuteRetriesToolDiscoveryAfterTimeoutWithoutCachingFailure(t *testing.T) {
	var attempts atomic.Int32
	client := &assistantTestToolLister{
		listFunc: func(ctx context.Context) ([]map[string]any, error) {
			if attempts.Add(1) == 1 {
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
		&GeneralAssistantTools{SystemMCP: client, ToolTimeout: 10 * time.Millisecond},
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
		t.Fatalf("ListTools calls = %d, want timeout followed by retry", got)
	}
}

func TestExecuteMakesToolDiscoveryValidationFailureNonRetryable(t *testing.T) {
	client := &assistantTestToolLister{
		definitions: []map[string]any{{
			"name":        "invalid",
			"inputSchema": "not-an-object",
		}},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: &completionProvider{},
		},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client},
	)

	updates := collectUpdates(t, assistant, testJob())

	failed := updates[len(updates)-1].GetRunFailed()
	if failed == nil || failed.Code != "tool_discovery_failed" || failed.Retryable {
		t.Fatalf("terminal update = %+v, want non-retryable tool_discovery_failed", updates[len(updates)-1])
	}
}

func TestExecuteUsesListToolsRetryClassification(t *testing.T) {
	tests := []struct {
		name      string
		listErr   error
		retryable bool
	}{
		{
			name:    "HTTP 401",
			listErr: assistantClassifiedListError{message: "MCP HTTP 401"},
		},
		{
			name:      "HTTP 500",
			listErr:   assistantClassifiedListError{message: "MCP HTTP 500", retryable: true},
			retryable: true,
		},
		{
			name:    "malformed response",
			listErr: assistantClassifiedListError{message: "malformed MCP response"},
		},
		{
			name:      "network failure",
			listErr:   assistantClassifiedListError{message: "network unavailable", retryable: true},
			retryable: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &completionProvider{}
			client := &assistantTestToolLister{listErrors: []error{test.listErr}}
			assistant := NewGeneralAssistant(
				map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
				fakeMessageClient{},
				&GeneralAssistantTools{SystemMCP: client},
			)

			updates := collectUpdates(t, assistant, testJob())

			failed := updates[len(updates)-1].GetRunFailed()
			if failed == nil || failed.Code != "tool_discovery_failed" || failed.Retryable != test.retryable {
				t.Fatalf("terminal update = %+v, want retryable=%t tool_discovery_failed", updates[len(updates)-1], test.retryable)
			}
			if got := provider.calls.Load(); got != 0 {
				t.Fatalf("provider calls = %d, want no model call after discovery failure", got)
			}
		})
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
		callMessage.ToolCalls[0].ID != "provider_call_1" {
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

func TestExecuteStopsAfterUncertainMCPCallFailure(t *testing.T) {
	callErr := errors.New("connection reset after request")
	var beacons []*turingv1.ToolCallBeacon
	runner := &tools.Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			beacons = append(beacons, beacon)
			return approvalToolCall(beacon), nil
		},
		WaitApproval: func(context.Context, string) (string, error) { return "token", nil },
	}
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "provider_call", Name: "system.write"}}}},
		{{Type: "delta", Text: "must not run"}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.write"}},
		callFunc: func(context.Context, string, map[string]any) (map[string]any, error) {
			return nil, callErr
		},
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

	if !errors.Is(err, callErr) || !strings.Contains(fmt.Sprintf("%T", err), "SideEffectUnknownError") {
		t.Fatalf("Execute error = %T %v, want SideEffectUnknownError", err, err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want no continuation after uncertain call", len(provider.requests))
	}
	for _, update := range updates {
		if update.GetRunCompleted() != nil {
			t.Fatalf("unexpected run completion after uncertain call: %+v", update)
		}
	}
	if len(beacons) != 2 ||
		beacons[0].GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE ||
		beacons[1].GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER ||
		beacons[1].GetStatus() != turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED ||
		beacons[1].GetError().GetCode() != "mcp_call_failed" {
		t.Fatalf("beacons = %+v, want BEFORE then AFTER FAILED mcp_call_failed", beacons)
	}
}

func TestExecuteRecoversThroughModelAfterSafeMCPCallFailure(t *testing.T) {
	callErr := errors.New("safe read unavailable")
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "provider_call", Name: "system.read"}}}},
		{{Type: "delta", Text: "recovered"}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.read"}},
		callFunc: func(context.Context, string, map[string]any) (map[string]any, error) {
			return nil, callErr
		},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{
			SystemMCP: client,
			Runner:    &tools.Runner{PostBeacon: allowToolCall},
		},
	)

	updates := collectUpdates(t, assistant, testJob())

	if len(provider.requests) != 2 {
		t.Fatalf("provider requests = %d, want recovery model call", len(provider.requests))
	}
	toolResult := provider.requests[1].Messages[len(provider.requests[1].Messages)-1]
	if toolResult.Role != "tool" || !strings.Contains(toolResult.Content, callErr.Error()) {
		t.Fatalf("tool result = %+v, want recoverable safe failure", toolResult)
	}
	if completed := updates[len(updates)-1].GetRunCompleted(); completed == nil || completed.Content != "recovered" {
		t.Fatalf("terminal update = %+v, want recovered completion", updates[len(updates)-1])
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
			return approvalToolCall(beacon), nil
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

	if err == nil || !tools.RunWasTerminalized(err) {
		t.Fatalf("Execute error = %T %v, want terminalized-run signal", err, err)
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

func TestExecuteStopsOnApprovalWaitFailures(t *testing.T) {
	tests := []struct {
		name string
		wait func(context.Context, string) (string, error)
	}{
		{
			name: "wait error",
			wait: func(context.Context, string) (string, error) {
				return "", errors.New("approval service unavailable")
			},
		},
		{name: "missing waiter"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var beacons []*turingv1.ToolCallBeacon
			runner := &tools.Runner{
				PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
					beacons = append(beacons, beacon)
					return approvalToolCall(beacon), nil
				},
				WaitApproval: test.wait,
			}
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

			if err == nil || !strings.Contains(fmt.Sprintf("%T", err), "ApprovalWaitError") {
				t.Fatalf("Execute error = %T %v, want ApprovalWaitError", err, err)
			}
			if len(provider.requests) != 1 {
				t.Fatalf("provider requests = %d, want no continuation", len(provider.requests))
			}
			if len(client.calls) != 0 {
				t.Fatalf("MCP calls = %d, want none before approval", len(client.calls))
			}
			for _, update := range updates {
				if update.GetRunCompleted() != nil {
					t.Fatalf("unexpected completion after approval wait failure: %+v", update)
				}
			}
			if len(beacons) != 2 ||
				beacons[0].GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE ||
				beacons[1].GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER ||
				beacons[1].GetStatus() != turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED ||
				beacons[1].GetError().GetCode() != "approval_wait_failed" {
				t.Fatalf("beacons = %+v, want BEFORE then AFTER FAILED approval_wait_failed", beacons)
			}
		})
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
	firstModelID := threaded[1].ToolCalls[0].ID
	secondModelID := threaded[1].ToolCalls[1].ID
	if firstModelID != "call_a" ||
		secondModelID != "call_b" ||
		threaded[2].Role != "tool" || threaded[2].ToolCallID != firstModelID ||
		threaded[3].Role != "tool" || threaded[3].ToolCallID != secondModelID {
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
		return &turingv1.ToolPolicyDecision{
			Decision:   turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED,
			ApprovalId: "approval_1",
			ToolCallId: beacon.GetToolCallId(),
		}, nil
	}, WaitApproval: func(context.Context, string) (string, error) { return "token", nil }}
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

func TestExecuteReturnsRunnerReportingFailureWithoutModelRecovery(t *testing.T) {
	reportErr := errors.New("failed beacon unavailable")
	runner := &tools.Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
			return nil, reportErr
		}
		return allowToolCall(context.Background(), beacon)
	}}
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "provider_call", Name: "system.read"}}}},
		{{Type: "delta", Text: "must not run"}},
	}}
	callErr := errors.New("MCP unavailable")
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.read"}},
		callFunc: func(context.Context, string, map[string]any) (map[string]any, error) {
			return nil, callErr
		},
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

	var reporting tools.ReportingFailureError
	if !errors.As(err, &reporting) || !errors.Is(err, reportErr) {
		t.Fatalf("Execute error = %T %v, want runner ReportingFailureError", err, err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want no model recovery", len(provider.requests))
	}
	if len(client.calls) != 1 {
		t.Fatalf("MCP calls = %d, want one failed call", len(client.calls))
	}
	for _, update := range updates {
		if update.GetRunFailed() != nil || update.GetRunCompleted() != nil {
			t.Fatalf("assistant emitted terminal runtime update: %+v", update)
		}
	}
}

func TestExecuteStopsAfterPostedBeforeBeaconDecisionFailure(t *testing.T) {
	waitErr := agentBeaconPostedTestError{err: context.DeadlineExceeded}
	var beacons []*turingv1.ToolCallBeacon
	ctx, cancel := context.WithCancel(context.Background())
	runner := &tools.Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
		beacons = append(beacons, beacon)
		if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE {
			cancel()
			return nil, waitErr
		}
		return allowToolCall(context.Background(), beacon)
	}}
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "provider_call", Name: "system.read"}}}},
		{{Type: "delta", Text: "must not run"}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.read"}},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: runner},
	)
	var updates []*turingv1.RuntimeUpdate

	err := assistant.Execute(ctx, testJob(), func(update *turingv1.RuntimeUpdate) error {
		updates = append(updates, update)
		return nil
	})

	if !errors.Is(err, context.DeadlineExceeded) || !tools.BeaconWasPosted(err) || !tools.ReportingFailed(err) {
		t.Fatalf("Execute error = %T %v, want posted reporting error wrapping deadline exceeded", err, err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests = %d, want no model recovery", len(provider.requests))
	}
	if len(client.calls) != 0 {
		t.Fatalf("MCP calls = %d, want 0", len(client.calls))
	}
	if len(beacons) != 2 ||
		beacons[0].GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE ||
		beacons[1].GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER ||
		beacons[1].GetStatus() != turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED ||
		beacons[1].GetError().GetCode() != "tool_policy_decision_failed" {
		t.Fatalf("beacons = %+v, want BEFORE then failed AFTER beacon", beacons)
	}
	for _, update := range updates {
		if update.GetRunFailed() != nil || update.GetRunCompleted() != nil {
			t.Fatalf("assistant emitted terminal runtime update: %+v", update)
		}
		if event := update.GetEvent(); event != nil {
			switch event.GetType() {
			case turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED,
				turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED,
				turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED:
				t.Fatalf("assistant synthesized tool lifecycle after beacon failure: %+v", event)
			}
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
			runner: &tools.Runner{PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
				return &turingv1.ToolPolicyDecision{
					Decision:   turingv1.ToolPolicyDecision_DECISION_DENY,
					Reason:     "blocked",
					ToolCallId: beacon.GetToolCallId(),
				}, nil
			}},
			wantError: "tool denied: blocked",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &queuedProvider{responses: [][]llm.StreamEvent{
				{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "call_recover", Name: "system.requested"}}}},
				{{Type: "delta", Text: "recovered"}},
			}}
			client := &assistantTestToolLister{definitions: test.definitions}
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
			// Unmarshal into map[string]any because the unknown-tool payload
			// carries more than the flat {"error": string} the other cases do
			// (it also lists available tools, an array).
			var result map[string]any
			if err := json.Unmarshal([]byte(resultMessage.Content), &result); err != nil {
				t.Fatalf("tool error result is not JSON: %q: %v", resultMessage.Content, err)
			}
			errText, _ := result["error"].(string)
			if !strings.Contains(errText, test.wantError) {
				t.Fatalf("tool error = %q, want substring %q", errText, test.wantError)
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

// unknownToolErrorPayload mirrors the JSON an unknown-tool result carries back to
// the model. Decoding into it also proves the content is valid JSON.
type unknownToolErrorPayload struct {
	Error          string   `json:"error"`
	RejectedTool   string   `json:"rejectedTool"`
	AvailableTools []string `json:"availableTools"`
	Truncated      bool     `json:"availableToolsTruncated"`
	TotalAvailable int      `json:"totalAvailableTools"`
}

func TestExecuteUnknownToolErrorNamesRejectedToolAndListsAvailable(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "call_bad", Name: "systm.tyme"}}}},
		{{Type: "delta", Text: "recovered"}},
	}}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.time"}, {"name": "files.create"}},
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}},
	)

	updates := collectUpdates(t, assistant, testJob())

	// An unknown name must not abort: the enriched error is the model's chance to
	// correct itself, so the loop threads it back and keeps going.
	if len(provider.requests) != 2 {
		t.Fatalf("provider requests = %d, want unknown-tool turn then recovery turn", len(provider.requests))
	}
	if completed := updates[len(updates)-1].GetRunCompleted(); completed == nil || completed.Content != "recovered" {
		t.Fatalf("terminal update = %+v, want recovered completion", updates[len(updates)-1])
	}

	resultMessage := provider.requests[1].Messages[2]
	if resultMessage.Role != "tool" {
		t.Fatalf("threaded tool result role = %q, want tool", resultMessage.Role)
	}
	var payload unknownToolErrorPayload
	if err := json.Unmarshal([]byte(resultMessage.Content), &payload); err != nil {
		t.Fatalf("unknown-tool result is not valid JSON: %q: %v", resultMessage.Content, err)
	}
	if payload.Error != "unknown_tool" {
		t.Fatalf("error = %q, want unknown_tool", payload.Error)
	}
	if payload.RejectedTool != "systm.tyme" {
		t.Fatalf("rejectedTool = %q, want the rejected name systm.tyme", payload.RejectedTool)
	}
	wantAvailable := map[string]bool{"system.time": true, "files.create": true}
	if len(payload.AvailableTools) != len(wantAvailable) {
		t.Fatalf("availableTools = %v, want the two registered tools", payload.AvailableTools)
	}
	for _, name := range payload.AvailableTools {
		if !wantAvailable[name] {
			t.Fatalf("availableTools = %v, contains unexpected %q", payload.AvailableTools, name)
		}
	}
	if payload.Truncated {
		t.Fatalf("availableToolsTruncated = true, want false for a small registry")
	}
	if payload.TotalAvailable != 0 {
		t.Fatalf("totalAvailableTools = %d, want it omitted (0) when the list is not truncated", payload.TotalAvailable)
	}
}

func TestExecuteUnknownToolErrorBoundsAvailableToolListAndFlagsTruncation(t *testing.T) {
	// Exercise the boundary directly: at exactly the cap nothing is truncated;
	// one past the cap the list is clipped and the truncation is reported.
	for _, test := range []struct {
		name          string
		registered    int
		wantListed    int
		wantTruncated bool
		wantTotal     int
	}{
		{name: "exact cap", registered: maxUnknownToolListing, wantListed: maxUnknownToolListing},
		{
			name:          "one past cap",
			registered:    maxUnknownToolListing + 1,
			wantListed:    maxUnknownToolListing,
			wantTruncated: true,
			wantTotal:     maxUnknownToolListing + 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			definitions := make([]map[string]any, test.registered)
			for index := range definitions {
				definitions[index] = map[string]any{"name": fmt.Sprintf("system.tool_%03d", index)}
			}
			provider := &queuedProvider{responses: [][]llm.StreamEvent{
				{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "call_bad", Name: "system.missing"}}}},
				{{Type: "delta", Text: "recovered"}},
			}}
			client := &assistantTestToolLister{definitions: definitions}
			assistant := NewGeneralAssistant(
				map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
				fakeMessageClient{},
				&GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}},
			)

			updates := collectUpdates(t, assistant, testJob())

			if completed := updates[len(updates)-1].GetRunCompleted(); completed == nil || completed.Content != "recovered" {
				t.Fatalf("terminal update = %+v, want recovered completion", updates[len(updates)-1])
			}
			resultMessage := provider.requests[1].Messages[2]
			var payload unknownToolErrorPayload
			if err := json.Unmarshal([]byte(resultMessage.Content), &payload); err != nil {
				t.Fatalf("unknown-tool result is not valid JSON: %q: %v", resultMessage.Content, err)
			}
			if len(payload.AvailableTools) != test.wantListed {
				t.Fatalf("availableTools length = %d, want %d", len(payload.AvailableTools), test.wantListed)
			}
			if payload.Truncated != test.wantTruncated {
				t.Fatalf("availableToolsTruncated = %t, want %t", payload.Truncated, test.wantTruncated)
			}
			if payload.TotalAvailable != test.wantTotal {
				t.Fatalf("totalAvailableTools = %d, want %d", payload.TotalAvailable, test.wantTotal)
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

	modelCall := provider.requests[1].Messages[1].ToolCalls[0]
	toolResult := provider.requests[1].Messages[2]
	if modelCall.ID != "provider_id" || toolResult.ToolCallID != modelCall.ID {
		t.Fatalf("model linkage = call %q result %q, want preserved provider ID", modelCall.ID, toolResult.ToolCallID)
	}
	if before == nil ||
		before.ToolCallId == "" ||
		before.ToolCallId == modelCall.ID ||
		before.ModelToolCallId != modelCall.ID ||
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
	for index := range runCount {
		index := index
		go func() {
			provider := &queuedProvider{responses: [][]llm.StreamEvent{
				{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "../../provider_reused", Name: "system.time"}}}},
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
			job := testJob()
			job.RunId = fmt.Sprintf("run_%d", index)
			err := assistant.Execute(context.Background(), job, discardUpdate)
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
		if got.id == "" || got.id == "../../provider_reused" {
			t.Fatalf("normalized ID = %q, want fresh runtime ID", got.id)
		}
		if _, exists := seen[got.id]; exists {
			t.Fatalf("duplicate runtime tool-call ID %q", got.id)
		}
		seen[got.id] = struct{}{}
	}
}

func TestExecuteGeneratesRetryStableToolCallIDsAcrossAssistantRestarts(t *testing.T) {
	execute := func(runID string) string {
		t.Helper()
		provider := &queuedProvider{responses: [][]llm.StreamEvent{
			{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: "../../provider-secret", Name: "system.time"}}}},
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
		job := testJob()
		job.RunId = runID
		if err := assistant.Execute(context.Background(), job, discardUpdate); err != nil {
			t.Fatalf("Execute(%q) returned error: %v", runID, err)
		}
		call := provider.requests[1].Messages[1].ToolCalls[0]
		if provider.requests[1].Messages[2].ToolCallID != call.ID {
			t.Fatalf("tool result ID = %q, want call ID %q", provider.requests[1].Messages[2].ToolCallID, call.ID)
		}
		return call.ID
	}

	first := execute("globally_unique_run_1")
	retry := execute("globally_unique_run_1")
	otherRun := execute("globally_unique_run_2")

	if first != retry {
		t.Fatalf("same run retry IDs = %q and %q, want stable ID", first, retry)
	}
	if first == otherRun {
		t.Fatalf("different run IDs generated the same tool ID %q", first)
	}
	if !strings.HasPrefix(first, "call_") || len(first) > 64 || strings.Contains(first, "provider-secret") {
		t.Fatalf("normalized ID = %q, want bounded opaque call_ ID", first)
	}
}

func TestExecuteGeneratesUniqueIDsForEmptyProviderToolCallIDs(t *testing.T) {
	provider := &queuedProvider{responses: [][]llm.StreamEvent{
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{
			{Name: "system.first"},
			{Name: "system.second"},
		}}},
		{{Type: "tool_call", ToolCalls: []llm.ToolCall{{Name: "system.third"}}}},
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
	thirdID := provider.requests[2].Messages[4].ToolCalls[0].ID
	if firstID == "" || secondID == "" || thirdID == "" ||
		firstID == secondID || firstID == thirdID || secondID == thirdID {
		t.Fatalf("normalized IDs = %q, %q, %q; want unique nonempty IDs", firstID, secondID, thirdID)
	}
	if firstTurn[2].ToolCallID != firstID || firstTurn[3].ToolCallID != secondID ||
		provider.requests[2].Messages[5].ToolCallID != thirdID {
		t.Fatalf("tool result linkage does not match assistant call IDs: %+v", provider.requests[2].Messages)
	}
}

func TestExecutePreservesValidUniqueProviderToolCallID(t *testing.T) {
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
	if calls[0].ID != "runtime_call_1" {
		t.Fatalf("first model linkage ID = %q, want preserved provider ID", calls[0].ID)
	}
	if calls[1].ID == "" || calls[1].ID == calls[0].ID {
		t.Fatalf("model linkage IDs = %q, %q; want unique", calls[0].ID, calls[1].ID)
	}
}

func TestExecuteSynthesizedToolCallIDDependsOnToolAndCanonicalArguments(t *testing.T) {
	execute := func(name string, arguments map[string]any) string {
		t.Helper()
		provider := &queuedProvider{responses: [][]llm.StreamEvent{
			{{Type: "tool_call", ToolCalls: []llm.ToolCall{{
				ID: "../../invalid", Name: name, Arguments: arguments,
			}}}},
			{{Type: "delta", Text: "done"}},
		}}
		client := &assistantTestToolLister{
			definitions: []map[string]any{{"name": name}},
			result:      map[string]any{"ok": true},
		}
		assistant := NewGeneralAssistant(
			map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
			fakeMessageClient{},
			&GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}},
		)
		collectUpdates(t, assistant, testJob())
		return provider.requests[1].Messages[1].ToolCalls[0].ID
	}

	first := execute("system.first", map[string]any{"a": 1, "b": 2})
	sameCanonical := execute("system.first", map[string]any{"b": 2, "a": 1})
	differentTool := execute("system.second", map[string]any{"a": 1, "b": 2})
	differentArgs := execute("system.first", map[string]any{"a": 1, "b": 3})
	if first != sameCanonical {
		t.Fatalf("canonical-equivalent args generated %q and %q", first, sameCanonical)
	}
	if first == differentTool || first == differentArgs || differentTool == differentArgs {
		t.Fatalf("IDs do not distinguish tool/args: first=%q tool=%q args=%q", first, differentTool, differentArgs)
	}
}

func TestExecuteSynthesizesInvalidAndOverlongProviderToolCallIDs(t *testing.T) {
	invalidIDs := []string{"", "contains space", "line\nbreak", "../../unsafe", strings.Repeat("x", 129)}
	for _, providerID := range invalidIDs {
		t.Run(fmt.Sprintf("%q", providerID), func(t *testing.T) {
			provider := &queuedProvider{responses: [][]llm.StreamEvent{
				{{Type: "tool_call", ToolCalls: []llm.ToolCall{{ID: providerID, Name: "system.time"}}}},
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
			collectUpdates(t, assistant, testJob())
			got := provider.requests[1].Messages[1].ToolCalls[0].ID
			if got == "" || got == providerID || len(got) > 128 {
				t.Fatalf("normalized ID = %q from provider ID %q", got, providerID)
			}
		})
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

func TestExecuteReturnsContextErrorWithoutRunFailedWhenMessageFetchFailsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assistant := NewGeneralAssistant(nil, fakeMessageClient{err: errors.New("fetch interrupted")}, nil)
	var updates []*turingv1.RuntimeUpdate

	err := assistant.Execute(ctx, testJob(), func(update *turingv1.RuntimeUpdate) error {
		updates = append(updates, update)
		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context.Canceled", err)
	}
	if len(updates) != 0 {
		t.Fatalf("updates = %+v, want none", updates)
	}
}

func TestExecuteClassifiesTypedMessageFetchFailures(t *testing.T) {
	tests := []struct {
		code      codes.Code
		retryable bool
	}{
		{code: codes.Unavailable, retryable: true},
		{code: codes.DeadlineExceeded, retryable: true},
		{code: codes.ResourceExhausted, retryable: true},
		{code: codes.Aborted, retryable: true},
		{code: codes.Unauthenticated, retryable: false},
		{code: codes.PermissionDenied, retryable: false},
		{code: codes.InvalidArgument, retryable: false},
		{code: codes.NotFound, retryable: false},
		{code: codes.FailedPrecondition, retryable: false},
		{code: codes.Canceled, retryable: false},
		{code: codes.Unimplemented, retryable: false},
	}
	for _, test := range tests {
		t.Run(test.code.String(), func(t *testing.T) {
			fetchErr := status.Error(test.code, "fetch failed")
			assistant := NewGeneralAssistant(nil, fakeMessageClient{err: fetchErr}, nil)

			updates := collectUpdates(t, assistant, testJob())

			failed := updates[len(updates)-1].GetRunFailed()
			if failed == nil || failed.Code != "message_fetch_failed" || failed.Retryable != test.retryable {
				t.Fatalf("terminal update = %+v, want retryable=%t message_fetch_failed", updates[len(updates)-1], test.retryable)
			}
		})
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

func TestExecuteClassifiesTypedProviderStreamStartFailures(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "unsupported JSON type",
			err:       &json.UnsupportedTypeError{Type: reflect.TypeOf(make(chan int))},
			retryable: false,
		},
		{
			name:      "unsupported JSON value",
			err:       &json.UnsupportedValueError{Value: reflect.ValueOf(1), Str: "unsupported"},
			retryable: false,
		},
		{
			name:      "malformed URL",
			err:       &url.Error{Op: "parse", URL: "://bad", Err: errors.New("missing protocol scheme")},
			retryable: false,
		},
		{
			name:      "temporary network error",
			err:       &net.DNSError{Err: "temporary", Name: "provider.test", IsTemporary: true},
			retryable: true,
		},
		{
			name:      "timeout network error",
			err:       &net.DNSError{Err: "timeout", Name: "provider.test", IsTimeout: true},
			retryable: true,
		},
		{
			name:      "generic untyped error",
			err:       errors.New("stream unavailable"),
			retryable: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assistant := NewGeneralAssistant(
				map[turingv1.ModelProvider]llm.Provider{
					turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: errorProvider{err: test.err},
				},
				fakeMessageClient{},
				nil,
			)

			updates := collectUpdates(t, assistant, testJob())

			failed := updates[len(updates)-1].GetRunFailed()
			if failed == nil || failed.Code != "model_stream_failed" || failed.Retryable != test.retryable {
				t.Fatalf("terminal update = %+v, want retryable=%t model_stream_failed", updates[len(updates)-1], test.retryable)
			}
		})
	}
}

func TestExecuteReturnsSynchronousProviderContextCancellation(t *testing.T) {
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: errorProvider{err: context.Canceled},
		},
		fakeMessageClient{},
		nil,
	)

	err := assistant.Execute(context.Background(), testJob(), discardUpdate)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context.Canceled", err)
	}
}

func TestExecuteMakesTypedTransientProviderFailureNonRetryableAfterSideEffect(t *testing.T) {
	provider := &toolThenModelFailureProvider{
		synchronous: true,
		startErr:    &net.DNSError{Err: "temporary", Name: "provider.test", IsTemporary: true},
	}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.write"}},
		result:      map[string]any{"ok": true},
	}
	runner := &tools.Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			return approvalToolCall(beacon), nil
		},
		WaitApproval: func(context.Context, string) (string, error) { return "token", nil },
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{SystemMCP: client, Runner: runner},
	)

	updates := collectUpdates(t, assistant, testJob())

	failed := updates[len(updates)-1].GetRunFailed()
	if failed == nil || failed.Code != "model_stream_failed" || failed.Retryable {
		t.Fatalf("terminal update = %+v, want side-effect override retryable=false", updates[len(updates)-1])
	}
}

func TestExecuteMakesLaterModelFailuresNonRetryableOnlyAfterApprovalGatedSuccess(t *testing.T) {
	tests := []struct {
		name            string
		synchronous     bool
		approvalGated   bool
		wantCode        string
		wantMessagePart string
		retryable       bool
	}{
		{
			name:            "safe synchronous stream start",
			synchronous:     true,
			wantCode:        "model_stream_failed",
			wantMessagePart: "stream unavailable",
			retryable:       true,
		},
		{
			name:            "safe streamed transport",
			wantCode:        "model_stream_error",
			wantMessagePart: "stream interrupted",
			retryable:       true,
		},
		{
			name:            "approval synchronous stream start",
			synchronous:     true,
			approvalGated:   true,
			wantCode:        "model_stream_failed",
			wantMessagePart: "stream unavailable",
		},
		{
			name:            "approval streamed transport",
			approvalGated:   true,
			wantCode:        "model_stream_error",
			wantMessagePart: "stream interrupted",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &toolThenModelFailureProvider{synchronous: test.synchronous}
			client := &assistantTestToolLister{
				definitions: []map[string]any{{"name": "system.write"}},
				result:      map[string]any{"ok": true},
			}
			post := allowToolCall
			runner := &tools.Runner{PostBeacon: post}
			if test.approvalGated {
				runner.PostBeacon = func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
					return approvalToolCall(beacon), nil
				}
				runner.WaitApproval = func(context.Context, string) (string, error) { return "token", nil }
			}
			assistant := NewGeneralAssistant(
				map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
				fakeMessageClient{},
				&GeneralAssistantTools{SystemMCP: client, Runner: runner},
			)

			updates := collectUpdates(t, assistant, testJob())

			failed := updates[len(updates)-1].GetRunFailed()
			if failed == nil || failed.Code != test.wantCode || failed.Retryable != test.retryable || !strings.Contains(failed.Message, test.wantMessagePart) {
				t.Fatalf("terminal update = %+v, want retryable=%t %s", updates[len(updates)-1], test.retryable, test.wantCode)
			}
			if len(client.calls) != 1 {
				t.Fatalf("MCP calls = %d, want one successful side effect", len(client.calls))
			}
		})
	}
}

func TestExecuteTimesOutProviderThatNeverClosesAndCancelsModelContext(t *testing.T) {
	provider := &neverClosingProvider{contextSeen: make(chan context.Context, 1)}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{ModelTimeout: 10 * time.Millisecond},
	)

	updates := collectUpdates(t, assistant, testJob())

	failed := updates[len(updates)-1].GetRunFailed()
	if failed == nil || failed.Code != "model_timeout" || !failed.Retryable {
		t.Fatalf("terminal update = %+v, want retryable model_timeout", updates[len(updates)-1])
	}
	modelCtx := <-provider.contextSeen
	select {
	case <-modelCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("model context was not canceled promptly")
	}
}

func TestExecuteModelTimeoutIsNonRetryableAfterApprovalGatedSuccess(t *testing.T) {
	provider := &toolThenNeverClosingProvider{}
	client := &assistantTestToolLister{
		definitions: []map[string]any{{"name": "system.write"}},
		result:      map[string]any{"ok": true},
	}
	runner := &tools.Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			return approvalToolCall(beacon), nil
		},
		WaitApproval: func(context.Context, string) (string, error) { return "token", nil },
	}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{
			SystemMCP:    client,
			Runner:       runner,
			ModelTimeout: 10 * time.Millisecond,
		},
	)

	updates := collectUpdates(t, assistant, testJob())

	failed := updates[len(updates)-1].GetRunFailed()
	if failed == nil || failed.Code != "model_timeout" || failed.Retryable {
		t.Fatalf("terminal update = %+v, want non-retryable model_timeout", updates[len(updates)-1])
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

func TestExecuteDebugToolUsesWholeToolTimeoutAndReportsAfter(t *testing.T) {
	var beacons []*turingv1.ToolCallBeacon
	runner := &tools.Runner{
		PostBeacon: func(_ context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
			beacons = append(beacons, beacon)
			if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
				return allowToolCall(context.Background(), beacon)
			}
			return approvalToolCall(beacon), nil
		},
		WaitApproval: func(ctx context.Context, _ string) (string, error) {
			if _, ok := ctx.Deadline(); !ok {
				return "", errors.New("debug tool did not receive a whole-tool deadline")
			}
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
	client := &assistantTestToolLister{definitions: []map[string]any{{"name": "files.create"}}}
	assistant := NewGeneralAssistant(
		nil,
		fakeMessageClient{},
		&GeneralAssistantTools{
			FilesMCP:         client,
			Runner:           runner,
			ToolTimeout:      time.Second,
			TotalToolTimeout: 10 * time.Millisecond,
		},
	)
	job := testJob()
	job.UserText = "/tool files.create"

	err := assistant.Execute(context.Background(), job, discardUpdate)

	if !errors.Is(err, context.DeadlineExceeded) || !tools.ApprovalWaitFailed(err) {
		t.Fatalf("Execute error = %T %v, want whole-tool approval deadline", err, err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("MCP calls = %d, want none after approval timeout", len(client.calls))
	}
	if len(beacons) != 2 ||
		beacons[0].GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE ||
		beacons[1].GetPhase() != turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER ||
		beacons[1].GetStatus() != turingv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED ||
		beacons[1].GetError().GetCode() != "approval_wait_failed" {
		t.Fatalf("beacons = %+v, want BEFORE then failed AFTER", beacons)
	}
}

func TestExecuteDebugToolRejectsTypedNilMCPClient(t *testing.T) {
	for _, test := range []struct {
		name     string
		userText string
		toolset  func(ToolLister) *GeneralAssistantTools
	}{
		{
			name:     "system",
			userText: "/tool system.time",
			toolset: func(client ToolLister) *GeneralAssistantTools {
				return &GeneralAssistantTools{SystemMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}}
			},
		},
		{
			name:     "files",
			userText: "/tool files.create",
			toolset: func(client ToolLister) *GeneralAssistantTools {
				return &GeneralAssistantTools{FilesMCP: client, Runner: &tools.Runner{PostBeacon: allowToolCall}}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var nilClient *assistantTestToolLister
			assistant := NewGeneralAssistant(nil, fakeMessageClient{}, test.toolset(nilClient))
			job := testJob()
			job.UserText = test.userText

			updates := collectUpdates(t, assistant, job)

			failed := updates[len(updates)-1].GetRunFailed()
			if failed == nil || failed.Code != "tool_call_failed" || failed.Retryable {
				t.Fatalf("terminal update = %+v, want non-retryable tool_call_failed", updates[len(updates)-1])
			}
		})
	}
}
func TestGeneralAssistantStreamsDeltasAndCompletesRun(t *testing.T) {
	provider := &scriptedProvider{events: []llm.StreamEvent{{Type: "delta", Text: "Hel"}, {Type: "delta", Text: "lo"}, {Type: "completed", FinishReason: "stop"}}}
	var fetchedSessionID string
	var fetchedBeforeMessageID string
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{
			messages: []llm.ChatMessage{{Role: "system", Content: "Be helpful"}},
			onFetch: func(sessionID string, beforeMessageID string) {
				fetchedSessionID = sessionID
				fetchedBeforeMessageID = beforeMessageID
			},
		},
		nil,
	)
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
	if fetchedSessionID != "sess_1" || fetchedBeforeMessageID != "msg_user" {
		t.Fatalf("FetchMessages(sessionID, before_message_id) = (%q, %q), want (%q, %q)", fetchedSessionID, fetchedBeforeMessageID, "sess_1", "msg_user")
	}
	wantMessages := []llm.ChatMessage{
		{Role: "system", Content: "Be helpful"},
		{Role: "user", Content: "hi"},
	}
	if len(provider.requests) != 1 || !reflect.DeepEqual(provider.requests[0].Messages, wantMessages) {
		t.Fatalf("provider requests = %+v, want messages %+v", provider.requests, wantMessages)
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
		{code: "model_timeout", retryable: true},
		{code: "model_quota_exceeded", retryable: false},
		{code: "model_auth_failed", retryable: false},
		{code: "model_request_failed", retryable: false},
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
	onFetch  func(sessionID string, beforeMessageID string)
}

func (c fakeMessageClient) FetchMessages(ctx context.Context, sessionID string, beforeMessageID string) ([]llm.ChatMessage, error) {
	if c.onFetch != nil {
		c.onFetch(sessionID, beforeMessageID)
	}
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

type assistantClassifiedListError struct {
	message   string
	retryable bool
}

func (e assistantClassifiedListError) Error() string   { return e.message }
func (e assistantClassifiedListError) Retryable() bool { return e.retryable }

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

func approvalToolCall(beacon *turingv1.ToolCallBeacon) *turingv1.ToolPolicyDecision {
	decision := turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED
	if beacon.GetPhase() == turingv1.ToolCallPhase_TOOL_CALL_PHASE_AFTER {
		decision = turingv1.ToolPolicyDecision_DECISION_ALLOW
	}
	return &turingv1.ToolPolicyDecision{
		Decision:   decision,
		ApprovalId: "approval_1",
		ToolCallId: beacon.GetToolCallId(),
	}
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

func toolResultWithSerializedSize(t *testing.T, size int) map[string]any {
	t.Helper()
	const overhead = len(`{"data":""}`)
	if size < overhead {
		t.Fatalf("serialized result size %d is below overhead %d", size, overhead)
	}
	result := map[string]any{"data": strings.Repeat("x", size-overhead)}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != size {
		t.Fatalf("serialized result size = %d, want %d", len(data), size)
	}
	return result
}

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

type silentLoopingToolProvider struct {
	calls int
}

type whitespaceLoopingToolProvider struct {
	calls int
}

func (p *whitespaceLoopingToolProvider) ID() string { return "whitespace-looping" }

func (p *whitespaceLoopingToolProvider) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	p.calls++
	out := make(chan llm.StreamEvent, 2)
	out <- llm.StreamEvent{Type: "delta", Text: " \n\t"}
	out <- llm.StreamEvent{Type: "tool_call", ToolCalls: []llm.ToolCall{{
		ID: fmt.Sprintf("call_%d", p.calls), Name: "system.repeat",
	}}}
	close(out)
	return out, nil
}

func (p *silentLoopingToolProvider) ID() string { return "silent-looping" }

func (p *silentLoopingToolProvider) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	p.calls++
	out := make(chan llm.StreamEvent, 1)
	out <- llm.StreamEvent{Type: "tool_call", ToolCalls: []llm.ToolCall{{
		ID: fmt.Sprintf("call_%d", p.calls), Name: "system.repeat",
	}}}
	close(out)
	return out, nil
}

type toolThenModelFailureProvider struct {
	calls       int
	synchronous bool
	startErr    error
}

func (p *toolThenModelFailureProvider) ID() string { return "tool-then-failure" }

func (p *toolThenModelFailureProvider) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	p.calls++
	if p.calls == 1 {
		out := make(chan llm.StreamEvent, 1)
		out <- llm.StreamEvent{Type: "tool_call", ToolCalls: []llm.ToolCall{{
			ID: "provider_call", Name: "system.write",
		}}}
		close(out)
		return out, nil
	}
	if p.synchronous {
		if p.startErr != nil {
			return nil, p.startErr
		}
		return nil, errors.New("stream unavailable")
	}
	out := make(chan llm.StreamEvent, 1)
	out <- llm.StreamEvent{Type: "error", Code: "model_stream_error", Message: "stream interrupted"}
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

type neverClosingProvider struct {
	contextSeen chan context.Context
}

func (p *neverClosingProvider) ID() string { return "never-closing" }

func (p *neverClosingProvider) StreamChat(ctx context.Context, _ llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	p.contextSeen <- ctx
	return make(chan llm.StreamEvent), nil
}

type toolThenNeverClosingProvider struct {
	calls int
}

func (p *toolThenNeverClosingProvider) ID() string { return "tool-then-never-closing" }

func (p *toolThenNeverClosingProvider) StreamChat(context.Context, llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	p.calls++
	if p.calls == 1 {
		out := make(chan llm.StreamEvent, 1)
		out <- llm.StreamEvent{Type: "tool_call", ToolCalls: []llm.ToolCall{{
			ID: "provider_call", Name: "system.write",
		}}}
		close(out)
		return out, nil
	}
	return make(chan llm.StreamEvent), nil
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

type agentBeaconPostedTestError struct {
	err error
}

func (e agentBeaconPostedTestError) Error() string      { return e.err.Error() }
func (e agentBeaconPostedTestError) Unwrap() error      { return e.err }
func (e agentBeaconPostedTestError) BeaconPosted() bool { return true }

// capturingProvider records the messages it was asked to complete, so a test can
// assert what actually reached the model.
type capturingProvider struct {
	seen []llm.ChatMessage
}

func (p *capturingProvider) ID() string { return "ollama" }

func (p *capturingProvider) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	p.seen = append([]llm.ChatMessage{}, req.Messages...)
	out := make(chan llm.StreamEvent, 2)
	out <- llm.StreamEvent{Type: "delta", Text: "done"}
	out <- llm.StreamEvent{Type: "completed", FinishReason: "stop"}
	close(out)
	return out, nil
}

type fakeRecaller struct {
	block     llm.ChatMessage
	ok        bool
	sessionID string
	userText  string
	inContext []llm.ChatMessage
	callCount int
}

func (r *fakeRecaller) Recall(_ context.Context, sessionID string, userText string, inContext []llm.ChatMessage) (llm.ChatMessage, bool) {
	r.callCount++
	r.sessionID, r.userText = sessionID, userText
	r.inContext = append([]llm.ChatMessage{}, inContext...)
	return r.block, r.ok
}

func TestExecutePrependsRecalledContext(t *testing.T) {
	provider := &capturingProvider{}
	recaller := &fakeRecaller{block: llm.ChatMessage{Role: "system", Content: "recalled material"}, ok: true}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{Recall: recaller},
	)

	collectUpdates(t, assistant, testJob())

	if recaller.callCount != 1 {
		t.Fatalf("Recall called %d times, want 1", recaller.callCount)
	}
	if len(provider.seen) == 0 {
		t.Fatal("provider saw no messages")
	}
	// Prepended, not appended: recalled material must sit before the live
	// conversation so it cannot be read as the user's latest turn.
	if provider.seen[0].Role != "system" || provider.seen[0].Content != "recalled material" {
		t.Fatalf("first message = %+v, want the recalled block first", provider.seen[0])
	}
	if last := provider.seen[len(provider.seen)-1]; last.Role != "user" {
		t.Fatalf("last message = %+v, want the user's turn last", last)
	}
	// Recall needs the request as built, to know what is already in front of the
	// model; passing nil would make it exclude the whole session instead.
	if len(recaller.inContext) == 0 {
		t.Fatal("Recall was not given the request messages")
	}
}

func TestExecuteRunsWithoutARecaller(t *testing.T) {
	provider := &capturingProvider{}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{},
	)
	updates := collectUpdates(t, assistant, testJob())
	if len(updates) == 0 {
		t.Fatal("run produced no updates without a recaller configured")
	}
	for _, message := range provider.seen {
		if message.Role == "system" {
			t.Fatalf("unconfigured recall injected a system message: %+v", message)
		}
	}
}

// Recall returning nothing is the common case and must be invisible.
func TestExecuteIgnoresEmptyRecall(t *testing.T) {
	provider := &capturingProvider{}
	recaller := &fakeRecaller{ok: false}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{Recall: recaller},
	)
	collectUpdates(t, assistant, testJob())
	for _, message := range provider.seen {
		if message.Role == "system" {
			t.Fatalf("empty recall still injected a message: %+v", message)
		}
	}
}

// The worker reports this on connect, and the orchestrator turns it into its
// policy registry. It must reuse the same discovery the agent serves from —
// reporting one tool set and executing against another would let a tool run
// under a policy that was never registered for it.
func TestDiscoveredToolsSharesTheAgentRegistry(t *testing.T) {
	client := &assistantTestToolLister{definitions: []map[string]any{
		{"name": "system.time", "inputSchema": map[string]any{"type": "object"}},
	}}
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{SystemMCP: client})

	first, err := assistant.DiscoveredTools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].ToolName != "system.time" || first[0].ServerName != "system" {
		t.Fatalf("snapshot = %+v, want system/system.time", first)
	}

	// A second call must not re-list: the registry is cached, and a reconnect
	// should not re-hit every MCP server.
	if _, err := assistant.DiscoveredTools(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := client.listCalls.Load(); got != 1 {
		t.Fatalf("ListTools called %d times, want 1 (cached registry)", got)
	}
}

func TestDiscoveredToolsPropagatesFailure(t *testing.T) {
	client := &assistantTestToolLister{listErrors: []error{errors.New("mcp unreachable")}}
	assistant := NewGeneralAssistant(nil, fakeMessageClient{}, &GeneralAssistantTools{SystemMCP: client})
	if _, err := assistant.DiscoveredTools(context.Background()); err == nil {
		t.Fatal("expected discovery failure to propagate so the worker can report FAILED")
	}
}

// runStepNotes returns the "note" of every agent.run.step event, in order.
func runStepNotes(updates []*turingv1.RuntimeUpdate) []string {
	var notes []string
	for _, update := range updates {
		event := update.GetEvent()
		if event == nil || event.Type != turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP {
			continue
		}
		note, _ := event.Payload.AsMap()["note"].(string)
		notes = append(notes, note)
	}
	return notes
}

const recallNotice = "Using material recalled from earlier conversations"

// Recalled material reaching the model unattributed is what makes an answer
// read as confabulation: the user is told a fact from a conversation weeks ago
// with no indication of where it came from.
func TestExecuteAnnouncesRecalledContext(t *testing.T) {
	provider := &capturingProvider{}
	recaller := &fakeRecaller{block: llm.ChatMessage{Role: "system", Content: "recalled material"}, ok: true}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{Recall: recaller},
	)

	updates := collectUpdates(t, assistant, testJob())

	notes := runStepNotes(updates)
	if len(notes) != 1 || notes[0] != recallNotice {
		t.Fatalf("run step notes = %q, want exactly [%q]", notes, recallNotice)
	}

	// The notice explains the answer, so it must precede it in the transcript.
	noticeIndex, deltaIndex := -1, -1
	for index, update := range updates {
		event := update.GetEvent()
		if event == nil {
			continue
		}
		if noticeIndex < 0 && event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP {
			noticeIndex = index
		}
		if deltaIndex < 0 && event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA {
			deltaIndex = index
		}
	}
	if noticeIndex < 0 {
		t.Fatal("no recall notice emitted")
	}
	if deltaIndex >= 0 && noticeIndex > deltaIndex {
		t.Fatalf("recall notice at %d lands after the first delta at %d", noticeIndex, deltaIndex)
	}
}

// Recall returning nothing is the common case; a notice there would claim a
// recall that never happened.
func TestExecuteDoesNotAnnounceEmptyRecall(t *testing.T) {
	provider := &capturingProvider{}
	recaller := &fakeRecaller{ok: false}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{Recall: recaller},
	)
	if notes := runStepNotes(collectUpdates(t, assistant, testJob())); len(notes) != 0 {
		t.Fatalf("empty recall emitted notes %q, want none", notes)
	}
}

func TestExecuteDoesNotAnnounceRecallWithoutARecaller(t *testing.T) {
	provider := &capturingProvider{}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{},
	)
	if notes := runStepNotes(collectUpdates(t, assistant, testJob())); len(notes) != 0 {
		t.Fatalf("unconfigured recall emitted notes %q, want none", notes)
	}
}

// Design decision: notices introduce no new failure mode — existing error
// handling stands. A failing emit means the runtime→orchestrator stream is
// broken, so continuing would stream an answer nobody receives. Without this
// test, changing the emit to `_ = emit(...)` would pass unnoticed.
func TestExecutePropagatesRecallNoticeEmitErrors(t *testing.T) {
	emitErr := errors.New("emit failed")
	provider := &capturingProvider{}
	recaller := &fakeRecaller{block: llm.ChatMessage{Role: "system", Content: "recalled material"}, ok: true}
	assistant := NewGeneralAssistant(
		map[turingv1.ModelProvider]llm.Provider{turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: provider},
		fakeMessageClient{},
		&GeneralAssistantTools{Recall: recaller},
	)

	err := assistant.Execute(context.Background(), testJob(), func(update *turingv1.RuntimeUpdate) error {
		event := update.GetEvent()
		if event != nil && event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP {
			return emitErr
		}
		return nil
	})
	if !errors.Is(err, emitErr) {
		t.Fatalf("Execute error = %v, want %v", err, emitErr)
	}
	if len(provider.seen) != 0 {
		t.Fatalf("model was called despite a broken stream: %+v", provider.seen)
	}
}
