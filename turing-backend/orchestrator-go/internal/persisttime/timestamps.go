package persisttime

import (
	"errors"
	"time"
)

const Layout = "2006-01-02T15:04:05.000000000Z"

var ErrInvalidTimestamp = errors.New("invalid persisted timestamp")

func Format(value time.Time) string {
	return value.UTC().Format(Layout)
}

func ParseCanonical(value string) (time.Time, error) {
	parsed, err := time.Parse(Layout, value)
	if err != nil || Format(parsed) != value {
		return time.Time{}, ErrInvalidTimestamp
	}
	return parsed, nil
}

func ParseLegacy(value string) (time.Time, error) {
	if !validRFC3339NanoShape(value) {
		return time.Time{}, ErrInvalidTimestamp
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, ErrInvalidTimestamp
	}
	return parsed, nil
}

func validRFC3339NanoShape(value string) bool {
	if len(value) < len("2006-01-02T15:04:05Z") {
		return false
	}
	zoneStart := len(value) - 1
	if value[zoneStart] != 'Z' {
		zoneStart = len(value) - len("-07:00")
		if zoneStart < 0 || (value[zoneStart] != '+' && value[zoneStart] != '-') {
			return false
		}
	}
	if zoneStart == len("2006-01-02T15:04:05") {
		return true
	}
	fractionStart := len("2006-01-02T15:04:05")
	if zoneStart < fractionStart+2 || zoneStart > fractionStart+10 || value[fractionStart] != '.' {
		return false
	}
	for i := fractionStart + 1; i < zoneStart; i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}
