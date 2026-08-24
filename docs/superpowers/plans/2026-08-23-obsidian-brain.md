# Turing's Brain: Vault-Backed Memory Implementation Plan (Phase 1)

Give Turing durable memory the user can open in a text editor. The brain is
an Obsidian-compatible vault of plain Markdown — persona, profile, beliefs,
and proposals as files the user owns, edits, links, and can watch as a graph
— with SQLite underneath holding what files cannot: provenance, evidence
links, and the erasure contract. This reshapes the substrate of the audit's
MEM ladder (`docs/architecture/2026-08-18-personal-agent-audit.md`) without
discarding a single one of its invariants; where this plan and MEM-005's
SQLite-only substrate differ, the acceptance criteria transfer and the plan
says how.

Prior art positions this deliberately (researched 2026-08-23): the
files-as-memory pattern is mature (basic-memory's frontmatter + observations
+ typed wikilinks; obsidian-second-brain's tiny pinned identity file and
scheduled consolidation; Letta's pinned core blocks + searched archival
tiers), but every one of those systems lets the agent write memory
**directly**, and the memory-poisoning literature (MINJA's >95% query-only
injection; MemoryGraft's implanted experiences) demonstrates precisely what
that costs. This plan takes the sources' shapes and the papers' defenses:
write-time gating, provenance binding, retrieval-time framing — all three on
machinery Turing already has.

## The problem, stated precisely

Turing has episodic recall (FTS over raw messages, MEM-002) and nothing
else: no persona, no user profile, no curated beliefs, no way to say
"remember this." The audit says it plainly: "not yet a personal agent that
learns about its user." Meanwhile the egress plumbing already reserves the
whole consent surface this feature needs — `MEMORY_PROFILE` is a defined
`EgressDataCategory`, and `MemoryProfileApplicable` rides through
`resolveEgressContext`, the signed challenge payload, the frozen decision,
and the runtime's shape validation — **and nothing ever sets it true**. This
plan is what that field was reserved for.

Three stakes:

1. **Memory writes are the most dangerous mutation in the system** —
   durable state that steers every future run, writable by a model that
   ingests untrusted content. MEM-009's acceptance line is the north star:
   *"Turing can learn without silently rewriting its beliefs."*
2. **Memory reads are untrusted prompt material** — the same invariant
   VISION applies to skill text applies to every retrieved memory.
3. **Memory leaving the machine is egress** — pinned memory riding to a
   remote model or external agent joins the per-run consent, category and
   fingerprint alike, exactly as skills did in #80.

## The brain's anatomy (locked)

The vault mounts like skills do: `cfg.MemoryRoot`, default `/memory`,
compose-mounted read/write, plain folder Obsidian can open directly.

```
memory/
  persona.md        # who Turing is — USER-authored, agent NEVER writes
  profile.md        # who the user is — promotion-gated, pinned
  inbox/            # agent proposals — candidates, never beliefs
  beliefs/          # promoted notes: people/, projects/, decisions/, ...
```

**Tier 1 — `persona.md`: user-owned configuration, not memory.** Voice,
values, standing disposition. Pinned into every run under a fixed budget.
The agent reads it and can never write it — a deliberate divergence from
Letta's self-editing persona, chosen because persona drift via accumulated
model writes is exactly the self-reinforcing compromise the Zombie-Agents
literature describes, and because in Turing's control-first ethos the
personality is the user's instrument panel, not the model's diary. Shipped
as a commented default by `init.sh`; edited in Obsidian or the app. This is
the **only instruction-bearing memory file**, and it may be one only
because a human is its sole author.

**Tier 2 — `profile.md`: distilled current beliefs about the user.**
Pinned beside the persona. Promotion-gated: the agent proposes changes into
the inbox; only the user lands them (in the app, or by editing the file in
Obsidian — user edits always win, as with skills).

**Tier 3 — `beliefs/`: the wiki-linked note graph.** Facts, projects,
people, decisions — basic-memory's conventions (frontmatter; short
observation bullets; typed `[[wikilinks]]`), because those conventions are
what make Obsidian's graph — and the 3D galaxy plugins — render the brain
for free: folders are colored clusters, links are edges, hubs are stars.
Not pinned; reached by the model through a search tool, returned as framed
evidence.

**Tier 4 — `inbox/`: thoughts not yet believed.** Every agent-originated
memory write lands here and nowhere else. In the graph it is its own
colored region — proposals visually distinct from beliefs, and promotion
physically moves the file from one region to the other.

**Episodic memory stays where it is.** FTS recall over real transcripts
(MEM-002) is the episodic layer; the vault distills it, never duplicates
it. Dated episode-summary notes are Phase 2.

## Design decisions (locked)

**Candidates never become beliefs by the agent's hand.** The write tool
(`memory.remember`) writes **only** into `inbox/`, whatever its policy — a
path-confined write, refused for any other target the way sandbox writes
are confined today. The tool's policy is the friction knob the user
controls (`approval_required` by default via `DefaultPolicyFor`'s unknown
fallback — no new defaulting logic; relaxable to `safe` for frictionless
*proposing*), but relaxing it never changes *where* writes land. Promotion
— moving a note from `inbox/` to `beliefs/` or applying a proposed profile
edit — is a user action, in the app or by moving the file. Direct
agent-writes to `beliefs/` (basic-memory's ergonomics) are explicitly
deferred, not designed here: they need MEM-010's reversibility story first.

**Memory is a pseudo-server, and every skills special-case gains the third
sibling.** `memory.remember`, `memory.search`, `memory.read` carry
`server_name = 'memory'`, `mcp_server_id IS NULL`. The audited list from
the integrations plan applies verbatim — `filterRegisteredWorkerTools`
(policy-only availability, no `present`/`enabled` reads),
`UpsertTools`' pseudo-server branch, `BundledServerForTool` (a `memory.`
prefix case, which also shadow-protects the names against registered MCP
servers), `mcpregistry/import.go`'s reserved names gaining `memory`,
`beaconServerName`'s fallback map, and the `0016` triggers widened to
`('skills','integrations','memory')` — a migration whose number is
whatever is next at implementation time (`0018` as of this writing; both
pins in `migrations_test.go` move).

**Reads are evidence, not authority — mechanically, not rhetorically.**
`memory.search`/`memory.read` results return through the same per-call
nonce-framed untrusted-content wrapper the integrations results use
(shared helper, not a copy), bounded by a named constant
(`maxMemoryResultBytes = 16 KiB`, rune-safe). The pinned tiers load with
the same sanitization discipline as skill names where text is rendered
into transcripts. Framing is hygiene, not a boundary — the boundary
remains that anything the model does still passes tool policy, approvals,
and egress consent.

**Pinned tiers have named budgets and snapshot semantics.**
`maxPersonaBytes = 4096`, `maxProfileBytes = 4096` (rune-safe truncation
with an in-context notice when over — a personality cut mid-sentence must
say so rather than silently end). Persona and profile are snapshotted at
enqueue into the job, exactly as skills are — a mid-run edit changes the
next run, not this one. A global memory toggle (settings) turns pinned
loading and the memory tools off together; per-session and per-turn
controls are MEM-008 scope, deferred and said so.

**Provenance lives in both layers, and each holds what it can.**
Frontmatter carries the human-legible half: `source_sessions`,
`observed_at`, `status`. SQLite sidecar rows (path + content hash +
evidence links to session/message ids) carry the queryable half — the
part the critique piece is right that Markdown cannot do. The sidecar is
derived state, rebuilt by reconcile-on-read (the `skillfiles` pattern:
files are truth for content; conflicts fail closed; user edits win).

**The erasure contract, reshaped honestly (this is MEM-005's hardest
acceptance criterion, kept).** Deleting a session (TUR-004):
`inbox/` candidates whose evidence traces to it are **deleted** — they are
machine-authored proposals and nothing of the user's is lost. Promoted
beliefs are different: promotion transferred authorship to the user, so
the file survives, but its provenance is rewritten — evidence references
to the deleted session become `withdrawn` markers in frontmatter and the
sidecar, so a belief can never cite evidence that no longer exists. The
distinction is the plan's answer to "how do files honor cascade deletion
without the system deleting user-owned documents."

**Memory egress joins the consent — category, flag, and fingerprint.**
When pinned memory would ride to a remote provider or external agent:
`MemoryProfileApplicable` is finally set (`providerEgress && pinned
memory non-empty`, both conjuncts, all composition mirrors in both
modules — the #80 checklist applies item for item, including
`validateEgressDecisionShape`'s exact-set list and the data-category
attachment), the disclosure carries `MEMORY_PROFILE`, and a
`memory_snapshot_fingerprint` joins the signed challenge payload and the
frozen decision the same way `skill_snapshot_fingerprint` does — prepare
derives pinned content and fingerprint from one read; a persona or
profile edit between prepare and send is refused with legible wording.
This means a challenge-payload field addition and the **lockstep version
bump pair** (`backendegress.DecisionVersion` + `egressChallengeVersion`)
with the fixture sweep that entails — budgeted, not discovered.
`memory.search` over the local vault is not egress and needs no consent;
what its *results* cause the model to do is gated elsewhere, as always.

**What does NOT go in memory: instructions.** Procedures live behind the
skill gate (enablement + capability grants). A note that says "always do
X" is a skill wearing a memory costume — the exact costume memory-
poisoning attacks wear. Phase 1 enforces this at the only place it can be
enforced cheaply and honestly: the loading frame declares memory
non-authoritative, the persona (human-authored) is the sole instruction
channel among the files, and the promotion UI shows full note content so
the human sees what they are about to believe. Automated
imperative-content linting on inbox candidates is Phase 2, named so
nobody believes it exists sooner.

**The visualization is earned, not built.** Zero rendering code in scope.
The layout above IS the visualization story: folders → clusters, typed
wikilinks → edges, persona/profile → hub stars, inbox → the
distinctly-colored "not yet believed" nebula, promotion → a star
migrating between regions. Obsidian's built-in graph shows it today;
Agentage Galaxy shows it in rotating 3D; agentcairn-style
provenance/currency coloring is possible later because the frontmatter
carries what it needs.

## What gets built (Phase 1)

- **Migration (next free number)** — widen the pseudo-server triggers to
  include `memory`; sidecar table (`memory_provenance`: path, content
  hash, evidence session/message refs, status, timestamps) with cascade
  hooks into session deletion; both `migrations_test.go` pins.
- **`internal/memoryfiles`** (new, `skillfiles`' sibling) — vault loader:
  layout, frontmatter parse, budgets, reconcile-on-read, path confinement
  (no `..`, no symlink escape — the sandbox rules).
- **`internal/repository`** — provenance sidecar CRUD; the erasure hooks
  in session deletion (delete inbox candidates, withdraw belief
  evidence); enqueue snapshot of persona/profile into the job (the skills
  snapshot pattern, same fingerprint helper family in
  `turing-backend/internal/egress`).
- **`internal/service/memory`** (new) — `memory.remember` (inbox-only,
  path-confined), `memory.search` (FTS over the sidecar index),
  `memory.read`; promotion/rejection RPCs (public facet; management
  refused internally — the facet split with both refusal directions
  tested, per #81's precedent); the global toggle.
- **Egress (both modules)** — the conditional `MEMORY_PROFILE` category
  with all mirrors; `memory_snapshot_fingerprint` through the signed
  payload, `payloadMatchesEgressContext` (via `EqualFunc` where structs
  hold slices), the enqueue freeze, `EnqueueRequestFingerprint` (version
  bump), and the runtime's `validateEgressDecisionShape`; the legible
  memory-changed refusal wording beside the skills one.
- **Runtime (`agent-runtime-go`)** — pinned injection of
  persona/profile from the job snapshot under their budgets; the memory
  tool lister (dynamic, policy-only predicate, empty-not-error on an
  unconfigured vault — the #81 lister lessons apply verbatim); framed
  tool results.
- **`proto/turing/v1`** — memory service RPCs; `memory_profile` fields
  where the disclosure needs them; job snapshot fields; pinned numbers,
  regenerate with the pinned toolchain, update
  `tests/proto_contract_test.go`.
- **Client** — a Memory page: the three tiers, inbox review
  (promote/edit/reject with full content shown), persona/profile editors,
  the global toggle, provenance display per note; egress dialog gains the
  memory line when `MEMORY_PROFILE` rides. Compact layout via the
  existing sweep.
- **`scripts/init.sh` + compose** — the `/memory` mount and the default
  `persona.md`.
- **Existing fixtures** — the version bump's sweep (the #80 inventory
  pattern: symbolic references, with the known hardcoded-JSON exception
  in `service/audit/service_test.go` left alone).

## The tests that gate the merge

For each numbered gate, break the production check, watch the right test
fail, restore.

1. **The agent cannot write beliefs.** `memory.remember` targeting
   `persona.md`, `profile.md`, or `beliefs/…` — by name, by `../`, or by
   symlink — is refused; the write lands only under `inbox/`. Relaxing
   the tool policy to `safe` changes approval friction, not the target
   confinement.
2. **Candidates are never active.** Pinned loading reads only persona +
   profile; `memory.search` over default scope excludes `inbox/` (or
   labels it proposal — pick one and test it); an inbox note influences
   no run until promoted.
3. **Promotion is user-land and reconciles.** Moving a file from
   `inbox/` to `beliefs/` (as Obsidian would) is picked up by
   reconcile-on-read with sidecar provenance carried over; the app's
   promote RPC does the same thing the file move does.
4. **Budgets and framing hold.** Persona/profile over-budget truncate
   rune-safe with an in-context truncation notice; search results are
   nonce-framed (two calls, different nonces) and bounded; a result
   reproducing the frame delimiters cannot close the frame.
5. **Erasure cascades correctly by tier.** Delete a session: inbox
   candidates citing it are gone; a promoted belief citing it survives
   with evidence marked withdrawn in both frontmatter and sidecar;
   nothing else changes.
6. **The egress binding is real.** Local-Ollama run with memory enabled
   and no remote destination: no `MEMORY_PROFILE`, no consent dialog.
   Remote run: category present, `MemoryProfileApplicable` true, and —
   through `SendMessage`, two-sided — editing `persona.md` between
   prepare and send is refused with the memory wording while non-memory
   drift keeps its own wording; the runtime's exact-set check passes end
   to end through `Execute` (the both-modules leg), and a decision
   claiming the category over an empty snapshot is refused.
7. **The toggle is total.** Memory off: nothing pinned, tools
   unavailable (absent from the registry, not offered-and-refused),
   category never attached, and turning it back on requires no restart
   (the registry-change notification, as #81).
8. **Snapshot semantics.** Persona edited mid-run: the running job keeps
   its snapshot; the next run sees the edit. Same for profile.
9. **Version skew fails closed at dispatch** — the literal pre-bump
   number, not `N-1` (the #80 lesson).
10. **The facets refuse in both directions** — runtime identity can call
    the tool RPCs and not the management RPCs; the client key the
    reverse.

## Deferred, deliberately (each is a named MEM rung, not a loose end)

- **Automatic candidate extraction** (MEM-009): after-run distillation
  proposing inbox notes — with its hard constraint pre-committed here:
  *user-authored text only* as source; assistant/tool/recalled content
  can never become a candidate. Phase 1 ships the substrate it lands on.
- **Consolidation automations** (MEM-010): scheduled
  contradiction-review and freshness passes writing proposals — the
  automations scheduler exists; the reversibility design does not yet.
- **Semantic retrieval** (MEM-013) and episode summaries; per-session
  and per-turn memory controls (MEM-008); imperative-content linting on
  candidates; direct-write trusted areas.
- **A vault chosen by the user** (pointing Turing at an existing
  personal vault): begins as a *separate mount* decision with its own
  trust story — Phase 1's vault is Turing's own brain, not the user's
  notes.

## Documentation the implementation PR must update

- `docs/architecture/2026-08-18-personal-agent-audit.md` — a substrate
  note on the MEM ladder: files-for-beliefs + SQLite-for-evidence, which
  rungs this fulfills (005/006/007 in reshaped form) and which remain.
- `docs/architecture/remote-egress-policy.md` — `MEMORY_PROFILE` goes
  from reserved to live; the memory fingerprint beside the skills one.
- `docs/VISION.md` — memory joins the untrusted-input invariant's list
  (skill text, retrieved integration content, retrieved memory); the
  "nothing leaves by default" section notes memory rides only under the
  consent it already describes.
- `CLAUDE.md` — the `/memory` mount beside `skills/` and `sandbox/`.
