package mcp

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

	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/safejson"
)

const (
	defaultMaxResponseBytes  int64 = 1024 * 1024
	maxListToolsPages              = 100
	maxListToolsTotalCount         = 10_000
	maxListToolsEncodedBytes       = 4 * 1024 * 1024
)

type Client struct {
	endpoint         string
	token            string
	httpClient       *http.Client
	maxResponseBytes int64
	mu               sync.Mutex
	nextID           int64
}

type RetryableError interface {
	error
	Retryable() bool
}

type classifiedError struct {
	err       error
	retryable bool
}

func (e classifiedError) Error() string   { return e.err.Error() }
func (e classifiedError) Unwrap() error   { return e.err }
func (e classifiedError) Retryable() bool { return e.retryable }

type JSONRPCError struct {
	Code    int64
	Message string
}

func (e JSONRPCError) Error() string {
	return fmt.Sprintf("MCP error %d: %s", e.Code, e.Message)
}

type ToolCallError struct {
	Result map[string]any
}

func (e ToolCallError) Error() string {
	message := "MCP tool call failed"
	content, ok := e.Result["content"].([]any)
	if !ok {
		return message
	}
	for _, item := range content {
		block, ok := item.(map[string]any)
		if !ok || block["type"] != "text" {
			continue
		}
		text, ok := block["text"].(string)
		if ok && strings.TrimSpace(text) != "" {
			return message + ": " + text
		}
	}
	return message
}

func Retryable(err error) bool {
	var classified RetryableError
	return errors.As(err, &classified) && classified.Retryable()
}

func NewClient(endpoint string, token string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{endpoint: endpoint, token: token, httpClient: httpClient, maxResponseBytes: defaultMaxResponseBytes, nextID: 1}
}

func (c *Client) ListTools(ctx context.Context) (tools []map[string]any, err error) {
	defer func() {
		err = classifyListToolsError(err)
	}()

	tools = make([]map[string]any, 0)
	encodedToolBytes := 0
	params := map[string]any{}
	seenCursors := make(map[string]struct{})
	for page := 0; page < maxListToolsPages; page++ {
		result, err := c.request(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		rawTools, present := result["tools"]
		values, ok := rawTools.([]any)
		if !present || !ok {
			return nil, fmt.Errorf("MCP tools/list page %d tools must be present and an array", page+1)
		}
		if len(values) > maxListToolsTotalCount-len(tools) {
			return nil, fmt.Errorf(
				"MCP tools/list page %d total tool count exceeds limit of %d",
				page+1,
				maxListToolsTotalCount,
			)
		}
		pageTools := make([]map[string]any, len(values))
		for index, value := range values {
			tool, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("MCP tools/list page %d tool %d must be an object", page+1, index)
			}
			encoded, err := json.Marshal(tool)
			if err != nil {
				return nil, fmt.Errorf("MCP tools/list page %d tool %d cannot be encoded: %w", page+1, index, err)
			}
			if len(encoded) > maxListToolsEncodedBytes-encodedToolBytes {
				return nil, fmt.Errorf(
					"MCP tools/list page %d tool %d makes aggregate encoded tool bytes exceed limit of %d",
					page+1,
					index,
					maxListToolsEncodedBytes,
				)
			}
			encodedToolBytes += len(encoded)
			pageTools[index] = tool
		}
		tools = append(tools, pageTools...)

		rawCursor, present := result["nextCursor"]
		if !present || rawCursor == nil {
			return tools, nil
		}
		cursor, ok := rawCursor.(string)
		if !ok {
			return nil, fmt.Errorf("MCP tools/list nextCursor must be a string, null, or absent")
		}
		if _, repeated := seenCursors[cursor]; repeated {
			return nil, fmt.Errorf("MCP tools/list returned repeated nextCursor %q", cursor)
		}
		seenCursors[cursor] = struct{}{}
		params = map[string]any{"cursor": cursor}
	}
	return nil, fmt.Errorf("MCP tools/list exceeded page limit of %d", maxListToolsPages)
}

func (c *Client) CallTool(ctx context.Context, name string, args map[string]any, approvalToken ...string) (map[string]any, error) {
	if args == nil {
		args = map[string]any{}
	}
	params := map[string]any{"name": name, "arguments": args}
	if len(approvalToken) > 0 && approvalToken[0] != "" {
		params["_meta"] = map[string]any{"approvalToken": approvalToken[0]}
	}
	result, err := c.request(ctx, "tools/call", params)
	if err != nil {
		return nil, err
	}
	if isError, ok := result["isError"].(bool); ok && isError {
		return nil, nonRetryableError(ToolCallError{Result: result})
	}
	return result, nil
}

func (c *Client) request(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	id := c.nextRequestID()
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, nonRetryableError(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, nonRetryableError(err)
	}
	req.Header.Set("content-type", "application/json")
	if c.token != "" {
		req.Header.Set("authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctxErr := directContextError(ctx, err); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, retryableError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := fmt.Errorf("MCP HTTP %d", resp.StatusCode)
		retryable := resp.StatusCode == http.StatusRequestTimeout ||
			resp.StatusCode == http.StatusTooManyRequests ||
			(resp.StatusCode >= http.StatusInternalServerError && resp.StatusCode < 600)
		return nil, classifiedError{err: err, retryable: retryable}
	}
	obj, err := decodeLimitedObject(resp.Body, c.maxResponseBytes)
	if err != nil {
		return nil, err
	}
	if version, ok := obj["jsonrpc"].(string); !ok || version != "2.0" {
		return nil, nonRetryableError(errors.New("MCP response jsonrpc must be \"2.0\""))
	}
	responseID, ok := obj["id"].(json.Number)
	if !ok {
		return nil, nonRetryableError(errors.New("MCP response id must be a request ID"))
	}
	responseIDValue, err := responseID.Int64()
	if err != nil || responseIDValue != id {
		return nil, nonRetryableError(fmt.Errorf("MCP response id does not match request ID %d", id))
	}
	result, hasResult := obj["result"]
	rawErr, hasError := obj["error"]
	if hasResult && hasError {
		return nil, nonRetryableError(errors.New("MCP response must not contain both result and error"))
	}
	if hasError {
		errorObj, ok := rawErr.(map[string]any)
		if !ok {
			return nil, nonRetryableError(errors.New("MCP error"))
		}
		codeNumber, ok := errorObj["code"].(json.Number)
		if !ok {
			return nil, nonRetryableError(errors.New("MCP error code must be an integer"))
		}
		code, err := codeNumber.Int64()
		if err != nil {
			return nil, nonRetryableError(errors.New("MCP error code must be an integer"))
		}
		message, ok := errorObj["message"].(string)
		if !ok || message == "" {
			return nil, nonRetryableError(errors.New("MCP error"))
		}
		rpcErr := JSONRPCError{Code: code, Message: message}
		retryable := code == -32603 || (code >= -32099 && code <= -32000)
		return nil, classifiedError{err: rpcErr, retryable: retryable}
	}
	if !hasResult {
		return nil, nonRetryableError(errors.New("MCP response must contain result or error"))
	}
	resultObj, ok := result.(map[string]any)
	if !ok {
		return nil, nonRetryableError(errors.New("MCP response result must be an object"))
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
		return nil, retryableError(err)
	}
	if int64(len(data)) > maxBytes {
		return nil, nonRetryableError(errors.New("MCP response too large"))
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	obj, err := safejson.DecodeObject(decoder)
	if err != nil {
		return nil, nonRetryableError(err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = errors.New("MCP response contains multiple JSON values")
		}
		return nil, nonRetryableError(fmt.Errorf("MCP response contains trailing data: %w", err))
	}
	return obj, nil
}

func classifyListToolsError(err error) error {
	if err == nil {
		return nil
	}
	if direct := directContextError(nil, err); direct != nil {
		return direct
	}
	var classified RetryableError
	if errors.As(err, &classified) {
		return err
	}
	return nonRetryableError(err)
}

func directContextError(ctx context.Context, err error) error {
	if ctx != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
	}
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func retryableError(err error) error {
	return classifiedError{err: err, retryable: true}
}

func nonRetryableError(err error) error {
	return classifiedError{err: err}
}
