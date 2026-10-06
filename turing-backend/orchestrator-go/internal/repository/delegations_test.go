package repository

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/skillfiles"
)

const testBrief = "BEGIN TURING_RETRIEVED_DELEGATION_BRIEF_x\nA brief.\nTask:\nGather the notes\nEND TURING_RETRIEVED_DELEGATION_BRIEF_x"

type delegationFixture struct {
	repo    *Repository
	ctx     context.Context
	parent  EnqueueUserMessageResult
	attempt string
}

// newDelegationFixture is a Turing run offered Research, claimed by a
// team-protocol worker and running, with team.delegate registered as safe and
// Research enabled and granted at the roster's revision.
func newDelegationFixture(t *testing.T) delegationFixture {
	t.Helper()
	repo, ctx := newTitleTestRepo(t)
	if err := repo.UpsertTools(ctx, []DiscoveredTool{{ServerName: "team", ToolName: "team.delegate", SchemaJSON: `{}`, Policy: "safe"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.ExecContext(ctx, `
		INSERT INTO agent_profile_settings (profile_id, enabled, granted_revision, granted_at, updated_at)
		VALUES ('research', 1, 'rev-1', '2026-10-05T00:00:00Z', '2026-10-05T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	session, err := repo.CreateSession(ctx, "Turing")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "prep me", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2", TeamRoster: testRoster,
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "team-worker", 0, time.Hour,
		localWorkerAt(1), func(RoutingRequirements) bool { return true })
	if err != nil || claimed.RunID != parent.RunID {
		t.Fatalf("claim = %+v, %v; want the parent", claimed, err)
	}
	return delegationFixture{repo: repo, ctx: ctx, parent: parent, attempt: claimed.AssignmentAttemptID}
}

func (f delegationFixture) input(toolCallID string) CreateDelegationInput {
	return CreateDelegationInput{
		ParentRunID: f.parent.RunID, AssignmentAttemptID: f.attempt, ToolCallID: toolCallID,
		ArgsHash: "sha256:" + toolCallID, Policy: "safe", MaxPerRun: 3,
		Profile: AgentProfileSnapshot{
			ProfileID: "research", Revision: "rev-1", DisplayName: "Research", Emoji: "🔬",
			Instructions: "You are Research.", MaxToolCalls: 12,
		},
		ModelProvider: "ollama",
		Model:         "llama3.2",
		SelectedTools: []string{"files/files.read", "system/system.time"},
		Brief:         testBrief,
	}
}

func (f delegationFixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var count int
	if err := f.repo.db.QueryRowContext(f.ctx, query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// wantNothingCreated asserts a refused call left no child behind.
func (f delegationFixture) wantNothingCreated(t *testing.T) {
	t.Helper()
	if got := f.count(t, `SELECT COUNT(*) FROM delegations`); got != 0 {
		t.Fatalf("delegations = %d, want none", got)
	}
	if got := f.count(t, `SELECT COUNT(*) FROM sessions WHERE kind = 'delegation'`); got != 0 {
		t.Fatalf("child sessions = %d, want none", got)
	}
	if got := f.count(t, `SELECT COUNT(*) FROM agent_runs`); got != 1 {
		t.Fatalf("runs = %d, want the parent alone", got)
	}
	if got := f.count(t, `SELECT COUNT(*) FROM events WHERE type = 'delegation.started'`); got != 0 {
		t.Fatalf("delegation.started events = %d, want none", got)
	}
}

// One transaction writes the whole child: a hidden session whose first
// message is the brief, a queued specialist run anchored on it, its job, the
// delegation row and the parent's DELEGATION_STARTED.
func TestCreateDelegationWritesTheChildAndItsBrief(t *testing.T) {
	f := newDelegationFixture(t)
	created, err := f.repo.CreateDelegation(f.ctx, f.input("call_1"))
	if err != nil {
		t.Fatal(err)
	}
	if created.Replayed {
		t.Fatal("a first call reported a replay")
	}
	delegation := created.Delegation
	if !strings.HasPrefix(delegation.ID, "dlg_") || delegation.State != "queued" || delegation.ProfileID != "research" ||
		delegation.ParentRunID != f.parent.RunID || delegation.ParentSessionID != f.parent.SessionID {
		t.Fatalf("delegation = %+v", delegation)
	}

	var kind, parentSession, title, titleOrigin, sessionUpdatedAt string
	if err := f.repo.db.QueryRowContext(f.ctx, `
		SELECT kind, COALESCE(parent_session_id, ''), COALESCE(title, ''), title_origin, updated_at FROM sessions WHERE id = ?`,
		delegation.ChildSessionID).Scan(&kind, &parentSession, &title, &titleOrigin, &sessionUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if kind != "delegation" || parentSession != f.parent.SessionID || title != "🔬 Research" || titleOrigin != "explicit" {
		t.Fatalf("child session = %s/%s/%q/%s", kind, parentSession, title, titleOrigin)
	}

	type message struct{ id, role, contentType, content, runID, createdAt string }
	rows, err := f.repo.db.QueryContext(f.ctx, `
		SELECT id, role, content_type, content, COALESCE(run_id, ''), created_at FROM messages WHERE session_id = ? ORDER BY sequence`,
		delegation.ChildSessionID)
	if err != nil {
		t.Fatal(err)
	}
	var messages []message
	for rows.Next() {
		var m message
		if err := rows.Scan(&m.id, &m.role, &m.contentType, &m.content, &m.runID, &m.createdAt); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, m)
	}
	_ = rows.Close()
	if len(messages) != 2 ||
		messages[0].role != "user" || messages[0].contentType != backendegress.DelegationBriefContentType || messages[0].content != testBrief ||
		messages[1].role != "assistant" || messages[1].contentType != "text" || messages[1].content != "" || messages[1].runID != delegation.ChildRunID {
		t.Fatalf("child messages = %+v, want the brief then an empty assistant placeholder", messages)
	}
	if sessionUpdatedAt != messages[0].createdAt {
		t.Fatalf("child session updated_at = %s, want the brief's created_at %s", sessionUpdatedAt, messages[0].createdAt)
	}

	var userMessage, assistantMessage, status, provider, model string
	if err := f.repo.db.QueryRowContext(f.ctx, `
		SELECT user_message_id, assistant_message_id, status, model_provider, model_name FROM agent_runs WHERE id = ?`,
		delegation.ChildRunID).Scan(&userMessage, &assistantMessage, &status, &provider, &model); err != nil {
		t.Fatal(err)
	}
	if userMessage != messages[0].id || assistantMessage != messages[1].id || status != "queued" || provider != "ollama" || model != "llama3.2" {
		t.Fatalf("child run = %s/%s/%s/%s/%s", userMessage, assistantMessage, status, provider, model)
	}

	var payloadJSON string
	if err := f.repo.db.QueryRowContext(f.ctx, `SELECT payload_json FROM jobs WHERE run_id = ?`, delegation.ChildRunID).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	var payload queuedJobPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	wantTools := []string{"files/files.read", "system/system.time"}
	if payload.UserText != testBrief || !slices.Equal(payload.SelectedTools, wantTools) || !slices.Equal(payload.RequestedTools, wantTools) ||
		!payload.EnforceSelectedTools || !payload.SkipAutomaticRecall || payload.MinimumTeamProtocolVersion != 1 ||
		payload.EgressDecision != nil || payload.ExternalAgent != nil ||
		payload.PinnedPersona == nil || !payload.PinnedPersona.Withheld || payload.PinnedProfile == nil || !payload.PinnedProfile.Withheld ||
		payload.MemorySnapshotFingerprint == "" {
		t.Fatalf("child job payload = %s", payloadJSON)
	}
	if wantProfile := f.input("call_1").Profile; payload.AgentProfile == nil || *payload.AgentProfile != wantProfile {
		t.Fatalf("agentProfile = %+v", payload.AgentProfile)
	}
	if strings.Contains(payloadJSON, "teamRoster") {
		t.Fatal("a child job carries a roster")
	}

	var argsHash, revision string
	if err := f.repo.db.QueryRowContext(f.ctx, `
		SELECT args_hash, profile_revision FROM delegations WHERE parent_run_id = ? AND parent_tool_call_id = 'call_1'`,
		f.parent.RunID).Scan(&argsHash, &revision); err != nil {
		t.Fatal(err)
	}
	if argsHash != "sha256:call_1" || revision != "rev-1" {
		t.Fatalf("delegation row = %s/%s", argsHash, revision)
	}

	var started Event
	for _, event := range created.Events {
		if event.Type == "delegation.started" {
			started = event
		}
	}
	if started.SessionID != f.parent.SessionID || started.RunID.String != f.parent.RunID {
		t.Fatalf("started event = %+v, want it on the parent's stream", started)
	}
	var startedPayload map[string]any
	if err := json.Unmarshal([]byte(started.PayloadJSON), &startedPayload); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{
		"delegationId": delegation.ID, "parentRunId": f.parent.RunID, "childRunId": delegation.ChildRunID,
		"childSessionId": delegation.ChildSessionID, "profileId": "research", "displayName": "Research",
		"emoji": "🔬", "state": "queued", "summary": "Asked 🔬 Research",
	} {
		if startedPayload[key] != want {
			t.Fatalf("started payload %s = %v, want %v", key, startedPayload[key], want)
		}
	}
	if got := f.count(t, `SELECT COUNT(*) FROM events WHERE type = 'delegation.started' AND session_id = ?`, f.parent.SessionID); got != 1 {
		t.Fatalf("delegation.started on the parent = %d, want 1", got)
	}
	if got := f.count(t, `SELECT COUNT(*) FROM events WHERE type = 'agent.run.queued' AND session_id = ? AND run_id = ?`,
		delegation.ChildSessionID, delegation.ChildRunID); got != 1 {
		t.Fatalf("child queued events = %d, want 1", got)
	}
	if got := f.count(t, `SELECT COUNT(*) FROM events WHERE type = 'session.updated' AND session_id = ?`, delegation.ChildSessionID); got != 0 {
		t.Fatalf("child session.updated events = %d, want none", got)
	}
	sessions, err := f.repo.ListSessions(f.ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range sessions {
		if session.SessionID == delegation.ChildSessionID {
			t.Fatal("the child session is listed among the user's chats")
		}
	}
}

// The child gets only the enabled skills its profile names, and only those
// whose capabilities are granted.
func TestCreateDelegationFreezesOnlyTheProfilesSkills(t *testing.T) {
	f := newDelegationFixture(t)
	root := t.TempDir()
	f.repo.SetSkillStore(skillfiles.New(root))
	writeRepositorySkill(t, root, "research/notes", "Notes", "Takes notes", nil, "Take notes.")
	writeRepositorySkill(t, root, "research/locked", "Locked", "Needs a grant", []string{"files.update"}, "Withheld.")
	writeRepositorySkill(t, root, "writing/style", "Style", "Not research", nil, "Write well.")
	for _, id := range []string{"research/notes", "research/locked", "writing/style"} {
		enableRepositorySkill(t, f.repo, id)
	}
	input := f.input("call_1")
	input.SkillPatterns = []string{"research/*"}
	created, err := f.repo.CreateDelegation(f.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	var payloadJSON string
	if err := f.repo.db.QueryRowContext(f.ctx, `SELECT payload_json FROM jobs WHERE run_id = ?`, created.Delegation.ChildRunID).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	var payload queuedJobPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, skill := range payload.Skills {
		ids = append(ids, skill.SkillID)
	}
	if !slices.Equal(ids, []string{"research/notes"}) {
		t.Fatalf("child skills = %v, want research/notes alone", ids)
	}
}

// A profile that names no skills never reads the skill library, so a library
// it would not use cannot refuse its delegation.
func TestCreateDelegationWithoutSkillsSkipsTheLibrary(t *testing.T) {
	f := newDelegationFixture(t)
	notADirectory := filepath.Join(t.TempDir(), "skills")
	if err := os.WriteFile(notADirectory, []byte("not a skills root"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.repo.SetSkillStore(skillfiles.New(notADirectory))
	withSkills := f.input("call_skills")
	withSkills.SkillPatterns = []string{"research/*"}
	if _, err := f.repo.CreateDelegation(f.ctx, withSkills); err == nil {
		t.Fatal("an unreadable skill library did not refuse a profile that names skills")
	}
	created, err := f.repo.CreateDelegation(f.ctx, f.input("call_1"))
	if err != nil {
		t.Fatalf("a profile without skills was refused by the skill library: %v", err)
	}
	var payloadJSON string
	if err := f.repo.db.QueryRowContext(f.ctx, `SELECT payload_json FROM jobs WHERE run_id = ?`, created.Delegation.ChildRunID).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	var payload queuedJobPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Skills) != 0 {
		t.Fatalf("child skills = %+v, want none", payload.Skills)
	}
}

// A retried tool call returns the delegation it already created and creates
// nothing; the same ID with other arguments is refused.
func TestCreateDelegationReplaysOneToolCall(t *testing.T) {
	f := newDelegationFixture(t)
	first, err := f.repo.CreateDelegation(f.ctx, f.input("call_1"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.repo.CreateDelegation(f.ctx, f.input("call_1"))
	if err != nil {
		t.Fatal(err)
	}
	if !again.Replayed || again.Delegation != first.Delegation || len(again.Events) != 0 {
		t.Fatalf("replay = %+v, want the first delegation and no events", again)
	}
	found, ok, err := f.repo.DelegationForToolCall(f.ctx, f.parent.RunID, "call_1")
	if err != nil || !ok || found != first.Delegation {
		t.Fatalf("DelegationForToolCall = %+v, %v, %v", found, ok, err)
	}
	if got := f.count(t, `SELECT COUNT(*) FROM delegations`); got != 1 {
		t.Fatalf("delegations = %d, want 1", got)
	}
	mismatch := f.input("call_1")
	mismatch.ArgsHash = "sha256:other"
	if _, err := f.repo.CreateDelegation(f.ctx, mismatch); !errors.Is(err, ErrDelegationArgumentsChanged) {
		t.Fatalf("replay with other arguments error = %v, want ErrDelegationArgumentsChanged", err)
	}
	if _, ok, err := f.repo.DelegationForToolCall(f.ctx, f.parent.RunID, "call_2"); err != nil || ok {
		t.Fatalf("an unknown tool call was found: %v, %v", ok, err)
	}
}

// A delegation's state is its child run's status, with recovering shown as
// running.
func TestADelegationsStateIsItsChildRunsStatus(t *testing.T) {
	f := newDelegationFixture(t)
	created, err := f.repo.CreateDelegation(f.ctx, f.input("call_1"))
	if err != nil {
		t.Fatal(err)
	}
	for status, want := range map[string]string{"running": "running", "recovering": "running", "completed": "completed"} {
		f.exec(t, `UPDATE agent_runs SET status = ? WHERE id = ?`, status, created.Delegation.ChildRunID)
		found, ok, err := f.repo.DelegationForToolCall(f.ctx, f.parent.RunID, "call_1")
		if err != nil || !ok || found.State != want {
			t.Fatalf("child %s: state = %q, %v, %v; want %q", status, found.State, ok, err, want)
		}
	}
}

// Everything the database holds that an approval wait could outlast is read
// again inside the creating transaction, and any change refuses the call.
func TestCreateDelegationRechecksInsideTheTransaction(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(t *testing.T, f delegationFixture, input *CreateDelegationInput)
		want   error
	}{
		{"a stale assignment attempt", func(_ *testing.T, _ delegationFixture, input *CreateDelegationInput) {
			input.AssignmentAttemptID = "attempt_stale"
		}, ErrAssignmentFenced},
		{"a run fenced to recovering", func(t *testing.T, f delegationFixture, _ *CreateDelegationInput) {
			f.exec(t, `UPDATE agent_runs SET status = 'recovering' WHERE id = ?`, f.parent.RunID)
		}, ErrAssignmentFenced},
		{"a conversation being deleted", func(t *testing.T, f delegationFixture, _ *CreateDelegationInput) {
			f.exec(t, `UPDATE sessions SET deletion_state = 'deleting' WHERE id = ?`, f.parent.SessionID)
		}, ErrAssignmentFenced},
		{"a changed policy", func(t *testing.T, f delegationFixture, _ *CreateDelegationInput) {
			f.exec(t, `UPDATE tools SET policy = 'approval_required' WHERE server_name = 'team'`)
		}, ErrDelegationPolicyChanged},
		{"a disabled profile", func(t *testing.T, f delegationFixture, _ *CreateDelegationInput) {
			f.exec(t, `UPDATE agent_profile_settings SET enabled = 0`)
		}, ErrDelegationProfileChanged},
		{"a grant for another revision", func(t *testing.T, f delegationFixture, _ *CreateDelegationInput) {
			f.exec(t, `UPDATE agent_profile_settings SET granted_revision = 'rev-2'`)
		}, ErrDelegationProfileChanged},
		{"a server named team", func(t *testing.T, f delegationFixture, _ *CreateDelegationInput) {
			f.exec(t, `INSERT INTO mcp_servers (id, name, transport, url, tier, enabled, created_at)
				VALUES ('mcp_team', 'Team', 'http', 'http://team:9000/mcp', 'local_container', 1, '2026-10-05T00:00:00Z')`)
		}, ErrTeamNameCollision},
		{"the per-run cap", func(_ *testing.T, _ delegationFixture, input *CreateDelegationInput) {
			input.MaxPerRun = 0
		}, ErrDelegationCapReached},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newDelegationFixture(t)
			input := f.input("call_1")
			test.change(t, f, &input)
			if _, err := f.repo.CreateDelegation(f.ctx, input); !errors.Is(err, test.want) {
				t.Fatalf("CreateDelegation error = %v, want %v", err, test.want)
			}
			f.wantNothingCreated(t)
		})
	}
}

// The cap counts what this run already delegated, and a replay is not one.
func TestCreateDelegationCountsTheRunsDelegations(t *testing.T) {
	f := newDelegationFixture(t)
	input := f.input("call_1")
	input.MaxPerRun = 1
	if _, err := f.repo.CreateDelegation(f.ctx, input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.repo.CreateDelegation(f.ctx, input); err != nil {
		t.Fatalf("a replay at the cap was refused: %v", err)
	}
	second := f.input("call_2")
	second.MaxPerRun = 1
	if _, err := f.repo.CreateDelegation(f.ctx, second); !errors.Is(err, ErrDelegationCapReached) {
		t.Fatalf("a second delegation over the cap error = %v, want ErrDelegationCapReached", err)
	}
	if count, err := f.repo.DelegationCount(f.ctx, f.parent.RunID); err != nil || count != 1 {
		t.Fatalf("DelegationCount = %d, %v; want 1", count, err)
	}
}

// A specialist job is claimed only by a worker that honors the contract, and
// carries the brief as its live turn.
func TestADelegatedJobIsClaimedOnlyByATeamProtocolWorker(t *testing.T) {
	f := newDelegationFixture(t)
	created, err := f.repo.CreateDelegation(f.ctx, f.input("call_1"))
	if err != nil {
		t.Fatal(err)
	}
	worker := localWorkerAt(0)
	worker.Tools = []string{"files/files.read", "system/system.time"}
	worker.MaxConcurrentRuns = 2
	if claimed, err := f.repo.ClaimNextCompatibleJobWithLimit(f.ctx, "general_assistant", "old-worker", 0, time.Hour,
		worker, func(RoutingRequirements) bool { return true }); err != nil || claimed.JobID != "" {
		t.Fatalf("version-0 claim = %+v, %v; want nothing", claimed, err)
	}
	worker.TeamProtocolVersion = 1
	claimed, err := f.repo.ClaimNextCompatibleJobWithLimit(f.ctx, "general_assistant", "team-worker-2", 0, time.Hour,
		worker, func(RoutingRequirements) bool { return true })
	if err != nil || claimed.RunID != created.Delegation.ChildRunID {
		t.Fatalf("version-1 claim = %+v, %v; want the child", claimed, err)
	}
	if claimed.UserText != testBrief || claimed.SessionID != created.Delegation.ChildSessionID ||
		claimed.AgentProfile == nil || claimed.AgentProfile.ProfileID != "research" || !claimed.EnforceSelectedTools || !claimed.SkipAutomaticRecall {
		t.Fatalf("claimed child = %+v", claimed)
	}
}

func (f delegationFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := f.repo.db.ExecContext(f.ctx, query, args...); err != nil {
		t.Fatal(err)
	}
}
