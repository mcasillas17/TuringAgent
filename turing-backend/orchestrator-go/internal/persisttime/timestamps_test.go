package persisttime

import (
	"errors"
	"testing"
	"time"
)

func TestFormatAndParseCanonicalRoundTrip(t *testing.T) {
	value := time.Date(2026, time.August, 20, 4, 0, 0, 123, time.FixedZone("offset", -7*60*60))
	const want = "2026-08-20T11:00:00.000000123Z"

	if got := Format(value); got != want {
		t.Fatalf("Format = %q, want %q", got, want)
	}
	parsed, err := ParseCanonical(want)
	if err != nil {
		t.Fatalf("ParseCanonical: %v", err)
	}
	if got := Format(parsed); got != want {
		t.Fatalf("parsed round trip = %q, want %q", got, want)
	}
}

func TestParseCanonicalRejectsAlternateRepresentations(t *testing.T) {
	for _, value := range []string{
		"2026-08-20T04:00:00Z",
		"2026-08-20T04:00:00.1Z",
		"2026-08-20T04:00:00.000000000+00:00",
		"2026-02-30T04:00:00.000000000Z",
		"not-a-time",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := ParseCanonical(value); !errors.Is(err, ErrInvalidTimestamp) {
				t.Fatalf("ParseCanonical(%q) error = %v, want ErrInvalidTimestamp", value, err)
			}
		})
	}
}

func TestParseLegacyAcceptsRFC3339NanoAndFormatsCanonicalUTC(t *testing.T) {
	for value, want := range map[string]string{
		"2026-08-20T04:00:00Z":                "2026-08-20T04:00:00.000000000Z",
		"2026-08-20T04:00:00.12Z":             "2026-08-20T04:00:00.120000000Z",
		"2026-08-19T21:00:00.000000001-07:00": "2026-08-20T04:00:00.000000001Z",
		"2026-08-20T04:00:00.000000002Z":      "2026-08-20T04:00:00.000000002Z",
	} {
		t.Run(value, func(t *testing.T) {
			parsed, err := ParseLegacy(value)
			if err != nil {
				t.Fatalf("ParseLegacy(%q): %v", value, err)
			}
			if got := Format(parsed); got != want {
				t.Fatalf("Format(ParseLegacy(%q)) = %q, want %q", value, got, want)
			}
		})
	}
}

func TestParseLegacyRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{
		"2026-02-30T04:00:00Z",
		"2026-08-20 04:00:00Z",
		"2026-08-20T04:00:00",
		"2026-08-20T04:00:00.0000000000Z",
		"2026-08-20T04:00:00Ztrailing",
	} {
		t.Run(value, func(t *testing.T) {
			if _, err := ParseLegacy(value); !errors.Is(err, ErrInvalidTimestamp) {
				t.Fatalf("ParseLegacy(%q) error = %v, want ErrInvalidTimestamp", value, err)
			}
		})
	}
}
