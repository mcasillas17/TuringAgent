package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

// A skill's full text is copied into jobs.payload_json for every message sent
// in a conversation using it, and into every request to a local model with a
// finite context. Bounded here rather than trusting the editor, which is one
// client of several.
const (
	maxSkillNameRunes         = 120
	maxSkillInstructionsRunes = 8000
)

var (
	ErrSkillNotFound         = errors.New("skill not found")
	ErrSkillNameTaken        = errors.New("a skill with that name already exists")
	ErrSkillNameEmpty        = errors.New("skill name is required")
	ErrSkillNoContent        = errors.New("skill instructions are required")
	ErrSkillNameTooLong      = errors.New("skill name is too long")
	ErrSkillInstructionsLong = errors.New("skill instructions are too long")
	ErrSkillNotAttached      = errors.New("skill is not attached to that session")
)

type Skill struct {
	SkillID      string
	Name         string
	Instructions string
	CreatedAt    string
	UpdatedAt    string
}

// AttachedSkill is what a queued job carries: enough to instruct the model and
// to say which skill an instruction came from, and nothing else.
// The json tags are load-bearing, not decoration: this struct is written into
// jobs.payload_json and read back when the job is claimed, possibly by a
// different build. Adding tags later would silently decode every already-queued
// job's skills to empty strings.
type AttachedSkill struct {
	Name         string `json:"name"`
	Instructions string `json:"instructions"`
}

// validateSkill trims and bounds the two fields every write shares.
func validateSkill(name, instructions string) (string, string, error) {
	name, instructions = strings.TrimSpace(name), strings.TrimSpace(instructions)
	switch {
	case name == "":
		return "", "", ErrSkillNameEmpty
	case instructions == "":
		return "", "", ErrSkillNoContent
	case len([]rune(name)) > maxSkillNameRunes:
		return "", "", ErrSkillNameTooLong
	case len([]rune(instructions)) > maxSkillInstructionsRunes:
		return "", "", ErrSkillInstructionsLong
	}
	return name, instructions, nil
}

func (r *Repository) CreateSkill(ctx context.Context, name, instructions string) (Skill, error) {
	name, instructions, err := validateSkill(name, instructions)
	if err != nil {
		return Skill{}, err
	}
	createdAt := now()
	skill := Skill{
		SkillID:      ids.New("skill"),
		Name:         name,
		Instructions: instructions,
		CreatedAt:    createdAt,
		UpdatedAt:    createdAt,
	}
	_, err = r.db.ExecContext(ctx,
		`INSERT INTO skills (id, name, instructions, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		skill.SkillID, skill.Name, skill.Instructions, createdAt, createdAt)
	if err != nil {
		if isUniqueViolation(err) {
			return Skill{}, ErrSkillNameTaken
		}
		return Skill{}, err
	}
	return skill, nil
}

func (r *Repository) UpdateSkill(ctx context.Context, skillID, name, instructions string) (Skill, error) {
	name, instructions, err := validateSkill(name, instructions)
	if err != nil {
		return Skill{}, err
	}
	result, err := r.db.ExecContext(ctx,
		`UPDATE skills SET name = ?, instructions = ?, updated_at = ? WHERE id = ?`,
		name, instructions, now(), skillID)
	if err != nil {
		if isUniqueViolation(err) {
			return Skill{}, ErrSkillNameTaken
		}
		return Skill{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Skill{}, err
	}
	if affected == 0 {
		return Skill{}, ErrSkillNotFound
	}
	return r.GetSkill(ctx, skillID)
}

func (r *Repository) GetSkill(ctx context.Context, skillID string) (Skill, error) {
	var skill Skill
	err := r.db.QueryRowContext(ctx,
		`SELECT id, name, instructions, created_at, updated_at FROM skills WHERE id = ?`, skillID).
		Scan(&skill.SkillID, &skill.Name, &skill.Instructions, &skill.CreatedAt, &skill.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Skill{}, ErrSkillNotFound
	}
	return skill, err
}

func (r *Repository) DeleteSkill(ctx context.Context, skillID string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM skills WHERE id = ?`, skillID)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrSkillNotFound
	}
	return nil
}

// ListSkills orders by name so the library reads the same way every time. A
// list that reorders itself between visits is one the user cannot learn.
func (r *Repository) ListSkills(ctx context.Context) ([]Skill, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, name, instructions, created_at, updated_at FROM skills ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanSkills(rows)
}

func (r *Repository) AttachSkill(ctx context.Context, sessionID, skillID string) ([]Skill, error) {
	// Attaching something already attached is what a double tap looks like, and
	// it should mean the same as attaching it once.
	if _, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO session_skills (session_id, skill_id, attached_at) VALUES (?, ?, ?)`,
		sessionID, skillID, now()); err != nil {
		if isForeignKeyViolation(err) {
			// The constraint does not say WHICH side is missing, and telling a
			// user their skill is gone when it is the conversation that went
			// stale sends them looking in the wrong place.
			return nil, r.missingAttachSide(ctx, sessionID)
		}
		return nil, err
	}
	return r.ListSessionSkills(ctx, sessionID)
}

// missingAttachSide probes which end of a failed attach does not exist. Only
// reached on the error path, so the extra read costs nothing in the normal case.
func (r *Repository) missingAttachSide(ctx context.Context, sessionID string) error {
	var exists int
	err := r.db.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id = ?`, sessionID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSessionNotFound
	}
	if err != nil {
		return err
	}
	return ErrSkillNotFound
}

func (r *Repository) DetachSkill(ctx context.Context, sessionID, skillID string) ([]Skill, error) {
	result, err := r.db.ExecContext(ctx,
		`DELETE FROM session_skills WHERE session_id = ? AND skill_id = ?`, sessionID, skillID)
	if err != nil {
		return nil, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, ErrSkillNotAttached
	}
	return r.ListSessionSkills(ctx, sessionID)
}

func (r *Repository) ListSessionSkills(ctx context.Context, sessionID string) ([]Skill, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT s.id, s.name, s.instructions, s.created_at, s.updated_at
		FROM session_skills ss
		JOIN skills s ON s.id = ss.skill_id
		WHERE ss.session_id = ?
		ORDER BY s.name COLLATE NOCASE, s.id
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanSkills(rows)
}

func scanSkills(rows *sql.Rows) ([]Skill, error) {
	var skills []Skill
	for rows.Next() {
		var skill Skill
		if err := rows.Scan(&skill.SkillID, &skill.Name, &skill.Instructions, &skill.CreatedAt, &skill.UpdatedAt); err != nil {
			return nil, err
		}
		skills = append(skills, skill)
	}
	return skills, rows.Err()
}

// attachedSkillsTx reads the skills to freeze into a job payload. It runs
// inside the enqueue transaction so the snapshot matches the message the user
// actually sent, not whatever the set became while the job sat in the queue.
func attachedSkillsTx(ctx context.Context, tx *sql.Tx, sessionID string) ([]AttachedSkill, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT s.name, s.instructions
		FROM session_skills ss
		JOIN skills s ON s.id = ss.skill_id
		WHERE ss.session_id = ?
		ORDER BY s.name COLLATE NOCASE, s.id
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var attached []AttachedSkill
	for rows.Next() {
		var skill AttachedSkill
		if err := rows.Scan(&skill.Name, &skill.Instructions); err != nil {
			return nil, err
		}
		attached = append(attached, skill)
	}
	return attached, rows.Err()
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

func isForeignKeyViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}
