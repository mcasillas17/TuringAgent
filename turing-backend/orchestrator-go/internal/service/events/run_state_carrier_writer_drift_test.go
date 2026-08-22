package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// payloadCarriesRunStateKey reports whether a durable event's raw stored
// bytes contain the runState key at all — the writer-side question this
// package's read-side carrier gate exists to answer for. It is deliberately a
// raw map-key check rather than a decode into the typed snapshot: a writer
// that started merging a malformed or partial runState value would still be
// a writer that started merging one, and this guard has to catch that too.
func payloadCarriesRunStateKey(t *testing.T, eventType string, payloadJSON string) bool {
	t.Helper()
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payloadJSON), &decoded); err != nil {
		t.Fatalf("decode %s payload %q: %v", eventType, payloadJSON, err)
	}
	_, present := decoded[runStatePayloadKey]
	return present
}

// driftGuardRun is one fresh queued-then-claimed run, driven far enough for
// every writer this test exercises to have somewhere valid to write to.
type driftGuardRun struct {
	enqueued repository.EnqueueUserMessageResult
	job      repository.Job
	workerID string
}

// startDriftGuardRun enqueues and claims a run through the exact real public
// entry points a production caller uses, so the run this test's writers act
// on is not a fixture built by reaching into repository internals.
func startDriftGuardRun(t *testing.T, repo *repository.Repository, title string) driftGuardRun {
	t.Helper()
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, title)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	workerID := "worker-" + title
	job, err := repo.ClaimNextJob(ctx, "general_assistant", workerID)
	if err != nil {
		t.Fatalf("ClaimNextJob: %v", err)
	}
	return driftGuardRun{enqueued: enqueued, job: job, workerID: workerID}
}

// requestDriftGuardApproval drives RecordToolCallBefore and
// CreateApprovalWithEvent for one more tool call on an already-claimed run,
// exactly as a worker asking permission for a tool call would.
func requestDriftGuardApproval(t *testing.T, repo *repository.Repository, run driftGuardRun, toolCallID string) (repository.ApprovalRecord, repository.Event) {
	t.Helper()
	ctx := context.Background()
	if err := repo.RecordToolCallBefore(ctx, repository.ToolCallRecord{
		ToolCallID: toolCallID, RunID: run.enqueued.RunID, ModelToolCallID: "model_" + toolCallID,
	}, "general_assistant", "files", "files.update", `{"path":"`+toolCallID+`.txt"}`, "sha256:"+toolCallID); err != nil {
		t.Fatalf("RecordToolCallBefore(%s): %v", toolCallID, err)
	}
	approval, event, err := repo.CreateApprovalWithEvent(ctx, run.enqueued.RunID, toolCallID,
		"general_assistant", "files.update", `{"path":"`+toolCallID+`.txt"}`, "sha256:"+toolCallID, "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("CreateApprovalWithEvent(%s): %v", toolCallID, err)
	}
	return approval, event
}

// startDriftGuardRunWithRoutingNotice enqueues a run through the same real
// EnqueueUserMessage entry point as startDriftGuardRun, but with an accepted
// remote-MCP egress decision — the one condition that makes EnqueueUserMessage
// itself append a real agent.run.step routing notice (appendRunNoticeTx) in
// the same transaction as the queued event. This is the only real writer of
// agent.run.step this test drives without reaching into a requeue/recovery
// path, so it is the minimal real repository write that gives the full-table
// scan below a genuine agent.run.step row to check, rather than the scan
// silently never seeing that canonical type at all.
func startDriftGuardRunWithRoutingNotice(t *testing.T, repo *repository.Repository, title string) repository.EnqueueUserMessageResult {
	t.Helper()
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, title)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	skillFingerprint, err := backendegress.SkillSnapshotFingerprint(nil)
	if err != nil {
		t.Fatalf("SkillSnapshotFingerprint: %v", err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "look up", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "local",
		SelectedTools: []string{"vendor/vendor.lookup"},
		EgressDecision: &repository.PendingEgressDecision{
			Version: 1, ChallengeNonce: "nonce_" + title, ChallengeFingerprint: "fingerprint_" + title,
			RequestDigest: "digest_" + title, Provider: "ollama", Model: "local",
			DataCategories: []string{
				"EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS",
				"EGRESS_DATA_CATEGORY_TOOL_RESULTS",
			},
			SelectedTools:            []string{"vendor/vendor.lookup"},
			SkillSnapshotFingerprint: skillFingerprint,
			ConsentGrantedAt:         "2026-08-21T00:00:00Z",
			RemoteMCPServers: []repository.RemoteMCPServerEgress{{
				ServerName: "vendor", Endpoint: "https://vendor.example/mcp", EndpointHost: "vendor.example",
			}},
		},
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage(routing notice): %v", err)
	}
	if len(enqueued.RoutingEvents) == 0 {
		t.Fatal("EnqueueUserMessage did not commit a routing notice for a remote-MCP egress decision")
	}
	return enqueued
}

// durableEventRow is the one shape the writer-side drift assertion needs from
// a committed events row: the stored type string, exactly as a writer wrote
// it, and the raw payload bytes, exactly as committed. Nothing about how the
// row got there survives into this shape, which is the point — the assertion
// this feeds must not be able to special-case a row by which flow produced
// it.
type durableEventRow struct {
	Type        string
	PayloadJSON string
}

// fetchAllDurableEvents reads back every row committed to the database's
// events table, in commit order, rather than the handful of event handles a
// caller happened to keep. A hand-picked set of returned Event values can
// only ever cover the writers this test remembered to capture; scanning the
// table itself covers every row any real flow above committed — including a
// terminal transition's subsidiary events (a tool.call.denied or
// tool.call.failed alongside an approval's own lifecycle event, a
// message.completed alongside a run's own completion) that a caller received
// but never asked this test to look at.
func fetchAllDurableEvents(t *testing.T, database *db.DB) []durableEventRow {
	t.Helper()
	rows, err := database.QueryContext(context.Background(), `SELECT type, payload_json FROM events ORDER BY rowid`)
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var events []durableEventRow
	for rows.Next() {
		var row durableEventRow
		if err := rows.Scan(&row.Type, &row.PayloadJSON); err != nil {
			t.Fatalf("scan event row: %v", err)
		}
		events = append(events, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate event rows: %v", err)
	}
	return events
}

// TestRunStateCarrierSetMatchesActualRepositoryWriters is the writer-side
// drift guard: it asserts, for EVERY row this test's real repository writers
// actually committed to the database's events table, that whether the row's
// payload carries a runState key agrees with isRunStateCarrier's prediction
// for that row's canonical type.
//
// run_state_carrier_test.go's runStateCarrierEventTypes is an oracle written
// down by a human reading the repository's source. It is only honest if it is
// exactly the set of types the writers actually commit a snapshot under, and
// that fact can drift the moment either side changes without the other. This
// test does not re-read the source to check; it drives every writer this
// package's carrier gate has to answer for through its real public entry
// point — EnqueueUserMessage, ClaimNextJob, CreateApprovalWithEvent (both the
// transition-borne primary path and the already-waiting fallback path),
// ApproveApprovalWithEvent (the only writer approval.approved has),
// ResumeApprovedRun (the state_changed writer), the three terminal
// transitions, DenyApprovalWithEvent, ExpireApprovalWithEvent,
// ConsumeApprovalWithEvent, and one EnqueueUserMessage call whose accepted
// egress decision makes it write its own agent.run.step routing notice — and
// then reads back every row any of them committed, not just the handful of
// event handles their own return values happen to expose. A terminal
// transition's subsidiary tool.call.denied/tool.call.failed event, or a
// message.completed alongside a run's own completion, is exactly the kind of
// row a hand-picked set of returned Event values could silently never look
// at; scanning the table itself cannot skip it.
//
// Every writer here is reachable through its ordinary public contract; none
// needed a repository-internal seam to exercise, so there is no "closest
// helper" fallback to document.
func TestRunStateCarrierSetMatchesActualRepositoryWriters(t *testing.T) {
	database := openEventTestDB(t)
	repo := repository.New(database)
	ctx := context.Background()

	// agent.run.queued / agent.run.started: EnqueueUserMessage's and
	// ClaimNextJob's own writers.
	primary := startDriftGuardRun(t, repo, "drift-primary")

	// approval.requested, primary path: this request IS the running ->
	// waiting_approval transition, so its own event carries the transition's
	// committed state.
	first, requestedPrimary := requestDriftGuardApproval(t, repo, primary, "call_drift_a")

	// approval.requested, fallback path: the run is already waiting on the
	// first request, so this second one cannot itself move the lifecycle, and
	// falls back to appendApprovalRunStateEventTx.
	second, requestedFallback := requestDriftGuardApproval(t, repo, primary, "call_drift_b")
	if requestedPrimary.Type != requestedFallback.Type {
		t.Fatalf("primary and fallback approval requests disagree on event type: %q vs %q",
			requestedPrimary.Type, requestedFallback.Type)
	}

	// approval.approved: appendApprovalRunStateEventTx is this event's only
	// writer — approving a decision never itself moves a run's lifecycle.
	waiting, err := repo.GetRunState(ctx, primary.enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	if _, err := repo.ApproveApprovalWithEvent(ctx, first.ApprovalID, "token-drift-a", sql.NullString{}, ""); err != nil {
		t.Fatalf("ApproveApprovalWithEvent(first): %v", err)
	}

	// agent.run.state_changed: ResumeApprovedRun is a real transition with no
	// lifecycle event of its own, so it falls through to the shared
	// state-changed projection.
	if _, err := repo.ResumeApprovedRun(ctx, repository.ResumeApprovedRunInput{
		RunID: primary.enqueued.RunID, ApprovalID: first.ApprovalID,
		WorkerID: primary.workerID, AssignmentAttemptID: primary.job.AssignmentAttemptID,
		ExpectedStateVersion: waiting.StateVersion,
	}); err != nil {
		t.Fatalf("ResumeApprovedRun: %v", err)
	}

	// A second approval.approved sample, now that the run is running again:
	// still the same only writer, still no lifecycle move.
	if _, err := repo.ApproveApprovalWithEvent(ctx, second.ApprovalID, "token-drift-b", sql.NullString{}, ""); err != nil {
		t.Fatalf("ApproveApprovalWithEvent(second): %v", err)
	}

	// agent.run.completed / agent.run.failed / agent.run.cancelled: each
	// terminalizes a run exactly once, so each gets its own fresh run.
	// CompleteRunCanonical's fixture also carries displayable content on an
	// assistant message, so it commits a real message.completed row too.
	completedRun := startDriftGuardRun(t, repo, "drift-completed")
	if _, err := repo.CompleteRunCanonical(ctx, repository.CompleteRunInput{
		RunID: completedRun.enqueued.RunID, AssistantMessageID: completedRun.enqueued.AssistantMessageID,
		Content: "done", ExpectedStateVersion: completedRun.job.ExpectedStateVersion,
	}); err != nil {
		t.Fatalf("CompleteRunCanonical: %v", err)
	}

	failedRun := startDriftGuardRun(t, repo, "drift-failed")
	if _, err := repo.FailRunCanonical(ctx, repository.FailRunInput{
		RunID: failedRun.enqueued.RunID, AssistantMessageID: failedRun.enqueued.AssistantMessageID,
		ExpectedStateVersion: failedRun.job.ExpectedStateVersion,
		Failure:              runoutcome.NormalizeFailure(runoutcome.OriginProviderTransport, "model_stream_failed", runoutcome.RetryClassNever),
	}); err != nil {
		t.Fatalf("FailRunCanonical: %v", err)
	}

	cancelledRun := startDriftGuardRun(t, repo, "drift-cancelled")
	if _, err := repo.CancelRunCanonical(ctx, repository.CancelRunInput{
		RunID: cancelledRun.enqueued.RunID, AssistantMessageID: cancelledRun.enqueued.AssistantMessageID,
		ExpectedStateVersion: cancelledRun.job.ExpectedStateVersion,
		Cancellation:         runoutcome.AbandonedCancellation(),
	}); err != nil {
		t.Fatalf("CancelRunCanonical: %v", err)
	}

	// approval.denied / approval.expired / approval.consumed: the three
	// non-carriers this task calls out by name. Each is driven through its
	// real terminal writer on its own fresh run. DenyApprovalWithEvent and
	// ExpireApprovalWithEvent also fail the run whose tool call they
	// terminalize, which is a real writer of tool.call.denied and
	// tool.call.failed respectively (and a second agent.run.failed sample) —
	// this test no longer has to reach past those return values to notice
	// them, because the table scan below reads every row regardless of
	// whether any caller kept the value that produced it.
	deniedRun := startDriftGuardRun(t, repo, "drift-denied")
	deniedApproval, _ := requestDriftGuardApproval(t, repo, deniedRun, "call_drift_denied")
	if _, err := repo.DenyApprovalWithEvent(ctx, deniedApproval.ApprovalID, sql.NullString{String: "no", Valid: true}, ""); err != nil {
		t.Fatalf("DenyApprovalWithEvent: %v", err)
	}

	expiredRun := startDriftGuardRun(t, repo, "drift-expired")
	expiredApproval, _ := requestDriftGuardApproval(t, repo, expiredRun, "call_drift_expired")
	if _, err := repo.ExpireApprovalWithEvent(ctx, expiredApproval.ApprovalID, ""); err != nil {
		t.Fatalf("ExpireApprovalWithEvent: %v", err)
	}

	consumedRun := startDriftGuardRun(t, repo, "drift-consumed")
	consumedApproval, _ := requestDriftGuardApproval(t, repo, consumedRun, "call_drift_consumed")
	if _, err := repo.ApproveApprovalWithEvent(ctx, consumedApproval.ApprovalID, "token-drift-consume", sql.NullString{}, ""); err != nil {
		t.Fatalf("ApproveApprovalWithEvent(consume fixture): %v", err)
	}
	if _, err := repo.ConsumeApprovalWithEvent(ctx, consumedApproval.ApprovalID, ""); err != nil {
		t.Fatalf("ConsumeApprovalWithEvent: %v", err)
	}

	// agent.run.step: the one writer above cannot reach on its own — none of
	// the approval or terminal flows ever notice a tool call, and none of them
	// retries or gives up. EnqueueUserMessage's own routing notice, driven
	// through an accepted remote-MCP egress decision, is the minimal real
	// repository write that gives this canonical type a genuine row to check.
	startDriftGuardRunWithRoutingNotice(t, repo, "drift-routing-notice")

	// The writer-side drift assertion itself: read back every row any fixture
	// above committed, in commit order, and require isRunStateCarrier's
	// prediction to hold for every single one — not just for the returned
	// event handles this test happened to keep. Comparing against
	// isRunStateCarrier — the same predicate Decode gates on — rather than
	// re-listing the carrier names here keeps this test and the production
	// gate reading off one shared source, so a future edit to the carrier set
	// cannot go stale against a second copy living in this file.
	allEvents := fetchAllDurableEvents(t, database)
	if len(allEvents) == 0 {
		t.Fatal("no events committed by any drift-guard fixture")
	}
	observedTypes := map[string]bool{}
	observedCarrierTypes := map[string]bool{}
	for _, row := range allEvents {
		canonicalType := CanonicalType(row.Type)
		if canonicalType == "" {
			t.Fatalf("committed event type %q does not resolve to a canonical type", row.Type)
		}
		hasState := payloadCarriesRunStateKey(t, row.Type, row.PayloadJSON)
		if hasState != isRunStateCarrier(canonicalType) {
			t.Errorf("%s: committed row has runState=%v, but isRunStateCarrier(%q)=%v",
				canonicalType, hasState, canonicalType, isRunStateCarrier(canonicalType))
		}
		observedTypes[canonicalType] = true
		if hasState {
			observedCarrierTypes[canonicalType] = true
		}
	}

	// The exact set this task asks for: every canonical type actually
	// observed carrying runState in a committed row must be exactly this
	// package's declared carrier set, so a writer this test forgot to drive —
	// or a declared carrier that stopped actually carrying a snapshot — fails
	// here instead of the comparison silently passing on an incomplete set.
	if len(observedCarrierTypes) != len(runStateCarrierTypes) {
		t.Fatalf("%d canonical types were observed carrying runState %v, want exactly the declared carrier set's %d members",
			len(observedCarrierTypes), observedCarrierTypes, len(runStateCarrierTypes))
	}
	for carrier := range runStateCarrierTypes {
		if !observedCarrierTypes[carrier] {
			t.Fatalf("declared carrier %q was never observed carrying runState in any committed row", carrier)
		}
	}

	// Every canonical type this suite of fixtures is supposed to exercise —
	// carrier and non-carrier alike, including the deny/expire tool events and
	// the message.completed/session.updated/agent.run.step rows real flows
	// commit as a side effect — must actually have shown up as a committed
	// row, so a fixture that silently stopped driving one of these writers
	// fails loudly instead of the full scan quietly running over fewer rows.
	wantObservedTypes := []string{
		"agent.run.queued", "agent.run.started", "agent.run.state_changed",
		"agent.run.completed", "agent.run.failed", "agent.run.cancelled",
		"approval.requested", "approval.approved",
		"approval.denied", "approval.expired", "approval.consumed",
		"session.updated", "message.completed",
		"tool.call.denied", "tool.call.failed",
		"agent.run.step",
	}
	if len(observedTypes) != len(wantObservedTypes) {
		t.Fatalf("observed %d distinct committed canonical types %v, want exactly %d: %v",
			len(observedTypes), observedTypes, len(wantObservedTypes), wantObservedTypes)
	}
	for _, want := range wantObservedTypes {
		if !observedTypes[want] {
			t.Fatalf("no committed row of expected canonical type %q", want)
		}
	}
}
