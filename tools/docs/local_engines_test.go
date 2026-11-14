package docs

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

const (
	cliSource       = "turing-backend/cmd/turing/internal/cli/cli.go"
	engineGuide     = "docs/architecture/local-engines.md"
	recommendSource = "turing-backend/cmd/turing/internal/machine/recommend.go"
	checkSource     = "turing-backend/agent-runtime-go/cmd/runtime/check_engine.go"
)

// TestTheEngineGuideIsWhereTheCommandPointsAt keeps `engine detect`'s "see
// <path>" pointing at a file that exists.
func TestTheEngineGuideIsWhereTheCommandPointsAt(t *testing.T) {
	path := extract(t, repoFile(t, "turing-backend/cmd/turing/internal/cli/engine_detect.go"),
		"engine_detect.go", `engineDocs\s*=\s*"([^"]+)"`)
	if path != engineGuide {
		t.Fatalf("engine detect points at %s, but this guard reads %s; update both together", path, engineGuide)
	}
	repoFile(t, path)
}

// TestTheEngineGuideStatesWhatTheCodeDoes checks the guide's numbers against
// the code that holds them: the memory classes and the model hints in the
// recommendation, and the four check-engine steps with their timeouts.
func TestTheEngineGuideStatesWhatTheCodeDoes(t *testing.T) {
	guide := repoFile(t, engineGuide)
	recommend := repoFile(t, recommendSource)
	check := repoFile(t, checkSource)

	floor64 := extract(t, recommend, recommendSource, `class64Floor\s*=\s*(\d+) \* gib`)
	floor96 := extract(t, recommend, recommendSource, `class96Floor\s*=\s*(\d+) \* gib`)
	if !attributes(guide, "64 GB class", floor64+" GiB") || !attributes(guide, "96 GB+ class", floor96+" GiB") {
		t.Errorf("%s does not state the memory classes as %s and %s GiB, which recommend.go draws them at", engineGuide, floor64, floor96)
	}
	for _, name := range []string{"ollamaModel", "ds4ServedModel"} {
		model := extract(t, recommend, recommendSource, name+`\s*=\s*"([^"]+)"`)
		if !strings.Contains(guide, "`"+model+"`") {
			t.Errorf("%s does not name the model `%s` (recommend.go's %s)", engineGuide, model, name)
		}
	}
	rows := regexp.MustCompile(`ds4\("(\w+)", "([^"]+)"`).FindAllStringSubmatch(recommend, -1)
	if len(rows) == 0 {
		t.Fatalf("%s has no ds4 rows; this guard reads them from there", recommendSource)
	}
	for _, row := range rows {
		if !attributes(guide, "ds4 ("+row[1]+")", row[2]) {
			t.Errorf("%s has no row for ds4 (%s) with the model hint %q", engineGuide, row[1], row[2])
		}
	}

	seconds := map[string]string{
		"listing":    extract(t, check, checkSource, `listingTimeout\s*=\s*(\d+) \* time\.Second`) + " s",
		"completion": extract(t, check, checkSource, `completionTimeout\s*=\s*(\d+) \* time\.Second`) + " s",
	}
	steps := regexp.MustCompile(`\{"([a-z ]+)", (listing|completion)Timeout, probe\.`).FindAllStringSubmatch(check, -1)
	if len(steps) != 4 {
		t.Fatalf("%s names %d steps, want 4; this guard reads them from there", checkSource, len(steps))
	}
	for i, step := range steps {
		label := fmt.Sprintf("%d/4 %s", i+1, step[1])
		if !attributes(guide, label, seconds[step[2]]) {
			t.Errorf("%s has no line for step %q with its %s timeout", engineGuide, label, seconds[step[2]])
		}
	}
	for _, name := range []string{"aliasNote", "probeToolName"} {
		value := extract(t, check, checkSource, name+`\s*=\s*"([^"]+)"`)
		if !strings.Contains(guide, value) {
			t.Errorf("%s does not state %q (check_engine.go's %s)", engineGuide, value, name)
		}
	}
	success := extract(t, repoFile(t, "turing-backend/agent-runtime-go/cmd/runtime/main.go"),
		"main.go", `Success: "([^"]+)"`)
	if !strings.Contains(guide, success) {
		t.Errorf("%s does not state check-engine's success line %q", engineGuide, success)
	}
}

// TestTheReadmeListsEveryCommandInTheUsage keeps the README's command table
// in step with ./turing help.
func TestTheReadmeListsEveryCommandInTheUsage(t *testing.T) {
	usage := extract(t, repoFile(t, cliSource), cliSource, "(?s)const Usage = `([^`]*)`")
	readme := repoFile(t, "README.md")
	commands := regexp.MustCompile(`(?m)^  ([a-z]+)(?: (detect|use|check))?`).FindAllStringSubmatch(usage, -1)
	if len(commands) < 10 {
		t.Fatalf("found %d commands in the usage, want at least 10; this guard reads them from there", len(commands))
	}
	for _, command := range commands {
		name := strings.TrimSpace(command[1] + " " + command[2])
		if !strings.Contains(readme, "./turing "+name) {
			t.Errorf("README.md does not list `./turing %s`, which ./turing help does", name)
		}
	}
}

// staleSetupClaims finds a sentence that calls setup, the engine commands or
// check-engine pending, in either order. Statements wrap, so the text is
// normalised to one line first.
var staleSetupClaims = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(\bsetup\b|\bengine (detect|use|check)\b|\bengine\b[^.|]{0,20}\bcommands\b|check-engine)(?:[^.]|\.\S){0,80}\b(pending|unimplemented|not implemented|not available yet|arrive in PR 3|still planned)`),
	regexp.MustCompile(`(?i)\b(pending|unimplemented|not implemented|not available yet|still planned)\b(?:[^.]|\.\S){0,40}(\bsetup\b|\bengine (detect|use|check)\b|check-engine)`),
	regexp.MustCompile(`(?i)hardware-guided engine (choice|selection)(?:[^.]|\.\S){0,20}(is not|remains)`),
	regexp.MustCompile(`(?i)OPS-001 is partly|OPS-001, partial|partly shipped: #171|remaining setup work`),
}

// TestNoDocumentCallsSetupOrTheEngineCommandsPending fails when a document
// goes on saying that what OPS-001 PR 3 delivered is still to come.
func TestNoDocumentCallsSetupOrTheEngineCommandsPending(t *testing.T) {
	for _, path := range []string{
		"README.md", "CLAUDE.md", "docs/NORTH_STAR.md",
		"docs/architecture/system-flows.md", "docs/architecture/tech-stack.md",
		"docs/architecture/turing-flow.template.html",
		"docs/superpowers/specs/2026-10-25-turing-host-command-design.md",
		"docs/superpowers/specs/2026-10-25-turing-local-inference-engines-design.md",
	} {
		text := strings.Join(strings.Fields(repoFile(t, path)), " ")
		for _, claim := range staleSetupClaims {
			for _, match := range claim.FindAllString(text, -1) {
				t.Errorf("%s still says setup or the engine commands are pending: %q", path, match)
			}
		}
	}
}
