# TUR-019 protobuf compatibility design

## Goal

Protect the checked-in `turing.v1` protobuf contract before memory APIs expand it. A pull request may add fields, messages, enums, or RPCs, but it must not remove or renumber a live field or otherwise break generated Flutter, Go, or future client source.

## Existing constraints

- `proto/turing/v1` is the schema source of truth.
- Go and Dart output is generated deterministically by `tools/proto/generate.sh` with pinned generators and checked by `tools/proto/check.sh`.
- CI checks out shallow history by default.
- The mainline protobuf history contains no removed fields, enum values, or proto files, so there are no proven historical names or numbers to reserve in this change.
- The compatibility check must work against the pull request's actual base branch in CI and against `origin/main` by default for local development.

## Considered approaches

### 1. Buf CLI `FILE` compatibility (selected)

Add a small Buf v2 workspace configuration for `proto`, pin Buf CLI 1.72.0, and wrap `buf breaking` in a repository script that refreshes and resolves the requested remote-tracking base ref.

`FILE` is the strictest built-in compatibility category. It protects generated source as well as wire and JSON compatibility, which matches the repository's checked-in Flutter and Go clients. Buf's native Git input can compare the working schema with the exact base commit without maintaining a second schema snapshot.

### 2. Hand-written descriptor comparison

Compile both schemas to descriptor sets and compare fields in repository-owned Go code. This avoids a new executable but would recreate only part of protobuf compatibility semantics and would need continuing maintenance for messages, enums, services, oneofs, maps, options, and future editions.

### 3. Checked-in baseline descriptor

Commit a descriptor image and compare changes against it. This works offline, but every accepted additive change requires an unrelated baseline update and the checked-in image can drift from `main`. It also weakens the requirement to compare the pull request with its current base.

## Architecture

`buf.yaml` defines one module rooted at `proto` and enables the `FILE` breaking category. It does not replace `protoc` generation, add Buf linting, or change generated output.

`tools/proto/breaking.sh` owns repository-specific orchestration:

1. Require Git and Buf CLI 1.72.0.
2. Accept a remote-tracking base ref, defaulting to `origin/main`.
3. Validate the ref shape before using it as a fetch destination.
4. Refresh that branch from its configured remote with a depth-one fetch. This makes shallow CI checkouts safe and prevents a stale local remote-tracking ref from silently becoming the baseline.
5. Resolve the fetched commit and run `buf breaking proto --against ".git#ref=<commit>,subdir=proto"`.

The script emits distinct diagnostics for a missing tool, unsupported Buf version, invalid base ref, failed fetch, unresolved commit, and schema incompatibility. It never falls back to an older local baseline after a fetch failure.

CI installs Buf 1.72.0 with the official Buf action in setup-only mode, then calls the repository script with `origin/${{ github.base_ref }}` for pull requests and `origin/main` for pushes. The existing deterministic generation step remains unchanged and runs in the same job.

## Testing

Real compatibility fixtures start from one baseline message and exercise three candidate schemas:

- additive field: succeeds;
- removed live field: fails with a field-deletion diagnostic;
- renumbered live field: fails because the original field number was deleted.

The Go regression test creates a temporary shallow-style Git repository with a bare `origin`, publishes the baseline to `main`, installs each candidate as the working tree, and invokes `tools/proto/breaking.sh`. It skips only when Buf is absent so ordinary root tests remain portable; the proto CI job installs the pinned Buf version and reruns the compatibility tests with skipping disabled.

CI self-guard tests assert the exact pinned setup action inputs, compatibility command, base-ref expression, and deterministic generation command. Script unit coverage also checks unsupported Buf versions and invalid base refs.

## Documentation and rollout

`tools/proto/README.md` documents installation, local usage, the `FILE` policy, and the rule that a removed field or enum value must retain both its old name and number as `reserved` if a future versioned API policy ever permits removal. No reservation is added now because mainline history proves no field has been removed.

The architecture audit records the implementation artifacts and pull-request status without marking TUR-019 as landed before merge.
