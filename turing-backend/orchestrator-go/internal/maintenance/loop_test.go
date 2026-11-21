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

// A kick asks for one tick now, ahead of the next scheduled one. It never
// blocks, whether the loop is not running yet or is busy in a tick, and kicks
// that arrive before their tick runs are coalesced into it.
func TestAKickTicksOnceSoonAndNeverBlocks(t *testing.T) {
	var mu sync.Mutex
	runs := 0
	started := make(chan struct{})
	release := make(chan struct{})
	ticked := make(chan struct{}, 4)
	loop := New()
	loop.Register("count", func(context.Context) error {
		mu.Lock()
		runs++
		first := runs == 1
		mu.Unlock()
		if first {
			close(started)
			<-release
		}
		ticked <- struct{}{}
		return nil
	})
	for range 3 {
		loop.Kick()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		loop.Run(ctx, nil)
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the kicks before Run did not tick")
	}
	for range 3 {
		loop.Kick()
	}
	close(release)
	for range 2 {
		select {
		case <-ticked:
		case <-time.After(5 * time.Second):
			t.Fatal("a kick did not tick")
		}
	}
	select {
	case <-ticked:
		t.Fatal("kicks were not coalesced: a third tick ran")
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if runs != 2 {
		t.Fatalf("duty ran %d times, want one tick for the kicks before Run and one for those during the first tick", runs)
	}
}
