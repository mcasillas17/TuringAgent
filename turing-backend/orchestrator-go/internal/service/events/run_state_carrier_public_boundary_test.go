package events

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

// TestListEventsPublicBoundaryProjectsRunStateOnlyForCarrierTypes is the
// EventService-level counterpart to TestDecodeProjectsTypedRunStateOnlyForCarrierTypes
// in run_state_carrier_test.go.
//
// That test calls Decode directly, so it would keep passing even if a future
// change moved the carrier gate somewhere Decode no longer owns — for example,
// if ListEvents' own mapper (mapEvent) started reading runState off the
// payload itself instead of trusting SafeEvent.RunState, or stopped calling
// Decode on some path entirely. This test drives the identical well-formed,
// self-naming legacy row through the actual public surface a client uses —
// appendLegacyEvent writes the durable row exactly as an unmigrated or
// hand-edited writer would, and listedEvent reads it back through the real
// ListEvents RPC — so a regression in mapEvent, not just in Decode, fails
// here.
//
// It reuses this package's own runStateCarrierEventTypes and
// runStateNonCarrierEventTypes tables (defined in run_state_carrier_test.go)
// rather than a second, hand-maintained list, so the noncarrier coverage here
// can never silently drift from the noncarrier coverage already pinned at the
// Decode level.
func TestListEventsPublicBoundaryProjectsRunStateOnlyForCarrierTypes(t *testing.T) {
	t.Run("carriers still project through ListEvents", func(t *testing.T) {
		for _, eventType := range runStateCarrierEventTypes {
			t.Run(eventType, func(t *testing.T) {
				h := newEventHarness(t)
				run := seedEventRun(t, h, "list carrier "+eventType)
				seeded := appendLegacyEvent(t, h, run, eventType, wellFormedSelfNamingRunState(run.runID))
				public := listedEvent(t, h, run.sessionID, seeded.EventID)
				if public.GetRunState() == nil {
					t.Fatalf("%s: ListEvents dropped the state of a carrier row it should still trust", eventType)
				}
				if public.GetRunState().GetRunId() != run.runID {
					t.Fatalf("%s: ListEvents run id = %q, want %q", eventType, public.GetRunState().GetRunId(), run.runID)
				}
				if public.GetRunState().GetStateVersion() != 999 {
					t.Fatalf("%s: ListEvents state version = %d, want the stored 999", eventType, public.GetRunState().GetStateVersion())
				}
			})
		}
	})

	t.Run("non-carriers never project through ListEvents", func(t *testing.T) {
		for _, eventType := range runStateNonCarrierEventTypes {
			t.Run(eventType, func(t *testing.T) {
				h := newEventHarness(t)
				run := seedEventRun(t, h, "list noncarrier "+eventType)
				seeded := appendLegacyEvent(t, h, run, eventType, wellFormedSelfNamingRunState(run.runID))
				public := listedEvent(t, h, run.sessionID, seeded.EventID)
				if public.GetRunState() != nil {
					t.Fatalf("%s: ListEvents projected a typed RunState %+v from a row its own writer never authored",
						eventType, public.GetRunState())
				}
				if _, republished := public.GetPayload().GetFields()[runStatePayloadKey]; republished {
					t.Fatalf("%s: ListEvents republished the refused snapshot in the payload: %s",
						eventType, public.GetPayload())
				}
			})
		}
	})
}

// awaitLiveBusEvent publishes the same live bus event repeatedly on a short
// ticker until the subscribed client observes it, or a deadline elapses.
//
// liveStreamReader is a single, long-lived reader of one
// SubscribeSessionEvents client stream, started once and shared by every
// awaitLiveBusEvent call in a test.
//
// A naive per-call "spawn a goroutine that calls stream.Recv() once" design is
// unsafe here: this test drives dozens of sequential subtests over the same
// shared stream, and if any one of them ever times out waiting for its event —
// which is exactly the observable symptom of the regression this test exists
// to catch — that call's goroutine is left blocked on Recv() forever. gRPC-go
// does not support more than one goroutine calling Recv concurrently on the
// same ClientStream, so the next subtest's own Recv() call would then race the
// leaked one, and a real regression could manifest as a confusing cascade of
// failures across many later subtests instead of one clean, attributable
// failure. Reading from the stream exactly once, in one goroutine for the
// stream's entire lifetime, and fanning received events out over a channel
// keeps every subtest's wait independent of every other subtest's outcome.
type liveStreamReader struct {
	events chan *turingv1.TuringEvent
	errs   chan error
}

func newLiveStreamReader(stream turingv1.EventService_SubscribeSessionEventsClient) *liveStreamReader {
	reader := &liveStreamReader{
		events: make(chan *turingv1.TuringEvent, 8),
		errs:   make(chan error, 1),
	}
	go func() {
		for {
			got, err := stream.Recv()
			if err != nil {
				reader.errs <- err
				return
			}
			reader.events <- got
		}
	}()
	return reader
}

// awaitLiveBusEvent publishes the same live bus event repeatedly on a short
// ticker until this stream's shared reader observes it, or a deadline
// elapses.
//
// Publishing before SubscribeSessionEvents' server-side goroutine has reached
// its own bus.Subscribe call is a silent no-op (Bus.Publish only fans out to
// subscriptions that already exist), so retrying is how a live event becomes
// observable without reaching into the server's internals to coordinate the
// race directly. TestEventServiceSubscribesToReplayAndBusEvents in
// service_test.go establishes this exact retry pattern; it is factored out
// here, over one shared reader rather than one goroutine per call, so it can
// drive many event types on one stream without risking two goroutines ever
// reading that stream at once.
func (r *liveStreamReader) awaitLiveBusEvent(t *testing.T, h *eventHarness, event Event) *turingv1.TuringEvent {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-r.events:
			return got
		case err := <-r.errs:
			t.Fatalf("Recv live bus event %s (sequence %d): %v", event.Type, event.Sequence, err)
		case <-ticker.C:
			h.bus.Publish(event)
		case <-deadline:
			t.Fatalf("timed out waiting for the live bus event %s (sequence %d)", event.Type, event.Sequence)
		}
	}
}

// TestSubscribeSessionEventsLiveBoundaryProjectsRunStateOnlyForCarrierTypes is
// the streaming half of the same rule, exercised against mapBusEvent rather
// than mapEvent.
//
// mapBusEvent is a second, hand-written construction of the same
// *turingv1.TuringEvent a live watcher receives — it calls the shared Decode
// exactly as mapEvent does, but nothing stops a future edit to just one of the
// two functions (for example, a mapBusEvent that stopped propagating
// safe.RunState, or that read runState off the bus payload directly instead
// of through Decode) from passing every Decode-level and ListEvents-level test
// while still leaking, or dropping, state on the live path. Publishing legacy,
// well-formed, self-naming rows straight onto the Bus and reading them back
// through a real SubscribeSessionEvents client stream is what makes that class
// of regression visible.
func TestSubscribeSessionEventsLiveBoundaryProjectsRunStateOnlyForCarrierTypes(t *testing.T) {
	h := newEventHarness(t)
	client := turingv1.NewEventServiceClient(h.conn)
	run := seedEventRun(t, h, "stream carrier boundary")

	// seedEventRun already durably wrote this run's own queued/session events,
	// so the subscription's own initial replay resends them before anything
	// live does. They have to be drained here, or the first assertion below
	// would silently pass against one of those instead of the event this test
	// actually published.
	_, alreadyPersisted, err := h.repo.ReplayEvents(context.Background(), run.sessionID, 0, 500)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}

	streamCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := client.SubscribeSessionEvents(streamCtx, &turingv1.SubscribeSessionEventsRequest{SessionId: run.sessionID})
	if err != nil {
		t.Fatalf("SubscribeSessionEvents: %v", err)
	}
	for i := int64(0); i < alreadyPersisted; i++ {
		if _, err := stream.Recv(); err != nil {
			t.Fatalf("drain seeded replay event %d: %v", i+1, err)
		}
	}
	// Exactly one goroutine ever calls stream.Recv() from here on, shared by
	// every awaitLiveBusEvent call below — see liveStreamReader's own doc for
	// why a reader per call is not safe over this many sequential subtests.
	reader := newLiveStreamReader(stream)

	sequence := alreadyPersisted
	nextSequence := func() int64 {
		sequence++
		return sequence
	}

	liveBusEvent := func(eventType string, sequence int64) Event {
		return Event{
			EventID: "evt_stream_" + eventType, SessionID: run.sessionID, RunID: run.runID, TraceID: "trace_stream",
			Sequence: sequence, Type: eventType, CreatedAt: "2026-08-20T00:00:00Z",
			PayloadJSON: wellFormedSelfNamingRunState(run.runID),
		}
	}

	t.Run("carriers still project over the live stream", func(t *testing.T) {
		for _, eventType := range runStateCarrierEventTypes {
			t.Run(eventType, func(t *testing.T) {
				got := reader.awaitLiveBusEvent(t, h, liveBusEvent(eventType, nextSequence()))
				if got.GetRunState() == nil {
					t.Fatalf("%s: the live stream dropped the state of a carrier row it should still trust", eventType)
				}
				if got.GetRunState().GetStateVersion() != 999 {
					t.Fatalf("%s: live state version = %d, want the stored 999", eventType, got.GetRunState().GetStateVersion())
				}
			})
		}
	})

	assertNoLiveRunState := func(t *testing.T, eventType string, got *turingv1.TuringEvent) {
		t.Helper()
		if got.GetRunState() != nil {
			t.Fatalf("%s: the live stream projected a typed RunState %+v from a row its own writer never authored",
				eventType, got.GetRunState())
		}
		if _, republished := got.GetPayload().GetFields()[runStatePayloadKey]; republished {
			t.Fatalf("%s: the live stream republished the refused snapshot in the payload: %s",
				eventType, got.GetPayload())
		}
	}

	t.Run("non-carriers never project over the live stream", func(t *testing.T) {
		for _, eventType := range runStateNonCarrierEventTypes {
			if eventType == "session.deleted" {
				// SubscribeSessionEvents treats session.deleted as a
				// stream-terminating sentinel (it sends the event and then
				// ends the RPC with NotFound) entirely independent of the
				// runState carrier gate this test covers. That is a real rule
				// worth pinning, but it has to be this shared stream's last
				// event rather than one of many, so it is handled below
				// instead of inside this loop.
				continue
			}
			t.Run(eventType, func(t *testing.T) {
				got := reader.awaitLiveBusEvent(t, h, liveBusEvent(eventType, nextSequence()))
				assertNoLiveRunState(t, eventType, got)
			})
		}
		t.Run("session.deleted", func(t *testing.T) {
			got := reader.awaitLiveBusEvent(t, h, liveBusEvent("session.deleted", nextSequence()))
			assertNoLiveRunState(t, "session.deleted", got)
			// The server closes the RPC right after sending this one, so this
			// has to be verified last: nothing published afterward on this
			// stream could ever be observed. The shared reader is still the
			// only goroutine allowed to call stream.Recv(), so this reads the
			// close off its error channel rather than calling Recv() again
			// directly here.
			select {
			case got := <-reader.events:
				t.Fatalf("stream stayed open after session.deleted and delivered another event: %+v", got)
			case err := <-reader.errs:
				if err == nil {
					t.Fatal("stream stayed open after session.deleted, want the server to end it with an error")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("timed out waiting for the stream to close after session.deleted")
			}
		})
	})
}
