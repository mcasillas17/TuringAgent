package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"runtime"
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

// searchSnippetPipelineAllocBudgetBytes is the transient heap one row's whole
// snippet pipeline may claim: validating the marker structure of the string the
// driver returned, streaming its chunks into the rolling window, and bounding
// that window into the public snippet.
//
// A searchSnippetWindowRunes window, the byte-offset table the bounding pass
// builds over it, and the published string need a few kilobytes between them.
// The budget is far above that and orders of magnitude below the multi-megabyte
// fragments the bound tests feed in, so it fails any allocation proportional to
// the input without being sensitive to allocator sizing.
const searchSnippetPipelineAllocBudgetBytes = 128 << 10

// searchSnippetScanAllocBudgetBytes is the heap one validation pass may claim.
// The scanner returns two counters and hands the consumer subslices of the
// string it was given, so a correct pass allocates nothing at all; the budget
// leaves room for test-harness noise between the two readings while still
// failing any copy of the fragment.
const searchSnippetScanAllocBudgetBytes = 4 << 10

// searchHitRowSnippetAllocFactor is how much of one row's own bytes the hit
// projection may allocate beyond what the legacy projection allocates for the
// same row. The extra snippet column is one more copy of that row, so a correct
// pipeline sits near 1x; the streaming rewrite removed two further copies that
// put it near 3x. Two is the midpoint, which leaves either side a factor of
// about two of headroom.
const searchHitRowSnippetAllocFactor = 2

// searchHitRowLegacyAllocCeiling keeps the baseline honest: the legacy
// projection returns the same row without a snippet column, so it must stay a
// small multiple of that row rather than growing into a budget large enough to
// hide a regression on the hit side.
const searchHitRowLegacyAllocCeiling = 5

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

// TestParseMarkedSearchSnippetAcceptsMarkerFreeFragment pins the marker-free
// outcome. FTS5's snippet() window is 32 tokens, so an exact phrase longer than
// that window has no in-window occurrence to wrap, and SQLite legitimately
// returns a fragment of the same matched row with no markers at all. That is a
// windowing result, not a structural failure: the payload passes through
// unchanged and carries no match spans.
func TestParseMarkedSearchSnippetAcceptsMarkerFreeFragment(t *testing.T) {
	start, end := fixedSearchMarkers()
	for _, test := range []struct {
		name string
		raw  string
	}{
		{name: "plain fragment", raw: "plain snippet with no markers"},
		{name: "fragment with edge ellipses", raw: "…a fragment cut on both edges…"},
		{name: "marker-shaped text with another nonce", raw: "[[TURING-FTS5-SNIPPET-START:v1:" + strings.Repeat("b", 32) + "]] tail"},
		{name: "truncated start marker", raw: start[:len(start)-1] + " tail"},
		{name: "truncated end marker", raw: "lead " + end[:len(end)-1]},
		{name: "invalid utf8 payload", raw: "lead \xff tail"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parseMarkedSearchSnippet([]byte(test.raw), start, end)
			if err != nil {
				t.Fatalf("parseMarkedSearchSnippet(%q) = %v, want a marker-free success", test.raw, err)
			}
			if string(parsed.text) != test.raw {
				t.Fatalf("text = %q, want the raw payload %q", parsed.text, test.raw)
			}
			if len(parsed.matches) != 0 {
				t.Fatalf("matches = %+v, want none", parsed.matches)
			}
		})
	}

	// The payload is the parser's own copy: the caller may not alias, and later
	// mutate, the buffer SQLite's value was scanned into.
	raw := []byte("aliasing check")
	parsed, err := parseMarkedSearchSnippet(raw, start, end)
	if err != nil {
		t.Fatal(err)
	}
	raw[0] = 'X'
	if string(parsed.text) != "aliasing check" {
		t.Fatalf("text = %q, want a copy independent of the input buffer", parsed.text)
	}
}

func TestParseMarkedSearchSnippetRejectsInvalidMarkerStates(t *testing.T) {
	start, end := fixedSearchMarkers()
	other := "[[TURING-FTS5-SNIPPET-START:v1:" + strings.Repeat("b", 32) + "]]"

	for _, test := range []struct {
		name string
		raw  string
	}{
		// An empty snippet is not a windowing outcome: FTS5 returns some text
		// for every row it matches, so nothing at all still fails closed.
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

// snippetSegment is one emission from the production scanner: a zero-copy
// subslice of the marked string and whether it sat inside a marker pair.
type snippetSegment struct {
	chunk   string
	inMatch bool
}

// collectSnippetSegments runs the production scanner and records everything it
// emitted, so a test can assert on the stream itself rather than on whatever a
// consumer happened to build out of it.
func collectSnippetSegments(raw, start, end string) ([]snippetSegment, markedSearchSnippetShape, error) {
	var segments []snippetSegment
	shape, err := scanMarkedSearchSnippet(raw, start, end,
		func(chunk string, inMatch bool) {
			segments = append(segments, snippetSegment{chunk: chunk, inMatch: inMatch})
		})
	return segments, shape, err
}

// TestScanMarkedSearchSnippetEmitsChunksAndShape pins the scanner's own
// contract, independently of the consumer that normalizes its output.
//
// The scanner is what production runs, so its emission order, its treatment of
// an empty match — which is still a boundary the window has to observe — and
// the shape it reports all need to be nailed down here rather than inferred
// from a snippet at the far end of the pipeline.
func TestScanMarkedSearchSnippetEmitsChunksAndShape(t *testing.T) {
	start, end := fixedSearchMarkers()

	for _, test := range []struct {
		name         string
		raw          string
		wantSegments []snippetSegment
		wantPairs    int
		wantPayload  int
	}{
		{
			name:         "marker free fragment",
			raw:          "plain fragment",
			wantSegments: []snippetSegment{{chunk: "plain fragment"}},
			wantPairs:    0,
			wantPayload:  14,
		},
		{
			name: "sequential pairs with context",
			raw:  "before " + start + "needle" + end + " middle " + start + "second" + end + " after",
			wantSegments: []snippetSegment{
				{chunk: "before "},
				{chunk: "needle", inMatch: true},
				{chunk: " middle "},
				{chunk: "second", inMatch: true},
				{chunk: " after"},
			},
			wantPairs:   2,
			wantPayload: 33,
		},
		{
			name:         "match is the whole fragment",
			raw:          start + "only" + end,
			wantSegments: []snippetSegment{{chunk: "only", inMatch: true}},
			wantPairs:    1,
			wantPayload:  4,
		},
		{
			name: "adjacent pairs emit no empty text between them",
			raw:  start + "ab" + end + start + "cd" + end,
			wantSegments: []snippetSegment{
				{chunk: "ab", inMatch: true},
				{chunk: "cd", inMatch: true},
			},
			wantPairs:   2,
			wantPayload: 4,
		},
		{
			// An empty match carries no payload but is still a span the window
			// must open and close on, so it is emitted rather than skipped.
			name: "empty match is still emitted",
			raw:  start + end + "tail",
			wantSegments: []snippetSegment{
				{chunk: "", inMatch: true},
				{chunk: "tail"},
			},
			wantPairs:   1,
			wantPayload: 4,
		},
		{
			name: "invalid utf8 around markers stays with its own side",
			raw:  "\xff" + start + "nee\xffdle" + end + "\xfe",
			wantSegments: []snippetSegment{
				{chunk: "\xff"},
				{chunk: "nee\xffdle", inMatch: true},
				{chunk: "\xfe"},
			},
			wantPairs:   1,
			wantPayload: 9,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			segments, shape, err := collectSnippetSegments(test.raw, start, end)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(segments, test.wantSegments) {
				t.Fatalf("segments = %+v, want %+v", segments, test.wantSegments)
			}
			if shape.pairs != test.wantPairs || shape.payloadBytes != test.wantPayload {
				t.Fatalf("shape = %+v, want pairs=%d payloadBytes=%d",
					shape, test.wantPairs, test.wantPayload)
			}

			// Every emitted chunk is a window into the caller's string rather
			// than a copy of it, which is the whole point of the scanner.
			for _, segment := range segments {
				if len(segment.chunk) == 0 {
					continue
				}
				if !strings.Contains(test.raw, segment.chunk) {
					t.Fatalf("chunk %q is not a subslice of the marked string", segment.chunk)
				}
			}
		})
	}
}

// TestScanMarkedSearchSnippetRejectsInvalidMarkerStates keeps the state machine
// exactly as strict at the scanner as it was at the byte parser, and pins that
// a rejected scan reports no shape a caller could act on.
func TestScanMarkedSearchSnippetRejectsInvalidMarkerStates(t *testing.T) {
	start, end := fixedSearchMarkers()
	other := "[[TURING-FTS5-SNIPPET-START:v1:" + strings.Repeat("b", 32) + "]]"
	same := "[[TURING-FTS5-SNIPPET-SAME:v1:" + strings.Repeat("a", 32) + "]]"

	for _, test := range []struct {
		name  string
		start string
		end   string
		raw   string
	}{
		{name: "empty input", start: start, end: end, raw: ""},
		{name: "missing end", start: start, end: end, raw: "before " + start + "needle"},
		{name: "end in text", start: start, end: end, raw: "before " + end + " after"},
		{name: "end only", start: start, end: end, raw: end},
		{name: "start in match", start: start, end: end, raw: start + "a" + start + "b" + end},
		{name: "reversed order", start: start, end: end, raw: end + "needle" + start},
		{name: "trailing match", start: start, end: end, raw: start + "a" + end + start + "b"},
		{name: "ends at dangling start", start: start, end: end, raw: start + "a" + end + "before " + start},
		{name: "wrong nonce start", start: start, end: end, raw: other + "needle" + end},
		{name: "empty start marker", start: "", end: end, raw: "needle" + end},
		{name: "empty end marker", start: start, end: "", raw: start + "needle"},
		{name: "identical markers", start: same, end: same, raw: "a" + same + "needle" + same + "b"},
	} {
		t.Run(test.name, func(t *testing.T) {
			shape, err := scanMarkedSearchSnippet(test.raw, test.start, test.end, nil)
			if !errors.Is(err, ErrInvalidSearchSnippetMarkers) {
				t.Fatalf("error = %v, want ErrInvalidSearchSnippetMarkers", err)
			}
			if shape != (markedSearchSnippetShape{}) {
				t.Fatalf("shape on failure = %+v, want the zero value", shape)
			}
			if strings.Contains(err.Error(), "needle") {
				t.Fatalf("error leaks snippet content: %q", err.Error())
			}

			// The same rejection reaches the caller through the production
			// entry point, which validates before it feeds anything forward.
			snippet, err := sanitizeMarkedSearchSnippet(test.raw, test.start, test.end)
			if !errors.Is(err, ErrInvalidSearchSnippetMarkers) {
				t.Fatalf("sanitize error = %v, want ErrInvalidSearchSnippetMarkers", err)
			}
			if snippet != "" {
				t.Fatalf("snippet on failure = %q", snippet)
			}
		})
	}
}

// TestNormalizeMarkedSnippetWindowJoinsScalarsAcrossMarkers pins the one place
// streaming could quietly change public output.
//
// Markers are removed while the input is still bytes, so a scalar whose
// encoding begins before a marker and ends after it is a single scalar of the
// payload — the marker was never part of it. A decoder that treated each
// stretch between markers as its own buffer would instead see two runs of
// invalid bytes and publish U+FFFD, and would also disagree about which side of
// the match the scalar fell on.
//
// Content is not trusted to be well-formed UTF-8, so this is a shape a message
// can actually produce, not a theoretical one.
func TestNormalizeMarkedSnippetWindowJoinsScalarsAcrossMarkers(t *testing.T) {
	start, end := fixedSearchMarkers()

	for _, test := range []struct {
		name        string
		raw         string
		wantSnippet string
		wantFound   bool
		wantMatch   runeSpan
	}{
		{
			// "界" is E7 95 8C: its lead and first continuation byte sit in
			// front of the match and its last byte opens the match. The scalar
			// began outside, so it is context, and the match starts after it.
			name:        "scalar opens before the match and closes inside it",
			raw:         "lead \xe7\x95" + start + "\x8ctail" + end,
			wantSnippet: "lead 界tail",
			wantFound:   true,
			wantMatch:   runeSpan{start: 6, end: 10},
		},
		{
			// Mirror image: the scalar began inside the match, so it belongs
			// to the match even though its last byte lies past the end marker.
			name:        "scalar opens inside the match and closes after it",
			raw:         start + "head \xe7\x95" + end + "\x8c trail",
			wantSnippet: "head 界 trail",
			wantFound:   true,
			wantMatch:   runeSpan{start: 0, end: 6},
		},
		{
			// F0 9F 98 80 is 😀, split three ways across two markers, so the
			// match's only byte is swallowed by a scalar that started before
			// it. No scalar ever lands inside the span, so there is no
			// retained match — the same outcome as a match that whitespace
			// collapses away — and the fragment fails rather than publishing
			// text with nothing matched in it.
			name:        "four byte scalar split across both markers",
			raw:         "a\xf0\x9f" + start + "\x98" + end + "\x80b",
			wantSnippet: "a😀b",
			wantFound:   false,
		},
		{
			// The completing bytes never arrive, so the trailing prefix is the
			// invalid run it actually is.
			name:        "unterminated scalar before the end of the fragment",
			raw:         start + "head" + end + " \xe7\x95",
			wantSnippet: "head \uFFFD",
			wantFound:   true,
			wantMatch:   runeSpan{start: 0, end: 4},
		},
		{
			// A marker-free fragment is one chunk streamed as an implicit
			// whole-fragment match, so its closing boundary is queued behind
			// the unterminated scalar and only comes due when the carry drains
			// at the end. Nothing else exercises that ordering by name.
			name:        "marker free fragment ending mid scalar",
			raw:         "tail \xe7\x95",
			wantSnippet: "tail \uFFFD",
			wantFound:   true,
			wantMatch:   runeSpan{start: 0, end: 6},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			window, err := normalizeMarkedSnippetWindow(test.raw, start, end)
			if err != nil {
				t.Fatal(err)
			}
			if string(window.text) != test.wantSnippet {
				t.Fatalf("window text = %q, want %q", string(window.text), test.wantSnippet)
			}
			if window.found != test.wantFound || window.match != test.wantMatch {
				t.Fatalf("match = %+v (found=%t), want %+v (found=%t)",
					window.match, window.found, test.wantMatch, test.wantFound)
			}
			// A fragment with no retained match publishes nothing at all,
			// rather than text with nothing matched in it.
			snippet, err := sanitizeMarkedSearchSnippet(test.raw, start, end)
			if test.wantFound {
				if err != nil {
					t.Fatal(err)
				}
				if snippet != test.wantSnippet {
					t.Fatalf("snippet = %q, want %q", snippet, test.wantSnippet)
				}
			} else if !errors.Is(err, ErrInvalidSearchSnippet) {
				t.Fatalf("snippet = %q, error = %v, want ErrInvalidSearchSnippet",
					snippet, err)
			}

			// The reference parser materializes the payload and decodes it as
			// one buffer, which is what "the marker was never part of it" has
			// to mean in practice.
			parsed, err := parseMarkedSearchSnippet([]byte(test.raw), start, end)
			if err != nil {
				t.Fatal(err)
			}
			reference := normalizeSnippetWindow(withWholeFragmentSpan(parsed))
			if string(window.text) != string(reference.text) ||
				window.match != reference.match ||
				window.found != reference.found ||
				window.totalRunes != reference.totalRunes ||
				window.totalBytes != reference.totalBytes {
				t.Fatalf("streamed %s disagrees with whole-buffer %s",
					snippetWindowSummary(window), snippetWindowSummary(reference))
			}
		})
	}
}

// TestScanMarkedSearchSnippetValidationPassIsAllocationFree pins the property
// the streaming design rests on: recognizing markers in a multi-megabyte string
// hands back subslices of it and allocates nothing proportional to it.
func TestScanMarkedSearchSnippetValidationPassIsAllocationFree(t *testing.T) {
	start, end := fixedSearchMarkers()
	token := strings.Repeat("x", 2<<20)
	raw := token + " " + start + "needle" + end + " " + token

	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	shape, err := scanMarkedSearchSnippet(raw, start, end, nil)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if shape.pairs != 1 || shape.payloadBytes != len(token)*2+8 {
		t.Fatalf("shape = %+v, want one pair over the whole payload", shape)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > searchSnippetScanAllocBudgetBytes {
		t.Fatalf("scanning %d bytes allocated %d bytes, want at most %d",
			len(raw), allocated, searchSnippetScanAllocBudgetBytes)
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
		{
			// Leading context longer than the retained window, so the window
			// has already discarded its own start before the match opens and
			// both cut edges are real.
			name: "token after leading context wider than the window",
			raw:  strings.Repeat("a", 300) + start + strings.Repeat("x", 500) + end,
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

// TestSanitizeSearchSnippetPreservesCompleteMatchAtExactBounds pins the
// priority between a complete match and the cut indicators drawn around it.
// The match is the reason the snippet exists, so a sanitized match that fits
// both caps on its own is published whole; the U+2026 indicators are
// best-effort at that boundary and are dropped together when they cannot both
// sit beside it. Charging them first would truncate an exactly-fitting match
// and publish a shorter excerpt than the caps allow.
func TestSanitizeSearchSnippetPreservesCompleteMatchAtExactBounds(t *testing.T) {
	start, end := fixedSearchMarkers()
	// 200 scalars / 200 bytes, and 200 scalars / 800 bytes: each sits exactly
	// on one of the two caps.
	exactASCII := strings.Repeat("x", searchSnippetMaxRunes)
	exactEmoji := strings.Repeat("😀", searchSnippetMaxRunes)

	for _, test := range []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "exact scalar cap with context on both sides",
			raw:  "lead " + start + exactASCII + end + " tail",
			want: exactASCII,
		},
		{
			name: "exact byte cap with context on both sides",
			raw:  "lead " + start + exactEmoji + end + " tail",
			want: exactEmoji,
		},
		{
			name: "exact scalar cap with leading context only",
			raw:  "lead " + start + exactASCII + end,
			want: exactASCII,
		},
		{
			name: "exact scalar cap with trailing context only",
			raw:  start + exactASCII + end + " tail",
			want: exactASCII,
		},
		{
			name: "exact byte cap with trailing context only",
			raw:  start + exactEmoji + end + " tail",
			want: exactEmoji,
		},
		{
			name: "one scalar under the cap still outranks a single indicator",
			raw: "lead " + start + strings.Repeat("x", searchSnippetMaxRunes-1) +
				end + " tail",
			want: strings.Repeat("x", searchSnippetMaxRunes-1),
		},
		{
			name: "two scalars under the cap leaves room for both indicators",
			raw: "lead " + start + strings.Repeat("x", searchSnippetMaxRunes-2) +
				end + " tail",
			want: "\u2026" + strings.Repeat("x", searchSnippetMaxRunes-2) + "\u2026",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := sanitizeSearchSnippet(parseFixedSearchSnippet(t, test.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("snippet = %q (runes=%d bytes=%d), want %q",
					got, utf8.RuneCountInString(got), len(got), test.want)
			}
			if utf8.RuneCountInString(got) > searchSnippetMaxRunes ||
				len(got) > searchSnippetMaxBytes {
				t.Fatalf("bounds: runes=%d bytes=%d",
					utf8.RuneCountInString(got), len(got))
			}
		})
	}
}

// TestSanitizeMarkedSearchSnippetBoundsPipelineAllocation is the allocation
// contract, measured around the whole production pipeline rather than around
// one stage of it.
//
// FTS5's 32-token fragment bound says nothing about the size of one token, so a
// single multi-megabyte unbroken token reaches this code as one fragment, and
// the marked string the driver returns is that fragment plus markers. Turning
// it into a 200-scalar/800-byte snippet must not copy it: not into a []byte,
// not into a parsed payload, and not into a scalar-per-source-scalar buffer.
// Everything between the driver's string and the published snippet is measured
// here, so a copy at any stage fails.
//
// Two token sizes share one budget. An allocation proportional to the fragment
// cannot pass both, so this proves independence from the input size rather than
// merely picking a generous ceiling.
//
// The scan itself stays linear in the source, which is unavoidable — trailing
// whitespace collapses, so whether any text survives after the window is only
// knowable by reading to the end. Linear *reading* of a string the caller
// already holds is not the same as linear *allocation* on top of it.
func TestSanitizeMarkedSearchSnippetBoundsPipelineAllocation(t *testing.T) {
	start, end := fixedSearchMarkers()

	for _, tokenScalars := range []int{2 << 20, 4 << 20} {
		// One unbroken token, well past any realistic message but exactly what
		// the caps exist to survive.
		token := strings.Repeat("x", tokenScalars)

		for _, test := range []struct {
			name         string
			raw          string
			wantSnippet  string
			wantTotal    int
			wantMatchLen int
		}{
			{
				name:         "marked oversized token",
				raw:          start + token + end,
				wantSnippet:  strings.Repeat("x", searchSnippetMaxRunes-1) + "\u2026",
				wantTotal:    tokenScalars,
				wantMatchLen: tokenScalars,
			},
			{
				name:         "oversized lead before the match",
				raw:          token + " " + start + "needle" + end,
				wantSnippet:  "\u2026" + strings.Repeat("x", 192) + " needle",
				wantTotal:    tokenScalars + 7,
				wantMatchLen: 6,
			},
			{
				name:         "oversized tail after the match",
				raw:          start + "needle" + end + " " + token,
				wantSnippet:  "needle " + strings.Repeat("x", 192) + "\u2026",
				wantTotal:    tokenScalars + 7,
				wantMatchLen: 6,
			},
			{
				name:         "marker free oversized fragment",
				raw:          token,
				wantSnippet:  strings.Repeat("x", searchSnippetMaxRunes-1) + "\u2026",
				wantTotal:    tokenScalars,
				wantMatchLen: tokenScalars,
			},
		} {
			t.Run(fmt.Sprintf("%s/%dMiB", test.name, tokenScalars>>20), func(t *testing.T) {
				// The marked string is the value the driver hands back, built
				// before the measurement opens: it is existing response data,
				// not working memory this pipeline adds.
				raw := test.raw

				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				got, err := sanitizeMarkedSearchSnippet(raw, start, end)
				runtime.ReadMemStats(&after)
				if err != nil {
					t.Fatal(err)
				}
				if allocated := after.TotalAlloc - before.TotalAlloc; allocated >
					searchSnippetPipelineAllocBudgetBytes {
					t.Fatalf("processing a %d byte marked snippet allocated %d bytes, "+
						"want at most %d",
						len(raw), allocated, searchSnippetPipelineAllocBudgetBytes)
				}

				if got != test.wantSnippet {
					t.Fatalf("snippet = %q (runes=%d bytes=%d), want %q",
						got, utf8.RuneCountInString(got), len(got), test.wantSnippet)
				}
				if utf8.RuneCountInString(got) > searchSnippetMaxRunes ||
					len(got) > searchSnippetMaxBytes {
					t.Fatalf("bounds: runes=%d bytes=%d",
						utf8.RuneCountInString(got), len(got))
				}

				// The retained buffer is checked separately, so a failure says
				// whether the window or the pipeline around it grew.
				window, err := normalizeMarkedSnippetWindow(raw, start, end)
				if err != nil {
					t.Fatal(err)
				}
				if len(window.text) > searchSnippetWindowRunes ||
					cap(window.text) > searchSnippetWindowRunes {
					t.Fatalf("window buffer len=%d cap=%d over %d source scalars, "+
						"want at most %d",
						len(window.text), cap(window.text), test.wantTotal,
						searchSnippetWindowRunes)
				}
				if window.totalRunes != test.wantTotal {
					t.Fatalf("totalRunes = %d, want %d", window.totalRunes, test.wantTotal)
				}
				if !window.found {
					t.Fatal("window recorded no retained match")
				}
				if got := window.match.end - window.match.start; got != test.wantMatchLen {
					t.Fatalf("match scalars = %d, want %d", got, test.wantMatchLen)
				}
			})
		}
	}
}

// snippetFuzzSource is a deterministic xorshift64 generator. The differential
// test needs reproducible pseudo-random fragments, not cryptographic ones, and
// an inline generator keeps the corpus identical on every machine and run.
type snippetFuzzSource struct{ state uint64 }

func (s *snippetFuzzSource) intn(n int) int {
	s.state ^= s.state << 13
	s.state ^= s.state >> 7
	s.state ^= s.state << 17
	return int(s.state % uint64(n))
}

// TestSearchSnippetRuneLenChargesUnencodableRunesAsReplacement pins the width
// accounting the byte cap rests on. string() writes U+FFFD for a rune Go cannot
// encode, so charging utf8.RuneLen's -1 would make a window measure smaller
// than the bytes it actually produces.
func TestSearchSnippetRuneLenChargesUnencodableRunesAsReplacement(t *testing.T) {
	for _, test := range []struct {
		name string
		r    rune
		want int
	}{
		{name: "ascii", r: 'x', want: 1},
		{name: "two byte", r: 'é', want: 2},
		{name: "three byte", r: '界', want: 3},
		{name: "four byte", r: '😀', want: 4},
		{name: "replacement character", r: utf8.RuneError, want: 3},
		{name: "unpaired surrogate", r: rune(0xd800), want: 3},
		{name: "out of range", r: rune(0x110000), want: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := searchSnippetRuneLen(test.r); got != test.want {
				t.Fatalf("searchSnippetRuneLen(%U) = %d, want %d",
					test.r, got, test.want)
			}
			if got, want := searchSnippetRuneLen(test.r), len(string(test.r)); got != want {
				t.Fatalf("searchSnippetRuneLen(%U) = %d, but string() writes %d bytes",
					test.r, got, want)
			}
		})
	}
}

// snippetWindowSummary reports a window without its buffer, which can hold
// hundreds of scalars that say nothing about a disagreement.
func snippetWindowSummary(w snippetWindow) string {
	return fmt.Sprintf("{base:%d totalRunes:%d totalBytes:%d match:%+v found:%t}",
		w.base, w.totalRunes, w.totalBytes, w.match, w.found)
}

// snippetFuzzCorpus builds one deterministic pseudo-random marked fragment.
//
// The chunk alphabet covers every sanitization class — plain text, collapsing
// whitespace, multi-byte scalars, invalid UTF-8, controls, and bidi overrides —
// and, crucially for a streaming parser, the *pieces* of multi-byte scalars.
// A lead byte written just before a marker and its continuation bytes written
// just after it put a scalar astride a chunk boundary, which is the one shape
// that can make a per-chunk decoder disagree with a decoder that sees the
// payload as one buffer.
func snippetFuzzCorpus(source *snippetFuzzSource, start, end string) string {
	chunks := []string{
		"a", "bc", "word ", "  ", "\t", "\n", "\u00a0", "😀", "界", "é",
		"\xff", "\xfe\xfd", "\x00", "\x1b", "\u202e", "\u200f", "…", "x",
		"\xe7", "\xe7\x95", "\x8c", "\xf0\x9f", "\x98\x80", "\xc3",
	}
	var raw strings.Builder
	inMatch := false
	chunkCount := source.intn(220)
	for chunk := 0; chunk < chunkCount; chunk++ {
		// Open and close matches at random, always in balanced pairs, so the
		// scanner sees the shape FTS5 emits.
		if !inMatch && source.intn(12) == 0 {
			raw.WriteString(start)
			inMatch = true
		} else if inMatch && source.intn(4) == 0 {
			raw.WriteString(end)
			inMatch = false
		}
		raw.WriteString(strings.Repeat(
			chunks[source.intn(len(chunks))], 1+source.intn(40)))
	}
	if inMatch {
		raw.WriteString(end)
	}
	return raw.String()
}

// TestScanMarkedSearchSnippetMatchesReferenceParser is the differential proof
// that the streaming scanner reads exactly what the materializing parser reads.
//
// The reference parser copies every non-marker byte into a payload buffer and
// records spans into it — the allocation production no longer performs — so
// reassembling the scanner's chunks and their match ranges and comparing the
// two catches any divergence in the state machine, in what counts as payload,
// or in where a match begins and ends.
func TestScanMarkedSearchSnippetMatchesReferenceParser(t *testing.T) {
	start, end := fixedSearchMarkers()
	source := snippetFuzzSource{state: 0x2545f4914f6cdd1d}
	var compared, matched int

	for run := 0; run < 400; run++ {
		raw := snippetFuzzCorpus(&source, start, end)

		parsed, parseErr := parseMarkedSearchSnippet([]byte(raw), start, end)
		segments, shape, scanErr := collectSnippetSegments(raw, start, end)

		if (parseErr == nil) != (scanErr == nil) {
			t.Fatalf("run %d: reference error = %v, scanner error = %v",
				run, parseErr, scanErr)
		}
		if parseErr != nil {
			if !errors.Is(scanErr, ErrInvalidSearchSnippetMarkers) {
				t.Fatalf("run %d: scanner error = %v, want ErrInvalidSearchSnippetMarkers",
					run, scanErr)
			}
			continue
		}
		compared++

		var text strings.Builder
		var matches []byteSpan
		for _, segment := range segments {
			if segment.inMatch {
				matches = append(matches,
					byteSpan{start: text.Len(), end: text.Len() + len(segment.chunk)})
			}
			text.WriteString(segment.chunk)
		}
		if text.String() != string(parsed.text) {
			t.Fatalf("run %d: scanned payload disagrees with the reference parser", run)
		}
		if !reflect.DeepEqual(matches, parsed.matches) {
			t.Fatalf("run %d: scanned matches = %+v, reference = %+v",
				run, matches, parsed.matches)
		}
		if shape.pairs != len(parsed.matches) || shape.payloadBytes != len(parsed.text) {
			t.Fatalf("run %d: shape = %+v, reference had %d pairs over %d payload bytes",
				run, shape, len(parsed.matches), len(parsed.text))
		}
		if len(matches) > 0 {
			matched++
		}
	}

	if compared < 300 || matched < 200 {
		t.Fatalf("corpus too weak: %d fragments parsed and %d carried a marker pair",
			compared, matched)
	}
}

// TestNormalizeMarkedSnippetWindowMatchesUnwindowedNormalization is the
// differential proof that neither streaming the fragment nor bounding the
// working buffer changed anything a caller can observe.
//
// Three runs are compared on every fragment. The production run reads the
// marked string directly and keeps only searchSnippetWindowRunes scalars. The
// parsed run drives the same normalizer from the reference parser's
// materialized payload, so a disagreement there is a streaming bug. The
// reference run drives it from that same payload with a retention radius wider
// than the fragment, so it drops nothing at all, and a disagreement there is a
// windowing bug. The counters, the located match, and above all the published
// snippet must agree across all three, including on fragments long enough to
// force several buffer compactions.
func TestNormalizeMarkedSnippetWindowMatchesUnwindowedNormalization(t *testing.T) {
	start, end := fixedSearchMarkers()
	source := snippetFuzzSource{state: 0x9e3779b97f4a7c15}
	var compacted, published, straddled int

	for run := 0; run < 400; run++ {
		raw := snippetFuzzCorpus(&source, start, end)

		parsed, err := parseMarkedSearchSnippet([]byte(raw), start, end)
		if err != nil {
			// Structurally impossible for a balanced generator except for the
			// empty fragment, which is its own documented failure.
			if !errors.Is(err, ErrInvalidSearchSnippetMarkers) {
				t.Fatalf("run %d: %v", run, err)
			}
			if _, err := normalizeMarkedSnippetWindow(raw, start, end); !errors.Is(
				err, ErrInvalidSearchSnippetMarkers) {
				t.Fatalf("run %d: production error = %v, want the same rejection", run, err)
			}
			continue
		}
		parsed = withWholeFragmentSpan(parsed)

		production, err := normalizeMarkedSnippetWindow(raw, start, end)
		if err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		bounded := normalizeSnippetWindow(parsed)
		// A window past twice the fragment's scalar count can never drop a
		// scalar, because there are fewer scalars than bytes.
		reference := normalizeParsedSnippet(newSnippetNormalizer(2*(len(parsed.text)+1)), parsed)

		if cap(production.text) > searchSnippetWindowRunes ||
			cap(bounded.text) > searchSnippetWindowRunes {
			t.Fatalf("run %d: window cap = %d/%d, want at most %d",
				run, cap(production.text), cap(bounded.text), searchSnippetWindowRunes)
		}
		if reference.base != 0 {
			t.Fatalf("run %d: reference dropped scalars at base %d",
				run, reference.base)
		}
		if production.base != bounded.base ||
			production.totalRunes != bounded.totalRunes ||
			production.totalBytes != bounded.totalBytes ||
			production.found != bounded.found ||
			production.match != bounded.match ||
			string(production.text) != string(bounded.text) {
			t.Fatalf("run %d: streamed %s disagrees with parsed %s",
				run, snippetWindowSummary(production), snippetWindowSummary(bounded))
		}
		if bounded.totalRunes != reference.totalRunes ||
			bounded.totalBytes != reference.totalBytes ||
			bounded.found != reference.found ||
			bounded.match != reference.match {
			t.Fatalf("run %d: bounded %s disagrees with reference %s",
				run, snippetWindowSummary(bounded), snippetWindowSummary(reference))
		}
		got, want := boundSearchSnippet(production), boundSearchSnippet(reference)
		if got != want {
			t.Fatalf("run %d: streamed snippet = %q, unwindowed = %q", run, got, want)
		}
		if bounded.base > 0 {
			compacted++
		}
		if got != "" {
			published++
		}
		if snippetFragmentStraddlesAMarker(raw, start, end) {
			straddled++
		}
	}

	// The corpus has to keep reaching the interesting states, or the agreement
	// above is agreement about nothing.
	if compacted < 40 || published < 200 || straddled < 20 {
		t.Fatalf("corpus too weak: %d runs compacted the buffer, %d published a "+
			"snippet, and %d put a scalar astride a marker", compacted, published, straddled)
	}
}

// snippetFragmentStraddlesAMarker reports whether removing the markers would
// join a partial UTF-8 sequence across a chunk boundary. It exists so the
// differential corpus can prove it keeps producing the shape that separates a
// per-chunk decoder from a whole-payload one.
func snippetFragmentStraddlesAMarker(raw, start, end string) bool {
	for _, marker := range []string{start, end} {
		rest := raw
		for {
			at := strings.Index(rest, marker)
			if at < 0 {
				break
			}
			if at > 0 && lastPartialRune(rest[:at]) != "" {
				return true
			}
			rest = rest[at+len(marker):]
		}
	}
	return false
}

// lastPartialRune returns the trailing bytes of text that are not yet a full
// encoding, or "" when text ends on a scalar boundary.
func lastPartialRune(text string) string {
	for back := 1; back <= utf8.UTFMax && back <= len(text); back++ {
		tail := text[len(text)-back:]
		if utf8.FullRuneInString(tail) {
			return ""
		}
		if utf8.RuneStart(tail[0]) {
			return tail
		}
	}
	return ""
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

// TestSanitizeSearchSnippetSkipsMatchEmptiedBySanitization pins which match the
// window opens on: the first recorded match can normalize away to nothing, and
// the snippet must then be windowed around a later surviving match instead of
// being rejected.
func TestSanitizeSearchSnippetSkipsMatchEmptiedBySanitization(t *testing.T) {
	start, end := fixedSearchMarkers()
	lead := strings.Repeat("z", 400)
	raw := lead + start + " " + end + "ctx" + start + "needle" + end

	parsed := parseFixedSearchSnippet(t, raw)
	if len(parsed.matches) != 2 {
		t.Fatalf("matches = %+v, want two recorded spans", parsed.matches)
	}
	// "z"*400 + " " + "ctx" is 404 scalars, so the emptied first match leaves
	// no span and the retained match is the "needle" that follows it.
	window := normalizeSnippetWindow(parsed)
	if !window.found ||
		!reflect.DeepEqual(window.match, runeSpan{start: 404, end: 410}) {
		t.Fatalf("window match = %+v (found=%t), want the surviving needle span",
			window.match, window.found)
	}
	if window.totalRunes != 410 {
		t.Fatalf("totalRunes = %d, want 410", window.totalRunes)
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

// TestNormalizeSnippetWindowDropsMatchEmptiedByPendingSpace pins the retention
// rule in the normalizer. The match is a single space that collapses into the
// pending space in front of it, so its start index is provisionally past the
// end of the emitted text. No scalar ever lands inside it, so it is not a
// retained match — rather than an empty or inverted span the bounding pass has
// to defend against.
func TestNormalizeSnippetWindowDropsMatchEmptiedByPendingSpace(t *testing.T) {
	start, end := fixedSearchMarkers()
	parsed := parseFixedSearchSnippet(t, "abc "+start+" "+end+"def")

	window := normalizeSnippetWindow(parsed)
	if string(window.text) != "abc def" || window.base != 0 ||
		window.totalRunes != 7 || window.totalBytes != 7 {
		t.Fatalf("window = %+v, want the whole %q", window, "abc def")
	}
	if window.found {
		t.Fatalf("window match = %+v, want no retained match", window.match)
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
// TestWithWholeFragmentSpanOnlyFillsAnAbsentMatch pins the narrowness of the
// windowing hint: it is applied only when FTS5 reported no match at all, and it
// never widens, merges, or reorders spans FTS5 did report.
func TestWithWholeFragmentSpanOnlyFillsAnAbsentMatch(t *testing.T) {
	markerFree := withWholeFragmentSpan(parsedSearchSnippet{text: []byte("plain fragment")})
	if string(markerFree.text) != "plain fragment" {
		t.Fatalf("text = %q, want it unchanged", markerFree.text)
	}
	if !reflect.DeepEqual(markerFree.matches, []byteSpan{{start: 0, end: 14}}) {
		t.Fatalf("matches = %+v, want one whole-fragment span", markerFree.matches)
	}

	marked := parsedSearchSnippet{text: []byte("abcdef"), matches: []byteSpan{{1, 2}, {4, 5}}}
	kept := withWholeFragmentSpan(marked)
	if !reflect.DeepEqual(kept.matches, []byteSpan{{1, 2}, {4, 5}}) {
		t.Fatalf("matches = %+v, want the recorded spans untouched", kept.matches)
	}

	// An empty span is still a span FTS5 recorded, so a match that sanitizes
	// away must not be silently replaced by the whole fragment.
	empty := parsedSearchSnippet{text: []byte("abc"), matches: []byteSpan{{1, 1}}}
	if got := withWholeFragmentSpan(empty); !reflect.DeepEqual(got.matches, []byteSpan{{1, 1}}) {
		t.Fatalf("matches = %+v, want the empty recorded span untouched", got.matches)
	}
}

// TestSanitizeMarkerFreeFragmentStillFailsClosedOnEmptyOutput proves the
// windowing hint does not weaken the empty-snippet guard: a fragment that
// sanitizes to nothing is still a failure, not a hit with blank text.
func TestSanitizeMarkerFreeFragmentStillFailsClosedOnEmptyOutput(t *testing.T) {
	start, end := fixedSearchMarkers()
	for _, test := range []struct {
		name string
		raw  string
	}{
		{name: "whitespace only", raw: " \t\n "},
		{name: "unicode whitespace only", raw: "\u2028\u00a0\u3000"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := parseMarkedSearchSnippet([]byte(test.raw), start, end)
			if err != nil {
				t.Fatalf("parse = %v, want a marker-free success", err)
			}
			got, err := sanitizeSearchSnippet(withWholeFragmentSpan(parsed))
			if !errors.Is(err, ErrInvalidSearchSnippet) {
				t.Fatalf("error = %v, want ErrInvalidSearchSnippet", err)
			}
			if got != "" {
				t.Fatalf("snippet on failure = %q", got)
			}
		})
	}
}

// TestSanitizeMarkerFreeFragmentIsBoundedAndUnhighlighted covers the public
// shape of an over-window excerpt: the caps still hold, the cut edge is marked
// with the same ellipsis every other cut uses, and nothing is emphasized.
func TestSanitizeMarkerFreeFragmentIsBoundedAndUnhighlighted(t *testing.T) {
	start, end := fixedSearchMarkers()
	raw := strings.Repeat("界", 400)

	parsed, err := parseMarkedSearchSnippet([]byte(raw), start, end)
	if err != nil {
		t.Fatalf("parse = %v, want a marker-free success", err)
	}
	got, err := sanitizeSearchSnippet(withWholeFragmentSpan(parsed))
	if err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(got) > searchSnippetMaxRunes || len(got) > searchSnippetMaxBytes {
		t.Fatalf("invalid bounds: runes=%d bytes=%d", utf8.RuneCountInString(got), len(got))
	}
	// The window opens at the fragment's own start, so only the tail is cut.
	if strings.HasPrefix(got, string(searchSnippetEllipsis)) ||
		!strings.HasSuffix(got, string(searchSnippetEllipsis)) {
		t.Fatalf("snippet = %q, want a trailing cut edge only", got)
	}
	if !strings.HasPrefix(got, strings.Repeat("界", 10)) {
		t.Fatalf("snippet = %q, want the fragment's own opening text", got)
	}
	if strings.ContainsAny(got, "<>*[]") || strings.Contains(got, "TURING-FTS5-SNIPPET") {
		t.Fatalf("snippet = %q, want no added emphasis or markers", got)
	}
}

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

// longPhraseSearchFixture builds a message whose exact query phrase is longer
// than FTS5's 32-token snippet window and is buried behind a filler prefix, plus
// the phrase itself.
func longPhraseSearchFixture() (content, phrase string) {
	words := func(format string, count int) []string {
		out := make([]string, 0, count)
		for i := 0; i < count; i++ {
			out = append(out, fmt.Sprintf(format, i))
		}
		return out
	}
	phrase = strings.Join(words("phraseword%02d", 40), " ")
	content = strings.Join(words("fillerword%03d", 100), " ") +
		" " + phrase + " " + strings.Join(words("trailword%02d", 20), " ")
	return content, phrase
}

// TestSearchMessageHitsReturnBoundedSnippetWhenPhraseExceedsFTS5Window pins the
// marker-free outcome end to end. `snippet(..., 32)` can only mark a phrase that
// fits inside its 32-token window, so a 40-token exact phrase legitimately comes
// back with zero markers. The legacy projection returns the row, so the hit
// projection must return it too — as a bounded, unhighlighted excerpt of that
// same message rather than an internal failure.
func TestSearchMessageHitsReturnBoundedSnippetWhenPhraseExceedsFTS5Window(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	content, phrase := longPhraseSearchFixture()
	if got := len(strings.Fields(phrase)); got != 40 {
		t.Fatalf("phrase token count = %d, want 40 so it exceeds the 32-token window", got)
	}
	const neighborSecret = "longphraseneighborsecret"
	insertSearchSession(t, ctx, database, "s1")
	insertSearchMessage(t, ctx, database, "m-long-phrase", "s1", content, 1)
	insertSearchMessage(t, ctx, database, "m-long-phrase-neighbor", "s1", neighborSecret+" untouched", 2)

	// The marked projection really does come back without a marker pair: the
	// assertions below would otherwise prove nothing about this window.
	start, end, err := newSearchSnippetMarkers(deterministicSearchEntropy(0x3c))
	if err != nil {
		t.Fatal(err)
	}
	var marked string
	if err := database.QueryRowContext(ctx, `
		SELECT snippet(messages_fts, 0, ?, ?, '…', 32)
		FROM messages_fts
		JOIN messages m ON m.rowid = messages_fts.rowid
		WHERE messages_fts MATCH ? AND m.id = 'm-long-phrase'`,
		start, end, fts5Phrase(phrase),
	).Scan(&marked); err != nil {
		t.Fatalf("select marked snippet: %v", err)
	}
	if strings.Contains(marked, start) || strings.Contains(marked, end) {
		t.Fatalf("fixture no longer exercises the marker-free window: %q", marked)
	}

	messages, err := repo.SearchMessages(ctx, "", "", phrase, 10)
	if err != nil {
		t.Fatalf("SearchMessages: %v", err)
	}
	assertSearchMessageIDs(t, messages, []string{"m-long-phrase"})

	hits, err := repo.SearchMessageHits(ctx, "", "", phrase, 10)
	if err != nil {
		t.Fatalf("SearchMessageHits: %v", err)
	}
	assertSearchHitIDs(t, hits, []string{"m-long-phrase"})
	if !reflect.DeepEqual(hits[0].Message, messages[0]) {
		t.Fatalf("hit message = %+v, legacy message = %+v", hits[0].Message, messages[0])
	}
	assertPublicScore(t, hits[0].Score)
	assertPublicSnippet(t, hits[0].Snippet)

	snippet := hits[0].Snippet
	excerpt := strings.TrimSpace(strings.Trim(snippet, string(searchSnippetEllipsis)))
	if excerpt == "" {
		t.Fatalf("snippet = %q, want text beyond the edge ellipses", snippet)
	}
	if !strings.Contains(hits[0].Message.Content, excerpt) {
		t.Fatalf("snippet %q is not a fragment of its own message %q", snippet, hits[0].Message.Content)
	}
	if strings.Contains(snippet, neighborSecret) {
		t.Fatalf("snippet = %q, want no neighbor content", snippet)
	}
	if strings.Contains(snippet, "TURING-FTS5-SNIPPET") {
		t.Fatalf("snippet = %q leaks internal marker bytes", snippet)
	}
}

// measureSearchAllocation reports the heap one query claims. TotalAlloc is
// cumulative and unaffected by collection, so a GC landing inside the window
// cannot make the reading look smaller than it was.
func measureSearchAllocation(t *testing.T, run func() error) uint64 {
	t.Helper()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	err := run()
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	return after.TotalAlloc - before.TotalAlloc
}

// TestSearchMessageHitsBoundPerRowSnippetAllocation is the end-to-end form of
// the allocation contract: the row travels through the real driver, the real
// scan, and the real snippet pipeline.
//
// The row's own bytes are charged twice by any hit query and cannot be avoided:
// the driver returns the message content, and it returns the marked snippet
// alongside it, which for one unbroken token is the whole row again. What must
// not appear is a *third* copy — the []byte conversion and the parsed payload
// the streaming pipeline replaced.
//
// The budget is stated against the legacy projection over the same row rather
// than as an absolute figure. Legacy returns that row without a snippet column,
// so it measures exactly what this driver costs to deliver these bytes today,
// and a driver or SQLite change moves both sides together instead of turning
// this into a maintenance burden or a silent pass.
func TestSearchMessageHitsBoundPerRowSnippetAllocation(t *testing.T) {
	for _, contentBytes := range []int{1 << 18, 1 << 20} {
		t.Run(fmt.Sprintf("%dKiB", contentBytes>>10), func(t *testing.T) {
			database := openTestDB(t)
			repo := New(database)
			ctx := context.Background()
			// One unbroken token. FTS5's 32-token window cannot cut it, so the
			// marked snippet SQLite returns is the entire row.
			token := strings.Repeat("z", contentBytes)
			insertSearchSession(t, ctx, database, "s1")
			insertSearchMessage(t, ctx, database, "m-alloc", "s1", token, 1)

			// Warm both statements so one-time preparation and page-cache
			// costs are not charged to the measured runs.
			if _, err := repo.SearchMessageHits(ctx, "", "", token, 10); err != nil {
				t.Fatal(err)
			}
			if _, err := repo.SearchMessages(ctx, "", "", token, 10); err != nil {
				t.Fatal(err)
			}

			// Background work can only ever *add* to a TotalAlloc delta, so the
			// smallest of several readings is the best estimate of what each
			// query really costs. Taking the minimum on both sides tightens the
			// budget rather than loosening it, so this removes flakiness
			// without weakening the guard.
			var hits []SearchHit
			hitAlloc, legacyAlloc := ^uint64(0), ^uint64(0)
			for reading := 0; reading < 3; reading++ {
				hitAlloc = min(hitAlloc, measureSearchAllocation(t, func() error {
					var err error
					hits, err = repo.SearchMessageHits(ctx, "", "", token, 10)
					return err
				}))
				legacyAlloc = min(legacyAlloc, measureSearchAllocation(t, func() error {
					_, err := repo.SearchMessages(ctx, "", "", token, 10)
					return err
				}))
			}

			assertSearchHitIDs(t, hits, []string{"m-alloc"})
			assertPublicSnippet(t, hits[0].Snippet)

			// An unanchored baseline would let a regression hide inside it.
			if legacyAlloc > uint64(searchHitRowLegacyAllocCeiling*contentBytes) {
				t.Fatalf("legacy baseline allocated %d bytes for a %d byte row, "+
					"want at most %dx it — the comparison below is no longer anchored",
					legacyAlloc, contentBytes, searchHitRowLegacyAllocCeiling)
			}
			budget := legacyAlloc + uint64(searchHitRowSnippetAllocFactor*contentBytes)
			if hitAlloc > budget {
				t.Fatalf("hit query allocated %d bytes for a %d byte row against a "+
					"%d byte legacy baseline, want at most %d (%dx the row above "+
					"the baseline)",
					hitAlloc, contentBytes, legacyAlloc, budget,
					searchHitRowSnippetAllocFactor)
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
// markers, which is the same shape a phrase wider than the snippet window
// produces, so it returns an unhighlighted excerpt rather than failing; a
// same-shape replacement can still produce balanced markers around stale
// offsets. In both cases MEM-002 only guarantees that the text stays confined
// to the returned message, never that the excerpt shows a real match.
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
	if err != nil {
		t.Fatalf("shortened row: %v", err)
	}
	assertSearchHitIDs(t, hits, []string{"m-divergent-short"})
	assertPublicSnippet(t, hits[0].Snippet)
	if hits[0].Snippet != "alpha" {
		t.Fatalf("snippet = %q, want only the returned row's own current content", hits[0].Snippet)
	}
	if strings.Contains(hits[0].Snippet, secret) {
		t.Fatalf("snippet = %q, want no neighbor content", hits[0].Snippet)
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
