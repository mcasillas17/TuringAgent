package runtime

import (
	"testing"

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
			DisplayName:   "Claude",
			BaseURL:       "https://api.anthropic.com/v1",
			CredentialRef: "claude",
		},
	})

	target := job.GetExternalAgent()
	if target == nil {
		t.Fatal("external agent = nil, want the routed destination")
	}
	if target.GetDisplayName() != "Claude" ||
		target.GetBaseUrl() != "https://api.anthropic.com/v1" ||
		target.GetCredentialRef() != "claude" {
		t.Fatalf("target = %+v, want the routed agent verbatim", target)
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
