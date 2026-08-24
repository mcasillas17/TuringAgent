# Turing's Brain: Vault-Backed Memory Implementation Plan (Phase 1)

Give Turing durable memory the user can open in a text editor. The brain is
an Obsidian-compatible vault of plain Markdown — persona, profile, beliefs,
and proposals as files the user owns, edits, links, and can watch as a graph
— with SQLite underneath holding what files cannot: identity, evidence,
search, and the erasure contract. This plan implements the audit's MEM
ladder rungs 005/006/007 in reshaped form, **under the governance contract
`docs/architecture/memory-governance.md` (MEM-001), which it amends in one
numbered section containing two named relaxations, and otherwise obeys to
the letter** — the audit calls that contract the blocker for every
derived-memory task, and this plan treats it that way.

Prior art (researched 2026-08-23): files-as-memory is mature
(basic-memory's frontmatter + typed wikilinks; obsidian-second-brain's tiny
pinned identity file; Letta's pinned core blocks + searched archival tiers)
— but every one of those systems lets the agent write memory directly, and
the poisoning literature (MINJA's >95% query-only injection, MemoryGraft's
implanted experiences) shows what that costs. This plan takes the sources'
shapes and the papers' defenses — write-time gating, provenance binding,
retrieval-time framing — on machinery Turing already has.

## The problem, stated precisely

Turing has episodic recall (FTS over raw messages, MEM-002) and nothing
else: no persona, no user profile, no curated beliefs, no "remember this."
The egress plumbing already reserves the entire consent surface this needs:
`MEMORY_PROFILE` is a defined category (allowlisted, labelled, mirrored in
Dart — exhaustiveness-forced by `TestEgressCategoryPolicyCoversProtoEnum`),
and `MemoryProfileApplicable` rides the full path — context, signed
challenge, frozen decision (`0014`'s column), enqueue fingerprint, job
proto, runtime validation — **and nothing ever sets it true**. One wrinkle,
load-bearing: the runtime today *hard-refuses* any decision claiming it —
`agent-runtime-go/internal/agent/egress.go` checks
`decision.GetMemoryProfileApplicable()` as a bare must-be-false, unlike
`RecallApplicable` which is recomputed and compared. Turning this on means
rewriting that line into an equality mirror, which means the pinned-memory
snapshot must live on the job proto and be re-derivable by the runtime.
Also: `TestRemoteRunRejectsUnsupportedDisclosureCategories` uses
`MEMORY_PROFILE` as its unsupported example and must be re-pointed at
`ATTACHMENTS` — the last append-site-free value.

Three stakes: memory writes are the most dangerous mutation in the system
(durable state steering every future run, writable by a model that ingests
untrusted content — MEM-009's line is the north star: *"Turing can learn
without silently rewriting its beliefs"*); memory reads are untrusted
prompt material (the invariant VISION applies to skill text); memory
leaving the machine is egress (category, flag, fingerprint — as #80 did
for skills).

## The governance amendment (one section, two named relaxations)

The contract's taxonomy opens the door — it classes *"explicit profile
edits, a deliberate 'remember this' action"* as **user-authored evidence**
that *"may become active memory through an explicit user action"*
(principle 1) — but the door is not already open: three passages currently
foreclose what this plan does, and the amendment must rewrite all three,
not reinterpret around them:

- §Ownership and scope's final paragraph (*"deleting the source withdraws
  the derived fact even though its recall scope was wider"*),
- §Retention (*"Explicit active memory remains until the user supersedes,
  retracts, deletes, or withdraws one of its sources"*),
- the **Active user-controlled memory** taxonomy row (*"an accepted
  candidate with immutable provenance"* — acceptance today preserves
  source dependency rather than reclassifying authorship).

**Relaxation 1 — promotion is authorship.** When the user promotes a
candidate — full content displayed, editable before acceptance — the
promotion is the explicit user action; the item's source becomes that
action, and links to the originating session demote from load-bearing
dependencies to annotations. Consequences, honestly:

- **Machine-owned state obeys the contract unamended.** Candidates and
  evidence annotations are derived state with **database-enforced cascade**
  (their own table, below) plus the external file-cleanup mechanism; a
  failed cleanup fails the withdrawal.
- **User-adopted content survives source deletion** — as a note the user
  typed by hand would — with evidence annotations withdrawn (rows removed
  in the deleting transaction; frontmatter refs rewritten to `withdrawn`
  on the next reconcile) so a belief can never *cite* deleted evidence,
  and the UI shows it as unevidenced. The *content* persists. That is the
  relaxation.

**Relaxation 2 — vault beliefs defer the revision chain.** §Correction
requires immutable revisions and supersession; §Writers requires
authenticated operations for every mutation. A vault the user edits in
Obsidian has neither — file-edit and RPC converge in reconcile, which
emits audit events for detected changes but cannot reconstruct a revision
history the file never carried. Phase 1 defers the belief revision chain
to MEM-010 explicitly (candidates, being machine-owned rows, do get
lifecycle-validated transitions and audit events from day one). Named
here, in the amendment, and in the deferred list — not discovered.

Two more honest boundaries: `memory.remember` writes bounded distillations
(`maxMemoryNoteBytes = 16 KiB`, **over-limit refused legibly, never
truncated** — a truncated memory silently changes meaning), never
transcripts; candidate bodies live in a cascade-owned table so even before
promotion they are never "a second, less-governed copy" (§Boundary is
satisfied by governance, not just by the bound). And the erasure claim
stops at the vault directory — a vault is designed to be synced, and
§Backup already scopes deletion against user-made copies.

The implementation PR updates `docs/architecture/memory-governance.md`
with this amendment as a numbered section touching the three passages.

## The brain's anatomy (locked)

The vault mounts like skills: `cfg.MemoryRoot` (`MEMORY_ROOT`, default
`/memory`, clean-absolute-validated), compose-mounted read/write,
provisioned by `init.sh` in the `provision_skills` mold, gitignored
(`memory/*` + `!memory/.gitkeep` — the profile is personal prose by
design), openable directly in Obsidian.

```
memory/
  persona.md        # who Turing is — USER-authored, agent NEVER writes
  profile.md        # who the user is — USER-authored prose; agent proposes diffs
  inbox/            # agent proposals — candidates, never beliefs
  beliefs/          # promoted notes: people/, projects/, decisions/, ...
```

**Tier 1 — `persona.md`**: user-owned configuration, not memory; pinned
into every run; the agent can never write it (deliberate divergence from
Letta's self-editing persona — drift via accumulated model writes is the
self-reinforcing compromise the literature describes). Shipped as a
commented default by `init.sh`. The only instruction-bearing memory file,
permitted because a human is its sole author.

**Tier 2 — `profile.md`**: user-authored prose about the user, pinned
beside the persona, carrying **no sidecar evidence and no per-statement
provenance**. The agent contributes by writing a *proposed edit* into the
inbox; applying it is authorship under Relaxation 1. Unapplied proposals
die with their source; applied text is the user's.

**Tier 3 — `beliefs/`**: the wiki-linked note graph — frontmatter, short
observation bullets, typed `[[wikilinks]]` (basic-memory's conventions,
which are what make Obsidian's graph and the 3D plugins render the brain
for free). Never pinned; reached through `memory.search`/`memory.read`,
returned as framed evidence.

**Tier 4 — `inbox/`**: thoughts not yet believed — every agent-originated
write lands here and nowhere else; its own colored region in the graph;
promotion physically moves the file between regions.

**Pinned-file failure posture, stated:** a missing, unreadable,
over-limit, or symlinked `persona.md`/`profile.md` is treated as **empty
for pinning** (no silent partial load), surfaces as a visible
unavailable/parse-error row on the Memory page, and — because the pinned
snapshot is empty — attaches no `MEMORY_PROFILE` claim it cannot honor.
The negative leg is tested; silence is the one behavior forbidden.

**Episodic memory stays where it is** (FTS recall, MEM-002). Episode
summaries are Phase 2.

## Design decisions (locked)

**Candidates never become beliefs by the agent's hand.**
`memory.remember` writes **only** into `inbox/`, whatever its policy. The
`memoryfiles` write layer is a named set of purpose-scoped primitives —
`createInboxNote` (confined to `inbox/`, refuses any other target *when
called directly, below the tool layer*), `promoteToBeliefs`,
`applyProfileEdit`, `rewriteFrontmatterRefs` — each carrying its own
confinement, so no single generic `writeConfined(root, rel)` with the
check hoisted into a handler can satisfy the tests. The server names
inbox files (ULID + sanitized slug of a model-supplied title — the model
never controls a path). `memory.remember` deliberately stays
**downgradeable** to `safe` by the by-name policy RPC — unlike
`files.create`, whose seed feeds `BundledToolRequiresApproval` — because
the invariant is the confinement, not the friction; stated so a reviewer
reads intent, not oversight. Promotion is a user action via RPC or file
move; both converge in reconcile. Direct agent-writes to `beliefs/` wait
for MEM-010.

**Note identity is a frontmatter ULID, not a path.** Assigned by
`memory.remember`, or by reconcile for user-created notes. The sidecar
keys on id; renames re-link by id (content hash as fallback). **Duplicate
ids** — Obsidian's duplicate-note command copies frontmatter verbatim —
are surfaced as per-note error rows (both copies flagged, neither indexed)
until the user resolves; deterministic and safe beats guessing.
Frontmatter parsing is **lenient** (unknown fields preserved-and-ignored;
the user is invited to annotate) and malformed frontmatter is a per-note
parse-error row: fail-soft, visible, never fatal.

**The vault walk is Obsidian-aware, symlink-refusing, and bounded.** The
scan indexes `*.md` only; **refuses symlinks at every component and every
entry (`Lstat`, never `Stat` — the skillfiles posture), including inside
`beliefs/`**, so a planted link cannot pull out-of-vault content into the
index; skips every dot-directory (`.obsidian/` holds plugin JavaScript,
`.trash/` holds soft-deletes), `*.canvas`, and sync `conflicted copy`
artifacts; and enforces `maxVaultIndexedFiles = 4096` with a legible
refusal that **degrades search and reconcile only — pinned tiers and
enqueue are never blocked by vault size**. The scan runs outside any
write transaction (transaction budget stated per MEM-009/010's own
discipline) with an (mtime, size) cache; a same-second, same-length edit
may serve stale index text until the next change — a named residual, not
a surprise.

**Path confinement: reimplement `safe_fs.go`'s opener, don't invent.**
The one in-repo model that already does confined *writes* is
`turing-backend/mcp-files/internal/tools/safe_fs.go` — root resolved
once, then a descriptor-relative `openat` walk with
`O_CLOEXEC|O_NOFOLLOW` ending in `O_CREAT|O_EXCL` for creates — but it
lives in a separate Go module the orchestrator cannot import.
`memoryfiles` reimplements that opener (cited as the model, including
its `processPathLocks` per-path locking and `syncFile`/`syncDirectory`
fsync discipline) under `orchestrator-go/internal/memoryfiles`.
**Profile updates are fd-verified, not rename-replaced**: open the target
`O_NOFOLLOW|O_RDWR` under the path lock, verify the content hash *via
the fd*, write through the same fd, fsync file and parent — because
read-hash-then-rename is itself the TOCTOU it claims to close, and
rename swaps the inode out from under an Obsidian editor holding the
file open. On mismatch: refuse with "the file changed — re-read and
retry", the same legible surface as the egress drift wording.

**Search has a real substrate — two tables, because the schema manifest
classifies per table, not per row:**
- **`memory_candidates`** (`cascade_owned`, `sourceTable: sessions`,
  `NOT NULL source_session_id … ON DELETE CASCADE`): inbox candidates —
  id, path, content hash, body, evidence refs, lifecycle state
  (validated transitions + audit events from day one). The
  database-enforced half of withdrawal, exactly as §Correction demands.
  No FTS — candidates are inactive and unsearchable by construction.
- **`memory_notes`** (`independent`, with the amendment cited as its
  manifest rationale): promoted beliefs — id, path, content hash,
  status, timestamps, and a **`content` column** (the body projection —
  named exactly `content`, because `validateExternalContentFTSProjection`
  hardcodes that column name in its expected delete command).
- **`memory_notes_fts`** (FTS5 external-content over `memory_notes`) with
  **all three triggers** (`_ai`, `_ad`, `_au` — the `messages_fts`
  precedent; the guard checks only the delete trigger, so a
  delete-only implementation passes the invariant while never indexing:
  the *edit-becomes-searchable* test leg exists because of that gap),
  and the `ftsProjectionDeleteChecks` entry.
- **`memory_evidence`** (`cascade_owned`, double FK): annotation rows for
  promoted beliefs, keyed to `memory_notes.id` and to sessions with
  `ON DELETE CASCADE`.
- Promotion is: file move (confined primitive), then one transaction —
  insert `memory_notes`, copy annotations to `memory_evidence`, delete
  the `memory_candidates` row. A crash between file move and transaction
  leaves a belief file without rows; reconcile heals it (creates the
  note row, re-links evidence from frontmatter) — the direction of
  authority below makes this safe.

**Direction of authority, per field:** files win for **content**; the
database wins for **evidence state**. A reconcile after a crash must not
resurrect withdrawn evidence from stale frontmatter — withdrawal marks
live in the sidecar and frontmatter is rewritten *from* the sidecar,
never the reverse. Reconcile's file-writing half (id assignment,
`withdrawn` rewrites) runs only on RPC-driven paths and startup, under a
vault-wide singleflight; **`memory.search`'s pre-query reconcile is
read-only** (index refresh, no file writes) — a `safe`, read-only tool
must not write files, and two concurrent searches must not race
frontmatter rewrites under `-race`.

**Erasure follows the artifact pattern — with the real budget, which is
five sites, not one.** The existing mechanism is sandbox-specific
end-to-end, so joining it means: (1) a `vault_artifacts` manifest (or a
`kind` discriminator with its own manifest classification) reserved in
the deleting transaction — the real symbol to mirror is
`Repository.ReserveSandboxArtifact`, whose path validation is
sandbox-shaped and does not transfer; (2) a second pending-count arm in
`AdvanceSessionDeletion`'s hardwired
`SELECT COUNT(*) FROM sandbox_artifacts …` gate, or the session reports
deleted while vault cleanup is pending; (3) failure scoping —
`MarkSessionDeletionExternalFailure` currently flips *every* sandbox
artifact row, and must not mark sandbox rows failed because the vault
cleaner failed; (4) the removal loop beside
`removeOwnedArtifactManifestRows`; (5) `SetArtifactCleaner` becoming a
cleaner list, with the retry contract stated: `session_deletions` carries
one `error_code`, so it reports the first failing cleaner, and retry
re-runs **all** cleaners, each of which must be idempotent. Test 5 gets
the partial-failure leg (one cleaner succeeds, one fails, retry
completes) because that two-cleaner interaction is the new mechanism.

**Reads are evidence, not authority — mechanically.** Search and read
results return through the per-call nonce framing — extracted first into
`turing-backend/internal/egress` as a parameterized helper (the current
`frameIntegrationResult` is unexported with hardcoded wording; both
callers move to the shared one). Bounds: `maxMemoryResultBytes = 16 KiB`,
rune-safe, truncation announced. `memory.search` has **no
model-controllable scope parameter**; default and only scope is
`beliefs/`. `memory.read` targets belief ids only. Neither can touch
`inbox/`, `persona.md`, or `profile.md` — the pinned tiers are readable
only as their fingerprinted, truncated pinned selves.

**Tool policies, with the honest rationale:** `memory.search` and
`memory.read` are seeded `safe` (local reads of local files — the
`files.read` class; leaving them `approval_required` makes the brain a
nag) and join `readOnlyTools` — which matters **only when the user
raises the policy to `approval_required`** (under `safe`/`DECISION_ALLOW`
the runner's `sideEffecting` is already false; the `read_only` bit is
what keeps a *raised* policy from turning a failed search into
`SideEffectUnknownError`, and the test sets the raised policy so the
entry is load-bearing, not decorative). `memory.remember` takes the
unknown-tool fallback. **The whole `memory` server is refused on
automation allowlists at save** — the integrations shape exactly (a
whole-server check in `normalizeAllowedTools`, its own sentinel, gRPC
mapping, and client string): an unattended run that searches the belief
graph on a remote-provider automation would be memory egress with no
human at prepare time, and an unattended `remember` is the hole through
which non-user-authored text becomes candidates before MEM-009's
extraction gate exists.

**Pinned tiers: budgets, snapshot, ladder seat, binding.**
`maxPersonaBytes = 4096`, `maxProfileBytes = 4096`, rune-safe truncation
with an in-context notice. Snapshotted at enqueue **in the same read
that computes the fingerprint**, and — the skills precedent at its
strongest — **recomputed inside the enqueue transaction** against the
frozen decision (`ErrEgressMemorySnapshotChanged`, a wrapped sentinel
beside the skills one, mapped in `mapEnqueueError`): without the
in-enqueue recompute, an Obsidian autosave between consent and enqueue
ships a job the runtime will hard-reject at `Execute` instead of a
legible re-prepare. Ladder seat: the pinned pair is an **upstream
pre-decision like the skill index** — built before `buildBudgetedContext`
in the `buildSkillMessagesWithinContext` mold, passed through as a
`MemoryOmitted` field on `contextOmissions` with its notice string,
`Omissions.Notice()` arm, and the emit-payload key beside the others —
not an in-ladder step, because the ladder never drops the skill index
either and the plan should not invent a placement the code contradicts.
Over budget truncates with notice; over context omits with notice;
silence is forbidden in both directions.

**Memory egress joins the consent — category, flag, fingerprint, both
conjuncts.** `MemoryProfileApplicable` = `providerEgress && (pinned
snapshot non-empty || memory tools in selected_tools)` — the second
conjunct closes the honesty gap where the belief graph leaves as generic
"Tool results" while the memory disclosure stays silent; both conjuncts
are runtime-re-derivable (`selected_tools` is an exact frozen set on the
job). The default `persona.md` counts as pinned content, so a fresh
install discloses honestly. All mirrors move in both modules, including
the rewritten truthiness gate as an equality mirror.
`memory_snapshot_fingerprint` (preimage: post-truncation pinned bytes of
both tiers, notices included — a shared no-`omitempty` struct in
`backendegress` beside the skills one; plain string compare in
`payloadMatchesEgressContext`) joins the signed payload, the enqueue
fingerprint (version bump — the lockstep pair with the #80 fixture
sweep, hardcoded-JSON exception included), and the frozen decision: the
**third** `run_egress_decisions` rename-copy-drop rebuild (0014 created;
0016 and 0017 rebuilt), fresh temp name, cascade FK preserved per the
schema pin, index recreated, new proto field at the next free number,
`proto_contract_test.go` pinned. Divergence stated: `RecallApplicable`
excludes external-agent runs; memory (like skills) rides to them —
adjacent lines, deliberate. Drift wording is memory-specific; the
Obsidian-autosave UX is named (client suggests closing the memory editor
and re-preparing; auto-re-prepare deferred).

**The toggle governs new enqueues AND refuses at dispatch.** Stored in
the existing `settings` table. Off: nothing pins at enqueue; the dynamic
lister (`ListMemoryTools`, internal facet, runtime-identity entry, the
`ListIntegrationTools` wiring shape — a static skills-style lister
structurally cannot deliver absence) returns empty; **and the memory
service refuses tool calls at dispatch when off** — because the `tools`
rows persist with `enabled = 1` after a toggle-off (UpsertTools never
lowers them), so registry absence alone leaves a direct-dispatch hole.
Registry-change notification fires on toggle; no restart. Queued and
in-flight jobs keep their snapshots and consented decisions —
consent-at-prepare semantics, asserted, not apologized for.

**The pseudo-server list, audited against post-#81 code:**
`filterRegisteredWorkerTools`' policy-only availability; `UpsertTools`'
pseudo branch; `BundledServerForTool` gains `memory.`;
`mcpregistry/import.go` reserved names AND the second copy in
`mcpregistry/management.go`; `ListPseudoServerTools`' RPC-level allowlist
gains a `memory` arm; the live triggers are **0017's** (drop/recreate
from 0017's text or silently lose the `integrations` carve-out);
`readOnlyTools`. Explicitly not on the list: `beaconServerName` —
`memory.*` splits correctly on the first-dot fallback; there is no map.

**What does NOT go in memory: instructions.** Procedures live behind the
skill gate. The frame declares memory non-authoritative; the persona is
the sole instruction channel; the promotion UI shows full content.
Imperative-content linting is Phase 2.

**The visualization is earned, not built.** Folders → clusters, typed
wikilinks → edges, persona/profile → hub stars, inbox → the "not yet
believed" region, promotion → a star migrating between regions.
Obsidian's graph today; Agentage Galaxy in 3D; agentcairn-style
provenance coloring later.

## What gets built (Phase 1)

- **Migration (`0018` as of this writing; both `migrations_test.go` pins
  move)** — triggers from 0017's text + `memory`; `memory_candidates`
  (cascade_owned), `memory_notes` (independent, `content` column),
  `memory_notes_fts` (three triggers), `memory_evidence` (double
  cascade); `vault_artifacts` (or kind column) with classification; the
  `run_egress_decisions` rebuild; every table classified in
  `schema_invariants_test.go` in the same change.
- **`internal/memoryfiles`** — vault layout, lenient frontmatter, ULID
  identity + duplicate handling, Obsidian-aware symlink-refusing bounded
  walk with (mtime,size) cache, the primitive set
  (`createInboxNote`/`promoteToBeliefs`/`applyProfileEdit`/
  `rewriteFrontmatterRefs`) reimplementing `safe_fs.go`'s opener with
  per-path locks and fsync discipline; fd-verified profile CAS.
- **`turing-backend/internal/egress`** — the extracted parameterized
  framing helper (integrations moves onto it); the memory preimage
  struct + fingerprint; the version-bump pair.
- **`internal/repository`** — candidates/notes/evidence CRUD +
  reconcile (read-only vs file-writing halves, singleflight); enqueue
  snapshot + in-transaction fingerprint recompute +
  `ErrEgressMemorySnapshotChanged`; vault-artifact reservation; the
  whole-server automation refusal (sentinel + mapping); the settings
  toggle.
- **`internal/service/sessions` + `session_delete.go` + `app.go`** — the
  cleaner list; the second pending-count arm; scoped failure marking;
  the second removal loop; idempotent-retry contract.
- **`internal/service/memory`** (new) — the three tools (toggle-checked
  at dispatch), promotion/rejection/profile-apply RPCs, `ListMemoryTools`
  (internal), toggle RPCs; public/internal facet split, both refusal
  directions.
- **Egress (both modules)** — both conjuncts with all mirrors including
  the rewritten truthiness gate; legible memory drift wording;
  `TestRemoteRunRejectsUnsupportedDisclosureCategories` → `ATTACHMENTS`.
- **Runtime** — pinned injection as an upstream pre-decision with the
  `MemoryOmitted` omission field, notice, and emit key; dynamic lister
  client wiring; framed results; `readOnlyTools` honored.
- **`proto/turing/v1`** — memory service; job snapshot fields; decision
  + disclosure fields; runtime-identity names; pinned toolchain,
  contract test.
- **Client** — Memory page (tiers, inbox review with full content,
  persona/profile editors with autosave-mismatch UX, toggle, provenance
  incl. withdrawn and parse-error rows); egress dialog memory line.
- **`scripts/init.sh` + compose + `.gitignore` + `CLAUDE.md`** — the
  `/memory` mount, default `persona.md`, `memory/*` gitignore +
  `.gitkeep`, the doc line.

## The tests that gate the merge

Break each production gate, watch the right test fail, restore.

1. **The agent cannot write beliefs — and cannot read outside them.**
   `memory.remember` targeting `persona.md`/`profile.md`/`beliefs/…` by
   name, `../`, absolute path, or a symlink planted inside `inbox/` —
   refused; `safe` policy changes friction, not confinement; **the
   `createInboxNote` primitive itself refuses a `beliefs/` target when
   called directly, below the tool layer** (the leg that kills a
   handler-hoisted check). Same suite for `memory.read`: `../`, absolute
   paths, `/skills`, the database file, symlinks. Over-limit
   `memory.remember` is **refused legibly, not truncated**.
2. **Candidates are never active.** Pinned loading reads only
   persona+profile; search and read cannot reach `inbox/` by any
   argument (no scope parameter exists); an inbox note influences
   nothing until promoted; `memory_candidates` rows are unsearchable.
3. **Promotion converges and survives Obsidian.** RPC-promotion and
   file-move promotion produce identical state; a rename inside
   `beliefs/` keeps id, provenance, and index; a crash between file
   move and transaction heals on reconcile; the profile CAS refuses on
   fd-verified hash mismatch with the re-read wording, and a concurrent
   Obsidian edit racing the apply loses no user text.
4. **Budgets and framing hold, on both tools.** Pinned over-budget
   truncates rune-safe with notices; search AND read results are
   nonce-framed (distinct per call), bounded, spoof-resistant,
   valid-UTF-8 at a computed mid-rune boundary.
5. **Erasure cascades by tier and survives failure.** Delete a session:
   evidence and candidate rows gone in the deleting transaction; candidate
   files removed via the cleaner; **one cleaner succeeding while the
   other fails** yields `failed_external` + retryable with sandbox rows
   unmarked by the vault failure, the session not reported deleted, and
   a retry (re-running both, idempotent) completing; promoted beliefs
   survive with evidence withdrawn in sidecar and, on next reconcile,
   frontmatter; **a reconcile after a crash cannot resurrect withdrawn
   evidence from stale frontmatter** (sidecar wins for evidence state).
6. **The egress binding is real, two-sided, per-tier, and
   failure-honest.** Local-only: no category. Remote with default
   persona only: category present. Editing persona — and separately
   profile — between prepare and send refuses with memory wording
   (per-tier preimage mutation, truncation bytes included); non-memory
   drift keeps its wording; the **enqueue-transaction recompute** refuses
   a post-consent change with the wrapped sentinel mapped to a
   distinguishable status; the runtime independently re-derives flag and
   fingerprint from the job and refuses mismatches, end to end through
   `Execute`; a decision claiming the category over an empty snapshot
   with no memory tools selected is refused; memory tools selected with
   empty pinned tiers still attaches the category; **a symlinked or
   unreadable persona pins nothing, attaches no category, and surfaces
   the visible unavailable row** (the negative honesty leg).
7. **The toggle governs enqueues, refuses at dispatch, and survives
   re-report.** Off: nothing pinned, tools absent from the registry,
   **a direct `CallMemoryTool`-path dispatch with the toggle off is
   refused** (the persisted-`tools`-rows hole); on without restart via
   the notification; off → capability re-report → still off; off →
   restart → on ⇒ works; a job enqueued before the flip keeps snapshot
   and consent.
8. **Snapshot semantics per tier** — mid-run edits change the next run.
9. **Version skew fails closed at dispatch** — the literal pre-bump
   number.
10. **Facets refuse in both directions.**
11. **The vault walk is Obsidian-proof.** `.obsidian/`/`.trash/` never
    indexed; **a symlink inside `beliefs/` pointing outside the vault is
    never ingested** (walk-level `Lstat` leg); a note deleted in
    Obsidian leaves the index on next reconcile; **an edited note's new
    text becomes searchable** (the insert/update-trigger leg the schema
    guard cannot check); the 4096 bound refuses legibly and blocks
    neither pinned tiers nor enqueue.
12. **Policy classes are right, tested at the policy that matters.**
    `memory.search`/`read` arrive `safe`; **raised to
    `approval_required`, a failed search is still a recoverable tool
    error** (the leg that makes the `readOnlyTools` entry load-bearing)
    and `ToolPolicyDecision.ReadOnly` is asserted on the wire;
    `memory.remember` arrives `approval_required`; the whole `memory`
    server is refused on an automation allowlist at save.

## Deferred, deliberately (named MEM rungs, not loose ends)

- **Automatic extraction** (MEM-009): user-authored text only;
  assistant/tool/recalled content never a candidate source.
- **Belief revision chains, supersession, lifecycle-validated vault
  edits** (MEM-010 — Relaxation 2's other half).
- **Consolidation automations** (MEM-010); semantic retrieval (MEM-013);
  episode summaries; per-session/per-turn controls (MEM-008);
  imperative-content linting; direct-write trusted areas;
  auto-re-prepare on autosave mismatch.
- **Pointing Turing at an existing personal vault** — a separate mount
  and trust decision; Phase 1's vault is Turing's brain, not the user's
  notes.

## Documentation the implementation PR must update

- **`docs/architecture/memory-governance.md`** — the amendment section:
  both relaxations, all three foreclosing passages rewritten, the vault
  named as a governed projection with its manifest classifications.
- `docs/architecture/2026-08-18-personal-agent-audit.md` — the substrate
  note (005/006/007 reshaped; what remains).
- `docs/architecture/remote-egress-policy.md` — `MEMORY_PROFILE` live:
  both conjuncts, the fingerprint beside the skills one, the
  external-agent divergence from recall.
- `docs/VISION.md` — memory joins the untrusted-input invariant's list.
- `CLAUDE.md` — the `/memory` mount.
