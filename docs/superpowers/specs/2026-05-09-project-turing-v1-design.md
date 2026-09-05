# TuringAgent v1.0 Design

**Historical design:** This record is not current implementation or status.
Use the [canonical roadmap](../../NORTH_STAR.md) for current truth.

## Problem

TuringAgent should become a local-first personal AI orchestration platform, not a single chatbot or a client-specific app. The v1.0 release needs a strong foundation that proves the core pipeline while avoiding premature complexity across native apps, distributed agents, semantic memory, external OAuth integrations, vision, voice, and IoT.

This design defines the v1.0 architecture around one backend brain, one first client face, and two initial tool hands. It supersedes earlier v1.0 notes that require Redis/BullMQ, separate agent containers, or a distributed agent runtime from day one.

## Goals

- Build a local-first, client-agnostic Node.js/TypeScript orchestrator.
- Use SQLite as the canonical local database for sessions, messages, events, jobs, approvals, audit logs, settings, and auth state.
- Route model calls to Ollama by default, with an optional OpenAI-compatible provider used only by explicit user selection.
- Stream responses and tool/run events to clients over WebSocket.
- Use Flutter as the first production client while keeping it thin and protocol-driven.
- Prove MCP tooling with safe system tools and sandboxed file tools.
- Include authentication, tool policy, approvals, and audit logs from day one.
- Design for concurrent agent runs, but implement one primary agent first.

## Non-goals for v1.0

- Native macOS app or bridge implementation.
- Native Windows app or bridge implementation.
- Semantic/vector memory.
- LangGraph or graph-based multi-agent orchestration.
- Redis, BullMQ, NATS, or distributed worker containers.
- Google/Microsoft OAuth integrations.
- Vision, voice, IoT, camera services, dog body-language analysis, or home automation.
- Arbitrary shell, AppleScript, PowerShell, screenshots, keyboard/mouse control, or unrestricted native automation.
- Destructive file operations such as delete and move.

## Architecture

TuringAgent v1.0 uses a thin orchestrator spine:

```text
Flutter client
  -> REST command/query API
  -> Node.js/TypeScript orchestrator
  -> SQLite persistence
  -> Ollama or explicitly selected OpenAI-compatible provider
  -> WebSocket event stream
  -> Flutter rendering
```

The orchestrator owns sessions, messages, auth, agent execution, model routing, tool registry, tool policy, approvals, audit logs, event persistence, and streaming. Clients do not own orchestration logic.

Agents run in-process in v1.0 behind an `AgentExecutor` interface. This keeps the first implementation simple while preserving a future split point for worker processes, LangGraph executors, or distributed agent containers.

Docker Compose should run:

- `turing-orchestrator`: Node.js/TypeScript service exposing port `3000`.
- `turing-mcp-system`: Go MCP server for safe system tools.
- `turing-mcp-files`: Go MCP server for sandboxed file tools.

Ollama runs directly on the Mac Mini host and is reached from Docker containers through:

```text
http://host.docker.internal:11434
```

Only the orchestrator exposes a host port. MCP servers stay on the internal Docker network.

## Orchestrator modules

The orchestrator should be one process but internally modular:

- `api`: REST routes.
- `ws`: WebSocket event streaming.
- `db`: SQLite connection, migrations, repositories, and transactions.
- `auth`: first-run setup, login, JWT access tokens, refresh tokens, logout, and revocation.
- `agents`: in-process agent executors and run lifecycle.
- `llm`: model-provider interface, Ollama adapter, and OpenAI-compatible adapter.
- `tools`: MCP registry, MCP clients, and tool invocation.
- `security`: tool policy, approvals, auth guards, and audit helpers.
- `events`: typed event envelopes, sequencing, persistence, and replay.
- `jobs`: SQLite-backed work state for run/job status.
- `logging`: structured JSON logs and correlation fields.

The first agent should be `general_assistant`. Additional agents can be added later without changing client contracts.

## API design

Use REST for commands and queries. Use WebSocket only for live events and state updates.

Minimum REST endpoints:

- `GET /health`
- `GET /version`
- `GET /config`
- `POST /setup`
- `POST /auth/login`
- `POST /auth/refresh`
- `POST /auth/logout`
- `POST /sessions`
- `GET /sessions`
- `GET /sessions/:sessionId`
- `GET /sessions/:sessionId/messages`
- `POST /sessions/:sessionId/messages`
- `GET /agents`
- `GET /tools`
- `POST /approvals/:approvalId/approve`
- `POST /approvals/:approvalId/deny`
- `GET /audit`
- `GET /tool-calls`

`POST /sessions/:sessionId/messages` should persist the user message, create an agent run, persist initial run state, start execution, and return identifiers immediately. The response should not wait for the final model answer.

The WebSocket endpoint requires authentication and subscribes a client to session events. It should support reconnect metadata:

- `sessionId`
- `lastSequence`

If the replay gap is available, the orchestrator sends missed events. If the gap is too large, it instructs the client to refetch messages and current run state.

## WebSocket event contract

Events should be typed envelopes with correlation and ordering:

```json
{
  "eventId": "evt_...",
  "sessionId": "sess_...",
  "runId": "run_...",
  "traceId": "trace_...",
  "sequence": 42,
  "type": "message.delta",
  "createdAt": "2026-05-09T00:00:00.000Z",
  "payload": {}
}
```

Minimum event types:

- `message.delta`
- `message.completed`
- `agent.run.started`
- `agent.run.step`
- `agent.run.completed`
- `tool.call.started`
- `tool.call.completed`
- `tool.call.failed`
- `approval.requested`
- `error`
- `system`

The orchestrator should coalesce tiny token deltas when needed, track socket send pressure, persist durable events, and degrade from token-level deltas to chunked deltas for slow clients. WebSocket disconnects should not cancel a run by default. Cancellation is explicit.

## SQLite persistence

SQLite is canonical v1.0 memory. The orchestrator is the only writer for canonical app data. MCP servers return tool results to the orchestrator and do not write orchestrator state directly.

Minimum tables:

- `schema_migrations`
- `users`
- `refresh_tokens`
- `settings`
- `sessions`
- `messages`
- `agent_runs`
- `agent_run_steps`
- `events`
- `jobs`
- `tools`
- `tool_calls`
- `approvals`
- `audit_logs`

SQLite should run in WAL mode with short transactions and a busy timeout. Migrations run on startup and fail fast if they cannot apply cleanly.

## Agent runtime and concurrency

v1.0 should design for concurrent runs but implement one primary agent first.

Each agent has:

- ID
- name
- description
- instructions
- allowed tools
- model preferences
- policy constraints
- optional event subscriptions

Each run has:

- `runId`
- `sessionId`
- `traceId`
- status
- event sequence
- cancellation hook
- tool-call records
- approval records

Concurrency limits should exist from day one:

- maximum active runs globally
- maximum active runs per session
- maximum tool calls per run
- model call timeout
- tool call timeout

The initial runtime should not include a multi-agent planner, autonomous swarm, or graph scheduler. The important v1.0 boundary is:

```ts
interface AgentExecutor {
  execute(input: AgentExecutionInput): AsyncIterable<AgentEvent>;
}
```

## Model routing

Ollama is the default provider. The optional cloud provider should be OpenAI-compatible, with configurable base URL and API key.

Cloud usage must be explicit. The orchestrator may use the OpenAI-compatible provider only when the user selects it for a message or session. If no cloud provider is configured, the local Ollama path remains fully functional.

Clients receive the same event types regardless of provider.

## MCP tools

Tool execution is default-deny. Agents receive allowlisted tools only. Each tool has a policy category:

- `safe`
- `approval_required`
- `disabled`

### `turing-mcp-system`

The first system MCP server should prove the tool pipeline with low-risk tools:

- `health.check`
- `time.now`
- `echo`
- limited `system.info`

### `turing-mcp-files`

The files MCP server supports a configurable list of approved host directories. It must:

- canonicalize paths
- reject path traversal
- reject symlink escapes
- enforce maximum file sizes
- keep operations inside approved directories
- return structured errors
- ensure every call is audited by the orchestrator

Allowed v1.0 file behavior:

- `files.list`: allowed inside approved directories
- `files.search`: allowed inside approved directories
- `files.read`: allowed inside approved directories
- `files.create`: approval required
- `files.update`: approval required
- `files.delete`: disabled
- `files.move`: disabled

## Approval flow

Sensitive actions follow a blocking approval flow:

1. Agent requests a sensitive tool action.
2. Orchestrator checks tool policy.
3. Orchestrator creates an approval record.
4. Orchestrator emits `approval.requested`.
5. Flutter displays an approval card.
6. User approves or denies.
7. Orchestrator executes only after approval.
8. Tool result, denial, expiration, or failure is persisted.
9. Audit log records the full outcome.

Approval denial and expiration are normal run outcomes, not silent failures.

## Authentication and networking

v1.0 is single-user/local but authenticated.

First-run setup creates one local admin user. The password is hashed in SQLite. Login returns a short-lived access JWT and a refresh token. Refresh tokens are stored and revocable in SQLite.

Authentication requirements:

- REST requires auth except `GET /health`, `GET /version`, and first-run setup/login endpoints.
- WebSocket requires auth.
- Flutter stores tokens through a secure-storage abstraction; Android and macOS targets use platform secure storage for v1.0.
- LAN and Tailscale provide reachability only; they are not authorization.
- Secrets are read from env/config and are not logged.

Android physical devices should connect to the Mac Mini over LAN or Tailscale, not `localhost`.

## Flutter client

Flutter is the first v1.0 client face. It remains thin and protocol-driven.

Responsibilities:

- first-run setup/login UI
- secure token storage for Android and macOS
- session list
- chat UI
- send message command
- WebSocket connection state
- streamed message rendering
- model selection per message/session
- approval cards
- reconnect and refetch behavior

Flutter should update the active streaming message in place instead of rebuilding the entire chat list for every token. It should persist the current `sessionId` and last seen event sequence so reconnect is deterministic.

The Flutter client should not own agents, model routing, tool routing, approvals, persistence, memory, or security decisions.

## Error handling

Errors should be visible, typed, and durable.

- REST validation failures return typed error responses.
- Run failures emit persisted `error` events.
- Model failures end the run with clear status.
- Tool failures persist tool-call records and audit entries.
- Approval denial or expiration is represented as a run step.
- WebSocket disconnects do not cancel runs.
- Explicit cancellation should be represented as a run state transition and event.

## Observability

Start with structured JSON logs. Log lines should include relevant correlation fields:

- `requestId`
- `sessionId`
- `runId`
- `traceId`
- `toolCallId`
- `agentId`
- `eventType`

Logs must not include secrets, auth headers, refresh tokens, API keys, or sensitive file contents.

OpenTelemetry, Grafana, and Tempo are optional later additions, not required for v1.0.

## Testing strategy

Backend tests should cover:

- first-run setup and login
- JWT and refresh-token behavior
- REST validation
- SQLite migrations
- session/message persistence
- event sequencing and replay
- mocked Ollama streaming
- mocked OpenAI-compatible streaming
- model selection rules
- tool policy decisions
- approval required, approved, denied, and expired paths
- MCP system client behavior
- MCP files client behavior
- file sandbox escape attempts

Flutter tests should cover:

- protocol model parsing
- WebSocket event application
- streaming bubble updates
- reconnect/refetch behavior
- login/setup flow
- approval card behavior
- model selection UI state

## Roadmap after v1.0

v1.1 should introduce the native macOS face and macOS bridge concept after the backend, Flutter chat, streaming, SQLite persistence, auth, and MCP tool pipeline are working. Early safe macOS tools can include notifications, active app, open app, and predefined Shortcuts, all permissioned and audited.

Later phases can add native Windows, semantic memory, richer agents, distributed workers, external integrations, vision, IoT, voice, and advanced native automation.
