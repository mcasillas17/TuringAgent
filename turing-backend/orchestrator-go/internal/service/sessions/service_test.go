package sessions

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type sessionHarness struct {
	database     *db.DB
	repo         *repository.Repository
	conn         *grpc.ClientConn
	capabilities *sessionCapabilitySource
}

type sessionCapabilitySource struct {
	providers map[turingv1.ModelProvider][]*turingv1.ModelCapability
	agents    map[turingv1.AgentId]bool
	tools     []string
}

func (s *sessionCapabilitySource) ProviderCapabilities() map[turingv1.ModelProvider][]*turingv1.ModelCapability {
	return s.providers
}

func (s *sessionCapabilitySource) AgentAvailable(agentID turingv1.AgentId) bool {
	return s.agents[agentID]
}

func (s *sessionCapabilitySource) RoutableDefaultModel(provider string, configured string) string {
	providerID := turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA
	if provider == "openai_compatible" {
		providerID = turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE
	}
	for _, model := range s.providers[providerID] {
		if model.GetModel() == configured {
			return configured
		}
	}
	if len(s.providers[providerID]) > 0 {
		return s.providers[providerID][0].GetModel()
	}
	return ""
}

func (s *sessionCapabilitySource) LiveToolNames() []string {
	return append([]string(nil), s.tools...)
}

func newSessionHarness(t *testing.T) *sessionHarness {
	t.Helper()
	database := openSessionTestDB(t)
	repo := repository.New(database)
	capabilities := &sessionCapabilitySource{
		providers: map[turingv1.ModelProvider][]*turingv1.ModelCapability{
			turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA: {{
				Provider:         turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
				Model:            "llama3.2",
				MaxContextTokens: 8192,
			}},
		},
		agents: map[turingv1.AgentId]bool{
			turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT: true,
		},
		tools: []string{"custom/custom.scan", "files/files.create", "system/system.time"},
	}
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	turingv1.RegisterSessionServiceServer(grpcServer, New(repo, config.Config{
		FilesMCPEnabled:   true,
		ApprovalJWTSecret: "approval-secret",
		OllamaModel:       "llama3.2",
		OpenAIEnabled:     true,
		OpenAIBaseURL:     "https://api.openai.com/v1",
		OpenAIModel:       "gpt-4o-mini",
	}, capabilities))
	go func() {
		_ = grpcServer.Serve(lis)
	}()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	// NewClient starts the channel IDLE; DialContext connected eagerly in the
	// background. Connect() restores that so handshake latency does not land
	// inside a test's deadline.
	conn.Connect()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = conn.Close()
	})
	return &sessionHarness{database: database, repo: repo, conn: conn, capabilities: capabilities}
}

func openSessionTestDB(t *testing.T) *db.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name())
	sqlDB, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=memory&cache=shared&_foreign_keys=on", name))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	database := &db.DB{DB: sqlDB}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return database
}

func TestSessionServiceServesPublicReadEndpoints(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()

	created, err := client.CreateSession(ctx, &turingv1.CreateSessionRequest{Title: "Test chat"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if created.SessionId == "" || created.CreatedAt == nil {
		t.Fatalf("bad CreateSession response: %+v", created)
	}
	session, err := client.GetSession(ctx, &turingv1.GetSessionRequest{SessionId: created.SessionId})
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session.SessionId != created.SessionId || session.Title != "Test chat" || session.Status != "active" {
		t.Fatalf("bad GetSession response: %+v", session)
	}
	listed, err := client.ListSessions(ctx, &turingv1.ListSessionsRequest{Page: &turingv1.PageRequest{Limit: 10}})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(listed.Sessions) != 1 || listed.Sessions[0].SessionId != created.SessionId {
		t.Fatalf("bad ListSessions response: %+v", listed.Sessions)
	}
	if _, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: created.SessionId, Content: "hello", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	}); err != nil {
		t.Fatalf("seed messages: %v", err)
	}

	messages, err := client.ListMessages(ctx, &turingv1.ListMessagesRequest{SessionId: created.SessionId, Limit: 10})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(messages.Messages))
	}
	if messages.Messages[0].Role != turingv1.MessageRole_MESSAGE_ROLE_USER || messages.Messages[0].Content != "hello" {
		t.Fatalf("bad user message: %+v", messages.Messages[0])
	}
	if messages.Messages[1].Role != turingv1.MessageRole_MESSAGE_ROLE_ASSISTANT {
		t.Fatalf("bad assistant message: %+v", messages.Messages[1])
	}

	cfg, err := client.GetConfig(ctx, &turingv1.GetConfigRequest{})
	if err != nil {
		t.Fatalf("GetConfig: %v", err)
	}
	if !cfg.ApprovalsEnabled || !cfg.FilesMcpEnabled {
		t.Fatalf("bad feature flags: approvals=%v files=%v", cfg.ApprovalsEnabled, cfg.FilesMcpEnabled)
	}
	if len(cfg.Providers) != 2 {
		t.Fatalf("provider count = %d, want 2", len(cfg.Providers))
	}
	if !cfg.Providers[0].GetEnabled() || len(cfg.Providers[0].GetModels()) != 1 ||
		cfg.Providers[0].GetModels()[0].GetMaxContextTokens() != 8192 {
		t.Fatalf("Ollama live capabilities = %+v", cfg.Providers[0])
	}
	if cfg.Providers[1].GetEnabled() || len(cfg.Providers[1].GetModels()) != 0 {
		t.Fatalf("unadvertised OpenAI provider = %+v, want disabled with no models", cfg.Providers[1])
	}
	if cfg.Providers[0].GetRemoteEndpoint() != "" || cfg.Providers[0].GetRequiresPerRunConsent() {
		t.Fatalf("Ollama egress metadata = %+v, want local", cfg.Providers[0])
	}
	if cfg.Providers[1].GetRemoteEndpoint() != "https://api.openai.com/v1" ||
		!cfg.Providers[1].GetRequiresPerRunConsent() {
		t.Fatalf("OpenAI egress metadata = %+v", cfg.Providers[1])
	}
	h.capabilities.providers[turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA] = []*turingv1.ModelCapability{{
		Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: "live-fallback",
	}}
	cfg, err = client.GetConfig(ctx, &turingv1.GetConfigRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Providers[0].GetDefaultModel(); got != "live-fallback" {
		t.Fatalf("default model = %q, want advertised fallback", got)
	}

	agents, err := client.ListAgents(ctx, &turingv1.ListAgentsRequest{})
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents.Agents) != 1 || agents.Agents[0].Id != turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT ||
		!agents.Agents[0].GetAvailable() {
		t.Fatalf("agents = %+v", agents.Agents)
	}

	if err := h.repo.UpsertTools(ctx, []repository.DiscoveredTool{
		{ServerName: "custom", ToolName: "custom.scan", SchemaJSON: `{}`, Policy: "approval_required"},
		{ServerName: "files", ToolName: "files.create", SchemaJSON: `{}`, Policy: "disabled"},
		{ServerName: "system", ToolName: "system.time", SchemaJSON: `{}`, Policy: "safe"},
	}); err != nil {
		t.Fatalf("seed tools: %v", err)
	}
	tools, err := client.ListTools(ctx, &turingv1.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	gotTools := map[string]turingv1.ToolPolicy{}
	for _, tool := range tools.Tools {
		gotTools[tool.ServerName+"/"+tool.ToolName] = tool.Policy
	}
	if len(gotTools) != 3 {
		t.Fatalf("tools = %+v, want exact database snapshot", tools.Tools)
	}
	if gotTools["system/system.time"] != turingv1.ToolPolicy_TOOL_POLICY_SAFE {
		t.Fatalf("system.time policy = %v", gotTools["system/system.time"])
	}
	if gotTools["files/files.create"] != turingv1.ToolPolicy_TOOL_POLICY_DISABLED {
		t.Fatalf("files.create policy = %v", gotTools["files/files.create"])
	}
	if gotTools["custom/custom.scan"] != turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED {
		t.Fatalf("custom.scan policy = %v", gotTools["custom/custom.scan"])
	}
}

func TestListToolsIsEmptyBeforeAnyWorkerReportsCapabilities(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	listed, err := client.ListTools(context.Background(), &turingv1.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}

	if len(listed.Tools) != 0 {
		t.Fatalf("ListTools before a worker handshake = %+v, want empty", listed.Tools)
	}
}

func TestListToolsExcludesPersistedToolsThatNoLiveWorkerAdvertises(t *testing.T) {
	h := newSessionHarness(t)
	if err := h.repo.UpsertTools(context.Background(), []repository.DiscoveredTool{
		{ServerName: "system", ToolName: "system.time", SchemaJSON: `{}`, Policy: "safe"},
		{ServerName: "files", ToolName: "files.create", SchemaJSON: `{}`, Policy: "approval_required"},
	}); err != nil {
		t.Fatalf("UpsertTools: %v", err)
	}
	h.capabilities.tools = []string{"system/system.time"}

	listed, err := turingv1.NewSessionServiceClient(h.conn).ListTools(context.Background(), &turingv1.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(listed.Tools) != 1 || listed.Tools[0].GetToolName() != "system.time" {
		t.Fatalf("live tools = %#v, want only system.time", listed.Tools)
	}
}

func TestSessionServiceSearchMessagesValidatesQuery(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()

	for name, req := range map[string]*turingv1.SearchMessagesRequest{
		"empty":      {},
		"whitespace": {Query: " \t\n "},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := client.SearchMessages(ctx, req)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("SearchMessages error = %v, want InvalidArgument", err)
			}
		})
	}

	_, err := New(h.repo, config.Config{}, h.capabilities).SearchMessages(ctx, nil)
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("SearchMessages nil request error = %v, want InvalidArgument", err)
	}

	response, err := client.SearchMessages(ctx, &turingv1.SearchMessagesRequest{Query: "..."})
	if err != nil {
		t.Fatalf("SearchMessages punctuation-only query: %v", err)
	}
	if len(response.Messages) != 0 {
		t.Fatalf("SearchMessages punctuation-only results = %+v, want none", response.Messages)
	}
}

func TestSessionServiceSearchMessagesReturnsGlobalAndScopedResults(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	insertServiceSearchSession(t, ctx, h.database, "session-a")
	insertServiceSearchSession(t, ctx, h.database, "session-b")
	insertServiceSearchMessage(t, ctx, h.database, "message-a", "session-a", "assistant", "recallneedle alpha", 1)
	insertServiceSearchMessage(t, ctx, h.database, "message-b", "session-b", "user", "recallneedle beta", 1)
	insertServiceSearchMessage(t, ctx, h.database, "message-c", "session-b", "tool", "unrelated", 2)
	if _, err := h.database.ExecContext(ctx, `UPDATE messages SET run_id = 'run-message-a' WHERE id = 'message-a'`); err != nil {
		t.Fatalf("set message run_id: %v", err)
	}

	global, err := client.SearchMessages(ctx, &turingv1.SearchMessagesRequest{Query: "recallneedle", Limit: 10})
	if err != nil {
		t.Fatalf("SearchMessages global: %v", err)
	}
	if len(global.Messages) != 2 {
		t.Fatalf("global message count = %d, want 2", len(global.Messages))
	}
	got := make(map[string]*turingv1.Message, len(global.Messages))
	for _, message := range global.Messages {
		got[message.MessageId] = message
	}
	if message := got["message-a"]; message == nil || message.SessionId != "session-a" || message.RunId != "run-message-a" || message.Content != "recallneedle alpha" || message.Role != turingv1.MessageRole_MESSAGE_ROLE_ASSISTANT {
		t.Fatalf("global message-a = %+v", message)
	}
	if message := got["message-b"]; message == nil || message.SessionId != "session-b" || message.Content != "recallneedle beta" || message.Role != turingv1.MessageRole_MESSAGE_ROLE_USER {
		t.Fatalf("global message-b = %+v", message)
	}

	scoped, err := client.SearchMessages(ctx, &turingv1.SearchMessagesRequest{Query: "recallneedle", SessionId: "session-b", Limit: 10})
	if err != nil {
		t.Fatalf("SearchMessages scoped: %v", err)
	}
	if len(scoped.Messages) != 1 || scoped.Messages[0].MessageId != "message-b" || scoped.Messages[0].SessionId != "session-b" {
		t.Fatalf("scoped messages = %+v", scoped.Messages)
	}

	excluded, err := client.SearchMessages(ctx, &turingv1.SearchMessagesRequest{
		Query:            "recallneedle",
		ExcludeSessionId: "session-b",
		Limit:            10,
	})
	if err != nil {
		t.Fatalf("SearchMessages excluded: %v", err)
	}
	if len(excluded.Messages) != 1 || excluded.Messages[0].MessageId != "message-a" || excluded.Messages[0].SessionId != "session-a" {
		t.Fatalf("excluded messages = %+v", excluded.Messages)
	}
}

func TestSessionServiceSearchMessagesHonorsLimit(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	insertServiceSearchSession(t, ctx, h.database, "session-limit")
	for i := 1; i <= 3; i++ {
		insertServiceSearchMessage(t, ctx, h.database, fmt.Sprintf("message-%d", i), "session-limit", "user", "limitneedle", int64(i))
	}

	response, err := client.SearchMessages(ctx, &turingv1.SearchMessagesRequest{Query: "limitneedle", Limit: 2})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(response.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(response.Messages))
	}
}

func TestSessionServiceSearchMessagesHidesDatabaseErrors(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	if err := h.database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	_, err := client.SearchMessages(context.Background(), &turingv1.SearchMessagesRequest{Query: "needle"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("SearchMessages error = %v, want Internal", err)
	}
	if got := status.Convert(err).Message(); got != "search messages failed" {
		t.Fatalf("SearchMessages message = %q, want %q", got, "search messages failed")
	}
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "sqlite") || strings.Contains(lower, "database is closed") {
		t.Fatalf("SearchMessages leaked database details: %v", err)
	}
}

func TestSessionServiceListsMessagesOnlyBeforeBoundary(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "Causal messages")
	if err != nil {
		t.Fatal(err)
	}
	for index, message := range []struct {
		id      string
		content string
	}{
		{id: "msg_a", content: "earlier"},
		{id: "msg_b", content: "current"},
		{id: "msg_c", content: "future"},
	} {
		if _, err := h.database.ExecContext(ctx, `
			INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
			VALUES (?, ?, 'user', ?, 'text', ?, '2026-08-10T22:42:30.000000000Z')
		`, message.id, session.SessionID, message.content, index+1); err != nil {
			t.Fatal(err)
		}
	}

	response, err := client.ListMessages(ctx, &turingv1.ListMessagesRequest{
		SessionId:       session.SessionID,
		BeforeMessageId: "msg_b",
		Limit:           50,
	})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(response.Messages) != 1 || response.Messages[0].GetMessageId() != "msg_a" {
		t.Fatalf("causal response = %+v, want only msg_a", response.Messages)
	}
}

func TestListMessagesBeforeAssignedTurnExcludesRapidlyQueuedLaterTurns(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	created, err := client.CreateSession(ctx, &turingv1.CreateSessionRequest{Title: "Rapid send"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: created.SessionId, Content: "first", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: created.SessionId, Content: "second", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: created.SessionId, Content: "third", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	}); err != nil {
		t.Fatal(err)
	}

	got, err := client.ListMessages(ctx, &turingv1.ListMessagesRequest{
		SessionId:       created.SessionId,
		Limit:           50,
		BeforeMessageId: second.UserMessageID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("message count = %d, want first turn only: %+v", len(got.Messages), got.Messages)
	}
	if got.Messages[0].MessageId != first.UserMessageID || got.Messages[0].Sequence != 1 {
		t.Fatalf("messages[0] = %+v, want first user at sequence 1", got.Messages[0])
	}
	if got.Messages[1].MessageId != first.AssistantMessageID || got.Messages[1].Sequence != 2 {
		t.Fatalf("messages[1] = %+v, want first assistant at sequence 2", got.Messages[1])
	}
}

func insertServiceSearchSession(t *testing.T, ctx context.Context, database *db.DB, id string) {
	t.Helper()
	_, err := database.ExecContext(ctx, `INSERT INTO sessions (id, created_at, updated_at) VALUES (?, '2026-08-10T00:00:00Z', '2026-08-10T00:00:00Z')`, id)
	if err != nil {
		t.Fatalf("insert session %s: %v", id, err)
	}
}

func insertServiceSearchMessage(t *testing.T, ctx context.Context, database *db.DB, id, sessionID, role, content string, sequence int64) {
	t.Helper()
	_, err := database.ExecContext(ctx, `
		INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
		VALUES (?, ?, ?, ?, 'text', ?, '2026-08-10T00:00:00Z')`, id, sessionID, role, content, sequence)
	if err != nil {
		t.Fatalf("insert message %s: %v", id, err)
	}
}

// Deletion is the only way a user can withdraw what they have said, so its
// failure modes must be distinguishable over the wire: a client has to be able
// to say "already gone" and "still running" rather than "something broke".
func TestDeleteSessionReportsDistinctStatusCodes(t *testing.T) {
	h := newSessionHarness(t)
	ctx := context.Background()
	client := turingv1.NewSessionServiceClient(h.conn)

	if _, err := client.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: ""}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("empty session_id = %v, want InvalidArgument", status.Code(err))
	}
	if _, err := client.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: "sess_missing"}); status.Code(err) != codes.NotFound {
		t.Fatalf("unknown session = %v, want NotFound", status.Code(err))
	}

	// A session whose run is still in flight must refuse rather than orphan it.
	session, err := h.repo.CreateSession(ctx, "Busy")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "in flight", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("live run = %v, want FailedPrecondition", status.Code(err))
	}

	// And the happy path actually removes it.
	idle, err := h.repo.CreateSession(ctx, "Idle")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: idle.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetSessionId() != idle.SessionID {
		t.Fatalf("response session_id = %q, want %q", resp.GetSessionId(), idle.SessionID)
	}
	if _, err := client.GetSession(ctx, &turingv1.GetSessionRequest{SessionId: idle.SessionID}); status.Code(err) != codes.NotFound {
		t.Fatalf("deleted session still readable: %v", status.Code(err))
	}
}
