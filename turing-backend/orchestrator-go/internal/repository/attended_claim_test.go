package repository

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// attendedTestDefaults fires automations on the model the local test worker
// serves, so one claim chooses between an automation run and a chat turn.
var attendedTestDefaults = AutomationRunDefaults{AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2"}

// claimTransactionBudget bounds one claim's p95 against a large backlog.
const claimTransactionBudget = 100 * time.Millisecond

// fireAutomation creates an automation and fires it once: a scheduled,
// unattended run queued in the automation's own conversation.
func fireAutomation(t *testing.T, ctx context.Context, repo *Repository, name string) AutomationFire {
	t.Helper()
	automation := mustCreateDueAutomation(t, repo, ctx, name, everyFiveMinutes())
	fire, found, err := repo.ClaimDueAutomation(ctx, mustParse(t, automation.NextDueAt), attendedTestDefaults)
	if err != nil || !found || fire.RunID == "" {
		t.Fatalf("fire %s = %+v, found %v, %v; want a queued run", name, fire, found, err)
	}
	return fire
}

// claimNext claims the next job a local worker can take under a global limit
// of limit running runs (0 is none), or returns a zero Job.
func claimNext(t *testing.T, ctx context.Context, repo *Repository, limit int) Job {
	t.Helper()
	claimed, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "worker-1", limit, time.Hour,
		localWorkerAt(0), func(RoutingRequirements) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	return claimed
}

func completeClaimed(t *testing.T, repo *Repository, claimed Job) {
	t.Helper()
	if _, err := completeRunAtCurrentVersion(t, repo, claimed.RunID, claimed.AssistantMessageID, "Done.", nil); err != nil {
		t.Fatal(err)
	}
}

// wantQueuedBefore asserts the earlier run's job was created first, so the
// order a test asserts is not first-in-first-out by accident.
func wantQueuedBefore(t *testing.T, ctx context.Context, repo *Repository, earlier, later string) {
	t.Helper()
	var earlierNanos, laterNanos int64
	for runID, nanos := range map[string]*int64{earlier: &earlierNanos, later: &laterNanos} {
		if err := repo.db.QueryRowContext(ctx, `SELECT created_at_ns FROM jobs WHERE run_id = ?`, runID).Scan(nanos); err != nil {
			t.Fatal(err)
		}
	}
	if earlierNanos >= laterNanos {
		t.Fatalf("job of %s created at %d, want before the job of %s at %d", earlier, earlierNanos, later, laterNanos)
	}
}

// wantUnattended asserts that the flag written onto runID's job and the one
// definition agree, and both say want.
func wantUnattended(t *testing.T, ctx context.Context, repo *Repository, runID string, want bool) {
	t.Helper()
	var flag bool
	if err := repo.db.QueryRowContext(ctx, `SELECT unattended FROM jobs WHERE run_id = ?`, runID).Scan(&flag); err != nil {
		t.Fatalf("read the flag of %s: %v", runID, err)
	}
	unattended, err := repo.RunUnattended(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if flag != unattended || flag != want {
		t.Fatalf("run %s: jobs.unattended = %v, RunUnattended = %v; want both %v", runID, flag, unattended, want)
	}
}

// percentile is the nearest-rank percentile of samples.
func percentile(samples []time.Duration, percent int) time.Duration {
	sorted := slices.Clone(samples)
	slices.Sort(sorted)
	return sorted[(len(sorted)*percent+99)/100-1]
}

// seedUnattendedBacklog queues count scheduled-automation runs as count fires
// would leave them, each in its own conversation and oldest first
// (run_backlog_00001), every job written unattended beside its
// automation_runs row.
func seedUnattendedBacklog(t *testing.T, ctx context.Context, repo *Repository, count int) {
	t.Helper()
	base := time.Now().UTC().Add(-time.Hour)
	const prefix = `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?),
		seed(at, empty_sha, base_ns) AS (SELECT ?, ?, ?) `
	tx, err := repo.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range []string{
		`INSERT INTO sessions (id, title, title_origin, created_at, updated_at)
			SELECT printf('sess_backlog_%05d', i), 'Backlog', 'explicit', at, at FROM n, seed`,
		`INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
			SELECT printf('msg_backlog_ask_%05d', i), printf('sess_backlog_%05d', i), 'user',
				'Summarise the sandbox.', 'text', 1, at FROM n, seed`,
		`INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at)
			SELECT printf('msg_backlog_reply_%05d', i), printf('sess_backlog_%05d', i), printf('run_backlog_%05d', i),
				'assistant', '', 'text', 2, at FROM n, seed`,
		`INSERT INTO agent_runs (id, session_id, user_message_id, assistant_message_id, agent_id, trace_id, status,
				model_provider, model_name, created_at, state_version, state_updated_at, outcome_reason,
				assistant_content_sha256, queued_since_ns)
			SELECT printf('run_backlog_%05d', i), printf('sess_backlog_%05d', i), printf('msg_backlog_ask_%05d', i),
				printf('msg_backlog_reply_%05d', i), 'general_assistant', printf('trace_backlog_%05d', i), 'queued',
				'ollama', 'llama3.2', at, 1, at, 'none', empty_sha, base_ns FROM n, seed`,
		`INSERT INTO jobs (id, run_id, agent_id, status, payload_json, created_at, created_at_ns, unattended)
			SELECT printf('job_backlog_%05d', i), printf('run_backlog_%05d', i), 'general_assistant', 'pending',
				'{"userText":"Summarise the sandbox."}', at, base_ns + i, 1 FROM n, seed`,
		`INSERT INTO automation_runs (run_id, automation_id, automation_name, allowed_tools_json, fired_at)
			SELECT printf('run_backlog_%05d', i), 'auto_backlog', 'Backlog', '[]', at FROM n, seed`,
	} {
		if _, err := tx.ExecContext(ctx, prefix+statement,
			count, FormatTimestamp(base), emptyAssistantContentSHA256, base.UnixNano()); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// TUR-027: an owner's turn is never queued behind work nobody asked for. An
// automation run queued before a chat turn is claimed after it, and is still
// claimed once nothing attended waits.
func TestAChatTurnIsClaimedBeforeAnOlderAutomationRun(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	fire := fireAutomation(t, ctx, repo, "Digest")
	chat := enqueueLocalTurn(t, ctx, repo, "Owner")
	wantQueuedBefore(t, ctx, repo, fire.RunID, chat.RunID)

	if got := claimNext(t, ctx, repo, 1); got.RunID != chat.RunID {
		t.Fatalf("first claim = %q, want the owner's turn %q ahead of the older automation run %q", got.RunID, chat.RunID, fire.RunID)
	}
	if got := claimNext(t, ctx, repo, 1); got.RunID != "" {
		t.Fatalf("claim while the owner's turn runs = %q, want nothing: concurrency is unchanged", got.RunID)
	}
	if _, err := completeRunAtCurrentVersion(t, repo, chat.RunID, chat.AssistantMessageID, "Answered.", nil); err != nil {
		t.Fatal(err)
	}
	if got := claimNext(t, ctx, repo, 1); got.RunID != fire.RunID {
		t.Fatalf("claim with nothing attended waiting = %q, want the automation run %q", got.RunID, fire.RunID)
	}
}

// Attended work goes first between conversations, never within one: an
// owner's turn sent into an automation's conversation behind its queued run
// still waits for that run, which is claimed although an attended job is
// pending.
func TestAConversationsTurnOrderStillHoldsForAnAttendedTurn(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	fire := fireAutomation(t, ctx, repo, "Digest")
	reply, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: fire.SessionID, Content: "and the inbox too", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}

	running := claimNext(t, ctx, repo, 0)
	if running.RunID != fire.RunID {
		t.Fatalf("first claim = %q, want the automation run %q, earlier in the conversation than %q", running.RunID, fire.RunID, reply.RunID)
	}
	if got := claimNext(t, ctx, repo, 0); got.RunID != "" {
		t.Fatalf("claim while the conversation's earlier run is going = %q, want nothing", got.RunID)
	}
	completeClaimed(t, repo, running)
	if got := claimNext(t, ctx, repo, 0); got.RunID != reply.RunID {
		t.Fatalf("claim after the automation run = %q, want the owner's turn %q", got.RunID, reply.RunID)
	}
}

// Delegated tasks and their continuation are attended, so at concurrency one
// they keep their order (the children by creation, then the continuation),
// all of it ahead of an automation run queued before the children were.
func TestDelegatedWorkKeepsItsOrderAheadOfOlderUnattendedWork(t *testing.T) {
	f := newDelegationFixture(t)
	fire := fireAutomation(t, f.ctx, f.repo, "Digest")
	first, second := f.delegate(t, "call_1"), f.delegate(t, "call_2")
	wantQueuedBefore(t, f.ctx, f.repo, fire.RunID, first.ChildRunID)
	f.complete(t, f.parent.RunID, "Asked Research twice.")

	claim := func() string {
		t.Helper()
		claimed, err := f.repo.ClaimNextCompatibleJobWithLimit(f.ctx, "general_assistant", "team-worker", 1, time.Hour,
			joinWorker(1), func(RoutingRequirements) bool { return true })
		if err != nil {
			t.Fatal(err)
		}
		return claimed.RunID
	}
	for _, child := range []Delegation{first, second} {
		if got := claim(); got != child.ChildRunID {
			t.Fatalf("claim = %q, want the child %q, in creation order, ahead of the automation run %q", got, child.ChildRunID, fire.RunID)
		}
		f.complete(t, child.ChildRunID, "Notes.")
	}
	continuation := f.continuationRunID(t)
	if got := claim(); continuation == "" || got != continuation {
		t.Fatalf("claim after the children = %q, want the continuation %q ahead of the automation run", got, continuation)
	}
	f.complete(t, continuation, "Here is what Research found.")
	if got := claim(); got != fire.RunID {
		t.Fatalf("claim after the team's work = %q, want the automation run %q", got, fire.RunID)
	}
}

// The flag is written once, at insert, by whoever creates the run, and on
// every enqueue path it is what the one definition says: an owner's turn
// (in an automation's conversation too), a delegated task and its
// continuation are attended; a scheduled automation fire is not.
func TestEveryEnqueuePathWritesTheFlagTheDefinitionGives(t *testing.T) {
	f := newDelegationFixture(t)
	wantUnattended(t, f.ctx, f.repo, f.parent.RunID, false)
	child := f.delegate(t, "call_1")
	wantUnattended(t, f.ctx, f.repo, child.ChildRunID, false)
	f.complete(t, f.parent.RunID, "Asked Research.")
	f.startChild(t, child)
	f.complete(t, child.ChildRunID, "Notes.")
	wantUnattended(t, f.ctx, f.repo, f.continuationRunID(t), false)

	fire := fireAutomation(t, f.ctx, f.repo, "Digest")
	wantUnattended(t, f.ctx, f.repo, fire.RunID, true)
	reply, err := f.repo.EnqueueUserMessage(f.ctx, EnqueueUserMessageInput{
		SessionID: fire.SessionID, Content: "and the inbox too", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	wantUnattended(t, f.ctx, f.repo, reply.RunID, false)
}

// An unattended run is queued only in the transaction that writes its
// automation_runs row, which a send never does, so a send cannot mark itself
// unattended.
func TestASendCannotQueueAnUnattendedRun(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Owner")
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2", Unattended: true,
	})
	if !errors.Is(err, errUnattendedSend) {
		t.Fatalf("unattended send = %v, want errUnattendedSend", err)
	}
	var jobs int
	if err := repo.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("jobs = %d, %v; want none queued", jobs, err)
	}
}

// The routing page decides routing waits and claims nothing, so it keeps
// listing pending work in creation order, attended or not, and its cursor
// pages through it as before.
func TestTheRoutingPageKeepsCreationOrderAcrossAttendedAndUnattendedWork(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	older := enqueueLocalTurn(t, ctx, repo, "First")
	fire := fireAutomation(t, ctx, repo, "Digest")
	newer := enqueueLocalTurn(t, ctx, repo, "Third")

	var got []string
	cursor := PendingRoutingCursor{}
	for range 3 {
		page, next, err := repo.ListPendingRoutingWorkPage(ctx, cursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page {
			got = append(got, item.RunID)
		}
		cursor = next
	}
	if want := []string{older.RunID, fire.RunID, newer.RunID}; !slices.Equal(got, want) {
		t.Fatalf("paged runs = %v, want creation order %v", got, want)
	}
}

// Each probe walks one index in claim order, whatever routing filter the
// worker brings: the attended probe the partial index of attended pending
// jobs, the other the claim index it always used. Neither sorts.
func TestTheClaimProbesWalkTheirIndexesInOrder(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	everywhere := localWorkerAt(1)
	everywhere.Models = append(everywhere.Models, RoutingModelCapability{Provider: "ollama", Model: "qwen2.5:7b", MaxContextTokens: 32768})
	everywhere.ExternalAgentCredentialRefs = []string{"cred-a", "cred-b"}
	everywhere.RemoteEgressDecisionVersion = RunEgressDecisionVersion
	for name, worker := range map[string]*WorkerRoutingCapabilities{
		"no routing filter":  nil,
		"local worker":       localWorkerAt(0),
		"tool worker":        egressWorker(),
		"multi-route worker": everywhere,
	} {
		filter, filterArgs := claimRoutingFilterSQL(worker)
		args := append([]any{"general_assistant"}, filterArgs...)
		for _, probe := range []struct {
			attendedOnly bool
			search       string
		}{
			{attendedOnly: true, search: "SEARCH j USING INDEX idx_jobs_attended_claim (agent_id=? AND status=?)"},
			{attendedOnly: false, search: "SEARCH j USING INDEX idx_jobs_capability_claim (agent_id=? AND status=?)"},
		} {
			plan := explainQueryPlan(t, ctx, repo, claimProbeQuery(probe.attendedOnly, filter), args...)
			if !strings.HasPrefix(plan, probe.search) || strings.Contains(plan, "TEMP B-TREE") {
				t.Fatalf("%s, attended only %v: plan\n%s\nwant its outer loop to be %q, with no sort", name, probe.attendedOnly, plan, probe.search)
			}
		}
	}
}

// With 10,000 unattended jobs pending and nothing attended, the attended
// probe visits no row, and a claim still takes the oldest unattended job.
// The claim's transaction time against that backlog is measured.
func TestTheAttendedProbeVisitsNoRowBehindAnUnattendedBacklog(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	const backlog = 10_000
	seedUnattendedBacklog(t, ctx, repo, backlog)
	filter, filterArgs := claimRoutingFilterSQL(localWorkerAt(0))
	args := append([]any{"general_assistant"}, filterArgs...)

	// No per-statement row counter reaches Go through this driver, so the
	// bound is shown from the plan and the index: the probe's outer loop is a
	// search of the partial index, which holds attended pending jobs alone
	// (TestAttendedClaimMigrationAddsThePartialIndex pins its predicate), and
	// with this backlog queued it holds none, so the probe visits no row.
	plan := explainQueryPlan(t, ctx, repo, claimProbeQuery(true, filter), args...)
	if !strings.HasPrefix(plan, "SEARCH j USING INDEX idx_jobs_attended_claim (agent_id=? AND status=?)") {
		t.Fatalf("attended probe plan\n%s\nwant its outer loop on idx_jobs_attended_claim", plan)
	}
	var indexed int
	if err := repo.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM jobs INDEXED BY idx_jobs_attended_claim
		WHERE agent_id = 'general_assistant' AND status = 'pending' AND unattended = 0`).Scan(&indexed); err != nil {
		t.Fatal(err)
	}
	if indexed != 0 {
		t.Fatalf("attended probe index entries = %d, want none for it to visit", indexed)
	}

	samples := make([]time.Duration, 0, 50)
	for index := range 51 {
		started := time.Now()
		claimed := claimNext(t, ctx, repo, 1)
		elapsed := time.Since(started)
		if want := fmt.Sprintf("run_backlog_%05d", index+1); claimed.RunID != want {
			t.Fatalf("claim %d = %q, want the oldest unattended run %q", index, claimed.RunID, want)
		}
		if index > 0 {
			samples = append(samples, elapsed)
		}
		completeClaimed(t, repo, claimed)
	}
	p50, p95 := percentile(samples, 50), percentile(samples, 95)
	t.Logf("claim transaction against %d queued unattended jobs: p50 %s, p95 %s", backlog, p50, p95)
	if p95 > claimTransactionBudget {
		t.Fatalf("claim p95 = %s, want under %s", p95, claimTransactionBudget)
	}
}

// The expected effect, measured in a mixed fixture: owner turns arriving
// while an unattended backlog drains at concurrency one each wait behind at
// most the run already going, where first-in-first-out queued them behind
// every older unattended job. The delay measured runs from the slot freeing
// to the turn's claim.
func TestOwnerTurnsWaitBehindAtMostTheRunningJob(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	const backlog = 10_000
	seedUnattendedBacklog(t, ctx, repo, backlog)

	delays := make([]time.Duration, 0, 50)
	fewestOlder, mostOlder := backlog, 0
	for index := range 51 {
		running := claimNext(t, ctx, repo, 1)
		if !strings.HasPrefix(running.RunID, "run_backlog_") {
			t.Fatalf("claim %d = %q, want an unattended run while nothing attended waits", index, running.RunID)
		}
		turn := enqueueLocalTurn(t, ctx, repo, fmt.Sprintf("Owner %02d", index))
		var older int
		if err := repo.db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM jobs
			WHERE status = 'pending' AND unattended = 1
				AND created_at_ns < (SELECT created_at_ns FROM jobs WHERE run_id = ?)`, turn.RunID).Scan(&older); err != nil {
			t.Fatal(err)
		}
		fewestOlder, mostOlder = min(fewestOlder, older), max(mostOlder, older)

		completeClaimed(t, repo, running)
		freed := time.Now()
		claimed := claimNext(t, ctx, repo, 1)
		delay := time.Since(freed)
		if claimed.RunID != turn.RunID {
			t.Fatalf("claim after the running job = %q, want the owner's turn %q", claimed.RunID, turn.RunID)
		}
		if index > 0 {
			delays = append(delays, delay)
		}
		completeClaimed(t, repo, claimed)
	}
	p50, p95 := percentile(delays, 50), percentile(delays, 95)
	t.Logf("attended claim delay after the running job ends: p50 %s, p95 %s; first-in-first-out would have queued each turn behind %d to %d older unattended jobs",
		p50, p95, fewestOlder, mostOlder)
	if p95 > claimTransactionBudget {
		t.Fatalf("attended claim delay p95 = %s, want under %s", p95, claimTransactionBudget)
	}
}
