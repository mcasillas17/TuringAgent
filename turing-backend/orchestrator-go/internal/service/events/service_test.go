package events

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

type eventHarness struct {
	repo *repository.Repository
	bus  *Bus
	conn *grpc.ClientConn
}

func newEventHarness(t *testing.T) *eventHarness {
	t.Helper()
	database := openEventTestDB(t)
	repo := repository.New(database)
	bus := NewBus(8)
	lis := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	turingv1.RegisterEventServiceServer(grpcServer, NewServer(repo, bus))
	go func() {
		_ = grpcServer.Serve(lis)
	}()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = conn.Close()
	})
	return &eventHarness{repo: repo, bus: bus, conn: conn}
}

func openEventTestDB(t *testing.T) *db.DB {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", ":", "_").Replace(t.Name())
	sqlDB, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=memory&cache=shared&_foreign_keys=on", name))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	database := &db.DB{DB: sqlDB}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return database
}

func TestEventServiceListsPersistedEvents(t *testing.T) {
	h := newEventHarness(t)
	client := turingv1.NewEventServiceClient(h.conn)
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "Events")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.AppendEvent(ctx, repository.AppendEventInput{SessionID: session.SessionID, TraceID: "trace_1", Type: "system", PayloadJSON: `{"a":1}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.AppendEvent(ctx, repository.AppendEventInput{SessionID: session.SessionID, TraceID: "trace_1", Type: "agent.run.queued", PayloadJSON: `{"status":"queued"}`}); err != nil {
		t.Fatal(err)
	}

	resp, err := client.ListEvents(ctx, &turingv1.ListEventsRequest{SessionId: session.SessionID, AfterSequence: 1, Limit: 10})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if resp.LatestSequence != 2 || len(resp.Events) != 1 {
		t.Fatalf("latest=%d events=%+v", resp.LatestSequence, resp.Events)
	}
	got := resp.Events[0]
	if got.Sequence != 2 || got.Type != turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_QUEUED {
		t.Fatalf("bad event: %+v", got)
	}
	if got.Payload.GetFields()["status"].GetStringValue() != "queued" {
		t.Fatalf("payload = %+v", got.Payload)
	}
}

func TestEventServiceListEventsRequiresResyncWhenClientSequenceIsAhead(t *testing.T) {
	h := newEventHarness(t)
	client := turingv1.NewEventServiceClient(h.conn)
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "Resync")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.AppendEvent(ctx, repository.AppendEventInput{SessionID: session.SessionID, TraceID: "trace_1", Type: "system", PayloadJSON: `{}`}); err != nil {
		t.Fatal(err)
	}

	resp, err := client.ListEvents(ctx, &turingv1.ListEventsRequest{SessionId: session.SessionID, AfterSequence: 5, Limit: 10})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if !resp.ResyncRequired {
		t.Fatal("resync_required = false, want true")
	}
}

func TestEventServiceSubscribesToReplayAndBusEvents(t *testing.T) {
	h := newEventHarness(t)
	client := turingv1.NewEventServiceClient(h.conn)
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "Stream")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.AppendEvent(ctx, repository.AppendEventInput{SessionID: session.SessionID, TraceID: "trace_1", Type: "system", PayloadJSON: `{"ready":true}`}); err != nil {
		t.Fatal(err)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := client.SubscribeSessionEvents(streamCtx, &turingv1.SubscribeSessionEventsRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatalf("SubscribeSessionEvents: %v", err)
	}
	replayed, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv replayed: %v", err)
	}
	if replayed.Sequence != 1 || replayed.Type != turingv1.TuringEventType_TURING_EVENT_TYPE_SYSTEM {
		t.Fatalf("bad replayed event: %+v", replayed)
	}

	received := make(chan *turingv1.TuringEvent, 1)
	errs := make(chan error, 1)
	go func() {
		got, err := stream.Recv()
		if err != nil {
			errs <- err
			return
		}
		received <- got
	}()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(time.Second)
	for {
		select {
		case got := <-received:
			if got.Sequence != 2 || got.Type != turingv1.TuringEventType_TURING_EVENT_TYPE_MESSAGE_DELTA {
				t.Fatalf("bad bus event: %+v", got)
			}
			if got.Payload.GetFields()["delta"].GetStringValue() != "hi" {
				t.Fatalf("payload = %+v", got.Payload)
			}
			if got.EventId != "evt_live_2" || got.CreatedAt == nil {
				t.Fatalf("live metadata missing: %+v", got)
			}
			return
		case err := <-errs:
			t.Fatalf("Recv bus event: %v", err)
		case <-ticker.C:
			h.bus.Publish(Event{EventID: "evt_live_2", SessionID: session.SessionID, TraceID: "trace_1", Sequence: 2, Type: "message.delta", CreatedAt: "2026-05-15T00:00:00Z", PayloadJSON: `{"delta":"hi"}`})
		case <-deadline:
			t.Fatal("timed out waiting for bus event")
		}
	}
}

func TestEventServiceCatchesEventsPublishedBetweenReplayAndSubscribe(t *testing.T) {
	h := newEventHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	session, err := h.repo.CreateSession(ctx, "Gap")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.AppendEvent(ctx, repository.AppendEventInput{SessionID: session.SessionID, TraceID: "trace_1", Type: "system", PayloadJSON: `{"ready":true}`}); err != nil {
		t.Fatal(err)
	}

	stream := &fakeEventStream{ctx: ctx}
	stream.onSend = func(event *turingv1.TuringEvent) {
		if event.Sequence != 1 {
			return
		}
		gapEvent, err := h.repo.AppendEvent(ctx, repository.AppendEventInput{SessionID: session.SessionID, TraceID: "trace_1", Type: "message.delta", PayloadJSON: `{"delta":"gap"}`})
		if err != nil {
			t.Errorf("append gap event: %v", err)
			cancel()
			return
		}
		h.bus.Publish(Event{SessionID: session.SessionID, TraceID: gapEvent.TraceID, Sequence: gapEvent.Sequence, Type: gapEvent.Type, PayloadJSON: gapEvent.PayloadJSON})
	}
	stream.afterSend = func(event *turingv1.TuringEvent) {
		if event.Sequence == 2 {
			cancel()
		}
	}

	err = NewServer(h.repo, h.bus).SubscribeSessionEvents(&turingv1.SubscribeSessionEventsRequest{SessionId: session.SessionID}, stream)
	if status.Code(err) != codes.Canceled {
		t.Fatalf("SubscribeSessionEvents error = %v, want canceled", err)
	}
	if len(stream.sent) < 2 {
		t.Fatalf("sent %d events, want gap catch-up event: %+v", len(stream.sent), stream.sent)
	}
	if stream.sent[1].Sequence != 2 || stream.sent[1].Payload.GetFields()["delta"].GetStringValue() != "gap" {
		t.Fatalf("bad gap catch-up event: %+v", stream.sent[1])
	}
}

type fakeEventStream struct {
	ctx       context.Context
	sent      []*turingv1.TuringEvent
	onSend    func(*turingv1.TuringEvent)
	afterSend func(*turingv1.TuringEvent)
}

func (s *fakeEventStream) Send(event *turingv1.TuringEvent) error {
	if s.onSend != nil {
		s.onSend(event)
	}
	s.sent = append(s.sent, event)
	if s.afterSend != nil {
		s.afterSend(event)
	}
	return nil
}

func (s *fakeEventStream) SetHeader(metadata.MD) error { return nil }

func (s *fakeEventStream) SendHeader(metadata.MD) error { return nil }

func (s *fakeEventStream) SetTrailer(metadata.MD) {}

func (s *fakeEventStream) Context() context.Context { return s.ctx }

func (s *fakeEventStream) SendMsg(any) error { return nil }

func (s *fakeEventStream) RecvMsg(any) error { return nil }
