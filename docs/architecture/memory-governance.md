# Memory governance and derived-state contract

**Status:** architecture contract for MEM-001. Turing does not yet have curated
long-term memory; this document constrains the memory work that follows.

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
repository deletion tests cover both run-correlated and session-targeted audit
rows.

This contract applies to future facts, preferences, instructions, candidates,
revisions, summaries, embeddings, vector indexes, graph edges, caches,
connector projections, and prompt-assembly snapshots. Renaming derived state
does not change its obligations.

## Trust classes

| Class | Examples | Permitted use |
|---|---|---|
| User-authored evidence | User messages, explicit profile edits, a deliberate "remember this" action | May become active memory through an explicit user action. It is evidence of what the user said or requested, not proof that the statement is objectively true. |
| Locally derived candidate | A fact, preference, summary, or consolidation proposed by a local model or deterministic process | Untrusted until reviewed. It may be stored as a source-linked candidate but must not silently become an active belief or instruction. |
| External or delegated evidence | Assistant replies, remote-model output, tool results, files, web content, connector data, imported records | Untrusted content. It must retain source and egress attribution and cannot directly create an active belief or procedural instruction. Imports are isolated candidates until the user accepts them. |
| Active user-controlled memory | A user-created item or an accepted candidate with immutable provenance and revision history | Eligible for recall within its scope. Recall must expose provenance and date, and active instructions must remain distinguishable from untrusted recalled content. |
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
withdraws the derived fact even though its recall scope was wider.

## Egress

Memory extraction, indexing, maintenance, and evaluation are local by default
and introduce no background network traffic. A remote provider selected for a
run does not gain standing access to the memory store.

Before memory can be sent off-machine, a future egress policy must make the
destination and exact data classes visible and bind the choice to the
session/run. Only the minimum selected items may be sent, each with provenance
and sensitivity filtering. Candidates, full-memory exports, connector data,
and background consolidation must not be sent to remote providers implicitly.
An egress record belongs to the run and remains attributable even if routing
configuration later changes.

## Retention

No derived row may outlive the source it depends on.

- Explicit active memory remains until the user supersedes, retracts, deletes,
  or withdraws one of its sources.
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
it, when, and which evidence supports each revision.

Retraction records that an item should no longer be used while preserving its
source-linked history. Deletion removes the item and its dependent
projections. Source or session withdrawal is stronger: every fact, candidate,
revision, summary, embedding, index row, cache entry, and prompt snapshot
derived from that source is removed in the same transaction or through a
database-enforced cascade. A failed cascade fails the withdrawal rather than
leaving an orphan.

FTS is cascade-equivalent rather than a policy exception:
`messages_fts` uses `messages` as external content, and an `AFTER DELETE`
trigger removes the matching index row in the deleting transaction. The schema
invariant verifies both the source declaration and delete trigger.

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
- a new unclassified application table is rejected; and
- an unapproved or unjustified scrubbed exception is rejected.

The guard is a CI contract, not a startup refusal. Migration authors must
classify a new table and prove its lifecycle in the same change that creates
it. Later memory tasks remain blocked until their schema satisfies this
contract.
