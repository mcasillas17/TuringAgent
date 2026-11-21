package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var quietTestDefaults = repository.AutomationRunDefaults{AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2"}

func quietTestConfig(databasePath string) config.Config {
	return config.Config{
		ClientAPIKey: "client",
		RuntimeToken: "internal", ApprovalConsumerToken: "internal-approval-consumer",
		ApprovalJWTSecret: "approval-secret",
		DatabasePath:      databasePath,
		LocalModel:        "llama3.2",
		// The reaper off: the drain must not depend on it.
		JobReaperIntervalMS: 0,
	}
}

func clientContext() context.Context {
	return metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer client"))
}

// fireDueAutomation creates an automation and fires it once, leaving its
// unattended run queued.
func fireDueAutomation(t *testing.T, repo *repository.Repository, name string) repository.AutomationFire {
	t.Helper()
	automation := createDueAutomation(t, repo, name)
	due, err := time.Parse(time.RFC3339Nano, automation.NextDueAt)
	if err != nil {
		t.Fatal(err)
	}
	fire, found, err := repo.ClaimDueAutomation(context.Background(), due, quietTestDefaults)
	if err != nil || !found || fire.RunID == "" {
		t.Fatalf("fire %s = %+v, found %v, %v", name, fire, found, err)
	}
	return fire
}

func createDueAutomation(t *testing.T, repo *repository.Repository, name string) repository.Automation {
	t.Helper()
	automation, err := repo.CreateAutomation(context.Background(), repository.AutomationInput{
		Name: name, Prompt: "Summarise the sandbox.", Enabled: true,
		Schedule: repository.Schedule{Kind: repository.ScheduleInterval, Interval: 5 * time.Minute},
	})
	if err != nil {
		t.Fatal(err)
	}
	return automation
}

func (a *App) quietCancelled(t *testing.T) int {
	t.Helper()
	return a.countRows(t, `SELECT COUNT(*) FROM agent_runs WHERE status = 'cancelled' AND cancellation_reason = ?`, runoutcome.CodeOwnerQuiet)
}

// TUR-028a: the quiet switch is the owner's. It sits behind the client key on
// the public server only; the runtime and every other internal identity are
// refused, so no tool or run can turn it off.
func TestTheQuietSwitchIsAuthenticatedAndPublicOnly(t *testing.T) {
	app := newTestApp(t)
	if _, ok := app.InternalServer.GetServiceInfo()["turing.v1.AutomationService"]; ok {
		t.Fatal("internal server exposes turing.v1.AutomationService")
	}
	bearer := func(token string) context.Context {
		return metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	}
	public := turingv1.NewAutomationServiceClient(newBufconnClient(t, app.PublicServer))
	internal := turingv1.NewAutomationServiceClient(newBufconnClient(t, app.InternalServer))
	calls := map[string]func(turingv1.AutomationServiceClient, context.Context) error{
		"GetUnattendedQuiet": func(client turingv1.AutomationServiceClient, ctx context.Context) error {
			_, err := client.GetUnattendedQuiet(ctx, &turingv1.GetUnattendedQuietRequest{})
			return err
		},
		"SetUnattendedQuiet": func(client turingv1.AutomationServiceClient, ctx context.Context) error {
			_, err := client.SetUnattendedQuiet(ctx, &turingv1.SetUnattendedQuietRequest{Quiet: false, Revision: 0})
			return err
		},
	}
	for name, call := range calls {
		if err := call(public, context.Background()); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("%s without a key = %v, want Unauthenticated", name, err)
		}
		for _, token := range []string{"internal", "internal-approval-consumer"} {
			if err := call(public, bearer(token)); status.Code(err) != codes.Unauthenticated {
				t.Fatalf("%s with internal token %q on the public server = %v, want Unauthenticated", name, token, err)
			}
			if err := call(internal, bearer(token)); status.Code(err) != codes.Unimplemented {
				t.Fatalf("%s with internal token %q on the internal server = %v, want Unimplemented", name, token, err)
			}
		}
		if err := call(public, bearer("client")); err != nil {
			t.Fatalf("%s with the client key = %v", name, err)
		}
	}
}

// Turning quiet on drains the queued automation runs at once, on the
// maintenance goroutine, with the job reaper off and no scheduled tick.
func TestTurningQuietOnDrainsAtOnceWithTheReaperOff(t *testing.T) {
	controlMaintenanceTicks(t)
	app, err := New(quietTestConfig(filepath.Join(t.TempDir(), "turing.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Stop)
	for _, name := range []string{"Digest", "Inbox", "Calendar"} {
		fireDueAutomation(t, app.Repository, name)
	}
	automations := turingv1.NewAutomationServiceClient(newBufconnClient(t, app.PublicServer))
	response, err := automations.SetUnattendedQuiet(clientContext(), &turingv1.SetUnattendedQuietRequest{Quiet: true})
	if err != nil || response.GetStale() || !response.GetSetting().GetQuiet() || response.GetSetting().GetRevision() != 1 {
		t.Fatalf("turn quiet on = %v, %v", response, err)
	}
	waitFor(t, "the queued automation runs to be cancelled owner_quiet", func() bool { return app.quietCancelled(t) == 3 })
	waitFor(t, "the drain to clear", func() bool {
		return app.countRows(t, `SELECT COUNT(*) FROM settings WHERE key = 'unattended_quiet_drain'`) == 0
	})
}

// A crash after turning quiet on and before its drain leaves the fence in
// place: after a restart with the reaper off, nothing unattended is claimed,
// and the next maintenance tick finishes the drain. Turning quiet off then
// lets an automation that came due meanwhile fire once.
func TestADrainCutShortByACrashIsFinishedByTheNextMaintenanceTick(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turing.db")
	ticks := controlMaintenanceTicks(t)
	before, err := New(quietTestConfig(path))
	if err != nil {
		t.Fatal(err)
	}
	var queued []string
	for _, name := range []string{"Digest", "Inbox"} {
		queued = append(queued, fireDueAutomation(t, before.Repository, name).RunID)
	}
	due := createDueAutomation(t, before.Repository, "Morning")
	// The switch commits, and the process dies before its service drains.
	if _, _, err := before.Repository.SetUnattendedQuiet(context.Background(), true, 0); err != nil {
		t.Fatal(err)
	}
	before.Stop()

	app, err := New(quietTestConfig(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Stop)
	if claimed, err := app.Repository.ClaimNextCompatibleJobWithLimit(context.Background(), "general_assistant", "worker-1",
		0, time.Hour, nil, nil); err != nil || claimed.RunID != "" {
		t.Fatalf("claim after the restart = %+v, %v; want nothing while the fence holds", claimed, err)
	}
	if got := app.quietCancelled(t); got != 0 {
		t.Fatalf("cancelled before any tick = %d, want the drain still pending", got)
	}
	ticks.ticks <- time.Now()
	waitFor(t, "the next tick to finish the drain", func() bool {
		return app.quietCancelled(t) == len(queued) &&
			app.countRows(t, `SELECT COUNT(*) FROM settings WHERE key = 'unattended_quiet_drain'`) == 0
	})

	automations := turingv1.NewAutomationServiceClient(newBufconnClient(t, app.PublicServer))
	if _, err := automations.SetUnattendedQuiet(clientContext(), &turingv1.SetUnattendedQuietRequest{Quiet: false, Revision: 1}); err != nil {
		t.Fatal(err)
	}
	at, err := time.Parse(time.RFC3339Nano, due.NextDueAt)
	if err != nil {
		t.Fatal(err)
	}
	fires := 0
	for range 3 {
		fire, found, err := app.Repository.ClaimDueAutomation(context.Background(), at.Add(time.Minute), quietTestDefaults)
		if err != nil {
			t.Fatal(err)
		}
		if found && fire.AutomationID == due.AutomationID && fire.RunID != "" {
			fires++
		}
	}
	if fires != 1 {
		t.Fatalf("the automation due during quiet fired %d times after it, want once", fires)
	}
}
