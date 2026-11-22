package integrations

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
)

// The cut measures escaped bytes itself rather than marshalling every rune, so
// it must agree with the encoder that writes the rows, byte for byte.
func TestEscapedRuneLengthMatchesTheEncoder(t *testing.T) {
	samples := []string{"\u0080", "\u07ff", "\u0800", "\u2027", "\u2028", "\u2029", "\ufffd", "\U00010000", "\U0010ffff", "\x80", "\xff"}
	for r := rune(0); r < utf8.RuneSelf; r++ {
		samples = append(samples, string(r))
	}
	for _, sample := range samples {
		encoded, err := json.Marshal(sample)
		if err != nil {
			t.Fatal(err)
		}
		r, width := utf8.DecodeRuneInString(sample)
		if got, want := jsonEscapedLen(r, width), len(encoded)-2; got != want {
			t.Errorf("escaped length of %q = %d, the encoder writes %d", sample, got, want)
		}
	}
}

func TestFitEscapedKeepsWhatFitsAndMarksTheCut(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		max   int
		want  string
	}{
		{name: "a value that fits", value: "bug", max: 48, want: "bug"},
		{name: "exactly the bound once escaped", value: strings.Repeat("<", 8), max: 48, want: strings.Repeat("<", 8)},
		{name: "one escaped byte over", value: strings.Repeat("<", 8) + "a", max: 48, want: "<<< [cut: 6 bytes not shown]"},
		{name: "quotes count twice", value: strings.Repeat(`"`, 300), max: 192, want: strings.Repeat(`"`, 82) + " [cut: 218 bytes not shown]"},
		{name: "two-byte characters stay whole", value: strings.Repeat("é", 200), max: 48, want: strings.Repeat("é", 10) + " [cut: 380 bytes not shown]"},
		{name: "four-byte characters stay whole", value: strings.Repeat("😀", 100), max: 48, want: strings.Repeat("😀", 5) + " [cut: 380 bytes not shown]"},
		{name: "a redacted run of newlines", value: strings.Repeat("\n", 100), max: 48, want: strings.Repeat("\n", 10) + " [cut: 90 bytes not shown]"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := fitEscaped(test.value, test.max)
			if got != test.want {
				t.Fatalf("fitEscaped = %q, want %q", got, test.want)
			}
			encoded, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if len(encoded)-2 > test.max {
				t.Fatalf("%q escapes to %d bytes, over %d", got, len(encoded)-2, test.max)
			}
		})
	}
}

// Whatever GitHub puts in an issue, its row stays within 1 KiB: every field is
// bounded on its own, so the bound never depends on what an issue holds.
func TestAnIssueRowIsAtMostOneKiBWhateverTheIssueHolds(t *testing.T) {
	labels := make([]githubLabel, 1000)
	for index := range labels {
		labels[index] = githubLabel{name: strings.Repeat(`"`, 10<<10)}
	}
	issue := githubIssueFields{
		Number:      math.MinInt64,
		Title:       strings.Repeat("<", 300<<10),
		State:       strings.Repeat("\u2028", 100<<10),
		PullRequest: true,
		User:        &githubUser{Login: strings.Repeat("&", 100<<10)},
		Labels:      labels,
		Comments:    math.MinInt64,
		UpdatedAt:   strings.Repeat("\x01", 50<<10),
		HTMLURL:     strings.Repeat(">", 100<<10),
	}
	row, err := issueRowJSON(issue, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(row) > maxIssueRowBytes {
		t.Fatalf("row is %d bytes, over %d", len(row), maxIssueRowBytes)
	}
	var decoded struct {
		Labels     []string `json:"labels"`
		LabelCount int      `json:"label_count"`
	}
	if err := json.Unmarshal(row, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Labels) != 5 || decoded.LabelCount != 1000 {
		t.Fatalf("labels listed = %d of %d, want 5 of 1000", len(decoded.Labels), decoded.LabelCount)
	}
}

func TestRowsPerFrameRowsAtTheMaximumFitOneFrameWithTheLongestContinuation(t *testing.T) {
	row := json.RawMessage(`{"title":"` + strings.Repeat("a", maxIssueRowBytes-12) + `"}`)
	if len(row) != maxIssueRowBytes {
		t.Fatalf("fixture row is %d bytes, want %d", len(row), maxIssueRowBytes)
	}
	rows := make([]json.RawMessage, rowsPerFrame)
	for index := range rows {
		rows[index] = row
	}
	longest := &issueListContinuation{Page: maxListPage, Limit: maxListLimit}
	content, err := issueListContent(rows, longest)
	if err != nil {
		t.Fatal(err)
	}
	framed, err := frameIntegrationResult(content)
	if err != nil {
		t.Fatal(err)
	}
	if len(framed) > backendegress.MaxFramedContentBytes || strings.Contains(framed, "Result truncated") {
		t.Fatalf("%d rows of %d bytes made a %d-byte frame (truncated: %v)", rowsPerFrame, maxIssueRowBytes, len(framed), strings.Contains(framed, "Result truncated"))
	}
	// The frame never cuts a list: content that would not fit is refused.
	if _, err := issueListContent(append(rows, row), longest); err == nil {
		t.Fatalf("%d rows of %d bytes were accepted past the frame's budget", rowsPerFrame+1, maxIssueRowBytes)
	}
}

func TestLinkHeaderNamesANextPageOnlyThroughItsRelation(t *testing.T) {
	for _, test := range []struct {
		name  string
		links []string
		want  bool
	}{
		{name: "GitHub's next and last", links: []string{`<https://api.github.com/repositories/1/issues?page=2>; rel="next", <https://api.github.com/repositories/1/issues?page=5>; rel="last"`}, want: true},
		{name: "only previous and first", links: []string{`<https://api.github.com/x?page=1>; rel="prev", <https://api.github.com/x?page=1>; rel="first"`}},
		{name: "a comma inside a target, an unquoted relation in capitals", links: []string{`<https://x/?a=1,2>; rel="last", <https://x/?page=3>; REL=NEXT`}, want: true},
		{name: "next written inside a target", links: []string{`<https://x/?rel="next">; rel="last"`}},
		{name: "next written inside another parameter", links: []string{`<https://x/>; title="rel=\"next\" here"; rel="prev"`}},
		{name: "one of several relation types", links: []string{`<https://x/>; rel="prev next"`}, want: true},
		{name: "a second header", links: []string{`<https://x/>; rel="prev"`, `<https://x/?page=2>; rel="next"`}, want: true},
		{name: "no target", links: []string{`rel="next"`}},
		{name: "no header"},
	} {
		t.Run(test.name, func(t *testing.T) {
			header := http.Header{}
			for _, link := range test.links {
				header.Add("Link", link)
			}
			if got := linkHasNext(header); got != test.want {
				t.Fatalf("linkHasNext = %v, want %v", got, test.want)
			}
		})
	}
}
