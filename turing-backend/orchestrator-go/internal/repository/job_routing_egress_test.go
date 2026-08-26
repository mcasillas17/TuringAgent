package repository

import (
	"context"
	"testing"
	"time"

	backendegress "github.com/mcasillas17/TuringAgent/turing-backend/internal/egress"
)

// localModelRemoteToolDecision is a run that never leaves the machine for its
// model and does leave it for a tool: an Ollama model calling a remote MCP
// server. It carries a full egress decision, and the worker that executes it
// has to be able to validate one.
func localModelRemoteToolDecision(t *testing.T) *PendingEgressDecision {
	t.Helper()
	skillFingerprint, err := backendegress.SkillSnapshotFingerprint(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &PendingEgressDecision{
		Version:              RunEgressDecisionVersion,
		ChallengeNonce:       "nonce_local_remote_tool",
		ChallengeFingerprint: "fingerprint_local_remote_tool",
		RequestDigest:        "digest_local_remote_tool",
		Provider:             "ollama",
		Model:                "local",
		DataCategories: []string{
			"EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS",
			"EGRESS_DATA_CATEGORY_TOOL_RESULTS",
		},
		SelectedTools:             []string{"vendor/vendor.lookup"},
		SkillSnapshotFingerprint:  skillFingerprint,
		MemorySnapshotFingerprint: vaultlessMemoryFingerprint("vendor/vendor.lookup"),
		ConsentGrantedAt:          "2026-08-21T00:00:00Z",
		RemoteMCPServers: []RemoteMCPServerEgress{{
			ServerName: "vendor", Endpoint: "https://vendor.example/mcp", EndpointHost: "vendor.example",
		}},
	}
}

func enqueueLocalModelRemoteToolRun(t *testing.T, repo *Repository, ctx context.Context, title string) EnqueueUserMessageResult {
	t.Helper()
	session, err := repo.CreateSession(ctx, title)
	if err != nil {
		t.Fatal(err)
	}
	decision := localModelRemoteToolDecision(t)
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "look up", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: decision.Model,
		SelectedTools: decision.SelectedTools, EgressDecision: decision,
	})
	if err != nil {
		t.Fatal(err)
	}
	return enqueued
}

// The version gate is about the decision, not about the model's destination.
// A worker that cannot validate a decision cannot be handed one, and a local
// model calling a remote tool carries exactly the same decision a remote model
// does — the tool arguments and results are leaving the machine either way.
//
// Gating only on the provider left the pre-memory worker claiming these runs
// and failing them at execution time, which is a terminal runtime failure the
// user sees instead of a job that simply waits for a worker that can run it.
func TestWorkerAdvertisingLiteralV2CannotClaimLocalModelRemoteToolDecision(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	enqueued := enqueueLocalModelRemoteToolRun(t, repo, ctx, "Local model, remote tool")

	claimed, err := repo.ClaimNextCompatibleJobWithLimit(
		ctx, "general_assistant", "stale-v2-worker", 0, time.Hour,
		&WorkerRoutingCapabilities{
			Models: []RoutingModelCapability{{
				Provider: "ollama", Model: "local", MaxContextTokens: 8192,
			}},
			Tools: []string{"vendor/vendor.lookup"}, MaxConcurrentRuns: 1,
			// The literal pre-memory number, not the constant: a worker built
			// before memory joined the decision cannot be trusted with one,
			// and this has to keep failing closed after the constant moves.
			RemoteEgressDecisionVersion: 2,
		},
		func(RoutingRequirements) bool { return true },
	)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.JobID != "" {
		t.Fatalf("literal-v2 worker claimed egress-bearing local job %q; queued %q", claimed.JobID, enqueued.JobID)
	}
}

func TestWorkerAdvertisingCurrentVersionClaimsLocalModelRemoteToolDecision(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	enqueued := enqueueLocalModelRemoteToolRun(t, repo, ctx, "Local model, remote tool, current worker")

	claimed, err := repo.ClaimNextCompatibleJobWithLimit(
		ctx, "general_assistant", "current-worker", 0, time.Hour,
		&WorkerRoutingCapabilities{
			Models: []RoutingModelCapability{{
				Provider: "ollama", Model: "local", MaxContextTokens: 8192,
			}},
			Tools: []string{"vendor/vendor.lookup"}, MaxConcurrentRuns: 1,
			RemoteEgressDecisionVersion: RunEgressDecisionVersion,
		},
		func(RoutingRequirements) bool { return true },
	)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.JobID != enqueued.JobID {
		t.Fatalf("current worker claimed %q, want the egress-bearing local job %q", claimed.JobID, enqueued.JobID)
	}
}

// The claim filter and the routing filter answer the same question, so the
// requirements the routing side is judged on have to say the job carries a
// decision at all. Without it the two disagree: the SQL refuses the job and
// routing reports a worker that could run it.
func TestPendingRoutingWorkReportsThatALocalRunCarriesAnEgressDecision(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	enqueueLocalModelRemoteToolRun(t, repo, ctx, "Routing sees the decision")

	work, _, err := repo.ListPendingRoutingWorkPage(ctx, PendingRoutingCursor{}, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(work) != 1 {
		t.Fatalf("pending routing work = %d rows, want 1", len(work))
	}
	if !work[0].Requirements.RemoteEgressDecision {
		t.Fatal("a run carrying an egress decision was reported as needing no egress-aware worker")
	}
}

func TestPendingRoutingWorkReportsNoEgressDecisionForAPurelyLocalRun(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Purely local")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "hello", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "local",
	}); err != nil {
		t.Fatal(err)
	}

	work, _, err := repo.ListPendingRoutingWorkPage(ctx, PendingRoutingCursor{}, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(work) != 1 {
		t.Fatalf("pending routing work = %d rows, want 1", len(work))
	}
	if work[0].Requirements.RemoteEgressDecision {
		t.Fatal("a run that never leaves the machine was reported as needing an egress-aware worker")
	}
}
