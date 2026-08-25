package memory

import (
	"context"
	"errors"
	"log"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/memoryfiles"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ServerName is the pseudo-server every memory tool is registered under. It has
// no MCP process behind it: the orchestrator dispatches these calls itself, so
// its tool rows carry a NULL mcp_server_id (see repository.IsPseudoServerName).
const ServerName = "memory"

// The three tools, and the whole of what a model may do with memory. There is
// no delete, no rename and no write outside the inbox, because none of those
// are things a model gets to decide about what the user is remembered as.
const (
	ToolSearch   = "memory.search"
	ToolRead     = "memory.read"
	ToolRemember = "memory.remember"
)

// AuditRecorder is the redacted trail a user action leaves behind.
//
// Two methods, because memory writes two different kinds of row. Record is for
// a decision the person made about their whole account — the toggle, an applied
// profile — which belongs to no conversation and is correlated to nothing.
// RecordForExistingRun is for what a model did inside one conversation, and it
// insists the run still exists: a memory row that named no run would sit
// outside the reach of the session-deletion scrub forever.
type AuditRecorder interface {
	Record(ctx context.Context, correlationID string, actorType string, actorID string, action string, target string, payload map[string]any) error
	RecordForExistingRun(ctx context.Context, runID string, actorType string, actorID string, action string, target string, payload map[string]any) (bool, error)
}

// RegistryChangeNotifier republishes the tool list to connected workers. The
// memory toggle has to reach a running worker without a restart, and this is
// the same fan-out the MCP registry and integrations already use.
type RegistryChangeNotifier interface {
	NotifyMCPRegistryChanged(context.Context) error
}

// ApprovalEnforcer consumes the approval a caller-enforced tool call depends
// on. memory.remember is approval_required by default, and the orchestrator is
// the component executing it, so the check has to happen on this side.
type ApprovalEnforcer interface {
	ConsumeApprovalForThirdParty(ctx context.Context, approvalID string, runID string, serverName string, serverID string, toolName string, args map[string]any) error
}

// Server owns the memory surface: the public facet a person reads and decides
// through, and the internal facet the runtime discovers and dispatches through.
// Both share one instance, because the toggle and the vault are one thing.
type Server struct {
	repo      *repository.Repository
	vault     *memoryfiles.Vault
	audit     AuditRecorder
	notifier  RegistryChangeNotifier
	approvals ApprovalEnforcer
}

func New(repo *repository.Repository, vault *memoryfiles.Vault, audit AuditRecorder) *Server {
	return &Server{repo: repo, vault: vault, audit: audit}
}

func (s *Server) SetRegistryChangeNotifier(notifier RegistryChangeNotifier) { s.notifier = notifier }
func (s *Server) SetApprovalEnforcer(enforcer ApprovalEnforcer)             { s.approvals = enforcer }

func (s *Server) notifyRegistryChanged() {
	if s.notifier == nil {
		return
	}
	if err := s.notifier.NotifyMCPRegistryChanged(context.Background()); err != nil {
		log.Printf("notify runtime of memory registry change: %v", err)
	}
}

func (s *Server) record(ctx context.Context, action string, target string, payload map[string]any) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Record(ctx, "", "client", "", action, target, payload); err != nil {
		log.Printf("record %s for %s: %v", action, target, err)
	}
}

// recordForRun writes the trail entry for something a model did inside one
// conversation, correlated to the run so that deleting the conversation can
// reach it.
//
// The one failure it reports is the run being gone, and it is a hard failure
// on purpose. There is no fallback to Record: the session-deletion scrub finds
// memory rows through the session's runs, so an uncorrelated row is residue
// the user can never withdraw. And an insert that found no run means the
// conversation is being withdrawn or already has been — the candidate went
// with it through the session cascade, or is about to — so there is no
// proposal left for a success to be about.
//
// A genuine write failure is logged and swallowed, like every other audit
// write in this orchestrator. It is the opposite case: the run is still there,
// the proposal is durable and sitting in the user's inbox, and refusing the
// call would only invite a retry that files the claim a second time. A missing
// row is a gap in the trail; two identical proposals are a mess in the vault
// the user has to clean up.
//
// actorID is deliberately empty. "runtime" is the whole of the identity there
// is to state — the orchestrator dispatched this itself — and a bounded empty
// id is honest where an invented one would not be.
func (s *Server) recordForRun(ctx context.Context, runID string, action string, target string, payload map[string]any) error {
	if s.audit == nil {
		// Unreachable once wired, and refused rather than skipped anyway: a
		// memory proposal that nothing can record is not a lesser version of
		// this call, it is the untraceable one this method exists to prevent.
		return status.Error(codes.Internal, "memory auditing is not configured")
	}
	recorded, err := s.audit.RecordForExistingRun(ctx, runID, "runtime", "", action, target, payload)
	if err != nil {
		log.Printf("record %s for %s: %v", action, target, err)
		return nil
	}
	if !recorded {
		return status.Error(codes.FailedPrecondition, "the conversation this memory was filed against is gone")
	}
	return nil
}

// memoryError maps this package's typed failures to codes a caller can act on.
//
// Everything unrecognised collapses to a fixed Internal message. That is not
// politeness: the errors underneath carry file paths, parser output and, in the
// candidate layer, the claim itself — none of which belongs in a status a model
// or a log will see.
func memoryError(err error, fallback string) error {
	var limit *memoryfiles.LimitError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, repository.ErrMemoryVaultUnavailable):
		return status.Error(codes.FailedPrecondition, "the memory vault is not available")
	case errors.Is(err, repository.ErrMemoryCandidateNotFound):
		return status.Error(codes.NotFound, "memory candidate not found")
	case errors.Is(err, repository.ErrMemoryNoteNotFound):
		return status.Error(codes.NotFound, "memory note not found")
	case errors.Is(err, repository.ErrMemoryCandidateInvalidTransition):
		return status.Error(codes.FailedPrecondition, "this memory candidate has already been decided")
	case errors.Is(err, repository.ErrMemoryCandidateKind):
		return status.Error(codes.FailedPrecondition, "this memory candidate is not of the kind this decision applies to")
	case errors.Is(err, repository.ErrMemoryCandidateBody):
		return status.Error(codes.InvalidArgument, "the proposed memory is empty or too large")
	case errors.Is(err, repository.ErrMemoryCandidateEvidence),
		errors.Is(err, repository.ErrMemoryVaultPathMismatch):
		return status.Error(codes.FailedPrecondition, "this memory candidate is not in a state Turing can act on")
	case errors.Is(err, repository.ErrMemoryCandidateQuery), errors.Is(err, repository.ErrMemorySearchQuery):
		return status.Error(codes.InvalidArgument, "the memory query is outside the bounds this server will run")
	case errors.Is(err, memoryfiles.ErrStaleContent):
		return status.Error(codes.Aborted, "the file changed since it was read; re-read it and decide again")
	case errors.Is(err, memoryfiles.ErrConfinement):
		return status.Error(codes.PermissionDenied, "that path is not somewhere memory may touch")
	case errors.Is(err, memoryfiles.ErrKind):
		return status.Error(codes.FailedPrecondition, "this memory candidate is not of the kind this decision applies to")
	case errors.As(err, &limit):
		return status.Errorf(codes.InvalidArgument, "%s exceeds its %d byte limit", limit.What, limit.Limit)
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "the memory request was cancelled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "the memory request ran out of time")
	default:
		return status.Error(codes.Internal, fallback)
	}
}
