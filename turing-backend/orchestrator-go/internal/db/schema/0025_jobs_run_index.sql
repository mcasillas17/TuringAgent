-- Every tool decision reads its run's job for the frozen tool selection, and
-- several other paths look a job up by its run. Without this index each of
-- those lookups scans every job ever queued.
CREATE INDEX IF NOT EXISTS idx_jobs_run ON jobs(run_id);
