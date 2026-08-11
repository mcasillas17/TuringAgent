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

By default, containers reach Ollama at `http://host.docker.internal:11434`.

## Install and run

Clone the repository and initialize local backend secrets:

```bash
git clone https://github.com/mcasillas17/TuringAgent.git
cd TuringAgent/turing-backend
./scripts/init.sh
```

`init.sh` creates `turing-backend/.env`, generates local bearer tokens, creates `data/` and `sandbox/`, and prints the Flutter client API key. Do not commit `.env`.

Start the backend stack:

```bash
./scripts/dev.sh
```

This builds and runs the orchestrator, agent runtime, and MCP servers through Docker Compose. The public gRPC API listens on `localhost:3000` by default.

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
go test -tags sqlite_fts5 ./... -count=1
go build -tags sqlite_fts5 ./...
cd turing-backend/mcp-files && go test ./... -count=1 && go build ./cmd/server
cd ../../turing-client/turing_app && flutter test
```

## Configuration

Backend configuration lives in `turing-backend/.env`, copied from `turing-backend/.env.example`.

Common values:

| Variable | Purpose |
|---|---|
| `TURING_CLIENT_API_KEY` | Bearer token for Flutter and other public gRPC clients |
| `TURING_INTERNAL_TOKEN` | Bearer token for internal runtime and approval gRPC calls |
| `TURING_APPROVAL_JWT_SECRET` | HS256 secret used for approval tokens |
| `ORCHESTRATOR_GRPC_ADDR` | Internal orchestrator gRPC address, usually `turing-orchestrator:3001` |
| `OLLAMA_BASE_URL` / `OLLAMA_MODEL` | Local model endpoint and default model |
| `OPENAI_API_KEY` / `OPENAI_MODEL` | Optional OpenAI-compatible model configuration |

## Troubleshooting

- **Backend is not reachable:** check that Docker Compose is running and port `3000` is free.
- **Authentication fails:** confirm the Flutter API key matches `TURING_CLIENT_API_KEY` in `turing-backend/.env`.
- **No model response:** ensure Ollama is running on the host and the configured model is available.
- **Smoke test times out:** inspect the `turing-orchestrator` and `turing-agent-runtime-general` container logs.
- **File tools fail:** confirm `turing-backend/sandbox/` exists and that approval-required write tools were approved in the client.

## Documentation

- [Tech stack and architecture](docs/architecture/tech-stack.md)
- [MCP security and approval flow](docs/mcp-security-and-integration.md)
- [Flutter client guide](turing-client/turing_app/README.md)
- [Go/gRPC migration design](docs/superpowers/specs/2026-05-15-turing-go-grpc-migration-design.md)
