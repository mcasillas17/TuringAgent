package events

import (
	"context"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/runstate"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

// rawDiagnostics is everything a legacy or unmigrated row could be carrying
// that a public response must never repeat: a provider's own sentence, a path
// off this machine, the arguments and result of a tool call, an approval token,
// and the words a human wrote when refusing one.
var rawDiagnostics = []string{
	"connection refused by ollama at 127.0.0.1:11434",
	"/Users/someone/secrets/private.key",
	`{"command":"rm -rf /Users/someone"}`,
	"the tool printed the customer's address",
	"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.token",
	"denied because this would email the whole company",
}

// forbiddenPayloadKeys are the generic machine diagnostic fields. A public
// payload carrying any of them is republishing text nobody vouched for, whether
// or not this build recognizes the value inside.
var forbiddenPayloadKeys = []string{
	"message", "error", "errorCode", "reason", "note", "detail", "details", "stack",
	"resultSummary", "args", "approvalToken", "token", "assignmentAttemptId", "workerId",
}

type seededEventRun struct {
	sessionID          string
	runID              string
	assistantMessageID string
	stateVersion       int64
}

func seedEventRun(t *testing.T, h *eventHarness, title string) seededEventRun {
	t.Helper()
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, title)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	enqueued, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "seed", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	state, err := h.repo.GetRunState(ctx, enqueued.RunID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	return seededEventRun{
		sessionID:          session.SessionID,
		runID:              enqueued.RunID,
		assistantMessageID: enqueued.AssistantMessageID,
		stateVersion:       state.StateVersion,
	}
}

func appendLegacyEvent(t *testing.T, h *eventHarness, run seededEventRun, eventType string, payloadJSON string) repository.Event {
	t.Helper()
	event, err := h.repo.AppendEvent(context.Background(), repository.AppendEventInput{
		SessionID:   run.sessionID,
		RunID:       run.runID,
		TraceID:     "trace_legacy",
		Type:        eventType,
		PayloadJSON: payloadJSON,
	})
	if err != nil {
		t.Fatalf("append %s: %v", eventType, err)
	}
	return event
}

func listedEvent(t *testing.T, h *eventHarness, sessionID string, eventID string) *turingv1.TuringEvent {
	t.Helper()
	response, err := turingv1.NewEventServiceClient(h.conn).ListEvents(context.Background(),
		&turingv1.ListEventsRequest{SessionId: sessionID, Limit: 500})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	for _, event := range response.GetEvents() {
		if event.GetEventId() == eventID {
			return event
		}
	}
	t.Fatalf("event %s missing from replay", eventID)
	return nil
}

func assertNoRawDiagnostics(t *testing.T, payload *structpb.Struct) {
	t.Helper()
	for _, key := range forbiddenPayloadKeys {
		if _, exists := payload.GetFields()[key]; exists {
			t.Fatalf("public payload carries the diagnostic key %q: %v", key, payload.AsMap())
		}
	}
	// The values are walked rather than matched against the message's rendered
	// form, because a rendering is free to elide or abbreviate and a security
	// assertion must not depend on that.
	assertNoRawDiagnosticValue(t, payload.AsMap())
}

func assertNoRawDiagnosticValue(t *testing.T, value any) {
	t.Helper()
	switch typed := value.(type) {
	case string:
		for _, poison := range rawDiagnostics {
			if strings.Contains(typed, poison) {
				t.Fatalf("public payload republished %q", poison)
			}
		}
	case map[string]any:
		for _, nested := range typed {
			assertNoRawDiagnosticValue(t, nested)
		}
	case []any:
		for _, nested := range typed {
			assertNoRawDiagnosticValue(t, nested)
		}
	}
}

// TestPublicFailureEventsNeverExposeRawDiagnostics walks the approved public
// failure inventory. Every row is seeded as an unmigrated legacy payload —
// provider prose, paths, arguments, results, tokens and a human's denial
// rationale — and the public read must return the identity and the allowlisted
// category and nothing else.
func TestPublicFailureEventsNeverExposeRawDiagnostics(t *testing.T) {
	tests := []struct {
		name        string
		eventType   string
		payload     string
		wantKeys    map[string]string
		wantNumbers map[string]float64
	}{
		{
			name:      "agent.run.failed",
			eventType: "agent.run.failed",
			payload: `{"runId":"RUN","code":"model_error","message":"connection refused by ollama at 127.0.0.1:11434",` +
				`"retryable":true,"detail":"/Users/someone/secrets/private.key"}`,
		},
		{
			name:      "agent.run.cancelled",
			eventType: "agent.run.cancelled",
			payload:   `{"runId":"RUN","reason":"the tool printed the customer's address","message":"/Users/someone/secrets/private.key"}`,
		},
		{
			name:      "agent.run.step dispatch retry",
			eventType: "agent.run.step",
			payload: `{"note":"retrying after connection refused by ollama at 127.0.0.1:11434","attempt":2,"maxAttempts":3,` +
				`"error":"/Users/someone/secrets/private.key"}`,
			wantKeys:    map[string]string{"category": "dispatch_retry"},
			wantNumbers: map[string]float64{"attempt": 2, "maxAttempts": 3},
		},
		{
			name:      "agent.run.step recovery retry",
			eventType: "agent.run.step",
			payload:   `{"note":"worker vanished","reason":"worker_unavailable","attempt":1,"maxAttempts":3}`,
			wantKeys:  map[string]string{"category": "recovery_retry"},
		},
		{
			name:        "agent.run.step give up",
			eventType:   "agent.run.step",
			payload:     `{"note":"giving up: connection refused by ollama at 127.0.0.1:11434","attempts":3,"maxAttempts":3}`,
			wantKeys:    map[string]string{"category": "recovery_exhausted"},
			wantNumbers: map[string]float64{"attempt": 3, "maxAttempts": 3},
		},
		{
			name:      "approval.denied",
			eventType: "approval.denied",
			payload: `{"approvalId":"appr_1","toolName":"system.shell","runId":"RUN","traceId":"trace_legacy",` +
				`"message":"denied because this would email the whole company",` +
				`"reason":"denied because this would email the whole company",` +
				`"args":{"command":"rm -rf /Users/someone"},"approvalToken":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.token"}`,
			wantKeys: map[string]string{"approvalId": "appr_1", "toolName": "system.shell", "category": "policy_denied"},
		},
		{
			name:      "approval.expired",
			eventType: "approval.expired",
			payload: `{"approvalId":"appr_2","toolName":"system.shell","runId":"RUN","traceId":"trace_legacy",` +
				`"message":"connection refused by ollama at 127.0.0.1:11434","approvalToken":"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.token"}`,
			wantKeys: map[string]string{"approvalId": "appr_2", "toolName": "system.shell", "category": "expired"},
		},
		{
			name:      "tool.call.failed",
			eventType: "tool.call.failed",
			payload: `{"toolCallId":"call_1","toolName":"files.read","serverName":"files",` +
				`"error":"/Users/someone/secrets/private.key","message":"the tool printed the customer's address",` +
				`"args":{"command":"rm -rf /Users/someone"},"resultSummary":"the tool printed the customer's address"}`,
			wantKeys: map[string]string{"toolCallId": "call_1", "toolName": "files.read", "serverName": "files", "category": "tool_failure"},
		},
		{
			name:      "tool.call.denied",
			eventType: "tool.call.denied",
			payload: `{"toolCallId":"call_2","toolName":"system.shell","serverName":"system",` +
				`"reason":"denied because this would email the whole company",` +
				`"args":{"command":"rm -rf /Users/someone"}}`,
			wantKeys: map[string]string{"toolCallId": "call_2", "toolName": "system.shell", "serverName": "system", "category": "policy_denied"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newEventHarness(t)
			run := seedEventRun(t, h, test.name)
			seeded := appendLegacyEvent(t, h, run, test.eventType,
				strings.ReplaceAll(test.payload, "RUN", run.runID))
			public := listedEvent(t, h, run.sessionID, seeded.EventID)
			assertNoRawDiagnostics(t, public.GetPayload())
			for key, want := range test.wantKeys {
				if got := public.GetPayload().GetFields()[key].GetStringValue(); got != want {
					t.Fatalf("payload[%q] = %q, want %q (%s)", key, got, want, public.GetPayload())
				}
			}
			for key, want := range test.wantNumbers {
				if got := public.GetPayload().GetFields()[key].GetNumberValue(); got != want {
					t.Fatalf("payload[%q] = %v, want %v (%s)", key, got, want, public.GetPayload())
				}
			}
		})
	}
}

// TestNonFailureRunStepNoticesSurvivePublicRead is the other half of the rule.
// The governed notices — the egress warning, the model-limit note — are the
// product telling the user something true about their own run, and sanitizing
// the failure notices must not take them with it.
func TestNonFailureRunStepNoticesSurvivePublicRead(t *testing.T) {
	h := newEventHarness(t)
	run := seedEventRun(t, h, "Governed notice")
	notice, err := h.repo.AppendRunNotice(context.Background(), run.runID,
		"Sending to Claude — this message leaves your machine",
		map[string]any{"externalAgent": "Claude", "endpoint": "api.anthropic.com"})
	if err != nil {
		t.Fatalf("AppendRunNotice: %v", err)
	}
	public := listedEvent(t, h, run.sessionID, notice.EventID)
	if got := public.GetPayload().GetFields()["note"].GetStringValue(); got != "Sending to Claude — this message leaves your machine" {
		t.Fatalf("governed notice note = %q, want the egress warning intact", got)
	}
	if got := public.GetPayload().GetFields()["endpoint"].GetStringValue(); got != "api.anthropic.com" {
		t.Fatalf("governed notice endpoint = %q, want it intact", got)
	}
}

// TestEventServiceSanitizesMalformedLegacyFailureEvents covers the row nobody
// can parse. It must not become a parser message on the wire, and it must not
// become a plausible outcome either.
func TestEventServiceSanitizesMalformedLegacyFailureEvents(t *testing.T) {
	for _, eventType := range []string{
		"agent.run.failed", "agent.run.cancelled", "agent.run.step",
		"approval.denied", "approval.expired", "tool.call.failed", "tool.call.denied",
	} {
		t.Run(eventType, func(t *testing.T) {
			h := newEventHarness(t)
			run := seedEventRun(t, h, eventType)
			seeded := appendLegacyEvent(t, h, run, eventType,
				`{"code":"model_error","message":"connection refused by ollama at 127.0.0.1:11434"`)
			public := listedEvent(t, h, run.sessionID, seeded.EventID)
			assertNoRawDiagnostics(t, public.GetPayload())
			if len(public.GetPayload().GetFields()) != 0 {
				t.Fatalf("unparseable payload produced fields %s", public.GetPayload())
			}
			if public.GetRunState() != nil {
				t.Fatalf("unparseable payload produced a run state %+v", public.GetRunState())
			}
			rendered := public.String()
			for _, parserText := range []string{"unexpected end of JSON", "invalid character", "cannot unmarshal"} {
				if strings.Contains(rendered, parserText) {
					t.Fatalf("public event carries parser text %q: %s", parserText, rendered)
				}
			}
		})
	}
}

// TestUnknownStoredLifecycleAndOutcomeMapToSemanticUnknown covers a row written
// by a newer server. Saying "a phase this build cannot name" is honest;
// guessing at the nearest one, or rendering the stored word, is not.
func TestUnknownStoredLifecycleAndOutcomeMapToSemanticUnknown(t *testing.T) {
	tests := []struct {
		name          string
		lifecycle     string
		outcome       string
		wantLifecycle turingv1.RunLifecycle
		wantOutcome   turingv1.RunOutcomeReason
	}{
		{
			name:          "unknown lifecycle",
			lifecycle:     "hibernating",
			outcome:       "none",
			wantLifecycle: turingv1.RunLifecycle_RUN_LIFECYCLE_UNKNOWN,
			wantOutcome:   turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_NONE,
		},
		{
			name:          "unknown outcome",
			lifecycle:     "failed",
			outcome:       "sunspots",
			wantLifecycle: turingv1.RunLifecycle_RUN_LIFECYCLE_FAILED,
			wantOutcome:   turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_UNKNOWN,
		},
		{
			name:          "both unknown",
			lifecycle:     "hibernating",
			outcome:       "sunspots",
			wantLifecycle: turingv1.RunLifecycle_RUN_LIFECYCLE_UNKNOWN,
			wantOutcome:   turingv1.RunOutcomeReason_RUN_OUTCOME_REASON_UNKNOWN,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newEventHarness(t)
			run := seedEventRun(t, h, test.name)
			seeded := appendLegacyEvent(t, h, run, "agent.run.failed", `{"runState":{`+
				`"runId":"`+run.runID+`",`+
				`"userMessageId":"msg_user",`+
				`"assistantMessageId":"`+run.assistantMessageID+`",`+
				`"lifecycle":"`+test.lifecycle+`",`+
				`"outcomeReason":"`+test.outcome+`",`+
				`"stateVersion":7,`+
				`"stateUpdatedAt":"2026-08-20T00:00:00.000000000Z",`+
				`"hasDisplayableContent":false}}`)
			public := listedEvent(t, h, run.sessionID, seeded.EventID)
			state := public.GetRunState()
			if state == nil {
				t.Fatal("a row this build cannot fully name produced no state at all")
			}
			if state.GetLifecycle() != test.wantLifecycle || state.GetOutcomeReason() != test.wantOutcome {
				t.Fatalf("state = %v/%v, want %v/%v",
					state.GetLifecycle(), state.GetOutcomeReason(), test.wantLifecycle, test.wantOutcome)
			}
			rendered := public.String()
			for _, stored := range []string{"hibernating", "sunspots"} {
				if strings.Contains(rendered, stored) {
					t.Fatalf("public event rendered the stored word %q: %s", stored, rendered)
				}
			}
		})
	}
}

// TestUnknownNumericRunStateValuesNeverPanic pins the other direction of the
// same rule: a value outside every generated enum arrives as a number, and the
// mapping boundary has to answer with the domain unknown rather than crash or
// render the integer.
func TestUnknownNumericRunStateValuesNeverPanic(t *testing.T) {
	state := &turingv1.RunState{
		RunId:        "run_future",
		Lifecycle:    turingv1.RunLifecycle(9999),
		StateVersion: 3,
	}
	encoded, err := proto.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	decoded := &turingv1.RunState{}
	if err := proto.Unmarshal(encoded, decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.GetLifecycle() != turingv1.RunLifecycle(9999) {
		t.Fatalf("lifecycle = %v, want the unrecognized numeric preserved on the wire", decoded.GetLifecycle())
	}
}

// TestEventReplayAndLiveBusCarryIdenticalVersionedRunState is the second half
// of the parity promise: the same durable transition, read from replay and
// received live, is the same versioned state.
//
// The stream is kept contiguous on purpose. SubscribeSessionEvents re-reads the
// database whenever a bus event arrives more than one sequence ahead of what it
// has sent, and that recovery path renders through the replay mapper — so a
// test that lets a gap open is not watching the live mapper at all. Publishing
// every durable row, in order, is what makes this an assertion about the live
// half rather than about the replay half twice.
func TestEventReplayAndLiveBusCarryIdenticalVersionedRunState(t *testing.T) {
	h := newEventHarness(t)
	run := seedEventRun(t, h, "Replay and live")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := turingv1.NewEventServiceClient(h.conn).SubscribeSessionEvents(ctx,
		&turingv1.SubscribeSessionEventsRequest{SessionId: run.sessionID})
	if err != nil {
		t.Fatalf("SubscribeSessionEvents: %v", err)
	}
	// Drain the whole replayed prefix, not just up to a type that happens to be
	// in it: what follows is delivered by the bus only if the server has
	// finished replaying everything already durable.
	seeded := latestSequence(t, h, run.sessionID)
	for {
		event, err := stream.Recv()
		if err != nil {
			t.Fatalf("recv replayed prefix: %v", err)
		}
		if event.GetSequence() >= seeded {
			break
		}
	}

	if err := h.repo.MarkRunRunning(context.Background(), run.runID); err != nil {
		t.Fatalf("MarkRunRunning: %v", err)
	}
	// The start's own projection is published before the completion, so the
	// completion arrives exactly one sequence past what the stream last sent.
	started := publishDurableTail(t, h, run.sessionID, seeded)
	liveStart := recvEventOfType(t, stream, turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED)
	if liveStart.GetRunState().GetLifecycle() != turingv1.RunLifecycle_RUN_LIFECYCLE_RUNNING {
		t.Fatalf("live start state = %+v, want running", liveStart.GetRunState())
	}

	running, err := h.repo.GetRunState(context.Background(), run.runID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	committed, err := h.repo.CompleteRunCanonical(context.Background(), repository.CompleteRunInput{
		RunID:                run.runID,
		AssistantMessageID:   run.assistantMessageID,
		Content:              "done",
		ExpectedStateVersion: running.StateVersion,
	})
	if err != nil {
		t.Fatalf("CompleteRunCanonical: %v", err)
	}
	publishDurableTail(t, h, run.sessionID, started)

	live := recvEventOfType(t, stream, turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_COMPLETED)
	if live.GetRunState() == nil {
		t.Fatal("the live completion carried no run state")
	}
	// The whole projection is compared, not just its version: a live mapper
	// that carried the right version onto the wrong identity, lifecycle or
	// finish time would be exactly the disagreement this promise removes.
	if !proto.Equal(live.GetRunState(), runstate.Project(committed.State)) {
		t.Fatalf("live state %+v is not the committed projection %+v",
			live.GetRunState(), runstate.Project(committed.State))
	}
	if live.GetRunState().GetStateVersion() != committed.State.StateVersion {
		t.Fatalf("live state version = %d, want the committed %d",
			live.GetRunState().GetStateVersion(), committed.State.StateVersion)
	}
	if live.GetRunId() != run.runID || live.GetSessionId() != run.sessionID {
		t.Fatalf("live event identity = %s/%s, want %s/%s",
			live.GetSessionId(), live.GetRunId(), run.sessionID, run.runID)
	}
	if live.GetCreatedAt() == nil {
		t.Fatal("the live completion carried no created_at")
	}

	replayed := listedEvent(t, h, run.sessionID, live.GetEventId())
	if !proto.Equal(live, replayed) {
		t.Fatalf("live event %s and replayed event %s disagree", live, replayed)
	}
}

// TestLiveAndReplayMappersRenderEveryRowIdentically is the mutation guard for
// the live mapper itself.
//
// The stream test above can only observe the live half while the sequence stays
// contiguous, which is a property of how that test publishes rather than of the
// mapper. This one holds the two mappers against the same durable row directly,
// so a change to either one alone — a dropped state, a dropped identity, a
// payload that skipped the allowlist — fails here whatever the stream does.
func TestLiveAndReplayMappersRenderEveryRowIdentically(t *testing.T) {
	h := newEventHarness(t)
	run := seedEventRun(t, h, "Mapper parity")
	if err := h.repo.MarkRunRunning(context.Background(), run.runID); err != nil {
		t.Fatalf("MarkRunRunning: %v", err)
	}
	running, err := h.repo.GetRunState(context.Background(), run.runID)
	if err != nil {
		t.Fatalf("GetRunState: %v", err)
	}
	if _, err := h.repo.CompleteRunCanonical(context.Background(), repository.CompleteRunInput{
		RunID:                run.runID,
		AssistantMessageID:   run.assistantMessageID,
		Content:              "done",
		ExpectedStateVersion: running.StateVersion,
	}); err != nil {
		t.Fatalf("CompleteRunCanonical: %v", err)
	}
	// A hand-edited failure row is included so the comparison covers the arm
	// where the payload is reduced to nothing as well as the arms that keep it.
	appendLegacyEvent(t, h, run, "AGENT_RUN_FAILED",
		`{"code":"model_error","message":"connection refused by ollama at 127.0.0.1:11434","assignmentAttemptId":"attempt_7"}`)

	rows, _, err := h.repo.ReplayEvents(context.Background(), run.sessionID, 0, 500)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("the seeded session produced no durable rows")
	}
	var carriedState int
	for _, row := range rows {
		runID := ""
		if row.RunID.Valid {
			runID = row.RunID.String
		}
		live := mapBusEvent(Event{
			EventID:     row.EventID,
			SessionID:   row.SessionID,
			RunID:       runID,
			TraceID:     row.TraceID,
			Sequence:    row.Sequence,
			Type:        row.Type,
			CreatedAt:   row.CreatedAt,
			PayloadJSON: row.PayloadJSON,
		})
		replayed := mapEvent(row)
		if !proto.Equal(live, replayed) {
			t.Fatalf("row %s (%s): live %s and replayed %s disagree", row.EventID, row.Type, live, replayed)
		}
		if live.GetRunState() != nil {
			carriedState++
		}
	}
	if carriedState == 0 {
		t.Fatal("no seeded row carried a run state, so the comparison proved nothing about it")
	}
}

// latestSequence asks the public replay how far the durable log currently goes.
func latestSequence(t *testing.T, h *eventHarness, sessionID string) int64 {
	t.Helper()
	response, err := turingv1.NewEventServiceClient(h.conn).ListEvents(context.Background(),
		&turingv1.ListEventsRequest{SessionId: sessionID, Limit: 500})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	return response.GetLatestSequence()
}

// publishDurableTail publishes every durable row past a mark onto the bus, in
// sequence order, the way the runtime publishes what it just committed.
func publishDurableTail(t *testing.T, h *eventHarness, sessionID string, after int64) int64 {
	t.Helper()
	rows, latest, err := h.repo.ReplayEvents(context.Background(), sessionID, after, 500)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}
	for _, row := range rows {
		runID := ""
		if row.RunID.Valid {
			runID = row.RunID.String
		}
		h.bus.Publish(Event{
			EventID:     row.EventID,
			SessionID:   row.SessionID,
			RunID:       runID,
			TraceID:     row.TraceID,
			Sequence:    row.Sequence,
			Type:        row.Type,
			CreatedAt:   row.CreatedAt,
			PayloadJSON: row.PayloadJSON,
		})
	}
	return latest
}

func recvEventOfType(t *testing.T, stream turingv1.EventService_SubscribeSessionEventsClient, want turingv1.TuringEventType) *turingv1.TuringEvent {
	t.Helper()
	received := make(chan *turingv1.TuringEvent, 1)
	failed := make(chan error, 1)
	go func() {
		for {
			event, err := stream.Recv()
			if err != nil {
				failed <- err
				return
			}
			if event.GetType() == want {
				received <- event
				return
			}
		}
	}()
	select {
	case event := <-received:
		return event
	case err := <-failed:
		t.Fatalf("recv %v: %v", want, err)
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %v", want)
	}
	return nil
}
