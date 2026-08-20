package events

import (
	"fmt"
	"testing"
	"time"
)

func TestBusPublishesOnlyMatchingSessionAndUnsubscribes(t *testing.T) {
	bus := NewBus(8)
	ch, unsubscribe := bus.Subscribe("sess_1")
	bus.Publish(Event{SessionID: "sess_2", Sequence: 1})
	select {
	case got := <-ch:
		t.Fatalf("unexpected event: %+v", got)
	default:
	}
	bus.Publish(Event{SessionID: "sess_1", Sequence: 2})
	select {
	case got := <-ch:
		if got.Sequence != 2 {
			t.Fatalf("sequence = %d", got.Sequence)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
	unsubscribe()
	bus.Publish(Event{SessionID: "sess_1", Sequence: 3})
	select {
	case got, ok := <-ch:
		if ok {
			t.Fatalf("received after unsubscribe: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("channel did not close")
	}
}

func TestBusPublishesEverySessionToGlobalSubscriber(t *testing.T) {
	bus := NewBus(8)
	ch, unsubscribe := bus.SubscribeSessionUpdates()
	defer unsubscribe()

	bus.Publish(Event{SessionID: "sess_1", Sequence: 1, Type: "session.updated"})
	bus.Publish(Event{SessionID: "sess_2", Sequence: 1, Type: "session.updated"})

	received := map[string]bool{}
	for len(received) < 2 {
		select {
		case event := <-ch:
			received[event.SessionID] = true
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for both sessions: %+v", received)
		}
	}
}

func TestBusSessionUpdateOverflowRetainsLatestUpdatePerSession(t *testing.T) {
	bus := NewBus(1)
	ch, unsubscribe := bus.SubscribeSessionUpdates()
	defer unsubscribe()

	bus.Publish(Event{SessionID: "sess_1", Sequence: 1, Type: "session.updated"})
	bus.Publish(Event{SessionID: "sess_2", Sequence: 1, Type: "message.delta"})
	bus.Publish(Event{SessionID: "sess_2", Sequence: 2, Type: "session.updated"})
	bus.Publish(Event{SessionID: "sess_1", Sequence: 3, Type: "session.updated"})

	latest := map[string]int64{}
	deadline := time.After(time.Second)
	for latest["sess_1"] != 3 || latest["sess_2"] != 2 {
		select {
		case event := <-ch:
			latest[event.SessionID] = event.Sequence
		case <-deadline:
			t.Fatalf("latest updates = %+v, want sess_1=3 and sess_2=2", latest)
		}
	}
}

func TestBusOverflowRetainsLatestTerminalNotification(t *testing.T) {
	bus := NewBus(128)
	ch, unsubscribe := bus.Subscribe("sess_slow")
	defer unsubscribe()

	for sequence := int64(1); sequence <= 140; sequence++ {
		eventType := "message.delta"
		if sequence == 140 {
			eventType = "agent.run.completed"
		}
		bus.Publish(Event{SessionID: "sess_slow", Sequence: sequence, Type: eventType})
	}

	deadline := time.After(time.Second)
	for {
		select {
		case event := <-ch:
			if event.Type == "agent.run.completed" {
				if event.Sequence != 140 {
					t.Fatalf("terminal notification = %+v, want sequence 140", event)
				}
				return
			}
		case <-deadline:
			t.Fatal("timed out waiting for retained terminal notification")
		}
	}
}

func TestBusOverflowDoesNotLetDelayedEventEvictNewerTerminal(t *testing.T) {
	bus := NewBus(1)
	ch, unsubscribe := bus.Subscribe("sess_out_of_order")
	defer unsubscribe()

	bus.Publish(Event{SessionID: "sess_out_of_order", Sequence: 3, Type: "agent.run.completed"})
	bus.Publish(Event{SessionID: "sess_out_of_order", Sequence: 2, Type: "message.delta"})

	select {
	case event := <-ch:
		if event.Sequence != 3 || event.Type != "agent.run.completed" {
			t.Fatalf("overflow retained %+v, want newer terminal sequence 3", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for retained terminal notification")
	}
}

func TestBusTerminatesSessionThroughDedicatedTerminalPath(t *testing.T) {
	bus := NewBus(1)
	ch, unsubscribe := bus.Subscribe("sess_deleted")
	defer unsubscribe()

	bus.Publish(Event{SessionID: "sess_deleted", Sequence: 1, Type: "message.delta"})
	bus.Publish(Event{SessionID: "sess_deleted", Sequence: 2, Type: "message.delta"})
	bus.TerminateSession(Event{SessionID: "sess_deleted", Sequence: 3, Type: "session.deleted"})
	bus.Publish(Event{SessionID: "sess_deleted", Sequence: 4, Type: "message.delta"})

	var terminalCount int
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				if terminalCount != 1 {
					t.Fatalf("terminal event count = %d, want exactly one", terminalCount)
				}
				return
			}
			if event.Sequence == 4 {
				t.Fatalf("received event after terminal fence: %+v", event)
			}
			if event.Type == "session.deleted" {
				terminalCount++
			}
		case <-time.After(time.Second):
			t.Fatal("terminal subscription did not close")
		}
	}
}

func TestBusPublishesTerminalDeletionToGlobalSubscribers(t *testing.T) {
	bus := NewBus(1)
	updates, unsubscribe := bus.SubscribeSessionUpdates()
	defer unsubscribe()

	bus.TerminateSession(Event{
		SessionID: "sess_deleted",
		Sequence:  1,
		Type:      "session.deleted",
	})

	select {
	case event := <-updates:
		if event.SessionID != "sess_deleted" || event.Type != "session.deleted" {
			t.Fatalf("global terminal event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("global subscriber did not receive terminal deletion")
	}
}

func TestBusBoundsTerminalSessionFences(t *testing.T) {
	bus := NewBus(1)
	for index := 0; index <= maxTerminatedSessionFences; index++ {
		bus.TerminateSession(Event{SessionID: fmt.Sprintf("sess_%d", index), Type: "session.deleted"})
	}
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if got := len(bus.terminatedSessions); got != maxTerminatedSessionFences {
		t.Fatalf("terminal session fences = %d, want %d", got, maxTerminatedSessionFences)
	}
}
