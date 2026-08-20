package app

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/metadata"
)

// The scheduler is the only thing in this process that creates a run nobody
// asked for, so the wiring that starts it — and the shutdown that stops it —
// is worth a test of its own.
func TestAppSchedulerFiresADueAutomationAndStops(t *testing.T) {
	cfg := config.Config{
		ClientAPIKey: "client", RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer", ApprovalJWTSecret: "approval-secret",
		DatabasePath: t.TempDir() + "/turing.db", AutomationTickMS: 5, OllamaModel: "qwen2.5:7b",
	}
	application, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			application.Stop()
		}
	}()
	conn := newBufconnClient(t, application.InternalServer)
	runtimeClient := turingv1.NewRuntimeServiceClient(conn)
	worker, err := runtimeClient.ConnectWorker(metadata.NewOutgoingContext(
		context.Background(), metadata.Pairs("authorization", "Bearer internal"),
	))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = worker.CloseSend() }()
	if err := worker.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{
		WorkerReady: &turingv1.RuntimeWorkerReady{
			WorkerId: "worker-automation", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1,
		},
	}}); err != nil {
		t.Fatal(err)
	}
	recvRuntimeCommand(t, worker, func(command *turingv1.RuntimeCommand) bool {
		return command.GetWorkerAccepted() != nil
	})
	if got := application.RuntimeService.RoutableDefaultModel("ollama", cfg.OllamaModel); got != cfg.OllamaModel {
		t.Fatalf("live default model = %q, want %q; providers = %+v", got, cfg.OllamaModel, application.RuntimeService.ProviderCapabilities())
	}
	if err := application.RuntimeService.ValidateRouting(context.Background(), repository.RoutingRequirements{
		AgentID: "general_assistant", ModelProvider: "ollama", Model: cfg.OllamaModel, MinimumWorkerMaxConcurrentRuns: 1,
	}); err != nil {
		t.Fatalf("worker accepted without becoming routable: %v", err)
	}
	ctx := context.Background()
	automation := createEnabledAutomation(t, application, "Digest")
	makeAutomationDue(t, cfg.DatabasePath, automation.AutomationID)

	deadline := time.Now().Add(10 * time.Second)
	var fired repository.Automation
	for time.Now().Before(deadline) {
		fired, err = application.Repository.GetAutomation(ctx, automation.AutomationID)
		if err != nil {
			t.Fatal(err)
		}
		if fired.LastRunID != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if fired.LastRunID == "" {
		t.Fatal("the scheduler never fired a due automation")
	}
	if fired.SessionID == "" {
		t.Fatal("the fired automation has no conversation to read")
	}

	// Stop has to join the scheduler goroutine, not merely signal it: a
	// shutdown that returns while it is still ticking can queue a run on its
	// way out, after the database is closed.
	done := make(chan struct{})
	go func() {
		defer close(done)
		application.Stop()
	}()
	select {
	case <-done:
		stopped = true
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not return while the scheduler was running")
	}
	select {
	case <-application.schedulerDone:
	default:
		t.Fatal("Stop returned without joining the scheduler")
	}
}

// A zero tick means the scheduler never runs, which is the only honest way to
// say "create no work on this machine". Stop must still return.
func TestAppWithNoAutomationTickNeverFiresAndStillStops(t *testing.T) {
	cfg := config.Config{
		ClientAPIKey: "client", RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer", ApprovalJWTSecret: "approval-secret",
		DatabasePath: t.TempDir() + "/turing.db", AutomationTickMS: 0, OllamaModel: "qwen2.5:7b",
	}
	application, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	automation := createEnabledAutomation(t, application, "Digest")
	makeAutomationDue(t, cfg.DatabasePath, automation.AutomationID)

	time.Sleep(100 * time.Millisecond)
	unfired, err := application.Repository.GetAutomation(context.Background(), automation.AutomationID)
	if err != nil {
		t.Fatal(err)
	}
	if unfired.LastRunID != "" {
		t.Fatalf("a disabled scheduler fired run %q", unfired.LastRunID)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		application.Stop()
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop hung with the scheduler disabled")
	}
}

func createEnabledAutomation(t *testing.T, application *App, name string) repository.Automation {
	t.Helper()
	automation, err := application.Repository.CreateAutomation(context.Background(), repository.AutomationInput{
		Name: name, Prompt: "Summarise the sandbox.",
		Schedule: repository.Schedule{Kind: repository.ScheduleInterval, Interval: time.Minute},
		Enabled:  true,
	})
	if err != nil {
		t.Fatalf("create automation: %v", err)
	}
	return automation
}

// makeAutomationDue backdates next_due_at through a second handle on the same
// file, rather than adding a test-only method to the repository. Waiting for a
// real interval would make the test take a minute.
func makeAutomationDue(t *testing.T, databasePath string, automationID string) {
	t.Helper()
	database, err := db.Open(databasePath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() { _ = database.Close() }()
	due := repository.FormatTimestamp(time.Now().Add(-time.Second))
	// The app holds its own connection, so this write can lose a race for the
	// lock. Nothing is due yet, so retrying is enough.
	var lastErr error
	for range 50 {
		_, lastErr = database.ExecContext(context.Background(),
			`UPDATE automations SET next_due_at = ? WHERE id = ?`, due, automationID)
		if lastErr == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("backdate automation: %v", lastErr)
}
