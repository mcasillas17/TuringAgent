# Skills Egress Disclosure Implementation Plan

Name the skills in the consent dialog. When a run may leave the machine — a
remote model, an external agent — the disclosure today says "Enabled skill
content" as a bare category line, and it says it **unconditionally**: the
category is attached on every provider-egress prepare, including with zero
skills enabled. The user acknowledges that *some* skill content may leave
without being told *which* skills, and sometimes acknowledges skill content
that does not exist. Both halves are honesty bugs in the consent story, and
both are cheap to fix because the binding machinery already exists — this plan
adds legibility to a decision that is already sound, and deliberately adds
**no new binding, no proto-breaking change, and no migration**.

## The problem, stated precisely

- `resolveEgressContext` (`service/chat/egress.go` ~483) attaches
  `EGRESS_DATA_CATEGORY_SKILL_CONTENT` whenever `providerEgress` is true —
  regardless of whether any skill is enabled. The Flutter dialog renders the
  category label ("Enabled skill content", `models/remote_egress.dart`) and
  nothing more.
- The decision *is* already bound to the exact skill state:
  `EgressSkillSnapshotFingerprint` (`repository/egress.go:201`) fingerprints
  the enabled-skill snapshots inside a read-only transaction at prepare; the
  fingerprint rides inside the signed challenge payload;
  `payloadMatchesEgressContext` compares it at send; and
  `enqueueUserMessageTx` re-derives and refuses on mismatch. **What is not
  yet true is that any test pins the refusal.** The existing
  `TestEgressSkillFingerprintMatchesEnqueueAfterSkillEditWithoutPrepareWrites`
  pins the *opposite direction* — that the read-only prepare path performs no
  writes, so its fingerprint agrees with enqueue's and no false-positive
  mismatch occurs (the edit in that test happens *before* prepare). The
  drift-refusal direction is new coverage this plan adds, not existing
  coverage it cites.
- Per the skills model (CLAUDE.md, PRs #55–#62): the enqueue snapshot carries
  *metadata* for every enabled, parseable skill, and marks a skill
  `Withheld` — body and references blanked — unless every declared
  capability is granted. Even a non-withheld body does not automatically
  ride: the runtime injects the skill *index* for every snapshot entry, and
  a body only when the user invokes `$<id>` or the model calls `skill_view`
  (which also reaches `references`). So "included" is a **ceiling** — the
  maximum that may leave — exactly as `remote-egress-policy.md` already
  frames disclosure, and this plan's UI language must say so.

## Design decisions (locked)

**Everything keys on the category, not on skill enablement.** The skills
list in the disclosure, and the skill names in the run notice, are populated
**iff `SKILL_CONTENT` is in that decision's data categories** — never "iff
skills are enabled." The two conditions genuinely diverge: a local-Ollama
run with a remote MCP tool has `providerEgress == false`, gets no
`SKILL_CONTENT` (correctly — no skill content can reach an MCP server), yet
the machine-wide enabled-skill snapshot is non-empty. Keyed on enablement,
the notice would claim skill egress on a run where it categorically cannot
happen — reintroducing the exact bug this plan exists to fix, one surface
over. The existing `TestLocalRemoteMCPConsentNoticeAndAuditNameDestination`
covers that scenario with zero skills; the new tests re-run it with skills
enabled.

**The category itself becomes `providerEgress && snapshot non-empty`, and
every composition mirror moves in the same commit.** Both conjuncts matter:
dropping `providerEgress` would newly attach the category to MCP-only local
runs (see above); dropping the non-empty check is today's over-claiming. The
mirrors:

- `resolveEgressContext` (`service/chat/egress.go`) — the attachment site;
- the runtime's `validateEgressDecisionShape`
  (`agent-runtime-go/internal/agent/egress.go` ~98–106) — its exact-set
  `required` list includes `SKILL_CONTENT` unconditionally for provider
  runs; it conditions on `len(job.GetSkills()) > 0`, which needs no new
  plumbing (the function already reads `job.GetSkills()` at line ~49) and is
  provably equivalent to the fingerprint condition, because the existing
  check already forces job-snapshot emptiness and the empty-fingerprint
  value to agree;
- the challenge-payload structural validation composes no category set
  (verified: `validChallengePayload` checks range/sort/dup only) — nothing
  to change there, stated so nobody goes looking.

Named consequence, accepted: **queued-job version skew fails closed.** Jobs
persist across restarts; a pre-upgrade decision carrying `SKILL_CONTENT`
with an empty snapshot fails the post-upgrade runtime's exact-set check and
the run is refused — the user re-sends and gets a fresh, honest decision.
Consent freshness beats replay convenience; the plan states this rather than
pretending the mirrors move atomically across queued state.

**Names are derived, displayed, and bound by the fingerprint that already
exists — they do not join the signed payload.** Prepare derives the skill
list and the fingerprint from the *same* snapshot read; the signature binds
the fingerprint; send and enqueue re-derive and refuse on mismatch. The real
invariant — stated because "one read" is necessary but not sufficient —
is: **every field the disclosure displays must be covered by the
fingerprint preimage.** All three displayed fields are (`SkillID`, `Name`,
and `Withheld` are fields of `backendegress.SkillSnapshot`, whose canonical
form the fingerprint digests), and a test pins it per-field: mutate each
displayed field, assert the fingerprint changes. A later field added to the
disclosure without preimage coverage silently voids the binding, and that
test is where the addition gets caught.

**`skill_id`, from `SkillSnapshot.SkillID` — not `FolderPath`.** The proto
field is named `skill_id` to match the identity used throughout the egress
code. The wrong-but-natural alternative is named so nobody wires it:
`Skill.FolderPath` is an **absolute filesystem path** under the container
root; using it would both leak `/skills/...` paths into the dialog and step
outside the fingerprint preimage, voiding the binding.

**`body_may_be_sent` comes from the snapshot's own `Withheld` bit — never
recomputed.** The field is `!snapshot.Withheld`, read from the same snapshot
entry the fingerprint covers. Recomputing it from `skill_capability_grants`
is the trap, twice over: `enabledSkillSnapshotsReadOnlyTx` classifies and
drops *stale-scoped* grants (a skill that widened its `requires:` since the
grant), while the naive `SELECT` does not — and prepare deliberately never
reconciles, so the divergence is live, not theoretical; and a skill
declaring zero capabilities must trivially read as not withheld. The proto
name says "may be" because it is a ceiling (see above); the dialog tags read
"full content may be sent" and "name and description only." References ride
under the same ceiling as the body (`skill_view` reaches both) and need no
separate field — "full content" covers them, and the docs say so.

```proto
message SkillEgressDisclosure {
  string skill_id = 1;          // SkillSnapshot.SkillID: relative path identity
  string display_name = 2;      // flattened + capped, see below
  bool body_may_be_sent = 3;    // !Withheld, from the snapshot entry
}
repeated SkillEgressDisclosure skills = 11;
```

**Skill names are untrusted strings entering a consent surface — flatten
and cap them.** Skill content is untrusted prompt material (CLAUDE.md), and
a name can carry newlines, absurd length, or direction-control characters
into the very dialog that gates egress. The runtime already flattens skill
metadata for exactly this reason (`oneLine()` in `agent/skills.go`); the
disclosure applies the same flattening plus a rune cap
(`maxSkillDisplayNameRunes = 80`, ellipsis-truncated) **server-side**, so
the property is testable in Go, not a Flutter styling hope. The list itself
is bounded: `maxEgressSkills = 256` (the `maxEgressTools` mold), refused at
`resolveEgressContext` with a legible `FailedPrecondition` naming the remedy
(disable skills); the run-notice line truncates at 8 names with "+N more".

**The legible refusal lives where users actually hit it —
`service/chat/egress.go`, not primarily `jobs.go`.** The common drift case
(skill enabled, disabled, edited, or a grant changed between prepare and
send) is caught by `applyRemoteEgress` → `payloadMatchesEgressContext`,
today's message "remote egress context changed; prepare the send again" —
`SendMessage` returns there and `EnqueueUserMessage` is never reached. That
message gains the specific cause when the fingerprint is the mismatched
field: "the skill snapshot changed since consent was prepared; prepare the
send again" — worded for *any* snapshot change (set membership, body edit,
grant change), not just enable/disable, because the fingerprint covers all
of them. The `jobs.go` check guards only the narrow race between
`applyRemoteEgress`'s read and the enqueue transaction; it gets a distinct
sentinel (`ErrEgressSkillSnapshotChanged`) plus a new arm in
`mapEnqueueError` (`service/chat/service.go`) — without that arm the
sentinel collapses into the generic "decision is invalid" status and the
wording never reaches a client. Both files are in the build list. One
pre-existing behavior inherited and accepted: the fingerprint is computed
and bound even for MCP-only local runs (harmless over-binding — a skill
edit refuses a send that carries no skills); the chosen wording reads
correctly there too, and unscoping it is deliberately out of scope.

**The run notice names them; the audit row does not change.** The
in-transcript notice is built at enqueue from the snapshot read the job
freezes — same read, same names. The `egress.consent.recorded` audit row
keeps its current typed payload (fingerprint included); adding names there
would touch the audit payload allowlist for data the event stream already
preserves. **No schema change** — the decision row keeps
`skill_snapshot_fingerprint` as its durable record, deliberately: a
`skills_json` column would mean another `run_egress_decisions`
rename-copy-drop and a migration-number race with the in-flight
integrations implementation.

**A change between prepare and send is refused by the existing mechanism,
now legibly and now tested.** Where users meet it: the client's retry path
reuses a stored consent, so a stale challenge after a skill change surfaces
this message on retry — the wording above is what they read.

**Scope refusals.** No per-skill consent toggle (the per-run decision is the
consent unit; disabling a skill is the existing, honest control). No change
to *what* rides — this plan discloses existing behavior. No automations
work (they cannot egress today; separate track). No new "compact layout"
requirement — the dialog is an `AlertDialog` capped at 520px whose content
already scrolls (`SingleChildScrollView`); the skill list renders inside
that existing envelope. Skill names are local data in a local dialog;
nothing new leaves the machine because of this plan.

## What gets built

- **`proto/turing/v1/common.proto`** — `SkillEgressDisclosure`,
  `skills = 11` on `RemoteEgressDisclosure`. Pinned toolchain, regenerate,
  commit `gen/` and `turing-client/turing_app/lib/generated/`. The proto
  contract test (`turing-backend/tests/proto_contract_test.go` pins
  `RemoteEgressDisclosure` fields 1–10 by number/kind) gains the new
  entries — `tools/proto/check.sh` is a different guard and does not cover
  this.
- **`internal/repository/egress.go`** — `EgressSkillSnapshotFingerprint`
  generalizes to return `(fingerprint, []SkillEgressInfo)` from one
  snapshot read; info carries `SkillID`, flattened/capped display name, and
  `!Withheld`.
- **`internal/service/chat/egress.go`** — the two-conjunct category
  condition; disclosure population keyed on the category; the skills-list
  bound; the specific mismatch wording in the context-changed path.
- **`internal/service/chat/service.go`** — the `mapEnqueueError` arm for
  the new sentinel.
- **`agent-runtime-go/internal/agent/egress.go`** — `required` conditions
  `SKILL_CONTENT` on `providerRemote && len(job.GetSkills()) > 0`.
- **`internal/repository/jobs.go`** — skill names in the run notice
  (keyed on the decision's categories, truncated at 8 + "+N more");
  `ErrEgressSkillSnapshotChanged` at the enqueue-window check.
- **Client** — `remote_egress_dialog.dart` renders the skill list with the
  may-be-sent tags; `models/remote_egress.dart` + `grpc_client.dart`
  mapping.
- **Existing fixtures** — a real, budgeted cost, not collateral: at least
  `agent-runtime-go/internal/agent/external_agent_test.go` (`routedJob`,
  `authorizeDirectRemoteJob`), `egress_mcp_test.go`,
  `service/runtime/service_test.go`, and `service/chat/egress_test.go`'s
  exact-category assertions all encode the unconditional rule and must
  move to the conditional one.

## The tests that gate the merge

For 1–5, break the production gate, watch the right test fail, restore.

1. **Preimage coverage, per displayed field.** For each of `SkillID`,
   `Name`, `Withheld`: mutate it in a snapshot, assert the fingerprint
   changes. This is the test that catches a future disclosure field added
   outside the preimage. Companion, repository-level: one call returns
   names and fingerprint describing the same snapshot generation (edit the
   file, call again, assert both moved together — no fake seam needed).
2. **The category tells the truth, in both modules and both directions.**
   Zero enabled skills ⇒ no `SKILL_CONTENT`, empty `skills` list, and the
   run executes end to end **through the agent's `Execute`** (the leg that
   fails if only the orchestrator moved). Skills enabled ⇒ category
   present, list non-empty, run executes. A decision claiming the category
   with an empty job snapshot ⇒ refused by the runtime; **a decision
   omitting the category while the job carries a non-empty snapshot ⇒
   refused by the runtime** — the undisclosed-content direction is the one
   that matters most. And the divergence case: enabled skills + local
   Ollama + remote MCP tool ⇒ no `SKILL_CONTENT`, no skill names in
   disclosure or notice (the scenario
   `TestLocalRemoteMCPConsentNoticeAndAuditNameDestination` covers with
   zero skills, re-run with skills). Plus: one *enabled but unparseable*
   skill and nothing else ⇒ snapshot empty ⇒ no category — the leg that
   kills gating on `skill_settings.enabled` instead of snapshot content.
3. **`body_may_be_sent` is the snapshot's bit.** All-granted ⇒ true; one
   grant revoked ⇒ false; a skill declaring **zero** capabilities ⇒ true;
   and the stale-scope case that kills a parallel grant query: widen a
   skill's `requires:` after granting, don't reconcile — the snapshot
   withholds, and the disclosure must say so.
4. **Drift is refused, legibly, end to end.** Through `SendMessage` (not a
   bare repository call — the repository-only variant passes without the
   service-layer wording ever existing): prepare, then enable a skill —
   and separately disable one, edit a body, and revoke a grant — then
   send ⇒ refused with the skill-snapshot-changed wording from
   `applyRemoteEgress`. The enqueue-window sentinel maps through
   `mapEnqueueError` to a distinguishable status. Pure-function leg: two
   snapshots differing only in set membership produce different
   fingerprints (the existing `context_test.go` pins a *content* change;
   membership is the new leg).
5. **The notice names them, keyed on the category.** A consented remote run
   with skills lists their names in the notice; a remote run with zero
   skills has no skill line; an MCP-only local run with enabled skills has
   no skill line; twelve skills render eight names + "+4 more".
6. **Bounds and sanitization hold.** A 257th enabled skill refuses at
   prepare with the legible message; a name with newlines and 300 runes
   arrives flattened and capped in the disclosure — asserted in Go against
   the server-side value, not in Flutter.
7. **The dialog renders the distinction.** Flutter: one may-be-sent and one
   metadata-only skill render with correct tags inside the existing
   scrolling dialog.

## Documentation the implementation PR must update

- `docs/architecture/remote-egress-policy.md` — the skill-content category
  documented as conditional and name-carrying; the ceiling semantics ("may
  be sent" — index always, body/references on invocation) restated where
  the category is described; one sentence on the fingerprint being the
  binding and the names being its legible face.
- `docs/VISION.md` — no invariant changes; if the egress section
  paraphrases the disclosure contents, the paraphrase gains the names.
