package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/safejson"
)

const defaultMaxResponseBytes int64 = 1024 * 1024

type Client struct {
	endpoint         string
	token            string
	httpClient       *http.Client
	maxResponseBytes int64
	mu               sync.Mutex
	nextID           int64
}

func NewClient(endpoint string, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{endpoint: endpoint, token: token, httpClient: httpClient, maxResponseBytes: defaultMaxResponseBytes, nextID: 1}
}

func (c *Client) ListTools(ctx context.Context) ([]map[string]any, error) {
	result, err := c.request(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	values, _ := result["tools"].([]any)
	tools := make([]map[string]any, 0, len(values))
	for _, value := range values {
		if tool, ok := value.(map[string]any); ok {
			tools = append(tools, tool)
		}
	}
	return tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, args map[string]any, approvalToken ...string) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	params := map[string]any{"name": name, "arguments": args}
	if len(approvalToken) > 0 && approvalToken[0] != "" {
		params["_meta"] = map[string]any{"approvalToken": approvalToken[0]}
	}
	return c.request(ctx, "tools/call", params)
}

func (c *Client) request(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	id := c.nextRequestID()
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", "application/json")
	if c.token != "" {
		req.Header.Set("authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("MCP HTTP %d", resp.StatusCode)
	}
	obj, err := decodeLimitedObject(resp.Body, c.maxResponseBytes)
	if err != nil {
		return nil, err
	}
	if rawErr, ok := obj["error"]; ok && rawErr != nil {
		errorObj, ok := rawErr.(map[string]any)
		if !ok {
			return nil, errors.New("MCP error")
		}
		message, ok := errorObj["message"].(string)
		if !ok || message == "" {
			return nil, errors.New("MCP error")
		}
		return nil, fmt.Errorf("MCP error: %s", message)
	}
	result, ok := obj["result"]
	if !ok || result == nil {
		return map[string]any{}, nil
	}
	resultObj, ok := result.(map[string]any)
	if !ok {
		return map[string]any{"value": result}, nil
	}
	return resultObj, nil
}

func (c *Client) nextRequestID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	return id
}

func decodeLimitedObject(reader io.Reader, maxBytes int64) (map[string]any, error) {
	limited := io.LimitReader(reader, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("MCP response too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	return safejson.DecodeObject(decoder)
}
