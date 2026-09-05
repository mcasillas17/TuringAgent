package docs

import (
	"bytes"
	"html"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	mdast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	tableast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

// Markdown context is parsed, not inferred from angle brackets or backtick
// patterns. The parser is used here by the documentation tests.
type statusMarkdown struct {
	source   []byte
	document mdast.Node
}

func parseStatusMarkdown(source string) statusMarkdown {
	data := []byte(source)
	parser := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()
	return statusMarkdown{source: data, document: parser.Parse(text.NewReader(data))}
}

func (m statusMarkdown) guardedTableSource() (string, *tableast.Table, string) {
	start, end := -1, -1
	tables, otherBlocks := 0, 0
	var table *tableast.Table
	for node := m.document.FirstChild(); node != nil; node = node.NextSibling() {
		if block, ok := node.(*mdast.HTMLBlock); ok {
			var value strings.Builder
			value.Write(block.Lines().Value(m.source))
			if block.HasClosure() {
				value.Write(block.ClosureLine.Value(m.source))
			}
			switch strings.TrimSpace(value.String()) {
			case statusBegin:
				if start >= 0 || end >= 0 {
					return "", nil, "malformed status-guard delimiters; require one ordered pair"
				}
				if block.HasClosure() {
					start = block.ClosureLine.Stop
				} else if block.Lines().Len() > 0 {
					start = block.Lines().At(block.Lines().Len() - 1).Stop
				}
				continue
			case statusEnd:
				if start < 0 || end >= 0 {
					return "", nil, "malformed status-guard delimiters; require one ordered pair"
				}
				if block.Lines().Len() > 0 {
					end = block.Lines().At(0).Start
				} else if block.HasClosure() {
					end = block.ClosureLine.Start
				}
				continue
			}
		}
		if start >= 0 && end < 0 {
			if current, ok := node.(*tableast.Table); ok {
				tables++
				table = current
			} else {
				otherBlocks++
			}
		}
	}
	if start < 0 || end < start || end > len(m.source) {
		return "", nil, "malformed status-guard delimiters; require top-level comments outside fenced/indented code and raw HTML blocks"
	}
	if tables != 1 || otherBlocks != 0 {
		table = nil
	}
	return string(m.source[start:end]), table, ""
}

func (m statusMarkdown) emptyScopeRows(table *tableast.Table) map[int]bool {
	if table == nil || table.FirstChild() == nil {
		return nil
	}
	empty := make(map[int]bool)
	ordinal := 3 // Header and separator occupy the first two source rows.
	for row := table.FirstChild().NextSibling(); row != nil; row = row.NextSibling() {
		scope := row.FirstChild()
		for column := 0; column < 2 && scope != nil; column++ {
			scope = scope.NextSibling()
		}
		empty[ordinal] = scope == nil || !scopeHasReadableText(m.plainText(scope))
		ordinal++
	}
	return empty
}

func scopeHasReadableText(value string) bool {
	for _, r := range value {
		if unicode.In(r, unicode.L, unicode.N, unicode.P, unicode.S) &&
			!unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r) {
			return true
		}
	}
	return false
}

func (m statusMarkdown) topLevelHeadings() string {
	var headings strings.Builder
	for node := m.document.FirstChild(); node != nil; node = node.NextSibling() {
		if _, ok := node.(*mdast.Heading); !ok || node.Lines().Len() == 0 {
			continue
		}
		start := node.Lines().At(0).Start
		start = bytes.LastIndexByte(m.source[:start], '\n') + 1
		stop := node.Lines().At(node.Lines().Len() - 1).Stop
		headings.Write(m.source[start:stop])
		headings.WriteByte('\n')
	}
	return headings.String()
}

func (m statusMarkdown) canonicalLinks(node mdast.Node, target string) (matching, total int) {
	_ = mdast.Walk(node, func(node mdast.Node, entering bool) (mdast.WalkStatus, error) {
		if !entering {
			return mdast.WalkContinue, nil
		}
		switch link := node.(type) {
		case *mdast.Link:
			total++
			destination := string(link.Destination)
			if scopeHasReadableText(m.plainText(link)) && (destination == target || strings.HasPrefix(destination, target+"#")) {
				matching++
			}
		case *mdast.AutoLink:
			total++
			destination := string(link.URL(m.source))
			if scopeHasReadableText(string(link.Label(m.source))) && (destination == target || strings.HasPrefix(destination, target+"#")) {
				matching++
			}
		}
		return mdast.WalkContinue, nil
	})
	return matching, total
}

func (m statusMarkdown) plainText(node mdast.Node) string {
	var output strings.Builder
	_ = mdast.Walk(node, func(node mdast.Node, entering bool) (mdast.WalkStatus, error) {
		if !entering {
			return mdast.WalkContinue, nil
		}
		switch node := node.(type) {
		case *mdast.Text:
			value := string(node.Value(m.source))
			if node.Parent() == nil || node.Parent().Kind() != mdast.KindCodeSpan {
				value = html.UnescapeString(value)
			}
			output.WriteString(value)
			if node.SoftLineBreak() || node.HardLineBreak() {
				output.WriteByte(' ')
			}
		case *mdast.String:
			output.Write(node.Value)
		case *mdast.AutoLink:
			output.Write(node.Label(m.source))
		}
		return mdast.WalkContinue, nil
	})
	return output.String()
}

func (m statusMarkdown) historicalPointerProblem(target string) string {
	first := m.document.FirstChild()
	if first == nil {
		return "historical pointer is empty"
	}
	heading, ok := first.(*mdast.Heading)
	if !ok || heading.Level != 1 || !strings.Contains(strings.ToLower(m.plainText(heading)), "historical") {
		return "historical pointer must start with one visible level-1 historical heading"
	}
	for node := first.NextSibling(); node != nil; node = node.NextSibling() {
		if _, ok := node.(*mdast.Paragraph); !ok {
			return "historical pointer must contain only paragraphs after its heading, not competing inventories or code"
		}
	}
	rawHTML := false
	_ = mdast.Walk(m.document, func(node mdast.Node, entering bool) (mdast.WalkStatus, error) {
		if _, ok := node.(*mdast.RawHTML); ok && entering {
			rawHTML = true
		}
		return mdast.WalkContinue, nil
	})
	if rawHTML {
		return "historical pointer must not contain raw HTML"
	}
	if len(strings.Fields(m.plainText(m.document))) > 120 {
		return "historical pointer must remain at most 120 words"
	}
	matching, total := m.canonicalLinks(m.document, target)
	if matching != 1 || total != 1 {
		return "historical pointer must link only to the canonical roadmap, not a code example"
	}
	return ""
}

func (m statusMarkdown) historicalNoticeProblem(target, kind string) string {
	first := m.document.FirstChild()
	if first == nil {
		return "historical record needs a heading and notice"
	}
	if heading, ok := first.(*mdast.Heading); !ok || heading.Level != 1 {
		return "historical record needs a top-level heading at level-1"
	}
	notice, ok := first.NextSibling().(*mdast.Paragraph)
	if !ok || !strings.HasPrefix(m.plainText(notice), "Historical "+kind+":") {
		return "historical record needs its top-level historical " + kind + " notice"
	}
	matching, _ := m.canonicalLinks(notice, target)
	if matching != 1 {
		return "historical record notice must link to the canonical roadmap"
	}
	return ""
}

func historicalPointerProblem(source, target string) string {
	return parseStatusMarkdown(source).historicalPointerProblem(target)
}
