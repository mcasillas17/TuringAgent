package safejson

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDecodeObjectUsesNumber(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(`{"count":9007199254740993}`))
	got, err := DecodeObject(dec)
	if err != nil {
		t.Fatalf("DecodeObject returned error: %v", err)
	}
	if _, ok := got["count"].(json.Number); !ok {
		t.Fatalf("count type = %T, want json.Number", got["count"])
	}
}

func TestNormalizeRejectsNaN(t *testing.T) {
	_, err := Normalize(map[string]any{"bad": math.NaN()})
	if err == nil {
		t.Fatal("expected NaN rejection")
	}
}

func TestMarshalCanonicalSortsKeysAndRejectsUnsafeValues(t *testing.T) {
	got, err := MarshalCanonical(map[string]any{"b": float64(2), "a": "one"})
	if err != nil {
		t.Fatalf("MarshalCanonical returned error: %v", err)
	}
	if string(got) != `{"a":"one","b":2}` {
		t.Fatalf("canonical JSON = %s", got)
	}
	if _, err := MarshalCanonical(map[string]any{"bad": math.NaN()}); err == nil {
		t.Fatal("MarshalCanonical succeeded with NaN")
	}
}

func TestMarshalCanonicalDoesNotHTMLEscapeStrings(t *testing.T) {
	got, err := MarshalCanonical(map[string]any{"content": "<tag>&value"})
	if err != nil {
		t.Fatalf("MarshalCanonical returned error: %v", err)
	}
	if string(got) != `{"content":"<tag>&value"}` {
		t.Fatalf("canonical JSON = %s", got)
	}
}

func TestToStructConvertsObject(t *testing.T) {
	got, err := ToStruct(map[string]any{"ok": true, "nested": map[string]any{"value": "x"}})
	if err != nil {
		t.Fatalf("ToStruct returned error: %v", err)
	}
	if !got.Fields["ok"].GetBoolValue() {
		t.Fatal("ok field was not true")
	}
}

func TestSummaryLimitsBytes(t *testing.T) {
	got := Summary(map[string]any{"value": strings.Repeat("a", 100)}, 20)
	if len(got) > 20 {
		t.Fatalf("summary length = %d, want <= 20", len(got))
	}
}

func TestSummaryTruncatesAtUTF8Boundary(t *testing.T) {
	got := Summary(map[string]any{"emoji": strings.Repeat("🔥", 10)}, 20)
	if !utf8.ValidString(got) {
		t.Fatalf("summary is not valid UTF-8: %q", got)
	}
}
