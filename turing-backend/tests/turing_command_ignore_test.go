package tests

import "testing"

// The turing wrapper compiles the command into turing-backend/.turing/bin and
// `turing config` locks turing-backend/.env.lock. Neither is source, and
// neither belongs in a commit or in the image build context.
func TestTuringCommandBuildOutputAndLockAreIgnored(t *testing.T) {
	for _, path := range []string{
		"turing-backend/.turing/bin/turing",
		"turing-backend/.env.lock",
	} {
		if !gitIgnores(t, path) {
			t.Errorf("%s is not ignored by git", path)
		}
	}
	lines := ignoreFileLines(t, ".dockerignore")
	for _, pattern := range []string{"turing-backend/.turing", "turing-backend/.env.lock"} {
		if !lines[pattern] {
			t.Errorf(".dockerignore missing %q", pattern)
		}
	}
}
