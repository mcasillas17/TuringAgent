package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
	"google.golang.org/protobuf/types/known/structpb"
)

// countingNotifier stands in for the runtime's registry-change fan-out. The
// only thing this package promises about it is when it fires, so counting is
// the whole fake.
type countingNotifier struct{ calls int }

func (n *countingNotifier) NotifyMCPRegistryChanged(context.Context) error {
	n.calls++
	return nil
}

func newMemoryService(t *testing.T) (*Server, *repository.Repository, *memoryfiles.Vault, context.Context) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "turing.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	repo := repository.New(database)
	root := t.TempDir()
	for _, dir := range []string{memoryfiles.InboxDirName, memoryfiles.BeliefsDirName} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatalf("prepare vault dir %q: %v", dir, err)
		}
	}
	vault, err := memoryfiles.Open(root)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	repo.SetMemoryVault(vault)
	return New(repo, vault, audit.New(repo)), repo, vault, context.Background()
}

func newRun(t *testing.T, repo *repository.Repository, ctx context.Context) (string, string) {
	t.Helper()
	session, err := repo.CreateSession(ctx, "memory")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "remember something",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	return enqueued.RunID, session.SessionID
}

func newAutomationRun(t *testing.T, repo *repository.Repository, ctx context.Context) string {
	t.Helper()
	automation, err := repo.CreateAutomation(ctx, repository.AutomationInput{
		Name:         "Nightly digest",
		Prompt:       "Summarise the sandbox.",
		Schedule:     repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute},
		Enabled:      true,
		AllowedTools: []repository.AutomationTool{{ServerName: "files", ToolName: "files.read"}},
	})
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	due, err := time.Parse(time.RFC3339Nano, automation.NextDueAt)
	if err != nil {
		t.Fatalf("parse next due %q: %v", automation.NextDueAt, err)
	}
	fire, found, err := repo.ClaimDueAutomation(ctx, due, repository.AutomationRunDefaults{
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "qwen2.5:7b",
	})
	if err != nil || !found || fire.RunID == "" {
		t.Fatalf("ClaimDueAutomation = %+v found=%v err=%v", fire, found, err)
	}
	return fire.RunID
}

func setPolicies(t *testing.T, repo *repository.Repository, ctx context.Context, policy string) {
	t.Helper()
	discovered := make([]repository.DiscoveredTool, 0, 3)
	for _, tool := range []string{ToolSearch, ToolRead, ToolRemember} {
		discovered = append(discovered, repository.DiscoveredTool{
			ServerName: ServerName, ToolName: tool, SchemaJSON: `{"type":"object"}`, Policy: policy,
		})
	}
	if err := repo.UpsertTools(ctx, discovered); err != nil {
		t.Fatalf("UpsertTools: %v", err)
	}
	for _, tool := range []string{ToolSearch, ToolRead, ToolRemember} {
		if err := repo.SetToolPolicyByName(ctx, ServerName, tool, policy); err != nil {
			t.Fatalf("SetToolPolicyByName(%s): %v", tool, err)
		}
	}
}

func callArgs(t *testing.T, values map[string]any) *structpb.Struct {
	t.Helper()
	args, err := structpb.NewStruct(values)
	if err != nil {
		t.Fatalf("build args: %v", err)
	}
	return args
}

func mustPromoteBelief(t *testing.T, repo *repository.Repository, ctx context.Context, sessionID, title, body string) repository.MemoryNote {
	t.Helper()
	candidate, err := repo.CreateMemoryCandidate(ctx, repository.CreateMemoryCandidateInput{
		SessionID: sessionID, Kind: repository.MemoryCandidateKindBelief, Title: title, Body: body,
	})
	if err != nil {
		t.Fatalf("CreateMemoryCandidate: %v", err)
	}
	note, err := repo.PromoteMemoryCandidate(ctx, candidate.CandidateID)
	if err != nil {
		t.Fatalf("PromoteMemoryCandidate: %v", err)
	}
	return note
}

func frameBody(t *testing.T, result *structpb.Struct) string {
	t.Helper()
	if result == nil {
		t.Fatal("dispatch returned no result")
	}
	content, ok := result.AsMap()["content"].(string)
	if !ok {
		t.Fatalf("result has no framed content: %v", result.AsMap())
	}
	if !strings.HasPrefix(content, "BEGIN TURING_RETRIEVED_MEMORY_") {
		t.Fatalf("result was not framed: %q", content)
	}
	return content
}

func frameMarker(t *testing.T, framed string) string {
	t.Helper()
	return strings.SplitN(strings.TrimPrefix(framed, "BEGIN "), "\n", 2)[0]
}

func toolNames(response *turingv1.ListMemoryToolsResponse) []string {
	names := make([]string, 0, len(response.GetTools()))
	for _, tool := range response.GetTools() {
		names = append(names, tool.GetToolName())
	}
	return names
}
