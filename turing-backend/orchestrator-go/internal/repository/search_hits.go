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
	text, matches := normalizeSnippetRunes(parsed)
	snippet := boundSearchSnippet(text, matches)
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

// normalizeSnippetRunes repairs and flattens snippet payload, translating the
// byte match spans into rune spans over the result.
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
func normalizeSnippetRunes(parsed parsedSearchSnippet) ([]rune, []runeSpan) {
	text := make([]rune, 0, len(parsed.text))
	spans := make([]runeSpan, 0, len(parsed.matches))

	pendingSpace := false
	openSpan := -1
	next := 0

	emit := func(r rune) {
		if pendingSpace {
			text = append(text, ' ')
			pendingSpace = false
		}
		text = append(text, r)
	}
	// nextRuneIndex is where the next emitted rune will land, including a space
	// that is still pending. Using it as a match start keeps a span over the
	// match's own runes instead of the collapsed space in front of it.
	nextRuneIndex := func() int {
		if pendingSpace {
			return len(text) + 1
		}
		return len(text)
	}
	// applySpanBoundaries closes and opens every match span that lands on this
	// byte offset, so spans that touch after marker removal stay separate.
	applySpanBoundaries := func(offset int) {
		for {
			if openSpan < 0 && next < len(parsed.matches) && parsed.matches[next].start <= offset {
				openSpan = nextRuneIndex()
				continue
			}
			if openSpan >= 0 && next < len(parsed.matches) && parsed.matches[next].end <= offset {
				start := openSpan
				if start > len(text) {
					// The whole match normalized away, so record an empty span
					// rather than an inverted one.
					start = len(text)
				}
				spans = append(spans, runeSpan{start: start, end: len(text)})
				openSpan = -1
				next++
				continue
			}
			return
		}
	}

	invalidRun := false
	for offset := 0; offset < len(parsed.text); {
		applySpanBoundaries(offset)

		decoded, width := utf8.DecodeRune(parsed.text[offset:])
		if decoded == utf8.RuneError && width <= 1 {
			// One maximal run of invalid bytes becomes a single U+FFFD, which
			// is what strings.ToValidUTF8 does; offsets still advance one byte
			// at a time so a span boundary inside the run is not skipped.
			if !invalidRun {
				emit(utf8.RuneError)
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
			if len(text) > 0 {
				pendingSpace = true
			}
		case isSearchSnippetControl(decoded), isSearchSnippetBidiControl(decoded):
			emit(utf8.RuneError)
		default:
			emit(decoded)
		}
		offset += width
	}
	applySpanBoundaries(len(parsed.text))

	return text, spans
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

// boundSearchSnippet cuts sanitized text down to a window that respects both
// caps while keeping matched text visible. It returns "" when no window can
// satisfy that, which the caller turns into ErrInvalidSearchSnippet.
//
// The window starts at the first surviving match and grows outward, so a
// complete match is preserved whenever it fits. When one matched token is
// larger than the caps on its own the bounds win and the largest prefix that
// fits stays visible, because an unbounded snippet is the thing this function
// exists to prevent. Both caps are checked with the ellipsis already paid for,
// and every cut lands on a rune boundary.
func boundSearchSnippet(text []rune, matches []runeSpan) string {
	if len(text) == 0 {
		return ""
	}
	match, found := firstRetainedSearchMatch(matches)
	if !found {
		return ""
	}

	// Prefix byte offsets keep every window measurement O(1); string(text[a:b])
	// per candidate window would be quadratic on a long oversized token.
	offsets := make([]int, len(text)+1)
	for i, r := range text {
		width := utf8.RuneLen(r)
		if width < 0 {
			// A rune Go cannot encode is written as U+FFFD by string(), so
			// charge it the same width.
			width = utf8.RuneLen(utf8.RuneError)
		}
		offsets[i+1] = offsets[i] + width
	}
	fits := func(low, high int) bool {
		runes := high - low
		size := offsets[high] - offsets[low]
		if low > 0 {
			runes += 1
			size += searchSnippetEllipsisBytes
		}
		if high < len(text) {
			runes += 1
			size += searchSnippetEllipsisBytes
		}
		return runes <= searchSnippetMaxRunes && size <= searchSnippetMaxBytes
	}

	if fits(0, len(text)) {
		return string(text)
	}

	low, high := match.start, match.end
	truncatedMatch := false
	for high > low && !fits(low, high) {
		high--
		truncatedMatch = true
	}
	if high <= low {
		return ""
	}
	if !truncatedMatch {
		// Grow context evenly so a match keeps its surroundings on both sides
		// when there is room for them.
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
	}

	// Trim space that would otherwise sit against an inserted ellipsis. This
	// only shrinks the window, so both caps still hold.
	if low > 0 {
		for low < high && text[low] == ' ' {
			low++
		}
	}
	if high < len(text) {
		for high > low && text[high-1] == ' ' {
			high--
		}
	}
	if high <= low || max(low, match.start) >= min(high, match.end) {
		return ""
	}

	var bounded strings.Builder
	if low > 0 {
		bounded.WriteRune(searchSnippetEllipsis)
	}
	bounded.WriteString(string(text[low:high]))
	if high < len(text) {
		bounded.WriteRune(searchSnippetEllipsis)
	}
	return bounded.String()
}

// firstRetainedSearchMatch returns the first match span that still covers real
// text. A span can be emptied by sanitization — a match made only of control
// characters, for example — and an empty span is not evidence of a match.
func firstRetainedSearchMatch(matches []runeSpan) (runeSpan, bool) {
	for _, match := range matches {
		if match.end > match.start {
			return match, true
		}
	}
	return runeSpan{}, false
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
