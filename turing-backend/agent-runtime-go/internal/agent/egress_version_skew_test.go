package agent

import (
	"context"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// What happens to a run whose consent was frozen under an older decision
// version, and why nothing here tries to rescue it.
//
// Round 8 raised the queued-v2 case: a job enqueued before the memory bump
// keeps its decision through migration 0019, and after the upgrade no worker
// will execute it. That is the specified outcome, not an oversight, and it is
// pinned here so it stays a decision rather than becoming an accident.
//
// The plan's acceptance item reads "version skew fails closed at dispatch —
// the literal pre-bump number". The migration deliberately keeps such a
// decision exactly as it was recorded, with an empty memory snapshot
// fingerprint, because a consent given before memory existed disclosed no
// memory and must never be retroactively credited with any. So the two
// alternatives to failing closed are both worse than the failure:
//
//   - Executing it would run the job against a disclosure the user never saw.
//     The decision they agreed to said nothing about their persona or profile,
//     and this build's run would carry both.
//   - Rewriting it to the current version would forge the same consent with a
//     signature this server applied on the user's behalf.
//
// A terminal, typed refusal is the honest third answer: the run ends, it says
// which gate ended it, and it is not retried, so the person is told rather than
// left watching something retry forever. Re-sending the message is a consent
// they actually give.
func v2QueuedJob(t *testing.T) *turingv1.AgentJob {
	t.Helper()
	skillFingerprint, err := backendegress.SkillSnapshotFingerprint(nil)
	if err != nil {
		t.Fatal(err)
	}
	job := &turingv1.AgentJob{
		RunId:         "run_queued_before_the_bump",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model:         "local-model",
		SelectedTools: []string{"vendor/vendor.lookup"},
		EgressDecision: &turingv1.RunEgressDecision{
			DecisionId: "egress_queued_before_the_bump",
			// The literal pre-bump number, never the constant minus one: this
			// has to keep failing closed after the constant moves again, and a
			// relative expression would follow it and pass forever.
			Version:  2,
			Provider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
			Model:    "local-model",
			DataCategories: []turingv1.EgressDataCategory{
				turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
				turingv1.EgressDataCategory_EGRESS_DATA_CATEGORY_TOOL_RESULTS,
			},
			ConsentGrantedAt:         timestamppb.Now(),
			ChallengeFingerprint:     "fingerprint",
			SelectedTools:            []string{"vendor/vendor.lookup"},
			SkillSnapshotFingerprint: skillFingerprint,
			RequestDigest:            "digest",
			RemoteMcpServers: []*turingv1.RemoteMcpEgressDestination{{
				ServerName: "vendor", Endpoint: "https://vendor.example/mcp", EndpointHost: "vendor.example",
			}},
		},
	}
	bindRuntimeMemory(job)
	return job
}

func TestADecisionFrozenUnderThePreviousVersionIsRefused(t *testing.T) {
	if err := validateEgressDecisionShape(v2QueuedJob(t)); err == nil {
		t.Fatal("a decision frozen under the previous version was accepted; the run would carry memory nobody consented to")
	}
	// The same job at the current version is accepted, so the refusal above is
	// about the version and not about the rest of the shape.
	current := v2QueuedJob(t)
	current.EgressDecision.Version = int32(backendegress.DecisionVersion)
	if err := validateEgressDecisionShape(current); err != nil {
		t.Fatalf("the same job at the current version was refused: %v", err)
	}
}

// And the refusal is terminal and named. A run that ends here says which gate
// ended it and is never retried automatically: nothing about it will be
// different in thirty seconds, and the way out is the person sending the
// message again under a consent they actually give.
func TestARefusedPreviousVersionEndsTheRunWithoutRetrying(t *testing.T) {
	job := v2QueuedJob(t)
	var updates []*turingv1.RuntimeUpdate
	assistant := &GeneralAssistant{}
	if err := assistant.Execute(context.Background(), job, func(update *turingv1.RuntimeUpdate) error {
		updates = append(updates, update)
		return nil
	}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("updates=%d, want exactly the failure; nothing else may happen on this run", len(updates))
	}
	failed := updates[0].GetRunFailed()
	if failed == nil {
		t.Fatalf("update=%+v, want a run failure", updates[0])
	}
	if failed.GetCode() != "egress_decision_invalid" {
		t.Fatalf("code=%q, want the gate that refused it named", failed.GetCode())
	}
	if failed.GetFailureOrigin() != turingv1.FailureOrigin_FAILURE_ORIGIN_TOOL_POLICY {
		t.Fatalf("origin=%v, want the policy gate", failed.GetFailureOrigin())
	}
	if failed.GetAutomaticRetryClass() != turingv1.AutomaticRetryClass_AUTOMATIC_RETRY_CLASS_NEVER {
		t.Fatalf("retry class=%v, want never: no retry can make an old consent current", failed.GetAutomaticRetryClass())
	}
}
