// Package maintenance runs the orchestrator's background duties on one
// goroutine of their own.
//
// It is not the job reaper's goroutine: a duty must run even with the reaper
// switched off (TURING_JOB_REAPER_INTERVAL_MS=0), and must never delay
// orphaned-assignment recovery. Each slice that adds a background duty
// registers it here instead of starting another loop.
package maintenance

import (
	"context"
	"log"
	"time"
)

// MaxInterval is the longest one tick waits. The reaper's interval has no
// upper bound, and every duty's bounds assume a tick of a minute at most.
const MaxInterval = time.Minute

// Interval is how often the maintenance tick runs, given the job reaper's
// interval: that interval or MaxInterval, whichever is shorter, and
// MaxInterval when the reaper is off.
func Interval(reaperInterval time.Duration) time.Duration {
	if reaperInterval <= 0 || reaperInterval > MaxInterval {
		return MaxInterval
	}
	return reaperInterval
}

// Duty is one background duty. It runs once per tick and should keep each of
// its transactions within the budget its design states.
type Duty func(context.Context) error

type registeredDuty struct {
	name string
	run  Duty
}

// Loop is the maintenance goroutine's set of duties. Register every duty
// before Run starts; a Loop is not safe to register on concurrently with Run.
type Loop struct {
	duties []registeredDuty
}

func New() *Loop { return &Loop{} }

// Register adds a duty, run on every tick after the duties registered before
// it. The name only labels its errors in the log.
func (l *Loop) Register(name string, duty Duty) {
	l.duties = append(l.duties, registeredDuty{name: name, run: duty})
}

// Tick runs every duty once, in the order registered. A duty's error is
// logged and the next duty still runs, since each owns its own work and its
// own retry on the next tick. Shutdown stops the tick between duties.
func (l *Loop) Tick(ctx context.Context) {
	for _, duty := range l.duties {
		if ctx.Err() != nil {
			return
		}
		if err := duty.run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("maintenance: %s: %v", duty.name, err)
		}
	}
}

// Run ticks once for every value ticks delivers, until ctx ends.
func (l *Loop) Run(ctx context.Context, ticks <-chan time.Time) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			l.Tick(ctx)
		}
	}
}
