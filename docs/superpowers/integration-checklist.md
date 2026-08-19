# TuringAgent Integration Checklist

Use this checklist against the current Go gRPC, MCP, and Flutter architecture.

## 1. Initialization and Compose

- [ ] `turing-backend/scripts/init.sh` creates mode-`0700` data and sandbox
  directories, a mode-`0700` skills directory, mode-`0600` SQLite files, and a
  mode-`0600` regular `.env` while running as a non-root host user.
- [ ] Initialization rejects symlinked, non-owned, inaccessible, or
  group/world-writable sandbox roots and unsafe writable legacy entries.
- [ ] `turing-backend/scripts/compose.sh` revalidates the sandbox, skills, and
  data bind sources plus SQLite modes immediately before a launch, then injects
  the current host UID/GID for both bind-mount writers.
- [ ] `cd turing-backend && ./scripts/compose.sh config --quiet` resolves the
  orchestrator, agent runtime, `mcp-system`, and `mcp-files` services.
- [ ] Every backend container uses an explicit non-root identity, a read-only
  root filesystem, `cap_drop: ALL`, and `no-new-privileges`.
- [ ] `TURING_DOCKER_SECURITY_LIVE=1 go test -tags sqlite_fts5
  ./turing-backend/tests -run TestBuiltBackendImagesDeclareNoWritableVolumes
  -count=1` confirms no built image inherits a writable volume.
- [ ] Only orchestrator `/app/data` and `/skills` plus `mcp-files` `/sandbox`
  are writable; every service replaces Docker's default writable `/dev/shm`
  with the approved read-only tmpfs.
- [ ] Only the orchestrator publishes a host port, fixed to `127.0.0.1`; MCP
  services remain on their intended internal Docker networks.
- [ ] MCP healthchecks run through the service binaries without write access or
  extra image utilities, and the runtime waits for both services to be healthy.

## 2. Protobuf and gRPC Contracts

- [ ] `proto/turing/v1/` is the source of truth for public and internal APIs.
- [ ] `tools/proto/check.sh` leaves the Go generated output unchanged; the
  Flutter contract test separately verifies checked-in Dart fields.
- [ ] The orchestrator exposes the public gRPC API on the configured loopback
  port (default `3000`) and its authenticated runtime API only on the internal
  port `3001`.
- [ ] Health, session creation, message sending, event replay/subscription, and
  approval RPCs reject missing or invalid bearer credentials.

## 3. Agent Runtime and Tool Loop

- [ ] The runtime leases jobs over internal gRPC and streams model updates back
  to the orchestrator.
- [ ] Ollama and OpenAI-compatible providers preserve model tool-call IDs and
  reject malformed or over-budget streamed tool calls.
- [ ] Tool discovery validates MCP catalogs and retains supported JSON Schema
  constraints.
- [ ] A discovery-capable runtime reports `TOOL_DISCOVERY_STATUS_COMPLETE`
  with its authoritative snapshot; failed discovery is rejected.
- [ ] `ListTools` reflects the persisted union of connected workers' tools,
  and disabled or absent registry entries fail closed without legacy fallback.
- [ ] Authorization is scoped to the worker that reported the capability.
- [ ] Per-run tool-call, model-output, tool-result, request/response, and timeout
  budgets fail closed.
- [ ] BEFORE and AFTER tool beacons correlate the runtime call ID and model tool
  call ID through protobuf.

## 4. MCP and Approval Boundary

- [ ] `mcp-system` and `mcp-files` require their per-agent bearer tokens.
- [ ] `system.echo` and file tools enforce the bounds advertised by
  `tools/list`.
- [ ] File paths reject absolute paths, traversal, reserved staging names, and
  byte/component/depth overflows.
- [ ] Directory metadata and file operations remain descriptor-relative and do
  not follow symlinks.
- [ ] `files.create` and `files.update` validate and consume a single-use
  approval JWT before mutation.
- [ ] `expectedHash` is treated as serialization for cooperating MCP writers,
  not as a global atomic guarantee against direct host writers.
- [ ] Audit records include authentication and tool lifecycle events.

## 5. Flutter Client

- [ ] `ResponsiveShell` remains the primary app surface.
- [ ] Settings, sessions, chat, event streaming, model selection, and approval
  cards use generated gRPC clients.
- [ ] `ToolCallBeacon.modelToolCallId` survives Dart protobuf serialization.
- [ ] Devices, Stats, and Integrations remain placeholders until contracts are
  defined.

## 6. Automated Verification

Run from the repository root:

```bash
go test -tags sqlite_fts5 -race ./... -count=1
go vet -tags sqlite_fts5 ./...
go build -tags sqlite_fts5 ./...
go test -tags sqlite_fts5 ./.github/workflows -count=1
go test -tags sqlite_fts5 ./turing-backend/scripts -count=1

(cd turing-backend/mcp-files &&
  go test -race ./... -count=1 &&
  go vet ./... &&
  go build ./cmd/server)

(cd turing-backend/mcp-system &&
  go test -race ./... -count=1 &&
  go vet ./... &&
  go build ./...)

golangci-lint run --config .golangci.yml --build-tags sqlite_fts5 ./... ./.github/workflows
(cd turing-backend/mcp-files && golangci-lint run --config ../../.golangci.yml ./...)
(cd turing-backend/mcp-system && golangci-lint run --config ../../.golangci.yml ./...)

bash -n turing-backend/scripts/*.sh tools/proto/*.sh
tools/proto/check.sh

(cd turing-client/turing_app &&
  flutter analyze &&
  flutter test)
```

For a Docker-backed end-to-end check:

```bash
cd turing-backend
./scripts/smoke-grpc.sh
```

Compose first waits (with a 60-second bound) for the MCP healthchecks. The smoke
client then waits for `HealthService.Check`, creates a session, sends a
deterministic `system.time` tool request, observes streamed events, and verifies
event replay. `scripts/smoke.sh` is an alias for this gRPC smoke path.

The deterministic smoke uses the `/tool` debug shortcut. To verify the live
model-driven path instead, ensure Ollama is running on the host and run:

```bash
cd turing-backend
TURING_VERIFY_MODEL=qwen2.5:7b TURING_VERIFY_ATTEMPTS=3 ./scripts/verify-tool-loop.sh
```

The verifier sends a natural-language prompt, requires `system.time`, and checks
that the same non-empty `toolCallId` and `toolName` complete before a later final
answer containing the returned UTC time. It retries inconclusive attempts,
including model or transport errors before that lifecycle is exercised. It
accepts ISO, 12/24-hour clock, and Unix seconds/milliseconds representations
within the observed tool execution window, considering only model output
streamed after the last successful tool completion. It
reports:

| Exit | Verdict | Meaning |
|---|---|---|
| `0` | `PASS` | The model chose `system.time`, the call completed, and a later final answer reflected its returned UTC time. |
| `1` | `FAIL` | The `system.time` lifecycle was malformed, mismatched, incomplete after starting (including a post-start timeout), or ended without a later final answer. |
| `2` | `INCONCLUSIVE` | The proof could not exercise the target lifecycle because of setup, model, transport or timeout before the tool started, a different/no tool choice, or an answer that could not be correlated to the returned time. |

`TURING_VERIFY_MODEL` defaults to `OLLAMA_MODEL` and then `qwen2.5:7b`.
`TURING_VERIFY_ATTEMPTS` defaults to `3`. The host preflight rewrites the
Compose-oriented `host.docker.internal` Ollama hostname to `localhost`; set
`TURING_VERIFY_OLLAMA_URL` to override the host-side probe and model endpoint.
Localhost overrides are mapped to `host.docker.internal` for Compose; use
`TURING_VERIFY_OLLAMA_CONTAINER_URL` when containers require a different URL.
The preflight also checks that the selected model is installed before starting
Compose. A model may recover from rejected arguments by issuing a later valid
`system.time` call; if every such call fails, the result is inconclusive. An
explicit model provider/stream failure or output, tool-result, or tool-call
limit is likewise inconclusive because the runtime or provider operated as
designed. An
inconclusive result with a small model should be repeated with a stronger
tool-calling model before changing the loop. The live verifier is intentionally
on-demand rather than a CI gate; CI only syntax-checks its shell wrapper.

Flutter coverage exercises the live wire shape separately: the mapper test
decodes persisted tool events with `toolCallId`, `toolName`, and `serverName`;
the chat widget tests then verify running-to-completed card updates and that the
post-tool answer renders below the card. Both macOS entitlements retain
`com.apple.security.network.client`, without which the sandboxed application
cannot reach the backend. The captured end-to-end macOS result is
[live-tool-loop-verification.png](../assets/live-tool-loop-verification.png).
