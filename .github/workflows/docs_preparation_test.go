package workflows_test

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func documentationPreparationProblem(data []byte) string {
	var workflow struct {
		Jobs map[string]struct {
			If       yaml.Node `yaml:"if"`
			Continue yaml.Node `yaml:"continue-on-error"`
			Steps    []struct {
				Uses     string    `yaml:"uses"`
				Run      string    `yaml:"run"`
				If       yaml.Node `yaml:"if"`
				Continue yaml.Node `yaml:"continue-on-error"`
				With     struct {
					Cache      yaml.Node `yaml:"cache"`
					CachePaths string    `yaml:"cache-dependency-path"`
				} `yaml:"with"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		return "malformed workflow"
	}
	job, exists := workflow.Jobs["go"]
	if !exists {
		return "missing root Go job"
	}
	if !unconditionalPreparationSetting(job.If, true) || !unconditionalPreparationSetting(job.Continue, false) {
		return "root Go job must be unconditional and gating"
	}
	setup, preparation, tests := -1, -1, -1
	const downloads = "(cd turing-backend/mcp-files && go mod download)\n(cd turing-backend/mcp-system && go mod download)"
	for index, step := range job.Steps {
		if strings.HasPrefix(step.Uses, "actions/setup-go@") {
			if !unconditionalPreparationSetting(step.If, true) || !unconditionalPreparationSetting(step.Continue, false) || !unconditionalPreparationSetting(step.With.Cache, true) {
				return "setup-go must run with caching enabled"
			}
			paths := make(map[string]bool)
			for _, line := range strings.Split(step.With.CachePaths, "\n") {
				paths[strings.TrimSpace(line)] = true
			}
			if !paths["go.sum"] || !paths["turing-backend/mcp-files/go.sum"] || !paths["turing-backend/mcp-system/go.mod"] {
				return "root setup-go cache paths are missing module inputs"
			}
			setup = index
		}
		if strings.TrimSpace(step.Run) == downloads {
			if !unconditionalPreparationSetting(step.If, true) || !unconditionalPreparationSetting(step.Continue, false) {
				return "module preparation must be unconditional and gating"
			}
			preparation = index
		}
		if strings.TrimSpace(step.Run) == "go test -tags sqlite_fts5 -race ./... -count=1" {
			if !unconditionalPreparationSetting(step.If, true) || !unconditionalPreparationSetting(step.Continue, false) {
				return "root tests must be unconditional and gating"
			}
			tests = index
		}
	}
	if setup < 0 || preparation <= setup || tests <= preparation {
		return "require active setup, module preparation and root tests in order"
	}
	return ""
}

func unconditionalPreparationSetting(node yaml.Node, want bool) bool {
	if node.Kind == 0 {
		return true
	}
	var value bool
	return node.Tag == "!!bool" && node.Decode(&value) == nil && value == want
}

func TestDocumentationPreparationFixtures(t *testing.T) {
	baseline, err := os.ReadFile("ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct{ name, old, replacement, want string }{
		{"baseline", "", "", ""},
		{"commented cache path", "            turing-backend/mcp-system/go.mod", "            # turing-backend/mcp-system/go.mod", "cache paths"},
		{"commented download", "          (cd turing-backend/mcp-files && go mod download)", "          # (cd turing-backend/mcp-files && go mod download)", "module preparation"},
		{"conditional preparation", "- name: Prepare documentation guard module caches", "- name: Prepare documentation guard module caches\n        if: false", "unconditional"},
		{"ignored preparation error", "- name: Prepare documentation guard module caches", "- name: Prepare documentation guard module caches\n        continue-on-error: true", "gating"},
		{"disabled root job", "  go:\n", "  go:\n    if: false\n", "root Go job"},
		{"non-gating root job", "  go:\n", "  go:\n    continue-on-error: true\n", "root Go job"},
		{"disabled setup", "- name: Set up Go", "- name: Set up Go\n        if: false", "setup-go"},
		{"disabled cache", "cache: true", "cache: false", "caching enabled"},
		{"disabled root tests", "- name: Run Go race tests", "- name: Run Go race tests\n        if: false", "root tests"},
		{"non-gating root tests", "- name: Run Go race tests", "- name: Run Go race tests\n        continue-on-error: true", "root tests"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			source := string(baseline)
			if fixture.old != "" {
				if !strings.Contains(source, fixture.old) {
					t.Fatal("workflow fixture anchor moved")
				}
				source = strings.Replace(source, fixture.old, fixture.replacement, 1)
			}
			problem := documentationPreparationProblem([]byte(source))
			if (fixture.want == "" && problem != "") || !strings.Contains(problem, fixture.want) {
				t.Fatalf("got %q, want %q", problem, fixture.want)
			}
		})
	}
}
