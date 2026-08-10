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

const maxStreamTokenBytes = 1024 * 1024

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
		Messages:    req.Messages,
		Stream:      true,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
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
			message, _ := obj["message"].(map[string]any)
			content, _ := message["content"].(string)
			if content != "" {
				if !sendStreamEvent(ctx, out, StreamEvent{Type: "delta", Text: content}) {
					return
				}
			}
			done, _ := obj["done"].(bool)
			if done {
				reason, _ := obj["done_reason"].(string)
				sendStreamEvent(ctx, out, StreamEvent{Type: "completed", FinishReason: reason})
				return
			}
		}
		if err := scanner.Err(); err != nil {
			sendStreamEvent(ctx, out, StreamEvent{Type: "error", Code: "model_stream_error", Message: err.Error()})
		}
	}()
	return out, nil
}

type ollamaChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"num_predict,omitempty"`
}

func decodeObjectLine(line []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expected JSON object")
	}
	return obj, nil
}

func sendStreamEvent(ctx context.Context, out chan<- StreamEvent, event StreamEvent) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case out <- event:
		return true
	case <-ctx.Done():
		return false
	}
}
