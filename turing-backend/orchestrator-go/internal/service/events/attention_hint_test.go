package events

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func nextUpdate(t *testing.T, updates <-chan Event) Event {
	t.Helper()
	select {
	case event := <-updates:
		return event
	case <-time.After(time.Second):
		t.Fatal("no update arrived")
		return Event{}
	}
}

func assertNoUpdate(t *testing.T, updates <-chan Event) {
	t.Helper()
	select {
	case event := <-updates:
		t.Fatalf("unexpected update %+v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

// Every source transition the Needs-you queue lists reaches the global
// updates stream as one content-free attention.changed hint: approvals,
// consents (delegation events), automation outcomes (run terminals) and
// deletions. Nothing else does.
func TestBusHintsAttentionForEverySourceTransition(t *testing.T) {
	for _, eventType := range []string{
		"approval.requested", "approval.approved", "approval.denied", "approval.expired", "approval.consumed",
		"delegation.started", "delegation.updated", "delegation.finished",
		"agent.run.completed", "agent.run.failed", "agent.run.cancelled",
	} {
		bus := NewBus(8)
		updates, unsubscribe := bus.SubscribeSessionUpdates()
		bus.Publish(Event{EventID: "evt_1", SessionID: "sess_secret", RunID: "run_secret", Sequence: 4,
			Type: eventType, PayloadJSON: `{"toolName":"SENTINEL"}`, CreatedAt: "2026-11-19T00:00:00.000000000Z"})
		if hint := nextUpdate(t, updates); hint != (Event{Type: AttentionChangedType}) {
			t.Fatalf("%s: update = %+v, want a bare attention.changed hint", eventType, hint)
		}
		assertNoUpdate(t, updates)
		unsubscribe()
	}
	for _, eventType := range []string{"message.delta", "agent.run.started", "agent.run.step", "tool.call.started", "session.updated"} {
		bus := NewBus(8)
		updates, unsubscribe := bus.SubscribeSessionUpdates()
		bus.Publish(Event{SessionID: "sess_1", Sequence: 1, Type: eventType})
		for {
			select {
			case event := <-updates:
				if event.Type == AttentionChangedType {
					t.Fatalf("%s hinted attention", eventType)
				}
				continue
			case <-time.After(50 * time.Millisecond):
			}
			break
		}
		unsubscribe()
	}
}

// A deletion's terminal event reaches the updates stream first, then a hint.
func TestBusHintsAttentionAfterATerminalDeletion(t *testing.T) {
	bus := NewBus(1)
	updates, unsubscribe := bus.SubscribeSessionUpdates()
	defer unsubscribe()
	bus.TerminateSession(Event{SessionID: "sess_deleted", Sequence: 1, Type: "session.deleted"})
	if first := nextUpdate(t, updates); first.Type != "session.deleted" {
		t.Fatalf("first update = %+v, want session.deleted", first)
	}
	if hint := nextUpdate(t, updates); hint.Type != AttentionChangedType {
		t.Fatalf("second update = %+v, want attention.changed", hint)
	}
}

// Hints coalesce: at most one waits per subscriber, so a burst of changes a
// slow reader has not caught up with arrives as one or two hints, never one
// per change.
func TestBusCoalescesAttentionHints(t *testing.T) {
	bus := NewBus(1)
	updates, unsubscribe := bus.SubscribeSessionUpdates()
	defer unsubscribe()
	for range 50 {
		bus.PublishAttentionChanged()
		bus.Publish(Event{SessionID: "sess_1", Sequence: 1, Type: "approval.requested"})
	}
	hints := 0
	for {
		select {
		case event := <-updates:
			if event.Type != AttentionChangedType {
				t.Fatalf("update = %+v, want only hints", event)
			}
			hints++
			continue
		case <-time.After(100 * time.Millisecond):
		}
		break
	}
	if hints < 1 || hints > 2 {
		t.Fatalf("hints for a burst of 100 changes = %d, want one or two", hints)
	}
}

// A deleted session's late events are fenced, hints included.
func TestBusFencesAttentionHintsFromATerminatedSession(t *testing.T) {
	bus := NewBus(1)
	bus.TerminateSession(Event{SessionID: "sess_gone", Sequence: 1, Type: "session.deleted"})
	updates, unsubscribe := bus.SubscribeSessionUpdates()
	defer unsubscribe()
	bus.Publish(Event{SessionID: "sess_gone", Sequence: 2, Type: "approval.expired"})
	assertNoUpdate(t, updates)
}

// The public stream forwards the hint as ATTENTION_CHANGED with nothing in
// it: no session, run, sequence, time or payload.
func TestSubscribeSessionUpdatesForwardsAContentFreeHint(t *testing.T) {
	h := newEventHarness(t)
	streamCtx, cancel := context.WithCancel(context.Background())
	stream := &fakeEventStream{ctx: streamCtx}
	stream.afterSend = func(*turingv1.TuringEvent) { cancel() }
	done := make(chan error, 1)
	go func() {
		done <- NewServer(h.repo, h.bus).SubscribeSessionUpdates(&turingv1.SubscribeSessionUpdatesRequest{}, stream)
	}()
	// The stream subscribes before it reads its snapshots, so keep publishing
	// until one hint has gone out.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
				h.bus.Publish(Event{EventID: "evt_1", SessionID: "sess_secret", RunID: "run_secret", Sequence: 9,
					Type: "approval.requested", PayloadJSON: `{"toolName":"SENTINEL"}`, CreatedAt: "2026-11-19T00:00:00.000000000Z"})
			}
		}
	}()
	if err := <-done; status.Code(err) != codes.Canceled {
		t.Fatalf("SubscribeSessionUpdates error = %v, want Canceled", err)
	}
	if len(stream.sent) == 0 {
		t.Fatal("no hint was sent")
	}
	for _, hint := range stream.sent {
		assertBareHint(t, hint)
	}
}

func assertBareHint(t *testing.T, hint *turingv1.TuringEvent) {
	t.Helper()
	if hint.GetType() != turingv1.TuringEventType_TURING_EVENT_TYPE_ATTENTION_CHANGED || hint.GetEventId() != "" ||
		hint.GetSessionId() != "" || hint.GetRunId() != "" || hint.GetTraceId() != "" || hint.GetSequence() != 0 ||
		hint.GetCreatedAt() != nil || len(hint.GetPayload().GetFields()) != 0 || hint.GetRunState() != nil {
		t.Fatalf("hint = %+v, want a bare ATTENTION_CHANGED", hint)
	}
}
