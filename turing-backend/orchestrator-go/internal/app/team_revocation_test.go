package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"
)

const researchProfile = "---\nname: Research\ndescription: Looks things up\ntools: [system.time]\nmemory: none\n---\nYou research.\n"

// controlledMaintenanceTicks replaces the maintenance goroutine's clock for
// one test: it records the interval the app asked for and ticks only when
// the test says so, so a one-minute tick need not be waited out.
type controlledMaintenanceTicks struct {
	mu       sync.Mutex
	interval time.Duration
	ticks    chan time.Time
}

func controlMaintenanceTicks(t *testing.T) *controlledMaintenanceTicks {
	t.Helper()
	controlled := &controlledMaintenanceTicks{ticks: make(chan time.Time)}
	original := maintenanceTicks
	maintenanceTicks = func(interval time.Duration) (<-chan time.Time, func()) {
		controlled.mu.Lock()
		controlled.interval = interval
		controlled.mu.Unlock()
		return controlled.ticks, func() {}
	}
	t.Cleanup(func() { maintenanceTicks = original })
	return controlled
}

func (c *controlledMaintenanceTicks) requested() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.interval
}

func newRevocationTestApp(t *testing.T, reaperIntervalMS int) (*App, string) {
	t.Helper()
	teamRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(teamRoot, "research"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(teamRoot, "research", "AGENT.md"), []byte(researchProfile), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := New(config.Config{
		ClientAPIKey: "client",
		RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
		ApprovalJWTSecret:   "approval-secret",
		DatabasePath:        t.TempDir() + "/turing.db",
		LocalModel:          "llama3.2",
		TeamRoot:            teamRoot,
		QueueMaxWaitMS:      3600000,
		JobReaperIntervalMS: reaperIntervalMS,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Stop)
	return app, teamRoot
}

// seedResearchTasks enables and grants Research, then has each of parents
// Turing turns delegate perParent tasks to it and complete; every other task
// waits for the user's consent to a GitHub tool. It returns the turns.
func seedResearchTasks(t *testing.T, app *App, parents, perParent int) []repository.EnqueueUserMessageResult {
	t.Helper()
	ctx := context.Background()
	repo := app.Repository
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
	var turns []repository.EnqueueUserMessageResult
	for range parents {
		session, err := repo.CreateSession(ctx, "Turing")
		if err != nil {
			t.Fatal(err)
		}
		parent, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
			SessionID: session.SessionID, Content: "prep me", AgentID: "general_assistant",
			ModelProvider: "ollama", Model: "llama3.2",
			TeamRoster:       []repository.TeamRosterEntry{{ProfileID: "research", Revision: profile.Revision, Name: "Research"}},
			TeamContinuation: repository.TeamContinuation{Tools: []string{}, ResultMaxBytes: 256},
		})
		if err != nil {
			t.Fatal(err)
		}
		claimed, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "seed-worker", 0, time.Hour,
			&repository.WorkerRoutingCapabilities{
				Models:            []repository.RoutingModelCapability{{Provider: "ollama", Model: "llama3.2", MaxContextTokens: 8192}},
				MaxConcurrentRuns: 1, TeamProtocolVersion: 1,
			}, func(route repository.RoutingRequirements) bool { return len(route.SelectedTools) == 0 })
		if err != nil || claimed.RunID != parent.RunID {
			t.Fatalf("claim = %+v, %v; want the turn %s", claimed, err, parent.RunID)
		}
		for index := range perParent {
			input := repository.CreateDelegationInput{
				ParentRunID: parent.RunID, AssignmentAttemptID: claimed.AssignmentAttemptID,
				ToolCallID: fmt.Sprintf("call_%d", index), ArgsHash: fmt.Sprintf("sha256:%d", index),
				Policy: "safe", MaxPerRun: perParent,
				Profile: repository.AgentProfileSnapshot{
					ProfileID: "research", Revision: profile.Revision, DisplayName: "Research", Instructions: "You research.",
				},
				ModelProvider: "ollama", Model: "llama3.2", SelectedTools: []string{"system/system.time"},
				Brief: "BEGIN TURING_RETRIEVED_DELEGATION_BRIEF_x\nTask:\nGather\nEND TURING_RETRIEVED_DELEGATION_BRIEF_x",
				Task:  "Gather",
			}
			if index%2 == 1 {
				input.EgressTools = []string{"integrations/github.get_issue"}
			}
			if _, err := repo.CreateDelegation(ctx, input); err != nil {
				t.Fatal(err)
			}
		}
		state, err := repo.GetRunState(ctx, parent.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := repo.CompleteRunCanonical(ctx, repository.CompleteRunInput{
			RunID: parent.RunID, AssistantMessageID: parent.AssistantMessageID, Content: "Asked Research.",
			ExpectedStateVersion: state.StateVersion,
		}); err != nil {
			t.Fatal(err)
		}
		turns = append(turns, parent)
	}
	return turns
}

func (a *App) countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var count int
	if err := a.database.QueryRowContext(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// The maintenance goroutine runs whether or not the job reaper does, at the
// reaper's interval capped at a minute, and its tick withdraws every task of a
// disabled specialist, joining each turn exactly once.
func TestTheMaintenanceTickWithdrawsRevokedTasksWithOrWithoutTheReaper(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reaperMS int
		interval time.Duration
	}{
		{name: "reaper off", reaperMS: 0, interval: time.Minute},
		{name: "reaper faster than a minute", reaperMS: 20, interval: 20 * time.Millisecond},
		{name: "reaper slower than a minute", reaperMS: 600000, interval: time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ticks := controlMaintenanceTicks(t)
			app, _ := newRevocationTestApp(t, tc.reaperMS)
			if got := ticks.requested(); got != tc.interval {
				t.Fatalf("maintenance interval = %v, want %v", got, tc.interval)
			}
			turns := seedResearchTasks(t, app, 3, 50)
			if _, err := app.Repository.SetAgentProfileEnabled(context.Background(), "research", false); err != nil {
				t.Fatal(err)
			}

			ticks.ticks <- time.Now()

			waitFor(t, "every task to be withdrawn", func() bool {
				return app.countRows(t, `SELECT COUNT(*) FROM agent_runs WHERE cancellation_reason = ?`,
					runoutcome.CodeProfileGrantRevoked) == 150
			})
			for _, turn := range turns {
				waitFor(t, "the turn to be joined", func() bool {
					return app.countRows(t, `SELECT COUNT(*) FROM agent_runs WHERE continues_run_id = ?`, turn.RunID) == 1
				})
				if joins := app.countRows(t, `SELECT COUNT(*) FROM messages WHERE session_id = ? AND content_type = 'delegation_results'`,
					turn.SessionID); joins != 1 {
					t.Fatalf("turn %s joined %d times, want once", turn.RunID, joins)
				}
			}
		})
	}
}

// The runtime checks each task's profile file through the team resolver
// between claim and send: a deleted file or an edited authority field
// withdraws the task before any worker is sent it, so the first run the
// worker gets is the continuation its withdrawal joined; an unchanged
// profile's task is sent.
func TestATaskWhoseProfileFileChangedNeverReachesAWorker(t *testing.T) {
	for _, tc := range []struct {
		name      string
		change    func(t *testing.T, teamRoot string)
		withdrawn bool
	}{
		{name: "file deleted", withdrawn: true, change: func(t *testing.T, teamRoot string) {
			if err := os.RemoveAll(filepath.Join(teamRoot, "research")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "authority field edited", withdrawn: true, change: func(t *testing.T, teamRoot string) {
			edited := "---\nname: Research\ndescription: Looks things up\ntools: [system.time]\nmemory: none\nmax_tool_calls: 5\n---\nYou research.\n"
			if err := os.WriteFile(filepath.Join(teamRoot, "research", "AGENT.md"), []byte(edited), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unchanged", change: func(*testing.T, string) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			controlMaintenanceTicks(t)
			app, teamRoot := newRevocationTestApp(t, 0)
			turns := seedResearchTasks(t, app, 1, 1)
			var childRunID string
			if err := app.database.QueryRowContext(context.Background(),
				`SELECT child_run_id FROM delegations WHERE parent_run_id = ?`, turns[0].RunID).Scan(&childRunID); err != nil {
				t.Fatal(err)
			}
			tc.change(t, teamRoot)

			client := turingv1.NewRuntimeServiceClient(newBufconnClient(t, app.InternalServer))
			ctx, cancel := context.WithCancel(metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer internal")))
			defer cancel()
			stream, err := client.ConnectWorker(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{
				WorkerId: "team-worker", RegistrationId: "registration-team",
				Capabilities: &turingv1.WorkerCapabilities{
					Models: []*turingv1.ModelCapability{{
						Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "llama3.2", MaxContextTokens: 8192,
					}},
					AgentIds:            []turingv1.AgentId{turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT},
					MaxConcurrentRuns:   1,
					TeamProtocolVersion: 1,
					Tools: []*turingv1.DiscoveredTool{{
						ServerName: "system", ToolName: "system.time", Schema: &structpb.Struct{},
					}},
				},
			}}}); err != nil {
				t.Fatal(err)
			}
			assigned := recvRuntimeCommand(t, stream, func(command *turingv1.RuntimeCommand) bool {
				return command.GetRunAssigned() != nil
			}).GetRunAssigned()

			child, err := app.Repository.GetRun(context.Background(), childRunID)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.withdrawn {
				if assigned.GetRunId() != childRunID {
					t.Fatalf("assigned %s, want the task %s", assigned.GetRunId(), childRunID)
				}
				return
			}
			if assigned.GetRunId() == childRunID || assigned.GetSessionId() != turns[0].SessionID {
				t.Fatalf("assigned %s in %s, want the turn's continuation, never the task", assigned.GetRunId(), assigned.GetSessionId())
			}
			if child.Status != "cancelled" || child.CancellationReason != runoutcome.CodeProfileGrantRevoked || child.ExecutionActive {
				t.Fatalf("task = %+v; want it withdrawn profile_grant_revoked, its execution released", child)
			}
		})
	}
}
