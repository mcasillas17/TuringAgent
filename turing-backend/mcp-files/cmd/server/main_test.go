package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/project-turing/mcp-files/internal/jsonrpc"
	filetools "github.com/project-turing/mcp-files/internal/tools"
)

const expectedFilesRequestLimit = 6*524288 + 64*1024
const expectedMCPResponseLimit = 1024 * 1024
const expectedAggregateToolResultLimit = 4 * 1024 * 1024
const expectedToolCallsPerRun = 10

func TestLoadConfigRequiresApprovalJWTSecret(t *testing.T) {
	t.Setenv("TURING_APPROVAL_JWT_SECRET", "")

	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "TURING_APPROVAL_JWT_SECRET is required") {
		t.Fatalf("loadConfig error = %v", err)
	}
}

func TestLoadConfigAcceptsConfiguredApprovalJWTSecret(t *testing.T) {
	t.Setenv("TURING_APPROVAL_JWT_SECRET", "approval-secret")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}
	if cfg.approvalJwtSecret != "approval-secret" {
		t.Fatalf("approvalJwtSecret = %q", cfg.approvalJwtSecret)
	}
}

func TestHTTPServerConfiguresConnectionTimeouts(t *testing.T) {
	server := newHTTPServer(":7110", http.NotFoundHandler())

	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %v, want 5s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 30*time.Second {
		t.Fatalf("ReadTimeout = %v, want 30s", server.ReadTimeout)
	}
	if server.WriteTimeout != 2*time.Minute {
		t.Fatalf("WriteTimeout = %v, want 2m", server.WriteTimeout)
	}
	if server.IdleTimeout != 60*time.Second {
		t.Fatalf("IdleTimeout = %v, want 60s", server.IdleTimeout)
	}
}

func TestMcpHandlerRejectsUnauthorizedRequests(t *testing.T) {
	handler := newHandler(serverConfig{
		filesToken:           "files-token",
		approvalJwtSecret:    "jwt-secret",
		orchestratorGRPCAddr: "orchestrator:3001",
		internalToken:        "internal-token",
		sandboxRoot:          t.TempDir(),
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized without bearer token, got %d", res.Code)
	}
}

func TestHealthEndpointIsAvailableWithoutBearerToken(t *testing.T) {
	handler := newHandler(serverConfig{
		filesToken:           "files-token",
		approvalJwtSecret:    "jwt-secret",
		orchestratorGRPCAddr: "orchestrator:3001",
		internalToken:        "internal-token",
		sandboxRoot:          t.TempDir(),
	})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("health status = %d, want %d", res.Code, http.StatusNoContent)
	}
}

func TestHealthcheckProbeAcceptsReadyEndpoint(t *testing.T) {
	handler := newHandler(serverConfig{
		filesToken:           "files-token",
		approvalJwtSecret:    "jwt-secret",
		orchestratorGRPCAddr: "orchestrator:3001",
		internalToken:        "internal-token",
		sandboxRoot:          t.TempDir(),
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	if err := checkHealth(context.Background(), server.URL+"/healthz"); err != nil {
		t.Fatalf("checkHealth failed: %v", err)
	}
}

func TestMcpHandlerListsFilesTools(t *testing.T) {
	handler := newHandler(serverConfig{
		filesToken:           "files-token",
		approvalJwtSecret:    "jwt-secret",
		orchestratorGRPCAddr: "orchestrator:3001",
		internalToken:        "internal-token",
		sandboxRoot:          t.TempDir(),
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Authorization", "Bearer files-token")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected OK, got %d: %s", res.Code, res.Body.String())
	}
	if !bytes.Contains(res.Body.Bytes(), []byte(`"files.read"`)) {
		t.Fatalf("expected tools/list response to include files.read, got %s", res.Body.String())
	}
}

func TestMcpHandlerCallsFilesReadTool(t *testing.T) {
	sandbox := t.TempDir()
	if err := os.WriteFile(filepath.Join(sandbox, "note.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(serverConfig{
		filesToken:           "files-token",
		approvalJwtSecret:    "jwt-secret",
		orchestratorGRPCAddr: "orchestrator:3001",
		internalToken:        "internal-token",
		sandboxRoot:          sandbox,
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.read","arguments":{"path":"note.txt"}}}`))
	req.Header.Set("Authorization", "Bearer files-token")
	res := httptest.NewRecorder()

	handler.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected OK, got %d: %s", res.Code, res.Body.String())
	}
	if !bytes.Contains(res.Body.Bytes(), []byte(`"content":"hello"`)) {
		t.Fatalf("expected files.read result, got %s", res.Body.String())
	}
}

func TestMcpHandlerRejectsOversizedRequestBody(t *testing.T) {
	handler := testFilesHandler(t)
	for name, body := range map[string]string{
		"valid JSON":      `{"jsonrpc":"2.0","id":1,"method":"tools/list","padding":"` + strings.Repeat("x", expectedFilesRequestLimit) + `"}`,
		"early malformed": `x` + strings.Repeat(" ", expectedFilesRequestLimit),
	} {
		t.Run(name, func(t *testing.T) {
			status, response := callFilesMCP(t, handler, body)
			if status != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want %d; response=%s", status, http.StatusRequestEntityTooLarge, response)
			}
			assertRPCErrorCode(t, response, -32600)
		})
	}
}

func TestMcpHandlerAllowsWorstCaseEscapedMaximumFileContent(t *testing.T) {
	handler := testFilesHandler(t)
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "files.create",
			"arguments": map[string]any{
				"path":    "note.txt",
				"content": strings.Repeat("\x01", filetools.MaxMutationContentBytes),
			},
			"_meta": map[string]any{"approvalToken": "invalid"},
		},
	}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > expectedFilesRequestLimit {
		t.Fatalf("test envelope size = %d, exceeds expected request limit %d", len(body), expectedFilesRequestLimit)
	}

	status, response := callFilesMCP(t, handler, string(body))

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; response=%s", status, response)
	}
	assertRPCErrorCode(t, response, -32000)
}

func TestAdvertisedReadLimitFitsTransportAndAggregateBudgets(t *testing.T) {
	const wantReadLimit = 64 * 1024
	readSchema := toolPropertySchema(t, listTools(), "files.read", "maxBytes")
	advertisedMaximum, ok := readSchema["maximum"].(int)
	if !ok {
		t.Fatalf("files.read maxBytes maximum = %#v, want integer", readSchema["maximum"])
	}
	if advertisedMaximum != wantReadLimit {
		t.Errorf("files.read maximum = %d, want %d", advertisedMaximum, wantReadLimit)
	}
	pathComponents := make([]string, 17)
	for index := range pathComponents {
		pathComponents[index] = strings.Repeat("\x01", 240)
	}
	path := strings.Join(pathComponents, "/")
	if len(path) != filetools.MaxSandboxPathBytes {
		t.Fatalf("worst-case path fixture = %d bytes, want %d", len(path), filetools.MaxSandboxPathBytes)
	}
	result := map[string]any{
		"path":      path,
		"content":   strings.Repeat("\x01", advertisedMaximum),
		"truncated": false,
		"bytesRead": advertisedMaximum,
	}
	encodedResult, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	encodedResponse, err := json.Marshal(jsonrpc.Response{JSONRPC: "2.0", ID: float64(1), Result: result})
	if err != nil {
		t.Fatal(err)
	}
	if len(encodedResponse) > expectedMCPResponseLimit {
		t.Errorf("worst-case files.read response = %d bytes, exceeds %d-byte MCP cap", len(encodedResponse), expectedMCPResponseLimit)
	}
	if len(encodedResult)*expectedToolCallsPerRun > expectedAggregateToolResultLimit {
		t.Errorf(
			"%d worst-case results total %d bytes, exceed %d-byte run aggregate",
			expectedToolCallsPerRun,
			len(encodedResult)*expectedToolCallsPerRun,
			expectedAggregateToolResultLimit,
		)
	}
}

func TestMcpResponseWriterRejectsOversizedEncodedResponse(t *testing.T) {
	response := httptest.NewRecorder()

	writeJSONRPC(response, jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      float64(1),
		Result:  map[string]any{"content": strings.Repeat("\x01", expectedMCPResponseLimit)},
	})

	if response.Body.Len() > expectedMCPResponseLimit {
		t.Fatalf("response body = %d bytes, exceeds %d-byte cap", response.Body.Len(), expectedMCPResponseLimit)
	}
	assertRPCErrorCode(t, response.Body.Bytes(), -32603)
}

func TestMcpResponseWriterDropsOversizedIDFromFallback(t *testing.T) {
	response := httptest.NewRecorder()

	writeJSONRPC(response, jsonrpc.Response{
		JSONRPC: "2.0",
		ID:      strings.Repeat("x", expectedMCPResponseLimit),
		Result:  map[string]any{"text": "ok"},
	})

	if response.Body.Len() > expectedMCPResponseLimit {
		t.Fatalf("response body = %d bytes, exceeds %d-byte cap", response.Body.Len(), expectedMCPResponseLimit)
	}
	var envelope struct {
		ID any `json:"id"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.ID != nil {
		t.Fatalf("fallback response id = %#v, want null", envelope.ID)
	}
}

func TestMcpHandlerStrictlyValidatesJSONRPCEnvelope(t *testing.T) {
	handler := testFilesHandler(t)
	tests := []struct {
		name string
		body string
		code int
	}{
		{name: "malformed JSON", body: `{`, code: -32700},
		{name: "root array", body: `[]`, code: -32600},
		{name: "wrong version", body: `{"jsonrpc":"1.0","id":1,"method":"tools/list"}`, code: -32600},
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
			status, response := callFilesMCP(t, handler, test.body)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200; response=%s", status, response)
			}
			assertRPCErrorCode(t, response, test.code)
		})
	}
}

func TestMcpHandlerAcceptsNotificationWithoutResponse(t *testing.T) {
	handler := testFilesHandler(t)

	status, response := callFilesMCP(t, handler, `{"jsonrpc":"2.0","method":"tools/list"}`)

	if status != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; response=%s", status, response)
	}
	if len(response) != 0 {
		t.Fatalf("notification response = %q, want empty body", response)
	}
}

func TestMcpHandlerRejectsNonObjectToolArguments(t *testing.T) {
	handler := testFilesHandler(t)
	for _, arguments := range []string{`null`, `"text"`, `[]`, `1`, `true`} {
		t.Run(arguments, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.list","arguments":` + arguments + `}}`
			status, response := callFilesMCP(t, handler, body)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200; response=%s", status, response)
			}
			assertRPCErrorCode(t, response, -32602)
		})
	}
}

func TestMcpHandlerRejectsMalformedToolCallParams(t *testing.T) {
	handler := testFilesHandler(t)
	for name, params := range map[string]string{
		"missing name":              `{}`,
		"non-string name":           `{"name":1}`,
		"empty name":                `{"name":" "}`,
		"non-object metadata":       `{"name":"files.list","_meta":[]}`,
		"non-string approval token": `{"name":"files.list","_meta":{"approvalToken":1}}`,
		"unknown parameter":         `{"name":"files.list","unexpected":true}`,
		"unknown metadata":          `{"name":"files.list","_meta":{"unexpected":true}}`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":` + params + `}`
			status, response := callFilesMCP(t, handler, body)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200; response=%s", status, response)
			}
			assertRPCErrorCode(t, response, -32602)
		})
	}
}

func TestMcpHandlerMapsToolArgumentErrorsToInvalidParams(t *testing.T) {
	handler := testFilesHandler(t)
	for name, body := range map[string]string{
		"missing required argument": `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.read"}}`,
		"unknown argument":          `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.list","arguments":{"unexpected":true}}}`,
		"wrong argument type":       `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.read","arguments":{"path":1}}}`,
		"invalid argument value":    `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.list","arguments":{"limit":0}}}`,
		"escaping path":             `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.read","arguments":{"path":"../outside.txt"}}}`,
		"unknown tool":              `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.missing"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			status, response := callFilesMCP(t, handler, body)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200; response=%s", status, response)
			}
			assertRPCErrorCode(t, response, -32602)
		})
	}
}

func TestMcpHandlerKeepsOperationalToolErrorsAsServerErrors(t *testing.T) {
	handler := testFilesHandler(t)

	status, response := callFilesMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.read","arguments":{"path":"missing.txt"}}}`)

	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; response=%s", status, response)
	}
	assertRPCErrorCode(t, response, -32000)
}

func TestMcpHandlerRejectsMalformedToolsListParams(t *testing.T) {
	handler := testFilesHandler(t)
	for name, params := range map[string]string{
		"non-string cursor":   `{"cursor":1}`,
		"non-object metadata": `{"_meta":[]}`,
		"unknown parameter":   `{"unexpected":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":` + params + `}`
			status, response := callFilesMCP(t, handler, body)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200; response=%s", status, response)
			}
			assertRPCErrorCode(t, response, -32602)
		})
	}
}

func TestMcpHandlerAllowsOmittedToolArguments(t *testing.T) {
	handler := testFilesHandler(t)

	status, response := callFilesMCP(t, handler, `{"jsonrpc":"2.0","id":"request-1","method":"tools/call","params":{"name":"files.list"}}`)

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

func TestListToolsAdvertisesOnlyCallableToolsWithAccurateSchemas(t *testing.T) {
	tools := listTools()
	wantRequired := map[string][]any{
		"files.list":   {},
		"files.search": {"query"},
		"files.read":   {"path"},
		"files.create": {"path", "content"},
		"files.update": {"path", "content"},
	}
	if len(tools) != len(wantRequired) {
		t.Fatalf("listTools returned %d tools, want %d: %#v", len(tools), len(wantRequired), tools)
	}
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		required, advertised := wantRequired[name]
		if !advertised {
			t.Errorf("disabled or unknown tool %q was advertised", name)
			continue
		}
		description, _ := tool["description"].(string)
		if strings.TrimSpace(description) == "" {
			t.Errorf("%s description is empty", name)
		}
		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok || schema["type"] != "object" {
			t.Errorf("%s inputSchema = %#v, want object root", name, tool["inputSchema"])
			continue
		}
		if schema["additionalProperties"] != false {
			t.Errorf("%s additionalProperties = %#v, want false", name, schema["additionalProperties"])
		}
		if !reflect.DeepEqual(schema["required"], required) {
			t.Errorf("%s required = %#v, want %#v", name, schema["required"], required)
		}
		properties, _ := schema["properties"].(map[string]any)
		for property, definition := range properties {
			if definition.(map[string]any)["type"] == nil {
				t.Errorf("%s property %s has no type", name, property)
			}
		}
	}

	assertIntegerBounds(t, tools, "files.list", "limit", 1, 1000)
	assertIntegerBounds(t, tools, "files.search", "limit", 1, 200)
	assertIntegerBounds(t, tools, "files.read", "maxBytes", 1, 65536)
}

func TestListToolsAdvertisesStringConstraintsConsistentWithValidators(t *testing.T) {
	advertised := listTools()
	for _, toolName := range []string{"files.list", "files.search", "files.read", "files.create", "files.update"} {
		path := toolPropertySchema(t, advertised, toolName, "path")
		if path["minLength"] != 1 || path["pattern"] != `\S` {
			t.Errorf("%s path schema = %#v, want nonblank string constraints", toolName, path)
		}
		description, _ := path["description"].(string)
		for _, detail := range []string{"4096 bytes", "255 bytes", "64 components"} {
			if !strings.Contains(description, detail) {
				t.Errorf("%s path description %q does not document %s", toolName, description, detail)
			}
		}
		if _, present := path["maxLength"]; present {
			t.Errorf("%s path schema uses character maxLength for a byte limit: %#v", toolName, path)
		}
	}

	query := toolPropertySchema(t, advertised, "files.search", "query")
	if query["minLength"] != 1 || query["pattern"] != `\S` {
		t.Errorf("files.search query schema = %#v, want nonblank constraints", query)
	}

	for _, toolName := range []string{"files.create", "files.update"} {
		content := toolPropertySchema(t, advertised, toolName, "content")
		if _, present := content["maxLength"]; present {
			t.Errorf("%s content schema uses character maxLength for a byte limit: %#v", toolName, content)
		}
		if description, _ := content["description"].(string); !strings.Contains(description, "524288-byte") {
			t.Errorf("%s content description = %q, want byte limit", toolName, description)
		}
	}

	expectedHash := toolPropertySchema(t, advertised, "files.update", "expectedHash")
	if expectedHash["minLength"] != 71 ||
		expectedHash["maxLength"] != 71 ||
		expectedHash["pattern"] != `^sha256:[0-9a-f]{64}$` {
		t.Errorf("files.update expectedHash schema = %#v, want exact lowercase SHA-256 constraints", expectedHash)
	}
}

func assertIntegerBounds(t *testing.T, tools []map[string]any, toolName, property string, minimum, maximum int) {
	t.Helper()
	for _, tool := range tools {
		if tool["name"] != toolName {
			continue
		}
		schema := tool["inputSchema"].(map[string]any)
		definition := schema["properties"].(map[string]any)[property].(map[string]any)
		if definition["type"] != "integer" || definition["minimum"] != minimum || definition["maximum"] != maximum {
			t.Fatalf("%s %s schema = %#v, want integer %d..%d", toolName, property, definition, minimum, maximum)
		}
		return
	}
	t.Fatalf("tool %s was not advertised", toolName)
}

func toolPropertySchema(t *testing.T, advertised []map[string]any, toolName, property string) map[string]any {
	t.Helper()
	for _, tool := range advertised {
		if tool["name"] != toolName {
			continue
		}
		schema := tool["inputSchema"].(map[string]any)
		definition, ok := schema["properties"].(map[string]any)[property].(map[string]any)
		if !ok {
			t.Fatalf("%s property %s is not advertised", toolName, property)
		}
		return definition
	}
	t.Fatalf("tool %s was not advertised", toolName)
	return nil
}

func testFilesHandler(t *testing.T) http.Handler {
	t.Helper()
	return newHandler(serverConfig{
		filesToken:        "files-token",
		approvalJwtSecret: "jwt-secret",
		sandboxRoot:       t.TempDir(),
	})
}

func callFilesMCP(t *testing.T, handler http.Handler, body string) (int, []byte) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+"files-token")
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
