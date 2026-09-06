package approvals

import (
	"context"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestRejectedFileArgumentsDoNotRetainContent(t *testing.T) {
	argsJSON, argsHash, err := canonicalArgs(map[string]any{
		"path": "note.txt", "content": "visible fixture body", "token": "1",
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	for _, server := range []string{"files", "vendor"} {
		t.Run(server, func(t *testing.T) {
			p, err := s.preparePreview(context.Background(), repository.ApprovalRecord{
				ServerName: server, ToolName: "files.update", ArgsJSON: argsJSON, ArgsHash: argsHash,
			}, repository.ApprovalPreviewContext{})
			if err != nil || p.State != turingv1.ApprovalPreviewState_APPROVAL_PREVIEW_STATE_REDACTED {
				t.Fatalf("prepare rejected arguments: state=%s error=%v", p.State, err)
			}
			if server == "files" && p.ArgumentsJSON != "{}" {
				t.Fatal("rejected file arguments retained content")
			}
			if server == "vendor" && !strings.Contains(p.ArgumentsJSON, "visible fixture body") {
				t.Fatal("non-file structured display was removed")
			}
		})
	}
}
