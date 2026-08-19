-- Title provenance separates an untitled or legacy-placeholder row from a
-- deliberate title whose text happens to be "New chat". The text alone cannot
-- make that distinction, and treating it as a sentinel lets a later message
-- rewrite a valid explicit or derived title.
ALTER TABLE sessions ADD COLUMN title_origin TEXT NOT NULL DEFAULT 'unset'
  CHECK (title_origin IN ('unset', 'explicit', 'derived'));

-- Before this column existed, every stored "New chat" came from the Flutter
-- placeholder. Other non-empty titles were supplied explicitly. The startup
-- backfill will turn legacy placeholders into derived titles.
UPDATE sessions
SET title_origin = CASE
  WHEN EXISTS (
    SELECT 1 FROM automations WHERE automations.session_id = sessions.id
  ) THEN 'explicit'
  WHEN title IS NULL OR title = '' OR title = 'New chat' THEN 'unset'
  ELSE 'explicit'
END;

-- The shell-wide update stream replays one latest title snapshot per session
-- on connect. Keep that read proportional to session-update rows rather than
-- every token/tool/run event in the log.
CREATE INDEX IF NOT EXISTS idx_events_session_updates
  ON events(session_id, sequence DESC)
  WHERE type = 'session.updated';
