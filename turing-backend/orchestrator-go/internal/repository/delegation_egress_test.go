package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

const githubIssueTool = "integrations/github.get_issue"

// delegateNeedingConsent creates a delegation whose specialist may also use a
// GitHub tool once the user consents for this task.
func (f delegationFixture) delegateNeedingConsent(t *testing.T, toolCallID string, required bool) Delegation {
	t.Helper()
	input := f.input(toolCallID)
	input.EgressTools = []string{githubIssueTool}
	input.EgressRequired = required
	created, err := f.repo.CreateDelegation(f.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	return created.Delegation
}

// egressWorker serves the fixture's children with the GitHub tool too, and can
// validate an egress decision.
func egressWorker() *WorkerRoutingCapabilities {
	worker := joinWorker(1)
	worker.Tools = append(worker.Tools, githubIssueTool)
	worker.RemoteEgressDecisionVersion = RunEgressDecisionVersion
	return worker
}

func (f delegationFixture) claim(t *testing.T, worker *WorkerRoutingCapabilities) Job {
	t.Helper()
	claimed, err := f.repo.ClaimNextCompatibleJobWithLimit(f.ctx, "general_assistant", "team-worker", 0, time.Hour,
		worker, func(RoutingRequirements) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	return claimed
}

// consentFor is the decision the team service would record for this
// context: the GitHub connection, the task's whole set, and the bindings the
// context computed.
func consentFor(context DelegationEgress, nonce string) *PendingEgressDecision {
	return &PendingEgressDecision{
		Version: RunEgressDecisionVersion, ChallengeNonce: nonce, ChallengeFingerprint: "sha256:challenge-" + nonce,
		RequestDigest: "sha256:request", Provider: "ollama", Model: context.Model,
		DataCategories:            []string{"EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS", "EGRESS_DATA_CATEGORY_TOOL_RESULTS"},
		SelectedTools:             slices.Clone(context.SelectedTools),
		SkillSnapshotFingerprint:  context.SkillSnapshotFingerprint,
		MemorySnapshotFingerprint: context.MemorySnapshotFingerprint,
		ConsentGrantedAt:          now(),
		IntegrationEndpoints: []IntegrationEndpointEgress{{
			Endpoint: GitHubIntegrationEndpoint, EndpointHost: GitHubIntegrationEndpointHost,
			ConnectionID: "conn_1", DisplayName: "GitHub", Tools: []string{"github.get_issue"},
		}},
	}
}

func (f delegationFixture) egressContext(t *testing.T, delegationID string) DelegationEgress {
	t.Helper()
	context, err := f.repo.DelegationEgressContext(f.ctx, delegationID)
	if err != nil {
		t.Fatal(err)
	}
	return context
}

func (f delegationFixture) childPayload(t *testing.T, childRunID string) queuedJobPayload {
	t.Helper()
	var raw string
	if err := f.repo.db.QueryRowContext(f.ctx, `SELECT payload_json FROM jobs WHERE run_id = ?`, childRunID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var payload queuedJobPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

// A task whose specialist may use a tool that leaves the machine waits for the
// user's consent before any worker can take it, and its card says so.
func TestATaskNeedingConsentWaitsUnclaimable(t *testing.T) {
	f := newDelegationFixture(t)
	input := f.input("call_1")
	input.EgressTools = []string{githubIssueTool}
	created, err := f.repo.CreateDelegation(f.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if created.Delegation.State != DelegationWaitingConsent {
		t.Fatalf("created state = %q, want %q", created.Delegation.State, DelegationWaitingConsent)
	}
	for _, event := range created.Events {
		if event.Type == "delegation.started" && (!strings.Contains(event.PayloadJSON, `"state":"waiting_consent"`) ||
			!strings.Contains(event.PayloadJSON, `"egressTools":["integrations/github.get_issue"]`) ||
			!strings.Contains(event.PayloadJSON, `"egressConsent":"pending"`) ||
			!strings.Contains(event.PayloadJSON, `"egressRequired":false`)) {
			t.Fatalf("delegation.started = %s, want the task waiting for consent to the GitHub tool", event.PayloadJSON)
		}
	}
	record, err := f.repo.GetDelegation(f.ctx, created.Delegation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != DelegationWaitingConsent || record.EgressConsent != "pending" ||
		!slices.Equal(record.EgressTools, []string{githubIssueTool}) || record.EgressRequired ||
		record.Summary != "🔬 Research is waiting for your consent" {
		t.Fatalf("card = %+v, want waiting for consent to use GitHub", record)
	}
	if claimed := f.claim(t, egressWorker()); claimed.RunID != "" {
		t.Fatalf("claimed %s while its consent was pending", claimed.RunID)
	}
	payload := f.childPayload(t, created.Delegation.ChildRunID)
	if slices.Contains(payload.SelectedTools, githubIssueTool) || payload.EgressDecision != nil {
		t.Fatalf("a waiting task's job = %+v, want only its local tools and no decision", payload)
	}
}

// The same tool call asked again returns the held task as it stands, still
// waiting for consent, and creates nothing.
func TestAReplayedCallReportsTheTaskWaitingForConsent(t *testing.T) {
	f := newDelegationFixture(t)
	input := f.input("call_1")
	input.EgressTools = []string{githubIssueTool}
	first, err := f.repo.CreateDelegation(f.ctx, input)
	if err != nil || first.Delegation.State != DelegationWaitingConsent {
		t.Fatalf("created = %+v, %v; want it waiting for consent", first, err)
	}
	again, err := f.repo.CreateDelegation(f.ctx, input)
	if err != nil || !again.Replayed || again.Delegation != first.Delegation || len(again.Events) != 0 {
		t.Fatalf("replay = %+v, %v; want the task as created, waiting, and no events", again, err)
	}
	found, ok, err := f.repo.DelegationForToolCall(f.ctx, f.parent.RunID, "call_1")
	if err != nil || !ok || found != first.Delegation {
		t.Fatalf("lookup = %+v, %v, %v; want the task waiting for consent", found, ok, err)
	}
}

// The tasks held for consent are counted until they are decided; a task that
// never asked is not one.
func TestTasksWaitingForConsentAreCounted(t *testing.T) {
	f := newDelegationFixture(t)
	unanswered := f.delegateNeedingConsent(t, "call_1", false)
	held := f.delegateNeedingConsent(t, "call_2", true)
	if _, err := f.repo.CreateDelegation(f.ctx, f.input("call_3")); err != nil {
		t.Fatal(err)
	}
	if n, err := f.repo.DelegationsWaitingForConsent(f.ctx); err != nil || n != 2 {
		t.Fatalf("waiting = %d, %v; want 2", n, err)
	}
	if _, err := f.repo.DeclineDelegationEgress(f.ctx, held.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := f.repo.DelegationsWaitingForConsent(f.ctx); err != nil || n != 1 {
		t.Fatalf("waiting after a decline = %d, %v; want 1", n, err)
	}
	// Its consent is never decided, but a task that ended waits for nothing.
	if _, err := f.repo.ExpireQueuedRun(f.ctx, ExpireQueuedRunInput{
		RunID: unanswered.ChildRunID, Policy: runoutcome.QueueTimeoutPolicyFail, Code: runoutcome.CodeQueueWaitExpired,
	}); err != nil {
		t.Fatal(err)
	}
	if n, err := f.repo.DelegationsWaitingForConsent(f.ctx); err != nil || n != 0 {
		t.Fatalf("waiting after the last one ended = %d, %v; want 0", n, err)
	}
}

// A task that needs no consent is claimable at once, as before.
func TestATaskNeedingNoConsentIsNotHeld(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegate(t, "call_1")
	if record, err := f.repo.GetDelegation(f.ctx, child.ID); err != nil || record.EgressConsent != "" || len(record.EgressTools) != 0 {
		t.Fatalf("card = %+v, %v; want no consent asked", record, err)
	}
	if claimed := f.claim(t, egressWorker()); claimed.RunID != child.ChildRunID {
		t.Fatalf("claimed %q, want the task", claimed.RunID)
	}
}

// The context a disclosure is prepared from names the task's whole set, the
// local tools it already had plus the ones it waits on, and binds the job's
// own skills and memory to it.
func TestTheConsentContextIsTheTasksWholeSet(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegateNeedingConsent(t, "call_1", true)
	context := f.egressContext(t, child.ID)
	want := []string{"files/files.read", githubIssueTool, "system/system.time"}
	if context.DelegationID != child.ID || context.ChildRunID != child.ChildRunID || context.ChildSessionID != child.ChildSessionID ||
		context.Model != "llama3.2" || !slices.Equal(context.SelectedTools, want) ||
		!slices.Equal(context.EgressTools, []string{githubIssueTool}) || !context.Required ||
		context.SkillSnapshotFingerprint == "" || context.MemorySnapshotFingerprint == "" {
		t.Fatalf("context = %+v, want the whole set %v", context, want)
	}
}

// Consenting freezes a decision onto the task's own run, widens its frozen set
// to the tools it was granted, and lets a worker that can validate the
// decision take it. Its turn's stream is told, and the task's own conversation
// says where its tool calls may go.
func TestConsentingLetsTheTaskRunWithItsTools(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegateNeedingConsent(t, "call_1", false)
	context := f.egressContext(t, child.ID)

	result, err := f.repo.GrantDelegationEgress(f.ctx, child.ID, consentFor(context, "nonce-1"))
	if err != nil {
		t.Fatal(err)
	}
	updates := decodeUpdates(t, result.Events)
	if len(updates) != 1 || updates[0].State != "queued" || updates[0].RunID != f.parent.RunID {
		t.Fatalf("grant's delegation events = %+v, want the task queued on its turn's stream", updates)
	}
	for _, event := range result.Events {
		if event.Type == "delegation.updated" && (!strings.Contains(event.PayloadJSON, `"egressConsent":"granted"`) ||
			!strings.Contains(event.PayloadJSON, `"egressTools":["integrations/github.get_issue"]`)) {
			t.Fatalf("delegation.updated = %s, want the consent granted for the GitHub tool", event.PayloadJSON)
		}
	}
	const wantNote = "Tool calls may go to api.github.com — disclosed data categories: Tool arguments, Tool results. " +
		"Data leaves your machine if this task calls one of those tools"
	notice := false
	for _, event := range result.Events {
		var payload struct {
			Note string `json:"note"`
		}
		if event.SessionID == child.ChildSessionID && json.Unmarshal([]byte(event.PayloadJSON), &payload) == nil {
			notice = notice || payload.Note == wantNote
		}
	}
	if !notice {
		t.Fatalf("grant events = %v, want the task's conversation told %q", eventTypes(result.Events), wantNote)
	}
	decision, err := f.repo.GetRunEgressDecision(f.ctx, child.ChildRunID)
	if err != nil || !slices.Equal(decision.SelectedTools, context.SelectedTools) {
		t.Fatalf("decision = %+v, %v; want the whole set on the task's run", decision, err)
	}
	payload := f.childPayload(t, child.ChildRunID)
	if !slices.Equal(payload.SelectedTools, context.SelectedTools) || !slices.Equal(payload.RequestedTools, context.SelectedTools) ||
		payload.EgressDecision == nil || payload.EgressDecision.DecisionID != decision.DecisionID ||
		payload.MemorySnapshotFingerprint != decision.MemorySnapshotFingerprint || !payload.EnforceSelectedTools {
		t.Fatalf("granted job = %+v, want the whole set and the decision", payload)
	}
	if record, err := f.repo.GetDelegation(f.ctx, child.ID); err != nil || record.EgressConsent != "granted" || record.State != "queued" {
		t.Fatalf("card = %+v, %v; want consent granted, queued", record, err)
	}
	if claimed := f.claim(t, joinWorker(1)); claimed.RunID != "" {
		t.Fatalf("a worker without the GitHub tool claimed %s", claimed.RunID)
	}
	claimed := f.claim(t, egressWorker())
	if claimed.RunID != child.ChildRunID || claimed.EgressDecision == nil {
		t.Fatalf("claimed %+v, want the task with its decision", claimed)
	}
	if allowed, err := f.repo.RunAllowsIntegration(f.ctx, child.ChildRunID, GitHubIntegrationEndpoint, "conn_1", "github.get_issue"); err != nil || !allowed {
		t.Fatalf("RunAllowsIntegration = %v, %v; want the granted tool allowed", allowed, err)
	}
}

// A decision that does not describe the task as it now stands is refused and
// changes nothing.
func TestConsentThatNoLongerMatchesIsRefused(t *testing.T) {
	cases := map[string]struct {
		edit func(*PendingEgressDecision)
		want error
	}{
		"tools":  {func(p *PendingEgressDecision) { p.SelectedTools = []string{"files/files.read", githubIssueTool} }, ErrEgressDecisionInvalid},
		"skills": {func(p *PendingEgressDecision) { p.SkillSnapshotFingerprint = "sha256:other" }, ErrEgressSkillSnapshotChanged},
		"memory": {func(p *PendingEgressDecision) { p.MemorySnapshotFingerprint = "sha256:other" }, ErrEgressMemorySnapshotChanged},
		"model":  {func(p *PendingEgressDecision) { p.Model = "llama3.1" }, ErrEgressDecisionInvalid},
		"remote": {func(p *PendingEgressDecision) { p.Provider = "openai_compatible" }, ErrEgressDecisionInvalid},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			f := newDelegationFixture(t)
			child := f.delegateNeedingConsent(t, "call_1", false)
			pending := consentFor(f.egressContext(t, child.ID), "nonce-1")
			test.edit(pending)
			if _, err := f.repo.GrantDelegationEgress(f.ctx, child.ID, pending); !errors.Is(err, test.want) {
				t.Fatalf("grant = %v, want %v", err, test.want)
			}
			if record, err := f.repo.GetDelegation(f.ctx, child.ID); err != nil || record.EgressConsent != "pending" {
				t.Fatalf("card = %+v, %v; want consent still pending", record, err)
			}
			if _, err := f.repo.GetRunEgressDecision(f.ctx, child.ChildRunID); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("a refused grant recorded a decision: %v", err)
			}
		})
	}
}

// Consent is given once per task, and a challenge's nonce once anywhere.
func TestConsentIsGivenOnce(t *testing.T) {
	f := newDelegationFixture(t)
	first := f.delegateNeedingConsent(t, "call_1", false)
	second := f.delegateNeedingConsent(t, "call_2", false)
	if _, err := f.repo.GrantDelegationEgress(f.ctx, first.ID, consentFor(f.egressContext(t, first.ID), "nonce-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.GrantDelegationEgress(f.ctx, first.ID, consentFor(f.egressContext(t, second.ID), "nonce-2")); !errors.Is(err, ErrDelegationEgressNotPending) {
		t.Fatalf("second grant = %v, want not pending", err)
	}
	if _, err := f.repo.DeclineDelegationEgress(f.ctx, first.ID); !errors.Is(err, ErrDelegationEgressNotPending) {
		t.Fatalf("decline after grant = %v, want not pending", err)
	}
	if _, err := f.repo.DelegationEgressContext(f.ctx, first.ID); !errors.Is(err, ErrDelegationEgressNotPending) {
		t.Fatalf("context after grant = %v, want not pending", err)
	}
	if _, err := f.repo.GrantDelegationEgress(f.ctx, second.ID, consentFor(f.egressContext(t, second.ID), "nonce-1")); !errors.Is(err, ErrEgressChallengeAlreadyUsed) {
		t.Fatalf("a reused nonce = %v, want already used", err)
	}
}

// Declining lets a task that does not require the tools run without them, as
// it would have had it never asked; its card and the join say so.
func TestDecliningRunsTheTaskWithoutTheTools(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegateNeedingConsent(t, "call_1", false)
	result, err := f.repo.DeclineDelegationEgress(f.ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updates := decodeUpdates(t, result.Events); len(updates) != 1 || updates[0].State != "queued" {
		t.Fatalf("decline's delegation events = %+v, want the task queued", updates)
	}
	record, err := f.repo.GetDelegation(f.ctx, child.ID)
	if err != nil || record.EgressConsent != "declined" || record.State != "queued" {
		t.Fatalf("card = %+v, %v; want consent declined, queued", record, err)
	}
	claimed := f.claim(t, joinWorker(1))
	if claimed.RunID != child.ChildRunID || claimed.EgressDecision != nil || slices.Contains(claimed.SelectedTools, githubIssueTool) {
		t.Fatalf("claimed %+v, want the task with only its local tools", claimed)
	}
	f.complete(t, child.ChildRunID, "Read the notes.")
	joined := f.complete(t, f.parent.RunID, "Asked Research.")
	if joined.ContinuationRunID == "" {
		t.Fatal("the task's end did not join")
	}
	if content := f.joinContent(t); !strings.Contains(content, "completed; you declined its use of "+githubIssueTool) {
		t.Fatalf("join = %q, want it to say the user declined the tool", content)
	}
}

// A declined task that never ran is not said to have run without the tools.
func TestADeclinedTaskThatNeverRanIsNotSaidToHaveRun(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegateNeedingConsent(t, "call_1", false)
	if _, err := f.repo.DeclineDelegationEgress(f.ctx, child.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.CancelUserRun(f.ctx, child.ChildSessionID, child.ChildRunID, "cancel-1"); err != nil {
		t.Fatal(err)
	}
	f.complete(t, f.parent.RunID, "Asked Research.")
	content := f.joinContent(t)
	if !strings.Contains(content, "cancelled by you; you declined its use of "+githubIssueTool) || strings.Contains(content, "ran without") {
		t.Fatalf("join = %q, want the cancel and the declined tool, and no claim that it ran", content)
	}
}

// Declining tools a task requires ends it without running it, and its end is
// joined like any other.
func TestDecliningRequiredToolsEndsTheTask(t *testing.T) {
	f := newDelegationFixture(t)
	child := f.delegateNeedingConsent(t, "call_1", true)
	f.complete(t, f.parent.RunID, "Asked Research.")
	result, err := f.repo.DeclineDelegationEgress(f.ctx, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.ContinuationRunID == "" {
		t.Fatal("ending the last task did not queue the continuation")
	}
	run, err := f.repo.GetRun(f.ctx, child.ChildRunID)
	if err != nil || run.Status != "cancelled" || run.OutcomeReason != string(runoutcome.ReasonAbandoned) ||
		run.CancellationReason != runoutcome.CodeEgressConsentDeclined {
		t.Fatalf("task run = %+v, %v; want cancelled because consent was declined", run, err)
	}
	record, err := f.repo.GetDelegation(f.ctx, child.ID)
	if err != nil || record.EgressConsent != "declined" ||
		record.Summary != "🔬 Research was not run because you declined the tools it needs" {
		t.Fatalf("card = %+v, %v", record, err)
	}
	if content := f.joinContent(t); !strings.Contains(content, "was not run because you declined the tools it needs") {
		t.Fatalf("join = %q, want it to say the task was not run", content)
	}
}

// A task that stopped waiting — its turn ended, it waited out the queue
// bound, or its conversation is being deleted — can no longer be consented to
// or declined.
func TestConsentEndsWithTheTask(t *testing.T) {
	t.Run("queue bound", func(t *testing.T) {
		f := newDelegationFixture(t)
		child := f.delegateNeedingConsent(t, "call_1", false)
		// The hold is only in the claim: the queue bound still sees a task
		// nobody answered for, like any other queued run.
		if _, err := f.repo.ExpireQueuedRun(f.ctx, ExpireQueuedRunInput{
			RunID: child.ChildRunID, Policy: runoutcome.QueueTimeoutPolicyFail, Code: runoutcome.CodeQueueWaitExpired,
		}); err != nil {
			t.Fatal(err)
		}
		if record, err := f.repo.GetDelegation(f.ctx, child.ID); err != nil || record.State != "failed" {
			t.Fatalf("delegation = %+v, %v; want it failed", record, err)
		}
		if _, err := f.repo.DelegationEgressContext(f.ctx, child.ID); !errors.Is(err, ErrDelegationEgressNotPending) {
			t.Fatalf("context = %v, want not pending", err)
		}
	})
	t.Run("turn failed", func(t *testing.T) {
		f := newDelegationFixture(t)
		child := f.delegateNeedingConsent(t, "call_1", false)
		f.fail(t, f.parent.RunID, runoutcome.NormalizeFailure(runoutcome.OriginProviderTransport, "model_stream_failed", runoutcome.RetryClassNever))
		if _, err := f.repo.DelegationEgressContext(f.ctx, child.ID); !errors.Is(err, ErrDelegationEgressNotPending) {
			t.Fatalf("context = %v, want not pending", err)
		}
		if _, err := f.repo.DeclineDelegationEgress(f.ctx, child.ID); !errors.Is(err, ErrDelegationEgressNotPending) {
			t.Fatalf("decline = %v, want not pending", err)
		}
	})
	t.Run("conversation deleting", func(t *testing.T) {
		f := newDelegationFixture(t)
		child := f.delegateNeedingConsent(t, "call_1", false)
		context := f.egressContext(t, child.ID)
		if _, err := f.repo.BeginSessionDeletion(f.ctx, f.parent.SessionID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.repo.DelegationEgressContext(f.ctx, child.ID); !errors.Is(err, ErrDelegationNotFound) {
			t.Fatalf("context = %v, want not found", err)
		}
		if _, err := f.repo.GrantDelegationEgress(f.ctx, child.ID, consentFor(context, "nonce-1")); !errors.Is(err, ErrDelegationNotFound) {
			t.Fatalf("grant = %v, want not found", err)
		}
	})
}

// joinContent is the parent conversation's join message.
func (f delegationFixture) joinContent(t *testing.T) string {
	t.Helper()
	var content string
	if err := f.repo.db.QueryRowContext(f.ctx, `
		SELECT content FROM messages WHERE session_id = ? AND content_type = 'delegation_results'`,
		f.parent.SessionID).Scan(&content); err != nil {
		t.Fatal(err)
	}
	return content
}
