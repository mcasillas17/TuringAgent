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
	"log"
	"strings"
	"time"
	"unicode/utf8"

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
	repo        *repository.Repository
	bus         *events.Bus
	audit       *audit.Server
	notifier    Notifier
	jwtSecret   string
	approvalTTL time.Duration
}

type PublicServer struct {
	turingv1.UnimplementedApprovalServiceServer
	service *Server
}

type InternalServer struct {
	turingv1.UnimplementedApprovalServiceServer
	service *Server
}

type Notifier interface {
	NotifyApprovalUpdated(ctx context.Context, runID string, approvalID string, status string, approvalToken string) error
}

const (
	defaultApprovalTTL        = 65 * time.Second
	postCommitTimeout         = 5 * time.Second
	maxDecisionRationaleBytes = 4096
	maxAuditRationaleBytes    = 512
)

func New(repo *repository.Repository, bus *events.Bus, jwtSecret string, approvalTTLs ...time.Duration) *Server {
	approvalTTL := defaultApprovalTTL
	if len(approvalTTLs) > 0 && approvalTTLs[0] > 0 {
		approvalTTL = approvalTTLs[0]
	}
	return &Server{repo: repo, bus: bus, audit: audit.New(repo), jwtSecret: jwtSecret, approvalTTL: approvalTTL}
}

func NewPublicServer(service *Server) *PublicServer {
	return &PublicServer{service: service}
}

func NewInternalServer(service *Server) *InternalServer {
	return &InternalServer{service: service}
}

func (s *PublicServer) ApproveApproval(ctx context.Context, req *turingv1.ApproveApprovalRequest) (*turingv1.ApprovalResponse, error) {
	return s.service.ApproveApproval(ctx, req)
}

func (s *PublicServer) DenyApproval(ctx context.Context, req *turingv1.DenyApprovalRequest) (*turingv1.ApprovalResponse, error) {
	return s.service.DenyApproval(ctx, req)
}

func (*PublicServer) GetApprovalForRuntime(context.Context, *turingv1.GetApprovalForRuntimeRequest) (*turingv1.RuntimeApprovalState, error) {
	return nil, status.Error(codes.PermissionDenied, "runtime approval method is internal")
}

func (*PublicServer) ConsumeApproval(context.Context, *turingv1.ConsumeApprovalRequest) (*turingv1.ApprovalResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "runtime approval method is internal")
}

func (*InternalServer) ApproveApproval(context.Context, *turingv1.ApproveApprovalRequest) (*turingv1.ApprovalResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "human approval method is public")
}

func (*InternalServer) DenyApproval(context.Context, *turingv1.DenyApprovalRequest) (*turingv1.ApprovalResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "human approval method is public")
}

func (s *InternalServer) GetApprovalForRuntime(ctx context.Context, req *turingv1.GetApprovalForRuntimeRequest) (*turingv1.RuntimeApprovalState, error) {
	return s.service.GetApprovalForRuntime(ctx, req)
}

func (s *InternalServer) ConsumeApproval(ctx context.Context, req *turingv1.ConsumeApprovalRequest) (*turingv1.ApprovalResponse, error) {
	return s.service.ConsumeApproval(ctx, req)
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
	approval, event, err := s.repo.CreateApprovalWithEvent(ctx, runID, toolCallID, agentID, toolName, argsJSON, argsHash, repository.FormatTimestamp(approvalExpiry(time.Now(), s.approvalTTL)))
	if err != nil {
		return "", err
	}
	if event.EventID != "" {
		s.publishEvent(event)
		_, _ = s.audit.RecordForExistingRun(ctx, approval.RunID, "runtime", "", "approval.requested", approval.ApprovalID, map[string]any{"toolName": approval.ToolName})
	}
	return approval.ApprovalID, nil
}

func (s *Server) ApproveApproval(ctx context.Context, req *turingv1.ApproveApprovalRequest) (*turingv1.ApprovalResponse, error) {
	if req == nil || req.ApprovalId == "" {
		return nil, status.Error(codes.InvalidArgument, "approval_id is required")
	}
	if err := validateDecisionRationale("comment", req.Comment); err != nil {
		return nil, err
	}
	approval, err := s.repo.GetApproval(ctx, req.ApprovalId)
	if err != nil {
		return nil, mapApprovalError(err)
	}
	if approval.Status != "pending" {
		if approval.Status == "approved" {
			if expired(approval.ExpiresAt) {
				if _, err := s.expireApproval(ctx, req.ApprovalId); err != nil {
					return nil, mapApprovalError(err)
				}
				return nil, status.Error(codes.FailedPrecondition, "approval expired")
			}
			return &turingv1.ApprovalResponse{ApprovalId: approval.ApprovalID, Status: turingv1.ApprovalStatus_APPROVAL_STATUS_APPROVED}, nil
		}
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
	transition, err := s.repo.ApproveApprovalWithEvent(
		ctx,
		req.ApprovalId,
		token,
		sql.NullString{String: req.Comment, Valid: true},
		"",
	)
	if errors.Is(err, repository.ErrApprovalExpired) {
		if _, expireErr := s.expireApproval(ctx, req.ApprovalId); expireErr != nil {
			return nil, mapApprovalError(expireErr)
		}
		return nil, status.Error(codes.FailedPrecondition, "approval expired")
	}
	if err != nil {
		return nil, mapApprovalError(err)
	}
	approved := transition.Approval
	s.runChangedApprovalPostCommitEffects(transition)
	return &turingv1.ApprovalResponse{ApprovalId: approved.ApprovalID, Status: turingv1.ApprovalStatus_APPROVAL_STATUS_APPROVED}, nil
}

func (s *Server) runChangedApprovalPostCommitEffects(transition repository.ApprovalTerminalization) {
	if !transition.Changed {
		return
	}
	approved := transition.Approval
	if transition.ApprovalEvent.EventID != "" {
		s.publishEvent(transition.ApprovalEvent)
	}
	s.finishPostCommit(approved, "client", "approval.approved", "approved", approved.ApprovalToken)
}

func (s *Server) DenyApproval(ctx context.Context, req *turingv1.DenyApprovalRequest) (*turingv1.ApprovalResponse, error) {
	if req == nil || req.ApprovalId == "" {
		return nil, status.Error(codes.InvalidArgument, "approval_id is required")
	}
	if err := validateDecisionRationale("reason", req.Reason); err != nil {
		return nil, err
	}
	transition, err := s.repo.DenyApprovalWithEvent(
		ctx,
		req.ApprovalId,
		sql.NullString{String: req.Reason, Valid: true},
		"",
	)
	if err != nil {
		return nil, mapApprovalError(err)
	}
	denied := transition.Approval
	if !transition.Changed {
		return &turingv1.ApprovalResponse{ApprovalId: denied.ApprovalID, Status: turingv1.ApprovalStatus_APPROVAL_STATUS_DENIED}, nil
	}
	if transition.ApprovalEvent.EventID != "" {
		s.publishEvent(transition.ApprovalEvent)
	}
	if transition.ToolEvent.EventID != "" {
		s.publishEvent(transition.ToolEvent)
	}
	if transition.RunFailedEvent.EventID != "" {
		s.publishEvent(transition.RunFailedEvent)
	}
	if denied.Status == "expired" {
		s.finishPostCommit(denied, "system", "approval.expired", "expired", "")
		return nil, status.Error(codes.FailedPrecondition, "approval expired")
	}
	s.finishPostCommit(denied, "client", "approval.denied", "denied", "")
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
	if (approval.Status == "pending" || approval.Status == "approved") && expired(approval.ExpiresAt) {
		approval, err = s.expireApproval(ctx, req.ApprovalId)
		if err != nil {
			if isPostCommitExpirationError(err) {
				return nil, err
			}
			approval, err = recoverExpirationConflict(ctx, err, func(ctx context.Context) (repository.ApprovalRecord, error) {
				return s.repo.GetApproval(ctx, req.ApprovalId)
			})
			if err != nil {
				return nil, mapApprovalError(err)
			}
		}
	}
	return &turingv1.RuntimeApprovalState{
		ApprovalId:    approval.ApprovalID,
		Status:        mapApprovalStatus(approval.Status),
		ApprovalToken: approval.ApprovalToken,
	}, nil
}

func recoverExpirationConflict(
	ctx context.Context,
	expirationErr error,
	getCurrent func(context.Context) (repository.ApprovalRecord, error),
) (repository.ApprovalRecord, error) {
	current, currentErr := getCurrent(ctx)
	if currentErr != nil {
		return repository.ApprovalRecord{}, currentErr
	}
	if current.Status == "pending" || current.Status == "approved" {
		return repository.ApprovalRecord{}, expirationErr
	}
	return current, nil
}

type postCommitExpirationError struct {
	err error
}

func (e *postCommitExpirationError) Error() string {
	return e.err.Error()
}

func (e *postCommitExpirationError) Unwrap() error {
	return e.err
}

func isPostCommitExpirationError(err error) bool {
	var postCommitErr *postCommitExpirationError
	return errors.As(err, &postCommitErr)
}

func (s *Server) expireApproval(ctx context.Context, approvalID string) (repository.ApprovalRecord, error) {
	transition, err := s.repo.ExpireApprovalWithEvent(ctx, approvalID, "")
	if err != nil {
		return repository.ApprovalRecord{}, err
	}
	expiredApproval := transition.Approval
	if !transition.Changed {
		return expiredApproval, nil
	}
	if transition.ApprovalEvent.EventID != "" {
		s.publishEvent(transition.ApprovalEvent)
	}
	if transition.ToolEvent.EventID != "" {
		s.publishEvent(transition.ToolEvent)
	}
	if transition.RunFailedEvent.EventID != "" {
		s.publishEvent(transition.RunFailedEvent)
	}
	s.finishPostCommit(expiredApproval, "system", "approval.expired", "expired", "")
	return expiredApproval, nil
}

func (s *Server) finishPostCommit(approval repository.ApprovalRecord, actorType string, action string, approvalStatus string, approvalToken string) {
	auditCtx, cancelAudit := context.WithTimeout(context.Background(), postCommitTimeout)
	if _, err := s.audit.RecordForExistingRun(auditCtx, approval.RunID, actorType, "", action, approval.ApprovalID, approvalAuditPayload(approval, action)); err != nil {
		log.Printf("record %s audit for %s: %v", action, approval.ApprovalID, err)
	}
	cancelAudit()
	s.notifyPostCommit(approval, approvalStatus, approvalToken)
}

func validateDecisionRationale(fieldName string, value string) error {
	if !utf8.ValidString(value) {
		return status.Errorf(codes.InvalidArgument, "%s must be valid UTF-8", fieldName)
	}
	if len(value) > maxDecisionRationaleBytes {
		return status.Errorf(codes.InvalidArgument, "%s must be at most %d bytes", fieldName, maxDecisionRationaleBytes)
	}
	return nil
}

func approvalAuditPayload(approval repository.ApprovalRecord, action string) map[string]any {
	payload := map[string]any{"toolName": approval.ToolName}
	switch action {
	case "approval.approved":
		if approval.ApprovalComment.Valid {
			comment, truncated := boundedAuditRationale(approval.ApprovalComment.String)
			payload["comment"] = comment
			if truncated {
				payload["commentTruncated"] = true
			}
		}
	case "approval.denied":
		if approval.DenialReason.Valid {
			reason, truncated := boundedAuditRationale(approval.DenialReason.String)
			payload["reason"] = reason
			if truncated {
				payload["reasonTruncated"] = true
			}
		}
	}
	return payload
}

func boundedAuditRationale(value string) (string, bool) {
	if len(value) <= maxAuditRationaleBytes {
		return value, false
	}
	prefix := value[:maxAuditRationaleBytes-3]
	for !utf8.ValidString(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix + "...", true
}

func (s *Server) notifyPostCommit(approval repository.ApprovalRecord, approvalStatus string, approvalToken string) {
	if s.notifier == nil {
		return
	}
	notifyCtx, cancelNotify := context.WithTimeout(context.Background(), postCommitTimeout)
	if err := s.notifier.NotifyApprovalUpdated(notifyCtx, approval.RunID, approval.ApprovalID, approvalStatus, approvalToken); err != nil {
		log.Printf("notify %s approval %s: %v", approvalStatus, approval.ApprovalID, err)
	}
	cancelNotify()
}

// GrantUnattendedApproval approves a pending approval on the strength of an
// automation's allowlist rather than a person deciding in the moment.
//
// It deliberately takes the SAME path a human approval takes:
// signApprovalToken mints the same short-lived HS256 token carrying the same
// tool and args_hash, and ApproveApprovalWithEvent performs the same
// pending -> approved transition and emits the same lifecycle event. mcp-files
// verifies and consumes it exactly as before, so the token remains single-use
// and bound to these arguments. What is weakened is who decided and when —
// not what a decision is worth, and not the token.
//
// serverName and toolName are the pair the policy lookup just used, passed in
// rather than parsed back out of the approval, so the check here is against
// the same identity the decision was made about.
//
// The grant itself is re-read from storage rather than accepted from the
// caller. That is what makes this the last gate: a caller cannot widen an
// automation's permissions by handing in an allowlist of its own, and a run
// that is not an automation's has no grant to find, so it cannot be approved
// through this door at all.
func (s *Server) GrantUnattendedApproval(ctx context.Context, approvalID string, serverName string, toolName string) error {
	if approvalID == "" {
		return status.Error(codes.InvalidArgument, "approval_id is required")
	}
	approval, err := s.repo.GetApproval(ctx, approvalID)
	if err != nil {
		return mapApprovalError(err)
	}
	grant, unattended, err := s.repo.GetAutomationRunGrant(ctx, approval.RunID)
	if err != nil {
		return status.Error(codes.Internal, "read automation grant failed")
	}
	if !unattended {
		return status.Error(codes.PermissionDenied, "run was not started by an automation")
	}
	if !grant.Allows(serverName, toolName) {
		return status.Error(codes.PermissionDenied, "tool is not on the automation's allowlist")
	}
	if approval.ToolName != toolName {
		return status.Error(codes.FailedPrecondition, "approval does not match the tool being allowed")
	}
	if approval.Status == "approved" {
		return nil
	}
	if approval.Status != "pending" {
		return status.Error(codes.FailedPrecondition, "approval is not pending")
	}
	if expired(approval.ExpiresAt) {
		if _, expireErr := s.expireApproval(ctx, approvalID); expireErr != nil {
			return mapApprovalError(expireErr)
		}
		return status.Error(codes.FailedPrecondition, "approval expired")
	}
	// Recorded BEFORE the token exists, and its failure returned rather than
	// logged. Every other approval audits after the fact because a person
	// watched themselves approve it; here the audit row is the only witness,
	// so an unrecordable grant is not granted at all. Over-recording — a row
	// for a grant that then failed to commit — is the safe direction, and the
	// approvals table still says what actually happened.
	recorded, err := s.audit.RecordForExistingRun(ctx, approval.RunID, "automation", grant.AutomationID, "approval.approved", approval.ApprovalID, map[string]any{
		"toolName":       approval.ToolName,
		"unattended":     true,
		"automationId":   grant.AutomationID,
		"automationName": grant.AutomationName,
	})
	if err != nil || !recorded {
		return status.Error(codes.Internal, "unattended approval could not be recorded")
	}
	token, err := s.signApprovalToken(approval)
	if err != nil {
		return err
	}
	transition, err := s.repo.ApproveApprovalWithEvent(ctx, approvalID, token, sql.NullString{}, "")
	if errors.Is(err, repository.ErrApprovalExpired) {
		if _, expireErr := s.expireApproval(ctx, approvalID); expireErr != nil {
			return mapApprovalError(expireErr)
		}
		return status.Error(codes.FailedPrecondition, "approval expired")
	}
	if err != nil {
		return mapApprovalError(err)
	}
	approved := transition.Approval
	if transition.ApprovalEvent.EventID != "" {
		s.publishEvent(transition.ApprovalEvent)
	}
	// The approval event itself is byte-identical to a person's, so without
	// this the conversation shows an approval card appearing and resolving
	// with nothing to say nobody was asked. The audit row records it too, and
	// is now readable through the redacted audit API — but that is an
	// operator-grade query surface, not something a person reading this
	// conversation will go looking at. This notice is the in-conversation
	// surface, and the only place the user sees it in context.
	notice, err := s.repo.AppendRunNotice(ctx, approved.RunID, fmt.Sprintf(
		"Approved automatically: %s is on %q's allowlist, so nobody was asked.",
		approved.ToolName, grant.AutomationName), map[string]any{
		"unattended":     true,
		"automationId":   grant.AutomationID,
		"automationName": grant.AutomationName,
		"toolName":       approved.ToolName,
	})
	if err != nil {
		log.Printf("record unattended approval notice for %s: %v", approved.ApprovalID, err)
	} else {
		s.publishEvent(notice)
	}
	s.notifyPostCommit(approved, "approved", approved.ApprovalToken)
	return nil
}

func (s *Server) ConsumeApproval(ctx context.Context, req *turingv1.ConsumeApprovalRequest) (*turingv1.ApprovalResponse, error) {
	if req == nil || req.ApprovalId == "" {
		return nil, status.Error(codes.InvalidArgument, "approval_id is required")
	}
	transition, err := s.repo.ConsumeApprovalWithEvent(ctx, req.ApprovalId, "")
	if errors.Is(err, repository.ErrApprovalExpired) {
		expiredApproval := transition.Approval
		if transition.ApprovalEvent.EventID != "" {
			s.publishEvent(transition.ApprovalEvent)
		}
		if transition.ToolEvent.EventID != "" {
			s.publishEvent(transition.ToolEvent)
		}
		if transition.RunFailedEvent.EventID != "" {
			s.publishEvent(transition.RunFailedEvent)
		}
		s.finishPostCommit(expiredApproval, "system", "approval.expired", "expired", "")
		return nil, status.Error(codes.FailedPrecondition, "approval expired")
	}
	if err != nil {
		return nil, mapApprovalError(err)
	}
	consumed := transition.Approval
	if transition.ApprovalEvent.EventID != "" {
		s.publishEvent(transition.ApprovalEvent)
	}
	_, _ = s.audit.RecordForExistingRun(ctx, consumed.RunID, "mcp", "", "approval.consumed", consumed.ApprovalID, map[string]any{"toolName": consumed.ToolName})
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

func expired(expiresAt string) bool {
	deadline, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return true
	}
	return !deadline.After(time.Now())
}

func approvalExpiry(start time.Time, ttl time.Duration) time.Time {
	expiresAt := start.Add(ttl)
	if expiresAt.Nanosecond() != 0 {
		return expiresAt.Truncate(time.Second).Add(time.Second)
	}
	return expiresAt
}

func (s *Server) signApprovalToken(approval repository.ApprovalRecord) (string, error) {
	if s.jwtSecret == "" {
		return "", status.Error(codes.FailedPrecondition, "approval signing is not configured")
	}
	now := time.Now()
	expiresAt, err := time.Parse(time.RFC3339Nano, approval.ExpiresAt)
	if err != nil {
		return "", fmt.Errorf("parse approval expiry: %w", err)
	}
	header := map[string]any{"alg": "HS256", "typ": "JWT"}
	payload := map[string]any{
		"iss":       "turing.orchestrator",
		"sub":       approval.AgentID,
		"aud":       "mcp-files",
		"jti":       approval.ApprovalID,
		"iat":       now.Unix(),
		"exp":       expiresAt.Unix(),
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
	if errors.Is(err, repository.ErrApprovalAlreadyConsumed) {
		return status.Error(codes.FailedPrecondition, "approval already consumed")
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
