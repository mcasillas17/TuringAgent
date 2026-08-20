package events

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"sync"
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
	// NewClient starts the channel IDLE; DialContext connected eagerly in the
	// background. Connect() restores that so handshake latency does not land
	// inside a test's deadline.
	conn.Connect()
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

func TestEventServiceMapsSessionUpdated(t *testing.T) {
	h := newEventHarness(t)
	client := turingv1.NewEventServiceClient(h.conn)
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.AppendEvent(ctx, repository.AppendEventInput{
		SessionID:   session.SessionID,
		TraceID:     "trace_1",
		Type:        "session.updated",
		PayloadJSON: `{"title":"First turn","updatedAt":"2026-08-18T20:00:00Z"}`,
	}); err != nil {
		t.Fatal(err)
	}

	resp, err := client.ListEvents(ctx, &turingv1.ListEventsRequest{
		SessionId: session.SessionID,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(resp.Events) != 1 {
		t.Fatalf("events = %+v, want one session update", resp.Events)
	}
	got := resp.Events[0]
	if got.Type != turingv1.TuringEventType_TURING_EVENT_TYPE_SESSION_UPDATED {
		t.Fatalf("event type = %v, want SESSION_UPDATED", got.Type)
	}
	if got.Payload.GetFields()["title"].GetStringValue() != "First turn" {
		t.Fatalf("payload = %+v", got.Payload)
	}
	if got.Payload.GetFields()["updatedAt"].GetStringValue() != "2026-08-18T20:00:00Z" {
		t.Fatalf("payload = %+v", got.Payload)
	}
}

func TestEventServiceMapsSessionDeleted(t *testing.T) {
	if got := mapEventType("session.deleted"); got != turingv1.TuringEventType(22) {
		t.Fatalf("session.deleted type = %v, want event enum value 22", got)
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

func TestEventServiceSubscribesToAllSessionUpdates(t *testing.T) {
	h := newEventHarness(t)
	ctx := context.Background()
	for _, title := range []string{"First", "Second"} {
		session, err := h.repo.CreateSession(ctx, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
			SessionID:     session.SessionID,
			Content:       title,
			AgentID:       "general_assistant",
			ModelProvider: "ollama",
			Model:         "qwen2.5:7b",
		}); err != nil {
			t.Fatal(err)
		}
	}

	streamCtx, cancel := context.WithCancel(ctx)
	stream := &fakeEventStream{ctx: streamCtx}
	stream.afterSend = func(event *turingv1.TuringEvent) {
		switch len(stream.sent) {
		case 2:
			live, err := h.repo.AppendEvent(ctx, repository.AppendEventInput{
				SessionID:   event.SessionId,
				TraceID:     "trace_live",
				Type:        "session.updated",
				PayloadJSON: `{"title":"Live","updatedAt":"2026-08-18T20:00:00Z"}`,
			})
			if err != nil {
				t.Errorf("append live update: %v", err)
				cancel()
				return
			}
			h.bus.Publish(Event{
				EventID:     live.EventID,
				SessionID:   live.SessionID,
				TraceID:     live.TraceID,
				Sequence:    live.Sequence,
				Type:        live.Type,
				CreatedAt:   live.CreatedAt,
				PayloadJSON: live.PayloadJSON,
			})
		case 3:
			cancel()
		}
	}

	err := NewServer(h.repo, h.bus).SubscribeSessionUpdates(
		&turingv1.SubscribeSessionUpdatesRequest{},
		stream,
	)
	if status.Code(err) != codes.Canceled {
		t.Fatalf("SubscribeSessionUpdates error = %v, want Canceled", err)
	}
	if len(stream.sent) != 3 {
		t.Fatalf("sent %d events, want two snapshots and one live update", len(stream.sent))
	}
	for _, event := range stream.sent {
		if event.Type != turingv1.TuringEventType_TURING_EVENT_TYPE_SESSION_UPDATED {
			t.Fatalf("event type = %v, want SESSION_UPDATED", event.Type)
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

func TestEventServiceDeliversSessionDeletedThenClosesNotFound(t *testing.T) {
	h := newEventHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	session, err := h.repo.CreateSession(ctx, "Terminal deletion")
	if err != nil {
		t.Fatal(err)
	}
	stream := &fakeEventStream{ctx: ctx}
	done := make(chan error, 1)
	go func() {
		done <- NewServer(h.repo, h.bus).SubscribeSessionEvents(
			&turingv1.SubscribeSessionEventsRequest{SessionId: session.SessionID},
			stream,
		)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		h.bus.mu.Lock()
		subscribers := len(h.bus.subs)
		h.bus.mu.Unlock()
		if subscribers == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("event service did not subscribe before terminal deletion")
		}
		time.Sleep(time.Millisecond)
	}
	h.bus.TerminateSession(Event{
		EventID:   "evt_deleted",
		SessionID: session.SessionID,
		Sequence:  1,
		Type:      "session.deleted",
	})

	select {
	case err := <-done:
		if status.Code(err) != codes.NotFound {
			t.Fatalf("terminal stream error = %v, want NotFound", err)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal stream did not close")
	}
	if len(stream.sent) != 1 {
		t.Fatalf("terminal stream sent %d events, want 1", len(stream.sent))
	}
	if got := stream.sent[0].GetType(); got != turingv1.TuringEventType_TURING_EVENT_TYPE_SESSION_DELETED {
		t.Fatalf("terminal event type = %v, want SESSION_DELETED", got)
	}
}

func TestEventServiceRejectsDeletingSessionReplayAndSubscription(t *testing.T) {
	h := newEventHarness(t)
	client := turingv1.NewEventServiceClient(h.conn)
	ctx := context.Background()
	session, err := h.repo.CreateSession(ctx, "Deleting events")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.BeginSessionDeletion(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}

	if _, err := client.ListEvents(ctx, &turingv1.ListEventsRequest{SessionId: session.SessionID}); status.Code(err) != codes.NotFound {
		t.Fatalf("ListEvents error = %v, want NotFound", err)
	}
	stream, err := client.SubscribeSessionEvents(ctx, &turingv1.SubscribeSessionEventsRequest{SessionId: session.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); status.Code(err) != codes.NotFound {
		t.Fatalf("SubscribeSessionEvents error = %v, want NotFound", err)
	}
}

func TestEventServiceDeliversTerminalDeletionAcrossWithdrawnReplayGap(t *testing.T) {
	h := newEventHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	session, err := h.repo.CreateSession(ctx, "Terminal replay gap")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.AppendEvent(ctx, repository.AppendEventInput{
		SessionID: session.SessionID, TraceID: "trace_gap", Type: "system", PayloadJSON: `{}`,
	}); err != nil {
		t.Fatal(err)
	}
	stream := &fakeEventStream{ctx: ctx}
	done := make(chan error, 1)
	go func() {
		done <- NewServer(h.repo, h.bus).SubscribeSessionEvents(
			&turingv1.SubscribeSessionEventsRequest{SessionId: session.SessionID},
			stream,
		)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		h.bus.mu.Lock()
		subscribers := len(h.bus.subs)
		h.bus.mu.Unlock()
		if subscribers == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("event service did not subscribe")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := h.repo.AppendEvent(ctx, repository.AppendEventInput{
		SessionID: session.SessionID, TraceID: "trace_gap", Type: "message.delta", PayloadJSON: `{"delta":"withdrawn"}`,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.BeginSessionDeletion(ctx, session.SessionID); err != nil {
		t.Fatal(err)
	}
	receipt, err := h.repo.AdvanceSessionDeletion(ctx, session.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	h.bus.TerminateSession(Event{
		EventID: "evt_deleted_gap", SessionID: session.SessionID,
		Sequence: receipt.TerminalSequence, Type: "session.deleted",
	})

	select {
	case err := <-done:
		if status.Code(err) != codes.NotFound {
			t.Fatalf("terminal gap stream error = %v, want NotFound", err)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal gap stream did not finish")
	}
	if len(stream.sent) != 2 ||
		stream.sent[1].GetType() != turingv1.TuringEventType_TURING_EVENT_TYPE_SESSION_DELETED {
		t.Fatalf("terminal gap events = %+v", stream.sent)
	}
}

func TestEventServiceReplaysOverflowGapAndDeliversTerminalExactlyOnce(t *testing.T) {
	database := openEventTestDB(t)
	repo := repository.New(database)
	bus := NewBus(128)
	server := NewServer(repo, bus)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	session, err := repo.CreateSession(ctx, "Slow subscriber")
	if err != nil {
		t.Fatal(err)
	}
	stream := &slowEventStream{
		ctx: ctx, cancel: cancel, firstStarted: make(chan struct{}), releaseFirst: make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() {
		done <- server.SubscribeSessionEvents(&turingv1.SubscribeSessionEventsRequest{SessionId: session.SessionID}, stream)
	}()
	deadline := time.Now().Add(time.Second)
	for {
		bus.mu.Lock()
		subscribers := len(bus.subs)
		bus.mu.Unlock()
		if subscribers == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("event service did not subscribe to bus")
		}
		time.Sleep(time.Millisecond)
	}

	appendAndPublish := func(eventType string, payload string) {
		t.Helper()
		event, err := repo.AppendEvent(context.Background(), repository.AppendEventInput{
			SessionID: session.SessionID, TraceID: "trace_slow", Type: eventType, PayloadJSON: payload,
		})
		if err != nil {
			t.Fatal(err)
		}
		bus.Publish(Event{
			EventID: event.EventID, SessionID: event.SessionID, TraceID: event.TraceID,
			Sequence: event.Sequence, Type: event.Type, CreatedAt: event.CreatedAt, PayloadJSON: event.PayloadJSON,
		})
	}
	appendAndPublish("message.delta", `{"delta":"1"}`)
	select {
	case <-stream.firstStarted:
	case <-time.After(time.Second):
		t.Fatal("slow subscriber did not block on first event")
	}
	for sequence := 2; sequence <= 140; sequence++ {
		eventType := "message.delta"
		payload := fmt.Sprintf(`{"delta":"%d"}`, sequence)
		if sequence == 140 {
			eventType = "agent.run.completed"
			payload = `{"runId":"run_slow"}`
		}
		appendAndPublish(eventType, payload)
	}
	close(stream.releaseFirst)
	select {
	case err := <-done:
		if status.Code(err) != codes.Canceled {
			t.Fatalf("SubscribeSessionEvents error = %v, want canceled after terminal delivery", err)
		}
	case <-time.After(time.Second):
		t.Fatal("slow subscriber was stranded after event bus overflow")
	}

	sent := stream.snapshot()
	if len(sent) != 140 {
		t.Fatalf("delivered %d events, want durable sequences 1..140 exactly once", len(sent))
	}
	terminalCount := 0
	for index, event := range sent {
		wantSequence := int64(index + 1)
		if event.GetSequence() != wantSequence {
			t.Fatalf("sent[%d] sequence = %d, want %d", index, event.GetSequence(), wantSequence)
		}
		if event.GetType() == turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_COMPLETED {
			terminalCount++
		}
	}
	if terminalCount != 1 {
		t.Fatalf("terminal event count = %d, want exactly 1", terminalCount)
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

type slowEventStream struct {
	ctx          context.Context
	cancel       context.CancelFunc
	firstStarted chan struct{}
	releaseFirst chan struct{}
	firstOnce    sync.Once
	mu           sync.Mutex
	sent         []*turingv1.TuringEvent
}

func (s *slowEventStream) Send(event *turingv1.TuringEvent) error {
	if event.GetSequence() == 1 {
		s.firstOnce.Do(func() {
			close(s.firstStarted)
			<-s.releaseFirst
		})
	}
	s.mu.Lock()
	s.sent = append(s.sent, event)
	s.mu.Unlock()
	if event.GetType() == turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_COMPLETED {
		s.cancel()
	}
	return nil
}

func (s *slowEventStream) snapshot() []*turingv1.TuringEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*turingv1.TuringEvent(nil), s.sent...)
}

func (s *slowEventStream) SetHeader(metadata.MD) error  { return nil }
func (s *slowEventStream) SendHeader(metadata.MD) error { return nil }
func (s *slowEventStream) SetTrailer(metadata.MD)       {}
func (s *slowEventStream) Context() context.Context     { return s.ctx }
func (s *slowEventStream) SendMsg(any) error            { return nil }
func (s *slowEventStream) RecvMsg(any) error            { return nil }
