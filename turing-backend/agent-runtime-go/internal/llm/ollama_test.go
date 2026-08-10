package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaStreamChatParsesDeltaAndCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":{"content":"Hel"},"done":false}` + "\n"))
		w.Write([]byte(`{"done":true,"done_reason":"stop"}` + "\n"))
	}))
	t.Cleanup(server.Close)
	provider := NewOllama(server.URL, server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "llama3.2", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 2 || got[0].Text != "Hel" || got[1].Type != "completed" {
		t.Fatalf("events = %+v", got)
	}
}

func TestOllamaStreamChatMalformedJSONReturnsErrorEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":` + "\n"))
	}))
	t.Cleanup(server.Close)
	provider := NewOllama(server.URL, server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "llama3.2"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if len(got) != 1 || got[0].Code != "model_bad_chunk" {
		t.Fatalf("events = %+v, want model_bad_chunk", got)
	}
}

func TestSendStreamEventRejectsReadySendAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := make(chan StreamEvent, 1)

	if sendStreamEvent(ctx, out, StreamEvent{Type: "delta", Text: "late"}) {
		t.Fatal("sendStreamEvent accepted an event after cancellation")
	}
	if len(out) != 0 {
		t.Fatal("sendStreamEvent delivered an event after cancellation")
	}
}

func collectEvents(events <-chan StreamEvent) []StreamEvent {
	var got []StreamEvent
	for event := range events {
		got = append(got, event)
	}
	return got
}
