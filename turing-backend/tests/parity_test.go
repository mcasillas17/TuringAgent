package tests

import (
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestParityForSessionMessageEventShapes(t *testing.T) {
	harness := newGRPCHarness(t)
	defer harness.close()

	sessionID := harness.createSession(t, "parity")
	chatEvents := harness.sendMessageToCompletion(t, sessionID, "hello")
	listed, err := harness.events.ListEvents(harness.clientContext(), &turingv1.ListEventsRequest{SessionId: sessionID, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}

	chatDeltas := chatTokenDeltas(chatEvents)
	eventDeltas := messageDeltaPayloads(listed.Events)
	if len(chatDeltas) != len(eventDeltas) {
		t.Fatalf("delta count mismatch: chat=%d events=%d", len(chatDeltas), len(eventDeltas))
	}
	for i := range chatDeltas {
		if chatDeltas[i].Sequence != eventDeltas[i].Sequence || chatDeltas[i].RunID != eventDeltas[i].RunID || chatDeltas[i].Delta != eventDeltas[i].Delta {
			t.Fatalf("delta[%d] mismatch: chat=%+v events=%+v", i, chatDeltas[i], eventDeltas[i])
		}
	}
	if got, want := messageCompletedContent(t, chatEvents), messageCompletedPayload(listed.Events); got != want || got != "Hello" {
		t.Fatalf("message.completed parity got chat=%q events=%q, want Hello", got, want)
	}
}

type deltaShape struct {
	Sequence int64
	RunID    string
	Delta    string
}

func assertTokenDeltas(t *testing.T, events []*turingv1.ChatStreamEvent, want []string) {
	t.Helper()
	got := make([]string, 0, len(want))
	for _, event := range events {
		if delta := event.GetTokenDelta(); delta != nil {
			got = append(got, delta.Delta)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("token deltas = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("token deltas = %#v, want %#v", got, want)
		}
	}
}

func chatTokenDeltas(events []*turingv1.ChatStreamEvent) []deltaShape {
	var out []deltaShape
	for _, event := range events {
		if delta := event.GetTokenDelta(); delta != nil {
			out = append(out, deltaShape{Sequence: event.Sequence, RunID: event.RunId, Delta: delta.Delta})
		}
	}
	return out
}

func messageDeltaPayloads(events []*turingv1.TuringEvent) []deltaShape {
	var out []deltaShape
	for _, event := range events {
		if event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA {
			out = append(out, deltaShape{Sequence: event.Sequence, RunID: event.RunId, Delta: stringField(event.Payload, "delta")})
		}
	}
	return out
}

func messageCompletedContent(t *testing.T, events []*turingv1.ChatStreamEvent) string {
	t.Helper()
	var completedMessages []*turingv1.MessageCompleted
	for _, event := range events {
		if completed := event.GetMessageCompleted(); completed != nil {
			completedMessages = append(completedMessages, completed)
		}
	}
	if len(completedMessages) != 1 {
		t.Fatalf("stream message.completed count = %d, want 1", len(completedMessages))
	}
	return completedMessages[0].Content
}

func messageCompletedPayload(events []*turingv1.TuringEvent) string {
	for _, event := range events {
		if event.Type == turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_COMPLETED {
			return stringField(event.Payload, "content")
		}
	}
	return ""
}

func hasRunCompleted(events []*turingv1.ChatStreamEvent) bool {
	for _, event := range events {
		if event.GetRunCompleted() != nil {
			return true
		}
	}
	return false
}

func assertPersistedTypes(t *testing.T, events []*turingv1.ChatStreamEvent, want ...turingv1.TuringEventType) {
	t.Helper()
	remaining := append([]turingv1.TuringEventType(nil), want...)
	for _, event := range events {
		persisted := event.GetPersistedEvent()
		if persisted == nil {
			continue
		}
		for i, eventType := range remaining {
			if persisted.Type == eventType {
				remaining = append(remaining[:i], remaining[i+1:]...)
				break
			}
		}
	}
	if len(remaining) > 0 {
		t.Fatalf("missing persisted event types: %v", remaining)
	}
}

func eventsAfter(events []*turingv1.TuringEvent, sequence int64) []*turingv1.TuringEvent {
	var out []*turingv1.TuringEvent
	for _, event := range events {
		if event.Sequence > sequence {
			out = append(out, event)
		}
	}
	return out
}

func assertSameEventSequenceAndTypes(t *testing.T, got []*turingv1.TuringEvent, want []*turingv1.TuringEvent) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d replayed events, want %d", len(got), len(want))
	}
	for i := range got {
		if got[i].Sequence != want[i].Sequence || got[i].Type != want[i].Type || got[i].RunId != want[i].RunId {
			t.Fatalf("event[%d] = (seq=%d type=%s run=%s), want (seq=%d type=%s run=%s)", i, got[i].Sequence, got[i].Type, got[i].RunId, want[i].Sequence, want[i].Type, want[i].RunId)
		}
	}
}

func stringField(payload *structpb.Struct, name string) string {
	if payload == nil || payload.Fields == nil || payload.Fields[name] == nil {
		return ""
	}
	return payload.Fields[name].GetStringValue()
}
