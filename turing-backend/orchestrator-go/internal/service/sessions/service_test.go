package sessions

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	eventsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
)

type sessionHarness struct {
	database     *db.DB
	repo         *repository.Repository
	conn         *grpc.ClientConn
	capabilities *sessionCapabilitySource
	bus          *eventsvc.Bus
	service      *Server
}

type sessionCapabilitySource struct {
	providers         map[turingv1.ModelProvider][]*turingv1.ModelCapability
	agents            map[turingv1.AgentId]bool
	tools             []string
	cancelledSessions []string
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

func (s *sessionCapabilitySource) CancelSessionRuns(_ context.Context, sessionID string, _ string) {
	s.cancelledSessions = append(s.cancelledSessions, sessionID)
}

func newSessionHarness(t *testing.T) *sessionHarness {
	t.Helper()
	return newSessionHarnessWithDB(t, openSessionTestDB(t))
}

// newSessionHarnessWithDB builds the service under test over a caller-supplied
// database. It deliberately does not own that database's lifetime: tests that
// need to prove something survives a process restart close and reopen the same
// file themselves, and a second owner would close it out from under them.
func newSessionHarnessWithDB(t *testing.T, database *db.DB) *sessionHarness {
	t.Helper()
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
	bus := eventsvc.NewBus(16)
	service := New(repo, config.Config{
		FilesMCPEnabled:   true,
		ApprovalJWTSecret: "approval-secret",
		CursorHMACKey:     [32]byte{1},
		OllamaModel:       "llama3.2",
		OpenAIEnabled:     true,
		OpenAIBaseURL:     "https://api.openai.com/v1",
		OpenAIModel:       "gpt-4o-mini",
	}, capabilities, bus)
	turingv1.RegisterSessionServiceServer(grpcServer, service)
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
	return &sessionHarness{
		database:     database,
		repo:         repo,
		conn:         conn,
		capabilities: capabilities,
		bus:          bus,
		service:      service,
	}
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

// openSessionTestDBAt opens a real on-disk database so a test can close it and
// reopen the same path. The shared in-memory helper cannot do that: its store
// disappears with the last connection, so "the sentinel is gone after a
// restart" would pass against an empty database.
//
// The cleanup close is a backstop for an early t.Fatalf; database/sql makes
// Close idempotent, so a test that closes the same handle first is fine.
func openSessionTestDBAt(t *testing.T, path string) *db.DB {
	t.Helper()
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return database
}

func TestSessionServiceCreatesSession(t *testing.T) {
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
}

func TestListSessionsPaginatesStablyAndSupportsPageSizeChanges(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	for index, id := range []string{"older-1", "older-2", "older-3", "older-4"} {
		if _, err := h.database.ExecContext(ctx, `
				INSERT INTO sessions (id, title, title_origin, status, created_at, updated_at)
				VALUES (?, ?, 'explicit', 'active', '2026-08-20T04:00:00.000000000Z', ?)`,
			id,
			id,
			fmt.Sprintf("2026-08-20T04:00:%02d.000000000Z", index+1),
		); err != nil {
			t.Fatal(err)
		}
	}

	first, err := client.ListSessions(ctx, &turingv1.ListSessionsRequest{
		Page: &turingv1.PageRequest{Limit: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertProtoSessionIDs(t, first.Sessions, []string{"older-4", "older-3"})
	if first.Page == nil || first.Page.NextCursor == "" {
		t.Fatalf("first page = %+v, want next cursor", first.Page)
	}
	if _, err := h.database.ExecContext(ctx, `
			INSERT INTO sessions (id, title, title_origin, status, created_at, updated_at)
			VALUES (
				'inserted-newest', 'inserted-newest', 'explicit', 'active',
				'2026-08-20T04:00:00.000000000Z', '2026-08-20T04:00:05.000000000Z'
			)`,
	); err != nil {
		t.Fatal(err)
	}

	second, err := client.ListSessions(ctx, &turingv1.ListSessionsRequest{
		Page: &turingv1.PageRequest{Limit: 1, Cursor: first.Page.NextCursor},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertProtoSessionIDs(t, second.Sessions, []string{"older-2"})
	if second.Page == nil || second.Page.NextCursor == "" {
		t.Fatalf("second page = %+v, want next cursor", second.Page)
	}

	finalPage, err := client.ListSessions(ctx, &turingv1.ListSessionsRequest{
		Page: &turingv1.PageRequest{Limit: 2, Cursor: second.Page.NextCursor},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertProtoSessionIDs(t, finalPage.Sessions, []string{"older-1"})
	if finalPage.Page == nil || finalPage.Page.NextCursor != "" {
		t.Fatalf("final page = %+v, want non-nil empty cursor", finalPage.Page)
	}
}

func TestListSessionsValidatesLimitsAndCursorsPredictably(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "Cursor")
	if err != nil {
		t.Fatal(err)
	}
	validPage, err := client.ListSessions(ctx, &turingv1.ListSessionsRequest{
		Page: &turingv1.PageRequest{Limit: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	validCursor := validPage.GetPage().GetNextCursor()
	if validCursor != "" {
		t.Fatal("single-row page unexpectedly has a next cursor")
	}
	second, err := h.repo.CreateSession(ctx, "Cursor 2")
	if err != nil {
		t.Fatal(err)
	}
	_ = second
	validPage, err = client.ListSessions(ctx, &turingv1.ListSessionsRequest{
		Page: &turingv1.PageRequest{Limit: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	validCursor = validPage.GetPage().GetNextCursor()
	if validCursor == "" {
		t.Fatal("two-row page has no next cursor")
	}

	for _, limit := range []int32{-1, 101} {
		_, err := client.ListSessions(ctx, &turingv1.ListSessionsRequest{
			Page: &turingv1.PageRequest{Limit: limit},
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("limit %d error = %v, want InvalidArgument", limit, err)
		}
	}
	if _, err := client.ListSessions(ctx, &turingv1.ListSessionsRequest{
		Page: &turingv1.PageRequest{},
	}); err != nil {
		t.Fatalf("default page limit: %v", err)
	}

	foreign, err := newSessionCursorCodec([32]byte{2}).encode(sessionCursor{
		Filter:    sessionFilterActive,
		UpdatedAt: validPage.Sessions[0].UpdatedAt.AsTime().UTC().Format("2006-01-02T15:04:05.000000000Z"),
		SessionID: validPage.Sessions[0].SessionId,
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, request := range map[string]*turingv1.ListSessionsRequest{
		"malformed": {
			Page: &turingv1.PageRequest{Limit: 1, Cursor: "not-base64!"},
		},
		"padded": {
			Page: &turingv1.PageRequest{Limit: 1, Cursor: validCursor + "="},
		},
		"wrong signing key": {
			Page: &turingv1.PageRequest{Limit: 1, Cursor: foreign},
		},
		"foreign filter": {
			Page:   &turingv1.PageRequest{Limit: 1, Cursor: validCursor},
			Filter: turingv1.SessionListFilter_SESSION_LIST_FILTER_ARCHIVED,
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := client.ListSessions(ctx, request)
			if status.Code(err) != codes.InvalidArgument ||
				status.Convert(err).Message() != "page.cursor is invalid" {
				t.Fatalf("cursor error = %v", err)
			}
		})
	}
	_ = session
}

func TestSessionLifecycleRPCsValidatePublishAndReconcileVisibility(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()

	created, err := client.CreateSession(ctx, &turingv1.CreateSessionRequest{Title: "  Initial  "})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := h.repo.GetSession(ctx, created.SessionId)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Title.String != "Initial" || stored.TitleOrigin != "explicit" {
		t.Fatalf("created session = %+v", stored)
	}
	empty, err := client.CreateSession(ctx, &turingv1.CreateSessionRequest{Title: " \n "})
	if err != nil {
		t.Fatal(err)
	}
	emptyStored, err := h.repo.GetSession(ctx, empty.SessionId)
	if err != nil {
		t.Fatal(err)
	}
	if emptyStored.Title.Valid || emptyStored.TitleOrigin != "unset" {
		t.Fatalf("empty-title session = %+v", emptyStored)
	}
	if _, err := client.CreateSession(ctx, &turingv1.CreateSessionRequest{
		Title: strings.Repeat("x", repository.MaxSessionTitleRunes+1),
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("oversize create title error = %v", err)
	}

	events, unsubscribe := h.bus.Subscribe(created.SessionId)
	defer unsubscribe()
	renamed, err := client.RenameSession(ctx, &turingv1.RenameSessionRequest{
		SessionId: created.SessionId,
		Title:     "  Renamed  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if renamed.GetSession().GetTitle() != "Renamed" {
		t.Fatalf("rename response = %+v", renamed)
	}
	assertLifecycleBusEvent(t, events, "active", "Renamed")

	if _, err := client.RenameSession(ctx, &turingv1.RenameSessionRequest{
		SessionId: created.SessionId,
		Title:     "Renamed",
	}); err != nil {
		t.Fatal(err)
	}
	assertNoLifecycleBusEvent(t, events)

	archived, err := client.ArchiveSession(ctx, &turingv1.ArchiveSessionRequest{SessionId: created.SessionId})
	if err != nil {
		t.Fatal(err)
	}
	if archived.GetSession().GetStatus() != "archived" {
		t.Fatalf("archive response = %+v", archived)
	}
	assertLifecycleBusEvent(t, events, "archived", "Renamed")
	if _, err := client.GetSession(ctx, &turingv1.GetSessionRequest{SessionId: created.SessionId}); err != nil {
		t.Fatalf("get archived session: %v", err)
	}
	active, err := client.ListSessions(ctx, &turingv1.ListSessionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range active.Sessions {
		if session.SessionId == created.SessionId {
			t.Fatal("archived session remained in default active list")
		}
	}
	archivedPage, err := client.ListSessions(ctx, &turingv1.ListSessionsRequest{
		Filter: turingv1.SessionListFilter_SESSION_LIST_FILTER_ARCHIVED,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertProtoSessionIDs(t, archivedPage.Sessions, []string{created.SessionId})

	if _, err := client.ArchiveSession(ctx, &turingv1.ArchiveSessionRequest{SessionId: created.SessionId}); err != nil {
		t.Fatal(err)
	}
	assertNoLifecycleBusEvent(t, events)
	restored, err := client.RestoreSession(ctx, &turingv1.RestoreSessionRequest{SessionId: created.SessionId})
	if err != nil {
		t.Fatal(err)
	}
	if restored.GetSession().GetStatus() != "active" {
		t.Fatalf("restore response = %+v", restored)
	}
	assertLifecycleBusEvent(t, events, "active", "Renamed")

	if _, err := client.ArchiveSession(ctx, &turingv1.ArchiveSessionRequest{SessionId: empty.SessionId}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: empty.SessionId}); err != nil {
		t.Fatalf("delete archived session: %v", err)
	}
}

func TestSessionLifecycleRPCsHonorDeletionPrecedence(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "Withdrawing")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.BeginSessionDeletion(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}

	for _, filter := range []turingv1.SessionListFilter{
		turingv1.SessionListFilter_SESSION_LIST_FILTER_ACTIVE,
		turingv1.SessionListFilter_SESSION_LIST_FILTER_ARCHIVED,
		turingv1.SessionListFilter_SESSION_LIST_FILTER_ALL,
	} {
		page, err := client.ListSessions(ctx, &turingv1.ListSessionsRequest{Filter: filter})
		if err != nil {
			t.Fatal(err)
		}
		for _, listed := range page.Sessions {
			if listed.SessionId == session.SessionID {
				t.Fatalf("filter %v returned deleting session", filter)
			}
		}
	}
	if _, err := client.GetSession(ctx, &turingv1.GetSessionRequest{
		SessionId: session.SessionID,
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("GetSession error = %v, want NotFound", err)
	}
	if _, err := client.ListMessages(ctx, &turingv1.ListMessagesRequest{
		SessionId: session.SessionID,
		Limit:     10,
	}); status.Code(err) != codes.NotFound || status.Convert(err).Message() != "session not found" {
		t.Fatalf("ListMessages error = %v, want NotFound session not found", err)
	}
	for name, operation := range map[string]func() error{
		"rename": func() error {
			_, err := client.RenameSession(ctx, &turingv1.RenameSessionRequest{
				SessionId: session.SessionID,
				Title:     "Withdrawing",
			})
			return err
		},
		"archive": func() error {
			_, err := client.ArchiveSession(ctx, &turingv1.ArchiveSessionRequest{
				SessionId: session.SessionID,
			})
			return err
		},
		"restore": func() error {
			_, err := client.RestoreSession(ctx, &turingv1.RestoreSessionRequest{
				SessionId: session.SessionID,
			})
			return err
		},
	} {
		if err := operation(); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("%s error = %v, want FailedPrecondition", name, err)
		}
	}
}

func TestSessionLifecycleRPCsValidateIDsAndUnknownSessions(t *testing.T) {
	h := newSessionHarness(t)
	ctx := context.Background()
	invalidSessionIDs := []string{"", "line\nbreak", strings.Repeat("x", 257), string([]byte{0xff})}
	for _, sessionID := range invalidSessionIDs {
		if _, err := h.service.GetSession(ctx, &turingv1.GetSessionRequest{SessionId: sessionID}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("GetSession(%q) error = %v, want InvalidArgument", sessionID, err)
		}
		for name, operation := range map[string]func() error{
			"rename": func() error {
				_, err := h.service.RenameSession(ctx, &turingv1.RenameSessionRequest{SessionId: sessionID, Title: "Title"})
				return err
			},
			"archive": func() error {
				_, err := h.service.ArchiveSession(ctx, &turingv1.ArchiveSessionRequest{SessionId: sessionID})
				return err
			},
			"restore": func() error {
				_, err := h.service.RestoreSession(ctx, &turingv1.RestoreSessionRequest{SessionId: sessionID})
				return err
			},
		} {
			if err := operation(); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("%s(%q) error = %v, want InvalidArgument", name, sessionID, err)
			}
		}
	}
	for name, operation := range map[string]func() error{
		"rename": func() error {
			_, err := h.service.RenameSession(ctx, nil)
			return err
		},
		"archive": func() error {
			_, err := h.service.ArchiveSession(ctx, nil)
			return err
		},
		"restore": func() error {
			_, err := h.service.RestoreSession(ctx, nil)
			return err
		},
	} {
		if err := operation(); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%s(nil) error = %v, want InvalidArgument", name, err)
		}
	}
	for _, operation := range []func() error{
		func() error {
			_, err := h.service.RenameSession(ctx, &turingv1.RenameSessionRequest{SessionId: "missing", Title: "Title"})
			return err
		},
		func() error {
			_, err := h.service.ArchiveSession(ctx, &turingv1.ArchiveSessionRequest{SessionId: "missing"})
			return err
		},
		func() error {
			_, err := h.service.RestoreSession(ctx, &turingv1.RestoreSessionRequest{SessionId: "missing"})
			return err
		},
	} {
		if err := operation(); status.Code(err) != codes.NotFound {
			t.Fatalf("unknown lifecycle session error = %v, want NotFound", err)
		}
	}
}

func TestRenameSessionRPCEnforcesUnicodeScalarTitleLimit(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	created, err := client.CreateSession(ctx, &turingv1.CreateSessionRequest{})
	if err != nil {
		t.Fatal(err)
	}

	maxTitle := strings.Repeat("😀", repository.MaxSessionTitleRunes)
	renamed, err := client.RenameSession(ctx, &turingv1.RenameSessionRequest{
		SessionId: created.SessionId,
		Title:     maxTitle,
	})
	if err != nil {
		t.Fatalf("rename with maximum title length: %v", err)
	}
	if renamed.GetSession().GetTitle() != maxTitle {
		t.Fatalf("rename title = %q, want %q", renamed.GetSession().GetTitle(), maxTitle)
	}

	for name, title := range map[string]string{
		"empty":          "",
		"whitespace":     " \n\t ",
		"too many runes": maxTitle + "x",
		"invalid UTF-8":  string([]byte{0xff}),
	} {
		_, err := h.service.RenameSession(ctx, &turingv1.RenameSessionRequest{
			SessionId: created.SessionId,
			Title:     title,
		})
		if status.Code(err) != codes.InvalidArgument ||
			status.Convert(err).Message() != "title is invalid" {
			t.Fatalf("%s rename error = %v, want value-free InvalidArgument", name, err)
		}
	}
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

	customServer, err := h.repo.RegisterMCPServer(ctx, repository.ImportedMCPServer{
		Name: "custom", URL: "http://custom:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.SetMCPServerEnabled(ctx, customServer.Server.ID, true); err != nil {
		t.Fatal(err)
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
	if len(gotTools) != 2 {
		t.Fatalf("tools = %+v, want only callable database snapshot", tools.Tools)
	}
	if gotTools["system/system.time"] != turingv1.ToolPolicy_TOOL_POLICY_SAFE {
		t.Fatalf("system.time policy = %v", gotTools["system/system.time"])
	}
	if _, present := gotTools["files/files.create"]; present {
		t.Fatal("disabled files.create was returned as callable")
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

func TestDeleteSessionStartsWithdrawalForLiveRun(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "Delete live run")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "cancel before deletion", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.ClaimNextJob(ctx, "general_assistant", "worker-delete-live"); err != nil {
		t.Fatal(err)
	}

	response, err := client.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if response.SessionId != session.SessionID {
		t.Fatalf("DeleteSession response = %+v, want session id %q", response, session.SessionID)
	}
	if response.Deletion == nil || response.Deletion.State != turingv1.SessionDeletionState_SESSION_DELETION_STATE_IN_PROGRESS {
		t.Fatalf("DeleteSession receipt = %+v, want in-progress receipt", response.Deletion)
	}
	if got := h.capabilities.cancelledSessions; len(got) != 1 || got[0] != session.SessionID {
		t.Fatalf("runtime cancellation sessions = %v, want [%s]", got, session.SessionID)
	}
}

func TestDeleteSessionPublishesTerminalEventAfterCompletedWithdrawal(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	bus := eventsvc.NewBus(1)
	server := New(repo, config.Config{}, &sessionCapabilitySource{}, bus)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Delete terminal")
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := bus.Subscribe(session.SessionID)
	defer unsubscribe()

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if response.Deletion.GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("DeleteSession receipt = %+v, want completed", response.Deletion)
	}
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("terminal event channel closed before delivery")
		}
		if event.Type != "session.deleted" || event.Sequence != response.Deletion.TerminalSequence {
			t.Fatalf("terminal event = %+v, receipt = %+v", event, response.Deletion)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for terminal deletion event")
	}
	if _, ok := <-events; ok {
		t.Fatal("terminal deletion event stream remained open")
	}
}

func TestDeleteSessionCompletesAfterArtifactCleanerRemovesOwnedNamespace(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	cleaner := &recordingArtifactCleaner{}
	server.SetArtifactCleaner(cleaner)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Delete owned artifact")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "write then withdraw", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sandbox_artifacts (
			id, session_id, run_id, logical_path_hash, physical_path, state, policy,
			deletion_generation, created_at
		) VALUES (?, ?, ?, ?, ?, 'ready', 'delete_on_session_delete', 0, ?)
	`,
		"artifact_service_cleanup",
		session.SessionID,
		enqueued.RunID,
		"sha256:service",
		"sessions/"+session.SessionID+"/runs/"+enqueued.RunID+"/files/note.txt",
		repository.FormatTimestamp(time.Now()),
	); err != nil {
		t.Fatal(err)
	}

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("deletion receipt = %+v, want completed", response.GetDeletion())
	}
	if response.GetDeletion().GetErrorCode() != "" {
		t.Fatalf("completed receipt error code = %q, want empty", response.GetDeletion().GetErrorCode())
	}
	if cleaner.sessionID != session.SessionID || cleaner.calls != 1 {
		t.Fatalf("cleaner calls = %+v, want one call for %q", cleaner, session.SessionID)
	}
}

func TestDeleteSessionCountsRetainedLegacyArtifactWithoutDeletingIt(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Retain legacy artifact")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "touch legacy", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sandbox_artifacts (
			id, session_id, run_id, logical_path_hash, physical_path, state, policy,
			deletion_generation, created_at
		) VALUES (?, ?, ?, ?, ?, 'ready', 'retain_legacy_unowned', 0, ?)
	`,
		"artifact_legacy_retained",
		session.SessionID,
		enqueued.RunID,
		"sha256:legacy",
		"legacy.txt",
		repository.FormatTimestamp(time.Now()),
	); err != nil {
		t.Fatal(err)
	}

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("deletion receipt = %+v, want completed", response.GetDeletion())
	}
	if got := response.GetDeletion().GetRetainedLegacyArtifactCount(); got != 1 {
		t.Fatalf("retained legacy artifact count = %d, want 1", got)
	}
}

func TestDeleteSessionRetainsFailedExternalReceiptWhenArtifactCleanerFails(t *testing.T) {
	database := openSessionTestDB(t)
	repo := repository.New(database)
	server := New(repo, config.Config{}, &sessionCapabilitySource{})
	cleaner := &flakyArtifactCleaner{err: errors.New("cleanup transport unavailable")}
	server.SetArtifactCleaner(cleaner)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Fail artifact cleanup")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "write then fail", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `
		INSERT INTO sandbox_artifacts (
			id, session_id, run_id, logical_path_hash, physical_path, state, policy,
			deletion_generation, created_at
		) VALUES (?, ?, ?, ?, ?, 'ready', 'delete_on_session_delete', 0, ?)
	`,
		"artifact_cleanup_failure",
		session.SessionID,
		enqueued.RunID,
		"sha256:failure",
		"sessions/"+session.SessionID+"/runs/"+enqueued.RunID+"/files/note.txt",
		repository.FormatTimestamp(time.Now()),
	); err != nil {
		t.Fatal(err)
	}

	response, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if response.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_FAILED_EXTERNAL ||
		response.GetDeletion().GetErrorCode() != "artifact_cleanup_failed" ||
		!response.GetDeletion().GetRetryable() {
		t.Fatalf("failed cleanup receipt = %+v", response.GetDeletion())
	}
	persisted, err := repo.SessionDeletionReceipt(ctx, session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ErrorCode != "artifact_cleanup_failed" {
		t.Fatalf("persisted cleanup error = %q, want artifact_cleanup_failed", persisted.ErrorCode)
	}
	var auditPayload string
	if err := database.QueryRowContext(ctx, `
		SELECT payload_json
		FROM audit_logs
		WHERE action = 'session.artifact.cleanup.failed'
			AND target = 'artifact_cleanup_failure'
	`).Scan(&auditPayload); err != nil {
		t.Fatalf("cleanup failure audit record: %v", err)
	}
	if strings.Contains(auditPayload, "note.txt") || strings.Contains(auditPayload, "sessions/") {
		t.Fatalf("cleanup failure audit leaked a path: %q", auditPayload)
	}
	cleaner.err = nil
	retry, err := server.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("retry DeleteSession: %v", err)
	}
	if retry.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED ||
		cleaner.calls != 2 {
		t.Fatalf("retry receipt = %+v cleaner calls=%d", retry.GetDeletion(), cleaner.calls)
	}
}

type recordingArtifactCleaner struct {
	sessionID string
	calls     int
}

type flakyArtifactCleaner struct {
	calls int
	err   error
}

func (c *flakyArtifactCleaner) CleanupSessionArtifacts(context.Context, string, int64) error {
	c.calls++
	return c.err
}

func (c *recordingArtifactCleaner) CleanupSessionArtifacts(_ context.Context, sessionID string, _ int64) error {
	c.calls++
	c.sessionID = sessionID
	return nil
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

	_, err := h.service.SearchMessages(ctx, nil)
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
	if len(response.GetHits()) != 0 {
		t.Fatalf("SearchMessages punctuation-only hits = %+v, want none", response.GetHits())
	}

	hits, err := client.SearchMessages(ctx, &turingv1.SearchMessagesRequest{
		Query:          "...",
		ResponseFormat: turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_HITS,
	})
	if err != nil {
		t.Fatalf("SearchMessages punctuation-only hit query: %v", err)
	}
	if len(hits.GetHits()) != 0 || len(hits.GetMessages()) != 0 {
		t.Fatalf("SearchMessages punctuation-only hit response = %+v, want empty", hits)
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

// The zero value of the response format enum is the legacy projection, so a
// client built before hits existed keeps getting whole messages and nothing
// else.
func TestSessionServiceSearchMessagesDefaultsToLegacyMessages(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	insertServiceSearchSession(t, ctx, h.database, "session-legacy")
	insertServiceSearchMessage(t, ctx, h.database, "message-legacy", "session-legacy", "user", "legacyneedle alpha", 1)

	for name, format := range map[string]turingv1.SearchMessagesResponseFormat{
		"unspecified": turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_UNSPECIFIED,
		"legacy":      turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_LEGACY_MESSAGES,
	} {
		t.Run(name, func(t *testing.T) {
			response, err := client.SearchMessages(ctx, &turingv1.SearchMessagesRequest{
				Query:          "legacyneedle",
				Limit:          10,
				ResponseFormat: format,
			})
			if err != nil {
				t.Fatalf("SearchMessages: %v", err)
			}
			if len(response.GetMessages()) != 1 || response.GetMessages()[0].GetMessageId() != "message-legacy" {
				t.Fatalf("messages = %+v, want only message-legacy", response.GetMessages())
			}
			if len(response.GetHits()) != 0 {
				t.Fatalf("hits = %+v, want none for the legacy projection", response.GetHits())
			}
		})
	}
}

// A hit response carries the repository's own score and snippet. The service is
// a mapper here: it may not round, rescale, or regenerate metadata, because a
// second opinion about a match is not the match.
func TestSessionServiceSearchMessagesReturnsOnlyHitsWhenRequested(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	insertServiceSearchSession(t, ctx, h.database, "session-hit-a")
	insertServiceSearchSession(t, ctx, h.database, "session-hit-b")
	insertServiceSearchMessage(t, ctx, h.database, "message-hit-a", "session-hit-a", "assistant", "hitneedle", 1)
	insertServiceSearchMessage(t, ctx, h.database, "message-hit-b", "session-hit-b", "user", "hitneedle with a longer tail of unrelated words", 1)
	if _, err := h.database.ExecContext(ctx, `UPDATE messages SET run_id = 'run-hit-a' WHERE id = 'message-hit-a'`); err != nil {
		t.Fatalf("set message run_id: %v", err)
	}

	want, err := h.repo.SearchMessageHits(ctx, "", "", "hitneedle", 10)
	if err != nil {
		t.Fatalf("repository SearchMessageHits: %v", err)
	}
	if len(want) != 2 {
		t.Fatalf("repository hits = %d, want 2", len(want))
	}

	response, err := client.SearchMessages(ctx, &turingv1.SearchMessagesRequest{
		Query:          "hitneedle",
		Limit:          10,
		ResponseFormat: turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_HITS,
	})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(response.GetMessages()) != 0 {
		t.Fatalf("messages = %+v, want none for the hit projection", response.GetMessages())
	}
	if len(response.GetHits()) != len(want) {
		t.Fatalf("hits = %d, want %d", len(response.GetHits()), len(want))
	}
	for index, hit := range response.GetHits() {
		wantHit := want[index]
		wantMessage := mapMessage(wantHit.Message.SessionID, wantHit.Message)
		if !proto.Equal(hit.GetMessage(), wantMessage) {
			t.Fatalf("hits[%d].message = %+v, want %+v", index, hit.GetMessage(), wantMessage)
		}
		// Scores are only comparable if the repository actually produced one:
		// against an all-zero fixture the equality below would hold no matter
		// what the service did with the value.
		if !(wantHit.Score > 0) { // NaN fails this too, which `<= 0` would let through
			t.Fatalf("repository hits[%d].score = %v, want a positive score so equality proves something", index, wantHit.Score)
		}
		if hit.GetScore() != wantHit.Score {
			t.Fatalf("hits[%d].score = %v, want the repository score %v", index, hit.GetScore(), wantHit.Score)
		}
		if hit.GetSnippet() != wantHit.Snippet {
			t.Fatalf("hits[%d].snippet = %q, want the repository snippet %q", index, hit.GetSnippet(), wantHit.Snippet)
		}
		if hit.GetSnippet() == "" {
			t.Fatalf("hits[%d] carries no snippet, so snippet equality proved nothing", index)
		}
	}
}

// An unknown format is a client the server does not understand, not a client to
// guess at: guessing would silently answer a future "hits only" request with
// legacy messages. The rejection names the field and nothing else, so a probe
// cannot use it to enumerate values.
func TestSessionServiceSearchMessagesRejectsUnknownResponseFormat(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	insertServiceSearchSession(t, ctx, h.database, "session-unknown")
	insertServiceSearchMessage(t, ctx, h.database, "message-unknown", "session-unknown", "user", "unknownneedle", 1)

	response, err := client.SearchMessages(ctx, &turingv1.SearchMessagesRequest{
		Query:          "unknownneedle",
		Limit:          10,
		ResponseFormat: turingv1.SearchMessagesResponseFormat(99),
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("SearchMessages error = %v (response %+v), want InvalidArgument", err, response)
	}
	message := status.Convert(err).Message()
	// Exact equality is the echo check: any rendering of the rejected value
	// would change this string, so a separate "does it contain 99" assertion
	// could never fail on its own.
	if message != "response_format is invalid" {
		t.Fatalf("SearchMessages message = %q, want %q", message, "response_format is invalid")
	}
}

// The two projections answer the same question and must never disagree about
// which messages a query can see. Each case runs both formats against the same
// unchanged fixture and compares the messages themselves, in order.
func TestSessionServiceSearchMessagesFormatsHaveMessageParity(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	insertServiceSearchSession(t, ctx, h.database, "parity-a")
	insertServiceSearchSession(t, ctx, h.database, "parity-b")
	insertServiceSearchSession(t, ctx, h.database, "parity-archived")
	if _, err := h.database.ExecContext(ctx, `UPDATE sessions SET status = 'archived' WHERE id = 'parity-archived'`); err != nil {
		t.Fatalf("archive session: %v", err)
	}
	insertServiceSearchMessage(t, ctx, h.database, "parity-1", "parity-a", "user", "parityneedle tiedcontent", 1)
	insertServiceSearchMessage(t, ctx, h.database, "parity-2", "parity-a", "assistant", "parityneedle with several additional trailing words that dilute the score", 2)
	insertServiceSearchMessage(t, ctx, h.database, "parity-3", "parity-b", "user", "parityneedle tiedcontent", 1)
	insertServiceSearchMessage(t, ctx, h.database, "parity-4", "parity-archived", "user", "parityneedle archivedmarker", 1)
	insertServiceSearchMessage(t, ctx, h.database, "parity-5", "parity-a", "tool", "unrelated content", 3)

	for _, testCase := range []struct {
		name      string
		request   *turingv1.SearchMessagesRequest
		wantCount int
	}{
		{
			name:      "ranked",
			request:   &turingv1.SearchMessagesRequest{Query: "parityneedle", Limit: 10},
			wantCount: 4,
		},
		{
			name:      "tied",
			request:   &turingv1.SearchMessagesRequest{Query: "tiedcontent", Limit: 10},
			wantCount: 2,
		},
		{
			name:      "scoped",
			request:   &turingv1.SearchMessagesRequest{Query: "parityneedle", SessionId: "parity-a", Limit: 10},
			wantCount: 2,
		},
		{
			name:      "excluded",
			request:   &turingv1.SearchMessagesRequest{Query: "parityneedle", ExcludeSessionId: "parity-a", Limit: 10},
			wantCount: 2,
		},
		{
			name:      "archived",
			request:   &turingv1.SearchMessagesRequest{Query: "archivedmarker", Limit: 10},
			wantCount: 1,
		},
		{
			name:      "limited",
			request:   &turingv1.SearchMessagesRequest{Query: "parityneedle", Limit: 1},
			wantCount: 1,
		},
		{
			name:      "empty",
			request:   &turingv1.SearchMessagesRequest{Query: "absentneedle", Limit: 10},
			wantCount: 0,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			legacyRequest := proto.Clone(testCase.request).(*turingv1.SearchMessagesRequest)
			legacyRequest.ResponseFormat = turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_LEGACY_MESSAGES
			legacy, err := client.SearchMessages(ctx, legacyRequest)
			if err != nil {
				t.Fatalf("SearchMessages legacy: %v", err)
			}
			hitRequest := proto.Clone(testCase.request).(*turingv1.SearchMessagesRequest)
			hitRequest.ResponseFormat = turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_HITS
			hits, err := client.SearchMessages(ctx, hitRequest)
			if err != nil {
				t.Fatalf("SearchMessages hits: %v", err)
			}

			if len(legacy.GetMessages()) != testCase.wantCount {
				t.Fatalf("legacy messages = %d, want %d: %+v", len(legacy.GetMessages()), testCase.wantCount, legacy.GetMessages())
			}
			if len(hits.GetHits()) != testCase.wantCount {
				t.Fatalf("hits = %d, want %d: %+v", len(hits.GetHits()), testCase.wantCount, hits.GetHits())
			}
			for index, message := range legacy.GetMessages() {
				if !proto.Equal(hits.GetHits()[index].GetMessage(), message) {
					t.Fatalf("hits[%d].message = %+v, want the legacy message %+v", index, hits.GetHits()[index].GetMessage(), message)
				}
			}
		})
	}
}

// A phrase longer than FTS5's 32-token snippet window has no in-window
// occurrence for `snippet()` to mark, so the marked projection comes back
// without markers. That is a windowing outcome, not a broken invariant: both
// formats must still answer with the same message, and the hit format must
// carry a bounded excerpt instead of an opaque Internal error.
func TestSessionServiceSearchMessagesReturnsBoundedSnippetForOverWindowPhrase(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	words := func(format string, count int) []string {
		out := make([]string, 0, count)
		for i := 0; i < count; i++ {
			out = append(out, fmt.Sprintf(format, i))
		}
		return out
	}
	phrase := strings.Join(words("overwindowword%02d", 40), " ")
	content := strings.Join(words("overwindowfiller%03d", 100), " ") +
		" " + phrase + " " + strings.Join(words("overwindowtail%02d", 20), " ")
	insertServiceSearchSession(t, ctx, h.database, "session-over-window")
	insertServiceSearchMessage(t, ctx, h.database, "message-over-window", "session-over-window", "user", content, 1)

	legacy, err := client.SearchMessages(ctx, &turingv1.SearchMessagesRequest{
		Query:          phrase,
		Limit:          10,
		ResponseFormat: turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_LEGACY_MESSAGES,
	})
	if err != nil {
		t.Fatalf("SearchMessages legacy: %v", err)
	}
	if len(legacy.GetMessages()) != 1 {
		t.Fatalf("legacy messages = %+v, want exactly the buried-phrase message", legacy.GetMessages())
	}

	hits, err := client.SearchMessages(ctx, &turingv1.SearchMessagesRequest{
		Query:          phrase,
		Limit:          10,
		ResponseFormat: turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_HITS,
	})
	if err != nil {
		t.Fatalf("SearchMessages hits: %v (code %v)", err, status.Code(err))
	}
	if len(hits.GetHits()) != 1 {
		t.Fatalf("hits = %+v, want exactly one", hits.GetHits())
	}
	if !proto.Equal(hits.GetHits()[0].GetMessage(), legacy.GetMessages()[0]) {
		t.Fatalf("hit message = %+v, want the legacy message %+v",
			hits.GetHits()[0].GetMessage(), legacy.GetMessages()[0])
	}
	snippet := hits.GetHits()[0].GetSnippet()
	if snippet == "" {
		t.Fatal("hit carries no snippet for an over-window phrase")
	}
	if utf8.RuneCountInString(snippet) > 200 || len(snippet) > 800 {
		t.Fatalf("snippet bounds: runes=%d bytes=%d", utf8.RuneCountInString(snippet), len(snippet))
	}
	if strings.Contains(snippet, "TURING-FTS5-SNIPPET") {
		t.Fatalf("snippet = %q leaks internal markers", snippet)
	}
	excerpt := strings.TrimSpace(strings.Trim(snippet, "…"))
	if excerpt == "" || !strings.Contains(content, excerpt) {
		t.Fatalf("snippet = %q is not a fragment of its own message", snippet)
	}
}

// A metadata invariant failure is a bug report the operator needs, and the row
// that triggered it is the user's private conversation. The log records which
// invariant broke and nothing about what broke it.
func TestSessionServiceSearchMessagesLogsOnlyInvariantClass(t *testing.T) {
	const (
		sessionSentinel = "sessionsentinel"
		querySentinel   = "querysentinel"
		messageSentinel = "messagesentinel"
		snippetSentinel = "snippetsentinel"
		markerSentinel  = "markersentinel"
		scoreSentinel   = "13.75"
	)
	contextual := func(cause error) error {
		return fmt.Errorf(
			"session %s query %s message %s snippet %s marker %s score %s: %w",
			sessionSentinel, querySentinel, messageSentinel, snippetSentinel, markerSentinel, scoreSentinel, cause,
		)
	}

	for _, testCase := range []struct {
		name  string
		cause error
		class string
	}{
		{name: "entropy", cause: repository.ErrSearchMarkerEntropy, class: "marker_entropy"},
		{name: "score", cause: repository.ErrInvalidSearchScore, class: "invalid_score"},
		{name: "collision", cause: repository.ErrSearchSnippetMarkerCollision, class: "marker_collision"},
		{name: "markers", cause: repository.ErrInvalidSearchSnippetMarkers, class: "marker_structure"},
		{name: "snippet", cause: repository.ErrInvalidSearchSnippet, class: "invalid_snippet"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			logged := captureSessionLog(t, func() {
				err := searchMessagesError(contextual(testCase.cause))
				if status.Code(err) != codes.Internal {
					t.Fatalf("searchMessagesError code = %v, want Internal", status.Code(err))
				}
				if got := status.Convert(err).Message(); got != "search messages failed" {
					t.Fatalf("searchMessagesError message = %q, want %q", got, "search messages failed")
				}
			})

			if !strings.Contains(logged, "search_messages") {
				t.Fatalf("log = %q, want the operation name", logged)
			}
			if !strings.Contains(logged, testCase.class) {
				t.Fatalf("log = %q, want the invariant class %q", logged, testCase.class)
			}
			for _, secret := range []string{
				sessionSentinel, querySentinel, messageSentinel, snippetSentinel,
				markerSentinel, scoreSentinel, testCase.cause.Error(),
			} {
				if strings.Contains(logged, secret) {
					t.Fatalf("log = %q leaked %q", logged, secret)
				}
			}
		})
	}

	t.Run("ordinary database error", func(t *testing.T) {
		logged := captureSessionLog(t, func() {
			err := searchMessagesError(errors.New("no such table: messages_fts"))
			if status.Code(err) != codes.Internal {
				t.Fatalf("searchMessagesError code = %v, want Internal", status.Code(err))
			}
			if got := status.Convert(err).Message(); got != "search messages failed" {
				t.Fatalf("searchMessagesError message = %q, want %q", got, "search messages failed")
			}
		})
		if logged != "" {
			t.Fatalf("log = %q, want nothing for an ordinary database error", logged)
		}
	})
}

// A metadata invariant failure has to survive the whole handler path, not just
// the mapper: the RPC answers with the same opaque status any other failure
// gets, and the only trace is the invariant's class name. A real database
// cannot be made to break its own score invariant on demand, so the search
// seam is replaced with a repository that fails that way, wrapped in the kind
// of context an error usually accumulates.
func TestSessionServiceSearchMessagesHitFormatLogsOnlyInvariantClassOverRPC(t *testing.T) {
	const (
		secretQuery   = "hitinvariantquerysecret"
		secretContent = "hitinvariantcontentsecret"
	)
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	stub := &failingMessageSearcher{
		err: fmt.Errorf(
			"search %q matched %q: %w",
			secretQuery, secretContent, repository.ErrInvalidSearchScore,
		),
	}
	h.service.search = stub

	var err error
	logged := captureSessionLog(t, func() {
		_, err = client.SearchMessages(context.Background(), &turingv1.SearchMessagesRequest{
			Query:          secretQuery,
			Limit:          10,
			ResponseFormat: turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_HITS,
		})
	})

	if stub.hitCalls != 1 {
		t.Fatalf("SearchMessageHits calls = %d, want 1", stub.hitCalls)
	}
	if stub.legacyCalls != 0 {
		t.Fatalf("SearchMessages legacy calls = %d, want none for the hit projection", stub.legacyCalls)
	}
	if status.Code(err) != codes.Internal {
		t.Fatalf("SearchMessages error = %v, want Internal", err)
	}
	if got := status.Convert(err).Message(); got != "search messages failed" {
		t.Fatalf("SearchMessages message = %q, want %q", got, "search messages failed")
	}
	if !strings.Contains(logged, "search_messages invariant=invalid_score") {
		t.Fatalf("log = %q, want the invariant class for a rejected score", logged)
	}
	for _, secret := range []string{secretQuery, secretContent, stub.err.Error()} {
		if strings.Contains(logged, secret) {
			t.Fatalf("log = %q leaked %q", logged, secret)
		}
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("status = %v leaked %q", err, secret)
		}
	}
}

// failingMessageSearcher answers both projections with one prepared failure.
// It satisfies messageSearcher, so it can only stand in for the repository as
// long as the handler asks the repository exactly what production asks it.
type failingMessageSearcher struct {
	err         error
	legacyCalls int
	hitCalls    int
}

func (f *failingMessageSearcher) SearchMessages(
	context.Context, string, string, string, int,
) ([]repository.Message, error) {
	f.legacyCalls++
	return nil, f.err
}

func (f *failingMessageSearcher) SearchMessageHits(
	context.Context, string, string, string, int,
) ([]repository.SearchHit, error) {
	f.hitCalls++
	return nil, f.err
}

// The hit projection must route its failures through the same content-free
// mapper the legacy projection uses, rather than growing its own error path.
func TestSessionServiceSearchMessagesHidesDatabaseErrorsInHitFormat(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	if err := h.database.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	var err error
	logged := captureSessionLog(t, func() {
		_, err = client.SearchMessages(context.Background(), &turingv1.SearchMessagesRequest{
			Query:          "needle",
			ResponseFormat: turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_HITS,
		})
	})
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
	// An ordinary database failure is not an invariant report, so it produces
	// no log line at all. Requiring emptiness rather than the absence of a few
	// keywords also catches a future line that leaks content this test never
	// thought to name.
	if logged != "" {
		t.Fatalf("log = %q, want nothing for an ordinary database error in the hit projection", logged)
	}
}

// A legacy response near the transport's own size ceiling proves the two
// projections are exclusive rather than additive: answering it with messages
// and hits together would double the payload past what a client can receive.
func TestSessionServiceSearchMessagesDoesNotDuplicateLargeLegacyPayload(t *testing.T) {
	h := newSessionHarness(t)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()
	insertServiceSearchSession(t, ctx, h.database, "session-large")
	const (
		largeMessages = 8
		filler        = "padding "
		fillerRepeats = 55000
	)
	content := "hugeneedle " + strings.Repeat(filler, fillerRepeats)
	for index := 1; index <= largeMessages; index++ {
		insertServiceSearchMessage(
			t, ctx, h.database,
			fmt.Sprintf("message-large-%d", index), "session-large", "user", content, int64(index),
		)
	}

	response, err := client.SearchMessages(ctx, &turingv1.SearchMessagesRequest{Query: "hugeneedle", Limit: 10})
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	if len(response.GetMessages()) != largeMessages {
		t.Fatalf("messages = %d, want %d", len(response.GetMessages()), largeMessages)
	}
	if len(response.GetHits()) != 0 {
		t.Fatalf("hits = %d, want none alongside a large legacy payload", len(response.GetHits()))
	}
	for index, message := range response.GetMessages() {
		if got := message.GetContent(); got != content {
			t.Fatalf(
				"messages[%d] content mismatch: got %d bytes, want %d bytes, first difference at byte %d",
				index, len(got), len(content), firstByteDifference(got, content),
			)
		}
	}
	size := proto.Size(response)
	const maxReceiveBytes = 4 * 1024 * 1024
	if size <= maxReceiveBytes/2 {
		t.Fatalf("response size = %d bytes, want a payload large enough that duplication would exceed %d", size, maxReceiveBytes)
	}
	if size >= maxReceiveBytes {
		t.Fatalf("response size = %d bytes, want it under the %d receive limit", size, maxReceiveBytes)
	}
}

// firstByteDifference locates where two payloads diverge. Reporting only the
// lengths is useless when they are equal, which is exactly the case where a
// mismatch is hardest to explain. It reports an offset, never the bytes.
func firstByteDifference(got, want string) int {
	limit := min(len(got), len(want))
	for index := 0; index < limit; index++ {
		if got[index] != want[index] {
			return index
		}
	}
	return limit
}

// TUR-004: deletion is the only way a person can take back what they said, so a
// withdrawn conversation must be unreachable through every search projection,
// and must stay unreachable after the process that served the request is gone.
func TestSessionServiceSearchMessagesCannotReturnWithdrawnContentInEitherFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turing.db")
	first := openSessionTestDBAt(t, path)
	h := newSessionHarnessWithDB(t, first)
	client := turingv1.NewSessionServiceClient(h.conn)
	ctx := context.Background()

	control, err := h.repo.CreateSession(ctx, "Withdrawal control")
	if err != nil {
		t.Fatal(err)
	}
	insertServiceSearchMessage(t, ctx, h.database, "message-control", control.SessionID, "user", "withdrawneedle controlmarker", 1)
	sentinel, err := h.repo.CreateSession(ctx, "Withdrawal sentinel")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: sentinel.SessionID, Content: "withdrawneedle sentinelmarker", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	}); err != nil {
		t.Fatal(err)
	}
	// A claimed job keeps the withdrawal in progress, so the sentinel row stays
	// on disk and is hidden by the search predicate rather than by having been
	// physically removed. That is the case a leak would actually come from.
	if _, err := h.repo.ClaimNextJob(ctx, "general_assistant", "worker-withdrawal"); err != nil {
		t.Fatal(err)
	}
	assertSearchSentinelVisibility(t, ctx, client, true)

	deletion, err := client.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sentinel.SessionID})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if deletion.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_IN_PROGRESS {
		t.Fatalf("deletion receipt = %+v, want in-progress", deletion.GetDeletion())
	}
	assertSearchSentinelVisibility(t, ctx, client, false)
	// The row is hidden, not gone: a purge would make the visibility assertion
	// above pass for the wrong reason and would not prove the search predicate
	// excludes rows that are still on disk.
	assertSessionMessageRowCount(t, ctx, first, sentinel.SessionID, "sentinelmarker", 1)

	if err := first.Close(); err != nil {
		t.Fatalf("close first database: %v", err)
	}
	second := openSessionTestDBAt(t, path)
	restarted := newSessionHarnessWithDB(t, second)
	assertSearchSentinelVisibility(t, ctx, turingv1.NewSessionServiceClient(restarted.conn), false)
	assertSessionMessageRowCount(t, ctx, second, sentinel.SessionID, "sentinelmarker", 1)
}

// assertSessionMessageRowCount reads the stored rows directly, underneath every
// search predicate, so a test can tell "hidden" apart from "deleted". It counts
// only the rows carrying the marker: enqueuing a turn also writes an empty
// assistant placeholder, which says nothing about whether the withdrawn text
// survived.
func assertSessionMessageRowCount(
	t *testing.T,
	ctx context.Context,
	database *db.DB,
	sessionID string,
	marker string,
	want int,
) {
	t.Helper()
	var got int
	if err := database.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM messages WHERE session_id = ? AND instr(content, ?) > 0`,
		sessionID, marker,
	).Scan(&got); err != nil {
		t.Fatalf("count %s messages for %s: %v", marker, sessionID, err)
	}
	if got != want {
		t.Fatalf("stored %s messages for %s = %d, want %d", marker, sessionID, got, want)
	}
}

// assertSearchSentinelVisibility checks both response formats at once. The
// control message must always come back: without it, "the sentinel is absent"
// would also pass against a search that returns nothing at all.
func assertSearchSentinelVisibility(
	t *testing.T,
	ctx context.Context,
	client turingv1.SessionServiceClient,
	wantSentinel bool,
) {
	t.Helper()
	legacy, err := client.SearchMessages(ctx, &turingv1.SearchMessagesRequest{Query: "withdrawneedle", Limit: 50})
	if err != nil {
		t.Fatalf("SearchMessages legacy: %v", err)
	}
	hits, err := client.SearchMessages(ctx, &turingv1.SearchMessagesRequest{
		Query:          "withdrawneedle",
		Limit:          50,
		ResponseFormat: turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_HITS,
	})
	if err != nil {
		t.Fatalf("SearchMessages hits: %v", err)
	}

	for _, projection := range []struct {
		name     string
		contents []string
	}{
		{name: "legacy", contents: searchMessageContents(legacy.GetMessages())},
		{name: "hits", contents: searchHitContents(hits.GetHits())},
	} {
		foundControl, foundSentinel := false, false
		for _, content := range projection.contents {
			if strings.Contains(content, "controlmarker") {
				foundControl = true
			}
			if strings.Contains(content, "sentinelmarker") {
				foundSentinel = true
			}
		}
		if !foundControl {
			t.Fatalf("%s projection lost the control message: %+v", projection.name, projection.contents)
		}
		if foundSentinel != wantSentinel {
			t.Fatalf("%s projection sentinel present = %t, want %t: %+v",
				projection.name, foundSentinel, wantSentinel, projection.contents)
		}
	}
}

func searchMessageContents(messages []*turingv1.Message) []string {
	out := make([]string, 0, len(messages))
	for _, message := range messages {
		out = append(out, message.GetContent())
	}
	return out
}

func searchHitContents(hits []*turingv1.SearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.GetMessage().GetContent()+" "+hit.GetSnippet())
	}
	return out
}

// captureSessionLog redirects the standard logger for the duration of run and
// returns what was written. It restores whatever writer was installed before,
// not os.Stderr: a caller that nests captures, or a harness that already
// redirected the logger, would otherwise silently lose its own output.
func captureSessionLog(t *testing.T, run func()) string {
	t.Helper()
	var buffer bytes.Buffer
	writer := log.Writer()
	flags := log.Flags()
	log.SetOutput(&buffer)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(writer)
		log.SetFlags(flags)
	})
	run()
	return buffer.String()
}

// Tests that assert on what a failure logs share one capture helper, so it has
// to leave the logger exactly as it found it. Restoring a hardcoded os.Stderr
// would send a still-running outer capture's output to the terminal, and every
// assertion that outer capture makes would then read an empty log.
func TestCaptureSessionLogRestoresTheWriterItFound(t *testing.T) {
	outer := captureSessionLog(t, func() {
		t.Run("inner", func(t *testing.T) {
			inner := captureSessionLog(t, func() { log.Print("inner-line") })
			if !strings.Contains(inner, "inner-line") {
				t.Fatalf("inner log = %q, want the inner line", inner)
			}
		})
		// The inner capture's restore ran when the subtest ended, so this line
		// belongs to the outer capture again.
		log.Print("outer-line")
	})
	if !strings.Contains(outer, "outer-line") {
		t.Fatalf("outer log = %q, want the line written after the nested capture ended", outer)
	}
	if strings.Contains(outer, "inner-line") {
		t.Fatalf("outer log = %q, want the nested capture to have kept its own line", outer)
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
// states must be distinguishable over the wire: a client has to be able to say
// "already gone", "still reconciling", or "completed" rather than "something
// broke".
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

	// A queued run is cancelled and completed in the same non-blocking
	// withdrawal call; it is not rejected or orphaned.
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
	busyResponse, err := client.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("queued-session deletion: %v", err)
	}
	if busyResponse.GetDeletion().GetState() != turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED {
		t.Fatalf("queued-session receipt = %+v, want completed", busyResponse.GetDeletion())
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

// TestListMessagesRoundTripsRunStateAfterDatabaseReopen is the promise the
// whole feature exists for: a user closes the app while a run is finishing, and
// what they see when they come back is what actually happened — read from the
// file, not from anything the previous process still held in memory.
func TestListMessagesRoundTripsRunStateAfterDatabaseReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turing.db")
	ctx := context.Background()
	database, err := db.Open(path)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.ApplyMigrations(ctx, database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	repo := repository.New(database)
	session, err := repo.CreateSession(ctx, "Reopened outcome")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "what happened?", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	running, err := repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := repo.CompleteRunCanonical(ctx, repository.CompleteRunInput{
		RunID:                enqueued.RunID,
		AssistantMessageID:   enqueued.AssistantMessageID,
		Content:              "it finished",
		ExpectedStateVersion: running.StateVersion,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := db.Open(path)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	h := newSessionHarnessWithDB(t, reopened)
	listed, err := turingv1.NewSessionServiceClient(h.conn).ListMessages(ctx, &turingv1.ListMessagesRequest{
		SessionId: session.SessionID, Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(listed.GetMessages()) != 2 {
		t.Fatalf("message count = %d, want the two rows the run owns", len(listed.GetMessages()))
	}
	user, assistant := listed.GetMessages()[0], listed.GetMessages()[1]
	if user.GetRunState() != nil {
		t.Fatalf("user message carries run state %+v", user.GetRunState())
	}
	state := assistant.GetRunState()
	if state == nil {
		t.Fatal("the reopened assistant message carries no run state")
	}
	if state.GetRunId() != enqueued.RunID || state.GetUserMessageId() != enqueued.UserMessageID ||
		state.GetAssistantMessageId() != enqueued.AssistantMessageID {
		t.Fatalf("run state identity = %+v, want the enqueued run", state)
	}
	if state.GetLifecycle() != turingv1.RunLifecycle_RUN_LIFECYCLE_COMPLETED ||
		state.GetOutcomeReason() != turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_NONE {
		t.Fatalf("run state outcome = %v/%v, want completed with no reason", state.GetLifecycle(), state.GetOutcomeReason())
	}
	if state.GetStateVersion() != completed.State.StateVersion {
		t.Fatalf("state version = %d, want the committed %d", state.GetStateVersion(), completed.State.StateVersion)
	}
	if !state.GetHasDisplayableContent() {
		t.Fatal("run state reports no displayable content for an answer that has some")
	}
	if got := state.GetStateUpdatedAt().AsTime().Format("2006-01-02T15:04:05.000000000Z"); got != completed.State.StateUpdatedAt {
		t.Fatalf("state updated at = %q, want %q", got, completed.State.StateUpdatedAt)
	}
	if got := state.GetFinishedAt().AsTime().Format("2006-01-02T15:04:05.000000000Z"); got != completed.State.FinishedAt.String {
		t.Fatalf("finished at = %q, want %q", got, completed.State.FinishedAt.String)
	}
}

func assertProtoSessionIDs(t *testing.T, sessions []*turingv1.Session, want []string) {
	t.Helper()
	got := make([]string, 0, len(sessions))
	for _, session := range sessions {
		got = append(got, session.SessionId)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("session IDs = %v, want %v", got, want)
	}
}

func assertLifecycleBusEvent(t *testing.T, events <-chan eventsvc.Event, wantStatus, wantTitle string) {
	t.Helper()
	select {
	case event := <-events:
		if event.Type != "session.updated" {
			t.Fatalf("bus event type = %q, want session.updated", event.Type)
		}
		var payload struct {
			Title  string `json:"title"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Title != wantTitle || payload.Status != wantStatus {
			t.Fatalf("bus payload = %+v, want title %q status %q", payload, wantTitle, wantStatus)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lifecycle bus event")
	}
}

func assertNoLifecycleBusEvent(t *testing.T, events <-chan eventsvc.Event) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("unexpected lifecycle bus event: %+v", event)
	default:
	}
}
