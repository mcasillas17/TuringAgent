# TuringAgent — North Star

**Status:** living document. The scope decisions below (multi-agent as a goal, invariants vs. deferrals, destructive-file-op scope) were made by the project owner on 2026-08-14.

Supersedes the stack claims in `docs/superpowers/specs/2026-05-09-project-turing-v1-design.md`, which still describes a Node.js/TypeScript orchestrator over REST/WebSocket. That was replaced by Go + gRPC in #10.

**Last verified against the code:** 2026-08-19.

---

## What TuringAgent is

A **local-first personal AI orchestration platform** — a private assistant stack that runs entirely on your own machine, owns its own state, and can use real tools under your control.

Not a chatbot. Not a wrapper around someone else's API. The distinguishing claim is that the orchestration — sessions, model routing, tool execution, approvals, memory, audit — is *yours*, running on hardware you own, against models you choose.

## Who it is for

Someone who wants a capable assistant without renting their data to anyone. Concretely: a developer or technical user with a machine that can run a local model, who wants an assistant that remembers, can act on their files and system, and never phones home unless explicitly told to.

The default path is fully local (Ollama). An OpenAI-compatible provider exists but is **opt-in per request**, never the default.

## The north star

> A private assistant that remembers what you told it, can actually do things on your machine, and never surprises you.

Three commitments, in priority order when they conflict:

1. **Private by construction.** Local-first is not a deployment option, it is the architecture. Secrets in a local `.env`, data in local SQLite, file tools confined to a sandbox, MCP servers never published to the host. **One exception, deliberate:** third-party credentials live in the database, sealed with AES-256-GCM under `TURING_INTEGRATION_KEY` — which stays in `.env` and is never written to `data/`. That makes a stray copy of the database useless without the key, and buys nothing against someone who can read the backend directory. With no key configured, connecting an account is refused rather than stored in the clear. Exactly one port is exposed — the orchestrator's public API, fixed to `127.0.0.1` with host port `3000` by default and guarded by a bearer API key minted by `init.sh`. That key is the whole client-side trust boundary today, which is why open question 4 is not academic.
2. **Trustworthy over capable.** An action the user did not sanction is a defect, even if useful. Mutating file tools require an explicit approval bound to the exact arguments; a run that retries, stalls, or draws on old context says so.
3. **Useful with the models people can actually run.** The target is a 7B-class local model on consumer hardware, not frontier ones (the current default and its measured footprint live in CLAUDE.md, which is easier to keep honest). When a small model gets something wrong in a recoverable way, the platform recovers rather than blaming the model — the zero-argument tool call and unknown-tool recovery paths exist for exactly that.

### How we would know this is working

A north star that cannot be falsified is decoration. These are the checks:

- A privacy claim is falsified the moment any default path sends data off the machine, or any port beyond `:3000` is published.
- Commitment 1 is also falsified by anything the user cannot withdraw.
  Whole-session withdrawal removes application-owned content from supported
  reads/search/recall, fences work and provenance capabilities when deletion
  begins, removes session-owned sandbox artifacts on successful finalization,
  and tells existing subscribers with one terminal event. A deliberate
  migration exception remains: files that predate session provenance at the
  sandbox root are classified `retain_legacy_unowned`, counted in the receipt,
  and never silently claimed or deleted. SQLite runs with `secure_delete` off
  and WAL on, so logical withdrawal is not byte-level forensic erasure: freed
  pages, WAL/shm files, snapshots, device remapping, and backups can retain
  bytes outside Turing's control.
- Commitment 2 is falsified by any state where the user waits with no indication why, or any mutation that happened without a matching approval record.
- Commitment 3 is falsified when the honest answer to "why did that fail?" is "use a bigger model."

## Principles the code already embodies

Each is a decision already made and defended in review, cited to where it happened. They are written down so the next decision matches.

**The user is never left guessing.** A run that retries, recovers, fails, is abandoned, exhausts its attempts, completes without content, hits the tool-iteration cap, answers from recalled context, omits optional prompt material, or stops at its configured output cap leaves durable typed state or a durable notice (#23, #24, #30, #33, TUR-009, TUR-020). Reopening reads the same terminal truth as the live stream. Silence is indistinguishable from a hang, so anything that delays or shapes an answer must be visible.

**Recalled context is attributed.** Memory that arrives unattributed reads as confabulation. If an answer draws on an earlier conversation, the user is told (#33).

**Approvals are single-use and argument-bound.** A bundled mutating file tool
needs a short-lived HS256 token carrying `iss`/`sub`/`aud`/tool/`args_hash`,
verified by the MCP server against the actual call and consumed exactly once.
For a non-bundled server, the orchestrator verifies the same run/tool/argument
binding and consumes before proxy dispatch. Approving one call does not approve
the next.

**Small-model accommodations are features, not hacks.** Zero-argument tool calls (#21) and recoverable unknown-tool errors (#29) exist because real local models make those mistakes. Meeting the model where it is beats requiring a better one.

**The client is thin; the orchestrator owns state.** Clients render a protocol; adding one must not mean reimplementing orchestration. The portability asset is `proto/turing/v1/*.proto` — note that nothing currently *enforces* wire compatibility (no `reserved` field numbers, no breaking-change check in CI; `tools/proto/check.sh` guards codegen determinism only). Treat the contract as stable by convention, not by tooling.

## What is true today

| Capability | State |
|---|---|
| Model-driven tool calling | Working, live-verified against a real model (#19, #27) |
| Dynamic tool discovery | Working; runtime reports its registry to the orchestrator (#17, #26) |
| Cross-session recall | Working — SQLite FTS5, keyword search, attributed to the user (#15, #18, #25, #33) |
| Context budgeting | Working — provider caps and output reservations are explicit, Ollama `num_ctx` is pinned to a stable per-request bucket below its cap, omissions are durable run events, and live tool protocol messages/correlation are never dropped (TUR-020) |
| Stable session titles | Working — derived deterministically from the first usable user turn, persisted by the orchestrator, and streamed to subscribed clients |
| Approvals | Working; single-use argument-bound JWT, consumed over internal gRPC |
| Audit | **Write + redacted read.** Selected approval, tool, integration, routing, deletion, memory, and auth-failure actions are recorded today — not every mutation; the memory reconcile and erasure paths record their own redacted `memory.*` rows (id and status, never content) through the same recording API, while a retry writer could do the same but none is implemented yet. `AuditService.ListAuditEntries` (public gRPC, same bearer auth as everything else) reads back what is recorded through a per-action typed field allowlist, including the approval comment or denial reason a person actually typed — free text the service discloses deliberately and cannot content-inspect, so it is not the place to paste a secret. Filterable by correlation/action/time, keyset-paginated, absent/present/scrubbed payload states. The Flutter client can call it (`TuringApi.listAuditEntries`), but there is no viewer screen yet — see [Audit read API](architecture/audit-read-api.md) |
| Telemetry | Working, and **local by construction** — aggregation over the orchestrator's own SQLite, served to the user's own client, with no collector, no identifier and no write path. Token counts are captured from what a provider reports (Ollama's `prompt_eval_count`/`eval_count`, an OpenAI-compatible `usage` object) and are **NULL when nothing reported them**; nothing estimates a token count anywhere. Runs routed off the machine are attributed per run at enqueue, so a later settings change cannot rewrite what left |
| Streaming + resilience | Working; reconnect, requeue, lease recovery, run-visibility notices (#24, #30, #33) |
| Durable run outcomes | Working - `agent_runs` owns versioned lifecycle/outcome truth; message history and live events carry the same redacted snapshot, and Flutter reconstructs localized recovery/terminal cards after reopen (TUR-009) |
| Job queue | Durable: SQLite job table with leases, fencing token, heartbeat renewal, orphan recovery, 3-attempt cap |
| Tool servers | Registry-backed: two bundled servers plus disabled-by-default local-container and remote-URL imports; stdio refused |
| Skills | File-backed `SKILL.md` library under `turing-backend/skills/`. Enabled metadata is indexed for every run; bodies and references load progressively only after every declared capability is granted. Grants gate loading and do **not** authorize tools. The 0011 upgrade retains legacy rows in a migration-only recovery table and re-exports them on startup; conflicts preserve recovery, and application code never removes nonempty rows. Cleanup is an offline/manual operator action after the files are verified. Enabled skill text selected by a routed run leaves the machine, and the routing picker says so |
| Curated memory | **User-owned vault shipped.** An Obsidian-compatible Markdown folder at `turing-backend/memory/` (mounted read/write at `/memory`): `persona.md` and `profile.md` pinned into every run, `beliefs/` reached through `memory.search`/`memory.read` as framed evidence, `inbox/` holding proposals that influence nothing until the user promotes them. `memory.remember` can write only into `inbox/`; promotion is a user action by RPC or by moving the file. SQLite holds identity, evidence, search and the erasure contract; a session deletion withdraws its candidates and evidence and rewrites the citations, while content the user promoted survives as theirs. Remote runs disclose it as its own consented category with a re-derived fingerprint. What is **not** here: automatic extraction, revision history for vault files, semantic retrieval, and sensitivity filtering — see [Memory governance](architecture/memory-governance.md) Amendment 1 |
| Third-party accounts | **GitHub-first consumer shipped.** Connections hold a credential the user minted themselves under explicit consent and revocation. GitHub tools open the named credential once per call in the orchestrator, behind signed per-run egress coverage and caller-side approval; IMAP, CalDAV, Notion, and OAuth remain deferred |
| Agents | **One** (`general_assistant`) behind an executor *interface* with one implementation. Multi-agent is a **goal** — see below |
| Process split | **Shipped** — the agent runtime is its own container, leased over a bidi gRPC stream. (It is *not* its own Go module; only `mcp-files` and `mcp-system` are.) |
| Clients | **One** (Flutter, macOS-focused). Codegen emits Go and Dart only; both are consumed today |
| Providers | Ollama (default), OpenAI-compatible with one-time per-run destination/category consent |

Context admission is conservative rather than tokenizer-exact: built-in providers measure their exact serialized request, count one UTF-8 byte as an upper bound of one prompt token, and reserve configured output tokens inside the window. Recall deduplicates against admitted history and converges if the suffix changes, so a budgeted-out current-session turn is not silently excluded from both paths. Oversized tool-result bodies can be replaced by explicit omission markers without dropping the tool message or its correlation ID. The operator configures each provider/model window; Turing does not yet discover model capabilities or persist exact provider token usage. Provenance-preserving summaries remain MEM-014, not part of TUR-020.

Known gaps, honestly: a requeued run with no worker waits indefinitely; historical tool cards and nonterminal run notices cannot be placed back into the transcript; partial live model deltas are not guaranteed to survive reopen; there is no explicit cancel-intent API, so the current transport path can claim only abandonment; curated memory now exists as a user-owned vault, but Turing does not yet learn on its own — nothing extracts a candidate from a conversation, vault beliefs carry no revision history or supersession, and there is no sensitivity filtering over what a consented remote run may carry; audit is now readable through a redacted public API and a thin Flutter client call, but there is no large audit viewer, and this API surfaces only the actions already recorded today - it does not make memory decisions or run retries inspectable beyond the safe typed lifecycle/outcome and subsidiary categories already recorded.

Commitment #1's sharpest gap is now materially closed: **whole-session
withdrawal is a durable state machine.** `SessionService.DeleteSession` starts
or advances a typed receipt; an active or externally blocked operation remains
visible and retryable rather than claiming success. Once deletion begins,
session reads, FTS search, recall, event replay, new messages, approvals and
tool calls fail closed. Existing subscribers receive exactly one terminal,
non-replayed deletion event only after artifact cleanup, audit scrub and the
database cascade commit. Audit keeps only a scrubbed tombstone. The client
removes a session only after a completed receipt or terminal event.

Still missing: **message-level deletion**, and any way to forget one fact without deleting the conversation around it — a curated belief can be deleted as a whole note, but a single statement inside a message still cannot be withdrawn on its own (MEM-011).

## Invariants — permanent, not deferrals

These are not capabilities we are declining. They are the properties the rest of the system is built on; violating one voids a commitment above, so they hold regardless of what gets built later.

- **Nothing leaves the machine by default.** A remote provider requires a
  one-time signed disclosure and explicit confirmation for the exact request,
  destination, tools, named skills and their content ceilings, context flags,
  and data categories. Consent is
  run-owned, not a session/provider preference; redirects and local-to-remote
  fallback are refused, and background work cannot inherit it. See
  Remote MCP endpoints join that exact run-owned decision and disclose tool
  arguments/results; enabling a server is not consent to reach it. See
  [Remote egress policy](architecture/remote-egress-policy.md).
- **Derived state cannot outlive its source.** Every application-owned table is classified, and user-derived state needs cascading provenance to its declared source. SQLite-managed indexes must prove equivalent transactional deletion; only an explicitly justified, content-free scrubbed audit tombstone may survive withdrawal. The one amendment to that reading is named and written down: content the user promoted into the vault by an explicit act, with the full text shown, and editable in the vault, is authored by that act and survives the deletion of the session it was proposed in — while every machine-owned row about it cascades away and every citation of the deleted source is rewritten to `withdrawn`. The trust, scope, writer, egress, retention, correction, export, deletion, and physical-erasure limits are defined in [`docs/architecture/memory-governance.md`](architecture/memory-governance.md) and enforced by the DB schema-invariant tests.
- **Every mutation is approved, argument-bound, and single-use.** New mutating capability inherits the existing approval flow; it does not get its own weaker one.
  - **Qualified by third-party MCP and integrations enforcement.** Bundled `mcp-files` verifies
    and consumes at the callee. A server we did not write receives ordinary
    JSON-RPC and cannot enforce Turing's token, so the orchestrator validates
    the run/server/tool/argument binding and consumes immediately before
    dispatch. What changes is who enforces, not whether approval exists. The
    guarantee is narrower: it holds because the orchestrator proxy is the only
    path holding that server's endpoint bearer, not because the third-party
    process would reject a direct forged call. The approval-consumer identity
    remains exclusive to bundled `mcp-files`.
  - **Qualified once, by automations.** A scheduled run has nobody to ask, so an automation carries a per-automation allowlist of specific `(server, tool)` pairs — never global, never a wildcard, never inherited by a conversation the user drives by hand. What that buys is *when* the decision is made, not *whether*: the orchestrator still creates the approval and still grants it through the same signing and state transition a person's click takes, so the token mcp-files verifies is the same short-lived, single-use, `args_hash`-bound token it always was. What is genuinely weaker is that consent is given in advance and in general ("this automation may run `files.update`") rather than in the moment and in particular ("write *this* to *that* path"). Unattended approvals are recorded with `actor_type = 'automation'` so an operator can tell them from a person's afterwards. A tool an automation was not pre-approved for fails the run rather than waiting for someone who is not there.
- **Bundled tools stay confined to the sandbox.** Capability may grow inside
  that reviewed boundary; nothing bundled gets an escape hatch out of it. A
  third-party process is explicitly labelled **not sandbox-confined** because
  Turing cannot truthfully claim confinement for code it did not write.
- **Skill text, retrieved integration content, and memory files are untrusted input, not authority.** A copied `SKILL.md`, a retrieved issue/file, a belief note, or a search result out of the vault may guide an answer only after its gates. None of them can override system/user precedence, tool policy, approval, or egress policy, and their contents never become tool permissions. Memory is framed as attributed evidence when it reaches a prompt, with one deliberate exception: `persona.md` is pinned unframed, because a human being is its only possible author — the agent has no write path to it at all. `profile.md` pins **framed**, because the agent may propose an edit the user accepts, and everything reached through `memory.search`/`memory.read` returns inside a per-call nonce frame. `inbox/` proposals are inert until promoted: nothing pins them, no tool argument can name them, and they influence no run.
- **The orchestrator owns durable state and control flow.** The job queue, leases, fencing, retries, recovery, and event streaming are ours. This is what was previously written as "no graph orchestration frameworks" — that framing was wrong. The real constraint is that nothing may take ownership of those, because they are the hard-won parts (#30, #31, #33).
- **The backend stays a single language.** It is 100% Go today. A framework requiring a Python or Node runtime in the backend costs a second toolchain, image, and dependency surface — that cost, not the abstraction, is the reason LangGraph-style tools are a poor fit here.

**On agent frameworks specifically:** a Go library that helps compose agent logic is fine and is *not* refused. What is refused is anything that wants to own durable state, dispatch, or retries, or that drags in a second language runtime. What is left for a framework to do is smaller than it looks: the durable half of orchestration already exists, and the queue is already *parameterised* by agent (`jobs.agent_id`, `ClaimNextJob(agentID, ...)`). It is not, however, close to multi-agent-ready — see "Decided" for what a second agent actually costs.

## Deferred, in order

Everything here is wanted eventually — this is a "not yet" list with gates, which is how the v1 design framed it (*"later phases can add..."*). Each entry says what must be true before it lands. Working on one out of order is allowed, but should be a deliberate call, not an accident.

1. **Message-level deletion.** Session deletion has shipped; removing a single message, or forgetting one fact without deleting the conversation, has not. Curated memory now exists, so the missing half is the message side: MEM-011's rules for deleting a message that a run anchor, a recall index, or a promoted note's provenance still points at.
2. **Destructive file operations — decided scope: soft-delete and move, approval-gated.** `files.move` becomes a real move; `files.delete` relocates into a sandbox-local trash rather than unlinking. Both require the same argument-bound approval as write. Recoverable by construction, so an approval given in error is not permanent. *Gates,* all three concrete and already implied by `mcp-files`:
   - **Tamper-resistance vs. visibility pull opposite ways.** `normalizeSandboxPath` rejects any path containing a reserved internal name (`safe_fs.go:119-121`), which is exactly how you stop a tool resurrecting or rewriting trashed content — but it also means the file tools cannot browse it. Making trash visible to the *user* therefore needs a separate affordance (a client view or its own RPC), not a tool path.
   - **Collisions.** Deleting two files of the same name must not clobber; the unique-naming pattern in `createTemporaryFile` is the precedent to follow.
   - **Growth.** Trash that is never emptied silently consumes the sandbox. Retention has to be decided before this ships, not after.
3. **Shell and native automation, under approval.** Wanted, but this is the capability most likely to void an invariant. *Gate:* it runs inside the sandbox, is approval-gated per invocation, and cannot be used to reach outside the sandbox. An unsandboxed shell is not a later phase — it is barred by the invariants above.
4. **External brokers and multi-machine distribution.** *Gate:* a measured failure of the SQLite queue under real concurrency — sustained lease-renewal loss, claim contention, or recovery that cannot keep up — captured with `MaxConcurrentRuns` raised above 1. "It feels like it needs a queue" does not qualify. Multi-agent will stress leases, fencing and recovery for the first time, so fix what that exposes before reaching for a broker.
5. **Vector/semantic memory.** *Gate:* a case where keyword recall demonstrably fails and embeddings demonstrably fix it. The concern stands — embeddings fail by producing confident nonsense — so this needs a test that can fail, not a vibe.
6. **Third-party OAuth, vision, voice, IoT, home automation.** Furthest out, and each needs its own answer to "how does this not phone home?" before it is designed.

   *Partially pulled forward, with the gate met:* **token-based** GitHub connections now have four orchestrator-owned tools. Nothing phones home in the background: each call is covered by the run's signed `(endpoint, connection_id)` decision, approval is caller-side and argument-bound, the credential is opened for one call and sent header-only through a hardened pinned-host transport, and retrieved content remains untrusted input. OAuth itself is still deferred and blocked by something outside the repo: it needs a client ID and secret registered to a published app plus a browser redirect, which a local-first install has no way to hold.

## How we decide what is next

One ordered list, derived from the commitments above so there is no second scoring system:

1. **Something the user is told, or shown, that is false.** Wrong beats missing. (Commitment 2)
2. **A privacy or consent boundary the code does not honour** — data that cannot be removed, an action without an approval record. (Commitment 1)
3. **Something that silently does nothing** — a hang, a dropped run, a notice nobody receives. (Commitment 2)
4. **A commitment the code does not yet honour** in a way users have not hit yet.
5. **A capability gap** that a real session actually hit. (Commitment 3)

Rule 1 currently matches nothing, which is a good sign rather than a reason to delete it.

A deferred item may be pulled forward at any time, but its gate still has to be met — see "Deferred, in order". Skipping a gate is the failure mode this document exists to prevent.

`docs/superpowers/plans/` is an **archive**, not a backlog: 12 of its 13 plans map to merged PRs. The only unshipped one is `2026-08-13-flutter-session-search.md` — the backend RPC exists; no Dart outside `lib/generated/` calls it. This document is what future plans should ladder up to.

---

## Decided

**Multi-agent is a goal** (2026-08-14). Precisely what this does and does not mean:

- The *process* split already shipped — the runtime is its own container, leased over a bidi gRPC stream. That is not the missing piece. (It is not its own Go module; only `mcp-files` and `mcp-system` are.)
- The missing piece is plural agents: `common.proto` has exactly one real `AgentId`, and there is one executor implementation.
- Most of the groundwork exists. Dispatch, claiming and capacity are already keyed by `agent_id`, so a second agent largely means a second worker pool rather than new orchestration.
- It will be the first thing to run agents concurrently. `MaxConcurrentRuns` has defaulted to 1, so leases, fencing and orphan recovery have never been under real concurrent load. Expect the next class of bugs there, and treat deferral 4 (brokers) as a response to evidence from this, not a prerequisite for it.
- It needs a plan, not a PR: a proto change to `AgentId`, per-agent tool policy, and a routing/handoff decision that no framework will be making for us (see invariants).

## Open questions — owner's call, not mine

1. **Is a second client a real destination?** Codegen emits only Go and Dart, both consumed today — there is no speculative stub generation. The portability asset is the `.proto` contract, which nothing currently protects from breaking changes. If a second client is real, that guard should exist.
2. **Is this for one person, or is it a product?** Everything above assumes single-user, single-machine, one bearer key. Multi-user would change auth, storage, and the approval model.

*(Previously open, now settled: multi-agent — see "Decided". The scope of "can do things on your machine" — answered by the invariants and the deferral order. Whether the non-goals were permanent — they were not; they are deferrals with gates, except the invariants.)*
