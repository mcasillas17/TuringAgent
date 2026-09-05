package docs

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
	"text/scanner"

	"gopkg.in/yaml.v3"
)

// Guarded claims have a closed status vocabulary and named implementation
// evidence. The fixtures exercise the same checker as the repository test.
type statusClaim struct {
	id             string
	clientRelevant bool
	evidence       []statusEvidence
}

type statusEvidence struct {
	path          string
	symbol        string
	require       string
	absent        string
	pattern       string
	rejectPattern string
	yamlPath      string
	workflowJob   string
	limitation    string
}

const statusBegin = "<!-- status-guard:begin -->"
const statusEnd = "<!-- status-guard:end -->"

var tableSeparatorPattern = regexp.MustCompile(`^:?-{3,}:?$`)

type statusResult struct {
	state       string
	witness     string
	diagnostics []string
}

func checkStatusDocument(files fs.FS, document string, claims []statusClaim) []string {
	results := evaluateStatusEvidence(files, claims)
	diagnostics := checkStatusTable(files, document, claims, results)
	for _, claim := range claims {
		for _, diagnostic := range results[claim.id].diagnostics {
			diagnostics = append(diagnostics, document+": "+diagnostic)
		}
	}
	return diagnostics
}

func evaluateStatusEvidence(files fs.FS, claims []statusClaim) map[string]statusResult {
	results := make(map[string]statusResult, len(claims))
	for _, claim := range claims {
		result := statusResult{state: "shipped"}
		implementationLimitation := false
		positiveImplementation := false
		for index, evidence := range claim.evidence {
			implementation := implementationFormat(evidence.path)
			positiveImplementation = positiveImplementation || (implementation && (evidence.require != "" || evidence.pattern != ""))
			if !implementation && evidence.limitation == "" {
				result.diagnostics = append(result.diagnostics, claim.id+": documentation context cannot justify shipped status; require implementation evidence")
			}
			if result.witness == "" || (implementation &&
				(evidence.limitation != "" || !implementationFormat(result.witness))) {
				result.witness = evidence.path
			}
			if evidence.limitation != "" {
				result.state = "pending"
				implementationLimitation = implementationLimitation || implementation
			}
			if problem := checkStatusEvidence(files, evidence); problem != "" {
				location := evidence.path
				if evidence.symbol != "" {
					location += " (" + evidence.symbol + ")"
				}
				result.diagnostics = append(result.diagnostics, fmt.Sprintf("%s: evidence %d in %s: %s; reconcile implementation, status and witness",
					claim.id, index+1, location, problem))
			}
		}
		if !positiveImplementation {
			result.diagnostics = append(result.diagnostics, claim.id+": invalid guard specification; require positive implementation evidence, not only absence")
		}
		if result.state == "pending" && !implementationLimitation {
			result.diagnostics = append(result.diagnostics, claim.id+": pending requires a concrete implementation limitation, not only a roadmap marker")
		}
		if len(result.diagnostics) != 0 {
			result.state = "" // Broken witnesses are unknown, never implicitly pending or shipped.
		}
		results[claim.id] = result
	}
	return results
}

func implementationFormat(name string) bool {
	switch path.Ext(name) {
	case ".go", ".dart", ".proto", ".xml", ".sh", ".yml", ".yaml":
		return true
	default:
		return false
	}
}

func checkStatusTable(files fs.FS, document string, claims []statusClaim, results map[string]statusResult) []string {
	data, err := fs.ReadFile(files, document)
	if err != nil {
		return []string{document + ": cannot read guarded document; restore it"}
	}
	parsed := parseStatusMarkdown(string(data))
	body, renderedTable, problem := parsed.guardedTableSource()
	if problem != "" {
		return []string{document + ": " + problem}
	}
	emptyScopes := parsed.emptyScopeRows(renderedTable)
	var diagnostics []string
	report := func(message string) { diagnostics = append(diagnostics, document+": "+message) }
	if len(claims) == 0 {
		report("invalid guard specification; no claims configured")
		return diagnostics
	}
	known := make(map[string]statusClaim, len(claims))
	for _, claim := range claims {
		if _, duplicate := known[claim.id]; duplicate || claim.id == "" || len(claim.evidence) == 0 {
			report("invalid guard specification; every claim needs a unique ID and evidence")
			return diagnostics
		}
		known[claim.id] = claim
	}
	seen := make(map[string]bool, len(claims))
	row := 0
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		row++
		rowReport := func(message string) { report(fmt.Sprintf("row %d: %s", row, message)) }
		cells := splitStatusRow(line)
		if len(cells) != 5 || cells[0] != "" || cells[4] != "" {
			rowReport("malformed guarded row; use Claim | Status | Scope with nonempty cells")
			continue
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		id, state := markdownCell(cells[1]), markdownCell(cells[2])
		if row == 1 {
			if id != "Claim" || state != "Status" || markdownCell(cells[3]) != "Scope" {
				rowReport("malformed table header; require Claim | Status | Scope")
			}
			continue
		}
		if row == 2 {
			if !tableSeparator(cells[1]) || !tableSeparator(cells[2]) || !tableSeparator(cells[3]) {
				rowReport("malformed table separator")
			}
			continue
		}
		if _, ok := known[id]; !ok {
			rowReport("unknown claim in guarded table; reconcile the entry with featureStatusClaims")
			continue
		}
		if seen[id] {
			rowReport(id + ": duplicate status entry; keep exactly one claim per document")
		}
		seen[id] = true
		if strings.TrimSpace(markdownCell(cells[3])) == "" || emptyScopes[row] {
			rowReport(id + ": malformed guarded row; rendered scope must contain text")
		}
		if !knownStatus(state) {
			rowReport(id + ": unknown status; use shipped or pending")
		} else if result, evaluated := results[id]; !evaluated {
			rowReport(id + ": missing evidence evaluation; reconcile the guard specification")
		} else if result.state != "" && state != result.state {
			rowReport(fmt.Sprintf("%s: status mismatch; validated witnesses require %s (see %s); reconcile the status, scope and evidence together",
				id, result.state, result.witness))
		}
	}
	if row < 2 {
		report("malformed guarded table; header and separator are required")
	}
	if renderedTable == nil {
		report("malformed guarded inventory; require exactly one rendered top-level GFM table")
	}
	for _, claim := range claims {
		if !seen[claim.id] {
			report(claim.id + ": missing required status entry; restore the scoped claim")
		}
	}
	return diagnostics
}

func splitStatusRow(line string) []string {
	var cells []string
	start, escaped := 0, false
	for i := 0; i < len(line); i++ {
		if line[i] == '|' && !escaped {
			cells = append(cells, line[start:i])
			start = i + 1
		}
		escaped = line[i] == '\\' && !escaped
	}
	return append(cells, line[start:])
}

func markdownCell(cell string) string {
	for _, wrapper := range []string{"`", "**"} {
		if len(cell) >= 2*len(wrapper) && strings.HasPrefix(cell, wrapper) && strings.HasSuffix(cell, wrapper) {
			return strings.TrimSpace(cell[len(wrapper) : len(cell)-len(wrapper)])
		}
	}
	return cell
}

func tableSeparator(cell string) bool {
	return tableSeparatorPattern.MatchString(cell)
}

func knownStatus(state string) bool {
	return state == "shipped" || state == "pending"
}

func checkStatusEvidence(files fs.FS, evidence statusEvidence) string {
	if evidence.path == "" || (evidence.require == "" && evidence.absent == "" && evidence.pattern == "" && evidence.rejectPattern == "") {
		return "missing evidence predicate"
	}
	if evidence.symbol != "" && path.Ext(evidence.path) != ".go" {
		return "symbol filtering is only supported for Go evidence"
	}
	if evidence.workflowJob != "" && (evidence.absent != "" || evidence.pattern != "" || evidence.rejectPattern != "" || evidence.yamlPath != "" || evidence.symbol != "") {
		return "workflow evidence must specify only its job and command"
	}
	if evidence.yamlPath != "" && path.Ext(evidence.path) != ".yml" && path.Ext(evidence.path) != ".yaml" {
		return "YAML selection is only supported for YAML evidence"
	}
	extension := path.Ext(evidence.path)
	if (extension == ".sh" || extension == ".yml" || extension == ".yaml") &&
		evidence.yamlPath == "" && evidence.workflowJob == "" && (evidence.require != "" || evidence.absent != "") {
		return "shell and YAML token predicates require parsed selection; use anchored patterns for shell evidence"
	}
	if (extension == ".yml" || extension == ".yaml") && evidence.yamlPath == "" && evidence.workflowJob == "" {
		return "YAML evidence requires a parsed mapping or workflow selection"
	}
	if evidence.require != "" || evidence.absent != "" {
		switch extension {
		case ".go", ".dart", ".proto", ".xml":
		default:
			if evidence.yamlPath == "" && evidence.workflowJob == "" {
				return "unsupported token predicate format; use an explicitly parsed format"
			}
		}
	}
	data, err := fs.ReadFile(files, evidence.path)
	if err != nil {
		return "cannot read evidence"
	}
	source := string(data)
	if evidence.workflowJob != "" {
		return checkWorkflowCommand(data, evidence.workflowJob, evidence.require)
	}
	if evidence.yamlPath != "" {
		var mapping map[string]any
		if err := yaml.Unmarshal(data, &mapping); err != nil {
			return "malformed YAML evidence"
		}
		var selected any = mapping
		for _, key := range strings.Split(evidence.yamlPath, "/") {
			object, ok := selected.(map[string]any)
			if !ok {
				return "missing YAML evidence mapping"
			}
			selected, ok = object[key]
			if !ok {
				return "missing YAML evidence key"
			}
		}
		rendered, err := yaml.Marshal(selected)
		if err != nil {
			return "cannot render YAML evidence"
		}
		source = string(rendered)
	}
	switch path.Ext(evidence.path) {
	case ".go":
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, evidence.path, data, parser.SkipObjectResolution)
		if err != nil {
			return "malformed Go evidence"
		}
		var node ast.Node = file
		if evidence.symbol != "" {
			var matches []*ast.FuncDecl
			for _, declaration := range file.Decls {
				if function, ok := declaration.(*ast.FuncDecl); ok && function.Name.Name == evidence.symbol {
					matches = append(matches, function)
				}
			}
			if len(matches) != 1 || matches[0].Body == nil {
				return "missing or ambiguous function evidence"
			}
			node = matches[0]
		}
		var formatted bytes.Buffer
		if err := format.Node(&formatted, set, node); err != nil {
			return "cannot format Go evidence"
		}
		source = formatted.String()
	case ".md":
		source = parseStatusMarkdown(source).topLevelHeadings()
	case ".xml":
		if path.Base(evidence.path) != "AndroidManifest.xml" {
			return "unsupported XML evidence; expected AndroidManifest.xml"
		}
		type permission struct {
			Name string `xml:"http://schemas.android.com/apk/res/android name,attr"`
		}
		var manifest struct {
			XMLName          xml.Name     `xml:"manifest"`
			Permissions      []permission `xml:"uses-permission"`
			SDK23Permissions []permission `xml:"uses-permission-sdk-23"`
		}
		if err := xml.Unmarshal(data, &manifest); err != nil {
			return "malformed Android manifest evidence"
		}
		source = ""
		for _, permission := range manifest.Permissions {
			source += "\n" + permission.Name
		}
		for _, permission := range manifest.SDK23Permissions {
			source += "\n" + permission.Name
		}
	}
	if evidence.rejectPattern != "" {
		pattern, err := regexp.Compile(evidence.rejectPattern)
		if err != nil {
			return "invalid rejected evidence pattern"
		}
		if pattern.MatchString(source) {
			return "limiting evidence changed"
		}
	}
	if evidence.pattern != "" {
		pattern, err := regexp.Compile(evidence.pattern)
		if err != nil {
			return "invalid evidence pattern"
		}
		if !pattern.MatchString(source) {
			return "required evidence pattern missing"
		}
	}
	source = syntaxTokens(source)
	if evidence.require != "" && !strings.Contains(source, syntaxTokens(evidence.require)) {
		return "required evidence contract missing"
	}
	if evidence.absent != "" && strings.Contains(source, syntaxTokens(evidence.absent)) {
		return "limiting evidence changed"
	}
	return ""
}

func checkWorkflowCommand(data []byte, jobName, command string) string {
	type runSettings struct {
		Shell            yaml.Node `yaml:"shell"`
		WorkingDirectory yaml.Node `yaml:"working-directory"`
	}
	type runDefaults struct {
		Run runSettings `yaml:"run"`
	}
	type step struct {
		Run             string      `yaml:"run"`
		If              yaml.Node   `yaml:"if"`
		ContinueOnError yaml.Node   `yaml:"continue-on-error"`
		Settings        runSettings `yaml:",inline"`
	}
	var workflow struct {
		On       yaml.Node   `yaml:"on"`
		Defaults runDefaults `yaml:"defaults"`
		Jobs     map[string]struct {
			If              yaml.Node   `yaml:"if"`
			ContinueOnError yaml.Node   `yaml:"continue-on-error"`
			Needs           yaml.Node   `yaml:"needs"`
			Defaults        runDefaults `yaml:"defaults"`
			Steps           []step      `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		return "malformed workflow evidence"
	}
	if !unfilteredPullRequestTrigger(workflow.On) {
		return "missing unfiltered pull_request trigger"
	}
	job, exists := workflow.Jobs[jobName]
	if !exists || !literalOrOmitted(job.If, true) || !literalOrOmitted(job.ContinueOnError, false) {
		return "missing or conditional/non-gating workflow job"
	}
	if job.Needs.Kind != 0 && (job.Needs.Kind != yaml.SequenceNode || len(job.Needs.Content) != 0) {
		return "dependent workflow job; reconcile upstream gating before relying on this witness"
	}
	matches := 0
	for _, step := range job.Steps {
		if strings.TrimSpace(step.Run) == command {
			if !literalOrOmitted(step.If, true) || !literalOrOmitted(step.ContinueOnError, false) {
				return "conditional/non-gating workflow step"
			}
			shell := firstDefinedSetting(step.Settings.Shell, job.Defaults.Run.Shell, workflow.Defaults.Run.Shell)
			if shell.Kind != 0 && (shell.Tag != "!!str" || (shell.Value != "bash" && shell.Value != "sh")) {
				return "unmodeled execution shell; require a built-in bash/sh shell"
			}
			directory := firstDefinedSetting(step.Settings.WorkingDirectory, job.Defaults.Run.WorkingDirectory, workflow.Defaults.Run.WorkingDirectory)
			if directory.Kind != 0 && (directory.Tag != "!!str" || (directory.Value != "." && directory.Value != "./")) {
				return "unmodeled working directory; run the guard command from the repository root"
			}
			matches++
		}
	}
	if matches != 1 {
		return "missing or ambiguous workflow command"
	}
	return ""
}

func firstDefinedSetting(settings ...yaml.Node) yaml.Node {
	for _, setting := range settings {
		if setting.Kind != 0 {
			return setting
		}
	}
	return yaml.Node{}
}

func literalOrOmitted(node yaml.Node, want bool) bool {
	if node.Kind == 0 {
		return true
	}
	var value bool
	return node.Tag == "!!bool" && node.Decode(&value) == nil && value == want
}

func unfilteredPullRequestTrigger(node yaml.Node) bool {
	switch node.Kind {
	case yaml.ScalarNode:
		return node.Value == "pull_request"
	case yaml.SequenceNode:
		for _, event := range node.Content {
			if event.Kind == yaml.ScalarNode && event.Value == "pull_request" {
				return true
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			if node.Content[i].Value == "pull_request" {
				options := node.Content[i+1]
				return options.Tag == "!!null" || (options.Kind == yaml.MappingNode && len(options.Content) == 0)
			}
		}
	}
	return false
}

// This is a lexical tripwire, not a compiler or a behavioral proof. C-style
// comments are skipped and quoted text is kept intact; spacing cannot join
// identifiers or turn a comment into a call. Dart single-quoted strings can be
// multi-character, unlike Go chars, so scanner's character-length errors are
// irrelevant here. Go syntax is validated separately above.
func syntaxTokens(source string) string {
	var lexer scanner.Scanner
	lexer.Init(strings.NewReader(source))
	lexer.Error = func(*scanner.Scanner, string) {}
	var tokens strings.Builder
	tokens.WriteByte(0)
	for tok := lexer.Scan(); tok != scanner.EOF; tok = lexer.Scan() {
		tokens.WriteString(lexer.TokenText())
		tokens.WriteByte(0)
	}
	return tokens.String()
}

func TestDocumentedFeatureStatus(t *testing.T) {
	results := evaluateStatusEvidence(os.DirFS("../.."), featureStatusClaims())
	for _, claim := range featureStatusClaims() {
		documents := "docs/NORTH_STAR.md"
		if claim.clientRelevant {
			documents += ", turing-client/turing_app/README.md"
		}
		for _, diagnostic := range results[claim.id].diagnostics {
			t.Errorf("%s: %s", documents, diagnostic)
		}
	}
	for _, document := range []string{"docs/NORTH_STAR.md", "turing-client/turing_app/README.md"} {
		t.Run(document, func(t *testing.T) {
			for _, diagnostic := range checkStatusTable(os.DirFS("../.."), document, claimsForDocument(document), results) {
				t.Error(diagnostic)
			}
		})
	}
}

func claimsForDocument(document string) []statusClaim {
	var claims []statusClaim
	for _, claim := range featureStatusClaims() {
		if document == "turing-client/turing_app/README.md" && !claim.clientRelevant {
			continue
		}
		claims = append(claims, claim)
	}
	return claims
}

func TestCanonicalRoadmapLinks(t *testing.T) {
	for _, document := range []struct{ path, target string }{
		{"README.md", "docs/NORTH_STAR.md"},
		{"turing-client/turing_app/README.md", "../../docs/NORTH_STAR.md"},
		{"docs/architecture/tech-stack.md", "../NORTH_STAR.md"},
		{"docs/architecture/audit-read-api.md", "../NORTH_STAR.md"},
		{"docs/architecture/tur-022-encrypted-database-retirement-design.md", "../NORTH_STAR.md"},
		{"docs/mcp-security-and-integration.md", "NORTH_STAR.md"},
		{"docs/superpowers/integration-checklist.md", "../NORTH_STAR.md"},
		{"docs/VISION.md", "NORTH_STAR.md"},
		{"docs/architecture/2026-08-18-personal-agent-audit.md", "../NORTH_STAR.md"},
		{"docs/superpowers/specs/2026-05-09-project-turing-v1-design.md", "../../NORTH_STAR.md"},
		{"docs/superpowers/specs/2026-05-09-project-turing-v1-design-copilot.md", "../../NORTH_STAR.md"},
		{"docs/superpowers/specs/2026-05-09-project-turing-v1-design-claude.md", "../../NORTH_STAR.md"},
		{"docs/superpowers/specs/2026-05-10-project-turing-v1-consolidation-report.md", "../../NORTH_STAR.md"},
		{"docs/superpowers/specs/2026-05-10-project-turing-v1-consolidation-report-claude.md", "../../NORTH_STAR.md"},
		{"docs/superpowers/plans/2026-05-09-project-turing-v1.md", "../../NORTH_STAR.md"},
		{"docs/superpowers/plans/2026-05-10-project-turing-v1-hybrid-runtime.md", "../../NORTH_STAR.md"},
		{"docs/superpowers/plans/2026-05-15-turing-go-grpc-migration.md", "../../NORTH_STAR.md"},
		{"docs/superpowers/specs/2026-05-15-turing-go-grpc-migration-design.md", "../../NORTH_STAR.md"},
	} {
		parsed := parseStatusMarkdown(repoFile(t, document.path))
		if document.path == "docs/VISION.md" || document.path == "docs/architecture/2026-08-18-personal-agent-audit.md" {
			if problem := parsed.historicalPointerProblem(document.target); problem != "" {
				t.Errorf("%s: %s", document.path, problem)
			}
		} else if strings.Contains(document.path, "/2026-05-") {
			kind := "design"
			if strings.Contains(document.path, "/plans/") {
				kind = "plan"
			} else if strings.Contains(document.path, "report") {
				kind = "report"
			}
			if problem := parsed.historicalNoticeProblem(document.target, kind); problem != "" {
				t.Errorf("%s: %s", document.path, problem)
			}
		} else if matching, _ := parsed.canonicalLinks(parsed.document, document.target); matching == 0 {
			t.Errorf("%s: missing visible canonical roadmap link to %s; restore the pointer instead of creating a competing roadmap", document.path, document.target)
		}
	}
}
