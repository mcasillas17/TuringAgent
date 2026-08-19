package repository

import (
	"errors"
	"testing"
	"time"
)

// Saving is not the same as asking for it now: a five-minute automation that
// fired on every edit would be a surprise every time you touched it.
func TestFirstDueWaitsAFullInterval(t *testing.T) {
	from := time.Date(2026, 8, 18, 9, 17, 30, 0, time.UTC)
	schedule := Schedule{Kind: ScheduleInterval, Interval: 5 * time.Minute}

	got, err := schedule.FirstDueAfter(from)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(from.Add(5 * time.Minute)) {
		t.Fatalf("first due = %s, want %s", got, from.Add(5*time.Minute))
	}
}

func TestFirstDailyDueIsTodayIfStillAheadAndTomorrowOtherwise(t *testing.T) {
	schedule := Schedule{Kind: ScheduleDaily, DailyMinuteUTC: 7*60 + 30}

	morning := time.Date(2026, 8, 18, 6, 0, 0, 0, time.UTC)
	got, err := schedule.FirstDueAfter(morning)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(time.Date(2026, 8, 18, 7, 30, 0, 0, time.UTC)) {
		t.Fatalf("first due from 06:00 = %s, want today 07:30Z", got)
	}

	afternoon := time.Date(2026, 8, 18, 15, 0, 0, 0, time.UTC)
	got, err = schedule.FirstDueAfter(afternoon)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(time.Date(2026, 8, 19, 7, 30, 0, 0, time.UTC)) {
		t.Fatalf("first due from 15:00 = %s, want tomorrow 07:30Z", got)
	}

	// Exactly on the minute schedules tomorrow, not this same instant again —
	// otherwise the row stays due forever and the scheduler spins.
	onTheMinute := time.Date(2026, 8, 18, 7, 30, 0, 0, time.UTC)
	got, err = schedule.FirstDueAfter(onTheMinute)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(time.Date(2026, 8, 19, 7, 30, 0, 0, time.UTC)) {
		t.Fatalf("first due from exactly 07:30 = %s, want tomorrow", got)
	}
}

// The next due time is always strictly in the future. If it were not, the
// automation would be due again the moment it finished firing.
func TestNextDueAfterFiringIsAlwaysAhead(t *testing.T) {
	schedule := Schedule{Kind: ScheduleInterval, Interval: time.Hour}
	firedDue := time.Date(2026, 8, 18, 1, 0, 0, 0, time.UTC)

	onTime, err := schedule.NextDueAfterFiring(firedDue, firedDue)
	if err != nil {
		t.Fatal(err)
	}
	if !onTime.Equal(firedDue.Add(time.Hour)) {
		t.Fatalf("on-time next = %s, want %s", onTime, firedDue.Add(time.Hour))
	}

	// Three hours of downtime produces ONE run and one next time, not three
	// catch-up runs nobody asked for.
	lateNow := firedDue.Add(3*time.Hour + 10*time.Minute)
	late, err := schedule.NextDueAfterFiring(firedDue, lateNow)
	if err != nil {
		t.Fatal(err)
	}
	if !late.After(lateNow) {
		t.Fatalf("late next = %s, is not after %s", late, lateNow)
	}
	if !late.Equal(time.Date(2026, 8, 18, 5, 0, 0, 0, time.UTC)) {
		t.Fatalf("late next = %s, want the first hourly slot after wake-up (05:00Z)", late)
	}
}

func TestNextDailyDueAfterFiringIsTheFollowingDay(t *testing.T) {
	schedule := Schedule{Kind: ScheduleDaily, DailyMinuteUTC: 30}
	firedDue := time.Date(2026, 8, 18, 0, 30, 0, 0, time.UTC)

	got, err := schedule.NextDueAfterFiring(firedDue, firedDue)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(time.Date(2026, 8, 19, 0, 30, 0, 0, time.UTC)) {
		t.Fatalf("next daily = %s, want 2026-08-19T00:30Z", got)
	}
}

func TestScheduleValidationRejectsWhatCannotBeKept(t *testing.T) {
	cases := []struct {
		name     string
		schedule Schedule
		want     error
	}{
		{"unset", Schedule{}, ErrScheduleKindUnknown},
		{"under a minute", Schedule{Kind: ScheduleInterval, Interval: 59 * time.Second}, ErrScheduleIntervalRange},
		{"over a week", Schedule{Kind: ScheduleInterval, Interval: 7*24*time.Hour + time.Minute}, ErrScheduleIntervalRange},
		{"minute 1440", Schedule{Kind: ScheduleDaily, DailyMinuteUTC: minutesPerDay}, ErrScheduleDailyMinute},
		{"a minute is fine", Schedule{Kind: ScheduleInterval, Interval: time.Minute}, nil},
		{"a week is fine", Schedule{Kind: ScheduleInterval, Interval: 7 * 24 * time.Hour}, nil},
		{"midnight is fine", Schedule{Kind: ScheduleDaily, DailyMinuteUTC: 0}, nil},
		{"23:59 is fine", Schedule{Kind: ScheduleDaily, DailyMinuteUTC: minutesPerDay - 1}, nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.schedule.Validate()
			if testCase.want == nil {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Validate = %v, want %v", err, testCase.want)
			}
		})
	}
}

// The allowlist has no wildcard, and this is where that is asserted rather
// than assumed.
func TestGrantAllowsOnlyTheExactServerAndToolNamed(t *testing.T) {
	grant := AutomationRunGrant{
		AutomationID:   "auto_1",
		AutomationName: "Digest",
		AllowedTools:   []AutomationTool{{ServerName: "files", ToolName: "files.create"}},
	}

	if !grant.Allows("files", "files.create") {
		t.Fatal("the named tool is not allowed")
	}
	if grant.Allows("files", "files.update") {
		t.Fatal("a sibling tool on the same server is allowed")
	}
	if grant.Allows("other", "files.create") {
		t.Fatal("the same tool name on another server is allowed")
	}
	if grant.Allows("*", "*") {
		t.Fatal("a wildcard is honoured as a wildcard")
	}
	if (AutomationRunGrant{}).Allows("files", "files.create") {
		t.Fatal("an empty allowlist allows something")
	}
}
