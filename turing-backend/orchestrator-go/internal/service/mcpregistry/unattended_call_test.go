package mcpregistry

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/approvalpreview"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TUR-026d: CallTool checks an unattended run's grant twice, at entry and in
// the read transaction that holds the dispatch fence, so a runtime that skips
// the beacon reaches no server outside its automation's allowlist.

var vendorWrite = repository.AutomationTool{ServerName: "vendor", ToolName: "vendor.write"}

type unattendedRefusal struct {
	runID string
	block repository.UnattendedToolBlock
	// ctxErr is the hook's context's error once beforeRecord has run.
	ctxErr error
}

// recordingRefuser stands in for the runtime's hook, which owns failing the
// run and publishing it; these tests are about what CallTool does.
type recordingRefuser struct {
	mu      sync.Mutex
	refused []unattendedRefusal
	err     error
	// beforeRecord, when set, runs as the hook is called, before it reads its
	// own context.
	beforeRecord func()
}

func (r *recordingRefuser) RefuseUnattendedCall(ctx context.Context, runID string, block repository.UnattendedToolBlock) error {
	if r.beforeRecord != nil {
		r.beforeRecord()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refused = append(r.refused, unattendedRefusal{runID: runID, block: block, ctxErr: ctx.Err()})
	return r.err
}

func (r *recordingRefuser) calls() []unattendedRefusal {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]unattendedRefusal(nil), r.refused...)
}

// automationToolCall fires an automation with the given allowlist, delivers
// its run to a worker and records the tool call, the state a direct call
// arrives in.
func (h *registryCallHarness) automationToolCall(t *testing.T, toolCallID string, args map[string]any, allowed ...repository.AutomationTool) (repository.Automation, string) {
	t.Helper()
	ctx := context.Background()
	automation, err := h.repo.CreateAutomation(ctx, repository.AutomationInput{
		Name: "Nightly sync", Prompt: "sync",
		Schedule: repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute},
		Enabled:  true, AllowedTools: allowed,
	})
	if err != nil {
		t.Fatal(err)
	}
	due, err := time.Parse(time.RFC3339Nano, automation.NextDueAt)
	if err != nil {
		t.Fatal(err)
	}
	fire, found, err := h.repo.ClaimDueAutomation(ctx, due, repository.AutomationRunDefaults{
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "test-model",
	})
	if err != nil || !found {
		t.Fatalf("claim due automation = found %v, %v", found, err)
	}
	h.claimAndDeliverRun(t, fire.RunID)
	encoded, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.RecordToolCallBefore(ctx, repository.ToolCallRecord{ToolCallID: toolCallID, RunID: fire.RunID},
		"general_assistant", "vendor", "vendor.write", string(encoded), approvalpreview.Hash(string(encoded))); err != nil {
		t.Fatal(err)
	}
	return automation, fire.RunID
}

func (h *registryCallHarness) makeVendorWriteSafe(t *testing.T) {
	t.Helper()
	if err := h.repo.SetMCPToolPolicy(context.Background(), h.serverID, "vendor.write", "safe"); err != nil {
		t.Fatal(err)
	}
}

// grantedApproval creates the call's approval and grants it the way an
// automation's run is granted one, then resumes the run as the worker would.
func (h *registryCallHarness) grantedApproval(t *testing.T, runID string, toolCallID string, args map[string]any) string {
	t.Helper()
	ctx := context.Background()
	approvalID, err := h.approvals.CreateApprovalForTool(ctx, runID, toolCallID, "general_assistant", "vendor.write", args)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.approvals.GrantUnattendedApproval(ctx, approvalID, "vendor", "vendor.write"); err != nil {
		t.Fatalf("GrantUnattendedApproval: %v", err)
	}
	h.resumeApprovedRun(t, runID, approvalID)
	return approvalID
}

func (h *registryCallHarness) approvalStatus(t *testing.T, approvalID string) string {
	t.Helper()
	approval, err := h.repo.GetApproval(context.Background(), approvalID)
	if err != nil {
		t.Fatal(err)
	}
	return approval.Status
}

func assertRefusedWith(t *testing.T, err error, code string) {
	t.Helper()
	if status.Code(err) != codes.PermissionDenied || !strings.Contains(err.Error(), code) {
		t.Fatalf("CallTool err = %v, want PermissionDenied naming %s", err, code)
	}
}

func assertOneRefusal(t *testing.T, refuser *recordingRefuser, runID string, automation repository.Automation, code string) {
	t.Helper()
	calls := refuser.calls()
	want := repository.UnattendedToolBlock{
		AutomationID: automation.AutomationID, AutomationName: "Nightly sync",
		ServerName: "vendor", ToolName: "vendor.write", Code: code,
	}
	if len(calls) != 1 || calls[0].runID != runID || calls[0].block != want || calls[0].ctxErr != nil {
		t.Fatalf("refusals = %+v, want one for run %s: %+v", calls, runID, want)
	}
}

// An unallowlisted safe tool on a local_container server is refused at entry:
// PermissionDenied naming the code, the hook told once, and nothing sent.
func TestAnUnattendedCallToAnUnlistedSafeToolReachesNoServer(t *testing.T) {
	h := newRegistryCallHarness(t)
	refuser := &recordingRefuser{}
	h.registry.SetUnattendedRefuser(refuser)
	h.makeVendorWriteSafe(t)
	args := map[string]any{"path": "x"}
	automation, runID := h.automationToolCall(t, "call_unlisted", args,
		repository.AutomationTool{ServerName: "files", ToolName: "files.read"})

	_, err := h.registry.CallTool(context.Background(), CallInput{
		ServerID: h.serverID, RunID: runID, ToolName: "vendor.write", Args: args,
	})

	assertRefusedWith(t, err, repository.AutomationToolNotAllowlistedCode)
	if got := h.reached.Load(); got != 0 {
		t.Fatalf("vendor requests = %d, want none", got)
	}
	assertOneRefusal(t, refuser, runID, automation, repository.AutomationToolNotAllowlistedCode)
}

// A failing hook does not let the call through: the refusal still reaches no
// server and still names the code.
func TestARefusalHookThatFailsStillRefusesTheCall(t *testing.T) {
	h := newRegistryCallHarness(t)
	refuser := &recordingRefuser{err: errors.New("database is gone")}
	h.registry.SetUnattendedRefuser(refuser)
	h.makeVendorWriteSafe(t)
	args := map[string]any{"path": "x"}
	_, runID := h.automationToolCall(t, "call_hook_fails", args)

	_, err := h.registry.CallTool(context.Background(), CallInput{
		ServerID: h.serverID, RunID: runID, ToolName: "vendor.write", Args: args,
	})

	assertRefusedWith(t, err, repository.AutomationToolNotAllowlistedCode)
	if got := h.reached.Load(); got != 0 {
		t.Fatalf("vendor requests = %d, want none", got)
	}
}

// With no refusal hook wired, an unattended call is refused whatever its
// automation allows, and reaches no server, as an approval-required call is
// with no enforcer.
func TestAnUnattendedCallWithNoRefusalHookIsRefused(t *testing.T) {
	h := newRegistryCallHarness(t)
	h.makeVendorWriteSafe(t)
	args := map[string]any{"path": "x"}
	_, runID := h.automationToolCall(t, "call_no_hook", args, vendorWrite)

	_, err := h.registry.CallTool(context.Background(), CallInput{
		ServerID: h.serverID, RunID: runID, ToolName: "vendor.write", Args: args,
	})

	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("CallTool err = %v, want PermissionDenied", err)
	}
	if got := h.reached.Load(); got != 0 {
		t.Fatalf("vendor requests = %d, want none", got)
	}
}

// An unallowlisted tool on a remote_url server is refused at entry, before
// the run's egress decision is consulted.
func TestAnUnattendedCallToAnUnlistedRemoteToolReachesNoServer(t *testing.T) {
	h := newRegistryCallHarness(t)
	refuser := &recordingRefuser{}
	h.registry.SetUnattendedRefuser(refuser)
	h.makeVendorWriteSafe(t)
	if _, err := h.database.ExecContext(context.Background(), `UPDATE mcp_servers SET tier = 'remote_url' WHERE id = ?`, h.serverID); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"path": "x"}
	automation, runID := h.automationToolCall(t, "call_remote", args)

	_, err := h.registry.CallTool(context.Background(), CallInput{
		ServerID: h.serverID, RunID: runID, ToolName: "vendor.write", Args: args,
	})

	assertRefusedWith(t, err, repository.AutomationToolNotAllowlistedCode)
	if got := h.reached.Load(); got != 0 {
		t.Fatalf("vendor requests = %d, want none", got)
	}
	assertOneRefusal(t, refuser, runID, automation, repository.AutomationToolNotAllowlistedCode)
}

// Revoked before the call starts: an approval-required tool whose approval
// was granted, then narrowed away or disabled, is refused at entry, before the
// approval step, so its approval is never consumed.
func TestARevocationBeforeTheCallStartsLeavesItsApprovalUnconsumed(t *testing.T) {
	for _, revocation := range []struct {
		name   string
		revoke func(t *testing.T, h *registryCallHarness, automation repository.Automation)
	}{
		{name: "narrowed", revoke: func(t *testing.T, h *registryCallHarness, automation repository.Automation) {
			narrowVendorAutomation(t, h, automation)
		}},
		{name: "disabled", revoke: func(t *testing.T, h *registryCallHarness, automation repository.Automation) {
			if _, err := h.repo.SetAutomationEnabled(context.Background(), automation.AutomationID, false); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(revocation.name, func(t *testing.T) {
			h := newRegistryCallHarness(t)
			refuser := &recordingRefuser{}
			h.registry.SetUnattendedRefuser(refuser)
			args := map[string]any{"path": "x"}
			automation, runID := h.automationToolCall(t, "call_revoked_before", args, vendorWrite)
			approvalID := h.grantedApproval(t, runID, "call_revoked_before", args)
			revocation.revoke(t, h, automation)

			_, err := h.registry.CallTool(context.Background(), CallInput{
				ServerID: h.serverID, RunID: runID, ApprovalID: approvalID, ToolName: "vendor.write", Args: args,
			})

			assertRefusedWith(t, err, repository.AutomationGrantRevokedCode)
			if got := h.approvalStatus(t, approvalID); got != "approved" {
				t.Fatalf("approval = %s, want it never consumed", got)
			}
			if got := h.reached.Load(); got != 0 {
				t.Fatalf("vendor requests = %d, want none", got)
			}
			assertOneRefusal(t, refuser, runID, automation, repository.AutomationGrantRevokedCode)
		})
	}
}

// Revoked during the call: a revocation committed after the entry check
// passed, while the call's approval is being consumed, is refused at
// dispatch. The approval is spent and nothing is sent.
func TestARevocationDuringTheCallIsRefusedAtDispatchWithItsApprovalSpent(t *testing.T) {
	for _, revocation := range []struct {
		name   string
		revoke func(t *testing.T, h *registryCallHarness, automation repository.Automation)
	}{
		{name: "disabled", revoke: func(t *testing.T, h *registryCallHarness, automation repository.Automation) {
			if _, err := h.repo.SetAutomationEnabled(context.Background(), automation.AutomationID, false); err != nil {
				t.Error(err)
			}
		}},
		{name: "narrowed", revoke: func(t *testing.T, h *registryCallHarness, automation repository.Automation) {
			narrowVendorAutomation(t, h, automation)
		}},
	} {
		t.Run(revocation.name, func(t *testing.T) {
			h := newRegistryCallHarness(t)
			refuser := &recordingRefuser{}
			h.registry.SetUnattendedRefuser(refuser)
			args := map[string]any{"path": "x"}
			automation, runID := h.automationToolCall(t, "call_revoked_during", args, vendorWrite)
			approvalID := h.grantedApproval(t, runID, "call_revoked_during", args)
			barrier := &revokingEnforcer{inner: h.approvals, revoke: func() { revocation.revoke(t, h, automation) }}
			h.registry.SetApprovalEnforcer(barrier)

			_, err := h.registry.CallTool(context.Background(), CallInput{
				ServerID: h.serverID, RunID: runID, ApprovalID: approvalID, ToolName: "vendor.write", Args: args,
			})

			if !barrier.reached {
				t.Fatal("the entry check refused the call; the barrier never ran")
			}
			assertRefusedWith(t, err, repository.AutomationGrantRevokedCode)
			if got := h.approvalStatus(t, approvalID); got != "consumed" {
				t.Fatalf("approval = %s, want it spent", got)
			}
			if got := h.reached.Load(); got != 0 {
				t.Fatalf("vendor requests = %d, want none", got)
			}
			assertOneRefusal(t, refuser, runID, automation, repository.AutomationGrantRevokedCode)
		})
	}
}

// The same direct call from an attended run, and for an allowlisted tool on
// an enabled automation, still reaches the server, and the hook is never
// told anything.
func TestAllowedDirectCallsStillReachTheServer(t *testing.T) {
	t.Run("attended", func(t *testing.T) {
		h := newRegistryCallHarness(t)
		refuser := &recordingRefuser{}
		h.registry.SetUnattendedRefuser(refuser)
		h.makeVendorWriteSafe(t)
		runID := h.runningToolCall(t, "call_attended", map[string]any{"path": "x"})
		if _, err := h.registry.CallTool(context.Background(), CallInput{
			ServerID: h.serverID, RunID: runID, ToolName: "vendor.write", Args: map[string]any{"path": "x"},
		}); err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		if got := h.reached.Load(); got != 1 {
			t.Fatalf("vendor requests = %d, want 1", got)
		}
		if calls := refuser.calls(); len(calls) != 0 {
			t.Fatalf("refusals = %+v, want none", calls)
		}
	})
	t.Run("allowlisted safe tool", func(t *testing.T) {
		h := newRegistryCallHarness(t)
		refuser := &recordingRefuser{}
		h.registry.SetUnattendedRefuser(refuser)
		h.makeVendorWriteSafe(t)
		args := map[string]any{"path": "x"}
		_, runID := h.automationToolCall(t, "call_listed", args, vendorWrite)
		if _, err := h.registry.CallTool(context.Background(), CallInput{
			ServerID: h.serverID, RunID: runID, ToolName: "vendor.write", Args: args,
		}); err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		if got := h.reached.Load(); got != 1 {
			t.Fatalf("vendor requests = %d, want 1", got)
		}
		if calls := refuser.calls(); len(calls) != 0 {
			t.Fatalf("refusals = %+v, want none", calls)
		}
	})
	t.Run("allowlisted approval-required tool", func(t *testing.T) {
		h := newRegistryCallHarness(t)
		refuser := &recordingRefuser{}
		h.registry.SetUnattendedRefuser(refuser)
		args := map[string]any{"path": "x"}
		_, runID := h.automationToolCall(t, "call_listed_approval", args, vendorWrite)
		approvalID := h.grantedApproval(t, runID, "call_listed_approval", args)
		if _, err := h.registry.CallTool(context.Background(), CallInput{
			ServerID: h.serverID, RunID: runID, ApprovalID: approvalID, ToolName: "vendor.write", Args: args,
		}); err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		if got := h.reached.Load(); got != 1 {
			t.Fatalf("vendor requests = %d, want 1", got)
		}
		if got := h.approvalStatus(t, approvalID); got != "consumed" {
			t.Fatalf("approval = %s, want consumed by the call", got)
		}
	})
}

func narrowVendorAutomation(t *testing.T, h *registryCallHarness, automation repository.Automation) {
	t.Helper()
	if _, err := h.repo.UpdateAutomation(context.Background(), automation.AutomationID, repository.AutomationInput{
		Name: automation.Name, Prompt: automation.Prompt, Schedule: automation.Schedule,
		AllowedTools: []repository.AutomationTool{{ServerName: "files", ToolName: "files.read"}},
	}); err != nil {
		t.Error(err)
	}
}

// revokingEnforcer is the barrier between the entry check and the approval's
// consumption: it revokes, then consumes through the real enforcer.
type revokingEnforcer struct {
	inner   ApprovalEnforcer
	revoke  func()
	reached bool
}

func (e *revokingEnforcer) ConsumeApprovalForThirdParty(ctx context.Context, approvalID string, runID string, serverName string, serverID string, toolName string, args map[string]any) error {
	e.reached = true
	e.revoke()
	return e.inner.ConsumeApprovalForThirdParty(ctx, approvalID, runID, serverName, serverID, toolName, args)
}

// The hook fails the run on a context of its own: a runtime that gives up on
// the call, or lets its deadline run out, cannot take the failure and its
// audit row down with it.
func TestTheRefusalHookOutlivesTheCallersContext(t *testing.T) {
	h := newRegistryCallHarness(t)
	h.makeVendorWriteSafe(t)
	args := map[string]any{"path": "x"}
	_, runID := h.automationToolCall(t, "call_caller_gone", args)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	refuser := &recordingRefuser{beforeRecord: cancel}
	h.registry.SetUnattendedRefuser(refuser)

	_, err := h.registry.CallTool(ctx, CallInput{
		ServerID: h.serverID, RunID: runID, ToolName: "vendor.write", Args: args,
	})

	assertRefusedWith(t, err, repository.AutomationToolNotAllowlistedCode)
	if calls := refuser.calls(); len(calls) != 1 || calls[0].ctxErr != nil {
		t.Fatalf("refusals = %+v, want one whose context outlived the caller's", calls)
	}
}

// The entry check runs for every tier and before the server's own refusals:
// an unattended call to a bundled server, or to a disabled one, is refused
// with its code, and the hook fails the run, rather than getting the plain
// error a cooperating runtime would see.
func TestTheEntryCheckRunsBeforeTheServersOwnRefusals(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(t *testing.T, h *registryCallHarness)
	}{
		{name: "bundled", setup: func(t *testing.T, h *registryCallHarness) {
			if _, err := h.database.ExecContext(context.Background(), `UPDATE mcp_servers SET tier = 'bundled' WHERE id = ?`, h.serverID); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "disabled", setup: func(t *testing.T, h *registryCallHarness) {
			if err := h.repo.SetMCPServerEnabled(context.Background(), h.serverID, false); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newRegistryCallHarness(t)
			refuser := &recordingRefuser{}
			h.registry.SetUnattendedRefuser(refuser)
			h.makeVendorWriteSafe(t)
			args := map[string]any{"path": "x"}
			automation, runID := h.automationToolCall(t, "call_"+test.name, args)
			test.setup(t, h)

			_, err := h.registry.CallTool(context.Background(), CallInput{
				ServerID: h.serverID, RunID: runID, ToolName: "vendor.write", Args: args,
			})

			assertRefusedWith(t, err, repository.AutomationToolNotAllowlistedCode)
			if got := h.reached.Load(); got != 0 {
				t.Fatalf("vendor requests = %d, want none", got)
			}
			assertOneRefusal(t, refuser, runID, automation, repository.AutomationToolNotAllowlistedCode)
		})
	}
}
