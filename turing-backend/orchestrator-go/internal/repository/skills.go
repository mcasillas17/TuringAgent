package repository

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/skillfiles"
)

var (
	ErrSkillNotFound              = errors.New("skill not found")
	ErrSkillCapabilityNotDeclared = errors.New("skill does not declare that capability")
)

type Skill struct {
	SkillID             string
	Name                string
	Description         string
	Category            string
	Body                string
	Version             string
	Author              string
	License             string
	Requires            []string
	References          map[string]string
	Enabled             bool
	GrantedCapabilities []string
	MissingCapabilities []string
	ParseError          string
	FolderPath          string
}

type grantScopeState int

const (
	grantScopeStale grantScopeState = iota
	grantScopeCurrent
	grantScopeRefresh
)

func classifyGrantScope(scope, currentScope string, currentRequires []string) grantScopeState {
	if scope == currentScope {
		return grantScopeCurrent
	}
	previousRequires, validScope := decodeGrantScope(scope)
	if validScope && !equalStrings(previousRequires, currentRequires) {
		return grantScopeRefresh
	}
	return grantScopeStale
}

// SkillSnapshot is the immutable skill state carried by a queued job. The
// JSON tags are a compatibility contract with jobs already persisted in
// payload_json; changing them would make a waiting job lose its instructions.
type SkillSnapshot struct {
	SkillID             string            `json:"skillId,omitempty"`
	Name                string            `json:"name"`
	Description         string            `json:"description,omitempty"`
	Category            string            `json:"category,omitempty"`
	Body                string            `json:"body,omitempty"`
	References          map[string]string `json:"references,omitempty"`
	Withheld            bool              `json:"withheld,omitempty"`
	MissingCapabilities []string          `json:"missingCapabilities,omitempty"`
	// Instructions reads jobs queued by the database-backed skills model. New
	// snapshots leave it empty and use Body; the runtime maps either form onto
	// the stable proto field 2.
	Instructions string `json:"instructions,omitempty"`
}

func (r *Repository) ReconcileSkills(ctx context.Context) error {
	files, err := r.scanSkills()
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := reconcileSkillsTx(ctx, tx, files); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) ListSkills(ctx context.Context) ([]Skill, error) {
	files, err := r.scanSkills()
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := reconcileSkillsTx(ctx, tx, files); err != nil {
		return nil, err
	}
	skills, err := decorateSkillsTx(ctx, tx, files)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return skills, nil
}

func (r *Repository) GetSkill(ctx context.Context, skillID string) (Skill, error) {
	skills, err := r.ListSkills(ctx)
	if err != nil {
		return Skill{}, err
	}
	return decoratedSkillByID(skills, skillID)
}

func (r *Repository) SetSkillEnabled(ctx context.Context, skillID string, enabled bool) (Skill, error) {
	files, err := r.scanSkills()
	if err != nil {
		return Skill{}, err
	}
	if _, found := fileSkillByID(files, skillID); !found {
		return Skill{}, ErrSkillNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Skill{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := reconcileSkillsTx(ctx, tx, files); err != nil {
		return Skill{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE skill_settings SET enabled = ? WHERE skill_id = ?`, boolToInt(enabled), skillID); err != nil {
		return Skill{}, err
	}
	skills, err := decorateSkillsTx(ctx, tx, files)
	if err != nil {
		return Skill{}, err
	}
	if err := tx.Commit(); err != nil {
		return Skill{}, err
	}
	return decoratedSkillByID(skills, skillID)
}

func (r *Repository) SetSkillGrant(ctx context.Context, skillID string, capability string, granted bool) (Skill, error) {
	files, err := r.scanSkills()
	if err != nil {
		return Skill{}, err
	}
	fileSkill, found := fileSkillByID(files, skillID)
	if !found {
		return Skill{}, ErrSkillNotFound
	}
	if fileSkill.ParseError != "" || !containsString(fileSkill.Requires, capability) {
		return Skill{}, ErrSkillCapabilityNotDeclared
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Skill{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := reconcileSkillsTx(ctx, tx, files); err != nil {
		return Skill{}, err
	}
	if granted {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO skill_capability_grants (skill_id, capability, grant_scope, granted_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(skill_id, capability) DO UPDATE SET
				grant_scope = excluded.grant_scope,
				granted_at = excluded.granted_at
		`, skillID, capability, encodeGrantScope(fileSkill), now()); err != nil {
			return Skill{}, err
		}
	} else if _, err := tx.ExecContext(ctx,
		`DELETE FROM skill_capability_grants WHERE skill_id = ? AND capability = ?`, skillID, capability); err != nil {
		return Skill{}, err
	}
	skills, err := decorateSkillsTx(ctx, tx, files)
	if err != nil {
		return Skill{}, err
	}
	if err := tx.Commit(); err != nil {
		return Skill{}, err
	}
	return decoratedSkillByID(skills, skillID)
}

func (r *Repository) enabledSkillSnapshotsTx(ctx context.Context, tx *sql.Tx) ([]SkillSnapshot, error) {
	files, err := r.scanSkills()
	if err != nil {
		return nil, err
	}
	if err := reconcileSkillsTx(ctx, tx, files); err != nil {
		return nil, err
	}
	skills, err := decorateSkillsTx(ctx, tx, files)
	if err != nil {
		return nil, err
	}
	return enabledSnapshots(skills), nil
}

func (r *Repository) enabledSkillSnapshotsReadOnlyTx(ctx context.Context, tx *sql.Tx) ([]SkillSnapshot, error) {
	files, err := r.scanSkills()
	if err != nil {
		return nil, err
	}
	skills := make([]Skill, 0, len(files))
	for _, fileSkill := range files {
		var enabled int
		err := tx.QueryRowContext(ctx,
			`SELECT enabled FROM skill_settings WHERE skill_id = ?`, fileSkill.ID).Scan(&enabled)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		rows, err := tx.QueryContext(ctx,
			`SELECT capability, grant_scope FROM skill_capability_grants WHERE skill_id = ? ORDER BY capability`, fileSkill.ID)
		if err != nil {
			return nil, err
		}
		currentScope := encodeGrantScope(fileSkill)
		var effectiveGrants []string
		for rows.Next() {
			var capability, scope string
			if err := rows.Scan(&capability, &scope); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if !containsString(fileSkill.Requires, capability) {
				continue
			}
			if classifyGrantScope(scope, currentScope, fileSkill.Requires) != grantScopeStale {
				effectiveGrants = append(effectiveGrants, capability)
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		missing := make([]string, 0, len(fileSkill.Requires))
		for _, capability := range fileSkill.Requires {
			if !containsString(effectiveGrants, capability) {
				missing = append(missing, capability)
			}
		}
		skills = append(skills, Skill{
			SkillID: fileSkill.ID, Name: fileSkill.Name,
			Description: fileSkill.Description, Category: fileSkill.Category,
			Body: fileSkill.Body, Version: fileSkill.Version,
			Author: fileSkill.Author, License: fileSkill.License,
			Requires:   append([]string(nil), fileSkill.Requires...),
			References: cloneStringMap(fileSkill.References),
			Enabled:    enabled == 1, GrantedCapabilities: effectiveGrants,
			MissingCapabilities: missing, ParseError: fileSkill.ParseError,
			FolderPath: fileSkill.FolderPath,
		})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].SkillID < skills[j].SkillID })
	return enabledSnapshots(skills), nil
}

func enabledSnapshots(skills []Skill) []SkillSnapshot {
	var snapshots []SkillSnapshot
	for _, skill := range skills {
		if !skill.Enabled || skill.ParseError != "" {
			continue
		}
		withheld := len(skill.MissingCapabilities) != 0
		snapshot := SkillSnapshot{
			SkillID:             skill.SkillID,
			Name:                skill.Name,
			Description:         skill.Description,
			Category:            skill.Category,
			Withheld:            withheld,
			MissingCapabilities: append([]string(nil), skill.MissingCapabilities...),
		}
		if !withheld {
			snapshot.Body = skill.Body
			snapshot.References = cloneStringMap(skill.References)
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots
}

func (r *Repository) scanSkills() ([]skillfiles.Skill, error) {
	if r.skillStore == nil {
		return nil, nil
	}
	return r.skillStore.Scan()
}

func reconcileSkillsTx(ctx context.Context, tx *sql.Tx, files []skillfiles.Skill) error {
	for _, skill := range files {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO skill_settings (skill_id, enabled) VALUES (?, 0)`, skill.ID); err != nil {
			return err
		}
		if skill.ParseError != "" {
			continue
		}
		rows, err := tx.QueryContext(ctx,
			`SELECT capability, grant_scope FROM skill_capability_grants WHERE skill_id = ?`, skill.ID)
		if err != nil {
			return err
		}
		var stale, refreshed []string
		currentScope := encodeGrantScope(skill)
		for rows.Next() {
			var capability, scope string
			if err := rows.Scan(&capability, &scope); err != nil {
				_ = rows.Close()
				return err
			}
			if !containsString(skill.Requires, capability) {
				stale = append(stale, capability)
				continue
			}
			switch classifyGrantScope(scope, currentScope, skill.Requires) {
			case grantScopeCurrent:
				continue
			case grantScopeRefresh:
				// A declaration that widened or narrowed keeps grants for the
				// capabilities present on both sides, while removed grants above are
				// deleted. Refreshing the scope records the declaration now observed.
				refreshed = append(refreshed, capability)
				continue
			default:
				stale = append(stale, capability)
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, capability := range stale {
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM skill_capability_grants WHERE skill_id = ? AND capability = ?`, skill.ID, capability); err != nil {
				return err
			}
		}
		for _, capability := range refreshed {
			if _, err := tx.ExecContext(ctx, `
				UPDATE skill_capability_grants SET grant_scope = ?
				WHERE skill_id = ? AND capability = ?
			`, currentScope, skill.ID, capability); err != nil {
				return err
			}
		}
	}
	return nil
}

func decorateSkillsTx(ctx context.Context, tx *sql.Tx, files []skillfiles.Skill) ([]Skill, error) {
	skills := make([]Skill, 0, len(files))
	for _, fileSkill := range files {
		var enabled int
		if err := tx.QueryRowContext(ctx,
			`SELECT enabled FROM skill_settings WHERE skill_id = ?`, fileSkill.ID).Scan(&enabled); err != nil {
			return nil, err
		}
		rows, err := tx.QueryContext(ctx,
			`SELECT capability FROM skill_capability_grants WHERE skill_id = ? ORDER BY capability`, fileSkill.ID)
		if err != nil {
			return nil, err
		}
		var grants []string
		for rows.Next() {
			var capability string
			if err := rows.Scan(&capability); err != nil {
				_ = rows.Close()
				return nil, err
			}
			grants = append(grants, capability)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		missing := make([]string, 0, len(fileSkill.Requires))
		for _, capability := range fileSkill.Requires {
			if !containsString(grants, capability) {
				missing = append(missing, capability)
			}
		}
		skills = append(skills, Skill{
			SkillID:             fileSkill.ID,
			Name:                fileSkill.Name,
			Description:         fileSkill.Description,
			Category:            fileSkill.Category,
			Body:                fileSkill.Body,
			Version:             fileSkill.Version,
			Author:              fileSkill.Author,
			License:             fileSkill.License,
			Requires:            append([]string(nil), fileSkill.Requires...),
			References:          cloneStringMap(fileSkill.References),
			Enabled:             enabled == 1,
			GrantedCapabilities: grants,
			MissingCapabilities: missing,
			ParseError:          fileSkill.ParseError,
			FolderPath:          fileSkill.FolderPath,
		})
	}
	sort.Slice(skills, func(i, j int) bool { return skills[i].SkillID < skills[j].SkillID })
	return skills, nil
}

func fileSkillByID(skills []skillfiles.Skill, skillID string) (skillfiles.Skill, bool) {
	for _, skill := range skills {
		if skill.ID == skillID {
			return skill, true
		}
	}
	return skillfiles.Skill{}, false
}

func decoratedSkillByID(skills []Skill, skillID string) (Skill, error) {
	for _, skill := range skills {
		if skill.SkillID == skillID {
			return skill, nil
		}
	}
	return Skill{}, ErrSkillNotFound
}

func containsString(values []string, wanted string) bool {
	index := sort.SearchStrings(values, wanted)
	return index < len(values) && values[index] == wanted
}

func encodeGrantScope(skill skillfiles.Skill) string {
	return skill.Revision + "\n" + strings.Join(skill.Requires, ",")
}

func decodeGrantScope(scope string) ([]string, bool) {
	parts := strings.SplitN(scope, "\n", 2)
	if len(parts) != 2 || parts[0] == "" {
		return nil, false
	}
	if parts[1] == "" {
		return nil, true
	}
	return strings.Split(parts[1], ","), true
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
