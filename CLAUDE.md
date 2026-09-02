# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Local-first AI orchestration platform: a Flutter desktop client + Go gRPC backend that owns chat sessions, model routing, streaming events, tool execution, approvals, and audit state. Runs locally via Docker Compose; secrets live in `turing-backend/.env`, data under `turing-backend/data/`, file tools are sandboxed to `turing-backend/sandbox/`, file-backed skills live under `turing-backend/skills/` (mounted read/write at `/skills` in the orchestrator), and the memory vault lives under `turing-backend/memory/` (mounted read/write at `/memory` in the orchestrator only, with `MEMORY_ROOT` set explicitly in Compose, and the host path passed separately as the display-only `MEMORY_DISPLAY_ROOT`).

## Multi-module layout (important)

This is a **multi-module** Go repo. `go build -tags sqlite_fts5 ./...` / `go test -tags sqlite_fts5 ./...` at the root does **NOT** cover these separate modules — you must `cd` into them:
- `/go.mod` — root module `github.com/mcasillas17/TuringAgent` (orchestrator-go, agent-runtime-go, gen, tests)
- `turing-backend/mcp-files/go.mod` — sandboxed file tools; has a `replace` back to root
- `turing-backend/mcp-system/go.mod` — standalone system tools; stdlib-only (no `go.sum`). CI has its own `mcp-system` job, but the root `./...` never reaches it, so run it explicitly when you touch it

**The three modules do not declare the same Go version.** Root and `mcp-files` are `go 1.25.0` (raised by Dependabot's grpc bump #92 and x/net bump #91 respectively); `mcp-system` remains `go 1.23`. A local toolchain below 1.25 cannot build the root module or `mcp-files` directly — with the default `GOTOOLCHAIN=auto` Go selects a conforming toolchain (using one already present, downloading `go1.25.0` otherwise) instead of failing. Install **Go 1.25 or newer** and the question does not arise locally. CI pins `go-version: "1.25.x"` on all five jobs (#92 raised the other four; `ci_test.go` asserts the lint job's pin).

## Toolchain versions (what is pinned, and where)

Nothing here is enforced by one place, so a bump is never a one-line edit. The **enforcer** column names what actually fails when your version is wrong — that is the file to read when a gate rejects you.

| Tool | Version | Enforcer (fails on mismatch) | Also asserted in |
|---|---|---|---|
| Go (local) | **1.25+** | root & `mcp-files` go.mod (`go 1.25.0`) | `mcp-system` go.mod says `1.23`; CI pins `1.25.x` on all five jobs, each asserted by `ci_test.go`; `tools/docs` asserts this table against the go.mod files |
| Go (containers, MCP images) | `1.27-alpine` | `mcp-files`/`mcp-system` Dockerfiles | Dependabot `docker` entry |
| Go (containers, orchestrator & agent-runtime) | `1.27-bookworm` | their Dockerfiles | Dependabot `docker` entry |
| golangci-lint | v2.12.2 | — (no local guard) | `ci.yml`, `ci_test.go` |
| buf | 1.72.0 | `tools/proto/breaking.sh`, `tools/proto/breaking_test.go` | `ci.yml`, `ci_test.go` |
| protoc | 34.1 exactly, first on `PATH` | `tools/proto/generate.sh` | `ci.yml`, `generate_test.go` |
| protoc-gen-go | v1.36.11 | `tools/proto/generate.sh` | `ci.yml`, `generate_test.go` |
| protoc-gen-go-grpc | 1.6.2 | `tools/proto/generate.sh` | `ci.yml`, `generate_test.go` |
| Dart `protoc_plugin` | 23.0.0 | `tools/proto/generate.sh` | `ci.yml`, `generate_test.go` |
| Flutter | **not pinned** (`channel: stable`) | — | `ci.yml` |
| Dart SDK | `^3.10.4` | `turing_app/pubspec.yaml` | — |

**`tools/proto/README.md` is the install guide** for the proto toolchain and buf, with the exact `go install` lines and Buf's verify-the-checksum steps. It is the older and more detailed source; this table exists to say *what fails* and *where*, not to replace it. Version strings above mirror each tool's own `--version` output, which the scripts string-compare literally — hence `v1.36.11` but `1.6.2`, even though both `go install` refs carry a `v`.

Three traps worth knowing before they cost an afternoon:

- **A wrong-version `buf` fails `go test ./...`, not just proto work.** `TURING_REQUIRE_BUF=1` (CI-only) governs *one* thing: whether a **missing** buf skips or fails, in `breaking_test.go`. It has no effect on `breaking.sh`, which hard-exits either way. And a buf that is *present but not 1.72.0* is an unconditional `t.Fatalf` — so any buf on `PATH` at the wrong version reddens the first command of the verification matrix below, with no proto changes at all.
- **`generate.sh` refuses anything but exactly `libprotoc 34.1`**, so a newer package-manager protoc earlier on `PATH` fails the gate rather than silently emitting a diff. Keep the pinned build in its own directory and prepend it for proto work only: `PATH="$HOME/.local/opt/protoc-34.1/bin:$PATH" tools/proto/check.sh`. The archive must keep its `include/` tree — thirteen protos import `google/protobuf/*.proto` and `generate.sh` passes only `-I "$PROTO_DIR"`.
- **Dependabot moves none of these.** Its `github-actions` ecosystem updates `uses:` refs only, never a `with: version:` input or a `go install …@vX` run step. Every version in this table except the Go module/container lines is a manual pin.

**Dependency bumps otherwise arrive by Dependabot** (`.github/dependabot.yml`, added in #93), with one `gomod` entry listing all three module directories, one `docker` entry listing four Dockerfile directories, and single entries for npm, pub and github-actions. `.github/workflows/dependabot_test.go` checks that config against a **hardcoded** list of ecosystem/directory pairs — it does not discover sources, and it errors on unexpected entries as well as missing ones — so adding a module or a Dockerfile means editing the config *and* that test.

Flutter being unpinned is worth knowing before debugging a CI-only failure: the runner takes whatever `stable` is that day, so `flutter analyze` can go red on a PR that changed no Dart, and a local pass proves less here than it does for the pinned tools.

## Verification (run the full matrix before claiming work is done)

Run each from the repo root. Subshells, not `cd X && ... && cd ../..` — on failure `&&` short-circuits and strands you in the subdirectory.

The root module requires SQLite FTS5, so its `go test` and `go build` commands must include `-tags sqlite_fts5` (or set `GOFLAGS=-tags=sqlite_fts5`).

```bash
go test -tags sqlite_fts5 ./... -count=1
go test -tags sqlite_fts5 -race ./... -count=1   # CI runs this; concurrency bugs hide without it
go build -tags sqlite_fts5 ./...
( cd turing-backend/mcp-files  && go test ./... -count=1 && go test -race ./... -count=1 && go build ./cmd/server )
( cd turing-backend/mcp-system && go test ./... -count=1 && go test -race ./... -count=1 && go build ./... )
( cd turing-client/turing_app  && flutter analyze && flutter test )
tools/proto/check.sh
golangci-lint run --build-tags sqlite_fts5 ./... ./.github/workflows
( cd turing-backend/mcp-files  && golangci-lint run ./... )
( cd turing-backend/mcp-system && golangci-lint run ./... )
```
The `/verify` skill runs this matrix.

## Gotchas

- **`turing-backend/package.json`'s `lint` script runs a tagged `go test`, not a linter.** The real Go linter is golangci-lint (v2, config at `.golangci.yml`): `golangci-lint run --build-tags sqlite_fts5 ./... ./.github/workflows` from the root (`./...` skips dot-directories, so the CI self-guard package needs naming explicitly), and separately inside `turing-backend/mcp-files` and `turing-backend/mcp-system`. CI's `lint` job runs exactly those three commands (pinned linter version, `--config` passed explicitly), so a lint regression fails the PR. `gofmt` runs automatically on save via the format hook. **`flutter analyze` IS in CI** — it is a gating step of the `Flutter tests` job, ahead of `flutter test`, and it fails the build on warnings, not just errors. Two unused optional parameters were enough to turn a PR red, so run it locally before pushing rather than relying on the test run.
- **Generated proto code is committed.** After editing a `.proto`, use the pinned generator versions from "Toolchain versions" above — they are listed once, there, so this note cannot drift out of step with the workflow. Generation invokes the exact platform shim under `PUB_CACHE` (or Dart's platform-default pub cache), not a PATH shadow, and deliberately limits canonical output to pinned Go and Dart generators. Run `tools/proto/generate.sh` and commit the regenerated `gen/` and `turing-client/turing_app/lib/generated/` output. CI's `tools/proto/check.sh` compares generated bytes, so codegen must be deterministic.
- **CI is self-guarding:** `.github/workflows/ci_test.go` asserts `ci.yml` contains specific commands **and specific pinned versions** — `golangci-lint@v2.12.2`, buf `1.72.0` and the buf-action commit SHA, `protoc_plugin 23.0.0`, and `go-version: "1.25.x"` in **every** `setup-go` job — each asserted inside its own job block, since a count of occurrences is satisfied by five copies in one job and none in another. A job drifting back to `1.23.x` would not fail outright, because `GOTOOLCHAIN=auto` would quietly switch toolchains and pass; that assertion is the only thing that makes the drift visible.

  **`tools/docs/versions_test.go` closes the same loop for the documentation.** It reads each version from the file that enforces it and fails when the table above, or `tools/proto/README.md`, disagrees — and it checks *attribution*, not mere presence, so a claim has to name the thing it describes on the same line. That is why the table gives each generator and each container image its own row: a row packing several claims onto one line lets the guard be satisfied by the neighbouring claim. So bumping any version means editing the workflow, `ci_test.go`, and this table — and two tests will tell you which you missed.
- **mcp-files approval flow:** mutating file tools require a short-lived HS256 approval JWT (signed by the orchestrator after user approval, passed as `params._meta.approvalToken`), verified by mcp-files, then consumed via `ApprovalService.ConsumeApproval` over internal gRPC. The write proceeds only on `APPROVAL_STATUS_CONSUMED`.
- **Compose uses explicit `environment:` blocks (not `env_file:`)** so each service gets only the secrets it needs. MCP servers are never published to the host (internal Docker networks only); only orchestrator `:3000` is public.
- **Skills are files, not durable content rows.** A skill is `skills/<category>/<skill>/SKILL.md`; its relative folder path is its identity. SQLite stores only enablement and per-capability grants during normal operation. The 0011 upgrade is the explicit exception: it retains legacy bodies in `legacy_skill_export_recovery` and re-exports them on every startup because SQLite and the filesystem cannot commit atomically. Conflicts fail closed and keep recovery intact; application code never deletes nonempty recovery. Cleanup is deliberately offline/manual: stop the orchestrator, back up the database, verify every legacy file under `skills/imported/`, then use a SQLite client to `DROP TABLE legacy_skill_export_recovery;` before restarting. The orchestrator snapshots enabled metadata at enqueue and includes bodies/references only for skills whose declared capabilities are all granted. Skill content is untrusted prompt material and a grant controls whether it loads — it does not authorize a tool call.
- **Memory is files too, and only the orchestrator mounts them.** The vault is `memory/persona.md`, `memory/profile.md`, `memory/inbox/`, `memory/beliefs/` under `MEMORY_ROOT` (`/memory`). `scripts/init.sh` provisions it 0700 with both tier directories, secures either pinned document to 0600 without ever rewriting it, and writes an active starter `persona.md`, pinned into every run exactly as written, **only when that file is absent** (it never creates `profile.md` — that one is the user's to write). `scripts/compose.sh` refuses a symlinked, missing, non-directory, or non-0700 host `memory/` before Docker resolves the bind, because the vault's own walk polices only the inside of the mount. `MEMORY_DISPLAY_ROOT` is the same directory named the way the user would name it — `init.sh` writes the canonical host path into `.env` as a Compose single-quoted literal (so `$`, `${...}`, `#`, spaces and apostrophes in a checkout path survive verbatim and no secret is interpolated into it), the compose file passes it through with `${MEMORY_DISPLAY_ROOT:-}`, and `compose.sh` requires it to be a non-empty clean absolute path on every path that starts or resolves services but never on `down`/`stop`/`rm`/`kill`, so teardown still works on a stale `.env` — and it is display-only: `MemorySettings.vault_root` carries it so the app never tells a desktop user their memory is at `/memory`, and nothing reads it for access or confinement. The agent can write into `inbox/` and nowhere else; promotion into `beliefs/` is a user action. `turing-backend/memory/*` is gitignored except `.gitkeep`, and `.dockerignore` excludes it **repository-scoped** — `**/memory` would also hide `agent-runtime-go/internal/memory` and `orchestrator-go/internal/service/memory`. Not shipped and not to be claimed: automatic extraction, revision history for vault files, sensitivity filtering, in-app editing of a proposal before acceptance (edit the inbox file in the vault instead; promotion is hash-bound so an edited candidate is re-read before it is accepted).
- Do not commit `.env` (gitignored; `init.sh` sets it `chmod 600`). `.env.example` is the whitelisted template.

## Running the stack

```bash
cd turing-backend && ./scripts/init.sh   # generates .env, tokens, data/, sandbox/, skills/, mcp/ & memory/ (with a default persona.md); prints the Flutter API key
./scripts/dev.sh                          # docker compose up --build (foreground)
```
Requires Docker + Compose, Go 1.25+ (the root and `mcp-files` modules' floor; see "Toolchain versions"), Flutter, and Ollama running on the host (`OLLAMA_BASE_URL=http://host.docker.internal:11434`, default model `qwen2.5:7b` (~4.9 GB resident). The runtime sends Ollama a per-request `keep_alive` (`OLLAMA_KEEP_ALIVE`, default `2m`) instead of relying on Ollama's own server-side env var, so the model is released once you stop talking to it. Keep it above `TURING_APPROVAL_WAIT_TIMEOUT_MS` or it unloads mid-run). Run the client: `cd turing-client/turing_app && flutter pub get && flutter run -d macos`.

## Review before pushing (required)

Before pushing a branch or opening a PR, dispatch a subagent with **Opus 4.8** to review the full diff. Give it the changed files and ask it to report:
- correctness bugs, edge cases, and gaps against the stated intent
- concrete improvements (reuse, simplification, clearer naming)
- **unit test coverage** — every new behavior and every fixed bug needs a test that fails without the fix; call out untested paths explicitly

Act on the findings (or state why one is rejected) before pushing. Do not treat a green test run as a substitute for this review — tests only prove what they cover.

## Repo etiquette

Work on an isolated git worktree + feature branch; open a PR into `main` (recent history is squash-merged PRs). Do not commit directly to `main`.
