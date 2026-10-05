package team

import (
	"context"
	"errors"
	"google.golang.org/protobuf/proto"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/teamfiles"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeRoutes struct {
	// models is what live team-protocol workers advertise for the Ollama
	// provider.
	models []string
	// older is what only workers older than the team protocol advertise.
	// They are advertised first, so a default chosen without the minimum
	// lands on one.
	older []string
	// tools is, per model, what EgressToolNames returns for that route.
	tools  map[string][]string
	routes []repository.RoutingRequirements
	// validated is every route ValidateRouting was asked about.
	validated []repository.RoutingRequirements
}

func (f *fakeRoutes) ProviderCapabilities() map[turingv1.ModelProvider][]*turingv1.ModelCapability {
	out := map[turingv1.ModelProvider][]*turingv1.ModelCapability{}
	for _, model := range slices.Concat(f.older, f.models) {
		out[turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA] = append(out[turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA],
			&turingv1.ModelCapability{Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA, Model: model})
	}
	return out
}

func (f *fakeRoutes) ValidateRouting(_ context.Context, route repository.RoutingRequirements) error {
	f.validated = append(f.validated, route)
	if slices.Contains(f.models, route.Model) ||
		(route.MinimumTeamProtocolVersion == 0 && slices.Contains(f.older, route.Model)) {
		return nil
	}
	return errors.New("no connected worker supports the requested route")
}

func (f *fakeRoutes) EgressToolNames(route repository.RoutingRequirements) []string {
	f.routes = append(f.routes, route)
	return f.tools[route.Model]
}

const defaultModel = "qwen2.5:7b"

var builtinTools = []string{
	"files/files.read", "files/files.write", "system/system.time",
	"memory/memory.search", "memory/memory.read", "memory/memory.remember",
	"skills/skills_list", "skills/skill_view",
}

type harness struct {
	repo   *repository.Repository
	root   string
	routes *fakeRoutes
	server *Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "turing.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	root := t.TempDir()
	repo.SetTeamStore(teamfiles.New(root))
	routes := &fakeRoutes{models: []string{defaultModel}, tools: map[string][]string{defaultModel: builtinTools}}
	h := &harness{repo: repo, root: root, routes: routes, server: New(repo, routes, defaultModel, true)}
	h.registerTools(t,
		repository.DiscoveredTool{ServerName: "files", ToolName: "files.read", SchemaJSON: `{}`, Policy: "safe"},
		repository.DiscoveredTool{ServerName: "files", ToolName: "files.write", SchemaJSON: `{}`, Policy: "approval_required"},
		repository.DiscoveredTool{ServerName: "system", ToolName: "system.time", SchemaJSON: `{}`, Policy: "safe"},
		repository.DiscoveredTool{ServerName: "memory", ToolName: "memory.search", SchemaJSON: `{}`, Policy: "safe"},
		repository.DiscoveredTool{ServerName: "memory", ToolName: "memory.read", SchemaJSON: `{}`, Policy: "safe"},
		repository.DiscoveredTool{ServerName: "memory", ToolName: "memory.remember", SchemaJSON: `{}`, Policy: "approval_required"},
		repository.DiscoveredTool{ServerName: "skills", ToolName: "skills_list", SchemaJSON: `{}`, Policy: "safe"},
		repository.DiscoveredTool{ServerName: "skills", ToolName: "skill_view", SchemaJSON: `{}`, Policy: "safe"},
		repository.DiscoveredTool{ServerName: "integrations", ToolName: "github.get_issue", SchemaJSON: `{}`, Policy: "safe"},
	)
	return h
}

// registerTools reports one full discovery snapshot, replacing the last one.
func (h *harness) registerTools(t *testing.T, tools ...repository.DiscoveredTool) {
	t.Helper()
	if err := h.repo.UpsertTools(context.Background(), tools); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) addTool(t *testing.T, tool repository.DiscoveredTool) {
	t.Helper()
	catalog, err := h.repo.ListToolCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	tools := []repository.DiscoveredTool{tool}
	for _, entry := range catalog {
		if entry.ServerTier == "" || entry.ServerTier == "bundled" {
			tools = append(tools, repository.DiscoveredTool{
				ServerName: entry.ServerName, ToolName: entry.ToolName, SchemaJSON: `{}`, Policy: entry.Policy,
			})
		}
	}
	h.registerTools(t, tools...)
}

func (h *harness) write(t *testing.T, id, frontmatter string) {
	t.Helper()
	h.writeFile(t, id, "---\n"+frontmatter+"---\nYou are a specialist.\n")
}

func (h *harness) writeFile(t *testing.T, id, content string) {
	t.Helper()
	dir := filepath.Join(h.root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) profile(t *testing.T, id string) *turingv1.AgentProfile {
	t.Helper()
	response, err := h.server.ListAgentProfiles(context.Background(), &turingv1.ListAgentProfilesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, profile := range response.GetProfiles() {
		if profile.GetProfileId() == id {
			return profile
		}
	}
	t.Fatalf("profile %q not listed in %+v", id, response.GetProfiles())
	return nil
}

// activate enables the profile and grants its current revision.
func (h *harness) activate(t *testing.T, id string) {
	t.Helper()
	ctx := context.Background()
	revision := h.profile(t, id).GetRevision()
	if _, err := h.server.GrantAgentProfile(ctx, &turingv1.GrantAgentProfileRequest{ProfileId: id, Revision: revision}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.server.SetAgentProfileEnabled(ctx, &turingv1.SetAgentProfileEnabledRequest{ProfileId: id, Enabled: true}); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) registerRemoteServer(t *testing.T, name string, tools ...string) {
	t.Helper()
	h.importServer(t, repository.ImportedMCPServer{
		Name: name, URL: "https://" + strings.ToLower(name) + ".example.test/mcp", Tier: repository.MCPServerTierRemoteURL,
	}, true, tools...)
}

// importServer registers an MCP server with safe tools, enabled or not, and
// returns its ID.
func (h *harness) importServer(t *testing.T, server repository.ImportedMCPServer, enabled bool, tools ...string) string {
	t.Helper()
	ctx := context.Background()
	for _, tool := range tools {
		server.Tools = append(server.Tools, repository.MCPServerTool{Name: tool, Policy: "safe", SchemaJSON: `{"type":"object"}`})
	}
	result, err := h.repo.ImportMCPServer(ctx, server)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.repo.SetMCPServerEnabled(ctx, result.Server.ID, enabled); err != nil {
		t.Fatal(err)
	}
	return result.Server.ID
}

func exclusion(profile *turingv1.AgentProfile, tool string) string {
	for _, excluded := range profile.GetExcludedTools() {
		if excluded.GetTool() == tool {
			return excluded.GetReason()
		}
	}
	return ""
}

func wantState(t *testing.T, profile *turingv1.AgentProfile, want turingv1.AgentProfileState) {
	t.Helper()
	if profile.GetState() != want {
		t.Fatalf("state = %v, want %v (reasons %q, excluded %+v, parse error %q)",
			profile.GetState(), want, profile.GetUnavailableReasons(), profile.GetExcludedTools(), profile.GetParseError())
	}
}

func wantReason(t *testing.T, profile *turingv1.AgentProfile, fragments ...string) {
	t.Helper()
	for _, reason := range profile.GetUnavailableReasons() {
		matched := true
		for _, fragment := range fragments {
			if !strings.Contains(reason, fragment) {
				matched = false
				break
			}
		}
		if matched {
			return
		}
	}
	t.Fatalf("no reason contains %q; reasons = %q", fragments, profile.GetUnavailableReasons())
}

const researchFrontmatter = "name: Research\nemoji: \"🔬\"\ndescription: Looks things up\nversion: 1\n" +
	"tools: [memory.search, memory.read, files.*, skills_list, skill_view, system.time]\n" +
	"memory: read\nmax_tool_calls: 12\n"

func TestAnActiveProfileReportsItsResolvedRunShape(t *testing.T) {
	h := newHarness(t)
	h.write(t, "research", researchFrontmatter)
	h.activate(t, "research")

	profile := h.profile(t, "research")
	wantState(t, profile, turingv1.AgentProfileState_AGENT_PROFILE_STATE_ACTIVE)
	want := []string{
		"files/files.read", "files/files.write", "memory/memory.read", "memory/memory.search",
		"skills/skill_view", "skills/skills_list", "system/system.time",
	}
	if !slices.Equal(profile.GetResolvedTools(), want) {
		t.Fatalf("resolved tools = %q, want %q", profile.GetResolvedTools(), want)
	}
	if profile.GetResolvedModel() != defaultModel || profile.GetModel() != "" {
		t.Fatalf("model = %q, resolved = %q", profile.GetModel(), profile.GetResolvedModel())
	}
	if profile.GetName() != "Research" || profile.GetEmoji() != "🔬" || profile.GetVersion() != "1" ||
		profile.GetMaxToolCalls() != 12 || profile.GetMemory() != turingv1.AgentProfileMemoryAccess_AGENT_PROFILE_MEMORY_ACCESS_READ ||
		!profile.GetEnabled() || profile.GetGrantedRevision() != profile.GetRevision() ||
		!slices.Equal(profile.GetTools(), []string{"files.*", "memory.read", "memory.search", "skill_view", "skills_list", "system.time"}) {
		t.Fatalf("profile = %+v", profile)
	}
	if len(profile.GetUnavailableReasons()) != 0 || len(profile.GetExcludedTools()) != 0 {
		t.Fatalf("reasons = %q, excluded = %+v", profile.GetUnavailableReasons(), profile.GetExcludedTools())
	}
	if len(h.routes.routes) == 0 {
		t.Fatal("route tools were never consulted")
	}
	for _, route := range h.routes.routes {
		if route.AgentID != "general_assistant" || route.ModelProvider != "ollama" || route.Model != defaultModel {
			t.Fatalf("route = %+v, want the child's general-assistant Ollama route", route)
		}
	}
}

func TestStatePrecedence(t *testing.T) {
	h := newHarness(t)
	h.write(t, "broken", "name: Broken\n")
	h.write(t, "off", researchFrontmatter+"requires: [gmail.*]\n")
	h.write(t, "ungranted", researchFrontmatter+"requires: [gmail.*]\n")
	h.write(t, "blocked", researchFrontmatter+"requires: [gmail.*]\n")
	h.write(t, "ready", researchFrontmatter)

	ctx := context.Background()
	if _, err := h.server.SetAgentProfileEnabled(ctx, &turingv1.SetAgentProfileEnabledRequest{ProfileId: "ungranted", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	h.activate(t, "blocked")
	h.activate(t, "ready")
	offRevision := h.profile(t, "off").GetRevision()
	if _, err := h.server.GrantAgentProfile(ctx, &turingv1.GrantAgentProfileRequest{ProfileId: "off", Revision: offRevision}); err != nil {
		t.Fatal(err)
	}

	broken := h.profile(t, "broken")
	wantState(t, broken, turingv1.AgentProfileState_AGENT_PROFILE_STATE_PARSE_ERROR)
	if broken.GetParseError() == "" || broken.GetRevision() != "" || len(broken.GetResolvedTools()) != 0 {
		t.Fatalf("broken = %+v", broken)
	}
	// Each of these is also unavailable; the earlier state still wins.
	wantState(t, h.profile(t, "off"), turingv1.AgentProfileState_AGENT_PROFILE_STATE_DISABLED)
	wantState(t, h.profile(t, "ungranted"), turingv1.AgentProfileState_AGENT_PROFILE_STATE_NEEDS_GRANT)
	wantState(t, h.profile(t, "blocked"), turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE)
	wantState(t, h.profile(t, "ready"), turingv1.AgentProfileState_AGENT_PROFILE_STATE_ACTIVE)
}

func TestReasonsAreReportedWhateverTheState(t *testing.T) {
	h := newHarness(t)
	h.write(t, "inbox", "name: Inbox\ndescription: Mail\nmemory: read\ntools: [gmail.*]\nrequires: [gmail.*]\n")

	inbox := h.profile(t, "inbox")
	wantState(t, inbox, turingv1.AgentProfileState_AGENT_PROFILE_STATE_DISABLED)
	wantReason(t, inbox, "gmail.*", "not connected")
}

func TestAnEditedDeclarationNeedsANewGrant(t *testing.T) {
	h := newHarness(t)
	h.write(t, "research", researchFrontmatter)
	h.activate(t, "research")
	h.write(t, "research", strings.Replace(researchFrontmatter, "max_tool_calls: 12", "max_tool_calls: 20", 1))

	profile := h.profile(t, "research")
	wantState(t, profile, turingv1.AgentProfileState_AGENT_PROFILE_STATE_NEEDS_GRANT)
	if !profile.GetEnabled() || profile.GetGrantedRevision() == profile.GetRevision() {
		t.Fatalf("profile = %+v, want enabled with a stale grant", profile)
	}
}

func TestEditingOnlyTheBodyAndDisplayFieldsKeepsTheGrant(t *testing.T) {
	h := newHarness(t)
	h.write(t, "research", researchFrontmatter)
	h.activate(t, "research")
	h.writeFile(t, "research", "---\n"+strings.Replace(researchFrontmatter, "Looks things up", "Finds things", 1)+"---\nNew body.\n")

	profile := h.profile(t, "research")
	wantState(t, profile, turingv1.AgentProfileState_AGENT_PROFILE_STATE_ACTIVE)
	if profile.GetDescription() != "Finds things" {
		t.Fatalf("profile = %+v", profile)
	}
}

func TestARequiredEgressingToolIsUnavailableWithTheConsentReason(t *testing.T) {
	h := newHarness(t)
	h.routes.tools[defaultModel] = append(slices.Clone(builtinTools), "integrations/github.get_issue")
	h.write(t, "dev", "name: Dev\ndescription: Code\ntools: [files.*, github.get_issue]\nrequires: [github.get_issue]\n")
	h.activate(t, "dev")

	dev := h.profile(t, "dev")
	wantState(t, dev, turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE)
	wantReason(t, dev, "github.get_issue", "consent")
	if reason := exclusion(dev, "integrations/github.get_issue"); !strings.Contains(reason, "consent") {
		t.Fatalf("exclusion = %q, want the consent reason", reason)
	}
	if slices.Contains(dev.GetResolvedTools(), "integrations/github.get_issue") {
		t.Fatalf("resolved tools %q include an egressing tool", dev.GetResolvedTools())
	}
}

func TestARemoteMCPServerToolIsEgressing(t *testing.T) {
	h := newHarness(t)
	h.registerRemoteServer(t, "notes", "notes.search")
	h.routes.tools[defaultModel] = append(slices.Clone(builtinTools), "notes/notes.search")
	h.write(t, "research", "name: Research\ndescription: d\ntools: [notes.*]\nrequires: [notes.search]\n")
	h.activate(t, "research")

	research := h.profile(t, "research")
	if reason := exclusion(research, "notes/notes.search"); !strings.Contains(reason, "consent") {
		t.Fatalf("exclusion = %q (excluded %+v)", reason, research.GetExcludedTools())
	}
	wantState(t, research, turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE)
}

func TestARequiredToolDisabledByPolicyIsUnavailable(t *testing.T) {
	h := newHarness(t)
	if err := h.repo.SetToolPolicyByName(context.Background(), "system", "system.time", "disabled"); err != nil {
		t.Fatal(err)
	}
	h.write(t, "clock", "name: Clock\ndescription: d\ntools: [system.*]\nrequires: [system.time]\n")
	h.activate(t, "clock")

	clock := h.profile(t, "clock")
	wantState(t, clock, turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE)
	wantReason(t, clock, "system.time", "disabled by policy")
	if reason := exclusion(clock, "system/system.time"); reason != "disabled by policy" {
		t.Fatalf("exclusion = %q", reason)
	}
}

// Workers stop reporting a tool once its policy disables it, and the refresh
// marks it absent. It is still the policy that withholds it, and the profile
// still says so.
func TestARequiredToolStaysDisabledByPolicyAfterAWorkerRefresh(t *testing.T) {
	h := newHarness(t)
	if err := h.repo.SetToolPolicyByName(context.Background(), "system", "system.time", "disabled"); err != nil {
		t.Fatal(err)
	}
	h.registerTools(t,
		repository.DiscoveredTool{ServerName: "files", ToolName: "files.read", SchemaJSON: `{}`, Policy: "safe"},
		repository.DiscoveredTool{ServerName: "memory", ToolName: "memory.search", SchemaJSON: `{}`, Policy: "safe"},
	)
	h.write(t, "clock", "name: Clock\ndescription: d\ntools: [system.*]\nrequires: [system.time]\n")
	h.activate(t, "clock")

	clock := h.profile(t, "clock")
	wantState(t, clock, turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE)
	wantReason(t, clock, "system.time", "disabled by policy")
	if reason := exclusion(clock, "system/system.time"); reason != "disabled by policy" {
		t.Fatalf("exclusion = %q (excluded %+v)", reason, clock.GetExcludedTools())
	}
}

// A local container's tools do not egress and its worker serves them, so the
// disabled server is the only thing keeping one out of a delegated run.
func TestARequiredToolOnADisabledServerIsUnavailable(t *testing.T) {
	h := newHarness(t)
	id := h.importServer(t, repository.ImportedMCPServer{
		Name: "vendor", URL: "http://vendor:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
	}, true, "vendor.lookup")
	if err := h.repo.SetMCPServerEnabled(context.Background(), id, false); err != nil {
		t.Fatal(err)
	}
	h.routes.tools[defaultModel] = append(slices.Clone(builtinTools), "vendor/vendor.lookup")
	h.write(t, "dev", "name: Dev\ndescription: d\ntools: [vendor.*]\nrequires: [vendor.lookup]\n")
	h.activate(t, "dev")

	dev := h.profile(t, "dev")
	wantState(t, dev, turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE)
	wantReason(t, dev, "vendor.lookup", "its server is disabled")
	if reason := exclusion(dev, "vendor/vendor.lookup"); reason != "its server is disabled" {
		t.Fatalf("exclusion = %q (excluded %+v)", reason, dev.GetExcludedTools())
	}
	if slices.Contains(dev.GetResolvedTools(), "vendor/vendor.lookup") {
		t.Fatalf("resolved tools %q include a disabled server's tool", dev.GetResolvedTools())
	}
}

func TestARequiredToolBeyondTheMemoryLevelIsUnavailable(t *testing.T) {
	h := newHarness(t)
	h.write(t, "scribe", "name: Scribe\ndescription: d\nmemory: read\ntools: [memory.*]\nrequires: [memory.remember]\n")
	h.activate(t, "scribe")

	scribe := h.profile(t, "scribe")
	wantState(t, scribe, turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE)
	wantReason(t, scribe, "memory.remember", "memory level")
	if !slices.Equal(scribe.GetResolvedTools(), []string{"memory/memory.read", "memory/memory.search"}) {
		t.Fatalf("resolved = %q", scribe.GetResolvedTools())
	}
}

func TestMemoryLevelsMapToTheirTools(t *testing.T) {
	for _, tc := range []struct {
		level string
		want  []string
	}{
		{"none", nil},
		{"read", []string{"memory/memory.read", "memory/memory.search"}},
		{"propose", []string{"memory/memory.read", "memory/memory.remember", "memory/memory.search"}},
	} {
		t.Run(tc.level, func(t *testing.T) {
			h := newHarness(t)
			h.write(t, "scribe", "name: Scribe\ndescription: d\nmemory: "+tc.level+"\ntools: [memory.*]\n")
			scribe := h.profile(t, "scribe")
			if !slices.Equal(scribe.GetResolvedTools(), tc.want) {
				t.Fatalf("resolved = %q, want %q", scribe.GetResolvedTools(), tc.want)
			}
		})
	}
}

func TestAnUnknownMemoryToolIsNeverGranted(t *testing.T) {
	h := newHarness(t)
	h.addTool(t, repository.DiscoveredTool{ServerName: "memory", ToolName: "memory.forget", SchemaJSON: `{}`, Policy: "safe"})
	h.routes.tools[defaultModel] = append(slices.Clone(builtinTools), "memory/memory.forget")
	h.write(t, "scribe", "name: Scribe\ndescription: d\nmemory: propose\ntools: [memory.*]\n")

	if reason := exclusion(h.profile(t, "scribe"), "memory/memory.forget"); !strings.Contains(reason, "memory level") {
		t.Fatalf("exclusion = %q", reason)
	}
}

func TestARequiredToolMissingFromARouteWorkerIsUnavailable(t *testing.T) {
	h := newHarness(t)
	h.routes.tools[defaultModel] = []string{"files/files.read"}
	h.write(t, "clock", "name: Clock\ndescription: d\ntools: [system.time, files.read]\nrequires: [system.time]\n")
	h.activate(t, "clock")

	clock := h.profile(t, "clock")
	wantState(t, clock, turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE)
	wantReason(t, clock, "system.time", "not served by every live worker")
	if !slices.Equal(clock.GetResolvedTools(), []string{"files/files.read"}) {
		t.Fatalf("resolved = %q", clock.GetResolvedTools())
	}
}

func TestARequiresPatternOutsideTheDeclaredToolsIsUnavailable(t *testing.T) {
	h := newHarness(t)
	h.write(t, "clock", "name: Clock\ndescription: d\ntools: [files.read]\nrequires: [system.time]\n")
	h.activate(t, "clock")

	clock := h.profile(t, "clock")
	wantState(t, clock, turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE)
	wantReason(t, clock, "system.time", "not in the profile's tools")
}

// A server named exactly team would have its rows captured by the team
// pseudo-tool, so it turns delegation off rather than serving as an ordinary
// server: even a profile that needs only its tool is unavailable.
func TestAServerNamedTeamTurnsDelegationOffInsteadOfServing(t *testing.T) {
	h := newHarness(t)
	h.importServer(t, repository.ImportedMCPServer{
		Name: "team", URL: "http://team:9000/mcp", Tier: repository.MCPServerTierLocalContainer,
	}, true, "lookup")
	h.routes.tools[defaultModel] = append(slices.Clone(builtinTools), "team/lookup")
	h.write(t, "dev", "name: Dev\ndescription: d\ntools: [lookup]\nrequires: [lookup]\n")
	h.activate(t, "dev")

	dev := h.profile(t, "dev")
	wantState(t, dev, turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE)
	if reasons := dev.GetUnavailableReasons(); len(reasons) == 0 || reasons[0] != repository.TeamServerCollisionReason {
		t.Fatalf("reasons = %q, want the team-server collision first", reasons)
	}
}

func TestATeamToolIsNeverResolved(t *testing.T) {
	h := newHarness(t)
	h.addTool(t, repository.DiscoveredTool{ServerName: "skills", ToolName: "team.delegate", SchemaJSON: `{}`, Policy: "safe"})
	h.routes.tools[defaultModel] = append(slices.Clone(builtinTools), "skills/team.delegate")
	h.write(t, "nested", "name: Nested\ndescription: d\ntools: [team.*]\n")

	nested := h.profile(t, "nested")
	if len(nested.GetResolvedTools()) != 0 {
		t.Fatalf("resolved = %q", nested.GetResolvedTools())
	}
	if reason := exclusion(nested, "skills/team.delegate"); !strings.Contains(reason, "delegate") {
		t.Fatalf("exclusion = %q", reason)
	}
}

func TestAnUnservedModelIsUnavailable(t *testing.T) {
	h := newHarness(t)
	h.write(t, "research", researchFrontmatter+"model: llama3.1:8b\nrequires: [files.read]\n")
	h.activate(t, "research")

	research := h.profile(t, "research")
	wantState(t, research, turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE)
	wantReason(t, research, "llama3.1:8b", "not served by any team-protocol worker")
	if research.GetResolvedModel() != "" || research.GetModel() != "llama3.1:8b" {
		t.Fatalf("model = %q, resolved = %q", research.GetModel(), research.GetResolvedModel())
	}
	wantOnlyTheModelReason(t, research)
}

// With no route there is nothing to compare a tool against, so the missing
// model is the reason and no tool is blamed on the route.
func wantOnlyTheModelReason(t *testing.T, profile *turingv1.AgentProfile) {
	t.Helper()
	if len(profile.GetResolvedTools()) != 0 {
		t.Fatalf("resolved = %q", profile.GetResolvedTools())
	}
	if len(profile.GetUnavailableReasons()) != 1 {
		t.Fatalf("reasons = %q, want only the model's", profile.GetUnavailableReasons())
	}
	for _, exclusion := range profile.GetExcludedTools() {
		if strings.Contains(exclusion.GetReason(), "live worker") {
			t.Fatalf("%s blamed on the route: %q", exclusion.GetTool(), exclusion.GetReason())
		}
	}
}

func TestADeclaredServedModelRoutesOnThatModel(t *testing.T) {
	h := newHarness(t)
	h.routes.models = append(h.routes.models, "llama3.1:8b")
	h.routes.tools["llama3.1:8b"] = []string{"files/files.read"}
	h.write(t, "research", "name: Research\ndescription: d\nmodel: llama3.1:8b\ntools: [files.*]\n")
	h.activate(t, "research")

	research := h.profile(t, "research")
	wantState(t, research, turingv1.AgentProfileState_AGENT_PROFILE_STATE_ACTIVE)
	if research.GetResolvedModel() != "llama3.1:8b" || !slices.Equal(research.GetResolvedTools(), []string{"files/files.read"}) {
		t.Fatalf("resolved model = %q, tools = %q", research.GetResolvedModel(), research.GetResolvedTools())
	}
	if reason := exclusion(research, "files/files.write"); !strings.Contains(reason, "not served by every live worker") {
		t.Fatalf("exclusion = %q", reason)
	}
}

func TestNoLiveWorkerMakesEveryProfileUnavailable(t *testing.T) {
	h := newHarness(t)
	h.routes.models = nil
	h.write(t, "research", researchFrontmatter+"requires: [files.read]\n")
	h.activate(t, "research")

	research := h.profile(t, "research")
	wantState(t, research, turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE)
	wantReason(t, research, "no team-protocol worker serves a local model")
	wantOnlyTheModelReason(t, research)
}

// A child runs only on a team-protocol worker, so a model only older workers
// serve is no model for one: the profile is unavailable and off the roster.
func TestAModelOnlyAnOlderWorkerServesIsNoModelForAChild(t *testing.T) {
	h := newHarness(t)
	h.routes.older = []string{"llama3.1:8b"}
	h.routes.tools["llama3.1:8b"] = builtinTools
	h.write(t, "research", researchFrontmatter+"model: llama3.1:8b\n")
	h.activate(t, "research")

	research := h.profile(t, "research")
	wantState(t, research, turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE)
	wantReason(t, research, "llama3.1:8b", "not served by any team-protocol worker")
	wantOnlyTheModelReason(t, research)
	if roster, err := h.server.Roster(context.Background()); err != nil || len(roster) != 0 {
		t.Fatalf("roster = %+v, %v; want empty", roster, err)
	}
}

// A profile that names no model runs on the configured default only while a
// team-protocol worker serves it, and otherwise on a model one does.
func TestTheDefaultModelIsOneATeamProtocolWorkerServes(t *testing.T) {
	h := newHarness(t)
	h.routes.older = []string{defaultModel}
	h.routes.models = []string{"llama3.2"}
	h.routes.tools["llama3.2"] = builtinTools
	h.write(t, "research", researchFrontmatter)
	h.activate(t, "research")

	research := h.profile(t, "research")
	wantState(t, research, turingv1.AgentProfileState_AGENT_PROFILE_STATE_ACTIVE)
	if research.GetResolvedModel() != "llama3.2" {
		t.Fatalf("resolved model = %q, want the team-protocol worker's llama3.2", research.GetResolvedModel())
	}

	h.routes.models = nil
	research = h.profile(t, "research")
	wantState(t, research, turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE)
	wantReason(t, research, "no team-protocol worker serves a local model")
}

func TestANilRoutesSourceMeansNoWorker(t *testing.T) {
	h := newHarness(t)
	h.server = New(h.repo, nil, defaultModel, true)
	h.write(t, "research", researchFrontmatter)
	h.activate(t, "research")

	wantReason(t, h.profile(t, "research"), "no team-protocol worker")
}

// A team folder or database the service cannot read is reported as a fixed
// Internal error, so container paths and storage text never reach the client.
func TestAnUnreadableTeamRootIsAnInternalErrorThatHidesTheCause(t *testing.T) {
	for _, tc := range []struct {
		name string
		make func(t *testing.T, root string)
	}{
		{"symlinked root", func(t *testing.T, root string) {
			if err := os.Symlink(t.TempDir(), root); err != nil {
				t.Fatal(err)
			}
		}},
		{"regular file root", func(t *testing.T, root string) {
			if err := os.WriteFile(root, []byte("not a folder"), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			root := filepath.Join(t.TempDir(), "secret-team-root")
			tc.make(t, root)
			h.repo.SetTeamStore(teamfiles.New(root))
			ctx := context.Background()
			for name, call := range map[string]func() error{
				"list": func() error {
					_, err := h.server.ListAgentProfiles(ctx, &turingv1.ListAgentProfilesRequest{})
					return err
				},
				"enable": func() error {
					_, err := h.server.SetAgentProfileEnabled(ctx, &turingv1.SetAgentProfileEnabledRequest{ProfileId: "research", Enabled: true})
					return err
				},
				"grant": func() error {
					_, err := h.server.GrantAgentProfile(ctx, &turingv1.GrantAgentProfileRequest{ProfileId: "research", Revision: "sha256:x"})
					return err
				},
			} {
				err := call()
				if status.Code(err) != codes.Internal || status.Convert(err).Message() != "team profiles are unavailable" {
					t.Fatalf("%s: err = %v, want Internal \"team profiles are unavailable\"", name, err)
				}
				if strings.Contains(err.Error(), "secret-team-root") {
					t.Fatalf("%s: error leaks the root path: %v", name, err)
				}
			}
		})
	}
}

func TestTeamServiceErrors(t *testing.T) {
	h := newHarness(t)
	h.write(t, "research", researchFrontmatter)
	h.write(t, "broken", "name: Broken\n")
	ctx := context.Background()
	shown := h.profile(t, "research").GetRevision()

	for _, tc := range []struct {
		name string
		call func() error
		want codes.Code
	}{
		{"enable without id", func() error {
			_, err := h.server.SetAgentProfileEnabled(ctx, &turingv1.SetAgentProfileEnabledRequest{Enabled: true})
			return err
		}, codes.InvalidArgument},
		{"grant without id", func() error {
			_, err := h.server.GrantAgentProfile(ctx, &turingv1.GrantAgentProfileRequest{Revision: shown})
			return err
		}, codes.InvalidArgument},
		{"grant without revision", func() error {
			_, err := h.server.GrantAgentProfile(ctx, &turingv1.GrantAgentProfileRequest{ProfileId: "research"})
			return err
		}, codes.InvalidArgument},
		{"enable unknown", func() error {
			_, err := h.server.SetAgentProfileEnabled(ctx, &turingv1.SetAgentProfileEnabledRequest{ProfileId: "nobody", Enabled: true})
			return err
		}, codes.NotFound},
		{"grant unknown", func() error {
			_, err := h.server.GrantAgentProfile(ctx, &turingv1.GrantAgentProfileRequest{ProfileId: "nobody", Revision: shown})
			return err
		}, codes.NotFound},
		{"enable broken", func() error {
			_, err := h.server.SetAgentProfileEnabled(ctx, &turingv1.SetAgentProfileEnabledRequest{ProfileId: "broken", Enabled: true})
			return err
		}, codes.FailedPrecondition},
		{"grant stale", func() error {
			_, err := h.server.GrantAgentProfile(ctx, &turingv1.GrantAgentProfileRequest{ProfileId: "research", Revision: "sha256:old"})
			return err
		}, codes.FailedPrecondition},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := status.Code(tc.call()); got != tc.want {
				t.Fatalf("code = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMutationsReturnTheResolvedProfile(t *testing.T) {
	h := newHarness(t)
	h.write(t, "research", researchFrontmatter)
	ctx := context.Background()
	shown := h.profile(t, "research").GetRevision()

	granted, err := h.server.GrantAgentProfile(ctx, &turingv1.GrantAgentProfileRequest{ProfileId: "research", Revision: shown})
	if err != nil {
		t.Fatal(err)
	}
	wantState(t, granted, turingv1.AgentProfileState_AGENT_PROFILE_STATE_DISABLED)
	enabled, err := h.server.SetAgentProfileEnabled(ctx, &turingv1.SetAgentProfileEnabledRequest{ProfileId: "research", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	wantState(t, enabled, turingv1.AgentProfileState_AGENT_PROFILE_STATE_ACTIVE)
	if len(enabled.GetResolvedTools()) == 0 {
		t.Fatal("the enable response carries no resolved tools")
	}
}

// One profile whose decoded text is not UTF-8 is a parse error, and the list
// beside it still marshals.
func TestABinaryFrontmatterValueDoesNotBreakTheList(t *testing.T) {
	h := newHarness(t)
	h.write(t, "research", researchFrontmatter)
	h.write(t, "broken", "name: !!binary /w==\ndescription: d\n")

	response, err := h.server.ListAgentProfiles(context.Background(), &turingv1.ListAgentProfilesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proto.Marshal(response); err != nil {
		t.Fatalf("the list does not marshal: %v", err)
	}
	wantState(t, h.profile(t, "broken"), turingv1.AgentProfileState_AGENT_PROFILE_STATE_PARSE_ERROR)
}

// Every profile in one list is resolved against the same worker snapshot, so
// each model's child route is checked once, not once per profile.
func TestOneListResolvesTheModelsOnce(t *testing.T) {
	h := newHarness(t)
	for _, id := range []string{"dev", "inbox", "research"} {
		h.write(t, id, "name: P\ndescription: d\ntools: [files.read]\n")
	}
	h.routes.validated = nil

	response, err := h.server.ListAgentProfiles(context.Background(), &turingv1.ListAgentProfilesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetProfiles()) != 3 || len(h.routes.validated) != 1 {
		t.Fatalf("profiles = %d, model checks = %d; want 3 and 1", len(response.GetProfiles()), len(h.routes.validated))
	}
	for _, profile := range response.GetProfiles() {
		if profile.GetResolvedModel() != defaultModel {
			t.Fatalf("%s resolved %q, want %q", profile.GetProfileId(), profile.GetResolvedModel(), defaultModel)
		}
	}
}

// A user server named team in any case, or a third-party team.* tool, turns
// delegation off. The usual state precedence still holds, so a profile that
// is off, ungranted or unreadable keeps that state, but every profile that
// can be resolved lists the collision first, and the one that would be
// active is unavailable. Each returns to its own state once the name is free.
func TestEveryProfileExplainsATeamNameCollision(t *testing.T) {
	h := newHarness(t)
	h.write(t, "broken", "name: Broken\n")
	h.write(t, "off", researchFrontmatter)
	h.write(t, "ungranted", researchFrontmatter)
	h.write(t, "research", researchFrontmatter)
	ctx := context.Background()
	if _, err := h.server.SetAgentProfileEnabled(ctx, &turingv1.SetAgentProfileEnabledRequest{ProfileId: "ungranted", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	h.activate(t, "research")
	wantState(t, h.profile(t, "research"), turingv1.AgentProfileState_AGENT_PROFILE_STATE_ACTIVE)

	wantStates := func(reason string, research turingv1.AgentProfileState) {
		t.Helper()
		for id, state := range map[string]turingv1.AgentProfileState{
			"off":       turingv1.AgentProfileState_AGENT_PROFILE_STATE_DISABLED,
			"ungranted": turingv1.AgentProfileState_AGENT_PROFILE_STATE_NEEDS_GRANT,
			"research":  research,
		} {
			profile := h.profile(t, id)
			wantState(t, profile, state)
			reasons := profile.GetUnavailableReasons()
			if reason == "" && len(reasons) != 0 {
				t.Fatalf("%s reasons = %q, want none", id, reasons)
			}
			if reason != "" && (len(reasons) == 0 || reasons[0] != reason) {
				t.Fatalf("%s reasons = %q, want %q first", id, reasons, reason)
			}
		}
		wantState(t, h.profile(t, "broken"), turingv1.AgentProfileState_AGENT_PROFILE_STATE_PARSE_ERROR)
	}

	serverID := h.importServer(t, repository.ImportedMCPServer{
		Name: "Team", URL: "http://team.example.test/mcp", Tier: repository.MCPServerTierLocalContainer,
	}, true, "lookup")
	wantStates(repository.TeamServerCollisionReason, turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE)

	if _, err := h.repo.DeleteMCPServer(ctx, serverID); err != nil {
		t.Fatal(err)
	}
	wantStates("", turingv1.AgentProfileState_AGENT_PROFILE_STATE_ACTIVE)

	h.importServer(t, repository.ImportedMCPServer{
		Name: "vendor", URL: "http://vendor.example.test/mcp", Tier: repository.MCPServerTierLocalContainer,
	}, true, "team.delegate")
	wantStates(repository.TeamToolCollisionReason("vendor", "team.delegate"), turingv1.AgentProfileState_AGENT_PROFILE_STATE_UNAVAILABLE)
}
