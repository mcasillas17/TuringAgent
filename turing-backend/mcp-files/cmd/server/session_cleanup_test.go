package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInternalSessionCleanupRemovesOnlyOwnedNamespace(t *testing.T) {
	sandbox := t.TempDir()
	owned := filepath.Join(sandbox, "sessions", "sess_cleanup", "runs", "run_1", "files", "note.txt")
	if err := os.MkdirAll(filepath.Dir(owned), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(owned, []byte("owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(sandbox, "legacy.txt")
	if err := os.WriteFile(outside, []byte("retain"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler := newHandler(serverConfig{
		filesToken:            "files-token",
		approvalJwtSecret:     "jwt-secret",
		approvalConsumerToken: "approval-consumer-token",
		cleanupToken:          "cleanup-token",
		sandboxRoot:           sandbox,
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/session-cleanup",
		bytes.NewBufferString(`{"sessionId":"sess_cleanup","lifecycleVersion":1}`),
	)
	request.Header.Set("authorization", "Bearer cleanup-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("cleanup status = %d: %s", response.Code, response.Body.String())
	}
	if _, err := os.Stat(filepath.Join(sandbox, "sessions", "sess_cleanup")); !os.IsNotExist(err) {
		t.Fatalf("owned namespace remains: %v", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "retain" {
		t.Fatalf("outside file changed: data=%q err=%v", data, err)
	}
}

func TestInternalSessionCleanupRejectsWrongCredential(t *testing.T) {
	handler := newHandler(serverConfig{
		filesToken:            "files-token",
		approvalJwtSecret:     "jwt-secret",
		approvalConsumerToken: "approval-consumer-token",
		cleanupToken:          "cleanup-token",
		sandboxRoot:           t.TempDir(),
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/internal/session-cleanup",
		bytes.NewBufferString(`{"sessionId":"sess_cleanup","lifecycleVersion":1}`),
	)
	request.Header.Set("authorization", "Bearer approval-consumer-token")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("cleanup status = %d, want unauthorized", response.Code)
	}
}

func TestAgentMCPRouteDoesNotExposeSessionCleanup(t *testing.T) {
	handler := newHandler(serverConfig{
		filesToken:            "files-token",
		approvalJwtSecret:     "jwt-secret",
		approvalConsumerToken: "approval-consumer-token",
		cleanupToken:          "cleanup-token",
		sandboxRoot:           t.TempDir(),
	})
	status, body := callFilesMCP(t, handler, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"files.session_cleanup","arguments":{"sessionId":"sess_cleanup","lifecycleVersion":1}}}`)
	if status != http.StatusOK || !strings.Contains(string(body), "unknown tool") {
		t.Fatalf("agent cleanup response = status=%d body=%s", status, body)
	}
}
