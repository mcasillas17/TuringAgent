# TUR-008 session lifecycle and pagination design

**Status:** Approved on 2026-08-19 at commit `0378f6a`

## Purpose

TUR-008 makes the existing session contract truthful and complete:

- session recency follows durable user-visible activity;
- `ListSessions` honors bounded cursor pagination;
- active and archived sessions have explicit, reversible lifecycle operations;
- rename and archive state propagate through durable session snapshots;
- backend and Flutter inputs have enforced limits; and
- malformed input fails predictably without exposing cursor, secret, or session
  content.

Current `main` already contains part of the intended activity behavior from
TUR-007. The accepted-message transaction updates `sessions.updated_at`, assigns
the initial derived title, and appends `session.updated` before it queues the
run. Exact `SendMessage` idempotency replay returns before those writes, so a
replay does not currently reorder a session. This work preserves that behavior,
hardens its timestamp invariant, and completes the remaining public surfaces.

## Goals

1. Return stable keyset pages ordered by `updated_at DESC, id DESC`.
2. Keep a pagination traversal stable when newer sessions are inserted.
3. Reject invalid page sizes and every invalid cursor shape with stable gRPC
   status codes.
4. Make active, archived, rename, archive, and restore behavior explicit.
5. Keep session metadata changes and their durable `session.updated` snapshots
   atomic.
6. Make Flutter consume pagination and lifecycle operations without allowing a
   stale RPC or event to resurrect an archived conversation.
7. Preserve protobuf wire compatibility and all compatible behavior already on
   `main`.
8. Preserve TUR-004 deletion gates, terminal events, tombstones, artifact
   cleanup, and client behavior if that work lands before TUR-008 finishes.

## Non-goals

- Archive is not deletion, content withdrawal, cancellation, or a write lock.
- Pagination is not a long-lived database snapshot of mutable rows.
- TUR-008 does not add cross-device synchronization beyond the existing durable
  `session.updated` stream.
- TUR-008 does not change message-history pagination or search pagination.
- TUR-008 does not replace the existing string-valued `Session.status` field.

## Public protocol

All protobuf changes are append-only. Existing field numbers and RPCs remain
unchanged.

### Session list filter

Add a `SessionListFilter` enum:

```proto
enum SessionListFilter {
  SESSION_LIST_FILTER_UNSPECIFIED = 0;
  SESSION_LIST_FILTER_ACTIVE = 1;
  SESSION_LIST_FILTER_ARCHIVED = 2;
  SESSION_LIST_FILTER_ALL = 3;
}
```

Append `SessionListFilter filter` to `ListSessionsRequest`. `UNSPECIFIED`
resolves to `ACTIVE`, so the normal conversation list excludes archived
sessions. `ALL` is explicit and never selected by omission.

No public archive operation exists today, so defaulting to active does not
change reachable behavior for existing clients unless a database was edited
outside the application.

### Lifecycle RPCs

Add request and response messages and these RPCs:

```proto
rpc RenameSession(RenameSessionRequest) returns (RenameSessionResponse);
rpc ArchiveSession(ArchiveSessionRequest) returns (ArchiveSessionResponse);
rpc RestoreSession(RestoreSessionRequest) returns (RestoreSessionResponse);
```

Each success response contains the authoritative `Session` snapshot. Separate
archive and restore methods make archive idempotence explicit and avoid a
boolean mutation whose intent is unclear in logs, tests, and callers.

`GetSession`, rename, restore, and delete may address archived sessions.
Archive and restore do not cancel active runs. An automation or other caller may
continue to append activity to an archived session; the session stays archived
and its recency changes within the archived list. Restoration is always
explicit.

If TUR-004 has landed, deleting or deleted lifecycle state dominates active and
archived visibility. Such rows remain hidden, and rename, archive, restore, and
activity use TUR-004's centralized deletion gates and public errors. TUR-008
must not duplicate or weaken those gates.

`ALL` means the union of publicly visible active and archived rows after those
same centralized deletion predicates. It is not a bypass around lifecycle
visibility and does not expose deleting, deleted, unknown, or future internal
states. A stored status other than the supported public `active` or `archived`
values fails repository/service mapping with a value-free `Internal` error.

### Limits and normalization

The server is authoritative for every limit:

| Input | Contract |
| --- | --- |
| `page.limit` | `0` means `50`; explicit values must be `1..100` |
| encoded cursor | at most 2,048 bytes before decoding |
| `session_id` | required where applicable; valid UTF-8, no control characters, at most 256 bytes |
| create title | `TrimSpace`; empty is allowed; at most 120 Unicode scalar values |
| rename title | `TrimSpace`; nonempty; at most 120 Unicode scalar values |

The title limit is counted after trimming and counts Go runes, matching Unicode
scalar values rather than UTF-8 bytes. A nonempty normalized create title is
stored with `title_origin = 'explicit'`; an empty normalized create title keeps
`title_origin = 'unset'` and remains eligible for derived naming.

Session IDs are not trimmed or rewritten. Validation first requires valid UTF-8
and the byte bound, then rejects any decoded rune for which Go
`unicode.IsControl` returns true. Structurally invalid values are rejected,
while valid unknown IDs reach the repository and return `NotFound`. Flutter
provides the same user-facing title bound, but the backend remains authoritative.

Negative or excessive page limits, excessive IDs, and invalid titles return
`InvalidArgument`. Unknown valid IDs return `NotFound`. Internal storage and
mapping failures return `Internal` without stored values.

Renaming to the same normalized title is a no-op only when `title_origin` is
already `explicit`. Renaming an automatically derived title to the same visible
text changes its origin to explicit, advances the activity timestamp, and
emits a snapshot so later title derivation cannot reinterpret the user's
choice.

## Session timestamps

### Canonical representation

All session timestamps use exactly:

```text
2006-01-02T15:04:05.000000000Z
```

A new lower-level `internal/persisttime` package owns the layout, canonical
formatter, strict canonical parser, and migration-only legacy parser. Both the
`db` migration runner and repository import it, avoiding the import cycle that
would result if `db` called repository helpers. Existing repository timestamp
helpers delegate to this package so unrelated callers keep one representation.
Every session write or read path changed by TUR-008 uses these functions.
Service mapping returns an error for malformed stored timestamps instead of
silently returning a protobuf message with a missing timestamp.

### Migration

The current migration runner already has version-specific behavior around
`0011_file_skills`, but ordinary migrations execute only an embedded SQL file.
TUR-008 adds a second, database-only hook at the existing transaction boundary:
after `ApplyMigrationsWithSkillsRoot` opens the migration's `*sql.Tx` and before
it executes that migration's index DDL, a version switch calls
`normalizeSessionTimestamps(ctx, tx)`. The SQL file contains only the two index
changes. Timestamp rewrites, index DDL, and the `schema_migrations` insert
therefore share the same transaction and commit.

`normalizeSessionTimestamps` uses `internal/persisttime`'s migration-only legacy
parser and canonical formatter rather than SQLite date functions:

1. read sessions in stable ID order in fixed-size batches;
2. parse both timestamps with Go's RFC3339Nano parser, rejecting malformed
   values and unsupported forms with one fixed, value-free migration error;
3. format both parsed instants through the canonical nine-digit UTC formatter;
4. update each row with an old-value guard inside the same transaction; and
5. finish all timestamp batches before the migration runner executes either
   index statement.

Batching bounds memory and closes each read cursor before updates on SQLite's
single connection. There are no network calls, filesystem effects, or waits in
the transaction. Its duration is necessarily proportional to the existing
session count because all rows must be made safe before either ordering index
can be trusted.

Any parse, guarded-update, or DDL error rolls back timestamp rewrites, both index
changes, and the migration record. A crash before commit has the same result.
After a successful commit, the migration record prevents a rerun. After a
rollback, a retry is idempotent because the parser accepts both supported legacy
forms and already-canonical output.

Migration tests cover whole-second, trimmed fractional, offset-to-UTC, and
canonical legacy forms plus malformed date, zone, suffix, and fraction cases.
They also prove that one malformed row rolls back earlier row rewrites, index
DDL, and the migration record.

TUR-004 currently proposes migration number `0014`. If it lands first, this
migration uses the next available number instead of reusing `0014`.

### Monotonic activity

For a real session mutation, the next timestamp is:

```text
max(wall_clock_now, current_session_updated_at + 1ns)
```

Accepted-message enqueue also ensures its user-message timestamp is strictly
after the latest stored message timestamp. The transaction therefore selects
the greater of the current session timestamp and latest message timestamp
before choosing the accepted-message timestamp.

These reads and writes occur in the same short SQLite transaction as the
durable activity. The invariant is:

- a committed real mutation advances `sessions.updated_at`;
- a rolled-back or rejected mutation does not;
- exact idempotent `SendMessage` replay does not;
- an archive or restore request already in the requested state does not; and
- rename with the same normalized text and already-explicit origin does not.

The extra nanosecond prevents a backward wall-clock adjustment, a prior rename,
or a prior archive transition from moving a conversation backward in recency.
No retry or replay may manufacture recency.

## Repository lifecycle operations

Rename, archive, and restore each run in a bounded transaction:

1. load and validate the current lifecycle row through the centralized session
   gate;
2. detect a semantic no-op;
3. choose a strictly newer canonical timestamp for a real change;
4. update title/origin or status;
5. append one durable `session.updated` event carrying the authoritative
   snapshot; and
6. commit before returning the event for publication.

The payload is:

```json
{
  "title": "Quarterly planning",
  "status": "active",
  "updatedAt": "2026-08-20T04:00:00.000000001Z"
}
```

Accepted-message snapshots add the same `status` field. Existing consumers
ignore an added JSON field, and Flutter treats a historical snapshot without a
status as active for replay compatibility.

The repository returns the authoritative session, whether a change occurred,
and the committed event when one exists. The service publishes only after
commit. A no-op has no event and no timestamp change.

SQLite's single configured connection serializes concurrent writes. Last
committed real mutation wins, but every committed event carries a strictly
increasing timestamp. No transaction remains open while publishing to the bus
or waiting on a client.

## Pagination

### Query

The repository accepts a resolved filter, optional validated anchor, and a
validated limit. It fetches `limit + 1` publicly visible rows. The simplified
active shape is:

```sql
WHERE status = ?
  AND (
    updated_at < ?
    OR (updated_at = ? AND id < ?)
  )
ORDER BY updated_at DESC, id DESC
LIMIT ?
```

The first page omits the anchor predicate. The real query also includes the
centralized TUR-004 visibility predicate when that lifecycle exists. `ALL`
selects only public active/archived values, applies the same deletion predicate,
and uses its dedicated ordering index. Active and archived pages use the
composite status index.

The extra row only proves that another page exists. It is never returned. When
there is another page, `next_cursor` anchors the last emitted row, not the
overfetched row. A full final page with no extra row has an empty cursor.

Changing to another valid page size while resuming is supported. Page size is
not cursor-bound.

### Stable-under-insert guarantee

The keyset anchor prevents a newly inserted row that sorts ahead of the anchor
from shifting or duplicating later results. Equal timestamps remain
deterministic because session ID is the second descending key.

Rows are mutable: activity, rename, archive, and restore can move an existing
row after a page has been read. A row that moves above the current anchor may be
omitted from that traversal. This is the deliberate stateless-keyset trade-off;
clients restart at page one when they need a current view. TUR-008 does not hold
a database snapshot across requests or create server-side cursor state.

A cursor remains usable if its anchor row was deleted because the query uses
the anchor values, not row existence.

### Index verification

Repository tests seed realistic cardinality and run `EXPLAIN QUERY PLAN` for
the actual deletion-gated query builders used in production:

- active/archived filtered pages; and
- `ALL` pages.

The assertions require the intended index and reject a session table scan. They
are updated with TUR-004's real predicate rather than testing a simplified
query. Data tests separately cover ordering, strict public-status mapping,
equal-timestamp tie-breaking, overfetch, filter isolation, and inserts between
pages.

## Authenticated cursor format

### Secret ownership

The existing `TURING_INTEGRATION_KEY` is orchestrator-only, but it is optional
and scoped to third-party credential encryption. Reusing it would couple
credential-key rotation and cursor invalidation. The approval JWT root is
distributed to approval-verifying services and is also unsuitable.

Add a dedicated `TURING_CURSOR_HMAC_SECRET`:

- `init.sh` generates 32 random bytes as 64 lowercase hex characters;
- real app configuration requires and validates exactly 32 decoded bytes;
- Compose distributes it only to the orchestrator;
- `.env.example` and operator documentation list it without a value; and
- the value is never logged, returned, or sent to another service.

There is no random-key fallback in app construction. Tests use an explicit
constructor or fixture key. Rotating the key intentionally invalidates every
outstanding cursor, which clients observe as the same value-free
`InvalidArgument` used for any invalid cursor.

### Binary body

The cursor is raw, unpadded base64url over one canonical binary representation:

| Field | Encoding |
| --- | --- |
| magic | four fixed ASCII bytes identifying a session-list cursor |
| schema version | one byte; initial value `1` |
| resolved filter | one byte for active, archived, or all |
| `updated_at` | exactly 30 ASCII bytes in the canonical session layout |
| session ID length | unsigned 16-bit big-endian |
| session ID | exact validated UTF-8 bytes |
| MAC | 32-byte HMAC-SHA256 |

The MAC is exactly:

```text
HMAC-SHA256(
  cursor_secret,
  "turing.session-list.cursor.v1\x00" || body_without_mac
)
```

The quoted ASCII bytes plus the terminating zero byte are the immutable domain
prefix. `body_without_mac` contains every binary field through the session ID.
The page limit is intentionally absent.

Fixed binary encoding prevents duplicate JSON keys, field reordering, alternate
number spellings, and trailing JSON tokens from creating alternate accepted
encodings. The decoder:

1. rejects encoded input over 2,048 bytes;
2. decodes with strict raw unpadded base64url only, then requires re-encoding
   the decoded bytes to reproduce the input exactly;
3. performs only the bounded envelope reads needed to obtain the declared ID
   length, prove the exact total size, and locate the final 32-byte tag;
4. computes and compares the MAC in constant time before trusting any semantic
   field; and
5. only after MAC success validates magic, version, filter byte, canonical
   timestamp, UTF-8/control-free ID, and equality with the request's resolved
   filter.

All failures return exactly `InvalidArgument("page.cursor is invalid")`. No
branch echoes the cursor, its anchor, a session ID, or a MAC. A cursor from
another endpoint, filter, schema version, install, or signing-key generation is
foreign and fails the same way.

## gRPC service behavior

`ListSessions` resolves and validates every client value before querying. It
decodes the cursor, asks the repository for `limit + 1`, maps session timestamps
strictly, and returns a non-nil `PageResponse` following existing conventions.

Lifecycle methods validate IDs and titles before opening a repository
transaction. Typed repository errors map to:

| Repository result | gRPC code |
| --- | --- |
| invalid structural input | `InvalidArgument` |
| unknown session | `NotFound` |
| TUR-004 lifecycle conflict | TUR-004's existing public code |
| unexpected database/event/mapping failure | `Internal` |

No public error includes a title, cursor, secret, or stored session content.

## Flutter behavior

### Models and networking

Add status to the Flutter `Session` model and gRPC mapper. Add a `SessionPage`
model containing sessions and `nextCursor`.

The production gRPC client exposes filter, cursor, and limit and preserves
`PageResponse.next_cursor`. The existing active-list convenience remains
available so unrelated fakes and call sites do not need pagination knowledge.

### Active list

The active sidebar loads page one and shows a `Load more` affordance while a
cursor exists. Loading another page merges by session ID and keeps the backend
order `(updated_at DESC, session_id DESC)`.

Each session row has an overflow menu:

- Rename
- Archive
- Delete

Rename uses a prefilled dialog. It trims and enforces the same visible
120-scalar bound, then replaces the row only with the authoritative RPC
response. Archive removes the row and clears the active conversation only after
the RPC succeeds.

### Archived sessions

The Chats header opens an `Archived chats` surface. It requests the archived
filter, paginates with the returned cursor, and offers:

- Rename
- Restore
- Delete

Restore removes the row from the archived surface and refreshes or inserts the
authoritative active snapshot. Archive and restore are not presented as
deletion and do not reuse deletion confirmation copy.

### Reconciliation

Flutter compares authoritative snapshots using exact nanosecond timestamps and
the session-ID tie-break used by the backend. Every real state change has a
newer timestamp, so an older list response, RPC response, replayed event, or
accepted-message event cannot replace a newer rename/archive/restore snapshot.

An archived snapshot removes the session from the active list and remains as a
process-lifetime authoritative reconciliation guard. Omission from an active
page proves nothing about archive state and never evicts that guard. Only a
strictly newer status-aware active restore snapshot or a stronger deletion
tombstone replaces it. A stale active page cannot resurrect it. Historical
`session.updated` payloads without status resolve to active only when no
status-aware guard exists; once an archived guard exists, a status-less event
cannot restore that session.

Deletion tombstones remain stronger and longer-lived than archive guards.
TUR-004 terminal deletion events and local tombstones, if present, always win
over active/archived snapshots.

## Test strategy

Strict TDD applies: every item below starts as a failing test.

### Migration and configuration

- required cursor secret missing, malformed, and valid cases;
- `init.sh` generation, preservation, and permissions;
- Compose secret distribution only to the orchestrator;
- legacy timestamp normalization and malformed-row rollback;
- both session indexes and both query plans.

### Repository

- active, archived, and all filters;
- exact order and equal-timestamp ID tie-breaking;
- `limit + 1`, final-page behavior, and cursor anchor selection inputs;
- insertion between pages without duplicate or shifted results;
- monotonic activity after a newer session mutation or backward clock;
- exact `SendMessage` replay and failed/conflicting enqueue do not touch;
- rename normalization, bounds, explicit-origin promotion, and true no-op;
- archive/restore state, true no-op, active-run behavior, and not-found cases;
- atomic session row plus durable snapshot;
- concurrent rename/archive/activity serialization;
- TUR-004 deletion gate precedence when available.

### Public gRPC

- default and explicit limits, negative and excessive limits;
- complete multi-page traversal and changed page size on resume;
- cursor tamper, padding, alphabet, oversize, truncation, trailing bytes, magic,
  version, filter, timestamp, ID length, wrong secret, and foreign endpoint;
- cursor reuse under another filter;
- non-nil `PageResponse`, correct empty final cursor, and last-emitted anchor;
- create/rename/ID input bounds and status codes;
- archive/restore idempotence and authoritative responses;
- post-commit event publication only.

### Flutter

- page and status mapping with exact nanoseconds;
- active `Load more` ordering and duplicate suppression;
- rename success, validation, no-op, and failure state;
- archive success/failure and stale-page/event suppression;
- delayed legacy status-less event after an active refresh omission;
- archived pagination, rename, restore, and delete;
- newer restore accepted after an archive guard;
- existing deletion tombstone behavior remains unchanged.

## Documentation and rollout

Implementation updates:

- the TUR-008 status in the personal-agent audit;
- a focused session lifecycle architecture document;
- `docs/architecture/session-titles.md` for status-aware snapshots;
- backend/root configuration documentation for the cursor secret; and
- Flutter client documentation for rename, archive, restore, and pagination.

Generated Go and Dart protobuf output is committed using
`tools/proto/generate.sh`. The implementation is merged with current
`origin/main` before final review; it never rebases or force-pushes. If TUR-004,
TUR-003, or TUR-006 has landed, their public contracts are preserved.

## Retained risks

- A mutable row can move above a cursor and be omitted from an in-progress
  traversal. Restarting at page one is the documented recovery.
- Cursor-secret rotation invalidates outstanding cursors by design.
- Archive does not stop active or automated work; users must delete or cancel
  under the corresponding contracts when they intend withdrawal or execution
  control.
- SQLite remains a single-connection store. The design uses short bounded
  transactions and one overfetch row, but large local session histories still
  rely on the two ordering indexes for predictable reads.
