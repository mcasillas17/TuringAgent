package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// fixedSearchMarkers returns a deterministic marker pair with the exact
// production grammar. Tests use it instead of newSearchSnippetMarkers so that
// expected offsets stay stable and no test depends on crypto/rand.
func fixedSearchMarkers() (string, string) {
	nonce := strings.Repeat("a", 32)
	return "[[TURING-FTS5-SNIPPET-START:v1:" + nonce + "]]",
		"[[TURING-FTS5-SNIPPET-END:v1:" + nonce + "]]"
}

func TestNormalizeSearchScore(t *testing.T) {
	for _, test := range []struct {
		name    string
		raw     float64
		want    float64
		wantErr bool
	}{
		{name: "negative", raw: -1.5, want: 1.5},
		{name: "smallest negative", raw: -1.419354838709677e-06, want: 1.419354838709677e-06},
		{name: "positive zero", raw: 0, want: 0},
		{name: "negative zero", raw: math.Copysign(0, -1), want: 0},
		{name: "positive", raw: 0.1, wantErr: true},
		{name: "nan", raw: math.NaN(), wantErr: true},
		{name: "positive infinity", raw: math.Inf(1), wantErr: true},
		{name: "negative infinity", raw: math.Inf(-1), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeSearchScore(test.raw)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidSearchScore) {
					t.Fatalf("error = %v, want ErrInvalidSearchScore", err)
				}
				if got != 0 {
					t.Fatalf("score = %v on error, want 0", got)
				}
				return
			}
			if err != nil || got != test.want || math.Signbit(got) {
				t.Fatalf("normalizeSearchScore(%v) = %v, %v", test.raw, got, err)
			}
		})
	}
}

// TestNormalizeSearchScoreOrdersWorseMatchesLower pins the direction of the
// negation: SQLite's bm25() is "smaller is better", the public score is
// "larger is better", and nothing in between may reorder equal-ranked rows.
func TestNormalizeSearchScoreOrdersWorseMatchesLower(t *testing.T) {
	best, err := normalizeSearchScore(-1.419354838709677e-06)
	if err != nil {
		t.Fatal(err)
	}
	worst, err := normalizeSearchScore(-8.301886792452831e-07)
	if err != nil {
		t.Fatal(err)
	}
	if !(best > worst) {
		t.Fatalf("best %v is not ranked above worst %v", best, worst)
	}
	tieA, err := normalizeSearchScore(-1e-06)
	if err != nil {
		t.Fatal(err)
	}
	tieB, err := normalizeSearchScore(-1e-06)
	if err != nil {
		t.Fatal(err)
	}
	if tieA != tieB {
		t.Fatalf("identical raw scores diverged: %v != %v", tieA, tieB)
	}
}

func TestNewSearchSnippetMarkersUsesExactASCIIGrammar(t *testing.T) {
	start, end, err := newSearchSnippetMarkers(
		bytes.NewReader(bytes.Repeat([]byte{0xab}, 16)),
	)
	if err != nil {
		t.Fatal(err)
	}
	wantNonce := strings.Repeat("ab", 16)
	if start != "[[TURING-FTS5-SNIPPET-START:v1:"+wantNonce+"]]" {
		t.Fatalf("start = %q", start)
	}
	if end != "[[TURING-FTS5-SNIPPET-END:v1:"+wantNonce+"]]" {
		t.Fatalf("end = %q", end)
	}
	if len(start) != 65 || len(end) != 63 || start == end {
		t.Fatalf("marker lengths/distinctness = %d/%d/%v", len(start), len(end), start == end)
	}
	if strings.IndexByte(start, 0) >= 0 || strings.IndexByte(end, 0) >= 0 {
		t.Fatal("marker contains NUL")
	}
	for _, marker := range []string{start, end} {
		for i := 0; i < len(marker); i++ {
			if marker[i] < 0x20 || marker[i] > 0x7e {
				t.Fatalf("marker %q has non-printable ASCII byte %#x at %d", marker, marker[i], i)
			}
		}
	}
	// The two markers must never be substrings of one another, or a complete
	// marker could be accepted in the wrong parser state.
	if strings.Contains(start, end) || strings.Contains(end, start) {
		t.Fatalf("markers overlap: %q / %q", start, end)
	}
}

// TestNewSearchSnippetMarkersConsumesExactlySixteenEntropyBytes proves the
// nonce comes from 128 bits and that the helper does not read past them.
func TestNewSearchSnippetMarkersConsumesExactlySixteenEntropyBytes(t *testing.T) {
	entropy := make([]byte, 20)
	for i := range entropy {
		entropy[i] = byte(i)
	}
	reader := bytes.NewReader(entropy)
	start, end, err := newSearchSnippetMarkers(reader)
	if err != nil {
		t.Fatal(err)
	}
	if reader.Len() != 4 {
		t.Fatalf("unread entropy = %d, want 4", reader.Len())
	}
	wantNonce := "000102030405060708090a0b0c0d0e0f"
	if start != "[[TURING-FTS5-SNIPPET-START:v1:"+wantNonce+"]]" ||
		end != "[[TURING-FTS5-SNIPPET-END:v1:"+wantNonce+"]]" {
		t.Fatalf("markers = %q / %q", start, end)
	}
}

// shortEntropyReader hands back fewer bytes than requested and then fails, the
// way a drained or broken random source does.
type shortEntropyReader struct {
	data []byte
	err  error
}

func (r *shortEntropyReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestNewSearchSnippetMarkersRequiresFullEntropyRead(t *testing.T) {
	reader := &shortEntropyReader{
		data: []byte("SECRETENTROPY15"),
		err:  errors.New("random source drained"),
	}
	start, end, err := newSearchSnippetMarkers(reader)
	if !errors.Is(err, ErrSearchMarkerEntropy) {
		t.Fatalf("error = %v, want ErrSearchMarkerEntropy", err)
	}
	if start != "" || end != "" {
		t.Fatalf("markers on failure = %q / %q, want empty", start, end)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error leaks entropy bytes: %q", err.Error())
	}
}

func TestParseMarkedSearchSnippet(t *testing.T) {
	start := "[[TURING-FTS5-SNIPPET-START:v1:" + strings.Repeat("a", 32) + "]]"
	end := "[[TURING-FTS5-SNIPPET-END:v1:" + strings.Repeat("a", 32) + "]]"

	valid := []byte("before " + start + "needle" + end + " middle " +
		start + "second" + end + " after")
	parsed, err := parseMarkedSearchSnippet(valid, start, end)
	if err != nil {
		t.Fatal(err)
	}
	if string(parsed.text) != "before needle middle second after" {
		t.Fatalf("text = %q", parsed.text)
	}
	if !reflect.DeepEqual(parsed.matches, []byteSpan{{7, 13}, {21, 27}}) {
		t.Fatalf("matches = %+v", parsed.matches)
	}
}

// TestParseMarkedSearchSnippetHandlesEdgeAndAdjacentPairs covers a match that
// is the whole snippet and two pairs that touch with no text between them.
func TestParseMarkedSearchSnippetHandlesEdgeAndAdjacentPairs(t *testing.T) {
	start, end := fixedSearchMarkers()

	whole, err := parseMarkedSearchSnippet([]byte(start+"only"+end), start, end)
	if err != nil {
		t.Fatal(err)
	}
	if string(whole.text) != "only" ||
		!reflect.DeepEqual(whole.matches, []byteSpan{{0, 4}}) {
		t.Fatalf("whole = %q %+v", whole.text, whole.matches)
	}

	adjacent, err := parseMarkedSearchSnippet(
		[]byte(start+"ab"+end+start+"cd"+end), start, end)
	if err != nil {
		t.Fatal(err)
	}
	if string(adjacent.text) != "abcd" ||
		!reflect.DeepEqual(adjacent.matches, []byteSpan{{0, 2}, {2, 4}}) {
		t.Fatalf("adjacent = %q %+v", adjacent.text, adjacent.matches)
	}
}

func TestParseMarkedSearchSnippetRejectsInvalidMarkerStates(t *testing.T) {
	start, end := fixedSearchMarkers()
	other := "[[TURING-FTS5-SNIPPET-START:v1:" + strings.Repeat("b", 32) + "]]"

	for _, test := range []struct {
		name string
		raw  string
	}{
		{name: "zero pairs", raw: "plain snippet with no markers"},
		{name: "empty input", raw: ""},
		{name: "missing end", raw: "before " + start + "needle"},
		{name: "end in text", raw: "before " + end + " after"},
		{name: "end only", raw: end},
		{name: "start in match", raw: start + "a" + start + "b" + end},
		{name: "reversed order", raw: end + "needle" + start},
		{name: "trailing match", raw: start + "a" + end + start + "b"},
		// A complete pair followed by a start marker that is the last byte of
		// the snippet: the loop runs out of input while still in match state,
		// which is the only way to reach the post-loop guard.
		{name: "ends at dangling start", raw: start + "a" + end + "before " + start},
		{name: "wrong nonce start", raw: other + "needle" + end},
		{name: "truncated start marker", raw: start[:len(start)-1] + "needle" + end},
		{name: "start marker split by invalid utf8", raw: start[:10] + "\xff" + start[10:] + "needle" + end},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parseMarkedSearchSnippet([]byte(test.raw), start, end)
			if !errors.Is(err, ErrInvalidSearchSnippetMarkers) {
				t.Fatalf("error = %v, want ErrInvalidSearchSnippetMarkers", err)
			}
			if parsed.text != nil || parsed.matches != nil {
				t.Fatalf("parsed payload on failure = %q %+v", parsed.text, parsed.matches)
			}
			if strings.Contains(err.Error(), "needle") {
				t.Fatalf("error leaks snippet content: %q", err.Error())
			}
		})
	}
}

// TestParseMarkedSearchSnippetStripsMarkersBeforeUTF8Repair pins the ordering
// the design requires: marker recognition runs on raw bytes, so invalid UTF-8
// sitting directly against a marker can neither absorb marker bytes nor
// survive as one.
func TestParseMarkedSearchSnippetStripsMarkersBeforeUTF8Repair(t *testing.T) {
	start, end := fixedSearchMarkers()
	raw := []byte("\xff" + start + "nee\xffdle" + end + "\xfe")

	parsed, err := parseMarkedSearchSnippet(raw, start, end)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("\xffnee\xffdle\xfe")
	if !bytes.Equal(parsed.text, want) {
		t.Fatalf("text = %q, want %q", parsed.text, want)
	}
	if !reflect.DeepEqual(parsed.matches, []byteSpan{{1, 8}}) {
		t.Fatalf("matches = %+v", parsed.matches)
	}
	if bytes.Contains(parsed.text, []byte(start)) || bytes.Contains(parsed.text, []byte(end)) ||
		bytes.Contains(parsed.text, []byte("TURING-FTS5-SNIPPET")) {
		t.Fatalf("marker bytes survived parsing: %q", parsed.text)
	}
}

func TestRejectSearchSnippetMarkerCollision(t *testing.T) {
	start, end := fixedSearchMarkers()
	for _, content := range []string{start, "prefix " + end + " suffix"} {
		if err := rejectSearchSnippetMarkerCollision(content, start, end); !errors.Is(err, ErrSearchSnippetMarkerCollision) {
			t.Fatalf("error = %v, want collision", err)
		}
	}
	markerLike := "[[TURING-FTS5-SNIPPET-START:v1:" + strings.Repeat("b", 32) + "]]"
	if err := rejectSearchSnippetMarkerCollision(markerLike, start, end); err != nil {
		t.Fatalf("marker-like content = %v", err)
	}
}

// TestRejectSearchSnippetMarkerCollisionAcceptsIncompleteMarkers keeps the
// check exact: only a complete generated marker is a collision, so ordinary
// content that merely looks structural still searches normally.
func TestRejectSearchSnippetMarkerCollisionAcceptsIncompleteMarkers(t *testing.T) {
	start, end := fixedSearchMarkers()
	for _, content := range []string{
		"",
		"[[TURING-FTS5-SNIPPET-START:v1:]]",
		"[[TURING-FTS5-SNIPPET-END:v2:" + strings.Repeat("a", 32) + "]]",
		start[:len(start)-1],
		end[:len(end)-1],
		strings.ToUpper(start),
		"[[TURING-FTS5-SNIPPET-START:v1:" + strings.Repeat("a", 31) + "]]",
	} {
		if err := rejectSearchSnippetMarkerCollision(content, start, end); err != nil {
			t.Fatalf("content %q = %v, want nil", content, err)
		}
	}
}

func parseFixedSearchSnippet(t *testing.T, raw string) parsedSearchSnippet {
	t.Helper()
	start, end := fixedSearchMarkers()
	parsed, err := parseMarkedSearchSnippet([]byte(raw), start, end)
	if err != nil {
		t.Fatalf("parseMarkedSearchSnippet(%q) = %v", raw, err)
	}
	return parsed
}

func TestSanitizeSearchSnippetRepairsAndBoundsText(t *testing.T) {
	start, end := fixedSearchMarkers()
	raw := append([]byte("lead \xff\n\u202E "),
		[]byte(start+"needle"+end+" "+strings.Repeat("界", 300))...)
	parsed, err := parseMarkedSearchSnippet(raw, start, end)
	if err != nil {
		t.Fatal(err)
	}
	got, err := sanitizeSearchSnippet(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) > 200 || len(got) > 800 {
		t.Fatalf("invalid bounds: runes=%d bytes=%d", utf8.RuneCountInString(got), len(got))
	}
	if strings.Contains(got, start) || strings.Contains(got, end) ||
		strings.ContainsRune(got, '\n') || strings.ContainsRune(got, '\u202E') ||
		!strings.Contains(got, "needle") {
		t.Fatalf("unsafe snippet = %q", got)
	}
	if strings.Contains(got, "\xff") || !strings.Contains(got, "lead \uFFFD \uFFFD needle") {
		t.Fatalf("repaired snippet = %q", got)
	}
}

func TestSanitizeSearchSnippetNormalizesWhitespaceControlsAndBidi(t *testing.T) {
	start, end := fixedSearchMarkers()
	for _, test := range []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "tab inside match becomes a space",
			raw:  start + "tab\there" + end,
			want: "tab here",
		},
		{
			name: "unicode whitespace collapses and trims",
			raw: " \t" + start + "match" + end +
				" \t\n\v\f\r\u0085\u00a0\u1680\u2000\u2028\u2029\u3000tail \n ",
			want: "match tail",
		},
		{
			name: "c0 c1 and del become replacement characters",
			raw:  start + "m" + end + " x\x00y\x01z\x7f\u0090w",
			want: "m x\uFFFDy\uFFFDz\uFFFD\uFFFDw",
		},
		{
			name: "explicit bidi controls become replacement characters",
			raw: start + "m" + end +
				" \u061c\u200e\u200f\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069",
			want: "m " + strings.Repeat("\uFFFD", 12),
		},
		{
			name: "invalid utf8 adjacent to markers is repaired after stripping",
			raw:  "\xff" + start + "ne\xffedle" + end + "\xfe\xfd",
			want: "\uFFFDne\uFFFDedle\uFFFD",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := sanitizeSearchSnippet(parseFixedSearchSnippet(t, test.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("snippet = %q, want %q", got, test.want)
			}
		})
	}
}

// TestSanitizeSearchSnippetKeepsMarkupLiteral pins the data contract: the
// snippet is plain text, so markup stays exactly as the user typed it and is
// never entity-escaped, emphasized, or turned into an ANSI sequence here.
// Escaping belongs at the rendering sink.
func TestSanitizeSearchSnippetKeepsMarkupLiteral(t *testing.T) {
	start, end := fixedSearchMarkers()
	raw := start + "<script>alert(1)</script>" + end + " **bold** \x1b[31mred\x1b[0m"

	got, err := sanitizeSearchSnippet(parseFixedSearchSnippet(t, raw))
	if err != nil {
		t.Fatal(err)
	}
	want := "<script>alert(1)</script> **bold** \uFFFD[31mred\uFFFD[0m"
	if got != want {
		t.Fatalf("snippet = %q, want %q", got, want)
	}
	if strings.Contains(got, "&lt;") || strings.Contains(got, "&amp;") ||
		strings.ContainsRune(got, '\x1b') {
		t.Fatalf("snippet was escaped or kept an escape sequence: %q", got)
	}
}

// TestSanitizeSearchSnippetPreservesNaturalScripts proves the bidi rule targets
// explicit formatting controls only: RTL letters, CJK, combining marks, and
// emoji joiners are content and survive untouched.
func TestSanitizeSearchSnippetPreservesNaturalScripts(t *testing.T) {
	body := "café e\u0301 מרחב مرحبا 世界 👨\u200d👩\u200d👧 👍🏽"
	start, end := fixedSearchMarkers()

	got, err := sanitizeSearchSnippet(parseFixedSearchSnippet(t, start+body+end))
	if err != nil {
		t.Fatal(err)
	}
	if got != body {
		t.Fatalf("snippet = %q, want %q", got, body)
	}
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("natural script text was replaced: %q", got)
	}
}

func TestSanitizeSearchSnippetWindowsMiddleAndEndMatches(t *testing.T) {
	start, end := fixedSearchMarkers()
	for _, test := range []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "middle match keeps context on both sides",
			raw: strings.Repeat("a", 300) + start + "needle" + end +
				strings.Repeat("b", 300),
			want: "\u2026" + strings.Repeat("a", 96) + "needle" +
				strings.Repeat("b", 96) + "\u2026",
		},
		{
			name: "end match keeps only leading context",
			raw:  strings.Repeat("a", 400) + start + "needle" + end,
			want: "\u2026" + strings.Repeat("a", 193) + "needle",
		},
		{
			name: "start match keeps only trailing context",
			raw:  start + "needle" + end + strings.Repeat("b", 400),
			want: "needle" + strings.Repeat("b", 193) + "\u2026",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := sanitizeSearchSnippet(parseFixedSearchSnippet(t, test.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("snippet = %q, want %q", got, test.want)
			}
			if utf8.RuneCountInString(got) != 200 || len(got) > 800 {
				t.Fatalf("bounds: runes=%d bytes=%d", utf8.RuneCountInString(got), len(got))
			}
		})
	}
}

// TestSanitizeSearchSnippetTruncatesOversizedMatchToken covers the case FTS5's
// own token limit cannot bound: a single matched token longer than the caps.
// Bounds win, and the largest prefix of the match that fits stays visible.
func TestSanitizeSearchSnippetTruncatesOversizedMatchToken(t *testing.T) {
	start, end := fixedSearchMarkers()
	for _, test := range []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "ascii token",
			raw:  start + strings.Repeat("x", 500) + end,
			want: strings.Repeat("x", 199) + "\u2026",
		},
		{
			name: "four byte runes",
			raw:  start + strings.Repeat("😀", 300) + end,
			want: strings.Repeat("😀", 199) + "\u2026",
		},
		{
			name: "token after leading context",
			raw:  "lead " + start + strings.Repeat("x", 500) + end,
			want: "\u2026" + strings.Repeat("x", 198) + "\u2026",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := sanitizeSearchSnippet(parseFixedSearchSnippet(t, test.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("snippet = %q, want %q", got, test.want)
			}
			if utf8.RuneCountInString(got) > 200 || len(got) > 800 {
				t.Fatalf("bounds: runes=%d bytes=%d", utf8.RuneCountInString(got), len(got))
			}
		})
	}
}

// TestSanitizeSearchSnippetTrimsSpaceAgainstInsertedEllipsis pins the cut-edge
// hygiene rule. Without it a window can end on a collapsed space and render as
// "word \u2026", which reads like the source said something it did not.
func TestSanitizeSearchSnippetTrimsSpaceAgainstInsertedEllipsis(t *testing.T) {
	start, end := fixedSearchMarkers()
	for _, test := range []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "trailing cut edge lands on a space",
			raw:  start + "needle" + end + strings.Repeat(" b", 300),
			want: "needle" + strings.Repeat(" b", 96) + "\u2026",
		},
		{
			name: "leading cut edge lands on a space",
			raw:  strings.Repeat("a ", 300) + start + "needle" + end,
			want: "\u2026" + strings.Repeat("a ", 96) + "needle",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := sanitizeSearchSnippet(parseFixedSearchSnippet(t, test.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("snippet = %q, want %q", got, test.want)
			}
			if strings.Contains(got, " \u2026") || strings.Contains(got, "\u2026 ") {
				t.Fatalf("space sits against an ellipsis: %q", got)
			}
			if utf8.RuneCountInString(got) > 200 || len(got) > 800 {
				t.Fatalf("bounds: runes=%d bytes=%d", utf8.RuneCountInString(got), len(got))
			}
		})
	}
}

func TestSanitizeSearchSnippetHonorsExactRuneAndByteBounds(t *testing.T) {
	start, end := fixedSearchMarkers()

	// Exactly 200 scalars and exactly 800 bytes: both caps are inclusive, so
	// this must survive untouched with no ellipsis.
	exact := strings.Repeat("😀", 200)
	got, err := sanitizeSearchSnippet(parseFixedSearchSnippet(t, start+exact+end))
	if err != nil {
		t.Fatal(err)
	}
	if got != exact || utf8.RuneCountInString(got) != 200 || len(got) != 800 {
		t.Fatalf("exact bound snippet: runes=%d bytes=%d", utf8.RuneCountInString(got), len(got))
	}

	// One scalar over: the window has to give up a scalar for the ellipsis and
	// the byte reserve has to be paid out of the 800-byte cap too.
	over := start + strings.Repeat("😀", 6) + end + strings.Repeat("😀", 195)
	got, err = sanitizeSearchSnippet(parseFixedSearchSnippet(t, over))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("😀", 199) + "\u2026"
	if got != want {
		t.Fatalf("snippet = %q", got)
	}
	if utf8.RuneCountInString(got) != 200 || len(got) != 799 {
		t.Fatalf("over bound snippet: runes=%d bytes=%d", utf8.RuneCountInString(got), len(got))
	}

	// Exactly at the caps as plain ASCII, so the rune cap alone is the edge.
	ascii := strings.Repeat("y", 200)
	got, err = sanitizeSearchSnippet(parseFixedSearchSnippet(t, start+ascii+end))
	if err != nil {
		t.Fatal(err)
	}
	if got != ascii {
		t.Fatalf("ascii bound snippet = %q", got)
	}
}

func TestSanitizeSearchSnippetRejectsEmptyOutput(t *testing.T) {
	start, end := fixedSearchMarkers()
	for _, test := range []struct {
		name string
		raw  string
	}{
		{name: "empty match", raw: start + end},
		{name: "whitespace only match", raw: start + " \t\n" + end},
		{name: "whitespace only snippet", raw: "  " + start + " " + end + "  "},
		{name: "match sanitizes away with surviving context", raw: "abc" + start + " " + end + "def"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := sanitizeSearchSnippet(parseFixedSearchSnippet(t, test.raw))
			if !errors.Is(err, ErrInvalidSearchSnippet) {
				t.Fatalf("error = %v, want ErrInvalidSearchSnippet", err)
			}
			if got != "" {
				t.Fatalf("snippet on failure = %q", got)
			}
		})
	}
}

// TestSanitizeSearchSnippetSkipsMatchEmptiedBySanitization pins the search in
// firstRetainedSearchMatch: the first recorded match can normalize away to an
// empty span, and the snippet must then be windowed around a later surviving
// match instead of being rejected.
func TestSanitizeSearchSnippetSkipsMatchEmptiedBySanitization(t *testing.T) {
	start, end := fixedSearchMarkers()
	lead := strings.Repeat("z", 400)
	raw := lead + start + " " + end + "ctx" + start + "needle" + end

	parsed := parseFixedSearchSnippet(t, raw)
	if len(parsed.matches) != 2 {
		t.Fatalf("matches = %+v, want two recorded spans", parsed.matches)
	}
	text, spans := normalizeSnippetRunes(parsed)
	if len(spans) != 2 || spans[0].end != spans[0].start || spans[1].end <= spans[1].start {
		t.Fatalf("spans = %+v over %q", spans, string(text))
	}

	got, err := sanitizeSearchSnippet(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "needle") {
		t.Fatalf("snippet = %q, want the surviving match", got)
	}
	if utf8.RuneCountInString(got) > searchSnippetMaxRunes || len(got) > searchSnippetMaxBytes {
		t.Fatalf("invalid bounds: runes=%d bytes=%d", utf8.RuneCountInString(got), len(got))
	}
}

// TestNormalizeSnippetRunesClampsMatchEmptiedByPendingSpace pins the clamp in
// applySpanBoundaries. The match is a single space that collapses into the
// pending space in front of it, so its start index is provisionally past the
// end of the emitted text; without the clamp the span would be inverted.
func TestNormalizeSnippetRunesClampsMatchEmptiedByPendingSpace(t *testing.T) {
	start, end := fixedSearchMarkers()
	parsed := parseFixedSearchSnippet(t, "abc "+start+" "+end+"def")

	text, spans := normalizeSnippetRunes(parsed)
	if string(text) != "abc def" {
		t.Fatalf("text = %q, want %q", string(text), "abc def")
	}
	if !reflect.DeepEqual(spans, []runeSpan{{start: 3, end: 3}}) {
		t.Fatalf("spans = %+v, want one empty span at 3", spans)
	}

	got, err := sanitizeSearchSnippet(parsed)
	if !errors.Is(err, ErrInvalidSearchSnippet) {
		t.Fatalf("error = %v, want ErrInvalidSearchSnippet", err)
	}
	if got != "" {
		t.Fatalf("snippet on failure = %q", got)
	}
}

// TestParseMarkedSearchSnippetRejectsDegenerateMarkers pins the precondition
// the two-state machine depends on: an empty marker matches at every offset,
// so the loop consumes nothing and spins forever, and a start marker equal to
// its end marker leaves no way to tell an opening from a closing marker, so
// the parser silently invents match boundaries. Production markers can never
// be degenerate, which is exactly why the caller has to be stopped at the door
// rather than trusted.
func TestParseMarkedSearchSnippetRejectsDegenerateMarkers(t *testing.T) {
	start, end := fixedSearchMarkers()
	same := "[[TURING-FTS5-SNIPPET-SAME:v1:" + strings.Repeat("a", 32) + "]]"

	for _, test := range []struct {
		name  string
		start string
		end   string
		raw   string
	}{
		{name: "empty start", start: "", end: end, raw: "needle" + end},
		{name: "empty end", start: start, end: "", raw: start + "needle"},
		{name: "both empty", start: "", end: "", raw: "needle"},
		{name: "identical markers", start: same, end: same, raw: "a" + same + "needle" + same + "b"},
		{name: "identical empty markers", start: "", end: "", raw: ""},
		{name: "identical real start marker", start: start, end: start, raw: start + "needle" + start},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parseMarkedSearchSnippet([]byte(test.raw), test.start, test.end)
			if !errors.Is(err, ErrInvalidSearchSnippetMarkers) {
				t.Fatalf("error = %v, want ErrInvalidSearchSnippetMarkers", err)
			}
			if parsed.text != nil || parsed.matches != nil {
				t.Fatalf("parsed payload on failure = %q %+v", parsed.text, parsed.matches)
			}
			if strings.Contains(err.Error(), "needle") {
				t.Fatalf("error leaks snippet content: %q", err.Error())
			}
		})
	}
}

// TestNewSearchSnippetMarkersAreNeverDegenerate ties the parser precondition
// back to the only production source of markers.
func TestNewSearchSnippetMarkersAreNeverDegenerate(t *testing.T) {
	start, end, err := newSearchSnippetMarkers(bytes.NewReader(bytes.Repeat([]byte{0xa5}, searchSnippetMarkerNonceBytes)))
	if err != nil {
		t.Fatal(err)
	}
	if start == "" || end == "" || start == end {
		t.Fatalf("degenerate generated markers: %q %q", start, end)
	}
}

// deterministicSearchEntropy returns exactly the bytes one marker nonce
// consumes, so a test can predict the markers a query will build without the
// exported method exposing an entropy seam.
func deterministicSearchEntropy(fill byte) *bytes.Reader {
	return bytes.NewReader(bytes.Repeat([]byte{fill}, searchSnippetMarkerNonceBytes))
}

func assertSearchHitIDs(t *testing.T, hits []SearchHit, want []string) {
	t.Helper()
	if hits == nil {
		t.Fatalf("hits slice is nil, want %v", want)
	}
	got := make([]string, 0, len(hits))
	for _, hit := range hits {
		got = append(got, hit.Message.MessageID)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hit message IDs = %v, want %v", got, want)
	}
}

// assertPublicSnippet checks every invariant the repository promises about a
// snippet it hands out, independently of the fixture that produced it.
func assertPublicSnippet(t *testing.T, snippet string) {
	t.Helper()
	if snippet == "" {
		t.Fatal("snippet is empty")
	}
	if !utf8.ValidString(snippet) {
		t.Fatalf("snippet is not valid UTF-8: %q", snippet)
	}
	if count := utf8.RuneCountInString(snippet); count > searchSnippetMaxRunes {
		t.Fatalf("snippet rune count = %d, want <= %d", count, searchSnippetMaxRunes)
	}
	if len(snippet) > searchSnippetMaxBytes {
		t.Fatalf("snippet byte size = %d, want <= %d", len(snippet), searchSnippetMaxBytes)
	}
	if strings.ContainsAny(snippet, "\n\r\t\x00") {
		t.Fatalf("snippet is not single-line plain text: %q", snippet)
	}
	if strings.Contains(snippet, searchSnippetStartPrefix) ||
		strings.Contains(snippet, searchSnippetEndPrefix) {
		t.Fatalf("snippet leaks internal markers: %q", snippet)
	}
}

func assertPublicScore(t *testing.T, score float64) {
	t.Helper()
	if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || math.Signbit(score) {
		t.Fatalf("score = %v, want finite and non-negative", score)
	}
}

// TestSearchMessageHitsExposeHigherIsBetterScoresAndLegacyParity pins the two
// contracts that make the hit projection safe to add: the score direction is
// inverted relative to raw bm25, and every message value is byte-identical to
// what the legacy projection already returns for the same fixture.
func TestSearchMessageHitsExposeHigherIsBetterScoresAndLegacyParity(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertSearchSession(t, ctx, database, "s1")
	insertSearchSession(t, ctx, database, "s2")
	insertSearchMessage(t, ctx, database, "m-rank-high", "s1", "rankterm rankterm rankterm", 1)
	insertSearchMessage(t, ctx, database, "m-rank-s1", "s1", "rankterm", 2)
	insertSearchMessage(t, ctx, database, "m-rank-s2", "s2", "rankterm", 1)
	insertSearchMessage(t, ctx, database, "m-not-a-match", "s2", "unrelated", 2)
	if _, err := database.ExecContext(ctx, `UPDATE messages SET run_id = 'run-rank-high' WHERE id = 'm-rank-high'`); err != nil {
		t.Fatalf("set message run_id: %v", err)
	}

	messages, err := repo.SearchMessages(ctx, "", "", "rankterm", 10)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	hits, err := repo.SearchMessageHits(ctx, "", "", "rankterm", 10)
	if err != nil {
		t.Fatalf("SearchMessageHits: %v", err)
	}
	assertSearchHitIDs(t, hits, []string{"m-rank-high", "m-rank-s1", "m-rank-s2"})
	if len(hits) != len(messages) {
		t.Fatalf("hit count = %d, legacy message count = %d", len(hits), len(messages))
	}
	for i := range hits {
		if !reflect.DeepEqual(hits[i].Message, messages[i]) {
			t.Fatalf("hit %d message = %+v, legacy message = %+v", i, hits[i].Message, messages[i])
		}
	}
	if hits[0].Message.RunID != "run-rank-high" {
		t.Fatalf("hit run ID = %q, want run-rank-high", hits[0].Message.RunID)
	}
	if !(hits[0].Score > hits[1].Score) {
		t.Fatalf("best hit score %v is not above %v", hits[0].Score, hits[1].Score)
	}
	for i, hit := range hits {
		assertPublicScore(t, hit.Score)
		assertPublicSnippet(t, hit.Snippet)
		if !strings.Contains(hit.Snippet, "rankterm") {
			t.Fatalf("hit %d snippet = %q, want the matched term", i, hit.Snippet)
		}
	}

	var rawBest float64
	if err := database.QueryRowContext(ctx, `
		SELECT bm25(messages_fts)
		FROM messages_fts
		JOIN messages m ON m.rowid = messages_fts.rowid
		WHERE messages_fts MATCH ? AND m.id = 'm-rank-high'`,
		`"rankterm"`,
	).Scan(&rawBest); err != nil {
		t.Fatalf("select raw bm25: %v", err)
	}
	if hits[0].Score != -rawBest {
		t.Fatalf("public score = %v, want exactly the negated raw bm25 %v", hits[0].Score, -rawBest)
	}
}

// TestSearchMessageHitsBreakEqualScoresByMessageID proves the tie-break is the
// message ID and that a real bm25 tie produces one shared public score rather
// than two values that merely sort the same way.
func TestSearchMessageHitsBreakEqualScoresByMessageID(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertSearchSession(t, ctx, database, "s1")
	insertSearchMessage(t, ctx, database, "m-tie-b", "s1", "tieterm", 1)
	insertSearchMessage(t, ctx, database, "m-tie-a", "s1", "tieterm", 2)

	rows, err := database.QueryContext(ctx, `
		SELECT bm25(messages_fts)
		FROM messages_fts
		JOIN messages m ON m.rowid = messages_fts.rowid
		WHERE messages_fts MATCH ?
		ORDER BY m.id`,
		`"tieterm"`,
	)
	if err != nil {
		t.Fatalf("select raw bm25: %v", err)
	}
	var raw []float64
	for rows.Next() {
		var value float64
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		raw = append(raw, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 || raw[0] != raw[1] {
		t.Fatalf("fixture raw bm25 values = %v, want two equal values", raw)
	}

	hits, err := repo.SearchMessageHits(ctx, "", "", "tieterm", 10)
	if err != nil {
		t.Fatalf("SearchMessageHits: %v", err)
	}
	assertSearchHitIDs(t, hits, []string{"m-tie-a", "m-tie-b"})
	if hits[0].Score != hits[1].Score {
		t.Fatalf("tied scores diverged: %v != %v", hits[0].Score, hits[1].Score)
	}
	assertPublicScore(t, hits[0].Score)
}

// TestSearchMessageHitsCanaryPinsFiniteNonPositiveBM25 pins the linked driver's
// bm25 sign. A term carried by nearly every document is the case where a
// conventional BM25 implementation would go positive; SQLite's negated form
// must not. A driver upgrade that changes this fails here rather than turning
// every production search into an internal error.
func TestSearchMessageHitsCanaryPinsFiniteNonPositiveBM25(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	insertSearchSession(t, ctx, database, "s1")
	const documents = 200
	const carriers = 199
	for i := 0; i < documents; i++ {
		content := fmt.Sprintf("filler%03d body words", i)
		if i < carriers {
			content = "canaryterm " + content
		}
		insertSearchMessage(t, ctx, database, fmt.Sprintf("m-canary-%03d", i), "s1", content, int64(i+1))
	}

	rows, err := database.QueryContext(ctx, `
		SELECT bm25(messages_fts)
		FROM messages_fts
		WHERE messages_fts MATCH ?`,
		`"canaryterm"`,
	)
	if err != nil {
		t.Fatalf("select raw bm25: %v", err)
	}
	defer func() { _ = rows.Close() }()
	scored := 0
	for rows.Next() {
		var raw float64
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if math.IsNaN(raw) || math.IsInf(raw, 0) || raw > 0 {
			t.Fatalf("raw bm25 = %v, want finite and non-positive", raw)
		}
		if _, err := normalizeSearchScore(raw); err != nil {
			t.Fatalf("normalizeSearchScore(%v) = %v", raw, err)
		}
		scored++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if scored != carriers {
		t.Fatalf("scored rows = %d, want %d", scored, carriers)
	}
}

// TestSearchMessageHitsRoundTripExactMarkersThroughSQLite is the linked-driver
// canary: bound marker TEXT must survive the driver byte-for-byte, and FTS5
// must wrap those exact bytes around a match at the start, middle, and end of
// real external-content rows.
func TestSearchMessageHitsRoundTripExactMarkersThroughSQLite(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	start, end, err := newSearchSnippetMarkers(deterministicSearchEntropy(0x5a))
	if err != nil {
		t.Fatal(err)
	}

	var gotStart, gotEnd string
	if err := database.QueryRowContext(ctx, `SELECT ?, ?`, start, end).Scan(&gotStart, &gotEnd); err != nil {
		t.Fatalf("round-trip bound markers: %v", err)
	}
	if gotStart != start || gotEnd != end {
		t.Fatalf("driver altered bound markers: %q/%q, want %q/%q", gotStart, gotEnd, start, end)
	}
	if strings.IndexByte(gotStart, 0) >= 0 || strings.IndexByte(gotEnd, 0) >= 0 {
		t.Fatal("driver introduced NUL into a marker")
	}
	if gotStart == gotEnd {
		t.Fatal("driver collapsed the marker pair into one value")
	}

	insertSearchSession(t, ctx, database, "s1")
	insertSearchMessage(t, ctx, database, "m-marker-1-start", "s1", "markerterm alpha bravo charlie delta", 1)
	insertSearchMessage(t, ctx, database, "m-marker-2-middle", "s1", "alpha bravo markerterm charlie delta", 2)
	insertSearchMessage(t, ctx, database, "m-marker-3-end", "s1", "alpha bravo charlie delta markerterm", 3)

	rows, err := database.QueryContext(ctx, `
		SELECT m.id, snippet(messages_fts, 0, ?, ?, '…', 32)
		FROM messages_fts
		JOIN messages m ON m.rowid = messages_fts.rowid
		WHERE messages_fts MATCH ?
		ORDER BY m.id`,
		start, end, `"markerterm"`,
	)
	if err != nil {
		t.Fatalf("select marked snippets: %v", err)
	}
	defer func() { _ = rows.Close() }()
	seen := 0
	for rows.Next() {
		var id, marked string
		if err := rows.Scan(&id, &marked); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(marked, start+"markerterm"+end) {
			t.Fatalf("%s marked snippet = %q, want exact markers around the match", id, marked)
		}
		if strings.IndexByte(marked, 0) >= 0 {
			t.Fatalf("%s marked snippet contains NUL: %q", id, marked)
		}
		parsed, err := parseMarkedSearchSnippet([]byte(marked), start, end)
		if err != nil {
			t.Fatalf("%s parse: %v", id, err)
		}
		if len(parsed.matches) != 1 {
			t.Fatalf("%s match spans = %+v, want one", id, parsed.matches)
		}
		matched := string(parsed.text[parsed.matches[0].start:parsed.matches[0].end])
		if matched != "markerterm" {
			t.Fatalf("%s match span text = %q, want markerterm", id, matched)
		}
		if strings.Contains(string(parsed.text), "[[TURING-FTS5-SNIPPET") {
			t.Fatalf("%s parsed payload retains marker bytes: %q", id, parsed.text)
		}
		snippet, err := sanitizeSearchSnippet(parsed)
		if err != nil {
			t.Fatalf("%s sanitize: %v", id, err)
		}
		assertPublicSnippet(t, snippet)
		seen++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != 3 {
		t.Fatalf("marked snippet rows = %d, want 3", seen)
	}
}

// TestSearchMessageHitsFailClosedOnMarkerCollision drives the collision guard
// end to end: with entropy pinned, source content can be authored to carry this
// query's exact marker, and the whole query must fail rather than trust the
// parser's boundaries.
func TestSearchMessageHitsFailClosedOnMarkerCollision(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		endSide bool
	}{
		{name: "start marker in content"},
		{name: "end marker in content", endSide: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			database := openTestDB(t)
			repo := New(database)
			ctx := context.Background()
			start, end, err := newSearchSnippetMarkers(deterministicSearchEntropy(0xaa))
			if err != nil {
				t.Fatal(err)
			}
			forged := start
			if testCase.endSide {
				forged = end
			}
			insertSearchSession(t, ctx, database, "s1")
			insertSearchMessage(t, ctx, database, "m-collision", "s1", forged+" needle", 1)

			hits, err := repo.searchMessageHits(
				ctx, "", "", "needle", 10, deterministicSearchEntropy(0xaa),
			)
			if !errors.Is(err, ErrSearchSnippetMarkerCollision) || hits != nil {
				t.Fatalf("hits, error = %+v, %v", hits, err)
			}
		})
	}
}

// TestSearchMessageHitsReturnMatchCenteredBoundedSnippets covers the public
// snippet's shape: it keeps the match, keeps context on both sides when there
// is room, obeys both caps, and treats markup, control bytes, and non-Latin
// scripts as inert text rather than formatting.
func TestSearchMessageHitsReturnMatchCenteredBoundedSnippets(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertSearchSession(t, ctx, database, "s1")

	filler := strings.TrimSpace(strings.Repeat("contextword12 ", 20))
	longBody := filler + " boundterm " + filler
	// One token larger than both caps: bounds win and the largest prefix that
	// still fits stays visible.
	oversizedToken := "oversizedterm" + strings.Repeat("q", 900)
	oversized := "prefixword " + oversizedToken + " suffixword"

	for i, fixture := range []struct {
		id      string
		content string
	}{
		{id: "m-snip-1-long", content: longBody},
		{id: "m-snip-2-markup", content: "<script>alert(\"markupterm\")</script> **markupterm** \x1b[31mred\x1b[0m"},
		{id: "m-snip-3-cjk", content: "会議 東京 予定 資料"},
		{id: "m-snip-4-rtl", content: "\u202eمرحبا rtlterm بالعالم\u202c"},
		{id: "m-snip-5-emoji", content: "cafe\u0301 🤖 emojiterm 👍🏽 done"},
		{id: "m-snip-6-lines", content: "first line\nsecond\tlineterm\r\nthird\x07line"},
		{id: "m-snip-7-oversized", content: oversized},
	} {
		insertSearchMessage(t, ctx, database, fixture.id, "s1", fixture.content, int64(i+1))
	}

	for _, testCase := range []struct {
		name        string
		query       string
		wantID      string
		wantContain []string
		wantAbsent  []string
		centered    bool
	}{
		{
			name:        "long body keeps context on both sides",
			query:       "boundterm",
			wantID:      "m-snip-1-long",
			wantContain: []string{"boundterm", "contextword12"},
			centered:    true,
		},
		{
			name:        "markup stays literal",
			query:       "markupterm",
			wantID:      "m-snip-2-markup",
			wantContain: []string{"markupterm", "<script>", "**"},
			wantAbsent:  []string{"\x1b"},
		},
		{
			name:        "cjk token survives",
			query:       "東京",
			wantID:      "m-snip-3-cjk",
			wantContain: []string{"東京", "会議"},
		},
		{
			name:        "rtl text keeps letters and drops explicit overrides",
			query:       "rtlterm",
			wantID:      "m-snip-4-rtl",
			wantContain: []string{"rtlterm", "مرحبا"},
			wantAbsent:  []string{"\u202e", "\u202c"},
		},
		{
			name:        "emoji and combining marks survive",
			query:       "emojiterm",
			wantID:      "m-snip-5-emoji",
			wantContain: []string{"emojiterm", "🤖", "cafe\u0301", "👍🏽"},
		},
		{
			name:        "line breaks and controls collapse",
			query:       "lineterm",
			wantID:      "m-snip-6-lines",
			wantContain: []string{"lineterm", "second lineterm"},
			wantAbsent:  []string{"\n", "\r", "\t", "\x07"},
		},
		{
			name:        "oversized match token is truncated to the caps",
			query:       oversizedToken,
			wantID:      "m-snip-7-oversized",
			wantContain: []string{"oversizedterm"},
			wantAbsent:  []string{"prefixword", "suffixword"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			hits, err := repo.SearchMessageHits(ctx, "", "", testCase.query, 10)
			if err != nil {
				t.Fatalf("SearchMessageHits(%q): %v", testCase.query, err)
			}
			assertSearchHitIDs(t, hits, []string{testCase.wantID})
			snippet := hits[0].Snippet
			assertPublicSnippet(t, snippet)
			for _, want := range testCase.wantContain {
				if !strings.Contains(snippet, want) {
					t.Fatalf("snippet = %q, want it to contain %q", snippet, want)
				}
			}
			for _, unwanted := range testCase.wantAbsent {
				if strings.Contains(snippet, unwanted) {
					t.Fatalf("snippet = %q, want it to omit %q", snippet, unwanted)
				}
			}
			if testCase.centered {
				index := strings.Index(snippet, testCase.query)
				if index <= 0 || index+len(testCase.query) >= len(snippet) {
					t.Fatalf("snippet = %q, want the match centered with context on both sides", snippet)
				}
			}
		})
	}
}

// TestSearchMessageHitsNeverReadSnippetTextFromNeighborMessage pins snippet
// provenance: the fragment comes from the hit's own row, never from the
// messages stored next to it.
func TestSearchMessageHitsNeverReadSnippetTextFromNeighborMessage(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	const secret = "sentinelsecret"
	insertSearchSession(t, ctx, database, "s1")
	insertSearchMessage(t, ctx, database, "m-provenance-1-before", "s1", secret+"alpha before the hit", 1)
	insertSearchMessage(t, ctx, database, "m-provenance-2-hit", "s1", "provenanceterm inside the hit message", 2)
	insertSearchMessage(t, ctx, database, "m-provenance-3-after", "s1", secret+"omega after the hit", 3)

	hits, err := repo.SearchMessageHits(ctx, "", "", "provenanceterm", 10)
	if err != nil {
		t.Fatalf("SearchMessageHits: %v", err)
	}
	assertSearchHitIDs(t, hits, []string{"m-provenance-2-hit"})
	assertPublicSnippet(t, hits[0].Snippet)
	if strings.Contains(hits[0].Snippet, secret) {
		t.Fatalf("snippet = %q, want no neighbor content", hits[0].Snippet)
	}
	if !strings.Contains(hits[0].Message.Content, strings.TrimSuffix(strings.TrimPrefix(hits[0].Snippet, "…"), "…")) {
		t.Fatalf("snippet = %q is not a fragment of its own message %q", hits[0].Snippet, hits[0].Message.Content)
	}
}

// TestSearchMessageHitsDocumentDivergentExternalContentBehavior records what
// happens when a database is mutated around the FTS triggers, which application
// writes never do. A row whose match position no longer exists yields no
// markers and fails closed; a same-shape replacement can still produce balanced
// markers around stale offsets, and MEM-002 only guarantees that the text stays
// confined to the returned message.
func TestSearchMessageHitsDocumentDivergentExternalContentBehavior(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	const secret = "divergentneighborsecret"
	insertSearchSession(t, ctx, database, "s1")
	insertSearchMessage(t, ctx, database, "m-divergent-short", "s1", "alpha beta divergentterm", 1)
	insertSearchMessage(t, ctx, database, "m-divergent-stale", "s1", "staleterm trailing", 2)
	insertSearchMessage(t, ctx, database, "m-divergent-neighbor", "s1", secret+" untouched", 3)

	// The trigger stays dropped for the rest of the test: openTestDB gives this
	// test its own temporary database and closes it, so nothing outside this
	// function can observe the divergent index.
	if _, err := database.ExecContext(ctx, `DROP TRIGGER messages_fts_au`); err != nil {
		t.Fatalf("drop update trigger: %v", err)
	}

	if _, err := database.ExecContext(ctx,
		`UPDATE messages SET content = 'alpha' WHERE id = 'm-divergent-short'`,
	); err != nil {
		t.Fatalf("shorten divergent row: %v", err)
	}
	hits, err := repo.SearchMessageHits(ctx, "", "", "divergentterm", 10)
	if !errors.Is(err, ErrInvalidSearchSnippetMarkers) || hits != nil {
		t.Fatalf("shortened row hits, error = %+v, %v", hits, err)
	}

	if _, err := database.ExecContext(ctx,
		`UPDATE messages SET content = 'replaced trailing' WHERE id = 'm-divergent-stale'`,
	); err != nil {
		t.Fatalf("replace divergent row: %v", err)
	}
	hits, err = repo.SearchMessageHits(ctx, "", "", "staleterm", 10)
	if err != nil {
		t.Fatalf("stale offsets: %v", err)
	}
	assertSearchHitIDs(t, hits, []string{"m-divergent-stale"})
	assertPublicSnippet(t, hits[0].Snippet)
	if hits[0].Snippet != "replaced trailing" {
		t.Fatalf("snippet = %q, want only the returned row's own current content", hits[0].Snippet)
	}
	if strings.Contains(hits[0].Snippet, secret) {
		t.Fatalf("snippet = %q, want no neighbor content", hits[0].Snippet)
	}
}

// TestSearchMessageHitsKeepExactIdentifierSemantics proves MEM-002 adds no ID
// lookup: an identifier is findable because its tokens are in the content, and
// an identifier that exists only as a primary key still matches nothing.
func TestSearchMessageHitsKeepExactIdentifierSemantics(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	const identifier = "msg_identifierterm_01"
	insertSearchSession(t, ctx, database, "s1")
	insertSearchMessage(t, ctx, database, identifier, "s1", "this body never names the key", 1)
	insertSearchMessage(t, ctx, database, "m-identifier-body", "s1",
		"leading words "+identifier+" trailing words", 2)

	hits, err := repo.SearchMessageHits(ctx, "", "", identifier, 10)
	if err != nil {
		t.Fatalf("SearchMessageHits: %v", err)
	}
	assertSearchHitIDs(t, hits, []string{"m-identifier-body"})
	assertPublicSnippet(t, hits[0].Snippet)
	if !strings.Contains(hits[0].Snippet, identifier) {
		t.Fatalf("snippet = %q, want the identifier", hits[0].Snippet)
	}
}

// TestSearchMessageHitsIncludeActiveAndArchivedSessions pins archive as
// reversible visibility state: archived conversations stay searchable.
func TestSearchMessageHitsIncludeActiveAndArchivedSessions(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertListSession(t, ctx, database, "s-active", "active", "2026-08-20T04:00:00.000000000Z")
	insertListSession(t, ctx, database, "s-archived", "archived", "2026-08-20T04:00:01.000000000Z")
	insertSearchMessage(t, ctx, database, "m-lifecycle-1-active", "s-active", "lifecycleterm", 1)
	insertSearchMessage(t, ctx, database, "m-lifecycle-2-archived", "s-archived", "lifecycleterm", 1)

	hits, err := repo.SearchMessageHits(ctx, "", "", "lifecycleterm", 10)
	if err != nil {
		t.Fatalf("SearchMessageHits: %v", err)
	}
	assertSearchHitIDs(t, hits, []string{"m-lifecycle-1-active", "m-lifecycle-2-archived"})

	scoped, err := repo.SearchMessageHits(ctx, "s-archived", "", "lifecycleterm", 10)
	if err != nil {
		t.Fatalf("SearchMessageHits scoped: %v", err)
	}
	assertSearchHitIDs(t, scoped, []string{"m-lifecycle-2-archived"})
}

// TestSearchMessageHitsExcludeDeletingSessions keeps deletion precedence ahead
// of both search and archive.
func TestSearchMessageHitsExcludeDeletingSessions(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertSearchSession(t, ctx, database, "s-keep")
	insertSearchSession(t, ctx, database, "s-deleting")
	insertSearchMessage(t, ctx, database, "m-deletion-keep", "s-keep", "deletionterm", 1)
	insertSearchMessage(t, ctx, database, "m-deletion-gone", "s-deleting", "deletionterm", 1)
	if _, err := database.ExecContext(ctx,
		`UPDATE sessions SET deletion_state = 'deleting' WHERE id = 's-deleting'`,
	); err != nil {
		t.Fatalf("mark session deleting: %v", err)
	}

	hits, err := repo.SearchMessageHits(ctx, "", "", "deletionterm", 10)
	if err != nil {
		t.Fatalf("SearchMessageHits: %v", err)
	}
	assertSearchHitIDs(t, hits, []string{"m-deletion-keep"})

	scoped, err := repo.SearchMessageHits(ctx, "s-deleting", "", "deletionterm", 10)
	if err != nil {
		t.Fatalf("SearchMessageHits scoped: %v", err)
	}
	assertSearchHitIDs(t, scoped, []string{})
}

// TestSearchMessageHitsPinPublicSessionStatusDomain freezes the searchable
// status domain in the schema itself, so adding a lifecycle status forces an
// explicit decision about whether its conversations are searchable.
func TestSearchMessageHitsPinPublicSessionStatusDomain(t *testing.T) {
	database := openTestDB(t)
	ctx := context.Background()
	var ddl string
	if err := database.QueryRowContext(ctx,
		`SELECT sql FROM sqlite_schema WHERE type = 'table' AND name = 'sessions'`,
	).Scan(&ddl); err != nil {
		t.Fatalf("read sessions DDL: %v", err)
	}
	normalized := strings.Join(strings.Fields(ddl), " ")
	const wantCheck = "CHECK (status IN ('active','archived'))"
	if !strings.Contains(normalized, wantCheck) {
		t.Fatalf("sessions DDL = %q, want it to contain %q", normalized, wantCheck)
	}
	domain := regexp.MustCompile(`CHECK \(status IN \(([^)]*)\)\)`).FindAllStringSubmatch(normalized, -1)
	if len(domain) != 1 || domain[0][1] != `'active','archived'` {
		t.Fatalf("sessions status domain = %v, want exactly one 'active','archived' domain", domain)
	}
}

// TestSearchMessageHitsPreserveScopeExclusionAndLimits keeps the hit projection
// on exactly the legacy scope, exclusion, and limit behavior, including the
// empty-but-not-nil result for a real token that matches nothing.
func TestSearchMessageHitsPreserveScopeExclusionAndLimits(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertSearchSession(t, ctx, database, "s1")
	insertSearchSession(t, ctx, database, "s2")
	for i := 1; i <= 21; i++ {
		insertSearchMessage(t, ctx, database, fmt.Sprintf("m-limit-%02d", i), "s1", "limitterm", int64(i))
	}
	insertSearchMessage(t, ctx, database, "m-limit-s2", "s2", "limitterm", 1)

	all := make([]string, 0, 22)
	for i := 1; i <= 21; i++ {
		all = append(all, fmt.Sprintf("m-limit-%02d", i))
	}
	all = append(all, "m-limit-s2")

	scoped, err := repo.SearchMessageHits(ctx, "s2", "", "limitterm", 10)
	if err != nil {
		t.Fatalf("SearchMessageHits scoped: %v", err)
	}
	assertSearchHitIDs(t, scoped, []string{"m-limit-s2"})

	excluded, err := repo.SearchMessageHits(ctx, "", "s1", "limitterm", 10)
	if err != nil {
		t.Fatalf("SearchMessageHits excluded: %v", err)
	}
	assertSearchHitIDs(t, excluded, []string{"m-limit-s2"})

	valid, err := repo.SearchMessageHits(ctx, "", "", "limitterm", 2)
	if err != nil {
		t.Fatalf("SearchMessageHits valid limit: %v", err)
	}
	assertSearchHitIDs(t, valid, all[:2])

	maximum, err := repo.SearchMessageHits(ctx, "", "", "limitterm", 100)
	if err != nil {
		t.Fatalf("SearchMessageHits maximum limit: %v", err)
	}
	assertSearchHitIDs(t, maximum, all)

	for _, limit := range []int{0, -1, 101} {
		defaulted, err := repo.SearchMessageHits(ctx, "", "", "limitterm", limit)
		if err != nil {
			t.Fatalf("SearchMessageHits limit %d: %v", limit, err)
		}
		assertSearchHitIDs(t, defaulted, all[:20])
	}

	noMatch, err := repo.SearchMessageHits(ctx, "", "", "missingterm", 10)
	if err != nil {
		t.Fatalf("SearchMessageHits no match: %v", err)
	}
	assertSearchHitIDs(t, noMatch, []string{})
}

// TestSearchMessageHitsKeepLiteralPhraseInjectionResistance keeps every query
// string data: FTS operators, unbalanced quotes, and NUL stay inside the
// server-built quoted phrase.
func TestSearchMessageHitsKeepLiteralPhraseInjectionResistance(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertSearchSession(t, ctx, database, "s1")
	insertSearchMessage(t, ctx, database, "m-literal", "s1", "operatorword OR broadword", 1)
	insertSearchMessage(t, ctx, database, "m-operator-only", "s1", "operatorword", 2)
	insertSearchMessage(t, ctx, database, "m-broad-only", "s1", "broadword", 3)
	insertSearchMessage(t, ctx, database, "m-malformed", "s1", "\"unterminated", 4)
	insertSearchMessage(t, ctx, database, "m-nul-phrase", "s1", "alpha beta", 5)
	insertSearchMessage(t, ctx, database, "m-nul-alpha", "s1", "alpha", 6)
	insertSearchMessage(t, ctx, database, "m-nul-beta", "s1", "beta", 7)

	for _, testCase := range []struct {
		name   string
		query  string
		wantID string
	}{
		{name: "operator looking", query: "operatorword OR broadword", wantID: "m-literal"},
		{name: "unbalanced quote", query: "\"unterminated", wantID: "m-malformed"},
		{name: "nul delimiter", query: "alpha\x00beta", wantID: "m-nul-phrase"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			hits, err := repo.SearchMessageHits(ctx, "", "", testCase.query, 10)
			if err != nil {
				t.Fatalf("SearchMessageHits(%q): %v", testCase.query, err)
			}
			assertSearchHitIDs(t, hits, []string{testCase.wantID})
			assertPublicSnippet(t, hits[0].Snippet)
		})
	}
}

// TestSearchMessageHitsTokenlessInputReturnsEmptySlice keeps a query FTS5 has
// no token for a successful empty result, and specifically not a nil slice a
// caller could confuse with an unset field.
func TestSearchMessageHitsTokenlessInputReturnsEmptySlice(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertSearchSession(t, ctx, database, "s1")
	insertSearchMessage(t, ctx, database, "m-tokenless", "s1", "ordinary searchable content", 1)

	for _, query := range []string{"", "...", "!!!", "🤖", "\x00", "\u0301"} {
		hits, err := repo.SearchMessageHits(ctx, "", "", query, 10)
		if err != nil {
			t.Fatalf("SearchMessageHits(%q): %v", query, err)
		}
		assertSearchHitIDs(t, hits, []string{})
	}
}

// TestSearchMessageHitsFailClosedOnMarkerEntropyBeforeQuerying pins the
// ordering that makes a random-source failure survivable: the markers are built
// before any reader opens, so the request fails closed, leaks none of the bytes
// the reader did hand back, and leaves the orchestrator's single SQLite
// connection free for the very next query. The follow-up legacy search runs on
// a deadline so a connection held open by an unclosed reader surfaces as a test
// failure instead of a hang.
func TestSearchMessageHitsFailClosedOnMarkerEntropyBeforeQuerying(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	insertSearchSession(t, ctx, database, "s1")
	insertSearchMessage(t, ctx, database, "m-entropy", "s1", "entropyterm inside the body", 1)

	// The fixture is searchable first, so the failure below can only come from
	// the entropy source and not from an empty or unmatched query.
	messages, err := repo.SearchMessages(ctx, "", "", "entropyterm", 10)
	if err != nil {
		t.Fatalf("seed SearchMessages: %v", err)
	}
	if len(messages) != 1 || messages[0].MessageID != "m-entropy" {
		t.Fatalf("seed messages = %+v, want m-entropy", messages)
	}

	reader := &shortEntropyReader{
		data: []byte("SECRETENTROPY15"),
		err:  errors.New("random source drained"),
	}
	hits, err := repo.searchMessageHits(ctx, "", "", "entropyterm", 10, reader)
	if !errors.Is(err, ErrSearchMarkerEntropy) || hits != nil {
		t.Fatalf("hits, error = %+v, %v", hits, err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error leaks entropy bytes: %v", err)
	}

	followUp, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	messages, err = repo.SearchMessages(followUp, "", "", "entropyterm", 10)
	if err != nil {
		t.Fatalf("SearchMessages after entropy failure: %v", err)
	}
	if len(messages) != 1 || messages[0].MessageID != "m-entropy" {
		t.Fatalf("messages after entropy failure = %+v, want m-entropy", messages)
	}
}

// TestSearchMessageHitsReturnContextAndRowErrors keeps failures typed and
// value-free instead of returning a partial result set.
func TestSearchMessageHitsReturnContextAndRowErrors(t *testing.T) {
	t.Run("canceled context", func(t *testing.T) {
		repo := New(openTestDB(t))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		hits, err := repo.SearchMessageHits(ctx, "", "", "anything", 10)
		if !errors.Is(err, context.Canceled) || hits != nil {
			t.Fatalf("hits, error = %+v, %v", hits, err)
		}
	})

	t.Run("row scan error", func(t *testing.T) {
		database := openTestDB(t)
		repo := New(database)
		ctx := context.Background()
		insertSearchSession(t, ctx, database, "s1")
		if _, err := database.ExecContext(ctx, `
			INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at)
			VALUES ('m-bad-sequence', 's1', 'user', 'scanerrorterm', 'text', 'not-a-number', '2026-08-10T00:00:00Z')`,
		); err != nil {
			t.Fatalf("insert malformed row: %v", err)
		}

		hits, err := repo.SearchMessageHits(ctx, "", "", "scanerrorterm", 10)
		if err == nil || hits != nil {
			t.Fatalf("hits, error = %+v, %v", hits, err)
		}
		if strings.Contains(err.Error(), "scanerrorterm") {
			t.Fatalf("error leaks message content: %v", err)
		}
	})
}
