package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const expectedSystemRequestLimit = 1024 * 1024

func TestMcpHandlerRequiresBearerToken(t *testing.T) {
	handler := newHandler("system-token")

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without bearer token, got %d", res.Code)
	}
}

func TestMcpHandlerListsSystemTools(t *testing.T) {
	handler := newHandler("system-token")

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer system-token")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected OK, got %d: %s", res.Code, res.Body.String())
	}
	if !bytes.Contains(res.Body.Bytes(), []byte(`"system.health"`)) {
		t.Fatalf("expected tools/list response to include system.health, got %s", res.Body.String())
	}
}

func TestMcpHandlerCallsSystemTool(t *testing.T) {
	handler := newHandler("system-token")

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"system.echo","arguments":{"text":"hello"}}}`))
	req.Header.Set("Authorization", "Bearer system-token")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected OK, got %d: %s", res.Code, res.Body.String())
	}
	if !bytes.Contains(res.Body.Bytes(), []byte(`"text":"hello"`)) {
		t.Fatalf("expected system.echo result, got %s", res.Body.String())
	}
}

func TestMcpHandlerRejectsOversizedRequestBody(t *testing.T) {
	handler := newHandler("system-token")
	for name, body := range map[string]string{
		"valid JSON":      `{"jsonrpc":"2.0","id":1,"method":"tools/list","padding":"` + strings.Repeat("x", expectedSystemRequestLimit) + `"}`,
		"early malformed": `x` + strings.Repeat(" ", expectedSystemRequestLimit),
	} {
		t.Run(name, func(t *testing.T) {
			status, response := callSystemMCP(t, handler, body)
			if status != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want %d; response=%s", status, http.StatusRequestEntityTooLarge, response)
			}
			assertRPCErrorCode(t, response, -32600)
		})
	}
}

func TestMcpHandlerStrictlyValidatesJSONRPCEnvelope(t *testing.T) {
	handler := newHandler("system-token")
	tests := []struct {
		name string
		body string
		code int
	}{
		{name: "malformed JSON", body: `{`, code: -32700},
		{name: "root array", body: `[]`, code: -32600},
		{name: "wrong version", body: `{"jsonrpc":"1.0","id":1,"method":"tools/list"}`, code: -32600},
		{name: "missing id", body: `{"jsonrpc":"2.0","method":"tools/list"}`, code: -32600},
		{name: "null id", body: `{"jsonrpc":"2.0","id":null,"method":"tools/list"}`, code: -32600},
		{name: "boolean id", body: `{"jsonrpc":"2.0","id":true,"method":"tools/list"}`, code: -32600},
		{name: "fractional id", body: `{"jsonrpc":"2.0","id":1.5,"method":"tools/list"}`, code: -32600},
		{name: "missing method", body: `{"jsonrpc":"2.0","id":1}`, code: -32600},
		{name: "empty method", body: `{"jsonrpc":"2.0","id":1,"method":""}`, code: -32600},
		{name: "array params", body: `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":[]}`, code: -32602},
		{name: "null params", body: `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":null}`, code: -32602},
		{name: "unknown envelope field", body: `{"jsonrpc":"2.0","id":1,"method":"tools/list","extra":true}`, code: -32600},
		{name: "second object", body: `{"jsonrpc":"2.0","id":1,"method":"tools/list"} {}`, code: -32600},
		{name: "trailing garbage", body: `{"jsonrpc":"2.0","id":1,"method":"tools/list"} garbage`, code: -32700},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, response := callSystemMCP(t, handler, test.body)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200; response=%s", status, response)
			}
			assertRPCErrorCode(t, response, test.code)
		})
	}
}

func TestMcpHandlerRejectsNonObjectToolArguments(t *testing.T) {
	handler := newHandler("system-token")
	for _, arguments := range []string{`null`, `"text"`, `[]`, `1`, `true`} {
		t.Run(arguments, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"system.health","arguments":` + arguments + `}}`
			status, response := callSystemMCP(t, handler, body)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200; response=%s", status, response)
			}
			assertRPCErrorCode(t, response, -32602)
		})
	}
}

func TestMcpHandlerRejectsMalformedToolCallParams(t *testing.T) {
	handler := newHandler("system-token")
	for name, params := range map[string]string{
		"missing name":        `{}`,
		"non-string name":     `{"name":1}`,
		"empty name":          `{"name":" "}`,
		"non-object metadata": `{"name":"system.health","_meta":[]}`,
		"unknown parameter":   `{"name":"system.health","unexpected":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":` + params + `}`
			status, response := callSystemMCP(t, handler, body)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200; response=%s", status, response)
			}
			assertRPCErrorCode(t, response, -32602)
		})
	}
}

func TestMcpHandlerRejectsMalformedToolsListParams(t *testing.T) {
	handler := newHandler("system-token")
	for name, params := range map[string]string{
		"non-string cursor":   `{"cursor":1}`,
		"non-object metadata": `{"_meta":[]}`,
		"unknown parameter":   `{"unexpected":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":` + params + `}`
			status, response := callSystemMCP(t, handler, body)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200; response=%s", status, response)
			}
			assertRPCErrorCode(t, response, -32602)
		})
	}
}

func TestMcpHandlerAllowsOmittedToolArguments(t *testing.T) {
	handler := newHandler("system-token")

	status, response := callSystemMCP(t, handler, `{"jsonrpc":"2.0","id":"request-1","method":"tools/call","params":{"name":"system.health"}}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; response=%s", status, response)
	}
	var envelope struct {
		ID     string         `json:"id"`
		Result map[string]any `json:"result"`
		Error  map[string]any `json:"error"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.ID != "request-1" || envelope.Result == nil || envelope.Error != nil {
		t.Fatalf("response = %s, want successful correlated result", response)
	}
}

func callSystemMCP(t *testing.T, handler http.Handler, body string) (int, []byte) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+"system-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response.Code, response.Body.Bytes()
}

func assertRPCErrorCode(t *testing.T, response []byte, want int) {
	t.Helper()
	var envelope struct {
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatalf("decode response %q: %v", response, err)
	}
	if envelope.Error == nil || envelope.Error.Code != want {
		t.Fatalf("response = %s, want JSON-RPC error code %d", response, want)
	}
}
