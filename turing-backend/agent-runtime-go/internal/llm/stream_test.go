package llm

import (
	"context"
	"sync"
	"testing"
)

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

func TestSendStreamEventCancellationUnblocksPendingSend(t *testing.T) {
	baseCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan StreamEvent)

	state := newPendingSendState()
	ctx := pendingSendContext{Context: baseCtx, state: state}

	go func() {
		result := sendStreamEvent(ctx, out, StreamEvent{Type: "delta", Text: "blocked"})

		state.mu.Lock()
		state.sent = result
		state.done = true
		state.condition.Broadcast()
		state.mu.Unlock()
	}()

	state.mu.Lock()
	for !state.pending {
		state.condition.Wait()
	}
	if state.done {
		state.mu.Unlock()
		t.Fatal("sendStreamEvent returned before cancellation or a receiver")
	}
	state.mu.Unlock()

	cancel()

	state.mu.Lock()
	for !state.done {
		state.condition.Wait()
	}
	delivered := state.sent
	state.mu.Unlock()

	if delivered {
		t.Fatal("sendStreamEvent reported delivery after cancellation")
	}
	select {
	case event := <-out:
		t.Fatalf("sendStreamEvent delivered event after pending send was canceled: %+v", event)
	default:
	}
}

type pendingSendState struct {
	mu        sync.Mutex
	condition *sync.Cond
	pending   bool
	done      bool
	sent      bool
}

func newPendingSendState() *pendingSendState {
	state := &pendingSendState{}
	state.condition = sync.NewCond(&state.mu)
	return state
}

type pendingSendContext struct {
	context.Context
	state *pendingSendState
}

func (ctx pendingSendContext) Done() <-chan struct{} {
	done := ctx.Context.Done()

	// Select evaluates Done before blocking, so this confirms the sender passed
	// the pre-cancel check and entered the unbuffered send select.
	ctx.state.mu.Lock()
	ctx.state.pending = true
	ctx.state.condition.Broadcast()
	ctx.state.mu.Unlock()

	return done
}
