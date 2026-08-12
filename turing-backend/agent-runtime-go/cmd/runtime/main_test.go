package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestShouldReconnect(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	for name, tc := range map[string]struct {
		ctx  context.Context
		err  error
		want bool
	}{
		// Transport problems are exactly what the loop exists for.
		"stream dropped":          {context.Background(), errors.New("rpc error: code = Unavailable"), true},
		"orchestrator not up yet": {context.Background(), errors.New("connection refused"), true},
		// Run returns nil when the orchestrator sent a shutdown command. Coming
		// back would fight an operator who just asked us to stop.
		"orchestrator asked us to stop": {context.Background(), nil, false},
		// The process was signalled; reconnecting would ignore SIGTERM.
		"signalled mid-stream":     {cancelled, errors.New("rpc error: code = Canceled"), false},
		"signalled with nil error": {cancelled, nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := shouldReconnect(tc.ctx, tc.err); got != tc.want {
				t.Fatalf("shouldReconnect = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBackoffGrowsAndCaps(t *testing.T) {
	delays := []time.Duration{initialBackoff}
	for len(delays) < 12 {
		delays = append(delays, nextBackoff(delays[len(delays)-1]))
	}
	if delays[0] != initialBackoff {
		t.Fatalf("first delay = %v, want %v", delays[0], initialBackoff)
	}
	for i := 1; i < len(delays); i++ {
		if delays[i] < delays[i-1] {
			t.Fatalf("backoff decreased at %d: %v", i, delays)
		}
	}
	if last := delays[len(delays)-1]; last != maxBackoff {
		t.Fatalf("backoff did not cap at %v: %v", maxBackoff, last)
	}
}

// The delay must be interruptible. Writing time.Sleep here instead of a select
// on ctx.Done() would make SIGTERM wait out a 30s backoff before exiting, which
// Docker turns into a SIGKILL.
func TestServeReturnsPromptlyWhenSignalledDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := make(chan struct{}, 4)
	run := func(context.Context) error {
		attempts <- struct{}{}
		return errors.New("stream dropped")
	}

	done := make(chan error, 1)
	go func() { done <- serveWith(ctx, run) }()

	<-attempts // the first attempt failed; serve is now sleeping out its backoff
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v, want nil on shutdown", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not return promptly after cancellation")
	}
}

func TestServeRetriesUntilShutdownRequested(t *testing.T) {
	var calls int
	run := func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("stream dropped")
		}
		return nil // the orchestrator asked us to stop
	}
	if err := serveWith(context.Background(), run); err != nil {
		t.Fatalf("serve returned %v, want nil", err)
	}
	if calls != 3 {
		t.Fatalf("run called %d times, want 3 (two failures then a clean stop)", calls)
	}
}

// Jitter must stay within [d/2, d]: a floor keeps a tight failure loop from
// spinning, and the ceiling keeps the cap meaningful.
func TestJitterStaysWithinHalfToFullDelay(t *testing.T) {
	const d = 8 * time.Second
	var sawBelowFull bool
	for i := 0; i < 200; i++ {
		got := jitter(d)
		if got < d/2 || got > d {
			t.Fatalf("jitter(%v) = %v, want within [%v, %v]", d, got, d/2, d)
		}
		if got < d {
			sawBelowFull = true
		}
	}
	if !sawBelowFull {
		t.Fatal("jitter never varied below the full delay")
	}
	if jitter(0) != 0 {
		t.Fatal("jitter(0) must be 0")
	}
}
