package runtime

import (
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// mapJob is the only place the snapshot crosses from storage into the message
// the worker receives. Without a test here, deleting one line silently strips
// skills from every run while the rest of the suite stays green.
func TestMapJobCarriesSkillsToTheWorker(t *testing.T) {
	job := mapJob(repository.Job{
		JobID:     "job_1",
		RunID:     "run_1",
		SessionID: "sess_1",
		UserText:  "hello",
		Skills: []repository.SkillSnapshot{
			{
				SkillID: "writing/tone", Name: "Tone", Description: "Brief prose", Category: "writing",
				Body: "Be brief.", References: map[string]string{"references/example.md": "Example"},
			},
			{Name: "Legacy format", Instructions: "Use bullets."},
			{
				SkillID: "files/organize", Name: "Organize", Description: "Files things", Category: "files",
				Withheld: true, MissingCapabilities: []string{"files.update"},
			},
		},
	})

	if len(job.GetSkills()) != 3 {
		t.Fatalf("skills = %+v, want three", job.GetSkills())
	}
	first := job.GetSkills()[0]
	if first.GetSkillId() != "writing/tone" || first.GetName() != "Tone" ||
		first.GetDescription() != "Brief prose" || first.GetCategory() != "writing" ||
		first.GetInstructions() != "Be brief." || first.GetReferences()["references/example.md"] != "Example" {
		t.Fatalf("first skill = %+v, want the frozen snapshot, in order", first)
	}
	// Jobs queued before the migration used Instructions directly. Their wire
	// field remains readable while new snapshots map Body into that same field.
	if job.GetSkills()[1].GetName() != "Legacy format" || job.GetSkills()[1].GetInstructions() != "Use bullets." {
		t.Fatalf("second skill = %+v, want legacy snapshot", job.GetSkills()[1])
	}
	withheld := job.GetSkills()[2]
	if !withheld.GetWithheld() || len(withheld.GetMissingCapabilities()) != 1 ||
		withheld.GetMissingCapabilities()[0] != "files.update" || withheld.GetInstructions() != "" {
		t.Fatalf("withheld skill = %+v", withheld)
	}
}

func TestMapJobSendsNoSkillsWhenSnapshotIsEmpty(t *testing.T) {
	job := mapJob(repository.Job{JobID: "job_1", UserText: "hello"})

	if len(job.GetSkills()) != 0 {
		t.Fatalf("skills = %+v, want none", job.GetSkills())
	}
}
