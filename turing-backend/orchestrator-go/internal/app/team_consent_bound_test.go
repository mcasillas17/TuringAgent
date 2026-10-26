package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/teamfiles"
)

// A task held for the user's consent outlives a restart, including one that
// turns the team off, and only the queue bound the recovery loop applies ends
// one nobody answers. So while one waits, the orchestrator refuses to start
// with either switched off.
func TestAnUnboundedQueueIsRefusedWhileATaskWaitsForConsent(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "turing.db")
	teamRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(teamRoot, "research"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(teamRoot, "research", "AGENT.md"),
		[]byte("---\nname: Research\ndescription: Looks things up\ntools: [system.time, github.get_issue]\n---\nYou research.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	holdATaskForConsent(t, databasePath, teamRoot)

	start := func(maxWaitMS, reaperMS int) (*App, error) {
		app, err := New(config.Config{
			ClientAPIKey: "client",
			RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
			ApprovalJWTSecret: "approval-secret",
			DatabasePath:      databasePath,
			LocalModel:        "llama3.2",
			TeamRoot:          teamRoot,
			QueueMaxWaitMS:    maxWaitMS, JobReaperIntervalMS: reaperMS,
		})
		if app != nil {
			t.Cleanup(app.Stop)
		}
		return app, err
	}
	if _, err := start(0, 60000); err == nil || !strings.Contains(err.Error(), "TURING_QUEUE_MAX_WAIT_MS") {
		t.Fatalf("start with no queue bound = %v, want a refusal naming TURING_QUEUE_MAX_WAIT_MS", err)
	}
	if _, err := start(3600000, 0); err == nil || !strings.Contains(err.Error(), "TURING_JOB_REAPER_INTERVAL_MS") {
		t.Fatalf("start with no recovery loop = %v, want a refusal naming TURING_JOB_REAPER_INTERVAL_MS", err)
	}
	if _, err := start(3600000, 60000); err != nil {
		t.Fatalf("start with both bounds = %v", err)
	}
}

// holdATaskForConsent leaves the database with one task Research was given
// that waits for the user's consent to a GitHub tool.
func holdATaskForConsent(t *testing.T, databasePath, teamRoot string) {
	t.Helper()
	ctx := context.Background()
	database, err := db.Open(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = database.Close() }()
	if err := db.ApplyMigrationsWithSkillsRoot(ctx, database, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	repo.SetTeamStore(teamfiles.New(teamRoot))
	if err := repo.UpsertTools(ctx, []repository.DiscoveredTool{{ServerName: "team", ToolName: "team.delegate", SchemaJSON: `{}`, Policy: "safe"}}); err != nil {
		t.Fatal(err)
	}
	profile, err := repo.SetAgentProfileEnabled(ctx, "research", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GrantAgentProfile(ctx, "research", profile.Revision); err != nil {
		t.Fatal(err)
	}
	session, err := repo.CreateSession(ctx, "Turing")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "prep me", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
		TeamRoster: []repository.TeamRosterEntry{{
			ProfileID: "research", Revision: profile.Revision, Name: "Research",
			Description: "Looks things up", Tools: []string{"system/system.time"},
		}},
		TeamContinuation: repository.TeamContinuation{Tools: []string{"system/system.time"}, ResultMaxBytes: 4096},
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "team-worker", 0, time.Hour,
		&repository.WorkerRoutingCapabilities{
			Models:            []repository.RoutingModelCapability{{Provider: "ollama", Model: "llama3.2", MaxContextTokens: 8192}},
			MaxConcurrentRuns: 1, TeamProtocolVersion: 1,
		}, func(repository.RoutingRequirements) bool { return true })
	if err != nil || claimed.RunID != parent.RunID {
		t.Fatalf("claim = %+v, %v; want the parent", claimed, err)
	}
	if _, err := repo.CreateDelegation(ctx, repository.CreateDelegationInput{
		ParentRunID: parent.RunID, AssignmentAttemptID: claimed.AssignmentAttemptID, ToolCallID: "call_1",
		ArgsHash: "sha256:call_1", Policy: "safe", MaxPerRun: 3,
		Profile: repository.AgentProfileSnapshot{
			ProfileID: "research", Revision: profile.Revision, DisplayName: "Research", Instructions: "You research.",
		},
		ModelProvider: "ollama", Model: "llama3.2", SelectedTools: []string{"system/system.time"},
		Brief: "BEGIN TURING_RETRIEVED_DELEGATION_BRIEF_x\nA brief.\nEND TURING_RETRIEVED_DELEGATION_BRIEF_x",
		Task:  "Gather the notes", EgressTools: []string{"integrations/github.get_issue"},
	}); err != nil {
		t.Fatal(err)
	}
}
