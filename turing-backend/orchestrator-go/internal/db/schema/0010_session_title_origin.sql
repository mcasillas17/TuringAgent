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
  WHEN title IS NULL OR title = '' OR title = 'New chat' THEN 'unset'
  ELSE 'explicit'
END;
