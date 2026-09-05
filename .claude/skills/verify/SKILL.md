---
name: verify
description: Run the full multi-module verification matrix (root Go, mcp-files, mcp-system, Flutter, proto check) before considering work complete. Use before committing, opening a PR, or claiming a change works.
---

Run the full verification matrix from the repo root. This repo has multiple Go modules; the root `go test -tags sqlite_fts5 ./...` does NOT cover the mcp-* modules, so each must be run explicitly.

Run these in order and report the outcome of each. Stop and surface failures — do not claim success unless every step passes.

```bash
# 0. Dependency preparation (may use the network; the docs guard is offline)
(cd turing-backend/mcp-files && go mod download)
(cd turing-backend/mcp-system && go mod download)

# 1. Root module
go test -tags sqlite_fts5 ./... -count=1
go test -tags sqlite_fts5 -race ./... -count=1
go build -tags sqlite_fts5 ./...

# 2. mcp-files (separate module)
( cd turing-backend/mcp-files && go test ./... -count=1 && go test -race ./... -count=1 && go build ./cmd/server )

# 3. mcp-system (separate module — the root ./... never reaches it)
( cd turing-backend/mcp-system && go test ./... -count=1 && go test -race ./... -count=1 && go build ./... )

# 4. Flutter client
( cd turing-client/turing_app && flutter analyze && flutter test )

# 5. Proto contract — regenerates and fails on any git diff
tools/proto/check.sh

# 6. Lint — all three Go modules, same commands as CI's `lint` job.
#    The root module is linted with the build tag it ships with. ./.github/workflows
#    is listed separately because `./...` skips dot-directories, which would
#    otherwise leave the CI self-guard test itself unlinted.
golangci-lint run --build-tags sqlite_fts5 ./... ./.github/workflows
( cd turing-backend/mcp-files  && golangci-lint run ./... )
( cd turing-backend/mcp-system && golangci-lint run ./... )
```

Notes:
- If you changed a `.proto`, run `tools/proto/generate.sh` and commit the regenerated code first; otherwise step 5 will fail on the diff.
- Every step above now has a CI counterpart (jobs: `go`, `mcp-files`, `mcp-system`, `flutter`, `proto-and-scripts`, `lint`), so a failure here is a failure that will block the PR. Running the matrix locally is about finding it first, not about covering a CI gap.
- Step 6 needs golangci-lint v2 on PATH (`golangci-lint --version`). If it is missing, say so rather than skipping silently — CI will still run it.
- If a step is skipped for a reason (e.g. Flutter not installed locally), say so explicitly rather than silently passing.
