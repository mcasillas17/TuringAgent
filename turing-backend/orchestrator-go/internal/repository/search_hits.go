package repository

import (
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

// markedSearchSnippetShape is everything one validation pass learns about a
// marked snippet without keeping any of it: how many complete marker pairs it
// carries, and how many payload bytes survive marker removal. Both are
// counters, so learning it costs nothing that grows with the fragment.
type markedSearchSnippetShape struct {
	pairs        int
	payloadBytes int
}

// scanMarkedSearchSnippet validates the marker pair FTS5 wrapped around each
// matched phrase and hands every stretch of payload between them to emit,
// tagged with whether it sat inside a match.
//
// It is a two-state machine over the raw string — text and match — and only the
// exact start marker may open a match, only the exact end marker may close one.
// An end marker in text, a start marker inside a match, a missing partner, or a
// snippet still in match state at the end all fail: FTS5 emits balanced
// markers, so a structurally broken one means the value did not come from the
// projection we asked for, and guessing which bytes were inserted is exactly
// the guess this design refuses to make.
//
// A snippet carrying no complete marker at all is a different case, and it is
// valid. `snippet()` can only mark a phrase that fits inside its token window,
// so an exact phrase longer than that window legitimately yields an unmarked
// fragment of the same matched row, emitted as one text chunk with no pairs.
// Only a snippet with no payload and no pair still fails, because FTS5 returns
// some text for every row it matches.
//
// Every emitted chunk is a subslice of raw, never a copy, so scanning a
// multi-megabyte fragment allocates nothing that grows with it. Callers that
// must not act on a structurally broken snippet pass a nil emit to validate
// first: emission is streamed, so a chunk before the failure has already been
// handed over by the time the error is returned.
//
// Running before rune decoding is deliberate. Markers are recognized while the
// input is still bytes, so invalid UTF-8 next to a marker cannot absorb marker
// bytes, turn a start into an end, or leave marker fragments in public output.
func scanMarkedSearchSnippet(
	raw, start, end string,
	emit func(chunk string, inMatch bool),
) (markedSearchSnippetShape, error) {
	// The state machine only terminates because every iteration consumes a
	// marker, and it can only distinguish the two states because the markers
	// differ. An empty marker matches at every offset and consumes nothing, so
	// the loop would spin forever; identical markers would let the scanner
	// invent match boundaries and report success. newSearchSnippetMarkers can
	// never produce either, so this rejects a caller mistake at the door rather
	// than letting it become a hang or a silently wrong snippet.
	if start == "" || end == "" || start == end {
		return markedSearchSnippetShape{}, fmt.Errorf(
			"%w: empty or identical markers", ErrInvalidSearchSnippetMarkers)
	}

	var shape markedSearchSnippetShape
	rest := raw
	inMatch := false

	for len(rest) > 0 {
		nextStart := strings.Index(rest, start)
		nextEnd := strings.Index(rest, end)

		if !inMatch {
			if nextStart < 0 {
				if nextEnd >= 0 {
					return markedSearchSnippetShape{}, fmt.Errorf(
						"%w: end marker outside a match", ErrInvalidSearchSnippetMarkers)
				}
				shape.payloadBytes += len(rest)
				if emit != nil {
					emit(rest, false)
				}
				break
			}
			if nextEnd >= 0 && nextEnd < nextStart {
				return markedSearchSnippetShape{}, fmt.Errorf(
					"%w: end marker before start marker", ErrInvalidSearchSnippetMarkers)
			}
			// An empty stretch of text is not a boundary anything downstream
			// can observe, so only a real one is emitted.
			if nextStart > 0 {
				shape.payloadBytes += nextStart
				if emit != nil {
					emit(rest[:nextStart], false)
				}
			}
			rest = rest[nextStart+len(start):]
			inMatch = true
			continue
		}

		if nextEnd < 0 {
			return markedSearchSnippetShape{}, fmt.Errorf(
				"%w: start marker without end marker", ErrInvalidSearchSnippetMarkers)
		}
		if nextStart >= 0 && nextStart < nextEnd {
			return markedSearchSnippetShape{}, fmt.Errorf(
				"%w: nested start marker", ErrInvalidSearchSnippetMarkers)
		}
		shape.payloadBytes += nextEnd
		shape.pairs++
		// An empty match is emitted even though it carries no payload: it is
		// still a span the consumer has to open and close on.
		if emit != nil {
			emit(rest[:nextEnd], true)
		}
		rest = rest[nextEnd+len(end):]
		inMatch = false
	}

	if inMatch {
		return markedSearchSnippetShape{}, fmt.Errorf(
			"%w: snippet ended inside a match", ErrInvalidSearchSnippetMarkers)
	}
	if shape.payloadBytes == 0 && shape.pairs == 0 {
		// FTS5 returns some text for every row it matches, so nothing at all is
		// not a windowing outcome — it is a projection that did not come from
		// the query we asked for.
		return markedSearchSnippetShape{}, fmt.Errorf(
			"%w: empty snippet", ErrInvalidSearchSnippetMarkers)
	}
	return shape, nil
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

// sanitizeMarkedSearchSnippet turns the marked string SQLite returned into the
// public snippet: valid UTF-8, single-line plain text, bounded, and still
// showing what matched.
//
// The fragment is read twice and copied never. The first pass validates the
// marker state machine and counts pairs, so a structurally broken snippet fails
// before anything is fed forward and a marker-free fragment is known to be one
// before normalization starts. The second pass streams the same subslices into
// the rolling window, which retains a fixed searchSnippetWindowRunes scalars
// whatever the fragment weighs.
//
// Empty output, or output that no longer contains any matched text, is a
// failure rather than a hit with invented or match-less text.
func sanitizeMarkedSearchSnippet(raw, start, end string) (string, error) {
	window, err := normalizeMarkedSnippetWindow(raw, start, end)
	if err != nil {
		return "", err
	}
	return boundedSearchSnippetOrError(window)
}

// boundedSearchSnippetOrError bounds a sanitized window and re-checks the
// result rather than trusting how it was built: this is the value that leaves
// the repository.
func boundedSearchSnippetOrError(window snippetWindow) (string, error) {
	snippet := boundSearchSnippet(window)
	if snippet == "" {
		return "", ErrInvalidSearchSnippet
	}
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
	// -1 when no match is open. complete records that the first retained match
	// has already closed; later matches are irrelevant, because the window
	// opens on the first one.
	openSpan int
	complete bool
	// invalidRun records that the previous byte was part of a run of invalid
	// UTF-8, so the whole run collapses into a single U+FFFD across chunk
	// boundaries exactly as it would inside one.
	invalidRun bool
	// carry holds the leading bytes of a scalar whose encoding continues into
	// the next chunk, so chunk splits decode identically to the concatenated
	// payload. A scalar is at most utf8.UTFMax bytes, so this never grows.
	carry    [utf8.UTFMax]byte
	carryLen int
	// deferredBoundaries counts span boundaries the decoder has not reached
	// yet, indexed by how many bytes ahead of it they sit. A boundary can only
	// be recorded while a scalar is carried, and a carried scalar is at most
	// utf8.UTFMax bytes, so the whole queue fits in this fixed array; index 0
	// is always empty because a boundary the decoder has reached is applied at
	// once.
	deferredBoundaries [utf8.UTFMax]int
}

// normalizeMarkedSnippetWindow validates a marked snippet and sanitizes its
// payload into a window bounded by searchSnippetWindowRunes, without ever
// holding a second copy of the fragment.
//
// The first pass proves the marker structure and reports whether the fragment
// carried any pair at all; a marker-free fragment is then normalized as one
// implicit whole-fragment match, so the bounding pass has a window to open
// around. That has to be known before normalization starts, which is why the
// scan runs twice rather than buffering what the first pass saw.
//
// The second pass reads the same string end to end, which is unavoidable:
// trailing whitespace collapses away, so whether any text survives after the
// window can only be known by reading to the end. Reading a string the driver
// already returned is not the same as allocating a second one beside it.
func normalizeMarkedSnippetWindow(raw, start, end string) (snippetWindow, error) {
	shape, err := scanMarkedSearchSnippet(raw, start, end, nil)
	if err != nil {
		return snippetWindow{}, err
	}
	// A fragment FTS5 could not mark is windowed as a whole rather than around
	// a match it never reported. The span is purely a windowing hint: it is
	// never published, adds no emphasis, and covers only this same row's text.
	// It is applied only when there is no pair at all, so it can never widen or
	// merge a match FTS5 did report.
	wholeFragment := shape.pairs == 0

	normalizer := newSnippetNormalizer(searchSnippetWindowRunes)
	if _, err := scanMarkedSearchSnippet(raw, start, end,
		func(chunk string, inMatch bool) {
			normalizer.writeChunk(chunk, inMatch || wholeFragment)
		}); err != nil {
		// Unreachable: the same string was just validated, and the scan is a
		// pure function of it. Failing closed keeps that an assumption this
		// code states rather than one it relies on silently.
		return snippetWindow{}, err
	}
	return normalizer.finish(), nil
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

// writeChunk feeds one stretch of payload into the window, opening a match span
// before it and closing that span after it when the chunk sat inside a marker
// pair. Chunks are subslices of the caller's string, so nothing is copied.
func (n *snippetNormalizer) writeChunk(chunk string, inMatch bool) {
	if inMatch {
		n.boundary()
	}
	n.write(chunk)
	if inMatch {
		n.boundary()
	}
}

// finish drains a scalar left straddling the end of the payload and applies any
// boundary the decoder never reached, then reports the window.
//
// A trailing incomplete encoding is charged one invalid byte at a time, which
// the invalid-run rule collapses into a single U+FFFD — exactly what decoding
// the whole payload as one buffer produces.
func (n *snippetNormalizer) finish() snippetWindow {
	for n.carryLen > 0 {
		decoded, width := utf8.DecodeRune(n.carry[:n.carryLen])
		n.consume(decoded, width)
		n.carryLen = copy(n.carry[:], n.carry[width:n.carryLen])
	}
	// Nothing is left pending once the carry drains, because a boundary is
	// never recorded further ahead than the carry is long. Applying the
	// remainder keeps that a property this code enforces rather than assumes.
	n.applyDeferredBoundaries(len(n.deferredBoundaries))
	return n.window
}

// write decodes one chunk, joining a scalar that began in an earlier chunk to
// the bytes that complete it so that the split is invisible to sanitization.
func (n *snippetNormalizer) write(chunk string) {
	for len(chunk) > 0 {
		if n.carryLen == 0 {
			if !utf8.FullRuneInString(chunk) {
				// The rest of this chunk is the opening bytes of a scalar the
				// next chunk finishes. It is at most utf8.UTFMax-1 bytes,
				// because a longer prefix would already be a full encoding.
				n.carryLen = copy(n.carry[:], chunk)
				return
			}
			decoded, width := utf8.DecodeRuneInString(chunk)
			n.consume(decoded, width)
			chunk = chunk[width:]
			continue
		}

		// Take only the bytes the carried scalar still needs, so bytes past it
		// stay on the chunk and cannot jump ahead of a boundary between them.
		for n.carryLen < len(n.carry) && len(chunk) > 0 &&
			!utf8.FullRune(n.carry[:n.carryLen]) {
			n.carry[n.carryLen] = chunk[0]
			n.carryLen++
			chunk = chunk[1:]
		}
		if !utf8.FullRune(n.carry[:n.carryLen]) {
			// Still short, and this chunk is spent.
			return
		}
		decoded, width := utf8.DecodeRune(n.carry[:n.carryLen])
		n.consume(decoded, width)
		n.carryLen = copy(n.carry[:], n.carry[width:n.carryLen])
	}
}

// consume applies the sanitization rules to one decoded scalar and advances the
// decoder past the bytes it occupied.
func (n *snippetNormalizer) consume(decoded rune, width int) {
	if decoded == utf8.RuneError && width <= 1 {
		// One maximal run of invalid bytes becomes a single U+FFFD, which is
		// what strings.ToValidUTF8 does; the decoder still advances one byte at
		// a time so a span boundary inside the run is not skipped.
		if !n.invalidRun {
			n.emit(utf8.RuneError)
			n.invalidRun = true
		}
		n.applyDeferredBoundaries(width)
		return
	}
	n.invalidRun = false

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
	n.applyDeferredBoundaries(width)
}

// boundary records one span boundary at the decoder's current position.
//
// While a scalar is carried the decoder still sits before it, so the boundary
// belongs after that scalar and is queued at its distance ahead instead of
// being applied now. That is what keeps a scalar straddling a marker attributed
// to the side it started on, as it is when the payload is decoded as one
// buffer.
func (n *snippetNormalizer) boundary() {
	if n.carryLen == 0 || n.carryLen >= len(n.deferredBoundaries) {
		// A full carry always decodes, so the second case is unreachable;
		// applying now keeps this total rather than indexing out of range.
		n.applyBoundary()
		return
	}
	n.deferredBoundaries[n.carryLen]++
}

// applyDeferredBoundaries applies every queued boundary sitting fewer than
// distance bytes ahead of the decoder and shifts the rest down by that much.
// Queued boundaries are recorded in arrival order, which is also ascending
// position order, so applying them as one run preserves both.
func (n *snippetNormalizer) applyDeferredBoundaries(distance int) {
	var shifted [utf8.UTFMax]int
	due := 0
	for ahead := 1; ahead < len(n.deferredBoundaries); ahead++ {
		count := n.deferredBoundaries[ahead]
		if count == 0 {
			continue
		}
		if ahead <= distance {
			due += count
			continue
		}
		shifted[ahead-distance] = count
	}
	n.deferredBoundaries = shifted
	for applied := 0; applied < due; applied++ {
		n.applyBoundary()
	}
}

// applyBoundary opens or closes a match span. Boundaries strictly alternate —
// every match chunk contributes exactly one open followed by one close — so the
// open span itself says which of the two this is, with no queue of kinds to
// keep alongside the queue of positions.
func (n *snippetNormalizer) applyBoundary() {
	if n.complete {
		// The first retained match has closed; nothing after it can change the
		// window, so later spans are not tracked at all.
		return
	}
	if n.openSpan < 0 {
		n.openSpan = n.nextRuneIndex()
		return
	}
	if n.window.found {
		n.window.match.end = n.window.totalRunes
		n.complete = true
	}
	n.openSpan = -1
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
		// The marked snippet is used as the string the driver returned: no
		// second copy of the fragment is made, and no representation
		// proportional to it is built. A marker-free fragment is windowed as a
		// whole rather than around a match FTS5 never reported, which yields an
		// unhighlighted excerpt of this same row, still sanitized and bounded.
		snippet, err := sanitizeMarkedSearchSnippet(markedSnippet, start, end)
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
