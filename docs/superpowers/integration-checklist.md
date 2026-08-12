# TuringAgent Integration Checklist

Use this checklist against the current Go gRPC, MCP, and Flutter architecture.

## 1. Initialization and Compose

- [ ] `turing-backend/scripts/init.sh` creates mode-`0700` data and sandbox
  directories, mode-`0600` SQLite files, and a mode-`0600` regular `.env` while
  running as a non-root host user.
- [ ] Initialization rejects symlinked, non-owned, inaccessible, or
  group/world-writable sandbox roots and unsafe writable legacy entries.
- [ ] `turing-backend/scripts/compose.sh` revalidates the sandbox bind source
  and data/SQLite modes immediately before a launch, then injects the current
  host UID/GID for both bind-mount writers.
- [ ] `cd turing-backend && ./scripts/compose.sh config --quiet` resolves the
  orchestrator, agent runtime, `mcp-system`, and `mcp-files` services.
- [ ] MCP containers are non-root, read-only, capability-free, and reachable
  only on their intended internal Docker networks.
- [ ] MCP healthchecks run through the service binaries without write access or
  extra image utilities, and the runtime waits for both services to be healthy.

## 2. Protobuf and gRPC Contracts

- [ ] `proto/turing/v1/` is the source of truth for public and internal APIs.
- [ ] `tools/proto/check.sh` leaves the Go generated output unchanged; the
  Flutter contract test separately verifies checked-in Dart fields.
- [ ] The orchestrator exposes the public gRPC API on port `3000` and its
  authenticated runtime API only on the internal port `3001`.
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
