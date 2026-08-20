# TUR-009 Reopenable Run Outcomes Design

**Date:** 2026-08-20
**Status:** Pending coordinator spec approval

## Goal and Scope

Reopening a conversation must show the same versioned run lifecycle truth that
the user saw live. An empty assistant message must never appear without an
adjacent, human-readable explanation.

The change covers queued, running, waiting-for-approval, recovering, completed,
failed, and cancelled runs. It includes successful runs with no assistant
content and runs terminalized by lease recovery. It does not add the no-worker
or queue-timeout policy tracked by TUR-010, and it does not reconstruct
historical tool cards.

## Existing Failure

Enqueue atomically creates the user message, an empty assistant placeholder,
the run, the job, initial events, and the optional send-message idempotency
record. Completion fills the assistant message, while failure and cancellation
normally leave it empty.

The run row and terminal event both persist outcome data, but
`SessionService.ListMessages` returns only the message. Flutter separately
replays all events from sequence zero and suppresses events at or below its
startup watermark. That suppression prevents duplicate live entries, but it
also drops the only failure or cancellation explanation during reopen.

Provider, runtime, and tool failure strings currently enter run, job, tool-call,
and approval-related event rows. Some then reach public ChatService and
EventService responses. Flutter also renders several backend strings directly.

Worker loss is stored as `status='running'` with an uncertain or fenced
execution state while the lease policy decides whether to requeue or
terminalize. Calling this interval running is not honest because no worker is
known to own forward progress.

## Selected Approach

`agent_runs` becomes the sole durable authority for public lifecycle and
outcome. `SessionService.ListMessages` returns a normalized run snapshot on the
correlated assistant message. Existing and new lifecycle events carry the same
snapshot as ordered delivery projections.

This produces one ordered history snapshot, avoids N+1 queries and
cross-request races, supports old rows without terminal events, and gives live
and reopened rendering one typed input.

The rejected alternatives are:

1. Reconstructing from event history, which preserves competing row/event
   truth, cannot recover missing legacy events, and has no reliable
   message/event interleaving key.
2. A separate run-history API, which adds pagination, another round trip, and
   snapshot reconciliation without improving the existing message correlation.

## Canonical Durable Model

The migration filename is selected only after current `origin/main` is merged.
No design assumes that the next migration number is `0014`.

The canonical `agent_runs` row retains its existing identifiers,
`assistant_message_id`, `status`, `finished_at`, and internal execution fields.
The migration adds:

- `state_version INTEGER NOT NULL`;
- `state_updated_at TEXT NOT NULL`;
- `outcome_reason TEXT NOT NULL`;
- `assistant_content_sha256 TEXT NOT NULL`.

The existing `status` constraint is rebuilt to include `recovering`; no second
durable lifecycle column is introduced.

`state_version` is a per-run monotonic signed 64-bit integer. A newly queued run
starts at version `1`. Existing rows migrate to version `1`.
`state_updated_at` starts at the run creation timestamp and existing rows
backfill from `finished_at`, then `started_at`, then `created_at`.

Every real public lifecycle transition increments `state_version` exactly once
and updates `state_updated_at` in the same transaction as its event projection.
Semantic no-ops and matching duplicate reports do not increment it. Updates
guard both the expected lifecycle and expected version.

The maximum version is SQLite's signed integer maximum,
`9,223,372,036,854,775,807`. A transition at that value fails closed with a
value-free `run state version exhausted` error and performs no write or event
append. Stored versions must be in the inclusive range `1..MaxInt64`; zero and
negative values are rejected. Version zero remains the protobuf
absence/default value.

`state_updated_at` uses the repository's fixed-width UTC nanosecond format,
`2006-01-02T15:04:05.000000000Z`. A real transition writes
`max(now_utc, prior_state_updated_at + 1ns)` in the guarded transaction; a
semantic no-op preserves the prior timestamp. If parsing fails or adding one
nanosecond would exceed the inclusive UTC range
`0001-01-01T00:00:00.000000000Z` through
`9999-12-31T23:59:59.999999999Z`, the transition fails closed without changing
the row or appending an event. Reconciliation still uses only `state_version`.

`outcome_reason` is `none` for every nonterminal lifecycle. Terminal lifecycle
and reason combinations are validated by this normative matrix:

| Lifecycle | Allowed outcome reasons |
|---|---|
| queued, running, waiting approval, recovering | none |
| completed with displayable content | none |
| completed without displayable content | completed no content |
| failed | expired, context limit, provider failure, tool failure, policy denied, retries exhausted, recovery interrupted, side effect uncertain, approval delivery failed, internal failure |
| cancelled | user cancelled, abandoned |
| unknown | unknown or legacy unknown |

Unspecified values are invalid in canonical rows. TUR-009 does not persist or
emit the unknown lifecycle; inconsistent legacy message correlation omits
`RunState` instead.
Existing `error_code`, `error_message`, and `cancellation_reason` cease to be
public outcome authority.

`assistant_content_sha256` is an internal lowercase SHA-256 identity of the
exact persisted assistant content bytes. It starts as the empty-content hash,
is backfilled from the message row, and changes only in the same transaction
that changes message content. It is never returned publicly.

## Public Protobuf Contract

Every new enum has both an `UNSPECIFIED = 0` value for wire absence and an
explicit nonzero `UNKNOWN` value for values a newer server may introduce.

`RunLifecycle` contains:

- `RUN_LIFECYCLE_UNSPECIFIED`;
- `RUN_LIFECYCLE_UNKNOWN`;
- `RUN_LIFECYCLE_QUEUED`;
- `RUN_LIFECYCLE_RUNNING`;
- `RUN_LIFECYCLE_WAITING_APPROVAL`;
- `RUN_LIFECYCLE_RECOVERING`;
- `RUN_LIFECYCLE_COMPLETED`;
- `RUN_LIFECYCLE_FAILED`;
- `RUN_LIFECYCLE_CANCELLED`.

`RunOutcomeReason` contains:

- `RUN_OUTCOME_REASON_UNSPECIFIED`;
- `RUN_OUTCOME_REASON_UNKNOWN`;
- `RUN_OUTCOME_REASON_NONE`;
- `RUN_OUTCOME_REASON_COMPLETED_NO_CONTENT`;
- `RUN_OUTCOME_REASON_USER_CANCELLED`;
- `RUN_OUTCOME_REASON_ABANDONED`;
- `RUN_OUTCOME_REASON_EXPIRED`;
- `RUN_OUTCOME_REASON_CONTEXT_LIMIT`;
- `RUN_OUTCOME_REASON_PROVIDER_FAILURE`;
- `RUN_OUTCOME_REASON_TOOL_FAILURE`;
- `RUN_OUTCOME_REASON_POLICY_DENIED`;
- `RUN_OUTCOME_REASON_RETRIES_EXHAUSTED`;
- `RUN_OUTCOME_REASON_RECOVERY_INTERRUPTED`;
- `RUN_OUTCOME_REASON_SIDE_EFFECT_UNCERTAIN`;
- `RUN_OUTCOME_REASON_APPROVAL_DELIVERY_FAILED`;
- `RUN_OUTCOME_REASON_INTERNAL_FAILURE`;
- `RUN_OUTCOME_REASON_LEGACY_UNKNOWN`.

`RunState` contains:

- run ID;
- initiating user message ID;
- assistant message ID;
- lifecycle;
- outcome reason;
- state version;
- state-updated timestamp;
- finished timestamp when terminal;
- whether the canonical assistant message has displayable content.

`RunState` has no retryable field. Automatic same-run retry is an internal
dispatch decision, and the product has no guarantee that repeating the user's
request is safe. The existing public `RunFailed.retryable = 4` field cannot be
removed without breaking wire compatibility; it is marked deprecated, is
always serialized as false for new events, and is ignored by new clients.

`Message` receives an additive `run_state` field. Completed, failed, and
cancelled stream messages receive the same additive `RunState`. Existing field
numbers and legacy fields are preserved. New clients use `RunState`; legacy
code/message/reason fields contain only safe generic values.

The exact additive allocations are:

| Message | Allocation |
|---|---|
| `Message` | `RunState run_state = 9` |
| `RunState` | run ID `1`, user message ID `2`, assistant message ID `3`, lifecycle `4`, outcome reason `5`, state version `6`, state-updated timestamp `7`, finished timestamp `8`, displayable-content flag `9` |
| `RunQueued` | `RunState run_state = 4` |
| `RunStarted` | `RunState run_state = 4` |
| `ApprovalEvent` | `RunState run_state = 4` when that approval event changes run lifecycle |
| `RunCompleted` | `RunState run_state = 3` |
| `RunFailed` | `RunState run_state = 5`; field `4` remains deprecated |
| `RunCancelled` | `RunState run_state = 3` |
| `RunStateChanged` | `RunState run_state = 1` |
| `ChatStreamEvent.oneof` | `RunStateChanged run_state_changed = 27` |
| `TuringEvent` | `RunState run_state = 9` |
| `TuringEventType` | `TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED = 23`; value `22` is reserved for TUR-004's `SESSION_DELETED` whether or not that PR has landed when generation runs |

Internal runtime allocations are `AgentJob.expected_state_version = 17`,
`RuntimeRunCompleted.expected_state_version = 6`,
`RuntimeRunFailed.failure_origin = 5`,
`RuntimeRunFailed.automatic_retry_class = 6`,
`RuntimeRunFailed.expected_state_version = 7`,
`RuntimeCancelledAck.observed_state_version = 2`,
`RuntimeApprovalResumeReady.run_id = 1`,
`RuntimeApprovalResumeReady.approval_id = 2`,
`RuntimeApprovalResumeReady.expected_state_version = 3`,
`RuntimeUpdate.approval_resume_ready = 9`,
`RuntimeApprovalResumeAccepted.run_id = 1`,
`RuntimeApprovalResumeAccepted.approval_id = 2`,
`RuntimeApprovalResumeAccepted.state_version = 3`,
`RuntimeCommand.approval_resume_accepted = 7`,
`RuntimeRunCancelled.state_version = 3`,
`RuntimeApprovalUpdated.state_version = 4`, and
`ToolPolicyDecision.run_state_version = 7`. Existing internal field numbers are
unchanged; `RuntimeUpdate` value `8` remains
`worker_capabilities_updated`. Existing runtime retryable field `4` is
deprecated and ignored by the new normalizer.

`FailureOrigin` assigns unspecified `0`, unknown `1`, then the exhaustive
origins below. `AutomaticRetryClass` assigns unspecified `0`, unknown `1`,
never `2`, and same-run-transient `3`. Unknown/unspecified retry class is
treated as never. It is internal dispatch policy, not a user retry promise.

## Lifecycle and Version Transitions

The public lifecycle transitions are:

| From | To | Trigger |
|---|---|---|
| absent | queued | atomic enqueue |
| queued | running | assignment starts |
| running | waiting approval | approval becomes required |
| waiting approval | running | the owned runtime attempt durably acknowledges that it resumed after the decision |
| running or waiting approval | recovering | worker ownership becomes uncertain or fenced |
| recovering | running | the same fenced attempt proves ownership and resumes |
| recovering | queued | lease recovery requeues the job |
| queued, running, waiting approval, or recovering | failed | canonical failure or exhausted recovery |
| queued, running, waiting approval, or recovering | cancelled | typed cancellation or abandonment terminalization |
| running, waiting approval, or recovering | completed | explicit successful terminal report |

Terminal states are immutable.

Every existing lifecycle event payload gains the resulting `RunState`. A
transition that does not already have a run lifecycle event appends
`agent.run.state_changed`. Terminal transitions append exactly one existing
terminal event (`agent.run.completed`, `agent.run.failed`, or
`agent.run.cancelled`) and do not append an additional state-changed event.

For every nonterminal lifecycle command, the canonical transition identity is
the run ID, expected version, target lifecycle, outcome `none`, and correlated
message IDs. If the row is already at `expected_version + 1` with that exact
identity, a repeated assignment-start, approval wait/resume, recovery fence,
requeue, or resume command returns the current state without a write or event.
A different target, outcome, correlation, or version is a conflict. Advisory
attempt counts and notices are projections and cannot turn an otherwise equal
canonical transition into a second state change.

Entering recovery is therefore durable and observable:

1. Worker send/receive loss changes the run from running or waiting approval to
   recovering, increments the version, records the update time, and appends the
   state projection in the fencing transaction.
2. While the execution lease is unresolved, reopen and live streaming both show
   recovering rather than running.
3. A matching owned attempt may return the run to running; otherwise lease
   recovery moves it to queued or a terminal outcome.
4. TUR-010 still owns how long a queued run may wait without a worker.

Internal send states such as pending-send, sending, delivered, uncertain, and
fenced remain execution details. They affect the public lifecycle only at the
transitions above.

Approval persistence, token consumption, or queuing `RuntimeApprovalUpdated`
does not move the run back to running. An approved worker sends the new
`RuntimeApprovalResumeReady` update only after it has accepted the decision and
restored the matching owned attempt to a ready-but-paused boundary. The
orchestrator validates run, approval, worker, attempt, and expected version,
then commits waiting-approval to running with its state event and returns
`RuntimeApprovalResumeAccepted` carrying the approval identity and new version.
The worker does not execute the approved tool or continue model work until it
receives that acceptance. A delivery failure or restart leaves the run
waiting-approval while ownership is still known, or moves it to recovering when
ownership is lost. Denial and expiry follow their existing terminal paths.

## Correlation and Query Invariants

One run owns at most one assistant message, and one assistant message belongs
to at most one run.

Current schema does not enforce both directions, so the migration:

1. Runs a value-free preflight for duplicate `agent_runs.assistant_message_id`
   values and duplicate assistant `messages.run_id` values.
2. Aborts without changing the database if a duplicate exists; the error does
   not include message content, IDs, or paths.
3. Adds a partial unique index on non-null
   `agent_runs.assistant_message_id`.
4. Adds a partial unique index on non-null assistant `messages.run_id`.

Enqueue remains the only writer that creates this circular correlation. Inside
its existing transaction it creates both rows and then calls one shared
correlation validator before any event or job insert. The validator requires
matching run IDs, assistant message IDs, session IDs, and assistant role.
Repository APIs do not expose link-update operations. The same validator is
used by migration preflight and joined history reads. The unique indexes,
existing foreign key, writer boundary, and value-free read failure together
enforce the invariant without relying on client deduplication.

Null or mismatched legacy correlation is not rewritten speculatively.
`ListMessages` joins by the message's scalar run ID to the run primary key and
requires the mirrored assistant message ID, role, and session to match before
returning `RunState`. That join is zero-or-one and cannot fan out a message row.

A duplicate in either correlation direction is different from a nullable or
single mismatched legacy row: it makes ownership ambiguous and intentionally
aborts startup. Migration surfaces only the sentinel remediation class
`run/message correlation conflict`; errors and logs contain no row IDs, message
content, or database paths. The operator must restore a consistent database
from a backup before retrying. Null or single mismatches retain the neutral
legacy fallback below.

If correlation is absent or inconsistent, the service omits `RunState`.
Flutter renders a neutral "No assistant response was recorded" card for a
non-displayable assistant message without usable state. It does not fabricate a
failed or completed lifecycle. `RUN_LIFECYCLE_UNKNOWN` and
`RUN_OUTCOME_REASON_LEGACY_UNKNOWN` are reserved for an explicit future
canonical unknown state; this migration does not synthesize one.

The existing message page boundary and limit are applied to message rows with a
zero-or-one primary-key join, so an embedded state cannot change page
cardinality. A state card is created only with its assistant row and remains
adjacent to it. Overlapping page loads deduplicate messages by message ID and
run states by run ID plus state version.

Flutter has one initial-load race buffer keyed by run ID, capped at 64 states
and retaining only the highest version for each run. It exists only until the
initial newest message page is committed. Historical/replayed events for
unloaded messages are discarded because the later message page carries the
authoritative snapshot. If more than 64 distinct live run states arrive before
the page commits, the buffer is cleared, one resync-required flag is set, and
one newest-page reload runs after initial load; further events are not retained.
After initial load, an event for an unloaded message is discarded and
coalesces into at most one newest-page resync request. No arbitrary event or
run-state collection waits for older pages.

When TUR-008 lands, its cursor, archive, and status-aware reconciliation remain
authoritative. TUR-009 does not create a second paging cursor or session status.

## Typed Failure Normalization

Failure normalization happens before any durable run, job, tool-call,
machine-generated approval failure, or event write. Human approval comments and
denial rationale remain governed audit input, not failure-normalizer input.

Runtime failure reports add a typed `FailureOrigin` supplied by the call site.
That internal protobuf enum also has `UNSPECIFIED = 0` and explicit `UNKNOWN`
values.
The normalizer accepts typed origin, an allowlisted internal code, and internal
automatic-retry policy. It never classifies by searching provider-controlled
message text. Unknown or unspecified origins fail closed to internal failure;
unknown codes from a typed provider origin become provider failure; unknown
codes from a typed tool origin become tool failure.

Provider adapters may classify from protocol facts such as HTTP status and
typed provider error code/type allowlists. They do not classify public outcome
from the provider's message. Runtime-supplied retry booleans are ignored for
unknown origin/code pairs.

The normalizer returns only:

- a typed failure origin;
- an allowlisted internal code;
- a closed `RunOutcomeReason`;
- an internal automatic-retry decision where the existing dispatch policy
  needs one.

No arbitrary diagnostic message is retained. If an operational fingerprint is
later proven necessary, it requires a separate design; TUR-009 does not add one.

A shared `runoutcome` package closes the ingestion boundary. Its normalized
failure values have private fields and can be created only by typed,
allowlisting constructors. Repository run-failure, job-failure, tool-after,
approval-terminalization, and failure-event writers accept those normalized
values instead of code/message/reason strings. Existing raw-string writer
signatures are removed rather than left as bypasses. Event builders accept
canonical `RunState` or normalized subsidiary categories only.

`FailureOrigin` is exhaustive:

- unspecified;
- unknown;
- context assembly;
- external provider;
- provider configuration;
- provider protocol;
- provider transport;
- provider output guard;
- tool infrastructure;
- tool execution;
- tool guard;
- tool policy;
- approval transport;
- approval expiry;
- automation policy;
- worker runtime;
- dispatch;
- recovery;
- orchestrator internal;
- client lifecycle.

Unrecognized protobuf enum numerics are handled at every Go and Dart mapping
boundary with a default branch that returns semantic unknown behavior. Go
mapping functions switch on the generated enum value and map `default` to the
domain unknown. Dart mapping functions accept the generated value or null and
map any value outside the accepted set, including an unspecified/default value
on a present `RunState`, to the domain unknown. An absent `RunState` still uses
the neutral legacy fallback. Neither mapper calls a generated name accessor
without a default, renders the raw integer, or panics. An unknown lifecycle
renders a neutral "Run status unavailable" state; an unknown outcome renders
generic outcome-unavailable copy; an unknown failure origin normalizes to
internal failure. Raw-wire compatibility tests inject numeric values absent
from the generated enum in both Go and Dart.

### Existing Run-Terminal Code Mapping

| Existing code or condition | Typed origin | Public outcome |
|---|---|---|
| `message_fetch_failed` | context assembly | internal failure |
| `external_agent_unavailable` | external provider | provider failure |
| `model_provider_unavailable` | provider configuration | provider failure |
| `tool_discovery_failed` | tool infrastructure | tool failure |
| `context_budget_exceeded` | context assembly | context limit |
| `model_timeout` | provider transport | provider failure |
| `model_stream_failed` | provider transport | provider failure |
| `model_output_limit_exceeded` | provider output guard | provider failure |
| `model_unavailable` | provider protocol | provider failure |
| `model_auth_failed` | provider protocol | provider failure |
| `model_request_failed` | provider protocol | provider failure |
| `model_error` | external provider | provider failure |
| `model_quota_exceeded` | provider protocol | provider failure |
| `model_bad_chunk` | provider protocol | provider failure |
| `model_stream_error` | provider transport | provider failure |
| `tool_call_failed` | tool execution | tool failure |
| `tool_call_limit_exceeded` | tool guard | tool failure |
| `tool_result_limit_exceeded` | tool guard | tool failure |
| `runtime_error` | worker runtime | internal failure |
| `retries_exhausted` | dispatch | retries exhausted |
| `job_timeout` | recovery | recovery interrupted |
| `side_effect_uncertain` | recovery | side effect uncertain |
| `approval_delivery_failed` | approval transport | approval delivery failed |
| `approval_expired` | approval expiry | expired |
| `automation_approval_failed` | automation policy | policy denied |
| `automation_tool_not_allowlisted` | automation policy | policy denied |
| explicit successful report with no displayable content | not a failure | completed with no content |
| current `client_cancelled` transport path | client lifecycle | abandoned |
| a future explicit typed cancel-intent RPC | client lifecycle | user cancelled |
| unknown code with typed external-provider/protocol/transport origin | matching provider origin | provider failure |
| unknown code with typed tool origin | tool | tool failure |
| unknown or unspecified origin | unknown | internal failure |

`worker_busy` and `worker_unavailable` are nonterminal dispatch/recovery
conditions. They keep `outcome_reason=none` while the lifecycle moves through
recovering or queued. If the attempt budget is exhausted, the terminal outcome
is retries exhausted or recovery interrupted according to the existing
terminal path.

The current ChatService uses the same stream-cancellation signal for an
intentional stop and an unkeyed transport loss, and Flutter exposes no explicit
cancel affordance. TUR-009 does not add a cancel-intent RPC. Current and legacy
`client_cancelled` rows therefore map to abandoned, never user-cancelled.
`USER_CANCELLED` remains reserved until a future explicit typed intent path can
prove it. Tests lock the ambiguous disconnect mapping so it cannot silently
regress to a claim about user intent.

### Subsidiary Failure Code Mapping

These codes do not independently determine the run outcome, but their durable
public event payloads are normalized before write:

| Existing code | Typed origin | Safe public category |
|---|---|---|
| `tool_policy_decision_failed` | tool policy | policy denied |
| `tool_policy_decision_invalid` | tool policy | policy denied |
| `approval_wait_failed` | approval transport | approval delivery failed |
| `mcp_call_failed` | tool execution | tool failure |
| `unknown_tool` | tool infrastructure | tool failure |
| `unknown_tool` | tool policy | policy denied |
| `tool_runner_unavailable` | tool infrastructure | tool failure |
| `worker_unavailable` notice | recovery | recovery interrupted |
| `cancelled` tool cleanup from the current transport path | client lifecycle | abandoned |
| unknown typed tool code | tool | tool failure |
| unknown or unspecified subsidiary origin | internal | internal failure |

## Public Failure Event Inventory

The following durable public failure-like paths are in scope for ingestion
normalization, migration rewriting, and defense-in-depth read sanitization:

| Event type | Safe payload after TUR-009 |
|---|---|
| `agent.run.failed` | canonical `RunState`; no raw code/message |
| `agent.run.cancelled` | canonical `RunState`; no arbitrary reason |
| `agent.run.step` retry/give-up/recovery notices | state version plus allowlisted category and numeric attempt metadata; no persisted `note` |
| `approval.denied` | IDs already intended by contract plus allowlisted category; no message |
| `approval.expired` | IDs already intended by contract plus allowlisted category; no message |
| `tool.call.failed` | tool-call identity already intended by contract plus allowlisted category; no error message or result |
| `tool.call.denied` | tool-call identity already intended by contract plus allowlisted category; no arbitrary policy text |

`message.completed` contains assistant output rather than a failure diagnostic
and remains unchanged. Approval tokens are never added to events.
Nonfailure `agent.run.step` projections, including redacted egress and model
limit notices, are preserved; only retry, give-up, and recovery failure payloads
are rewritten by this migration.

TUR-002's human approval rationale is not a generic failure diagnostic.
Migration never changes `approvals.approval_comment`,
`approvals.denial_reason`, or the governed bounded rationale projection in
`audit_logs`. Sanitizing `approval.denied` event payloads removes only generic
machine message/reason fields; the rationale remains in its approval and audit
storage and is never copied into `RunState` or a failure event.

ChatService and EventService sanitize malformed or unmigrated legacy rows again
at the public read boundary. Malformed payloads map to the enum unknown/internal
fallback and never return parser text. Flutter derives all user-visible copy
from exhaustive enum switches through Flutter localization resources; it never
renders backend `message`, `note`, `reason`, or unknown code strings. The
current app has no general localization framework, so TUR-009 introduces the
built-in Flutter localization plumbing and English resources only for these new
run-state cards. It does not migrate unrelated existing UI copy.

## Migration and Legacy Scrubbing

The migration runner gains a version-keyed Go hook with `Before` and `After`
phases that execute inside the same `*sql.Tx` as the migration SQL. For ordinary
migrations behavior is unchanged. For the selected post-merge TUR-009 version
the runner performs:

1. `before` hook;
2. the version's SQL file;
3. `after` hook;
4. `schema_migrations` insert;
5. transaction commit.

The `before` hook performs the value-free correlation preflight, creates a
temporary backfill table inside the transaction, and scans existing runs by
stable keyset in batches of at most 128. Each `Rows` cursor is read into a
bounded Go slice and closed before any insert or update on SQLite's single
connection. The hook parses legacy `finished_at`, `started_at`, and `created_at`
only in the accepted RFC3339Nano shapes
`YYYY-MM-DDTHH:MM:SS[.1-9 digits]Z` and
`YYYY-MM-DDTHH:MM:SS[.1-9 digits](+|-)HH:MM`, using TUR-008's lower-level
`persisttime.ParseLegacy` when PR #68 has landed, or introducing that exact
shared lower-level parser if it has not. It formats every derived timestamp with
`persisttime.Format` and the fixed-width canonical UTC layout, hashes exact
assistant content, and writes only normalized lifecycle/outcome values to the
backfill table. Invalid timestamps fail with a value-free migration error;
variable-width or offset source text is never copied into `state_updated_at`.

The SQL file rebuilds `agent_runs` with the recovering status and canonical
constraints, copying derived values from the backfill table. After all read
cursors are closed it swaps the rebuilt table, creates the correlation indexes,
and scrubs run/job/tool-call raw diagnostic columns. SQLite transactional DDL
keeps the rebuild and indexes inside the migration transaction.

The `after` hook scans selected events by `rowid` in batches of at most 128,
closes each cursor before writes, rewrites safe canonical JSON using Go's JSON
encoder, validates the rebuilt correlations and canonical fields, and drops the
temporary backfill table. It makes no filesystem, model, tool, or network
calls.

Within that mechanism, the migration:

1. Validates the correlation uniqueness preconditions without returning row
   values.
2. Rebuilds or extends `agent_runs` for recovering status and canonical state
   fields.
3. Backfills version `1`, state-updated time, lifecycle, displayable-content
   presence, and normalized outcome from status, execution state, assistant
   content, cancellation reason, and allowlisted existing codes.
4. Maps active uncertain/fenced execution to recovering.
5. Adds both partial correlation unique indexes.
6. Sets `agent_runs.error_message`, `jobs.error_message`, and
   `tool_calls.error_message` to NULL.
7. Replaces non-allowlisted legacy error codes with a safe internal/provider/
   tool code selected from trusted row context.
8. Rewrites the known public failure event payloads above to their safe
   canonical projection or category, using the run's migrated version.

It never copies arbitrary error text into canonical state or safe event
payloads. Public read sanitization remains defense in depth for malformed rows
that cannot be normalized.

Any hook, SQL, validation, JSON, timestamp, or injected test error rolls back
the shadow-table work, table swap, raw-field scrub, event rewrites, indexes, and
`schema_migrations` row together. Tests inject failures after each phase and
after the migration-record insert but before commit, then reopen the database
to prove the pre-migration schema and data remain intact.

Approval rows have no error-message column. Their argument JSON and tokens,
plus tool-call argument JSON and result summaries, remain internal operational
state; TUR-009 does not expose them through failure events or `RunState`.

Terminal rows without terminal events still reopen from the canonical run row.
Old clients continue to decode unchanged existing field numbers.

## Content Presence

Displayable assistant content is content containing at least one Unicode scalar
outside this exact Unicode White_Space set:

- U+0009 through U+000D;
- U+0020;
- U+0085;
- U+00A0;
- U+1680;
- U+2000 through U+200A;
- U+2028, U+2029, and U+202F;
- U+205F;
- U+3000.

The exact original content bytes are persisted; whitespace classification is
used only to compute the boolean. Backend and Flutter implement this explicit
table rather than delegating to runtime-specific `trim` behavior. Shared
conformance vectors classify empty, ASCII whitespace, NBSP, EM SPACE, and
IDEOGRAPHIC SPACE as non-displayable; `a`, ZERO WIDTH SPACE (U+200B),
replacement character (U+FFFD), and whitespace surrounding text as
displayable. Flutter uses the canonical historical boolean and the same table
for live bubble visibility.

Only an explicit successful terminal report may commit completed. Empty or
whitespace-only successful content commits
`completed/completed_no_content`, preserves the empty original content, and
renders a neutral completion card. Provider stream EOF, worker disconnect, or
transport cancellation without an explicit success report moves through
recovering or a failure outcome and never becomes completed.

The current runtime synthesizes fallback text for an empty successful model
result and for an empty result at the tool-iteration limit. TUR-009 removes both
success fallbacks so explicit empty completion can be represented honestly.

Any assistant content already durable when failure or cancellation occurs is
preserved byte-for-byte and renders before the outcome card. Current live
message deltas are event rows and do not update the canonical message until
completion; therefore partial live deltas generally do not survive reopen.
TUR-009 does not claim or add partial-delta persistence.

## Terminalization and Duplicate Semantics

Each runtime assignment carries the run's expected state version. Every
run-related orchestrator command that changes public lifecycle carries the
resulting version, and the worker retains the highest accepted version for that
assignment. Terminal reports echo that expected version. A terminal transition
succeeds only from an allowed nonterminal lifecycle at the expected version,
then increments once and appends exactly one terminal event projection.

A duplicate report is a semantic no-op only when all canonical identity
matches:

- expected pre-terminal state version and resulting terminal version;
- terminal lifecycle;
- outcome reason;
- assistant message identity;
- exact `assistant_content_sha256` and displayable-content flag for every
  terminal outcome;
- for completion, the exact reported content bytes also match the persisted
  message.

An identical duplicate returns success, does not increment, and does not append
an event. The same terminal lifecycle with different content, reason, message
identity, or version expectation is a fenced conflict. Completion versus
cancellation and recovery versus a late worker report linearize on the guarded
row update. Send-message replay continues to return the original run and
message IDs.

Failed and cancelled work never creates success-shaped assistant text.

## Flutter Reconstruction and Reconciliation

Flutter stores run ID and state version on timeline entries and maintains one
run-ID-keyed state entry.

Reconciliation rules are evaluated in this order:

1. No existing state: accept a valid nonzero version.
2. Lower version: ignore as stale.
3. Equal version and identical state, including an identical terminal state:
   semantic no-op.
4. Equal version with different state: reject as inconsistent.
5. Higher version after an existing terminal state: reject as inconsistent.
6. Higher version from a nonterminal state: accept only an allowed lifecycle
   transition.

History and live/replayed events use this same path; event arrival order,
`finished_at`, and lifecycle rank are not reconciliation inputs.

Timeline rendering is:

- completed with displayable content: assistant bubble, no redundant card;
- completed without displayable content: no blank bubble, neutral completion
  card;
- failed, cancelled, expired, abandoned, or recovery terminalization:
  preserved displayable content followed by the matching terminal card;
- queued, running, waiting approval, or recovering without displayable content:
  no blank bubble, adjacent nonterminal status card;
- missing usable legacy state with non-displayable assistant content: no blank
  bubble, neutral legacy fallback.

Live output may use multiple visual text segments around tool cards while
reopen collapses those segments into the one persisted assistant message. That
existing presentation difference remains, but terminal truth is identical.

## Testing Strategy

Strict test-driven development covers:

- migration number selection after merging main;
- exact protobuf field and enum allocation revalidation after merging main,
  including event type 23 and the occupied runtime-update value 8;
- migration canonical timestamp parsing/formatting, outcome backfill, version
  bounds, value-free duplicate-correlation failure, indexes, raw-field
  scrubbing, safe event rewriting, approval-rationale preservation, and
  injected rollback before SQL, after rebuild, after scrub, after event
  rewrite, after index creation, before the migration record, and after its
  insert but before commit;
- repository lifecycle/version transactions, monotonic update time under a
  regressing clock, timestamp/version overflow rollback, and
  one-event-per-transition;
- one-query message/run projection without pagination fan-out;
- public gRPC restart round trips for every lifecycle and terminal reason;
- completed-with-content and completed-with-whitespace-only content;
- provider EOF/disconnect never becoming completion;
- ambiguous `client_cancelled` and tool cleanup mapping to abandoned, with no
  user-cancelled claim in migration, live events, or reopen;
- typed ingestion normalization for every mapping row and unknown fallback;
- defense-in-depth ChatService and EventService sanitization for malformed
  legacy events;
- raw-wire unknown numeric lifecycle/outcome/origin mapping to semantic unknown
  behavior in Go and Dart without panic or numeric copy;
- approval decision delivery failure and restart preserving waiting/recovering,
  plus waiting-to-running only after the owned runtime resume update and its
  durable acceptance before tool/model continuation;
- exact duplicate terminal reports and conflicting content/reason/version
  reports;
- completion/cancellation/recovery races;
- send-message idempotency stability;
- Flutter adjacent ordering and blank-bubble suppression;
- run ID plus version stale/equal/newer reconciliation;
- history/live parity and overlapping page boundaries;
- 10,000 unloaded historical/live run events retaining at most the 64-entry
  initial buffer, deterministically evicting to one coalesced resync, and never
  creating detached cards;
- TUR-002 denial rationale surviving migration in approval/audit storage while
  remaining absent from `RunState` and public failure events;
- neutral legacy fallback;
- exhaustive enum-to-client-copy switches.

Concurrency tests use channels, barriers, completers, and synchronous stream
controllers. They do not use sleeps for ordering.

## Merge Compatibility and Retained Limits

Before implementation review, current `origin/main` is merged normally. If
TUR-004 lands, deletion NotFound/tombstone/terminal-event precedence is
preserved. If TUR-008 lands, pagination, archive, and status-aware
reconciliation are preserved. If TUR-003 lands, run-owned egress decisions and
redacted notices remain authoritative.

After that merge, every proposed field and enum number is checked against the
merged descriptors before generation. TUR-009 never renumbers an existing
field; `TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED` remains 23 because 22 belongs
to TUR-004 even if the merge timing differs.

Retained limitations are:

- no no-worker or queue-timeout policy (TUR-010);
- no historical tool-card reconstruction;
- no explicit user-cancel intent API; current transport cancellation is shown
  as abandonment;
- no guarantee that partial live deltas survive reopen;
- live tool-separated text segments collapse into one persisted assistant
  message after reopen;
- no broad localization migration; TUR-009 localizes only its new run-state
  copy.
