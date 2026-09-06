package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
)

func TestPreparePreviewUsesReplacementAndNeverCreatesDirectories(t *testing.T) {
	root := t.TempDir()
	f := NewFilesTools(root)
	args := map[string]any{"path": "nested/new.txt", "content": "new\n"}
	p, err := f.PreparePreview(context.Background(), CallRequest{Name: "files.create", Args: args})
	if err != nil || p.State != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_READY || p.File.BeforeExists || p.File.AfterText != "new\n" {
		t.Fatalf("create preview = %+v, %v", p, err)
	}
	if _, err := os.Stat(filepath.Join(root, "nested")); !os.IsNotExist(err) {
		t.Fatal("preview created directories")
	}
	if err := os.WriteFile(filepath.Join(root, "old.txt"), []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err = f.PreparePreview(context.Background(), CallRequest{Name: "files.update", Args: map[string]any{"path": "old.txt", "content": "replacement\n"}})
	if err != nil || p.File.BeforeText != "original\n" || p.File.AfterText != "replacement\n" || !strings.Contains(p.File.UnifiedDiff, "-original") {
		t.Fatalf("replacement preview = %+v, %v", p, err)
	}
}

func TestPreparePreviewRefusesUnsafeContent(t *testing.T) {
	for _, tt := range []struct {
		name, path, before, after string
		state                     turingv1.ApprovalPreviewState
	}{
		{"binary", "a.txt", "\x00binary", "after", turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_BINARY},
		{"oversized", "a.txt", strings.Repeat("a", 65537), "after", turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_OVERSIZED},
		{"secret path", ".env", "PASSWORD=secret", "after", turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED},
		{"secret value", "a.txt", "hello", "Bearer private-credential", turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, tt.path), []byte(tt.before), 0600); err != nil {
				t.Fatal(err)
			}
			p, err := NewFilesTools(root).PreparePreview(context.Background(), CallRequest{Name: "files.update", Args: map[string]any{"path": tt.path, "content": tt.after}})
			if err != nil || p.State != tt.state || p.File != nil {
				t.Fatalf("preview=%+v err=%v", p, err)
			}
		})
	}

}

func TestReviewedUpdateRevalidatesBeforeConsumeAndAfterStaging(t *testing.T) {
	for _, phase := range []string{"unchanged", "before consume", "after consume", "shadow after consume", "withdraw after consume"} {
		t.Run(phase, func(t *testing.T) {
			guard := &fakeGuard{provenance: Provenance{SessionID: "sess_1", RunID: "run_2", LogicalPath: "note.txt"}}
			f, root := guardedTools(t, guard)
			physical := "sessions/sess_1/runs/run_1/files/note.txt"
			target := filepath.Join(root, physical)
			if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte("before"), 0600); err != nil {
				t.Fatal(err)
			}
			req := CallRequest{Name: "files.update", Args: map[string]any{"path": "note.txt", "content": "reviewed <&>\n"}, AgentID: "agent", ProvenanceToken: "provenance", ApprovalToken: "approval"}
			p, err := f.PreparePreview(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			guard.binding = &approvalpreview.Binding{PreviewHash: "immutable-review", SessionID: "sess_1", RunID: "run_2", PhysicalPath: p.File.PhysicalPath,
				BeforeExists: p.File.BeforeExists, BeforeHash: p.File.BeforeHash, AfterHash: p.File.AfterHash}
			if phase == "before consume" {
				if err := os.WriteFile(target, []byte("changed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			f.syncFile = func(file *os.File) error {
				switch phase {
				case "after consume":
					if err := os.WriteFile(target, []byte("changed"), 0600); err != nil {
						return err
					}
				case "shadow after consume":
					shadow := filepath.Join(root, "sessions/sess_1/runs/run_2/files/note.txt")
					if err := os.MkdirAll(filepath.Dir(shadow), 0700); err != nil {
						return err
					}
					if err := os.WriteFile(shadow, []byte("shadow"), 0600); err != nil {
						return err
					}
				case "withdraw after consume":
					guard.sessionStateEnd = context.Canceled
				}
				return file.Sync()
			}
			_, err = f.CallRequestContext(context.Background(), req)
			actual, readErr := os.ReadFile(target)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if phase == "unchanged" {
				if err != nil || string(actual) != p.File.AfterText || contentHash(string(actual)) != p.File.AfterHash {
					t.Fatalf("write=%q err=%v", actual, err)
				}
			} else {
				if err == nil || string(actual) == p.File.AfterText {
					t.Fatalf("stale mutation landed: %q, %v", actual, err)
				}
				if phase == "before consume" && len(guard.stages()) != 0 {
					t.Fatal("stale precondition consumed approval")
				}
				if phase != "before consume" && guard.lastCall(t).committed {
					t.Fatal("stale staged write was committed")
				}
			}
		})
	}
}

func TestReviewedCreateRetainsAbsentPrecondition(t *testing.T) {
	guard := &fakeGuard{provenance: Provenance{SessionID: "sess_1", RunID: "run_1", LogicalPath: "note.txt"}}
	f, root := guardedTools(t, guard)
	req := CallRequest{Name: "files.create", Args: map[string]any{"path": "note.txt", "content": "reviewed"}, AgentID: "agent", ProvenanceToken: "provenance", ApprovalToken: "approval"}
	p, err := f.PreparePreview(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	guard.binding = &approvalpreview.Binding{PreviewHash: "immutable-review", SessionID: "sess_1", RunID: "run_1", PhysicalPath: p.File.PhysicalPath, AfterHash: p.File.AfterHash}
	target := filepath.Join(root, p.File.PhysicalPath)
	f.syncFile = func(file *os.File) error {
		if err := os.WriteFile(target, []byte("collision"), 0600); err != nil {
			return err
		}
		return file.Sync()
	}
	if _, err := f.CallRequestContext(context.Background(), req); err == nil {
		t.Fatal("create overwrote collision")
	}
	actual, err := os.ReadFile(target)
	if err != nil || string(actual) != "collision" {
		t.Fatalf("collision lost: %q %v", actual, err)
	}
}
