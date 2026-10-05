package team

import (
	"context"
	"slices"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// A child runs only on a worker that honors the specialist-job contract, so
// its tools are those common to the team-protocol workers on its route.
func TestTheChildRouteRequiresTheTeamProtocol(t *testing.T) {
	h := newHarness(t)
	h.write(t, "research", researchFrontmatter)
	h.profile(t, "research")

	if len(h.routes.routes) == 0 || len(h.routes.validated) == 0 {
		t.Fatal("the child route was never consulted")
	}
	for _, route := range slices.Concat(h.routes.routes, h.routes.validated) {
		if route.MinimumTeamProtocolVersion != 1 || route.AgentID != "general_assistant" || route.ModelProvider != "ollama" {
			t.Fatalf("route = %+v, want the child route at team protocol 1", route)
		}
	}
}

// The roster is the active team, ordered by ID, each specialist with the
// tools its child would receive. Anything not active is left out.
func TestTheRosterListsOnlyActiveProfiles(t *testing.T) {
	h := newHarness(t)
	h.write(t, "research", researchFrontmatter)
	h.activate(t, "research")
	h.write(t, "dev", "name: Dev\nemoji: \"💻\"\ndescription: Works on code\nversion: 1\ntools: [files.*]\nmemory: none\n")
	h.activate(t, "dev")
	h.write(t, "inbox", "name: Inbox\ndescription: Mail\nversion: 1\ntools: [files.read]\nrequires: [gmail.*]\nmemory: none\n")
	h.activate(t, "inbox")
	h.write(t, "draft", "name: Draft\ndescription: Not granted\nversion: 1\ntools: [files.read]\nmemory: none\n")
	h.write(t, "finance", "name: Finance\ndescription: Disabled\nversion: 1\ntools: [files.read]\nmemory: none\n")
	h.activate(t, "finance")
	if _, err := h.repo.SetAgentProfileEnabled(context.Background(), "finance", false); err != nil {
		t.Fatal(err)
	}

	roster, err := h.server.Roster(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []repository.TeamRosterEntry{
		{
			ProfileID: "dev", Revision: h.profile(t, "dev").GetRevision(), Name: "Dev", Emoji: "💻",
			Description: "Works on code", Tools: []string{"files/files.read", "files/files.write"},
		},
		{
			ProfileID: "research", Revision: h.profile(t, "research").GetRevision(), Name: "Research", Emoji: "🔬",
			Description: "Looks things up", Tools: h.profile(t, "research").GetResolvedTools(),
		},
	}
	if len(roster) != len(want) {
		t.Fatalf("roster = %+v, want %+v", roster, want)
	}
	for i := range want {
		if roster[i].ProfileID != want[i].ProfileID || roster[i].Revision != want[i].Revision ||
			roster[i].Name != want[i].Name || roster[i].Emoji != want[i].Emoji ||
			roster[i].Description != want[i].Description || !slices.Equal(roster[i].Tools, want[i].Tools) {
			t.Fatalf("roster[%d] = %+v, want %+v", i, roster[i], want[i])
		}
	}
}

// Off, there is no team to delegate to, whatever the profiles say.
func TestTheRosterIsEmptyWhileTheTeamIsOff(t *testing.T) {
	h := newHarness(t)
	h.write(t, "research", researchFrontmatter)
	h.activate(t, "research")

	roster, err := New(h.repo, h.routes, defaultModel, false).Roster(context.Background())
	if err != nil || roster != nil {
		t.Fatalf("roster = %+v, %v; want nil while the team is off", roster, err)
	}
}

// A server named `team` turns delegation off for every profile.
func TestTheRosterIsEmptyOnATeamNameCollision(t *testing.T) {
	h := newHarness(t)
	h.write(t, "research", researchFrontmatter)
	h.activate(t, "research")
	h.importServer(t, repository.ImportedMCPServer{
		Name: "team", URL: "http://team:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
	}, true, "lookup")

	roster, err := h.server.Roster(context.Background())
	if err != nil || len(roster) != 0 {
		t.Fatalf("roster = %+v, %v; want none during a collision", roster, err)
	}
}
