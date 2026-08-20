package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func capturedToolCallParams(t *testing.T, call func(client *Client) error) map[string]any {
	t.Helper()
	var params map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var request struct {
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
			return
		}
		params = request.Params
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true}}`))
	}))
	defer server.Close()
	if err := call(NewClient(server.URL, "", server.Client())); err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	return params
}

func TestCallToolSendsProvenanceTokenInMeta(t *testing.T) {
	params := capturedToolCallParams(t, func(client *Client) error {
		_, err := client.CallTool(context.Background(), "files.read", map[string]any{"path": "note.txt"}, "", "provenance-capability")
		return err
	})

	meta, ok := params["_meta"].(map[string]any)
	if !ok {
		t.Fatalf("params = %+v, want a _meta object carrying the capability", params)
	}
	if meta["provenanceToken"] != "provenance-capability" {
		t.Fatalf("_meta = %+v, want the provenance capability", meta)
	}
	if _, present := meta["approvalToken"]; present {
		t.Fatalf("_meta = %+v, want no approval token for a safe call", meta)
	}
}

func TestCallToolSendsApprovalAndProvenanceTokensTogether(t *testing.T) {
	params := capturedToolCallParams(t, func(client *Client) error {
		_, err := client.CallTool(context.Background(), "files.update", map[string]any{"path": "note.txt"}, "approval-token", "provenance-capability")
		return err
	})

	meta, ok := params["_meta"].(map[string]any)
	if !ok {
		t.Fatalf("params = %+v, want a _meta object", params)
	}
	if meta["approvalToken"] != "approval-token" || meta["provenanceToken"] != "provenance-capability" {
		t.Fatalf("_meta = %+v, want both tokens", meta)
	}
}

func TestCallToolOmitsMetaWhenNoTokensAreIssued(t *testing.T) {
	params := capturedToolCallParams(t, func(client *Client) error {
		_, err := client.CallTool(context.Background(), "system.time", nil)
		return err
	})

	if _, present := params["_meta"]; present {
		t.Fatalf("params = %+v, want no _meta for a server that was issued no tokens", params)
	}
}
