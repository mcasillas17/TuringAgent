# Integrations Consumer Implementation Plan

Give the model tools that act on connected accounts. Integrations today is a
vault with no customers: users connect IMAP, CalDAV, Notion, and GitHub
accounts, the credential is AES-256-GCM-sealed under `TURING_INTEGRATION_KEY`,
consent is recorded, revocation works — and no code path ever *opens* a
credential to do anything. The consent screen tells the user what the
credential grants; nothing exercises a single grant. `docs/VISION.md` even
leans on this: its Integrations section currently argues the feature is safe
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
ciphertext on any read path (`credentialHeaderBytes`), with a file comment
promising the sealed secret is read by nothing in the file. The sealing
service (`internal/service/integrations`) already exists; it seals and never
opens.

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
credentials. The bundled MCP servers keep no egress as a property of their
code and of never being published; the third-party MCP network
(`net-mcp-registry`) is `internal: true`. An integrations container would
need real internet egress, which no MCP container has today — adding one
would put a credentialed, internet-dialing process outside the only component
that enforces egress decisions. The orchestrator already dispatches
third-party calls on the runtime's behalf (`CallRegisteredMcpTool`, PR #73);
integration tools take the same shape over a new `CallIntegrationTool` RPC.
This *extends* the existing `internal/service/integrations` package — it does
not create it.

**The plaintext credential exists in one stack frame, and the repository
changes to allow exactly that.** A single new repository method returns the
sealed blob *and* the connection status in one statement, so a revoked row
can never be read as live. It is the only method that touches full
ciphertext; the file's header comment is rewritten to name it rather than
quietly falsified. The service unseals per call, inside the provider-client
call; no provider client, cache, or struct field holds the plaintext between
calls. The token travels in the `Authorization` header only — never in a URL,
because Go transport errors (`*url.Error`) embed the full URL in error
strings, which would put a query-string token into logs on a mere dial
failure.

**The provider client reuses the registry's transport hardening.** The GitHub
client dials through the same guarded resolution as remote MCP
(`resolvePublicMCPAddress` — refuses loopback, private, link-local,
multicast, unspecified) and rejects all redirects before they are followed
(`rejectMCPRedirect`), factored to be shareable rather than copied. Without
both, a 30x from the provider or a hostile DNS answer for `api.github.com`
sends the PAT somewhere the egress decision never named. Phase one pins the
GitHub host as a constant (`https://api.github.com`); GitHub Enterprise —
whose whole point is a different host — is explicitly out of scope, as is
every use of the `endpoint` column, whose current `isPlausibleHost`
validation accepts loopback and link-local values and must be tightened
before any provider ever dials it.

**`integrations` is a pseudo-server like `skills`, and every mechanism that
special-cases `skills` is updated in the same commit.** Tools carry
`server_name = 'integrations'`, `mcp_server_id IS NULL`. That touches, at
minimum:

- the 0016 triggers (`0017` widens the carve-out to `('skills',
  'integrations')`);
- `repository/tools.go` `UpsertTools`, which branches on the literal
  `"skills"` — without a matching branch, integration tools are *silently
  never registered* (the non-skills path inserts via a `SELECT` from
  `mcp_servers` that yields zero rows and continues without error);
- `service/tools/defaults.go` `BundledServerForTool`, which gains a
  `github.` prefix case returning `integrations` — this is also what stops a
  registered third-party MCP server from exporting a colliding `github.*`
  tool and taking down all tool discovery at `BuildToolRegistry`'s duplicate
  check;
- `runtime/service.go` `beaconServerName`, whose dot-prefix fallback would
  resolve a server-nameless `github.list_issues` beacon to server `"github"`
  — the runtime always stamps `integrations` on these beacons, and the
  fallback learns the same mapping defensively.

Tool names stay `github.*`: shadow-protection and beacon routing are fixed at
the mechanisms above, not by contorting the names.

**Per-tool policy editing must actually work, which today it does not for any
pseudo-server.** `UpdateMcpToolPolicy` and the Flutter editor are keyed on
`mcp_servers.id` end to end; `skills_list`/`skill_view` are uneditable today
for exactly this reason. This plan adds a server-name-keyed policy RPC
(`UpdateToolPolicyByName(server_name, tool_name, policy)`) plus repository
method and client wiring, used by the Integrations page — and incidentally
making skills tools editable. Without it, "the user relaxes reads per-tool"
is a claim about a control that does not exist.

**Everything defaults to `approval_required`, reads included, and the policy
comes from the `tools` table.** `DefaultPolicyFor` seeds nothing for
`integrations`; unknown tools already fall to `approval_required`, and no new
defaulting logic is added. The dispatch path reads the stored policy — `safe`
genuinely skips the approval gate, `disabled` genuinely refuses — asserted by
a round-trip test, so an implementation that hardcodes `approval_required`
and never registers the tools cannot pass.

**A failed read is a tool error, not a dead run.** Today an
`approval_required` decision marks the call side-effecting, and a call error
becomes `SideEffectUnknownError`, which fails the whole run — correct for
mutations, absurd for a GitHub 500 on `list_issues`. The read tools are
declared side-effect-free at the runner level: once past their gates, a
provider error returns to the model as a bounded tool error. Write tools keep
the existing behavior; "we don't know whether the comment posted" *should*
halt the run.

**Every integration call joins the per-run egress decision — through the
signed challenge, not just the database row.** PR #73's pattern, all of it:

- The decision gains `integration_endpoints_json`: an array of
  `{endpoint, connection_id, tools}` where `endpoint` is a canonical keyed
  endpoint (`backendegress.ParseKeyedEndpoint`, HTTPS only).
- Those entries ride inside the HMAC-signed `egressChallengePayload`
  alongside `RemoteMCPServers`, are covered by the structural validation and
  by `payloadMatchesEgressContext`, and are frozen at enqueue — so the list
  the user acknowledged is the list the signature binds, and a connection
  set that changes between prepare and send is detected, not absorbed.
- Validation before dispatch checks all three legs of `RunAllowsRemoteMCP`'s
  shape, plus one: `integrations/<tool>` present in the decision's
  `selected_tools`; `TOOL_ARGUMENTS` and `TOOL_RESULTS` present in the data
  categories; and the `(endpoint, connection_id)` *pair* present in the
  decision's integration endpoints. Matching on host alone is refused as a
  design: two GitHub connections share `api.github.com`, and consent for
  connection A must not authorize connection B.
- Revalidation happens again after the approval wait, and a liveness check
  equivalent to `MCPDispatchActive` (run executing, not cancelled, session
  not mid-deletion) runs immediately before the network call — integration
  tools bypass the `mcp_servers` join that query uses, so they need their
  own.
- The "local provider needs no decision" rule is mirrored in Go in four
  places, and every one widens from "has remote MCP servers" to "has remote
  MCP servers or integration endpoints": `normalizePendingEgressDecision`
  (`repository/egress.go`), the local-decision refusal in
  `repository/jobs.go`, the early return in `resolveEgressContext`
  (`service/chat/egress.go` — miss this one and a local-Ollama run never
  gets a consent dialog and the feature silently does not exist for local
  models), and the challenge-payload validation in the same file. The 0016
  CHECK widens to match.

**A call names its connection; the decision is authoritative; the tool is as
static as its snapshot.** `connection_id` is a required string argument,
validated by the orchestrator against the frozen decision — not an enum baked
into the schema, so the runtime needs no connection list and there is no
prepare-vs-schema TOCTOU: a connection created after the decision is simply
not in it and is refused; the user re-sends to include it. Because
`connection_id` is an argument, the approval's existing `ArgsHash` binding
covers it for free — approve against connection A, dispatch against B, and
`ConsumeApprovalForThirdParty`'s args-hash check refuses. Integration tools
are advertised while at least one live connection for the provider exists;
connecting or revoking fires the same registry-changed notification the MCP
registry uses (`NotifyMCPRegistryChanged` → worker re-discovery), so the tool
set is never stale until restart. Revoking or deleting a connection fails the
*call* closed (the unseal path sees status in the same read). Revoking the
**last** connection additionally un-advertises the tools, and a run already
frozen with them in `selected_tools` fails at the run level with a clear
notice — that is the honest consequence of the frozen-snapshot design, and
this plan states it rather than promising per-call granularity it cannot
deliver.

**Writes are enforced caller-side, and it is almost entirely reuse.**
`ConsumeApprovalForThirdParty` already binds `RunID`, `ServerName`,
`ToolName`, and `ArgsHash`; the write path needs the right `server_name` on
the beacon and no new approval code. What *is* new: the approval the human
sees must show the full destination (owner/repo/issue) and the full body —an
approval showing a truncated body is an approval for something the user did
not read. A write whose rendered arguments exceed the approval display bound
is refused before the approval is ever created, rather than truncated.

**Retrieved content is data.** Results come back bounded
(`maxIntegrationResultBytes = 16 KiB` per call, named constant, truncation
announced to the model in the result rather than silent), framed as retrieved
third-party content, with the framing spoof-resistant: a response body that
reproduces the framing delimiters must not be able to close the frame. The
framing is hygiene, not a security boundary — the boundary is that anything
the model *does* in response still passes the same approval and egress gates
as ever. The plan claims nothing stronger, in the code or the docs.

**Automations are excluded, twice, and the exclusion is written down.** A
local-model automation run enqueues with no egress decision, so every
integration call from one is refused — the right failure, but it must be
documented and surfaced in the automations editor ("integrations are not
available to automations"), not discovered by a user whose nightly
"check my GitHub issues" automation silently never works. And integration
tools are refused on automation allowlists in phase one: the allowlist
pre-authorizes approvals, and an unattended, pre-authorized write to an
external account is exactly what the approval model's assume-a-human-present
stance exists to prevent. Lifting either half is future work with its own
consent design.

**The product consequence is named: connecting an account makes local sends
ask.** The egress policy's existing rule for remote MCP tools applies
verbatim — an enabled integration tool makes every local run a run that may
call it, so each send asks for its own decision. Connecting GitHub therefore
converts every local chat send into a consent-dialog send until the user
disables the tools they don't want offered — which is why the policy-editing
RPC above is a prerequisite, not a nicety. The Integrations page says this at
connect time.

**Phase one is GitHub, alone.** One provider proves the entire path:
unseal-per-call, signed-challenge egress coverage, read tools, one write tool
through the caller-side approval flow. Four tools: `github.list_issues`,
`github.get_issue`, `github.get_file`, `github.create_comment`. Coverage is
endpoint-granular, not repo-granular — consent for `api.github.com` under a
PAT covers every repository that PAT reaches, and
`docs/architecture/remote-egress-policy.md` says so in one sentence so the
run notice is not read as more precise than it is. IMAP and CalDAV are
deferred: different wire protocols, zero architectural novelty, and IMAP
drags in parsing arbitrary MIME, which deserves its own review. Notion is
mechanical after GitHub.

## What gets built

The honest file-level list — this is about twelve sites, not five:

- **Migration `0017`** — widen the 0016 tool triggers to
  `NOT IN ('skills','integrations')`; rebuild `run_egress_decisions` (0016's
  rename-copy-drop) adding `integration_endpoints_json` with
  `json_valid`/`json_type` CHECKs, the widened local-provider CHECK, the
  preserved `run_id … REFERENCES agent_runs(id) ON DELETE CASCADE` (the
  schema-invariants test pins cascade ownership), and the recreated
  provider/consent index. `internal/db/migrations_test.go` pins the list;
  update it.
- **`internal/repository/integrations.go`** — the single sealed-blob+status
  accessor; tightened endpoint validation; rewritten header comment.
- **`internal/repository/tools.go`** — the `integrations` branch in
  `UpsertTools`.
- **`internal/repository/egress.go`, `internal/repository/jobs.go`,
  `internal/service/chat/egress.go`** — the four widened local-provider
  mirrors; `integration_endpoints_json` through the signed challenge payload,
  structural validation, `payloadMatchesEgressContext`, and the enqueue
  freeze.
- **`internal/service/integrations`** (extended) — the GitHub provider
  client on the shared hardened transport; three-leg-plus-connection
  decision validation; post-approval revalidation and the
  dispatch-liveness check; caller-side approval consumption for writes;
  result bounding and framing.
- **`internal/service/tools/defaults.go`** — the `github.` case in
  `BundledServerForTool`.
- **`proto/turing/v1`** — `CallIntegrationTool`; `UpdateToolPolicyByName`;
  `integration_endpoints` on the egress disclosure and decision messages
  (pinned field numbers, `tools/proto/check.sh` compares bytes).
- **Runtime (`agent-runtime-go`)** — an `integrationsToolLister` synthesized
  like `newSkillToolLister()` (nothing arrives via the MCP registry path,
  which skips serverless tools); read tools declared side-effect-free;
  beacons stamped `integrations`; forwarding over `CallIntegrationTool`.
- **Orchestrator runtime service** — connection-change notifications reusing
  the MCP registry-changed path; `beaconServerName` mapping.
- **Client** — the Integrations page shows each connection's tools with the
  new policy editor and the connect-time "sends will ask" notice; the egress
  dialog lists integration endpoints beside remote MCP servers; the
  automations editor states integrations are unavailable. Compact layout
  (<840px) included.

## The tests that gate the merge

Adapt names to the implementation; every assertion must survive. For 1–8,
break the production gate, watch the right test fail, restore.

1. **Credential hygiene, including transport failure.** After a full call
   cycle — an HTTP-status failure *and* a dial/TLS-level failure (the
   `*url.Error` path that embeds URLs in error strings) — the plaintext
   credential appears in no event payload, audit row, log line, tool result,
   or error string.
2. **One stack frame.** The unseal happens exactly once per call (counter on
   the sealer) and no long-lived structure retains the plaintext between
   calls.
3. **No decision, no dial — on all legs.** Refused before any network I/O,
   asserted with a transport that fails the test if touched: a tool name
   absent from the decision's `selected_tools`; a decision missing
   `TOOL_ARGUMENTS` or `TOOL_RESULTS`; an `(endpoint, connection_id)` pair
   absent — specifically, a decision naming connection A refuses a call
   naming connection B on the same host.
4. **The signed challenge binds the endpoints.** A prepared challenge whose
   integration endpoints are altered between prepare and send is refused by
   the payload-match check.
5. **Writes consume exactly once, bound to the connection.**
   `github.create_comment` without a valid unconsumed approval is refused;
   with one, consumed exactly once; an args mismatch — including only
   `connection_id` differing — is refused.
6. **Revalidation and liveness.** The run is cancelled (or the tool
   disabled, or the session enters deletion) during the approval wait ⇒ no
   dispatch, transport untouched.
7. **Revocation and deletion mean now.** A connection revoked after consent
   fails the next call closed; a connection *deleted* (the separate code
   path) likewise; the sealed-blob accessor returns status in the same read.
8. **Policy round-trip.** Tools registered through `UpsertTools` land
   `approval_required`; set `safe` ⇒ a read dispatches with no approval; set
   `disabled` ⇒ refused. This is the test that a
   hardcoded-policy, never-registered implementation cannot pass.
9. **The happy path exists.** A local-Ollama run offered GitHub tools gets a
   non-empty egress disclosure, and with consent granted completes a mocked
   `github.list_issues` end to end. Every other test is refusal-shaped; this
   is the one that proves the feature works for local models at all.
10. **A failed read is not a dead run.** A provider 500 on an approved read
    returns a bounded tool error to the model; the run continues. A
    transport failure on a write still halts the run.
11. **Transport hardening.** A 30x from the provider is refused before the
    redirect is followed; a resolver answer mapping the provider host to a
    private address is refused.
12. **No ambient egress, automations included.** A run offered no
    integration tools has no integration endpoints in its decision; an
    automation run's integration call is refused (no decision exists); an
    integration tool on an automation allowlist is rejected at save.
13. **Bounded, framed, spoof-resistant results.** An oversized response is
    truncated to `maxIntegrationResultBytes` with the truncation announced;
    a body containing the framing delimiters cannot close the frame.

## Deferred, deliberately

- **IMAP, CalDAV, Notion providers** — after GitHub proves the path. The
  endpoint-bearing providers additionally wait on the tightened endpoint
  validation being exercised for real.
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

- `docs/mcp-security-and-integration.md` — integrations join the caller-side
  enforcement section; the credential-handling story (sealed at rest, one
  stack frame in flight, header-only transport, hardened dialer, never in
  events or logs) written down.
- `docs/architecture/remote-egress-policy.md` — integration endpoints named
  as the third egress path beside remote model providers and remote MCP
  servers; the every-send-asks consequence and the endpoint-not-repo
  granularity sentence.
- `docs/VISION.md` — three edits, no invariant weakened: the Integrations
  section's "no tool consumes a connection … nothing dials anywhere"
  rationale is rewritten to describe the gates that replaced it; the
  caller-side qualification on the approval invariant, added for the MCP
  registry, extends to integrations; and the untrusted-input invariant gains
  retrieved integration content as the sibling of skill text.
