# TuringAgent North Star and Roadmap

**Status:** Canonical product direction and implementation backlog.

**Verified baseline:** `main` at `82eb7e7f` on 2026-09-02.

**Supersedes:** `docs/VISION.md` and
`docs/architecture/2026-08-18-personal-agent-audit.md` as active roadmap
documents. Those files remain historical decision records.

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

### Shipped and usable

| Capability | Current state |
|---|---|
| Local chat and tool loop | Ollama is the default; streaming, bounded tool iterations, zero-argument recovery, and unknown-tool recovery are implemented. |
| Durable orchestration | Sessions, runs, events, jobs, leases, fencing, retries, recovery, and reopenable run outcomes are persisted in SQLite. |
| File actions | Sandboxed read/list/search plus approval-gated create/update are implemented. |
| Approvals | Mutations use short-lived, argument-bound, single-use approval tokens. |
| Session management | Stable titles, pagination, search, rename, archive, restore, and durable whole-session withdrawal are implemented. |
| Recall | Cross-session FTS5 recall and scored search hits are implemented and attributed. |
| Context control | Provider context limits, output reserves, explicit omissions, and terminal notices are implemented. |
| Memory vault | `persona.md`, `profile.md`, `inbox/`, and `beliefs/` are user-owned Markdown; `memory.search`, `memory.read`, and `memory.remember` exist. |
| Skills | File-backed `SKILL.md` loading, progressive disclosure, enablement, and Turing capability grants exist. |
| Automations | Interval and daily unattended runs with per-automation tool allowlists exist. |
| GitHub integration | Connected credentials can drive issue listing, issue reading, file reading, and issue comments under egress and approval policy. |
| Tool registry | Bundled, local-container, and remote-URL tool endpoints can be registered, disabled, inspected, and assigned policies. |
| Remote inference | A conversation can be routed to an OpenAI-compatible endpoint under per-run disclosure. |
| Audit and telemetry | Redacted audit read APIs and local-only usage aggregation exist. |
| Contract safety | Protobuf breaking checks and deterministic generated-code checks run in CI. |

The configuration-driven registry scope originally named `CON-002` is treated
as shipped by the registry, import, enablement, token-rotation, and policy work
merged in PRs #73, #81, and #82. Dependencies on `CON-002` below are therefore
satisfied. `CON-001` remains open because protocol initialization and
capability negotiation did not ship with that registry.

Other stable dependencies referenced by pending tasks are already satisfied:
`TUR-001` (idempotent sends), `TUR-004` (session withdrawal), `TUR-006`
(service-scoped identities), and `MEM-001` (memory governance and derived-state
contract) are shipped on the verified baseline.

### Partial or misleading today

| Capability | Honest status |
|---|---|
| Personal learning | The vault and proposal workflow exist, but normal conversation does not automatically extract candidates, revise beliefs, or measure learning quality. |
| External agents | `ExternalAgentService` routes to remote model endpoints. It is not agent-to-agent delegation. |
| MCP | Turing implements an HTTP JSON-RPC subset for `tools/list` and `tools/call`, but not the MCP initialization and capability lifecycle. |
| Skills ecosystem | The parser accepts a Turing-specific subset. It rejects several standard Agent Skills fields and has no import, quarantine, authoring, or marketplace flow. |
| Integrations | GitHub has functional tools. The INT-001 candidate makes IMAP, CalDAV, and Notion descriptor-only, rejecting new credentials while retaining earlier-release rows for explicit cleanup; this change is pending merge after DOC-001. Google, Microsoft, and Slack account connections and tools are not implemented. |
| Mobile | Flutter has iOS/Android scaffolding and responsive layouts, but the backend is loopback-only, the Android release manifest lacks network permission, and there is no device pairing or non-loopback TLS contract. |
| Audit UX | The API exists; a user-facing audit viewer does not. |
| Multi-agent | The runtime is process-separated, but there is one `AgentId`, one general executor, and a default concurrency of one. |

### Not implemented

- automatic memory extraction and belief revision;
- per-session and per-turn memory controls;
- message-level deletion and single-fact withdrawal;
- a memory and personal-agent evaluation suite;
- native Anthropic and Gemini provider transports;
- secure mobile reachability and per-device identity;
- Telegram, Slack, WhatsApp, or Teams channels;
- a conformant public or private Turing MCP server;
- A2A client/server behavior or ACP editor integration;
- agent-authored skills or tools;
- sandboxed executable tool generation;
- plural local agents and parent/child delegation;
- consistent export, backup/restore, bounded retention, and database encryption;
- evidence-gated vision, voice, and richer prospective memory.

## Envisioned scenarios: what works now

| Scenario | Status | Boundary |
|---|---|---|
| "Search what we discussed last month." | Works | FTS5 session search and attributed recall. |
| "Read this project file." | Works | File must be inside the sandbox. |
| "Update this sandbox file." | Works | Requires an argument-bound approval. |
| "Remember that I prefer concise answers." | Partial | The model may call `memory.remember`; the proposal is inert until the user promotes it. There is no automatic extraction. |
| "Comment on this GitHub issue." | Works with setup | Requires a connected GitHub credential, per-run egress coverage, and tool approval. |
| "Run this report every morning." | Partial | The automation can run, but there is no mobile/channel delivery and unattended tools are limited to its explicit allowlist. |
| "Ask Claude/Gemini/ChatGPT instead." | Partial | A compatible remote model endpoint can answer under disclosure; this is model inference, not a conversation with another agent product. |
| "Delegate this coding task to Copilot." | Does not work | No vendor delegation adapter or A2A peer exists. |
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

Turing first becomes conformant on its existing bounded tool surface:
initialization, capability negotiation, ping/liveness, paginated tool listing,
and tool calls. Resources, prompts, sampling, elicitation, OAuth, and stdio are
not part of the first conformance task.

Registry discovery is read-only. Namespace verification is not security
review. Any discovered server is imported disabled and approval-required.

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

## First ten implementation tasks

### 1. DOC-001 - Canonical truth and status guard

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

- **Implementation status:** Implemented in the INT-001 candidate, pending merge
  after DOC-001; not yet shipped on `main`.
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
- **Dependencies:** No technical prerequisite. DOC-001 must land first; preserve
  its documentation/status guard when integrating this behavior.

### 3. TUR-010 - No-worker and queue-timeout truth

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
- **Dependencies:** TUR-019 (shipped).

### 7. CXL-001 - Explicit cancel intent

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

- **Outcome:** Turing supports plural agents with explicit routing and policy.
- **Scope:** A second `AgentId`, deterministic routing/handoff, per-agent tools,
  provider capabilities, capacity, events, and concurrency tests.
- **Acceptance:** Routing is visible; handoff preserves provenance; concurrent
  agents do not violate session ordering, leases, fencing, or approvals.
- **Dependencies:** TUR-010, TUR-011, TUR-014, TUR-018, TUR-019, CON-002, and
  AGT-000.

#### AGT-002 - Parent/child delegated runs

- **Outcome:** An agent can delegate bounded work without hiding control flow.
- **Scope:** Parent/child run identity, budgets, cancellation propagation,
  artifact return, and failure aggregation.
- **Acceptance:** A parent can spawn, observe, cancel, and collect a child;
  children cannot inherit undeclared tools, egress, memory, or approval.
- **Dependencies:** AGT-001 and EVAL-001.

#### AGT-003 - Visible planning and handoff

- **Outcome:** The user can inspect what is being delegated and why.
- **Scope:** Typed plan/handoff events, UI, audit, and replay.
- **Acceptance:** Hidden delegation is impossible; reopen shows the same plan,
  agents, scopes, and outcomes.
- **Dependencies:** AGT-002, AUD-001, and EVT-001.

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

## Priority spine after the first ten

This is a dependency-aware recommendation, not a second immutable queue:

1. Finish resilience: TUR-023, TUR-011, TUR-024, TUR-014, TUR-025,
   TUR-012, AUD-001, EVT-001.
2. Establish whole-run evidence with EVAL-001 as soon as TUR-010, TUR-021,
   and MEM-003 are complete.
3. Build measurable learning: MEM-004, MEM-016, MEM-008, MEM-017,
   MEM-009, then MEM-010/MEM-011 and MEM-012/MEM-015.
4. Establish concurrency evidence and provider metadata: AGT-000 and PROV-002.
5. Ship native mobile: MOB-001 and MOB-002.
6. Complete safe skill import/authoring: SKL-002 through SKL-004.
7. Add outbound-only channels: CHN-000 through CHN-002, Telegram, then Slack.
8. Add local interoperability: CON-REG-001 and MCPX-001.
9. Add bounded delegation: A2A-001, then optional A2A-002 and AGT-001/002.
10. Add executable extensibility: XTOOL-001 before XTOOL-002.
11. Add webhook channels, account connectors, longevity, and modalities only
    when their gates pass.

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
- [MCP specification](https://modelcontextprotocol.io/specification/2025-11-25)
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

- Update the verified baseline whenever status changes.
- A task becomes **shipped** only with a merged commit and acceptance evidence.
- Do not duplicate task status in archived plans.
- Preserve existing stable task IDs and meanings.
- New tasks require an outcome, scope, dependencies, and falsifiable
  acceptance criteria.
- When a task spans independent subsystems, split it before implementation.
- Implementation guidance must name the affected system boundaries, tests, and
  observable completion criteria.
