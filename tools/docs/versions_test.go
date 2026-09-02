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
	"go/version"
	"os"
	"regexp"
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
