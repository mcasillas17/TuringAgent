package persisttime

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseLegacyAcceptsApprovedRFC3339NanoShapes(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Time
	}{
		{
			name:  "whole_second_zulu",
			value: "2030-01-02T03:04:05Z",
			want:  time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
		},
		{
			name:  "one_fraction_digit_zulu",
			value: "2030-01-02T03:04:05.1Z",
			want:  time.Date(2030, 1, 2, 3, 4, 5, 100000000, time.UTC),
		},
		{
			name:  "nine_fraction_digits_zulu",
			value: "2030-01-02T03:04:05.123456789Z",
			want:  time.Date(2030, 1, 2, 3, 4, 5, 123456789, time.UTC),
		},
		{
			name:  "canonical_fixed_width",
			value: "2030-01-02T03:04:05.000000001Z",
			want:  time.Date(2030, 1, 2, 3, 4, 5, 1, time.UTC),
		},
		{
			name:  "zero_offset",
			value: "2030-01-02T03:04:05+00:00",
			want:  time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
		},
		{
			name:  "negative_offset_normalizes_to_utc",
			value: "2030-01-02T03:04:05.5-07:00",
			want:  time.Date(2030, 1, 2, 10, 4, 5, 500000000, time.UTC),
		},
		{
			name:  "half_hour_positive_offset_normalizes_to_utc",
			value: "2030-01-02T03:04:05.5+05:30",
			want:  time.Date(2030, 1, 1, 21, 34, 5, 500000000, time.UTC),
		},
		{
			name:  "lower_range_bound",
			value: "0001-01-01T00:00:00.000000000Z",
			want:  time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:  "upper_range_bound",
			value: "9999-12-31T23:59:59.999999999Z",
			want:  time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseLegacy(test.value)
			if err != nil {
				t.Fatalf("ParseLegacy(%q) error: %v", test.value, err)
			}
			if !got.Equal(test.want) {
				t.Fatalf("ParseLegacy(%q) = %s, want %s", test.value, got, test.want)
			}
			if got.Location() != time.UTC {
				t.Fatalf("ParseLegacy(%q) location = %s, want UTC", test.value, got.Location())
			}
		})
	}
}

// Rejected shapes fail with one sentinel that never echoes the value: these
// strings come from legacy rows the migration reads, and a parser error is not
// a safe place to print database contents.
func TestParseLegacyRejectsVariableOrInvalidShapesValueFree(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "missing_zone", value: "2030-01-02T03:04:05"},
		{name: "space_separator", value: "2030-01-02 03:04:05Z"},
		{name: "lowercase_designators", value: "2030-01-02t03:04:05z"},
		{name: "empty_fraction", value: "2030-01-02T03:04:05.Z"},
		{name: "ten_fraction_digits", value: "2030-01-02T03:04:05.1234567890Z"},
		{name: "offset_without_colon", value: "2030-01-02T03:04:05+0000"},
		{name: "trailing_space", value: "2030-01-02T03:04:05Z "},
		{name: "leading_space", value: " 2030-01-02T03:04:05Z"},
		{name: "trailing_garbage", value: "2030-01-02T03:04:05Zjunk"},
		{name: "double_zone", value: "2030-01-02T03:04:05.123456789Z+01:00"},
		{name: "two_digit_year", value: "30-01-02T03:04:05Z"},
		{name: "month_thirteen", value: "2030-13-02T03:04:05Z"},
		{name: "day_thirty_two", value: "2030-01-32T03:04:05Z"},
		{name: "hour_twenty_four", value: "2030-01-02T24:04:05Z"},
		{name: "second_sixty", value: "2030-01-02T03:04:60Z"},
		{name: "unix_seconds", value: "1893553445"},
		{name: "date_only", value: "2030-01-02"},
		{name: "sql_datetime", value: "2030-01-02 03:04:05"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseLegacy(test.value)
			if err == nil {
				t.Fatalf("ParseLegacy(%q) = %s, want an error", test.value, got)
			}
			if !errors.Is(err, ErrInvalidTimestamp) {
				t.Fatalf("ParseLegacy(%q) = %v, want ErrInvalidTimestamp", test.value, err)
			}
			if err.Error() != ErrInvalidTimestamp.Error() {
				t.Fatalf("error = %q, want exactly the sentinel %q", err, ErrInvalidTimestamp)
			}
			if test.value != "" && strings.Contains(err.Error(), test.value) {
				t.Fatalf("error %q leaked the parsed value", err)
			}
			if !got.IsZero() {
				t.Fatalf("ParseLegacy(%q) returned %s with an error, want the zero time", test.value, got)
			}
		})
	}
}

// Fixed width is what makes lexical SQLite comparison agree with chronological
// order, so a shorter or longer rendering is a correctness bug, not cosmetics.
func TestFormatUsesFixedWidthUTCNanoseconds(t *testing.T) {
	base := time.Date(2030, 1, 2, 3, 4, 5, 0, time.FixedZone("non-UTC", -7*60*60))
	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{name: "whole_second_from_a_non_utc_zone", at: base, want: "2030-01-02T10:04:05.000000000Z"},
		{name: "first_nanosecond", at: base.Add(time.Nanosecond), want: "2030-01-02T10:04:05.000000001Z"},
		{name: "tenth_of_a_second", at: base.Add(100 * time.Millisecond), want: "2030-01-02T10:04:05.100000000Z"},
		{name: "lower_range_bound", at: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), want: "0001-01-01T00:00:00.000000000Z"},
		{name: "upper_range_bound", at: time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC), want: "9999-12-31T23:59:59.999999999Z"},
	}
	var previous string
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := Format(test.at)
			if got != test.want {
				t.Fatalf("Format(%s) = %q, want %q", test.at, got, test.want)
			}
			if len(got) != len("2006-01-02T15:04:05.000000000Z") {
				t.Fatalf("Format(%s) = %q, which is not fixed width", test.at, got)
			}
			parsed, err := ParseLegacy(got)
			if err != nil {
				t.Fatalf("ParseLegacy(Format(%s)) error: %v", test.at, err)
			}
			if !parsed.Equal(test.at) {
				t.Fatalf("round trip = %s, want %s", parsed, test.at)
			}
		})
	}
	for index := 1; index < 3; index++ {
		if previous != "" && tests[index].want <= previous {
			t.Fatalf("timestamp order %q then %q is not chronological", previous, tests[index].want)
		}
		previous = tests[index].want
	}
}

// A transition must always move the update time forward, even when the host
// clock stalls or steps backward, because reopen and live streaming compare
// these strings directly.
func TestNextStateTimeUsesNowOrPriorPlusOneNanosecond(t *testing.T) {
	tests := []struct {
		name  string
		now   time.Time
		prior string
		want  string
	}{
		{
			name:  "advancing_clock_uses_now",
			now:   time.Date(2030, 1, 2, 3, 4, 6, 0, time.UTC),
			prior: "2030-01-02T03:04:05.000000000Z",
			want:  "2030-01-02T03:04:06.000000000Z",
		},
		{
			name:  "regressing_clock_uses_prior_plus_one_nanosecond",
			now:   time.Date(2029, 6, 1, 0, 0, 0, 0, time.UTC),
			prior: "2030-01-02T03:04:05.000000000Z",
			want:  "2030-01-02T03:04:05.000000001Z",
		},
		{
			name:  "stalled_clock_uses_prior_plus_one_nanosecond",
			now:   time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
			prior: "2030-01-02T03:04:05.000000000Z",
			want:  "2030-01-02T03:04:05.000000001Z",
		},
		{
			name:  "now_exactly_one_nanosecond_ahead",
			now:   time.Date(2030, 1, 2, 3, 4, 5, 1, time.UTC),
			prior: "2030-01-02T03:04:05.000000000Z",
			want:  "2030-01-02T03:04:05.000000001Z",
		},
		{
			name:  "non_utc_now_is_normalized",
			now:   time.Date(2030, 1, 2, 3, 4, 6, 0, time.FixedZone("non-UTC", -7*60*60)),
			prior: "2030-01-02T03:04:05.000000000Z",
			want:  "2030-01-02T10:04:06.000000000Z",
		},
		{
			name:  "legacy_whole_second_prior",
			now:   time.Date(2029, 6, 1, 0, 0, 0, 0, time.UTC),
			prior: "2030-01-02T03:04:05Z",
			want:  "2030-01-02T03:04:05.000000001Z",
		},
		{
			name:  "legacy_offset_prior",
			now:   time.Date(2029, 6, 1, 0, 0, 0, 0, time.UTC),
			prior: "2030-01-02T03:04:05.5-07:00",
			want:  "2030-01-02T10:04:05.500000001Z",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NextStateTime(test.now, test.prior)
			if err != nil {
				t.Fatalf("NextStateTime(%s, %q) error: %v", test.now, test.prior, err)
			}
			if got != test.want {
				t.Fatalf("NextStateTime(%s, %q) = %q, want %q", test.now, test.prior, got, test.want)
			}
			if got <= test.prior && len(test.prior) == len(got) {
				t.Fatalf("next state time %q did not move past prior %q", got, test.prior)
			}
		})
	}

	t.Run("unparseable_prior_fails_closed_value_free", func(t *testing.T) {
		const prior = "not a timestamp"
		got, err := NextStateTime(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC), prior)
		if err == nil {
			t.Fatalf("NextStateTime(now, %q) = %q, want an error", prior, got)
		}
		if !errors.Is(err, ErrInvalidTimestamp) {
			t.Fatalf("error = %v, want ErrInvalidTimestamp", err)
		}
		if err.Error() != ErrInvalidTimestamp.Error() || strings.Contains(err.Error(), prior) {
			t.Fatalf("error = %q, want exactly the value-free sentinel %q", err, ErrInvalidTimestamp)
		}
		if got != "" {
			t.Fatalf("NextStateTime returned %q with an error, want an empty string", got)
		}
	})
}

// At the top of the representable range there is no next nanosecond, so the
// transition fails closed rather than writing a wrapped or truncated value.
func TestNextStateTimeRejectsUpperBoundOverflow(t *testing.T) {
	tests := []struct {
		name  string
		now   time.Time
		prior string
	}{
		{
			name:  "prior_at_the_upper_bound",
			now:   time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
			prior: "9999-12-31T23:59:59.999999999Z",
		},
		{
			name:  "prior_at_the_upper_bound_with_a_later_clock",
			now:   time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC),
			prior: "9999-12-31T23:59:59.999999999Z",
		},
		{
			name:  "clock_beyond_the_upper_bound",
			now:   time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
			prior: "2030-01-02T03:04:05.000000000Z",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NextStateTime(test.now, test.prior)
			if err == nil {
				t.Fatalf("NextStateTime(%s, %q) = %q, want an overflow error", test.now, test.prior, got)
			}
			if !errors.Is(err, ErrTimestampRangeExhausted) {
				t.Fatalf("error = %v, want ErrTimestampRangeExhausted", err)
			}
			if err.Error() != ErrTimestampRangeExhausted.Error() {
				t.Fatalf("error = %q, want exactly the sentinel %q", err, ErrTimestampRangeExhausted)
			}
			if strings.Contains(err.Error(), test.prior) {
				t.Fatalf("error %q leaked the prior timestamp", err)
			}
			if got != "" {
				t.Fatalf("NextStateTime returned %q with an error, want an empty string", got)
			}
		})
	}
}
