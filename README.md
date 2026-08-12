# TuringAgent

TuringAgent is a local-first AI orchestration platform for running a private assistant stack on your own machine. It pairs a Flutter client with a Go gRPC backend that owns chat sessions, model routing, streaming events, tool execution, approvals, and audit state.

The project is designed for local development first: secrets stay in your local `.env`, data is stored under `turing-backend/data/`, and file tools are constrained to `turing-backend/sandbox/`.

## What it does

- Runs a Go gRPC orchestrator for sessions, messages, runs, events, and approvals.
- Runs a Go agent runtime that connects to local or OpenAI-compatible models.
- Exposes MCP tool servers for safe system tools and approval-gated sandboxed file tools.
- Provides a Flutter client with settings, session list, chat, streamed responses, and approval cards.
- Ships a Docker Compose local stack and an end-to-end gRPC smoke test.

## Requirements

- Docker and Docker Compose
- Go 1.23+
- Flutter
- Ollama running on the host for the default local model path
- A non-root host account for backend initialization and Compose launches

By default, containers reach Ollama at `http://host.docker.internal:11434`.

## Install and run

Clone the repository and initialize local backend secrets:

```bash
git clone https://github.com/mcasillas17/TuringAgent.git
cd TuringAgent/turing-backend
./scripts/init.sh
```

`init.sh` rejects root execution, creates `turing-backend/.env`, generates local
bearer tokens, records the current non-root UID/GID for the bind-mounted
sandbox, creates `data/` and a real (non-symlink) `sandbox/`, and prints the
Flutter client API key. It fails rather than changing ownership or permissions
when existing sandbox content is inaccessible. Do not commit `.env`.

Start the backend stack:

```bash
./scripts/dev.sh
```

This builds and runs the orchestrator, agent runtime, and MCP servers through Docker Compose. The public gRPC API listens on `localhost:3000` by default.

Use the repository scripts rather than invoking this Compose file directly.
`scripts/compose.sh` validates and injects the current non-root host UID/GID;
this prevents stale `.env` values or exported `HOST_UID`/`HOST_GID` variables
from selecting the identity used for the sandbox bind mount.

In another terminal, run the Flutter app:

```bash
cd turing-client/turing_app
flutter pub get
flutter run -d macos
```

On first launch, enter:

- **Backend URL:** `http://localhost:3000`
- **API key:** the `Flutter client API key` printed by `./scripts/init.sh`

## Verify the stack

Run the backend smoke test:

```bash
cd turing-backend
./scripts/smoke-grpc.sh
```

Run developer checks from the repository root:

```bash
go test -tags sqlite_fts5 -race ./... -count=1
go vet -tags sqlite_fts5 ./...
go build -tags sqlite_fts5 ./...
cd turing-backend/mcp-files && go test -race ./... -count=1 && go vet ./... && go build ./cmd/server
cd ../mcp-system && go test -race ./... -count=1 && go vet ./... && go build ./...
cd ../../turing-client/turing_app && flutter analyze && flutter test
```

## Configuration

Backend configuration lives in `turing-backend/.env`, copied from `turing-backend/.env.example`.

Common values:

| Variable | Purpose |
|---|---|
| `TURING_CLIENT_API_KEY` | Bearer token for Flutter and other public gRPC clients |
| `TURING_INTERNAL_TOKEN` | Bearer token for internal runtime and approval gRPC calls |
| `TURING_APPROVAL_JWT_SECRET` | HS256 secret used for approval tokens |
| `TURING_APPROVAL_TIMEOUT_MS` / `TURING_APPROVAL_WAIT_TIMEOUT_MS` | Approval lifetime and the longer runtime observation bound (defaults: 65s / 71s) |
| `TURING_TOOL_TIMEOUT_MS` / `TURING_TOOL_TOTAL_TIMEOUT_MS` | Per-request MCP timeout and whole-tool lifecycle timeout (defaults: 30s / 180s) |
| `HOST_IDENTITY_MODE` | Managed compatibility marker; `init.sh` always resets it to `auto` |
| `HOST_UID` / `HOST_GID` | Current canonical non-root host IDs, managed by `init.sh` and overridden safely by `scripts/compose.sh` at launch |
| `ORCHESTRATOR_GRPC_ADDR` | Internal orchestrator gRPC address, usually `turing-orchestrator:3001` |
| `OLLAMA_BASE_URL` / `OLLAMA_MODEL` | Local model endpoint and default model |
| `OPENAI_API_KEY` / `OPENAI_MODEL` | Optional OpenAI-compatible model configuration |

## Troubleshooting

- **Backend is not reachable:** check that Docker Compose is running and port `3000` is free.
- **Authentication fails:** confirm the Flutter API key matches `TURING_CLIENT_API_KEY` in `turing-backend/.env`.
- **No model response:** ensure Ollama is running on the host and the configured model is available.
- **Smoke test times out:** inspect the `turing-orchestrator` and `turing-agent-runtime-general` container logs.
- **Initialization refuses root:** run it from the non-root host account that owns the checkout and sandbox; do not use `sudo`.
- **Initialization reports legacy sandbox content:** restore ownership and owner read/write access (plus directory traversal) outside the script, or move the content aside, then rerun `scripts/init.sh`. The script deliberately does not recurse with `chmod` or `chown`.
- **File tools fail:** confirm `turing-backend/sandbox/` is a real directory, rerun `scripts/init.sh`, and confirm approval-required writes were approved. Rootless Docker, `userns-remap`, and SELinux may require daemon-specific ownership/mapping or labeling; see the MCP security guide.

## Documentation

- [Tech stack and architecture](docs/architecture/tech-stack.md)
- [MCP security and approval flow](docs/mcp-security-and-integration.md)
- [Flutter client guide](turing-client/turing_app/README.md)
- [Go/gRPC migration design](docs/superpowers/specs/2026-05-15-turing-go-grpc-migration-design.md)
