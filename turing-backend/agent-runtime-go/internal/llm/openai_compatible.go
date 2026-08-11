package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	body, err := json.Marshal(openAIChatRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Stream:      true,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
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
			event, done, err := parseOpenAIData([]byte(data))
			if err != nil {
				sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_bad_chunk", Message: err.Error()})
				return
			}
			if event.Type != "" {
				if !sendStreamEvent(ctx, out, event) {
					return
				}
			}
			if done {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_stream_error", Message: err.Error()})
		}
	}()
	return out, nil
}

type openAIChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

type openAIChunk struct {
	Choices []struct {
		Delta        json.RawMessage `json:"delta"`
		FinishReason *string         `json:"finish_reason"`
	} `json:"choices"`
}

type openAIDelta struct {
	Content *string `json:"content"`
}

func parseOpenAIData(data []byte) (StreamEvent, bool, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var chunk openAIChunk
	if err := decoder.Decode(&chunk); err != nil {
		return StreamEvent{}, false, err
	}
	if len(chunk.Choices) == 0 {
		return StreamEvent{}, false, nil
	}
	choice := chunk.Choices[0]
	if len(choice.Delta) > 0 && string(choice.Delta) != "null" {
		var delta openAIDelta
		decoder := json.NewDecoder(bytes.NewReader(choice.Delta))
		decoder.UseNumber()
		if err := decoder.Decode(&delta); err != nil {
			return StreamEvent{}, false, err
		}
		if delta.Content != nil && *delta.Content != "" {
			return StreamEvent{Type: "delta", Text: *delta.Content}, false, nil
		}
	}
	if choice.FinishReason != nil {
		return StreamEvent{Type: "completed", FinishReason: *choice.FinishReason}, true, nil
	}
	return StreamEvent{}, false, nil
}
