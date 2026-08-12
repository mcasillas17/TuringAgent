package events

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSubscribeSessionEventsRejectsFutureCursorAndDoesNotDropAppendedSequences(t *testing.T) {
	h := newEventHarness(t)
	client := turingv1.NewEventServiceClient(h.conn)
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "Future cursor")
	if err != nil {
		t.Fatal(err)
	}
	first, err := h.repo.AppendEvent(ctx, repository.AppendEventInput{SessionID: session.SessionID, TraceID: "trace_future", Type: "system", PayloadJSON: `{"sequence":1}`})
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 1 {
		t.Fatalf("first sequence = %d, want 1", first.Sequence)
	}
	futureCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	future, err := client.SubscribeSessionEvents(futureCtx, &turingv1.SubscribeSessionEventsRequest{SessionId: session.SessionID, AfterSequence: 5})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := future.Recv(); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("future cursor Recv error = %v, want FailedPrecondition resync", err)
	}

	for sequence := 2; sequence <= 3; sequence++ {
		if _, err := h.repo.AppendEvent(ctx, repository.AppendEventInput{
			SessionID: session.SessionID, TraceID: "trace_future", Type: "message.delta", PayloadJSON: `{"sequence":` + string(rune('0'+sequence)) + `}`,
		}); err != nil {
			t.Fatal(err)
		}
	}
	resyncCtx, cancelResync := context.WithTimeout(ctx, time.Second)
	defer cancelResync()
	resync, err := client.SubscribeSessionEvents(resyncCtx, &turingv1.SubscribeSessionEventsRequest{SessionId: session.SessionID, AfterSequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	for want := int64(2); want <= 3; want++ {
		event, err := resync.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if event.Sequence != want {
			t.Fatalf("replayed sequence = %d, want %d", event.Sequence, want)
		}
	}
}
