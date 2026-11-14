package repository

import (
	"context"
	"testing"
)

// TUR-026a: an unattended run's frozen grant is intersected with the
// automation as it stands now. It may narrow or end; it never widens.
func TestUnattendedGrantStateFollowsTheAutomationAsItStandsNow(t *testing.T) {
	update := AutomationTool{ServerName: "files", ToolName: "files.update"}
	create := AutomationTool{ServerName: "files", ToolName: "files.create"}
	read := AutomationTool{ServerName: "files", ToolName: "files.read"}

	for _, test := range []struct {
		name string
		// change is what the owner does to the automation after it fired.
		change func(t *testing.T, repo *Repository, ctx context.Context, automationID string)
		pair   AutomationTool
		// wantRevoked is the run losing consent it was given.
		wantRevoked bool
		// wantFrozen is whether the run's own grant holds the pair at all.
		wantFrozen bool
	}{
		{name: "unchanged", pair: update, wantFrozen: true},
		{
			name: "unrelated edit",
			change: func(t *testing.T, repo *Repository, ctx context.Context, id string) {
				saveAllowlist(t, repo, ctx, id, "Renamed digest", update, create)
			},
			pair: update, wantFrozen: true,
		},
		{
			name: "disabled",
			change: func(t *testing.T, repo *Repository, ctx context.Context, id string) {
				if _, err := repo.SetAutomationEnabled(ctx, id, false); err != nil {
					t.Fatal(err)
				}
			},
			pair: update, wantRevoked: true, wantFrozen: true,
		},
		{
			name: "disabled, for a tool it never listed",
			change: func(t *testing.T, repo *Repository, ctx context.Context, id string) {
				if _, err := repo.SetAutomationEnabled(ctx, id, false); err != nil {
					t.Fatal(err)
				}
			},
			pair: read, wantRevoked: true,
		},
		{
			name: "deleted",
			change: func(t *testing.T, repo *Repository, ctx context.Context, id string) {
				if err := repo.DeleteAutomation(ctx, id); err != nil {
					t.Fatal(err)
				}
			},
			pair: update, wantRevoked: true, wantFrozen: true,
		},
		{
			name: "narrowed away",
			change: func(t *testing.T, repo *Repository, ctx context.Context, id string) {
				saveAllowlist(t, repo, ctx, id, "Digest", create)
			},
			pair: update, wantRevoked: true, wantFrozen: true,
		},
		{
			name: "narrowed, the pair that stayed",
			change: func(t *testing.T, repo *Repository, ctx context.Context, id string) {
				saveAllowlist(t, repo, ctx, id, "Digest", create)
			},
			pair: create, wantFrozen: true,
		},
		{
			// Widening reaches nothing: the pair is on the current allowlist
			// but never was on the frozen one, so it is not "revoked" — it is
			// simply not granted, which Grant.Allows answers.
			name: "widened",
			change: func(t *testing.T, repo *Repository, ctx context.Context, id string) {
				saveAllowlist(t, repo, ctx, id, "Digest", update, create, read)
			},
			pair: read,
		},
		{
			// The intersection is with the automation as it stands at the
			// call: re-enabled before the run's next call, nothing was lost.
			name: "disabled then re-enabled",
			change: func(t *testing.T, repo *Repository, ctx context.Context, id string) {
				if _, err := repo.SetAutomationEnabled(ctx, id, false); err != nil {
					t.Fatal(err)
				}
				if _, err := repo.SetAutomationEnabled(ctx, id, true); err != nil {
					t.Fatal(err)
				}
			},
			pair: update, wantFrozen: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, ctx := newTitleTestRepo(t)
			automation, err := repo.CreateAutomation(ctx, AutomationInput{
				Name: "Digest", Prompt: "x", Schedule: everyFiveMinutes(), Enabled: true,
				AllowedTools: []AutomationTool{update, create},
			})
			if err != nil {
				t.Fatal(err)
			}
			fire, _, err := repo.ClaimDueAutomation(ctx, mustParse(t, automation.NextDueAt), automationDefaults)
			if err != nil {
				t.Fatal(err)
			}
			if test.change != nil {
				test.change(t, repo, ctx, automation.AutomationID)
			}

			state, found, err := repo.GetUnattendedGrantState(ctx, fire.RunID, test.pair.ServerName, test.pair.ToolName)
			if err != nil || !found {
				t.Fatalf("grant state = found %v err %v, want found", found, err)
			}
			if got := state.Revoked(test.pair.ServerName, test.pair.ToolName); got != test.wantRevoked {
				t.Fatalf("revoked = %v, want %v (state %+v)", got, test.wantRevoked, state)
			}
			if got := state.Grant.Allows(test.pair.ServerName, test.pair.ToolName); got != test.wantFrozen {
				t.Fatalf("frozen grant allows %s = %v, want %v", test.pair.ToolName, got, test.wantFrozen)
			}
			if state.Grant.AutomationID != automation.AutomationID || state.Grant.AutomationName != "Digest" {
				t.Fatalf("grant = %+v, want the automation as it fired", state.Grant)
			}
		})
	}
}

// A run the user typed has no grant state, so the revocation check has
// nothing to apply to it and the ordinary path is unchanged.
func TestAHandDrivenRunHasNoUnattendedGrantState(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "By hand")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "write a file", AgentID: "general_assistant",
		ModelProvider: "ollama", Model: "qwen2.5:7b",
	})
	if err != nil {
		t.Fatal(err)
	}
	state, found, err := repo.GetUnattendedGrantState(ctx, enqueued.RunID, "files", "files.update")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("a hand-driven run reported grant state: %+v", state)
	}
}

func saveAllowlist(t *testing.T, repo *Repository, ctx context.Context, automationID string, name string, tools ...AutomationTool) {
	t.Helper()
	if _, err := repo.UpdateAutomation(ctx, automationID, AutomationInput{
		Name: name, Prompt: "x", Schedule: everyFiveMinutes(), AllowedTools: tools,
	}); err != nil {
		t.Fatal(err)
	}
}
