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
	maxStreamTokenBytes            = 1024 * 1024
	maxOllamaToolCalls             = 128
	maxOllamaToolCallArgumentBytes = maxStreamTokenBytes
)

type Ollama struct {
	baseURL string
	client  *http.Client
}

func NewOllama(baseURL string, client *http.Client) *Ollama {
	if client == nil {
		client = http.DefaultClient
	}
	return &Ollama{baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

func (p *Ollama) ID() string { return "ollama" }

func (p *Ollama) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	body, err := json.Marshal(ollamaChatRequest{
		Model:       req.Model,
		Messages:    ollamaMessages(req.Messages),
		Stream:      true,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Tools:       ollamaTools(req.Tools),
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
			sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_unavailable", Message: fmt.Sprintf("Ollama returned %d", resp.StatusCode)})
			return
		}
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), maxStreamTokenBytes)
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
			if rawMessage, present := obj["message"]; present {
				message, ok := rawMessage.(map[string]any)
				if !ok {
					sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: "message must be an object"})
					return
				}
				hasToolCalls := false
				if rawToolCalls, present := message["tool_calls"]; present {
					toolCalls, err := parseOllamaToolCalls(rawToolCalls)
					if err != nil {
						sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: err.Error()})
						return
					}
					if len(toolCalls) > 0 {
						hasToolCalls = true
						if !sendStreamEvent(ctx, out, StreamEvent{Type: "tool_call", ToolCalls: toolCalls}) {
							return
						}
					}
				}
				if !hasToolCalls {
					rawContent, present := message["content"]
					if present {
						content, ok := rawContent.(string)
						if !ok {
							sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: "message content must be a string"})
							return
						}
						if content != "" {
							if !sendStreamEvent(ctx, out, StreamEvent{Type: "delta", Text: content}) {
								return
							}
						}
					}
				}
			}
			if rawDone, present := obj["done"]; present {
				done, ok := rawDone.(bool)
				if !ok {
					sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: "done must be a boolean"})
					return
				}
				if !done {
					continue
				}
				reason := ""
				if rawReason, present := obj["done_reason"]; present {
					var ok bool
					reason, ok = rawReason.(string)
					if !ok {
						sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: "done_reason must be a string"})
						return
					}
				}
				sendStreamEvent(ctx, out, StreamEvent{Type: "completed", FinishReason: reason})
				return
			}
		}
		if err := scanner.Err(); err != nil {
			if ctx.Err() == nil {
				sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_stream_error", Message: err.Error()})
			}
			return
		}
		if ctx.Err() == nil {
			sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_stream_error", Message: "Ollama stream ended before a terminal event"})
		}
	}()
	return out, nil
}

func parseOllamaToolCalls(value any) ([]ToolCall, error) {
	rawCalls, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("tool_calls must be an array")
	}
	if len(rawCalls) > maxOllamaToolCalls {
		return nil, fmt.Errorf("tool call count exceeds %d", maxOllamaToolCalls)
	}
	calls := make([]ToolCall, 0, len(rawCalls))
	for index, rawCall := range rawCalls {
		call, ok := rawCall.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool call %d must be an object", index)
		}
		if _, present := call["id"]; present {
			return nil, fmt.Errorf("tool call %d contains unsupported ID", index)
		}
		if _, present := call["type"]; present {
			return nil, fmt.Errorf("tool call %d contains unsupported type", index)
		}
		function, ok := call["function"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool call %d function must be an object", index)
		}
		name, ok := function["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("tool call %d function name must be a non-empty string", index)
		}
		arguments, ok := function["arguments"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool call %d function arguments must be an object", index)
		}
		encodedArguments, err := json.Marshal(arguments)
		if err != nil {
			return nil, fmt.Errorf("tool call %d function arguments: %w", index, err)
		}
		if len(encodedArguments) > maxOllamaToolCallArgumentBytes {
			return nil, fmt.Errorf("tool call %d arguments exceed %d bytes", index, maxOllamaToolCallArgumentBytes)
		}
		calls = append(calls, ToolCall{Name: name, Arguments: arguments})
	}
	return calls, nil
}

type ollamaChatRequest struct {
	Model       string          `json:"model"`
	Messages    []ollamaMessage `json:"messages"`
	Stream      bool            `json:"stream"`
	Temperature float64         `json:"temperature,omitempty"`
	MaxTokens   int             `json:"num_predict,omitempty"`
	Tools       []ollamaTool    `json:"tools,omitempty"`
}

type ollamaMessage struct {
	Role      string                  `json:"role"`
	Content   string                  `json:"content"`
	ToolName  string                  `json:"tool_name,omitempty"`
	ToolCalls []ollamaMessageToolCall `json:"tool_calls,omitempty"`
}

type ollamaMessageToolCall struct {
	Function ollamaFunctionCall `json:"function"`
}

type ollamaFunctionCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func ollamaMessages(messages []ChatMessage) []ollamaMessage {
	converted := make([]ollamaMessage, 0, len(messages))
	for _, message := range messages {
		result := ollamaMessage{Role: message.Role, Content: message.Content}
		if message.Role == "tool" {
			result.ToolName = message.Name
		} else if message.Role == "assistant" {
			for _, call := range message.ToolCalls {
				arguments := call.Arguments
				if arguments == nil {
					arguments = map[string]any{}
				}
				result.ToolCalls = append(result.ToolCalls, ollamaMessageToolCall{
					Function: ollamaFunctionCall{Name: call.Name, Arguments: arguments},
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
