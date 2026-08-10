# MCP Security and Integration Guide

This document describes the two Go-based MCP (Model Context Protocol) servers
delivered for TuringAgent v1.0 — `mcp-system` and `mcp-files` — and the
security model that bounds what the assistant runtime can do through them. It
is paired with the integrated v1 runtime/orchestrator implementation on
`pturing-v1-base`.

The intent of this guide is that a reviewer can evaluate the security posture
without reading the Go source.

## Overview

TuringAgent v1.0 splits agent execution out of the orchestrator. The agent
runtime calls a small number of MCP servers over JSON-RPC 2.0 (Streamable HTTP)
on the internal Docker network. Two MCP servers are in scope for v1.0:

| Service       | Port            | Bearer token env              | Purpose                                                |
|---------------|-----------------|-------------------------------|--------------------------------------------------------|
| `mcp-system`  | `:7100/mcp`     | `MCP_SYSTEM_TOKEN_GENERAL`    | Safe "beacon" tools the runtime can call without approval. |
| `mcp-files`   | `:7110/mcp`     | `MCP_FILES_TOKEN_GENERAL`     | Sandboxed filesystem access; mutating tools are approval-gated. |

Both servers expect to be reachable only from the orchestrator and the agent
runtime over the internal Docker network. Neither server is published to the
host in the expected v1.0 compose layout.

The default Compose stack starts both servers internally; neither `/mcp`
endpoint is published to the host.

## System MCP server (`mcp-system`)

`mcp-system` exposes four read-only tools that the runtime can invoke without
involving the approval pipeline. All four are labeled `safe` in the server's
own `tools/list` response.

### Tools

| Name            | Policy | Request fields | Response fields                                           |
|-----------------|--------|----------------|-----------------------------------------------------------|
| `system.health` | `safe` | (none)         | `ok: bool`, `service: "turing-mcp-system"`                |
| `system.time`   | `safe` | (none)         | `iso: string` (RFC3339Nano UTC), `unixMs: int`, `timezone: "UTC"` |
| `system.echo`   | `safe` | `text: string` | `text: string`                                            |
| `system.info`   | `safe` | (none)         | `os: string`, `arch: string`, `runtime: string` (Go version) |

Deliberate omissions:

- `system.info` does **not** expose process environment variables. There is a
  unit test (`TestSystemInfoDoesNotExposeSecrets`) that asserts the response
  has no `env` key. Adding an `env` field would be a regression.
- `system.time` always reports UTC. The `timezone` field is hard-coded.

### Authentication

All `/mcp` requests must include `Authorization: Bearer <MCP_SYSTEM_TOKEN_GENERAL>`.
The middleware (`internal/auth/auth.go`) treats an **empty configured token**
as outright rejection — that is, if `MCP_SYSTEM_TOKEN_GENERAL` is unset, every
request is denied with `401 Unauthorized`. This prevents accidental
"no token = open" deployments.

There is no per-agent identity in `mcp-system`; the single bearer is enough
because every tool is safe and read-only.

### JSON-RPC 2.0 envelope

Both servers speak JSON-RPC 2.0 with a single HTTP endpoint at `/mcp`. The
request and response shapes are identical across both services.

`tools/list` request:

```json
{ "jsonrpc": "2.0", "id": 1, "method": "tools/list" }
```

`tools/list` response (system server):

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "tools": [
      { "name": "system.health", "policy": "safe" },
      { "name": "system.time",   "policy": "safe" },
      { "name": "system.echo",   "policy": "safe" },
      { "name": "system.info",   "policy": "safe" }
    ]
  }
}
```

`tools/call` request (echo example):

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "system.echo",
    "arguments": { "text": "hello" }
  }
}
```

`tools/call` response:

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": { "text": "hello" }
}
```

Unknown tools return a JSON-RPC error with code `-32601` (`method not found`)
and message `unknown tool`. Unknown methods return the same code with message
`method not found`.

## Files MCP server (`mcp-files`)

`mcp-files` exposes a sandboxed filesystem under a single configurable root.
The server returns a tool list that is the contract between runtime, policy
engine, and operators:

| Name           | Policy              |
|----------------|---------------------|
| `files.list`   | `safe`              |
| `files.search` | `safe`              |
| `files.read`   | `safe`              |
| `files.create` | `approval_required` |
| `files.update` | `approval_required` |
| `files.delete` | `disabled`          |
| `files.move`   | `disabled`          |

`files.delete` and `files.move` are advertised in `tools/list` but the dispatcher
returns `tool disabled` for any `tools/call` against them. They cannot be enabled
without a code change.

### Tool request and response shapes

All paths are interpreted relative to the sandbox root. A leading `/` is
trimmed by the resolver, so `"/notes/today.md"` and `"notes/today.md"` both
resolve to the same location under the sandbox.

`files.list`:

- Request: `{ "path": string }`
- Response: `{ "items": [ { "name": string, "isDir": bool }, ... ] }`

`files.search`:

- Request: `{ "path": string, "query": string, "limit"?: number }`
- Response: `{ "matches": [ { "path": string, "snippet": string }, ... ] }`
- `query` is required; an empty string is rejected.
- `limit` defaults to `50` and is clamped to `200`.
- `snippet` is up to 40 characters either side of the first match in each file.

`files.read`:

- Request: `{ "path": string, "maxBytes"?: number }`
- Response: `{ "path": string, "content": string, "truncated": bool }`
- See "Read limits and content rules" below.

`files.create` and `files.update`:

- Request: `{ "path": string, "content": string, "expectedHash"?: string }`
  (`expectedHash` is honored only by `files.update`.)
- Response: `{ "path": string, "sha256": string }` where `sha256` is the
  `sha256:<hex>` digest of the written content.
- Both require an approval JWT delivered out-of-band on the JSON-RPC envelope
  (see below). Neither will touch disk before the approval has been validated
  *and* consumed.

#### `_meta.approvalToken` channel

For `files.create` and `files.update`, the caller must place the approval JWT
on the `params._meta.approvalToken` field of the JSON-RPC request, not inside
`arguments`. Example:

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "method": "tools/call",
  "params": {
    "name": "files.create",
    "arguments": { "path": "notes/today.md", "content": "..." },
    "_meta": { "approvalToken": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." }
  }
}
```

Keeping the token outside `arguments` is what allows the approval to bind to
a hash of `arguments` without that hash including the token itself (see
"Approval JWT validation" below).

### Authentication and agent identity

All requests must include `Authorization: Bearer <MCP_FILES_TOKEN_GENERAL>`.
The middleware in `internal/auth/auth.go` enforces this and, on success,
returns the **agent identity** that the rest of the request will be bound to.

In v1.0 there is a single token mapped to a single agent:

```go
// v1.0 has one runtime/MCP token for the general assistant; v1.1 should
// replace this with a token-to-agent map.
return "general_assistant", nil
```

That `general_assistant` value is what flows downstream into approval JWT
verification (`sub` claim) — so widening the agent map without also updating
the orchestrator's JWT signer would invalidate approvals.

An empty configured token is treated as rejection, identical to `mcp-system`.

## Sandbox model

The sandbox is the security heart of `mcp-files`. Every path-bearing tool
(read, list, search, create, update) routes through a single `resolve` step
that is designed to fail closed against both `..` traversal and symlink
confused-deputy escapes.

### Sandbox root

The sandbox root is configured by `FILES_SANDBOX_ROOT` (default `/sandbox`).
The standalone image runs as its fixed non-root UID/GID 1000. For the local
Compose workflow, `scripts/init.sh` records the host account as `HOST_UID` and
`HOST_GID`, and Compose runs `mcp-files` with that identity so the process can
write to the host-owned `sandbox/` bind mount. Compose defaults both values to
1000 when they are unset; the sandbox is not made world-writable.
On construction, the root is processed in two steps:

1. `filepath.Abs(root)` — make it absolute.
2. `filepath.EvalSymlinks(abs)` — canonicalize through any symlinks on the
   parent chain. If `EvalSymlinks` fails, the original absolute path is kept.

Step 2 matters because, without it, environments where the parent path
traverses a symlink — macOS (`/var` is a symlink to `/private/var`),
container bind mounts, automount points — will compare a non-canonical root
against a canonicalized resolved target, and every legitimate call will
register as a false-positive "escape". The plan as originally written did
only `filepath.Abs`; the implementation does both. This is a **spec
correction** worth flagging: tests on macOS will not pass against the
plan's literal pseudocode, but they do pass against the shipped resolver.

### Per-call resolve algorithm

For a caller-supplied `path`:

1. Strip a leading `/`, then `filepath.Clean` to fold out `.` and `..`.
2. Join with the sandbox root.
3. Walk up the path until an existing ancestor is found, accumulating the
   missing trailing components. If the walk reaches the filesystem root
   (`parent == existing`) without finding an existing ancestor, reject with
   `path escapes sandbox`.
4. `filepath.EvalSymlinks` on the existing ancestor.
5. Rejoin the resolved ancestor with the missing trailing components.
6. `filepath.Rel(root, resolved)`. Reject with `path escapes sandbox` if
   the relative path starts with `..` or is absolute.

Two consequences:

- Traversal (`"../../etc/passwd"`) and symlink escape (a `link.txt` inside
  the sandbox whose target is outside) both fail at step 6. There is a unit
  test for each (`TestReadRejectsTraversal`, `TestReadRejectsSymlinkEscape`).
- `files.create` can target a path whose directory does not yet exist —
  step 3 is what supports that — because the resolver canonicalizes the
  deepest existing ancestor instead of requiring the full path to exist.

### Search and symlinks

`files.search` walks the resolved root with `filepath.WalkDir`. For each
entry it:

- Skips directories.
- Skips any entry whose `os.DirEntry.Type()` reports the symlink bit
  (`os.ModeSymlink`). Symlinks are never followed during the walk.
- Re-resolves the candidate via `EvalSymlinks` and the same `filepath.Rel`
  check as the resolver. If the resolved candidate falls outside the root
  (which it should not, because we already filtered symlinks, but the check
  is defensive), the entry is skipped.
- Skips files larger than `maxReadBytes` and skips non-UTF-8 files.

The result: symlink-bearing entries cannot produce search hits, and search
cannot leak content from outside the sandbox even if a symlink was somehow
walked. `TestSearchRejectsSymlinkEscape` exercises this path.

## Read limits and content rules

`files.read` enforces three layers of size and content discipline:

- **Default cap:** `defaultReadBytes = 64 * 1024` (64 KiB).
- **Maximum honored cap:** `maxReadBytes = 512 * 1024` (512 KiB). The caller's
  `maxBytes` argument is clamped to this value and then used as the truncation
  threshold.
- **Hard rejection on stat:** if `os.Stat().Size()` exceeds `maxReadBytes`,
  the read is rejected with `file too large` — the file is not opened, not
  read, not truncated. This is the DoS bound. `TestReadRejectsFileTooLarge`
  asserts this behavior.
- **UTF-8 enforcement:** the entire file must be valid UTF-8. If
  `utf8.Valid(content)` is false, the read is rejected with
  `unsupported media type`. Binary blobs and files containing invalid UTF-8
  sequences never reach the caller. `TestReadRejectsBinaryContent` asserts this.
- **Truncation hygiene:** when truncation does fire, the content is sliced to
  the byte cap and then trailing bytes are trimmed off until the result is
  again valid UTF-8. This prevents partial code points from being returned
  to the model as garbled glyphs.
- **`truncated` flag:** the response includes `truncated: true` whenever the
  cap fired. Callers should treat the content as a prefix when this flag is
  set.

## Approval JWT validation

`files.create` and `files.update` are the only mutating tools, and both are
gated by an approval JWT that the orchestrator signs. The JWT verifier lives
in `internal/approval/jwt.go` and is invoked from the tool dispatcher before
any file I/O. Verification has six steps; **none of them fails open**.

### Algorithm

- Only **HS256** is accepted. The token header's `alg` field is parsed and
  compared *before* the signature is verified and *before* any payload claim
  is parsed. Tokens with any other `alg` (including `none`, `RS256`, etc.) are
  rejected with `invalid token algorithm`.
- The signing input is `base64url(header) + "." + base64url(payload)`.
- The signature is recomputed with `hmac.New(sha256.New, secret)` and compared
  against the third token segment using `hmac.Equal` (constant-time
  comparison). Mismatch returns `invalid signature`.
- Only after the signature checks out does the verifier decode and unmarshal
  the payload.

### Time bound

The decoded payload's `exp` claim is compared to `time.Now().Unix()`. If
`exp < now`, the verifier returns `token expired`. There is no skew
tolerance.

### Bound claims

After the structural and time checks pass, the consumer enforces four claim
bindings:

| Claim       | Required value                              | Threat addressed                                  |
|-------------|---------------------------------------------|---------------------------------------------------|
| `aud`       | `"mcp-files"`                               | Token issued for another service is not accepted. |
| `sub`       | `agentID` returned by `AgentFromBearer`     | Approval issued to a different agent is not accepted. |
| `tool`      | `params.name` of the JSON-RPC call          | An approval for `files.update` cannot be replayed as `files.create`, and vice versa. |
| `args_hash` | `sha256(canonical JSON of arguments)`       | An approval bound to one set of arguments cannot be replayed with different arguments. |

`TestValidateRejectsMismatchedApprovalBinding` exercises each of these four
bindings independently.

### Canonical argument hashing

The `args_hash` is computed as:

```
sha256:<hex>  where  hex = sha256(canonicalJSON(arguments))
```

`canonicalJSON` is Go's `encoding/json` encoder output, with
`SetEscapeHTML(false)` and the trailing newline trimmed. It is
deterministic on a `map[string]any` because Go's encoder sorts map keys.

There is a parity test, `TestCanonicalArgsHashMatchesTypeScriptFixture`,
that pins the hash for `{"B": 1, "a": 2}` to:

```
sha256:812e5e7fb7bb816dc477e91a136430192eadcf83ff303881298146e106ae0161
```

This fixture is the source of truth for v1.0. The Go orchestrator signer must
reproduce the same canonical-JSON byte sequence so its hash matches. If the
orchestrator emits a different hash (different key ordering, different HTML
escaping, different whitespace), every approval will fail with
`approval args_hash does not match call`.

### Single-use consume

After the four claim checks pass, the verifier calls the orchestrator's gRPC
`ApprovalService.ConsumeApproval` method with the JWT's unique identifier
(`jti`). This is what makes approvals single-use.

- Address: `${ORCHESTRATOR_GRPC_ADDR}`. The default is
  `turing-orchestrator:3001`.
- Metadata: `authorization: Bearer ${TURING_INTERNAL_TOKEN}`.
- Response handling:
  - `APPROVAL_STATUS_CONSUMED` — the JWT was unused; the verifier returns
    success and the write proceeds.
  - `FailedPrecondition` — the JWT was already used, or the underlying
    approval record was never approved. The verifier returns
    `approval already consumed or not approved` and the write is aborted.
  - Any other gRPC error — generic failure: `approval consume failed: <error>`.
    The write is aborted.

The write happens **only after consume returns consumed**. In other words, a
successful consume is the act of marking the JWT used, and any failure path
after that point (write error, etc.) does not roll consume back. This is
intentional: a partially completed write is still a "used" approval.

`TestValidateRejectsConsumeReplayConflict` asserts the `FailedPrecondition`
path.

### Failure-mode ordering

The verification pipeline is, in order:

1. Wrong segment count or non-base64url segments — `invalid token`.
2. Wrong `alg` — `invalid token algorithm`.
3. Invalid signature — `invalid signature`.
4. Expired (`exp < now`) — `token expired`.
5. `aud != "mcp-files"` — `invalid approval audience`.
6. `sub != agentID` — `approval subject does not match agent`.
7. `tool != params.name` — `approval tool does not match call`.
8.  `args_hash` mismatch — `approval args_hash does not match call`.
9.  Consume returns gRPC `FailedPrecondition` — `approval already consumed or not approved`.
10. Consume returns any other gRPC error — `approval consume failed: <error>`.

None of these branches fall through to the I/O path.

## Failure modes and security rationale

A concise mapping from rejection to threat:

- **Path traversal (`..` segments).** Resolver step 6 (`Rel` + `..`/absolute
  check) refuses to return paths outside the sandbox root.
- **Symlink confused deputy.** Two layers: the resolver canonicalizes via
  `EvalSymlinks` and re-validates with `Rel`; `files.search` additionally
  refuses to walk symlink entries at all.
- **Oversized read DoS.** `os.Stat().Size() > maxReadBytes` is a hard
  rejection before any read occurs. Memory ceiling is 512 KiB per read.
- **Binary / non-UTF-8 leakage to the model.** Whole-file UTF-8 validation
  refuses non-UTF-8 content. Truncation trims trailing partial code points.
- **Approval replay across tools.** `tool` claim must equal the JSON-RPC
  method's `name` argument.
- **Approval replay across arguments.** `args_hash` claim must equal the
  canonical-JSON SHA-256 of the call's `arguments`.
- **Approval reuse.** Single-use `jti` consume against the orchestrator;
  `FailedPrecondition` aborts the write.
- **Token-to-agent mismatch.** `sub` claim must equal the agent identity
  the bearer mapped to. v1.0 has only one agent, but the binding is in
  place for v1.1.
- **No-token "open" misconfiguration.** Empty configured bearer in either
  service is treated as rejection rather than as "everyone allowed".
- **Non-HS256 token substitution (`alg: "none"`, `alg: "RS256"`).** Header
  `alg` is checked before signature verification or claim parse.
- **Disabled mutating tools (`delete`, `move`).** Returned as `tool disabled`
  by the dispatcher; cannot be enabled without a code change.

## Runtime / orchestrator integration

### Agent runtime (Tasks 8 and 11)

- Holds one bearer token per MCP server: `MCP_SYSTEM_TOKEN_GENERAL` and
  `MCP_FILES_TOKEN_GENERAL`.
- Routes JSON-RPC requests to `http://turing-mcp-system:7100/mcp` and
  `http://turing-mcp-files:7110/mcp` on the internal Docker network. Both
  base URLs are configurable through `MCP_SYSTEM_BASE_URL` and
  `MCP_FILES_BASE_URL`, with those Docker-network URLs as defaults.
- For approval-gated tools, attaches the orchestrator-issued JWT to
  `params._meta.approvalToken`, not to `params.arguments`.
- Treats any HTTP non-2xx from an MCP server as a hard error (e.g.
  `MCP HTTP 401`) rather than as a tool result.

### Orchestrator

The orchestrator implements the signing side and the gRPC consume method that
the Files MCP verifier calls.

#### Dynamic tool discovery and policy

The runtime is the only component that connects to MCP servers, so it reports
its `tools/list` results in the first `RuntimeWorkerReady` message. Each entry
contains only `server_name`, `tool_name`, and the JSON argument schema. Policy
is never accepted from the runtime; the orchestrator remains authoritative.

`RuntimeWorkerReady.tool_discovery_status` makes the snapshot semantics
explicit:

- `TOOL_DISCOVERY_STATUS_COMPLETE` means `tools` is authoritative, including
  when it is empty.
- `TOOL_DISCOVERY_STATUS_FAILED` rejects the worker before admission. A failed
  discovery therefore cannot fall back to a permissive compatibility catalog.
- `TOOL_DISCOVERY_STATUS_UNSPECIFIED` is reserved for runtimes that predate
  discovery. An admitted legacy worker contributes the static v1 compatibility
  catalog while it remains connected.

The orchestrator validates a complete snapshot before admitting the worker,
rejecting blank identities, missing or invalid schemas, and duplicate
`(server_name, tool_name)` pairs. It reconciles the union of all active
workers' snapshots into the SQLite `tools` registry. Re-reporting updates the
schema and enabled state without overwriting an operator-set policy; tools no
longer present in the active union are disabled rather than deleted. On a
fresh database with no connected worker, `ListTools` returns an empty list.

Policy lookup always uses the exact `(server_name, tool_name)` pair. Known v1
read-only tools seed as `safe`, files mutations seed as
`approval_required`, and unknown tools seed as `approval_required` rather
than `safe`. `files.delete` and `files.move` remain permanently disabled.
Once discovery initializes the registry, absent or disabled tools are denied;
they cannot fall through to legacy defaults. Authorization is also scoped to
the authenticated worker connection, so one worker cannot borrow a capability
reported only by another worker even though `ListTools` exposes their union.

The separately owned runtime integration must send `COMPLETE` after every
successful discovery (including a successful empty result) and `FAILED` when
any required discovery attempt fails. Until that runtime change ships, the
current runtime is intentionally treated as legacy through `UNSPECIFIED`.

JWT signing requirements:

- Algorithm: **HS256** only. The header must be `{"alg":"HS256","typ":"JWT"}`.
- Secret: the same `TURING_APPROVAL_JWT_SECRET` that the Files MCP verifier
  is configured with. The MCP server has no way to discover any other secret.
- Claims:
  - `aud`: `"mcp-files"`.
  - `sub`: the agent identity (currently always `"general_assistant"`).
  - `tool`: the exact `name` from the upcoming JSON-RPC `tools/call`
    (e.g. `"files.create"`).
  - `args_hash`: `sha256:<hex>` of `canonicalJSON(arguments)` (see below).
  - `jti`: a unique identifier per approval. Must be passed as
    `ConsumeApproval.approval_id`.
  - `exp`: short enough to make replay risk negligible. A few minutes is
    appropriate; longer than that is a policy choice that the security
    review should be aware of.
  - `iat` and `iss` are accepted by the verifier but not validated.

Canonical-hash parity:

- The Go verifier's `canonicalJSON` is `encoding/json` output with
  `SetEscapeHTML(false)` and the trailing newline trimmed. Go's encoder
  sorts map keys.
- The Go signer must produce byte-identical canonical JSON. The fixture
  `{"B": 1, "a": 2}` must hash to
  `sha256:812e5e7fb7bb816dc477e91a136430192eadcf83ff303881298146e106ae0161`.
  Mismatches here will cause every approval to fail at the `args_hash`
  check, even though signatures, audience, subject, and tool are correct.

Consume method requirements:

- RPC: `ApprovalService.ConsumeApproval`.
- Request: `approval_id` is the JWT `jti`.
- Auth: gRPC metadata `authorization: Bearer ${TURING_INTERNAL_TOKEN}`. The
  service must stay on the internal gRPC port (not published to the host).
- Semantics:
  - First call for a given `jti` that corresponds to an approved request
    returns `APPROVAL_STATUS_CONSUMED`.
  - Subsequent calls for the same `jti`, or calls for a `jti` whose
    underlying approval was never granted, return gRPC `FailedPrecondition`.
    The MCP server maps that to `approval already consumed or not approved`
    and aborts the write.
  - Any other gRPC error is treated by the MCP server as a generic failure
    and aborts the write.

### v1.1 follow-ups intentionally deferred

- **Token-to-agent map.** Today, `mcp-files`'s `AgentFromBearer` returns
  the hard-coded string `"general_assistant"` for any holder of
  `MCP_FILES_TOKEN_GENERAL`. v1.1 should replace this with a real map so
  multiple agents can have distinct identities. Until that lands, the
  orchestrator must always set `sub: "general_assistant"` in approvals
  destined for `mcp-files`. There is an inline comment in
  `internal/auth/auth.go` to that effect.
- **`files.delete` and `files.move`.** Currently advertised but disabled.
  Re-enabling them is a code change plus a policy decision.

## Verifying locally

The two services have Go test suites that exercise the server entrypoints,
sandbox, read limits, approval verifier, and auth middleware:

```sh
cd turing-backend/mcp-system && go test ./... && go vet ./... && go build ./...
cd turing-backend/mcp-files  && go test ./... && go vet ./... && go build ./...
```

The full runtime path — runtime calling MCP, orchestrator signing approvals,
and the consume endpoint returning `200`/`409` — is covered by the local
Compose smoke path when Docker and Ollama are available:

```sh
cd turing-backend
./scripts/smoke.sh
```
