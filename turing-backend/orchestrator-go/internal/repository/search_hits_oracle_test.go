package repository

import (
	"bytes"
	"fmt"
	"unicode"
	"unicode/utf8"
)

// This file holds the reference implementation of snippet marker parsing and
// the adapters that drive the production window from it. None of it is
// production code and none of it is reachable from production: it exists so the
// streaming scanner can be differentially checked against a materializing
// parser that is allowed to allocate, and so tests can inspect a whole parsed
// payload the streaming pipeline deliberately never builds.
//
// Production reads the marked string the driver returned and never copies it.
// Everything here copies freely, which is exactly why it stays on this side of
// the boundary.

// byteSpan is a half-open range over the parsed snippet's raw bytes, recorded
// after marker removal so offsets refer to the payload the caller keeps.
type byteSpan struct {
	start int
	end   int
}

// parsedSearchSnippet is snippet payload with its internal match spans. The
// markers themselves are already gone; the spans are the only surviving record
// of what FTS5 matched.
type parsedSearchSnippet struct {
	text    []byte
	matches []byteSpan
}

// parseMarkedSearchSnippet is the test-only reference parser: the same
// two-state machine scanMarkedSearchSnippet runs, written the obvious
// materializing way.
//
// It copies every non-marker byte into a fresh payload buffer and records each
// enclosed range as a span, which is precisely the allocation production no
// longer performs. Keeping it here gives the differential tests an independent
// oracle whose behavior was pinned before the streaming rewrite, and gives the
// sanitization tests a whole payload to assert on.
func parseMarkedSearchSnippet(raw []byte, start, end string) (parsedSearchSnippet, error) {
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
		return parsedSearchSnippet{}, fmt.Errorf(
			"%w: empty snippet", ErrInvalidSearchSnippetMarkers)
	}
	return parsedSearchSnippet{text: text, matches: matches}, nil
}

// withWholeFragmentSpan gives a marker-free fragment one implicit match span
// covering the whole payload, which is what production expresses by treating a
// pairless fragment's single chunk as a match.
func withWholeFragmentSpan(parsed parsedSearchSnippet) parsedSearchSnippet {
	if len(parsed.matches) > 0 {
		return parsed
	}
	parsed.matches = []byteSpan{{start: 0, end: len(parsed.text)}}
	return parsed
}

// normalizeParsedSnippet is the test-only reference normalizer: the whole
// payload decoded as one buffer, with match spans applied by byte offset.
//
// It is deliberately not a chunked run of the production normalizer. A decoder
// that sees the payload whole is the only oracle that can catch a streaming
// decoder splitting a scalar across a chunk boundary, or attributing one to the
// wrong side of a match. It reuses the production emitting and retention
// primitives, so a disagreement is always about reading, never about windowing.
func normalizeParsedSnippet(n *snippetNormalizer, parsed parsedSearchSnippet) snippetWindow {
	nextMatch := 0
	applySpanBoundaries := func(offset int) {
		for !n.complete {
			if n.openSpan < 0 && nextMatch < len(parsed.matches) &&
				parsed.matches[nextMatch].start <= offset {
				n.openSpan = n.nextRuneIndex()
				continue
			}
			if n.openSpan >= 0 && nextMatch < len(parsed.matches) &&
				parsed.matches[nextMatch].end <= offset {
				if n.window.found {
					n.window.match.end = n.window.totalRunes
					n.complete = true
				}
				n.openSpan = -1
				nextMatch++
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
	applySpanBoundaries(len(parsed.text))

	return n.window
}

// normalizeSnippetWindow is the parsed-payload form of the production window,
// at the production retention width.
func normalizeSnippetWindow(parsed parsedSearchSnippet) snippetWindow {
	return normalizeParsedSnippet(newSnippetNormalizer(searchSnippetWindowRunes), parsed)
}

// sanitizeSearchSnippet is the parsed-payload form of the production
// sanitization result, so the sanitizer's behavior can be exercised against
// payloads that are easier to state as bytes and spans than as marked text.
func sanitizeSearchSnippet(parsed parsedSearchSnippet) (string, error) {
	return boundedSearchSnippetOrError(normalizeSnippetWindow(parsed))
}
