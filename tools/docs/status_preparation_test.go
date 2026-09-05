package docs

import (
	"regexp"
	"strings"
	"testing"

	mdast "github.com/yuin/goldmark/ast"
)

func verificationPreparationProblems(source string) []string {
	document := parseStatusMarkdown(source)
	var problems []string
	found := false
	_ = mdast.Walk(document.document, func(node mdast.Node, entering bool) (mdast.WalkStatus, error) {
		block, ok := node.(*mdast.FencedCodeBlock)
		if !entering || !ok {
			return mdast.WalkContinue, nil
		}
		body := string(block.Lines().Value(document.source))
		rootTest := directRootTestOffset(body)
		if rootTest < 0 {
			return mdast.WalkContinue, nil
		}
		found = true
		for _, module := range []string{"mcp-files", "mcp-system"} {
			pattern := regexp.MustCompile(`(?m)^\s*\(\s*cd\s+turing-backend/` + module + `\s*&&\s*go\s+mod\s+download\s*\)`)
			match := pattern.FindStringIndex(body)
			if match == nil || match[0] > rootTest {
				problems = append(problems, "prepare "+module+" dependencies before the offline root tests")
			}
		}
		return mdast.WalkContinue, nil
	})
	if !found {
		problems = append(problems, "expected a documented root verification command block")
	}
	return problems
}

func directRootTestOffset(body string) int {
	offset := 0
	for _, line := range strings.SplitAfter(body, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "go" && fields[1] == "test" {
			root, tags := false, false
			for i, field := range fields {
				root = root || field == "./..."
				tags = tags || (field == "-tags" && i+1 < len(fields) && fields[i+1] == "sqlite_fts5")
			}
			if root && tags {
				return offset
			}
		}
		offset += len(line)
	}
	return -1
}

func TestVerificationInstructionsPrepareNestedModuleCaches(t *testing.T) {
	for _, path := range []string{
		"README.md", "docs/architecture/tech-stack.md",
		"docs/superpowers/integration-checklist.md", "CLAUDE.md",
		".claude/skills/verify/SKILL.md",
	} {
		t.Run(path, func(t *testing.T) {
			for _, problem := range verificationPreparationProblems(repoFile(t, path)) {
				t.Errorf("%s: %s", path, problem)
			}
		})
	}
}

func TestPreparationIgnoresNonCommands(t *testing.T) {
	for _, body := range []string{
		"# go test -tags sqlite_fts5 ./...\n",
		"echo go test -tags sqlite_fts5 ./...\n",
		"printf 'go test -tags sqlite_fts5 ./...'\n",
		"go test -tags sqlite_fts5 ./tools/docs\n",
	} {
		if offset := directRootTestOffset(body); offset != -1 {
			t.Fatalf("non-root command detected at %d", offset)
		}
	}
}

func TestVerificationPreparationContexts(t *testing.T) {
	commands := "(cd turing-backend/mcp-files && go mod download)\n(cd turing-backend/mcp-system && go mod download)\ngo test -tags sqlite_fts5 ./... -count=1\n"
	for _, fixture := range []struct{ name, prefix string }{
		{"top level", ""}, {"blockquote", "> "}, {"list", "  "},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			wrap := func(commands string) string {
				block := "```bash\n" + commands + "```\n"
				block = fixture.prefix + strings.ReplaceAll(block, "\n", "\n"+fixture.prefix)
				if fixture.name == "list" {
					block = "- Checks:\n\n" + block
				}

				return block
			}
			if problems := verificationPreparationProblems(wrap(commands)); len(problems) != 0 {
				t.Fatalf("prepared block rejected: %v", problems)
			}
			missing := wrap("go test -tags sqlite_fts5 ./... -count=1\n")
			if problems := strings.Join(verificationPreparationProblems(missing), "\n"); !strings.Contains(problems, "prepare mcp-files") || !strings.Contains(problems, "prepare mcp-system") {
				t.Fatalf("nested missing preparation passed: %s", problems)
			}
		})
	}
}
