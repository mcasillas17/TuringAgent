package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// delegate creates one delegation for the fixture's parent.
func (f delegationFixture) delegate(t *testing.T, toolCallID string) Delegation {
	t.Helper()
	created, err := f.repo.CreateDelegation(f.ctx, f.input(toolCallID))
	if err != nil {
		t.Fatal(err)
	}
	return created.Delegation
}

// joinWorker is a worker at a team protocol version that serves the
// fixture's children and continuations, with room for all of them at once.
func joinWorker(teamProtocolVersion int) *WorkerRoutingCapabilities {
	worker := localWorkerAt(teamProtocolVersion)
	worker.Tools = []string{"files/files.read", "memory/memory.search", "system/system.time"}
	worker.MaxConcurrentRuns = 10
	return worker
}

// startChild claims the next job, which must be the delegation's child.
func (f delegationFixture) startChild(t *testing.T, delegation Delegation) {
	t.Helper()
	claimed, err := f.repo.ClaimNextCompatibleJobWithLimit(f.ctx, "general_assistant", "team-worker", 0, time.Hour,
		joinWorker(1), func(RoutingRequirements) bool { return true })
	if err != nil || claimed.RunID != delegation.ChildRunID {
		t.Fatalf("claim = %+v, %v; want the child %s", claimed, err, delegation.ChildRunID)
	}
}

func (f delegationFixture) stateVersion(t *testing.T, runID string) int64 {
	t.Helper()
	state, err := f.repo.GetRunState(f.ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	return state.StateVersion
}

func (f delegationFixture) assistantMessage(t *testing.T, runID string) string {
	t.Helper()
	var id string
	if err := f.repo.db.QueryRowContext(f.ctx, `SELECT assistant_message_id FROM agent_runs WHERE id = ?`, runID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// complete reports runID's success with content.
func (f delegationFixture) complete(t *testing.T, runID, content string) RunTransitionResult {
	t.Helper()
	result, err := f.repo.CompleteRunCanonical(f.ctx, CompleteRunInput{
		RunID: runID, AssistantMessageID: f.assistantMessage(t, runID), Content: content,
		ExpectedStateVersion: f.stateVersion(t, runID),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func (f delegationFixture) fail(t *testing.T, runID string, failure runoutcome.Failure) RunTransitionResult {
	t.Helper()
	result, err := f.repo.FailRunCanonical(f.ctx, FailRunInput{
		RunID: runID, Failure: failure, ExpectedStateVersion: f.stateVersion(t, runID),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// joinMessages are the parent session's join messages.
func (f delegationFixture) joinMessages(t *testing.T) []Message {
	t.Helper()
	rows, err := f.repo.db.QueryContext(f.ctx, `
		SELECT id, role, content, content_type FROM messages
		WHERE session_id = ? AND content_type = 'delegation_results' ORDER BY sequence`, f.parent.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var messages []Message
	for rows.Next() {
		var message Message
		if err := rows.Scan(&message.MessageID, &message.Role, &message.Content, &message.ContentType); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
	}
	return messages
}

func (f delegationFixture) continuationRunID(t *testing.T) string {
	t.Helper()
	var id string
	err := f.repo.db.QueryRowContext(f.ctx, `SELECT id FROM agent_runs WHERE continues_run_id = ?`, f.parent.RunID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f delegationFixture) wantNoJoin(t *testing.T) {
	t.Helper()
	if messages := f.joinMessages(t); len(messages) != 0 {
		t.Fatalf("join messages = %+v, want none", messages)
	}
	if id := f.continuationRunID(t); id != "" {
		t.Fatalf("continuation %s, want none", id)
	}
	if got := f.count(t, `SELECT COUNT(*) FROM delegations WHERE joined = 1`); got != 0 {
		t.Fatalf("joined delegations = %d, want none", got)
	}
	if got := f.count(t, `SELECT COUNT(*) FROM events WHERE type = 'delegation.finished'`); got != 0 {
		t.Fatalf("delegation.finished events = %d, want none", got)
	}
}

func eventTypes(events []Event) []string {
	types := make([]string, 0, len(events))
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

// The join waits for the parent to complete and for every child to finish,
// whichever of them finishes last, and then writes everything at once: the
// results in the parent's conversation, the continuation that answers from
// them, the delegations marked joined, and one delegation.finished each.
func TestAJoinWaitsForTheParentAndEveryChild(t *testing.T) {
	f := newDelegationFixture(t)
	first, second := f.delegate(t, "call_1"), f.delegate(t, "call_2")

	f.startChild(t, first)
	if result := f.complete(t, first.ChildRunID, "Notes gathered."); result.ContinuationRunID != "" {
		t.Fatalf("a child finishing under a running parent joined: %+v", result)
	}
	f.wantNoJoin(t)
	if result := f.complete(t, f.parent.RunID, "I asked Research twice."); result.ContinuationRunID != "" {
		t.Fatalf("the parent finishing with a child outstanding joined: %+v", result)
	}
	f.wantNoJoin(t)

	f.startChild(t, second)
	result := f.complete(t, second.ChildRunID, "Second notes.")
	continuation := f.continuationRunID(t)
	if continuation == "" || result.ContinuationRunID != continuation {
		t.Fatalf("result continuation = %q, stored %q; want the one continuation", result.ContinuationRunID, continuation)
	}
	joins := f.joinMessages(t)
	if len(joins) != 1 || joins[0].Role != "system" {
		t.Fatalf("join messages = %+v, want one at role system", joins)
	}
	if !strings.Contains(joins[0].Content, "Notes gathered.") || !strings.Contains(joins[0].Content, "Second notes.") ||
		strings.Index(joins[0].Content, "Notes gathered.") > strings.Index(joins[0].Content, "Second notes.") {
		t.Fatalf("join content = %q, want both results in the order they were asked for", joins[0].Content)
	}
	if strings.Contains(joins[0].Content, "TURING_RETRIEVED") {
		t.Fatalf("join content = %q, want it stored unframed", joins[0].Content)
	}
	var anchor, status, provider, model string
	if err := f.repo.db.QueryRowContext(f.ctx, `SELECT user_message_id, status, model_provider, model_name FROM agent_runs WHERE id = ?`,
		continuation).Scan(&anchor, &status, &provider, &model); err != nil {
		t.Fatal(err)
	}
	if anchor != joins[0].MessageID || status != "queued" || provider != "ollama" || model != "llama3.2" {
		t.Fatalf("continuation = anchor %s, %s on %s/%s; want queued on the join, on the parent's route", anchor, status, provider, model)
	}
	if got := f.count(t, `SELECT COUNT(*) FROM delegations WHERE joined = 1 AND finished_at IS NOT NULL
		AND result_bytes > 0 AND error_code IS NULL`); got != 2 {
		t.Fatalf("delegations joined with their outcome = %d, want 2", got)
	}
	types := eventTypes(result.Events)
	if !slices.Equal(types[len(types)-3:], []string{"delegation.finished", "delegation.finished", "agent.run.queued"}) {
		t.Fatalf("transition events = %v, want both finished then the continuation queued", types)
	}
	for _, event := range result.Events[len(result.Events)-3:] {
		if event.SessionID != f.parent.SessionID {
			t.Fatalf("join event %s on session %s, want the parent's", event.Type, event.SessionID)
		}
	}
	var finished map[string]any
	if err := json.Unmarshal([]byte(result.Events[len(result.Events)-3].PayloadJSON), &finished); err != nil {
		t.Fatal(err)
	}
	if finished["delegationId"] != first.ID || finished["state"] != "completed" || finished["continuationRunId"] != continuation ||
		finished["childRunId"] != first.ChildRunID || finished["displayName"] != "Research" || finished["summary"] == "" {
		t.Fatalf("delegation.finished = %+v, want the first delegation, completed", finished)
	}
}

// When the children finish first, the parent's own completion joins.
func TestAJoinRunsWhenTheParentFinishesLast(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegate(t, "call_1")
	f.startChild(t, child)
	f.complete(t, child.ChildRunID, "Done early.")
	f.wantNoJoin(t)

	result := f.complete(t, f.parent.RunID, "Asked Research.")
	if result.ContinuationRunID == "" || result.ContinuationRunID != f.continuationRunID(t) {
		t.Fatalf("parent completion continuation = %q, want the join's", result.ContinuationRunID)
	}
}

// A run that never delegated is never joined, and nor is a continuation,
// which cannot delegate: a join needs at least one unjoined delegation.
func TestARunThatNeverDelegatedIsNotJoined(t *testing.T) {
	f := newDelegationFixture(t)
	if result := f.complete(t, f.parent.RunID, "No help needed."); result.ContinuationRunID != "" {
		t.Fatalf("a run with no delegations joined: %+v", result)
	}
	f.wantNoJoin(t)
}

// Only a completed parent is joined. A failed one leaves its children's
// results where they are.
func TestAParentThatDidNotCompleteIsNotJoined(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegate(t, "call_1")
	f.fail(t, f.parent.RunID, runoutcome.NormalizeFailure(runoutcome.OriginExternalProvider, "model_error", runoutcome.RetryClassNever))
	f.startChild(t, child)
	f.complete(t, child.ChildRunID, "Nobody will read this.")
	f.wantNoJoin(t)
}

// A parent being deleted is never joined: its set of children is closed and a
// continuation would be new work in a conversation that is going away.
func TestAParentBeingDeletedIsNotJoined(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegate(t, "call_1")
	f.complete(t, f.parent.RunID, "Asked Research.")
	f.startChild(t, child)
	if _, err := f.repo.BeginSessionDeletion(f.ctx, f.parent.SessionID); err != nil {
		t.Fatal(err)
	}
	result := f.complete(t, child.ChildRunID, "Too late.")
	if result.ContinuationRunID != "" {
		t.Fatalf("a child finishing under a parent being deleted joined: %+v", result)
	}
	f.wantNoJoin(t)
}

// The join runs once. Calling it again for the same parent finds nothing
// unjoined and writes nothing; the unique index is the backstop if it did.
func TestAJoinRunsOnce(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegate(t, "call_1")
	f.complete(t, f.parent.RunID, "Asked Research.")
	f.startChild(t, child)
	f.complete(t, child.ChildRunID, "Notes.")

	tx, err := f.repo.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	continuation, events, err := maybeJoinTx(f.ctx, tx, child.ChildRunID)
	if err != nil || continuation != "" || len(events) != 0 {
		t.Fatalf("second join = %q, %v, %v; want nothing", continuation, eventTypes(events), err)
	}
	if _, err := tx.ExecContext(f.ctx, `UPDATE delegations SET joined = 0`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := maybeJoinTx(f.ctx, tx, child.ChildRunID); err == nil || !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("a forced second join: err = %v, want the unique index to refuse a second continuation", err)
	}
}

// The continuation is Turing speaking: it pins what its parent pinned, gets
// only the frozen continuation tools, cannot delegate or recall, and only a
// team-protocol worker may run it. Its live turn is the results, framed.
func TestTheContinuationJobAnswersFromTheResultsAlone(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegate(t, "call_1")
	f.complete(t, f.parent.RunID, "Asked Research.")
	f.startChild(t, child)
	f.complete(t, child.ChildRunID, "Notes.")
	continuation := f.continuationRunID(t)

	var parentJob, jobID string
	if err := f.repo.db.QueryRowContext(f.ctx, `SELECT id FROM jobs WHERE run_id = ?`, continuation).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if err := f.repo.db.QueryRowContext(f.ctx, `SELECT id FROM jobs WHERE run_id = ?`, f.parent.RunID).Scan(&parentJob); err != nil {
		t.Fatal(err)
	}
	keys := jobPayloadKeys(t, f.ctx, f.repo, jobID)
	parent := jobPayloadKeys(t, f.ctx, f.repo, parentJob)
	var userText string
	if err := json.Unmarshal(keys["userText"], &userText); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(userText, "BEGIN TURING_RETRIEVED_DELEGATION_RESULTS_") || !strings.Contains(userText, "Notes.") {
		t.Fatalf("userText = %q, want the results framed as DELEGATION_RESULTS", userText)
	}
	tools, _ := json.Marshal(testContinuation.Tools)
	for _, key := range []string{"selectedTools", "requestedTools"} {
		if string(keys[key]) != string(tools) {
			t.Fatalf("%s = %s, want the frozen continuation tools %s", key, keys[key], tools)
		}
	}
	for key, want := range map[string]string{
		"enforceSelectedTools": "true", "skipAutomaticRecall": "true", "minimumTeamProtocolVersion": "1",
		"egressDecision": "null", "externalAgent": "null",
	} {
		if string(keys[key]) != want {
			t.Fatalf("%s = %s, want %s", key, keys[key], want)
		}
	}
	for _, key := range []string{"teamRoster", "teamContinuation", "agentProfile"} {
		if _, ok := keys[key]; ok {
			t.Fatalf("payload carries %s = %s, want none: a continuation cannot delegate and is not a specialist", key, keys[key])
		}
	}
	for _, key := range []string{"pinnedPersona", "pinnedProfile", "skills"} {
		if string(keys[key]) != string(parent[key]) {
			t.Fatalf("%s = %s, want the parent's %s", key, keys[key], parent[key])
		}
	}
	var persona PinnedPersonaSnapshot
	var profile PinnedProfileSnapshot
	if err := json.Unmarshal(keys["pinnedPersona"], &persona); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(keys["pinnedProfile"], &profile); err != nil {
		t.Fatal(err)
	}
	want, err := backendegress.MemorySnapshotFingerprint(continuationMemoryPreimage(persona, profile, testContinuation.Tools))
	if err != nil {
		t.Fatal(err)
	}
	if string(keys["memorySnapshotFingerprint"]) != `"`+want+`"` {
		t.Fatalf("memorySnapshotFingerprint = %s, want %q for the continuation's own tools", keys["memorySnapshotFingerprint"], want)
	}

	if claimed, err := f.repo.ClaimNextCompatibleJobWithLimit(f.ctx, "general_assistant", "old-worker", 0, time.Hour,
		joinWorker(0), func(RoutingRequirements) bool { return true }); err != nil || claimed.JobID != "" {
		t.Fatalf("version-0 claim = %+v, %v; want nothing", claimed, err)
	}
	claimed, err := f.repo.ClaimNextCompatibleJobWithLimit(f.ctx, "general_assistant", "team-worker", 0, time.Hour,
		joinWorker(1), func(RoutingRequirements) bool { return true })
	if err != nil || claimed.RunID != continuation {
		t.Fatalf("team-protocol claim = %+v, %v; want the continuation", claimed, err)
	}
}

// Each result is cut to the frozen cap on a character boundary, visibly, and
// the delegation records how long it really was. A specialist that failed or
// was cancelled says so, with its error code.
func TestAJoinReportsEachOutcomeAndCutsLongResults(t *testing.T) {
	f := newDelegationFixture(t)
	// 17 bytes falls inside the ninth two-byte character, so only a cut on a
	// character boundary keeps 16.
	f.exec(t, `UPDATE jobs SET payload_json = json_set(payload_json, '$.teamContinuation.resultMaxBytes', 17) WHERE run_id = ?`, f.parent.RunID)
	long, failed, cancelled := f.delegate(t, "call_1"), f.delegate(t, "call_2"), f.delegate(t, "call_3")
	f.complete(t, f.parent.RunID, "Asked three.")

	result := strings.Repeat("é", 20) // 40 bytes, two per character
	f.startChild(t, long)
	f.complete(t, long.ChildRunID, result)
	f.startChild(t, failed)
	f.fail(t, failed.ChildRunID, runoutcome.NormalizeFailure(runoutcome.OriginToolExecution, "tool_call_failed", runoutcome.RetryClassNever))
	if _, err := f.repo.CancelUserRun(f.ctx, cancelled.ChildSessionID, cancelled.ChildRunID, "cancel-1"); err != nil {
		t.Fatal(err)
	}

	joins := f.joinMessages(t)
	if len(joins) != 1 {
		t.Fatalf("join messages = %d, want 1", len(joins))
	}
	content := joins[0].Content
	if !utf8.ValidString(content) || !strings.Contains(content, strings.Repeat("é", 8)) || strings.Contains(content, strings.Repeat("é", 9)) {
		t.Fatalf("join content = %q, want the long result cut at 17 bytes back to 16, on a character boundary", content)
	}
	if !strings.Contains(content, "16 of 40 bytes") {
		t.Fatalf("join content = %q, want a visible cut saying how much was kept", content)
	}
	if !strings.Contains(content, "failed") || !strings.Contains(content, "tool_call_failed") {
		t.Fatalf("join content = %q, want the failure and its code", content)
	}
	if !strings.Contains(content, "cancelled by you") {
		t.Fatalf("join content = %q, want the user's cancellation", content)
	}
	if got := f.count(t, `SELECT result_bytes FROM delegations WHERE id = ?`, long.ID); got != 40 {
		t.Fatalf("result_bytes = %d, want the whole result's 40", got)
	}
	var code string
	if err := f.repo.db.QueryRowContext(f.ctx, `SELECT error_code FROM delegations WHERE id = ?`, failed.ID).Scan(&code); err != nil || code != "tool_call_failed" {
		t.Fatalf("failed delegation error_code = %q, %v; want tool_call_failed", code, err)
	}
}

// The continuation is the first run anchored on a message that is not the
// user's. Turn order still follows anchor sequences: a message the user sent
// before the join runs first, the continuation after it.
func TestAContinuationTakesItsTurnByItsAnchor(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegate(t, "call_1")
	f.complete(t, f.parent.RunID, "Asked Research.")
	next, err := f.repo.EnqueueUserMessage(f.ctx, EnqueueUserMessageInput{
		SessionID: f.parent.SessionID, Content: "and another thing", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.startChild(t, child)
	f.complete(t, child.ChildRunID, "Notes.")
	continuation := f.continuationRunID(t)

	claim := func() string {
		claimed, err := f.repo.ClaimNextCompatibleJobWithLimit(f.ctx, "general_assistant", "team-worker", 0, time.Hour,
			joinWorker(1), func(RoutingRequirements) bool { return true })
		if err != nil {
			t.Fatal(err)
		}
		return claimed.RunID
	}
	if got := claim(); got != next.RunID {
		t.Fatalf("first claim = %s, want the user's earlier turn %s", got, next.RunID)
	}
	if got := claim(); got != "" {
		t.Fatalf("second claim = %s, want nothing while the earlier turn runs", got)
	}
	f.complete(t, next.RunID, "Answered.")
	if got := claim(); got != continuation {
		t.Fatalf("claim after the earlier turn = %s, want the continuation %s", got, continuation)
	}
}

// A child can also finish because its approval was denied, which the
// approval path reports event by event. The join it triggers comes back
// with it, for the caller to publish and dispatch.
func TestADeniedChildApprovalReturnsItsJoin(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegate(t, "call_1")
	f.complete(t, f.parent.RunID, "Asked Research.")
	f.startChild(t, child)
	if err := f.repo.RecordToolCallBefore(f.ctx, ToolCallRecord{
		ToolCallID: "call_child_write", RunID: child.ChildRunID, ModelToolCallID: "model_child_write",
	}, "general_assistant", "files", "files.update", `{"path":"note.txt"}`, "sha256:child-write"); err != nil {
		t.Fatal(err)
	}
	approval, _, err := f.repo.CreateApprovalWithEvent(f.ctx, child.ChildRunID, "call_child_write", "general_assistant",
		"files.update", `{"path":"note.txt"}`, "sha256:child-write", "2099-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}

	denied, err := f.repo.DenyApprovalWithEvent(f.ctx, approval.ApprovalID, sql.NullString{String: "no", Valid: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	continuation := f.continuationRunID(t)
	if continuation == "" || denied.ContinuationRunID != continuation {
		t.Fatalf("denial continuation = %q, stored %q; want the join's", denied.ContinuationRunID, continuation)
	}
	if types := eventTypes(denied.JoinEvents); !slices.Equal(types, []string{"delegation.finished", "agent.run.queued"}) {
		t.Fatalf("join events = %v, want the delegation finished and the continuation queued", types)
	}
}

// A conversation that has delegated replays its specialists' results in
// every later turn. Only a worker that frames them as data may run one, so
// every later turn needs the team protocol, with or without a roster.
func TestEveryLaterTurnInAConversationThatDelegatedNeedsTheTeamProtocol(t *testing.T) {
	f := newDelegationFixture(t)
	f.delegate(t, "call_1")
	var validated []RoutingRequirements
	later, err := f.repo.EnqueueUserMessage(f.ctx, EnqueueUserMessageInput{
		SessionID: f.parent.SessionID, Content: "and another thing", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
		ValidateRouting: func(_ context.Context, route RoutingRequirements) error {
			validated = append(validated, route)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(validated) != 1 || validated[0].MinimumTeamProtocolVersion != 1 {
		t.Fatalf("validated routes = %+v, want one at team protocol 1", validated)
	}
	keys := jobPayloadKeys(t, f.ctx, f.repo, later.JobID)
	if string(keys["minimumTeamProtocolVersion"]) != "1" {
		t.Fatalf("minimumTeamProtocolVersion = %s, want 1 without a roster", keys["minimumTeamProtocolVersion"])
	}
	if _, ok := keys["teamRoster"]; ok {
		t.Fatalf("payload carries a roster %s, want none", keys["teamRoster"])
	}

	// The roster is optional and is dropped when no team-protocol worker is
	// left, but the history is not: such a turn is refused, not handed to an
	// older worker.
	refusal := errors.New("no connected worker supports team protocol v1")
	_, err = f.repo.EnqueueUserMessage(f.ctx, EnqueueUserMessageInput{
		SessionID: f.parent.SessionID, Content: "once more", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2", TeamRoster: testRoster, TeamContinuation: testContinuation,
		ValidateRouting: func(_ context.Context, route RoutingRequirements) error {
			if route.MinimumTeamProtocolVersion > 0 {
				return refusal
			}
			return nil
		},
	})
	if !errors.Is(err, refusal) {
		t.Fatalf("enqueue with no team-protocol worker = %v, want the routing refusal", err)
	}
}

// The frozen cap was validated against the delegation limit of its day; a
// restart can raise the limit and leave a parent with more tasks than that cap
// was sized for. The join therefore also cuts each result to what its actual
// number of tasks leaves room for, so the frame never cuts the whole.
func TestAJoinCutsResultsToFitTheTasksItActuallyHas(t *testing.T) {
	f := newDelegationFixture(t)
	f.exec(t, `UPDATE jobs SET payload_json = json_set(payload_json, '$.teamContinuation.resultMaxBytes', 63000) WHERE run_id = ?`, f.parent.RunID)
	delegations := []Delegation{f.delegate(t, "call_1"), f.delegate(t, "call_2"), f.delegate(t, "call_3")}
	f.complete(t, f.parent.RunID, "Asked three.")
	result := strings.Repeat("x", 30000)
	for _, delegation := range delegations {
		f.startChild(t, delegation)
		f.complete(t, delegation.ChildRunID, result)
	}

	budget := backendegress.DelegationResultBudget(3)
	joins := f.joinMessages(t)
	if len(joins) != 1 || strings.Count(joins[0].Content, fmt.Sprintf("[Result cut: %d of 30000 bytes kept.]", budget)) != 3 {
		t.Fatalf("join = %d messages, want each result cut to the %d bytes three tasks leave room for", len(joins), budget)
	}
	var userText string
	if err := f.repo.db.QueryRowContext(f.ctx, `SELECT json_extract(payload_json, '$.userText') FROM jobs WHERE run_id = ?`,
		f.continuationRunID(t)).Scan(&userText); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(userText, joins[0].Content) {
		t.Fatal("the continuation's frame cut the join; only each result may be cut")
	}
}

// Each task's header, however long the names and codes it reports, stays
// inside the allowance the frame budget reserves for it, so at every count the
// results are cut to their budget and the frame never cuts the whole.
func TestAJoinAtItsBudgetAlwaysFitsItsFrame(t *testing.T) {
	for _, count := range []int{1, 3, 7, 30, 62} {
		budget := backendegress.DelegationResultBudget(count)
		delegations := make([]joinedDelegation, count)
		for index := range delegations {
			delegations[index] = joinedDelegation{
				profileID: strings.Repeat("p", 500), displayName: strings.Repeat("n", 500), emoji: strings.Repeat("🔬", 50),
				state: lifecycleFailed, errorCode: strings.Repeat("e", 500), result: strings.Repeat("r", budget+100),
			}
		}
		content := delegationResultsContent(delegations, budget)
		framed, err := backendegress.FrameRetrievedContent(backendegress.DelegationResultsFraming(), []byte(content))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(framed, content) {
			t.Fatalf("%d tasks at a %d-byte budget: the frame cut the join", count, budget)
		}
	}
}

// A task whose attempts run out ends through the retry path, a terminal
// writer of its own, and its parent is joined there like anywhere else.
func TestATaskThatRunsOutOfAttemptsIsJoined(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegate(t, "call_1")
	f.complete(t, f.parent.RunID, "Asked Research.")
	f.startChild(t, child)

	decision, err := f.repo.RequeueOrFailRetryableRun(f.ctx, RetryableRunFailureInput{
		RunID: child.ChildRunID, Failure: dispatchCondition("worker_busy"), MaxAttempts: 1,
	})
	if err != nil || decision.Requeued {
		t.Fatalf("decision = %+v, %v; want the task failed", decision, err)
	}
	if f.continuationRunID(t) == "" {
		t.Fatal("no continuation after the last task ran out of attempts")
	}
	if types := eventTypes(decision.Events); !slices.Contains(types, "delegation.finished") {
		t.Fatalf("retry events = %v, want the join's delegation.finished among them", types)
	}
}
