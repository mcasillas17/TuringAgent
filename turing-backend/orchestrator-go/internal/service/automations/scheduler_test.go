package automations

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
)

var schedulerDefaults = repository.AutomationRunDefaults{
	AgentID:       "general_assistant",
	ModelProvider: "ollama",
	Model:         "qwen2.5:7b",
}

type countingDispatcher struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (d *countingDispatcher) DispatchPending(context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	return d.err
}

func (d *countingDispatcher) RefreshPendingRoutingState(context.Context, string) error {
	return nil
}

func (d *countingDispatcher) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

type schedulerHarness struct {
	scheduler  *Scheduler
	repo       *repository.Repository
	database   *db.DB
	bus        *events.Bus
	dispatcher *countingDispatcher
	ctx        context.Context
}

func newSchedulerHarness(t *testing.T) *schedulerHarness {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "turing.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if err := db.ApplyMigrations(ctx, database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	repo := repository.New(database)
	bus := events.NewBus(8)
	dispatcher := &countingDispatcher{}
	return &schedulerHarness{
		scheduler:  NewScheduler(repo, bus, dispatcher, schedulerDefaults),
		repo:       repo,
		database:   database,
		bus:        bus,
		dispatcher: dispatcher,
		ctx:        ctx,
	}
}

// finishRuns terminalizes everything queued so far, which is what a worker
// getting through its backlog looks like.
func (h *schedulerHarness) finishRuns(t *testing.T) {
	t.Helper()
	if _, err := h.database.ExecContext(h.ctx,
		`UPDATE agent_runs SET status = 'completed' WHERE status NOT IN ('completed','failed','cancelled')`); err != nil {
		t.Fatalf("finish runs: %v", err)
	}
}

// firedRuns counts what the automation actually produced, which is the number
// that has to stay at one across restarts and racing ticks.
func (h *schedulerHarness) firedRuns(t *testing.T, automationID string) int {
	t.Helper()
	var count int
	if err := h.database.QueryRowContext(h.ctx,
		`SELECT COUNT(*) FROM automation_runs WHERE automation_id = ?`, automationID).Scan(&count); err != nil {
		t.Fatalf("count automation runs: %v", err)
	}
	return count
}

func createEnabled(t *testing.T, repo *repository.Repository, ctx context.Context, name string, schedule repository.Schedule) repository.Automation {
	t.Helper()
	automation, err := repo.CreateAutomation(ctx, repository.AutomationInput{
		Name: name, Prompt: "Summarise the sandbox.", Schedule: schedule, Enabled: true,
	})
	if err != nil {
		t.Fatalf("create automation: %v", err)
	}
	return automation
}

func parseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed.UTC()
}

func TestTickFiresNothingBeforeAnAutomationIsDue(t *testing.T) {
	h := newSchedulerHarness(t)
	automation := createEnabled(t, h.repo, h.ctx, "Digest", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	due := parseTime(t, automation.NextDueAt)
	h.scheduler.now = func() time.Time { return due.Add(-time.Second) }

	if err := h.scheduler.Tick(h.ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if h.dispatcher.count() != 0 {
		t.Fatalf("dispatched %d times before the automation was due", h.dispatcher.count())
	}
	if got := h.firedRuns(t, automation.AutomationID); got != 0 {
		t.Fatalf("fired %d runs before it was due", got)
	}
}

func TestTickFiresOnceWhenDueAndAgainOnlyAfterTheNextInterval(t *testing.T) {
	h := newSchedulerHarness(t)
	automation := createEnabled(t, h.repo, h.ctx, "Digest", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	due := parseTime(t, automation.NextDueAt)

	h.scheduler.now = func() time.Time { return due }
	if err := h.scheduler.Tick(h.ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	fired, err := h.repo.GetAutomation(h.ctx, automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	if fired.LastRunID == "" || fired.SessionID == "" {
		t.Fatalf("after firing: run %q session %q", fired.LastRunID, fired.SessionID)
	}
	if h.dispatcher.count() != 1 {
		t.Fatalf("dispatched %d times, want 1", h.dispatcher.count())
	}
	if got := h.firedRuns(t, automation.AutomationID); got != 1 {
		t.Fatalf("fired %d runs, want 1", got)
	}

	// A second tick at the same instant must not fire again.
	if err := h.scheduler.Tick(h.ctx); err != nil {
		t.Fatalf("second tick: %v", err)
	}
	if got := h.firedRuns(t, automation.AutomationID); got != 1 {
		t.Fatalf("two ticks at the same instant produced %d runs, want 1", got)
	}

	// The previous run has to finish before the next occurrence is allowed to
	// fire, or a slow automation would queue work faster than it drains.
	h.finishRuns(t)
	h.scheduler.now = func() time.Time { return due.Add(5 * time.Minute) }
	if err := h.scheduler.Tick(h.ctx); err != nil {
		t.Fatalf("third tick: %v", err)
	}
	if got := h.firedRuns(t, automation.AutomationID); got != 2 {
		t.Fatalf("after the next interval it had fired %d times, want 2", got)
	}
}

func TestTickPublishesSessionUpdatedBeforeQueued(t *testing.T) {
	h := newSchedulerHarness(t)
	automation := createEnabled(t, h.repo, h.ctx, "Digest", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	session, err := h.repo.CreateSession(h.ctx, automation.Name)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := h.database.ExecContext(h.ctx,
		`UPDATE automations SET session_id = ? WHERE id = ?`,
		session.SessionID, automation.AutomationID); err != nil {
		t.Fatalf("attach session: %v", err)
	}
	published, unsubscribe := h.bus.Subscribe(session.SessionID)
	defer unsubscribe()
	h.scheduler.now = func() time.Time { return parseTime(t, automation.NextDueAt) }

	if err := h.scheduler.Tick(h.ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	first := <-published
	if first.Type != "session.updated" {
		t.Fatalf("first published event = %q, want session.updated", first.Type)
	}
	second := <-published
	if second.Type != "agent.run.queued" {
		t.Fatalf("second published event = %q, want agent.run.queued", second.Type)
	}
}

// The state that stops a re-fire lives in the row, not in this process. A
// fresh Scheduler over the same database is a restart.
func TestARestartedSchedulerDoesNotRefireWhatAlreadyRan(t *testing.T) {
	h := newSchedulerHarness(t)
	automation := createEnabled(t, h.repo, h.ctx, "Digest", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	due := parseTime(t, automation.NextDueAt)
	h.scheduler.now = func() time.Time { return due }
	if err := h.scheduler.Tick(h.ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	restarted := NewScheduler(h.repo, events.NewBus(8), &countingDispatcher{}, schedulerDefaults)
	restarted.now = func() time.Time { return due }
	if err := restarted.Tick(h.ctx); err != nil {
		t.Fatalf("tick after restart: %v", err)
	}
	if got := h.firedRuns(t, automation.AutomationID); got != 1 {
		t.Fatalf("a restart produced %d runs, want 1", got)
	}
}

func TestADisabledAutomationNeverFires(t *testing.T) {
	h := newSchedulerHarness(t)
	automation := createEnabled(t, h.repo, h.ctx, "Digest", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	due := parseTime(t, automation.NextDueAt)
	if _, err := h.repo.SetAutomationEnabled(h.ctx, automation.AutomationID, false); err != nil {
		t.Fatal(err)
	}

	// Well past due, and repeatedly.
	h.scheduler.now = func() time.Time { return due.Add(time.Hour) }
	for range 3 {
		if err := h.scheduler.Tick(h.ctx); err != nil {
			t.Fatalf("tick: %v", err)
		}
	}
	if h.dispatcher.count() != 0 {
		t.Fatalf("a disabled automation dispatched %d times", h.dispatcher.count())
	}
	if got := h.firedRuns(t, automation.AutomationID); got != 0 {
		t.Fatalf("a disabled automation fired %d runs", got)
	}
	reloaded, err := h.repo.GetAutomation(h.ctx, automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.SessionID != "" {
		t.Fatalf("a disabled automation produced conversation %q", reloaded.SessionID)
	}
}

// Two schedulers ticking simultaneously is the shape of a restart overlapping
// an old process, and of a future second orchestrator. Neither may produce a
// second run.
func TestTwoSchedulersTickingAtOnceFireOnce(t *testing.T) {
	h := newSchedulerHarness(t)
	automation := createEnabled(t, h.repo, h.ctx, "Digest", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	due := parseTime(t, automation.NextDueAt)
	other := NewScheduler(h.repo, events.NewBus(8), &countingDispatcher{}, schedulerDefaults)
	h.scheduler.now = func() time.Time { return due }
	other.now = func() time.Time { return due }

	var wait sync.WaitGroup
	errs := make(chan error, 2)
	wait.Add(2)
	for _, ticker := range []*Scheduler{h.scheduler, other} {
		go func() {
			defer wait.Done()
			if err := ticker.Tick(h.ctx); err != nil {
				errs <- err
			}
		}()
	}
	wait.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("tick: %v", err)
	}

	if got := h.firedRuns(t, automation.AutomationID); got != 1 {
		t.Fatalf("two simultaneous ticks produced %d runs, want 1", got)
	}
}

// A run that cannot be handed to a worker right now is still queued and
// durable, so one failing dispatch must not stop the automations behind it.
func TestAFailingDispatchDoesNotStopTheRemainingAutomations(t *testing.T) {
	h := newSchedulerHarness(t)
	h.dispatcher.err = errors.New("no worker")
	first := createEnabled(t, h.repo, h.ctx, "First", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	second := createEnabled(t, h.repo, h.ctx, "Second", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	h.scheduler.now = func() time.Time { return parseTime(t, first.NextDueAt).Add(time.Minute) }

	if err := h.scheduler.Tick(h.ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	for _, automation := range []repository.Automation{first, second} {
		if got := h.firedRuns(t, automation.AutomationID); got != 1 {
			t.Fatalf("%s fired %d runs despite a dispatch failure, want 1", automation.Name, got)
		}
	}
	if h.dispatcher.count() != 2 {
		t.Fatalf("dispatched %d times, want one per fired automation", h.dispatcher.count())
	}
}

// A five-minute automation whose run takes longer than five minutes must not
// accumulate a queue. Every queued automation job sits ahead of whatever the
// user types next, so an unbounded backlog silently stops their own chat.
func TestASlowAutomationDoesNotAccumulateABacklog(t *testing.T) {
	h := newSchedulerHarness(t)
	automation := createEnabled(t, h.repo, h.ctx, "Digest", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	at := parseTime(t, automation.NextDueAt)

	// Twelve occurrences pass while the first run never finishes.
	for range 12 {
		h.scheduler.now = func() time.Time { return at }
		if err := h.scheduler.Tick(h.ctx); err != nil {
			t.Fatalf("tick: %v", err)
		}
		at = at.Add(5 * time.Minute)
	}
	if got := h.firedRuns(t, automation.AutomationID); got != 1 {
		t.Fatalf("a run that never finished produced %d queued runs, want 1", got)
	}

	// And it resumes once the worker catches up, rather than staying stuck.
	h.finishRuns(t)
	h.scheduler.now = func() time.Time { return at.Add(time.Hour) }
	if err := h.scheduler.Tick(h.ctx); err != nil {
		t.Fatalf("tick after catching up: %v", err)
	}
	if got := h.firedRuns(t, automation.AutomationID); got != 2 {
		t.Fatalf("after the run finished it had fired %d times, want 2", got)
	}
}

// One unreadable row must not stop the automations behind it. Before the
// skip existed, Tick returned on the error and the same row was re-selected
// forever.
func TestOnePoisonedAutomationDoesNotStopTheRest(t *testing.T) {
	h := newSchedulerHarness(t)
	poisoned := createEnabled(t, h.repo, h.ctx, "Poisoned", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	healthy := createEnabled(t, h.repo, h.ctx, "Healthy", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	if _, err := h.database.ExecContext(h.ctx,
		`UPDATE automations SET schedule_kind = 'lunar', interval_seconds = NULL WHERE id = ?`,
		poisoned.AutomationID); err != nil {
		t.Fatal(err)
	}
	h.scheduler.now = func() time.Time {
		return parseTime(t, healthy.NextDueAt).Add(time.Minute)
	}

	if err := h.scheduler.Tick(h.ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	if got := h.firedRuns(t, healthy.AutomationID); got != 1 {
		t.Fatalf("the healthy automation fired %d times, want 1", got)
	}
	if got := h.firedRuns(t, poisoned.AutomationID); got != 0 {
		t.Fatalf("the poisoned automation fired %d times, want 0", got)
	}
}

// A cancelled context stops the loop rather than draining a due backlog on the
// way out.
func TestTickStopsWhenTheContextIsCancelled(t *testing.T) {
	h := newSchedulerHarness(t)
	automation := createEnabled(t, h.repo, h.ctx, "Digest", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	h.scheduler.now = func() time.Time { return parseTime(t, automation.NextDueAt) }
	cancelled, cancel := context.WithCancel(h.ctx)
	cancel()

	if err := h.scheduler.Tick(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("tick error = %v, want context.Canceled", err)
	}
	if got := h.firedRuns(t, automation.AutomationID); got != 0 {
		t.Fatalf("a cancelled tick fired %d runs", got)
	}
}

// Run must return when its context is cancelled, or shutdown blocks forever
// waiting on a goroutine that is still ticking.
func TestRunReturnsOnCancellation(t *testing.T) {
	h := newSchedulerHarness(t)
	runCtx, cancel := context.WithCancel(h.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.scheduler.Run(runCtx, time.Millisecond)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

// An interval of zero means "off", and off must mean the loop never starts
// rather than spinning on a ticker that panics.
func TestRunWithNoIntervalDoesNothing(t *testing.T) {
	h := newSchedulerHarness(t)
	automation := createEnabled(t, h.repo, h.ctx, "Digest", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	h.scheduler.now = func() time.Time { return parseTime(t, automation.NextDueAt).Add(time.Hour) }

	done := make(chan struct{})
	go func() {
		defer close(done)
		h.scheduler.Run(h.ctx, 0)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run with a zero interval did not return")
	}
	if got := h.firedRuns(t, automation.AutomationID); got != 0 {
		t.Fatalf("a disabled scheduler fired %d runs", got)
	}
}

// The tick loop is bounded so a backlog cannot queue runs indefinitely before
// the loop next looks at its context.
func TestOneTickFiresAtMostItsBound(t *testing.T) {
	h := newSchedulerHarness(t)
	var latest time.Time
	for index := range maxFiresPerTick + 5 {
		automation := createEnabled(t, h.repo, h.ctx, "Digest "+string(rune('a'+index)),
			repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
		if due := parseTime(t, automation.NextDueAt); due.After(latest) {
			latest = due
		}
	}
	h.scheduler.now = func() time.Time { return latest }

	if err := h.scheduler.Tick(h.ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}
	var total int
	if err := h.database.QueryRowContext(h.ctx, `SELECT COUNT(*) FROM automation_runs`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != maxFiresPerTick {
		t.Fatalf("one tick fired %d runs, want the bound of %d", total, maxFiresPerTick)
	}
}
