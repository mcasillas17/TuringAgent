---
name: verify
description: Run the full multi-module verification matrix (root Go, mcp-files, mcp-system, Flutter, proto check) before considering work complete. Use before committing, opening a PR, or claiming a change works.
---

Run the full verification matrix from the repo root. This repo has multiple Go modules; the root `go test ./...` does NOT cover the mcp-* modules, so each must be run explicitly.

Run these in order and report the outcome of each. Stop and surface failures — do not claim success unless every step passes.

```bash
# 1. Root module
go test ./... -count=1
go build ./...

# 2. mcp-files (separate module)
( cd turing-backend/mcp-files && go test ./... -count=1 && go build ./cmd/server )

# 3. mcp-system (separate module, NOT covered by CI — always run manually)
( cd turing-backend/mcp-system && go test ./... -count=1 && go build ./... )

# 4. Flutter client
( cd turing-client/turing_app && flutter test )

# 5. Proto contract — regenerates and fails on any git diff
tools/proto/check.sh
```

Notes:
- If you changed a `.proto`, run `tools/proto/generate.sh` and commit the regenerated code first; otherwise step 5 will fail on the diff.
- Only steps 1, 2, 4, and 5 run in CI. Step 3 (mcp-system) is not in CI — running it here is the only automatic coverage it gets.
- If a step is skipped for a reason (e.g. Flutter not installed locally), say so explicitly rather than silently passing.
