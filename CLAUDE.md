# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Local-first AI orchestration platform: a Flutter desktop client + Go gRPC backend that owns chat sessions, model routing, streaming events, tool execution, approvals, and audit state. Runs locally via Docker Compose; secrets live in `turing-backend/.env`, data under `turing-backend/data/`, file tools sandboxed to `turing-backend/sandbox/`.

## Multi-module layout (important)

This is a **multi-module** Go repo. `go build ./...` / `go test ./...` at the root does **NOT** cover these separate modules — you must `cd` into them:
- `/go.mod` — root module `github.com/mcasillas17/TuringAgent` (orchestrator-go, agent-runtime-go, gen, tests)
- `turing-backend/mcp-files/go.mod` — sandboxed file tools; has a `replace` back to root
- `turing-backend/mcp-system/go.mod` — standalone system tools; **not covered by CI** — build/test it manually when you touch it

## Verification (run the full matrix before claiming work is done)

```bash
go test ./... -count=1
go build ./...
cd turing-backend/mcp-files && go test ./... -count=1 && go build ./cmd/server && cd ../..
cd turing-client/turing_app && flutter test && cd ../..
tools/proto/check.sh
# if you changed turing-backend/mcp-system:
cd turing-backend/mcp-system && go test ./... -count=1 && go build ./... && cd ../..
```
The `/verify` skill runs this matrix.

## Gotchas

- **`turing-backend/package.json`'s `lint` script runs `go test`, not a linter.** The real Go linter is golangci-lint (v2, config at `.golangci.yml`): `golangci-lint run ./...` from the root, and separately inside `turing-backend/mcp-files` and `turing-backend/mcp-system`. Not yet wired into CI. `gofmt` runs automatically on save via the format hook; Flutter has `flutter analyze` (defaults only, not in CI).
- **Generated proto code is committed.** After editing a `.proto`, run `tools/proto/generate.sh` and commit the regenerated `gen/` (and Dart output). CI's `tools/proto/check.sh` fails on any diff, so codegen must be deterministic.
- **CI is self-guarding:** `.github/workflows/ci_test.go` asserts `ci.yml` contains specific commands — editing CI commands may require updating that test.
- **mcp-files approval flow:** mutating file tools require a short-lived HS256 approval JWT (signed by the orchestrator after user approval, passed as `params._meta.approvalToken`), verified by mcp-files, then consumed via `ApprovalService.ConsumeApproval` over internal gRPC. The write proceeds only on `APPROVAL_STATUS_CONSUMED`.
- **Compose uses explicit `environment:` blocks (not `env_file:`)** so each service gets only the secrets it needs. MCP servers are never published to the host (internal Docker networks only); only orchestrator `:3000` is public.
- Do not commit `.env` (gitignored; `init.sh` sets it `chmod 600`). `.env.example` is the whitelisted template.

## Running the stack

```bash
cd turing-backend && ./scripts/init.sh   # generates .env, tokens, data/ & sandbox/; prints the Flutter API key
./scripts/dev.sh                          # docker compose up --build (foreground)
```
Requires Docker + Compose, Go 1.23+, Flutter, and Ollama running on the host (`OLLAMA_BASE_URL=http://host.docker.internal:11434`, default model `llama3.2`). Run the client: `cd turing-client/turing_app && flutter pub get && flutter run -d macos`.

## Repo etiquette

Work on an isolated git worktree + feature branch; open a PR into `main` (recent history is squash-merged PRs). Do not commit directly to `main`.
