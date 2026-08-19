package repository

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/skillfiles"
)

func TestSkillLoadsWhenEveryDeclaredCapabilityIsGranted(t *testing.T) {
	repo, root := newSkillRepository(t)
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", []string{"files.update"}, "Be brief.")
	grantRepositoryCapability(t, repo, "writing/tone", "files.update")
	enableRepositorySkill(t, repo, "writing/tone")

	loaded := loadableRepositorySkills(t, repo)
	if len(loaded) != 1 || loaded[0].SkillID != "writing/tone" {
		t.Fatalf("loaded = %+v, want writing/tone", loaded)
	}
}

func TestACapabilityAddedAfterTheGrantIsNotGranted(t *testing.T) {
	repo, root := newSkillRepository(t)
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", []string{"files.update"}, "Be brief.")
	grantRepositoryCapability(t, repo, "writing/tone", "files.update")
	enableRepositorySkill(t, repo, "writing/tone")
	if len(loadableRepositorySkills(t, repo)) != 1 {
		t.Fatal("skill did not load before its declaration changed")
	}

	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", []string{"files.update", "system.time"}, "Be brief.")
	if loaded := loadableRepositorySkills(t, repo); len(loaded) != 0 {
		t.Fatalf("loaded = %+v, want widened skill withheld", loaded)
	}
	skill := repositorySkillByID(t, repo, "writing/tone")
	if !reflect.DeepEqual(skill.GrantedCapabilities, []string{"files.update"}) {
		t.Fatalf("grants = %v, want existing grant retained", skill.GrantedCapabilities)
	}
	if !reflect.DeepEqual(skill.MissingCapabilities, []string{"system.time"}) {
		t.Fatalf("missing = %v, want system.time", skill.MissingCapabilities)
	}
}

func TestRevokingOneCapabilityLeavesTheOthers(t *testing.T) {
	repo, root := newSkillRepository(t)
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", []string{"files.read", "files.update"}, "Be brief.")
	for _, capability := range []string{"files.read", "files.update"} {
		grantRepositoryCapability(t, repo, "writing/tone", capability)
	}
	enableRepositorySkill(t, repo, "writing/tone")

	if _, err := repo.SetSkillGrant(context.Background(), "writing/tone", "files.read", false); err != nil {
		t.Fatal(err)
	}
	skill := repositorySkillByID(t, repo, "writing/tone")
	if !reflect.DeepEqual(skill.GrantedCapabilities, []string{"files.update"}) {
		t.Fatalf("grants = %v, want files.update retained", skill.GrantedCapabilities)
	}
	if len(loadableRepositorySkills(t, repo)) != 0 {
		t.Fatal("skill loaded with a revoked declared capability")
	}
}

func TestReAddingACapabilityDoesNotRestoreItsOldGrant(t *testing.T) {
	repo, root := newSkillRepository(t)
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", []string{"files.update"}, "Be brief.")
	grantRepositoryCapability(t, repo, "writing/tone", "files.update")
	enableRepositorySkill(t, repo, "writing/tone")
	path := filepath.Join(root, "writing", "tone", "SKILL.md")
	originalInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", nil, "Be brief.")
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", []string{"files.update"}, "Be brief.")
	if err := os.Chtimes(path, originalInfo.ModTime(), originalInfo.ModTime()); err != nil {
		t.Fatal(err)
	}

	skill := repositorySkillByID(t, repo, "writing/tone")
	if len(skill.GrantedCapabilities) != 0 {
		t.Fatalf("grants = %v, want fresh consent after re-add", skill.GrantedCapabilities)
	}
}

func TestBodyAndDescriptionEditsRequireFreshConsentForSameDeclaration(t *testing.T) {
	repo, root := newSkillRepository(t)
	writeRepositorySkill(t, root, "writing/tone", "Tone", "First description", []string{"files.update"}, "First body.")
	grantRepositoryCapability(t, repo, "writing/tone", "files.update")
	enableRepositorySkill(t, repo, "writing/tone")

	writeRepositorySkill(t, root, "writing/tone", "Tone", "Edited description", []string{"files.update"}, "Edited body.")
	skill := repositorySkillByID(t, repo, "writing/tone")
	if len(skill.GrantedCapabilities) != 0 || !reflect.DeepEqual(skill.MissingCapabilities, []string{"files.update"}) {
		t.Fatalf("grant state after content edit = granted %v missing %v", skill.GrantedCapabilities, skill.MissingCapabilities)
	}
	if len(loadableRepositorySkills(t, repo)) != 0 {
		t.Fatal("content-only edit retained consent for an unobserved declaration history")
	}
}

func TestReAddingCapabilityWithChangedBodyStillRequiresFreshConsent(t *testing.T) {
	repo, root := newSkillRepository(t)
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", []string{"files.update"}, "First body.")
	grantRepositoryCapability(t, repo, "writing/tone", "files.update")
	enableRepositorySkill(t, repo, "writing/tone")

	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", nil, "Intermediate body.")
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Edited description", []string{"files.update"}, "Changed body.")

	skill := repositorySkillByID(t, repo, "writing/tone")
	if len(skill.GrantedCapabilities) != 0 || !reflect.DeepEqual(skill.MissingCapabilities, []string{"files.update"}) {
		t.Fatalf("grant state after changed re-add = granted %v missing %v", skill.GrantedCapabilities, skill.MissingCapabilities)
	}
}

func TestNewSkillIsDisabledAndEnablingDoesNotGrant(t *testing.T) {
	repo, root := newSkillRepository(t)
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", []string{"files.update"}, "Be brief.")

	skill := repositorySkillByID(t, repo, "writing/tone")
	if skill.Enabled {
		t.Fatal("new skill arrived enabled")
	}
	enableRepositorySkill(t, repo, "writing/tone")
	skill = repositorySkillByID(t, repo, "writing/tone")
	if len(skill.GrantedCapabilities) != 0 || !reflect.DeepEqual(skill.MissingCapabilities, []string{"files.update"}) {
		t.Fatalf("enabled skill grants = %v, missing = %v", skill.GrantedCapabilities, skill.MissingCapabilities)
	}
}

func TestRenamingFolderCreatesANewDisabledIdentity(t *testing.T) {
	repo, root := newSkillRepository(t)
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", nil, "Be brief.")
	enableRepositorySkill(t, repo, "writing/tone")
	oldFolder := filepath.Join(root, "writing", "tone")
	newFolder := filepath.Join(root, "writing", "voice")
	if err := os.Rename(oldFolder, newFolder); err != nil {
		t.Fatal(err)
	}

	skills, err := repo.ListSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(skills) != 1 || skills[0].SkillID != "writing/voice" || skills[0].Enabled {
		t.Fatalf("skills = %+v, want renamed path disabled", skills)
	}
}

func TestListSkillsReadsEditedDescriptionWithoutDatabaseWrite(t *testing.T) {
	repo, root := newSkillRepository(t)
	writeRepositorySkill(t, root, "writing/tone", "Tone", "First description", nil, "Be brief.")
	_ = repositorySkillByID(t, repo, "writing/tone")
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Edited on disk", nil, "Be brief.")

	skill := repositorySkillByID(t, repo, "writing/tone")
	if skill.Description != "Edited on disk" {
		t.Fatalf("description = %q", skill.Description)
	}
	for _, column := range []string{"name", "description", "category"} {
		var count int
		if err := repo.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('skill_settings') WHERE name = ?`, column).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("skill_settings unexpectedly caches %s", column)
		}
	}
}

func TestMalformedSkillIsListedButNeverLoadable(t *testing.T) {
	repo, root := newSkillRepository(t)
	path := filepath.Join(root, "writing", "broken", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: Broken\n---\nBody\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	skill := repositorySkillByID(t, repo, "writing/broken")
	if !strings.Contains(skill.ParseError, "description") {
		t.Fatalf("parse error = %q", skill.ParseError)
	}
	if _, err := repo.SetSkillEnabled(context.Background(), "writing/broken", true); err != nil {
		t.Fatal(err)
	}
	if len(loadableRepositorySkills(t, repo)) != 0 {
		t.Fatal("malformed skill became loadable")
	}
}

func TestEnqueueFreezesEnabledSkillBodyAndReferences(t *testing.T) {
	repo, root := newSkillRepository(t)
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", []string{"files.update"}, "Original body.")
	reference := filepath.Join(root, "writing", "tone", "references", "example.md")
	if err := os.MkdirAll(filepath.Dir(reference), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reference, []byte("Original reference."), 0o600); err != nil {
		t.Fatal(err)
	}
	grantRepositoryCapability(t, repo, "writing/tone", "files.update")
	enableRepositorySkill(t, repo, "writing/tone")
	session, err := repo.CreateSession(context.Background(), "Snapshot")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello", AgentID: "general_assistant", ModelProvider: "ollama", Model: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetSkillEnabled(context.Background(), "writing/tone", false); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetSkillGrant(context.Background(), "writing/tone", "files.update", false); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SetSkillGrant(context.Background(), "writing/tone", "files.update", true); err != nil {
		t.Fatal(err)
	}
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Changed", nil, "Changed body.")
	if err := os.WriteFile(reference, []byte("Changed reference."), 0o600); err != nil {
		t.Fatal(err)
	}

	job, err := repo.ClaimNextJob(context.Background(), "general_assistant", "worker")
	if err != nil {
		t.Fatal(err)
	}
	if job.JobID != enqueued.JobID || len(job.Skills) != 1 {
		t.Fatalf("job = %+v", job)
	}
	skill := job.Skills[0]
	if skill.Body != "Original body." || skill.References["references/example.md"] != "Original reference." {
		t.Fatalf("snapshot = %+v", skill)
	}
}

func TestEnqueueIndexesEnabledWithheldSkillWithoutItsContent(t *testing.T) {
	repo, root := newSkillRepository(t)
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", []string{"files.update"}, "Must stay withheld.")
	enableRepositorySkill(t, repo, "writing/tone")
	session, err := repo.CreateSession(context.Background(), "Withheld snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello", AgentID: "general_assistant", ModelProvider: "ollama", Model: "test",
	}); err != nil {
		t.Fatal(err)
	}
	grantRepositoryCapability(t, repo, "writing/tone", "files.update")

	job, err := repo.ClaimNextJob(context.Background(), "general_assistant", "worker")
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Skills) != 1 {
		t.Fatalf("skills = %+v, want enabled metadata snapshot", job.Skills)
	}
	skill := job.Skills[0]
	if skill.SkillID != "writing/tone" || !skill.Withheld ||
		!reflect.DeepEqual(skill.MissingCapabilities, []string{"files.update"}) {
		t.Fatalf("skill = %+v, want withheld files.update", skill)
	}
	if skill.Body != "" || len(skill.References) != 0 {
		t.Fatalf("withheld content leaked into job: %+v", skill)
	}
}

func TestClaimNextJobReadsLegacyNameInstructionsPayload(t *testing.T) {
	repo, _ := newSkillRepository(t)
	session, err := repo.CreateSession(context.Background(), "Legacy skills payload")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello", AgentID: "general_assistant", ModelProvider: "ollama", Model: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	var payloadJSON string
	if err := repo.db.QueryRow(`SELECT payload_json FROM jobs WHERE id = ?`, enqueued.JobID).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	payload["skills"] = []map[string]string{{"name": "Legacy tone", "instructions": "Use bullets."}}
	legacyJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.db.Exec(`UPDATE jobs SET payload_json = ? WHERE id = ?`, string(legacyJSON), enqueued.JobID); err != nil {
		t.Fatal(err)
	}

	job, err := repo.ClaimNextJob(context.Background(), "general_assistant", "worker")
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Skills) != 1 || job.Skills[0].Name != "Legacy tone" || job.Skills[0].Instructions != "Use bullets." {
		t.Fatalf("legacy snapshot = %+v", job.Skills)
	}
}

func TestUnreadableUnrelatedSubtreeDoesNotBlockEnqueue(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced on Windows")
	}
	repo, root := newSkillRepository(t)
	writeRepositorySkill(t, root, "writing/tone", "Tone", "Brief prose", nil, "Original body.")
	enableRepositorySkill(t, repo, "writing/tone")
	blocked := filepath.Join(root, "broken", "blocked")
	if err := os.MkdirAll(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o700) })
	session, err := repo.CreateSession(context.Background(), "Unreadable sibling")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EnqueueUserMessage(context.Background(), EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello", AgentID: "general_assistant", ModelProvider: "ollama", Model: "test",
	}); err != nil {
		t.Fatal(err)
	}
	job, err := repo.ClaimNextJob(context.Background(), "general_assistant", "worker")
	if err != nil {
		t.Fatal(err)
	}
	if len(job.Skills) != 1 || job.Skills[0].SkillID != "writing/tone" {
		t.Fatalf("skills = %+v, want valid sibling snapshotted", job.Skills)
	}
}

func newSkillRepository(t *testing.T) (*Repository, string) {
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
	repo.SetSkillStore(skillfiles.New(root))
	return repo, root
}

func writeRepositorySkill(t *testing.T, root, id, name, description string, requires []string, body string) {
	t.Helper()
	var declaration strings.Builder
	declaration.WriteString("---\nname: ")
	declaration.WriteString(name)
	declaration.WriteString("\ndescription: ")
	declaration.WriteString(description)
	declaration.WriteString("\n")
	if len(requires) > 0 {
		declaration.WriteString("requires:\n")
		for _, capability := range requires {
			declaration.WriteString("  - ")
			declaration.WriteString(capability)
			declaration.WriteString("\n")
		}
	}
	declaration.WriteString("---\n")
	declaration.WriteString(body)
	declaration.WriteString("\n")
	path := filepath.Join(root, filepath.FromSlash(id), "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(declaration.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func repositorySkillByID(t *testing.T, repo *Repository, id string) Skill {
	t.Helper()
	skills, err := repo.ListSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, skill := range skills {
		if skill.SkillID == id {
			return skill
		}
	}
	t.Fatalf("skill %q not found in %+v", id, skills)
	return Skill{}
}

func enableRepositorySkill(t *testing.T, repo *Repository, id string) {
	t.Helper()
	if _, err := repo.SetSkillEnabled(context.Background(), id, true); err != nil {
		t.Fatal(err)
	}
}

func grantRepositoryCapability(t *testing.T, repo *Repository, id, capability string) {
	t.Helper()
	if _, err := repo.SetSkillGrant(context.Background(), id, capability, true); err != nil {
		t.Fatal(err)
	}
}

func loadableRepositorySkills(t *testing.T, repo *Repository) []SkillSnapshot {
	t.Helper()
	skills, err := repo.ListSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var loadable []SkillSnapshot
	for _, snapshot := range enabledSnapshots(skills) {
		if !snapshot.Withheld {
			loadable = append(loadable, snapshot)
		}
	}
	return loadable
}
