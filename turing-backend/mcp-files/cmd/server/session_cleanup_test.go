package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cleanupHandler(t *testing.T, internalToken string) (http.Handler, string) {
	t.Helper()
	sandbox := t.TempDir()
	seedSessionArtifact(t, sandbox, "sessions/sess_1/runs/run_1/files/notes/todo.txt", "owned")
	seedSessionArtifact(t, sandbox, "sessions/sess_2/runs/run_9/files/private.txt", "another session")
	seedSessionArtifact(t, sandbox, "legacy.txt", "pre-existing")
	return newHandler(serverConfig{
		filesToken:        "files-token",
		approvalJwtSecret: "jwt-secret",
		internalToken:     internalToken,
		sandboxRoot:       sandbox,
	}), sandbox
}

func seedSessionArtifact(t *testing.T, sandbox string, relative string, content string) {
	t.Helper()
	full := filepath.Join(sandbox, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func cleanupBody(t *testing.T, meta map[string]any) string {
	t.Helper()
	params := map[string]any{
		"name":      "files.session_cleanup",
		"arguments": map[string]any{"sessionId": "sess_1", "lifecycleVersion": 1},
	}
	if meta != nil {
		params["_meta"] = meta
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": params})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func assertSessionOneIntact(t *testing.T, sandbox string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(sandbox, "sessions", "sess_1", "runs", "run_1", "files", "notes", "todo.txt")); err != nil {
		t.Fatalf("session storage was removed by a refused cleanup: %v", err)
	}
}

func TestSessionCleanupRunsWithTheConfiguredInternalToken(t *testing.T) {
	handler, sandbox := cleanupHandler(t, "internal-token")

	status, response := callFilesMCP(t, handler, cleanupBody(t, map[string]any{"internalCleanupToken": "internal-token"}))

	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, response)
	}
	var envelope struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error != nil {
		t.Fatalf("response error = %v", envelope.Error)
	}
	if removed, _ := envelope.Result["namespaceRemoved"].(bool); !removed {
		t.Fatalf("result = %+v, want the namespace removed", envelope.Result)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "sessions", "sess_1")); !os.IsNotExist(err) {
		t.Fatalf("session namespace survived cleanup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "sessions", "sess_2", "runs", "run_9", "files", "private.txt")); err != nil {
		t.Fatalf("another session's artifact was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sandbox, "legacy.txt")); err != nil {
		t.Fatalf("a pre-existing root file was removed: %v", err)
	}
}

func TestSessionCleanupReportsOnlyCounts(t *testing.T) {
	handler, _ := cleanupHandler(t, "internal-token")

	_, response := callFilesMCP(t, handler, cleanupBody(t, map[string]any{"internalCleanupToken": "internal-token"}))

	for _, leak := range []string{"todo.txt", "notes", "run_1", "owned", "sessions/", "sandbox"} {
		if strings.Contains(string(response), leak) {
			t.Fatalf("cleanup response %s leaks %q", response, leak)
		}
	}
}

func TestSessionCleanupIsIdempotentOverTheEndpoint(t *testing.T) {
	handler, _ := cleanupHandler(t, "internal-token")
	meta := map[string]any{"internalCleanupToken": "internal-token"}
	if _, response := callFilesMCP(t, handler, cleanupBody(t, meta)); strings.Contains(string(response), "error") {
		t.Fatalf("first cleanup = %s", response)
	}

	status, response := callFilesMCP(t, handler, cleanupBody(t, meta))

	if status != http.StatusOK {
		t.Fatalf("status = %d: %s", status, response)
	}
	var envelope struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Error != nil {
		t.Fatalf("repeated cleanup error = %v", envelope.Error)
	}
	if removed, _ := envelope.Result["namespaceRemoved"].(bool); removed {
		t.Fatalf("result = %+v, want namespaceRemoved false the second time", envelope.Result)
	}
}

func TestSessionCleanupRejectsWrongOrMissingInternalToken(t *testing.T) {
	cases := map[string]map[string]any{
		"missing metadata":    nil,
		"empty metadata":      {},
		"wrong token":         {"internalCleanupToken": "not-the-internal-token"},
		"empty token":         {"internalCleanupToken": ""},
		"token prefix":        {"internalCleanupToken": "internal-toke"},
		"token with extra":    {"internalCleanupToken": "internal-token "},
		"approval token only": {"approvalToken": "internal-token"},
		"provenance sneak-in": {"provenanceToken": "internal-token"},
	}
	for name, meta := range cases {
		t.Run(name, func(t *testing.T) {
			handler, sandbox := cleanupHandler(t, "internal-token")

			status, response := callFilesMCP(t, handler, cleanupBody(t, meta))

			if status != http.StatusOK {
				t.Fatalf("status = %d: %s", status, response)
			}
			var envelope struct {
				Error map[string]any `json:"error"`
			}
			if err := json.Unmarshal(response, &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope.Error == nil {
				t.Fatalf("response = %s, want a refusal", response)
			}
			assertSessionOneIntact(t, sandbox)
		})
	}
}

func TestSessionCleanupIsRefusedWhenNoInternalTokenIsConfigured(t *testing.T) {
	handler, sandbox := cleanupHandler(t, "")

	for _, meta := range []map[string]any{nil, {"internalCleanupToken": ""}, {"internalCleanupToken": "anything"}} {
		_, response := callFilesMCP(t, handler, cleanupBody(t, meta))

		var envelope struct {
			Error map[string]any `json:"error"`
		}
		if err := json.Unmarshal(response, &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Error == nil {
			t.Fatalf("response = %s, want a refusal from a server with no internal token", response)
		}
	}
	assertSessionOneIntact(t, sandbox)
}

func TestSessionCleanupIsNotReachableWithARuntimeCapability(t *testing.T) {
	handler, sandbox := cleanupHandler(t, "internal-token")
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name":      "files.session_cleanup",
			"arguments": map[string]any{"sessionId": "sess_1", "lifecycleVersion": 1},
			"_meta": map[string]any{
				"provenanceToken": testProvenanceToken(t, "files.session_cleanup", map[string]any{"sessionId": "sess_1", "lifecycleVersion": 1}, "sess_1"),
				"approvalToken":   "approval",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, response := callFilesMCP(t, handler, string(body))

	assertRPCErrorCode(t, response, -32602)
	assertSessionOneIntact(t, sandbox)
}

func TestSessionCleanupRequiresTheMCPBearerToken(t *testing.T) {
	handler, sandbox := cleanupHandler(t, "internal-token")
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(cleanupBody(t, map[string]any{"internalCleanupToken": "internal-token"})))
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want unauthorized", response.Code)
	}
	assertSessionOneIntact(t, sandbox)
}

func TestSessionCleanupToolIsNotAdvertised(t *testing.T) {
	for _, tool := range listTools() {
		if tool["name"] == "files.session_cleanup" {
			t.Fatal("files.session_cleanup is advertised through tools/list")
		}
	}
	handler, _ := cleanupHandler(t, "internal-token")

	_, response := callFilesMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)

	if strings.Contains(string(response), "session_cleanup") {
		t.Fatalf("tools/list response advertises session cleanup: %s", response)
	}
}

func TestSessionCleanupRejectsMalformedInternalMetadata(t *testing.T) {
	handler, sandbox := cleanupHandler(t, "internal-token")
	for name, body := range map[string]string{
		"non-string token": `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.session_cleanup","arguments":{"sessionId":"sess_1","lifecycleVersion":1},"_meta":{"internalCleanupToken":7}}}`,
		"unknown meta key": `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.session_cleanup","arguments":{"sessionId":"sess_1","lifecycleVersion":1},"_meta":{"internalCleanupToken":"internal-token","extra":"x"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, response := callFilesMCP(t, handler, body)

			assertRPCErrorCode(t, response, -32602)
			assertSessionOneIntact(t, sandbox)
		})
	}
}

func TestSessionCleanupRejectsUnsafeSessionIdentifiersOverTheEndpoint(t *testing.T) {
	handler, sandbox := cleanupHandler(t, "internal-token")
	for name, sessionID := range map[string]string{
		"parent traversal": "..",
		"nested traversal": "../sess_2",
		"path separator":   "sess_1/runs",
		"absolute path":    "/etc",
	} {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": 1, "method": "tools/call",
				"params": map[string]any{
					"name":      "files.session_cleanup",
					"arguments": map[string]any{"sessionId": sessionID, "lifecycleVersion": 1},
					"_meta":     map[string]any{"internalCleanupToken": "internal-token"},
				},
			})
			if err != nil {
				t.Fatal(err)
			}

			_, response := callFilesMCP(t, handler, string(body))

			assertRPCErrorCode(t, response, -32602)
			assertSessionOneIntact(t, sandbox)
			if _, statErr := os.Stat(filepath.Join(sandbox, "sessions", "sess_2", "runs", "run_9", "files", "private.txt")); statErr != nil {
				t.Fatalf("a traversal attempt removed another session: %v", statErr)
			}
		})
	}
}

func TestOrdinaryToolCallsCannotCarryTheInternalCleanupToken(t *testing.T) {
	handler, _ := cleanupHandler(t, "internal-token")
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{
			"name":      "files.read",
			"arguments": map[string]any{"path": "legacy.txt"},
			"_meta": map[string]any{
				"provenanceToken":      testProvenanceToken(t, "files.read", map[string]any{"path": "legacy.txt"}, "legacy.txt"),
				"internalCleanupToken": "internal-token",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, response := callFilesMCP(t, handler, string(body))

	assertRPCErrorCode(t, response, -32602)
}
