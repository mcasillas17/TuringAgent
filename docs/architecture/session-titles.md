# Stable session titles

Session titles are durable orchestrator state. Flutter renders them, but it does
not derive, summarize, or persist them.

## Lifecycle

Flutter creates a conversation without a title. Until the user says something
usable, the client displays `New chat`; that string is a presentation fallback,
not the value written for a new session.

When a user message is accepted, the repository updates the session in the same
SQLite transaction that inserts the message, creates the assistant placeholder,
queues the run, and appends its events. A title is assigned only when the stored
title is null, empty, or the legacy literal `New chat`. A caller-supplied title
and a title derived by an earlier message are never replaced.

`DeriveSessionTitle` applies one deterministic policy:

1. Split on Unicode whitespace and join fields with one ASCII space, producing a
   single line.
2. Return no title for empty or whitespace-only content.
3. Keep input of at most 60 runes unchanged.
4. For longer input, keep at most 60 runes and append `…`. Prefer the last word
   boundary when that leaves at least 30 runes; otherwise hard-cut the first 60.

This is a truncation of the user's words, not a model-generated summary. It has
no model call, token cost, provider dependency, or nondeterministic output.

## Durable client updates

Every accepted message updates `sessions.updated_at` and appends a
`session.updated` event before `agent.run.queued`. The event is session-scoped,
has no `run_id`, and carries the authoritative metadata snapshot:

```json
{
  "title": "What is in the sandbox?",
  "updatedAt": "2026-08-18T20:00:00.000000000Z"
}
```

The event is committed with the message and job. Chat and automation publishers
then place it on the in-process event bus in persisted sequence order. A
subscriber that reconnects can replay the same event from the event log.

Flutter maps protocol event
`TURING_EVENT_TYPE_SESSION_UPDATED` to `session.updated`, applies `title` and
`updatedAt` to its local session list, and moves that session to the front. It
does not call `ListSessions` after sending a message. Search group headings load
the same stored session title, so the sidebar and search do not invent separate
names for one conversation.

Whitespace-only messages still produce a session update because they touch
`updated_at`, but their empty title snapshot keeps the `New chat` display
fallback. A later usable message can assign the title.

## Existing data and deletion

At startup, before gRPC servers accept subscriptions, the orchestrator scans
legacy sessions whose title is null, empty, or the old literal `New chat`. It
derives each title from that session's first stored user message using the same
function as the live path. The pass is idempotent, leaves explicitly named and
never-started sessions alone, and emits no live event because no subscriber can
exist yet. Startup fails if this compatibility pass cannot complete rather than
serving a partially repaired session list.

Deleting a session deletes its title with the session row. Existing foreign-key
cascades also remove its messages, jobs, runs, and durable `session.updated`
events, so neither search nor event replay can recover the deleted title.

## Configuration and limits

There is no title-related environment variable. The 60-rune and 30-rune
word-boundary thresholds are code-level UI contracts.

The implementation is rune-safe, not grapheme-cluster-aware. A combining
sequence or emoji ZWJ sequence that crosses the exact cutoff can end with an odd
final glyph. CJK characters can also occupy more rendered width than Latin
characters despite using the same rune budget.

## Verification

Focused checks from the repository root:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository \
  ./turing-backend/orchestrator-go/internal/service/events \
  ./turing-backend/orchestrator-go/internal/service/chat \
  ./turing-backend/orchestrator-go/internal/service/automations -count=1
( cd turing-client/turing_app && flutter test \
  test/models/grpc_mappers_test.dart \
  test/ui/shell_navigation_test.dart \
  test/features/search/search_screen_test.dart )
tools/proto/check.sh
```

Before opening a pull request, run the repository `/verify` skill. It covers the
root Go tests and race tests, all Go builds and separate MCP modules, Flutter
tests, deterministic proto generation, and all configured linters.
