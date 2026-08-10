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
	"strings"
)

const (
	maxOpenAIEventDataBytes                   = maxStreamTokenBytes
	maxOpenAIToolCalls                        = 128
	maxOpenAIToolCallArgumentBytes            = maxStreamTokenBytes
	maxOpenAIToolCallAggregateArgumentBytes   = maxStreamTokenBytes
	maxOpenAIToolCallAggregateIdentifierBytes = 64 * 1024
)

var errOpenAIPhysicalSSELineTooLong = errors.New("OpenAI-compatible SSE line exceeds maximum size")

type OpenAICompatible struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

func NewOpenAICompatible(baseURL string, apiKey string, client *http.Client) *OpenAICompatible {
	if client == nil {
		client = http.DefaultClient
	}
	return &OpenAICompatible{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: client}
}

func (p *OpenAICompatible) ID() string { return "openai_compatible" }

func (p *OpenAICompatible) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	messages, err := openAIMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(openAIChatRequest{
		Model:       req.Model,
		Messages:    messages,
		Stream:      true,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Tools:       openAITools(req.Tools),
	})
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("authorization", "Bearer "+p.apiKey)
	}
	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	out := make(chan StreamEvent)
	go func() {
		defer close(out)
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_unavailable", Message: fmt.Sprintf("OpenAI-compatible provider returned %d", resp.StatusCode)})
			return
		}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), maxStreamTokenBytes)
		scanner.Split(splitSSELines)
		state := newOpenAIStreamState()
		dispatch := func(data string) bool {
			if strings.TrimSpace(data) == "" {
				return false
			}
			if strings.TrimSpace(data) == "[DONE]" {
				if len(state.toolCalls) > 0 {
					sendStreamEvent(ctx, out, StreamEvent{
						Type:    "error",
						Code:    "model_bad_chunk",
						Message: fmt.Sprintf("[DONE] received with %d unfinished tool call(s)", len(state.toolCalls)),
					})
				} else {
					sendStreamEvent(ctx, out, StreamEvent{Type: "completed", FinishReason: "stop"})
				}
				return true
			}
			events, done, err := parseOpenAIData([]byte(data), state)
			if err != nil {
				sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: err.Error()})
				return true
			}
			for _, event := range events {
				if !sendStreamEvent(ctx, out, event) {
					return true
				}
			}
			return done
		}

		var dataLines []string
		dataBytes := 0
		firstLine := true
		for scanner.Scan() {
			line := scanner.Text()
			if firstLine {
				line = strings.TrimPrefix(line, "\uFEFF")
				firstLine = false
			}
			if line == "" {
				if len(dataLines) > 0 {
					data := strings.Join(dataLines, "\n")
					dataLines = nil
					dataBytes = 0
					if dispatch(data) {
						return
					}
				}
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			field, value, found := strings.Cut(line, ":")
			if !found {
				value = ""
			}
			if field != "data" {
				continue
			}
			if strings.HasPrefix(value, " ") {
				value = value[1:]
			}
			additionalBytes := len(value)
			if len(dataLines) > 0 {
				additionalBytes++
			}
			if additionalBytes > maxOpenAIEventDataBytes-dataBytes {
				sendStreamEvent(ctx, out, StreamEvent{
					Type:    "error",
					Code:    "model_bad_chunk",
					Message: fmt.Sprintf("SSE event data exceeds %d bytes", maxOpenAIEventDataBytes),
				})
				return
			}
			dataLines = append(dataLines, value)
			dataBytes += additionalBytes
		}
		if err := scanner.Err(); err != nil {
			if ctx.Err() == nil {
				code := "model_stream_error"
				if errors.Is(err, errOpenAIPhysicalSSELineTooLong) {
					code = "model_bad_chunk"
				}
				sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: code, Message: err.Error()})
			}
			return
		}
		if ctx.Err() != nil {
			return
		}
		message := "OpenAI-compatible stream ended before a terminal event"
		if len(state.toolCalls) > 0 {
			message += fmt.Sprintf(" with %d unfinished tool call(s)", len(state.toolCalls))
		}
		sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_stream_error", Message: message})
	}()
	return out, nil
}

func splitSSELines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i, b := range data {
		switch b {
		case '\n':
			return i + 1, data[:i], nil
		case '\r':
			if i+1 == len(data) && !atEOF {
				return 0, nil, nil
			}
			advance := i + 1
			if i+1 < len(data) && data[i+1] == '\n' {
				advance++
			}
			return advance, data[:i], nil
		}
	}
	if len(data) >= maxStreamTokenBytes {
		return 0, nil, errOpenAIPhysicalSSELineTooLong
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

type openAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	Stream      bool            `json:"stream"`
	Temperature float64         `json:"temperature,omitempty"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Tools       []openAITool    `json:"tools,omitempty"`
}

type openAIMessage struct {
	Role       string                  `json:"role"`
	Content    string                  `json:"content"`
	Name       string                  `json:"name,omitempty"`
	ToolCallID string                  `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIMessageToolCall `json:"tool_calls,omitempty"`
}

type openAIMessageToolCall struct {
	ID       string                    `json:"id"`
	Type     string                    `json:"type"`
	Function openAIMessageFunctionCall `json:"function"`
}

type openAIMessageFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func openAIMessages(messages []ChatMessage) ([]openAIMessage, error) {
	converted := make([]openAIMessage, 0, len(messages))
	for _, message := range messages {
		result := openAIMessage{
			Role:    message.Role,
			Content: message.Content,
			Name:    message.Name,
		}
		if message.Role == "tool" {
			result.Name = ""
			result.ToolCallID = message.ToolCallID
		} else if message.Role == "assistant" {
			for _, call := range message.ToolCalls {
				callArguments := call.Arguments
				if callArguments == nil {
					callArguments = map[string]any{}
				}
				arguments, err := json.Marshal(callArguments)
				if err != nil {
					return nil, err
				}
				result.ToolCalls = append(result.ToolCalls, openAIMessageToolCall{
					ID:   call.ID,
					Type: "function",
					Function: openAIMessageFunctionCall{
						Name:      call.Name,
						Arguments: string(arguments),
					},
				})
			}
		}
		converted = append(converted, result)
	}
	return converted, nil
}

type openAITool struct {
	Type     string                   `json:"type"`
	Function openAIFunctionDefinition `json:"function"`
}

type openAIFunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

func openAITools(definitions []ToolDefinition) []openAITool {
	tools := make([]openAITool, 0, len(definitions))
	for _, definition := range definitions {
		parameters := definition.Parameters
		if parameters == nil {
			parameters = map[string]any{"type": "object"}
		}
		tools = append(tools, openAITool{
			Type: "function",
			Function: openAIFunctionDefinition{
				Name:        definition.Name,
				Description: definition.Description,
				Parameters:  parameters,
			},
		})
	}
	return tools
}

type openAIEnvelope struct {
	Choices json.RawMessage `json:"choices"`
	Error   json.RawMessage `json:"error"`
}

type openAIChoice struct {
	Index        *int            `json:"index"`
	Delta        json.RawMessage `json:"delta"`
	FinishReason *string         `json:"finish_reason"`
}

type openAIErrorEnvelope struct {
	Message string          `json:"message"`
	Code    json.RawMessage `json:"code"`
}

type openAIDelta struct {
	Content   *string               `json:"content"`
	ToolCalls []openAIToolCallDelta `json:"tool_calls"`
}

type openAIToolCallDelta struct {
	Index    *int   `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIToolCall struct {
	id        strings.Builder
	name      strings.Builder
	arguments strings.Builder
}

type openAIStreamState struct {
	toolCalls       map[int]*openAIToolCall
	argumentBytes   int
	identifierBytes int
}

func newOpenAIStreamState() *openAIStreamState {
	return &openAIStreamState{toolCalls: make(map[int]*openAIToolCall)}
}

func parseOpenAIData(data []byte, state *openAIStreamState) ([]StreamEvent, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var envelope openAIEnvelope
	if err := decoder.Decode(&envelope); err != nil {
		return nil, false, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return nil, false, fmt.Errorf("malformed chunk: %w", err)
	}

	if len(envelope.Error) > 0 {
		if len(envelope.Choices) > 0 {
			return nil, false, fmt.Errorf("malformed error envelope: contains choices")
		}
		event, err := parseOpenAIErrorEnvelope(envelope.Error)
		if err != nil {
			return nil, false, err
		}
		return []StreamEvent{event}, true, nil
	}

	var choices []openAIChoice
	if len(envelope.Choices) == 0 {
		return nil, false, fmt.Errorf("chunk must contain exactly one choice, got 0")
	}
	if err := json.Unmarshal(envelope.Choices, &choices); err != nil {
		return nil, false, fmt.Errorf("malformed choices: %w", err)
	}
	if len(choices) != 1 {
		return nil, false, fmt.Errorf("chunk must contain exactly one choice, got %d", len(choices))
	}
	choice := choices[0]
	if choice.Index == nil {
		return nil, false, fmt.Errorf("choice is missing index")
	}
	if *choice.Index != 0 {
		return nil, false, fmt.Errorf("choice has index %d, want 0", *choice.Index)
	}

	events := make([]StreamEvent, 0, 2)
	if len(choice.Delta) > 0 && string(choice.Delta) != "null" {
		var delta openAIDelta
		decoder := json.NewDecoder(bytes.NewReader(choice.Delta))
		decoder.UseNumber()
		if err := decoder.Decode(&delta); err != nil {
			return nil, false, err
		}
		for _, fragment := range delta.ToolCalls {
			if fragment.Index == nil {
				return nil, false, fmt.Errorf("tool call is missing index")
			}
			index := *fragment.Index
			if index < 0 {
				return nil, false, fmt.Errorf("tool call has negative index %d", index)
			}
			if fragment.Type != "" && fragment.Type != "function" {
				return nil, false, fmt.Errorf("tool call %d has unsupported type %q", index, fragment.Type)
			}
			if err := state.appendToolCallFragment(index, fragment); err != nil {
				return nil, false, err
			}
		}
		if delta.Content != nil && *delta.Content != "" {
			events = append(events, StreamEvent{Type: "delta", Text: *delta.Content})
		}
	}
	if choice.FinishReason != nil {
		if *choice.FinishReason == "tool_calls" {
			if len(state.toolCalls) == 0 {
				return nil, false, fmt.Errorf("finish_reason tool_calls received without any accumulated tool calls")
			}
			calls := make([]ToolCall, 0, len(state.toolCalls))
			ids := make(map[string]struct{}, len(state.toolCalls))
			for index := 0; index < len(state.toolCalls); index++ {
				call, ok := state.toolCalls[index]
				if !ok {
					return nil, false, fmt.Errorf("tool call indices are non-contiguous: missing index %d", index)
				}
				id := call.id.String()
				if id == "" {
					return nil, false, fmt.Errorf("tool call %d is missing an ID", index)
				}
				if _, duplicate := ids[id]; duplicate {
					return nil, false, fmt.Errorf("tool call %d has duplicate ID %q", index, id)
				}
				ids[id] = struct{}{}
				name := call.name.String()
				if name == "" {
					return nil, false, fmt.Errorf("tool call %d is missing a function name", index)
				}
				arguments := make(map[string]any)
				if call.arguments.Len() > 0 {
					decoder := json.NewDecoder(strings.NewReader(call.arguments.String()))
					decoder.UseNumber()
					var decoded any
					if err := decoder.Decode(&decoded); err != nil {
						return nil, false, fmt.Errorf("tool call %d arguments are malformed: %w", index, err)
					}
					if err := decoder.Decode(&struct{}{}); err != io.EOF {
						if err == nil {
							err = fmt.Errorf("multiple JSON values")
						}
						return nil, false, fmt.Errorf("tool call %d arguments are malformed: %w", index, err)
					}
					var ok bool
					arguments, ok = decoded.(map[string]any)
					if !ok {
						return nil, false, fmt.Errorf("tool call %d arguments must be a JSON object", index)
					}
				}
				calls = append(calls, ToolCall{ID: id, Name: name, Arguments: arguments})
			}
			events = append(events, StreamEvent{Type: "tool_call", ToolCalls: calls})
		} else if len(state.toolCalls) > 0 {
			return nil, false, fmt.Errorf("finish_reason %q received with unfinished tool calls", *choice.FinishReason)
		}
		events = append(events, StreamEvent{Type: "completed", FinishReason: *choice.FinishReason})
		return events, true, nil
	}
	return events, false, nil
}

func parseOpenAIErrorEnvelope(data []byte) (StreamEvent, error) {
	var providerError openAIErrorEnvelope
	if err := json.Unmarshal(data, &providerError); err != nil {
		return StreamEvent{}, fmt.Errorf("malformed error envelope: %w", err)
	}
	if strings.TrimSpace(providerError.Message) == "" {
		return StreamEvent{}, fmt.Errorf("malformed error envelope: missing message")
	}

	code := ""
	if len(providerError.Code) > 0 && string(providerError.Code) != "null" {
		if err := json.Unmarshal(providerError.Code, &code); err != nil || strings.TrimSpace(code) == "" {
			return StreamEvent{}, fmt.Errorf("malformed error envelope: code must be a non-empty string")
		}
	}
	message := "OpenAI-compatible provider error"
	if code != "" {
		message += fmt.Sprintf(" (%s)", code)
	}
	message += ": " + providerError.Message
	return StreamEvent{Type: "error", Code: "model_unavailable", Message: message}, nil
}

func (state *openAIStreamState) appendToolCallFragment(index int, fragment openAIToolCallDelta) error {
	call := state.toolCalls[index]
	if call == nil && len(state.toolCalls) >= maxOpenAIToolCalls {
		return fmt.Errorf("tool call count exceeds %d", maxOpenAIToolCalls)
	}

	identifierBytes := len(fragment.ID) + len(fragment.Function.Name)
	if identifierBytes > maxOpenAIToolCallAggregateIdentifierBytes-state.identifierBytes {
		return fmt.Errorf("tool call ID and name bytes exceed %d", maxOpenAIToolCallAggregateIdentifierBytes)
	}

	argumentBytes := len(fragment.Function.Arguments)
	currentCallArgumentBytes := 0
	if call != nil {
		currentCallArgumentBytes = call.arguments.Len()
	}
	if argumentBytes > maxOpenAIToolCallArgumentBytes-currentCallArgumentBytes {
		return fmt.Errorf("tool call %d arguments exceed %d bytes", index, maxOpenAIToolCallArgumentBytes)
	}
	if argumentBytes > maxOpenAIToolCallAggregateArgumentBytes-state.argumentBytes {
		return fmt.Errorf("aggregate arguments exceed %d bytes", maxOpenAIToolCallAggregateArgumentBytes)
	}

	if call == nil {
		call = &openAIToolCall{}
		state.toolCalls[index] = call
	}
	call.id.WriteString(fragment.ID)
	call.name.WriteString(fragment.Function.Name)
	call.arguments.WriteString(fragment.Function.Arguments)
	state.identifierBytes += identifierBytes
	state.argumentBytes += argumentBytes
	return nil
}
