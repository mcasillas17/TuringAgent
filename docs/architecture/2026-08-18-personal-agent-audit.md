# TuringAgent Personal-Agent Audit — Memory, Trust, and Operability

**Audit date:** 2026-08-18  
**Repository snapshot:** `be2c8c9`  
**Last verified against the code:** 2026-08-18, at `be2c8c9`  
**Scope:** Backend architecture, local personal-agent behavior, durable memory, privacy, reliability, integrations, and the path to multiple specialized agents.

## Executive conclusion

TuringAgent already has the difficult foundation of a trustworthy local agent: durable SQLite state, replayable events, leased and fenced jobs, bounded model and tool execution, sandboxed file access, and argument-bound single-use approvals. Those parts should be preserved.

Turing is not yet a personal agent that learns about its user. Its current memory is attributed keyword recall over raw messages. It has no curated profile or fact store, no memory lifecycle, no contradiction handling, no user correction workflow, no memory-specific evaluation, and no complete erasure contract for future derived data.

The recommended path is:

1. Fix existing correctness, consent, privacy, and operability gaps.
2. Make recall measurable and cheaper before adding embeddings.
3. Define erasure and provenance invariants before creating derived memory.
4. Add user-controlled, evidence-backed semantic and procedural memory.
5. Add automatic learning only as a reviewable candidate workflow.
6. Add local embeddings only if the evaluation suite proves lexical retrieval is insufficient.
7. Conform to MCP and add connectors only after the memory and trust boundaries are stable.
8. Add a second specialized agent only after capability discovery, routing, and concurrent-worker behavior are explicit.

The target is not a graph-heavy autonomous system. It is a local, inspectable assistant whose beliefs can be traced, corrected, exported, and withdrawn.

## Audit method

Three independent model streams were used:

| Model | Primary responsibility |
|---|---|
| GPT-5.6 Sol | Static backend audit: correctness, reliability, security, persistence, tools, approvals, and operational gaps |
| GPT-5.6 Terra | Primary-source research on personal-agent memory systems and comparison with Turing |
| Claude Opus 5 | End-state architecture, sequencing, over-engineering checks, and an independent gap analysis |

The synthesis was checked against the repository's Go, protobuf, SQL, Compose, Flutter, and architecture files. External claims below are limited to primary documentation or papers accessed on 2026-08-18.

## Relationship to `docs/VISION.md`

`docs/VISION.md` remains the north star. This audit is subordinate to it and does not change its owner-decided invariants or deferrals:

1. **Message-level deletion:** MEM-011.
2. **Destructive file operations:** Still deferred with the existing soft-delete/move, approval, visibility, collision, and retention gates unchanged. This audit does not schedule them. They remain disabled by `permanentlyDisabled` in `turing-backend/orchestrator-go/internal/service/tools/defaults.go` and the MCP-files dispatch guard in `turing-backend/mcp-files/internal/tools/files.go`.
3. **Shell and native automation:** Still deferred with the sandbox and per-invocation approval gates unchanged. This audit does not schedule it.
4. **External brokers:** Not scheduled. AGT-001 is expected to generate concurrency evidence; a broker remains gated on measured SQLite queue failure.
5. **Vector/semantic memory:** MEM-013 uses VISION's existing evidence gate. MEM-003 makes that gate measurable.

Multi-agent remains a decided goal. Single-user scope and protobuf compatibility remain explicit project decisions/open boundaries rather than assumptions introduced by this audit.

## What Turing already does well

| Capability | Current state | Evidence |
|---|---|---|
| Local-first deployment | SQLite and Ollama are local defaults; one loopback-bound public gRPC port; MCP services remain internal | `docs/VISION.md:11-31`; `turing-backend/infra/docker-compose.yml:1-44,88-149` |
| Durable execution | Jobs, runs, events, leases, recovery, and per-session sequencing are persisted | `turing-backend/orchestrator-go/internal/db/schema/0001_initial.sql:34-94`; `0002_go_runtime.sql`; `0004_execution_exit_gate.sql`; runtime repository and service tests |
| Tool safety | Unknown tools fail closed, mutating file operations require approval, and filesystem access is sandboxed | `turing-backend/orchestrator-go/internal/service/tools/defaults.go`; `turing-backend/mcp-files/internal/tools/safe_fs.go` |
| Approval integrity | Approval tokens are short-lived, argument-bound, and single-use | `turing-backend/orchestrator-go/internal/service/approvals/service.go`; `turing-backend/mcp-files/internal/approval/jwt.go` |
| Session evidence | Messages, runs, events, tools, approvals, and audit entries retain a durable execution history | `turing-backend/orchestrator-go/internal/db/schema/0001_initial.sql:12-157` |
| Cross-session recall | FTS5 retrieves previous messages and injects bounded, dated, role-labelled excerpts | `docs/architecture/session-recall.md`; `turing-backend/agent-runtime-go/internal/memory/recall.go` |
| Recall hardening | Recalled text is flattened, attributed, bounded, and framed as untrusted historical material | `turing-backend/agent-runtime-go/internal/memory/recall.go:64-67,412-469` and its rendering tests |
| Session deletion | Session-owned rows cascade and FTS triggers remove deleted message content; audit payloads are scrubbed | `turing-backend/orchestrator-go/internal/repository/session_delete.go`; `turing-backend/orchestrator-go/internal/repository/session_delete_test.go` |

These are architectural assets, not temporary implementation details. Future memory, connectors, and agents should reuse the existing orchestrator-owned state, event, policy, and approval boundaries.

## What "memory" means for Turing

External systems use the word *memory* for several different capabilities. Turing should model them separately:

| Layer | Purpose | Turing today | Target |
|---|---|---|---|
| Working context | Current model request and active tool loop | Implemented | Token-aware assembly with explicit budgets |
| Session memory | Conversation history within one thread | Implemented, capped at 50 fetched messages | Preserve; add summaries only when measured |
| Episodic memory | What happened across conversations and tool use | Partial: raw messages are searchable; tool-result knowledge is not | Searchable, attributed evidence with retention controls |
| Semantic memory | Facts, preferences, goals, and project context believed to be current | Missing | Versioned, evidence-backed, scoped, editable memories |
| Procedural memory | Standing instructions and learned workflows | Partial: tool policy exists; user procedures do not | User-approved instructions separate from factual beliefs |
| Prospective memory | Reminders and intended future actions | Missing | A future scheduler/task domain, not prose stuffed into memory |

LangGraph's official memory guide distinguishes short-term thread state from cross-thread long-term memory and separates semantic, episodic, and procedural memory. Letta's official documentation distinguishes always-visible memory blocks from on-demand archival search. OpenAI Agents SDK sessions persist one session's history but are not, by themselves, a cross-session personal belief model. These distinctions match Turing's needs.

Sources:

- [LangGraph memory concepts](https://docs.langchain.com/oss/python/concepts/memory) (accessed 2026-08-18)
- [Letta memory blocks](https://docs.letta.com/v1-sdk/memory/memory-blocks) (versioned V1 documentation; accessed 2026-08-18)
- [Letta archival memory](https://docs.letta.com/v1-sdk/memory/archival-memory) (versioned V1 documentation; accessed 2026-08-18)
- [OpenAI Agents SDK sessions](https://openai.github.io/openai-agents-python/sessions/) (accessed 2026-08-18)

## Current recall: useful, safe, and architecturally limited

Turing's current recall package explicitly stores nothing and derives no facts (`turing-backend/agent-runtime-go/internal/memory/recall.go:1-7`). It:

- extracts at most six terms from the current user turn;
- performs one exact-phrase FTS query per term;
- requests up to 40 hits per term;
- removes visible/current-session duplicates;
- ranks by distinct matched terms and recency;
- injects at most five excerpts and 2,000 characters.

The repository search orders rows by SQLite `bm25(messages_fts)`, but `SearchMessagesResponse` returns only `Message`, so the score is discarded (`turing-backend/orchestrator-go/internal/repository/sessions.go:153-193`; `proto/turing/v1/sessions.proto:58-66`). The runtime then re-ranks with a coarser signal. This creates up to six local RPCs and six FTS queries per turn while making the system unable to measure the quality of the server's ranking.

This is good historical recall, not user learning. Recalled assistant messages may also contain model-generated or tool-derived text. They are appropriately framed as untrusted context, but they must never be promoted automatically into durable beliefs.

## Research synthesis

### Patterns worth adopting

1. **Separate a compact profile from retrieved evidence.** Always-visible, bounded identity/preferences should not be the same object as an unbounded archive.
2. **Store evidence and lifecycle state with every belief.** A fact needs source messages, observed and validity times, status, confidence, sensitivity, and revision history.
3. **Treat memory writing as a policy decision.** Storing every turn creates noise and poisoning risk. Explicit "remember this" should be the first write path; automatic extraction should initially create reviewable candidates.
4. **Support correction, supersession, retraction, and deletion.** A current belief can replace an older belief without erasing the historical evidence, while source deletion must remove every derived representation.
5. **Evaluate retrieval and memory use separately.** LongMemEval tests extraction, multi-session reasoning, temporal reasoning, updates, and abstention. Turing needs a smaller local fixture suite covering the same failure classes before selecting retrieval technology.
6. **Keep lexical retrieval.** Exact terms, identifiers, filenames, and code symbols are a strength of FTS/BM25. Semantic retrieval should complement it, not replace it.
7. **Make memory use explainable.** Every injected memory should be attributable and visible through "why was this remembered/used?" APIs and UI.
8. **Keep local behavior local.** Extraction and embeddings must not create implicit remote egress. Remote use requires explicit policy and visible data-scope disclosure.
9. **Defer graph infrastructure.** Temporal facts and supersession provide most near-term value without adding a graph database and multi-call extraction pipeline.
10. **Use standard MCP for connectivity, but keep policy local.** The MCP specification requires treating tool annotations as untrusted and emphasizes explicit consent. Turing's orchestrator-owned, fail-closed policy should treat all server-provided text, including descriptions, as untrusted.

Additional primary sources:

- [LongMemEval](https://arxiv.org/abs/2410.10813) (accessed 2026-08-18)
- [Mem0 architecture](https://github.com/mem0ai/mem0/blob/main/skills/mem0/references/architecture.md) (current V3 architecture; accessed 2026-08-18)
- [Graphiti overview](https://help.getzep.com/graphiti/getting-started/overview) (vendor-authored architecture documentation; accessed 2026-08-18)
- [MCP specification, 2026-07-28](https://modelcontextprotocol.io/specification/2026-07-28) (accessed 2026-08-18)
- [OWASP LLM01: Prompt Injection](https://genai.owasp.org/llmrisk/llm01-prompt-injection/) (accessed 2026-08-18)

Mem0's current V3 documentation demonstrates scoped, ADD-only fact extraction, explicit update/delete APIs, and hybrid retrieval. Graphiti's vendor documentation demonstrates temporal validity and provenance over changing facts. Those are useful design patterns, but their managed/vector/graph implementations should not be copied wholesale into Turing.

## Comparison with the desired personal-agent state

| Concern | Turing now | Desired state | Gap |
|---|---|---|---|
| User model | None | Compact, editable profile and standing instructions | Critical |
| Durable facts | None | Scoped facts/preferences/goals with evidence and revisions | Critical |
| Automatic learning | None | Candidate extraction with review and safe promotion | Critical, but should follow manual memory |
| Contradictions | Raw messages coexist | Supersession and temporal validity | Critical |
| Forgetting | Whole-session delete | Memory-, message-, source-, and session-level withdrawal | Critical |
| Recall quality | FTS phrases; score discarded | Scored, evaluated, explainable lexical baseline | High |
| Semantic retrieval | None | Optional local embeddings behind an evaluation gate | Later |
| Tool-result memory | Tool summaries/events only | Selected tool evidence can be imported with provenance and trust class | High |
| Memory UI | Recall notice only | List, inspect, edit, approve, reject, retract, export, and explain | Critical |
| Remote privacy | Provider selected per request | Explicit per-session/run egress policy and visible data scope | Critical |
| Audit | Write-only | Redacted, filterable read path | High |
| Export/backup | Missing | Open-format export, consistent backup, tested restore | High |
| Connectors | Two hard-coded in-repo JSON-RPC tool servers | Standards-conformant MCP plus connector provenance and consent | High |
| Multi-agent | One general assistant | Capability-aware routing and explicit handoff | Later |
| Evaluation | No memory/retrieval harness | Checked-in quality, safety, latency, and deletion suites | Critical |
| Retention | Unbounded events/audit/run detail | Explicit, default-safe retention and pruning | High |

## Target architecture

### Durable ownership

The orchestrator remains the only owner of durable state and policy. Memory is a projection over source evidence, not an independent store whose lifecycle can drift from messages, events, or imported artifacts.

The orchestrator currently pins SQLite to one connection (`turing-backend/orchestrator-go/internal/db/connection.go:30`). Streamed deltas and every proposed background writer contend on that connection. Batching, retention sweeps, extraction, consolidation, and concurrent agents must each state and test a transaction budget rather than assuming background work is free.

### Memory records

Every memory item should carry:

- stable ID;
- scope: initially `user`, `project`, or `session` without introducing speculative multi-user identity;
- kind: `fact`, `preference`, `goal`, or `instruction`;
- normalized content;
- lifecycle: `candidate`, `active`, `superseded`, `retracted`, or `expired`;
- source type and source IDs;
- extractor/model/version when model-generated;
- observed time and `valid_from`/`valid_to`;
- confidence and sensitivity class;
- creator: explicit user action, importer, or extractor;
- revision and supersession links.

Source evidence must be immutable and attributable. A memory may be revised, but provenance must not be caller-forgeable.

### Write paths

1. **Explicit path:** "Remember this" creates an active memory from the user's own content.
2. **Manual path:** The user creates or edits profile and instruction entries.
3. **Candidate path:** A local extractor creates a candidate from user-authored content after a run finishes.
4. **Import path:** A previewed, attributed, reversible import creates isolated candidates or evidence.
5. **Consolidation path:** A bounded local job proposes deduplication or supersession and keeps a preimage for review/revert.

Assistant text, recalled text, files, web pages, and tool results are untrusted evidence. They must not directly create active user beliefs or procedural instructions.

### Read paths

Model context should use independent budgets for:

1. active compact profile/instructions;
2. active relevant memory items;
3. raw transcript evidence;
4. current session history and tool protocol.

Every injected item should include its date and provenance. The UI should expose the exact items used for a run.

### Erasure invariant

Any derived row must either:

- have cascading foreign keys to its source evidence; or
- be completely rebuildable from surviving sources and removed transactionally when a source is withdrawn.

Deleting a source must remove its FTS/vector rows, caches, candidate memories, active memories, summaries, and connector projections. Audit may retain only a scrubbed tombstone proving deletion occurred.

### Retrieval progression

1. Expose BM25 scores/snippets.
2. Build the evaluation corpus and baseline.
3. Collapse term fan-out into one structured query.
4. Add temporal, scope, status, and source filters.
5. Add local embeddings only when a labelled failure class improves without regressing deletion, provenance, or latency.
6. Add graph traversal only if measured multi-hop/temporal failures remain after versioned facts and hybrid retrieval.

## Architectural decisions and non-goals

- Stay single-user for now; model memory scope, not speculative account identity.
- Keep SQLite as the durable store and transaction boundary.
- Do not adopt a Python/Node agent framework that owns state, retries, or dispatch.
- Do not add a graph database in the first memory implementation.
- Do not add an external vector service.
- Do not permit background network egress.
- Do not allow extracted content to change tool policy or system instructions.
- Do not enable arbitrary third-party MCP servers in the same change that adds protocol conformance.
- Do not add a second agent until worker capabilities, routing, tool policy, and concurrency behavior are explicit.
- Treat MCP tool annotations as untrusted, as required by the MCP specification. Treat descriptions and all server-provided text as untrusted content under Turing's existing policy boundary.

## Prioritized implementation tasks

Each item below is intended to become one implementation plan and one independently reviewable PR. Every changed behavior requires a test that fails without the change. Generated protobuf code must be regenerated and checked whenever a `.proto` changes.

Task IDs are stable references, not priority numbers. Delivery order follows `docs/VISION.md`'s decision ladder and the dependency table below.

### Phase 0: User-visible truth, consent, and privacy

#### TUR-002 — Persist approval decision rationale

**Outcome:** A recorded approval or denial keeps the reason the user supplied instead of silently discarding an advertised API field.  
**Scope:** Add decision-comment and denial-reason storage; consume `ApproveApprovalRequest.comment` and `DenyApprovalRequest.reason`; expose both through audit with empty and absent values distinguished.  
**Likely files:** approvals schema/proto/service/repository and audit mapping.  
**Acceptance:** A comment or reason survives restart; repository/service tests prove both request fields are consumed and included in the corresponding audit record. TUR-013 later makes that record user-readable.  
**Dependencies:** None.
**Status:** Implemented. Human decisions persist the matching field atomically;
because the existing proto3 scalars have no presence, omitted and explicit empty
input share the documented empty-string representation, while non-human paths
remain `NULL`. Audit receives only a bounded rationale copy and `toolName`, and
session deletion still scrubs that payload. TUR-013 remains the public audit
read path and TUR-021 remains the approval preview/diff UX.

#### TUR-007 — Derive stable session titles from the first user turn — Implemented

**Outcome:** Conversations and search groups are distinguishable instead of remaining "New chat."  
**Delivered:** The enqueue transaction derives a deterministic, single-line, rune-safe title from the first usable user turn, preserves it on later turns, and persists `session.updated` with the authoritative title and timestamp. Flutter creates untitled sessions and applies the durable event without polling; startup backfill repairs legacy `New chat` rows.

**Verification:** Repository tests cover whitespace-only, multiline, long, non-ASCII, explicit-title, later-message, backfill, replay, and deletion behavior. Event service and Flutter widget tests cover protocol mapping and live session/search rendering. See `docs/architecture/session-titles.md`.

**Dependencies:** None.

#### TUR-020 — Pin and enforce the model context budget

**Outcome:** The assembled prompt cannot silently exceed the model's context window, and recall cannot be dropped without notice.  
**Scope:** Set Ollama `num_ctx` explicitly from configuration; measure history, recall, tool schemas, and tool results before dispatch; apply an explicit priority/truncation policy; emit a durable notice when content is omitted.  
**Likely files:** Ollama provider/config, general-assistant context assembly, events, long-context tests.  
**Acceptance:** The runtime never relies on a host default; an overflowing fixture produces a notice; a representative long session proves the recall block reaches the provider.  
**Dependencies:** None. No `num_ctx` or equivalent is configured today; the runtime combines a 50-message history, bounded recall, tool schemas, and potentially large tool results without measuring the final prompt.

#### TUR-001 — Make `SendMessage` idempotent

**Outcome:** Retrying the same client operation cannot create duplicate messages, runs, or side effects.  
**Scope:** Persist and consume `SendMessageRequest.idempotency_key`; bind it to session and request identity; return the original IDs for an identical replay; reject a conflicting payload.  
**Likely files:** `proto/turing/v1/chat.proto`, chat service, jobs repository, schema migration, integration tests.  
**Acceptance:** Concurrent duplicate requests produce one user message and one run; replay resumes the same event stream; conflicting reuse returns a typed error.  
**Dependencies:** None.

#### TUR-021 — Add inspectable approval previews

**Outcome:** The user can inspect exactly what a proposed mutation will change before deciding.  
**Scope:** Add approval detail APIs with structured arguments, bounded previews/diffs and hashes; link approval events to details.  
**Likely files:** approvals proto/service/repository, file-tool precondition data, Flutter approval UI.  
**Acceptance:** File writes show a bounded before/after preview; secrets and oversized content are redacted; the preview is bound to the same argument hash that approval authorizes.  
**Dependencies:** TUR-013.

#### TUR-003 — Enforce explicit remote-provider egress policy

**Outcome:** Users know and control when conversation, recall, memory, and tool data leave the machine.  
**Scope:** Record remote-provider consent per run, disclose the data categories included, enforce HTTPS for keyed non-loopback endpoints, and prohibit implicit fallback.  
**Likely files:** session/provider schema and proto, chat validation, runtime provider configuration, Flutter provider selection.  
**Acceptance:** Each remote run records consent and disclosed data categories; local failure never silently falls back remotely; no background feature inherits consent from an interactive request.  
**Dependencies:** None.

#### TUR-004 — Close session-deletion withdrawal gaps

**Outcome:** Deleting a session deterministically withdraws session-owned database state, derived state, and tool artifacts.  
**Scope:** Publish a terminal deletion event, close/reconcile subscribers, attach session/run provenance to sandbox artifacts, define keep/delete policy, and document SQLite/WAL physical-erasure limits. Evaluate whole-database encryption plus key destruction as the credible byte-withdrawal strategy instead of promising reliable file overwrite on SSDs.  
**Likely files:** session deletion repository/service, events service, MCP file metadata, client session state, architecture docs.  
**Acceptance:** Subscribers are notified; session-owned artifacts are listed and deleted or explicitly retained by policy; no search or memory path returns deleted content.  
**Dependencies:** MEM-001.

#### TUR-005 — Harden runtime and orchestrator containers

**Outcome:** Every backend service follows the same least-privilege container posture already used by MCP services.  
**Scope:** Non-root users, read-only roots, dropped capabilities, `no-new-privileges`, and minimal writable mounts.  
**Likely files:** orchestrator/runtime Dockerfiles, Compose, Docker security tests.  
**Acceptance:** Security tests cover all services; normal startup, model calls, and persistence still work.  
**Dependencies:** None.
**Status:** Pending merge. Branch `mcasillas17-tur-005-container-hardening`
adds the runtime image identity, applies one fail-closed Compose posture to all
four backend services, and allowlists only `/app/data`, `/skills`, and `/sandbox`
as writable storage. The pull request must record fresh Docker-backed startup,
model-loop, and persistence evidence before merge.

#### TUR-006 — Introduce service-scoped internal identities

**Outcome:** Compromise of one internal service does not grant access to every internal RPC.  
**Scope:** Separate runtime, approval-consumer, and connector credentials; authorize per method; remove provider and MCP secrets from processes that do not need them.  
**Likely files:** auth interceptors, app registration, internal clients, Compose/init scripts.  
**Acceptance:** Each service can call only required methods; existing approval consumption remains atomic; no new network listener is introduced.  
**Dependencies:** None.

### Phase 1: Make the existing product honest and operable

#### TUR-008 — Complete session lifecycle and pagination

**Outcome:** Session ordering, pagination, archive/rename behavior, and limits match the public contract.  
**Scope:** Update `sessions.updated_at` on activity; implement bounded stable cursors; consume `PageRequest.cursor`; populate `PageResponse`; add explicit rename/archive operations and input limits. Today `updated_at` is only set at creation, so ordering is creation order, and the service advertises but ignores cursor pagination.  
**Likely files:** sessions proto/service/repository, jobs transaction, Flutter session actions.  
**Acceptance:** Pagination is stable under inserts; active conversations reorder correctly; invalid cursors and excessive limits fail predictably.  
**Dependencies:** None.

#### TUR-009 — Persist reopenable run outcomes

**Outcome:** Reopening a conversation never shows an unexplained empty assistant turn.  
**Scope:** Expose run status and failure/cancellation reason with message history or a run-history API; render terminal cards after reopen.  
**Likely files:** message/run proto, sessions repository/service, Flutter conversation timeline.  
**Acceptance:** Completed, failed, and cancelled runs round-trip after restart; no empty placeholder is ambiguous.  
**Dependencies:** None.

#### TUR-010 — Surface no-worker and queue-timeout state

**Outcome:** A queued run cannot wait indefinitely without explanation.  
**Scope:** Persist queue age and worker availability notices; add configurable terminalization/pause policy for unavailable capabilities.  
**Likely files:** dispatcher/reaper, jobs repository, events, Flutter status cards.  
**Acceptance:** A run with no eligible worker shows an immediate durable notice and reaches the configured terminal/pause state.  
**Dependencies:** TUR-018.

#### TUR-011 — Batch model deltas

**Outcome:** Streaming remains responsive without one SQLite transaction and replay cycle per provider chunk.  
**Scope:** Implement time/byte coalescing using a real, documented flush setting while preserving sequence and terminal durability.  
**Likely files:** runtime output pipeline or orchestrator event ingestion, configuration, streaming tests.  
**Acceptance:** A 1,000-chunk fixture produces a bounded number of writes; resume reconstructs byte-identical output; terminal events flush pending text; the test records the transaction budget on the single SQLite connection.  
**Dependencies:** None.

#### TUR-012 — Extend approval push to approved decisions

**Outcome:** Approval wakes the exact waiting tool call immediately under normal operation.  
**Scope:** Extend the existing denied/expired push handling to approved decisions; retain polling only after stream loss; handle duplicate and late updates.  
**Likely files:** runtime worker, tool runner, orchestrator client, runtime command tests.  
**Acceptance:** Approval latency is not tied to the one-second poll interval; reconnect races preserve single consumption.  
**Dependencies:** None.

#### TUR-013 — Add a redacted audit read API

**Outcome:** Users can inspect approvals, mutations, retries, and memory decisions.  
**Scope:** Add paginated/filterable audit RPCs by correlation, action, and time; distinguish scrubbed payloads; default to redaction.  
**Likely files:** new audit proto/service, audit repository, app registration, Flutter inspection UI.  
**Acceptance:** Written rows are retrievable; deleted-session rows remain visible only as scrubbed evidence; credentials and raw sensitive payloads never appear.  
**Dependencies:** None.

#### TUR-014 — Capture provider usage and actionable health

**Outcome:** Local operators can explain latency, model usage, queue pressure, and dependency readiness.  
**Scope:** Persist provider tokens/timing where available; honor `LOG_LEVEL`; expose structured readiness for DB, workers, MCP, and configured providers; add local metrics without sensitive content.  
**Likely files:** provider parsers, run completion/schema/proto, health service, process logging.  
**Acceptance:** Usage and latency appear per run; health is degraded when a required dependency is unavailable; local models never invent monetary cost.  
**Dependencies:** TUR-018.

#### TUR-019 — Enforce protobuf compatibility

**Outcome:** Additive evolution remains safe for Flutter and future clients before the memory APIs expand the contract.  
**Scope:** Reserve removed fields/numbers and add CI breaking-change checks against `main` while retaining deterministic generation checks.  
**Likely files:** proto files, `tools/proto`, CI workflow and self-guard tests.  
**Acceptance:** Additive changes pass; renumbering/removing a live field fails CI.  
**Dependencies:** None.
**Implementation pending merge:** `buf.yaml` selects Buf 1.72.0 `FILE`
compatibility for the `proto` module; `tools/proto/breaking.sh` refreshes and
resolves the requested remote-tracking base before comparison; real fixtures in
`tools/proto/breaking_test.go` prove additive changes pass and live-field
removal or renumbering fails, including removal that reserves the old name and
number; `.github/workflows/ci.yml` and its self-guard run the pinned check
without replacing deterministic Go/Dart generation. Mainline history contains
no removed protobuf fields, enum values, or files, so this pending
implementation adds no speculative reservations.

### Phase 2: Make recall measurable before changing technology

#### MEM-001 — Define the memory threat model and derived-state contract

**Outcome:** Memory cannot weaken local-first privacy, approval, or deletion guarantees.  
**Scope:** Document trust classes, writers, scopes, egress, retention, backup/export, correction, and deletion rules; add a schema guard that rejects user-derived tables without cascading provenance or an explicit scrubbed exception.  
**Likely files:** `docs/VISION.md`, new memory architecture doc, DB schema-invariant tests.  
**Acceptance:** The guard passes today and fails for a synthetic derived-text table with no source cascade; exceptions are explicit and justified.  
**Dependencies:** None. **Blocks every derived memory task.**

#### MEM-002 — Return scored, explainable search hits

**Outcome:** Search consumers receive the ranking signal SQLite already computes.  
**Scope:** Add an additive `SearchHit` with message, normalized score semantics, and snippet while temporarily preserving the legacy messages field.  
**Likely files:** `proto/turing/v1/sessions.proto`, sessions repository/service, generated Go/Dart, client mappers.  
**Acceptance:** Results expose documented score ordering and safe snippets; legacy callers receive identical messages; proto checks pass.  
**Dependencies:** None.

#### MEM-003 — Build a deterministic recall evaluation harness

**Outcome:** Retrieval changes are decided by evidence instead of intuition.  
**Scope:** Add synthetic, checked-in sessions and labelled queries covering exact IDs, paraphrases, updates, temporal questions, multi-session synthesis, CJK, injection, deletion, and abstention; report Recall@k, MRR, nDCG, stale-use rate, latency, assembled-prompt size, and whether the selected recall block survived context assembly.  
**Likely files:** backend test fixtures and a dedicated recall-evaluation test package.  
**Acceptance:** The suite records a baseline and fails on regression; each required failure class has at least one fixture; expected lexical failures remain visible.  
**Dependencies:** MEM-002.

#### MEM-004 — Collapse recall into one structured search request

**Outcome:** Recall makes one RPC and preserves server-side relevance ranking.  
**Scope:** Add validated repeated terms and explicit match mode; build safe FTS queries server-side; retain deduplication and byte budgets in the runtime.  
**Likely files:** sessions proto/repository/service, runtime recall/search client, fuzz and parity tests.  
**Acceptance:** One RPC per turn; FTS operators cannot be injected; evaluation metrics are equal or better than baseline.  
**Dependencies:** MEM-002, MEM-003.

#### MEM-016 — Make CJK recall behavior explicit and useful

**Outcome:** CJK users are not left with a silently weaker memory path.  
**Scope:** Use MEM-003 to choose and document a lexical strategy for scripts without whitespace token boundaries, such as validated n-grams or an alternate FTS tokenizer; retain exact-ID behavior and injection resistance.  
**Likely files:** recall term extraction, search repository, tokenizer configuration/migration if justified, evaluation fixtures.  
**Acceptance:** Labelled CJK Recall@5 improves over the recorded baseline without regressing exact identifiers or FTS query safety.  
**Dependencies:** MEM-003, MEM-004.

### Phase 3: Add user-controlled long-term memory

#### MEM-005 — Add the versioned memory schema

**Outcome:** Turing has a first-class semantic/procedural memory domain with provenance and temporal revision.  
**Scope:** Add the minimum manual-memory substrate: items, evidence, revisions, scope, fact/preference/instruction kinds, active/superseded/retracted state, observed/valid times, supersession links, and FTS projection. Candidate/extractor/confidence fields land with MEM-009 when they have real behavior.  
**Likely files:** DB migrations, migration tests, repository models.  
**Acceptance:** Source/session deletion cascades through memory and FTS; supersession preserves history but removes the old item from current beliefs; MEM-001's guard passes.  
**Dependencies:** MEM-001.

#### MEM-006 — Add authenticated memory APIs and repository operations

**Outcome:** Clients can create, inspect, edit, supersede, retract, delete, and export manual memory.  
**Scope:** Public memory RPCs and transactional repository operations; server-owned provenance; mutation audit events.  
**Likely files:** new memory proto/service/repository, app registration, generated clients.  
**Acceptance:** Invalid lifecycle transitions fail; concurrent revisions are conflict-safe; callers cannot forge source or provenance metadata.  
**Dependencies:** MEM-005, TUR-013.

#### MEM-007 — Ship manual profile and "remember this" UX

**Outcome:** Useful personal memory exists before automatic extraction.  
**Scope:** Editable profile/instruction views, explicit remember action from a user message, source/date/status display, correction, and forget controls.  
**Likely files:** Flutter memory feature, gRPC client/mappers, conversation actions.  
**Acceptance:** Explicit remember creates an active memory; normal conversation creates no active memory; every item has visible provenance and correction/delete actions.  
**Dependencies:** MEM-006.

#### MEM-008 — Compose safe, transparent memory recall

**Outcome:** Active profile, curated memories, and raw transcript recall influence answers under separate budgets.  
**Scope:** Filter by status, scope, and temporal validity; frame all retrieved content as attributed evidence; emit structured memory-use events and per-turn controls.  
**Likely files:** runtime memory package, general assistant, events proto, Flutter run notices/details.  
**Acceptance:** Superseded or deleted items never appear; each injected item is inspectable; users can disable memory globally, per session, or per turn.  
**Dependencies:** MEM-006, MEM-007, MEM-003.

#### MEM-009 — Add local candidate extraction and review

**Outcome:** Turing can learn without silently rewriting its beliefs.  
**Scope:** Run bounded extraction after answer completion; use user-authored text only in v1; create candidates, never active memories; add approve/edit/reject review queue.  
**Likely files:** runtime extraction pipeline, internal memory service, candidate events, Flutter review queue.  
**Acceptance:** Extraction never delays or fails a run; malformed/timeout results create nothing; assistant/tool/recalled content cannot become a candidate source; no remote egress occurs by default; the implementation states and tests its transaction budget on the single SQLite connection.  
**Dependencies:** MEM-006, MEM-007, MEM-003.

#### MEM-010 — Add reversible consolidation and supersession

**Outcome:** Duplicate or conflicting candidates are resolved without destructive, opaque rewriting.  
**Scope:** Bounded local background job, deterministic eligibility, duplicate detection, proposed supersession, preimage, review log, and revert.  
**Likely files:** orchestrator job domain, memory repository/service, review UI.  
**Acceptance:** Failure is a safe no-op; active beliefs change only through an audited transition; every consolidation can be inspected and reverted; work is batched so the single SQLite connection cannot be monopolized.  
**Dependencies:** MEM-009.

#### MEM-011 — Add fact- and message-level forgetting

**Outcome:** Users can withdraw one fact or source without deleting the surrounding conversation.  
**Scope:** Delete/retract memory APIs; message deletion rules; cascade through FTS, future vectors, candidates, summaries, caches, and prompts; handle run anchor foreign keys explicitly.  
**Likely files:** sessions and memory proto/services/repositories, deletion events, Flutter actions.  
**Acceptance:** Deleted content cannot be recalled after restart; in-flight and run-anchor cases have explicit tested behavior; audit retains only scrubbed tombstones.  
**Dependencies:** MEM-005, MEM-006, MEM-001.

#### MEM-012 — Add memory-specific observability

**Outcome:** A user can answer "why did Turing remember or use this?"  
**Scope:** Trace retrieval, injection, extraction, candidate creation, promotion, supersession, retraction, and index deletion without recording sensitive content by default.  
**Likely files:** audit/event schemas and services, memory pipeline, Flutter run/memory details.  
**Acceptance:** Each memory used in a run links to its source and decision history; retention/redaction are configurable and tested.  
**Dependencies:** TUR-013, MEM-008, MEM-009.

#### MEM-015 — Capture selected tool results as attributed evidence

**Outcome:** Useful facts read from files or system tools can be found later without treating tool output as trusted user belief.  
**Scope:** Persist an allowlisted, bounded evidence representation for selected tool results with tool call, run, session, trust class, and source-artifact provenance; keep it outside active profile/instructions until explicit user promotion.  
**Likely files:** evidence schema/repository, tool completion pipeline, search/memory APIs, deletion propagation tests.  
**Acceptance:** "What was in the file we inspected?" can retrieve attributed evidence; prompt-like tool content cannot create active memory; deleting the source session/artifact removes every projection.  
**Dependencies:** MEM-001, MEM-005, MEM-011.

### Phase 4: Retrieval upgrades, gated by evidence

#### MEM-013 — Add optional local semantic retrieval

**Outcome:** Paraphrase retrieval improves only where the evaluation corpus proves FTS is insufficient.  
**Entry criterion:** MEM-003 identifies a labelled query class that MEM-004 does not solve and local embeddings improve without unacceptable latency or stale-use regressions.  
**Scope:** Explicit local embedding provider, same-store vector projection if feasible, lexical fallback, scope/status/time filters, fusion, rebuild, and deletion propagation.  
**Acceptance:** Enabled/disabled metrics are checked in; embedder failure degrades to lexical results; deletion removes vector rows transactionally; remote embeddings are opt-in and disclosed.  
**Dependencies:** MEM-003, MEM-004, MEM-005, MEM-011.

#### MEM-014 — Add token-aware context assembly and summaries

**Outcome:** Long sessions fit model limits without silently losing live tool protocol or important evidence.  
**Scope:** Provider/model capability metadata, explicit token budgets, provenance-preserving summaries, and priority order across profile, memory, recall, history, and tools.  
**Likely files:** provider interface, general assistant context builder, summary persistence, evaluation fixtures.  
**Acceptance:** Long-session and tool-chain tests stay within model limits; summary failure falls back safely; deletion removes source-derived summaries.  
**Dependencies:** TUR-018, MEM-001, MEM-003.

### Phase 5: Ownership, retention, and portability

#### TUR-015 — Add session and memory export

**Outcome:** "Your data" includes possession, not only deletion.  
**Scope:** Stream allowlisted JSON Lines for sessions, messages, runs, tool calls, approvals, events, and memory; omit tokens/JTIs/secrets.  
**Likely files:** session/memory proto and services, export repository queries, Flutter save flow.  
**Acceptance:** Export round-trips content byte-for-byte and streams large sessions; secret-denylist tests pass.  
**Dependencies:** MEM-006.

#### TUR-016 — Add consistent backup, restore, and migration integrity

**Outcome:** Local state can be recovered and schema history cannot drift silently.  
**Scope:** WAL-aware consistent backup, optional encrypted export, restore verification, migration checksums, and documented recovery. Preserve the current full-filename migration ordering; duplicate numeric prefixes are cosmetic, not a defect.  
**Likely files:** DB package, scripts, migration runner/tests, operator docs.  
**Acceptance:** Automated backup/restore reproduces database invariants; altered applied migrations are detected; secrets are not bundled unintentionally.  
**Dependencies:** TUR-015.

#### TUR-017 — Add bounded retention

**Outcome:** Events, audit detail, run steps, tool results, rejected candidates, and trash cannot grow forever.  
**Scope:** Default-safe retention policy, bounded sweeps, active-run exclusions, replay gap semantics, and immutable approval/deletion evidence.  
**Likely files:** configuration, reaper, repositories, audit/memory policy UI.  
**Acceptance:** Defaults preserve existing data; enabled pruning is batched and observable; replay reports a resync requirement instead of silently omitting history; sweeps have a tested transaction budget on the single SQLite connection.  
**Dependencies:** TUR-013, MEM-012.

#### TUR-018 — Advertise worker provider, model, and agent capabilities

**Outcome:** The orchestrator can validate routing before enqueue and reason about unavailable work.  
**Scope:** Worker-ready protocol advertises providers, models, context limits, tool support, agent IDs, and capacity.  
**Likely files:** runtime proto, worker registration, dispatcher, session config APIs.  
**Acceptance:** Unsupported selections fail before enqueue; capability loss updates queue notices; reconnect restores the registry.  
**Dependencies:** None.

### Phase 6: Connectors and multiple agents

#### CON-001 — Make the in-repo tool protocol MCP-conformant

**Outcome:** Turing's client and servers interoperate with standard MCP implementations.  
**Scope:** Adopt the official Go SDK or implement the current protocol surface; preserve `_meta.approvalToken`, server isolation, and orchestrator-owned policy.  
**Likely files:** runtime MCP client, both MCP servers/modules, conformance tests.  
**Acceptance:** A stock client discovers and calls Turing tools; Turing calls a stock test server; unknown tools still default to approval-required; no MCP port is published.  
**Dependencies:** TUR-019. Do not enable arbitrary servers in this task; service-scoped identities are required before CON-002 enables configurable servers.

#### CON-002 — Make MCP servers configuration-driven

**Outcome:** Named servers and credentials can be added without recompiling the runtime.  
**Scope:** Configured endpoints, service-scoped credentials, health/capability registry, per-server network and policy scopes.  
**Likely files:** runtime config/registry, Compose/init scripts, tool policy administration.  
**Acceptance:** Multiple servers can be added/disabled independently; discovery failure is isolated; server descriptions remain untrusted data.  
**Dependencies:** CON-001, TUR-006, TUR-018.

#### CON-003 — Add local imports before live connectors

**Outcome:** Turing can ingest user-owned knowledge with a reversible, inspectable workflow.  
**Scope:** Markdown/JSON import preview, source identity, content hashes, idempotency, candidate isolation, and rollback.  
**Likely files:** import proto/service/repository, memory candidate pipeline, Flutter import flow.  
**Acceptance:** Re-import is idempotent; imported content is not automatically added to profile/system instructions; rollback removes every projection.  
**Dependencies:** MEM-006, MEM-009, MEM-011.

#### CON-004 — Add connector consent, provenance, and revocation

**Outcome:** Calendar, email, source-control, and other integrations share one safe lifecycle.  
**Scope:** Connector registry, explicit scopes, secret storage boundary, sync cursors, source-deletion propagation, revoke, and per-source trust classification.  
**Likely files:** new connector domain, internal identities, memory evidence, UI consent/revoke flows.  
**Acceptance:** Revocation stops sync and invalidates credentials; source deletion propagates; connector content cannot promote itself into active memory or policy.  
**Dependencies:** CON-002, CON-003, TUR-006, MEM-011.

#### AGT-001 — Add a second specialized agent and explicit handoff

**Outcome:** Turing supports plural agents without surrendering dispatch, recovery, or policy ownership.  
**Scope:** Add a second `AgentId`, deterministic routing/handoff, per-agent tools and provider capabilities, capacity, events, and concurrent-run tests.  
**Likely files:** common/runtime proto, executor implementations, dispatcher, worker pools, policy registry, Flutter agent status.  
**Acceptance:** Routing is deterministic and visible; handoff preserves trace/provenance; concurrent agents do not violate session serialization, leases, fencing, or approval scope; load tests record transaction contention and queue latency with SQLite's single connection.  
**Dependencies:** TUR-010, TUR-011, TUR-014, TUR-018, TUR-019, CON-002.

## Dependency table

Every edge in this table appears in the corresponding task's `Dependencies` line. The delivery order below is topological with respect to this table.

| Task | Depends on |
|---|---|
| TUR-001 | None |
| TUR-002 | None |
| TUR-003 | None |
| TUR-004 | MEM-001 |
| TUR-005 | None |
| TUR-006 | None |
| TUR-007 | None |
| TUR-008 | None |
| TUR-009 | None |
| TUR-010 | TUR-018 |
| TUR-011 | None |
| TUR-012 | None |
| TUR-013 | None |
| TUR-014 | TUR-018 |
| TUR-015 | MEM-006 |
| TUR-016 | TUR-015 |
| TUR-017 | TUR-013, MEM-012 |
| TUR-018 | None |
| TUR-019 | None |
| TUR-020 | None |
| TUR-021 | TUR-013 |
| MEM-001 | None |
| MEM-002 | None |
| MEM-003 | MEM-002 |
| MEM-004 | MEM-002, MEM-003 |
| MEM-005 | MEM-001 |
| MEM-006 | MEM-005, TUR-013 |
| MEM-007 | MEM-006 |
| MEM-008 | MEM-003, MEM-006, MEM-007 |
| MEM-009 | MEM-003, MEM-006, MEM-007 |
| MEM-010 | MEM-009 |
| MEM-011 | MEM-001, MEM-005, MEM-006 |
| MEM-012 | TUR-013, MEM-008, MEM-009 |
| MEM-013 | MEM-003, MEM-004, MEM-005, MEM-011 |
| MEM-014 | TUR-018, MEM-001, MEM-003 |
| MEM-015 | MEM-001, MEM-005, MEM-011 |
| MEM-016 | MEM-003, MEM-004 |
| CON-001 | TUR-019 |
| CON-002 | CON-001, TUR-006, TUR-018 |
| CON-003 | MEM-006, MEM-009, MEM-011 |
| CON-004 | CON-002, CON-003, TUR-006, MEM-011 |
| AGT-001 | TUR-010, TUR-011, TUR-014, TUR-018, TUR-019, CON-002 |

## Recommended delivery order

1. **Fix user-visible falsehoods and context loss:** TUR-002, TUR-007, TUR-020.
2. **Install preventative contracts:** TUR-013, TUR-018, TUR-019, MEM-001.
3. **Close existing correctness, consent, and operability gaps:** TUR-001, TUR-003, TUR-005, TUR-006, TUR-008 through TUR-012, TUR-014, TUR-021, then TUR-004.
4. **Measure and improve lexical recall:** MEM-002, MEM-003, MEM-004, MEM-016.
5. **Ship minimum lovable memory:** MEM-005, MEM-006, MEM-007, MEM-008, MEM-011.
6. **Add safe learning and explainability:** MEM-009, MEM-010, MEM-012, MEM-015.
7. **Deliver ownership and longevity:** TUR-015, TUR-016, TUR-017.
8. **Make evidence-gated retrieval upgrades:** MEM-013 and MEM-014 only when their entry criteria pass.
9. **Add connectivity:** CON-001, CON-002, CON-003, CON-004.
10. **Add plural agents:** AGT-001 after capability, queue, streaming, observability, and MCP foundations are proven.

The first cohesive product milestone is not embeddings or connectors. It is: **the user can explicitly teach Turing a fact or instruction, inspect where it came from, see when it was used, correct it, and remove it completely.**

## Success criteria

Turing reaches the intended state when all of the following are true:

- the default path performs no background network egress;
- every durable belief has inspectable evidence and lifecycle history;
- a user can explicitly remember, correct, supersede, retract, forget, export, and explain;
- deleted source content cannot return through FTS, vectors, summaries, caches, connectors, or prompts;
- memory extraction cannot promote untrusted assistant/tool/web/file content;
- recall quality and stale-use behavior are measured in CI;
- embeddings and graphs are optional optimizations justified by recorded failures;
- remote providers and connectors disclose exactly what data they receive;
- audit, health, queue state, and terminal outcomes are visible after restart;
- standard MCP integrations preserve Turing's fail-closed policy and approval boundary;
- additional agents use explicit routing, capabilities, and handoff rather than hidden framework state.
