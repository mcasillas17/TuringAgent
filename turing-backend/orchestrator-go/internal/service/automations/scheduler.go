package automations

import (
	"context"
	"log"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
)

// maxFiresPerTick bounds one pass over the due list. Without it, a database
// holding a hundred overdue automations would queue a hundred runs before the
// loop next looked at its context — including its cancellation.
const maxFiresPerTick = 20

// Dispatcher is the runtime service, narrowed to the one thing the scheduler
// needs from it: telling a worker there is work.
type Dispatcher interface {
	DispatchPending(context.Context) error
}

// Scheduler creates the runs nobody asked for.
//
// It owns no schedule state of its own. Everything it needs is the automations
// table's next_due_at, which is why restarting the process cannot re-fire
// something that already ran, and why two of these ticking at once still
// produce one run each time: the claim advances the row inside the same
// transaction that queues the run, so the second tick's read no longer sees it
// as due.
type Scheduler struct {
	repo       *repository.Repository
	bus        *events.Bus
	dispatcher Dispatcher
	defaults   repository.AutomationRunDefaults
	// now is injectable so a test can put the clock where it needs it instead
	// of sleeping through a real interval.
	now func() time.Time
}

func NewScheduler(repo *repository.Repository, bus *events.Bus, dispatcher Dispatcher, defaults repository.AutomationRunDefaults) *Scheduler {
	return &Scheduler{repo: repo, bus: bus, dispatcher: dispatcher, defaults: defaults, now: time.Now}
}

// Run ticks until the context is cancelled, mirroring the recovery loop's
// shape so there is one pattern for background work in this process.
func (s *Scheduler) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Tick(ctx); err != nil {
				log.Printf("automation tick: %v", err)
			}
		}
	}
}

// Tick works through every automation that has come due, one at a time.
//
// One at a time rather than in a batch because each claim is its own
// transaction: a failure part-way through leaves the automations that already
// fired fired, and the rest still due, which is the state a later tick can
// finish from.
//
// Not every due automation produces a run — one whose previous run is still
// going passes the occurrence by. That still counts against the per-tick
// bound, because the work of claiming it is what the bound is protecting.
func (s *Scheduler) Tick(ctx context.Context) error {
	for fired := 0; fired < maxFiresPerTick; fired++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		fire, found, err := s.repo.ClaimDueAutomation(ctx, s.now(), s.defaults)
		if err != nil {
			return err
		}
		if !found {
			return nil
		}
		if fire.Skipped {
			// Logged rather than swallowed: an automation that keeps skipping
			// is running slower than its schedule, and the only place that is
			// currently legible is here and on its card.
			log.Printf("automation %s (%s) skipped an occurrence: %s",
				fire.Name, fire.AutomationID, fire.SkippedReason)
			continue
		}
		if s.bus != nil {
			if fire.SessionUpdatedEvent.EventID != "" {
				s.bus.Publish(busEvent(fire.SessionUpdatedEvent))
			}
			if fire.QueuedEvent.EventID != "" {
				s.bus.Publish(busEvent(fire.QueuedEvent))
			}
		}
		if s.dispatcher != nil {
			if err := s.dispatcher.DispatchPending(ctx); err != nil {
				// The run is queued and durable; a worker will pick it up on
				// the next dispatch either way. Reported, not fatal, so one
				// failing dispatch does not stop the remaining automations.
				log.Printf("dispatch automation run %s: %v", fire.RunID, err)
			}
		}
	}
	return nil
}

func busEvent(event repository.Event) events.Event {
	runID := ""
	if event.RunID.Valid {
		runID = event.RunID.String
	}
	return events.Event{
		EventID:     event.EventID,
		SessionID:   event.SessionID,
		RunID:       runID,
		TraceID:     event.TraceID,
		Sequence:    event.Sequence,
		Type:        event.Type,
		CreatedAt:   event.CreatedAt,
		PayloadJSON: event.PayloadJSON,
	}
}
