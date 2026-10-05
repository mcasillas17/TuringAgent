package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
)

// ToolSelection is the tool set a run's job was frozen with, and whether the
// job asked for it to be enforced. A job that does not enforce its set allows
// every tool here; policy and the egress decision still decide each call.
type ToolSelection struct {
	Enforced bool
	Tools    []string
}

// Allows reports whether a qualified server/tool name may run. An enforced
// empty set allows nothing.
func (s ToolSelection) Allows(qualified string) bool {
	return !s.Enforced || slices.Contains(s.Tools, qualified)
}

// RunToolSelection reads the selection from the run's persisted job, so a
// caller's own account of its tools can never widen it.
//
// Every tool decision calls it, so it reads the two keys it needs rather than
// decoding the whole payload, and only decodes the set when it is enforced.
func (r *Repository) RunToolSelection(ctx context.Context, runID string) (ToolSelection, error) {
	var enforced bool
	var selectedJSON string
	if err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(json_extract(payload_json, '$.enforceSelectedTools'), 0) = 1,
			COALESCE(json_extract(payload_json, '$.selectedTools'), '[]')
		FROM jobs WHERE run_id = ?`, runID).Scan(&enforced, &selectedJSON); err != nil {
		return ToolSelection{}, err
	}
	if !enforced {
		return ToolSelection{}, nil
	}
	var tools []string
	if err := json.Unmarshal([]byte(selectedJSON), &tools); err != nil {
		return ToolSelection{}, err
	}
	return ToolSelection{Enforced: true, Tools: tools}, nil
}

// RunAllowsToolCall reports whether a recorded tool call of the run lies inside
// the run's enforced selection. The server is read from the call's own record,
// since a bare tool name could belong to more than one server. A job that does
// not enforce its set allows every call, and so does a run with no job, which
// the caller's own checks refuse. An enforcing job allows no call it has no
// record of.
func (r *Repository) RunAllowsToolCall(ctx context.Context, runID, toolCallID, toolName string) (bool, error) {
	selection, err := r.RunToolSelection(ctx, runID)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !selection.Enforced {
		return true, nil
	}
	var serverName, recordedTool string
	err = r.db.QueryRowContext(ctx, `SELECT server_name, tool_name FROM tool_calls WHERE id = ? AND run_id = ?`, toolCallID, runID).Scan(&serverName, &recordedTool)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return recordedTool == toolName && selection.Allows(serverName+"/"+toolName), nil
}
