# Turing's Brain: Vault-Backed Memory Implementation Plan (Phase 1)

Give Turing durable memory the user can open in a text editor. The brain is
an Obsidian-compatible vault of plain Markdown — persona, profile, beliefs,
and proposals as files the user owns, edits, links, and can watch as a graph
— with SQLite underneath holding what files cannot: identity, evidence,
search, and the erasure contract. This plan implements the audit's MEM
ladder rungs 005/006/007 in reshaped form, **under the governance contract
`docs/architecture/memory-governance.md` (MEM-001), which it amends in one
named place and otherwise obeys to the letter** — the audit calls that
contract the blocker for every derived-memory task, and this plan treats it
that way.

Prior art (researched 2026-08-23): files-as-memory is mature
(basic-memory's frontmatter + typed wikilinks; obsidian-second-brain's tiny
pinned identity file and scheduled consolidation; Letta's pinned core
blocks + searched archival tiers) — but every one of those systems lets the
agent write memory directly, and the poisoning literature (MINJA's >95%
query-only injection, MemoryGraft's implanted experiences) shows what that
costs. This plan takes the sources' shapes and the papers' defenses —
write-time gating, provenance binding, retrieval-time framing — on
machinery Turing already has.

## The problem, stated precisely

Turing has episodic recall (FTS over raw messages, MEM-002) and nothing
else: no persona, no user profile, no curated beliefs, no "remember this."
The egress plumbing already reserves the entire consent surface this needs:
`MEMORY_PROFILE` is a defined category (allowlisted in
`repository/egress.go`, labelled in the notice map, mirrored in Dart —
exhaustiveness-forced by `TestEgressCategoryPolicyCoversProtoEnum`), and
`MemoryProfileApplicable` rides the full path — context, signed challenge,
frozen decision (`0014`'s column), enqueue fingerprint, job proto, runtime
validation — **and nothing ever sets it true**. One wrinkle, load-bearing:
the runtime today *hard-refuses* any decision claiming it —
`agent-runtime-go/internal/agent/egress.go` checks
`decision.GetMemoryProfileApplicable()` as a bare must-be-false, unlike
`RecallApplicable` which is recomputed and compared. Turning this feature
on means rewriting that line into an equality mirror, which means the
pinned-memory snapshot must live on the job proto and be re-derivable by
the runtime. Also: `TestRemoteRunRejectsUnsupportedDisclosureCategories`
uses `MEMORY_PROFILE` as its unsupported example and must be re-pointed at
`ATTACHMENTS` — the last remaining append-site-free value.

Three stakes: memory writes are the most dangerous mutation in the system
(durable state steering every future run, writable by a model that ingests
untrusted content — MEM-009's line is the north star: *"Turing can learn
without silently rewriting its beliefs"*); memory reads are untrusted
prompt material (the invariant VISION applies to skill text); memory
leaving the machine is egress (category, flag, and fingerprint, as #80 did
for skills).

## The governance amendment (one, explicit)

The contract's withdrawal rule: *"every fact, candidate, revision, …
derived from that source is removed in the same transaction or through a
database-enforced cascade"* and *"no derived row may outlive the source it
depends on."* Applied naively to a vault, deleting a session would delete
user-curated notes — files the user may have edited, linked, and built on.

The contract itself supplies the resolution: its evidence taxonomy already
classes *"explicit profile edits, a deliberate 'remember this' action"* as
**user-authored evidence** that *"may become active memory through an
explicit user action"* (its principle 1). This plan's amendment makes one
thing explicit: **promotion is authorship.** When the user promotes an
inbox candidate — with its full content displayed, editable before
acceptance — the promotion is itself the explicit user action; the item's
*source becomes that action*, and its links to the originating session
demote from load-bearing dependencies to annotations. Consequences,
honestly:

- **Machine-owned rows obey the contract unamended.** Inbox candidates,
  their sidecar rows, and evidence annotations are derived state: source
  deletion removes them via database-enforced cascade plus the external
  file-cleanup mechanism below. A failed cleanup fails the withdrawal.
- **User-adopted content survives source deletion** — the same way a note
  the user typed by hand about a conversation would — with its evidence
  annotations withdrawn (removed rows; frontmatter refs rewritten to
  `withdrawn` on the next reconcile), so a belief can never *cite*
  evidence that no longer exists, and the UI shows it as unevidenced.
  Stated plainly: the *content* of an adopted belief persists after its
  originating session is deleted. That is the amendment.
- **The vault is not a second copy of conversation content** (the
  contract's §Boundary). `memory.remember` writes bounded distillations
  (`maxMemoryNoteBytes = 16 KiB`), never transcripts; the promotion UI is
  where a human confirms that. Verbatim-copy prevention is review-gated
  in Phase 1 and linted in Phase 2, and the plan says so rather than
  claiming enforcement it doesn't have.
- The erasure claim **stops at the vault directory**: the contract's
  §Backup already scopes deletion against user-made copies, and a vault
  is designed to be synced. Restated here so nobody reads more into it.

The implementation PR updates `docs/architecture/memory-governance.md`
with this amendment as a numbered section — the contract is code review
for memory changes, and this plan refuses to route around it.

## The brain's anatomy (locked)

The vault mounts like skills: `cfg.MemoryRoot` (`MEMORY_ROOT`, default
`/memory`, clean-absolute-validated like `SKILLS_ROOT`), compose-mounted
read/write, provisioned by `init.sh` in the `provision_skills` mold
(symlink checks, `0700`, ownership), openable directly in Obsidian.

```
memory/
  persona.md        # who Turing is — USER-authored, agent NEVER writes
  profile.md        # who the user is — USER-authored prose; agent proposes diffs
  inbox/            # agent proposals — candidates, never beliefs
  beliefs/          # promoted notes: people/, projects/, decisions/, ...
```

**Tier 1 — `persona.md`: user-owned configuration, not memory.** Voice,
values, standing disposition; pinned into every run. The agent reads it
and can never write it — a deliberate divergence from Letta's
self-editing persona, because persona drift via accumulated model writes
is the self-reinforcing compromise the literature describes. Shipped as a
commented default by `init.sh`. The **only instruction-bearing memory
file**, permitted to be one only because a human is its sole author.

**Tier 2 — `profile.md`: user-authored prose about the user.** Pinned
beside the persona. It carries **no sidecar evidence and no per-statement
provenance** — it is tier-1-like user-owned text. The agent contributes
by writing a *proposed edit* into the inbox (a candidate carrying the
suggested new text and its evidence); applying it — in the app or by
hand — is authorship under the amendment. This dissolves the otherwise
unsolvable problem of withdrawing one session's contribution from a
freely-edited prose file: unapplied proposals die with their source;
applied text is the user's.

**Tier 3 — `beliefs/`: the wiki-linked note graph.** Facts, projects,
people, decisions — basic-memory's conventions (frontmatter, short
observation bullets, typed `[[wikilinks]]`), because those conventions
are what make Obsidian's graph and the 3D galaxy plugins render the brain
for free: folders are colored clusters, links are edges, hubs are stars.
Never pinned; reached through `memory.search`/`memory.read`, returned as
framed evidence.

**Tier 4 — `inbox/`: thoughts not yet believed.** Every agent-originated
write lands here and nowhere else — its own colored region in the graph,
proposals visually distinct from beliefs, promotion physically moving the
file between regions.

**Episodic memory stays where it is** (FTS recall over transcripts,
MEM-002). The vault distills it, never duplicates it. Episode summaries
are Phase 2.

## Design decisions (locked)

**Candidates never become beliefs by the agent's hand.**
`memory.remember` writes **only** into `inbox/`, whatever its policy. The
confinement lives in the `memoryfiles` write primitive itself — not in
any RPC- or approval-adjacent layer — so relaxing the tool's policy to
`safe` changes proposal friction, never the target. The server names the
file (ULID plus a sanitized slug of a model-supplied title — the model
never controls the path), and the note body is bounded
(`maxMemoryNoteBytes`). Promotion — moving a note to `beliefs/` or
applying a profile proposal — is a user action, via RPC or by moving the
file in Obsidian; both converge in reconcile. Direct agent-writes to
`beliefs/` are deferred to MEM-010's reversibility design, not designed
here.

**Note identity is a frontmatter id, not a path.** Obsidian's most common
operation is rename, and renames rewrite wikilinks. Each note carries
`id` (ULID) in frontmatter — assigned by `memory.remember`, or by
reconcile for user-created notes lacking one. The sidecar keys on id;
renames re-link by id (content hash as fallback), so provenance survives
the thing vaults do daily. Frontmatter parsing is **lenient** — unknown
fields are preserved-and-ignored (the user is invited to annotate; strict
`KnownFields` is the wrong loader posture here, unlike SKILL.md), and
malformed frontmatter surfaces as a per-note parse-error row, skills
style: fail-soft, visible, never fatal to the vault.

**The vault walk is Obsidian-aware and bounded.** The scan indexes
`*.md` only, skips every dot-directory (`.obsidian/` holds plugin
JavaScript; `.trash/` holds Obsidian's soft-deletes — indexing either is
untrusted-content ingestion), skips `*.canvas` and sync
`conflicted copy` artifacts, and enforces `maxVaultIndexedFiles = 4096`
with a legible refusal naming the remedy. The scan runs **outside** any
write transaction (skills' `scanSkills` precedent, with the transaction
budget stated per MEM-009/010's own acceptance discipline) with an
mtime/size cache so reconcile cost is proportional to churn, not vault
size.

**Path confinement follows skillfiles, not mcp-files — refuse symlinks,
never resolve them** (the two in-repo precedents genuinely differ:
`mcp-files` `EvalSymlinks`-resolves; `skillfiles` walks with
`openat`+`O_NOFOLLOW` and refuses). `memoryfiles` needs what no module
currently has and cannot import across module boundaries: a
**write-capable** confined opener — the component-wise `openat` walk
terminating in `O_CREAT|O_EXCL|O_NOFOLLOW` for creates, and a
compare-and-swap variant (content-hash-conditioned rename-into-place) for
the promotion RPC's updates, because the vault has a second concurrent
writer named Obsidian and a lost profile update is a real bug, not a
theory. Reads get the same confinement — `memory.read` is an escape
surface exactly as large as the write one.

**Search has a real substrate, and it is an index of the vault, not a
copy of conversations.** The sidecar splits in two, each classified in
`schema_invariants_test.go`'s manifest in the same migration that creates
it (the contract's own rule):
- `memory_notes` (independent): id, path, content hash, status,
  timestamps, **and a body projection** — the note text, mirrored from
  the file by reconcile. This is an index projection *of the vault*
  (whose primary is the files), not a second copy of conversation
  content; the §Boundary sentence targets transcripts, and
  `memory.remember`'s bounded-distillation rule is what keeps transcripts
  out of the vault in the first place.
- `memory_notes_fts` (FTS5 external-content over `memory_notes`, with the
  `AFTER DELETE` trigger and an entry in `ftsProjectionDeleteChecks` —
  the guard's probe row works because deletes flow through the table,
  driven by reconcile when a file vanishes; a note the user deletes or
  trashes in Obsidian leaves the index on the next reconcile, and
  `memory.search` reconciles before querying).
- `memory_evidence` (cascade-owned twice over): candidate/annotation rows
  keyed to `memory_notes.id` AND to source sessions with
  `ON DELETE CASCADE` — the database-enforced half of withdrawal.

**Erasure follows the artifact-cleaner pattern, because "cascade hook"
does not exist for files.** Session deletion already refuses to finalize
while external artifacts remain (`ReserveSandboxArtifactTx`, a manifest
row inside the transaction, `failed_external` + retryable receipt,
bounded out-of-band cleanup, re-advance). Vault file deletion for
inbox candidates joins that mechanism — which requires promoting
`SessionService`'s single hardwired `SetArtifactCleaner` into a cleaner
**list** (a named change in `sessions/service.go` + `app.go`, not a
discovery). DB-side: `memory_evidence` cascades in the deleting
transaction; `memory_notes` rows for inbox candidates cascade with their
session; belief rows are `independent` — under the amendment they are
user-authored primary content, not derived rows, and the manifest
classification says so out loud.

**Reads are evidence, not authority — mechanically.** Search and read
results return through the per-call nonce framing — which today lives as
the **unexported** `frameIntegrationResult` with hardcoded wording, so
the real first task is extracting a parameterized helper into
`turing-backend/internal/egress` (named destination; both callers move
to it, the "one implementation of a security control" rule). Bounds:
`maxMemoryResultBytes = 16 KiB`, rune-safe, truncation announced.
`memory.search` has **no model-controllable scope parameter** in Phase 1
— default and only scope is `beliefs/`; `memory.read` targets
`beliefs/` ids only. Neither can touch `inbox/` (candidates stay
inactive), nor `persona.md`/`profile.md` — the pinned tiers are readable
only as their fingerprinted, truncated pinned selves, so no tool path
lets over-budget persona bytes reach the model or a remote provider
outside the preimage.

**Tool policies: two seeded, one defaulted, all read-only-flagged
correctly.** `memory.search` and `memory.read` are seeded `safe` in
`seedPolicies` — they are local reads of local files, the exact class as
the already-safe-seeded `files.read`; leaving them `approval_required`
would make the brain a nag (and `skills_list` shows the precedent's
cost). Both join `readOnlyTools`, or a failed local search kills the run
as `SideEffectUnknownError` — the exact bug the `read_only` bit exists to
prevent. `memory.remember` takes the unknown-tool fallback
(`approval_required`) and is **refused on automation allowlists** at save
(the integrations precedent, same repository check): an unattended,
pre-authorized remember is the Phase 1 hole through which
non-user-authored text would become candidates before MEM-009's
extraction gate exists.

**Pinned tiers: budgets, snapshot, context-ladder seat, and binding.**
`maxPersonaBytes = 4096`, `maxProfileBytes = 4096`, rune-safe truncation
with an in-context notice. Snapshotted at enqueue into the job proto
(fields the runtime can re-derive the flag and fingerprint from) — a
mid-run edit changes the next run. In the context-budget ladder the
pinned pair is a **new droppable dimension** (`MemoryOmitted`, with its
notice string and `Omissions.Notice()` arm), dropping after recall and
before the skill index — silently truncating a persona is the one
behavior every implementer defaults to and this plan forbids: over
budget truncates *with notice*; over context drops *with notice*.

**Memory egress joins the consent — category, flag, fingerprint, and the
tool-results honesty gap closed.** When pinned memory would ride
(`providerEgress && pinned snapshot non-empty` — and the default
`persona.md` shipped by init counts, so a fresh install discloses
honestly rather than a test passing on an accidentally-empty vault) OR
when memory tools are in `selected_tools` on a provider-egress run
(belief content reaches the remote as tool results; without this
conjunct the entire belief graph could leave under a generic "Tool
results" line while the memory disclosure stays silent — the exact
over/under-claiming #80 existed to fix): `MemoryProfileApplicable` is
set, the category attaches, and both runtime mirrors move — the
exact-set `required` list AND the truthiness gate rewritten as an
equality mirror recomputed from the job. `memory_snapshot_fingerprint`
(preimage: the **post-truncation pinned bytes of both tiers, notices
included** — exactly what the job carries, so the runtime re-derives and
refuses on mismatch like it does for skills; a shared no-`omitempty`
preimage struct in `backendegress`) joins the signed challenge payload
(a plain string compare — no `EqualFunc` needed), the enqueue
fingerprint (version bump), and the frozen decision — which means the
**fourth `run_egress_decisions` rename-copy-drop rebuild**
(fresh temp name, cascade FK preserved per the schema-invariants pin,
index recreated), a new `RunEgressDecision` proto field (next free
number), and the `proto_contract_test.go` pin: budgeted, since #80
explicitly refused this rebuild for a lesser reason and this plan should
not pretend it is free. Lockstep version pair
(`backendegress.DecisionVersion` + `egressChallengeVersion`) with the
#80 fixture sweep, including its known hardcoded-JSON exception.
Divergence noted in one sentence: `RecallApplicable` excludes
external-agent runs; memory (like skills) rides to them — adjacent lines
in both modules, deliberate, stated. The drift refusal gets its own
legible wording beside the skills one; the Obsidian-autosave reality —
a user with `profile.md` open on autosave will hit prepare/send
mismatches repeatedly — is named as UX: the client surfaces "close the
memory editor and re-prepare" rather than a bare mismatch loop
(auto-re-prepare is deferred, said so).

**The toggle governs new enqueues; snapshots and consents already taken
stand.** Stored in the existing `settings` table. Off: nothing pinned at
enqueue, memory tools absent from the runtime registry — which forces
the lister to be **dynamic** (`ListMemoryTools` on the memory service's
internal facet, runtime-identity entry in `app.go`, the
`ListIntegrationTools` wiring shape; a static skills-style lister cannot
deliver absence), empty-not-error on an unconfigured or toggled-off
vault, with the registry-change notification firing on toggle so no
restart is needed. Queued and in-flight jobs keep their snapshots and
their consented decisions — consistent with skills semantics and with
consent-at-prepare; test 7 asserts exactly this split rather than a
"total" claim the snapshot design contradicts.

**The pseudo-server list, audited against post-#81 code (three arms
everywhere there are two, and no phantom edits):**
`filterRegisteredWorkerTools`' policy-only availability;
`UpsertTools`' pseudo branch; `BundledServerForTool` gains `memory.`;
`mcpregistry/import.go` reserved names AND the **second copy** in
`mcpregistry/management.go` (`RegisterMcpServer`'s check — miss it and a
user can register an MCP server named `memory` from the app);
`ListPseudoServerTools`' RPC-level allowlist gains a `memory` arm (the
Memory page's policy display has no read API without it); the live
triggers — which are **0017's**, not 0016's (0017 already
dropped/recreated them; SQLite has no `ALTER TRIGGER`, so the new
migration drops/recreates from 0017's text or silently loses the
`integrations` carve-out); `readOnlyTools`. Explicitly **not** on the
list: `beaconServerName` — `memory.*` names split correctly on the
generic first-dot fallback, and there is no map to extend; budgeting an
edit there sends an implementer hunting phantom work.

**What does NOT go in memory: instructions.** Procedures live behind the
skill gate. The loading frame declares memory non-authoritative; the
persona (human-authored) is the sole instruction channel among the
files; the promotion UI shows full content so the human sees what they
are about to believe. Imperative-content linting on candidates is Phase
2, named so nobody believes it exists sooner.

**The visualization is earned, not built.** Zero rendering code. Folders
→ clusters, typed wikilinks → edges, persona/profile → hub stars, inbox
→ the distinctly-colored "not yet believed" region, promotion → a star
migrating between regions. Obsidian's graph shows it today; Agentage
Galaxy in 3D; agentcairn-style provenance coloring later because the
frontmatter carries what it needs.

## What gets built (Phase 1)

- **Migration (next free number; `0018` as of this writing — both
  `migrations_test.go` pins move)** — triggers dropped/recreated from
  0017's text with the `memory` arm; `memory_notes` (+ body projection),
  `memory_notes_fts` (+ `AFTER DELETE` trigger), `memory_evidence`
  (double cascade); the `run_egress_decisions` rebuild for
  `memory_snapshot_fingerprint`; `schema_invariants_test.go` manifest
  entries (`memory_notes` independent — with the amendment as its stated
  justification; `memory_evidence` cascade-owned; the FTS pair in
  `ftsProjectionDeleteChecks`).
- **`internal/memoryfiles`** (new) — vault layout, lenient frontmatter,
  id assignment, Obsidian-aware bounded walk with mtime cache, the
  read/write/CAS confined-open primitives (refuse-symlinks posture).
- **`turing-backend/internal/egress`** — the extracted, parameterized
  framing/truncation helper (integrations moves onto it); the memory
  preimage struct + fingerprint beside the skills one; the version bump
  pair.
- **`internal/repository`** — sidecar CRUD + reconcile; enqueue snapshot
  of persona/profile; the erasure paths (evidence cascade in-tx; inbox
  file cleanup via the artifact-manifest mechanism); the automation
  allowlist refusal for `memory.remember`; the settings-table toggle.
- **`internal/service/sessions` + `app.go`** — `SetArtifactCleaner`
  becomes a cleaner list; the vault cleaner registers beside the
  mcp-files one.
- **`internal/service/memory`** (new) — `memory.remember` /
  `memory.search` / `memory.read` (public/internal facet split, both
  refusal directions); promotion/rejection/profile-apply RPCs;
  `ListMemoryTools` (internal); toggle RPCs.
- **Egress (both modules)** — the conditional category with all mirrors
  including the runtime's rewritten truthiness gate and re-derived
  fingerprint; the two-conjunct applicability (pinned OR memory tools
  selected); legible drift wording;
  `TestRemoteRunRejectsUnsupportedDisclosureCategories` re-pointed at
  `ATTACHMENTS`.
- **Runtime (`agent-runtime-go`)** — pinned injection under the
  `MemoryOmitted` budget dimension; the dynamic lister client wiring
  (`cmd/runtime/main.go`, `GeneralAssistantTools`); framed tool results;
  `readOnlyTools` classification honored.
- **`proto/turing/v1`** — memory service; job snapshot fields; decision
  + disclosure fields; runtime-identity method names; pinned numbers,
  pinned-toolchain regeneration, `proto_contract_test.go`.
- **Client** — Memory page (tiers, inbox review with full content,
  persona/profile editors with the autosave-mismatch UX, toggle,
  provenance-with-withdrawn display); egress dialog's memory line.
- **`scripts/init.sh` + compose + `CLAUDE.md`** — the `/memory` mount,
  default `persona.md`, the doc line beside `skills/` and `sandbox/`.

## The tests that gate the merge

Break each production gate, watch the right test fail, restore.

1. **The agent cannot write beliefs — and cannot read outside them.**
   `memory.remember` targeting `persona.md`, `profile.md`, `beliefs/…` —
   by name, `../`, absolute path, or a symlink planted *inside*
   `inbox/` — is refused (the leg that kills a lexical-only check);
   `safe` policy changes friction, not confinement. **Same suite for
   `memory.read`**: `../`, absolute paths, `/skills`, the database file,
   symlinks — the read tool is an equal escape surface and gets equal
   tests.
2. **Candidates are never active.** Pinned loading reads only
   persona+profile; search and read cannot reach `inbox/` **by any
   argument** (no scope parameter exists to abuse); an inbox note
   influences nothing until promoted.
3. **Promotion converges and survives Obsidian.** RPC-promotion and
   file-move promotion produce identical state; a **rename inside
   `beliefs/`** keeps id, provenance, and index (the daily-Obsidian-op
   leg); a concurrent Obsidian edit racing the profile-apply RPC loses
   no user text (the CAS leg).
4. **Budgets and framing hold, on both tools.** Persona/profile
   over-budget truncate rune-safe with notices; `memory.search` AND
   `memory.read` results are nonce-framed (two calls, distinct nonces),
   bounded, spoof-resistant; a truncation landing mid-multibyte-rune
   yields valid UTF-8 (the probe computes the boundary, per #78's
   lesson).
5. **Erasure cascades by tier, and survives failure.** Delete a session:
   evidence rows gone in the deleting transaction; inbox candidates'
   files removed via the cleaner — and when the cleaner **fails**, the
   receipt is `failed_external` + retryable and the session is not
   reported deleted until retry succeeds (the non-atomicity leg);
   promoted beliefs survive with evidence withdrawn in sidecar and, on
   next reconcile, frontmatter; a reconcile after a crash **cannot
   resurrect withdrawn evidence from stale frontmatter** (sidecar wins
   for evidence state — the direction of authority is per-field and this
   test pins it).
6. **The egress binding is real, two-sided, and per-tier.** Local-only
   run: no category, no dialog. Remote run with the **default persona
   only**: category present (the fresh-install honesty leg). Editing
   `persona.md` — and separately `profile.md` — between prepare and send
   refuses with the memory wording (per-tier preimage mutation, the #80
   per-field discipline, truncation bytes included); non-memory drift
   keeps its wording; the runtime **independently re-derives** flag and
   fingerprint from the job and refuses a mismatched decision (the
   strongest wrong-implementation: an orchestrator-only binding);
   end-to-end through `Execute`; a decision claiming the category over
   an empty snapshot with no memory tools selected is refused. A remote
   run with memory tools in `selected_tools` and empty pinned tiers
   still attaches the category (the tool-results honesty leg).
7. **The toggle governs enqueues and survives re-report.** Off: nothing
   pinned, tools absent from the registry (not offered-and-refused), no
   category; on without restart via the notification; **off → worker
   capability re-report → still off; off → restart → on ⇒ works** (the
   one-way-door family, #76's deep legs); a job enqueued before the
   flip keeps its snapshot and consent — asserted, not apologized for.
8. **Snapshot semantics per tier.** Persona and profile edited mid-run:
   running job unchanged, next run sees it.
9. **Version skew fails closed at dispatch** — the literal pre-bump
   number.
10. **Facets refuse in both directions**, runtime identity vs client
    key, tools vs management.
11. **The vault walk is Obsidian-proof.** Files under `.obsidian/` and
    `.trash/` are never indexed nor searchable; a note deleted in
    Obsidian leaves the search index on next reconcile (the
    user-deletion leg the session-deletion test does not cover); the
    4096-file bound refuses legibly.
12. **Policy classes are right.** `memory.search`/`read` arrive `safe`
    and read-only (a failed search returns a tool error, run continues);
    `memory.remember` arrives `approval_required` and is refused on an
    automation allowlist at save.

## Deferred, deliberately (named MEM rungs, not loose ends)

- **Automatic extraction** (MEM-009) with its pre-committed constraint:
  user-authored text only; assistant/tool/recalled content never a
  candidate source.
- **Consolidation automations** (MEM-010) — the scheduler exists; the
  reversibility design does not.
- **Semantic retrieval** (MEM-013); episode summaries; per-session and
  per-turn memory controls (MEM-008); imperative-content linting;
  direct-write trusted areas; auto-re-prepare on autosave mismatch.
- **Pointing Turing at an existing personal vault** — a separate mount
  decision with its own trust story; Phase 1's vault is Turing's brain,
  not the user's notes.

## Documentation the implementation PR must update

- **`docs/architecture/memory-governance.md`** — the promotion-is-
  authorship amendment as a numbered section; the vault named as a
  governed projection with its manifest classifications.
- `docs/architecture/2026-08-18-personal-agent-audit.md` — the substrate
  note on the MEM ladder (005/006/007 reshaped; what remains).
- `docs/architecture/remote-egress-policy.md` — `MEMORY_PROFILE` live:
  both conjuncts (pinned; memory tools selected), the fingerprint beside
  the skills one, the external-agent divergence from recall.
- `docs/VISION.md` — memory joins the untrusted-input invariant's list.
- `CLAUDE.md` — the `/memory` mount.
