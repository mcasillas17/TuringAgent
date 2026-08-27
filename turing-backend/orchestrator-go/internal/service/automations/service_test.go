package automations

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newTestServer(t *testing.T) (*Server, *repository.Repository, context.Context) {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "turing.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	if err := db.ApplyMigrations(ctx, database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	repo := repository.New(database)
	return New(repo), repo, ctx
}

func everyFiveMinutes() *turingv1.AutomationSchedule {
	return &turingv1.AutomationSchedule{
		Kind:            turingv1.AutomationScheduleKind_AUTOMATION_SCHEDULE_KIND_INTERVAL,
		IntervalMinutes: 5,
	}
}

func TestCreateAutomationRoundTripsEverythingTheClientSent(t *testing.T) {
	server, _, ctx := newTestServer(t)

	created, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
		Name:     "Morning digest",
		Prompt:   "Summarise the sandbox.",
		Schedule: everyFiveMinutes(),
		Enabled:  true,
		AllowedTools: []*turingv1.AutomationTool{
			{ServerName: "files", ToolName: "files.create"},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.GetName() != "Morning digest" || created.GetPrompt() != "Summarise the sandbox." {
		t.Fatalf("created = %+v", created)
	}
	if created.GetSchedule().GetKind() != turingv1.AutomationScheduleKind_AUTOMATION_SCHEDULE_KIND_INTERVAL ||
		created.GetSchedule().GetIntervalMinutes() != 5 {
		t.Fatalf("schedule = %+v", created.GetSchedule())
	}
	if !created.GetEnabled() || created.GetNextRunAt() == nil {
		t.Fatalf("enabled=%v nextRun=%v, want enabled with a next run", created.GetEnabled(), created.GetNextRunAt())
	}
	if len(created.GetAllowedTools()) != 1 || created.GetAllowedTools()[0].GetToolName() != "files.create" {
		t.Fatalf("allowed tools = %+v", created.GetAllowedTools())
	}
}

// A disabled automation has no next run, and reporting one would be a claim
// about the future that is false.
func TestDisabledAutomationsReportNoNextRun(t *testing.T) {
	server, _, ctx := newTestServer(t)

	created, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
		Name: "Digest", Prompt: "x", Schedule: everyFiveMinutes(), Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := server.SetAutomationEnabled(ctx, &turingv1.SetAutomationEnabledRequest{
		AutomationId: created.GetAutomationId(), Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if disabled.GetEnabled() || disabled.GetNextRunAt() != nil {
		t.Fatalf("disabled = enabled %v nextRun %v", disabled.GetEnabled(), disabled.GetNextRunAt())
	}
}

func TestAutomationErrorsMapToCodesAClientCanActOn(t *testing.T) {
	server, _, ctx := newTestServer(t)

	existing, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
		Name: "Digest", Prompt: "x", Schedule: everyFiveMinutes(),
	})
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		call func() error
		want codes.Code
	}{
		{"nil create request", func() error {
			_, err := server.CreateAutomation(ctx, nil)
			return err
		}, codes.InvalidArgument},
		{"nil update request", func() error {
			_, err := server.UpdateAutomation(ctx, nil)
			return err
		}, codes.InvalidArgument},
		{"nil enable request", func() error {
			_, err := server.SetAutomationEnabled(ctx, nil)
			return err
		}, codes.InvalidArgument},
		{"nil delete request", func() error {
			_, err := server.DeleteAutomation(ctx, nil)
			return err
		}, codes.InvalidArgument},
		{"duplicate name", func() error {
			_, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
				Name: "digest", Prompt: "y", Schedule: everyFiveMinutes(),
			})
			return err
		}, codes.AlreadyExists},
		{"empty name", func() error {
			_, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
				Name: "   ", Prompt: "y", Schedule: everyFiveMinutes(),
			})
			return err
		}, codes.InvalidArgument},
		{"empty prompt", func() error {
			_, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
				Name: "Nameless work", Prompt: "  ", Schedule: everyFiveMinutes(),
			})
			return err
		}, codes.InvalidArgument},
		{"oversized name", func() error {
			_, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
				Name: strings.Repeat("n", 500), Prompt: "y", Schedule: everyFiveMinutes(),
			})
			return err
		}, codes.InvalidArgument},
		{"oversized prompt", func() error {
			_, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
				Name: "Long", Prompt: strings.Repeat("p", 9000), Schedule: everyFiveMinutes(),
			})
			return err
		}, codes.InvalidArgument},
		{"no schedule", func() error {
			_, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{Name: "Unscheduled", Prompt: "y"})
			return err
		}, codes.InvalidArgument},
		{"sub-minute interval", func() error {
			_, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
				Name: "Too fast", Prompt: "y",
				Schedule: &turingv1.AutomationSchedule{Kind: turingv1.AutomationScheduleKind_AUTOMATION_SCHEDULE_KIND_INTERVAL},
			})
			return err
		}, codes.InvalidArgument},
		{"minute past the day", func() error {
			_, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
				Name: "Out of range", Prompt: "y",
				Schedule: &turingv1.AutomationSchedule{
					Kind: turingv1.AutomationScheduleKind_AUTOMATION_SCHEDULE_KIND_DAILY, DailyMinuteUtc: 1440,
				},
			})
			return err
		}, codes.InvalidArgument},
		{"half-named tool", func() error {
			_, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
				Name: "Half named", Prompt: "y", Schedule: everyFiveMinutes(),
				AllowedTools: []*turingv1.AutomationTool{{ServerName: "files"}},
			})
			return err
		}, codes.InvalidArgument},
		{"update a missing automation", func() error {
			_, err := server.UpdateAutomation(ctx, &turingv1.UpdateAutomationRequest{
				AutomationId: "auto_nope", Name: "n", Prompt: "p", Schedule: everyFiveMinutes(),
			})
			return err
		}, codes.NotFound},
		{"enable a missing automation", func() error {
			_, err := server.SetAutomationEnabled(ctx, &turingv1.SetAutomationEnabledRequest{AutomationId: "auto_nope"})
			return err
		}, codes.NotFound},
		{"delete a missing automation", func() error {
			_, err := server.DeleteAutomation(ctx, &turingv1.DeleteAutomationRequest{AutomationId: "auto_nope"})
			return err
		}, codes.NotFound},
		{"rename onto a taken name", func() error {
			second, createErr := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
				Name: "Second", Prompt: "y", Schedule: everyFiveMinutes(),
			})
			if createErr != nil {
				return createErr
			}
			_, err := server.UpdateAutomation(ctx, &turingv1.UpdateAutomationRequest{
				AutomationId: second.GetAutomationId(), Name: existing.GetName(), Prompt: "y", Schedule: everyFiveMinutes(),
			})
			return err
		}, codes.AlreadyExists},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.call()
			if status.Code(err) != testCase.want {
				t.Fatalf("code = %s (%v), want %s", status.Code(err), err, testCase.want)
			}
		})
	}
}

// A storage failure must not hand the caller a SQLite message. Closing the
// database is the cheapest way to make every statement fail.
func TestStorageFailuresDoNotLeakTheirText(t *testing.T) {
	ctx := context.Background()
	database, err := db.Open(filepath.Join(t.TempDir(), "closed.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ApplyMigrations(ctx, database); err != nil {
		t.Fatal(err)
	}
	_ = database.Close()
	server := New(repository.New(database))

	_, err = server.ListAutomations(ctx, &turingv1.ListAutomationsRequest{})
	if status.Code(err) != codes.Internal || status.Convert(err).Message() != "list automations failed" {
		t.Fatalf("list error = %v, want a fixed Internal message", err)
	}
	_, err = server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
		Name: "Digest", Prompt: "x", Schedule: everyFiveMinutes(),
	})
	if status.Code(err) != codes.Internal || status.Convert(err).Message() != "create automation failed" {
		t.Fatalf("create error = %v, want a fixed Internal message", err)
	}
	if strings.Contains(status.Convert(err).Message(), "sql") {
		t.Fatalf("storage text leaked: %v", err)
	}
}

func TestListAutomationsReturnsThemByName(t *testing.T) {
	server, _, ctx := newTestServer(t)

	for _, name := range []string{"Zebra", "apple", "Mango"} {
		if _, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
			Name: name, Prompt: "x", Schedule: everyFiveMinutes(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	listed, err := server.ListAutomations(ctx, &turingv1.ListAutomationsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(listed.GetAutomations()))
	for _, automation := range listed.GetAutomations() {
		got = append(got, automation.GetName())
	}
	want := []string{"apple", "Mango", "Zebra"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("listed %v, want %v", got, want)
	}
}

func TestDeleteAutomationRemovesItFromTheList(t *testing.T) {
	server, _, ctx := newTestServer(t)

	created, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
		Name: "Digest", Prompt: "x", Schedule: everyFiveMinutes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := server.DeleteAutomation(ctx, &turingv1.DeleteAutomationRequest{AutomationId: created.GetAutomationId()}); err != nil {
		t.Fatal(err)
	}
	listed, err := server.ListAutomations(ctx, &turingv1.ListAutomationsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.GetAutomations()) != 0 {
		t.Fatalf("listed %+v after delete, want none", listed.GetAutomations())
	}
}

// An unattended run is the one context where nobody is present to see a memory
// being read or proposed, so a memory tool cannot even be put on an
// automation's allowlist. The refusal is its own status, distinct from the
// integration one, so a client can say which rule it hit.
func TestAutomationsRefuseMemoryToolsOnTheAllowlist(t *testing.T) {
	server, _, ctx := newTestServer(t)

	for _, tool := range []string{"memory.search", "memory.read", "memory.remember"} {
		_, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
			Name:     "Digest " + tool,
			Prompt:   "Summarise the sandbox.",
			Schedule: everyFiveMinutes(),
			AllowedTools: []*turingv1.AutomationTool{
				{ServerName: "memory", ToolName: tool},
			},
		})
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("create with %s error = %v, want FailedPrecondition", tool, err)
		}
		if got := status.Convert(err).Message(); got != "memory tools are not available to automations" {
			t.Fatalf("message = %q, want the memory-specific sentence", got)
		}
	}

	created, err := server.CreateAutomation(ctx, &turingv1.CreateAutomationRequest{
		Name:     "Digest",
		Prompt:   "Summarise the sandbox.",
		Schedule: everyFiveMinutes(),
		AllowedTools: []*turingv1.AutomationTool{
			{ServerName: "files", ToolName: "files.read"},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Updating an existing automation is the other door onto the same list.
	if _, err := server.UpdateAutomation(ctx, &turingv1.UpdateAutomationRequest{
		AutomationId: created.GetAutomationId(),
		Name:         "Digest",
		Prompt:       "Summarise the sandbox.",
		Schedule:     everyFiveMinutes(),
		AllowedTools: []*turingv1.AutomationTool{
			{ServerName: "memory", ToolName: "memory.search"},
		},
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("update error = %v, want FailedPrecondition", err)
	}
}
