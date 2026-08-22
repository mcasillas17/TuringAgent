# MCP Security and Integration Guide

This guide describes the implemented security boundary for bundled and
registered MCP servers and their integration with the agent runtime and
orchestrator.

## Deployment boundary

| Tier | Example | Egress | Sandbox-confined | Approval enforcement |
|---|---|---|---|---|
| Bundled | `mcp-system`, `mcp-files` | No | Yes | Cooperating MCP server |
| Local container, third-party | `http://vendor-mcp:9000/mcp` | No | No | Orchestrator caller |
| Remote URL | `https://vendor.example/mcp` | Yes, at enable (discovery) and per run (calls) | No | Orchestrator caller |
| stdio / `command` / `npx` | `command: "npx"` | Refused | Refused | Not registered |

The two bundled servers remain on private Docker networks; Compose does not
publish their ports to the host. Third-party local containers join the
dedicated internal-only `net-mcp-registry` network (`172.31.254.0/24`) and
expose no host port; the caller rejects any local-tier resolution outside that
subnet. An empty
configured bundled bearer token denies every request rather than opening the
service.

Servers can be registered directly from the Flutter MCPs page — a name, an
explicit local-container or remote-URL tier, a hardened URL, and an optional
write-only bearer — with no file edit and no backend restart. In-app
registration runs the same validation an `mcp.json` import runs: name-pattern
and bundled-name refusal, stdio refusal, URL canonicalization/hardening, and
bearer-token normalization, then seals the token with the same
`internal/secretbox` sealer (the server name as AAD) under
`TURING_INTEGRATION_KEY`; no public or internal response contains a token or
ciphertext. Naming an existing non-bundled, url-empty legacy migration-0016
placeholder is the one existing name this call does not refuse — it is
treated as the operator's own consent to adopt that row in place (see below),
which lets a mobile operator who cannot edit `mcp.json` register a server the
backend already knows about by name. Any other existing name, or a bundled
name, is still refused. Every genuinely new registration still arrives
disabled, and registration itself never contacts the server's endpoint. The
`mcp.server.registered` audit record distinguishes which branch ran via an
`adopted: bool` field alongside the existing token-free name/tier/url keys —
computed inside the same transaction that decided which happened, so it can
never diverge from, or race, what was actually committed. The MCPs page
renders each server's canonical `url` (selectable, ellipsized, with a tooltip
for the full value) so an operator can verify the destination they
registered; an unadopted placeholder with no URL renders an explicit
"Endpoint not configured" warning instead of blank space.

`mcp.json` remains the bulk/config-file path. The orchestrator imports it at
startup and, without a restart, whenever an operator chooses Re-import
mcp.json on the MCPs page; both runs report imported, skipped, and refused
names with reasons. A `command` entry is refused as an import issue
explaining that stdio is unsupported, and no server row is created for it.
Malformed documents and entries whose token cannot be sealed are reported the
same way, without preventing the rest of the backend from starting. An
entry's `headers` object may carry at most one case-insensitive
`Authorization` key: a second one (for example both `Authorization` and
`authorization`, which decode into distinct map entries) is refused
deterministically with a fixed, generic reason, never a value that depends on
Go's randomized map iteration order; header names are sorted first, so an
unsupported header name, if one is also present, is always the
lexicographically first one reported and always takes precedence over the
duplicate-Authorization refusal, regardless of how many of either are
present. Each on-demand Reimport RPC's response is call-local and
deterministic: its `Refused` list is built from that call's own report,
sorted by name, rather than by re-reading the shared issues table two
overlapping reimports could otherwise race and swap into each other's
response. Skipped names in that response mean the row already had a real,
non-empty endpoint and mcp.json's current url/token/policy for it was
**not** applied — the MCPs page's reimport dialog states this explicitly per
skipped name ("already registered; existing settings were kept") so an edit
to an already-registered entry is never mistaken for having taken effect.

Reimport is create-only: an existing row for a name that already has a real,
non-empty endpoint is left completely untouched — its enabled state,
endpoint, tier, liveness, rotated token, tool snapshot, and policies never
change, and that entry's own `Tools` is not even inspected. The one exception
is a legacy migration-0016 placeholder: a non-bundled row seeded with
`url == ""` solely to carry a pre-registry tool policy forward until an
operator supplies a real endpoint. A file reimport or an explicit in-app
Register naming that exact server both adopt the row in place (same id) and
are fail-closed about it: url, sealed token, and tier update to the newly
supplied values; the row is forced disabled regardless of what it carried;
liveness resets to unknown/empty, because whatever status the placeholder's
`url == ""` row happened to carry says nothing about the endpoint now
replacing it; and every tool the placeholder carried is withdrawn
(present=0, enabled=0) before this call's own tools, if any, are considered.
A withdrawn tool is not gone for good — a valid static `tools` snapshot
supplied by that same reimport, or a later live discovery once the adopted
server is enabled, reconfirms any tool by matching name, and reconfirmation
preserves whatever policy an operator had already migrated/edited onto it
rather than resetting it to a default; only the tool's presence/enabled state
was ever touched by the withdrawal. Every genuinely new entry, whether from a
file import or in-app registration, still arrives disabled.

An mcp.json entry's optional static `tools` snapshot is fully validated
before the repository is ever touched — well-formed name/schema shape, no
bundled-namespace collision, and the entry's own configured bearer token
never appearing verbatim in a tool's name or serialized schema — and bounded
by the exact same tool-count and encoded-byte limits live `tools/list`
discovery enforces, counted the same way. It is then handed to the same
repository helper (`replaceServerToolsTx`) live discovery's `RecordDiscovery`
also uses, inside the very same transaction as the server row insert or
placeholder adoption: an inter-server tool-name collision there rolls back
that whole transaction too. Either way, an invalid, colliding, token-bearing,
or oversized snapshot refuses the whole entry with a fixed, generic reason
that never echoes the token, the offending name/schema, or which check
tripped, and leaves no partial row behind for a corrected reimport to get
stuck skipping.

Deleting a server writes a local import tombstone, so an unchanged
`mcp.json` cannot silently recreate it on the next reimport; the file path
keeps refusing a tombstoned name. An explicit in-app Register of that same
name is the user's own consent: it atomically clears the tombstone in the
same transaction and does not require a new name. Registering over a name
that still has a live row, or over a bundled name, is refused either way.

Token rotation is write-only for non-bundled servers: a new bearer replaces
the sealed value, an empty bearer clears it, and a bundled server refuses
rotation outright. A nonempty token still requires `TURING_INTEGRATION_KEY`
to seal. Because a prior Up/Down liveness observation was made under the
credential rotation is replacing or clearing, rotation atomically resets
liveness to unknown/empty in the same transaction as the sealed-token
update — a status-write failure rolls back the token change too, so a
rotated (or cleared) token can never be left paired with a stale liveness
reading taken under the credential it just replaced. No response, log line,
registry-change event, or audit row ever carries the plaintext token or its
ciphertext; audit rows record only the server name and whether a token is
now configured.

Enabling any non-bundled server — local-container or remote-URL — performs a
bounded `tools/list` liveness discovery as part of that call. For a
remote-URL server this is a real, explicit enable-time network request to
the configured endpoint, sending the configured bearer if one is set. That
is separate from per-run consent: invoking a remote tool during a run still
requires the caller to prepare, and the run to acknowledge, a signed
per-run egress decision naming the endpoint and the tool-argument and
tool-result categories, before any call is dispatched. Registration and
`mcp.json` import never contact an endpoint on their own while a server
stays disabled — only an explicit enable does, and only at that moment.

A successful discovery reconciles the server's live tools and schemas: tools
no longer reported are marked absent, newly reported tools are seeded through
`DefaultPolicyFor` (`approval_required` unless already known safe), and an
operator's edited policy on a tool that is still present is left untouched. A
failed discovery leaves the enabled state exactly as the operator set it,
marks the server down with a bounded, bearer-redacted status message, and
preserves whatever tool snapshot the last successful discovery produced.
Enable/disable, discovery outcome, registration (including whether it
adopted a placeholder), and token rotation are all audited; the audit
payload and any status text never carry a token.

Peer-controlled MCP errors and results are scrubbed of the registered bearer
before they can cross the internal RPC boundary or reach liveness state, tool
events, audit, or persisted result summaries.

All four bundled backend services run as explicit non-root users. Compose makes every
root filesystem read-only, drops all Linux capabilities without adding any
back, and sets `no-new-privileges`. Writable storage is allowlisted:

| Service | Runtime identity | Writable storage |
|---|---|---|
| `turing-orchestrator` | Validated host UID/GID | `data/` at `/app/data`; `skills/` at `/skills` |
| `turing-agent-runtime-general` | Image user `turing-agent-runtime` (UID/GID 1000) | None |
| `turing-mcp-system` | Image user `mcp-system` (UID/GID 1000) | None |
| `turing-mcp-files` | Validated host UID/GID | `sandbox/` at `/sandbox` |

No service receives a writable temporary filesystem. Each container replaces
Docker's default writable `/dev/shm` with a 64 KiB read-only, `nosuid`, `nodev`,
and `noexec` tmpfs. The static security guard
decodes the complete Compose service map, rejects unresolved `include` or
`extends` inheritance and unknown service/build keys, applies this mount
allowlist, and rejects root or missing users, writable roots, capability
additions, incomplete capability drops, missing `no-new-privileges`, device or
deploy-resource mappings, namespace sharing, supplementary groups,
image overrides or Dockerfile-declared volumes, host aliases, and unapproved
secrets. A Docker-gated companion check builds the four images and rejects
volumes inherited through their base-image metadata; run it with
`TURING_DOCKER_SECURITY_LIVE=1 go test -tags sqlite_fts5 ./turing-backend/tests -run TestBuiltBackendImagesDeclareNoWritableVolumes -count=1`.
The guard also
pins each service to its reviewed build context and Dockerfile, keeps both
networks project-scoped, and permits only one orchestrator public port on the
fixed loopback bind; MCP ports are exposed only inside their assigned network.
The orchestrator configures SQLite `temp_store=MEMORY`, so sorts and transient
b-trees do not require write access to `/tmp`; durable SQLite files and WAL
artifacts remain under `/app/data`.
The bundled servers bound request bodies and configure header, read, write, and idle
HTTP timeouts. `mcp-system` accepts at most 1 MiB per request; `mcp-files`
allows the worst-case escaped 512 KiB mutation envelope (about 3.1 MiB). Both
cap encoded responses at 1 MiB. Responses are ordinary bounded JSON responses,
not open-ended streams, so a finite write timeout is appropriate.

Both services accept JSON-RPC 2.0 `tools/list` and `tools/call` requests at
`/mcp`. Malformed tool arguments use `-32602`; operational tool failures use
`-32000`; an unknown JSON-RPC method uses `-32601`. Request IDs are echoed in
responses.

## Catalog discovery and filtering

`mcp-system` advertises four `safe` tools:

| Tool | Arguments | Result |
|---|---|---|
| `system.health` | none | `{ "ok": true, "service": "turing-mcp-system" }` |
| `system.time` | none | `{ "iso": string, "unixMs": number, "timezone": "UTC" }` |
| `system.echo` | optional `text: string`, at most 65,536 Unicode characters | `{ "text": string }` |
| `system.info` | none | `{ "os": string, "arch": string, "runtime": string }` |

`system.info` deliberately omits environment variables.

`mcp-files` advertises only the five callable tools:

| Tool | Policy |
|---|---|
| `files.list` | `safe` |
| `files.search` | `safe` |
| `files.read` | `safe` |
| `files.create` | `approval_required` |
| `files.update` | `approval_required` |

`files.delete` and `files.move` are not advertised. Their dispatcher cases
remain defensive and return `tool disabled` if called directly.

## Session provenance and withdrawal

Every `files.*` call carries a server-issued provenance capability in
`params._meta.provenanceToken`, outside model-controlled `arguments`. The
capability is short-lived and binds the agent, session, run, deletion
generation, tool, canonical argument hash, and logical path. `mcp-files`
verifies it before and after filesystem I/O. Approval-gated writes also carry
the existing single-use approval token.

Before a mutation reaches the filesystem, the internal approval-consumption
RPC reserves an orchestrator-owned `sandbox_artifacts` row. New files are
written beneath `sessions/<session>/runs/<run>/files/` and default to
`delete_on_session_delete`; a crash between reservation and finalization still
leaves an enumerable record. A post-write internal finalizer marks the manifest
ready. A session deletion that races this flow either rejects the write before
I/O or records it for cleanup and returns failure rather than success.

`retain_legacy_unowned` is the only retention exception: an existing
pre-provenance sandbox-root file that a session updates is recorded but never
deleted with that session. It is not session content and no new retained/shared
policy is implied. Cleanup uses the existing mcp-files listener's dedicated
`/internal/session-cleanup` route, authenticated by
`TURING_MCP_FILES_CLEANUP_TOKEN`; it is not advertised to models or clients
and accepts only a
strictly scoped session namespace request. Public audit records opaque artifact
identity, policy, state, and error class, never path or content.

Bundled discovery follows paginated `tools/list` responses in order, with limits of
100 pages, 10,000 tools, and 4 MiB of aggregate encoded descriptors. It
validates names, descriptions, object-rooted input schemas, and duplicate
names across servers. Catalog entries with policy `disabled` are filtered out
of both model definitions and runtime lookup. Policies `safe` and
`approval_required` remain callable; an unknown or non-string policy makes
discovery fail closed.

The 64 KiB `files.read` content cap and 65,536-character `system.echo` cap are
chosen so their worst-case `encoding/json` escaping fits the runtime client's
1 MiB per-response limit. Even with a maximum-length escaped file path, ten
worst-case results fit the runtime's 4 MiB aggregate tool-result budget.

## File request and result shapes

Paths are sandbox-relative and limited to 4,096 input bytes, 255 bytes per
component, and 64 components. Unix absolute paths, volume-qualified paths, and
any cleaned `..` component are rejected rather than rewritten. Unknown
arguments and wrong types are rejected rather than ignored. These are byte
limits; tool schemas document them rather than misrepresenting them with JSON
Schema `maxLength`, which counts characters. Required paths and search queries
advertise nonblank string constraints.

### `files.list`

- Arguments: optional `path` (defaults to `"."`) and optional integer `limit`
  (default 200, range 1–1000).
- Result:
  `{ "items": [{ "name": string, "isDir": bool }], "truncated": bool }`.
- Scanning is bounded to 4,000 directory entries. Internal staging names are
  omitted. Encoded collection data is capped at 384 KiB. `truncated` is true
  when the requested result limit, scan budget, or result budget prevents an
  exhaustive listing.

### `files.search`

- Arguments: optional `path` (defaults to `"."`), required non-empty `query`,
  and optional integer `limit` (default 50, range 1–200).
- A regular file may be supplied as the starting path.
- Result fields are:
  - `matches`: `{path, snippet}` entries;
  - `truncated`: a result or traversal budget stopped the search;
  - `incomplete`: one or more traversal/read/close failures occurred;
  - `errors`, `errorCount`, and `errorDetailsTruncated`;
  - `entriesVisited`, `directoryEntriesRead`, `directoriesScanned`,
    `filesScanned`, `skippedFiles`, and `bytesScanned`.
- Search budgets are 2,000 visited entries, 1,000 opened files, 8 MiB of
  aggregate reads, and 512 KiB per file. At most 20 error details are returned.
  Matches and error details share a 384 KiB encoded collection budget.
  Symlinks and non-UTF-8 files are skipped. Snippets contain the first match
  with up to 40 bytes of context on each side, adjusted to valid UTF-8
  boundaries.

### `files.read`

- Arguments: required non-empty `path` and optional integer `maxBytes`
  (default and maximum 64 KiB, range 1 byte–64 KiB).
- Result:
  `{ "path": string, "content": string, "truncated": bool, "bytesRead": number }`.
- Reads return at most `maxBytes`, so larger files remain inspectable through a
  bounded prefix with `truncated: true`. Returned content must be UTF-8 and is
  trimmed to a valid UTF-8 boundary. `bytesRead` reports the opened file's full
  length.

### `files.create` and `files.update`

- `create` arguments: required `path` and `content`.
- `update` arguments: required `path` and `content`, plus optional
  `expectedHash` matching exactly `sha256:[0-9a-f]{64}`.
- Content is limited to 512 KiB by bytes.
- Content schemas describe the byte limit without a misleading
  character-based `maxLength`.
- Result: `{ "path": string, "sha256": "sha256:<hex>" }`.
- `create` is exclusive and never overwrites an existing path.
- `update` rejects non-regular files and files with no write permission bit.
  When supplied, `expectedHash` validates the observed content before approval
  and again before replacement.

## Descriptor-relative confinement

The configured root is made absolute and canonicalized once when possible.
Every operation then normalizes the relative input and walks from an open root
directory descriptor:

1. Each directory component is opened with `openat`, `O_DIRECTORY`, and
   `O_NOFOLLOW`.
2. Leaf files are opened relative to the verified parent descriptor with
   `O_NOFOLLOW`.
3. Type and identity checks use `fstat`/`fstatat`; mutations use
   descriptor-relative `linkat`, `renameat`, and `unlinkat`.
4. Directory batches are read as names, then classified with
   descriptor-relative `fstatat(..., AT_SYMLINK_NOFOLLOW)`. This avoids
   `ReadDir`'s path-based unknown-type fallback and never follows a symlink.
5. Search performs a bounded descriptor-relative breadth-first traversal.

This avoids check/use races caused by repeatedly resolving absolute path
strings. A parent renamed concurrently remains the directory represented by
the already-open descriptor; replacing it with a symlink does not redirect
the operation outside the sandbox. Reserved, case-folded
`.turing-create-*` and `.turing-update-*` path components are rejected so
callers cannot address staging files.

Long walks check request cancellation between descriptor opens, directory
creation, and synchronization work. Creation synchronizes the containing
directory and each required ancestor before success; ancestor descriptors are
reopened and closed one at a time rather than retained for the full path depth.

The sandbox restricts path reachability; it is not a substitute for operating
system permissions, container isolation, or careful mount selection.

## Approval ordering

The runtime and file server both enforce ordering:

1. The runtime posts a BEFORE tool-call beacon to the orchestrator.
2. A deny stops immediately. For `APPROVAL_REQUIRED`, the runtime waits for an
   approved token; it does not call MCP while approval is pending.
3. The runtime calls MCP with the JWT in `params._meta.approvalToken`, outside
   `arguments`, so the token is not included in the argument hash.
4. `mcp-files` validates arguments and performs only non-mutating precondition
   checks (for example, existence/type/permission and optional current-hash
   checks) before approval consumption.
5. The server requires `TURING_APPROVAL_JWT_SECRET` at startup. It verifies the
   HS256 signature, `typ == "JWT"`, `iss == "turing.orchestrator"`, and expiry
   (`exp <= now` is expired), then binds `aud` to `mcp-files`, `sub` to the
   bearer-derived agent, `tool` to the requested tool, and `args_hash` to
   canonical JSON of the exact arguments.
6. It synchronously consumes the JWT `jti` through the orchestrator's
   `ApprovalService.ConsumeApproval`. Only `APPROVAL_STATUS_CONSUMED`
   succeeds; replay/not-approved maps to `FailedPrecondition`.
7. Only after successful consumption can file content or namespace state be
   mutated. A later write failure does not restore the single-use approval.
8. The runtime posts an AFTER beacon with the result or failure.

### Caller-side enforcement for non-bundled servers

A third-party server does not know Turing's approval JWT format and never
receives `TURING_APPROVAL_CONSUMER_TOKEN`. Giving it that identity would let it
consume approvals for calls it did not make.

For local-container and remote tiers, the runtime sends the approved
`approval_id` back to the orchestrator over its existing least-privilege
internal connection. The orchestrator verifies the run, server, tool and
canonical argument hash, atomically consumes the approval, and only then sends
JSON-RPC to the registered endpoint. The signed approval JWT is not forwarded
on this path.

This preserves argument binding and single use, but the enforcement point is
different and the guarantee is narrower. Bundled `mcp-files` rejects a forged
direct request itself. A third-party server would not; the guarantee holds
because its registered endpoint and sealed bearer are usable only through the
orchestrator proxy. A process that can reach or authenticate to that server by
some other route is outside this guarantee.

Immediately before HTTP dispatch, the proxy rechecks that the run is still
execution-active, the session is not being withdrawn, the server and tool are
still present/enabled, and (for remote tiers) the endpoint and tool remain in
the run-owned egress decision. That final check is the dispatch linearization
point. A cancellation committed after it observes an already in-flight call;
as with a bundled write after approval consumption, it does not retroactively
restore the consumed approval.

Human approval comments and denial reasons are durable decision evidence. The
orchestrator stores them in separate nullable columns in the same transaction as
the decision. The existing proto3 scalar fields do not expose presence, so an
omitted human field and an explicitly empty one both persist as `""`; `NULL`
means that a non-human path, such as an unattended approval or expiration, never
carried that field. Non-empty rationale must be valid UTF-8 and is limited to
4096 bytes.

Human decision audit payloads use an allowlist: `toolName` plus the matching
`comment` or `reason`. The audit copy is truncated on a UTF-8 boundary to 512
bytes and gets a `commentTruncated` or `reasonTruncated` marker when shortened.
Approval tokens, JTIs, argument hashes, and tool arguments are never copied into
this payload. Deleting the session cascades the approval row and replaces
correlated audit payloads, including rationale, with `{"scrubbed":true}`.

The canonical argument digest is `sha256:<hex>` over Go `encoding/json`
output with HTML escaping disabled and the trailing newline removed. Map keys
are deterministically sorted by the encoder.

The current file bearer maps to `general_assistant`; approval JWTs must use
that subject. Approval consumption uses the internal gRPC bearer and has its
own 10-second timeout. The default approval lifetime is 65 seconds, the runtime
wait bound is 71 seconds, an individual MCP request is bounded to 30 seconds,
and the whole tool lifecycle retains a 180-second timeout.

## Atomic creation and replacement

Both mutation paths stage content in a randomly named file in the destination
directory, write it completely, `fsync` it, publish it atomically, and
`fsync` the directory. `create` publishes with `linkat` so an existing target
cannot be overwritten. `update` revalidates the original device/inode and,
when requested, its hash before publishing with `renameat`.

`update` preserves only the target's ordinary `0777` permission bits. It
deliberately strips setuid, setgid, and sticky bits from the replacement.
Atomic replacement creates a new inode whose ownership follows the MCP
process and parent-directory creation rules. It therefore does not preserve
the old inode number, old owner/group, old POSIX ACL, extended attributes,
file flags, hard-link identity, or other inode metadata. Attempting to copy
these attributes is nonportable and can reintroduce privilege or race hazards;
callers that require such metadata must manage it outside this
content-oriented tool.

The process-wide normalized path lock serializes `read`, `create`, and `update`
operations made by cooperating `mcp-files` callers in that server process.
Accordingly, `expectedHash` acts as compare-and-replace protection among those
cooperating calls. It is not globally atomic against a host process that writes
the sandbox directly: such a writer does not take the lock and can modify the
target after the final hash/identity validation but before `renameat`. Callers
must not treat `expectedHash` as coordination with non-cooperating host
writers.

## Bind-mount identities and host security systems

Every backend image defines a non-root user. The standalone orchestrator,
agent-runtime, `mcp-system`, and `mcp-files` identities use UID/GID 1000.
Repository Compose overrides the orchestrator and `mcp-files` bind-mount writers
with the validated current host UID/GID so `data/`, `skills/`, and `sandbox/`
remain writable without broadening their permissions. The runtime and
`mcp-system` retain their fixed image identities because they have no writable
mount.

`scripts/init.sh` accepts only canonical positive UID/GID values for the
current process and rejects root, invalid, or out-of-range identities before it
mutates the sandbox or `.env`. It always rewrites `HOST_UID` and `HOST_GID` to
the current host values; manual or stale values are not supported. It rejects a
pre-existing sandbox or skills symlink, creates real mode-`0700` sandbox,
skills, and data directories independently of the caller's umask, and checks
that the sandbox root and existing nested
directories/files are owned, accessible, and not group/world-writable. Symlink
entries are not followed. Data and configured SQLite files must have safe types
and ownership; owned database files are restricted to mode `0600`. A
pre-existing `.env` must be a regular non-symlink file before the script changes
its mode or content. The script reports unsafe legacy content and exits; it
never recursively runs `chmod` or `chown`.

All repository launch scripts delegate to `scripts/compose.sh`. That wrapper
revalidates the current non-root identity and supplies it directly to Compose,
overriding both exported and `.env` `HOST_UID`/`HOST_GID` values. Direct
`docker compose` invocation is unsupported because shell variables take
precedence over `.env` and can bypass that preflight. Immediately before a
launch, the wrapper also verifies that `sandbox/`, `skills/`, and `data/` are
real, owned, restrictive directories and that SQLite files are regular, owned,
mode-`0600` files.

Rootless Docker and daemon `userns-remap` translate container IDs through
daemon-specific subordinate-ID mappings. A host UID copied into Compose is
therefore not guaranteed to map back to that same host account, and bind-mount
writes may fail. Configure the daemon/user namespace and mount ownership
together, or use a pre-provisioned named volume; do not solve mapping failures
by running the service as root or making the sandbox world-writable.

On SELinux-enforcing hosts, DAC ownership can be correct while the bind mount
is still denied by its label. The shared Compose file intentionally does not
append `:Z`, because private relabeling is platform- and deployment-specific
and can be harmful for shared host paths. Apply an appropriate persistent
label, or use a local Compose override with `:Z` for a private mount or `:z`
for a deliberately shared mount. Never relabel an unrelated or sensitive host
directory merely to make it available to the agent.

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
normalizes its relative input, rejects absolute/traversing/reserved paths, and
walks from an open root descriptor without following symlinks.

### Sandbox root

The sandbox root is configured by `FILES_SANDBOX_ROOT` (default `/sandbox`).
The standalone image runs as its fixed non-root UID/GID 1000. For the local
Compose workflow, `scripts/init.sh` records the host account as `HOST_UID` and
`HOST_GID`, and `scripts/compose.sh` requires and reinjects that current
identity so the process can write to the host-owned `sandbox/` bind mount.
Unset or stale identity values are not accepted; the sandbox is not made
world-writable.
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

### Per-call descriptor walk

For a caller-supplied `path`:

1. Reject an absolute/volume-qualified path, `..`, empty required path, reserved
   staging component, or a byte/component/depth overflow.
2. Open the configured root as a directory descriptor.
3. Open each directory component relative to the preceding descriptor with
   `O_DIRECTORY|O_NOFOLLOW`.
4. Open or inspect the leaf relative to the verified parent with `O_NOFOLLOW`
   and descriptor-relative type/identity checks.
5. Publish and clean up through `linkat`, `renameat`, and `unlinkat`, never by
   reconstructing a trusted absolute leaf path.

Two consequences:

- Traversal (`"../../etc/passwd"`) is rejected during normalization, while a
  symlinked parent or leaf fails the no-follow descriptor open.
- `files.create` may create missing parent directories, reopening and
  synchronizing one verified descriptor at a time.

### Search and symlinks

`files.search` uses a bounded descriptor-relative breadth-first walk. Directory
entry names are classified with `fstatat(..., AT_SYMLINK_NOFOLLOW)`, including
when `ReadDir` reports `DT_UNKNOWN`; symlinks are skipped and never opened.
Regular files are opened relative to their verified parent and bounded by the
per-file and aggregate read budgets.

The result: symlink-bearing entries cannot produce search hits, and search
cannot leak content from outside the sandbox even if a symlink was somehow
walked. `TestSearchRejectsSymlinkEscape` exercises this path.

## Read limits and content rules

`files.read` enforces three layers of size and content discipline:

- **Default cap:** `defaultReadBytes = 64 * 1024` (64 KiB).
- **Maximum honored cap:** 64 KiB. Larger regular files return a bounded prefix
  and `truncated: true`; `bytesRead` still reports the full opened-file size.
- **UTF-8 enforcement:** the returned prefix must be valid UTF-8. Binary data
  is rejected rather than exposed to the model.
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
in `internal/approval/jwt.go` and is invoked before any mutation. Non-mutating
precondition checks may run first; **none of the verification steps fails open**.

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
`exp <= now`, the verifier returns `token expired`. There is no skew
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
- Metadata: `authorization: Bearer ${TURING_APPROVAL_CONSUMER_TOKEN}`.
- Response handling:
  - `APPROVAL_STATUS_CONSUMED` — the JWT was unused; the verifier returns
    success and the write proceeds.
  - `FailedPrecondition` — the JWT was already used, or the underlying
    approval record was never approved. The verifier returns
    `approval already consumed or not approved` and the write is aborted.
  - Any other gRPC error — generic failure: `approval consume failed: <error>`.
    The write is aborted.

The write happens only after consume returns `consumed` and its reservation
identifies the server-derived artifact path. mcp-files finalizes that reservation
after I/O and rechecks the session capability. In other words, a
successful consume is the act of marking the JWT used, and any failure path
after that point (write error, etc.) does not roll consume back. This is
intentional: a partially completed write is still a "used" approval.

The internal gRPC server authorizes each caller by which of two registered
tokens its bearer matches, not by anything the caller claims about itself.
`TURING_APPROVAL_CONSUMER_TOKEN` (held only by the bundled `mcp-files`
consumer) is authorized for
`ApprovalService.ConsumeApproval`, `FinalizeSandboxArtifact`, and
`CheckSessionCapability`. `TURING_RUNTIME_TOKEN` (held by
`agent-runtime-go`) is authorized for that method plus
`ApprovalService.GetApprovalForRuntime`, `RuntimeService.ConnectWorker`, and
`SessionService.ListMessages`/`SearchMessages`. The two tokens must differ —
the orchestrator refuses to start otherwise — so a compromised `mcp-files`
cannot present the runtime's token to claim a job or read conversation
history, and a compromised runtime cannot pose as a different service's
approval consumer.

`TestValidateRejectsConsumeReplayConflict` asserts the `FailedPrecondition`
path.

### Failure-mode ordering

The verification pipeline is, in order:

1. Wrong segment count or non-base64url segments — `invalid token`.
2. Wrong `alg` — `invalid token algorithm`.
3. Invalid signature — `invalid signature`.
4. Missing or unexpected `iss` — `invalid token issuer`.
5. Expired (`exp <= now`) — `token expired`.
6. `aud != "mcp-files"` — `invalid approval audience`.
7. `sub != agentID` — `approval subject does not match agent`.
8. `tool != params.name` — `approval tool does not match call`.
9. `args_hash` mismatch — `approval args_hash does not match call`.
10. Consume returns gRPC `FailedPrecondition` — `approval already consumed or not approved`.
11. Consume returns any other gRPC error — `approval consume failed: <error>`.

None of these branches fall through to the I/O path.

## Failure modes and security rationale

A concise mapping from rejection to threat:

- **Path traversal (`..` or absolute paths).** Input normalization rejects the
  request before descriptor traversal begins.
- **Symlink confused deputy.** Directory and leaf opens use no-follow,
  descriptor-relative operations; search classifies entries without following.
- **Oversized read DoS.** Reads return at most a 64 KiB prefix; search also has
  per-file, aggregate-byte, entry, and file-count budgets.
- **Binary / non-UTF-8 leakage to the model.** The bounded returned prefix must
  be valid UTF-8. Truncation trims trailing partial code points.
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

Flutter consumes four public event types:
`tool.call.started`, `tool.call.completed`, `tool.call.failed`, and
`tool.call.denied`. Their payload contract uses camelCase keys and contains
`toolCallId`, `toolName`, optional `serverName`, and `error` only on failed or
denied events. Provider IDs, arguments, status, duration, and result summaries
remain in persisted tool/audit state rather than this UI payload.

### Agent runtime

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
- Lists enabled non-bundled servers over the internal registry RPC and invokes
  them through `CallRegisteredMcpTool`; it never receives their endpoint bearer.
- Refreshes and re-reports its capability snapshot when server enablement,
  discovery or tool policy changes.

### Orchestrator

The orchestrator implements the signing side and the gRPC consume method that
the Files MCP verifier calls.

#### Dynamic tool discovery, registry and policy

The runtime connects directly only to bundled MCP servers. The orchestrator
discovers local third-party servers and proxies all non-bundled calls. The
runtime reports the combined snapshot in `RuntimeWorkerReady`; each entry
contains only `server_name`, `tool_name`, and the JSON argument schema. Policy
is never accepted from the runtime; the orchestrator remains authoritative.

`RuntimeWorkerReady.tool_discovery_status` makes the snapshot semantics
explicit:

- `TOOL_DISCOVERY_STATUS_COMPLETE` means `tools` is authoritative, including
  when it is empty.
- `TOOL_DISCOVERY_STATUS_FAILED` rejects the worker before admission. A failed
  discovery therefore cannot fall back to a permissive compatibility catalog.
- `TOOL_DISCOVERY_STATUS_UNSPECIFIED` is reserved for runtimes that predate
  discovery. An admitted legacy worker's reported ready-message tools are authoritative
  when present; only a ready message with no tools uses the explicit capability profile's
  fallback list, and an empty fallback means no tools. A modern capability registration
  without a discovery callback reports `COMPLETE` with an authoritative empty set rather
  than selecting legacy behavior.

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

The shipped runtime sends `COMPLETE` after every successful discovery,
including a successful empty result, and `FAILED` when a required discovery
attempt fails.

For a run using a remote model or remote MCP server, the orchestrator freezes
the selected `server/tool` names and every remote MCP endpoint into the one-time
egress decision and job. The runtime filters its local registry to that exact
set before serializing tool schemas; a missing selected tool fails the run
rather than widening or substituting the set. Remote MCP tool arguments and
results are disclosed categories. The proxy refuses a remote call whose server,
endpoint and tool are absent from the run-owned decision. Egress controls do
not replace tool policy or approval.

JWT signing requirements:

- Algorithm: **HS256** only. The header must be `{"alg":"HS256","typ":"JWT"}`.
- Secret: the same `TURING_APPROVAL_JWT_SECRET` that the Files MCP verifier
  is configured with. The MCP server has no way to discover any other secret.
- Claims:
  - `aud`: `"mcp-files"`.
  - `sub`: the agent identity (currently always `"general_assistant"`).
  - `tool`: the exact `name` from the JSON-RPC `tools/call`
    (e.g. `"files.create"`).
  - `args_hash`: `sha256:<hex>` of `canonicalJSON(arguments)` (see below).
  - `jti`: a unique identifier per approval. Must be passed as
    `ConsumeApproval.approval_id`.
  - `exp`: the configured approval expiry (65 seconds by default).
  - `iss`: exactly `"turing.orchestrator"`; the verifier rejects any other
    value. `iat` is included for audit context but is not a separate gate.

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
- Auth: gRPC metadata `authorization: Bearer ${TURING_APPROVAL_CONSUMER_TOKEN}`. The
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
- **`files.delete` and `files.move`.** They remain permanently disabled and
  are not advertised. Re-enabling them is a code change plus a policy decision.

The repository-root `.dockerignore` excludes Git/agent state, `.env` and key
material, runtime data, sandbox contents, dependency caches, and generated
build/test output from root-context backend image builds. Required Go modules
and generated protobuf sources remain in the context. Because Compose builds
`mcp-system` from its module subdirectory, that module has its own
`.dockerignore` with equivalent credential and build-artifact exclusions.

## Local verification

The two services have Go test suites that exercise the server entrypoints,
sandbox, read limits, approval verifier, and auth middleware:

```sh
go test -tags sqlite_fts5 ./.github/workflows -count=1
go test -tags sqlite_fts5 ./turing-backend/scripts -count=1
(cd turing-backend/mcp-system && go test -race ./... -count=1 && go vet ./... && go build ./...)
(cd turing-backend/mcp-files && go test -race ./... -count=1 && go vet ./... && go build ./cmd/server)
golangci-lint run --config .golangci.yml --build-tags sqlite_fts5 ./... ./.github/workflows
(cd turing-backend/mcp-files && golangci-lint run --config ../../.golangci.yml ./...)
(cd turing-backend/mcp-system && golangci-lint run --config ../../.golangci.yml ./...)
```

The Docker smoke path is optional and requires Docker plus its configured
model endpoint:

```sh
cd turing-backend
./scripts/smoke.sh
```
