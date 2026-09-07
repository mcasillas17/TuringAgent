package runtime

import (
	"context"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

func TestAutomationGrantCannotUseAnotherServersAllowlist(t *testing.T) {
	h := newHarness(t)
	fire := h.fireAutomation(t, "preview binding", []repository.AutomationTool{{ServerName: "vendor", ToolName: "files.update"}})
	id := h.pendingApprovalFor(t, fire.RunID, fire.SessionID, fire.TraceID, "call_cross_server", "files.update")
	if err := h.approvals.GrantUnattendedApproval(context.Background(), id, "vendor", "files.update"); err == nil {
		t.Fatal("grant for a different recorded server approved file mutation")
	}
}
