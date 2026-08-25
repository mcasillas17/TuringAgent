package sessions

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/persisttime"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	eventsvc "github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/runstate"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	turingv1.UnimplementedSessionServiceServer
	repo             *repository.Repository
	search           messageSearcher
	cfg              config.Config
	capabilities     capabilitySource
	cursors          sessionCursorCodec
	bus              *eventsvc.Bus
	artifactCleaners []SessionArtifactCleaner
	memoryCompletion repository.SessionDeletionCompletion
}

// messageSearcher is the only part of the repository the search handler needs.
// It exists so a test can hand the handler a repository that fails the way a
// broken metadata invariant fails, which a real SQLite database will not do on
// demand. It is deliberately narrow: every other operation still goes through
// the concrete repository, so this seam cannot be widened into a general
// stand-in for storage.
//
// The signatures mirror *repository.Repository exactly, so the compiler
// rejects a stub that answers a different question than production does.
type messageSearcher interface {
	SearchMessages(
		ctx context.Context,
		sessionID string,
		excludedSessionID string,
		query string,
		limit int,
	) ([]repository.Message, error)
	SearchMessageHits(
		ctx context.Context,
		sessionID string,
		excludedSessionID string,
		query string,
		limit int,
	) ([]repository.SearchHit, error)
}

type capabilitySource interface {
	ProviderCapabilities() map[turingv1.ModelProvider][]*turingv1.ModelCapability
	AgentAvailable(turingv1.AgentId) bool
	RoutableDefaultModel(string, string) string
	LiveToolNames() []string
}

type sessionDeletionCanceler interface {
	CancelSessionRuns(context.Context, string, string)
}

const artifactCleanupTimeout = 10 * time.Second

// memoryCompletionTimeout bounds the on-disk work a withdrawal owes after its
// rows are gone. It is larger than one cleaner call because the pass walks the
// whole vault, and it is bounded at all because an unbounded one would hold a
// withdrawal open on a filesystem that has stopped answering.
const memoryCompletionTimeout = 30 * time.Second

func New(repo *repository.Repository, cfg config.Config, capabilities capabilitySource, buses ...*eventsvc.Bus) *Server {
	var bus *eventsvc.Bus
	if len(buses) > 0 {
		bus = buses[0]
	}
	return &Server{
		repo:         repo,
		search:       repo,
		cfg:          cfg,
		capabilities: capabilities,
		cursors:      newSessionCursorCodec(cfg.CursorHMACKey),
		bus:          bus,
	}
}

// RegisterArtifactCleaners records the cleaners a withdrawal dispatches, one
// per manifest scope. It appends rather than replaces, so a second call adds a
// scope instead of silently dropping the one registered first.
//
// The list is a list, not an order of precedence: every pass attempts all of
// them, so registration order cannot change what a withdrawal leaves behind.
func (s *Server) RegisterArtifactCleaners(cleaners ...SessionArtifactCleaner) {
	for _, cleaner := range cleaners {
		if cleaner == nil {
			continue
		}
		s.artifactCleaners = append(s.artifactCleaners, cleaner)
	}
}

// SetMemoryReconcileCompletion attaches the file-writing pass a withdrawal owes
// once its rows are gone: a belief the user kept must stop citing a
// conversation Turing has just told them no longer exists.
func (s *Server) SetMemoryReconcileCompletion(completion repository.SessionDeletionCompletion) {
	s.memoryCompletion = completion
}

// ResumePendingDeletions retries durable non-completed receipts. It is safe to
// call repeatedly: each receipt uses the same lifecycle version and only a
// completed receipt can publish the terminal event.
func (s *Server) ResumePendingDeletions(ctx context.Context) error {
	sessionIDs, err := s.repo.PendingSessionDeletionIDs(ctx)
	if err != nil {
		return err
	}
	var resumeErr error
	for _, sessionID := range sessionIDs {
		if _, err := s.DeleteSession(ctx, &turingv1.DeleteSessionRequest{SessionId: sessionID}); err != nil {
			if status.Code(err) == codes.NotFound {
				continue
			}
			resumeErr = errors.Join(resumeErr, err)
		}
	}
	return resumeErr
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

// DeleteSession starts or advances a durable, non-blocking withdrawal. A live
// execution leaves a retryable receipt rather than keeping the RPC open or
// deleting rows from under the worker.
func (s *Server) DeleteSession(ctx context.Context, req *turingv1.DeleteSessionRequest) (*turingv1.DeleteSessionResponse, error) {
	if req == nil || !validSessionID(req.SessionId) {
		return nil, status.Error(codes.InvalidArgument, "session_id is invalid")
	}
	receipt, err := s.repo.BeginSessionDeletion(ctx, req.SessionId)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrSessionNotFound):
			return nil, status.Error(codes.NotFound, "session not found")
		default:
			return nil, status.Error(codes.Internal, "delete session failed")
		}
	}
	if canceler, ok := s.capabilities.(sessionDeletionCanceler); ok {
		canceler.CancelSessionRuns(ctx, req.SessionId, "session_deleting")
	}
	receipt, err = s.repo.AdvanceSessionDeletion(ctx, req.SessionId, s.deletionCompletion())
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrSessionNotFound):
			return nil, status.Error(codes.NotFound, "session not found")
		default:
			return nil, status.Error(codes.Internal, "delete session failed")
		}
	}
	// Exactly this literal, and nothing else, dispatches the cleaners. Every
	// other failure class describes a withdrawal that is stuck for a reason
	// deleting the user's files would not fix — answering one of those by
	// reaching into the sandbox or the vault is a deletion nobody asked for.
	if receipt.State == "failed_external" &&
		receipt.ErrorCode == repository.SessionDeletionArtifactCleanupPending &&
		len(s.artifactCleaners) > 0 {
		outcome := s.runArtifactCleaners(ctx, receipt)
		if outcome.failed() {
			current, err := s.recordArtifactCleanupFailure(ctx, receipt.SessionID, outcome)
			if err != nil {
				return nil, err
			}
			receipt = current
		} else {
			receipt, err = s.repo.AdvanceSessionDeletion(ctx, req.SessionId, s.deletionCompletion())
			if err != nil {
				return nil, status.Error(codes.Internal, "delete session failed")
			}
		}
	}
	if receipt.State == "completed" {
		s.publishSessionDeleted(receipt)
	}
	return &turingv1.DeleteSessionResponse{
		SessionId: req.SessionId,
		Deletion:  mapSessionDeletionReceipt(receipt),
	}, nil
}

// deletionCompletion hands the withdrawal the on-disk work it owes after the
// cascade. It is nil when nothing is wired, which is what every caller that has
// no vault gets.
func (s *Server) deletionCompletion() repository.SessionDeletionCompletion {
	completion := s.memoryCompletion
	if completion == nil {
		return nil
	}
	return func(ctx context.Context) error {
		completionCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), memoryCompletionTimeout)
		defer cancel()
		return completion(completionCtx)
	}
}

// artifactCleanupOutcome is what one dispatch pass observed, split by what
// actually went wrong.
//
// The split is the point. A scope whose removal failed still has the user's
// files in it, and saying so means marking every one of its rows delete_failed
// with an audit row naming the file. A scope whose removal succeeded and whose
// manifest could not then be drained is the opposite situation described in the
// same breath: the files are gone, the rows that named them are the retry's
// worklist, and marking them files a per-file audit row claiming Turing could
// not delete a file the user's own withdrawal did delete. Collapsing the two
// into one list of "failed scopes" is what makes that false record possible.
type artifactCleanupOutcome struct {
	// cleanupFailures names the scopes whose external files are still there.
	cleanupFailures []string
	// finalizeFailures names the scopes whose files are gone and whose
	// manifest rows could not be dropped.
	finalizeFailures []string
}

func (o artifactCleanupOutcome) failed() bool {
	return len(o.cleanupFailures) > 0 || len(o.finalizeFailures) > 0
}

// unsupportedScope reports whether any failure came from a cleaner registered
// under a scope this withdrawal has no manifest for.
func (o artifactCleanupOutcome) unsupportedScope() bool {
	for _, scope := range o.cleanupFailures {
		if scope != ArtifactScopeSandbox && scope != ArtifactScopeVault {
			return true
		}
	}
	for _, scope := range o.finalizeFailures {
		if scope != ArtifactScopeSandbox && scope != ArtifactScopeVault {
			return true
		}
	}
	return false
}

// runArtifactCleaners attempts every registered cleaner and reports what it
// observed.
//
// It never short-circuits. Each scope is a separate store with a separate
// manifest, and stopping at the first failure would leave the other scope's
// files in place with nothing recording why — the sandbox holding the user's
// scratch output because the vault was unreachable, or the reverse. A cleaner
// that finishes also forgets its own rows here, even when a sibling failed, so
// the manifest that survives names exactly the files that survived.
func (s *Server) runArtifactCleaners(
	ctx context.Context,
	receipt repository.SessionDeletionReceipt,
) artifactCleanupOutcome {
	var outcome artifactCleanupOutcome
	completed := make([]SessionArtifactCleaner, 0, len(s.artifactCleaners))
	for _, cleaner := range s.artifactCleaners {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), artifactCleanupTimeout)
		err := cleaner.CleanupSessionArtifacts(cleanupCtx, receipt.SessionID, receipt.LifecycleVersion)
		cancel()
		if err != nil {
			outcome.cleanupFailures = append(outcome.cleanupFailures, cleaner.ArtifactScope())
			continue
		}
		completed = append(completed, cleaner)
	}
	for _, cleaner := range completed {
		if err := cleaner.ForgetCleanedArtifacts(context.WithoutCancel(ctx), receipt.SessionID); err != nil {
			// The files are gone and the rows are not. That still holds the
			// withdrawal open — the pending gate counts rows, so the next pass
			// reruns this scope, and removal is idempotent, so rerunning it
			// over files that are already gone drains the rows rather than
			// repeating the work. What it is not is a file Turing could not
			// delete, and it is reported separately so nothing records it as
			// one.
			outcome.finalizeFailures = append(outcome.finalizeFailures, cleaner.ArtifactScope())
		}
	}
	return outcome
}

// recordArtifactCleanupFailure marks each failing scope's own manifest and
// leaves every other scope's rows and audit trail alone.
//
// The receipt carries one error class for the whole withdrawal, derived from
// what failed rather than from the order the cleaners were tried in, so two
// installs that registered their cleaners differently produce the same receipt.
// The per-scope attribution lives where it can be acted on: in the rows that
// were marked and the audit row each one produced.
//
// Only a scope whose files are still there has its manifest marked. A manifest
// that could not be finalized, and a scope with no manifest here at all, are
// recorded on the receipt alone — there is nothing truthful to mark in either
// case, and marking anyway is how a withdrawal files a deletion failure for a
// file it deleted, or attributes a stranger's failure to a store that worked.
func (s *Server) recordArtifactCleanupFailure(
	ctx context.Context,
	sessionID string,
	outcome artifactCleanupOutcome,
) (repository.SessionDeletionReceipt, error) {
	errorCode := artifactCleanupErrorCode(outcome)
	failureCtx := context.WithoutCancel(ctx)
	marked := false
	// A stranger scope decides the receipt's class but does not silence the
	// failures beside it. A sandbox or vault removal that genuinely failed left
	// the user's files on disk, and the rows naming them are the only place
	// that fact is written down; withholding it until someone unregisters a
	// misconfigured cleaner would lose the evidence for the failure that
	// actually touched their data.
	failed := make(map[string]bool, len(outcome.cleanupFailures))
	for _, scope := range outcome.cleanupFailures {
		failed[scope] = true
	}
	if failed[ArtifactScopeSandbox] {
		if err := s.repo.MarkSessionDeletionSandboxFailure(failureCtx, sessionID, errorCode); err != nil {
			return repository.SessionDeletionReceipt{}, status.Error(codes.Internal, "record session artifact cleanup failure")
		}
		marked = true
	}
	if failed[ArtifactScopeVault] {
		if err := s.repo.MarkSessionDeletionVaultFailure(failureCtx, sessionID, errorCode); err != nil {
			return repository.SessionDeletionReceipt{}, status.Error(codes.Internal, "record session artifact cleanup failure")
		}
		marked = true
	}
	// Nothing was marked, so nothing has recorded the failure yet. Leaving it
	// there hands back the pending gate — a withdrawal that looks like it is
	// politely waiting for a cleaner which has in fact already failed, and it
	// waits forever.
	if !marked {
		if err := s.repo.MarkSessionDeletionReceiptFailure(failureCtx, sessionID, errorCode); err != nil {
			return repository.SessionDeletionReceipt{}, status.Error(codes.Internal, "record session artifact cleanup failure")
		}
	}
	current, err := s.repo.SessionDeletionReceipt(ctx, sessionID)
	if err != nil {
		return repository.SessionDeletionReceipt{}, status.Error(codes.Internal, "read session deletion receipt")
	}
	return current, nil
}

// artifactCleanupErrorCode names the one opaque class the receipt reports for
// what a dispatch pass observed.
//
// A scope this withdrawal has no manifest for comes first, because it is a
// wiring mistake rather than a store being unavailable and no retry will fix
// it. Then a removal that failed, because the user's files are still there: a
// vault-only failure says so, since it is the one a user can act on by closing
// their editor, and anything wider falls back to the general artifact class
// rather than picking a winner between scopes. A manifest that could not be
// finalized is last and separate — the files are gone, and reporting that under
// a cleanup class would tell the user their notes are still on disk.
func artifactCleanupErrorCode(outcome artifactCleanupOutcome) string {
	if outcome.unsupportedScope() {
		return repository.SessionDeletionUnsupportedArtifactScope
	}
	if len(outcome.cleanupFailures) == 0 {
		return repository.SessionDeletionArtifactManifestFinalizeFailed
	}
	if len(outcome.cleanupFailures) == 1 && outcome.cleanupFailures[0] == ArtifactScopeVault {
		return repository.SessionDeletionVaultCleanupFailed
	}
	return repository.SessionDeletionSandboxCleanupFailed
}

func (s *Server) ListSessionDeletionReceipts(ctx context.Context, _ *turingv1.ListSessionDeletionReceiptsRequest) (*turingv1.ListSessionDeletionReceiptsResponse, error) {
	receipts, err := s.repo.PendingSessionDeletionReceipts(ctx)
	if err != nil {
		return nil, status.Error(codes.Internal, "list session deletion receipts failed")
	}
	out := make([]*turingv1.SessionDeletionReceipt, 0, len(receipts))
	for _, receipt := range receipts {
		out = append(out, mapSessionDeletionReceipt(receipt))
	}
	return &turingv1.ListSessionDeletionReceiptsResponse{Deletions: out}, nil
}

func (s *Server) publishSessionDeleted(receipt repository.SessionDeletionReceipt) {
	if s.bus == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"lifecycleVersion": receipt.LifecycleVersion,
		"runs":             receipt.RunCount,
		"messages":         receipt.MessageCount,
	})
	if err != nil {
		return
	}
	s.bus.TerminateSession(eventsvc.Event{
		EventID:     "session_deleted:" + receipt.SessionID,
		SessionID:   receipt.SessionID,
		Sequence:    receipt.TerminalSequence,
		Type:        "session.deleted",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		PayloadJSON: string(payload),
	})
}

func mapSessionDeletionReceipt(receipt repository.SessionDeletionReceipt) *turingv1.SessionDeletionReceipt {
	state := turingv1.SessionDeletionState_SESSION_DELETION_STATE_IN_PROGRESS
	switch receipt.State {
	case "failed_external":
		state = turingv1.SessionDeletionState_SESSION_DELETION_STATE_FAILED_EXTERNAL
	case "completed":
		state = turingv1.SessionDeletionState_SESSION_DELETION_STATE_COMPLETED
	}
	return &turingv1.SessionDeletionReceipt{
		SessionId:                   receipt.SessionID,
		State:                       state,
		LifecycleVersion:            receipt.LifecycleVersion,
		Retryable:                   receipt.Retryable,
		ErrorCode:                   receipt.ErrorCode,
		TerminalSequence:            receipt.TerminalSequence,
		RunCount:                    int32(receipt.RunCount),
		MessageCount:                int32(receipt.MessageCount),
		RetainedLegacyArtifactCount: int32(receipt.RetainedLegacyArtifactCount),
	}
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
			if req.BeforeMessageId == "" {
				return nil, status.Error(codes.NotFound, "session not found")
			}
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

// SearchMessages answers in exactly one projection. The response carries both
// a legacy message list and a scored hit list, and filling both would let a
// client read whichever it happened to know about while paying twice for the
// same rows on the wire.
func (s *Server) SearchMessages(ctx context.Context, req *turingv1.SearchMessagesRequest) (*turingv1.SearchMessagesResponse, error) {
	if req == nil || strings.TrimSpace(req.Query) == "" {
		return nil, status.Error(codes.InvalidArgument, "query is required")
	}
	// No default fallback: an unrecognized format is a client this server does
	// not understand, and answering it with a guess would silently serve a
	// future format's request with today's shape.
	switch req.GetResponseFormat() {
	case turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_UNSPECIFIED,
		turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_LEGACY_MESSAGES:
		messages, err := s.search.SearchMessages(
			ctx,
			req.SessionId,
			req.ExcludeSessionId,
			req.Query,
			int(req.Limit),
		)
		if err != nil {
			return nil, searchMessagesError(err)
		}
		return &turingv1.SearchMessagesResponse{Messages: mapSearchMessages(messages)}, nil
	case turingv1.SearchMessagesResponseFormat_SEARCH_MESSAGES_RESPONSE_FORMAT_HITS:
		hits, err := s.search.SearchMessageHits(
			ctx,
			req.SessionId,
			req.ExcludeSessionId,
			req.Query,
			int(req.Limit),
		)
		if err != nil {
			return nil, searchMessagesError(err)
		}
		return &turingv1.SearchMessagesResponse{Hits: mapSearchHits(hits)}, nil
	default:
		return nil, status.Error(codes.InvalidArgument, "response_format is invalid")
	}
}

// searchMessagesError turns any search failure into one opaque status.
//
// A metadata invariant failure means the repository refused to publish a score
// or snippet it could not prove, which is a defect an operator has to be able
// to see. The row that provoked it is someone's private conversation, and the
// wrapped error carries its text, so only the invariant's class name is
// logged: enough to find the bug, nothing about who was searching for what.
// Ordinary database errors are not logged here at all; they say nothing an
// operator cannot get from the database itself, and their text quotes content.
func searchMessagesError(err error) error {
	if class, ok := searchInvariantClass(err); ok {
		log.Printf("search_messages invariant=%s", class)
	}
	return status.Error(codes.Internal, "search messages failed")
}

// searchInvariantClass names the broken invariant without quoting it. The
// names are a stable log vocabulary, deliberately separate from the sentinel
// error strings so that rewording an error does not silently rename a log
// class an operator greps for.
func searchInvariantClass(err error) (string, bool) {
	switch {
	case errors.Is(err, repository.ErrSearchMarkerEntropy):
		return "marker_entropy", true
	case errors.Is(err, repository.ErrInvalidSearchScore):
		return "invalid_score", true
	case errors.Is(err, repository.ErrSearchSnippetMarkerCollision):
		return "marker_collision", true
	case errors.Is(err, repository.ErrInvalidSearchSnippetMarkers):
		return "marker_structure", true
	case errors.Is(err, repository.ErrInvalidSearchSnippet):
		return "invalid_snippet", true
	default:
		return "", false
	}
}

func mapSearchMessages(messages []repository.Message) []*turingv1.Message {
	out := make([]*turingv1.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, mapMessage(message.SessionID, message))
	}
	return out
}

func mapSearchHits(hits []repository.SearchHit) []*turingv1.SearchHit {
	out := make([]*turingv1.SearchHit, 0, len(hits))
	for _, hit := range hits {
		out = append(out, &turingv1.SearchHit{
			Message: mapMessage(hit.Message.SessionID, hit.Message),
			Score:   hit.Score,
			Snippet: hit.Snippet,
		})
	}
	return out
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
			Provider:              turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE,
			Enabled:               len(advertised[turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE]) > 0,
			DefaultModel:          openAIDefault,
			Models:                advertised[turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE],
			RemoteEndpoint:        s.cfg.OpenAIBaseURL,
			RequiresPerRunConsent: true,
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
	mapped := &turingv1.Message{
		MessageId:   message.MessageID,
		SessionId:   sessionID,
		RunId:       message.RunID,
		Role:        mapRole(message.Role),
		Content:     message.Content,
		ContentType: message.ContentType,
		Sequence:    message.Sequence,
		CreatedAt:   parseTimestamp(message.CreatedAt),
	}
	// Absent state stays absent. The repository returns state only for a
	// message whose run correlation it could prove, and the projection returns
	// none for a row it cannot vouch for; either way what is published is
	// silence rather than an outcome nobody stands behind. Flutter renders that
	// absence neutrally instead of inventing a terminal result.
	if message.RunState != nil {
		mapped.RunState = runstate.Project(*message.RunState)
	}
	return mapped
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
