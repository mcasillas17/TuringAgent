package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/safejson"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
)

type MessageClient interface {
	FetchMessages(ctx context.Context, sessionID string) ([]llm.ChatMessage, error)
}

type GeneralAssistantTools struct {
	SystemMCP          ToolLister
	FilesMCP           ToolLister
	Runner             *tools.Runner
	MaxToolCallsPerRun int
}

const (
	maxToolIterations         = 5
	defaultMaxToolCallsPerRun = 10
	toolIterationFallback     = "Tool iteration limit reached before a final response."
)

var errRunTerminalized = errors.New("run already terminalized")

type GeneralAssistant struct {
	providers          map[turingv1.ModelProvider]llm.Provider
	messages           MessageClient
	tools              *GeneralAssistantTools
	maxToolCallsPerRun int

	registryMu sync.Mutex
	registry   *ToolRegistry
	discovery  *toolDiscovery
}

type toolDiscovery struct {
	done                   chan struct{}
	registry               *ToolRegistry
	err                    error
	retryAfterLeaderCancel bool
}

func NewGeneralAssistant(providers map[turingv1.ModelProvider]llm.Provider, messages MessageClient, toolset *GeneralAssistantTools) *GeneralAssistant {
	maxToolCallsPerRun := defaultMaxToolCallsPerRun
	if toolset != nil && toolset.MaxToolCallsPerRun > 0 {
		maxToolCallsPerRun = toolset.MaxToolCallsPerRun
	}
	return &GeneralAssistant{
		providers:          providers,
		messages:           messages,
		tools:              toolset,
		maxToolCallsPerRun: maxToolCallsPerRun,
	}
}

func (a *GeneralAssistant) SetToolBeaconPoster(post func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)) {
	if a.tools == nil || a.tools.Runner == nil {
		return
	}
	a.tools.Runner.PostBeacon = post
}

func (a *GeneralAssistant) Execute(ctx context.Context, job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error {
	if job == nil {
		return fmt.Errorf("job is required")
	}
	messages, err := a.messages.FetchMessages(ctx, job.GetSessionId())
	if err != nil {
		return emitRunFailed(emit, job, "message_fetch_failed", err.Error(), true)
	}
	if err := emit(messageEvent(job, turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_STARTED, map[string]any{"messageId": job.GetAssistantMessageId(), "role": "assistant"})); err != nil {
		return err
	}
	trimmed := strings.TrimSpace(job.GetUserText())
	if handled, err := a.tryDebugTool(ctx, job, trimmed, emit); handled || err != nil {
		return err
	}
	provider := a.providers[job.GetModelProvider()]
	if provider == nil {
		return emitRunFailed(emit, job, "model_provider_unavailable", fmt.Sprintf("Provider %s is not configured", job.GetModelProvider().String()), false)
	}
	registry, err := a.discoverTools(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return emitRunFailed(emit, job, "tool_discovery_failed", err.Error(), ToolDiscoveryRetryable(err))
	}
	requestMessages := append([]llm.ChatMessage{}, messages...)
	requestMessages = append(requestMessages, llm.ChatMessage{Role: "user", Content: job.GetUserText()})
	content := ""
	usedToolCallIDs := make(map[string]struct{})
	toolCallCount := 0
	successfulToolSideEffect := false
	for toolIteration := 0; ; {
		if err := ctx.Err(); err != nil {
			return err
		}
		events, err := provider.StreamChat(ctx, llm.ChatRequest{
			Model:    job.GetModel(),
			Messages: requestMessages,
			Tools:    registry.Definitions(),
		})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return emitRunFailed(emit, job, "model_stream_failed", err.Error(), !successfulToolSideEffect)
		}
		turnText := ""
		var calls []llm.ToolCall
	stream:
		for {
			var event llm.StreamEvent
			var ok bool
			select {
			case <-ctx.Done():
				return ctx.Err()
			case event, ok = <-events:
				if !ok {
					break stream
				}
			}
			switch event.Type {
			case "delta":
				turnText += event.Text
				content += event.Text
				if err := emit(messageEvent(job, turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA, map[string]any{"messageId": job.GetAssistantMessageId(), "delta": event.Text})); err != nil {
					return err
				}
			case "tool_call":
				calls = append(calls, event.ToolCalls...)
			case "error":
				code := event.Code
				if code == "" {
					code = "model_error"
				}
				message := event.Message
				if message == "" {
					message = code
				}
				return emitRunFailed(emit, job, code, message, retryableModelError(code) && !successfulToolSideEffect)
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(calls) == 0 {
			return completeRun(emit, job, content)
		}
		if toolCallCount+len(calls) > a.maxToolCallsPerRun {
			return emitRunFailed(
				emit,
				job,
				"tool_call_limit_exceeded",
				fmt.Sprintf("model requested more than %d tool calls", a.maxToolCallsPerRun),
				false,
			)
		}
		toolCallCount += len(calls)
		normalizeToolCallIDs(calls, usedToolCallIDs)
		requestMessages = append(requestMessages, llm.ChatMessage{
			Role:      "assistant",
			Content:   turnText,
			ToolCalls: calls,
		})
		for _, call := range calls {
			outcome, err := a.executeToolCall(ctx, job, emit, registry, call, &requestMessages)
			successfulToolSideEffect = successfulToolSideEffect || outcome.SuccessfulSideEffect
			if err != nil {
				if errors.Is(err, errRunTerminalized) {
					return nil
				}
				return err
			}
		}
		toolIteration++
		if toolIteration >= maxToolIterations {
			if err := emit(messageEvent(job, turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP, map[string]any{
				"note":              "maximum tool iterations reached",
				"maxToolIterations": maxToolIterations,
			})); err != nil {
				return err
			}
			if content == "" {
				if err := emit(messageEvent(job, turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA, map[string]any{
					"messageId": job.GetAssistantMessageId(),
					"delta":     toolIterationFallback,
				})); err != nil {
					return err
				}
				content = toolIterationFallback
			}
			return completeRun(emit, job, content)
		}
	}
}

func (a *GeneralAssistant) discoverTools(ctx context.Context) (*ToolRegistry, error) {
	for {
		a.registryMu.Lock()
		if a.registry != nil {
			registry := a.registry
			a.registryMu.Unlock()
			return registry, nil
		}
		if a.discovery != nil {
			discovery := a.discovery
			a.registryMu.Unlock()
			select {
			case <-discovery.done:
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if discovery.err == nil {
					return discovery.registry, nil
				}
				if discovery.retryAfterLeaderCancel {
					continue
				}
				return nil, discovery.err
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		discovery := &toolDiscovery{done: make(chan struct{})}
		a.discovery = discovery
		a.registryMu.Unlock()

		servers := make(map[string]ToolLister)
		if a.tools != nil {
			if !isNilToolLister(a.tools.SystemMCP) {
				servers["system"] = a.tools.SystemMCP
			}
			if !isNilToolLister(a.tools.FilesMCP) {
				servers["files"] = a.tools.FilesMCP
			}
		}
		registry, err := BuildToolRegistry(ctx, servers)

		a.registryMu.Lock()
		discovery.registry = registry
		discovery.err = err
		discovery.retryAfterLeaderCancel = err != nil && ctx.Err() != nil && errors.Is(err, ctx.Err())
		if err == nil {
			a.registry = registry
		}
		a.discovery = nil
		close(discovery.done)
		a.registryMu.Unlock()
		return registry, err
	}
}

func (a *GeneralAssistant) executeToolCall(
	ctx context.Context,
	job *turingv1.AgentJob,
	emit func(*turingv1.RuntimeUpdate) error,
	registry *ToolRegistry,
	call llm.ToolCall,
	messages *[]llm.ChatMessage,
) (toolCallOutcome, error) {
	if err := ctx.Err(); err != nil {
		return toolCallOutcome{}, err
	}
	entry, found := registry.Lookup(call.Name)
	if !found {
		if err := emitAssistantToolCallFailed(emit, job, call, "unknown_tool"); err != nil {
			return toolCallOutcome{}, err
		}
		return toolCallOutcome{}, appendToolError(messages, call, "unknown_tool")
	}
	if a.tools == nil || a.tools.Runner == nil {
		if err := emitAssistantToolCallFailed(emit, job, call, "tool_runner_unavailable"); err != nil {
			return toolCallOutcome{}, err
		}
		return toolCallOutcome{}, appendToolError(messages, call, "tool_runner_unavailable")
	}
	result, err := a.tools.Runner.Run(ctx, tools.RunInput{
		AgentID:    turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		RunID:      job.GetRunId(),
		TraceID:    job.GetTraceId(),
		ToolCallID: call.ID,
		ServerName: entry.ServerName,
		ToolName:   call.Name,
		Args:       call.Arguments,
		MCPClient:  entry.Client,
	})
	if ctxErr := ctx.Err(); ctxErr != nil {
		return toolCallOutcome{}, ctxErr
	}
	if err != nil {
		if tools.RunWasTerminalized(err) {
			return toolCallOutcome{}, errRunTerminalized
		}
		if tools.SideEffectWasCommitted(err) || tools.ReportingFailed(err) {
			return toolCallOutcome{}, err
		}
		if !tools.BeaconWasPosted(err) {
			if emitErr := emitAssistantToolCallFailed(emit, job, call, err.Error()); emitErr != nil {
				return toolCallOutcome{}, emitErr
			}
		}
		return toolCallOutcome{}, appendToolError(messages, call, err.Error())
	}
	outcome := toolCallOutcome{SuccessfulSideEffect: true}
	data, err := json.Marshal(result)
	if err != nil {
		return outcome, ToolResultReportingError{ToolCallID: call.ID, err: err}
	}
	*messages = append(*messages, toolResultMessage(call, data))
	return outcome, nil
}

type toolCallOutcome struct {
	SuccessfulSideEffect bool
}

type ToolResultReportingError struct {
	ToolCallID string
	err        error
}

func (e ToolResultReportingError) Error() string {
	return fmt.Sprintf("report tool result %s: %v", e.ToolCallID, e.err)
}

func (e ToolResultReportingError) Unwrap() error {
	return e.err
}

func emitAssistantToolCallFailed(emit func(*turingv1.RuntimeUpdate) error, job *turingv1.AgentJob, call llm.ToolCall, message string) error {
	if err := emit(messageEvent(job, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED, map[string]any{
		"toolName": call.Name, "toolCallId": call.ID,
	})); err != nil {
		return err
	}
	return emitToolCallFailed(emit, job, call, message)
}

func emitToolCallFailed(emit func(*turingv1.RuntimeUpdate) error, job *turingv1.AgentJob, call llm.ToolCall, message string) error {
	return emit(messageEvent(job, turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED, map[string]any{
		"toolName": call.Name, "toolCallId": call.ID, "error": message,
	}))
}

func toolResultMessage(call llm.ToolCall, content []byte) llm.ChatMessage {
	return llm.ChatMessage{Role: "tool", Name: call.Name, ToolCallID: call.ID, Content: string(content)}
}

func appendToolError(messages *[]llm.ChatMessage, call llm.ToolCall, message string) error {
	data, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		return err
	}
	*messages = append(*messages, toolResultMessage(call, data))
	return nil
}

func normalizeToolCallIDs(calls []llm.ToolCall, used map[string]struct{}) {
	for index := range calls {
		for {
			candidate := tools.NewToolCallID()
			if _, exists := used[candidate]; !exists {
				calls[index].ID = candidate
				used[candidate] = struct{}{}
				break
			}
		}
	}
}

func completeRun(emit func(*turingv1.RuntimeUpdate) error, job *turingv1.AgentJob, content string) error {
	if err := emit(messageEvent(job, turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED, map[string]any{"messageId": job.GetAssistantMessageId(), "content": content})); err != nil {
		return err
	}
	return emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{RunId: job.GetRunId(), AssistantMessageId: job.GetAssistantMessageId(), Content: content}}})
}

func (a *GeneralAssistant) tryDebugTool(ctx context.Context, job *turingv1.AgentJob, trimmed string, emit func(*turingv1.RuntimeUpdate) error) (bool, error) {
	if a.tools == nil || a.tools.Runner == nil {
		return false, nil
	}
	var client tools.MCPClient
	serverName := ""
	toolName := ""
	args := map[string]any{}
	switch trimmed {
	case "/tool system.time":
		client = a.tools.SystemMCP
		serverName = "system"
		toolName = "system.time"
	case "/tool files.create":
		client = a.tools.FilesMCP
		serverName = "files"
		toolName = "files.create"
		args = map[string]any{"path": "runtime-smoke.txt", "content": "created through approval flow"}
	default:
		return false, nil
	}
	if client == nil {
		return true, emitRunFailed(emit, job, "tool_call_failed", "MCP client is not configured", false)
	}
	result, err := a.tools.Runner.Run(ctx, tools.RunInput{AgentID: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, RunID: job.GetRunId(), TraceID: job.GetTraceId(), ServerName: serverName, ToolName: toolName, Args: args, MCPClient: client})
	if err != nil {
		if tools.RunWasTerminalized(err) {
			return true, nil
		}
		if tools.SideEffectWasCommitted(err) || tools.ReportingFailed(err) {
			return true, err
		}
		return true, emitRunFailed(emit, job, "tool_call_failed", err.Error(), false)
	}
	data, err := json.Marshal(result)
	if err != nil {
		return true, ToolResultReportingError{err: err}
	}
	content := string(data)
	if err := emit(messageEvent(job, turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA, map[string]any{"messageId": job.GetAssistantMessageId(), "delta": content})); err != nil {
		return true, err
	}
	if err := emit(messageEvent(job, turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED, map[string]any{"messageId": job.GetAssistantMessageId(), "content": content})); err != nil {
		return true, err
	}
	return true, emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{RunId: job.GetRunId(), AssistantMessageId: job.GetAssistantMessageId(), Content: content}}})
}

func messageEvent(job *turingv1.AgentJob, eventType turingv1.TuringEventType, payload map[string]any) *turingv1.RuntimeUpdate {
	structPayload, err := safejson.ToStruct(payload)
	if err != nil {
		structPayload = nil
	}
	return &turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_Event{Event: &turingv1.TuringEvent{SessionId: job.GetSessionId(), RunId: job.GetRunId(), TraceId: job.GetTraceId(), Type: eventType, Payload: structPayload}}}
}

func emitRunFailed(emit func(*turingv1.RuntimeUpdate) error, job *turingv1.AgentJob, code string, message string, retryable bool) error {
	return emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunFailed{RunFailed: &turingv1.RuntimeRunFailed{RunId: job.GetRunId(), Code: code, Message: message, Retryable: retryable}}})
}

func retryableModelError(code string) bool {
	switch code {
	case "model_unavailable", "model_stream_error":
		return true
	default:
		return false
	}
}
