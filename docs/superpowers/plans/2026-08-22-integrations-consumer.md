# Integrations Consumer Implementation Plan

Give the model tools that act on connected accounts. Integrations today is a
vault with no customers: users connect IMAP, CalDAV, Notion, and GitHub
accounts, the credential is AES-256-GCM-sealed under `TURING_INTEGRATION_KEY`,
consent is recorded, revocation works — and no code path ever *opens* a
credential to do anything. The consent screen tells the user what the
credential grants; nothing exercises a single grant.

The interesting part is not the tools. It is that an integration call is the
first tool whose *normal, read-only operation* is egress, and whose results
are someone else's words entering the model's context. This plan says which
existing gate covers each of those, and adds no new gate.

## The problem, stated precisely

`schema/0008_integrations.sql` stores sealed credentials. The repository can
create, list, revoke, and delete a connection
(`internal/repository/integrations.go`) — there is deliberately no method that
returns a plaintext credential, and today nothing needs one.

The moment something does, three things are at stake:

1. **"Nothing leaves the machine by default."** Calling `api.github.com` sends
   model-authored arguments off the machine. Every integration call — reads
   included — is egress in exactly the sense TUR-003 regulates.
2. **"Every mutation is approved, argument-bound, and single-use."** Creating
   an issue comment or sending mail is a mutation at a provider that cannot
   verify our approval token. This is the third-party-MCP problem again, and
   it has the same honest answer.
3. **Retrieved content is untrusted input.** An issue body or an email is
   written by an arbitrary third party and lands in the model's context. No
   existing invariant names this; this plan does.

## Design decisions (locked)

**The consumer lives in the orchestrator. Not a new MCP server, not the
runtime.** Our MCP containers have no internet egress, by design and by
compose file — an integrations container would void that boundary quietly.
The runtime never holds `TURING_INTEGRATION_KEY`; giving it credentials would
widen the exposure surface for no gain. The orchestrator already holds the
sealer and already dispatches third-party calls on the runtime's behalf
(`CallRegisteredMcpTool`, PR #73). Integration tools take the same shape: the
runtime forwards the call over internal gRPC, the orchestrator unseals the
credential *inside the provider-client call*, performs the HTTPS request, and
returns a bounded result. The plaintext credential exists in one stack frame.
It never appears in tool args, tool results, events, audit rows, logs, error
strings, or the model context.

**`integrations` is a pseudo-server like `skills`, not a registry entry.**
The 0016 triggers already carve out `server_name = 'skills'` for tools with no
MCP server behind them. Migration `0017` widens that carve-out to
`'integrations'`. These tools do not appear on the MCPs page — they appear on
the Integrations page, next to the connection they act for, because that is
where the user reasons about them.

**Every integration call joins the per-run egress decision. No new consent
mechanism.** PR #73 settled the pattern: a remote MCP server does not get its
own egress flag; it joins `run_egress_decisions` (`remote_mcp_servers_json`),
and `mcpregistry/call.go` refuses any call the decision does not cover, then
revalidates at dispatch. Integrations reuse that shape verbatim: the decision
gains `integration_endpoints_json` (provider host + connection id + tool
names), `PrepareRemoteEgress` includes the integration endpoints a run may
reach, the egress dialog lists them, and the orchestrator refuses an
integration call whose endpoint the run's decision does not name — before any
network I/O. The 0016 CHECK that lets a local-Ollama run hold a decision row
only when it has remote MCP servers widens to "remote MCP servers or
integration endpoints".

**Writes are enforced caller-side, and the guarantee is stated, not
substituted.** GitHub will execute whatever request arrives with a valid
bearer; it cannot verify our approval JWT. So the orchestrator is the
enforcement point: a mutating integration tool dispatches only against a
valid, unconsumed, argument-bound approval, which the orchestrator consumes
itself — the same narrower guarantee `docs/mcp-security-and-integration.md`
already documents for third-party MCP servers, holding because the
orchestrator is the only path to the credential. The approval-consumer
identity (`TURING_APPROVAL_CONSUMER_TOKEN`) is not involved and nothing new
learns it.

**Everything defaults to `approval_required`, reads included.**
`DefaultPolicyFor` seeds nothing for `integrations`; unknown tools already
fall to `approval_required`, and no new defaulting logic is added. A user who
wants friction-free reads relaxes them per-tool in the existing policy editor
— a deliberate decision on a named tool, not a default we chose for them.
Egress consent still applies either way; policy and egress are different
gates and neither substitutes for the other.

**Retrieved content is data.** Results come back as bounded excerpts, framed
as retrieved third-party content. This framing is hygiene, not a security
boundary — the boundary is that anything the model *does* in response still
passes the same approval and egress gates as ever. The plan claims nothing
stronger, in the code or the docs.

**A call names its connection.** Tool arguments carry `connection_id`; the
tool schema the model sees enumerates the live connections for that provider.
Two GitHub accounts are two distinguishable destinations, and the egress
decision records which one. A revoked or deleted connection fails the call
closed mid-run — revocation means *now*, not at the next session.

**Phase one is GitHub, alone.** One provider proves the entire path:
unseal-per-call, egress coverage, read tools, one write tool through the
caller-side approval flow. Four tools: `github.list_issues`,
`github.get_issue`, `github.get_file`, `github.create_comment`. IMAP and
CalDAV are deferred — different wire protocols, zero architectural novelty,
and IMAP drags in parsing arbitrary MIME, which deserves its own review.
Notion is mechanical after GitHub.

## What gets built

- **Migration `0017`** — widen the 0016 tool triggers to allow
  `server_name = 'integrations'`; rebuild `run_egress_decisions` (the 0016
  rename-copy-drop pattern) adding `integration_endpoints_json` with the
  widened local-provider CHECK. `internal/db/migrations_test.go` pins the
  list; update it.
- **`internal/service/integrations` (orchestrator)** — the GitHub provider
  client; per-call unseal via a deliberately scoped helper that never returns
  the plaintext to a caller outside the package; egress-decision validation
  and revalidation modeled on `mcpregistry/call.go`; caller-side approval
  consumption for writes; response bounding.
- **Runtime** — offer integration tools to the model only when at least one
  live connection exists for the provider; forward calls over a new
  `CallIntegrationTool` RPC (a separate RPC, so the MCP registry service
  stays about MCP servers).
- **Egress** — `PrepareRemoteEgress` request/response and the frozen decision
  carry integration endpoints; the client egress dialog lists them alongside
  remote MCP servers.
- **Client** — the Integrations page shows each connection's tools with the
  existing per-tool policy editor; the egress dialog gains the provider
  hosts. Compact layout (<840px) included.

## The tests that gate the merge

Adapt names to the implementation; every assertion must survive. Break the
production gate for 1–4, watch the right test fail, restore.

1. **Credential hygiene.** After a full call cycle — including a *failing*
   call — the plaintext credential appears in no event payload, audit row,
   log line, tool result, or error string. This is the test to write first,
   and the error path is the half that catches real leaks.
2. **No decision, no dial.** An integration call whose endpoint (or
   connection) is absent from the run's egress decision is refused before any
   network I/O — asserted with a transport that fails the test if touched.
3. **Writes consume exactly once.** `github.create_comment` without a valid
   unconsumed approval is refused by the orchestrator; with one, it is
   consumed exactly once; an argument mismatch against the approval's bound
   args is refused.
4. **Revocation means now.** A connection revoked after the run's egress
   decision was granted fails the next call closed.
5. **No ambient egress.** A run offered no integration tools produces no
   integration endpoints in its decision, and a run that merely *could* call
   GitHub but was denied consent dispatches nothing.
6. **Bounded, framed results.** An oversized provider response is truncated
   to the bound and delivered framed as retrieved content; the bound is a
   constant with a test, not a hope.

## Deferred, deliberately

- **IMAP, CalDAV, Notion providers** — after GitHub proves the path.
- **OAuth flows** — Integrations takes credentials the user minted at the
  provider, by design; a redirect-based flow is its own project.
- **Provider-call budgets and rate limiting** — telemetry already counts tool
  calls; enforcement is future work and saying otherwise here would be a lie.

## Documentation the implementation PR must update

- `docs/mcp-security-and-integration.md` — integrations join the caller-side
  enforcement section; the credential-handling story (sealed at rest, one
  stack frame in flight, never in events or logs) written down.
- `docs/architecture/remote-egress-policy.md` — integration endpoints named
  as the third egress path, beside remote model providers and remote MCP
  servers.
- `docs/VISION.md` — no invariant changes. "Nothing leaves by default" holds
  via the per-run decision; the approval invariant's caller-side
  qualification, added for the MCP registry, now also covers integrations —
  extend that sentence rather than adding a new one.
