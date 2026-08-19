package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const (
	maxStreamTokenBytes                     = 1024 * 1024
	maxOllamaToolCalls                      = 128
	maxOllamaToolCallIDBytes                = 1024
	maxOllamaToolCallNameBytes              = 1024
	maxOllamaToolCallArgumentBytes          = maxStreamTokenBytes
	maxOllamaToolCallAggregateArgumentBytes = maxStreamTokenBytes
)

type Ollama struct {
	baseURL             string
	client              *http.Client
	keepAlive           json.RawMessage
	contextWindowTokens int
	maxOutputTokens     int
}

var _ Provider = (*Ollama)(nil)

func NewOllama(baseURL string, client *http.Client) *Ollama {
	if client == nil {
		client = http.DefaultClient
	}
	return &Ollama{
		baseURL:             strings.TrimRight(baseURL, "/"),
		client:              client,
		contextWindowTokens: DefaultContextWindowTokens,
		maxOutputTokens:     DefaultMaxOutputTokens,
	}
}

func NewOllamaWithLimits(
	baseURL string,
	client *http.Client,
	contextWindowTokens int,
	maxOutputTokens int,
) (*Ollama, error) {
	if err := ValidateContextLimits(contextWindowTokens, maxOutputTokens); err != nil {
		return nil, err
	}
	provider := NewOllama(baseURL, client)
	provider.contextWindowTokens = contextWindowTokens
	provider.maxOutputTokens = maxOutputTokens
	return provider, nil
}

// WithKeepAlive sets how long Ollama should hold the model in memory after a
// request. Ollama's own default is 5 minutes, which on a personal machine keeps
// several GB of unified memory occupied long after the answer is finished —
// exactly the "does not slow my machine down" problem a local-first assistant
// has to care about.
//
// Sending it per request rather than relying on the server-wide
// OLLAMA_KEEP_ALIVE means the setting belongs to this deployment, travels with
// it, and survives an Ollama restart we do not control. Empty leaves Ollama's
// default alone.
func (p *Ollama) WithKeepAlive(keepAlive string) *Ollama {
	// A conversion error cannot happen for a value that passed config
	// validation, and silently omitting is the safe degradation: the request
	// still works, Ollama just uses its own default.
	encoded, _ := EncodeOllamaKeepAlive(keepAlive)
	p.keepAlive = encoded
	return p
}

// EncodeOllamaKeepAlive turns a user-supplied keep-alive into the JSON Ollama
// accepts, and is the single definition of what is valid.
//
// Ollama takes EITHER a duration string ("30s", "2m") or a bare number of
// seconds, where -1 means "keep loaded forever". Those are not
// interchangeable: sending "-1" as a JSON *string* fails with
// `time: missing unit in duration "-1"` and a 400 on every request. That
// matters because OLLAMA_KEEP_ALIVE is also Ollama's own server env var, so a
// user who already exports -1 for their server would otherwise have it
// injected here and break every chat.
//
// Empty means "say nothing and let Ollama decide".
func EncodeOllamaKeepAlive(value string) (json.RawMessage, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	if seconds, err := strconv.Atoi(trimmed); err == nil {
		// Bare number: seconds, and -1 means forever. Must go out unquoted.
		return json.RawMessage(strconv.Itoa(seconds)), nil
	}
	if _, err := time.ParseDuration(trimmed); err != nil {
		return nil, fmt.Errorf("keep-alive %q must be a duration such as 30s or 2m, or a whole number of seconds (-1 for forever)", value)
	}
	quoted, err := json.Marshal(trimmed)
	if err != nil {
		return nil, err
	}
	return quoted, nil
}

func (p *Ollama) ID() string { return "ollama" }

func (p *Ollama) ContextWindowTokens() int { return p.contextWindowTokens }

func (p *Ollama) MaxOutputTokens() int { return p.maxOutputTokens }

func (p *Ollama) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	body, err := marshalProviderRequest("Ollama", func() ([]byte, error) {
		return p.marshalRequest(req)
	})
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	out := make(chan StreamEvent)
	go func() {
		defer close(out)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: providerHTTPErrorCode(resp.StatusCode), Message: fmt.Sprintf("Ollama returned %d", resp.StatusCode)})
			return
		}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), maxStreamTokenBytes)
		state := newOllamaStreamState()
		for scanner.Scan() {
			line := bytes.TrimSpace(scanner.Bytes())
			if len(line) == 0 {
				continue
			}
			obj, err := decodeObjectLine(line)
			if err != nil {
				sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: err.Error()})
				return
			}
			if rawProviderError, present := obj["error"]; present {
				providerError, ok := rawProviderError.(string)
				if !ok || strings.TrimSpace(providerError) == "" || len(obj) != 1 {
					sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: "invalid Ollama provider error"})
					return
				}
				sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_unavailable", Message: "Ollama provider error: " + providerError})
				return
			}

			rawDone, present := obj["done"]
			done, ok := rawDone.(bool)
			if !present || !ok {
				sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: "done must be a boolean"})
				return
			}
			reason := ""
			if rawReason, present := obj["done_reason"]; present {
				var ok bool
				reason, ok = rawReason.(string)
				if !ok {
					sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: "done_reason must be a string"})
					return
				}
				if !done {
					sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: "done_reason is only valid on a terminal chunk"})
					return
				}
			}

			content := ""
			if rawMessage, present := obj["message"]; present {
				message, ok := rawMessage.(map[string]any)
				if !ok {
					sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: "message must be an object"})
					return
				}
				if rawRole, present := message["role"]; present {
					if _, ok := rawRole.(string); !ok {
						sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: "message role must be a string"})
						return
					}
				}
				if rawContent, present := message["content"]; present {
					var ok bool
					content, ok = rawContent.(string)
					if !ok {
						sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: "message content must be a string"})
						return
					}
				}
				if rawToolCalls, present := message["tool_calls"]; present {
					if err := state.appendToolCallFragments(rawToolCalls); err != nil {
						sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: err.Error()})
						return
					}
				}
			}
			if content != "" {
				if !sendStreamEvent(ctx, out, StreamEvent{Type: "delta", Text: content}) {
					return
				}
			}
			if !done {
				continue
			}
			if state.hasToolCalls() {
				toolCalls, err := state.finalizeToolCalls()
				if err != nil {
					sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: err.Error()})
					return
				}
				if !sendStreamEvent(ctx, out, StreamEvent{Type: "tool_call", ToolCalls: toolCalls}) {
					return
				}
			}
			sendStreamEvent(ctx, out, StreamEvent{Type: "completed", FinishReason: reason, Usage: ollamaTokenUsage(obj)})
			return
		}
		if err := scanner.Err(); err != nil {
			if ctx.Err() == nil {
				code := "model_stream_error"
				if strings.Contains(err.Error(), "token too long") {
					code = "model_bad_chunk"
				}
				sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: code, Message: err.Error()})
			}
			return
		}
		if ctx.Err() == nil {
			sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_stream_error", Message: "Ollama stream ended before a terminal event"})
		}
	}()
	return out, nil
}

func (p *Ollama) EstimateRequestTokens(req ChatRequest) (int, error) {
	body, err := p.marshalRequest(req)
	if err != nil {
		return 0, err
	}
	return len(body), nil
}

func (p *Ollama) marshalRequest(req ChatRequest) ([]byte, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = p.maxOutputTokens
	}
	if maxTokens >= p.contextWindowTokens {
		return nil, fmt.Errorf("max output tokens %d must be less than context window %d", maxTokens, p.contextWindowTokens)
	}
	tools := ollamaTools(req.Tools)
	messages := ollamaMessages(req.Messages)
	contextWindowTokens := p.contextWindowTokens
	var body []byte
	var err error
	for range 10 {
		body, err = json.Marshal(ollamaChatRequest{
			Model:     req.Model,
			Messages:  messages,
			Stream:    true,
			Options:   ollamaRequestOptions(req.Temperature, maxTokens, contextWindowTokens),
			Tools:     tools,
			KeepAlive: p.keepAlive,
		})
		if err != nil {
			return nil, err
		}
		required := len(body) + maxTokens
		if required > p.contextWindowTokens {
			required = p.contextWindowTokens
		}
		if required == contextWindowTokens {
			return body, nil
		}
		contextWindowTokens = required
	}
	return nil, errors.New("Ollama request context size did not converge")
}

// ollamaTokenUsage reads the token counts Ollama puts on its terminal chunk.
//
// prompt_eval_count is the prompt Ollama actually evaluated and eval_count is
// what it generated. Both are documented but neither is guaranteed: an older
// Ollama, a proxy in front of it, or a cached prompt (where prompt_eval_count
// is omitted entirely) can leave either absent. Absent stays absent — nil, not
// zero.
//
// Deliberately lenient where the rest of this file is strict. Everything else
// parsed from a chunk shapes the answer, so a malformed value has to fail the
// run; a token count only shapes a report about the run, and failing a
// completed answer because a number could not be parsed would trade something
// the user asked for against something they did not.
func ollamaTokenUsage(chunk map[string]any) *TokenUsage {
	usage := TokenUsage{
		InputTokens:  ollamaTokenCount(chunk["prompt_eval_count"]),
		OutputTokens: ollamaTokenCount(chunk["eval_count"]),
	}
	if !usage.Reported() {
		return nil
	}
	return &usage
}

func ollamaTokenCount(value any) *int64 {
	number, ok := value.(json.Number)
	if !ok {
		return nil
	}
	parsed, err := number.Int64()
	if err != nil || parsed < 0 {
		return nil
	}
	return TokenCount(parsed)
}

type ollamaStreamToolCall struct {
	id            string
	name          string
	arguments     map[string]any
	argumentBytes int
}

type ollamaStreamState struct {
	toolCalls     map[int]*ollamaStreamToolCall
	argumentBytes int
}

func newOllamaStreamState() *ollamaStreamState {
	return &ollamaStreamState{toolCalls: make(map[int]*ollamaStreamToolCall)}
}

func (state *ollamaStreamState) hasToolCalls() bool {
	return len(state.toolCalls) > 0
}

func (state *ollamaStreamState) appendToolCallFragments(value any) error {
	rawCalls, ok := value.([]any)
	if !ok {
		return fmt.Errorf("tool_calls must be an array")
	}
	if len(rawCalls) > maxOllamaToolCalls {
		return fmt.Errorf("tool call count exceeds %d", maxOllamaToolCalls)
	}
	indices := make(map[int]struct{}, len(rawCalls))
	for position, rawCall := range rawCalls {
		call, ok := rawCall.(map[string]any)
		if !ok {
			return fmt.Errorf("tool call %d must be an object", position)
		}
		if rawType, present := call["type"]; present {
			callType, ok := rawType.(string)
			if !ok {
				return fmt.Errorf("tool call %d type must be a string", position)
			}
			if callType != "function" {
				return fmt.Errorf("tool call %d type must be %q", position, "function")
			}
		}
		id := ""
		if rawID, present := call["id"]; present {
			id, ok = rawID.(string)
			if !ok {
				return fmt.Errorf("tool call %d ID must be a string", position)
			}
			if len(id) > maxOllamaToolCallIDBytes {
				return fmt.Errorf("tool call ID exceeds %d bytes", maxOllamaToolCallIDBytes)
			}
		}
		function, ok := call["function"].(map[string]any)
		if !ok {
			return fmt.Errorf("tool call %d function must be an object", position)
		}
		index := position
		if rawIndex, present := function["index"]; present {
			number, ok := rawIndex.(json.Number)
			if !ok {
				return fmt.Errorf("tool call %d function index must be an integer", position)
			}
			parsed, err := number.Int64()
			if err != nil {
				return fmt.Errorf("tool call %d function index must be an integer", position)
			}
			if parsed < 0 {
				return fmt.Errorf("tool call has negative index %d", parsed)
			}
			if parsed >= maxOllamaToolCalls {
				return fmt.Errorf("tool call index %d exceeds %d", parsed, maxOllamaToolCalls-1)
			}
			index = int(parsed)
		}
		if _, duplicate := indices[index]; duplicate {
			return fmt.Errorf("tool calls contain duplicate index %d", index)
		}
		indices[index] = struct{}{}

		name := ""
		if rawName, present := function["name"]; present {
			name, ok = rawName.(string)
			if !ok {
				return fmt.Errorf("tool call %d function name must be a string", index)
			}
			if len(name) > maxOllamaToolCallNameBytes {
				return fmt.Errorf("tool call name exceeds %d bytes", maxOllamaToolCallNameBytes)
			}
		}

		// A tool that takes no arguments is not malformed, and it is the common
		// case here: every mcp-system tool declares an empty schema, and Ollama's
		// ToolCallFunction has no omitempty on Arguments, so such a call arrives
		// as `"arguments":null` (or absent, from other servers). Rejecting those
		// fails the entire run without ever dispatching the tool. Only a present,
		// non-null, non-object value is a protocol error.
		arguments := map[string]any{}
		if rawArguments, present := function["arguments"]; present && rawArguments != nil {
			// Scoped so a failed assertion cannot leave `arguments` nil for the
			// caller, and so the loop's outer `ok` is not clobbered.
			parsed, isObject := rawArguments.(map[string]any)
			if !isObject {
				return fmt.Errorf("tool call %d function arguments must be an object", index)
			}
			arguments = parsed
		}
		if err := state.appendToolCallFragment(index, id, name, arguments); err != nil {
			return err
		}
	}
	return nil
}

func (state *ollamaStreamState) appendToolCallFragment(index int, id, name string, arguments map[string]any) error {
	call := state.toolCalls[index]
	if call == nil && len(state.toolCalls) >= maxOllamaToolCalls {
		return fmt.Errorf("tool call count exceeds %d", maxOllamaToolCalls)
	}
	if call != nil && id != "" && call.id != "" && id != call.id {
		return fmt.Errorf("tool call %d has conflicting ID %q after %q", index, id, call.id)
	}
	if call != nil && name != "" && call.name != "" && name != call.name {
		return fmt.Errorf("tool call %d has conflicting function name %q after %q", index, name, call.name)
	}

	mergedArguments := make(map[string]any)
	if call != nil {
		mergedArguments = cloneOllamaArgumentMap(call.arguments)
	}
	if err := mergeOllamaArgumentMaps(mergedArguments, arguments, ""); err != nil {
		return fmt.Errorf("tool call %d %w", index, err)
	}
	encodedArguments, err := json.Marshal(mergedArguments)
	if err != nil {
		return fmt.Errorf("tool call %d function arguments: %w", index, err)
	}
	if len(encodedArguments) > maxOllamaToolCallArgumentBytes {
		return fmt.Errorf("tool call %d arguments exceed %d bytes", index, maxOllamaToolCallArgumentBytes)
	}
	oldArgumentBytes := 0
	if call != nil {
		oldArgumentBytes = call.argumentBytes
	}
	aggregateArgumentBytes := state.argumentBytes - oldArgumentBytes + len(encodedArguments)
	if aggregateArgumentBytes > maxOllamaToolCallAggregateArgumentBytes {
		return fmt.Errorf("aggregate arguments exceed %d bytes", maxOllamaToolCallAggregateArgumentBytes)
	}

	if call == nil {
		call = &ollamaStreamToolCall{}
		state.toolCalls[index] = call
	}
	if id != "" {
		call.id = id
	}
	if name != "" {
		call.name = name
	}
	call.arguments = mergedArguments
	call.argumentBytes = len(encodedArguments)
	state.argumentBytes = aggregateArgumentBytes
	return nil
}

func (state *ollamaStreamState) finalizeToolCalls() ([]ToolCall, error) {
	calls := make([]ToolCall, 0, len(state.toolCalls))
	ids := make(map[string]struct{}, len(state.toolCalls))
	for index := 0; index < len(state.toolCalls); index++ {
		call, ok := state.toolCalls[index]
		if !ok {
			return nil, fmt.Errorf("tool call indices are non-contiguous: missing index %d", index)
		}
		if call.name == "" {
			return nil, fmt.Errorf("tool call %d is missing a function name", index)
		}
		if call.id != "" {
			if _, duplicate := ids[call.id]; duplicate {
				return nil, fmt.Errorf("tool call %d has duplicate ID %q", index, call.id)
			}
			ids[call.id] = struct{}{}
		}
		calls = append(calls, ToolCall{
			ID:        call.id,
			Name:      call.name,
			Arguments: cloneOllamaArgumentMap(call.arguments),
		})
	}
	return calls, nil
}

func mergeOllamaArgumentMaps(destination, fragment map[string]any, path string) error {
	for key, fragmentValue := range fragment {
		argumentPath := key
		if path != "" {
			argumentPath = path + "." + key
		}
		existingValue, present := destination[key]
		if !present {
			destination[key] = cloneOllamaArgumentValue(fragmentValue)
			continue
		}
		existingMap, existingIsMap := existingValue.(map[string]any)
		fragmentMap, fragmentIsMap := fragmentValue.(map[string]any)
		if existingIsMap && fragmentIsMap {
			if err := mergeOllamaArgumentMaps(existingMap, fragmentMap, argumentPath); err != nil {
				return err
			}
			continue
		}
		if !reflect.DeepEqual(existingValue, fragmentValue) {
			return fmt.Errorf("has conflicting argument %q", argumentPath)
		}
	}
	return nil
}

func cloneOllamaArgumentMap(value map[string]any) map[string]any {
	cloned := make(map[string]any, len(value))
	for key, item := range value {
		cloned[key] = cloneOllamaArgumentValue(item)
	}
	return cloned
}

func cloneOllamaArgumentValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneOllamaArgumentMap(typed)
	case []any:
		cloned := make([]any, len(typed))
		for index, item := range typed {
			cloned[index] = cloneOllamaArgumentValue(item)
		}
		return cloned
	default:
		return value
	}
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  *ollamaOptions  `json:"options,omitempty"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
	// omitempty matters: Ollama rejects an empty keep_alive, so "unset" must
	// stay off the wire rather than serialize as "".
	KeepAlive json.RawMessage `json:"keep_alive,omitempty"`
}

type ollamaOptions struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
	NumCtx      int     `json:"num_ctx"`
}

func ollamaRequestOptions(temperature float64, maxTokens int, contextWindowTokens int) *ollamaOptions {
	return &ollamaOptions{
		Temperature: temperature,
		NumPredict:  maxTokens,
		NumCtx:      contextWindowTokens,
	}
}

type ollamaMessage struct {
	Role       string                  `json:"role"`
	Content    string                  `json:"content"`
	ToolName   string                  `json:"tool_name,omitempty"`
	ToolCallID string                  `json:"tool_call_id,omitempty"`
	ToolCalls  []ollamaMessageToolCall `json:"tool_calls,omitempty"`
}

type ollamaMessageToolCall struct {
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type"`
	Function ollamaFunctionCall `json:"function"`
}

type ollamaFunctionCall struct {
	Index     int            `json:"index"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func ollamaMessages(messages []ChatMessage) []ollamaMessage {
	converted := make([]ollamaMessage, 0, len(messages))
	for _, message := range messages {
		result := ollamaMessage{Role: message.Role, Content: message.Content}
		switch message.Role {
		case "tool":
			result.ToolName = message.Name
			result.ToolCallID = message.ToolCallID
		case "assistant":
			for index, call := range message.ToolCalls {
				arguments := call.Arguments
				if arguments == nil {
					arguments = map[string]any{}
				}
				result.ToolCalls = append(result.ToolCalls, ollamaMessageToolCall{
					ID:   call.ID,
					Type: "function",
					Function: ollamaFunctionCall{
						Index:     index,
						Name:      call.Name,
						Arguments: arguments,
					},
				})
			}
		}
		converted = append(converted, result)
	}
	return converted
}

type ollamaTool struct {
	Type     string                   `json:"type"`
	Function ollamaFunctionDefinition `json:"function"`
}

type ollamaFunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

func ollamaTools(definitions []ToolDefinition) []ollamaTool {
	tools := make([]ollamaTool, 0, len(definitions))
	for _, definition := range definitions {
		parameters := definition.Parameters
		if parameters == nil {
			parameters = map[string]any{"type": "object"}
		}
		tools = append(tools, ollamaTool{
			Type: "function",
			Function: ollamaFunctionDefinition{
				Name:        definition.Name,
				Description: definition.Description,
				Parameters:  parameters,
			},
		})
	}
	return tools
}

func decodeObjectLine(line []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return nil, fmt.Errorf("malformed chunk: %w", err)
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON object")
	}
	return obj, nil
}
