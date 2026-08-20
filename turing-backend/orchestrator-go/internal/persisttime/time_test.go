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
		{
			name:  "negative_offset_landing_exactly_on_the_upper_bound",
			value: "9999-12-31T22:59:59.999999999-01:00",
			want:  time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC),
		},
		{
			name:  "positive_offset_landing_exactly_on_the_lower_bound",
			value: "0001-01-01T01:00:00+01:00",
			want:  time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC),
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

// The offset is two numeric fields, not four free digits. RFC 3339 bounds them
// at 00-23 hours and 00-59 minutes, and time.Parse does not: it happily reads
// +24:00 and +00:60 and silently renormalizes them into a neighbouring day or
// hour. A legacy row carrying one of those is a corrupt row, and accepting it
// writes a real-looking instant that no writer ever meant, so the shape check
// has to bound both fields before the calendar is consulted.
func TestParseLegacyRejectsOutOfRangeOffsetFieldsValueFree(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "offset_hour_twenty_four_positive", value: "2030-01-02T03:04:05.000000000+24:00"},
		{name: "offset_hour_twenty_four_negative", value: "2030-01-02T03:04:05.000000000-24:00"},
		{name: "offset_minute_sixty_positive", value: "2030-01-02T03:04:05.000000000+00:60"},
		{name: "offset_minute_sixty_negative", value: "2030-01-02T03:04:05.000000000-00:60"},
		{name: "offset_hour_twenty_four_whole_second", value: "2030-01-02T03:04:05+24:00"},
		{name: "offset_minute_sixty_whole_second", value: "2030-01-02T03:04:05-00:60"},
		{name: "offset_both_fields_out_of_range", value: "2030-01-02T03:04:05+24:60"},
		{name: "offset_hour_ninety_nine", value: "2030-01-02T03:04:05+99:00"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseLegacy(test.value)
			if err == nil {
				t.Fatalf("ParseLegacy(%q) = %s, want an out-of-range offset error", test.value, got)
			}
			if !errors.Is(err, ErrInvalidTimestamp) {
				t.Fatalf("ParseLegacy(%q) = %v, want ErrInvalidTimestamp", test.value, err)
			}
			if err.Error() != ErrInvalidTimestamp.Error() {
				t.Fatalf("error = %q, want exactly the sentinel %q", err, ErrInvalidTimestamp)
			}
			if strings.Contains(err.Error(), test.value) {
				t.Fatalf("error %q leaked the parsed value", err)
			}
			if !got.IsZero() {
				t.Fatalf("ParseLegacy(%q) returned %s with an error, want the zero time", test.value, got)
			}
		})
	}
}

// Bounding the offset fields must not narrow the contract to Zulu-only. The
// extreme legal offsets are the ones a tightening is most likely to take out by
// accident, so both are pinned with the exact UTC instant they normalize to.
func TestParseLegacyKeepsTheExtremeLegalOffsets(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  time.Time
	}{
		{
			name:  "maximum_positive_offset",
			value: "2030-01-02T03:04:05.000000000+23:59",
			want:  time.Date(2030, 1, 1, 3, 5, 5, 0, time.UTC),
		},
		{
			name:  "maximum_negative_offset",
			value: "2030-01-02T03:04:05.000000000-23:59",
			want:  time.Date(2030, 1, 3, 3, 3, 5, 0, time.UTC),
		},
		{
			name:  "maximum_offset_hour_with_zero_minutes",
			value: "2030-01-02T03:04:05-23:00",
			want:  time.Date(2030, 1, 3, 2, 4, 5, 0, time.UTC),
		},
		{
			name:  "maximum_offset_minute_with_zero_hours",
			value: "2030-01-02T03:04:05+00:59",
			want:  time.Date(2030, 1, 2, 2, 5, 5, 0, time.UTC),
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

// An approved shape is not automatically an approved instant. A legacy offset
// value can sit inside the shape and still normalize outside the inclusive UTC
// range the contract allows, and the row it would produce is unusable: year
// 10000 renders a five-digit year, which is wider than every other persisted
// timestamp and therefore sorts wrong in a text-compared column.
func TestParseLegacyRejectsInstantsOutsideTheApprovedRangeValueFree(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "offset_normalizes_past_the_upper_bound", value: "9999-12-31T23:59:59.999999999-01:00"},
		{name: "smallest_offset_crossing_the_upper_bound", value: "9999-12-31T23:59:59.999999999-00:01"},
		{name: "whole_second_offset_crossing_the_upper_bound", value: "9999-12-31T23:00:00-14:00"},
		{name: "offset_normalizes_before_the_lower_bound", value: "0001-01-01T00:00:00+01:00"},
		{name: "smallest_offset_crossing_the_lower_bound", value: "0001-01-01T00:00:00.000000000+00:01"},
		{name: "year_zero_zulu", value: "0000-12-31T23:59:59.999999999Z"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseLegacy(test.value)
			if err == nil {
				t.Fatalf("ParseLegacy(%q) = %s, want an out-of-range error", test.value, got)
			}
			if !errors.Is(err, ErrInvalidTimestamp) {
				t.Fatalf("ParseLegacy(%q) = %v, want ErrInvalidTimestamp", test.value, err)
			}
			if err.Error() != ErrInvalidTimestamp.Error() || strings.Contains(err.Error(), test.value) {
				t.Fatalf("error = %q, want exactly the value-free sentinel %q", err, ErrInvalidTimestamp)
			}
			if !got.IsZero() {
				t.Fatalf("ParseLegacy(%q) returned %s with an error, want the zero time", test.value, got)
			}
		})
	}
}

// The parser and the formatter are one contract: anything ParseLegacy accepts
// must come back out of Format as a fixed-width canonical string that reparses
// to the same instant. Offsets are the interesting input here, because they are
// the only accepted shape that can move an instant across a range bound.
func TestParseLegacyAcceptedValuesFormatFixedWidthAndRoundTrip(t *testing.T) {
	values := []string{
		"2030-01-02T03:04:05Z",
		"2030-01-02T03:04:05.1Z",
		"2030-01-02T03:04:05.123456789Z",
		"2030-01-02T03:04:05.000000001Z",
		"2030-01-02T03:04:05+00:00",
		"2030-01-02T03:04:05.5-07:00",
		"2030-01-02T03:04:05.5+05:30",
		"0001-01-01T00:00:00.000000000Z",
		"9999-12-31T23:59:59.999999999Z",
		"0001-01-01T01:00:00+01:00",
		"9999-12-31T22:59:59.999999999-01:00",
		// Rejected below, but listed so an accepting regression has to prove
		// the canonical rendering still holds instead of silently widening it.
		"0001-01-01T00:00:00+01:00",
		"0001-01-01T00:00:00.000000000+00:01",
		"9999-12-31T23:59:59.999999999-00:01",
		"9999-12-31T23:59:59.999999999-01:00",
		"9999-12-31T23:00:00-14:00",
	}
	for _, value := range values {
		t.Run(value, func(t *testing.T) {
			parsed, err := ParseLegacy(value)
			if err != nil {
				if !errors.Is(err, ErrInvalidTimestamp) {
					t.Fatalf("ParseLegacy(%q) = %v, want ErrInvalidTimestamp", value, err)
				}
				return
			}
			canonical := Format(parsed)
			if len(canonical) != len(layout) {
				t.Fatalf("Format(ParseLegacy(%q)) = %q with width %d, want the canonical width %d",
					value, canonical, len(canonical), len(layout))
			}
			reparsed, err := ParseLegacy(canonical)
			if err != nil {
				t.Fatalf("ParseLegacy(Format(ParseLegacy(%q))) = %v, want a round trip", value, err)
			}
			if !reparsed.Equal(parsed) {
				t.Fatalf("round trip of %q = %s, want %s", value, reparsed, parsed)
			}
			if Format(reparsed) != canonical {
				t.Fatalf("round trip of %q reformatted to %q, want %q", value, Format(reparsed), canonical)
			}
		})
	}
}

// Fixed width is what makes lexical SQLite comparison agree with chronological
// order, so a shorter or longer rendering is a correctness bug, not cosmetics.
func TestFormatUsesFixedWidthUTCNanoseconds(t *testing.T) {
	base := time.Date(2030, 1, 2, 3, 4, 5, 0, time.FixedZone("non-UTC", -7*60*60))
	// The fixture is in chronological order on purpose: the loop below compares
	// every consecutive canonical rendering, so the list is what proves lexical
	// order still agrees with time order across a nanosecond step and across
	// the era boundaries at both ends of the range.
	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		{name: "lower_range_bound", at: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), want: "0001-01-01T00:00:00.000000000Z"},
		{name: "lower_range_bound_plus_one_nanosecond", at: time.Date(1, 1, 1, 0, 0, 0, 1, time.UTC), want: "0001-01-01T00:00:00.000000001Z"},
		{name: "whole_second_from_a_non_utc_zone", at: base, want: "2030-01-02T10:04:05.000000000Z"},
		{name: "first_nanosecond", at: base.Add(time.Nanosecond), want: "2030-01-02T10:04:05.000000001Z"},
		{name: "tenth_of_a_second", at: base.Add(100 * time.Millisecond), want: "2030-01-02T10:04:05.100000000Z"},
		{name: "upper_range_bound_minus_one_nanosecond", at: time.Date(9999, 12, 31, 23, 59, 59, 999999998, time.UTC), want: "9999-12-31T23:59:59.999999998Z"},
		{name: "upper_range_bound", at: time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC), want: "9999-12-31T23:59:59.999999999Z"},
	}
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

	// Compare the rendered output rather than the wanted strings, so a layout
	// that drops or widens the fraction fails here and not only on equality.
	for index := 1; index < len(tests); index++ {
		previous, current := tests[index-1], tests[index]
		if !previous.at.Before(current.at) {
			t.Fatalf("fixture %s is not before %s; the chronological assertion below proves nothing",
				previous.name, current.name)
		}
		if formatted, wanted := Format(previous.at), Format(current.at); formatted >= wanted {
			t.Fatalf("timestamp order %q (%s) then %q (%s) is not chronological",
				formatted, previous.name, wanted, current.name)
		}
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
