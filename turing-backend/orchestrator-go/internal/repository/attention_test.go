package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/attention"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// UX-002a fixtures. They write rows directly so a test can pin every
// timestamp the queue orders by.

func seedAttentionSession(t *testing.T, ctx context.Context, repo *Repository, sessionID, title string) {
	t.Helper()
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO sessions (id, title, title_origin, status, created_at, updated_at)
		VALUES (?, ?, 'explicit', 'active', '2026-01-01T00:00:00.000000000Z', '2026-01-01T00:00:00.000000000Z')`,
		sessionID, title); err != nil {
		t.Fatalf("insert session: %v", err)
	}
}

func seedAttentionRun(t *testing.T, ctx context.Context, repo *Repository, sessionID, runID, status string, at time.Time) {
	t.Helper()
	run := telemetryRun{id: runID, sessionID: sessionID, createdAt: at, status: status}
	if status == "completed" || status == "failed" || status == "cancelled" {
		run.startedAt, run.finishedAt = at, at
	}
	insertTelemetryRun(t, ctx, repo, run)
}

func seedAttentionApproval(t *testing.T, ctx context.Context, repo *Repository, approvalID, runID string, createdAt time.Time, expiresAt string) {
	t.Helper()
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO approvals (id, run_id, agent_id, tool_name, args_json, args_hash, status, expires_at, created_at)
		VALUES (?, ?, 'general_assistant', 'files.create', '{"path":"SENTINEL_ARGUMENT"}', 'sha256:test', 'pending', ?, ?)`,
		approvalID, runID, expiresAt, FormatTimestamp(createdAt)); err != nil {
		t.Fatalf("insert approval: %v", err)
	}
}

// seedAttentionDelegation writes a delegation from parentRun in
// parentSession to a hidden child session whose run has childStatus.
func seedAttentionDelegation(t *testing.T, ctx context.Context, repo *Repository, delegationID, parentSession, parentRun, consent, childStatus string, createdAt time.Time) (childSession string, childRun string) {
	t.Helper()
	childSession, childRun = "sess_child_"+delegationID, "run_child_"+delegationID
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO sessions (id, kind, parent_session_id, status, created_at, updated_at)
		VALUES (?, 'delegation', ?, 'active', '2026-01-01T00:00:00.000000000Z', '2026-01-01T00:00:00.000000000Z')`,
		childSession, parentSession); err != nil {
		t.Fatalf("insert child session: %v", err)
	}
	seedAttentionRun(t, ctx, repo, childSession, childRun, childStatus, createdAt)
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO delegations (id, parent_session_id, parent_run_id, parent_tool_call_id, child_session_id, child_run_id,
			profile_id, profile_revision, args_hash, created_at, egress_tools_json, egress_consent)
		VALUES (?, ?, ?, ?, ?, ?, 'research', 'rev-1', 'sha256:x', ?, '["integrations/github.list_issues"]', ?)`,
		delegationID, parentSession, parentRun, "call_"+delegationID, childSession, childRun,
		FormatTimestamp(createdAt), consent); err != nil {
		t.Fatalf("insert delegation: %v", err)
	}
	return childSession, childRun
}

// seedFailedAutomation creates an enabled automation whose last run ended
// with status and code at the given time, in its own conversation.
func seedFailedAutomation(t *testing.T, ctx context.Context, repo *Repository, name, status, code string, at time.Time) (Automation, string) {
	t.Helper()
	automation, err := repo.CreateAutomation(ctx, AutomationInput{
		Name: name, Prompt: "x", Schedule: everyFiveMinutes(), Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "sess_" + automation.AutomationID
	seedAttentionSession(t, ctx, repo, sessionID, name)
	runID := "run_" + automation.AutomationID
	seedAttentionRun(t, ctx, repo, sessionID, runID, status, at)
	setAttentionRunOutcome(t, ctx, repo, runID, status, code)
	if _, err := repo.db.ExecContext(ctx,
		`UPDATE automations SET last_run_id = ?, last_run_at = ?, session_id = ? WHERE id = ?`,
		runID, FormatTimestamp(at), sessionID, automation.AutomationID); err != nil {
		t.Fatal(err)
	}
	return automation, runID
}

func setAttentionRunOutcome(t *testing.T, ctx context.Context, repo *Repository, runID, status, code string) {
	t.Helper()
	var err error
	switch status {
	case "failed":
		_, err = repo.db.ExecContext(ctx, `UPDATE agent_runs SET error_code = ? WHERE id = ?`, code, runID)
	case "cancelled":
		reason := "abandoned"
		if code == runoutcome.CodeUserCancelled {
			reason = "user_cancelled"
		}
		_, err = repo.db.ExecContext(ctx,
			`UPDATE agent_runs SET cancellation_reason = ?, outcome_reason = ? WHERE id = ?`, code, reason, runID)
	}
	if err != nil {
		t.Fatal(err)
	}
}

// seedRemoteEgressBlock writes the record ClaimDueAutomation writes when it
// blocks an occurrence before any run exists.
func seedRemoteEgressBlock(t *testing.T, ctx context.Context, repo *Repository, automationID string, at time.Time) string {
	t.Helper()
	auditID := fmt.Sprintf("audit_%s_%d", automationID, at.UnixNano())
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO audit_logs (id, actor_type, actor_id, action, target, payload_json, created_at)
		VALUES (?, 'automation', ?, 'automation.remote_egress_blocked', ?, ?, ?)`,
		auditID, automationID, automationID,
		`{"code":"`+AutomationFailureRemoteEgressRequiresInteractiveConsent+`","provider":"openai_compatible"}`,
		FormatTimestamp(at)); err != nil {
		t.Fatal(err)
	}
	return auditID
}

func seedDeletionReceipt(t *testing.T, ctx context.Context, repo *Repository, sessionID, state string, retryable bool, began time.Time, parent, code string) {
	t.Helper()
	var parentID, errorCode any
	if parent != "" {
		parentID = parent
	}
	if code != "" {
		errorCode = code
	}
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO session_deletions (session_id, lifecycle_version, state, quiesce_deadline_at, terminal_sequence,
			retryable, error_code, parent_session_id)
		VALUES (?, 1, ?, ?, 1, ?, ?, ?)`,
		sessionID, state, FormatTimestamp(began.Add(sessionDeletionQuiesceLease)), boolToInt(retryable), errorCode, parentID); err != nil {
		t.Fatal(err)
	}
}

func firstAttentionPage(t *testing.T, ctx context.Context, repo *Repository) AttentionPage {
	t.Helper()
	page, err := repo.ListAttention(ctx, AttentionQuery{Limit: MaxAttentionPageLimit})
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func attentionRowsOfKind(page AttentionPage, kind attention.Kind) []AttentionRow {
	var rows []AttentionRow
	for _, row := range page.Rows {
		if row.Kind == kind {
			rows = append(rows, row)
		}
	}
	return rows
}

// One row per source, each in its stage's place, carrying the ids its action
// needs. A delegated task's approval appears under its parent conversation.
func TestListAttentionPlacesEachSourceInItsStage(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)

	seedAttentionSession(t, ctx, repo, "sess_chat", "Trip planning")
	seedAttentionRun(t, ctx, repo, "sess_chat", "run_chat", "waiting_approval", base)
	seedAttentionApproval(t, ctx, repo, "appr_chat", "run_chat", base.Add(time.Minute), FormatTimestamp(time.Now().Add(time.Hour)))
	seedAttentionRun(t, ctx, repo, "sess_chat", "run_parent", "completed", base)
	_, childRun := seedAttentionDelegation(t, ctx, repo, "dlg_waiting", "sess_chat", "run_parent", "", "waiting_approval", base)
	seedAttentionApproval(t, ctx, repo, "appr_child", childRun, base.Add(2*time.Minute), FormatTimestamp(time.Now().Add(time.Hour)))
	_, consentRun := seedAttentionDelegation(t, ctx, repo, "dlg_consent", "sess_chat", "run_parent", "pending", "queued", base.Add(3*time.Minute))
	automation, failedRun := seedFailedAutomation(t, ctx, repo, "Nightly digest", "failed", AutomationToolNotAllowlistedCode, base.Add(4*time.Minute))
	seedDeletionReceipt(t, ctx, repo, "sess_withdrawn", "failed_external", true, base.Add(5*time.Minute), "", SessionDeletionArtifactCleanupPending)

	page := firstAttentionPage(t, ctx, repo)
	if page.More || len(page.Rows) != 5 {
		t.Fatalf("page = %d rows, more %v; want 5 and no next page: %+v", len(page.Rows), page.More, page.Rows)
	}
	want := []AttentionRow{
		{Kind: attention.KindApproval, ID: "appr_child", SessionID: "sess_chat", SessionTitle: "Trip planning",
			ApprovalID: "appr_child", RunID: childRun, DelegationID: "dlg_waiting",
			Conditions: attention.Conditions{PendingApproval: true}, SinceNanos: base.Add(2 * time.Minute).UnixNano()},
		{Kind: attention.KindApproval, ID: "appr_chat", SessionID: "sess_chat", SessionTitle: "Trip planning",
			ApprovalID: "appr_chat", RunID: "run_chat",
			Conditions: attention.Conditions{PendingApproval: true}, SinceNanos: base.Add(time.Minute).UnixNano()},
		{Kind: attention.KindDelegation, ID: "dlg_consent", SessionID: "sess_chat", SessionTitle: "Trip planning",
			RunID: consentRun, DelegationID: "dlg_consent",
			Conditions: attention.Conditions{PendingConsent: true}, SinceNanos: base.Add(3 * time.Minute).UnixNano()},
		{Kind: attention.KindAutomation, ID: automation.AutomationID, SessionID: "sess_" + automation.AutomationID,
			SessionTitle: "Nightly digest", RunID: failedRun, AutomationID: automation.AutomationID,
			AutomationName: "Nightly digest", OccurrenceKey: failedRun, Cause: AutomationToolNotAllowlistedCode,
			Conditions: attention.Conditions{BlockedAutomation: true}, SinceNanos: base.Add(4 * time.Minute).UnixNano()},
		{Kind: attention.KindDeletion, ID: "sess_withdrawn", SessionID: "sess_withdrawn",
			Cause:      SessionDeletionArtifactCleanupPending,
			Conditions: attention.Conditions{RetryableDeletion: true}, SinceNanos: base.Add(5 * time.Minute).UnixNano()},
	}
	for index := range want {
		if page.Rows[index] != want[index] {
			t.Fatalf("row %d = %+v\nwant %+v", index, page.Rows[index], want[index])
		}
	}
}

// Healthy states take no space: an approval already decided or expired, a
// consent decided or whose task stopped, a child's deletion receipt, one
// still quiescing or no longer retryable, and anything under a conversation
// being deleted.
func TestListAttentionLeavesOutWhatNeedsNobody(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	base := time.Now().UTC().Add(-time.Hour)
	future := FormatTimestamp(time.Now().Add(time.Hour))

	seedAttentionSession(t, ctx, repo, "sess_chat", "Chat")
	seedAttentionRun(t, ctx, repo, "sess_chat", "run_chat", "waiting_approval", base)
	seedAttentionApproval(t, ctx, repo, "appr_expired", "run_chat", base, FormatTimestamp(time.Now().Add(-time.Second)))
	seedAttentionApproval(t, ctx, repo, "appr_decided", "run_chat", base, future)
	if _, err := repo.db.ExecContext(ctx, `UPDATE approvals SET status = 'approved' WHERE id = 'appr_decided'`); err != nil {
		t.Fatal(err)
	}
	seedAttentionRun(t, ctx, repo, "sess_chat", "run_parent", "completed", base)
	seedAttentionDelegation(t, ctx, repo, "dlg_granted", "sess_chat", "run_parent", "granted", "queued", base)
	seedAttentionDelegation(t, ctx, repo, "dlg_stopped", "sess_chat", "run_parent", "pending", "cancelled", base)
	seedAttentionDelegation(t, ctx, repo, "dlg_none", "sess_chat", "run_parent", "", "queued", base)

	seedAttentionSession(t, ctx, repo, "sess_deleting", "Going away")
	if _, err := repo.db.ExecContext(ctx, `UPDATE sessions SET deletion_state = 'deleting' WHERE id = 'sess_deleting'`); err != nil {
		t.Fatal(err)
	}
	seedAttentionRun(t, ctx, repo, "sess_deleting", "run_deleting", "waiting_approval", base)
	seedAttentionApproval(t, ctx, repo, "appr_deleting", "run_deleting", base, future)
	seedAttentionDelegation(t, ctx, repo, "dlg_deleting", "sess_deleting", "run_deleting", "pending", "queued", base)

	seedDeletionReceipt(t, ctx, repo, "sess_child_gone", "failed_external", true, base, "sess_parent", SessionDeletionArtifactCleanupPending)
	seedDeletionReceipt(t, ctx, repo, "sess_quiescing", "quiescing", true, base, "", "")
	seedDeletionReceipt(t, ctx, repo, "sess_done", "completed", false, base, "", "")
	seedDeletionReceipt(t, ctx, repo, "sess_stuck", "failed_external", false, base, "", "execution_unreconciled")

	if page := firstAttentionPage(t, ctx, repo); len(page.Rows) != 0 {
		t.Fatalf("rows = %+v, want none", page.Rows)
	}
}

// An approval whose expiry cannot be read is listed in its place as
// unclassifiable rather than dropped: the projection cannot say whether it
// is still pending, and an empty list must be a fact.
func TestListAttentionListsAnUnreadableExpiryAsUnclassified(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	base := time.Now().UTC().Add(-time.Hour)
	seedAttentionSession(t, ctx, repo, "sess_chat", "Chat")
	seedAttentionRun(t, ctx, repo, "sess_chat", "run_chat", "waiting_approval", base)
	seedAttentionApproval(t, ctx, repo, "appr_unreadable", "run_chat", base, "not a time")

	page := firstAttentionPage(t, ctx, repo)
	if len(page.Rows) != 1 || page.Rows[0].ApprovalID != "appr_unreadable" {
		t.Fatalf("rows = %+v, want the approval listed", page.Rows)
	}
	if got := attention.Stage(page.Rows[0].Conditions); got != attention.Unavailable {
		t.Fatalf("stage = %s, want unavailable", got)
	}
}

// A block whose payload is not JSON is still listed, with no cause, rather
// than failing the whole read, and the automation library reads it alike.
func TestAMalformedBlockPayloadStillListsItsRow(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	automation, err := repo.CreateAutomation(ctx, AutomationInput{Name: "Remote", Prompt: "x", Schedule: everyFiveMinutes(), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	auditID := seedRemoteEgressBlock(t, ctx, repo, automation.AutomationID, time.Now().UTC().Add(-time.Hour))
	if _, err := repo.db.ExecContext(ctx, `UPDATE audit_logs SET payload_json = 'not json' WHERE id = ?`, auditID); err != nil {
		t.Fatal(err)
	}
	rows := attentionRowsOfKind(firstAttentionPage(t, ctx, repo), attention.KindAutomation)
	if len(rows) != 1 || rows[0].OccurrenceKey != auditID || rows[0].Cause != "" {
		t.Fatalf("rows = %+v, want the block listed by %s with no cause", rows, auditID)
	}
	library, err := repo.ListAutomations(ctx)
	if err != nil || len(library) != 1 || library[0].LastOccurrenceFailureCode != "" || library[0].LastOccurrenceFailedAt == "" {
		t.Fatalf("ListAutomations = %+v, %v; want the block's time and no code", library, err)
	}
}

// Stage 3 lists an enabled automation whose latest occurrence failed or was
// blocked, keyed by that occurrence, and nothing else.
func TestListAttentionRaisesABlockedRowOnlyForAFailingLatestOccurrence(t *testing.T) {
	long := time.Now().UTC().Add(-90 * 24 * time.Hour)
	recent := time.Now().UTC().Add(-time.Hour)
	for _, test := range []struct {
		name     string
		status   string
		code     string
		at       time.Time
		disable  bool
		deleting bool
		raised   bool
	}{
		{name: "a failed run", status: "failed", code: "provider_unavailable", at: recent, raised: true},
		{name: "a failure 90 days old that is still the latest", status: "failed", code: "provider_unavailable", at: long, raised: true},
		{name: "a run the queue bound cancelled", status: "cancelled", code: runoutcome.CodeQueueWaitExpired, at: recent, raised: true},
		{name: "a completed run", status: "completed", at: recent},
		{name: "a run still going", status: "running", at: recent},
		{name: "a run the owner cancelled", status: "cancelled", code: runoutcome.CodeUserCancelled, at: recent},
		{name: "a run the owner's quiet switch cancelled", status: "cancelled", code: runoutcome.CodeOwnerQuiet, at: recent},
		{name: "a disabled automation's failure", status: "failed", code: "provider_unavailable", at: recent, disable: true},
		{name: "a run abandoned by deleting its conversation", status: "cancelled", code: runoutcome.CodeClientCancelled, at: recent, deleting: true},
		{name: "a failed run whose conversation is being deleted", status: "failed", code: "provider_unavailable", at: recent, deleting: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, ctx := newTitleTestRepo(t)
			automation, runID := seedFailedAutomation(t, ctx, repo, "Digest", test.status, test.code, test.at)
			if test.disable {
				if _, err := repo.SetAutomationEnabled(ctx, automation.AutomationID, false); err != nil {
					t.Fatal(err)
				}
			}
			if test.deleting {
				if _, err := repo.db.ExecContext(ctx, `UPDATE sessions SET deletion_state = 'deleting' WHERE id = ?`,
					"sess_"+automation.AutomationID); err != nil {
					t.Fatal(err)
				}
			}
			rows := attentionRowsOfKind(firstAttentionPage(t, ctx, repo), attention.KindAutomation)
			if !test.raised {
				if len(rows) != 0 {
					t.Fatalf("rows = %+v, want none", rows)
				}
				return
			}
			if len(rows) != 1 || rows[0].OccurrenceKey != runID || rows[0].Cause != test.code ||
				rows[0].SinceNanos != test.at.UnixNano() || attention.Stage(rows[0].Conditions) != attention.PersonBlocked {
				t.Fatalf("rows = %+v, want one person/blocked row for run %s (%s)", rows, runID, test.code)
			}
		})
	}
}

// A remote-egress block has no run: it is keyed by its audit record, and a
// block older than the latest run no longer counts once that run succeeded.
func TestListAttentionKeysARemoteEgressBlockByItsAuditRecord(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	base := time.Now().UTC().Add(-time.Hour)
	automation, err := repo.CreateAutomation(ctx, AutomationInput{Name: "Remote", Prompt: "x", Schedule: everyFiveMinutes(), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	auditID := seedRemoteEgressBlock(t, ctx, repo, automation.AutomationID, base)
	rows := attentionRowsOfKind(firstAttentionPage(t, ctx, repo), attention.KindAutomation)
	if len(rows) != 1 || rows[0].OccurrenceKey != auditID || rows[0].RunID != "" ||
		rows[0].Cause != AutomationFailureRemoteEgressRequiresInteractiveConsent || rows[0].SinceNanos != base.UnixNano() {
		t.Fatalf("rows = %+v, want one row keyed by %s", rows, auditID)
	}

	completed, runID := seedFailedAutomation(t, ctx, repo, "Healthy", "completed", "", base.Add(time.Minute))
	seedRemoteEgressBlock(t, ctx, repo, completed.AutomationID, base)
	for _, row := range attentionRowsOfKind(firstAttentionPage(t, ctx, repo), attention.KindAutomation) {
		if row.AutomationID == completed.AutomationID {
			t.Fatalf("row %+v raised for a block older than successful run %s", row, runID)
		}
	}
}

// TUR-026d's allowlist-ceiling block and a revoked grant on a still-enabled
// automation whose allowlist was narrowed each raise a person/blocked row,
// through the real refusal path.
func TestListAttentionRaisesBlockedRowsForTUR026Blocks(t *testing.T) {
	for _, test := range []struct {
		name   string
		code   string
		narrow bool
	}{
		{name: "allowlist ceiling", code: AutomationToolNotAllowlistedCode},
		{name: "grant revoked by a narrowed allowlist", code: AutomationGrantRevokedCode, narrow: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, ctx := newTitleTestRepo(t)
			listed := AutomationTool{ServerName: "files", ToolName: "files.update"}
			automation, fire := runningAutomationRun(t, repo, ctx, listed, AutomationTool{ServerName: "files", ToolName: "files.read"})
			tool := AutomationTool{ServerName: "vendor", ToolName: "vendor.lookup"}
			if test.narrow {
				tool = listed
				if _, err := repo.UpdateAutomation(ctx, automation.AutomationID, AutomationInput{
					Name: automation.Name, Prompt: automation.Prompt, Schedule: automation.Schedule, Enabled: true,
					AllowedTools: []AutomationTool{{ServerName: "files", ToolName: "files.read"}},
				}); err != nil {
					t.Fatal(err)
				}
			}
			failure := runoutcome.NormalizeFailure(runoutcome.OriginAutomationPolicy, test.code, runoutcome.RetryClassNever)
			if _, moved, err := repo.FailUnattendedRun(ctx, fire.RunID, failure, &UnattendedToolBlock{
				AutomationID: automation.AutomationID, AutomationName: automation.Name,
				ServerName: tool.ServerName, ToolName: tool.ToolName, Code: test.code,
			}); err != nil || !moved {
				t.Fatalf("FailUnattendedRun = moved %v, %v", moved, err)
			}
			rows := attentionRowsOfKind(firstAttentionPage(t, ctx, repo), attention.KindAutomation)
			if len(rows) != 1 || rows[0].OccurrenceKey != fire.RunID || rows[0].Cause != test.code ||
				attention.Stage(rows[0].Conditions) != attention.PersonBlocked {
				t.Fatalf("rows = %+v, want one person/blocked row for %s", rows, test.code)
			}
		})
	}
}

// Dismissing the listed occurrence hides exactly that row; the next failing
// run raises it again.
func TestDismissAttentionHidesOneOccurrenceUntilTheNextFailure(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	base := time.Now().UTC().Add(-time.Hour)
	automation, runID := seedFailedAutomation(t, ctx, repo, "Digest", "failed", "provider_unavailable", base)
	other, _ := seedFailedAutomation(t, ctx, repo, "Other", "failed", "provider_unavailable", base)

	if err := repo.DismissAttention(ctx, automation.AutomationID, runID); err != nil {
		t.Fatal(err)
	}
	rows := attentionRowsOfKind(firstAttentionPage(t, ctx, repo), attention.KindAutomation)
	if len(rows) != 1 || rows[0].AutomationID != other.AutomationID {
		t.Fatalf("rows after dismissal = %+v, want only %s", rows, other.AutomationID)
	}

	nextRun := "run_next_" + automation.AutomationID
	seedAttentionRun(t, ctx, repo, "sess_"+automation.AutomationID, nextRun, "failed", base.Add(time.Minute))
	if _, err := repo.db.ExecContext(ctx, `UPDATE automations SET last_run_id = ?, last_run_at = ? WHERE id = ?`,
		nextRun, FormatTimestamp(base.Add(time.Minute)), automation.AutomationID); err != nil {
		t.Fatal(err)
	}
	rows = attentionRowsOfKind(firstAttentionPage(t, ctx, repo), attention.KindAutomation)
	if len(rows) != 2 || rows[0].OccurrenceKey != nextRun {
		t.Fatalf("rows after the next failure = %+v, want %s raised again first", rows, nextRun)
	}
}

// A block with no run can be dismissed, and a later block raises the row again
// although last_run_id never changed.
func TestDismissAttentionBindsANoRunBlockToItsAuditRecord(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	base := time.Now().UTC().Add(-time.Hour)
	automation, runID := seedFailedAutomation(t, ctx, repo, "Remote", "completed", "", base)
	first := seedRemoteEgressBlock(t, ctx, repo, automation.AutomationID, base.Add(time.Minute))
	if err := repo.DismissAttention(ctx, automation.AutomationID, first); err != nil {
		t.Fatal(err)
	}
	if rows := attentionRowsOfKind(firstAttentionPage(t, ctx, repo), attention.KindAutomation); len(rows) != 0 {
		t.Fatalf("rows after dismissing the block = %+v, want none", rows)
	}
	second := seedRemoteEgressBlock(t, ctx, repo, automation.AutomationID, base.Add(2*time.Minute))
	rows := attentionRowsOfKind(firstAttentionPage(t, ctx, repo), attention.KindAutomation)
	if len(rows) != 1 || rows[0].OccurrenceKey != second {
		t.Fatalf("rows after a later block = %+v, want %s raised again (last_run_id still %s)", rows, second, runID)
	}
}

// A stale key, a run's or an audit record's, is refused and leaves the newer
// occurrence listed: a stale screen cannot hide a newer failure.
func TestDismissAttentionRefusesAStaleOccurrenceKey(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	base := time.Now().UTC().Add(-time.Hour)
	automation, runID := seedFailedAutomation(t, ctx, repo, "Digest", "failed", "provider_unavailable", base)
	listed := attentionRowsOfKind(firstAttentionPage(t, ctx, repo), attention.KindAutomation)[0].OccurrenceKey
	if listed != runID {
		t.Fatalf("listed key = %s, want %s", listed, runID)
	}

	block := seedRemoteEgressBlock(t, ctx, repo, automation.AutomationID, base.Add(time.Minute))
	if err := repo.DismissAttention(ctx, automation.AutomationID, listed); !errors.Is(err, ErrAttentionOccurrenceStale) {
		t.Fatalf("dismissing the run listed before a newer block = %v, want ErrAttentionOccurrenceStale", err)
	}
	rows := attentionRowsOfKind(firstAttentionPage(t, ctx, repo), attention.KindAutomation)
	if len(rows) != 1 || rows[0].OccurrenceKey != block {
		t.Fatalf("rows = %+v, want the newer block %s still listed", rows, block)
	}

	nextRun := "run_next_" + automation.AutomationID
	seedAttentionRun(t, ctx, repo, "sess_"+automation.AutomationID, nextRun, "failed", base.Add(2*time.Minute))
	if _, err := repo.db.ExecContext(ctx, `UPDATE automations SET last_run_id = ?, last_run_at = ? WHERE id = ?`,
		nextRun, FormatTimestamp(base.Add(2*time.Minute)), automation.AutomationID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DismissAttention(ctx, automation.AutomationID, block); !errors.Is(err, ErrAttentionOccurrenceStale) {
		t.Fatalf("dismissing the block listed before a newer run = %v, want ErrAttentionOccurrenceStale", err)
	}
	for _, key := range []string{"", "run_unknown", "audit_unknown"} {
		if err := repo.DismissAttention(ctx, automation.AutomationID, key); !errors.Is(err, ErrAttentionOccurrenceStale) {
			t.Fatalf("dismissing %q = %v, want ErrAttentionOccurrenceStale", key, err)
		}
	}
	rows = attentionRowsOfKind(firstAttentionPage(t, ctx, repo), attention.KindAutomation)
	if len(rows) != 1 || rows[0].OccurrenceKey != nextRun {
		t.Fatalf("rows = %+v, want the newer run %s still listed", rows, nextRun)
	}
}

// Only a failing occurrence can be dismissed, and only an automation's:
// approvals, consents and deletions have no automation id to name.
func TestDismissAttentionRefusesWhatIsNotAFailingAutomation(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	base := time.Now().UTC().Add(-time.Hour)
	healthy, runID := seedFailedAutomation(t, ctx, repo, "Healthy", "running", "", base)
	if err := repo.DismissAttention(ctx, healthy.AutomationID, runID); !errors.Is(err, ErrAttentionOccurrenceStale) {
		t.Fatalf("pre-dismissing a run still going = %v, want ErrAttentionOccurrenceStale", err)
	}
	quieted, quietedRun := seedFailedAutomation(t, ctx, repo, "Quieted", "cancelled", runoutcome.CodeOwnerQuiet, base)
	if err := repo.DismissAttention(ctx, quieted.AutomationID, quietedRun); !errors.Is(err, ErrAttentionOccurrenceStale) {
		t.Fatalf("dismissing a run the quiet switch cancelled = %v, want ErrAttentionOccurrenceStale", err)
	}
	disabled, disabledRun := seedFailedAutomation(t, ctx, repo, "Disabled", "failed", "provider_unavailable", base)
	if _, err := repo.SetAutomationEnabled(ctx, disabled.AutomationID, false); err != nil {
		t.Fatal(err)
	}
	if err := repo.DismissAttention(ctx, disabled.AutomationID, disabledRun); !errors.Is(err, ErrAttentionOccurrenceStale) {
		t.Fatalf("dismissing a disabled automation = %v, want ErrAttentionOccurrenceStale", err)
	}
	seedAttentionSession(t, ctx, repo, "sess_chat", "Chat")
	seedAttentionRun(t, ctx, repo, "sess_chat", "run_chat", "waiting_approval", base)
	seedAttentionApproval(t, ctx, repo, "appr_chat", "run_chat", base, FormatTimestamp(time.Now().Add(time.Hour)))
	seedDeletionReceipt(t, ctx, repo, "sess_withdrawn", "failed_external", true, base, "", SessionDeletionArtifactCleanupPending)
	for _, id := range []string{"appr_chat", "run_chat", "sess_withdrawn", "dlg_unknown"} {
		if err := repo.DismissAttention(ctx, id, id); !errors.Is(err, ErrAutomationNotFound) {
			t.Fatalf("dismissing %s = %v, want ErrAutomationNotFound", id, err)
		}
	}
	if err := repo.DismissAttention(ctx, healthy.AutomationID+"_deleted", runID); !errors.Is(err, ErrAutomationNotFound) {
		t.Fatalf("dismissing a deleted automation = %v, want ErrAutomationNotFound", err)
	}
}

// attentionSourceStatement is the statement ListAttention runs for one
// source's first page.
func attentionSourceStatement(t *testing.T, kind attention.Kind) (string, []any) {
	t.Helper()
	for _, source := range attentionSources {
		if source.kind == kind {
			query, args, read := source.statement(AttentionQuery{Limit: MaxAttentionPageLimit}, time.Now().UnixNano())
			if !read {
				t.Fatalf("source %d reads nothing on a first page", kind)
			}
			return query, args
		}
	}
	t.Fatalf("no source of kind %d", kind)
	return "", nil
}

func explainQueryPlan(t *testing.T, ctx context.Context, repo *Repository, query string, args ...any) string {
	t.Helper()
	rows, err := repo.db.QueryContext(ctx, `EXPLAIN QUERY PLAN `+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(plan, "\n")
}

// seedAutomationHistory is the cost fixture: 1,000 enabled automations that
// never fired, and 100,000 remote-egress blocks belonging to another one.
func seedAutomationHistory(t *testing.T, ctx context.Context, repo *Repository) Automation {
	t.Helper()
	tx, err := repo.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	created := FormatTimestamp(time.Now().UTC().Add(-time.Hour))
	for index := range 1000 {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO automations (id, name, prompt, schedule_kind, interval_seconds, enabled, next_due_at, created_at, updated_at)
			VALUES (?, ?, 'x', 'interval', 300, 1, ?, ?, ?)`,
			fmt.Sprintf("auto_quiet_%04d", index), fmt.Sprintf("Quiet %04d", index), created, created, created); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	noisy, err := repo.CreateAutomation(ctx, AutomationInput{Name: "Noisy", Prompt: "x", Schedule: everyFiveMinutes(), Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	tx, err = repo.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	statement, err := tx.PrepareContext(ctx, `
		INSERT INTO audit_logs (id, actor_type, actor_id, action, target, payload_json, created_at)
		VALUES (?, 'automation', ?, 'automation.remote_egress_blocked', ?, '{"code":"remote_egress_requires_interactive_consent"}', ?)`)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(-48 * time.Hour)
	for index := range 100_000 {
		if _, err := statement.ExecContext(ctx, fmt.Sprintf("audit_noisy_%06d", index), noisy.AutomationID, noisy.AutomationID,
			FormatTimestamp(start.Add(time.Duration(index)*time.Second))); err != nil {
			t.Fatal(err)
		}
	}
	_ = statement.Close()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return noisy
}

// p95 times call after one untimed warm-up, so a cold cache is not a sample.
func p95(t *testing.T, runs int, call func() error) time.Duration {
	t.Helper()
	if err := call(); err != nil {
		t.Fatal(err)
	}
	samples := make([]time.Duration, 0, runs)
	for range runs {
		started := time.Now()
		if err := call(); err != nil {
			t.Fatal(err)
		}
		samples = append(samples, time.Since(started))
	}
	slices.Sort(samples)
	return samples[(len(samples)*95+99)/100-1]
}

const attentionPageBudget = 100 * time.Millisecond

// Each automation's latest-block lookup, in the list and in a dismissal, is
// one seek on the partial index, so another automation's long history does
// not grow either.
func TestAttentionLatestBlockLookupUsesItsPartialIndex(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	noisy := seedAutomationHistory(t, ctx, repo)
	const index = "idx_audit_remote_egress_blocked"

	// One seek per automation: a search on the index by actor and time, never
	// a scan of it or of the table.
	seek := "SEARCH candidate USING INDEX " + index + " (actor_id=? AND created_at>?)"
	listQuery, listArgs := attentionSourceStatement(t, attention.KindAutomation)
	listPlan := explainQueryPlan(t, ctx, repo, listQuery, listArgs...)
	if !strings.Contains(listPlan, seek) || strings.Contains(listPlan, "SCAN candidate") {
		t.Fatalf("ListAttention's latest-block lookup is not one seek on %s:\n%s", index, listPlan)
	}
	dismissPlan := explainQueryPlan(t, ctx, repo, automationOccurrenceQuery, noisy.AutomationID)
	if !strings.Contains(dismissPlan, seek) || strings.Contains(dismissPlan, "SCAN candidate") {
		t.Fatalf("DismissAttention's latest-block lookup is not one seek on %s:\n%s", index, dismissPlan)
	}
	libraryPlan := explainQueryPlan(t, ctx, repo, automationSelect+` WHERE a.id = ?`, noisy.AutomationID)
	if !strings.Contains(libraryPlan, seek) || strings.Contains(libraryPlan, "SCAN candidate") {
		t.Fatalf("the automation library's latest-block lookup is not one seek on %s:\n%s", index, libraryPlan)
	}
	consentQuery, consentArgs := attentionSourceStatement(t, attention.KindDelegation)
	consentPlan := explainQueryPlan(t, ctx, repo, consentQuery, consentArgs...)
	if !strings.Contains(consentPlan, "SCAN d USING INDEX idx_delegations_consent_pending") {
		t.Fatalf("ListAttention's consent plan does not use idx_delegations_consent_pending:\n%s", consentPlan)
	}

	rows := attentionRowsOfKind(firstAttentionPage(t, ctx, repo), attention.KindAutomation)
	if len(rows) != 1 || rows[0].AutomationID != noisy.AutomationID || rows[0].OccurrenceKey != "audit_noisy_099999" {
		t.Fatalf("rows = %+v, want the noisy automation keyed by its latest block", rows)
	}
	if got := p95(t, 20, func() error {
		_, err := repo.ListAttention(ctx, AttentionQuery{Limit: MaxAttentionPageLimit})
		return err
	}); got > attentionPageBudget {
		t.Fatalf("ListAttention p95 = %s, want under %s", got, attentionPageBudget)
	}
	if got := p95(t, 20, func() error {
		err := repo.DismissAttention(ctx, noisy.AutomationID, "audit_noisy_099999")
		return err
	}); got > attentionPageBudget {
		t.Fatalf("DismissAttention p95 = %s, want under %s", got, attentionPageBudget)
	}
}

// With 10,000 terminal runs, 2,000 completed deletions and 200 stale consents
// behind them and 100 pending obligations, every page of the pass reads within
// the budget.
func TestListAttentionPageStaysWithinItsBudget(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	base := time.Now().UTC().Add(-48 * time.Hour)
	seedAttentionSession(t, ctx, repo, "sess_history", "History")
	tx, err := repo.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index := range 10_000 {
		at := FormatTimestamp(base.Add(time.Duration(index) * time.Second))
		messageID := fmt.Sprintf("msg_history_%05d", index)
		if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
			VALUES (?, 'sess_history', 'user', 'x', 'text', ?, ?)`, messageID, index+1, at); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_runs (id, session_id, user_message_id, agent_id, trace_id, status,
				model_provider, model_name, created_at, started_at, finished_at, state_version, state_updated_at, outcome_reason, assistant_content_sha256)
			VALUES (?, 'sess_history', ?, 'general_assistant', 'trace', 'completed', 'ollama', 'qwen2.5:7b', ?, ?, ?, 1, ?, 'completed_no_content', ?)`,
			fmt.Sprintf("run_history_%05d", index), messageID, at, at, at, at, emptyAssistantContentSHA256); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// History the reads must not grow with: completed deletion receipts, kept
	// forever, and consents whose task stopped before anyone answered, which
	// keep their pending mark.
	tx, err = repo.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index := range 2000 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO session_deletions (session_id, lifecycle_version, state,
				quiesce_deadline_at, terminal_sequence, retryable) VALUES (?, 1, 'completed', ?, 1, 0)`,
			fmt.Sprintf("sess_deleted_%04d", index), FormatTimestamp(base)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	seedAttentionRun(t, ctx, repo, "sess_history", "run_parent", "completed", base)
	for index := range 200 {
		seedAttentionDelegation(t, ctx, repo, fmt.Sprintf("dlg_stale_%03d", index), "sess_history", "run_parent", "pending", "cancelled", base)
	}
	future := FormatTimestamp(time.Now().Add(time.Hour))
	for index := range 100 {
		runID := fmt.Sprintf("run_waiting_%03d", index)
		seedAttentionRun(t, ctx, repo, "sess_history", runID, "waiting_approval", base)
		seedAttentionApproval(t, ctx, repo, fmt.Sprintf("appr_%03d", index), runID, base.Add(time.Duration(index)*time.Minute), future)
	}
	if got := p95(t, 20, func() error {
		page, err := repo.ListAttention(ctx, AttentionQuery{Limit: 60})
		if err != nil || !page.More {
			return fmt.Errorf("first page = %d rows, more %v, %v", len(page.Rows), page.More, err)
		}
		last := page.Rows[len(page.Rows)-1]
		cursor := last.Cursor()
		next, err := repo.ListAttention(ctx, AttentionQuery{Limit: 60, After: &cursor, Watermark: page.Watermark})
		if err != nil || next.More || len(next.Rows) != 40 {
			return fmt.Errorf("second page = %d rows, more %v, %v", len(next.Rows), next.More, err)
		}
		return nil
	}); got > 2*attentionPageBudget {
		t.Fatalf("two-page pass p95 = %s, want under %s per page", got, attentionPageBudget)
	}
}

// Stage 3 is the one stage with two kinds, so within a stage the keyset
// compares since before kind: a kind sorting after the cursor's reads
// since <= the cursor's, and one sorting before it reads since < it. No
// watch source exists until PRO-001, so a synthetic one stands in for it.
func TestKeysetComparesSinceBeforeKindWithinAStage(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	at := time.Now().UTC().Add(-time.Hour)
	for _, offset := range []time.Duration{time.Minute, 0, -time.Minute} {
		seedFailedAutomation(t, ctx, repo, fmt.Sprintf("At %s", offset), "failed", "provider_unavailable", at.Add(offset))
	}
	watches := attentionSource{kind: attention.KindWatch, read: func(int64) (string, []any) {
		row := func(id string, since time.Time) string {
			return fmt.Sprintf(`SELECT '%s' AS source_id, %d AS since_ns, 1 AS qualifies, '' AS session_id, '' AS session_title,
				'' AS approval_id, '' AS run_id, '' AS delegation_id, '' AS automation_id, '' AS automation_name,
				'' AS occurrence_key, '' AS cause`, id, since.UnixNano())
		}
		return row("watch_newer", at.Add(time.Minute)) + ` UNION ALL ` + row("watch_same", at) + ` UNION ALL ` +
			row("watch_older", at.Add(-time.Minute)), nil
	}}
	sinceOffsets := func(source attentionSource, after AttentionCursor) []string {
		t.Helper()
		statement, args, read := source.statement(AttentionQuery{After: &after, Watermark: time.Now().UnixNano(), Limit: 10}, time.Now().UnixNano())
		if !read {
			return nil
		}
		tx, err := repo.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback() }()
		rows, err := readAttentionRows(ctx, tx, source.kind, statement, args)
		if err != nil {
			t.Fatal(err)
		}
		var offsets []string
		for _, row := range rows {
			offsets = append(offsets, fmt.Sprint(row.SinceNanos-at.UnixNano()))
		}
		return offsets
	}
	automations := attentionSources[2]
	if automations.kind != attention.KindAutomation {
		t.Fatalf("source 2 is kind %d, want automation", automations.kind)
	}
	stage3 := attention.Rank(attention.PersonBlocked)
	// A cursor on an automation: a watch at the same since sorts after it.
	onAutomation := AttentionCursor{Rank: stage3, SinceNanos: at.UnixNano(), Kind: attention.KindAutomation, ID: "auto_zzz"}
	if got, want := sinceOffsets(watches, onAutomation), []string{"0", fmt.Sprint(-time.Minute.Nanoseconds())}; !slices.Equal(got, want) {
		t.Fatalf("watches after an automation cursor = %v, want the same since and older", got)
	}
	// A cursor on a watch: an automation at the same since sorted before it.
	onWatch := AttentionCursor{Rank: stage3, SinceNanos: at.UnixNano(), Kind: attention.KindWatch, ID: "watch_same"}
	if got, want := sinceOffsets(automations, onWatch), []string{fmt.Sprint(-time.Minute.Nanoseconds())}; !slices.Equal(got, want) {
		t.Fatalf("automations after a watch cursor = %v, want only the older one", got)
	}
	// An earlier stage reads nothing after a stage 3 cursor; a later one reads
	// every row whatever its since.
	if _, _, read := attentionSources[0].statement(AttentionQuery{After: &onWatch, Limit: 10}, 0); read {
		t.Fatal("the approval source read after a stage 3 cursor")
	}
	onApproval := AttentionCursor{Rank: 1, SinceNanos: 0, Kind: attention.KindApproval, ID: "appr_a"}
	if got := sinceOffsets(automations, onApproval); len(got) != 3 {
		t.Fatalf("automations after a stage 1 cursor = %v, want all three", got)
	}
}
