package repository

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// specialistPayload stamps a queued job with the keys a delegated specialist's
// job carries, the way PR 2c's producer will write them.
func specialistPayload(t *testing.T, ctx context.Context, repo *Repository, jobID string, selected string) {
	t.Helper()
	if _, err := repo.db.ExecContext(ctx, `
		UPDATE jobs SET payload_json = json_set(payload_json,
			'$.agentProfile', json('{"profileId":"research","revision":"rev-1","displayName":"Research","emoji":"🔬","instructions":"Find sources.","maxToolCalls":4}'),
			'$.enforceSelectedTools', json('true'),
			'$.skipAutomaticRecall', json('true'),
			'$.selectedTools', json(?))
		WHERE id = ?`, selected, jobID); err != nil {
		t.Fatal(err)
	}
}

func TestAClaimedJobCarriesTheSpecialistContract(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	enqueued := enqueueLocalTurn(t, ctx, repo, "Specialist")
	specialistPayload(t, ctx, repo, enqueued.JobID, `["files/files.read"]`)

	job, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "worker-v1", 0, time.Hour, nil, nil)
	if err != nil || job.JobID != enqueued.JobID {
		t.Fatalf("claim = %+v, %v", job, err)
	}
	want := &AgentProfileSnapshot{
		ProfileID: "research", Revision: "rev-1", DisplayName: "Research", Emoji: "🔬",
		Instructions: "Find sources.", MaxToolCalls: 4,
	}
	if !reflect.DeepEqual(job.AgentProfile, want) {
		t.Fatalf("agent profile = %+v, want %+v", job.AgentProfile, want)
	}
	if !job.EnforceSelectedTools || !job.SkipAutomaticRecall {
		t.Fatalf("enforce = %v, skip recall = %v; want both set", job.EnforceSelectedTools, job.SkipAutomaticRecall)
	}
	if !reflect.DeepEqual(job.SelectedTools, []string{"files/files.read"}) {
		t.Fatalf("selected tools = %v", job.SelectedTools)
	}
}

// Every job queued before the contract, and every ordinary turn after it,
// lacks the keys and must claim exactly as it did.
func TestAnOrdinaryJobCarriesNoSpecialistContract(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	enqueued := enqueueLocalTurn(t, ctx, repo, "Ordinary")

	job, err := repo.ClaimNextCompatibleJobWithLimit(ctx, "general_assistant", "worker-v1", 0, time.Hour, nil, nil)
	if err != nil || job.JobID != enqueued.JobID {
		t.Fatalf("claim = %+v, %v", job, err)
	}
	if job.AgentProfile != nil || job.EnforceSelectedTools || job.SkipAutomaticRecall {
		t.Fatalf("ordinary job = profile %+v enforce %v skip recall %v; want none", job.AgentProfile, job.EnforceSelectedTools, job.SkipAutomaticRecall)
	}
}

// The orchestrator's own enforcement reads the run's persisted job, never a
// caller's claim about it.
func TestRunToolSelectionReadsThePersistedJob(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	specialist := enqueueLocalTurn(t, ctx, repo, "Specialist")
	specialistPayload(t, ctx, repo, specialist.JobID, `["files/files.read","memory/memory.search"]`)
	empty := enqueueLocalTurn(t, ctx, repo, "No tools")
	specialistPayload(t, ctx, repo, empty.JobID, `[]`)
	ordinary := enqueueLocalTurn(t, ctx, repo, "Ordinary")

	for _, test := range []struct {
		name     string
		runID    string
		enforced bool
		tools    []string
	}{
		{"specialist", specialist.RunID, true, []string{"files/files.read", "memory/memory.search"}},
		{"empty set", empty.RunID, true, nil},
		{"ordinary", ordinary.RunID, false, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			selection, err := repo.RunToolSelection(ctx, test.runID)
			if err != nil {
				t.Fatal(err)
			}
			if selection.Enforced != test.enforced || len(selection.Tools) != len(test.tools) {
				t.Fatalf("selection = %+v, want enforced %v tools %v", selection, test.enforced, test.tools)
			}
			for _, tool := range test.tools {
				if !selection.Allows(tool) {
					t.Fatalf("selection %+v does not allow %q", selection, tool)
				}
			}
			if test.enforced && selection.Allows("system/system.time") {
				t.Fatalf("enforced selection %+v allows an unlisted tool", selection)
			}
			if !test.enforced && !selection.Allows("system/system.time") {
				t.Fatal("an unenforced selection refused a tool")
			}
		})
	}
	if _, err := repo.RunToolSelection(ctx, "run_missing"); err == nil {
		t.Fatal("a run with no job reported a selection")
	}
}

// A recorded tool call is judged by the server and tool its own record names.
// Only a job that enforces its set constrains anything, so an ordinary run and
// a run with no job allow every call, and an enforcing job allows nothing it
// has no matching record of.
func TestRunAllowsToolCallJudgesTheRecordedCall(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	specialist := enqueueLocalTurn(t, ctx, repo, "Specialist")
	specialistPayload(t, ctx, repo, specialist.JobID, `["files/files.read","files/files.update"]`)
	ordinary := enqueueLocalTurn(t, ctx, repo, "Ordinary")
	now := time.Now()
	insertTelemetryToolCall(t, ctx, repo, "call_in_set", specialist.RunID, "files", "files.read", "completed", nil, now)
	insertTelemetryToolCall(t, ctx, repo, "call_other_server", specialist.RunID, "vendor", "files.read", "completed", nil, now)
	insertTelemetryToolCall(t, ctx, repo, "call_out_of_set", specialist.RunID, "system", "system.time", "completed", nil, now)
	insertTelemetryToolCall(t, ctx, repo, "call_ordinary", ordinary.RunID, "files", "files.read", "completed", nil, now)

	for _, test := range []struct {
		name, runID, toolCallID, toolName string
		allowed                           bool
	}{
		{"in the set", specialist.RunID, "call_in_set", "files.read", true},
		{"same tool on another server", specialist.RunID, "call_other_server", "files.read", false},
		{"outside the set", specialist.RunID, "call_out_of_set", "system.time", false},
		{"a tool its record does not name", specialist.RunID, "call_in_set", "files.update", false},
		{"no record of the call", specialist.RunID, "call_unrecorded", "files.read", false},
		{"another run's in-set call", specialist.RunID, "call_ordinary", "files.read", false},
		{"an ordinary run", ordinary.RunID, "call_unrecorded", "system.time", true},
		{"a run with no job", "run_missing", "call_unrecorded", "system.time", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			allowed, err := repo.RunAllowsToolCall(ctx, test.runID, test.toolCallID, test.toolName)
			if err != nil || allowed != test.allowed {
				t.Fatalf("RunAllowsToolCall = %v, %v; want %v", allowed, err, test.allowed)
			}
		})
	}
}
