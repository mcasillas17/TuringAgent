# TuringAgent v1.0 Design — Hybrid Runtime

**Historical design:** This record is not current implementation or status.
Use the [canonical roadmap](../../NORTH_STAR.md) for current truth.

## Status

This is the updated Copilot-side v1.0 spec after reading and integrating the relevant pieces of:

- `docs/superpowers/specs/2026-05-09-project-turing-v1-design-claude.md`
- `docs/superpowers/specs/2026-05-10-project-turing-v1-consolidation-report.md`
- `docs/superpowers/specs/2026-05-10-project-turing-v1-consolidation-report-claude.md`

It keeps the product-facing pieces from the Copilot design and adopts the Claude runtime/security pieces requested by the user:

- separate orchestrator and agent-runtime containers
- agent-runtime calls MCP servers directly
- orchestrator authorizes and audits through internal APIs, beacons, and tokens
- Docker network isolation
- per-agent MCP tokens
- internal orchestrator token
- `.env` API-key client auth for v1.0
- approval JWT shape for approval-required MCP calls

## Problem

TuringAgent should become a local-first personal AI orchestration platform, not a single chatbot or a client-specific app. The v1.0 release needs a strong foundation that proves the core pipeline while avoiding premature complexity across native apps, semantic memory, external OAuth integrations, vision, voice, and IoT.

The v1.0 architecture should prove that one backend brain can coordinate one first client face and several tool hands while preserving security boundaries that will still make sense when more agents and MCP servers are added.

## Goals

- Build a local-first, client-agnostic Node.js/TypeScript orchestrator.
- Run a separate Node.js/TypeScript `turing-agent-runtime-general` container for the primary agent.
- Use SQLite as the canonical local database for sessions, messages, runs, jobs, events, tool calls, approvals, audit logs, and settings.
- Route model calls to Ollama by default, with an optional OpenAI-compatible provider used only by explicit user selection.
- Stream model, run, tool, and approval events to clients over WebSocket.
- Use Flutter as the first production client while keeping it thin and protocol-driven.
- Prove MCP tooling with safe system tools and sandboxed file tools.
- Use `.env` API-key auth for v1.0 clients.
- Add layered runtime security: internal API token, per-agent MCP tokens, Docker network isolation, audit beacons, and approval JWTs for approval-required tools.
- Design for multiple agents later while implementing one primary `general_assistant` runtime first.

## Non-goals for v1.0

- Native macOS app or bridge implementation.
- Native Windows app or bridge implementation.
- Semantic/vector memory.
- LangGraph or graph-based multi-agent orchestration.
- Redis, BullMQ, NATS, or distributed queue infrastructure.
- Google/Microsoft OAuth integrations.
- Vision, voice, IoT, camera services, dog body-language analysis, or home automation.
- Arbitrary shell, AppleScript, PowerShell, screenshots, keyboard/mouse control, or unrestricted native automation.
- Destructive file operations such as delete and move.
- Multi-user accounts, password login, JWT client login, refresh tokens, or per-client API-key management.

## Scope note

Claude's smaller cut moved files MCP and approval UI to v1.1. This canonical hybrid spec intentionally keeps files MCP and active approval cards in v1.0 as Phase 5, followed by Phase 6 hardening. That makes v1.0 broader, but it gives the first release a real security loop for sandboxing, approval, approval JWT validation, and audit instead of leaving those paths unexercised.

## Architecture

TuringAgent v1.0 uses a split runtime spine:

```text
Flutter client
  -> REST command/query API
  -> turing-orchestrator
      -> SQLite canonical state
      -> WebSocket event broadcast
      -> job queue in SQLite
      -> internal API for agent-runtime
      -> tool policy, approval, audit
  -> turing-agent-runtime-general
      -> long-polls jobs from orchestrator
      -> fetches context from orchestrator
      -> streams model calls to Ollama or selected OpenAI-compatible provider
      -> calls MCP servers directly with per-agent tokens
      -> posts events, tool beacons, completion, and failures back to orchestrator
  -> internal MCP servers
```

The orchestrator owns client-facing APIs, canonical persistence, authorization, approval state, audit logs, job state, event persistence, and WebSocket streaming. The agent runtime owns execution of the primary `general_assistant` agent, including prompt construction, model calls, tool-call planning, and direct MCP invocation.

Clients do not own orchestration logic. MCP servers do not expose host ports. The agent runtime cannot reach MCP servers unless Docker networking and MCP bearer tokens allow it.

## Docker services

Docker Compose should run these v1.0 services:

- `turing-orchestrator`
  - Node.js/TypeScript
  - public REST and WebSocket on port `3000`
  - internal HTTP API on port `3001`, not published to host
  - owns SQLite database
  - owns client API-key auth, internal token auth, tool policy, approvals, audit, and event broadcast

- `turing-agent-runtime-general`
  - Node.js/TypeScript
  - no host-published port
  - authenticates to orchestrator internal API with `TURING_INTERNAL_TOKEN`
  - authenticates to MCP servers with per-agent MCP tokens
  - calls Ollama through `host.docker.internal:11434`

- `turing-mcp-system`
  - Go MCP server
  - internal-only Streamable HTTP endpoint
  - safe tools: `system.health`, `system.time`, `system.echo`, limited `system.info`
  - validates per-agent bearer token

- `turing-mcp-files`
  - Go MCP server
  - internal-only Streamable HTTP endpoint
  - sandboxed to approved mounted directories
  - validates per-agent bearer token
  - validates approval JWT for create/update

Ollama runs directly on the Mac Mini host and is reached from containers through:

```text
http://host.docker.internal:11434
```

Only the orchestrator publishes a host port.

Concrete Compose shape:

```yaml
services:
  turing-orchestrator:
    build: ../orchestrator
    env_file: ../.env
    ports:
      - "${ORCHESTRATOR_PUBLIC_PORT:-3000}:3000"
    expose:
      - "3001"
    volumes:
      - ../data:/app/data
    networks:
      - net-system
      - net-files

  turing-agent-runtime-general:
    build: ../agent-runtime
    env_file: ../.env
    depends_on:
      - turing-orchestrator
      - turing-mcp-system
      - turing-mcp-files
    extra_hosts:
      - "host.docker.internal:host-gateway"
    networks:
      - net-system
      - net-files

  turing-mcp-system:
    build: ../mcp-system
    env_file: ../.env
    expose:
      - "7100"
    networks:
      - net-system

  turing-mcp-files:
    build: ../mcp-files
    env_file: ../.env
    expose:
      - "7110"
    volumes:
      - ../sandbox:/sandbox
    networks:
      - net-files

networks:
  net-system:
  net-files:
```

## Docker network isolation

Docker networks should be scoped by capability. v1.0 can start with:

- `net-system`: orchestrator, agent runtime, `turing-mcp-system`
- `net-files`: orchestrator, agent runtime, `turing-mcp-files`

The orchestrator joins all MCP capability networks because it owns policy, approval, and health checks. The `general_assistant` runtime joins only networks for MCP servers it is allowed to use. Future agents should join only the networks for their allowed MCP servers.

This is a security boundary: if an agent runtime is compromised or buggy, it cannot call MCP services that are not reachable on its Docker networks, and it still needs valid per-agent MCP tokens for reachable services.

## Configuration and secrets

v1.0 uses `.env` secrets generated locally.

`turing-backend/.env` should contain:

```env
TURING_CLIENT_API_KEY=tk_<random>
TURING_INTERNAL_TOKEN=<random>
MCP_SYSTEM_TOKEN_GENERAL=<random>
MCP_FILES_TOKEN_GENERAL=<random>
TURING_APPROVAL_JWT_SECRET=<random>
ORCHESTRATOR_PUBLIC_PORT=3000
ORCHESTRATOR_INTERNAL_PORT=3001
DATABASE_PATH=/app/data/turing.db
FILES_SANDBOX_ROOT=/sandbox
OLLAMA_BASE_URL=http://host.docker.internal:11434
OLLAMA_MODEL=llama3.2
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_API_KEY=
OPENAI_MODEL=gpt-4o-mini
TURING_JOB_TIMEOUT_MS=300000
TURING_JOB_REAPER_INTERVAL_MS=60000
TURING_JOB_MAX_ATTEMPTS=3
TURING_DELTA_FLUSH_MS=50
LOG_LEVEL=info
LOG_PRETTY=0
```

`.env.example` should be committed with empty or sample values. Real `.env` files must be gitignored.

Bootstrap scripts:

- `scripts/init.sh`: idempotently creates `.env` if missing, fills only missing secrets if `.env` already exists, and prints `TURING_CLIENT_API_KEY` once for the user to paste/save in Flutter.
- `scripts/dev.sh`: starts Docker Compose with readable local-development logs, service names, and timestamps.
- `scripts/rotate-client-key.sh`: rotates only `TURING_CLIENT_API_KEY` and prints the new client key once.
- `scripts/reset.sh`: requires an explicit confirmation prompt, stops services, wipes local runtime/database state, and regenerates `.env`.

All secret reads go through a `SecretsBackend` interface:

```ts
export interface SecretsBackend {
  get(name: string): string | undefined;
  require(name: string): string;
}
```

v1.0 implements only `EnvSecretsBackend`, backed by `turing-backend/.env`. Do not read `process.env` throughout the app; load config once through this interface. Future secrets backends such as macOS Keychain, `keytar`, or Vault should be a one-file replacement behind the same interface.

Concrete `.gitignore` coverage must include:

```gitignore
.env
.env.*
*.env
!.env.example
!**/.env.example
.runtime/
turing-backend/.runtime/
data/turing.db*
turing-backend/data/turing.db*
```

## Orchestrator responsibilities

The orchestrator is the source of truth for:

- public REST API
- public WebSocket event stream
- client API-key auth
- internal API token auth
- SQLite migrations and canonical writes
- sessions
- messages
- jobs
- agent runs
- event append/replay/broadcast
- tool registry and allowlists
- tool policy decisions
- approval records
- approval JWT signing
- audit logs
- model/provider metadata exposed to clients
- recovery after reconnect/restart

The orchestrator should expose two listeners:

- `3000`: public REST and WebSocket; published to host; requires `TURING_CLIENT_API_KEY` except health/version.
- `3001`: internal API; Docker-network only; requires `TURING_INTERNAL_TOKEN`.

## Agent runtime responsibilities

`turing-agent-runtime-general` is responsible for:

- long-polling the orchestrator for jobs
- claiming work for `general_assistant`
- fetching recent session messages for context
- enforcing local run limits from agent config
- calling Ollama by default
- calling the optional OpenAI-compatible provider only when the job explicitly selects it
- deciding which allowed tools to call
- sending tool-call authorization/audit beacons before and after MCP calls
- calling MCP servers directly with per-agent bearer tokens
- including approval JWTs for approval-required MCP calls
- posting streamed model/tool/run events to the orchestrator
- marking runs completed or failed through the orchestrator internal API

The agent runtime must not write SQLite directly. It only communicates canonical state changes through the orchestrator.

The runtime's job loop should depend on agents through one `AgentExecutor` interface only:

```ts
export interface AgentExecutor {
  readonly agentId: string;
  execute(job: AgentJob, context: AgentExecutionContext): AsyncIterable<AgentExecutionUpdate>;
}
```

`generalAssistant.ts` implements this interface. The job loop imports the interface and registry, not agent internals. The orchestrator does not import agent implementations at all; its coupling to execution is the shared job DTO and internal API contract.

## Orchestrator modules

```text
orchestrator/src/
  server.ts
  api/                 public REST routes
  ws/                  WebSocket gateway
  internal/            internal agent-runtime API
  db/                  SQLite connection, migrations, repositories
  jobs/                job enqueue, claim, reaper
  sessions/            session/message services
  events/              append, replay, broadcast
  agents/              registry and metadata
  tools/               registry, allowlist, policy, approval JWT signing
  approvals/           approval state machine
  audit/               audit writer/query
  security/            API-key and internal-token middleware
  llm/                 provider metadata; no direct model execution in orchestrator hot path
  logging/             Pino structured logs
  config.ts
```

## Agent runtime modules

```text
agent-runtime/src/
  main.ts
  config.ts
  agents/generalAssistant.ts
  executor/            job loop and execution state
  llm/                 Ollama and OpenAI-compatible streaming clients
  mcp/                 Streamable HTTP MCP client
  orchestrator/        internal API client
  audit/               before/after beacon helper
  logging/
```

## Shared contracts and repository layout

The split runtime makes shared contracts important. Add a small TypeScript package for code shared between the orchestrator and agent runtime:

```text
turing-backend/shared-types/
  src/events.ts      event envelope and event type names
  src/jobs.ts        job payloads and run status types
  src/protocol.ts    public/internal API DTOs
  src/tools.ts       tool policy, beacon, and approval DTOs
```

The backend layout should be:

```text
turing-backend/
  orchestrator/       Node/TS public + internal API, SQLite owner
  agent-runtime/      Node/TS general assistant worker
  shared-types/       shared TS contracts
  mcp-system/         Go MCP server
  mcp-files/          Go MCP server
  infra/
    docker-compose.yml
  scripts/
    init.sh
    dev.sh
    reset.sh
    rotate-client-key.sh
  data/
  sandbox/
  .env.example
  .gitignore
```

The client layout is explicitly:

```text
turing-client/
  flutter_app/        first v1.0 client; rename existing turing_app here
  macos_app/          reserved for later native macOS client/bridge
  windows_app/        reserved for later native Windows client/bridge
  android_app/        reserved only if a native Android app is added later
```

The existing `turing-client/turing_app` directory should be renamed to `turing-client/flutter_app` during implementation planning. This is a decided repo-layout choice.

Phase 0 cleanup should remove old dead backend MCP stubs that no longer match the v1.0 cut-line, replace stub Dockerfiles with real service Dockerfiles, and move Compose under `turing-backend/infra/docker-compose.yml`.

## Public REST API

Use REST for public commands and queries. Use WebSocket for events.

Public base: `http://<host>:3000`.

Public API routes live under `/api`, except `GET /health` and `GET /version`.

Authentication:

- `GET /health` and `GET /version` are public.
- All other public REST routes require `Authorization: Bearer ${TURING_CLIENT_API_KEY}`.

Concrete public endpoints:

| Method | Path | Request | Response |
|---|---|---|---|
| `GET` | `/health` | none | `{ "ok": true }` |
| `GET` | `/version` | none | `{ "version": "1.0.0", "schemaVersion": "0001" }` |
| `GET` | `/api/config` | none | public config: ports, enabled providers, default model, feature flags |
| `POST` | `/api/sessions` | `{ "title"?: string }` | `201 { "sessionId": "sess_...", "createdAt": "..." }` |
| `GET` | `/api/sessions?limit=50&after=<sess_id>` | none | `{ "sessions": SessionSummary[] }` |
| `GET` | `/api/sessions/:sessionId` | none | `SessionDetail` |
| `GET` | `/api/sessions/:sessionId/messages?limit=50&before=<msg_id>` | none | `{ "messages": Message[] }` |
| `GET` | `/api/sessions/:sessionId/events?after=<sequence>&limit=500` | none | `{ "events": TuringEvent[], "latestSequence": number }` |
| `POST` | `/api/sessions/:sessionId/messages` | `SendMessageRequest` | `202 SendMessageResponse` |
| `GET` | `/api/agents` | none | `{ "agents": AgentMetadata[] }` |
| `GET` | `/api/tools` | none | `{ "tools": ToolMetadata[] }` |
| `POST` | `/api/approvals/:approvalId/approve` | `{ "comment"?: string }` | `{ "approvalId": "...", "status": "approved" }` |
| `POST` | `/api/approvals/:approvalId/deny` | `{ "reason"?: string }` | `{ "approvalId": "...", "status": "denied" }` |
| `GET` | `/api/audit?runId=<run_id>&limit=100` | none | `{ "entries": AuditEntry[] }` |
| `GET` | `/api/tool-calls?runId=<run_id>&limit=100` | none | `{ "toolCalls": ToolCall[] }` |

`SendMessageRequest`:

```ts
type SendMessageRequest = {
  content: string;
  contentType?: "text";
  agentId?: "general_assistant";
  modelProvider?: "ollama" | "openai_compatible";
  model?: string;
  idempotencyKey?: string;
};
```

`SendMessageResponse`:

```ts
type SendMessageResponse = {
  sessionId: string;
  userMessageId: string;
  assistantMessageId: string;
  runId: string;
  jobId: string;
  traceId: string;
  status: "queued";
};
```

Typed error response:

```ts
type ApiError = {
  error: {
    code: string;
    message: string;
    requestId: string;
    details?: unknown;
  };
};
```

`POST /api/sessions/:sessionId/messages` persists the user message, creates an agent run, enqueues a SQLite-backed job for `general_assistant`, emits an initial run event, and returns identifiers immediately. It does not wait for model completion.

## Public WebSocket API

Endpoint: `ws://<host>:3000/ws`

Authentication:

- The client provides the API key during connection, either as a bearer header when possible or as a query parameter/subprotocol where platform limitations require it.
- Invalid or missing API keys reject the connection.

WebSocket is for live events and replay, not primary mutations.

Client sends:

```ts
{ type: "hello", sessionId: string, lastSequence?: number }
{ type: "ping", ts: number }
```

Server sends:

```ts
{ type: "hello_ack", sessionId: string, latestSequence: number, replayedEvents: TuringEvent[] }
{ type: "event", event: TuringEvent }
{ type: "pong", ts: number }
{ type: "resync_required", reason: string }
{ type: "error", code: string, message: string, traceId?: string, runId?: string }
```

On reconnect, the client sends `lastSequence`. The orchestrator replays persisted events where `sequence > lastSequence`. If the gap is too large or unavailable, the server instructs the client to refetch messages and current run state over REST.

## Internal orchestrator API

Internal base: `http://turing-orchestrator:3001/internal`

Authentication:

- All internal endpoints require `Authorization: Bearer ${TURING_INTERNAL_TOKEN}`.
- The internal listener is not published to the host.

Minimum internal endpoints:

| Method | Endpoint | Request | Response |
|---|---|---|---|
| `GET` | `/jobs/next?agent=general_assistant&waitMs=30000` | none | `200 AgentJob` or `204` timeout |
| `POST` | `/runs/:runId/events` | `{ "event": TuringEventInput }` | `{ "eventId": "...", "sequence": number }` |
| `POST` | `/runs/:runId/audit/tool-call` | `ToolCallBeacon` | `ToolPolicyDecision` |
| `POST` | `/runs/:runId/approval-request` | `ApprovalRequest` | `202 { "approvalId": "appr_...", "status": "pending" }` |
| `GET` | `/approvals/:approvalId` | none | approval status, plus approval JWT only when approved |
| `POST` | `/runs/:runId/complete` | `{ "assistantMessageId": string, "content": string, "usage"?: LlmUsage }` | `{ "status": "completed" }` |
| `POST` | `/runs/:runId/fail` | `{ "code": string, "message": string, "retryable": boolean }` | `{ "status": "failed" }` |
| `GET` | `/sessions/:sessionId/messages?limit=50` | none | `{ "messages": Message[] }` |

`AgentJob`:

```ts
type AgentJob = {
  jobId: string;
  runId: string;
  sessionId: string;
  userMessageId: string;
  assistantMessageId: string;
  agentId: "general_assistant";
  traceId: string;
  modelProvider: "ollama" | "openai_compatible";
  model: string;
  payload: {
    userText: string;
    requestedTools?: string[];
  };
  attempt: number;
};
```

`ToolPolicyDecision`:

```ts
type ToolPolicyDecision =
  | { decision: "allow"; toolCallId: string }
  | { decision: "deny"; toolCallId: string; reason: string }
  | { decision: "approval_required"; toolCallId: string; approvalId: string };
```

Tool-call authorization should happen before MCP execution. For safe tools, the before beacon records the intended tool call and returns allow. For disabled tools, it returns deny and records audit. For approval-required tools, it creates or references an approval and returns block until the user approves.

## Event contract

Events are written to SQLite before broadcast.

```json
{
  "eventId": "evt_...",
  "sessionId": "sess_...",
  "runId": "run_...",
  "traceId": "trace_...",
  "sequence": 42,
  "type": "message.delta",
  "createdAt": "2026-05-10T00:00:00.000Z",
  "payload": {}
}
```

Concrete event types:

| Type | Payload |
|---|---|
| `message.started` | `{ "messageId": "msg_...", "role": "assistant" }` |
| `message.delta` | `{ "messageId": "msg_...", "delta": "text" }` |
| `message.completed` | `{ "messageId": "msg_...", "content": "full text" }` |
| `agent.run.queued` | `{ "runId": "run_...", "jobId": "job_...", "agentId": "general_assistant" }` |
| `agent.run.started` | `{ "runId": "run_...", "agentId": "general_assistant", "attempt": 1 }` |
| `agent.run.step` | `{ "stepId": "step_...", "kind": "model" | "tool" | "approval", "summary": "..." }` |
| `agent.run.completed` | `{ "runId": "run_...", "assistantMessageId": "msg_..." }` |
| `agent.run.failed` | `{ "runId": "run_...", "code": "model_unavailable", "message": "..." }` |
| `tool.call.started` | `{ "toolCallId": "call_...", "serverName": "system", "toolName": "system.time" }` |
| `tool.call.completed` | `{ "toolCallId": "call_...", "resultSummary": "..." }` |
| `tool.call.failed` | `{ "toolCallId": "call_...", "code": "mcp_5xx", "message": "..." }` |
| `tool.call.denied` | `{ "toolCallId": "call_...", "reason": "policy_denied" }` |
| `approval.requested` | `{ "approvalId": "appr_...", "toolName": "files.update", "argsSummary": "..." }` |
| `approval.approved` | `{ "approvalId": "appr_..." }` |
| `approval.denied` | `{ "approvalId": "appr_...", "reason"?: string }` |
| `error` | `{ "code": string, "message": string, "retryable": boolean }` |
| `system` | `{ "message": string }` |

Use both `eventId` and `sequence`: `eventId` is identity, `sequence` is replay order within a session.

Back-pressure behavior:

- The agent runtime should coalesce tiny model deltas before posting events when practical.
- The orchestrator should not buffer unbounded WebSocket writes.
- If a socket fails or is too slow, close it and rely on persisted replay.

## SQLite persistence

SQLite is canonical v1.0 memory. The orchestrator is the only writer.

Database file: `turing-backend/data/turing.db`, mounted only into `turing-orchestrator`.

Migration files live in `turing-backend/orchestrator/migrations/` and use names like `0001_initial.sql`.

Concrete v1.0 schema:

```sql
CREATE TABLE schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  title TEXT,
  status TEXT NOT NULL DEFAULT 'active'
    CHECK (status IN ('active','archived')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX idx_sessions_updated ON sessions(updated_at);

CREATE TABLE messages (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  run_id TEXT,
  role TEXT NOT NULL CHECK (role IN ('user','assistant','system','tool')),
  content TEXT NOT NULL,
  content_type TEXT NOT NULL DEFAULT 'text',
  sequence INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(session_id, sequence)
);
CREATE INDEX idx_messages_session_created ON messages(session_id, created_at);

CREATE TABLE agent_runs (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  user_message_id TEXT NOT NULL REFERENCES messages(id),
  assistant_message_id TEXT REFERENCES messages(id),
  agent_id TEXT NOT NULL,
  trace_id TEXT NOT NULL,
  status TEXT NOT NULL
    CHECK (status IN ('queued','running','waiting_approval','completed','failed','cancelled')),
  model_provider TEXT NOT NULL,
  model_name TEXT NOT NULL,
  error_code TEXT,
  error_message TEXT,
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT
);
CREATE INDEX idx_runs_session_created ON agent_runs(session_id, created_at);
CREATE INDEX idx_runs_status ON agent_runs(status, created_at);

CREATE TABLE agent_run_steps (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  step_index INTEGER NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('model','tool','approval','system')),
  status TEXT NOT NULL CHECK (status IN ('started','completed','failed','denied','expired')),
  summary TEXT,
  payload_json TEXT,
  created_at TEXT NOT NULL,
  completed_at TEXT,
  UNIQUE(run_id, step_index)
);

CREATE TABLE jobs (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  agent_id TEXT NOT NULL,
  status TEXT NOT NULL
    CHECK (status IN ('pending','in_progress','completed','failed','cancelled')),
  attempt INTEGER NOT NULL DEFAULT 1,
  payload_json TEXT NOT NULL,
  picked_up_at TEXT,
  finished_at TEXT,
  error_code TEXT,
  error_message TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_jobs_claim ON jobs(agent_id, status, created_at);
CREATE INDEX idx_jobs_reaper ON jobs(status, picked_up_at);

CREATE TABLE events (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  run_id TEXT REFERENCES agent_runs(id) ON DELETE CASCADE,
  trace_id TEXT NOT NULL,
  sequence INTEGER NOT NULL,
  type TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(session_id, sequence)
);
CREATE INDEX idx_events_replay ON events(session_id, sequence);
CREATE INDEX idx_events_run ON events(run_id, sequence);

CREATE TABLE tools (
  id TEXT PRIMARY KEY,
  server_name TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  policy TEXT NOT NULL CHECK (policy IN ('safe','approval_required','disabled')),
  schema_json TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  discovered_at TEXT NOT NULL,
  UNIQUE(server_name, tool_name)
);

CREATE TABLE tool_calls (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  step_id TEXT REFERENCES agent_run_steps(id),
  agent_id TEXT NOT NULL,
  server_name TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  args_json TEXT NOT NULL,
  args_hash TEXT NOT NULL,
  status TEXT NOT NULL
    CHECK (status IN ('requested','allowed','approval_required','completed','failed','denied')),
  result_summary TEXT,
  error_code TEXT,
  error_message TEXT,
  approval_id TEXT,
  duration_ms INTEGER,
  created_at TEXT NOT NULL,
  completed_at TEXT
);
CREATE INDEX idx_tool_calls_run ON tool_calls(run_id, created_at);

CREATE TABLE approvals (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  tool_call_id TEXT REFERENCES tool_calls(id),
  agent_id TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  args_json TEXT NOT NULL,
  args_hash TEXT NOT NULL,
  status TEXT NOT NULL
    CHECK (status IN ('pending','approved','denied','expired','consumed')),
  approval_jti TEXT,
  expires_at TEXT NOT NULL,
  decided_at TEXT,
  consumed_at TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_approvals_status ON approvals(status, expires_at);

CREATE TABLE audit_logs (
  id TEXT PRIMARY KEY,
  correlation_id TEXT,
  actor_type TEXT NOT NULL CHECK (actor_type IN ('client','runtime','mcp','system')),
  actor_id TEXT,
  action TEXT NOT NULL,
  target TEXT,
  payload_json TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX idx_audit_action ON audit_logs(action, created_at);
CREATE INDEX idx_audit_correlation ON audit_logs(correlation_id, created_at);
```

Do not add `users`, `refresh_tokens`, or `api_keys` tables in v1.0. Client auth uses one `.env` key.

SQLite settings:

- WAL mode
- `busy_timeout=5000`
- migrations run on startup and fail fast
- short transactions

IDs should use prefixed ULIDs:

- `sess_...`
- `msg_...`
- `run_...`
- `job_...`
- `evt_...`
- `tool_...`
- `call_...`
- `appr_...`
- `audit_...`
- `trace_...`

Timestamps are ISO 8601 UTC strings.

Tables explicitly not in v1.0:

- `users`
- `refresh_tokens`
- `api_keys`
- `attachments`
- per-agent SQLite databases

No retention policy is required for v1.0. Use time-ordered ULIDs and indexed timestamps so pruning can be added later if events grow too large.

Job lifecycle:

```text
pending -> in_progress -> completed
                    \-> failed
                    \-> cancelled
```

Agent run lifecycle:

```text
queued -> running -> waiting_approval -> completed
                              \-> failed
                              \-> cancelled
```

When a tool policy returns `approval_required`, the orchestrator must set `agent_runs.status = 'waiting_approval'` before broadcasting `approval.requested`. Approval moves the run back to `running` so the runtime can continue; denial or expiration ends the run with a visible failed/denied step.

A reaper runs every `TURING_JOB_REAPER_INTERVAL_MS` and reclaims jobs that remain `in_progress` past `TURING_JOB_TIMEOUT_MS`. It increments `attempt` and marks the job `pending` again until `TURING_JOB_MAX_ATTEMPTS`; after that, it marks the job and run `failed`.

## Agents and concurrency

v1.0 implements one agent: `general_assistant`.

The architecture must still support multiple concurrent runs and later multiple agent-runtime containers.

Each agent has:

- ID
- display name
- instructions
- allowed MCP servers
- allowed tools
- model policy
- concurrency limits
- tool-call limits
- timeouts

Concrete v1.0 agent config:

```ts
export function createGeneralAssistantAgent(config: { ollamaModel: string }) {
  return {
    id: "general_assistant",
    displayName: "General Assistant",
    instructionsFile: "agents/general_assistant.md",
    allowedMcps: ["system", "files"],
    allowedTools: [
      "system.health",
      "system.time",
      "system.echo",
      "system.info",
      "files.list",
      "files.search",
      "files.read",
      "files.create",
      "files.update"
    ],
    defaultModelProvider: "ollama",
    defaultModel: config.ollamaModel,
    modelProviders: ["ollama", "openai_compatible"],
    limits: {
      maxConcurrentRuns: 2,
      maxConcurrentRunsPerSession: 1,
      maxToolCallsPerRun: 8,
      modelTimeoutMs: 120000,
      toolTimeoutMs: 30000,
      approvalTimeoutMs: 60000
    }
  } as const;
}
```

Each run has:

- `runId`
- `sessionId`
- `traceId`
- `agentId`
- status
- model provider
- model name
- cancellation state
- persisted steps
- persisted events

Concurrency limits:

- max active runs globally
- max active runs per session
- max active runs per agent
- max tool calls per run
- model call timeout
- tool call timeout

## Model routing

The agent runtime performs model calls.

Ollama is the default provider. The optional OpenAI-compatible provider is enabled only when `OPENAI_API_KEY` is configured.

Cloud usage must be explicit. The client or session must select the OpenAI-compatible provider; the runtime must not auto-escalate from Ollama to cloud.

Both model providers must implement the same `LlmProvider` interface so adapters do not diverge:

```ts
export interface LlmProvider {
  readonly id: "ollama" | "openai_compatible";
  streamChat(request: LlmChatRequest): AsyncIterable<LlmStreamEvent>;
}

export interface LlmChatRequest {
  model: string;
  messages: Array<{ role: "system" | "user" | "assistant"; content: string }>;
  temperature?: number;
  maxTokens?: number;
  abortSignal?: AbortSignal;
}

export type LlmStreamEvent =
  | { type: "delta"; text: string }
  | { type: "completed"; finishReason?: string; usage?: LlmUsage }
  | { type: "error"; code: string; message: string };
```

The runtime converts provider stream events into the shared orchestrator event envelope. Provider-specific response formats must not leak into public REST, WebSocket, jobs, or persisted events.

## MCP tools

MCP transport is Streamable HTTP JSON-RPC.

Agent runtime calls MCP servers directly:

```text
turing-agent-runtime-general
  -> POST http://turing-mcp-system:7100/mcp
  -> POST http://turing-mcp-files:7110/mcp
```

Each MCP call includes:

- per-agent MCP bearer token
- JSON-RPC `tools/call`
- `traceId`
- `runId`
- `toolCallId`
- approval JWT in `_meta.approvalToken` for approval-required tools

The agent runtime should call `tools/list` at startup for each allowed MCP server and cache discovered schemas. The orchestrator keeps the policy registry and should not blindly trust discovery; discovery tells the runtime what exists, while orchestrator policy decides what may be called.

### `turing-mcp-system`

Concrete safe v1.0 tools:

| Tool | Args | Result |
|---|---|---|
| `system.health` | `{}` | `{ "ok": true, "service": "turing-mcp-system" }` |
| `system.time` | `{ "timezone"?: string }` | `{ "iso": "...", "unixMs": 1778390000000, "timezone": "..." }` |
| `system.echo` | `{ "text": string }` | `{ "text": string }` |
| `system.info` | `{}` | limited OS/arch/hostname/runtime info; no env vars, usernames, process lists, or secrets |

### `turing-mcp-files`

The files MCP server supports a configurable list of approved host directories.

It must:

- canonicalize paths
- reject path traversal
- reject symlink escapes
- enforce maximum file sizes
- keep operations inside approved directories
- reject delete and move
- validate per-agent MCP token
- validate approval JWT for create/update
- return structured JSON-RPC errors

Allowed v1.0 behavior:

| Tool | Args | Policy | Result |
|---|---|---|---|
| `files.list` | `{ "path": string, "recursive"?: boolean, "limit"?: number }` | safe | file metadata list |
| `files.search` | `{ "path": string, "query": string, "limit"?: number }` | safe | matching paths/snippets |
| `files.read` | `{ "path": string, "maxBytes"?: number }` | safe | `{ "path": "...", "content": "...", "truncated": boolean }` |
| `files.create` | `{ "path": string, "content": string }` | approval required | created file metadata |
| `files.update` | `{ "path": string, "content": string, "expectedHash"?: string }` | approval required | updated file metadata |
| `files.delete` | any | disabled | policy error |
| `files.move` | any | disabled | policy error |

File limits:

- `files.read` default max: 64 KiB; absolute max: 512 KiB.
- `files.search` default result limit: 50; absolute max: 200.
- All returned content should be UTF-8 text. Binary files return a typed unsupported-media error.
- Paths are resolved against approved roots and compared after canonicalization.

## Tool policy and audit beacons

The orchestrator owns policy. The agent runtime executes.

Before every MCP tool call, the agent runtime posts a before beacon to the orchestrator:

```json
{
  "phase": "before",
  "toolCallId": "call_...",
  "agentId": "general_assistant",
  "serverName": "files",
  "toolName": "files.read",
  "args": {},
  "runId": "run_...",
  "traceId": "trace_...",
  "createdAt": "2026-05-10T00:00:00.000Z"
}
```

The orchestrator:

1. verifies the agent is allowed to use the MCP server and tool
2. checks policy: `safe`, `approval_required`, or `disabled`
3. records audit
4. returns `allow`, `deny`, or `approval_required`

After the MCP call, the agent runtime posts an after beacon with status, result summary, duration, and error metadata. If the after beacon fails, the agent logs locally and continues; v1.0 audit beacons are best-effort after the before authorization has succeeded.

After beacon:

```json
{
  "phase": "after",
  "toolCallId": "call_...",
  "agentId": "general_assistant",
  "serverName": "files",
  "toolName": "files.read",
  "status": "completed",
  "resultSummary": "Read 12 lines from notes.txt",
  "durationMs": 42,
  "error": null,
  "runId": "run_...",
  "traceId": "trace_...",
  "createdAt": "2026-05-10T00:00:00.000Z"
}
```

Audit actions:

- `auth.failed`
- `internal.auth.failed`
- `mcp.auth.failed`
- `tool.call.before`
- `tool.call.after`
- `tool.call.policy_denied`
- `approval.created`
- `approval.approved`
- `approval.denied`
- `approval.expired`
- `approval.consumed`
- `security.bad_token`

Never log secrets, API keys, auth headers, full file contents, or unredacted sensitive payloads.

## Approval flow

Approval-required tools are blocked until the user approves.

Flow:

1. Agent runtime sends before beacon for approval-required tool.
2. Orchestrator creates an approval record.
3. Orchestrator emits `approval.requested` to WebSocket clients.
4. Flutter displays approval card.
5. User approves or denies through REST.
6. If denied, orchestrator records denial and emits `approval.denied`.
7. If approved, orchestrator signs an args-bound approval JWT.
8. Agent runtime polls approval status, receives JWT, and calls MCP with `_meta.approvalToken`.
9. MCP server validates JWT signature, expiry, tool name, agent subject, and args hash.
10. MCP executes only if token is valid.
11. Agent runtime posts after beacon.
12. Orchestrator marks approval consumed and emits completion/failure events.

Approval denial and expiration are normal run outcomes, not silent failures.

Approval JWT format:

- Algorithm: HS256.
- Secret: `TURING_APPROVAL_JWT_SECRET`.
- TTL: 60 seconds from issuance.
- Binding: `args_hash` is `sha256:` plus a SHA-256 digest of canonical JSON args.
- Single-use: `jti` maps to the approval record ID and becomes invalid once the approval is consumed, denied, expired, or superseded.
- Audience: the target MCP server.
- Subject: the agent ID that is allowed to use the token.

Claims:

```ts
{
  iss: "turing.orchestrator",
  sub: "general_assistant",
  aud: "mcp-files",
  jti: "appr_...",
  tool: "files.update",
  args_hash: "sha256:...",
  exp: 1778390000,
  iat: 1778389940
}
```

## Flutter client

Flutter is the first v1.0 face. It remains thin and protocol-driven.

Concrete Flutter module shape after renaming to `turing-client/flutter_app`:

```text
lib/
  main.dart
  app.dart
  networking/
    api_client.dart       REST commands/queries
    ws_client.dart        event stream and reconnect
    auth_storage.dart     secure API-key storage
  models/
    session.dart
    message.dart
    turing_event.dart
    approval.dart
    tool_call.dart
  features/
    settings/             backend URL + API-key entry
    sessions/             session list
    chat/                 streaming chat view
    approvals/            approval cards
  logic/                  existing state management, adapted to backend events
  ui/                     existing visual components
```

Responsibilities:

- backend URL setting
- API-key entry and secure storage
- session list
- chat UI
- send message command through REST
- WebSocket connection state
- streamed message rendering
- model provider selection per message/session
- approval cards
- reconnect and event replay

Flutter should not own agents, model routing, tool routing, approvals, persistence, memory, or security decisions.

Android physical devices should connect to the Mac Mini over LAN or Tailscale, not `localhost`.

## Error handling

Errors should be visible, typed, and durable.

- REST validation failures return typed error responses.
- Bad client API key returns 401 and records `auth.failed`.
- Bad internal token returns 401 and records `internal.auth.failed`.
- Bad MCP token returns 401 from MCP and is recorded as `mcp.auth.failed` or `security.bad_token`.
- Model failures end the run with clear status and emit `error`.
- Tool failures persist tool-call records and audit entries.
- Approval denial or expiration is represented as a run step.
- WebSocket disconnects do not cancel runs.
- Agent-runtime crash leaves job `in_progress`; reaper reclaims it.
- Orchestrator restart drops WebSockets; clients reconnect and replay.
- Explicit cancellation is a run state transition and event.

Failure-mode behavior:

| Failure | v1.0 behavior |
|---|---|
| Ollama unreachable | Mark run failed with `model_unavailable`; emit `error`; do not retry automatically. |
| OpenAI-compatible provider unavailable | Mark run failed unless the user explicitly retries with Ollama; do not silently fall back across privacy boundaries. |
| MCP server 5xx | Retry once for safe idempotent tools, then fail the tool call and continue or fail the run according to tool config. |
| MCP server 401 | Fail the run, record `security.bad_token`, and require operator token rotation. |
| Before audit beacon denied | Do not call MCP; emit `tool.call.denied`. |
| Before audit beacon network failure | Do not call MCP; retry the internal API briefly, then fail the tool call because authorization could not be confirmed. |
| After audit beacon network failure | Log locally and continue; before beacon is the authorization gate. |
| Agent runtime crashes mid-run | Job remains `in_progress`; reaper reclaims after timeout. |
| Orchestrator restarts | Clients reconnect and replay events; agent runtime retries internal polling. |
| Client disconnects | Run continues; persisted events allow replay. |

## Observability

Start with structured JSON logs. Log lines should include relevant correlation fields:

- `requestId`
- `sessionId`
- `runId`
- `traceId`
- `jobId`
- `toolCallId`
- `agentId`
- `eventType`

OpenTelemetry, Grafana, and Tempo are optional later additions, not required for v1.0.

## Testing strategy

Backend orchestrator tests should cover:

- `.env` config validation
- API-key REST auth acceptance/rejection
- API-key WebSocket auth acceptance/rejection
- internal token acceptance/rejection
- SQLite migrations
- session/message persistence
- job enqueue/claim/reaper behavior
- event sequencing and replay
- run state transitions
- tool policy decisions
- before/after audit beacons
- approval required, approved, denied, expired, and consumed paths
- approval JWT signing and validation inputs
- audit query endpoints

Agent runtime tests should cover:

- internal job polling
- prompt context fetch
- mocked Ollama streaming
- mocked OpenAI-compatible streaming
- explicit provider selection
- safe MCP tool call path
- approval-required tool call path
- before/after beacon behavior
- MCP 401/5xx handling
- run completion/failure posting

Go MCP tests should cover:

- system tools list/call
- files path canonicalization
- path traversal rejection
- symlink escape rejection
- max file size enforcement
- per-agent token rejection
- approval JWT required for create/update
- delete/move disabled

Flutter tests should cover:

- protocol model parsing
- API-key storage/settings
- REST send-message command
- WebSocket event application
- streaming bubble updates
- reconnect/refetch behavior
- approval card approve/deny behavior
- model selection UI state

## v1.0 phases

### Phase 0: Scaffolding and secrets

- backend packages for orchestrator, agent runtime, system MCP, files MCP
- Docker Compose with public/internal ports and networks
- `.env.example`
- backend `.gitignore` for `.env`, runtime state, and SQLite files
- `scripts/init.sh`, `scripts/dev.sh`, `scripts/reset.sh`, `scripts/rotate-client-key.sh`
- SQLite migration skeleton
- rename `turing-client/turing_app` to `turing-client/flutter_app`
- create reserved client app directories for future native surfaces

Demoable goal: `scripts/init.sh` creates `.env`, and Docker Compose validates/builds all service skeletons.

### Phase 1: Orchestrator REST, auth, SQLite, jobs

- health/version/config
- API-key middleware
- internal-token middleware
- migrations
- sessions/messages
- job enqueue/claim
- audit base

Demoable goal: bad API keys return 401, a valid key can create a session/message, and the internal API can claim a pending job with the internal token.

### Phase 2: WebSocket events and replay

- authenticated WebSocket
- event append before broadcast
- replay by `lastSequence`
- slow/broken socket handling

Demoable goal: Flutter or a WebSocket test client connects, receives `hello_ack`, disconnects, reconnects with `lastSequence`, and receives missed events.

### Phase 3: Agent runtime and model streaming

- runtime long-polls jobs
- fetches context
- streams Ollama
- optional OpenAI-compatible adapter
- posts deltas and completion through internal API

Demoable goal: sending a chat message creates a job, the runtime streams a local Ollama response, and the client sees live deltas.

### Phase 4: System MCP

- Go system MCP
- per-agent MCP token
- direct agent-runtime MCP call
- tool before/after beacons
- tool events to client

Demoable goal: asking for the time triggers a direct `system.time` MCP call, records before/after audit rows, and streams tool events plus a final answer.

### Phase 5: Files MCP and approvals

- Go files MCP
- approved directory sandbox
- safe read/list/search
- approval-required create/update
- approval cards in Flutter
- approval JWT validation

Demoable goal: a files create/update request blocks for approval, Flutter displays an approval card, approval issues a JWT, the MCP server validates it, and the result is audited.

### Phase 6: End-to-end hardening

- Docker smoke
- Flutter smoke
- reconnect/replay smoke
- audit/tool-call inspection
- documentation updates

Demoable goal: a clean local checkout can run the documented smoke path and recover chat history/events after reconnect.

## Open considerations after v1.0

- **Secrets backend:** `.env` is a v1.0 simplification. Revisit macOS Keychain, `keytar`, or Vault before OAuth tokens, multi-user support, or broader deployment.
- **Approval latency:** v1.0 approval JWTs can use a short TTL. If real approval decisions often take longer, increase TTL or add a held approval-token flow.
- **Event growth:** event retention is unnecessary for v1.0. Add pruning when event volume grows due to sensors, webhooks, or more agents.
- **Agent runtime scaling:** v1.0 has one runtime. When there are multiple agents, decide between one runtime polling multiple agent IDs or one container per agent.
- **OpenTelemetry:** structured logs are enough for v1.0. Add OTel when multiple runtimes/tool servers make trace correlation painful.
- **Per-agent state:** do not add per-agent SQLite databases until an agent needs durable private state beyond orchestrator-owned runs/events.

## Roadmap after v1.0

v1.1 should introduce the native macOS face and macOS bridge concept after the backend, Flutter chat, streaming, SQLite persistence, auth, and MCP tool pipeline are working. Early safe macOS tools can include notifications, active app, open app, and predefined Shortcuts, all permissioned and audited.

Later phases can add semantic memory, Google/Microsoft MCPs, richer multi-agent routing, native Windows, vision, IoT, voice, and advanced native automation.
