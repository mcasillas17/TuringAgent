package tests

import "testing"

// init.sh writes data/team-seeded on the first install. Committed, it would
// stop every fresh clone from ever being seeded; the profiles themselves are
// the user's and stay out of the repository too.
func TestGitIgnoresTheTeamSeedReceiptAndProfiles(t *testing.T) {
	for _, path := range []string{
		"turing-backend/data/team-seeded",
		"turing-backend/team/dev/AGENT.md",
	} {
		if !gitIgnores(t, path) {
			t.Errorf("%s is not ignored", path)
		}
	}
	if gitIgnores(t, "turing-backend/team/.gitkeep") {
		t.Error("turing-backend/team/.gitkeep is ignored; the empty directory must stay tracked")
	}
}
