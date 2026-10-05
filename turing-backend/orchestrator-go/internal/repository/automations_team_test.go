package repository

import (
	"context"
	"errors"
	"testing"
	"time"
)

// An unattended run never delegates: nobody is there to see what a specialist
// was asked to do, so the allowlist refuses the team tool at save time.
func TestAutomationsRefuseTeamToolsAtSaveTime(t *testing.T) {
	repo := New(openTestDB(t))
	_, err := repo.CreateAutomation(context.Background(), AutomationInput{
		Name: "nightly delegation", Prompt: "summarise",
		Schedule:     Schedule{Kind: ScheduleInterval, Interval: time.Hour},
		AllowedTools: []AutomationTool{{ServerName: "team", ToolName: "team.delegate"}},
	})
	if !errors.Is(err, ErrAutomationTeamToolUnsupported) {
		t.Fatalf("CreateAutomation with team.delegate error = %v, want ErrAutomationTeamToolUnsupported", err)
	}
}
