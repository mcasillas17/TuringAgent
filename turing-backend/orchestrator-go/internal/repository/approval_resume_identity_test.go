package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
)

// ---------------------------------------------------------------------------
// Which approval resumed a run.
//
// The row a resume produces says "running, owned by this worker and attempt".
// It does not say which authorization moved it, and two approvals on one run
// are ordinary — a model asks for two tools, a user answers one. Without the
// approval in the durable record, the second Ready arriving at the version the
// first already left is indistinguishable from the first one repeating, and the
// shared write-free duplicate rule answers it with an acceptance: the run is
// told to act on an authorization nobody ever resumed it for.
//
// So the approval is committed into the transition's own event, and the
// duplicate rule reads it back from there. That is what makes the identity
// durable rather than a fact about a process that happens to still be running:
// the reopened-database test at the bottom is the whole point.
// ---------------------------------------------------------------------------

// approvalResumeFixture leaves a claimed run waiting on one approved approval,
// with the attempt its worker actually holds.
type approvalResumeFixture struct {
	repo     *Repository
	runID    string
	workerID string
	attempt  string
	waiting  RunState
}

func newApprovalResumeFixture(t *testing.T, repo *Repository, worker string) *approvalResumeFixture {
	t.Helper()
	enqueued, _ := approvalPairFixture(t, repo, worker)
	fixture := &approvalResumeFixture{repo: repo, runID: enqueued.RunID, workerID: worker}
	fixture.attempt = approvalPairAttemptID(t, repo, enqueued.RunID)
	return fixture
}

// approve records one approval on this run and answers it, which is exactly as
// far as a decision goes: the row is still waiting for the worker to prove it
// restored the paused attempt.
func (f *approvalResumeFixture) approve(t *testing.T, toolCallID string, path string) string {
	t.Helper()
	approvalID := f.request(t, toolCallID, path)
	if _, err := f.repo.ApproveApproval(context.Background(), approvalID, "token-"+approvalID, sql.NullString{}, now()); err != nil {
		t.Fatalf("ApproveApproval: %v", err)
	}
	f.readWaiting(t)
	return approvalID
}

// request records one approval on this run and leaves it undecided.
func (f *approvalResumeFixture) request(t *testing.T, toolCallID string, path string) string {
	t.Helper()
	ctx := context.Background()
	if err := f.repo.RecordToolCallBefore(ctx, ToolCallRecord{
		ToolCallID: toolCallID, RunID: f.runID, ModelToolCallID: "model_" + toolCallID,
	}, "general_assistant", "files", "files.update", `{"path":"`+path+`"}`, "sha256:"+toolCallID); err != nil {
		t.Fatalf("RecordToolCallBefore: %v", err)
	}
	approval, _, err := f.repo.CreateApprovalWithEvent(ctx, f.runID, toolCallID,
		"general_assistant", "files.update", `{"path":"`+path+`"}`, "sha256:"+toolCallID, "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("CreateApprovalWithEvent: %v", err)
	}
	f.readWaiting(t)
	return approval.ApprovalID
}

func (f *approvalResumeFixture) readWaiting(t *testing.T) {
	t.Helper()
	state, err := f.repo.GetRunState(context.Background(), f.runID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	if state.Lifecycle != lifecycleWaitingApproval {
		t.Fatalf("fixture lifecycle = %q, want waiting_approval", state.Lifecycle)
	}
	f.waiting = state
}

// resume replays the exact Ready a worker sends: this run, this approval, the
// worker and attempt the row records, and the waiting version.
func (f *approvalResumeFixture) resume(approvalID string) (RunTransitionResult, error) {
	return f.resumeWith(f.repo, approvalID, f.waiting.StateVersion)
}

func (f *approvalResumeFixture) resumeWith(repo *Repository, approvalID string, version int64) (RunTransitionResult, error) {
	return repo.ResumeApprovedRun(context.Background(), ResumeApprovedRunInput{
		RunID:                f.runID,
		ApprovalID:           approvalID,
		WorkerID:             f.workerID,
		AssignmentAttemptID:  f.attempt,
		ExpectedStateVersion: version,
	})
}

func (f *approvalResumeFixture) state(t *testing.T) RunState {
	t.Helper()
	state, err := f.repo.GetRunState(context.Background(), f.runID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	return state
}

// assertResumeRefused pins the whole shape of a refusal: the conflict sentinel,
// no committed state handed back, and a run that did not move by a version or
// an event.
func (f *approvalResumeFixture) assertResumeRefused(t *testing.T, approvalID string) {
	t.Helper()
	f.assertResumeRefusedAt(t, approvalID, f.waiting.StateVersion)
}

func (f *approvalResumeFixture) assertResumeRefusedAt(t *testing.T, approvalID string, version int64) {
	t.Helper()
	before := f.state(t)
	events := countRunEvents(t, f.repo, f.runID)

	result, err := f.resumeWith(f.repo, approvalID, version)

	if !errors.Is(err, ErrRunTransitionConflict) {
		t.Fatalf("refused resume error = %v, want %v", err, ErrRunTransitionConflict)
	}
	// A refusal names the condition and nothing else: an identifier in an error
	// string is row content leaving the row, and these travel out to callers
	// that must not learn which approvals or runs exist.
	for _, secret := range []string{approvalID, f.runID} {
		if secret != "" && strings.Contains(err.Error(), secret) {
			t.Fatalf("conflict error %q leaked the identifier %q", err.Error(), secret)
		}
	}
	if result.State != (RunState{}) || result.Duplicate || len(result.Events) != 0 {
		t.Fatalf("refused resume returned %+v, want nothing", result)
	}
	if after := f.state(t); after != before {
		t.Fatalf("refused resume changed the run: %+v, want %+v", after, before)
	}
	if after := countRunEvents(t, f.repo, f.runID); after != events {
		t.Fatalf("refused resume appended %d events", after-events)
	}
}

// resumeEventPayload reads back the durable payload of the state-changed event
// a resume committed, from the log rather than from the writer's return value.
func resumeEventPayload(t *testing.T, repo *Repository, runID string, version int64) map[string]any {
	t.Helper()
	var payloadJSON string
	err := repo.db.QueryRowContext(context.Background(), `
		SELECT payload_json FROM events
		WHERE run_id = ? AND type = ?
			AND json_extract(payload_json, '$.runState.stateVersion') = ?
	`, runID, runStateChangedEventType, version).Scan(&payloadJSON)
	if err != nil {
		t.Fatalf("read the %s event at version %d: %v", runStateChangedEventType, version, err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("decode resume payload %q: %v", payloadJSON, err)
	}
	return payload
}

// TestApprovalResumeCommitsWhichApprovalMovedTheRun pins the durable record the
// duplicate rule is going to read. The approval is published because a client
// watching this run has to know which of its outstanding authorizations was
// acted on; the worker and the attempt are not, because ownership is a row
// guard and no client's business.
func TestApprovalResumeCommitsWhichApprovalMovedTheRun(t *testing.T) {
	repo := New(openTestDB(t))
	fixture := newApprovalResumeFixture(t, repo, "worker-resume-identity")
	approvalID := fixture.approve(t, "call_resume_identity", "note.txt")

	resumed, err := fixture.resume(approvalID)
	if err != nil {
		t.Fatalf("ResumeApprovedRun: %v", err)
	}
	if resumed.Duplicate {
		t.Fatal("the first resume reported itself a duplicate")
	}

	if len(resumed.Events) != 1 || resumed.Events[0].Type != runStateChangedEventType {
		t.Fatalf("resume events = %+v, want exactly one %s", resumed.Events, runStateChangedEventType)
	}
	payload := resumeEventPayload(t, repo, fixture.runID, resumed.State.StateVersion)
	if payload["approvalId"] != approvalID {
		t.Fatalf("resume payload approvalId = %v, want %q: %#v", payload["approvalId"], approvalID, payload)
	}
	for key := range payload {
		if key == "approvalId" || key == "runState" {
			continue
		}
		t.Fatalf("resume payload published %q, which is neither the approval nor the run state: %#v", key, payload)
	}
	// Ownership stays a row guard. A worker ID or an attempt ID here would put
	// the orchestrator's internal dispatch bookkeeping in front of every client
	// that reads a lifecycle event.
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{fixture.workerID, fixture.attempt} {
		if secret == "" {
			t.Fatal("the fixture proves nothing with an empty identity")
		}
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("resume payload leaked the internal identity %q: %s", secret, encoded)
		}
	}
}

// TestIdenticalApprovalResumeReplaysWithoutWriting is the replay a worker that
// lost its acceptance depends on. It has to be answered with the same state,
// and it has to write nothing — a second event would tell every client the run
// resumed twice.
func TestIdenticalApprovalResumeReplaysWithoutWriting(t *testing.T) {
	repo := New(openTestDB(t))
	fixture := newApprovalResumeFixture(t, repo, "worker-resume-replay")
	approvalID := fixture.approve(t, "call_resume_replay", "note.txt")
	first, err := fixture.resume(approvalID)
	if err != nil {
		t.Fatalf("first ResumeApprovedRun: %v", err)
	}
	events := countRunEvents(t, repo, fixture.runID)

	second, err := fixture.resume(approvalID)

	if err != nil {
		t.Fatalf("replayed ResumeApprovedRun: %v", err)
	}
	if !second.Duplicate {
		t.Fatal("the replayed resume was not recognized as a duplicate")
	}
	if second.State != first.State {
		t.Fatalf("replayed state = %+v, want the committed %+v", second.State, first.State)
	}
	if len(second.Events) != 0 {
		t.Fatalf("the replayed resume appended %+v", second.Events)
	}
	if after := countRunEvents(t, repo, fixture.runID); after != events {
		t.Fatalf("the replayed resume appended %d events", after-events)
	}
}

// TestSecondApprovalResumeAfterCommitIsFenced is the violation this durable
// identity exists for. Both approvals belong to this run, both were approved,
// both name the worker and attempt the row records, and both compute the same
// waiting version — everything the shared duplicate rule compares is identical.
// The only difference is which authorization is being resumed, and answering
// the second one with an acceptance would let a run act on a tool call the
// orchestrator never resumed it for.
func TestSecondApprovalResumeAfterCommitIsFenced(t *testing.T) {
	repo := New(openTestDB(t))
	fixture := newApprovalResumeFixture(t, repo, "worker-resume-second")
	first := fixture.approve(t, "call_resume_first", "first.txt")
	waiting := fixture.waiting
	second := fixture.approve(t, "call_resume_second", "second.txt")
	// The second approval did not move the run, so a Ready for it computes the
	// same expected version the first one did.
	if fixture.waiting.StateVersion != waiting.StateVersion {
		t.Fatalf("the second request moved the run to version %d, want %d",
			fixture.waiting.StateVersion, waiting.StateVersion)
	}
	if _, err := fixture.resume(first); err != nil {
		t.Fatalf("first ResumeApprovedRun: %v", err)
	}

	fixture.assertResumeRefused(t, second)
}

// foreignApproval records an approved approval on a DIFFERENT run, which is
// what a resume must never be able to use as its authority. It is answered
// rather than left pending so the refusal below can only come from ownership.
func foreignApproval(t *testing.T, repo *Repository, title string) string {
	t.Helper()
	ctx := context.Background()
	other := enqueueRun(t, repo, title)
	if err := repo.MarkRunRunning(ctx, other.RunID); err != nil {
		t.Fatalf("MarkRunRunning: %v", err)
	}
	approval, err := repo.CreateApproval(ctx, other.RunID, "", "general_assistant",
		"files.update", `{}`, "sha256:foreign", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("CreateApproval: %v", err)
	}
	if _, err := repo.ApproveApproval(ctx, approval.ApprovalID, "token-foreign", sql.NullString{}, now()); err != nil {
		t.Fatalf("ApproveApproval: %v", err)
	}
	return approval.ApprovalID
}

// TestApprovalResumeRequiresAnApprovalThisRunOwns closes the gap between "an
// approval that authorizes something" and "an approval that authorizes
// something HERE".
//
// The foreign approval is genuinely approved, so nothing about its own status
// refuses it; what refuses it is that it belongs to another run. Ownership is
// established by scoping the lookup to this run, and a resume that could borrow
// somebody else's answer would restart a run on a decision made about a
// different tool call entirely.
func TestApprovalResumeRequiresAnApprovalThisRunOwns(t *testing.T) {
	repo := New(openTestDB(t))
	fixture := newApprovalResumeFixture(t, repo, "worker-resume-foreign")
	fixture.approve(t, "call_resume_owned", "owned.txt")

	for name, approvalID := range map[string]string{
		"an approval owned by another run": foreignApproval(t, repo, "Another run"),
		"an approval that never existed":   "appr_never_existed",
	} {
		t.Run(name, func(t *testing.T) {
			fixture.assertResumeRefused(t, approvalID)

			if state := fixture.state(t); state.Lifecycle != lifecycleWaitingApproval {
				t.Fatalf("lifecycle after %s = %q, want waiting_approval", name, state.Lifecycle)
			}
		})
	}
}

// TestApprovalOwnershipIsCheckedBeforeAWriteFreeReplay covers the window a
// replay opens.
//
// A duplicate is recognised from the row: the run is already exactly where the
// command wanted it, one version on. That is just as true for a command naming
// an approval this run never owned, so the row-level rule alone would answer it
// with this run's state and report a successful replay — the strongest possible
// "yes" to a command that had no authority at all. The refusal therefore has to
// outrank that rule, and here it is outranked twice: ownership is established
// before anything else can answer, and the durable trigger this transition
// commits does not name the approval being claimed either. The genuine repeat
// still costs nothing.
func TestApprovalOwnershipIsCheckedBeforeAWriteFreeReplay(t *testing.T) {
	repo := New(openTestDB(t))
	fixture := newApprovalResumeFixture(t, repo, "worker-resume-replay-identity")
	approvalID := fixture.approve(t, "call_resume_replay_identity", "owned.txt")
	waiting := fixture.waiting
	committed, err := fixture.resume(approvalID)
	if err != nil {
		t.Fatalf("ResumeApprovedRun: %v", err)
	}

	for name, foreign := range map[string]string{
		"an approval owned by another run": foreignApproval(t, repo, "Another run entirely"),
		"an approval that never existed":   "appr_never_existed",
	} {
		t.Run(name, func(t *testing.T) {
			fixture.assertResumeRefusedAt(t, foreign, waiting.StateVersion)
		})
	}

	replayed, err := fixture.resumeWith(repo, approvalID, waiting.StateVersion)
	if err != nil {
		t.Fatalf("replayed ResumeApprovedRun: %v", err)
	}
	if !replayed.Duplicate || replayed.State != committed.State {
		t.Fatalf("replayed resume = %+v, want the committed %+v as a duplicate", replayed, committed.State)
	}
}

// TestApprovalResumeRequiresAnAuthorizedApproval covers the other half of the
// same question: not just which approval, but whether it authorizes anything.
//
// A pending approval is a question nobody has answered, and a denied or expired
// one is an answer of no. Resuming a run on any of them would restart it to
// make a call the user never permitted.
func TestApprovalResumeRequiresAnAuthorizedApproval(t *testing.T) {
	t.Run("pending", func(t *testing.T) {
		repo := New(openTestDB(t))
		fixture := newApprovalResumeFixture(t, repo, "worker-resume-pending")
		pending := fixture.request(t, "call_resume_pending", "pending.txt")

		fixture.assertResumeRefused(t, pending)

		if state := fixture.state(t); state.Lifecycle != lifecycleWaitingApproval {
			t.Fatalf("lifecycle after an undecided resume = %q, want waiting_approval", state.Lifecycle)
		}
	})

	// Denied and expired are written straight onto the approval row here, with
	// the run left waiting. Going through DenyApproval or ExpireApproval would
	// terminalize the run in the same transaction, and a terminal run is
	// already refused by the lifecycle guard — which would prove the lifecycle
	// check works and say nothing about this one. The pair is not invented: the
	// recovery writers rewrite approval statuses in bulk for a whole run, so a
	// refused authorization sitting under a live run is a shape the log can
	// hold, and the resume must refuse it on the authorization alone.
	for _, refused := range []string{"denied", "expired"} {
		t.Run(refused, func(t *testing.T) {
			repo := New(openTestDB(t))
			fixture := newApprovalResumeFixture(t, repo, "worker-resume-"+refused)
			approvalID := fixture.request(t, "call_resume_"+refused, refused+".txt")
			if _, err := repo.db.ExecContext(context.Background(),
				`UPDATE approvals SET status = ?, decided_at = ? WHERE id = ?`,
				refused, now(), approvalID); err != nil {
				t.Fatalf("record the %s approval: %v", refused, err)
			}
			fixture.readWaiting(t)

			fixture.assertResumeRefused(t, approvalID)

			if state := fixture.state(t); state.Lifecycle != lifecycleWaitingApproval {
				t.Fatalf("lifecycle after a %s resume = %q, want waiting_approval", refused, state.Lifecycle)
			}
		})
	}

	t.Run("consumed replays", func(t *testing.T) {
		repo := New(openTestDB(t))
		fixture := newApprovalResumeFixture(t, repo, "worker-resume-consumed")
		approvalID := fixture.approve(t, "call_resume_consumed", "consumed.txt")
		first, err := fixture.resume(approvalID)
		if err != nil {
			t.Fatalf("ResumeApprovedRun: %v", err)
		}
		// The approved call ran and spent its token. The authorization is used
		// up, not withdrawn, so a worker that lost the acceptance still gets
		// the same answer rather than a fence.
		if _, err := repo.ConsumeApproval(context.Background(), approvalID, now()); err != nil {
			t.Fatalf("ConsumeApproval: %v", err)
		}

		replayed, err := fixture.resume(approvalID)

		if err != nil {
			t.Fatalf("replayed ResumeApprovedRun after consumption: %v", err)
		}
		if !replayed.Duplicate || replayed.State != first.State {
			t.Fatalf("replayed resume = %+v, want the committed %+v as a duplicate", replayed, first.State)
		}
	})
}

// TestApprovalResumeIdentitySurvivesProcessRestart is what makes this identity
// durable rather than a fact about the process that committed it. Everything in
// memory is gone: the database is closed and reopened, and a brand new
// repository answers the same two questions from the log alone.
func TestApprovalResumeIdentitySurvivesProcessRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turing.db")
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	repo := New(database)
	fixture := newApprovalResumeFixture(t, repo, "worker-resume-restart")
	first := fixture.approve(t, "call_restart_first", "first.txt")
	second := fixture.approve(t, "call_restart_second", "second.txt")
	committed, err := fixture.resume(first)
	if err != nil {
		t.Fatalf("ResumeApprovedRun: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	restarted, err := db.Open(path)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	fixture.repo = New(restarted)

	replayed, err := fixture.resume(first)
	if err != nil {
		t.Fatalf("replayed ResumeApprovedRun after restart: %v", err)
	}
	if !replayed.Duplicate || replayed.State != committed.State {
		t.Fatalf("replayed resume after restart = %+v, want the committed %+v as a duplicate", replayed, committed.State)
	}
	// And the fence survives with it: the process that could have remembered
	// which approval it resumed is gone, and the answer is the same.
	fixture.assertResumeRefused(t, second)
}

// TestSequentialApprovalResumesFenceTheEarlierReady covers a run that stops
// twice: one authorization resumes it, its call runs, and a second tool needs
// authorization of its own. Both resumes are legitimate and both must commit,
// and the earlier Ready must not be able to come back and claim the later one's
// version as its own replay.
func TestSequentialApprovalResumesFenceTheEarlierReady(t *testing.T) {
	repo := New(openTestDB(t))
	fixture := newApprovalResumeFixture(t, repo, "worker-resume-sequential")
	first := fixture.approve(t, "call_sequential_first", "first.txt")
	firstWaiting := fixture.waiting
	if _, err := fixture.resume(first); err != nil {
		t.Fatalf("first ResumeApprovedRun: %v", err)
	}

	// The resumed call finished and the next one needs its own authorization,
	// so the run stops again a version further on.
	second := fixture.approve(t, "call_sequential_second", "second.txt")
	if fixture.waiting.StateVersion <= firstWaiting.StateVersion {
		t.Fatalf("the second wait is at version %d, want past the first's %d",
			fixture.waiting.StateVersion, firstWaiting.StateVersion)
	}
	resumed, err := fixture.resume(second)
	if err != nil {
		t.Fatalf("second ResumeApprovedRun: %v", err)
	}

	replayed, err := fixture.resume(second)
	if err != nil {
		t.Fatalf("replayed second ResumeApprovedRun: %v", err)
	}
	if !replayed.Duplicate || replayed.State != resumed.State {
		t.Fatalf("replayed second resume = %+v, want the committed %+v as a duplicate", replayed, resumed.State)
	}
	// The first Ready, arriving late at the version it originally computed.
	fixture.assertResumeRefusedAt(t, first, firstWaiting.StateVersion)
}

// TestApprovalResumeWithoutADurableTriggerIsFenced pins the fail-closed edge:
// an event committed before this rule existed carries no approval, so nothing
// durable says which authorization put the run where it is. A resume replayed
// across that upgrade is refused rather than answered, and the ownership fence
// that follows sends the run to recovery — which is recoverable, where handing
// out an acceptance nobody can justify is not.
func TestApprovalResumeWithoutADurableTriggerIsFenced(t *testing.T) {
	repo := New(openTestDB(t))
	fixture := newApprovalResumeFixture(t, repo, "worker-resume-legacy")
	approvalID := fixture.approve(t, "call_resume_legacy", "legacy.txt")
	resumed, err := fixture.resume(approvalID)
	if err != nil {
		t.Fatalf("ResumeApprovedRun: %v", err)
	}
	// Exactly the shape the previous build wrote: the state projection, with no
	// record of the trigger beside it.
	if _, err := repo.db.ExecContext(context.Background(), `
		UPDATE events
		SET payload_json = json_remove(payload_json, ?)
		WHERE run_id = ? AND type = ?
			AND json_extract(payload_json, ?) = ?
	`, approvalIdentityPayloadPath, fixture.runID, runStateChangedEventType,
		runStateVersionPayloadPath, resumed.State.StateVersion); err != nil {
		t.Fatalf("rewrite the resume event as a legacy one: %v", err)
	}
	if payload := resumeEventPayload(t, repo, fixture.runID, resumed.State.StateVersion); payload["approvalId"] != nil {
		t.Fatalf("the rewritten event still carries an approval: %#v", payload)
	}

	fixture.assertResumeRefused(t, approvalID)
}
