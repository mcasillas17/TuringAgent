package team

import (
	"context"
	"reflect"
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

// A continuation answers for Turing from its specialists' results, with the
// tools of its parent's route on team-protocol workers, but never the team
// again, never a tool that leaves the machine, and never one a policy or a
// disabled server turned off.
func TestAContinuationGetsItsRoutesToolsLessTheTeamEgressAndDisabled(t *testing.T) {
	h := newHarness(t)
	h.server.SetDelegationResultMaxBytes(4096)
	h.registerRemoteServer(t, "Remote", "remote.lookup")
	local := h.importServer(t, repository.ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
	}, true, "vendor.lookup")
	if err := h.repo.SetMCPServerEnabled(context.Background(), local, false); err != nil {
		t.Fatal(err)
	}
	if err := h.repo.SetToolPolicyByName(context.Background(), "system", "system.time", "disabled"); err != nil {
		t.Fatal(err)
	}
	h.routes.tools[defaultModel] = append(slices.Clone(builtinTools),
		"integrations/github.get_issue", "Remote/remote.lookup", "vendor/vendor.lookup", repository.TeamDelegateTool)
	route := repository.RoutingRequirements{
		AgentID: "general_assistant", ModelProvider: "ollama", Model: defaultModel, MinimumTeamProtocolVersion: 1,
	}

	continuation, err := h.server.Continuation(context.Background(), route)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"files/files.read", "files/files.write",
		"memory/memory.read", "memory/memory.remember", "memory/memory.search",
		"skills/skill_view", "skills/skills_list",
	}
	if !slices.Equal(continuation.Tools, want) || continuation.ResultMaxBytes != 4096 {
		t.Fatalf("continuation = %+v, want tools %v and the configured cap", continuation, want)
	}
	if got := h.routes.routes[len(h.routes.routes)-1]; !reflect.DeepEqual(got, route) {
		t.Fatalf("route tools read for %+v, want the parent's route %+v", got, route)
	}
}

// With no worker on the route there are no tools: the continuation answers
// from the results alone, rather than waiting on a tool nobody serves.
func TestAContinuationOnAnUnservedRouteGetsNoTools(t *testing.T) {
	h := newHarness(t)
	continuation, err := h.server.Continuation(context.Background(), repository.RoutingRequirements{
		AgentID: "general_assistant", ModelProvider: "ollama", Model: "unserved", MinimumTeamProtocolVersion: 1,
	})
	if err != nil || len(continuation.Tools) != 0 {
		t.Fatalf("continuation = %+v, %v; want no tools", continuation, err)
	}
}
