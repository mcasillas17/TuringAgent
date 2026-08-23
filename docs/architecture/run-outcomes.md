# Durable Run Outcomes

TUR-009 makes one stored run row the authoritative answer to "what happened to
this run." Message history and live events project that same answer, so reopening
a conversation cannot turn a failed, cancelled, recovering, or silent completed
run into an unexplained empty assistant turn.

## Authority and public projection

`agent_runs` is the sole durable lifecycle and outcome authority. Event rows are
ordered delivery projections, not competing state, and clients do not rebuild
outcomes by replaying events.

The canonical row uses:

- existing `id`, `session_id`, `user_message_id`, `assistant_message_id`,
  `status`, and `finished_at`;
- `state_version`, a per-run signed 64-bit version starting at 1;
- `state_updated_at`, fixed-width UTC nanoseconds;
- `outcome_reason`, a closed public category;
- `assistant_content_sha256`, an internal digest of the exact persisted
  assistant bytes used only to identify duplicate terminal reports.

Internal worker, attempt, execution, lease, token, diagnostic, and content-digest
fields never appear in public `RunState`.

`SessionService.ListMessages` attaches `RunState` to the correlated assistant
message with one zero-or-one join. The public snapshot contains run,
user-message, and assistant-message IDs; lifecycle; outcome reason; version;
state-update time; terminal time when applicable; and whether the canonical
assistant content is displayable.

Only the event types whose own repository writer commits that snapshot may
ever carry a typed `RunState`: `agent.run.queued`, `agent.run.started`,
`agent.run.state_changed`, `agent.run.completed`, `agent.run.failed`,
`agent.run.cancelled`, `approval.requested`, and `approval.approved`.
`approval.requested` carries it through two writers: primarily the
`running -> waiting_approval` transition itself, and as a fallback — carrying
the same run state as it stands — when the run was already waiting on an
earlier approval and this request does not itself move the lifecycle.
`approval.approved` never moves a run's lifecycle (approving only records a
decision; a separate resume is what moves the run), so its snapshot always
comes through that same fallback. Every other type — every `message.*` and
`tool.call.*` event, `agent.run.step`, `system`, `error`, `session.*`, and any
type this build does not recognize — never projects a typed `RunState`, even
when its stored payload contains a well-formed value under a `runState` key
that correctly names the row's own run. `events.Decode` is the one place that
gate is enforced: it resolves the row's canonical type once and only offers
the payload to the RunState projection when that type is one of the eight
carriers above, so ChatService and EventService cannot come to different
answers about the same row, and a pre-migration row of a non-carrier type
cannot smuggle a forged, high-version snapshot in as canonical truth.

## Lifecycle, reasons, and content

The durable lifecycles are `queued`, `running`, `waiting_approval`,
`recovering`, `completed`, `failed`, and `cancelled`. Terminal lifecycles are
immutable. `recovering` is public because uncertain worker ownership is not
honestly described as running.

| Lifecycle | Allowed outcome reasons |
|---|---|
| queued, running, waiting approval, recovering | `none` |
| completed with displayable content | `none` |
| completed without displayable content | `completed_no_content` |
| failed | `expired`, `context_limit`, `provider_failure`, `tool_failure`, `policy_denied`, `retries_exhausted`, `recovery_interrupted`, `side_effect_uncertain`, `approval_delivery_failed`, `internal_failure` |
| cancelled | `user_cancelled`, `abandoned` |

Unknown protobuf numerics map to semantic unknown behavior and generic localized
copy; raw integers are never rendered. TUR-009 does not write an unknown
lifecycle. The current transport cannot distinguish deliberate cancellation
from connection loss, so its `client_cancelled` path is `abandoned`.
`user_cancelled` is reserved for a future typed cancel-intent API.

Displayable content contains at least one scalar outside the explicit Unicode
White_Space table shared by Go and Dart. Original bytes are preserved. Only an
explicit successful runtime report may complete a run. Empty or whitespace-only
success is `completed/completed_no_content`; EOF, disconnect, and transport
cancellation never synthesize success. Content already durable before failure or
cancellation stays before the outcome card.

## Ordering and exactly-once transitions

Every real public transition:

1. checks the allowed source lifecycle, expected version, complete message
   correlation, and any worker/attempt/approval identity;
2. increments `state_version` exactly once;
3. writes `state_updated_at = max(now_utc, prior + 1ns)`;
4. commits subsidiary rows and one canonical lifecycle event in the same short
   SQLite transaction.

Versions are `1..9223372036854775807`; zero is protobuf absence. Version or
timestamp exhaustion fails closed without a row or event write. Reconciliation
uses only `(run_id, state_version)`, not timestamps, event sequence, or lifecycle
rank.

Existing lifecycle events carry the resulting state. A transition without an
existing event type uses `agent.run.state_changed`. A terminal transition emits
only its existing completed, failed, or cancelled event, never redundant
terminal noise. Subsidiary writes occur before the state projection in the same
transaction, so the event describes committed truth.

An exact duplicate transition is a write-free replay only when expected and
resulting versions, lifecycle, outcome, correlation, trigger identity, content
presence, and terminal content digest all agree. Completion additionally
requires exact content bytes. A different version, reason, assistant identity,
approval, worker, assignment attempt, or content conflicts. Terminal states
cannot be revived.

## Correlation and legacy fallback

One run owns at most one assistant message, and one assistant message belongs to
at most one run. Partial unique indexes enforce both non-null directions.
Enqueue creates both sides in its existing bounded transaction and validates
matching run ID, assistant-message ID, assistant role, and session before any job
or event insert. Claim ordering uses the initiating user-message sequence, so a
corrupt earlier active run cannot be skipped in favor of later work.

Migration derives lifecycle first. Every nonterminal legacy row must pass the
complete shared correlation validator. A null, one-sided, role-mismatched,
session-mismatched, or duplicate nonterminal link aborts startup with the
value-free `run/message correlation conflict` class. All migration work,
including its record, rolls back.

A terminal legacy row does not need to make future progress. A single null or
unusable terminal link therefore remains as neutral history. History omits
`RunState`, and Flutter renders "No assistant response was recorded" rather than
inventing success or failure. Duplicate ownership in either direction is always
ambiguous and still aborts migration.

## Typed failure and redaction boundary

Provider, tool, worker, and generic diagnostic strings are untrusted. The shared
`runoutcome` package accepts a typed origin, an allowlisted internal code, and an
internal automatic-retry class. It returns only a typed origin, safe code, closed
outcome category, and internal retry policy. Unknown codes fall back by trusted
origin family; unknown origins become internal failure. Classification never
searches error-message text.

`RunState` and public failure-like events exclude provider errors, worker
messages, credentials, tool arguments and results, approval tokens, file paths,
database paths, worker IDs, attempt IDs, and content digests.

| Public event | Durable public failure data |
|---|---|
| `agent.run.failed`, `agent.run.cancelled` | canonical `RunState`; no raw code, message, or reason |
| failure-like `agent.run.step` | state version, allowlisted category, and counters satisfying `1 <= attempt <= max_attempts <= 1000` |
| `approval.denied`, `approval.expired` | intended IDs plus `policy_denied` or `expired` category |
| `tool.call.failed`, `tool.call.denied` | intended tool-call identity plus `tool_failure` or `policy_denied` category |

Human approval comments and denial rationale remain governed approval/audit data.
They are not copied into `RunState` or failure events. Public ChatService and
EventService readers sanitize malformed legacy payloads again as defense in
depth.

Automation summaries expose the durable terminal status but never project the
legacy `agent_runs.error_message`. The retained `last_run_error` protobuf field
is wire-compatible and empty; until that surface carries a typed outcome
category, Flutter renders a neutral "The last run failed" notice and links to
the conversation for the canonical run card.

## Assignment and approval fencing

Every `AgentJob` includes the state version at assignment and a durable
`assignment_attempt_id`. The worker echoes the expected version on terminal
reports and the attempt ID when resuming an approval. A stale predecessor cannot
change a run owned by a later attempt.

The direct `running -> queued` edge is limited to a transaction that proves the
assignment was not delivered (`pending_send`) or that the authenticated current
attempt released it as a same-run transient failure. Delivered, uncertain,
fenced, expired, or otherwise unresolved execution goes through durable
`recovering` before requeue or terminalization.

Approval resume is a two-phase fence:

| Boundary | Durable result |
|---|---|
| Decision cannot be delivered before Ready while ownership is proven | Stay `waiting_approval`; worker-loss reconciliation may later enter `recovering` |
| Matching Ready arrives | Validate run, approval, worker, attempt, and expected version; atomically commit `waiting_approval -> running` |
| Accepted is delivered | Worker may continue the approved tool/model work |
| Accepted delivery fails after the commit | Fence `running -> recovering`; never revert to waiting |
| Accepted was sent but not observed | Same identity/version Ready replays the exact Accepted without a write |
| Ownership is lost after an unobserved Accepted | Fence `running -> recovering` |

## Flutter live/reopen parity

History first creates message rows, then reconciles their embedded states. Live
and replayed states use the same pure rules:

1. reject absent/invalid state or a state for an unloaded assistant row;
2. accept the first valid state for a run;
3. drop a lower version;
4. ignore an equal identical state;
5. reject an equal conflicting state;
6. reject every update after a terminal state;
7. otherwise accept only a defined higher-version lifecycle transition.

Completed content renders without a redundant success card. Completed with no
content renders a neutral completion card. A queued, running, waiting-approval,
or recovering run with no displayable content suppresses the blank bubble and
renders an adjacent localized nonterminal status card from either history or
live state. Failed and cancelled runs render localized terminal cards; any
preserved content remains first. A completed state that promises content not
loaded locally renders "Response unavailable" and requests a bounded resync.
Missing state on an empty legacy assistant row uses the neutral fallback.

Before the newest history page commits, a run-ID-keyed buffer retains at most 64
highest-version states. Overflow clears it and schedules one newest-page reload.
After initial load, states for unloaded messages are discarded and coalesce into
one resync, plus at most one follow-up when events arrive during an in-flight
request. Older pages are non-destructive: overlapping boundaries deduplicate by
message ID and run ID/version, and stale pages cannot replace newer terminal
truth or erase optimistic content.

Historical tool cards and nonterminal run-notice placement remain unavailable
because persisted messages do not carry an event/message interleaving key.

## Migration guarantees

Migration `0017_run_outcomes` rebuilds `agent_runs` on one pinned SQLite
connection because it is the parent of run-owned child tables. Foreign keys are
disabled only on that pinned connection before the transaction, all populated
children remain in place, `foreign_key_check` must pass, and the runner proves
foreign keys were restored before returning the connection.

The before hook, sectioned SQL rebuild/scrub/index work, after hook, and
`schema_migrations` insert share one transaction. Run and event keyset scans are
capped at both 128 rows and 16 MiB of selected variable-width data. Length is
accounted before materializing values; one oversized row fails with a value-free
sentinel. Cursors close before writes. The migration performs no filesystem,
model, tool, or network work.

The migration backfills version 1, canonical time, lifecycle, content presence,
content digest, and normalized outcome; rewrites failure-like events; nulls raw
diagnostic messages; bounds safe codes; validates canonical fields and
correlation; and creates the two unique indexes. Any validation, JSON,
timestamp, SQL, hook, or injected failure rolls back the schema, child data,
scrub, events, indexes, and migration record together.

Populated run-owned children, including `run_egress_decisions` and idempotency
replay rows, survive the parent rebuild byte-for-byte. Legacy TUR-003 terminal
codes `egress_decision_required` and `egress_decision_invalid` normalize to the
closed public `policy_denied` reason; their raw diagnostic text and failure-event
payload are scrubbed in the same transaction. Reapplying the idempotency key
after migration returns the original terminal run without another write.

## Retained limitations

- There is no no-worker or queue-timeout policy; TUR-010 owns it.
- Historical tool-card reconstruction is unavailable.
- There is no explicit user-cancel intent API; current transport cancellation is
  abandonment.
- Partial live deltas are not guaranteed to survive reopen.
- Live tool-separated text segments collapse into one persisted assistant
  message after reopen.
- Only the new run-state copy is localized.
