package tests

import (
	"slices"
	"strings"
	"testing"
)

// The memory vault is the most personal state this repository can accidentally
// publish: persona.md and profile.md are prose about the user, beliefs/ is what
// Turing has been told about their life, and inbox/ holds proposals that were
// never even accepted. This repository is public, so every path under
// turing-backend/memory must be ignored while the empty directory itself stays
// tracked, exactly as the skills library is.
func TestRuntimeMemoryFilesAreIgnoredWithoutHidingSentinel(t *testing.T) {
	for _, path := range []string{
		"turing-backend/memory/persona.md",
		"turing-backend/memory/profile.md",
		"turing-backend/memory/inbox/01J-note.md",
		"turing-backend/memory/beliefs/people/someone.md",
		"turing-backend/memory/.obsidian/workspace.json",
	} {
		if !gitIgnores(t, path) {
			t.Errorf("%q is not ignored; personal memory content could be committed", path)
		}
	}

	const sentinel = "turing-backend/memory/.gitkeep"
	if gitIgnores(t, sentinel) {
		t.Errorf("%q is ignored; the empty vault directory cannot remain tracked", sentinel)
	}
	if !slices.Contains(gitTrackedPaths(t, repoRootFromTests), sentinel) {
		t.Errorf("%q is not tracked", sentinel)
	}
}

// The absence claim the ignore rules exist to protect: nothing under the vault
// is committed today except the sentinel.
func TestNoMemoryVaultContentIsTracked(t *testing.T) {
	paths := gitTrackedPaths(t, repoRootFromTests)
	if !slices.Contains(paths, "go.mod") {
		t.Fatalf("tracked-path scan returned %d paths and no go.mod; it did not see this repository", len(paths))
	}
	var tracked []string
	for _, path := range paths {
		if strings.HasPrefix(path, "turing-backend/memory/") && path != "turing-backend/memory/.gitkeep" {
			tracked = append(tracked, path)
		}
	}
	if len(tracked) > 0 {
		t.Errorf("memory vault content is tracked in this public repository: %v", tracked)
	}
}
