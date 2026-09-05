package docs

import (
	"strings"
	"testing"
)

func TestHistoricalPointerFixtures(t *testing.T) {
	for _, document := range []struct{ path, target string }{
		{"docs/VISION.md", "NORTH_STAR.md"},
		{"docs/architecture/2026-08-18-personal-agent-audit.md", "../NORTH_STAR.md"},
	} {
		baseline := repoFile(t, document.path)
		for _, fixture := range []struct{ name, source, want string }{
			{"baseline", baseline, ""},
			{"empty pointer", "", "historical pointer is empty"},
			{"level-two heading", strings.Replace(baseline, "# ", "## ", 1), "level-1 historical heading"},
			{"regained inventory", baseline + "\n## Current status\n\n| Search | pending |\n", "only paragraphs"},
			{"indented pointer", "    " + strings.ReplaceAll(baseline, "\n", "\n    "), "historical pointer"},
			{"fenced pointer", "```\n" + baseline + "\n```", "historical pointer"},
			{"hidden pointer", "<pre>\n" + baseline + "\n</pre>", "historical pointer"},
			{"not historical", strings.ReplaceAll(baseline, "Historical", "Active"), "historical pointer"},
			{"lost canonical link", strings.ReplaceAll(baseline, document.target, "elsewhere.md"), "historical pointer"},
			{"competing list", baseline + "\n- Search is pending.\n", "historical pointer"},
			{"setext inventory heading", baseline + "\nCurrent status\n---\n", "historical pointer"},
			{"blockquoted inventory", baseline + "\n> - Search is pending.\n", "historical pointer"},
			{"inline-code link", "# Historical pointer\n\n`[Canonical](" + document.target + ")`\n", "historical pointer"},
			{"extra authority link", baseline + "\n[Another roadmap](elsewhere.md)\n", "historical pointer"},
			{"extra authority autolink", baseline + "\n<https://example.invalid/roadmap>\n", "historical pointer"},
			{"oversized pointer", baseline + strings.Repeat("word ", 121), "at most 120 words"},
			{"reworded pointer", baseline + "\nEarlier decisions are retained in Git history.\n", ""},
		} {
			t.Run(document.path+"/"+fixture.name, func(t *testing.T) {
				got := historicalPointerProblem(fixture.source, document.target)
				if (fixture.want == "" && got != "") || !strings.Contains(got, fixture.want) {
					t.Fatalf("got %q, want %q", got, fixture.want)
				}
			})
		}
	}
}

func TestHistoricalNoticeFixtures(t *testing.T) {
	for _, kind := range []string{"plan", "design", "report"} {
		const target = "../../NORTH_STAR.md"
		baseline := "# Old record\n\n**Historical " + kind + ":** Retained record.\nUse the [canonical roadmap](" + target + ").\n\n## Old body\n\nHistorical details remain.\n"
		for _, fixture := range []struct{ name, source, want string }{
			{"baseline", baseline, ""},
			{"paragraph instead of heading", strings.Replace(baseline, "# Old record", "Old record", 1), "top-level heading"},
			{"level-two heading", strings.Replace(baseline, "# Old record", "## Old record", 1), "level-1"},
			{"active notice", strings.Replace(baseline, "Historical "+kind, "Active "+kind, 1), "historical record"},
			{"missing notice", "# Old record\n\n## Old body\n", "historical record"},
			{"missing heading", "**Historical " + kind + ":** [Canonical](" + target + ")", "historical record"},
			{"lost notice link", strings.ReplaceAll(baseline, target, "elsewhere.md"), "notice must link"},
			{"inline-code notice link", "# Old record\n\n**Historical " + kind + ":** `[Canonical](" + target + ")`\n", "historical record"},
		} {
			t.Run(kind+"/"+fixture.name, func(t *testing.T) {
				problem := parseStatusMarkdown(fixture.source).historicalNoticeProblem(target, kind)
				if (fixture.want == "" && problem != "") || !strings.Contains(problem, fixture.want) {
					t.Fatalf("got %q, want %q", problem, fixture.want)
				}
			})
		}
	}
}

func TestCanonicalLinkContexts(t *testing.T) {
	const target = "NORTH_STAR.md"
	const link = "[Canonical](NORTH_STAR.md)"
	for _, fixture := range []struct {
		name, source string
		want         int
	}{
		{"normal link", link, 1},
		{"reference link", "[Canonical][roadmap]\n\n[roadmap]: NORTH_STAR.md", 1},
		{"inline code", "`" + link + "`", 0},
		{"double inline code", "``" + link + "``", 0},
		{"fenced code", "```\n" + link + "\n```", 0},
		{"indented code", "    " + link, 0},
		{"HTML block", "<div>\n" + link + "\n</div>", 0},
		{"processing instruction", "<?test\n" + link + "\n?>", 0},
		{"CDATA", "<![CDATA[\n" + link + "\n]]>", 0},
		{"comment", "<!-- " + link + " -->", 0},
		{"empty label", "[](NORTH_STAR.md)", 0},
		{"invisible label", "[&#x200B;](NORTH_STAR.md)", 0},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			parsed := parseStatusMarkdown(fixture.source)
			matching, _ := parsed.canonicalLinks(parsed.document, target)
			if matching != fixture.want {
				t.Fatalf("visible canonical links = %d, want %d", matching, fixture.want)
			}
		})
	}
}
