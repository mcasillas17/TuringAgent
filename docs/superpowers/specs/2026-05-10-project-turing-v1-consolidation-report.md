# TuringAgent v1.0 Consolidation Report

**Historical report:** This record is not current implementation or status.
Use the [canonical roadmap](../../NORTH_STAR.md) for current truth.

## Sources compared

- Hybrid Copilot spec: `docs/superpowers/specs/2026-05-09-project-turing-v1-design-copilot.md`
- Claude spec: `docs/superpowers/specs/2026-05-09-project-turing-v1-design-claude.md`
- Claude consolidation report: `docs/superpowers/specs/2026-05-10-project-turing-v1-consolidation-report-claude.md`

## Executive summary

The two approaches now converge on a hybrid v1.0: keep Copilot's client protocol, product slice, files/approval ambition, optional explicit cloud adapter, and detailed API/event behavior; adopt Claude's split runtime, Docker isolation, `.env` API-key auth, internal API, per-agent MCP tokens, direct agent-to-MCP calls, and stronger phase/failure discipline.

The consolidated architecture is no longer a single in-process orchestrator. v1.0 should run a public `turing-orchestrator` and a separate `turing-agent-runtime-general`. The orchestrator owns public REST/WebSocket, SQLite, policy, approvals, audit, jobs, and event streaming. The agent runtime claims jobs through an internal API, calls models, calls MCP servers directly, and sends before/after beacons plus events back to the orchestrator.

## Areas of agreement

| Area | Shared direction |
|---|---|
| Product model | One backend brain, multiple thin client faces, MCP/tool hands. |
| First client | Flutter first, with client-agnostic REST/WebSocket APIs. |
| Backend host | Mac Mini local-first runtime. |
| Database | SQLite is the canonical local database for v1.0. |
| Ollama | Ollama runs on the Mac host and is reached from Docker through `host.docker.internal:11434`. |
| Docker | Backend services run through Docker Compose. |
| Public exposure | Only the orchestrator exposes a host port. |
| Native OS | Native macOS and Windows bridges are not v1.0 work. |
| Semantic memory | Vector/semantic memory is deferred. |
| External integrations | Google/Microsoft OAuth and similar integrations are deferred. |
| LangGraph/Redis | LangGraph, Redis, BullMQ, NATS, and graph orchestration are not required for v1.0. |
| Reconnect | Events must be persisted so clients can reconnect and recover state. |
| Audit | Tool/security activity should be durable and inspectable. |

## Consolidated decisions

| Topic | Consolidated decision |
|---|---|
| Runtime process model | Adopt Claude's split process: orchestrator plus separate `turing-agent-runtime-general`. |
| Public API | Keep Copilot's REST-for-commands and WebSocket-for-events model. |
| Client auth | Adopt Claude's `.env` API key: `TURING_CLIENT_API_KEY`; defer setup/login/JWT auth. |
| Internal auth | Add `TURING_INTERNAL_TOKEN` for agent-runtime to orchestrator internal API. |
| MCP auth | Add per-agent MCP bearer tokens, e.g. `MCP_SYSTEM_TOKEN_GENERAL` and `MCP_FILES_TOKEN_GENERAL`. |
| Tool call path | Agent runtime calls MCP servers directly; orchestrator authorizes/audits with before/after beacons. |
| SQLite ownership | Orchestrator is the only canonical SQLite writer. |
| Event replay | Use both `eventId` and monotonic `sequence`; write events before broadcast. |
| Model providers | Ollama default; optional OpenAI-compatible provider only with explicit user selection/config. |
| System MCP | Include in v1.0. |
| Files MCP | Keep in v1.0 as the approval/security proving ground; this intentionally broadens Claude's smaller phases 0-5 cut. |
| Approvals | Active approval records and Flutter approval cards; approval-required runs use `waiting_approval`; approval JWT is consumed by MCP. |
| Shared contracts | Add `turing-backend/shared-types` for event/job/API/tool DTOs. |
| Repo layout | Use `turing-backend/` plus `turing-client/{flutter_app,macos_app,windows_app,android_app}/`; rename existing `turing_app` to `flutter_app`. |
| Interfaces | Define `AgentExecutor`, `LlmProvider`, and `SecretsBackend` seams explicitly. |

## Final v1.0 baseline

v1.0 should ship:

- `turing-orchestrator` public REST/WS on port 3000 and internal API on port 3001.
- `turing-agent-runtime-general` as a separate worker container.
- `turing-mcp-system` and `turing-mcp-files` as internal Go MCP services.
- Docker network isolation so clients cannot reach runtime/MCP services directly.
- SQLite tables for settings, sessions, messages, runs, run steps, jobs, events, tools, tool calls, approvals, and audit logs.
- `.env.example`, backend `.gitignore`, `scripts/init.sh`, `scripts/dev.sh`, `scripts/reset.sh`, `scripts/rotate-client-key.sh`, generated client/internal/MCP secrets, and key rotation docs.
- Flutter client connection/settings screen for backend URL and API key.
- REST session/message/approval/audit/tool APIs.
- WebSocket event subscription, replay by `lastSequence`, delta streaming, and reconnect behavior.
- Ollama local streaming path.
- Optional OpenAI-compatible adapter if it does not block the local path.
- Active files approval flow for create/update; delete/move disabled.
- Shared `AgentExecutor`, `LlmProvider`, and `SecretsBackend` interfaces so later runtime/provider/secrets changes stay mechanical.

v1.0 should not ship:

- Client JWT login, setup users, refresh tokens, or an `api_keys` table.
- Native macOS or Windows bridge.
- Files delete/move.
- Google/Microsoft OAuth.
- Semantic/vector memory.
- Redis, BullMQ, NATS, LangGraph, or distributed graph orchestration.
- Vision, voice, IoT, or sensor integrations.

## Runtime architecture

```text
Flutter client
  -> REST commands / WebSocket events
  -> turing-orchestrator:3000
       - public API auth via TURING_CLIENT_API_KEY
       - session/message/run/job/event persistence
       - policy, approvals, audit, event streaming
       - internal API on 3001
  -> SQLite

turing-agent-runtime-general
  -> internal API with TURING_INTERNAL_TOKEN
  -> claims jobs and fetches context
  -> calls Ollama/OpenAI-compatible provider
  -> sends model deltas/events/completion to orchestrator
  -> sends before/after beacons for tool calls
  -> calls MCP servers directly with per-agent tokens

turing-mcp-system / turing-mcp-files
  -> validate per-agent MCP token
  -> validate approval JWT for approval-required tools
  -> execute allowed tool calls only
```

## Security architecture

Security is layered instead of relying on a single check:

1. Public clients authenticate with `TURING_CLIENT_API_KEY`.
2. Only orchestrator publishes host port 3000.
3. Agent runtime authenticates to internal API with `TURING_INTERNAL_TOKEN`.
4. Docker networks restrict which services can reach each other.
5. MCP servers require per-agent bearer tokens.
6. Orchestrator policy remains default-deny and is checked before tool execution.
7. Approval-required tools need an orchestrator-issued, args-bound, single-use approval JWT.
8. Audit logs record auth failures, policy decisions, tool attempts, approvals, denials, and failures.

## What changed from the original Copilot approach

- Replaced in-process `AgentExecutor` with a separate agent-runtime container.
- Added internal orchestrator API on port 3001.
- Changed MCP invocation from orchestrator-mediated calls to agent-runtime direct calls.
- Added before/after tool beacons so the orchestrator still authorizes and audits direct MCP use.
- Adopted per-agent MCP tokens and internal API tokens.
- Replaced JWT login/setup with `.env` API-key auth.
- Added shared contract package and more explicit repo layout.
- Added concrete bootstrap/reset/dev/key-rotation scripts, concrete `.gitignore`, `SecretsBackend`, and `LlmProvider`.

## What changed from Claude's approach

- Kept REST commands and WebSocket events instead of WebSocket commands as the primary mutation path.
- Kept files MCP in v1.0 rather than deferring it, because it exercises approval, sandboxing, and audit.
- Kept active Flutter approval cards in v1.0.
- Kept optional OpenAI-compatible provider as explicit user-selected routing, not silent fallback.
- Kept a richer public API surface for sessions, messages, tools, approvals, audit, and event replay.
- Kept `sequence` replay in addition to ULID event IDs for simple gap detection.

## Revised implementation phases

### Phase 0: Scaffolding and secrets

- Add orchestrator, agent-runtime, shared-types, system MCP, and files MCP packages.
- Replace old backend stubs with Docker Compose services and networks.
- Use `turing-backend/{orchestrator,agent-runtime,shared-types,mcp-system,mcp-files,infra,scripts,data}`.
- Rename `turing-client/turing_app` to `turing-client/flutter_app` and reserve `macos_app`, `windows_app`, and `android_app`.
- Add `.env.example`, backend `.gitignore`, `scripts/init.sh`, `scripts/dev.sh`, `scripts/reset.sh`, `scripts/rotate-client-key.sh`, and migration skeleton.
- Demo goal: init creates secrets and Compose validates/builds service skeletons.

### Phase 1: Orchestrator REST, auth, SQLite, jobs

- Add public API-key middleware and internal-token middleware.
- Add migrations, sessions/messages, jobs, and audit base.
- Demo goal: valid key creates a session/message; runtime can claim a job with the internal token.

### Phase 2: WebSocket events and replay

- Add authenticated WebSocket, durable event writes, replay by `lastSequence`, and slow socket handling.
- Demo goal: disconnect/reconnect replays missed events.

### Phase 3: Agent runtime and model streaming

- Add runtime job polling, context fetch, Ollama streaming, optional OpenAI-compatible adapter, and completion posting.
- Demo goal: client sends a message, runtime streams model deltas, and SQLite persists the conversation.

### Phase 4: System MCP

- Add Go system MCP, direct runtime MCP calls, per-agent token validation, and before/after audit beacons.
- Demo goal: a time/date request triggers `system.time`, records audit rows, and streams tool events.

### Phase 5: Files MCP and approvals

- Add files sandbox, safe read/list/search, approval-required create/update, Flutter approval cards, and approval JWT validation.
- Demo goal: create/update blocks for approval, approval issues JWT, MCP validates it, and the result is audited.

### Phase 6: End-to-end hardening

- Add Docker, Flutter, reconnect/replay, audit/tool-call, and failure-mode smoke tests.
- Demo goal: clean checkout can run the documented local smoke path and recover events after reconnect.

## Downstream updates required

1. **Canonical spec**
   - Ensure all references use split orchestrator/runtime, `.env` API-key auth, internal token, per-agent MCP tokens, and direct runtime-to-MCP calls.
   - Remove stale first-run setup/login, refresh-token, and in-process executor assumptions.

2. **Implementation plan**
   - Rewrite from scratch. The existing plan is stale because it assumes JWT auth and an in-process executor.
   - Reflect the six revised phases above.

3. **Flutter client scope**
   - Replace login/setup with backend URL and API-key connection settings.
   - Store API key in secure storage.
   - Add approval-card UI for files create/update.
   - Rename the existing app directory to `turing-client/flutter_app`; this is decided, not open.

4. **Database schema**
   - Keep orchestrator-owned SQLite only.
   - Do not add `users`, `refresh_tokens`, `api_keys`, per-agent DBs, or attachments in v1.0.

5. **Security docs**
   - Document client key, internal token, MCP tokens, approval JWTs, Docker networks, and token rotation.
   - Keep JWT terminology only for future approval tokens, not client login.

6. **Tests**
   - Replace JWT/login tests with API-key, internal-token, and MCP-token tests.
   - Add job claim/reaper, event replay, beacon denial, approval JWT, and direct MCP smoke tests.

## Decisions already resolved before planning

1. Files MCP and active approval UI remain in v1.0 Phase 5.
2. Optional OpenAI-compatible provider is included as an explicit adapter, but Ollama remains the default and no cloud fallback is automatic.
3. The current Flutter app is renamed from `turing-client/turing_app` to `turing-client/flutter_app`.
