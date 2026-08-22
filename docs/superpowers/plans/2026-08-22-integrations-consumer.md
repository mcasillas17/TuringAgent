# Integrations Consumer Implementation Plan

Give the model tools that act on connected accounts. Integrations today is a
vault with no customers: users connect IMAP, CalDAV, Notion, and GitHub
accounts, the credential is AES-256-GCM-sealed under `TURING_INTEGRATION_KEY`,
consent is recorded, revocation works — and no code path ever *opens* a
credential to do anything. The consent screen tells the user what the
credential grants; nothing exercises a single grant. `docs/VISION.md` even
leans on this: deferred-list item 6 currently argues the feature is safe
*because* "no tool consumes a connection" and "nothing dials anywhere." This
plan makes that sentence false on purpose, and rewrites it.

The interesting part is not the tools. It is that an integration call is the
first tool whose *normal, read-only operation* is egress, and whose results
are someone else's words entering the model's context. This plan says which
existing gate covers each of those, and adds no new gate.

## The problem, stated precisely

`schema/0008_integrations.sql` stores sealed credentials.
`internal/repository/integrations.go` can create, list, revoke, and delete a
connection — and deliberately exposes at most five header bytes of the
ciphertext on any read path (`credentialHeaderBytes`), with the `Connection`
struct's doc comment promising the sealed secret is read by nothing in the
file. The sealing service (`internal/service/integrations`) already exists;
it seals and never opens.

The moment something opens, three things are at stake:

1. **"Nothing leaves the machine by default."** Calling `api.github.com`
   sends model-authored arguments off the machine. Every integration call —
   reads included — is egress in exactly the sense TUR-003 regulates.
2. **"Every mutation is approved, argument-bound, and single-use."** Creating
   an issue comment is a mutation at a provider that cannot verify our
   approval token. This is the third-party-MCP problem again, and it has the
   same honest answer.
3. **Retrieved content is untrusted input.** An issue body is written by an
   arbitrary third party and lands in the model's context. VISION already
   names this for skill text ("Skill text is untrusted input, not
   authority"); it gains the sibling sentence for retrieved integration
   content.

## Design decisions (locked)

**The consumer lives in the orchestrator. Not an MCP container, not the
runtime.** Precisely why: `TURING_INTEGRATION_KEY` is scoped to the
orchestrator alone in compose, and stays that way — the runtime never holds
credentials. (The network claim, stated honestly: only `net-mcp-registry` is
`internal: true`; the bundled servers sit on ordinary bridge networks and
keep no egress as a property of their code and of never being published. The
credential-scoping argument is the load-bearing one.) The orchestrator
already dispatches third-party calls on the runtime's behalf
(`CallRegisteredMcpTool`, PR #73); integration tools take the same shape over
a new `CallIntegrationTool` RPC. This *extends* the existing
`internal/service/integrations` package — it does not create it.

**The service surface splits, and the identities are wired explicitly —
this is where a correct-looking implementation dies with
`PermissionDenied`.** `IntegrationService` is registered public-only today,
under a comment ("nothing internal reads a connection") that this plan
falsifies and must rewrite. It adopts the `PublicServer`/`InternalServer`
split `mcpregistry` already models: `CallIntegrationTool` and
`ListIntegrationTools` exist only on the internal facet — the client cannot
reach them — and the management RPCs (`ConnectAccount`, `ListConnections`,
`DeleteConnection`, …) are refused on the internal facet, so the runtime
cannot reach credentials management. The `runtime` service identity in
`internal/app/app.go` gains the two new full-method names, exactly as it
carries `CallRegisteredMcpTool` today; without that line every integration
call is `PermissionDenied` and the feature does not run at all. Connection-
change notification is wired the way `mcpregistry`'s is: the integrations
service gets a registry-change notifier hook in `app.go`, pointing at the
runtime service.

**The plaintext credential exists in one stack frame, and the repository
changes to allow exactly that.** A single new repository method returns the
sealed blob *and* the connection status in one statement, so a revoked row
can never be read as live. It is the only method that touches full
ciphertext; the `Connection` doc comment is rewritten to name it rather than
quietly falsified. The service unseals per call, inside the provider-client
call; no provider client, cache, or struct field holds the plaintext between
calls. The opener sits behind a small interface in the service (today it is
a concrete `*secretbox.Sealer` field) so the once-per-call property is
testable with a counting fake. The token travels in the `Authorization`
header only — never in a URL, because Go transport errors (`*url.Error`)
embed the full URL in error strings, which would put a query-string token
into logs on a mere dial failure.

**The provider client reuses the transport hardening that already exists —
without inventing a third mechanism.** `turing-backend/internal/egress`
already has `NoRedirectClient` and `RedactRedirectError`, used by both LLM
provider clients; the GitHub client uses those, and `RedactRedirectError` is
directly load-bearing for the credential-hygiene test (a raw redirect error
carries the full URL). Guarded address resolution currently lives in
`mcpregistry/transport.go` (`resolvePublicMCPAddress` — refusing loopback,
private, link-local, multicast and unspecified addresses, requiring global
unicast, and screening the special-use networks list; the shared version
keeps all of it); it moves to (or is wrapped from) the shared egress
package so both callers use one implementation. Phase one pins the GitHub host as a constant
(`https://api.github.com`); GitHub Enterprise — whose whole point is a
different host — is explicitly out of scope, as is every use of the
`endpoint` column. Two latent holes are named now for the deferred
endpoint-bearing providers: `isPlausibleHost` accepts loopback and
link-local values, and `ParseKeyedEndpoint` permits `http://` for loopback
hosts — the future rule is "keyed HTTPS endpoint + the shared public-address
class check", and nothing dials a stored endpoint until that lands.

**`integrations` is a pseudo-server like `skills`, and every mechanism that
special-cases `skills` is updated in the same commit.** Tools carry
`server_name = 'integrations'`, `mcp_server_id IS NULL`. The audited list —
each verified against the code, and the first one decides whether the
feature exists at all:

- `service/runtime/service.go` `filterRegisteredWorkerTools`, which passes
  `"skills"` through and asks `MCPToolAvailable` (an `mcp_servers` join)
  about everything else — without an `integrations` case, reported
  integration tools are dropped before registration *and* pruned from the
  worker's capabilities, so they never reach the `tools` table, never appear
  in `EgressToolNames`, and every beacon dies as `unknown_tool`;
- the 0016 triggers — `0017` drops and recreates both (SQLite has no `ALTER
  TRIGGER`) widening the carve-out to `('skills','integrations')`;
- `repository/tools.go` `UpsertTools`, which branches on the literal
  `"skills"` — without a matching branch, integration tools are *silently
  never registered* (the non-skills path inserts via a `SELECT` from
  `mcp_servers` that yields zero rows and continues without error);
- `service/tools/defaults.go` `BundledServerForTool`, which gains a
  `github.` prefix case returning `integrations` — this is also what stops a
  registered third-party MCP server from exporting a colliding `github.*`
  tool and taking down all tool discovery at `BuildToolRegistry`'s duplicate
  check;
- `mcpregistry/import.go`'s reserved-name check (`system`/`files`/`skills`),
  which gains `integrations` — otherwise an `mcp.json` server named
  `integrations` imports cleanly and then the runtime's shadow check fails
  discovery permanently;
- `runtime/service.go` `beaconServerName`, whose dot-prefix fallback would
  resolve a server-nameless `github.list_issues` beacon to server `"github"`
  — the runtime always stamps `integrations` on these beacons, and the
  fallback learns the same mapping defensively.

Tool names stay `github.*`: shadow-protection and beacon routing are fixed at
the mechanisms above, not by contorting the names.

**The runtime's lister is dynamic and orchestrator-sourced — and it is how
the model learns its connections.** Not a static lister like
`newSkillToolLister()`: a static lister advertises the tools with zero
connections and can never carry connection identity. The runtime calls a new
`ListIntegrationTools` RPC at discovery (the same pattern as
`mcp.NewRegistryClients`), which returns tool definitions only while at
least one live connection for the provider exists — and whose tool
*descriptions* enumerate the current live connections as `(connection_id,
display name)` pairs. That is the answer to "how does the model obtain a
valid `connection_id`": it reads it from the tool description, which is
refreshed — along with advertisement itself — by firing the existing
registry-changed notification (`NotifyMCPRegistryChanged` → worker
re-discovery) whenever a connection is created, revoked, or deleted. Without
that wiring the tool set and the id list are stale until restart.

**Per-tool policy editing must actually work, which today it does not for
any pseudo-server.** `UpdateMcpToolPolicy` and the Flutter editor are keyed
on `mcp_servers.id` end to end; `skills_list`/`skill_view` are uneditable
today for exactly this reason. This plan adds a server-name-keyed policy RPC
(`UpdateToolPolicyByName(server_name, tool_name, policy)`) plus repository
method and client wiring, used by the Integrations page — and incidentally
making skills tools editable. It lives on `McpRegistryService`'s public
facet, beside `UpdateMcpToolPolicy` — that is where the guard and the
notification already live, and where the client's API key can reach — and
is refused on the internal facet like the other management RPCs. Four
properties the RPC must carry, each a trap if dropped:

- **the same bundled-mutating-tool guard** as `UpdateMcpToolPolicy`
  (`BundledToolRequiresApproval`) — a by-name RPC without it is a privilege
  escalation that sets `files.create` to `safe`;
- **integration mutating tools are un-`safe`-able, and the bundled guard
  does not cover them** — `BundledToolRequiresApproval` reads the seed map,
  and this plan deliberately seeds nothing for `integrations`, so the guard
  is vacuously false for `github.create_comment`. Setting the one write
  tool to `safe` would skip the legible approval render, void the
  `ArgsHash` connection binding, and — because a `safe` decision is not
  side-effecting to the runner — leave a failed POST retryable, exactly the
  double-comment hazard the side-effect machinery exists to prevent. The
  integrations service's per-tool `read_only` table is the source of truth:
  `!read_only ⇒ refuse safe`, and the refusal is tested;
- **the same `notifyRegistryChanged()` call** on success — dispatch reads
  the `tools` table so tests of dispatch alone cannot catch its absence,
  but without it a tool the user just disabled stays in the worker's
  capabilities and keeps appearing in every egress disclosure until a
  restart;
- **a pseudo-server-aware `enabled` derivation** in the repository — the
  existing `SetMCPToolPolicy` SQL derives `enabled` from an `EXISTS` against
  `mcp_servers`, which is false for `mcp_server_id IS NULL` and would
  disable the very tool being edited; NULL-server rows are enabled the way
  the skills insert already enables them.

**Everything defaults to `approval_required`, reads included, and the policy
comes from the `tools` table.** `DefaultPolicyFor` seeds nothing for
`integrations`; unknown tools already fall to `approval_required`, and no
new defaulting logic is added. The dispatch path reads the stored policy —
`safe` genuinely skips the approval gate, `disabled` genuinely refuses — and
**an `approval_required` call waits for and consumes an approval whether it
is a read or a write**; consumption is a property of the policy, not of
mutation.

**A failed read is a tool error, not a dead run — via an explicit
`read_only` bit authored by the orchestrator.** Today the runner derives
`sideEffecting` solely from `DECISION_APPROVAL_REQUIRED`, and that one bit
drives approval-waiting *and* every downstream side-effect consequence.
Those meanings are decoupled: `ToolPolicyDecision` gains a `read_only`
field, set by the orchestrator from the integrations service's own per-tool
table (the three GET tools), and when it is true the runner suppresses the
side-effect consequences at **all three** of their sites while
approval-waiting is unchanged: a failed call is a recoverable tool error,
not `SideEffectUnknownError`; a *successful* call whose after-report to the
orchestrator fails is likewise recoverable, not `SideEffectCommittedError`
(the GET's result is already in hand — halting the run there would
reintroduce the bug for the other branch); and `RunOutcome.SideEffecting`
stays false, so run-level retryability is unaffected by reads. Named
consequence, intended: a run whose last tool call was a `read_only` GET
stays retryable and may re-issue the GET. Write tools keep the existing
behavior at every site; "we don't know whether the comment posted" *should*
halt the run.

**Every integration call joins the per-run egress decision — through the
signed challenge, not just the database row.** PR #73's pattern, all of it:

- The decision gains `integration_endpoints_json`: an array of
  `{endpoint, connection_id, display_name, tools}` where `endpoint` is a
  canonical keyed endpoint. Canonicality is specified because the signed
  challenge requires byte-identical re-marshalling
  (`parseEgressChallenge` re-marshals and `bytes.Equal`-compares): entries
  sorted by `(endpoint, connection_id)`, deduplicated, at most
  `maxIntegrationEndpoints = 16` entries, each entry's serialized form at
  most `maxIntegrationEndpointEntryBytes = 1 KiB` — mirroring the
  sortedness and strict-ordering rules `validChallengePayload` already
  enforces for `SelectedTools` and `RemoteMCPServers`. Two mechanical
  consequences are named so nobody discovers them in a debugger: the inner
  `tools` slice is always marshalled non-nil and sorted (nil vs. `[]` is a
  byte difference that silently voids valid challenges), and the entry
  struct is not `comparable` (it holds a slice), so
  `payloadMatchesEgressContext` compares these with `slices.EqualFunc`,
  not `slices.Equal`. Exceeding the entry cap is refused at
  `resolveEgressContext` with a legible `FailedPrecondition` naming the
  remedy (revoke connections or disable tools) — the alternative is an
  opaque `Internal` on **every send on every provider**, and unlike
  `maxEgressTools`, sixteen connections is a reachable number once the
  deferred providers land.
- Those entries ride inside the HMAC-signed `egressChallengePayload`, are
  covered by the structural validation and by
  `payloadMatchesEgressContext`, and are frozen at enqueue — so the list the
  user acknowledged is the list the signature binds, and a connection set
  that changes between prepare and send is detected, not absorbed.
- The disclosure carries the connection's **display name**, and the egress
  dialog renders one line per connection — `conn_9f3a…` is not consent to an
  account, and consent for the personal account must not read as consent for
  the work account.
- Validation before dispatch checks four legs:
  `integrations/<tool>` present in the decision's `selected_tools`;
  `TOOL_ARGUMENTS` and `TOOL_RESULTS` present in the data categories; the
  `(endpoint, connection_id)` *pair* present in the decision's integration
  endpoints — two GitHub connections share `api.github.com`, and consent
  for connection A must not authorize connection B; and the called tool
  present in that entry's `tools` list, so the array is load-bearing, not
  decorative data inside a signed payload.
- Revalidation happens again after the approval wait, and a liveness check
  equivalent to `MCPDispatchActive` (run executing, not cancelled, session
  not mid-deletion) runs immediately before the network call — integration
  tools bypass the `mcp_servers` join that query uses, so they need their
  own (`IntegrationDispatchActive`), along with an
  `IntegrationEndpointsForTools` resolver analogous to
  `RemoteMCPServersForTools`.
- The "local provider needs no decision" rule is mirrored in Go in four
  places, and every one widens from "has remote MCP servers" to "has remote
  MCP servers or integration endpoints": `normalizePendingEgressDecision`
  (`repository/egress.go`), the local-decision refusal in
  `repository/jobs.go`, the early return in `resolveEgressContext`
  (`service/chat/egress.go` — miss this one and a local-Ollama run never
  gets a consent dialog and the feature silently does not exist for local
  models), and the challenge-payload validation in the same file. **A fifth
  edit in `resolveEgressContext` is separate from the early return:** the
  data-category attachment currently adds `TOOL_ARGUMENTS`/`TOOL_RESULTS`
  only for provider egress with selected tools or for remote MCP servers —
  a local run with only integration endpoints would get neither category
  and refuse its own calls at the second leg. The 0016 CHECK widens to
  match, and `CurrentSchemaVersion` moves to `0017` (a second pin beside
  the migration list).

**A call names its connection; the decision is authoritative.**
`connection_id` is a required string argument — the model reads valid ids
from the tool description above — validated by the orchestrator against the
frozen decision. There is no prepare-vs-schema TOCTOU: a connection created
after the decision is simply not in it and is refused; the user re-sends to
include it. Because `connection_id` is an argument, the approval's existing
`ArgsHash` binding covers it for free — approve against connection A,
dispatch against B, and `ConsumeApprovalForThirdParty`'s args-hash check
refuses. Revoking or deleting a connection fails the *call* closed (the
unseal path sees status in the same read), and so does a missing or rotated
`TURING_INTEGRATION_KEY` — the sealer is nil without the key, and
`SealedWithThisKey` exists precisely because a connection can outlive its
key; both fail legibly, not with a panic or a generic internal error. Revoking the **last** connection
additionally un-advertises the tools, and a run already frozen with them in
`selected_tools` fails at the run level (`DefinitionsFor`'s
snapshot-unavailable error path) with a clear notice — the honest
consequence of the frozen-snapshot design, stated rather than promising
per-call granularity it cannot deliver.

**Approval consumption is caller-side, and the approval must be legible.**
`ConsumeApprovalForThirdParty` already binds `RunID`, `ServerName`,
`ToolName`, and `ArgsHash`; the consumption path needs the right
`server_name` on the beacon and no new approval code. The legibility half is
real work the current system cannot do: today's approval event carries only
a one-line `args_summary` ("Requested tool use"), and that is all the
Flutter approval card renders. An integration write approval must show the
acting **connection's display name**, the full destination
(owner/repo/issue), and the full body — an approval showing a truncated
body is an approval for something the user did not read. Where that work
actually lives, because the obvious place is wrong twice: the approval
event does **not** travel as a typed proto message — it is a JSON payload
map built in `repository/approvals.go` and delivered as a free-form event
`Struct` — so the full render is a new key in that map (which is also where
the connection display name is joined in), plus Flutter approval-card
rendering; there is no proto field to add. And the size refusal cannot live
in `internal/service/integrations`, which only runs *after* the approval is
granted — it is a new deny branch in the beacon path
(`runtime/service.go`, beside the existing `denyToolBefore` reasons) with
its own reason string, so no truncated approval is ever created. The bound
gets a name and a number: `maxIntegrationApprovalRenderBytes = 32 KiB`.

**Retrieved content is data.** Results come back bounded
(`maxIntegrationResultBytes = 16 KiB` per call, named constant, truncated on
a UTF-8 rune boundary, truncation announced to the model in the result
rather than silent), framed as retrieved third-party content, with the
framing spoof-resistant: a response body that reproduces the framing
delimiters must not be able to close the frame. The framing is hygiene, not
a security boundary — the boundary is that anything the model *does* in
response still passes the same approval and egress gates as ever. The plan
claims nothing stronger, in the code or the docs.

**Automations are excluded, twice, and the exclusion is written down.** A
local-model automation run enqueues with no egress decision, so every
integration call from one is refused — the right failure, but it must be
documented and surfaced in the automations editor ("integrations are not
available to automations"), not discovered by a user whose nightly
"check my GitHub issues" automation silently never works. And integration
tools are refused on automation allowlists at save — the allowlist check in
`repository/automations.go` is purely syntactic today, so this is a new
check, not a filter tweak. The allowlist pre-authorizes approvals, and an
unattended, pre-authorized write to an external account is exactly what the
approval model's assume-a-human-present stance exists to prevent. Lifting
either half is future work with its own consent design.

**The product consequence is named: connecting an account makes local sends
ask.** The egress policy's existing rule for remote MCP tools applies
verbatim — an enabled integration tool makes every local run a run that may
call it, so each send asks for its own decision. Connecting GitHub therefore
converts every local chat send into a consent-dialog send until the user
disables the tools they don't want offered — which is why the policy-editing
RPC above is a prerequisite, not a nicety. The Integrations page says this at
connect time.

**Phase one is GitHub, alone.** One provider proves the entire path:
unseal-per-call, signed-challenge egress coverage, read tools, one write
tool through the caller-side approval flow. Four tools: `github.list_issues`,
`github.get_issue`, `github.get_file`, `github.create_comment`. Coverage is
endpoint-granular, not repo-granular — consent for `api.github.com` under a
PAT covers every repository that PAT reaches, and
`docs/architecture/remote-egress-policy.md` says so in one sentence so the
run notice is not read as more precise than it is. IMAP and CalDAV are
deferred: different wire protocols, zero architectural novelty, and IMAP
drags in parsing arbitrary MIME, which deserves its own review. Notion is
mechanical after GitHub.

## What gets built

The honest file-level list:

- **Migration `0017`** — drop and recreate the two 0016 tool triggers with
  the widened carve-out; rebuild `run_egress_decisions` (0016's
  rename-copy-drop, under a fresh temp-table name — 0016 already used
  `_before_mcp_registry`) adding `integration_endpoints_json` with
  `json_valid`/`json_type` CHECKs, the widened local-provider CHECK, the
  preserved `run_id … REFERENCES agent_runs(id) ON DELETE CASCADE` (the
  schema-invariants test pins cascade ownership), and the recreated
  provider/consent index. `internal/db/migrations_test.go` pins both the
  list and `CurrentSchemaVersion`; update both.
- **`internal/repository/integrations.go`** — the single sealed-blob+status
  accessor; rewritten `Connection` doc comment.
- **`internal/repository/approvals.go`** — the full-render key in the
  approval event payload map, joined with the connection display name.
- **`internal/app/app.go` and `internal/auth`** — the
  `IntegrationService` public/internal facet split and its registrations;
  the two new full-method names on the `runtime` service identity; the
  integrations registry-change notifier wiring; the rewritten
  "nothing internal reads a connection" comment.
- **`internal/repository/tools.go`** — the `integrations` branch in
  `UpsertTools`; the by-name policy method with the pseudo-server `enabled`
  derivation.
- **`internal/repository/automations.go`** — the integrations refusal in
  allowlist validation.
- **`internal/repository/egress.go`, `internal/repository/jobs.go`,
  `internal/service/chat/egress.go`** — the four widened local-provider
  mirrors plus the data-category attachment for integration-only runs;
  `integration_endpoints_json` through the signed challenge payload with its
  canonical ordering, structural validation,
  `payloadMatchesEgressContext`, and the enqueue freeze;
  `IntegrationEndpointsForTools`.
- **`internal/service/integrations`** (extended) — the GitHub provider
  client on `NoRedirectClient`/`RedactRedirectError` and the shared guarded
  resolver; the per-tool `read_only` table; four-leg decision validation;
  post-approval revalidation and `IntegrationDispatchActive`; caller-side
  approval consumption; result bounding and framing; the sealer interface
  seam; the internal facet.
- **`internal/service/tools/defaults.go`** — the `github.` case in
  `BundledServerForTool`.
- **`internal/service/mcpregistry/import.go`** — `integrations` added to
  the reserved server names.
- **`proto/turing/v1`** — `CallIntegrationTool`, `ListIntegrationTools`,
  `UpdateToolPolicyByName`; `integration_endpoints` on the egress
  disclosure and decision messages; `read_only` on `ToolPolicyDecision`
  (pinned field numbers, `tools/proto/check.sh` compares bytes). The
  approval render is deliberately **not** here — see above.
- **Runtime (`agent-runtime-go`)** — the dynamic integrations lister over
  `ListIntegrationTools`; beacons stamped `integrations`; forwarding over
  `CallIntegrationTool`; `read_only`-aware error classification in
  `tools/runner.go`.
- **Orchestrator runtime service** — `filterRegisteredWorkerTools`'s
  `integrations` case; `beaconServerName` mapping; the pre-approval
  render-size deny branch beside `denyToolBefore` with its own reason
  string.
- **`internal/service/mcpregistry/service.go`** — `UpdateToolPolicyByName`
  on the public facet, with all four properties above.
- **Client** — the Integrations page shows each connection's tools with the
  new policy editor and the connect-time "sends will ask" notice; the
  egress dialog renders one labeled line per connection; the approval card
  renders the full-arguments field; the automations editor states
  integrations are unavailable. Compact layout (<840px) included.

## The tests that gate the merge

Adapt names to the implementation; every assertion must survive. For 1–8
and 16, break the production gate, watch the right test fail, restore.

1. **Credential hygiene, including transport failure.** After a full call
   cycle — an HTTP-status failure *and* a dial/TLS-level failure (the
   `*url.Error` path that embeds URLs in error strings) — the plaintext
   credential appears in no event payload, audit row, log line, tool
   result, or error string.
2. **One stack frame.** Two consecutive calls unseal exactly twice
   (counting fake behind the sealer interface) — the assertion that
   mechanically catches a per-connection client caching the plaintext.
3. **No decision, no dial — on all four legs.** Refused before any network
   I/O, asserted with a transport that fails the test if touched: a tool
   name absent from `selected_tools`; a decision missing `TOOL_ARGUMENTS`
   or `TOOL_RESULTS`; an `(endpoint, connection_id)` pair absent —
   specifically, a decision naming connection A refuses a call naming
   connection B on the same host; a tool absent from the matched entry's
   `tools` list.
4. **The signed challenge binds the endpoints — tested by changing the
   world, not the token.** Tampering with the challenge fails the HMAC and
   proves nothing about the match check. The test that isolates
   `payloadMatchesEgressContext`: prepare, then revoke (or create) a
   connection, then send — refused as "context changed; prepare again". An
   implementation that threads endpoints everywhere except the match check
   passes a tamper test and fails this one.
5. **Approval-required calls consume exactly once, reads included, bound to
   the connection.** A write without a valid unconsumed approval is
   refused; with one, consumed exactly once; an args mismatch — including
   only `connection_id` differing — is refused. An `approval_required`
   *read* also waits for and consumes an approval bound to its args.
6. **Revalidation and liveness.** The run is cancelled (or the tool
   disabled, or the session enters deletion) during the approval wait ⇒ no
   dispatch, transport untouched.
7. **Revocation and deletion mean now — at both granularities.** A
   connection revoked after consent fails the next call closed; a
   connection *deleted* (the separate code path) likewise. Revoking the
   last connection un-advertises the tools, fires the registry-changed
   notification, and a run frozen with the tool in `selected_tools` fails
   at the run level via the snapshot-unavailable path.
8. **Policy round-trip, with both guards and the notification.** Tools
   registered through `UpsertTools` land `approval_required`;
   `UpdateToolPolicyByName` set `safe` ⇒ a read dispatches with no
   approval; set `disabled` ⇒ refused — and the same RPC **refuses** to set
   a bundled mutating tool (`files.create`) to `safe`, **refuses** to set
   an integration write (`github.create_comment`) to `safe`, does not
   disable the pseudo-server tool it edits, and fires the registry-changed
   notification — a disabled tool leaves `EgressToolNames` and the next
   disclosure without a restart.
9. **The happy path exists, connection id included.** A local-Ollama run
   offered GitHub tools gets a non-empty egress disclosure whose entries
   carry display names; the advertised tool description enumerates the live
   `(connection_id, display name)` pairs; with consent granted, a mocked
   `github.list_issues` completes end to end using an id taken from the
   description, not hardcoded.
10. **A failed read is not a dead run — at both failure sites.** A provider
    500 on an approved read returns a bounded tool error to the model and
    the run continues; a *successful* read whose after-report to the
    orchestrator fails is likewise recoverable, not
    `SideEffectCommittedError`. A transport failure on a write still halts
    the run.
11. **Transport hardening.** A 30x from the provider is refused before the
    redirect is followed and the resulting error is redacted; a resolver
    answer mapping the provider host to a private address is refused.
12. **No ambient egress, automations included.** A run offered no
    integration tools has no integration endpoints in its decision; an
    automation run's integration call is refused (no decision exists); an
    integration tool on an automation allowlist is rejected at save.
13. **The approval is legible, or the write is refused.** An integration
    write approval event carries the full render — connection display name,
    destination, complete body; a write whose render exceeds
    `maxIntegrationApprovalRenderBytes` is refused before any approval is
    created, with no truncated approval ever emitted.
14. **Bounded, framed, spoof-resistant results.** An oversized response is
    truncated to `maxIntegrationResultBytes` on a rune boundary with the
    truncation announced; a body containing the framing delimiters cannot
    close the frame.
15. **Zero connections, zero surface.** With no live connections the tools
    are absent from the registry and from `EgressToolNames`, prepare
    returns no integration endpoints, and connecting one fires the
    registry-changed notification that makes them appear without a
    restart.
16. **The credential used is the credential named.** Two live GitHub
    connections; an approved call naming connection B; the outbound
    `Authorization` header carries B's credential, not A's — the test that
    kills a resolver which validates `connection_id` against the decision
    and then unseals `connections[0]`.

## Deferred, deliberately

- **IMAP, CalDAV, Notion providers** — after GitHub proves the path. The
  endpoint-bearing providers additionally wait on the tightened endpoint
  rule (keyed HTTPS endpoint + the shared public-address class check;
  today's `isPlausibleHost` accepts loopback and `ParseKeyedEndpoint`
  permits loopback `http://`).
- **GitHub Enterprise** — a different host is exactly what the pinned-host
  decision refuses in phase one.
- **OAuth flows** — Integrations takes credentials the user minted at the
  provider, by design; a redirect-based flow is its own project.
- **Automations access** — both halves (egress consent for unattended runs,
  allowlisting integration writes) need their own consent design.
- **Provider-call budgets and rate limiting** — telemetry already counts
  tool calls; enforcement is future work and saying otherwise here would be
  a lie.

## Documentation the implementation PR must update

- `docs/mcp-security-and-integration.md` — integrations join the
  caller-side enforcement section; the credential-handling story (sealed at
  rest, one stack frame in flight, header-only transport, hardened
  no-redirect dialer, never in events or logs) written down.
- `docs/architecture/remote-egress-policy.md` — integration endpoints named
  as the third egress path beside remote model providers and remote MCP
  servers; the every-send-asks consequence and the endpoint-not-repo
  granularity sentence.
- `docs/VISION.md` — three edits, no invariant weakened: deferred-list item
  6's "no tool consumes a connection … nothing dials anywhere" rationale is
  rewritten to describe the gates that replaced it; the caller-side
  qualification on the approval invariant, added for the MCP registry,
  extends to integrations; and the untrusted-input invariant gains
  retrieved integration content as the sibling of skill text.
