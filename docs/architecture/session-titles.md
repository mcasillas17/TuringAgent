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
`title_origin` is `unset`. Creating a session with any non-empty explicit title
sets the origin to `explicit`; this includes the named conversations created for
automations. Assignment from a message changes it to `derived`. The text itself
is never used as a sentinel, so an explicit or derived title whose words are
exactly `New chat` is not replaced by a later message.

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
  "updatedAt": "2026-08-18T20:00:00.000000000Z",
  "status": "active"
}
```

The event is committed with the message and job. Chat and automation publishers
then place it on the in-process event bus in persisted sequence order. A
subscriber that reconnects can replay the same event from the event log.

Flutter maps protocol event
`TURING_EVENT_TYPE_SESSION_UPDATED` to `session.updated`, applies `title` and
`updatedAt` to its local session list, and sorts by that durable recency key.
The shell opens `SubscribeSessionUpdates`, which first replays the latest
durable snapshot for the same 50 most-recent sessions returned by the session
list and then receives live updates from every session, including inactive
conversations and new automation conversations. The service filters this
subscription to `session.updated` and `session.deleted`; the bus coalesces
backlog by session, so a slow
client retains each session's latest update without an unrelated event evicting
it. The active chat's per-session stream can deliver the same event; timestamp
idempotency makes the duplicate harmless.

Each global subscription performs one indexed replay query. Migration `0010`
adds a partial index containing only `session.updated` rows, so reconnect work
scales with title-update history and returns at most 50 rows; there is no timer
or polling loop. A terminal stream error or completion triggers a refresh and a
capped exponential reconnect (1, 2, 4, 8, 16, then 32 seconds). A durable event
does not reset recovery because it may be an initial replay snapshot; 30
uninterrupted seconds on the replacement stream reset the backoff. Per-session
chat events never affect global-stream recovery. Synchronous
factory/connect/listen failures use the same path as terminal stream errors.

A replayed older event cannot reorder the list. An update for a session outside
the loaded page inserts it, and a concurrent older `ListSessions` response is
merged without overwriting newer event state. Sessions created by this shell
stay retained until a server page observes them;
unknown global/per-session snapshots survive a concurrent refresh but expire
when a later page omits them, so off-page or remotely deleted rows are not
pinned. Older refresh responses are discarded by request generation. Ordering
matches the backend with an exact nanosecond key: `updatedAt` descending, then
session ID descending when timestamps tie. List/Get mappings read protobuf
seconds/nanos directly, and CreateSession returns the same exact key alongside
its display timestamp, so a newly created row does not lose sub-microsecond
ordering. Locally deleted IDs remain tombstoned for the shell lifetime because omission
from an active page cannot prove absence. The same rule protects archive state:
the latest authoritative archived snapshot remains guarded until a strictly
newer restore or deletion tombstone replaces it, so page omission or a delayed
legacy status-less event cannot resurrect the row. See
[Session lifecycle and pagination](session-lifecycle.md).
Flutter does not call `ListSessions` after sending a message. Search group
headings load the same stored session title, so the sidebar and search do not
invent separate names for one conversation.

Whitespace-only messages still produce a session update because they touch
`updated_at`, but their empty title snapshot keeps the `New chat` display
fallback. A later usable message can assign the title.

## Existing data and deletion

Migration `0010_session_title_origin.sql` first marks sessions linked from an
automation as `explicit`, including an automation legitimately named
`New chat`. It then classifies remaining legacy null, empty, and literal
`New chat` rows as `unset`; other existing non-empty titles become `explicit`.
New sessions record provenance at creation, so a new explicit `New chat` is
distinguishable from old placeholder data.

At startup, before gRPC servers accept subscriptions, the orchestrator scans
only sessions whose origin is `unset`. It derives each title from that session's
first usable stored user message using the same function as the live path,
skipping empty or whitespace-only turns, and marks the result `derived`. The
pass is idempotent, leaves explicitly named and never-started sessions alone,
and emits no live event because no subscriber can exist yet. Startup fails if
this compatibility pass cannot complete rather than serving a partially
repaired session list.

Deleting a session deletes its title with the session row. Existing foreign-key
cascades also remove its messages, jobs, runs, and durable `session.updated`
events, so neither search nor event replay can recover the deleted title.

`session.updated` is not a deletion event. Completed withdrawal calls
`Bus.TerminateSession`, which publishes `session.deleted` to that session's
subscribers and to every `SubscribeSessionUpdates` subscriber. The production
Flutter shell opens the global stream and removes a row when it receives that
terminal event, even if the chat is not open; a completed deletion receipt
also removes the local row. A disconnected client that misses the live event
uses list refresh as its reconnect/fallback reconciliation.

`TestBusPublishesTerminalDeletionToGlobalSubscribers` in
`turing-backend/orchestrator-go/internal/service/events/bus_test.go` covers the
global publication; `ResponsiveShell._applyGlobalSessionUpdated` handles
`session.deleted` through the same local deletion path.

## Configuration and limits

There is no title-related environment variable. Derived titles use the 60-rune
and 30-rune word-boundary thresholds above. Explicit rename titles are trimmed
and limited independently to 120 Unicode scalar values.

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
