package tests

import (
	"slices"
	"testing"
)

func TestRuntimeSkillFilesAreIgnoredWithoutHidingSentinel(t *testing.T) {
	for _, path := range []string{
		"turing-backend/skills/private/SKILL.md",
		"turing-backend/skills/private/references/account.md",
		"turing-backend/skills/.private",
	} {
		if !gitIgnores(t, path) {
			t.Errorf("%q is not ignored; private runtime skill content could be committed", path)
		}
	}

	const sentinel = "turing-backend/skills/.gitkeep"
	if gitIgnores(t, sentinel) {
		t.Errorf("%q is ignored; the empty runtime skills directory cannot remain tracked", sentinel)
	}
	if !slices.Contains(gitTrackedPaths(t, repoRootFromTests), sentinel) {
		t.Errorf("%q is not tracked", sentinel)
	}
}
