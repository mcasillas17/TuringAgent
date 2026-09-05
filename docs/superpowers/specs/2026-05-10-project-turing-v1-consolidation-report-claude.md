# TuringAgent v1.0 Consolidation Report (Claude perspective)

**Historical report:** This record is not current implementation or status.
Use the [canonical roadmap](../../NORTH_STAR.md) for current truth.

**Author:** Claude Code session, 2026-05-10
**Companion to:** `2026-05-10-project-turing-v1-consolidation-report.md` (Copilot perspective)

This report is the parallel of Copilot's consolidation report, written from the perspective of the brainstorming session conducted in Claude Code on 2026-05-09–10. It exists because the two threads produced incompatible recommendations, and the user asked for a side-by-side so Copilot can review.

## Sources compared

- Copilot-approved spec: `docs/superpowers/specs/2026-05-09-project-turing-v1-design-copilot.md`
- Claude draft spec from today's brainstorming: `docs/superpowers/specs/2026-05-09-project-turing-v1-design-claude.md`
- Copilot's consolidation: `docs/superpowers/specs/2026-05-10-project-turing-v1-consolidation-report.md`

## Executive summary

The two threads agree on the product direction but make opposite implementation cuts.

The Copilot spec is a **product-capability-first** v1.0: ship a richer user-facing slice (JWT setup/login, optional cloud provider, files MCP with approvals, approval UI cards) inside a single-process orchestrator with in-process agents. Tradeoff: more user value sooner, less runtime isolation.

Today's Claude session is a **runtime-isolation-first** v1.0: ship a smaller user-facing slice (single API key, system MCP only, no approval UI exercised) inside a two-process backend with agent-direct MCP calls and per-capability network isolation. Tradeoff: less user value at v1.0, more architectural readiness for v1.1+ multi-agent and security-sensitive tools.

Both are coherent. The choice is about *what kind of v1.0 do you want to ship*. The conflict is not technical — it is scope and risk preference.

## Why this parallel report exists

Copilot's consolidation report claims the user "explicitly selected" several decisions (in-process executor, JWT login, files MCP in v1.0, orchestrator-mediated tool calls). Today's brainstorming session in Claude recorded the **opposite** selections via the AskUserQuestion tool. Both cannot be the user's current intent simultaneously.

This report does not assert that today's session is more authoritative. It documents what was selected today, with direct evidence, so the user can compare against the consolidation's claims and decide which thread to keep.

## Today's brainstorming decision log

These are direct user selections from this session's structured questions, in order. Each was a multiple-choice prompt; the user picked one option.

| # | Question | User's selection |
|---|---|---|
| 1 | Spec authority: new brief vs old Codex spec | "New brief replaces old spec" |
| 2 | v1.0 cut-line | "Pipe + tools + auth (phases 0–5)" — files MCP, semantic memory, native macOS PoC slip to v1.1 |
| 3 | First client | "Build on existing Flutter app, target macOS + Android" |
| 4 | Repo layout | "Keep turing-backend and turing-client split but split the client one into different apps depending on the platform" (custom answer) |
| 5 | Architecture approach | "Approach B" — split processes (orchestrator + agent-runtime), SQLite-backed jobs table |
| 6 | MCP call path (after extended discussion) | "Yes, layered option C sounds good" — agent-direct MCP with Docker network isolation, per-agent tokens, audit beacons, approval JWTs |
| 7 | Secrets approach | "let's go with a [.env], we'll consider a different approach later" |
| 8 | Auth model | "API key (simple, ours)" |
| 9 | Model providers | "Ollama + OpenAI-compatible (Copilot's)" |

Item 9 is the one decision that aligns between the two threads. Items 1–8 do not.

## Major conflicts

### 1. Runtime process model

| Topic | Copilot | Claude (today) | Note |
|---|---|---|---|
| Agent runtime | In-process `AgentExecutor` inside orchestrator | Separate `turing-agent-runtime-general` container, long-polling internal jobs | Direct opposite |
| Internal API | None | `/internal/*` on port 3001, Docker-network-bound | Required by split-process model |
| Tool invocation | Orchestrator calls MCP | Agent-runtime calls MCP directly | Direct opposite |

Today's reasoning: the user explicitly chose Approach B over Approach A when shown them side-by-side, with the architectural shape spelled out. The split is intentional, accepting more day-one infrastructure cost to avoid a future refactor when multi-agent and per-agent isolation matter.

Copilot's reasoning (per its consolidation): the user selected in-process. If both selections are accurate, this changed between sessions.

### 2. MCP call path

| Topic | Copilot | Claude (today) |
|---|---|---|
| Who calls MCP | Orchestrator | Agent-runtime, directly |
| MCP auth | Internal Docker network only | Per-agent bearer tokens validated by MCP server |
| Network isolation | Single internal network | Per-capability networks (`net-system`, future `net-files`, etc.) |
| Approval token | Not in v1.0 | HS256 args-bound JWT, scaffolded in v1.0, exercised in v1.1 |

Today's reasoning: when offered three options (orchestrator-only, hybrid, agent-direct), the user pushed for agent-direct, then asked if security could be layered on. The Layered Option C answer (network isolation + tokens + beacons + JWTs) was their chosen design.

### 3. v1.0 scope

| Topic | Copilot | Claude (today) |
|---|---|---|
| `mcp-system` | v1.0 | v1.0 |
| `mcp-files` | v1.0 (read/list/search safe; create/update approval; delete/move disabled) | v1.1 |
| Approval UI in Flutter | v1.0 | v1.1 |
| Active approval flow | v1.0 (files create/update triggers it) | Scaffold only in v1.0 (no v1.0 tool triggers it) |

Today's reasoning: the user explicitly chose phases 0–5 over phases 0–6, with files MCP and semantic memory and the macOS PoC each "deserving its own design pass." This was the cut-line decision.

### 4. Auth model

| Topic | Copilot | Claude (today) |
|---|---|---|
| Client auth | First-run setup, hashed admin password, JWT access + refresh tokens, `users` and `refresh_tokens` tables | Single `TURING_CLIENT_API_KEY` from `.env`; no users, no refresh, no JWT |
| Internal auth | Not needed (no internal API) | `TURING_INTERNAL_TOKEN` for orchestrator ↔ runtime |
| MCP auth | Tool policy in orchestrator | Per-agent MCP tokens |

Today's reasoning: when offered API key vs JWT+users vs hybrid, user explicitly chose API key. Tradeoff was discussed (multi-user is a v2.0 refactor concern). User accepted that tradeoff.

### 5. Approval flow

| Topic | Copilot | Claude (today) |
|---|---|---|
| v1.0 active flow | Yes — files create/update fires it; Flutter cards required | No — no v1.0 tool requires approval; data model + REST + JWT scaffold only |
| Approval JWT | Not used in v1.0 | Scaffolded in v1.0 (HS256, args-bound, single-use), dormant until v1.1 |

Today's reasoning: this is a consequence of the v1.0 cut-line. With files MCP deferred and `mcp-system` tools all classified `safe`, no tool fires approval in v1.0. Building the data model and scaffold validates the architecture for v1.1 without requiring full UI work today.

### 6. Event model

| Topic | Copilot | Claude (today) |
|---|---|---|
| Replay cursor | Monotonic `sequence` per session | `eventId` (ULID) — also lexically time-ordered |
| Event envelope | `eventId, sessionId, runId, traceId, sequence, type, createdAt, payload` | `eventId, sessionId, runId, sequence, event_type, payload_json, created_at` |

This conflict is shallow. ULID `eventId` is itself sortable, so a separate `sequence` is redundant — but cheap. **Adopting both** (Copilot's pattern) is the obvious merge: `eventId` for identity, `sequence` for replay queries. Today's session left this implicit; Copilot makes it explicit and better.

### 7. Concurrency limits

| Topic | Copilot | Claude (today) |
|---|---|---|
| Limits specified | Yes — max active runs (global/per-session), max tool calls per run, model + tool timeouts | Not addressed |

**Copilot wins this one regardless of process model.** Limits are needed in either architecture. This is a clean addition to whatever spec we keep.

### 8. Testing strategy and error handling

| Topic | Copilot | Claude (today) |
|---|---|---|
| Backend testing checklist | Detailed | Not addressed |
| Flutter testing checklist | Detailed | Not addressed |
| Error-handling section | Explicit | Implicit |

**Copilot wins these regardless of process model.** Both are clean additions.

## What today's session would merge from Copilot

Independent of which side wins on the runtime process model, these Copilot ideas are strong and should land in any merged spec:

- **Concurrency limits** (max active runs global/per-session, max tool calls per run, model+tool timeouts).
- **Testing strategy** (backend + Flutter).
- **Explicit error-handling section** (REST validation, run failure events, model failures, tool failures, denial as run state, WS disconnects don't cancel).
- **Monotonic `sequence`** alongside `eventId` for replay.
- **`POST /sessions/:sessionId/messages`** REST mutation endpoint as the canonical send path, with WebSocket as event-only. (Today's session had `send_message` going over WebSocket; Copilot's REST-for-mutations + WS-for-events is cleaner.)
- **`GET /audit`, `GET /tool-calls`** ops endpoints for inspecting persistent state.
- **`agent_run_steps` table** for granular step persistence beyond raw events.
- **Operational endpoints** (`GET /config`, `GET /agents`, `GET /tools`).

## What Copilot's consolidation would discard that today's session considers important

If the user keeps Copilot's spec, these today's-session contributions are at risk of being dropped. They are worth retaining as future-extension seams even if v1.0 is single-process:

- **`AgentExecutor` interface as the future-split point.** Both specs claim this is preserved; the test is whether `AgentExecutor.execute(input): AsyncIterable<Event>` is *the only* coupling between request handling and agent execution. If yes, splitting later is mechanical.
- **`SecretsBackend` interface.** Even with `.env` in v1.0, isolating secret reads behind an interface lets the post-v1.0 swap to Keychain/Vault be a one-file change.
- **`LlmProvider` interface.** Both threads agree on Ollama + OpenAI-compatible; the interface is required either way.
- **Phase plan with demoable goals.** Copilot's consolidation does adopt this. Make sure the merged spec keeps the phase structure.
- **Job reaper concept.** Even in-process, durable jobs that survive process restart need a reaper for orphaned `in_progress` rows.
- **Event-write-before-broadcast.** Copilot's consolidation adopts this as mandatory. Do not lose it.
- **Network-isolation thinking.** Even if v1.0 ships one Docker network, naming the future per-capability split keeps the design honest.

## The two coherent v1.0 paths

### Path A: Today's session (process-isolation-first, smaller scope)

- Two Node processes (orchestrator + agent-runtime).
- Agent-runtime calls MCP directly with per-agent bearer tokens.
- Per-capability Docker networks.
- API key auth, no users table.
- Phases 0–5: scaffold + WS pipe + Ollama streaming + persistence + system MCP + auth/audit/approval scaffold.
- Files MCP and approval UI in v1.1.
- Concurrency limits, testing strategy, error handling adopted from Copilot.

### Path B: Copilot's consolidation (capability-first, larger scope)

- Single Node process with in-process `AgentExecutor`.
- Orchestrator calls MCP.
- Single internal Docker network.
- JWT setup/login with `users` + `refresh_tokens` tables.
- Phases 0–6: scaffold + auth/SQLite + WS replay + model streaming + system MCP + files MCP + approval UI + hardening.
- Files MCP active and approval cards exercised.
- `AgentExecutor` interface preserved for future external runtime extraction.

## Final recommendation

The user should pick one path explicitly. Mid-merging produces a Frankenstein design that neither thread endorses.

If the user picks **Path A**, today's session's `2026-05-09-project-turing-v1-design-claude.md` is the basis; merge in Copilot's concurrency limits, testing, error handling, REST send-message, ops endpoints, and `agent_run_steps`. Tag final canonical spec.

If the user picks **Path B**, Copilot's `2026-05-09-project-turing-v1-design-copilot.md` is the basis; merge in today's session's interface seams (`AgentExecutor`, `SecretsBackend`, `LlmProvider`), event-write-before-broadcast, job reaper, and phase plan with demoable goals. Tag final canonical spec.

If the user picks **a hybrid that's not one of these two paths**, walk through the seven conflict areas explicitly to lock each. This will be slower but produces a single coherent design instead of two glued-together half-designs.

The author of this report (Claude session) does not have a stake in which path wins. The brainstorming today produced Path A because the user selected each underlying decision; if those selections were misread or have since changed, Path B is equally valid as a v1.0.

## Decisions still needed from the user

1. **Process model** — in-process executor (B) or split orchestrator + agent-runtime (A)?
2. **MCP call path** — orchestrator-mediated (B) or agent-direct with layered security (A)?
3. **v1.0 scope** — phases 0–5 (A) or phases 0–6 with files MCP and approval UI (B)?
4. **Auth model** — API key (A) or JWT setup/login with users (B)?
5. **Approval flow exercise** — scaffold-only in v1.0 (A) or active flow with Flutter cards (B)?

These are the only places where the merged spec cannot be written without an explicit pick.

— end of report —
