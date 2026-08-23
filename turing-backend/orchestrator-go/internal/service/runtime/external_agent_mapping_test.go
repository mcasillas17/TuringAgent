package runtime

import (
	"reflect"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
)

// mapJob is the only place the destination crosses from storage into the
// message the worker receives. Without a test here, dropping one line would
// silently answer every routed conversation locally while the rest of the
// suite stays green — which is the worst possible failure for this feature,
// because the user was told their message went elsewhere.
func TestMapJobCarriesTheRoutedAgentToTheWorker(t *testing.T) {
	job := mapJob(repository.Job{
		JobID:     "job_1",
		RunID:     "run_1",
		SessionID: "sess_1",
		UserText:  "hello",
		ExternalAgent: &repository.ExternalAgentTarget{
			AgentID:       "agent_claude",
			DisplayName:   "Claude",
			BaseURL:       "https://api.anthropic.com/v1",
			CredentialRef: "claude",
		},
		EgressDecision: &repository.RunEgressDecision{
			DecisionID: "egress_1", Version: repository.RunEgressDecisionVersion, Provider: "openai_compatible",
			Model: "claude-sonnet-4", Endpoint: "https://api.anthropic.com/v1",
			EndpointHost: "api.anthropic.com",
			DataCategories: []string{
				"EGRESS_DATA_CATEGORY_CURRENT_MESSAGE",
				"EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY",
			},
			SelectedTools:             []string{"system/system.time"},
			ConsentGrantedAt:          "2026-08-20T01:02:03.000000000Z",
			ChallengeFingerprint:      "fingerprint_1",
			RequestDigest:             "request_digest_1",
			ExternalCredentialRefHash: "credential-ref-hash",
		},
		SelectedTools: []string{"system/system.time"},
	})

	target := job.GetExternalAgent()
	if target == nil {
		t.Fatal("external agent = nil, want the routed destination")
	}
	if target.GetDisplayName() != "Claude" ||
		target.GetAgentId() != "agent_claude" ||
		target.GetBaseUrl() != "https://api.anthropic.com/v1" ||
		target.GetCredentialRef() != "claude" {
		t.Fatalf("target = %+v, want the routed agent verbatim", target)
	}
	decision := job.GetEgressDecision()
	if decision.GetDecisionId() != "egress_1" ||
		decision.GetProvider() != turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE ||
		decision.GetEndpoint() != "https://api.anthropic.com/v1" ||
		!reflect.DeepEqual(job.GetSelectedTools(), []string{"system/system.time"}) {
		t.Fatalf("egress mapping = decision %+v tools %v", decision, job.GetSelectedTools())
	}
}

// nil must stay nil. An empty target would look like a routed run pointing at
// no endpoint, which fails worse than an unrouted one.
func TestMapJobSendsNoAgentForAnUnroutedConversation(t *testing.T) {
	job := mapJob(repository.Job{JobID: "job_1", UserText: "hello"})

	if job.GetExternalAgent() != nil {
		t.Fatalf("external agent = %+v, want nil", job.GetExternalAgent())
	}
}
