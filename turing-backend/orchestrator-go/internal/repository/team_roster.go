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

// TeamContinuation is what a run's continuation may use, frozen onto the run
// beside its roster so the join, inside whichever transaction finishes the
// last task, reads nothing but the database.
type TeamContinuation struct {
	// Tools are the continuation's selected tools: the tools of the run's
	// route on team-protocol workers, less the team, egressing tools and
	// disabled ones.
	Tools []string `json:"tools"`
	// ResultMaxBytes is how much of each specialist's result the join keeps.
	ResultMaxBytes int `json:"resultMaxBytes"`
}

// frozenTeamContinuation is the continuation frozen onto a run that keeps its
// roster. A continuation never gains a tool its parent lacked, so a parent
// whose tools a decision froze keeps only the continuation tools that set
// holds.
func frozenTeamContinuation(continuation TeamContinuation, frozen bool, selectedTools []string) TeamContinuation {
	tools := make([]string, 0, len(continuation.Tools))
	for _, tool := range continuation.Tools {
		if !frozen || slices.Contains(selectedTools, tool) {
			tools = append(tools, tool)
		}
	}
	continuation.Tools = tools
	return continuation
}

// SessionHasDelegations reports whether a conversation has delegated a task.
// Every later turn in one needs a team-protocol worker.
func (r *Repository) SessionHasDelegations(ctx context.Context, sessionID string) (bool, error) {
	return sessionHasDelegationsTx(ctx, r.db, sessionID)
}

func sessionHasDelegationsTx(ctx context.Context, q rowQuerier, sessionID string) (bool, error) {
	var delegated bool
	err := q.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM delegations WHERE parent_session_id = ?)`, sessionID).Scan(&delegated)
	return delegated, err
}

// RunTeamContinuation reads the continuation frozen onto the run's job: the
// zero value for a run that was not offered the team.
func (r *Repository) RunTeamContinuation(ctx context.Context, runID string) (TeamContinuation, error) {
	return runTeamContinuation(ctx, r.db, runID)
}

func runTeamContinuation(ctx context.Context, q rowQuerier, runID string) (TeamContinuation, error) {
	var raw sql.NullString
	if err := q.QueryRowContext(ctx,
		`SELECT json_extract(payload_json, '$.teamContinuation') FROM jobs WHERE run_id = ?`, runID,
	).Scan(&raw); err != nil {
		return TeamContinuation{}, err
	}
	var continuation TeamContinuation
	if !raw.Valid {
		return continuation, nil
	}
	err := json.Unmarshal([]byte(raw.String), &continuation)
	return continuation, err
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
