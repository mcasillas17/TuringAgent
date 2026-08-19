package main

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/agent-runtime-go/internal/worker"
)

func TestAdvertisedModelsReflectConfiguredProvidersAndContextLimits(t *testing.T) {
	cfg := config.Config{
		OllamaModel: "qwen", OllamaContextWindowTokens: 32768,
		OpenAIModel: "gpt", OpenAIContextWindowTokens: 128000,
	}
	models := advertisedModels(cfg)
	if len(models) != 1 || models[0].GetProvider() != turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA ||
		models[0].GetModel() != "qwen" || models[0].GetMaxContextTokens() != 32768 {
		t.Fatalf("models without OpenAI key = %+v", models)
	}
	cfg.OpenAIAPIKey = "configured"
	models = advertisedModels(cfg)
	if len(models) != 2 || models[1].GetProvider() != turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE ||
		models[1].GetModel() != "gpt" || models[1].GetMaxContextTokens() != 128000 {
		t.Fatalf("models with OpenAI key = %+v", models)
	}
}

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
	// Real time.After here: the point is that cancellation interrupts a genuine
	// wait rather than the loop happening to come round again.
	go func() { done <- serveWith(ctx, run, healthyStreamDuration, time.After) }()

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
	if err := serveWith(context.Background(), run, healthyStreamDuration, instantSleep); err != nil {
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

// instantSleep makes the loop's delay a no-op so timing behaviour can be
// asserted without waiting real seconds.
func instantSleep(time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	ch <- time.Time{}
	return ch
}

// recordingSleep captures each requested delay and returns immediately.
func recordingSleep(into *[]time.Duration) func(time.Duration) <-chan time.Time {
	return func(d time.Duration) <-chan time.Time {
		*into = append(*into, d)
		return instantSleep(d)
	}
}

// Without this, deleting `backoff = nextBackoff(backoff)` from the loop leaves
// the suite green while the runtime retries at the initial delay forever.
func TestServeGrowsTheDelayBetweenFailedAttempts(t *testing.T) {
	var delays []time.Duration
	var calls int
	run := func(context.Context) error {
		calls++
		if calls > 4 {
			return nil
		}
		return errors.New("stream dropped")
	}
	if err := serveWith(context.Background(), run, time.Hour, recordingSleep(&delays)); err != nil {
		t.Fatal(err)
	}
	if len(delays) != 4 {
		t.Fatalf("recorded %d delays, want 4: %v", len(delays), delays)
	}
	// Jitter is [d/2, d], so consecutive samples can overlap; compare the ends.
	if delays[len(delays)-1] <= delays[0] {
		t.Fatalf("delay did not grow across attempts: %v", delays)
	}
}

// Without this, inverting or deleting the healthy-stream reset is invisible.
func TestServeResetsTheDelayAfterAHealthyStream(t *testing.T) {
	var delays []time.Duration
	var calls int
	run := func(context.Context) error {
		calls++
		if calls > 4 {
			return nil
		}
		return errors.New("stream dropped")
	}
	// healthyFor 0 means every attempt counts as healthy, so the delay must
	// never climb past the initial value.
	if err := serveWith(context.Background(), run, 0, recordingSleep(&delays)); err != nil {
		t.Fatal(err)
	}
	for i, d := range delays {
		if d > initialBackoff {
			t.Fatalf("delay %d = %v exceeded the initial backoff; the reset did not apply: %v", i, d, delays)
		}
	}
}

// A configuration Run rejects can never succeed on retry. Looping it would keep
// the process alive, never exit non-zero, and hide the fault from the operator.
func TestServeDoesNotRetryInvalidConfiguration(t *testing.T) {
	var calls int
	run := func(context.Context) error {
		calls++
		return fmt.Errorf("%w: max concurrent runs must be between 1 and 128", worker.ErrInvalidConfig)
	}
	var delays []time.Duration
	if err := serveWith(context.Background(), run, time.Hour, recordingSleep(&delays)); err != nil {
		t.Fatalf("serve returned %v, want nil", err)
	}
	if calls != 1 {
		t.Fatalf("run called %d times, want 1 — an unretryable config error must not loop", calls)
	}
}
