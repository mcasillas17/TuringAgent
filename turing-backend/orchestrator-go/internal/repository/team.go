package repository

import (
	"context"
	"errors"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/teamfiles"
)

var (
	ErrAgentProfileNotFound      = errors.New("agent profile not found")
	ErrAgentProfileInvalid       = errors.New("agent profile failed to load")
	ErrAgentProfileRevisionStale = errors.New("agent profile changed since it was shown")
)

// AgentProfile is a team/<id>/AGENT.md file decorated with the user's
// settings. A settings row whose folder is gone is ignored, and applies again
// if a folder with that ID reappears; a changed revision needs a new grant.
type AgentProfile struct {
	teamfiles.Profile
	Enabled         bool
	GrantedRevision string
}

// ToolCatalogEntry is one currently advertised tool row with the facts that
// decide whether a specialist may use it.
type ToolCatalogEntry struct {
	ServerName string
	ToolName   string
	Policy     string
	Enabled    bool
	ServerTier string
}

func (r *Repository) SetTeamStore(store *teamfiles.Store) {
	r.teamStore = store
}

// ListAgentProfiles is read-only: listing never creates settings rows.
func (r *Repository) ListAgentProfiles(ctx context.Context) ([]AgentProfile, error) {
	files, err := r.scanTeam()
	if err != nil {
		return nil, err
	}
	settings, err := r.agentProfileSettings(ctx)
	if err != nil {
		return nil, err
	}
	profiles := make([]AgentProfile, 0, len(files))
	for _, file := range files {
		profiles = append(profiles, decorateAgentProfile(file, settings))
	}
	return profiles, nil
}

func (r *Repository) SetAgentProfileEnabled(ctx context.Context, profileID string, enabled bool) (AgentProfile, error) {
	file, err := r.agentProfileFile(profileID)
	if err != nil {
		return AgentProfile{}, err
	}
	if enabled && file.ParseError != "" {
		return AgentProfile{}, ErrAgentProfileInvalid
	}
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO agent_profile_settings (profile_id, enabled, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(profile_id) DO UPDATE SET
			enabled = excluded.enabled,
			updated_at = excluded.updated_at
	`, profileID, boolToInt(enabled), now()); err != nil {
		return AgentProfile{}, err
	}
	return r.agentProfileWithSettings(ctx, file)
}

// GrantAgentProfile binds consent to the declaration revision the user was
// shown. It neither enables nor disables the profile.
func (r *Repository) GrantAgentProfile(ctx context.Context, profileID, revision string) (AgentProfile, error) {
	file, err := r.agentProfileFile(profileID)
	if err != nil {
		return AgentProfile{}, err
	}
	if file.ParseError != "" {
		return AgentProfile{}, ErrAgentProfileInvalid
	}
	if file.Revision != revision {
		return AgentProfile{}, ErrAgentProfileRevisionStale
	}
	grantedAt := now()
	if _, err := r.db.ExecContext(ctx, `
		INSERT INTO agent_profile_settings (profile_id, enabled, granted_revision, granted_at, updated_at)
		VALUES (?, 0, ?, ?, ?)
		ON CONFLICT(profile_id) DO UPDATE SET
			granted_revision = excluded.granted_revision,
			granted_at = excluded.granted_at,
			updated_at = excluded.updated_at
	`, profileID, revision, grantedAt, grantedAt); err != nil {
		return AgentProfile{}, err
	}
	return r.agentProfileWithSettings(ctx, file)
}

// ListToolCatalog returns every tool row currently advertised, plus every row a
// policy disables: workers stop reporting a disabled tool, so a refresh marks it
// absent, yet the policy is still why a profile cannot have it. Rows carry
// their server's tier; pseudo-server rows have no server and an empty tier.
func (r *Repository) ListToolCatalog(ctx context.Context) ([]ToolCatalogEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT tool.server_name, tool.tool_name, tool.policy, tool.enabled, COALESCE(server.tier, '')
		FROM tools tool
		LEFT JOIN mcp_servers server ON server.id = tool.mcp_server_id
		WHERE tool.present = 1 OR tool.policy = 'disabled'
		ORDER BY tool.server_name, tool.tool_name
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	entries := make([]ToolCatalogEntry, 0)
	for rows.Next() {
		var entry ToolCatalogEntry
		var enabled int
		if err := rows.Scan(&entry.ServerName, &entry.ToolName, &entry.Policy, &enabled, &entry.ServerTier); err != nil {
			return nil, err
		}
		entry.Enabled = enabled == 1
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func (r *Repository) scanTeam() ([]teamfiles.Profile, error) {
	if r.teamStore == nil {
		return nil, nil
	}
	return r.teamStore.Scan()
}

func (r *Repository) agentProfileFile(profileID string) (teamfiles.Profile, error) {
	files, err := r.scanTeam()
	if err != nil {
		return teamfiles.Profile{}, err
	}
	for _, file := range files {
		if file.ID == profileID {
			return file, nil
		}
	}
	return teamfiles.Profile{}, ErrAgentProfileNotFound
}

type agentProfileSetting struct {
	enabled         bool
	grantedRevision string
}

func (r *Repository) agentProfileSettings(ctx context.Context) (map[string]agentProfileSetting, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT profile_id, enabled, COALESCE(granted_revision, '') FROM agent_profile_settings`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	settings := make(map[string]agentProfileSetting)
	for rows.Next() {
		var profileID string
		var enabled int
		var setting agentProfileSetting
		if err := rows.Scan(&profileID, &enabled, &setting.grantedRevision); err != nil {
			return nil, err
		}
		setting.enabled = enabled == 1
		settings[profileID] = setting
	}
	return settings, rows.Err()
}

func (r *Repository) agentProfileWithSettings(ctx context.Context, file teamfiles.Profile) (AgentProfile, error) {
	settings, err := r.agentProfileSettings(ctx)
	if err != nil {
		return AgentProfile{}, err
	}
	return decorateAgentProfile(file, settings), nil
}

func decorateAgentProfile(file teamfiles.Profile, settings map[string]agentProfileSetting) AgentProfile {
	setting := settings[file.ID]
	return AgentProfile{Profile: file, Enabled: setting.enabled, GrantedRevision: setting.grantedRevision}
}
