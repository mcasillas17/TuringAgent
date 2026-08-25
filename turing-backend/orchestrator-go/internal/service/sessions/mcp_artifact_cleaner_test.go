package sessions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMCPArtifactCleanerUsesAuthenticatedInternalCleanupCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/session-cleanup" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("authorization"); got != "Bearer approval-consumer-token" {
			t.Fatalf("authorization = %q", got)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["sessionId"] != "sess_cleanup" || request["lifecycleVersion"] != float64(7) {
			t.Fatalf("request = %+v", request)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"namespaceRemoved":true,"removedFiles":1,"removedDirectories":5,"lifecycleVersion":7}`))
	}))
	defer server.Close()

	cleaner := NewMCPArtifactCleaner(nil, server.URL, "approval-consumer-token", server.Client())
	if err := cleaner.CleanupSessionArtifacts(context.Background(), "sess_cleanup", 7); err != nil {
		t.Fatalf("CleanupSessionArtifacts: %v", err)
	}
}

func TestMCPArtifactCleanerTreatsMissingNamespaceAsSuccessfulCleanup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"namespaceRemoved":false,"removedFiles":0,"removedDirectories":0,"lifecycleVersion":3}`))
	}))
	defer server.Close()

	cleaner := NewMCPArtifactCleaner(nil, server.URL, "approval-consumer-token", server.Client())
	if err := cleaner.CleanupSessionArtifacts(context.Background(), "sess_missing", 3); err != nil {
		t.Fatalf("CleanupSessionArtifacts(missing namespace): %v", err)
	}
}
