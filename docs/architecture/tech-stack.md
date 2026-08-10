# TuringAgent Tech Stack and Architecture

This document captures the implementation details that are useful for contributors but too detailed for the root README.

## Runtime architecture

TuringAgent is split into four local runtime pieces:

| Component | Location | Responsibility |
|---|---|---|
| Go orchestrator | `turing-backend/orchestrator-go` | Public gRPC API, internal runtime gRPC API, sessions, messages, runs, events, approvals, audit records, SQLite persistence |
| Go agent runtime | `turing-backend/agent-runtime-go` | Connects to the orchestrator, loads session context, calls model providers, executes MCP tools, streams runtime updates |
| MCP system server | `turing-backend/mcp-system` | Safe system tools exposed over JSON-RPC 2.0 Streamable HTTP |
| MCP files server | `turing-backend/mcp-files` | Sandboxed file tools; mutating tools require approval JWT validation and gRPC approval consumption |
| Flutter client | `turing-client/turing_app` | Thin UI for settings, sessions, chat, streamed events, model selection, and approvals |

The client talks to the orchestrator through gRPC. The agent runtime talks to MCP servers over internal HTTP JSON-RPC. MCP servers are not published to the host.

## gRPC and protobuf

Protocol definitions live under `proto/turing/v1/`.

Generated code:

- Go: `gen/turing/v1/go/turing/v1/`
- Dart: `turing-client/turing_app/lib/gen/turing/v1/`

Useful commands:

```bash
tools/proto/check.sh
go test -tags sqlite_fts5 ./... -count=1
go build -tags sqlite_fts5 ./...
```

The public orchestrator gRPC port defaults to `3000`. The internal runtime gRPC port defaults to `3001`.

## Docker Compose services

`turing-backend/infra/docker-compose.yml` starts:

| Service | Network exposure |
|---|---|
| `turing-orchestrator` | Publishes public gRPC port `3000`; exposes internal gRPC port `3001` only inside Docker networks |
| `turing-agent-runtime-general` | Internal Docker networks only |
| `turing-mcp-system` | Internal `net-system` network only |
| `turing-mcp-files` | Internal `net-files` network only |

Compose uses explicit `environment:` blocks instead of `env_file:` so services receive only the secrets and config they need.

## Model providers

The default local model path is Ollama:

```text
OLLAMA_BASE_URL=http://host.docker.internal:11434
OLLAMA_MODEL=llama3.2
```

OpenAI-compatible models can be configured with:

```text
OPENAI_BASE_URL=https://api.openai.com/v1
OPENAI_API_KEY=
OPENAI_MODEL=gpt-4o-mini
```

The Flutter client sends the selected provider with each message. The backend owns provider routing and model execution.

## Approval flow

Approval-gated file writes use a two-step flow:

1. The orchestrator creates an approval record for the requested tool call.
2. After user approval, the orchestrator signs a short-lived HS256 JWT.
3. The agent runtime sends that JWT to `mcp-files` as `params._meta.approvalToken`.
4. `mcp-files` verifies audience, subject, tool name, argument hash, signature, and expiration.
5. `mcp-files` calls `ApprovalService.ConsumeApproval` over internal gRPC using `authorization: Bearer ${TURING_INTERNAL_TOKEN}`.
6. The file write proceeds only if the consume response is `APPROVAL_STATUS_CONSUMED`.

See [MCP security and approval flow](../mcp-security-and-integration.md) for the detailed threat model and test coverage.

## Local data and secrets

`turing-backend/scripts/init.sh` creates:

- `turing-backend/.env`
- `turing-backend/data/`
- `turing-backend/sandbox/`

Do not commit generated secrets, local databases, or sandbox files.

## Verification matrix

Run from the repository root unless noted:

```bash
go test -tags sqlite_fts5 ./... -count=1
go build -tags sqlite_fts5 ./...
cd turing-backend/mcp-files && go test ./... -count=1 && go build ./cmd/server
cd ../../turing-client/turing_app && flutter test
cd ../.. && tools/proto/check.sh
cd turing-backend && ./scripts/smoke-grpc.sh
```

The smoke script initializes local secrets, builds the Compose stack, checks `HealthService.Check`, creates a session, sends a deterministic `/tool system.time` message, waits for streamed events, and verifies replay with `EventService.ListEvents`.
