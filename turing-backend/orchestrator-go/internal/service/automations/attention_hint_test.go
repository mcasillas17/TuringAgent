package automations

import (
	"sync"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
)

type countingHints struct {
	mu    sync.Mutex
	count int
}

func (h *countingHints) PublishAttentionChanged() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.count++
}

func (h *countingHints) take() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	count := h.count
	h.count = 0
	return count
}

// UX-002a: whether an automation is listed as blocked changes when it is
// enabled, disabled, edited or deleted, none of which publishes an event, so
// each hints the Needs-you queue. A refused change hints nothing.
func TestAutomationChangesHintAttention(t *testing.T) {
	server, _, ctx := newTestServer(t)
	hints := &countingHints{}
	server.SetAttentionHints(hints)
	created, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
		Name: "Digest", Prompt: "x", Schedule: everyFiveMinutes(), Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := hints.take(); got != 0 {
		t.Fatalf("hints after creating = %d, want 0: a new automation has no occurrence", got)
	}
	for _, step := range []struct {
		name   string
		change func() error
	}{
		{"disable", func() error {
			_, err := server.SetAutomationEnabled(ctx, &turingv1.SetAutomationEnabledRequest{AutomationId: created.GetAutomationId()})
			return err
		}},
		{"edit", func() error {
			_, err := server.UpdateAutomation(ctx, &turingv1.UpdateAutomationRequest{
				AutomationId: created.GetAutomationId(), Name: "Renamed", Prompt: "x", Schedule: everyFiveMinutes(),
			})
			return err
		}},
		{"delete", func() error {
			_, err := server.DeleteAutomation(ctx, &turingv1.DeleteAutomationRequest{AutomationId: created.GetAutomationId()})
			return err
		}},
	} {
		if err := step.change(); err != nil {
			t.Fatalf("%s: %v", step.name, err)
		}
		if got := hints.take(); got != 1 {
			t.Fatalf("hints after %s = %d, want 1", step.name, got)
		}
	}
	if _, err := server.DeleteAutomation(ctx, &turingv1.DeleteAutomationRequest{AutomationId: created.GetAutomationId()}); err == nil {
		t.Fatal("deleting twice succeeded")
	}
	if got := hints.take(); got != 0 {
		t.Fatalf("hints after a refused change = %d, want 0", got)
	}
}

func schedulerAttentionHints(updates <-chan events.Event) int {
	hints := 0
	for {
		select {
		case event := <-updates:
			if event.Type == events.AttentionChangedType {
				hints++
			}
		case <-time.After(100 * time.Millisecond):
			return hints
		}
	}
}

// A fire changes the Needs-you queue: a remote-egress block, which writes only
// an audit record, raises a row, and a queued run takes the previous
// failure's place. An occurrence passed by while the last run is still going
// changes nothing.
func TestTickHintsAttentionForEachOccurrenceItRecords(t *testing.T) {
	h := newSchedulerHarness(t)
	updates, unsubscribe := h.bus.SubscribeSessionUpdates()
	defer unsubscribe()
	automation := createEnabled(t, h.repo, h.ctx, "Digest", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	due := parseTime(t, automation.NextDueAt)
	h.scheduler.now = func() time.Time { return due }
	if err := h.scheduler.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	if got := schedulerAttentionHints(updates); got < 1 {
		t.Fatal("a queued run did not hint")
	}
	h.scheduler.now = func() time.Time { return due.Add(5 * time.Minute) }
	if err := h.scheduler.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	if got := schedulerAttentionHints(updates); got != 0 {
		t.Fatalf("hints for an occurrence passed by = %d, want 0", got)
	}

	blocked := createEnabled(t, h.repo, h.ctx, "External digest", repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute})
	session, err := h.repo.CreateSession(h.ctx, blocked.Name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.database.ExecContext(h.ctx, `UPDATE automations SET session_id = ? WHERE id = ?`,
		session.SessionID, blocked.AutomationID); err != nil {
		t.Fatal(err)
	}
	agent, err := h.repo.CreateExternalAgent(h.ctx, repository.ExternalAgentInput{
		DisplayName: "External", Provider: "anthropic", BaseURL: "https://example.com",
		Model: "external-model", CredentialRef: "external",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.SetSessionAgent(h.ctx, session.SessionID, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	h.scheduler.now = func() time.Time { return parseTime(t, blocked.NextDueAt) }
	if err := h.scheduler.Tick(h.ctx); err != nil {
		t.Fatal(err)
	}
	if got := schedulerAttentionHints(updates); got != 1 {
		t.Fatalf("hints for a remote-egress block = %d, want 1", got)
	}
}
