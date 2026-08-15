# TuringAgent — North Star

**Status:** living document. The scope decisions below (multi-agent as a goal, invariants vs. deferrals, destructive-file-op scope) were made by the project owner on 2026-08-14.

Supersedes the stack claims in `docs/superpowers/specs/2026-05-09-project-turing-v1-design.md`, which still describes a Node.js/TypeScript orchestrator over REST/WebSocket. That was replaced by Go + gRPC in #10.

**Last verified against the code:** 2026-08-13, at `e4ae748` (#33).

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

1. **Private by construction.** Local-first is not a deployment option, it is the architecture. Secrets in a local `.env`, data in local SQLite, file tools confined to a sandbox, MCP servers never published to the host. Exactly one port is exposed — the orchestrator's `:3000`, bound to `127.0.0.1` **by default but configurable** (`ORCHESTRATOR_PUBLIC_BIND_HOST`), guarded by a bearer API key minted by `init.sh`. That key is the whole client-side trust boundary today, which is why open question 4 is not academic — and why widening that bind host is the single easiest way to break commitment #1 by accident.
2. **Trustworthy over capable.** An action the user did not sanction is a defect, even if useful. Mutating file tools require an explicit approval bound to the exact arguments; a run that retries, stalls, or draws on old context says so.
3. **Useful with the models people can actually run.** The target is llama3.2-class local models, not frontier ones. When a small model gets something wrong in a recoverable way, the platform recovers rather than blaming the model.

### How we would know this is working

A north star that cannot be falsified is decoration. These are the checks:

- A privacy claim is falsified the moment any default path sends data off the machine, or any port beyond `:3000` is published.
- Commitment 1 is also falsified by anything the user cannot withdraw. Whole-session deletion now exists and removes the conversation from the search index; the check fails again the moment something the user said survives a delete somewhere they cannot see. **Three places it currently does:** files written into the sandbox by `files.*` tools are not session-scoped (`mcp-files` has no notion of a session), so they outlive the conversation; SQLite runs with `secure_delete` off and WAL on, so deleted text stays in freed pages until overwritten or `VACUUM`ed — deletion is row-level, not byte-level; and a client already subscribed to a deleted session is told nothing, since no event is published on deletion.
- Commitment 2 is falsified by any state where the user waits with no indication why, or any mutation that happened without a matching approval record.
- Commitment 3 is falsified when the honest answer to "why did that fail?" is "use a bigger model."

## Principles the code already embodies

Each is a decision already made and defended in review, cited to where it happened. They are written down so the next decision matches.

**The user is never left guessing.** A run that retries, exhausts its attempts, hits the tool-iteration cap, or answers from recalled context emits a notice (#23, #24, #30, #33). Silence is indistinguishable from a hang, so anything that delays or shapes an answer must be visible.

**Recalled context is attributed.** Memory that arrives unattributed reads as confabulation. If an answer draws on an earlier conversation, the user is told (#33).

**Approvals are single-use and argument-bound.** A mutating file tool needs a short-lived HS256 token carrying `iss`/`sub`/`aud`/tool/`args_hash`, verified by the MCP server against the actual call and consumed exactly once. Approving one write does not approve the next.

**Small-model accommodations are features, not hacks.** Zero-argument tool calls (#21) and recoverable unknown-tool errors (#29) exist because real local models make those mistakes. Meeting the model where it is beats requiring a better one.

**The client is thin; the orchestrator owns state.** Clients render a protocol; adding one must not mean reimplementing orchestration. The portability asset is `proto/turing/v1/*.proto` — note that nothing currently *enforces* wire compatibility (no `reserved` field numbers, no breaking-change check in CI; `tools/proto/check.sh` guards codegen determinism only). Treat the contract as stable by convention, not by tooling.

## What is true today

| Capability | State |
|---|---|
| Model-driven tool calling | Working, live-verified against a real model (#19, #27) |
| Dynamic tool discovery | Working; runtime reports its registry to the orchestrator (#17, #26) |
| Cross-session recall | Working — SQLite FTS5, keyword search, attributed to the user (#15, #18, #25, #33) |
| Approvals | Working; single-use argument-bound JWT, consumed over internal gRPC |
| Audit | **Write-only.** Rows are recorded; there is no read path in any proto or client |
| Streaming + resilience | Working; reconnect, requeue, lease recovery, run-visibility notices (#24, #30, #33) |
| Job queue | Durable: SQLite job table with leases, fencing token, heartbeat renewal, orphan recovery, 3-attempt cap |
| Tool servers | Two: safe system tools, sandboxed file tools |
| Agents | **One** (`general_assistant`) behind an executor *interface* with one implementation. Multi-agent is a **goal** — see below |
| Process split | **Shipped** — the agent runtime is its own container, leased over a bidi gRPC stream. (It is *not* its own Go module; only `mcp-files` and `mcp-system` are.) |
| Clients | **One** (Flutter, macOS-focused). Codegen emits Go and Dart only; both are consumed today |
| Providers | Ollama (default), OpenAI-compatible (opt-in per request) |

Known gaps, honestly: a live `agent.run.failed` or `agent.run.cancelled` now renders as an inline failure or cancellation card, but — like tool cards and run notices — that entry is suppressed on session reopen by the replay watermark, so a past failed or cancelled run can still surface as an unexplained empty turn; a requeued run with no worker waits indefinitely; startup-recovery notices are published before the gRPC servers exist and so reach no subscriber; there is no curated user memory, only keyword recall over raw messages; audit is not inspectable.

Commitment #1's sharpest gap is now partly closed: **whole-session deletion works.** `SessionService.DeleteSession` removes a session and cascades to its messages, runs, jobs, events, tool calls and approvals; the content leaves the FTS index too, so recall cannot resurface it. Audit rows survive with their content scrubbed, so the record still evidences that something happened without retaining what was withdrawn. Deleting a session with a run in flight is refused rather than orphaning the worker.

Still missing: **message-level deletion**, and any way to forget one fact without deleting the conversation around it. Those need curated memory, which does not exist yet.

## Invariants — permanent, not deferrals

These are not capabilities we are declining. They are the properties the rest of the system is built on; violating one voids a commitment above, so they hold regardless of what gets built later.

- **Nothing leaves the machine by default.** A remote provider is opt-in per request, never a default path, and no feature may introduce background egress.
- **Every mutation is approved, argument-bound, and single-use.** New mutating capability inherits the existing approval flow; it does not get its own weaker one.
- **Tools stay confined to the sandbox.** Capability may grow inside that boundary; nothing gets an escape hatch out of it.
- **The orchestrator owns durable state and control flow.** The job queue, leases, fencing, retries, recovery, and event streaming are ours. This is what was previously written as "no graph orchestration frameworks" — that framing was wrong. The real constraint is that nothing may take ownership of those, because they are the hard-won parts (#30, #31, #33).
- **The backend stays a single language.** It is 100% Go today. A framework requiring a Python or Node runtime in the backend costs a second toolchain, image, and dependency surface — that cost, not the abstraction, is the reason LangGraph-style tools are a poor fit here.

**On agent frameworks specifically:** a Go library that helps compose agent logic is fine and is *not* refused. What is refused is anything that wants to own durable state, dispatch, or retries, or that drags in a second language runtime. What is left for a framework to do is smaller than it looks: the durable half of orchestration already exists, and the queue is already *parameterised* by agent (`jobs.agent_id`, `ClaimNextJob(agentID, ...)`). It is not, however, close to multi-agent-ready — see "Decided" for what a second agent actually costs.

## Deferred, in order

Everything here is wanted eventually — this is a "not yet" list with gates, which is how the v1 design framed it (*"later phases can add..."*). Each entry says what must be true before it lands. Working on one out of order is allowed, but should be a deliberate call, not an accident.

1. **Message-level deletion.** Session deletion has shipped; removing a single message, or forgetting one fact without deleting the conversation, has not. It depends on curated memory (item 5's neighbour, not yet planned).
2. **Destructive file operations — decided scope: soft-delete and move, approval-gated.** `files.move` becomes a real move; `files.delete` relocates into a sandbox-local trash rather than unlinking. Both require the same argument-bound approval as write. Recoverable by construction, so an approval given in error is not permanent. *Gates,* all three concrete and already implied by `mcp-files`:
   - **Tamper-resistance vs. visibility pull opposite ways.** `normalizeSandboxPath` rejects any path containing a reserved internal name (`safe_fs.go:119-121`), which is exactly how you stop a tool resurrecting or rewriting trashed content — but it also means the file tools cannot browse it. Making trash visible to the *user* therefore needs a separate affordance (a client view or its own RPC), not a tool path.
   - **Collisions.** Deleting two files of the same name must not clobber; the unique-naming pattern in `createTemporaryFile` is the precedent to follow.
   - **Growth.** Trash that is never emptied silently consumes the sandbox. Retention has to be decided before this ships, not after.
3. **Shell and native automation, under approval.** Wanted, but this is the capability most likely to void an invariant. *Gate:* it runs inside the sandbox, is approval-gated per invocation, and cannot be used to reach outside the sandbox. An unsandboxed shell is not a later phase — it is barred by the invariants above.
4. **External brokers and multi-machine distribution.** *Gate:* a measured failure of the SQLite queue under real concurrency — sustained lease-renewal loss, claim contention, or recovery that cannot keep up — captured with `MaxConcurrentRuns` raised above 1. "It feels like it needs a queue" does not qualify. Multi-agent will stress leases, fencing and recovery for the first time, so fix what that exposes before reaching for a broker.
5. **Vector/semantic memory.** *Gate:* a case where keyword recall demonstrably fails and embeddings demonstrably fix it. The concern stands — embeddings fail by producing confident nonsense — so this needs a test that can fail, not a vibe.
6. **Third-party OAuth, vision, voice, IoT, home automation.** Furthest out, and each needs its own answer to "how does this not phone home?" before it is designed.

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
