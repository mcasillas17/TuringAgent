// Package persisttime owns the one persisted timestamp format.
//
// Rows are compared as text in SQLite, so chronological order only survives if
// every writer emits the same fixed-width UTC rendering. A second layout
// constant anywhere else would reintroduce the ordering bug where
// "…:05.1Z" sorts after "…:05.000000001Z". Readers must also cope with the
// variable-width RFC 3339 values older code wrote, which is what ParseLegacy is
// for.
package persisttime

import (
	"errors"
	"regexp"
	"time"
)

// layout is deliberately unexported: callers go through Format so no second
// implementation of the same layout can drift.
const layout = "2006-01-02T15:04:05.000000000Z"

var (
	// ErrInvalidTimestamp names the class of problem without echoing the value.
	// Legacy values come from database rows, and a parser error is not a safe
	// place to print row contents.
	ErrInvalidTimestamp = errors.New("invalid persisted timestamp")
	// ErrTimestampRangeExhausted reports that there is no representable next
	// instant, so the caller must fail closed instead of writing a wrapped or
	// truncated value.
	ErrTimestampRangeExhausted = errors.New("persisted timestamp range exhausted")
)

var (
	minInstant = time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)
	maxInstant = time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC)
)

// approvedShape admits only the RFC 3339 forms this system has ever written:
// four-digit year, uppercase T, seconds, an optional one-to-nine-digit
// fraction, and either Z or a ±HH:MM offset. time.Parse alone is looser than
// that, so the shape is checked before the calendar.
var approvedShape = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?(Z|[+-]\d{2}:\d{2})$`)

// ParseLegacy reads a persisted timestamp in any approved shape and normalizes
// it to UTC.
func ParseLegacy(value string) (time.Time, error) {
	if !approvedShape.MatchString(value) {
		return time.Time{}, ErrInvalidTimestamp
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, ErrInvalidTimestamp
	}
	return parsed.UTC(), nil
}

// Format renders an instant as the canonical fixed-width UTC nanosecond value.
func Format(value time.Time) string {
	return value.UTC().Format(layout)
}

// NextStateTime returns the timestamp a real lifecycle transition must write:
// max(now in UTC, prior + 1ns). A stalled or backward-stepping host clock
// therefore still moves the row forward, which keeps the persisted order
// consistent with the version order clients reconcile on. It fails closed when
// prior cannot be parsed or when no representable next instant exists.
func NextStateTime(now time.Time, prior string) (string, error) {
	priorInstant, err := ParseLegacy(prior)
	if err != nil {
		return "", err
	}
	if !priorInstant.Before(maxInstant) {
		return "", ErrTimestampRangeExhausted
	}
	next := priorInstant.Add(time.Nanosecond)
	if nowUTC := now.UTC(); nowUTC.After(next) {
		next = nowUTC
	}
	if next.Before(minInstant) || next.After(maxInstant) {
		return "", ErrTimestampRangeExhausted
	}
	return Format(next), nil
}
