package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

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

func TestListToolsAdvertisesOnlyCallableToolsWithAccurateSchemas(t *testing.T) {
	tools := listTools()
	wantRequired := map[string][]any{
		"files.list":   {},
		"files.search": {"query"},
		"files.read":   {},
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

	assertIntegerBounds(t, tools, "files.search", "limit", 1, 200)
	assertIntegerBounds(t, tools, "files.read", "maxBytes", 1, 524288)
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
