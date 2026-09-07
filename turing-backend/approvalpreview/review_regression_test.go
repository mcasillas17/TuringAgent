package approvalpreview

import (
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestDockerAndCookieCredentialsAreRedacted(t *testing.T) {
	const credential = "fixture-private-value"
	for name, args := range map[string]map[string]any{
		"docker path":            {"path": ".docker/config.json", "content": credential},
		"normalized docker path": {"path": "work/.docker/tmp/../config.json", "content": credential},
		"auth field":             {"auth": credential},
		"auths object":           {"auths": map[string]any{"registry.example": map[string]any{"auth": credential}}},
		"auth JSON text":         {"content": `{"auths":{"registry.example":{"auth":"` + credential + `"}}}`},
		"cookie header":          {"content": "Cookie: sid=" + credential},
		"set-cookie header":      {"content": "Set-Cookie: sid=" + credential + "; Secure"},
	} {
		t.Run(name, func(t *testing.T) {
			safe, state := SanitizeArguments(args)
			if state != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED || strings.Contains(safe, credential) {
				t.Fatalf("credential not removed; state=%s", state)
			}
		})
	}
}

func TestPathDetectionDoesNotTreatProseAsAFilename(t *testing.T) {
	prose := "This explains secret and token handling and authorization without assigning credentials."
	safe, state := SanitizeArguments(map[string]any{"content": prose, "description": "A secret is mentioned here."})
	if state != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY || !strings.Contains(safe, prose) {
		t.Fatalf("benign prose was hidden: %s", state)
	}
	for _, value := range []string{"secret=x", "token=1", "authorization: x", "password=a"} {
		_, state := SanitizeArguments(map[string]any{"content": value})
		if state != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED {
			t.Fatalf("low-entropy credential assignment was allowed: %s", state)
		}
	}
	safe, state = SanitizeArguments(map[string]any{"nested": map[string]any{"path": ".env", "content": "private fixture"}})
	if state != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED || strings.Contains(safe, "private fixture") {
		t.Fatal("nested credential-file content was not removed")
	}
}

func TestDiffExactFormatting(t *testing.T) {
	for _, tt := range []struct{ name, before, after, want string }{
		{"empty both", "", "", "--- before\n+++ after\n@@ -0,0 +0,0 @@\n"},
		{"create", "", "new\n", "--- before\n+++ after\n@@ -0,0 +1,1 @@\n+new\n"},
		{"empty after", "old\n", "", "--- before\n+++ after\n@@ -1,1 +0,0 @@\n-old\n"},
		{"normal newline", "old\n", "new\n", "--- before\n+++ after\n@@ -1,1 +1,1 @@\n-old\n+new\n"},
		{"no trailing newline", "old", "new", "--- before\n+++ after\n@@ -1,1 +1,1 @@\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file\n"},
		{"add final newline", "old", "new\n", "--- before\n+++ after\n@@ -1,1 +1,1 @@\n-old\n\\ No newline at end of file\n+new\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := Diff(tt.before, tt.after); got != tt.want {
				t.Fatalf("Diff = %q, want %q", got, tt.want)
			}
		})
	}
}
