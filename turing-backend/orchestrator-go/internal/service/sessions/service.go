package sessions

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/persisttime"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	eventsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	turingv1.UnimplementedSessionServiceServer
	repo         *repository.Repository
	cfg          config.Config
	capabilities capabilitySource
	cursors      sessionCursorCodec
	bus          *eventsvc.Bus
}

type capabilitySource interface {
	ProviderCapabilities() map[turingv1.ModelProvider][]*turingv1.ModelCapability
	AgentAvailable(turingv1.AgentId) bool
	RoutableDefaultModel(string, string) string
	LiveToolNames() []string
}

func New(repo *repository.Repository, cfg config.Config, capabilities capabilitySource, bus *eventsvc.Bus) *Server {
	return &Server{
		repo:         repo,
		cfg:          cfg,
		capabilities: capabilities,
		cursors:      newSessionCursorCodec(cfg.CursorHMACKey),
		bus:          bus,
	}
}

func (s *Server) CreateSession(ctx context.Context, req *turingv1.CreateSessionRequest) (*turingv1.CreateSessionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	title := strings.TrimSpace(req.Title)
	if !utf8.ValidString(title) || utf8.RuneCountInString(title) > repository.MaxSessionTitleRunes {
		return nil, status.Error(codes.InvalidArgument, "title is invalid")
	}
	session, err := s.repo.CreateSession(ctx, title)
	if err != nil {
		return nil, status.Error(codes.Internal, "create session failed")
	}
	createdAt, err := parseSessionTimestamp(session.CreatedAt)
	if err != nil {
		return nil, status.Error(codes.Internal, "create session failed")
	}
	return &turingv1.CreateSessionResponse{SessionId: session.SessionID, CreatedAt: createdAt}, nil
}

func (s *Server) ListSessions(ctx context.Context, req *turingv1.ListSessionsRequest) (*turingv1.ListSessionsResponse, error) {
	filter, repositoryFilter, err := sessionListFilter(req.GetFilter())
	if err != nil {
		return nil, err
	}
	limit := 50
	if req != nil && req.Page != nil {
		if req.Page.Limit < 0 || req.Page.Limit > 100 {
			return nil, status.Error(codes.InvalidArgument, "page.limit must be between 1 and 100")
		}
		if req.Page.Limit > 0 {
			limit = int(req.Page.Limit)
		}
	}
	var after *repository.SessionCursor
	if req != nil && req.GetPage().GetCursor() != "" {
		decoded, err := s.cursors.decode(req.GetPage().GetCursor(), filter)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "page.cursor is invalid")
		}
		after = &repository.SessionCursor{
			UpdatedAt: decoded.UpdatedAt,
			SessionID: decoded.SessionID,
		}
	}
	sessions, err := s.repo.ListSessionsPage(ctx, repository.ListSessionsInput{
		Filter: repositoryFilter,
		After:  after,
		Limit:  limit + 1,
	})
	if err != nil {
		return nil, status.Error(codes.Internal, "list sessions failed")
	}
	hasMore := len(sessions) > limit
	if hasMore {
		sessions = sessions[:limit]
	}
	out := make([]*turingv1.Session, 0, len(sessions))
	for _, session := range sessions {
		mapped, err := mapSession(session)
		if err != nil {
			return nil, status.Error(codes.Internal, "list sessions failed")
		}
		out = append(out, mapped)
	}
	page := &turingv1.PageResponse{}
	if hasMore {
		last := sessions[len(sessions)-1]
		page.NextCursor, err = s.cursors.encode(sessionCursor{
			Filter:    filter,
			UpdatedAt: last.UpdatedAt,
			SessionID: last.SessionID,
		})
		if err != nil {
			return nil, status.Error(codes.Internal, "list sessions failed")
		}
	}
	return &turingv1.ListSessionsResponse{Sessions: out, Page: page}, nil
}

func (s *Server) GetSession(ctx context.Context, req *turingv1.GetSessionRequest) (*turingv1.Session, error) {
	if req == nil || !validSessionID(req.SessionId) {
		return nil, status.Error(codes.InvalidArgument, "session_id is invalid")
	}
	session, err := s.repo.GetSession(ctx, req.SessionId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "session not found")
		}
		return nil, status.Error(codes.Internal, "get session failed")
	}
	mapped, err := mapSession(session)
	if err != nil {
		return nil, status.Error(codes.Internal, "get session failed")
	}
	return mapped, nil
}

// DeleteSession removes a session and everything it produced. It is the only
// way a user can withdraw what they have said, so the failure modes are
// reported distinctly rather than collapsed into Internal: NotFound so the
// client can say the session is already gone, and FailedPrecondition so it can
// explain that work is still in flight rather than implying a bug.
func (s *Server) DeleteSession(ctx context.Context, req *turingv1.DeleteSessionRequest) (*turingv1.DeleteSessionResponse, error) {
	if req == nil || !validSessionID(req.SessionId) {
		return nil, status.Error(codes.InvalidArgument, "session_id is invalid")
	}
	if err := s.repo.DeleteSession(ctx, req.SessionId); err != nil {
		switch {
		case errors.Is(err, repository.ErrSessionNotFound):
			return nil, status.Error(codes.NotFound, "session not found")
		case errors.Is(err, repository.ErrSessionHasActiveRun):
			return nil, status.Error(codes.FailedPrecondition, "session has a run in progress")
		default:
			return nil, status.Error(codes.Internal, "delete session failed")
		}
	}
	return &turingv1.DeleteSessionResponse{SessionId: req.SessionId}, nil
}

func (s *Server) ListMessages(ctx context.Context, req *turingv1.ListMessagesRequest) (*turingv1.ListMessagesResponse, error) {
	if req == nil || !validSessionID(req.SessionId) {
		return nil, status.Error(codes.InvalidArgument, "session_id is invalid")
	}
	var (
		messages []repository.Message
		err      error
	)
	if req.BeforeMessageId == "" {
		messages, err = s.repo.ListMessages(ctx, req.SessionId, int(req.Limit))
	} else {
		messages, err = s.repo.ListMessagesBefore(ctx, req.SessionId, req.BeforeMessageId, int(req.Limit))
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "before_message_id not found in session")
		}
		return nil, status.Error(codes.Internal, "list messages failed")
	}
	out := make([]*turingv1.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, mapMessage(req.SessionId, message))
	}
	return &turingv1.ListMessagesResponse{Messages: out}, nil
}

func (s *Server) SearchMessages(ctx context.Context, req *turingv1.SearchMessagesRequest) (*turingv1.SearchMessagesResponse, error) {
	if req == nil || strings.TrimSpace(req.Query) == "" {
		return nil, status.Error(codes.InvalidArgument, "query is required")
	}
	messages, err := s.repo.SearchMessages(
		ctx,
		req.SessionId,
		req.ExcludeSessionId,
		req.Query,
		int(req.Limit),
	)
	if err != nil {
		return nil, status.Error(codes.Internal, "search messages failed")
	}
	out := make([]*turingv1.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, mapMessage(message.SessionID, message))
	}
	return &turingv1.SearchMessagesResponse{Messages: out}, nil
}

func (s *Server) GetConfig(context.Context, *turingv1.GetConfigRequest) (*turingv1.GetConfigResponse, error) {
	var advertised map[turingv1.ModelProvider][]*turingv1.ModelCapability
	if s.capabilities != nil {
		advertised = s.capabilities.ProviderCapabilities()
	}
	ollamaDefault, openAIDefault := "", ""
	if s.capabilities != nil {
		ollamaDefault = s.capabilities.RoutableDefaultModel("ollama", s.cfg.OllamaModel)
		openAIDefault = s.capabilities.RoutableDefaultModel("openai_compatible", s.cfg.OpenAIModel)
	}
	providers := []*turingv1.ProviderConfig{
		{
			Provider:     turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
			Enabled:      len(advertised[turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA]) > 0,
			DefaultModel: ollamaDefault,
			Models:       advertised[turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA],
		},
		{
			Provider:     turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
			Enabled:      len(advertised[turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE]) > 0,
			DefaultModel: openAIDefault,
			Models:       advertised[turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE],
		},
	}
	return &turingv1.GetConfigResponse{
		Providers:        providers,
		ApprovalsEnabled: s.cfg.ApprovalJWTSecret != "",
		FilesMcpEnabled:  s.cfg.FilesMCPEnabled,
	}, nil
}

func (s *Server) ListAgents(context.Context, *turingv1.ListAgentsRequest) (*turingv1.ListAgentsResponse, error) {
	available := false
	if s.capabilities != nil {
		available = s.capabilities.AgentAvailable(turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT)
	}
	agents := []*turingv1.AgentDescriptor{{
		Id:          turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT,
		DisplayName: "General Assistant",
		Available:   available,
	}}
	return &turingv1.ListAgentsResponse{Agents: agents}, nil
}

func (s *Server) ListTools(ctx context.Context, _ *turingv1.ListToolsRequest) (*turingv1.ListToolsResponse, error) {
	discovered, err := s.repo.ListEnabledTools(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list tools failed")
	}
	live := map[string]struct{}{}
	if s.capabilities != nil {
		for _, name := range s.capabilities.LiveToolNames() {
			live[name] = struct{}{}
		}
	}
	tools := make([]*turingv1.ToolDescriptor, 0, len(discovered))
	for _, tool := range discovered {
		if _, ok := live[tool.ServerName+"/"+tool.ToolName]; !ok {
			continue
		}
		tools = append(tools, &turingv1.ToolDescriptor{
			ServerName: tool.ServerName,
			ToolName:   tool.ToolName,
			Policy:     toProtoToolPolicy(tool.Policy),
		})
	}
	return &turingv1.ListToolsResponse{Tools: tools}, nil
}

func toProtoToolPolicy(policy string) turingv1.ToolPolicy {
	switch policy {
	case "safe":
		return turingv1.ToolPolicy_TOOL_POLICY_SAFE
	case "approval_required":
		return turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED
	case "disabled":
		return turingv1.ToolPolicy_TOOL_POLICY_DISABLED
	default:
		return turingv1.ToolPolicy_TOOL_POLICY_UNSPECIFIED
	}
}

func mapSession(session repository.Session) (*turingv1.Session, error) {
	if session.Status != string(repository.SessionListActive) &&
		session.Status != string(repository.SessionListArchived) {
		return nil, repository.ErrInvalidSessionStatus
	}
	createdAt, err := parseSessionTimestamp(session.CreatedAt)
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseSessionTimestamp(session.UpdatedAt)
	if err != nil {
		return nil, err
	}
	title := ""
	if session.Title.Valid {
		title = session.Title.String
	}
	return &turingv1.Session{
		SessionId: session.SessionID,
		Title:     title,
		Status:    session.Status,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

func mapMessage(sessionID string, message repository.Message) *turingv1.Message {
	return &turingv1.Message{
		MessageId:   message.MessageID,
		SessionId:   sessionID,
		RunId:       message.RunID,
		Role:        mapRole(message.Role),
		Content:     message.Content,
		ContentType: message.ContentType,
		Sequence:    message.Sequence,
		CreatedAt:   parseTimestamp(message.CreatedAt),
	}
}

func mapRole(role string) turingv1.MessageRole {
	switch role {
	case "system":
		return turingv1.MessageRole_MESSAGE_ROLE_SYSTEM
	case "user":
		return turingv1.MessageRole_MESSAGE_ROLE_USER
	case "assistant":
		return turingv1.MessageRole_MESSAGE_ROLE_ASSISTANT
	case "tool":
		return turingv1.MessageRole_MESSAGE_ROLE_TOOL
	default:
		return turingv1.MessageRole_MESSAGE_ROLE_UNSPECIFIED
	}
}

func parseTimestamp(value string) *timestamppb.Timestamp {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return timestamppb.New(t)
}

func parseSessionTimestamp(value string) (*timestamppb.Timestamp, error) {
	parsed, err := persisttime.ParseCanonical(value)
	if err != nil {
		return nil, repository.ErrInvalidSessionTimestamp
	}
	return timestamppb.New(parsed), nil
}

func sessionListFilter(filter turingv1.SessionListFilter) (sessionFilter, repository.SessionListFilter, error) {
	switch filter {
	case turingv1.SessionListFilter_SESSION_LIST_FILTER_UNSPECIFIED,
		turingv1.SessionListFilter_SESSION_LIST_FILTER_ACTIVE:
		return sessionFilterActive, repository.SessionListActive, nil
	case turingv1.SessionListFilter_SESSION_LIST_FILTER_ARCHIVED:
		return sessionFilterArchived, repository.SessionListArchived, nil
	case turingv1.SessionListFilter_SESSION_LIST_FILTER_ALL:
		return sessionFilterAll, repository.SessionListAll, nil
	default:
		return 0, "", status.Error(codes.InvalidArgument, "filter is invalid")
	}
}
