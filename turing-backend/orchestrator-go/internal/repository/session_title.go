package repository

import (
	"strings"
)

// maxTitleRunes bounds a derived title to about one line of the client's
// conversation list. Titles are counted in runes rather than bytes, so a
// message in a non-Latin script is not cut to a third of the length of an
// English one. Runes are not grapheme clusters and CJK is double-width, so
// this is a bound on code points, not on rendered width: a combining accent
// or an emoji ZWJ sequence sitting exactly on the boundary can still be
// split. Fixing that properly needs a grapheme library the repo does not
// carry, and the failure mode is a slightly odd final character in a title.
const maxTitleRunes = 60

// minWordBoundaryRunes is the shortest title we will accept from a word-boundary
// cut. Without it, a first "word" longer than the budget (a URL, a pasted
// stack frame, a base64 blob) would collapse the title to whatever tiny
// fragment preceded it, or to nothing at all.
const minWordBoundaryRunes = maxTitleRunes / 2

// DeriveSessionTitle turns the first thing a user said into a conversation
// title. It returns "" when the message carries no usable text, which callers
// treat as "leave this session untitled" rather than as a title of "".
//
// The result is deliberately a truncation of the user's own words rather than
// a model-generated summary: it is instant, deterministic, costs no tokens,
// and cannot hallucinate. A summarising pass could be layered on later without
// changing the storage contract.
func DeriveSessionTitle(content string) string {
	// strings.Fields splits on every unicode space, so this collapses newlines,
	// tabs and runs of spaces in one step. A pasted multi-line message becomes
	// a single line instead of a title with a line break buried in it.
	collapsed := strings.Join(strings.Fields(content), " ")
	if collapsed == "" {
		return ""
	}

	runes := []rune(collapsed)
	if len(runes) <= maxTitleRunes {
		return collapsed
	}

	truncated := string(runes[:maxTitleRunes])
	// Cut back to the last space so the title ends on a whole word — but only
	// when doing so leaves something worth reading.
	if idx := strings.LastIndex(truncated, " "); idx > 0 {
		if candidate := truncated[:idx]; len([]rune(candidate)) >= minWordBoundaryRunes {
			truncated = candidate
		}
	}
	return strings.TrimRight(truncated, " ") + "…"
}
