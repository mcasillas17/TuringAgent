package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
	"google.golang.org/protobuf/proto"
)

func TestNonReadyFilePreviewDoesNotRetainArgumentContent(t *testing.T) {
	for _, tt := range []struct {
		name, before, after string
		exists              bool
		state               turingv1.ApprovalPreviewState
	}{
		{"oversized before", strings.Repeat("a", approvalpreview.MaxTextBytes+1), "reviewed-after", true, turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_OVERSIZED},
		{"oversized diff", strings.Repeat("\n", approvalpreview.MaxTextBytes), strings.Repeat("\n", approvalpreview.MaxTextBytes-16), true, turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_OVERSIZED},
		{"redacted arguments", "before", "token=1", true, turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED},
		{"unavailable target", "", "reviewed-after", false, turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if tt.exists {
				if err := os.WriteFile(filepath.Join(root, "f"), []byte(tt.before), 0600); err != nil {
					t.Fatal(err)
				}
			}
			args := map[string]any{"path": "f", "content": tt.after}
			if tt.name == "oversized diff" {
				if _, state := approvalpreview.SanitizeArguments(args); state != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY {
					t.Fatal("fixture must reach diff generation, not the argument limit")
				}
				if len(approvalpreview.Diff(tt.before, tt.after)) <= approvalpreview.MaxDiffBytes {
					t.Fatal("fixture must exceed the diff bound")
				}
			}
			p, err := NewFilesTools(root).PreparePreview(context.Background(), CallRequest{Name: "files.update", Args: args})
			if err != nil {
				t.Fatal(err)
			}
			if p.State != tt.state || p.File != nil || p.ArgumentsJSON != "{}" {
				t.Fatalf("non-ready preview retained display content: state=%s, hasFile=%t, argumentBytes=%d", p.State, p.File != nil, len(p.ArgumentsJSON))
			}
			if args["content"] != tt.after {
				t.Fatal("preview modified execution arguments")
			}
		})
	}
}

func TestReadyFilePreviewOmitsDuplicateArgumentsButKeepsExactMutation(t *testing.T) {
	content := strings.Repeat("a", approvalpreview.MaxTextBytes)
	args := map[string]any{"path": "note.txt", "content": content}
	hash, err := CanonicalArgsHash(args)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewFilesTools(t.TempDir()).PreparePreview(context.Background(), CallRequest{Name: "files.create", Args: args})
	if err != nil {
		t.Fatal(err)
	}
	if p.State != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY || p.ArgumentsJSON != "{}" {
		t.Fatal("READY file preview retained a redundant argument body")
	}
	if p.File == nil || p.File.AfterText != content || p.File.AfterHash != contentHash(content) || !strings.Contains(p.File.UnifiedDiff, content) {
		t.Fatal("compaction removed exact reviewed content or preconditions")
	}
	afterHash, err := CanonicalArgsHash(args)
	if err != nil {
		t.Fatal(err)
	}
	if afterHash != hash {
		t.Fatal("preview compaction mutated execution arguments")
	}
	fullArgs, state := approvalpreview.SanitizeArguments(args)
	if state != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY {
		t.Fatal(state)
	}
	old := p
	old.ArgumentsJSON = fullArgs
	oldJSON, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	newJSON, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	oldWire := proto.Size(&turingv1.ApprovalDetails{ArgumentsJson: fullArgs, FilePreview: p.File})
	newWire := proto.Size(&turingv1.ApprovalDetails{ArgumentsJson: p.ArgumentsJSON, FilePreview: p.File})
	if len(oldJSON)-len(newJSON) < approvalpreview.MaxTextBytes || oldWire-newWire < approvalpreview.MaxTextBytes {
		t.Fatal("full duplicate content was not removed")
	}
	t.Logf("64 KiB content: snapshot JSON %d -> %d bytes (saved %d); protobuf details %d -> %d bytes (saved %d)",
		len(oldJSON), len(newJSON), len(oldJSON)-len(newJSON), oldWire, newWire, oldWire-newWire)
}
