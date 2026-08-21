package repository

import (
	"bytes"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
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
