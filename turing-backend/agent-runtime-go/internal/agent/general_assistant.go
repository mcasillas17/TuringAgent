package agent

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/llm"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/safejson"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/tools"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MessageClient interface {
	FetchMessages(ctx context.Context, sessionID string, beforeMessageID string) ([]llm.ChatMessage, error)
}

// ContextRecaller supplies material from the user's earlier sessions. Kept as an
// interface here so the agent does not depend on the memory package's concrete
// type and a test can substitute a fake. Optional: a nil recaller simply means
// no recall.
type ContextRecaller interface {
	Recall(ctx context.Context, sessionID string, userText string, inContext []llm.ChatMessage) (llm.ChatMessage, bool)
}

type GeneralAssistantTools struct {
	SystemMCP          ToolLister
	FilesMCP           ToolLister
	Runner             *tools.Runner
	Recall             ContextRecaller
	MaxToolCallsPerRun int
	ModelTimeout       time.Duration
	ToolTimeout        time.Duration
	TotalToolTimeout   time.Duration
}

const (
	maxToolIterations         = 5
	defaultMaxToolCallsPerRun = 10
	maxRuntimeMessageBytes    = 4 * 1024 * 1024
	runtimeUpdateHeadroom     = 64 * 1024
	maxModelOutputBytes       = maxRuntimeMessageBytes - runtimeUpdateHeadroom
	maxToolResultBytes        = 4 * 1024 * 1024
	toolIterationFallback     = "Tool iteration limit reached before a final response."
	emptyFinalFallback        = "The model returned an empty response."
	// maxUnknownToolListing caps how many tool names an unknown-tool error echoes
	// back to the model. A small model only needs a handful of candidates to
	// correct itself, and the registry could grow large; an unbounded list would
	// eat into the tool-result byte budget. When more tools exist than this, the
	// payload lists the first maxUnknownToolListing, flags the truncation, and
	// reports the true total so the truncation is never silent.
	maxUnknownToolListing = 32
)

var errRunTerminalized = errors.New("run already terminalized")

type terminalizedRunExitError struct{}

func (terminalizedRunExitError) Error() string     { return errRunTerminalized.Error() }
func (terminalizedRunExitError) RunTerminal() bool { return true }

type GeneralAssistant struct {
	providers          map[turingv1.ModelProvider]llm.Provider
	messages           MessageClient
	tools              *GeneralAssistantTools
	recall             ContextRecaller
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
	var recall ContextRecaller
	if toolset != nil {
		recall = toolset.Recall
	}
	return &GeneralAssistant{
		providers:          providers,
		messages:           messages,
		tools:              toolset,
		recall:             recall,
		maxToolCallsPerRun: maxToolCallsPerRun,
	}
}

func (a *GeneralAssistant) SetToolBeaconPoster(post func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)) {
	if a.tools == nil || a.tools.Runner == nil {
		return
	}
	a.tools.Runner.PostBeacon = post
}

// DiscoveredTools is the snapshot the worker reports on connect. It reuses the
// same discovery Execute serves from, so what the orchestrator registers and
// what the agent will actually run can never diverge — reporting one tool set
// and executing against another would let a tool run under a policy that was
// never registered for it. The registry is cached, so a reconnect does not
// re-list every MCP server.
func (a *GeneralAssistant) DiscoveredTools(ctx context.Context) ([]DiscoveredTool, error) {
	registry, err := a.discoverTools(ctx)
	if err != nil {
		return nil, err
	}
	return registry.Discovered(), nil
}

func (a *GeneralAssistant) Execute(ctx context.Context, job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error {
	if job == nil {
		return fmt.Errorf("job is required")
	}
	messages, err := a.messages.FetchMessages(
		ctx,
		job.GetSessionId(),
		job.GetUserMessageId(),
	)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return emitRunFailed(emit, job, "message_fetch_failed", err.Error(), retryableMessageFetchError(err))
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
	// Recalled material is prepended so it sits before the live conversation and
	// cannot be read as the user's latest turn. Recall is given the request as
	// built, which is how it knows what is already in front of the model; it
	// never returns an error, degrading to "no block" instead, so a slow or
	// unavailable search cannot fail the run.
	if a.recall != nil {
		if block, ok := a.recall.Recall(ctx, job.GetSessionId(), job.GetUserText(), requestMessages); ok {
			requestMessages = append([]llm.ChatMessage{block}, requestMessages...)
			// The block tells the model where the material came from; without this
			// the user gets no such hint, and an answer drawn from a conversation
			// weeks ago reads as confabulation. Emitted before the model request so
			// it precedes the answer it explains. Recall returning nothing stays
			// silent — that is the common case.
			if err := emit(messageEvent(job, turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP, map[string]any{
				"note": "Using material recalled from earlier conversations",
			})); err != nil {
				return err
			}
		}
	}
	var content strings.Builder
	toolCallCount := 0
	successfulToolSideEffect := false
	usedModelToolCallIDs := make(map[string]struct{})
	toolResultBytes := 0
	for toolIteration := 0; ; {
		if err := ctx.Err(); err != nil {
			return err
		}
		modelCtx, cancelModel := boundedContext(ctx, a.modelTimeout())
		events, err := provider.StreamChat(modelCtx, llm.ChatRequest{
			Model:    job.GetModel(),
			Messages: requestMessages,
			Tools:    registry.Definitions(),
		})
		if err != nil {
			modelErr := modelCtx.Err()
			cancelModel()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(modelErr, context.DeadlineExceeded) {
				return emitRunFailed(emit, job, "model_timeout", modelErr.Error(), !successfulToolSideEffect)
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			return emitRunFailed(emit, job, "model_stream_failed", err.Error(), retryableProviderStartError(err) && !successfulToolSideEffect)
		}
		var turnText strings.Builder
		var calls []llm.ToolCall
	stream:
		for {
			var event llm.StreamEvent
			var ok bool
			select {
			case <-modelCtx.Done():
				modelErr := modelCtx.Err()
				cancelModel()
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return emitRunFailed(emit, job, "model_timeout", modelErr.Error(), !successfulToolSideEffect)
			case event, ok = <-events:
				if !ok {
					break stream
				}
			}
			switch event.Type {
			case "delta":
				if len(event.Text) > maxModelOutputBytes-content.Len() {
					cancelModel()
					return emitRunFailed(
						emit,
						job,
						"model_output_limit_exceeded",
						fmt.Sprintf("model output exceeds %d bytes", maxModelOutputBytes),
						false,
					)
				}
				turnText.WriteString(event.Text)
				content.WriteString(event.Text)
				if err := emit(messageEvent(job, turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA, map[string]any{"messageId": job.GetAssistantMessageId(), "delta": event.Text})); err != nil {
					cancelModel()
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
				cancelModel()
				return emitRunFailed(emit, job, code, message, retryableModelError(code) && !successfulToolSideEffect)
			}
		}
		modelErr := modelCtx.Err()
		cancelModel()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(modelErr, context.DeadlineExceeded) {
			return emitRunFailed(emit, job, "model_timeout", modelErr.Error(), !successfulToolSideEffect)
		}
		if len(calls) == 0 {
			if strings.TrimSpace(content.String()) == "" {
				if err := emit(messageEvent(job, turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA, map[string]any{
					"messageId": job.GetAssistantMessageId(),
					"delta":     emptyFinalFallback,
				})); err != nil {
					return err
				}
				content.Reset()
				content.WriteString(emptyFinalFallback)
			}
			return completeRun(emit, job, content.String())
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
		normalizeToolCallIDs(calls, job.GetRunId(), toolIteration, usedModelToolCallIDs)
		requestMessages = append(requestMessages, llm.ChatMessage{
			Role:      "assistant",
			Content:   turnText.String(),
			ToolCalls: calls,
		})
		for _, call := range calls {
			outcome, err := a.executeToolCall(ctx, job, emit, registry, call)
			successfulToolSideEffect = successfulToolSideEffect || outcome.SuccessfulSideEffect
			if err != nil {
				if errors.Is(err, errRunTerminalized) {
					return terminalizedRunExitError{}
				}
				return err
			}
			if outcome.ResultMessage != nil {
				if outcome.AppendedBytes > maxToolResultBytes-toolResultBytes {
					return emitRunFailed(
						emit,
						job,
						"tool_result_limit_exceeded",
						fmt.Sprintf("serialized tool results exceed %d bytes", maxToolResultBytes),
						false,
					)
				}
				requestMessages = append(requestMessages, *outcome.ResultMessage)
				toolResultBytes += outcome.AppendedBytes
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
			if strings.TrimSpace(content.String()) == "" {
				if err := emit(messageEvent(job, turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA, map[string]any{
					"messageId": job.GetAssistantMessageId(),
					"delta":     toolIterationFallback,
				})); err != nil {
					return err
				}
				content.Reset()
				content.WriteString(toolIterationFallback)
			}
			return completeRun(emit, job, content.String())
		}
	}
}

func (a *GeneralAssistant) discoverTools(ctx context.Context) (*ToolRegistry, error) {
	parentCtx := ctx
	ctx, cancel := boundedContext(ctx, a.toolTimeout())
	defer cancel()
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
		discovery.retryAfterLeaderCancel = err != nil && parentCtx.Err() != nil && errors.Is(err, parentCtx.Err())
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
) (toolCallOutcome, error) {
	if err := ctx.Err(); err != nil {
		return toolCallOutcome{}, err
	}
	entry, found := registry.Lookup(call.Name)
	if !found {
		if err := emitAssistantToolCallFailed(emit, job, call, "unknown_tool"); err != nil {
			return toolCallOutcome{}, err
		}
		return unknownToolOutcome(call, registry)
	}
	if a.tools == nil || a.tools.Runner == nil {
		if err := emitAssistantToolCallFailed(emit, job, call, "tool_runner_unavailable"); err != nil {
			return toolCallOutcome{}, err
		}
		return toolErrorOutcome(call, "tool_runner_unavailable")
	}
	runOutcome, err := a.tools.Runner.RunWithOutcome(ctx, tools.RunInput{
		AgentID:         turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		RunID:           job.GetRunId(),
		TraceID:         job.GetTraceId(),
		ModelToolCallID: call.ID,
		ServerName:      entry.ServerName,
		ToolName:        call.Name,
		Args:            call.Arguments,
		MCPClient:       entry.Client,
		Timeout:         a.toolTimeout(),
		TotalTimeout:    a.totalToolTimeout(),
	})
	outcome := toolCallOutcome{SuccessfulSideEffect: runOutcome.SideEffecting}
	if ctxErr := ctx.Err(); ctxErr != nil && !tools.ReportingFailed(err) {
		return toolCallOutcome{}, ctxErr
	}
	if err != nil {
		if tools.RunWasTerminalized(err) {
			return toolCallOutcome{}, errRunTerminalized
		}
		if tools.SideEffectWasCommitted(err) || tools.SideEffectWasUncertain(err) ||
			tools.ApprovalWaitFailed(err) || tools.ReportingFailed(err) {
			return toolCallOutcome{}, err
		}
		if !tools.BeaconWasPosted(err) {
			if emitErr := emitAssistantToolCallFailed(emit, job, call, err.Error()); emitErr != nil {
				return toolCallOutcome{}, emitErr
			}
		}
		return toolErrorOutcome(call, err.Error())
	}
	data, err := json.Marshal(runOutcome.Result)
	if err != nil {
		return outcome, ToolResultReportingError{ToolCallID: call.ID, err: err}
	}
	message := toolResultMessage(call, data)
	outcome.ResultMessage = &message
	outcome.AppendedBytes = len(data)
	return outcome, nil
}

type toolCallOutcome struct {
	SuccessfulSideEffect bool
	ResultMessage        *llm.ChatMessage
	AppendedBytes        int
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

// unknownToolPayload is the tool-role content returned when the model names a
// tool the registry does not hold. Unlike the flat {"error": ...} of a failed
// call, it says the name is unknown, echoes WHICH name was rejected, and lists
// the tools that actually exist so a small model has something concrete to
// correct toward instead of re-emitting the same wrong name every iteration.
type unknownToolPayload struct {
	Error          string   `json:"error"`
	RejectedTool   string   `json:"rejectedTool"`
	AvailableTools []string `json:"availableTools"`
	// Truncated and TotalAvailable are only present when the list was capped, so
	// the model can tell a short list from a truncated one.
	Truncated      bool `json:"availableToolsTruncated,omitempty"`
	TotalAvailable int  `json:"totalAvailableTools,omitempty"`
}

// unknownToolOutcome builds the recoverable error for an unknown tool name. Same
// outcome shape as toolErrorOutcome — a tool-role result threaded back to the
// model — but with actionable content.
func unknownToolOutcome(call llm.ToolCall, registry *ToolRegistry) (toolCallOutcome, error) {
	definitions := registry.Definitions()
	available := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		available = append(available, definition.Name)
	}
	payload := unknownToolPayload{
		Error:          "unknown_tool",
		RejectedTool:   call.Name,
		AvailableTools: available,
	}
	if len(available) > maxUnknownToolListing {
		payload.AvailableTools = available[:maxUnknownToolListing]
		payload.Truncated = true
		payload.TotalAvailable = len(available)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return toolCallOutcome{}, err
	}
	result := toolResultMessage(call, data)
	return toolCallOutcome{ResultMessage: &result, AppendedBytes: len(data)}, nil
}

func toolErrorOutcome(call llm.ToolCall, message string) (toolCallOutcome, error) {
	data, err := json.Marshal(map[string]string{"error": message})
	if err != nil {
		return toolCallOutcome{}, err
	}
	result := toolResultMessage(call, data)
	return toolCallOutcome{ResultMessage: &result, AppendedBytes: len(data)}, nil
}

func normalizeToolCallIDs(calls []llm.ToolCall, runID string, toolRound int, used map[string]struct{}) {
	counts := make(map[string]int, len(calls))
	for _, call := range calls {
		if validProviderToolCallID(call.ID) {
			counts[call.ID]++
		}
	}
	for id, count := range counts {
		if count > 1 {
			used[id] = struct{}{}
		}
	}
	for index := range calls {
		providerID := calls[index].ID
		_, alreadyUsed := used[providerID]
		if validProviderToolCallID(providerID) && counts[providerID] == 1 && !alreadyUsed {
			used[providerID] = struct{}{}
			continue
		}
		arguments, err := json.Marshal(calls[index].Arguments)
		if err != nil {
			arguments = []byte(fmt.Sprintf("%#v", calls[index].Arguments))
		}
		for collision := 0; ; collision++ {
			input := fmt.Sprintf(
				"%d:%s:%d:%d:%d:%s:%d:%s:%d",
				len(runID), runID,
				toolRound,
				index,
				len(calls[index].Name), calls[index].Name,
				len(arguments), arguments,
				collision,
			)
			sum := sha256.Sum256([]byte(input))
			id := "call_" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(sum[:])
			if _, duplicate := used[id]; duplicate {
				continue
			}
			calls[index].ID = id
			used[id] = struct{}{}
			break
		}
	}
}

func validProviderToolCallID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for index := range len(id) {
		character := id[index]
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
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
	var client ToolLister
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
	if isNilToolLister(client) {
		return true, emitRunFailed(emit, job, "tool_call_failed", "MCP client is not configured", false)
	}
	result, err := a.tools.Runner.Run(ctx, tools.RunInput{
		AgentID:      turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		RunID:        job.GetRunId(),
		TraceID:      job.GetTraceId(),
		ServerName:   serverName,
		ToolName:     toolName,
		Args:         args,
		MCPClient:    client,
		Timeout:      a.toolTimeout(),
		TotalTimeout: a.totalToolTimeout(),
	})
	if err != nil {
		if tools.RunWasTerminalized(err) {
			return true, terminalizedRunExitError{}
		}
		if tools.SideEffectWasCommitted(err) || tools.SideEffectWasUncertain(err) ||
			tools.ApprovalWaitFailed(err) || tools.ReportingFailed(err) {
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
	case "model_unavailable", "model_stream_error", "model_timeout":
		return true
	default:
		return false
	}
}

func retryableMessageFetchError(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Aborted:
		return true
	case codes.Canceled, codes.Unimplemented, codes.Unauthenticated, codes.PermissionDenied, codes.InvalidArgument, codes.NotFound, codes.FailedPrecondition:
		return false
	default:
		return true
	}
}

func retryableProviderStartError(err error) bool {
	var classified interface{ Retryable() bool }
	if errors.As(err, &classified) {
		return classified.Retryable()
	}

	var unsupportedType *json.UnsupportedTypeError
	if errors.As(err, &unsupportedType) {
		return false
	}
	var unsupportedValue *json.UnsupportedValueError
	if errors.As(err, &unsupportedValue) {
		return false
	}

	var urlError *url.Error
	if errors.As(err, &urlError) {
		if urlError.Op == "parse" {
			return false
		}
	}
	var escapeError url.EscapeError
	if errors.As(err, &escapeError) {
		return false
	}
	var invalidHostError url.InvalidHostError
	return !errors.As(err, &invalidHostError)
}

func (a *GeneralAssistant) modelTimeout() time.Duration {
	if a.tools == nil {
		return 0
	}
	return a.tools.ModelTimeout
}

func (a *GeneralAssistant) toolTimeout() time.Duration {
	if a.tools == nil {
		return 0
	}
	return a.tools.ToolTimeout
}

func (a *GeneralAssistant) totalToolTimeout() time.Duration {
	if a.tools == nil {
		return 0
	}
	return a.tools.TotalToolTimeout
}

func boundedContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}
