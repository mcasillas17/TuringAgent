package automations

import (
	"context"
	"errors"
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
// the revision that repairs it.
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
	repaired := setQuiet(t, ctx, server, false, 6)
	if !proto.Equal(repaired.GetSetting(), &turingv1.UnattendedQuiet{Revision: 7}) || repaired.GetStale() {
		t.Fatalf("repair = %v, want off at revision 7", repaired)
	}
}

// The maintenance duty finishes a drain: the queued automation run is
// cancelled owner_quiet and its cancellation is published, and a duty
// interrupted between batches is finished by the next one.
func TestTheDrainDutyCancelsQueuedRunsAndPublishesThem(t *testing.T) {
	h := newSchedulerHarness(t)
	server := New(h.repo)
	publisher := &recordingPublisher{}
	server.SetEventPublisher(publisher)
	runs := make([]string, 0, 2)
	for _, name := range []string{"Digest", "Inbox"} {
		automation := createEnabled(t, h.repo, h.ctx, name, repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
		fire, found, err := h.repo.ClaimDueAutomation(h.ctx, parseTime(t, automation.NextDueAt), schedulerDefaults)
		if err != nil || !found {
			t.Fatalf("fire %s = %v, %v", name, found, err)
		}
		runs = append(runs, fire.RunID)
	}
	setQuiet(t, h.ctx, server, true, 0)

	interrupted, interrupt := context.WithCancel(h.ctx)
	server.quietDrainBatchDone = func(int) { interrupt() }
	if err := server.DrainUnattendedQuiet(interrupted); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted drain = %v, want it cut short", err)
	}
	server.quietDrainBatchDone = nil
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
