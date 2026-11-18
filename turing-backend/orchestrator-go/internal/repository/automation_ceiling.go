package repository

import (
	"context"
	"errors"

	"github.com/mcasillas17/TuringAgent/turing-backend/internal/safejson"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/runoutcome"
)

// UnattendedToolBlock is the automation.tool.blocked audit row a refused tool
// call of an unattended run writes. It names the server, the tool, the
// automation and the code, never the call's arguments.
type UnattendedToolBlock struct {
	AutomationID   string
	AutomationName string
	ServerName     string
	ToolName       string
	// ToolCallID is the beacon's tool call, the row's target. A direct
	// registered-MCP call carries none, so it is empty there.
	ToolCallID string
	Code       string
}

// FailUnattendedRun fails an unattended run under the automation-policy
// failure it is given and, when block is not nil, writes the
// automation.tool.blocked row that says why, in one transaction.
//
// The run's state version is resolved inside that transaction, so there is no
// separate read for another writer's lifecycle bump to race. Its execution is
// preserved: an active one stays active and is marked uncertain until the
// worker acknowledges. The row is written only when the transition applied,
// under RecordAuditForExistingRun's condition that the run's session is still
// active; a failed insert rolls the failure back with it.
//
// A run that is already terminal, that another transition moved first, or
// whose failure this is a write-free replay of, returns moved=false with no
// events and writes nothing, so a repeated refusal changes nothing.
func (r *Repository) FailUnattendedRun(ctx context.Context, runID string, failure runoutcome.Failure, block *UnattendedToolBlock) (RunTransitionResult, bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return RunTransitionResult{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := failRunTx(ctx, tx, FailRunInput{
		RunID:              runID,
		Failure:            failure,
		PreserveExecution:  true,
		resolveVersionInTx: true,
	})
	if errors.Is(err, ErrRunNotFailable) || errors.Is(err, ErrRunTransitionConflict) {
		return RunTransitionResult{}, false, nil
	}
	if err != nil {
		return RunTransitionResult{}, false, err
	}
	if result.Duplicate {
		return RunTransitionResult{}, false, nil
	}
	if block != nil {
		payload, err := safejson.MarshalCanonical(map[string]any{
			"toolName":       block.ToolName,
			"serverName":     block.ServerName,
			"automationId":   block.AutomationID,
			"automationName": block.AutomationName,
			"code":           block.Code,
		})
		if err != nil {
			return RunTransitionResult{}, false, err
		}
		if _, err := recordAuditForExistingRun(ctx, tx, runID, "automation", block.AutomationID,
			"automation.tool.blocked", block.ToolCallID, string(payload)); err != nil {
			return RunTransitionResult{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return RunTransitionResult{}, false, err
	}
	return result, true, nil
}
