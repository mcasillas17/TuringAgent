package integrations

import (
	"bytes"
	"context"
	"errors"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"
)

func (s *Server) CallIntegrationTool(ctx context.Context, req *turingv1.CallIntegrationToolRequest) (*turingv1.CallIntegrationToolResponse, error) {
	if req == nil || req.GetRunId() == "" || req.GetToolName() == "" || req.GetArgs() == nil {
		return nil, status.Error(codes.InvalidArgument, "run_id, tool_name, and args are required")
	}
	if _, ok := lookupIntegrationTool(req.GetToolName()); !ok {
		return nil, status.Error(codes.NotFound, "integration tool not found")
	}
	args := req.GetArgs().AsMap()
	connectionID, err := requiredString(args, "connection_id")
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.validateIntegrationDecision(ctx, req.GetRunId(), connectionID, req.GetToolName(), "not covered by the run egress decision"); err != nil {
		return nil, err
	}
	policy, found, err := s.repo.PseudoServerToolPolicy(ctx, "integrations", req.GetToolName())
	if err != nil {
		return nil, status.Error(codes.Internal, "read integration tool policy failed")
	}
	if !found || policy == "disabled" {
		return nil, status.Error(codes.FailedPrecondition, "integration tool is disabled or unregistered")
	}
	switch policy {
	case "safe":
	case "approval_required":
		if s.approvals == nil {
			return nil, status.Error(codes.FailedPrecondition, "caller-side approval enforcement is not configured")
		}
		if err := s.approvals.ConsumeApprovalForThirdParty(ctx, req.GetApprovalId(), req.GetRunId(), "integrations", req.GetToolName(), args); err != nil {
			return nil, err
		}
	default:
		return nil, status.Error(codes.FailedPrecondition, "integration tool policy is unsupported")
	}
	if err := s.validateIntegrationDecision(ctx, req.GetRunId(), connectionID, req.GetToolName(), "egress consent changed before dispatch"); err != nil {
		return nil, err
	}
	active, err := s.repo.IntegrationDispatchActive(ctx, req.GetRunId(), req.GetToolName(), policy)
	if err != nil {
		return nil, status.Error(codes.Internal, "validate integration dispatch state failed")
	}
	if !active {
		return nil, status.Error(codes.FailedPrecondition, "integration call was revoked before dispatch")
	}
	sealed, err := s.repo.GetSealedConnectionCredential(ctx, connectionID)
	if err != nil {
		if errors.Is(err, repository.ErrConnectionNotFound) || errors.Is(err, repository.ErrConnectionNotUsable) {
			return nil, status.Error(codes.FailedPrecondition, "integration connection is revoked or deleted")
		}
		return nil, status.Error(codes.Internal, "read integration connection failed")
	}
	if sealed.Provider != "github" {
		return nil, status.Error(codes.InvalidArgument, "connection is not a GitHub account")
	}
	if s.sealer == nil || !s.sealer.SealedWithThisKey(sealed.Ciphertext) {
		return nil, status.Error(codes.FailedPrecondition, "integration credential is unreadable; reconnect the account")
	}
	credential, err := s.sealer.Open(sealed.Ciphertext, []byte(connectionID))
	if err != nil {
		return nil, status.Error(codes.FailedPrecondition, "integration credential is unreadable; reconnect the account")
	}
	defer func() {
		for index := range credential {
			credential[index] = 0
		}
	}()
	var dispatchGateErr error
	content, err := s.callGitHubGuarded(ctx, req.GetToolName(), args, credential, func(dispatchCtx context.Context) error {
		dispatchGateErr = s.validateImmediatelyBeforeIntegrationDispatch(
			dispatchCtx, req.GetRunId(), connectionID, req.GetToolName(), policy, sealed.Ciphertext,
		)
		return dispatchGateErr
	})
	if dispatchGateErr != nil {
		return nil, dispatchGateErr
	}
	if err != nil {
		return nil, status.Error(codes.Unavailable, err.Error())
	}
	result, err := structpb.NewStruct(map[string]any{"content": content})
	if err != nil {
		return nil, status.Error(codes.Internal, "encode integration result failed")
	}
	return &turingv1.CallIntegrationToolResponse{Result: result}, nil
}

func (s *Server) validateImmediatelyBeforeIntegrationDispatch(
	ctx context.Context,
	runID, connectionID, toolName, policy string,
	expectedCiphertext []byte,
) error {
	if err := s.validateIntegrationDecision(ctx, runID, connectionID, toolName, "egress consent changed before dispatch"); err != nil {
		return err
	}
	active, err := s.repo.IntegrationDispatchActive(ctx, runID, toolName, policy)
	if err != nil {
		return status.Error(codes.Internal, "validate integration dispatch state failed")
	}
	if !active {
		return status.Error(codes.FailedPrecondition, "integration call was revoked before dispatch")
	}
	current, err := s.repo.GetSealedConnectionCredential(ctx, connectionID)
	if err != nil {
		if errors.Is(err, repository.ErrConnectionNotFound) || errors.Is(err, repository.ErrConnectionNotUsable) {
			return status.Error(codes.FailedPrecondition, "integration connection is revoked or deleted")
		}
		return status.Error(codes.Internal, "read integration connection failed")
	}
	if current.Provider != "github" || !bytes.Equal(current.Ciphertext, expectedCiphertext) {
		return status.Error(codes.FailedPrecondition, "integration connection changed before dispatch")
	}
	return nil
}

func (s *Server) validateIntegrationDecision(ctx context.Context, runID, connectionID, toolName, message string) error {
	allowed, err := s.repo.RunAllowsIntegration(ctx, runID, repository.GitHubIntegrationEndpoint, connectionID, toolName)
	if err != nil {
		return status.Error(codes.Internal, "validate integration egress decision failed")
	}
	if !allowed {
		return status.Error(codes.PermissionDenied, "integration call is "+message)
	}
	return nil
}
