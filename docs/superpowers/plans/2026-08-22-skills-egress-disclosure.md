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
- The decision *is* already bound to the exact skill set:
  `EgressSkillSnapshotFingerprint` (`repository/egress.go:201`) fingerprints
  the enabled-skill snapshots inside a read-only transaction at prepare, the
  fingerprint rides inside the signed challenge payload
  (`payloadMatchesEgressContext` compares it), and enqueue re-derives the
  snapshot and refuses on mismatch — the regression test
  `TestEgressSkillFingerprintMatchesEnqueueAfterSkillEditWithoutPrepareWrites`
  already pins that a skill *edit* between prepare and send is detected. What
  is missing is purely that the human never sees the names behind the hash.
- Per the skills model (CLAUDE.md, PRs #55–#62): the enqueue snapshot carries
  *metadata* for every enabled skill, and *bodies only for skills whose
  declared capabilities are all granted*. The disclosure should preserve that
  distinction — "this skill's full text leaves" is a different fact from
  "this skill's name and description leave".

## Design decisions (locked)

**Names are derived, displayed, and bound by the fingerprint that already
exists — they do not join the signed payload.** Prepare derives the skill
list and the fingerprint from the *same* snapshot read
(`enabledSkillSnapshotsReadOnlyTx`); the signature binds the fingerprint;
enqueue re-derives and refuses on mismatch. Therefore what was named at
prepare is exactly what rides at send, or the send is refused — the guarantee
the integration endpoints needed the signed payload for comes free here,
because the fingerprint is a digest of the very object the names describe.
Threading a name list into `egressChallengePayload` would grow the 32 KiB
challenge for zero additional binding, and this plan refuses it. One
consequence stated plainly: the names in the *disclosure message* are not
themselves signature-covered, so their correctness rests on prepare deriving
names and fingerprint in one read — a single function returns both, and a
test asserts they come from one snapshot (not two reads that could straddle
a concurrent skill edit).

**`SKILL_CONTENT` becomes conditional, and every category-set mirror moves in
the same commit.** The category is attached only when the enabled-skill
snapshot is non-empty. The mirrors that must agree — this repo's egress
rules are duplicated per module, and #75's review history shows what happens
when one is missed:

- `resolveEgressContext` (`service/chat/egress.go`) — the attachment site;
- the runtime's `validateEgressDecisionShape`
  (`agent-runtime-go/internal/agent/egress.go`) — its exact-set check builds
  a `required` list that includes `SKILL_CONTENT` for provider runs; it must
  require the category **iff the job's skill snapshot is non-empty**, which
  the runtime can see directly on the job it is validating — no new plumbing;
- the challenge-payload structural validation, if it enumerates category
  expectations (verify at implementation; as of this writing it validates
  shape, not the category set's composition).

An empty-snapshot remote run therefore stops claiming skill content, and a
run whose decision claims skill content it does not carry is refused by the
runtime — in both directions, the disclosure only says what is true.

**The disclosure names skills with their body/metadata distinction.**
`RemoteEgressDisclosure` gains a repeated message (next free field number;
pinned, `tools/proto/check.sh` compares bytes):

```proto
message SkillEgressDisclosure {
  string skill_path = 1;     // the skill's identity: relative folder path
  string display_name = 2;
  bool body_included = 3;    // all declared capabilities granted
}
repeated SkillEgressDisclosure skills = 11;
```

`body_included` mirrors the enqueue rule exactly — computed by the same
grant check the snapshot builder uses, not a re-implementation. The dialog
renders the skill list under the existing "Enabled skill content" category
line: name plus a "full content" or "name and description only" tag. Compact
layout (<840px) included; a long skill list scrolls within the dialog rather
than growing it unboundedly.

**The run notice names them too, from the enqueue-time snapshot.** The
in-transcript notice and the consent audit trail are built at enqueue
(`enqueueUserMessageTx`), which already reads the snapshot it freezes into
the job — the names come from that read, not from a second query, so the
notice describes the snapshot that actually rode. **No schema change**: the
decision row keeps `skill_snapshot_fingerprint` as its durable record, and
the names live in the notice/event text. This is deliberate — a
`skills_json` column would mean another `run_egress_decisions`
rename-copy-drop and a migration-number race with the in-flight integrations
implementation, for data the event stream already preserves.

**A skill *enabled or disabled* between prepare and send is refused by the
existing mechanism, and now the refusal is legible.** Fingerprint mismatch
at enqueue already catches it (enable/disable changes the snapshot set, not
just an edit). The error message names the cause — "the enabled skill set
changed; prepare the send again" — instead of a bare mismatch.

**Scope refusals.** No per-skill consent toggle (the per-run decision is the
consent unit; per-skill opt-out inside a dialog is a policy editor wearing a
consent costume — disabling a skill is the existing, honest control, one
click away). No change to *what* rides — this plan discloses the existing
behavior; changing the grant rules is out of scope. No automations work
(they cannot egress; separate track). Skill *names* are user-authored local
data appearing in a local dialog — nothing new leaves the machine because of
this plan.

## What gets built

- **`proto/turing/v1/common.proto`** — `SkillEgressDisclosure`, `skills = 11`
  on `RemoteEgressDisclosure`. Pinned toolchain, regenerate, commit `gen/`
  and `turing-client/turing_app/lib/generated/`.
- **`internal/repository/egress.go`** — `EgressSkillSnapshotFingerprint`
  generalizes to return `(fingerprint, []SkillEgressInfo)` from one snapshot
  read; the info carries path, display name, and the grant-derived
  `body_included`.
- **`internal/service/chat/egress.go`** — conditional `SKILL_CONTENT`
  attachment; disclosure population.
- **`agent-runtime-go/internal/agent/egress.go`** — the `required` list's
  `SKILL_CONTENT` entry conditioned on the job's snapshot being non-empty.
- **`internal/repository/jobs.go`** — skill names in the run notice text,
  from the enqueue-time snapshot read; the legible fingerprint-mismatch
  message.
- **Client** — `remote_egress_dialog.dart` renders the skill list with the
  body/metadata tag; `models/remote_egress.dart` + `grpc_client.dart`
  mapping; compact layout.

## The tests that gate the merge

For 1–4, break the production gate, watch the right test fail, restore.

1. **Names and fingerprint from one read.** The disclosure's skill list and
   its fingerprint derive from a single snapshot read; the test interposes a
   skill edit between two hypothetical reads (fake the repository) and
   asserts the disclosure cannot mix a pre-edit list with a post-edit
   fingerprint or vice versa.
2. **The category tells the truth, in both modules.** Zero enabled skills ⇒
   no `SKILL_CONTENT` in the prepared categories and an empty `skills` list —
   and the run executes end to end through the agent's `Execute` (the
   runtime's exact-set mirror widened correctly — this is the leg that fails
   if only the orchestrator side changes). With skills enabled ⇒ category
   present, list non-empty, run executes. A decision claiming the category
   with an empty job snapshot ⇒ refused by the runtime.
3. **`body_included` mirrors the grant rule.** A skill with all capabilities
   granted shows `body_included = true`; revoke one grant ⇒ same skill shows
   `false`; the flag is computed by the same code path the snapshot builder
   uses (break the grant check once, both change together).
4. **Set changes are refused legibly.** Enable (and separately disable) a
   skill between prepare and send ⇒ refused with the skill-set-changed
   message, via the existing fingerprint comparison — and the pure-function
   leg: two snapshots differing only in one enabled skill produce different
   fingerprints (this is presumably already pinned; extend, don't duplicate).
5. **The notice names them.** The enqueue run notice for a consented remote
   run lists the snapshot's skill names; a run with no skills omits the
   skill line entirely rather than printing an empty list.
6. **The dialog renders the distinction.** Flutter: a disclosure with one
   body-included and one metadata-only skill renders both names with the
   correct tags, in the compact layout without overflow.

## Documentation the implementation PR must update

- `docs/architecture/remote-egress-policy.md` — the skill-content category
  documented as conditional and name-carrying; one sentence on the
  fingerprint being the binding and the names being its legible face.
- `docs/VISION.md` — no invariant changes; if the egress section paraphrases
  the disclosure contents, the paraphrase gains the names.
