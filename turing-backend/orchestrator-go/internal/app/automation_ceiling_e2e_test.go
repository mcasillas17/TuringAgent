package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	runtimetestkit "github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/testkit"
	"github.com/mcasillas17/TuringAgent/turing-backend/mcpwire"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	automationsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/automations"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/structpb"
)

// TUR-026d end to end, with a real worker: an automation's allowlist bounds
// every tool its run calls. The automation fires through the scheduler, the
// runtime offers the model every tool it serves (the job carries no enforced
// selection), and the orchestrator's beacon decides.

type ceilingToolCall struct {
	name string
	args map[string]any
}

// ceilingModel answers /api/chat as a local model would: it makes its calls
// one per turn, then answers once every call has a result.
type ceilingModel struct {
	mu       sync.Mutex
	calls    []ceilingToolCall
	requests []string
}

func (m *ceilingModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	request := string(body)
	m.mu.Lock()
	m.requests = append(m.requests, request)
	calls := m.calls
	m.mu.Unlock()
	w.Header().Set("Content-Type", "application/x-ndjson")
	if turn := strings.Count(request, `"role":"tool"`); turn < len(calls) {
		args, err := json.Marshal(calls[turn].args)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprintf(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":%q,"arguments":%s}}]},"done":true,"done_reason":"tool_calls"}`+"\n",
			calls[turn].name, args)
		return
	}
	_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"Done."},"done":true,"done_reason":"stop"}`+"\n")
}

func (m *ceilingModel) seen() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.requests...)
}

// fakeFilesServer is the bundled files server as the runtime reaches it: an
// MCP endpoint serving files.read and files.list, recording every tool call.
type fakeFilesServer struct {
	server *httptest.Server
	mu     sync.Mutex
	called []string
}

func newFakeFilesServer(t *testing.T) *fakeFilesServer {
	t.Helper()
	fake := &fakeFilesServer{}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.handle))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeFilesServer) handle(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(request.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	var result any
	switch request.Method {
	case "initialize":
		result = map[string]any{
			"protocolVersion": mcpwire.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "fake-files", "version": "1.0.0"},
		}
	case "tools/list":
		schema := map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}}}
		result = map[string]any{"tools": []any{
			map[string]any{"name": "files.read", "description": "Read a file.", "inputSchema": schema},
			map[string]any{"name": "files.list", "description": "List files.", "inputSchema": schema},
		}}
	case "tools/call":
		f.mu.Lock()
		f.called = append(f.called, request.Params.Name)
		f.mu.Unlock()
		result = map[string]any{"content": "the note says hello"}
	default:
		result = map[string]any{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
}

func (f *fakeFilesServer) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.called...)
}

// recordingTransport records every request the GitHub client would send, and
// sends none.
type recordingTransport struct{ requests atomic.Int32 }

func (r *recordingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	r.requests.Add(1)
	return nil, errors.New("no request may reach GitHub in this test")
}

type ceilingE2E struct {
	app    *App
	model  *ceilingModel
	files  *fakeFilesServer
	github *recordingTransport
	// vendorRequests counts requests to the registered third-party server.
	vendorRequests atomic.Int32
	publicConn     turingv1.IntegrationServiceClient
	publicCtx      context.Context
}

const ceilingSkillBody = "Write the digest in three lines."

// ceilingVaultSentinel is in a belief memory.search would find.
const ceilingVaultSentinel = "TUR026D-VAULT-SENTINEL"

// newCeilingE2E starts the app with a memory vault, a granted skill and a
// GitHub connection available, a fake local model and files server, a
// registered third-party server with a safe tool, and a real worker.
func newCeilingE2E(t *testing.T, calls ...ceilingToolCall) *ceilingE2E {
	t.Helper()
	vault := t.TempDir()
	for _, dir := range []string{memoryfiles.InboxDirName, memoryfiles.BeliefsDirName} {
		if err := os.MkdirAll(filepath.Join(vault, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(vault, memoryfiles.BeliefsDirName, "plans.md"),
		[]byte("# Plans\n\nThe user's plans: "+ceilingVaultSentinel+".\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	skills := t.TempDir()
	skillPath := filepath.Join(skills, "writing", "digest", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("---\nname: Digest\ndescription: How to write the digest\n---\n"+ceilingSkillBody+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	app, err := New(config.Config{
		ClientAPIKey: "client",
		RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
		ApprovalJWTSecret: "approval-secret",
		DatabasePath:      t.TempDir() + "/turing.db",
		LocalModel:        "llama3.2",
		MemoryRoot:        vault,
		SkillsRoot:        skills,
		IntegrationKey:    strings.Repeat("ab", 32),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Stop)
	h := &ceilingE2E{app: app, model: &ceilingModel{calls: calls}, files: newFakeFilesServer(t), github: &recordingTransport{}}
	app.IntegrationService.SetHTTPClient(&http.Client{Transport: h.github})
	ctx := context.Background()
	if _, err := app.Repository.SetSkillEnabled(ctx, "writing/digest", true); err != nil {
		t.Fatal(err)
	}

	vendor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		h.vendorRequests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(vendor.Close)
	registered, err := app.Repository.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "vendor", URL: vendor.URL + "/mcp", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Repository.SetMCPServerEnabled(ctx, registered.Server.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := app.Repository.ReplaceMCPServerTools(ctx, registered.Server.ID, []repository.MCPServerTool{
		{Name: "vendor.lookup", Policy: "safe", SchemaJSON: `{"type":"object","properties":{"query":{"type":"string"}}}`},
	}); err != nil {
		t.Fatal(err)
	}

	publicConn := newBufconnClient(t, app.PublicServer)
	h.publicConn = turingv1.NewIntegrationServiceClient(publicConn)
	h.publicCtx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer client")
	if _, err := h.publicConn.ConnectAccount(h.publicCtx, &turingv1.ConnectAccountRequest{
		Provider: turingv1.IntegrationProvider_INTEGRATION_PROVIDER_GITHUB, DisplayName: "Work GitHub",
		Credential: "ghp_test", ConsentAcknowledged: true,
	}); err != nil {
		t.Fatal(err)
	}

	model := httptest.NewServer(h.model)
	t.Cleanup(model.Close)
	internalConn := newBufconnClient(t, app.InternalServer)
	workerCtx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- runtimetestkit.RunWorker(workerCtx, runtimetestkit.WorkerConfig{
			Conn: internalConn, RuntimeToken: "internal", WorkerID: "ceiling-runtime",
			MaxConcurrentRuns: 1, MaxToolCallsPerRun: 10,
			OpenAIBaseURL: model.URL, OpenAIModel: "unused",
			OllamaBaseURL: model.URL, LocalModel: "llama3.2",
			MCPFilesBaseURL: h.files.server.URL, MCPFilesToken: "files-token",
		})
	}()
	t.Cleanup(func() {
		stopWorker()
		<-workerDone
	})
	waitFor(t, "the runtime worker to serve the local model with files", func() bool {
		return app.RuntimeService.ValidateRouting(ctx, repository.RoutingRequirements{
			AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
			RequestedTools: []string{"files/files.read"},
		}) == nil
	})
	return h
}

// fire creates an automation with the given allowlist and fires it through
// the scheduler, as its schedule would, returning its run once it ends.
func (h *ceilingE2E) fire(t *testing.T, allowed ...repository.AutomationTool) (repository.Automation, repository.Run) {
	t.Helper()
	ctx := context.Background()
	automation, err := h.app.Repository.CreateAutomation(ctx, repository.AutomationInput{
		Name: "Nightly digest", Prompt: "Write the nightly digest.",
		Schedule: repository.Schedule{Kind: repository.ScheduleInterval, Interval: time.Hour},
		Enabled:  true, AllowedTools: allowed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.app.database.ExecContext(ctx, `UPDATE automations SET next_due_at = ? WHERE id = ?`,
		repository.FormatTimestamp(time.Now().Add(-time.Second)), automation.AutomationID); err != nil {
		t.Fatal(err)
	}
	scheduler := automationsvc.NewScheduler(h.app.Repository, h.app.EventBus, h.app.RuntimeService, repository.AutomationRunDefaults{
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
		ValidateRouting: h.app.RuntimeService.ValidateRouting,
	})
	if err := scheduler.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var runID string
	if err := h.app.database.QueryRowContext(ctx, `SELECT run_id FROM automation_runs WHERE automation_id = ?`, automation.AutomationID).Scan(&runID); err != nil {
		t.Fatalf("the automation did not fire: %v", err)
	}
	var run repository.Run
	waitFor(t, "the automation's run to end", func() bool {
		run, err = h.app.Repository.GetRun(ctx, runID)
		return err == nil && (run.Status == "completed" || run.Status == "failed" || run.Status == "cancelled")
	})
	return automation, run
}

func (h *ceilingE2E) blockedRows(t *testing.T, runID string) []string {
	t.Helper()
	rows, err := h.app.database.QueryContext(context.Background(), `
		SELECT COALESCE(payload_json, '') FROM audit_logs
		WHERE correlation_id = ? AND action = 'automation.tool.blocked' ORDER BY rowid`, runID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var payloads []string
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			t.Fatal(err)
		}
		payloads = append(payloads, payload)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return payloads
}

func (h *ceilingE2E) errorCode(t *testing.T, runID string) string {
	t.Helper()
	var code string
	if err := h.app.database.QueryRowContext(context.Background(),
		`SELECT COALESCE(error_code, '') FROM agent_runs WHERE id = ?`, runID).Scan(&code); err != nil {
		t.Fatal(err)
	}
	return code
}

func (h *ceilingE2E) toolCallStatuses(t *testing.T, runID string, tool string) []string {
	t.Helper()
	rows, err := h.app.database.QueryContext(context.Background(),
		`SELECT status FROM tool_calls WHERE run_id = ? AND tool_name = ? ORDER BY created_at`, runID, tool)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var statuses []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		statuses = append(statuses, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return statuses
}

func (h *ceilingE2E) approvals(t *testing.T, runID string) int {
	t.Helper()
	var count int
	if err := h.app.database.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM approvals WHERE run_id = ?`, runID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// assertOffered checks the model's first request offered the tool: the job
// carries no enforced selection, so the run is offered every tool its worker
// serves.
func (h *ceilingE2E) assertOffered(t *testing.T, tool string) {
	t.Helper()
	requests := h.model.seen()
	if len(requests) == 0 || !strings.Contains(requests[0], `"name":"`+tool+`"`) {
		t.Fatalf("the model was not offered %s", tool)
	}
}

func (h *ceilingE2E) assertFailedNotAllowlisted(t *testing.T, run repository.Run, server string, tool string) {
	t.Helper()
	if run.Status != "failed" || h.errorCode(t, run.RunID) != repository.AutomationToolNotAllowlistedCode {
		t.Fatalf("run = %s / %q, want failed / %s", run.Status, h.errorCode(t, run.RunID), repository.AutomationToolNotAllowlistedCode)
	}
	rows := h.blockedRows(t, run.RunID)
	if len(rows) != 1 || !strings.Contains(rows[0], `"code":"automation_tool_not_allowlisted"`) ||
		!strings.Contains(rows[0], `"serverName":"`+server+`"`) || !strings.Contains(rows[0], `"toolName":"`+tool+`"`) {
		t.Fatalf("automation.tool.blocked rows = %q, want one naming %s/%s", rows, server, tool)
	}
	if statuses := h.toolCallStatuses(t, run.RunID, tool); len(statuses) != 1 || statuses[0] != "denied" {
		t.Fatalf("%s tool calls = %q, want one denied", tool, statuses)
	}
}

// An automation whose allowlist lacks files.read is still offered it, calls
// it, and the beacon fails the run with automation_tool_not_allowlisted and
// an audit row, with nothing read.
func TestAnAutomationThatCallsAnUnlistedReadFailsWithNothingRead(t *testing.T) {
	h := newCeilingE2E(t, ceilingToolCall{name: "files.read", args: map[string]any{"path": "note.txt"}})
	_, run := h.fire(t, repository.AutomationTool{ServerName: "files", ToolName: "files.list"})

	h.assertOffered(t, "files.read")
	h.assertFailedNotAllowlisted(t, run, "files", "files.read")
	if calls := h.files.calls(); len(calls) != 0 {
		t.Fatalf("files server calls = %q, want nothing read", calls)
	}
}

// A third-party tool the owner marked safe, and that the automation does not
// list, is blocked the same way, and its server is never reached.
func TestAnAutomationThatCallsAnUnlistedSafeThirdPartyToolFails(t *testing.T) {
	h := newCeilingE2E(t, ceilingToolCall{name: "vendor.lookup", args: map[string]any{"query": "orders"}})
	_, run := h.fire(t, repository.AutomationTool{ServerName: "files", ToolName: "files.read"})

	h.assertOffered(t, "vendor.lookup")
	h.assertFailedNotAllowlisted(t, run, "vendor", "vendor.lookup")
	if got := h.vendorRequests.Load(); got != 0 {
		t.Fatalf("vendor requests = %d, want none", got)
	}
}

// A listed safe tool runs without an approval, and the run completes.
func TestAnAutomationRunsAListedSafeToolWithoutApproval(t *testing.T) {
	h := newCeilingE2E(t, ceilingToolCall{name: "files.read", args: map[string]any{"path": "note.txt"}})
	_, run := h.fire(t, repository.AutomationTool{ServerName: "files", ToolName: "files.read"})

	if run.Status != "completed" || run.AssistantContent != "Done." {
		t.Fatalf("run = %s %q, want completed with the model's answer", run.Status, run.AssistantContent)
	}
	if calls := h.files.calls(); len(calls) != 1 || calls[0] != "files.read" {
		t.Fatalf("files server calls = %q, want the one read", calls)
	}
	if got := h.approvals(t, run.RunID); got != 0 {
		t.Fatalf("approvals = %d, want none for a safe tool", got)
	}
	if rows := h.blockedRows(t, run.RunID); len(rows) != 0 {
		t.Fatalf("automation.tool.blocked rows = %q, want none", rows)
	}
}

// An automation's model that calls memory.search is refused with
// automation_tool_unsupported, reads nothing from the vault, writes no
// automation.tool.blocked row, and its run carries on and completes.
func TestAnAutomationsMemorySearchIsRefusedAndTheRunCompletes(t *testing.T) {
	h := newCeilingE2E(t, ceilingToolCall{name: "memory.search", args: map[string]any{"query": "plans"}})
	_, run := h.fire(t, repository.AutomationTool{ServerName: "files", ToolName: "files.read"})

	h.assertOffered(t, "memory.search")
	if run.Status != "completed" || run.AssistantContent != "Done." {
		t.Fatalf("run = %s %q (code %q), want it to carry on and complete", run.Status, run.AssistantContent, h.errorCode(t, run.RunID))
	}
	if statuses := h.toolCallStatuses(t, run.RunID, "memory.search"); len(statuses) != 1 || statuses[0] != "denied" {
		t.Fatalf("memory.search tool calls = %q, want one denied", statuses)
	}
	if rows := h.blockedRows(t, run.RunID); len(rows) != 0 {
		t.Fatalf("automation.tool.blocked rows = %q, want none", rows)
	}
	var reasons []string
	rows, err := h.app.database.QueryContext(context.Background(), `
		SELECT COALESCE(json_extract(payload_json, '$.reason'), '') FROM audit_logs
		WHERE correlation_id = ? AND action = 'tool.call.before'`, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var reason string
		if err := rows.Scan(&reason); err != nil {
			t.Fatal(err)
		}
		reasons = append(reasons, reason)
	}
	if len(reasons) != 1 || reasons[0] != repository.AutomationToolUnsupportedCode {
		t.Fatalf("tool.call.before reasons = %q, want the one refusal as %s", reasons, repository.AutomationToolUnsupportedCode)
	}
	requests := h.model.seen()
	if len(requests) != 2 || !strings.Contains(requests[1], `"role":"tool"`) {
		t.Fatalf("model requests = %d, want the refusal handed back as a tool result", len(requests))
	}
	for _, request := range requests {
		if strings.Contains(request, ceilingVaultSentinel) {
			t.Fatal("a belief from the vault reached the model")
		}
	}
}

// With a GitHub connection, an automation's model that calls a GitHub read,
// or a GitHub write, fails the run with automation_tool_not_allowlisted and
// an audit row, and no request is sent to GitHub.
func TestAnAutomationsGitHubCallFailsTheRunWithNothingSent(t *testing.T) {
	for _, call := range []ceilingToolCall{
		{name: "github.list_issues", args: map[string]any{"connection_id": "any", "owner": "octo", "repo": "app"}},
		{name: "github.create_comment", args: map[string]any{"connection_id": "any", "owner": "octo", "repo": "app", "issue_number": 1, "body": "hi"}},
	} {
		t.Run(call.name, func(t *testing.T) {
			h := newCeilingE2E(t, call)
			_, run := h.fire(t, repository.AutomationTool{ServerName: "files", ToolName: "files.read"})

			h.assertOffered(t, call.name)
			h.assertFailedNotAllowlisted(t, run, "integrations", call.name)
			if got := h.github.requests.Load(); got != 0 {
				t.Fatalf("GitHub requests = %d, want none", got)
			}
		})
	}
}

// skills_list and skill_view still work in an automation with no skills tool
// listed: the skill reaches the model. They default to approval_required, so
// they work unattended, today and now, once the owner marks them safe; the
// ceiling exempts them, so that is still all it takes.
func TestAnAutomationStillLoadsItsSkillsWithNoSkillsToolListed(t *testing.T) {
	h := newCeilingE2E(t,
		ceilingToolCall{name: "skills_list", args: map[string]any{}},
		ceilingToolCall{name: "skill_view", args: map[string]any{"id": "writing/digest"}},
	)
	registry := turingv1.NewMcpRegistryServiceClient(newBufconnClient(t, h.app.PublicServer))
	for _, tool := range []string{"skills_list", "skill_view"} {
		if _, err := registry.UpdateToolPolicyByName(h.publicCtx, &turingv1.UpdateToolPolicyByNameRequest{
			ServerName: "skills", ToolName: tool, Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE,
		}); err != nil {
			t.Fatalf("mark %s safe: %v", tool, err)
		}
	}
	_, run := h.fire(t, repository.AutomationTool{ServerName: "files", ToolName: "files.read"})

	if run.Status != "completed" || run.AssistantContent != "Done." {
		t.Fatalf("run = %s %q (code %q), want it completed", run.Status, run.AssistantContent, h.errorCode(t, run.RunID))
	}
	for _, tool := range []string{"skills_list", "skill_view"} {
		if statuses := h.toolCallStatuses(t, run.RunID, tool); len(statuses) != 1 || statuses[0] == "denied" {
			t.Fatalf("%s tool calls = %q, want one that ran", tool, statuses)
		}
	}
	requests := h.model.seen()
	if len(requests) != 3 || !strings.Contains(requests[2], ceilingSkillBody) {
		t.Fatalf("the skill's body never reached the model in %d requests", len(requests))
	}
}

// forgingExecutor is a worker whose executor sends a team.delegate beacon
// the general assistant would never send for an automation's run, then
// carries on and completes.
type forgingExecutor struct {
	poster   func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)
	decision chan *turingv1.ToolPolicyDecision
}

func (e *forgingExecutor) SetToolBeaconPoster(post func(context.Context, *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error)) {
	e.poster = post
}

func (e *forgingExecutor) Execute(ctx context.Context, job *turingv1.AgentJob, emit func(*turingv1.RuntimeUpdate) error) error {
	args, err := structpb.NewStruct(map[string]any{"agent": "research", "task": "Gather the notes"})
	if err != nil {
		return err
	}
	decision, err := e.poster(ctx, &turingv1.ToolCallBeacon{
		RunId: job.GetRunId(), TraceId: job.GetTraceId(), ToolCallId: "call_forged_delegate",
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, ServerName: "team", ToolName: "team.delegate",
		Phase: turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE, Args: args,
	})
	if err != nil {
		return err
	}
	e.decision <- decision
	return emit(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_RunCompleted{RunCompleted: &turingv1.RuntimeRunCompleted{
		RunId: job.GetRunId(), AssistantMessageId: job.GetAssistantMessageId(), Content: "Carried on.",
	}}})
}

// A forged team.delegate beacon from an automation's run gets the same
// non-fatal refusal, and the run continues and completes.
func TestAForgedTeamDelegateBeaconFromAnAutomationIsRefusedAndTheRunContinues(t *testing.T) {
	app := newTestApp(t)
	internalConn := newBufconnClient(t, app.InternalServer)
	executor := &forgingExecutor{decision: make(chan *turingv1.ToolPolicyDecision, 1)}
	ctx := context.Background()
	workerCtx, stopWorker := context.WithCancel(ctx)
	workerDone := make(chan error, 1)
	go func() {
		workerDone <- runtimetestkit.RunWorkerWithExecutor(workerCtx, runtimetestkit.WorkerConfig{
			Conn: internalConn, RuntimeToken: "internal", WorkerID: "forging-runtime",
			MaxConcurrentRuns: 1, OllamaBaseURL: "http://127.0.0.1:1", LocalModel: "llama3.2",
			DiscoveredTools: []*turingv1.DiscoveredTool{{ServerName: "team", ToolName: "team.delegate", Schema: &structpb.Struct{}}},
		}, executor)
	}()
	t.Cleanup(func() {
		stopWorker()
		<-workerDone
	})
	waitFor(t, "the forging worker to serve the local model", func() bool {
		return app.RuntimeService.ValidateRouting(ctx, repository.RoutingRequirements{
			AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
		}) == nil
	})
	h := &ceilingE2E{app: app}
	_, run := h.fire(t, repository.AutomationTool{ServerName: "files", ToolName: "files.read"})

	decision := <-executor.decision
	if decision.GetDecision() != turingv1.ToolPolicyDecision_DECISION_DENY || decision.GetTerminalRun() ||
		decision.GetReason() != repository.AutomationToolUnsupportedCode {
		t.Fatalf("decision = %+v, want a non-terminal %s deny", decision, repository.AutomationToolUnsupportedCode)
	}
	if run.Status != "completed" || run.AssistantContent != "Carried on." {
		t.Fatalf("run = %s %q, want it to carry on and complete", run.Status, run.AssistantContent)
	}
	if rows := h.blockedRows(t, run.RunID); len(rows) != 0 {
		t.Fatalf("automation.tool.blocked rows = %q, want none", rows)
	}
	if count, err := app.Repository.DelegationCount(ctx, run.RunID); err != nil || count != 0 {
		t.Fatalf("delegations = %d, %v; want none", count, err)
	}
}
