package chat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"sort"
	"strings"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/safejson"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	turingv1.UnimplementedChatServiceServer
	repo        *repository.Repository
	bus         *events.Bus
	runtime     runtimeDispatcher
	ollamaModel string
	openAIModel string
	// afterRunStateReadForCancel is the barrier the disconnect version-race
	// regression installs, at the exact point between reading the run's version
	// and guarding the cancellation with it. It is nil in production.
	//
	// It lives on the server rather than on the package so two harnesses in one
	// test binary cannot install it over each other, and so a forgotten restore
	// cannot leak into an unrelated test. Written by the test that owns this
	// server before it triggers the path, and read from whatever goroutine runs
	// it — an ordering the test establishes, not one this field enforces.
	afterRunStateReadForCancel func(runID string)
}

type runtimeDispatcher interface {
	DispatchPending(context.Context) error
	CancelRun(context.Context, string, string)
	ValidateRouting(context.Context, repository.RoutingRequirements) error
	RefreshPendingRoutingState(context.Context, string) error
	RoutableDefaultModel(string, string) string
}

func New(repo *repository.Repository, bus *events.Bus, runtimeServer runtimeDispatcher, ollamaModel string, openAIModel string) *Server {
	return &Server{repo: repo, bus: bus, runtime: runtimeServer, ollamaModel: ollamaModel, openAIModel: openAIModel}
}

func (s *Server) SendMessage(req *turingv1.SendMessageRequest, stream turingv1.ChatService_SendMessageServer) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is required")
	}
	if req.SessionId == "" {
		return status.Error(codes.InvalidArgument, "session_id is required")
	}
	if req.Content == "" {
		return status.Error(codes.InvalidArgument, "content is required")
	}
	if err := requestContentType(req.ContentType); err != nil {
		return err
	}
	requestedTools, err := requestTools(req.GetRequestedTools())
	if err != nil {
		return err
	}
	if req.GetRequiredContextTokens() < 0 {
		return status.Error(codes.InvalidArgument, "required_context_tokens must be non-negative")
	}
	if req.GetMinimumWorkerMaxConcurrentRuns() < 0 {
		return status.Error(codes.InvalidArgument, "minimum_worker_max_concurrent_runs must be non-negative")
	}
	agentID, err := requestAgentID(req.AgentId)
	if err != nil {
		return err
	}
	modelProvider, err := requestModelProvider(req.ModelProvider)
	if err != nil {
		return err
	}
	model := req.Model
	executionModel := model
	if executionModel == "" {
		configured := s.ollamaModel
		if modelProvider == "openai_compatible" {
			configured = s.openAIModel
		}
		model = configured
		executionModel = configured
		if s.runtime != nil {
			executionModel = s.runtime.RoutableDefaultModel(modelProvider, configured)
		}
	}
	if _, err := s.repo.GetSession(ctx, req.SessionId); err != nil {
		return mapSessionError(ctx, err)
	}
	ch, unsubscribe := s.bus.Subscribe(req.SessionId)
	defer unsubscribe()
	input := repository.EnqueueUserMessageInput{
		SessionID:                      req.SessionId,
		Content:                        req.Content,
		ContentType:                    "text",
		AgentID:                        agentID,
		ModelProvider:                  modelProvider,
		Model:                          model,
		ExecutionModel:                 executionModel,
		IdempotencyKey:                 req.IdempotencyKey,
		RequestedTools:                 requestedTools,
		RequiredContextTokens:          int(req.GetRequiredContextTokens()),
		MinimumWorkerMaxConcurrentRuns: int(req.GetMinimumWorkerMaxConcurrentRuns()),
	}
	if s.runtime != nil {
		input.ValidateRouting = s.runtime.ValidateRouting
	}
	enqueued, err := s.repo.EnqueueUserMessage(ctx, input)
	if err != nil {
		return mapEnqueueError(ctx, err)
	}
	if !enqueued.Replayed {
		s.bus.Publish(busEventFromRepository(enqueued.SessionUpdatedEvent))
	}
	queuedEvent := enqueued.QueuedEvent
	if !enqueued.Replayed {
		s.bus.Publish(busEventFromRepository(queuedEvent))
	}
	// Published alongside the queued event so a subscriber that is not this
	// stream — a second window on the same conversation — still learns that
	// the message left the machine. Replay would eventually deliver them, but
	// only after a poll, and "you were told late" is not much better than "you
	// were not told".
	if !enqueued.Replayed {
		for _, event := range enqueued.RoutingEvents {
			s.bus.Publish(busEventFromRepository(event))
		}
	}
	cancelRunOnClientDisconnect := req.IdempotencyKey == ""
	if err := stream.Send(&turingv1.ChatStreamEvent{
		SessionId: req.SessionId,
		RunId:     enqueued.RunID,
		TraceId:   enqueued.TraceID,
		Sequence:  queuedEvent.Sequence,
		Event: &turingv1.ChatStreamEvent_RunQueued{RunQueued: &turingv1.RunQueued{
			RunId:   enqueued.RunID,
			JobId:   enqueued.JobID,
			TraceId: enqueued.TraceID,
		}},
	}); err != nil {
		if req.IdempotencyKey != "" {
			if dispatchErr := s.dispatchEnqueued(ctx, enqueued, cancelRunOnClientDisconnect); dispatchErr != nil {
				return dispatchErr
			}
		}
		if cancelRunOnClientDisconnect {
			s.cancelRunIfClientCancelled(ctx, enqueued.RunID)
		}
		return err
	}
	if err := s.dispatchEnqueued(ctx, enqueued, cancelRunOnClientDisconnect); err != nil {
		return err
	}
	lastSent := queuedEvent.Sequence
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if cancelRunOnClientDisconnect {
				s.cancelRun(enqueued.RunID)
			}
			return status.Error(codes.Canceled, "client cancelled stream")
		case _, ok := <-ch:
			if !ok {
				return nil
			}
			done, err := s.streamAvailableEvents(ctx, req.SessionId, enqueued.RunID, &lastSent, cancelRunOnClientDisconnect, stream)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		case <-ticker.C:
			done, err := s.streamAvailableEvents(ctx, req.SessionId, enqueued.RunID, &lastSent, cancelRunOnClientDisconnect, stream)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
		}
	}
}

func (s *Server) dispatchEnqueued(ctx context.Context, enqueued repository.EnqueueUserMessageResult, cancelRunOnFailure bool) error {
	if enqueued.Replayed {
		replayCtx, replayCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		run, err := s.repo.GetRun(replayCtx, enqueued.RunID)
		replayCancel()
		if err != nil {
			if ctx.Err() != nil {
				return status.Error(codes.Canceled, "client cancelled stream")
			}
			return status.Error(codes.Internal, "get replayed run failed")
		}
		if run.Status != "queued" {
			return nil
		}
	}
	return s.dispatchPending(ctx, enqueued.RunID, cancelRunOnFailure)
}

func (s *Server) dispatchPending(ctx context.Context, runID string, cancelRunOnFailure bool) error {
	if s.runtime == nil {
		return nil
	}
	if err := s.runtime.DispatchPending(context.WithoutCancel(ctx)); err != nil {
		if cancelRunOnFailure {
			s.cancelRun(runID)
		}
		if ctx.Err() != nil {
			return status.Error(codes.Canceled, "client cancelled stream")
		}
		return status.Error(codes.Internal, "dispatch pending job failed")
	}
	if err := s.runtime.RefreshPendingRoutingState(context.WithoutCancel(ctx), "message enqueued"); err != nil {
		log.Printf("refresh pending routing state for run %s: %v", runID, err)
	}
	return nil
}

func (s *Server) streamAvailableEvents(ctx context.Context, sessionID string, runID string, lastSent *int64, cancelRunOnClientDisconnect bool, stream turingv1.ChatService_SendMessageServer) (bool, error) {
	const replayLimit = 500
	for {
		replayed, _, err := s.repo.ReplayEvents(ctx, sessionID, *lastSent, replayLimit)
		if err != nil {
			if ctx.Err() != nil {
				if cancelRunOnClientDisconnect {
					s.cancelRun(runID)
				}
				return false, status.Error(codes.Canceled, "client cancelled stream")
			}
			return false, status.Error(codes.Internal, "replay events failed")
		}
		if len(replayed) == 0 {
			return false, nil
		}
		for _, event := range replayed {
			if event.Sequence > *lastSent {
				*lastSent = event.Sequence
			}
			if !event.RunID.Valid || event.RunID.String != runID {
				continue
			}
			if err := stream.Send(mapChatEvent(busEventFromRepository(event))); err != nil {
				if cancelRunOnClientDisconnect {
					s.cancelRunIfClientCancelled(ctx, runID)
				}
				return false, err
			}
			if isTerminalEvent(event.Type) {
				return true, nil
			}
		}
		if len(replayed) < replayLimit {
			return false, nil
		}
	}
}

func requestAgentID(agentID turingv1.AgentId) (string, error) {
	switch agentID {
	case turingv1.AgentId_AGENT_ID_UNSPECIFIED, turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT:
		return "general_assistant", nil
	default:
		return "", status.Errorf(codes.InvalidArgument, "agent_id %d is unsupported", agentID)
	}
}

func requestTools(requested []string) ([]string, error) {
	unique := make(map[string]struct{}, len(requested))
	for _, value := range requested {
		value = strings.TrimSpace(value)
		parts := strings.Split(value, "/")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return nil, status.Error(codes.InvalidArgument, "requested_tools entries must use server/tool")
		}
		unique[strings.TrimSpace(parts[0])+"/"+strings.TrimSpace(parts[1])] = struct{}{}
	}
	tools := make([]string, 0, len(unique))
	for tool := range unique {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	return tools, nil
}

func requestModelProvider(provider turingv1.ModelProvider) (string, error) {
	switch provider {
	case turingv1.ModelProvider_MODEL_PROVIDER_UNSPECIFIED, turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA:
		return "ollama", nil
	case turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE:
		return "openai_compatible", nil
	default:
		return "", status.Error(codes.InvalidArgument, "model_provider is unsupported")
	}
}

func requestContentType(contentType string) error {
	if contentType == "" || contentType == "text" {
		return nil
	}
	return status.Error(codes.InvalidArgument, "content_type is unsupported")
}

func isTerminalEvent(eventType string) bool {
	switch eventType {
	case "agent.run.completed", "agent.run.failed", "agent.run.cancelled":
		return true
	default:
		return false
	}
}

func mapSessionError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return status.Error(codes.Canceled, "client cancelled stream")
	}
	if errors.Is(err, sql.ErrNoRows) {
		return status.Error(codes.NotFound, "session not found")
	}
	return status.Error(codes.Internal, "get session failed")
}

func mapEnqueueError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return status.Error(codes.Canceled, "client cancelled stream")
	}
	if errors.Is(err, repository.ErrIdempotencyConflict) {
		return status.Error(codes.AlreadyExists, "idempotency key was already used for a different request")
	}
	if status.Code(err) == codes.FailedPrecondition {
		return err
	}
	return status.Error(codes.Internal, "enqueue user message failed")
}

// errAbandonmentContended reports that a run's version kept moving underneath
// the abandonment until its budget ran out.
//
// It says that and nothing else: it is logged, and a log line is a durable
// operator-facing record, so a run ID, a session, or a database driver's
// sentence must not be able to ride out on it.
var errAbandonmentContended = errors.New("abandonment lost its version race on every attempt")

// maxCancelVersionAttempts bounds how many times an abandonment re-reads a run
// whose version moved underneath it.
//
// The window is one read and one guarded update wide, and the writers that can
// land inside it are the lifecycle transitions a single run makes — an ownership
// fence, a resume, an approval decision. Three attempts covers a run that is
// being moved while its client disappears; a fourth would mean something is
// rewriting this run continuously, and spinning against that would hold a
// goroutine and a database connection forever rather than let the recovery
// loop deal with it.
const maxCancelVersionAttempts = 3

// cancelRun terminalizes a run whose client went away.
//
// The one stream-cancellation signal this product has covers a deliberate stop
// and an unkeyed transport loss alike, and the client offers no cancel
// affordance, so this cannot claim the user meant it. It reports abandonment,
// which is the strongest thing that is actually true. Nothing about the
// transport's own wording is persisted.
//
// The read and the guarded update are two steps, so another writer can commit
// between them and the update then loses on version. That loss used to be
// discarded: no event, no retry, and a run nobody was watching kept executing.
// It is retried instead, against a freshly read version, a bounded number of
// times. Only a version conflict is retried — a refusal means the run is
// already terminal and there is nothing left to cancel, and a missing run means
// it was deleted.
func (s *Server) cancelRun(runID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.abandonRun(ctx, runID); err != nil {
		// Losing the whole budget is not a no-op: a run that was supposed to
		// stop is still marked as going, and nobody is watching it. Discarding
		// that made an exhausted abandonment look exactly like a clean one — no
		// log line, no test could observe it, and the only evidence was a run
		// that never ended. Reported here rather than returned, because the
		// caller is a deferred cleanup with nowhere to put an error.
		log.Printf("abandon run %s: %v", runID, err)
	}
	// Told to stop either way. The durable half failing does not make the run
	// stop executing, and leaving the runtime uninformed would turn a lost race
	// into a worker slot held by a run nobody wants.
	if s.runtime != nil {
		s.runtime.CancelRun(ctx, runID, "client_cancelled")
	}
}

// abandonRun is the durable half: it terminalizes the run and publishes what
// that transition produced, or reports why it could not.
//
// Split out from cancelRun so the outcome is a value rather than a side effect.
// A caller — and a test — can then tell "terminalized", "already terminal or
// gone", and "contended until the budget ran out" apart, which the single
// swallowing loop could not express.
func (s *Server) abandonRun(ctx context.Context, runID string) error {
	for attempt := 0; attempt < maxCancelVersionAttempts; attempt++ {
		state, err := s.repo.GetRunState(ctx, runID)
		if err != nil {
			// Deletion and a closed database both land here. Neither is a race
			// this can win, and neither leaves a run to terminalize. Reported as
			// success rather than as a contended loss: nothing was lost.
			return nil
		}
		if s.afterRunStateReadForCancel != nil {
			s.afterRunStateReadForCancel(runID)
		}
		result, err := s.repo.CancelRunCanonical(ctx, repository.CancelRunInput{
			RunID:                runID,
			ExpectedStateVersion: state.StateVersion,
			Cancellation:         runoutcome.AbandonedCancellation(),
		})
		if errors.Is(err, repository.ErrRunTransitionConflict) && ctx.Err() == nil {
			continue
		}
		if err != nil {
			// A refusal means the run is already terminal and there is nothing
			// left to cancel. The error itself is not reported onward: it is a
			// repository sentinel or a driver's sentence, and this value is
			// logged.
			return nil
		}
		// Only the committed transition publishes. A duplicate carries no
		// events, so a replayed abandonment cannot announce a second
		// cancellation for a run that already has one.
		for _, event := range result.Events {
			s.bus.Publish(busEventFromRepository(event))
		}
		return nil
	}
	return errAbandonmentContended
}

func (s *Server) cancelRunIfClientCancelled(ctx context.Context, runID string) {
	if ctx.Err() == nil {
		return
	}
	s.cancelRun(runID)
}

func busEventFromRepository(event repository.Event) events.Event {
	runID := ""
	if event.RunID.Valid {
		runID = event.RunID.String
	}
	return events.Event{
		EventID:     event.EventID,
		SessionID:   event.SessionID,
		RunID:       runID,
		TraceID:     event.TraceID,
		Sequence:    event.Sequence,
		Type:        event.Type,
		CreatedAt:   event.CreatedAt,
		PayloadJSON: event.PayloadJSON,
	}
}

func mapChatEvent(event events.Event) *turingv1.ChatStreamEvent {
	payload, err := decodePayload(event.PayloadJSON)
	if err != nil {
		out := baseChatEvent(event)
		out.Event = &turingv1.ChatStreamEvent_RunFailed{RunFailed: &turingv1.RunFailed{
			RunId:   event.RunID,
			Code:    "invalid_event_payload",
			Message: err.Error(),
		}}
		return out
	}
	switch event.Type {
	case "message.delta":
		out := baseChatEvent(event)
		out.Event = &turingv1.ChatStreamEvent_TokenDelta{TokenDelta: &turingv1.TokenDelta{
			MessageId: payloadString(payload, "messageId", "message_id"),
			Delta:     payloadString(payload, "delta"),
		}}
		return out
	case "message.completed":
		out := baseChatEvent(event)
		out.Event = &turingv1.ChatStreamEvent_MessageCompleted{MessageCompleted: &turingv1.MessageCompleted{
			MessageId: payloadString(payload, "messageId", "message_id"),
			Content:   payloadString(payload, "content"),
		}}
		return out
	case "agent.run.started":
		out := baseChatEvent(event)
		out.Event = &turingv1.ChatStreamEvent_RunStarted{RunStarted: &turingv1.RunStarted{
			RunId:   event.RunID,
			JobId:   payloadString(payload, "jobId", "job_id"),
			Attempt: payloadInt32(payload, "attempt"),
		}}
		return out
	case "agent.run.completed":
		out := baseChatEvent(event)
		out.Event = &turingv1.ChatStreamEvent_RunCompleted{RunCompleted: &turingv1.RunCompleted{
			RunId:              event.RunID,
			AssistantMessageId: payloadString(payload, "assistantMessageId", "assistant_message_id"),
		}}
		return out
	case "agent.run.failed":
		out := baseChatEvent(event)
		out.Event = &turingv1.ChatStreamEvent_RunFailed{RunFailed: &turingv1.RunFailed{
			RunId:     event.RunID,
			Code:      payloadString(payload, "code"),
			Message:   payloadString(payload, "message"),
			Retryable: payloadBool(payload, "retryable"),
		}}
		return out
	case "agent.run.cancelled":
		out := baseChatEvent(event)
		out.Event = &turingv1.ChatStreamEvent_RunCancelled{RunCancelled: &turingv1.RunCancelled{
			RunId:  event.RunID,
			Reason: payloadString(payload, "reason"),
		}}
		return out
	default:
		persisted, err := persistedEvent(event, payload)
		if err != nil {
			out := baseChatEvent(event)
			out.Event = &turingv1.ChatStreamEvent_RunFailed{RunFailed: &turingv1.RunFailed{
				RunId:   event.RunID,
				Code:    "invalid_event_payload",
				Message: err.Error(),
			}}
			return out
		}
		out := baseChatEvent(event)
		out.Event = &turingv1.ChatStreamEvent_PersistedEvent{PersistedEvent: persisted}
		return out
	}
}

func baseChatEvent(event events.Event) *turingv1.ChatStreamEvent {
	return &turingv1.ChatStreamEvent{
		SessionId: event.SessionID,
		RunId:     event.RunID,
		TraceId:   event.TraceID,
		Sequence:  event.Sequence,
	}
}

func decodePayload(payloadJSON string) (map[string]any, error) {
	if payloadJSON == "" {
		payloadJSON = "{}"
	}
	return safejson.DecodeObject(json.NewDecoder(strings.NewReader(payloadJSON)))
}

func payloadString(payload map[string]any, names ...string) string {
	for _, name := range names {
		if value, ok := payload[name].(string); ok {
			return value
		}
	}
	return ""
}

func payloadBool(payload map[string]any, name string) bool {
	value, ok := payload[name].(bool)
	return ok && value
}

func payloadInt32(payload map[string]any, names ...string) int32 {
	for _, name := range names {
		value, ok := payload[name].(json.Number)
		if !ok {
			continue
		}
		parsed, err := value.Int64()
		if err == nil && parsed >= -2147483648 && parsed <= 2147483647 {
			return int32(parsed)
		}
	}
	return 0
}

func persistedEvent(event events.Event, payload map[string]any) (*turingv1.TuringEvent, error) {
	protoPayload, err := safejson.ToStruct(payload)
	if err != nil {
		return nil, err
	}
	return &turingv1.TuringEvent{
		EventId:   event.EventID,
		SessionId: event.SessionID,
		RunId:     event.RunID,
		TraceId:   event.TraceID,
		Sequence:  event.Sequence,
		Type:      mapEventType(event.Type),
		CreatedAt: parseTimestamp(event.CreatedAt),
		Payload:   protoPayload,
	}, nil
}

func mapEventType(value string) turingv1.TuringEventType {
	normalized := strings.ToLower(value)
	normalized = strings.TrimPrefix(normalized, "turing_event_type_")
	normalized = strings.ReplaceAll(normalized, "_", ".")
	switch normalized {
	case "message.started":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_STARTED
	case "message.delta":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA
	case "message.completed":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED
	case "agent.run.queued":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_QUEUED
	case "agent.run.started":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STARTED
	case "agent.run.step":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP
	case "agent.run.completed":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_COMPLETED
	case "agent.run.failed":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_FAILED
	case "agent.run.cancelled":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_CANCELLED
	case "tool.call.started":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_STARTED
	case "tool.call.completed":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_COMPLETED
	case "tool.call.failed":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_FAILED
	case "tool.call.denied":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_TOOL_CALL_DENIED
	case "approval.requested":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_REQUESTED
	case "approval.approved":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_APPROVED
	case "approval.denied":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_DENIED
	case "approval.expired":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_EXPIRED
	case "approval.consumed":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_APPROVAL_CONSUMED
	case "error":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_ERROR
	case "system":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_SYSTEM
	case "session.updated":
		return turingv1.TuringEventType_TURING_EVENT_TYPE_SESSION_UPDATED
	default:
		return turingv1.TuringEventType_TURING_EVENT_TYPE_UNSPECIFIED
	}
}

func parseTimestamp(value string) *timestamppb.Timestamp {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return timestamppb.New(t)
}
