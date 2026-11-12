package maintenance

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

// The tick runs at the job reaper's interval or one minute, whichever is
// shorter, and at one minute when the reaper is off: never longer, because
// the reaper's interval has no upper bound and the duties assume a minute.
func TestTheIntervalIsTheReapersCappedAtAMinute(t *testing.T) {
	for _, tc := range []struct {
		reaper, want time.Duration
	}{
		{reaper: 0, want: time.Minute},
		{reaper: -time.Second, want: time.Minute},
		{reaper: 20 * time.Millisecond, want: 20 * time.Millisecond},
		{reaper: time.Minute, want: time.Minute},
		{reaper: 10 * time.Minute, want: time.Minute},
	} {
		if got := Interval(tc.reaper); got != tc.want {
			t.Fatalf("Interval(%v) = %v, want %v", tc.reaper, got, tc.want)
		}
	}
}

// A tick runs every duty once, in the order registered; one duty failing
// does not keep the next from running.
func TestATickRunsEveryDutyInOrder(t *testing.T) {
	var ran []string
	loop := New()
	loop.Register("first", func(context.Context) error {
		ran = append(ran, "first")
		return errors.New("first failed")
	})
	loop.Register("second", func(context.Context) error {
		ran = append(ran, "second")
		return nil
	})
	loop.Tick(context.Background())
	if !slices.Equal(ran, []string{"first", "second"}) {
		t.Fatalf("ran %v, want both duties in order", ran)
	}
}

// The loop ticks on each tick it is given, and stops when its context ends.
func TestRunTicksUntilItsContextEnds(t *testing.T) {
	var mu sync.Mutex
	runs := 0
	ticked := make(chan struct{}, 2)
	loop := New()
	loop.Register("count", func(context.Context) error {
		mu.Lock()
		runs++
		mu.Unlock()
		ticked <- struct{}{}
		return nil
	})
	ticks := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		loop.Run(ctx, ticks)
	}()
	for range 2 {
		ticks <- time.Now()
		<-ticked
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after its context ended")
	}
	mu.Lock()
	defer mu.Unlock()
	if runs != 2 {
		t.Fatalf("duty ran %d times, want once per tick", runs)
	}
}

// A tick cut short by shutdown runs no further duties.
func TestATickStopsAtShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var ran []string
	loop := New()
	loop.Register("first", func(context.Context) error {
		ran = append(ran, "first")
		cancel()
		return nil
	})
	loop.Register("second", func(context.Context) error {
		ran = append(ran, "second")
		return nil
	})
	loop.Tick(ctx)
	if !slices.Equal(ran, []string{"first"}) {
		t.Fatalf("ran %v, want only the duty before shutdown", ran)
	}
}
