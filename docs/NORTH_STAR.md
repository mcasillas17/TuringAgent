# TuringAgent North Star and Roadmap

**Status:** Canonical product direction and implementation backlog.

**Inspected baseline:** `main` at `fcaf6062`, inspected 2026-10-02. Seven of
the first ten tasks below have shipped; PROV-001, SKL-001 and SEC-001 remain.

**MCP protocol review:** Added 2026-10-15 against `main` at `09d3d5dc`.
The MCPP adoption plan does not change the shipped capability inventory.

**Agent team:** Updated 2026-10-25 against `main` at `4148cd92`, after the
specialist team landed in PRs #150, #152–#158, #162, #163 and #167
([design](superpowers/specs/2026-10-03-turing-orchestrator-agent-team-design.md)).
Its roadmap amendments are applied to AGT-001 through AGT-004, WEB-001, AZ-001
and the priority spine below. Delegation is experimental and off by default.

**Local inference and operator UX:** Reconciled 2026-10-31 against `main`
at `57461e49`, with OPS-001 PR 3. Local engine selection and app labels shipped
in #169. OPS-001 is complete: #171 adds service `check-config`; #173 adds the
host `turing` command for status, lifecycle, connection and validated
configuration changes; OPS-001 PR 3 adds `turing setup`,
`turing engine detect | use | check` and the runtime's `check-engine`.
OPS-002/003 remain pending. See the [current architecture and flows](architecture/system-flows.md) and
[local inference engines](architecture/local-engines.md).

**Supersedes:** `docs/VISION.md` and
`docs/architecture/2026-08-18-personal-agent-audit.md` as active roadmap
documents. Those paths remain historical pointers; the earlier records are
available in Git history.

## North star

TuringAgent is a home-hub personal agent: a trusted local kernel that remembers
what the user accepts, acts only through explicit policy and argument-bound
consent, and reaches models, tools, agents, devices, and channels without
surrendering data ownership.

Learning, downloaded capabilities, and generated code remain inert,
reviewable candidates until the user grants them power. Turing should become as
smart and customizable as Hermes Agent by adopting portable files, progressive
disclosure, evaluation, delegation, and learning loops - not by copying
unsandboxed host access or broad messaging-gateway defaults.

The product promise is:

> Turing gets more useful as it learns who you are, but never more powerful
> than the permissions you knowingly gave it.

## Owner decisions

These decisions are part of the roadmap, not implementation suggestions:

1. Turing is single-user and home-hub-first. Multi-tenant cloud hosting is not
   on this roadmap.
2. The orchestrator remains the sole owner of durable state, policy, approvals,
   egress decisions, audit, and lifecycle truth.
3. Mobile devices, channel gateways, MCP hosts, and remote agents are
   revocable clients of the home hub, never additional policy authorities.
4. Local models remain the default. Remote models are optional inference
   providers under per-run disclosure.
5. Model providers, MCP tools, A2A agents, ACP editor clients, account
   integrations, and messaging channels are separate trust planes.
6. Automatic learning proposes changes. It never silently changes active
   memory, persona, skills, tools, or policy.
7. Unsandboxed host shell access, self-installation, self-enablement, and
   self-escalation are barred.
8. The native desktop loopback path remains supported. Strong device identity
   and TLS are mandatory for non-loopback access.

## Why Hermes Agent is the benchmark

The benchmark is the current
[NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent)
project, not the Hermes model family. Its official repository and documentation
describe a model-agnostic agent with a terminal UI, many providers, scheduled
automations, messaging gateways, subagents, MCP, persistent memory,
agentskills.io-compatible skills, autonomous skill creation, and multiple
sandbox backends.

Hermes demonstrates what makes an agent feel capable:

- it can be reached where the user already works;
- it can use many models and tools;
- it remembers across sessions;
- it turns successful work into reusable skills;
- it delegates and parallelizes;
- it has a broad customization vocabulary.

Turing's advantage is the control plane Hermes does not make its defining
product claim:

- short-lived, argument-bound, single-use approvals;
- a run-owned, signed remote-egress decision;
- sandboxed bundled file tools;
- service-scoped internal identities;
- durable leased and fenced execution;
- user-owned memory files with explicit promotion;
- source withdrawal and provenance;
- untrusted framing for retrieved content and skills.

The strategy is therefore **trusted kernel plus untrusted adapters**. Feature
parity cloning would import too much authority. A cloud-first control plane
would weaken the local-first promise. The trusted-kernel approach lets Turing
adopt Hermes's useful loops while preserving Turing's stronger boundaries.

## Current status

### Guarded capability inventory

These explicit status cells are checked against repository wiring, contracts,
and regression-test witnesses by `go test ./tools/docs -count=1`. `shipped`
means the bounded capability in **Scope**, not every possible interpretation
of its name. `pending` means that capability is not delivered, even when a
related UI or substrate exists. Static wiring checks and an isolated codegen
probe are not live end-to-end product proof; see
[coverage, limitations and update procedure](../tools/docs/README.md).

<!-- status-guard:begin -->
| Claim | Status | Scope |
| --- | --- | --- |
| proto-breaking | shipped | TUR-019: CI runs Buf compatibility checks against the PR base, separately from generation. |
| proto-codegen | shipped | CI checks deterministic, committed Go and Dart protobuf output. |
| flutter-search | shipped | Exact-phrase shell search calls the backend RPC. Selecting a hit sets the conversation shown after search is dismissed; no date filter is provided. |
| flutter-workspace | shipped | Chats, Skills, Memory, Integrations, MCPs, Automations, Agents and Telemetry have backend-connected surfaces; this is not full support for every advertised provider or protocol. |
| explicit-cancel | shipped | CXL-001: authenticated, idempotent exact-run cancellation and Flutter Stop distinguish durable acceptance from execution reconciliation; completed effects are not rolled back. |
| mcp-registry | shipped | Registration, import, enablement, token rotation and tool-policy management. Protocol conformance is the separate `mcp-lifecycle` row. |
| mcp-lifecycle | shipped | CON-001: MCP 2025-11-25 over Streamable HTTP — initialization, version negotiation, advertised capabilities, tools/list and tools/call on both client paths and both bundled servers, with cursor pagination on the client side and ping on the runtime client and both bundled servers — the client's ping is protocol surface exercised by the conformance fixtures, not the mechanism container liveness uses. The bundled servers are sessionless, answer tools/list in one page and refuse a cursor, and only mcp-files cancels an in-flight request. No resources, prompts, sampling, elicitation or stdio. |
| remote-model-routing | shipped | The conversation's destination bar selects a route through ExternalAgentService; the runtime calls its OpenAI-compatible model endpoint under per-run disclosure. |
| agent-delegation | pending | No A2A delegation or connection to an existing Claude, Copilot, Gemini or ChatGPT product conversation. |
| github-tools | shipped | Connected GitHub credentials support issue listing/reading, file reading and issue comments under egress and approval policy. |
| other-integration-tools | pending | IMAP, CalDAV and Notion have no tool consumers and refuse new credentials; earlier-release rows remain for explicit local cleanup. INT-001 shipped this boundary. Google, Microsoft and Slack account connections are not implemented. |
| mobile-client | pending | iOS/Android scaffolding and responsive layouts are not production mobile support; the main Android manifest lacks INTERNET permission, while debug/profile manifests grant it. |
| mobile-reachability | pending | Compose publishes on 127.0.0.1; changing the client URL to a LAN/tailnet address does not expose it or add device identity/TLS. |
<!-- status-guard:end -->

### Additional shipped capabilities

The guarded inventory above owns the selected status claims. The following
tables describe additional capability scope, not a second inventory of those
claims.

| Capability | Current state |
|---|---|
| MCP lifecycle | Turing speaks MCP revision `2025-11-25` over the Streamable HTTP transport in both directions: the two bundled servers answer `initialize`, `notifications/initialized`, `ping`, `tools/list` (one page; a cursor is refused), `tools/call` and `notifications/cancelled`, and both client paths negotiate before any tool call and page through a third-party server's cursors. Bundled servers are sessionless, and only mcp-files cancels in-flight work; resources, prompts, sampling, elicitation, OAuth and stdio are out of scope. |
| Local chat and tool loop | Ollama is the default; streaming, bounded tool iterations, zero-argument recovery, and unknown-tool recovery are implemented. |
| Pluggable local inference | #169 adds `TURING_LOCAL_ENGINE=ollama` or `openai` for a same-machine engine, configured through `TURING_LOCAL_*`. The app names its engine and model; the local wire identity remains `ollama`. Local inference needs no remote-model consent, but remote tools still require their own disclosure. OPS-001 adds hardware-guided engine selection: `turing engine detect` recommends an engine for the machine, `turing engine use` saves the choice, and `turing engine check` proves it from inside the runtime ([design](superpowers/specs/2026-10-25-turing-local-inference-engines-design.md); [guide](architecture/local-engines.md)). |
| Host operation (OPS-001) | A user-run `./turing` command provides guided `setup`, status, start/stop/restart, logs, connection/key rotation, validated `.env` configuration, and `engine detect \| use \| check`. It checks image support before running a service's `check-config` or the runtime's `check-engine`, writes atomically after validation, and offers apply/restore. `check-engine` is the one check that uses the network, only to reach the configured local engine, and sends a fixed prompt with no user data. It never opens SQLite or gives Docker access to the agent. |
| Durable orchestration | Sessions, runs, events, jobs, leases, fencing, retries, recovery, and reopenable run outcomes are persisted in SQLite. |
| Bounded queue waiting | A queued run records whether any live worker can serve its route, and two configurable bounds end one that waits too long. Not a pause/resume state: both outcomes are terminal. |
| File actions | Sandboxed read/list/search plus preview-bound create/update are implemented; both reviewed file versions must be safe text within 64 KiB. |
| Approvals | Authenticated bounded details bind human decisions to canonical arguments and file preconditions; stale writes are refused under the documented filesystem assumptions. |
| Session management | Stable titles, pagination, search, rename, archive, restore, and durable whole-session withdrawal are implemented. |
| Recall | Cross-session FTS5 recall and scored search hits are implemented and attributed. |
| Recall evaluation | MEM-003 provides a deterministic synthetic baseline for search, runtime selection and budgeted request evidence, with independent safety gates; it does not judge model answers. |
| Context control | Provider context limits, output reserves, explicit omissions, and terminal notices are implemented. |
| Memory vault | `persona.md`, `profile.md`, `inbox/`, and `beliefs/` are user-owned Markdown; `memory.search`, `memory.read`, and `memory.remember` exist. |
| Skills | File-backed `SKILL.md` loading, progressive disclosure, enablement, and Turing capability grants exist. |
| Automations | Interval and daily unattended runs with per-automation tool allowlists exist. |
| Specialist team (experimental) | File-defined specialist profiles (Dev, Inbox, Research) with revision-bound grants. With `TURING_AGENT_TEAM_ENABLED=true` (default `false`), Turing delegates bounded tasks to them: each runs in a hidden child session with its own frozen tool set and approvals, asks the user before using a tool that leaves the machine, stops with its turn or conversation, and returns its result for Turing to answer from. The app shows each task as a card. Runs execute one at a time, and automations cannot delegate. |
| Audit and telemetry | Redacted audit read APIs and local-only usage aggregation exist. |

The configuration-driven registry scope originally named `CON-002` is treated
as shipped by the registry, import, enablement, token-rotation, and policy work
merged in PRs #73, #81, and #82. Dependencies on `CON-002` below are therefore
satisfied. `CON-001` is now shipped as well: the bounded MCP lifecycle
described below landed after that registry, so "MCP server" in this repository
means a peer Turing initializes and negotiates with, not only an HTTP endpoint
it posts `tools/list` to.

Other stable dependencies referenced by pending tasks are already satisfied:
`TUR-001` (idempotent sends), `TUR-004` (session withdrawal), `TUR-006`
(service-scoped identities), and `MEM-001` (memory governance and derived-state
contract) are shipped on the inspected baseline.

### Partial or misleading today

| Capability | Honest status |
|---|---|
| Personal learning | The vault and proposal workflow exist, but normal conversation does not automatically extract candidates, revise beliefs, or measure learning quality. |
| Skills ecosystem | The parser accepts a Turing-specific subset. It rejects several standard Agent Skills fields and has no import, quarantine, authoring, or marketplace flow. |
| Audit UX | The API exists; a user-facing audit viewer does not. |
| Multi-agent | Specialists are file-defined profiles under the one `AgentId` and run on the same general executor. Delegation is experimental and off by default until EVAL-001, tasks run one at a time (concurrency one), and specialists cannot ask each other. |

### Not implemented

- automatic memory extraction and belief revision;
- per-session and per-turn memory controls;
- message-level deletion and single-fact withdrawal;
- a model-answer and whole-personal-agent evaluation suite (EVAL-001);
- native Anthropic and Gemini provider transports;
- secure mobile reachability and per-device identity;
- Telegram, Slack, WhatsApp, or Teams channels;
- a conformant public or private Turing MCP server;
- A2A client/server behavior or ACP editor integration;
- agent-authored skills or tools;
- sandboxed executable tool generation;
- default-on delegation, parallel specialists, and specialist-to-specialist
  requests (EVAL-001, AGT-000 and AGT-004);
- consistent export, backup/restore, bounded retention, and database encryption;
- evidence-gated vision, voice, and richer prospective memory.

## Envisioned scenarios: what works now

These examples illustrate the [guarded capability inventory](#guarded-capability-inventory),
which owns the corresponding status claims. Update these explanations with that
inventory; the example wording itself is not machine-checked.

| Scenario | Status | Boundary |
|---|---|---|
| "Find the exact phrase 'release checklist'." | Works | FTS5 phrase search; selecting a hit sets the conversation to view after dismissing search. |
| "Read this project file." | Works | File must be inside the sandbox. |
| "Update this sandbox file." | Works within preview limits | Requires a current argument/preview-bound approval; stale, redacted, binary or oversized changes fail closed. |
| "Remember that I prefer concise answers." | Partial | The model may call `memory.remember`; the proposal is inert until the user promotes it. There is no automatic extraction. |
| "Comment on this GitHub issue." | Works with setup | Requires a connected GitHub credential, per-run egress coverage, and tool approval. |
| "Run this report every morning." | Partial | The automation can run, but there is no mobile/channel delivery and unattended tools are limited to its explicit allowlist. |
| "Ask Claude/Gemini/ChatGPT instead." | Partial | A compatible remote model endpoint can answer under disclosure; this is model inference, not a conversation with another agent product. |
| "Delegate this coding task to Copilot." | Does not work | No vendor delegation adapter or A2A peer exists. Turing's own specialists (experimental) are local profiles, not other agent products. |
| "Use Turing from my phone." | Does not work safely | UI scaffolding exists; secure remote identity and reachability do not. |
| "Message Turing in Slack or Telegram." | Does not work | No channel gateway exists. |
| "Learn this workflow and make a skill." | Does not work | Skills are read-only to the agent. |
| "Build a new tool for yourself." | Does not work | No generated-code quarantine or executable sandbox exists. |

## Target architecture

```text
 Native desktop      Paired mobile       Channel adapters      Local MCP/ACP hosts
 (loopback bearer)   (device identity)   (untrusted clients)   (scoped clients)
        |                   |                    |                    |
        +-------------------+--------------------+--------------------+
                                    |
                         consent and policy membrane
                                    |
       +----------------------------------------------------------------+
       |                    Go orchestrator / home hub                   |
       | sessions | jobs | leases | runs | events | approvals | egress |
       | audit | telemetry | identities | routing | memory provenance    |
       +-----------+----------------+----------------+-------------------+
                   |                |                |
             leased workers    tool adapters    integration adapters
                   |                |                |
        local/remote models   sandboxed MCP     GitHub/OAuth services
                   |
        +----------+-----------+
        |                      |
  memory candidates      skill/tool candidates
  user promotes          user reviews and grants
```

Every new capability is an arrow through the membrane, never a bypass around
it.

### Trust planes

1. **Inference providers:** Generate model output. They do not become agents or
   receive standing access to the vault.
2. **MCP tools:** Offer resources or actions. Descriptions and annotations are
   untrusted; Turing policy remains authoritative.
3. **A2A peers:** Exchange bounded tasks and artifacts. They do not share
   internal memory, policy, or raw tool access.
4. **ACP clients:** Embed a coding-agent experience in an editor. ACP is not
   A2A and does not replace the Flutter protocol.
5. **Account integrations:** Hold service credentials and expose reviewed
   provider-specific tools.
6. **Channels:** Carry messages through a third-party platform. The platform
   can see those messages.
7. **Native devices:** Paired clients that can be trusted for approval only
   after device-bound authentication.

## Permanent invariants

### Privacy and egress

- Nothing leaves the machine on the default path.
- Every off-machine model, tool, agent, channel, or integration destination is
  named in policy and audit.
- Interactive remote inference remains run-owned and one-time; enabling a
  provider, channel, or integration is not consent to send a run.
- Sensitive memory and context require explicit data-scope policy before
  remote use.
- A third-party channel is not end-to-end encrypted to Turing. Product text
  must say that the platform receives the message.

### Identity and authorization

- Loopback desktop compatibility remains supported.
- Every non-loopback native client has a device identity, scoped credential,
  revocation record, and authenticated encrypted transport.
- Tailnet or LAN membership is not authentication.
- Unbound channel identities have zero capability. Rejected attempts produce a
  bounded content-free audit event and no model run.
- Channel buttons are not trusted approval devices. Initial remote approvals
  happen only in a paired native Turing client.

### Actions and tools

- Mutations require policy evaluation and an argument-bound, single-use
  approval.
- Automation pre-approval is scoped to one automation and exact
  `(server, tool)` pairs; no wildcard exists.
- Bundled file tools remain sandbox-confined.
- Third-party servers are clearly labeled when Turing cannot guarantee their
  confinement.
- Unsandboxed host shell access is barred.
- Stdio MCP and install-time `npx`/bootstrap execution remain refused.

### Learning and customization

- Persona is human-authored. No agent path writes it.
- Memory extraction creates candidates only.
- Imported or generated skills are inert until review, enablement, and
  capability grants.
- Capability grants allow skill text to load; they never authorize a tool.
- Agent Skills `allowed-tools` is metadata, never Turing policy.
- Generated executable code never loads into the orchestrator process.
- Executable candidates run only in an ephemeral, resource-bounded sandbox
  with no direct network. The only communication path is a brokered internal
  HTTP/gRPC transport; any off-machine request is performed by an
  orchestrator-controlled egress tool under its own destination policy.

### Durable truth

- The orchestrator owns state, lifecycle, leases, retries, and policy.
- A client or adapter renders protocol truth; it does not invent lifecycle.
- Derived state cannot outlive its source except where explicit user promotion
  makes the promoted file the user's authored artifact.
- Deletion claims logical withdrawal, not forensic erasure.
- Every new background writer states and tests its SQLite transaction budget.
- The backend remains Go. No framework or second-language runtime may own
  orchestration.

## Secure reach and channels

### Native mobile

The preferred architecture keeps the home hub authoritative:

1. The desktop creates a short-lived pairing challenge.
2. The mobile app creates a device key and proves possession during pairing.
3. The orchestrator stores the public identity, allowed scopes, and revocation
   state, never a reusable pairing secret.
4. Non-loopback requests use an authenticated encrypted transport and a scoped
   device token.
5. A private overlay may expose the loopback service through a Serve-class
   reverse proxy. Merely entering a LAN or tailnet IP is insufficient.
6. A future optional relay stores only opaque encrypted envelopes and cannot
   mint identity, decrypt content, or grant capability.
7. Push notifications contain wake-up metadata only; clients fetch encrypted
   content from the paired transport.

Binding Compose to `0.0.0.0` or enabling a public tunnel by default is not an
acceptable mobile implementation.

### Messaging channels

Channel adapters run as separate, service-scoped clients:

- Telegram begins with Bot API long polling.
- Slack begins with Socket Mode.
- Both make outbound connections from the home hub and need no public request
  URL.
- Each external account is paired to the owner from a native Turing client.
- Channel sessions start with no memory, no skills, and no tools. The user may
  grant narrower scopes later.
- Inbound platform IDs, update IDs, envelope IDs, and timestamps feed bounded
  replay/idempotency checks.
- A durable delivery ledger labels ambiguous retries instead of silently
  duplicating replies.
- Approvals are completed in the paired native app.
- Revocation disables the adapter, invalidates its Turing credential, and
  tells the user when a vendor-side credential must also be revoked.

WhatsApp Cloud and Teams require public webhook infrastructure. They follow
only after the blind-relay task proves authentication, replay protection,
bounded buffering, encrypted payload custody, deletion, and failure behavior.
Their vendors still see channel content; the relay does not change that fact.

## Safe learning and self-customization

### Memory learning loop

```text
 completed run
      |
 local extractor reads user-authored evidence only
      |
 measured candidate with provenance and sensitivity
      |
 inbox (inert)
      |
 user edits / promotes / rejects
      |
 active belief with revision and withdrawal links
```

Extraction never delays or fails the original run. Assistant text, recalled
text, tool output, channel content from other people, and downloaded content
cannot become an active user belief. Contradictions create a reviewable
supersession proposal, not an overwrite.

### Skill learning loop

```text
 explicit "teach this" or repeated successful workflow
      |
 agent drafts an Agent Skills-compatible bundle
      |
 quarantine + provenance + static scan
      |
 optional sandbox verification with intercepted mutations
      |
 user reviews full diff and capability request
      |
 install disabled -> grant -> enable
```

The live skill tree is never the agent's scratch space. Imported packages keep
source identity, content hashes, scanner version, requested capabilities, and
review decisions. Updating a skill produces another reviewable diff.

### Tool and plugin learning loop

Generated tools are MCP-compatible out-of-process packages, not Go plugins:

1. The agent writes source into a quarantined project.
2. A static manifest declares language, build inputs, resources, filesystem
   mounts, requested remote destinations, secrets, and tools. A requested
   destination is review metadata, not direct sandbox network access.
3. Builds and tests run in an ephemeral sandbox with no network by default.
4. Results, diffs, and requested capabilities are shown to the user.
5. Installation creates a disabled server registration.
6. The user separately grants server capability, tool policy, and any egress.
7. Runtime calls cross the normal orchestrator policy and approval path.

Marketplace content never receives orchestrator credentials or the bundled
approval-consumer identity.

## Marketplace and interoperability strategy

### Agent Skills

Turing will implement a compatibility profile for
[agentskills.io](https://agentskills.io/specification):

- standard `name`, `description`, `license`, `compatibility`, and `metadata`
  are accepted as untrusted metadata;
- Turing's capability requests remain a separate extension and grant store;
- `allowed-tools` is displayed but never honored as authorization;
- scripts, assets, and references can be imported, but scripts stay inert
  unless a later executable-tool review promotes them;
- bundles are quarantined, hashed, reviewed, and installed disabled.

### MCP

CON-001 shipped the bounded **2025-11-25** tool surface: initialization,
capability negotiation, ping, paginated client-side tool listing and tool
calls. This is legacy-era interoperability, not support for every current MCP
feature. The guarded inventory above retains that precise shipped scope.

The current published specification checked on 2026-10-15 is **2026-07-28**.
Its stateless request model and optional official Tasks extension change how
clients call tools and wait; the older experimental Tasks API is not a
compatible substitute. [MCP protocol evolution](#mcp-protocol-evolution) tracks
the staged migration, with the full [research and adoption
plan](architecture/mcp-protocol-adoption.md). All MCPP tasks remain pending.
Modern discovery must preserve legacy peers, and durable external waits must
preserve Turing's approvals, egress ownership and worker fencing.

Registry discovery is read-only. Namespace verification is not security
review. Any discovered server is imported disabled and approval-required.
Resources, prompts, sampling, elicitation, OAuth and stdio are not silently
enabled by a protocol revision change.

### Connecting to Claude, Copilot, Gemini, and ChatGPT

- **As models:** native Anthropic and Gemini adapters join Ollama and the
  generic OpenAI-compatible adapter. This is inference, not delegation.
- **As local MCP hosts:** an opt-in loopback Turing MCP server can expose a
  bounded, user-granted surface to local applications. Every client has a
  scoped identity and every disclosure is audited.
- **As remote MCP hosts:** a separate later task requires the blind relay,
  per-host grants, and explicit signed egress for tool arguments and results.
  Turing never publishes a direct public MCP port.
- **As A2A peers:** Turing first learns to call a registered A2A peer with a
  bounded task envelope. An inbound A2A server is optional, disabled by
  default, identity-bound, and maps each task to a normal Turing run.
- **As editor clients:** ACP is an optional editor adapter and remains
  separate from A2A.

Vendor products that do not expose an applicable protocol cannot be treated as
agents merely because their model API is available.

## Delivery model

Every roadmap item is an independently deliverable implementation contract. A
task is not complete because a design exists or a UI renders a placeholder. It
is complete only when its stated behavior and acceptance evidence are present
in `main`.

The **first ten** below are the only serial near-term queue. The later
workstreams are thematic, not a competing sequence. After the first ten,
independent tasks may proceed in parallel when their dependencies and entry
gates are satisfied.

The MCPP track is independently scoped protocol maintenance and optional
capability work. Its legacy-hardening and modern-interoperability slices may
proceed when their own dependencies are met; they do not wait on native model
providers or mobile work. Long-running task execution remains behind its
separate lifecycle, visibility and evaluation gates.

## First ten implementation tasks

### 1. DOC-001 - Canonical truth and status guard

- **Implementation status:** Shipped on `main` in #123 (`b3e3e352`). Active
  setup/status guidance is corrected; `tools/docs` checks the explicit
  inventories in this file and the Flutter README against named implementation
  evidence. Isolated fixtures cover false shipped/pending claims and missing or
  changed evidence. [Guard maintenance](../tools/docs/README.md) defines bounded
  assurance and probe limits. DOC-001 does not change application behavior.
- **Outcome:** Product documentation cannot silently drift from merged reality.
- **Scope:** Make this file canonical; mark old roadmap documents historical;
  correct current status, integration labels, remote-model naming, mobile
  claims, and MCP terminology; add a lightweight repository test that pins
  status claims to concrete files or task markers.
- **Likely files:** `docs/NORTH_STAR.md`, `docs/VISION.md`,
  `docs/architecture/2026-08-18-personal-agent-audit.md`,
  `turing-client/turing_app/README.md`, `tools/docs/`.
- **Acceptance:** Stale claims about protobuf checks, Flutter search,
  placeholder pages, full MCP conformance, and mobile reachability are gone;
  a fixture proves the drift guard fails on a false shipped/pending claim.
- **Dependencies:** None.

### 2. INT-001 - Powerless-credential honesty

- **Implementation status:** Shipped on `main` in #122 (`c75ffc44`).
- **Outcome:** Turing does not solicit or store new credentials for providers
  without functional tools. Credentials saved by earlier releases remain until
  the user explicitly revokes or deletes their connections.
- **Scope:** Keep enum wire compatibility; mark IMAP, CalDAV, and Notion
  descriptor-only/unsupported until tools ship; reject new connections; retain
  existing rows for explicit revoke/delete without attempting use; make the UI
  explain the transition.
- **Likely files:** `proto/turing/v1/integrations.proto`, integration provider,
  service and repository packages, Flutter integrations page, migration tests.
- **Acceptance:** New powerless credentials are rejected before secret sealing;
  existing rows remain visible and revocable; GitHub behavior is unchanged;
  wire compatibility checks pass.
- **Usage:** GitHub remains the only functional account integration (issue
  listing/reading, file reading, issue comments under approval and egress
  policy). IMAP, CalDAV, and Notion remain visible with no credential form.
  Their stored status and historical grants are preserved independently of
  tool availability. In Integrations, **Revoke access** deletes the local
  credential and retains the record; **Remove** deletes the row and credential.
  Neither requires decryption or the original sealing key. Audit records
  remain, and local deletion does not revoke copies at the vendor.
- **Dependencies:** No technical prerequisite. INT-001 landed independently;
  preserve DOC-001's documentation/status guard when integrating this behavior.

### 3. TUR-010 - No-worker and queue-timeout truth

- **Implementation status:** Shipped on `main` in #124 (`7a67d9cd`). A queued
  run's durable state records whether any live worker can serve its route, and
  two configurable bounds — a no-worker timeout and an overall queue-age ceiling
  accumulated across requeues — end a run that waits past them as `failed` or
  `cancelled` under `TURING_QUEUE_TIMEOUT_POLICY`. Pausing is explicitly not
  offered. [Bounded queue waiting](architecture/queue-wait.md) defines the
  semantics, configuration and limits.
- **Outcome:** A queued run cannot wait indefinitely without explanation.
- **Scope:** Persist queue age and worker availability notices; define
  configurable pause/terminal policy; render durable state in Flutter.
- **Likely files:** dispatcher/reaper, jobs and runs repositories, events,
  configuration, Flutter run cards.
- **Acceptance:** With every eligible worker stopped, a run immediately gains a
  durable notice and reaches the configured bounded outcome; restart and
  reconnect preserve the same truth.
- **Dependencies:** TUR-018 (shipped).

### 4. TUR-021 - Inspectable approval previews

- **Implementation status:** Shipped on `main` in #125 (`501246a8`).
  Authenticated structured details and complete bounded file diffs are bound to
  the stored approval, canonical arguments and actual session/run-scoped target.
  Approval and the protected write revalidate file preconditions; the desktop
  client handles loading, retry, explicit refresh, expiry and terminal
  reconciliation. [Inspectable approval
  previews](architecture/approval-previews.md) defines the 64 KiB before/after
  ceiling, conservative redaction, non-file policy, retention, older-client
  refusal and filesystem assumptions.
- **Outcome:** The user can see exactly what a mutation will change.
- **Scope:** Add structured approval details, bounded before/after file diffs,
  redaction, content hashes, and Flutter review UX.
- **Likely files:** approvals proto/service/repository, file preconditions,
  Flutter approval cards.
- **Acceptance:** The preview is bound to the same `args_hash` as the approval;
  stale files invalidate the decision; secrets and oversized content are
  redacted; tests prove the executed arguments match the reviewed preview.
- **Dependencies:** TUR-013 (shipped).

### 5. MEM-003 - Deterministic recall evaluation

- **Implementation status:** Shipped on `main` in #129 (`50fd4e3e`). A versioned
  21-case synthetic corpus runs real FTS5 repository/RPC search, existing
  runtime recall and actual budgeted provider-request admission offline.
  Per-case baseline thresholds and independent privacy/deletion/framing gates
  run in the existing root CI race suite. [Evaluation
  guide](../turing-backend/recall/eval/README.md) defines labels, formulas,
  limitations, reproducible reports and deliberate baseline updates. This does
  not change production ranking or evaluate model answers; MEM-004, MEM-016 and
  EVAL-001 remain separate work.
- **Outcome:** Retrieval and memory changes are selected by evidence.
- **Scope:** Checked-in fixtures for exact identifiers, paraphrases, updates,
  temporal questions, multi-session synthesis, CJK, injection, deletion, and
  abstention; report Recall@k, MRR, nDCG, stale-use, latency, prompt size, and
  context admission.
- **Likely files:** dedicated backend evaluation package, fixtures, CI command,
  contributor documentation.
- **Acceptance:** A baseline is checked in; every failure class has a fixture;
  regression thresholds fail CI; expected lexical failures remain visible.
- **Dependencies:** MEM-002 (shipped).

### 6. CON-001 - Bounded MCP tool lifecycle conformance

- **Implementation status:** Shipped on `main` in #131 (`6ffe90bc`). The
  selected revision, transport profile, negotiated capabilities and deliberate
  non-scope are documented in
  [MCP security and integration](mcp-security-and-integration.md#mcp-lifecycle-conformance);
  the guarded `mcp-lifecycle` row above owns the status claim.
- **Outcome:** Turing interoperates honestly with standard MCP tool clients and
  test servers on the surface it supports.
- **Scope:** Implement `initialize`/`initialized`, protocol and capability
  negotiation, ping/liveness, paginated `tools/list`, `tools/call`, untrusted
  annotations, cancellation, and bounded error handling.
- **Non-scope:** Resources, prompts, sampling, elicitation, OAuth, stdio,
  public ports, and arbitrary package execution.
- **Likely files:** runtime MCP client, both bundled tool servers, registry
  adapter, conformance fixtures.
- **Acceptance:** A stock test client discovers and calls bundled tools; Turing
  calls a stock test server; version skew fails clearly; ports remain internal;
  stdio remains refused.
- **Delivered:** MCP `2025-11-25` over Streamable HTTP. Both bundled servers
  answer `initialize` (negotiating the one supported revision, advertising only
  a `tools` capability), `notifications/initialized`, `ping`,
  `notifications/cancelled`, and `tools/list` / `tools/call` (each server
  answers tools/list in a single page and refuses a cursor; client-side cursor
  pagination is exercised against third-party servers, which do page); both refuse an unsupported `MCP-Protocol-Version` header with
  400, refuse any request carrying an `Origin`, and answer GET and DELETE on
  `/mcp` with 405. Both client paths — the runtime's client for the bundled
  servers and the orchestrator's registry client for configured third-party
  endpoints — handshake before any operation-phase call, announce the
  negotiated revision and any server-assigned session, read either a JSON or an
  event-stream response, and emit `notifications/cancelled` when their caller
  goes away. Conformance is exercised in both directions against the official
  MCP Go SDK, pinned at v1.8.0 as a test-only dependency.
- **Dependencies:** TUR-019 (shipped).

### 7. CXL-001 - Explicit cancel intent

- **Implementation status:** Shipped on `main` in #130 (`54eb9374`).
  Authenticated `CancelRun` persists explicit user intent and a replayable
  result using the canonical transition. `GetRunCancellation` and Flutter
  Stop/Check status distinguish acceptance from worker shutdown or recovery.
  Matching-attempt execution fences survive lost delivery and restart; late
  reports cannot revive the run. [Durable run
  outcomes](architecture/run-outcomes.md#explicit-cancellation) defines retry,
  upgrade, and no-rollback semantics.
- **Outcome:** A user can durably request cancellation instead of relying on a
  dropped transport.
- **Scope:** Public cancel RPC, idempotent request identity, run/version fencing,
  worker notification, terminal/recovering semantics, and Flutter action.
- **Likely files:** chat/runtime proto, run repository, runtime stream service,
  Flutter chat and run-state UI.
- **Acceptance:** Repeated cancellation is harmless; restart preserves intent;
  late deltas and completions cannot revive the run; the response distinguishes
  accepted, already terminal, and not found without leaking data.
- **Dependencies:** TUR-009 (shipped). TUR-025 later closes the broader
  cancellation-versus-tool-authorization race for every cancellation source.

### 8. PROV-001 - Native Anthropic and Gemini providers

- **Outcome:** Production use does not depend on compatibility shims that omit
  provider-native behavior.
- **Scope:** Native streaming, tool calls, usage, stop reasons, errors, context
  limits, and redirects for Anthropic Messages and Gemini; preserve the generic
  OpenAI-compatible adapter; add an additive `RemoteModelRoute` vocabulary
  without removing existing `ExternalAgent` wire fields.
- **Likely files:** `agent-runtime-go/internal/llm/`, provider configuration,
  external-agent proto/service/UI, egress disclosure tests.
- **Acceptance:** Native provider contract fixtures cover text, tools, usage,
  limits, malformed streams, and redirects; every remote call requires the
  existing run-owned disclosure; old clients continue to work.
- **Dependencies:** TUR-003 and TUR-019 (shipped).

### 9. SKL-001 - Agent Skills compatibility profile

- **Outcome:** Portable skills can be read without importing their authority.
- **Scope:** Parse standard Agent Skills metadata, validate names and bundle
  paths, preserve Turing extensions, expose unsupported executable content, and
  display `allowed-tools` only as untrusted requested metadata.
- **Likely files:** skill loader/models/proto, Flutter skill UI, compatibility
  fixtures.
- **Acceptance:** Conforming fixtures load; malicious paths and oversized
  bundles fail closed; `allowed-tools` cannot enable, grant, or call a tool;
  existing Turing skills remain compatible.
- **Dependencies:** None.

### 10. SEC-001 - Non-loopback device identity and transport

- **Outcome:** Remote reach does not turn the shared desktop bearer into a
  network-wide master key.
- **Scope:** Pairing challenges, device public keys, scoped short-lived
  credentials, revocation, device audit, TLS/mTLS transport contract, rotation,
  and recovery; preserve loopback desktop behavior.
- **Likely files:** new device proto/service/repository, auth interceptors,
  migrations, Flutter pairing/settings UI, Compose/operator docs.
- **Acceptance:** LAN/tailnet membership alone grants nothing; stolen expired or
  revoked tokens fail; a paired device can authenticate through a test TLS
  endpoint; the existing loopback client still works unchanged.
- **Dependencies:** None.

## Thematic workstreams after the first ten

### Workstream A - Truth, resilience, and trust UX

#### TUR-023 - Harden legacy event payloads

- **Outcome:** Public history cannot expose arbitrary sensitive fields retained
  in legacy system, error, or unknown events.
- **Scope:** Strict per-type public projections and bounded idempotent
  migration/read scrub.
- **Acceptance:** Seeded legacy secrets cannot appear in list, subscribe,
  replay, or restart paths; allowlisted content survives.
- **Dependencies:** TUR-009 and TUR-010.

#### TUR-011 - Batch model deltas

- **Outcome:** Streaming does not create one SQLite transaction per provider
  fragment.
- **Scope:** Time/byte coalescing with explicit flush configuration.
- **Acceptance:** A 1,000-fragment fixture creates bounded writes and reopens to
  byte-identical output; terminal events flush pending text.
- **Dependencies:** None.

#### TUR-024 - Recover lost approval notifications

- **Outcome:** A committed decision reaches the matching waiter after a lost
  post-commit notification.
- **Scope:** Durable idempotent delivery keyed by approval, run, assignment,
  attempt, and state version.
- **Acceptance:** Loss/reconnect/duplicate tests resume only the matching waiter
  and never execute twice.
- **Dependencies:** TUR-009 and TUR-011.

#### TUR-014 - Provider usage and actionable health

- **Outcome:** Operators can explain latency, usage, queue pressure, and
  dependency readiness.
- **Scope:** Provider timing/usage, structured readiness, and local metrics with
  no sensitive content.
- **Acceptance:** Health degrades for unavailable required dependencies;
  provider-reported usage remains distinct from estimates and cost.
- **Dependencies:** TUR-018.

#### OPS-001 - Host command for setup and operation

- **Implementation status:** Shipped on `main` in #171, #173 and OPS-001 PR 3.
  #171 delivers `check-config` and safe argument handling in both services;
  #173 delivers the host `turing` command's `status`, lifecycle, `logs`,
  `connect` and `config`, plus a CI Linux desktop build; PR 3 delivers guided
  `setup`, `engine detect | use | check` and the runtime's `check-engine`, with
  the [local engines guide](architecture/local-engines.md). `setup` runs
  `scripts/init.sh` for provisioning, which stays non-interactive. The command
  invokes `compose.sh`, rather than replacing its launch checks.
- **Outcome:** A user sets up, runs, reconfigures and connects to a Turing
  server without editing `.env` or reading scripts.
- **Scope:** A user-run `turing` command on the server: guided `setup`,
  `status`, lifecycle, `logs`, `connect`, `config` validated by each service's
  own `check-config` before anything is written, and `engine detect | use |
  check` from the local inference engines design. Until SEC-001, the app
  runs on the server itself (owner decision, 2026-10-26). Design:
  [host command](superpowers/specs/2026-10-25-turing-host-command-design.md).
- **Acceptance:** Every `.env` setting a user should change is changed through
  the command; a value a service's configuration check refuses, or that could
  not be checked, leaves `.env` unchanged (the generated app key is the one
  value written without that check); a refusal only a running orchestrator can
  see is reported on apply, with a restore offered; no image that predates
  `check-config` is run with an argument; no secret appears on a command line,
  and the app's key appears only in `connect` and `setup`; nothing in Turing
  can invoke the command, and the orchestrator gains no Docker control.
- **Dependencies:** None.

#### OPS-002 - Server settings in the app

- **Outcome:** Settings the orchestrator can apply safely while running are
  changed from the app, wherever it runs.
- **Scope:** An orchestrator-owned settings store with server-side validation,
  sealed provider keys, live application to workers, and a one-time import from
  `.env`; host-owned settings (engine, ports, secrets) stay with OPS-001.
- **Acceptance:** Each setting has one home; a change applies without a
  restart or reports that it cannot; `turing config` and the app never
  disagree.
- **Dependencies:** OPS-001.

#### OPS-003 - Profile and skill editors

- **Outcome:** Specialist profiles and skills are authored in forms, not
  Markdown files.
- **Scope:** App editors that write `team/` and `skills/` through the
  orchestrator, reusing SKL-002's staging, diff and install-disabled path for
  skills, and the change to the read-only `/team` mount's threat model.
- **Acceptance:** An authority change still requires a fresh grant; no agent
  path can write a profile or a skill.
- **Dependencies:** OPS-002 and SKL-002.

#### TUR-025 - Fence tool authorization cancellation

- **Outcome:** A tool cannot be authorized after its run leaves an active
  lifecycle.
- **Scope:** Guard active run/version and tool/approval creation in one
  transaction.
- **Acceptance:** Deterministic races prove cancellation creates no late
  approval, tool record, event, or side effect.
- **Dependencies:** TUR-009, TUR-010, and TUR-021.

#### TUR-012 - Push approved decisions

- **Outcome:** Approval wakes the exact waiter without ordinary one-second
  polling.
- **Scope:** Push approved decisions; polling remains reconnect fallback.
- **Acceptance:** Duplicate and late updates preserve single consumption.
- **Dependencies:** None; deliver after TUR-024 in the priority spine.

#### AUD-001 - Flutter audit viewer

- **Outcome:** The existing redacted audit API is usable by the owner.
- **Scope:** Filtered, paginated timeline with payload-state explanations.
- **Acceptance:** The UI never renders fields absent from the API allowlist and
  handles scrubbed, absent, malformed, and empty states.
- **Dependencies:** TUR-013.

#### EVT-001 - Durable transcript interleaving

- **Outcome:** Reopening preserves the meaningful order of messages, tool
  cards, run notices, approvals, and partial model output.
- **Scope:** Durable interleaving key, replay projection, and partial-delta
  persistence without duplicating lifecycle truth.
- **Acceptance:** Crash fixtures reopen to the same visible order and content
  as the live timeline.
- **Dependencies:** TUR-009 and TUR-011.

### Workstream B - Measurable personal intelligence

#### EVAL-001 - End-to-end personal-agent benchmark

- **Outcome:** Turing's usefulness and safety are measured across whole runs.
- **Scope:** Scenario fixtures for tool selection, consent, abstention,
  personalization, channel isolation, latency, and small-model recovery.
- **Acceptance:** Reports separate capability, safety, privacy, and latency;
  no score can hide a privacy failure behind higher task success.
- **Dependencies:** TUR-010, TUR-021, and MEM-003.

#### MEM-004 - Structured one-call recall

- **Outcome:** Recall uses one safe server-ranked request.
- **Scope:** Repeated validated terms, match modes, filters, and one RPC.
- **Acceptance:** FTS operators cannot be injected and evaluation is no worse
  than the MEM-003 baseline.
- **Dependencies:** MEM-002 and MEM-003.

#### MEM-016 - Explicit CJK lexical recall

- **Outcome:** CJK users do not receive silently weaker recall.
- **Scope:** Select a measured tokenizer or n-gram strategy.
- **Acceptance:** Labelled CJK Recall@5 improves without regressing exact IDs or
  query safety.
- **Dependencies:** MEM-003 and MEM-004.

#### MEM-008 - Transparent scoped memory use

- **Outcome:** Profile, curated memory, and transcript recall use separate
  budgets and controls.
- **Scope:** Status/scope/time filters, memory-use events, and global,
  per-session, and per-turn controls.
- **Acceptance:** Disabled, superseded, or deleted content never appears; each
  injected item is inspectable.
- **Dependencies:** MEM-003 and the shipped MEM-006/MEM-007 substrate.

#### MEM-017 - Sensitivity and data-scope policy

- **Outcome:** Memory has an explicit policy for where it may be used.
- **Scope:** User-visible sensitivity classes, source defaults, manual
  overrides, remote/channel rules, and redacted audit.
- **Acceptance:** Sensitive classes are excluded by default from remote models,
  channels, MCP exports, and A2A; overrides are destination-specific and
  recorded.
- **Dependencies:** MEM-008.

#### MEM-009 - Local candidate extraction

- **Outcome:** Turing learns from conversation without silently rewriting the
  user's beliefs.
- **Scope:** Post-run local extraction from user-authored evidence only;
  candidates, never active memory; review queue; bounded transaction budget.
- **Acceptance:** Extraction cannot delay/fail a run; malformed or timed-out
  extraction writes nothing; assistant/tool/recalled content cannot become a
  candidate source; no network egress occurs.
- **Dependencies:** MEM-003, MEM-008, MEM-017, and shipped MEM-006/MEM-007.

#### MEM-010 - Reversible revision and supersession

- **Outcome:** Beliefs evolve without opaque overwrites.
- **Scope:** Immutable revisions, duplicate/contradiction proposals,
  supersession links, preimages, review, and revert.
- **Acceptance:** Current belief selection is deterministic; every replacement
  can be explained and reversed.
- **Dependencies:** MEM-009.

#### MEM-011 - Fact- and message-level forgetting

- **Outcome:** One fact or source can be withdrawn without deleting a session.
- **Scope:** Memory and message deletion, run-anchor rules, and propagation
  through FTS, candidates, summaries, caches, and future vectors.
- **Acceptance:** Deleted content cannot return after restart; audit retains
  only scrubbed evidence.
- **Dependencies:** MEM-001 and the shipped MEM-005/MEM-006 substrate.

#### MEM-012 - Memory observability

- **Outcome:** The user can answer why Turing remembered or used something.
- **Scope:** Trace retrieval, injection, extraction, promotion, supersession,
  retraction, and index deletion.
- **Acceptance:** Every used memory links to source and decision history without
  logging content by default.
- **Dependencies:** TUR-013, MEM-008, and MEM-009.

#### MEM-015 - Tool results as attributed evidence

- **Outcome:** Selected tool knowledge can be recalled without becoming trusted
  belief.
- **Scope:** Allowlisted bounded evidence with tool/run/session/source
  provenance and trust class.
- **Acceptance:** Prompt-like tool output cannot create active memory; deleting
  source state removes every projection.
- **Dependencies:** MEM-001, MEM-011, and the MEM-005 substrate.

#### MEM-013 - Optional local semantic retrieval

- **Outcome:** Paraphrase retrieval improves only where lexical evaluation
  proves a gap.
- **Entry gate:** MEM-003/MEM-004 identify a labelled failure class and a local
  embedder improves it without unacceptable stale-use or latency.
- **Acceptance:** Lexical fallback remains; deletion removes vector state;
  enabled/disabled metrics are checked in.
- **Dependencies:** MEM-003, MEM-004, MEM-011, and the MEM-005 substrate.

#### MEM-014 - Token-aware summaries

- **Outcome:** Long sessions fit model limits without losing live protocol or
  provenance.
- **Scope:** Provider capability metadata, tokenizer-aware budgets, and
  source-linked summaries.
- **Acceptance:** Summary failure falls back safely; deletion removes derived
  summaries; tool protocol is never split.
- **Dependencies:** MEM-001, MEM-003, and TUR-018.

### Workstream C - Providers and plural local agents

#### PROV-002 - Provider capability discovery

- **Outcome:** Routing knows context limits, modalities, tool support, usage,
  and reasoning controls.
- **Scope:** Versioned capability records from configuration or provider
  discovery with conservative fallbacks.
- **Acceptance:** Unsupported selections fail before enqueue and capability
  changes cannot rewrite queued-run truth.
- **Dependencies:** PROV-001 and TUR-018.

#### AGT-000 - Concurrency evidence

- **Outcome:** Multi-agent design is based on measured SQLite and lease
  behavior.
- **Scope:** Raise concurrency above one in deterministic load fixtures and
  record claim, heartbeat, cancellation, recovery, and transaction metrics.
- **Acceptance:** Thresholds are documented; failures identify the violated
  budget; no broker is introduced without measured evidence.
- **Dependencies:** TUR-010 and TUR-011.

#### AGT-001 - Second specialized agent

- **Status:** Shipped as file-defined specialist profiles under the existing
  `AgentId` (PR #150 and the delegation PRs), not as a second `AgentId`: an
  enum value per agent could not be user-extensible, and every new agent would
  need a proto change.
- **Outcome:** Turing supports plural agents with explicit routing and policy.
- **Scope:** File-defined profiles under the existing `AgentId`, deterministic
  routing/handoff, per-agent tools, provider capabilities, capacity, events,
  and concurrency tests.
- **Acceptance:** Routing is visible; handoff preserves provenance; concurrent
  agents do not violate session ordering, leases, fencing, or approvals.
- **Delivered prerequisites:** TUR-010, TUR-018, TUR-019 and CON-002.
- **Follow-on hardening:** TUR-011 (delta batching) and TUR-014 (provider
  usage and health) remain pending; they are not claimed as delivered by the
  single-slot specialist implementation. AGT-000 still gates raising
  concurrency above one, which this item does not do.

#### AGT-002 - Parent/child delegated runs

- **Outcome:** An agent can delegate bounded work without hiding control flow.
- **Scope:** Parent/child run identity, budgets, cancellation propagation,
  artifact return, and failure aggregation.
- **Acceptance:** A parent can spawn, observe, cancel, and collect a child;
  children cannot inherit undeclared tools, egress, memory, or approval.
- **Status:** Shipped behind `TURING_AGENT_TEAM_ENABLED`, default `false`
  (PRs #152–#158, #162, #163 and #167), including child-owned egress consent
  per task. The flag defaults to on only after EVAL-001's delegation scenarios
  pass.
- **Dependencies:** AGT-001 and EVAL-001 (the latter now gates the default).

#### AGT-003 - Visible planning and handoff

- **Outcome:** The user can inspect what is being delegated and why.
- **Scope:** Typed plan/handoff events, UI, audit, and replay.
- **Acceptance:** Hidden delegation is impossible; reopen shows the same plan,
  agents, scopes, and outcomes.
- **Status:** Partly shipped (PR #163): delegation events, task cards and
  reopen. Typed plans and audit-viewer integration remain, with AUD-001 and
  EVT-001.
- **Dependencies:** AGT-002, AUD-001, and EVT-001.

#### AGT-004 - Brokered specialist requests

- **Outcome:** A specialist can ask another for help without Turing spending a
  model call to relay it.
- **Scope:** A request through the orchestrator service (for example
  `team.ask`) with flow labels that refuse private-to-egress paths, a budget,
  and no cycles.
- **Acceptance:** A request never widens the asking specialist's tools, egress,
  memory or approvals; cycles and over-budget chains are refused before any
  run starts; the user can see who asked whom.
- **Dependencies:** AGT-002, with AGT-000 evidence before requests run in
  parallel.

### Workstream D - Safe customization ecosystem

#### CON-REG-001 - Curated registry discovery

- **Outcome:** Users can discover MCP packages without treating registry
  presence as trust.
- **Scope:** Read-only aggregator API, namespace/source metadata, hashes,
  review, disabled import, and refresh policy.
- **Acceptance:** Discovery never installs or executes code; imported servers
  start disabled and approval-required.
- **Dependencies:** CON-001.

#### SKL-002 - Skill import quarantine

- **Outcome:** Skills from files or marketplaces have a reversible review path.
- **Scope:** Bundle staging, hashes, source URL, license, scanner findings,
  path confinement, diff, install disabled, and rollback.
- **Acceptance:** Re-import is idempotent; changed bundles require review; no
  content reaches a prompt before enablement and grants.
- **Dependencies:** SKL-001.

#### SKL-003 - Self-authored skill candidates

- **Outcome:** Turing can turn an explicit lesson or repeated workflow into a
  reusable draft.
- **Scope:** Explicit teach action plus measured repeated-success eligibility;
  agent writes only to quarantine; full diff and requested capabilities.
- **Acceptance:** Drafts are not discoverable by runs; only a user can install,
  grant, and enable; source evidence remains linked.
- **Dependencies:** SKL-002, MEM-009, and EVAL-001.

#### SKL-004 - Skill improvement proposals

- **Outcome:** Skills improve without silently editing active instructions.
- **Scope:** Usage evidence, proposed patch, regression examples, preimage,
  review, and revert.
- **Acceptance:** Active bytes never change before approval; each revision has
  provenance and measurable acceptance evidence.
- **Dependencies:** SKL-003 and MEM-010.

#### XTOOL-001 - Sandboxed executable tool runtime

- **Outcome:** Reviewed code can extend capability without entering the trusted
  kernel.
- **Scope:** Ephemeral read-only-root containers, CPU/memory/time limits, no
  direct network, declared mounts/secrets, brokered internal HTTP/gRPC,
  orchestrator-owned egress tools for reviewed remote destinations, and
  complete cleanup.
- **Acceptance:** Escape, undeclared network, undeclared files, credential
  access, and stale-process fixtures fail closed; stdio remains unsupported.
- **Dependencies:** CON-001, TUR-014, and TUR-021.

#### XTOOL-002 - Generated tool candidates

- **Outcome:** Turing can propose a new tool, test it, and ask the user to
  install it safely.
- **Scope:** Quarantined source, reproducible build inputs, manifest, static
  scan, sandbox tests, diff, disabled registration, grants, and rollback.
- **Acceptance:** Generated code never loads into the Go process; no build has
  network by default; installation does not enable tools; runtime calls still
  require normal policy and approval.
- **Dependencies:** XTOOL-001, SKL-003, and EVAL-001.

### Workstream E - Secure mobile and channel reach

#### MOB-001 - Production mobile client

- **Outcome:** The Flutter app is a supported paired iOS/Android client.
- **Scope:** Release network entitlements, secure key storage, pairing UX,
  certificate validation, reconnect, lifecycle parity, and mobile CI.
- **Acceptance:** Physical-device tests cover chat, replay, cancel, approval,
  memory review, revocation, and offline recovery through the SEC-001 contract.
- **Dependencies:** SEC-001, CXL-001, and EVT-001.

#### MOB-002 - Private reachability and optional blind relay

- **Outcome:** A paired phone can reach the home hub without a public direct
  port.
- **Scope:** Serve-class private reverse-proxy guidance plus an optional opaque
  envelope relay with queue bounds, expiry, deletion, and no decryption/key
  minting.
- **Acceptance:** Direct public access remains closed; relay compromise reveals
  no plaintext; expired/revoked device traffic fails; outage state is visible.
- **Dependencies:** SEC-001, MOB-001, TUR-010, and CXL-001.

#### CHN-000 - Channel threat and privacy contract

- **Outcome:** Every channel implements the same explicit boundary.
- **Scope:** Identity, platform visibility, context scopes, rate/replay limits,
  approvals, delivery, channel-specific retention bounds, revocation, incident
  response, and group-chat non-scope.
- **Acceptance:** Adversarial fixtures exist before the first adapter; product
  copy names platform custody and prohibited claims.
- **Dependencies:** SEC-001 and MEM-017. TUR-017 later unifies channel records
  with the product-wide retention system; this task must define safe bounded
  channel defaults before any adapter ships.

#### CHN-001 - Service-scoped channel gateway

- **Outcome:** Channel adapters cannot impersonate the runtime or native client.
- **Scope:** New internal identity, allowed RPCs, pairing records, channel
  sessions, scope snapshots, and content-free auth-failure audit.
- **Acceptance:** Wrong-service calls fail before handlers; unbound senders run
  no model and store no message content.
- **Dependencies:** TUR-006, SEC-001, and CHN-000.

#### CHN-002 - Durable inbound/outbound delivery

- **Outcome:** Platform retries and crashes do not duplicate turns or lose
  replies silently.
- **Scope:** Platform event identity, idempotent enqueue, outbound delivery
  ledger, bounded retries, and visible ambiguity.
- **Acceptance:** Crash-before-send, crash-mid-send, duplicate update, reorder,
  and expiry fixtures have deterministic outcomes.
- **Dependencies:** CHN-001, TUR-001, and TUR-009.

#### CHN-TG-001 - Telegram direct-message adapter

- **Outcome:** The paired owner can converse through Telegram without public
  ingress.
- **Scope:** Long polling, owner binding, direct messages only, text/media
  limits, minimal-context defaults, delivery ledger, and native-app approvals.
- **Acceptance:** Group/unbound traffic runs no model; Telegram outages are
  bounded; remote-model use still requires TUR-003 consent.
- **Dependencies:** CHN-001, CHN-002, MOB-001, and MEM-017.

#### CHN-SL-001 - Slack Socket Mode adapter

- **Outcome:** The paired owner can converse through a selected Slack install
  without a public request URL.
- **Scope:** Socket Mode, exact workspace/user/channel binding, app-token
  rotation, signature/envelope checks, minimal-context defaults, and delivery
  ledger.
- **Acceptance:** Workspace installation alone grants no Turing access; only
  bound direct-message scope is enabled; approvals stay in the native app.
- **Dependencies:** CHN-001, CHN-002, MOB-001, and MEM-017.

#### CHN-RELAY-001 - Blind webhook ingress

- **Outcome:** Webhook-only vendors can reach a home hub without a public
  Turing endpoint or relay plaintext custody.
- **Scope:** Vendor signature termination, encrypted re-envelope to a paired
  hub, bounded queue, replay defense, expiry, deletion, and operator recovery.
- **Acceptance:** The relay cannot call Turing, decrypt envelopes, broaden
  scope, or retain expired payloads; direct public ports remain closed.
- **Dependencies:** MOB-002, CHN-001, CHN-002, and TUR-017.

#### CHN-WA-001 - WhatsApp Cloud adapter

- **Outcome:** An explicitly connected business number can reach a scoped
  Turing channel.
- **Scope:** Webhook verification, message/status handling, templates and
  platform windows, media limits, owner binding, and vendor-custody disclosure.
- **Acceptance:** No consumer end-to-end encryption claim; unknown numbers run
  no model; deletion/revocation limits are stated honestly.
- **Dependencies:** CHN-RELAY-001 and INT-002.

#### CHN-TEAMS-001 - Microsoft Teams adapter

- **Outcome:** An explicitly installed Teams app can reach a scoped Turing
  channel.
- **Scope:** Bot endpoint identity, tenant/user binding, activity idempotency,
  adaptive-card limits, Entra credential lifecycle, and platform disclosure.
- **Acceptance:** Tenant installation alone grants nothing; unknown users run
  no model; permissions and revocation are inspectable.
- **Dependencies:** CHN-RELAY-001 and INT-002.

### Workstream F - Agent and client interoperability

#### MCPX-001 - Loopback Turing MCP server

- **Outcome:** Explicitly paired local host applications can use a bounded
  Turing surface.
- **Scope:** Loopback only, per-client scoped tokens, reviewed resources/tools,
  approval routing, audit, revocation, and response budgets.
- **Acceptance:** No client receives vault-wide or mutation access by default;
  every disclosure is audited; no non-loopback listener exists.
- **Dependencies:** CON-001, TUR-021, and MEM-017.

#### MCPX-002 - Remote MCP host bridge

- **Outcome:** A vendor-cloud MCP host can reach only the surface explicitly
  shared for one destination.
- **Scope:** Blind relay, host identity, signed egress disclosure for arguments
  and results, per-host grants, revocation, and bounded callback state.
- **Acceptance:** No public direct MCP port; enabling a host is not run consent;
  every tool response that leaves is covered by the exact decision.
- **Dependencies:** MCPX-001, SEC-001, MOB-002, and MEM-017.

#### A2A-001 - Outbound A2A client

- **Outcome:** Turing can delegate a bounded task to a registered peer agent.
- **Scope:** Agent Card validation, endpoint pinning, auth, task/message/artifact
  budgets, streaming/cancel, provenance, and egress disclosure.
- **Acceptance:** Peer descriptions are untrusted; no peer inherits memory,
  tools, approvals, or session consent; failures remain visible.
- **Dependencies:** TUR-003, PROV-002, and EVAL-001.

#### A2A-002 - Optional inbound A2A server

- **Outcome:** A registered peer can ask Turing to perform explicitly allowed
  work.
- **Scope:** Disabled-by-default endpoint, peer identity, allowed skills,
  task/data budgets, approval routing, rate limits, audit, and normal run
  lifecycle mapping.
- **Acceptance:** Anonymous peers get zero capability; inbound tasks cannot
  bypass tool, memory, or egress policy; owner disable/revoke is immediate.
- **Dependencies:** A2A-001, SEC-001, TUR-010, TUR-021, and MEM-017.

#### ACP-001 - Optional editor adapter

- **Outcome:** Compatible editors can host Turing's coding-agent experience
  without changing Turing's internal protocol.
- **Scope:** ACP session, prompt, tool, permission, diff, and terminal mapping
  through a separate adapter.
- **Acceptance:** ACP cannot widen workspace roots or tool policy; closing the
  editor does not corrupt the durable run; ACP remains distinct from A2A.
- **Dependencies:** MCPX-001, AGT-002, and XTOOL-001.

### Workstream G - Account integrations and imports

#### WEB-001 - Web search and fetch

- **Outcome:** Research can search the web and read pages.
- **Scope:** Orchestrator-owned `web.search` and `web.fetch` with framed,
  attributed results, a remote-egress disclosure, and domain and size limits;
  a specialist reaches them through child egress consent.
- **Acceptance:** No query or page leaves the machine without the disclosed
  consent; fetched content is framed as untrusted data and cannot authorize a
  tool call; size and domain limits fail closed.
- **Dependencies:** TUR-003 and AGT-002's child egress consent.

#### AZ-001 - Azure tooling

- **Outcome:** Dev can inspect Azure resources.
- **Scope:** Read-first resource inspection; any write is a mutation behind an
  argument-bound approval. Credentials stay out of the runtime, as for GitHub.
- **Acceptance:** Reads need the disclosed egress consent; no write runs
  without an approval bound to its arguments; credentials never reach a run,
  log or event.
- **Dependencies:** TUR-021 and AGT-002's child egress consent; INT-002 if
  sign-in requires OAuth.

#### INT-002 - User-owned OAuth client architecture

- **Outcome:** OAuth-only providers can be connected without a Turing-operated
  multi-tenant credential broker.
- **Scope:** User-provided client registration, PKCE, loopback/native redirect,
  state/nonce, sealed refresh tokens, scope snapshots, rotation, and revoke.
- **Acceptance:** Missing registration fails before consent; tokens never enter
  logs/events; scope widening requires a new consent; provider revoke limits
  are stated.
- **Dependencies:** SEC-001 and TUR-003.

#### CON-003 - Reversible local imports

- **Outcome:** User-owned Markdown/JSON can enter memory through preview and
  rollback.
- **Scope:** Source identity, hashes, idempotency, candidate isolation, preview,
  and deletion propagation.
- **Acceptance:** Re-import is idempotent; imported text cannot become profile
  or policy without promotion; rollback removes every projection.
- **Dependencies:** MEM-006, MEM-009, and MEM-011.

#### CON-004 - Shared connector lifecycle

- **Outcome:** Account connectors use one consent, provenance, sync, and revoke
  model.
- **Scope:** Connector registry, scopes, credential boundary, cursors, source
  deletion, trust class, and per-call egress.
- **Acceptance:** Revocation stops sync; source deletion propagates; connector
  content cannot promote itself.
- **Dependencies:** CON-002, CON-003, TUR-006, and MEM-011.

#### INT-003 - Functional IMAP tools

- **Outcome:** A connected mailbox provides useful, bounded operations.
- **Scope:** Folder/message search and read first; mutations later under
  separate approval policies; TLS, endpoint validation, and content framing.
- **Acceptance:** Read scope and retention are visible; message content is
  untrusted; deletes/moves cannot ship before their lifecycle is specified.
- **Dependencies:** CON-004 and INT-001.

#### INT-004 - Functional CalDAV tools

- **Outcome:** A connected calendar supports inspected reads and approved
  changes.
- **Scope:** Calendar/event list/read, create/update/delete with ETags and
  argument-bound previews.
- **Acceptance:** Conflicts fail safely; private event content follows memory
  sensitivity and egress policy.
- **Dependencies:** CON-004, INT-001, and TUR-021.

#### INT-005 - Functional Notion tools

- **Outcome:** Shared Notion pages can be read and changed within provider
  scope.
- **Scope:** Search/read first, then page mutations with previews and
  provider-enforced page boundaries.
- **Acceptance:** Unshared content is unreachable; retrieved instructions stay
  untrusted; mutations require approval.
- **Dependencies:** CON-004, INT-001, and TUR-021.

#### INT-006 - Google Workspace connector

- **Outcome:** Gmail, Calendar, and Drive can be connected under explicit OAuth
  scopes.
- **Scope:** Start read-only and provider-separated; add mutations only after
  preview and retention policies.
- **Acceptance:** Each product/scope is independently revocable; background
  sync is opt-in and bounded.
- **Dependencies:** INT-002, CON-004, MEM-017, and TUR-021.

#### INT-007 - Microsoft 365 connector

- **Outcome:** Outlook, Calendar, and files can be connected under explicit
  Entra scopes.
- **Scope:** Same phased read-before-write model as INT-006.
- **Acceptance:** Tenant/account identity is explicit; scopes, revocation, and
  platform retention are visible.
- **Dependencies:** INT-002, CON-004, MEM-017, and TUR-021.

#### INT-008 - Slack account connector

- **Outcome:** Slack workspace data can be used as an integration separately
  from Slack as a chat channel.
- **Scope:** OAuth install, selected-workspace scopes, search/read, optional
  approved writes, and source deletion.
- **Acceptance:** Channel gateway credentials and account-integration tokens are
  separate; workspace content remains untrusted.
- **Dependencies:** INT-002, CON-004, MEM-017, and TUR-021.

### Workstream H - Ownership, retention, and encrypted longevity

#### TUR-015 - Session and memory export

- **Outcome:** Data ownership includes possession.
- **Scope:** Stream allowlisted open-format records without secrets.
- **Acceptance:** Large exports stream; content round-trips; denylist tests
  cover credentials, tokens, and approval identifiers.
- **Dependencies:** MEM-006.

#### TUR-016 - Backup, restore, and migration integrity

- **Outcome:** Local state can be recovered without silent schema drift.
- **Scope:** WAL-aware backup, restore verification, migration checksums, and
  operator recovery.
- **Acceptance:** Restored state passes invariants; altered applied migrations
  are detected; secrets are not bundled accidentally.
- **Dependencies:** TUR-015.

#### TUR-017 - Bounded retention

- **Outcome:** Events, audit, tool results, rejected candidates, channel
  envelopes, and trash cannot grow forever.
- **Scope:** Default-safe policies, bounded sweeps, active-run exclusions,
  replay gaps, and immutable evidence.
- **Acceptance:** Defaults preserve data; enabled pruning is batched and
  observable; replay requests resync instead of silently omitting history.
- **Dependencies:** TUR-013 and MEM-012.

#### TUR-022 - Encrypted database and retirement

- **Outcome:** Managed SQLite state and backups are protected at rest and an
  entire managed database can be retired cryptographically where custody
  qualifies.
- **Scope:** Implement the approved design only after backup/restore exists;
  preserve the documented key domains, migration budgets, platform custody,
  rollback, and limitations.
- **Acceptance:** The approved design's encryption, migration, restore,
  inventory, key-loss, and retirement tests pass without claiming per-session
  forensic erasure.
- **Dependencies:** TUR-004 and TUR-016. The design gate is shipped in PR #85.

### Workstream I - Evidence-gated modalities and prospective memory

#### MOD-001 - Typed attachments and media safety

- **Outcome:** Images, audio, and files have a bounded, attributable lifecycle.
- **Scope:** Content types, size limits, local staging, malware/type checks,
  deletion, egress categories, and client rendering.
- **Acceptance:** Media never enters a provider implicitly; deleted attachments
  cannot survive through cache or derived state.
- **Dependencies:** MEM-017, TUR-015, and TUR-017.

#### MOD-002 - Vision

- **Outcome:** Turing can inspect user-selected images locally or through an
  explicitly disclosed provider.
- **Scope:** Provider capability routing, local-model path, redaction preview,
  and evidence attribution.
- **Acceptance:** Image egress is separately disclosed; unsupported providers
  fail before enqueue; evaluation records accuracy and latency.
- **Dependencies:** MOD-001, PROV-002, and EVAL-001.

#### MOD-003 - Local-first voice

- **Outcome:** The native app can transcribe and speak without requiring remote
  processing by default.
- **Scope:** Local STT/TTS providers, push-to-talk, interruption, audio
  retention, device routing, and optional remote disclosure.
- **Acceptance:** The default path stays local; recordings follow explicit
  retention; interruption and cancellation are reliable.
- **Dependencies:** MOD-001, MOB-001, CXL-001, and EVAL-001.

#### PRS-001 - Prospective memory and reminders

- **Outcome:** Reminders and intended actions are structured state, not prose
  hidden in beliefs.
- **Scope:** Task/reminder domain, due times, timezone, completion, snooze,
  automation link, delivery targets, and approval boundaries.
- **Acceptance:** Missed delivery remains visible; timezone changes are
  deterministic; reminders cannot smuggle unattended tool permission.
- **Dependencies:** TUR-010, the shipped automation substrate, MOB-001, and
  CHN-002 for channel delivery.

#### UX-001 - Learning journey and explanation

- **Outcome:** The user can inspect how Turing's profile, beliefs, skills, and
  capabilities changed over time.
- **Scope:** Read-only timeline/graph over revisions, provenance, promotions,
  grants, and withdrawals; edit/retract actions use existing services.
- **Acceptance:** The visualization derives from authoritative state; it cannot
  create or rewrite memory, skills, or grants on its own.
- **Dependencies:** MEM-010, MEM-012, SKL-004, and AUD-001.

### Workstream J - MCP protocol evolution

<a id="mcp-protocol-evolution"></a>

**Status:** Planned. No MCPP capability is delivered by this document.
The [research and adoption plan](architecture/mcp-protocol-adoption.md)
records current-versus-legacy protocol facts, the four implementation surfaces,
immutable sources, and failure/authorization contracts. `CON-001` remains
shipped in its documented 2025-11-25 scope.

#### MCPP-001 - Honest legacy interoperability and diagnostics

- **Outcome:** Unsupported modern peers and task-only tools fail clearly,
  without unsafe retries or misleading availability.
- **Scope:** Typed bounded HTTP/JSON-RPC errors, era-aware retry classification,
  filtering of legacy `execution.taskSupport: required` tools on both client
  paths, and explicit premature-stream-closure outcomes.
- **Likely files:** `turing-backend/mcpwire/`, runtime MCP client and tool
  registry, orchestrator registry client and discovery.
- **Acceptance:** Modern-only stock-peer, required-task and disconnect
  fixtures prove clear failure; no peer-controlled error text or credentials
  enter persisted status; an uncertain approved mutation is never replayed.
- **Dependencies:** CON-001 (shipped).

#### MCPP-002 - Modern registry client with legacy fallback

- **Outcome:** Third-party modern-only, dual-era and legacy servers interoperate
  without changing Turing's policy authority.
- **Scope:** Safe `server/discover` probing, per-request version/capabilities,
  required HTTP metadata and validated parameter headers, typed results,
  structured output/schema bounds and cache hints; modern stream-close
  cancellation versus legacy notifications/session cleanup.
- **Likely files:** Shared wire helpers, registry client/transport/discovery,
  tool descriptor contracts and conformance fixtures.
- **Acceptance:** An explicit stock-peer matrix covers all three peer modes;
  header/body mismatch and unsupported capabilities are actionable; unknown
  result types fail closed; negotiation never retries a mutation; queued-run
  tool snapshots and credential/endpoint boundaries survive cache refresh.
- **Dependencies:** MCPP-001 and CON-002 (shipped). No PROV-001 dependency.

#### MCPP-003 - Dual-era bundled MCP transport

- **Outcome:** The runtime and bundled servers support the modern core while
  retaining the current legacy callers.
- **Scope:** Runtime client reuse of the era-aware wire layer; modern
  `server/discover`, exact metadata allowlists, headers and result envelopes
  in both bundled servers. Tasks are not required for short bundled tools.
- **Likely files:** Runtime MCP client; `mcp-files` and `mcp-system` server
  lifecycle/dispatch code; shared wire and stock-client tests.
- **Acceptance:** Modern and legacy stock clients work; approval/provenance
  identities and Origin rejection remain intact; error-envelope changes
  preserve uncertain-effect handling; the system binary stays stdlib-only.
- **Dependencies:** MCPP-002. Not a prerequisite for MCPP-004.

#### MCPP-004 - Bounded consumption of the modern Tasks extension

- **Outcome:** A registered modern tool can return a durable remote task handle
  instead of forcing one long HTTP response.
- **Scope:** Explicit opt-in to `io.modelcontextprotocol/tasks`, sealed handle
  persistence, bounded inline `tasks/get`, cooperative `tasks/cancel`, task
  expiry and exact result/error mapping. No `tasks/result` or `tasks/list`.
- **Likely files:** Registry dispatch, repository/schema, cancellation/recovery
  paths and schema-pinned task-peer fixtures.
- **Acceptance:** One logical dispatch issues one initial `tools/call`;
  task status/result validation, TTL, pacing and cancel-completes races are
  covered; handles never leak; no traffic follows withdrawal. Inline mode
  keeps its existing worker slot and does not poll orphaned handles after a
  run has ended without operation-specific authorization.
- **Dependencies:** MCPP-002 and CXL-001 (shipped).

#### MCPP-005 - Durable external waits without occupied worker slots

- **Outcome:** Long remote tasks survive restarts and release scarce execution
  slots while Turing retains authoritative lifecycle truth.
- **Entry gate:** Review a durable operation-custody design before implementation:
  post-dispatch consent lifetime, exact endpoint/credential binding, allowed
  poll/cancel methods, deadlines, revocation/deletion fences, cancellation
  receipts and result retention. Existing active-run dispatch checks cannot
  simply be bypassed after the dispatch turn completes.
- **Scope:** Deferred tool receipts, orchestrator-owned polling, version-gated
  worker acknowledgement and one generalized join across remote tasks and
  specialist results. Initially attended local-model turns only; no automatic
  reuse of a parent's consent for a remote-provider continuation.
- **Likely files:** Repository/schema and lifecycle, orchestrator poller,
  runtime job/worker contract, generalized result join and events.
- **Acceptance:** With concurrency one, another turn finishes while a task
  waits. Mixed tasks and specialists join once across crashes. Cancellation,
  revocation and deletion stop unauthorized traffic and late writes. Lost
  creation responses remain uncertain; no mutation is automatically repeated.
- **Dependencies:** MCPP-004, TUR-010, CXL-001, and the merged team join/lifecycle
  foundation through #162. MCPP-006 and EVAL-001 are default-enable gates;
  AGT-000 still gates increased concurrency.

#### MCPP-006 - Reopenable remote-operation UI

- **Outcome:** Waiting, cancellation acknowledgement and unknown remote outcomes
  remain visible and distinct after reconnect/reopen.
- **Scope:** Public operation read/cancel contracts, bounded status messages,
  cards, durable event reconciliation and continuation attribution; reuse
  team-client components where their contracts fit.
- **Likely files:** Operation proto/service, Flutter API/mappers/chat/cards,
  widget and replay fixtures.
- **Acceptance:** Every task state and cancellation race has live/reopen parity;
  no opaque remote handle is shown; a completed dispatch turn can still expose
  a separately cancellable pending operation without rewriting that run.
- **Dependencies:** MCPP-005 operation APIs and the team-client delivery slice
  ([design slice 3](superpowers/specs/2026-10-03-turing-orchestrator-agent-team-design.md#14-delivery-slices),
  shipped in [#163](https://github.com/mcasillas17/TuringAgent/pull/163)).
  The reusable team UI prerequisite is satisfied; the MCP remote-operation
  APIs and UI are still pending and must ship before deferred execution
  becomes default-on.

#### MCPP-007 - Explicit user input for MCP operations

- **Outcome:** Supported form elicitation can pause an operation for the user
  without turning model output into consent.
- **Scope:** Bounded MRTR and task `inputRequests`/`tasks/update`, user-only
  answers, destination-bound input disclosure, deduplication and audit.
  First delivery supports non-mutating tools only.
- **Likely files:** Wire codec, operation/input services, consent contracts
  and Flutter forms.
- **Acceptance:** Negotiated MRTR uses a new request ID, unchanged canonical
  tool arguments and the issued opaque state; lost-response replay remains
  forbidden. Undeclared input types, model answers, stale keys and excessive
  rounds fail closed. Sensitive URL-mode flows are not downgraded to forms.
- **Dependencies:** MCPP-002, MCPP-004, MCPP-006 and TUR-021's approval boundary.
  EVAL-001 and explicit input-disclosure evidence gate default enablement.

#### MCPP-008 - Optional Tasks for Turing MCP hosts

- **Outcome:** A future scoped MCP host can observe a Turing operation through
  task handles without becoming its policy authority.
- **Scope:** Separate host-facing task mapping, ownership, bounded retention
  and authentication on every task method; not tasks in bundled tool servers.
- **Acceptance:** Cross-client task access, guessing and stale credentials fail;
  cancellation never grants rollback or bypasses the Turing lifecycle.
- **Dependencies:** MCPX-001 and a reviewed server-task mapping. Optional.

#### MCPP-009 - Versioned protocol and extension evidence

- **Outcome:** Future protocol updates are based on pinned facts and behavior,
  not SDK version numbers or moving draft pages.
- **Scope:** Release-time core/extension/schema review, immutable fixture
  provenance and an independent compatibility matrix.
- **Acceptance:** Each adopted requirement names its revision and evidence;
  stable extensions are distinguished from experimental/draft material;
  missing SDK Tasks support is not mistaken for task conformance.
- **Dependencies:** None. This is a review checklist, not a recurring
  automation or an automatic protocol/policy upgrade.

## Priority spine after the first ten

This is a dependency-aware recommendation, not a second immutable queue:

1. Finish resilience: TUR-023, TUR-011, TUR-024, TUR-014, TUR-025,
   TUR-012, AUD-001, EVT-001.
2. Establish whole-run evidence with EVAL-001; its TUR-010, TUR-021 and
   MEM-003 prerequisites have shipped.
3. Build measurable learning: MEM-004, MEM-016, MEM-008, MEM-017,
   MEM-009, then MEM-010/MEM-011 and MEM-012/MEM-015.
4. Establish concurrency evidence and provider metadata: AGT-000 and PROV-002.
5. Ship native mobile: MOB-001 and MOB-002.
6. Complete safe skill import/authoring: SKL-002 through SKL-004.
7. Add outbound-only channels: CHN-000 through CHN-002, Telegram, then Slack.
8. Add local interoperability: CON-REG-001 and MCPX-001.
9. Add bounded delegation: AGT-001/002 have shipped behind
   `TURING_AGENT_TEAM_ENABLED`, whose default waits only for EVAL-001
   (item 2). Next: A2A-001, then optional A2A-002 and AGT-004. AGT-000
   (item 4) gates only raising `TURING_MAX_CONCURRENT_RUNS_GENERAL` above 1.
10. Add executable extensibility: XTOOL-001 before XTOOL-002.
11. Add webhook channels, account connectors, longevity, and modalities only
    when their gates pass.

Alongside that product sequence, run the bounded MCP maintenance track:
MCPP-001 then MCPP-002; MCPP-003 and MCPP-004 can follow independently.
MCPP-005/006 provide the long-wait capacity and UI benefit, behind explicit
custody and EVAL-001 gates. MCPP-007 follows with user-input support; MCPP-008
waits for the separate host bridge. Do not bypass those gates to advertise
the latest protocol revision.

An operator track also runs alongside: OPS-001, which a new server needs before
anything else, has shipped; OPS-002 and OPS-003 follow.

## Success measures

Turing is approaching the north star when all of these are continuously true:

- a 7B-class local model completes the checked-in core scenario suite;
- no default path sends content off the machine;
- every remote destination and data category is visible before use;
- every mutation has a matching approval or exact automation allowlist;
- queue, cancellation, retry, recovery, and terminal state survive restart;
- retrieval and learning quality are measured, including abstention and stale
  belief use;
- automatic learning creates only reviewable candidates;
- every active belief has provenance, revision, correction, and withdrawal;
- portable skills can be imported without importing tool authority;
- generated tools cannot escape their sandbox or enter the trusted process;
- mobile and channel identities are individually revocable;
- channel platforms are never described as end-to-end encrypted to Turing;
- Turing can distinguish model inference, MCP tool use, A2A delegation, ACP
  editor integration, and account connections in protocol and UI;
- exports and backups are restorable, retention is bounded, and encryption
  claims match actual key custody;
- new capabilities preserve the home hub as the only durable policy authority.

## Primary sources used for this roadmap

External product and protocol claims were checked against primary sources:

- [Hermes Agent repository](https://github.com/NousResearch/hermes-agent)
- [Hermes Agent documentation](https://hermes-agent.nousresearch.com/docs/)
- [Hermes memory](https://hermes-agent.nousresearch.com/docs/user-guide/features/memory)
- [Hermes skills](https://hermes-agent.nousresearch.com/docs/user-guide/features/skills)
- [Hermes security](https://hermes-agent.nousresearch.com/docs/user-guide/security)
- [Hermes messaging](https://hermes-agent.nousresearch.com/docs/user-guide/messaging)
- [Hermes architecture](https://hermes-agent.nousresearch.com/docs/developer-guide/architecture)
- [Agent Skills specification](https://agentskills.io/specification)
- [MCP implemented legacy specification](https://modelcontextprotocol.io/specification/2025-11-25)
- [MCP current specification](https://modelcontextprotocol.io/specification/2026-07-28)
- [MCP Tasks extension, pinned specification](https://github.com/modelcontextprotocol/ext-tasks/blob/899c1bbc869567670093face3591b39c28ce3219/specification/2026-07-28/tasks.md)
- [MCP adoption research and immutable sources](architecture/mcp-protocol-adoption.md#primary-sources)
- [MCP Registry](https://modelcontextprotocol.io/registry/about)
- [A2A 1.0 specification](https://a2a-protocol.org/latest/specification/)
- [Agent Client Protocol](https://agentclientprotocol.com/overview/introduction)
- [OpenAI MCP and connectors](https://developers.openai.com/api/docs/guides/tools-connectors-mcp)
- [Anthropic OpenAI SDK compatibility](https://platform.claude.com/docs/en/cli-sdks-libraries/libraries/openai-sdk)
- [Gemini OpenAI compatibility](https://ai.google.dev/gemini-api/docs/openai)
- [GitHub Copilot MCP support](https://docs.github.com/en/copilot/how-tos/provide-context/use-mcp-in-your-ide/extend-copilot-chat-with-mcp)
- [Slack request verification](https://docs.slack.dev/authentication/verifying-requests-from-slack)
- [Slack Socket Mode](https://docs.slack.dev/apis/events-api/using-socket-mode)
- [Telegram Bot API](https://core.telegram.org/bots/api)
- [WhatsApp Cloud webhooks](https://developers.facebook.com/documentation/business-messaging/whatsapp/webhooks/overview)
- [Microsoft Teams conversational agents](https://learn.microsoft.com/en-us/microsoftteams/platform/bots/build-conversational-capability)
- [Tailscale Serve](https://tailscale.com/docs/reference/tailscale-cli/serve)
- [WebAuthn Level 3](https://www.w3.org/TR/webauthn-3/)

## Maintaining this document

- Update the inspected baseline whenever status changes.
- Reconcile the guarded inventory and its witnesses in
  `tools/docs/status_claims_test.go`; run `go test ./tools/docs -count=1`.
- A task becomes **shipped** only with a merged commit and acceptance evidence.
- Do not duplicate task status in archived plans.
- Preserve existing stable task IDs and meanings.
- New tasks require an outcome, scope, dependencies, and falsifiable
  acceptance criteria.
- When a task spans independent subsystems, split it before implementation.
- Implementation guidance must name the affected system boundaries, tests, and
  observable completion criteria.
