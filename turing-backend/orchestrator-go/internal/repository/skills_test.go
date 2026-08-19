package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCreateSkillTrimsAndRejectsEmptyFields(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	skill, err := repo.CreateSkill(ctx, "  Tone  ", "  Answer in short sentences.  ")
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if skill.Name != "Tone" || skill.Instructions != "Answer in short sentences." {
		t.Fatalf("stored %q / %q, want both trimmed", skill.Name, skill.Instructions)
	}

	if _, err := repo.CreateSkill(ctx, "   ", "something"); !errors.Is(err, ErrSkillNameEmpty) {
		t.Fatalf("whitespace name error = %v, want ErrSkillNameEmpty", err)
	}
	// A skill with no instructions would attach fine and do nothing, which is
	// indistinguishable from the feature being broken.
	if _, err := repo.CreateSkill(ctx, "Named", "  \n "); !errors.Is(err, ErrSkillNoContent) {
		t.Fatalf("whitespace instructions error = %v, want ErrSkillNoContent", err)
	}
}

// Names are how a user refers to a skill and how the runtime attributes an
// instruction back to one, so two skills sharing a name makes both ambiguous.
func TestCreateSkillRejectsDuplicateNamesRegardlessOfCase(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	if _, err := repo.CreateSkill(ctx, "Tone", "first"); err != nil {
		t.Fatalf("create skill: %v", err)
	}

	if _, err := repo.CreateSkill(ctx, "Tone", "second"); !errors.Is(err, ErrSkillNameTaken) {
		t.Fatalf("duplicate error = %v, want ErrSkillNameTaken", err)
	}
	if _, err := repo.CreateSkill(ctx, "tone", "third"); !errors.Is(err, ErrSkillNameTaken) {
		t.Fatalf("case-different duplicate error = %v, want ErrSkillNameTaken", err)
	}
}

func TestUpdateSkillReportsMissingSkills(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)

	if _, err := repo.UpdateSkill(ctx, "skill_nope", "Name", "Instructions"); !errors.Is(err, ErrSkillNotFound) {
		t.Fatalf("update error = %v, want ErrSkillNotFound", err)
	}
	if err := repo.DeleteSkill(ctx, "skill_nope"); !errors.Is(err, ErrSkillNotFound) {
		t.Fatalf("delete error = %v, want ErrSkillNotFound", err)
	}
}

func TestAttachSkillIsIdempotentAndReturnsTheFullSet(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	tone, err := repo.CreateSkill(ctx, "Tone", "Be brief.")
	if err != nil {
		t.Fatalf("create tone: %v", err)
	}
	format, err := repo.CreateSkill(ctx, "Format", "Use bullet points.")
	if err != nil {
		t.Fatalf("create format: %v", err)
	}

	if _, err := repo.AttachSkill(ctx, session.SessionID, tone.SkillID); err != nil {
		t.Fatalf("attach tone: %v", err)
	}
	// A double tap must mean the same as one tap.
	attached, err := repo.AttachSkill(ctx, session.SessionID, tone.SkillID)
	if err != nil {
		t.Fatalf("re-attach tone: %v", err)
	}
	if len(attached) != 1 {
		t.Fatalf("attached %d skills after re-attach, want 1", len(attached))
	}

	attached, err = repo.AttachSkill(ctx, session.SessionID, format.SkillID)
	if err != nil {
		t.Fatalf("attach format: %v", err)
	}
	// Sorted by name so the set reads the same way every time it is shown.
	if len(attached) != 2 || attached[0].Name != "Format" || attached[1].Name != "Tone" {
		t.Fatalf("attached = %+v, want Format then Tone", attached)
	}
}

func TestAttachSkillRejectsUnknownIDs(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	if _, err := repo.AttachSkill(ctx, session.SessionID, "skill_nope"); !errors.Is(err, ErrSkillNotFound) {
		t.Fatalf("attach unknown skill error = %v, want ErrSkillNotFound", err)
	}
	skill, err := repo.CreateSkill(ctx, "Tone", "Be brief.")
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}
	// Deliberately NOT ErrSkillNotFound: the skill is fine, the conversation is
	// gone, and saying otherwise sends the user looking in the wrong place.
	if _, err := repo.AttachSkill(ctx, "sess_nope", skill.SkillID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("attach to unknown session error = %v, want ErrSessionNotFound", err)
	}
}

func TestDetachSkillReportsWhenItWasNotAttached(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	skill, err := repo.CreateSkill(ctx, "Tone", "Be brief.")
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}

	if _, err := repo.DetachSkill(ctx, session.SessionID, skill.SkillID); !errors.Is(err, ErrSkillNotAttached) {
		t.Fatalf("detach error = %v, want ErrSkillNotAttached", err)
	}

	if _, err := repo.AttachSkill(ctx, session.SessionID, skill.SkillID); err != nil {
		t.Fatalf("attach: %v", err)
	}
	remaining, err := repo.DetachSkill(ctx, session.SessionID, skill.SkillID)
	if err != nil {
		t.Fatalf("detach: %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("remaining = %+v, want none", remaining)
	}
}

// A deleted skill cannot keep instructing conversations it was attached to.
func TestDeletingASkillDetachesItEverywhere(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	skill, err := repo.CreateSkill(ctx, "Tone", "Be brief.")
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if _, err := repo.AttachSkill(ctx, session.SessionID, skill.SkillID); err != nil {
		t.Fatalf("attach: %v", err)
	}

	if err := repo.DeleteSkill(ctx, skill.SkillID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	attached, err := repo.ListSessionSkills(ctx, session.SessionID)
	if err != nil {
		t.Fatalf("list session skills: %v", err)
	}
	if len(attached) != 0 {
		t.Fatalf("attached = %+v, want none after the skill was deleted", attached)
	}
}

// The payload is the contract with the runtime: the skills a run was told to
// follow are the ones attached when the message was sent.
func TestEnqueueUserMessageSnapshotsAttachedSkills(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	skill, err := repo.CreateSkill(ctx, "Tone", "Be brief.")
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if _, err := repo.AttachSkill(ctx, session.SessionID, skill.SkillID); err != nil {
		t.Fatalf("attach: %v", err)
	}

	result, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "hello",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	skills := payloadSkills(t, ctx, repo, result.JobID)
	if len(skills) != 1 || skills[0].Name != "Tone" || skills[0].Instructions != "Be brief." {
		t.Fatalf("payload skills = %+v, want the attached skill", skills)
	}
}

// Editing a skill must not reach back into a run that is already waiting: the
// user pressed send on the instructions as they were at that moment.
func TestQueuedJobKeepsTheSkillsItWasEnqueuedWith(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	skill, err := repo.CreateSkill(ctx, "Tone", "Be brief.")
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if _, err := repo.AttachSkill(ctx, session.SessionID, skill.SkillID); err != nil {
		t.Fatalf("attach: %v", err)
	}
	result, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "hello",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	if _, err := repo.UpdateSkill(ctx, skill.SkillID, "Tone", "Write at length."); err != nil {
		t.Fatalf("update skill: %v", err)
	}
	if _, err := repo.DetachSkill(ctx, session.SessionID, skill.SkillID); err != nil {
		t.Fatalf("detach: %v", err)
	}

	skills := payloadSkills(t, ctx, repo, result.JobID)
	if len(skills) != 1 || skills[0].Instructions != "Be brief." {
		t.Fatalf("payload skills = %+v, want the instructions as they were at enqueue", skills)
	}
}

// The order the runtime renders sections in comes from here, so a snapshot
// that reorders between messages would silently reorder the instructions.
func TestEnqueueSnapshotIsOrderedByName(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	// Attached in an order that does not match the expected output, so passing
	// cannot be an accident of insertion order.
	for _, name := range []string{"Zeta", "alpha", "Middle"} {
		skill, err := repo.CreateSkill(ctx, name, "instructions for "+name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := repo.AttachSkill(ctx, session.SessionID, skill.SkillID); err != nil {
			t.Fatalf("attach %s: %v", name, err)
		}
	}

	result, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "hello",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	skills := payloadSkills(t, ctx, repo, result.JobID)
	var names []string
	for _, skill := range skills {
		names = append(names, skill.Name)
	}
	// Case-insensitive, so "alpha" sorts before "Middle" rather than after "Zeta".
	if got := strings.Join(names, ","); got != "alpha,Middle,Zeta" {
		t.Fatalf("snapshot order = %s, want alpha,Middle,Zeta", got)
	}
}

// The payload is persisted and read back by a possibly different build, so the
// key names are part of the contract, not an implementation detail.
func TestJobPayloadUsesStableSkillKeys(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	skill, err := repo.CreateSkill(ctx, "Tone", "Be brief.")
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if _, err := repo.AttachSkill(ctx, session.SessionID, skill.SkillID); err != nil {
		t.Fatalf("attach: %v", err)
	}
	result, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "hello",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	var payloadJSON string
	if err := repo.db.QueryRowContext(ctx, `SELECT payload_json FROM jobs WHERE id = ?`, result.JobID).Scan(&payloadJSON); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	if !strings.Contains(payloadJSON, `"name":"Tone"`) {
		t.Fatalf("payload does not use the lower-case key: %s", payloadJSON)
	}
	if !strings.Contains(payloadJSON, `"instructions":"Be brief."`) {
		t.Fatalf("payload does not use the lower-case key: %s", payloadJSON)
	}
}

// Renaming a skill onto another's name must be refused the same way creating a
// duplicate is.
func TestUpdateSkillRejectsRenamingOntoAnExistingName(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	if _, err := repo.CreateSkill(ctx, "Tone", "Be brief."); err != nil {
		t.Fatalf("create tone: %v", err)
	}
	format, err := repo.CreateSkill(ctx, "Format", "Use bullets.")
	if err != nil {
		t.Fatalf("create format: %v", err)
	}

	if _, err := repo.UpdateSkill(ctx, format.SkillID, "tone", "Use bullets."); !errors.Is(err, ErrSkillNameTaken) {
		t.Fatalf("rename error = %v, want ErrSkillNameTaken", err)
	}
}

// Deleting a conversation must not leave orphaned attachment rows behind.
func TestDeletingASessionRemovesItsAttachments(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	skill, err := repo.CreateSkill(ctx, "Tone", "Be brief.")
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if _, err := repo.AttachSkill(ctx, session.SessionID, skill.SkillID); err != nil {
		t.Fatalf("attach: %v", err)
	}

	if err := repo.DeleteSession(ctx, session.SessionID); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	var remaining int
	if err := repo.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM session_skills WHERE session_id = ?`, session.SessionID).Scan(&remaining); err != nil {
		t.Fatalf("count attachments: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("attachments remaining = %d, want 0", remaining)
	}
	// The skill itself is the user's, and outlives any one conversation.
	if _, err := repo.GetSkill(ctx, skill.SkillID); err != nil {
		t.Fatalf("skill should have survived: %v", err)
	}
}

// A claimed job carries its snapshot through to the worker.
func TestClaimNextJobReturnsTheSnapshottedSkills(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	skill, err := repo.CreateSkill(ctx, "Tone", "Be brief.")
	if err != nil {
		t.Fatalf("create skill: %v", err)
	}
	if _, err := repo.AttachSkill(ctx, session.SessionID, skill.SkillID); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if _, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "hello",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	job, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(job.Skills) != 1 || job.Skills[0].Name != "Tone" {
		t.Fatalf("claimed job skills = %+v, want the attached skill", job.Skills)
	}
}

// Jobs enqueued before skills existed have no "skills" key at all. Decoding
// must treat that as "none attached" rather than failing the claim, which
// would strand every queued run across the upgrade.
func TestClaimNextJobToleratesAPayloadWithoutSkills(t *testing.T) {
	repo, ctx := newTitleTestRepo(t)
	session, err := repo.CreateSession(ctx, "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	result, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "hello",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "qwen2.5:7b",
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// Rewrite the payload to the shape this version never writes but must read.
	legacy := `{"userText":"hello","sessionId":"` + session.SessionID + `","modelProvider":"ollama","model":"qwen2.5:7b"}`
	if _, err := repo.db.ExecContext(ctx, `UPDATE jobs SET payload_json = ? WHERE id = ?`, legacy, result.JobID); err != nil {
		t.Fatalf("rewrite payload: %v", err)
	}

	job, err := repo.ClaimNextJob(ctx, "general_assistant", "worker-1")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if job.JobID != result.JobID {
		t.Fatalf("claimed %q, want %q", job.JobID, result.JobID)
	}
	if len(job.Skills) != 0 {
		t.Fatalf("skills = %+v, want none", job.Skills)
	}
	if job.UserText != "hello" {
		t.Fatalf("userText = %q, want it still decoded", job.UserText)
	}
}

func payloadSkills(t *testing.T, ctx context.Context, repo *Repository, jobID string) []AttachedSkill {
	t.Helper()
	var payloadJSON string
	if err := repo.db.QueryRowContext(ctx, `SELECT payload_json FROM jobs WHERE id = ?`, jobID).Scan(&payloadJSON); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	var payload struct {
		Skills []AttachedSkill `json:"skills"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return payload.Skills
}
