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
(principle 1) — but the door is not already open: five passages currently
foreclose what this plan does, and the amendment must rewrite all five,
not reinterpret around them:

- §Ownership and scope's final paragraph (*"deleting the source withdraws
  the derived fact even though its recall scope was wider"*),
- §Retention (*"Explicit active memory remains until the user supersedes,
  retracts, deletes, or withdraws one of its sources"*),
- the **Active user-controlled memory** taxonomy row (*"an accepted
  candidate with immutable provenance"* — acceptance today preserves
  source dependency rather than reclassifying authorship),
- §Correction's withdrawal sentence itself (*"every fact, candidate,
  revision, … derived from that source is removed"* — a promoted belief
  is a "fact"; Relaxation 1's authorship reclassification neutralizes it
  exactly as it neutralizes §Retention, and under this plan's own
  rewrite-don't-reinterpret posture it gets rewritten, not argued past),
- §Egress (*"memory/profile … is not currently applicable or sent"* —
  false the day this ships — and *"only selected items with provenance
  and sensitivity filtering may be included"*: Phase 1 pins the tiers
  wholesale under per-run consent; the amendment names the pinned tiers
  as user-authored configuration the user consents to per run, and
  **defers sensitivity filtering explicitly** rather than claiming it).

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
  by a reconcile the deletion flow itself triggers as its vault cleaner
  completes — not left waiting for the next restart) so a belief never
  *cites* deleted evidence for longer than the deletion takes, and the
  UI shows it as unevidenced. The *content* persists. That is the
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
with this amendment as a numbered section touching the five passages.

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
inbox — a `memory_candidates` row like any other, distinguished by a
`kind` discriminator (`belief` | `profile_edit`), so profile proposals get
the same cascade, lifecycle validation, and erasure tracking as belief
candidates; `applyProfileEdit` consumes a `profile_edit` candidate.
Unapplied proposals die with their source; applied text is the user's.
A **session-less file in `inbox/`** — hand-dropped or duplicated there by
the user in Obsidian — can have no candidate row (`NOT NULL` session FK)
and is listed as an *unmanaged draft* with a visible row: promotable by
file move only (there is no RPC over what the RPC never created), outside
the erasure contract by construction because it is user-authored.

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
`applyProfileEdit`, `rewriteFrontmatterRefs`, `removeInboxNote` — each carrying its own
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
parse-error row: fail-soft, visible, never fatal. Because
`rewriteFrontmatterRefs` is the one primitive that *writes* frontmatter,
preservation is a write-path property too: the rewrite is a **node-level
YAML edit (or byte-level splice of the refs block)** — never a struct
decode/re-encode, which satisfies "preserved" on read and destroys
annotations, key order, and formatting on write. The only in-repo parser
(`skillfiles`' strict `KnownFields`) is the opposite posture; this is
new code, and the round-trip is tested.

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
`turing-backend/mcp-files/internal/tools` (`safe_fs.go` for the opener
and `processPathLocks`; `files.go` for `syncFile`/`syncDirectory` and
the link-staging create) — root resolved
once, then a descriptor-relative `openat` walk with
`O_CLOEXEC|O_NOFOLLOW` ending in `O_CREAT|O_EXCL` for creates — but it
lives in a separate Go module the orchestrator cannot import.
`memoryfiles` reimplements that opener (cited as the model, including
its `processPathLocks` per-path locking, `syncFile`/`syncDirectory`
fsync discipline, and — load-bearing for a directory Obsidian is
actively watching — its **link-staging create**: `O_CREAT|O_EXCL` opens
a random staging name, and the target name is installed by `Linkat`,
whose `EEXIST` is the real exclusivity guarantee, so a partially
written note is never visible at its final path) under
`orchestrator-go/internal/memoryfiles`.
**Profile updates are fd-verified, not rename-replaced**: open the target
`O_NOFOLLOW|O_RDWR` under the path lock, verify the content hash *via
the fd*, write through the same fd, fsync file and parent — because
read-hash-then-rename is itself the TOCTOU it claims to close, and
rename swaps the inode out from under an Obsidian editor holding the
file open. On mismatch: refuse with "the file changed — re-read and
retry", the same legible surface as the egress drift wording. Named
residual: the fd-in-place write is deliberately non-atomic (inode
stability for an open editor is the point), so a crash or concurrent
read mid-write can observe a torn `profile.md` — the pinned-file
failure posture's parse-error row is the recovery surface.

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
- `memory_notes` keeps its **implicit rowid** — a ULID text primary key
  invites `WITHOUT ROWID`, which breaks external-content FTS outright
  (the guard also hardcodes `content_rowid='rowid'`); and the
  `ftsProjectionDeleteChecks` entry is a **hand-written probe function**
  in the `validateMessagesFTSDeleteBehavior` mold, not just a map entry.
- **`memory_notes_fts`** (FTS5 external-content over `memory_notes`) with
  **all three triggers** (`_ai`, `_ad`, `_au` — the `messages_fts`
  precedent; the guard's probe inserts-then-matches, so a missing insert trigger
  already fails it — the genuinely uncaught gap is the **update**
  trigger (stale index on edit), which is why the
  *edit-becomes-searchable* test leg exists),
  and the `ftsProjectionDeleteChecks` entry.
- **`memory_evidence`** (`cascade_owned`, double FK): annotation rows for
  promoted beliefs, keyed to `memory_notes.id` and to sessions with
  `ON DELETE CASCADE`.
- Promotion is: file move (confined primitive), then one transaction —
  insert `memory_notes`, copy annotations to `memory_evidence`, delete
  the `memory_candidates` row, **and release the `vault_artifacts`
  reservation** (likewise when `applyProfileEdit` consumes a
  `profile_edit` candidate): a surviving reservation would hand the
  session-deletion cleaner a promoted belief — the exact content
  Relaxation 1 protects. Crash-heal reconcile clears reservations whose
  file no longer sits under `inbox/`. A crash between file move and transaction
  leaves a belief file without rows; reconcile heals it (creates the
  note row, re-links evidence from frontmatter, and **removes the
  orphaned candidate row whose inbox file no longer exists** — no
  phantom entries in inbox review) — the direction of authority below
  makes this safe.

**Direction of authority, per field:** files win for **content**; the
database wins for **evidence state**. A reconcile after a crash must not
resurrect withdrawn evidence from stale frontmatter — withdrawal marks
live in the sidecar and frontmatter is rewritten *from* the sidecar,
never the reverse. The heal trigger is named: a belief
file **without a `memory_notes` row** is what reconcile heals — that
discriminator is what lets "files win for content, sidecar wins for
evidence state" and "crash-heal re-links from frontmatter" coexist. And
heal is withdrawal-aware: a frontmatter ref naming a session that no
longer exists (deleted before the heal ran — the candidate row cascaded
away) is written as `withdrawn`, never re-inserted, because
`memory_evidence`'s `NOT NULL` session FK would refuse it and the heal
must not fail permanently over evidence the contract says is gone.
Reconcile's file-writing half (id assignment,
`withdrawn` rewrites) runs on RPC-driven paths, startup, and the
deletion flow's cleaner-completion trigger, under a vault-wide
singleflight; **`memory.search`'s pre-query reconcile is
read-only** (index refresh, no file writes) — a `safe`, read-only tool
must not write files, and two concurrent searches must not race
frontmatter rewrites under `-race`; the read half's index writes and
(mtime, size) cache are mutex-guarded/singleflighted too, since CI runs
the race detector over everything.

**Erasure follows the artifact pattern — with the real budget, which is
six sites, not one — and the reservation ordering that makes it
complete.** `vault_artifacts` rows are reserved **before the file is
written**, at candidate creation — the mold is
`reserveArtifactForConsume`, whose own comment states why: reservation
is the last point where withdrawal can be a refusal instead of an
orphaned file. (`Repository.ReserveSandboxArtifact`'s path validation is
sandbox-shaped and does not transfer; the ordering does.) A crash
between reserve and write leaves a row with no file, which the
idempotent cleaner tolerates — tested; the reverse order would leave a
model distillation the cleaner never learns about while the deletion
reports complete. The six sites: (1) the manifest (its own schema
classification); (2) a second pending-count arm in
`AdvanceSessionDeletion`'s hardwired
`SELECT COUNT(*) FROM sandbox_artifacts …` gate; (3) failure scoping — `MarkSessionDeletionExternalFailure` marks
`session_deletions` AND bulk-flips every owned **sandbox** artifact row
to `delete_failed` with a per-artifact audit row each; reused as-is for
a vault-cleaner failure it would mark sandbox rows failed and emit
false audit rows for files the failure never touched, so the vault
cleaner gets its **own scoped failure marking against
`vault_artifacts`** (the receipt still carries one `error_code`, so
per-cleaner attribution is by message), and the removal loop is
per-cleaner-scoped; (4) the removal loop beside
`removeOwnedArtifactManifestRows`; (5) `SetArtifactCleaner` becoming a
cleaner list whose pass is **continue-on-error — every cleaner is
attempted independently within a pass, never short-circuited on the
first failure** (otherwise the partial-failure outcome is an accident
of registration order, and the second cleaner silently never runs); (6) **the cleaner-dispatch gate itself** —
`DeleteSession`'s retry path invokes cleaners only when
`receipt.ErrorCode == "artifact_cleanup_pending"`, a single literal the
second pending arm must also satisfy or vault-only pending states loop
on the reconcile ticker forever. Retry contract stated honestly:
`error_code` is transient bookkeeping (the pending gate overwrites a
failure code on the next advance); retry re-runs **all** cleaners, each
idempotent. Test 5 gets
the partial-failure leg (one cleaner succeeds, one fails, retry
completes) because that two-cleaner interaction is the new mechanism.

**Reads are evidence, not authority — mechanically.** Search and read
results return through the per-call nonce framing — extracted first into
`turing-backend/internal/egress` as a parameterized helper (the current
`frameIntegrationResult` is unexported with hardcoded wording and one
production caller, which moves to the shared one). Bounds: `maxMemoryResultBytes = 16 KiB`,
rune-safe, truncation announced. `memory.search` has **no
model-controllable scope parameter**; default and only scope is
`beliefs/`. `memory.read` targets belief ids only and serves the **file**, freshly
read through the confined opener — the (mtime, size) staleness residual
applies to search's projection, never to read. Neither can touch
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
unknown-tool fallback. **Unattended runs cannot touch memory, enforced
at dispatch, not at the allowlist** — because the allowlist is consulted
only for `approval_required` tools (`GetAutomationRunGrant` sits inside
that policy branch; the picker's own comment says safe tools never
appear), a save-time refusal cannot stop a `safe`-seeded search or a
downgraded `remember`: the memory service **refuses all three tools on
automation runs regardless of policy**, in the same dispatch check that
enforces the toggle. The save-time allowlist refusal exists too (a
whole-server check in `normalizeAllowedTools`, its own sentinel and gRPC
mapping; client-side, *editing* the existing integrations sentence above
the picker) — but as legibility and pre-grant prevention, not as the
enforcement, and the plan says which is which. Rationale stated with its
own moot point: a remote-provider automation cannot even enqueue today
(`ErrRemoteEgressConsentRequired` with no decision), so the live risk is
the unattended local `remember` — the hole through which
non-user-authored text becomes candidates before MEM-009's extraction
gate exists — and unattended belief reads feeding later egress.

**Pinned tiers: budgets, snapshot, ladder seat, binding.**
`maxPersonaBytes = 4096`, `maxProfileBytes = 4096`, rune-safe truncation
with an in-context notice; "non-empty" is measured on the
whitespace-trimmed post-truncation bytes, so a whitespace-only file
attaches no category. Snapshotted at enqueue **in the same read
that computes the fingerprint**, and — the skills precedent at its
strongest — **recomputed inside the enqueue transaction** against the
frozen decision (`ErrEgressMemorySnapshotChanged`, a wrapped sentinel
beside the skills one, mapped in `mapEnqueueError` — distinguishable by
message text within `FailedPrecondition`, exactly as the skills sentinel
is): without the
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
fingerprint (a bump touching **three** version sites, not the #80 pair
alone: `backendegress.DecisionVersion` and `egressChallengeVersion` —
the lockstep pair with the #80 fixture sweep, hardcoded-JSON exception
included — **plus the bare inline `version` local in
`enqueueRequestFingerprint`**, `jobs.go`, last moved in #76, `5 → 6` as
`MemorySnapshotFingerprint` joins the `egressFingerprint` struct;
missing that third literal is not cosmetic — the enqueue fingerprint
distinguishes "same idempotency key, different request", so a changed
persona under a reused key would hash identically and silently replay
the earlier job with the earlier snapshot), and the frozen decision: the
**third** `run_egress_decisions` rename-copy-drop rebuild (0014 created;
0016 and 0017 rebuilt), fresh temp name, cascade FK preserved (enforced
behaviorally by the `cascade_owned` classification — there is no
column-shape pin), and `idx_run_egress_decisions_provider_created`
recreated as a **manual obligation**: no test asserts that index, so
dropping it would be silent; new proto field at the next free number,
`proto_contract_test.go` pinned. Divergence stated: `RecallApplicable`
excludes external-agent runs; memory (like skills) rides to them —
adjacent lines, deliberate. Drift wording is memory-specific; the
Obsidian-autosave UX is named (client suggests closing the memory editor
and re-preparing; auto-re-prepare deferred).

**The toggle governs new enqueues AND refuses at dispatch.** Stored in
the `settings` table — a new key beside its single existing occupant
(`tool_registry_initialized`); a fine choice, not an established mold.
Off: nothing pins at enqueue; the dynamic lister (`ListMemoryTools`,
internal facet, runtime-identity entry, the `ListIntegrationTools`
wiring shape — a static skills-style lister structurally cannot deliver
absence) returns empty; **and the memory service refuses tool calls at
dispatch when off** — not because rows stay enabled (they don't:
`UpsertTools`' zero-then-restore lowers un-reported pseudo rows), but
because `PseudoServerToolAvailable` is deliberately policy-only and a
direct tool-RPC call bypasses registry *availability* state, as
`CallIntegrationTool` does (which still re-checks the policy column —
the toggle is a settings key orthogonal to policy, hence its own
check).
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
the sole *unframed* channel. `profile.md` pins **framed** — wrapped as
user-authored context, not instructions — because the agent can reach it
through one accepted proposal; the honest invariant is therefore: the
agent can never author unframed pinned text (persona), and can author
framed pinned text only through one explicit user acceptance with full
content shown. Imperative-content linting is Phase 2.

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
  cascade); `vault_artifacts` (a separate table — a kind column on
  `sandbox_artifacts` would collapse the six-site budget and inherit
  sandbox-shaped validation) with classification; the
  `run_egress_decisions` rebuild; every table classified in
  `schema_invariants_test.go` in the same change.
- **`internal/memoryfiles`** — vault layout, lenient frontmatter, ULID
  identity + duplicate handling, Obsidian-aware symlink-refusing bounded
  walk with (mtime,size) cache, the primitive set
  (`createInboxNote`/`promoteToBeliefs`/`applyProfileEdit`/
  `rewriteFrontmatterRefs`/**`removeInboxNote`** — the rejection RPC
  and the vault cleaner both delete files, and both delete **through
  this primitive**, which refuses any target outside `inbox/`: a
  cleaner deleting by manifest-row path with no confinement is exactly
  how a stale reservation would reach a promoted belief, and the
  primitive makes that impossible by construction) reimplementing the
  mcp-files opener (`safe_fs.go` for the walk and `processPathLocks`;
  `files.go` for `syncFile`/`syncDirectory` and the link-staging
  create) with per-path locks and fsync discipline; fd-verified
  profile CAS.
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
- **`scripts/init.sh` + compose + `.gitignore` + `.dockerignore` +
  `CLAUDE.md`** — the `/memory` mount, default `persona.md`, `memory/*`
  gitignore + `.gitkeep`, the doc line — plus the **five** CI/build guards the mount trips:
  `docker_compose_security_test.go` pins the orchestrator's volume list
  by exact equality (add `../memory:/memory`); compose sets
  `MEMORY_ROOT: /memory` explicitly in the `SKILLS_ROOT` mold, so
  **both** pinned environment lists move; `scripts/compose.sh` gains
  `validate_memory_bind_source` beside the skills/sandbox/mcp
  validators — **security-relevant**, because the walk-level `Lstat`
  posture polices only the inside of the mount and a symlinked host
  `turing-backend/memory` would bind straight through without it —
  with its `compose_test.go` guard beside
  `TestComposeLaunchRejectsUnsafeSkillsBindSource`, and the
  mount-presence test beside
  `TestComposeMountsFileBackedSkillsIntoTheOrchestrator`; and
  `.dockerignore` needs a **repo-scoped** `turing-backend/memory` entry
  — `**/memory` would hide `agent-runtime-go/internal/memory`, a Go
  package that **exists today** (recall), on top of the new
  `internal/service/memory` — with new entries **added to** (not
  covered by) the ignore-assertion enumeration and the
  overbroad-pattern list in the security test.

## The tests that gate the merge

Break each production gate, watch the right test fail, restore.

1. **The agent cannot write beliefs — and cannot read outside them.**
   `memory.remember` targeting `persona.md`/`profile.md`/`beliefs/…` by
   name, `../`, absolute path, or a symlink planted inside `inbox/` —
   refused; `safe` policy changes friction, not confinement; **every
   write primitive carries its own confinement, asserted by direct
   calls below the tool layer**: `createInboxNote` refuses a `beliefs/`
   target, `applyProfileEdit` refuses `persona.md` (Tier 1's headline
   invariant is one confused caller away without this leg),
   `promoteToBeliefs` refuses a source outside `inbox/`, a destination
   outside `beliefs/`, **and a `profile_edit`-kind candidate**;
   `applyProfileEdit` likewise refuses a `belief`-kind one; `rewriteFrontmatterRefs` refuses paths outside its scope, and
   `removeInboxNote` refuses any target outside `inbox/` — the suite that kills a single generic
   `writeConfined` with checks hoisted into handlers. Same suite for `memory.read`: `../`, absolute
   paths, `/skills`, the database file, symlinks. Over-limit
   `memory.remember` is **refused legibly, not truncated**.
2. **Candidates are never active.** Pinned loading reads only
   persona+profile; search and read cannot reach `inbox/` by any
   argument (no scope parameter exists); an inbox note influences
   nothing until promoted; `memory_candidates` rows are unsearchable;
   **rejection removes the row AND the file through `removeInboxNote`**
   — a rejection that drops the row and leaves the file resurrects a
   user-refused candidate as a promotable unmanaged draft.
3. **Promotion converges and survives Obsidian.** RPC-promotion and
   file-move promotion produce identical state **for managed
   candidates** (a session-less hand-dropped inbox file is an unmanaged
   draft: visible row, file-move promotion only — asserted); a rename inside
   `beliefs/` keeps id, provenance, and index; a crash between file
   move and transaction heals on reconcile **including removal of the
   orphaned candidate row** (no phantom inbox entries); the profile CAS
   refuses on fd-verified hash mismatch with the re-read wording, and an
   Obsidian edit that **completes before** the apply loses no user text
   (that is the window the CAS covers — an edit landing *mid-write* is
   the named torn-file residual, recovered through the parse-error row,
   and the test says which window it exercises).
4. **Budgets, framing, and the omission path hold.** Pinned over-budget
   truncates rune-safe with notices; **over-context omission emits the
   `MemoryOmitted` notice and payload key** (the existing omission
   guards cover only 3 of 5 fields, so this leg cannot be inherited —
   and an exhaustiveness guard is **built** (none exists today) covering every
   `contextOmissions` field while the file is open); search AND read
   results are nonce-framed (distinct per call), bounded,
   spoof-resistant, valid-UTF-8 at a computed mid-rune boundary;
   **profile pins framed, persona pins unframed** (the invariant leg);
   a `withdrawn` frontmatter rewrite leaves unknown keys, key order,
   and the note body byte-identical (the round-trip leg that kills a
   decode/re-encode rewriter).
5. **Erasure cascades by tier, survives failure, and reserves before
   writing.** A crash between manifest reservation and file write
   leaves a row with no file, tolerated by the idempotent cleaner; the
   reverse order is the untracked-orphan bug and the test exists to
   forbid it. Delete a session:
   evidence and candidate rows gone in the deleting transaction; candidate
   files removed via the cleaner; **one cleaner succeeding while the
   other fails** yields `failed_external` + retryable, **sandbox
   artifact rows unmarked and no false sandbox audit rows emitted by
   the vault failure** (the scoped-failure-marking leg round 5
   mistakenly deleted, restored), **the failing cleaner's manifest rows
   surviving the partial success** (per-cleaner removal scoping — drop
   both cleaners' rows and a no-retry path reports complete with files
   on disk), the session not reported deleted, and a retry (re-running
   both, idempotent) completing; **a session with zero sandbox
   artifacts and only pending vault artifacts flows end-to-end** — the
   pending gate fires with the exact dispatch literal, the vault
   cleaner runs, the deletion completes (asserting the pending status
   alone passes with any other literal while the ticker loops forever);
   **a belief crash-healed by reconcile survives a subsequent session
   deletion** (the composite leg: crash between move and transaction →
   reconcile → delete session → the belief file is untouched — an
   implementation releasing reservations only inside the promotion
   transaction deletes crash-healed beliefs and passes everything
   else), and in the reverse order (**session deleted before the
   heal runs**) the heal completes with the gone session's refs written
   `withdrawn` instead of failing on the evidence FK; promoted beliefs
   survive with evidence withdrawn in sidecar and, on next reconcile,
   frontmatter; **a reconcile after a crash cannot resurrect withdrawn
   evidence from stale frontmatter** (sidecar wins for evidence state).
6. **The egress binding is real, two-sided, per-tier, and
   failure-honest.** Local-only: no category. Remote with default
   persona only: category present. Editing persona — and separately
   profile — between prepare and send refuses with memory wording
   (per-tier preimage mutation, truncation bytes included); non-memory
   drift keeps its wording; the **enqueue-transaction recompute** refuses
   a post-consent change with the wrapped sentinel — distinguishable by
   message text within `FailedPrecondition`, with the specific arm
   ordered before the generic `ErrEgressDecisionInvalid` arm (the
   wrapped sentinel satisfies both, so ordering is load-bearing);
   **a whitespace-only `persona.md` yields no category on both sides
   and the run executes** (the trim rule mirrored identically in the
   runtime's re-derivation — one-sided trimming is a flag mismatch and
   a hard refusal at `Execute`); **`memory.read` returns bytes
   identical to the file, not the projection** (edit the file, read
   without an intervening reconcile, see the fresh bytes); the runtime independently re-derives flag and
   fingerprint from the job and refuses mismatches, end to end through
   `Execute`; a decision claiming the category over an empty snapshot
   with no memory tools selected is refused; memory tools selected with
   empty pinned tiers still attaches the category; **a symlinked or
   unreadable persona pins nothing, attaches no category, and surfaces
   the visible unavailable row** (the negative honesty leg).
7. **The toggle governs enqueues, refuses at dispatch, and survives
   re-report.** Off: nothing pinned, tools absent from the registry,
   **a direct tool-RPC dispatch with the toggle off is
   refused** (registry state is bypassable by construction — the
   `CallIntegrationTool` analogy — so absence alone is not enforcement); on without restart via
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
    text becomes searchable** (the update-trigger leg the schema guard
    cannot check); **duplicate frontmatter ids flag both copies and
    index neither; malformed frontmatter is a per-note error row, never
    fatal to the vault**; **two concurrent searches and a concurrent
    reconcile run under the race detector** (the leg that makes the
    mutex/singleflight claim falsifiable — remove the guard, `-race`
    fails); **a search over a vault containing notes with unassigned
    ids leaves every file byte-identical** (read-only means no ULID
    assignment, no rewrite — a mutex-guarded write passes `-race` and
    still mutates the user's vault); the 4096 bound refuses legibly and blocks neither pinned
    tiers nor enqueue.
12. **Policy classes are right, tested at the policy that matters.**
    `memory.search`/`read` arrive `safe`; **raised to
    `approval_required`, a failed search is still a recoverable tool
    error** (the leg that makes the `readOnlyTools` entry load-bearing)
    and `ToolPolicyDecision.ReadOnly` is asserted on the wire;
    `memory.remember` arrives `approval_required`; the whole `memory`
    server is refused on an automation allowlist at save; **and every
    memory tool is refused at dispatch on an automation run regardless
    of policy — including `memory.remember` deliberately set `safe`**
    (the leg that kills allowlist-only enforcement, which safe tools
    never reach).

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
  both relaxations, all five foreclosing passages rewritten, the vault
  named as a governed projection with its manifest classifications.
- `docs/architecture/2026-08-18-personal-agent-audit.md` — the substrate
  note (005/006/007 reshaped; what remains).
- `docs/architecture/remote-egress-policy.md` — `MEMORY_PROFILE` live:
  both conjuncts, the fingerprint beside the skills one, the
  external-agent divergence from recall.
- `docs/VISION.md` — memory joins the untrusted-input invariant's list.
- `CLAUDE.md` — the `/memory` mount.
