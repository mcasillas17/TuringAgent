# Session lifecycle and pagination

The orchestrator owns session ordering, visibility, names, and lifecycle state.
Clients render the authoritative `Session` snapshots returned by RPCs and
`session.updated` events.

## Ordering and activity

Session lists use the deterministic order
`(updated_at DESC, session_id DESC)`. Persisted timestamps use UTC with exactly
nine fractional digits.

Creating a session establishes its initial activity time. Accepting a new user
message advances `updated_at` atomically with the message, assistant placeholder,
job, and durable events. Rename, archive, and restore advance it atomically with
their durable `session.updated` snapshot. When wall-clock time has not advanced,
the repository uses the later of the current session time and latest message
time, plus one nanosecond.

Exact `SendMessage` replay, conflicting replay, failed transactions, repeated
archive/restore, and renaming an already-explicit title to the same normalized
text do not advance activity. Renaming a derived title to the same visible text
does persist the explicit title origin and therefore advances activity.

## Listing and cursors

`ListSessions` defaults to active sessions. Callers can request archived
sessions or all publicly visible active and archived sessions. Internal or
unsupported persisted statuses fail closed; the `ALL` filter never bypasses
deletion visibility rules.

The page size defaults to 50 and must be between 1 and 100. A response includes
`next_cursor` only when another row exists. The cursor anchors the last emitted
row, and callers may resume it with a different valid page size.

Cursors are opaque, versioned, raw-unpadded-base64url values authenticated with
HMAC-SHA256 under the orchestrator-only `TURING_CURSOR_HMAC_SECRET`. They bind
the schema version and list filter, contain no session title or message content,
and use one value-free `InvalidArgument` response for malformed, tampered,
foreign-filter, or otherwise invalid values. Rotating the signing key
intentionally invalidates outstanding cursors.

Descending keyset pagination is stable under concurrent inserts: a newly
inserted row ahead of the anchor does not shift later pages. It is not a
snapshot of mutable rows. If an existing session receives activity between page
requests, it can move ahead of the anchor and be omitted from that traversal;
a movement in the other direction can produce a repeat. Clients should start a
fresh traversal when they need a current first page.

## Rename, archive, restore, and delete

`RenameSession`, `ArchiveSession`, and `RestoreSession` return the authoritative
post-commit `Session`. Rename trims surrounding whitespace, rejects an empty
result, and limits the normalized title to 120 Unicode scalar values. Session
IDs are valid UTF-8, at most 256 bytes, and contain no rune for which Go
`unicode.IsControl` is true. A nonempty normalized create title is explicit; an
empty create title remains eligible for deterministic derived naming.

Archive is reversible visibility state, not deletion. Archived sessions leave
the default active list but remain addressable by get, rename, restore, and
permanent delete operations. Archiving does not cancel an active run or disable
an automation; either can continue to add durable activity while the session
remains archived. Restoring makes the session visible in the active list again.

Permanent deletion is a separate lifecycle with its own centralized gates and
cleanup. Archive must never expose a deleting/deleted session or weaken those
rules.

## Flutter behavior

The Flutter sidebar loads the active list a page at a time and preserves the
server cursor for **Load more**. Selected rows expose rename, archive, and
permanent delete actions. **Archived conversations** opens an independently
paginated view with rename, restore, and permanent delete actions. Refreshing
the first active page preserves rows already loaded from later cursor pages and
keeps the deepest continuation cursor instead of replaying those pages.

The client preserves protobuf timestamp nanoseconds and orders rows by the same
backend key. RPC results, list pages, and status-aware events are reconciled by
authoritative session snapshot. An archived snapshot is retained for the
process lifetime until a strictly newer active restore or deletion tombstone
replaces it. Omission from an active page does not clear that guard, and a
delayed legacy `session.updated` event without status cannot resurrect an
archived row.
