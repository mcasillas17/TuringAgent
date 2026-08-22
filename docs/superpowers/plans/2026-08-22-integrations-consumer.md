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
  worker's capabilities, so they never reach the `tools` table, never
  appear in `EgressToolNames`, and every beacon dies as `unknown_tool`.
  **And the `skills`-style blind pass-through is the wrong model for the
  new case**: the policy-change notification prunes capabilities and then
  triggers a worker re-report that flows back through this very filter, so
  a pass-through re-admits a just-disabled tool seconds later — silently
  breaking the one escape hatch from every-send-asks, and re-feeding the
  disabled tool's endpoint into future decisions. The `integrations` case
  consults a pseudo-server-aware availability check (live `tools` row,
  `policy != 'disabled'`, with **no row yet counting as available** or a
  new tool could never bootstrap into registration) — and since
  `UpdateToolPolicyByName` makes skills policies editable for the first
  time, the `skills` pass-through gets the same treatment in the same
  commit. The predicate is **policy-only** — explicitly *not* `present` or
  `enabled` — because `UpsertTools` opens every upsert by zeroing both for
  pseudo-server rows and restores them only for tools in the reported
  union, which this very check gates: include `present = 1` in the
  predicate and disabling becomes a **permanently** one-way door — a
  restart replays the same zero-then-restore sequence, so not even that
  reopens it. `IntegrationEndpointsForTools` likewise filters on
  `tools.enabled`, as `RemoteMCPServersForTools` already does;
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
least one live connection for the provider exists, **filtered on policy
and enabled state exactly as its precedent filters**
(`RegistryClient.ListTools` skips `!enabled` and disabled-policy
descriptors — that is how a disabled tool leaves the model's registry
rather than lingering as an offer whose every call dies `unknown_tool`),
**with the same missing-row rule as the availability check**: the
precedent never faces a missing row because import seeds its `tools` rows
before discovery, but integrations have no seeding step, so a literal
"missing ⇒ not enabled" reading deadlocks the bootstrap (never listed ⇒
never reported ⇒ never inserted ⇒ never listed) — a tool with no row yet
is listed, and lands `approval_required` on first registration as always —
and whose tool *descriptions* enumerate the current live connections as
`(connection_id, display name)` pairs. That is the answer to "how does the model obtain a
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
  per-tool `read_only` table in `service/tools` is the source of truth,
  and the rule is **scoped to `server_name == "integrations"`**: an
  integration tool not marked `read_only` refuses `safe`; tools of other
  servers are untouched by this rule — stated because the unscoped reading
  ("anything not in the table refuses safe") would refuse `safe` for
  `skills_list` and kill the very editability this RPC exists to provide.
  Both directions are tested: the integration write's refusal, and a
  skills tool successfully set `safe`;
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
field, set by the orchestrator at **every** `ToolPolicyDecision`
construction site on the before path — the `approval_required` return, the
`safe` return, and the easy one to miss, `existingToolBeforeDecision`,
which rebuilds a fresh decision for a re-delivered beacon and would
otherwise re-arm side-effect handling on a read (setting it there is a
signature change, not a field assignment — the `ToolCallRecord` it takes
carries no tool identity, so the beacon's server and tool name thread in
from the callers, which all have it in scope) — from a static per-tool
table that lives in `service/tools` beside `BundledServerForTool` — not inside
`service/integrations`, because its two readers, the by-name policy RPC in
`mcpregistry` (the un-`safe`-able guard) and the runtime service's beacon
path (this decision bit), both already import `service/tools`, which is a
stdlib-only leaf; importing `service/integrations` from both would be
possible (no cycle exists) but adds two cross-service edges for one static
map. When `read_only` is true the runner changes the
side-effect handling at its three sites while approval-waiting is
unchanged — with the three sites claimed honestly, because they do not all
deliver the same thing: a failed **call** is a recoverable tool error, not
`SideEffectUnknownError`, and the run continues; `RunOutcome.SideEffecting`
stays false, so run-level retryability is unaffected by reads; and a
*successful* call whose after-report to the orchestrator fails is
**classified** as a reporting failure rather than
`SideEffectCommittedError` — the run still ends there, deliberately,
because continuing when the orchestrator has lost track of a tool call's
state is wrong for reads and writes alike; what changes is that the failure
is not recorded as a committed side effect — and the reporting-failure
branch's hardcoded "safe tool call completed" message becomes policy-aware
in the same commit, or the transcript states a falsehood about an
`approval_required` call. Two named consequences,
intended: a run whose last tool call was a `read_only` GET stays retryable
and may re-issue the GET; and on the crash/lease-expiry path the stale-
assignment sweep still marks any open tool call `side_effect_uncertain`
regardless of `read_only` — a coarse edge this plan accepts rather than
touching the reconciliation machinery. Write tools keep the existing
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
  most `maxIntegrationEndpointEntryBytes = 768` bytes — mirroring the
  sortedness and strict-ordering rules `validChallengePayload` already
  enforces for `SelectedTools` and `RemoteMCPServers`. The arithmetic is
  stated because the first draft got it wrong: these are **sub-budgets**
  under the pre-existing `maxEgressChallengeBytes = 32 KiB` total, in the
  `maxEgressSelectedToolBytes` mold, and they must sum comfortably below
  it — 16 KiB of selected tools + 12 KiB of endpoint entries leaves ~4 KiB
  for the nonce, digest, fingerprints, JSON structure **and the
  `remote_mcp_servers` list, the third variable-length term in the same
  payload** (bounded in name length but not URL length — worth remembering
  when spending the slack), where 16 × 1 KiB would have filled the total
  exactly and made the opaque signing failure the *guaranteed* outcome at
  both caps. To fit 768 bytes with worst-case
  JSON escaping, the entry's `display_name` is capped at 64 runes
  (ellipsis-truncated for the disclosure; the Integrations page still
  shows the full name). Two mechanical
  consequences are named so nobody discovers them in a debugger: the inner
  `tools` slice is always marshalled non-nil and sorted (nil vs. `[]` is a
  byte difference that silently voids valid challenges), and the entry
  struct is not `comparable` (it holds a slice), so
  `payloadMatchesEgressContext` compares these with `slices.EqualFunc`,
  not `slices.Equal`. Exceeding either sub-budget — entry count or
  per-entry bytes — is refused at `resolveEgressContext` with a legible
  `FailedPrecondition` naming the remedy (revoke connections or disable
  tools), not left to surface from `signEgressChallenge` as the opaque
  `Internal` that would then appear on **every send on every provider**.
  (`resolveEgressContext` cannot enforce the 32 KiB *total* — it has no
  nonce, digest, or timestamps yet — which is exactly why the sub-budgets
  must sum under it with slack.) The per-entry bound is measured by **one
  shared helper** at both check sites (`resolveEgressContext` and
  `validChallengePayload`) — a slightly more permissive early check
  recreates the exact opaque signing failure the sub-budgets exist to
  prevent. And `clonePendingEgressDecision`, which sits on the fingerprint
  path, clones and sorts `RemoteMCPServers` explicitly today — the
  integration slice gets the same clone-and-sort or it rides through
  aliased and unsorted. These caps are reachable, unlike `maxEgressTools`:
  sixteen connections is plausible once the deferred providers land.
- Those entries ride inside the HMAC-signed `egressChallengePayload`, are
  covered by the structural validation and by
  `payloadMatchesEgressContext`, and are frozen at enqueue — so the list the
  user acknowledged is the list the signature binds, and a connection set
  that changes between prepare and send is detected, not absorbed. They
  also join the enqueue idempotency fingerprint
  (`EnqueueRequestFingerprint`, with its version bumped) — `RemoteMCPServers`
  earned a dedicated regression test when it was added there, and
  integration endpoints get the sibling: a resend under the same
  idempotency key whose only difference is the acknowledged connection set
  must not be absorbed as a replay of the earlier consent.
- The transcript run notice and the consent audit row name the
  destinations: `enqueueUserMessageTx` builds its destination list from the
  provider endpoint host and the remote MCP hosts today, which the widened
  CHECK makes emptiable — integration endpoint hosts join it,
  deduplicated, or an integration-only local run ships the one blank
  disclosure line in the one place the project insists disclosure must
  appear.
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
  equivalent to `MCPDispatchActive` runs immediately before the network
  call — equivalence includes everything its precedent checks: run
  executing, not cancelled, session not mid-deletion, **and the tool's
  live policy and enabled state**, which is what catches a tool disabled
  after enqueue, since the frozen decision cannot. Integration tools bypass
  the `mcp_servers` join that query uses, so they need their own
  (`IntegrationDispatchActive`), along with an
  `IntegrationEndpointsForTools` resolver analogous to
  `RemoteMCPServersForTools`.
- The "local provider needs no decision" rule is mirrored in Go in **five**
  places across **two modules**, and every one widens from "has remote MCP
  servers" to "has remote MCP servers or integration endpoints":
  `normalizePendingEgressDecision` (`repository/egress.go`), the
  local-decision refusal in `repository/jobs.go`, the early return in
  `resolveEgressContext` (`service/chat/egress.go` — miss this one and a
  local-Ollama run never gets a consent dialog and the feature silently
  does not exist for local models), the challenge-payload validation in
  the same file — and the one in the *other module*, the runtime's
  `validateEgressDecisionShape` (`agent-runtime-go/internal/agent/
  egress.go`), which rejects an integration-only local decision **twice
  independently**: "local run carries an inapplicable egress decision"
  (no remote MCP servers), and the exact-set data-category check, whose
  `required` list stays empty while the decision carries
  `TOOL_ARGUMENTS`/`TOOL_RESULTS`. Fix only the orchestrator side and
  every integration-only local run dies at `Execute` as
  `egress_decision_invalid` before the first model call. Feeding that
  check also means `toProtoEgressDecision` (`runtime/service.go`)
  populates the new `integration_endpoints` field on the job-side
  `RunEgressDecision`. **A fifth
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
its own reason string, so no truncated approval is ever created. The render
is built by **one shared function used by both sites** — the beacon-path
size check and the payload key — or the two drift and the no-truncated-
approval invariant fails silently; and since `repository` imports no
`service/*` package, that function lives in `repository` (or a leaf
package), not in `service/integrations`, which an implementer would
otherwise discover as an import cycle. The bound gets a name and a number:
`maxIntegrationApprovalRenderBytes = 32 KiB`.

**Retrieved content is data.** Results come back bounded
(`maxIntegrationResultBytes = 16 KiB` per call, named constant, truncated on
a UTF-8 rune boundary, truncation announced to the model in the result
rather than silent), framed as retrieved third-party content, with the
framing spoof-resistant by a named mechanism, since no in-repo precedent
exists to imitate: the delimiters carry a per-call random nonce, so a
response body that reproduces the delimiter text cannot close the frame —
guessing the nonce is the only way, and the nonce never appears in
anything the provider can see. The framing is hygiene, not
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
- **`internal/app/app.go` and `internal/app/app_test.go`** — the
  `IntegrationService` public/internal facet split and its registrations;
  the two new full-method names on the `runtime` service identity (the
  identity list is a literal in `app.go`; `internal/auth` itself needs no
  change, and the facet refusals are asserted in `app_test.go`); the
  integrations registry-change notifier wiring; the rewritten
  "nothing internal reads a connection" comment. A third comment falsified
  by this work is named with the other two: `service/integrations`' package
  doc rule that the package "never reads the sealed column back".
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
  `EnqueueRequestFingerprint` widened with its version bump; the run-notice
  destination list gaining integration endpoint hosts;
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
  `tools/runner.go`; the widened `validateEgressDecisionShape` in
  `internal/agent/egress.go` (both rejections).
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

Adapt names to the implementation; every assertion must survive. For 1–8,
16 and 17, break the production gate, watch the right test fail, restore.

1. **Credential hygiene, including transport failure.** After a full call
   cycle — an HTTP-status failure *and* a dial/TLS-level failure (the
   `*url.Error` path that embeds URLs in error strings) — the plaintext
   credential appears in no event payload, audit row, log line, tool
   result, or error string.
2. **One stack frame.** Two consecutive calls **to the same connection**
   unseal exactly twice (counting fake behind the sealer interface) — the
   same-connection pin is the point: a per-connection cache produces two
   cache-misses on two different connections and passes, but a repeat call
   to one connection exposes the cache hit.
3. **No decision, no dial — on all four legs.** Refused before any network
   I/O, asserted with a transport that fails the test if touched: a tool
   name absent from `selected_tools`; a decision missing `TOOL_ARGUMENTS`
   or `TOOL_RESULTS`; an `(endpoint, connection_id)` pair absent —
   specifically, a decision naming connection A refuses a call naming
   connection B on the same host; a tool absent from the matched entry's
   `tools` list.
4. **The signed challenge binds the endpoints — tested by changing the
   world, not the token.** Tampering with the challenge fails the HMAC and
   proves nothing about the match check. The variant is pinned, because
   the obvious one accidentally tests the wrong comparison: with **two**
   live connections, prepare, revoke one, and send — `SelectedTools` is
   unchanged (the tools are still advertised via the survivor), so the
   refusal can only come from the endpoint comparison in
   `payloadMatchesEgressContext`, and it must be the context-changed one.
   Revoking an *only* connection instead changes `SelectedTools` and the
   pre-existing comparison catches it, proving nothing. The per-entry byte
   bound gets the boundary probe test 13 gives the approval render: an
   entry rendering to exactly `maxIntegrationEndpointEntryBytes` prepares,
   signs, and verifies; one byte over is refused at `resolveEgressContext`
   with the legible `FailedPrecondition`, never at signing — the leg that
   catches two check sites measuring with different arithmetic. And the
   enqueue idempotency fingerprint is exercised the way `RemoteMCPServers` was
   when it joined — as a pure-function test (two decisions differing only
   in the connection set produce different fingerprints); the live
   observable is a refusal as an idempotency **conflict**
   (`AlreadyExists`), not a fresh enqueue, so do not write a test that
   waits for a second run.
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
8. **Policy round-trip, with both guards and the notification — a real
   round trip, for both pseudo-servers.** Tools registered through
   `UpsertTools` land `approval_required`; `UpdateToolPolicyByName` set
   `safe` ⇒ a read dispatches with no approval; set `disabled` ⇒ refused
   and gone from the model's offer — asserted per pseudo-server by the
   observable each can actually deliver: the **integration** tool leaves
   the runtime's tool registry (`ListIntegrationTools` filters), while the
   **skills** tool — whose static lister has no policy source, and this
   plan does not re-architect it — leaves the *offered definitions*
   (`selected_tools` / `DefinitionsFor`) via the capabilities prune; then
   **re-enabled ⇒ works again** — the leg that catches a `present = 1`
   predicate making disable a one-way door. The
   same RPC **refuses** to set a bundled mutating tool (`files.create`) to
   `safe`, **refuses** to set an integration write (`github.create_comment`)
   to `safe`, **allows** a skills tool to be set `safe` (the unscoped
   guard's failure mode), does not disable the pseudo-server tool it
   edits, and fires the registry-changed notification — a disabled tool
   leaves `EgressToolNames` and the next disclosure without a restart,
   **and stays out after the worker's capabilities re-report**, asserted
   for an integration tool **and for a skills tool** (`skills_list`),
   since the skills pass-through's self-revert is newly reachable the
   moment skills policies become editable: the notification triggers
   re-discovery whose report flows back through
   `filterRegisteredWorkerTools`, which is exactly where a blind
   pass-through re-admits the tool seconds after the prune.
9. **The happy path exists, connection id included, disclosure legible.**
   A local-Ollama run offered GitHub tools gets a non-empty egress
   disclosure whose entries carry display names; the advertised tool
   description enumerates the live `(connection_id, display name)` pairs;
   with consent granted, a mocked `github.list_issues` completes end to
   end **through the agent's `Execute`** — the leg that fails if the
   runtime's own decision-shape validation was not widened — using an id
   taken from the description, not hardcoded; and the transcript run
   notice and consent audit row name `api.github.com`, never an empty
   destination.
10. **A failed read is not a dead run — with each site asserting what is
    actually true.** A provider 500 on an approved read returns a bounded
    tool error to the model and the run continues; a *successful* read
    whose after-report to the orchestrator fails still ends the run but is
    classified as a reporting failure, not `SideEffectCommittedError`. A
    transport failure on a write still halts the run.
11. **Transport hardening.** A 30x from the provider is refused before the
    redirect is followed and the resulting error is redacted; a resolver
    answer mapping the provider host to a private address is refused.
12. **No ambient egress, automations included.** A run offered no
    integration tools has no integration endpoints in its decision; an
    automation run's integration call is refused **by the missing egress
    decision** — pinned by using a `read_only` tool set to `safe`, so the
    allowlist/approval gate cannot fire first and the refusal can only
    come from the mechanism this leg exists to test; an integration tool
    on an automation allowlist is rejected at save.
13. **The approval is legible, or the write is refused — through one
    builder.** An integration write approval event carries the full render
    — connection display name, destination, complete body; a write whose
    render exceeds `maxIntegrationApprovalRenderBytes` is refused before
    any approval is created, with no truncated approval ever emitted. The
    boundary is probed to catch two drifting render functions: a write
    rendering to exactly the bound is approved with its payload render
    byte-identical to what the size check measured; one byte over is
    denied.
14. **Bounded, framed, spoof-resistant results.** An oversized response is
    truncated to `maxIntegrationResultBytes` on a rune boundary with the
    truncation announced; a body containing the framing delimiters cannot
    close the frame — and two calls carry **different** delimiters, the
    assertion that kills a process-wide constant nonce (which the model,
    and therefore anything the model writes to a provider, eventually
    reveals).
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
17. **The name-collision guards hold.** An `mcp.json` server named
    `integrations` is reported unsupported, not imported; a third-party
    MCP server exporting a `github.*` tool is refused at discovery as a
    collision — the two audited edits (`import.go` reserved names,
    `BundledServerForTool`) that no other test reaches, and whose failure
    mode is permanently broken discovery.

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
