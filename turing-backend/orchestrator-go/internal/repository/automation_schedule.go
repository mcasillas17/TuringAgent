package repository

import (
	"errors"
	"time"
)

// ScheduleKind is deliberately a closed set of two. A cron expression would be
// more expressive, and being sure it is correct costs a parser and a test
// suite of its own; nothing here needs that yet, and a schedule the user
// cannot read back is a schedule they cannot trust.
type ScheduleKind string

const (
	ScheduleInterval ScheduleKind = "interval"
	ScheduleDaily    ScheduleKind = "daily"
)

const (
	// A minute is the floor because the scheduler ticks on a timer: anything
	// finer would be a schedule the process cannot actually keep.
	MinAutomationInterval = time.Minute
	MaxAutomationInterval = 7 * 24 * time.Hour
	minutesPerDay         = 24 * 60
)

var (
	ErrScheduleKindUnknown    = errors.New("schedule must be an interval or a daily time")
	ErrScheduleIntervalRange  = errors.New("interval must be between 1 minute and 7 days")
	ErrScheduleDailyMinute    = errors.New("daily time must be a minute of the day")
	ErrScheduleNotComputable  = errors.New("schedule has no next occurrence")
	errScheduleColumnsInvalid = errors.New("stored schedule is not valid")
)

// Schedule is when an automation fires. Exactly one of the two fields is
// meaningful, decided by Kind.
type Schedule struct {
	Kind ScheduleKind

	// Interval only.
	Interval time.Duration

	// Daily only: minutes past midnight, UTC.
	DailyMinuteUTC int
}

func (s Schedule) Validate() error {
	switch s.Kind {
	case ScheduleInterval:
		if s.Interval < MinAutomationInterval || s.Interval > MaxAutomationInterval {
			return ErrScheduleIntervalRange
		}
		return nil
	case ScheduleDaily:
		if s.DailyMinuteUTC < 0 || s.DailyMinuteUTC >= minutesPerDay {
			return ErrScheduleDailyMinute
		}
		return nil
	default:
		return ErrScheduleKindUnknown
	}
}

// FirstDueAfter is when a newly created or newly enabled automation should
// first fire.
//
// An interval automation waits a full interval rather than firing the instant
// it is saved: saving is not the same as asking for it now, and a five-minute
// automation that fires on every edit is a surprise.
func (s Schedule) FirstDueAfter(from time.Time) (time.Time, error) {
	if err := s.Validate(); err != nil {
		return time.Time{}, err
	}
	from = from.UTC()
	switch s.Kind {
	case ScheduleInterval:
		return from.Add(s.Interval), nil
	case ScheduleDaily:
		return nextDailyAfter(from, s.DailyMinuteUTC), nil
	default:
		return time.Time{}, ErrScheduleKindUnknown
	}
}

// NextDueAfterFiring is the schedule advanced past a firing that has just
// happened.
//
// Missed occurrences are skipped rather than replayed. If the process was down
// for three hours, an hourly automation fires once and is then due an hour
// from now — not three times in a row, which is a burst nobody asked for and
// which no unattended allowlist was granted with in mind.
func (s Schedule) NextDueAfterFiring(firedDue time.Time, now time.Time) (time.Time, error) {
	if err := s.Validate(); err != nil {
		return time.Time{}, err
	}
	firedDue, now = firedDue.UTC(), now.UTC()
	switch s.Kind {
	case ScheduleInterval:
		next := firedDue.Add(s.Interval)
		if !next.After(now) {
			// Jump straight to the first occurrence after now instead of
			// looping: an automation whose next_due_at is years stale would
			// otherwise spin here once per missed interval.
			missed := now.Sub(next)/s.Interval + 1
			next = next.Add(time.Duration(missed) * s.Interval)
		}
		return next, nil
	case ScheduleDaily:
		return nextDailyAfter(now, s.DailyMinuteUTC), nil
	default:
		return time.Time{}, ErrScheduleKindUnknown
	}
}

// nextDailyAfter is the next instant strictly after from at the given minute
// of the UTC day. Strictly after, so a firing that lands exactly on the minute
// schedules tomorrow rather than immediately re-arming for the same instant.
func nextDailyAfter(from time.Time, minuteOfDay int) time.Time {
	from = from.UTC()
	midnight := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	candidate := midnight.Add(time.Duration(minuteOfDay) * time.Minute)
	if !candidate.After(from) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate
}
