package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
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
		toolCalls := make(map[int]*openAIToolCall)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			if data == "[DONE]" {
				sendStreamEvent(ctx, out, StreamEvent{Type: "completed", FinishReason: "stop"})
				return
			}
			events, done, err := parseOpenAIData([]byte(data), toolCalls)
			if err != nil {
				sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: err.Error()})
				return
			}
			for _, event := range events {
				if !sendStreamEvent(ctx, out, event) {
					return
				}
			}
			if done {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			if ctx.Err() == nil {
				sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_stream_error", Message: err.Error()})
			}
		}
	}()
	return out, nil
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
				arguments, err := json.Marshal(call.Arguments)
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
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type openAIToolCall struct {
	id        string
	name      string
	arguments string
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
	if len(choice.Delta) > 0 && string(choice.Delta) != "null" {
		var delta openAIDelta
		decoder := json.NewDecoder(bytes.NewReader(choice.Delta))
		decoder.UseNumber()
		if err := decoder.Decode(&delta); err != nil {
			return nil, false, err
		}
		for _, fragment := range delta.ToolCalls {
			call := toolCalls[fragment.Index]
			if call == nil {
				call = &openAIToolCall{}
				toolCalls[fragment.Index] = call
			}
			call.id += fragment.ID
			call.name += fragment.Function.Name
			call.arguments += fragment.Function.Arguments
		}
		if delta.Content != nil && *delta.Content != "" {
			return []StreamEvent{{Type: "delta", Text: *delta.Content}}, false, nil
		}
	}
	if choice.FinishReason != nil {
		if *choice.FinishReason == "tool_calls" {
			calls := make([]ToolCall, 0, len(toolCalls))
			indices := make([]int, 0, len(toolCalls))
			for index := range toolCalls {
				indices = append(indices, index)
			}
			sort.Ints(indices)
			for _, index := range indices {
				call := toolCalls[index]
				arguments := make(map[string]any)
				if call.arguments != "" {
					decoder := json.NewDecoder(strings.NewReader(call.arguments))
					decoder.UseNumber()
					var decoded any
					if err := decoder.Decode(&decoded); err != nil {
						return nil, false, err
					}
					if err := decoder.Decode(&struct{}{}); err != io.EOF {
						if err == nil {
							err = fmt.Errorf("multiple JSON values")
						}
						return nil, false, fmt.Errorf("malformed tool call arguments: %w", err)
					}
					var ok bool
					arguments, ok = decoded.(map[string]any)
					if !ok {
						return nil, false, fmt.Errorf("tool call arguments must be a JSON object")
					}
				}
				calls = append(calls, ToolCall{ID: call.id, Name: call.name, Arguments: arguments})
			}
			return []StreamEvent{
				{Type: "tool_call", ToolCalls: calls},
				{Type: "completed", FinishReason: *choice.FinishReason},
			}, true, nil
		}
		return []StreamEvent{{Type: "completed", FinishReason: *choice.FinishReason}}, true, nil
	}
	return nil, false, nil
}
