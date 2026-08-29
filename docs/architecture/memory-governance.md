# Memory governance and derived-state contract

**Status:** architecture contract for MEM-001, amended once. Phase 1 of the
vault-backed memory work ships under Amendment 1 below, which rewrites five
passages of the original contract and names two relaxations. Everything the
amendment does not name is unchanged and binding.

## Boundary and invariant

Turing's durable memory is a projection over evidence owned by the local
orchestrator. It must not become a second, less-governed copy of conversation
content.

Every application-owned SQLite table is classified by the schema invariant
test. A table that stores content derived from a user-controlled source must
identify that source and have a transitive `ON DELETE CASCADE` path to it. A new
or unclassified table fails the invariant. The only alternatives are:

- an SQLite-managed projection whose source linkage and transactional delete
  behavior the invariant can verify, such as the external-content
  `messages_fts` index; or
- an explicitly named scrubbed-tombstone exception that retains no user
  content and includes a non-empty justification.

Today, `audit_logs` is the sole scrubbed-tombstone exception. Session deletion
replaces the affected audit payload with `{"scrubbed":true}` before deleting
the session. The remaining row may prove that an action or deletion occurred;
it may not preserve prompts, arguments, results, extracted facts, summaries,
identifiers copied from content, or any other recoverable user material.
The schema manifest restricts the exception to an explicit allowlist, while
repository deletion tests cover run-correlated rows, uncorrelated rows that
target the session, and unrelated correlated rows that merely share a target
value.

This contract applies to future facts, preferences, instructions, candidates,
revisions, summaries, embeddings, vector indexes, graph edges, caches,
connector projections, and prompt-assembly snapshots. Renaming derived state
does not change its obligations.

## Amendment 1 — vault-backed memory (Phase 1)

Phase 1 gives Turing a memory the user can open in a text editor: an
Obsidian-compatible vault of plain Markdown at `MEMORY_ROOT` (`/memory` in the
container, `turing-backend/memory/` on the host — the latter is what the app
shows, as the display-only `MEMORY_DISPLAY_ROOT`), holding `persona.md`,
`profile.md`, `inbox/` and `beliefs/`. This section is the whole amendment. It
names two relaxations and rewrites five passages of the contract above —
§Ownership and scope's final paragraph, §Retention's first bullet, the **Active
user-controlled memory** taxonomy row, §Correction's withdrawal sentence, and
§Egress — in place, rather than leaving them to be read around.

### Relaxation 1 — promotion is authorship

When the user promotes a candidate, with its full content displayed and — by
editing its inbox file in the vault — editable before acceptance, that act is
the item's source. Links to the session the proposal came from demote from
load-bearing dependencies to annotations. The decision is bound to the exact
text by hash: a candidate edited in the vault after the user read it is
refused rather than accepted unseen, so what promotion authors is always text
the user was shown. Editing a proposal inside the app before accepting it is
deferred — the server refuses `edited_content` explicitly rather than
accepting and ignoring it.

The consequences, stated rather than implied:

- **Machine-owned state obeys the unamended contract.** Candidates and evidence
  annotations are derived state with a database-enforced cascade to the session
  that produced them, plus an external file cleanup for the candidate files
  themselves. A failed cleanup fails the withdrawal.
- **User-adopted content survives the deletion of its source**, as a note the
  user typed by hand would. Its evidence annotations do not: the rows are
  removed in the deleting transaction, and the frontmatter references that cite
  the deleted session are rewritten to `withdrawn` by a reconcile the deletion
  flow triggers as its vault cleaner completes — not left for the next restart.
  A promoted belief therefore never cites deleted evidence for longer than the
  deletion takes, and the client shows it as unevidenced. The *content*
  persists. That is the relaxation, and it is the only place a "fact" outlives
  its originating session. What does not persist is its standing to be answered
  with: a withdrawn note is refused by search *and* by a read of its identity,
  so a model still holding the id from before the deletion cannot retrieve it.

### Relaxation 2 — vault beliefs defer the revision chain

§Correction requires immutable revisions and supersession, and §Writers requires
an authenticated orchestrator operation for every mutation. A folder the user
edits in Obsidian offers neither: a file edit and an RPC converge in reconcile,
which emits audit events for the changes it detects but cannot reconstruct a
revision history the file never carried. Phase 1 therefore defers the belief
revision chain, supersession, and lifecycle-validated vault edits to **MEM-010**,
explicitly. `memory_candidates` rows, being machine-owned, keep validated
lifecycle transitions and audit events from day one.

Nothing here ships revision history for vault files, sensitivity filtering,
in-app editing of a proposal before acceptance, or automatic extraction.
Automatic candidate extraction from conversation remains
**MEM-009**: today a candidate exists only because a run called
`memory.remember`, and assistant, tool, and recalled content are not a
candidate source.

### The vault is a governed projection, and its tables say so

The files are the content of record; SQLite holds what files cannot — identity,
evidence, search, and the erasure contract. The schema manifest classifies every
table in the same change that created it:

- `memory_candidates` — **cascade_owned**, source `sessions`, `NOT NULL`
  session foreign key with `ON DELETE CASCADE`. Inbox proposals, of both kinds
  (`belief` and `profile_edit`), are machine-owned rows. They are never
  searchable and never active: no tool argument can name `inbox/`, and nothing
  in it is pinned. One lifecycle state, `profile_applying`, is not a decision
  waiting to be taken: it is the claim an apply records — under the
  per-candidate lock, before `profile.md` is written — carrying the hash of the
  document it is replacing and the hash of the one it is about to produce, and
  nothing else. No decision RPC accepts a candidate in that state, so a
  rejection cannot retire a proposal whose words the user's own document may
  already carry, and a pass that finds the claim after a crash resolves it by
  reading `profile.md`: the result hash finishes the apply, the base hash hands
  the proposal back, and anything else leaves the claim standing rather than
  guessing. Only hashes are stored; the resulting document lives in the request
  and in the user's file, never in a row.
- `memory_evidence` — **cascade_owned**, source `sessions`, cascading from both
  the note and the session. These are the annotations Relaxation 1 demotes.
- `vault_artifacts` — **cascade_owned**, source `sessions`. A reservation is
  written *before* the candidate file is created, so a crash leaves a row with
  no file, which the idempotent cleaner tolerates, rather than a file no cleaner
  knows about while the deletion reports complete. The same rule holds at the
  other end: an applied proposal whose file could not be removed keeps its
  reservation, because releasing it beside a file that is still there would
  leave a claim about the user in their vault with nothing in the manifest
  naming it.
- `memory_notes` — **independent**, with Relaxation 1 as its written rationale:
  promoted notes are user-authored files whose lifecycle the user controls, and
  their session provenance cascades separately through `memory_evidence`.
- `memory_notes_fts` — an **external-content FTS5 projection** over
  `memory_notes.content`, with insert, delete, and update triggers, verified by
  the same guard that verifies `messages_fts`.

Two further boundaries, kept honest: `memory.remember` writes bounded
distillations, refused legibly when over the limit rather than truncated, and
never transcripts; and candidate bodies live in the cascade-owned table so that
even before promotion they are not a second, less-governed copy of conversation
content.

### Named residuals

These are known, bounded, and deliberately not claimed away:

- **A vault is designed to be synced.** The erasure claim stops at the vault
  directory. Copies made by Obsidian Sync, iCloud, Dropbox, git, or a backup
  tool are outside it, exactly as §Backup and export already scopes deletion
  against user-made copies.
- **A profile write is deliberately not atomic.** `profile.md` is written
  through a descriptor opened in place, verified by content hash on the same
  descriptor, because a rename would swap the inode out from under an editor
  holding the file open. A crash or a concurrent read mid-write can therefore
  observe a torn `profile.md`; the pinned-file failure posture recovers it as a
  visible parse-error row rather than as silence.
- **The scan's (inode, mtime, size) cache can serve stale text.** An edit made
  to the same file in the same second that leaves it the same length may not be
  noticed until the next change. The inode is part of the key so a rename —
  which moves a note onto a name another note was holding — is always a miss.
  It affects belief search and index discovery only: `memory.read` serves the
  file, freshly read, and the inbox is never answered from the cache at all.
  Every pass reads, parses and hashes each candidate again, because a candidate
  is the text a user decides about and the hash their decision carries, and a
  remembered parse there would show one proposal on the page while the server
  refused every decision about it.
- **The 4096-file scan bound degrades search and reconcile only.** A vault past
  it refuses legibly; pinned tiers and enqueue are never blocked by vault size.
  A second bound sits under it: the walk refuses after examining 16,384
  directory entries in total — folders, attachments and skipped files
  included, none of which the note bound counts — through the same legible
  refusal, so a vault of things that are not notes cannot stall the scan
  either.
- **A proposal nothing can open cannot be rejected from the app.** Every
  removal here is authorised by a descriptor whose own identity is checked,
  because an unlink names a name and the entry under a name can be replaced
  between the check and the removal. A file with no permissions yields no
  descriptor, and a second failure to open is not evidence about which entry a
  name holds — a different file, unreadable in the same way, answers
  identically. So the rejection refuses, says which file and why, and names the
  way out: make it readable in the vault, or delete it there, which needs no
  permission on the file itself. This is scoped to the one case that cannot be
  opened at all; a proposal past the size bound opens, proves itself, and is
  still removable.
- **A file Turing cannot prove it wrote is never withdrawn, and its manifest
  row stays.** Every vault artifact the session manifest tracks carries the hash
  of the whole file exactly as it was written, recorded when the write is
  confirmed, and the withdrawal that removes it deletes those bytes or nothing.
  A path is not an owner: the user may move a proposal out of the inbox and save
  something of their own under the name it had, or open it and rewrite it in
  place. Either way the removal refuses, the file is untouched, the row is kept
  and marked, and the withdrawal reports itself unfinished and retryable rather
  than complete. A reservation whose write never landed still drains, because
  there is no file to remove; a write that landed but whose bookkeeping was lost
  to a crash is bound by the reconcile pass, which reads the file under the
  reserved path afresh and adopts it only when it is a managed note carrying the
  identity in the name this server minted. So a user who leaves their own file
  at a withdrawn proposal's path keeps that file and keeps a manifest row until
  they move it, which is the trade this makes deliberately.
- **The vault path shown in the app is display-only and may be absent.** The
  orchestrator opens the vault at `MEMORY_ROOT`, which under Compose is
  `/memory` — a directory inside one container and nowhere the person reading
  the Memory page can go. What the page shows is `MEMORY_DISPLAY_ROOT`, the host
  directory the vault is bound from, written into `.env` by `scripts/init.sh`
  as a Compose single-quoted literal and passed through Compose. It is never
  consulted for access, confinement or any other decision; a value that is not a
  clean absolute path is omitted rather than rendered, and the page then names
  no folder at all instead of naming one that does not exist. The requirement
  that it be present and usable is enforced by `scripts/compose.sh` before it
  starts or resolves services, and deliberately not by the compose file: a
  required interpolation there is evaluated on `down`, `stop` and `rm` as well,
  and a teardown that refuses on a stale `.env` leaves containers running over
  data `reset.sh` is about to delete.
- **A link the filesystem refuses to drop is kept and named, not deleted.**
  Every removal here takes the entry off its name first and unlinks only what it
  then verified. When that unlink fails, the entry goes back under its own name
  — with a link that refuses to clobber whatever may have taken it — so the
  record that names the file still finds it and the retry can prove ownership
  all over again. The link the failed drop left behind is reported in the
  failure, by name, and it outlives the retry that removes the visible name —
  so a session withdrawal, whose whole job is that the bytes go, sweeps the
  reserved entries in the inbox whose content is exactly what it was already
  entitled to delete, and keeps its manifest row when one of them will not go.
  A reserved entry it cannot name is another writer's and is never touched, so
  a vault left holding one after a failure nothing could clear still needs a
  person to remove it. Two kinds cannot be named at all and are named here
  instead: a copy of a proposal too large to read, which nothing can hash, and a
  copy of bytes the user rewrote after the decision read them, which hashes to
  something no record holds — including when a crash leaves the recovery pass
  asking about a proposal with only the hash it was created with. Both are
  reported in the failure that left them, by name. Where the original name has been taken in the meantime,
  the bytes stay under the reserved name and both places are named: a decided
  proposal is never republished into the inbox as a fresh draft, because the
  user has already answered it. And the rule about putting an entry back is the
  area's, not the caller's: only under `inbox/` does a name have a record
  pointing at it. The compensating removals under `beliefs/` and at the vault
  root are undoing a write nothing durable names, so a refused unlink there
  keeps the bytes under the reserved name rather than re-linking them —
  re-linking would publish a belief the user never accepted and take the name
  the abandoned promotion would need to be retried under.
- **A promotion abandoned before its original was removed can leave a belief a
  crash restores.** The rollback of the copy such a promotion installed unlinks
  it and flushes; when that flush fails, a crash can bring the copy back under
  its own name, where the walk indexes it. The proposal is still in the inbox
  and the user did ask for the promotion, so what this costs is a claim
  appearing twice rather than one appearing that nobody accepted. The failure
  names the copy either way.
- **A vault failure that left a copy is recorded before it is returned.** A
  decision that fails inside the vault writes nothing else: the proposal stays
  pending and its manifest row stays as it was. The same is true of a creation
  that abandons the bytes it just wrote. Where the record cannot be written at
  all — a process that dies between the two — the reconcile pass asks the vault
  itself before releasing a row, because a reserved entry holding exactly the
  bytes a row names is that row's own file. When the failure says bytes were
  left under a name only the vault can produce, the row is marked first, because
  the path it records now holds nothing and the tidying below would otherwise
  release it — taking the last thing that could find those bytes with it.
- **A manifest row whose cleanup failed is not released by the reconcile
  pass.** Reconcile releases reservations whose path the inbox no longer holds,
  and a failed removal is one of the ways a path comes to hold nothing. A row
  marked `delete_failed` therefore belongs to the session cleaner, which
  re-verifies ownership before it removes anything and drains the row when the
  file is really gone; until a session is deleted, such a row stays. Keeping one
  costs a row in a manifest. Releasing one costs the user a note nothing in the
  system can ever find again.
- **A promotion whose original cannot be put back keeps the belief it wrote.**
  When the removal of a promoted original fails *and* the name it came off has
  been taken in the same moment, the bytes stay under the reserved name, the
  belief that was written stays, and the failure says so. Undoing the belief
  there would leave the user with a claim they accepted reachable only under a
  name no listing shows; keeping it leaves a hidden duplicate of content they
  did accept. Neither is tidy, and the second loses nothing.
- **An absence is only reported once it has reached the disk.** Removing a file
  and retiring the record that names it are two durable facts, and an unlink
  that is still only in the page cache is not the first of them. So the removal
  primitive flushes the directory before it reports a missing file as gone, and
  a flush that fails keeps the record — the next pass asks again rather than
  retiring a row for a file a crash can bring back. A vault whose root cannot be
  opened at all is not an absence either: the notes are wherever the vault is,
  and every record naming one is kept.

### What the amendment does not touch

The physical-erasure limit, the backup and export rules, the scrubbed-tombstone
exception, the writer authority rules, and the schema-invariant enforcement
below are unchanged. Logical withdrawal remains distinct from physical erasure;
a promoted note that survives a session deletion under Relaxation 1 is content
the user authored, not a byte-level exception to anything.

## Trust classes

| Class | Examples | Permitted use |
|---|---|---|
| User-authored evidence | User messages, explicit profile edits, a deliberate "remember this" action | May become active memory through an explicit user action. It is evidence of what the user said or requested, not proof that the statement is objectively true. |
| Locally derived candidate | A fact, preference, summary, or consolidation proposed by a local model or deterministic process | Untrusted until reviewed. It may be stored as a source-linked candidate but must not silently become an active belief or instruction. |
| External or delegated evidence | Assistant replies, remote-model output, tool results, files, web content, connector data, imported records | Untrusted content. It must retain source and egress attribution and cannot directly create an active belief or procedural instruction. Imports are isolated candidates until the user accepts them. |
| Active user-controlled memory | A user-created item, or a candidate the user promoted with its full content shown, and editable in the vault, before acceptance | Eligible for recall within its scope. Recall must expose provenance and date, and active instructions must remain distinguishable from untrusted recalled content. Promotion is authorship: the promoted content's source is that act, and its links to the originating session are annotations rather than dependencies (Amendment 1, Relaxation 1). Immutable revision history is required of database-owned items and deferred for vault files (Relaxation 2). |
| Operational metadata | IDs, timestamps, lifecycle state, hashes, model/extractor version, token counts | May support policy, audit, and explanation. It must not smuggle a content copy around deletion rules. |
| Scrubbed audit tombstone | Minimal evidence that an action or deletion occurred | May survive source deletion only under the explicit exception above. It cannot be recalled as memory or reconstructed into deleted content. |

## Writers and authority

The orchestrator is the only durable writer. Clients, runtimes, models, tools,
MCP servers, and connectors do not write memory tables directly and cannot
supply authoritative provenance fields.

The allowed logical write paths are:

1. An explicit user action creates active memory from user-authored content.
2. A user edits profile or instruction state, creating a new revision.
3. A local extractor proposes a source-linked candidate after a run.
4. A previewed import creates attributed candidates in an isolated import
   scope.
5. A bounded local consolidation job proposes deduplication or supersession
   while retaining a reversible preimage.

Background writers may create candidates and operational metadata only. They
cannot activate, broaden the scope of, or convert untrusted content into a
belief or instruction. Active-memory creation, correction, supersession,
retraction, and deletion must use authenticated orchestrator operations,
validate lifecycle transitions, and produce a redacted audit event. Memory
work does not create a weaker substitute for argument-bound tool approval:
any tool mutation caused by a run still uses the existing approval path.

## Ownership and scope

Turing remains single-user. The initial memory scopes are:

- `user`: available across the local user's conversations;
- `project`: available only inside one explicitly identified project boundary;
- `session`: owned by one conversation and deleted with it.

Scope is a recall boundary, not an identity or access-control claim. A writer
must choose the narrowest scope supported by its source and must not promote a
session or project item to a wider scope automatically. Cross-scope
consolidation requires an explicit user decision and preserves links to every
source.

Source ownership is separate from recall scope. A user-scoped fact derived
from a session message still depends on that message; deleting the source
withdraws the derived fact even though its recall scope was wider — with one
exception, defined in Amendment 1: content the user promoted by an explicit
act with the full text shown, and editable in the vault, is authored by that
act, so it survives
the deletion of the session it was proposed in, while every machine-owned row
about it is removed and every citation of the deleted source is rewritten to
`withdrawn`.

## Egress

Memory extraction, indexing, maintenance, and evaluation are local by default
and introduce no background network traffic. A remote provider selected for a
run does not gain standing access to the memory store.

The remote-egress policy makes the destination and conservative maximum data
classes visible and binds a one-time decision to the exact request and run.
Memory/profile is a typed category and, since Amendment 1, is applicable and
sent: a remote run attaches it when the pinned persona/profile snapshot survives
trimming or when a memory tool is in the frozen tool set. The consent is
per run over whole pinned tiers rather than per item: the pinned documents are
user-authored configuration the user consents to as a whole, disclosed by tier
and vault path, and re-derived by the runtime before a provider is contacted.
Per-item selection and sensitivity filtering are **deferred, not claimed** —
nothing filters a pinned document by sensitivity today. Candidates, full-memory
exports, connector data, and background consolidation must not be sent
implicitly: `inbox/` is unreachable by any tool argument and is never pinned.
The egress record belongs to the run and remains attributable even if routing
configuration later changes. See
[Remote egress policy](remote-egress-policy.md) for the exact applicability
rule and its two-sided re-derivation.

## Retention

No derived row may outlive the source it depends on.

- Explicit active memory remains until the user supersedes, retracts, deletes,
  or withdraws one of its sources — except for content the user authored by
  promotion under Amendment 1, which depends on no session and remains until the
  user deletes it. Its machine-owned annotations still obey the sentence above:
  they are removed with the session they cite.
- Candidate retention must be bounded and documented before automatic
  extraction ships. Expired or rejected candidates are deleted, not hidden
  indefinitely from the active view.
- Superseded revisions may remain for correction history only while all of
  their sources survive. They are excluded from current beliefs and recall.
- Caches, indexes, embeddings, summaries, and prompt snapshots are disposable
  projections. They are deleted with their source and may be rebuilt only from
  surviving sources.
- Audit retains only the scrubbed tombstone described above after source
  withdrawal.

The current database retains most operational history without a pruning
policy. MEM-001 does not claim otherwise and does not introduce automatic
retention jobs.

## Backup and export

A consistent backup is a copy of governed state, not an escape from the
contract. Backup tooling must use SQLite's supported consistency mechanisms
rather than copying a live database and WAL independently. A future restore
path must validate the restored schema before use. The current guard is
CI-only, so backup/restore work must first extract or reproduce the invariant
in callable production code; MEM-001 does not provide restore tooling.

Memory export must be explicit, local by default, and open-format. It includes
scope, lifecycle, revisions, provenance, and timestamps so an export does not
turn attributed memory into unexplained assertions. It excludes encryption
keys, credentials, approval tokens, and scrubbed payloads. Import treats the
file as untrusted external evidence and previews attributed candidates before
activation.

Deletion affects the active database and projections it owns. It cannot recall
or erase copies the user already exported, copied, synced, or backed up.
Backup retention and deletion controls must therefore be disclosed by the
backup feature before that feature can make an erasure claim.

## Correction, supersession, and withdrawal

Correction creates a new immutable revision and marks the previous revision
superseded; it does not rewrite history. Only active revisions participate in
current beliefs or recall. The UI and API must show what changed, who changed
it, when, and which evidence supports each revision. Amendment 1's Relaxation 2
defers this for vault belief **files**, which the user edits in their own
editor: those changes are detected and audited, not versioned. Database-owned
candidates keep validated lifecycle transitions and audit events from day one.

Retraction records that an item should no longer be used while preserving its
source-linked history. Deletion removes the item and its dependent
projections. Source or session withdrawal is stronger: every candidate,
revision, summary, embedding, index row, cache entry, and prompt snapshot
derived from that source is removed in the same transaction or through a
database-enforced cascade, together with every fact except one — content the
user authored by promotion under Amendment 1, whose citations of the withdrawn
source are rewritten to `withdrawn` in the same flow that removes the rows. A
failed cascade, or a failed external file cleanup, fails the withdrawal rather
than leaving an orphan.

FTS is cascade-equivalent rather than a policy exception:
`messages_fts` uses `messages` as external content, and an `AFTER DELETE`
trigger removes the matching index row in the deleting transaction.
`memory_notes_fts` is the same shape over `memory_notes`, with an update trigger
as well, so an edited note becomes searchable instead of leaving stale text in
the index. The schema invariant verifies both the source declaration and delete
trigger for each.

## SQLite physical-erasure limit

Logical deletion is not forensic byte erasure. Turing currently uses SQLite
with WAL enabled and does not enable `secure_delete`. Deleted text can remain
in freed database pages, WAL files, filesystem snapshots, storage-device
remapping, and user-created backups until those layers overwrite or discard
it. `VACUUM`, checkpointing, or `secure_delete` can narrow some remnants but
cannot prove erasure across copied files or storage hardware.

Product language must therefore distinguish:

- **logical withdrawal:** rows and all queryable/rebuildable projections are
  gone and cannot be recalled by Turing; from
- **physical erasure:** no recoverable bytes remain on the underlying system,
  which Turing does not currently guarantee.

Encryption-at-rest and backup lifecycle work require separate designs; this
contract does not imply that either has shipped.

Whole-database encryption plus destruction of every database-key wrapper and
encrypted backup is the credible strategy to evaluate for retiring an entire
database without promising SSD overwrite. A single database key cannot
selectively erase one session, however: per-session byte withdrawal would need
separate encrypted storage/key boundaries and its own restore, key-loss, WAL,
and backup lifecycle design. TUR-004 therefore documents this limit and does
not add SQLCipher, key management, or an encryption migration.

## Enforcement and tests

The DB schema-invariant test maintains an exhaustive policy manifest for
application-owned ordinary and virtual tables. It ignores SQLite's internal
and FTS shadow tables, but not new application tables. For each derived table,
the guard requires a distinct, present, classified source, then walks declared
foreign keys and requires a transitive `ON DELETE CASCADE` path whose child
columns are explicitly `NOT NULL`. Independent source/configuration roots need
a written rationale rather than acting as a silent escape hatch. The guard
separately verifies the external-content declaration and delete-trigger shape
for `messages_fts`, then inserts and deletes a probe to prove the index row is
removed transactionally.

The regression suite proves:

- today's complete schema is classified and valid;
- a synthetic derived-text table with a source foreign key but no
  `ON DELETE CASCADE` is rejected;
- a scrubbed-exception table cannot become another derived table's provenance
  source;
- a new unclassified application table is rejected; and
- an unapproved or unjustified scrubbed exception is rejected.

The guard is a CI contract, not a startup refusal. Migration authors must
classify a new table and prove its lifecycle in the same change that creates
it. Later memory tasks remain blocked until their schema satisfies this
contract.
