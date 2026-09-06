package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPreviewRejectsNormalCredentials(t *testing.T) {
	h := newHandler(serverConfig{sandboxRoot: t.TempDir(), filesToken: "worker", cleanupToken: "cleanup", approvalJwtSecret: "secret"})
	for _, token := range []string{"", "worker", "cleanup", "consumer"} {
		r := httptest.NewRequest(http.MethodPost, "/internal/approval-preview", strings.NewReader(`{}`))
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("credential %q: status %d, want 401", token, w.Code)
		}
	}
}
