-- Telemetry: the columns a run needs so the orchestrator can answer "how much
-- did this cost me, and what left this machine" from its own database.
--
-- No new table. Almost everything the telemetry report needs is already
-- recorded — tool_calls has server, tool, status and duration_ms; agent_runs
-- has status, provider, model and timestamps; automation_runs marks the runs
-- nobody was present for. Aggregation reads those. What was missing was token
-- usage, which nothing captured anywhere, and per-run attribution of where a
-- routed message was actually sent.
--
-- Adding the columns here rather than in a side table also means they inherit
-- the deletion story unchanged: deleting a session cascades agent_runs, so a
-- conversation the user withdraws stops counting toward the totals. Telemetry
-- must not become the place a deleted conversation survives.

-- Tokens a PROVIDER REPORTED for this run, summed over its model turns.
--
-- NULL means the provider reported nothing, and that is a different fact from
-- 0. Ollama returns prompt_eval_count/eval_count on its terminal chunk and an
-- OpenAI-compatible endpoint returns a usage object, but neither is
-- guaranteed: an older Ollama, a proxy that strips fields, or a provider that
-- ignores stream_options all yield nothing. Every read of these columns has to
-- distinguish the two, and nothing is allowed to write an estimate into them.
-- A token count nobody measured is worse than none at all.
ALTER TABLE agent_runs ADD COLUMN input_tokens INTEGER;
ALTER TABLE agent_runs ADD COLUMN output_tokens INTEGER;

-- Where a run was sent when the conversation was routed off this machine.
-- NULL for the local assistant, which is the default and the overwhelming
-- majority.
--
-- Denormalised on purpose. The live routing lives in session_external_agent
-- and the agent's configuration lives in external_agents, but both can be
-- edited or deleted afterwards, and a record of what left the machine must not
-- be rewritten by a later settings change. Joining to the current routing
-- would answer "where would this go today", which is a different question and
-- the wrong one.
--
-- The host, not the URL: a base URL can carry a path, a query and, from a
-- careless paste, a credential. The host is the part that answers "who
-- received this" and is the only part worth keeping.
ALTER TABLE agent_runs ADD COLUMN external_agent_name TEXT;
ALTER TABLE agent_runs ADD COLUMN external_agent_host TEXT;

-- DELIBERATELY NO INDEX for the telemetry window.
--
-- The obvious one, on agent_runs(created_at), would never be used. Rows
-- written before 0005 use a shorter timestamp layout, so every window
-- predicate normalises through sqliteTimestampNanos() rather than comparing
-- the stored text — and a predicate that wraps its column in an expression
-- cannot seek an index on that column. EXPLAIN QUERY PLAN confirms it: a
-- covering-index scan, which is a full scan wearing a hat, plus a write cost
-- on the hottest table in the system.
--
-- This is a personal machine's database, and the reports are read by one
-- person on demand. If it ever needs an index, the shape is the one 0005
-- already established: a stored `created_at_ns` column, indexed, so the
-- predicate can be written against a plain column.
