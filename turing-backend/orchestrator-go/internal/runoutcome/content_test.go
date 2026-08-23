package runoutcome

import (
	"strings"
	"testing"
)

// The approved table is explicit rather than delegated to strings.TrimSpace or
// unicode.IsSpace, because the backend and Flutter must agree scalar for scalar
// on whether a stored assistant message is worth showing. A runtime that later
// widens or narrows its own whitespace notion would silently change history.
func TestHasDisplayableContentUsesTheApprovedUnicodeTable(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "empty", content: "", want: false},
		{name: "character_tabulation_U+0009", content: "\u0009", want: false},
		{name: "line_feed_U+000A", content: "\u000A", want: false},
		{name: "line_tabulation_U+000B", content: "\u000B", want: false},
		{name: "form_feed_U+000C", content: "\u000C", want: false},
		{name: "carriage_return_U+000D", content: "\u000D", want: false},
		{name: "space_U+0020", content: "\u0020", want: false},
		{name: "next_line_U+0085", content: "\u0085", want: false},
		{name: "no_break_space_U+00A0", content: "\u00A0", want: false},
		{name: "ogham_space_mark_U+1680", content: "\u1680", want: false},
		{name: "en_quad_U+2000", content: "\u2000", want: false},
		{name: "em_quad_U+2001", content: "\u2001", want: false},
		{name: "en_space_U+2002", content: "\u2002", want: false},
		{name: "em_space_U+2003", content: "\u2003", want: false},
		{name: "three_per_em_space_U+2004", content: "\u2004", want: false},
		{name: "four_per_em_space_U+2005", content: "\u2005", want: false},
		{name: "six_per_em_space_U+2006", content: "\u2006", want: false},
		{name: "figure_space_U+2007", content: "\u2007", want: false},
		{name: "punctuation_space_U+2008", content: "\u2008", want: false},
		{name: "thin_space_U+2009", content: "\u2009", want: false},
		{name: "hair_space_U+200A", content: "\u200A", want: false},
		{name: "line_separator_U+2028", content: "\u2028", want: false},
		{name: "paragraph_separator_U+2029", content: "\u2029", want: false},
		{name: "narrow_no_break_space_U+202F", content: "\u202F", want: false},
		{name: "medium_mathematical_space_U+205F", content: "\u205F", want: false},
		{name: "ideographic_space_U+3000", content: "\u3000", want: false},
		{name: "every_whitespace_scalar_together", content: allApprovedWhitespace(), want: false},
		{name: "ascii_letter", content: "a", want: true},
		{name: "zero_width_space_U+200B", content: "\u200B", want: true},
		{name: "replacement_character_U+FFFD", content: "\uFFFD", want: true},
		{name: "whitespace_surrounding_text", content: "\u0020\u3000\n a \t\u00A0", want: true},
		{name: "whitespace_then_zero_width_space", content: "\u0020\u200B\u0020", want: true},
		// Content is persisted byte for byte, so an invalid sequence still has
		// a scalar outside the table and must not be hidden as "blank".
		{name: "invalid_utf8_byte", content: "\xff", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := HasDisplayableContent(test.content); got != test.want {
				t.Fatalf("HasDisplayableContent(%q) = %t, want %t", test.content, got, test.want)
			}
		})
	}
}

// The digest is an internal identity for the exact persisted bytes: it decides
// whether a duplicate terminal report is the same completion, so it must never
// normalize, trim, or re-encode what it hashes.
func TestContentSHA256IdentifiesExactBytes(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{name: "empty", content: "", want: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{name: "ascii", content: "a", want: "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb"},
		{name: "multibyte_scalars", content: "h\u00e9llo\u00a0\U0001F30D", want: "1d9355824d8f3e586a541fd2249489d69bba7b9e02d910f715beba4fb993b91b"},
		{name: "whitespace_only_still_has_identity", content: "\u00a0", want: "abfbd10daf8965c8860b3582af942d7a7cac972b31d1c50f382b67d9b6c07365"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ContentSHA256(test.content)
			if got != test.want {
				t.Fatalf("ContentSHA256(%q) = %q, want %q", test.content, got, test.want)
			}
			if got != strings.ToLower(got) {
				t.Fatalf("digest %q is not lowercase", got)
			}
		})
	}

	if ContentSHA256("a") == ContentSHA256("a ") {
		t.Fatal("trailing whitespace changed the bytes but not the digest")
	}
}

func allApprovedWhitespace() string {
	var builder strings.Builder
	for _, scalar := range []rune{
		'\u0009', '\u000A', '\u000B', '\u000C', '\u000D', '\u0020', '\u0085', '\u00A0',
		'\u1680', '\u2000', '\u2001', '\u2002', '\u2003', '\u2004', '\u2005', '\u2006',
		'\u2007', '\u2008', '\u2009', '\u200A', '\u2028', '\u2029', '\u202F', '\u205F',
		'\u3000',
	} {
		builder.WriteRune(scalar)
	}
	return builder.String()
}
