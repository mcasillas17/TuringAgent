package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestPreviewDistinguishesUnreadableTargetsFromCreateCollisions(t *testing.T) {
	for _, scenario := range []string{"unreadable", "symlink", "create collision"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "note.txt")
			tool := "files.update"
			switch scenario {
			case "unreadable":
				if err := os.WriteFile(target, []byte("must not be an empty before"), 0200); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				outside := filepath.Join(t.TempDir(), "outside.txt")
				if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			case "create collision":
				tool = "files.create"
				if err := os.WriteFile(target, []byte("collision"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			p, err := NewFilesTools(root).PreparePreview(context.Background(), CallRequest{Name: tool, Args: map[string]any{"path": "note.txt", "content": "after"}})
			want := turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE
			if scenario == "create collision" {
				want = turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_STALE
			}
			if err != nil || p.State != want || p.File != nil {
				t.Fatalf("preview=%+v %v", p, err)
			}
		})
	}
}

func TestPreviewCannotReadAnotherSessionsArtifactOrUnscopedPath(t *testing.T) {
	guard := &fakeGuard{provenance: Provenance{SessionID: "own", RunID: "ownrun", LogicalPath: "note.txt"}}
	f, root := guardedTools(t, guard)
	other := filepath.Join(root, "sessions/other/runs/otherrun/files/note.txt")
	if err := os.MkdirAll(filepath.Dir(other), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("other session private content"), 0600); err != nil {
		t.Fatal(err)
	}
	req := CallRequest{Name: "files.update", Args: map[string]any{"path": "note.txt", "content": "after"}, AgentID: "agent", ProvenanceToken: "prov"}
	p, err := f.PreparePreview(context.Background(), req)
	if err != nil || p.File != nil || p.State != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_UNAVAILABLE {
		t.Fatalf("cross-session preview=%+v %v", p, err)
	}
	for _, logical := range []string{"../escape", "different.txt", "sessions/other/runs/otherrun/files/note.txt"} {
		req.Args["path"] = logical
		if _, err := f.PreparePreview(context.Background(), req); err == nil {
			t.Fatalf("unscoped preview accepted %q", logical)
		}
	}
}
