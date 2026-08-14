# TuringAgent — North Star

**Status:** living document, and partly a *proposal* — see "What we will not build", which asks for a decision rather than recording one.

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
| Agents | **One** (`general_assistant`) behind an executor *interface* with one implementation |
| Process split | **Shipped** — the agent runtime is its own container and Go module, leased over a bidi gRPC stream |
| Clients | **One** (Flutter, macOS-focused). Codegen emits Go and Dart only; both are consumed today |
| Providers | Ollama (default), OpenAI-compatible (opt-in per request) |

Known gaps, honestly: the client ignores `agent.run.failed` entirely, so run failures are silent unless a notice covers them; a requeued run with no worker waits indefinitely; startup-recovery notices are published before the gRPC servers exist and so reach no subscriber; there is no curated user memory, only keyword recall over raw messages; audit is not inspectable.

The sharpest gap is against commitment #1: **there is no way to delete anything.** No `DeleteSession`, no message deletion, nothing in the proto surface — the only way to remove what you said is deleting the SQLite file by hand. A system that remembers across sessions and cannot forget is not yet keeping its own first promise.

## What we will not build

**This section is a proposal, not an inheritance.** The v1 design listed nine non-goals, but explicitly framed them as deferrals: *"Later phases can add native Windows, semantic memory, richer agents, distributed workers, external integrations, vision, IoT, voice, and advanced native automation."* Converting deferrals into standing refusals is a **new decision** and needs the owner's signature. It is proposed here because a non-goal that is really a "not yet" cannot arbitrate anything.

Proposed standing refusals:

- **Vector/semantic memory.** Keyword recall over your own messages is legible and debuggable; embeddings are neither, and the failure mode is confident nonsense.
- **Graph orchestration frameworks** (LangGraph and friends). This bars adopting a framework, not multi-agent itself — see open question 1.
- **External brokers** (Redis, BullMQ, NATS). Note what already exists: a durable SQLite job queue with leases and a separate worker container. The refusal is of an external broker and multi-machine distribution, not of queuing.
- **Arbitrary shell, AppleScript, PowerShell, screenshots, keyboard/mouse control.** The sandbox is the product; an escape hatch that runs anything voids it. (Currently true: `os/exec` appears only in test files.)
- **Destructive file operations.** `files.delete` and `files.move` are disabled, and the code is stricter than this document — refused in three independent layers: not advertised, dispatcher-rejected, and policy-blocked ahead of any DB lookup.
- **Third-party OAuth integrations**, vision, voice, IoT, home automation.

Two of v1's nine are deliberately **not** carried forward: "native macOS app or bridge" and "native Windows app or bridge". Flutter desktop already is the client, and a second client is open question 2 rather than a refusal.

Adding any of the above should require revising this document first, not a PR that quietly introduces one.

## How we decide what is next

One ordered list, derived from the commitments above so there is no second scoring system:

1. **Something the user is told, or shown, that is false.** Wrong beats missing. (Commitment 2)
2. **A privacy or consent boundary the code does not honour** — data that cannot be removed, an action without an approval record. (Commitment 1)
3. **Something that silently does nothing** — a hang, a dropped run, a notice nobody receives. (Commitment 2)
4. **A commitment the code does not yet honour** in a way users have not hit yet.
5. **A capability gap** that a real session actually hit. (Commitment 3)

Rule 1 currently matches nothing, which is a good sign rather than a reason to delete it.

`docs/superpowers/plans/` is an **archive**, not a backlog: 12 of its 13 plans map to merged PRs. The only unshipped one is `2026-08-13-flutter-session-search.md` — the backend RPC exists; no Dart outside `lib/generated/` calls it. This document is what future plans should ladder up to.

---

## Open questions — owner's call, not mine

This document codifies what the code demonstrates. The following are genuine product decisions the codebase cannot answer:

1. **Is multi-agent a goal or a non-goal?** Precisely: the *process* split has shipped — the runtime is already a separate container leased over gRPC. What has never been used is the *multi-agent* part; there is one `AgentId` and one executor implementation. Committing either way would simplify a lot of future design.
2. **Is a second client a real destination?** Today codegen emits only Go and Dart, both consumed — there is no speculative stub generation. The portability asset is the `.proto` contract, which nothing currently protects from breaking changes. If a second client is real, that guard should exist.
3. **How far does "can do things on your machine" go?** Files and safe system tools exist today. The refusals above bar arbitrary automation, but the line between "sandboxed file tools" and "useful desktop assistant" is not drawn.
4. **Is this for one person, or is it a product?** Everything above assumes single-user, single-machine, one bearer key. Multi-user would change auth, storage, and the approval model.
5. **Do the proposed refusals stand?** See "What we will not build" — v1 called them deferrals, and this document proposes making them permanent.
