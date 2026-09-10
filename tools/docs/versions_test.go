// Package docs guards the version claims in CLAUDE.md, README.md and
// tools/proto/README.md against the files that actually enforce them.
//
// The toolchain table in CLAUDE.md went stale twice in two days: a Dependabot
// PR raised a module's Go directive, and a later one repinned every CI job and
// raised the root module too. Both times the docs were corrected only because
// someone happened to re-read them. Nothing failed.
//
// These tests close that loop the same way .github/workflows/ci_test.go closes
// it for the workflow: the version lives in exactly one enforcing file, this
// test reads it from there, and the documentation has to agree. A bump now
// fails a test with a message naming the file to edit, instead of leaving prose
// that quietly describes a toolchain nobody runs any more.
//
// Direction matters. These assertions never check that a version is "correct" —
// only that the docs and the enforcer say the same thing. Changing a pin is
// always legitimate; changing it without updating the docs is what this catches.
//
// They also check *attribution*, not mere presence. An earlier draft asked only
// whether the version string appeared somewhere in the document, which is much
// weaker than it looks: "1.23" is satisfied by the unrelated "1.23-bookworm"
// container tag, so the module-split prose could have been deleted wholesale
// and this file would still have passed. Every check below requires the version
// to appear near the subject it describes.
package docs

import (
	"go/parser"
	"go/token"
	"go/version"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// repoFile reads a path relative to the repository root.
func repoFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile("../../" + path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// extract pulls the single capture group of pattern out of source, failing if
// the pattern does not match exactly once. An enforcer that stops matching is
// itself a signal: the pin moved somewhere this test no longer watches.
func extract(t *testing.T, source, sourceName, pattern string) string {
	t.Helper()
	matches := regexp.MustCompile(pattern).FindAllStringSubmatch(source, -1)
	if len(matches) != 1 {
		t.Fatalf("%s: pattern %q matched %d times, want exactly 1 — the pin moved or changed shape, so this guard needs updating too",
			sourceName, pattern, len(matches))
	}
	return matches[0][1]
}

// attributes reports whether doc states value as a property of subject, by
// requiring both to appear on the same line.
//
// Same-line is deliberately stricter than a character window. These documents
// put one claim per line — a table row, or an unwrapped sentence — and a window
// wide enough to span a row is also wide enough to span the *neighbouring*
// claim: with a 110-character window, the single row "1.27-alpine for both MCP
// images, 1.23-bookworm for orchestrator & agent-runtime" satisfied
// "orchestrator is 1.27-alpine", so swapping which service ran which image
// passed. Line scoping costs nothing here and closes that.
func attributes(doc, subject, value string) bool {
	for _, line := range strings.Split(doc, "\n") {
		if strings.Contains(line, value) && strings.Contains(line, subject) {
			return true
		}
	}
	return false
}

// TestDocumentedToolVersionsMatchTheirEnforcers checks every version the
// CLAUDE.md toolchain table names against the file that fails when it is wrong.
//
// tools/proto/README.md is checked alongside it: CLAUDE.md calls that file "the
// install guide", and a stale install guide misleads more directly than a stale
// reference table.
func TestDocumentedToolVersionsMatchTheirEnforcers(t *testing.T) {
	claude := repoFile(t, "CLAUDE.md")
	protoReadme := repoFile(t, "tools/proto/README.md")
	ci := repoFile(t, ".github/workflows/ci.yml")
	generate := repoFile(t, "tools/proto/generate.sh")
	breaking := repoFile(t, "tools/proto/breaking.sh")
	rootMod := repoFile(t, "go.mod")

	for _, check := range []struct {
		tool     string
		subject  string
		enforcer string
		source   string
		pattern  string
		// inProtoReadme is false for tools that file has no reason to name.
		inProtoReadme bool
	}{
		{
			tool:     "golangci-lint",
			subject:  "golangci-lint",
			enforcer: ".github/workflows/ci.yml",
			source:   ci,
			pattern:  `golangci-lint/v2/cmd/golangci-lint@(v[0-9]+\.[0-9]+\.[0-9]+)`,
		},
		{
			tool:          "buf",
			subject:       "uf", // matches both "buf" and "Buf"
			enforcer:      "tools/proto/breaking.sh",
			source:        breaking,
			pattern:       `REQUIRED_BUF_VERSION="([0-9]+\.[0-9]+\.[0-9]+)"`,
			inProtoReadme: true,
		},
		{
			tool:    "protoc",
			subject: "protoc",
			// The closing quote bounds the capture, so a future "34.10" cannot
			// satisfy a guard written when the pin was "34.1".
			enforcer:      "tools/proto/generate.sh",
			source:        generate,
			pattern:       `"libprotoc ([0-9]+\.[0-9]+)"`,
			inProtoReadme: true,
		},
		{
			tool:          "protoc-gen-go",
			subject:       "protoc-gen-go",
			enforcer:      "tools/proto/generate.sh",
			source:        generate,
			pattern:       `"protoc-gen-go (v[0-9]+\.[0-9]+\.[0-9]+)"`,
			inProtoReadme: true,
		},
		{
			tool:          "protoc-gen-go-grpc",
			subject:       "protoc-gen-go-grpc",
			enforcer:      "tools/proto/generate.sh",
			source:        generate,
			pattern:       `"protoc-gen-go-grpc ([0-9]+\.[0-9]+\.[0-9]+)"`,
			inProtoReadme: true,
		},
		{
			// The conformance fixtures' peer implementation. Dependabot's single
			// gomod entry covers all three module directories and will bump it,
			// so the documented pin has to be read out of a go.mod rather than
			// trusted: an unguarded version row is exactly the drift this table
			// claims to prevent.
			tool:     "MCP Go SDK",
			subject:  "MCP Go SDK",
			enforcer: "go.mod",
			source:   rootMod,
			pattern:  `github\.com/modelcontextprotocol/go-sdk (v[0-9]+\.[0-9]+\.[0-9]+)`,
		},
		{
			tool:    "Dart protoc_plugin",
			subject: "protoc_plugin",
			// Anchored on the equality check rather than the two install
			// messages that also carry the number, so this matches once.
			enforcer:      "tools/proto/generate.sh",
			source:        generate,
			pattern:       `dart_plugin_version" != "([0-9]+\.[0-9]+\.[0-9]+)"`,
			inProtoReadme: true,
		},
	} {
		t.Run(check.tool, func(t *testing.T) {
			want := extract(t, check.source, check.enforcer, check.pattern)
			if !attributes(claude, check.subject, want) {
				t.Errorf("CLAUDE.md does not state %s %s (which %s enforces) — update the toolchain table",
					check.tool, want, check.enforcer)
			}
			if check.inProtoReadme && !attributes(protoReadme, check.subject, want) {
				t.Errorf("tools/proto/README.md does not state %s %s (which %s enforces) — the install guide is stale",
					check.tool, want, check.enforcer)
			}
		})
	}
}

// TestTheConformanceSDKIsPinnedIdenticallyInEveryModule keeps the single
// documented version row honest: it is one claim about three go.mod files, and
// Dependabot updates them independently.
func TestTheConformanceSDKIsPinnedIdenticallyInEveryModule(t *testing.T) {
	pattern := regexp.MustCompile(`github\.com/modelcontextprotocol/go-sdk (v[0-9]+\.[0-9]+\.[0-9]+)`)
	pinned := map[string]string{}
	for _, module := range []string{"go.mod", "turing-backend/mcp-files/go.mod", "turing-backend/mcp-system/go.mod"} {
		matches := pattern.FindStringSubmatch(repoFile(t, module))
		if matches == nil {
			t.Fatalf("%s does not require the MCP Go SDK; the conformance fixtures need it in every module", module)
		}
		pinned[module] = matches[1]
	}
	for module, version := range pinned {
		if version != pinned["go.mod"] {
			t.Errorf("%s pins MCP Go SDK %s but the root module pins %s; the documented row names one version",
				module, version, pinned["go.mod"])
		}
	}
}

// goDirective matches a module's declared language version.
var goDirective = regexp.MustCompile(`(?m)^go ([0-9]+\.[0-9]+(?:\.[0-9]+)?)$`)

// TestDocumentedGoFloorMatchesTheModules checks the Go version story in both
// documents against the three go.mod files.
//
// This is the claim that went stale twice, and it is the one a reader is most
// likely to act on: it decides which toolchain they install before anything
// else works.
func TestDocumentedGoFloorMatchesTheModules(t *testing.T) {
	claude := repoFile(t, "CLAUDE.md")
	readme := repoFile(t, "README.md")

	highest := "go0.0"
	for _, module := range []struct{ name, subject, path string }{
		{"root", "root", "go.mod"},
		{"mcp-files", "mcp-files", "turing-backend/mcp-files/go.mod"},
		{"mcp-system", "mcp-system", "turing-backend/mcp-system/go.mod"},
	} {
		matches := goDirective.FindStringSubmatch(repoFile(t, module.path))
		if matches == nil {
			t.Fatalf("%s: no go directive found", module.path)
		}
		declared := matches[1]
		// Attribution, not presence: a bare "1.23" is satisfied by the
		// "1.23-bookworm" container tag several rows away.
		if !attributes(claude, module.subject, declared) {
			t.Errorf("CLAUDE.md does not attribute Go %s to %s (declared in %s) — the module split is documented there",
				declared, module.name, module.path)
		}
		if version.Compare("go"+declared, highest) > 0 {
			highest = "go" + declared
		}
	}

	// Both documents state an install floor. It must be the highest directive
	// any module declares: a floor below one of them cannot build the repo.
	// Compared with go/version rather than string ordering, which would rank
	// "1.9" above "1.10".
	floor := strings.TrimPrefix(version.Lang(highest), "go")
	// The floor is written two ways on purpose — a table wants "Go 1.25+",
	// running prose wants "Go 1.25 or newer" — so accept either rather than
	// reddening the build over a legitimate rewording.
	phrasing := regexp.MustCompile(`Go ` + regexp.QuoteMeta(floor) + `(\+| or newer)`)
	for _, doc := range []struct{ name, body string }{{"CLAUDE.md", claude}, {"README.md", readme}} {
		if !phrasing.MatchString(doc.body) {
			t.Errorf("%s does not state a %q install floor, which is the highest module directive (%s)",
				doc.name, "Go "+floor, strings.TrimPrefix(highest, "go"))
		}
	}
}

// TestDocumentedContainerImagesMatchTheDockerfiles guards the row most likely
// to rot next: Dependabot has open PRs moving two of these four images, and
// nothing else would notice the table falling behind.
//
// Each tag is checked against the service it belongs to, not merely against the
// document as a whole. The four Dockerfiles carry only two distinct Go tags, so
// a presence-only check would still pass if the row swapped which service ran
// which image.
func TestDocumentedContainerImagesMatchTheDockerfiles(t *testing.T) {
	claude := repoFile(t, "CLAUDE.md")
	base := regexp.MustCompile(`(?m)^FROM golang:([^\s]+)`)

	for _, image := range []struct{ path, subject string }{
		{"turing-backend/mcp-files/Dockerfile", "MCP"},
		{"turing-backend/mcp-system/Dockerfile", "MCP"},
		{"turing-backend/orchestrator-go/Dockerfile", "orchestrator"},
		{"turing-backend/agent-runtime-go/Dockerfile", "agent-runtime"},
	} {
		matches := base.FindStringSubmatch(repoFile(t, image.path))
		if matches == nil {
			t.Fatalf("%s: no golang base image found", image.path)
		}
		if !attributes(claude, image.subject, matches[1]) {
			t.Errorf("CLAUDE.md does not attribute the %q base image to %s (%s) — update the container row",
				matches[1], image.subject, image.path)
		}
	}
}

// TestDocumentedDartFloorMatchesThePubspec guards the one requirement the docs
// carried unversioned the longest.
func TestDocumentedDartFloorMatchesThePubspec(t *testing.T) {
	pubspec := repoFile(t, "turing-client/turing_app/pubspec.yaml")
	constraint := extract(t, pubspec, "pubspec.yaml", `(?m)^\s+sdk:\s*(\^[0-9]+\.[0-9]+\.[0-9]+)$`)

	if claude := repoFile(t, "CLAUDE.md"); !attributes(claude, "Dart", constraint) {
		t.Errorf("CLAUDE.md does not attribute the Dart SDK constraint %q to Dart (from pubspec.yaml)", constraint)
	}
	// README states it as a bare version rather than a caret constraint, since
	// it is prose for a human installing Flutter rather than a table entry.
	bare := strings.TrimPrefix(constraint, "^")
	if readme := repoFile(t, "README.md"); !attributes(readme, "Dart", bare) {
		t.Errorf("README.md does not attribute the Dart SDK floor %q to Dart (from pubspec.yaml)", bare)
	}
}

// TestTheDocumentedCollectionBudgetMatchesTheConstant reads the collection
// budget out of the file that enforces it and fails when the integration guide
// disagrees.
//
// This exists because the number has already drifted twice: the guide's own
// tool-surface entries for files.list and files.search kept saying 384 KiB
// after the constant moved to 320 KiB, and an integrator sizing expectations
// for `truncated` from those entries would have been 64 KiB out. The budget is
// part of each tool's documented contract, so a bump has to move the prose with
// it — the same loop versions_test.go already closes for pinned tool versions.
func TestTheDocumentedCollectionBudgetMatchesTheConstant(t *testing.T) {
	source := repoFile(t, "turing-backend/mcp-files/internal/tools/files.go")
	matches := regexp.MustCompile(`MaxCollectionResultJSONBytes = (\d+) \* 1024`).FindStringSubmatch(source)
	if matches == nil {
		t.Fatal("files.go does not declare MaxCollectionResultJSONBytes as a KiB multiple; this guard reads it from there")
	}
	documented := matches[1] + " KiB"

	guide := repoFile(t, "docs/mcp-security-and-integration.md")
	for _, claim := range []string{
		"Encoded collection data is capped at " + documented,
		"Matches and error details share a " + documented + " encoded collection budget",
	} {
		if !strings.Contains(guide, claim) {
			t.Errorf("docs/mcp-security-and-integration.md does not state %q; the collection budget moved without the tool surface following", claim)
		}
	}
	if !strings.Contains(guide, "`MaxCollectionResultJSONBytes`, "+documented+")") {
		t.Errorf("the budget table does not name %s for MaxCollectionResultJSONBytes", documented)
	}
}

// TestEveryModuleNamesTheSameMCPRevision is the only place that can hold the
// revision invariant, because it is the only one that can read all three files.
//
// mcpwire declares the revision, mcp-files aliases it, and mcp-system keeps its
// own literal because it cannot import mcpwire: its *shipped binary* imports
// only the standard library, guarded by
// TestTheShippedBinaryImportsOnlyTheStandardLibrary and
// TestTheShippedBinaryBuildsWithNoModuleProxy. The module itself is not
// dependency-free — it carries one pinned test-only requirement, the MCP SDK the
// conformance fixtures run against. Nothing inside mcp-system can compare its copy against
// the others, and for a while its own test claimed to while comparing a literal
// to a literal in the same file. A maintainer bumping the revision would have
// edited mcp-system, watched that module pass, and learned about the real
// coupling from an unrelated-looking failure somewhere else.
func TestEveryModuleNamesTheSameMCPRevision(t *testing.T) {
	declared := regexp.MustCompile(`ProtocolVersion\s+=\s+"([0-9]{4}-[0-9]{2}-[0-9]{2})"`).
		FindStringSubmatch(repoFile(t, "turing-backend/mcpwire/mcpwire.go"))
	if declared == nil {
		t.Fatal("mcpwire does not declare ProtocolVersion as a dated revision; this guard reads it from there")
	}
	revision := declared[1]

	// mcp-files takes it from the compiler, so the witness is the alias itself.
	files := repoFile(t, "turing-backend/mcp-files/cmd/server/lifecycle.go")
	if !strings.Contains(files, "supportedProtocolVersion = mcpwire.ProtocolVersion") {
		t.Error("mcp-files no longer aliases mcpwire.ProtocolVersion; if it now keeps its own literal, this guard must compare it")
	}

	// mcp-system cannot, so its literal is compared here.
	system := regexp.MustCompile(`supportedProtocolVersion = "([0-9]{4}-[0-9]{2}-[0-9]{2})"`).
		FindStringSubmatch(repoFile(t, "turing-backend/mcp-system/cmd/server/lifecycle.go"))
	if system == nil {
		t.Fatal("mcp-system does not declare supportedProtocolVersion as a dated revision")
	}
	if system[1] != revision {
		t.Errorf("mcp-system speaks MCP %s but mcpwire declares %s; the bundled servers must not diverge", system[1], revision)
	}
}

// TestTheConformanceSDKNeverEntersAShippedBinary enforces the property the
// integration guide claims, across every module that requires the SDK.
//
// mcp-system asserts it for itself, because its whole shipped binary is
// stdlib-only. But the root module and mcp-files now carry the SDK as a direct
// requirement too, and nothing stopped a production import there: one
// `import "github.com/modelcontextprotocol/go-sdk/mcp"` in orchestrator-go or
// agent-runtime-go would link an MCP protocol framework into a shipped binary,
// falsify the documented claim, and cut against CON-001's requirement to add no
// unnecessary protocol framework. Every other version and behaviour claim in
// this change is closed by a guard; this one was not.
func TestTheConformanceSDKNeverEntersAShippedBinary(t *testing.T) {
	const sdk = "github.com/modelcontextprotocol/go-sdk"
	// The whole backend tree, not a hand-written list of packages. Enumerating
	// roots is how this guard first shipped, and it silently omitted
	// turing-backend/internal — a root-module package both shipped binaries
	// import — so an SDK import there would have linked the framework into the
	// orchestrator and the agent runtime without failing anything. Walking the
	// tree cannot miss a directory nobody remembered.
	// gen/ is generated protobuf output, but it compiles into both shipped
	// binaries just the same, so it is walked rather than trusted.
	roots := []string{"turing-backend", "gen"}

	fileSet := token.NewFileSet()
	inspected := 0
	for _, root := range roots {
		err := filepath.WalkDir("../../"+root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// The runtime-writable trees are tracked (each keeps a .gitkeep) but
			// their contents are the agent's sandbox, the user's skills and the
			// vault — never anything that links into a binary. Parsing them would
			// let a file the agent itself wrote fail this guard, or fail the whole
			// root suite on a syntax error that has nothing to do with the SDK.
			//
			// Excluding is safe in the direction enumerating roots was not:
			// forgetting an exclusion here only adds noise, while nothing shipped
			// can hide behind a missing one.
			if entry.IsDir() && runtimeDataDirectory(path) {
				return fs.SkipDir
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			parsed, parseErr := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
			if parseErr != nil {
				return parseErr
			}
			inspected++
			for _, imported := range parsed.Imports {
				line, quoteErr := strconv.Unquote(imported.Path.Value)
				if quoteErr != nil {
					return quoteErr
				}
				if strings.HasPrefix(line, sdk) {
					t.Errorf(
						"%s imports %s in non-test code; the SDK is conformance evidence, not a shipped dependency",
						path, line,
					)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if inspected == 0 {
		t.Fatal("walked no non-test Go sources; this guard reads them from the backend modules")
	}
}

// runtimeDataDirectory reports whether path is one of the backend's
// runtime-writable roots, whose contents are gitignored and never compiled.
func runtimeDataDirectory(path string) bool {
	for _, data := range []string{"sandbox", "data", "skills", "memory", "mcp"} {
		if path == filepath.Join("..", "..", "turing-backend", data) {
			return true
		}
	}
	return false
}
