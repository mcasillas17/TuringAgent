package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/skillfiles"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestListSkillsReturnsFileMetadataBodyAndDecisionState(t *testing.T) {
	server, repo, root := newSkillService(t)
	writeServiceSkill(t, root, "writing/tone", `---
name: Tone
description: Keeps prose direct
requires: [files.update]
---
Be brief.
`)
	if _, err := repo.SetSkillEnabled(context.Background(), "writing/tone", true); err != nil {
		t.Fatal(err)
	}

	response, err := server.ListSkills(context.Background(), &turingv1.ListSkillsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(response.GetSkills()) != 1 {
		t.Fatalf("skills = %+v", response.GetSkills())
	}
	skill := response.GetSkills()[0]
	if skill.GetSkillId() != "writing/tone" || skill.GetCategory() != "writing" || skill.GetBody() != "Be brief." {
		t.Fatalf("skill = %+v", skill)
	}
	if skill.GetFolderPath() != filepath.Join(root, "writing", "tone") {
		t.Fatalf("folder path = %q", skill.GetFolderPath())
	}
	if !skill.GetEnabled() || len(skill.GetGrantedCapabilities()) != 0 || len(skill.GetMissingCapabilities()) != 1 {
		t.Fatalf("decision state = %+v", skill)
	}
}

func TestGetSkillReportsUnknownPathWithoutLeakingStorageErrors(t *testing.T) {
	server, _, _ := newSkillService(t)
	_, err := server.GetSkill(context.Background(), &turingv1.GetSkillRequest{SkillId: "missing/skill"})
	if status.Code(err) != codes.NotFound || status.Convert(err).Message() != "skill not found" {
		t.Fatalf("error = %v", err)
	}
}

func TestEnableAndGrantAreSeparateMutations(t *testing.T) {
	server, _, root := newSkillService(t)
	writeServiceSkill(t, root, "writing/tone", `---
name: Tone
description: Keeps prose direct
requires: [files.update]
---
Be brief.
`)

	enabled, err := server.SetSkillEnabled(context.Background(), &turingv1.SetSkillEnabledRequest{
		SkillId: "writing/tone", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(enabled.GetGrantedCapabilities()) != 0 || len(enabled.GetMissingCapabilities()) != 1 {
		t.Fatalf("enable implicitly granted: %+v", enabled)
	}
	granted, err := server.SetSkillCapabilityGrant(context.Background(), &turingv1.SetSkillCapabilityGrantRequest{
		SkillId: "writing/tone", Capability: "files.update", Granted: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(granted.GetGrantedCapabilities()) != 1 || len(granted.GetMissingCapabilities()) != 0 {
		t.Fatalf("grant state = %+v", granted)
	}
}

func TestGrantRejectsCapabilityTheFileDidNotDeclare(t *testing.T) {
	server, _, root := newSkillService(t)
	writeServiceSkill(t, root, "writing/tone", "---\nname: Tone\ndescription: Direct\n---\nBody\n")
	_, err := server.SetSkillCapabilityGrant(context.Background(), &turingv1.SetSkillCapabilityGrantRequest{
		SkillId: "writing/tone", Capability: "system.time", Granted: true,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v", err)
	}
}

func TestNilMutationRequestsAreRejected(t *testing.T) {
	server, _, _ := newSkillService(t)
	if _, err := server.SetSkillEnabled(context.Background(), nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("SetSkillEnabled nil error = %v", err)
	}
	if _, err := server.SetSkillCapabilityGrant(context.Background(), nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("SetSkillCapabilityGrant nil error = %v", err)
	}
}

func newSkillService(t *testing.T) (*Server, *repository.Repository, string) {
	t.Helper()
	database, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(database)
	root := t.TempDir()
	repo.SetSkillStore(skillfiles.New(root))
	return New(repo), repo, root
}

func writeServiceSkill(t *testing.T, root, id, content string) {
	t.Helper()
	filename := filepath.Join(root, filepath.FromSlash(id), "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
