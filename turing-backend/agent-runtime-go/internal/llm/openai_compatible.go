package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	maxOpenAIEventDataBytes        = maxStreamTokenBytes
	maxOpenAIToolCallArgumentBytes = maxStreamTokenBytes
)

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
		toolCalls := make(map[int]*openAIToolCall)
		dispatch := func(data string) bool {
			if strings.TrimSpace(data) == "" {
				return false
			}
			if strings.TrimSpace(data) == "[DONE]" {
				if len(toolCalls) > 0 {
					sendStreamEvent(ctx, out, StreamEvent{
						Type:    "error",
						Code:    "model_bad_chunk",
						Message: fmt.Sprintf("[DONE] received with %d unfinished tool call(s)", len(toolCalls)),
					})
				} else {
					sendStreamEvent(ctx, out, StreamEvent{Type: "completed", FinishReason: "stop"})
				}
				return true
			}
			events, done, err := parseOpenAIData([]byte(data), toolCalls)
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
				sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_stream_error", Message: err.Error()})
			}
			return
		}
		if ctx.Err() != nil {
			return
		}
		message := "OpenAI-compatible stream ended before a terminal event"
		if len(toolCalls) > 0 {
			message += fmt.Sprintf(" with %d unfinished tool call(s)", len(toolCalls))
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

type openAIChunk struct {
	Choices []struct {
		Delta        json.RawMessage `json:"delta"`
		FinishReason *string         `json:"finish_reason"`
	} `json:"choices"`
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
	id        string
	name      string
	arguments strings.Builder
}

func parseOpenAIData(data []byte, toolCalls map[int]*openAIToolCall) ([]StreamEvent, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var chunk openAIChunk
	if err := decoder.Decode(&chunk); err != nil {
		return nil, false, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return nil, false, fmt.Errorf("malformed chunk: %w", err)
	}
	if len(chunk.Choices) == 0 {
		return nil, false, nil
	}
	choice := chunk.Choices[0]
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
			call := toolCalls[index]
			if call == nil {
				call = &openAIToolCall{}
				toolCalls[index] = call
			}
			call.id += fragment.ID
			call.name += fragment.Function.Name
			if len(fragment.Function.Arguments) > maxOpenAIToolCallArgumentBytes-call.arguments.Len() {
				return nil, false, fmt.Errorf("tool call %d arguments exceed %d bytes", index, maxOpenAIToolCallArgumentBytes)
			}
			call.arguments.WriteString(fragment.Function.Arguments)
		}
		if delta.Content != nil && *delta.Content != "" {
			events = append(events, StreamEvent{Type: "delta", Text: *delta.Content})
		}
	}
	if choice.FinishReason != nil {
		if *choice.FinishReason == "tool_calls" {
			if len(toolCalls) == 0 {
				return nil, false, fmt.Errorf("finish_reason tool_calls received without any accumulated tool calls")
			}
			calls := make([]ToolCall, 0, len(toolCalls))
			ids := make(map[string]struct{}, len(toolCalls))
			for index := 0; index < len(toolCalls); index++ {
				call, ok := toolCalls[index]
				if !ok {
					return nil, false, fmt.Errorf("tool call indices are non-contiguous: missing index %d", index)
				}
				if call.id == "" {
					return nil, false, fmt.Errorf("tool call %d is missing an ID", index)
				}
				if _, duplicate := ids[call.id]; duplicate {
					return nil, false, fmt.Errorf("tool call %d has duplicate ID %q", index, call.id)
				}
				ids[call.id] = struct{}{}
				if call.name == "" {
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
				calls = append(calls, ToolCall{ID: call.id, Name: call.name, Arguments: arguments})
			}
			events = append(events, StreamEvent{Type: "tool_call", ToolCalls: calls})
		} else if len(toolCalls) > 0 {
			return nil, false, fmt.Errorf("finish_reason %q received with unfinished tool calls", *choice.FinishReason)
		}
		events = append(events, StreamEvent{Type: "completed", FinishReason: *choice.FinishReason})
		return events, true, nil
	}
	return events, false, nil
}
