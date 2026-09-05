package docs

import (
	"regexp"
	"strings"
	"testing"

	mdast "github.com/yuin/goldmark/ast"
)

func TestVerificationInstructionsPrepareNestedModuleCaches(t *testing.T) {
	for _, path := range []string{
		"README.md", "docs/architecture/tech-stack.md",
		"docs/superpowers/integration-checklist.md", "CLAUDE.md",
		".claude/skills/verify/SKILL.md",
	} {
		t.Run(path, func(t *testing.T) {
			document := parseStatusMarkdown(repoFile(t, path))
			found := false
			for node := document.document.FirstChild(); node != nil; node = node.NextSibling() {
				block, ok := node.(*mdast.FencedCodeBlock)
				if !ok {
					continue
				}
				body := string(block.Lines().Value(document.source))
				rootTest := strings.Index(body, "go test -tags sqlite_fts5")
				if rootTest < 0 || !strings.Contains(body, "./...") {
					continue
				}
				found = true
				for _, module := range []string{"mcp-files", "mcp-system"} {
					pattern := regexp.MustCompile(`(?m)^\(\s*cd\s+turing-backend/` + module + `\s*&&\s*go\s+mod\s+download\s*\)`)
					match := pattern.FindStringIndex(body)
					if match == nil || match[0] > rootTest {
						t.Errorf("%s: prepare %s dependencies before the root tests, whose docs guard resolves all module graphs offline", path, module)
					}
				}
			}
			if !found {
				t.Fatal("expected a documented root verification command block")
			}
		})
	}
}
