package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

// TUR-026a: an unattended run's frozen grant is intersected with the
// automation as it stands now. It may narrow or end; it never widens.
func TestUnattendedGrantStateFollowsTheAutomationAsItStandsNow(t *testing.T) {
	update := AutomationTool{ServerName: "files", ToolName: "files.update"}
	create := AutomationTool{ServerName: "files", ToolName: "files.create"}
	read := AutomationTool{ServerName: "files", ToolName: "files.read"}

	for _, test := range []struct {
		name string
		// change is what the owner does to the automation after it fired.
		change func(t *testing.T, repo *Repository, ctx context.Context, automationID string)
		pair   AutomationTool
		// wantRevoked is the run losing consent it was given.
		wantRevoked bool
		// wantFrozen is whether the run's own grant holds the pair at all.
		wantFrozen bool
	}{
		{name: "unchanged", pair: update, wantFrozen: true},
		{
			name: "unrelated edit",
			change: func(t *testing.T, repo *Repository, ctx context.Context, id string) {
				saveAllowlist(t, repo, ctx, id, "Renamed digest", update, create)
			},
			pair: update, wantFrozen: true,
		},
		{
			name: "disabled",
			change: func(t *testing.T, repo *Repository, ctx context.Context, id string) {
				if _, err := repo.SetAutomationEnabled(ctx, id, false); err != nil {
					t.Fatal(err)
				}
			},
			pair: update, wantRevoked: true, wantFrozen: true,
		},
		{
			name: "disabled, for a tool it never listed",
			change: func(t *testing.T, repo *Repository, ctx context.Context, id string) {
				if _, err := repo.SetAutomationEnabled(ctx, id, false); err != nil {
					t.Fatal(err)
				}
			},
			pair: read, wantRevoked: true,
		},
		{
			name: "deleted",
			change: func(t *testing.T, repo *Repository, ctx context.Context, id string) {
				if err := repo.DeleteAutomation(ctx, id); err != nil {
					t.Fatal(err)
				}
			},
			pair: update, wantRevoked: true, wantFrozen: true,
		},
		{
			name: "narrowed away",
			change: func(t *testing.T, repo *Repository, ctx context.Context, id string) {
				saveAllowlist(t, repo, ctx, id, "Digest", create)
			},
			pair: update, wantRevoked: true, wantFrozen: true,
		},
		{
			name: "narrowed, the pair that stayed",
			change: func(t *testing.T, repo *Repository, ctx context.Context, id string) {
				saveAllowlist(t, repo, ctx, id, "Digest", create)
			},
			pair: create, wantFrozen: true,
		},
		{
			// Widening reaches nothing: the pair is on the current allowlist
			// but never was on the frozen one, so it is not "revoked" — it is
			// simply not granted, which Grant.Allows answers.
			name: "widened",
			change: func(t *testing.T, repo *Repository, ctx context.Context, id string) {
				saveAllowlist(t, repo, ctx, id, "Digest", update, create, read)
			},
			pair: read,
		},
		{
			// The intersection is with the automation as it stands at the
			// call: re-enabled before the run's next call, nothing was lost.
			name: "disabled then re-enabled",
			change: func(t *testing.T, repo *Repository, ctx context.Context, id string) {
				if _, err := repo.SetAutomationEnabled(ctx, id, false); err != nil {
					t.Fatal(err)
				}
				if _, err := repo.SetAutomationEnabled(ctx, id, true); err != nil {
					t.Fatal(err)
				}
			},
			pair: update, wantFrozen: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, ctx := newTitleTestRepo(t)
			automation, err := repo.CreateAutomation(ctx, AutomationInput{
				Name: "Digest", Prompt: "x", Schedule: everyFiveMinutes(), Enabled: true,
				AllowedTools: []AutomationTool{update, create},
			})
			if err != nil {
				t.Fatal(err)
			}
			fire, _, err := repo.ClaimDueAutomation(ctx, mustParse(t, automation.NextDueAt), automationDefaults)
			if err != nil {
				t.Fatal(err)
			}
			if test.change != nil {
				test.change(t, repo, ctx, automation.AutomationID)
			}

			state, found, err := repo.GetUnattendedGrantState(ctx, fire.RunID, test.pair.ServerName, test.pair.ToolName)
			if err != nil || !found {
				t.Fatalf("grant state = found %v err %v, want found", found, err)
			}
			if got := state.Revoked(test.pair.ServerName, test.pair.ToolName); got != test.wantRevoked {
				t.Fatalf("revoked = %v, want %v (state %+v)", got, test.wantRevoked, state)
			}
			if got := state.Grant.Allows(test.pair.ServerName, test.pair.ToolName); got != test.wantFrozen {
				t.Fatalf("frozen grant allows %s = %v, want %v", test.pair.ToolName, got, test.wantFrozen)
			}
			if state.Grant.AutomationID != automation.AutomationID || state.Grant.AutomationName != "Digest" {
				t.Fatalf("grant = %+v, want the automation as it fired", state.Grant)
			}
		})
	}
}

// A run the user typed has no grant state, so the revocation check has
// nothing to apply to it and the ordinary path is unchanged.
func TestAHandDrivenRunHasNoUnattendedGrantState(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "By hand")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "write a file", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "qwen2.5:7b",
	})
	if err != nil {
		t.Fatal(err)
	}
	state, found, err := repo.GetUnattendedGrantState(ctx, enqueued.RunID, "files", "files.update")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("a hand-driven run reported grant state: %+v", state)
	}
}

func saveAllowlist(t *testing.T, repo *Repository, ctx context.Context, automationID string, name string, tools ...AutomationTool) {
	t.Helper()
	if _, err := repo.UpdateAutomation(ctx, automationID, AutomationInput{
		Name: name, Prompt: "x", Schedule: everyFiveMinutes(), AllowedTools: tools,
	}); err != nil {
		t.Fatal(err)
	}
}

// repeatFixture is one automation's run, claimed by a worker, as the commit
// transaction sees it: tool calls recorded, approvals created and previewed.
type repeatFixture struct {
	repo       *Repository
	ctx        context.Context
	automation Automation
	fire       AutomationFire
	job        Job
}

func newRepeatFixture(t *testing.T) repeatFixture {
	t.Helper()
	repo, ctx := newTitleTestRepo(t)
	automation, err := repo.CreateAutomation(ctx, AutomationInput{
		Name: "Digest", Prompt: "x", Schedule: everyFiveMinutes(), Enabled: true,
		AllowedTools: []AutomationTool{
			{ServerName: "files", ToolName: "files.update"},
			{ServerName: "files", ToolName: "files.create"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	fire, _, err := repo.ClaimDueAutomation(ctx, mustParse(t, automation.NextDueAt), automationDefaults)
	if err != nil {
		t.Fatal(err)
	}
	job, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "worker-1", 0, time.Hour, nil, nil)
	if err != nil || job.RunID != fire.RunID {
		t.Fatalf("claim = %+v, %v; want the automation's run", job, err)
	}
	return repeatFixture{repo: repo, ctx: ctx, automation: automation, fire: fire, job: job}
}

// pending records one tool call and leaves its approval pending with the
// preview the commit checks, the way the beacon and the preview refresh do.
func (f repeatFixture) pending(t *testing.T, toolCallID string, tool AutomationTool, argsHash string) string {
	t.Helper()
	if err := f.repo.RecordToolCallBefore(f.ctx, ToolCallRecord{ToolCallID: toolCallID, RunID: f.fire.RunID},
		"general_assistant", tool.ServerName, tool.ToolName, `{"path":"note.txt"}`, argsHash); err != nil {
		t.Fatal(err)
	}
	approval, _, err := f.repo.CreateApprovalWithEvents(f.ctx, f.fire.RunID, toolCallID, "general_assistant",
		tool.ToolName, `{"path":"note.txt"}`, argsHash, "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.repo.StoreApprovalPreview(f.ctx, ApprovalPreviewRecord{
		ApprovalID: approval.ApprovalID, Hash: "preview:" + toolCallID, SnapshotJSON: "{}",
	}, false); err != nil {
		t.Fatal(err)
	}
	return approval.ApprovalID
}

func (f repeatFixture) approve(toolCallID string, approvalID string) error {
	_, err := f.repo.ApproveUnattendedApprovalWithEvent(f.ctx, approvalID, "token:"+toolCallID, "preview:"+toolCallID)
	return err
}

// resume moves the run back to running once a decision is in, as the worker's
// ready does, so the next call can wait on an approval of its own.
func (f repeatFixture) resume(t *testing.T, approvalID string) {
	t.Helper()
	state, err := f.repo.GetRunState(f.ctx, f.fire.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.ResumeApprovedRun(f.ctx, ResumeApprovedRunInput{
		RunID: f.fire.RunID, ApprovalID: approvalID, WorkerID: "worker-1",
		AssignmentAttemptID: f.job.AssignmentAttemptID, ExpectedStateVersion: state.StateVersion,
	}); err != nil {
		t.Fatalf("resume: %v", err)
	}
}

// TUR-026c: an unattended approval is refused, in the transaction that would
// approve it, when the run already spent an approval on the same server, tool
// and arguments. Only a consumed approval proves the call ran.
func TestAnUnattendedApprovalRefusesACallTheRunAlreadySpent(t *testing.T) {
	update := AutomationTool{ServerName: "files", ToolName: "files.update"}
	for _, test := range []struct {
		name string
		// earlier is the run's call made before the one being approved, a
		// files.update with sha256:same; spent says whether its token was
		// consumed.
		earlier     AutomationTool
		earlierHash string
		spent       bool
		// disable turns the automation off before the second approval.
		disable bool
		wantErr error
	}{
		{name: "spent, same arguments", earlier: update, earlierHash: "sha256:same", spent: true, wantErr: ErrUnattendedRepeatBlocked},
		{name: "spent, different arguments", earlier: update, earlierHash: "sha256:other", spent: true},
		{name: "spent, same server and arguments, another tool",
			earlier: AutomationTool{ServerName: "files", ToolName: "files.create"}, earlierHash: "sha256:same", spent: true},
		{name: "spent, same tool and arguments on another server",
			earlier: AutomationTool{ServerName: "notes", ToolName: "files.update"}, earlierHash: "sha256:same", spent: true},
		{name: "approved, never spent", earlier: update, earlierHash: "sha256:same"},
		// A withdrawn grant is the owner's decision and says more than a
		// repeat does, so it is the refusal the run reports.
		{name: "spent, and the automation disabled since", earlier: update, earlierHash: "sha256:same", spent: true,
			disable: true, wantErr: ErrAutomationGrantRevoked},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newRepeatFixture(t)
			earlier := f.pending(t, "call_earlier", test.earlier, test.earlierHash)
			if test.earlier.ServerName == "files" {
				if err := f.approve("call_earlier", earlier); err != nil {
					t.Fatalf("approve the earlier call: %v", err)
				}
			} else if _, err := f.repo.ApproveApprovalWithEvent(f.ctx, earlier, "token:call_earlier", sql.NullString{}, ""); err != nil {
				t.Fatalf("approve the earlier call: %v", err)
			}
			f.resume(t, earlier)
			if test.spent {
				if _, err := f.repo.ConsumeApprovalWithEvent(f.ctx, earlier, ""); err != nil {
					t.Fatalf("consume: %v", err)
				}
			}
			if test.disable {
				if _, err := f.repo.SetAutomationEnabled(f.ctx, f.automation.AutomationID, false); err != nil {
					t.Fatal(err)
				}
			}

			again := f.pending(t, "call_again", update, "sha256:same")
			err := f.approve("call_again", again)
			if test.wantErr == nil {
				if err != nil {
					t.Fatalf("approve = %v, want approved", err)
				}
				return
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("approve = %v, want %v", err, test.wantErr)
			}
			record, err := f.repo.GetApproval(f.ctx, again)
			if err != nil {
				t.Fatal(err)
			}
			if record.Status != "pending" || record.ApprovalToken != "" {
				t.Fatalf("approval = %s token %q, want it left pending and unsigned", record.Status, record.ApprovalToken)
			}
			var audited int
			if err := f.repo.db.QueryRowContext(f.ctx,
				`SELECT COUNT(*) FROM audit_logs WHERE action = 'approval.approved' AND target = ?`, again).Scan(&audited); err != nil {
				t.Fatal(err)
			}
			if audited != 0 {
				t.Fatalf("approval.approved audit rows for the refused approval = %d, want none", audited)
			}
		})
	}
}

// The lookup starts from the run's own tool calls, so it costs one index
// range per approval however many approvals the database holds. Statistics
// claiming consumed approvals are rare and a run's tool calls are many would
// turn a plain join around to start from idx_approvals_status; the plan must
// hold under them too.
func TestTheRepeatLookupStartsFromTheRunsToolCalls(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	conn, err := repo.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	for _, statement := range []string{
		`ANALYZE`,
		`DELETE FROM sqlite_stat1`,
		`INSERT INTO sqlite_stat1 (tbl, idx, stat) VALUES
			('approvals', 'idx_approvals_status', '1000000 1 1'),
			('tool_calls', 'idx_tool_calls_run', '1000000 100000 1')`,
		`ANALYZE sqlite_schema`,
	} {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	rows, err := conn.QueryContext(ctx, `EXPLAIN QUERY PLAN `+unattendedRepeatQuery, "run", "files", "files.update", "sha256:same")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "SEARCH tc USING INDEX idx_tool_calls_run (run_id=?)") ||
		!strings.Contains(joined, "SEARCH a USING INDEX idx_approvals_tool_call (tool_call_id=?)") ||
		strings.Contains(joined, "SCAN tc") || strings.Contains(joined, "SCAN a") ||
		strings.Index(joined, "SEARCH tc") > strings.Index(joined, "SEARCH a") {
		t.Fatalf("plan:\n%s\nwant a seek on idx_tool_calls_run, then approvals by idx_approvals_tool_call, and no scan of either", joined)
	}
}
