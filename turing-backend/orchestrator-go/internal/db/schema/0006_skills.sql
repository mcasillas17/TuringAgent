-- Skills: named instructions the user writes once and attaches per
-- conversation.
--
-- Attachment is a join table rather than a column on sessions because a
-- conversation can carry several, and because detaching must not require
-- rewriting a list that another attach is concurrently editing.
CREATE TABLE IF NOT EXISTS skills (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  instructions TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

-- Names are how the user refers to a skill and how the runtime attributes an
-- instruction back to one, so two skills sharing a name would make both
-- ambiguous. Compared case-insensitively: "Tone" and "tone" are the same
-- name to a person.
CREATE UNIQUE INDEX IF NOT EXISTS skills_name_unique ON skills (name COLLATE NOCASE);

CREATE TABLE IF NOT EXISTS session_skills (
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  skill_id TEXT NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
  attached_at TEXT NOT NULL,
  PRIMARY KEY (session_id, skill_id)
);

-- Deleting a skill detaches it everywhere via ON DELETE CASCADE. That is the
-- intended behaviour: a skill that no longer exists cannot keep instructing
-- conversations. Runs already queued are unaffected, because the job payload
-- captured the instructions at enqueue time.
CREATE INDEX IF NOT EXISTS session_skills_by_skill ON session_skills (skill_id);
