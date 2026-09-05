# TuringAgent v1.0 Implementation Plan

**Historical plan:** This record is not current implementation or status.
Use the [canonical roadmap](../../NORTH_STAR.md) for current truth.

**Goal:** Build the v1.0 local-first TuringAgent vertical slice: Flutter client -> authenticated REST command API -> Node/TypeScript orchestrator -> SQLite -> Ollama/OpenAI-compatible streaming -> WebSocket events -> Flutter rendering, with system/files MCP tools, approvals, and audit logs.

**Architecture:** The orchestrator is one modular Node.js process using Fastify, SQLite, JWT auth, typed persisted events, an in-process `AgentExecutor`, model-provider adapters, and MCP tool clients. Flutter remains thin: it owns login/setup UI, session/chat UI, WebSocket rendering, model selection, and approval cards. Go MCP servers run as internal Docker Compose services; Ollama runs on the Mac host.

**Tech Stack:** Node 20, TypeScript, Fastify, `@fastify/websocket`, SQLite via `better-sqlite3`, Vitest, Pino, Zod, Jose JWT, bcryptjs, Go 1.23, Flutter/Dart, Docker Compose.

---

## Scope check

This plan implements the approved v1.0 foundation as one testable vertical slice. It intentionally excludes native macOS/Windows bridges, semantic memory, Redis/BullMQ, LangGraph, Google/Microsoft OAuth, vision, IoT, voice, arbitrary shell/native automation, screenshots, delete/move file operations, and distributed agent containers.

## File structure

### Backend orchestrator

- Create `turing-backend/orchestrator/package.json`: Node scripts and dependencies.
- Create `turing-backend/orchestrator/tsconfig.json`: TypeScript configuration.
- Create `turing-backend/orchestrator/vitest.config.ts`: Vitest configuration.
- Modify `turing-backend/orchestrator/Dockerfile`: build the new TypeScript service with runtime SQLite support.
- Create `turing-backend/orchestrator/.env.example`: documented local runtime configuration.
- Create `turing-backend/orchestrator/src/server.ts`: process entrypoint.
- Create `turing-backend/orchestrator/src/app.ts`: Fastify app composition.
- Create `turing-backend/orchestrator/src/config.ts`: environment parsing.
- Create `turing-backend/orchestrator/src/logging/logger.ts`: Pino setup.
- Create `turing-backend/orchestrator/src/contracts/ids.ts`: ULID helpers.
- Create `turing-backend/orchestrator/src/contracts/events.ts`: WebSocket event types.
- Create `turing-backend/orchestrator/src/contracts/errors.ts`: typed API errors.
- Create `turing-backend/orchestrator/src/db/connection.ts`: SQLite connection factory.
- Create `turing-backend/orchestrator/src/db/migrate.ts`: migration runner.
- Create `turing-backend/orchestrator/src/db/repositories/*.ts`: auth, sessions, messages, events, runs, tools, approvals, audit.
- Create `turing-backend/orchestrator/src/auth/*.ts`: password hashing, JWT, refresh tokens, auth routes and hooks.
- Create `turing-backend/orchestrator/src/api/*.ts`: health, config, sessions, messages, agents, tools, approvals, audit routes.
- Create `turing-backend/orchestrator/src/ws/sessionSocket.ts`: authenticated WebSocket stream and replay.
- Create `turing-backend/orchestrator/src/events/eventStore.ts`: persisted event append/replay.
- Create `turing-backend/orchestrator/src/agents/*.ts`: `AgentExecutor`, run service, `general_assistant`.
- Create `turing-backend/orchestrator/src/llm/*.ts`: provider interface, Ollama adapter, OpenAI-compatible adapter.
- Create `turing-backend/orchestrator/src/tools/*.ts`: MCP client, registry, policy, invocation service.
- Create `turing-backend/orchestrator/src/security/*.ts`: approval and audit services.
- Create `turing-backend/orchestrator/migrations/001_initial.sql`: canonical schema.
- Create `turing-backend/orchestrator/tests/**/*.test.ts`: backend unit/integration tests.

### MCP servers

- Replace `turing-backend/services/google-mcp` and `turing-backend/services/microsoft-mcp` in Compose with:
  - `turing-backend/services/mcp-system/go.mod`
  - `turing-backend/services/mcp-system/main.go`
  - `turing-backend/services/mcp-system/Dockerfile`
  - `turing-backend/services/mcp-files/go.mod`
  - `turing-backend/services/mcp-files/main.go`
  - `turing-backend/services/mcp-files/Dockerfile`
  - `turing-backend/services/mcp-files/main_test.go`

### Runtime

- Modify `turing-backend/docker-compose.yml`: orchestrator, system MCP, files MCP, SQLite data mount, allowed file sandbox mount, host Ollama access.
- Create `turing-backend/.env.example`: Compose-level env.
- Create `turing-backend/data/.gitkeep`: local data directory placeholder.
- Create `turing-backend/sandbox/.gitkeep`: default file-MCP sandbox placeholder.

### Flutter client

- Modify `turing-client/turing_app/pubspec.yaml`: add HTTP, WebSocket, secure storage, UUID/model helpers as needed.
- Replace stale `turing-client/turing_app/test/widget_test.dart`: current test references `MyApp`, but app entrypoint is `TuringApp`.
- Create `turing-client/turing_app/lib/core/config/turing_config.dart`: backend URL config.
- Create `turing-client/turing_app/lib/core/network/api_client.dart`: REST client with bearer token.
- Create `turing-client/turing_app/lib/core/network/turing_socket.dart`: WebSocket event stream.
- Create `turing-client/turing_app/lib/core/storage/token_store.dart`: secure token abstraction.
- Create `turing-client/turing_app/lib/models/turing_event.dart`: event parsing.
- Create `turing-client/turing_app/lib/models/session.dart`: session models.
- Modify `turing-client/turing_app/lib/models/chat_message.dart`: align with backend message/event model.
- Create `turing-client/turing_app/lib/features/auth/setup_login_screen.dart`: setup/login UI.
- Create `turing-client/turing_app/lib/features/chat/chat_controller.dart`: session state and event reducer.
- Modify `turing-client/turing_app/lib/ui/chat/chat_screen.dart`: send REST command and render streaming events.
- Modify `turing-client/turing_app/lib/ui/chat/widgets/chat_bubble.dart`: render pending, streaming, tool, and approval states.
- Modify `turing-client/turing_app/lib/app.dart`: choose setup/login or main shell.
- Create `turing-client/turing_app/test/models/turing_event_test.dart`
- Create `turing-client/turing_app/test/features/chat/chat_controller_test.dart`
- Update `turing-client/turing_app/test/widget_test.dart`

---

## Task 1: Bootstrap orchestrator project, config, logging, and health endpoints

**Files:**
- Create: `turing-backend/orchestrator/package.json`
- Create: `turing-backend/orchestrator/tsconfig.json`
- Create: `turing-backend/orchestrator/vitest.config.ts`
- Create: `turing-backend/orchestrator/.env.example`
- Create: `turing-backend/orchestrator/src/config.ts`
- Create: `turing-backend/orchestrator/src/logging/logger.ts`
- Create: `turing-backend/orchestrator/src/app.ts`
- Create: `turing-backend/orchestrator/src/server.ts`
- Create: `turing-backend/orchestrator/tests/config.test.ts`
- Create: `turing-backend/orchestrator/tests/health.test.ts`

- [ ] **Step 1: Create the failing config test**

Create `turing-backend/orchestrator/tests/config.test.ts`:

```ts
import { describe, expect, test } from 'vitest';
import { loadConfig } from '../src/config';

describe('loadConfig', () => {
  test('loads defaults and configured provider URLs', () => {
    const config = loadConfig({
      PORT: '3333',
      TURING_DATA_DIR: '/tmp/turing-test',
      TURING_JWT_SECRET: 'test-secret-with-at-least-32-characters',
      OLLAMA_BASE_URL: 'http://ollama.local:11434',
      OLLAMA_MODEL: 'llama3.2',
      OPENAI_BASE_URL: 'https://api.openai.com/v1',
      OPENAI_API_KEY: 'sk-test',
      OPENAI_MODEL: 'gpt-4o-mini',
      MCP_SYSTEM_URL: 'http://mcp-system:8080/mcp',
      MCP_FILES_URL: 'http://mcp-files:8080/mcp',
      TURING_FILE_ALLOWED_DIRS: '/tmp/a,/tmp/b',
    });

    expect(config.port).toBe(3333);
    expect(config.dataDir).toBe('/tmp/turing-test');
    expect(config.ollama.baseUrl).toBe('http://ollama.local:11434');
    expect(config.openAi.enabled).toBe(true);
    expect(config.files.allowedDirs).toEqual(['/tmp/a', '/tmp/b']);
  });

  test('rejects weak JWT secrets', () => {
    expect(() =>
      loadConfig({
        TURING_JWT_SECRET: 'short',
      }),
    ).toThrow(/TURING_JWT_SECRET/);
  });
});
```

- [ ] **Step 2: Create the failing health test**

Create `turing-backend/orchestrator/tests/health.test.ts`:

```ts
import { describe, expect, test } from 'vitest';
import { buildApp } from '../src/app';
import { loadConfig } from '../src/config';

describe('health routes', () => {
  test('GET /health returns ok', async () => {
    const app = await buildApp({
      config: loadConfig({
        TURING_JWT_SECRET: 'test-secret-with-at-least-32-characters',
      }),
    });

    const response = await app.inject({ method: 'GET', url: '/health' });

    expect(response.statusCode).toBe(200);
    expect(response.json()).toMatchObject({ status: 'ok', service: 'turing-orchestrator' });
  });

  test('GET /version returns API version', async () => {
    const app = await buildApp({
      config: loadConfig({
        TURING_JWT_SECRET: 'test-secret-with-at-least-32-characters',
      }),
    });

    const response = await app.inject({ method: 'GET', url: '/version' });

    expect(response.statusCode).toBe(200);
    expect(response.json()).toMatchObject({ version: '1.0.0', apiVersion: 'v1' });
  });
});
```

- [ ] **Step 3: Run tests and verify they fail because the project is not bootstrapped**

Run:

```bash
cd turing-backend/orchestrator
npm test -- --run tests/config.test.ts tests/health.test.ts
```

Expected: command fails because `package.json`, `src/config.ts`, and `src/app.ts` do not exist.

- [ ] **Step 4: Add Node project files**

Create `turing-backend/orchestrator/package.json`:

```json
{
  "name": "turing-orchestrator",
  "version": "1.0.0",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "tsx watch src/server.ts",
    "build": "tsc -p tsconfig.json",
    "start": "node dist/server.js",
    "test": "vitest",
    "test:run": "vitest run",
    "lint": "tsc -p tsconfig.json --noEmit"
  },
  "dependencies": {
    "@fastify/cors": "^10.0.1",
    "@fastify/websocket": "^11.0.1",
    "bcryptjs": "^2.4.3",
    "better-sqlite3": "^11.8.1",
    "fastify": "^5.2.1",
    "jose": "^5.9.6",
    "pino": "^9.6.0",
    "ulid": "^2.3.0",
    "zod": "^3.24.1"
  },
  "devDependencies": {
    "@types/bcryptjs": "^2.4.6",
    "@types/better-sqlite3": "^7.6.12",
    "@types/node": "^22.10.7",
    "pino-pretty": "^13.0.0",
    "tsx": "^4.19.2",
    "typescript": "^5.7.3",
    "vitest": "^2.1.8",
    "ws": "^8.18.0"
  }
}
```

Create `turing-backend/orchestrator/tsconfig.json`:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "NodeNext",
    "moduleResolution": "NodeNext",
    "lib": ["ES2022"],
    "strict": true,
    "noUncheckedIndexedAccess": true,
    "exactOptionalPropertyTypes": true,
    "esModuleInterop": true,
    "forceConsistentCasingInFileNames": true,
    "skipLibCheck": true,
    "outDir": "dist",
    "rootDir": "src",
    "types": ["node"]
  },
  "include": ["src/**/*.ts"],
  "exclude": ["dist", "node_modules"]
}
```

Create `turing-backend/orchestrator/vitest.config.ts`:

```ts
import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    environment: 'node',
    include: ['tests/**/*.test.ts'],
    testTimeout: 10000,
  },
});
```

Create `turing-backend/orchestrator/.env.example`:

```dotenv
PORT=3000
TURING_DATA_DIR=/data
TURING_JWT_SECRET=replace-with-at-least-32-random-characters
TURING_ACCESS_TOKEN_TTL_SECONDS=900
TURING_REFRESH_TOKEN_TTL_DAYS=30
OLLAMA_BASE_URL=http://host.docker.internal:11434
OLLAMA_MODEL=llama3.2
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_API_KEY=
OPENAI_MODEL=gpt-4o-mini
MCP_SYSTEM_URL=http://turing-mcp-system:8080/mcp
MCP_FILES_URL=http://turing-mcp-files:8080/mcp
TURING_FILE_ALLOWED_DIRS=/sandbox
```

- [ ] **Step 5: Implement config, logger, app, and server**

Create `turing-backend/orchestrator/src/config.ts`:

```ts
import { z } from 'zod';

const EnvSchema = z.object({
  PORT: z.coerce.number().int().positive().default(3000),
  TURING_DATA_DIR: z.string().default('./data'),
  TURING_JWT_SECRET: z.string().min(32, 'TURING_JWT_SECRET must be at least 32 characters'),
  TURING_ACCESS_TOKEN_TTL_SECONDS: z.coerce.number().int().positive().default(900),
  TURING_REFRESH_TOKEN_TTL_DAYS: z.coerce.number().int().positive().default(30),
  OLLAMA_BASE_URL: z.string().url().default('http://host.docker.internal:11434'),
  OLLAMA_MODEL: z.string().default('llama3.2'),
  OPENAI_BASE_URL: z.string().url().default('https://api.openai.com/v1'),
  OPENAI_API_KEY: z.string().optional(),
  OPENAI_MODEL: z.string().default('gpt-4o-mini'),
  MCP_SYSTEM_URL: z.string().url().default('http://turing-mcp-system:8080/mcp'),
  MCP_FILES_URL: z.string().url().default('http://turing-mcp-files:8080/mcp'),
  TURING_FILE_ALLOWED_DIRS: z.string().default('/sandbox'),
});

export type TuringConfig = ReturnType<typeof loadConfig>;

export function loadConfig(env: NodeJS.ProcessEnv = process.env) {
  const parsed = EnvSchema.parse(env);
  return {
    port: parsed.PORT,
    dataDir: parsed.TURING_DATA_DIR,
    jwt: {
      secret: parsed.TURING_JWT_SECRET,
      accessTokenTtlSeconds: parsed.TURING_ACCESS_TOKEN_TTL_SECONDS,
      refreshTokenTtlDays: parsed.TURING_REFRESH_TOKEN_TTL_DAYS,
    },
    ollama: {
      baseUrl: parsed.OLLAMA_BASE_URL,
      model: parsed.OLLAMA_MODEL,
    },
    openAi: {
      enabled: Boolean(parsed.OPENAI_API_KEY),
      baseUrl: parsed.OPENAI_BASE_URL,
      apiKey: parsed.OPENAI_API_KEY ?? '',
      model: parsed.OPENAI_MODEL,
    },
    mcp: {
      systemUrl: parsed.MCP_SYSTEM_URL,
      filesUrl: parsed.MCP_FILES_URL,
    },
    files: {
      allowedDirs: parsed.TURING_FILE_ALLOWED_DIRS.split(',').map((value) => value.trim()).filter(Boolean),
    },
  };
}
```

Create `turing-backend/orchestrator/src/logging/logger.ts`:

```ts
import pino from 'pino';

export function createLogger(service = 'turing-orchestrator') {
  return pino({
    name: service,
    level: process.env.LOG_LEVEL ?? 'info',
    redact: ['*.authorization', '*.cookie', '*.token', '*.secret', '*.apiKey', '*.password'],
    transport:
      process.env.LOG_PRETTY === '1'
        ? { target: 'pino-pretty', options: { colorize: true, singleLine: true } }
        : undefined,
  });
}
```

Create `turing-backend/orchestrator/src/app.ts`:

```ts
import Fastify, { type FastifyInstance } from 'fastify';
import cors from '@fastify/cors';
import websocket from '@fastify/websocket';
import { type TuringConfig, loadConfig } from './config.js';
import { createLogger } from './logging/logger.js';

export interface BuildAppOptions {
  config?: TuringConfig;
}

export async function buildApp(options: BuildAppOptions = {}): Promise<FastifyInstance> {
  const config = options.config ?? loadConfig();
  const app = Fastify({ loggerInstance: createLogger() });

  await app.register(cors, { origin: true });
  await app.register(websocket);

  app.get('/health', async () => ({
    status: 'ok',
    service: 'turing-orchestrator',
  }));

  app.get('/version', async () => ({
    version: '1.0.0',
    apiVersion: 'v1',
  }));

  app.get('/config', async () => ({
    apiVersion: 'v1',
    authRequired: true,
    providers: {
      ollama: { enabled: true, model: config.ollama.model },
      openAiCompatible: { enabled: config.openAi.enabled, model: config.openAi.model },
    },
  }));

  return app;
}
```

Create `turing-backend/orchestrator/src/server.ts`:

```ts
import { buildApp } from './app.js';
import { loadConfig } from './config.js';

const config = loadConfig();
const app = await buildApp({ config });

await app.listen({ host: '0.0.0.0', port: config.port });
```

- [ ] **Step 6: Install dependencies**

Run:

```bash
cd turing-backend/orchestrator
npm install
```

Expected: `node_modules` and `package-lock.json` are created.

- [ ] **Step 7: Run tests and type-check**

Run:

```bash
cd turing-backend/orchestrator
npm run test:run -- tests/config.test.ts tests/health.test.ts
npm run build
```

Expected: both commands pass.

- [ ] **Step 8: Commit**

Run:

```bash
git add turing-backend/orchestrator
git commit -m "feat: bootstrap orchestrator service" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 2: Add SQLite schema, migrations, and core repositories

**Files:**
- Create: `turing-backend/orchestrator/migrations/001_initial.sql`
- Create: `turing-backend/orchestrator/src/contracts/ids.ts`
- Create: `turing-backend/orchestrator/src/db/connection.ts`
- Create: `turing-backend/orchestrator/src/db/migrate.ts`
- Create: `turing-backend/orchestrator/src/db/repositories/sessionRepository.ts`
- Create: `turing-backend/orchestrator/src/db/repositories/messageRepository.ts`
- Create: `turing-backend/orchestrator/src/db/repositories/eventRepository.ts`
- Create: `turing-backend/orchestrator/tests/db/migrate.test.ts`
- Create: `turing-backend/orchestrator/tests/db/sessionMessageEventRepositories.test.ts`

- [ ] **Step 1: Write failing migration test**

Create `turing-backend/orchestrator/tests/db/migrate.test.ts`:

```ts
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, test } from 'vitest';
import { openDatabase } from '../../src/db/connection';
import { runMigrations } from '../../src/db/migrate';

describe('runMigrations', () => {
  test('creates canonical v1 tables and enables WAL', () => {
    const dir = mkdtempSync(join(tmpdir(), 'turing-db-'));
    const db = openDatabase(join(dir, 'orchestrator.db'));

    runMigrations(db);

    const tables = db
      .prepare("SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name")
      .all()
      .map((row) => (row as { name: string }).name);

    expect(tables).toEqual(
      expect.arrayContaining([
        'agent_run_steps',
        'agent_runs',
        'approvals',
        'audit_logs',
        'events',
        'jobs',
        'messages',
        'refresh_tokens',
        'schema_migrations',
        'sessions',
        'settings',
        'tool_calls',
        'tools',
        'users',
      ]),
    );
    expect(db.pragma('journal_mode', { simple: true })).toBe('wal');
  });
});
```

- [ ] **Step 2: Write failing repository test**

Create `turing-backend/orchestrator/tests/db/sessionMessageEventRepositories.test.ts`:

```ts
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, test } from 'vitest';
import { createId } from '../../src/contracts/ids';
import { openDatabase } from '../../src/db/connection';
import { runMigrations } from '../../src/db/migrate';
import { EventRepository } from '../../src/db/repositories/eventRepository';
import { MessageRepository } from '../../src/db/repositories/messageRepository';
import { SessionRepository } from '../../src/db/repositories/sessionRepository';

function createTestDb() {
  const dir = mkdtempSync(join(tmpdir(), 'turing-repo-'));
  const db = openDatabase(join(dir, 'orchestrator.db'));
  runMigrations(db);
  return db;
}

describe('session/message/event repositories', () => {
  test('persists a session, message, and replayable event', () => {
    const db = createTestDb();
    const sessions = new SessionRepository(db);
    const messages = new MessageRepository(db);
    const events = new EventRepository(db);

    const session = sessions.create({ title: 'Test session' });
    const traceId = createId('trace');
    const message = messages.create({
      sessionId: session.sessionId,
      traceId,
      role: 'user',
      contentType: 'text',
      contentJson: JSON.stringify({ text: 'hello' }),
    });
    const event = events.append({
      sessionId: session.sessionId,
      runId: createId('run'),
      traceId,
      type: 'message.delta',
      payloadJson: JSON.stringify({ messageId: message.id, delta: 'hi' }),
    });

    expect(messages.listBySession(session.sessionId)).toHaveLength(1);
    expect(events.replayAfter(session.sessionId, 0)).toMatchObject([{ sequence: event.sequence }]);
  });
});
```

- [ ] **Step 3: Run tests and verify they fail**

Run:

```bash
cd turing-backend/orchestrator
npm run test:run -- tests/db/migrate.test.ts tests/db/sessionMessageEventRepositories.test.ts
```

Expected: FAIL because DB modules and migrations do not exist.

- [ ] **Step 4: Add ID helper**

Create `turing-backend/orchestrator/src/contracts/ids.ts`:

```ts
import { ulid } from 'ulid';

export type IdPrefix = 'user' | 'rt' | 'sess' | 'msg' | 'run' | 'step' | 'evt' | 'job' | 'tool' | 'call' | 'appr' | 'audit' | 'trace';

export function createId(prefix: IdPrefix): string {
  return `${prefix}_${ulid()}`;
}
```

- [ ] **Step 5: Add migration SQL**

Create `turing-backend/orchestrator/migrations/001_initial.sql`:

```sql
CREATE TABLE IF NOT EXISTS schema_migrations (
  version TEXT PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS refresh_tokens (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  revoked_at TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value_json TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  session_id TEXT PRIMARY KEY,
  title TEXT,
  model_provider TEXT NOT NULL DEFAULT 'ollama',
  model_name TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS messages (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
  trace_id TEXT NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system', 'tool')),
  content_type TEXT NOT NULL,
  content_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_messages_session_created ON messages(session_id, created_at);

CREATE TABLE IF NOT EXISTS agent_runs (
  run_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
  trace_id TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'waiting_approval', 'completed', 'failed', 'cancelled')),
  model_provider TEXT NOT NULL,
  model_name TEXT NOT NULL,
  input_message_id TEXT REFERENCES messages(id),
  output_message_id TEXT REFERENCES messages(id),
  error_json TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  completed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_agent_runs_session_created ON agent_runs(session_id, created_at);

CREATE TABLE IF NOT EXISTS agent_run_steps (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES agent_runs(run_id) ON DELETE CASCADE,
  sequence INTEGER NOT NULL,
  status TEXT NOT NULL,
  label TEXT NOT NULL,
  detail_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(run_id, sequence)
);

CREATE TABLE IF NOT EXISTS events (
  event_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(session_id) ON DELETE CASCADE,
  run_id TEXT,
  trace_id TEXT NOT NULL,
  sequence INTEGER NOT NULL,
  type TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(session_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_events_session_sequence ON events(session_id, sequence);

CREATE TABLE IF NOT EXISTS jobs (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled')),
  payload_json TEXT NOT NULL,
  error_json TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tools (
  id TEXT PRIMARY KEY,
  server_name TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  policy TEXT NOT NULL CHECK (policy IN ('safe', 'approval_required', 'disabled')),
  schema_json TEXT NOT NULL DEFAULT '{}',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(server_name, tool_name)
);

CREATE TABLE IF NOT EXISTS tool_calls (
  id TEXT PRIMARY KEY,
  run_id TEXT,
  trace_id TEXT NOT NULL,
  agent_id TEXT NOT NULL,
  server_name TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  args_json TEXT NOT NULL,
  result_json TEXT,
  status TEXT NOT NULL CHECK (status IN ('requested', 'ok', 'error', 'denied')),
  duration_ms INTEGER,
  created_at TEXT NOT NULL,
  completed_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_tool_calls_trace ON tool_calls(trace_id, created_at);

CREATE TABLE IF NOT EXISTS approvals (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL,
  tool_call_id TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending', 'approved', 'denied', 'expired')),
  requested_action_json TEXT NOT NULL,
  decided_at TEXT,
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_logs (
  id TEXT PRIMARY KEY,
  trace_id TEXT,
  session_id TEXT,
  run_id TEXT,
  tool_call_id TEXT,
  actor TEXT NOT NULL,
  action TEXT NOT NULL,
  target TEXT NOT NULL,
  metadata_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL
);
```

- [ ] **Step 6: Implement DB connection and migrations**

Create `turing-backend/orchestrator/src/db/connection.ts`:

```ts
import Database from 'better-sqlite3';

export type TuringDatabase = Database.Database;

export function openDatabase(path: string): TuringDatabase {
  const db = new Database(path);
  db.pragma('journal_mode = WAL');
  db.pragma('foreign_keys = ON');
  db.pragma('busy_timeout = 5000');
  return db;
}
```

Create `turing-backend/orchestrator/src/db/migrate.ts`:

```ts
import { existsSync, readdirSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { type TuringDatabase } from './connection.js';

const currentDir = dirname(fileURLToPath(import.meta.url));
const defaultMigrationsDir = join(currentDir, '../../migrations');

export function runMigrations(db: TuringDatabase, migrationsDir = defaultMigrationsDir): void {
  db.exec(`
    CREATE TABLE IF NOT EXISTS schema_migrations (
      version TEXT PRIMARY KEY,
      applied_at TEXT NOT NULL
    );
  `);

  if (!existsSync(migrationsDir)) {
    throw new Error(`Migrations directory not found: ${migrationsDir}`);
  }

  const applied = new Set(
    db.prepare('SELECT version FROM schema_migrations').all().map((row) => (row as { version: string }).version),
  );

  const files = readdirSync(migrationsDir).filter((file) => file.endsWith('.sql')).sort();
  const insertMigration = db.prepare('INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)');

  for (const file of files) {
    if (applied.has(file)) continue;
    const sql = readFileSync(join(migrationsDir, file), 'utf8');
    const apply = db.transaction(() => {
      db.exec(sql);
      insertMigration.run(file, new Date().toISOString());
    });
    apply();
  }
}
```

- [ ] **Step 7: Implement session, message, and event repositories**

Create `turing-backend/orchestrator/src/db/repositories/sessionRepository.ts`:

```ts
import { createId } from '../../contracts/ids.js';
import { type TuringDatabase } from '../connection.js';

export interface SessionRecord {
  sessionId: string;
  title: string | null;
  modelProvider: string;
  modelName: string | null;
  createdAt: string;
  updatedAt: string;
}

export class SessionRepository {
  constructor(private readonly db: TuringDatabase) {}

  create(input: { title?: string; modelProvider?: string; modelName?: string | null }): SessionRecord {
    const now = new Date().toISOString();
    const record: SessionRecord = {
      sessionId: createId('sess'),
      title: input.title ?? null,
      modelProvider: input.modelProvider ?? 'ollama',
      modelName: input.modelName ?? null,
      createdAt: now,
      updatedAt: now,
    };
    this.db
      .prepare(
        `INSERT INTO sessions (session_id, title, model_provider, model_name, created_at, updated_at)
         VALUES (@sessionId, @title, @modelProvider, @modelName, @createdAt, @updatedAt)`,
      )
      .run(record);
    return record;
  }

  get(sessionId: string): SessionRecord | null {
    const row = this.db.prepare('SELECT * FROM sessions WHERE session_id = ?').get(sessionId) as Record<string, unknown> | undefined;
    if (!row) return null;
    return mapSession(row);
  }

  list(): SessionRecord[] {
    return this.db
      .prepare('SELECT * FROM sessions ORDER BY updated_at DESC')
      .all()
      .map((row) => mapSession(row as Record<string, unknown>));
  }
}

function mapSession(row: Record<string, unknown>): SessionRecord {
  return {
    sessionId: String(row.session_id),
    title: row.title === null ? null : String(row.title),
    modelProvider: String(row.model_provider),
    modelName: row.model_name === null ? null : String(row.model_name),
    createdAt: String(row.created_at),
    updatedAt: String(row.updated_at),
  };
}
```

Create `turing-backend/orchestrator/src/db/repositories/messageRepository.ts`:

```ts
import { createId } from '../../contracts/ids.js';
import { type TuringDatabase } from '../connection.js';

export type MessageRole = 'user' | 'assistant' | 'system' | 'tool';

export interface MessageRecord {
  id: string;
  sessionId: string;
  traceId: string;
  role: MessageRole;
  contentType: string;
  contentJson: string;
  createdAt: string;
}

export class MessageRepository {
  constructor(private readonly db: TuringDatabase) {}

  create(input: Omit<MessageRecord, 'id' | 'createdAt'>): MessageRecord {
    const record: MessageRecord = {
      id: createId('msg'),
      createdAt: new Date().toISOString(),
      ...input,
    };
    this.db
      .prepare(
        `INSERT INTO messages (id, session_id, trace_id, role, content_type, content_json, created_at)
         VALUES (@id, @sessionId, @traceId, @role, @contentType, @contentJson, @createdAt)`,
      )
      .run(record);
    return record;
  }

  listBySession(sessionId: string, limit = 100): MessageRecord[] {
    return this.db
      .prepare('SELECT * FROM messages WHERE session_id = ? ORDER BY created_at ASC LIMIT ?')
      .all(sessionId, limit)
      .map((row) => mapMessage(row as Record<string, unknown>));
  }
}

function mapMessage(row: Record<string, unknown>): MessageRecord {
  return {
    id: String(row.id),
    sessionId: String(row.session_id),
    traceId: String(row.trace_id),
    role: String(row.role) as MessageRole,
    contentType: String(row.content_type),
    contentJson: String(row.content_json),
    createdAt: String(row.created_at),
  };
}
```

Create `turing-backend/orchestrator/src/db/repositories/eventRepository.ts`:

```ts
import { createId } from '../../contracts/ids.js';
import { type TuringDatabase } from '../connection.js';

export interface EventRecord {
  eventId: string;
  sessionId: string;
  runId: string | null;
  traceId: string;
  sequence: number;
  type: string;
  payloadJson: string;
  createdAt: string;
}

export class EventRepository {
  constructor(private readonly db: TuringDatabase) {}

  append(input: Omit<EventRecord, 'eventId' | 'sequence' | 'createdAt'>): EventRecord {
    const nextSequence =
      ((this.db.prepare('SELECT COALESCE(MAX(sequence), 0) AS value FROM events WHERE session_id = ?').get(input.sessionId) as { value: number }).value ?? 0) + 1;
    const record: EventRecord = {
      eventId: createId('evt'),
      sequence: nextSequence,
      createdAt: new Date().toISOString(),
      ...input,
    };
    this.db
      .prepare(
        `INSERT INTO events (event_id, session_id, run_id, trace_id, sequence, type, payload_json, created_at)
         VALUES (@eventId, @sessionId, @runId, @traceId, @sequence, @type, @payloadJson, @createdAt)`,
      )
      .run(record);
    return record;
  }

  replayAfter(sessionId: string, lastSequence: number): EventRecord[] {
    return this.db
      .prepare('SELECT * FROM events WHERE session_id = ? AND sequence > ? ORDER BY sequence ASC')
      .all(sessionId, lastSequence)
      .map((row) => mapEvent(row as Record<string, unknown>));
  }
}

function mapEvent(row: Record<string, unknown>): EventRecord {
  return {
    eventId: String(row.event_id),
    sessionId: String(row.session_id),
    runId: row.run_id === null ? null : String(row.run_id),
    traceId: String(row.trace_id),
    sequence: Number(row.sequence),
    type: String(row.type),
    payloadJson: String(row.payload_json),
    createdAt: String(row.created_at),
  };
}
```

- [ ] **Step 8: Run DB tests and build**

Run:

```bash
cd turing-backend/orchestrator
npm run test:run -- tests/db/migrate.test.ts tests/db/sessionMessageEventRepositories.test.ts
npm run build
```

Expected: tests and build pass.

- [ ] **Step 9: Commit**

Run:

```bash
git add turing-backend/orchestrator
git commit -m "feat: add orchestrator sqlite persistence" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 3: Implement first-run setup, login, refresh tokens, and authenticated hooks

**Files:**
- Create: `turing-backend/orchestrator/src/db/repositories/authRepository.ts`
- Create: `turing-backend/orchestrator/src/auth/passwords.ts`
- Create: `turing-backend/orchestrator/src/auth/tokens.ts`
- Create: `turing-backend/orchestrator/src/auth/authService.ts`
- Create: `turing-backend/orchestrator/src/auth/authPlugin.ts`
- Modify: `turing-backend/orchestrator/src/app.ts`
- Create: `turing-backend/orchestrator/tests/auth/authRoutes.test.ts`

- [ ] **Step 1: Write failing auth route tests**

Create `turing-backend/orchestrator/tests/auth/authRoutes.test.ts`:

```ts
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, test } from 'vitest';
import { buildApp } from '../../src/app';
import { loadConfig } from '../../src/config';

async function createApp() {
  const dataDir = mkdtempSync(join(tmpdir(), 'turing-auth-'));
  return buildApp({
    config: loadConfig({
      TURING_DATA_DIR: dataDir,
      TURING_JWT_SECRET: 'test-secret-with-at-least-32-characters',
    }),
  });
}

describe('auth routes', () => {
  test('setup creates the first admin user and blocks a second setup', async () => {
    const app = await createApp();

    const setup = await app.inject({
      method: 'POST',
      url: '/setup',
      payload: { username: 'miguel', password: 'correct horse battery staple' },
    });
    expect(setup.statusCode).toBe(201);
    expect(setup.json()).toHaveProperty('accessToken');
    expect(setup.json()).toHaveProperty('refreshToken');

    const second = await app.inject({
      method: 'POST',
      url: '/setup',
      payload: { username: 'other', password: 'correct horse battery staple' },
    });
    expect(second.statusCode).toBe(409);
  });

  test('login and refresh return new tokens', async () => {
    const app = await createApp();
    await app.inject({
      method: 'POST',
      url: '/setup',
      payload: { username: 'miguel', password: 'correct horse battery staple' },
    });

    const login = await app.inject({
      method: 'POST',
      url: '/auth/login',
      payload: { username: 'miguel', password: 'correct horse battery staple' },
    });
    expect(login.statusCode).toBe(200);

    const refresh = await app.inject({
      method: 'POST',
      url: '/auth/refresh',
      payload: { refreshToken: login.json().refreshToken },
    });
    expect(refresh.statusCode).toBe(200);
    expect(refresh.json().accessToken).not.toEqual(login.json().accessToken);
  });

  test('protected routes require bearer auth', async () => {
    const app = await createApp();
    const unauthorized = await app.inject({ method: 'GET', url: '/sessions' });
    expect(unauthorized.statusCode).toBe(401);
  });
});
```

- [ ] **Step 2: Run test and verify it fails**

Run:

```bash
cd turing-backend/orchestrator
npm run test:run -- tests/auth/authRoutes.test.ts
```

Expected: FAIL because auth modules and routes do not exist.

- [ ] **Step 3: Add auth repository and password/token services**

Create the auth repository with methods `hasUsers()`, `createUser()`, `findUserByUsername()`, `storeRefreshToken()`, `findValidRefreshToken()`, and `revokeRefreshToken()`. Use SHA-256 to store refresh-token hashes, not raw tokens.

Core code for `turing-backend/orchestrator/src/auth/passwords.ts`:

```ts
import bcrypt from 'bcryptjs';

const PASSWORD_ROUNDS = 12;

export async function hashPassword(password: string): Promise<string> {
  return bcrypt.hash(password, PASSWORD_ROUNDS);
}

export async function verifyPassword(password: string, hash: string): Promise<boolean> {
  return bcrypt.compare(password, hash);
}
```

Core code for `turing-backend/orchestrator/src/auth/tokens.ts`:

```ts
import { createHash, randomBytes } from 'node:crypto';
import { SignJWT, jwtVerify } from 'jose';
import { type TuringConfig } from '../config.js';

export interface AccessTokenClaims {
  userId: string;
  username: string;
}

export function hashOpaqueToken(token: string): string {
  return createHash('sha256').update(token).digest('hex');
}

export function createRefreshToken(): string {
  return randomBytes(48).toString('base64url');
}

export async function createAccessToken(config: TuringConfig, claims: AccessTokenClaims): Promise<string> {
  const secret = new TextEncoder().encode(config.jwt.secret);
  return new SignJWT({ username: claims.username })
    .setProtectedHeader({ alg: 'HS256' })
    .setSubject(claims.userId)
    .setIssuedAt()
    .setExpirationTime(`${config.jwt.accessTokenTtlSeconds}s`)
    .sign(secret);
}

export async function verifyAccessToken(config: TuringConfig, token: string): Promise<AccessTokenClaims> {
  const secret = new TextEncoder().encode(config.jwt.secret);
  const result = await jwtVerify(token, secret);
  const userId = result.payload.sub;
  const username = result.payload.username;
  if (!userId || typeof username !== 'string') throw new Error('Invalid access token');
  return { userId, username };
}
```

- [ ] **Step 4: Add auth plugin and routes**

In `authPlugin.ts`, decorate Fastify with `authenticate` and require bearer JWTs for protected routes. Public routes are `/health`, `/version`, `/setup`, `/auth/login`, and `/auth/refresh`.

Add route behavior:

- `POST /setup`: requires username length >= 2 and password length >= 12; returns 409 if any user exists.
- `POST /auth/login`: returns 401 on invalid credentials.
- `POST /auth/refresh`: rotates refresh token by revoking the old token and storing a new token.
- `POST /auth/logout`: revokes the supplied refresh token and returns 204.

- [ ] **Step 5: Wire app initialization**

Modify `buildApp` so it creates the data directory, opens SQLite at `${config.dataDir}/orchestrator.db`, runs migrations, creates repositories/services, registers auth routes, and registers auth hooks before protected routes.

- [ ] **Step 6: Run auth tests and existing tests**

Run:

```bash
cd turing-backend/orchestrator
npm run test:run -- tests/auth/authRoutes.test.ts tests/config.test.ts tests/health.test.ts tests/db/migrate.test.ts tests/db/sessionMessageEventRepositories.test.ts
npm run build
```

Expected: tests and build pass.

- [ ] **Step 7: Commit**

Run:

```bash
git add turing-backend/orchestrator
git commit -m "feat: add local jwt authentication" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 4: Implement REST sessions, messages, runs, and a stub streaming executor

**Files:**
- Create: `turing-backend/orchestrator/src/db/repositories/runRepository.ts`
- Create: `turing-backend/orchestrator/src/contracts/events.ts`
- Create: `turing-backend/orchestrator/src/events/eventStore.ts`
- Create: `turing-backend/orchestrator/src/agents/agentExecutor.ts`
- Create: `turing-backend/orchestrator/src/agents/generalAssistantExecutor.ts`
- Create: `turing-backend/orchestrator/src/agents/runService.ts`
- Create: `turing-backend/orchestrator/src/api/sessionRoutes.ts`
- Create: `turing-backend/orchestrator/src/api/agentRoutes.ts`
- Modify: `turing-backend/orchestrator/src/app.ts`
- Create: `turing-backend/orchestrator/tests/api/sessionMessageRoutes.test.ts`

- [ ] **Step 1: Write failing REST run test**

Create `turing-backend/orchestrator/tests/api/sessionMessageRoutes.test.ts`:

```ts
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, test } from 'vitest';
import { buildApp } from '../../src/app';
import { loadConfig } from '../../src/config';

async function authedApp() {
  const app = await buildApp({
    config: loadConfig({
      TURING_DATA_DIR: mkdtempSync(join(tmpdir(), 'turing-api-')),
      TURING_JWT_SECRET: 'test-secret-with-at-least-32-characters',
    }),
  });
  const setup = await app.inject({
    method: 'POST',
    url: '/setup',
    payload: { username: 'miguel', password: 'correct horse battery staple' },
  });
  return { app, auth: `Bearer ${setup.json().accessToken}` };
}

describe('session and message routes', () => {
  test('creates a session and starts an agent run for a message', async () => {
    const { app, auth } = await authedApp();

    const session = await app.inject({
      method: 'POST',
      url: '/sessions',
      headers: { authorization: auth },
      payload: { title: 'First chat' },
    });
    expect(session.statusCode).toBe(201);

    const send = await app.inject({
      method: 'POST',
      url: `/sessions/${session.json().sessionId}/messages`,
      headers: { authorization: auth },
      payload: { text: 'Hello Turing', modelProvider: 'ollama' },
    });
    expect(send.statusCode).toBe(202);
    expect(send.json()).toMatchObject({
      sessionId: session.json().sessionId,
      role: 'user',
    });
    expect(send.json().runId).toMatch(/^run_/);

    const messages = await app.inject({
      method: 'GET',
      url: `/sessions/${session.json().sessionId}/messages`,
      headers: { authorization: auth },
    });
    expect(messages.statusCode).toBe(200);
    expect(messages.json().messages[0].content.text).toBe('Hello Turing');
  });

  test('rejects cloud provider when not explicitly configured', async () => {
    const { app, auth } = await authedApp();
    const session = await app.inject({ method: 'POST', url: '/sessions', headers: { authorization: auth }, payload: {} });

    const send = await app.inject({
      method: 'POST',
      url: `/sessions/${session.json().sessionId}/messages`,
      headers: { authorization: auth },
      payload: { text: 'Use cloud', modelProvider: 'openai' },
    });

    expect(send.statusCode).toBe(400);
    expect(send.json().error.code).toBe('MODEL_PROVIDER_UNAVAILABLE');
  });
});
```

- [ ] **Step 2: Run test and verify it fails**

Run:

```bash
cd turing-backend/orchestrator
npm run test:run -- tests/api/sessionMessageRoutes.test.ts
```

Expected: FAIL because routes and run service do not exist.

- [ ] **Step 3: Add event contract and run service interfaces**

Create `turing-backend/orchestrator/src/contracts/events.ts`:

```ts
export type TuringEventType =
  | 'message.delta'
  | 'message.completed'
  | 'agent.run.started'
  | 'agent.run.step'
  | 'agent.run.completed'
  | 'tool.call.started'
  | 'tool.call.completed'
  | 'tool.call.failed'
  | 'approval.requested'
  | 'error'
  | 'system';

export interface TuringEvent<TPayload = unknown> {
  eventId: string;
  sessionId: string;
  runId: string | null;
  traceId: string;
  sequence: number;
  type: TuringEventType;
  createdAt: string;
  payload: TPayload;
}
```

Create `turing-backend/orchestrator/src/agents/agentExecutor.ts`:

```ts
import { type TuringEventType } from '../contracts/events.js';

export interface AgentExecutionInput {
  sessionId: string;
  runId: string;
  traceId: string;
  messageId: string;
  text: string;
  modelProvider: 'ollama' | 'openai';
  modelName: string;
}

export interface AgentEvent {
  type: TuringEventType;
  payload: Record<string, unknown>;
}

export interface AgentExecutor {
  execute(input: AgentExecutionInput): AsyncIterable<AgentEvent>;
}
```

- [ ] **Step 4: Add stub `general_assistant` executor**

Create `turing-backend/orchestrator/src/agents/generalAssistantExecutor.ts`:

```ts
import { type AgentEvent, type AgentExecutionInput, type AgentExecutor } from './agentExecutor.js';

export class GeneralAssistantExecutor implements AgentExecutor {
  async *execute(input: AgentExecutionInput): AsyncIterable<AgentEvent> {
    yield {
      type: 'agent.run.started',
      payload: { agentId: 'general_assistant', modelProvider: input.modelProvider, modelName: input.modelName },
    };
    yield {
      type: 'message.delta',
      payload: { delta: `I received: ${input.text}` },
    };
    yield {
      type: 'message.completed',
      payload: { text: `I received: ${input.text}` },
    };
    yield {
      type: 'agent.run.completed',
      payload: { status: 'completed' },
    };
  }
}
```

- [ ] **Step 5: Implement REST routes and run service**

Implement:

- `POST /sessions`: creates a session and returns `{ sessionId, title, createdAt, updatedAt }`.
- `GET /sessions`: returns `{ sessions: [...] }`.
- `GET /sessions/:sessionId`: returns 404 when missing.
- `GET /sessions/:sessionId/messages`: returns parsed JSON content as `{ messages: [...] }`.
- `POST /sessions/:sessionId/messages`: validates non-empty text, rejects `openai` when disabled, persists user message, creates run, starts executor asynchronously, and returns 202 with `{ sessionId, messageId, runId, traceId, role: 'user' }`.
- `GET /agents`: returns `general_assistant`.

Use `setImmediate` for the first async in-process run dispatch so the REST response returns before execution completes.

- [ ] **Step 6: Run route tests and build**

Run:

```bash
cd turing-backend/orchestrator
npm run test:run -- tests/api/sessionMessageRoutes.test.ts
npm run test:run
npm run build
```

Expected: tests and build pass.

- [ ] **Step 7: Commit**

Run:

```bash
git add turing-backend/orchestrator
git commit -m "feat: add session message run api" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 5: Add authenticated WebSocket streaming, replay, and slow-client-safe deltas

**Files:**
- Create: `turing-backend/orchestrator/src/ws/sessionSocket.ts`
- Modify: `turing-backend/orchestrator/src/events/eventStore.ts`
- Modify: `turing-backend/orchestrator/src/agents/runService.ts`
- Modify: `turing-backend/orchestrator/src/app.ts`
- Create: `turing-backend/orchestrator/tests/ws/sessionSocket.test.ts`

- [ ] **Step 1: Write failing replay/event-store test**

Create `turing-backend/orchestrator/tests/ws/sessionSocket.test.ts`:

```ts
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { describe, expect, test } from 'vitest';
import { buildApp } from '../../src/app';
import { loadConfig } from '../../src/config';

async function setupApp() {
  const app = await buildApp({
    config: loadConfig({
      TURING_DATA_DIR: mkdtempSync(join(tmpdir(), 'turing-ws-')),
      TURING_JWT_SECRET: 'test-secret-with-at-least-32-characters',
    }),
  });
  const setup = await app.inject({
    method: 'POST',
    url: '/setup',
    payload: { username: 'miguel', password: 'correct horse battery staple' },
  });
  return { app, token: setup.json().accessToken as string };
}

describe('session websocket support', () => {
  test('rejects missing token at handshake endpoint', async () => {
    const { app } = await setupApp();
    const response = await app.inject({ method: 'GET', url: '/ws?sessionId=sess_missing&lastSequence=0' });
    expect(response.statusCode).toBe(401);
  });

  test('exposes replay endpoint for reconnect fallback', async () => {
    const { app, token } = await setupApp();
    const session = await app.inject({
      method: 'POST',
      url: '/sessions',
      headers: { authorization: `Bearer ${token}` },
      payload: {},
    });
    await app.inject({
      method: 'POST',
      url: `/sessions/${session.json().sessionId}/messages`,
      headers: { authorization: `Bearer ${token}` },
      payload: { text: 'Replay me' },
    });

    const replay = await app.inject({
      method: 'GET',
      url: `/sessions/${session.json().sessionId}/events?after=0`,
      headers: { authorization: `Bearer ${token}` },
    });

    expect(replay.statusCode).toBe(200);
    expect(replay.json().events.length).toBeGreaterThan(0);
    expect(replay.json().events[0].sequence).toBe(1);
  });
});
```

- [ ] **Step 2: Run test and verify it fails**

Run:

```bash
cd turing-backend/orchestrator
npm run test:run -- tests/ws/sessionSocket.test.ts
```

Expected: FAIL because `/ws` auth behavior and event replay route do not exist.

- [ ] **Step 3: Add event replay route**

Add `GET /sessions/:sessionId/events?after=<sequence>` to the session routes. It should:

- require auth
- return 404 if session does not exist
- parse `after` as integer >= 0
- return `{ events: TuringEvent[] }`
- parse `payloadJson` into `payload`

- [ ] **Step 4: Add WebSocket session stream**

Create `sessionSocket.ts` with:

- JWT token accepted as `Authorization: Bearer <token>` or `?token=<token>`.
- Required query params: `sessionId`; optional `lastSequence`.
- On connect, replay persisted events with sequence > `lastSequence`.
- Keep an in-memory `Map<sessionId, Set<WebSocket>>`.
- Export `broadcastEvent(event)` used by `EventStore.append`.
- Before sending, check `socket.readyState === socket.OPEN`.
- Coalesce model deltas in `RunService` by buffering small deltas for up to 50 ms or 120 characters before append/broadcast.

- [ ] **Step 5: Run WebSocket tests and build**

Run:

```bash
cd turing-backend/orchestrator
npm run test:run -- tests/ws/sessionSocket.test.ts
npm run test:run
npm run build
```

Expected: tests and build pass.

- [ ] **Step 6: Commit**

Run:

```bash
git add turing-backend/orchestrator
git commit -m "feat: add session websocket events" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 6: Replace stub executor with Ollama and OpenAI-compatible streaming providers

**Files:**
- Create: `turing-backend/orchestrator/src/llm/modelProvider.ts`
- Create: `turing-backend/orchestrator/src/llm/ollamaProvider.ts`
- Create: `turing-backend/orchestrator/src/llm/openAiCompatibleProvider.ts`
- Create: `turing-backend/orchestrator/src/llm/modelRouter.ts`
- Modify: `turing-backend/orchestrator/src/agents/generalAssistantExecutor.ts`
- Modify: `turing-backend/orchestrator/src/app.ts`
- Create: `turing-backend/orchestrator/tests/llm/modelProviders.test.ts`

- [ ] **Step 1: Write failing provider tests**

Create `turing-backend/orchestrator/tests/llm/modelProviders.test.ts`:

```ts
import { afterEach, describe, expect, test, vi } from 'vitest';
import { OllamaProvider } from '../../src/llm/ollamaProvider';
import { OpenAiCompatibleProvider } from '../../src/llm/openAiCompatibleProvider';

afterEach(() => {
  vi.restoreAllMocks();
});

function streamResponse(lines: string[]) {
  return new Response(lines.join('\n'), {
    status: 200,
    headers: { 'content-type': 'application/jsonl' },
  });
}

describe('model providers', () => {
  test('Ollama streams response chunks', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        streamResponse([
          JSON.stringify({ message: { content: 'hello' }, done: false }),
          JSON.stringify({ message: { content: ' world' }, done: false }),
          JSON.stringify({ done: true }),
        ]),
      ),
    );
    const provider = new OllamaProvider({ baseUrl: 'http://ollama.test', model: 'llama3.2' });
    const chunks: string[] = [];

    for await (const chunk of provider.streamChat([{ role: 'user', content: 'hi' }])) chunks.push(chunk);

    expect(chunks).toEqual(['hello', ' world']);
  });

  test('OpenAI-compatible provider streams delta chunks', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async () =>
        new Response('data: {"choices":[{"delta":{"content":"hi"}}]}\n\ndata: {"choices":[{"delta":{"content":"!"}}]}\n\ndata: [DONE]\n\n', {
          status: 200,
          headers: { 'content-type': 'text/event-stream' },
        }),
      ),
    );
    const provider = new OpenAiCompatibleProvider({ baseUrl: 'https://api.test/v1', apiKey: 'sk-test', model: 'gpt-test' });
    const chunks: string[] = [];

    for await (const chunk of provider.streamChat([{ role: 'user', content: 'hi' }])) chunks.push(chunk);

    expect(chunks).toEqual(['hi', '!']);
  });
});
```

- [ ] **Step 2: Run test and verify it fails**

Run:

```bash
cd turing-backend/orchestrator
npm run test:run -- tests/llm/modelProviders.test.ts
```

Expected: FAIL because provider modules do not exist.

- [ ] **Step 3: Add provider interface**

Create `turing-backend/orchestrator/src/llm/modelProvider.ts`:

```ts
export type ChatRole = 'system' | 'user' | 'assistant' | 'tool';

export interface ChatMessageInput {
  role: ChatRole;
  content: string;
}

export interface ModelProvider {
  readonly id: 'ollama' | 'openai';
  readonly model: string;
  streamChat(messages: ChatMessageInput[], signal?: AbortSignal): AsyncIterable<string>;
}
```

- [ ] **Step 4: Implement Ollama provider**

Implement `OllamaProvider.streamChat()` using `POST ${baseUrl}/api/chat` with body:

```json
{
  "model": "llama3.2",
  "stream": true,
  "messages": [{ "role": "user", "content": "hi" }]
}
```

Parse newline-delimited JSON. Yield `message.content` chunks. Throw an error containing status code and response body for non-2xx responses.

- [ ] **Step 5: Implement OpenAI-compatible provider**

Implement `OpenAiCompatibleProvider.streamChat()` using `POST ${baseUrl}/chat/completions` with bearer API key and body:

```json
{
  "model": "gpt-4o-mini",
  "stream": true,
  "messages": [{ "role": "user", "content": "hi" }]
}
```

Parse server-sent event lines that start with `data: `. Stop on `[DONE]`. Yield `choices[0].delta.content` chunks. Throw an error containing status code and response body for non-2xx responses.

- [ ] **Step 6: Update executor to stream through selected provider**

Modify `GeneralAssistantExecutor` so it receives a `ModelRouter`, builds messages:

```ts
[
  { role: 'system', content: 'You are TuringAgent, a local-first personal AI assistant. Be concise, useful, and explicit when a tool or approval is needed.' },
  { role: 'user', content: input.text }
]
```

It should emit:

- `agent.run.started`
- one or more `message.delta`
- `message.completed` with full text
- `agent.run.completed`

On provider error, emit `error` and end run as failed in `RunService`.

- [ ] **Step 7: Run provider tests and route tests**

Run:

```bash
cd turing-backend/orchestrator
npm run test:run -- tests/llm/modelProviders.test.ts tests/api/sessionMessageRoutes.test.ts tests/ws/sessionSocket.test.ts
npm run build
```

Expected: tests and build pass.

- [ ] **Step 8: Commit**

Run:

```bash
git add turing-backend/orchestrator
git commit -m "feat: add streaming model providers" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 7: Add MCP tool registry, policy checks, approvals, and audit logs

**Files:**
- Create: `turing-backend/orchestrator/src/db/repositories/toolRepository.ts`
- Create: `turing-backend/orchestrator/src/db/repositories/approvalRepository.ts`
- Create: `turing-backend/orchestrator/src/db/repositories/auditRepository.ts`
- Create: `turing-backend/orchestrator/src/tools/toolPolicy.ts`
- Create: `turing-backend/orchestrator/src/tools/mcpClient.ts`
- Create: `turing-backend/orchestrator/src/tools/toolService.ts`
- Create: `turing-backend/orchestrator/src/security/approvalService.ts`
- Create: `turing-backend/orchestrator/src/api/toolRoutes.ts`
- Create: `turing-backend/orchestrator/src/api/approvalRoutes.ts`
- Create: `turing-backend/orchestrator/tests/tools/toolPolicyApproval.test.ts`

- [ ] **Step 1: Write failing policy and approval tests**

Create `turing-backend/orchestrator/tests/tools/toolPolicyApproval.test.ts`:

```ts
import { describe, expect, test, vi } from 'vitest';
import { ToolPolicyService } from '../../src/tools/toolPolicy';

describe('tool policy', () => {
  test('allows safe tools, blocks disabled tools, and requires approval for write tools', () => {
    const policy = new ToolPolicyService([
      { serverName: 'system', toolName: 'time.now', policy: 'safe' },
      { serverName: 'files', toolName: 'files.update', policy: 'approval_required' },
      { serverName: 'files', toolName: 'files.delete', policy: 'disabled' },
    ]);

    expect(policy.decision('system', 'time.now')).toEqual({ kind: 'allow' });
    expect(policy.decision('files', 'files.update')).toEqual({ kind: 'approval_required' });
    expect(policy.decision('files', 'files.delete')).toEqual({ kind: 'deny', reason: 'Tool is disabled' });
    expect(policy.decision('unknown', 'tool')).toEqual({ kind: 'deny', reason: 'Tool is not allowlisted' });
  });
});
```

- [ ] **Step 2: Run test and verify it fails**

Run:

```bash
cd turing-backend/orchestrator
npm run test:run -- tests/tools/toolPolicyApproval.test.ts
```

Expected: FAIL because tool policy module does not exist.

- [ ] **Step 3: Implement tool policy**

Create `turing-backend/orchestrator/src/tools/toolPolicy.ts`:

```ts
export type ToolPolicy = 'safe' | 'approval_required' | 'disabled';

export interface ToolPolicyRecord {
  serverName: string;
  toolName: string;
  policy: ToolPolicy;
}

export type ToolDecision =
  | { kind: 'allow' }
  | { kind: 'approval_required' }
  | { kind: 'deny'; reason: string };

export class ToolPolicyService {
  private readonly policies: Map<string, ToolPolicy>;

  constructor(records: ToolPolicyRecord[]) {
    this.policies = new Map(records.map((record) => [`${record.serverName}:${record.toolName}`, record.policy]));
  }

  decision(serverName: string, toolName: string): ToolDecision {
    const policy = this.policies.get(`${serverName}:${toolName}`);
    if (!policy) return { kind: 'deny', reason: 'Tool is not allowlisted' };
    if (policy === 'disabled') return { kind: 'deny', reason: 'Tool is disabled' };
    if (policy === 'approval_required') return { kind: 'approval_required' };
    return { kind: 'allow' };
  }
}
```

- [ ] **Step 4: Implement MCP client and repositories**

Create `McpClient.callTool()` that sends JSON-RPC:

```json
{
  "jsonrpc": "2.0",
  "id": "call_...",
  "method": "tools/call",
  "params": {
    "name": "time.now",
    "arguments": {}
  }
}
```

Persist every call in `tool_calls`. Persist audit rows for:

- `tool.allowed`
- `tool.denied`
- `approval.requested`
- `approval.approved`
- `approval.denied`
- `tool.completed`
- `tool.failed`

- [ ] **Step 5: Implement approval routes**

`POST /approvals/:approvalId/approve`:

- requires auth
- only accepts pending approvals
- marks approval approved
- invokes the stored tool call through `ToolService`
- emits `tool.call.started`, `tool.call.completed` or `tool.call.failed`

`POST /approvals/:approvalId/deny`:

- requires auth
- only accepts pending approvals
- marks approval denied
- emits `agent.run.step` with denied status

- [ ] **Step 6: Add default tool registry seed**

When migrations complete, seed these tools if absent:

```ts
[
  { serverName: 'system', toolName: 'health.check', policy: 'safe' },
  { serverName: 'system', toolName: 'time.now', policy: 'safe' },
  { serverName: 'system', toolName: 'echo', policy: 'safe' },
  { serverName: 'system', toolName: 'system.info', policy: 'safe' },
  { serverName: 'files', toolName: 'files.list', policy: 'safe' },
  { serverName: 'files', toolName: 'files.search', policy: 'safe' },
  { serverName: 'files', toolName: 'files.read', policy: 'safe' },
  { serverName: 'files', toolName: 'files.create', policy: 'approval_required' },
  { serverName: 'files', toolName: 'files.update', policy: 'approval_required' },
  { serverName: 'files', toolName: 'files.delete', policy: 'disabled' },
  { serverName: 'files', toolName: 'files.move', policy: 'disabled' }
]
```

- [ ] **Step 7: Run tool tests and build**

Run:

```bash
cd turing-backend/orchestrator
npm run test:run -- tests/tools/toolPolicyApproval.test.ts
npm run test:run
npm run build
```

Expected: tests and build pass.

- [ ] **Step 8: Commit**

Run:

```bash
git add turing-backend/orchestrator
git commit -m "feat: add tool policy approvals audit" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 8: Build Go MCP system server

**Files:**
- Create: `turing-backend/services/mcp-system/go.mod`
- Create: `turing-backend/services/mcp-system/main.go`
- Create: `turing-backend/services/mcp-system/main_test.go`
- Create: `turing-backend/services/mcp-system/Dockerfile`
- Modify: `turing-backend/orchestrator/tests/tools/mcpSystemClient.test.ts`

- [ ] **Step 1: Write failing Go tests**

Create `turing-backend/services/mcp-system/main_test.go`:

```go
package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPSystemToolsListAndCall(t *testing.T) {
	server := httptest.NewServer(router())
	defer server.Close()

	resp, err := http.Post(server.URL+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	callBody := []byte(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hello"}}}`)
	callResp, err := http.Post(server.URL+"/mcp", "application/json", bytes.NewReader(callBody))
	if err != nil {
		t.Fatal(err)
	}
	defer callResp.Body.Close()
	if callResp.StatusCode != http.StatusOK {
		t.Fatalf("call status = %d", callResp.StatusCode)
	}
}
```

- [ ] **Step 2: Run Go test and verify it fails**

Run:

```bash
cd turing-backend/services/mcp-system
go test ./...
```

Expected: FAIL because `go.mod`, `main.go`, and `router()` do not exist.

- [ ] **Step 3: Implement system MCP**

Create `turing-backend/services/mcp-system/go.mod`:

```go
module project-turing/mcp-system

go 1.23
```

Create `main.go` with:

- `POST /mcp`
- JSON-RPC `tools/list`
- JSON-RPC `tools/call`
- tools:
  - `health.check` returns `{ "status": "ok" }`
  - `time.now` returns `{ "iso": "<time.Now().UTC().Format(time.RFC3339Nano)>" }`
  - `echo` returns the supplied string under `text`
  - `system.info` returns `runtime.GOOS`, `runtime.GOARCH`, and service name only

Reject unknown methods and tools with JSON-RPC errors.

- [ ] **Step 4: Add Dockerfile**

Create `turing-backend/services/mcp-system/Dockerfile`:

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server main.go

FROM alpine:3.21
WORKDIR /app
COPY --from=builder /app/server ./server
EXPOSE 8080
CMD ["./server"]
```

- [ ] **Step 5: Run Go test and build image**

Run:

```bash
cd turing-backend/services/mcp-system
go test ./...
docker build -t turing-mcp-system:test .
```

Expected: test and Docker build pass.

- [ ] **Step 6: Commit**

Run:

```bash
git add turing-backend/services/mcp-system
git commit -m "feat: add system mcp server" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 9: Build Go MCP files server with sandbox protections

**Files:**
- Create: `turing-backend/services/mcp-files/go.mod`
- Create: `turing-backend/services/mcp-files/main.go`
- Create: `turing-backend/services/mcp-files/main_test.go`
- Create: `turing-backend/services/mcp-files/Dockerfile`

- [ ] **Step 1: Write failing sandbox tests**

Create `turing-backend/services/mcp-files/main_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSandboxAllowsApprovedReadAndRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	sandbox := NewSandbox([]string{dir}, 1024)

	content, err := sandbox.Read(filepath.Join(dir, "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if content != "hello" {
		t.Fatalf("content = %q", content)
	}

	if _, err := sandbox.Read(filepath.Join(dir, "..", "outside.txt")); err == nil {
		t.Fatal("expected traversal read to fail")
	}
}

func TestSandboxRequiresApprovalForWriteAndDisablesDelete(t *testing.T) {
	dir := t.TempDir()
	sandbox := NewSandbox([]string{dir}, 1024)

	decision := sandbox.Policy("files.update")
	if decision != "approval_required" {
		t.Fatalf("files.update policy = %s", decision)
	}
	if decision := sandbox.Policy("files.delete"); decision != "disabled" {
		t.Fatalf("files.delete policy = %s", decision)
	}
}
```

- [ ] **Step 2: Run Go test and verify it fails**

Run:

```bash
cd turing-backend/services/mcp-files
go test ./...
```

Expected: FAIL because files server does not exist.

- [ ] **Step 3: Implement files sandbox**

Create `go.mod`:

```go
module project-turing/mcp-files

go 1.23
```

Implement `Sandbox` in `main.go`:

- Read allowed directories from `TURING_FILE_ALLOWED_DIRS`, comma-separated.
- Resolve absolute paths with `filepath.Abs`.
- Resolve symlinks with `filepath.EvalSymlinks` for existing paths.
- Reject paths outside every allowed directory.
- Enforce `TURING_FILE_MAX_BYTES`, default `1048576`.
- Implement:
  - `files.list`
  - `files.search`
  - `files.read`
  - `files.create`
  - `files.update`
- Return policy metadata for write tools so the orchestrator can require approval.
- Return JSON-RPC errors for `files.delete` and `files.move`.

- [ ] **Step 4: Add Dockerfile**

Create `turing-backend/services/mcp-files/Dockerfile`:

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o server main.go

FROM alpine:3.21
WORKDIR /app
COPY --from=builder /app/server ./server
EXPOSE 8080
CMD ["./server"]
```

- [ ] **Step 5: Run Go tests and build image**

Run:

```bash
cd turing-backend/services/mcp-files
go test ./...
docker build -t turing-mcp-files:test .
```

Expected: tests and Docker build pass.

- [ ] **Step 6: Commit**

Run:

```bash
git add turing-backend/services/mcp-files
git commit -m "feat: add sandboxed files mcp server" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 10: Update Docker Compose and orchestrator Dockerfile

**Files:**
- Modify: `turing-backend/docker-compose.yml`
- Modify: `turing-backend/orchestrator/Dockerfile`
- Create: `turing-backend/.env.example`
- Create: `turing-backend/data/.gitkeep`
- Create: `turing-backend/sandbox/.gitkeep`

- [ ] **Step 1: Write the target Compose file**

Replace `turing-backend/docker-compose.yml` with:

```yaml
services:
  turing-orchestrator:
    build: ./orchestrator
    container_name: turing-orchestrator
    restart: unless-stopped
    ports:
      - "3000:3000"
    environment:
      PORT: "3000"
      TURING_DATA_DIR: "/data"
      TURING_JWT_SECRET: "${TURING_JWT_SECRET}"
      OLLAMA_BASE_URL: "http://host.docker.internal:11434"
      OLLAMA_MODEL: "${OLLAMA_MODEL:-llama3.2}"
      OPENAI_BASE_URL: "${OPENAI_BASE_URL:-https://api.openai.com/v1}"
      OPENAI_API_KEY: "${OPENAI_API_KEY:-}"
      OPENAI_MODEL: "${OPENAI_MODEL:-gpt-4o-mini}"
      MCP_SYSTEM_URL: "http://turing-mcp-system:8080/mcp"
      MCP_FILES_URL: "http://turing-mcp-files:8080/mcp"
      TURING_FILE_ALLOWED_DIRS: "/sandbox"
    volumes:
      - ./data:/data
      - ./sandbox:/sandbox
    depends_on:
      - turing-mcp-system
      - turing-mcp-files
    extra_hosts:
      - "host.docker.internal:host-gateway"
    networks:
      - turing-internal

  turing-mcp-system:
    build: ./services/mcp-system
    container_name: turing-mcp-system
    restart: unless-stopped
    expose:
      - "8080"
    networks:
      - turing-internal

  turing-mcp-files:
    build: ./services/mcp-files
    container_name: turing-mcp-files
    restart: unless-stopped
    expose:
      - "8080"
    environment:
      TURING_FILE_ALLOWED_DIRS: "/sandbox"
      TURING_FILE_MAX_BYTES: "1048576"
    volumes:
      - ./sandbox:/sandbox
    networks:
      - turing-internal

networks:
  turing-internal:
    driver: bridge
```

- [ ] **Step 2: Update orchestrator Dockerfile**

Replace `turing-backend/orchestrator/Dockerfile` with:

```dockerfile
FROM node:20-bookworm-slim AS builder
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends python3 make g++ && rm -rf /var/lib/apt/lists/*
COPY package*.json ./
RUN npm ci
COPY tsconfig.json ./
COPY migrations ./migrations
COPY src ./src
RUN npm run build
RUN npm prune --omit=dev

FROM node:20-bookworm-slim
WORKDIR /app
ENV NODE_ENV=production
COPY --from=builder /app/package*.json ./
COPY --from=builder /app/node_modules ./node_modules
COPY --from=builder /app/dist ./dist
COPY --from=builder /app/migrations ./migrations
EXPOSE 3000
CMD ["node", "dist/server.js"]
```

- [ ] **Step 3: Add Compose env example and placeholder directories**

Create `turing-backend/.env.example`:

```dotenv
TURING_JWT_SECRET=replace-with-at-least-32-random-characters
OLLAMA_MODEL=llama3.2
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_API_KEY=
OPENAI_MODEL=gpt-4o-mini
```

Create empty files:

```bash
mkdir -p turing-backend/data turing-backend/sandbox
touch turing-backend/data/.gitkeep turing-backend/sandbox/.gitkeep
```

- [ ] **Step 4: Validate Compose config**

Run:

```bash
cd turing-backend
TURING_JWT_SECRET=test-secret-with-at-least-32-characters docker compose config >/tmp/turing-compose.yml
```

Expected: command exits 0 and generated config includes only host port `3000:3000`.

- [ ] **Step 5: Build backend images**

Run:

```bash
cd turing-backend
TURING_JWT_SECRET=test-secret-with-at-least-32-characters docker compose build
```

Expected: all three images build successfully.

- [ ] **Step 6: Commit**

Run:

```bash
git add turing-backend
git commit -m "feat: wire backend docker compose" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 11: Add Flutter protocol models, auth storage, REST client, and WebSocket client

**Files:**
- Modify: `turing-client/turing_app/pubspec.yaml`
- Create: `turing-client/turing_app/lib/core/config/turing_config.dart`
- Create: `turing-client/turing_app/lib/core/network/api_client.dart`
- Create: `turing-client/turing_app/lib/core/network/turing_socket.dart`
- Create: `turing-client/turing_app/lib/core/storage/token_store.dart`
- Create: `turing-client/turing_app/lib/models/turing_event.dart`
- Create: `turing-client/turing_app/lib/models/session.dart`
- Modify: `turing-client/turing_app/lib/models/chat_message.dart`
- Create: `turing-client/turing_app/test/models/turing_event_test.dart`

- [ ] **Step 1: Add Flutter dependencies**

Modify `pubspec.yaml` dependencies:

```yaml
dependencies:
  flutter:
    sdk: flutter
  cupertino_icons: ^1.0.8
  http: ^1.2.2
  web_socket_channel: ^3.0.1
  flutter_secure_storage: ^9.2.4
```

Run:

```bash
cd turing-client/turing_app
flutter pub get
```

Expected: `pubspec.lock` updates.

- [ ] **Step 2: Write failing event model test**

Create `turing-client/turing_app/test/models/turing_event_test.dart`:

```dart
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_app/models/turing_event.dart';

void main() {
  test('parses message delta event', () {
    final event = TuringEvent.fromJson({
      'eventId': 'evt_1',
      'sessionId': 'sess_1',
      'runId': 'run_1',
      'traceId': 'trace_1',
      'sequence': 2,
      'type': 'message.delta',
      'createdAt': '2026-05-09T00:00:00.000Z',
      'payload': {'delta': 'hello'},
    });

    expect(event.type, TuringEventType.messageDelta);
    expect(event.payload['delta'], 'hello');
  });
}
```

- [ ] **Step 3: Run test and verify it fails**

Run:

```bash
cd turing-client/turing_app
flutter test test/models/turing_event_test.dart
```

Expected: FAIL because `TuringEvent` does not exist.

- [ ] **Step 4: Add event and session models**

Create `turing-client/turing_app/lib/models/turing_event.dart`:

```dart
enum TuringEventType {
  messageDelta('message.delta'),
  messageCompleted('message.completed'),
  agentRunStarted('agent.run.started'),
  agentRunStep('agent.run.step'),
  agentRunCompleted('agent.run.completed'),
  toolCallStarted('tool.call.started'),
  toolCallCompleted('tool.call.completed'),
  toolCallFailed('tool.call.failed'),
  approvalRequested('approval.requested'),
  error('error'),
  system('system');

  const TuringEventType(this.wireName);
  final String wireName;

  static TuringEventType fromWireName(String value) {
    return TuringEventType.values.firstWhere((type) => type.wireName == value);
  }
}

class TuringEvent {
  const TuringEvent({
    required this.eventId,
    required this.sessionId,
    required this.runId,
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
  final TuringEventType type;
  final DateTime createdAt;
  final Map<String, dynamic> payload;

  factory TuringEvent.fromJson(Map<String, dynamic> json) {
    return TuringEvent(
      eventId: json['eventId'] as String,
      sessionId: json['sessionId'] as String,
      runId: json['runId'] as String?,
      traceId: json['traceId'] as String,
      sequence: json['sequence'] as int,
      type: TuringEventType.fromWireName(json['type'] as String),
      createdAt: DateTime.parse(json['createdAt'] as String),
      payload: Map<String, dynamic>.from(json['payload'] as Map),
    );
  }
}
```

Create `turing-client/turing_app/lib/models/session.dart` with `TuringSession` fields `sessionId`, `title`, `createdAt`, and `updatedAt`.

- [ ] **Step 5: Add config, token store, REST client, and socket client**

Create `turing_config.dart`:

```dart
class TuringConfig {
  const TuringConfig({required this.baseUrl});

  final String baseUrl;

  Uri rest(String path) => Uri.parse('$baseUrl$path');

  Uri ws(String path, {Map<String, String>? query}) {
    final uri = Uri.parse(baseUrl);
    return uri.replace(
      scheme: uri.scheme == 'https' ? 'wss' : 'ws',
      path: path,
      queryParameters: query,
    );
  }
}
```

Create `token_store.dart` using `FlutterSecureStorage` with methods `readAccessToken()`, `readRefreshToken()`, `saveTokens()`, and `clear()`.

Create `api_client.dart` with methods:

- `setup(username, password)`
- `login(username, password)`
- `createSession({String? title})`
- `listSessions()`
- `listMessages(sessionId)`
- `sendMessage(sessionId, text, {modelProvider})`
- `approve(approvalId)`
- `deny(approvalId)`

Create `turing_socket.dart` with a `Stream<TuringEvent> connect({required sessionId, required lastSequence, required token})`.

- [ ] **Step 6: Run Flutter model tests and analyze**

Run:

```bash
cd turing-client/turing_app
flutter test test/models/turing_event_test.dart
flutter analyze
```

Expected: test and analyzer pass.

- [ ] **Step 7: Commit**

Run:

```bash
git add turing-client/turing_app/pubspec.yaml turing-client/turing_app/pubspec.lock turing-client/turing_app/lib turing-client/turing_app/test/models/turing_event_test.dart
git commit -m "feat: add flutter turing protocol clients" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 12: Connect Flutter chat UI to backend streams and approvals

**Files:**
- Create: `turing-client/turing_app/lib/features/auth/setup_login_screen.dart`
- Create: `turing-client/turing_app/lib/features/chat/chat_controller.dart`
- Modify: `turing-client/turing_app/lib/app.dart`
- Modify: `turing-client/turing_app/lib/models/chat_message.dart`
- Modify: `turing-client/turing_app/lib/ui/chat/chat_screen.dart`
- Modify: `turing-client/turing_app/lib/ui/chat/widgets/chat_bubble.dart`
- Modify: `turing-client/turing_app/test/widget_test.dart`
- Create: `turing-client/turing_app/test/features/chat/chat_controller_test.dart`

- [ ] **Step 1: Write failing chat controller test**

Create `turing-client/turing_app/test/features/chat/chat_controller_test.dart`:

```dart
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_app/features/chat/chat_controller.dart';
import 'package:turing_app/models/turing_event.dart';

void main() {
  test('applies streaming delta to active assistant message', () {
    final controller = ChatController();

    controller.applyEvent(TuringEvent(
      eventId: 'evt_1',
      sessionId: 'sess_1',
      runId: 'run_1',
      traceId: 'trace_1',
      sequence: 1,
      type: TuringEventType.messageDelta,
      createdAt: DateTime.parse('2026-05-09T00:00:00.000Z'),
      payload: {'delta': 'hello'},
    ));
    controller.applyEvent(TuringEvent(
      eventId: 'evt_2',
      sessionId: 'sess_1',
      runId: 'run_1',
      traceId: 'trace_1',
      sequence: 2,
      type: TuringEventType.messageDelta,
      createdAt: DateTime.parse('2026-05-09T00:00:01.000Z'),
      payload: {'delta': ' world'},
    ));

    expect(controller.messages.single.text, 'hello world');
    expect(controller.lastSequence, 2);
  });
}
```

- [ ] **Step 2: Run test and verify it fails**

Run:

```bash
cd turing-client/turing_app
flutter test test/features/chat/chat_controller_test.dart
```

Expected: FAIL because `ChatController` does not exist.

- [ ] **Step 3: Implement chat controller**

Create `chat_controller.dart` with:

- `ValueNotifier<List<ChatMessage>> messages`
- `int lastSequence`
- `applyEvent(TuringEvent event)`
- append deltas to the active assistant message for a run
- mark completion on `message.completed`
- create approval card message on `approval.requested`
- create error message on `error`

- [ ] **Step 4: Replace stale widget test**

Replace `test/widget_test.dart` with:

```dart
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_app/app.dart';

void main() {
  testWidgets('renders TuringAgent app shell', (tester) async {
    await tester.pumpWidget(const TuringApp());
    expect(find.text('TuringAgent'), findsWidgets);
  });
}
```

- [ ] **Step 5: Implement setup/login screen and app routing**

`setup_login_screen.dart` should include:

- backend URL field defaulting to `http://localhost:3000`
- username field
- password field
- setup button
- login button
- visible error text on failed requests

`app.dart` should show setup/login until tokens are available, then show `ResponsiveShell`.

- [ ] **Step 6: Connect chat screen**

Modify `chat_screen.dart` so:

- it uses `ChatController`
- send button calls `ApiClient.sendMessage`
- WebSocket events feed `controller.applyEvent`
- reconnect passes `lastSequence`
- no simulated two-second response remains

- [ ] **Step 7: Update chat bubble rendering**

`ChatBubble` should render:

- user text
- assistant streaming text
- loading state before first delta
- approval card with Approve and Deny buttons
- error message with warning style

- [ ] **Step 8: Run Flutter tests and analyze**

Run:

```bash
cd turing-client/turing_app
flutter test
flutter analyze
```

Expected: tests and analyzer pass.

- [ ] **Step 9: Commit**

Run:

```bash
git add turing-client/turing_app
git commit -m "feat: connect flutter chat to orchestrator" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 13: End-to-end local verification

**Files:**
- Modify: `README.md`
- Create: `turing-backend/scripts/smoke.sh`

- [ ] **Step 1: Create smoke script**

Create `turing-backend/scripts/smoke.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:3000}"
USERNAME="${TURING_SMOKE_USERNAME:-miguel}"
PASSWORD="${TURING_SMOKE_PASSWORD:-correct horse battery staple}"

setup_response="$(curl -sS -X POST "$BASE_URL/setup" \
  -H 'content-type: application/json' \
  -d "{\"username\":\"$USERNAME\",\"password\":\"$PASSWORD\"}")"

access_token="$(printf '%s' "$setup_response" | node -e "const fs=require('fs'); const body=JSON.parse(fs.readFileSync(0,'utf8')); console.log(body.accessToken || '')")"

if [[ -z "$access_token" ]]; then
  login_response="$(curl -sS -X POST "$BASE_URL/auth/login" \
    -H 'content-type: application/json' \
    -d "{\"username\":\"$USERNAME\",\"password\":\"$PASSWORD\"}")"
  access_token="$(printf '%s' "$login_response" | node -e "const fs=require('fs'); const body=JSON.parse(fs.readFileSync(0,'utf8')); console.log(body.accessToken)")"
fi

session_response="$(curl -sS -X POST "$BASE_URL/sessions" \
  -H "authorization: Bearer $access_token" \
  -H 'content-type: application/json' \
  -d '{"title":"Smoke test"}')"
session_id="$(printf '%s' "$session_response" | node -e "const fs=require('fs'); const body=JSON.parse(fs.readFileSync(0,'utf8')); console.log(body.sessionId)")"

curl -sS -X POST "$BASE_URL/sessions/$session_id/messages" \
  -H "authorization: Bearer $access_token" \
  -H 'content-type: application/json' \
  -d '{"text":"Say hello from TuringAgent smoke test","modelProvider":"ollama"}'

printf '\nSmoke session: %s\n' "$session_id"
```

- [ ] **Step 2: Update README run instructions**

Add concise sections to `README.md`:

```md
## TuringAgent v1 local run

1. Start Ollama on the Mac host and pull the configured model.
2. Copy `turing-backend/.env.example` to `turing-backend/.env`.
3. Set `TURING_JWT_SECRET` to at least 32 random characters.
4. Run `docker compose up --build` from `turing-backend`.
5. Open the Flutter app and connect to `http://<mac-lan-or-tailscale-ip>:3000` on physical Android, or `http://localhost:3000` on desktop/simulator.

Only the orchestrator exposes port 3000. MCP services stay internal to Docker Compose.
```

- [ ] **Step 3: Run backend verification**

Run:

```bash
cd turing-backend/orchestrator
npm run test:run
npm run build
cd ../services/mcp-system
go test ./...
cd ../mcp-files
go test ./...
cd ../../
TURING_JWT_SECRET=test-secret-with-at-least-32-characters docker compose config >/tmp/turing-compose.yml
```

Expected: all commands pass.

- [ ] **Step 4: Run Flutter verification**

Run:

```bash
cd turing-client/turing_app
flutter test
flutter analyze
```

Expected: tests and analyzer pass.

- [ ] **Step 5: Run optional manual smoke with Ollama available**

Run:

```bash
cd turing-backend
cp .env.example .env
TURING_JWT_SECRET=test-secret-with-at-least-32-characters docker compose up --build
```

In a second terminal:

```bash
cd turing-backend
BASE_URL=http://localhost:3000 bash scripts/smoke.sh
```

Expected: script prints a smoke session ID, and Docker logs show model deltas and persisted events.

- [ ] **Step 6: Commit**

Run:

```bash
git add README.md turing-backend/scripts/smoke.sh
git commit -m "docs: add v1 local smoke instructions" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Final verification checklist

- [ ] `cd turing-backend/orchestrator && npm run test:run`
- [ ] `cd turing-backend/orchestrator && npm run build`
- [ ] `cd turing-backend/services/mcp-system && go test ./...`
- [ ] `cd turing-backend/services/mcp-files && go test ./...`
- [ ] `cd turing-backend && TURING_JWT_SECRET=test-secret-with-at-least-32-characters docker compose config >/tmp/turing-compose.yml`
- [ ] `cd turing-client/turing_app && flutter test`
- [ ] `cd turing-client/turing_app && flutter analyze`
- [ ] With Ollama running: `cd turing-backend && docker compose up --build`
- [ ] With backend running: `cd turing-backend && BASE_URL=http://localhost:3000 bash scripts/smoke.sh`

## Implementation notes

- Do not expose MCP services on host ports.
- Do not log auth headers, refresh tokens, API keys, passwords, or file contents.
- Keep the orchestrator as the only writer to canonical SQLite app data.
- Keep Flutter protocol-driven and avoid duplicating orchestration logic in the client.
- Preserve existing user changes, including the current unstaged `turing-client/turing_app/pubspec.lock` change unless execution confirms it belongs to the dependency update task.
