package docs

import (
	"fmt"
	"io/fs"
	"maps"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

// Each fixture owns its files. Mutations never write to the checkout.
func TestStatusGuardFixtures(t *testing.T) {
	const document = "guide.md"
	const baseline = `# Current capabilities

<!-- status-guard:begin -->
| Claim | Status | Scope |
| --- | --- | --- |
| search | shipped | Searches conversations. |
| mobile | pending | No paired remote access. |
<!-- status-guard:end -->
`
	claims := []statusClaim{
		{id: "search", evidence: []statusEvidence{{path: "search.go", require: `return backend.Search()`}}},
		{id: "mobile", evidence: []statusEvidence{{path: "network.go", require: `return "127.0.0.1"`, limitation: "loopback only"}}},
	}
	for _, test := range []struct {
		name   string
		mutate func(fstest.MapFS) error
		want   []string
	}{
		{name: "valid baseline"},
		{name: "false shipped", mutate: replaceFixture(document, "| mobile | pending |", "| mobile | shipped |"), want: []string{"mobile", document, "pending", "network.go", "reconcile"}},
		{name: "false pending", mutate: replaceFixture(document, "| search | shipped |", "| search | pending |"), want: []string{"search", document, "shipped", "search.go"}},
		{name: "false placeholder", mutate: replaceFixture(document, "| search | shipped |", "| search | placeholder |"), want: []string{"search", document, "unknown status"}},
		{name: "unknown status", mutate: replaceFixture(document, "| mobile | pending |", "| mobile | maybe |"), want: []string{"mobile", "unknown status"}},
		{name: "unsupported partial status", mutate: replaceFixture(document, "| mobile | pending |", "| mobile | partial |"), want: []string{"mobile", "unknown status"}},
		{name: "contradictory duplicate", mutate: replaceFixture(document, "<!-- status-guard:end -->", "| search | pending | Contradiction. |\n<!-- status-guard:end -->"), want: []string{"search", "duplicate"}},
		{name: "missing claim", mutate: replaceFixture(document, "| search | shipped | Searches conversations. |\n", ""), want: []string{"search", "missing"}},
		{name: "missing status", mutate: replaceFixture(document, "| search | shipped |", "| search | |"), want: []string{"search", "unknown status"}},
		{name: "missing scope", mutate: replaceFixture(document, "Searches conversations.", ""), want: []string{"malformed", document}},
		{name: "empty bold scope", mutate: replaceFixture(document, "Searches conversations.", "** **"), want: []string{"malformed", document}},
		{name: "empty code scope", mutate: replaceFixture(document, "Searches conversations.", "` `"), want: []string{"malformed", document}},
		{name: "comment-only scope", mutate: replaceFixture(document, "Searches conversations.", "<!-- hidden -->"), want: []string{"rendered scope", document}},
		{name: "empty link scope", mutate: replaceFixture(document, "Searches conversations.", "[](https://example.invalid)"), want: []string{"rendered scope", document}},
		{name: "entity-only scope", mutate: replaceFixture(document, "Searches conversations.", "&nbsp;"), want: []string{"rendered scope", document}},
		{name: "zero-width scope", mutate: replaceFixture(document, "Searches conversations.", "&#x200B;"), want: []string{"rendered scope"}},
		{name: "format-only scope", mutate: replaceFixture(document, "Searches conversations.", "&#xFE0F;&#x034F;&#x3164;"), want: []string{"rendered scope"}},
		{name: "non-Latin scope", mutate: replaceFixture(document, "Searches conversations.", "&#x641C;&#x7D22;")},
		{name: "literal entity scope", mutate: replaceFixture(document, "Searches conversations.", "`&nbsp;`")},
		{name: "missing header", mutate: replaceFixture(document, "| Claim | Status | Scope |\n", ""), want: []string{"malformed table header"}},
		{name: "missing separator", mutate: replaceFixture(document, "| --- | --- | --- |\n", ""), want: []string{"malformed table separator"}},
		{name: "empty guarded block", mutate: replaceFixture(document, baseline, statusBegin+"\n"+statusEnd), want: []string{"header and separator are required"}},
		{name: "split table", mutate: replaceFixture(document, "| search |", "\n| search |"), want: []string{"rendered top-level GFM table"}},
		{name: "two tables", mutate: replaceFixture(document, statusEnd, "\n| Claim | Status | Scope |\n| --- | --- | --- |\n| search | shipped | Second table. |\n"+statusEnd), want: []string{"rendered top-level GFM table"}},
		{name: "text before first pipe", mutate: replaceFixture(document, "| search |", "prefix | search |"), want: []string{"malformed guarded row"}},
		{name: "duplicate header", mutate: replaceFixture(document, "| search |", "| Claim | Status | Scope |\n| search |"), want: []string{"unknown claim", "row 3"}},
		{name: "fenced table", mutate: replaceFixture(document, baseline, "````markdown\n"+baseline+"\n````"), want: []string{"delimiters"}},
		{name: "tilde fenced table", mutate: replaceFixture(document, baseline, "~~~\n"+baseline+"\n~~~"), want: []string{"delimiters"}},
		{name: "HTML code table", mutate: replaceFixture(document, baseline, "<pre>\n"+baseline+"\n</pre>"), want: []string{"raw HTML", document}},
		{name: "self-closing HTML code table", mutate: replaceFixture(document, baseline, "<pre/>\n"+baseline), want: []string{"raw HTML", document}},
		{name: "processing instruction hides table", mutate: replaceFixture(document, baseline, "<?hidden\n"+baseline+"\n?>"), want: []string{"delimiters", document}},
		{name: "CDATA hides table", mutate: replaceFixture(document, baseline, "<![CDATA[\n"+baseline+"\n]]>"), want: []string{"delimiters", document}},
		{name: "literal HTML example", mutate: replaceFixture(document, "# Current capabilities", "# Current capabilities\n\nA literal `<pre>` example is not an HTML wrapper.")},
		{name: "double-backtick HTML literal", mutate: replaceFixture(document, "# Current capabilities", "# Current capabilities\n\nUse ``<pre>`` as text.")},
		{name: "ordinary angle prose", mutate: replaceFixture(document, "# Current capabilities", "# Current capabilities\n\nUse List<String> in Dart or compare a<b and c>d.")},
		{name: "indented code table", mutate: replaceFixture(document, baseline, "    "+strings.ReplaceAll(baseline, "\n", "\n    ")), want: []string{"indented", document}},
		{name: "three-space table indentation", mutate: replaceFixture(document, "\n|", "\n   |")},
		{name: "indented rendered data row", mutate: replaceFixture(document, "| search | shipped | Searches conversations. |", "    | search | shipped | Searches conversations. |")},
		{name: "extra column", mutate: replaceFixture(document, "Searches conversations.", "Searches | conversations."), want: []string{"malformed", document}},
		{name: "unknown claim", mutate: replaceFixture(document, "| search |", "| surprise |"), want: []string{"unknown claim", document}},
		{name: "missing begin", mutate: replaceFixture(document, "<!-- status-guard:begin -->", ""), want: []string{"delimiters", document}},
		{name: "reversed delimiters", mutate: replaceFixture(document, baseline, statusEnd+"\n"+statusBegin), want: []string{"delimiters", document}},
		{name: "duplicate block", mutate: replaceFixture(document, "<!-- status-guard:end -->", "<!-- status-guard:end -->\n<!-- status-guard:begin -->\n<!-- status-guard:end -->"), want: []string{"delimiters", document}},
		{name: "malformed marker", mutate: replaceFixture(document, "<!-- status-guard:end -->", "<!-- status-guard:finish -->"), want: []string{"delimiters", document}},
		{name: "missing document", mutate: func(files fstest.MapFS) error { delete(files, document); return nil }, want: []string{document, "cannot read"}},
		{name: "missing evidence", mutate: func(files fstest.MapFS) error { delete(files, "search.go"); return nil }, want: []string{"search", document, "search.go", "cannot read"}},
		{name: "missing pending evidence", mutate: func(files fstest.MapFS) error { delete(files, "network.go"); return nil }, want: []string{"mobile", document, "network.go", "cannot read"}},
		{name: "implementation drift", mutate: replaceFixture("search.go", "backend.Search()", "nil"), want: []string{"search", "search.go", "evidence", "reconcile"}},
		{name: "comment is not evidence", mutate: replaceFixture("search.go", "return backend.Search()", "/* return backend.Search() */ return nil"), want: []string{"search", "search.go", "evidence"}},
		{name: "malformed source", mutate: replaceFixture("search.go", "return backend.Search()", "return ("), want: []string{"search", "malformed Go evidence"}},
		{name: "harmless wording", mutate: replaceFixture(document, "Searches conversations.", "Finds earlier messages without changing scope.")},
		{name: "escaped pipe in wording", mutate: replaceFixture(document, "Searches conversations.", `Searches conversations \| messages.`)},
		{name: "bold headers", mutate: replaceFixture(document, "| Claim | Status | Scope |", "| **Claim** | **Status** | **Scope** |")},
		{name: "harmless table formatting", mutate: replaceFixture(document, "| search | shipped |", "|  `search`  | **shipped** |")},
		{name: "harmless source formatting", mutate: replaceFixture("search.go", "return backend.Search()", "return backend.\n Search( )")},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := fstest.MapFS{
				document:     {Data: []byte(baseline)},
				"search.go":  {Data: []byte("package example\nfunc search() any { return backend.Search() }\n")},
				"network.go": {Data: []byte("package example\nfunc bind() string { return \"127.0.0.1\" }\n")},
			}
			if test.mutate != nil {
				if err := test.mutate(files); err != nil {
					t.Fatal(err)
				}
			}
			diagnostics := checkStatusDocument(files, document, claims)
			if len(test.want) == 0 {
				if len(diagnostics) != 0 {
					t.Fatalf("valid fixture rejected: %s", strings.Join(diagnostics, "\n"))
				}
				return
			}
			message := strings.Join(diagnostics, "\n")
			for _, want := range test.want {
				if !strings.Contains(message, want) {
					t.Errorf("diagnostics %q do not contain %q", message, want)
				}
			}
		})
	}
}

func replaceFixture(path, old, replacement string) func(fstest.MapFS) error {
	return func(files fstest.MapFS) error {
		var fields []string
		for _, field := range strings.Fields(old) {
			fields = append(fields, regexp.QuoteMeta(field))
		}
		pattern := regexp.MustCompile(strings.Join(fields, `\s+`))
		file, exists := files[path]
		if !exists {
			return fmt.Errorf("fixture file missing: %s", path)
		}
		source := string(file.Data)
		if strings.Contains(source, old) {
			source = strings.ReplaceAll(source, old, replacement)
		} else if len(fields) > 0 && pattern.MatchString(source) {
			source = pattern.ReplaceAllStringFunc(source, func(string) string { return replacement })
		} else {
			return fmt.Errorf("fixture replacement has no match in %s", path)
		}
		files[path] = &fstest.MapFile{Data: []byte(source)}
		return nil
	}
}

func TestStatusEvidenceFixtures(t *testing.T) {
	for _, test := range []struct {
		name     string
		evidence statusEvidence
		source   string
		want     string
	}{
		{"missing predicate", statusEvidence{path: "source.go"}, "package fixture", "missing evidence predicate"},
		{"missing function", statusEvidence{path: "source.go", symbol: "missing", require: "return nil"}, "package fixture\nfunc present() any { return nil }", "missing or ambiguous function"},
		{"wrong function", statusEvidence{path: "source.go", symbol: "target", require: "backend.Search()"}, "package fixture\nfunc target() any { return nil }\nfunc unrelated() any { return backend.Search() }", "required evidence contract missing"},
		{"ambiguous function", statusEvidence{path: "source.go", symbol: "target", require: "return nil"}, "package fixture\nfunc (a A) target() any { return nil }\nfunc (b B) target() any { return nil }", "ambiguous function"},
		{"bodyless function", statusEvidence{path: "source.go", symbol: "target", require: "target"}, "package fixture\nfunc target()", "missing or ambiguous function"},
		{"bad pattern", statusEvidence{path: "source.md", pattern: "["}, "words", "invalid evidence pattern"},
		{"bad reject pattern", statusEvidence{path: "source.go", rejectPattern: "["}, "package fixture", "invalid rejected evidence pattern"},
		{"non-Go symbol", statusEvidence{path: "source.dart", symbol: "search", require: "api.search()"}, "api.search();", "symbol filtering is only supported for Go"},
		{"mixed workflow predicates", statusEvidence{path: "ci.yml", workflowJob: "proto", require: "check", pattern: "check"}, "jobs: {}", "workflow evidence must specify only"},
		{"non-YAML selection", statusEvidence{path: "source.go", yamlPath: "a/b", require: "x"}, "package fixture", "YAML selection is only supported"},
		{"missing YAML key", statusEvidence{path: "source.yml", yamlPath: "a/b", require: "x"}, "a: {}", "missing YAML evidence key"},
		{"hidden task marker", statusEvidence{path: "source.md", pattern: `(?m)^### A2A-001`}, "<!--\n### A2A-001 - hidden\n-->", "required evidence pattern missing"},
		{"fenced task marker", statusEvidence{path: "source.md", pattern: `(?m)^### A2A-001`}, "```md\n### A2A-001 - hidden\n```", "required evidence pattern missing"},
		{"visible task marker", statusEvidence{path: "source.md", pattern: `(?m)^### A2A-001`}, "### A2A-001 - Task", ""},
		{"commented YAML policy", statusEvidence{path: "source.yaml", yamlPath: "breaking/use", pattern: `FILE`}, "breaking:\n # use:\n  - FILE", "missing YAML evidence mapping"},
		{"commented shell command", statusEvidence{path: "source.sh", pattern: `(?m)^buf breaking`}, "# buf breaking proto", "required evidence pattern missing"},
		{"shell token predicate refused", statusEvidence{path: "source.sh", require: "buf breaking proto"}, "# buf breaking proto", "shell and YAML token predicates"},
		{"unparsed YAML token predicate refused", statusEvidence{path: "source.yml", require: "run: check"}, "# run: check", "shell and YAML token predicates"},
		{"unparsed YAML pattern refused", statusEvidence{path: "source.yml", pattern: "FILE"}, "# use: FILE", "YAML evidence requires"},
		{"Dockerfile token predicate refused", statusEvidence{path: "Dockerfile", require: "FROM golang"}, "# FROM golang", "unsupported token predicate"},
		{"Python token predicate refused", statusEvidence{path: "source.py", require: "api.search()"}, "# api.search()", "unsupported token predicate"},
		{"absent literal present", statusEvidence{path: "source.go", require: "package fixture", absent: `"initialize"`}, "package fixture\nvar method = \"initialize\"", "limiting evidence changed"},
		{"commented Dart call", statusEvidence{path: "source.dart", require: "api.search()"}, "// api.search()\n/* api.search() */", "required evidence contract missing"},
		{"commented Dart pattern refused", statusEvidence{path: "source.dart", pattern: "api.search"}, "// api.search()", "regex predicates are unsupported"},
		{"commented proto pattern refused", statusEvidence{path: "source.proto", rejectPattern: "service"}, "// service Example", "regex predicates are unsupported"},
		{"Dart URL is not a comment", statusEvidence{path: "source.dart", require: "'https://example.invalid'"}, "final url = 'https://example.invalid';", ""},
		{"identifier boundary", statusEvidence{path: "source.dart", require: "api.search()"}, "otherapi.search();", "required evidence contract missing"},
		{"unsupported XML", statusEvidence{path: "source.xml", require: "anything"}, "<anything/>", "unsupported XML evidence"},
		{"malformed XML", statusEvidence{path: "AndroidManifest.xml", absent: "android.permission.INTERNET"}, "<manifest>", "malformed Android manifest"},
		{"wrong XML root", statusEvidence{path: "AndroidManifest.xml", absent: "android.permission.INTERNET"}, "<other/>", "malformed Android manifest"},
		{"commented permission", statusEvidence{path: "AndroidManifest.xml", require: "android.permission.INTERNET"}, `<manifest><!-- <uses-permission android:name="android.permission.INTERNET"/> --></manifest>`, "required evidence contract missing"},
		{"wrong permission namespace", statusEvidence{path: "AndroidManifest.xml", require: "android.permission.INTERNET"}, `<manifest><uses-permission name="android.permission.INTERNET"/></manifest>`, "required evidence contract missing"},
		{"formatted permission", statusEvidence{path: "AndroidManifest.xml", require: "android.permission.INTERNET"}, "<manifest xmlns:a='http://schemas.android.com/apk/res/android'>\n <uses-permission a:name = 'android.permission.INTERNET' />\n</manifest>", ""},
		{"SDK 23 permission limits absence", statusEvidence{path: "AndroidManifest.xml", absent: "android.permission.INTERNET"}, `<manifest xmlns:android="http://schemas.android.com/apk/res/android"><uses-permission-sdk-23 android:name="android.permission.INTERNET"/></manifest>`, "limiting evidence changed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := fstest.MapFS{test.evidence.path: {Data: []byte(test.source)}}
			got := checkStatusEvidence(files, test.evidence)
			if (test.want == "" && got != "") || !strings.Contains(got, test.want) {
				t.Fatalf("got %q; want %q", got, test.want)
			}
		})
	}
}

func TestStatusGuardRejectsInvalidSpecifications(t *testing.T) {
	files := fstest.MapFS{"guide.md": {Data: []byte(statusBegin + "\n" + statusEnd)}}
	valid := statusClaim{id: "search", evidence: []statusEvidence{{path: "source.go", require: "Search"}}}
	for name, claims := range map[string][]statusClaim{
		"empty":       {},
		"missing ID":  {{evidence: valid.evidence}},
		"no evidence": {{id: "search"}},
		"duplicate":   {valid, valid},
	} {
		t.Run(name, func(t *testing.T) {
			if got := strings.Join(checkStatusDocument(files, "guide.md", claims), "\n"); !strings.Contains(got, "invalid guard specification") {
				t.Fatalf("invalid specification accepted: %s", got)
			}

		})
	}
}

func TestWorkflowEvidenceFixtures(t *testing.T) {
	const baseline = "on: pull_request\njobs:\n  proto:\n    steps:\n      - run: tools/proto/check.sh\n"
	for _, test := range []struct{ name, source, want string }{
		{"baseline", baseline, ""},
		{"job absent", "on: pull_request\njobs: {}", "missing"},
		{"job conditional", "on: pull_request\njobs:\n  proto:\n    if: false\n    steps:\n      - run: tools/proto/check.sh\n", "conditional"},
		{"job non-gating", "on: pull_request\njobs:\n  proto:\n    continue-on-error: true\n    steps:\n      - run: tools/proto/check.sh\n", "non-gating"},
		{"step conditional", baseline + "        if: false\n", "conditional"},
		{"step non-gating", baseline + "        continue-on-error: true\n", "non-gating"},
		{"command ambiguous", baseline + "      - run: tools/proto/check.sh\n", "ambiguous"},
		{"command commented", strings.ReplaceAll(baseline, "run:", "# run:"), "missing"},
		{"malformed YAML", "jobs: [", "malformed"},
		{"no PR trigger", strings.ReplaceAll(baseline, "on: pull_request", "on: workflow_dispatch"), "pull_request"},
		{"PR trigger list", strings.ReplaceAll(baseline, "on: pull_request", "on: [push, pull_request]"), ""},
		{"PR trigger mapping", strings.ReplaceAll(baseline, "on: pull_request", "on:\n  pull_request: {}"), ""},
		{"PR path filter", strings.ReplaceAll(baseline, "on: pull_request", "on:\n  pull_request:\n    paths: ['docs/**']"), "pull_request"},
		{"job dependency", strings.ReplaceAll(baseline, "  proto:", "  proto:\n    needs: optional"), "dependent"},
		{"job explicit gating", strings.ReplaceAll(baseline, "  proto:", "  proto:\n    continue-on-error: false"), ""},
		{"step explicit gating", baseline + "        continue-on-error: false\n", ""},
		{"job always true", strings.ReplaceAll(baseline, "  proto:", "  proto:\n    if: true"), ""},
		{"step always true", baseline + "        if: true\n", ""},
		{"unknown condition", baseline + "        if: '${{ inputs.enabled }}'\n", "conditional"},
		{"quoted condition", baseline + "        if: 'true'\n", "conditional"},
		{"quoted continue-on-error", baseline + "        continue-on-error: 'false'\n", "non-gating"},
		{"custom step shell", baseline + "        shell: 'true {0}'\n", "execution shell"},
		{"custom job shell", strings.ReplaceAll(baseline, "  proto:", "  proto:\n    defaults:\n      run:\n        shell: 'true {0}'"), "execution shell"},
		{"custom workflow shell", "defaults:\n  run:\n    shell: 'true {0}'\n" + baseline, "execution shell"},
		{"explicit bash", baseline + "        shell: bash\n", ""},
		{"explicit sh", baseline + "        shell: sh\n", ""},
		{"step shell overrides default", "defaults:\n  run:\n    shell: 'true {0}'\n" + baseline + "        shell: bash\n", ""},
		{"job shell overrides default", "defaults:\n  run:\n    shell: 'true {0}'\n" + strings.ReplaceAll(baseline, "  proto:", "  proto:\n    defaults:\n      run:\n        shell: bash"), ""},
		{"step shell overrides job", strings.ReplaceAll(baseline, "  proto:", "  proto:\n    defaults:\n      run:\n        shell: 'true {0}'") + "        shell: bash\n", ""},
		{"wrong working directory", baseline + "        working-directory: /tmp\n", "working directory"},
		{"job working directory", strings.ReplaceAll(baseline, "  proto:", "  proto:\n    defaults:\n      run:\n        working-directory: /tmp"), "working directory"},
		{"workflow working directory", "defaults:\n  run:\n    working-directory: /tmp\n" + baseline, "working directory"},
		{"step directory overrides default", "defaults:\n  run:\n    working-directory: /tmp\n" + baseline + "        working-directory: .\n", ""},
		{"step directory overrides job", strings.ReplaceAll(baseline, "  proto:", "  proto:\n    defaults:\n      run:\n        working-directory: /tmp") + "        working-directory: .\n", ""},
		{"job directory overrides workflow", "defaults:\n  run:\n    working-directory: /tmp\n" + strings.ReplaceAll(baseline, "  proto:", "  proto:\n    defaults:\n      run:\n        working-directory: ."), ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := checkWorkflowCommand([]byte(test.source), "proto", "tools/proto/check.sh")
			if (test.want == "" && got != "") || !strings.Contains(got, test.want) {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestEvidencePolarity(t *testing.T) {
	files := fstest.MapFS{"source.dart": {Data: []byte("loadState(); listTools();")}}
	for _, test := range []struct {
		name, want string
		evidence   statusEvidence
	}{
		{"no placeholders supports wired page", "shipped", statusEvidence{path: "source.dart", require: "loadState()", absent: "PlaceholderPage("}},
		{"missing initialization limits protocol", "pending", statusEvidence{path: "source.dart", require: "listTools()", absent: "initialize(", limitation: "initialization is not implemented"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			claim := statusClaim{id: "capability", evidence: []statusEvidence{test.evidence}}
			result := evaluateStatusEvidence(files, []statusClaim{claim})[claim.id]
			if len(result.diagnostics) != 0 || result.state != test.want {
				t.Fatalf("evidence classification = %+v, want %s", result, test.want)
			}
		})
	}
	for _, claim := range featureStatusClaims() {
		for _, evidence := range claim.evidence {
			if evidence.path == "docs/NORTH_STAR.md" && evidence.limitation == "" {
				t.Errorf("%s: a pendingTask witness must carry its limiting meaning", claim.id)
			}
		}
	}
}

func TestPendingDiagnosticPrefersItsImplementationLimitation(t *testing.T) {
	files := fstest.MapFS{
		"feature.dart": {Data: []byte("loadState();")},
		"limit.dart":   {Data: []byte("loopbackOnly();")},
		"roadmap.md":   {Data: []byte("### TASK-001 - Future capability\n")},
		"guide.md":     {Data: []byte(statusBegin + "\n| Claim | Status | Scope |\n| --- | --- | --- |\n| capability | shipped | Incorrect. |\n" + statusEnd)},
	}
	claim := statusClaim{id: "capability", evidence: []statusEvidence{
		{path: "feature.dart", require: "loadState()"},
		{path: "limit.dart", require: "loopbackOnly()", limitation: "loopback access only"},
		{path: "roadmap.md", pattern: `TASK-001`, limitation: "remaining task"},
	}}
	diagnostic := strings.Join(checkStatusDocument(files, "guide.md", []statusClaim{claim}), "\n")
	if !strings.Contains(diagnostic, "see limit.dart") || strings.Contains(diagnostic, "see feature.dart") || strings.Contains(diagnostic, "see roadmap.md") {
		t.Fatalf("mismatch must name its implementation limitation: %s", diagnostic)
	}
}

func TestPendingRequiresAnImplementationLimitation(t *testing.T) {
	files := fstest.MapFS{
		"source.dart": {Data: []byte("loadState();")},
		"roadmap.md":  {Data: []byte("### TASK-001 - Future capability\n")},
	}
	claim := statusClaim{id: "capability", evidence: []statusEvidence{
		{path: "source.dart", require: "loadState()"},
		{path: "roadmap.md", pattern: `TASK-001`, limitation: "remaining task"},
	}}
	result := evaluateStatusEvidence(files, []statusClaim{claim})[claim.id]
	if result.state != "" || !strings.Contains(strings.Join(result.diagnostics, "\n"), "concrete implementation limitation") {
		t.Fatalf("roadmap-only pending status must fail: %+v", result)
	}
}

func TestMarkdownCannotJustifyShippedStatus(t *testing.T) {
	for _, name := range []string{"plan.md", "plan.txt", "plan.markdown", "plan.adoc", "plan.rst"} {
		t.Run(name, func(t *testing.T) {
			files := fstest.MapFS{name: {Data: []byte("### Feature shipped\n")}}
			claim := statusClaim{id: "capability", evidence: []statusEvidence{{path: name, pattern: "Feature shipped"}}}
			result := evaluateStatusEvidence(files, []statusClaim{claim})[claim.id]
			if result.state != "" || !strings.Contains(strings.Join(result.diagnostics, "\n"), "documentation context cannot justify shipped") {
				t.Fatalf("prose-only shipped status must fail: %+v", result)
			}
		})
	}
}

func TestAbsenceAloneCannotJustifyShippedStatus(t *testing.T) {
	files := fstest.MapFS{"source.go": {Data: []byte("package fixture\n")}}
	claim := statusClaim{id: "capability", evidence: []statusEvidence{{path: "source.go", absent: "Placeholder"}}}
	result := evaluateStatusEvidence(files, []statusClaim{claim})[claim.id]
	if result.state != "" || !strings.Contains(strings.Join(result.diagnostics, "\n"), "invalid guard specification") {
		t.Fatalf("absence-only shipped status must fail: %+v", result)
	}
}

func TestMissingEvidenceEvaluationFailsClosed(t *testing.T) {
	files := fstest.MapFS{"guide.md": {Data: []byte(statusBegin + "\n| Claim | Status | Scope |\n| --- | --- | --- |\n| capability | shipped | Has behavior. |\n" + statusEnd)}}
	claim := statusClaim{id: "capability", evidence: []statusEvidence{{path: "source.go", require: "Behavior"}}}
	diagnostic := strings.Join(checkStatusTable(files, "guide.md", []statusClaim{claim}, map[string]statusResult{}), "\n")
	if !strings.Contains(diagnostic, "missing evidence evaluation") {
		t.Fatalf("missing evaluation must not pass: %s", diagnostic)
	}
}

func TestStatusGuardDiagnosticsDoNotEchoUnknownContent(t *testing.T) {
	const sentinel = "sensitive-fixture-value"
	files := fstest.MapFS{
		"guide.md": {Data: []byte(statusBegin + "\n| " + sentinel + " | shipped | " + sentinel + " |\n| search | " + sentinel + " | scope |\n" + statusEnd)},
		"bad.go":   {Data: []byte("package " + sentinel)},
	}
	claims := []statusClaim{{id: "search", evidence: []statusEvidence{{path: "bad.go", require: "Search"}}}}
	got := strings.Join(checkStatusDocument(files, "guide.md", claims), "\n")
	if got == "" || strings.Contains(got, sentinel) {
		t.Fatalf("diagnostics must be nonempty and content-free: %s", got)
	}
}

// The real claim set is also exercised, so a correct generic parser cannot
// mask a disconnected or accidentally omitted repository witness.
func TestStatusGuardRepositoryFixtures(t *testing.T) {
	baseline := fstest.MapFS{}
	repo := os.DirFS("../..")
	load := func(path string) {
		t.Helper()
		data, err := fs.ReadFile(repo, path)
		if err != nil {
			t.Fatalf("load fixture %s: %v", path, err)
		}
		baseline[path] = &fstest.MapFile{Data: data}
	}
	for _, claim := range featureStatusClaims() {
		for _, evidence := range claim.evidence {
			if _, loaded := baseline[evidence.path]; !loaded {
				load(evidence.path)
			}
		}
	}
	results := evaluateStatusEvidence(baseline, featureStatusClaims())
	for id, result := range results {
		if len(result.diagnostics) != 0 {
			t.Fatalf("baseline %s evidence invalid: %v", id, result.diagnostics)
		}
	}
	for _, document := range []string{"docs/NORTH_STAR.md", "turing-client/turing_app/README.md"} {
		load(document)
		claims := claimsForDocument(document)
		if diagnostics := checkStatusTable(baseline, document, claims, results); len(diagnostics) != 0 {
			t.Fatalf("baseline fixture invalid: %s", strings.Join(diagnostics, "\n"))
		}
		for _, claim := range claims {
			t.Run(document+"/"+claim.id+"/false-status", func(t *testing.T) {
				files := maps.Clone(baseline)
				falseStatus := "shipped"
				if results[claim.id].state == "shipped" {
					falseStatus = "pending"
				}
				if err := replaceFixture(document, "| "+claim.id+" | "+results[claim.id].state+" |", "| "+claim.id+" | "+falseStatus+" |")(files); err != nil {
					t.Fatal(err)
				}
				diagnostic := strings.Join(checkStatusTable(files, document, claims, results), "\n")
				if !strings.Contains(diagnostic, claim.id+": status mismatch") {
					t.Fatalf("false status not detected: %s", diagnostic)
				}
				if results[claim.id].state == "pending" && strings.HasSuffix(results[claim.id].witness, ".md") {
					t.Fatalf("pending diagnostic must name implementation evidence, not the roadmap: %s", diagnostic)
				}
			})
		}
	}
	const app = "turing-client/turing_app/"
	const backend = "turing-backend/"
	const runtime = backend + "agent-runtime-go/internal/"
	const orchestrator = backend + "orchestrator-go/internal/"
	for _, test := range []struct{ name, path, old, replacement, claim string }{
		{"search RPC disconnected", app + "lib/networking/grpc_client.dart", "await _sessions.searchMessages(", "await _sessions.listMessages(", "flutter-search"},
		{"workspace page replaced", app + "lib/ui/shell/responsive_shell.dart", "return IntegrationsPage(", "return PlaceholderPage(", "flutter-workspace"},
		{"breaking check commented", ".github/workflows/ci.yml", "run: tools/proto/breaking.sh", "# run: tools/proto/breaking.sh", "proto-breaking"},
		{"breaking step disabled", ".github/workflows/ci.yml", "- name: Check protobuf compatibility", "- name: Check protobuf compatibility\n        if: false", "proto-breaking"},
		{"PR trigger removed", ".github/workflows/ci.yml", "pull_request:", "workflow_dispatch:", "proto-breaking"},
		{"codegen failure ignored", ".github/workflows/ci.yml", "- name: Check generated protobuf output", "- name: Check generated protobuf output\n        continue-on-error: true", "proto-codegen"},
		{"proto job disabled", ".github/workflows/ci.yml", "  proto-and-scripts:", "  proto-and-scripts:\n    if: false", "proto-breaking"},
		{"Buf policy commented", "buf.yaml", "use:\n    - FILE", "# use:\n    - FILE", "proto-breaking"},
		{"registry persistence disconnected", orchestrator + "service/mcpregistry/service.go", "s.repo.RegisterMCPServer(", "s.repo.ListMCPServers(", "mcp-registry"},
		{"runtime lifecycle added", runtime + "mcp/client.go", `params := map[string]any{}`, `params := map[string]any{"method": "initialize"}`, "mcp-lifecycle"},
		{"routing provider changed", runtime + "agent/external_agent.go", "llm.NewOpenAICompatibleWithLimits(", "llm.NewNativeProvider(", "remote-model-routing"},
		{"routing RPC registration removed", orchestrator + "app/app.go", "turingv1.RegisterExternalAgentServiceServer(publicServer, agentService)", "", "remote-model-routing"},
		{"GitHub consumer removed", orchestrator + "service/integrations/github.go", `case "github.get_file":`, `case "github.no_file":`, "github-tools"},
		{"GitHub egress validation removed", orchestrator + "service/integrations/call.go", "s.validateIntegrationDecision(", "s.skippedIntegrationDecision(", "github-tools"},
		{"non-GitHub tool added", orchestrator + "service/integrations/tools.go", "var githubTools = []integrationTool{", "var githubTools = []integrationTool{\n{name: \"imap.read\"},", "other-integration-tools"},
		{"second integration consumer table", orchestrator + "service/integrations/tools.go", "for _, tool := range githubTools {", "for _, extra := range imapTools { if extra.name == name { return extra, true } }\nfor _, tool := range githubTools {", "other-integration-tools"},
		{"credential-only acceptance changed", orchestrator + "service/integrations/providers.go", "supported: true,", "supported: false,", "other-integration-tools"},
		{"unsupported provider marked supported", orchestrator + "service/integrations/providers.go", "supported: false,", "supported: true,", "other-integration-tools"},
		{"release network permission added", app + "android/app/src/main/AndroidManifest.xml", "<application", `<uses-permission android:name="android.permission.INTERNET"/><application`, "mobile-client"},
		{"loopback bind widened", backend + "infra/docker-compose.yml", `"127.0.0.1:`, `"0.0.0.0:`, "mobile-reachability"},
		{"task marker removed", "docs/NORTH_STAR.md", "A2A-001 - Outbound", "A2A-099 - Outbound", "agent-delegation"},
		{"A2A service added", orchestrator + "app/app.go", "turingv1.RegisterSessionServiceServer(publicServer, sessionService)", "turingv1.RegisterA2AServiceServer(publicServer, peerService)\nturingv1.RegisterSessionServiceServer(publicServer, sessionService)", "agent-delegation"},
		{"A2A bridge added", orchestrator + "app/app.go", "turingv1.RegisterSessionServiceServer(publicServer, sessionService)", "NewA2ABridge(cfg)\nturingv1.RegisterSessionServiceServer(publicServer, sessionService)", "agent-delegation"},
		{"agent-to-agent adapter added", orchestrator + "app/app.go", "turingv1.RegisterSessionServiceServer(publicServer, sessionService)", "agentToAgent.NewDelegator()\nturingv1.RegisterSessionServiceServer(publicServer, sessionService)", "agent-delegation"},
		{"placeholder destination added", app + "lib/ui/shell/shell_destination.dart", "enum ShellDestination {", "enum ShellDestination {\n devices(implemented: false),", "flutter-workspace"},
	} {
		if _, loaded := baseline[test.path]; !loaded {
			load(test.path)
		}
		t.Run(test.name, func(t *testing.T) {
			files := maps.Clone(baseline)
			if err := replaceFixture(test.path, test.old, test.replacement)(files); err != nil {
				t.Fatal(err)
			}
			// Exercise the mutated claim through the real evaluator. Unrelated
			// claims were validated on the baseline and are not this fixture's
			// subject; re-parsing their large Go files adds no coverage.
			mutatedResults := maps.Clone(results)
			for _, claim := range featureStatusClaims() {
				if claim.id == test.claim {
					mutatedResults[claim.id] = evaluateStatusEvidence(files, []statusClaim{claim})[claim.id]
				}
			}
			diagnostics := strings.Join(append(
				checkStatusTable(files, "docs/NORTH_STAR.md", featureStatusClaims(), mutatedResults),
				mutatedResults[test.claim].diagnostics...,
			), "\n")
			if !regexp.MustCompile(regexp.QuoteMeta(test.claim) + `: evidence [0-9]+ in ` + regexp.QuoteMeta(test.path) + `[: (]`).MatchString(diagnostics) {
				t.Fatalf("evidence drift not diagnosed: %s", diagnostics)
			}
		})
	}
	for _, claim := range featureStatusClaims() {
		for index, evidence := range claim.evidence {
			t.Run(fmt.Sprintf("%s/missing-witness-%d", claim.id, index+1), func(t *testing.T) {
				files := maps.Clone(baseline)
				delete(files, evidence.path)
				probe := statusClaim{id: claim.id, evidence: []statusEvidence{evidence}}
				result := evaluateStatusEvidence(files, []statusClaim{probe})[claim.id]
				want := fmt.Sprintf("%s: evidence 1 in %s", claim.id, evidence.path)
				if result.state != "" || !strings.Contains(strings.Join(result.diagnostics, "\n"), want) {
					t.Fatalf("missing witness must fail closed: %+v", result)
				}
			})
		}
	}
}
