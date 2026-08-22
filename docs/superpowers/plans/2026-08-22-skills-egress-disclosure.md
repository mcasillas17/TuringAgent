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

**And the runtime *binary* skew is handled by a version bump, because the
default configuration otherwise breaks.** Dispatch gates on
`capabilities.RemoteEgressDecisionVersion >= RunEgressDecisionVersion`;
without a bump, a stale runtime still counts as egress-aware, still
enforces the *unconditional* required set, and refuses **every remote run
with zero enabled skills** — the common case — with a baffling
missing-category error. `backendegress.DecisionVersion` is therefore
bumped — **and it does not travel alone**: the signed payload's version
comes from a *second* constant, `egressChallengeVersion` in
`service/chat/egress.go`, and `normalizePendingEgressDecision` requires
the payload's version to equal `RunEgressDecisionVersion` — bump one
constant without the other and **every remote send fails**. Both move in
lockstep, named here so neither is discovered in production. A stale
runtime then stops being egress-aware for new decisions and fails closed
at dispatch (it cannot claim the run) until upgraded — the same
restart-both-sides story every compose deployment already has; the
mirror direction fails closed symmetrically. Two knock-on consequences,
stated: a consent **prepared before the upgrade** fails
`validChallengePayload` after it and surfaces "remote egress challenge is
invalid" — not the drift wording — so the retry-path claim below holds
for same-version challenges only, and a user upgrading mid-conversation
re-prepares once; and the queued-job skew above is now refused by the
runtime's *version equality* check before the category set is ever
compared — same fail-closed outcome, different error string, which
matters to whoever writes the expected-message assertion. One production
string also moves: the routing-unavailable detail hardcodes
"remote egress decision v1".

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
is the trap, and the divergence case is precise — get it wrong and the
anti-trap test tests nothing: grant scope encodes
`Revision + requires-list`, and `classifyGrantScope` **drops** a grant as
stale when the scope string changed while the requires list stayed
identical — i.e. **a body/file edit with `requires:` unchanged** (a
*widened* `requires:` classifies as Refresh and is kept, and diverges
nothing: both computations see the new capability ungranted). Prepare
classifies without reconciling (`enabledSkillSnapshotsReadOnlyTx`), while
the naive `SELECT` is only correct after `reconcileSkillsTx` has deleted
stale rows — so at prepare, after a body edit, the snapshot withholds and
the naive query says fully granted. That is the live divergence. And a
skill declaring zero capabilities must trivially read as not withheld. The proto
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

**Skill names are untrusted strings entering two consent surfaces — one
shared sanitizer, in the shared package, on both paths.** Skill content is
untrusted prompt material (CLAUDE.md), and a name can carry newlines,
absurd length, or format/direction-control characters into the dialog that
gates egress and the transcript notice that records it. The runtime's
`oneLine()` is the precedent but **not sufficient for the named threat**
(`strings.Fields` splits on whitespace only — RLO, bidi isolates, and
zero-width characters are category Cf, not whitespace, and survive; a name
of only zero-width characters even renders as a blank consent row), and it
is **not importable anyway**: it lives under `agent-runtime-go/internal/`,
which `orchestrator-go` cannot reach. The sanitizer therefore lives in
`turing-backend/internal/egress` (the shared package both modules already
import): whitespace-flatten, strip `unicode.Cf` and control runes, cap at
`maxSkillDisplayNameRunes = 80` with ellipsis on a **rune** boundary (a
byte-based cap can split a multibyte rune and produce invalid UTF-8 in a
proto string field, which fails marshaling of the entire prepare
response), and — the clause whose omission would restate the very bug
this paragraph cites — **when sanitizing leaves an empty string, fall
back to `sanitize(skill_id)`, and to the literal `"(unnamed)"` if that is
also empty — where "empty" includes a result consisting solely of path
separators**: a skill id always contains `/`, which is neither
whitespace, Cf, nor a control rune, so without the separators-only rule a
Cf-named folder pair sanitizes to the puzzling display name `"/"` and the
`"(unnamed)"` terminal is unreachable on the real path. The id is *not* inherently safe: `skillID` validation
constrains path *shape* only (no `.`/`..`, exactly one separator), never
rune content, and the id derives from directory names on a
read/write-mounted `/skills` — a folder named in zero-width or RLO runes
produces an id carrying exactly what the sanitizer strips, reproducing
the blank consent row one level down. The `oneLine` precedent's
`"(unnamed)"` terminal exists for exactly this reason and must not be
silently dropped. The sanitizer is
applied on **both surfaces** (both in orchestrator-go — the runtime's
`oneLine` is precedent, not a call site, and stays untouched): the
disclosure (`SkillEgressInfo`) and the skill-name line this plan adds to
the run notice — the notice code is new, and it uses the same helper or
an unflattened untrusted string lands in the transcript event payload
while every test passes. Reachability,
stated correctly: the loader caps name *length* at 120 runes but not
*content* — `strings.TrimSpace` does not strip Cf runes, so a name made
of zero-width or direction-control characters parses fine and reaches
the disclosure end to end; only the over-120-rune inputs are confined to
unit territory. The
list itself is bounded: `maxEgressSkills = 256` (the `maxEgressTools`
mold), refused at `resolveEgressContext` with a legible
`FailedPrecondition` naming the remedy (disable skills) — **conditioned on
`providerEgress`**, matching the condition that populates the list, so an
MCP-only local run with 257 enabled skills is not refused over a list that
is empty by construction. The run-notice line truncates at 8 names with
"+N more"; the disclosure's structured list is **never** truncated below
the 256 bound — the two policies are different on purpose and tested as
different.

**The legible refusal lives where users actually hit it —
`service/chat/egress.go`, not primarily `jobs.go`.** The common drift case
(skill enabled, disabled, edited, or a grant changed between prepare and
send) is caught by `applyRemoteEgress` → `payloadMatchesEgressContext`,
today's message "remote egress context changed; prepare the send again" —
`SendMessage` returns there and `EnqueueUserMessage` is never reached. That
message gains the specific cause when the fingerprint is the mismatched
field — mechanically: `payloadMatchesEgressContext` returns a bare `bool`
and stays that way; the call site adds a separate
`payload.SkillSnapshotFingerprint != resolved.SkillSnapshotFingerprint`
comparison to pick the wording — **guarded by `resolved != nil`**, because
the branch it joins is `resolved == nil || !payloadMatches...`, and
`resolved` is nil on a live path (a remote prepare followed by a send
that stopped being an egress run), which keeps the generic wording — "the skill snapshot changed since consent
was prepared; prepare the send again", worded for *any* snapshot change
(set membership, body edit, grant change) because the fingerprint covers
all of them. Non-skill drift (tools, endpoints, categories) keeps the
existing generic wording, and a test pins that boundary — an
implementation that swaps the skill message onto every mismatch mislabels
every kind of drift. The `jobs.go` check guards only the narrow race between
`applyRemoteEgress`'s read and the enqueue transaction; it gets a distinct
sentinel (`ErrEgressSkillSnapshotChanged`) plus a new arm in
`mapEnqueueError` (`service/chat/service.go`). The sentinel **wraps**
`ErrEgressDecisionInvalid` (`fmt.Errorf("...%w...")`), so the existing
`errors.Is` arm still matches and the status class stays
`FailedPrecondition` even if the new arm is ever dropped — a bare
`errors.New` sentinel with no arm would degrade to the `Internal`
catch-all, which is worse than today. The new arm checks the specific
sentinel before the general one. Stated for the test-writer: that window is
single-threaded with no seam, so it is **not reachable through
`SendMessage`** — it is pinned by two direct unit assertions (the
repository check returns the sentinel; `mapEnqueueError` maps the sentinel
to its distinguishable status), not by hunting for an interleaving that
does not exist. Both files are in the build list. One
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
- **`turing-backend/internal/egress`** — the shared name sanitizer (both
  modules and both call sites use this one function); the
  `DecisionVersion` bump.
- **Existing fixtures** — a real, budgeted cost, not collateral:
  `agent-runtime-go/internal/agent/external_agent_test.go` (`routedJob`,
  `authorizeDirectRemoteJob`, **and
  `TestRemoteRunWithoutToolResultConsentDoesNotSendUnknownToolResult`,
  whose inline category list breaks deceptively** — its skills-free job
  fails the new exact-set check at shape validation, so the symptom is
  `len(remote.requests) == 0` reading as a tool-consent regression, not
  a category error) and `service/chat/egress_test.go`'s exact-category
  assertions encode the unconditional rule and must move to the
  conditional one — for the skills-free helpers, "move" means **drop
  `SKILL_CONTENT` from the fixture**, not make the helpers conditional;
  `TestRemoteRunRejectsSkillSnapshotFingerprintMismatch` looks like it
  should break but stays green either way (its fingerprint check
  precedes the category comparison); the two `repository/egress_test.go` callers of
  `EgressSkillSnapshotFingerprint`
  (`TestEgressSkillFingerprintMatchesEnqueueAfterSkillEditWithoutPrepareWrites`,
  `TestEgressSkillFingerprintTreatsNewUnreconciledSkillAsDisabled`) break
  on the signature change and are updated. (`egress_mcp_test.go` needs
  **no** repair — its local-provider decision never carried
  `SKILL_CONTENT`; it is the natural home for the new divergence test.
  `service/runtime/service_test.go`'s fixture *mentions* the category but
  nothing in that service composes or validates category sets — no change
  required.)
- **The version bump's fixture sweep** — the largest single cost in this
  plan, roughly fifteen sites in ten files, split by failure mode:
  decision-version literals (`repository/egress_test.go` — both the
  fixture `Version: 1` **and** its chained audit assertion
  `decisionVersion != float64(1)`; `mcp_egress_notice_test.go`;
  `mcp_egress_fingerprint_test.go`;
  `service/runtime/external_agent_mapping_test.go`; and
  `external_agent_test.go`'s two — where the version break fires *before*
  the category break, with a different message, so gate-breaking
  discipline reads a misleading signal if run naively) and
  worker-advertisement literals that silently un-egress-aware the test
  worker (`repository/job_routing_test.go`,
  `service/runtime/capabilities_test.go`, `capability_lifecycle_test.go`,
  `service/chat/service_test.go:~1010`, `tests/grpc_harness_test.go`).
  The repair pattern is the one the surviving fixtures already use:
  reference the constants symbolically
  (`repository.RunEgressDecisionVersion`, `backendegress.DecisionVersion`)
  instead of the literal `1` — with one named exception where that
  pattern is actively wrong: `service/audit/service_test.go`'s `!= 1` is
  asserted against a **hardcoded stored-payload JSON fixture** that
  nothing re-derives; it does not break on the bump and needs no repair.
- **The chat service test harness** — `newHarnessWithDatabase` never calls
  `SetSkillStore`, so no skill can be enabled in any `service/chat` test
  today; tests 2, 4, and 6's service-layer legs need the harness to gain
  the skill-store wiring the repository tests already have. And test 4's
  non-skill drift lever, named so nobody hunts: mutate the service's
  OpenAI base URL between prepare and send (the test file is in
  `package chat`; endpoint drift changes the resolved context without
  touching skills).

## The tests that gate the merge

For 1–6 and 8, break the production gate, watch the right test fail,
restore.

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
   disclosure or notice — the notice half extends
   `TestLocalRemoteMCPConsentNoticeAndAuditNameDestination`'s
   repository-level scenario with skills; the disclosure half needs a
   **service-layer** variant, since that test constructs no disclosure at
   all. Plus: one *enabled but unparseable*
   skill and nothing else ⇒ snapshot empty ⇒ no category — the leg that
   kills gating on `skill_settings.enabled` instead of snapshot content.
3. **`body_may_be_sent` is the snapshot's bit.** All-granted ⇒ true; one
   grant revoked ⇒ false; a skill declaring **zero** capabilities ⇒ true;
   and the stale-scope case that kills a parallel grant query — the case
   is a **body edit with `requires:` unchanged**, after granting, without
   reconciling: the snapshot classifies the grant stale and withholds,
   while the naive grant `SELECT` says fully granted; the disclosure must
   report `false`. (Widening `requires:` is *not* the kill-case — that
   classifies Refresh and both computations agree.)
4. **Drift is refused, legibly, end to end — and only skill drift gets the
   skill wording.** Through `SendMessage` (not a bare repository call —
   the repository-only variant passes without the service-layer wording
   ever existing): prepare, then enable a skill — and separately disable
   one, edit a body, and revoke a grant — then send ⇒ refused with the
   skill-snapshot-changed wording from `applyRemoteEgress`. The negative
   boundary: change something *other* than skills between prepare and
   send ⇒ the existing generic context-changed wording, not the skill
   message. And the nil leg, with its lever named because every obvious
   one is blocked by the request digest firing first: prepare with the
   session **bound to an external agent** (request provider `ollama`, so
   routing supplies the remoteness), then unbind the agent before the
   send — the digest does not cover the session's agent binding, so this
   is the input that flips `providerEgress` without tripping the
   digest-mismatch check, reaching `resolved == nil` with the generic
   wording and no panic. The enqueue-window sentinel is pinned by its two direct unit
   assertions (repository returns it; `mapEnqueueError` maps it) — that
   window has no `SendMessage`-reachable seam. Pure-function leg: two
   snapshots differing only in set membership produce different
   fingerprints (the existing `context_test.go` pins a *content* change;
   membership is the new leg).
5. **The notice names them, keyed on the category — and the two truncation
   policies stay distinct.** A consented remote run with skills lists
   their names in the notice; a remote run with zero skills has no skill
   line; an MCP-only local run with enabled skills has no skill line;
   twelve skills render eight notice names + "+4 more" **while the same
   run's disclosure `skills` list carries all twelve** — the leg that
   catches the notice's truncation leaking into the structured list.
6. **Bounds and sanitization hold, on both surfaces.** A 257th enabled
   skill refuses at prepare with the legible message — and an MCP-only
   local run with 257 skills is *not* refused; a ~100-rune name with
   embedded newlines arrives flattened and capped end to end in **both**
   the disclosure and the notice; **a zero-width-only name arrives end to
   end as the `skill_id` fallback, never a blank row** (Cf content passes
   the loader — only over-length inputs don't); and the sanitizer unit
   tests cover 300 runes, RLO/bidi isolates, the empty-after-stripping
   fallback **including a Cf-runed skill *folder* name** (the leg that
   catches an unsanitized `skill_id` fallback — perturbing only the
   frontmatter name under a normal folder misses it), and a multibyte
   name truncated on a rune boundary (a byte cap that splits a rune
   breaks proto marshaling of the whole prepare response).
7. **The dialog renders the distinction — and the mapping actually maps.**
   Flutter: one may-be-sent and one metadata-only skill render with
   correct tags inside the existing scrolling dialog; and the
   proto→model mapping in `grpc_client.dart` is asserted to populate the
   skills list — the one line whose omission leaves backend and dialog
   both individually correct while the feature does nothing.
8. **Version skew fails closed at dispatch.** A worker advertising the
   **literal `1`** — not `RunEgressDecisionVersion - 1`, which stays
   green forever and merely re-tests the pre-existing legacy gate —
   cannot claim a run carrying a post-bump decision.

## Documentation the implementation PR must update

- `docs/architecture/remote-egress-policy.md` — the skill-content category
  documented as conditional and name-carrying; the ceiling semantics ("may
  be sent" — index always, body/references on invocation) restated where
  the category is described; one sentence on the fingerprint being the
  binding and the names being its legible face.
- `docs/VISION.md` — no invariant changes; if the egress section
  paraphrases the disclosure contents, the paraphrase gains the names.
