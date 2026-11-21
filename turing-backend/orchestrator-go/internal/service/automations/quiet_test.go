package automations

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// recordingPublisher keeps every event the service published.
type recordingPublisher struct {
	mu     sync.Mutex
	events []events.Event
}

func (p *recordingPublisher) Publish(event events.Event) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
}

func (p *recordingPublisher) cancelled(runID string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	count := 0
	for _, event := range p.events {
		if event.RunID == runID && event.Type == "agent.run.cancelled" {
			count++
		}
	}
	return count
}

func setQuiet(t *testing.T, ctx context.Context, server *Server, quiet bool, revision int64) *turingv1.SetUnattendedQuietResponse {
	t.Helper()
	response, err := server.SetUnattendedQuiet(ctx, &turingv1.SetUnattendedQuietRequest{Quiet: quiet, Revision: revision})
	if err != nil {
		t.Fatalf("SetUnattendedQuiet(%v, %d): %v", quiet, revision, err)
	}
	return response
}

func quietRunStatus(t *testing.T, repo *repository.Repository, ctx context.Context, runID string) (string, string) {
	t.Helper()
	run, err := repo.GetRun(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	return run.Status, run.CancellationReason
}

// TUR-028a: the switch reads as stored, and a write answers with the
// setting as it now stands. Only a change to on starts the drain; a stale
// write answers with the current setting, writes nothing and starts nothing.
func TestTheQuietSwitchIsReadAndSetCompareAndSet(t *testing.T) {
	server, _, ctx := newTestServer(t)
	kicks := 0
	server.SetQuietDrainTrigger(func() { kicks++ })

	got, err := server.GetUnattendedQuiet(ctx, &turingv1.GetUnattendedQuietRequest{})
	if err != nil || !proto.Equal(got, &turingv1.UnattendedQuiet{}) {
		t.Fatalf("GetUnattendedQuiet = %v, %v; want off at revision 0", got, err)
	}
	on := setQuiet(t, ctx, server, true, 0)
	if !proto.Equal(on, &turingv1.SetUnattendedQuietResponse{Setting: &turingv1.UnattendedQuiet{Quiet: true, Revision: 1}}) || kicks != 1 {
		t.Fatalf("turn quiet on = %v, %d kicks; want on at revision 1 and the drain started", on, kicks)
	}
	stale := setQuiet(t, ctx, server, false, 0)
	if !proto.Equal(stale, &turingv1.SetUnattendedQuietResponse{Setting: &turingv1.UnattendedQuiet{Quiet: true, Revision: 1}, Stale: true}) {
		t.Fatalf("stale quiet-off = %v, want stale with the current on at revision 1", stale)
	}
	again := setQuiet(t, ctx, server, true, 1)
	if !proto.Equal(again, &turingv1.SetUnattendedQuietResponse{Setting: &turingv1.UnattendedQuiet{Quiet: true, Revision: 1}}) {
		t.Fatalf("turn quiet on while on = %v, want the setting unchanged", again)
	}
	off := setQuiet(t, ctx, server, false, 1)
	if !proto.Equal(off, &turingv1.SetUnattendedQuietResponse{Setting: &turingv1.UnattendedQuiet{Revision: 2}}) {
		t.Fatalf("turn quiet off = %v, want off at revision 2", off)
	}
	if kicks != 1 {
		t.Fatalf("drain kicks = %d, want 1: only turning quiet on starts it", kicks)
	}
	if _, err := server.SetUnattendedQuiet(ctx, nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("SetUnattendedQuiet(nil) = %v, want InvalidArgument", err)
	}
}

// A stored value that cannot be read is shown as on and unavailable, with
// the revision that repairs it; repairing it to on starts the drain like any
// other change to on.
func TestACorruptQuietValueIsShownUnavailable(t *testing.T) {
	h := newSchedulerHarness(t)
	server, ctx := New(h.repo), h.ctx
	if _, err := h.database.ExecContext(ctx, `
		INSERT INTO settings (key, value_json, revision, updated_at) VALUES ('unattended_quiet', '[]', 6, '2026-11-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	got, err := server.GetUnattendedQuiet(ctx, &turingv1.GetUnattendedQuietRequest{})
	if err != nil || !proto.Equal(got, &turingv1.UnattendedQuiet{Quiet: true, Revision: 6, Unavailable: true}) {
		t.Fatalf("GetUnattendedQuiet = %v, %v; want on, unavailable, at revision 6", got, err)
	}
	kicks := 0
	server.SetQuietDrainTrigger(func() { kicks++ })
	repaired := setQuiet(t, ctx, server, true, 6)
	if !proto.Equal(repaired.GetSetting(), &turingv1.UnattendedQuiet{Quiet: true, Revision: 7}) || repaired.GetStale() || kicks != 1 {
		t.Fatalf("repair to on = %v with %d kicks, want on at revision 7 and the drain started", repaired, kicks)
	}
}

// queueAutomationRuns fires count automations once each, leaving their
// unattended runs queued, and returns the runs.
func queueAutomationRuns(t *testing.T, h *schedulerHarness, count int) []string {
	t.Helper()
	runs := make([]string, 0, count)
	for index := range count {
		automation := createEnabled(t, h.repo, h.ctx, fmt.Sprintf("Digest %03d", index),
			repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
		fire, found, err := h.repo.ClaimDueAutomation(h.ctx, parseTime(t, automation.NextDueAt), schedulerDefaults)
		if err != nil || !found {
			t.Fatalf("fire %d = %v, %v", index, found, err)
		}
		runs = append(runs, fire.RunID)
	}
	return runs
}

func (h *schedulerHarness) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var count int
	if err := h.database.QueryRowContext(h.ctx, query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// The maintenance duty finishes a drain: every queued automation run is
// cancelled owner_quiet and its cancellation is published once. A duty cut
// short after a committed batch, with work left, leaves the drain pending,
// and the next run of the duty finishes it.
func TestTheDrainDutyCancelsQueuedRunsAndPublishesThem(t *testing.T) {
	h := newSchedulerHarness(t)
	server := New(h.repo)
	publisher := &recordingPublisher{}
	server.SetEventPublisher(publisher)
	runs := queueAutomationRuns(t, h, repository.MaxQuietDrainBatch+1)
	setQuiet(t, h.ctx, server, true, 0)

	interrupted, interrupt := context.WithCancel(h.ctx)
	server.quietDrainBatchDone = func(int) { interrupt() }
	if err := server.DrainUnattendedQuiet(interrupted); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted drain = %v, want it cut short", err)
	}
	server.quietDrainBatchDone = nil
	if cancelled := h.count(t, `SELECT COUNT(*) FROM agent_runs WHERE cancellation_reason = 'owner_quiet'`); cancelled != repository.MaxQuietDrainBatch {
		t.Fatalf("cancelled after one batch = %d, want %d", cancelled, repository.MaxQuietDrainBatch)
	}
	if pending := h.count(t, `SELECT COUNT(*) FROM settings WHERE key = 'unattended_quiet_drain'`); pending != 1 {
		t.Fatal("a drain cut short with work left cleared its key")
	}
	if err := server.DrainUnattendedQuiet(h.ctx); err != nil {
		t.Fatal(err)
	}
	for _, runID := range runs {
		if runStatus, code := quietRunStatus(t, h.repo, h.ctx, runID); runStatus != "cancelled" || code != runoutcome.CodeOwnerQuiet {
			t.Fatalf("run %s = %s/%s, want cancelled owner_quiet", runID, runStatus, code)
		}
		if got := publisher.cancelled(runID); got != 1 {
			t.Fatalf("published %d cancellations of %s, want 1", got, runID)
		}
	}
	if err := server.DrainUnattendedQuiet(h.ctx); err != nil {
		t.Fatalf("a duty with nothing to drain = %v", err)
	}
}

// Over a quiet hour, with one automation run already going and another
// automation due every five minutes, no unattended run starts and nothing
// fires; the run already going keeps running to its end.
func TestAQuietHourStartsNoUnattendedRun(t *testing.T) {
	h := newSchedulerHarness(t)
	server := New(h.repo)
	running := createEnabled(t, h.repo, h.ctx, "Digest", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	start := parseTime(t, running.NextDueAt)
	h.scheduler.now = func() time.Time { return start }
	if err := h.scheduler.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	claimed, err := h.repo.ClaimNextCompatibleJobWithLimit(h.ctx, "general_assistant", "worker-1", 0, time.Hour, nil, nil)
	if err != nil || claimed.RunID == "" {
		t.Fatalf("claim = %+v, %v; want the automation run started", claimed, err)
	}
	due := createEnabled(t, h.repo, h.ctx, "Inbox", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	setQuiet(t, h.ctx, server, true, 0)
	if err := server.DrainUnattendedQuiet(h.ctx); err != nil {
		t.Fatal(err)
	}

	for minute := range 61 {
		at := start.Add(time.Duration(minute) * time.Minute)
		h.scheduler.now = func() time.Time { return at }
		if err := h.scheduler.Tick(h.ctx); err != nil {
			t.Fatal(err)
		}
		if next, err := h.repo.ClaimNextCompatibleJobWithLimit(h.ctx, "general_assistant", "worker-1", 0, time.Hour, nil, nil); err != nil || next.RunID != "" {
			t.Fatalf("claim at minute %d = %+v, %v; want no unattended run started", minute, next, err)
		}
	}
	if got := h.firedRuns(t, running.AutomationID) + h.firedRuns(t, due.AutomationID); got != 1 {
		t.Fatalf("automation runs after a quiet hour = %d, want only the one already going", got)
	}
	if runStatus, _ := quietRunStatus(t, h.repo, h.ctx, claimed.RunID); runStatus != "running" {
		t.Fatalf("the run already going = %s after the quiet hour, want it still running", runStatus)
	}
	state, err := h.repo.GetRunState(h.ctx, claimed.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.CompleteRunCanonical(h.ctx, repository.CompleteRunInput{
		RunID: claimed.RunID, AssistantMessageID: claimed.AssistantMessageID, Content: "Done.", ExpectedStateVersion: state.StateVersion,
	}); err != nil {
		t.Fatalf("the run already going could not finish: %v", err)
	}
}

// A due automation does not fire while quiet, and its next due time stays
// where it was; once quiet is off and the drain is done, it fires exactly
// once.
func TestADueAutomationFiresOnceAfterQuiet(t *testing.T) {
	h := newSchedulerHarness(t)
	server := New(h.repo)
	automation := createEnabled(t, h.repo, h.ctx, "Digest", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	due := parseTime(t, automation.NextDueAt)
	setQuiet(t, h.ctx, server, true, 0)
	h.scheduler.now = func() time.Time { return due.Add(time.Minute) }
	if err := h.scheduler.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	if got := h.firedRuns(t, automation.AutomationID); got != 0 {
		t.Fatalf("fired %d while quiet, want none", got)
	}
	held, err := h.repo.GetAutomation(h.ctx, automation.AutomationID)
	if err != nil || held.NextDueAt != automation.NextDueAt {
		t.Fatalf("next due while quiet = %q, %v; want it unchanged at %q", held.NextDueAt, err, automation.NextDueAt)
	}
	setQuiet(t, h.ctx, server, false, 1)
	if err := h.scheduler.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	if got := h.firedRuns(t, automation.AutomationID); got != 0 {
		t.Fatalf("fired %d before the drain finished, want none", got)
	}
	if err := server.DrainUnattendedQuiet(h.ctx); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := h.scheduler.Tick(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.firedRuns(t, automation.AutomationID); got != 1 {
		t.Fatalf("fired %d after quiet, want exactly once", got)
	}
}

// A daily automation held quiet for three days fires once when quiet ends,
// not once for each day it missed, and is next due on the following day.
func TestADailyAutomationQuietForThreeDaysFiresOnce(t *testing.T) {
	h := newSchedulerHarness(t)
	server := New(h.repo)
	automation := createEnabled(t, h.repo, h.ctx, "Morning", repository.Schedule{Kind: repository.ScheduleDaily, DailyMinuteUTC: 7 * 60})
	due := parseTime(t, automation.NextDueAt)
	setQuiet(t, h.ctx, server, true, 0)
	for day := range 4 {
		at := due.Add(time.Duration(day) * 24 * time.Hour)
		h.scheduler.now = func() time.Time { return at }
		if err := h.scheduler.Tick(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	held, err := h.repo.GetAutomation(h.ctx, automation.AutomationID)
	if err != nil || held.NextDueAt != automation.NextDueAt || h.firedRuns(t, automation.AutomationID) != 0 {
		t.Fatalf("after three quiet days: next due %q (want %q unchanged), %d fired (want none), %v",
			held.NextDueAt, automation.NextDueAt, h.firedRuns(t, automation.AutomationID), err)
	}
	setQuiet(t, h.ctx, server, false, 1)
	if err := server.DrainUnattendedQuiet(h.ctx); err != nil {
		t.Fatal(err)
	}
	resumed := due.Add(3*24*time.Hour + time.Hour)
	h.scheduler.now = func() time.Time { return resumed }
	for range 3 {
		if err := h.scheduler.Tick(h.ctx); err != nil {
			t.Fatal(err)
		}
	}
	if got := h.firedRuns(t, automation.AutomationID); got != 1 {
		t.Fatalf("fired %d after three quiet days, want once", got)
	}
	fired, err := h.repo.GetAutomation(h.ctx, automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	if next := parseTime(t, fired.NextDueAt); !next.After(resumed) || next.Sub(resumed) > 24*time.Hour {
		t.Fatalf("next due = %s, want the next 07:00Z after %s", next, resumed)
	}
}

// A queued unattended run blocks the later turns of its own conversation, so
// cancelling it can make an owner's turn claimable that no other event will
// dispatch, with the reaper off. Each drain batch that cancelled something
// dispatches pending work, as does turning quiet off; turning it on does
// not, and a failing dispatch fails neither.
func TestTheDrainAndTurningQuietOffDispatchPendingWork(t *testing.T) {
	h := newSchedulerHarness(t)
	server := New(h.repo)
	dispatcher := &countingDispatcher{err: errors.New("no worker answered")}
	server.SetDispatcher(dispatcher)
	automation := createEnabled(t, h.repo, h.ctx, "Digest", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	if _, found, err := h.repo.ClaimDueAutomation(h.ctx, parseTime(t, automation.NextDueAt), schedulerDefaults); err != nil || !found {
		t.Fatalf("fire = %v, %v", found, err)
	}

	setQuiet(t, h.ctx, server, true, 0)
	if got := dispatcher.count(); got != 0 {
		t.Fatalf("dispatches after turning quiet on = %d, want none", got)
	}
	if err := server.DrainUnattendedQuiet(h.ctx); err != nil {
		t.Fatalf("drain with a failing dispatch = %v, want it finished", err)
	}
	if got := dispatcher.count(); got != 1 {
		t.Fatalf("dispatches after the drain = %d, want one, after the batch that cancelled", got)
	}
	if err := server.DrainUnattendedQuiet(h.ctx); err != nil || dispatcher.count() != 1 {
		t.Fatalf("a drain with nothing to do = %v with %d dispatches, want none more", err, dispatcher.count())
	}
	setQuiet(t, h.ctx, server, false, 1)
	if got := dispatcher.count(); got != 2 {
		t.Fatalf("dispatches after turning quiet off = %d, want one more", got)
	}
	setQuiet(t, h.ctx, server, false, 2)
	if got := dispatcher.count(); got != 2 {
		t.Fatalf("dispatches after a no-op write = %d, want none more", got)
	}
}

// A batch that fails rolls back whole and the duty reports it; the drain stays
// pending, so the next tick's run of the duty finishes it.
func TestAFailingDrainBatchIsRetriedOnTheNextTick(t *testing.T) {
	h := newSchedulerHarness(t)
	server := New(h.repo)
	runs := queueAutomationRuns(t, h, 1)
	setQuiet(t, h.ctx, server, true, 0)
	if _, err := h.database.ExecContext(h.ctx, `
		CREATE TRIGGER refuse_quiet_cancel BEFORE UPDATE ON agent_runs
		WHEN NEW.cancellation_reason = 'owner_quiet'
		BEGIN SELECT RAISE(ABORT, 'injected cancellation failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := server.DrainUnattendedQuiet(h.ctx); err == nil {
		t.Fatal("a failing batch reported no error")
	}
	if runStatus, _ := quietRunStatus(t, h.repo, h.ctx, runs[0]); runStatus != "queued" ||
		h.count(t, `SELECT COUNT(*) FROM settings WHERE key = 'unattended_quiet_drain'`) != 1 {
		t.Fatalf("after a failed batch the run is %s, want it still queued with the drain pending", runStatus)
	}
	if _, err := h.database.ExecContext(h.ctx, `DROP TRIGGER refuse_quiet_cancel`); err != nil {
		t.Fatal(err)
	}
	if err := server.DrainUnattendedQuiet(h.ctx); err != nil {
		t.Fatal(err)
	}
	if runStatus, code := quietRunStatus(t, h.repo, h.ctx, runs[0]); runStatus != "cancelled" || code != runoutcome.CodeOwnerQuiet {
		t.Fatalf("after the retry the run is %s/%s, want cancelled owner_quiet", runStatus, code)
	}
}
