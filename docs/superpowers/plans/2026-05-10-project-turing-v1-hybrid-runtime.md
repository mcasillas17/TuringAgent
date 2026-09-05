# TuringAgent v1.0 Hybrid Runtime Implementation Plan

**Historical plan:** This record is not current implementation or status.
Use the [canonical roadmap](../../NORTH_STAR.md) for current truth.

**Goal:** Build the v1.0 local-first TuringAgent vertical slice: Flutter client -> Node/TypeScript orchestrator -> SQLite -> separate agent runtime -> Ollama/MCP -> durable WebSocket event stream.

**Architecture:** The orchestrator owns public REST/WebSocket APIs, SQLite, policy, approvals, jobs, and audit. A separate `turing-agent-runtime-general` container claims jobs over the internal API, streams model/tool events back to the orchestrator, and calls MCP servers directly with per-agent tokens. Go MCP servers are internal-only and protected by Docker networks, bearer tokens, and approval JWTs for sensitive file writes.

**Tech Stack:** Node.js 20, TypeScript, Fastify, `ws`, SQLite, Vitest, Pino, Docker Compose, Go 1.23, Flutter/Dart, Ollama, Streamable HTTP JSON-RPC for MCP.

---

## Source of truth

- Canonical spec: `docs/superpowers/specs/2026-05-09-project-turing-v1-design-copilot.md`
- Consolidation report: `docs/superpowers/specs/2026-05-10-project-turing-v1-consolidation-report.md`
- Historical/stale implementation plan: `docs/superpowers/plans/2026-05-09-project-turing-v1.md` must not be used for execution.

## Scope check

The spec spans backend infrastructure, runtime, two MCP servers, and Flutter client work. This plan keeps those subsystems in one sequence because each phase produces a testable vertical slice and all tasks converge on one v1.0 product pipeline. If execution becomes too large for one branch, split after Task 8: backend/runtime/MCP first, Flutter second.

## Target file structure

```text
turing-backend/
  package.json
  tsconfig.base.json
  .env.example
  .gitignore
  data/.gitkeep
  sandbox/.gitkeep
  infra/docker-compose.yml
  scripts/init.sh
  scripts/dev.sh
  scripts/reset.sh
  scripts/rotate-client-key.sh
  shared-types/
    package.json
    tsconfig.json
    src/events.ts
    src/jobs.ts
    src/protocol.ts
    src/tools.ts
    src/llm.ts
    src/index.ts
  orchestrator/
    package.json
    tsconfig.json
    Dockerfile
    migrations/0001_initial.sql
    src/server.ts
    src/config.ts
    src/api/routes.ts
    src/ws/gateway.ts
    src/internal/routes.ts
    src/db/connection.ts
    src/db/migrations.ts
    src/db/repositories.ts
    src/jobs/service.ts
    src/sessions/service.ts
    src/events/service.ts
    src/tools/policy.ts
    src/approvals/service.ts
    src/audit/service.ts
    src/security/auth.ts
    src/logging/logger.ts
    tests/*.test.ts
  agent-runtime/
    package.json
    tsconfig.json
    Dockerfile
    src/main.ts
    src/config.ts
    src/agents/generalAssistant.ts
    src/executor/types.ts
    src/executor/jobLoop.ts
    src/llm/provider.ts
    src/llm/ollama.ts
    src/llm/openaiCompatible.ts
    src/mcp/client.ts
    src/orchestrator/client.ts
    src/audit/beacons.ts
    src/logging/logger.ts
    tests/*.test.ts
  mcp-system/
    Dockerfile
    go.mod
    cmd/server/main.go
    internal/auth/auth.go
    internal/jsonrpc/jsonrpc.go
    internal/tools/system.go
    internal/tools/system_test.go
  mcp-files/
    Dockerfile
    go.mod
    cmd/server/main.go
    internal/auth/auth.go
    internal/approval/jwt.go
    internal/jsonrpc/jsonrpc.go
    internal/tools/files.go
    internal/tools/files_test.go

turing-client/
  flutter_app/                 # renamed from turing_app
    lib/networking/*.dart
    lib/models/*.dart
    lib/features/settings/*.dart
    lib/features/sessions/*.dart
    lib/features/chat/*.dart
    lib/features/approvals/*.dart
  macos_app/.gitkeep
  windows_app/.gitkeep
  android_app/.gitkeep
```

## Commit strategy

Commit after every task that passes its verification command. Use the commit messages shown in each task.

---

### Task 1: Repo layout, workspace, scripts, and Docker skeleton

**Files:**
- Rename: `turing-client/turing_app/` -> `turing-client/flutter_app/`
- Create: `turing-client/macos_app/.gitkeep`
- Create: `turing-client/windows_app/.gitkeep`
- Create: `turing-client/android_app/.gitkeep`
- Create: `turing-backend/package.json`
- Create: `turing-backend/tsconfig.base.json`
- Create: `turing-backend/.env.example`
- Create: `turing-backend/.gitignore`
- Create: `turing-backend/data/.gitkeep`
- Create: `turing-backend/sandbox/.gitkeep`
- Create: `turing-backend/infra/docker-compose.yml`
- Create: `turing-backend/scripts/init.sh`
- Create: `turing-backend/scripts/dev.sh`
- Create: `turing-backend/scripts/reset.sh`
- Create: `turing-backend/scripts/rotate-client-key.sh`
- Remove: `turing-backend/docker-compose.yml`
- Remove: `turing-backend/services/google-mcp/`
- Remove: `turing-backend/services/microsoft-mcp/`
- Modify: `.gitignore`
- Modify: `turing-client/flutter_app/pubspec.yaml`

- [ ] **Step 1: Move the Flutter app and reserve future client surfaces**

Run:

```bash
mkdir -p turing-client/macos_app turing-client/windows_app turing-client/android_app
git mv turing-client/turing_app turing-client/flutter_app
touch turing-client/macos_app/.gitkeep turing-client/windows_app/.gitkeep turing-client/android_app/.gitkeep
```

Expected: `git status --short` shows `R  turing-client/turing_app/... -> turing-client/flutter_app/...` entries and three new `.gitkeep` files.

- [ ] **Step 2: Update Flutter package name**

Edit `turing-client/flutter_app/pubspec.yaml`:

```yaml
name: turing_flutter_app
description: "TuringAgent Flutter client."
publish_to: 'none'
version: 1.0.0+1
```

Expected: only the package metadata changes; existing dependencies remain.

- [ ] **Step 3: Create backend workspace root**

Create `turing-backend/package.json`:

```json
{
  "name": "project-turing-backend",
  "private": true,
  "workspaces": [
    "shared-types",
    "orchestrator",
    "agent-runtime"
  ],
  "scripts": {
    "build": "npm run build -ws",
    "test": "npm run test -ws",
    "typecheck": "npm run typecheck -ws",
    "lint": "npm run typecheck -ws"
  },
  "devDependencies": {
    "@types/node": "^20.17.10",
    "typescript": "^5.7.2",
    "vitest": "^2.1.8"
  }
}
```

Create `turing-backend/tsconfig.base.json`:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "NodeNext",
    "moduleResolution": "NodeNext",
    "strict": true,
    "esModuleInterop": true,
    "forceConsistentCasingInFileNames": true,
    "skipLibCheck": true,
    "resolveJsonModule": true,
    "outDir": "dist"
  }
}
```

- [ ] **Step 4: Add concrete environment template and ignore rules**

Create `turing-backend/.env.example`:

```env
TURING_CLIENT_API_KEY=
TURING_INTERNAL_TOKEN=
MCP_SYSTEM_TOKEN_GENERAL=
MCP_FILES_TOKEN_GENERAL=
TURING_APPROVAL_JWT_SECRET=
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

Create `turing-backend/.gitignore`:

```gitignore
.env
.env.*
*.env
!.env.example
!**/.env.example
.runtime/
data/turing.db*
node_modules/
dist/
coverage/
*.log
```

Append missing project-level entries to `.gitignore` if absent:

```gitignore
*.env
!**/.env.example
.runtime/
turing-backend/.runtime/
data/turing.db*
turing-backend/data/turing.db*
```

- [ ] **Step 5: Add backend scripts**

Create `turing-backend/scripts/init.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

generate_secret() {
  openssl rand -hex 32
}

generate_client_key() {
  printf 'tk_%s\n' "$(openssl rand -hex 32)"
}

if [[ ! -f .env ]]; then
  cp .env.example .env
fi

ensure_var() {
  local name="$1"
  local value="$2"
  if ! grep -q "^${name}=" .env || grep -q "^${name}=$" .env; then
    if grep -q "^${name}=" .env; then
      sed -i.bak "s|^${name}=.*|${name}=${value}|" .env
    else
      printf '%s=%s\n' "$name" "$value" >> .env
    fi
  fi
}

ensure_var TURING_CLIENT_API_KEY "$(generate_client_key)"
ensure_var TURING_INTERNAL_TOKEN "$(generate_secret)"
ensure_var MCP_SYSTEM_TOKEN_GENERAL "$(generate_secret)"
ensure_var MCP_FILES_TOKEN_GENERAL "$(generate_secret)"
ensure_var TURING_APPROVAL_JWT_SECRET "$(generate_secret)"
rm -f .env.bak
mkdir -p data sandbox

client_key="$(grep '^TURING_CLIENT_API_KEY=' .env | cut -d= -f2-)"
printf 'TuringAgent backend initialized.\n'
printf 'Flutter client API key: %s\n' "$client_key"
```

Create `turing-backend/scripts/dev.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
LOG_PRETTY=1 docker compose -f infra/docker-compose.yml up --build
```

Create `turing-backend/scripts/reset.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
read -r -p "Delete TuringAgent local data and regenerate .env? Type RESET: " answer
if [[ "$answer" != "RESET" ]]; then
  echo "Reset cancelled."
  exit 1
fi
docker compose -f infra/docker-compose.yml down --remove-orphans || true
rm -rf data .runtime .env
mkdir -p data sandbox
./scripts/init.sh
```

Create `turing-backend/scripts/rotate-client-key.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [[ ! -f .env ]]; then
  echo ".env missing; run scripts/init.sh first" >&2
  exit 1
fi
new_key="tk_$(openssl rand -hex 32)"
sed -i.bak "s|^TURING_CLIENT_API_KEY=.*|TURING_CLIENT_API_KEY=${new_key}|" .env
rm -f .env.bak
printf 'New Flutter client API key: %s\n' "$new_key"
```

Run:

```bash
chmod +x turing-backend/scripts/*.sh
```

- [ ] **Step 6: Add Docker Compose skeleton**

Create `turing-backend/infra/docker-compose.yml`:

```yaml
services:
  turing-orchestrator:
    build:
      context: ..
      dockerfile: orchestrator/Dockerfile
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
    build:
      context: ..
      dockerfile: agent-runtime/Dockerfile
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

Create data directories:

```bash
mkdir -p turing-backend/data turing-backend/sandbox
touch turing-backend/data/.gitkeep turing-backend/sandbox/.gitkeep
```

- [ ] **Step 7: Remove stale backend stubs**

Run:

```bash
git rm -r turing-backend/services/google-mcp turing-backend/services/microsoft-mcp turing-backend/docker-compose.yml
```

Expected: old Google/Microsoft stub Dockerfiles and old Compose are removed.

- [ ] **Step 8: Verify scaffolding**

Run:

```bash
cd turing-backend
./scripts/init.sh
test -f .env
test -d data
test -d sandbox
docker compose -f infra/docker-compose.yml config --quiet
```

Expected: `.env` is created, the API key prints once, and Compose config exits with code 0. If Docker is unavailable, record that as an environment blocker and still verify `./scripts/init.sh`.

- [ ] **Step 9: Commit**

```bash
git add .gitignore turing-backend turing-client
git commit -m "chore: scaffold Turing hybrid runtime workspace"
```

---

### Task 2: Shared TypeScript contracts

**Files:**
- Create: `turing-backend/shared-types/package.json`
- Create: `turing-backend/shared-types/tsconfig.json`
- Create: `turing-backend/shared-types/src/events.ts`
- Create: `turing-backend/shared-types/src/jobs.ts`
- Create: `turing-backend/shared-types/src/protocol.ts`
- Create: `turing-backend/shared-types/src/tools.ts`
- Create: `turing-backend/shared-types/src/llm.ts`
- Create: `turing-backend/shared-types/src/index.ts`
- Test: `turing-backend/shared-types/src/events.test.ts`

- [ ] **Step 1: Create package metadata**

Create `turing-backend/shared-types/package.json`:

```json
{
  "name": "@turing/shared-types",
  "version": "1.0.0",
  "private": true,
  "type": "module",
  "main": "dist/index.js",
  "types": "dist/index.d.ts",
  "scripts": {
    "build": "tsc -p tsconfig.json",
    "typecheck": "tsc -p tsconfig.json --noEmit",
    "test": "vitest run"
  },
  "devDependencies": {
    "vitest": "^2.1.8"
  }
}
```

Create `turing-backend/shared-types/tsconfig.json`:

```json
{
  "extends": "../tsconfig.base.json",
  "compilerOptions": {
    "rootDir": "src",
    "outDir": "dist",
    "declaration": true,
    "declarationMap": true
  },
  "include": ["src/**/*.ts"]
}
```

- [ ] **Step 2: Write event contract test**

Create `turing-backend/shared-types/src/events.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { isTuringEventType, type TuringEvent } from "./events.js";

describe("events contract", () => {
  it("recognizes supported event types", () => {
    expect(isTuringEventType("message.delta")).toBe(true);
    expect(isTuringEventType("tool.call.denied")).toBe(true);
    expect(isTuringEventType("not.real")).toBe(false);
  });

  it("allows a concrete event envelope", () => {
    const event: TuringEvent = {
      eventId: "evt_01JTEST",
      sessionId: "sess_01JTEST",
      runId: "run_01JTEST",
      traceId: "trace_01JTEST",
      sequence: 1,
      type: "message.delta",
      createdAt: "2026-05-10T00:00:00.000Z",
      payload: { messageId: "msg_01JTEST", delta: "hello" }
    };

    expect(event.sequence).toBe(1);
  });
});
```

- [ ] **Step 3: Run test to verify it fails**

Run:

```bash
cd turing-backend
npm install
npm test -w @turing/shared-types -- events.test.ts
```

Expected: FAIL because `src/events.ts` does not exist.

- [ ] **Step 4: Implement shared event, job, protocol, tool, and LLM types**

Create `turing-backend/shared-types/src/events.ts`:

```ts
export const TURING_EVENT_TYPES = [
  "message.started",
  "message.delta",
  "message.completed",
  "agent.run.queued",
  "agent.run.started",
  "agent.run.step",
  "agent.run.completed",
  "agent.run.failed",
  "tool.call.started",
  "tool.call.completed",
  "tool.call.failed",
  "tool.call.denied",
  "approval.requested",
  "approval.approved",
  "approval.denied",
  "approval.expired",
  "approval.consumed",
  "error",
  "system"
] as const;

export type TuringEventType = (typeof TURING_EVENT_TYPES)[number];

export type TuringEvent = {
  eventId: string;
  sessionId: string;
  runId?: string;
  traceId: string;
  sequence: number;
  type: TuringEventType;
  createdAt: string;
  payload: Record<string, unknown>;
};

export type TuringEventInput = Omit<TuringEvent, "eventId" | "sequence" | "createdAt"> & {
  createdAt?: string;
};

export function isTuringEventType(value: string): value is TuringEventType {
  return (TURING_EVENT_TYPES as readonly string[]).includes(value);
}
```

Create `turing-backend/shared-types/src/jobs.ts`:

```ts
export type AgentId = "general_assistant";
export type ModelProviderId = "ollama" | "openai_compatible";

export type AgentJob = {
  jobId: string;
  runId: string;
  sessionId: string;
  userMessageId: string;
  assistantMessageId: string;
  agentId: AgentId;
  traceId: string;
  modelProvider: ModelProviderId;
  model: string;
  payload: {
    userText: string;
    requestedTools?: string[];
  };
  attempt: number;
};

export type AgentExecutionUpdate =
  | { type: "event"; event: import("./events.js").TuringEventInput }
  | { type: "complete"; content: string; usage?: import("./llm.js").LlmUsage }
  | { type: "fail"; code: string; message: string; retryable: boolean };
```

Create `turing-backend/shared-types/src/protocol.ts`:

```ts
import type { AgentId, ModelProviderId } from "./jobs.js";

export type SendMessageRequest = {
  content: string;
  contentType?: "text";
  agentId?: AgentId;
  modelProvider?: ModelProviderId;
  model?: string;
  idempotencyKey?: string;
};

export type SendMessageResponse = {
  sessionId: string;
  userMessageId: string;
  assistantMessageId: string;
  runId: string;
  jobId: string;
  traceId: string;
  status: "queued";
};

export type ApiError = {
  error: {
    code: string;
    message: string;
    requestId: string;
    details?: unknown;
  };
};
```

Create `turing-backend/shared-types/src/tools.ts`:

```ts
export type ToolPolicy = "safe" | "approval_required" | "disabled";

export type ToolCallBeacon = {
  phase: "before" | "after";
  toolCallId: string;
  agentId: "general_assistant";
  serverName: "system" | "files";
  toolName: string;
  args?: Record<string, unknown>;
  status?: "completed" | "failed" | "denied";
  resultSummary?: string;
  durationMs?: number;
  error?: { code: string; message: string } | null;
  runId: string;
  traceId: string;
  createdAt?: string;
};

export type ToolPolicyDecision =
  | { decision: "allow"; toolCallId: string }
  | { decision: "deny"; toolCallId: string; reason: string }
  | { decision: "approval_required"; toolCallId: string; approvalId: string };
```

Create `turing-backend/shared-types/src/llm.ts`:

```ts
export type LlmUsage = {
  promptTokens?: number;
  completionTokens?: number;
  totalTokens?: number;
};

export type LlmChatRequest = {
  model: string;
  messages: Array<{ role: "system" | "user" | "assistant"; content: string }>;
  temperature?: number;
  maxTokens?: number;
  abortSignal?: AbortSignal;
};

export type LlmStreamEvent =
  | { type: "delta"; text: string }
  | { type: "completed"; finishReason?: string; usage?: LlmUsage }
  | { type: "error"; code: string; message: string };
```

Create `turing-backend/shared-types/src/index.ts`:

```ts
export * from "./events.js";
export * from "./jobs.js";
export * from "./protocol.js";
export * from "./tools.js";
export * from "./llm.js";
```

- [ ] **Step 5: Run shared type tests**

Run:

```bash
cd turing-backend
npm test -w @turing/shared-types
npm run build -w @turing/shared-types
```

Expected: PASS and `shared-types/dist` is generated locally.

- [ ] **Step 6: Commit**

```bash
git add turing-backend/package.json turing-backend/package-lock.json turing-backend/tsconfig.base.json turing-backend/shared-types
git commit -m "feat: add shared Turing protocol contracts"
```

---

### Task 3: Orchestrator configuration, server bootstrap, logging, and auth

**Files:**
- Create: `turing-backend/orchestrator/package.json`
- Create: `turing-backend/orchestrator/tsconfig.json`
- Create: `turing-backend/orchestrator/Dockerfile`
- Create: `turing-backend/orchestrator/src/config.ts`
- Create: `turing-backend/orchestrator/src/logging/logger.ts`
- Create: `turing-backend/orchestrator/src/security/auth.ts`
- Create: `turing-backend/orchestrator/src/server.ts`
- Test: `turing-backend/orchestrator/tests/config.test.ts`
- Test: `turing-backend/orchestrator/tests/auth.test.ts`

- [ ] **Step 1: Create package files**

Create `turing-backend/orchestrator/package.json`:

```json
{
  "name": "@turing/orchestrator",
  "version": "1.0.0",
  "private": true,
  "type": "module",
  "scripts": {
    "build": "tsc -p tsconfig.json",
    "typecheck": "tsc -p tsconfig.json --noEmit",
    "test": "vitest run",
    "start": "node dist/server.js",
    "dev": "tsx src/server.ts"
  },
  "dependencies": {
    "@fastify/websocket": "^11.0.1",
    "@turing/shared-types": "1.0.0",
    "better-sqlite3": "^11.7.0",
    "dotenv": "^16.4.7",
    "fastify": "^5.2.1",
    "jose": "^5.9.6",
    "pino": "^9.5.0",
    "ulid": "^2.3.0",
    "ws": "^8.18.0"
  },
  "devDependencies": {
    "@types/better-sqlite3": "^7.6.12",
    "@types/ws": "^8.5.13",
    "tsx": "^4.19.2",
    "vitest": "^2.1.8"
  }
}
```

Create `turing-backend/orchestrator/tsconfig.json`:

```json
{
  "extends": "../tsconfig.base.json",
  "compilerOptions": {
    "rootDir": "src",
    "outDir": "dist"
  },
  "include": ["src/**/*.ts", "tests/**/*.ts"]
}
```

- [ ] **Step 2: Write failing config and auth tests**

Create `turing-backend/orchestrator/tests/config.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { loadConfigFromEnv } from "../src/config.js";

describe("loadConfigFromEnv", () => {
  it("requires client, internal, mcp, and approval secrets", () => {
    expect(() => loadConfigFromEnv({})).toThrow(/TURING_CLIENT_API_KEY/);
  });

  it("parses ports and defaults", () => {
    const config = loadConfigFromEnv({
      TURING_CLIENT_API_KEY: "tk_test",
      TURING_INTERNAL_TOKEN: "internal",
      MCP_SYSTEM_TOKEN_GENERAL: "system",
      MCP_FILES_TOKEN_GENERAL: "files",
      TURING_APPROVAL_JWT_SECRET: "approval",
      DATABASE_PATH: ":memory:"
    });

    expect(config.publicPort).toBe(3000);
    expect(config.internalPort).toBe(3001);
    expect(config.ollamaModel).toBe("llama3.2");
  });
});
```

Create `turing-backend/orchestrator/tests/auth.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { bearerTokenFromHeader, tokenMatches } from "../src/security/auth.js";

describe("auth helpers", () => {
  it("extracts bearer tokens", () => {
    expect(bearerTokenFromHeader("Bearer tk_test")).toBe("tk_test");
    expect(bearerTokenFromHeader("Basic nope")).toBeUndefined();
  });

  it("compares tokens without accepting empty values", () => {
    expect(tokenMatches("tk_test", "tk_test")).toBe(true);
    expect(tokenMatches("", "")).toBe(false);
    expect(tokenMatches("wrong", "tk_test")).toBe(false);
  });
});
```

- [ ] **Step 3: Run tests to verify they fail**

Run:

```bash
cd turing-backend
npm test -w @turing/orchestrator -- config.test.ts auth.test.ts
```

Expected: FAIL because `config.ts` and `auth.ts` do not exist.

- [ ] **Step 4: Implement config and auth helpers**

Create `turing-backend/orchestrator/src/config.ts`:

```ts
import { config as loadDotenv } from "dotenv";

export type SecretsBackend = {
  get(name: string): string | undefined;
  require(name: string): string;
};

export class EnvSecretsBackend implements SecretsBackend {
  constructor(private readonly env: NodeJS.ProcessEnv = process.env) {}

  get(name: string): string | undefined {
    const value = this.env[name];
    return value && value.length > 0 ? value : undefined;
  }

  require(name: string): string {
    const value = this.get(name);
    if (!value) throw new Error(`Missing required env var ${name}`);
    return value;
  }
}

export type OrchestratorConfig = {
  clientApiKey: string;
  internalToken: string;
  mcpSystemTokenGeneral: string;
  mcpFilesTokenGeneral: string;
  approvalJwtSecret: string;
  publicPort: number;
  internalPort: number;
  databasePath: string;
  ollamaBaseUrl: string;
  ollamaModel: string;
  openaiBaseUrl: string;
  openaiApiKey?: string;
  openaiModel: string;
  jobTimeoutMs: number;
  jobReaperIntervalMs: number;
  jobMaxAttempts: number;
  maxConcurrentRunsGeneral: number;
  maxToolCallsPerRun: number;
  modelTimeoutMs: number;
  toolTimeoutMs: number;
  logLevel: string;
};

function intFromEnv(env: NodeJS.ProcessEnv, name: string, fallback: number): number {
  const raw = env[name];
  if (!raw) return fallback;
  const parsed = Number.parseInt(raw, 10);
  if (!Number.isFinite(parsed)) throw new Error(`Invalid integer env var ${name}`);
  return parsed;
}

export function loadConfigFromEnv(env: NodeJS.ProcessEnv = process.env): OrchestratorConfig {
  const secrets = new EnvSecretsBackend(env);
  return {
    clientApiKey: secrets.require("TURING_CLIENT_API_KEY"),
    internalToken: secrets.require("TURING_INTERNAL_TOKEN"),
    mcpSystemTokenGeneral: secrets.require("MCP_SYSTEM_TOKEN_GENERAL"),
    mcpFilesTokenGeneral: secrets.require("MCP_FILES_TOKEN_GENERAL"),
    approvalJwtSecret: secrets.require("TURING_APPROVAL_JWT_SECRET"),
    publicPort: intFromEnv(env, "ORCHESTRATOR_PUBLIC_PORT", 3000),
    internalPort: intFromEnv(env, "ORCHESTRATOR_INTERNAL_PORT", 3001),
    databasePath: env.DATABASE_PATH ?? "/app/data/turing.db",
    ollamaBaseUrl: env.OLLAMA_BASE_URL ?? "http://host.docker.internal:11434",
    ollamaModel: env.OLLAMA_MODEL ?? "llama3.2",
    openaiBaseUrl: env.OPENAI_BASE_URL ?? "https://api.openai.com/v1",
    openaiApiKey: secrets.get("OPENAI_API_KEY"),
    openaiModel: env.OPENAI_MODEL ?? "gpt-4o-mini",
    jobTimeoutMs: intFromEnv(env, "TURING_JOB_TIMEOUT_MS", 300000),
    jobReaperIntervalMs: intFromEnv(env, "TURING_JOB_REAPER_INTERVAL_MS", 60000),
    jobMaxAttempts: intFromEnv(env, "TURING_JOB_MAX_ATTEMPTS", 3),
    maxConcurrentRunsGeneral: intFromEnv(env, "TURING_MAX_CONCURRENT_RUNS_GENERAL", 1),
    maxToolCallsPerRun: intFromEnv(env, "TURING_MAX_TOOL_CALLS_PER_RUN", 10),
    modelTimeoutMs: intFromEnv(env, "TURING_MODEL_TIMEOUT_MS", 120000),
    toolTimeoutMs: intFromEnv(env, "TURING_TOOL_TIMEOUT_MS", 30000),
    logLevel: env.LOG_LEVEL ?? "info"
  };
}

export function loadConfig(): OrchestratorConfig {
  loadDotenv();
  return loadConfigFromEnv();
}
```

Create `turing-backend/orchestrator/src/security/auth.ts`:

```ts
import { Buffer } from "node:buffer";
import { timingSafeEqual } from "node:crypto";
import type { FastifyReply, FastifyRequest } from "fastify";

export function bearerTokenFromHeader(header: string | undefined): string | undefined {
  if (!header) return undefined;
  const [scheme, token] = header.split(" ");
  return scheme === "Bearer" && token ? token : undefined;
}

export function tokenMatches(actual: string | undefined, expected: string): boolean {
  if (!actual || !expected) return false;
  const actualBytes = Buffer.from(actual);
  const expectedBytes = Buffer.from(expected);
  return actualBytes.length === expectedBytes.length && timingSafeEqual(actualBytes, expectedBytes);
}

export function requireBearer(expectedToken: string) {
  return async (request: FastifyRequest, reply: FastifyReply): Promise<void> => {
    const token = bearerTokenFromHeader(request.headers.authorization);
    if (!tokenMatches(token, expectedToken)) {
      await reply.code(401).send({
        error: {
          code: "unauthorized",
          message: "Invalid or missing bearer token",
          requestId: request.id
        }
      });
    }
  };
}
```

Create `turing-backend/orchestrator/src/logging/logger.ts`:

```ts
import pino from "pino";

export function createLogger(level: string) {
  return pino({ level });
}
```

- [ ] **Step 5: Implement minimal dual-listener server**

Create `turing-backend/orchestrator/src/server.ts`:

```ts
import Fastify from "fastify";
import websocket from "@fastify/websocket";
import { loadConfig } from "./config.js";
import { createLogger } from "./logging/logger.js";
import { requireBearer } from "./security/auth.js";

const config = loadConfig();
const logger = createLogger(config.logLevel);

export async function buildPublicServer() {
  const app = Fastify({ logger, genReqId: () => crypto.randomUUID() });
  await app.register(websocket);

  app.get("/health", async () => ({ ok: true }));
  app.get("/version", async () => ({ version: "1.0.0", schemaVersion: "0001" }));

  app.addHook("preHandler", async (request, reply) => {
    if (request.routeOptions.url === "/health" || request.routeOptions.url === "/version" || request.routeOptions.url === "/ws") return;
    await requireBearer(config.clientApiKey)(request, reply);
  });

  return app;
}

export async function buildInternalServer() {
  const app = Fastify({ logger, genReqId: () => crypto.randomUUID() });
  app.addHook("preHandler", requireBearer(config.internalToken));
  app.get("/internal/health", async () => ({ ok: true }));
  return app;
}

if (import.meta.url === `file://${process.argv[1]}`) {
  const publicServer = await buildPublicServer();
  const internalServer = await buildInternalServer();

  await internalServer.listen({ host: "0.0.0.0", port: config.internalPort });
  await publicServer.listen({ host: "0.0.0.0", port: config.publicPort });
}
```

Create `turing-backend/orchestrator/Dockerfile`:

```dockerfile
FROM node:20-alpine AS deps
WORKDIR /repo
COPY package*.json ./
COPY shared-types/package.json shared-types/package.json
COPY orchestrator/package.json orchestrator/package.json
RUN npm install

FROM deps AS builder
COPY tsconfig.base.json ./
COPY shared-types shared-types
COPY orchestrator orchestrator
RUN npm run build -w @turing/shared-types && npm run build -w @turing/orchestrator

FROM node:20-alpine
WORKDIR /app
ENV NODE_ENV=production
COPY --from=deps /repo/node_modules /app/node_modules
COPY --from=builder /repo/shared-types/dist /app/shared-types/dist
COPY --from=builder /repo/orchestrator/dist /app/dist
COPY --from=builder /repo/orchestrator/migrations /app/migrations
COPY orchestrator/package.json /app/package.json
EXPOSE 3000 3001
CMD ["node", "dist/server.js"]
```

- [ ] **Step 6: Run tests and typecheck**

Run:

```bash
cd turing-backend
npm test -w @turing/orchestrator -- config.test.ts auth.test.ts
npm run typecheck -w @turing/orchestrator
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add turing-backend/orchestrator turing-backend/package-lock.json
git commit -m "feat: add orchestrator bootstrap and auth"
```

---

### Task 4: SQLite migrations, repositories, sessions, messages, jobs, events, and audit

**Files:**
- Create: `turing-backend/orchestrator/migrations/0001_initial.sql`
- Create: `turing-backend/orchestrator/src/db/connection.ts`
- Create: `turing-backend/orchestrator/src/db/migrations.ts`
- Create: `turing-backend/orchestrator/src/db/repositories.ts`
- Create: `turing-backend/orchestrator/src/events/service.ts`
- Create: `turing-backend/orchestrator/src/sessions/service.ts`
- Create: `turing-backend/orchestrator/src/jobs/service.ts`
- Create: `turing-backend/orchestrator/src/audit/service.ts`
- Test: `turing-backend/orchestrator/tests/db.test.ts`
- Test: `turing-backend/orchestrator/tests/services.test.ts`

- [ ] **Step 1: Write failing database migration test**

Create `turing-backend/orchestrator/tests/db.test.ts`:

```ts
import Database from "better-sqlite3";
import { describe, expect, it } from "vitest";
import { applyMigrations } from "../src/db/migrations.js";

describe("migrations", () => {
  it("creates the v1 schema", () => {
    const db = new Database(":memory:");
    applyMigrations(db);

    const tables = db.prepare("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name").all() as Array<{ name: string }>;
    expect(tables.map((row) => row.name)).toContain("sessions");
    expect(tables.map((row) => row.name)).toContain("jobs");
    expect(tables.map((row) => row.name)).toContain("audit_logs");
  });
});
```

- [ ] **Step 2: Run migration test to verify it fails**

Run:

```bash
cd turing-backend
npm test -w @turing/orchestrator -- db.test.ts
```

Expected: FAIL because `db/migrations.ts` does not exist.

- [ ] **Step 3: Add migration SQL**

Create `turing-backend/orchestrator/migrations/0001_initial.sql` using the exact schema from the spec. Include all tables and indexes:

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  title TEXT,
  status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','archived')),
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sessions_updated ON sessions(updated_at);

CREATE TABLE IF NOT EXISTS messages (
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
CREATE INDEX IF NOT EXISTS idx_messages_session_created ON messages(session_id, created_at);

CREATE TABLE IF NOT EXISTS agent_runs (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  user_message_id TEXT NOT NULL REFERENCES messages(id),
  assistant_message_id TEXT REFERENCES messages(id),
  agent_id TEXT NOT NULL,
  trace_id TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('queued','running','waiting_approval','completed','failed','cancelled')),
  model_provider TEXT NOT NULL,
  model_name TEXT NOT NULL,
  error_code TEXT,
  error_message TEXT,
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_runs_session_created ON agent_runs(session_id, created_at);
CREATE INDEX IF NOT EXISTS idx_runs_status ON agent_runs(status, created_at);

CREATE TABLE IF NOT EXISTS agent_run_steps (
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

CREATE TABLE IF NOT EXISTS jobs (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  agent_id TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending','in_progress','completed','failed','cancelled')),
  attempt INTEGER NOT NULL DEFAULT 1,
  payload_json TEXT NOT NULL,
  picked_up_at TEXT,
  finished_at TEXT,
  error_code TEXT,
  error_message TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_jobs_claim ON jobs(agent_id, status, created_at);
CREATE INDEX IF NOT EXISTS idx_jobs_reaper ON jobs(status, picked_up_at);

CREATE TABLE IF NOT EXISTS events (
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
CREATE INDEX IF NOT EXISTS idx_events_replay ON events(session_id, sequence);
CREATE INDEX IF NOT EXISTS idx_events_run ON events(run_id, sequence);

CREATE TABLE IF NOT EXISTS tools (
  id TEXT PRIMARY KEY,
  server_name TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  policy TEXT NOT NULL CHECK (policy IN ('safe','approval_required','disabled')),
  schema_json TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  discovered_at TEXT NOT NULL,
  UNIQUE(server_name, tool_name)
);

CREATE TABLE IF NOT EXISTS tool_calls (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  step_id TEXT REFERENCES agent_run_steps(id),
  agent_id TEXT NOT NULL,
  server_name TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  args_json TEXT NOT NULL,
  args_hash TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('requested','allowed','approval_required','completed','failed','denied')),
  result_summary TEXT,
  error_code TEXT,
  error_message TEXT,
  approval_id TEXT,
  duration_ms INTEGER,
  created_at TEXT NOT NULL,
  completed_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_tool_calls_run ON tool_calls(run_id, created_at);

CREATE TABLE IF NOT EXISTS approvals (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  tool_call_id TEXT REFERENCES tool_calls(id),
  agent_id TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  args_json TEXT NOT NULL,
  args_hash TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending','approved','denied','expired','consumed')),
  approval_jti TEXT,
  approval_token TEXT,
  expires_at TEXT NOT NULL,
  decided_at TEXT,
  consumed_at TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_approvals_status ON approvals(status, expires_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_approvals_tool_call ON approvals(tool_call_id) WHERE tool_call_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS audit_logs (
  id TEXT PRIMARY KEY,
  correlation_id TEXT,
  actor_type TEXT NOT NULL CHECK (actor_type IN ('client','runtime','mcp','system')),
  actor_id TEXT,
  action TEXT NOT NULL,
  target TEXT,
  payload_json TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_logs(action, created_at);
CREATE INDEX IF NOT EXISTS idx_audit_correlation ON audit_logs(correlation_id, created_at);
```

- [ ] **Step 4: Implement DB connection and migrations**

Create `turing-backend/orchestrator/src/db/connection.ts`:

```ts
import Database from "better-sqlite3";

export type TuringDatabase = Database.Database;

export function openDatabase(path: string): TuringDatabase {
  const db = new Database(path);
  db.pragma("journal_mode = WAL");
  db.pragma("busy_timeout = 5000");
  db.pragma("foreign_keys = ON");
  return db;
}
```

Create `turing-backend/orchestrator/src/db/migrations.ts`:

```ts
import fs from "node:fs";
import path from "node:path";
import type { TuringDatabase } from "./connection.js";

const MIGRATIONS_DIR = path.resolve(new URL(".", import.meta.url).pathname, "../../migrations");

export function applyMigrations(db: TuringDatabase, migrationsDir = MIGRATIONS_DIR): void {
  const files = fs.readdirSync(migrationsDir).filter((file) => file.endsWith(".sql")).sort();
  db.exec("CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)");

  const applied = db.prepare("SELECT version FROM schema_migrations").all() as Array<{ version: string }>;
  const appliedSet = new Set(applied.map((row) => row.version));

  const insert = db.prepare("INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)");
  const runMigration = db.transaction((file: string) => {
    const sql = fs.readFileSync(path.join(migrationsDir, file), "utf8");
    db.exec(sql);
    insert.run(file, new Date().toISOString());
  });

  for (const file of files) {
    if (!appliedSet.has(file)) runMigration(file);
  }
}
```

- [ ] **Step 5: Write failing service test**

Create `turing-backend/orchestrator/tests/services.test.ts`:

```ts
import Database from "better-sqlite3";
import { describe, expect, it } from "vitest";
import { applyMigrations } from "../src/db/migrations.js";
import { createSessionsService } from "../src/sessions/service.js";
import { createJobsService } from "../src/jobs/service.js";

describe("sessions and jobs services", () => {
  it("creates a session, message, run, and claimable job", () => {
    const db = new Database(":memory:");
    applyMigrations(db);

    const sessions = createSessionsService(db);
    const jobs = createJobsService(db, { jobTimeoutMs: 300000, maxAttempts: 3 });

    const session = sessions.createSession({ title: "Test" });
    const queued = sessions.enqueueUserMessage({
      sessionId: session.sessionId,
      content: "hello",
      agentId: "general_assistant",
      modelProvider: "ollama",
      model: "llama3.2"
    });

    const job = jobs.claimNext("general_assistant");
    expect(job?.jobId).toBe(queued.jobId);
    expect(job?.payload.userText).toBe("hello");
  });
});
```

- [ ] **Step 6: Implement focused services**

Create `turing-backend/orchestrator/src/sessions/service.ts`:

```ts
import { ulid } from "ulid";
import type { SendMessageResponse } from "@turing/shared-types";
import type { TuringDatabase } from "../db/connection.js";

const now = () => new Date().toISOString();
const id = (prefix: string) => `${prefix}_${ulid()}`;

export function createSessionsService(db: TuringDatabase) {
  return {
    createSession(input: { title?: string }) {
      const createdAt = now();
      const sessionId = id("sess");
      db.prepare("INSERT INTO sessions (id, title, created_at, updated_at) VALUES (?, ?, ?, ?)").run(
        sessionId,
        input.title ?? null,
        createdAt,
        createdAt
      );
      return { sessionId, createdAt };
    },

    listSessions(limit = 50) {
      return db.prepare("SELECT id, title, status, created_at AS createdAt, updated_at AS updatedAt FROM sessions ORDER BY updated_at DESC LIMIT ?").all(limit);
    },

    getMessages(sessionId: string, limit = 50) {
      return db.prepare("SELECT id, role, content, content_type AS contentType, sequence, created_at AS createdAt FROM messages WHERE session_id = ? ORDER BY sequence DESC LIMIT ?").all(sessionId, limit).reverse();
    },

    enqueueUserMessage(input: {
      sessionId: string;
      content: string;
      agentId: "general_assistant";
      modelProvider: "ollama" | "openai_compatible";
      model: string;
    }): SendMessageResponse {
      const createdAt = now();
      const userMessageId = id("msg");
      const assistantMessageId = id("msg");
      const runId = id("run");
      const jobId = id("job");
      const traceId = id("trace");

      const sequenceRow = db.prepare("SELECT COALESCE(MAX(sequence), 0) + 1 AS next FROM messages WHERE session_id = ?").get(input.sessionId) as { next: number };

      const tx = db.transaction(() => {
        db.prepare("INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at) VALUES (?, ?, 'user', ?, 'text', ?, ?)").run(
          userMessageId,
          input.sessionId,
          input.content,
          sequenceRow.next,
          createdAt
        );
        db.prepare("INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at) VALUES (?, ?, ?, 'assistant', '', 'text', ?, ?)").run(
          assistantMessageId,
          input.sessionId,
          runId,
          sequenceRow.next + 1,
          createdAt
        );
        db.prepare("INSERT INTO agent_runs (id, session_id, user_message_id, assistant_message_id, agent_id, trace_id, status, model_provider, model_name, created_at) VALUES (?, ?, ?, ?, ?, ?, 'queued', ?, ?, ?)").run(
          runId,
          input.sessionId,
          userMessageId,
          assistantMessageId,
          input.agentId,
          traceId,
          input.modelProvider,
          input.model,
          createdAt
        );
        db.prepare("INSERT INTO jobs (id, run_id, agent_id, status, payload_json, created_at) VALUES (?, ?, ?, 'pending', ?, ?)").run(
          jobId,
          runId,
          input.agentId,
          JSON.stringify({ userText: input.content, sessionId: input.sessionId, userMessageId, assistantMessageId, traceId, modelProvider: input.modelProvider, model: input.model }),
          createdAt
        );
      });
      tx();

      return { sessionId: input.sessionId, userMessageId, assistantMessageId, runId, jobId, traceId, status: "queued" };
    }
  };
}
```

Create `turing-backend/orchestrator/src/jobs/service.ts`:

```ts
import type { AgentJob } from "@turing/shared-types";
import type { TuringDatabase } from "../db/connection.js";

export function createJobsService(db: TuringDatabase, config: { jobTimeoutMs: number; maxAttempts: number }) {
  return {
    claimNext(agentId: "general_assistant"): AgentJob | undefined {
      const row = db.prepare("SELECT * FROM jobs WHERE agent_id = ? AND status = 'pending' ORDER BY created_at LIMIT 1").get(agentId) as any;
      if (!row) return undefined;

      const pickedUpAt = new Date().toISOString();
      const updated = db.prepare("UPDATE jobs SET status = 'in_progress', picked_up_at = ? WHERE id = ? AND status = 'pending'").run(pickedUpAt, row.id);
      if (updated.changes !== 1) return undefined;
      db.prepare("UPDATE agent_runs SET status = 'running', started_at = ? WHERE id = ? AND status = 'queued'").run(pickedUpAt, row.run_id);

      const payload = JSON.parse(row.payload_json);
      return {
        jobId: row.id,
        runId: row.run_id,
        sessionId: payload.sessionId,
        userMessageId: payload.userMessageId,
        assistantMessageId: payload.assistantMessageId,
        agentId,
        traceId: payload.traceId,
        modelProvider: payload.modelProvider,
        model: payload.model,
        payload: { userText: payload.userText },
        attempt: row.attempt
      };
    },

    reapStaleJobs(): number {
      const cutoff = new Date(Date.now() - config.jobTimeoutMs).toISOString();
      const stale = db.prepare("SELECT id, run_id, attempt FROM jobs WHERE status = 'in_progress' AND picked_up_at < ?").all(cutoff) as Array<{ id: string; run_id: string; attempt: number }>;
      let count = 0;
      const tx = db.transaction(() => {
        for (const job of stale) {
          if (job.attempt >= config.maxAttempts) {
            db.prepare("UPDATE jobs SET status = 'failed', finished_at = ?, error_code = 'job_timeout', error_message = 'Job timed out' WHERE id = ?").run(new Date().toISOString(), job.id);
            db.prepare("UPDATE agent_runs SET status = 'failed', error_code = 'job_timeout', error_message = 'Job timed out', finished_at = ? WHERE id = ?").run(new Date().toISOString(), job.run_id);
          } else {
            db.prepare("UPDATE jobs SET status = 'pending', attempt = attempt + 1, picked_up_at = NULL WHERE id = ?").run(job.id);
          }
          count += 1;
        }
      });
      tx();
      return count;
    }
  };
}
```

Create `turing-backend/orchestrator/src/events/service.ts` and `src/audit/service.ts` with minimal append/query helpers:

```ts
import { ulid } from "ulid";
import type { TuringEventInput } from "@turing/shared-types";
import type { TuringDatabase } from "../db/connection.js";

export function createEventsService(db: TuringDatabase) {
  return {
    append(input: TuringEventInput) {
      const row = db.prepare("SELECT COALESCE(MAX(sequence), 0) + 1 AS next FROM events WHERE session_id = ?").get(input.sessionId) as { next: number };
      const event = {
        eventId: `evt_${ulid()}`,
        sequence: row.next,
        createdAt: input.createdAt ?? new Date().toISOString(),
        ...input
      };
      db.prepare("INSERT INTO events (id, session_id, run_id, trace_id, sequence, type, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)").run(
        event.eventId,
        event.sessionId,
        event.runId ?? null,
        event.traceId,
        event.sequence,
        event.type,
        JSON.stringify(event.payload),
        event.createdAt
      );
      return event;
    },

    replay(sessionId: string, afterSequence: number) {
      return db.prepare("SELECT * FROM events WHERE session_id = ? AND sequence > ? ORDER BY sequence LIMIT 500").all(sessionId, afterSequence);
    }
  };
}
```

```ts
import { ulid } from "ulid";
import type { TuringDatabase } from "../db/connection.js";

export function createAuditService(db: TuringDatabase) {
  return {
    record(input: { correlationId?: string; actorType: "client" | "runtime" | "mcp" | "system"; actorId?: string; action: string; target?: string; payload?: unknown }) {
      db.prepare("INSERT INTO audit_logs (id, correlation_id, actor_type, actor_id, action, target, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)").run(
        `audit_${ulid()}`,
        input.correlationId ?? null,
        input.actorType,
        input.actorId ?? null,
        input.action,
        input.target ?? null,
        input.payload ? JSON.stringify(input.payload) : null,
        new Date().toISOString()
      );
    },

    list(limit = 100) {
      return db.prepare("SELECT id, correlation_id AS correlationId, actor_type AS actorType, actor_id AS actorId, action, target, payload_json AS payloadJson, created_at AS createdAt FROM audit_logs ORDER BY created_at DESC LIMIT ?").all(limit);
    }
  };
}
```

- [ ] **Step 7: Run database and service tests**

Run:

```bash
cd turing-backend
npm test -w @turing/orchestrator -- db.test.ts services.test.ts
npm run typecheck -w @turing/orchestrator
```

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add turing-backend/orchestrator/migrations turing-backend/orchestrator/src/db turing-backend/orchestrator/src/events turing-backend/orchestrator/src/sessions turing-backend/orchestrator/src/jobs turing-backend/orchestrator/src/audit turing-backend/orchestrator/tests
git commit -m "feat: add orchestrator persistence services"
```

---

### Task 5: Public REST API for sessions, messages, config, agents, audit, and tools

**Files:**
- Create: `turing-backend/orchestrator/src/api/routes.ts`
- Modify: `turing-backend/orchestrator/src/server.ts`
- Modify: `turing-backend/orchestrator/src/db/repositories.ts`
- Test: `turing-backend/orchestrator/tests/public-api.test.ts`

- [ ] **Step 1: Write failing public API test**

Create `turing-backend/orchestrator/tests/public-api.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { buildPublicServerForTest } from "./testServer.js";

describe("public REST API", () => {
  it("rejects missing API keys", async () => {
    const app = await buildPublicServerForTest();
    const response = await app.inject({ method: "GET", url: "/api/sessions" });
    expect(response.statusCode).toBe(401);
  });

  it("creates a session and queues a message", async () => {
    const app = await buildPublicServerForTest();
    const auth = { authorization: "Bearer tk_test" };

    const sessionResponse = await app.inject({ method: "POST", url: "/api/sessions", headers: auth, payload: { title: "Test" } });
    expect(sessionResponse.statusCode).toBe(201);
    const session = sessionResponse.json() as { sessionId: string };

    const messageResponse = await app.inject({
      method: "POST",
      url: `/api/sessions/${session.sessionId}/messages`,
      headers: auth,
      payload: { content: "hello", modelProvider: "ollama" }
    });

    expect(messageResponse.statusCode).toBe(202);
    expect(messageResponse.json()).toMatchObject({ status: "queued", sessionId: session.sessionId });
  });

  it("lists audit entries through the public audit endpoint", async () => {
    const app = await buildPublicServerForTest();
    const response = await app.inject({ method: "GET", url: "/api/audit", headers: { authorization: "Bearer tk_test" } });
    expect(response.statusCode).toBe(200);
    expect(response.json()).toMatchObject({ entries: expect.any(Array) });
  });
});
```

Create `turing-backend/orchestrator/tests/testServer.ts`:

```ts
import Database from "better-sqlite3";
import { buildPublicServer } from "../src/server.js";
import { applyMigrations } from "../src/db/migrations.js";

export async function buildPublicServerForTest() {
  const db = new Database(":memory:");
  applyMigrations(db);
  return buildPublicServer({
    db,
    config: {
      clientApiKey: "tk_test",
      internalToken: "internal",
      mcpSystemTokenGeneral: "system",
      mcpFilesTokenGeneral: "files",
      approvalJwtSecret: "approval",
      publicPort: 3000,
      internalPort: 3001,
      databasePath: ":memory:",
      ollamaBaseUrl: "http://ollama",
      ollamaModel: "llama3.2",
      openaiBaseUrl: "https://api.openai.com/v1",
      openaiModel: "gpt-4o-mini",
      jobTimeoutMs: 300000,
      jobReaperIntervalMs: 60000,
      jobMaxAttempts: 3,
      logLevel: "silent"
    }
  });
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
cd turing-backend
npm test -w @turing/orchestrator -- public-api.test.ts
```

Expected: FAIL because `buildPublicServer` does not accept injected dependencies and routes are missing.

- [ ] **Step 3: Implement route registration**

Create `turing-backend/orchestrator/src/api/routes.ts`:

```ts
import type { FastifyInstance } from "fastify";
import type { OrchestratorConfig } from "../config.js";
import { createSessionsService } from "../sessions/service.js";
import { createJobsService } from "../jobs/service.js";
import { createAuditService } from "../audit/service.js";
import { createEventsService } from "../events/service.js";
import type { TuringDatabase } from "../db/connection.js";

export async function registerPublicRoutes(app: FastifyInstance, deps: { db: TuringDatabase; config: OrchestratorConfig; hub?: { broadcast(event: unknown): void } }) {
  const sessions = createSessionsService(deps.db);
  const jobs = createJobsService(deps.db, { jobTimeoutMs: deps.config.jobTimeoutMs, maxAttempts: deps.config.jobMaxAttempts });
  const audit = createAuditService(deps.db);
  const events = createEventsService(deps.db);

  app.get("/api/config", async () => ({
    providers: {
      ollama: { enabled: true, defaultModel: deps.config.ollamaModel },
      openai_compatible: { enabled: Boolean(deps.config.openaiApiKey), defaultModel: deps.config.openaiModel }
    },
    features: { approvals: true, filesMcp: true }
  }));

  app.post<{ Body: { title?: string } }>("/api/sessions", async (request, reply) => {
    return reply.code(201).send(sessions.createSession({ title: request.body?.title }));
  });

  app.get("/api/sessions", async () => ({ sessions: sessions.listSessions() }));

  app.get<{ Params: { sessionId: string } }>("/api/sessions/:sessionId", async (request, reply) => {
    const session = deps.db.prepare("SELECT id AS sessionId, title, created_at AS createdAt, updated_at AS updatedAt FROM sessions WHERE id = ?").get(request.params.sessionId);
    if (!session) return reply.code(404).send({ error: { code: "session_not_found", message: "Session not found", requestId: request.id } });
    return session;
  });

  app.get<{ Params: { sessionId: string } }>("/api/sessions/:sessionId/messages", async (request) => ({
    messages: sessions.getMessages(request.params.sessionId)
  }));

  // REST replay fallback for reconnect/debug clients; WebSocket hello replay remains the primary streaming path.
  app.get<{ Params: { sessionId: string }; Querystring: { after?: string } }>("/api/sessions/:sessionId/events", async (request) => {
    const replayedEvents = events.replay(request.params.sessionId, Number(request.query.after ?? 0));
    return {
      events: replayedEvents,
      latestSequence: replayedEvents.at(-1)?.sequence ?? Number(request.query.after ?? 0)
    };
  });

  app.post<{ Params: { sessionId: string }; Body: { content: string; modelProvider?: "ollama" | "openai_compatible"; model?: string } }>(
    "/api/sessions/:sessionId/messages",
    async (request, reply) => {
      if (!request.body?.content || typeof request.body.content !== "string") {
        return reply.code(400).send({ error: { code: "invalid_request", message: "content is required", requestId: request.id } });
      }
      const modelProvider = request.body.modelProvider ?? "ollama";
      const model = request.body.model ?? (modelProvider === "ollama" ? deps.config.ollamaModel : deps.config.openaiModel);
      const result = sessions.enqueueUserMessage({
        sessionId: request.params.sessionId,
        content: request.body.content,
        agentId: "general_assistant",
        modelProvider,
        model
      });
      const queuedEvent = events.append({
        sessionId: request.params.sessionId,
        runId: result.runId,
        traceId: result.traceId,
        type: "agent.run.queued",
        payload: { runId: result.runId, status: "queued", agentId: "general_assistant" }
      });
      deps.hub?.broadcast(queuedEvent);
      jobs.reapStaleJobs();
      return reply.code(202).send(result);
    }
  );

  app.get("/api/agents", async () => ({
    agents: [{ id: "general_assistant", displayName: "General Assistant" }]
  }));

  app.get("/api/tools", async () => ({
    tools: [
      { serverName: "system", toolName: "system.time", policy: "safe" },
      { serverName: "files", toolName: "files.update", policy: "approval_required" }
    ]
  }));

  app.get("/api/audit", async () => ({ entries: audit.list(100) }));
  app.get("/api/tool-calls", async () => ({
    toolCalls: deps.db.prepare("SELECT id, run_id AS runId, tool_name AS toolName, status, args_hash AS argsHash, approval_id AS approvalId, duration_ms AS durationMs, created_at AS createdAt, completed_at AS completedAt FROM tool_calls ORDER BY created_at DESC LIMIT 100").all()
  }));
}
```

- [ ] **Step 4: Refactor server dependency injection**

Modify `turing-backend/orchestrator/src/server.ts` so `buildPublicServer` accepts optional deps:

```ts
import Fastify from "fastify";
import websocket from "@fastify/websocket";
import { loadConfig, type OrchestratorConfig } from "./config.js";
import { createLogger } from "./logging/logger.js";
import { requireBearer } from "./security/auth.js";
import { openDatabase, type TuringDatabase } from "./db/connection.js";
import { applyMigrations } from "./db/migrations.js";
import { registerPublicRoutes } from "./api/routes.js";

type BroadcastHub = { broadcast(event: unknown): void };
type ServerDeps = { config?: OrchestratorConfig; db?: TuringDatabase; hub?: BroadcastHub };

export async function buildPublicServer(deps: ServerDeps = {}) {
  const config = deps.config ?? loadConfig();
  const db = deps.db ?? openDatabase(config.databasePath);
  if (!deps.db) applyMigrations(db);

  const app = Fastify({ logger: createLogger(config.logLevel), genReqId: () => crypto.randomUUID() });
  await app.register(websocket);
  app.decorate("db", db);

  app.get("/health", async () => ({ ok: true }));
  app.get("/version", async () => ({ version: "1.0.0", schemaVersion: "0001" }));

  app.addHook("preHandler", async (request, reply) => {
    if (request.routeOptions.url === "/health" || request.routeOptions.url === "/version" || request.routeOptions.url === "/ws") return;
    await requireBearer(config.clientApiKey)(request, reply);
  });

  await registerPublicRoutes(app, { db, config, hub: deps.hub });
  return app;
}
```

Keep `buildInternalServer` compiling; it will be expanded in Task 6.

- [ ] **Step 5: Run public API tests**

Run:

```bash
cd turing-backend
npm test -w @turing/orchestrator -- public-api.test.ts
npm run typecheck -w @turing/orchestrator
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add turing-backend/orchestrator/src turing-backend/orchestrator/tests
git commit -m "feat: add orchestrator public REST API"
```

---

### Task 6: Internal orchestrator API and job/run lifecycle

**Files:**
- Create: `turing-backend/orchestrator/src/internal/routes.ts`
- Modify: `turing-backend/orchestrator/src/server.ts`
- Modify: `turing-backend/orchestrator/src/jobs/service.ts`
- Modify: `turing-backend/orchestrator/src/events/service.ts`
- Test: `turing-backend/orchestrator/tests/internal-api.test.ts`

- [ ] **Step 1: Write failing internal API test**

Create `turing-backend/orchestrator/tests/internal-api.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { buildInternalServerForTest, seedQueuedJob } from "./testServer.js";

describe("internal API", () => {
  it("requires the internal token", async () => {
    const app = await buildInternalServerForTest();
    const response = await app.inject({ method: "GET", url: "/internal/jobs/next?agent=general_assistant" });
    expect(response.statusCode).toBe(401);
  });

  it("claims a queued job and appends a run event", async () => {
    const { app, db } = await buildInternalServerForTest();
    const seeded = seedQueuedJob(db);
    const headers = { authorization: "Bearer internal" };

    const claim = await app.inject({ method: "GET", url: "/internal/jobs/next?agent=general_assistant", headers });
    expect(claim.statusCode).toBe(200);
    expect(claim.json()).toMatchObject({ jobId: seeded.jobId, runId: seeded.runId });

    const event = await app.inject({
      method: "POST",
      url: `/internal/runs/${seeded.runId}/events`,
      headers,
      payload: {
        event: {
          sessionId: seeded.sessionId,
          runId: seeded.runId,
          traceId: seeded.traceId,
          type: "message.delta",
          payload: { messageId: seeded.assistantMessageId, delta: "hi" }
        }
      }
    });

    expect(event.statusCode).toBe(200);
    expect(event.json()).toMatchObject({ sequence: 1 });
  });
});
```

- [ ] **Step 2: Add test helper seed function**

Modify `turing-backend/orchestrator/tests/testServer.ts`:

```ts
import { createSessionsService } from "../src/sessions/service.js";
import type { TuringDatabase } from "../src/db/connection.js";
import { buildInternalServer } from "../src/server.js";

export async function buildInternalServerForTest() {
  const db = new Database(":memory:");
  applyMigrations(db);
  const app = await buildInternalServer({ db, config: testConfig });
  return { app, db };
}

export function seedQueuedJob(db: TuringDatabase) {
  const sessions = createSessionsService(db);
  const session = sessions.createSession({ title: "Internal" });
  return sessions.enqueueUserMessage({
    sessionId: session.sessionId,
    content: "hello",
    agentId: "general_assistant",
    modelProvider: "ollama",
    model: "llama3.2"
  });
}
```

Define `testConfig` once in the helper and reuse it for public/internal builders.

- [ ] **Step 3: Run test to verify it fails**

Run:

```bash
cd turing-backend
npm test -w @turing/orchestrator -- internal-api.test.ts
```

Expected: FAIL because internal routes are missing.

- [ ] **Step 4: Implement internal routes**

Create `turing-backend/orchestrator/src/internal/routes.ts`:

```ts
import type { FastifyInstance } from "fastify";
import { createHash } from "node:crypto";
import type { TuringEventInput, ToolCallBeacon } from "@turing/shared-types";
import type { TuringDatabase } from "../db/connection.js";
import { createJobsService } from "../jobs/service.js";
import { createEventsService } from "../events/service.js";
import { createSessionsService } from "../sessions/service.js";
import { createAuditService } from "../audit/service.js";
import type { OrchestratorConfig } from "../config.js";

export async function registerInternalRoutes(app: FastifyInstance, deps: { db: TuringDatabase; config: OrchestratorConfig; hub?: { broadcast(event: unknown): void } }) {
  const jobs = createJobsService(deps.db, { jobTimeoutMs: deps.config.jobTimeoutMs, maxAttempts: deps.config.jobMaxAttempts });
  const events = createEventsService(deps.db);
  const sessions = createSessionsService(deps.db);
  const audit = createAuditService(deps.db);

  app.get<{ Querystring: { agent?: "general_assistant" } }>("/internal/jobs/next", async (request, reply) => {
    const agent = request.query.agent ?? "general_assistant";
    const job = jobs.claimNext(agent);
    if (!job) return reply.code(204).send();
    events.append({
      sessionId: job.sessionId,
      runId: job.runId,
      traceId: job.traceId,
      type: "agent.run.started",
      payload: { runId: job.runId, status: "running", agentId: job.agentId }
    });
    return job;
  });

  app.post<{ Params: { runId: string }; Body: { event: TuringEventInput } }>("/internal/runs/:runId/events", async (request) => {
    return events.append({ ...request.body.event, runId: request.params.runId });
  });

  app.post<{ Params: { runId: string }; Body: ToolCallBeacon }>("/internal/runs/:runId/audit/tool-call", async (request) => {
    if (request.body.phase === "before") {
      deps.db.prepare("INSERT OR IGNORE INTO tool_calls (id, run_id, agent_id, server_name, tool_name, args_json, args_hash, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'requested', ?)").run(
        request.body.toolCallId,
        request.params.runId,
        request.body.agentId,
        request.body.serverName,
        request.body.toolName,
        JSON.stringify(request.body.args ?? {}),
        `sha256:${createHash("sha256").update(JSON.stringify(request.body.args ?? {})).digest("hex")}`,
        new Date().toISOString()
      );
      const run = deps.db.prepare("SELECT session_id, trace_id FROM agent_runs WHERE id = ?").get(request.params.runId) as { session_id: string; trace_id: string };
      events.append({ sessionId: run.session_id, runId: request.params.runId, traceId: run.trace_id, type: "tool.call.started", payload: request.body });
    } else {
      deps.db.prepare("UPDATE tool_calls SET status = ?, duration_ms = ?, completed_at = ? WHERE id = ?").run(
        request.body.status ?? "completed",
        request.body.durationMs ?? null,
        new Date().toISOString(),
        request.body.toolCallId
      );
      const run = deps.db.prepare("SELECT session_id, trace_id FROM agent_runs WHERE id = ?").get(request.params.runId) as { session_id: string; trace_id: string };
      const type = request.body.status === "failed" ? "tool.call.failed" : request.body.status === "denied" ? "tool.call.denied" : "tool.call.completed";
      events.append({ sessionId: run.session_id, runId: request.params.runId, traceId: run.trace_id, type, payload: request.body });
    }
    audit.record({
      correlationId: request.params.runId,
      actorType: "runtime",
      actorId: request.body.agentId,
      action: request.body.phase === "before" ? "tool.call.before" : "tool.call.after",
      target: `${request.body.serverName}.${request.body.toolName}`,
      payload: request.body
    });
    return { decision: "allow", toolCallId: request.body.toolCallId };
  });

  app.get<{ Params: { sessionId: string } }>("/internal/sessions/:sessionId/messages", async (request) => ({
    messages: sessions.getMessages(request.params.sessionId)
  }));

  app.post<{ Params: { runId: string }; Body: { assistantMessageId: string; content: string } }>("/internal/runs/:runId/complete", async (request) => {
    const run = deps.db.prepare("SELECT session_id, trace_id FROM agent_runs WHERE id = ?").get(request.params.runId) as { session_id: string; trace_id: string };
    jobs.completeRun(request.params.runId, request.body.assistantMessageId, request.body.content);
    events.append({ sessionId: run.session_id, runId: request.params.runId, traceId: run.trace_id, type: "agent.run.completed", payload: { runId: request.params.runId } });
    return { status: "completed" };
  });

  app.post<{ Params: { runId: string }; Body: { code: string; message: string; retryable: boolean } }>("/internal/runs/:runId/fail", async (request) => {
    const run = deps.db.prepare("SELECT session_id, trace_id FROM agent_runs WHERE id = ?").get(request.params.runId) as { session_id: string; trace_id: string };
    jobs.failRun(request.params.runId, request.body.code, request.body.message);
    events.append({ sessionId: run.session_id, runId: request.params.runId, traceId: run.trace_id, type: "agent.run.failed", payload: { runId: request.params.runId, code: request.body.code, message: request.body.message } });
    return { status: "failed" };
  });
}
```

- [ ] **Step 5: Extend job service for completion/failure**

Add to `createJobsService`:

```ts
completeRun(runId: string, assistantMessageId: string, content: string): void {
  const finishedAt = new Date().toISOString();
  const tx = db.transaction(() => {
    db.prepare("UPDATE messages SET content = ? WHERE id = ?").run(content, assistantMessageId);
    db.prepare("UPDATE agent_runs SET status = 'completed', finished_at = ? WHERE id = ?").run(finishedAt, runId);
    db.prepare("UPDATE jobs SET status = 'completed', finished_at = ? WHERE run_id = ?").run(finishedAt, runId);
  });
  tx();
},

failRun(runId: string, code: string, message: string): void {
  const finishedAt = new Date().toISOString();
  const tx = db.transaction(() => {
    db.prepare("UPDATE agent_runs SET status = 'failed', error_code = ?, error_message = ?, finished_at = ? WHERE id = ?").run(code, message, finishedAt, runId);
    db.prepare("UPDATE jobs SET status = 'failed', error_code = ?, error_message = ?, finished_at = ? WHERE run_id = ?").run(code, message, finishedAt, runId);
  });
  tx();
}
```

- [ ] **Step 6: Register internal routes in server**

Modify `buildInternalServer` in `server.ts`:

```ts
import { registerInternalRoutes } from "./internal/routes.js";

export async function buildInternalServer(deps: ServerDeps = {}) {
  const config = deps.config ?? loadConfig();
  const db = deps.db ?? openDatabase(config.databasePath);
  if (!deps.db) applyMigrations(db);
  const app = Fastify({ logger: createLogger(config.logLevel), genReqId: () => crypto.randomUUID() });
  app.addHook("preHandler", requireBearer(config.internalToken));
  app.get("/internal/health", async () => ({ ok: true }));
  await registerInternalRoutes(app, { db, config, hub: deps.hub });
  return app;
}
```

- [ ] **Step 7: Run internal API tests**

Run:

```bash
cd turing-backend
npm test -w @turing/orchestrator -- internal-api.test.ts
npm run typecheck -w @turing/orchestrator
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add turing-backend/orchestrator/src turing-backend/orchestrator/tests
git commit -m "feat: add orchestrator internal runtime API"
```

---

### Task 7: WebSocket gateway with durable replay

**Files:**
- Create: `turing-backend/orchestrator/src/ws/gateway.ts`
- Modify: `turing-backend/orchestrator/src/api/routes.ts`
- Modify: `turing-backend/orchestrator/src/events/service.ts`
- Modify: `turing-backend/orchestrator/src/server.ts`
- Test: `turing-backend/orchestrator/tests/ws.test.ts`

- [ ] **Step 1: Write failing event service replay test**

Create `turing-backend/orchestrator/tests/ws.test.ts`:

```ts
import Database from "better-sqlite3";
import { describe, expect, it } from "vitest";
import { applyMigrations } from "../src/db/migrations.js";
import { createEventsService } from "../src/events/service.js";
import { createWsHub } from "../src/ws/gateway.js";

describe("event replay", () => {
  it("persists before replaying by sequence", () => {
    const db = new Database(":memory:");
    applyMigrations(db);
    db.prepare("INSERT INTO sessions (id, created_at, updated_at) VALUES ('sess_1', 'now', 'now')").run();

    const events = createEventsService(db);
    events.append({ sessionId: "sess_1", traceId: "trace_1", type: "system", payload: { message: "one" } });
    events.append({ sessionId: "sess_1", traceId: "trace_1", type: "system", payload: { message: "two" } });

    expect(events.replay("sess_1", 1)).toHaveLength(1);
  });

  it("does not broadcast session events to clients before hello registration", () => {
    const hub = createWsHub();
    const sent: string[] = [];
    hub.add({
      socket: {
        send: (message: string) => sent.push(message),
        close: () => undefined,
        addEventListener: () => undefined
      } as unknown as WebSocket
    });

    hub.broadcast({ eventId: "evt_1", sessionId: "sess_1", traceId: "trace_1", sequence: 1, type: "system", payload: {}, createdAt: "now" });

    expect(sent).toEqual([]);
  });
});
```

- [ ] **Step 2: Run test and fix replay row mapping**

Run:

```bash
cd turing-backend
npm test -w @turing/orchestrator -- ws.test.ts
```

If it fails because rows are raw DB rows, update `events.replay` to map `payload_json` into `payload` and `id` into `eventId`:

```ts
replay(sessionId: string, afterSequence: number) {
  const rows = db.prepare("SELECT * FROM events WHERE session_id = ? AND sequence > ? ORDER BY sequence LIMIT 500").all(sessionId, afterSequence) as any[];
  return rows.map((row) => ({
    eventId: row.id,
    sessionId: row.session_id,
    runId: row.run_id ?? undefined,
    traceId: row.trace_id,
    sequence: row.sequence,
    type: row.type,
    createdAt: row.created_at,
    payload: JSON.parse(row.payload_json)
  }));
}
```

- [ ] **Step 3: Implement WebSocket gateway**

Create `turing-backend/orchestrator/src/ws/gateway.ts`:

```ts
import type { FastifyInstance } from "fastify";
import type { TuringEvent } from "@turing/shared-types";
import type { TuringDatabase } from "../db/connection.js";
import { createEventsService } from "../events/service.js";
import { tokenMatches } from "../security/auth.js";

type Client = {
  sessionId?: string;
  socket: WebSocket;
};

export function createWsHub() {
  const clients = new Set<Client>();

  return {
    add(client: Client) {
      clients.add(client);
      client.socket.addEventListener("close", () => clients.delete(client));
    },

    broadcast(event: TuringEvent) {
      for (const client of clients) {
        if (!client.sessionId || client.sessionId !== event.sessionId) continue;
        try {
          client.socket.send(JSON.stringify({ type: "event", event }));
        } catch {
          client.socket.close();
          clients.delete(client);
        }
      }
    }
  };
}

export async function registerWebSocket(app: FastifyInstance, deps: { db: TuringDatabase; clientApiKey: string; hub: ReturnType<typeof createWsHub> }) {
  const events = createEventsService(deps.db);

  app.get("/ws", { websocket: true }, (socket, request) => {
    const token = (request.query as { token?: string }).token;
    if (!tokenMatches(token, deps.clientApiKey)) {
      socket.close(1008, "unauthorized");
      return;
    }

    const client = { socket: socket as unknown as WebSocket };
    deps.hub.add(client);

    socket.on("message", (raw) => {
      const message = JSON.parse(raw.toString()) as { type: string; sessionId?: string; lastSequence?: number; ts?: number };
      if (message.type === "hello" && message.sessionId) {
        client.sessionId = message.sessionId;
        const replayedEvents = events.replay(message.sessionId, message.lastSequence ?? 0);
        socket.send(JSON.stringify({ type: "hello_ack", sessionId: message.sessionId, latestSequence: replayedEvents.at(-1)?.sequence ?? message.lastSequence ?? 0, replayedEvents }));
      }
      if (message.type === "ping") {
        socket.send(JSON.stringify({ type: "pong", ts: message.ts }));
      }
    });
  });
}
```

- [ ] **Step 4: Broadcast appended events from internal API**

In `server.ts`, wire the WebSocket gateway into the public server:

```ts
import { createWsHub, registerWebSocket } from "./ws/gateway.js";

type ServerDeps = { config?: OrchestratorConfig; db?: TuringDatabase; hub?: ReturnType<typeof createWsHub> };

// inside buildPublicServer, after auth hooks and before registerPublicRoutes:
if (deps.hub) await registerWebSocket(app, { db, clientApiKey: config.clientApiKey, hub: deps.hub });
await registerPublicRoutes(app, { db, config, hub: deps.hub });
```

Then load config, open the database, and apply migrations once in the executable entrypoint. Create one hub, pass the same `db` handle to public/internal servers, and start both listeners. Task 13 wires the maintenance sweep once the approval service exists.

```ts
import { createWsHub } from "./ws/gateway.js";

const config = loadConfig();
const db = openDatabase(config.databasePath);
applyMigrations(db);
const hub = createWsHub();

const app = await buildPublicServer({ db, config, hub });
const internalApp = await buildInternalServer({ db, config, hub });

await internalApp.listen({ host: "0.0.0.0", port: config.internalPort });
await app.listen({ host: "0.0.0.0", port: config.publicPort });

process.once("SIGTERM", async () => {
  await Promise.all([app.close(), internalApp.close()]);
  db.close();
});
```

In `internal/routes.ts`, after appending an event:

```ts
const event = events.append({ ...request.body.event, runId: request.params.runId });
    deps.hub?.broadcast(event);
return event;
```

Update the `registerInternalRoutes` signature to include `hub`.

- [ ] **Step 5: Run WebSocket-related tests**

Run:

```bash
cd turing-backend
npm test -w @turing/orchestrator -- ws.test.ts internal-api.test.ts
npm run typecheck -w @turing/orchestrator
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add turing-backend/orchestrator/src/ws turing-backend/orchestrator/src turing-backend/orchestrator/tests
git commit -m "feat: add durable websocket event replay"
```

---

### Task 8: Agent runtime package, internal client, executor interface, and job loop

**Files:**
- Create: `turing-backend/agent-runtime/package.json`
- Create: `turing-backend/agent-runtime/tsconfig.json`
- Create: `turing-backend/agent-runtime/Dockerfile`
- Create: `turing-backend/agent-runtime/src/config.ts`
- Create: `turing-backend/agent-runtime/src/logging/logger.ts`
- Create: `turing-backend/agent-runtime/src/executor/types.ts`
- Create: `turing-backend/agent-runtime/src/orchestrator/client.ts`
- Create: `turing-backend/agent-runtime/src/executor/jobLoop.ts`
- Create: `turing-backend/agent-runtime/src/agents/generalAssistant.ts`
- Create: `turing-backend/agent-runtime/src/main.ts`
- Test: `turing-backend/agent-runtime/tests/jobLoop.test.ts`

- [ ] **Step 1: Create package files**

Create `turing-backend/agent-runtime/package.json`:

```json
{
  "name": "@turing/agent-runtime",
  "version": "1.0.0",
  "private": true,
  "type": "module",
  "scripts": {
    "build": "tsc -p tsconfig.json",
    "typecheck": "tsc -p tsconfig.json --noEmit",
    "test": "vitest run",
    "start": "node dist/main.js",
    "dev": "tsx src/main.ts"
  },
  "dependencies": {
    "@turing/shared-types": "1.0.0",
    "dotenv": "^16.4.7",
    "pino": "^9.5.0",
    "ulid": "^2.3.0"
  },
  "devDependencies": {
    "tsx": "^4.19.2",
    "vitest": "^2.1.8"
  }
}
```

Create `turing-backend/agent-runtime/tsconfig.json`:

```json
{
  "extends": "../tsconfig.base.json",
  "compilerOptions": {
    "rootDir": "src",
    "outDir": "dist"
  },
  "include": ["src/**/*.ts", "tests/**/*.ts"]
}
```

- [ ] **Step 2: Write failing job loop test**

Create `turing-backend/agent-runtime/tests/jobLoop.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import type { AgentJob } from "@turing/shared-types";
import { runOneJob } from "../src/executor/jobLoop.js";
import type { AgentExecutor } from "../src/executor/types.js";

const job: AgentJob = {
  jobId: "job_1",
  runId: "run_1",
  sessionId: "sess_1",
  userMessageId: "msg_user",
  assistantMessageId: "msg_assistant",
  agentId: "general_assistant",
  traceId: "trace_1",
  modelProvider: "ollama",
  model: "llama3.2",
  payload: { userText: "hello" },
  attempt: 1
};

describe("runOneJob", () => {
  it("posts events and completion from an AgentExecutor", async () => {
    const calls: string[] = [];
    const executor: AgentExecutor = {
      agentId: "general_assistant",
      async *execute() {
        yield { type: "event", event: { sessionId: "sess_1", runId: "run_1", traceId: "trace_1", type: "message.delta", payload: { messageId: "msg_assistant", delta: "hi" } } };
        yield { type: "complete", content: "hi" };
      }
    };

    await runOneJob(job, executor, {
      fetchMessages: async () => [],
      postEvent: async () => calls.push("event"),
      completeRun: async () => calls.push("complete"),
      failRun: async () => calls.push("fail")
    });

    expect(calls).toEqual(["event", "complete"]);
  });
});
```

- [ ] **Step 3: Run test to verify it fails**

Run:

```bash
cd turing-backend
npm test -w @turing/agent-runtime -- jobLoop.test.ts
```

Expected: FAIL because runtime files do not exist.

- [ ] **Step 4: Implement AgentExecutor interface and job loop**

Create `turing-backend/agent-runtime/src/executor/types.ts`:

```ts
import type { AgentExecutionUpdate, AgentJob } from "@turing/shared-types";

export type AgentExecutionContext = {
  messages: Array<{ role: "system" | "user" | "assistant"; content: string }>;
};

export interface AgentExecutor {
  readonly agentId: "general_assistant";
  execute(job: AgentJob, context: AgentExecutionContext): AsyncIterable<AgentExecutionUpdate>;
}
```

Create `turing-backend/agent-runtime/src/executor/jobLoop.ts`:

```ts
import type { AgentJob, ToolCallBeacon, ToolPolicyDecision, TuringEventInput } from "@turing/shared-types";
import type { AgentExecutor } from "./types.js";

export type RuntimeOrchestratorClient = {
  fetchMessages(sessionId: string): Promise<Array<{ role: "system" | "user" | "assistant"; content: string }>>;
  postEvent(runId: string, event: TuringEventInput): Promise<void>;
  completeRun(runId: string, assistantMessageId: string, content: string): Promise<void>;
  failRun(runId: string, error: { code: string; message: string; retryable: boolean }): Promise<void>;
};

export async function runOneJob(job: AgentJob, executor: AgentExecutor, client: RuntimeOrchestratorClient): Promise<void> {
  try {
    const messages = await client.fetchMessages(job.sessionId);
    for await (const update of executor.execute(job, { messages })) {
      if (update.type === "event") await client.postEvent(job.runId, update.event);
      if (update.type === "complete") await client.completeRun(job.runId, job.assistantMessageId, update.content);
      if (update.type === "fail") await client.failRun(job.runId, { code: update.code, message: update.message, retryable: update.retryable });
    }
  } catch (error) {
    await client.failRun(job.runId, {
      code: "runtime_error",
      message: error instanceof Error ? error.message : "Unknown runtime error",
      retryable: false
    });
  }
}
```

- [ ] **Step 5: Implement internal API client**

Create `turing-backend/agent-runtime/src/orchestrator/client.ts`:

```ts
import type { AgentJob, TuringEventInput } from "@turing/shared-types";
import type { RuntimeOrchestratorClient } from "../executor/jobLoop.js";

export class OrchestratorClient implements RuntimeOrchestratorClient {
  constructor(private readonly baseUrl: string, private readonly token: string) {}

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const response = await fetch(`${this.baseUrl}${path}`, {
      ...init,
      headers: {
        "content-type": "application/json",
        authorization: `Bearer ${this.token}`,
        ...init.headers
      }
    });
    if (!response.ok) throw new Error(`Orchestrator request failed: ${response.status} ${path}`);
    return response.status === 204 ? (undefined as T) : ((await response.json()) as T);
  }

  claimNext(agentId: "general_assistant"): Promise<AgentJob | undefined> {
    return this.request<AgentJob | undefined>(`/jobs/next?agent=${agentId}&waitMs=30000`);
  }

  async fetchMessages(sessionId: string) {
    const result = await this.request<{ messages: Array<{ role: "system" | "user" | "assistant"; content: string }> }>(`/sessions/${sessionId}/messages?limit=50`);
    return result.messages;
  }

  async postEvent(runId: string, event: TuringEventInput): Promise<void> {
    await this.request(`/runs/${runId}/events`, { method: "POST", body: JSON.stringify({ event }) });
  }

  async postToolBeacon(runId: string, beacon: ToolCallBeacon): Promise<ToolPolicyDecision> {
    return this.request<ToolPolicyDecision>(`/runs/${runId}/audit/tool-call`, { method: "POST", body: JSON.stringify(beacon) });
  }

  async completeRun(runId: string, assistantMessageId: string, content: string): Promise<void> {
    await this.request(`/runs/${runId}/complete`, { method: "POST", body: JSON.stringify({ assistantMessageId, content }) });
  }

  async failRun(runId: string, error: { code: string; message: string; retryable: boolean }): Promise<void> {
    await this.request(`/runs/${runId}/fail`, { method: "POST", body: JSON.stringify(error) });
  }
}
```

- [ ] **Step 6: Add config, logger, placeholder executor, main loop, and Dockerfile**

Create `turing-backend/agent-runtime/src/config.ts`:

```ts
import { config as loadDotenv } from "dotenv";

export type SecretsBackend = {
  get(name: string): string | undefined;
  require(name: string): string;
};

export class EnvSecretsBackend implements SecretsBackend {
  constructor(private readonly env: NodeJS.ProcessEnv = process.env) {}

  get(name: string): string | undefined {
    const value = this.env[name];
    return value && value.length > 0 ? value : undefined;
  }

  require(name: string): string {
    const value = this.get(name);
    if (!value) throw new Error(`Missing required env var ${name}`);
    return value;
  }
}

export function loadRuntimeConfig(env: NodeJS.ProcessEnv = process.env) {
  loadDotenv();
  const secrets = new EnvSecretsBackend(env);
  return {
    orchestratorInternalBaseUrl: secrets.get("ORCHESTRATOR_INTERNAL_BASE_URL") ?? "http://turing-orchestrator:3001/internal",
    internalToken: secrets.require("TURING_INTERNAL_TOKEN"),
    agentId: "general_assistant" as const,
    ollamaBaseUrl: secrets.get("OLLAMA_BASE_URL") ?? "http://host.docker.internal:11434",
    ollamaModel: secrets.get("OLLAMA_MODEL") ?? "llama3.2",
    openaiBaseUrl: secrets.get("OPENAI_BASE_URL") ?? "https://api.openai.com/v1",
    openaiApiKey: secrets.get("OPENAI_API_KEY"),
    openaiModel: secrets.get("OPENAI_MODEL") ?? "gpt-4o-mini",
    mcpSystemBaseUrl: secrets.get("MCP_SYSTEM_BASE_URL") ?? "http://turing-mcp-system:7100/mcp",
    mcpFilesBaseUrl: secrets.get("MCP_FILES_BASE_URL") ?? "http://turing-mcp-files:7110/mcp",
    mcpSystemToken: secrets.require("MCP_SYSTEM_TOKEN_GENERAL"),
    mcpFilesToken: secrets.require("MCP_FILES_TOKEN_GENERAL"),
    maxConcurrentRuns: Number.parseInt(secrets.get("TURING_MAX_CONCURRENT_RUNS_GENERAL") ?? "1", 10),
    maxToolCallsPerRun: Number.parseInt(secrets.get("TURING_MAX_TOOL_CALLS_PER_RUN") ?? "10", 10),
    modelTimeoutMs: Number.parseInt(secrets.get("TURING_MODEL_TIMEOUT_MS") ?? "120000", 10),
    toolTimeoutMs: Number.parseInt(secrets.get("TURING_TOOL_TIMEOUT_MS") ?? "30000", 10),
    logLevel: secrets.get("LOG_LEVEL") ?? "info"
  };
}
```

Create `src/logging/logger.ts`:

```ts
import pino from "pino";
export function createLogger(level: string) {
  return pino({ level });
}
```

Create `src/agents/generalAssistant.ts`:

```ts
import type { AgentExecutor } from "../executor/types.js";

export function createGeneralAssistantExecutor(): AgentExecutor {
  return {
    agentId: "general_assistant",
    async *execute(job) {
      yield {
        type: "event",
        event: {
          sessionId: job.sessionId,
          runId: job.runId,
          traceId: job.traceId,
          type: "message.delta",
          payload: { messageId: job.assistantMessageId, delta: "Runtime connected. Model streaming arrives in the next task." }
        }
      };
      yield { type: "complete", content: "Runtime connected. Model streaming arrives in the next task." };
    }
  };
}
```

Create `src/main.ts`:

```ts
import { loadRuntimeConfig } from "./config.js";
import { OrchestratorClient } from "./orchestrator/client.js";
import { runOneJob } from "./executor/jobLoop.js";
import { createGeneralAssistantExecutor } from "./agents/generalAssistant.js";
import { createLogger } from "./logging/logger.js";

const config = loadRuntimeConfig();
const logger = createLogger(config.logLevel);
const client = new OrchestratorClient(config.orchestratorInternalBaseUrl, config.internalToken);
const executor = createGeneralAssistantExecutor();

while (true) {
  const job = await client.claimNext(config.agentId);
  if (job) {
    logger.info({ jobId: job.jobId, runId: job.runId }, "claimed job");
    await runOneJob(job, executor, client);
  } else {
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
}
```

Create `turing-backend/agent-runtime/Dockerfile`:

```dockerfile
FROM node:20-alpine AS deps
WORKDIR /app
COPY package.json package-lock.json ./
COPY shared-types/package.json shared-types/package.json
COPY agent-runtime/package.json agent-runtime/package.json
RUN npm ci

FROM deps AS build
COPY tsconfig.base.json ./
COPY shared-types shared-types
COPY agent-runtime agent-runtime
RUN npm run build -w @turing/shared-types && npm run build -w @turing/agent-runtime

FROM node:20-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
COPY --from=deps /app/node_modules ./node_modules
COPY --from=build /app/shared-types/dist ./shared-types/dist
COPY --from=build /app/shared-types/package.json ./shared-types/package.json
COPY --from=build /app/agent-runtime/dist ./agent-runtime/dist
COPY --from=build /app/agent-runtime/package.json ./agent-runtime/package.json
CMD ["node", "agent-runtime/dist/main.js"]
```

- [ ] **Step 7: Run runtime tests**

Run:

```bash
cd turing-backend
npm test -w @turing/agent-runtime -- jobLoop.test.ts
npm run typecheck -w @turing/agent-runtime
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add turing-backend/agent-runtime turing-backend/package-lock.json
git commit -m "feat: add agent runtime job loop"
```

---

### Task 9: LLM provider interface, Ollama streaming, and OpenAI-compatible adapter

**Files:**
- Create: `turing-backend/agent-runtime/src/llm/provider.ts`
- Create: `turing-backend/agent-runtime/src/llm/ollama.ts`
- Create: `turing-backend/agent-runtime/src/llm/openaiCompatible.ts`
- Modify: `turing-backend/agent-runtime/src/agents/generalAssistant.ts`
- Test: `turing-backend/agent-runtime/tests/llm.test.ts`

- [ ] **Step 1: Write failing provider test**

Create `turing-backend/agent-runtime/tests/llm.test.ts`:

```ts
import { describe, expect, it, vi } from "vitest";
import { OllamaProvider } from "../src/llm/ollama.js";

describe("OllamaProvider", () => {
  it("converts Ollama streamed chunks into LlmStreamEvents", async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      body: ReadableStream.from([
        new TextEncoder().encode(JSON.stringify({ message: { content: "hi" }, done: false }) + "\n"),
        new TextEncoder().encode(JSON.stringify({ done: true, done_reason: "stop" }) + "\n")
      ])
    }));

    const provider = new OllamaProvider("http://ollama", fetchMock as unknown as typeof fetch);
    const events = [];
    for await (const event of provider.streamChat({ model: "llama3.2", messages: [{ role: "user", content: "hello" }] })) {
      events.push(event);
    }

    expect(events).toEqual([{ type: "delta", text: "hi" }, { type: "completed", finishReason: "stop" }]);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
cd turing-backend
npm test -w @turing/agent-runtime -- llm.test.ts
```

Expected: FAIL because `OllamaProvider` does not exist.

- [ ] **Step 3: Implement provider interface and Ollama adapter**

Create `turing-backend/agent-runtime/src/llm/provider.ts`:

```ts
import type { LlmChatRequest, LlmStreamEvent } from "@turing/shared-types";

export interface LlmProvider {
  readonly id: "ollama" | "openai_compatible";
  streamChat(request: LlmChatRequest): AsyncIterable<LlmStreamEvent>;
}
```

Create `turing-backend/agent-runtime/src/llm/ollama.ts`:

```ts
import type { LlmChatRequest, LlmStreamEvent } from "@turing/shared-types";
import type { LlmProvider } from "./provider.js";

export class OllamaProvider implements LlmProvider {
  readonly id = "ollama" as const;

  constructor(private readonly baseUrl: string, private readonly fetchImpl: typeof fetch = fetch) {}

  async *streamChat(request: LlmChatRequest): AsyncIterable<LlmStreamEvent> {
    const response = await this.fetchImpl(`${this.baseUrl}/api/chat`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ model: request.model, messages: request.messages, stream: true }),
      signal: request.abortSignal
    });

    if (!response.ok || !response.body) {
      yield { type: "error", code: "model_unavailable", message: `Ollama returned ${response.status}` };
      return;
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split("\n");
      buffer = lines.pop() ?? "";
      for (const line of lines) {
        if (!line.trim()) continue;
        const chunk = JSON.parse(line);
        if (chunk.message?.content) yield { type: "delta", text: chunk.message.content };
        if (chunk.done) yield { type: "completed", finishReason: chunk.done_reason };
      }
    }
  }
}
```

- [ ] **Step 4: Implement OpenAI-compatible adapter**

Create `turing-backend/agent-runtime/src/llm/openaiCompatible.ts`:

```ts
import type { LlmChatRequest, LlmStreamEvent } from "@turing/shared-types";
import type { LlmProvider } from "./provider.js";

export class OpenAICompatibleProvider implements LlmProvider {
  readonly id = "openai_compatible" as const;

  constructor(private readonly baseUrl: string, private readonly apiKey: string, private readonly fetchImpl: typeof fetch = fetch) {}

  async *streamChat(request: LlmChatRequest): AsyncIterable<LlmStreamEvent> {
    const response = await this.fetchImpl(`${this.baseUrl}/chat/completions`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        authorization: `Bearer ${this.apiKey}`
      },
      body: JSON.stringify({ model: request.model, messages: request.messages, stream: true }),
      signal: request.abortSignal
    });

    if (!response.ok || !response.body) {
      yield { type: "error", code: "model_unavailable", message: `OpenAI-compatible provider returned ${response.status}` };
      return;
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split("\n");
      buffer = lines.pop() ?? "";
      for (const line of lines) {
        const trimmed = line.trim();
        if (!trimmed.startsWith("data:")) continue;
        const data = trimmed.slice("data:".length).trim();
        if (data === "[DONE]") {
          yield { type: "completed", finishReason: "stop" };
          continue;
        }
        const chunk = JSON.parse(data);
        const text = chunk.choices?.[0]?.delta?.content;
        if (text) yield { type: "delta", text };
      }
    }
  }
}
```

- [ ] **Step 5: Wire provider into general assistant**

Update `createGeneralAssistantExecutor` to accept providers:

```ts
import type { LlmProvider } from "../llm/provider.js";

export function createGeneralAssistantExecutor(providers: Record<string, LlmProvider>): AgentExecutor {
  return {
    agentId: "general_assistant",
    async *execute(job, context) {
      const provider = providers[job.modelProvider];
      if (!provider) {
        yield { type: "fail", code: "model_provider_unavailable", message: `Provider ${job.modelProvider} is not configured`, retryable: false };
        return;
      }

      let content = "";
      yield {
        type: "event",
        event: { sessionId: job.sessionId, runId: job.runId, traceId: job.traceId, type: "message.started", payload: { messageId: job.assistantMessageId, role: "assistant" } }
      };

      for await (const event of provider.streamChat({ model: job.model, messages: context.messages.concat([{ role: "user", content: job.payload.userText }]) })) {
        if (event.type === "delta") {
          content += event.text;
          yield {
            type: "event",
            event: { sessionId: job.sessionId, runId: job.runId, traceId: job.traceId, type: "message.delta", payload: { messageId: job.assistantMessageId, delta: event.text } }
          };
        }
        if (event.type === "error") {
          yield { type: "fail", code: event.code, message: event.message, retryable: false };
          return;
        }
      }

      yield {
        type: "event",
        event: { sessionId: job.sessionId, runId: job.runId, traceId: job.traceId, type: "message.completed", payload: { messageId: job.assistantMessageId, content } }
      };
      yield { type: "complete", content };
    }
  };
}
```

Update `agent-runtime/src/main.ts` to instantiate providers before constructing the executor:

```ts
import { OllamaProvider } from "./llm/ollama.js";
import { OpenAICompatibleProvider } from "./llm/openaiCompatible.js";

const providers = {
  ollama: new OllamaProvider(config.ollamaBaseUrl),
  ...(config.openaiApiKey
    ? { openai_compatible: new OpenAICompatibleProvider(config.openaiBaseUrl, config.openaiApiKey) }
    : {})
};
const executor = createGeneralAssistantExecutor(providers);
```

- [ ] **Step 6: Run provider tests**

Run:

```bash
cd turing-backend
npm test -w @turing/agent-runtime -- llm.test.ts jobLoop.test.ts
npm run typecheck -w @turing/agent-runtime
```

Expected: PASS. If `jobLoop.test.ts` still constructs `createGeneralAssistantExecutor()` with no providers, update the test executor setup to pass this fake provider:

```ts
const executor = createGeneralAssistantExecutor({
  ollama: {
    async *streamChat() {
      yield { type: "delta" as const, text: "hi" };
      yield { type: "completed" as const, finishReason: "stop" };
    }
  }
});
```

- [ ] **Step 7: Commit**

```bash
git add turing-backend/agent-runtime/src/llm turing-backend/agent-runtime/src/agents turing-backend/agent-runtime/tests
git commit -m "feat: add runtime LLM providers"
```

---

### Task 10: System MCP Go server

**Files:**
- Create: `turing-backend/mcp-system/go.mod`
- Create: `turing-backend/mcp-system/Dockerfile`
- Create: `turing-backend/mcp-system/cmd/server/main.go`
- Create: `turing-backend/mcp-system/internal/auth/auth.go`
- Create: `turing-backend/mcp-system/internal/jsonrpc/jsonrpc.go`
- Create: `turing-backend/mcp-system/internal/tools/system.go`
- Test: `turing-backend/mcp-system/internal/tools/system_test.go`

- [ ] **Step 1: Write failing Go tool test**

Create `turing-backend/mcp-system/internal/tools/system_test.go`:

```go
package tools

import "testing"

func TestCallSystemTime(t *testing.T) {
	result, err := Call("system.time", map[string]any{"timezone": "UTC"})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if result["iso"] == "" {
		t.Fatalf("expected iso timestamp")
	}
}

func TestSystemInfoDoesNotExposeSecrets(t *testing.T) {
	result, err := Call("system.info", map[string]any{})
	if err != nil {
		t.Fatalf("Call returned error: %v", err)
	}
	if _, ok := result["env"]; ok {
		t.Fatalf("system.info must not expose env")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
cd turing-backend/mcp-system
go test ./...
```

Expected: FAIL because `go.mod` and implementation do not exist.

- [ ] **Step 3: Implement system tools**

Create `go.mod`:

```go
module github.com/project-turing/mcp-system

go 1.23
```

Create `internal/tools/system.go`:

```go
package tools

import (
	"errors"
	"runtime"
	"time"
)

func List() []map[string]any {
	return []map[string]any{
		{"name": "system.health", "policy": "safe"},
		{"name": "system.time", "policy": "safe"},
		{"name": "system.echo", "policy": "safe"},
		{"name": "system.info", "policy": "safe"},
	}
}

func Call(name string, args map[string]any) (map[string]any, error) {
	switch name {
	case "system.health":
		return map[string]any{"ok": true, "service": "turing-mcp-system"}, nil
	case "system.time":
		now := time.Now().UTC()
		return map[string]any{"iso": now.Format(time.RFC3339Nano), "unixMs": now.UnixMilli(), "timezone": "UTC"}, nil
	case "system.echo":
		text, _ := args["text"].(string)
		return map[string]any{"text": text}, nil
	case "system.info":
		return map[string]any{"os": runtime.GOOS, "arch": runtime.GOARCH, "runtime": runtime.Version()}, nil
	default:
		return nil, errors.New("unknown tool")
	}
}
```

- [ ] **Step 4: Implement JSON-RPC HTTP server with bearer auth**

Create `internal/auth/auth.go`:

```go
package auth

import "net/http"

func RequireBearer(expected string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if expected == "" || r.Header.Get("Authorization") != "Bearer "+expected {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

Create `internal/jsonrpc/jsonrpc.go`:

```go
package jsonrpc

type Request struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}
```

Create `cmd/server/main.go`:

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/project-turing/mcp-system/internal/auth"
	"github.com/project-turing/mcp-system/internal/jsonrpc"
	"github.com/project-turing/mcp-system/internal/tools"
)

func main() {
	token := os.Getenv("MCP_SYSTEM_TOKEN_GENERAL")
	mux := http.NewServeMux()
	mux.Handle("/mcp", auth.RequireBearer(token, http.HandlerFunc(handleMCP)))
	log.Fatal(http.ListenAndServe(":7100", mux))
}

func handleMCP(w http.ResponseWriter, r *http.Request) {
	var req jsonrpc.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	response := jsonrpc.Response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "tools/list":
		response.Result = map[string]any{"tools": tools.List()}
	case "tools/call":
		name, _ := req.Params["name"].(string)
		args, _ := req.Params["arguments"].(map[string]any)
		result, err := tools.Call(name, args)
		if err != nil {
			response.Error = map[string]any{"code": -32601, "message": err.Error()}
		} else {
			response.Result = result
		}
	default:
		response.Error = map[string]any{"code": -32601, "message": "method not found"}
	}
	_ = json.NewEncoder(w).Encode(response)
}
```

Create `Dockerfile`:

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /app/server ./server
EXPOSE 7100
CMD ["./server"]
```

- [ ] **Step 5: Run Go tests**

Run:

```bash
cd turing-backend/mcp-system
go test ./...
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add turing-backend/mcp-system
git commit -m "feat: add system MCP server"
```

---

### Task 11: Runtime MCP client and system tool beacons

**Files:**
- Create: `turing-backend/agent-runtime/src/mcp/client.ts`
- Create: `turing-backend/agent-runtime/src/audit/beacons.ts`
- Create: `turing-backend/agent-runtime/src/tools/toolRunner.ts`
- Modify: `turing-backend/agent-runtime/src/agents/generalAssistant.ts`
- Modify: `turing-backend/orchestrator/src/internal/routes.ts`
- Modify: `turing-backend/orchestrator/src/tools/policy.ts`
- Test: `turing-backend/agent-runtime/tests/mcpClient.test.ts`
- Test: `turing-backend/agent-runtime/tests/beacons.test.ts`
- Test: `turing-backend/agent-runtime/tests/toolRunner.test.ts`
- Test: `turing-backend/orchestrator/tests/tool-policy.test.ts`

- [ ] **Step 1: Write failing MCP client test**

Create `turing-backend/agent-runtime/tests/mcpClient.test.ts`:

```ts
import { describe, expect, it, vi } from "vitest";
import { McpClient } from "../src/mcp/client.js";

describe("McpClient", () => {
  it("sends bearer-authenticated tools/call JSON-RPC requests", async () => {
    const fetchMock = vi.fn(async () => ({
      ok: true,
      json: async () => ({ jsonrpc: "2.0", id: 1, result: { iso: "now" } })
    }));
    const client = new McpClient("http://mcp:7100/mcp", "token", fetchMock as unknown as typeof fetch);
    const result = await client.callTool("system.time", {});

    expect(result).toEqual({ iso: "now" });
    expect(fetchMock).toHaveBeenCalledWith(
      "http://mcp:7100/mcp",
      expect.objectContaining({ headers: expect.objectContaining({ authorization: "Bearer token" }) })
    );
  });

  it("surfaces MCP HTTP 401 instead of treating it as a tool result", async () => {
    const fetchMock = vi.fn(async () => ({
      ok: false,
      status: 401,
      text: async () => "unauthorized"
    }));
    const client = new McpClient("http://mcp:7110/mcp", "bad-token", fetchMock as unknown as typeof fetch);

    await expect(client.callTool("files.read", { path: "note.txt" })).rejects.toThrow("MCP HTTP 401");
  });
});
```

Create `turing-backend/agent-runtime/tests/beacons.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { postToolBeacon } from "../src/audit/beacons.js";

describe("postToolBeacon", () => {
  it("fails closed when the orchestrator before-beacon request fails", async () => {
    const decision = await postToolBeacon(
      async () => {
        throw new Error("connect ECONNREFUSED");
      },
      { phase: "before", toolCallId: "call_1", agentId: "general_assistant", serverName: "system", toolName: "system.time", runId: "run_1", traceId: "trace_1" }
    );

    expect(decision).toEqual({ decision: "deny", toolCallId: "call_1", reason: "before_beacon_failed" });
  });
});
```

Create `turing-backend/agent-runtime/tests/toolRunner.test.ts`:

```ts
import { describe, expect, it, vi } from "vitest";
import { runAuthorizedMcpTool } from "../src/tools/toolRunner.js";

describe("runAuthorizedMcpTool", () => {
  it("posts before/after beacons around an allowed MCP call", async () => {
    const postBeacon = vi.fn(async (beacon) => ({ decision: "allow" as const, toolCallId: beacon.toolCallId }));
    const mcpClient = { callTool: vi.fn(async () => ({ iso: "now" })) };

    const result = await runAuthorizedMcpTool({
      agentId: "general_assistant",
      runId: "run_1",
      traceId: "trace_1",
      serverName: "system",
      toolName: "system.time",
      args: {},
      mcpClient,
      postBeacon
    });

    expect(result).toEqual({ iso: "now" });
    expect(postBeacon).toHaveBeenCalledWith(expect.objectContaining({ phase: "before", toolName: "system.time" }));
    expect(postBeacon).toHaveBeenCalledWith(expect.objectContaining({ phase: "after", status: "completed" }));
  });
});
```

- [ ] **Step 2: Implement MCP client**

Create `turing-backend/agent-runtime/src/mcp/client.ts`:

```ts
export class McpClient {
  private nextId = 1;

  constructor(private readonly endpoint: string, private readonly token: string, private readonly fetchImpl: typeof fetch = fetch) {}

  async listTools(): Promise<unknown[]> {
    const result = await this.request("tools/list", {});
    return (result as { tools?: unknown[] }).tools ?? [];
  }

  async callTool(name: string, args: Record<string, unknown>, approvalToken?: string): Promise<unknown> {
    return this.request("tools/call", {
      name,
      arguments: args,
      ...(approvalToken ? { _meta: { approvalToken } } : {})
    });
  }

  private async request(method: string, params: Record<string, unknown>): Promise<unknown> {
    const response = await this.fetchImpl(this.endpoint, {
      method: "POST",
      headers: { "content-type": "application/json", authorization: `Bearer ${this.token}` },
      body: JSON.stringify({ jsonrpc: "2.0", id: this.nextId++, method, params })
    });
    if (!response.ok) throw new Error(`MCP HTTP ${response.status}`);
    const payload = await response.json();
    if (payload.error) throw new Error(String(payload.error.message ?? "MCP error"));
    return payload.result;
  }
}
```

Create `turing-backend/agent-runtime/src/tools/toolRunner.ts`:

```ts
import { ulid } from "ulid";
import type { ToolCallBeacon, ToolPolicyDecision } from "@turing/shared-types";

type McpToolClient = {
  callTool(name: string, args: Record<string, unknown>, approvalToken?: string): Promise<unknown>;
};

export async function runAuthorizedMcpTool(input: {
  agentId: "general_assistant";
  runId: string;
  traceId: string;
  serverName: "system" | "files";
  toolName: string;
  args: Record<string, unknown>;
  mcpClient: McpToolClient;
  postBeacon: (beacon: ToolCallBeacon) => Promise<ToolPolicyDecision>;
}): Promise<unknown> {
  const toolCallId = `call_${ulid()}`;
  const before = await input.postBeacon({
    phase: "before",
    toolCallId,
    agentId: input.agentId,
    serverName: input.serverName,
    toolName: input.toolName,
    args: input.args,
    runId: input.runId,
    traceId: input.traceId
  });
  if (before.decision !== "allow") {
    throw new Error(before.decision === "deny" ? before.reason : "approval_required");
  }

  try {
    const result = await input.mcpClient.callTool(input.toolName, input.args);
    await input.postBeacon({
      phase: "after",
      toolCallId,
      agentId: input.agentId,
      serverName: input.serverName,
      toolName: input.toolName,
      args: input.args,
      status: "completed",
      resultSummary: JSON.stringify(result).slice(0, 500),
      runId: input.runId,
      traceId: input.traceId
    });
    return result;
  } catch (error) {
    await input.postBeacon({
      phase: "after",
      toolCallId,
      agentId: input.agentId,
      serverName: input.serverName,
      toolName: input.toolName,
      args: input.args,
      status: "failed",
      error: { code: "mcp_call_failed", message: error instanceof Error ? error.message : "MCP call failed" },
      runId: input.runId,
      traceId: input.traceId
    });
    throw error;
  }
}
```

- [ ] **Step 3: Add orchestrator policy**

Create `turing-backend/orchestrator/src/tools/policy.ts`:

```ts
import type { ToolCallBeacon, ToolPolicy } from "@turing/shared-types";

const POLICIES = new Map<string, "safe" | "approval_required" | "disabled">([
  ["system.health", "safe"],
  ["system.time", "safe"],
  ["system.echo", "safe"],
  ["system.info", "safe"],
  ["files.list", "safe"],
  ["files.search", "safe"],
  ["files.read", "safe"],
  ["files.create", "approval_required"],
  ["files.update", "approval_required"],
  ["files.delete", "disabled"],
  ["files.move", "disabled"]
]);

export function getToolPolicy(toolName: string): ToolPolicy {
  return POLICIES.get(toolName) ?? "disabled";
}
```

Create `turing-backend/orchestrator/tests/tool-policy.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { getToolPolicy } from "../src/tools/policy.js";

describe("tool policy", () => {
  it("classifies safe, approval-required, and disabled tools", () => {
    expect(getToolPolicy("system.time")).toBe("safe");
    expect(getToolPolicy("files.update")).toBe("approval_required");
    expect(getToolPolicy("files.delete")).toBe("disabled");
  });
});
```

- [ ] **Step 4: Use policy in internal beacons**

Modify `internal/routes.ts` before-beacon path:

```ts
import { getToolPolicy } from "../tools/policy.js";

if (request.body.phase === "after") {
  return { decision: "allow", toolCallId: request.body.toolCallId };
}
const policy = getToolPolicy(request.body.toolName);
if (policy === "safe") return { decision: "allow", toolCallId: request.body.toolCallId };
if (policy === "disabled") return { decision: "deny", toolCallId: request.body.toolCallId, reason: "policy_denied" };
return { decision: "deny", toolCallId: request.body.toolCallId, reason: "approval_required" };
```

Task 13 replaces this fail-closed approval-required branch with the real approval creation/polling flow. Until then, approval-required tools are denied rather than allowed with a fake approval ID.

- [ ] **Step 5: Add runtime beacon helper**

Create `turing-backend/agent-runtime/src/audit/beacons.ts`:

```ts
import type { ToolCallBeacon, ToolPolicyDecision } from "@turing/shared-types";

export async function postToolBeacon(
  post: (beacon: ToolCallBeacon) => Promise<ToolPolicyDecision>,
  beacon: ToolCallBeacon
): Promise<ToolPolicyDecision> {
  try {
    return await post(beacon);
  } catch (error) {
    if (beacon.phase === "before") {
      return { decision: "deny", toolCallId: beacon.toolCallId, reason: "before_beacon_failed" };
    }
    throw error;
  }
}
```

- [ ] **Step 6: Run tests**

Run:

```bash
cd turing-backend
npm test -w @turing/agent-runtime -- mcpClient.test.ts
npm test -w @turing/agent-runtime -- beacons.test.ts
npm test -w @turing/orchestrator -- tool-policy.test.ts internal-api.test.ts
npm run typecheck -w @turing/agent-runtime
npm run typecheck -w @turing/orchestrator
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add turing-backend/agent-runtime/src/mcp turing-backend/agent-runtime/src/audit turing-backend/agent-runtime/tests turing-backend/orchestrator/src/tools turing-backend/orchestrator/src/internal turing-backend/orchestrator/tests
git commit -m "feat: add MCP client and tool policy beacons"
```

---

### Task 12: Files MCP server with sandboxing and approval JWT validation

**Files:**
- Create: `turing-backend/mcp-files/go.mod`
- Create: `turing-backend/mcp-files/Dockerfile`
- Create: `turing-backend/mcp-files/cmd/server/main.go`
- Create: `turing-backend/mcp-files/internal/auth/auth.go`
- Create: `turing-backend/mcp-files/internal/approval/jwt.go`
- Create: `turing-backend/mcp-files/internal/jsonrpc/jsonrpc.go`
- Create: `turing-backend/mcp-files/internal/tools/files.go`
- Test: `turing-backend/mcp-files/internal/auth/auth_test.go`
- Test: `turing-backend/mcp-files/internal/approval/jwt_test.go`
- Test: `turing-backend/mcp-files/internal/tools/files_test.go`

- [ ] **Step 1: Write failing files tool tests**

Create `turing-backend/mcp-files/internal/tools/files_test.go`:

```go
package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	_, err := NewFilesTools(root).Read(map[string]any{"path": "../outside.txt"})
	if err == nil {
		t.Fatalf("expected traversal rejection")
	}
}

func TestReadInsideSandbox(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "note.txt")
	if err := os.WriteFile(file, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := NewFilesTools(root).Read(map[string]any{"path": "note.txt"})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if result["content"] != "hello" {
		t.Fatalf("unexpected content: %#v", result)
	}
}

func TestReadRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFilesTools(root).Read(map[string]any{"path": "link.txt"}); err == nil {
		t.Fatalf("expected symlink escape rejection")
	}
}

func TestReadRejectsFileTooLarge(t *testing.T) {
	root := t.TempDir()
	content := strings.Repeat("x", maxReadBytes+1)
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFilesTools(root).Read(map[string]any{"path": "large.txt"}); err == nil {
		t.Fatalf("expected max file size rejection")
	}
}

func TestReadHonorsMaxBytesWithTruncation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := NewFilesTools(root).Read(map[string]any{"path": "note.txt", "maxBytes": float64(3)})
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if result["content"] != "abc" || result["truncated"] != true {
		t.Fatalf("expected truncated content, got %#v", result)
	}
}

func TestReadRejectsBinaryContent(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "binary.bin"), []byte{0xff, 0xfe, 0xfd}, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFilesTools(root).Read(map[string]any{"path": "binary.bin"}); err == nil {
		t.Fatalf("expected binary/invalid UTF-8 rejection")
	}
}

func TestSearchInsideSandbox(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("alpha beta"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "other.txt"), []byte("gamma"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := NewFilesTools(root).Search(map[string]any{"path": ".", "query": "alpha"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	matches := result["matches"].([]map[string]any)
	if len(matches) != 1 || !strings.Contains(matches[0]["path"].(string), "note.txt") {
		t.Fatalf("unexpected matches: %#v", matches)
	}
}

func TestSearchRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("alpha secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	result, err := NewFilesTools(root).Search(map[string]any{"path": ".", "query": "alpha"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(result["matches"].([]map[string]any)) != 0 {
		t.Fatalf("expected symlink escape to be skipped, got %#v", result)
	}
}

func TestCreateAndUpdateRequireValidatedApproval(t *testing.T) {
	root := t.TempDir()
	validator := fakeApprovalValidator{valid: true}
	files := NewFilesTools(root).WithApprovalValidator(validator)

	if _, err := files.Create(map[string]any{"path": "note.txt", "content": "hello"}, "approval-token", "general_assistant"); err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if _, err := files.Update(map[string]any{"path": "note.txt", "content": "updated"}, "approval-token-2", "general_assistant"); err != nil {
		t.Fatalf("update failed: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(root, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "updated" {
		t.Fatalf("expected updated content, got %q", string(content))
	}
}

func TestCreateRejectsApprovalForDifferentArgs(t *testing.T) {
	root := t.TempDir()
	validator := fakeApprovalValidator{valid: false}
	files := NewFilesTools(root).WithApprovalValidator(validator)
	if _, err := files.Create(map[string]any{"path": "note.txt", "content": "hello"}, "bad-token", "general_assistant"); err == nil {
		t.Fatalf("expected approval validation failure")
	}
}

func TestDeleteAndMoveDisabled(t *testing.T) {
	files := NewFilesTools(t.TempDir())
	if _, err := files.Call("files.delete", map[string]any{}, "", "general_assistant"); err == nil {
		t.Fatalf("expected delete to be disabled")
	}
	if _, err := files.Call("files.move", map[string]any{}, "", "general_assistant"); err == nil {
		t.Fatalf("expected move to be disabled")
	}
}

type fakeApprovalValidator struct {
	valid bool
}

func (f fakeApprovalValidator) Validate(token string, tool string, args map[string]any, agentID string) error {
	if f.valid {
		return nil
	}
	return os.ErrPermission
}
```

Create `turing-backend/mcp-files/internal/auth/auth_test.go`:

```go
package auth

import (
	"net/http/httptest"
	"testing"
)

func TestAgentFromBearerRejectsWrongToken(t *testing.T) {
	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	if _, err := AgentFromBearer(req, "expected"); err == nil {
		t.Fatalf("expected 401-equivalent auth error")
	}
}

func TestAgentFromBearerMapsTokenToGeneralAssistant(t *testing.T) {
	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Header.Set("Authorization", "Bearer expected")
	agent, err := AgentFromBearer(req, "expected")
	if err != nil {
		t.Fatalf("unexpected auth error: %v", err)
	}
	if agent != "general_assistant" {
		t.Fatalf("unexpected agent %q", agent)
	}
}
```

Create `turing-backend/mcp-files/internal/approval/jwt_test.go`:

```go
package approval

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func TestValidateChecksClaimsAndConsumesOnce(t *testing.T) {
	consumeCount := 0
	consumer := Consumer{
		OrchestratorBaseURL: "http://orchestrator/internal",
		InternalToken:       "internal",
		JWTSecret:           "secret",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			consumeCount++
			if req.URL.Path != "/internal/approvals/appr_1/consume" || req.Header.Get("Authorization") != "Bearer internal" {
				t.Fatalf("unexpected consume request: %s %s", req.Method, req.URL.Path)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
		})},
	}
	args := map[string]any{"content": "hello", "path": "note.txt"}
	token := signTestToken(t, "secret", Claims{Sub: "general_assistant", Aud: "mcp-files", JTI: "appr_1", Tool: "files.create", ArgsHash: hashArgs(t, args), Exp: time.Now().Add(time.Minute).Unix(), Iat: time.Now().Unix()})

	if err := consumer.Validate(token, "files.create", args, "general_assistant"); err != nil {
		t.Fatalf("expected valid approval: %v", err)
	}
	if consumeCount != 1 {
		t.Fatalf("expected one consume call, got %d", consumeCount)
	}
}

func TestValidateRejectsMismatchedApprovalBinding(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "note.txt"}
	base := Claims{Sub: "general_assistant", Aud: "mcp-files", JTI: "appr_1", Tool: "files.create", ArgsHash: hashArgs(t, args), Exp: time.Now().Add(time.Minute).Unix(), Iat: time.Now().Unix()}
	cases := []struct {
		name   string
		claims Claims
		tool   string
		args   map[string]any
		agent  string
	}{
		{"audience", withClaim(base, func(c *Claims) { c.Aud = "other" }), "files.create", args, "general_assistant"},
		{"subject", withClaim(base, func(c *Claims) { c.Sub = "other_agent" }), "files.create", args, "general_assistant"},
		{"tool", withClaim(base, func(c *Claims) { c.Tool = "files.update" }), "files.create", args, "general_assistant"},
		{"args_hash", base, "files.create", map[string]any{"content": "changed", "path": "note.txt"}, "general_assistant"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			consumer := Consumer{OrchestratorBaseURL: "http://orchestrator/internal", InternalToken: "internal", JWTSecret: "secret", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				t.Fatalf("consume must not be called for invalid binding")
				return nil, nil
			})}}
			if err := consumer.Validate(signTestToken(t, "secret", tc.claims), tc.tool, tc.args, tc.agent); err == nil {
				t.Fatalf("expected validation failure")
			}
		})
	}
}

func TestValidateRejectsConsumeReplayConflict(t *testing.T) {
	args := map[string]any{"content": "hello", "path": "note.txt"}
	consumer := Consumer{OrchestratorBaseURL: "http://orchestrator/internal", InternalToken: "internal", JWTSecret: "secret", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusConflict, Body: http.NoBody}, nil
	})}}
	token := signTestToken(t, "secret", Claims{Sub: "general_assistant", Aud: "mcp-files", JTI: "appr_1", Tool: "files.create", ArgsHash: hashArgs(t, args), Exp: time.Now().Add(time.Minute).Unix(), Iat: time.Now().Unix()})
	if err := consumer.Validate(token, "files.create", args, "general_assistant"); err == nil {
		t.Fatalf("expected replay/consume conflict")
	}
}

func TestCanonicalArgsHashMatchesTypeScriptFixture(t *testing.T) {
	if got := hashArgs(t, map[string]any{"B": float64(1), "a": float64(2)}); got != "sha256:812e5e7fb7bb816dc477e91a136430192eadcf83ff303881298146e106ae0161" {
		t.Fatalf("unexpected canonical hash %s", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func signTestToken(t *testing.T, secret string, claims Claims) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payloadBytes, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
	input := header + "." + payload
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(input))
	return input + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func hashArgs(t *testing.T, args map[string]any) string {
	t.Helper()
	canonical, err := canonicalJSON(args)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func withClaim(claims Claims, mutate func(*Claims)) Claims {
	mutate(&claims)
	return claims
}
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
cd turing-backend/mcp-files
go test ./...
```

Expected: FAIL because files MCP does not exist.

- [ ] **Step 3: Implement sandboxed files tools**

Create `go.mod`:

```go
module github.com/project-turing/mcp-files

go 1.23
```

Create `internal/tools/files.go`:

```go
package tools

import (
	"bytes"
	"errors"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const defaultReadBytes = 64 * 1024
const maxReadBytes = 512 * 1024
const defaultSearchResults = 50
const maxSearchResults = 200

type ApprovalValidator interface {
	Validate(token string, tool string, args map[string]any, agentID string) error
}

type FilesTools struct {
	root      string
	validator ApprovalValidator
}

func NewFilesTools(root string) FilesTools {
	abs, _ := filepath.Abs(root)
	return FilesTools{root: abs}
}

func (f FilesTools) WithApprovalValidator(validator ApprovalValidator) FilesTools {
	f.validator = validator
	return f
}

func (f FilesTools) resolve(input string) (string, error) {
	clean := filepath.Clean(strings.TrimPrefix(input, "/"))
	full := filepath.Join(f.root, clean)
	existing := full
	missing := []string{}
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", errors.New("path escapes sandbox")
		}
		missing = append([]string{filepath.Base(existing)}, missing...)
		existing = parent
	}
	resolvedExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	resolved := filepath.Join(append([]string{resolvedExisting}, missing...)...)
	rel, err := filepath.Rel(f.root, resolved)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", errors.New("path escapes sandbox")
	}
	return resolved, nil
}

func (f FilesTools) Read(args map[string]any) (map[string]any, error) {
	pathValue, _ := args["path"].(string)
	limit := readLimit(args)
	full, err := f.resolve(pathValue)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxReadBytes {
		return nil, errors.New("file too large")
	}
	content, err := os.ReadFile(full)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(content) {
		return nil, errors.New("unsupported media type")
	}
	truncated := len(content) > limit
	if truncated {
		content = content[:limit]
		for !utf8.Valid(content) && len(content) > 0 {
			content = content[:len(content)-1]
		}
	}
	return map[string]any{"path": pathValue, "content": string(content), "truncated": truncated}, nil
}

func (f FilesTools) List(args map[string]any) (map[string]any, error) {
	pathValue, _ := args["path"].(string)
	full, err := f.resolve(pathValue)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, err
	}
	items := []map[string]any{}
	for _, entry := range entries {
		items = append(items, map[string]any{"name": entry.Name(), "isDir": entry.IsDir()})
	}
	return map[string]any{"items": items}, nil
}

func (f FilesTools) Search(args map[string]any) (map[string]any, error) {
	pathValue, _ := args["path"].(string)
	query, _ := args["query"].(string)
	if query == "" {
		return nil, errors.New("query is required")
	}
	limit := searchLimit(args)
	full, err := f.resolve(pathValue)
	if err != nil {
		return nil, err
	}
	matches := []map[string]any{}
	err = filepath.WalkDir(full, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if len(matches) >= limit {
			return filepath.SkipAll
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(f.root, resolved)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > maxReadBytes {
			return nil
		}
		content, err := os.ReadFile(resolved)
		if err != nil {
			return nil
		}
		if !utf8.Valid(content) {
			return nil
		}
		text := string(content)
		if strings.Contains(text, query) {
			matches = append(matches, map[string]any{"path": rel, "snippet": firstSnippet(text, query)})
		}
		return nil
	})
	return map[string]any{"matches": matches}, err
}

func (f FilesTools) Create(args map[string]any, approvalToken string, agentID string) (map[string]any, error) {
	if err := f.validateApproval("files.create", args, approvalToken, agentID); err != nil {
		return nil, err
	}
	pathValue, _ := args["path"].(string)
	content, _ := args["content"].(string)
	full, err := f.resolve(pathValue)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(full); err == nil {
		return nil, errors.New("file already exists")
	}
	if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		return nil, err
	}
	return map[string]any{"path": pathValue, "sha256": contentHash(content)}, nil
}

func (f FilesTools) Update(args map[string]any, approvalToken string, agentID string) (map[string]any, error) {
	if err := f.validateApproval("files.update", args, approvalToken, agentID); err != nil {
		return nil, err
	}
	pathValue, _ := args["path"].(string)
	content, _ := args["content"].(string)
	full, err := f.resolve(pathValue)
	if err != nil {
		return nil, err
	}
	if expectedHash, ok := args["expectedHash"].(string); ok && expectedHash != "" {
		current, err := os.ReadFile(full)
		if err != nil {
			return nil, err
		}
		if contentHash(string(current)) != expectedHash {
			return nil, errors.New("expectedHash mismatch")
		}
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		return nil, err
	}
	return map[string]any{"path": pathValue, "sha256": contentHash(content)}, nil
}

func (f FilesTools) Call(name string, args map[string]any, approvalToken string, agentID string) (map[string]any, error) {
	switch name {
	case "files.list":
		return f.List(args)
	case "files.search":
		return f.Search(args)
	case "files.read":
		return f.Read(args)
	case "files.create":
		return f.Create(args, approvalToken, agentID)
	case "files.update":
		return f.Update(args, approvalToken, agentID)
	case "files.delete", "files.move":
		return nil, errors.New("tool disabled")
	default:
		return nil, errors.New("unknown tool")
	}
}

func (f FilesTools) validateApproval(tool string, args map[string]any, approvalToken string, agentID string) error {
	if f.validator == nil {
		return errors.New("approval validator not configured")
	}
	if approvalToken == "" {
		return errors.New("approval token required")
	}
	return f.validator.Validate(approvalToken, tool, args, agentID)
}

func readLimit(args map[string]any) int {
	if value, ok := args["maxBytes"].(float64); ok && value > 0 {
		if int(value) > maxReadBytes {
			return maxReadBytes
		}
		return int(value)
	}
	return defaultReadBytes
}

func searchLimit(args map[string]any) int {
	if value, ok := args["limit"].(float64); ok && value > 0 {
		if int(value) > maxSearchResults {
			return maxSearchResults
		}
		return int(value)
	}
	return defaultSearchResults
}

func firstSnippet(text string, query string) string {
	index := strings.Index(text, query)
	if index < 0 {
		return ""
	}
	start := index - 40
	if start < 0 {
		start = 0
	}
	end := index + len(query) + 40
	if end > len(text) {
		end = len(text)
	}
	return text[start:end]
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func CanonicalArgsHash(args map[string]any) (string, error) {
	canonical, err := canonicalJSON(args)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func canonicalJSON(args map[string]any) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(args); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}
```

- [ ] **Step 4: Implement approval JWT verifier**

Use HMAC-SHA256 and verify `exp`, `aud`, `sub`, `tool`, and `args_hash`.

Create `internal/approval/jwt.go`:

```go
package approval

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Claims struct {
	Iss      string `json:"iss"`
	Sub      string `json:"sub"`
	Aud      string `json:"aud"`
	JTI      string `json:"jti"`
	Tool     string `json:"tool"`
	ArgsHash string `json:"args_hash"`
	Exp      int64  `json:"exp"`
	Iat      int64  `json:"iat"`
}

func VerifyHS256(token string, secret string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("invalid token")
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, err
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return Claims{}, err
	}
	if header.Alg != "HS256" {
		return Claims{}, errors.New("invalid token algorithm")
	}
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signingInput))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[2])) {
		return Claims{}, errors.New("invalid signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, err
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, err
	}
	if claims.Exp < time.Now().Unix() {
		return Claims{}, errors.New("token expired")
	}
	return claims, nil
}

type Consumer struct {
	OrchestratorBaseURL string
	InternalToken       string
	JWTSecret           string
	HTTPClient          *http.Client
}

func (c Consumer) Validate(token string, tool string, args map[string]any, agentID string) error {
	claims, err := VerifyHS256(token, c.JWTSecret)
	if err != nil {
		return err
	}
	if claims.Aud != "mcp-files" {
		return errors.New("invalid approval audience")
	}
	if claims.Sub != agentID {
		return errors.New("approval subject does not match agent")
	}
	if claims.Tool != tool {
		return errors.New("approval tool does not match call")
	}
	argsHash, err := canonicalArgsHash(args)
	if err != nil {
		return err
	}
	if claims.ArgsHash != argsHash {
		return errors.New("approval args_hash does not match call")
	}
	return c.consume(claims.JTI)
}

func (c Consumer) consume(jti string) error {
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/approvals/%s/consume", strings.TrimRight(c.OrchestratorBaseURL, "/"), jti), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.InternalToken)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode == http.StatusConflict {
		return errors.New("approval already consumed or not approved")
	}
	return fmt.Errorf("approval consume failed: HTTP %d", resp.StatusCode)
}

func canonicalArgsHash(args map[string]any) (string, error) {
	canonical, err := canonicalJSON(args)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(canonical))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func canonicalJSON(args map[string]any) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(args); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}
```

- [ ] **Step 5: Implement HTTP JSON-RPC server**

Create `internal/auth/auth.go` so MCP bearer authentication returns the agent identity that approval JWT validation must match:

```go
package auth

import (
	"errors"
	"net/http"
)

func AgentFromBearer(r *http.Request, systemToken string) (string, error) {
	if systemToken == "" || r.Header.Get("Authorization") != "Bearer "+systemToken {
		return "", errors.New("unauthorized")
	}
	// v1.0 has one runtime/MCP token for the general assistant; v1.1 should replace this with a token-to-agent map.
	return "general_assistant", nil
}
```

Create `internal/jsonrpc/jsonrpc.go`:

```go
package jsonrpc

type Request struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

type Response struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      any            `json:"id"`
	Result  any            `json:"result,omitempty"`
	Error   map[string]any `json:"error,omitempty"`
}
```

Create `cmd/server/main.go`:

```go
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	approval "github.com/project-turing/mcp-files/internal/approval"
	"github.com/project-turing/mcp-files/internal/auth"
	"github.com/project-turing/mcp-files/internal/jsonrpc"
	filetools "github.com/project-turing/mcp-files/internal/tools"
)

func main() {
	validator := approval.Consumer{
		OrchestratorBaseURL: getenv("ORCHESTRATOR_INTERNAL_BASE_URL", "http://turing-orchestrator:3001/internal"),
		InternalToken:       os.Getenv("TURING_INTERNAL_TOKEN"),
		JWTSecret:           os.Getenv("TURING_APPROVAL_JWT_SECRET"),
	}
	tools := filetools.NewFilesTools(getenv("FILES_SANDBOX_ROOT", "/sandbox")).WithApprovalValidator(validator)
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		agentID, err := auth.AgentFromBearer(r, os.Getenv("MCP_FILES_TOKEN_GENERAL"))
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handleMCP(w, r, tools, agentID)
	})
	log.Fatal(http.ListenAndServe(":7110", mux))
}

func handleMCP(w http.ResponseWriter, r *http.Request, tools filetools.FilesTools, agentID string) {
	var req jsonrpc.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	response := jsonrpc.Response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "tools/list":
		response.Result = map[string]any{"tools": []map[string]any{
			{"name": "files.list", "policy": "safe"},
			{"name": "files.search", "policy": "safe"},
			{"name": "files.read", "policy": "safe"},
			{"name": "files.create", "policy": "approval_required"},
			{"name": "files.update", "policy": "approval_required"},
			{"name": "files.delete", "policy": "disabled"},
			{"name": "files.move", "policy": "disabled"},
		}}
	case "tools/call":
		name, _ := req.Params["name"].(string)
		args, _ := req.Params["arguments"].(map[string]any)
		meta, _ := req.Params["_meta"].(map[string]any)
		approvalToken, _ := meta["approvalToken"].(string)
		result, err := tools.Call(name, args, approvalToken, agentID)
		if err != nil {
			response.Error = map[string]any{"code": -32000, "message": err.Error()}
		} else {
			response.Result = result
		}
	default:
		response.Error = map[string]any{"code": -32601, "message": "method not found"}
	}
	_ = json.NewEncoder(w).Encode(response)
}

func getenv(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
```

This handler must validate all approval-bound calls before writes: `files.create` and `files.update` require `_meta.approvalToken`; `approval.Consumer.Validate` verifies signature, `exp`, `aud == "mcp-files"`, `sub == agentID`, `tool == params.name`, and `args_hash == sha256(canonical JSON arguments)`, then calls the orchestrator consume endpoint. The write happens only after consume succeeds. `files.delete` and `files.move` return typed JSON-RPC errors.

- [ ] **Step 6: Add Dockerfile**

Create `turing-backend/mcp-files/Dockerfile`:

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

FROM alpine:3.20
WORKDIR /app
COPY --from=builder /app/server ./server
EXPOSE 7110
CMD ["./server"]
```

- [ ] **Step 7: Run files MCP tests**

Run:

```bash
cd turing-backend/mcp-files
go test ./...
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add turing-backend/mcp-files
git commit -m "feat: add files MCP sandbox"
```

---

### Task 13: Approval records, approval JWT signing, and approval REST flow

**Files:**
- Create: `turing-backend/orchestrator/src/approvals/service.ts`
- Create: `turing-backend/agent-runtime/src/approvals/approvalPolling.ts`
- Modify: `turing-backend/orchestrator/src/api/routes.ts`
- Modify: `turing-backend/orchestrator/src/internal/routes.ts`
- Modify: `turing-backend/orchestrator/src/tools/policy.ts`
- Modify: `turing-backend/agent-runtime/src/orchestrator/client.ts`
- Modify: `turing-backend/agent-runtime/src/audit/beacons.ts`
- Test: `turing-backend/orchestrator/tests/approvals.test.ts`
- Test: `turing-backend/agent-runtime/tests/approvalPolling.test.ts`

- [ ] **Step 1: Write failing approval service test**

Create `turing-backend/orchestrator/tests/approvals.test.ts`:

```ts
import Database from "better-sqlite3";
import { describe, expect, it } from "vitest";
import { applyMigrations } from "../src/db/migrations.js";
import { createApprovalsService, stableArgsHash } from "../src/approvals/service.js";
import { seedQueuedJob } from "./testServer.js";

describe("approvals", () => {
  it("creates, emits approval.requested, approves, signs, and consumes an approval once", async () => {
    const db = new Database(":memory:");
    applyMigrations(db);
    const seeded = seedQueuedJob(db);
    const approvals = createApprovalsService(db, "secret");

    const created = approvals.createApproval({
      runId: seeded.runId,
      agentId: "general_assistant",
      toolName: "files.update",
      args: { path: "note.txt", content: "hello" }
    });
    expect((db.prepare("SELECT status FROM agent_runs WHERE id = ?").get(seeded.runId) as { status: string }).status).toBe("waiting_approval");
    const approvalEvent = db.prepare("SELECT type, payload_json FROM events WHERE session_id = ? AND type = 'approval.requested'").get(seeded.sessionId) as { type: string; payload_json: string };
    expect(approvalEvent.type).toBe("approval.requested");
    expect(JSON.parse(approvalEvent.payload_json)).toMatchObject({ approvalId: created.approvalId, toolName: "files.update" });

    const approved = await approvals.approve(created.approvalId);
    const runtimeApproval = approvals.getForRuntime(created.approvalId);

    expect(approved).toMatchObject({ approvalId: created.approvalId, status: "approved", event: expect.objectContaining({ type: "approval.approved" }) });
    expect(db.prepare("SELECT action FROM audit_logs WHERE action = 'approval.approved'").get()).toMatchObject({ action: "approval.approved" });
    expect(runtimeApproval.approvalToken).toContain(".");
    expect((db.prepare("SELECT status FROM agent_runs WHERE id = ?").get(seeded.runId) as { status: string }).status).toBe("running");
    expect(stableArgsHash({ path: "note.txt", content: "hello" })).toMatch(/^sha256:/);
    expect(approvals.consume(created.approvalId)).toMatchObject({ approvalId: created.approvalId, status: "consumed", event: expect.objectContaining({ type: "approval.consumed" }) });
    expect(approvals.get(created.approvalId).status).toBe("consumed");
    expect(() => approvals.consume(created.approvalId)).toThrow(/not approved/);
  });

  it("reuses an existing approval for repeated before-beacons for the same tool call", () => {
    const db = new Database(":memory:");
    applyMigrations(db);
    const seeded = seedQueuedJob(db);
    const approvals = createApprovalsService(db, "secret");
    const input = {
      runId: seeded.runId,
      toolCallId: "call_1",
      agentId: "general_assistant",
      toolName: "files.update",
      args: { path: "note.txt", content: "hello" }
    };

    const first = approvals.createApproval(input);
    const second = approvals.createApproval(input);

    expect(second.approvalId).toBe(first.approvalId);
    expect(db.prepare("SELECT COUNT(*) AS count FROM approvals WHERE tool_call_id = 'call_1'").get()).toMatchObject({ count: 1 });
  });

  it("canonicalizes args with byte-order keys and rejects undefined values", () => {
    expect(stableArgsHash({ B: 1, a: 2 })).toBe("sha256:812e5e7fb7bb816dc477e91a136430192eadcf83ff303881298146e106ae0161");
    expect(() => stableArgsHash({ path: "note.txt", content: undefined })).toThrow(/undefined/);
  });

  it("expires pending approvals", () => {
    const db = new Database(":memory:");
    applyMigrations(db);
    const seeded = seedQueuedJob(db);
    const approvals = createApprovalsService(db, "secret");
    const created = approvals.createApproval({
      runId: seeded.runId,
      agentId: "general_assistant",
      toolName: "files.update",
      args: { path: "note.txt", content: "hello" }
    });
    db.prepare("UPDATE approvals SET expires_at = ? WHERE id = ?").run("2000-01-01T00:00:00.000Z", created.approvalId);
    expect(approvals.expirePendingApprovals(new Date("2000-01-01T00:00:01.000Z"))).toEqual([expect.objectContaining({ type: "approval.expired" })]);
    expect(approvals.get(created.approvalId).status).toBe("expired");
    expect(db.prepare("SELECT action FROM audit_logs WHERE action = 'approval.expired'").get()).toMatchObject({ action: "approval.expired" });
  });

  it("denies approvals with a durable event and audit entry", () => {
    const db = new Database(":memory:");
    applyMigrations(db);
    const seeded = seedQueuedJob(db);
    const approvals = createApprovalsService(db, "secret");
    const created = approvals.createApproval({
      runId: seeded.runId,
      agentId: "general_assistant",
      toolName: "files.update",
      args: { path: "note.txt", content: "hello" }
    });

    const denied = approvals.deny(created.approvalId);

    expect(denied).toMatchObject({ approvalId: created.approvalId, status: "denied", event: expect.objectContaining({ type: "approval.denied" }) });
    expect(db.prepare("SELECT action FROM audit_logs WHERE action = 'approval.denied'").get()).toMatchObject({ action: "approval.denied" });
  });

  it("does not approve an expired approval", async () => {
    const db = new Database(":memory:");
    applyMigrations(db);
    const seeded = seedQueuedJob(db);
    const approvals = createApprovalsService(db, "secret");
    const created = approvals.createApproval({
      runId: seeded.runId,
      agentId: "general_assistant",
      toolName: "files.update",
      args: { path: "note.txt", content: "hello" }
    });
    db.prepare("UPDATE approvals SET expires_at = ? WHERE id = ?").run("2000-01-01T00:00:00.000Z", created.approvalId);
    await expect(approvals.approve(created.approvalId)).rejects.toThrow(/expired/);
    expect(approvals.get(created.approvalId).status).toBe("expired");
  });
});
```

Create `turing-backend/agent-runtime/tests/approvalPolling.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { waitForApprovalToken } from "../src/approvals/approvalPolling.js";

describe("waitForApprovalToken", () => {
  it("returns the stored approval token once the user approves", async () => {
    const states = [
      { approvalId: "appr_1", status: "pending" as const },
      { approvalId: "appr_1", status: "approved" as const, approvalToken: "jwt-token" }
    ];

    const token = await waitForApprovalToken("appr_1", async () => states.shift()!, { pollMs: 1, timeoutMs: 100 });

    expect(token).toBe("jwt-token");
  });

  it("fails when the approval is denied or expires", async () => {
    await expect(
      waitForApprovalToken("appr_1", async () => ({ approvalId: "appr_1", status: "expired" }), { pollMs: 1, timeoutMs: 100 })
    ).rejects.toThrow("Approval expired");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run:

```bash
cd turing-backend
npm test -w @turing/orchestrator -- approvals.test.ts
npm test -w @turing/agent-runtime -- approvalPolling.test.ts
```

Expected: FAIL because approvals service is missing.

- [ ] **Step 3: Implement approval service**

Create `turing-backend/orchestrator/src/approvals/service.ts`:

```ts
import { Buffer } from "node:buffer";
import { createHash } from "node:crypto";
import { SignJWT } from "jose";
import { ulid } from "ulid";
import type { TuringDatabase } from "../db/connection.js";
import { createAuditService } from "../audit/service.js";
import { createEventsService } from "../events/service.js";

export function canonicalJson(value: unknown): string {
  if (value === undefined) throw new Error("Cannot canonicalize undefined value");
  if (value === null || typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(canonicalJson).join(",")}]`;
  const entries = Object.entries(value as Record<string, unknown>).sort(([a], [b]) => Buffer.compare(Buffer.from(a), Buffer.from(b)));
  return `{${entries.map(([key, item]) => {
    if (item === undefined) throw new Error(`Cannot canonicalize undefined value for key ${key}`);
    return `${JSON.stringify(key)}:${canonicalJson(item)}`;
  }).join(",")}}`;
}

export function stableArgsHash(args: unknown): string {
  return `sha256:${createHash("sha256").update(canonicalJson(args)).digest("hex")}`;
}

export function createApprovalsService(db: TuringDatabase, jwtSecret: string) {
  const events = createEventsService(db);
  const audit = createAuditService(db);

  return {
    createApproval(input: { runId: string; toolCallId?: string; agentId: string; toolName: string; args: Record<string, unknown> }) {
      if (input.toolCallId) {
        const existing = db.prepare("SELECT id, status FROM approvals WHERE tool_call_id = ?").get(input.toolCallId) as { id: string; status: string } | undefined;
        if (existing) return { approvalId: existing.id, status: existing.status as "pending" | "approved" | "denied" | "expired" | "consumed", event: undefined };
      }
      const approvalId = `appr_${ulid()}`;
      const now = new Date();
      const expiresAt = new Date(now.getTime() + 60000).toISOString();
      const run = db.prepare("SELECT session_id, trace_id FROM agent_runs WHERE id = ?").get(input.runId) as { session_id: string; trace_id: string } | undefined;
      if (!run) throw new Error("Run not found");
      let requestedEvent: ReturnType<typeof events.append> | undefined;
      const tx = db.transaction(() => {
        db.prepare("INSERT INTO approvals (id, run_id, tool_call_id, agent_id, tool_name, args_json, args_hash, status, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)").run(
          approvalId,
          input.runId,
          input.toolCallId ?? null,
          input.agentId,
          input.toolName,
          JSON.stringify(input.args),
          stableArgsHash(input.args),
          expiresAt,
          now.toISOString()
        );
        db.prepare("UPDATE agent_runs SET status = 'waiting_approval' WHERE id = ?").run(input.runId);
        requestedEvent = events.append({
          sessionId: run.session_id,
          runId: input.runId,
          traceId: run.trace_id,
          type: "approval.requested",
          payload: {
            approvalId,
            toolName: input.toolName,
            argsSummary: summarizeArgs(input.args)
          }
        });
      });
      tx();
      return { approvalId, status: "pending" as const, event: requestedEvent };
    },

    get(approvalId: string) {
      return db.prepare("SELECT * FROM approvals WHERE id = ?").get(approvalId) as any;
    },

    getForRuntime(approvalId: string) {
      const approval = this.get(approvalId);
      if (!approval) return undefined;
      return {
        approvalId,
        status: approval.status as "pending" | "approved" | "denied" | "expired" | "consumed",
        approvalToken: approval.status === "approved" ? approval.approval_token : undefined
      };
    },

    async approve(approvalId: string) {
      const approval = this.get(approvalId);
      if (!approval || approval.status !== "pending") throw new Error("Approval is not pending");
      const run = db.prepare("SELECT session_id, trace_id FROM agent_runs WHERE id = ?").get(approval.run_id) as { session_id: string; trace_id: string };
      if (Date.parse(approval.expires_at) <= Date.now()) {
        const decidedAt = new Date().toISOString();
        db.prepare("UPDATE approvals SET status = 'expired', decided_at = ? WHERE id = ? AND status = 'pending'").run(decidedAt, approvalId);
        db.prepare("UPDATE agent_runs SET status = 'failed', error_code = 'approval_expired', error_message = 'Approval expired', finished_at = ? WHERE id = ? AND status = 'waiting_approval'").run(decidedAt, approval.run_id);
        events.append({ sessionId: run.session_id, runId: approval.run_id, traceId: run.trace_id, type: "approval.expired", payload: { approvalId, toolName: approval.tool_name } });
        audit.record({ correlationId: approval.run_id, actorType: "system", action: "approval.expired", target: approvalId, payload: { toolName: approval.tool_name } });
        throw new Error("Approval expired");
      }
      const secret = new TextEncoder().encode(jwtSecret);
      const exp = Math.floor(Date.now() / 1000) + 60;
      const token = await new SignJWT({
        tool: approval.tool_name,
        args_hash: approval.args_hash
      })
        .setProtectedHeader({ alg: "HS256" })
        .setIssuer("turing.orchestrator")
        .setSubject(approval.agent_id)
        .setAudience("mcp-files")
        .setJti(approval.id)
        .setIssuedAt()
        .setExpirationTime(exp)
        .sign(secret);
      const decidedAt = new Date().toISOString();
      const tx = db.transaction(() => {
        db.prepare("UPDATE approvals SET status = 'approved', approval_jti = ?, approval_token = ?, decided_at = ? WHERE id = ?").run(approval.id, token, decidedAt, approval.id);
        db.prepare("UPDATE agent_runs SET status = 'running' WHERE id = ? AND status = 'waiting_approval'").run(approval.run_id);
      });
      tx();
      const event = events.append({ sessionId: run.session_id, runId: approval.run_id, traceId: run.trace_id, type: "approval.approved", payload: { approvalId, toolName: approval.tool_name } });
      audit.record({ correlationId: approval.run_id, actorType: "client", action: "approval.approved", target: approvalId, payload: { toolName: approval.tool_name } });
      return { approvalId, status: "approved" as const, event };
    },

    deny(approvalId: string) {
      const approval = this.get(approvalId);
      if (!approval || approval.status !== "pending") throw new Error("Approval is not pending");
      const run = db.prepare("SELECT session_id, trace_id FROM agent_runs WHERE id = ?").get(approval.run_id) as { session_id: string; trace_id: string };
      const decidedAt = new Date().toISOString();
      const tx = db.transaction(() => {
        db.prepare("UPDATE approvals SET status = 'denied', decided_at = ? WHERE id = ? AND status = 'pending'").run(decidedAt, approvalId);
        db.prepare("UPDATE agent_runs SET status = 'failed', error_code = 'approval_denied', error_message = 'User denied approval', finished_at = ? WHERE id = ?").run(decidedAt, approval.run_id);
      });
      tx();
      const event = events.append({ sessionId: run.session_id, runId: approval.run_id, traceId: run.trace_id, type: "approval.denied", payload: { approvalId, toolName: approval.tool_name } });
      audit.record({ correlationId: approval.run_id, actorType: "client", action: "approval.denied", target: approvalId, payload: { toolName: approval.tool_name } });
      return { approvalId, status: "denied" as const, event };
    },

    consume(approvalId: string) {
      const approval = this.get(approvalId);
      if (!approval) throw new Error("Approval is not approved");
      const run = db.prepare("SELECT session_id, trace_id FROM agent_runs WHERE id = ?").get(approval.run_id) as { session_id: string; trace_id: string };
      const result = db.prepare("UPDATE approvals SET status = 'consumed', consumed_at = ? WHERE id = ? AND status = 'approved'").run(new Date().toISOString(), approvalId);
      if (result.changes !== 1) throw new Error("Approval is not approved");
      const event = events.append({ sessionId: run.session_id, runId: approval.run_id, traceId: run.trace_id, type: "approval.consumed", payload: { approvalId, toolName: approval.tool_name } });
      audit.record({ correlationId: approval.run_id, actorType: "mcp", action: "approval.consumed", target: approvalId, payload: { toolName: approval.tool_name } });
      return { approvalId, status: "consumed" as const, event };
    },

    expirePendingApprovals(now = new Date()) {
      const expired = db.prepare("SELECT id, run_id, tool_name FROM approvals WHERE status = 'pending' AND expires_at <= ?").all(now.toISOString()) as Array<{ id: string; run_id: string; tool_name: string }>;
      const expiredEvents: ReturnType<typeof events.append>[] = [];
      const tx = db.transaction(() => {
        for (const approval of expired) {
          db.prepare("UPDATE approvals SET status = 'expired', decided_at = ? WHERE id = ? AND status = 'pending'").run(now.toISOString(), approval.id);
          db.prepare("UPDATE agent_runs SET status = 'failed', error_code = 'approval_expired', error_message = 'Approval expired', finished_at = ? WHERE id = ? AND status = 'waiting_approval'").run(now.toISOString(), approval.run_id);
          const run = db.prepare("SELECT session_id, trace_id FROM agent_runs WHERE id = ?").get(approval.run_id) as { session_id: string; trace_id: string };
          expiredEvents.push(events.append({ sessionId: run.session_id, runId: approval.run_id, traceId: run.trace_id, type: "approval.expired", payload: { approvalId: approval.id, toolName: approval.tool_name } }));
          audit.record({ correlationId: approval.run_id, actorType: "system", action: "approval.expired", target: approval.id, payload: { toolName: approval.tool_name } });
        }
      });
      tx();
      return expiredEvents;
    }
  };
}

function summarizeArgs(args: Record<string, unknown>): string {
  const path = typeof args.path === "string" ? args.path : "unknown path";
  return `Requested change to ${path}`;
}
```

- [ ] **Step 4: Add shared maintenance sweep helper**

Create `turing-backend/orchestrator/src/maintenance/sweeps.ts` so server startup has one concrete place for stale-job reaping and approval expiration:

```ts
import type { OrchestratorConfig } from "../config.js";
import type { TuringDatabase } from "../db/connection.js";
import { createApprovalsService } from "../approvals/service.js";
import { createJobsService } from "../jobs/service.js";

type BroadcastHub = { broadcast(event: unknown): void };

export function startSweeps(deps: { db: TuringDatabase; config: OrchestratorConfig; hub?: BroadcastHub }) {
  const approvals = createApprovalsService(deps.db, deps.config.approvalJwtSecret);
  const jobs = createJobsService(deps.db, { jobTimeoutMs: deps.config.jobTimeoutMs, maxAttempts: deps.config.jobMaxAttempts });
  const timer = setInterval(() => {
    jobs.reapStaleJobs();
    const expiredEvents = approvals.expirePendingApprovals();
    for (const event of expiredEvents) deps.hub?.broadcast(event);
  }, deps.config.jobReaperIntervalMs);

  timer.unref?.();
  return { stop: () => clearInterval(timer) };
}
```

- [ ] **Step 5: Wire public approval routes**

In `api/routes.ts`:

```ts
import { createApprovalsService } from "../approvals/service.js";

const approvals = createApprovalsService(deps.db, deps.config.approvalJwtSecret);

app.post<{ Params: { approvalId: string } }>("/api/approvals/:approvalId/approve", async (request) => {
  const approved = await approvals.approve(request.params.approvalId);
  if (approved.event) deps.hub?.broadcast(approved.event);
  return approved;
});

app.post<{ Params: { approvalId: string } }>("/api/approvals/:approvalId/deny", async (request) => {
  const denied = approvals.deny(request.params.approvalId);
  if (denied.event) deps.hub?.broadcast(denied.event);
  return denied;
});
```

- [ ] **Step 6: Wire beacon-created approvals, consume endpoint, and polling**

In `internal/routes.ts`, implement approvals only through the before-tool-call beacon path. Do not add a separate `/internal/runs/:runId/approval-request` endpoint for v1.0; keeping one creation path prevents duplicate pending approvals for the same tool call.

```ts
import { getToolPolicy } from "../tools/policy.js";

app.post<{ Params: { runId: string }; Body: ToolCallBeacon }>("/internal/runs/:runId/audit/tool-call", async (request) => {
  if (request.body.phase === "before") {
    deps.db.prepare("INSERT OR IGNORE INTO tool_calls (id, run_id, agent_id, server_name, tool_name, args_json, args_hash, status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, 'requested', ?)").run(
      request.body.toolCallId,
      request.params.runId,
      request.body.agentId,
      request.body.serverName,
      request.body.toolName,
      JSON.stringify(request.body.args ?? {}),
      stableArgsHash(request.body.args ?? {}),
      new Date().toISOString()
    );
  } else {
    deps.db.prepare("UPDATE tool_calls SET status = ?, duration_ms = ?, completed_at = ? WHERE id = ?").run(
      request.body.status ?? "completed",
      request.body.durationMs ?? null,
      new Date().toISOString(),
      request.body.toolCallId
    );
  }

  audit.record({
    correlationId: request.params.runId,
    actorType: "runtime",
    actorId: request.body.agentId,
    action: request.body.phase === "before" ? "tool.call.before" : "tool.call.after",
    target: `${request.body.serverName}.${request.body.toolName}`,
    payload: request.body
  });

  if (request.body.phase === "after") return { decision: "allow", toolCallId: request.body.toolCallId };
  const policy = getToolPolicy(request.body.toolName);
  if (policy === "safe") return { decision: "allow", toolCallId: request.body.toolCallId };
  if (policy === "disabled") return { decision: "deny", toolCallId: request.body.toolCallId, reason: "policy_denied" };
  if (!request.body.args) return { decision: "deny", toolCallId: request.body.toolCallId, reason: "approval_args_missing" };

  const created = approvals.createApproval({
    runId: request.params.runId,
    toolCallId: request.body.toolCallId,
    agentId: request.body.agentId,
    toolName: request.body.toolName,
    args: request.body.args
  });
  deps.db.prepare("UPDATE tool_calls SET status = 'approval_required', approval_id = ? WHERE id = ?").run(created.approvalId, request.body.toolCallId);
  if (created.event) deps.hub?.broadcast(created.event);
  return { decision: "approval_required", toolCallId: request.body.toolCallId, approvalId: created.approvalId };
});

app.get<{ Params: { approvalId: string } }>("/internal/approvals/:approvalId", async (request, reply) => {
  const approval = approvals.getForRuntime(request.params.approvalId);
  if (!approval) return reply.code(404).send({ error: { code: "approval_not_found", message: "Approval not found" } });
  return approval;
});

app.post<{ Params: { approvalId: string } }>("/internal/approvals/:approvalId/consume", async (request, reply) => {
  try {
    const consumed = approvals.consume(request.params.approvalId);
    if (consumed.event) deps.hub?.broadcast(consumed.event);
    return consumed;
  } catch (error) {
    return reply.code(409).send({
      error: {
        code: "approval_not_consumable",
        message: error instanceof Error ? error.message : "Approval is not consumable"
      }
    });
  }
});
```

Create `turing-backend/agent-runtime/src/approvals/approvalPolling.ts`:

```ts
export type RuntimeApprovalState = {
  approvalId: string;
  status: "pending" | "approved" | "denied" | "expired" | "consumed";
  approvalToken?: string;
};

export async function waitForApprovalToken(
  approvalId: string,
  loadApproval: (approvalId: string) => Promise<RuntimeApprovalState>,
  options: { pollMs: number; timeoutMs: number } = { pollMs: 1000, timeoutMs: 65000 }
): Promise<string> {
  const deadline = Date.now() + options.timeoutMs;
  while (Date.now() < deadline) {
    const approval = await loadApproval(approvalId);
    if (approval.status === "approved" && approval.approvalToken) return approval.approvalToken;
    if (approval.status === "denied") throw new Error("Approval denied");
    if (approval.status === "expired") throw new Error("Approval expired");
    if (approval.status === "consumed") throw new Error("Approval already consumed");
    await new Promise((resolve) => setTimeout(resolve, options.pollMs));
  }
  throw new Error("Approval timed out");
}
```

Add a runtime client method in `agent-runtime/src/orchestrator/client.ts`:

```ts
import type { RuntimeApprovalState } from "../approvals/approvalPolling.js";

getApproval(approvalId: string): Promise<RuntimeApprovalState> {
  return this.request<RuntimeApprovalState>(`/approvals/${approvalId}`);
}
```

Update `agent-runtime/src/audit/beacons.ts` with a helper that turns an approval-required beacon response into an MCP approval token before a write:

```ts
import { waitForApprovalToken } from "../approvals/approvalPolling.js";

export async function authorizeToolCall(
  post: (beacon: ToolCallBeacon) => Promise<ToolPolicyDecision>,
  getApproval: (approvalId: string) => Promise<{ approvalId: string; status: "pending" | "approved" | "denied" | "expired" | "consumed"; approvalToken?: string }>,
  beacon: ToolCallBeacon
): Promise<{ approvalToken?: string }> {
  const decision = await postToolBeacon(post, beacon);
  if (decision.decision === "allow") return {};
  if (decision.decision === "deny") throw new Error(`Tool denied: ${decision.reason}`);
  const approvalToken = await waitForApprovalToken(decision.approvalId, getApproval);
  return { approvalToken };
}
```

Update `agent-runtime/src/tools/toolRunner.ts` so approval-required file writes receive the approved JWT before calling MCP:

```ts
import { authorizeToolCall } from "../audit/beacons.js";

const authorization = await authorizeToolCall(input.postBeacon, input.getApproval, {
  phase: "before",
  toolCallId,
  agentId: input.agentId,
  serverName: input.serverName,
  toolName: input.toolName,
  args: input.args,
  runId: input.runId,
  traceId: input.traceId
});
const result = await input.mcpClient.callTool(input.toolName, input.args, authorization.approvalToken);
```

The final `runAuthorizedMcpTool` input type must include `getApproval`:

```ts
getApproval: (approvalId: string) => Promise<{ approvalId: string; status: "pending" | "approved" | "denied" | "expired" | "consumed"; approvalToken?: string }>;
```

Update `agent-runtime/src/agents/generalAssistant.ts` so explicit debug tool requests exercise MCP end-to-end before normal LLM routing:

```ts
if (job.payload.userText.trim() === "/tool system.time") {
  const result = await tools.runAuthorizedMcpTool({
    agentId: "general_assistant",
    runId: job.runId,
    traceId: job.traceId,
    serverName: "system",
    toolName: "system.time",
    args: {},
    mcpClient: tools.systemMcpClient,
    postBeacon: tools.postBeacon,
    getApproval: tools.getApproval
  });
  const content = JSON.stringify(result);
  yield { type: "event", event: { sessionId: job.sessionId, runId: job.runId, traceId: job.traceId, type: "message.delta", payload: { messageId: job.assistantMessageId, delta: content } } };
  yield { type: "complete", content };
  return;
}

if (job.payload.userText.trim() === "/tool files.create") {
  const args = { path: "runtime-smoke.txt", content: "created through approval flow" };
  const result = await tools.runAuthorizedMcpTool({
    agentId: "general_assistant",
    runId: job.runId,
    traceId: job.traceId,
    serverName: "files",
    toolName: "files.create",
    args,
    mcpClient: tools.filesMcpClient,
    postBeacon: tools.postBeacon,
    getApproval: tools.getApproval
  });
  const content = JSON.stringify(result);
  yield { type: "event", event: { sessionId: job.sessionId, runId: job.runId, traceId: job.traceId, type: "message.delta", payload: { messageId: job.assistantMessageId, delta: content } } };
  yield { type: "complete", content };
  return;
}
```

Update `agent-runtime/src/main.ts` to construct MCP clients and pass tool dependencies into `createGeneralAssistantExecutor`:

```ts
import { McpClient } from "./mcp/client.js";
import { runAuthorizedMcpTool } from "./tools/toolRunner.js";

const systemMcpClient = new McpClient(config.mcpSystemBaseUrl, config.mcpSystemToken);
const filesMcpClient = new McpClient(config.mcpFilesBaseUrl, config.mcpFilesToken);
const executor = createGeneralAssistantExecutor(providers, {
  systemMcpClient,
  filesMcpClient,
  runAuthorizedMcpTool,
  postBeacon: (beacon) => client.postToolBeacon(beacon.runId, beacon),
  getApproval: (approvalId) => client.getApproval(approvalId)
});
```

- [ ] **Step 7: Wire shared maintenance sweep**

In orchestrator startup, use the shared `startSweeps` helper from Task 13 so stale jobs and pending approval expiration run on `config.jobReaperIntervalMs` and emitted expiration events are broadcast:

```ts
import { startSweeps } from "./maintenance/sweeps.js";

const sweeps = startSweeps({ db, config, hub });

process.on("SIGTERM", () => sweeps.stop());
```

The sweep is intentionally short-lived and DB-backed: pending approvals expire after 60 seconds, waiting runs move to a terminal failure state, and a later implementation can replace this with a unified scheduler without changing the approval contract.

- [ ] **Step 8: Run approval tests**

Run:

```bash
cd turing-backend
npm test -w @turing/orchestrator -- approvals.test.ts internal-api.test.ts public-api.test.ts
npm test -w @turing/agent-runtime -- approvalPolling.test.ts beacons.test.ts
npm run typecheck -w @turing/orchestrator
npm run typecheck -w @turing/agent-runtime
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add turing-backend/orchestrator/src/approvals turing-backend/orchestrator/src/maintenance turing-backend/orchestrator/src/api turing-backend/orchestrator/src/internal turing-backend/orchestrator/tests turing-backend/agent-runtime/src/approvals turing-backend/agent-runtime/src/orchestrator turing-backend/agent-runtime/src/audit turing-backend/agent-runtime/tests
git commit -m "feat: add approval JWT flow"
```

---

### Task 14: Flutter client networking, models, settings, and auth storage

**Files:**
- Modify: `turing-client/flutter_app/pubspec.yaml`
- Create: `turing-client/flutter_app/lib/models/turing_event.dart`
- Create: `turing-client/flutter_app/lib/models/session.dart`
- Create: `turing-client/flutter_app/lib/models/message.dart`
- Create: `turing-client/flutter_app/lib/models/approval.dart`
- Create: `turing-client/flutter_app/lib/networking/api_client.dart`
- Create: `turing-client/flutter_app/lib/networking/ws_client.dart`
- Create: `turing-client/flutter_app/lib/networking/auth_storage.dart`
- Create: `turing-client/flutter_app/test/networking/api_client_test.dart`
- Create: `turing-client/flutter_app/test/models/turing_event_test.dart`

- [ ] **Step 1: Add client dependencies**

Modify `turing-client/flutter_app/pubspec.yaml`:

```yaml
dependencies:
  flutter:
    sdk: flutter
  cupertino_icons: ^1.0.8
  http: ^1.2.2
  web_socket_channel: ^3.0.1
  flutter_secure_storage: ^9.2.2
```

Run:

```bash
cd turing-client/flutter_app
flutter pub get
```

- [ ] **Step 2: Write failing event model test**

Create `test/models/turing_event_test.dart`:

```dart
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/models/turing_event.dart';

void main() {
  test('parses event envelope', () {
    final event = TuringEvent.fromJson({
      'eventId': 'evt_1',
      'sessionId': 'sess_1',
      'runId': 'run_1',
      'traceId': 'trace_1',
      'sequence': 1,
      'type': 'message.delta',
      'createdAt': '2026-05-10T00:00:00.000Z',
      'payload': {'delta': 'hi'}
    });

    expect(event.type, 'message.delta');
    expect(event.payload['delta'], 'hi');
  });
}
```

- [ ] **Step 3: Implement models**

Create `lib/models/turing_event.dart`:

```dart
class TuringEvent {
  const TuringEvent({
    required this.eventId,
    required this.sessionId,
    this.runId,
    required this.traceId,
    required this.sequence,
    required this.type,
    required this.createdAt,
    required this.payload,
  });

  final String eventId;
  final String sessionId;
  final String? runId;
  final String traceId;
  final int sequence;
  final String type;
  final DateTime createdAt;
  final Map<String, dynamic> payload;

  factory TuringEvent.fromJson(Map<String, dynamic> json) {
    return TuringEvent(
      eventId: json['eventId'] as String,
      sessionId: json['sessionId'] as String,
      runId: json['runId'] as String?,
      traceId: json['traceId'] as String,
      sequence: json['sequence'] as int,
      type: json['type'] as String,
      createdAt: DateTime.parse(json['createdAt'] as String),
      payload: Map<String, dynamic>.from(json['payload'] as Map),
    );
  }
}
```

Create `lib/models/session.dart`:

```dart
class Session {
  const Session({required this.sessionId, required this.title, required this.updatedAt});

  final String sessionId;
  final String? title;
  final DateTime updatedAt;

  factory Session.fromJson(Map<String, dynamic> json) {
    return Session(
      sessionId: json['sessionId'] as String,
      title: json['title'] as String?,
      updatedAt: DateTime.parse(json['updatedAt'] as String),
    );
  }
}
```

Create `lib/models/message.dart`:

```dart
class Message {
  const Message({required this.messageId, required this.role, required this.content, required this.sequence, required this.createdAt});

  final String messageId;
  final String role;
  final String content;
  final int sequence;
  final DateTime createdAt;

  factory Message.fromJson(Map<String, dynamic> json) {
    return Message(
      messageId: json['messageId'] as String,
      role: json['role'] as String,
      content: json['content'] as String,
      sequence: json['sequence'] as int,
      createdAt: DateTime.parse(json['createdAt'] as String),
    );
  }
}
```

Create `lib/models/approval.dart`:

```dart
class Approval {
  const Approval({required this.approvalId, required this.toolName, required this.argsSummary, required this.status});

  final String approvalId;
  final String toolName;
  final String argsSummary;
  final String status;

  factory Approval.fromJson(Map<String, dynamic> json) {
    return Approval(
      approvalId: json['approvalId'] as String,
      toolName: json['toolName'] as String,
      argsSummary: json['argsSummary'] as String? ?? '',
      status: json['status'] as String,
    );
  }
}
```

- [ ] **Step 4: Implement API client**

Create `lib/networking/api_client.dart`:

```dart
import 'dart:convert';
import 'package:http/http.dart' as http;

class TuringApiClient {
  TuringApiClient({required this.baseUrl, required this.apiKey, http.Client? httpClient})
      : _httpClient = httpClient ?? http.Client();

  final String baseUrl;
  final String apiKey;
  final http.Client _httpClient;

  Map<String, String> get _headers => {
        'authorization': 'Bearer $apiKey',
        'content-type': 'application/json',
      };

  Future<Map<String, dynamic>> createSession({String? title}) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/sessions'),
      headers: _headers,
      body: jsonEncode({'title': title}),
    );
    return _decode(response);
  }

  Future<Map<String, dynamic>> sendMessage({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
  }) async {
    final response = await _httpClient.post(
      Uri.parse('$baseUrl/api/sessions/$sessionId/messages'),
      headers: _headers,
      body: jsonEncode({'content': content, 'modelProvider': modelProvider}),
    );
    return _decode(response);
  }

  Future<Map<String, dynamic>> approveApproval(String approvalId) async {
    final response = await _httpClient.post(Uri.parse('$baseUrl/api/approvals/$approvalId/approve'), headers: _headers);
    return _decode(response);
  }

  Future<Map<String, dynamic>> denyApproval(String approvalId) async {
    final response = await _httpClient.post(Uri.parse('$baseUrl/api/approvals/$approvalId/deny'), headers: _headers);
    return _decode(response);
  }

  Map<String, dynamic> _decode(http.Response response) {
    final body = jsonDecode(response.body) as Map<String, dynamic>;
    if (response.statusCode >= 400) {
      throw StateError(body['error']?['message'] as String? ?? 'Request failed');
    }
    return body;
  }
}
```

- [ ] **Step 5: Implement WebSocket client and secure storage**

Create `lib/networking/ws_client.dart`:

```dart
import 'dart:convert';
import 'package:web_socket_channel/web_socket_channel.dart';
import '../models/turing_event.dart';

class TuringWsClient {
  TuringWsClient({required this.baseUrl, required this.apiKey});

  final String baseUrl;
  final String apiKey;
  WebSocketChannel? _channel;

  Stream<TuringEvent> connect({required String sessionId, int? lastSequence}) {
    final wsUrl = baseUrl.replaceFirst(RegExp(r'^http'), 'ws');
    _channel = WebSocketChannel.connect(Uri.parse('$wsUrl/ws?token=$apiKey'));
    _channel!.sink.add(jsonEncode({'type': 'hello', 'sessionId': sessionId, 'lastSequence': lastSequence}));
    return _channel!.stream.expand((raw) {
      final message = jsonDecode(raw as String) as Map<String, dynamic>;
      if (message['type'] == 'hello_ack') {
        return (message['replayedEvents'] as List).map((item) => TuringEvent.fromJson(Map<String, dynamic>.from(item as Map)));
      }
      if (message['type'] == 'event') {
        return [TuringEvent.fromJson(Map<String, dynamic>.from(message['event'] as Map))];
      }
      return const <TuringEvent>[];
    });
  }

  void close() => _channel?.sink.close();
}
```

Create `lib/networking/auth_storage.dart`:

```dart
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

class AuthStorage {
  const AuthStorage([this._storage = const FlutterSecureStorage()]);

  final FlutterSecureStorage _storage;
  static const _backendUrlKey = 'turing_backend_url';
  static const _apiKeyKey = 'turing_api_key';

  Future<void> save({required String backendUrl, required String apiKey}) async {
    await _storage.write(key: _backendUrlKey, value: backendUrl);
    await _storage.write(key: _apiKeyKey, value: apiKey);
  }

  Future<String?> readBackendUrl() => _storage.read(key: _backendUrlKey);
  Future<String?> readApiKey() => _storage.read(key: _apiKeyKey);
}
```

- [ ] **Step 6: Run Flutter tests**

Run:

```bash
cd turing-client/flutter_app
flutter test
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add turing-client/flutter_app/pubspec.yaml turing-client/flutter_app/pubspec.lock turing-client/flutter_app/lib turing-client/flutter_app/test
git commit -m "feat: add Flutter backend protocol clients"
```

---

### Task 15: Flutter settings, sessions, streaming chat, and approval cards

**Files:**
- Create: `turing-client/flutter_app/lib/features/settings/settings_screen.dart`
- Create: `turing-client/flutter_app/lib/features/sessions/session_list_screen.dart`
- Create: `turing-client/flutter_app/lib/features/chat/chat_screen.dart`
- Create: `turing-client/flutter_app/lib/features/chat/model_provider_selector.dart`
- Create: `turing-client/flutter_app/lib/features/approvals/approval_card.dart`
- Modify: `turing-client/flutter_app/lib/app.dart`
- Test: `turing-client/flutter_app/test/features/chat_screen_test.dart`
- Test: `turing-client/flutter_app/test/features/model_provider_selector_test.dart`
- Test: `turing-client/flutter_app/test/features/approval_card_test.dart`

- [ ] **Step 1: Write failing approval card widget test**

Create `test/features/approval_card_test.dart`:

```dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/approvals/approval_card.dart';

void main() {
  testWidgets('approval card exposes approve and deny actions', (tester) async {
    var approved = false;
    var denied = false;

    await tester.pumpWidget(MaterialApp(
      home: ApprovalCard(
        toolName: 'files.update',
        argsSummary: 'Update note.txt',
        onApprove: () => approved = true,
        onDeny: () => denied = true,
      ),
    ));

    await tester.tap(find.text('Approve'));
    expect(approved, true);
    await tester.tap(find.text('Deny'));
    expect(denied, true);
  });
}
```

- [ ] **Step 2: Write failing model provider selector test**

Create `test/features/model_provider_selector_test.dart`:

```dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/model_provider_selector.dart';

void main() {
  testWidgets('model provider selector changes between Ollama and OpenAI-compatible', (tester) async {
    var selected = 'ollama';
    await tester.pumpWidget(MaterialApp(
      home: Scaffold(
        body: ModelProviderSelector(
          value: selected,
          onChanged: (value) => selected = value,
        ),
      ),
    ));

    await tester.tap(find.byType(DropdownButton<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('OpenAI-compatible').last);

    expect(selected, 'openai_compatible');
  });
}
```

- [ ] **Step 3: Implement approval card**

Create `lib/features/approvals/approval_card.dart`:

```dart
import 'package:flutter/material.dart';

class ApprovalCard extends StatelessWidget {
  const ApprovalCard({
    super.key,
    required this.toolName,
    required this.argsSummary,
    required this.onApprove,
    required this.onDeny,
  });

  final String toolName;
  final String argsSummary;
  final VoidCallback onApprove;
  final VoidCallback onDeny;

  @override
  Widget build(BuildContext context) {
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text('Approval requested: $toolName', style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 8),
            Text(argsSummary),
            const SizedBox(height: 12),
            Row(
              children: [
                FilledButton(onPressed: onApprove, child: const Text('Approve')),
                const SizedBox(width: 8),
                OutlinedButton(onPressed: onDeny, child: const Text('Deny')),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
```

- [ ] **Step 4: Implement model provider selector and settings screen**

Create `lib/features/chat/model_provider_selector.dart`:

```dart
import 'package:flutter/material.dart';

class ModelProviderSelector extends StatelessWidget {
  const ModelProviderSelector({super.key, required this.value, required this.onChanged});

  final String value;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) {
    return DropdownButton<String>(
      value: value,
      items: const [
        DropdownMenuItem(value: 'ollama', child: Text('Ollama')),
        DropdownMenuItem(value: 'openai_compatible', child: Text('OpenAI-compatible')),
      ],
      onChanged: (value) {
        if (value != null) onChanged(value);
      },
    );
  }
}
```

Create `lib/features/settings/settings_screen.dart`:

```dart
import 'package:flutter/material.dart';
import '../../networking/auth_storage.dart';

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key, required this.authStorage});
  final AuthStorage authStorage;

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  final _backendUrl = TextEditingController(text: 'http://localhost:3000');
  final _apiKey = TextEditingController();

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('TuringAgent Settings')),
      body: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          children: [
            TextField(controller: _backendUrl, decoration: const InputDecoration(labelText: 'Backend URL')),
            TextField(controller: _apiKey, decoration: const InputDecoration(labelText: 'API key'), obscureText: true),
            const SizedBox(height: 16),
            FilledButton(
              onPressed: () async {
                await widget.authStorage.save(backendUrl: _backendUrl.text, apiKey: _apiKey.text);
                if (context.mounted) Navigator.of(context).pop();
              },
              child: const Text('Save'),
            ),
          ],
        ),
      ),
    );
  }
}
```

- [ ] **Step 5: Implement chat screen event application**

Create `lib/features/chat/chat_screen.dart` with state that appends `message.delta` to the active assistant bubble and sends the selected model provider with each message:

```dart
import 'dart:async';
import 'package:flutter/material.dart';
import '../../models/turing_event.dart';
import '../../networking/api_client.dart';
import '../../networking/ws_client.dart';
import '../approvals/approval_card.dart';
import 'model_provider_selector.dart';

class ChatScreen extends StatefulWidget {
  const ChatScreen({super.key, required this.sessionId, required this.apiClient, required this.wsClient});
  final String sessionId;
  final TuringApiClient apiClient;
  final TuringWsClient wsClient;

  @override
  State<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends State<ChatScreen> {
  final _controller = TextEditingController();
  final List<String> _messages = [];
  final List<_PendingApproval> _approvals = [];
  StreamSubscription<TuringEvent>? _subscription;
  String _modelProvider = 'ollama';

  @override
  void initState() {
    super.initState();
    _subscription = widget.wsClient.connect(sessionId: widget.sessionId).listen(_applyEvent);
  }

  void _applyEvent(TuringEvent event) {
    if (event.type == 'message.delta') {
      setState(() {
        if (_messages.isEmpty || !_messages.last.startsWith('Assistant: ')) {
          _messages.add('Assistant: ');
        }
        _messages[_messages.length - 1] += event.payload['delta'] as String;
      });
    }
    if (event.type == 'approval.requested') {
      setState(() {
        _approvals.add(_PendingApproval(
          approvalId: event.payload['approvalId'] as String,
          toolName: event.payload['toolName'] as String,
          argsSummary: event.payload['argsSummary'] as String? ?? '',
        ));
      });
    }
    if (event.type == 'approval.approved' || event.type == 'approval.denied' || event.type == 'approval.expired' || event.type == 'approval.consumed') {
      final approvalId = event.payload['approvalId'] as String?;
      if (approvalId != null) {
        setState(() => _approvals.removeWhere((approval) => approval.approvalId == approvalId));
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('TuringAgent')),
      body: Column(
        children: [
          Expanded(child: ListView(children: _messages.map((message) => ListTile(title: Text(message))).toList())),
          ..._approvals.map((approval) => ApprovalCard(
                toolName: approval.toolName,
                argsSummary: approval.argsSummary,
                onApprove: () async {
                  await widget.apiClient.approveApproval(approval.approvalId);
                  setState(() => _approvals.remove(approval));
                },
                onDeny: () async {
                  await widget.apiClient.denyApproval(approval.approvalId);
                  setState(() => _approvals.remove(approval));
                },
              )),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 8),
            child: Row(
              children: [
                const Text('Model provider: '),
                ModelProviderSelector(
                  value: _modelProvider,
                  onChanged: (value) => setState(() => _modelProvider = value),
                ),
              ],
            ),
          ),
          Row(
            children: [
              Expanded(child: TextField(controller: _controller)),
              IconButton(
                icon: const Icon(Icons.send),
                onPressed: () async {
                  final text = _controller.text;
                  setState(() => _messages.add('You: $text'));
                  _controller.clear();
                  await widget.apiClient.sendMessage(sessionId: widget.sessionId, content: text, modelProvider: _modelProvider);
                },
              ),
            ],
          )
        ],
      ),
    );
  }

  @override
  void dispose() {
    _subscription?.cancel();
    widget.wsClient.close();
    _controller.dispose();
    super.dispose();
  }
}

class _PendingApproval {
  const _PendingApproval({required this.approvalId, required this.toolName, required this.argsSummary});
  final String approvalId;
  final String toolName;
  final String argsSummary;
}
```

- [ ] **Step 6: Wire app navigation**

Create `lib/features/sessions/session_list_screen.dart`:

```dart
import 'package:flutter/material.dart';
import '../../networking/api_client.dart';
import '../../networking/ws_client.dart';
import '../chat/chat_screen.dart';

class SessionListScreen extends StatefulWidget {
  const SessionListScreen({super.key, required this.apiClient, required this.wsClientFactory});

  final TuringApiClient apiClient;
  final TuringWsClient Function() wsClientFactory;

  @override
  State<SessionListScreen> createState() => _SessionListScreenState();
}

class _SessionListScreenState extends State<SessionListScreen> {
  bool _creating = false;

  Future<void> _createSession() async {
    setState(() => _creating = true);
    final result = await widget.apiClient.createSession(title: 'New chat');
    if (!mounted) return;
    setState(() => _creating = false);
    await Navigator.of(context).push(MaterialPageRoute(
      builder: (_) => ChatScreen(
        sessionId: result['sessionId'] as String,
        apiClient: widget.apiClient,
        wsClient: widget.wsClientFactory(),
      ),
    ));
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('TuringAgent Sessions')),
      body: Center(
        child: FilledButton(
          onPressed: _creating ? null : _createSession,
          child: Text(_creating ? 'Creating...' : 'New chat'),
        ),
      ),
    );
  }
}
```

Modify `lib/app.dart` so the app starts at settings if no saved API key exists; otherwise show session list/chat:

```dart
import 'package:flutter/material.dart';
import 'features/sessions/session_list_screen.dart';
import 'features/settings/settings_screen.dart';
import 'networking/api_client.dart';
import 'networking/auth_storage.dart';
import 'networking/ws_client.dart';

class TuringApp extends StatelessWidget {
  const TuringApp({super.key, this.authStorage = const AuthStorage()});

  final AuthStorage authStorage;

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'TuringAgent',
      theme: ThemeData(colorSchemeSeed: Colors.deepPurple, useMaterial3: true),
      home: FutureBuilder<_ClientConfig?>(
        future: _loadConfig(),
        builder: (context, snapshot) {
          if (snapshot.connectionState != ConnectionState.done) {
            return const Scaffold(body: Center(child: CircularProgressIndicator()));
          }
          final config = snapshot.data;
          if (config == null) return SettingsScreen(authStorage: authStorage);
          final apiClient = TuringApiClient(baseUrl: config.backendUrl, apiKey: config.apiKey);
          return SessionListScreen(
            apiClient: apiClient,
            wsClientFactory: () => TuringWsClient(baseUrl: config.backendUrl, apiKey: config.apiKey),
          );
        },
      ),
    );
  }

  Future<_ClientConfig?> _loadConfig() async {
    final backendUrl = await authStorage.readBackendUrl();
    final apiKey = await authStorage.readApiKey();
    if (backendUrl == null || apiKey == null || backendUrl.isEmpty || apiKey.isEmpty) return null;
    return _ClientConfig(backendUrl: backendUrl, apiKey: apiKey);
  }
}

class _ClientConfig {
  const _ClientConfig({required this.backendUrl, required this.apiKey});
  final String backendUrl;
  final String apiKey;
}
```

- [ ] **Step 7: Run Flutter tests**

Run:

```bash
cd turing-client/flutter_app
flutter test
```

Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add turing-client/flutter_app/lib turing-client/flutter_app/test
git commit -m "feat: add Flutter chat and approval UI"
```

---

### Task 16: End-to-end Docker smoke and local documentation

**Files:**
- Create: `turing-backend/scripts/smoke.sh`
- Create: `turing-backend/scripts/smoke-ws.mjs`
- Modify: `README.md`
- Modify: `docs/superpowers/specs/2026-05-09-project-turing-v1-design-copilot.md` only if implementation reveals a spec correction

- [ ] **Step 1: Add smoke script**

Create `turing-backend/scripts/smoke.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

mkdir -p .runtime
./scripts/init.sh >.runtime/turing-init.log
docker compose -f infra/docker-compose.yml up --build -d

api_key="$(grep '^TURING_CLIENT_API_KEY=' .env | cut -d= -f2-)"

curl --fail http://localhost:3000/health
curl --fail -H "Authorization: Bearer ${api_key}" http://localhost:3000/api/config

session_json="$(curl --fail -s -H "Authorization: Bearer ${api_key}" -H "content-type: application/json" -d '{"title":"Smoke"}' http://localhost:3000/api/sessions)"
session_id="$(node -e "console.log(JSON.parse(process.argv[1]).sessionId)" "$session_json")"

message_json="$(curl --fail -s \
  -H "Authorization: Bearer ${api_key}" \
  -H "content-type: application/json" \
  -d '{"content":"Say hello from TuringAgent smoke test","modelProvider":"ollama"}' \
  "http://localhost:3000/api/sessions/${session_id}/messages")"
run_id="$(node -e "console.log(JSON.parse(process.argv[1]).runId)" "$message_json")"

node scripts/smoke-ws.mjs "${api_key}" "${session_id}" "${run_id}"

curl --fail -s -H "Authorization: Bearer ${api_key}" "http://localhost:3000/api/audit" | node -e "JSON.parse(require('fs').readFileSync(0, 'utf8')); console.log('Audit endpoint OK')"
curl --fail -s -H "Authorization: Bearer ${api_key}" "http://localhost:3000/api/tool-calls" | node -e "JSON.parse(require('fs').readFileSync(0, 'utf8')); console.log('Tool-call endpoint OK')"

echo "Smoke test queued session ${session_id}"
```

Create `turing-backend/scripts/smoke-ws.mjs`:

```js
import WebSocket from "ws";

const [apiKey, sessionId, runId] = process.argv.slice(2);
if (!apiKey || !sessionId || !runId) {
  console.error("Usage: node scripts/smoke-ws.mjs <api-key> <session-id> <run-id>");
  process.exit(2);
}

async function connectAndHello(lastSequence, waitForCompleted = false) {
  return new Promise((resolve, reject) => {
    const ws = new WebSocket(`ws://localhost:3000/ws?token=${encodeURIComponent(apiKey)}`);
    let ack;
    const timeout = setTimeout(() => {
      ws.close();
      reject(new Error(waitForCompleted ? `Timed out waiting for message.completed for run ${runId}` : "Timed out waiting for WebSocket hello_ack"));
    }, waitForCompleted ? 90000 : 5000);

    ws.on("open", () => {
      ws.send(JSON.stringify({ type: "hello", sessionId, lastSequence }));
    });
    ws.on("message", (raw) => {
      const message = JSON.parse(raw.toString());
      if (message.type === "hello_ack") {
        ack = message;
        if (waitForCompleted && Array.isArray(message.replayedEvents) && message.replayedEvents.some((event) => event.type === "message.completed" && event.runId === runId)) {
          clearTimeout(timeout);
          ws.close();
          resolve(message);
        }
        if (waitForCompleted) return;
        clearTimeout(timeout);
        ws.close();
        resolve(message);
      }
      if (waitForCompleted && message.type === "event" && message.event?.type === "message.completed" && message.event.runId === runId) {
        clearTimeout(timeout);
        ws.close();
        resolve({ ...ack, latestSequence: message.event.sequence ?? ack?.latestSequence ?? 0, completedEvent: message.event });
      }
    });
    ws.on("error", reject);
  });
}

const firstAck = await connectAndHello(0, true);
const latestSequence = firstAck.latestSequence ?? 0;
if (latestSequence < 1) {
  throw new Error("Expected at least one persisted event before replay smoke");
}
const replayAck = await connectAndHello(latestSequence - 1);
if (!Array.isArray(replayAck.replayedEvents) || replayAck.replayedEvents.length < 1) {
  throw new Error("Expected replayedEvents to include the missed event");
}
console.log(`WebSocket reconnect/replay OK for ${sessionId} at sequence ${latestSequence}`);
```

Run:

```bash
chmod +x turing-backend/scripts/smoke.sh
```

- [ ] **Step 2: Document local development**

Add to `README.md`:

````markdown
## TuringAgent v1.0 local runtime

Initialize backend secrets:

```bash
cd turing-backend
./scripts/init.sh
```

Start the backend:

```bash
./scripts/dev.sh
```

Run backend smoke checks:

```bash
./scripts/smoke.sh
```

Run Flutter verification before manual app smoke:

```bash
cd ../turing-client/flutter_app
flutter test
flutter run -d macos
```

Flutter client:

```bash
cd turing-client/flutter_app
flutter pub get
flutter run -d macos
```

Paste the printed `TURING_CLIENT_API_KEY` into the Flutter settings screen. Use `http://localhost:3000` on the Mac and the Mac Mini LAN/Tailscale address from physical Android devices. Send one message with `Ollama` selected and one with `OpenAI-compatible` selected if `OPENAI_API_KEY` is configured.
````

- [ ] **Step 3: Run full verification**

Run:

```bash
cd turing-backend
npm test
npm run typecheck
(cd mcp-system && go test ./...)
(cd mcp-files && go test ./...)
docker compose -f infra/docker-compose.yml config --quiet
```

Expected: all commands pass.

Run Flutter verification:

```bash
cd turing-client/flutter_app
flutter test
```

Expected: all Flutter tests pass.

- [ ] **Step 4: Run smoke script if Docker and Ollama are available**

Run:

```bash
cd turing-backend
./scripts/smoke.sh
```

Expected: `/health` and `/api/config` return success, a session is created, a message is queued, the WebSocket hello/reconnect/replay handshake succeeds, and audit/tool-call inspection endpoints return valid JSON. If Ollama is unavailable, the queue path should still work and the run should fail durably with `model_unavailable`.

- [ ] **Step 5: Commit**

```bash
git add README.md turing-backend/scripts/smoke.sh turing-backend/scripts/smoke-ws.mjs
git commit -m "docs: add Turing local runtime smoke path"
```

---

## Final verification checklist

- [ ] `cd turing-backend && npm test`
- [ ] `cd turing-backend && npm run typecheck`
- [ ] `cd turing-backend/mcp-system && go test ./...`
- [ ] `cd turing-backend/mcp-files && go test ./...`
- [ ] `cd turing-backend && docker compose -f infra/docker-compose.yml config --quiet`
- [ ] `cd turing-client/flutter_app && flutter test`
- [ ] `cd turing-backend && ./scripts/smoke.sh` when Docker/Ollama are available

## Self-review notes

- Spec coverage: tasks cover repo layout, `.env` scripts, Docker networks, shared contracts, orchestrator REST/internal APIs, SQLite schema, event replay, agent runtime, LLM providers, direct MCP calls, system/files MCP, approvals/JWT, Flutter settings/chat/approval UI, and smoke docs.
- No stale JWT-login/user/refresh-token implementation tasks are included.
- The plan intentionally keeps `files MCP` and active approval cards in v1.0, matching the approved hybrid spec.
- The old plan at `docs/superpowers/plans/2026-05-09-project-turing-v1.md` is superseded.
