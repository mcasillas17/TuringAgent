package approvals

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/safejson"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/audit"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	turingv1.UnimplementedApprovalServiceServer
	repo      *repository.Repository
	bus       *events.Bus
	audit     *audit.Server
	notifier  Notifier
	jwtSecret string
}

type Notifier interface {
	NotifyApprovalUpdated(ctx context.Context, runID string, approvalID string, status string, approvalToken string) error
}

func New(repo *repository.Repository, bus *events.Bus, jwtSecret string) *Server {
	return &Server{repo: repo, bus: bus, audit: audit.New(repo), jwtSecret: jwtSecret}
}

func (s *Server) SetNotifier(notifier Notifier) {
	s.notifier = notifier
}

func (s *Server) CreateApprovalForTool(ctx context.Context, runID string, toolCallID string, agentID string, toolName string, args map[string]any) (string, error) {
	if runID == "" || toolCallID == "" || agentID == "" || toolName == "" {
		return "", status.Error(codes.InvalidArgument, "approval tool context is required")
	}
	if existing, err := s.repo.GetApprovalByToolCall(ctx, runID, toolCallID); err == nil {
		return existing.ApprovalID, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	argsJSON, argsHash, err := canonicalArgs(args)
	if err != nil {
		return "", status.Error(codes.InvalidArgument, "tool args are not valid JSON")
	}
	approval, err := s.repo.CreateApproval(ctx, runID, toolCallID, agentID, toolName, argsJSON, argsHash, time.Now().Add(time.Minute).Format(time.RFC3339Nano))
	if err != nil {
		return "", err
	}
	event, err := s.appendApprovalEvent(ctx, approval, "approval.requested", map[string]any{
		"approvalId":  approval.ApprovalID,
		"toolName":    approval.ToolName,
		"argsSummary": summarizeArgs(args),
	})
	if err != nil {
		return "", err
	}
	s.publishEvent(event)
	if err := s.audit.Record(ctx, approval.RunID, "runtime", "", "approval.requested", approval.ApprovalID, map[string]any{"toolName": approval.ToolName}); err != nil {
		return "", err
	}
	return approval.ApprovalID, nil
}

func (s *Server) ApproveApproval(ctx context.Context, req *turingv1.ApproveApprovalRequest) (*turingv1.ApprovalResponse, error) {
	if req == nil || req.ApprovalId == "" {
		return nil, status.Error(codes.InvalidArgument, "approval_id is required")
	}
	approval, err := s.repo.GetApproval(ctx, req.ApprovalId)
	if err != nil {
		return nil, mapApprovalError(err)
	}
	if approval.Status != "pending" {
		return nil, status.Error(codes.FailedPrecondition, "approval is not pending")
	}
	if expired(approval.ExpiresAt) {
		if _, err := s.expireApproval(ctx, req.ApprovalId); err != nil {
			return nil, mapApprovalError(err)
		}
		return nil, status.Error(codes.FailedPrecondition, "approval expired")
	}
	token, err := s.signApprovalToken(approval)
	if err != nil {
		return nil, err
	}
	approved, err := s.repo.ApproveApproval(ctx, req.ApprovalId, token, "")
	if err != nil {
		return nil, mapApprovalError(err)
	}
	event, err := s.appendApprovalEvent(ctx, approved, "approval.approved", map[string]any{"approvalId": approved.ApprovalID, "toolName": approved.ToolName})
	if err != nil {
		return nil, err
	}
	s.publishEvent(event)
	if err := s.audit.Record(ctx, approved.RunID, "client", "", "approval.approved", approved.ApprovalID, map[string]any{"toolName": approved.ToolName}); err != nil {
		return nil, err
	}
	if s.notifier != nil {
		if err := s.notifier.NotifyApprovalUpdated(ctx, approved.RunID, approved.ApprovalID, "approved", approved.ApprovalToken); err != nil {
			return nil, err
		}
	}
	return &turingv1.ApprovalResponse{ApprovalId: approved.ApprovalID, Status: turingv1.ApprovalStatus_APPROVAL_STATUS_APPROVED}, nil
}

func (s *Server) DenyApproval(ctx context.Context, req *turingv1.DenyApprovalRequest) (*turingv1.ApprovalResponse, error) {
	if req == nil || req.ApprovalId == "" {
		return nil, status.Error(codes.InvalidArgument, "approval_id is required")
	}
	denied, err := s.repo.DenyApproval(ctx, req.ApprovalId, "")
	if err != nil {
		return nil, mapApprovalError(err)
	}
	event, err := s.appendApprovalEvent(ctx, denied, "approval.denied", map[string]any{"approvalId": denied.ApprovalID, "toolName": denied.ToolName})
	if err != nil {
		return nil, err
	}
	s.publishEvent(event)
	if err := s.audit.Record(ctx, denied.RunID, "client", "", "approval.denied", denied.ApprovalID, map[string]any{"toolName": denied.ToolName}); err != nil {
		return nil, err
	}
	if s.notifier != nil {
		if err := s.notifier.NotifyApprovalUpdated(ctx, denied.RunID, denied.ApprovalID, "denied", ""); err != nil {
			return nil, err
		}
	}
	return &turingv1.ApprovalResponse{ApprovalId: denied.ApprovalID, Status: turingv1.ApprovalStatus_APPROVAL_STATUS_DENIED}, nil
}

func (s *Server) GetApprovalForRuntime(ctx context.Context, req *turingv1.GetApprovalForRuntimeRequest) (*turingv1.RuntimeApprovalState, error) {
	if req == nil || req.ApprovalId == "" {
		return nil, status.Error(codes.InvalidArgument, "approval_id is required")
	}
	approval, err := s.repo.GetApproval(ctx, req.ApprovalId)
	if err != nil {
		return nil, mapApprovalError(err)
	}
	if approval.Status == "pending" && expired(approval.ExpiresAt) {
		approval, err = s.expireApproval(ctx, req.ApprovalId)
		if err != nil {
			current, currentErr := s.repo.GetApproval(ctx, req.ApprovalId)
			if currentErr != nil {
				return nil, mapApprovalError(err)
			}
			if current.Status == "pending" {
				return nil, mapApprovalError(err)
			}
			approval = current
		}
	}
	return &turingv1.RuntimeApprovalState{
		ApprovalId:    approval.ApprovalID,
		Status:        mapApprovalStatus(approval.Status),
		ApprovalToken: approval.ApprovalToken,
	}, nil
}

func (s *Server) expireApproval(ctx context.Context, approvalID string) (repository.ApprovalRecord, error) {
	expiredApproval, err := s.repo.ExpireApproval(ctx, approvalID, "")
	if err != nil {
		return repository.ApprovalRecord{}, err
	}
	event, err := s.appendApprovalEvent(ctx, expiredApproval, "approval.expired", map[string]any{"approvalId": expiredApproval.ApprovalID, "toolName": expiredApproval.ToolName})
	if err != nil {
		return repository.ApprovalRecord{}, err
	}
	s.publishEvent(event)
	if err := s.audit.Record(ctx, expiredApproval.RunID, "system", "", "approval.expired", expiredApproval.ApprovalID, map[string]any{"toolName": expiredApproval.ToolName}); err != nil {
		return repository.ApprovalRecord{}, err
	}
	if s.notifier != nil {
		if err := s.notifier.NotifyApprovalUpdated(ctx, expiredApproval.RunID, expiredApproval.ApprovalID, "expired", ""); err != nil {
			return repository.ApprovalRecord{}, err
		}
	}
	return expiredApproval, nil
}

func (s *Server) ConsumeApproval(ctx context.Context, req *turingv1.ConsumeApprovalRequest) (*turingv1.ApprovalResponse, error) {
	if req == nil || req.ApprovalId == "" {
		return nil, status.Error(codes.InvalidArgument, "approval_id is required")
	}
	consumed, err := s.repo.ConsumeApproval(ctx, req.ApprovalId, "")
	if err != nil {
		return nil, mapApprovalError(err)
	}
	event, err := s.appendApprovalEvent(ctx, consumed, "approval.consumed", map[string]any{"approvalId": consumed.ApprovalID, "toolName": consumed.ToolName})
	if err != nil {
		return nil, err
	}
	s.publishEvent(event)
	if err := s.audit.Record(ctx, consumed.RunID, "mcp", "", "approval.consumed", consumed.ApprovalID, map[string]any{"toolName": consumed.ToolName}); err != nil {
		return nil, err
	}
	return &turingv1.ApprovalResponse{ApprovalId: consumed.ApprovalID, Status: turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED}, nil
}

func canonicalArgs(args map[string]any) (string, string, error) {
	data, err := safejson.MarshalCanonical(args)
	if err != nil {
		return "", "", err
	}
	hash := sha256.Sum256(data)
	return string(data), "sha256:" + fmt.Sprintf("%x", hash[:]), nil
}

func summarizeArgs(args map[string]any) string {
	if path, ok := args["path"].(string); ok && path != "" {
		return "Requested change to " + path
	}
	return "Requested tool use"
}

func expired(expiresAt string) bool {
	deadline, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return true
	}
	return !deadline.After(time.Now())
}

func (s *Server) signApprovalToken(approval repository.ApprovalRecord) (string, error) {
	if s.jwtSecret == "" {
		return "", status.Error(codes.FailedPrecondition, "approval signing is not configured")
	}
	now := time.Now()
	header := map[string]any{"alg": "HS256", "typ": "JWT"}
	payload := map[string]any{
		"iss":       "turing.orchestrator",
		"sub":       approval.AgentID,
		"aud":       "mcp-files",
		"jti":       approval.ApprovalID,
		"iat":       now.Unix(),
		"exp":       now.Add(time.Minute).Unix(),
		"tool":      approval.ToolName,
		"args_hash": approval.ArgsHash,
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(payloadJSON)
	mac := hmac.New(sha256.New, []byte(s.jwtSecret))
	_, _ = mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Server) appendApprovalEvent(ctx context.Context, approval repository.ApprovalRecord, eventType string, payload map[string]any) (repository.Event, error) {
	run, err := s.repo.GetRun(ctx, approval.RunID)
	if err != nil {
		return repository.Event{}, err
	}
	payloadJSON, err := safejson.MarshalCanonical(payload)
	if err != nil {
		return repository.Event{}, err
	}
	return s.repo.AppendEvent(ctx, repository.AppendEventInput{
		SessionID:   run.SessionID,
		RunID:       approval.RunID,
		TraceID:     run.TraceID,
		Type:        eventType,
		PayloadJSON: string(payloadJSON),
	})
}

func (s *Server) publishEvent(event repository.Event) {
	if s.bus == nil {
		return
	}
	runID := ""
	if event.RunID.Valid {
		runID = event.RunID.String
	}
	s.bus.Publish(events.Event{
		EventID:     event.EventID,
		SessionID:   event.SessionID,
		RunID:       runID,
		TraceID:     event.TraceID,
		Sequence:    event.Sequence,
		Type:        event.Type,
		CreatedAt:   event.CreatedAt,
		PayloadJSON: event.PayloadJSON,
	})
}

func mapApprovalError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return status.Error(codes.NotFound, "approval not found")
	}
	if strings.Contains(err.Error(), "not pending") || strings.Contains(err.Error(), "not approved") || strings.Contains(err.Error(), "not waiting") || strings.Contains(err.Error(), "not found for approval") {
		return status.Error(codes.FailedPrecondition, err.Error())
	}
	return err
}

func mapApprovalStatus(value string) turingv1.ApprovalStatus {
	switch value {
	case "pending":
		return turingv1.ApprovalStatus_APPROVAL_STATUS_PENDING
	case "approved":
		return turingv1.ApprovalStatus_APPROVAL_STATUS_APPROVED
	case "denied":
		return turingv1.ApprovalStatus_APPROVAL_STATUS_DENIED
	case "expired":
		return turingv1.ApprovalStatus_APPROVAL_STATUS_EXPIRED
	case "consumed":
		return turingv1.ApprovalStatus_APPROVAL_STATUS_CONSUMED
	default:
		return turingv1.ApprovalStatus_APPROVAL_STATUS_UNSPECIFIED
	}
}
