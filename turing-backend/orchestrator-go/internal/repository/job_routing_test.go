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
