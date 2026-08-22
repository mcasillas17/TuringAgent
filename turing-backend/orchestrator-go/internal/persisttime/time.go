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

// Layout is the fixed-width UTC representation used for persisted timestamps.
const Layout = "2006-01-02T15:04:05.000000000Z"

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

// approvedShape admits only the RFC 3339 forms the approved contract accepts
// from legacy rows: `YYYY-MM-DDTHH:MM:SS[.1-9 digits]Z` and
// `YYYY-MM-DDTHH:MM:SS[.1-9 digits](+|-)HH:MM`, where the offset fields are
// bounded at 00-23 hours and 00-59 minutes. time.Parse alone is looser than
// that — it reads `+24:00` and `+00:60` and silently renormalizes them into a
// neighbouring day or hour, turning a corrupt row into a plausible instant — so
// the shape is checked before the calendar.
var approvedShape = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?(Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)$`)

// ParseLegacy reads a persisted timestamp in any approved shape and normalizes
// it to UTC.
//
// An approved shape is not yet an approved instant: an offset can carry a
// syntactically fine value outside the inclusive range
// 0001-01-01T00:00:00Z through 9999-12-31T23:59:59.999999999Z, and Format
// cannot render those at the canonical width — year 10000 takes five digits and
// would sort ahead of every other row in a text-compared column. So the
// normalized instant is range-checked here, and the whole class fails with the
// one value-free sentinel.
func ParseLegacy(value string) (time.Time, error) {
	if !approvedShape.MatchString(value) {
		return time.Time{}, ErrInvalidTimestamp
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, ErrInvalidTimestamp
	}
	normalized := parsed.UTC()
	if normalized.Before(minInstant) || normalized.After(maxInstant) {
		return time.Time{}, ErrInvalidTimestamp
	}
	return normalized, nil
}

// Format renders an instant as the canonical fixed-width UTC nanosecond value.
func Format(value time.Time) string {
	return value.UTC().Format(Layout)
}

// ParseCanonical accepts only the exact fixed-width representation emitted by
// Format.
func ParseCanonical(value string) (time.Time, error) {
	parsed, err := ParseLegacy(value)
	if err != nil || Format(parsed) != value {
		return time.Time{}, ErrInvalidTimestamp
	}
	return parsed, nil
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
