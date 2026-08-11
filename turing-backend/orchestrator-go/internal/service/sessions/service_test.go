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
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type sessionHarness struct {
	repo *repository.Repository
	conn *grpc.ClientConn
}

func newSessionHarness(t *testing.T) *sessionHarness {
	t.Helper()
	database := openSessionTestDB(t)
	repo := repository.New(database)
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	turingv1.RegisterSessionServiceServer(grpcServer, New(repo, config.Config{
		MCPFilesTokenGeneral: "files-token",
		ApprovalJWTSecret:    "approval-secret",
		OllamaModel:          "llama3.2",
		OpenAIAPIKey:         "openai-key",
		OpenAIModel:          "gpt-4o-mini",
	}))
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
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = conn.Close()
	})
	return &sessionHarness{repo: repo, conn: conn}
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

	agents, err := client.ListAgents(ctx, &turingv1.ListAgentsRequest{})
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents.Agents) != 1 || agents.Agents[0].Id != turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT {
		t.Fatalf("agents = %+v", agents.Agents)
	}

	tools, err := client.ListTools(ctx, &turingv1.ListToolsRequest{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	gotTools := map[string]turingv1.ToolPolicy{}
	for _, tool := range tools.Tools {
		gotTools[tool.ToolName] = tool.Policy
	}
	if gotTools["system.time"] != turingv1.ToolPolicy_TOOL_POLICY_SAFE {
		t.Fatalf("system.time policy = %v", gotTools["system.time"])
	}
	if gotTools["files.create"] != turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED {
		t.Fatalf("files.create policy = %v", gotTools["files.create"])
	}
}
