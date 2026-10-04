package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/teamfiles"
)

func newTeamRepository(t *testing.T) (*Repository, string) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := New(database)
	root := t.TempDir()
	repo.SetTeamStore(teamfiles.New(root))
	return repo, root
}

func writeTeamProfile(t *testing.T, root, id, content string) {
	t.Helper()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const researchProfile = "---\nname: Research\ndescription: Looks things up\ntools: [files.read_file]\n---\nBe careful.\n"

func agentSettingsRows(t *testing.T, repo *Repository) int {
	t.Helper()
	var count int
	if err := repo.db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM agent_profile_settings`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func onlyAgentProfile(t *testing.T, repo *Repository) AgentProfile {
	t.Helper()
	profiles, err := repo.ListAgentProfiles(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 1 {
		t.Fatalf("profiles = %+v, want one", profiles)
	}
	return profiles[0]
}

func TestListAgentProfilesWithoutAStoreIsEmpty(t *testing.T) {
	repo, _ := newTeamRepository(t)
	repo.SetTeamStore(nil)
	profiles, err := repo.ListAgentProfiles(context.Background())
	if err != nil || len(profiles) != 0 {
		t.Fatalf("profiles, err = %+v, %v; want empty", profiles, err)
	}
	if _, err := repo.SetAgentProfileEnabled(context.Background(), "research", true); !errors.Is(err, ErrAgentProfileNotFound) {
		t.Fatalf("enable err = %v, want ErrAgentProfileNotFound", err)
	}
}

func TestListAgentProfilesIsReadOnlyAndDefaultsToDisabledAndUngranted(t *testing.T) {
	repo, root := newTeamRepository(t)
	writeTeamProfile(t, root, "research", researchProfile)

	profile := onlyAgentProfile(t, repo)
	if profile.ID != "research" || profile.Name != "Research" {
		t.Fatalf("profile = %+v", profile)
	}
	if profile.Enabled || profile.GrantedRevision != "" {
		t.Fatalf("profile = %+v, want disabled and ungranted", profile)
	}
	if rows := agentSettingsRows(t, repo); rows != 0 {
		t.Fatalf("listing wrote %d settings rows, want 0", rows)
	}
}

func TestSetAgentProfileEnabledRoundTripsAndKeepsTheGrant(t *testing.T) {
	ctx := context.Background()
	repo, root := newTeamRepository(t)
	writeTeamProfile(t, root, "research", researchProfile)
	revision := onlyAgentProfile(t, repo).Revision

	if _, err := repo.GrantAgentProfile(ctx, "research", revision); err != nil {
		t.Fatal(err)
	}
	enabled, err := repo.SetAgentProfileEnabled(ctx, "research", true)
	if err != nil {
		t.Fatal(err)
	}
	if !enabled.Enabled || enabled.GrantedRevision != revision {
		t.Fatalf("after enable = %+v", enabled)
	}
	disabled, err := repo.SetAgentProfileEnabled(ctx, "research", false)
	if err != nil {
		t.Fatal(err)
	}
	if disabled.Enabled || disabled.GrantedRevision != revision {
		t.Fatalf("after disable = %+v, want disabled with the grant kept", disabled)
	}
	if listed := onlyAgentProfile(t, repo); listed.Enabled || listed.GrantedRevision != revision {
		t.Fatalf("listed = %+v", listed)
	}
}

func TestGrantAgentProfileDoesNotEnable(t *testing.T) {
	repo, root := newTeamRepository(t)
	writeTeamProfile(t, root, "research", researchProfile)
	revision := onlyAgentProfile(t, repo).Revision

	granted, err := repo.GrantAgentProfile(context.Background(), "research", revision)
	if err != nil {
		t.Fatal(err)
	}
	if granted.Enabled || granted.GrantedRevision != revision {
		t.Fatalf("granted = %+v, want granted and still disabled", granted)
	}
}

func TestGrantAgentProfileRefusesAStaleRevision(t *testing.T) {
	ctx := context.Background()
	repo, root := newTeamRepository(t)
	writeTeamProfile(t, root, "research", researchProfile)
	shown := onlyAgentProfile(t, repo).Revision
	writeTeamProfile(t, root, "research", "---\nname: Research\ndescription: Looks things up\ntools: [files.*]\n---\nBe careful.\n")

	if _, err := repo.GrantAgentProfile(ctx, "research", shown); !errors.Is(err, ErrAgentProfileRevisionStale) {
		t.Fatalf("err = %v, want ErrAgentProfileRevisionStale", err)
	}
	if rows := agentSettingsRows(t, repo); rows != 0 {
		t.Fatalf("a refused grant wrote %d rows", rows)
	}
}

func TestAChangedDeclarationLeavesTheOldGrantBehind(t *testing.T) {
	ctx := context.Background()
	repo, root := newTeamRepository(t)
	writeTeamProfile(t, root, "research", researchProfile)
	shown := onlyAgentProfile(t, repo).Revision
	if _, err := repo.GrantAgentProfile(ctx, "research", shown); err != nil {
		t.Fatal(err)
	}
	writeTeamProfile(t, root, "research", "---\nname: Research\ndescription: Looks things up\ntools: [files.*]\n---\nBe careful.\n")

	profile := onlyAgentProfile(t, repo)
	if profile.GrantedRevision != shown || profile.Revision == shown {
		t.Fatalf("profile = %+v, want the old grant against a new revision", profile)
	}
}

func TestAgentProfileMutationsRejectUnknownAndBrokenProfiles(t *testing.T) {
	ctx := context.Background()
	repo, root := newTeamRepository(t)
	writeTeamProfile(t, root, "broken", "---\nname: Broken\n---\n")

	if _, err := repo.SetAgentProfileEnabled(ctx, "missing", true); !errors.Is(err, ErrAgentProfileNotFound) {
		t.Fatalf("enable missing err = %v", err)
	}
	if _, err := repo.GrantAgentProfile(ctx, "missing", "x"); !errors.Is(err, ErrAgentProfileNotFound) {
		t.Fatalf("grant missing err = %v", err)
	}
	if _, err := repo.SetAgentProfileEnabled(ctx, "broken", true); !errors.Is(err, ErrAgentProfileInvalid) {
		t.Fatalf("enable broken err = %v, want ErrAgentProfileInvalid", err)
	}
	if _, err := repo.GrantAgentProfile(ctx, "broken", ""); !errors.Is(err, ErrAgentProfileInvalid) {
		t.Fatalf("grant broken err = %v, want ErrAgentProfileInvalid", err)
	}
	disabled, err := repo.SetAgentProfileEnabled(ctx, "broken", false)
	if err != nil || disabled.Enabled {
		t.Fatalf("disable broken = %+v, %v; want allowed", disabled, err)
	}
}

func TestSettingsForARemovedFolderAreKeptAndIgnored(t *testing.T) {
	ctx := context.Background()
	repo, root := newTeamRepository(t)
	writeTeamProfile(t, root, "research", researchProfile)
	revision := onlyAgentProfile(t, repo).Revision
	if _, err := repo.GrantAgentProfile(ctx, "research", revision); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetAgentProfileEnabled(ctx, "research", true); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(root, "research")); err != nil {
		t.Fatal(err)
	}

	profiles, err := repo.ListAgentProfiles(ctx)
	if err != nil || len(profiles) != 0 {
		t.Fatalf("profiles, err = %+v, %v; want none", profiles, err)
	}
	if rows := agentSettingsRows(t, repo); rows != 1 {
		t.Fatalf("settings rows = %d, want the orphan kept", rows)
	}
	writeTeamProfile(t, root, "research", researchProfile)
	if back := onlyAgentProfile(t, repo); !back.Enabled || back.GrantedRevision != revision {
		t.Fatalf("restored = %+v, want the old settings to apply again", back)
	}
}

func TestListToolCatalogJoinsTheServerTier(t *testing.T) {
	ctx := context.Background()
	repo, _ := newTeamRepository(t)
	if err := repo.UpsertTools(ctx, []DiscoveredTool{
		{ServerName: "files", ToolName: "read_file", SchemaJSON: `{}`, Policy: "safe"},
		{ServerName: "integrations", ToolName: "github.list_issues", SchemaJSON: `{}`, Policy: "approval_required"},
		{ServerName: "memory", ToolName: "memory.search", SchemaJSON: `{}`, Policy: "disabled"},
		{ServerName: "system", ToolName: "system.time", SchemaJSON: `{}`, Policy: "disabled"},
	}); err != nil {
		t.Fatal(err)
	}
	// The refresh a worker sends once system.time is disabled: it no longer
	// reports the tool.
	if err := repo.UpsertTools(ctx, []DiscoveredTool{
		{ServerName: "files", ToolName: "read_file", SchemaJSON: `{}`, Policy: "safe"},
		{ServerName: "memory", ToolName: "memory.search", SchemaJSON: `{}`, Policy: "disabled"},
	}); err != nil {
		t.Fatal(err)
	}

	entries, err := repo.ListToolCatalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ToolCatalogEntry{}
	for _, entry := range entries {
		byName[entry.ServerName+"/"+entry.ToolName] = entry
	}
	if _, ok := byName["integrations/github.list_issues"]; ok {
		t.Fatalf("withdrawn tool listed: %+v", entries)
	}
	files, ok := byName["files/read_file"]
	if !ok || files.ServerTier != "bundled" || !files.Enabled || files.Policy != "safe" {
		t.Fatalf("files/read_file = %+v, %v", files, ok)
	}
	memory, ok := byName["memory/memory.search"]
	if !ok || memory.ServerTier != "" || memory.Enabled || memory.Policy != "disabled" {
		t.Fatalf("memory/memory.search = %+v, %v", memory, ok)
	}
	withdrawn, ok := byName["system/system.time"]
	if !ok || withdrawn.Enabled || withdrawn.Policy != "disabled" {
		t.Fatalf("a disabled tool its worker stopped reporting = %+v, %v; want it kept for its reason", withdrawn, ok)
	}
}
