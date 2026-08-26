package runtime

import (
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/protobuf/proto"
)

// The runtime is handed the snapshot the enqueue froze. It never reads the
// vault: a job that arrived at a worker minutes after it was accepted has to
// carry the persona the user consented to, not the one on disk now.
//
// Every value here is distinct, including the two the persona carries for its
// own name. A snapshot whose id and display name are the same string cannot
// tell a mapping that carries both from one that carries the id twice — or
// from one that drops the display name and leaves the user's chosen name off
// the run they chose it for.
func TestMapJobCarriesTheFrozenPinnedSnapshot(t *testing.T) {
	job := mapJob(repository.Job{
		JobID: "job", RunID: "run", SessionID: "session", ModelProvider: "ollama",
		PinnedPersona: &repository.PinnedPersonaSnapshot{
			PersonaID: "persona.md", DisplayName: "Turing, plainly",
			Body: "Speak plainly.", ContentHash: "persona-hash",
		},
		PinnedProfile: &repository.PinnedProfileSnapshot{
			ProfileID: "profile.md", Body: "The user keeps chickens.", ContentHash: "profile-hash",
		},
		MemorySnapshotFingerprint: "memory-fingerprint",
	})
	if job.GetPinnedPersona().GetBody() != "Speak plainly." ||
		job.GetPinnedPersona().GetPersonaId() != "persona.md" ||
		job.GetPinnedPersona().GetDisplayName() != "Turing, plainly" ||
		job.GetPinnedPersona().GetContentHash() != "persona-hash" ||
		job.GetPinnedPersona().GetWithheld() {
		t.Fatalf("pinned persona = %+v", job.GetPinnedPersona())
	}
	if job.GetPinnedProfile().GetBody() != "The user keeps chickens." ||
		job.GetPinnedProfile().GetProfileId() != "profile.md" ||
		job.GetPinnedProfile().GetContentHash() != "profile-hash" ||
		job.GetPinnedProfile().GetWithheld() {
		t.Fatalf("pinned profile = %+v", job.GetPinnedProfile())
	}
	if job.GetMemorySnapshotFingerprint() != "memory-fingerprint" {
		t.Fatalf("memory fingerprint = %q", job.GetMemorySnapshotFingerprint())
	}
}

// The assertions above name the fields these messages carry today, which is a
// list that goes stale the moment one is added. A field added to the wire and
// not to the mapping is the quiet version of the bug: the job still builds, the
// run still starts, and the worker is handed a snapshot missing part of what
// the user consented to.
//
// So the fields are read off the descriptor rather than typed out. Every one of
// them is given a value nothing else could have produced, and every one of them
// has to arrive set — which is also what says out loud that the profile
// snapshot has no display name to carry, rather than leaving that as an
// assertion somebody forgot to write.
func TestPinnedSnapshotMappingsCarryEveryFieldOnTheWire(t *testing.T) {
	job := mapJob(repository.Job{
		JobID: "job", RunID: "run", SessionID: "session", ModelProvider: "ollama",
		PinnedPersona: &repository.PinnedPersonaSnapshot{
			PersonaID: "persona.md", DisplayName: "Turing, plainly",
			Body: "Speak plainly.", ContentHash: "persona-hash", Withheld: true,
		},
		PinnedProfile: &repository.PinnedProfileSnapshot{
			ProfileID: "profile.md", Body: "The user keeps chickens.",
			ContentHash: "profile-hash", Withheld: true,
		},
	})
	for _, snapshot := range []proto.Message{job.GetPinnedPersona(), job.GetPinnedProfile()} {
		message := snapshot.ProtoReflect()
		fields := message.Descriptor().Fields()
		for index := 0; index < fields.Len(); index++ {
			field := fields.Get(index)
			if !message.Has(field) {
				t.Fatalf(
					"%s.%s is on the wire and nothing fills it in: %+v",
					message.Descriptor().Name(), field.Name(), snapshot,
				)
			}
		}
	}
}

// A withheld tier is a fact of its own. It must reach the runtime as withheld
// rather than as an empty body, which would read as "the user wrote nothing".
func TestMapJobKeepsWithheldDistinctFromEmpty(t *testing.T) {
	job := mapJob(repository.Job{
		JobID: "job", RunID: "run", SessionID: "session", ModelProvider: "ollama",
		PinnedPersona: &repository.PinnedPersonaSnapshot{PersonaID: "persona.md", Withheld: true},
		PinnedProfile: &repository.PinnedProfileSnapshot{ProfileID: "profile.md"},
	})
	if !job.GetPinnedPersona().GetWithheld() || job.GetPinnedPersona().GetBody() != "" {
		t.Fatalf("withheld persona = %+v", job.GetPinnedPersona())
	}
	if job.GetPinnedProfile().GetWithheld() {
		t.Fatalf("an empty profile was reported as withheld: %+v", job.GetPinnedProfile())
	}
}

// Jobs queued before the vault existed carry no snapshot at all, and nil is the
// honest answer: that run was never offered a persona.
func TestMapJobLeavesLegacyJobsWithoutASnapshot(t *testing.T) {
	job := mapJob(repository.Job{
		JobID: "job", RunID: "run", SessionID: "session", ModelProvider: "ollama",
	})
	if job.GetPinnedPersona() != nil || job.GetPinnedProfile() != nil {
		t.Fatalf("legacy job invented a snapshot: %+v / %+v", job.GetPinnedPersona(), job.GetPinnedProfile())
	}
	if job.GetMemorySnapshotFingerprint() != "" {
		t.Fatalf("legacy job invented a memory fingerprint: %q", job.GetMemorySnapshotFingerprint())
	}
}

// The decision the runtime re-checks against carries its own copy of the
// binding.
func TestProtoEgressDecisionCarriesTheMemoryFingerprint(t *testing.T) {
	decision := toProtoEgressDecision(&repository.RunEgressDecision{
		DecisionID: "egress", Version: repository.RunEgressDecisionVersion,
		Provider: "openai_compatible", Model: "gpt-4o-mini",
		MemorySnapshotFingerprint: "memory-fingerprint",
		MemoryProfileApplicable:   true,
	})
	if decision.GetMemorySnapshotFingerprint() != "memory-fingerprint" {
		t.Fatalf("decision memory fingerprint = %q", decision.GetMemorySnapshotFingerprint())
	}
	if !decision.GetMemoryProfileApplicable() {
		t.Fatal("decision lost the memory applicability flag")
	}
}
