# TUR-004 Session Deletion Withdrawal Design

## Goal

Deleting a session removes every session-owned application row and sandbox
artifact, stops existing subscribers with one typed terminal event, and makes
all later reads behave as if the session never existed. A deletion that cannot
remove an external artifact remains durably incomplete and never reports
success.

## Current boundary

`Repository.DeleteSession` currently scrubs affected audit payloads, deletes
the `sessions` root, and relies on foreign-key cascades plus the
`messages_fts_ad` trigger. That is the correct logical-delete primitive for
database-owned content, but it cannot coordinate live execution or external
filesystem effects. Deleting the event rows also leaves an existing event
subscriber without a terminal notification, while replaying a missing session
currently resembles an empty event history.

`mcp-files` operates on one shared sandbox root. It receives an
approval-bound JWT for mutation but no session/run provenance, so no durable
record can connect a sandbox path to a session.

## Non-goals

- This task does not add per-message deletion, curated memory, backups,
  encryption-at-rest, or a user-visible artifact browser.
- This task does not promise forensic overwrite of SQLite, a filesystem, or an
  SSD.
- This task does not create a retained/shared sandbox artifact type. Any future
  exception must use an explicit policy and a separately reviewed API.

## Deletion state machine

The orchestrator is the sole state-machine authority. A session is visible to
ordinary APIs only in `active` or `archived`; `deleting` is hidden and all
session-owned mutators reject it.

```text
ACTIVE/ARCHIVED
  | DeleteSession (single transactional compare-and-set)
  v
DELETING_QUIESCING
  | cancel/fence queued work; revoke unconsumed approvals; wait for active
  | execution acknowledgement and artifact write reservations
  v
DELETING_ARTIFACTS
  | delete every delete-on-session-delete artifact and namespace entry
  +-------------------+
  | external failure  | retry DeleteSession
  v                   |
FAILED_EXTERNAL -------+
  |
  | all external deletes succeed
  v
DELETED
```

The state is stored in a content-free `session_deletions` receipt keyed by
session id. It records a lifecycle version (the deletion generation), state,
terminal-event sequence/time, bounded counters, retryability, and a typed
opaque error class; it never contains a title, message, path, tool arguments,
tool result, credential, or external error text. It is a separately governed,
explicitly justified scrubbed-state exception in the MEM-001 schema manifest.
The existing audit tombstone remains the only retained audit content for source
withdrawal.

Beginning deletion changes `sessions.status` to `deleting` inside the same
transaction that creates or advances the receipt. Every session-rooted
operation checks the active status in its data mutation or read predicate;
none relies on the eventual cascade:

- `SendMessage`, idempotency replay, routing changes, approval creation and
  approval consumption reject a deleting session.
- Session, title, message, search, audit payload, event replay, job,
  approval, and artifact-listing reads exclude deleting and deleted sessions.
- A run already executing is terminalized as `cancelled`, its pending job and
  approvals are fenced, and physical deletion waits only through a bounded
  execution lease/drain window for its existing execution-exit acknowledgement.
  On lease expiry it remains `FAILED_EXTERNAL` with a typed opaque
  `execution_unreconciled` class; it does not cascade or claim success. Late
  worker reports use existing ownership/fence predicates and fail rather than
  recreating content.

`DeleteSession` is idempotent and never waits indefinitely. Its additive
response carries a typed `SessionDeletionReceipt` with
`IN_PROGRESS`, `FAILED_EXTERNAL`, or `COMPLETED`, its lifecycle version, a
retryability bit, and only the opaque error class. A concurrent or retrying
caller advances the same receipt; it cannot create a second receipt or terminal
event. A completed receipt returns the same session id and `COMPLETED`.
`FAILED_EXTERNAL` and `IN_PROGRESS` are truthful non-success responses that a
client can retry or poll; success is represented only by `COMPLETED` after all
steps below complete.

## Event contract

`TURING_EVENT_TYPE_SESSION_DELETED` is a terminal-only event. Its payload
contains only the stable deletion receipt id, lifecycle version, and counters,
never deleted content. It is sent exactly once to subscribers that were
authorized and attached when deletion began, then their stream closes with a
typed terminal-deletion reason.

The event is not appended to `events`: those rows must be removed with the
session. Instead, the event bus owns a dedicated non-droppable terminal slot
for each eligible subscription. Publishing a terminal event fences subsequent
publication for that session, wakes every subscription even if its ordinary
buffer overflowed, delivers the terminal event once, and closes the stream.
This preserves the existing durable replay repair for ordinary events without
preserving deleted event rows.

The receipt stores the terminal sequence as one greater than the pre-delete
event sequence. A stream may receive that sequence after a gap because all
intermediate events have been withdrawn; the server recognizes the terminal
event and never attempts to replay deleted rows. The service emits it only
after artifact cleanup, audit scrub, root cascade, and receipt completion
commit all succeed. A new `ListEvents` or `SubscribeSessionEvents` first
verifies an active session and returns `NotFound` for a deleting or deleted
one. Reconnection therefore cannot revive an empty stream or reveal a past
event.

The Flutter client treats `SESSION_DELETED` as final: it removes the session,
closes the chat event source, does not schedule a reconnect for that id, and
ignores all stale session-update or chat events. A pending external deletion is
shown as an unsuccessful delete operation; the client does not optimistically
discard a conversation until the terminal response/event.

## Sandbox artifact contract

All `mcp-files` artifacts created or changed by a run receive server-issued
session/run provenance. The public logical path remains a tool argument; a new
session-owned file's physical path is under an opaque sandbox namespace:

```text
/sandbox/sessions/<session-id>/runs/<run-id>/files/<logical-relative-path>
```

The orchestrator writes a durable `sandbox_artifacts` manifest row before
allowing the file mutation. Each row has a server-generated artifact id,
session id, run id, logical path hash, physical relative path, lifecycle
(`writing`, `ready`, `delete_failed`), and typed policy. New namespace files
use `delete_on_session_delete`. Migration-time/pre-existing sandbox files are
explicitly classified `retain_legacy_unowned`, are never retroactively claimed
or deleted, and a write to one preserves that policy while recording the
session/run provenance. Its foreign keys cascade from the session and run only
after external cleanup succeeds. The schema manifest classifies it as
session-owned derived state. Receipts report retained counts but never call
them withdrawn.

Each file tool receives a signed, short-lived provenance capability generated
by the orchestrator from server-owned run/session data. It is bound to agent,
tool, argument hash, session, run, deletion generation, operation, scoped
path, and expiry. `mcp-files` accepts it only in trusted `_meta`; neither a
model nor a caller can supply trusted provenance in tool arguments.
Approval-gated mutation also keeps the existing approval JWT and single-use
consumption. No new listener is introduced: the existing authenticated
internal orchestrator/MCP channel authorizes reservations and finalization.

For a mutation, the internal authorization RPC atomically checks that the
session is active and creates a bounded `writing` reservation before the MCP
server opens the target. The MCP server verifies the generation before I/O,
does the durable file write, verifies/finalizes after I/O, and returns
deletion-in-progress failure if the generation changed. If deletion starts
after reservation but before finalization, finalization records the artifact
and fails; the deletion worker removes it. If the tool crashes between write
and finalization, the pre-existing reservation, its bounded lease, and the
session namespace allow the deleter to enumerate and reconcile it. A stale
capability cannot finalize. Thus a delete/write race cannot return a successful
tool write that outlives the session.

Read, list, and search calls also require the provenance capability, verify its
generation before and after I/O, and resolve only the corresponding active
session namespace or explicitly retained legacy artifact. This prevents an old
runtime, reconnected client, or global sandbox traversal from listing a
deleted session's content.

Deletion deletes manifest-declared session-owned artifacts and scans the owned
namespace for reserved/orphaned entries before the database cascade. A
filesystem failure, expired reservation, unavailable MCP runtime, or manifest/
filesystem divergence sets `FAILED_EXTERNAL`, retains only safe receipt state
and manifest ownership, records a redacted audit result with opaque artifact
id/policy/state/error class, and prevents successful deletion. Retrying
continues idempotently across crash/restart, duplicate finalization, missing
files, and stale capabilities. The only retained exception is the documented
`RETAIN_LEGACY_UNOWNED` migration classification. A future
`RETAIN_USER_MANAGED` policy must define an explicit user owner, separate
non-session namespace, listing path, retention disclosure, and tests before it
is admitted.

## Finalization transaction

Once execution and filesystem reservations are reconciled and all artifact
deletions succeed, one repository transaction:

1. scrubs run-correlated and session-targeted audit payloads;
2. records a minimal `session.deleted` audit tombstone with counters;
3. deletes the session root, cascading messages, runs, jobs, events, tool
   calls, approvals, idempotency records, manifest rows, and FTS rows; and
4. completes the content-free deletion receipt.

The service publishes the in-memory terminal event only after this transaction
commits. A commit failure publishes nothing and leaves the receipt retryable.
An external cleanup failure never reaches this transaction.

## Privacy and physical-erasure language

TUR-004 guarantees **logical withdrawal**: application-owned rows and
queryable/rebuildable projections are removed or made unavailable immediately
while finalization is in progress, and no supported application API returns
deleted content after finalization.

It does not guarantee **physical erasure**. The application opens SQLite with
WAL enabled (`internal/db/connection.go`). SQLite documents WAL's associated
quasi-persistent `-wal` and `-shm` files and checkpointing behavior
([WAL mode](https://www.sqlite.org/wal.html)). SQLite also documents that
`secure_delete` has limitations, including FTS shadow-table data
([PRAGMA secure_delete](https://www.sqlite.org/pragma.html#pragma_secure_delete)).
Freed pages, WAL/shm files, filesystem snapshots, SSD wear leveling, copied
files, and user-created backups can retain bytes outside this task's control.
Checkpointing, `VACUUM`, and `secure_delete` are not described as a forensic
erasure guarantee.

Whole-database encryption with cryptographic key destruction is the credible
byte-withdrawal strategy to evaluate for **database retirement**, not a
per-session guarantee: SQLCipher documents a database key and encrypted export
mechanism ([SQLCipher API](https://www.zetetic.net/sqlcipher/sqlcipher-api/)).
Destroying one database key makes the whole database unavailable only if every
key wrapper and encrypted backup is also destroyed; it cannot selectively
erase one session from a database encrypted under a single key. A separate
feasibility design must assess a SQLCipher-compatible driver, WAL/shm and
backup coverage, OS-keystore envelope-key custody, migration/recovery,
key-loss behavior, and evidence-based disposal of every wrapped-key copy.
TUR-004 documents this evaluation and does not migrate encryption libraries.

## Test matrix

- Repository and gRPC end-to-end deletion delivers one terminal event to every
  authorized, deletion-start subscriber, closes it with the typed reason, and
  rejects replay/reconnect/list/read/search afterward.
- Event-bus overflow plus concurrent terminal deletion still delivers exactly
  one terminal event and no post-terminal event.
- Concurrent deletion with enqueue/idempotency replay, approval consume,
  worker terminal report, and file mutation fails closed without orphan rows or
  resurrected content.
- Active execution, queued jobs, pending/approved approvals, and tool calls
  reach fenced/cancelled states before finalization; offline runtime/MCP,
  lease expiry, and bounded drain failure remain durable `FAILED_EXTERNAL`
  without a false success.
- FTS, runtime recall search, session title/listing, audit read payload,
  events, jobs, approvals, idempotency records, manifests, and artifact
  list/search cannot return a deleted-session sentinel.
- Artifact prewrite reservation, crash between reserve/write/finalize,
  delete-during-write, duplicate finalization, missing file, stale capability,
  namespace scan, delete failure, and idempotent retry are covered in the
  orchestrator and mcp-files modules with deterministic fences rather than
  sleeps.
- Flutter removes a terminally deleted conversation, suppresses stale events,
  and does not reconnect a terminal stream.
