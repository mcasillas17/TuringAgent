package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
)

// TeamDelegateTool is the qualified name of the one team tool. A frozen tool
// set names it when a run may delegate.
const TeamDelegateTool = "team/team.delegate"

// TeamRosterEntry is one specialist a Turing run may delegate to, frozen onto
// the run's job when it is enqueued: a profile edited, disabled or revoked
// while the run waits never changes the team that run was offered.
type TeamRosterEntry struct {
	ProfileID   string   `json:"profileId"`
	Revision    string   `json:"revision"`
	Name        string   `json:"name"`
	Emoji       string   `json:"emoji"`
	Description string   `json:"description"`
	Tools       []string `json:"tools"`
}

// teamRosterForRoute is the roster a run on this route may carry. The service
// builds one only for a run that can delegate; this is the writer refusing to
// freeze one onto a run that cannot. A remote model never delegates, and an
// external agent is one, and a run whose tools are frozen by an egress decision
// delegates only if that set names team.delegate.
//
// The roster, not the frozen tool set, decides whether a run may delegate. A
// consented send freezes the set it was consented with, team.delegate
// included, even when the team could not be read at send time and the run has
// no roster; such a run is offered no team tool.
func teamRosterForRoute(input EnqueueUserMessageInput, route RoutingRequirements) []TeamRosterEntry {
	if len(input.TeamRoster) == 0 || route.ModelProvider == "openai_compatible" {
		return nil
	}
	if input.EgressDecision != nil && !slices.Contains(input.EgressDecision.SelectedTools, TeamDelegateTool) {
		return nil
	}
	return input.TeamRoster
}

// RunTeamRoster reads the roster frozen onto the run's job, never the live
// profiles. It is empty for a run that was not offered the team, and
// sql.ErrNoRows when the run has no job.
func (r *Repository) RunTeamRoster(ctx context.Context, runID string) ([]TeamRosterEntry, error) {
	var raw sql.NullString
	if err := r.db.QueryRowContext(ctx,
		`SELECT json_extract(payload_json, '$.teamRoster') FROM jobs WHERE run_id = ?`, runID,
	).Scan(&raw); err != nil {
		return nil, err
	}
	if !raw.Valid {
		return nil, nil
	}
	var roster []TeamRosterEntry
	if err := json.Unmarshal([]byte(raw.String), &roster); err != nil {
		return nil, err
	}
	return roster, nil
}
