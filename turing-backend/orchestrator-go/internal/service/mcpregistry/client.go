package mcpregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

const maxMCPResponseBytes int64 = 1024 * 1024
const maxMCPTools = 10_000
const maxMCPToolPages = 100
const maxMCPToolBytes = 4 * 1024 * 1024

// maxMCPImportDocumentBytes bounds an entire mcp.json document's raw size,
// checked before it is ever handed to json.Decoder — the same reasoning
// maxMCPResponseBytes already applies to a single live HTTP response, but
// for the whole file a reimport reads instead. Without it, an
// arbitrarily large document (most of it outside any single server's
// "tools" snapshot, so maxMCPToolBytes alone never bounds it) could force
// this process to buffer and decode an unbounded amount of memory. The
// cap is tied to, not independent of, the existing per-snapshot limit:
// mcp.json commonly registers only a handful of servers, and a document
// giving eight of them a full maxMCPToolBytes-sized static snapshot would
// still comfortably fit inside this bound, with slack left over for the
// URL/header/JSON-syntax overhead none of that per-tool accounting counts.
const maxMCPImportDocumentBytes = 8 * maxMCPToolBytes

type mcpClient struct {
	endpoint   string
	token      string
	httpClient *http.Client
	mu         sync.Mutex
	nextID     int64
}

type redactedMCPError struct {
	cause   error
	message string
}

func (e redactedMCPError) Error() string { return e.message }
func (e redactedMCPError) Unwrap() error { return e.cause }

func newMCPClient(endpoint string, token string, httpClient *http.Client) *mcpClient {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &mcpClient{
		endpoint:   endpoint,
		token:      token,
		httpClient: httpClient,
		nextID:     1,
	}
}

func (c *mcpClient) callTool(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}

	return c.request(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": args,
	})
}

func (c *mcpClient) listTools(ctx context.Context) (tools []map[string]any, err error) {
	defer func() {
		err = redactMCPErrorValue(err, c.token)
	}()
	tools = make([]map[string]any, 0)
	params := map[string]any{}
	seenCursors := make(map[string]struct{})
	encodedBytes := 0
	for page := 0; page < maxMCPToolPages; page++ {
		result, err := c.request(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		raw, ok := result["tools"].([]any)
		if !ok {
			return nil, errors.New("MCP tools/list result must contain a tools array")
		}
		if len(raw) > maxMCPTools-len(tools) {
			return nil, fmt.Errorf("MCP tools/list exceeds limit of %d tools", maxMCPTools)
		}
		for index, value := range raw {
			tool, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("MCP tools/list page %d tool %d must be an object", page+1, index)
			}
			encoded, err := json.Marshal(tool)
			if err != nil || len(encoded) > maxMCPToolBytes-encodedBytes {
				return nil, fmt.Errorf("MCP tools/list exceeds encoded descriptor limit of %d bytes", maxMCPToolBytes)
			}
			encodedBytes += len(encoded)
			tools = append(tools, tool)
		}
		cursorValue, present := result["nextCursor"]
		if !present || cursorValue == nil {
			return tools, nil
		}
		cursor, ok := cursorValue.(string)
		if !ok {
			return nil, errors.New("MCP tools/list nextCursor must be a string, null, or absent")
		}
		if _, repeated := seenCursors[cursor]; repeated {
			return nil, fmt.Errorf("MCP tools/list repeated nextCursor %q", cursor)
		}
		seenCursors[cursor] = struct{}{}
		params = map[string]any{"cursor": cursor}
	}
	return nil, fmt.Errorf("MCP tools/list exceeded page limit of %d", maxMCPToolPages)
}

func (c *mcpClient) request(ctx context.Context, method string, params map[string]any) (result map[string]any, err error) {
	defer func() {
		err = redactMCPErrorValue(err, c.token)
	}()
	id := c.nextRequestID()
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("content-type", "application/json")
	if c.token != "" {
		request.Header.Set("authorization", "Bearer "+c.token)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("MCP HTTP %d", response.StatusCode)
	}
	limited := io.LimitReader(response.Body, maxMCPResponseBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxMCPResponseBytes {
		return nil, errors.New("MCP response too large")
	}
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int64           `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int64  `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return nil, err
	}
	if envelope.JSONRPC != "2.0" || envelope.ID != id {
		return nil, errors.New("MCP response does not match request")
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf(
			"MCP error %d: %s",
			envelope.Error.Code,
			redactMCPSecretString(envelope.Error.Message, c.token),
		)
	}
	if len(envelope.Result) == 0 {
		return nil, errors.New("MCP response result is required")
	}
	result = make(map[string]any)
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		return nil, errors.New("MCP response result must be an object")
	}
	if result == nil {
		return nil, errors.New("MCP response result must be an object")
	}
	redacted, ok := redactMCPSecret(result, c.token).(map[string]any)
	if !ok {
		return nil, errors.New("MCP response result must be an object")
	}
	return redacted, nil
}

func redactMCPErrorValue(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	redacted := redactMCPSecretString(err.Error(), secret)
	if redacted == err.Error() {
		return err
	}
	return redactedMCPError{cause: err, message: redacted}
}

func (c *mcpClient) nextRequestID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	return id
}

func redactMCPSecret(value any, secret string) any {
	if secret == "" {
		return value
	}
	switch typed := value.(type) {
	case string:
		return redactMCPSecretString(typed, secret)
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = redactMCPSecret(item, secret)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[redactMCPSecretString(key, secret)] = redactMCPSecret(item, secret)
		}
		return result
	default:
		return value
	}
}

func redactMCPSecretString(value string, secret string) string {
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "[redacted]")
}
