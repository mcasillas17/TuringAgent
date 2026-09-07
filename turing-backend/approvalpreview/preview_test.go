package approvalpreview

import (
	"encoding/json"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestCredentialsNeverAppearInRenderedArguments(t *testing.T) {
	for _, args := range []map[string]any{
		{"token": "test-fixture-private-value"},
		{"nested": map[string]any{"authorization": "test-fixture-private-value"}},
		{"content": "TOKEN=test-fixture-private-value"},
		{"content": "client_secret=test-fixture-private-value"},
		{"path": ".aws/config", "content": "test-fixture-private-value"},
	} {
		safe, state := SanitizeArguments(args)
		if state != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED || strings.Contains(safe, "test-fixture-private-value") {
			t.Fatalf("credential not redacted: state=%v safe=%q", state, safe)
		}
		if !json.Valid([]byte(safe)) {
			t.Fatal("invalid structured JSON")
		}
	}
}

func TestBoundedDiffAndBinaryArguments(t *testing.T) {
	before, after := strings.Repeat("a\n", MaxTextBytes/2), strings.Repeat("b\n", MaxTextBytes/2)
	if len(Diff(before, after)) > MaxDiffBytes {
		t.Fatal("bounded inputs generated oversized diff")
	}
	_, state := SanitizeArguments(map[string]any{"content": "binary\x00"})
	if state != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_BINARY {
		t.Fatalf("binary state=%v", state)
	}
	_, state = SanitizeArguments(map[string]any{"content": strings.Repeat("x", MaxArgsBytes+1)})
	if state != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_OVERSIZED {
		t.Fatalf("oversized state=%v", state)
	}
}
