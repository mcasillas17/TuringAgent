// Package agents serves the user's list of external agents — assistants that
// do not run on this machine — and the routing of a conversation to one.
package agents

import (
	"context"
	"errors"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	turingv1.UnimplementedExternalAgentServiceServer
	repo *repository.Repository
	// credentials is the set of credential NAMES the backend can resolve. It
	// never holds a key. Enough to tell a client an agent will not work yet,
	// which is the difference between a page that admits a gap and one that
	// lets the user discover it by sending a message that fails.
	credentials map[string]struct{}
}

func New(repo *repository.Repository, credentialNames []string) *Server {
	credentials := make(map[string]struct{}, len(credentialNames))
	for _, name := range credentialNames {
		credentials[name] = struct{}{}
	}
	return &Server{repo: repo, credentials: credentials}
}

func (s *Server) CreateExternalAgent(ctx context.Context, req *turingv1.CreateExternalAgentRequest) (*turingv1.ExternalAgent, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	provider, err := providerToString(req.GetProvider())
	if err != nil {
		return nil, err
	}
	agent, err := s.repo.CreateExternalAgent(ctx, repository.ExternalAgentInput{
		DisplayName:   req.GetDisplayName(),
		Provider:      provider,
		BaseURL:       req.GetBaseUrl(),
		Model:         req.GetModel(),
		CredentialRef: req.GetCredentialRef(),
	})
	if err != nil {
		return nil, agentError(err, "create agent failed")
	}
	return s.toProto(agent), nil
}

func (s *Server) UpdateExternalAgent(ctx context.Context, req *turingv1.UpdateExternalAgentRequest) (*turingv1.ExternalAgent, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	provider, err := providerToString(req.GetProvider())
	if err != nil {
		return nil, err
	}
	agent, err := s.repo.UpdateExternalAgent(ctx, req.GetAgentId(), repository.ExternalAgentInput{
		DisplayName:   req.GetDisplayName(),
		Provider:      provider,
		BaseURL:       req.GetBaseUrl(),
		Model:         req.GetModel(),
		CredentialRef: req.GetCredentialRef(),
	})
	if err != nil {
		return nil, agentError(err, "update agent failed")
	}
	return s.toProto(agent), nil
}

func (s *Server) DeleteExternalAgent(ctx context.Context, req *turingv1.DeleteExternalAgentRequest) (*turingv1.DeleteExternalAgentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.repo.DeleteExternalAgent(ctx, req.GetAgentId()); err != nil {
		return nil, agentError(err, "delete agent failed")
	}
	return &turingv1.DeleteExternalAgentResponse{}, nil
}

func (s *Server) ListExternalAgents(ctx context.Context, _ *turingv1.ListExternalAgentsRequest) (*turingv1.ListExternalAgentsResponse, error) {
	agents, err := s.repo.ListExternalAgents(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list agents failed")
	}
	return &turingv1.ListExternalAgentsResponse{Agents: s.toProtoList(agents)}, nil
}

func (s *Server) GetSessionAgent(ctx context.Context, req *turingv1.GetSessionAgentRequest) (*turingv1.SessionAgentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	agent, routed, err := s.repo.GetSessionAgent(ctx, req.GetSessionId())
	if err != nil {
		return nil, agentError(err, "read conversation destination failed")
	}
	return s.sessionAgentResponse(agent, routed), nil
}

func (s *Server) SetSessionAgent(ctx context.Context, req *turingv1.SetSessionAgentRequest) (*turingv1.SessionAgentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	agent, err := s.repo.SetSessionAgent(ctx, req.GetSessionId(), req.GetAgentId())
	if err != nil {
		return nil, agentError(err, "route conversation failed")
	}
	return s.sessionAgentResponse(agent, true), nil
}

func (s *Server) ClearSessionAgent(ctx context.Context, req *turingv1.ClearSessionAgentRequest) (*turingv1.SessionAgentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if err := s.repo.ClearSessionAgent(ctx, req.GetSessionId()); err != nil {
		return nil, agentError(err, "return conversation to the local assistant failed")
	}
	// An empty agent is not an omission: it is the local assistant, which is
	// what the caller just asked for.
	return &turingv1.SessionAgentResponse{}, nil
}

func (s *Server) sessionAgentResponse(agent repository.ExternalAgent, routed bool) *turingv1.SessionAgentResponse {
	if !routed {
		return &turingv1.SessionAgentResponse{}
	}
	return &turingv1.SessionAgentResponse{Agent: s.toProto(agent)}
}

// agentError maps the repository's named failures to codes a client can act
// on. Everything else collapses to Internal with a fixed message, so a storage
// error never leaks its text to the caller.
func agentError(err error, fallback string) error {
	switch {
	case errors.Is(err, repository.ErrExternalAgentNotFound):
		return status.Error(codes.NotFound, "agent not found")
	case errors.Is(err, repository.ErrSessionNotFound):
		return status.Error(codes.NotFound, "conversation not found")
	case errors.Is(err, repository.ErrExternalAgentNameTaken):
		return status.Error(codes.AlreadyExists, "an agent with that name already exists")
	case errors.Is(err, repository.ErrExternalAgentNameEmpty):
		return status.Error(codes.InvalidArgument, "agent name is required")
	case errors.Is(err, repository.ErrExternalAgentNameTooLong):
		return status.Error(codes.InvalidArgument, "agent name is too long")
	case errors.Is(err, repository.ErrExternalAgentModelEmpty):
		return status.Error(codes.InvalidArgument, "agent model is required")
	case errors.Is(err, repository.ErrExternalAgentModelTooLong):
		return status.Error(codes.InvalidArgument, "agent model is too long")
	case errors.Is(err, repository.ErrExternalAgentBaseURLEmpty):
		return status.Error(codes.InvalidArgument, "agent base URL is required")
	case errors.Is(err, repository.ErrExternalAgentBaseURLInvalid):
		return status.Error(codes.InvalidArgument, "agent base URL must be an absolute http or https URL with no query or fragment")
	case errors.Is(err, repository.ErrExternalAgentBaseURLInsecure):
		return status.Error(codes.InvalidArgument, "an agent that is not on this machine must be reached over https")
	case errors.Is(err, repository.ErrExternalAgentCredentialRefEmpty):
		return status.Error(codes.InvalidArgument, "agent credential name is required")
	case errors.Is(err, repository.ErrExternalAgentCredentialRefFormat):
		return status.Error(codes.InvalidArgument, "agent credential name may only contain letters, digits, dot, dash and underscore")
	case errors.Is(err, repository.ErrExternalAgentProviderInvalid):
		return status.Error(codes.InvalidArgument, "agent provider is unsupported")
	default:
		return status.Error(codes.Internal, fallback)
	}
}

// providerToString rejects an unknown enum value rather than defaulting it.
// Defaulting would record a vendor the user did not pick against an endpoint
// their conversation is about to be sent to.
func providerToString(provider turingv1.ExternalAgentProvider) (string, error) {
	switch provider {
	case turingv1.ExternalAgentProvider_EXTERNAL_AGENT_PROVIDER_ANTHROPIC:
		return "anthropic", nil
	case turingv1.ExternalAgentProvider_EXTERNAL_AGENT_PROVIDER_OPENAI:
		return "openai", nil
	case turingv1.ExternalAgentProvider_EXTERNAL_AGENT_PROVIDER_GOOGLE:
		return "google", nil
	case turingv1.ExternalAgentProvider_EXTERNAL_AGENT_PROVIDER_XAI:
		return "xai", nil
	case turingv1.ExternalAgentProvider_EXTERNAL_AGENT_PROVIDER_OTHER:
		return "other", nil
	default:
		return "", status.Error(codes.InvalidArgument, "agent provider is unsupported")
	}
}

func providerFromString(provider string) turingv1.ExternalAgentProvider {
	switch provider {
	case "anthropic":
		return turingv1.ExternalAgentProvider_EXTERNAL_AGENT_PROVIDER_ANTHROPIC
	case "openai":
		return turingv1.ExternalAgentProvider_EXTERNAL_AGENT_PROVIDER_OPENAI
	case "google":
		return turingv1.ExternalAgentProvider_EXTERNAL_AGENT_PROVIDER_GOOGLE
	case "xai":
		return turingv1.ExternalAgentProvider_EXTERNAL_AGENT_PROVIDER_XAI
	case "other":
		return turingv1.ExternalAgentProvider_EXTERNAL_AGENT_PROVIDER_OTHER
	default:
		// A row written by a later build. Reported as unspecified rather than
		// guessed at, because the label names who receives the conversation.
		return turingv1.ExternalAgentProvider_EXTERNAL_AGENT_PROVIDER_UNSPECIFIED
	}
}

func (s *Server) toProtoList(agents []repository.ExternalAgent) []*turingv1.ExternalAgent {
	converted := make([]*turingv1.ExternalAgent, 0, len(agents))
	for _, agent := range agents {
		converted = append(converted, s.toProto(agent))
	}
	return converted
}

func (s *Server) toProto(agent repository.ExternalAgent) *turingv1.ExternalAgent {
	_, available := s.credentials[agent.CredentialRef]
	return &turingv1.ExternalAgent{
		AgentId:     agent.AgentID,
		DisplayName: agent.DisplayName,
		Provider:    providerFromString(agent.Provider),
		BaseUrl:     agent.BaseURL,
		Model:       agent.Model,
		// The name of the key, never the key. There is no code path that could
		// put a secret on this message, because the server does not hold one.
		CredentialRef:       agent.CredentialRef,
		CredentialAvailable: available,
		CreatedAt:           parseTimestamp(agent.CreatedAt),
		UpdatedAt:           parseTimestamp(agent.UpdatedAt),
	}
}

func parseTimestamp(value string) *timestamppb.Timestamp {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return timestamppb.New(parsed)
}
