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
		Skills: []repository.AttachedSkill{
			{Name: "Tone", Instructions: "Be brief."},
			{Name: "Format", Instructions: "Use bullets."},
		},
	})

	if len(job.GetSkills()) != 2 {
		t.Fatalf("skills = %+v, want two", job.GetSkills())
	}
	first := job.GetSkills()[0]
	if first.GetName() != "Tone" || first.GetInstructions() != "Be brief." {
		t.Fatalf("first skill = %+v, want the attached one, in order", first)
	}
	// Order is the order the runtime renders sections in, so it must survive.
	if job.GetSkills()[1].GetName() != "Format" {
		t.Fatalf("second skill = %q, want Format", job.GetSkills()[1].GetName())
	}
}

func TestMapJobSendsNoSkillsWhenNoneAreAttached(t *testing.T) {
	job := mapJob(repository.Job{JobID: "job_1", UserText: "hello"})

	if len(job.GetSkills()) != 0 {
		t.Fatalf("skills = %+v, want none", job.GetSkills())
	}
}
