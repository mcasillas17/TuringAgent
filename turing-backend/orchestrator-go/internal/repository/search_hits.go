package repository

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Errors returned by the search hit primitives. Every one of them fails the
// whole query: a search that cannot prove its own score or snippet is safe
// returns nothing rather than a success-shaped guess.
var (
	ErrInvalidSearchScore           = errors.New("invalid search score")
	ErrSearchMarkerEntropy          = errors.New("search marker entropy unavailable")
	ErrSearchSnippetMarkerCollision = errors.New("search snippet marker collision")
	ErrInvalidSearchSnippetMarkers  = errors.New("invalid search snippet markers")
	ErrInvalidSearchSnippet         = errors.New("invalid search snippet")
)

// searchSnippetMarkerNonceBytes is the entropy width behind a marker nonce.
// 128 bits makes an accidental collision with real message content, or a guess
// by whoever wrote that content, not worth reasoning about further.
const searchSnippetMarkerNonceBytes = 16

// The public snippet is bounded twice. The scalar cap is what a person reads;
// the byte cap is what a transport and a database column carry. FTS5's own
// token limit bounds neither, because one unbroken token can be arbitrarily
// long.
const (
	searchSnippetMaxRunes = 200
	searchSnippetMaxBytes = 800
)

// searchSnippetEllipsis marks a cut edge. It is the same code point SQLite is
// asked to use for its own omissions, so a source-authored ellipsis and an
// inserted one are indistinguishable — which is fine, because neither has any
// control meaning.
const (
	searchSnippetEllipsis      = '\u2026'
	searchSnippetEllipsisBytes = 3
)

// runeSpan is a half-open range over sanitized snippet runes. Bounds are
// enforced in scalars, so match positions have to be tracked in scalars too.
type runeSpan struct {
	start int
	end   int
}

const (
	searchSnippetStartPrefix  = "[[TURING-FTS5-SNIPPET-START:v1:"
	searchSnippetEndPrefix    = "[[TURING-FTS5-SNIPPET-END:v1:"
	searchSnippetMarkerSuffix = "]]"
)

// byteSpan is a half-open range over the parsed snippet's raw bytes, recorded
// after marker removal so offsets refer to the payload the caller keeps.
type byteSpan struct {
	start int
	end   int
}

// parsedSearchSnippet is snippet payload with its internal match spans. The
// markers themselves are already gone; the spans are the only surviving record
// of what FTS5 matched, and they never cross the repository boundary.
type parsedSearchSnippet struct {
	text    []byte
	matches []byteSpan
}

// normalizeSearchScore turns SQLite's bm25() value into the public score.
//
// FTS5 multiplies the conventional BM25 formula by -1 so that ascending order
// is best-first, which means the raw value is finite and non-positive. The
// public contract is the opposite direction — larger is better — so this
// negates it. This is direction normalization only: the result is not a
// probability, a percentage, or comparable across queries or snapshots.
//
// Non-positivity is a pinned assumption about the linked driver, not a
// portable promise of every BM25 implementation, so an unexpected positive or
// non-finite value fails the query instead of being clamped into a plausible
// looking number.
func normalizeSearchScore(raw float64) (float64, error) {
	if math.IsNaN(raw) || math.IsInf(raw, 0) || raw > 0 {
		return 0, ErrInvalidSearchScore
	}
	score := -raw
	// Negating +0 yields -0, which compares equal to 0 but serializes and
	// prints differently. Canonicalize it so no client ever sees "-0".
	if score == 0 {
		score = 0
	}
	if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || math.Signbit(score) {
		return 0, ErrInvalidSearchScore
	}
	return score, nil
}

// newSearchSnippetMarkers builds the per-query marker pair handed to FTS5's
// snippet(). Both values are bound SQL TEXT, never query text.
//
// The grammar is deliberately plain single-byte ASCII: no NUL, no control
// character, no non-ASCII code point, and nothing locale dependent. That is
// what lets the parser run on raw bytes before UTF-8 repair and still be sure
// that broken source encoding cannot be repaired into a marker.
//
// The nonce is per query rather than a fixed constant so that message content
// cannot be authored to impersonate a marker; entropy is a parameter rather
// than a package global so tests stay deterministic without a mutable seam.
func newSearchSnippetMarkers(entropy io.Reader) (string, string, error) {
	var seed [searchSnippetMarkerNonceBytes]byte
	if _, err := io.ReadFull(entropy, seed[:]); err != nil {
		// Only the reader's own error is wrapped; the partially read bytes are
		// never put in a message, since they would otherwise reach logs.
		return "", "", fmt.Errorf("%w: %v", ErrSearchMarkerEntropy, err)
	}
	nonce := hex.EncodeToString(seed[:])
	return searchSnippetStartPrefix + nonce + searchSnippetMarkerSuffix,
		searchSnippetEndPrefix + nonce + searchSnippetMarkerSuffix,
		nil
}

// parseMarkedSearchSnippet strips the marker pair FTS5 wrapped around each
// matched phrase and records where those phrases ended up in the payload.
//
// It is a two-state machine over raw bytes — text and match — and only the
// exact start marker may open a match, only the exact end marker may close
// one. An end marker in text, a start marker inside a match, a missing
// partner, or a snippet still in match state at the end all fail: FTS5 emits
// balanced markers, so a structurally broken one means the value did not come
// from the projection we asked for, and guessing which bytes were inserted is
// exactly the guess this design refuses to make.
//
// A snippet carrying no complete marker at all is a different case, and it is
// valid. `snippet()` can only mark a phrase that fits inside its token window,
// so an exact phrase longer than that window legitimately yields an unmarked
// fragment of the same matched row. The payload is returned as-is with no match
// spans; only a completely empty snippet still fails, because FTS5 returns some
// text for every row it matches.
//
// Running before rune decoding is deliberate. Marker bytes are removed while
// the input is still bytes, so invalid UTF-8 next to a marker cannot absorb
// marker bytes, turn a start into an end, or leave marker fragments in public
// output.
func parseMarkedSearchSnippet(raw []byte, start, end string) (parsedSearchSnippet, error) {
	// The state machine only terminates because every iteration consumes a
	// marker, and it can only distinguish the two states because the markers
	// differ. An empty marker matches at every offset and consumes nothing, so
	// the loop would spin forever; identical markers would let the parser
	// invent match boundaries and report success. newSearchSnippetMarkers can
	// never produce either, so this rejects a caller mistake at the door rather
	// than letting it become a hang or a silently wrong snippet.
	if start == "" || end == "" || start == end {
		return parsedSearchSnippet{}, fmt.Errorf(
			"%w: empty or identical markers", ErrInvalidSearchSnippetMarkers)
	}

	startMarker := []byte(start)
	endMarker := []byte(end)

	text := make([]byte, 0, len(raw))
	var matches []byteSpan
	matchStart := 0
	inMatch := false

	for len(raw) > 0 {
		nextStart := bytes.Index(raw, startMarker)
		nextEnd := bytes.Index(raw, endMarker)

		if !inMatch {
			if nextStart < 0 {
				if nextEnd >= 0 {
					return parsedSearchSnippet{}, fmt.Errorf(
						"%w: end marker outside a match", ErrInvalidSearchSnippetMarkers)
				}
				text = append(text, raw...)
				break
			}
			if nextEnd >= 0 && nextEnd < nextStart {
				return parsedSearchSnippet{}, fmt.Errorf(
					"%w: end marker before start marker", ErrInvalidSearchSnippetMarkers)
			}
			text = append(text, raw[:nextStart]...)
			raw = raw[nextStart+len(startMarker):]
			matchStart = len(text)
			inMatch = true
			continue
		}

		if nextEnd < 0 {
			return parsedSearchSnippet{}, fmt.Errorf(
				"%w: start marker without end marker", ErrInvalidSearchSnippetMarkers)
		}
		if nextStart >= 0 && nextStart < nextEnd {
			return parsedSearchSnippet{}, fmt.Errorf(
				"%w: nested start marker", ErrInvalidSearchSnippetMarkers)
		}
		text = append(text, raw[:nextEnd]...)
		raw = raw[nextEnd+len(endMarker):]
		matches = append(matches, byteSpan{start: matchStart, end: len(text)})
		inMatch = false
	}

	if inMatch {
		return parsedSearchSnippet{}, fmt.Errorf(
			"%w: snippet ended inside a match", ErrInvalidSearchSnippetMarkers)
	}
	if len(text) == 0 && len(matches) == 0 {
		// FTS5 returns some text for every row it matches, so nothing at all is
		// not a windowing outcome — it is a projection that did not come from
		// the query we asked for.
		return parsedSearchSnippet{}, fmt.Errorf(
			"%w: empty snippet", ErrInvalidSearchSnippetMarkers)
	}
	return parsedSearchSnippet{text: text, matches: matches}, nil
}

// rejectSearchSnippetMarkerCollision refuses a row whose own content already
// contains one of this query's markers.
//
// FTS5 inserts the markers into a projection of the same content, so if the
// content carries a complete marker there is no way to tell an inserted marker
// from an authored one, and the parser would silently believe the wrong match
// boundaries. A 128-bit nonce makes that effectively impossible to provoke, so
// the check exists to fail closed rather than to be hit. Only complete markers
// count: marker-shaped text with a different nonce is ordinary content.
func rejectSearchSnippetMarkerCollision(content, start, end string) error {
	if strings.Contains(content, start) || strings.Contains(content, end) {
		return ErrSearchSnippetMarkerCollision
	}
	return nil
}

// sanitizeSearchSnippet turns parsed snippet payload into the public snippet:
// valid UTF-8, single-line plain text, bounded, and still showing what matched.
//
// Empty output, or output that no longer contains any matched text, is a
// failure rather than a hit with invented or match-less text.
func sanitizeSearchSnippet(parsed parsedSearchSnippet) (string, error) {
	snippet := boundSearchSnippet(normalizeSnippetWindow(parsed))
	if snippet == "" {
		return "", ErrInvalidSearchSnippet
	}
	// Re-check the public result rather than trusting the construction above:
	// this is the value that leaves the repository.
	if !utf8.ValidString(snippet) ||
		utf8.RuneCountInString(snippet) > searchSnippetMaxRunes ||
		len(snippet) > searchSnippetMaxBytes {
		return "", ErrInvalidSearchSnippet
	}
	return snippet, nil
}

// searchSnippetWindowRunes is the ceiling on how many sanitized scalars the
// repository holds at once while preparing one snippet, whatever the source
// fragment weighs.
//
// FTS5's 32-token fragment bound says nothing about how long one token is, so a
// single unbroken multi-megabyte token arrives here as one fragment. Every
// window this package can publish lies inside
// [match start - searchSnippetMaxRunes, match start + searchSnippetMaxRunes):
// the output is at most searchSnippetMaxRunes scalars and always covers at
// least one scalar of the match, so neither edge can travel a full cap away
// from the match start. Retaining exactly that span, and counting the rest,
// makes the working set a fixed 400 scalars rather than a copy of the input.
const searchSnippetWindowRunes = 2 * searchSnippetMaxRunes

// snippetWindow is the bounded working representation of a sanitized snippet.
//
// text holds the sanitized scalars for global indices [base, base+len(text)),
// which is a window around the first retained match rather than the whole
// fragment. The counters describe everything sanitization saw, so the bounding
// pass can still tell whether text was dropped at either edge without holding
// it.
type snippetWindow struct {
	text       []rune
	base       int
	totalRunes int
	totalBytes int
	// match is the first retained match in global scalar indices. It is only
	// meaningful when found is set, and its end can lie beyond the retained
	// window when one matched token is longer than the caps.
	match runeSpan
	found bool
}

// searchSnippetRuneLen is the UTF-8 width string() would write for r. A rune Go
// cannot encode is written as U+FFFD, so it is charged that width.
func searchSnippetRuneLen(r rune) int {
	if width := utf8.RuneLen(r); width > 0 {
		return width
	}
	return utf8.RuneLen(utf8.RuneError)
}

// snippetNormalizer repairs and flattens snippet payload into a bounded
// snippetWindow, locating the first match that survives sanitization as it
// goes.
//
// The rules are the design's, in this order: invalid UTF-8 becomes U+FFFD;
// Unicode whitespace and line separators become one ASCII space, collapsed and
// trimmed; remaining C0/C1/DEL controls become U+FFFD; and explicit bidi
// controls become U+FFFD. Whitespace is handled first so that a tab, newline,
// or U+0085 becomes a readable space instead of a replacement character.
//
// Natural right-to-left letters and joiners are content, not formatting, and
// are deliberately left alone: stripping them would corrupt Arabic, Hebrew, and
// emoji sequences to defend against an override the explicit-control rule
// already removes.
type snippetNormalizer struct {
	// retain is how many sanitized scalars are kept on each side of the match
	// start, so the buffer is twice it. Production derives it from
	// searchSnippetWindowRunes; it is a parameter rather than a constant read
	// here so the bounded window can be differentially checked against an
	// unwindowed run of this same code, with no mutable package state and no
	// production seam.
	retain       int
	window       snippetWindow
	pendingSpace bool
	// openSpan is the global scalar index where the current match opened, or
	// -1 when no match is open.
	openSpan int
	// nextMatch is the byte span being tracked, and complete records that the
	// first retained match has already closed. Later matches are irrelevant:
	// the window opens on the first one.
	nextMatch int
	complete  bool
}

// normalizeSnippetWindow sanitizes parsed snippet payload into a window bounded
// by searchSnippetWindowRunes.
//
// The source is read once, byte by byte, which is unavoidable: trailing
// whitespace collapses away, so whether any text survives after the window can
// only be known by reading to the end. Reading a buffer the caller already
// holds is not the same as allocating a second one the size of it.
func normalizeSnippetWindow(parsed parsedSearchSnippet) snippetWindow {
	return newSnippetNormalizer(searchSnippetWindowRunes).normalize(parsed)
}

// newSnippetNormalizer builds a normalizer whose retained buffer is exactly
// width scalars: half reaching back from the match start and half forward from
// it, which is the split the caps justify. A test can pass a width wider than
// the fragment to get an unwindowed reference run of this same code.
func newSnippetNormalizer(width int) *snippetNormalizer {
	return &snippetNormalizer{
		retain:   width / 2,
		window:   snippetWindow{text: make([]rune, 0, width)},
		openSpan: -1,
	}
}

func (n *snippetNormalizer) normalize(parsed parsedSearchSnippet) snippetWindow {
	invalidRun := false
	for offset := 0; offset < len(parsed.text); {
		n.applySpanBoundaries(parsed.matches, offset)

		decoded, width := utf8.DecodeRune(parsed.text[offset:])
		if decoded == utf8.RuneError && width <= 1 {
			// One maximal run of invalid bytes becomes a single U+FFFD, which
			// is what strings.ToValidUTF8 does; offsets still advance one byte
			// at a time so a span boundary inside the run is not skipped.
			if !invalidRun {
				n.emit(utf8.RuneError)
				invalidRun = true
			}
			offset++
			continue
		}
		invalidRun = false

		switch {
		case unicode.IsSpace(decoded):
			// Leading whitespace is dropped, and a pending space that is never
			// followed by content trims the tail.
			if n.window.totalRunes > 0 {
				n.pendingSpace = true
			}
		case isSearchSnippetControl(decoded), isSearchSnippetBidiControl(decoded):
			n.emit(utf8.RuneError)
		default:
			n.emit(decoded)
		}
		offset += width
	}
	n.applySpanBoundaries(parsed.matches, len(parsed.text))

	return n.window
}

func (n *snippetNormalizer) emit(r rune) {
	if n.pendingSpace {
		n.pendingSpace = false
		n.push(' ')
	}
	n.push(r)
}

// nextRuneIndex is where the next emitted scalar will land, including a space
// that is still pending. Using it as a match start keeps a span over the
// match's own scalars instead of the collapsed space in front of it.
func (n *snippetNormalizer) nextRuneIndex() int {
	if n.pendingSpace {
		return n.window.totalRunes + 1
	}
	return n.window.totalRunes
}

// push counts one sanitized scalar and retains it only while it can still
// appear in some legal output window.
//
// A match is "retained" exactly when a scalar lands at or after the index its
// span opened on. That replaces an after-the-fact emptiness check: a match made
// only of controls or collapsed whitespace never receives a scalar, so it is
// never mistaken for evidence of a match, and a start index provisionally past
// the emitted text can never become an inverted span.
func (n *snippetNormalizer) push(r rune) {
	index := n.window.totalRunes
	n.window.totalRunes++
	n.window.totalBytes += searchSnippetRuneLen(r)

	if !n.window.found && n.openSpan >= 0 && index >= n.openSpan {
		n.window.found = true
		n.window.match = runeSpan{start: n.openSpan, end: n.openSpan}
		// The match start is now known, so everything a window can never reach
		// again is dropped in one shift, leaving room for the scalars after it.
		n.compact()
	}
	if n.window.found && index >= n.window.match.start+n.retain {
		// Past the furthest scalar any legal window reaches. It still counts
		// toward the totals, but it is not stored.
		return
	}

	if len(n.window.text) == cap(n.window.text) {
		n.compact()
		if len(n.window.text) == cap(n.window.text) {
			// Unreachable given the bound above: refusing to grow keeps
			// searchSnippetWindowRunes an absolute ceiling rather than a
			// ceiling that holds only while the proof does.
			return
		}
	}
	n.window.text = append(n.window.text, r)
}

// compact drops retained scalars below the floor any future window can reach,
// shifting the remainder down inside the same backing array.
//
// Before a match is found the floor trails the scan by retain scalars, because a
// match starting at the very next scalar could still window back that far. Once
// the match start is known the floor is fixed, so this shifts at most once more
// and the remaining retain scalars after the match always have room.
func (n *snippetNormalizer) compact() {
	floor := n.window.totalRunes - 1 - n.retain
	if n.window.found {
		floor = n.window.match.start - n.retain
	}
	drop := floor - n.window.base
	if drop <= 0 {
		return
	}
	if drop > len(n.window.text) {
		drop = len(n.window.text)
	}
	n.window.text = n.window.text[:copy(n.window.text, n.window.text[drop:])]
	n.window.base += drop
}

// applySpanBoundaries closes and opens every match span that lands on this byte
// offset, so spans that touch after marker removal stay separate. Tracking
// stops once the first retained match closes.
func (n *snippetNormalizer) applySpanBoundaries(matches []byteSpan, offset int) {
	for !n.complete {
		if n.openSpan < 0 && n.nextMatch < len(matches) &&
			matches[n.nextMatch].start <= offset {
			n.openSpan = n.nextRuneIndex()
			continue
		}
		if n.openSpan >= 0 && n.nextMatch < len(matches) &&
			matches[n.nextMatch].end <= offset {
			if n.window.found {
				n.window.match.end = n.window.totalRunes
				n.complete = true
			}
			n.openSpan = -1
			n.nextMatch++
			continue
		}
		return
	}
}

// isSearchSnippetControl reports the C0, C1, and DEL code points. Whitespace
// controls are classified before this is consulted.
func isSearchSnippetControl(r rune) bool {
	return r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}

// isSearchSnippetBidiControl reports the explicit bidirectional formatting
// controls. They can reorder rendered text against its logical order, which
// would let a search result display something other than what was stored.
func isSearchSnippetBidiControl(r rune) bool {
	switch {
	case r == '\u061c', r == '\u200e', r == '\u200f':
		return true
	case r >= '\u202a' && r <= '\u202e':
		return true
	case r >= '\u2066' && r <= '\u2069':
		return true
	default:
		return false
	}
}

// boundSearchSnippet cuts a sanitized window down to a snippet that respects
// both caps while keeping matched text visible. It returns "" when no window
// can satisfy that, which the caller turns into ErrInvalidSearchSnippet.
//
// The priority order is deliberate. A sanitized match that fits both caps on
// its own is published complete: it is the reason the snippet exists, so it
// outranks the U+2026 cut indicators around it. Those indicators are added only
// when every edge that needs one can be paid for beside the whole match, and
// are dropped together otherwise, because a snippet that marks one cut edge and
// silently swallows the other describes the source less honestly than one that
// marks neither. Context grows outward only after the whole match is secured,
// and every scalar of context is charged for its indicators as it is taken.
//
// When one matched token is larger than the caps on its own the bounds win and
// the largest prefix that fits stays visible, with indicators charged as usual,
// because an unbounded snippet is the thing this function exists to prevent.
// Every cut lands on a scalar boundary.
func boundSearchSnippet(window snippetWindow) string {
	if !window.found || window.totalRunes == 0 {
		return ""
	}
	matchRunes := window.match.end - window.match.start
	if matchRunes <= 0 {
		return ""
	}
	// Nothing was dropped at either edge and both caps already hold, so the
	// whole fragment is the snippet. A marker-free fragment reaches this with
	// its implicit whole-fragment span and is published unwindowed.
	if window.base == 0 &&
		window.totalRunes <= searchSnippetMaxRunes &&
		window.totalBytes <= searchSnippetMaxBytes {
		return string(window.text)
	}

	text := window.text
	// Prefix byte offsets keep every window measurement O(1); string(text[a:b])
	// per candidate window would be quadratic across the retained window.
	offsets := make([]int, len(text)+1)
	for i, r := range text {
		offsets[i+1] = offsets[i] + searchSnippetRuneLen(r)
	}
	// The retained window is a slice of a longer fragment, so a cut edge is a
	// question about the global position, not about this buffer.
	cutBefore := func(low int) bool { return window.base+low > 0 }
	cutAfter := func(high int) bool { return window.base+high < window.totalRunes }
	fits := func(low, high int) bool {
		runes := high - low
		size := offsets[high] - offsets[low]
		if cutBefore(low) {
			runes++
			size += searchSnippetEllipsisBytes
		}
		if cutAfter(high) {
			runes++
			size += searchSnippetEllipsisBytes
		}
		return runes <= searchSnippetMaxRunes && size <= searchSnippetMaxBytes
	}

	low := window.match.start - window.base
	high := low + matchRunes
	if high > len(text) {
		// Only an oversized match can outrun the retained window, and that
		// match is about to be truncated anyway.
		high = len(text)
	}
	if matchRunes <= searchSnippetMaxRunes &&
		offsets[high]-offsets[low] <= searchSnippetMaxBytes {
		// Grow context evenly so a match keeps its surroundings on both sides
		// when there is room for them once its indicators are paid for.
		for {
			grew := false
			if low > 0 && fits(low-1, high) {
				low--
				grew = true
			}
			if high < len(text) && fits(low, high+1) {
				high++
				grew = true
			}
			if !grew {
				break
			}
		}
	} else {
		for high > low && !fits(low, high) {
			high--
		}
		if high <= low {
			return ""
		}
	}

	// Indicators are affordable for every window reached by growth or
	// truncation above; only an exactly-fitting match left alone can fail this.
	indicate := fits(low, high)
	if indicate {
		// Trim space that would otherwise sit against an inserted ellipsis.
		// This only shrinks the window, so both caps still hold. With no
		// ellipsis to sit against there is nothing to trim, and trimming would
		// eat into the complete match this branch exists to preserve.
		if cutBefore(low) {
			for low < high && text[low] == ' ' {
				low++
			}
		}
		if cutAfter(high) {
			for high > low && text[high-1] == ' ' {
				high--
			}
		}
	}
	matchLow := window.match.start - window.base
	matchHigh := matchLow + matchRunes
	if high <= low || max(low, matchLow) >= min(high, matchHigh) {
		return ""
	}

	var bounded strings.Builder
	if indicate && cutBefore(low) {
		bounded.WriteRune(searchSnippetEllipsis)
	}
	bounded.WriteString(string(text[low:high]))
	if indicate && cutAfter(high) {
		bounded.WriteRune(searchSnippetEllipsis)
	}
	return bounded.String()
}

// withWholeFragmentSpan gives a marker-free fragment one implicit match span
// covering the whole payload.
//
// The bounding pass windows around a match, so with no span at all it would
// reject a fragment that is perfectly good text. The span is purely a
// windowing hint for that pass: it is never published, adds no emphasis to the
// public snippet, and the text it covers is still only the matched message's
// own content. A fragment with real marker spans is returned untouched, so this
// can never widen or merge a match FTS5 actually reported.
func withWholeFragmentSpan(parsed parsedSearchSnippet) parsedSearchSnippet {
	if len(parsed.matches) > 0 {
		return parsed
	}
	parsed.matches = []byteSpan{{start: 0, end: len(parsed.text)}}
	return parsed
}

// SearchHit is one ranked search result: the same message the legacy search
// returns, plus the ranking and preview values that only make sense in the
// context of the query that produced them. Neither is persisted, and neither is
// comparable across queries or database snapshots.
type SearchHit struct {
	Message Message
	Score   float64
	Snippet string
}

// SearchMessageHits runs a message search and returns each result with its
// score and a bounded snippet.
//
// It is additive: the legacy projection keeps returning whole messages with no
// ranking metadata, and both share one predicate so the two searches can never
// disagree about which sessions and messages are visible.
func (r *Repository) SearchMessageHits(
	ctx context.Context,
	sessionID string,
	excludedSessionID string,
	query string,
	limit int,
) ([]SearchHit, error) {
	return r.searchMessageHits(ctx, sessionID, excludedSessionID, query, limit, rand.Reader)
}

// searchMessageHits is SearchMessageHits with its entropy source as a
// parameter. Tests pin the marker nonce through it; the exported method is the
// only production entry point and always uses crypto/rand, so there is no
// exported seam that could weaken markers at runtime.
//
// The whole result set comes from one statement over one reader. The
// orchestrator runs a single SQLite connection, so a per-row query or nested
// repository call while this reader is open would deadlock against itself and
// hold the connection away from deletion and lifecycle writers.
func (r *Repository) searchMessageHits(
	ctx context.Context,
	sessionID string,
	excludedSessionID string,
	query string,
	limit int,
	entropy io.Reader,
) ([]SearchHit, error) {
	predicate, predicateArgs, ok := searchMessagesPredicate(searchMessagesInput{
		sessionID:         sessionID,
		excludedSessionID: excludedSessionID,
		query:             query,
		limit:             limit,
	})
	if !ok {
		return []SearchHit{}, nil
	}
	// Markers are built before the reader opens: a random-source failure must
	// fail the request rather than hold a connection.
	start, end, err := newSearchSnippetMarkers(entropy)
	if err != nil {
		return nil, err
	}

	// The marker placeholders sit in the SELECT list, so their arguments come
	// before the predicate's phrase, scope, and limit arguments.
	sqlQuery := `
		SELECT
			m.id, m.session_id, COALESCE(m.run_id, ''), m.role, m.content,
			m.content_type, m.sequence, m.created_at,
			bm25(messages_fts),
			snippet(messages_fts, 0, ?, ?, '…', 32)` + predicate
	args := append([]any{start, end}, predicateArgs...)

	rows, err := r.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	hits := make([]SearchHit, 0)
	for rows.Next() {
		var (
			message       Message
			rawScore      float64
			markedSnippet string
		)
		if err := rows.Scan(
			&message.MessageID, &message.SessionID, &message.RunID,
			&message.Role, &message.Content, &message.ContentType,
			&message.Sequence, &message.CreatedAt,
			&rawScore, &markedSnippet,
		); err != nil {
			return nil, err
		}
		// Every step below fails the whole query rather than dropping a row or
		// emitting a plausible-looking default: a result that cannot prove its
		// own score and snippet is worse than no result at all.
		if err := rejectSearchSnippetMarkerCollision(message.Content, start, end); err != nil {
			return nil, err
		}
		score, err := normalizeSearchScore(rawScore)
		if err != nil {
			return nil, err
		}
		parsed, err := parseMarkedSearchSnippet([]byte(markedSnippet), start, end)
		if err != nil {
			return nil, err
		}
		// A marker-free fragment is windowed as a whole rather than around a
		// match FTS5 never reported. The result is an unhighlighted excerpt of
		// this same row, still sanitized and still bounded.
		snippet, err := sanitizeSearchSnippet(withWholeFragmentSpan(parsed))
		if err != nil {
			return nil, err
		}
		hits = append(hits, SearchHit{Message: message, Score: score, Snippet: snippet})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return hits, nil
}
