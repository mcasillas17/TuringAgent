package events

import (
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

	var terminal Event
	for {
		select {
		case event := <-ch:
			if event.Type == "agent.run.completed" {
				terminal = event
			}
		default:
			if terminal.Sequence != 140 {
				t.Fatalf("terminal notification = %+v, want retained sequence 140 after overflow", terminal)
			}
			return
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
