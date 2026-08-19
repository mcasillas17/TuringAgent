# TUR-019 protobuf compatibility implementation plan

**Goal:** Add a pinned protobuf compatibility guard that accepts additive schema changes and rejects live-field removal or renumbering against the current base branch.

**Architecture:** Buf CLI 1.72.0 applies its `FILE` breaking rules to the `proto` module. A repository shell script refreshes a validated remote-tracking base ref before comparing schemas, while real fixture tests and CI self-guard tests lock down behavior and workflow wiring. Existing pinned `protoc` generation remains unchanged.

**Tech stack:** Protocol Buffers, Buf CLI 1.72.0, Bash, Go tests, Git, GitHub Actions.

---

## File map

- Create `buf.yaml`: declare the `proto` module and `FILE` breaking policy.
- Create `tools/proto/breaking.sh`: validate tools/base ref, refresh the base branch, resolve its commit, and run Buf.
- Create `tools/proto/breaking_test.go`: exercise the repository script with real Buf and temporary Git repositories.
- Create `tools/proto/testdata/breaking/{base,additive,removed,removed_reserved,renumbered}/turing/v1/example.proto`: compatibility regression schemas.
- Modify `.github/workflows/ci.yml`: install pinned Buf and run the compatibility guard and required fixture tests.
- Modify `.github/workflows/ci_test.go`: guard the exact action pin, CLI version, base-ref command, fixture test command, and deterministic generation command.
- Modify `tools/proto/README.md`: document installation, compatibility policy, local usage, and reservation rules.
- Modify `docs/architecture/2026-08-18-personal-agent-audit.md`: record TUR-019 implementation artifacts without marking an unmerged PR as landed.

### Task 1: Add failing compatibility regression fixtures

**Files:**
- Create: `tools/proto/breaking_test.go`
- Create: `tools/proto/testdata/breaking/base/turing/v1/example.proto`
- Create: `tools/proto/testdata/breaking/additive/turing/v1/example.proto`
- Create: `tools/proto/testdata/breaking/removed/turing/v1/example.proto`
- Create: `tools/proto/testdata/breaking/removed_reserved/turing/v1/example.proto`
- Create: `tools/proto/testdata/breaking/renumbered/turing/v1/example.proto`

- [ ] **Step 1: Add the baseline and candidate schemas**

The baseline message contains `id = 1` and `display_name = 2`. The additive fixture keeps both and adds a field, enum, message, and service. The removed fixture deletes `display_name`; the removed-and-reserved fixture deletes it while reserving both its old name and number, proving `FILE` remains stricter than wire-only policies. The renumbered fixture moves `display_name` from 2 to 3.

```proto
syntax = "proto3";

package turing.v1;

message CompatibilityExample {
  string id = 1;
  string display_name = 2;
}
```

- [ ] **Step 2: Add a real Buf integration test**

`TestBreakingCompatibility` locates Buf, requiring it when `TURING_REQUIRE_BUF=1`; builds a temporary repository with a bare `origin`; pushes the baseline to `main`; replaces the working schema with each fixture; invokes the not-yet-created `tools/proto/breaking.sh`; and asserts:

```go
tests := []struct {
	name           string
	fixture        string
	wantFailure    bool
	wantDiagnostic []string
}{
	{name: "additive field", fixture: "additive"},
	{
		name:           "removed live field",
		fixture:        "removed",
		wantFailure:    true,
		wantDiagnostic: []string{`field "2"`, "was deleted"},
	},
	{
		name:           "renumbered live field",
		fixture:        "renumbered",
		wantFailure:    true,
		wantDiagnostic: []string{`field "2"`, "was deleted"},
	},
}
```

The tests copy the repository script and `buf.yaml` into temporary full and shallow checkouts, delete `refs/remotes/origin/main`, and verify the script restores that ref without changing the repository's existing shallow/full state. This proves the compatibility behavior and missing-base retrieval through the same entry point CI uses.

- [ ] **Step 3: Run the regression test and observe the intended failure**

Run:

```bash
TURING_REQUIRE_BUF=1 go test ./tools/proto -run '^TestBreakingCompatibility$' -count=1
```

Expected result: failure because `tools/proto/breaking.sh` and `buf.yaml` do not exist yet.

- [ ] **Step 4: Commit the red tests**

```bash
git add tools/proto/breaking_test.go tools/proto/testdata/breaking
git commit -m "test(proto): define compatibility regressions" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

### Task 2: Implement the Buf compatibility guard

**Files:**
- Create: `buf.yaml`
- Create: `tools/proto/breaking.sh`
- Modify: `tools/proto/breaking_test.go`

- [ ] **Step 1: Declare the compatibility policy**

```yaml
version: v2
modules:
  - path: proto
breaking:
  use:
    - FILE
```

- [ ] **Step 2: Add script unit cases before the implementation**

Add tests that place a fake `buf` on `PATH` and assert:

- version `9.9.9` fails with `buf 1.72.0 is required`;
- base `main` fails with `base ref must be a remote-tracking ref`;
- base `origin/../main` fails before any fetch.

Run:

```bash
go test ./tools/proto -run 'TestBreakingRejects' -count=1
```

Expected result: failure because `breaking.sh` does not exist.

- [ ] **Step 3: Implement `tools/proto/breaking.sh`**

The script uses `set -euo pipefail`, resolves the repository root from its own path, checks `git` and `buf`, and requires `buf --version` to equal `1.72.0`. The base defaults to `${PROTO_BREAKING_BASE_REF:-origin/main}` and must match a real configured remote plus a branch accepted by `git check-ref-format --branch`.

The refresh and comparison commands preserve full history while keeping an existing shallow checkout shallow. The forced refspec accepts a base branch whose tip was legitimately rewritten, and the exact resolved commit is exported locally for Buf:

```bash
fetch_args=(--no-tags)
if [[ "$(git -C "$ROOT" rev-parse --is-shallow-repository)" == "true" ]]; then
  fetch_args+=(--depth=1)
fi
git -C "$ROOT" fetch "${fetch_args[@]}" "$remote" \
  "+refs/heads/$branch:refs/remotes/$remote/$branch"
base_commit="$(git -C "$ROOT" rev-parse --verify "refs/remotes/$remote/$branch^{commit}")"
baseline="$(mktemp -d "${TMPDIR:-/tmp}/turing-proto-breaking.XXXXXX")"
git -C "$ROOT" archive "$base_commit" proto | tar -x -C "$baseline"
buf breaking "$ROOT/proto" --against "$baseline/proto"
```

Every failure path prints the failing tool, ref, or recovery action. A fetch failure exits; it never reuses an older remote-tracking ref.

- [ ] **Step 4: Run focused tests**

Run:

```bash
go test ./tools/proto -run 'TestBreaking' -count=1
TURING_REQUIRE_BUF=1 go test ./tools/proto -run '^TestBreakingCompatibility$' -count=1
```

Expected result: additive fixture passes; removal and renumbering are detected as expected; invalid refs and unsupported versions fail.

- [ ] **Step 5: Confirm deterministic generation remains unchanged**

Run:

```bash
tools/proto/check.sh
git status --short
```

Expected result: generation check passes and no generated Go or Dart files change.

- [ ] **Step 6: Commit the guard**

```bash
git add buf.yaml tools/proto/breaking.sh tools/proto/breaking_test.go
git commit -m "feat(proto): enforce source compatibility" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

### Task 3: Wire compatibility into CI under self-guard tests

**Files:**
- Modify: `.github/workflows/ci_test.go`
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add failing CI self-guard assertions**

Assert the proto job contains all of these exact fragments:

```go
requireContains(t, protoJob, "uses: bufbuild/buf-action@8c6a16e16f12ba20b6470afa9c2ba9b5ba8c97c3 # v1.5.0")
requireContains(t, protoJob, `version: "1.72.0"`)
requireContains(t, protoJob, "setup_only: true")
requireContains(t, protoJob, `TURING_REQUIRE_BUF=1 go test ./tools/proto -run '^TestBreaking' -count=1`)
requireContains(t, protoJob, `tools/proto/breaking.sh "origin/${GITHUB_BASE_REF:-main}"`)
requireContains(t, protoJob, "tools/proto/check.sh")
requireContains(t, protoJob, "bash -n tools/proto/breaking.sh turing-backend/scripts/compose.sh turing-backend/scripts/dev.sh turing-backend/scripts/init.sh turing-backend/scripts/reset.sh turing-backend/scripts/rotate-client-key.sh turing-backend/scripts/smoke-grpc.sh turing-backend/scripts/smoke.sh turing-backend/scripts/verify-tool-loop.sh")
```

- [ ] **Step 2: Run the workflow test and observe failure**

Run:

```bash
go test -tags sqlite_fts5 ./.github/workflows -run 'TestCIWorkflowCoversCoreChecks' -count=1
```

Expected result: failure naming the first missing Buf workflow fragment.

- [ ] **Step 3: Add pinned Buf setup and checks to the proto job**

Add setup-only Buf action configuration and these repository commands:

```yaml
- name: Set up Buf
  uses: bufbuild/buf-action@8c6a16e16f12ba20b6470afa9c2ba9b5ba8c97c3 # v1.5.0
  with:
    version: "1.72.0"
    setup_only: true

- name: Test protobuf compatibility guard
  run: TURING_REQUIRE_BUF=1 go test ./tools/proto -run '^TestBreaking' -count=1

- name: Check protobuf compatibility
  run: tools/proto/breaking.sh "origin/${GITHUB_BASE_REF:-main}"
```

Extend the shell syntax command with `tools/proto/breaking.sh`; do not alter the existing pinned generator setup or deterministic generation step.

- [ ] **Step 4: Run focused CI and proto tests**

Run:

```bash
go test -tags sqlite_fts5 ./.github/workflows ./tools/proto -count=1
bash -n tools/proto/breaking.sh
```

Expected result: all tests and shell syntax checks pass.

- [ ] **Step 5: Commit CI wiring**

```bash
git add .github/workflows/ci.yml .github/workflows/ci_test.go
git commit -m "ci: check protobuf compatibility against base" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

### Task 4: Document the policy and roadmap artifacts

**Files:**
- Modify: `tools/proto/README.md`
- Modify: `docs/architecture/2026-08-18-personal-agent-audit.md`

- [ ] **Step 1: Document local installation and use**

Add Buf 1.72.0 to the pinned toolchain and document:

```bash
tools/proto/breaking.sh
tools/proto/breaking.sh origin/release-branch
```

Explain that the script fetches the named remote branch, `FILE` protects generated source, additive changes pass, and deletion/renumbering fails. State that future intentional removal under a versioned policy must reserve both the old name and number; no reservations are added in TUR-019 because mainline history contains no proven removals.

- [ ] **Step 2: Record implementation artifacts in the audit**

Under TUR-019, add an implementation note naming:

- `buf.yaml`;
- `tools/proto/breaking.sh`;
- fixture coverage in `tools/proto/breaking_test.go`;
- CI and self-guard coverage;
- Buf 1.72.0 with `FILE`;
- pull request pending merge.

Do not mark TUR-019 complete or landed.

- [ ] **Step 3: Check documentation diff**

Run:

```bash
git diff --check
```

Expected result: no whitespace errors.

- [ ] **Step 4: Commit documentation**

```bash
git add tools/proto/README.md docs/architecture/2026-08-18-personal-agent-audit.md
git commit -m "docs: record protobuf compatibility policy" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

### Task 5: Reconcile, review, and verify

**Files:** all branch changes.

- [ ] **Step 1: Merge current `origin/main` normally**

```bash
git fetch origin main
git merge --no-edit origin/main
```

Resolve conflicts by preserving both landed main changes and TUR-019. Do not rebase, rewrite, or force-push.

- [ ] **Step 2: Run parallel full-diff review rounds**

For each fresh round, provide the complete `origin/main...HEAD` diff to Claude Opus 5 and GPT-5.6 Luna. Address every valid correctness, edge-case, naming, simplification, and unit-coverage finding. Repeat with fresh contexts until both reviewers explicitly report no remaining feedback. Record round number, reviewer, result, and addressed findings.

- [ ] **Step 3: Run the repository-policy review**

Provide the final full diff and repository policy to Claude Opus 4.8. Address every valid finding, then rerun focused tests affected by any changes.

- [ ] **Step 4: Run the complete verification matrix**

Invoke the repository `verify` skill after the final edit and final main reconciliation. It must cover root Go tests/race/build with `sqlite_fts5`, both nested modules' tests/race/builds, Flutter analyze/tests, deterministic proto check, and all three golangci-lint commands.

- [ ] **Step 5: Commit review fixes if needed**

```bash
git add buf.yaml tools/proto/breaking.sh tools/proto/breaking_test.go \
  tools/proto/README.md .github/workflows/ci.yml .github/workflows/ci_test.go \
  docs/architecture/2026-08-18-personal-agent-audit.md
git commit -m "fix: address protobuf compatibility review" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

### Task 6: Publish and inspect the pull request

**Files:** GitHub pull request metadata.

- [ ] **Step 1: Push normally and create the focused PR**

Push the branch without force, create a PR into `main`, apply `turing-roadmap`, and include scope, docs, compatibility mechanism/version, review rounds, and fresh verification in the body.

- [ ] **Step 2: Read live PR state**

Use GitHub's live state to confirm the PR remains open and reports mergeable/clean. Read all six CI job conclusions; if any job is still queued or running, report it as pending rather than claiming success.

- [ ] **Step 3: Report to the coordinator**

Send session `62cf9513-4bf4-4de3-a427-bc9d705157c3` the PR URL, head SHA, Buf mechanism/version, changed docs, review rounds/results, full verification result, live mergeability, six-job CI status, and downstream dependencies. Do not merge the PR.
