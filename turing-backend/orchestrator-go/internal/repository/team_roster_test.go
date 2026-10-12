package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"
)

var testRoster = []TeamRosterEntry{{
	ProfileID: "research", Revision: "rev-1", Name: "Research", Emoji: "🔬",
	Description: "Looks things up", Tools: []string{"files/files.read", "system/system.time"},
}}

// jobPayloadKeys decodes a job's payload into its top-level keys.
func jobPayloadKeys(t *testing.T, ctx context.Context, repo *Repository, jobID string) map[string]json.RawMessage {
	t.Helper()
	var raw string
	if err := repo.db.QueryRowContext(ctx, `SELECT payload_json FROM jobs WHERE id = ?`, jobID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	keys := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(raw), &keys); err != nil {
		t.Fatal(err)
	}
	return keys
}

func wantNoRosterKeys(t *testing.T, keys map[string]json.RawMessage) {
	t.Helper()
	if _, ok := keys["teamRoster"]; ok {
		t.Fatalf("payload carries teamRoster %s, want none", keys["teamRoster"])
	}
	if _, ok := keys["minimumTeamProtocolVersion"]; ok {
		t.Fatalf("payload carries minimumTeamProtocolVersion %s, want none", keys["minimumTeamProtocolVersion"])
	}
	if _, ok := keys["teamContinuation"]; ok {
		t.Fatalf("payload carries teamContinuation %s, want none", keys["teamContinuation"])
	}
}

// testContinuation includes a memory tool, so a continuation's memory
// fingerprint differs from its parent's, which selected none.
var testContinuation = TeamContinuation{
	Tools: []string{"files/files.read", "memory/memory.search", "system/system.time"}, ResultMaxBytes: 4096,
}

// What a parent's continuation may use is frozen with the parent's roster,
// so the join, inside whichever transaction finishes the last task, reads
// nothing but the database.
func TestAContinuationIsFrozenWithTheRoster(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Delegating")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "prep me", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2", TeamRoster: testRoster, TeamContinuation: testContinuation,
	})
	if err != nil {
		t.Fatal(err)
	}
	var persisted TeamContinuation
	keys := jobPayloadKeys(t, ctx, repo, enqueued.JobID)
	if err := json.Unmarshal(keys["teamContinuation"], &persisted); err != nil || !reflect.DeepEqual(persisted, testContinuation) {
		t.Fatalf("teamContinuation = %s (%v), want %+v", keys["teamContinuation"], err, testContinuation)
	}
}

// A continuation never gains a tool its parent lacked: when a decision froze
// the parent's tools, the continuation's are cut to that set.
func TestAContinuationKeepsOnlyToolsItsParentsFrozenSetHolds(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Delegating")
	if err != nil {
		t.Fatal(err)
	}
	decision := localModelRemoteToolDecision(t)
	decision.SelectedTools = append(slices.Clone(decision.SelectedTools), TeamDelegateTool)
	frozen := decision.SelectedTools[0]
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hi", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: decision.Model,
		EgressDecision: decision, SelectedTools: decision.SelectedTools,
		TeamRoster:       testRoster,
		TeamContinuation: TeamContinuation{Tools: []string{frozen, "system/system.time"}, ResultMaxBytes: 4096},
	})
	if err != nil {
		t.Fatal(err)
	}
	var persisted TeamContinuation
	if err := json.Unmarshal(jobPayloadKeys(t, ctx, repo, enqueued.JobID)["teamContinuation"], &persisted); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(persisted.Tools, []string{frozen}) || persisted.ResultMaxBytes != 4096 {
		t.Fatalf("teamContinuation = %+v, want only %s, the one the frozen set holds", persisted, frozen)
	}
}

// A roster is frozen onto the job together with the minimum that keeps an
// older worker from claiming it: that worker would run the parent without
// the team. Enqueue checks the same route the claim will enforce.
func TestARosterIsFrozenOntoTheJobWithItsMinimum(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Delegating")
	if err != nil {
		t.Fatal(err)
	}
	var validated []RoutingRequirements
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "prep me", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2", TeamRoster: testRoster,
		ValidateRouting: func(_ context.Context, route RoutingRequirements) error {
			validated = append(validated, route)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	keys := jobPayloadKeys(t, ctx, repo, enqueued.JobID)
	var persisted []TeamRosterEntry
	if err := json.Unmarshal(keys["teamRoster"], &persisted); err != nil || !reflect.DeepEqual(persisted, testRoster) {
		t.Fatalf("teamRoster = %s (%v), want %+v", keys["teamRoster"], err, testRoster)
	}
	if string(keys["minimumTeamProtocolVersion"]) != "1" {
		t.Fatalf("minimumTeamProtocolVersion = %s, want 1", keys["minimumTeamProtocolVersion"])
	}
	if len(validated) != 1 || validated[0].MinimumTeamProtocolVersion != 1 {
		t.Fatalf("validated routes = %+v, want one at team protocol 1", validated)
	}
	roster, err := repo.RunTeamRoster(ctx, enqueued.RunID)
	if err != nil || !reflect.DeepEqual(roster, testRoster) {
		t.Fatalf("RunTeamRoster = %+v, %v; want %+v", roster, err, testRoster)
	}

	if claimed, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "old-worker", 0, time.Hour,
		localWorkerAt(0), func(RoutingRequirements) bool { return true }); err != nil || claimed.JobID != "" {
		t.Fatalf("version-0 claim = %+v, %v; want nothing", claimed, err)
	}
	claimed, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "team-worker", 0, time.Hour,
		localWorkerAt(1), func(RoutingRequirements) bool { return true })
	if err != nil || claimed.JobID != enqueued.JobID || claimed.MinimumTeamProtocolVersion != 1 {
		t.Fatalf("version-1 claim = %+v, %v; want the job at minimum 1", claimed, err)
	}
}

// The team is optional. When the last team-protocol worker leaves between the
// roster check and the enqueue, the turn is enqueued exactly as it would have
// been without the team, rather than refused for a minimum the team added.
func TestARosterIsDroppedWhenNoTeamProtocolWorkerIsLeft(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Delegating")
	if err != nil {
		t.Fatal(err)
	}
	refusal := errors.New("no connected worker supports team protocol v1")
	var validated []RoutingRequirements
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "prep me", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2", TeamRoster: testRoster, TeamContinuation: testContinuation,
		ValidateRouting: func(_ context.Context, route RoutingRequirements) error {
			validated = append(validated, route)
			if route.MinimumTeamProtocolVersion > 0 {
				return refusal
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("enqueue = %v, want the turn enqueued without the team", err)
	}
	wantNoRosterKeys(t, jobPayloadKeys(t, ctx, repo, enqueued.JobID))
	if len(validated) != 2 || validated[1].MinimumTeamProtocolVersion != 0 {
		t.Fatalf("validated routes = %+v, want the team route then the plain one", validated)
	}

	// A route nothing serves is still refused, with the plain route's answer.
	_, err = repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "prep me again", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2", TeamRoster: testRoster,
		ValidateRouting: func(context.Context, RoutingRequirements) error { return refusal },
	})
	if !errors.Is(err, refusal) {
		t.Fatalf("enqueue on an unserved route = %v, want the routing refusal", err)
	}
}

// Every run that is not offered the team is enqueued exactly as before.
func TestAJobWithoutARosterCarriesNeitherKey(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	enqueued := enqueueLocalTurn(t, ctx, repo, "Ordinary")

	wantNoRosterKeys(t, jobPayloadKeys(t, ctx, repo, enqueued.JobID))
	roster, err := repo.RunTeamRoster(ctx, enqueued.RunID)
	if err != nil || len(roster) != 0 {
		t.Fatalf("RunTeamRoster = %+v, %v; want empty", roster, err)
	}
	if _, err := repo.RunTeamRoster(ctx, "run_missing"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("RunTeamRoster(missing) error = %v, want sql.ErrNoRows", err)
	}
}

// The writer refuses to freeze a roster onto a run that could never
// delegate: one whose model is remote or an external agent, or one whose
// frozen tools leave team.delegate out. A decision that includes it keeps the
// roster.
func TestARosterIsDroppedWhereTheRunCannotDelegate(t *testing.T) {
	withTeam := func(decision *PendingEgressDecision) *PendingEgressDecision {
		decision.SelectedTools = append(slices.Clone(decision.SelectedTools), TeamDelegateTool)
		return decision
	}
	for _, test := range []struct {
		name  string
		input func(t *testing.T, repo *Repository, ctx context.Context, sessionID string) EnqueueUserMessageInput
		keep  bool
	}{
		{"a remote model", func(_ *testing.T, _ *Repository, _ context.Context, sessionID string) EnqueueUserMessageInput {
			decision := withTeam(remoteDecision())
			return EnqueueUserMessageInput{
				SessionID: sessionID, Content: "hi", AgentID: "general_assistant",
				ModelProvider: "openai_compatible", Model: "gpt-5-mini",
				EgressDecision: decision, SelectedTools: decision.SelectedTools,
			}
		}, false},
		{"an external agent", func(t *testing.T, repo *Repository, ctx context.Context, sessionID string) EnqueueUserMessageInput {
			agent := mustCreateAgent(t, ctx, repo, anthropicAgent())
			if _, err := repo.SetSessionAgent(ctx, sessionID, agent.AgentID); err != nil {
				t.Fatal(err)
			}
			decision := withTeam(testRemoteEgressDecision(t, agent.Model, agent.BaseURL, agent.AgentID, agent.CredentialRef))
			return EnqueueUserMessageInput{
				SessionID: sessionID, Content: "hi", AgentID: "general_assistant",
				ModelProvider: "ollama", Model: "qwen2.5:7b",
				EgressDecision: decision, SelectedTools: decision.SelectedTools,
			}
		}, false},
		{"a frozen set without team.delegate", func(t *testing.T, _ *Repository, _ context.Context, sessionID string) EnqueueUserMessageInput {
			decision := localModelRemoteToolDecision(t)
			return EnqueueUserMessageInput{
				SessionID: sessionID, Content: "hi", AgentID: "general_assistant",
				ModelProvider: "ollama", Model: decision.Model,
				EgressDecision: decision, SelectedTools: decision.SelectedTools,
			}
		}, false},
		{"a frozen set with team.delegate", func(t *testing.T, _ *Repository, _ context.Context, sessionID string) EnqueueUserMessageInput {
			decision := withTeam(localModelRemoteToolDecision(t))
			return EnqueueUserMessageInput{
				SessionID: sessionID, Content: "hi", AgentID: "general_assistant",
				ModelProvider: "ollama", Model: decision.Model,
				EgressDecision: decision, SelectedTools: decision.SelectedTools,
			}
		}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, ctx := newTitleTestRepo(t)
			session, err := repo.CreateSession(ctx, test.name)
			if err != nil {
				t.Fatal(err)
			}
			input := test.input(t, repo, ctx, session.SessionID)
			input.TeamRoster = testRoster
			input.TeamContinuation = testContinuation
			enqueued, err := repo.EnqueueUserMessage(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			keys := jobPayloadKeys(t, ctx, repo, enqueued.JobID)
			if !test.keep {
				wantNoRosterKeys(t, keys)
				return
			}
			if _, ok := keys["teamRoster"]; !ok || string(keys["minimumTeamProtocolVersion"]) != "1" {
				t.Fatalf("payload keys = %v, want the roster and its minimum kept", keys)
			}
		})
	}
}
