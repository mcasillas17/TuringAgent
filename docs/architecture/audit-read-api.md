# Audit Read API

TUR-013 gives the local user a way to read the audit trail the backend already
writes today for selected approval, tool, integration, routing, deletion, and
auth-failure actions — not every mutation in the system. Before this,
`audit_logs` was write-only: rows were recorded (`Repository.RecordAudit`) but
no proto, service, or client could read them back. This document describes the
public `AuditService.ListAuditEntries` RPC, its redaction contract, and the
deliberate limits of what TUR-013 ships. Any future writer — memory, retry, or
otherwise — can call the same `Repository.RecordAudit` and be retrievable
through this API the moment it does, but no such writer is implemented here.

## Goal and scope

The goal is narrow: let the person who owns this local install answer "what
did this system actually do?" without exposing anything a tool call, approval,
or credential needs kept private. The one thing it discloses *because* it is
private-by-nature is the rationale a person typed on an approval or denial —
that is the answer, not a leak (see
[Approval decision rationale](#approval-decision-rationale)). It is **not** a
general-purpose log viewer,
it does not add new audited actions, and it never reads the vault. A future
action can already be retrieved through this API the moment it is recorded —
see [Action allowlist](#action-allowlist) — but it is redacted to metadata
only until someone reviews it and writes an explicit typed rule for it. That
is how the `memory.*` actions memory now records — candidate lifecycle,
promotion, and profile application on the curated-memory paths; note
indexing/withdrawal and orphan cleanup on the vault reconcile and erasure
paths; id and status only, never content — surface here: as metadata, with
no typed payload rule yet. Retry decisions remain uninspectable — nothing beyond the
`tool.call.*` `reason` field says why a run retried — and the "why did
Turing remember or use this?" tracing question is still MEM-012's job,
tracked as its own task, not implied here.

- Proto: `proto/turing/v1/audit.proto`
- Service: `turing-backend/orchestrator-go/internal/service/audit/service.go`
- Repository: `turing-backend/orchestrator-go/internal/repository/audit.go`
- Registration: `turing-backend/orchestrator-go/internal/app/app.go`
- Flutter access: `turing-client/turing_app/lib/networking/api_client.dart`,
  `grpc_client.dart`, `lib/models/audit.dart`, `lib/models/grpc_mappers.dart`

## Authentication and registration

`AuditService` is registered **only** on the public gRPC server
(`turingv1.RegisterAuditServiceServer(publicServer, auditService)` in
`app.go`), never on the internal server the agent runtime and `mcp-files` use.
It is authenticated exactly like every other public service: the same bearer
token, `TURING_CLIENT_API_KEY`, checked by the same `auth.UnaryInterceptor`
that guards `SessionService`, `ChatService`, and the rest. There is no
separate audit credential and no relaxed or elevated access path. Access is a
capability of the bearer token, not a property tied to a particular user or
process: any process or person holding a valid `TURING_CLIENT_API_KEY` can
read the redacted rows, exactly as it can already call `SessionService` or
`ChatService`. The internal runtime token used by the agent runtime and
`mcp-files` cannot read audit rows, because `AuditService` is never
registered on the internal server that token authenticates against — there
is no code path from that token to this RPC, regardless of what the token
holds. This still depends on treating `TURING_CLIENT_API_KEY` as a secret:
whoever controls the key controls read access to the audit trail, so the
usual local deployment and secret-handling practice for that key (keeping
`.env` out of version control, not exposing it beyond the local machine)
remains required — TUR-013 does not add or relax any of it. This read-access
capability is separate from cursor integrity: the bearer token gates who may
read the redacted rows, but the cursor MAC is keyed by a server-side secret
(`TURING_APPROVAL_JWT_SECRET`, see below) that is shared with components that
verify approval capabilities, including `mcp-files`, and is never a public
client bearer secret. Only the orchestrator derives the audit cursor subkey
from it, so holding the client bearer token lets a caller page through the
trail without letting it forge or tamper with a cursor.

## Flutter access: thin, not a viewer

`TuringApi.listAuditEntries(...)` and the models in `lib/models/audit.dart`
give the client a typed way to call the RPC and decode its response. That is
the entire client-side surface TUR-013 ships: there is no audit screen, no
navigation entry, and no UI that renders these rows anywhere in
`turing_app/lib`. Building an inspection UI is out of scope for this task —
the roadmap (`docs/architecture/2026-08-18-personal-agent-audit.md`) leaves it
as a decision the Flutter side can make in a follow-up once a real read
contract exists to build against.

## Request: filters, ordering, and paging

`ListAuditEntriesRequest` (`proto/turing/v1/audit.proto`):

| Field | Semantics |
|---|---|
| `correlation_id` (optional) | Exact match against the stored `correlation_id` column. |
| `action` (optional) | Exact match against the stored `action` column. |
| `created_at_start` (optional) | **Inclusive** lower bound on `created_at`. |
| `created_at_end` (optional) | **Exclusive** upper bound on `created_at`. |
| `order` | `AUDIT_ORDER_UNSPECIFIED` or `AUDIT_ORDER_DESCENDING` both sort newest-first; `AUDIT_ORDER_ASCENDING` sorts oldest-first. Unspecified is documented to mean descending, not left undefined. |
| `page.limit` | `0` (or an absent `page`) means the default of **50**. Any other value must be in `1..100`; `100` is the hard maximum. |
| `page.cursor` | Opaque string from a previous response's `page.next_cursor`. Empty means "first page." |

A window where `created_at_start` is not strictly before `created_at_end`
(including equal) is rejected as `InvalidArgument` — such a window can never
match a row, so it is a caller mistake, not a valid empty result
(`resolveAuditFilter` in `service.go`).

## Pagination: stable keyset cursor, not row offset

Rows are ordered by `(created_at, rowid)` — `rowid` is SQLite's own
monotonically increasing identity, retained specifically so ties on
`created_at` sort deterministically (see migration `0009`'s indexes and
`0012_audit_read.sql`'s comment). This keyset shape means a cursor anchors at
an exact `(created_at, rowid)` pair and the next page is `WHERE (created_at,
rowid) < (anchor)` (or `>` ascending) — stable under concurrent inserts,
unlike an offset that shifts when new rows land.

The repository fetches `Limit + 1` rows in one query
(`ListAuditRecords`); the extra row is never returned to the caller, it only
tells the service whether to set `page.next_cursor`. `maxAuditRecordsLimit =
100` caps `Limit` at 100, so the repository reads at most `Limit + 1` rows —
that is, at most 101 — while the response the caller sees returns at most
100.

### Cursor contents and authentication

The cursor the client receives is not a documented data contract — it is an
opaque token. Its current shape (subject to change without notice) is
raw-URL-base64 (`base64.RawURLEncoding`, no padding) over a JSON object
carrying:

- `v`: cursor format version (currently `1`; a mismatch is rejected, never
  reinterpreted);
- `createdAt`: the anchor row's canonical `created_at` string;
- `rowID`: the anchor row's SQLite `rowid`;
- `fingerprint`: a SHA-256 hex digest over the exact filter set and resolved
  order the cursor was minted under (correlation/action presence and value,
  start/end, and order — with `AUDIT_ORDER_UNSPECIFIED` folded into
  descending before hashing, since they produce the same query);
- `mac`: an HMAC-SHA256 hex digest, computed over `v`, `createdAt`, `rowID`,
  and `fingerprint`, under a server-held key.

None of this is secret — the anchor and fingerprint are plain values, not
encrypted — and the JSON field names above are implementation detail, not a
supported wire contract a caller should parse or construct directly. What
makes the cursor trustworthy is the MAC: `decodeAuditCursor` recomputes it
over the decoded body and rejects the cursor (`errInvalidAuditCursor`, a
single value-free `InvalidArgument`) unless it matches under a constant-time
comparison, and separately requires the embedded fingerprint to
constant-time-match the *current* request's filters. A client cannot swap in
a different anchor, resume a page under different filters than it was issued
for, or otherwise forge a cursor without the server's key.

The MAC key is derived, never random-per-request: `audit.New` takes an
optional secret and — when one is configured — computes
`SHA-256("turing.audit.cursor.v1" + secret)`, domain-separating it from any
other use of the same secret elsewhere in the process. This derives a separate
cursor key, not a reuse of the approval token key: raw approval-token key bytes
are not reused directly; the orchestrator derives a domain-separated cursor key
via `SHA-256(domain || approval secret)`. Anyone who has that server-side
approval secret can derive the same cursor key; a holder of the public
`TURING_CLIENT_API_KEY` bearer token cannot, because that bearer token does not
include the server-side approval secret. `app.go` passes `cfg.ApprovalJWTSecret`
(`TURING_APPROVAL_JWT_SECRET`), the server-side approval secret shared with
components that verify approval capabilities, including `mcp-files`, and never
a public client bearer secret; only the orchestrator derives this audit cursor
subkey from it. Reading rows and forging a cursor are separate capabilities.
Rotating the client bearer key alone leaves every outstanding cursor valid;
changing to a different approval secret invalidates them all by design, while
identical approval secrets yield the same derived cursor key, so cursors
survive restarts or installs that keep the secret unchanged. If no secret is
configured, each `audit.New` call without a supplied secret draws a fresh
random key instead — not one per process — so cursors are still unforgeable but
do not survive a reconstruction. That random fallback exists only for direct
constructor use in isolated unit tests and `Record`-only callers, including
some production audit instances; the publicly registered app audit service
always receives the required approval secret, so its public cursor validation
persists across restart.

The encoded cursor a client may submit is capped at 2048 bytes
(`maxAuditCursorBytes`) before any base64 decoding happens, so a client cannot
force an unbounded decode from an oversized string.

## Error semantics

- **`InvalidArgument`** for anything the *client* controls and got wrong: a
  nil request, an out-of-range `page.limit`, an empty/blank/over-long/control-
  character/invalid-UTF-8 `correlation_id` or `action` filter, an invalid
  `created_at_start`/`created_at_end` timestamp, an inverted or empty
  start/end window, an unrecognized `order`, or any structurally invalid,
  tampered, unsupported-version, or filter-mismatched cursor. Every one of these
  is validated before the single repository query runs.
- **`Internal`**, generic and value-free, for anything on the *server* side:
  a repository/database failure, or a stored `created_at` that fails to parse
  under the canonical layout. The response message is always the fixed string
  `"list audit entries failed"` — it never echoes a driver error, a stored
  value, or which row or column caused the failure.

## Query and cost bounds

Every read behind `ListAuditEntries` is exactly **one** indexed SQL query
(`Repository.ListAuditRecords`), filtered by exact-match `correlation_id`/
`action` and the `created_at` window, ordered by `(created_at, rowid)`, and
bounded to `Limit + 1` rows (maximum 101, since `Limit` itself is capped at
100).

Bounded-prefix projection keeps a single oversized stored value from
inflating scan cost or response size:

- **Structural metadata** (`id`, `correlation_id`, `actor_type`, `actor_id`,
  `action`, `target`): the repository projects at most the first **512**
  bytes of each column (`maxAuditMetadataReadBytes`), using
  `substr(CAST(col AS BLOB), 1, 513)` rather than `length()`ing the whole
  column, so the *scan* cost is bounded too, not just the returned size. A
  required column (`id`, `actor_type`, `action`) collapses to an empty string
  when the stored value is over that bound; an optional column
  (`correlation_id`, `actor_id`, `target`) collapses to SQL NULL. The service
  then applies its own, tighter, per-field bounds on top before anything
  reaches the wire:

  | Field | Service bound (bytes) |
  |---|---|
  | `audit_id` | 256 |
  | `correlation_id` | 256 |
  | `actor_type` | 32 |
  | `actor_id` | 256 |
  | `action` | 128 |
  | `target` | 512 |

- **Payload** (`payload_json`): the query inspects at most the first
  **16 KiB + 1** bytes (`maxAuditPayloadReadBytes = 16 * 1024`) of the column
  via the same bounded `substr`, and only ever returns the value when it is
  **≤ 16 KiB**. A payload over that bound is treated the same as any other
  unusable payload: `PRESENT` with no fields, never a partial or truncated
  body.

- **Legacy timestamp normalization**: migration `0012_audit_read.sql`
  rewrites every stored `created_at` into `repository.FormatTimestamp`'s
  fixed-width canonical UTC form (`YYYY-MM-DDTHH:MM:SS.SSSSSSSSSZ`, always
  nine fractional digits) before the `idx_audit_created_at` index is built.
  This exists because an older code path used to leave the fractional part
  off when it was exactly zero, which — compared as plain text — sorted a
  whole-second row *after* the immediately following nanosecond, silently
  reversing order. Every comparison and the ordering index now see one
  canonical form only.

## Structural metadata redaction

Every entry always carries three **required** fields: `audit_id`,
`actor_type`, `action`. `mapAuditEntry` (`service.go`) projects each through
`requiredAuditMetadata`, which returns the stored value only when it is
non-empty, within its byte bound, valid UTF-8, and free of NUL/Unicode control
characters. Otherwise it substitutes the fixed literal **`"[redacted]"`** —
never the unsafe stored bytes, and never a bare empty string standing in for
an overflowed or missing required value.

The three **optional** fields — `correlation_id`, `actor_id`, `target` — go
through `optionalAuditMetadata` instead: when the stored value is
structurally unsafe by the same rules, the field is **omitted** from the
response entirely (proto `optional` absent), rather than redacted to a
placeholder. A genuinely `NULL` optional column stays absent; a non-`NULL`
*empty string* is preserved as a present, empty value — the two are not
conflated, which is what lets a caller distinguish "never set" from "set to
nothing."

### Action-specific structural omissions

Structural safety answers "are these bytes safe to put on the wire", which is
a different question from "is this value safe to disclose at all". Two stored
values are perfectly clean by every structural rule above and still must never
be returned, so `mapAuditEntry` drops them by action (`auditDisclosureFor` in
`service.go`), before any structural check even applies:

| Action(s) | Omitted field | Why |
|---|---|---|
| `approval.*` — every current and future approval action | `target` | Approval rows are written with the approval id as `target` (`service/approvals/service.go`), and that same approval id is the `jti` claim of the short-lived approval JWT that authorizes a mutating file tool. Returning `target` would return a JTI, which this API never does. The rule is a **prefix**, not a five-way list, so a future `approval.*` writer inherits the omission instead of leaking a JTI until someone notices. |
| `auth.failed` | `actor_id` | `app.go`'s `persistAuthFailure` stores the caller's peer address as `actor_id`. A recorded peer address is never returned (see [Never returned](#never-returned)). |

Everything else on those rows still maps. An `approval.*` entry keeps its
correlation id (the run), actor type, actor id, action, timestamp, and its
allowlisted payload fields; an `auth.failed` entry keeps its `target` (the
gRPC method) and its allowlisted `method` / `request_id` payload fields. The
omission is scoped to the one field that carries the sensitive value, not to
the row.

The `approval.*` omission cannot be reopened by metadata bounding. The
repository derives a dedicated **approval-prefix disclosure bit**
(`AuditRecord.ActionHasApprovalPrefix`) inside the same SQL query, from only
the first **9** bytes (the length of `"approval."`) of the *original* action —
`COALESCE(substr(CAST(action AS BLOB), 1, 9) = CAST('approval.' AS BLOB), 0)`,
NULL-safe and using fixed SQL literals, never client input. It survives the
bounded `action` projection collapsing an over-512-byte action to `""`: without
it, an `approval.*` action padded past the read bound would read back empty,
`strings.HasPrefix` would miss the family, and the row's `target` (the JTI)
would leak. `auditDisclosureFor` omits `target` when that bit is set **or**,
as defense for records built without the repository (tests, direct callers),
when the raw mapped action itself begins with `approval.`. The
key action-specific *payload* mapping stays keyed on the bounded action, so an
oversized or malformed action still projects no rationale fields (default
deny). The bit is internal disclosure classification, not part of the public
read contract. Neither rule changes what is *stored*: the audit log keeps the
full record on disk, and this task deliberately does not rewrite history. These
rules bound only what this read API discloses.

## Payload states

`AuditPayloadState` has three members, and the mapping is exhaustive and
non-overlapping:

- **`ABSENT`** — the stored `payload_json` column is SQL `NULL`. This row
  never had a payload recorded at all.
- **`SCRUBBED`** — the stored payload matches the exact deletion tombstone
  (`{"scrubbed":true}`) byte-for-byte. This is a deliberate withdrawal, not an
  ordinary absence: a payload *did* exist and was intentionally replaced.
- **`PRESENT`** — every other case: a normal, well-formed payload for a known
  action; a payload for an unknown or future action; a payload that is
  malformed JSON, not a JSON object, or larger than the 16 KiB read bound; and
  a payload whose typed fields all failed their structural checks. `PRESENT`
  only asserts that a payload existed (or exists) and was not the scrub
  tombstone — it **never** promises that any field of it will actually be
  visible in the response. A `PRESENT` entry with no populated payload fields
  is an expected, correct outcome, not a bug.

## Action allowlist

`applyAuditActionPolicy` (`service.go`) is a closed, per-action switch. Only
the fields listed below are ever copied out of a stored payload; everything
else in the JSON object is silently ignored, whatever the action:

| Action(s) | Disclosed fields |
|---|---|
| `tool.call.before`, `tool.call.after` | tool name, server name, phase, status, reason, duration (ms), error code (from a nested `error.code` only — `error.message` is never read) |
| `approval.requested`, `approval.expired`, `approval.consumed` | tool name, unattended (bool), automation id, automation name |
| `approval.approved` | the above, plus the **decision comment** the person typed and, when the stored record says so, a **decision comment truncated** flag |
| `approval.denied` | the above common fields, plus the **denial reason** the person gave and, when the stored record says so, a **denial reason truncated** flag |
| `automation.tool.blocked` | tool name, server name, automation id, automation name |
| `integration.connected`, `integration.revoked`, `integration.deleted` | provider, display name |
| `auth.failed` | method, request id |
| `session.deleted` | deleted run count, deleted message count |
| `egress.consent.recorded` | provider, endpoint host, typed data categories, decision version, consent timestamp |
| `automation.remote_egress_blocked` | error code, provider |
| `mcp.server.registered` | server name, MCP server tier, MCP server URL, adopted (bool) |
| `mcp.server.enabled`, `mcp.server.disabled` | server name, MCP server tier, remote discovery attempted (bool), discovery succeeded (bool) |
| `mcp.server.token_rotated`, `mcp.server.token_cleared` | server name, token configured (bool) — never the token or its sealed form |
| `mcp.server.reimported` | imported/skipped/refused server counts, status (`completed` or `partial`) — never the server names or refusal reasons |
| `mcp.server.deleted` | server name, MCP server tier |
| `mcp.server.tool_policy_changed` | server name, tool name, tool policy (the canonical `safe` / `approval_required` / `disabled` string) |
| `session.routed`, `session.unrouted`, and any unknown or future action | metadata only — no payload fields at all |

MCP server tier, MCP server URL, and tool policy are their own typed fields
(`mcp_server_tier`, `mcp_server_url`, `tool_policy`) rather than reuses of
`provider`/`display_name` — an MCP server registration is not an
`integration.*` row and is disclosed under its own name. `tool_name` and
`server_name` are reused as-is: the MCP registry actions above are simply
another writer of those two existing fields, the same way `tool.call.*` and
`automation.tool.blocked` already are.

### Approval decision rationale

TUR-002 records what a person typed when they approved or denied a tool call.
This is the read path that makes it user-readable, and it is the one disclosed
payload field whose content a **human wrote in their own words** rather than a
call site emitting a machine label. That difference drives every rule below.

The two rationale fields are separate from each other and from everything
else:

- `approval.approved` discloses `decision_comment` (stored key `comment`) and
  `decision_comment_truncated` (stored key `commentTruncated`);
- `approval.denied` discloses `denial_reason` (stored key `reason`) and
  `denial_reason_truncated` (stored key `reasonTruncated`);
- no other action discloses either, including the other three `approval.*`
  actions. A stray `comment` on a denial, a stray `reason` on an approval, or
  either key on `approval.requested` / `approval.expired` /
  `approval.consumed` is ignored like any other unlisted key.

A denial reason is deliberately **not** projected into the `reason` field that
`tool.call.*` uses. That field is the tool-policy reason a call needed
approval ("needs-approval"); this one is a sentence a person wrote. Giving them
one name would have made a human's words indistinguishable from a policy label
in every consumer.

**Present-empty versus absent is the answer, not a formatting detail.** The
approvals request proto has no field presence on these scalars, so an omitted
comment and an explicitly empty one both arrive as an empty string and are
both stored as an explicit empty rationale (this limitation is documented in
[the roadmap's TUR-002 entry](2026-08-18-personal-agent-audit.md)). What is
preserved end to end is the difference between that and no human field at all:

| Response | Meaning |
|---|---|
| present, non-empty | the person decided and typed this |
| present, **empty string** | a human decided and supplied no words (omitted or explicitly empty — the write path cannot tell these apart) |
| **absent** | no human rationale was recorded for this row: an unattended automation grant, an expiry, a consumption, or a stored value this API rejected |

So `auditHumanRationale` in `service.go` is a separate reader from
`auditString`, not a reuse of it: `auditString` omits an empty value, which is
correct for a machine label and wrong here.

**Bounds and truncation belong to the writer.** The approvals service accepts
up to 4096 bytes of rationale on the request, and copies at most **512 bytes**
of it into the audit payload, setting the matching `*Truncated` boolean when
it had to shorten. This read API re-checks the same 512-byte bound against the
untrusted stored bytes and **never truncates, escapes, or repairs** anything: a
stored value over the bound is omitted entirely, because a value that long did
not come from that writer and this path has no business guessing what it meant.
The `*_truncated` flags are projected only when the stored payload carried that
exact JSON boolean — `true` or `false` — so the flag reports the writer's
truncation and never this reader's opinion.

**Allowed characters are the ones a person types.** The rationale must be a
JSON string and valid UTF-8. Unlike every other projected string, **newline
(U+000A), carriage return (U+000D), and tab (U+0009) are preserved verbatim** —
they are how a typed sentence is formatted. Every other Unicode control
character makes the whole value unsafe and omits the field: NUL, the rest of
C0 (BEL, ESC, …), DEL, and the C1 range including U+0085 NEL. Those carry no
authored meaning and are the ones that can rewrite a terminal or a log line.
An omitted rationale does not change the row: it still returns `PRESENT` with
its other allowlisted fields.

Nothing here relaxes the surrounding contract. An `approval.*` row still never
returns its `target` — that value is the approval id, which is also the
approval JWT's `jti` — and the rationale travels in its own typed field, never
as raw payload JSON.

`session.routed` currently stores an agent id, display name, endpoint host, and
model in its payload (`repository/external_agents.go`), while
`session.unrouted` stores an empty `{}` payload. `auth.failed` is recorded with
a method, request id, user agent, and peer address (`app.go`'s
`persistAuthFailure`) — but none of the routing fields are ever disclosed
through this API, and `session.routed` and `session.unrouted` remain
metadata-only in the public response under default deny. (Deleting the session
also scrubs those two rows on disk — see
[Deletion semantics](#deletion-semantics).) Only `method` and
`request_id` are for `auth.failed`. This is the concrete meaning of
"default-deny by action": being retrievable and being disclosed are different
guarantees, and only the second is action-specific.

This also means a hypothetical future memory action (write, retraction,
recall-used, and so on) would land here the moment it started calling
`Record`, and would already be *retrievable* — but strictly metadata-only,
with the same `PRESENT` state and no fields, until someone adds a reviewed
row to this table. TUR-013 does not add such a row and does not implement any
memory-specific action; that remains MEM-012's scope.

Every value copied out is additionally type- and shape-guarded, never
truncated or coerced:

- String fields must be an actual JSON string, non-empty, within their
  service-side byte bound (512, 256, 128, or — for `mcp_server_url` only,
  matching the registry's own stored-URL bound — 2048 bytes, depending on
  field; see `service.go`'s bound constants), and free of NUL/control
  characters. Any violation omits the field; it is never shortened to fit.
- The two approval rationale strings are the documented exception to the
  "non-empty" and "no control characters" halves of that rule — see
  [Approval decision rationale](#approval-decision-rationale). They are still
  bounded, still type-guarded, and still never truncated here.
- The boolean fields (`unattended`, the two rationale `*_truncated` flags,
  `adopted`, `token_configured`, `remote_discovery_attempted`, and
  `discovery_succeeded`) must be an exact JSON `bool`.
- Integer fields (`duration_ms`, `deleted_runs`, `deleted_messages`,
  `imported_servers`, `skipped_servers`, `refused_servers`) must be
  an exact, non-negative integer that parses cleanly as `int64` — decoded via
  `encoding/json`'s `UseNumber()` so `1.5`, `1e3`, and `" 1"` are all rejected
  rather than coerced into a number.
- `egress_decision_version` must be a positive `int32`, data categories must
  be unique known `EgressDataCategory` enum names, and the consent timestamp
  must parse as RFC3339Nano. A malformed field is omitted rather than repaired.

## Never returned

Regardless of action, state, or filters, the response can never carry: raw
payload JSON, tool call arguments or results, human-readable error messages
(only a bounded `error_code`, and only for `tool.call.*`), approval tokens or
JTIs, bearer tokens or any `authorization` header value (including an MCP
server's bearer token or its sealed/ciphertext form — `mcp.server.registered`
discloses the server's URL but never its token, and `.token_rotated` /
`.token_cleared` disclose only whether a token is now configured), API keys or
other credentials/passwords/secrets, the recorded user agent or peer address
(even though `auth.failed` payloads carry them today), or the routing
endpoint, model, or agent-display-name fields recorded on `session.routed`.
Unknown JSON fields on any payload — including fields not listed in the
action's row above — are always ignored, never passed through.

### What that guarantee is, and what it is not

The list above is a promise about **fields this API knows to be
secret-bearing**: a token, a JTI, a credential, a header value, a raw tool
argument or result. Each of those is identified by *where it is stored*, so
the projection can withhold it unconditionally, and does.

It is **not** a promise that no response can ever contain a secret, and it
would be dishonest to state it that way. The approval decision comment and
denial reason are free text a person typed, disclosed on purpose because a
rationale nobody can read is not a rationale. This service cannot inspect that
text and decide whether the person pasted an API key into it — no allowlist
can, because the field's whole purpose is to carry words the system did not
choose. Two things follow, and both are load-bearing:

- **Anyone holding the `TURING_CLIENT_API_KEY` bearer token can read the
  rationale**, exactly as they can read every other disclosed field. Read
  access to the audit trail is a capability of that token (see
  [Authentication](#authentication-and-registration)), not a per-field
  permission.
- **Do not type credentials, tokens, or other secrets into an approval comment
  or denial reason.** It is stored, it is disclosed by design, and a session
  deletion is the only thing that withdraws it (see
  [Deletion semantics](#deletion-semantics)).

The rationale is still bounded, type-guarded, control-character-filtered, and
confined to its own typed field — it just is not, and cannot be, content-
inspected.

Two of those guarantees are **not** payload rules, because the value in
question is stored in a structural metadata column rather than in
`payload_json`. They are enforced by the action-scoped omissions documented
under [Structural metadata redaction](#action-specific-structural-omissions),
and they are what make the "no JTIs" and "no peer address" promises above true
rather than aspirational:

- an `approval.*` row's `target` **is** the approval JWT `jti`, so `target` is
  omitted for every `approval.*` action;
- an `auth.failed` row's `actor_id` **is** the peer address, so `actor_id` is
  omitted for `auth.failed`.

The exact payload allowlist is a separate mechanism from these two structural
omissions: the allowlist decides which *payload* fields a known action may
disclose, the omissions decide which *metadata* fields an action must
withhold. A field has to clear both to reach the wire.

All of this is enforced by the allowlist being additive and closed: a field
reaches the wire only if a reviewed rule names it, never by default.

## Deletion semantics

The session-deletion pipeline (`BeginSessionDeletion` /
`AdvanceSessionDeletion` in `repository/session_delete.go`, both sharing
`scrubSessionAuditPayloadsSQL`) scrubs audit content **before** the cascading
delete, in the same transaction. One statement covers two disjoint sets of
rows:

- every `audit_logs` row whose `correlation_id` matches one of the session's
  runs — the ordinary case, since `correlation_id` is the run id at almost
  every writer; and
- every row whose `target` is the session id and whose `correlation_id` is
  `NULL` or empty. This covers `session.routed` / `session.unrouted` today,
  because those writers identify the session through `target`, and it also
  makes future uncorrelated session-level actions scrubbed by default. Without
  this target path, routing payloads such as the third-party endpoint host,
  model, and agent display name would outlive the conversation they describe.

Either way the payload is overwritten with the exact tombstone
`{"scrubbed":true}`. The row itself — `audit_id`, `correlation_id`,
`actor_type`, `action`, `target`, `created_at` — is untouched; only the
payload is replaced. Through this API that row now reports
`AuditPayloadState.SCRUBBED`, and no content from the original payload is
recoverable through it.

The deletion is itself audited, and that record is **not** scrubbed:
`session.deleted` is recorded with the session id as `target` and the exact
run/message counts that were removed, and it stays `PRESENT` indefinitely. It
is inserted *after* the scrub statement, in the same transaction, which is
what keeps it out of the scrub's own target match even though it also carries
the session id as `target`. The invariant this whole flow rests on is: audit
evidence that something happened must survive a deletion; only the withdrawn
content itself may be
removed. A correlated row's shape surviving deletion, with its content
replaced rather than the row disappearing, is what lets an operator still
answer "was this session deleted, and when" without being able to reconstruct
what was deleted.

## Trust flow, in short

1. A local client authenticates with the same bearer token every other public
   RPC requires — there is no separate or weaker gate for audit reads.
2. Every filter, limit, and cursor is validated against a fixed set of rules
   before the one repository query runs; anything the caller could have
   gotten wrong fails closed as `InvalidArgument`, never as a partial or
   best-effort result.
3. The repository returns bounded, prefix-limited raw column data; nothing
   past 512 (metadata) or 16 KiB (payload) bytes of any single value is even
   read.
4. The service re-validates every structural field against its own tighter
   bounds, drops the two metadata fields no action may disclose (an
   `approval.*` `target`, an `auth.failed` `actor_id`), and projects payload
   content only through a closed, reviewed, per-action allowlist.
5. Anything not explicitly allowed — an unrecognized field, an unreviewed
   action, an oversized or malformed value — is dropped rather than passed
   through.

That last step is the rationale for default deny: the set of things a stored
audit payload *could* contain is unbounded and untrusted (it is written by
many call sites across the codebase, some already existing before this API,
some not yet written), while the set of things this API is allowed to
disclose must stay small, reviewed, and enumerable. An allowlist that fails
open — showing a field until someone notices it should not have been shown —
would leak the first time an unreviewed call site added a payload key. Failing
closed means the worst case for an unreviewed field is that it is invisible,
never that it is exposed.
