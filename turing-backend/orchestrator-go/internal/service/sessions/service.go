package sessions

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	turingv1.UnimplementedSessionServiceServer
	repo *repository.Repository
	cfg  config.Config
}

func New(repo *repository.Repository, cfg config.Config) *Server {
	return &Server{repo: repo, cfg: cfg}
}

func (s *Server) CreateSession(ctx context.Context, req *turingv1.CreateSessionRequest) (*turingv1.CreateSessionResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	session, err := s.repo.CreateSession(ctx, req.Title)
	if err != nil {
		return nil, status.Error(codes.Internal, "create session failed")
	}
	return &turingv1.CreateSessionResponse{SessionId: session.SessionID, CreatedAt: parseTimestamp(session.CreatedAt)}, nil
}

func (s *Server) ListSessions(ctx context.Context, req *turingv1.ListSessionsRequest) (*turingv1.ListSessionsResponse, error) {
	limit := 50
	if req != nil && req.Page != nil {
		if req.Page.Limit < 0 {
			return nil, status.Error(codes.InvalidArgument, "page.limit must be non-negative")
		}
		if req.Page.Limit > 0 {
			limit = int(req.Page.Limit)
		}
	}
	sessions, err := s.repo.ListSessions(ctx, limit)
	if err != nil {
		return nil, status.Error(codes.Internal, "list sessions failed")
	}
	out := make([]*turingv1.Session, 0, len(sessions))
	for _, session := range sessions {
		out = append(out, mapSession(session))
	}
	return &turingv1.ListSessionsResponse{Sessions: out, Page: &turingv1.PageResponse{}}, nil
}

func (s *Server) GetSession(ctx context.Context, req *turingv1.GetSessionRequest) (*turingv1.Session, error) {
	if req == nil || req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	session, err := s.repo.GetSession(ctx, req.SessionId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "session not found")
		}
		return nil, status.Error(codes.Internal, "get session failed")
	}
	return mapSession(session), nil
}

func (s *Server) ListMessages(ctx context.Context, req *turingv1.ListMessagesRequest) (*turingv1.ListMessagesResponse, error) {
	if req == nil || req.SessionId == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	messages, err := s.repo.ListMessages(ctx, req.SessionId, int(req.Limit))
	if err != nil {
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
	messages, err := s.repo.SearchMessages(ctx, req.SessionId, req.Query, int(req.Limit))
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
	providers := []*turingv1.ProviderConfig{
		{Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Enabled: s.cfg.OllamaModel != "", DefaultModel: s.cfg.OllamaModel},
		{Provider: turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE, Enabled: s.cfg.OpenAIAPIKey != "", DefaultModel: s.cfg.OpenAIModel},
	}
	return &turingv1.GetConfigResponse{
		Providers:        providers,
		ApprovalsEnabled: s.cfg.ApprovalJWTSecret != "",
		FilesMcpEnabled:  s.cfg.MCPFilesTokenGeneral != "",
	}, nil
}

func (s *Server) ListAgents(context.Context, *turingv1.ListAgentsRequest) (*turingv1.ListAgentsResponse, error) {
	agents := []*turingv1.AgentDescriptor{{Id: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, DisplayName: "General Assistant"}}
	return &turingv1.ListAgentsResponse{Agents: agents}, nil
}

func (s *Server) ListTools(context.Context, *turingv1.ListToolsRequest) (*turingv1.ListToolsResponse, error) {
	tools := []*turingv1.ToolDescriptor{
		{ServerName: "system", ToolName: "system.time", Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE},
		{ServerName: "files", ToolName: "files.create", Policy: turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED},
	}
	return &turingv1.ListToolsResponse{Tools: tools}, nil
}

func mapSession(session repository.Session) *turingv1.Session {
	title := ""
	if session.Title.Valid {
		title = session.Title.String
	}
	return &turingv1.Session{
		SessionId: session.SessionID,
		Title:     title,
		Status:    session.Status,
		CreatedAt: parseTimestamp(session.CreatedAt),
		UpdatedAt: parseTimestamp(session.UpdatedAt),
	}
}

func mapMessage(sessionID string, message repository.Message) *turingv1.Message {
	return &turingv1.Message{
		MessageId:   message.MessageID,
		SessionId:   sessionID,
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
