package repository

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestClaimNextCompatibleJobSkipsUnsupportedPendingWork(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()

	unsupportedSession, err := repo.CreateSession(ctx, "Unsupported")
	if err != nil {
		t.Fatal(err)
	}

	unsupported, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:             unsupportedSession.SessionID,
		Content:               "needs files",
		AgentID:               "general_assistant",
		ModelProvider:         "ollama",
		Model:                 "llama3.2",
		RequestedTools:        []string{"files/files.read"},
		RequiredContextTokens: 8192,
	})
	if err != nil {
		t.Fatal(err)
	}

	supportedSession, err := repo.CreateSession(ctx, "Supported")
	if err != nil {
		t.Fatal(err)
	}
	supported, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:                      supportedSession.SessionID,
		Content:                        "tell the time",
		AgentID:                        "general_assistant",
		ModelProvider:                  "ollama",
		Model:                          "llama3.2",
		RequestedTools:                 []string{"system/system.time"},
		RequiredContextTokens:          4096,
		MinimumWorkerMaxConcurrentRuns: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	compatibilityChecks := 0
	claimed, err := repo.ClaimNextCompatibleJobWithLimit(
		ctx,
		"general_assistant",
		"worker-system",
		0,
		time.Hour,
		&WorkerRoutingCapabilities{
			Models: []RoutingModelCapability{{
				Provider: "ollama", Model: "llama3.2", MaxContextTokens: 4096,
			}},
			Tools: []string{"system/system.time"}, MaxConcurrentRuns: 2,
		},
		func(route RoutingRequirements) bool {
			compatibilityChecks++
			return route.ModelProvider == "ollama" &&
				route.Model == "llama3.2" &&
				len(route.RequestedTools) == 1 &&
				route.RequestedTools[0] == "system/system.time" &&
				route.RequiredContextTokens == 4096 &&
				route.MinimumWorkerMaxConcurrentRuns == 2 &&
				!route.ExternalAgent
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.JobID != supported.JobID {
		t.Fatalf("claimed job = %q, want compatible %q; unsupported was %q", claimed.JobID, supported.JobID, unsupported.JobID)
	}
	if compatibilityChecks != 1 {
		t.Fatalf("compatibility checks = %d, want SQL routing filter to return only the compatible candidate", compatibilityChecks)
	}
	if len(claimed.RequestedTools) != 1 || claimed.RequestedTools[0] != "system/system.time" ||
		claimed.RequiredContextTokens != 4096 || claimed.MinimumWorkerMaxConcurrentRuns != 2 {
		t.Fatalf("claimed routing requirements = %+v", claimed)
	}

	var unsupportedStatus string
	if err := repo.db.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, unsupported.JobID).Scan(&unsupportedStatus); err != nil {
		t.Fatal(err)
	}
	if unsupportedStatus != "pending" {
		t.Fatalf("unsupported job status = %q, want pending", unsupportedStatus)
	}
}

func TestIncompatibleRemoteJobDoesNotStarveLaterLocalJob(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	remoteSession, err := repo.CreateSession(ctx, "Remote first")
	if err != nil {
		t.Fatal(err)
	}
	decision := remoteDecision()
	if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: remoteSession.SessionID, Content: "remote", AgentID: "general_assistant",
		ModelProvider: "openai_compatible", Model: decision.Model,
		EgressDecision: decision, SelectedTools: decision.SelectedTools,
	}); err != nil {
		t.Fatal(err)
	}
	localSession, err := repo.CreateSession(ctx, "Local second")
	if err != nil {
		t.Fatal(err)
	}
	local, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: localSession.SessionID, Content: "local", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimNextCompatibleJobWithLimit(
		ctx, "general_assistant", "local-worker", 0, time.Hour,
		&WorkerRoutingCapabilities{
			Models: []RoutingModelCapability{{
				Provider: "ollama", Model: "llama3.2", MaxContextTokens: 8192,
			}},
			MaxConcurrentRuns: 1,
		},
		func(route RoutingRequirements) bool { return route.ModelProvider == "ollama" },
	)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.JobID != local.JobID {
		t.Fatalf("claimed job = %q, want later compatible local job %q", claimed.JobID, local.JobID)
	}
}

func TestWorkerAdvertisingLiteralV1CannotClaimPostBumpRemoteDecision(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Post-bump remote")
	if err != nil {
		t.Fatal(err)
	}
	decision := remoteDecision()
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "remote", AgentID: "general_assistant",
		ModelProvider: "openai_compatible", Model: decision.Model,
		EgressDecision: decision, SelectedTools: decision.SelectedTools,
	})
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := repo.ClaimNextCompatibleJobWithLimit(
		ctx, "general_assistant", "stale-v1-worker", 0, time.Hour,
		&WorkerRoutingCapabilities{
			Models: []RoutingModelCapability{{
				Provider: "openai_compatible", Model: decision.Model, MaxContextTokens: 8192,
			}},
			Tools: decision.SelectedTools, MaxConcurrentRuns: 1,
			RemoteEgressDecisionVersion: 1,
		},
		func(RoutingRequirements) bool { return true },
	)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.JobID != "" {
		t.Fatalf("literal-v1 worker claimed post-bump job %q; queued %q", claimed.JobID, enqueued.JobID)
	}
}

func TestWorkerAdvertisingLiteralV2CannotClaimPostBumpRemoteDecision(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "Post-memory-bump remote")
	if err != nil {
		t.Fatal(err)
	}
	decision := remoteDecision()
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "remote", AgentID: "general_assistant",
		ModelProvider: "openai_compatible", Model: decision.Model,
		EgressDecision: decision, SelectedTools: decision.SelectedTools,
	})
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := repo.ClaimNextCompatibleJobWithLimit(
		ctx, "general_assistant", "stale-v2-worker", 0, time.Hour,
		&WorkerRoutingCapabilities{
			Models: []RoutingModelCapability{{
				Provider: "openai_compatible", Model: decision.Model, MaxContextTokens: 8192,
			}},
			Tools: decision.SelectedTools, MaxConcurrentRuns: 1,
			// Deliberately the literal pre-bump number, not the constant: a
			// worker built before memory existed in the decision cannot be
			// trusted to honour a memory-bearing one, and the check has to fail
			// closed even after the constant moves again.
			RemoteEgressDecisionVersion: 2,
		},
		func(RoutingRequirements) bool { return true },
	)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.JobID != "" {
		t.Fatalf("literal-v2 worker claimed post-bump job %q; queued %q", claimed.JobID, enqueued.JobID)
	}
}

func TestListPendingRoutingWorkPageUsesStableKeyset(t *testing.T) {
	repo := New(openTestDB(t))
	ctx := context.Background()
	var want []string
	for _, title := range []string{"First", "Second", "Third"} {
		session, err := repo.CreateSession(ctx, title)
		if err != nil {
			t.Fatal(err)
		}

		enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
			SessionID: session.SessionID, Content: title, AgentID: "general_assistant",
			ModelProvider: "ollama", Model: "llama3.2",
		})
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, enqueued.RunID)
	}

	first, cursor, err := repo.ListPendingRoutingWorkPage(ctx, PendingRoutingCursor{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := repo.ListPendingRoutingWorkPage(ctx, cursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(first)+len(second))
	for _, item := range append(first, second...) {
		got = append(got, item.RunID)
	}
	if len(first) != 2 || len(second) != 1 || !slices.Equal(got, want) {
		t.Fatalf("paged run IDs = %v (%d/%d), want %v", got, len(first), len(second), want)
	}
}

func TestClaimExternalAgentJobRejectsPositiveContextWithoutGuarantee(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "External context")
	if err != nil {
		t.Fatal(err)
	}
	agent := mustCreateAgent(t, ctx, repo, anthropicAgent())
	if _, err := repo.SetSessionAgent(ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "external", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "ignored", RequiredContextTokens: 1,
		EgressDecision: testRemoteEgressDecision(t, agent.Model, agent.BaseURL, agent.AgentID, agent.CredentialRef),
	})
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := repo.ClaimNextCompatibleJobWithLimit(
		ctx,
		"general_assistant",
		"worker-external",
		0,
		time.Hour,
		&WorkerRoutingCapabilities{
			ExternalAgentCredentialRefs: []string{"claude"}, MaxConcurrentRuns: 1,
			RemoteEgressDecisionVersion: RunEgressDecisionVersion,
		},
		func(RoutingRequirements) bool { return true },
	)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.JobID != "" {
		t.Fatalf("claimed external job %q with unguaranteed context; enqueued %q", claimed.JobID, enqueued.JobID)
	}
}

func TestClaimExternalAgentJobRequiresExactCredentialRef(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	unsupportedSession, err := repo.CreateSession(ctx, "Unsupported external credential")
	if err != nil {
		t.Fatal(err)
	}
	claude := mustCreateAgent(t, ctx, repo, anthropicAgent())
	if _, err := repo.SetSessionAgent(ctx, unsupportedSession.SessionID, claude.AgentID); err != nil {
		t.Fatal(err)
	}
	unsupported, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: unsupportedSession.SessionID, Content: "claude", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "ignored",
		EgressDecision: testRemoteEgressDecision(t, claude.Model, claude.BaseURL, claude.AgentID, claude.CredentialRef),
	})
	if err != nil {
		t.Fatal(err)
	}

	supportedSession, err := repo.CreateSession(ctx, "Supported external credential")
	if err != nil {
		t.Fatal(err)
	}
	openAIInput := anthropicAgent()
	openAIInput.DisplayName = "OpenAI"
	openAIInput.Model = "gpt-4o-mini"
	openAIInput.CredentialRef = "openai"
	openAI := mustCreateAgent(t, ctx, repo, openAIInput)
	if _, err := repo.SetSessionAgent(ctx, supportedSession.SessionID, openAI.AgentID); err != nil {
		t.Fatal(err)
	}
	supported, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: supportedSession.SessionID, Content: "openai", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "ignored",
		EgressDecision: testRemoteEgressDecision(t, openAI.Model, openAI.BaseURL, openAI.AgentID, openAI.CredentialRef),
	})
	if err != nil {
		t.Fatal(err)
	}

	compatibilityChecks := 0
	claimed, err := repo.ClaimNextCompatibleJobWithLimit(
		ctx,
		"general_assistant",
		"worker-openai",
		0,
		time.Hour,
		&WorkerRoutingCapabilities{
			ExternalAgentCredentialRefs: []string{"openai"}, MaxConcurrentRuns: 1,
			RemoteEgressDecisionVersion: RunEgressDecisionVersion,
			Tools:                       []string{"files/files.read", "system/system.time"},
		},
		func(route RoutingRequirements) bool {
			compatibilityChecks++
			return route.ExternalAgent && route.ExternalAgentCredentialRef == "openai"
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.JobID != supported.JobID {
		t.Fatalf("claimed job = %q, want matching %q; mismatched was %q", claimed.JobID, supported.JobID, unsupported.JobID)
	}
	if compatibilityChecks != 1 {
		t.Fatalf("compatibility checks = %d, want SQL to return only the exact credential match", compatibilityChecks)
	}
	var unsupportedStatus string
	if err := repo.db.QueryRowContext(ctx, `SELECT status FROM jobs WHERE id = ?`, unsupported.JobID).Scan(&unsupportedStatus); err != nil {
		t.Fatal(err)
	}
	if unsupportedStatus != "pending" {
		t.Fatalf("mismatched credential job status = %q, want pending", unsupportedStatus)
	}
}
