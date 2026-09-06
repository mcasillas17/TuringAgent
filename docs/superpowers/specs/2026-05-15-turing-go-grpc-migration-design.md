# Turing Orchestrator Go + gRPC Migration Design

**Historical design:** This record is not current implementation or status.
Use the [canonical roadmap](../../NORTH_STAR.md) for current truth.

## Status

Approved design for migrating the current Node.js/TypeScript Turing backend runtime to Go and replacing client WebSocket/REST APIs with gRPC.

The migration approach is contract-first and side-by-side: define stable `.proto` contracts, build Go services against the current SQLite-backed behavior, migrate Flutter and generated future-client stubs, verify parity, then remove the TypeScript orchestrator/runtime and REST/WebSocket surfaces.

## Goals

- Replace the TypeScript orchestrator and TypeScript agent-runtime with Go services.
- Remove npm runtime/package exposure from backend orchestration and agent execution.
- Make gRPC the public client API; remove REST and WebSocket APIs during the cutover.
- Use gRPC server-streaming for AI token responses.
- Preserve the separate orchestrator and agent-runtime service boundary.
- Use gRPC for internal orchestrator/runtime communication.
- Preserve SQLite as the canonical local database and keep the current schema except for explicit forward-only migrations needed for cancellation and worker leasing.
- Keep existing Go MCP servers on MCP JSON-RPC/HTTP, called safely by the Go agent-runtime.
- Treat all dynamic JSON from local model providers and MCP servers as untrusted input.
- Propagate gRPC stream cancellation through orchestrator cleanup, runtime cancellation, and downstream local model/tool calls.
- Translate Promise.all-style concurrent work into idiomatic Go using `errgroup.WithContext` or goroutines with explicit error propagation.
- Generate and check in client/server stubs for Flutter plus future macOS, Windows, and Android clients.

## Non-goals

- Adding user accounts, password login, refresh tokens, or external OAuth.
- Replacing SQLite with a networked database.
- Rewriting MCP servers to native gRPC.
- Preserving REST or WebSocket as long-term product APIs.
- Adding a broad compatibility bridge once the Go/gRPC API reaches parity.
- Adding a JavaScript/TypeScript backend runtime dependency in the target architecture.

## Existing system summary

The current backend has:

- a TypeScript `@turing/orchestrator` service with public REST, WebSocket streaming, internal HTTP APIs, SQLite persistence, auth, approvals, audit, jobs, and events;
- a TypeScript `agent-runtime` that polls jobs, fetches context, streams model providers, calls MCP servers, and posts events/completion/failures back to the orchestrator;
- Go MCP servers for system and file tools using MCP JSON-RPC/HTTP;
- a Flutter client that uses REST commands/queries plus WebSocket event streaming.

The target keeps the same logical responsibilities but moves orchestration and runtime execution to Go and replaces client transport with gRPC.

## Target architecture

```text
Generated clients
  Flutter now
  macOS / Windows / Android stubs for future clients
        |
        | public gRPC, bearer API key metadata
        v
Go turing-orchestrator
  - public gRPC API
  - SQLite canonical state
  - sessions, messages, runs, jobs, events
  - auth, approvals, tool policy, audit
  - run event bus and stream fan-out
        |
        | internal gRPC, internal bearer token
        v
Go turing-agent-runtime-general
  - worker connection to orchestrator
  - prompt/context assembly
  - local model streaming
  - MCP JSON-RPC/HTTP calls
  - tool beacons, run updates, completion/failure
        |
        | internal HTTP, per-MCP bearer tokens
        v
Go MCP servers + local model providers
```

The orchestrator remains the source of truth. It owns database writes, event sequencing, auth decisions, approval state, audit records, and client streams. The runtime owns execution of the `general_assistant` agent and communicates progress through internal gRPC messages.

The TypeScript services may run side-by-side only during migration. They are removed after proto parity, SQLite behavior parity, and Flutter gRPC parity are verified.

## Proto layout

Use `proto/turing/v1` as the source of truth. Generated code should be checked into the repository for Go and supported client targets so normal builds do not require npm or runtime codegen.

Required files:

| File | Purpose |
| --- | --- |
| `common.proto` | Shared IDs, timestamps, enums, request metadata, pagination, and error details. |
| `sessions.proto` | Session CRUD, message listing, provider config, agent listing. |
| `chat.proto` | Client message submission and AI token streaming. |
| `events.proto` | Canonical event model, replay, and live session subscription. |
| `approvals.proto` | Client approval approve/deny and runtime approval consumption shapes. |
| `tools.proto` | Tool policy, tool-call beacon, tool-call summaries. |
| `runtime.proto` | Internal worker connection, job assignment, run updates, cancellation, completion, failure. |
| `mcp.proto` | Safe MCP JSON-RPC request/result envelopes for runtime internals. |
| `health.proto` | Health and version RPCs. |

### Dynamic data policy

Core protocol fields stay typed: IDs, statuses, event types, roles, model providers, tool policies, timestamps, and known request fields.

Use `google.protobuf.Struct` only at dynamic boundaries:

- `TuringEvent.payload`
- MCP tool args/results
- model provider metadata
- tool schema fragments
- structured error details
- audit payload snapshots

Do not use `Struct` for fields that have stable TypeScript types today.

### Public client services

`ChatService.SendMessage` is the primary streaming path:

```proto
rpc SendMessage(SendMessageRequest) returns (stream ChatStreamEvent);
```

`SendMessage` validates the request, creates the user and assistant messages, creates the run/job, then streams queued/started events, token deltas, tool/approval events, completion, or run failure.

Required public services:

- `SessionService`
  - `CreateSession`
  - `ListSessions`
  - `GetSession`
  - `ListMessages`
  - `GetConfig`
  - `ListAgents`
  - `ListTools`
- `ChatService`
  - `SendMessage` server-streaming
- `EventService`
  - `ListEvents`
  - `SubscribeSessionEvents` server-streaming for replay/live follow after reconnect
- `ApprovalService`
  - `ApproveApproval`
  - `DenyApproval`
- `AuditService`
  - `ListAuditEntries`
  - `ListToolCalls`
- `HealthService`
  - `Check`
  - `Version`

Although `SendMessage` is the primary AI token path, `SubscribeSessionEvents` remains useful for reconnect, replay, multi-window observation, and passive session updates.

### Stream event shape

`ChatStreamEvent` should use a typed `oneof` envelope:

- `run_queued`
- `run_started`
- `message_started`
- `token_delta`
- `tool_call_started`
- `tool_call_completed`
- `tool_call_failed`
- `approval_requested`
- `approval_approved`
- `approval_denied`
- `approval_expired`
- `approval_consumed`
- `message_completed`
- `run_completed`
- `run_failed`
- `run_cancelled`
- `event` for a full persisted `TuringEvent` when clients need canonical replay shape

Invalid client requests return gRPC status errors immediately. Runtime/model/tool failures become typed stream events, are persisted as failed runs, and then the stream ends normally unless transport itself failed.

## Internal runtime communication

The Go runtime should initiate an internal authenticated worker connection to the orchestrator. A bidirectional worker stream is the cleanest shape for assignment, updates, heartbeats, and cancellation:

```proto
rpc ConnectWorker(stream RuntimeUpdate) returns (stream RuntimeCommand);
```

`RuntimeCommand` includes:

- `worker_accepted`
- `run_assigned`
- `run_cancelled`
- `approval_updated`
- `shutdown_requested`

`RuntimeUpdate` includes:

- `worker_ready`
- `heartbeat`
- `event`
- `tool_beacon`
- `approval_poll`
- `run_completed`
- `run_failed`
- `run_cancelled_ack`

This keeps job claiming runtime-initiated while allowing the orchestrator to push cancellation immediately. The Go migration should implement this bidirectional worker stream before removing the TypeScript runtime.

## SendMessage data flow

1. Client calls `ChatService.SendMessage` with API-key metadata.
2. Orchestrator validates metadata, request shape, session existence, model provider, and model name.
3. Orchestrator opens a transaction to insert:
   - user message,
   - placeholder assistant message,
   - agent run,
   - job,
   - initial queued event.
4. Orchestrator subscribes the stream to the run/session event bus before publishing the queued event to avoid missed events.
5. Runtime worker receives the assigned run over internal gRPC.
6. Runtime fetches message context and any needed tool metadata.
7. Runtime starts model generation with a context derived from the assigned run context.
8. Runtime sends token deltas and other run updates to the orchestrator.
9. Orchestrator persists canonical events, updates run state, and forwards stream events to the client.
10. On completion, orchestrator stores final assistant content, marks the job/run completed, emits completion events, and closes the stream.

## Cancellation and cleanup

Every streaming handler must treat `stream.Context().Done()` as authoritative.

For `SendMessage`:

- derive a cancellable run context from `stream.Context()`;
- `defer` event-bus unsubscribe, channel close ownership cleanup, and context cancellation;
- select on event channel, terminal run channel, and `ctx.Done()`;
- if the client disconnects before a terminal run state, mark job/run cancelled in SQLite;
- publish `run_cancelled`;
- send a runtime `run_cancelled` command over internal gRPC;
- return `codes.Canceled` only for transport cancellation after cleanup is complete.

For runtime execution:

- maintain a `runID -> cancel` map for active model/tool work;
- derive provider and MCP call contexts from the run context;
- pass context into local model HTTP requests and MCP HTTP requests;
- stop reading SSE/NDJSON/model bodies when context is cancelled;
- close output channels from the producer side only;
- acknowledge cancellation to the orchestrator;
- avoid goroutine leaks by waiting for model/tool goroutines to exit before deleting active-run state.

If the internal worker stream disconnects, the orchestrator must lease or requeue unfinished work according to the existing job timeout/max-attempt policy.

## Dynamic JSON safety

Local model providers and MCP servers can return arbitrary JSON. The Go implementation must never assume those payloads are well-formed beyond the boundary being parsed.

Required practices:

- bound request and response bodies with `io.LimitReader` or server/client max message sizes;
- use `json.Decoder.UseNumber()` when decoding arbitrary JSON;
- decode provider streaming chunks into narrow structs with `json.RawMessage` for nested unknown fields;
- validate every optional nested field with explicit type checks;
- convert arbitrary JSON to protobuf `Struct` only through a shared `safejson` helper;
- recursively normalize values before `structpb.NewStruct`, rejecting unsupported values such as `NaN`, `Inf`, functions, channels, or non-string map keys;
- return typed errors instead of panicking on malformed chunks;
- keep malformed provider chunks isolated to the affected run and record a failed run event.

The shared helper must expose functions equivalent to:

- `DecodeObject(decoder) (map[string]any, error)`
- `Normalize(value any) (any, error)`
- `ToStruct(map[string]any) (*structpb.Struct, error)`
- `Summary(value any, maxBytes int) string`

All handlers that accept dynamic JSON from MCPs, local models, or future tool schemas must use this helper rather than ad hoc type assertions.

## Concurrency design

Where TypeScript used `Promise.all`, Go should use `errgroup.WithContext` when sibling work should cancel on the first error. Use plain `sync.WaitGroup` only when all errors are collected independently and cancellation is not needed.

Examples:

- fetching message context and independent runtime metadata;
- listing tools from `system` and `files` MCP servers;
- fetching tool schemas or provider config from independent sources;
- running bounded parallel tool-data preloads.

Rules:

- create the group from the current request/run context;
- protect shared output with local variables assigned by one goroutine each, or a mutex when necessary;
- return contextual errors with operation names;
- use bounded concurrency for any untrusted or user-expanded work;
- after `group.Wait()`, check `ctx.Err()` to distinguish cancellation from provider/tool failure.

## Persistence

Keep SQLite as the canonical local database and preserve the existing schema except for explicit forward-only migrations needed for cancellation and worker leasing:

- `sessions`
- `messages`
- `agent_runs`
- `jobs`
- `events`
- `tools`
- `tool_calls`
- `approvals`
- `audit_logs`
- `settings`

Go code should use transactions for multi-row state transitions, especially message enqueue, run completion, failure, approval transitions, and cancellation.

The migration should keep the existing event sequence semantics: per-session monotonically increasing `sequence`, replay by `session_id` and `sequence`, and replay limit behavior. If any schema changes are required for cancellation or worker leasing, add forward-only migrations and keep backward-compatible reads during the side-by-side phase.

Use `database/sql` with `github.com/mattn/go-sqlite3`. Dockerfiles must provide the CGO build environment needed for that driver. This keeps the Go dependency surface small while preserving SQLite WAL support and mature behavior.

## Auth and service security

Preserve the current local auth model:

- public gRPC clients send the local client API key in metadata;
- internal runtime RPCs use `TURING_INTERNAL_TOKEN` in metadata;
- runtime-to-MCP calls keep per-server/per-agent bearer tokens;
- approval-required MCP calls keep short-lived approval JWTs;
- tokens are compared with constant-time comparison;
- gRPC reflection is disabled by default and enabled only in explicit development mode;
- message size limits and deadlines are configured for public and internal servers;
- logs and errors never include bearer tokens, approval JWTs, raw secrets, or full sensitive tool args.

No external account system is introduced in this migration.

## Generated stubs and clients

The repository should include generated stubs for:

- Go orchestrator/runtime server and client code;
- Dart/Flutter client code used by the current app;
- Swift/macOS future client stubs;
- C# or suitable Windows future client stubs;
- Kotlin/Java Android future client stubs.

Flutter must be integrated end-to-end in this migration. Future platform stubs are generated, committed, and covered by deterministic regeneration checks; they are not wired into platform apps until those apps are implemented.

Generated code should be checked in. Code generation tooling can be documented and scripted, but normal backend runtime/builds should not depend on npm.

## Flutter migration surfaces

Replace the current Flutter REST/WebSocket networking layer with gRPC clients:

- API-key metadata interceptor;
- `SessionService` calls for sessions, messages, config, agents, and tools;
- `ChatService.SendMessage` stream for sending a prompt and rendering token deltas;
- `EventService.SubscribeSessionEvents` for reconnect/replay/passive updates;
- `ApprovalService` for approval cards;
- error mapping from gRPC status to user-visible UI messages;
- stream cancellation when leaving a chat, switching sessions, or closing the app.

The UI should remain protocol-driven: chat screens consume typed stream events instead of hand-parsed WebSocket JSON.

## Testing strategy

Required tests:

- proto/golden tests for generated JSON/proto compatibility and stable enum values;
- unit tests for auth interceptors, config loading, services, repositories, tool policy, approvals, and audit;
- database migration tests against the current SQLite schema;
- parity tests comparing key TypeScript behaviors before removal:
  - session creation,
  - message enqueue,
  - event sequencing/replay,
  - tool beacon decisions,
  - approval approve/deny/expire/consume,
  - run completion/failure;
- table tests for malformed model provider chunks and malformed MCP results;
- fuzz-style tests for dynamic JSON normalization and `Struct` conversion;
- stream tests that cancel `SendMessage` and assert:
  - stream handler exits,
  - event subscription is removed,
  - run/job is cancelled,
  - runtime receives cancellation,
  - model/MCP context is cancelled,
  - goroutines exit;
- errgroup tests proving first-error cancellation and no partial success-shaped results;
- integration tests with fake model and fake MCP servers for:
  - token streaming,
  - tool call success,
  - approval-required tool calls,
  - run failure,
  - reconnect/replay;
- Flutter integration or widget tests around gRPC stream consumption and cancellation.

## Cutover plan at a high level

1. Add proto contracts and generated stubs.
2. Build Go orchestrator services against the existing SQLite schema.
3. Build Go runtime worker and provider/MCP clients.
4. Wire fake model/MCP integration tests.
5. Migrate Flutter networking to gRPC.
6. Run Go and TypeScript systems side-by-side against isolated databases for parity tests.
7. Switch local Docker Compose to Go services.
8. Remove TypeScript orchestrator/runtime packages, REST routes, WebSocket gateway, and smoke scripts that depend on WebSocket.
9. Update README and operational docs.

## Fixed implementation decisions

- SQLite access uses `database/sql` with `github.com/mattn/go-sqlite3`.
- Generated code is checked in under language-specific directories rooted at `gen/turing/v1/`.
- Internal runtime communication uses the bidirectional `ConnectWorker` stream.
- Flutter is the only client integrated end-to-end in this migration.
- Future macOS, Windows, and Android stubs are generated and committed, with deterministic regeneration checks.

The final backend runtime is Go, public client API is gRPC, AI token responses use server-streaming, dynamic JSON is safely handled, cancellation propagates end-to-end, and REST/WebSocket/TypeScript backend runtime are removed after parity.
