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
		if got := r.Header.Get("authorization"); got != "Bearer files-token" {
			t.Fatalf("authorization = %q", got)
		}
		var request struct {
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
				Meta      map[string]any `json:"_meta"`
			} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Method != "tools/call" || request.Params.Name != "files.session_cleanup" {
			t.Fatalf("request = %+v", request)
		}
		if request.Params.Arguments["sessionId"] != "sess_cleanup" ||
			request.Params.Arguments["lifecycleVersion"] != float64(7) {
			t.Fatalf("arguments = %+v", request.Params.Arguments)
		}
		if request.Params.Meta["internalCleanupToken"] != "internal-token" {
			t.Fatalf("meta = %+v", request.Params.Meta)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"namespaceRemoved":true,"removedFiles":1,"removedDirectories":5,"lifecycleVersion":7}}`))
	}))
	defer server.Close()

	cleaner := NewMCPArtifactCleaner(server.URL, "files-token", "internal-token", server.Client())
	if err := cleaner.CleanupSessionArtifacts(context.Background(), "sess_cleanup", 7); err != nil {
		t.Fatalf("CleanupSessionArtifacts: %v", err)
	}
}

func TestMCPArtifactCleanerTreatsMissingNamespaceAsSuccessfulCleanup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"namespaceRemoved":false,"removedFiles":0,"removedDirectories":0,"lifecycleVersion":3}}`))
	}))
	defer server.Close()

	cleaner := NewMCPArtifactCleaner(server.URL, "files-token", "internal-token", server.Client())
	if err := cleaner.CleanupSessionArtifacts(context.Background(), "sess_missing", 3); err != nil {
		t.Fatalf("CleanupSessionArtifacts(missing namespace): %v", err)
	}
}
