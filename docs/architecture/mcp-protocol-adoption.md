# MCP protocol evolution and adoption plan

**Status:** Research and proposed implementation contracts, not shipped support.
**Checked:** 2026-10-15 UTC (2026-10-14 in America/Los_Angeles).
**Repository baseline:** `09d3d5dc07521c9f7fb17c58162d4fbb3c84b8bb`.

The [canonical roadmap](../NORTH_STAR.md#mcp-protocol-evolution) owns task
status. This document records the specification research, implementation
comparison, design choices, and acceptance boundaries behind MCPP-001 through
MCPP-009. It does not change protocol versions, defaults, approvals, or runtime
behavior. Repository paths and line references describe the inspected commit.

## Findings in brief

The current published MCP revision is **2026-07-28**, not Turing's implemented
**2025-11-25**. The official versioning page identifies it as current, and the
release is pinned by the `2026-07-28` tag at
`5f5440bb26a62e2cf3440b92da5a667efa03b267`. [S1] [S2]

The important improvement is not a larger timeout. Modern MCP separates a
short request from durable work:

- The core is stateless: request metadata replaces initialization and session
  headers, and `server/discover` describes the server.
- Multi Round-Trip Requests (MRTR) replace server-initiated requests with an
  explicit `input_required` response and a subsequent client request.
- The official, optional `io.modelcontextprotocol/tasks` extension lets a
  server return a durable task handle. `tasks/get` returns status and, once
  terminal, the result itself. `tasks/update` supplies requested input;
  `tasks/cancel` requests cancellation without promising it won the race.
- SSE resumability is removed in the modern core. Durable operations use
  task handles instead of transport reconnection. [S2] [S3] [S4]

Turing has bounded legacy interoperability, not modern MCP or Tasks support.
Its current tool calls generally have a 30-second default execution deadline,
and waiting occupies a runtime slot. Its stock Go SDK fixtures pass through
the SDK's legacy mode or fallback; that does not prove modern conformance.

The recommended sequence is: make legacy limitations explicit, add modern
third-party interoperability while retaining legacy peers, consume Tasks
within bounded existing waits, then introduce orchestrator-owned deferred
work that releases worker slots. The last step is a lifecycle and authorization
change, not just an MCP-client change.

## 1. What is current, stable, optional, or deprecated

| Item | Verified position |
| --- | --- |
| MCP protocol | `2026-07-28` is current; `2025-11-25` is the preceding handshake-era revision. |
| Modern core | Stateless requests, per-request capabilities, `server/discover`, typed results, MRTR, HTTP routing headers, and cache hints. |
| Modern Tasks | Optional official extension, `io.modelcontextprotocol/tasks`, with stable `2026-07-28` specification/schema. |
| Old Tasks | Experimental utility in the 2025-11-25 core. Not wire-compatible with the modern extension; do not implement it as the migration target. |
| Extension snapshot | Stable schema locked at `0d0a6bd4c258b35caa3c810a1dd506cf105b1501`; specification text at `899c1bbc869567670093face3591b39c28ce3219` includes a later editorial correction. Its npm package versions are not protocol revisions. |
| Draft | The inspected draft changelog had no new release changes. Recheck at implementation time; do not silently adopt draft behavior. |
| Go SDK | Turing pins `v1.8.0` as a test peer. That SDK supports both protocol eras, but does not supply a Tasks implementation. |
| Deprecated features | Roots, Sampling, Logging, the old HTTP+SSE transport, and Dynamic Client Registration are listed as deprecated. Deprecation is not immediate removal. |

Sources: released core and versioning [S1], [S2], [S3]; Tasks [S4], [S5];
feature registry [S6]; SDK [S7], [S8]. The legacy initialization era is not listed as
deprecated. Continuing to support legacy peers is therefore deliberate
compatibility, not a claim that Turing implements the latest revision.

There is a source inconsistency worth preserving: SEP-2663's compatibility
table mentions `2026-06-30`, while the released extension specification and
schema are under `2026-07-28`. Use the released schema, not that apparent
pre-release date. Interpret changelog summaries alongside normative transport
and extension requirements; in particular, a new request ID is not an
idempotency guarantee for repeating a mutation.

## 2. How calling and waiting changed

### Protocol comparison

| Concern | Legacy 2025-11-25 | Modern 2026-07-28 |
| --- | --- | --- |
| Initialization | `initialize`, then `notifications/initialized` | No handshake; version and client capabilities on every request |
| Discovery | Capabilities from initialization | `server/discover` is mandatory for servers; optional for clients |
| Session addressing | Optional `Mcp-Session-Id` | No protocol session header |
| HTTP routing | Protocol version header | Also `Mcp-Method`, and `Mcp-Name` where defined; header/body consistency rules |
| Result kind | Ordinary result; experimental task variants | `resultType`: `complete`, `input_required`, or extension-defined `task` |
| Additional input | Server-initiated requests on an open connection | MRTR: result carries `inputRequests` and/or opaque `requestState` |
| Transport recovery | Optional SSE event IDs and reconnect behavior | No `Last-Event-ID` resume or message redelivery |
| In-flight HTTP cancellation | `notifications/cancelled`; disconnect alone is not cancellation | Closing the request's response stream cancels that request |
| Long-running Tasks | Experimental request-level `task`, `tasks/result`, `tasks/list` | Optional extension; `tasks/get`, `tasks/update`, `tasks/cancel` |

These are different wire contracts, not interchangeable feature flags. [S2] [S3] [S4]
The previous HTTP+SSE transport is also distinct from an SSE response carried
over Streamable HTTP; modern Streamable HTTP still supports SSE responses.

### Modern requests

Every request carries these metadata keys:

- `io.modelcontextprotocol/protocolVersion`;
- `io.modelcontextprotocol/clientCapabilities`;
- `io.modelcontextprotocol/clientInfo` is recommended.

On Streamable HTTP the protocol-version header must agree with metadata.
`Mcp-Method` and, for a tool call, `Mcp-Name` must agree with the JSON-RPC
operation. The specification defines encoding for non-ASCII names and
`x-mcp-header` parameter mirroring; clients must validate the declarations
rather than letting a tool overwrite arbitrary headers. [S3]

A client can probe with `server/discover`, prefer modern behavior when
supported, and fall back to initialization for a legacy peer. A recognized
modern error body is not a reason to pretend the server is legacy. Era
selection must happen on a discovery request, never by retrying a possibly
executed mutation in another era.

Modern errors distinguish:

| Code | Meaning | Turing implication |
| --- | --- | --- |
| `-32020` | Header mismatch | Correct the request; do not retry unchanged. |
| `-32021` | Missing required client capability | Explain the unsupported capability without enabling it automatically. |
| `-32022` | Unsupported protocol version | Use the supported-version information during safe negotiation. |

The modern specification reserves `-32020` through `-32099`. Do not classify
that entire range as transient server failure. Error decoding must account for
the negotiated era and known error shape rather than interpreting every
legacy server's custom error as a modern negotiation response.

### Durable Tasks

The client explicitly declares the extension in its per-request capabilities:

```json
{
  "extensions": {
    "io.modelcontextprotocol/tasks": {}
  }
}
```

Once that capability is declared, **the server**, not a per-call client flag,
decides whether a supported `tools/call` returns an ordinary result or a task.
A server must not return a task to a request that did not declare support.
Turing must advertise only capabilities it can actually handle. [S4]

A task-creation result is flat, not a normal tool result wrapped around a
task:

```json
{
  "resultType": "task",
  "taskId": "opaque-server-generated-handle",
  "status": "working",
  "createdAt": "2026-10-15T00:00:00Z",
  "lastUpdatedAt": "2026-10-15T00:00:00Z",
  "ttlMs": 60000,
  "pollIntervalMs": 5000
}
```

The server must durably create the task before returning this handle.
`tasks/get` is the short, repeatable status read; its result has
`resultType: "complete"` and task state:

- `working`: keep waiting within Turing's independent deadline.
- `input_required`: outstanding `inputRequests` require explicit handling.
- `completed`: the original tool result is inline in `result`. A tool result
  with `isError: true` is still a completed task.
- `failed`: the task carries a JSON-RPC `error`, not an ordinary tool failure.
- `cancelled`: cancellation won; no successful result is implied.

`ttlMs` may be null and can change. `pollIntervalMs` is advisory and can also
change; respect the server's pacing without accepting an unbounded wait.
Unknown or expired task handles produce `-32602`. Each task method carries
`Mcp-Name: <taskId>` and the required capabilities.

`tasks/update` supplies responses keyed by the outstanding input identifiers.
Its acknowledgement does not mean processing is complete. Deduplicate input
keys and preserve the trust boundary of the underlying elicitation.
`tasks/cancel` is similarly an acknowledgement, not terminal proof: the task
may still complete. Ordinary `notifications/cancelled` is not task
cancellation. [S4]

JSON-RPC request IDs identify individual exchanges. Task IDs identify durable
operations and may be bearer secrets. Store task IDs sealed; do not put them
in logs, telemetry, chat, or public event payloads.

### MRTR and progress are different mechanisms

MRTR returns `resultType: "input_required"` from `tools/call`, `resources/read`,
or `prompts/get`. The client supplies `inputResponses`, echoes the opaque
`requestState`, and issues a new JSON-RPC request ID. This is an explicitly
negotiated continuation, not permission to retry a lost mutation blindly.
Turing's first input-support slice permits user-answered form elicitation for
non-mutating tools only. It does not let a model answer a consent prompt.

Progress notifications describe a request that is still open. They do not
make it durable. A client may use progress for an idle timeout, but must keep
a hard total deadline. Tasks use task status/status messages instead; Turing
will initially poll rather than depend on notification delivery.
`notifications/tasks` through `subscriptions/listen` is already an optional
part of the stable Tasks extension, not draft-only functionality.

## 3. What Turing implements today

The comparison covers both production client paths, both bundled servers,
and the shared wire layer. Updating only one client would leave inconsistent
behavior.

| Surface | Existing support | Missing modern behavior |
| --- | --- | --- |
| Runtime MCP client | Legacy handshake, session adoption, bounded paginated discovery, JSON/SSE replies, ping, legacy cancellation; no automatic replay of `tools/call` on session loss | Stateless requests, modern metadata/headers/errors, Tasks, MRTR, progress consumption |
| Orchestrator registry client | Legacy handshake per operation, session cleanup, third-party policy/approval/egress enforcement, redirects refused, result credential redaction | Modern negotiation, durable task handles, task polling/input/cancel, deferred-result lifecycle |
| `mcp-files` | Sessionless legacy server; bounded file tools, approval/provenance metadata, identity-scoped in-flight cancellation | Modern `server/discover` and metadata/results; no Tasks |
| `mcp-system` | Sessionless legacy server; synchronous short tools; accepts cancellation notifications without stopping in-flight work | Modern core and Tasks; no in-flight cancellation mechanism |
| Shared `mcpwire` | Exact 2025-11-25 negotiation, bounded response parsing and paging, connection-generation/session fencing | Protocol-era abstraction, typed modern result discrimination, progress dispatch |

Repository evidence:

- `turing-backend/mcpwire/mcpwire.go:24-34,136-191,204-282,295-366`:
  revision, empty initialization capabilities, single-revision negotiation,
  response reader and paging limits.
- `turing-backend/mcpwire/connection.go:148-217,283-296`: handshake
  generations, cancellation detection and session loss.
- `turing-backend/agent-runtime-go/internal/mcp/client.go:146-195,213-226,407`:
  approval/provenance metadata, structured object results, restricted replay
  and error classification.
- `turing-backend/orchestrator-go/internal/service/mcpregistry/client.go:68-87,166-188,389-433,476-482`:
  per-operation connection, transport, cleanup and HTTP-error handling.
- `turing-backend/orchestrator-go/internal/service/mcpregistry/call.go:51-195`:
  selected tools, egress, consumed approval, dispatch fence and server status.
- `turing-backend/orchestrator-go/internal/service/mcpregistry/service.go:1252-1298`:
  discovery projection retains name/input schema but drops execution metadata
  and output schema.
- `turing-backend/mcp-files/cmd/server/{main.go,lifecycle.go,inflight.go}` and
  `turing-backend/mcp-system/cmd/server/{main.go,lifecycle.go}`:
  transport, metadata allowlists, tool envelopes and cancellation.

Both bundled servers refuse any `Origin`, reject nonmatching protocol-version
headers, and return 405 for GET/DELETE. Their metadata allowlists would reject
modern reserved request keys. Keep the Origin boundary and add only the exact
metadata keys justified by a supported protocol version, not a wildcard
allowlist.

Bundled execution errors currently use JSON-RPC errors rather than
`CallToolResult.isError`; this is a documented deviation, not proof of complete
tool-result conformance. Changing it requires regression coverage for how the
runtime reports a failed or uncertain mutation.

### Waiting and capacity

| Boundary | Current behavior |
| --- | --- |
| Runtime call | `TURING_TOOL_TIMEOUT_MS` defaults to 30 seconds. |
| Whole tool stage | `TURING_TOOL_TOTAL_TIMEOUT_MS` defaults to 180 seconds, including its approval/resume budgets. |
| Third-party HTTP | Registry `http.Client.Timeout` and response-header timeout are 30 seconds; the total timeout includes body/SSE reading. |
| Worker capacity | General runtime concurrency defaults to one. |
| Approval wait | `waiting_approval` retains the lease and worker slot. |
| SSE response reader | Skips notifications and server-initiated requests; ignores event IDs/retry hints; stops at a response or fails at EOF. |
| Crash after mutation dispatch | Existing recovery preserves uncertainty around consumed approvals rather than blindly repeating the action. |

These are default bounds, not a claim that every deployment has identical
configuration. Raising them alone makes a single-slot runtime wait longer; it
does not provide durability or release capacity.

### Concrete interoperability gaps

1. **Opaque modern-only failures.** Both clients discard non-success HTTP
   response bodies, losing structured version/capability error information.
2. **Task-required legacy tools are not filtered.** Discovery drops
   `execution.taskSupport`; a tool requiring the old experimental task
   mechanism can be offered even though Turing cannot call it correctly.
3. **Legacy disconnect-polling is unsupported.** A legacy peer may close an
   SSE response expecting reconnect. Turing instead reports an ended stream;
   disconnect does not cancel that legacy server's operation. An approved
   mutation must remain uncertain, not be replayed.
4. **Retry classification is too broad for modern errors.** The runtime
   classifies the entire JSON-RPC server-error range as retryable. This
   currently affects discovery retries; it is not evidence that mutations
   are already automatically replayed.
5. **Progress and server requests are skipped.** An open SSE response does
   not mean the client handles progress or bidirectional capabilities.
6. **Modern result/schema handling needs work.** Modern `structuredContent`
   may be any JSON value, while Turing's convenience unwrapping is
   object-oriented. `outputSchema`, safe reference handling, composition
   bounds, cache hints and HTTP parameter mirroring need explicit coverage.
7. **Conformance evidence is narrower than the SDK version suggests.**
   Upgrading the SDK fixture dependency did not upgrade production MCP.
8. **Documentation needs separate terms.** Legacy experimental Tasks,
   modern core, the modern Tasks extension, and Turing's own delegated runs
   are four different things.

These gaps were identified by source inspection; they were not all reproduced
against live third-party servers during the investigation.

## 4. What the existing conformance evidence proves

The investigator ran stock-SDK conformance tests in all three Go modules,
using `sqlite_fts5` for root tests. They passed at the inspected baseline.

| Fixture | What it proves | Important limitation |
| --- | --- | --- |
| Runtime client against Go SDK v1.8.0 | Legacy handshake, discovery, call, ping, sessions, restart-aware safe discovery | The SDK server uses its default stateful/legacy mode, not modern-only stateless mode. |
| Registry client against Go SDK | Legacy discovery/call, annotation distrust, authentication failure redaction, session cleanup | It does not exercise the complete production approval/egress dispatch path against a modern peer. |
| Stock client against bundled servers | The dual-era SDK client falls back from a modern probe and successfully uses Turing's legacy surface | Successful fallback is not modern server support. |
| Handwritten lifecycle fixtures | Paging/size bounds, redirects, cancellation correlation, session fencing and no call replay | They are not an independent implementation of Tasks or MRTR. |

The Go SDK supports modern stateless operation, but its Tasks support is not
implemented. [S7] [S8] Use its modern-only mode for core compatibility tests.
For Tasks, pin the official extension schema and add independent wire fixtures;
do not claim a task interoperability oracle merely because the peer is the Go
SDK. The official conformance repository has modern and server-task scenarios;
the investigation did not find equivalent client-task coverage. [S9]

## 5. Adoption options and chosen direction

| Approach | Benefit | Cost / limitation |
| --- | --- | --- |
| Bounded inline task polling | Smallest useful task-consumption slice; preserves current run lifecycle | Still occupies the worker slot; cannot promise a resumed model turn after a crash |
| Deferred result plus continuation | Durable poller outside worker slots; natural fit for local single-slot Turing | Needs a distinct operation-authorization lifetime, exactly-once join, cancellation and UI |
| Suspend/resume the same run | Could preserve one logical in-turn transcript | Requires durable mid-turn checkpoints and redesigned lease/attempt/recovery semantics |

Adopt inline consumption as a disabled-by-default compatibility step, then
deferred results as the target. Do not introduce a new suspended-run state
without its own design. Do not replace the current clients wholesale with an
SDK just to obtain Tasks: the pinned Go SDK does not implement them.

The team rearchitecture supplies useful patterns, not a ready-made external
task subsystem:

- Generalize the exactly-once join over all deferred items from one parent
  turn, so two remote tasks plus a specialist still produce one continuation.
- Reuse bounded untrusted result framing, frozen continuation tools, and
  version-gated workers.
- Do not store remote MCP tasks as hidden specialist sessions or pretend their
  server states are Turing run/lease states.
- Reuse cancellation/deletion fencing patterns, but distinguish a cooperative
  remote cancel from stopping a Turing worker.
- Reuse the [team design's client slice 3](../superpowers/specs/2026-10-03-turing-orchestrator-agent-team-design.md#14-delivery-slices)
  task-card components where appropriate. That is tracked by
  [GitHub #163](https://github.com/mcasillas17/TuringAgent/pull/163), unmerged
  when checked, not GitHub PR #3. It is a separate delivery dependency, not
  something this plan ships.

## 6. Authorization, recovery and failure contract

The following are Turing design requirements, not permissions supplied by MCP.

### Authority and durable custody

The orchestrator remains the only owner of runs, attempts, leases, approvals,
egress policy and audit. Remote statuses are untrusted observations. Only
explicit, fenced orchestrator transitions may turn them into a tool outcome or
continuation.

The initial tool dispatch must pass the existing selected-tool, policy,
argument/preview-bound approval, active-attempt and egress checks. Consume a
mutation approval once. Polling an already-created task does not consume it
again and never authorizes another mutation.

Today's `MCPDispatchActive` requires an executing, running run and an active
session. `RunAllowsRemoteMCP` checks the recorded decision's selected tool,
data categories and destination; it is not a lifetime/revocation gate.
There is no general MCP-decision revocation API at this baseline. Existing
revocation boundaries include server/tool disablement or removal, endpoint or
credential changes, cancellation and session withdrawal. A future explicit
decision-revocation API must also fence operation custody.

Deferred work must not bypass `MCPDispatchActive` merely because the original
run completed. Before MCPP-005 implementation, define and test a separate
durable operation authorization, created at the original authorized dispatch:

- owner session and deletion generation; parent run, tool call and dispatch
  attempt; original argument/preview identity;
- server identity, exact endpoint and credential generation, never a movable
  origin or bearer inherited from another server;
- allowed follow-up methods, restricted initially to `tasks/get` and
  `tasks/cancel` for the one sealed handle;
- an explicit operation deadline and consent to follow-up status traffic
  after normal dispatch-turn completion;
- revocation, cancellation, deletion and expiry state, checked before every
  send and before every committed result.

Normal completion of a dispatch turn may release the slot while that bounded
operation remains authorized. Failure or cancellation of the parent must
abandon its pending operations and suppress a continuation. A completed
parent cannot later be changed to cancelled to implement the UI's Cancel
button: cancelling deferred work needs an operation-specific command and
receipt. These are separate state machines with linked ownership.
The investigation considered bounded observation-only polling after a parent
failure. This plan does not authorize that option: stopping further traffic
is the conservative default, with an unknown-outcome receipt. Any later
observation-only mode needs an explicit, separately reviewed consent lifetime.

A local model does not mean a local tool. Existing remote-tool disclosure must
explicitly cover the destination, arguments/results, polling and allowed
cancellation traffic for that operation. Remote-provider continuations need a
separate disclosure design: neither the original run's consent nor the team's
future child-consent feature automatically authorizes a new model run.
MCPP-005 initially permits local-model continuations only.

### Failure behavior

| Situation | Required behavior |
| --- | --- |
| Call response lost before handle persistence | Record unknown outcome for a potentially mutating call. Never redispatch automatically. |
| Handle persisted, process restarts | MCPP-005 resumes bounded status reads only under valid operation custody; never repeat `tools/call`. MCPP-004 has no post-run custody: orphaned inline calls become uncertain and their handles are purged, with no recovery polling. |
| Worker dies before slot release is acknowledged | Preserve dispatch-attempt fencing and reconcile the deferred receipt before allowing replacement execution. |
| Unknown/expired task, authentication loss, exhausted wait | Report remote outcome unknown when an effect may have occurred; do not translate missing task state into successful cancellation. |
| `completed` plus `isError: true` | A completed remote task with a failed tool result, not protocol failure or success. |
| Cancel acknowledged, work later completes | Preserve local cancellation/abandonment; show that the remote effect may have completed. Never revive a terminal run. |
| Credential rotated, endpoint changed, server/tool disabled, consent revoked | Stop new traffic, including cancellation traffic if its authorization has been revoked. Never forward the handle to a new endpoint or credential generation. |
| Session deletion | Fence dispatch/polls/results immediately and remove owned handles/results through the deletion lifecycle. Late responses cannot recreate content. No post-withdrawal network cleanup is assumed. |
| Unexpected `input_required` | Fail closed with a typed unsupported-input outcome; never silently wait forever or let the model approve it. |
| Request retry | Safe discovery/status reads may retry within bounds. Blind call replay is forbidden; negotiated MRTR is a separate, explicit continuation path. |

Keep task handles and MRTR state out of diagnostics, traces and URLs. The
server may use a task ID as a bearer credential. Public events carry a
Turing-owned operation ID. Bound and escape remote status messages; do not
insert them into model context as trusted progress instructions.

Polling must respect server pacing while enforcing Turing's deadline. The
initial implementation contract should use a 5-second default when no hint is
present, a minimum 2-second interval, backoff/`Retry-After`, and bounded global
and per-server concurrency. Never cap a large server hint downward and thereby
poll faster than requested; if it exceeds the remaining deadline, stop.
The deferred default wait is proposed as 30 minutes with a 24-hour absolute
configuration ceiling, independently capped by authorization and task TTL.
These are proposed product defaults, not MCP requirements.

### Input and visibility

Form elicitation is a later, explicit capability. Only the user answers;
the disclosure must cover those answers leaving the machine. Input keys,
opaque state, destination, time, rounds and bytes are bounded. Persist send
intent and deduplicate task input keys. MRTR uses a new JSON-RPC ID while
retaining the logical operation identity and original tool arguments.
Approval-required mutations remain excluded from the first MRTR/input slice.
URL elicitation is excluded from this plan, not assumed deprecated.
Form-only support must refuse sensitive-input flows that the specification
requires to use URL mode; it must not downgrade them to a form.

Before default-on deferred execution, ship reopenable cards with pending,
waiting-for-input, cancel-requested, completed, failed and unknown-outcome
states. Transport disconnection, task cancellation acknowledgement and
filesystem rollback must never share a success label.

## 7. Implementation sequence and verification

Each row is an independently reviewable delivery slice. Exact runtime and
migration interfaces are to be settled in that slice's reviewed design; do
not infer that the example table or service names here already exist.

| Roadmap task | Owning surfaces | Deliverable and acceptance evidence |
| --- | --- | --- |
| MCPP-001 | `mcpwire`, runtime MCP client/tool registry, registry discovery/client | Typed, redacted peer errors; explicit retry classification; reject task-required legacy tools; distinguish premature stream closure. Modern-only SDK peer, task-required descriptor and disconnect fixtures fail before the fix and pass after it. |
| MCPP-002 | `mcpwire`, registry client/transport/discovery, tool descriptor contracts | Modern-first safe discovery with legacy fallback; exact metadata/headers; typed results; bounded schemas/cache hints. Matrix of modern-only, dual-era and legacy peers, wrong headers, malformed results, redirects and cancellation. |
| MCPP-003 | Runtime MCP client, both bundled server lifecycles, shared wire tests | Dual-era internal path without changing approval/provenance identity or `mcp-system`'s stdlib-only binary. Stock modern and legacy clients both work without claiming Tasks. |
| MCPP-004 | Registry dispatch, repository/schema, cancellation, recovery | Opt-in inline Tasks; persist sealed handle before polling; bounded get/cancel and exact terminal mapping. Schema-pinned peers prove no duplicate call, no handle leakage and no traffic after withdrawal. |
| MCPP-005 | Repository lifecycle/schema, orchestrator poller, runtime job/worker contract, generalized join | Durable operation authorization, deferred receipts and slot release; one continuation across mixed deferred work. Restart, competing pollers, lost delivery, cancel and deletion races are deterministic. |
| MCPP-006 | Public operation read/cancel contracts, Flutter API/mappers/chat/events/cards | Reopenable remote-operation state and explicit cancellation receipts; no raw task IDs. Every lifecycle state, reconnect and out-of-order response has UI coverage. |
| MCPP-007 | MRTR codec, bounded input service, consent and Flutter forms | User-answered form input for non-mutating tools; no capability advertisement until supported. Wrong destination, stale/duplicate keys, excessive rounds/bytes and model-provided answers fail closed. |
| MCPP-008 | Future MCPX-001 host bridge, not bundled file/system tools | Optional host-facing Tasks mapping with scoped client identity, authentication on every request and task isolation. Only after the host bridge exists. |
| MCPP-009 | Documentation, pinned test schemas and conformance fixtures | Release-time standards review distinguishing stable core, stable extensions and drafts. No recurring automation or automatic dependency/policy change. |

### Dependency and rollout rules

- MCPP-001 builds on shipped CON-001; MCPP-002 also uses shipped CON-002.
  Neither waits on native providers, mobile identity or portable skills.
- MCPP-003 follows MCPP-002 but is not a prerequisite for consuming external
  Tasks. Short bundled tools do not need a task server.
- MCPP-004 follows MCPP-002 and existing CXL-001 fences; its inline budget
  defaults to the current tool deadline, with no implicit timeout increase.
- MCPP-005 follows MCPP-004 and the merged team join/lifecycle foundation.
  TUR-010 governs continuation queue waiting. Its operation-authorization
  contract is a mandatory entry gate, not an optional future enhancement.
- MCPP-006 consumes MCPP-005's operation APIs and reuses the team client
  components when merged. It is a default-enable gate for MCPP-005, not a
  reason to ship invisible long-running operations.
- MCPP-007 depends on MCPP-002/004/006 and explicit input disclosure. Child
  egress consent, PROV-001 and SEC-001 do not substitute for that consent.
- MCPP-008 depends on MCPX-001 and a separately reviewed server-task mapping.
  It must not expose a new public MCP port.

Proposed flags keep legacy behavior as default initially:
`TURING_MCP_CLIENT_ERA=legacy|dual`,
`TURING_MCP_TASKS_ENABLED=false`, and
`TURING_MCP_DEFERRED_RESULTS_ENABLED=false`.
Do not declare the extension merely because the peer supports it.

Default-on Tasks, deferred results and input support require the appropriate
conformance matrix, compatible older workers/clients, visible state,
rollback/upgrade fixtures and EVAL-001 evidence. Core dual-era interoperability
alone requires its compatibility matrix, not the whole-agent benchmark.
No fixture-only substitute waives EVAL-001. Repeat the deterministic lifecycle
scenarios 20 times under the race detector, with zero safety/privacy failures.
For deferred results, specifically prove that at concurrency one an unrelated
turn runs while a remote task waits, a mixed team/task turn joins once, and
restart/cancellation/deletion never replay a mutation or leak content.
Report usefulness, faithfulness, latency, safety and privacy separately; better
task success cannot compensate for a privacy failure. Raised concurrency
remains a separate AGT-000 decision.

### Useful modern features beyond Tasks

Carry output schemas and arbitrary JSON structured results without silently
dropping data. Bound schema/reference processing and never fetch remote schema
references without policy. Treat discovery cache TTL as a refresh hint, not
permission to replace the tool snapshot on an already-queued run.

OpenTelemetry propagation can disclose metadata to a remote server; keep it
opt-in. OAuth issuer binding, authorization response validation and client
registration belong with the separate OAuth architecture, not an automatic
extension of static bearer support. Task subscriptions can reduce polling,
but are not needed for correctness in the first polling implementation.

## 8. What this plan deliberately does not adopt

- The incompatible 2025-11-25 experimental Tasks API.
- Legacy SSE resumability as a new strategic investment. Legacy peers remain
  supported within explicitly documented limits.
- Deprecated Sampling, Roots or Logging capabilities.
- Unreviewed stdio servers, package bootstrap/install commands, broader OAuth
  or public host exposure.
- MCP Apps, Skills-over-MCP, or Server Cards merely because they are official
  extensions. Each needs its own product/trust-boundary review.
- Model-answered elicitation, implicit permission expansion, or a task handle
  treated as mutation approval.
- Tasks on `mcp-files` or `mcp-system`; their current tools are short-lived.
- Provider-hosted MCP execution or code-mode execution as a shortcut around
  Turing's policy boundary. Those are separate execution architectures, not
  prerequisites for modern MCP; they would require proof that every operation
  still crosses Turing's authorization and egress checks.

## 9. Evidence limitations

The investigation combined primary specifications, SDK source, repository
inspection and existing stock-peer tests. It did not measure adoption among
third-party servers, run a real long-running task service, or establish
live-model quality. Passing a legacy stock-peer test is not a current-protocol
or Tasks certification. The extension evolves independently, so later work
must review changes from the pinned snapshot rather than silently following
its main branch.

The immediate implementation recommendation is **MCPP-001**, followed by
**MCPP-002**. The user-visible capacity benefit comes from MCPP-005/006, not
from a protocol date bump or simply increasing timeouts.

## Primary sources

[S1]: https://modelcontextprotocol.io/specification/versioning
[S2]: https://github.com/modelcontextprotocol/modelcontextprotocol/blob/5f5440bb26a62e2cf3440b92da5a667efa03b267/docs/specification/2026-07-28/changelog.mdx
[S3]: https://github.com/modelcontextprotocol/modelcontextprotocol/tree/5f5440bb26a62e2cf3440b92da5a667efa03b267/docs/specification/2026-07-28/basic
[S4]: https://github.com/modelcontextprotocol/ext-tasks/blob/899c1bbc869567670093face3591b39c28ce3219/specification/2026-07-28/tasks.md
[S5]: https://github.com/modelcontextprotocol/ext-tasks/blob/0d0a6bd4c258b35caa3c810a1dd506cf105b1501/schema/2026-07-28/schema.json
[S6]: https://github.com/modelcontextprotocol/modelcontextprotocol/blob/5f5440bb26a62e2cf3440b92da5a667efa03b267/docs/specification/2026-07-28/deprecated.mdx
[S7]: https://github.com/modelcontextprotocol/go-sdk/blob/v1.8.0/docs/protocol.md
[S8]: https://github.com/modelcontextprotocol/go-sdk/issues/942
[S9]: https://github.com/modelcontextprotocol/conformance

- [Current-version declaration][S1].
- [Immutable released-core changelog][S2] and [core transport, versioning,
  cancellation and MRTR definitions][S3].
- [Pinned Tasks specification][S4] and [stable schema][S5].
- [Released deprecated-feature registry][S6].
- [Go SDK v1.8.0 protocol behavior][S7] and [Tasks implementation status][S8].
- [Official conformance fixtures][S9]; pin an exact compatible revision when
  adding them to verification.
