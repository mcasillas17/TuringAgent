package memory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

// recordedAudit is one redacted trail entry, kept whole so a test can assert
// what a refusal did and did not write down.
type recordedAudit struct {
	action  string
	target  string
	payload map[string]any
}

// recordingAudit stands in for the audit service when the thing under test is
// what reaches the trail rather than how it is stored.
type recordingAudit struct{ records []recordedAudit }

func (a *recordingAudit) Record(_ context.Context, _ string, _ string, _ string, action string, target string, payload map[string]any) error {
	a.records = append(a.records, recordedAudit{action: action, target: target, payload: payload})
	return nil
}

// RecordForExistingRun reports the row as inserted, because the runs these
// tests build are real and still there. What this fake is for is what the
// trail was told, not whether the correlated insert found its run.
func (a *recordingAudit) RecordForExistingRun(_ context.Context, _ string, _ string, _ string, action string, target string, payload map[string]any) (bool, error) {
	a.records = append(a.records, recordedAudit{action: action, target: target, payload: payload})
	return true, nil
}

// text renders everything this recorder has seen as one string, which is the
// shape a "this must appear nowhere" assertion actually needs.
func (a *recordingAudit) text() string {
	var builder strings.Builder
	for _, record := range a.records {
		fmt.Fprintf(&builder, "%s %s %v\n", record.action, record.target, record.payload)
	}
	return builder.String()
}

func newMemoryService(t *testing.T) (*Server, *repository.Repository, *memoryfiles.Vault, context.Context) {
	t.Helper()
	return newMemoryServiceAt(t, filepath.Join(t.TempDir(), "turing.db"), newVaultRoot(t), nil)
}

// newVaultRoot lays out an empty vault the way init.sh provisions one.
func newVaultRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{memoryfiles.InboxDirName, memoryfiles.BeliefsDirName} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatalf("prepare vault dir %q: %v", dir, err)
		}
	}
	return root
}

// newMemoryServiceAt builds the whole stack over paths the caller names, so a
// test can put a second one over the same database and vault — which is what
// a restart is.
func newMemoryServiceAt(
	t *testing.T,
	dbPath string,
	root string,
	recorder AuditRecorder,
) (*Server, *repository.Repository, *memoryfiles.Vault, context.Context) {
	t.Helper()
	service, repo, vault, _, callCtx := newMemoryServiceStack(t, dbPath, root, recorder)
	return service, repo, vault, callCtx
}

// newMemoryServiceStack is the same build with the database handed back, for
// the tests that have to put the store into a state no production path
// produces on purpose — a candidate file whose row is gone, for one.
func newMemoryServiceStack(
	t *testing.T,
	dbPath string,
	root string,
	recorder AuditRecorder,
) (*Server, *repository.Repository, *memoryfiles.Vault, *db.DB, context.Context) {
	t.Helper()
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	repo := repository.New(database)
	vault, err := memoryfiles.Open(root)
	if err != nil {
		t.Fatalf("open vault: %v", err)
	}
	repo.SetMemoryVault(vault)
	if recorder == nil {
		recorder = audit.New(repo)
	}
	server := New(repo, vault, recorder)
	// The native default, which is what app.New passes when nobody configured a
	// separate display root: the folder the client names is the folder the
	// orchestrator opened. Tests about the Docker case override it.
	server.SetMemoryDisplayRoot(vault.Root())
	return server, repo, vault, database, context.Background()
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
	setPolicyPerTool(t, repo, ctx, map[string]string{
		ToolSearch: policy, ToolRead: policy, ToolRemember: policy,
	})
}

// setPolicyPerTool gives each tool its own policy class, so a gate that is
// meant to hold whatever the policy says can be tested against a mixture
// rather than against three copies of one answer.
func setPolicyPerTool(t *testing.T, repo *repository.Repository, ctx context.Context, policies map[string]string) {
	t.Helper()
	tools := make([]string, 0, len(policies))
	for tool := range policies {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	discovered := make([]repository.DiscoveredTool, 0, len(tools))
	for _, tool := range tools {
		discovered = append(discovered, repository.DiscoveredTool{
			ServerName: ServerName, ToolName: tool, SchemaJSON: `{"type":"object"}`, Policy: policies[tool],
		})
	}
	if err := repo.UpsertTools(ctx, discovered); err != nil {
		t.Fatalf("UpsertTools: %v", err)
	}
	for _, tool := range tools {
		if err := repo.SetToolPolicyByName(ctx, ServerName, tool, policies[tool]); err != nil {
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
	note, err := repo.PromoteMemoryCandidate(ctx, repository.MemoryCandidateDecision{CandidateID: candidate.CandidateID})
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
