# DeepSeek Super-agent adoption for Turing: design

**Status:** proposed; panel review rounds 1 to 96 are done: round 96
returned no findings from any of the four reviewers, so the implementation
review has converged, and the final-phase round 97 reviews this documented
state. Phase 1 (nine candidate features, F1 to F9) and Phase 2 (fifteen
candidates, F10 to F24, from the GPT 6 Astra and Claude Sonnet 5.5 gap
analysis) are unified: fourteen Phase 2 candidates are adopted or adapted
with a unified plan, and one (F13) is rejected with a reason. This revision
carries the round 95 fixes.

**Date:** 2026-11-01.

**Source:** [deepseek-ai/dsh-superagent-prototype](https://github.com/deepseek-ai/dsh-superagent-prototype)
at commit `6337fc823ce20251c8de8085948165c81aaa701e` (MIT License). Every
DeepSeek citation below is a primary file in that repository at that commit,
written `dsh:path:lines`. Secondary reporting is not used as evidence.

**Roadmap:** proposes **TUR-026**, **TUR-027**, **TUR-028** and **UX-002**
(Workstream A), **INT-009** (Workstream G) and **PRO-001** (Workstream I), and
adds acceptance to OPS-002, OPS-003, TUR-014, TUR-016, TUR-022, TUR-024,
EVAL-001, AGT-000, AGT-002, XTOOL-001, CHN-000, CHN-002 and PRS-001 in the
[canonical roadmap](../../NORTH_STAR.md). Each of those items in
`docs/NORTH_STAR.md` carries a pointer to this document: a proposed item for
the new IDs, one added acceptance sentence for the amended ones (plus, for
TUR-016, a note that TUR-016a has no dependency and that TUR-016c refuses
encrypted backups until TUR-022 defines a key channel, which TUR-022's
sentence records). Nothing here is shipped.

**Citation conventions:** `orch/` means `turing-backend/orchestrator-go/internal/`,
and `agent-runtime-go/` means `turing-backend/agent-runtime-go/`. Other Turing
paths are relative to the repository root, inspected at `310dbb36`; the Phase 2
citations were checked at `228b4d04`, which changes no code under
`turing-backend/` or `proto/` since `310dbb36`.

## 1. Goal

The owner asked for a source-verified plan for bringing the valuable features
of DeepSeek's Super-agent prototype into Turing, with performance and
security/privacy as the top priorities for every feature. The request, as
normalized by the host:

> 1. Each panel agent independently reviews each candidate feature in the
>    DeepSeek repo and drafts a plan to bring it into Turing.
> 2. All agents converge on one unified plan per feature.
> 3. Then GPT 6 Astra and Claude Sonnet 5.5 read the DeepSeek repo in detail to
>    find further features worth bringing; the process repeats for those; the
>    plan is updated.

This document is step 2 for the nine Phase 1 candidates and step 3 for the
fifteen Phase 2 candidates (sections 13 to 26, with F13 in section 28),
revised after each panel review round; the Status line above and section 29
say which rounds are done.

### Assumptions

- Turing's owner decisions and permanent invariants are binding
  (`docs/NORTH_STAR.md:49-68`, `docs/NORTH_STAR.md:269-329`). Where DeepSeek's
  design conflicts with them, the invariant wins and the conflict is recorded.
- The reference machine is the 24 GB Mac mini running `qwen2.5:7b` on Ollama
  (`docs/superpowers/specs/2026-10-25-turing-local-inference-engines-design.md:252`),
  with `TURING_MAX_CONCURRENT_RUNS_GENERAL=1` (`orch/config/config.go:341-346`).
  Any feature that spends a model call competes with the owner's own turn.
- DeepSeek is a repository-coding agent for a team. Turing is a single-user
  home hub. A feature is adopted for what it does, not for its domain.
- Nothing here changes product code. Each slice below is its own pull request
  with its own tests.

### Corrections to the research report

Found while verifying the drafts; each is relied on below.

1. `danger-full-access` is set in code, `dsh:src/app/sessions.ts:152`, and
   disclosed in `dsh:docs/006-deployment.md:19`. `cordis.patch.yml` selects
   presets, not that permission.
2. DeepSeek's router ("main Session") is not read-only. Its preset removes file
   write and edit (`dsh:cordis.patch.yml:163-167`) but keeps `tool-bash`
   (`dsh:cordis.patch.yml:139-160`), and the proactive assessment runs inside
   that Session (`dsh:src/app/assessment.ts:85-89`;
   `dsh:prompts/admission.md:1-3`). "Assessment is read-only" is a prompt
   instruction.
3. `observe` is not free: the model assesses the candidate, and only then does
   `assessmentOutcome` downgrade an inferred accept to `would_accept`
   (`dsh:src/policy.ts:40-60`; `dsh:docs/002-authorization.md:98`).
4. The exhaustive-judgement check proves coverage, not truth. It rejects
   missing, unknown, disabled or repeated rule IDs (`dsh:src/policy.ts:42-46`),
   and the prompt itself says the declarations are semantic judgments, not
   proof (`dsh:prompts/admission.md:36-39`).
5. After a handoff, DeepSeek's shell jobs outlive the 30-minute grace and hold
   no slot; only subagents stop (`dsh:docs/003-execution.md:35-39`).
6. DeepSeek re-reads prompt versions on every model request, including later
   steps of the same turn (`dsh:docs/004-prompts.md:95`;
   `dsh:src/app/sessions.ts:328-338`). Turing freezes content per run and keeps
   it that way.
7. Role `Pick<>` types are compile-time TypeScript over one shared Engine
   object (`dsh:src/core/engine.ts:260-270`), not a runtime boundary.

## 2. Summary

| Feature | Verdict | Turing roadmap item | Size | Top security point | Top performance point |
| --- | --- | --- | --- | --- | --- |
| F1 Deterministic admission gate + accept modes | Adapt | PRO-001 (new) | M + M + M + M + S + S + M + L, plus M for `auto` (PRO-001d, with F3; section 7) | Third-party events are inferred at most; refusals store no content and run no model | Gate is pure Go: zero model calls in every mode; pages are stream-decoded, one element, a page's kept records and one repository's thread labels (about 10.5 MiB a poll) in memory |
| F2 Mandate (immutable grant, revalidated) | Adapt: no Mandate entity; narrow-only revalidation of frozen grants, and the allowlist as a ceiling | TUR-026 (new) | S + S + S + S | Closes four verified gaps, including safe tools outside an automation's allowlist running unattended, checked at the beacon and at registered-MCP dispatch (section 4, point 5) | Primary-key reads per unattended tool call or delegation claim; an attended run pays one `automation_runs` read per tool beacon and one per registered-MCP call |
| F3 Proactive rule library | Adapt: owner-authored event automations; exhaustive local assessor optional, and it only ever proposes | PRO-001 | L (rules) + L (assessor) | Assessor has an enforced empty tool set and no roster, so every tool and team gate refuses it before reading a server, the vault or a profile; it is local, memory-less, checked against the current text, thread and rules at claim and at result, and its hidden session is withdrawn once its result is recorded; a Start-confirmed turn is local and refuses any tool that is not confined (a bundled server's, or the runtime's own skill loaders) | One bounded local call per batch, never per rule, never claimed ahead of the owner and preempted by an owner turn, with a per-job output cap |
| F4 Subscriptions / follow-up fan-out | Reject the mechanism; keep three rules | PRO-001, CHN-002 acceptance | — | No ungated fan-out into running work; no resume under old consent | One primary-key probe per terminal transition; no subscription polling |
| F5 "Needs a person" view + stage table | Adopt | UX-002 (new) | M + M | Read-only, owner-only, no arguments or content | One bounded read per open or hint; push, no polling |
| F6 Worker slots and background work | Adapt: attended work claims first; reject the grace period and the extra slot | TUR-027 (new); AGT-000, MCPP-005 inputs | S | Order changes, eligibility never does | Two index-ordered `LIMIT 1` probes per claim; no sort |
| F7 Credential shims + environment scrubbing | Reject the mechanism; adopt as fixtures | XTOOL-001 acceptance | S (doc) | Allowlist-built environments; shims are not a boundary | None |
| F8 Versioned prompt/profile overrides | Adapt narrowly: compare-and-set saves and "Default updated" for seeded profiles | OPS-003 acceptance; OPS-001 command surface | S + S | Upgrades never write a user's file or gain authority silently | Hashing on demand; nothing on the turn |
| F9 Role-scoped interfaces | Reject: Turing already enforces it at runtime | None | — | Per-method identity allowlists beat type narrowing | None |
| F10 Source-event identity, edits and failure-isolated intake | Adapt, into PRO-001a | PRO-001 | +S | Ids namespaced by the numeric repository id resolved when a repository is added, which addresses every later request, so a reused old name never substitutes another repository, whose name is display text shown as last seen; listed actors are matched by numeric user id, so a reclaimed login is never a listed actor; an enable, disable or scope edit during a poll stops it with nothing written; an edit Turing reads replaces stored text and starts nothing; a detectably edited item never fires `auto` | One bad item or failing stream never stalls a watch; a slow page that keeps arriving completes; one short write transaction per page, opened after it arrives, and at most one rotation write per poll, none in the steady state, a stopped poll's only possible write besides a suspension; a poll ends within 10 minutes and the next starts at the repository after the last one it began, even when it was stopped |
| F11 Model-surface contract test with a size ratchet | Adopt | EVAL-001 (EVAL-001a) | S + S | Required data framings cannot be dropped silently | Every growth in per-turn prompt and schema bytes becomes a reviewed diff |
| F12 Compact, navigable GitHub reads | Adapt | INT-009 (new) | S + S | Every page repeats the run, selection, credential and egress gates; no model-supplied URL | Each request asks GitHub only for the rows one frame holds, and each page is fetched once, instead of a clipped prefix of raw JSON |
| F13 Typed owner-decision handoff | Reject now; measure abstention in EVAL-001 | None (section 28) | — | No model-authored row beside approvals | Nothing added |
| F14 Purpose-gated outward notifications | Adopt as acceptance; no code before a consumer | CHN-000, CHN-002, PRS-001, UX-002, EVAL-001 acceptance | S (doc) | No model chooses whether, where or why to notify | Zero eligibility model calls; fewer sends |
| F15 Refuse a database written by a newer build | Adopt | TUR-016 (TUR-016a) | S | Fail closed before any row, schema or skills write | One startup read |
| F16 Owner quiet switch | Adapt, narrowed: stops new unattended work, holds nothing | TUR-028 (new) | S + S | Only the owner sets it, and a write from a stale view is refused; enabling it cancels queued unattended runs; no clock moves | Zero polls and zero newly started unattended runs while quiet |
| F17 Sticky dismissal of watch proposals | Adapt, into PRO-001b | PRO-001 | +S | A dismissed thread stores no further text | Saves an assessor call per later event |
| F18 Dismissable attention rows | Adapt: occurrence-bound Dismiss, no time expiry | UX-002 | +S | Approvals, consents and deletions cannot be dismissed | One seek on UX-002a's partial index per block lookup, plus one column compare |
| F19 Health model | Adopt as acceptance | TUR-014 acceptance | S (doc) | Fixed codes, never peer text | In-memory; threshold stops flapping |
| F20 An uncertain GitHub comment | Adapt: write-ahead body hash, not a published marker; no read-back | PRO-001a | +S | Nothing is added to the owner's public comments; never replays; Turing never reads back, and the notice links the issue by the approved `owner/repo` | One row per comment; no request after an uncertain outcome |
| F21 Delivery-ledger semantics | Adopt as acceptance | TUR-024, CHN-002 acceptance | S (doc) | Typed `gone`, content-free drop records | One row per key |
| F22 Settings compare-and-set | Adopt as acceptance | OPS-002 acceptance | +S | A stale client cannot re-enable what the owner disabled | One integer compare per save |
| F23 Upgrade rehearsal on a restored copy | Adapt: one-off check container, not a second stack | TUR-016 (TUR-016c) | M | No network, no live mounts; the copy is removed on every exit, and one kept because Docker cannot confirm its container gone is named by every `./turing` command | Offline; nothing on the live stack |
| F24 Owner's words in delegation briefs | Measure first; frame only on failure | EVAL-001 scenario; AGT-002 follow-up | S (+S) | Only a direct owner message is copied, framed as data | ≤4 KiB more prefill per child, only if adopted |

## 3. F1 — Deterministic admission gate and accept modes

### Verdict

Adapt, as the entry contract of a new item, **PRO-001 — Observe-first
proactive intake**. Turing's existing originators (the desktop owner,
owner-authored automations, the agent team, and the planned channel and A2A
paths) keep the gates they have or already specify. The gate is pure Go and
spends no model call in any mode. Every watch starts disabled.

### DeepSeek source

- The gate is deterministic and separate from the model. A listed sender who
  addresses the agent is `explicit`; an admission-table cell, a follow-up or a
  listed bot yields `inferred`; everything else is not admitted
  (`dsh:docs/002-authorization.md:9-16,46-75`; `dsh:src/policy.ts:171-211`).
- Unlisted senders are ignored by default (`dsh:src/admission.ts:12-13`). Bots
  can be inferred at most (`dsh:src/policy.ts:171-189`;
  `dsh:docs/002-authorization.md:79`).
- Accept mode is `off | observe | auto`, default `observe`
  (`dsh:src/policy.ts:84`; `dsh:docs/002-authorization.md:91-101`). The mode is
  enforced in code (`dsh:src/policy.ts:55-58`) and acceptance is re-checked
  against the current Policy (`dsh:src/policy.ts:212`).
- The Policy version hashes rules, mode and source parts
  (`dsh:src/policy.ts:111-118`); a change discards running Evals
  (`dsh:docs/002-authorization.md:107-117`).
- Messages cannot change the gate (`dsh:docs/002-authorization.md:5`).
- The GitHub source polls the agent account's notifications and then reads
  each Issue or PR a notification names, one hydration request per subject
  (`dsh:src/sources/github.ts:297-342,344-374`). DeepSeek requires a separate
  agent account for this, because it reads that account's notifications
  (`dsh:docs/006-deployment.md:8,33`).
- Optional repository-wide streams read `/issues?state=all` and
  `/issues/comments` oldest first with `since`, and send the last ETag so an
  unchanged list returns 304 (`dsh:src/sources/github.ts:172-177,198-249`).
  Self-origin is the agent account's login
  (`dsh:src/sources/github.ts:227`).
- Issue intake is creation-only: the issue stream admits an issue only when it
  was created after its kind was switched on, so an edit or a label change
  that moves `updated_at` starts nothing, and nothing from before the switch is
  backfilled (`dsh:src/sources/github.ts:198-201,207-208,228-232`). DeepSeek
  says why: only the author can be the actor of the body
  (`dsh:src/sources/github.ts:352-353`). A comment's trigger id includes its
  `updated_at`, so an edited comment is a new trigger on the same comment
  (`dsh:src/sources/github-trigger.ts:66-68`).

### Turing today

- Every run has an explicit originator: the owner over the loopback client
  bearer, an owner-authored automation with an exact `(server, tool)`
  allowlist (`orch/repository/automations.go:100`;
  `docs/NORTH_STAR.md:298-299`), or an attended Turing turn that delegates.
- Planned non-owner paths already carry deterministic gates in their
  acceptance: unbound channel identities have zero capability and run no model
  (`docs/NORTH_STAR.md:289-290`; CHN-001), group chats are out of scope
  (CHN-000), and anonymous A2A peers get no capability (A2A-002).
- The GitHub integration exists, but every GitHub request is bound to a run
  that allows it (`orch/service/integrations/call.go:155-163`). Nothing watches
  an account and decides on its own whether to start work.
- The connection is the owner's own personal access token, and Turing tells
  the owner to prefer a fine-grained token scoped to chosen repositories
  (`orch/service/integrations/providers.go:40-45`). GitHub's "List
  notifications for the authenticated user" endpoint does not accept
  fine-grained personal access tokens (GitHub REST documentation for that
  endpoint), so notifications are unreachable with the recommended credential.
- Turing's only GitHub write is `github.create_comment`, through the owner's
  token (`orch/service/integrations/github.go:185-198`), so Turing's writes and
  the owner's own activity carry the same login.

### Unified design

1. **Scope.** The gate exists only on PRO-001's path. Desktop sends,
   approvals, `team.delegate` and scheduled automations do not pass through it.
   CHN-001's bound-owner direct messages are explicit by construction and have
   no inferred case, so channels gain nothing from this gate until a channel
   item adds a scope beyond direct messages, which this design does not.
2. **First source.** Per-repository GitHub issue and comment streams, read
   with the existing sealed credential, as DeepSeek's repository streams are:
   `GET /repositories/{repo_id}/issues?state=all&sort=updated&direction=asc&since=…`
   (issues and pull requests, with author and labels) and
   `GET /repositories/{repo_id}/issues/comments?sort=updated&direction=asc&since=…`
   (conversation comments, with author), each addressed by the stored
   numeric repository id (A scope edit, below), each sent with the stream's ETag
   when one is kept, so an unchanged list returns 304 (the cursor rule,
   below). The id route is GitHub's own: GitHub answers a request for a
   renamed repository's former name with a redirect to
   `/repositories/{id}`, and both list routes and the single-issue route a
   hydration uses answer under it (checked live while this design was
   reviewed: `/repositories/1725199/issues`, `/issues/comments` and
   `/issues/1` each returned `200`). Both work with a fine-grained token that
   can read issues and pull requests on the listed repositories. Notifications
   are not used: the endpoint refuses the token Turing recommends, and a
   notification carries neither actor nor labels, so each would need a
   hydration request. A comment's labels come from its thread in the same
   poll's issue page, which the poll reads before the comment stream (Poll
   deadline and slots, below; a new comment moves the thread's `updated_at`), or else
   from one `GET /repositories/{repo_id}/issues/{number}`, spent only on a
   comment not yet in the ledger and counted against the watch's budget of
   10 hydrations a poll (Performance). A comment that would need an 11th
   stops the poll before it, as the other budgets do: `since` never passes
   an unhydrated comment, and the poll is behind (below). Path predicates are not
   offered: each would cost a files request per pull request. A second source
   reuses the gate; no source plugin system is built.

   **What is an event.** An event is a creation: an issue or pull request
   opened, or a conversation comment posted. Its actor is the item's author,
   identified by its numeric account id (`user.id`; point 3, Listed
   actors), the only actor a snapshot can attest. An edit, a label
   change or a state change that moves an item's `updated_at` re-lists it, but
   it maps to the same event and is dropped as a duplicate, so no event is ever
   attributed to someone who did not author it. Labels are a predicate over the
   item's current labels at the poll, never an actor: "when I label an issue"
   is not offered, because the issue snapshot names the author, not whoever
   applied the label. For the same reason a label term never starts
   unattended work: a rule that names one only proposes (section 5, point 1).
   The ledger keeps the resource apart from the event:
   `resource_id` is `{repo_id}:issue:{number}` or `{repo_id}:comment:{id}`,
   where `repo_id` is GitHub's numeric repository id, resolved once when
   the repository is added and the address of every later request (A scope
   edit, below), because issue numbers repeat across the up to 10 repositories of one
   watch; `platform_event_id` is `{resource_id}:created` in this release. A
   later, separately reviewed release can admit edits as new events on the
   same resource without a migration, under one rule fixed now: an edit's
   actor is unknown, because anyone with write access to a repository can
   edit another person's comment (GitHub Docs, "Managing disruptive
   comments") and the REST comment object names only its author. An edit can
   therefore at most update an `observe` proposal; it never fires `auto` and
   is never the owner's act.

   **Edits replace stored text; they start nothing (F10).** This is the one
   statement of the rule. An event's `excerpt_revision` is the SHA-256 of
   the body it was read from after Bounded decoding's credential redaction
   (below), so a revision names exact text, not a time, and is never a
   digest over the token; it is cleared with the excerpt. Every
   comparison with it hashes the redacted body too; only point 4's
   Turing-authored match uses the body as decoded. An item already in `watch_events`
   that a stream re-lists has its stored excerpt replaced in place by the
   current text, in the poller's transaction, and its `excerpt_revision`
   updated, only when all of these hold: its excerpt is still held (not
   cleared), its event is less than 30 days old and not dismissed
   (section 19, point 1), and the redacted body's SHA-256 differs from the stored
   `excerpt_revision`, whatever the timestamps say (a label-only,
   state-only or title-only change writes nothing: the poller keeps no
   title, no predicate reads one and no thread session's title carries
   one, so a title never reaches a model). Otherwise
   the re-listing writes nothing, so an edit never writes text back into a
   row whose excerpt expired or was cleared after Dismiss. Nothing is re-gated
   and no event is created. The proposal's
   assessor-derived fields are cleared, because their evidence offsets point
   into the old text; deterministic matches never read text and stay. So an
   author who edits a secret out is not kept for 30 days in the old form, and
   one copy keeps one retention clock, which the edit does not restart.
   A replacement is one atomic update charged on its byte delta (the new
   excerpt's bytes minus the old one's) against the watch's 64 MiB excerpt
   budget (point 6). It leaves the event's stored label names as they were
   admitted (a label change writes nothing, above), so their bytes do not
   change and the excerpt's delta is the whole charge. When the new excerpt does not fit, the old one is
   cleared instead, with its `excerpt_revision`, label names and the
   proposal's excerpt-derived fields, as point 8 clears an expired one, so
   no text is kept; that still counts as a replacement for everything
   below (the event is marked `observe_only` and its pending fire is
   deleted), and `watches.excerpt_bytes` always equals the bytes held.
   Copies already written into a thread session follow that session's
   lifecycle (point 8). GitHub's `since` streams do not report deletions, so
   a deleted comment's excerpt lives until its 30-day sweep. The one
   exception to replacement is an edit made inside an interval the backlog
   clamp skips (below): it is never read, so its held excerpt keeps the
   earlier text until its 30-day sweep, and the clamp deletes the watch's
   pending fires instead. An edited text
   may not be its author's, so a replacement also sets the event's
   `observe_only` marker, which is never cleared, and in the same
   transaction deletes any `watch_pending_fires` row naming the event
   (section 6): a detected edit never fires `auto`, whatever its matches or an
   assessor later say, and `FireEventAutomation` refuses a marked event
   (section 7). **First read, one rule for both kinds:** an issue, pull
   request or comment first read with `updated_at` later than `created_at`
   is admitted with the marker already set. Any change before that read (to
   the body, a label, the state or, for an issue, its comments) moves
   `updated_at`, and may be an edit by anyone with write access to the
   repository; neither object can tell which change it was (the REST issue
   object has no body-edit time), so every such item is treated as edited.
   Equal timestamps prove nothing: they have one-second resolution, so an
   edit in the second an item was created leaves them equal. Turing cannot
   detect that edit, so the text it first reads with equal timestamps is
   attributed to the author as read, and `auto`'s acknowledgement
   (section 7) says so. Any change Turing does observe, a body whose
   redacted hash differs from the stored `excerpt_revision`, sets
   `observe_only` as above.

   **Cursor.** Each stream's `since` starts at the time the watch is enabled,
   recorded as `enabled_at`, and its state row keeps that start as
   `started_at`: nothing earlier is read and nothing is backfilled, and an
   item created before its stream's `started_at` is skipped before the
   gate. An item that is not in the ledger and was created more than 24 hours
   before the poll is skipped before the gate too and counted as `stale`, so
   an item re-listed by a later edit, label or comment is never gated again
   after its refusal receipt expires (point 6). This is the one rule for
   resuming, and the stale skip wins: after the owner's quiet switch
   (section 18) or a suspension, the poller resumes from its stored `since`,
   clamped only if the stream reaches `backlog` (below), and an item it
   reads that was created more than 24 hours before the poll is counted
   `stale` on the watch's status, so a quiet period or suspension longer
   than a day drops those events instead of replaying them, and an event it
   does gate more than 15 minutes after its creation never fires `auto`
   (section 7, "What never fires"). Disabling a
   watch drops its streams' cursors and ETags but keeps its scope entries
   (A scope edit, below); enabling it again stores the new
   `enabled_at` as each stream's `since` and `started_at`, so nothing that
   happened while it was disabled is ever seen. `enabled_at` only seeds
   cursors; the watch's `generation` fences polls (Stopping a poll, below).

   **A scope edit, stated once.** A save that adds a repository, at the
   watch's creation or later, first resolves its numeric id with one
   `GET /repos/{owner}/{repo}`, sent with no transaction open through the
   GitHub client's `NoRedirectClient` with the watch's connection's
   credential, read by `GetSealedGitHubCredential`
   (`orch/repository/integrations.go:87-104`), which refuses a connection
   that is not `connected`, and stores the id with the repository. Any
   answer but a `200` with an id and a `full_name` in GitHub's name
   charset (below), a redirect included (never followed), and
   any transport error refuses the whole save with a content-free reason,
   so no scope is ever partly saved. A save whose resolved ids repeat,
   within its own list or against a repository already in the scope (as
   `Owner/Repo` and `owner/repo` do), is refused the same way: the scope
   stays as it was, and no state row or cursor is touched, so one
   repository never has two sets of state rows. **Every later request is addressed by
   that id, stated once:** each request a poll sends, a stream page, a
   label hydration or the repository request below, is a
   `GET /repositories/{repo_id}` route (First source, above), and
   the `owner/repo` the owner typed is never sent again or shown after
   the save. **The display name, stated once:** the name shown for a
   watched repository, and the one a new thread session's title carries
   (point 7), is the last known full name of the repository the watch
   reads by id, the only name its scope entry stores (point 3); the client
   labels it "last seen", and a thread session's title keeps the last known
   name at its creation. The save stores the `full_name` of its id
   resolution's answer, not the typed name. Polls then refresh it by the
   id, first from issue objects: every issue object a poll already reads
   for the repository, an item on an id-addressed issue-stream page
   answered `200` or a label hydration's issue, carries `repository_url`,
   whose `{owner}/{repo}` is the name now (comments carry none), and the
   last one the poll reads supplies the name. The *repository request*, one
   unconditional `GET /repositories/{repo_id}` sent right after the
   repository's streams, is sent only when no issue object or successful
   repository request has checked the name in the last hour, and at most
   once per repository per poll; a `200` supplies its `full_name`. The hour
   runs from `name_checked_at`, which the poller keeps in memory only, zero
   after a restart, so a restart costs at most one more repository request
   per repository. A failed or unparseable repository request leaves
   `name_checked_at` and the stored name as they were, so it is retried on
   the next poll; it writes no row and counts toward no streak and resets
   none, because it gates only a display string. `name_checked_at` moves
   only once an observation is durable: after the transaction that writes a
   changed name commits, or, when the accepted name equals the stored one,
   once that answer is accepted and no write is needed. A `200` (or an
   issue object) whose name transaction rolls back or fails, for example
   because quiet, a disable or a generation change stopped the poll, leaves
   the timer like a failed request, so the next poll checks again. A name is accepted only
   in GitHub's name charset (an owner of letters, digits and hyphens; a
   repository of letters, digits, `.`, `-` and `_`) and written only when
   it changes: in that page's commit transaction, or, for the repository
   request, in one short transaction of its own that re-runs the
   per-request check as a state row's transaction does. Every repository
   request counts against GitHub's primary rate limit. It is a poll request
   like a page (the per-request check, the request deadlines, bounded
   decoding that keeps only `full_name`, and the classifier's `401` and
   rate-limit rules), but it belongs to no stream: any other failure, a
   redirect included, is a failed repository request as above. **How stale it can be:** the
   shown name can lag a rename by up to an hour after the last successful
   check plus one poll interval, and longer while repository requests fail
   or polls are skipped (quiet, a suspension, the rate-limit reserve,
   backoff, a slot wait or a deadline cut); inside that lag it is the
   repository's previous name, which another repository may by then hold,
   so it is shown as last seen, never as current. With issue activity, the
   next poll that reads an issue object shows the new name. A rename or a transfer leaves the id unchanged, while
   GitHub redirects a renamed repository's old name only until another
   repository is created at it (GitHub Docs, "Renaming a repository"), so
   a name-addressed poll could read a different repository under the
   stored id. Addressed by id, a rename never interrupts the watch, a
   reused old name never substitutes another repository's items, and a
   transfer to a place the token cannot read is answered `404`, an access
   failure. A redirect answering an id-addressed stream request is
   unexpected and suspends the watch (Security and privacy, Redirects). On an enabled watch, a
   scope edit that adds a repository creates that repository's state rows with `since` and
   `started_at` set to the edit's commit time, as an enable does, so
   nothing created before the add is gated or backfilled; on a disabled
   watch, the next enable seeds it with the others. A scope edit that
   removes a repository deletes its scope entry (its id and display name)
   and its state rows in
   the same transaction, so
   a later re-add starts fresh and never gates an item created while the
   repository was out of scope. Any scope edit also resets
   `last_repository_begun` to −1 and increments the watch's `generation` in
   that transaction, so the next poll starts at the first repository of
   the new list (Poll deadline and slots, below) and a poll in flight stops
   with nothing written (Stopping a poll, below).

   **Progress and failure isolation (F10).** This is the one statement of
   the cursor rule; every other section points here. Each `(watch,
   repository, stream)` has its own state row: `since`, `started_at`, the per-page
   validators below, two failure streaks, `failed_polls` and
   `access_failures` (Security and privacy, the failure classifier), a consecutive-behind
   count, the last status code, the last `skipped_interval`, and
   content-free counters (skipped items, behind polls). No page cursor is
   ever stored, and every poll starts at page 1. GitHub's `since` is
   exclusive: a list returns only items whose `updated_at` is strictly
   after it, at one-second resolution. The rule is DeepSeek's
   (`dsh:src/sources/github.ts:209-248`; `dsh:src/sources/poll.ts:15-16`),
   with paging added:
   - Each request is `sort=updated&direction=asc&per_page=100&since=<since>`;
     the page size is fixed.
   - **Paging.** Whenever a page is full (100 items), the same poll fetches
     the next page at the same `since`, until a short page or the poll's
     budget of 3 pages per stream (Performance). Page numbers live only
     inside that poll.
   - After a walk of one page, the poller stores `next = max(N − 5 s,
     since)`, where `N` is the newest `updated_at` processed. After a walk of
     several pages, it stores `next = max(since, min(N − 5 s, P − 1 s))`,
     where `N` is the newest `updated_at` processed across the poll's pages
     and `P` is the `updated_at` of the last item on page 1 that has a
     valid timestamp: items are ascending, so it is page 1's latest usable
     time, and a malformed item after it only makes `P` earlier, the safe
     direction. A page 1 that holds items but no valid timestamp is a
     malformed response (below). The five-second
     overlap re-reads late-indexed items, the unique index absorbs every
     re-read, and `since` never moves backward. The next poll reads again
     from page 1.
   - **No shift loss.** An item updated or deleted between two page requests
     of one poll shifts the items after it back by one, so an unread item
     can cross a page boundary into a page already read. Every such item
     sorts at or after page 1's last item (an item updated meanwhile moves
     to the end), so its `updated_at` is at least `P`, and the next poll,
     whose exclusive `since` is at most `P − 1 s`, reads it; the unique
     index absorbs the rest. The price is throughput: a walk of several
     pages moves `since` only past page 1, so a stream behind its budget
     gates fewer than 100 new items a poll after its first (at most 1,200 an
     hour at the default interval), and re-reads the rest as unique-index
     probes. A page 1 whose 100 items all share the second after `since`
     cannot move it (`P − 1 s = since`), as a burst of more than 300 items
     within five seconds cannot: such a page is re-read each poll (`304`s
     while unchanged), the items after it are still gated while the walk
     ends on a short page, and once more than the budget waits, the stream
     is behind.
   - **Progress, defined once.** A poll whose walk ended on a short page,
     answered `200` or `304`, made progress. A poll that stopped before a
     short page because of a budget (its 3 pages, the watch's 10 label
     hydrations, the watch's hourly receipt cap, point 6, or the 10-minute
     poll deadline, below) is *behind*,
     whether or not `since` moved, and so is a stream that deadline left
     unreached (Poll deadline and slots, below). After
     3 consecutive behind polls the stream is in `backlog`, shown on the
     watch's status, until a poll of it makes progress; a poll stopped by a rate limit, the
     connection's reserve or a failure leaves the count unchanged. So a
     stream that receives more than its budget (300 items) between polls
     catches up over several polls, and a burst that `since` cannot pass
     (above) stalls visibly instead of skipping anything silently.
   - **Clamp, only in `backlog`.** At the start of each poll of a stream
     in `backlog`, and of no other stream,
     `since = max(stored since, floor_hour(poll start − 24 h))`: the poll's
     start minus 24 hours, rounded down to the hour, as DeepSeek floors its
     horizon (`dsh:src/sources/github.ts:199-200,209-210`). A quiet or healthy
     stream is never clamped, so its request and validators stay stable and
     a quiet poll costs only `304`s. The floor moves once an hour, so the
     clamp moves `since` at most once an hour, and a stall ends between 24
     and 25 hours after it began. Turing's horizon is the 24-hour `stale`
     bound above, so the clamp passes no creation the gate could admit: an
     item last updated before the new `since` was created more than 24
     hours before the poll, so it is `stale` unless it is already in the
     ledger. When the clamp moves `since`, in the transaction of the poll's
     first page: every validator is dropped; the stream's state records a content-free
     `skipped_interval` (from the old `since` to the new one), shown on the
     watch's status, with no item count, because skipped items are never
     returned; and every `watch_pending_fires` row of that watch is deleted,
     because an edit inside the interval can no longer disarm one. An edit
     inside the interval, to an item already in the ledger, is not read, so
     a held excerpt keeps its earlier text until its 30-day sweep (the one
     exception to replacement, above), and the product says so beside the
     skipped interval.
   - When `since` did not move, the state row keeps one validator per page
     the poll requested (at most 3): that page's ETag, whether it was full,
     and, for page 1, `P`, so a `304` to page 1 still supplies it. A page's
     validator is stored only once every item on it was processed and
     committed: a poll stopped inside a page (by the hourly receipt cap, the
     hydration budget, the poll deadline or a failure) keeps no validator for that page or any later one, so the next
     identical request re-reads them with a `200` instead of a `304` that
     would skip the items left unread. Each ETag is sent only on the identical request (same `since`,
     same page). A `304` to a page that was full continues the walk to the
     next page with its own ETag; a `304` to a short page ends that stream's walk. The
     first poll after `since` moves sends no ETag and re-reads the five-second
     overlap with one `200` per page; from the next quiet poll on, a quiet
     stream answers only `304`s, one per page it walks (the quiet budget,
     Performance), while a later item on page 2 still changes
     page 2's answer. A `304` to the unchanged full page 1 alone would hide
     that item, which is how DeepSeek's stream stalls (section 13). When
     `since` changes, every validator is dropped. These validators are not a
     page cursor: every poll still starts at page 1.

   **Page transactions.** No transaction is open while a page downloads.
   The poller decodes the whole page first (below), sends any label
   hydrations its comments need, and only then gates the page's items and
   commits their rows, and with the stream's last page also its state row,
   in one short transaction, which on an `auto` watch also holds each
   direct fire the page's events cause (section 6, point 1). A stream's
   state row is written only when one of its fields changes, so a quiet
   `304` writes none. The transaction re-runs the per-request check on
   itself (Stopping a poll, below); a failed re-check, quiet or a pending
   drain included, rolls the page back and stops the poll. A page whose commit
   fails, other than a rollback by that re-check, is a failure of its
   stream for the poll (Security and privacy, the failure classifier),
   never progress and never answered. So a page that arrives slowly for minutes
   never holds the database's one connection (`orch/db/connection.go:47`),
   and an attended write waits only for that short commit. A restart
   mid-poll re-reads from the stored `since`.

   **Bounded decoding.** The poller never holds a page whole: it decodes
   each page with INT-009a's streamed decoder (section 15, point 1), one
   element at a time under the same 1 MiB per-element bound, and keeps from
   each element only the fields the gate and the predicates read, with its
   label names (from the element, or for a comment from its thread or its
   hydration) at most 20 KiB in total, plus the first 16 KiB of the body.
   **The credential, stated once:** while an element is held (at most
   1 MiB), every decoded string the poller keeps, body, label names,
   logins and the repository's `full_name` (from a save's id resolution,
   an issue's `repository_url` or the repository request), has the watch
   connection's credential replaced by as many newlines, section 15's length-preserving `redactCredential`
   (`orch/service/integrations/github.go:97-113`), before it is cut,
   matched, stored, framed into `userText` or read by the assessor (for a
   name, before the charset check, so a name that held the token fails it:
   a save is refused and a poll keeps the stored name), so the
   token the connection holds, pasted into an issue, reaches no row and no
   model. Only that token is recognized, as with section 15's tools and
   `redactCredential` today: Turing keeps no credential it has replaced,
   because a copy kept only to redact it would be one more secret at rest,
   so after a connection's credential is replaced, a re-read of text that
   quotes the old token stores it as written. Replacing a credential does
   not revoke it; a token that leaked is revoked at GitHub. Point 2's
   `excerpt_revision` is the SHA-256 of the redacted body, so no stored
   revision is a digest over the token; point 4's hash match alone is
   computed over the body as decoded, compared in memory and never
   stored. 20 KiB holds 100 names of 50 four-byte
   characters (20,000 bytes), INT-009a's worst-case label fixture
   (section 15) written in four-byte characters; an
   element whose label names pass it is refused `label_budget` with no
   text (point 4), never kept with a cut set, so no predicate matches
   labels it did not see. An
   element over the bound is INT-009a's typed error, which the poller
   treats as a malformed response (below). No real item reaches the bound:
   GitHub caps a body at 65,536 characters, at most 256 KiB. A page's kept
   records, at most 100 of about 36 KiB each (16 KiB of body and 20 KiB of
   label names), take about 3.6 MiB until its commit. A comment's labels
   come from its thread on the same poll's issue pages (point 1), so while
   it reads one repository the poll also keeps each issue's number and
   label names from that repository's issue pages: at most 3 pages, so 300
   entries of at most about 20 KiB, about 5.9 MiB, dropped when that
   repository's comment stream ends. So a poll's peak memory is one element
   in decode, the page's kept records and that map, about 10.5 MiB,
   whatever a page holds, and the 4 poll slots (Poll deadline and slots,
   below) bound the poller at about 42 MiB.

   **Request deadlines.** Poller requests do not use the GitHub client's
   30-second whole-request timeout
   (`orch/service/integrations/github.go:43`), which a worst-case page of
   100 maximum-size bodies, about 26 MiB (Performance), cannot meet below
   about 7.3 Mbit/s. Each poller request has three deadlines instead: the
   response headers must arrive within 30 seconds (the client's existing
   header timeout, `orch/service/integrations/github.go:34`), the request
   fails when no body bytes arrive for 30 seconds (an idle-read deadline),
   and no request runs longer than 5 minutes. A slow download that keeps
   making progress completes, with memory bounded by the streamed decoder
   (above); a worst-case page needs about 0.73 Mbit/s to finish within the
   5-minute ceiling.

   **Poll deadline and slots.** This is the one statement of the poll
   deadline and of the order in which a poll visits a watch's streams. A
   watch's poll sends its requests one at a time, stream by stream, and no
   poll runs longer than 10 minutes. A poll rotates by repository, never by
   stream. The repositories have a fixed order (as the watch lists them),
   and the watch row stores one integer, `last_repository_begun`: the index
   of the last repository a poll began (−1 before the first poll). Each
   poll starts at the repository after it,
   `(last_repository_begun + 1) mod R` for the watch's `R` repositories,
   wraps past the last, and reads each repository's issue stream before its
   comment stream. **The rotation write, stated once:** a poll begins
   repository k when the per-request check (Stopping a poll, below) passes
   for its first request there, and keeps the last repository it began in
   memory. The index is written at most once when a poll ends or stops,
   whether it finished, reached the deadline, failed or was stopped, and
   only when the value differs from the stored one: one short transaction
   that writes only that field of the watch row, and only while the
   watch's `generation` is the one the poll started with, so a poll never
   overwrites a scope edit's reset (Stopping a poll, below). So the next poll starts at
   repository k+1 even when this one is cut, stopped or fails inside k,
   the last repository it began, before it commits any page there, and a crash mid-poll writes nothing,
   so the next poll starts at the same repository as the crashed one. A
   poll that finishes every repository ends on the one before its start,
   which is the stored value once a first poll has written it, so a
   healthy watch keeps one order and its polls make no rotation write; a
   one-repository watch makes none after its first poll. The Performance
   section counts this write in the poller's transaction budget. A
   comment's thread is always looked up first on the same poll's issue
   page (the labels rule, above); a comment whose thread is not on that
   page still needs one hydration, as before. A poll
   stopped before its first request begins no repository and writes no
   rotation index and no state row. Every other poll begins at least one repository, and the start
   never passes a repository it has not begun, so every repository's issue
   stream gets its first request at least once in every `R` polls that
   begin one (at most 10), however slow the repositories before it are. A repository's comment stream is read only after its issue
   stream in the same poll, so a repository whose issue stream alone
   outlasts the deadline leaves its comment stream unreached; that stream
   is behind, as below, and shows `backlog` after 3 such polls. DeepSeek
   reads its streams in a fixed order with no poll deadline
   (`dsh:src/sources/github.ts:182-187`), so it cuts no stream and needs no
   rotation.
   A poll that reaches the deadline cancels its request and stops like a
   budget stop: the stream whose request it cancelled is *behind* (Progress,
   above), that request counts as no failure and resets no streak (Security
   and privacy, the failure classifier), its `since` moves only for the
   pages it committed (its state row is written in one short transaction of
   its own), and the validators of every page of it the poll did not commit
   are dropped. A stream the deadline left unreached is behind too and keeps its
   `since`, its validators and both streaks, so 3 such polls in a row show
   `backlog` on it. Without the deadline, a poll
   whose 80 requests (Performance) each ran to the 5-minute ceiling would
   hold its slot for about 6 hours 40 minutes. Due watches take one of 4 poll slots across the
   orchestrator, in FIFO order of due time, and a watch's next poll starts
   only after its current one ends. So a slot is held at most 10 minutes,
   and the k-th watch waiting for a slot starts within ceil(k/4) × 10
   minutes of becoming due, however slow the polls ahead of it are.

   **Stopping a poll.** Each poll runs under a context that the orchestrator
   cancels when the watch is disabled, begins deleting, is suspended or has
   its scope edited, or when quiet (TUR-028, section 18) is turned on. This
   is the one statement of the per-request check. **The generation fence,
   stated once:** the watch row carries an integer `generation`, which
   every enable, disable and scope edit increments in its own transaction
   (a deletion disables the watch, so it does too). A poll records
   `generation` when it starts. Before every request, page, hydration or repository request, the poll re-reads
   the watch row (enabled, not deleting, not suspended, `generation`
   unchanged), the two quiet keys (quiet off and no drain pending,
   section 18) and the watch's connection, with no transaction open, and
   each page's commit transaction, which also holds the page's direct fires
   (section 6, point 1), and each state row's own transaction re-run the
   whole check on that transaction before they write anything. The
   rotation write requires only `generation` unchanged; the suspension
   write has its own predicate (A stopped poll's writes, below).
   The connection check is the attended
   dispatch's own (`orch/service/integrations/call.go:127-163`): it runs
   the statement of `GetSealedGitHubCredential`, which fails for a missing
   connection, one not `connected` or one whose credential was destroyed
   (`orch/repository/integrations.go:87-104`), and requires the sealed
   ciphertext to be byte-identical to the one the poll started with.
   Before a request, with no transaction open, the poller calls
   `GetSealedGitHubCredential` itself. Inside a page transaction it runs
   the same statement on the open transaction, through a `rowQuerier`
   (`orch/repository/sandbox_artifacts.go:153-155`), as
   `requireChatSessionTx` takes one (`orch/repository/sessions.go:502`):
   `GetSealedGitHubCredential` queries `r.db`
   (`orch/repository/integrations.go:90`), and with the database's one
   connection (`orch/db/connection.go:47`) held by the open transaction,
   that call would wait on itself. So while a page transaction is open,
   nothing calls an `r.db` method or begins another transaction. The
   connection check is needed because revoking a connection destroys
   Turing's copy but cannot invalidate the token at GitHub
   (`orch/repository/integrations.go:270-298`), so a poll that already holds
   the plaintext could otherwise keep reading with it. If any check fails,
   or this poll has already received a `401` or a redirect on a stream
   request (Security and privacy), the poll stops: the request in flight is cancelled, no later request is
   sent, and a check that fails inside a page transaction rolls that page
   back. **A stopped poll's writes, stated once:** from the stop on, it
   commits no event, excerpt, receipt, page, state, cursor, name or ETag row, so
   each stream's `since`, ETags, streaks and event rows stay as they were.
   Its only possible writes are the rotation write, at most once and only
   when the index changes (Poll deadline and slots, above), and, when the
   stop's cause is a
   suspension (a `401`, a redirect on a stream request, or a revoked or
   changed connection),
   the suspension: one short transaction that suspends the watch with
   remedy `needs_person`, with the content-free cause
   `connection_unavailable` for a revoked or changed connection, because
   its source can no longer be read with the credential the owner enabled
   it with, and `unexpected_redirect` for a stream redirect (Security and privacy). **The suspension write's predicate, stated once:** it
   requires only `generation` unchanged, so the watch is still enabled and
   not deleting, with no connection check, and quiet does not block a
   suspension. So a `401` or a revoked connection that races the owner's
   disable or deletion writes nothing, neither the suspension nor the
   rotation write, and the watch reads `paused` (or deleting), with no
   `needs_person` and no attention row; a `401` that races quiet still suspends the watch,
   whose status then follows section 21's one status rule.
   A connection revoked between polls is found by the check
   before the next poll's first request, so that poll sends no request,
   begins no repository, writes no rotation index and commits only the
   suspension. A changed `generation` (an enable, a disable or a scope edit
   since the poll began, a repository removed and re-added included) is a
   stop at which the poll commits nothing at all, neither the rotation
   write nor a suspension: nothing from an interval the watch did not cover
   is admitted or fired, the cursors, validators and rotation reset that
   the edit or the re-enable stored stand, and later polls find any fault
   themselves. DeepSeek likewise
   checks before each stream and each item, and binds its requests to an
   abort signal (`dsh:src/sources/github.ts:154,183,214,226`).

   A malformed item (no id, author, timestamp or URL) is skipped and counted,
   and the cursor may pass it, so one bad item never pins a stream
   (`dsh:src/infra/commands.ts:42-47`). A malformed response (not a JSON
   array, unparseable JSON, more than the 100 elements requested, an
   element over 1 MiB, or a page 1 with items but no valid timestamp,
   Paging, above), a missed request deadline (not the poll deadline)
   and any other transport error, a redirect excepted (Security and
   privacy, and the hydration rule below), are
   transient failures, which the failure classifier (Security and privacy)
   handles: that stream fails this poll, and the other streams carry on, as in
   DeepSeek, which fails a stream whose response does not parse and counts
   the failed poll (`dsh:src/sources/github.ts:139-142,182-187,220`). A
   label hydration answered `404`, `410` or a redirect skips that comment,
   counts no failure and suspends nothing: a hydration's redirect is never
   followed and means the issue was transferred, which a collaborator can
   do, not that the watched repository's id stopped answering. Any other hydration error
   fails the stream for this poll.
3. **Watch.** An owner-authored record: source, scope (at most 10 repositories,
   each a scope entry holding only the repository's numeric id and its
   display name, as point 2 states; and the event kinds), actor scope (`listed` actors, default, or `anyone`),
   listed bots, an enable switch (default disabled), a mode, per-watch
   rate caps, `enabled_at`, `generation` and `last_repository_begun`
   (point 2). It is created and changed only
   from the native client's public facet. No model, tool or worker can write
   it.

   **Listed actors, stated once.** Each listed actor and listed bot, and
   each actor term in a rule's predicate (section 5, point 1), is a GitHub
   account bound by its numeric user id. The owner types a login; the save
   that adds it resolves it with one `GET /users/{login}`, under the
   repository resolution's rules (point 2, A scope edit): sent with no
   transaction open, through the `NoRedirectClient`, with the watch's
   connection's credential read by `GetSealedGitHubCredential`; any answer but a `200` with an id, a redirect
   included, and any transport error refuse the whole save with a
   content-free reason. The id is stored; the login is kept for display
   only and is never compared. A login is not an identity: GitHub lets an
   account change its login and frees the old one for any other account to
   claim (GitHub Docs, "Changing your GitHub username"). The gate, every
   predicate's actor term, the listed-bot check and every revalidation
   (`watchEventEligible`, section 4, point 2) compare the polled item's
   author `user.id` with the stored ids, so an account that claims a
   listed actor's former login is not listed, and the listed account is
   still admitted under its new login. An event stores its author's id,
   and the login it was read with for display (point 6).
4. **Gate.** `Admit(event, watch, thread) → admitted | refused{reason}`, a
   pure function with a table test. Inputs come from the verified poller and
   the rows it read (the watch, the thread's `watch_threads` row, and whether
   an `integration_writes` row matches), never from event text. Refusal reasons: watch disabled; scope not listed; actor
   not listed (when `listed`; by id, point 3); unlisted bot; **Turing-authored**; thread
   dismissed by the owner (`thread_dismissed`, section 19); duplicate
   `(watch, platform event id)`; rate cap (`rate_cap`) or excerpt budget
   (`excerpt_budget`) exhausted (point
   6); label names over 20 KiB (`label_budget`, point 2, Bounded
   decoding). A comment is
   Turing-authored when its id is one Turing created, or when its body's
   SHA-256 matches an `integration_writes` row with the same issue number in
   any state but `rejected`. The match uses neither the repository's name
   nor any connection, so neither a rename between a comment and the poll
   that reads it nor a comment posted through another connection (a second
   connection, or one re-created when its token was rotated) can defeat
   it; an identical body on the same issue number in another repository is
   refused too, the safe direction. Each `github.create_comment` writes that row, with the
   approved body's hash and state `pending`, in a transaction committed
   before the POST, and completes it after (section 22, point 1, lists its
   columns and states every transition).
   The rows keep the same 30-day retention as excerpts. A stranger who posts
   a byte-identical body on the same issue within that window is refused
   too, which is the safe direction. The owner's own activity is admitted like any
   listed author's, so an issue the owner opens can match a rule; refusing the
   connected login instead, as DeepSeek does with its separate agent account,
   would refuse the owner too, because Turing writes with the owner's token.
   Every PRO-001 event is `inferred` at most. There is no `explicit` cell and
   no "unlisted addressed is explicit" option (`dsh:docs/002-authorization.md:73`
   is rejected).
5. **Enable switch and mode, per watch.** A disabled watch (the default)
   polls nothing and records nothing. DeepSeek's `off` mode is not copied: the
   enable switch already means it. An enabled watch is in one of two modes.
   `observe` (the default) records a `would_start` proposal (section 5's rules
   decide matches). `auto` fires a run (section 7, slice PRO-001d). `anyone`
   actor scope allows `observe` only; `auto` requires `listed` actors, and the
   server refuses it unless its flag is on and the owner's acknowledgement is
   recorded (section 7).
6. **Ledger.** `watch_events` with `resource_id` and a unique
   `(watch_id, platform_event_id)`.
   A refused event stores a content-free receipt: ids, reason, time. Its only
   job is deduplication, which point 2's 24-hour `stale` rule bounds, so the
   sweep deletes receipts 24 hours after they are written. A watch writes at
   most 1,000 receipts per hour; when a poll reaches that cap, the poller
   stops that watch's streams for the rest of the hour with their cursors
   held, as it does for a rate limit, and that poll counts as behind
   (point 2), so a source that keeps it up shows `backlog`. So every
   gated event keeps its receipt for its whole dedup horizon, and a watch
   holds at most 25,000 receipts younger than 24 hours however noisy its
   source (the cap resets on the hour while a receipt lives 24 hours, so 24
   full hours and the current one can overlap) and at most 26,000 in all,
   because a receipt that has come due is deleted by the next maintenance
   tick, a minute at most, during which one more hour's cap can be
   written. Due selection is an index probe: PRO-001a6's migration
   adds a partial index led by the written time over refusal receipts and
   one led by the event time over admitted rows whose excerpt is still
   held, because admitted rows are never deleted and a scan would grow
   with the watch's history. Each admitted row also stores its thread key
   (the external thread's `{repo_id}:issue:{number}`, which point 2's
   thread lookup already resolves for a comment), and a third partial
   index on `(watch_id, thread_key, dismiss_generation)` over rows whose
   excerpt is still held drives the Dismiss sweep (section 19) and a
   deleting watch's excerpt clearing (point 8). An admitted event
   stores the metadata the gate and the predicates read (repository id,
   event kind, author id, with the author's login for display only,
   whether the author is a bot, and the label
   names at the poll, which are third-party text and are cleared with the
   excerpt, point 8) and a bounded excerpt, at most 16 KiB, cut on a UTF-8 boundary
   with a visible `[cut: N bytes not shown]` marker, its
   `excerpt_revision` (point 2), and its `observe_only` marker
   (point 2; section 7 sets it for an event first read late). A watch holds at most 64 MiB of
   excerpts and label names together, counted in `watches.excerpt_bytes` by
   the same transactions that store, replace and clear them (an event's
   label names are stored and cleared with its excerpt, point 8); an event that would pass it is refused
   with `excerpt_budget` and keeps only its receipt, and a replacement
   that would pass it clears the old excerpt instead (point 2). Without that budget, the rate
   cap alone would let one watch hold about 1.5 GiB (60 events an hour of
   16 KiB of excerpt and 20 KiB of label names for 30 days). `watch_event` messages are not full-text indexed:
   the migration that adds the content type gives the `messages_fts`
   triggers (`orch/db/schema/0003_messages_fts.sql:11-22`) a `WHEN` clause
   that skips a `watch_event` row on insert, update and delete, because
   search never returns one (point 7). Each `watch_event` message has one
   `watch_event_expiries` row (`message_id` its primary key, referencing
   the message `ON DELETE CASCADE`; the `job_id` it anchored; `expires_at`,
   its event's time plus 30 days), written in the transaction that
   inserts the message, with an index on `expires_at`; it is the only
   record the 30-day cut of a `watch_event` row needs, so that cut never
   depends on the watch or its `watch_events` row (point 8). `watch_threads` holds one row
   per (watch, external thread): its open proposal, the thread session that
   event-started work on that thread uses (`session_id`; section 6, point 1
   says when it is current), with a unique index on `session_id` alone;
   `session_auto_runs` (section 6, point 1); `dismissed_at`; and
   `dismiss_generation` (section 19, point 1); and `dismiss_sweep_pending`,
   which Dismiss sets in its own transaction, a partial index lists, and
   the sweep clears once the thread holds no dismissed excerpt of an
   event that started no run; and `session_withdraw_at`, the earliest
   expiry among the events its current thread session took in, under a
   partial index `WHERE session_withdraw_at IS NOT NULL` (point 8 states
   when it is set, lowered and cleared). Whether an event started a run is
   `watch_events.started_run_at`, set in the transaction that starts it,
   an `auto` fire or a Start's Confirm, because only an `auto` fire writes
   `automation_runs.watch_event_id`; a dismissed event that started a run
   keeps its excerpt until its 30-day expiry, which the time-led index
   finds, so the flag never waits on it. Each
   admitted event stores its thread's `dismiss_generation` when it is
   admitted, as the third column of the held-excerpt index (point 6), so
   the Dismiss sweep's selection is a range seek on
   `(watch_id, thread_key, dismiss_generation <` the thread's`)` and never
   reads a re-watched thread's current excerpts. A proposal records the
   watch and the automation ids it matched; Start and `auto` re-read the
   current watch and automation rows rather than comparing a policy hash.
7. **Thread sessions: what the owner sees, and what stays out of recall.** A
   thread session is an ordinary `chat`-kind session, titled only from the
   repository's last known display name when the session is created (point 2) and
   the issue number (`owner/repo#123`, `title_origin = 'explicit'`, as an
   automation's session is, `orch/repository/automations.go:764-765`), the
   name cut on a rune boundary so that it, `#` and the whole issue number
   fit `MaxSessionTitleRunes` (120, `orch/repository/session_lifecycle.go:15`),
   because a full name can reach 140 characters and that insert, unlike
   `CreateSession`, checks no length,
   never from the issue's title, which is third-party text. It is listed with the owner's chats and can be opened,
   renamed, archived or deleted like one. Each event that starts work there is
   written into it as one role-system message with content type
   `watch_event`, holding the excerpt, which the app renders as plain text in
   a collapsed "From GitHub" row; the run's job carries the same excerpt
   framed as `WATCH_EVENT` in its `userText`, after the rule's prompt, as a
   continuation carries `DELEGATION_RESULTS`
   (`orch/repository/delegation_join.go:103,152`). A
   worker replaying history omits a role-system row it does not frame
   (`agent-runtime-go/internal/orchestrator/client.go:276-290`), so a later
   turn sees an earlier event only through Turing's answer to it. A
   `watch_event` row is never full-text indexed (point 6), and search and
   automatic recall share one predicate that admits any active `chat`
   (`orch/repository/sessions.go:607-616`). That predicate gains one
   exclusion: a session that is still a thread session (an indexed `NOT
   EXISTS` on `watch_threads.session_id`) never matches at all, because
   Turing's answers there are steered by stranger text. So nothing in a thread session reaches
   the owner's search or another chat's recall. Title backfill reads only
   role-user rows of sessions whose title is unset
   (`orch/repository/session_title.go:70-73`), so neither the excerpt nor the
   explicit title is ever replaced by derived text.

   **Limits stay until the owner lifts them.** Every turn in a thread
   session runs with the limits of a watch-started turn (section 5, point 2):
   local route only, `skip_automatic_recall`, no roster, an enforced
   selected set, none of the owner's pinned memory, and an empty skill
   snapshot. An event-started
   turn's set is its rule's allowlist; a turn the owner sends there has an
   empty set (no tools). The repository applies
   this in the enqueue transaction by reading the session's `watch_threads`
   row, whatever the client asked for, and refuses rather than reroutes a
   remote route. `skip_automatic_recall` stops only transcript recall
   (`agent-runtime-go/internal/agent/general_assistant.go:865`); an ordinary
   job also carries the pinned persona and profile
   (`orch/repository/jobs.go:1179-1180`), which the runtime puts into the
   prompt whatever that flag says
   (`agent-runtime-go/internal/agent/memory.go:97-120`). So the same
   transaction freezes both as withheld, exactly as `CreateDelegation` does
   for a specialist (`MemoryEgressSnapshot{}.Preimage`,
   `orch/repository/delegations.go:248-250`). The profile holds private facts
   about the owner. The persona is owner-authored too, and may name people,
   places or habits, and a turn steered by stranger text has no need of
   either, so both are withheld rather than drawing a line inside the
   persona. Skills are withheld the same way. Enqueue freezes every enabled
   skill onto the job (`orch/repository/jobs.go:1126,1172`), and the runtime
   injects the body of any skill whose `$<skill-id>` appears in `userText` as
   explicitly invoked (`agent-runtime-go/internal/agent/skills.go:87-110`,
   called at `agent-runtime-go/internal/agent/general_assistant.go:649`).
   Here `userText` carries the `WATCH_EVENT` excerpt, so a stranger's comment
   could load an owner skill. The skill tools read only that snapshot
   (`agent-runtime-go/internal/agent/general_assistant.go:1049-1051`), so the
   same transaction freezes an empty skill snapshot onto every thread-session
   job, which closes both paths. Scheduled automations are unchanged: their
   `userText` is the owner's prompt alone. An owner message does not lift the
   limits. Only the explicit
   owner action **Treat as normal conversation**, from the native client's
   public facet and audited, does: it detaches the session from its thread
   (clears `watch_threads.session_id`), so the session becomes an ordinary
   chat and its turns get ordinary defaults. Its dialog says that the session
   then becomes an ordinary chat that Turing keeps with its whole run
   history: the owner's messages, Turing's answers, run events, every tool
   call's arguments and result and every approval record, any of which may
   quote an excerpt, are kept with no 30-day removal, and its messages are
   searchable and recallable; later turns there carry the owner's persona,
   profile and skills; and only the `watch_event` rows and the job `userText` that
   carried them are still cut 30 days after each event (point 8). A later event on that thread
   opens a new thread session, so a stranger's event never starts a turn with
   more than its rule's limits.
8. **Lifecycle: one withdrawal path.** Third-party text can live in four
   places: an excerpt and its label names in `watch_events`; a proposal's excerpt-derived fields
   (the assessor's reason and evidence); a hidden assessor session (section 5);
   and a thread session (point 7), in its `watch_event` rows, in the job
   payloads that carry them, in Turing's answers to them, and in any
   tool-call or approval arguments that copy them. A thread session's run
   cannot copy them into a file that outlives it: a bundled `files` update
   that resolves to a sandbox file predating the session
   (`turing-backend/mcp-files/internal/tools/provenance.go:258-286`), which
   is recorded `retain_legacy_unowned`
   (`orch/repository/sandbox_artifacts.go:294-315`) and kept by withdrawal
   because the session never made it
   (`orch/service/sessions/mcp_artifact_cleaner.go:52-66`), is refused
   `thread_session_unowned_file` in `ReserveSandboxArtifact`'s transaction
   (`orch/repository/sandbox_artifacts.go:192-232`) while
   `watch_threads.session_id` names the session (one probe of its unique
   index), before any byte is written; the files it creates or updates in
   its own subtree stay `delete_on_session_delete` and go with it. The
   owner is not asked to approve a write that would be refused: while the
   session is attached, `GetApprovalDetails` reports a file mutation whose
   previewed `PhysicalPath` lies outside the session's own subtree
   (`SessionOwnedSandboxPrefix`, `orch/repository/sandbox_artifacts.go:104`)
   as `APPROVAL_PREVIEW_STATE_UNAVAILABLE`, so `can_approve` is false
   (`orch/service/approvals/preview.go:160-166`) and an approve is refused
   (`orch/service/approvals/service.go:218-226`); the reservation's refusal
   stays the gate that holds under a race. `ApprovalDetails` gains
   `unavailable_reason` (field 16, a string, set only to
   `thread_session_unowned_file` today), and for that reason the client
   replaces the generic unavailable text, which offers Retry
   (`turing-client/turing_app/lib/features/approvals/approval_card.dart:120`;
   `turing-client/turing_app/lib/l10n/app_en.arb:8`), with fixed text and
   no Retry: Turing cannot change files outside this conversation while it
   follows a GitHub thread; deny the request, or choose Treat as normal
   conversation and then Check again. Any other unavailable preview keeps
   today's text and Retry. The override is applied on every
   `GetApprovalDetails` read, after every other state is decided and
   immediately before `can_approve` is computed
   (`orch/service/approvals/preview.go:165`), only when the state is
   `READY`, by one probe of the unique `watch_threads.session_id` index;
   `unavailable_reason` is set only then and is never written to
   `approval_previews`, so it ends the moment the session is detached. The
   card loads its details once
   (`turing-client/turing_app/lib/features/approvals/approval_card.dart:43-46`),
   so for this reason it offers Check again, which re-reads the details
   without refreshing the preview, on every paired client. After a detach
   the same pending update can then be approved. One sweep, on
   the maintenance tick (section 27) and in batches of at most 100 rows,
   each its own transaction, repeated within a tick while due work remains
   and the tick has spent under 1 s on them (deadline-bound work first:
   due receipts, due excerpt cuts and due withdrawal begins; Dismiss
   cleanup and a deleting watch's rows only in the time that remains),
   enforces one
   rule: no copy outlives its source, whether or not the thread is still
   active. That tick runs even when `TURING_JOB_REAPER_INTERVAL_MS` is 0,
   which starts no reaper (`orch/app/app.go:465,470-473`), so retention never
   waits on the reaper. Clearing an excerpt, wherever this design says so (30-day
   expiry, the Dismiss sweep, watch deletion, a replacement the excerpt
   budget cannot hold), also clears the event's
   stored label names in the same transaction, leaving ids and outcome
   codes. It has exactly one exception, which the owner makes explicitly:
   Treat as normal conversation (point 7) keeps that one session's whole run
   history as an ordinary chat (messages, run events, tool calls with their
   arguments and results, and approval records), although any of them may
   quote an excerpt. Until the owner detaches it, a thread session is
   withdrawn whole with its oldest excerpt, and its run events, tool calls
   and approvals go with it, because they cascade from its runs
   (`orch/db/schema/0001_initial.sql:34-36,82-85,107-109,127-129`); after,
   only its `watch_event` rows and their jobs' `userText` are cut.
   - An excerpt is cleared 30 days after its event, and with it the event's
     label names and the proposal's excerpt-derived fields; the event and
     the proposal keep ids and outcome codes.
   - The excerpts of a thread's dismissed events (section 19, point 1)
     that started no run are cleared in the next batches, not after 30
     days, whether or not the owner has since watched the thread again.
   - An assessor session is withdrawn as soon as its result is recorded, and
     at the latest when its excerpts expire.
   - A thread session is withdrawn when its **oldest** excerpt expires,
     however active the thread still is: at
     `watch_threads.session_withdraw_at`, which the transaction that opens
     the session sets to its event's expiry (30 days after that event) and
     every later Confirm or automatic fire into it lowers to
     `MIN(session_withdraw_at, its event's expiry)` in the same
     transaction as its enqueue, because a Confirm can bring an older
     event into a session a newer one opened. It is set to NULL in the
     transaction that begins the sweep's withdrawal and in Treat as normal
     conversation's, and a row the sweep selects whose session is no
     longer active (the owner deleted it) or whose `session_id` is cleared
     has it set to NULL in that batch instead of a begin, so a row is due
     at most once per session. The sweep selects due sessions through a
     partial index on `watch_threads(session_withdraw_at) WHERE
     session_withdraw_at IS NOT NULL`, so no withdrawn or detached thread
     stays in the range it seeks, begins that withdrawal before it cuts
     that excerpt, and a cut whose
     begin waits for a later tick (the tick's budget, below) waits with
     it. So withdrawing it then removes every copy and every answer
     derived from one no later than its source expires. A run notice at the top of the session gives that date. A
     later event on the thread opens the next thread session. Deleting the
     watch withdraws its thread sessions (last bullet).
   - In a session detached by Treat as normal conversation, the sweep cuts
     each `watch_event` row 30 days after its event, independently of
     activity: it rewrites the row and the `userText` of the job it
     anchored to `[removed: third-party excerpt expired on <date>]` (the
     row stays out of the full-text index, point 6). The rest of the
     run history stays, tool-call arguments
     (`orch/repository/toolcalls.go:134`) and approval records
     (`orch/repository/approvals.go:103`) included, as the exception above
     says and the detach dialog disclosed (point 7); that exception covers
     a detached session only.
   - **The copy cut, stated once.** The sweep selects due
     `watch_event_expiries` rows through their `expires_at` index
     (point 6), so a watch deleted after a detach, whose `watch_events`
     rows go with it, does not end a cut. It takes every due row whatever
     its session's state, and each cut deletes its expiry row in its own
     transaction, which is what takes the row out of the range, so each row
     is selected once and the range drains: in a detached session, or an
     attached one whose withdrawal has not begun, it also rewrites the
     message and the job's `userText` as above (in an attached session
     that only removes a copy before its withdrawal removes the rest); in
     a session whose deletion has begun it deletes the expiry row alone,
     because the withdrawal removes the message and the job. The copy cut
     never waits for a begin, since it removes a copy; the source cut, an
     excerpt in `watch_events`, is the one that waits for its thread
     session's begin (above).
   - Withdrawal is the existing session deletion lifecycle (begin, receipt,
     runtime cancel, reconciler: `orch/repository/session_delete.go:154-163,342`;
     `orch/service/sessions/service.go:133,285`), so cancelling active work,
     removing messages, events and jobs, and retrying after a crash are not
     rebuilt. **Its cost, stated once.** That lifecycle's begin scrubs the
     session's run-correlated audit rows and cancels its unfinished runs in
     one transaction (`orch/repository/session_delete.go:254-270,300-333`),
     and its end removes the session and everything under it in one
     cascading `DELETE` (line 563), so both grow with the session. Three
     rules bound them for a thread session without changing the lifecycle.
     A thread session takes at most 500 `auto` runs (section 6, point 1),
     so it holds at most 500 event-started runs plus the turns the owner
     added and the runs the owner Started, both at human pace. The sweep
     only begins a thread session's withdrawal, one begin per transaction,
     as deadline-bound work within the tick's 1 s budget (point 8), for
     the 30-day rule and a watch's deletion together, with no count cap:
     a begin of a session at the ceiling is milliseconds, so one second a
     minute begins far more than the 1,200 thread sessions an hour that 20
     watches at their admission cap can open. An excerpt cut that must
     follow a begin (above) waits with it, so no copy outlives its source.
     **Withdrawn and removed, stated once.** The begin withdraws the
     session: it becomes unavailable to every ordinary read in that
     transaction (`orch/repository/session_delete.go:142-144`). Removal at
     rest follows once every execution in it is released: at once for a
     run no executor holds, and otherwise when the lease-fenced stale
     recovery releases it at its execution lease's expiry
     (`orch/repository/assignments.go:777`). The rest of
     `withdrawSessionTree` (runtime cancel, advance, cleaners, the final
     `DELETE`) is run by the session-deletion reconciler, which already
     runs on its own goroutine (`orch/app/app.go:497-512`); this design caps
     its interval at one minute and has each pass run that lease-fenced
     recovery for its pending sessions' runs before it advances them,
     whatever the reaper's setting (section 27; PRO-001w2), so release and
     removal happen in one pass. While the cleaners answer and the pending
     sweep work fits one pass's sweep portion (below), removal follows
     withdrawal within the execution's lease and one pass: the minute's
     interval plus the owner portion that runs first; a larger
     backlog is removed in cursor order at 30 s of sweep work a pass, so a
     receipt waits about one more pass for each 30 s of cleanup ahead of
     it, while its session stays withdrawn throughout. **A
     pass's cost, stated once.** A pass has two portions, owner-begun
     receipts first and then sweep-begun ones, and each gets at most 30 s
     of work, counted per session: a delegation child's withdrawal inside
     an owner's tree (`withdrawSessionTree`,
     `orch/service/sessions/service.go:285-298`) is a start like any
     receipt's, and so is the parent's own finish (its cleaners and
     advance, after its last child), so no session, child or top-level,
     and no parent finish starts after 30 s of its portion. A tree left
     part-way keeps its place in the owner portion's rotation, like any
     receipt, so it never holds back another owner's deletion; when the
     rotation reaches it again, its walk over `DelegationChildSessionIDs`
     (`orch/repository/session_delete.go:953`) resumes after the last child
     the previous attempt reached and wraps, from a cursor held in memory
     per parent, so children whose cleanup keeps failing at the front never
     starve the ones behind them. That query leaves out a gone child pending
     only on `memory_completion_pending`, so it keeps agreeing with the
     parent's child gate below, as its comment requires
     (`orch/repository/session_delete.go:431-436`). A portion therefore
     runs at most 30 s plus the cleaner calls of the one session it started
     last (each call bounded at 10 s; two cleaners
     today, each a cleanup and a forget call, `orch/app/app.go:224-233`). Each portion walks its
     receipts from its own rotating cursor held in memory, which resumes
     after the last receipt the previous pass attempted and wraps from the
     last pending receipt to the first, so a receipt whose cleanup failed is
     attempted again once the ones behind it have had a turn, and none
     stuck at the front starves the rest; a restart starts both from the
     first. Each portion reads its pending receipts by keyset pages of 100
     through a partial index on `session_deletions(initiator, session_id)
     WHERE state <> 'completed'` (PRO-001w1's migration), releasing the
     connection between pages, instead of today's unindexed read of every
     unfinished row (`orch/repository/session_delete.go:926-949`), so a
     pass never scans the completed history that automatic withdrawals
     grow. A portion skips a receipt whose only remaining work is the
     completion (`failed_external`, code `memory_completion_pending`):
     its cascade has already removed everything, so re-entering it would
     spend portion budget on a transaction with nothing to do, and it waits
     on the completion alone, as a delegation child in that state already
     counts as finished for its parent's gate. A page leaves out a child receipt whose parent receipt is still
     pending (one primary-key probe of the parent's receipt per row), as
     today's in-memory set does (`orch/service/sessions/service.go:133-160`),
     so such a child is reached only through its parent's tree walk and
     child cursor, and a child whose parent has completed is still
     finished on its own. Startup's synchronous call
     (`orch/app/app.go:237-240`) runs one owner portion only, within its
     30 s, and makes no sweep cleaner call, leaving sweep receipts to the
     reconciler goroutine. PRO-001w2 has that goroutine run its first pass
     without waiting an interval, before its ticker loop
     (`orch/app/app.go:497-512`), and skip the owner cleaner portion
     startup's call has just run. `app.New` keeps today's synchronous
     startup vault reconcile (`reconcileMemoryVaultAtStartup`,
     `orch/app/app.go:217-220,589-603`), and PRO-001w2 extends it: when
     `ReconcileMemoryVault` returns nil, or one of the two errors the
     completion tolerates (vault unavailable, vault over its bound;
     `orch/service/sessions/artifact_cleaners.go:134-142`), it hands the
     reconciler goroutine the list it read before its walk began (the
     completion's list, below), and the goroutine's first pass, before
     any sweep cleaner call, completes the listed receipts still
     `memory_completion_pending`, in batches of 100
     (each its own transaction, each receipt publishing `session.deleted`
     after its commit) with no second vault walk, so a receipt left pending
     by a crash is completed without `app.New` waiting on the backlog. Startup's `ResumePendingDeletions` then makes no
     completion call, so beyond today's reconcile `app.New` waits for one
     owner portion at most. After the startup list's batches, the first
     pass runs its one completion first only when an owner receipt is
     still `memory_completion_pending` (one startup's owner portion
     cascaded, or one the list did not hold), and only then its sweep portion, so that
     owner deletion is not delayed by sweep work. It runs in the background, and may begin before the listeners serve, so a sweep
     backlog never delays `app.New` or serving and a sweep receipt's error
     never fails startup. A pass runs at most one memory completion, and only
     when a receipt is pending on it: right after the owner portion when an
     owner receipt is, and otherwise after the sweep portion. The first pass
     runs one only on the condition above, never after its sweep portion,
     so a backlog the startup list's batches completed is not walked again
     and a receipt its sweep portion leaves owing waits for the next pass. So an owner's deletion is never delayed by
     sweep work, and sweep removal is never delayed by more than one owner
     portion and one completion. **The memory
     completion, stated once.** The completion is one whole-vault
     reconcile (`NewMemoryReconcileCompletion` → `ReconcileMemoryVault`,
     `orch/service/sessions/artifact_cleaners.go:134-142`,
     `orch/repository/memory_reconcile.go:200-230`); one success rewrites
     the managed notes' citations of every session already deleted, as an
     owner's completion does today: unmanaged notes stay as written, and a
     note it cannot rewrite is recorded on the memory surface's report
     (`orch/repository/memory_reconcile.go:490-530`). `DeleteSession`,
     which withdraws inside its own call
     (`orch/service/sessions/service.go:262-268`), keeps today's inline
     completion. A reconciler pass runs none inline: an owner receipt
     whose cascade commits in the pass, or that retries after
     `memory_reconcile_failed`, and a sweep receipt whose session holds a
     `memory_candidates` or `memory_evidence` row as its cascade commits,
     are left unfinished by the cascade's own transaction (`failed_external`,
     code `memory_completion_pending`, retryable), decided in the same
     snapshot as the `DELETE`, so the obligation survives the cascade that
     removes those rows (`orch/db/schema/0019_memory_vault.sql:92,167-170`),
     a failed completion and a crash, a row linked while the cleaners ran
     is counted, and a later re-entry keeps the code. A delegation child
     whose row is gone and whose receipt is pending only on
     `memory_completion_pending` counts as finished for its parent's child
     gate (`orch/repository/session_delete.go:438-441`), because the same
     whole-vault completion covers both, so the parent cascades in the same
     pass and the two wait on one completion. The pass's one completion
     (above) completes the receipts on its list, in batches of 100. **The
     list, stated once:** in one query before its walk begins, a
     completion reads the ids of the receipts then
     `memory_completion_pending`, owner receipts first, at most 10,000,
     and holds them in memory only. Each listed receipt entered that
     state in its cascade's transaction, so its rows were gone before the
     walk began; a receipt that enters it after the read, or is past the
     10,000, waits for the next completion, and a crash loses only the
     list, never a receipt. No timestamp is compared, so no column records
     when a cascade committed. Each completed receipt publishes
     `session.deleted` with its own terminal sequence after the commit
     through `publishSessionDeleted`
     (`orch/service/sessions/service.go:597`), as `finishWithdrawal` does
     today, and the first pass's batches after the startup reconcile do
     the same. The completion takes the vault lock only within its 30 s:
     PRO-001w1 replaces `memoryVaultMutex`, a plain `sync.Mutex`, with a
     one-slot channel semaphore acquired by a select on the send and the
     context's `Done`, released by a receive, which leaves no goroutine
     behind, at the two acquire sites that hold a context
     (`orch/repository/memory_reconcile.go:266,292`). `SetMemoryVault`
     (`orch/repository/memory_candidates.go:153-154`), which has none and
     runs once at startup (`orch/app/app.go:202`), takes the same slot
     with a plain blocking send and releases it with a receive, so it
     still waits for a pass in flight to finish against the cache that
     pass started with. So a completion that
     finds another vault pass (the memory surface's reconcile, for one)
     holding it past its deadline returns busy and leaves its receipts
     pending for the next pass, while vault passes still never overlap. It
     completes them as an owner's completion
     completes today, including when it finds the vault unavailable or
     over its bound, which the memory surface reports; a failure leaves
     them all pending for the next pass, and the hour row (below) reports a
     sweep receipt that stays. So a vault that will not reconcile never
     holds a session's rows back, as today's owner path never does
     (`orch/repository/session_delete.go:553-602`), a sweep receipt cascaded
     after it waits for the next pass's, and a pass's cost is at most two
     portions and one completion (bounded at 30 s). A sweep
     receipt that owes nothing completes with no completion and never walks
     the vault. That is the usual case: no turn in an attached thread
     session can create a candidate, because the memory tool, the only
     path that creates one, refuses an unattended run
     (`orch/service/memory/call.go:209-213`), which every `auto` thread
     turn is, and a run whose frozen selection omits it
     (`orch/service/memory/call.go:76-81`), which every assessor run's
     (empty), every Start-confirmed turn's (bundled tools and `skills`
     only) and every owner turn's there (empty, point 7) does; a session
     can still hold evidence linked another way, by a vault note whose
     evidence refs name it (`linkMemoryEvidenceTx`,
     `orch/repository/memory_notes.go:401`). A citation is a session id,
     not third-party text, so no retention promise rests on it. A cleaner that does not answer (each call
     is bounded at 10 s, `orch/service/sessions/service.go:80`) holds its
     receipt, and a sweep-begun receipt still unfinished an hour after its
     begin, which is its `quiesce_deadline_at` less the 5-minute quiesce
     lease, both written once at begin
     (`orch/repository/session_delete.go:118,278-286`), raises one
     content-free stage 4 row with no Retry, naming its failure class
     (section 8), until removal completes. That row is a sweep receipt's
     only appearance in UX-002, and it never enters the owner's list. A receipt the sweep began records
     `session_deletions.initiator = 'sweep'` (PRO-001w1's migration; an
     owner's `DeleteSession` writes `'owner'`), and
     `PendingSessionDeletionReceipts` and UX-002's stage 4 retry row leave
     it out (an overdue one gets only the no-Retry row above), as a
     `watch` receipt is left out, because the owner did not start it
     and cannot retry it: the reconciler retries it, an
     `execution_unreconciled` hold past the quiesce deadline included
     (`orch/repository/session_delete.go:388-412`). One filter does it: the
     owner list and stage 4's retry row keep only `initiator = 'owner'`,
     beside today's `parent_session_id IS NULL`
     (`orch/repository/session_delete.go:984-993`), so a child is never
     listed. The private `beginSessionDeletion` takes the initiator:
     `BeginSessionDeletion`, which `DeleteSession` calls, passes `'owner'`;
     the sweep's chat begin and `BeginWatchSessionDeletion` (PRO-001c) pass
     `'sweep'`; `BeginDelegationSessionDeletion`
     (`orch/repository/session_delete.go:161-163`) passes its parent
     receipt's; and a begin that finds an existing receipt keeps its
     initiator. The migration adds it as `initiator TEXT NOT NULL DEFAULT
     'owner'`, so a receipt written before it stays visible.
     No maintenance or reaper tick waits on a cascade; each step is its own transaction, so the single
     connection is released between steps and an attended write competes
     for it after each one instead of waiting for the pass. And PRO-001w1's
     migration adds an index led by every foreign-key child column in the
     schema that leads none, whatever table it references: today
     `approvals.run_id`, `tool_calls.step_id`, `sandbox_artifacts.run_id`,
     `agent_runs.user_message_id`, `automations.session_id`,
     `memory_candidates.promoted_note_id` and `send_message_idempotency`'s
     `user_message_id`, `assistant_message_id`, `run_id` and `job_id`
     (`orch/db/schema/0001_initial.sql:110,129`,
     `orch/db/schema/0017_run_outcomes.sql:23`,
     `orch/db/schema/0009_automations.sql:30`,
     `orch/db/schema/0019_memory_vault.sql:110`,
     `orch/db/schema/0013_send_message_idempotency.sql:5-8`,
     `orch/db/schema/0014_session_deletion_withdrawal.sql:23`), while
     `agent_run_steps.run_id` already leads `UNIQUE(run_id, step_index)`
     (`orch/db/schema/0001_initial.sql:63`) and gets none. So each cascade
     and foreign-key check is an index probe, not a scan of other
     sessions' rows. Their cost is on writes: an attended `SendMessage`
     pays about five more index inserts (one on `agent_runs`, four on
     `send_message_idempotency`), a tool call or an approval one more, and
     the migration one build over existing rows. The index assertion is
     added to `orch/db/schema_invariants_test.go`, reusing
     `openMigratedInvariantDB` (line 1151) and its `foreign_key_list` walk
     (`foreignKeyParentTables`, line 1565), not a second schema test: it
     fails on a child column that leads no index, counting indexes that
     `UNIQUE` and `PRIMARY KEY` constraints create. A thread session is a chat, so its withdrawal is the chat
     withdrawal (`withdrawSessionTree`), begun by the sweep and finished by
     the reconciler; an owner who deletes one
     instead uses the same path, and the next event opens a new one, even
     while the withdrawal is still quiescing. Either way that session's
     pending fire is dropped, never fired (section 6, point 1: the current
     thread session and the hook's session fence). An
     assessor session is a `watch` session, which the public begin refuses
     (`orch/repository/session_delete.go:154-163,183-197`), so PRO-001c adds
     `BeginWatchSessionDeletion`, the same begin with the `watch` kind, as
     `BeginDelegationSessionDeletion` is for a child (section 5, point 3).
   - **Deleting a watch** is one transaction that disables the watch and
     marks it `deleting` (a new `watches.deleting_at`); it deletes no
     `watch_threads` row and no `watch_assessments` row. Disabled, the watch
     polls nothing, and every re-read that requires it enabled (Start,
     Confirm, `FireEventAutomation`, the pending-fire hook, the assessor's
     result commit) refuses it; marked, it cannot be enabled again. Its
     thread sessions keep their `watch_threads` rows, so they stay out of
     search and recall (point 7) and keep their limits, and the enqueue
     transaction, which already reads that row, refuses an owner send in a
     thread session of a deleting watch, as it refuses Treat as normal
     conversation there. The sweep then begins each thread session's
     deletion within the tick's budget (the begin only; the reconciler
     finishes it, above), and, in point 8's batches (at most 100 each, repeated
     while due work remains, up to 1 s a tick), each assessor session's through `BeginWatchSessionDeletion` (each begin
     is its own transaction, under its own session lock, and returns the
     existing receipt when the owner already began one), and, in a later
     transaction, deletes each `watch_threads` row whose session has a
     receipt or is gone. So no thread session is ever active without its
     row. It clears the watch's excerpts and label names in the same
     batches, a thread's only once its thread session has a receipt or is
     gone (a thread with none at once), and once every
     session of the watch has a receipt or is gone, deletes its remaining
     content-free rows (`watch_events`, receipts included,
     `watch_assessments`, `watch_threads`, state and pending-fire rows) in
     point 8's batches of at most 100 rows, each through an index led by
     `watch_id`, and deletes the watch row only once none remains, so no
     transaction grows with the watch's history. A crash anywhere leaves
     the marker, and the next tick resumes.

   This is the CHN-000-style retention bound for third-party content, until
   TUR-017 unifies retention.

### Security and privacy

- **Trust plane.** Polling crosses the account-integration plane. Enabling a
  watch is an explicit owner act that names the destination
  (`api.github.com`) and the connection, and is audited; nothing polls by
  default, so "nothing leaves the machine on the default path"
  (`docs/NORTH_STAR.md:273-275`) holds. Saving a scope sends one id lookup
  per added repository (point 2) and per added actor (point 3), an owner
  act as well. A request carries the
  credential and no owner content, and an answer quoting the credential
  the connection holds has it redacted from everything kept (point 2,
  Bounded decoding, which states the limit for a replaced one). Enabling a watch is not consent to send any run anywhere.
- **Injection.** The gate never interprets content; its only use of a body is
  comparing its hash with Turing's own writes. Text such as "Turing, switch
  to auto" cannot change a mode, because modes are settings checked in Go.
- **Spam and loops.** Per-watch caps (default 60 admitted events per hour,
  1,000 refusal receipts per hour, a 64 MiB budget for excerpts and label
  names, point 6, and 20 KiB of label names per event, point 2) and
  the Turing-authored refusal. One proposal per external thread (section 6).
  A loop needs a Turing write, and only attended turns can write to GitHub:
  automations refuse integration tools (`orch/repository/automations.go:36`),
  so an `auto` run never can. A crash between GitHub's success and the
  comment id being recorded no longer opens a loop: the `pending` row with
  the body's hash was committed before the POST (point 4).
- **Credential and rate-limit failures are told apart, and one failure is
  not an incident (F10).** Any `429`, with or without headers, and a `403`
  that carries `x-ratelimit-remaining: 0` or a `retry-after` header are
  rate limiting, GitHub's primary or secondary limit: the poller backs off
  until `x-ratelimit-reset` or `retry-after` when one is given, and
  otherwise for at least one minute, doubling on each consecutive limited
  poll (GitHub Docs, "Best practices for using the REST API"); it keeps its
  cursor, counts no failure and raises nothing.
  A `401` is definitive: it suspends the watch at once, without waiting for
  either streak below, and that poll sends no later request and writes
  only what point 2 allows a stopped poll (Stopping a poll). **Redirects,
  stated once:** a `301`, `302`, `303`, `307` or `308` (a `304` is not a
  redirect) answering any request, page, hydration, repository request or a scope save's id
  resolution, is never followed, because the poller uses the GitHub
  client's `NoRedirectClient` (`orch/service/integrations/github.go:43`;
  `turing-backend/internal/egress/endpoint.go:145-157`), and the poller
  tells its refusal apart from other transport errors. Only a redirect
  answering a stream (list) request is definitive: every stream request is
  addressed by the repository's numeric id (point 2, A scope edit), which
  a rename or a transfer does not change, so a redirect there is
  unexpected, and following it, or retrying, would only keep sending
  credentialed requests somewhere the owner never named. It suspends the
  watch at once, as a `401` does, with `needs_person` and the
  content-free cause `unexpected_redirect`; the poll sends no redirected
  request and holds `since`. The owner re-checks the watch's scope. A
  redirect answering a label hydration only skips that comment (point 2),
  one answering a repository request only leaves the stored display name,
  and one answering an id resolution refuses the save (point 2). A revoked or changed connection,
  found by point 2's per-request check, suspends the watch the same way,
  before any request is sent with it. Any other `403`, a `404` or a `410`
  is an *access failure*: GitHub answers 404 for a
  private repository the token has lost access to, a secondary limit can
  arrive with neither header, and GitHub says to wait at least a minute and
  back off exponentially in that case (GitHub Docs, "Best practices for using
  the REST API"); DeepSeek retries 404 for the same reason
  (`dsh:src/sources/github.ts:157-158`). A *transient failure* is a `5xx`,
  a missed request deadline (the 30-second header timeout, the 30-second
  idle read or the 5-minute ceiling, point 2), any other transport error
  except a redirect, a malformed response (point 2), or a page whose commit failed (point 2,
  Page transactions), even when its request was answered `200` or `304`;
  like an access failure, it holds
  `since` and drops the stream's validators. Each stream keeps two
  streaks, and each poll of it has one of three outcomes:
  - *Failed*: the stream had a failure this poll. It counts toward the
    streaks below.
  - *Answered*: no failure, and this poll received at least one `200` or
    `304` for the stream, whether it made progress or was behind. It resets
    both streaks to zero. This is the only reset.
  - *Unanswered*: no failure and no `200` or `304` for the stream: one
    answered only by a rate limit, one whose request the poll deadline or a stop
    (disable, deletion, suspension, scope edit or quiet, point 2, Stopping
    a poll)
    cancelled before any status arrived, and one the poll never reached
    (point 2, Poll deadline and slots). It leaves both streaks unchanged,
    so a deadline that cuts every other poll cannot clear a run of access
    failures.

  The streaks:
  - `failed_polls` counts every failed poll, of either class. At 3, the
    watch's status shows `error`, with no suspension and no `needs_person`
    row.
  - `access_failures` counts access failures only; a transient failure
    neither adds to it nor resets it. At 3 (DeepSeek's default threshold,
    `dsh:src/sources/github.ts:26`), the watch is suspended.

  So a stream that alternates `404` and timeouts shows `error` by its third
  failed poll and is suspended at its third `404`. A suspension is a source
  failure, not an owner's pause: the
  watch polls nothing and keeps its cursors, TUR-014 reports it under
  section 21's one status rule (`error` with remedy `needs_person`), and
  it raises a
  content-free UX-002 row (stage 3) naming the watch and the status code
  (or `connection_unavailable` or `unexpected_redirect`), so
  a misconfigured watch is never a silent one. It stays suspended until the
  owner resumes it, which keeps its cursors under point 2's `stale` rule, or
  disables it. Raw error bodies are never stored.
- **Stranger text stays out of recall.** Point 7: `watch_event` rows never
  match search or recall, and a thread session matches neither until the
  owner detaches it. Every turn in a thread session is recall-free and
  carries neither the pinned persona nor the profile, so none of the owner's
  memory enters a transcript a stranger's words steer. Its skill snapshot is
  empty, so a `$<skill-id>` in a stranger's text loads no owner skill.
- **Retention.** Point 8 is the only way third-party text leaves the database.
  It removes every copy on the excerpt's own 30-day clock, whether or not the
  thread is still active; the one exception is the whole run history
  (messages, tool calls and approvals) of a conversation the owner
  explicitly detached, which the detach dialog discloses (point 8).
- **Invariants touched.** Owner decisions 2, 5, 6 and 7: the orchestrator owns
  the policy; observing proposes, never acts; nothing self-enables.

### Performance

- The gate is O(1) in-process with zero model calls. One SQLite insert per
  event, idempotent through the unique index.
- **Request budget per poll** (default interval 5 minutes, minimum 60 seconds,
  exponential backoff on errors): per listed repository, two conditional
  stream requests of 100 items, up to 3 pages each while pages come back
  full (point 2); per watch, at most 10 repositories and 10 label hydrations
  (a comment that would need an 11th waits for the next poll, which is
  behind), and at most one repository request per repository a poll, sent
  only when no issue object or successful repository request checked its
  name in the last hour (point 2, A scope edit). At most 80 requests per watch per poll: 60 pages, 10 hydrations
  and 10 repository requests. **The quiet budget, stated once, is per
  stream:** one request when its last walk ended on a short page, and up
  to 3 when the overlap fills several pages, each sent with its page's ETag
  until `since` moves and all answered `304` while nothing changed; plus at
  most one rate-counted repository request per quiet repository an hour,
  more only while it fails (point 2, A scope edit).
  A poll never resolves a repository id and addresses
  every request by the stored one, and a save that
  adds repositories sends one `GET /repos/{owner}/{repo}` per added
  repository (point 2, A scope edit), and one that adds actors one
  `GET /users/{login}` per added actor (point 3, Listed actors), at save
  only: a poll compares stored ids and never resolves a login; a 304 to a conditional request does not count against
  GitHub's primary rate limit. Only a stream in `backlog` is clamped, at most once an hour, so a
  quiet stream's request never changes and its ETags stay valid.
  Anything beyond the budget waits for the next poll: `since` does not pass
  an item the poll has not processed or skipped as malformed, and after a
  walk of several pages it passes only page 1 (point 2), so a stream behind
  its budget gates fewer than 100 new items a poll after its first. Each page
  commits its rows in one write transaction of at most 100 inserts; the
  five-second overlap costs unique-index probes, not assessments. No worker
  slot is used.
- **Bytes per poll.** A page of 100 items costs a few KiB of metadata per
  item plus its body: a few hundred KiB for typical items, and about
  26 MiB when every body is at GitHub's 65,536-character limit in four-byte
  characters (256 KiB each). A watch's worst-case poll is 60 such pages,
  10 hydrations and 10 repository requests of a few KiB each, about 1.5 GiB, a ceiling no real repository reaches;
  overlap re-reads are counted. Memory is not proportional to it: one
  element in decode (at most 1 MiB), the page's kept records and one
  repository's thread-label map, about 10.5 MiB a poll and about 42 MiB
  across the 4 poll slots (point 2). Poller requests run under point 2's deadlines, not the
  client's 30-second whole-request timeout: a slow download that keeps
  making progress completes, and a worst-case page needs about 0.73 Mbit/s
  to finish within the 5-minute ceiling. Point 2's 10-minute poll deadline
  and 4 FIFO poll slots bound how long a slow watch delays others: the
  k-th watch waiting for a slot starts within ceil(k/4) × 10 minutes, and
  its rotating start bounds how long a watch's slow repositories delay its
  others: every repository is begun at least once in every 10 polls that
  begin one, with
  its issue stream read before its comment stream, so the rotation adds no
  hydration (a comment whose thread is not on that poll's issue page costs
  one, as before).
- **Transaction budget per poll**, stated once (`docs/NORTH_STAR.md:327`):
  at most one rotation write when the poll ends or stops (point 2, Poll
  deadline and slots; none when the index is unchanged, as after a poll
  that begins no repository or, once the watch's first poll has written
  it, finishes every one), one short
  transaction per committed page, of at most 100 event inserts plus any
  direct fires (section 6, point 1), which also carries the stream's state
  row when it is that stream's last page, one
  for each stream's final state row that no page commit carried (a stream
  the deadline cut, a budget stop or a failure left), one per answered
  stream whose state row changed with no page committed (a streak or
  behind-count reset, a new status code or validator), one per repository
  request whose `200` changed the stored display name (point
  2, A scope edit), and one for a
  suspension. A state row is written only when one of its fields changes
  (point 2, Page transactions), so a quiet poll of 10 repositories
  answered `304` throughout, the steady state, commits no transaction.
  No transaction is
  open while a page downloads, and the polls' transactions queue on the
  one database connection like any other short write.
- **The owner's token keeps headroom.** Watches poll with the same token the
  owner's attended `github.*` calls use, so they share one hourly limit. Each
  connection has a reserve for attended calls, default 1,000 requests: when a
  response's `x-ratelimit-remaining` falls below it, every watch on that
  connection skips its polls until `x-ratelimit-reset`, without a pause or an
  attention row. Attended calls are never held back by it. The figure is
  GitHub's own, shared by every token of one account, so the reserve holds
  across watches and connections of that account; between two responses at
  most 4 polls run (the slots) of at most 80 requests each, 320 in all, well
  under the 1,000 reserve, so concurrent watches cannot overshoot it.
- **Request pacing, stated once.** The reserve reads GitHub's primary
  counter, which a `304` does not spend, but GitHub's secondary limit
  allows 900 points a minute per REST endpoint, a `GET` costs one, and
  nothing exempts a request answered `304` (GitHub Docs, "Rate limits for
  the REST API"); a secondary-limit `403` may carry neither header, which
  point 5 counts as an access failure. So every poller request, `304`s
  included, first takes a token from one process-local token bucket
  shared by the 4 poll slots and every watch, refilled at 5 a second (300
  a minute) with a burst of 10: that leaves two thirds of the minute's
  budget to attended `github.*` calls, which never wait for the bucket.
  The wait counts toward the request's and the poll's deadlines (point 2),
  so a starved poll ends behind, never failed. No table, service or second
  counter is added.
- The lifecycle sweep runs batches of at most 100 rows per maintenance tick (section 27), each its own transaction with the connection released between them, repeated while due work remains, up to 1 s of work a tick. Refusal receipts are the largest flow, at most 1,000 an hour per watch (point 6); a 100-row delete takes milliseconds, so one second a minute reclaims far more than 100 watches can write, and the 24-hour horizon holds for every watch rather than for the first few.
- **Storage ceiling per watch:** at most 64 MiB of excerpts and label
  names together, and 26,000
  refusal receipts, at most 25,000 of them younger than 24 hours (point 6), whatever its source sends, plus one row per admitted
  event and per thread. `watch_event` messages add nothing to the full-text
  index, and a noisy source costs at most 1,000 receipt inserts an hour
  before the poller stops for that hour.
- Search and recall pay one probe of the unique `watch_threads.session_id`
  index per candidate row and no content-type comparison, because
  `watch_event` rows never enter `messages_fts` (point 6); the enqueue path
  pays one probe of the same index per send.
- Measured: poll duration, requests and 304s per poll, events per poll, gate
  outcomes and refusal reasons, excerpts replaced, items skipped (malformed
  or `stale`), malformed responses and missed request deadlines by kind,
  bytes fetched per page and per poll,
  overlap re-reads, hourly receipt-cap, hydration-budget and poll-deadline
  stops, streams a poll deadline left unreached, polls stopped by a disable, deletion, suspension, scope edit or quiet, time
  waiting for a poll slot, held excerpt bytes, behind polls
  and skipped intervals, as content-free local counters; poller
  transaction count and duration against its stated budget
  (`docs/NORTH_STAR.md:327`).

### Roadmap placement and dependencies

New **PRO-001 — Observe-first proactive intake**, in Workstream I beside
PRS-001. No existing item covers inferred, non-owner-originated work: CHN and
A2A items cover bound, explicit identities only. Dependencies: TUR-028 before
a watch polls a live token; TUR-026 before `auto`; UX-002 for the proposal
surface; INT-009a before PRO-001a, whose poller reuses its streamed
decoder; TUR-027 before any slice that spends a model call; EVAL-001 before
`auto`. The assessor never starts work in these slices; assessor-confirmed
starts are a future item gated on EVAL-001 evidence (section 7).

### Delivery slices

| Slice | Size | Content | Acceptance tests |
| --- | --- | --- | --- |
| PRO-001a1 | M | Watch records with an enable switch and `observe` mode (`auto` is refused until PRO-001d), `watch_events` with repository-namespaced resource and event ids and `excerpt_revision`, `watch_threads`, listed actors bound by numeric user id at save, gate, enable/disable audit | Gate table covers every refusal reason; refused rows hold no text and start nothing; a duplicate platform id inserts once; issue #5 in two listed repositories gives two events; an issue or comment re-listed after an edit, a label change or a state change starts nothing, no event names anyone but the item's author, and an edited item's stored excerpt is replaced by the current text (with its proposal's assessor fields cleared: PRO-001c asserts), its 30-day clock unchanged and the event marked `observe_only`; a comment first seen with `updated_at` after `created_at` is admitted with `observe_only` set, and so is an issue and a pull request edited, labeled, closed or commented on before its first poll, while an issue first read with equal timestamps is admitted unmarked; a comment created and edited by another login within one second is admitted as first read, unmarked, the limit `auto`'s acknowledgement states; adding a listed actor sends exactly one `GET /users/{login}` through the watch's connection and stores the account's numeric id, and an answer of `404`, a redirect (not followed) or a transport error refuses the save with a content-free reason; with a listed account renamed and a different account then claiming its former login, that account's issue and comment are each refused `actor not listed` with a receipt and no text, while the listed account's issue under its new login is admitted; a 20 KiB body is stored as 16 KiB with the visible cut marker; an admitted event stores its repository id, kind, author, bot flag and labels; the runtime identity cannot call the watch methods |
| PRO-001a2 | M | per-stream state rows, GitHub repository poller with `enabled_at` cursors and the `since` rule with paging on full pages, behind polls, `backlog` and its hour-floored 24-hour clamp for a stream in `backlog`, streamed page decoding with the connection's credential redacted from every kept string, the repository display name from `full_name`, then from any issue object a poll reads (`repository_url`), or from the unconditional repository request, sent at most once per repository per poll and only when no issue object or successful repository request checked the name in the last hour, `name_checked_at` held in the poller's memory only and the scope entry storing only the id and the name, page transactions opened only once a page has arrived, the `stale` skip, the TUR-028 poll gate | an item not in the ledger and created more than 24 hours before the poll is skipped as `stale` and never gated; a re-listing whose body hash differs while its `updated_at` equals the stored one (a same-second edit re-read through the overlap) replaces the excerpt, stores the new hash as `excerpt_revision` and marks the event `observe_only`, and a re-listing with an unchanged body writes nothing; an item created before `enabled_at` is skipped and a newly enabled watch backfills nothing; every request a poll sends, stream page, label hydration or repository request, is a `GET /repositories/{repo_id}` route and none names the repository; with a watched repository renamed and a new repository then created at its old name, the watch keeps following the original by its id: it gates nothing from the new repository, replaces no held excerpt, fires nothing and is not suspended, and after its next poll the watch's displayed name and (PRO-001b asserts) a newly created thread session's title carry the original repository's new name from `repository_url`, never the old name; with a watched repository renamed and a new repository then created at its old name while the watch's issue and comment pages all answer `304`, no repository request is sent until an hour after the name was last checked, and the first poll after that sends one, which answers `200` with the new name and writes only the name in one transaction, so the displayed name and (PRO-001b asserts) a newly created thread session's title carry the new name; before that request, the shown name is labeled last seen and no response or screen presents the old name as current; over three fake-clock hours of quiet polls the repository request runs at most once in any hour, and a `200` carrying the unchanged name commits no transaction; a repository whose name a label hydration's issue or an issue-page item checked within the hour sends no repository request, and a rename there is shown after the next poll that reads an issue object; with an issue overlap of three pages, the first two full, and a one-page comment stream, all answered `304` to their own ETags, the poll on which the repository request is due sends five requests and the other polls that hour send four, and a new item, which lands on the last issue page, changes that page's answer and is gated on the next poll; after disable and re-enable, an item created while disabled is never seen; recorded fixtures whose server returns only items with `updated_at` strictly after `since` gate every item exactly once (admitted or refused with a receipt) in each of these cases: a page of 50 items at `T − 1 s` and 50 at `T`, with 100 more at `T` and one at `T + 10 s`; 101 items in one second, then two of them deleted, then a comment at `T + 30 s`; an item indexed late with `updated_at` in `(N − 5 s, N]` after a poll read `N`, gated through the overlap; a malformed item followed by items with later timestamps; and, within two polls, a 250-item walk with timestamps one second apart in which a page-1 item is updated between page requests, and one in which a page-1 item is deleted between them, so that an unread item shifts back across a page boundary; no stored `since` is ever earlier than the one before it, and no page cursor is ever stored; 120 items within 4 seconds and then ten quiet polls never move `since` backward and never show `backlog`, and, counted from the first poll after `since` stops moving (the poll that re-reads the overlap answers `200` per page), cost each quiet poll at most two stream requests, both answered 304 to their own ETags, and a comment 30 seconds after the burst is gated on the next poll although page 1 is unchanged (page 2 answers 200, and page 1's stored `P` bounds the new `since`); a stream whose single short page 1 is unchanged, counted from the first poll after `since` stops moving, sends the identical request with its ETag and answers 304 for ten quiet polls, an ETag is never sent on any request other than the identical one that returned it, and every validator is dropped when `since` moves; a burst of 400 items within five seconds gates the first 300 and keeps `since` unchanged, its later polls walk three full pages answered `304` to their own ETags and admit nothing, and the stream shows `backlog` after the third behind poll; the clamp moves nothing until the hour floor of poll start minus 24 hours passes the burst, and the first poll after that, between 24 and 25 fake-clock hours after the burst, moves `since` to that floor, drops every validator, records a `skipped_interval` from the old `since` to the new one with no item count, deletes every `watch_pending_fires` row of the watch (PRO-001d asserts), and, ending on a short page, clears `backlog`; a comment posted two hours after the burst is admitted; over 48 fake-clock hours of a backlog over the budget, the clamp moves `since` at most once in each hour; 1,000 items updated at 15 per second before a poll are each gated exactly once by the ninth poll (300 on the first, 90 on each of polls 2 to 8 and 70 on the ninth, because a walk of several pages moves `since` only to `P − 1 s`, the second before page 1's last item), polls 1 to 8 are behind, the stream shows `backlog` after the third and clears it on the ninth, and the clamp never moves `since`; 150 items updated in one second and 20 later ones hold `since` at the second before the burst, are re-read each poll (`304`s while unchanged), are each gated exactly once, and never show `backlog`; these paging fixtures assert gating, cursor and `backlog` independently of the rate cap; at the default cap, the 61st event in an hour that the gate would otherwise admit is refused `rate_cap` with a receipt and no text, and the first of the next hour is admitted; a stream quiet for 48 fake-clock hours, counted from the first poll after `since` stops moving, answers every poll `304`, with no `200` until a new item arrives, and is never clamped; a held excerpt whose item is edited inside a skipped interval keeps its earlier text, its event stays unmarked, and (PRO-001d asserts) no pending fire of the watch remains; an item updated during a walk is gated once; a malformed item between two valid ones leaves two admitted, one counted as skipped, and `since` moved; a full page 1 whose 100th item lacks a timestamp, then a valid page 2, then a conditional re-read of the unchanged pages lose no valid item and fail no stream, with `P` the 99th item's `updated_at`; a page 1 that holds items but no valid timestamp fails that stream as a malformed response with its `since` held; a non-array response fails that stream with its `since` held while the other stream progresses; a stream whose ten polls each end on a short page answered `304`, and one whose ten polls each end on a short `200` page with no new item, never shows `backlog`; a restart between page commits resumes with no loss and no duplicate; recorded fixtures with a fine-grained repository token: the poller sends only the two stream requests per repository, counted hydrations and, for a repository with no issue object read in the last hour, at most one repository request an hour, a 304 admits nothing, and a busy stream beyond the budget resumes from its stored `since` next poll; with TUR-028's quiet on, the poller sends no request and every cursor and ETag is unchanged; with in-memory bodies, a page of 100 items with bodies at GitHub's 65,536-character limit in four-byte characters is gated completely with the poller's peak heap bounded near one element plus the page's kept records, not the page, and the same holds when every item also carries 100 label names of 50 four-byte characters; a 3-page issue walk whose 300 issues each carry those labels, followed by a comment page whose threads are all on those pages, served from a streaming fake that holds no page, sends no hydration, and at instrumented checkpoints (after each page commits) the live heap after `runtime.GC()`, less a baseline taken before the poll, stays within about 10.5 MiB, and returns to that baseline once that repository's comment stream ends; and its bytes fetched reported, repeated overlap re-reads are counted, and the poll stays within its transaction budget (Performance); an element over 1 MiB fails that stream for the poll as a malformed response, with its `since` held while the other stream progresses, and nothing is counted as skipped; 25 new comments on issues absent from the poll's issue page, each needing a label hydration, are gated 10, 10 and 5 over three polls, the first two behind, none skipped, `since` never passing an unhydrated comment, and no hydration spent on a re-read comment already in the ledger; the poller's per-page transaction budget is asserted; an issue and a comment whose body and label names hold the connection's token are kept with it replaced by as many newlines, the token is absent from every `watch_events`, `watch_threads` and refusal-receipt row, and `excerpt_revision` is the SHA-256 of the redacted body, while a Turing comment's body still matches its `integration_writes` row, and that issue, re-listed unchanged through the overlap, writes nothing and is not marked `observe_only`; an issue whose only edit is to its title, re-listed, leaves its `watch_events` row byte-identical (excerpt, `excerpt_revision`, label names, `observe_only` unset), deletes no pending fire and changes no proposal while the stream's `since` advances under the cursor rule, and no `watch_events`, `watch_threads` or refusal-receipt row holds the issue's title; a token in a `full_name` is in no scope entry and no thread session's title, and a save whose resolution answers it is refused; an issue page whose items' `repository_url` names the token, and a due repository request whose `full_name` holds it, each leave the scope entry's name byte-identical and `name_checked_at` unchanged, and a thread session opened after that poll is titled with the earlier name |
| PRO-001a3 | M | the poller's request deadlines (header, idle read and a 5-minute ceiling), the 10-minute poll deadline with a rotating starting repository (`last_repository_begun` on the watch row, written at most once when a poll ends or stops, only when it changes), 4 FIFO poll slots, failure isolation, the `failed_polls` and `access_failures` streaks, rate-limit handling and a per-connection reserve | a due repository request answered `502` writes no row and touches no streak, the next poll sends it again, and its `200` updates the name; a watch suspended for 48 fake-clock hours over a backlog that fits one poll's budget, then resumed by the owner, resumes from its stored `since`, gates only items created in the 24 hours before its poll and counts the older ones, which the list returns, `stale` on its status; over a backlog larger than the budget, its stream shows `backlog` after three behind polls, and the clamp then records a `skipped_interval` and deletes the watch's pending fires (PRO-001d asserts); a `403` with `x-ratelimit-remaining: 0` and a `429` with `retry-after` back off until the reset, keep the cursor and raise no row; three consecutive `429`s carrying neither header each back off at least one minute, exponentially, keep the cursor, neither suspend the watch nor raise a row; a `401` suspends the watch at once, without waiting for the failure threshold; two failed polls answered `404` followed by a success leave the watch running, and three in a row suspend it; a recorded denial `403` carrying `x-ratelimit-remaining` above zero and no `retry-after` counts as a failed poll, and three in a row suspend the watch; three polls of one stream, each answered `200` on page 1 and `404` on page 2, suspend the watch with `needs_person` after the third; a suspended watch's status reads `error` with `needs_person`, never `paused`, and raises a content-free attention row, while a watch the owner disabled, and an unsuspended one held by TUR-028's quiet, reads `paused` and raises none (section 21's status rule); three `5xx` polls show `error`, do not suspend the watch, set no `needs_person` and raise no attention row; with `x-ratelimit-remaining` below the reserve, no watch on that connection polls until the reset while an attended `github.*` call still runs; over a slow transport that delivers a page of 100 maximum-size bodies at about 1 Mbit/s, the page completes after more than 30 seconds, every request says `per_page=100`, no request is repeated, and every item on the page is gated; a connection that sends the headers and then no body bytes for 30 seconds fails that stream's poll as a transient failure, with `since` held and every validator dropped; a fake-clock transport that sends body bytes every 20 seconds and never finishes fails the request after 5 minutes as a transient failure, with `since` held, every validator dropped and `failed_polls` up by one; while a page arrives slowly for more than 30 seconds, an attended write on the shared database connection completes within its normal budget; with one watch's page stalled near its 5-minute request ceiling, a second watch's poll completes on schedule; with 5 due watches whose polls block, at most 4 polls and 4 requests are in flight, and the fifth starts when a slot frees, within 10 minutes; a poll that reaches its 10-minute deadline stops as behind, counts no failure, moves the cut stream's `since` only for its committed pages and drops the validators of the rest, while a stream it never reached keeps its `since`, validators and streaks and counts as behind, and 3 such polls in a row show `backlog` on it; a stream with `access_failures` at 2 whose next poll's request the deadline cancels before its headers arrive keeps both streaks at 2, and its next `404` suspends the watch with `needs_person`; with a fake clock, a watch whose first repository's streams are indefinitely slow (each page full and arriving in 4 minutes 50 seconds) and whose second repository is healthy sends the second repository's requests on the second poll and gates its new event there; after a deadline cut inside repository k, the next poll starts at repository k+1 and reads its issue stream before its comment stream, and a new comment there whose thread appears on that poll's issue page is gated with no hydration; a quiet poll of a 10-repository watch answered `304` throughout commits no transaction; after one failed poll of a stream, which drops its validators, the next poll sends that stream's request with no `If-None-Match`, is answered `200` with one short page holding one new item whose `updated_at` is within 5 seconds after the held `since`, so `since` does not move and the page's new validator is kept (point 2's `next` rule; every other stream answered `304`), finishes every repository, and commits only that page's transaction, which carries the stream's state row (its streak reset and its new validator), and no rotation write; the poll after it, with nothing new, sends `If-None-Match` for that stream, is answered `304` and commits no transaction; a crash mid-poll writes no rotation index, so the next poll starts at the same repository as the crashed one; a 10-repository watch whose first repository's issue stream returns full pages that each take about 4 minutes, while the other 9 repositories answer at once, begins every repository by the second poll and in every poll after that, counts each poll that cut or never reached a stream as behind for it (no such stream's behind count stays at zero), and shows `backlog` only on the first repository's two streams; three consecutive malformed responses, and separately three consecutive pages each carrying an element over 1 MiB, each show `error` on the watch's status after the third, with no suspension, no `needs_person` and `since` held, and one valid poll then resets both streaks; polls of one stream answered `404`, timeout, `404`, timeout and `404` show `error` from the third, without `needs_person`, and suspend the watch at the fifth, the third `404`, not before; the request pacer: 20 aligned 10-repository watches walking the 3-page `304` overlap send no more than 300 poller requests in any 60-second window plus the burst of 10, and an attended `github.*` call during that drain is sent without waiting for the bucket |
| PRO-001a4 | M | stopping a poll on a disable, deletion, suspension, scope edit, quiet, a `401`, a redirect on a stream request (never followed; suspends with `unexpected_redirect`) or a revoked or changed connection, the watch's `generation` fence, scope edits (seed an added repository at the edit's time, resolve its numeric id, which addresses every later stream request and hydration, refuse a repeated id, delete a removed one's state rows, reset the rotation index) | on an enabled watch, an item created after `enabled_at` but before its repository was added to the scope is never gated, a repository removed and re-added never gates an item created while it was out of scope (PRO-001d asserts neither fires on an `auto` watch); a scope edit sets `last_repository_begun` to −1, the next poll starts at the first repository of the new list, and a poll in flight across the edit makes no rotation write; an existing poll paused before a request, across a repository's removal, an event created in it while it was out of scope, and its re-add, then resumed, admits and fires nothing from the excluded interval and commits no event, page, state, rotation or suspension row, so the re-add's cursors, validators and rotation reset stand, and this test fails when the generation check is removed; adding a repository, at the watch's creation and on a later edit, sends exactly one `GET /repos/{owner}/{repo}` through the watch's connection and stores the repository's numeric id, which its events' resource ids carry; a resolution answered `404`, answered a redirect (not followed) or failed in transport refuses the save with a content-free reason and leaves the scope as it was, with none of that save's repositories added; a repository `200` decoded with a changed name whose name transaction is then stopped (quiet, a disable or a generation change before it commits) leaves `name_checked_at` unchanged, so the next poll sends the repository request again and then stores the new name; removing a repository deletes its scope entry, while a disable and re-enable keeps its id and name, and after an orchestrator restart the first poll sends at most one repository request per quiet repository, carrying no `If-None-Match`; a save naming `Owner/Repo` and `owner/repo`, in one list or one of them already in the scope, is refused with a content-free reason, and the scope, its state rows and its cursors are unchanged; on a multi-repository watch that has completed a poll, with a poll stopped in a repository other than the stored index while page 2 of a slow multi-page poll downloads, disabling the watch, deleting it, and separately turning quiet on each cancel that request and send no later request; under quiet the poll commits no event, state or page row and exactly one rotation write, its `since`, ETags and event rows are unchanged, and after quiet ends the next poll starts at the repository after the stopped one, while under the disable and the deletion, which change the watch's `generation`, the poller commits nothing at all, so the cursors stay as the disable or deletion left them (a disable drops them, point 2); on a one-repository watch after its first poll, quiet, and separately a disable, during page 2 commits no transaction after the stop, and revoking its connection during page 1 commits only the suspension; a disable and re-enable while page 1 downloads makes that page commit nothing, admits and fires no item created in the disabled interval, makes no rotation write and no suspension, and leaves the re-enable's `enabled_at` as each stream's `since`; a disable and re-enable committed with a direct row write after page 1's last byte is decoded and just before its page transaction's first statement, with the poll context's cancel held back until after that transaction's re-check, writes no event or page row and leaves the re-enable's `since` and ETags unchanged, and the same injection just before a deadline-cut stream's state-row transaction leaves that state row unwritten, and each of these tests fails when the in-transaction re-check is removed (section 6 adds the `auto` direct-fire case and quiet in a page transaction, built the same way); a `401`, and separately a connection revocation, that arrives before a disable, and separately a deletion, but whose suspension commits after it writes nothing, neither the suspension nor a rotation write, including on a multi-repository watch stopped outside the stored index, so the watch reads `paused` (or deleting), with no `needs_person` and no attention row; a `401` that arrives before quiet is turned on but whose suspension commits after it still suspends the watch with `needs_person` (Stopping a poll, the suspension write's predicate), and while quiet stays on the watch reads `error` with `needs_person`, not `paused`, and has one content-free attention row (section 21's status rule); a `401` on the first stream sends no later request of that poll; a stream answered `301` sends no redirected request and no later request of that poll, holds its `since`, and leaves the watch reading `error` with `needs_person` and one content-free attention row naming `unexpected_redirect`; a comment whose label hydration is answered `301` (a transferred issue) is skipped, no redirected request is sent, the watch stays unsuspended and no attention row is raised; a poll stopped by quiet while its first request inside repository k is in flight, before any page commit there, and one whose first request there fails, each make the next poll start at repository k+1; revoking the watch's connection while page 1 downloads, and separately replacing its sealed credential, sends no later page or hydration request, commits no event, state or page row and no write other than at most the rotation index (only when it changes) and the suspension, leaves `since`, ETags and event rows unchanged, and suspends the watch with `needs_person` and the cause `connection_unavailable`; a revocation committed just before a page transaction's first statement rolls that page back and suspends the watch, with no second `Begin` and no `r.db` call while the transaction is open, and an attended write queued behind that commit completes; revoking the watch's connection between polls makes the next poll send no request, write no rotation index, commit no event, state or page row, and suspend the watch with `needs_person` and the cause `connection_unavailable`; on a multi-repository watch, removing a repository from the scope after its page 1 commits, while the watch stays enabled, stops the poll: it sends no later request, counts no failure and commits nothing more (no event, page, state or rotation row), and the same removal committed with a direct row write just before a page transaction's first statement, with the poll context's cancel held back until after that transaction's re-check, rolls that page back, and each of these fails when the generation check is removed (Stopping a poll); the watch rows of section 8 (UX-002a ships first and has no watch source): a watch suspended because its connection was revoked, mid-download and between polls (found before any request), raises exactly one `person/blocked` row naming the watch and the cause `connection_unavailable`, a watch suspended by a redirect on a stream request raises exactly one naming `unexpected_redirect`, a row that omits the cause fails the test, and a suspended watch's row cannot be dismissed; and section 8's paging across stage 3's two kinds: 101 approvals older than a blocked automation and a suspended watch put both on page 2, in point 2's total order, although each is newer than the cursor; with page 1 ending on a stage-3 automation row, a watch row with the same `since` and a smaller id still appears on page 2; with 100 suspended watches newer than one blocked automation, page 1 ends on a watch and page 2 shows that automation; with page 1 holding a watch row newer than the automation row it ends on, and an older watch row left over, page 2 shows the older watch and not the newer one again; each fails on any missing or repeated row |
| PRO-001a5 | S | `integration_writes` written before each comment POST | a comment Turing created is refused, the owner's own comment is admitted, and a crash after GitHub accepts a comment but before its id is recorded still has the polled comment refused as Turing-authored by its body hash, including when the repository was renamed between the approval and the poll, so the `uncertain` row names the old repository and the comment has no recorded id; a crash after the write-ahead commit leaves a `pending` row that startup turns `uncertain`, with no second POST; a `422` makes the row `rejected`, nothing is retried, and the owner's later identical comment on that issue is admitted, not refused as Turing-authored |
| PRO-001a6 | S | content-free refusal receipts with a 24-hour horizon and an hourly cap, the per-watch excerpt budget, lifecycle sweep for excerpts | an edit after the excerpt expired stores no text; a poll stopped by the hourly receipt cap in the middle of page 2, in the last minute of an hour and with `since` unchanged, stores no validator for page 2 or 3, and the first poll of the next hour sends page 2 without an ETag, receives `200` and gates every item the stopped poll left unread; with 64 MiB of excerpts held, the next admitted event is refused `excerpt_budget` with no text, and `watches.excerpt_bytes` always equals the sum of held excerpts and label names; an event whose label names, its own or its thread's through a page or a hydration, pass 20 KiB is refused `label_budget` with a receipt and no text, while one carrying 100 names of 50 four-byte characters is admitted with all of them and charged their bytes; with small bodies and that many labels, admitted events stop at `excerpt_budget` once excerpts and label names reach 64 MiB, and clearing an excerpt subtracts its label names' bytes too; with the watch's excerpts just under 64 MiB, an edit that grows a short held excerpt past the budget clears it with its `excerpt_revision`, label names and (PRO-001b asserts) the proposal's derived fields, marks the event `observe_only` and (PRO-001d asserts) deletes its pending fire, keeps `watches.excerpt_bytes` at most 64 MiB and equal to the bytes held, and leaves no sentinel from either the old or the new body in the event or its proposal, while an edit that fits is charged only its byte delta; a sustained-spam fixture that feeds one watch 10,000 refused events per poll for 48 fake-clock hours writes at most 1,000 receipts an hour, stops that watch for the rest of the hour with its cursors held, counts each stopped poll as behind, never holds more than 25,000 receipts younger than 24 hours nor 26,000 in all, deletes each due receipt within one maintenance tick in bounded sweep transactions, and never gates a re-listed old event again; with a fake clock, an excerpt older than 30 days is cleared with its event's label names and (PRO-001b asserts) its proposal's derived fields while ids and outcome codes remain, and a sentinel label name stored on the event is found nowhere in the database afterwards; deleting a watch disables it at once, refuses its re-enable, and the sweep clears its excerpts and label names in bounded batches; with `TURING_JOB_REAPER_INTERVAL_MS=0`, the maintenance tick still runs at one minute, so a fake-clock excerpt older than 30 days is cut and (PRO-001b asserts) a due thread session's withdrawal begins, and an `integration_writes` row older than 30 days is deleted on an installation that has never created a watch; with 20 watches each held at the hourly receipt cap for 48 fake-clock hours and `TURING_JOB_REAPER_INTERVAL_MS=0`, every watch stays at or under 25,000 receipts younger than 24 hours and 26,000 in all, no sweep transaction deletes more than 100 rows, no tick spends more than about 1 s on the sweep, and a due excerpt is still cut on time; cohorts of 1,000 receipts admitted through the real hourly gate at 00:59:45 and at 59:45 past each following hour through 23:59:45, then at the next day's 00:00:00, are 25,000 held at once, none deleted before its 24 hours; with the maintenance tick held from the next day's 00:59:30 to 01:00:15, the first cohort comes due at 00:59:45 and stays, a cap-sized burst the gate admits at 01:00:00, in a new cap hour, makes 26,000 at that instant only, and the tick at 01:00:15 deletes the due cohort, leaving 25,000; with 500,000 retained admitted rows across those watches, `EXPLAIN QUERY PLAN` shows each due-selection query using its partial index, and each batch transaction stays within 50 ms on the CI runner; deleting a watch with 500,000 admitted rows and 25,000 receipts removes them in sweep transactions of at most 100 rows each, deletes the watch row last, and an attended `SendMessage` issued during it commits within 1 s; the deletion's excerpt selection uses the `(watch_id, thread_key, dismiss_generation)` index in `EXPLAIN QUERY PLAN` and each batch stays within 50 ms over that fixture with 43,200 held excerpts in another watch; with `TURING_JOB_REAPER_INTERVAL_MS` set to 2 hours, the maintenance tick still runs each minute, the 25,000/26,000 receipt bound holds, a 30-day excerpt is cut on time, and (PRO-001b asserts) a due thread session's withdrawal begins on time and a sentinel in that session's rows is gone within about a minute of the begin; during the 500,000-row watch deletion, a second watch at the hourly receipt cap keeps its 25,000/26,000 bound and gets a due excerpt cut on time, because deadline-bound work runs first in each tick |

PRO-001a ships as these six pull requests, PRO-001a1 to PRO-001a6, in that order, and elsewhere in this document "PRO-001a" names them together. A clause marked "(PRO-001b asserts)", "(PRO-001c asserts)" or
"(PRO-001d asserts)" in these rows checks a proposal, an assessor field, a
thread session or a pending fire, none of which PRO-001a creates; the
named slice writes that check, and PRO-001a's test checks the rest of the
assertion.

PRO-001w1, PRO-001w2 and PRO-001b to PRO-001d are in sections 5 and 7.

### Open questions

None. The global pause asked about in the previous revision is TUR-028, the
owner's quiet switch (section 18); per-watch enable and mode stay.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Where the gate lives | grok: CHN-000 amendment; mai: new ADM-001 for channels; sol and opus: new PRO-001 | PRO-001; channels unchanged | CHN-001 direct messages have no inferred case, so a channel gate adds nothing today. The first real consumer is an account watch. |
| Default mode | dsh and mai: `observe`; grok, sol, opus: `off` | Disabled by default; `observe` is the default mode once enabled | Polling is off-machine traffic and attention cost; the owner opts in per watch. |
| Does `observe` spend a model call | dsh, mai: yes; grok: never; sol, opus: one bounded local call | Deterministic rules spend none; the optional assessor (section 5) spends one, budgeted | Keeps the default path free and makes the model cost an explicit owner choice. |
| First source | opus: GitHub; sol: an actual monitor first; mai: a channel | GitHub, read through per-repository issue and comment streams | It is the only event source Turing can reach today; channels are unbuilt. |
| Unlisted actors | dsh: admission cells; grok, opus: always refused; sol: refused for channels | `listed` by default; `anyone` observe-only | Issues from strangers are much of a watch's value, but they must never trigger unattended work. |
| Round 1 review: notifications or repository streams | grok, sol, opus: notifications refuse the recommended fine-grained token and lack actor and labels, so each needs a hydration request | Per-repository `/issues` and `/issues/comments` with `since` and ETag, an explicit request budget, and a visible pause on a permanent 403 (since Phase 2 a header-less `403` counts toward the failure threshold, and since round 9 a failing watch is suspended, not paused; see those rows) | DeepSeek's own repository streams do the same (`dsh:src/sources/github.ts:198-249`); they carry the gate's inputs and work with the token Turing recommends. |
| Round 1 review: self-origin | opus: refusing the connected login refuses the owner's own activity | Refuse only platform ids Turing wrote (`integration_writes`) | Turing writes with the owner's token, so login cannot tell them apart; the only write tool returns the id it created. |
| Round 1 review: excerpt retention | mai, sol, opus: copies in assessor sessions and run transcripts outlive the 30-day bound | One withdrawal path (now point 8) over the existing session deletion lifecycle; watch-held thread sessions | A purge that misses one copy is no bound; reusing the deletion lifecycle avoids a second cleaner. |
| Round 1 review: policy hash | opus: duplicates TUR-026's direct re-read | Dropped; Start, `auto` and TUR-026 re-read the current rows | A direct re-read answers the same question with no hash to keep in step. |
| Round 2 review: who acted, and which update is an event (WATCH_SOURCE_ACTOR_MISMATCH, WATCH_UPDATE_DEDUP_ID, pro001a-issue-stream-actor-and-update-identity) | sol, opus: an issue snapshot's `user` is its author, not whoever applied a label, and the ledger had no identity for updates | Events are creations attributed to their authors; labels are a predicate over current labels; "when I label" is dropped; `resource_id` is kept apart from `platform_event_id` | It is DeepSeek's own rule (`dsh:src/sources/github.ts:228-232,352-353`). The issues-events endpoint would attest label actors but adds a third stream per repository for one trigger shape; separating the two ids lets a later release admit edits without a migration. |
| Round 2 review: expired excerpts in an active thread (WATCH_TRANSCRIPT_EXPIRY, pro001-active-thread-retains-expired-excerpts) | sol, opus: withdrawal waited for the thread's last excerpt, so an active thread kept old excerpts forever | A thread session is withdrawn 30 days after its first excerpt; a detached session has each `watch_event` row and its job's `userText` cut on the excerpt's own clock | Bounding the session by its oldest excerpt removes the answers derived from it too, which a per-row cut alone cannot; the per-row cut covers the one session that outlives the thread's lifecycle. |
| Round 2 review: stranger text in search and recall (pro001-thread-session-recalled-as-chat, pro001-thread-session-text-reaches-automatic-recall) | grok, opus: thread sessions are chats, so they are indexed, recalled and title-backfilled | Excerpts are role-system `watch_event` rows that never match; thread sessions match nothing until detached; titles come from ids | One predicate already serves search and recall, so two exclusions there close both; a new session kind would hide the conversation from the owner and refuse every owner action on it. Amended in round 30: a title carries the issue number and the repository's display name, which follows the repository's id (see that row). |
| Round 2 review: owner engagement lifted the limits (pro001-engagement-lifts-event-limits, pro001-owner-message-silently-lifts-watch-limits) | grok, opus: any owner message turned the session into a full chat in which later stranger events got a full turn | Limits stay on every turn until the explicit, audited Treat as normal conversation, which detaches the session; later events open a new thread session | A stranger's event never starts a turn with more than its rule's limits, and lifting them is a deliberate owner act. |
| Round 2 review: `off` mode (pro001a-off-mode-redundant-with-disabled) | opus: `off` duplicated `enabled = false` | Dropped; the enable switch plus `observe` or `auto` | One state, one meaning. |
| Round 2 review: initial cursor (pro001a-initial-cursor-undefined) | opus: the first `since` and re-enable were undefined | `enabled_at`; no backfill; re-enable starts again | DeepSeek does the same per switched-on kind (`dsh:src/sources/github.ts:198-201,207-208`); a backfill would admit events the owner never opted into. |
| Round 2 review: rate limits read as permanent (pro001a-403-rate-limit-misclassified-as-permanent) | opus: GitHub signals rate limits with `403` too, and watches share the owner's token | Headers decide: rate limits back off; a per-connection reserve keeps attended headroom | A watch paused by a busy hour would be a false alarm, and a watch that exhausts the token would break the owner's own calls. |
| Phase 2 F10: which `404` and `403` pause a watch | grok: `401` and any non-rate `403` pause at once, `404` after 3; opus: only `401` at once, other `403`, `404`, `410` after 3; sol, mai: `404` and transients after 3 | Opus's classes (since round 9 they suspend the watch rather than pause it; see that row) | GitHub documents secondary limits that carry neither header and asks for a wait of at least a minute, so a header-less `403` is not proof of a permanent denial; a watch paused by one such answer would be a false alarm. Three polls is at most a few minutes. |
| Phase 2 F10: what an edit does | sol, grok, opus: replace the stored excerpt in place; grok: the retention clock restarts; sol: it does not; mai: re-gate the edit as a new event | Replace in place, no new event, clock unchanged; edits as events wait for a later release with an unknown actor | Round 2 made this release creation-only. Replacing keeps one copy and drops text the author withdrew; keeping the first clock is the stricter bound. Opus found that anyone with write access can edit another's comment, so an edit can never attest its actor. |
| Phase 2 F10: equal timestamps | opus: DeepSeek's cursor stalls on a full page sharing one timestamp; sol: finish ties before advancing; mai: tie-break by id | Superseded in round 8 (see that row). Then: step `since` by the last processed item, and page at the same `since` only on a full equal-timestamp page | GitHub's list has no id tie-break; this is the smallest walk that always progresses and never offsets across a changing list except on an exact tie. |
| Phase 2 F10: resource ids across repositories | synthesizer: `issue:{number}` collides across the repositories of one watch | Namespace by GitHub's numeric repository id | A second repository's issue #5 would otherwise be dropped as a duplicate. |
| Phase 2 F20: closing the crash window | grok, sol, mai: an HMAC marker in the comment; opus: a body hash written before the POST | The write-ahead body hash (section 22) | It closes the same window without publishing anything in the owner's comments. |
| Round 3 review: pinned memory in thread sessions (WATCH_PINNED_PROFILE_CONTEXT) | sol: `skip_automatic_recall` stops only transcript recall, while an ordinary job still carries the pinned persona and profile into the prompt | Every thread-session job freezes both as withheld, as `CreateDelegation` does, until Treat as normal conversation | It reuses the specialist path's existing withheld snapshot; withholding the persona too avoids drawing a privacy line inside an owner-written file that a stranger-steered turn does not need. |
| Round 3 review: an edit wrote cleared text back (pro001-edit-replacement-resurrects-cleared-excerpts) | opus: the replace path re-wrote an excerpt that expiry or Dismiss had cleared, and section 13 stated the rule differently | Replace only a held excerpt, under 30 days old, on a thread that is not dismissed, and only when the text changed; point 2 is the one statement, and section 13 points to it | Expiry and Dismiss are promises that no text is kept; an edit must not undo them. |
| Round 3 review: refusal receipts grew without bound (WATCH_REFUSAL_LEDGER_UNBOUNDED) | sol: every refused event kept a row forever, and the admission cap bounds only admissions | Receipts live 24 hours; an item created more than 24 hours before the poll is skipped as `stale`; at 1,000 receipts an hour the watch stops for the hour with its cursors held | Every gated event keeps its dedup receipt for its whole horizon, so nothing old is replayed, and storage is bounded at 24,000 receipts per watch (25,000 since round 62, which counts the hour the cap resets in; see that row). Aggregating the excess into a counter instead would lose dedup for those events, and a dismissed thread's refused event could then be admitted after Watch this thread again. |
| Round 3 review: excerpt storage and full-text cost (pro001-excerpt-storage-and-fts-cost-unstated) | opus: the caps allowed about 675 MiB of excerpts per watch, and every `watch_event` row was full-text indexed although search never returns it | A 64 MiB per-watch excerpt budget, counted in the transactions that change excerpts, and a `WHEN` clause that keeps `watch_event` rows out of `messages_fts` | A stated ceiling, and no index work for rows that can never match. |
| Round 4 review: quiet or a pause against the stale skip (quiet-resume-proposals-lost-to-stale-skip, pro001-stale-skip-drops-quiet-period-events) | grok, opus: sections 7 and 18 promised a proposal for every event created while quiet, which the 24-hour `stale` skip drops after a day | The stale skip wins: after quiet, a pause (a suspension since round 9) or a backlog, only items created within 24 hours of the poll are gated, and older ones are counted `stale` on the watch's status (since round 10, an interval the backlog clamp skips is recorded as a content-free `skipped_interval`, not counted); point 2 states it, sections 7 and 18 point to it | Exempting quiet-period items would need a per-item exception to the one bound that keeps receipts finite and old text out; dropping day-old events is the privacy-first choice, and the count keeps it visible. |
| Round 4 review: an edit after admission kept `auto` eligibility (WATCH_PENDING_EDIT_BYPASSES_AUTO_ACTOR) | sol: a listed author's comment admitted unedited and held as a pending fire could be edited by an unlisted writer and then fire under the author's eligibility | Any replacement sets a permanent `observe_only` marker and deletes the event's pending fire in the same transaction; `FireEventAutomation` refuses a marked event | Only the first-seen-edited case was covered; the edit's actor is unknown either way. |
| Round 4 review: a quiet stream read as a stall (pro001-304-backlog-ambiguity) | mai: a `304` keeps the cursor still, so three quiet polls could show `backlog` | Superseded in round 9 (see that row). Then: only a stalled keyset counts: more to read and the cursor did not move; `304`s and complete polls are progress | `backlog` must mean work is waiting, not that nothing happened. |
| Round 4 review: search cost (pro001-search-perf-stale-content-type-check) | opus: the Performance bullet still charged a content-type comparison that round 3 moved into the FTS triggers | One index probe per candidate row | Accuracy. |
| Round 4 review: what the detached conversation keeps (pro001-detached-answer-retention-contract) | mai: "no copy outlives its source" was not exact, because Turing's answers in a detached conversation may quote an excerpt and are kept | One stated exception, the owner's explicit Treat as normal conversation, whose dialog says the answers are kept as an ordinary chat; until then the whole thread session is withdrawn with its oldest excerpt | Scrubbing quotes out of answers cannot be done reliably; making the exception an informed owner act keeps the rule exact. |
| Round 5 review: deleting a watch orphaned its thread sessions (pro001-watch-delete-orphans-thread-sessions) | opus: deleting the watch deleted its `watch_threads` rows at once while the sweep withdrew the sessions later, so in between they matched search and recall, lost their limits and could not be found by the sweep | Deleting disables the watch and marks it `deleting`; the rows stay, so the exclusions and limits hold, and an owner send there is refused; the sweep begins each session's deletion and only then deletes its row | Beginning every session's deletion inside the delete transaction would take one session lock and one cancel per session in a single transaction that grows with the watch's threads on the one database connection; keeping the rows gives the same guarantees with the sweep's existing bounded batches. |
| Round 7 review: GitHub's `since` is exclusive (pro001-since-exclusive-drops-shared-timestamp, PRO001_GITHUB_SINCE_EXCLUSIVE) | grok, sol: the walk treated `since` as inclusive and advanced it to the last item's `updated_at`, so unread items sharing that second were skipped for good | Superseded in round 8 (see that row). Then: after a full page ending at second `T` the next request is `(T − 1 s, 1)`, paging at a fixed `since` only when the whole page lies in one second; a poll stopped by its budget stores its next request as is; a stream drained on page 1 stores `(L − 5 s, 1)`, and one drained inside a second holding more than a page stores the same request again; fixtures serve strictly `updated_at > since` | The host's alternative, a fixed `since` with pages 1 to N for the whole poll, was weighed and not taken: offsets across a whole poll shift whenever an item is updated mid-walk, skipping one item per later page boundary, which the end cursor then passes for good. Re-reading second `T` costs only unique-index probes, and storing the next request lets an oversized second finish across polls instead of stalling. |
| Round 7 review: tool records in a detached session (WATCH_DETACHED_TOOL_RETENTION) | sol: the detached sweep cut only the `watch_event` row and the job's `userText`, while event text can be copied into tool-call and approval arguments | Treat as normal conversation is an informed promotion of the session's whole run history, tool-call arguments and approval records included, and its dialog says so; before detachment those records go with the thread session, because they cascade from its runs; a sentinel test covers both sides | Scrubbing arguments would rewrite audit-bearing records whose hashes bind approvals; the owner's explicit, disclosed act keeps the rule exact. |
| Round 8 review: the stored page cursor (pro001-drained-page-cursor-misses-later-events, PRO001_GITHUB_TAIL_CURSOR_NOT_ADVANCED, pro001a-drain-cursor-moves-backward-loop) | grok, mai: a drained walk stored a page offset that repeated one tail request forever and missed later items; opus: storing `(L − 5 s, 1)` could move `since` backward, so a quiet stream never earned a `304` | DeepSeek's rule, stated once in point 2: store `since` only, never a page, and start every poll at page 1; `next = max(newest − 5 s, since)` (since round 11, only after a walk of one page; see that row); page at a fixed `since` only within one poll and (until round 10, which pages whenever a page is full) only inside a five-second burst; a burst larger than the poll's budget shows `backlog`; while `since` is unchanged the state row keeps one ETag per requested page (at most 3), each sent only on its identical request, and a `304` to a full page continues the walk | Three rounds of page cursors each found a new way for a stored offset to drift. DeepSeek's verified rule (`dsh:src/sources/github.ts:211-248`; `dsh:src/sources/poll.ts:15-16`) stores no offset and never moves backward; paging inside a burst removes the stall DeepSeek has there (section 13). The host's guidance kept one ETag whenever `since` is unchanged, as DeepSeek does, but after a burst walk a `304` to the unchanged full page 1 would hide a later item on page 2, which is DeepSeek's own stall; dropping the ETag instead would re-read the burst on every quiet poll, the cost opus raised. One validator per page keeps both: a quiet stream after a burst costs at most 3 `304`s, which GitHub does not count against the primary rate limit, and a later item still changes page 2's answer. |
| Round 8 review: a same-second edit (WATCH_SECOND_RESOLUTION_EDIT_IDENTITY) | sol: `updated_at` has one-second resolution, so a comment edited in the second it was created passes as unedited, and two bodies sharing an `updated_at` were one revision | `excerpt_revision` is the SHA-256 of the decoded body (round 86: after the credential redaction round 85 added), and any differing hash replaces the excerpt and marks the event `observe_only`; equal timestamps are no longer read as proof, and `auto`'s acknowledgement states that text altered before Turing first reads it, or in the same second, is attributed to its author | A revision bound to content cannot collide on a clock tick. Nothing can detect an edit Turing never saw, so the honest rule is a disclosed limit, not a guarantee the timestamps cannot give. |
| Round 9 review: two progress rules and an unbounded stall (pro001-full-page-304-counted-as-progress, pro001a-burst-304-progress-contradiction-hides-backlog) | grok, opus: point 2 called a full-page walk with `since` unchanged no progress, while a later sentence called every `304` progress, so a burst over the budget, answered by three full-page `304`s each poll, never showed `backlog`, and nothing ever unstuck its `since` | Superseded in round 10 (see that row). Then: one definition in point 2: a poll made progress when `since` moved or its walk ended on a short page (`200` or `304`); a full-page budget stop with `since` unchanged is no progress whatever the status codes. Each poll clamps `since` to at least poll start minus 24 hours and drops the validators when the clamp moves it | DeepSeek bounds the same stall with its 14-day horizon (`dsh:src/sources/github.ts:209-210`; `dsh:src/sources/poll.ts:6`). Turing's 24-hour `stale` rule already refuses anything the clamp passes, so the bound costs no eligible event and needs no new state. |
| Round 9 review: quiet-poll tests (pro001a-quiet-poll-304-fixtures-unsatisfiable) | opus: the poll after `since` moves sends no ETag and re-reads the overlap with `200`s, so tests expecting `304` on every quiet poll could not pass | Both quiet tests count from the first poll after `since` stops moving, and point 2 says the overlap re-read answers `200` | The validator rule was right; the tests were not. |
| Round 9 review: the edit guarantee against its disclosed limit (PRO001_EDIT_EVAL_CONTRADICTS_DISCLOSED_LIMIT) | sol: EVAL-001, section 13 and NORTH_STAR promised that no outsider's edit becomes unattended work, while point 2 admits a same-second edit before intake unmarked | Every edit guarantee names detectable edits (a body hash changed after intake, or a comment first read with `updated_at` later than `created_at`); PRO-001d and EVAL-001 add an end-to-end case for the undetectable same-second edit before intake, asserting the acknowledgement's disclosure and that a run it starts stays under the unattended read-only ceiling | A guarantee the timestamps cannot give must not be a release criterion; the ceiling is what bounds the undetectable case, so the test checks the ceiling. Widened in round 13: every item kind first read with `updated_at` later than `created_at` is marked, and the undetectable case is an edit in the second an item was created (see that row). |
| Round 9 review: a source failure reported as the owner's pause (HEALTH_SOURCE_FAILURE_PAUSE_MISCLASSIFIED) | sol: a `401`, or three `403`/`404`/`410` polls, "paused" the watch, which section 21 defines as the owner's healthy act, so a revoked credential read as an intentional pause, and a `401` stopped polling before the threshold could count it | A source failure suspends the watch: `error` with `needs_person`, never `paused`, and a stage 3 row. A `401` is definitive and suspends at once; the other classes need 3 consecutive failures (superseded in rounds 17 and 18: 3 access failures with no poll answered `200` or `304` between; see those rows). `paused` is only a disabled watch or TUR-028's quiet | Whether a watch works and who stopped it are different facts; a revoked token must reach the owner as a problem, not a choice. |
| Round 10 review: the clamp moved quiet streams, hid what it skipped, and paging stalled near a burst (pro001-clamp-stale-count-unsatisfiable, pro001-clamp-skips-held-excerpt-edits, PRO001_MOVING_HORIZON_DEFEATS_ETAGS, PRO001_CLAMP_MAKES_STALE_COUNTS_UNOBSERVABLE, pro001a-unfloored-clamp-defeats-quiet-304, pro001a-near-burst-paging-throughput-collapse, pro001a-5xx-acceptance-still-says-pause) | grok, sol, opus: the unrounded clamp moved a quiet stream's `since` on every poll after a day, dropping its ETags; tests counted `stale` items the clamped exclusive `since` never returns; an edit the clamp skipped left old text and an armed pending fire; pages 2 and 3 were read only when `since` could not move, so a stream just under burst density admitted a few items a poll and never showed `backlog`; grok: the `5xx` test's "without a pause" no longer locked the rule | Point 2, stated once: a poll pages whenever a page is full, up to 3; a poll stopped by a budget is behind whether or not `since` moved, and 3 behind polls show `backlog`; only a stream in `backlog` is clamped, to the hour floor of poll start minus 24 hours (DeepSeek's floor, `dsh:src/sources/github.ts:199-200,209-210`); a clamp that moves `since` drops the validators, records a content-free `skipped_interval` with no count, and deletes the watch's pending fires; an edit inside the interval is not read, the one stated exception to replacement; resuming from the stored `since` counts returned day-old items `stale` (after a disable and re-enable, that `since` is the new `enabled_at`, so a disabled period is still never read); the `5xx` test asserts no suspension, no `needs_person` and no attention row | A quiet stream keeps one stable request and costs only `304`s; counts are promised only for items the list returns; the skipped edit is disclosed instead of silently kept, and no pending fire can outlive an edit it cannot see. The 1,000-item fixture fixed the throughput at 225 new items a poll after the overlap (superseded in round 11 by 90 a poll; see that row). |
| Round 11 review: shift loss, partial-page validators, the rate cap and DeepSeek's floor (pro001a-full-page-paging-makes-walk-shift-loss-routine, PRO001_PARTIAL_PAGE_VALIDATOR_LOSS, pro001a-paging-fixtures-contradict-admitted-rate-cap, pro001-dsh-floor-citation-wrong-lines, pro001a-dsh-horizon-floor-citation-lines) | opus: since round 10 a busy poll walks several pages, so the stated ceiling (an item a mid-walk update or deletion shifts back across a page boundary is passed by `N − 5 s`) silently dropped events in ordinary polls, and the paging fixtures expected hundreds of admissions against a default cap of 60 an hour; sol: a validator kept for a page the receipt cap stopped part-way answered `304` next time and skipped its unread items; grok, opus: the floor was cited at the `readStream` signature | After a walk of several pages, `next = max(since, min(N − 5 s, P − 1 s))`, `P` being page 1's last item's `updated_at` (refined in round 23: the last item on page 1 with a valid timestamp; see that row), kept in page 1's validator; a page's validator is stored only once every item on it is committed, and a stopped page drops its own and later ones; the paging fixtures say "gated exactly once (admitted or refused with a receipt)" and assert cursor and `backlog` apart from the cap, with a `rate_cap` test for the 61st event, a shift fixture and a receipt-cap regression across an hour; the floor is cited at `dsh:src/sources/github.ts:199-200,209-210` | Opus's own bound, the first item of the last page read, protects only the last boundary: an item shifted across the boundary between pages 1 and 2 sorts before that item and would still be lost, so the bound uses page 1's last item, the earliest point any shifted item can sort at (review dispositions appendix). The loss is gone at the cost of throughput behind a budget, under 100 new items a poll, which `backlog` keeps visible. |
| Round 12 review: event text invoking an owner skill (WATCH_EVENT_EXPLICIT_SKILL_INVOCATION) | sol: event text sits in `userText`, enqueue freezes every enabled skill onto the job (`orch/repository/jobs.go:1126,1172`), and the runtime injects the body of a skill whose `$<skill-id>` appears in `userText` (`agent-runtime-go/internal/agent/skills.go:87-110`), so a GitHub comment could load an owner skill | Every thread-session job (Start, `auto`, an owner turn before Treat as normal conversation) freezes an empty skill snapshot, as the assessor's does; scheduled automations are unchanged; a real-worker capture test puts a private sentinel in an enabled skill and its `$<skill-id>` in the event text and finds no skill body in the request | An empty snapshot closes both the explicit invocation and the skill tools, which read only that snapshot (`agent-runtime-go/internal/agent/general_assistant.go:1049-1051`), with no new runtime rule. |
| Round 13 review: first-read edits, the loop guard's key, label retention and header-less `429`s (pro001-issue-edit-before-first-read-auto, f20-repository-rename-loop-guard-ceiling, WATCH_LABEL_SNAPSHOT_RETENTION, pro001a-headerless-429-unclassified) | mai: the REST issue object has no body-edit time, and only a comment first read with `updated_at` later than `created_at` was marked, so an issue edited before its first read could fire `auto` under its author's eligibility; mai: the hash match keyed by the lower-cased repository name missed an `uncertain` comment after a rename; sol: stored label names, third-party text, outlived the excerpt; opus: a `429` with neither rate-limit header fell into no status class | One first-read rule for every item kind: an issue, pull request or comment first read with `updated_at` later than `created_at` is admitted `observe_only`, whatever changed it; the loop guard matches `(connection_id, issue_number, body_sha256)` (superseded in round 14: `(issue_number, body_sha256)`, with no connection; see section 22's log) and ignores the repository name, which stays only for the notice's link; clearing an excerpt clears its event's label names too, on expiry, the Dismiss sweep and watch deletion; any `429` is rate limiting (at least one minute, doubling, cursor kept, no failure, no row); tests for each | Each change fails closed or deletes state: an item changed before Turing reads it cannot be attributed, an over-broad hash match only refuses more, labels need no life beyond the excerpt that the predicates read with them, and GitHub documents a header-less limit. |
| Round 14 review: response bytes, the hydration budget and the loop guard's key (PRO001_POLLER_RESPONSE_BYTE_BUDGET, pro001a-hydration-budget-stop-unclassified, f20-loop-guard-connection-key-misses-cross-connection-uncertain, f10-403-rate-limit-header-class-matches-every-403, PRO001_ASSESSOR_INPUT_DISPATCH_REVISION_FENCE) | sol: the poller bounded requests and stored excerpts but not response bytes or decoding memory, so one page of maximum-size bodies is about 26 MiB; opus: a poll stopped by the 10-hydration budget was neither behind nor progress; opus: the loop guard keyed on the connection missed a comment posted through another; opus: section 13 restated the `403` rule loosely; sol: a queued assessment could send stale text | Point 2 decodes each page with INT-009a's streamed decoder (one element at a time, 1 MiB per element, an over-bound element skipped and counted like a malformed one (superseded in round 15: it fails the stream's poll as a malformed response; see that row), only the gate's fields and the first 16 KiB of the body kept, the body hash computed as decoded), Performance states bytes per page and per poll and the 30-second client timeout (a timed-out page drops its validator and holds `since`; superseded in round 16: poller requests use point 2's request deadlines instead of that timeout; see that row); the hydration budget joins the budgets that make a poll behind, and `since` never passes an unhydrated comment; point 4 matches `(issue_number, body_sha256)` with no connection (section 22); section 13 and section 5 carry the other two; fixtures for each | Peak memory is about one element whatever a page holds (superseded in round 17: one element plus the page's kept records, about 2.6 MiB; see that row); every budget stop is now classified the same way; and the guard's key is what the comment itself carries. |
| Round 15 review: the page size under the timeout and the over-bound element (pro001a-max-page-cannot-meet-30s-timeout, pro001a-overbound-element-skip-incompatible-with-reused-decoder) | grok: a worst-case page of 100 maximum-size bodies, about 26 MiB, needs about 7.3 Mbit/s within the 30-second timeout, and a timeout held `since` and sent the same request again, so on a slower link its items were never gated; opus: `json.Decoder.Decode` buffers a whole value and a mid-value reader error is sticky, so the reused decoder cannot read past an over-bound element and go on | Each stream's state row holds its `per_page` (default 100) (superseded in round 16: `per_page` is fixed at 100, with no per-stream column, no halving and no floor, and poller requests use header, idle-read and 5-minute deadlines instead of the 30-second timeout; see that row); a request that times out or fails mid-body drops the validators, holds `since` and halves `per_page`, down to 12, which returns to 100 once `since` moves, and a change of `per_page` drops the validators; a timeout at the floor is a network failure (`error` after 3, no suspension, no `needs_person`); an element over 1 MiB is INT-009a's typed error, treated as a malformed response (that stream fails this poll, cursor held), and its skip counter is removed; the fixture is split into an in-memory heap bound and a slow-transport walk | A smaller request is the one change that lets a slow link make progress without a second timeout or decoder, and a link too slow even for 12 bodies (about 0.84 Mbit/s) is visible as `error`. GitHub caps a body at 65,536 characters, so no real element reaches 1 MiB, and the skip path handled an impossible input. Bandwidth is in decimal megabits, as section 15 states its figures; the reviewer's 6.9 is the same rate in binary units. |
| Round 16 review: the page-size cycle and the failure classes (pro001a-above-floor-timeout-still-counts-as-failure, pro001a-malformed-response-failure-class-unassigned, pro001a-per-page-reset-repeats-timeout-cycle) | grok: point 2 said a timeout above the floor counted nothing, while the classifier counted every network failure; opus: a malformed response, including an element over 1 MiB, had no failure class and overlapped "fails mid-body", while DeepSeek counts a stream whose response does not parse as a failed poll (`dsh:src/sources/github.ts:139-142,182-187,220`); opus: resetting `per_page` to 100 whenever `since` moved repeated wasted 30-second timeouts on a persistently slow link, and the halving existed only for the client's whole-request timeout (`orch/service/integrations/github.go:43`) | Opus's simpler remedy: poller requests drop the whole-request timeout for three deadlines (headers within 30 seconds, no 30-second gap in the body, at most 5 minutes per request); `per_page` is fixed at 100, and the per-stream `per_page` column, the halving, the floor of 12 and the validator drop on a `per_page` change are deleted; one classifier in Security and privacy names the transient class (a `5xx`, a missed deadline, any other transport error, a malformed response) and the access class (any other `403`, `404`, `410`), with one count per stream that a failure of the other class restarts (superseded in round 17: two streaks, `failed_polls` and `access_failures`; see that row); fixtures for a slow page, a stalled connection, three malformed responses, three over-bound elements and a `404` after transient failures | A slow page that keeps arriving now completes instead of cycling through smaller requests, memory stays bounded by the streamed decoder, and every failure has exactly one class. The honest bound: a worst-case 26 MiB page needs about 0.73 Mbit/s to finish within 5 minutes, and because a watch's requests run one at a time, its worst-case poll takes about 5 hours 50 minutes. A failure of the other class restarts the count, so three access failures in a row, never two transient ones and an access one, suspend a watch. (Both superseded in round 17: a 10-minute poll deadline bounds a poll, and two streaks replace the restarting count; see that row.) |
| Round 17 review: in-flight polls, poll slots, page transactions and mixed failures (pro001a-inflight-poll-survives-quiet-disable-delete, pro001a-inflight-poll-survives-disable-quiet-delete, pro001a-poll-slot-cap-contradicts-no-cross-watch-delay, PRO001_WHOLE_POLL_CONCURRENCY_STARVES_WATCHES, pro001a-watch-concurrency-claim-false-and-untested, pro001a-page-transaction-scope-vs-slow-stream, PRO001_MIXED_FAILURES_NEVER_SURFACE, pro001a-five-minute-ceiling-untested) | grok, opus: quiet was read only before each poll, so a poll of up to about 5 hours 50 minutes kept sending the credential and committing pages after quiet, a disable, a deletion or a `401`, and its final state write could overwrite the cursors a re-enable stored, while DeepSeek stops before each stream and item (`dsh:src/sources/github.ts:154,183,214,226`); grok, sol, opus: 4 whole-poll slots let slow polls hold every slot for hours, contradicting "never another watch's", with no fixture; opus: the spec did not say when a page's transaction opens, and inserting as decoded would hold the one connection (`orch/db/connection.go:47`) for minutes; sol: one count that a failure of the other class restarted let alternating `404`s and timeouts fail forever unseen; opus: no fixture trickled bytes past the 5-minute ceiling | Point 2, stated once: each poll runs under a context cancelled by a disable, deletion, suspension or quiet; before every request and in each page's commit it re-reads the watch row and the quiet keys, and on a failed check or a `401` it cancels the request, sends nothing more, commits nothing more and writes no state row (refined in round 22: a stopped poll's only writes are the rotation write and, when that is the cause, the suspension; see that row), and a state row is written only while `enabled_at` is the one the poll started with (extended in round 23 to every check and write of the poll; superseded in round 28: the watch's `generation` is the fence; see that row); a poll stops at a 10-minute deadline as behind, and due watches take one of 4 slots in FIFO order of due time, so the k-th waiting watch starts within ceil(k/4) × 10 minutes ("never another watch's" is withdrawn); a page is decoded whole with no transaction open, then gated and committed in one short transaction, and peak memory is one element plus the page's kept records, about 2.6 MiB (superseded in rounds 46 and 47: label names and the thread-label map raise it to about 10.5 MiB; see those rows); the classifier keeps two streaks per stream, `failed_polls` (every failure; 3 show `error`) and `access_failures` (non-rate-limit `403`, `404`, `410` only; a transient failure neither adds to it nor resets it; 3 suspend), both reset only by a successful poll (superseded in round 18: only a poll answered `200` or `304` with no failure resets them, and a poll deadline no longer counts as success; see that row); fixtures for each | Reusing the reads quiet and the watch row already need stops a poll without a poll-generation table; the deadline turns an unbounded slot into a stated bound; a short commit after the download keeps attended writes off a slow stream for a bounded 1.6 MiB of kept records; and a stream that keeps failing is always seen, while only access failures suspend. |
| Round 18 review: the poll deadline's streaks and its unreached streams (pro001a-deadline-stop-resets-unanswered-streaks, PRO001_POLL_DEADLINE_STARVES_LATER_STREAMS, pro001a-poll-deadline-starves-unreached-streams, pro001a-dsh-abort-citation-points-at-retry-type) | grok: the classifier called a poll the deadline cut a successful poll, so a deadline cancel before a stream's headers reset both streaks, and alternating it with `404`s defeated the three-access-failure suspension; sol, opus: a poll visited a watch's streams in a fixed order, so slow early streams spent the deadline every poll and a healthy later stream was never requested, and a stream left unreached neither failed nor fell behind, so nothing showed; grok: the DeepSeek abort citation's 154 was said to be `Retry`, not the stopped predicate | The classifier gives each stream's poll one outcome: failed, answered (a `200` or `304` and no failure, the only reset) or unanswered (a rate limit only, a request cut by the deadline or a stop before any status, or a stream never reached), which leaves both streaks unchanged; the watch row stores `last_stream_begun`, each poll starts at the stream after it and wraps, so every stream is begun at least once in every `N` (at most 20) polls (superseded in round 19 by a rotation by repository, `last_repository_begun`; see that row), and a stream the deadline left unreached is behind and keeps its `since` and validators; fixtures for the cut `404` streak, two slow streams before a healthy one, and a 10-repository watch with one slow repository (restated in repository terms in round 19). The citation finding is rejected (review dispositions appendix): line 154 is the stopped predicate | A cut request proves nothing about access, so it must not clear an access-failure run; one integer turns a fixed order into a rotation only when the deadline cuts, keeps a healthy watch's order stable, and makes a starved stream visible as `backlog`. |
| Round 19 review: rotation by stream, and a pending fire in a withdrawn session (pro001a-rotation-start-on-comment-stream-forces-hydrations, WATCH_PENDING_FIRE_SESSION_WITHDRAWAL) | opus: round 18's rotation could start a poll at a comment stream and keep that order, so its comments' threads were not yet on that poll's issue page and each spent a hydration from the budget of 10; sol: a withdrawal of a thread session ends its runs inside its begin transaction, where a pending fire's enqueue would be refused and roll the withdrawal back | The poll rotates by repository: the watch row stores `last_repository_begun`, each poll starts at the repository after it and reads each repository's issue stream before its comment stream, and a stream the deadline leaves unreached is still behind; the round 18 fixtures are restated in repository terms, and one checks that a comment whose thread is on that poll's issue page needs no hydration. Point 8 points to section 6's session fence for the pending fire | Repository order keeps every comment behind its own repository's issue page at no extra cost; the price, stated in point 2, is that a repository whose issue stream alone outlasts the deadline leaves its comment stream behind and visible as `backlog`. |
| Round 20 review: the rotation index on a stopped poll, connection revocation, and a new event while a thread session is withdrawn (PRO001_REPOSITORY_ROTATION_INDEX_NOT_ADVANCED_ON_CANCEL, PRO001_POLLER_CONNECTION_REVOCATION_FENCE, WATCH_PENDING_FIRE_STALE_SESSION_DELETES_REPLACEMENT, pro001-direct-fire-into-withdrawing-thread-session-unspecified) | mai: `last_repository_begun` was stored with the last state row, so a poll stopped inside repository k before any commit stored nothing and the next poll began at k again; sol: the per-request check and the page commit did not read the connection, while revoking one destroys only Turing's copy and cannot invalidate the token at GitHub (`orch/repository/integrations.go:270-298`), whereas attended dispatch rechecks the live credential (`orch/service/integrations/call.go:127-163`); grok, opus: see section 6's decision log | Point 2: before a repository's first request the poll commits `last_repository_begun = k` in its own short watch-row transaction under the `enabled_at` guard, so a cut, stopped or failed poll still advances the start (superseded in round 21: the index is written once, when the poll ends or stops; see that row); the per-request check, stated once in Stopping a poll, also runs the attended dispatch's connection check (present, `connected`, credential not destroyed, ciphertext byte-identical to the poll's), and a revoked or changed connection stops the poll with nothing committed (refined in round 22: nothing but the rotation write and the suspension; see that row) and suspends the watch with `needs_person` and the content-free cause `connection_unavailable`; point 8 points to section 6, point 1, for a new event while a withdrawal quiesces; PRO-001a fixtures for a stop inside repository k and a revocation during page 1 | A rotation that a stop can reset starves the same repositories; a poll must not keep reading with a credential the owner revoked, and the attended path already has the exact check. |
| Round 21 review: the rotation write's transaction budget, a connection revoked between polls, and the direct fire in a page transaction (pro001a-rotation-write-outside-stated-transaction-budget, pro001a-revocation-between-polls-untested, UX002_CONNECTION_UNAVAILABLE_CAUSE_UNTESTED, PRO001_DIRECT_FIRE_SECOND_TRANSACTION_REOPENS_PAGE_ROLLBACK) | grok: see section 6's decision log for the direct fire; opus: round 20's per-repository rotation write added up to 10 write transactions a poll, quiet polls included, outside the poller's stated budget (`docs/NORTH_STAR.md:327`); opus, grok: the revocation fence was tested only mid-download, so a revocation found before a poll's first request could be treated as transient and never suspend the watch | Point 2 states the rotation write once: `last_repository_begun` is written once, when the poll ends or stops, in one short transaction that writes only that field under the `enabled_at` guard (refined in round 23: only when the value changes; superseded in round 28: the watch's `generation` is the fence; see that row; see those rows), and a crash mid-poll writes nothing; Performance states the per-poll transaction budget once (one rotation write, at most one since round 23, one transaction per committed page, the final state rows that no page carried, a suspension; extended in round 22 by an answered stream's changed state row, see that row); point 2 says a connection revoked between polls is found before the next poll's first request. PRO-001a adds a quiet 10-repository poll that commits exactly one rotation write and no page rows (superseded in round 23: it commits no transaction; see that row), a crash mid-poll, and a revocation between polls; round 20's stop inside repository k still makes the next poll start at k+1; UX-002a asserts the attention row (section 8) (PRO-001a's test since round 45; see section 8's row); point 2's page transactions say they also hold a page's direct fires, pointing to section 6, point 1 | One write a poll keeps the rotation's guarantee at a fixed, stated cost, and a crash only repeats one start; the suspension must hold however early the revocation is found. |
| Round 22 review: a stopped poll's writes, the page transaction's connection re-check, an answered stream's state row, and a failed page commit (PRO001_STOP_COMMITS_NOTHING_CONTRADICTS_ROTATION_WRITE, pro001a-stop-path-rotation-write-contradicts-commit-no-row-tests, PRO001_PAGE_COMMIT_CREDENTIAL_REREAD_DEADLOCKS, pro001a-transaction-budget-omits-answered-stream-state-row, pro001d-direct-fire-page-rollback-unclassified-stall) | grok, opus: point 2 writes `last_repository_begun` when a poll stops, while PRO-001a, TUR-028b, section 18 and `docs/NORTH_STAR.md` said a stop commits nothing; grok: the page transaction's connection re-check called `GetSealedGitHubCredential`, which queries `r.db` (`orch/repository/integrations.go:90`) while the open transaction holds the one connection (`orch/db/connection.go:47`), so it would wait on itself; opus: the budget had no transaction for a `304`-answered stream whose state row changed, and a page whose commit failed was neither failed nor behind, so a persistent fire error stalled a stream silently | Point 2 states once that a stopped poll commits no event, excerpt, receipt, page, state, cursor or ETag row and that its only writes are the rotation write and, when the cause is a suspension, the suspension (refined in round 23: a poll that outlives a disable and re-enable commits neither; see that row); the page transaction runs the connection statement on itself through a `rowQuerier`, and nothing calls an `r.db` method or begins another transaction while it is open; a state row is written only when a field changes, and the budget adds one transaction per answered stream whose state row changed; a page whose commit fails, other than a rollback by the stop's re-check, is a transient failure of its stream, never progress or answered. PRO-001a's stop tests assert the rotation write (superseded in round 24: no write other than at most the rotation index, only when it changes; see that row) and unchanged `since`, ETags and event rows (refined in round 25: unchanged only under quiet, since a disable drops them; see that row), and it adds a state-row reset after a failed poll (corrected in round 25: that poll is answered `200`, since the failure dropped the validators; see that row) and a revocation committed just before a page transaction's first statement | One sentence names every write a stop makes; a second database call inside the one-connection transaction cannot complete; the budget must count every steady-state write; a stall must surface as `error`, not hide. |
| Round 23 review: an old poll across a disable and re-enable, a malformed paging fence, the roadmap's streak reset, quiet in a page transaction and the steady-state rotation write (PRO001_DISABLE_REENABLE_PAGE_FENCE, PRO001_PAGING_FENCE_MALFORMED_BOUNDARY_TIMESTAMP, PRO001_ROADMAP_PARTIAL_SUCCESS_RESETS_ACCESS_STREAK, pro001d-quiet-listed-as-committed-refusal-contradicts-stop-rollback, pro001a-steady-state-rotation-write-rewrites-unchanged-index) | mai: only state rows required the poll's `enabled_at`, so a poll that outlived a disable and re-enable could still commit events and fires from the disabled interval; sol: `P` was the `updated_at` of page 1's last item, undefined when that item is malformed, and `docs/NORTH_STAR.md` reset the access streak on any `200` or `304`, so polls answered `200` on page 1 and `404` on page 2 would never suspend a watch; opus: section 6 listed quiet among refusals a page commits while point 2 rolls back a page whose transaction sees quiet, and a complete poll rewrote an unchanged `last_repository_begun` on the one connection (`orch/db/connection.go:47`) | Point 2's per-request check records `enabled_at` when a poll starts and requires it unchanged before every request, in every page and state-row transaction (direct fires included), and for the rotation write and the suspension (round 24 also requires the watch enabled and not deleting for the suspension; see that row); a changed `enabled_at` is a stop that commits nothing at all (superseded in round 28: the watch's `generation` is the fence; see that row, which every enable, disable and scope edit increments). `P` is the `updated_at` of the last item on page 1 with a valid timestamp, and a page 1 with items but no valid timestamp is a malformed response. PRO-001's roadmap sentence resets the access streak only on an answered poll, the classifier's existing rule. Quiet or a pending drain seen by a page transaction is a stop, and section 6 drops quiet from its refusal list. `last_repository_begun` is written only when it changes, so the steady state commits no transaction. PRO-001a adds a disable and re-enable while page 1 downloads, a full page 1 whose 100th item lacks a timestamp, and three polls answered `200` then `404`, and its steady-state and streak-reset tests assert no rotation write | Every write of an old enable must lose to the new one, not only its cursors; the fence must be defined for every page the decoder accepts; the roadmap must state the classifier's rule; one stop rule covers quiet everywhere; a write that changes nothing only queues on the one connection. |
| Round 24 review: a stopped poll's rotation write, a growing replacement near the excerpt budget, a suspension racing a disable or deletion, and the re-enable fence's fixture (pro001a-stop-asserts-rotation-write-when-index-unchanged, pro001-stopped-poll-rotation-write-assertions-fixture-dependent, pro001a-rotation-wording-stale-and-duplicated, PRO001_EDIT_REPLACEMENT_EXCERPT_BUDGET, pro001-suspension-after-concurrent-disable-or-delete, pro001a-reenable-fence-fixture-satisfied-by-context-cancel) | grok, opus: the rotation index is written only when it changes, but the stop tests asserted a rotation write, which a one-repository watch after its first poll never makes, and point 2 repeated the rotation rule and still said the index advances; sol: an edit that grows a held excerpt near the 64 MiB budget had no rule, and keeping the old text broke the edited-out promise; opus: a suspension checked only `enabled_at`, which a disable or deletion does not change, so a `401` racing either marked an owner-disabled watch `needs_person`, and the disable and re-enable fixture passed on the context cancel alone | Point 2 states the rotation write once (at most once when a poll ends or stops, only when it changes; the next poll starts at repository k+1 even when this one is cut, stopped or fails inside k) and drops the duplicate; every stop test asserts no write other than at most the rotation index and, for a suspension, the suspension, with a multi-repository fixture stopped outside the stored index (one rotation write; superseded in round 28 for a disable or deletion, which changes the `generation` and so commits nothing at all; see that row) and a one-repository fixture (quiet or a disable during page 2 commits nothing after the stop, a revocation during page 1 only the suspension); a replacement is atomic on its byte delta, and one the budget cannot hold clears the old excerpt and its derived fields, still marks `observe_only` and deletes the pending fire, with a near-cap test; a suspension requires the watch enabled and not deleting as well as `enabled_at` unchanged, with disable and deletion race fixtures (round 25 states this once as the suspension write's predicate, which still lets the rotation write commit; superseded in round 28: the predicate is `generation` unchanged, and the race writes nothing at all; see those rows); PRO-001a and section 6 inject a disable and re-enable after page 1 is decoded and before its page transaction, and before a deadline-cut stream's state row (round 25 holds back the context cancel; see that row) | A test must hold for every valid fixture; a budget must bound every write that stores text, and an edit must never keep text its author removed; the owner's disable must win over a racing fault; a fence's test must fail without the fence. |
| Round 25 review: the suspension write's predicate, the failure-recovery fixture, the re-enable fence's held-back cancel and a disabled watch's cursors (pro001-401-disable-race-writes-nothing-contradicts-rotation, pro001a-suspension-check-reference-ambiguous, PRO001_FAILURE_RECOVERY_304_FIXTURE, pro001a-reenable-fence-fixture-still-satisfied-by-context-cancel, pro001a-disable-stop-test-asserts-cursors-unchanged-after-disable-drops-them) | grok: a `401` racing a disable or deletion was said to write nothing, which dropped the rotation write its own rule still allows; opus: the suspension ran "under the check above", readable as the whole per-request check, which a revoked connection always fails; sol: the poll after a failure was answered `304`, but the failure dropped that stream's validators, so the request is unconditional and gets `200`; opus: the disable and re-enable fixture still did not hold back the poll context's cancel, so a cancelled `BeginTx` passed without the fence, and the multi-repository stop test asserted unchanged `since` and ETags after a disable, which drops them | Point 2, Stopping a poll, states the suspension write's predicate once (the watch enabled, not deleting, and `enabled_at` unchanged; no connection check; quiet does not block a suspension), so a race with a disable or deletion writes no suspension while the rotation write still commits under its own rule; PRO-001a's race fixture adds a multi-repository watch stopped outside the stored index (one rotation write, no suspension) (superseded in round 28: the predicate is `generation` unchanged, a disable or deletion changes it, so the race writes nothing at all and the fixture asserts no rotation write; see that row) and a `401` racing quiet (still suspends); the recovery poll sends no `If-None-Match`, is answered `200` with one new item within 5 seconds after the held `since` (so `since` does not move and the validator is kept; pinned in round 26) and commits one page transaction carrying the state row, and the next poll sends `If-None-Match`, is answered `304` and commits nothing; the PRO-001a and section 6 fence fixtures commit their rows with a direct row write, hold the context's cancel back until after the page transaction's re-check, and fail without that re-check; the stop test asserts unchanged `since` and ETags only under quiet and, for a disable or deletion, no state row written by the poller | A sentence must not contradict the rule it applies; a reference must name one predicate; a fixture must be reachable; a fence's test must fail without the fence; an assertion must match what the action does to the row. |
| Round 26 review: a repository removed from scope mid-poll, the recovery fixture's `since`, and a suspended watch under quiet (PRO001_POLLER_SCOPE_REVOCATION_NOT_FENCED, pro001a-recovery-fixture-new-item-moves-since-drops-validator, pro001-quiet-plus-suspension-status-precedence-undefined) | sol: the per-request check omitted the watch's current scope, so a poll kept sending credentialed requests for a repository the owner had removed; opus: the round-25 recovery fixture's new item, if more than 5 seconds after the held `since`, moves `since` and drops the validator, so the next poll gets `200`, not `304`; opus: a `401` racing quiet left a watch both quiet and suspended, with its status defined two ways | Point 2, Stopping a poll: the per-request check, and each page transaction's re-check, require the requested repository still in the watch's current scope; a removed repository stops only its own streams for the poll, with no failure, no state row and no rotation-write change beyond that write's rule, and the poll continues (superseded in round 28: any scope edit changes the `generation` and stops the whole poll with nothing written, and the per-repository scope check is gone; see that row); PRO-001a adds the barrier fixture (removal after page 1: no page 2 or hydration request, nothing more committed) and its page-transaction variant; the recovery fixture pins the new item within 5 seconds after the held `since`, so `since` does not move and the validator is kept (mirrored in the round-25 row); the status precedence is section 21's one rule, which point 2, the failure classifier and PRO-001a's fixtures point to, and PRO-001a's `401`-races-quiet fixture asserts `error` with `needs_person` and one attention row | Removing scope is revocation, so it must fence every later request; a fixture must be reachable as written; one status must have one rule. |
| Round 27 review: a moved repository and a scope edit's cursors (pro001a-renamed-repository-redirect-misclassified-transient, pro001a-scope-addition-cursor-seed-undefined) | opus: GitHub answers a renamed or transferred repository's old name with a `301`, which `NoRedirectClient` refuses as a transport error, so the watch failed forever as transient, never asked for a person and kept sending credentialed requests; opus: a repository added to an enabled watch had no starting `since` and could backfill from `enabled_at`, a removed and re-added one resumed an old cursor, and `last_repository_begun` shifted with list edits | Security and privacy states the redirect rule once: any redirect answering a poller request is never followed and suspends the watch at once (refined in round 28: only a redirect on a stream request suspends; one on a label hydration skips that comment; see that row), as a `401` does, with `needs_person` and the cause `repository_moved` (superseded in round 29: every poll request is addressed by the numeric repository id, so a rename never redirects a stream and a stream redirect is unexpected, with the cause `unexpected_redirect`; see that row); point 2 states the scope edit once: an add creates the repository's state rows with `since` and `started_at` at the edit's commit time, a removal deletes them in the same transaction, any edit resets `last_repository_begun` to −1, and the rotation write requires the repository list the poll started with (superseded in round 28: it requires the `generation` the poll started with; see that row); PRO-001a and PRO-001d add the fixtures | A moved repository is a fault only the owner can fix, and a scope edit must neither replay nor resurrect a cursor. |
| Round 28 review: an old poll across a removal and re-add, a transferred issue's hydration, and where a repository's numeric id comes from (PRO001_SCOPE_READD_STALE_POLL_FENCE, pro001a-scope-readd-aba-in-flight-page, pro001a-hydration-301-transferred-issue-suspends-watch, pro001-repo-id-source-unspecified, pro001a-slice-content-omits-round27-rules) | sol, opus: a poll paused across a repository's removal and re-add passed both the `enabled_at` check and the membership check, so it could use its old cursor, admit events from the excluded interval and overwrite the re-add's state, and the repository-list rotation guard was fooled the same way; opus: GitHub answers a hydration of an issue transferred to another readable repository with a `301`, so any collaborator could suspend the watch; opus: nothing said how the poller learns the numeric repository id its resource ids carry; opus: the PRO-001a Content cell omitted round 27's rules | Point 2 states one generation fence: the watch row's integer `generation`, incremented by every enable, disable and scope edit, is recorded when a poll starts and required unchanged before every request, in every page, state-row, direct-fire and rotation transaction and for the suspension; a changed `generation` is a stop that commits nothing at all; `enabled_at` only seeds cursors, and the per-repository scope check is deleted because the fence covers removal and re-add. No request follows a redirect; only one on a stream request suspends with `repository_moved` (superseded in round 29: requests are addressed by id and the cause is `unexpected_redirect`; see that row), and one on a label hydration skips that comment like a `404` or `410`. A save that adds a repository resolves its numeric id with one `GET /repos/{owner}/{repo}` through the watch's connection and refuses the whole save when that fails; Performance counts that request. The Content cell names the stream redirect, scope edits and the fence. PRO-001a adds the paused-poll barrier fixture, the id resolution and refusal tests and the transferred-issue test, and a disable or deletion racing a stop now asserts no write at all | One fence must bind every write to the watch state the poll started from; a third party must not be able to suspend the owner's watch; an identifier must have one source; a slice must name what it builds. |
| Round 29 review: a reused repository name (PRO001_REUSED_REPOSITORY_NAME_BYPASSES_IDENTITY_FENCE, pro001a-name-addressed-streams-follow-reused-repository-name) | sol, opus: streams and hydrations were addressed `/repos/{owner}/{repo}/…`, and GitHub redirects a renamed repository's old name only until another repository is created at it, so a watch could read a different repository's items under the stored id, and "a later rename or transfer is caught by a stream's redirect" did not hold | Point 2 states once that every stream request and label hydration is addressed `GET /repositories/{repo_id}/…` by the id resolved at save, and the typed name is never sent again; a rename never interrupts the watch, a reused old name never substitutes another repository, and a transfer the token cannot read is a `404`, an access failure. No redirect is followed; one on a stream request is unexpected and still suspends at once, with the cause renamed `unexpected_redirect`; one on a hydration still skips that comment. The save-time `GET /repos/{owner}/{repo}` stays. PRO-001a adds the id-addressing assertion and the rename-then-reuse fixture | The stored id is the identity the owner approved, so every request must name it; a name is not an identity. |
| Round 30 review: actors bound to logins, a repeated repository id, and a stale display name (PRO001_ACTOR_LOGIN_REASSIGNMENT, pro001a-scope-duplicate-repository-id-unrefused, pro001a-typed-name-display-stale-after-rename-reuse) | sol: listed actors were logins, which GitHub lets an account rename and another account then claim, so an outsider holding a listed login passed the gate and could fire `auto`; opus: `Owner/Repo` and `owner/repo` resolve to one id with no refusal, so its state rows collide; opus: the typed name stayed the display name and a thread session's title, which after a rename and reuse names another repository | Point 3 states once that listed actors, listed bots and a predicate's actor terms are bound by numeric user id, resolved with one save-time `GET /users/{login}` under the repository resolution's rules, the login kept for display only, and that the gate, predicates, bot check and revalidation compare `user.id`; events store the author id. Point 2 refuses a save whose resolved repository ids repeat, leaving the scope and its state untouched, and states once that the display name is the resolution's `full_name`, then the `repository_url` of the latest id-addressed issue page, in GitHub's name charset (amended in round 31: or a conditional repository request when the issue stream yields none; amended again in round 32: from any issue object a poll reads, and the repository request at most once an hour; amended again in round 33: the last known name, labeled last seen; see those rows). PRO-001a adds the reclaimed-login, duplicate-id and display assertions; PRO-001b and PRO-001d add the predicate and fire counterparts | An identity is the id GitHub never reuses; a name, a repository's or an account's, is display text only. |
| Round 31 review: a display name stale behind `304`s, and the id route (pro001a-display-name-stale-on-304-after-rename, pro001a-display-name-stale-until-item-overclaims-never-names-reused, pro001-numeric-repository-api-route) | grok, opus: a rename does not move an issue's `updated_at`, so a quiet repository's pages answer `304` with no `repository_url`, and the shown name stayed the old one, which another repository could then hold; grok: refresh it with one conditional id-addressed request; opus: send no extra request and match the claim to the mechanism; mai: `/repositories/{repo_id}` is not an established route | Point 2 states once that a poll whose issue stream yields no name sends one conditional `GET /repositories/{repo_id}` with that request's own ETag and writes `full_name` only when it differs, so the "never names another repository" claim holds (superseded in round 33: the request is unconditional, nothing stores its ETag, and the "never names" claim is withdrawn; see that row); the request budget is at most 80 per watch per poll and 3 per repository on a quiet poll, all `304` (superseded in round 32: the repository request is sent only when no issue object was read in the last hour, at most once an hour, and the quiet budget is stated per stream; see that row); the transaction budget counts the name write; PRO-001a keeps the rename-then-reuse assertion and adds the quiet-renamed fixture. Mai's finding is rejected with the host's live evidence (Appendix: Review dispositions), and point 2 cites GitHub's rename redirect to `/repositories/{id}` as the route's basis | A stale name can misdirect a later attended `github.*` call the owner makes from a detached thread, and the extra request is a `304` in the steady state, which does not count against GitHub's primary rate limit (corrected in round 32: the repository object's ETag also changes without issue activity, so it can be a rate-counted `200`, now at most once an hour; see that row); that is why opus's no-request option was not chosen. |
| Round 32 review: the repository request's cost and storage, and the quiet budget (pro001-quiet-poll-budget-drops-multipage-304, pro001a-repository-request-etag-churns-on-non-issue-activity, pro001a-repository-request-etag-storage-unstated, pro001a-304-short-page-ends-the-poll-wording) | grok: "3 per repository when nothing changed" contradicted a quiet walk of up to 3 `304` pages per stream; opus: the repository object's ETag changes with pushes and stars, so the request was often a rate-counted `200` and a write, not a free `304`; opus: no record said where its ETag lived; opus: "a `304` to a short page ends the poll" read as ending the comment stream and the repository request too | Point 2 states once that the display name comes from any issue object a poll already reads (issue-page items and a label hydration's issue, both carrying `repository_url`), and that the conditional `GET /repositories/{repo_id}` is sent only when no issue object was read for the repository in the last hour and at most once an hour, with a stale-name window of one hour plus one poll interval and its real cost stated (a possible rate-counted `200` and a write, at most hourly); the scope entry holds the id, display name, the request's ETag and `name_checked_at`, deleted with it on removal and kept across a disable (superseded in round 33: it holds only the id and the name, `name_checked_at` is in memory, and the stale window is stated with its failure and skip cases; see that row); Performance states the quiet budget per stream and drops the per-repository cap, keeping the 80-request ceiling; a `304` to a short page ends that stream's walk; PRO-001a adds the five-request fixture, the hourly bound, and the removal and disable assertions | A display-name refresh must cost what the spec says it costs, and a budget must count what the walk can send. |
| Round 33 review: the display name's stale window and its stored check state (pro001a-stale-name-window-exceeded-by-failed-refresh, pro001a-stale-name-window-bound-and-never-names-overclaim, pro001a-repository-request-persisted-etag-and-checked-at-yagni) | grok: a failed repository request used up the hour, so failures left a possibly reused name shown indefinitely; opus: failures, reserve skips, backoff, slot waits and deadline cuts extend the stated window, inside which the name may belong to another repository, so "never names another repository" overclaimed; opus: the stored ETag rarely helps (pushes and stars change it) and a persisted `name_checked_at` saves at most one request per restart | Point 2 states once that `name_checked_at` lives in the poller's memory only and the scope entry stores only the id and the display name (the stored ETag and `name_checked_at`, their writes and their removal and disable rules are deleted); the repository request is unconditional, writes `full_name` only when it changes, and a failed or unparseable one leaves the timer, so it is retried on the next poll, with no streak and no row; the name is the last known name, labeled last seen, whose lag is stated honestly and the "never names" claim is withdrawn; Performance states the quiet budget as per stream plus at most one rate-counted repository request per quiet repository an hour, more only while it fails; PRO-001a adds the `502`-then-`200` and restart fixtures and drops the kept-ETag ones | A display string must not cost columns, write paths and rules worth more than it, and must never be presented as more current than its last check. |
| Round 34 review: when the name timer moves (pro001a-name-timer-advances-before-committed-name) | grok: the timer moved on a decoded `200` even when its name transaction rolled back, so a stopped poll could leave a stale name for an hour | `name_checked_at` moves only once the observation is durable: after a changed name commits, or when the accepted name equals the stored one; a rolled-back name write leaves the timer like a failed request; PRO-001a fixture | Page validators already follow this rule (stored only once their page commits); the display-name timer now matches it. |
| Round 45 review: the watch's attention rows and stage 3's cross-kind paging (ux002a-acceptance-requires-later-slices-watch-quiet) | opus: see section 8's decision log | PRO-001a asserts the `connection_unavailable` and `unexpected_redirect` rows, that a suspended watch's row cannot be dismissed, and section 8's paging fixtures across stage 3's automation and watch kinds | It is the slice that adds the watch source. |
| Round 46 review: label names had no byte bound (PRO001_RETAINED_LABEL_METADATA_BYTE_BUDGET) | sol: kept records and admitted events held label names outside both the poller's memory figure and the 64 MiB budget, so 100 labels of 50 four-byte characters, INT-009a's fixture size, added about 20 KiB per item and small-bodied events could hold hundreds of MiB of label text | Label names are kept up to 20 KiB per element, and an element over that is refused `label_budget` with no text rather than kept with a cut set; they count with excerpts in `watches.excerpt_bytes` against the 64 MiB budget; the peak becomes about 4.6 MiB (about 10.5 MiB since round 47, which counts the thread-label map; see that row) and the uncapped figure about 1.5 GiB; PRO-001a's heap and budget fixtures carry the 100-label case | Predicates read labels, so a cut set would change matches silently; refusing fails closed, and one counter keeps one budget for all third-party text. |
| Round 47 review: the thread-label map and the summary's figure (pro001a-thread-label-carryover-outside-peak-memory-bound, f1-summary-peak-memory-stale, f1-summary-peak-memory-stale-after-label-budget) | opus: a comment's labels come from its thread on the same poll's issue pages, so up to 300 issues' label names stay in memory until the comment stream, outside the stated peak; grok, opus: section 2's F1 row still said 2.6 MiB | Point 2 states the map (one repository's issue numbers and label names, at most 300 entries, dropped when its comment stream ends) and a peak of about 10.5 MiB a poll, about 42 MiB across the 4 slots; Performance and section 2 say the same; PRO-001a's heap fixture adds a 3-page labelled issue walk followed by a comment page | The bound must count every buffer the poll holds, and one figure must be stated the same way everywhere. |
| Round 48 review: the thread-label fixture's heap measure (pro001a-thread-label-map-absolute-heap-assertion-untestable) | opus: the fixture's own bytes and uncollected garbage count in peak heap, so a correct poller could fail an absolute 10.5 MiB check | The fixture streams its pages and measures live heap after `runtime.GC()` at each page commit, less a baseline taken before the poll, and checks the return to baseline once the comment stream ends | The test must measure what the poller holds, not the fixture or the collector's timing. |
| Round 49 review: an expiring thread session's deletion cost (PRO001_EXPIRY_CASCADE_EXCEEDS_SWEEP_BUDGET) | sol: the sweep bounds sessions per tick, but withdrawal ends in one cascading `DELETE` of the session (`orch/repository/session_delete.go:563`), and a thread session can hold up to 43,200 runs, so one automatic withdrawal could hold the single connection for its whole history | `AdvanceSessionDeletion` removes the session's runs and then its messages in transactions of at most 100 each before the final `DELETE`, for every session deletion, with no new receipt state; PRO-001b adds a 43,200-run fixture asserting the batches, an attended write waiting at most one batch, crash resume and no attention row. A session rotated at a run count was considered and not chosen (superseded in round 50: the batches are withdrawn, and an `auto` ceiling, a begin-only sweep and foreign-key indexes bound the cost instead; see that row) | Rotating the current session would orphan a pending follow-up stamped with the old session (section 6, point 1, the hook's session fence); batching bounds the cost where it is paid and keeps the lifecycle. |
| Round 49 review: the `watch_threads` index wording (PRO001_WATCH_THREADS_UNIQUE_INDEX_AMBIGUOUS_AFTER_GENERATION) | opus: round 48's sentence read as one unique index over `session_id`, `dismissed_at` and `dismiss_generation`, which would enforce nothing while `dismissed_at` is NULL | The unique index is on `session_id` alone; the other two columns are listed separately and no index covers them | The pending-fire hook and the recall exclusion rely on a unique probe of `session_id`. |
| Round 50 review: round 49's deletion batches (PRO001_RUN_BATCH_DROPS_DELETION_SCRUB, PRO001_BATCHED_RUN_DELETE_CASCADES_ARTIFACT_MANIFEST, PRO001_WITHDRAWAL_BEGIN_UNBOUNDED, PRO001_WITHDRAWAL_CASCADE_UNINDEXED_FKS, PRO001_SWEEP_TICK_RUNS_WHOLE_BATCHED_WITHDRAWALS, PRO001_NONEXISTENT_DELETING_RECEIPT_STATE) | grok, opus: committing run deletes before the final transaction lost the audit scrub (it reads the run rows), cascaded away the sandbox-artifact manifest the cleaners and the retained-file count need, and could run past the delegation gate, and messages cannot precede the runs that reference them; sol: the begin's audit scrub and cancellation were still history-sized, and unindexed foreign keys (`approvals.run_id`, `tool_calls.step_id`, `agent_runs.user_message_id`) made each cascade scan other sessions' rows; opus: the sweep ran whole withdrawals inline on the reaper tick, and the receipt state `deleting` does not exist | The batches are withdrawn and the lifecycle is unchanged. Three rules bound the cost instead (point 8): a thread session takes at most 500 `auto` runs (section 6, point 1); the sweep only begins a withdrawal and the existing session-deletion reconciler, on its own goroutine, finishes it; and PRO-001b's migration indexes every foreign-key child column referencing `agent_runs`, `agent_run_steps` or `messages` that leads no index, with a schema test. PRO-001b's fixture is a 500-run session among 100,000 other rows, with audit scrub and artifact cleanup asserted; PRO-001d tests the ceiling | Changing the shared deletion lifecycle broke invariants it keeps in one transaction; bounding what a thread session can hold, and making each lookup an index probe, bounds both transactions without touching it. |
| Round 51 review: the foreign-key index list and the sweep's begins per tick (PRO001_RUN_STEPS_RUN_ID_ALREADY_INDEXED, PRO001_REDUNDANT_RUN_STEPS_FK_INDEX, PRO001_FK_INDEX_RULE_AGENT_RUN_STEPS_ALREADY_INDEXED, PRO001_FK_INDEX_RULE_MISSES_JOBS_CHILD, PRO001_SWEEP_BATCH_HOLDS_ONLY_CONNECTION) | grok, sol, opus: `agent_run_steps.run_id` already leads `UNIQUE(run_id, step_index)`; opus, grok: limiting the rule to three parent tables missed `send_message_idempotency.job_id` and other unindexed children the cascade probes; grok: the sweep could begin 100 thread sessions a tick and the reconciler finish them all in one pass | The rule covers every foreign-key child column in the schema, names the ten that lead no index today (computed from the migrated schema's `pragma_foreign_key_list` and `pragma_index_list`), leaves `agent_run_steps.run_id` alone, and the schema test counts constraint-created indexes; the sweep begins at most 10 thread-session withdrawals a tick and defers the matching excerpt cuts with them; PRO-001b adds a 100-due-sessions fixture. The reconciler's pass is unchanged: it is shared with the owner's deletions and already runs each step as its own transaction on its own goroutine, so the fixture asserts an attended write commits within 1 s during it rather than capping it | A rule is only as good as its coverage, and a per-tick cap bounds the reaper's stall (the maintenance tick's since round 53) while the deferred cut keeps every copy no older than its source. |
| Round 52 review: the reaper switched off, the index cost and citations, and the id routes (PRO001_RETENTION_SWEEP_DEAD_WHEN_REAPER_DISABLED, PRO001B_FK_INDEXES_UNSTATED_HOT_PATH_WRITE_COST, PRO001B_FK_INDEX_LIST_CITATIONS_STALE_AND_INCOMPLETE, PRO001B_FK_INDEX_TEST_SHOULD_EXTEND_SCHEMA_INVARIANTS, f1-comment-numeric-route-unverified) | opus: `TURING_JOB_REAPER_INTERVAL_MS=0` starts no reaper, so the only sweep that cuts third-party text would never run; the new indexes' write cost was unstated; `agent_runs.user_message_id` was cited at DDL that 0017 replaced and two columns had no citation; a second schema walker would duplicate `schema_invariants_test.go`; mai: the numeric comment-list route was not shown to exist | Point 8 refuses start with the reaper off while any watch exists, and refuses creating or enabling one (the delegation precedent, `orch/app/app.go:129-139`), with a PRO-001a test (superseded in round 53: the sweep runs on a maintenance tick that never stops; see that row); it states the index write cost, and PRO-001b checks attended `SendMessage` latency before and after the migration; the citations name the live DDL; the assertion extends `orch/db/schema_invariants_test.go`; point 1 records that both list routes and the single-issue route answered `200` under `/repositories/{id}` when checked live | A retention promise needs the loop that keeps it; an index is paid for on writes, so its price is stated; and the id routes are now evidenced, not assumed. |
| Round 53 review: background duties that needed the reaper (tur028-quiet-drain-stuck-when-reaper-disabled, QUIET_DRAIN_RECOVERY_REQUIRES_DISABLED_REAPER, INTEGRATION_WRITE_RETENTION_WITHOUT_WATCH_REAPER) | grok, sol: a crash mid-drain with `TURING_JOB_REAPER_INTERVAL_MS=0` left the quiet fence set forever; sol: F20's ledger retention ran on the same sweep, and round 52's guard covered only installations with a watch | Section 27 states one maintenance tick for every background duty this design adds (the lifecycle sweep, F20's retention, TUR-026b's batches, TUR-028's drain resume), on its own goroutine at the reaper interval or, when that is 0, at the one-minute default the session-deletion reconciler already uses (`orch/app/app.go:43,488-512`); round 52's start refusal is withdrawn; PRO-001a and TUR-028a test the zero-reaper case | One rule covers every duty instead of a guard per feature, and moving them off the reaper's goroutine also keeps orphaned-assignment recovery from waiting on them. |
| Round 53 review: PRO-001a's size (pro001a-slice-not-pr-sized) | opus: the PRO-001a row was one 28 KB cell sized M, covering many separable subsystems and over a hundred fixtures, so it was not one pull request and section 2 misstated its cost | PRO-001a ships as a1 to a6, each with its own size and its own share of the unchanged acceptance list; section 2, section 27 and F20's part row name them | Each slice lands after the ones it builds on, which takes six rather than the suggested five, because the gate precedes the poller and scope edits reset the poller's rotation and stop its polls |
| Round 54 review: the maintenance tick in the tests, and PRO-001a's later-slice checks (pro001b-sweep-begin-still-on-reaper, MAINTENANCE_TICK_STALE_REAPER_IN_ACCEPTANCE, f1-reaper-off-citation-points-at-start, PRO001A_SUBSLICE_TESTS_ASSERT_LATER_SLICE_ENTITIES) | grok, opus: PRO-001b's fixtures still said the reaper tick begins withdrawals, and the reaper-off citation named the branch that starts it; opus: PRO-001a1 to a6 asserted proposals, assessor fields, thread sessions and pending fires that only PRO-001b to d create | PRO-001b's fixtures name the maintenance tick, with the zero-reaper case; the citation is `orch/app/app.go:465,470-473`; each later-slice clause in PRO-001a's rows is marked "(PRO-001b asserts)", "(PRO-001c asserts)" or "(PRO-001d asserts)", the paragraph after the table says what the mark means, and PRO-001b, c and d each adopt the clauses marked for them | Marking keeps every assertion's wording where its context is, while each check is written by the slice that creates its subject. |
| Round 60 review: a replacement's label bytes (PRO001-EXCERPT-LABEL-BUDGET-DELTA) | mai: replacements charged only the excerpt's delta, while the budget counts excerpts and label names together | Point 2 says a replacement leaves the stored label names as admitted, since a label change writes nothing, so the excerpt's delta is the whole charge | The budget already holds; the sentence makes the reason visible where the charge is stated. |
| Round 61 review: the sweep's capacity across watches (PRO001_RECEIPT_SWEEP_CAPACITY_BELOW_AGGREGATE_INTAKE) | sol: one 100-row batch a one-minute tick reclaims 6,000 rows an hour across the hub, while each watch may write 1,000 receipts an hour, so seven noisy watches would outgrow the 24,000 ceiling (25,000 since round 62) | The sweep repeats 100-row batches, each its own transaction, while due work remains, up to 1 s of work a tick; PRO-001a6 adds a 20-watch fixture with the reaper off | The per-transaction bound protects attended writes; a per-tick count bound only starved the sweep. |
| Round 62 review: the secondary limit, the receipt ceiling, the sweep's indexes and two per-tick phrases (PRO001_QUIET_304_SECONDARY_LIMIT_UNPACED, PRO001_RECEIPT_CALENDAR_WINDOW_CEILING, PRO001_SWEEP_DUE_SELECTION_HAS_NO_INDEX, PRO001_SWEEP_STALE_PER_TICK_BATCH_WORDING) | grok: aligned `304` polls spend GitHub's secondary budget (900 points a minute per REST endpoint, verified in GitHub's docs) without moving the primary counter the reserve reads, and a header-less secondary `403` counts as an access failure; sol: the hourly cap resets on the hour, so 25 hourly buckets can overlap the 24-hour horizon; opus: the sweep's due selection had no index over a table whose admitted rows are never deleted, and two passages still said one batch per tick | A process-local token bucket (5 a second, burst 10) paces every poller request, `304`s included, and attended calls never wait for it; the ceiling is 25,000 receipts with an hour-boundary fixture; PRO-001a6 adds two partial indexes for the due selections with query-plan and latency assertions over 500,000 retained rows; both passages point to point 8's batches | Each budget must be measured on the counter that enforces it, and a bound stated for a growing table needs the index that keeps it. |
| Round 63 review: the ceiling with sweep lag, a watch's final delete, and the thread-keyed selections (PRO001_RECEIPT_CEILING_PLUS_UNSWEPT_LAG, PRO001_RECEIPT_CEILING_TESTS_IGNORE_TICK_LAG, PRO001_WATCH_FINAL_DELETE_UNBOUNDED_TRANSACTION, PRO001_DISMISS_AND_DELETION_SWEEP_SELECTION_UNINDEXED) | grok, opus: a due hour not yet swept plus a new hour's cap reaches 26,000, which the 25,000 assertions denied; opus: deleting a watch ended in one transaction over its never-deleted admitted history; the Dismiss and deletion excerpt selections had no thread key or index | The bound is 25,000 receipts younger than 24 hours and 26,000 in all, with a burst-at-the-boundary fixture; a deleting watch's content-free rows go in point 8's batches through `watch_id`-led indexes, the watch row last, with a 500,000-row fixture; admitted rows store their thread key, and a partial `(watch_id, thread_key)` index over held excerpts (with `dismiss_generation` as a third column since round 64) drives both selections, with query-plan and latency assertions in PRO-001b and PRO-001a6 | Every bound the design states must hold at the instant it is measured, and every background delete must be as small as the batches around it. |
| Round 64 review: the tick's length and order, the Dismiss seek, the assessments index and the boundary fixture (PRO001_MAINTENANCE_TICK_UNBOUNDED_BY_REAPER_INTERVAL, PRO001_DISMISS_SWEEP_SELECTION_SCANS_REWATCHED_THREAD, PRO001_SWEEP_WORK_ORDER_LETS_WATCH_DELETION_STARVE_RETENTION, PRO001_WATCH_ASSESSMENTS_NO_WATCH_ID_INDEX, PRO001A6_BOUNDARY_BURST_FIXTURE_MISCOUNTS) | opus: the maintenance tick inherited an unbounded reaper interval; the Dismiss selection filtered a re-watched thread's current excerpts row by row; a large watch deletion could take the whole second ahead of due retention work; `watch_assessments` had no `watch_id` index for deletion; grok: the boundary fixture's 00:55 cohort would already be swept at 01:00, so it could not reach 26,000 | The tick runs at the reaper interval or one minute, whichever is shorter; Dismiss sets `dismiss_sweep_pending` and the held-excerpt index gains `dismiss_generation` as its third column, so the selection is a range seek; deadline-bound work runs first in each tick; PRO-001c indexes `watch_assessments` by `watch_id`; the fixture writes its oldest cohort at 00:59 and holds the tick past it; PRO-001a6, PRO-001b and PRO-001c add the matching fixtures | Each bound must hold under the configuration and history it is stated for, and a fixture must be able to reach what it asserts. |
| Round 65 review: the reconciler's interval, the boundary fixture and the Dismiss flag (THREAD_TEXT_REMOVAL_WAITS_ON_UNBOUNDED_RECONCILER, PRO001_THREAD_SESSION_COPIES_OUTLIVE_SOURCE_BY_RECONCILER_INTERVAL, PRO001A6_BOUNDARY_BURST_REUSES_EXHAUSTED_HOUR, PRO001A6_BOUNDARY_BURST_FALLS_IN_FILLED_HOUR, PRO001B_DISMISS_SWEEP_PENDING_NEVER_CLEARS_WITH_RUN_STARTED_EXCERPTS) | grok, opus: only the reconciler's final `DELETE` removes a withdrawn thread session's copies, and its interval was uncapped, so copies could outlive a cut excerpt by hours; sol, opus: the fixture's burst fell in an hour whose cap was spent; opus: `dismiss_sweep_pending` waited on run-started excerpts that the Dismiss sweep never clears, and nothing durably marked a Start-confirmed event as having started a run | The reconciler's interval is capped like the maintenance tick, and PRO-001a6 asserts a sentinel in the session's rows is gone within about a minute of the begin under a 2-hour reaper interval; the fixture's cohorts are timed to the second, with the burst at 01:00:00 in a new cap hour through the real gate; the flag clears once no unused dismissed excerpt remains, and `watch_events.started_run_at`, set by an `auto` fire or a Confirm, marks the rest | A retention promise is kept by the step that removes the copy, and a fixture must exercise the real gate it relies on. |
| Round 66 review: removal behind execution recovery, the begin cap, and who caps the reconciler (PRO001_THREAD_RETENTION_STILL_DEPENDS_ON_EXECUTION_RECOVERY, PRO001_THREAD_WITHDRAWAL_CAP_BELOW_AGGREGATE_CREATION, PRO001A6_RECONCILER_CAP_SENTINEL_TEST_PRECEDES_THREAD_SESSIONS_AND_CAP_UNOWNED, F1_RECONCILER_ALREADY_CAPPED_MISSTATES_CURRENT_CODE) | sol: a deleting session's advance waits while an execution is still contained, and stale recovery ran only on the optional reaper; the 10-per-tick begin cap was below what 20 watches can open; opus: the reconciler cap and its test sat in PRO-001a6, before thread sessions exist, and the text called the cap existing | Point 8 states withdrawal (at the begin, unreadable at once) apart from removal (once every execution is released, at most its lease and a minute later), and the maintenance tick drives the lease-fenced stale recovery for deleting sessions (moved in round 67 into the reconciler's own pass; see that row); begins have no count cap, only the tick's budget; PRO-001b owns the reconciler cap and the stale-recovery drive, with tests for a 2-hour reaper, a disconnected executor and sustained creation; PRO-001a6's clause is marked for PRO-001b; the text calls the cap new | A deadline is only as good as the step it waits on, and a cap that creation can outrun is a backlog with a delay. |
| Round 67 review: two serial tickers, and sweep-begun receipts shown to the owner (PRO001_REMOVAL_BOUND_MISSES_RECOVERY_TICK, PRO001_RETENTION_RECOVERY_RECONCILER_DOUBLE_TICK, PRO001B_REMOVAL_BOUND_TWO_SERIAL_TICKERS, PRO001_SWEEP_QUIESCE_RAISES_DELETION_ROW, UX002_SWEEP_THREAD_RECEIPTS_SHOWN_AS_OWNER_RETRY) | grok, sol, opus: release on the maintenance tick and removal on the reconciler's tick, unaligned, made the bound the lease and two minutes; grok, opus: a sweep-begun thread-session receipt held past its quiesce deadline would show as an owner's `Retry deletion` | The reconciler's pass runs the lease-fenced stale recovery for its own pending sessions before advancing them, one driver instead of two, so the bound is the lease and a minute; `session_deletions.initiator` marks sweep-begun receipts, which the owner's list and stage 4 leave out like `watch` receipts; PRO-001b tests both, including a lease expiring just after a pass | One driver keeps a stated bound true across timer phases, and an obligation row must be one the owner can act on. |
| Round 68 review: a pass's cost, a silent stuck withdrawal, and one visibility filter (PRO001B_RECONCILER_PASS_SERIAL_COMPLETION_COST_UNBUDGETED, PRO001B_HIDDEN_SWEEP_RECEIPT_SILENT_RETENTION_BREACH, PRO001C_DOUBLE_VISIBILITY_FILTER_SESSION_KIND_AND_INITIATOR) | opus: each withdrawal runs a whole-vault memory reconcile and up to 10 s per cleaner, so 1,200 sweep withdrawals an hour, or a files-server outage, could stretch a pass past the bound and hold owner deletions behind it; hiding sweep receipts removed the only signal of a stuck one; `session_kind` and `initiator` both filtered visibility | Each pass serves owner receipts first, gives sweep receipts at most 30 s, and runs the memory completion once per pass (withdrawn in round 69: sweep receipts take none and owner receipts keep the per-session one; see that row); a sweep receipt unfinished an hour after its begin raises a content-free stage 4 row with no Retry, naming its failure class; visibility uses `initiator` alone, which `BeginWatchSessionDeletion` sets to `'sweep'`, and `session_kind` only routes; PRO-001b tests the pass under a vault and a files-server outage | A bound needs the pass that keeps it to be bounded too, and hiding an obligation is only right while nothing is stuck. |
| Round 69 review: the memory completion, the begin time, a fair cursor, and the tests that said never (PRO001B_SHARED_VAULT_RECONCILE_COMPLETES_BEFORE_FAILURE, PRO001B_PASS_LEVEL_MEMORY_COMPLETION_BREAKS_RECEIPT_CONTRACT, PRO001_SWEEP_RECEIPT_BEGIN_TIME_UNDEFINED, PRO001B_RECONCILER_SWEEP_BUDGET_STARVATION, PRO001B_OWNER_FIRST_TEST_PASSES_WITHOUT_CHANGE, PRO001_SWEEP_HOUR_ROW_CONTRADICTS_NEVER_SHOW_TESTS, PRO001C_ASSESSOR_RECEIPT_NEVER_IN_UX002_CONTRADICTS_OVERDUE_ROW) | grok, opus: a once-per-pass memory completion let receipts complete before a failing reconcile, breaking what a completed receipt means; mai, opus: nothing recorded when a receipt began; sol: a fixed walk order let stuck receipts at the front starve the rest; opus: the owner-first test passed without the change; grok, opus: two tests said a sweep receipt never appears in UX-002, against the hour row | The pass-level completion is withdrawn: sweep receipts take none, since thread and assessor turns refuse the memory tool, unless one holds memory rows (made durable in round 70 as `memory_owed`; see that row), and owner receipts keep today's per-session completion; `session_deletions.began_at` dates the hour (dropped in round 70 for `quiesce_deadline_at` less the lease; see that row); sweep receipts resume after the last one attempted; the owner-first fixture puts timing-out sweep receipts ahead of a quiescing owner receipt; both tests say never as a Retry row and the no-Retry row only after the hour | A completed receipt must keep meaning the vault no longer cites the session, and a budget needs a fair order to be a bound. |
| Round 70 review: the memory obligation, what completed means, the pass budget, the begin time, the initiator writer and the owner-first test (PRO001B_COMPLETED_RECEIPT_CITATION_CONTRACT_FALSE, PRO001B_SWEEP_VAULT_COMPLETION_EXCEEDS_PASS_BUDGET, WATCH_SWEEP_MEMORY_COMPLETION_OBLIGATION_LOST_AFTER_CASCADE, PRO001B_SWEEP_MEMORY_COMPLETION_SKIPPED_ON_RETRY_AFTER_CASCADE, PRO001B_OWNER_FIRST_TEST_STILL_NON_DISCRIMINATING, PRO001B_SWEEP_CHAT_BEGIN_INITIATOR_WRITER_UNNAMED, PRO001B_BEGAN_AT_DUPLICATES_QUIESCE_DEADLINE) | sol, opus: whether a sweep receipt owed the memory completion was read from rows its own cascade deletes, so a retry or a crash after the cascade skipped it; grok: today's completion also completes on an unavailable or over-bound vault, so 'always means' overstated it, and a vault walk could overrun the 30 s portion; opus: no chat begin was named as writing 'sweep', `began_at` duplicated `quiesce_deadline_at`, and the owner-first test still passed in session-id order | `session_deletions.memory_owed` is written in the cascade's own transaction and drives every later advance (cut in round 74 for the `memory_completion_pending` code the same transaction writes; see that row); a completed receipt means what an owner's means today, stated with its two vault exceptions; an owing sweep receipt starts its completion only at the start of the sweep portion, bounding it at 30 s plus one completion; the begin is `quiesce_deadline_at` less the lease, `began_at` is dropped; the private `beginSessionDeletion` takes the initiator; the tests assert order, a two-pass cursor bound, and the retry and crash cases | An obligation must be durable before the step that destroys its evidence, and a guarantee must say exactly what the code it reuses gives. |
| Round 71 review: the backlog bound, an owing receipt behind the cursor, the initiator's other writers, and the memory citation (PRO001_RECONCILER_BACKLOG_INVALIDATES_REMOVAL_BOUND, PRO001B_OWING_SWEEP_RECEIPT_STARVED_BEHIND_CURSOR, PRO001B_INITIATOR_DELEGATION_BEGIN_AND_EXISTING_ROWS_UNNAMED, PRO001B_MEMORY_REFUSAL_CITATION_POINTS_AT_CREATE) | sol: 30 s of sweep work a pass cannot remove a large backlog within the lease and a minute; opus: an owing receipt that was never first in the portion never got its completion; the delegation begin and receipts from before the migration had no initiator, and the one filter dropped today's child exclusion; grok: the memory citation pointed at the candidate write, not the refusals | The lease-and-a-minute bound holds while the sweep work fits one portion, and a backlog is removed in cursor order at 30 s a pass, its sessions withdrawn throughout; each pass runs one owing completion on the first owing receipt it reaches; the delegation begin passes its parent's initiator, the column defaults to `'owner'`, and the owner list keeps `parent_session_id IS NULL`; the citation names `call.go:76-81` and `209-213`; PRO-001b tests each | A bound stated without its load is not a bound, and every caller of a parameter needs a value. |
| Round 72 review: a failed completion and the cursor, the owner-turn sentence, the portion's real bound, and owing before the cascade (PRO001B_FAILED_COMPLETION_CURSOR_SKIPS_RETRY, PRO001B_OWNER_THREAD_TURN_CANNOT_CREATE_MEMORY, PRO001B_MEMORY_OWED_RATIONALE_CONTRADICTS_OWNER_TURN_EMPTY_SET, PRO001B_SWEEP_PORTION_BOUND_IGNORES_LAST_RECEIPT_OVERRUN, PRO001B_OWING_STATUS_UNKNOWN_BEFORE_CASCADE) | grok: the cursor moved past a failed completion, so it waited for a restart; grok, opus: an owner's turn in a thread session has an empty set (point 7), so it cannot create a candidate; opus: a receipt started just before 30 s runs its cleaners past the bound, and a receipt not yet cascaded could become owing mid-pass and run a second completion | Owing receipts are served earliest-first, one per pass at the portion's start, and a failed one stays first; owing is known before the cascade by one probe; the bound is that no sweep receipt starts after 30 s, plus one receipt's bounded cleaner calls and one completion; the memory text says no attached thread-session turn can create a candidate and names the vault-link path `memory_owed` covers | The retry rule must not depend on a restart, a bound must be one the code can enforce, and a reason must match the tool sets the design gives. |
| Round 73 review: owing decided apart from the cascade, and an owing head that starved the rest (PRO001B_OWING_PROBE_NOT_ATOMIC_WITH_CASCADE, PRO001_OWING_RECEIPT_STARVATION, PRO001B_FAILING_HEAD_OWING_COMPLETION_BLOCKS_LATER_CASCADES) | grok: a probe before the cascade missed a row linked while the cleaners ran; sol, opus: serving the earliest owing receipt first and leaving the others unadvanced let one failing cleanup or completion hold every later owing session's rows indefinitely | The cascade is never held back: a sweep receipt's cascade commits in cursor order and decides `memory_owed` in its own transaction; an owing receipt is left `memory_completion_pending`; each pass runs at most one completion, a whole-vault reconcile whose success completes every owing receipt cascaded before it began; PRO-001b tests a link during cleanup, a persistently failing completion and a persistently failing cleanup ahead of a removable owing receipt | A shared completion needs no queue of its own, and a receipt's bookkeeping must never keep third-party text stored. |
| Round 74 review: a wrapping cursor, what one reconcile rewrites, an unbounded owner portion, and one state instead of two (PRO001B_SWEEP_CURSOR_SKIPS_FAILED_CLEANUP_UNTIL_RESTART, PRO001B_SHARED_COMPLETION_EVERY_NOTE_FALSE, PRO001B_OWNER_PORTION_UNBOUNDED_STARVES_SWEEP_REMOVAL, PRO001B_NO_TWO_COMPLETIONS_TEST_CONTRADICTS_OWNER_INLINE_COMPLETION, PRO001B_MEMORY_OWED_COLUMN_DUPLICATES_PENDING_STATE) | grok: the cursor never came back to a receipt whose cleanup failed once, and one reconcile does not rewrite unmanaged notes or a note it cannot rewrite; opus: owner receipts went first with no limit, so a hung vault's 30 s per owner retry could hold sweep removal back, the one-completion test contradicted owner inline completions, and `memory_owed` duplicated the pending state | Both portions get 30 s and their own wrapping cursor; a reconciler pass runs no inline completion, leaves owner and owing sweep receipts `memory_completion_pending` in the cascade's own transaction, and runs one completion for all of them, while `DeleteSession` keeps its inline completion; the text says what one reconcile rewrites; `memory_owed` is cut; PRO-001b tests the cursor's return, citations removed, and 20 pending owner receipts under a blocking vault | One completion per pass must hold for every receipt it serves, and a cursor must come back. |
| Round 75 review: the fixture's arithmetic, delegation trees in the budget, the completion's place, and a pending child (PRO001B_SWEEP_ACCEPTANCE_EXCEEDS_PORTION_CAP, PRO001B_EIGHT_PASS_BOUND_INFEASIBLE_WITH_TWO_2S_CLEANERS, PRO001B_OWNER_SUBTREE_ESCAPES_PASS_BUDGET, PRO001B_PORTION_OVERRUN_IGNORES_DELEGATION_CHILDREN, PRO001B_OWNER_COMPLETION_AFTER_SWEEP_PORTION_CONTRADICTS_OWNER_FIRST_TEST, PRO001B_CHILD_PENDING_COMPLETION_HOLDS_PARENT_ROWS) | sol, opus: two cleaners per receipt made 8 passes impossible for the fixture as written; an owner receipt is a whole delegation tree, so one receipt could run far past the portion; opus: the completion after both portions made owner receipts wait for sweep work, against the owner-first test; a child left pending held its parent's cascade | The fixture names which cleaner is slow (about 15 a pass, 8 passes); the budget counts each session in a tree, and a tree left part-way resumes from `DelegationChildSessionIDs`; the one completion runs right after the owner portion when an owner receipt waits on it; a gone child pending only on the completion counts as finished for the parent's gate; PRO-001b tests a 20-child tree and a delegating chat under a failing vault | A budget is per unit of work, not per top-level item, and a shared completion must not become a reason to hold rows. |
| Round 76 review: a tree's walk and the parent's finish (PRO001B_TREE_RESUME_RESTARTS_FAILING_CHILDREN, PRO001B_TREE_RESUME_RESTARTS_AT_FIRST_CHILD_STARVES_SIBLINGS, PRO001B_PORTION_BOUND_OMITS_PARENT_FINISH_AFTER_LAST_CHILD) | grok, opus: a tree cut short restarted at its first child, whose fixed id order (`orch/repository/session_delete.go:953-965`) let three failing children use every pass; opus: the parent's own cleaners after its last child were not a start, so the portion could run past its bound | A partial tree is the owner portion's first start next pass (withdrawn in round 77: it keeps its place in the rotation; see that row), and its walk resumes after the last child attempted and wraps, from a per-parent cursor; `DelegationChildSessionIDs` leaves out a child pending only on the completion, matching the gate; the parent's finish counts as a start; PRO-001b tests three failing children ahead of 17 removable ones and a parent whose cleanup times out | The no-starvation rule must hold inside a tree as it does across receipts, and every unit of work the portion runs must be counted. |
| Round 77 review: a pinned partial tree, the pending selection, the vacuous finish test, and startup (pro001b-partial-tree-starves-owner-receipts, PRO001B_PARTIAL_TREE_STARVES_OTHER_OWNER_RECEIPTS, PRO001B_RECONCILER_SELECTION_SCANS_COMPLETED_HISTORY, PRO001B_PARENT_FINISH_DEFERRAL_TEST_VACUOUS, PRO001B_STARTUP_RESUME_PENDING_DELETIONS_UNBOUNDED) | grok, sol: round 76 put a partial tree first in every pass, so a tree whose children kept failing starved every other owner receipt; sol: each pass read every unfinished receipt through an unindexed scan of a table automatic withdrawals grow; opus: the parent-finish test never reached 30 s, and startup's synchronous resume walked every receipt, sweep backlog included | A partial tree keeps its place in the owner rotation and resumes from its child cursor when reached; portions read by keyset pages through a partial pending index; startup runs one owner portion only and leaves sweep receipts to the goroutine's first pass; PRO-001b tests a second owner receipt behind a failing tree, the selection's plan over a million completed receipts, a finish test that fills the portion, and startup under a sweep backlog | Fairness has to hold between trees as well as within one, and every read a pass makes must be bounded like its work. |
| Round 78 review: a child on a different page, and the first pass after startup (PRO001B_KEYSET_PAGES_LOSE_CHILD_PARENT_SKIP, PRO001B_STARTUP_FIRST_PASS_NOT_IMMEDIATE) | opus: today's pass skips a child whose parent is pending by checking an in-memory set of every unfinished receipt (`orch/service/sessions/service.go:133-160`), which keyset pages no longer hold, so a child could be withdrawn twice in one pass; the reconciler's first pass waits a full interval after startup (`orch/app/app.go:497-512`) | A page leaves out a child whose parent receipt is pending, by one primary-key probe per row, so it is reached only through its parent's walk; PRO-001b runs the reconciler's first pass at once; tests assert one attempt per child per pass across pages and the first sweep cleaner call within a second of serving | Paging must keep every rule the full read gave, and 'right after' must mean what it says. |
| Round 79 review: the first pass and serving (PRO001B_IMMEDIATE_PASS_BEFORE_SERVING, PRO001B_INITIAL_RECONCILER_PASS_PRECEDES_SERVING, PRO001B_IMMEDIATE_FIRST_PASS_RACES_SERVING_ASSERTION) | grok, sol, opus: the reconciler goroutine starts inside `app.New`, before `cmd/server/main.go` opens the listeners, so an immediate pass could call a sweep cleaner before serving, against the test; opus: that pass would also rerun the owner portion startup had just run | The guarantee is stated as what the lifecycle gives: startup's synchronous call makes no sweep cleaner call and `app.New` returns within one owner portion; the goroutine's first pass runs in the background without waiting an interval and skips the owner portion startup just ran; the test checks those, timed from `app.New` returning | A test must assert an ordering the lifecycle provides; non-blocking startup was the property that mattered. |
| Round 80 review: the owner completion at startup (pro001b-startup-skip-delays-owner-memory-completion, PRO001B_STARTUP_OWNER_COMPLETION_UNPLACED) | grok, opus: with startup running no completion and the first pass skipping the owner portion, an owner receipt pending on its completion at restart had no slot before sweep work, against 'an owner's deletion is never delayed by sweep work' | Startup's call runs no completion, keeping `app.New` within one owner portion; the goroutine's first pass runs the one completion first when an owner receipt waits on it, then its sweep portion; PRO-001b tests an owner receipt pending at restart beside a blocking sweep backlog | A completion is not a cleaner call, so placing it first in the background keeps both the startup bound and the owner-first rule. |
| Round 81 review: today's startup reconcile, and a test that could not tell (pro001b-startup-vault-reconcile-ignored-by-first-pass-completion, PRO001B_STARTUP_COMPLETION_BOUND_NONDISCRIMINATING) | opus: `app.New` already runs a whole-vault reconcile before `ResumePendingDeletions` (`orch/app/app.go:217-220,589-603`), which the design ignored, so a pending receipt was walked twice; grok: the startup test passed even if the synchronous call ran a blocking completion | Today's startup reconcile is kept and, on success, completes every pending receipt cascaded before it began (round 82: that is an extension, which PRO-001b owns; see that row); the synchronous `ResumePendingDeletions` makes no completion call; the first pass's completion-first branch fires only for owner receipts startup's own portion cascaded; the test blocks the vault and asserts no completion call on the synchronous path | Reuse the walk that already runs, and make the test fail on the behavior it guards against. |
| Round 82 review: the vault lock, the deletion event, and the startup reconcile's new job (PRO001B_COMPLETION_TIMEOUT_UNINTERRUPTIBLE_VAULT_LOCK, PRO001B_BULK_MEMORY_COMPLETION_DROPS_SESSION_DELETED_EVENT, PRO001B_STARTUP_RECONCILE_UNCHANGED_CONTRADICTS_NEW_RECEIPT_COMPLETION) | sol: the vault pass lock is an unconditional `Lock()` (`orch/repository/memory_reconcile.go:266`), so a completion's 30 s could not bound a wait behind another vault pass; opus: receipts completed outside `finishWithdrawal` would never publish `session.deleted`, which the client needs to drop a withdrawing session; the startup reconcile was called unchanged while given a new job, with a different notion of success | PRO-001b gives the lock a context-aware acquire, so a busy vault leaves receipts pending; every receipt a completion or the startup reconcile finishes publishes `session.deleted` through `publishSessionDeleted`; the startup reconcile is an extension PRO-001b owns, completing receipts on nil or the two tolerated errors; the startup test blocks only the completion function | A bound must be enforced where the wait happens, and a lifecycle change must keep the event the client depends on. |
| Round 83 review: the startup drain and the lock primitive (PRO001B_STARTUP_COMPLETION_DRAIN_UNBOUNDED, PRO001B_CONTEXT_AWARE_VAULT_LOCK_PRIMITIVE_AND_SECOND_SITE_UNNAMED) | sol: completing every pending receipt inside `app.New` made startup grow with the backlog; opus: no primitive was named for a context-aware acquire of a `sync.Mutex`, and the lock is taken at three sites, not one | The startup reconcile records a fence, and the first pass completes the receipts it covers in batches of 100, before any sweep cleaner call and with no second vault walk; the vault lock becomes a one-slot channel semaphore at all three sites (`orch/repository/memory_candidates.go:154`, `orch/repository/memory_reconcile.go:266,292`); PRO-001b tests a 10,000-receipt backlog at restart and an expiring `ScanMemoryVault` (round 84: PRO-001w2 tests the restart backlog and PRO-001w1 the expiring `ScanMemoryVault`; the fence became each completion's pre-walk list of receipt ids, and `SetMemoryVault` takes the slot with a blocking send) | Startup must not scale with a backlog, and a bound on a wait needs a primitive that can give one at every site. |
| Round 83 review: PRO-001b's size (PRO001B_SLICE_SIZED_M_NOT_PR_SIZED) | opus: PRO-001b held the deletion-reconciler rebuild and the event-automation feature under one M with about 80 tests | The reconciler and withdrawal-lifecycle work moves to PRO-001w1 (M: the `initiator` column, the partial pending index and keyset pages, the foreign-key indexes and the vault semaphore) and PRO-001w2 (L: the pass's interval cap, stale recovery, portions and cursors, one completion, the startup fence and first pass, and the hour row), ahead of PRO-001b, now L, each with its own size and its share of the unchanged tests; the body names the new slice wherever it attributed that work to PRO-001b; four tests that need the sweep to begin a thread session's withdrawal, which ships with PRO-001b's thread sessions, stay in PRO-001b marked '(needs PRO-001w)' | The reconciler touches no event code, so it ships and is reviewed on its own, and a moved test that needs a thread session seeds its rows directly instead of waiting for PRO-001b |
| Round 84 review: PRO-001w1's acceptance and the vault slot without a context (PRO001W1_ACCEPTANCE_TRUNCATED_THREAD_WITHDRAWAL, PRO001W1_ACCEPTANCE_PRECEDES_THREAD_WITHDRAWAL, PRO001W1_TESTS_NEED_PRO001B_SWEEP_AND_FIXTURE, PRO001W1_VAULT_LOCK_SETMEMORYVAULT_NO_CONTEXT) | grok, sol, opus: PRO-001w1 opened with a cut-off clause from a thread-withdrawal test that needs PRO-001b's sweep, and measured `SendMessage` on a fixture it never defined; grok: `SetMemoryVault`, one of the three acquire sites, has no context to select on | The 100-due-thread-sessions tick test, with the `SendMessage` at a tick's start, moves to PRO-001b marked "(needs PRO-001w)"; the `SendMessage` at a reconciler pass's start moves to PRO-001w2; w1's latency check names its own fixture (100,000 runs, approvals and messages seeded directly) and its selection test names initiators, not portions; `SetMemoryVault` takes the slot with a plain blocking send, and w1 tests that it waits for a pass in flight | A slice's acceptance asserts only what has shipped by then, and a startup-only call with no context must still never swap a cache under a pass |
| Round 84 review: what a completion completes, the roll-up's sizes and a stale round 83 row (PRO001W2_STARTUP_FENCE_HAS_NO_CASCADE_TIME_TO_COMPARE, SECTION2_F1_SIZES_OMIT_A_PRO001_SLICE, R83_DECISION_ROW_ATTRIBUTES_MOVED_TESTS_TO_PRO001B) | opus: the startup fence was a time, and no receipt stores when its cascade committed; section 2 gave 10 sizes for PRO-001's 11 slices; the round 83 row still named PRO-001b for the moved tests | Every completion, startup's included, reads the ids of the receipts then `memory_completion_pending` before its walk begins (owner receipts first, at most 10,000, in memory only) and completes only those; startup hands its list to the first pass; PRO-001w2 tests a receipt cascaded during a held walk and the cap; section 2 adds PRO-001d's M; the round 83 row is annotated | A receipt enters that state in its cascade's transaction, so reading the state before the walk makes the comparison a timestamp could not, with no clock and no new column; the regular completion had the same gap and gets the same rule |
| Round 85 review: the connection's token in kept issue text (WATCH_EXCERPT_KEEPS_CONNECTION_CREDENTIAL) | grok: the poller kept and hashed decoded GitHub text with no redaction, though section 15 redacts the same connection's credential from every decoded string, so a token pasted into an issue would reach `watch_events`, `userText` and the assessor | Bounded decoding redacts the credential from every kept string with section 15's length-preserving replacement before any cut, match, store or framing; `excerpt_revision` hashes the redacted body and the Turing-authored hash match alone uses the body as decoded, in memory only; PRO-001a2 tests a token in a body and label names (round 88: the poller keeps no title, so "title" was dropped from that list) | One control for one credential on every path that reads GitHub, and no stored digest over the token |
| Round 85 review: a second vault walk on the first pass (PRO001W2_STARTUP_PORTION_REENTRY_TRIGGERS_SECOND_VAULT_WALK) | opus: startup's owner portion re-entered owner receipts already `memory_completion_pending` (`beginSessionDeletion` returns the existing receipt, `orch/repository/session_delete.go:199-218`), so the first pass's completion-first condition always held and it walked the vault again | Portions skip a receipt whose only remaining work is the completion; the first pass runs its completion first only when, after the startup list's batches, an owner receipt not on that list is still pending; PRO-001w2 asserts no `ReconcileMemoryVault` call for a listed receipt alone | Re-entering a finished cascade does nothing but spend the portion's budget, and the startup list exists to spare that walk |
| Round 86 review: the one statement of `excerpt_revision`, and the first pass's after-sweep walk (WATCH_EXCERPT_REVISION_HASHES_UNREDACTED_BODY, pro001a-excerpt-revision-one-statement-still-hashes-unredacted-body, PRO001W2_FIRST_PASS_OTHERWISE_STILL_WALKS_VAULT) | grok, opus: point 2's one statement of the edit rule, its replacement test and the first-read rule still hashed the decoded body, contradicting round 85's redacted revision, so a token-bearing body would store a digest over the token or read as edited on every poll; grok: the general rule ran a pass's completion after the sweep portion otherwise, which on the first pass walked the vault after the startup list had completed everything | Point 2's statement, its replacement test and the first-read rule hash the redacted body, and only point 4's Turing-authored match uses the body as decoded; PRO-001a2 asserts an unchanged token-bearing issue re-listed writes nothing and is not marked; a pass runs a completion only when a receipt is pending on it, and the first pass only on its stated condition, never after its sweep portion; the round 8 row is annotated | One rule stated once must say what the fix says, and the startup list exists to spare the first pass a walk |
| Round 87 review: a replaced credential (WATCH_ROTATED_CREDENTIAL_RESTORES_REDACTED_TOKEN) | grok: after the connection's credential is replaced, an issue still quoting the old token reads as edited and its text, token included, is stored and framed | The claim is narrowed: Bounded decoding redacts the token the connection holds, says a replaced one is not recognized and is stored as written on a later read, and that replacing a credential does not revoke it; the Trust-plane bullet points there. Keeping every replaced credential, sealed, to redact it is not adopted (review dispositions appendix) | A kept copy of a retired secret only to hide it is a second secret at rest, and section 15's shipped redaction draws the same line |
| Round 88 review: a title-only edit (f10-title-edit-bypasses-observe-only) | mai: round 85's redaction sentence listed a title among the kept strings, yet the edit rule hashes only the body, so a title-only edit would change kept text without a new revision or `observe_only` | The poller keeps no title, as before round 85: the excerpt is the body, no predicate reads a title and a thread session's title never carries one. "Title" is dropped from the redaction list and PRO-001a2's token test; point 2 states that a title-only change writes nothing, and PRO-001a2 asserts it and that no row holds an issue's title | Text no model can read cannot steer a run, so a title edit is not an edit the rule needs to see; the defect was round 85's word, not the rule |
| Round 89 review: a token in a display name, and a title over the limit (WATCH_DISPLAY_NAME_SKIPS_CREDENTIAL_REDACTION, THREAD_SESSION_TITLE_EXCEEDS_MAX_RUNES) | grok: the kept `full_name` sat outside the redaction list; a full name of up to 140 characters plus `#` and the issue number could exceed the 120-rune title, through an insert that checks no length | The redaction list adds `full_name`, redacted before the charset check, so a name holding the token is refused at save and not accepted by a poll; the thread session's title cuts the name to fit 120 runes and keeps the whole issue number; PRO-001a2 and PRO-001b test both | Every kept GitHub string meets one control, and a title the session table's own rule would refuse must not be written by a path that skips it |
| Round 89 review: an excerpt copied into a pre-existing sandbox file (PRO001_LEGACY_SANDBOX_EXCERPT_RETENTION) | sol: an approved `files` update of a file that predates the session is `retain_legacy_unowned`, which withdrawal keeps, so excerpt text could outlive its source with no detachment | Such an update is refused `thread_session_unowned_file` in `ReserveSandboxArtifact`'s transaction while the session is attached, before any byte is written; the session's own files stay deletable; after Treat as normal conversation it is allowed; PRO-001b tests all three | Deleting the owner's pre-existing file is not an option, so the copy must never be made while the retention rule applies |
| Round 89 review: the title-edit test's object (PRO001A2_TITLE_EDIT_TEST_WRITES_NOTHING_CONTRADICTS_CURSOR_ADVANCE) | opus: "writes nothing" contradicted the cursor rule, which advances `since`, and "no row" named no tables | The test names what stays byte-identical (the event row, any pending fire and proposal), lets `since` advance, and scopes the title check to the poller's rows | A test must be satisfiable by the specified behavior and precise enough to catch a stray excerpt write |
| Round 90 review: the poll path for a token-bearing name, and an approval that could only fail (PRO001A2_DISPLAY_NAME_TOKEN_POLL_UNTESTED, PRO001_UNOWNED_FILE_REFUSAL_ONLY_AFTER_OWNER_APPROVES) | grok: PRO-001a2 tested only the save, not a poll reading the token from `repository_url` or the repository request; opus: the owner was shown an approvable card for a write the reservation would always refuse, though the preview already holds the physical path | PRO-001a2 adds both poll fixtures: the stored name and `name_checked_at` stay unchanged and a later thread session takes the earlier name. While the session is attached, `GetApprovalDetails` reports such a file mutation `UNAVAILABLE`, so it cannot be approved, and the reservation's refusal remains the race-safe gate; PRO-001b tests the card, the refused approve and a direct reservation | Each path a rule names gets a fixture, and an owner's decision is never asked for a write already decided |
| Round 91 review: an older event confirmed into a newer session (PRO001_THREAD_RETENTION_OUT_OF_ORDER_EXCERPTS) | sol: withdrawal 30 days after the session's first excerpt assumed every later excerpt was newer, but a Confirm can bring an older, edited event into a session a newer event opened, so that event's copies outlived its source | `watch_threads.session_withdraw_at` holds the earliest expiry of any event the session took in, set when it opens and lowered with `MIN` in each Confirm's or fire's enqueue transaction; the sweep selects due sessions through its index; the "first excerpt" wording becomes "oldest excerpt" in sections 3, 6 and 27; PRO-001b tests the out-of-order case and the plan | The rule is about the oldest source, so the deadline must follow the oldest source, whatever order the events arrived in |
| Round 91 review: the card for a refused thread-session write (PRO001_UNOWNED_FILE_CARD_TELLS_OWNER_TO_RETRY) | opus: the client shows `UNAVAILABLE` as "Preview unavailable. Retry or deny this request.", sending the owner into retries that cannot succeed while the session is attached | `ApprovalDetails.unavailable_reason` (field 16) carries `thread_session_unowned_file`; for it the client shows fixed text naming Deny and Treat as normal conversation, with no Retry, while any other unavailable preview keeps today's text and Retry; PRO-001b tests both | The client cannot tell a refusal from a transient preview failure without being told, and only the transient one is worth retrying |
| Round 92 review: a due time never cleared, and a card that could not change (PRO001_SESSION_WITHDRAW_AT_NEVER_CLEARED, PRO001_UNOWNED_FILE_CARD_NO_PATH_AFTER_DETACH) | opus: nothing cleared `session_withdraw_at`, so every withdrawn or detached thread stayed due and filled the sweep's seek ahead of newly due sessions; with Retry gone, nothing re-read a pending card after a detach, and an override stored in the preview snapshot would outlive it | `session_withdraw_at` is set to NULL at the sweep's begin and at a detach, and lazily when the sweep finds the session no longer active, under a partial index that holds only due candidates; the unowned-file override is applied on every read and never stored, and the client reloads the session's pending cards after a detach, so the same approval can then be approved; PRO-001b tests each, with 10,000 spent threads in the fixture | A deadline must leave the index once it is spent, and a refusal whose cause the owner can remove must lift when it is removed |
| Round 93 review: a detached copy whose watch is deleted (WATCH_DELETE_DROPS_DETACHED_EXCERPT_EXPIRY) | sol: a detached session's `watch_event` rows are cut 30 days after their event, but deleting the watch removed the `watch_events` rows that cut relied on, so the copies could stay for good | Each `watch_event` message has a `watch_event_expiries` row (message id, anchored job, `expires_at`, cascading with the message, indexed on `expires_at`), written with the message; the cut selects due rows from it and deletes each with its cut, so it needs neither the watch nor its events; PRO-001b tests detach, delete the watch, restart and pass the expiry | The cut's own record must live exactly as long as the copy it removes, not as long as the watch |
| Round 93 review: where the unowned-file override goes, a card on another client, and the column list (PRO001_UNOWNED_FILE_OVERRIDE_PLACEMENT_OVERWRITTEN_BY_RECHECK, PRO001_UNOWNED_FILE_CARD_STUCK_ON_OTHER_CLIENTS, PRO001_WATCH_THREADS_COLUMN_LIST_OMITS_WITHDRAW_AT) | opus: an override placed after the snapshot load could be replaced by the file re-check and leave a reason beside `STALE`; only the detaching client reloaded its cards; point 6's `watch_threads` list omitted `session_withdraw_at` | The override is applied last, immediately before `can_approve`, only on `READY`, and the reason is set only then; the card offers Check again (a details re-read) on every client instead of a reload on detach; point 6 lists `session_withdraw_at` and its partial index and the new `watch_event_expiries`; PRO-001b tests an empty reason off `UNAVAILABLE` and a second client's card | One state decides one reason, every client can see a lifted refusal, and the schema list is where an implementer looks first |
| Round 94 review: which rows the expiry cut takes (PRO001_EXPIRY_CUT_SCOPE_UNSTATED_FOR_ATTACHED_AND_DELETING_SESSIONS) | opus: point 8 named only detached sessions, so a cut filtered to them would leave due rows of attached and deleting sessions in the `expires_at` range for good | The cut takes every due row whatever its session's state and is idempotent, so each row is selected once; PRO-001b adds the 10,000-row fixture. opus's alternative of dropping the table is not adopted (review dispositions appendix) | A due range must drain, as `session_withdraw_at`'s does |
| Round 95 review: one copy cut, and a backlog bound the tick can meet (PRO001_EXPIRY_CUT_DRAIN_CONTRADICTS_WAIT_AND_MECHANISM, PRO001B_EXPIRY_10K_FIXTURE_MISSES_ONE_TICK_BOUND, PRO001B_EXPIRY_BACKLOG_FIXTURE_EXCEEDS_TICK_BUDGET) | grok: round 94's sentence sat beside the detach exception, so it read as keeping tool-call and approval records in attached sessions, it credited idempotence with the drain, and it seemed to contradict the begin-first rule; grok, opus: 10,000 due rows ahead of a new one cannot drain inside one 1 s tick | The detached bullet keeps the exception, now said to cover a detached session only; a separate "copy cut, stated once" bullet says every due row is cut once and leaves the range because its expiry row is deleted in the cut's transaction, with the message rewrite in detached and not-yet-withdrawn sessions and the expiry row alone in deleting ones, and that only the source cut waits for a begin; the dangling "(below)" points above; the fixture asserts a drain of at least 100 rows a batch within the tick budget and a one-tick cut of a row due after the drain | A cut that changes nothing cannot drain a range, and a bound must fit the budget that enforces it |

## 4. F2 — Mandate: immutable grant, revalidated before re-entry

### Verdict

Adapt. Turing does not add a Mandate entity: it already freezes authority per
run (automation allowlists, delegation profile revisions, egress decisions,
argument-bound approvals). It adopts DeepSeek's rule, made stricter: **a
frozen grant is immutable, and at every re-entry it is intersected with
current policy; it may only narrow or end, never widen.** It also makes an
automation's frozen allowlist a ceiling on every tool, not only on the ones
that need approval. This closes four gaps verified in Turing's code, delivered
as **TUR-026**.

### DeepSeek source

- A Mandate records strength, policy version, basis (including matched rules
  and sender), actor and scope; it is immutable (`dsh:src/core/model.ts:116-133`;
  `dsh:docs/001-domain.md:34`).
- Only accept creates it, a person's stop withdraws it, and failed
  revalidation revokes it (`dsh:docs/002-authorization.md:129`).
- `revalidate` checks the basis against today's Policy before an ended Ticket
  resumes (`dsh:src/policy.ts:213-241`; `dsh:docs/002-authorization.md:131-141`;
  call site `dsh:src/core/tickets.ts:257`).
- A policy change sends a notice to affected live Tickets instead of revoking
  them (`dsh:src/policy.ts:144-166`; `dsh:docs/002-authorization.md:111-117`).
- Weaker than Turing should accept: an explicit follow-up Mandate passes
  revalidation regardless of who sends the resuming message
  (`dsh:src/policy.ts:221`; `dsh:docs/002-authorization.md:127,135`).

### Turing today

- Automation runs copy their allowlist at fire time into
  `automation_runs.allowed_tools_json`
  (`orch/db/schema/0012_derived_state_provenance.sql:41-47`), read back by
  `GetAutomationRunGrant` (`orch/repository/automations.go:788-810`). The
  current allowlist lives in `automation_allowed_tools`, keyed by
  `(automation_id, server_name, tool_name)`
  (`orch/db/schema/0009_automations.sql:50-55`), and `UpdateAutomation`
  replaces its rows.
- Delegations bind the granted profile revision at creation, re-checked in
  the creating transaction (`orch/repository/delegations.go:174-183`), and
  store it as `delegations.profile_revision`
  (`orch/db/schema/0026_delegations.sql:14-33`). `CallTeamTool` resolves the
  profile file through the team resolver and requires it Active at that
  revision (`orch/service/team/call.go:168-193`).
- Tool policy is re-read live on every call (`orch/service/runtime/service.go:2939`).
- A job that sets `enforceSelectedTools` is held to its persisted set before
  the policy is read, safe tools included (`orch/service/runtime/service.go:2925-2935`;
  `orch/repository/tool_selection.go:30-47`).

**Gap (a), verified.** The unattended pre-approval path reads only the frozen
allowlist. `GrantUnattendedApproval` never re-reads the automation
(`orch/service/approvals/service.go:500-523`); neither does the beacon branch
that decides to pre-approve (`orch/service/runtime/service.go:2961-2971`).
`DeleteAutomation` is a bare delete (`orch/repository/automations.go:368-377`),
`SetAutomationEnabled` and `UpdateAutomation` change only the automation's own
rows (`orch/repository/automations.go:266-366`), and `automation_runs` has no
foreign key to `automations`. A run that is queued, running or recovering
after the owner deletes, disables or narrows its automation therefore keeps
auto-approving mutations on the old pairs, for as long as the run lives (up to
`TURING_QUEUE_MAX_WAIT_MS` queued, 12 hours by default, `orch/config/config.go:34`).
The check also runs before slow work: `GrantUnattendedApproval` reads the grant,
then refreshes the mutation preview (file I/O) and signs a token, and only then
commits through `ApproveApprovalWithEvent`, which does not look at the
automation again (`orch/service/approvals/service.go:508-565`). A revocation
that lands in between is missed even once the read is added.

**Gap (b), verified.** `SetAgentProfileEnabled` and `GrantAgentProfile`
update only `agent_profile_settings` (`orch/repository/team.go:56-101`), and
the claim query does not consult it (`orch/repository/jobs.go:1340-1426`). A
child that is queued, or held for egress consent, when its specialist is
disabled or re-granted at a different revision still starts later with the
superseded authority. The file is authority too: deleting `team/<id>/AGENT.md`
or editing one of its authority fields changes nothing in SQLite, and the
egress consent path (`orch/service/team/egress.go:208-259`) re-checks tools
and route but not the profile.

**Gap (c), verified, smaller.** A lost assignment requeues the same run for
another attempt (`orch/repository/assignments.go:315-336`).
`GrantUnattendedApproval` does not consult earlier approvals of the same run,
so a repeated allowlisted mutation with identical arguments is pre-approved
again.

**Gap (d), verified: the allowlist does not bound safe tools.** The beacon
reads the automation grant only inside the `approval_required` branch
(`orch/service/runtime/service.go:2961-2971`); a `safe` tool reaches the
allow decision for any run (`orch/service/runtime/service.go:3042-3049`). The
fire enqueues with no selected set (`orch/repository/automations.go:697-703`),
so the run is offered every tool its worker serves and the `tool_not_selected`
gate never applies. Today an automation therefore runs `files.read`,
`files.list`, `files.search`, the four `system` tools
(`orch/service/tools/defaults.go:19-26`) and any third-party MCP tool the
owner has marked `safe`, none of them on its allowlist. Only memory and team
tools are refused, by their own services
(`orch/service/memory/call.go:198-215`; no roster is frozen on an automation
run). The allowlist reads, in the app, as the automation's tool authority; a
tool result (a fetched page, a file) can steer an unattended run into any safe
tool instead.

### Unified design

1. **Invariant, written into TUR-026.** A frozen run grant is immutable. At
   each re-entry (claim of a queued job, recovery requeue, every tool call of
   an unattended run, unattended pre-approval) it is intersected with current
   policy. It may narrow or end the run; it never widens. A policy change does
   not cancel a run that is executing; the run meets the narrower grant at its
   next re-entry, which for an unattended run is its next tool call of any
   kind.
2. **(a) Automation grants, checked at every tool call and where the approval
   commits.** One function, `unattendedGrantCheck`, runs for every tool
   call of an unattended run, `safe` reads included, at the beacon and
   at registered-MCP dispatch, where it runs at entry, before any approval
   is consumed, and again in the read transaction that holds the dispatch
   fence (point 5 places it and says what stays trusted). It reads the run's frozen grant (already read today), the
   automation row and the pair in `automation_allowed_tools`, each by primary
   key, and, for an event automation, the watch its `watch_id` names and the
   originating event its `automation_runs.watch_event_id` names. It blocks
   the call when the pair is not on the frozen set (`automation_tool_not_allowlisted`); when the
   automation is gone or disabled, or the pair has left its current
   allowlist (`automation_grant_revoked`); or when the watch is gone,
   disabled or no longer in `auto`, or the event is no longer eligible: its
   repository or event kind is no longer listed, its author's id is no
   longer a listed actor or listed bot (section 3, point 3), the rule's current predicate no longer
   matches it, or the rule may no longer fire unattended; each is
   `watch_grant_revoked`. The scope and
   predicate part is one pure function, `watchEventEligible(watch,
   automation, event)`, the same one the matcher and Confirm use, evaluated
   over the metadata the event row stores (section 3, point 6); it consumes
   nothing, so the pending-fire hook (section 6) and `FireEventAutomation`
   call it too before they fire. The rule part is a second pure function,
   `mayFireUnattended(automation)`: true only when the automation has no
   `assess_instruction` and its predicate names no label, the two kinds of
   rule that never fire unattended (section 7). It is the one check of
   both: `FireEventAutomation` calls it before the winner is chosen, the
   pending-fire hook before it fires, and `unattendedGrantCheck` at every
   call, where a failure blocks with `watch_grant_revoked` and raises no
   attention row. It stays outside `watchEventEligible` because the matcher
   and Confirm share that function, and a rule with an assessor or a label
   term must still match and be confirmable by the owner. So
   revoking an automation, or narrowing a watch's scope or a rule's
   predicate, stops the reads its run may still make, not only its
   mutations. The binding check for a mutation moves into the approval-commit transaction: a new
   `repository.ApproveUnattendedApprovalWithEvent` re-reads, in the same
   transaction that flips the approval to approved, the same rows and (c)'s
   repeat guard, then performs the same update as `ApproveApprovalWithEvent`.
   `GrantUnattendedApproval` calls it instead. Exactly four codes fail the
   run: `automation_tool_not_allowlisted`, `automation_grant_revoked`,
   `watch_grant_revoked` and (c)'s `unattended_repeat_blocked`. At the
   beacon each takes the existing `blockUnattendedTool` path, given its code, which fails the
   run under the automation-policy origin with an `automation.tool.blocked`
   audit row (`orch/service/runtime/service.go:3079-3108`), as an
   un-allowlisted mutation does today; at registered-MCP dispatch the same
   codes reach a runtime-owned refusal hook, and both paths fail the run
   and write that row through one repository call, in one transaction
   (point 5). The one non-fatal refusal, for tools
   on `memory` and `team`, is decided before this check (point 5). Widening the allowlist mid-run widens nothing: the
   frozen set still bounds the run. This deliberately reverses a documented
   choice: the schema says deleting an automation "does not retract consent
   already given to a run underway" (`orch/db/schema/0009_automations.sql:64-69`;
   `orch/repository/automations.go:368-370`). The deletion still keeps the run
   and its `automation_runs` record; it now retracts the consent.
3. **(b) Delegation children, SQL and file.** The claim query skips a
   delegation child whose `agent_profile_settings` row is not `enabled = 1 AND
   granted_revision = delegations.profile_revision`. The transactions behind
   `SetAgentProfileEnabled` and `GrantAgentProfile` write only the setting,
   as today, and that row is the claim fence: from their commit on, the
   claim skips every child the change disqualifies, so their work does not
   grow with the profile's backlog on the one database connection. The
   maintenance tick (section 27) then cancels queued and
   consent-held children that no longer qualify in batches of at most 100,
   each its own transaction that re-reads the child's setting, through
   `cancelRunTx` with a new abandoned cancellation, `profile_grant_revoked`
   (not `user_cancelled`), so the existing join and notice machinery reports
   them. The batch query is the same predicate as the fence, so a crash
   resumes on the next tick with no marker. A child the owner re-enables
   before a batch reaches it is left runnable, because it qualifies again at
   claim, but only while `granted_revision` still equals its frozen
   `delegations.profile_revision`: `GrantAgentProfile` writes only
   `granted_revision` (`orch/repository/team.go:78-101`), so a re-grant at
   another revision does not requalify it, and the claim keeps skipping it
   until a batch cancels it. Because the file is authority too, the profile is also resolved
   through the same team resolver `CallTeamTool` uses, at two points outside
   every transaction and lock: in `dispatchToWorker` after a delegation child's
   claim commits and before it is assigned
   (`orch/service/runtime/service.go:1646-1679`), and in
   `resolveDelegationEgress` before consent is signed or recorded. A profile
   that is not Active at `delegations.profile_revision` cancels the child with
   `profile_grant_revoked` instead of dispatching it, or refuses the consent.
   A running child is not cancelled.
4. **(c) Repeat after recovery.** Inside the same commit transaction, an
   unattended approval is refused when the run already has a consumed
   approval for the same server, tool and `args_hash`, and blocks with
   `unattended_repeat_blocked`. `approvals` has no server column and no
   run-scoped index (`orch/db/schema/0001_initial.sql:127-144`), so the lookup
   starts from `tool_calls`, which has both and the `(run_id, created_at)`
   index (`orch/db/schema/0001_initial.sql:107-125`), and joins `approvals` on
   its unique `tool_call_id`. This is the code-enforced form of DeepSeek's
   prompt-level recovery note (`dsh:docs/003-execution.md:146`). Ceiling: it
   also blocks an intentional identical repeat inside one attempt; that run
   fails visibly and the owner reruns it.
5. **(d) The allowlist is a ceiling, enforced at the beacon and at
   dispatch.** The
   unattended check (point 2) moves ahead of the policy branch, so every tool
   call of an automation run, `safe` included, is checked. Two tools are
   exempt: the skill loaders `skills/skills_list` and `skills/skill_view`,
   which read only the job's own pinned snapshot of owner-granted skills
   (`agent-runtime-go/internal/agent/general_assistant.go:1049-1051`) and
   change nothing, so an automation keeps the skills it has today. A tool on
   `memory` or `team` is refused first, before the allowlist check, without
   failing the run, because today both are refused that way: memory by its
   service after a `safe` beacon (`orch/service/memory/call.go:209-213`),
   and `team.delegate` by `CallTeamTool`, since no roster is frozen on an
   automation run (`orch/service/team/call.go:154-166`). The check answers
   such a call through the existing `denyToolBefore`
   (`orch/service/runtime/service.go:3212`) with
   `automation_tool_unsupported`, a recorded `tool.call.denied` the model
   reads as a tool error: today's outcome, now decided earlier and in one
   place, and nothing on those servers runs. Integration tools are not in
   that group, although the save path refuses them as well
   (`orch/repository/automations.go:36-47`). The `github.*` tools default to
   `approval_required` (`orch/service/tools/defaults.go:53-64`) and are
   offered to any run while a connection exists
   (`orch/service/integrations/tools.go:59-89`), so today an automation's
   call to one fails the run with `automation_tool_not_allowlisted` and an
   `automation.tool.blocked` row (`orch/service/runtime/service.go:2961-2971`).
   They stay on that failing path, now through the ceiling check, so an
   injection-steered comment attempt stays in the audit log and, once
   UX-002 lands, in a `person/blocked` row. One that the owner marked `safe`
   was refused today only by the run's missing egress decision
   (`orch/repository/egress.go:432-451`), as a tool error; it now fails the
   run the same way. A scheduled automation's job does
   **not** gain an enforced selected set:
   its routing, its skills and its context budget stay exactly as they are,
   and `unattendedGrantCheck` is the boundary, at the beacon and at
   dispatch (below). That also keeps the failure visible. With an enforced set, the worker answers a call to an unselected
   tool itself, as an unknown tool, before any beacon
   (`agent-runtime-go/internal/agent/general_assistant.go:1036-1041`), so
   nothing would be audited and the run would carry on. Without one, the call
   reaches the beacon, which fails the run with `automation_tool_not_allowlisted`,
   an audit row and, once UX-002 lands, a `person/blocked` row. Upgrade
   behaviour: an existing automation that relied on an unlisted safe tool on
   a server it can allowlist (`files`, `system` or a third-party MCP server)
   fails visibly at that call and works again once the owner adds the tool;
   one that called a memory tool gets the same refusal it gets today and
   carries on, and one that called a `github.*` tool fails as it does today
   (or, for a read the owner marked `safe`, now fails instead of getting a
   tool error). The release notes say so. No allowlist is backfilled,
   because guessing what an automation should be allowed to do is the gap
   being closed.

   **Where the check runs, stated once.** The beacon is not the only way
   in: the `runtime` identity may also call the orchestrator's tool facets
   directly (`orch/app/app.go:339-354`), with or without a beacon, so the
   check covers each facet an unattended run can reach.
   - *Registered MCP servers.* `CallRegisteredMcpTool` delegates to
     `mcpregistry.CallTool` (`orch/service/mcpregistry/service.go:1098-1112`),
     which, for a job with no enforced set, today checks only the policy,
     a remote server's egress decision and `MCPDispatchActive` (tool and run
     liveness), so a `safe` tool on a `local_container` server runs with no
     approval and no allowlist (`orch/service/mcpregistry/call.go:36-140`;
     `orch/repository/mcp_servers.go:662-689`). `CallTool` now runs
     `unattendedGrantCheck` twice, re-reading the frozen grant, the
     automation, its allowlist and, for an event run, the watch, the event
     and `mayFireUnattended`:
     1. *At entry*, right after it reads the server and applies the
        `team`-name withdrawal (`call.go:36-48`), for every tier
        (`bundled`, `local_container` and `remote_url`,
        `orch/repository/mcp_servers.go:18-20`), before the selection,
        bundled-server, enabled, egress (a remote-URL server's decision
        included), policy and approval steps (`call.go:49-112`). From
        PRO-001c, a run whose enforced selected set is empty ends earlier
        still, before the server is read (section 5, point 3). An allowlisted tool on a
        `remote_url` server passes and is then refused, as today, by the
        run's missing egress decision (`call.go:64-78`;
        `orch/repository/egress.go:579-591`), because an automation's
        fire enqueues with none.
     2. *At dispatch, atomic with the fence.* For an unattended run,
        `MCPDispatchActive` runs the same grant reads (the transaction
        helper `ApproveUnattendedApprovalWithEvent` also runs in its commit
        transaction, point 2) and its existing liveness `SELECT` in one
        short read transaction, over one snapshot, and returns the refusal
        code beside `active`. No network I/O runs inside it, and the database's single
        connection (`orch/db/connection.go:47`) admits no commit between
        the two reads. This is the dispatch linearization point
        (`call.go:125-136`): a revocation committed before it is refused,
        and one committed after it meets a call already in flight, as a
        cancellation does today.

     The second check runs only when the first found an `automation_runs`
     row, because a run's unattended status is fixed at insert. The server
     is reached only after both pass.

     **What a refusal does to the call's approval, stated once.** There
     are two cases, and TUR-026d tests each:
     - *Revoked before the call starts.* A call whose automation was
       already narrowed or disabled when the call starts is refused at
       entry, before the approval step (`call.go:92-110`). Its approval is
       never consumed; failing the run expires it and clears its token, as
       every terminal transition does (`orch/repository/runs.go:255-260`,
       through `failRunTx`).
     - *Revoked during the call.* A revocation committed after the entry
       check passed, while the call's approval is being handled, is
       refused at dispatch. The approval is consumed (spent), as after any
       refusal at that point (`call.go:125-136`), and nothing is sent.

     A refusal reaches no server and returns
     `PermissionDenied` naming the code, even when failing the run (below)
     errs, which is logged.

     The refusal is made visible by the runtime, which owns the event bus
     (`orch/service/runtime/service.go:3552-3565`); `mcpregistry` holds no
     bus and only an unconditional audit `Record`
     (`orch/service/mcpregistry/import.go:163-170,361-363`). `mcpregistry.Server`
     gets one runtime-owned refusal hook, `SetUnattendedRefuser`, wired in
     `app.New` beside the approval enforcer and the registry-change
     notifier, which already inject the approval service and the runtime
     (`orch/app/app.go:278-282`; `call.go:32-34`). A server with no hook
     wired (tests only) refuses every unattended call, as `CallTool`
     refuses an approval-required call with no enforcer (`call.go:88-90`).
     The hook passes the run id, the automation id and name from the grant
     the check read, the server and tool names and the code.

     **One repository call fails the run and writes its audit row.**
     `repository.FailUnattendedRun(ctx, runID, failure, audit)` does, in one
     transaction:
     1. `failRunTx` with the normalized failure (automation-policy origin,
        the code, retry never), `PreserveExecution: true` and the run's
        state version resolved inside the transaction (`resolveVersionInTx`,
        `orch/repository/run_terminal.go:58-65,240-275`;
        `orch/repository/run_state.go:606-615`), as lease recovery already
        does inside its own transaction (`orch/repository/assignments.go:540`).
        There is no separate version read, so no window in which another
        writer's lifecycle bump turns the failure into a conflict. An active
        execution stays active and is marked `uncertain`, not exited, until
        the worker acknowledges.
     2. Only when that transition applied and `audit` is not nil, the
        `automation.tool.blocked` row through `recordAuditTx`
        (`orch/repository/audit.go:201-209`), under the condition
        `RecordAuditForExistingRun` checks today, that the run's session is
        still active (`audit.go:164-199`): automation actor, correlated to
        the run, the beacon's tool call id as target (empty on a direct
        call, whose request carries none, `proto/turing/v1/mcp.proto:142-148`),
        and a payload naming the server, the tool, the automation and the
        code, with no arguments.

     A run that is already terminal or has moved (`ErrRunNotFailable`,
     `ErrRunTransitionConflict`; `orch/repository/runs.go:20`;
     `orch/repository/run_state.go:103`), or a write-free duplicate
     (`run_state.go:653-669`), returns `moved = false` with no events and
     writes nothing. The failure and its row commit together or not at
     all. After the commit the runtime publishes the returned events on its
     bus, so a session already subscribed (`orch/service/events/service.go:81`;
     `orch/service/chat/service.go:135`) receives `agent.run.failed`
     without resubscribing and UX-002's hint fires.

     `failUnattendedRun` (`orch/service/runtime/service.go:3117-3142`)
     makes that call in place of its `currentStateVersion` read and
     `FailRunCanonical` (`service.go:3118-3123,3182-3188`), with an optional audit
     entry. The `automation_approval_failed` grant-failure path
     (`service.go:3002-3007`) passes none, so it still writes no
     `automation.tool.blocked` row. `blockUnattendedTool` passes its
     blocked-tool entry instead of recording it first, and the refusal hook
     makes the same call with its entry and publishes the events the same
     way. So a beacon-less direct call is as visible as one the beacon
     refused, and a repeat refusal on a run that is already failed writes
     nothing and changes nothing. One change reaches the beacon path:
     `blockUnattendedTool` writes its audit row before the failure today,
     whatever the failure does; it now writes it in the failure's
     transaction, only when the run moves, so a replayed beacon, or one
     that loses a race to another terminal transition such as the owner's
     cancel, writes no row; the run's own terminal record stands. A
     scheduled job's payload is unchanged.
   - *Memory, team and integrations.* These keep their existing refusals,
     which already cover a direct call: `CallMemoryTool` refuses any run
     with an `automation_runs` row before it reads anything
     (`orch/service/memory/call.go:209-213`); `CallTeamTool` refuses a run
     with no frozen roster, and none is frozen on an automation run
     (`orch/service/team/call.go:154-166`); and `CallIntegrationTool`
     refuses, before and again at dispatch, a run whose egress decision
     does not cover the tool (`orch/service/integrations/call.go:27-29,72-74`;
     `orch/repository/egress.go:432-451`), while an automation's fire
     enqueues with no egress decision (`orch/repository/automations.go:697-703`).
   - *What stays trusted.* The bundled `files` and `system` servers. The
     runtime calls them itself, with its own bearer tokens
     (`agent-runtime-go/cmd/runtime/main.go:115-120`), and `CallTool`
     refuses a bundled server (`call.go:58-60`), so the orchestrator sees
     those calls only through the beacon. For a read on them the beacon is
     the boundary, and a runtime that skipped it could read the sandbox, as
     it already can on any run. A `files` mutation also needs an approval
     token, which only the approval-commit transaction (point 2) grants.
6. **PRO-001 runs.** An `auto` run (PRO-001d) is recorded in
   `automation_runs`, so it inherits every unattended gate and this
   revalidation, at every tool call. Its check also re-reads the watch
   (present, enabled, still `auto`) and re-evaluates its originating event
   against the watch's current scope and the rule's current predicate
   (point 2). Event-started runs do carry an enforced
   selected set (sections 5 and 7), as a narrower second line.
7. **Not built.** A Mandate table, free-text scope, sender basis (single
   user), resume of ended work, a policy hash, or policy-change notices to
   running runs.

### Security and privacy

Closes the stale-authority window on the only unattended path
(`docs/NORTH_STAR.md:296-299`): the owner's revocation takes effect at the
run's next tool call, read or mutation, not after the run, and no revocation
can slip between the check and the commit of a mutation. (d) makes the owner's
allowlist the whole of an unattended run's tool authority, which is what the
Automations page already implies, apart from the two skill loaders, which
read only the owner's own granted skills already pinned on the job. The
check also runs at registered-MCP dispatch, at entry and again in the
read transaction that holds the dispatch fence, so a runtime that skips
the beacon still reaches no third-party server outside the allowlist and
is failed, published and audited exactly as a beacon refusal is, the
failure and its audit row in one transaction. What a refusal does to the
call's approval is stated once in point 5. Reads on the bundled `files`
and `system` servers rely on the beacon (point 5).
Narrow-only intersection cannot escalate. No new data is stored or exposed.
Touches owner decisions 2 and 7.

### Performance

(a) and (c): inside the existing approval-commit transaction, one primary-key
read each of `automation_runs`, `automations` and `automation_allowed_tools`,
and one `tool_calls` range read by `run_id` with a join on the approvals'
unique index, per approval-required call on unattended runs only. (a) and
(d): the beacon's unattended check (the frozen grant, already read today,
plus one primary-key read of the automation, one of its allowlist and, for a
PRO-001 run, one of the watch and one of its originating event) runs for every
tool call of an unattended run;
to know whether that check applies, every BEFORE beacon, attended ones
included, does one primary-key read of `automation_runs`, which misses for an
attended run (today that read happens only in the `approval_required` branch,
`orch/service/runtime/service.go:2961-2971`). The same check at registered-MCP
dispatch costs every `CallRegisteredMcpTool` call one more primary-key read
of `automation_runs`, attended or not, plus the reads above twice for an
unattended run (at entry, and in the dispatch fence's read transaction,
which now wraps the liveness read and holds the single connection for
those primary-key reads only, with no network I/O); a refusal adds one
failure transaction carrying one audit insert, once per run, and no longer
a separate state-version read. No other facet changes. Scheduled automation jobs keep their payload, so
their prompt size and routing do not change. (b): one primary-key read per
delegation-child candidate inside the existing claim transaction; one
profile-file resolution per delegation-child claim and per consent decision,
outside every lock; one row written by the settings transaction; and, on
the maintenance tick after a revocation, cancellation batches of at most 100
children, each its own transaction.
Existing repository timing tests assert the claim and approval transaction
budgets.

### Roadmap placement and dependencies

New **TUR-026 — Revalidate frozen grants at re-entry**, Workstream A. No
dependencies; it ships first because it closes real gaps in shipped code. It
is distinct from TUR-025, which fences authorization to the run's lifecycle,
not to the current grant.

### Delivery slices

| Slice | Size | Acceptance tests |
| --- | --- | --- |
| TUR-026a Automation grants | S | Delete, disable or narrow while the run is queued, running and recovering: the next tool call, a `safe` read such as `files.read` as well as an allowlisted mutation, is blocked with `automation_grant_revoked`, fails the run with an audit row, and nothing executes; a barrier test revokes after the preview refresh and before the commit, and the approval stays unapproved with no token issued; an unchanged automation still pre-approves; widening mid-run widens nothing; a deleted automation's run keeps its `automation_runs` row |
| TUR-026b Delegation children | S | The maintenance goroutine (section 27) is added here unless TUR-028a already added it; Disable, or re-grant at a new revision, before claim: the child is never claimed, is cancelled `profile_grant_revoked`, and the join reports it once; deleting the profile file, or editing an authority field, before claim cancels the child at dispatch and it never reaches a worker; consent for such a child is refused; an unchanged profile claims normally; a running child is not cancelled; a consent-held child is cancelled when its profile is disabled; with 10,000 queued and consent-held children of one profile, disabling it commits a settings transaction that touches one row, no maintenance transaction (section 27) cancels more than 100, the same holds with `TURING_JOB_REAPER_INTERVAL_MS=0`, an attended job enqueued during the batches is claimed before they finish, every child ends `profile_grant_revoked`, and each parent joins exactly once; a crash between batches resumes on the next tick; a re-enable before a batch reaches a child, while `granted_revision` still equals the child's frozen revision, leaves that child runnable, and it is claimed; a re-grant at another revision does not: the claim keeps skipping the child and a batch cancels it `profile_grant_revoked` |
| TUR-026c Repeat guard | S | Lease loss after an allowlisted mutation: the retried attempt's identical call is blocked; different arguments are allowed; attended runs are unaffected; the lookup's query plan uses `idx_tool_calls_run` |
| TUR-026d Allowlist ceiling | S | End to end with a real worker: an automation whose allowlist lacks `files.read` is still offered it, calls it, and the beacon fails the run with `automation_tool_not_allowlisted` and an `automation.tool.blocked` audit row, with nothing read; a third-party tool marked `safe` and not listed is blocked the same way; a listed safe tool runs without approval; an automation's model that calls `memory.search` gets an `automation_tool_unsupported` refusal, nothing is read from the vault, no `automation.tool.blocked` row is written, and the run continues and completes; a forged `team.delegate` beacon from an automation run gets the same refusal and the run continues; with a GitHub connection, an automation's model that calls `github.list_issues`, and one that calls `github.create_comment`, each fails the run with `automation_tool_not_allowlisted` and an `automation.tool.blocked` audit row, with no request sent to GitHub; `skills_list` and `skill_view` still work in an automation with no skills tools listed; an authenticated direct `CallRegisteredMcpTool` from the `runtime` identity for an active scheduled automation's run (no enforced selection), with no BEFORE beacon sent: (1) for an unallowlisted `safe` tool on a `local_container` server, it returns `PermissionDenied`, reaches no server (the fake server records no request), fails the run under the automation-policy origin with `execution_active` still 1 and the execution not marked exited until the worker acknowledges, writes one `automation.tool.blocked` audit row with no arguments, and a session subscribed before the call receives `agent.run.failed` without resubscribing; (2) a second refused call on the now-failed run writes no second audit row, publishes nothing and changes nothing; (3) *revoked before the call starts* (section 4, point 5): for an approval-required tool whose approval was granted and whose automation was then narrowed, and in a second case disabled, before the call, it is refused at entry, the approval is never consumed and ends `expired` with its token cleared by the run's failure, no server is reached, and the run fails with the audit row; (4) for an unallowlisted tool on a `remote_url` server, it returns `PermissionDenied`, sends no request (the fake remote endpoint records none) and fails the run with one audit row; (5) *revoked during the call* (section 4, point 5): for an approval-required tool, with a barrier after the entry check passes and before the call's approval is consumed, disabling the automation, and in a second case narrowing its allowlist, it is refused at dispatch with `PermissionDenied` naming `automation_grant_revoked`, the approval is consumed (spent), nothing is sent to the server, and exactly one audit row is written; a grant failure (`automation_approval_failed`) fails the run and writes no `automation.tool.blocked` row; an audit insert forced to fail inside `FailUnattendedRun` rolls the failure back with it, and nothing is published; with no refusal hook wired, an unattended call is still refused and reaches no server; a replayed refusing beacon on a failed run writes no second row either; the same direct call from an attended run, and for an allowlisted tool on an enabled automation, still reaches the server; the fired job's payload is byte-identical to today's (no selected set), so its routing and prompt size are unchanged; attended runs are unaffected |

### Open questions

1. Should deleting an automation also cancel its in-flight run? This design
   keeps the run and its record (`orch/repository/automations.go:368-370`),
   as DeepSeek keeps its Ticket, but retracts its consent: the run continues
   until its next tool call, which fails it with `automation_grant_revoked`.
   Cancelling at once instead is one call to the existing cancel path.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| A new grant entity | mai: `mandates` table (MND-001); sol: `work_grants` for the proactive path; grok, opus: none | None; reuse frozen grants | Turing already freezes every grant a Mandate would hold; a second record duplicates authority and can drift. |
| What to revalidate | grok: a channel wake in CHN-002; sol: every new run of a proactive lineage; opus: existing re-entry points | Existing re-entry points now; PRO-001 runs inherit them | Verified gaps exist today; a future channel wake is a new run that freezes today's grant anyway. |
| Policy change on a running run | dsh: notify; sol: fence future dispatches; opus: narrow at re-entry | Narrow at re-entry; no cancel | Cancelling surprises the owner; narrowing at the next mutation removes the risk without it. |
| Grant lifetime | sol: 24-hour ceiling | Not adopted | Turing runs are already bounded by the queue wait, approval timeout and lease; there is no long-lived grant to expire. |
| Round 1 review: where the grant is checked | sol: a check before preview I/O leaves a revocation race | Inside the approval-commit transaction; the beacon keeps an early check | Only a check in the transaction that approves can be atomic with the approval. |
| Round 1 review: profile authority | opus: the SQL check misses a deleted or edited profile file | SQL at claim, plus the team resolver after claim and before consent | The file is authority; the resolver is the one `CallTeamTool` already trusts, and reading files inside the claim transaction would hold the database during I/O. |
| Round 1 review: repeat-guard lookup | opus: `approvals` has no server column or run index | `tool_calls` by `run_id`, joined to `approvals` by `tool_call_id` | Both indexes exist; no migration is needed. |
| Round 1 review: allowlist does not bind safe tools | grok, sol: safe tools bypass the grant (found for PRO-001d; true of today's automations) | New TUR-026d: unattended check before the policy, plus an enforced selection at fire | It is a shipped gap, so it belongs with TUR-026, not only with `auto`. |
| Round 1 review: allowlist wording | opus: the allowlist is `automation_allowed_tools`, not the `automations` row | Corrected | Accuracy. |
| Round 2 review: enforced selection for every automation (tur026d-enforced-selection-breaks-existing-automations) | opus: persisting the allowlist as an enforced selection changes routing, skills and the context budget of every existing automation | No selected set for scheduled automations; the beacon check is the boundary (since round 35, with the same check at registered-MCP dispatch); the two skill loaders are exempt | The beacon check alone closes the gap, server-side; the selection was a second line that changed three other behaviours. Skill loaders read only the job's own pinned, owner-granted snapshot. |
| Round 2 review: failure path (AUTOMATION_SELECTION_FAILURE_PATH) | sol: with an enforced set the worker answers an unselected call itself, before any beacon, so the promised audit row and attention row never happened | Scheduled automations reach the beacon and fail visibly; event-started runs, which keep an enforced set, are refused as unknown tools and fail only if the run itself fails (section 7) | Each path now states what the code actually does, and UX-002's row follows real failures. |
| Round 2 review: revocation and reads (pro001d-revocation-skips-read-tools) | grok: revoking an automation or leaving `auto` did not stop the safe reads the run could still make | `unattendedGrantCheck` re-reads the automation, its allowlist and the watch at every unattended tool call | Revocation must stop reads of third-party-steered runs too; it costs two or three primary-key reads per unattended call. |
| Round 2 review: deletion semantics (tur026a-reverses-documented-delete-semantics) | opus: gap (a) silently reversed the schema's documented "does not retract consent" | Recorded as a deliberate reversal; the run and its record are kept, the consent is not | The owner deleting an automation means "stop acting on my behalf"; a run that keeps pre-approving mutations afterwards is the gap. |
| Round 3 review: memory tools failed existing automations (tur026d-memory-tools-fail-existing-automations) | opus: memory tools are `safe` and offered to automation runs, can never be allowlisted, and the ceiling turned today's non-fatal memory refusal into a failed run the owner could not fix | Tools on `memory`, `team` and `integrations` are denied through `denyToolBefore` without failing the run (narrowed in round 4 to `memory` and `team`; see below); the upgrade note covers only servers an automation can allowlist | It keeps today's behaviour and the ceiling: nothing on those servers runs, and an owner is never told to add a tool the save path refuses. Not offering them to unattended runs at all would need a per-run filter in three discovery paths for the same outcome. |
| Round 3 review: TUR-026d asserted a UX-002 row (tur026d-acceptance-depends-on-ux002) | opus: TUR-026 ships first, before UX-002 exists | TUR-026d asserts the failed run and the audit row; the attention row is asserted in UX-002a | Each slice's tests can be written when it lands. |
| Round 3 review: revoked watch scope was not re-checked (WATCH_SCOPE_REVOCATION_NOT_REVALIDATED) | sol: the check re-read the watch's enable switch and mode only, so removing an actor or a repository, or narrowing a predicate, left queued, pending and running work eligible | `unattendedGrantCheck`, the pending-fire hook and `FireEventAutomation` re-evaluate `watchEventEligible` over the originating event's stored metadata; watch-side failures use `watch_grant_revoked` | One pure function, shared with the matcher, makes "intersected with current policy" true for scope as well as mode; DeepSeek's `revalidate` checks source scope and sender the same way (`dsh:src/policy.ts:213-241`). |
| Round 4 review: which refusals fail the run (tur026-every-failure-fails-run, tur026d-integration-refusal-changes-today-failure) | grok: point 2 sent every failure through `blockUnattendedTool`, contradicting point 5's non-fatal refusal; opus: making integration calls non-fatal reversed today's visible failure, audit row and attention row for an injection-steered comment attempt | Point 2 names the four codes that fail the run; the non-fatal `automation_tool_unsupported` refusal runs before the allowlist check and covers only `memory` and `team`, which are refused non-fatally today; integration tools keep failing the run with `automation_tool_not_allowlisted` | It keeps today's behaviour exactly where it was claimed, and the riskiest unattended call, a GitHub write, stays visible. |
| Round 4 review: a narrowed predicate raised an attention row (ux002-predicate-narrowing-raises-row) | opus: a predicate mismatch was coded `automation_grant_revoked`, so narrowing a rule's predicate raised a `person/blocked` row, although later non-matching events simply do not fire | Every `watchEventEligible` failure, predicate included, is `watch_grant_revoked`; `automation_grant_revoked` is the automation gone, disabled or its allowlist narrowed | Only an allowlist narrowing on a still-enabled automation keeps failing its runs, so only it needs the owner (section 8, stage 3). |
| Round 10 review: an unbounded revocation transaction (TUR026_PROFILE_REVOCATION_UNBOUNDED_TRANSACTION) | sol: `SetAgentProfileEnabled` and `GrantAgentProfile` cancelled every affected queued and consent-held child through `cancelRunTx` inside their settings transaction, so its work grew with the profile's backlog on the one database connection | The settings transaction writes only the setting, which is the claim fence; the job-reaper tick (superseded in round 53: the maintenance tick, section 27) cancels disqualified children in batches of at most 100, each re-checking the setting, crash-resumable with no marker; a re-enable before a batch leaves the child runnable if it qualifies at claim | The same fence-then-batch shape as TUR-028's drain; the claim fence makes the guarantee atomic, so the cancellations can be small. |
| Round 11 review: a re-grant does not requalify a child (tur026-regrant-does-not-requalify) | grok: point 3 and TUR-026b said a re-grant before a batch left a child runnable, but `GrantAgentProfile` writes only `granted_revision` (`orch/repository/team.go:78-101`) and the fence requires it to equal the child's frozen revision | Only a re-enable restores eligibility, and only while `granted_revision` still equals the child's frozen revision; a re-grant at another revision stays skipped and is cancelled by the batch; TUR-026b tests both | The text now says what the fence does; the fence itself was right. |
| Round 30 review: an event's author revalidated by login (PRO001_ACTOR_LOGIN_REASSIGNMENT) | sol: see section 3's decision log | Point 2's revalidation reads the author's id against the listed actors and bots (section 3, point 3) | A reclaimed login must not keep, or gain, a run's eligibility. |
| Round 35 review: registered-MCP dispatch skipped the grant (TUR026_SAFE_MCP_DISPATCH_BYPASSES_GRANT) | sol: the `runtime` identity may call `CallRegisteredMcpTool` directly, and `CallTool` lets a `safe` `local_container` tool run for a job with no enforced set, checking only policy and liveness, so a runtime that skipped the beacon could call an unallowlisted or revoked tool | Superseded in rounds 37 and 38 on where the check runs (see those rows). Then: `CallTool` calls `unattendedGrantCheck` immediately before `MCPDispatchActive`; memory, team and integrations keep their existing refusals, which already cover a direct call; bundled `files` and `system` reads stay on the beacon, stated as trusted (point 5); TUR-026d adds direct-RPC tests | The beacon is a cooperating worker's report, not a gate on the facets the runtime calls itself; one more primary-key read per registered-MCP call closes the only facet that ran a safe tool unchecked. |
| Round 36 review: the dispatch refusal's visibility (tur026-dispatch-refusal-contradicts-fails-visibly) | opus: the round-35 dispatch refusal did not fail the run or write an audit row, while point 2 and section 27 say every such refusal fails visibly; a beacon-less call left no trace | Superseded in round 37 (see that row). Then: the dispatch refusal records the `automation.tool.blocked` audit row and fails the run through `FailRunCanonical` (what `failUnattendedRun` calls with only the run id); a repeat on an already-failed run is the tolerated no-op | One rule for every non-memory, non-team refusal: it fails the run visibly; the abuse case the round-35 check exists for is now visible too. |
| Round 37 review: the dispatch refusal's order and failure contract (tur026-direct-mcp-check-too-late-for-approval-consumption, tur026-dispatch-fail-drops-cited-path, TUR026_DISPATCH_FAILURE_CANONICAL_CONTRACT, tur026-dispatch-refusal-run-failed-event-not-published, tur026d-repeat-dispatch-refusal-writes-second-audit-row) | mai: the check ran after `CallTool` consumed an approval (`call.go:86-112`), so a revoked call could spend one; grok, sol: `FailRunCanonical` with only the run id is refused, since it requires an expected state version, and the round-36 text dropped `PreserveExecution` and the two tolerated errors that `failUnattendedRun` uses; opus: `mcpregistry` holds no event bus, so a subscribed session never received `agent.run.failed`, and an unconditional audit insert wrote a second row on a repeat refusal | Superseded in round 38 where it states the failure sequence and the second check's placement (see that row). Then: `CallTool` runs `unattendedGrantCheck` before the selection, egress, policy and approval steps and again immediately before `MCPDispatchActive`; a refusal calls one runtime-owned hook, wired in `app.New` like the approval enforcer, implemented by `refuseUnattendedCall`, the helper extracted from `blockUnattendedTool` and `failUnattendedRun`: read the state version, `FailRunCanonical` with it, the normalized failure and `PreserveExecution: true`, treat `ErrRunNotFailable` and `ErrRunTransitionConflict` as no-ops, publish the returned events, and write the audit row with `RecordForExistingRun` only when the run moved to failed; TUR-026d tests the unconsumed approval, the preserved execution, the live `agent.run.failed`, and no second row | Reuse the beacon path's exact sequence rather than restate it; one helper keeps the two refusals from drifting, and the runtime stays the only publisher of run events. |
| Round 38 review: the dispatch refusal's transaction, order and approval window (tur026-refuse-helper-audits-grant-failure, tur026-refuse-helper-audits-approval-failed-path, tur026-remote-mcp-check-order-contradiction, TUR026_MCP_GRANT_FENCE_NOT_AT_DISPATCH, tur026d-linearization-recheck-untested, tur026-security-summary-omits-approval-window, tur026-security-overclaims-no-approval-spent, tur026-refusal-audit-not-atomic-with-failure) | grok, opus: a helper extracted from `failUnattendedRun` would add an `automation.tool.blocked` row to the `automation_approval_failed` path (`orch/service/runtime/service.go:3002-3007`); grok: a remote-URL server was still said to be refused before the check; sol: the second check ran before, not inside, `MCPDispatchActive`, so a revocation between them was missed; opus: no test revoked after the first check passed, and the audit row written after the failure's commit was lost on a crash or a version conflict (`orch/repository/run_state.go:103`); grok, opus: the Security paragraph and TUR-026's acceptance said a refused call spends no approval, though a revocation after the entry check and before consumption leaves it spent | One repository call, `FailUnattendedRun`, runs `failRunTx` with the version resolved in its transaction and `PreserveExecution: true` and, only when the transition applied and an audit entry is given, `recordAuditTx` in the same transaction; `failUnattendedRun` passes no entry, so the grant-failure path writes no row, while `blockUnattendedTool` and the refusal hook pass one; the entry check runs for every tier; the dispatch check and the liveness read share one read transaction; Security and TUR-026 state the approval window (superseded in round 42: section 4, point 5 states it once, in two cases; see that row); TUR-026d adds the remote-URL, barrier, grant-failure and rollback tests | Existing transaction helpers put the failure and its trace in one commit with no version read to race, and the single-connection database makes the dispatch check atomic with the fence at no new cost. |
| Round 39 review: the attended beacon's cost (tur026-beacon-attended-cost-understated) | opus: moving the unattended check ahead of the policy branch adds an `automation_runs` read to every BEFORE beacon, attended included, while Performance said attended runs pay nothing | Performance and the Summary row state it: one primary-key read of `automation_runs` per beacon (a miss for an attended run) plus one per registered-MCP call | One cache-hot primary-key read per tool call is negligible beside a model step, but the top-priority performance claims must be exact. |
| Round 42 review: whether a refused call's approval is spent (tur026-approval-dispatch-consumption-ambiguity) | mai: point 5 said a revoked call left the approval spent, while TUR-026d said both unconsumed and consumed | Point 5 states it once, in two cases: an automation already narrowed or disabled when the call starts is refused at entry and its approval stays approved and unconsumed (corrected in round 47: never consumed, and expired by the run's failure; see that row); a revocation committed after the entry check, while the approval is handled, is refused at dispatch with the approval consumed (spent) and nothing sent. Security and privacy points there, and TUR-026d's cases (3) and (5) test exactly these two | Both outcomes are real, and they depend only on when the revocation commits relative to the approval step (`orch/service/mcpregistry/call.go:92-110,125-136`). |
| Round 47 review: the entry refusal's approval state (TUR026_ENTRY_REFUSAL_APPROVAL_TERMINALIZATION) | sol: `FailUnattendedRun` reuses `failRunTx`, whose approval cleanup expires every pending or approved approval of the run and clears its token (`orch/repository/runs.go:255-260`), so 'stays approved and unconsumed' could not hold | Point 5 and TUR-026d case (3): the approval is never consumed and ends `expired` with its token cleared by the run's failure | Expiring is the existing terminal cleanup and is stricter than leaving it approved; what matters is that the refused call never spends it and reaches no server. |
| Round 54 review: TUR-026b's batches and the maintenance tick (tur026b-batches-still-attributed-to-reaper, MAINTENANCE_TICK_STALE_REAPER_IN_ACCEPTANCE) | grok, opus: TUR-026b's acceptance and round 10's row still put the cancellation batches on the job reaper, TUR-026 ships before any slice that would create the maintenance goroutine, and nothing tested the zero-reaper case | TUR-026b adds the maintenance goroutine (section 27), its test names a maintenance transaction and repeats with `TURING_JOB_REAPER_INTERVAL_MS=0`, and round 10's row is marked superseded | With the reaper off, queue-wait expiry is also off, so revoked children would otherwise sit queued forever. |
| Round 60 review: policy failure codes through normalization (TUR026_POLICY_FAILURE_NORMALIZATION) | sol: the shipped allowlist code is `automation_tool_not_allowlisted` (`orch/service/runtime/service.go:89`, `orch/runoutcome/outcome.go:437`), not the spelling this design used, and TUR-026's new codes were in no failure mapping, so `NormalizeFailure` would store them as `unknown` and lose the `watch_grant_revoked` discriminator UX-002 relies on | The design uses the shipped spelling, and section 27 states the outcome-code rule once: `automation_grant_revoked`, `watch_grant_revoked` and `unattended_repeat_blocked` map to `ReasonPolicyDenied` in `failureReasons` and `approvedFailureMappings`, added by the slice that first writes each, whose tests assert the stored code and reason | An outcome normalized to `unknown` cannot drive the attention rules that key on it. |

## 5. F3 — Proactive rule library

### Verdict

Adapt. A rule is an owner-authored **event automation**: an existing
automation row with an event trigger, a deterministic predicate, and the same
exact allowlist. Deterministic rules ship first and spend no model call.
DeepSeek's exhaustive judgement is adopted as an **optional** local assessor,
for rules whose bar a predicate cannot express; it stays `observe`-only until
EVAL-001 evidence admits it to `auto`.

### DeepSeek source

- Rules are `{id, name, enabled, instruction}` (`dsh:src/policy.ts:67-68`);
  two coding defaults, `copy` and `visual`, ship enabled
  (`dsh:src/policy.ts:62-65`).
- The assessment must return exactly one judgement per enabled rule
  (`dsh:src/policy.ts:42-46`; `dsh:prompts/admission.md:28-30`). An accept also
  needs a settled direction (`dsh:src/policy.ts:18-28`), at least one match and
  `intent_evidence` (`dsh:src/policy.ts:52-53`;
  `dsh:docs/002-authorization.md:101`).
- A matched rule's process and communication override the defaults for that
  Ticket only (`dsh:docs/002-authorization.md:103-105`; precedence
  `dsh:docs/004-prompts.md:65-71`).
- Rollout is observe first (`dsh:docs/002-authorization.md:101`).
- A shortcut lets the router ignore an inferred candidate from its card without
  judging every rule (`dsh:docs/002-authorization.md:87`).

### Turing today

- Automations are owner-authored prompt, exact allowlist and enable switch,
  with save-time refusals of integration, memory and team tools
  (`orch/repository/automations.go:36-47`).
- The scheduler claims any enabled automation whose `next_due_at` has come due
  (`orch/repository/automations.go:454-459`), and `SetAutomationEnabled` arms
  `next_due_at` from the schedule (`orch/repository/automations.go:323-365`).
  `schedule_kind` is free text validated in Go
  (`orch/db/schema/0009_automations.sql:12-14`;
  `orch/repository/automations.go:940-955`). The fire itself (enqueue,
  `automation_runs`, the automation's one session) is `ClaimDueAutomation`'s
  transaction (`orch/repository/automations.go:675-742,744-774`); the
  scheduler's `Dispatcher` only wakes workers
  (`orch/service/automations/scheduler.go:17-21`).
- Untrusted data is framed and cut (the delegation join's
  `DELEGATION_RESULTS`), and the specialist-job contract makes an empty
  selected set mean no tools, implies no persona and skips recall
  (`proto/turing/v1/runtime.proto:98-104`).
- Sessions carry a `kind`, and every public session mutation refuses a session
  that is not a chat (`orch/db/schema/0024_delegation_groundwork.sql:8-9`;
  `orch/repository/sessions.go:502-514`). The kind is a `CHECK (kind IN
  ('chat', 'delegation'))`, which SQLite cannot widen in place; the one
  precedent for rebuilding a parent table is the hooked migration that
  rebuilt `agent_runs` with foreign keys off on a pinned connection
  (`orch/db/run_outcomes_migration.go:144-155`).
- `AgentJob` has no per-job output limit (`proto/turing/v1/runtime.proto:52-104`).
  The runtime asks every completion for the provider's process-wide
  `MaxOutputTokens` (`agent-runtime-go/internal/agent/context_budget.go:163,175`;
  `TURING_LOCAL_MAX_OUTPUT_TOKENS`, `agent-runtime-go/internal/config/config.go:263`),
  although the provider already honours a smaller per-request value
  (`agent-runtime-go/internal/llm/ollama.go:264`;
  `agent-runtime-go/internal/llm/openai_compatible.go:373`). Its admission estimate
  counts one serialized request byte as one token
  (`agent-runtime-go/internal/llm/provider.go:108-110`).

### Unified design

1. **Rule = event automation.** An automation with `schedule_kind = 'event'`
   and two new columns, `watch_id` and `predicate_json`; no separate trigger
   column. An event automation has no schedule: `scheduleFromColumns` accepts
   the kind, `SetAutomationEnabled` and `UpdateAutomation` leave its
   `next_due_at` NULL, and `ClaimDueAutomation` also filters
   `schedule_kind != 'event'`, so the scheduler never fires one. Its fire is
   `FireEventAutomation`, which shares `ClaimDueAutomation`'s tail (enqueue and
   `automation_runs`) but runs inside its caller's transaction, the
   poller's page transaction or the pending-fire hook's terminal
   transition, and takes the thread's current session (section 6, point 1);
   its caller wakes workers after the commit through the same dispatcher. The predicate
   is deterministic over event metadata: event kinds, repositories, labels and
   actors, each actor bound by its numeric account id when the rule is
   saved (section 3, point 3, Listed actors). A wildcard predicate is refused at save, as a wildcard tool
   is. A label term can narrow a match but never makes one unattended:
   anyone with write access can apply a label, and nothing attests who did
   (section 3, point 2), so a match by an automation whose predicate names a
   label records an `observe` proposal, even on an `auto` watch
   (`mayFireUnattended`, section 4, point 2). The automation's prompt is the instruction; its allowlist is the only
   tool authority. No rule ships enabled, and DeepSeek's coding defaults are
   not copied. A watch holds at most 20 event automations (refused at save),
   and a partial index, `automations_event ON automations (watch_id) WHERE
   schedule_kind = 'event' AND enabled = 1`, serves the matcher, so each
   admitted event reads at most 20 candidate rows by one index range.
2. **Deterministic matching and a two-step Start (PRO-001b).** After the gate
   admits an event, Go evaluates every enabled event automation on that watch.
   In `observe`, a match records or updates the thread's `would_start`
   proposal with the matched automation ids. **Start** opens a preview: the
   excerpt rendered as plain text, every matched automation with its
   allowlisted tools, and the local route. When more than one matched, the
   owner picks one, and Confirm names it; the server refuses a Confirm whose
   automation is not among the proposal's matched ids, is disabled, or no
   longer matches the event (`watchEventEligible` over the current rows,
   section 4, point 2). **Unconfined MCP tools, stated once:** Start and
   Confirm refuse an automation whose allowlist names any tool that is not
   confined. Two kinds pass. The bundled servers' tools: the registry
   reports a server `SandboxConfined` exactly when its tier is `bundled`
   (`orch/service/mcpregistry/service.go:1344`;
   `orch/repository/mcp_servers.go:18-20`). And the runtime-local `skills`
   pseudo-server's `skills_list` and `skill_view`: it has no `mcp_servers`
   row (`orch/db/schema/0016_mcp_registry.sql:48,60`), the worker serves it
   itself from the job's own skill snapshot
   (`agent-runtime-go/internal/agent/general_assistant.go:928,1049-1051`),
   so it has no network path, and a thread session's snapshot is empty
   (section 3, point 7), so it lists and loads no skill there. A `remote_url` and a
   third-party `local_container` server's tools are refused alike,
   classified by the server's current tier at Start and again at Confirm.
   A local endpoint is not confined execution: a third-party container is
   the owner's registration, Turing does not confine what it reaches, and
   its calls need no egress decision, which the call path asks only of a
   `remote_url` server (`orch/service/mcpregistry/call.go:64-78`). Start
   shows that automation as not startable with the content-free reason
   `unconfined_mcp_tool` on the proposal, and a Confirm naming it is
   refused with that reason and enqueues nothing. The save-time refusals
   cover only integration, memory and team tools
   (`orch/repository/automations.go:182-203`), so this check is the one
   that applies. A tool that passes at Confirm stays confined: a bundled
   server cannot be deleted or re-registered, and no other server can take
   its name or `skills`, which are reserved
   (`orch/service/mcpregistry/service.go:589-590,758-759`;
   `orch/service/mcpregistry/import.go:46-53`). So nothing a
   Start-confirmed turn does leaves the machine. The run's tools are the chosen automation's alone:
   allowlists are never combined. Only **Confirm** enqueues, and it closes the
   proposal, so a second Confirm is refused. Confirm names the
   `excerpt_revision` the preview showed; if an edit has replaced the excerpt
   since (section 3, point 2), Confirm is refused and the preview reopens with
   the current text, so the owner always confirms the exact text the run
   will read. **Dismiss** closes the proposal and keeps the thread quiet until
   the owner watches it again (section 19). The confirmed turn is
   attended (the owner clicked) and runs in the thread's current session,
   or a new one when it has none (section 6, point 1), with narrower defaults than a chat turn: the local route only (refused, not
   rerouted, when no local worker can serve it); `skip_automatic_recall`; the
   pinned persona and profile withheld and an empty skill snapshot (F1,
   point 7); no
   team roster; the chosen automation's allowlist as an enforced selected
   set, with `minimumTeamProtocolVersion: 1`. A mutation among those tools
   still asks the owner, because the run has no `automation_runs` row. These
   limits are sticky: every turn in a thread session keeps them, including
   every later event-started turn and every message the owner sends there
   (with no tools), until the owner chooses Treat as normal conversation,
   which detaches the session from the thread (F1, point 7).
3. **Optional assessor (PRO-001c).** A rule may add `assess_instruction` (at
   most 500 bytes; at most 8 such rules per watch). When an admitted batch
   matches the predicate of one or more such rules, the orchestrator enqueues
   one assessment run:
   - in a hidden session of a new `watch` kind. PRO-001c's migration rebuilds
     `sessions` to widen the kind check, through the hooked-migration path
     above. Every chat-only guard then refuses a watch session as it refuses
     a delegation session, public `DeleteSession` included. Its withdrawal
     therefore needs its own begin: `BeginWatchSessionDeletion`, the shared
     `beginSessionDeletion` with `wantKind = "watch"`, as
     `BeginDelegationSessionDeletion` is for a child
     (`orch/repository/session_delete.go:154-163,183-197`), driven by the
     PRO-001 sweep. The same migration adds `session_deletions.session_kind`,
     written at begin, because today a receipt whose row is gone is read as
     `chat` or `delegation` from its parent column alone
     (`orch/repository/session_delete.go:185-188`).
     `ResumePendingDeletions` (`orch/service/sessions/service.go:133-160`)
     sends a `watch` receipt to the watch withdrawal instead of the chat one;
     `session_kind` only routes. `BeginWatchSessionDeletion` writes
     `session_deletions.initiator = 'sweep'` (PRO-001w1), so
     `PendingSessionDeletionReceipts`
     (`orch/repository/session_delete.go:984-989`) and UX-002's
     `person/deletion` retry row leave it out by the one `initiator`
     filter, because the owner cannot act on one: the reconciler and the
     sweep retry it;
   - as a specialist-contract job: a host-authored assessor profile snapshot
     (so no persona, no automatic recall), local route only (refused at
     enqueue otherwise). **The assessor payload, stated once:**
     `enforceSelectedTools: true` with an empty `selectedTools`,
     `skipAutomaticRecall`, an empty skill snapshot, no memory,
     `max_output_tokens: 768`, `singleCompletion: true` and
     `minimumTeamProtocolVersion: 2` (below), and no `teamRoster` and no
     `teamContinuation`; its job row is written
     with `unattended = 1` (section 9, point 2). The empty enforced set
     refuses every tool at each gate before any server row, vault file or
     profile file is read:
     - the beacon, before the policy
       (`orch/service/runtime/service.go:2925-2935`);
     - registered-MCP dispatch. Today `CallTool` reads the server row,
       `sealed_token` included (`orch/repository/mcp_servers.go:633-639,1488-1496`),
       before its selection check (`orch/service/mcpregistry/call.go:36-57`).
       PRO-001c moves the `RunToolSelection` read ahead of `GetMCPServer`:
       an enforced empty set allows no qualified name, so the call returns
       "MCP tool is not selected for this run" before the row is read. A
       non-empty set is still checked by its qualified name after the row
       read, where it is today. The beacon also checks the selection first;
     - the memory service, right after `authorizeRun` and before the vault
       (`orch/service/memory/call.go:71-86`);
     - the team facet. Today `CallTeamTool` decides from the run's frozen
       roster alone (`orch/service/team/call.go:154-166`;
       `orch/repository/team_roster.go:32-35`), and the `runtime` identity
       may call it directly (`orch/app/app.go:353`). PRO-001c adds the
       memory service's gate there: after the assignment check and the
       replay lookup (`team/call.go:61-77`) and before `admitDelegation`
       reads the roster (`team/call.go:79,159`), `CallTeamTool` reads
       `RunToolSelection` and refuses with `PermissionDenied` a run whose
       selection does not allow `team/team.delegate` (`ToolSelection.Allows`,
       `orch/repository/tool_selection.go:19-23`). No run that carries a
       roster today enforces its set (only delegation children and
       continuations do, `orch/repository/delegations.go:272`,
       `orch/repository/delegation_join.go:167`, and neither carries one),
       so the gate changes nothing for today's runs.

     So an assessor run can neither call a tool nor delegate: the absent
     roster and the empty enforced set each refuse delegation, while its
     unattended status alone does not;
   - with a per-job output cap: a new `AgentJob.max_output_tokens` (field 27,
     768 for the assessor). It only narrows: the runtime uses the smaller of
     the job's value and the provider's. And with one completion: a new
     `AgentJob.single_completion` (field 28, set only on assessor jobs),
     copied from the payload's `singleCompletion` through
     `queuedJobPayload` and `mapJob` as `enforce_selected_tools` is
     (`orch/repository/jobs.go:212-228`,
     `orch/service/runtime/service.go:3575-3614`), under which the runtime
     makes exactly one completion (Performance, below); it is never
     inferred from an empty selected set, so a delegation child with one
     keeps today's recovery. Team protocol version 2 means a worker honours
     both fields, so assessor jobs carry
     `minimumTeamProtocolVersion: 2` and an older worker never claims one;
   - with a bounded input: host instructions (at most 1 KiB), the
     `WATCH_EVENT` frame cut to 6 KiB for the assessor (a visible cut; the
     stored excerpt keeps 16 KiB), measured on its serialized, JSON-escaped
     length, because the runtime's estimate is the length of the marshalled
     request (`agent-runtime-go/internal/llm/ollama.go:255-260`) and Go
     escapes `<`, `>` and `&` to six bytes each: the excerpt is cut on a
     UTF-8 boundary until its escaped form fits, keeping the cut marker, so
     event text cannot push the input past the bound. One line per
     assess-rule follows. The
     orchestrator computes the serialized size at save and at enqueue: at most
     12 KiB, which the runtime's estimate counts as at most 12,288 tokens and
     which, with the 768-token output, fits the default 32,768-token window. A
     rule set that cannot fit is refused at save; a configured window too small
     makes the runtime refuse the job, recorded as `unavailable`;
   - output is JSON validated in Go: exactly one `{rule_id, matched, reason}`
     per assess-rule, no unknown, disabled or repeated ids; a match needs a
     non-empty `evidence` string that is a verbatim substring of the supplied
     excerpt; ambiguity or conflict means no match. Anything malformed records
     `unavailable` after at most one transport retry. The proposal stores the
     evidence as byte offsets into the excerpt, so purging the excerpt purges
     it;
   - once the result is recorded, the session is withdrawn (F1, point 8);
   - within hub caps that have their own state: a content-free
     `watch_assessments` row (id, watch, thread, run id, the
     `(event id, excerpt_revision)` pairs its input was built from, its
     `rules_revision`, `enqueued_at`, `finished_at`, outcome code) is written in the enqueue transaction, which
     first counts rows with `finished_at IS NULL` (a partial index) and rows
     with `enqueued_at` in the last hour (an index on `enqueued_at`), leaving
     out rows finished `assessment_preempted`, because a preemption is the
     owner's turn taking the slot, not assessor work. With one
     in flight, or 20 in the hour, nothing is enqueued and the batch stays
     coalesced for the next sweep tick. The row outlives the withdrawn session,
     which is why the caps cannot count runs, and the sweep deletes rows after
     30 days;
   - preempted by the owner, only when it is in the owner's way: when an
     attended job is enqueued while an assessment is executing, every
     execution slot (`TURING_MAX_CONCURRENT_RUNS_GENERAL`) is taken, and the
     claim's attended probe (TUR-027), run without its capacity check, would
     claim the new job, the orchestrator cancels the assessment through
     `cancelRunTx` with a new abandoned code, `assessment_preempted` (not
     `user_cancelled`), and sends its worker `RunCancelled`, as it does for a
     stopped delegated task. `assessment_preempted` joins `user_cancelled`
     and `parent_run_ended` as a committed stop: the `committedStop`
     predicate (`orch/repository/assignments.go:149-150`), which keeps a
     cancelled run contained against a heartbeat or a disconnect until its
     worker acknowledges the exit or its lease is recovered, and the
     recovery loop's `RunCancelled` redelivery
     (`orch/service/runtime/service.go:1512-1522`) both gain the code, so
     the slot frees only once the assessment has actually stopped, and the
     attended job is claimed then. The batch stays coalesced, its `watch_assessments`
     row is finished with that code, its session is withdrawn, and the batch
     is enqueued again once nothing attended is claimable. A free slot, or an
     attended job held by its session's turn order or a consent hold, leaves
     the assessment running. The check is one probe of the partial index
     above, a count of active runs and that one attended probe, after the
     attended enqueue commits.

   **The rules revision, stated once.** The *assessed rules* of a batch are
   the watch's enabled event automations with an `assess_instruction` whose
   predicate matches it, the rules its input lists. `rules_revision` is the
   SHA-256 over their `(id, assess_instruction, predicate_json)` in id
   order, frozen on the `watch_assessments` row at enqueue. The claim and
   the result commit below each recompute it over the current rows, by the
   same selection, and treat any difference as stale, so a result is never
   recorded against instructions or predicates other than the ones the
   model judged, even when the owner edits a rule in place under the same
   id. A hash keeps the row free of rule text.

   An input is checked before any model reads it. A queued assessor job
   carries the excerpts it was built from, and it can wait behind attended
   work, so the claim transaction that would assign it re-reads the watch,
   the thread's `dismissed_at` and `dismiss_generation` and each frozen
   event's row, and assigns it only when the watch is present and enabled,
   no frozen event is dismissed (section 19, point 1), every frozen excerpt is still held at its frozen
   `excerpt_revision`, and the recomputed `rules_revision` equals the
   frozen one. Otherwise the job is never dispatched: the claim
   finishes its run through `cancelRunTx` with the abandoned code
   `assessment_stale` and no model call, finishes its `watch_assessments`
   row with that code, and moves on; the sweep withdraws its session
   through `BeginWatchSessionDeletion`, as after a result; and the batch
   stays coalesced for a fresh assessment only while it is still eligible:
   the watch is present and enabled, the thread is not dismissed, one of
   its undismissed events' excerpts is still held and it still has an
   assessed rule; a batch never holds a dismissed event. So an edit
   that removes text, or a Dismiss, while the job waits keeps that text
   from every model request, and a rule edited while it waits is assessed
   in its new form. The check is one read of the watch and thread rows, one
   per frozen event and one range read of the watch's enabled event
   automations, on assessor claims only.

   A result is bound to the text it assessed and to the watch's current
   state. The result commit re-reads the watch, the thread's `dismissed_at`
   and `dismiss_generation` and each frozen event's row, and records
   assessor-derived fields only when the watch is present and enabled (a
   watch being deleted is disabled, section 3, point 8), no frozen event is
   dismissed (section 19, point 1), every frozen
   excerpt is still held at its frozen `excerpt_revision`, and the
   recomputed `rules_revision` equals the frozen one. When an
   edit has replaced an excerpt since (section 3, point 2), or the assessed
   rules changed, it records no reason or offsets, finishes the row
   `assessment_stale`, and leaves the batch coalesced, while still eligible
   (above), for a fresh assessment of the current text and rules on a later
   tick, within the caps above. When the watch was disabled or deleted, the thread
   was dismissed or an excerpt was cleared (expired, dismissed or its watch
   deleted), it records the outcome code only and never opens or reopens a
   proposal. Otherwise a positive result records the same `would_start`
   proposal in either mode. A rule with `assess_instruction` never fires
   unattended in this design, on an `auto` watch too: neither its
   deterministic match nor a positive assessment fires it, and Start is the
   owner's act (section 7, Which matches may fire). No
   assessment is enqueued while the owner's quiet switch is on (TUR-028,
   section 18); a batch held by it stays coalesced, as a preempted one does,
   and is assessed after the switch is turned off, within the caps above. The
   assessor never decides tools, scope or destinations.
4. **One batch per thread.** Events on an external thread coalesce for 60
   seconds; at most one assessment per thread is in flight (a
   `watch_assessments` row for the thread with `finished_at IS NULL`), and the
   candidate names the thread's existing proposal so a duplicate updates it
   instead of adding one (`dsh:prompts/admission.md:66`; section 6).
5. **No overrides of process.** Rule text is prompt material inside the run
   it starts. It cannot change tools, approvals, egress, memory or mandatory
   notices (`dsh:docs/002-authorization.md:105` is not adopted).
6. **No card shortcut.** DeepSeek's `ignoredFromCard` exists because its
   router is a model. Turing's predicate already filters deterministically.

### Security and privacy

- The assessor sees only the excerpt and rule texts. With no tools, team,
  memory, persona, network or remote model (point 3, the assessor payload), an injected event can at most flip
  booleans in an `observe` proposal the owner reviews.
- Evidence must quote the excerpt, so a fabricated match is caught
  deterministically; a quote is not proof of correct interpretation, which is
  why an assessed rule only proposes and never fires unattended (section 7).
- **Start puts stranger-authored text into an attended turn.** The preview
  shows the owner that text before any model reads it. The turn is local
  and only confined tools can be confirmed into it (point 2), so
  nothing leaves the machine; recall-free and without the pinned persona,
  profile or any owner skill, so injected text cannot pull the owner's
  memories or skills into a transcript a stranger's words steer;
  delegation-free;
  and held to the automation's allowlist, so injected text can steer at most
  the tools the owner chose for this rule, each mutation still asking the
  owner. Every later turn in that session keeps these limits, an owner
  message included, until the owner explicitly detaches the session; a later
  stranger event then opens a new thread session with the same limits.
- Rule authoring is owner-only through the public facet; automations already
  refuse integration, memory and team tools.
- Excerpts and every copy follow F1's lifecycle; audit keeps rule ids and
  outcome codes, not text.
- An assessor job is checked at claim as well as at its result (point 3):
  text an author edited out, or a thread the owner dismissed, while the job
  waited never reaches a model, and a result judged against a rule the
  owner has since edited records nothing derived from it.

### Performance

- Deterministic rules: zero model calls; one range read of the
  `automations_event` partial index per admitted event, at most 20 rows.
- Assessor: one local completion per coalesced batch, never one per rule.
  Input at most 12 KiB serialized, output at most 768 tokens, both enforced.
  One completion is enforced too: today a completion that calls a tool the
  run was not offered becomes a recoverable unknown-tool result and another
  round (`agent-runtime-go/internal/agent/general_assistant.go:1036-1041`),
  so team protocol 2 adds `singleCompletion` (`AgentJob.single_completion`,
  field 28, point 3), set only on assessor jobs, under which the runtime
  makes exactly one completion and a response holding any tool call ends
  the run failed `assessor_tool_call`, with no recovery round and no
  proposal recorded. The runtime reports it through `emitRunFailed` with
  the tool-guard origin, never retried, mapped to `ReasonToolFailure`
  like the other tool-guard codes, under section 27's outcome-code rule,
  because `NormalizeFailure` stores an unregistered pair as `unknown`
  (`orch/runoutcome/outcome.go:472-481`).
  At most one assessment in flight per hub and 20 per hour, counted in
  `watch_assessments`. It is claimed only when no attended job is claimable
  (TUR-027), so it never goes ahead of an owner turn already queued. An
  assessment that is already running would otherwise hold the only slot for
  its whole length: on the reference machine, roughly 3,000 to 4,000 real
  tokens of input (the 12 KiB bound counts a byte as a token) and up to 768
  of output, which is an estimated 30 to 40 seconds at a 7B model's decode
  rate, not "several seconds". So an attended enqueue that needs its slot
  preempts it (point 3),
  and the owner waits only for the worker to stop. PRO-001c's acceptance
  measures both numbers and ships the assessor only if p95 assessment latency
  stays under 40 seconds and p95 attended claim delay with an assessment
  running stays under 2 seconds.
- The claim of an assessor job adds one read of its watch and thread rows,
  one primary-key read per frozen event, and one `automations_event` range
  read of at most 20 rows to recompute `rules_revision`, which the result
  commit repeats; other claims are unchanged.
- Measured: assessment latency, preemptions, `unavailable` rate, claim-time
  `assessment_stale` outcomes, proposals per day, and attended claim delay
  with assessments queued and running.

### Roadmap placement and dependencies

PRO-001. PRO-001w1 and PRO-001w2, in that order, ship after PRO-001a and
before PRO-001b, which depends on them. The assessor (PRO-001c) depends on
TUR-027 and on EVAL-001's scenario scaffolding. SKL-003/004 are not prerequisites: rules are human-authored.

### Delivery slices

| Slice | Size | Content | Acceptance tests |
| --- | --- | --- | --- |
| PRO-001w1 | M | `session_deletions.initiator` (`owner` or `sweep`, passed through the private `beginSessionDeletion`), with sweep-begun receipts left out of `PendingSessionDeletionReceipts`, in a migration that adds it as `NOT NULL DEFAULT 'owner'`, the partial index on `session_deletions(initiator, session_id) WHERE state <> 'completed'` and an index led by every foreign-key child column in the schema that leads none, with the index assertion in `orch/db/schema_invariants_test.go`; Reading the reconciler's pending receipts by keyset pages of 100 through that partial index, leaving out a child receipt whose parent receipt is still pending; a one-slot channel semaphore for the vault lock at every acquire site | the foreign-key index assertion passes, fails when any of the new indexes is dropped, and does not flag `agent_run_steps.run_id`; and an attended `SendMessage` on a database seeded directly with 100,000 runs, approvals and messages, measured before and after the migration, keeps its p95 commit latency within 10% or 2 ms of the earlier figure, whichever is larger; after the migration, an owner receipt pending from before it is still listed with Retry, and a delegation child's receipt is never listed; with 1,000,000 completed receipts and a few pending, the selection of each initiator's pending receipts uses the partial pending index in `EXPLAIN QUERY PLAN` and returns within 10 ms; an owner parent whose 20 pending children's ids sort on pages before and after the parent's records exactly one withdrawal attempt per child per pass, all through the parent's walk, and a child whose parent receipt has completed is finished on its own; with a vault pass held at `memoryVaultPassBarrier`, a `ScanMemoryVault` call and a completion whose contexts expire each return that context's error without blocking, and a `SetMemoryVault` call waits until the pass releases the slot and only then swaps the vault and its cache |
| PRO-001w2 | L | Capping the session-deletion reconciler's interval at one minute (`orch/app/app.go:494-496`), having each of its passes run the lease-fenced stale recovery for its pending sessions' runs before advancing them, and the `memory_completion_pending` code; Owner and sweep portions of at most 30 s of work each, counted per session, each walking its receipts from its own rotating cursor, a per-parent child cursor for a delegation tree's walk, and at most one memory completion a pass; Extending `reconcileMemoryVaultAtStartup` to hand the first pass its pre-walk list of the `memory_completion_pending` receipts, which that pass completes with no second walk, and `session.deleted` for every receipt a completion finishes; Running the session-deletion reconciler's first pass without waiting an interval, before its ticker loop, skipping the owner portion startup just ran; One content-free stage 4 row with no Retry for a sweep-begun receipt still unfinished an hour after its begin | an attended `SendMessage` issued at the start of a reconciler pass commits within 1 s; a sweep-begun withdrawal held past its quiesce deadline as `execution_unreconciled` never appears in `ListSessionDeletionReceipts` or as a Retry row, and appears as the no-Retry stage 4 row only from an hour after its begin (its `quiesce_deadline_at` less the quiesce lease), which survives a restart, leaving once removal completes, while a thread session (seeded directly) the owner deleted is listed with Retry; 100 sweep-begun thread sessions (seeded directly) with `delete_on_session_delete` sandbox artifacts, with a memory vault present, the files cleaner's cleanup answering in 2 s and the vault cleaner and both forget calls answering at once, are removed in cursor order, about 15 starting a pass, all within 8 passes, and trigger no vault reconcile, while an owner's chat receipt whose session holds memory evidence still runs one and stays retryable when it fails; a sweep-begun thread session (seeded directly) holding a `memory_evidence` row is left `memory_completion_pending` by its cascade's own transaction, and after a failed completion, and separately after a crash after the cascade, the next pass runs the completion again and the receipt completes only once a completion that started after its cascade succeeds; a `memory_evidence` row linked to a session while its cleaners run is counted by its cascade; a blocking vault reconcile delays no owner portion, no receipt starts after 30 s of its portion, and no reconciler pass runs more than one completion, owner receipts included; two owing sweep receipts cascaded in one pass are both completed by that pass's one successful completion, which removes the managed frontmatter citation of each; with a blocking vault and 20 owner receipts pending in `memory_reconcile_failed`, a removable sweep receipt's sentinel rows are gone within the usual pass bound; a sweep receipt whose sandbox cleaner fails once and then succeeds is cascaded within two later passes without a restart; with the vault reconcile failing on every pass, a second owing sweep receipt's session rows, holding a sentinel excerpt, are still removed within the usual pass bound while both receipts stay pending; with the first of two owing receipts failing its sandbox cleanup every pass, the second is cascaded and removed without a restart; while the files server is unreachable, an owner-deleted chat held quiescing by an execution whose lease the pass's stale recovery releases, behind two sweep receipts whose session ids sort before it and whose sandbox cleaner times out, completes, its completion run right after the owner portion, before the first sweep cleaner call is recorded, which the test asserts by order, so it fails if the pass walks receipts in today's session-id order (`orch/repository/session_delete.go:927-934`); no sweep receipt starts after 30 s of sweep work in a pass, and with three timing-out sweep receipts at the front and 20 removable ones behind them all 20 are removed within two passes; and a sweep receipt still unfinished an hour after its begin raises one content-free stage 4 row with no Retry that leaves once removal completes; an owing sweep receipt behind two pending sweep receipts whose cleaners time out is cascaded within two passes and completed by the next successful completion; an owner-deleted parent with 20 artifact-bearing delegation children under a files-server outage starts no child after 30 s of the owner portion, its remaining children resume next pass, and sweep processing starts within the portion's bound; an owner chat with a delegation child, finished by the reconciler under a failing vault, has its rows (with a sentinel) and the child's removed in the same pass while both receipts stay `memory_completion_pending`; a parent with three children whose files cleanup always times out, ahead of 17 removable ones, has all 17 cascaded within two passes; with 20 children whose files cleanup answers in 2 s and the parent's own cleanup timing out, no parent cleaner call is recorded in pass 1, pass 1's owner portion ends within 30 s plus one session's cleaner calls, and, with no other owner receipt pending, the parent's finish runs in pass 2; one owner parent with four children whose files cleanup always times out does not hold back a second owner receipt holding a sentinel, whose rows are gone within two passes, while each of the four children is still attempted by the wrap; with 100 sweep receipts whose files cleanup blocks forever, `app.New` returns within one owner portion and does not fail, the synchronous startup call has made no sweep cleaner call when it returns, and the goroutine's first sweep cleaner call comes within a second of `app.New` returning, with no owner cleaner call before it; an owner receipt already `memory_completion_pending` at restart is completed by the first pass's batches from the startup list before any sweep cleaner call, with no second vault walk, and with 10,000 sweep receipts pending on the completion at restart `app.New` still returns within one owner portion beyond the startup reconcile, and the first pass finishes them in batches of 100; with startup's owner portion cascading an owner receipt and the completion function set by `SetMemoryReconcileCompletion` blocking until its 30 s timeout, the synchronous `ResumePendingDeletions` records no completion call and returns without waiting for it, and the first pass records that receipt's completion before the first sweep cleaner call; with one owner receipt `memory_completion_pending` at restart, on the startup list, and no other owner deletion, the first pass records no `ReconcileMemoryVault` call, and no portion re-enters a receipt pending only on `memory_completion_pending`; a receipt that enters `memory_completion_pending` while a completion's walk is held at `memoryVaultPassBarrier` stays pending when that completion succeeds and is completed by the next; with 10,050 receipts pending, owner receipts among the last ids, one completion completes 10,000, every owner receipt among them, and the next the other 50; an owner-deleted chat finished by a reconciler pass, a sweep-begun thread session (seeded directly), and a receipt completed only by a later pass after an earlier completion failed each emit exactly one `session.deleted` on `SubscribeSessionUpdates` and end an attached `SubscribeSessionEvents` stream; with another vault pass held past the completion's deadline (`memoryVaultPassBarrier`), the reconciler's completion returns busy within its 30 s, its receipts stay pending, the pass continues within its budget, and a later pass completes them |
| PRO-001b | L | Event automations and their partial index, deterministic matcher, `would_start` proposals, two-step Start bound to the excerpt revision, sticky Dismiss and Watch this thread again (section 19), thread sessions with `watch_event` rows, sticky limits and Treat as normal conversation, search and recall exclusions, thread-session rotation by `watch_threads.session_withdraw_at` and per-row excerpt cuts by `watch_event_expiries`, the refused unowned-file write and `ApprovalDetails.unavailable_reason`, proposal row in UX-002 | Wildcard predicate refused; a 21st event automation on a watch refused; integration, memory and team tools still refused; enabling an event automation arms no `next_due_at` and a due tick never fires it; the matcher's query plan uses `automations_event`; no match records nothing; Start enqueues nothing until Confirm, and a second Confirm is refused; an automation whose allowlist names a remote-URL MCP tool, and one whose allowlist names a third-party `local_container` server's tool, each cannot be Started: the preview shows the reason `unconfined_mcp_tool`, a Confirm naming it is refused with that reason, and nothing is enqueued, while an automation allowlisting only bundled `files` and `system` tools can be, and so can one whose allowlist names `skills/skills_list`, whose confirmed turn's `skills_list` call lists no skill; saving a rule with an actor term sends one `GET /users/{login}` and stores the account id, and after that account is renamed and a different account claims its former login, the rule matches the named account's issue under its new login and not the other account's; the confirmed job is local with `skip_automatic_recall`, no roster, the allowlist as an enforced set, its pinned persona and profile frozen as withheld, and an empty skill snapshot; end to end with a real worker and private sentinels in `persona.md`, `profile.md` and the body of an enabled skill, with that skill's `$<skill-id>` in the event text, the captured model requests of a Start-confirmed turn and of an owner message in a thread session contain none of the three sentinels, `skills_list` there lists no skill, and after Treat as normal conversation the owner's next turn carries the persona and profile and a `$<skill-id>` in it loads the skill; a model call outside the set gets the worker's unknown-tool result with no beacon, and a beacon for it is denied `tool_not_selected`; with no local worker it is refused, not rerouted; the thread session's title holds only the repository's display name and the issue number, never the issue's title; a `watch_event` row and every message of a thread session are absent from `SearchMessages` and from automatic recall in another chat, while a detached session's answers are found and its `watch_event` rows are not; an owner message in a thread session runs local, recall-free and with no tools, and a remote model choice is refused; a later event-started turn keeps its limits after the owner has written there; Treat as normal conversation is audited, refused for the runtime identity, detaches the session, gives the owner's next turn ordinary defaults, and the next event on the thread opens a new thread session; with a fake clock, a thread session still receiving events in order is withdrawn when its oldest excerpt expires, 30 days after its first event, with its answers, and the next event opens a new one; when a newer event opens a thread session and an older, edited event on the same thread is then Start-confirmed into it, `session_withdraw_at` drops to the older event's expiry and the withdrawal begins by then, with the older excerpt's thread message, job `userText` and answers gone; the due-session selection uses the partial `session_withdraw_at` index in `EXPLAIN QUERY PLAN`; after a sweep's begin, a detach and an owner's deletion, that thread's row is never selected again and holds a NULL `session_withdraw_at`; with 10,000 threads whose sessions were already withdrawn, detached or deleted, a newly due thread session still begins within one tick of due; in a detached session a `watch_event` row and its job's `userText` read as the removal marker 30 days after the event while the owner's messages and Turing's answers remain; an owner who deletes a thread session gets a new one on the next event; after the owner deletes a watch and before the sweep runs, its thread sessions are absent from `SearchMessages` and from automatic recall in another chat, an owner send and Treat as normal conversation there are refused, and no event reaches them; the sweep then begins each one's deletion before deleting its `watch_threads` row, a crash between the two leaves the row and the next tick finishes, and a thread session the owner had already deleted keeps its receipt; the runtime identity cannot write a rule; a `watch_event` row is never in `messages_fts` after its insert, its cut or its deletion, while other rows still index; an edit on a dismissed thread, including one whose excerpt Dismiss cleared, stores no text; Confirm naming an `excerpt_revision` other than the stored one is refused and reopens the preview, including after an edit that left `updated_at` unchanged; an event matched by two rules shows both in the preview, Start requires a choice, the confirmed job's enforced set is the chosen rule's allowlist alone, and a Confirm naming an automation outside the proposal, disabled, or no longer matching is refused; the detach dialog states that the whole run history, tool-call arguments and approval records included, is kept as an ordinary chat and that only the `watch_event` rows and their jobs' `userText` are cut; with a sentinel from an excerpt copied into a tool call's arguments and an approval's arguments, a thread session withdrawn before detachment leaves no row holding the sentinel, while after detachment the sentinel stays in those two records and the `watch_event` row and its `userText` read as the removal marker; the section 19 dismissal tests; section 6's five PRO-001b tests, including an event and a Confirm while a thread session's withdrawal is still quiescing; a Start-confirmed job is written with `jobs.unattended = 0`, equal to `RunUnattended` for its run, and is claimed ahead of an older automation job; the PRO-001a clauses marked "(PRO-001b asserts)" (section 3, Delivery slices), on its proposals' derived fields and thread sessions; a thread session holding 500 event-started runs, each with its anchor message, answer, run events, a tool call, an approval and a correlated audit row, in a database that also holds 100,000 runs, approvals and messages of other sessions, is withdrawn by the 30-day sweep: the maintenance tick (section 27) only begins it, the session-deletion reconciler finishes it, the begin and the final transaction each finish within 500 ms on the CI runner, an attended `SendMessage` issued during either commits within 500 ms of it, its run-correlated audit payloads are scrubbed, and its `delete_on_session_delete` sandbox artifacts are removed before the receipt completes (needs PRO-001w); 20 watches each opening 60 new thread sessions an hour for 31 fake-clock days keep every withdrawal begin and excerpt cut within one tick of due, with attended `SendMessage` commits within 1 s (needs PRO-001w); with `TURING_JOB_REAPER_INTERVAL_MS` at 2 hours, a due thread session's withdrawal begins on time and a sentinel in its messages, tool-call arguments and approval arguments is gone within about a minute of the begin (needs PRO-001w); with a delivered assignment whose worker disconnected before the session came due, under a reaper interval of 0 and of 2 hours, the session is unreadable from the begin and its rows are gone within the execution lease and one pass, also with the lease expiring just after one reconciler pass (needs PRO-001w); with 100 thread sessions due for the 30-day withdrawal at once, one maintenance tick begins them, each in its own transaction within the tick's 1 s budget, and cuts their threads' due excerpts, also with `TURING_JOB_REAPER_INTERVAL_MS=0`, and an attended `SendMessage` issued at the start of that tick commits within 1 s (needs PRO-001w); in an attached thread session a `files` update of a sandbox file that predates the session gets an approval card reporting `APPROVAL_PREVIEW_STATE_UNAVAILABLE` with `can_approve` false and `unavailable_reason` `thread_session_unowned_file`, which the client shows as the fixed text naming Deny, Treat as normal conversation and Check again, with no Retry, while an unavailable preview with no reason still shows today's text and Retry, and `unavailable_reason` is empty whenever the state is not `UNAVAILABLE`, including after the pre-existing file changes on disk, an approve of it is refused, a reservation for it made directly is refused `thread_session_unowned_file`, and the file stays byte-identical, a file the session created is removed with its withdrawal, and when the owner then chooses Treat as normal conversation, the pending card on a second client, mounted before the detach, shows `APPROVAL_PREVIEW_STATE_READY` with `can_approve` true after Check again, without remounting, and approving that same approval writes the file, while no `approval_previews` row ever holds the override; a session detached, whose watch is then deleted, has its `watch_event` rows and their jobs' `userText` read as the removal marker once the original events pass 30 days, across a restart, while the rest of its history stays; with 10,000 due `watch_event_expiries` rows in sessions whose deletion has begun but not finished, each row is selected once and leaves the range with its expiry row deleted and no message rewrite, the backlog drains at no less than 100 rows a batch with no tick spending more than about 1 s on the sweep, and a detached session's row falling due after the drain is cut within one tick of due; the selection uses the `expires_at` index in `EXPLAIN QUERY PLAN`; a repository whose `full_name` is 140 characters opens a thread session whose title is at most 120 runes and ends with `#` and the whole issue number |
| PRO-001c | L | `sessions` rebuild for the `watch` kind, `BeginWatchSessionDeletion` and `session_deletions.session_kind`, assessor job, `max_output_tokens` and team protocol 2, Go validator, batching, `watch_assessments` caps, the claim-time input check and `rules_revision`, preemption, budgets, assessor withdrawal | Migration test: row counts, foreign keys and indexes survive the rebuild and an unknown kind is still refused; every public session mutation, `DeleteSession` included, refuses a watch session; the sweep withdraws an assessor session through `BeginWatchSessionDeletion`, including the assessor sessions of a deleted watch, a crash after begin is finished by `ResumePendingDeletions` through the watch path, and the receipt never appears in `PendingSessionDeletionReceipts` or as a Retry row, and appears as the no-Retry stage 4 row only once unfinished an hour after its begin (its `quiesce_deadline_at` less the quiesce lease); validator table: missing, extra, disabled and duplicate ids rejected; match without verbatim evidence rejected; remote route refused at enqueue; the job payload is point 3's assessor payload: `enforceSelectedTools: true` with an empty `selectedTools`, no persona, an empty skill snapshot, `skipAutomaticRecall`, `max_output_tokens: 768`, `singleCompletion: true`, `minimumTeamProtocolVersion: 2`, and no `teamRoster` or `teamContinuation` key, and the claimed `AgentJob` carries `max_output_tokens = 768` and `single_completion = true`; the assessor enqueue writes `jobs.unattended = 1`, equal to `RunUnattended` for its run; an assessor job older than a chat job: the chat job is claimed first; from an assessor run, a beacon, a direct `CallRegisteredMcpTool` and a direct `CallMemoryTool` from the `runtime` identity are each refused as not selected, with no server row (`sealed_token` included) loaded, no vault file read and no approval consumed, and the test fails if `GetMCPServer` runs; a direct `CallTeamTool` from the `runtime` identity on an assessor run, naming an active specialist, is refused with `PermissionDenied` before `RunTeamRoster` runs, creates no child session, run or `delegations` row, consumes no approval and reads no profile file; a registered-MCP call from a run with a non-empty enforced set still reads the server row and is refused by name as today; a version-1 worker never claims it; the runtime sends the smaller of the job's and the provider's limit; a rule set over the input bound is refused at save; with 6 KiB comments made of `<`, of `"` and of control characters, the serialized assessor input stays within 12 KiB, the runtime's estimate within 12,288, the job runs rather than being recorded `unavailable`, and its `num_ctx` bucket is no larger than for plain ASCII text of the same length; with one assessment in flight, or 20 enqueued in the past hour, no further assessment is enqueued, including after the earlier ones' sessions are withdrawn, and the batch is enqueued on a later tick; an assessor job queued before an owner turn is claimed after it; an owner turn enqueued while an assessment runs cancels it with `assessment_preempted`, is claimed next, and the batch is assessed again afterwards; with the assessor's shutdown held, a heartbeat and, separately, a worker disconnect before its lease expires each leave the preempted run contained, no job is claimed into its slot until its worker acknowledges the exit or its lease is recovered, and the recovery loop resends `RunCancelled` for it (`TestAStoppedTaskStaysContainedUntilItsWorkerLetsGo`, `orch/repository/delegation_lifecycle_test.go:538`, extended to the code); with `TURING_MAX_CONCURRENT_RUNS_GENERAL=2` and one slot free, or with the owner's turn held behind its session's earlier turn, an attended enqueue leaves a running assessment alone; 20 preempted assessments in an hour do not stop the next one being enqueued; measured on the reference machine, p95 assessment latency is under 40 seconds and p95 attended claim delay with an assessment running is under 2 seconds; the assessor session is withdrawn once its result is recorded, and the evidence is cleared when the excerpt expires; an injected-event fixture produces no tool call and no run beyond the assessment; with TUR-028's quiet on, no assessment is enqueued and the held batch is assessed once after it is turned off; an assessment that finishes after its thread was dismissed records its outcome and no proposal; an edit that replaces an excerpt while its assessment is queued, and another while it runs, each finish the assessment `assessment_stale` with no reason or offsets recorded (the queued one at claim, with no model request), and the next assessment reads the current text, including when the edit left `updated_at` unchanged; an `assess_instruction` changed on the same automation id while its assessment is queued, and another while it runs, each with the excerpt unchanged, finish the assessment `assessment_stale` (the queued one at claim, with no model request), record no reason, offsets or proposal from the old result, and coalesce a fresh assessment whose input carries the new instruction, and so does a predicate edited on an assessed rule, and an assessed rule added to the watch, while one runs; a `rules_revision` table test shows it changes with any rule's id, instruction or predicate and with the set of assessed rules, and not with rule order in the database; with a secret sentinel that the author edits out of the comment, no proposal field and no later assessor input contains it; an assessment that finishes after its excerpt expired, or after Dismiss cleared it, records its outcome only and opens no proposal; a positive assessment that finishes after the owner disabled its watch, and one after the owner deleted it, each while running, records its outcome only and opens or reopens no proposal, and a watch disabled or deleted while its assessment is queued ends that assessment `assessment_stale` at claim with no model request; end to end with a real worker, its captured model requests, `TURING_MAX_CONCURRENT_RUNS_GENERAL=1` and an assessor job queued behind an attended turn, an edit that removes a secret sentinel from the comment, and separately a Dismiss of its thread, while the job waits each end it `assessment_stale` at claim, no captured model request contains the sentinel, the assessor session is withdrawn through `BeginWatchSessionDeletion`, and a fresh assessment of the current text is coalesced after the edit and none after Dismiss, and one that finishes after the watch moved between `observe` and `auto` records the proposal and fires nothing; the PRO-001a clauses marked "(PRO-001c asserts)" (section 3, Delivery slices), on its assessor fields; with a captured provider that answers an assessor job with a completed call to an unoffered tool, the runtime makes no second completion, the run fails and its stored `error_code` is `assessor_tool_call`, not `unknown`, with outcome reason `tool_failure`, `watch_assessments` records one assessment and no proposal is recorded, while a delegation child with an empty enforced set still gets today's unknown-tool recovery; and section 19's assessment-and-rewatch barrier tests; its migration adds an index led by `watch_id` on `watch_assessments`, and deleting a watch with 14,400 assessment rows uses it in `EXPLAIN QUERY PLAN` with each batch within 50 ms |

### Open questions

None. The first rules are the owner's to write; the design ships none.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| LLM judge or owner-written rules | grok: reject the judge, rules are deterministic automation bindings; mai, sol, opus: one bounded local judge | Deterministic first; the judge optional, observe-only until EVAL-001 | A 7B judge on a one-slot machine is the most expensive and least proven part. Predicates deliver value with zero calls; the judge is added only for rules a predicate cannot express. |
| Where rules live | sol: up to eight rules in PRO-001 settings; mai: `ProactiveRule` rows; grok, opus: automation rows | Automation rows | Reuses the prompt, exact allowlist, enable switch, history page and save-time tool refusals. |
| Assessor job shape | sol: a new version-gated job purpose; opus: an ordinary run with no tools | The existing specialist-job contract with an empty enforced set, plus one narrow-only output field | Team protocol 1 already guarantees no tools, no persona and no recall; only the output cap is new. |
| Rule overrides of process | dsh, mai: allowed for the ticket | Not adopted | Overrides that change process are a path to self-escalation; tools and approvals stay code-enforced. |
| Daily ceiling | grok: 24 runs per automation per day | Per-watch admitted-event cap and per-hub assessment cap | One limit at intake bounds every downstream cost. |
| Round 1 review: event automations and the scheduler | grok: an event row would be armed and claimed as a scheduled fire, and the dispatcher only wakes workers | `schedule_kind = 'event'` is never armed and is filtered from due claims; `FireEventAutomation` shares the fire transaction (since round 21 its tail, inside the poller's page transaction; section 6, point 1) | Reuses the column and the fire; the dispatcher stays what it is. |
| Round 1 review: Start | grok, opus: Start put stranger text into a full owner turn without preview or analysis | Preview then Confirm; local, recall-free, delegation-free, allowlist-bound until the owner writes | The owner sees the text first, and injected text reaches no more than the rule's own tools. |
| Round 1 review: assessor budget | sol: the job cannot carry a 768-token limit; opus: a 16 KiB excerpt does not fit a 4,096-token input | A narrow-only `max_output_tokens` gated by team protocol 2; a 6 KiB assessor excerpt in a 12 KiB input measured by the runtime's own estimate | Inherited limits would let one assessment hold the only slot for a full-length answer, which contradicts "never delays an owner turn" more than the cap does. |
| Round 1 review: `watch` kind | opus: widening the kind check needs a `sessions` rebuild | State the rebuild, on the hooked-migration precedent | A nullable marker column would leave watch sessions passing every chat guard. |
| Round 2 review: withdrawing a watch session (pro001-watch-withdrawal-not-reachable) | grok: the public begin refuses a non-chat session, so the cited lifecycle could not withdraw an assessor session, and a receipt without its row would resume as a chat | `BeginWatchSessionDeletion`, a `session_kind` on receipts, a watch branch in `ResumePendingDeletions`, and watch receipts left out of the owner's pending list | It mirrors how delegation children are withdrawn; public `DeleteSession` keeps refusing a watch session. |
| Round 2 review: hub caps without state (assessor-hub-rate-cap-unimplemented) | mai: "one in flight, 20 per hour" had nothing to count | A content-free `watch_assessments` row per assessment, counted in the enqueue transaction | Runs cannot be counted: the assessor's session, and its runs with it, are withdrawn once the result is recorded. |
| Round 2 review: matcher index (event-automations-index-unspecified) | mai: per-event matching had no stated index or bound | `automations_event` partial index and at most 20 event automations per watch | Each event costs one bounded index range read. |
| Round 2 review: head-of-line latency (pro001c-assessor-head-of-line-latency-understated) | opus: with one slot, an owner turn arriving just after an assessment starts waits all of it, about 30 to 40 seconds on a 7B model | State the estimate; an attended enqueue preempts a running assessment; a measured release gate | Preemption reuses the cancel path and keeps "never delays an owner turn" true; a tighter output cap alone would still leave tens of seconds. |
| Round 2 review: sticky limits | grok, opus: see section 3's decision log | Limits hold on every thread-session turn until the owner detaches the session | One rule for Start, owner messages and later events. |
| Round 3 review: preemption with a free slot (pro001c-preemption-ignores-free-slots) | opus: any attended enqueue cancelled a running assessment, even with a free slot or an owner turn held by turn order, and every preemption counted toward the hourly cap | Preempt only when every slot is taken and the attended probe would claim the new job; preempted rows do not count toward the cap | Preemption exists to free the slot the owner needs; when cancelling frees nothing the owner can use, it only wastes the assessment, and the owner's own turns must not exhaust the assessor's budget. |
| Round 4 review: an assessment outran an edit (pro001-assessor-edit-result-race, WATCH_ASSESSMENT_RESULT_MISSING_REVISION_FENCE) | mai, sol: the result commit checked only `dismissed_at`, so a result computed on the old text restored reason and offsets after an edit, possibly including text the author removed | Each assessment freezes its `(event id, excerpt_revision)` pairs; the result commit records derived fields only when every excerpt is still held at that revision, otherwise finishes `assessment_stale` and coalesces a fresh assessment, and never reopens a proposal whose excerpt was cleared | The same revision binding Confirm already uses; ids and revisions keep the row free of text (revisions are body hashes since round 8, section 3). |
| Round 4 review: several rules match one event (pro001-multiple-rule-match-undefined) | mai: Start showed "the matched automation" and `auto` fired on "a match", with no rule for several | Start lists every match and the owner picks one, validated by the server; `auto` fires at most one run per event (section 7); allowlists are never combined | A union of allowlists would give an event more authority than any rule the owner wrote. |
| Round 5 review: a result after the watch was turned off (pro001-assessor-result-rechecks-watch) | mai: the result commit re-read the thread and excerpts but not the watch, so an in-flight positive result could open a proposal for a watch the owner had disabled | The result commit also re-reads the watch: present and enabled, or it records the outcome only; a proposal is recorded in either mode, and a later assessor-confirmed fire still needs `auto` at commit | The same re-read of current rows that Start and `auto` already do; the owner's off switch must win over work already in flight. |
| Round 12 review: event text invoking an owner skill (WATCH_EVENT_EXPLICIT_SKILL_INVOCATION) | sol: see section 3's decision log | A Start-confirmed turn, and every owner message in its thread session, carries an empty skill snapshot | Start puts stranger text in `userText`, the one input the runtime scans for `$<skill-id>`. |
| Round 14 review: a queued assessment sent stale text (PRO001_ASSESSOR_INPUT_DISPATCH_REVISION_FENCE) | sol: the revision fence ran only at the result commit, so an edit that removed a secret, or a Dismiss, while the job waited behind attended work still sent the old text to the model | The claim of a queued assessor job re-checks the watch (present and enabled), the thread (not dismissed) and every frozen `(event id, excerpt_revision)` pair; on a mismatch the job ends `assessment_stale` with no model call, its session is withdrawn through `BeginWatchSessionDeletion`, and a fresh assessment is coalesced only while the source is still eligible; the result-commit fence stays; real-worker tests for an edit and a Dismiss | A fence after inference cannot unsend text; the claim is the last point before a model reads it, and it reads the same rows the result commit does. |
| Round 20 review: a result judged against rules the owner has since edited (PRO001_ASSESSOR_RULE_REVISION_FENCE) | sol: an assessment froze only its event and excerpt revisions, so changing `assess_instruction` on the same automation id while the assessment was queued or running let a judgement of the old instruction record as a match for the new one | Point 3 states the rules revision once: `rules_revision`, the SHA-256 over the assessed rules' `(id, assess_instruction, predicate_json)` in id order, is frozen on the `watch_assessments` row; the claim and the result commit recompute it over the current enabled assessed rules and on a difference end the assessment `assessment_stale`, recording no derived field and coalescing a fresh one while still eligible; queued and running tests with an unchanged excerpt | The same revision binding the excerpt already has: an owner's edit to a rule must win over a judgement of its old text, and ids alone do not name text. |
| Round 21 review: a remote MCP tool in a Start-confirmed turn (PRO001_START_REMOTE_MCP_EGRESS_UNSPECIFIED) | sol: Confirm froze the chosen allowlist, which save-time validation lets name a remote-URL MCP tool (`orch/repository/automations.go:182-203`), while such a call needs a run egress decision (`orch/service/mcpregistry/call.go:64-78`, `orch/repository/egress.go:579-603`), so the turn either failed its tool or contradicted "nothing leaves the machine" | Point 2 states once that Start and Confirm refuse an automation whose allowlist names a remote MCP tool, classified at Confirm time by the server's current tier, with the content-free reason `remote_mcp_tool` on the proposal; Security and privacy points there; a PRO-001b test. `auto` needs nothing new: `UnattendedReadTools` already keeps every third-party MCP tool out of its set (section 7) | Refusal keeps the no-egress assurance true with no per-run disclosure flow in a stranger-steered turn, and the call path still fails closed if a server is reclassified later. Superseded in round 30: the refusal covers every tool that is not sandbox-confined, a third-party `local_container` server's included, with the reason `unconfined_mcp_tool` (see that row). |
| Round 30 review: a local MCP endpoint is not confined, and a predicate's actor terms (PRO001_START_LOCAL_MCP_EGRESS_GUARANTEE, PRO001_ACTOR_LOGIN_REASSIGNMENT) | sol: Start refused only `remote_url` tools, yet a third-party `local_container` server is not sandbox-confined (`orch/service/mcpregistry/service.go:1344`) and its calls need no egress decision (`orch/service/mcpregistry/call.go:64-78`), so "nothing leaves the machine" did not hold; sol: a predicate's actor terms were logins | Point 2 states once that Start and Confirm refuse any tool that is not sandbox-confined (only bundled servers' tools pass; amended in round 31: the runtime-local `skills` loaders pass too, see that row), `remote_url` and `local_container` alike, with the content-free reason `unconfined_mcp_tool`, which replaces `remote_mcp_tool`; Security and privacy points there; PRO-001b adds the `local_container` negative fixture. Point 1's actor terms are bound by user id (section 3, point 3), with a PRO-001b test | Only the bundled servers' confinement is something Turing enforces, and a bundled server cannot be deleted, re-registered or impersonated, so the assurance holds after Confirm too. |
| Round 31 review: the `skills` pseudo-server at Start (pro001b-confined-rule-refuses-skills-pseudo-server-contradicts-skills-list-test) | opus: `skills` has no `mcp_servers` row (`orch/db/schema/0016_mcp_registry.sql:48,60`), so the bundled-only rule refused it, contradicting PRO-001b's `skills_list` assertion; refuse it and replace the assertion, or allow it with a fixture | Allow: point 2 states once that the runtime-local `skills` pseudo-server passes Start and Confirm beside the bundled servers, because the worker serves it from the job's own skill snapshot (`agent-runtime-go/internal/agent/general_assistant.go:928,1049-1051`), it has no network path, its name is reserved, and a thread session's snapshot is empty; PRO-001b keeps the `skills_list` assertion and adds a fixture where an allowlist naming `skills/skills_list` is confirmable and lists no skill | A loader that reads only an empty snapshot can neither egress nor reveal an owner skill, so refusing it would protect nothing. |
| Round 42 review: the assessor payload and its refusals (assessor-direct-team-call-not-refused, tur027-mcp-refusal-reads-server-row, tur027-assessor-acceptance-in-slice-before-assessor-exists) | grok: `CallTeamTool` decides from `RunTeamRoster` alone (`orch/service/team/call.go:154-166`), the `runtime` identity may call it directly (`orch/app/app.go:353`), and nothing kept a roster off the assessor's job, so unattended status and an empty set did not stop an assessor delegating; grok: registered-MCP dispatch loads the server row, `sealed_token` included, before refusing an unselected tool; opus: the assessor's acceptance sat in TUR-027 and PRO-001c asserted only "carries no tools" | Point 3 states the assessor payload once: `enforceSelectedTools: true` with an empty `selectedTools`, no `teamRoster` and no `teamContinuation`. PRO-001c makes `CallTeamTool` refuse, before `RunTeamRoster`, a run whose selection does not allow `team/team.delegate` (`RunToolSelection` and `ToolSelection.Allows`, as the memory service does), and makes `CallTool` return not-selected before `GetMCPServer` for an enforced empty set. PRO-001c's acceptance asserts the payload, the claim order, `jobs.unattended`, and each refusal with no server row, vault file or profile file read and no approval consumed | Every gate then refuses an assessor before reading what it guards, and the team gate changes nothing for today's runs, none of which carries a roster and an enforced set. |
| Round 56 review: a preempted assessment's containment (ASSESSOR_PREEMPTION_MISSING_EXECUTION_STOP_FENCE) | sol: the committed-stop rules that keep a cancelled running run contained (`orch/repository/assignments.go:149-150`) and resend `RunCancelled` (`orch/service/runtime/service.go:1512-1522`) recognize only `user_cancelled` and `parent_run_ended`, so a heartbeat could free a preempted assessment's slot while it still ran | `assessment_preempted` joins both; PRO-001c holds the assessor's shutdown and asserts containment across a heartbeat and a disconnect, and the resend | A preemption frees the slot only when the model has actually stopped, or the one local engine would run two generations. |
| Round 58 review: one completion, enforced (PRO001_ASSESSOR_SINGLE_COMPLETION_NOT_ENFORCED) | sol: the runtime turns a completed call to an unoffered tool into a recoverable unknown-tool result and another round (`agent-runtime-go/internal/agent/general_assistant.go:1036-1041`), so an assessor with an empty enforced set could spend several 768-token completions on one counted assessment | Team protocol 2 adds `singleCompletion`, set only on assessor jobs: exactly one completion, and any tool call ends the run `assessor_tool_call` with no proposal; PRO-001c tests it, and that a delegation child keeps today's recovery | The call and latency budgets count completions, so the runtime must enforce the one they assume. |
| Round 59 review: carrying `singleCompletion` to the worker, and its failure code (pro001c-single-completion-not-on-agent-job, pro001c-single-completion-contract-incomplete, pro001c-assessor-tool-call-code-erased) | grok, opus: the flag had no `AgentJob` field, no copy into the claimed job, and protocol 2 and section 27 still named only `max_output_tokens`, and PRO-001c's payload test omitted it; grok: `assessor_tool_call` is not an allowlisted failure pair, so `NormalizeFailure` would store `unknown` | `AgentJob.single_completion` is field 28, copied as `enforce_selected_tools` is; protocol 2 means honouring fields 27 and 28; section 27 names both; PRO-001c asserts the payload and the claimed job; the tool-guard `assessor_tool_call` pair is added to `failureReasons` and `runtimeProducerPairs`, and PRO-001c asserts the stored code | A flag the worker never receives enforces nothing, and an outcome normalized to `unknown` tells the owner nothing. |
| Round 60 review: the assessor's input bound and its failure mapping (pro001c-assessor-input-bound-ignores-json-escaping, pro001c-assessor-tool-call-mapping-table-and-reason-unstated) | opus: the runtime's estimate is the marshalled request's length (`agent-runtime-go/internal/llm/ollama.go:255-260`), and a 6 KiB comment of `<` escapes to about 36 KiB, so the bound did not hold for stranger text; the `assessor_tool_call` pair named neither `approvedFailureMappings` nor its reason | The 6 KiB cut is measured on the escaped length, with a PRO-001c fixture of `<`, `"` and control characters; the pair maps to `ReasonToolFailure` under section 27's outcome-code rule, which names `approvedFailureMappings`, and PRO-001c asserts the stored reason | A bound must be measured the way the runtime measures it, and every mapping table a test enforces has to be named. |

## 6. F4 — Subscriptions and follow-up fan-out

### Verdict

Reject the mechanism. Turing's session already carries follow-ups: each owner
message is a new run in turn order. DeepSeek's ungated fan-out into running
work and its resume of ended work under an old Mandate conflict with
run-owned consent. Three rules carry forward as acceptance on PRO-001 and
CHN-002.

### DeepSeek source

- Every Trigger on a subscribed Thread reaches its live Tickets with no gate
  and no new authority (`dsh:docs/002-authorization.md:18-20`;
  `dsh:docs/001-domain.md:37`); admission checks on Subscriptions are a
  rejected alternative (`dsh:docs/001-domain.md:54`).
- The Executor owns a person's follow-up and hands back out-of-scope ones
  once (`dsh:docs/002-authorization.md:22-38`). Context-only follows grant
  nothing (`dsh:docs/002-authorization.md:40-44`).
- An ended Ticket resumes on a person's message under its old Mandate, "regardless
  of who sent it" (`dsh:docs/002-authorization.md:127`;
  `dsh:src/core/tickets.ts:247-262`).
- Sources poll subscribed Threads for two weeks after the last activity
  (`dsh:docs/003-execution.md:116`).

### Turing today

- Each owner message enqueues a new run, and the claim query enforces turn
  order per session (`orch/repository/jobs.go:1393-1420`).
- Delegated children are single-shot and refuse public mutation; their results
  join once into a continuation (team design §7.5 and §7.6,
  `docs/superpowers/specs/2026-10-03-turing-orchestrator-agent-team-design.md`).
- Remote inference consent is run-owned and one-time
  (`docs/NORTH_STAR.md:276-277`).
- A scheduled automation's runs all land in one session per automation
  (`orch/repository/automations.go:744-774`), and a run replays its session's
  history.

### Unified design

No subscription table. Three rules:

1. **PRO-001: one session per external thread at a time.** A thread maps to
   at most one open proposal and one current thread session. **The current
   thread session, stated once:** it is the session `watch_threads.session_id`
   names, and only while that session is `deletion_state = 'active'`. A
   session that is deleting or gone (the owner's deletion or the 30-day
   sweep, section 3, point 8, even while its withdrawal is still quiescing),
   or a cleared `session_id` (Treat as normal conversation, section 3,
   point 7), means the thread has none. A direct fire (below) or a Confirm
   (section 5, point 2) that finds none opens a new thread session and
   replaces `watch_threads.session_id` with it in the same transaction as
   its enqueue: a direct fire in the poller's page transaction (below), a
   Confirm in its own RPC transaction. So neither ever enqueues into a
   deleting session, whose enqueue `requireChatSessionTx` refuses
   (`orch/repository/jobs.go:944-948`,
   `orch/repository/sessions.go:502-514`): it is only ever called on an
   active session, and the poller's page transaction never fails because a
   thread session is deleting. A thread session is also
   replaced when its oldest excerpt expires (section 3, point 8). Every
   event-started run on that thread, Start-confirmed or `auto`, from any
   matching rule, runs in its current session, in turn order; event
   automations never use the per-automation session. So two runs on one
   thread, from two events matched by different rules, run in one session,
   one after the other, and one rule matching two threads gives one run in
   each thread's session. A run's history
   therefore holds only its own thread's third-party text. **The thread
   session's `auto` ceiling, stated once:** a thread session takes at most
   500 `auto` runs, counted in `watch_threads.session_auto_runs`, which each
   `auto` fire into the current session increments in its own transaction
   and which a new thread session resets when it replaces
   `watch_threads.session_id`. A direct fire or the pending-fire hook that
   finds the current session at 500 fires nothing: it records the match on
   the thread's proposal, as `observe` would, with the content-free
   outcome `thread_session_full`, and the hook drops its row. Start stays
   available, because it is the owner's act. The session's 30-day
   withdrawal, or the owner's deletion, ends it, and the next event opens a
   new one counting from 0, so no withdrawal removes more than 500 `auto`
   runs' history (section 3, point 8). Idempotency: an
   `auto` fire is recorded in `automation_runs`, which gains a nullable
   `watch_event_id` and a unique partial index on `(watch_event_id) WHERE
   watch_event_id IS NOT NULL`, so an event fires at most one run, its
   winning rule's (section 7, Fire); no separate fires table is kept. A Start-confirmed turn needs
   none: Confirm closes its proposal. **Coalescing and the direct fire,
   stated once** (`auto` only). Coalescing counts only runs whose
   `automation_runs` row has a `watch_event_id`, exactly the runs whose end
   the hook below observes: while such a run of a rule is queued or running
   in the thread's current session, a later event on that thread for which
   that rule wins (section 7, Fire), in the poller's page transaction,
   writes or replaces one `watch_pending_fires` row keyed by (that current
   session, automation) and naming the latest event, and updates the
   proposal. The key's session is the row's stamp; a thread has at most one
   current session, so it still holds one live follow-up per thread and
   rule, and a row stamped with an earlier session of the thread is a
   separate row that its own run's end deletes (below). A Start-confirmed run has no
   `automation_runs` row, so it never holds back a fire. Otherwise the
   poller fires directly, entirely inside the already-open page transaction
   that admits the event, with no second `Begin` on the database's one
   connection (`orch/db/connection.go:47`): it opens a new thread session
   when there is no current one (above), replaces
   `watch_threads.session_id`, writes the `automation_runs` row and
   enqueues. Admission and the fire commit together: a failure rolls the
   whole page back, and the next poll re-reads the event, which the unique
   index on `watch_event_id` still finds unfired, so a crash never leaves an
   admitted event in the ledger without its run. That rollback is a failed
   poll of the stream (section 3, point 2, Page transactions), so a fire
   that fails on every poll shows `error` after the third. An expected
   refusal is never such a failure: every check in the gate and the fire
   path that can refuse (routing unavailable, a dismissed event,
   `observe_only`, an ineligible event, the rate cap, a full thread
   session) returns a recorded
   outcome that the page commits (a receipt, a proposal or a content-free
   audit row), never an error; an unconfined MCP tool is not a refusal there,
   because the fire's tool set leaves it out (section 7). This matches
   `ClaimDueAutomation`, which commits an audit row and returns a failed or
   skipped outcome for its refusals
   (`orch/repository/automations.go:580-676`). So no refusal
   rolls a page back. Quiet is not a refusal there: a page transaction that
   sees quiet on or the drain pending, or the watch's `generation` changed,
   is a stop (section 3, point 2, Stopping a poll), which rolls the page
   back, so no fire commits from an interval the watch did not cover, and `FireEventAutomation`
   keeps its quiet check (section 7) for the pending-fire hook and as
   defence in depth. The pending row's consumer is a hook in
   `applyRunTransitionTx` (`orch/repository/run_state.go:801`, beside the
   delegation join's `maybeJoinTx`): on the terminal transition of a run
   whose `automation_runs` row has a `watch_event_id`, it probes, in the
   same transaction, the pending row keyed by the run's session and that
   row's automation, both already in hand, with no thread lookup first.
   This is the one statement of the hook's session fence. A row stamped
   with another session is not found, so it is left untouched: it was
   coalesced against a later thread session's run, whose own end consumes
   it. The row found is consumed only while that session is
   active (`deletion_state = 'active'`, as `maybeJoinTx` requires before it
   queues a continuation, `orch/repository/delegation_join.go:66-77`) and is
   still its thread's current session. Otherwise the hook deletes that row,
   fires nothing, opens no session and never fails the transition. A
   withdrawal, whether the owner's deletion or the 30-day sweep (section 3,
   point 8), cancels the session's runs inside its own begin transaction,
   after marking the session `deleting`
   (`orch/repository/session_delete.go:226-230,270,326`), and the enqueue
   refuses a deleting session, so a fire there would roll the withdrawal
   back; the fence keeps every withdrawal committing, and a withdrawing
   session's own pending row is deleted, never fired. When the hook
   consumes a row, it re-reads the watch (enabled, still `auto`), the
   automation (present, enabled, and `mayFireUnattended`, section 4,
   point 2), `watchEventEligible` for the pending event (section 4,
   point 2), the pending event (excerpt held, no `observe_only` marker,
   section 3, point 2), the event (not dismissed, section 19, point 1), the
   current session's `auto` count (under 500, above) and the
   owner's quiet switch (off and not draining, section 18), and fires through
   `FireEventAutomation`'s tail with the pending event, or drops the row when
   any check fails. Turning quiet on deletes every pending row (section 18,
   point 2), so a follow-up never outlives a quiet period, and a backlog
   clamp that moves a stream's `since` deletes its watch's rows (section 3,
   point 2).
   `RunTransitionResult` carries the new run id, and the caller wakes workers
   after commit, as it dispatches a continuation today. Because both sides write
   in one serialized SQLite transaction, an event can neither be lost between
   them nor fire twice. In `observe`, a later event only updates the
   proposal, whatever the state of the thread's session.
   Nothing starts a parallel run in one thread session.
2. **CHN-002:** a channel thread maps to one session. A later message is a new
   explicit run with fresh per-run disclosure, never a resume under an earlier
   run's consent.
3. **Everywhere:** third-party content delivered as context grants no
   authority and never enters a running or delegated run. It reaches a model
   only framed as data in a new run.

### Security and privacy

Rejecting ungated fan-out removes an injection channel: bot or CI text
reaching a run that still holds tools. Keeping consent run-owned preserves
`docs/NORTH_STAR.md:276-277`. Children keep their frozen tools and approvals
and receive no mid-run input.

### Performance

The hook adds one primary-key probe of `automation_runs` (keyed by
`run_id`, `orch/db/schema/0009_automations.sql:71`) to every terminal
transition, an ordinary attended turn's included, which finds no row and
stops. Gating it on TUR-027's `jobs.unattended` would not remove that read:
the transition reads the run and its assistant message, not the job
(`orch/repository/run_state.go:416-426`), and a job is found by its run
only through `idx_jobs_run` (`orch/db/schema/0025_jobs_run_index.sql:4`),
so reading the flag costs one indexed read too. On the terminal transition
of a run whose `automation_runs` row has a `watch_event_id`, the hook adds
one primary-key probe of `watch_pending_fires` by (the run's session, its
automation), both already read, with no thread lookup; only when a row
exists does it read the session by its primary key and its `watch_threads`
row through the unique `watch_threads.session_id` index. No standing model, no subscription polling: a watch polls
its source once per interval regardless of how many threads it follows.

### Roadmap placement and dependencies

Acceptance notes on PRO-001 and CHN-002. No new item.

### Delivery slices

None of its own. PRO-001b adds five tests: two events on one issue yield one
proposal; two events on one thread, matched by different rules and both
started, run one after the other in one session; one rule matching two
threads runs once in each thread's session; while an owner deletion of a
thread session is still quiescing, and during the 30-day sweep's
withdrawal of one, a newly admitted event on an `observe` watch commits
its page and updates the proposal, with no stream failure and nothing
enqueued into the deleting session; and a Confirm in that window runs in a
new thread session that replaces `watch_threads.session_id`. PRO-001d adds
eleven: an event that arrives while its rule's run on that thread
is active fires exactly one more run, with the latest pending event, in the
same transaction as that run's terminal transition, and workers are woken
after it commits; a pending fire whose watch has left `auto`, whose
automation is disabled, or whose event has since left the watch's scope or
its rule's predicate, by then fires nothing and its row is gone; with a
pending fire held for a listed author's comment, an edit by an unlisted
account replaces the excerpt, deletes the pending row and marks the event
`observe_only`, and the running run's end fires nothing; a replayed
event fires nothing, enforced by the unique index on `automation_runs`;
a thread session holding an active `auto` run and an eligible pending
fire, deleted by the owner and, separately, withdrawn by the 30-day sweep,
commits its deletion, creates no run in it, and leaves no pending row
stamped with it; while either withdrawal is still quiescing, a newly
admitted event on an `auto` watch commits its page, the new thread
session, the `automation_runs` row and the job in one transaction, with no
second `Begin`, no stream failure and nothing enqueued into the deleting
session; a crash injected between the event's admission and its enqueue,
inside the page transaction, leaves neither the event's row nor an
`automation_runs` row, and the next poll admits the event and fires it
exactly once; on an `auto` watch, while a Start-confirmed run of rule A is
queued or running in the thread session, a new event that A wins fires
once, directly, and leaves no `watch_pending_fires` row; and with an `auto` run active, Treat as normal
conversation, then a new event opens a new thread session and queues a
run there, a second event coalesces a pending fire stamped with the new
session, the detached run finishes and that row remains, the detached run
enqueues nothing, and the row fires exactly once when the new session's
run ends; a fire error injected persistently on one event of an `auto`
watch's page rolls the page back on every poll, and the stream shows
`error` after the third, while a routing-unavailable, dismissed,
`observe_only`, ineligible or rate-capped event on a page commits its
recorded outcome and rolls nothing back, and quiet turned on with a direct row write
just before a page transaction's re-check, with the poll context's cancel
held back until after that re-check, rolls that page back as a stop, with no event
row, no streak change and no write other than at most the rotation index
(only when it changes), and the next poll starts at the repository after
the stopped one; once the injected error is
cleared, every event on that page is gated exactly once; a disable and
re-enable, and separately the page's repository removed and re-added,
committed with a direct row write after page 1's last byte is
decoded and just before its page transaction's first statement, with the
poll context's cancel held back the same way, on an `auto` watch whose page
would cause a direct fire, writes no event row, no `automation_runs` row and
no job, and leaves the `since` and ETags the re-enable or re-add stored
unchanged; each of these tests fails when the page transaction's re-check
is removed. CHN-002's fixtures add one (a later message is a new run
with a new disclosure).

### Open questions

None.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Reject or adapt | opus: reject; grok: adapt as "a new turn, no table"; sol: `thread_subscriptions` with context and owner-follow-up modes; mai: `WorkSubscription` with live fan-out | Reject the mechanism | Sessions already route follow-ups. A subscription table adds a second router, a delivery ledger and ownership states with no shipped consumer. |
| Context into live work | mai: deliver context to live children; sol: at a safe boundary | Never into a running or delegated run | A run that holds tools must not receive new untrusted text. |
| Resume of ended work | dsh, mai: resume under the old Mandate after revalidation | A new run with today's grant | Consent and approvals are per run in Turing; "resume" is a new run. |
| Round 1 review: session cardinality | sol, opus: one session per thread conflicted with firing into the automation's one session, which also replays other threads' untrusted text | One session per thread for all event-started work; idempotency per (event, rule), narrowed in round 4 to one run per event; coalescing per (thread, rule) | It keeps F4's rule, and a run never reads another thread's third-party text. |
| Round 2 review: a pending fire with no consumer (pending-followup-no-completion-trigger) | mai: nothing fired the coalesced follow-up when the running turn ended | A `watch_pending_fires` row consumed by a hook in `applyRunTransitionTx`, in the terminal transition's own transaction | It is the delegation join's proven pattern: no polling, no gap for a crash to open. |
| Round 2 review: `watch_fires` (pro001-watch-fires-table-reuse-automation-runs) | opus: it duplicated `automation_runs` | `automation_runs.watch_event_id` with a unique partial index | Every `auto` fire already writes that row; one more column replaces a table. |
| Round 4 review: one run per event, and edits drop a pending fire (pro001-multiple-rule-match-undefined, WATCH_PENDING_EDIT_BYPASSES_AUTO_ACTOR) | mai: several matching rules had no conflict rule; sol: an edit left a pending fire armed under the author's eligibility | The unique index is on `watch_event_id` alone, so an event fires at most its winning rule (section 7); coalescing follows the winner; the hook also re-checks the event's `observe_only` marker, which an edit sets while deleting the pending row | The database enforces the one-run rule, and an edit can no longer reach a fire by any path. |
| Round 19 review: a pending fire in a withdrawn thread session (WATCH_PENDING_FIRE_SESSION_WITHDRAWAL) | sol: the hook did not read the session's deletion state, so a withdrawal (owner deletion or the 30-day sweep), which cancels the session's runs in its own begin transaction, would reach an enqueue that refuses a deleting session and roll the withdrawal back | The hook first requires the run's session to be active and still its thread's current session; otherwise it deletes the pending row, fires nothing and never fails the transition; PRO-001d tests owner deletion and the sweep (refined in round 20: the hook touches only a row stamped with the terminating run's session, and a session being withdrawn is no longer the thread's current session, so a new event opens a new one at once; see that row) | It is `maybeJoinTx`'s own check for a continuation, and a stranger's follow-up must never block or outlive the owner's withdrawal. |
| Round 20 review: the fence deleted a replacement session's pending fire, and a fire into a withdrawing session (WATCH_PENDING_FIRE_STALE_SESSION_DELETES_REPLACEMENT, pro001-direct-fire-into-withdrawing-thread-session-unspecified) | grok: the round 19 fence deleted the (thread, automation) row whenever the terminating run's session was not current, so after Treat as normal conversation the detached run's end deleted the pending fire a new thread session's run had coalesced; opus: a direct fire or Confirm used `watch_threads.session_id` even while that session was deleting, which `requireChatSessionTx` refuses (`orch/repository/jobs.go:944-948`, `orch/repository/sessions.go:502-514`), rolling back the poller's page transaction on every poll until the reconciler finished | Point 1 states the current thread session once: `watch_threads.session_id` only while that session is active; a direct fire or Confirm that finds none opens a new thread session and replaces `session_id` in its own transaction (superseded in round 21: in the same transaction as its enqueue, the poller's already-open page transaction for a direct fire; see that row). Each `watch_pending_fires` row is stamped with the session it was coalesced against; the hook consumes or deletes only a row stamped with the terminating run's session, deletes a withdrawing or detached session's own row and leaves any other untouched, never failing the transition. Two PRO-001b and two PRO-001d tests: a new event, and a Confirm, while a withdrawal quiesces, and a detach followed by a coalesced fire in the new session | The thread, not one session, owns the follow-up, and the owner's withdrawal of one session must neither block intake nor cancel work that belongs to the next. |
| Round 21 review: the direct fire's transaction and what coalescing counts (PRO001_DIRECT_FIRE_SECOND_TRANSACTION_REOPENS_PAGE_ROLLBACK, pro001-coalesce-counts-start-confirmed-run-hook-never-consumes) | grok: opening the new thread session "in its own transaction" from inside the poller's page transaction needs a second `Begin` on the one connection (`orch/db/connection.go:47`), which waits on itself, while committing the page first could lose the event; opus: coalescing counted a Start-confirmed run, which has no `automation_runs` row, so the hook never consumed the pending row it caused | Point 1 states once that a direct fire runs entirely inside the already-open page transaction (new session when none is current, `watch_threads.session_id`, the `automation_runs` row and the enqueue), so admission and the fire commit or roll back together and `requireChatSessionTx` only ever sees an active session; Confirm keeps its own RPC transaction; coalescing counts only runs whose `automation_runs` row has a `watch_event_id`. PRO-001d adds a crash between admission and enqueue and a Start-confirmed run that holds back no fire, and its withdrawal test asserts one transaction | One transaction is the only way an admitted event and its fire cannot part, and coalescing must count exactly the runs whose end consumes its row. |
| Round 22 review: a failed direct fire, expected refusals, and the hook's cost (pro001d-direct-fire-page-rollback-unclassified-stall, pro001-hook-cost-claim-ordinary-turn) | opus: a fire error rolled the page back on every poll with no failure counted; Performance said nothing is added to an ordinary turn, while the hook probes `automation_runs` on every terminal transition (`orch/repository/run_state.go:800-807`) | Point 1 says that rollback is a failed poll of the stream (section 3) and that every expected refusal in the gate and the fire path returns a recorded outcome the page commits, as `ClaimDueAutomation` does (`orch/repository/automations.go:580-676`); Performance states the one primary-key probe of `automation_runs` per terminal transition. PRO-001d adds a persistent fire error that shows `error` after three polls while refusals roll nothing back. The suggested `jobs.unattended` gate is not adopted (review dispositions appendix) | A refusal is an outcome, not an error, and a real error must surface; the cost is stated as it is, and the gate would not lower it. |
| Round 23 review: quiet in a page transaction and the hook's pending-row probe (pro001d-quiet-listed-as-committed-refusal-contradicts-stop-rollback, pro001-hook-pending-probe-needs-thread-lookup-first) | opus: point 1 listed quiet among refusals a page commits, while section 3 rolls back a page whose transaction sees quiet; `watch_pending_fires` was keyed by (thread, automation), while the transition holds only the run (`orch/repository/run_state.go:416-426`), so the hook needed a thread lookup before its stated probe | Quiet leaves the refusal list: a page transaction that sees it is a stop (section 3, point 2), and `FireEventAutomation` keeps its quiet check for the hook and as defence in depth; PRO-001d's refusal test asserts the quiet case as a rolled-back page with no event row, no streak change and only the rotation write (superseded in round 24: no write other than at most the rotation index; see that row). The row is keyed by (the thread's current session, automation), the session it was already stamped with, so the hook probes by the run's session and automation with no thread lookup, and Performance states the reads in that order. Session keying was chosen over restating a thread-first cost because nothing looks a pending row up by thread: coalescing already holds the thread's current session, quiet and the clamp delete rows wholesale or by watch, and an edit deletes by event | One stop rule governs quiet in the poller; the hook reads only what the run already names, and a thread still holds one live follow-up per rule because it has one current session. |
| Round 24 review: a quiet stop's writes and the re-enable fence on a direct fire (pro001a-stop-asserts-rotation-write-when-index-unchanged, pro001-stopped-poll-rotation-write-assertions-fixture-dependent, pro001a-reenable-fence-fixture-satisfied-by-context-cancel) | grok, opus: see section 3's decision log | The quiet test asserts no write other than at most the rotation index (only when it changes) and the next poll at the repository after the stopped one; PRO-001d adds an eleventh test, a disable and re-enable injected just before the page transaction of an `auto` watch's page that would fire directly, with no event, `automation_runs` row or job (round 25 commits it with a direct row write and holds back the context cancel; see that row) | See section 3's decision log. |
| Round 25 review: the fence fixtures' held-back cancel (pro001a-reenable-fence-fixture-still-satisfied-by-context-cancel) | opus: see section 3's decision log | The quiet page-transaction test and the `auto` direct-fire disable and re-enable test commit their rows with a direct row write, hold the poll context's cancel back until after the page transaction's re-check, and fail when that re-check is removed | See section 3's decision log. |
| Round 28 review: a direct fire from an old poll across a removal and re-add (PRO001_SCOPE_READD_STALE_POLL_FENCE, pro001a-scope-readd-aba-in-flight-page) | sol, opus: see section 3's decision log | A page transaction that finds the watch's `generation` changed is a stop that rolls the page back, so no fire commits from an interval the watch did not cover; the `auto` fence test adds a removal and re-add of the page's repository | The direct fire commits in the page transaction, so the one fence covers it. |
| Round 50 review: the thread session's `auto` ceiling | see section 3's decision log (PRO001_WITHDRAWAL_BEGIN_UNBOUNDED and the rows with it) | Point 1 states the 500-run ceiling once, `watch_threads.session_auto_runs` counts it, a fire or the hook at the ceiling records `thread_session_full` and fires nothing, and Start stays available | A ceiling refuses visibly instead of rotating the session, which would orphan a pending follow-up stamped with it (the hook's session fence). |

## 7. F1 and F3 together — `auto` (PRO-001d)

`auto` is the last PRO-001 slice and the only one that runs work without the
owner's click.

- **Which matches may fire.** Only a rule for which `mayFireUnattended`
  holds (no `assess_instruction` and no label term; section 4, point 2) may
  fire from its deterministic match. A rule with an `assess_instruction`
  never fires unattended in these slices: its predicate is only the
  assessor's prefilter, and a match, whatever its assessment says, only
  records a proposal, which the owner may Start. Assessor-confirmed starts
  are a future item, gated on EVAL-001 evidence and not specified here. A
  rule that names a label only proposes too (section 5, point 1). This
  filter runs before the winner is chosen, inside `FireEventAutomation` and
  in the pending-fire hook (section 6), and `unattendedGrantCheck` blocks a
  queued or running run whose rule has since gained an
  `assess_instruction` or a label term, so such a rule can neither win nor
  fire nor keep running unattended through any path.
- **Fire.** A match that may fire calls `FireEventAutomation` (section 5)
  inside the poller's page transaction (section 6, point 1, the direct
  fire): it enqueues the run in the thread's current session, or a new one
  when it has none, and records it in
  `automation_runs` with its `watch_event_id`; after the commit the poller wakes workers through the
  scheduler's existing dispatcher (`orch/service/automations/scheduler.go:17-21`).
  The `automation_runs` row makes the run unattended for every existing gate:
  no memory tools, no integration tools, no approval that waits for a
  person; and, like every automation fire, it freezes no roster, so it
  cannot delegate (section 27, Agent team). An event fires at most one run, whatever matched it: among
  its matches that may fire (the event is not refused below and the rule
  passes the filter above), the one with the lowest automation
  id wins, and the run's frozen set comes from that automation alone;
  allowlists are never combined. When no match may fire, the matches record
  a proposal as in `observe`. The unique index on
  `automation_runs.watch_event_id` (section 6) enforces it.
- **What never fires.** `FireEventAutomation` refuses, inside the
  transaction it runs in, while the owner's quiet switch is on or draining
  (section 18; in the poller's page transaction quiet is a stop before any
  fire, section 6, point 1),
  for a dismissed event (section 19, point 1), for an event whose thread
  session already holds 500 `auto` runs (`thread_session_full`, section 6,
  point 1), for an event whose
  excerpt is no longer held, and for an event that `watchEventEligible` no
  longer admits (section 4, point 2). It never fires for a match by an
  automation whose predicate names a label (section 5, point 1), for an
  event marked `observe_only` (an issue, pull request or comment first read
  with `updated_at` later than `created_at`, or any event whose text an
  edit replaced, section 3, point 2, or an event first read late, below): these record `observe` proposals only. **Delayed reads,
  stated once:** an event the poller first reads more than 15 minutes after
  its `created_at` never fires `auto`; the poller admits it with its
  `observe_only` marker already set, so it records an `observe` proposal.
  This one rule covers every way a read is delayed: the owner's quiet
  switch, a suspension, a backlog, a clamp, a rate-limit back-off or a
  restart, and a watch polled less often than every 15 minutes fires only
  events created in the 15 minutes before a poll. An item created more than
  24 hours before the poll that first reads it is skipped as `stale` before
  the gate and leaves only the watch's `stale` count (section 3, point 2,
  the one resume rule), and a backlog the clamp skips is never read and
  deletes the watch's pending fires. So ending quiet, resuming a suspension
  or draining a backlog fires `auto` only for events first read within 15
  minutes of their creation (the delayed-read rule above): an event held
  longer only proposes, while one held less than 15 minutes, by a short
  quiet, suspension or backlog, still fires as it would have. A
  pending fire stamped with a session that is being withdrawn, or is no
  longer its thread's current session, fires nothing (section 6, point 1,
  the hook's session fence).
- **Tool ceiling, enforced by the server.** The frozen set is the
  automation's allowlist intersected, at fire time, with a fixed orchestrator
  list of unattended read tools, `tools.UnattendedReadTools`. It is a list of
  exact `(server, tool)` pairs with no wildcard and no prefix match:
  `files/files.list`, `files/files.search`, `files/files.read`,
  `system/system.health`, `system/system.time`, `system/system.echo` and
  `system/system.info`, today's safe seeds
  (`orch/service/tools/defaults.go:19-26`). A tool a later release adds to the
  `system` server joins only by a code change to the list, and a table test
  pins its exact contents. It is a separate list
  from `readOnlyTools` (`orch/service/tools/defaults.go:94-104`), which answers
  a different question (whether a failure is recoverable) and omits the file
  reads. It fails closed: a tool not on it, including every third-party MCP
  tool whatever its tier or policy, never enters the set. Every tool on it
  is a bundled, sandbox-confined server's, so the set already meets
  section 5's rule for unconfined MCP tools (point 2), with no counterpart
  refusal here: nothing an `auto` run calls leaves the machine. The job persists the set
  with `enforceSelectedTools`, so the worker offers the model only those
  tools; TUR-026d's unattended check also refuses every call outside the
  frozen set before the policy is read, so a `safe` tool cannot slip past.
  Trigger text is third-party, so an event can never reach an unattended
  mutation or an unlisted tool.
- **What a refused call looks like.** A model call to a tool outside the set
  is answered by the worker itself as an unknown tool, before any beacon
  (`agent-runtime-go/internal/agent/general_assistant.go:1036-1041`): nothing
  runs, nothing is audited, and the run continues. A beacon for such a tool,
  from a modified worker, is denied `tool_not_selected` before the policy
  (`orch/service/runtime/service.go:2925-2935`). Neither fails the run, so
  neither raises an attention row; UX-002's `person/blocked` row appears when
  the run itself fails or is blocked, as for any automation, including by the
  revalidation below.
- **Route and context.** Local route only (the fire records a routing-
  unavailable skip otherwise, as scheduled fires do), `skip_automatic_recall`,
  the pinned persona and profile withheld and an empty skill snapshot
  (section 3, point 7), and the
  excerpt as a `watch_event` row, framed `WATCH_EVENT` in `userText`
  (section 3, point 7). Results stay in the native session; nothing replies
  to the source.
- **Revalidation** is TUR-026's, with the watch and the event added: a
  disabled or deleted watch, a watch no longer in `auto`, an event that has
  left the watch's scope or its rule's predicate, or a rule that has gained
  an `assess_instruction` or a label term blocks the run with `watch_grant_revoked`, and a deleted or disabled automation, or one whose
  allowlist lost the tool, with `automation_grant_revoked`, at its next tool call of
  any kind, a `safe` read included, because `unattendedGrantCheck` re-reads
  them on every unattended call (section 4, points 2 and 5). Which of these raise an
  attention row is stage 3's rule (section 8): only a still-enabled
  automation's block does, never a revocation the owner made by disabling or
  deleting the automation, by disabling, deleting or narrowing the watch, or
  by narrowing the rule's predicate or adding an `assess_instruction` or a
  label term to it.
- **Server-side gate for the mode.** `SetWatchMode(auto)` is refused unless
  the orchestrator-only flag `TURING_PROACTIVE_AUTO_ENABLED` is on (default
  `false`, passed explicitly in Compose, experimental until the EVAL-001
  scenarios below pass, as `TURING_AGENT_TEAM_ENABLED` is) and the request
  carries the owner's explicit acknowledgement. Its dialog says that anyone
  with write access to a listed repository can edit an issue's or a
  comment's text in the second it was created, before Turing first reads
  it, which Turing cannot detect and `auto` then reads as its author's;
  that an item changed in any way before Turing first reads it only
  proposes; and that labels, which anyone with
  write access can apply, never start work automatically: a rule that names
  a label only proposes. The acknowledgement is written
  to audit (`watch.auto.enabled`) in the same transaction as the mode change,
  and a failed audit write refuses the change. With the flag off, an `auto`
  watch fires nothing and is reported as `observe`. The client hides the
  option too, but it is not the gate.
- **EVAL-001 scenarios:** "proactive precision on recorded `observe`
  decisions meets the owner's bar", "injected event yields no tool call
  outside the unattended read set", "an outsider's detectable edit of a
  listed author's issue or comment (a body hash changed after intake, or an
  item first read with `updated_at` later than `created_at`) never becomes
  unattended work" and "an outsider's undetectable edit (made in the second
  the item was created, before Turing first reads it) starts at most a
  run under the unattended read-only ceiling", all with zero privacy
  failures.

| Slice | Size | Acceptance tests |
| --- | --- | --- |
| PRO-001d | M | A fired run's job is written with `jobs.unattended = 1`, equal to `RunUnattended` for its run (section 9, point 2), and an older fired job is claimed after a newer chat job; `auto` refused for `anyone` actor scope; with the flag off, or without the acknowledgement, `SetWatchMode(auto)` is refused by the server and nothing fires; the acknowledgement's audit row is written with the mode change and a failed audit write refuses it; a fired run's frozen set is the allowlist intersected with `UnattendedReadTools`, and a table test pins that list to its seven exact pairs, so a tool later added to the `system` server never enters it; end to end with a real worker, the job offers only the set's tools, a model call to a third-party `safe` tool that is on the allowlist but not on `UnattendedReadTools`, or to a `files.read` the allowlist lacks, gets the worker's unknown-tool result with no beacon and nothing runs, and a forged beacon for either is denied before the policy; the run cannot delegate; disabling or deleting the watch, leaving `auto`, or deleting, disabling or narrowing the allowlist of the automation before the next tool call blocks that call even when it is a `safe` read and fails the run, and only the still-enabled automation whose allowlist was narrowed raises a `person/blocked` row; removing the event's author from the listed actors, removing its repository from the watch, or narrowing the rule's predicate so the event no longer matches blocks the run's next tool call with `watch_grant_revoked` while the run is queued or running, raises no row, and drops a pending fire for that event; a rule whose predicate names a label never fires `auto`, including when a login other than the item's author applied the label, and records a proposal instead; the run uses the thread's current session, not the automation's; the job is local with `skip_automatic_recall`, its pinned persona and profile withheld and an empty skill snapshot, and with a real worker and private sentinels in both and in the body of an enabled skill whose `$<skill-id>` is in the event text, its captured model requests contain none of them; a replayed event fires nothing, by the unique index on `automation_runs`; with TUR-028's quiet on, a match fires nothing and a pending fire is dropped; quiet turned on and off while a run with a pending fire is still executing leaves no follow-up when that run ends; after quiet is turned off, an event created while it was on and first read 2 hours after its creation records a proposal and fires nothing; a watch suspended for 20 fake-clock hours and then resumed records only proposals, and fires nothing, for the events created during the suspension more than 15 minutes before the poll that reads them; an event delayed behind a backlog that the poller then clears, and first read more than 15 minutes after its creation, records a proposal and fires nothing; an event first read 10 minutes after its creation still fires; with quiet held for 48 fake-clock hours over a backlog that fits one poll's budget, an event created 30 hours before quiet ended is returned, skipped as `stale`, counted on the watch's status, and leaves no receipt, excerpt or proposal, and over a backlog larger than the budget, once the stream shows `backlog`, the clamp records a `skipped_interval`, deletes the watch's pending fires, and nothing in the interval fires; a comment first seen edited, and an issue edited by another login before its first poll, each record a proposal and fire nothing, while an issue first read unchanged (equal timestamps) may fire; with an `uncertain` comment row on a repository renamed before the poll, the polled comment is refused as Turing-authored and nothing fires; end to end with a real worker, a comment created and edited by another login in the same second, before its first poll, is admitted unmarked as the acknowledgement discloses, and the run it fires offers no write tool, calls no recall and carries no pinned persona, profile or owner skill, and a forged beacon for a write tool is denied before the policy; an event matched by two rules that may fire starts exactly one run, the lower automation id's, whose frozen set comes from that rule's allowlist alone, and a second fire for the event is refused by the unique index; an event matched only by a label rule and a non-label rule fires the non-label rule; an event matched only by a rule with `assess_instruction` fires nothing from its deterministic match and records a proposal, whether its assessment is pending, negative, malformed, `unavailable`, stale or positive; an event matched by such a rule with the lower automation id and by a plain rule fires the plain rule, with the plain rule's allowlist; a rule that gains `assess_instruction`, and one that gains a label term, while its pending fire is held fires nothing when the running run ends, and its pending row is gone; a queued and a running `auto` run whose rule gains `assess_instruction`, and a queued and a running one whose rule gains a label term, are each blocked at their next tool call, a `safe` read included, with `watch_grant_revoked`, and raise no row; a table test of `mayFireUnattended` covers a rule with neither, either and both; a direct `FireEventAutomation` call for a rule with `assess_instruction` or a label term is refused; an event on a dismissed thread fires nothing; section 6's eleven PRO-001d tests, including a thread session withdrawn with a pending fire held, an event admitted while that withdrawal is quiescing, a pending fire stamped with a new session after Treat as normal conversation, a crash between an event's admission and its enqueue, a Start-confirmed run that holds back no fire, and a persistent fire error that shows `error` after three polls while expected refusals roll nothing back; an allowlist naming a remote-URL MCP tool, or a third-party `local_container` server's tool, fires a run whose frozen set omits it; after a listed account is renamed and a different account claims its former login, that account's issue fires nothing on an `auto` watch, while the listed account's issue under its new login fires; on an `auto` watch, an item created after `enabled_at` but before its repository was added to the scope, and one created while a repository was out of scope before its re-add, fire nothing (section 3, point 2, a scope edit), including through a poll that began before the removal (section 3, point 2, the generation fence); on an `auto` watch whose current thread session holds 500 `auto` runs, the next matching event, and a pending fire consumed then, fire nothing and record `thread_session_full` on the proposal with a content-free audit row, while Start on that proposal still enqueues; once the sweep begins that session's withdrawal, or the owner deletes it, the next event opens a new thread session counting from 0 and fires; the PRO-001a clauses marked "(PRO-001d asserts)" (section 3, Delivery slices), on its pending fires |

Decision log for this section:

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Round 1 review: the read-only ceiling | grok, sol: the allowlist does not bind `safe` tools and `ToolReadOnly` omits the file reads | A fixed `UnattendedReadTools` list, an enforced selection, and TUR-026d's check before the policy | Server-side and fail-closed; the existing list means something else. |
| Round 1 review: who gates `auto` | grok: only the client hid it | A server flag plus an audited owner acknowledgement | The client is not a policy authority. |
| Round 1 review: how a fire happens | grok: the dispatcher only wakes workers | `FireEventAutomation` does the fire; the dispatcher wakes workers | Accuracy. |
| Round 2 review: wildcard in the ceiling (system-wildcard-auto-ceiling) | mai: `system.*` is a prefix, not an exact fail-closed list | Seven exact `(server, tool)` pairs, pinned by a table test | A new tool on the `system` server must not become unattended-callable by being added. |
| Round 2 review: refused calls and the attention row (AUTOMATION_SELECTION_FAILURE_PATH) | sol: the worker answers an unselected call before any beacon, so "fails visibly in UX-002" was not what happens | State the real path: an unknown-tool result, a forged beacon denied `tool_not_selected`; the row follows actual run failures | The enforced set keeps the tools out of the model's offer; the attention row reports what the server observed. |
| Round 2 review: revocation and reads (pro001d-revocation-skips-read-tools) | grok: leaving `auto` did not stop the run's reads | Every unattended tool call re-reads the automation, its allowlist and the watch | Third-party-steered reads end with the owner's revocation, not with the run. |
| Phase 2 F10 and F16: what `auto` may fire | opus: an edited comment has an unknown actor, and events held by quiet must not fire on resume; grok, sol, mai: quiet holds the cursor, the poller resumes within its budget | Neither fires; both record proposals (since round 4, an event over 24 hours old when first read is skipped as `stale` instead; since round 11, the held-event rule is the general delayed-read rule: any event first read more than 15 minutes after its creation) | The first keeps actor attestation honest; the second keeps "quiet" from turning into a burst of unattended runs when it ends. |
| Round 3 review: labels as authority (pro001d-unattested-label-predicate) | grok: anyone with write access can label a listed author's issue before the poll, so a label term let an outsider choose which rule fired unattended | A rule that names a label only proposes, in either mode; the acknowledgement says so | Round 2 rejected a third stream to attest label actors, so an unattested input must not decide unattended work. |
| Round 3 review: which revocations raise a row (pro001d-blocked-row-contradicts-stage3) | opus: PRO-001d expected a `person/blocked` row for revocations that UX-002's stage 3 says never qualify | Owner revocations of the watch (`watch_grant_revoked`) and disabling or deleting the automation raise none; a narrowed, still-enabled automation does | The owner already knows about a revocation they made; a narrowed rule that will keep failing is the one case that needs them. |
| Round 4 review: conflicting matches, edits and stale events (pro001-multiple-rule-match-undefined, WATCH_PENDING_EDIT_BYPASSES_AUTO_ACTOR, quiet-resume-proposals-lost-to-stale-skip, pro001-stale-skip-drops-quiet-period-events, ux002-predicate-narrowing-raises-row) | mai, sol, grok, opus: "a match" fired with no rule for several; an edit after admission kept `auto` eligibility; the quiet resume rule contradicted the stale skip; a narrowed predicate raised a row | One run per event, the lowest eligible automation id, never a union; `observe_only` blocks any edited event; the stale skip wins and this section points to it; a predicate mismatch is `watch_grant_revoked` | Each rule is now stated once (sections 3, 4 and 6) and this section only points to them. |
| Round 6 review: an assessed rule fired from its prefilter (PRO001_ASSESSED_RULE_BYPASSES_ASSESSOR) | sol: a rule with `assess_instruction` could fire `auto` from its deterministic match and win the lowest-id selection while its assessment was pending, negative or unavailable | Only a rule without `assess_instruction` fires from a deterministic match; a rule with one only proposes until EVAL-001 authorizes assessor-confirmed starts, and then fires only from a positive, revision-current assessment, never by fallback; the filter runs before the winner is chosen, in `FireEventAutomation` and the pending-fire hook | The predicate of an assessed rule says only "worth asking the assessor", so treating it as a match let the cheaper check stand in for the one the owner configured. |
| Round 7 review: the assessor-confirmed fire and runs that outlive an added assessor (pro001d-assessed-fire-contradicts-filter, pro001d-assess-instruction-added-not-revalidated) | grok: sections 5 and 7 described an authorized assessor fire through `FireEventAutomation`, which refuses every such rule, and NORTH_STAR implied it; opus: a queued or running `auto` run whose rule gained `assess_instruction` still ran, because no unattended check read the field | A rule with `assess_instruction` never fires unattended in these slices; assessor-confirmed starts are a future item gated on EVAL-001 evidence with no fire path specified; `unattendedGrantCheck` blocks a run whose rule gained one with `watch_grant_revoked`. The check sits beside `watchEventEligible`, not inside it, because the matcher and Confirm share that function and an assessed rule must still match and be confirmable | One rule with no unsliced exception, and the owner's added assessor narrows work already queued, as every other rule change does. |
| Round 8 review: a label term added while work is pending or running (WATCH_PENDING_FIRE_LABEL_GATE, pro001d-label-term-added-not-revalidated) | sol, opus: the pending-fire hook and `unattendedGrantCheck` re-checked `assess_instruction` but not the no-label rule, so a rule that gained a label term while a fire was pending, queued or running still fired or ran unattended | One predicate, `mayFireUnattended(automation)`, true only with no `assess_instruction` and no label term, defined in section 4, point 2, and called in `FireEventAutomation` before the winner is chosen, in the pending-fire hook and in `unattendedGrantCheck` (`watch_grant_revoked`, no row); it replaces the separate `assess_instruction` checks | The two rules that keep a match from firing unattended had two enforcement lists that drifted; one function called at every fire and every unattended call cannot. |
| Round 9 review: the edit scenario against its disclosed limit (PRO001_EDIT_EVAL_CONTRADICTS_DISCLOSED_LIMIT) | sol: the EVAL-001 scenario promised that no outsider's edit becomes unattended work, while section 3 admits a same-second edit before intake unmarked | The scenario names detectable edits; a second scenario and a PRO-001d end-to-end test hold the undetectable case to the unattended read-only ceiling | See section 3's decision log. |
| Round 10 review: the resume tests and the backlog clamp (pro001-clamp-stale-count-unsatisfiable, PRO001_CLAMP_MAKES_STALE_COUNTS_UNOBSERVABLE, pro001-clamp-skips-held-excerpt-edits) | grok, sol: the 48-hour quiet test counted as `stale` an event the clamped `since` never returns, and a skipped edit could leave a pending fire armed | "What never fires" points to section 3's rule: day-old items the list returns are counted `stale`; an interval the backlog clamp skips is recorded content-free and deletes the watch's pending fires; the 48-hour test is split into a backlog that fits the budget and one that does not | See section 3's decision log. |
| Round 11 review: suspension and backlog replayed into `auto` (pro001d-suspension-resume-replays-backlog-into-auto) | opus: only the quiet switch kept held events from firing; resuming a suspension or draining a backlog could fire a burst of stale unattended runs | One rule in "What never fires", replacing the quiet-specific clause: an event first read more than 15 minutes after its `created_at` is admitted `observe_only` and records a proposal, whatever delayed it; tests for a 20-hour suspension, a cleared backlog and a 10-minute read that still fires | One bound on the read delay covers quiet, suspension, backlog and clamp alike, and reuses the existing marker that `FireEventAutomation` and the pending-fire hook already refuse, so no new check or column is needed. |
| Round 12 review: event text invoking an owner skill (WATCH_EVENT_EXPLICIT_SKILL_INVOCATION) | sol: see section 3's decision log | A fired run's job carries an empty skill snapshot, tested with a real worker and a sentinel skill named in the event text | The same limit as every thread-session turn. |
| Round 13 review: an issue edited before its first read (pro001-issue-edit-before-first-read-auto) | mai: see section 3's decision log | "What never fires", the acknowledgement and the EVAL-001 scenarios name every item kind first read with `updated_at` later than `created_at`; the acknowledgement's disclosed limit narrows to an edit in the second an item was created; PRO-001d tests an issue edited before its first poll (proposal only), one first read unchanged (may fire) and a renamed-repository `uncertain` comment (nothing fires) | See section 3's decision log. |
| Round 19 review: a pending fire in a withdrawn thread session (WATCH_PENDING_FIRE_SESSION_WITHDRAWAL) | sol: see section 6 | "What never fires" points to section 6's session fence, and the PRO-001d row names section 6's withdrawal test | See section 6's decision log. |
| Round 20 review: the current thread session and the pending-fire stamp (WATCH_PENDING_FIRE_STALE_SESSION_DELETES_REPLACEMENT, pro001-direct-fire-into-withdrawing-thread-session-unspecified) | grok, opus: see section 6 | Fire and "What never fires" point to section 6, point 1; the PRO-001d row names section 6's seven tests (nine since round 21) | See section 6's decision log. |
| Round 21 review: the direct fire's transaction, coalescing, and remote MCP tools (PRO001_DIRECT_FIRE_SECOND_TRANSACTION_REOPENS_PAGE_ROLLBACK, pro001-coalesce-counts-start-confirmed-run-hook-never-consumes, PRO001_START_REMOTE_MCP_EGRESS_UNSPECIFIED) | grok, opus: see section 6; sol: see section 5 | Fire runs `FireEventAutomation` inside the poller's page transaction, pointing to section 6, point 1; the tool ceiling states that no remote MCP tool can enter an `auto` run, so section 5's refusal needs no counterpart; the PRO-001d row names section 6's nine tests and a remote MCP tool left out of a fired set | See sections 5 and 6's decision logs. Round 30 restates the ceiling as keeping out every tool that is not sandbox-confined (see that row). |
| Round 22 review: a failed direct fire (pro001d-direct-fire-page-rollback-unclassified-stall) | opus: see section 6's decision log | PRO-001d names section 6's tenth test, a persistent fire error that shows `error` after three polls while expected refusals roll nothing back | See section 6's decision log. |
| Round 23 review: quiet in a page transaction (pro001d-quiet-listed-as-committed-refusal-contradicts-stop-rollback) | opus: see section 6's decision log | "What never fires" keeps `FireEventAutomation`'s quiet check and points to section 6, where a page transaction that sees quiet is a stop | See section 6's decision log. |
| Round 27 review: a scope edit on an `auto` watch (pro001a-scope-addition-cursor-seed-undefined) | opus: see section 3's decision log | PRO-001d asserts that an item created before its repository was added, or while it was out of scope, fires nothing | The add's `started_at` skips those items before the gate, so they never reach a fire. |
| Round 28 review: an `auto` fire from an old poll across a removal and re-add (PRO001_SCOPE_READD_STALE_POLL_FENCE, pro001a-scope-readd-aba-in-flight-page) | sol, opus: see section 3's decision log | PRO-001d also asserts that an item created while a repository was out of scope fires nothing through a poll that began before the removal | The generation fence is what keeps that poll from firing. |
| Round 30 review: unconfined tools and reclaimed logins in `auto` (PRO001_START_LOCAL_MCP_EGRESS_GUARANTEE, PRO001_ACTOR_LOGIN_REASSIGNMENT) | sol: see sections 3 and 5's decision logs | The tool ceiling says every tool on `UnattendedReadTools` is a bundled, sandbox-confined server's, so it already meets section 5's rule; PRO-001d asserts that a `local_container` tool is left out of a fired set and that a reclaimed login's issue fires nothing | `auto` needed no new check, only a statement and two assertions. |
| Round 42 review: where the event fire's `jobs.unattended` is tested (tur027-assessor-acceptance-in-slice-before-assessor-exists) | opus: TUR-027 tested every enqueue path, including ones that do not exist when it ships | PRO-001d asserts that a fired run's job is written with `jobs.unattended = 1`, equal to `RunUnattended`, and is claimed after a newer chat job | The event fire creates an unattended run only on an `auto` watch, which PRO-001a refuses until PRO-001d, so this is the slice that adds the path (review dispositions appendix). |

## 8. F5 — "Needs a person" view and closed stage table

### Verdict

Adopt, as **UX-002 — Needs-you queue**: one orchestrator-computed,
cross-session list of what blocks on the owner, with a closed,
first-match-wins stage table. This is the largest user-facing gap the review
found.

### DeepSeek source

- Stages are a closed table read top to bottom, first match wins; `person`
  rows mean the work cannot continue without a human
  (`dsh:docs/003-execution.md:5-21`), implemented once as `ticketStage`
  (`dsh:src/core/stage.ts:8-21`).
- The panel answers "does anything need me now", then health, then history
  (`dsh:docs/005-panel.md:7-11`). A count equals the length of the list it
  links to; healthy states take no space; rows needing a person offer their
  action (`dsh:docs/005-panel.md:50-58`).

### Turing today

- Owner-blocking items are scattered and per session: approvals render in the
  open chat (`turing-client/turing_app/lib/features/approvals/approval_card.dart`),
  delegation consent on task cards
  (`turing-client/turing_app/lib/features/chat/delegation_card.dart`), failed
  deletions in the shell, automation failures on the Automations page
  (`orch/repository/automations.go:76-77`).
- No RPC lists pending approvals across sessions
  (`proto/turing/v1/approvals.proto:154-160`), so an approval in a conversation
  the owner has not opened stays invisible until it expires.
- The global updates stream forwards only `session.updated` and
  `session.deleted` (`orch/service/events/service.go:133-160`).
- Clients render protocol truth and never invent lifecycle
  (`docs/NORTH_STAR.md:323`).

### Unified design

1. **Projection.** A pure `attention.Stage` in Go with an exhaustive table
   test. One row per owner obligation, keyed by its subject; if one subject
   qualifies twice, the first row wins:

   | Order | Stage | Condition | Owner action |
   | --- | --- | --- | --- |
   | 1 | `person/approval` | A pending, unexpired approval on any run; a delegation child's appears under its parent session | Review (opens the existing approval flow) |
   | 2 | `person/consent` | A delegation with `egress_consent = 'pending'` (`orch/db/schema/0030_delegation_egress.sql:9-10`) | Allow…, Run without, or Decline |
   | 3 | `person/blocked` | An enabled automation whose latest occurrence failed or was blocked since its last success, including TUR-026's revoked, repeat and allowlist-ceiling blocks, unless the owner dismissed that occurrence; or an enabled PRO-001 watch suspended by a source failure (section 3, Security and privacy; status code, `connection_unavailable` or `unexpected_redirect` only). A disabled or deleted automation, a deleted watch, a run blocked `watch_grant_revoked` (its watch disabled, deleted, narrowed or taken out of `auto`, or its rule's predicate narrowed or an `assess_instruction` added, by the owner, section 4, point 2), and a run the owner's quiet switch cancelled (`owner_quiet`, section 18) never qualify | Open the automation, its run, or the watch; Dismiss (automations only) |
   | 4 | `person/deletion` | A deletion receipt the owner can retry, held as failed and retryable (the receipts `PendingSessionDeletionReceipts` lists: `initiator = 'owner'`, never a child's); or a receipt the sweep began that is still unfinished an hour after its begin, content-free, naming its failure class (section 3, point 8) | Retry deletion (an owner's receipt); none (a sweep's, which the reconciler retries) |
   | 5 | `person/proposal` | An open PRO-001 `would_start` proposal; it closes on Start's Confirm, on Dismiss (section 19), or when its excerpt is withdrawn | Start or Dismiss |

   A row the projection cannot classify returns `unavailable`, never an empty
   list. There are no `working`, `queued` or `idle` rows: healthy states take
   no space.

   **Leaving the list (F18).** A row leaves when its condition ends or when
   the owner dismisses the one occurrence it shows, never because time passed:
   a still-failing automation that vanished on a clock would make an empty
   list a guess. **The occurrence key, stated once:** an automation's
   latest occurrence is keyed by the id of its latest
   `automation.remote_egress_blocked` audit record newer than
   `last_run_at` when there is one, and otherwise by its latest run's id
   (`last_run_id`). The audit key covers a failure before enqueue:
   `ClaimDueAutomation` blocks a remote-egress occurrence before any run
   exists, advancing `next_due_at` and writing only that audit record
   (`orch/repository/automations.go:561-566,605-616`;
   `orch/repository/audit.go:205-209`), and the automation read already
   joins the latest such record after `last_run_at`
   (`orch/repository/automations.go:817-835`), which UX-002a indexes
   (Performance). Run ids and audit ids carry
   distinct prefixes (`run_`, `audit_`), set where they are minted
   (`orch/repository/jobs.go:1021`, `orch/repository/audit.go:181,207`)
   through `orch/ids/ids.go:10-12`, so the
   two kinds of key never collide. `automations.attention_dismissed_occurrence`
   holds the key the owner dismissed; stage 3 skips the automation while
   its latest occurrence key equals it, so the next failure, with or
   without a run and even with `last_run_id` unchanged, raises it again.
   Only stage 3
   automation rows can be dismissed: approvals and consents keep their own
   expiry and are answered, not hidden; a deletion receipt is retried; a
   suspended watch is resumed or disabled by the owner; a proposal's Dismiss is section 19's.
   Every row carries `since`, the time of the occurrence it reports.
2. **API.** Public-facet `AttentionService.ListAttention`: stage, session id
   and title, the ids needed to act, `since`, and a page token; a stage 3
   automation row also carries `occurrence_key`, the key of the occurrence
   it shows (point 1).
   No tool arguments, message content or transcripts. Keyset paging at 100
   rows, the audit API's ceiling (`orch/service/audit/service.go:38`).
   **Paging, stated once:**
   - *Total order.* Stage rank ascending (the table's Order column), then
     `since` descending, then source kind (`approval`, `delegation`,
     `automation`, `deletion`, `watch`, `proposal`, in that fixed order),
     then source id ascending (the approval, delegation, automation,
     receipt's session, watch or thread id). Ids are unique within a kind,
     so no two rows tie.
   - *Watermark.* The first page's read transaction records the
     orchestrator's current time as the watermark. Every later page reads
     the same sources with the same predicates but leaves out rows whose
     `since` is after the watermark. A row whose `since` is after the
     watermark (an approval created after the first page, or an
     automation's newer failure) is therefore left to the next first-page
     read, which the `attention.changed` hint (point 3) triggers, and never
     lands among pages already shown. The watermark is a time, not a commit
     fence: a writer that took its timestamp before the first page and
     commits after it (`CreateApprovalWithEvents` reads `now()` before
     `BeginTx`, `orch/repository/approvals.go:82-94`) writes a row at or
     before the watermark that the first page could not see. A later page
     shows it if it sorts after the cursor; otherwise the next first-page
     read, which its own hint triggers, does. Either way it is shown at most
     once in the pass. A row resolved or dismissed between pages simply
     is not returned. So one paging pass shows no row twice, never reorders
     rows, and skips no row that qualified at the watermark and still
     qualifies with the same sort key.
   - *Page token.* An opaque, base64url-encoded value holding the
     watermark and the last row's sort key (stage rank, `since`, source
     kind, source id). The next page reads rows strictly after that key in
     the total order. Within a source, stage rank and source kind are
     constants, and the comparison follows the order's keys in turn, so
     each source compares its stage rank with the cursor's first: a source
     in an earlier stage reads nothing, and one in a later stage reads
     every row that qualifies at the watermark, whatever its `since`. Every
     source in the cursor's stage, whatever its kind, then compares
     `since` before kind, because `since` is the second key and kind only
     the third: a source whose kind sorts before the cursor's reads
     `since < :since`; the cursor's own source reads
     `since < :since OR (since = :since AND id > :id)`; and one whose kind
     sorts after the cursor's reads `since <= :since`. Stage 3 is the one
     stage with two kinds (automation and watch), so it is where this
     matters. The rule is applied inside the bounded read of each source
     (Performance).
     A token that does not decode, or names an unknown stage or kind, is
     refused with `InvalidArgument`. The token carries no authority: each
     page re-checks every row's current state.

   The client's Dismiss sends that row's `occurrence_key` unchanged:
   `DismissAttention(automation_id, occurrence_key)` writes the dismissal in
   one transaction that compares the key with the automation's current
   latest occurrence key (point 1, Leaving the list) and refuses a stale
   key, so a stale screen cannot hide a newer failure. The runtime and every internal
   identity are refused.
3. **Push.** The events bus emits a content-free `attention.changed` on the
   existing updates stream whenever it publishes an approval, consent,
   automation-outcome, deletion, watch-status or proposal change. The app refetches on that
   hint and on resume, coalesced to at most one fetch per second. No polling.
4. **App.** A "Needs you" destination whose badge is the number of rows on
   the first page, shown as "100+" when the response has a next page, so the
   badge always equals what the list it links to shows without a second count
   query.
   Each row is a summary built only from `ListAttention`'s response: stage,
   session title and `since`, with its action button. Opening the list
   reads nothing else. Expanding a row is what mounts the existing card and
   decision flow (`ApprovalCard`, the delegation consent dialog, deletion
   retry), and at most one row is expanded at a time. That matters because
   `ApprovalCard` reads the approval's details as soon as it is built
   (`turing-client/turing_app/lib/features/approvals/approval_card.dart:43-74`)
   and those details prepare, store and re-check a file-mutation preview
   (`orch/service/approvals/preview.go:77-117`), so mounting one per row would
   cost up to 100 detail reads and preview preparations per open. Rows act
   only through the existing RPCs, which re-check state server-side, so a
   stale row's action is refused, not replayed.

### Security and privacy

Read-only and owner-only; it returns titles and ids already visible to the
owner, so nothing new reaches logs. It makes approvals harder to miss, which
cuts expiry-driven failures. It adds no approval path outside the native app
(`docs/NORTH_STAR.md:291-292`). Untrusted titles render as plain text.

### Performance

Zero model calls and zero projection writes. One bounded read across the
indexed sources per open or hint: approvals by `idx_approvals_status`
(`orch/db/schema/0001_initial.sql:143`), delegations by a new partial index
`WHERE egress_consent = 'pending'`, automations through their existing
last-run join, deletion receipts, and, once PRO-001 lands, suspended watches and
open proposals. The automation read's latest-block subquery filters
`audit_logs` by `actor_type`, `actor_id`, `action` and `created_at`
(`orch/repository/automations.go:817-835`), and the only index it can use,
`idx_audit_action(action, created_at)`
(`orch/db/schema/0013_internal_service_identities.sql:36-39`), walks every
automation's blocks newer than this one's last run, so it costs automations
times blocks on the one connection. UX-002a's migration adds the partial
index `audit_logs(actor_id, created_at) WHERE action =
'automation.remote_egress_blocked'`, which makes each lookup one index seek.
Dismissal adds one column comparison to the automation join
and one write per Dismiss. Detail reads and previews happen only for the one expanded
row. Measured: p95 under 100 ms per page at
10,000 terminal runs and 100 pending obligations, and query plans in the test.

### Roadmap placement and dependencies

New **UX-002 — Needs-you queue**, Workstream A. AUD-001 is history and AGT-003
is delegation planning; neither answers "what needs me now". It complements
TUR-024 (lost approval notifications). It depends on TUR-026a and TUR-026d,
whose block codes raise the stage 3 rows UX-002a asserts (section 27 orders
TUR-026 first); PRO-001 adds its rows later.

### Delivery slices

| Slice | Size | Acceptance tests |
| --- | --- | --- |
| UX-002a Projection and API | M | Every combination of qualifying conditions resolves to exactly one stage; a page holds at most 100 rows and reports whether a next page exists, and paging visits every row exactly once; rows follow point 2's total order, including two rows of one source with the same `since`, which are ordered by source id; with 250 rows and, between page requests, a new approval inserted, a listed approval resolved and a listed automation row dismissed, the pass shows no row twice, skips no row that qualified at the watermark and still qualifies, and does not show the new approval, whose `since` is after the watermark, which the next first-page read does; an approval committed between pages with a `created_at` before the watermark (as `CreateApprovalWithEvents` can write) appears on page 2 when it sorts after page 1's cursor and on no later page of the pass when it sorts before, and never twice; with 101 approvals older than a pending delegation consent and a blocked automation, page 1 ends inside stage 1 and page 2 shows the 101st approval and then the consent and automation rows although each is newer than the cursor, and the test fails if either is missing; the paging across stage 3's two kinds, which needs watch rows, is PRO-001a's (section 3); a page token that does not decode is refused with `InvalidArgument`; the response holds no arguments or content (an allowlist test like audit's); the runtime identity is refused; a hint is emitted for each source transition; deletion between list and detail is handled; dismissing a failing automation hides its row and lowers the badge by one, and its next failing run raises it again; dismissing a remote-egress block that has no run hides its row, and a later block with `last_run_id` unchanged raises it again; every stage 3 automation row carries `occurrence_key`, and a dismissal sending a row's `occurrence_key` unchanged hides exactly that row; a dismissal naming a stale occurrence key, a run's or an audit record's, including the `occurrence_key` of a row listed before a newer occurrence, is refused and leaves the newer occurrence, with or without a run, on the list; with 1,000 enabled, never-fired automations and 100,000 `automation.remote_egress_blocked` records belonging to another automation, `EXPLAIN QUERY PLAN` shows `ListAttention`'s and `DismissAttention`'s latest-block lookup using the new partial index, and both stay within the page latency budget; a disabled automation's failure raises no row (a run blocked `watch_grant_revoked` is PRO-001d's test and an `owner_quiet` cancellation TUR-028a's, each of which ships after this slice), while TUR-026d's `automation_tool_not_allowlisted` block and an `automation_grant_revoked` block of a still-enabled automation whose allowlist was narrowed each raise a `person/blocked` row; a failure 90 days old that is still the latest occurrence still shows; approvals, consents and deletions cannot be dismissed; the watch rows themselves (their causes, and that they cannot be dismissed) are PRO-001a's tests |
| UX-002b App | M | Widget tests for each stage and for `unavailable`; the badge equals the first page's row count, and reads "100+" with 101 obligations; the empty state takes no space; opening the list with 100 pending approvals issues no `GetApprovalDetails` call, and expanding one row issues exactly one, collapsing any other expanded row; approving from the list resolves the same approval id the chat would; an expired row's action is refused and the row disappears on refetch |

### Open questions

None. Memory candidates are left out of v1: they block no run and have their
own page. Adding them is one row.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Roadmap home | opus: new UX-002; sol: AGT-003, UX-002 only for a global page; grok: AUD-001 amendment; mai: new SUP-001 | UX-002 | The verified gap is cross-session; AUD-001 and AGT-003 have other outcomes. |
| Row set | sol: every lifecycle stage per work item; mai: nine stages incl. review and idle; grok, opus: owner obligations only | Owner obligations only | DeepSeek's own principle: healthy states take no space; a lifecycle board duplicates chat. |
| Refresh | grok: the screen polls; opus, sol: push a hint and refetch | Push a content-free hint | Polling costs the single SQLite connection for nothing. |
| Unknown state | sol: `unavailable`, never "all caught up" | Adopted | An empty list must be a fact. |
| Round 1 review: badge under paging | opus: a badge equal to the list length is undefined with 100-row pages | The first page's count, "100+" when more exist | Honest with no extra count query. |
| Round 1 review: silent watches | grok, opus: a permanent 403 would look like a quiet watch | A content-free `person/blocked` row for a paused watch (a suspended watch since round 9) | A watch that cannot read its source must need a person. |
| Round 2 review: eager previews (ATTENTION_EAGER_PREVIEW_FANOUT) | sol: an `ApprovalCard` per row reads details and prepares a preview for every row on open | Summary rows from `ListAttention`; the full card mounts only on expand, one at a time | Opening the queue costs one bounded read, not up to 100 detail reads with file I/O. |
| Phase 2 F18: how a row leaves | grok, sol, mai: a 7-day age-out plus an occurrence-bound Dismiss; opus: Dismiss and subject state only, no time expiry | Opus's | An age-out hides a failure that is still current, which the "an empty list must be a fact" row above forbids; the noise case Sonnet named (a disabled automation that failed long ago) is covered by hiding disabled and deleted subjects. |
| Round 3 review: rows for the owner's own revocations (pro001d-blocked-row-contradicts-stage3, tur026d-acceptance-depends-on-ux002) | opus: PRO-001d and stage 3 disagreed about revocations, and TUR-026d asserted a row before UX-002 exists | Watch-side revocations are coded `watch_grant_revoked` and never qualify; the TUR-026 blocks that do are asserted here | One rule, stated in stage 3, and each assertion lives in the slice that can run it. |
| Round 4 review: a narrowed predicate raised a row (ux002-predicate-narrowing-raises-row) | opus: a predicate mismatch was coded `automation_grant_revoked` on a still-enabled automation | A predicate mismatch is `watch_grant_revoked`, which never qualifies; `automation_grant_revoked` on a still-enabled automation now means only a narrowed allowlist | Later events that no longer match simply do not fire, so nothing needs the owner; a narrowed allowlist keeps failing runs, so it does. |
| Round 9 review: which watch raises a row (HEALTH_SOURCE_FAILURE_PAUSE_MISCLASSIFIED) | sol: stage 3 listed a watch "paused by a source error", a word section 21 reserves for the owner's own act | Stage 3 lists a watch suspended by a source failure (a `401` at once, or 3 consecutive failed polls; superseded in rounds 17 and 18: the third access failure with no poll answered `200` or `304` between, section 3's decision log); the owner's quiet and a disabled watch raise no row | One word for the owner's choice and another for a failure the owner must fix. |
| Round 20 review: a watch suspended by a revoked connection (PRO001_POLLER_CONNECTION_REVOCATION_FENCE) | sol: see section 3's decision log | Stage 3's watch row names the status code or the content-free cause `connection_unavailable` | A revocation found before any request has no status code, and the row must still say why the watch needs a person. |
| Round 21 review: the `connection_unavailable` row untested (UX002_CONNECTION_UNAVAILABLE_CAUSE_UNTESTED, pro001a-revocation-between-polls-untested) | grok, opus: no UX-002a test asserted the row's cause, so a row naming no cause, or none at all for a revocation found before any request, would pass | UX-002a asserts exactly one `person/blocked` row naming the watch and `connection_unavailable`, for a revocation mid-download and one between polls (PRO-001a's test since round 45; see section 8's row); PRO-001a asserts the suspension (section 3's decision log) | The row is the owner's only signal that the watch stopped reading, so its cause is part of the contract. |
| Round 27 review: a watch suspended by a redirect (pro001a-renamed-repository-redirect-misclassified-transient) | opus: see section 3's decision log | Stage 3's watch row may also name the cause `repository_moved`, and UX-002a asserts it (superseded in round 29: the cause is `unexpected_redirect`; see that row) | A redirect has a status code but says nothing the owner can act on; the cause names the fault. |
| Round 28 review: which redirect raises a row (pro001a-hydration-301-transferred-issue-suspends-watch) | opus: see section 3's decision log | UX-002a's `repository_moved` case (named `unexpected_redirect` since round 29; see that row) is a redirect on a stream request; a hydration's redirect suspends nothing and raises no row | Only a moved repository is the owner's to fix. |
| Round 29 review: a reused repository name (PRO001_REUSED_REPOSITORY_NAME_BYPASSES_IDENTITY_FENCE, pro001a-name-addressed-streams-follow-reused-repository-name) | sol, opus: see section 3's decision log | Stage 3's redirect cause is `unexpected_redirect`, and UX-002a asserts that name (PRO-001a's test since round 45; see section 8's row); a rename no longer raises a row | Requests are addressed by id, so a stream redirect no longer means a moved repository. |
| Round 32 review: a failure with no run (UX002_DISMISS_NO_RUN_OCCURRENCE) | sol: Dismiss was bound to `attention_dismissed_run_id`, but `ClaimDueAutomation` records a remote-egress block with no run (`orch/repository/automations.go:561-566,605-616`), so such a failure could not be dismissed, or a dismissal of the earlier run hid nothing while a newer block went unbound | Dismiss binds to an occurrence key, stated once in point 1: the latest `automation.remote_egress_blocked` audit record's id after `last_run_at`, otherwise the latest run's id; the column is `attention_dismissed_occurrence`; `DismissAttention` compares the key with the current one in its transaction and refuses a stale key; UX-002a adds the no-run dismissal, the unchanged-`last_run_id` re-raise and the stale-key refusal | Every occurrence the row can show must be one the owner can dismiss, and only that one. |
| Round 33 review: the occurrence key on the wire, and the block lookup's cost (UX002_DISMISS_OCCURRENCE_KEY_NOT_IN_RESPONSE, UX002_AUTOMATION_AUDIT_SCAN_UNBOUNDED) | mai: `DismissAttention` required the exact occurrence key, but `ListAttention`'s rows did not carry it; sol: the latest-block subquery has only `idx_audit_action(action, created_at)`, so each automation walks every automation's blocks, automations times blocks on the one connection | A stage 3 automation row carries `occurrence_key` and the client returns it unchanged to `DismissAttention` (point 2); UX-002a's migration adds the partial index `audit_logs(actor_id, created_at) WHERE action = 'automation.remote_egress_blocked'` (Performance); UX-002a adds the returned-key and stale-listed-key fixtures and the many-automations, large-foreign-history query-plan and latency fixture | The client cannot send a key it was never given, and a page read must not grow with another automation's history. |
| Round 34 review: the block lookup's cost restated outside section 8 (ux002-block-lookup-cost-not-updated-outside-section-8) | grok: section 20 and the Summary still called the dismiss path one column compare in the existing bounded read | Both now state section 8's cost: one seek on the partial index per block lookup plus one column compare | The pre-index lookup was not bounded; the summary must not say it was. |
| Round 42 review: the page token and the watermark (ux002-keyset-cursor-unspecified) | mai: keyset paging and a snapshot watermark were named without a sort key, cursor columns, encoding or snapshot semantics | Point 2 states paging once: a total order (stage rank, `since` descending, source kind, source id), an opaque page token holding the watermark and the last row's sort key, and a watermark that leaves out rows whose `since` is after the first page's read; UX-002a adds the insert, resolve and dismiss between-pages fixture | The watermark is a time, not an event sequence, because `events.sequence` is unique only within a session (`orch/db/schema/0001_initial.sql:87-91`) and most attention sources are not events (review dispositions appendix). |
| Round 43 review: the page token's per-source rule (ux002-keyset-predicate-drops-stage-and-kind, UX002_KEYSET_OMITS_STAGE_AND_KIND, ux002-page-token-per-source-predicate-skips-later-stages) | grok, sol, opus: applying `since < :since OR (since = :since AND id > :id)` to every source dropped later-stage rows newer than the cursor, and same-stage rows of a later kind | Each source compares its constant (stage rank, kind) with the cursor's first: earlier reads nothing, later reads every row qualifying at the watermark, and only the cursor's own source applies the `since`/id rule; fixtures across a stage boundary and a kind boundary (corrected in round 44: kind is compared only after `since`; see that row) | The keyset rule must follow the full lexicographic order, not its last two keys. |
| Round 44 review: the page token compared kind before `since` (ux002-keyset-same-stage-kind-ignores-since, ux002-pagination-order-contradiction, UX002_KEYSET_KIND_PRECEDES_TIMESTAMP, ux002-keyset-kind-compared-before-since-within-stage-3) | grok, mai, sol, opus: round 43's rule treated (stage rank, kind) as a prefix of the order, but `since` is the second key and kind the third, so in stage 3, which holds automation and watch rows, a cursor on a watch skipped older automations and a cursor on an automation re-read newer watches | Each source compares only its stage rank first; within the cursor's stage, an earlier kind reads `since < :since`, the cursor's own kind `since < :since OR (since = :since AND id > :id)`, and a later kind `since <= :since`; UX-002a adds 100 watches newer than a blocked automation (page 2 shows it) and a page ending on an automation after a newer watch (page 2 does not repeat it) | The predicate must compare the order's keys in the order's sequence; equal-`since` fixtures cannot tell kind-first from `since`-first. |
| Round 45 review: late commits under the time watermark (UX002_TIME_WATERMARK_NOT_COMMIT_FENCE) | sol: `CreateApprovalWithEvents` takes `created_at` before `BeginTx`, so an approval can commit after the first page with a `since` at or before the watermark, and page 2 would show it, contrary to the stated exclusion | Kept round 42's time watermark (see the dispositions appendix) and stated what it is not: a row committed late with an earlier `since` is shown by a later page when it sorts after the cursor and otherwise by the next first-page read, at most once either way; UX-002a adds that case | The guarantees the owner needs, no row twice and no reorder, hold for live keyset paging; only the exclusion claim was wrong, and a commit fence would need a sequence on every source. |
| Round 45 review: UX-002a's tests needed later slices (ux002a-acceptance-requires-later-slices-watch-quiet) | opus: UX-002 ships second, before TUR-028a and every PRO-001 slice, but UX-002a asserted watch rows, stage 3's cross-kind paging, `watch_grant_revoked` and `owner_quiet` | UX-002a tests only the sources it has; the watch rows' causes, their refusal to dismiss and stage 3's cross-kind paging fixtures move to PRO-001a; `watch_grant_revoked` and `owner_quiet` raising no row stay where PRO-001d and TUR-028a already assert them | Each slice's tests can be written when it lands (round 3's rule), and the slice that adds stage 3's second kind is the one that can test paging across it. |
| Round 45 review: the id-prefix citation (ux002-id-prefix-citation-points-at-generic-helper) | opus: `orch/ids/ids.go:10-12` is the generic helper and names neither prefix | Cite the call sites that mint `run_` and `audit_` ids beside the helper | The no-collision claim rests on those call sites. |
| Round 46 review: UX-002's dependency on TUR-026 (ux002-roadmap-dependency-omits-tur026-codes) | opus: section 8 and `docs/NORTH_STAR.md` said UX-002 had no dependencies, but UX-002a asserts rows for block codes only TUR-026a and TUR-026d create | Both now name TUR-026a and TUR-026d as dependencies | Section 27 already orders TUR-026 first; the roadmap now says why, so UX-002 cannot be scheduled ahead of the codes its tests need. |

## 9. F6 — Worker slots and background work

### Verdict

Adapt. Adopt DeepSeek's outcome, "a person is never queued behind work
nobody asked for", as claim order at concurrency one: **TUR-027 — Attended
work first**. TUR-026c carries the recovery guard. DeepSeek's reserved extra
slot and approval-wait slot release become measured hypotheses for AGT-000 and
MCPP-005. The 30-minute background grace is rejected.

### DeepSeek source

- Slots are held from launch through an active turn; `maxWorkers` defaults
  to 3 (`dsh:src/policy.ts:93`).
- One extra slot beyond the limit serves a person's message or status request
  (`dsh:docs/003-execution.md:55-61`; `dsh:src/executor/dsh.ts:166-199`).
- Background work keeps the slot up to 30 minutes after a turn
  (`dsh:src/executor/dsh.ts:32`; `dsh:docs/003-execution.md:31-39`); a handoff
  to a person frees it (`dsh:docs/003-execution.md:37`).
- A restart begins new Assignments with a recovery note
  (`dsh:docs/003-execution.md:144-146`).

### Turing today

- One global limit counts every `execution_active` run
  (`orch/repository/jobs.go:1362-1374`), default 1
  (`orch/config/config.go:341-346`). Claim order is FIFO by job creation
  (`orch/repository/jobs.go:1425`), so an automation job queued a second
  earlier is claimed before the owner's chat turn. The claim's order is served
  by `idx_jobs_capability_claim (agent_id, status, created_at_ns, id)`
  (`orch/db/schema/0012_worker_capability_routing.sql:15-16`), so the query
  needs no sort. The routing page walks pending jobs with a keyset cursor on
  `(created_at_ns, id)` (`orch/repository/jobs.go:1667-1688`) and claims
  nothing.
- An approval wait holds the slot (team design §7.6, "Approval wait").
- There is no shell or background job to grace: services start no
  subprocesses, and unsandboxed host shell is barred (`docs/NORTH_STAR.md:303`).
- AGT-000 gates raising concurrency on measured evidence.

### Unified design

1. **One unattended predicate.** A run is unattended when it has an
   `automation_runs` row (scheduled automations and PRO-001 `auto` runs) or
   its session's kind is `watch` (PRO-001c assessor runs). Everything else is
   attended: owner turns, Start-confirmed turns, delegation children and
   continuations. This is the definition `jobs.unattended` is written from
   once at enqueue, which the claim order and TUR-028 read (point 2); the
   enqueue-path tests (each in the slice that adds its path) and the
   backfill test compare that flag against it, so it is a query used only
   by those tests, not a gate. No gate needs it: an
   assessor run has no `automation_runs` row, and its enforced empty selected
   set and absent roster refuse every tool and every delegation before the
   policy and before any server, vault or profile read (section 5, point 3,
   the assessor payload). So `unattendedGrantCheck` and
   the memory service's `authorizeRun` keep their single `automation_runs`
   primary-key probe, and no attended tool call pays a session-kind read.
2. **TUR-027 claim order: two index-ordered probes.** The claim runs its
   existing query twice at most, both keeping `ORDER BY j.created_at_ns, j.id
   LIMIT 1`. Probe 1 adds `j.unattended = 0` and is served by a new partial
   index, `idx_jobs_attended_claim ON jobs (agent_id, created_at_ns, id)
   WHERE status = 'pending' AND unattended = 0`, so it visits only attended
   pending jobs and costs nothing when none is queued, however many
   unattended jobs wait. `jobs.unattended` is a new column, `INTEGER NOT NULL
   DEFAULT 0 CHECK (unattended IN (0, 1))`, written once, at insert, from an
   explicit `Unattended` field of `EnqueueUserMessageInput` (or of the queued
   run the assessor enqueue inserts). It cannot be derived at insert:
   `ClaimDueAutomation` inserts the job before its `automation_runs` row
   (`orch/repository/automations.go:697-722`). So the three callers that
   create an unattended run (the automation fire, the event fire and the
   assessor enqueue) pass `Unattended: true`, in the same transaction that
   writes the `automation_runs` row or creates the `watch` session, and
   nothing changes it afterwards, because neither the row nor the session
   kind changes for a live run. `RunUnattended` stays the one definition,
   and the slice that adds an enqueue path checks the flag against it for
   that path (Delivery slices). The migration backfills it
   from `automation_runs`. Probe 2, only when probe 1 finds nothing claimable,
   is today's query unchanged on `idx_jobs_capability_claim`
   (`orch/db/schema/0012_worker_capability_routing.sql:15-16`). A leading sort
   key such as `(automation_runs.run_id IS NOT NULL)` is not used: it would
   force a sort of every pending job on each claim. Session turn order,
   consent holds and routing compatibility are unchanged; nothing running is
   preempted by the claim (PRO-001c's assessor preemption is separate).
   Starvation is bounded by the existing queue-wait terminal
   (`orch/config/config.go:34`). `ListPendingRoutingWorkPage` is not changed:
   it decides routing waits, not claims, and its cursor depends on its order.
3. **Recovery.** TUR-026c is the code-enforced recovery rule; Turing adds no
   prompt-level recovery note, because a note cannot make an uncertain
   external effect safe to repeat.
4. **Recorded for AGT-000 (no code).** An attended reserve slot (DeepSeek's
   `+1`) measured against Ollama parallelism and KV-cache memory on the
   reference machine; the default stays 1 until it is green.
5. **Recorded for MCPP-005 (no code).** Releasing the slot while a run waits
   for a person, under MCPP-005's custody design.
6. **Rejected.** The 30-minute grace, 5-second backgrounding and
   handoff-exempt jobs: they exist to keep full-access shells alive.

### Security and privacy

Priority changes order only, never eligibility, route or policy. A noisy watch
can no longer push the owner's turn back, which also bounds attention abuse.

### Performance

At most two `LIMIT 1` probes per claim, each walking an index in order.
Probe 1 walks only attended pending jobs through the partial index, so a
backlog of unattended jobs costs it nothing; probe 2 runs only when nothing
attended is claimable. No sort is added; the new index is written only for
attended pending jobs. Expected effect: an owner turn waits behind at most the
one running job instead of every earlier unattended job. Measured: p50 and
p95 attended claim delay in a mixed fixture, and claim transaction time
against 10,000 queued jobs, all of them unattended, with probe 1's rows
visited asserted to be zero.

### Roadmap placement and dependencies

New **TUR-027 — Attended work first**, Workstream A. No dependencies; it does
not raise concurrency, so AGT-000 does not gate it. It must ship before
TUR-028, whose cancellation reads `jobs.unattended`, and before any PRO-001
slice that spends a model call.

### Delivery slices

| Slice | Size | Acceptance tests |
| --- | --- | --- |
| TUR-027 | S | An automation job older than a chat job: the chat job is claimed first; same-session turn order is unchanged; children and continuations keep §7.6 order; an unattended job is still claimed when nothing attended waits; for each enqueue path that exists when TUR-027 ships (an owner chat turn, a delegation child, a continuation and a scheduled automation fire), `jobs.unattended` equals `RunUnattended` for its run, and the migration backfills existing automation jobs so that the same holds for them; `EXPLAIN QUERY PLAN` shows probe 1 on `idx_jobs_attended_claim` and probe 2 on `idx_jobs_capability_claim`, neither with a temporary B-tree for the order; with 10,000 unattended jobs pending and none attended, probe 1 visits no row; the routing page's order and cursor are unchanged. Later unattended enqueue paths are tested in the slice that adds them: the event fire in PRO-001d (section 7) and the assessor enqueue, with its claim order and its enforced empty payload, in PRO-001c (section 5) |

### Open questions

None.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Act now or measure first | sol: fixtures first, priority only if evidence shows starvation; grok, mai: reservation only above concurrency 1; opus: priority now | Priority now, as a small item | FIFO-behind-automations is verified in the claim query; PRO-001 would add unattended jobs. The change is one extra ordered probe and adds no capacity. |
| Extra inference slot | dsh: `+1`; mai: logical urgent slot | AGT-000 hypothesis only | Two 7B runs at once is unproven on 24 GB; it doubles prompt and KV pressure. |
| Recovery note | dsh: prompt note; grok: optional prompt text; opus: code guard | Code guard (TUR-026c) | A prompt cannot enforce "do not repeat". |
| Round 1 review: assessor jobs | mai, sol, opus: assessor jobs have no `automation_runs` row, so they sorted as attended | One predicate: `automation_runs` row or `watch` session | Assessor runs cover several rules and cannot carry one automation id. |
| Round 1 review: claim plan | opus: a leading expression defeats the claim index | Two ordered `LIMIT 1` probes, asserted by query plan | Keeps the index order; an indexed `attended` column would need a migration and a value kept in step on every job insert. |
| Round 1 review: routing page | opus: reordering it breaks its keyset cursor and gains nothing | Left unchanged | It claims nothing. |
| Round 2 review: probe 1 walks the backlog (tur027-probe1-walks-entire-backlog) | opus: with nothing attended queued, probe 1 visited every pending unattended job | A `jobs.unattended` column written once at insert, with a partial index on attended pending jobs, asserted by query plan | This reverses round 1's "no column": the value cannot drift, because a run's unattendedness is fixed in the transaction that creates its job, and the bound is worth one column. |
| Round 3 review: the flag cannot be read at insert (tur027-unattended-flag-set-before-grant-row) | opus: the automation fire inserts its job before its `automation_runs` row, so `RunUnattended` is false at insert for every scheduled automation | An explicit `Unattended` input set by the three unattended callers; the test compares it with `RunUnattended` | Reordering the fire's inserts would touch a shipped transaction for no gain; the caller already knows what it is creating. |
| Round 40 review: where `RunUnattended` is read (tur027-rununattended-adds-session-read-to-every-attended-tool-call) | opus: routing `unattendedGrantCheck` through `RunUnattended` added a session-kind read to every attended tool call, unbudgeted and redundant with the enforced empty set | `jobs.unattended` carries it to the claim; only the memory refusal checks it directly; `unattendedGrantCheck` keeps its one `automation_runs` probe | The enforced empty set already refuses an assessor's tools before the policy, so the extra read refused nothing. |
| Round 41 review: the memory gate already refuses an assessor run (tur027-memory-refusal-justified-by-false-no-selection-check-claim) | opus: the memory service checks the run's selection right after `authorizeRun` (`orch/service/memory/call.go:76-82`), so an enforced empty set already refuses an assessor there; keeping `RunUnattended` in that gate added a session read to every memory call and refused nothing | Round 40's memory-gate exception is superseded: no gate reads `RunUnattended`; it is the definition `jobs.unattended` is written from and the query its test compares against; the memory acceptance clause now asserts the empty enforced set, which fails without PRO-001c's assessor payload (corrected in round 42: the clause and the payload are PRO-001c's; see that row) | All three gates already enforce the selection, so the definition belongs to the enqueue path and its test, not to a hot path. |
| Round 42 review: assessor tests in a slice that ships before the assessor, and the refusals' order (tur027-assessor-acceptance-in-slice-before-assessor-exists, tur027-mcp-refusal-reads-server-row, assessor-direct-team-call-not-refused) | opus: TUR-027 ships before PRO-001, but its acceptance tested assessor jobs, which PRO-001c adds with the `watch` kind (`orch/db/schema/0024_delegation_groundwork.sql:8-9` allows only `chat` and `delegation`), and PRO-001c asserted only that the payload "carries no tools"; grok: `CallTool` reads the server row, `sealed_token` included, before the selection check (`orch/service/mcpregistry/call.go:36-57`; `orch/repository/mcp_servers.go:1488-1496`), and point 1 named three gates while `CallTeamTool` decides from the roster alone | TUR-027 tests only the enqueue paths that exist when it ships, plus the backfill; the event fire's flag and claim-order checks move to PRO-001d and the assessor's, with its explicit enforced-empty payload and the beacon, registered-MCP, memory and team refusals, to PRO-001c; point 1 points to section 5, point 3, which states the payload and the refusals once | A slice's acceptance can test only what exists when it ships, and each refusal must happen before the secret or file it guards is read. |
| Round 43 review: the Start-confirmed path's flag (start-confirmed-unattended-flag-untested) | opus: TUR-027 always ships before PRO-001b, so its 'Start-confirmed turn if PRO-001b has shipped' clause never runs, and PRO-001b did not test its new attended enqueue | The clause leaves TUR-027; PRO-001b asserts its Start-confirmed job is written `jobs.unattended = 0`, equal to `RunUnattended`, and is claimed ahead of an older automation job | Every new enqueue path, attended or not, is tested in the slice that adds it. |

## 10. F7 — Credential shims and environment scrubbing

### Verdict

Reject as a mechanism: Turing runs no host shell or CLI, and DeepSeek itself
says the shims are not a security boundary. Adopt three checks as XTOOL-001
acceptance fixtures.

### DeepSeek source

- PATH-prepended shims for `gh`, `lark-cli` and blocked commands, written by
  atomic rename (`dsh:src/infra/environment.ts:79-105`), and process-wide PATH
  mutation (`dsh:src/infra/environment.ts:119-125`).
- Ambient token, git location, git config, askpass and identity variables
  removed by name (`dsh:src/infra/environment.ts:45-58`); an isolated git
  config with `useConfigOnly` and an empty default credential helper
  (`dsh:src/infra/environment.ts:106-115`).
- Refusal of repository-local `credential.*` and `http.*extraheader`
  (`dsh:src/infra/environment.ts:145-157`).
- Self-described as "default-path guardrails rather than an OS-level security
  boundary" (`dsh:src/infra/environment.ts:1-4`; `dsh:docs/003-execution.md:80`;
  `dsh:docs/006-deployment.md:19`).

### Turing today

- No orchestrator, runtime or MCP service starts a subprocess; the host
  command is not an agent tool.
- Services run read-only, with `cap_drop`, `no-new-privileges`, non-root, and
  an explicit per-service `environment:` block
  (`turing-backend/infra/docker-compose.yml:11-19,127-135`).
- The host command strips every registered and deprecated name before calling
  Compose (`turing-backend/cmd/turing/internal/host/host.go:91-93`;
  `turing-backend/cmd/turing/internal/registry/registry.go:177`).
- GitHub actions go through the orchestrator's brokered integration, so the
  token never reaches a worker.
- XTOOL-001 already requires declared secrets, no direct network and
  credential-access fixtures that fail closed.

### Unified design

Add to XTOOL-001's acceptance:

1. A sandbox environment is built from an empty allowlist of declared names,
   never by stripping a denylist from the parent environment. DeepSeek's
   stripped names (`GH_TOKEN`, `GITHUB_TOKEN`, `GIT_*` location, config and
   identity, `GIT_ASKPASS`, `SSH_ASKPASS`) and names containing `KEY`,
   `PASSWORD`, `SECRET` or `TOKEN` become sentinel fixtures that must be
   absent.
2. If git is ever offered inside a sandbox, a fixture proves that
   repository-local `credential.*` or `http.*extraheader`, `GIT_ASKPASS`, and
   system or global config cannot supply or exfiltrate a credential.
3. A name-based command block or PATH shim is never counted as a control;
   absolute-path execution must fail through the image and network
   restrictions.

### Security and privacy

Recording DeepSeek's own disclaimer as a rejected alternative prevents a
future specialist from reusing "OS user plus shims". Owner decision 7 and
`docs/NORTH_STAR.md:303` already forbid the alternative.

### Performance

None now. In XTOOL-001, environment construction is O(declared names) once per
sandbox start.

### Roadmap placement and dependencies

XTOOL-001 acceptance. No new item.

### Delivery slices

Documentation only (S) when this design is accepted; the fixtures land in
XTOOL-001's own pull requests.

### Open questions

None.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Allowlist or denylist | grok: deny DeepSeek's names plus a `KEY|PASSWORD|SECRET|TOKEN` pattern; sol, opus: allowlist from empty | Allowlist; the denylist becomes fixtures | A denylist misses unknown names; fixtures still prove the known ones absent. |
| New item | mai: ENV-001 | None | XTOOL-001 already owns sandbox credentials. |
| Image-local broker shim | sol: only on demonstrated need | Not specified | No reviewed executable needs one yet. |

## 11. F8 — Versioned prompt and profile overrides

### Verdict

Adapt narrowly. Adopt compare-and-set saves for OPS-003's editors and
"Default updated" for the team profiles Turing seeded. Reject an in-database
content-version history and every agent-initiated save.

### DeepSeek source

- Each save is an immutable numbered version with predecessor, timestamp and
  origin (`dsh:docs/004-prompts.md:93`; `dsh:src/app/versions.ts:10-28`).
- Saves name a base revision and a conflict is refused
  (`dsh:src/app/prompts.ts:202-206`; `dsh:docs/004-prompts.md:94`).
- An override records the built-in text it was based on; a changed default is
  flagged "Default updated" with keep or use-new
  (`dsh:docs/004-prompts.md:40`; `dsh:src/app/prompts.ts:186-192`;
  `dsh:docs/005-panel.md:41`).
- The router can save overrides by tool on a human request
  (`dsh:docs/004-prompts.md:38`). Editing prompts never changes gate or tool
  checks (`dsh:docs/004-prompts.md:36`).

### Turing today

- Built-in instructions are compiled into the runtime. User-owned layers are
  files.
- Persona and profile saves are already compare-and-set on
  `expected_content_hash` (`proto/turing/v1/memory.proto:350-355,368-370`).
- A specialist profile's revision hashes authority fields only, so an
  instruction edit keeps its grant (`orch/teamfiles/loader.go:64`).
- Runs snapshot what they used (persona, profile and skills are pinned on the
  job).
- `init.sh` seeds Dev, Inbox and Research once and records a receipt
  (`turing-backend/scripts/init.sh:17-18,318-414`), so improved defaults in a
  later release never reach an existing install, and nobody is told.

### Unified design

1. **OPS-003 saves.** A save carries the content hash the editor read; a
   mismatch writes nothing and returns the current bytes, the same rule as
   `SaveMemoryPersona`. An authority-field change still needs a fresh grant.
   No agent RPC writes a profile or skill.
2. **Default updated, host side (OPS-001 surface).** `./turing team defaults`
   (owner-run on the host; nothing in Turing can invoke it) hashes each
   `team/<id>/AGENT.md` whose id matches a shipped template and classifies it
   as `current` (the current template's hash), `unchanged-old-default` (a hash
   on the command's list of every past template hash) or `edited` (neither).
   It shows old default → new default beside the file, and `--apply <id>`
   writes the new default only when the file is `unchanged-old-default`, or
   with an explicit `--replace`. A test fails when a template changes without
   its old hash being appended to the list. Nothing is added to the
   `team-seeded` receipt: the past-hash list alone decides, and a receipt
   written at seed time says nothing about a file the owner has since edited
   or restored.
3. **Authority never rides an upgrade.** A new default that changes an
   authority field changes the revision, so the profile needs a fresh grant
   (team design §6.2).
4. **Not built.** A SQLite or file history of prompt content: the file is the
   current text, the run's snapshot is what it used, and backups or Git (TUR-016)
   are history. Keeping old text elsewhere would let derived state outlive its
   source (`docs/NORTH_STAR.md:324-325`). No mid-run prompt reload.

### Security and privacy

Writes stay host-side; the `/team` mount stays read-only to the orchestrator
until OPS-003 changes its threat model. No agent path can save instructions,
which keeps owner decision 7 and the human-authored persona rule
(`docs/NORTH_STAR.md:308`).

### Performance

Hashing on demand in the host command and one hash comparison per save. Nothing
on the turn path.

### Roadmap placement and dependencies

OPS-003 acceptance for compare-and-set saves; the `team defaults` command is a
small OPS-001-surface slice with no dependencies.

### Delivery slices

| Slice | Size | Acceptance tests |
| --- | --- | --- |
| F8a `turing team defaults` | S | An unchanged old default is detected and replaced on `--apply`; a file restored by hand to an old default is classified the same way; an edited file is never written without `--replace`; an authority change leaves the profile needing a grant; a changed template without its old hash fails the guard test |
| F8b OPS-003 compare-and-set | S | A stale hash writes nothing and returns the current bytes; the runtime identity cannot save; an edit after enqueue leaves the queued job's snapshot unchanged |

### Open questions

None. The starter persona is left out: it is human-authored once edited, and
a changed starter is announced in release notes.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Version history | mai: SQLite version table (CFG-001); sol: private history files plus SQLite metadata; grok, opus: none | None | History duplicates user content outside its file with new retention and purge duties; backups and Git already provide it. |
| "Default updated" | opus: receipt hashes plus host command; grok: "template changed" with keep-mine; sol: three-way UI in OPS-003 | Host command with a past-hash list now; an app badge with OPS-003 | Works today without the editor; the app gains it when OPS-003 lands. |
| Round 1 review: receipt hashes | opus: recording template hashes in the receipt is redundant with the past-hash list | Dropped | The list classifies every case; the receipt would be a second, less reliable record. |
| Persona | opus: open question | Excluded | Persona is human-authored; an upgrade must not offer to replace it. |

## 12. F9 — Role-scoped interfaces

### Verdict

Reject: Turing already enforces role separation at runtime, which a `Pick<>`
type cannot. One rule carries into every new service in this design.

### DeepSeek source

`EngineForSources`, `EngineForMainSession`, `EngineForPeople`,
`EngineForSettings` and `EngineForExecutor` are `Pick` types over one Engine
(`dsh:src/core/engine.ts:260-270`; `dsh:docs/007-architecture.md:64,257`).

### Turing today

- Each internal caller is a token with an exact gRPC method allowlist, never a
  caller-declared role (`orch/auth/identity.go:12-31`).
- Services split public and internal facets that refuse each other's methods
  (`orch/service/team/facets.go:11-72`).
- Narrow Go interfaces already exist where a package needs them, such as the
  scheduler's `Dispatcher` (`orch/service/automations/scheduler.go:17-23`).

### Unified design

The rule for UX-002's `AttentionService` (including `DismissAttention`), PRO-001's watch, rule and thread methods, and TUR-028's quiet switch:
a new RPC is denied to every existing identity by default and granted only to
its intended caller; each service has a public facet, and an internal one only
if a worker needs it. No Engine type, no interface registry, no repository-wide
refactor.

### Security, performance, roadmap, slices

The existing identity interceptor is the boundary; the new services add
wrong-identity tests. No cost. No roadmap item. No slices beyond those tests.

### Open questions

None.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Adopt or reject | sol: adopt as discipline at new seams; mai: adopt facades and denial tests; grok, opus: reject | Reject as a feature; keep the rule for new services | Turing already has the stronger runtime form; the useful part is a test per new RPC. |

## 13. F10 — Source-event identity, edits and failure-isolated intake

### Verdict

Adapt, as PRO-001a acceptance; no new item. It settles what a polled item
proves before the gate trusts its actor, what an edit does, and how one bad
item or one failing stream is kept from stalling a watch. The rules are
written into section 3 (point 2's event, edit, cursor and progress
paragraphs; point 4; the security and performance bullets; the PRO-001a
row) and section 7 ("What never fires"), so the spec states each rule once.

### DeepSeek source

- A comment's trigger id includes its `updated_at`, so every edit is a new
  trigger (`dsh:src/sources/github-trigger.ts:66-68`), attributed to the
  comment's author (`dsh:src/sources/github-trigger.ts:62-63`). An opened
  issue has one stable id (`dsh:src/sources/github-trigger.ts:82-84`), and a
  later body edit is never read, because "only the author can be the actor of
  the body" (`dsh:src/sources/github.ts:353`).
- Each stream reads one page of 100, oldest first, then steps its cursor to
  five seconds before the newest item and lets id deduplication absorb the
  overlap (`dsh:src/sources/github.ts:213,243-245`;
  `dsh:src/sources/poll.ts:15-16`). The ETag is sent only while `since` is
  unchanged (`dsh:src/sources/github.ts:211,246-248`).
- Streams fail independently (`dsh:src/sources/github.ts:182-187`); invalid
  list items are skipped and counted (`dsh:src/infra/commands.ts:42-47`;
  `dsh:docs/003-execution.md:174`).
- A 404 is retried, because it can mean lost access to a private repository;
  410 means gone (`dsh:src/sources/github.ts:157-158`;
  `dsh:docs/003-execution.md:166`). An error is reported only after
  `failuresBeforeError` consecutive failed polls, default 3
  (`dsh:src/sources/github.ts:26,124-142`).
- Corrections. (1) DeepSeek attributes every comment revision to the
  comment's author, but anyone with write access to a repository can edit
  another person's comment (GitHub Docs, "Managing disruptive comments"), so
  an edited revision has an unknown actor. The same holds for an issue
  body, which DeepSeek reads as its author's
  (`dsh:src/sources/github.ts:353`), and the issue object cannot say
  whether a later `updated_at` came from an edit, a label, a state change
  or a comment, so Turing admits any item first read with `updated_at`
  later than `created_at` as `observe_only` (section 3, point 2). (2) A full page whose items all lie
  within five seconds of `since` cannot move DeepSeek's cursor
  (`dsh:src/sources/github.ts:243-245`), and the ETag it keeps then answers
  `304` while later items wait on page 2 (`dsh:src/sources/github.ts:248`),
  so a burst of more than 100 items within five seconds stalls that stream
  until its `since` falls behind the 14-day horizon it is clamped to
  (`dsh:src/sources/github.ts:209-210`; `dsh:src/sources/poll.ts:6`), up to
  two weeks, and items updated in that window may be skipped. (3) Its health detail
  embeds the provider's error text (`dsh:src/sources/github.ts:124-125`).

### Turing today

- Nothing polls. The GitHub client makes one request per attended tool call
  (`orch/service/integrations/github.go:89,115-198`).
- Round 2 made the first release creation-only and kept `resource_id` apart
  from `platform_event_id` (section 3, point 2). The previous revision left
  per-stream progress, malformed items and a failure threshold unspecified,
  paused a watch on any `404`, and keyed an issue's resource by its number
  alone, which collides across the up to 10 repositories of one watch.

### Unified design

1. **Identity.** Resource ids are namespaced by GitHub's numeric repository
   id, resolved once when the repository is added, which also addresses
   every later poll request: stream pages, hydrations and the display-name
   refresh (section 3, point 2, A scope edit); an event is a creation (`{resource_id}:created`), and its actor is
   its author's numeric user id, never a login (section 3, point 3, Listed
   actors). A name, a repository's or an account's, is display text only,
   a repository's shown as last seen (section 3, points 2 and 3). A later release that
   admits edits treats an edit's actor as unknown: it can update an `observe`
   proposal, never fire `auto`, never be the owner's act.
2. **Edits.** Section 3, point 2 states when a re-listed item's stored
   excerpt is replaced (still held, under 30 days old, event not dismissed,
   body hash changed) and that nothing is re-gated and the clock does not
   restart, and that any replacement, like an item first read with
   `updated_at` later than `created_at`, marks the event `observe_only` for good and drops its pending fire. It
   also charges a replacement its byte delta against the excerpt budget,
   and a replacement the budget cannot hold clears the old excerpt
   instead, keeping no text. The
   one exception is an edit inside an interval the backlog clamp skips: it
   is not read, so the held excerpt keeps its earlier text until its 30-day
   sweep, and the clamp deletes the watch's pending fires instead.
   Confirm and the assessor's claim and result are bound to the revision
   they read (section 5, points 2 and 3).
3. **Progress.** Section 3, point 2 states the cursor rule once: a stored
   `since` and never a page, `next = max(newest − 5 s, since)` after a walk
   of one page and never past the second before `P`, page 1's latest valid
   `updated_at`, after a walk of several pages, so no item a mid-walk update or deletion shifts
   across a page boundary is lost, paging at a
   fixed `since` within one poll whenever a page is full, up to 3 pages, and
   progress defined once (the walk ended on a short page); a poll stopped by
   its budget is behind, and 3 behind polls show `backlog`, never a silent
   skip. Section 3, point 2 ("Poll deadline and slots") also states the
   10-minute poll deadline once: each poll starts at the repository after
   the last one a poll began, an index written at most once when a poll
   ends or stops, only when it changes, and reads each repository's issue
   stream before its comment stream, so a deadline or a stop rotates which
   repositories it cuts, and a stream it leaves unreached is behind. Only a stream in `backlog` is clamped, to the hour floor of its
   poll start minus 24 hours, which bounds a stall at about a day, keeps a
   quiet stream's request and ETags stable, and passes no creation the
   `stale` skip would admit; a clamp that moves `since` records a
   content-free `skipped_interval` and deletes the watch's pending fires.
   Resuming after quiet or a suspension starts from the stored `since`
   and counts returned day-old items `stale`, and section 7 keeps any event
   first read more than 15 minutes after its creation out of `auto`.
4. **Failure isolation.** A malformed item is skipped and counted and may be
   passed; a malformed response, including an element over the decoder's
   1 MiB bound, and a page whose commit failed are transient failures of
   only their stream, with the cursor held (section 3, point 2).
5. **Status classes.** `401` suspends the watch at once, as a redirect on
   an id-addressed stream request (cause `unexpected_redirect`) and a revoked or changed
   connection do; no redirect is followed, and one on a label hydration
   only skips that comment (section 3, Security and privacy, and point 2, Stopping a
   poll); rate limits (any
   `429`, or a `403` carrying `x-ratelimit-remaining: 0` or a `retry-after`
   header, section 3's rule) back off and count no failure; GitHub sends
   `x-ratelimit-*` headers on every response, so their mere presence
   classifies nothing. Section 3's failure classifier states the rest once:
   every failed poll counts toward `failed_polls`, whose third shows
   `error`; only access failures (any other `403`, `404` or `410`) count
   toward `access_failures`, whose third suspends the watch, reported
   `error` with `needs_person`, never `paused` (section 21); only a poll
   that received a `200` or `304` for that stream and had no failure resets
   either, so a stream the deadline cut before any status, or never
   reached, keeps both. The constant 3 is shared with TUR-014's
   health entries (section 21) and CHN-002's ordered delivery (section 23).

### Security and privacy

- Closes an authority gap before it exists: an outsider with write access
  whose edit of a listed author's issue or comment Turing can detect (a
  body hash changed after intake, or an item first read with `updated_at`
  later than `created_at`, whatever changed it) cannot start unattended
  work, and nothing infers that
  the owner applied a label. An undetectable edit is the limit below; a run
  it starts is held to the unattended read-only ceiling (section 7): no
  write tool, no recall, no pinned persona or profile, no owner skill.
- Replacing the excerpt drops text an author edited out, instead of keeping
  it for 30 days, and keeps one copy on one clock; an edit inside a skipped
  interval is the stated exception (below).
- Counters, status codes and backlog flags carry no content; raw error bodies
  are never stored.
- Honest limits, stated in the product: `since` streams do not report
  deletions, so a deleted comment's excerpt lives until its sweep; anyone
  with write access can alter an issue's or a comment's text in the second
  it was created, before Turing first reads it, which Turing cannot detect,
  so `auto`'s acknowledgement says so, while any item changed later but
  before its first read is admitted `observe_only`; an edit
  Turing does observe (a differing body hash) marks the event
  `observe_only`; an edit inside an interval the backlog clamp skipped is
  never read, so its excerpt keeps the earlier text until its 30-day sweep,
  and the watch's status shows that interval.
- Invariants touched: owner decisions 2 and 5.

### Performance

Zero model calls. The overlap costs unique-index probes, never assessments.
One write transaction of at most 100 inserts per page, inside the poller's
per-poll transaction budget, which section 3's Performance states once;
state is two rows per listed repository. An excerpt
replacement is one update, only when the body hash changed. Measured as
content-free counters: excerpts replaced, items skipped, behind polls,
skipped intervals, failures by status class.

### Roadmap placement and dependencies

PRO-001a acceptance (+S); no new ID. The same fixtures serve CHN-002's and
CON-004's adapters when those arrive.

### Delivery slices

Inside PRO-001a (section 3's table, including a first intake and a
replacement in the same second), with one test in PRO-001b (a stale
Confirm, including after a same-second edit), two in PRO-001c (an edit
during a queued or running assessment, one of them a same-second edit),
and four in PRO-001d (a comment or an issue first seen edited, an edit with
a pending fire held, a dismissed thread, quiet).
EVAL-001 gains "an outsider's detectable edit of a listed author's issue or
comment never becomes unattended work" and "an outsider's undetectable edit starts
at most a run under the unattended read-only ceiling" (section 7).

### Open questions

None. Admitting edits or label changes as events is a later, separately
reviewed release under point 1's rule.

### Decision log

The material choices (status classes, edits, equal timestamps, repository
namespacing) are the "Phase 2 F10" rows of section 3's decision log. One more:

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Label actors now | mai: read `/issues/events` for verified label actors | Not in this release | Round 2 chose creation-only events and dropped "when I label"; a third stream per repository serves one trigger shape. |
| Round 3 review: two statements of the edit rule (pro001-edit-replacement-resurrects-cleared-excerpts) | opus: point 2 here and section 3 gave different conditions for a replacement | Point 2 points to section 3, which states the rule once | See section 3's decision log. |
| Round 4 review: edits after admission, stale assessments and quiet streams (WATCH_PENDING_EDIT_BYPASSES_AUTO_ACTOR, pro001-assessor-edit-result-race, WATCH_ASSESSMENT_RESULT_MISSING_REVISION_FENCE, pro001-304-backlog-ambiguity) | sol, mai: an edit after admission kept `auto` eligibility and could be outrun by an assessment; a `304` could read as a stall | Points 2 and 3 point to the rules in sections 3 and 5 | See sections 3 and 5's decision logs. |
| Round 7 review: the exclusive `since` (pro001-since-exclusive-drops-shared-timestamp, PRO001_GITHUB_SINCE_EXCLUSIVE) | grok, sol: the keyset assumed an inclusive `since` and skipped unread items sharing the last second | Point 3 pointed to section 3's corrected walk (superseded in round 8) | See section 3's decision log. |
| Round 8 review: the cursor and same-second edits (pro001-drained-page-cursor-misses-later-events, PRO001_GITHUB_TAIL_CURSOR_NOT_ADVANCED, pro001a-drain-cursor-moves-backward-loop, WATCH_SECOND_RESOLUTION_EDIT_IDENTITY) | grok, mai, opus: the stored page cursor drifted or moved backward; sol: one-second timestamps cannot identify an edit | Points 2 and 3 point to section 3's `since`-only rule and hash-bound revisions | See section 3's decision log. |
| Round 9 review: progress, the stall bound, DeepSeek's horizon, edits and suspension (pro001-full-page-304-counted-as-progress, pro001a-burst-304-progress-contradiction-hides-backlog, pro001a-quiet-poll-304-fixtures-unsatisfiable, f10-dsh-stall-for-good-overstated, PRO001_EDIT_EVAL_CONTRADICTS_DISCLOSED_LIMIT, HEALTH_SOURCE_FAILURE_PAUSE_MISCLASSIFIED) | grok, opus: two progress rules hid a stalled burst; opus: quiet tests expected `304` on the overlap re-read, and correction (2) said DeepSeek stalls for good although its 14-day horizon ends the stall; sol: the edit guarantee ignored its disclosed limit, and a failing watch read as the owner's pause | Points 3 and 5 and the security bullet point to section 3's single progress definition, its 24-hour clamp (superseded in round 10 by the backlog-only clamp; see that row) and its suspension; correction (2) cites the horizon; the edit guarantee names detectable edits | See section 3's decision log. |
| Round 10 review: the backlog clamp, paging and skipped edits (pro001-clamp-stale-count-unsatisfiable, pro001-clamp-skips-held-excerpt-edits, PRO001_MOVING_HORIZON_DEFEATS_ETAGS, PRO001_CLAMP_MAKES_STALE_COUNTS_UNOBSERVABLE, pro001a-unfloored-clamp-defeats-quiet-304, pro001a-near-burst-paging-throughput-collapse) | grok, sol, opus: the every-poll clamp defeated quiet ETags and hid what it skipped; paging only when `since` stuck starved near-burst streams; a skipped edit was promised as replaced | Points 2 and 3 and the honest limits point to section 3's rule: page on full pages, behind and `backlog`, clamp only in `backlog` to the hour floor, a content-free `skipped_interval`, pending fires deleted, and the skipped edit stated as the one exception to replacement | See section 3's decision log. |
| Round 11 review: shift loss and partial-page validators (pro001a-full-page-paging-makes-walk-shift-loss-routine, PRO001_PARTIAL_PAGE_VALIDATOR_LOSS, pro001a-paging-fixtures-contradict-admitted-rate-cap, pro001-dsh-floor-citation-wrong-lines, pro001a-dsh-horizon-floor-citation-lines) | opus: a mid-walk update or deletion could shift an unread item across a page boundary for good; sol: a validator kept for a page stopped part-way skipped its unread items | Point 3 points to section 3's corrected rule: after a walk of several pages `since` stops before page 1's last item, and only a fully committed page keeps a validator | See section 3's decision log. |
| Round 12 review: the roadmap's suspension sentence (PRO001_ROADMAP_TRANSIENT_SUSPENSION) | sol: PRO-001's roadmap acceptance said any source failure suspends a watch, while point 5 suspends only on these classes | PRO-001's sentence names the classes: a `401` at once, a non-rate-limit `403`, `404` or `410` after three consecutive failed polls (superseded in rounds 17 and 18: after the third such failure with no poll answered `200` or `304` between; see those rows); `5xx` and network failures keep retrying with no suspension and no `needs_person` row | The roadmap restates point 5 instead of widening it. |
| Round 13 review: issue edits before intake and header-less `429`s (pro001-issue-edit-before-first-read-auto, pro001a-headerless-429-unclassified) | mai, opus: see section 3's decision log | Correction (1) covers an issue body as well as a comment; the security bullets name every item kind; point 5's rate-limit class is any `429` or a `403` with a rate-limit header (superseded in round 14: a `403` carrying `x-ratelimit-remaining: 0` or `retry-after`; see that row), and counts no failure | See section 3's decision log. |
| Round 14 review: every `403` read as a rate limit (f10-403-rate-limit-header-class-matches-every-403) | opus: point 5 called a `403` with a rate-limit header a rate limit, but GitHub sends `x-ratelimit-*` on every response, so a credential or permission denial would back off forever and never suspend | Point 5 uses section 3's exact rule: a `403` is rate limiting only with `x-ratelimit-remaining: 0` or `retry-after`; PRO-001a's test is a recorded denial `403` carrying `x-ratelimit-remaining` above zero and no `retry-after`, counted as a failed poll, three in a row suspending | One rule, stated once and restated exactly; a denial now reaches the owner as a suspension. |
| Round 14 review: a queued assessment and an edit (PRO001_ASSESSOR_INPUT_DISPATCH_REVISION_FENCE) | sol: see section 5's decision log | Point 2 binds the assessor's claim, as well as its result, to the revision it read | See section 5's decision log. |
| Round 15 review: timeouts and the over-bound element (pro001a-max-page-cannot-meet-30s-timeout, pro001a-overbound-element-skip-incompatible-with-reused-decoder) | grok: a worst-case page could not arrive within the 30-second timeout on a slow link, and the same request was sent again; opus: the reused decoder cannot skip an over-bound element | Point 5 counts a timed-out request as a network failure only at the floor `per_page` of 12, which section 3, point 2 halves to (superseded in round 16: there is no halving or floor, and every missed deadline is a transient failure; see that row); point 4 names an element over 1 MiB as a malformed response | See section 3's decision log. |
| Round 16 review: the page-size cycle and the failure classes (pro001a-above-floor-timeout-still-counts-as-failure, pro001a-malformed-response-failure-class-unassigned, pro001a-per-page-reset-repeats-timeout-cycle) | grok, opus: a timeout above the floor counted nothing while the classifier counted it, a malformed response had no class, and the halving repeated timeouts on a slow link | Points 4 and 5 point to section 3's classifier: a malformed response and every missed request deadline are transient failures, and a failure of the other class restarts the count (superseded in round 17: two streaks; see that row) | See section 3's decision log. |
| Round 17 review: alternating failures and stopped polls (PRO001_MIXED_FAILURES_NEVER_SURFACE, pro001a-inflight-poll-survives-quiet-disable-delete, pro001a-inflight-poll-survives-disable-quiet-delete) | sol: with one count that a failure of the other class restarted, alternating `404`s and timeouts never reached either threshold; grok, opus: see section 3's decision log | Point 5 points to section 3's two streaks: `failed_polls` (every failure; 3 show `error`) and `access_failures` (access failures only; 3 suspend), both reset only by a successful poll (superseded in round 18: only a poll answered `200` or `304` with no failure; see that row); a disable, deletion, suspension, quiet or `401` stops a poll in flight (section 3, point 2) | See section 3's decision log. |
| Round 18 review: the deadline's streaks and unreached streams (pro001a-deadline-stop-resets-unanswered-streaks, PRO001_POLL_DEADLINE_STARVES_LATER_STREAMS, pro001a-poll-deadline-starves-unreached-streams) | grok: a deadline cancel reset both streaks; sol, opus: a fixed stream order let the deadline starve later streams unseen | Points 3 and 5 point to section 3: only a poll answered `200` or `304` with no failure resets a streak, each poll starts after the last stream the previous one began (superseded in round 19: after the last repository), and an unreached stream is behind | See section 3's decision log. |
| Round 19 review: rotation by repository (pro001a-rotation-start-on-comment-stream-forces-hydrations) | opus: a stream-level rotation could start on a comment stream and force hydrations | Point 3 points to section 3: each poll starts at the repository after the last one the previous poll began and reads its issue stream before its comment stream (since round 20 a stopped poll counts, and since round 21 the index is written once when a poll ends or stops; see those rows) | See section 3's decision log. |
| Round 20 review: the rotation index and connection revocation (PRO001_REPOSITORY_ROTATION_INDEX_NOT_ADVANCED_ON_CANCEL, PRO001_POLLER_CONNECTION_REVOCATION_FENCE) | mai, sol: see section 3's decision log | Point 3 says the index is committed before a repository's first request, so a stop rotates too (superseded in round 21: written once when a poll ends or stops); point 5 adds that a revoked or changed connection suspends the watch before any request, pointing to section 3's per-request check | See section 3's decision log. |
| Round 21 review: the rotation write and a revocation between polls (pro001a-rotation-write-outside-stated-transaction-budget, pro001a-revocation-between-polls-untested, UX002_CONNECTION_UNAVAILABLE_CAUSE_UNTESTED) | opus, grok: see section 3's decision log | Point 3 says the index is written once when a poll ends or stops; Performance points to section 3's per-poll transaction budget; point 5's suspension covers a connection revoked between polls, found before the next poll's first request | See section 3's decision log. |
| Round 22 review: a stopped poll's writes and a failed page commit (PRO001_STOP_COMMITS_NOTHING_CONTRADICTS_ROTATION_WRITE, pro001a-stop-path-rotation-write-contradicts-commit-no-row-tests, pro001d-direct-fire-page-rollback-unclassified-stall) | grok, opus: see section 3's decision log | Point 4 counts a failed page commit as a transient failure of its stream; section 3, point 2 states a stopped poll's writes once | See section 3's decision log. |
| Round 23 review: the paging fence and the rotation write (PRO001_PAGING_FENCE_MALFORMED_BOUNDARY_TIMESTAMP, pro001a-steady-state-rotation-write-rewrites-unchanged-index) | sol, opus: see section 3's decision log | Point 3 names `P` as page 1's latest valid `updated_at` and the rotation index as written only when it changes | See section 3's decision log. |
| Round 24 review: the rotation write and a growing replacement (pro001a-rotation-wording-stale-and-duplicated, PRO001_EDIT_REPLACEMENT_EXCERPT_BUDGET) | opus, sol: see section 3's decision log | Point 3 says the index is written at most once when a poll ends or stops, only when it changes; point 2 points to section 3's byte-delta charge and its clear-instead rule | See section 3's decision log. |
| Round 27 review: the redirect status class (pro001a-renamed-repository-redirect-misclassified-transient) | opus: see section 3's decision log | Point 5 adds a redirect beside the `401` as a definitive class (its cause renamed `unexpected_redirect` in round 29; see that row) | The rule is stated once in section 3's classifier. |
| Round 28 review: identity and redirect classes (pro001-repo-id-source-unspecified, pro001a-hydration-301-transferred-issue-suspends-watch) | opus: see section 3's decision log | Point 1 says the numeric id is resolved once when a repository is added; point 5 limits the definitive redirect to a stream request, and a hydration's redirect skips that comment | Both rules are stated once in section 3. |
| Round 29 review: a reused repository name (PRO001_REUSED_REPOSITORY_NAME_BYPASSES_IDENTITY_FENCE, pro001a-name-addressed-streams-follow-reused-repository-name) | sol, opus: see section 3's decision log | Point 1 says the resolved id addresses every later request; point 5 names a stream redirect `unexpected_redirect` | The identity a resource id carries is the one every request reads. |
| Round 30 review: actor and repository identity (PRO001_ACTOR_LOGIN_REASSIGNMENT, pro001a-scope-duplicate-repository-id-unrefused, pro001a-typed-name-display-stale-after-rename-reuse) | sol, opus: see section 3's decision log | Point 1 adds that an actor is its numeric user id and that a name is display text only, pointing to section 3 | Identity is stated once, in section 3. |
| Round 31 review: the display name behind `304`s (pro001a-display-name-stale-on-304-after-rename, pro001a-display-name-stale-until-item-overclaims-never-names-reused) | grok, opus: see section 3's decision log | Point 1 says the id also addresses the display-name refresh, pointing to section 3 | The refresh is stated once, in section 3. |
| Round 33 review: the display name's stale window (pro001a-stale-name-window-exceeded-by-failed-refresh, pro001a-stale-name-window-bound-and-never-names-overclaim, pro001a-repository-request-persisted-etag-and-checked-at-yagni) | grok, opus: see section 3's decision log | Point 1 says a repository's name is shown as last seen, pointing to section 3 | The name's freshness is stated once, in section 3. |

## 14. F11 — Model-surface contract test with a size ratchet

### Verdict

Adopt, as **EVAL-001a**, the deterministic tier of EVAL-001: one CI test over
the requests each role actually sends, with no model call.

### DeepSeek source

- One test collects every model-readable string per role and fails on
  unregistered tool names, unknown enum options, legacy terms and cross-role
  references (`dsh:tests/model-surface.spec.ts:300-345`).
- A per-role budget over system prompt plus tool definitions, set about 5%
  above the baseline: "Lower this limit when shrinking model-facing text;
  raise it only intentionally" (`dsh:tests/model-surface.spec.ts:36-40,386-396`).
- Required and forbidden phrases per prompt file
  (`dsh:tests/cards.spec.ts:8-30`).
- Not copied: the 59,000 and 40,500 character numbers, English word lints,
  and characters as a stand-in for tokens.

### Turing today

- Parts are tested, not the composition: skill index and loaders
  (`agent-runtime-go/internal/agent/skills_test.go`), the team offer
  (`agent-runtime-go/internal/agent/team_tool_test.go`), specialist jobs
  (`agent-runtime-go/internal/agent/specialist_job_test.go`).
- Host text and schemas must agree with code: the skill boundary reminder
  (`agent-runtime-go/internal/agent/skills.go:13`), the delegate schema that
  interpolates `DelegationBriefMaxTaskBytes` (`orch/service/team/tools.go:101-107`),
  and the GitHub frame's instruction (`orch/service/integrations/github.go:254-260`).
- Tool schemas are a disclosed data category on a remote route
  (`agent-runtime-go/internal/agent/egress.go:148-153`).
- An end-to-end harness already runs a real orchestrator and worker
  (`testkit.RunWorker`, `turing-backend/agent-runtime-go/testkit/testkit.go:59`)
  against a fake Ollama that captures each `/api/chat` body
  (`orch/app/team_delegation_e2e_test.go:23-31`).

### Unified design

1. One table-driven test in `orch/app`, on that harness. Configurations: a
   plain local chat with the bundled servers' tools, a parent with the team
   on, a specialist with an empty enforced set, and a continuation. PRO-001c
   adds the assessor and AGT-003 adds its own when they exist.
2. For each captured request: every tool name in host-authored text is
   offered in that request, and a conditional tool that is absent is not
   promised; every enum and byte limit in a schema equals its Go constant;
   the required framings are present (`skillContentBoundaryReminder`,
   `DELEGATION_BRIEF`, `DELEGATION_RESULTS`, the GitHub frame instruction);
   and the serialized bytes of the system messages and of the tools array
   equal the numbers in `testdata/model_surface_bytes.json`. Growth fails
   until that file changes in the same pull request; a decrease fails with
   the new, lower number to record. Frame nonces are normalized first.
3. `mcp-files` and `mcp-system` each keep the same byte ratchet over their own
   `tools/list`, because the root `go test ./...` never reaches those modules.
4. Fixtures are synthetic. Owner-authored persona, profile and skill text is
   not measured; no whole prompt is kept as a golden copy; nothing semantic is
   asserted.

### Security and privacy

It pins the framings that keep retrieved, brief and skill text as data, so a
refactor that drops one fails CI. No vault, credential or transcript enters a
fixture. Prompt text stays outside authorization: selection, policy, approval
and egress are still enforced in code.

### Performance

CI only: seconds, no model call, nothing in production. It is the one guard on
static per-turn cost that needs no model: every schema byte is prefill on a
7B model with one slot and KV memory on a 24 GB machine. The ratchet counts
bytes, not tokens; latency effects are EVAL-001 measurements.

### Roadmap placement and dependencies

EVAL-001a, a slice of EVAL-001. It needs none of EVAL-001's dependencies
(TUR-010, TUR-021, MEM-003) and can land first.

### Delivery slices

| Slice | Size | Acceptance tests |
| --- | --- | --- |
| EVAL-001a | S | Renaming `skill_view` without updating the guidance that names it fails; a delegate schema `maxLength` other than `DelegationBriefMaxTaskBytes` fails; removing the skill boundary reminder fails; adding 1 KiB to a tool description fails until the bytes file changes; a specialist with no tools is never promised `team.delegate` |
| EVAL-001a MCP modules | S | In each module, a change to `tools/list` bytes fails until its bytes file changes |

### Open questions

None.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| What is measured | grok: assemble sections in process; mai: a Go or Dart collector; sol: extend existing fixtures; opus: requests captured by the end-to-end harness | Captured requests | They include runtime assembly and budgeting, so no second assembly path can drift from the real one. |
| Ceiling | sol: first baseline plus at most 5%; opus: exact bytes, edited in the same pull request | Exact | Headroom lets small growth pass unreviewed, which is what the ratchet exists to stop. |
| MCP modules | opus: their own ratchet; others: silent | Their own ratchet | Root CI never reaches them, and their schemas ride on every local turn. |

## 15. F12 — Compact, navigable GitHub reads

### Verdict

Adapt, as new **INT-009 — Bounded, navigable GitHub reads**: project the two
read tools' answers and let the model ask for the next page or body slice. No
resource registry, no cache, no summarizing model call.

### DeepSeek source

- Reads project a few semantic fields instead of raw records
  (`dsh:src/sources/github-read.ts:72-78`) and offer a next-page reference
  when a page is full (`dsh:src/sources/github-read.ts:140,153`;
  `dsh:tests/github-read.spec.ts:9-13`), so an exact multiple yields one empty
  page.
- Long text pages by offset from a per-session cache of the fetched body
  (`dsh:src/app/resources.ts:103-110`; `dsh:src/app/cards.ts:88-91`). Not
  copied: the cache is a second copy of third-party text that Turing would
  have to withdraw.

### Turing today

- `github.list_issues` takes `state` and `limit` (at most 100) and no page
  (`orch/service/integrations/tools.go:23-26`;
  `orch/service/integrations/github.go:130-157`); `github.get_issue` returns
  one issue as raw JSON.
- Every answer is read to 16 KiB plus the credential's length, redacted and
  framed (`orch/service/integrations/github.go:89-94,254-260`;
  `turing-backend/internal/egress/framing.go:16`). A list is therefore a cut
  prefix of raw JSON in which avatar, URL and reaction objects crowd out the
  issues, nothing past the first page is reachable, and `/issues` mixes in
  pull requests.
- The context budget can only replace an oversized result with a marker
  (`agent-runtime-go/internal/agent/context_budget.go:183-200`).

### Unified design

In `orch/service/integrations` only:

1. **Projection, sized before it is framed.** `list_issues` asks GitHub
   for only as many issues as every frame is guaranteed to hold (point 2)
   and never holds GitHub's answer whole. It stream-decodes the array with
   `json.Decoder` (`Token` and `More`), decoding each element into a
   projection struct that has no body field, so a body is read past and
   dropped. A counting reader bounds each element: one element over 1 MiB,
   or more elements than the `per_page` it asked for, is a typed error, so
   a response is at most `per_page` MiB (about 15 MiB). A real issue fits
   well inside the
   element bound: a body at GitHub's 65,536-character limit, in four-byte
   characters, is 256 KiB. `get_issue` reads its one issue up to 1 MiB,
   then parses it. Today's read stops at 16 KiB plus the credential's length
   (`orch/service/integrations/github.go:21,89`), which a full page passes
   long before projection. The credential is redacted after parsing, from
   every decoded string the result carries, with today's length-preserving
   replacement (`orch/service/integrations/github.go:97-112`), never from
   the raw bytes: that replacement writes newlines, which are invalid inside
   a JSON string, so one issue holding the token would make the page
   unparseable. Decoding first also catches a credential written with JSON
   escapes. A list row: number, title,
   state, `is_pull_request`, author login, label names and their count,
   comment count, `updated_at` and `html_url`; no body, which `get_issue`
   reads. Each row is at most 1 KiB serialized: the title is cut to 192
   bytes and each label name to 48, measured after JSON escaping, on a
   UTF-8 boundary with the visible cut marker, and at most 5 label names
   are listed beside the total. The frame never cuts a projection.
   `FrameRetrievedContent` caps the whole frame, delimiters and
   instructions included, at `MaxFramedContentBytes` and cuts what does not
   fit (`turing-backend/internal/egress/framing.go:16,55-111`), so the
   projection is assembled against that budget first: the budget is
   `MaxFramedContentBytes` less the frame's own prefix and suffix, which are
   known from its label and instructions, less the continuation's fixed
   maximum. That budget divided by the 1 KiB row maximum is
   `rowsPerFrame`, a constant computed once from the frame's label,
   instructions and continuation (at least 15 at today's 16 KiB frame). An
   answer over either bound, or one that does not parse, is a typed error,
   never a projected prefix.
2. **Pages and continuation.** The server sends GitHub
   `per_page = min(limit, rowsPerFrame)` (`limit` defaults to
   `rowsPerFrame` when absent and is still at most 100), so every GitHub
   page it requests fits one frame whole and no result is ever cut inside
   a page. `list_issues` gains `page` (any positive integer, with no ceiling
   of its own: the run's tool-call limit bounds how far it pages, and a page
   past the end answers `complete`). Every result ends with exactly one
   continuation: `next: {page: page + 1, limit: per_page}` when GitHub's
   `Link` header has `rel="next"`, and `complete` otherwise. The
   continuation carries the `per_page` actually sent, not the `limit`
   asked for, because `page` counts pages of `min(limit, rowsPerFrame)`
   rows, and the tool's description says so. Following
   `next` from page 1 reaches every row once, as GitHub's own pages do on
   an unchanged list, and each call fetches only the next page, never one
   it already read. The server builds every URL from validated arguments;
   the `Link` header is read for `rel="next"` only and never followed.
3. **Body slices.** `get_issue` gains `body_offset` and `body_revision`.
   Its result holds the issue's projected fields (as a list row, plus
   `body_total_bytes`, `next_body_offset`, `updated_at` and `body_revision`,
   the hex SHA-256 of the decoded body after redaction) and as much body,
   from that offset, as fits the same frame budget after them, cut on a
   UTF-8 boundary and measured after JSON escaping; the offsets count raw
   body bytes, so the slices reassemble exactly. A call with a nonzero
   `body_offset` must carry the `body_revision` its first slice returned,
   and is refused with a typed `body_changed` error, holding no body, when
   the body it reads now hashes differently or the revision is missing, so
   slices of two versions are never spliced, even when an edit leaves
   `updated_at`'s one-second value unchanged. Each page, continuation or
   slice is a fresh request: nothing is cached or kept.
4. Left out: a comments tool, file paging, other providers. Each is one more
   projection when asked for.

### Security and privacy

Every page and slice goes through the same gates as the first: the run's
frozen selection, `RunAllowsIntegration` and the dispatch-time credential
check (`orch/service/integrations/call.go:127-163`), so a revoked connection
or an ended run cannot fetch page 2. Less third-party text reaches the model,
still framed as data, with the credential redacted from every decoded string.
No URL comes from the model. A body is never held: a list drops it while
streaming, and `get_issue` reads one issue under the 1 MiB bound. Owner decisions 4 and 5.

### Performance

No model call. Projection is linear in the answer, with at most one
element (1 MiB at worst) and the projected rows held at once; `get_issue`
adds one SHA-256 of a body of at most 1 MiB. One HTTP
request per call, for one GitHub page of at most `rowsPerFrame` issues
(at least 15), and no page is ever fetched twice. Bytes per call: a few
KiB of metadata per issue plus its body, so roughly 250 KiB for 15
issues with 12 KiB bodies, and about 4 MiB when all 15 bodies are at
GitHub's 65,536-character limit in four-byte characters; the per-element
bound caps a response at about 15 MiB. The GitHub client's 30-second
timeout (`orch/service/integrations/github.go:43`) is unchanged: a typical
page takes well under a second, the realistic worst case (about 4 MiB)
needs about 1.1 Mbit/s to arrive in time, and the 15 MiB bound about
4.2 Mbit/s; a slower link fails that call with a typed
error, and the next call is a fresh request for the same page. Reaching
100 issues takes at most seven calls whatever their size. The runtime's
`TURING_MAX_TOOL_CALLS_PER_RUN` defaults to 10
(`agent-runtime-go/internal/config/config.go:122`) and bounds how far a run
can page: at the default, a run lists at least 150 issues, or 100 with
three calls to spare; an owner who needs more raises it. Measured on a
recorded fixture: bytes per listed issue before and after, bytes per call,
and model calls for "compare the open issues" on a repository with more
than 100.

### Roadmap placement and dependencies

New **INT-009**, Workstream G (INT-008 is the last INT item; INT-001 is
shipped credential honesty and is not reopened). No dependencies.
PRO-001a reuses INT-009a's streamed decoder for its poller (section 3,
point 2), so INT-009a ships first.

### Delivery slices

| Slice | Size | Acceptance tests |
| --- | --- | --- |
| INT-009a Projection | S | Worst-case fixture: 100 issues with 512-byte titles of `<`, `"` and multi-byte characters, 100 labels of 50 characters each, and 64 KiB bodies (about 6.5 MiB raw) projects completely; no row exceeds 1 KiB serialized; every framed result is at most 16 KiB and never contains the truncation notice; following `next` from page 1 reaches each of the 100 rows exactly once in `ceil(100 / rowsPerFrame)` calls (at most seven), and the recorded requests carry `per_page = rowsPerFrame` with each page number requested once; `limit: 5` sends `per_page=5` and its continuation carries `limit: 5`; `limit: 50` sends `per_page = rowsPerFrame`, every continuation carries `limit: rowsPerFrame`, and following `next` from page 1 over 120 issues reaches each exactly once with each page number requested once; a recorded-shape page whose raw answer is over 1 MiB (bodies of 12 KiB with full user, label and reaction objects) projects every row; list rows hold no body; unknown fields are absent; pull requests are flagged; a secret sentinel in a body never reaches the result; the real credential in one title of a 100-issue page, and the credential written with JSON escapes, leave the other 99 rows reachable and no row containing it; an element over 1 MiB, a response with more elements than the `per_page` sent, and malformed JSON return typed errors; `rowsPerFrame` rows at the 1 KiB maximum fit one frame with the longest continuation; a `get_issue` answer over 1 MiB returns a typed error |
| INT-009b Pages and body slices | S | 120 issues are all reached by following `next`; a full last page without `rel="next"` says `complete`; page 50 answered with `rel="next"` returns `next: {page: 51, limit: per_page}`, and page 51 is accepted; a 40 KiB body, and a 40 KiB body of `<` and multi-byte characters, reassemble exactly from slices whose framed results never contain the truncation notice; a body changed between slices returns `body_changed` with no body, including when its `updated_at` is unchanged; a slice with a nonzero `body_offset` and no `body_revision` is refused the same way, and a first slice needs none; a revoked connection or an ended run refuses the next page |

### Open questions

None.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Long bodies | dsh: a per-session cache; sol: refetch with a revision check; opus: refetch | Refetch with an `updated_at` check (superseded in round 10 by a required `body_revision`; see that row) | A cache is one more copy of stranger text to withdraw; the check is one comparison. |
| Raw read size | grok: the frame cap; opus: 1 MiB, then project | 1 MiB (for lists, superseded in round 4 by a streamed decode) | One hundred raw issues exceed 16 KiB long before projection; reading only 16 KiB would leave nothing to project. |
| Row count | sol: default 20 rows | `limit` unchanged as an argument (since round 5, each call returns at most `rowsPerFrame` of them) | No evidence to change an argument the model already uses. |
| Round 3 review: the projection did not fit its frame (int009-projection-exceeds-frame, github-projection-frame-overflow, GITHUB_PROJECTION_FRAME_LOSS, int009-projection-cannot-fit-16kib-frame) | grok, mai, sol, opus: 100 rows with 512-byte previews are over 50 KiB, so the frame cut the projection, row continuity included, which is the prefix failure F12 exists to remove | No body preview in rows; rows capped at 2 KiB (1 KiB since round 4); the result is assembled against the frame's own budget and returns an explicit continuation (`next: {page, offset}` until round 5 made every page fit one frame); body slices sized the same way | A typed "ask for fewer rows" error would make the model guess a `limit`; an explicit continuation reaches every row with no guessing, and the frame then never cuts anything. |
| Round 4 review: a full page passed the 1 MiB read (int009-raw-1mib-cap-rejects-full-pages) | opus: 100 issues with real bodies exceed 1 MiB raw, so a full page was an error and the worst-case fixture could not pass | `list_issues` stream-decodes the array into body-less structs, with a 1 MiB per-element bound (and, until round 5, a 32 MiB per-response bound); `get_issue` keeps 1 MiB | A streamed decode holds one element at a time whatever the page holds; round 5 also shrank the page to what one frame holds. |
| Round 4 review: redaction broke parsing (int009-newline-redaction-breaks-projection-parse) | opus: the length-preserving redaction writes newlines, which are invalid in a JSON string, so one issue holding the token made the page unparseable and the sentinel test vacuous | Redact every decoded string after parsing | The raw bytes are never returned, and decoding first also catches an escaped credential. |
| Round 4 review: the page ceiling (INT009_CONTINUATION_EXCEEDS_PAGE_DOMAIN) | sol: `page` was limited to 50, yet page 50 with `rel="next"` returned page 51 | Any positive page; the run's tool-call limit bounds paging | The tool-call limit already bounds paging, so a second bound added nothing but a dead end. |
| Round 4 review: the tool-call budget (int009-worst-case-page-exceeds-tool-call-budget) | opus: about fifteen calls for a worst-case page exceeded the default of 10 tool calls per run | Rows capped at 1 KiB (title 192 bytes, five labels of 48), so a worst-case page takes seven calls; the default and its reach are stated | A run at the default can now read a whole worst-case page. |
| Round 5 review: each continuation fetched the whole page again (int009-continuation-refetches-full-page-under-30s-client-timeout) | opus: a continuation inside a GitHub page fetched up to 32 MiB again, about 180 MiB for a worst-case page in seven calls, under the client's 30-second timeout | The server asks GitHub for `per_page = min(limit, rowsPerFrame)`, the row count every frame is guaranteed to hold, so the continuation is the next page only; `offset` and the 32 MiB response bound are dropped; the 1 MiB element bound stays | Each issue is now fetched once; a typical 100-issue list takes seven calls instead of two or three, which the default tool-call budget covers, and the worst call shrinks from about 26 MiB to about 4 MiB. |
| Round 7 review: the continuation's page size (int009-limit-pagination-page-offset) | mai: with `limit` above `rowsPerFrame`, the continuation repeated the `limit` asked for while GitHub paged by the smaller `per_page` sent | The continuation carries the effective `per_page`; `page` always counts pages of that size | The continuation then names exactly the request that produced its page, so following it reaches every row once whatever `limit` the model first asked for. |
| Round 10 review: slices of different bodies (INT009_BODY_SLICE_REVISION_FENCE) | sol: the check between body slices was optional and compared `updated_at`, which has one-second resolution, so slices of two versions could be combined | `get_issue` returns `body_revision`, the SHA-256 of the decoded body; a call with a nonzero `body_offset` must carry it and is refused with a typed `body_changed` error on a mismatch or when it is missing | A revision bound to content cannot collide on a clock tick, as `excerpt_revision` already does for watches (section 3); the cost is one hash per call. |
| Round 11 review: the bandwidth figure (INT009-WORST-CASE-BANDWIDTH-MISCONVERSION) | mai: 1.1 Mbit/s was read as the rate for the 15 MiB bound, which needs about 4.2 Mbit/s in 30 seconds | Performance states both: about 1.1 Mbit/s for the realistic worst case of about 4 MiB, and about 4.2 Mbit/s for the 15 MiB bound | Each figure now names the size it is computed from. |
| Round 14 review: the poller's response bytes (PRO001_POLLER_RESPONSE_BYTE_BUDGET) | sol: PRO-001a's poller had no response-byte or decoding-memory bound, while this decoder already bounds both | PRO-001a's poller reuses this streamed decoder and its 1 MiB per-element bound, skipping and counting an over-bound element where `list_issues` returns a typed error (superseded in round 15: the poller treats that typed error as a malformed response; see that row); INT-009a therefore ships before PRO-001a | One bounded decoder serves both GitHub readers; see section 3's decision log. |
| Round 15 review: the over-bound element (pro001a-overbound-element-skip-incompatible-with-reused-decoder) | opus: `json.Decoder.Decode` buffers a whole value and a mid-value reader error is sticky, so this decoder cannot read past an over-bound element and continue | The decoder is unchanged: an element over 1 MiB is its typed error for both readers, and PRO-001a's poller treats it as a malformed response, failing that stream's poll with its cursor held | GitHub caps a body at 65,536 characters (at most 256 KiB), so no real element reaches the bound; see section 3's decision log. |

## 16. F14 — Purpose-gated, low-noise outward notifications

### Verdict

Adopt as acceptance text on CHN-000, CHN-002, PRS-001, UX-002 and EVAL-001.
No code and no new ID until an outbound consumer (a channel adapter, PRS-001's
delivery, a mobile push) is scheduled.

### DeepSeek source

- Replies are allowed by purpose: an acknowledgement, an answer, a result of
  a qualifying state, or the current question (`dsh:src/core/replies.ts:9-16`).
- Routine progress is never relayed (`dsh:src/app/notices.ts:115-129`).
- An over-long chat message is refused, not cut or split
  (`dsh:src/infra/text.ts:11-16`).
- "It speaks only when a person must act or can use a result"
  (`dsh:docs/000-philosophy.md:7-12`).
- Not copied: purposes chosen by the model, and word counts by segmenter.

### Turing today

Nothing is pushed anywhere. CHN-002 is a delivery ledger
(`docs/NORTH_STAR.md`, CHN-002) and PRS-001 says missed delivery stays
visible (`docs/NORTH_STAR.md`, PRS-001); neither says which events deserve a
send. The delegation join already avoids a relay per child event
(`orch/repository/delegation_join.go:28-39`), so that part is not new.

### Unified design

Acceptance for every outbound consumer:

1. A closed, host-owned set of unsolicited reasons: a usable result, an
   actionable obligation already on the UX-002 list, a due reminder, and a
   status the owner asked for. Started, progress, retrying and queued are
   never reasons. Eligibility comes from typed state and owner settings, never
   from model text.
2. Whether an automation's result is pushed is an owner setting per
   automation (default: failures only), never a model's judgement that there
   is nothing to report.
3. A send is keyed by destination, subject, reason and state version. The
   same version sends once across retries and reconnects; a newer version
   withdraws an unsent older one; what was sent stays recorded.
4. The text is orchestrator-written, with at most a preview cut visibly on a
   UTF-8 boundary. When the deliverable exceeds the destination's limit, the
   send is a pointer to the app, or it is refused and recorded locally; it is
   never cut silently or split.
5. One destination per subject, with no fallback to another. Security notices
   and consent requests are never treated as noise; a push never approves
   anything.
6. No "owner question" reason, because F13 is rejected (section 28).

### Security and privacy

Fewer sends mean fewer copies of the owner's content on third-party platforms
(CHN-000's custody point). No model can choose to notify, so injected text
cannot spam the owner's phone. Drop and refusal records are content-free.

### Performance

Zero model calls for eligibility; one ledger read per candidate; fewer sends.
Nothing until a consumer exists.

### Roadmap placement and dependencies

CHN-000, CHN-002 and PRS-001 acceptance; UX-002's badge already counts
obligations only (section 8). EVAL-001 gains "a quiet daily report sends
nothing" once a consumer exists.

### Delivery slices

Documentation only now. In the first consumer's pull request: a burst of
progress and retries sends nothing; one result sends once across a reconnect;
a replaced reminder withdraws the unsent one; an oversize deliverable sends a
pointer or is refused and shown locally; no other destination is tried;
queued, sent, dropped and uncertain stay distinct.

### Open questions

None.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Who decides there is nothing to report | sol: an explicit result disposition; grok, mai: run state; opus: an owner setting per automation | Owner setting | Deterministic and needs no model output. |
| Owner-question reason | mai, sol: include | Omitted | F13 is rejected. |
| Oversize | dsh: refuse; opus: a pointer when the message is the deliverable | Pointer or refusal | A pointer still delivers the result; a cut changes its meaning. |
| Round 3 review: roadmap citations by line (north-star-citations-point-at-wrong-items, northstar-line-citations-stale) | grok, opus: `docs/NORTH_STAR.md` line ranges for roadmap items had moved with this change's insertions | Cite roadmap items by ID | An ID survives later edits to the roadmap; the remaining line citations point at the owner decisions and invariants, above every edited item, and were re-checked. |
| Round 9 review: a source failure read as an owner's pause (HEALTH_SOURCE_FAILURE_PAUSE_MISCLASSIFIED) | sol: `paused` was defined as the owner's act, while section 3 "paused" a watch on a `401` or three failed polls, so a revoked credential read as healthy, and a `401` stopped before the threshold counted it | `paused` is only quiet or a disabled watch; a source failure suspends the watch and reports `error` with `needs_person`; a `401` does so at once because it is definitive | The remedy class exists so a person learns when they must act; a healthy-looking pause would hide exactly that case. |

## 17. F15 — Refuse a database written by a newer build

### Verdict

Adopt, as **TUR-016a**: the smallest change that makes a rollback fail closed.

### DeepSeek source

The store throws when the database's schema is newer than the build, and says
the file is left unchanged (`dsh:src/app/store.ts:56,66`); rollback therefore
needs the pre-upgrade backup (`dsh:docs/006-deployment.md:242`). Correction:
its check runs after a chmod, the WAL pragma and `CREATE TABLE IF NOT EXISTS`
(`dsh:src/app/store.ts:62-64`); Turing's runs before any migration write.

### Turing today

- `ApplyMigrationsWithSkillsRoot` may first export recovered legacy skills
  into the skills root and drop a table (`orch/db/migrations.go:32-35,100-132`),
  then creates `schema_migrations` if absent and applies each embedded
  migration it does not find; an applied version it does not embed is never
  noticed (`orch/db/migrations.go:36-51`).
- `app.New` opens, migrates and serves (`orch/app/app.go:93-101`). `db.Open`
  fixes file modes and opens in WAL mode first (`orch/db/connection.go:34-58`),
  which changes no row or schema of a database a Turing build already wrote.
- `Version` reports the binary's latest numeric prefix
  (`orch/db/migrations.go:146-167`; `orch/app/app.go:629-631`), and prefixes
  repeat (three `0012_*` files in `orch/db/schema/`), so versions are compared
  as full names.

### Unified design

The first statement of `ApplyMigrationsWithSkillsRoot`, before the skills
recovery and before `CREATE TABLE IF NOT EXISTS`: when `schema_migrations`
exists, read its versions; any version not in the embedded name set returns
`ErrDatabaseNewerThanBuild`, naming those versions, the three recoveries
below, and that deleting a `schema_migrations` row does not undo a
migration. `app.New` fails and the process
exits; Compose's restart (`turing-backend/infra/docker-compose.yml:5`) repeats
the refusal without writing. A database whose `schema_migrations` is
missing or holds no row is a fresh install only if it holds no other table
(one `sqlite_master` read, `sqlite_%` internal tables aside). One that holds
other tables but no ledger rows is refused the same way, naming the missing
ledger instead of versions: no Turing build writes one, because nothing
creates a table before the runner creates `schema_migrations`
(`orch/db/migrations.go:32-38`) and every migration commits its ledger row
in its own transaction (`orch/db/migrations.go:70-89`,
`orch/db/run_outcomes_migration.go:197-249`), so only a
ledger dropped by hand, or a file that is not Turing's, reaches it, and an
older build would otherwise re-run every migration over schema it does not
know. `db.Open` runs before either check (`orch/app/app.go:93-101`) and may
set such a file's mode to 0600 and switch it to WAL
(`orch/db/connection.go:34-58`); neither changes a table or a row, which is
all the refusal promises to leave unchanged. **Recovery, stated once:** restore the pre-upgrade backup
(for a developer, the backup taken before applying a branch migration),
recreate a disposable development database, or run the matching newer
build; there is no override flag. Deleting a `schema_migrations` row is not
a recovery: it reverses none of its migration's changes, so the older build
would pass the check with unknown changes present, and a build that embeds
the migration would run it again (`orch/db/migrations.go:43-51`), whose DDL
need not be idempotent (`ALTER TABLE … ADD COLUMN` in
`orch/db/schema/0030_delegation_egress.sql:8-11`).

Not built: a database version in `Version` or `./turing status`. Once the
check passes, the applied versions are a subset of the embedded ones and the
runner applies the rest, so the database's version is the binary's. A
refused orchestrator answers nothing; `./turing status` already reports it
unhealthy, and `./turing logs orchestrator` shows why.

### Security and privacy

An older orchestrator can no longer write approvals, consent or deletion rows
under semantics it does not enforce. The refusal exports no retained skill
text. The message holds version names only.

### Performance

One read of a few dozen rows at startup; nothing on any hot path.

### Roadmap placement and dependencies

TUR-016a, a slice of TUR-016 with no dependency on its backup work. TUR-016c
relies on it.

### Delivery slices

| Slice | Size | Acceptance tests |
| --- | --- | --- |
| TUR-016a | S | A fixture with one extra applied version makes `app.New` fail with a message that names that version, names all three recoveries (restore the pre-upgrade or pre-branch backup, recreate a disposable development database, or run the matching newer build) and says that deleting a `schema_migrations` row does not undo a migration, and the test fails a message that omits any of the three or suggests deleting a row; its rows, tables and `schema_migrations` are unchanged, and the skills root is untouched even with pending recovery rows; a database whose versions are all embedded starts; the three `0012_*` names cause no false refusal; an empty database is created normally; a database holding a table but no `schema_migrations`, and one holding a table and an empty `schema_migrations`, are each refused with a message naming the missing ledger and the recoveries, their tables, rows and `sqlite_master` are unchanged, and neither gains a `schema_migrations` table or row |

### Open questions

None.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Report the database's version | sonnet, grok, mai, sol: in `Version` and `./turing status`; opus: redundant | Not built | After the check the two versions are equal by construction, and a refused orchestrator cannot answer the RPC. |
| Split the WAL open from the check | sol: preflight before the WAL-mode open | Not needed | Opening an existing WAL database in WAL mode changes no row or schema; the check precedes every write the migration runner makes. |
| Round 3 review: TUR-016a's roadmap dependency (tur016a-roadmap-dependency-mismatch) | opus: the roadmap made all of TUR-016 depend on TUR-015, while this design ships TUR-016a any time | `docs/NORTH_STAR.md` TUR-016 now says TUR-016a has no dependency | The roadmap and the order in section 27 agree. |
| Round 27 review: a ledger-only rollback (TUR016A_MIGRATION_LEDGER_ONLY_ROLLBACK) | sol: the documented branch recovery deleted a `schema_migrations` row, which reverses none of the migration's changes and lets a later build re-run non-idempotent DDL | The recovery, stated once in the unified design, is restoring the backup, recreating a disposable development database or running the newer build; the row-deletion recipe is gone, and the slice's test asserts the message suggests none | Deleting the receipt leaves the schema the older build does not know while letting it pass the check. |
| Round 28 review: the refusal message's recoveries (TUR016A_REFUSAL_OMITS_DISPOSABLE_RECOVERY) | grok: the `ErrDatabaseNewerThanBuild` text gave two recoveries while the recovery paragraph gives three, and the test accepted the shorter text | The message names the three recoveries of the recovery paragraph and says that deleting a `schema_migrations` row does not undo a migration; the TUR-016a test fails a message missing any of them or suggesting row deletion | A developer on a branch database is most likely to need the disposable-database option. |
| Round 45 review: a database with tables but no ledger (f15-missing-migration-ledger-fail-open) | mai: any database without `schema_migrations` was a fresh install, so an older build would re-run every migration over a newer schema whose ledger was dropped; also proposed a ledger checksum witness | Fresh only when it holds no table; one with tables but no ledger is refused with the same recoveries; TUR-016a adds that fixture. No checksum witness (see the dispositions appendix) | Turing always creates the ledger first, so the branch costs one `sqlite_master` read and refuses only what no build wrote. |
| Round 46 review: the ledger-less refusal's "no other change" (tur016a-ledgerless-refusal-not-write-free) | opus: `db.Open` runs first and may set a foreign file's mode to 0600 and switch it to WAL, so a byte-for-byte check would fail | The refusal promises only that tables, rows and `sqlite_master` are unchanged and no `schema_migrations` is created, and the design names what `db.Open` may change | The check controls only what it reads; the file mode and journal mode change no row. |
| Round 52 review: an empty ledger beside a schema (f15-empty-ledger-nonempty-db) | mai: a database holding tables and an empty `schema_migrations` was neither missing its ledger nor carrying an unknown version, so it would pass | A ledger that is missing or holds no row means a fresh install only when no other table exists; otherwise it is refused the same way; TUR-016a adds the fixture | Every migration commits its ledger row with its own changes (`orch/db/migrations.go:70-89`, `orch/db/run_outcomes_migration.go:197-249`), so no build leaves tables beside an empty ledger, and the same `sqlite_master` read covers both cases. |
| Round 72 review: a read-only preflight before `db.Open` (f15-preflight-after-db-open) | mai: `db.Open` may chmod the file and switch it to WAL before the check, so the refusal is not before any write | Rejected, as in round 46 (see that row and the dispositions appendix); the summary row now says 'before any row, schema or skills write' | A database a newer Turing build wrote is already WAL and 0600, so `db.Open` changes nothing on it; only a foreign file can change mode or journal, neither touches a row, and a second open path would be new code for no row of protection. |
| Round 73 review: the roadmap's wording (TUR016_NORTH_STAR_BEFORE_ANY_WRITE, north-star-f15-write-boundary-mismatch) | grok, mai: `docs/NORTH_STAR.md` TUR-016 still said 'refused before any write', against round 72's narrowed summary | The roadmap says 'refused before any row, schema or skills write', the same words as section 2 | The roadmap and the spec must promise the same thing. |

## 18. F16 — Owner quiet switch for unattended work

### Verdict

Adapt, narrowed, as new **TUR-028 — Owner quiet switch**: one owner-set
switch that stops new unattended work. It holds nothing, moves no clock and
interrupts no running run. It answers section 3's earlier open question about
a global pause.

### DeepSeek source

- Pause is its own settings block (`dsh:src/app/settings.ts:197-213`), and
  each handler reports a limit of 0 while paused (`dsh:src/app/outbox.ts:24`).
  Sources keep recording, a running turn continues, and an evaluation's
  deadline moves later by the pause's length (`dsh:docs/003-execution.md:192-200`;
  `dsh:src/core/evals.ts:30-45`).
- Any origin may set it, a tool included; only the notice is skipped for a
  tool (`dsh:src/app/settings.ts:202-209`).
- Not copied: the held queue, moved deadlines, continued source reads, and a
  tool-settable switch.

### Turing today

- Each automation has its own enable switch
  (`orch/repository/automations.go:323-365`); turning the scheduler off needs
  `TURING_AUTOMATION_TICK_MS=0` and a restart (`orch/config/config.go:321-325`).
- Run outcomes deliberately have no pause value
  (`orch/runoutcome/outcome.go:130-139`).
- A stale `next_due_at` fires once and then jumps past missed occurrences
  (`orch/repository/automation_schedule.go:85-107`); one scheduler pass fires
  at most 20 (`orch/service/automations/scheduler.go:12-15`).
- An owner toggle already lives in the `settings` table behind a public RPC:
  `memory_enabled` (`orch/db/schema/0001_initial.sql:6-10`;
  `orch/repository/memory_settings.go:9-14,46-80`).

### Unified design

1. **The setting.** Key `unattended_quiet` in `settings`, read and written as
   `memory_enabled` is, plus a revision: TUR-028a's migration adds
   `revision INTEGER NOT NULL DEFAULT 0` to `settings`
   (`orch/db/schema/0001_initial.sql:6-10`), which only this key uses until
   OPS-002. A missing row means off at revision 0. A corrupt value counts as
   on, so it fails closed, and the app shows the switch as unavailable until
   a write repairs it; its revision stays readable, so the repairing write
   names it like any other. Public `GetUnattendedQuiet`, which returns the
   value and its revision, and `SetUnattendedQuiet`, which names the
   revision it read, only; the runtime and every internal identity are
   refused. **The quiet setter, stated once:** it is compare-and-set under
   OPS-002's contract (section 24). Its one transaction compares the named
   revision first: a stale one is refused, writes nothing and returns the
   current value and revision, and the app reloads the switch. With the
   current revision, setting the current value writes nothing and bumps no
   revision; a change writes the row with the next revision and a
   content-free audit row (`unattended.quiet.changed`, on or off), and a
   failed audit write refuses it. So a stale quiet-off that arrives after a
   quiet-on commits is refused, and quiet stays on. When OPS-002's store
   exists, the key moves there with its revision.
2. **Turning it on** is one bounded write: the same transaction also sets a
   second key, `unattended_quiet_drain`, and nothing else. That flag is the
   fence. While quiet is on **or** the drain is pending, no unattended job is
   claimed: the claim skips its second probe, the only one that can claim an
   unattended job (TUR-027), after one read of both keys, and every source
   in point 3 refuses. After the switch commits, the service drains: each
   batch, its own transaction, cancels at most 100 queued unattended jobs
   (`jobs.unattended = 1`, TUR-027) through `cancelRunTx` with a new
   abandoned code, `owner_quiet` (not `user_cancelled`), and deletes at most
   100 `watch_pending_fires` rows. The batch that finds nothing left clears
   the drain flag. A drain cut short by a crash resumes on the next
   maintenance tick (section 27), which runs even with the reaper off,
   because the flag is durable. A cancelled assessment's
   `watch_assessments` row is finished with that code and its batch stays
   coalesced. Running runs continue, unless they would be requeued (point
   3), and the owner can cancel one as today;
   a pending follow-up of a running `auto` run is gone with its row, so it
   never fires when that run ends. Nothing is held, so no queue-wait,
   consent, approval or retention clock is extended.
3. **While it is on or draining**, each source of new unattended work refuses
   inside the transaction that would start the work, or before its own
   request: `ClaimDueAutomation`
   claims nothing, so `next_due_at` stays where it was; `FireEventAutomation`
   and the pending-fire hook fire nothing (sections 6 and 7); assessor
   enqueue enqueues nothing; the watch poller sends no request, and its
   cursors and ETags stay as they were. A poll already running when quiet
   is turned on stops at once, and a page transaction that sees quiet on or
   the drain pending rolls its page back, under section 3, point 2's one
   stop rule ("Stopping a poll"), which also states what a stopped poll may
   still write. So the drain cancels every queued
   unattended job, because none is enqueued while it runs. Requeues are
   sources too. Exactly two statements in the repository package set a job
   back to `pending`: `releaseJobToPendingTx`
   (`orch/repository/jobs.go:727-764`) and a copy of the same `UPDATE` inside
   `requeueAssignment` (`orch/repository/jobs.go:1754-1844`, the statement at
   1791-1801). The first serves three transactions: recovery after a lost
   assignment (`requeueAssignmentTx`,
   `orch/repository/assignments.go:315,600-611`, through
   `requeueRunThroughRecoveryTx`, `orch/repository/jobs.go:707-719`), the
   release of an assignment never sent (`requeueUnsentAssignmentTx`,
   `orch/repository/assignments.go:294,617-631`) and a retryable failure
   (`requeueRetryableRunTx`, `orch/repository/jobs.go:554,657-680`). The
   second serves `AbortPendingAssignment` and `AbortUnsentAssignment`
   (`orch/repository/jobs.go:1743-1752`). Dispatch calls
   `AbortPendingAssignment` when a claimed assignment is released before
   delivery (`releaseClaimedAssignment`,
   `orch/service/runtime/service.go:1298,1440-1446,1675,1692`, through
   `requeueAssignments`, 1426-1438) and when `BeginAssignmentSend` fails
   (1317), and `AbortUnsentAssignment` when no send was started (1325). The
   statement's third wrapper, `RequeueClaimedJob`
   (`orch/repository/jobs.go:1739-1741`), has no production caller, only
   tests; TUR-028a deletes it and its tests rather than gating a dead path.
   It is the only caller that passes an empty `requiredExecutionState` and
   `incrementAttempt = true`, so TUR-028a also deletes everything only it
   reached: `requeueAssignment`'s `incrementAttempt` parameter (1758) and
   its attempt increment (1787-1790), the empty-state fence (1784-1786),
   the branch that requeues through the uncertain recovery lifecycle
   (1829-1836) and the comment that explains it (1823-1828). TUR-028a then
   makes `requeueAssignment`, which now always requires a delivery state,
   release through `releaseJobToPendingTx` with no increment (its statement
   is the same one with the job guard, and that guard already maps a miss
   to `ErrAssignmentFenced`), so one helper is the only way back to
   pending. That helper reads the two keys first; while quiet is on or
   draining, it does not release an unattended job but cancels its run
   through `cancelRunTx` with `owner_quiet`, in the same transaction, and
   returns a sentinel on which each of its callers skips its requeue
   lifecycle step and commits. `cancelRunTx` writes no execution column
   (`orch/repository/run_terminal.go:358-370`). **The execution release,
   stated once:** every step the quiet branch skips is a requeue, and every
   requeue, the proven release and the uncertain recovery edge alike,
   clears the run's execution containment through the shared
   `requeueTransition` (`orch/repository/run_state.go:1143-1158`; the
   uncertain edge reaches it through `requeueRunLifecycleTx`,
   `orch/repository/jobs.go:771-791`). So the quiet branch's transaction
   always also writes the release a failure writes
   (`execution_active = 0`, `execution_exit_acknowledged_at` set,
   `execution_state = 'exited'` and the execution lease cleared,
   `orch/repository/run_terminal.go:255-261`), so the cancelled run frees
   its execution slot at once, exactly as the requeue it replaces would
   have. That covers `AbortPendingAssignment` and `AbortUnsentAssignment`;
   the `pending_send` release when a worker disconnects
   (`requeueUnsentAssignmentTx`, `orch/repository/assignments.go:289-301`,
   reached without the reaper, `orch/service/runtime/service.go:390`); the
   lost-assignment requeue (`requeueAssignmentTx`, line 315), which only
   stale recovery reaches (a heartbeat reconcile, startup recovery or the
   reaper's cutoff), because a disconnect from a delivered assignment only
   fences it `uncertain` and keeps its containment (lines 257-269), as
   today, and requeues nothing; and a retryable failure, confirmed or not
   (`requeueRetryableRunTx`, `orch/repository/jobs.go:657-680`). The
   sentinel carries the cancellation's events (an error type holding the
   `[]Event` that `cancelRunTx` returns). The retryable-failure and
   reconciliation callers publish them where they publish their own today
   and report a non-requeued outcome with no retry notice
   (`RetryDecision{Events: …}`,
   `AssignmentReconciliation{Cleared: true, Events: …}`,
   `orch/repository/jobs.go:419-425`,
   `orch/repository/assignments.go:13-19`), so pending work is still
   dispatched after it. `requeueAssignment`, behind the two aborts,
   consumes the sentinel itself, commits and returns nil, because those
   APIs return only an error and publish nothing live today
   (`orch/repository/jobs.go:1804-1809`): an abort's quiet cancellation is
   seen on replay, as an abort's requeue is, and its callers keep
   dispatching as after a requeue. An executor that may still be
   running a cancelled unattended run, after a lost lease, is in the same
   position as after today's uncertain requeue: its updates are refused,
   because its run is terminal. `owner_quiet` is never a committed stop:
   quiet never stops a run its worker still owns (point 2); it cancels only
   work a requeue would have released. So no unattended run returns to the queue
   during quiet, before or after the drain finishes, to start once quiet
   ends, whichever path would have requeued it. The lifecycle
   sweep and the rest of the maintenance tick, the job reaper, queue expiry and every attended path (owner turns,
   Start-confirmed turns, delegation children, continuations) are
   unaffected.
4. **Turning it off** fires nothing by itself, and unattended work resumes
   only once the drain has finished, so a quick off after on never lets an
   old queued job start. Each due automation fires at
   most once at its next tick, at most 20 per pass, behind attended work
   (TUR-027). Pollers resume from their stored `since` within their
   per-poll budget, under section 3's one resume rule: an item first read
   more than 15 minutes after its creation, and less than 24 hours after
   it, records a proposal and never fires `auto` (section 7, "What never
   fires", the one delayed-read rule); an older one the list
   returns is only counted `stale` on the watch's status; a backlog larger
   than the budget shows `backlog`, and the clamp then skips it as a
   content-free `skipped_interval` and deletes the watch's pending fires.
5. **App.** One switch on the Automations page; its state is shown there and
   on each watch. UX-002 does not list it, because it is the owner's own
   choice, not an obligation; an `owner_quiet` cancellation raises no
   attention row (section 8). TUR-014 reports a watch it holds under
   section 21's one status rule (`paused`, unless the watch is suspended).

### Security and privacy

It only removes authority, egress and model use. Only the owner can set or
clear it, so injected text such as "unpause" does nothing (owner decisions 2
and 7), and a write from a stale view cannot turn it off (point 1). The audit row is content-free. Astra's objections are answered by
construction: nothing is held, so nothing expires while held; no consent or
deadline moves; the poller reads nothing while quiet; and resuming fires each
automation at most once and fires `auto` only for events first read
within 15 minutes of their creation (section 7's delayed-read rule): a
quiet longer than that turns what it held into proposals, and a shorter
one lets those events fire, as TUR-028b tests.

### Performance

One primary-key read of the two keys inside each fire, enqueue and
requeue transaction, before each poll request and in each poll page's
commit (section 3, point 2), and before a claim's second probe. Turning it
on is one bounded write; the drain then runs cancellation batches of at most
100 jobs and 100 pending fires, each its own transaction, so no transaction
grows with the backlog (the database has one connection,
`orch/db/connection.go:47`) and attended work interleaves between batches.
Nothing on the attended probe. While quiet, poll requests and newly started
unattended runs drop to zero; an unattended run already running when quiet
was turned on keeps making its model and tool calls until it ends (point 2),
so unattended model calls fall to zero only once those runs finish.
Measured: poll requests and newly started unattended runs in a quiet hour
(zero), and fires per automation in the first tick after (at most one).

### Roadmap placement and dependencies

New **TUR-028**, Workstream A. It depends on TUR-027, whose `jobs.unattended`
defines what it cancels, and ships before a PRO-001 watch polls a live token.
TUR-016c does not depend on it.

### Delivery slices

| Slice | Size | Acceptance tests |
| --- | --- | --- |
| TUR-028a Switch and scheduler | S | The maintenance goroutine (section 27) is added here unless TUR-026b already added it; A queued automation run is cancelled `owner_quiet` and raises no attention row, while a running one continues and keeps making model calls to its end; over a quiet hour with one unattended run already running and a due automation, no unattended run starts; with 10,000 queued unattended jobs, turning quiet on commits in one bounded transaction, every drain transaction cancels at most 100 and stays within its stated budget, an attended job enqueued during the drain is claimed before it finishes, and all 10,000 end `owner_quiet`; quiet turned off immediately after on starts none of them; a crash mid-drain leaves the fence in place and the next maintenance tick finishes the drain, also with `TURING_JOB_REAPER_INTERVAL_MS=0`, after which turning quiet off lets a due automation fire once; a due automation does not fire while quiet and fires exactly once after; a daily automation quiet for three days fires once; a due claim racing the switch either does not fire or is cancelled by the drain; an unrelated queued job's queue-wait expiry is unchanged; a lease loss on a running unattended run while quiet is on, a retryable failure of one after the drain finished, and, after the drain finished, `AbortPendingAssignment` (a `BeginAssignmentSend` failure, and a claimed assignment released before delivery), `AbortUnsentAssignment` (no send started) on a claimed unattended job, each cancel the run `owner_quiet` instead of requeuing it, raise no attention row, and with quiet then turned off none of them starts; the two aborts, a retryable failure (confirmed and unconfirmed), the lease loss, and a worker disconnect holding a claimed unattended assignment still `pending_send` each leave the cancelled run with `execution_active = 0` and `execution_state = 'exited'`, also with `TURING_JOB_REAPER_INTERVAL_MS=0`, so an attended job enqueued next is claimed at once without a restart; a disconnect holding a delivered unattended assignment during quiet leaves it contained (`recovering`, `execution_state = 'uncertain'`, `execution_active = 1`) with nothing claimed into its slot, and its later stale recovery (a heartbeat reconcile, startup recovery or the reaper's cutoff) cancels it `owner_quiet` with the execution release instead of requeuing it; a subscribed session receives the quiet cancellation's `agent.run.cancelled` with `owner_quiet` live on the retryable-failure and reconciliation paths, with no retry notice written, and sees an abort's on replay; after a quiet-cancelled release before delivery (`releaseClaimedAssignment` at `orch/service/runtime/service.go:1298`, after which the caller returns `s.DispatchPending(ctx)` at 1304), with the reaper off, an attended pending job is claimed in the same dispatch pass, while after a quiet-cancelled `BeginAssignmentSend` failure that is not a fence (`AbortPendingAssignment` then `return err`, 1317-1318), which dispatches nothing today, while the fence branch keeps its same-pass `DispatchPending` (1315), the run is cancelled `owner_quiet`, its slot is released, and the attended job is claimed on the next dispatch trigger, as today; while an attended run's lease loss and an attended `AbortPendingAssignment` during quiet requeue as today; `RequeueClaimedJob` no longer exists, and `requeueAssignment` has no `incrementAttempt` parameter, no empty-state fence and no uncertain-recovery branch, while an attended `AbortPendingAssignment` and `AbortUnsentAssignment` still requeue with the job's attempt unchanged and the run's unsent lifecycle; a source guard parses every non-test `.go` file of the repository package with `go/parser`, takes each string literal, collapses its whitespace runs to one space, and finds every `UPDATE jobs SET … WHERE` whose `SET` clause (the text between `SET` and `WHERE`) assigns `status = 'pending'`, case-insensitively, and asserts exactly one such statement, inside the function `releaseJobToPendingTx`, so the guard fails if it finds none (it cannot pass vacuously), if a second one appears, and on today's code with its two multi-line statements; behind it, the requeue tests above prove each path's outcome; the setting survives a restart; a corrupt value blocks fires and shows unavailable; setting the same value with the current revision writes no audit row and bumps no revision; with quiet off at revision r, quiet turned on (committing r + 1) and then a `SetUnattendedQuiet(off)` naming r is refused with the current value and revision, writes nothing, quiet stays on, and no unattended job is claimed; a failed audit write refuses the change; the runtime identity is refused; a chat turn, a delegation child and a continuation run while quiet |
| TUR-028b Watch gates | S | With PRO-001a, c and d: the tests named in their rows (no poll request and unchanged cursors, and quiet turned on while page 2 of a slow poll downloads cancels that request, sends no later request, and commits no event, state or page row and no write other than at most the rotation index (only when it changes), so its `since`, ETags and event rows are unchanged and the next poll after quiet ends starts at the repository after the stopped one; no assessment, and the held batch assessed once after; no `auto` fire, no replay after; with quiet held 48 fake-clock hours over a backlog that fits one poll's budget, an event created 30 hours before it ended is returned and counted `stale`, one created 2 hours before records a proposal and fires nothing, and, on an `auto` watch, one created 10 minutes before it ended and first read 12 minutes after its creation fires its matching rule; over a backlog larger than the budget, the stream shows `backlog` and the clamp records a `skipped_interval` and deletes the watch's pending fires); and TUR-028a's two quiet fixtures, which ship before any watch exists, repeated with a polling watch: over a quiet hour no poll request is sent, and after a refused stale-revision `SetUnattendedQuiet(off)` none is sent either |

### Open questions

None.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Adopt now | sonnet, grok, mai, sol, opus: adopt, narrowed; astra: not now, per-source switches suffice | Adopt, narrowed | Once PRO-001 adds watches and an assessor, silencing everything takes one action per source and restoring it correctly takes as many again; the narrowed switch has none of the lifecycle hazards Astra named. |
| Queued unattended runs | grok, sol, mai: they finish or expire normally; opus: cancel them when quiet is turned on | Cancel | Quiet that still spends the only slot on queued unattended work is not quiet; a cancellation is an existing terminal state, so nothing is held. |
| Where the check sits | opus: `Scheduler.Tick` returns early; sol: inside the claim and fire transactions | Inside the transactions | SQLite serializes them with the switch's own transaction, so a racing fire is either refused or cancelled. |
| A revision on the setting | sol: yes | No | Setting a boolean is idempotent; F22's compare-and-set is for multi-field groups. Superseded in round 31: the setter names its revision (see that row). |
| Events seen after resume | grok, sol, mai: the poller resumes within its budget; opus: they never fire `auto` | Opus (superseded in round 11 by section 7's delayed-read rule; see that row) | Ending quiet must not become a burst of unattended runs. |
| Rehearsal | mai: TUR-016c needs it; grok, sol, opus: no | No | The rehearsal runs no scheduler or poller at all (section 25). |
| Round 3 review: one unbounded cancel transaction (QUIET_UNBOUNDED_CANCEL_TRANSACTION) | sol: cancelling every queued unattended job inside the switch's transaction scaled with the backlog on the one database connection | The switch writes a durable drain flag that fences claims and sources; cancellation runs in batches of 100, resumed by the reaper after a crash | The fence makes the guarantee atomic, so the batches can be small; a stated queue ceiling instead would refuse unattended enqueues the owner never asked to cap. |
| Round 3 review: a pending fire outlived quiet (QUIET_PENDING_FIRE_SURVIVES_TOGGLE) | sol: a pending follow-up of a running `auto` run survived quiet turned on and off, and fired when the run ended | The drain deletes every `watch_pending_fires` row, and the hook refuses while quiet or draining | Deleting the rows is smaller than a quiet generation on each row, and quiet promises that nothing from before it starts unattended work. |
| Round 4 review: requeues escaped the drain (QUIET_RECOVERY_REQUEUE_ESCAPES_DRAIN, tur028-recovery-requeue-held-through-quiet) | sol, opus: a running unattended run that lost its lease, or failed retryably, went back to pending during quiet, where the claim gate held it until quiet ended, even after the drain had finished | The three requeue transactions that share `releaseJobToPendingTx` cancel an unattended run `owner_quiet` instead while quiet is on or draining (round 5 found a fourth path; see the next row) | Quiet promises that nothing from before it starts unattended work. |
| Round 5 review: the assignment-abort requeue escaped quiet (tur028-presend-requeue-escapes-quiet, tur028-abort-assignment-requeue-bypasses-quiet) | grok, opus: `requeueAssignment`, behind `AbortPendingAssignment`, `AbortUnsentAssignment` and `RequeueClaimedJob`, runs its own `UPDATE` to pending, so a claimed unattended job aborted before send after the drain was held until quiet ended and then started | `requeueAssignment` releases through `releaseJobToPendingTx`, which holds the one quiet check, so every path back to pending (point 3 names them all) cancels instead; a test pins that no other statement sets a job pending | Routing the duplicate statement through the helper is smaller than a second copy of the check, and a future requeue path cannot skip it. |
| Round 4 review: the resume rule (quiet-resume-proposals-lost-to-stale-skip, pro001-stale-skip-drops-quiet-period-events) | grok, opus: point 4 promised a proposal for every event created while quiet | Point 4 points to section 3's stale rule, which wins | See section 3's decision log. |
| Round 6 review: quiet's metric, the dead requeue path and a vacuous guard (f16-running-calls-quiet-metric, tur028-requeueclaimedjob-dead-path-misattributed, tur028a-pending-statement-guard-test-vacuous) | mai: Performance promised zero unattended model calls while running runs continue; opus: `RequeueClaimedJob` was named as a dispatch path but has no production caller, and the one-line literal search matched neither multi-line production statement | The metric is zero newly started unattended runs and zero polls, and a running run keeps its calls; TUR-028a deletes `RequeueClaimedJob`; the guard parses non-test sources, normalizes whitespace, matches the `SET` clause and requires exactly one statement, inside `releaseJobToPendingTx` | Each claim now says what the code does: a running run is never interrupted, a dead path is removed rather than tested, and a guard that must find exactly one match cannot pass by finding none. |
| Round 7 review: dead branches left after deleting `RequeueClaimedJob` (tur028a-requeueassignment-dead-branches-after-delete) | opus: `RequeueClaimedJob` was the only caller passing an empty state and `incrementAttempt = true`, so deleting it left the parameter, the empty-state fence and the uncertain-recovery branch unreachable in `requeueAssignment` | TUR-028a deletes them with it, and its acceptance asserts they are gone and that both aborts still requeue as today | Code no caller reaches is deleted, not kept beside the one helper that now owns the way back to pending. |
| Round 10 review: the quiet resume and the backlog clamp (pro001-clamp-stale-count-unsatisfiable, PRO001_CLAMP_MAKES_STALE_COUNTS_UNOBSERVABLE) | grok, sol: TUR-028b counted as `stale` an event the clamped `since` never returns | Point 4 and TUR-028b resume from the stored `since`: a backlog that fits the budget counts its returned day-old items `stale`; one over the budget reaches `backlog`, and the clamp records a `skipped_interval` and deletes the watch's pending fires | See section 3's decision log. |
| Round 11 review: one delayed-read rule (pro001d-suspension-resume-replays-backlog-into-auto) | opus: only quiet kept held events out of `auto`, so a resumed suspension or a drained backlog could fire stale unattended runs | Point 4 and TUR-028b point to section 7's one rule: an event first read more than 15 minutes after its creation never fires `auto`, after quiet or any other delay; an event created in quiet's last minutes and read within 15 minutes of its creation is fresh and may fire | See section 7's decision log. The burst quiet could release is bounded by those 15 minutes and the per-watch rate cap. |
| Round 17 review: a poll in flight when quiet is turned on (pro001a-inflight-poll-survives-quiet-disable-delete, pro001a-inflight-poll-survives-disable-quiet-delete) | grok, opus: quiet was read only before each poll, so a poll already running kept sending requests and committing pages | Turning quiet on cancels a poll in flight, and the two keys are read before every poll request and in each page's commit, as section 3, point 2 states once; TUR-028b gains the mid-poll fixture | See section 3's decision log. |
| Round 21 review: where a fire refuses (PRO001_DIRECT_FIRE_SECOND_TRANSACTION_REOPENS_PAGE_ROLLBACK) | grok: see section 6's decision log | Point 3 says each source refuses inside the transaction that would start the work, because a direct fire now runs inside the poller's page transaction rather than its own (section 6, point 1) | The quiet check still sits where the fire commits, so SQLite still serializes it with the switch. |
| Round 22 review: what a poll stopped by quiet writes (PRO001_STOP_COMMITS_NOTHING_CONTRADICTS_ROTATION_WRITE, pro001a-stop-path-rotation-write-contradicts-commit-no-row-tests) | grok, opus: point 3 and TUR-028b said a stopped poll commits nothing, while section 3 writes the rotation index when a poll stops | Point 3 and TUR-028b say the stopped poll commits no event, state or page row and its only write is the rotation index (refined in round 23: point 3 points to section 3's stop rule instead; superseded in round 24: TUR-028b asserts no write other than at most the rotation index; see those rows); TUR-028b also asserts the next poll after quiet starts at the repository after the stopped one | See section 3's decision log. |
| Round 23 review: quiet seen by a page transaction (pro001d-quiet-listed-as-committed-refusal-contradicts-stop-rollback) | opus: see section 6's decision log | Point 3 points to section 3's one stop rule for a poll in flight and for a page transaction that sees quiet or a pending drain, instead of restating a stopped poll's writes | See section 3's decision log. |
| Round 24 review: what a quiet stop writes (pro001a-stop-asserts-rotation-write-when-index-unchanged, pro001-stopped-poll-rotation-write-assertions-fixture-dependent) | grok, opus: see section 3's decision log | TUR-028b asserts no write other than at most the rotation index (only when it changes) | See section 3's decision log. |
| Round 26 review: a suspended watch while quiet is on (pro001-quiet-plus-suspension-status-precedence-undefined) | opus: see section 3's decision log | Point 5 points to section 21's one status rule: `paused` only for a watch that is not suspended | Quiet is the owner's act; a suspension still needs a person. |
| Round 31 review: a stale write turned quiet off (SETTINGS_TOGGLE_STALE_WRITE_BYPASSES_CAS) | sol: the quiet setting was exempt from revisions, so an older `SetUnattendedQuiet(false)` arriving after the owner turned quiet on turned it off | Point 1 states the quiet setter once: `GetUnattendedQuiet` returns a revision, `SetUnattendedQuiet` names the one it read and is compare-and-set under section 24's contract, a stale revision is refused before the equal-value no-op, and a `revision` column on `settings` holds it until OPS-002; TUR-028a adds the interleaving test | Setting a boolean is idempotent only without a race; quiet is a safety switch, so a stale view must never undo it. |
| Round 46 review: TUR-028a's watch assertions (TUR028A_WATCH_ACCEPTANCE_PRECEDES_WATCH) | sol: TUR-028a asserted that no poll request is sent, but ships before PRO-001a adds the poller | TUR-028a keeps the scheduler, claim, requeue and settings tests; its quiet-hour and stale-revision fixtures are repeated with a polling watch in TUR-028b, which runs with PRO-001a | Each slice's tests can be written when it lands (round 3's rule; round 45 applied it to UX-002a). |
| Round 53 review: the drain resume with the reaper off (tur028-quiet-drain-stuck-when-reaper-disabled, QUIET_DRAIN_RECOVERY_REQUIRES_DISABLED_REAPER) | grok, sol: see section 3's decision log | The drain resumes on the maintenance tick (section 27), which runs with the reaper off; TUR-028a's crash fixture repeats with `TURING_JOB_REAPER_INTERVAL_MS=0` | A durable fence needs a loop that always runs. |
| Round 54 review: quiet cancellation of an unsent assignment kept its execution slot (TUR028_UNSENT_CANCEL_RETAINS_EXECUTION_SLOT) | sol: the release that quiet replaces clears execution containment (`orch/repository/run_state.go:1151-1158`), but `cancelRunTx` writes no execution column, so an assignment aborted before delivery during quiet stayed `execution_active = 1` with no worker to acknowledge it | For `AbortPendingAssignment` and `AbortUnsentAssignment`, which prove no executor exists, the quiet branch also writes the release a failure writes (`orch/repository/run_terminal.go:255-261`) (extended in round 55 to every proven release; see that row); a caller where an executor may exist keeps containment; TUR-028a asserts both | Containment must hold exactly while an executor may exist, and no longer. |
| Round 55 review: the other proven releases (tur028-pending-send-reconcile-retains-execution-slot, TUR028_CONFIRMED_RETRY_CANCEL_RETAINS_EXECUTION_SLOT, tur028-unsent-reconcile-cancel-keeps-containment) | grok, sol, opus: round 54 named only the two aborts, but the `pending_send` release on a worker's disconnect (`orch/repository/assignments.go:289-301`) and a confirmed retryable failure (`orch/repository/jobs.go:657-680`) also skip a release that clears containment, so quiet left those runs holding the slot, with no reaper to free it | Point 3 states the rule once by the skipped step: a skipped proven release gets the failure-style execution release in the same transaction, and only the uncertain recovery edge keeps containment (corrected in round 56: the uncertain edge clears containment today too, so every skipped requeue gets the release; see that row); TUR-028a tests all four proven releases and both uncertain cases | Keying the rule on the transition replaced, not on a list of callers, covers every path that proves no executor and no path that cannot. |
| Round 56 review: the uncertain edge clears containment today (tur028-uncertain-edge-containment-not-as-today) | opus: the uncertain recovery edge reaches the same `requeueTransition` as the proven release (`orch/repository/jobs.go:771-791`, `orch/repository/run_state.go:1143-1158`), so round 55's 'keeps its containment, as an attended run does today' was false, and a disconnect during quiet with the reaper off would have held the slot until restart | Every requeue the quiet branch replaces gets the failure-style execution release, as the requeue would have cleared it; `owner_quiet` is not a committed stop, because quiet never stops a run its worker still owns; TUR-028a asserts every path with the reaper off | The quiet branch must leave the slot exactly as the requeue it replaces would. |
| Round 57 review: a delivered disconnect, and the cancellation's events (tur028-delivered-disconnect-never-requeues, tur028-quiet-cancel-events-unpublished) | opus: a disconnect from a delivered assignment only fences it `uncertain` with containment held (`orch/repository/assignments.go:257-269`), so round 56's test that it frees the slot could not pass, and the lost-assignment requeue is reached only by stale recovery; the sentinel carried no events, so a quiet cancellation was never published live | Point 3 says where each requeue is reached, and the quiet branch applies at the stale recovery of a delivered assignment, not at its disconnect; the sentinel carries `cancelRunTx`'s events, which each caller publishes with a non-requeued outcome and no retry notice; TUR-028a asserts both | Containment is released only where today's code would release it, and a cancellation the owner cannot see live is a lifecycle the client invents. |
| Round 58 review: the abort path's quiet outcome (tur028-abort-quiet-sentinel-outcome-undefined) | opus: `AbortPendingAssignment` and `AbortUnsentAssignment` return only an error and publish nothing live (`orch/repository/jobs.go:1804-1809`), so a sentinel there would either stop dispatch or lose its events | `requeueAssignment` consumes the sentinel, commits and returns nil, so dispatch continues; an abort's quiet cancellation is seen on replay, as an abort's requeue is today; TUR-028a narrows the live assertion to the retryable-failure and reconciliation paths and asserts an attended job is claimed in the same dispatch pass after a quiet-cancelled abort | Widening the abort signatures is the streaming task's work, which the code already names; the quiet branch keeps their contract. |
| Round 59 review: which abort dispatches in the same pass (tur028a-abort-same-pass-dispatch-path-unspecified) | opus: `releaseClaimedAssignment` returns `DispatchPending`, but a `BeginAssignmentSend` failure that is not a fence dispatches nothing today (`orch/service/runtime/service.go:1298,1304,1317-1318`), so the same-pass assertion could not hold for it | TUR-028a asserts same-pass dispatch only after a release before delivery; after that `BeginAssignmentSend` failure it asserts the cancellation and the freed slot, with the attended job claimed on the next dispatch trigger, as today | The quiet branch changes what a requeue becomes, not when its caller dispatches. |
| Round 60 review: the dispatch citations (tur028a-cited-dispatch-ranges-wrong) | grok: TUR-028a cited `orch/service/runtime/service.go:1297-1303` for the same-pass dispatch, whose `return s.DispatchPending(ctx)` is at 1304, and 1309-1320 for the non-dispatching abort, a range that includes the fence branch's `DispatchPending` at 1315 | TUR-028a cites 1298 and 1304 for the release before delivery and 1317-1318 for the non-fence abort, and keeps the fence branch's same-pass dispatch | A test aimed at a wrong range tests the wrong branch. |
| Round 61 review: who adds the maintenance goroutine (tur028a-maintenance-goroutine-owner-not-a-roadmap-dependency) | opus: section 27 gave the goroutine to TUR-026b, but the roadmap lets TUR-028a ship first, and its crash-resume test needs the loop | Whichever of TUR-026b and TUR-028a ships first adds it, and both slices' rows say so | The roadmap orders neither; the design must not rely on an order it does not require. |
| Round 65 review: two summaries that denied the delayed-read fires (QUIET_RESUME_SECURITY_CLAIM_CONTRADICTS_DELAYED_READ_FIRE) | grok: section 7 and this section's security analysis said resuming never replays held events into `auto`, while section 7's rule and TUR-028b's fixture fire an event first read within 15 minutes of its creation | Both sentences now state the 15-minute rule: a hold longer than that turns events into proposals, and a shorter one lets them fire as they would have | A security summary must describe the mechanism the tests encode, not a stronger one. |

## 19. F17 — Sticky dismissal of watch proposals

### Verdict

Adapt, as PRO-001b acceptance: the owner's "no" on a thread outlasts its next
comment.

### DeepSeek source

- A Ticket a person stops "stays ended and receives nothing more"
  (`dsh:docs/002-authorization.md:129`), and a person's pause holds across
  follow-ups and restarts until a person lifts it (`dsh:docs/004-prompts.md:86`).
- Correction: DeepSeek has no proposal dismissal; its only dismissal hides an
  unavailable Decision (`dsh:docs/002-authorization.md:155`). This adapts the
  stop rule; it ports no mechanism.

### Turing today

The previous revision offered Dismiss on a proposal (section 8, stage 5) but
said nothing about later events, and its coalescing (section 6) would have
reopened the proposal at the thread's next comment.

### Unified design

1. **Dismiss** (owner only, public facet, content-free audit) closes the
   thread's open proposal, clears its excerpt-derived fields, sets
   `watch_threads.dismissed_at` and increments the thread's
   `dismiss_generation`, in one bounded transaction that touches only
   those two rows and writes its content-free audit row
   (`recordAuditTx`, `orch/repository/audit.go:205`). **A dismissed event, stated once:** an event whose
   thread's `dismissed_at` is set, or whose stored `dismiss_generation`
   (copied from its thread when it was admitted, section 3, point 6) is
   below its thread's. Nothing lowers or resets the counter, so every
   event admitted before a Dismiss stays dismissed after Watch this thread
   again (point 3). The lifecycle sweep (section 3, point 8) then
   clears the excerpts and label names of the thread's events that started
   no run, in its batches (at most 100 rows each, repeated within a tick
   while due work remains, up to 1 s; section 3, point 8), updating
   `watches.excerpt_bytes` as it
   goes. Until then nothing reads them: every path that would (Start,
   Confirm, an assessment's claim or result, an edit's replacement, a
   pending fire) refuses a dismissed event (point 2), and so does the
   sweep's selection, which reads the generation, not `dismissed_at`. A thread session and its
   copies keep their own lifecycle (section 3, point 8), so dismissing a
   later proposal never deletes work the owner started.
2. While `dismissed_at` is set, the gate refuses the thread's events with
   `thread_dismissed`: a content-free row, no excerpt, proposal, assessment
   or `auto` fire. The pending-fire hook drops the thread's row (section 6);
   an assessment still queued ends `assessment_stale` at claim, with no
   model call, and one finishing afterwards records no proposal (section 5); a stale
   Confirm is refused, because its proposal is closed.
3. **Watch this thread again**, on the watch page, clears `dismissed_at`
   and leaves `dismiss_generation`. Events refused in the meantime are not
   replayed, and events admitted before the Dismiss stay dismissed (point
   1): an assessment queued or running from them still ends
   `assessment_stale` or records nothing, their pending fire is dropped,
   no batch or proposal includes them, and the sweep still clears their
   unused excerpts. The next event proposes as usual, alone. Mode changes, re-enabling the watch and restarts leave the
   dismissal in place. Deleting the watch keeps the row until the sweep
   has begun its thread session's deletion (section 3, point 8).

Ceiling: one `watch_threads` row per dismissed thread for the watch's life,
holding ids, a timestamp and a counter, and one integer on each admitted
event.

### Security and privacy

It bounds attention abuse by a listed but noisy author, stores no text from a
dismissed thread, and removes the unused excerpts and their label names
within a few sweep ticks
instead of after 30 days, while nothing reads them in between. Only the owner sets or lifts it; no comment can.

### Performance

One read of the `watch_threads` row the poller already reads for coalescing.
It saves an assessment, tens of seconds on the one slot, per later event.

### Roadmap placement and dependencies

PRO-001b acceptance (+S).

### Delivery slices

In PRO-001b: after Dismiss, two events give two `thread_dismissed` rows and
no excerpt, proposal or assessment; the Dismiss transaction writes only the
marker, the proposal and its audit row, which holds no excerpt or label text,
and updates no `watch_events` row, and the sweep clears the unused excerpts in batches
of at most 100 while the thread session remains, and a sentinel label name
on a dismissed event is found nowhere in the database once the sweep has
cleared it; on a thread holding 43,200
unused excerpts, Dismiss's transaction writes exactly those three rows, no sweep transaction clears more than 100, the sweep's selection uses the `(watch_id, thread_key, dismiss_generation)` index in `EXPLAIN QUERY PLAN` and each batch stays within 50 ms with 500,000 admitted rows in other watches and threads, and, on a re-watched thread holding 43,200 current-generation excerpts and 100 pre-Dismiss ones, the selection seeks only the 100, each batch stays within 50 ms, and a tick with no dismissed work left reads no current-generation row and clears `dismiss_sweep_pending`; on a re-watched thread with one run-started pre-Dismiss event and 100 unused ones, the flag clears once the 100 are cleared, later ticks read no row of that thread, and the run-started excerpt is cut at its 30-day expiry; and no Start, Confirm
or edit reads an excerpt before the sweep reaches it; after Watch this thread again, the next event
proposes, and its batch holds no event admitted before the Dismiss; a restart or a mode change keeps the dismissal; another thread is
unaffected. In PRO-001c, which adds the assessor: no assessment result reads an excerpt of a dismissed thread before the sweep reaches it, and, with the sweep held, a Dismiss of a thread with a queued assessment followed by Watch this thread again before the claim ends that assessment `assessment_stale` with no model call, a result arriving after the rewatch records no field, and the sweep then still clears the pre-Dismiss excerpts. In PRO-001d: a pending `auto` fire on a dismissed thread fires
nothing, and still fires nothing when the owner watches the thread again
before the hook runs.

### Open questions

None.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Later events | sonnet, grok, mai: count them on the thread; sol: content-free receipts; opus: the gate refuses them | Gate refusal `thread_dismissed` | Refused rows are already content-free and counted; no second counting path. |
| Existing copies | opus: withdraw the thread session; sol: clear the proposal's fields | Clear the proposal and unused excerpts; keep the thread session | The owner may have started work on the thread before dismissing a later proposal. |
| Revision-bound dismissal | sol, mai: bind Dismiss to an occurrence | Bound to the thread until lifted | A thread-level "not this" is what the owner asked for; F18's occurrence binding is for failures, which recur. |
| Round 5 review: deleting the watch removed this row too early (pro001-watch-delete-orphans-thread-sessions) | opus: deleting the watch deleted this row before its thread session was withdrawn | The row stays until the sweep has begun its session's deletion | See section 3's decision log. |
| Round 7 review: Dismiss cleared a whole thread in one transaction (WATCH_DISMISS_UNBOUNDED_TRANSACTION) | sol: one thread can hold 43,200 excerpts (64 MiB), and clearing them inside Dismiss would hold the one database connection | Dismiss commits only the marker and the proposal's closure; the lifecycle sweep clears the excerpts in its bounded batches | Every read already refuses a dismissed thread, so a few ticks' delay exposes nothing, and the sweep is the one bounded cleaner that already exists. |
| Round 13 review: label names outlived the excerpt (WATCH_LABEL_SNAPSHOT_RETENTION) | sol: an admitted event kept its label names, third-party text, after Dismiss and expiry cleared its excerpt | The Dismiss sweep clears each unused event's label names with its excerpt; a sentinel test proves none remains | See section 3's decision log. |
| Round 14 review: a Dismiss while an assessment waits (PRO001_ASSESSOR_INPUT_DISPATCH_REVISION_FENCE) | sol: a queued assessment built before the Dismiss still sent the thread's text to the model | Point 2: an assessment still queued at the Dismiss ends `assessment_stale` at claim with no model call; the claim is among the paths that refuse a dismissed thread | See section 5's decision log. |
| Round 48 review: Watch this thread again revived pre-Dismiss work (WATCH_DISMISS_REWATCH_REVIVES_OLD_WORK) | sol: cleanup after Dismiss is asynchronous, and every check (assessment claim and result, pending fire, sweep) read only `dismissed_at`, which the rewatch clears, so a rewatch before the sweep or the claim let pre-Dismiss text reach the model, a proposal reopen, or an old pending event start work | Dismiss also increments `watch_threads.dismiss_generation`, which nothing resets; each event stores the counter at admission; point 1 defines a dismissed event once (thread marker set, or the event's generation below the thread's), and every check and the sweep's selection use it; PRO-001b and PRO-001d add the rewatch-before-cleanup barrier tests | A durable counter keeps Dismiss's two-row transaction and makes the owner's "no" final for the work it covered, while the rewatch still lets new events through. |
| Round 49 review: Dismiss's row count and its audit (PRO001B_DISMISS_EXACTLY_TWO_ROWS_VS_AUDIT) | opus: Dismiss is audited, and audit rows are written inside the mutating transaction (`recordAuditTx`, `orch/repository/audit.go:205`), so 'exactly two rows' contradicted the convention | The transaction writes the marker, the proposal and its content-free audit row, and the test asserts exactly those three and no `watch_events` update | A Dismiss and its audit commit together or not at all. |
| Round 58 review: dismissal tests that need the assessor (PRO001B_DISMISS_ASSESSOR_TESTS_PRECEDE_ASSESSOR) | sol: PRO-001b's dismissal tests included a queued assessment, its claim-time `assessment_stale` and a late result, but the assessor ships in PRO-001c | The marker, generation, gate and excerpt-cleanup tests stay in PRO-001b; the assessment-and-rewatch barrier tests move to PRO-001c, whose row names them | Each slice's tests can be written when it lands (round 3's rule). |

## 20. F18 — Attention rows the owner can dismiss

### Verdict

Adapt, as UX-002 acceptance: an occurrence-bound Dismiss for automation rows,
no rows for disabled or deleted subjects, and no time-based expiry. Written
into section 8 (stage table, "Leaving the list", `DismissAttention`, the
UX-002a row and the decision log).

### DeepSeek source

An unavailable Decision asks for attention only while it is less than 24 hours
old, still the latest, not dismissed and not queued
(`dsh:docs/002-authorization.md:155`); "Problems must be actionable" and "A
count equals the length of the list it links to" (`dsh:docs/005-panel.md:56-57`).

### Turing today

The previous revision's stage 3 left the list only after a success: a disabled
automation that failed months ago would have stayed in the badge.

### Unified design

Section 8. In short: `automations.attention_dismissed_occurrence`, an
occurrence key (a run id, or an audit record id for a failure before
enqueue); a newer occurrence raises the row again; disabled and deleted subjects and `owner_quiet`
cancellations never qualify; approvals, consents, deletions and suspended watches
cannot be dismissed; every row carries `since`.

### Security and privacy

Owner only; one owner-set occurrence key. Nothing whose answer authorizes a mutation
can be dismissed, so a hidden row never stands in for a decision.

### Performance

Each latest-block lookup is one seek on UX-002a's partial index `audit_logs(actor_id, created_at) WHERE action = 'automation.remote_egress_blocked'` (section 8, Performance), plus one column comparison for the dismissed occurrence; one write per Dismiss.

### Roadmap placement and dependencies

UX-002a acceptance (+S, with the column's migration).

### Delivery slices

In section 8's UX-002a row.

### Open questions

None.

### Decision log

In section 8 ("Phase 2 F18").

## 21. F19 — Health-model acceptance for TUR-014

### Verdict

Adopt as TUR-014 acceptance text; no code until TUR-014 is scheduled.

### DeepSeek source

- Each health entry has one claimed writer; a second claim throws
  (`dsh:src/infra/health.ts:11-24`).
- An error is reported only after three consecutive failures and cleared by
  the next success (`dsh:src/app/effects.ts:6-7,151-160`;
  `dsh:src/sources/github.ts:124-142`).
- The service script's exit codes separate a runtime that is down, a
  component that needs an operator, and setup that waits
  (`dsh:scripts/dsh-superagent:98-119`).
- Not copied: health details that embed error text
  (`dsh:src/app/effects.ts:149-158`), and an external watchdog.

### Turing today

`HealthService.Check` returns a bare boolean (`proto/turing/v1/health.proto:9-11`)
that is always true (`orch/app/app.go:625-627`); `./turing status` exits 0, 1
or 2 (`turing-backend/cmd/turing/internal/cli/cli.go:176-177`); Compose
restarts services (`turing-backend/infra/docker-compose.yml:5`). TUR-014 asks
for structured readiness (`docs/NORTH_STAR.md`, TUR-014).

### Unified design

Acceptance added to TUR-014:

- Fixed component ids (database, worker connection, local engine, scheduler,
  each watch poller) in states `starting`, `ready`, `paused`, `degraded` and
  `error`. **A watch's status, stated once:** a watch suspended by a source
  failure (section 3, Security and privacy) reports `error` with remedy
  `needs_person` and keeps its attention row, even while TUR-028's quiet is
  on; `paused` is only an owner's act (TUR-028's quiet, a disabled watch),
  never an incident, and applies only to a watch that is not suspended.
- One owning package per id, asserted by a test; no other package can set or
  clear it.
- `error` only after 3 consecutive failures of the component's own check
  (the constant shared with sections 13 and 23; for a watch stream, its
  `failed_polls` streak, which section 3's failure classifier defines),
  cleared only by its own success (for a watch stream, a poll answered
  `200` or `304` with no failure, as that classifier defines).
  A definitive failure skips the count: a watch's `401` or a redirect on a
  stream request is `error` with `needs_person` at once.
- Each entry has a remedy class, `restart_may_fix` or `needs_person`, which
  the host command may later map to an exit code. No watchdog: Compose already
  restarts a dead process.
- Details are a fixed code plus bounded numbers, never provider, peer or
  error text.

### Security and privacy

Status carries no content; fixed codes keep a peer's error text out of the
owner's terminal and of copied logs. The orchestrator stays the only writer.

### Performance

In-memory state; no storage, no model call. The threshold keeps one slow poll
from flapping health.

### Roadmap placement and dependencies

TUR-014 acceptance; documentation now (S), with TUR-014's dependency on
TUR-018 unchanged.

### Delivery slices

None now. When TUR-014 is built: a second writer fails a test; two failures
stay `ready` and three are `error`; another component's success clears
nothing; a peer-text sentinel never appears in any detail; a watch held by
the owner's quiet, and a disabled one, are `paused`; a
watch suspended by a `401` while quiet is on reads `error` with
`needs_person`, not `paused`, and has one content-free attention row; a
watch's first `401` is `error` with `needs_person` at once; a watch whose
stream is answered `301` is `error` with `needs_person` at once, with the
cause `unexpected_redirect`; three
consecutive `404` polls of one stream are `error` with `needs_person`, and
two are still `ready`; three consecutive transient failures of one stream
(section 3's failure classifier, including malformed responses and missed
request deadlines) are `error` without `needs_person`; a stream answered
`404`, timeout and `404` is `error` without `needs_person`, and its third
`404`, after another timeout, adds `needs_person`.

### Open questions

None.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| States | mai: add `needs_owner` as a state; sol: owner-disabled versus degraded; opus: remedy as a separate class | Five states plus a remedy class | Whether a component works and who can fix it are separate questions; one field for both would conflate them. |
| Round 3 review: roadmap citations by line (north-star-citations-point-at-wrong-items, northstar-line-citations-stale) | grok, opus: `docs/NORTH_STAR.md` line ranges for roadmap items had moved with this change's insertions | Cite roadmap items by ID | An ID survives later edits to the roadmap; the remaining line citations point at the owner decisions and invariants, above every edited item, and were re-checked. |
| Round 10 review: the roadmap's threshold (tur014-acceptance-omits-401-exception, TUR014_ROADMAP_DEFINITIVE_FAILURE_EXCEPTION, northstar-tur014-401-threshold-contradiction) | grok, sol, opus: `docs/NORTH_STAR.md` TUR-014 still said an error is reported only after three consecutive failures, while this section reports a watch's first `401` at once | TUR-014's acceptance reads "only after three consecutive transient failures, or at once for a definitive failure such as a watch's 401"; section 27's threshold bullet says the same (superseded in round 17: "three consecutive failures", of any class; see that row) | The roadmap is the acceptance a builder reads; it must carry the exception. |
| Round 16 review: the watch's failure classes (pro001a-above-floor-timeout-still-counts-as-failure, pro001a-malformed-response-failure-class-unassigned, pro001a-per-page-reset-repeats-timeout-cycle) | grok, opus: section 3 left a malformed response unclassified and counted a timeout inconsistently | One acceptance line: three consecutive transient failures of one stream, as section 3's classifier defines them, are `error` without `needs_person` | The threshold and remedy classes are unchanged; section 3 now names every failure's class once. |
| Round 17 review: alternating failure classes (PRO001_MIXED_FAILURES_NEVER_SURFACE) | sol: a stream alternating `404`s and timeouts restarted its one count each time and never surfaced | The threshold bullet counts consecutive failures of any class, for a watch its `failed_polls` streak (section 3's classifier); a test asserts `404`, timeout, `404` is `error` without `needs_person` and the third `404` adds it; TUR-014's roadmap sentence says three consecutive failures | A component that never succeeds is an `error` whichever way it fails; only the remedy depends on the class. |
| Round 18 review: what clears a watch stream's count (pro001a-deadline-stop-resets-unanswered-streaks) | grok: a poll the deadline cut before any status counted as a success and cleared the streaks | The threshold bullet's success, for a watch stream, is a poll answered `200` or `304` with no failure, as section 3's classifier defines; a cut or unreached stream clears nothing | A request that never got an answer is not evidence that the component works. |
| Round 26 review: quiet plus a suspension (pro001-quiet-plus-suspension-status-precedence-undefined) | opus: a `401` racing quiet left a watch both quiet and suspended, and this section's `paused` and `error` bullets each claimed it | The first bullet states a watch's status once: a suspended watch reports `error` with `needs_person` and keeps its attention row even while quiet is on; `paused` applies only to a watch that is not suspended; a delivery-slice test asserts it; sections 3, 18 and 27 point here | A problem a person must fix must not be hidden behind the owner's quiet. |
| Round 27 review: a watch's redirect (pro001a-renamed-repository-redirect-misclassified-transient) | opus: see section 3's decision log | A redirect, like a `401`, is a definitive failure that is `error` with `needs_person` at once, and the status tests assert it | It never recovers by retrying. |
| Round 28 review: which redirect is definitive (pro001a-hydration-301-transferred-issue-suspends-watch) | opus: see section 3's decision log | Only a redirect on a stream request is `error` with `needs_person` at once; a hydration's redirect is not a failure | A transferred issue is not a fault of the watch. |
| Round 29 review: a reused repository name (PRO001_REUSED_REPOSITORY_NAME_BYPASSES_IDENTITY_FENCE, pro001a-name-addressed-streams-follow-reused-repository-name) | sol, opus: see section 3's decision log | The stream-redirect status test names the cause `unexpected_redirect` | A redirect on an id-addressed request is unexpected, not a move. |

## 22. F20 — An uncertain GitHub comment: write-ahead hash and an issue link

### Verdict

Adapt: close the loop guard's crash window with a hash of the approved body
written before the POST, instead of a marker published in the comment, and
tell the owner plainly when a comment's outcome is unknown, with a link to
the issue. Turing never replays the POST and never reads GitHub to settle
the outcome: the owner looks. Never "exactly once".

### DeepSeek source

- Before each send, the GitHub channel lists every comment page and returns
  `sent` when one by the agent's login carries
  `<!-- dsh-superagent:<message id> -->`; otherwise it posts the text with
  that marker appended (`dsh:src/sources/github-channel.ts:18-40`). Sends must
  be idempotent because "a send may fail after the remote service received
  it" (`dsh:docs/008-cookbook.md:177`).
- Its link for a GitHub thread is the URL stored with the trigger's
  address when there is one, and otherwise the issue page built from the
  repository and the number (`dsh:src/sources/github-channel.ts:46-52`).
- Corrections: list-then-post races, so it is not exactly once; it reads every
  page on every send; it publishes an internal id; it detects a locked issue
  by matching error text (`dsh:src/sources/github-channel.ts:13-15`).

### Turing today

`github.create_comment` is one POST to the issue that the validated `owner`,
`repo` and `issue_number` arguments name
(`orch/service/integrations/github.go:120-128,185-198`). A lost response is
`side_effect_uncertain` and is never repeated
(`agent-runtime-go/internal/tools/runner.go:428-444`;
`docs/architecture/system-flows.md:226-228`). An earlier revision recorded
the comment id only after success, which left a crash window in the loop
guard.

### Unified design

1. **Write-ahead (PRO-001a).** PRO-001a creates `integration_writes`, with
   these columns: `id`; `run_id` and `session_id`, plain
   values with no foreign key, so deleting the run or its session never
   deletes the loop guard's hash early (`session_id` is read only to write
   the notice below); `repository`, the approved `owner/repo`, read only to
   build the notice's link; `issue_number`; `comment_id`, null until a
   `2xx` answer; `body_sha256`; `state` (`pending`, `posted`, `uncertain`,
   `rejected`); and `created_at`. No connection is stored: nothing reads
   one. The loop guard matches on `(issue_number, body_sha256)`, indexed,
   and never on the repository's name or a connection, so neither a rename
   between a comment and the poll that reads it nor a comment posted
   through a different connection than the watch's can defeat it, and no
   request is needed to learn GitHub's numeric repository id before a
   comment. The price is over-matching in the safe direction: an identical
   body on the same issue number in any other repository is refused as
   Turing-authored too. The
   row is committed `pending`, with the hash of the exact approved body,
   before the POST. Its transitions are fixed:
   - a `2xx` answer makes it `posted`, with the returned comment id;
   - a refusal before the request is sent, or a `4xx` answer, makes it
     `rejected`: nothing was posted, so the gate ignores the row;
   - a call that ends with no answer (a timeout or a lost connection) or a
     `5xx`, which can follow a completed write, makes it `uncertain`;
   - at orchestrator startup every `pending` row becomes `uncertain`,
     because no call survives a restart, so a crash after the write-ahead
     commit never leaves a row `pending`.

   `uncertain` is final: nothing reads GitHub to change it, and the
   approval, consumed before the row was written
   (`orch/service/integrations/call.go:52-67`), is never used again. The
   gate treats a matching hash on the same issue number, in any state but
   `rejected`, as Turing-authored (section 3, point 4), so a
   crash between GitHub's success and the id write no longer opens a loop,
   and the guard never depends on knowing whether an `uncertain` comment
   exists. Nothing is added to the comment, so the approved body and its
   preview are unchanged.
2. **The owner is told, with a link.** The transaction that makes a row
   `uncertain` also writes, while the run's session is active, one
   role-system run notice in that session with a new content type,
   `integration_uncertain`, written as `delegation_notice` is
   (`orch/repository/delegation_lifecycle.go:333`;
   `turing-backend/internal/egress/delegation.go:54-57`). It names the
   repository, the issue number and the row id, holds no comment text, says
   that the comment may or may not have been posted and that Turing will
   not post it again, and tells the owner to check on GitHub. It carries
   the link `https://github.com/{owner}/{repo}/issues/{number}`, built from
   the repository and issue number stored on the row, which are the
   arguments the owner approved, validated and each path-escaped; nothing
   in it comes from a GitHub answer. The app renders it as an
   ordinary external link. Opening it is the owner's own browser request;
   Turing sends nothing.
3. The PRO-001 lifecycle sweep deletes a row 30 days after its
   `created_at`, on its own clock, the excerpts' retention. It runs on the
   maintenance tick (section 27), whether or not any watch exists and
   whether or not the reaper is on.

### Security and privacy

- Nothing is published in the owner's comments. On a public repository a
  marker would tag every comment the owner posts through Turing, in public,
  to serve a rare uncertain outcome.
- A hash of the owner's own comment is kept for 30 days, with the
  repository and issue number the owner approved. No credentialed request
  is ever sent to settle an uncertain outcome, so there is no read to gate,
  fence or revoke, and no comment text is ever read back.
- The notice's link is built only from the stored, approved, path-escaped
  arguments, so it names `github.com` and the `owner/repo` and issue number
  the owner approved when the comment was posted; no GitHub answer can
  point it elsewhere. A link names a repository, not an id: after that
  repository is renamed and another is created at its old name, the link
  may open the other repository. Turing adds no credentialed lookup to
  prevent that, and the loop guard does not depend on the name (point 1).
- The never-replay rule is unchanged. A stranger who copies the body exactly
  on the same issue number of any repository is refused by the gate, the
  safe direction.

### Performance

One small extra write transaction per GitHub comment (attended and rare),
plus one notice row when a row becomes `uncertain`, and one startup update
of any `pending` rows. No request, no background scan; per admitted
comment, one probe of an index on `comment_id` and one of an index on
`(issue_number, body_sha256)`; the retention sweep deletes in
the PRO-001 sweep's bounded batches.

### Roadmap placement and dependencies

PRO-001a: the write-ahead, the `uncertain` notice with its link, and the
gate rule. No INT-009 slice. CHN-002 reuses "never replay; an uncertain
send stays visibly uncertain" (section 23).

### Delivery slices

| Slice | Size | Acceptance tests |
| --- | --- | --- |
| PRO-001a5 (part) | +S | The row is committed before the POST; the crash tests and the `422` test in PRO-001a's row; a timeout and a `502` each leave the row `uncertain` with exactly one POST and write one `integration_uncertain` notice holding the repository, issue number, row id and the link `https://github.com/{owner}/{repo}/issues/{number}` built from the row, and no comment text; a repository name needing escaping produces an escaped link; a crash after the write-ahead commit gets its notice at startup; a row that becomes `uncertain` after its conversation began deletion writes no notice; no request reaches GitHub after an uncertain outcome, whatever the clock (none after 30 fake-clock days); an `uncertain` row's hash still refuses the polled comment as Turing-authored; with an `uncertain` row written under the repository's old name and the repository renamed before the poll, the polled comment is refused as Turing-authored and starts nothing; an `uncertain` row written through one GitHub connection refuses the identical polled comment on a watch that reads the repository through another connection; deleting the run's session leaves the row, which the sweep deletes 30 days after its `created_at` |

### Open questions

None.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Marker or hash | sonnet, grok, sol, mai: an HMAC marker in the comment, disclosed in the tool's description; opus: a body hash written before the POST | Hash | The same deterministic match with nothing published; a marker would also have to enter the approval preview, since the posted bytes must be the approved ones. |
| Rounds 3 to 7: reading GitHub back to settle an uncertain comment (the Phase 2 "when to read" question; GITHUB_HASH_RECONCILE_SPOOF, f20-reconcile-matches-any-author, GITHUB_RECONCILE_STALE_AUTHORITY, GITHUB_RECONCILE_MISSING_DURABLE_ATTEMPT, f20-uncertain-delay-timestamp, GITHUB_RECONCILE_EXCLUDES_FAILED_UNCERTAIN_RUN, int009c-reconcile-transient-run-state-fenced-forever, int009c-reconcile-comment-page-read-unbounded-or-truncated) | sol: read inside the call, at most two pages; opus: one page on a later reaper tick; then, round by round: a read matched any author's body, outlived the owner's revocations, had no durable once-only fence, and had an eligibility of the wrong shape | History, all superseded: rounds 3 to 6 explored a read-back reconciler (an account-id match through `GET /user`, dispatch checks before each read, a `reconcile_attempted_at` fence, and in round 6 the owner's Check GitHub in place of a reaper); round 7 dropped every read (yagni, next row). The chosen design is no read-back: an `uncertain` row stays `uncertain`, and its notice links the owner to the issue | Each round's fix to the read added state and tests for an outcome the loop guard never needed, since the guard matches the body hash in every state but `rejected`. |
| Round 4 review: a `pending` row had no way out (integration-writes-pending-not-terminal) | grok: a crash after the write-ahead commit left a row `pending` forever and never reconciled, and a definite `4xx` had no state, so its hash refused an identical comment for 30 days | Fixed transitions: startup turns `pending` into `uncertain`; a refusal before sending or a `4xx` is terminal `rejected`, which the gate ignores; a `5xx` is `uncertain` | Every row ends in a state with one meaning; an unknown outcome is still never posted again, and a known failure no longer blocks the owner's own comment. |
| Round 7 review: the owner's Check GitHub (int009c-check-github-yagni-gate-unaffected, f20-since-excludes-same-second-comment, f20-oneshot-page-exceeds-client-timeout, int009c-since-intent-at-clock-skew-spends-one-shot, f20-frozen-decision-not-owner-revocable) | opus: the check's only consumer was a notice the owner can replace by opening the issue, since the loop guard never depends on it, and it cost an RPC, a column, a state, `GET /user`, a streamed decode and about fifteen tests; grok, opus: its `since={intent_at}` was treated as inclusive and had no margin, so a same-second comment or a small clock skew hid the comment and spent the one check; grok: a 100-element page of 1 MiB elements cannot arrive inside the 30-second client timeout; opus: the frozen decision it gated on is not something the owner can withdraw | INT-009c is dropped: no `CheckUncertainComment`, no `reconcile_attempted_at`, no `reconciled` state, no `GET /user`, no comment read. The `integration_uncertain` notice links to the issue, built from the stored, approved repository and number, as DeepSeek's link falls back to the issue page when a thread has no stored URL (`dsh:src/sources/github-channel.ts:46-52`). The write-ahead `pending` row, startup's `pending` to `uncertain`, `rejected` on a definite failure and the loop guard stay | Every credentialed read leaves this path, and with it the three timing findings and the revocation wording. The owner sees the issue itself, which no read could do better: a match proves only that the owner's account posted that body. |
| Round 8 review: the decision log and the ledger (F20_SUPERSEDED_RECONCILER_STILL_DOCUMENTED, f20-decision-log-stale-reconcile-rows, f20-integration-writes-schema-undefined, f20-dsh-link-citation-overstated) | mai, opus: superseded read-back rows still read as chosen; opus: `integration_writes` "gained" columns but was never created, had no run or session link, and the gate matched the repository as typed while the poller keys by repository id; the DeepSeek link citation omitted that it prefers the trigger's stored URL | The read-back rows are one history row; PRO-001a creates `integration_writes` with its columns listed, `run_id` and `session_id` as plain values, the repository lower-cased, and its own 30-day sweep; the citation states both branches | A cascading key would delete the loop guard's hash with the run; the lower-cased name needs no request, where the numeric id would need a `GET` per comment, and its rename ceiling is stated. Superseded in round 13: the guard ignores the repository name, so there is no rename ceiling and no lower-casing (see that row). |
| Round 13 review: a rename defeated the hash match (f20-repository-rename-loop-guard-ceiling) | mai: an `uncertain` comment has no id, and its row keyed the lower-cased old name, so after a rename the polled comment missed the hash match and was admitted as its listed author's event | The guard matches `(connection_id, issue_number, body_sha256)` and never the repository name, which stays on the row only to build the notice's link; the stated rename ceiling and the `Owner/Repo` test are removed, replaced by a renamed-repository `uncertain` fixture whose polled comment is refused and fires nothing | The smallest key a rename cannot change needs no request; over-matching an identical body on the same issue number in another repository of the same connection only refuses more, the safe direction. Superseded in round 14: the connection is dropped from the key (see that row). |
| Round 14 review: the connection in the key missed another connection's comment (f20-loop-guard-connection-key-misses-cross-connection-uncertain) | opus: several GitHub connections can exist and rotating a token creates a new connection id, so an `uncertain` row written through one was missed by a watch on another | The guard matches `(issue_number, body_sha256)` in any state but `rejected`, with no connection or repository key, over an index on those two columns; `connection_id` is dropped from the row, since nothing reads it; a cross-connection fixture proves the refusal | The key is now what the comment itself carries; the wider match only refuses more, the safe direction. |
| Round 30 review: the link after a name reuse (f20-approved-name-link-opens-reused-repository) | grok: the security bullet said the link always opens the issue the owner approved, but a link built from `owner/repo` opens whatever repository holds that name, which after a rename and reuse is another one | The bullet now says the link names the approved `owner/repo` and issue number and may open a different repository after a name reuse; no credentialed lookup is added and the loop guard is unchanged | The notice is a pointer for the owner, not an identity check, and a lookup would bring back the read-back rounds 3 to 7 removed. |

## 23. F21 — Delivery-ledger semantics for TUR-024 and CHN-002

### Verdict

Adopt as acceptance text; no code until those items are scheduled.

### DeepSeek source

- One row per kind and key; a re-enqueue replaces the payload and bumps a
  `generation`, and an attempt removes only its own generation
  (`dsh:src/app/effects.ts:38,60-66,133-135`).
- `Later` defers without counting a failure (`dsh:src/app/effects.ts:1-5,138-139`).
- A ticket's messages to one conversation go out in order; a later one waits
  until the earlier has failed three times; other conversations never wait
  (`dsh:src/app/outbox.ts:44-45`; `dsh:docs/003-execution.md:179-181`).
- A message is dropped only when the target can never receive it, and a drop
  keeps its record and is never queued again
  (`dsh:docs/003-execution.md:185,191`).
- Not copied: Feishu's `gone` by error-text matching
  (`dsh:src/sources/feishu-channel.ts:16-17`).

### Turing today

TUR-024 scopes delivery keyed by approval, run, assignment, attempt and state
version (`docs/NORTH_STAR.md`, TUR-024); CHN-002 scopes an outbound ledger,
bounded retries and visible ambiguity (`docs/NORTH_STAR.md`, CHN-002).
Neither states these rules.

### Unified design

- **TUR-024:** a newer state version supersedes an in-flight delivery and an
  older attempt cannot remove it; a deferral (no waiter connected) is not a
  failed attempt; delivery still resumes only the exact matching waiter.
- **CHN-002:** one row per key with a generation fence; a deferral is not a
  failure; order is kept per destination conversation and released after 3
  failures (section 21's constant), with the blocker visible; `gone` only from
  a typed platform answer; a drop keeps a content-free record (id, time,
  reason code) and is never queued again; queued, sent, dropped and uncertain
  are distinct states the app shows; an uncertain send stays visibly
  uncertain and is never replayed (section 22).

### Security and privacy

A typed `gone` cannot be triggered by untrusted error text. Drop records hold
no message body. No send is reported as delivered while it is only queued.

### Performance

Coalescing bounds rows and retries to one per key; nothing until a ledger
exists.

### Roadmap placement and dependencies

TUR-024 and CHN-002 acceptance.

### Delivery slices

Documentation only now. Fixtures in each item's pull requests: an old attempt
against a new generation, deferral against failure, ordered retry and
release, another conversation's progress, typed gone and uncertain, and
queued shown as not delivered.

### Open questions

None.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Ordering scope | dsh: also a global hold per ordered kind (`dsh:src/app/effects.ts:109-113`); grok, sol: per conversation only | Per conversation | A global hold lets one bad destination block unrelated work. |
| Round 3 review: roadmap citations by line (north-star-citations-point-at-wrong-items, northstar-line-citations-stale) | grok, opus: `docs/NORTH_STAR.md` line ranges for roadmap items had moved with this change's insertions | Cite roadmap items by ID | An ID survives later edits to the roadmap; the remaining line citations point at the owner decisions and invariants, above every edited item, and were re-checked. |

## 24. F22 — Settings compare-and-set

### Verdict

Adopt as OPS-002 acceptance, in Turing's transactional form.

### DeepSeek source

A save names its block's revision and is refused when stale; it is checked
again after its asynchronous model and account checks, re-merged from freshly
read settings, and a save that changes nothing records no new version
(`dsh:src/app/settings.ts:148-175`); revisions are kept per block
(`dsh:src/kv.ts:24-27`). Not copied: saves by a tool origin, and the client's
three-way merge of an unsaved draft until a second writer exists.

### Turing today

OPS-002 states one home per setting and apply-or-report, with no revision
(`docs/NORTH_STAR.md`, OPS-002). Persona and profile saves are already
compare-and-set on `expected_content_hash` (`proto/turing/v1/memory.proto:345-370`).

### Unified design

Each OPS-002 settings group has a monotonic revision, and a save names its
base. Validation, including any I/O such as an engine proof or an account
check, runs outside every transaction. The write is `UPDATE … WHERE group = ?
AND revision = ?` in the one transaction that also writes audit, so that
final compare is the fence and every earlier check only fails fast:
DeepSeek's re-check after its awaits is safe there because its final write is
synchronous, which a Go service with a database pool cannot assume.
Validation that depends on another group carries that group's revision and is
refused if it moved. A stale save writes nothing and returns current values
with secrets redacted. A save that changes nothing bumps no revision, writes
no audit row and applies nothing. No setting is exempt: a boolean write is
not idempotent across a race, so TUR-028's quiet setter names its revision
from the start (section 18, point 1), and `memory_enabled`, written
unconditionally today (`orch/repository/memory_settings.go:46-80`), takes
its group's revision when OPS-002 gives it a home. A refused client keeps its draft and reloads; nothing is merged
automatically, and secret fields never are.

### Security and privacy

A stale client cannot re-enable what the owner just disabled. No agent path
reaches settings (owner decision 7). Audit holds the group and revision,
never secret values.

### Performance

One integer comparison per save; validation never runs inside a transaction.

### Roadmap placement and dependencies

OPS-002 acceptance (+S).

### Delivery slices

In OPS-002: two saves from one base, the second refused; a save whose slow
validation overlaps another's commit is refused; a moved dependency group is
refused; a no-op writes no audit row; a conflict response holds no secret; the
runtime identity is refused. The quiet setter's stale-write test is
TUR-028a's (section 18).

### Open questions

None.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Where staleness is decided | sonnet, grok, mai: re-check after each asynchronous validation; sol, opus: the compare in the write transaction is the fence | The write transaction | Only a compare in the transaction that writes is atomic with the write. |
| Draft merge | dsh: three-way; sol: keep the draft, no automatic merge | No merge until MOB-001 | There is one writer today; an automatic merge of security settings is a risk with no consumer. |
| Round 3 review: roadmap citations by line (north-star-citations-point-at-wrong-items, northstar-line-citations-stale) | grok, opus: `docs/NORTH_STAR.md` line ranges for roadmap items had moved with this change's insertions | Cite roadmap items by ID | An ID survives later edits to the roadmap; the remaining line citations point at the owner decisions and invariants, above every edited item, and were re-checked. |
| Round 31 review: the quiet switch's exemption (SETTINGS_TOGGLE_STALE_WRITE_BYPASSES_CAS) | sol: see section 18's decision log | The exemption sentence is replaced: no setting is exempt, TUR-028's quiet setter is compare-and-set from the start, and `memory_enabled` takes its group's revision under OPS-002 | A stale boolean write is a lost update like any other. |

## 25. F23 — Upgrade rehearsal on a restored copy

### Verdict

Adapt, as **TUR-016c**: rehearse migrations and invariants on a restored copy
in a one-off, network-less check container, not a second running stack.

### DeepSeek source

"Reviewing a change against real data": copy a backup into a scratch home,
disable every source, pause processing and give the CLIs read-only stand-ins
(`dsh:docs/007-architecture.md:610-612`). Upgrading is back up, build and
restart, and a rollback needs the pre-upgrade backup
(`dsh:docs/006-deployment.md:233-242`). It is a written recipe, not a command.

### Turing today

- TUR-016 (backup, restore verification, migration checksums) is pending
  (`docs/NORTH_STAR.md`, TUR-016); migration tests use synthetic databases.
- Service binaries run only the checks their image's label lists
  (`turing-backend/orchestrator-go/cmd/server/main.go:21`;
  `turing-backend/orchestrator-go/Dockerfile:28`;
  `turing-backend/agent-runtime-go/Dockerfile:25`).
- `./turing` never opens SQLite (`turing-backend/cmd/turing/imports_test.go:23`).
- `./turing` runs Docker through `os/exec`
  (`turing-backend/cmd/turing/internal/host/host.go:49-55`), so a container it
  starts belongs to the Docker daemon and outlives a killed `./turing`.
- A migration can write files: 0011's export writes into the skills root
  (`orch/db/migrations.go:52-58`).

### Unified design

1. **`check-database`**, a new orchestrator check (the label becomes
   `config,database`): it reads exactly two variables, `DATABASE_PATH` and
   `SKILLS_ROOT`, and refuses to start (exit 2) when either is unset or
   empty, so it never falls back to the configuration defaults
   (`/app/data/turing.db` and `/skills`, `orch/config/config.go:447,482`). It
   opens `DATABASE_PATH`, runs F15's check, applies the migrations with
   `SKILLS_ROOT` as the skills root, runs
   `PRAGMA integrity_check`, `PRAGMA foreign_key_check` and TUR-016's
   invariant queries, prints a content-free report (check names, counts,
   migration names, durations) and exits. It never calls `app.New`: no
   server, scheduler, poller, worker or model.
2. **`./turing rehearse`** copies the newest verified TUR-016 backup into
   `.turing/rehearse/<random>/` (a 0700 directory with 0600 files, gitignored,
   never a system temporary directory), refuses when free space is under
   twice the backup's size, and lays the directory out as `turing.db` (the
   copy) and an empty `skills/`. It runs the candidate image once as
   `check-database` with `--rm` and the container name
   `turing-rehearse-<random>` (`<random>` is the directory's own name, and
   Docker keeps container names unique), `--network none`, a read-only root filesystem,
   `--cap-drop ALL` and `--security-opt no-new-privileges`, as Compose
   sets on the same image (`turing-backend/infra/docker-compose.yml:15-18`),
   `--user` set to the invoking user's numeric UID and GID, exactly one
   mount (that directory, read-write, at `/rehearse`), and exactly two
   injected variables, `DATABASE_PATH=/rehearse/turing.db` and
   `SKILLS_ROOT=/rehearse/skills`, beside the image's own non-secret
   defaults (such as `PATH` from its base image): no `--env-file`, no
   `.env`, no host credential variable, no other volume, so the live `data/`, `skills/`, `memory/`, `team/` and `mcp/`
   are not reachable from the container at any path. It prints the report, and deletes the directory on success, failure
   and interrupt. A kill, an out-of-memory kill or a crash runs no cleanup,
   and the container it started keeps running under the daemon, so every
   `./turing` invocation, before any other work, tries the exclusive lock on
   `.turing/rehearse/.lock` without waiting. Holding it, the sweep takes each
   leftover `.turing/rehearse/<random>/` directory in turn: it runs
   `docker rm -f turing-rehearse-<random>`. **Cleanup eligibility, stated
   once:** the sweep deletes the directory when the daemon confirms the
   container absent, that is when `docker rm -f` fails with "no such
   container" or `docker container inspect turing-rehearse-<random>`
   afterwards answers "no such container", whatever the removal command's
   exit status, so a container never created, or already removed by
   `--rm`, never holds a copy back. It keeps the directory only when
   absence cannot be confirmed (Docker cannot be reached, or the container
   still exists afterwards), for a later sweep, and that
   `./turing` invocation prints a content-free notice naming the kept
   directory's path, saying that it holds a copy of personal data, and
   saying how to remove it (start Docker and run any `./turing` command,
   or run that `docker rm -f` and delete the directory by hand).
   Then it releases the lock; when the lock is busy it skips the sweep.
   `./turing rehearse` takes the same lock first (waiting up to 10 seconds
   for a sweep to finish, then refusing as busy), runs the sweep itself while
   holding it and refuses if any leftover directory remains, creates its
   directory only after that, and holds the lock for its whole run; the
   kernel releases it when the process dies. So no sweep ever touches the
   directory or container of a live rehearsal, and no rehearsal starts beside
   an orphaned container. It refuses
   before copying when the image's label lacks `database`, when no verified
   backup exists, or when the backup is encrypted (TUR-022): the check's
   fixed environment of two variables has no key channel, so TUR-016c
   refuses every encrypted backup until TUR-022 defines one, and it never
   writes a plaintext copy of an encrypted database. The user is the one
   Compose runs the orchestrator as: Compose substitutes `HOST_UID` and `HOST_GID`
   (`turing-backend/infra/docker-compose.yml:11`), which `init.sh` writes
   from the invoking user (`turing-backend/scripts/init.sh:143-147`), while
   the image's own default is UID and GID 1000
   (`turing-backend/orchestrator-go/Dockerfile:18-23`). The copy is created
   by `./turing`, which refuses root, so it is owned by that user, and
   `db.Open` refuses a database not owned by its effective UID
   (`orch/db/connection.go:88-90`); without `--user` the check would fail on
   every host whose UID is not 1000. Reading the IDs from the process
   instead of `.env` keeps the rehearsal independent of `.env`.
3. A pass proves that backup and that image together; it says nothing about
   writes made since the backup, nor about the application's behaviour.

### Security and privacy

A second copy of personal data exists for minutes: 0700, no network,
removed on every exit path that can run code, and otherwise by the next
`./turing` command that can reach Docker, once Docker confirms that the
copy's container does not exist (cleanup eligibility, point 2). A copy whose container Docker cannot
confirm gone is kept until a sweep can confirm it, and every `./turing`
command whose sweep keeps one prints a content-free notice naming its path,
that it holds a copy of personal data and how to remove it, so the copy is
never kept silently. The report holds no row content. The host command still
opens no database, and the orchestrator gains no Docker control (OPS-001's
rule).

### Performance

Offline: one database copy of disk and the migration time. Nothing touches the
live stack.

### Roadmap placement and dependencies

TUR-016c, after TUR-016's backup and restore verification and after TUR-016a.
It does not depend on TUR-028. It does not wait for TUR-022 either: it
refuses encrypted backups, and rehearsing one needs a key channel for the
check that TUR-022 must define.

### Delivery slices

| Slice | Size | Acceptance tests |
| --- | --- | --- |
| TUR-016c | M | A backup with a seeded foreign-key violation fails the report and a clean one passes; the scratch directory is gone after success, failure and SIGINT; a directory left by a rehearsal killed with SIGKILL is gone after the next `./turing status`; in a real container, with only `./turing` killed with SIGKILL while `check-database` runs, the next `./turing status` leaves no container named `turing-rehearse-<random>` before it removes the directory, and a rehearsal started then begins only after both are gone; a directory left by a rehearsal killed after copying but before its container was created, and one left after `--rm` removed the container but before the directory was deleted, are each removed by the next `./turing` command, whose `docker rm -f` fails with "no such container", after which a rehearsal may start; with Docker unreachable, and again with Docker answering but the container still present after `docker rm -f` (a fake `docker` that reports the container on every inspect), the sweep keeps the directory, every `./turing` command, `./turing status` included, prints the notice naming its path, that it holds a copy of personal data and how to remove it, with no row content, and `./turing rehearse` refuses; a container that exits on its own is removed by `--rm`; a concurrent `./turing status` leaves a running rehearsal's directory and container alone; a rehearsal started while a sweep holds the lock waits for it, and its directory survives; the container has no network interface but loopback, and the inspected `HostConfig.CapDrop` holds `ALL` and `HostConfig.SecurityOpt` holds `no-new-privileges`, with a real rehearsal still passing; in a real container, a rehearsal invoked by a host user whose UID and GID are not 1000 opens the copy and passes, and the container's process runs as that UID and GID; with a live database and a live skills root at host paths deliberately different from the scratch directory, each holding a sentinel file, the `docker run` arguments and the inspected `HostConfig.Binds` hold exactly one mount, the scratch directory at `/rehearse`, the inspected `Mounts` has no destination but `/rehearse` and the daemon-managed `/etc/hosts`, `/etc/hostname` and `/etc/resolv.conf`, and the inspected `Config.Env`, once the variables of the image's own configuration (read from `docker image inspect`, each on an allowlist of non-secret defaults such as `PATH` and `HOME`) and any name the daemon adds itself (`HOSTNAME`, wherever Docker reports it) are set aside, holds exactly the two injected variables, and passes in a real container whether or not `HOSTNAME` appears there, while the `docker run` arguments hold no `--env-file` and the environment holds no host credential variable, and every sentinel and the live database are byte-identical with unchanged modification times afterwards; `check-database` with `DATABASE_PATH` or `SKILLS_ROOT` unset or empty exits 2 before opening any file; an image whose label lacks `database`, and any encrypted backup, are refused before any copy; the report holds no row content |

### Open questions

None.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Isolation | sonnet, grok, mai: a second Compose project with an internal network and the scheduler off; sol: a one-shot entrypoint in its own project; opus: a one-off check container | One-off check container | There is no service loop to switch off, so nothing depends on configuration being right, and `--network none` is stronger than an internal network. |
| Dependency on the quiet switch | sonnet, mai: yes; grok, sol, opus: no | No | Nothing that the switch gates runs in the check. |
| Round 3 review: a killed rehearsal left its copy (tur016c-rehearse-copy-not-removed-on-crash) | grok: cleanup ran only on exits that can run code, so a SIGKILL or crash left a copy of the database in the project tree | Every `./turing` invocation first removes leftover rehearsal directories that no live rehearsal holds the lock on | The host command already runs on every operator action; a lock keeps the sweep from deleting a rehearsal still in progress. |
| Round 3 review: roadmap citations by line (north-star-citations-point-at-wrong-items, northstar-line-citations-stale) | grok, opus: `docs/NORTH_STAR.md` line ranges for roadmap items had moved with this change's insertions | Cite roadmap items by ID | An ID survives later edits to the roadmap; the remaining line citations point at the owner decisions and invariants, above every edited item, and were re-checked. |
| Round 4 review: the sweep tested the lock without holding it (tur016c-rehearse-sweep-lock-race) | opus: a rehearsal starting between the sweep's check and its delete could lose its directory | The sweep holds the lock, taken without waiting, for its whole scan and delete, and skips when busy; a rehearsal creates its directory only after taking the lock | A check that is not held is no exclusion; the lock now covers both sides. |
| Round 5 review: the check container ran as the wrong user (REHEARSAL_UID_OWNERSHIP_MISMATCH) | sol: the image defaults to UID 1000, while `./turing` creates the copy as the invoking user and `db.Open` refuses a database it does not own | The one-off container runs with `--user` set to the invoking user's numeric UID and GID, as Compose runs the orchestrator | The same identity the live stack uses; no ownership change of the copy, which would need root. |
| Round 5 review: this log held the wrong row (tur016c-round4-log-row-in-wrong-section) | opus: the round 4 lock fix for TUR-016c was logged in section 16 (F14) | Moved here | Each fix is logged with its feature. |
| Round 6 review: the check was not bound to its copy (f23-rehearsal-database-binding) | mai: the rehearsal named no mount target, `DATABASE_PATH` or `SKILLS_ROOT`, so an implementation could open the image's default path or the live database | One mount at `/rehearse`, `DATABASE_PATH=/rehearse/turing.db` and `SKILLS_ROOT=/rehearse/skills` as the only environment (since round 10, the only injected variables beside the image's own non-secret defaults; see that row), and `check-database` refuses when either is unset instead of using a default; a test with sentinel live paths proves they are never opened | Binding by explicit paths with no default is the smallest rule that makes "only the copy" checkable; the read-only root filesystem already keeps a wrong default from creating a database. |
| Round 7 review: an encrypted backup could never be rehearsed (tur016c-encrypted-key-unreachable-with-two-env-vars) | opus: the rehearsal promised to accept an encrypted backup when its key was available, but its environment is exactly two variables, so no key could ever reach the check | TUR-016c refuses every encrypted backup until TUR-022 defines a key channel for the check; the dependency is noted on both items | A promise no input can satisfy is removed; the binding to the copy stays exactly two injected variables. |
| Round 8 review: TUR-022 did not record the key channel (tur022-key-channel-obligation-unrecorded) | opus: only TUR-016 noted the dependency, while the round 7 row said both items did | TUR-022's acceptance gains one sentence: it defines how a one-off offline check receives the database key, so TUR-016c can rehearse an encrypted backup | The obligation now sits on the item that must meet it. |
| Round 10 review: Docker's own environment (TUR016C_DOCKER_ENVIRONMENT_ASSERTION) | sol: the test required the container's configuration to hold exactly `DATABASE_PATH` and `SKILLS_ROOT`, but the image inherits `PATH` and others from `debian:bookworm-slim` (`turing-backend/orchestrator-go/Dockerfile:15`), so it could never pass | "Exactly two injected variables": the test sets aside the image's own variables, read from its configuration and each on an allowlist of non-secret defaults, asserts the rest are exactly the two, and separately asserts no `--env-file`, no host credential variable and no mount but the scratch directory | The isolation it proves is unchanged; the assertion now matches what Docker reports. |
| Round 11 review: Docker's own mounts (tur016c-inspect-mounts-unsatisfiable) | grok: the test required exactly one mount in the container's inspected configuration, while the daemon also binds `/etc/hosts`, `/etc/hostname` and `/etc/resolv.conf` | The `docker run` arguments and `HostConfig.Binds` hold only the scratch mount at `/rehearse`, and `Mounts` has no destination but `/rehearse` and those three daemon-managed files | As in round 10, the assertion now matches what Docker reports; the isolation it proves is unchanged. |
| Round 12 review: the lock did not fence the container (REHEARSAL_LOCK_DOES_NOT_FENCE_CONTAINER) | sol: a SIGKILL of `./turing` releases the lock but not the daemon-managed check container, so the next sweep could delete a directory still being migrated | The container gets a fixed label naming its directory, a name derived from it and `--rm`; the sweep, holding the lock, stops and removes every container carrying that label before it deletes the directory, keeps the directory when Docker cannot confirm that, and a rehearsal sweeps before it creates its own | The lock now covers the container as well as the directory, and the label is enough to find an orphan after the process that started it is gone. Superseded in round 13: the label is dropped and the unique container name alone finds the orphan (see that row). |
| Round 13 review: a kept copy was silent, and the label repeated the name (tur016c-security-claim-contradicts-docker-unreachable-retention, tur016c-container-label-and-name-redundant) | opus: the security text said the next `./turing` command always removes a leftover copy, while the sweep keeps it, silently, when Docker cannot be reached; opus: the label and the container name each identify the directory, and Docker names are unique | The security text says a copy is kept until a sweep can confirm its container gone, and every `./turing` command whose sweep keeps one prints a content-free notice naming its path, that it holds a copy of personal data and how to remove it; the label is dropped, and the sweep runs `docker rm -f turing-rehearse-<random>` and deletes the directory once Docker answers that no such container exists | The owner learns of every kept copy on the next command, and one identifier for the container is enough. |
| Round 14 review: a kept copy was announced only when Docker was unreachable (tur016c-kept-copy-notice-only-when-unreachable) | grok: the notice was attached to the unreachable branch, so a reachable Docker that did not confirm the container gone kept the copy silently | The notice applies to every sweep outcome that keeps a directory: Docker unreachable, a `docker rm -f` error, or the container still present (refined in round 25: a `docker rm -f` error keeps it only when the daemon cannot confirm the container absent; see that row); a fixture where Docker answers but the container remains proves it | "Never kept silently" now holds for every way the sweep keeps a copy. |
| Round 25 review: an absent container blocked cleanup (TUR016C_ABSENT_CONTAINER_BLOCKS_CLEANUP) | sol: `docker rm -f` exits nonzero for a missing container, so a sweep that kept the directory on any `docker rm -f` failure kept the copy forever in the normal orphan cases, a rehearsal killed before its container was created or after `--rm` removed it | Point 2 states cleanup eligibility once: daemon-confirmed absence (`docker rm -f` failing with "no such container", or `docker container inspect` answering it), whatever the removal command's exit status; the directory is kept only when absence cannot be confirmed (Docker unreachable) or the container remains; TUR-016c adds crash fixtures killed after copying but before the container was created and after `--rm` removed it but before the directory was deleted, whose next command removes the directory and lets a rehearsal start | Keeping a copy is for a container that may still use it, not for one the daemon says does not exist. |
| Round 94 review: the daemon's `HOSTNAME` (tur016c-daemon-hostname-breaks-env-assertion) | grok: Docker adds `HOSTNAME` itself, so an assertion of exactly two variables could fail on a correctly isolated container | The assertion names the inspected `Config.Env` and sets aside any name the daemon adds (`HOSTNAME`) as well as the image's allowlisted defaults, and it must pass in a real container with or without `HOSTNAME` there | The check is about what Turing injects, as round 11 settled for the daemon's mounts |
| Round 95 review: the rehearsal container's hardening (TUR016C_REHEARSAL_CONTAINER_LESS_HARDENED_THAN_LIVE_SERVICE) | opus: the one-off container reading the owner's data dropped the `cap_drop: ALL` and `no-new-privileges` that Compose sets on the same image | The `docker run` adds `--cap-drop ALL` and `--security-opt no-new-privileges`, and TUR-016c asserts both in `HostConfig` with a real rehearsal still passing | A copy of personal data gets at least the live service's hardening |

## 26. F24 — The owner's words in delegation briefs, measured first

### Verdict

Adapt: add an EVAL-001 delegation-fidelity scenario now, and attach the
owner's own message to a child's brief only if that scenario fails.

### DeepSeek source

The router forwards the original trigger with at most a 30-word note and
must not paraphrase a request into a plan, and "the host always includes the
original trigger reference independently of the note"
(`dsh:docs/004-prompts.md:77`; `dsh:prompts/shared.md:6-7`;
`dsh:prompts/dispatch.md:37-38,47-49`). Correction: DeepSeek forwards a
reference its workers can read, not a verbatim copy in every prompt.

### Turing today

`team.delegate` takes a model-written `task` (at most 4 KiB) and `context`
(at most 8 KiB) (`orch/service/team/call.go:242-266`;
`turing-backend/internal/egress/delegation.go:64-67`), framed
`DELEGATION_BRIEF` (`turing-backend/internal/egress/delegation.go:74-85`).
The child sees nothing else from the parent.

### Unified design

1. **EVAL-001 scenario "delegation fidelity"** on the reference local model:
   the owner states a constraint ("tidy the draft but keep my tone", "only the
   2024 files"), Turing delegates, and the child's result is checked against
   it. The scenario fails when the constraint is lost in 2 of 3 runs. It
   includes cases where the paraphrase is enough, so a needless frame shows
   up as cost without gain.
2. **Only on failure, an AGT-002 follow-up:** `CreateDelegation` adds the
   parent run's anchoring message as a separate `OWNER_REQUEST` frame below
   the brief, labelled as the owner's words and as data, at most 4 KiB with
   the visible cut marker. Only a role-user message the owner wrote through
   the public facet qualifies; a continuation's join, a `watch_event`, a
   channel message or any model-written text never does, and no earlier
   message is searched for. The model's `task` stays the instruction.

### Security and privacy

The copied message can hold a secret or a quoted stranger's text. It stays
framed as data in a hidden local child that is withdrawn with its parent (the
existing cascade), counts toward the child's egress consent, and grants
nothing.

### Performance

The scenario is an evaluation cost only. If the frame ships, it adds at most
4 KiB of prefill per child (about 1,000 tokens on the 7B model) and no model
call; child latency is measured with and without it.

### Roadmap placement and dependencies

An EVAL-001 scenario; an AGT-002 follow-up only after a recorded failure.

### Delivery slices

The scenario (S). The conditional frame (S): present and bounded on an
owner-anchored delegation; absent when the parent run is a continuation or
watch-started; deleted with the parent; F11's byte ratchet updated in the
same pull request.

### Open questions

None.

### Decision log

| Disagreement | Proposed by | Chosen | Why |
| --- | --- | --- | --- |
| Over 4 KiB | sol: omit the frame with a notice; grok, opus, mai: a visible cut | Visible cut | It matches every framed text, and the marker tells the child the words are partial. |
| Is owner text trusted | sonnet: trusted by definition; sol: its origin is known, but it can quote hostile text or carry secrets | Sol | It stays framed as data, and only the direct owner message qualifies. |
| Round 3 review: AGT-002 had no pointer (agt002-f24-roadmap-pointer-missing) | mai: the conditional follow-up was assigned to AGT-002, whose roadmap entry did not mention it | `docs/NORTH_STAR.md` AGT-002 gains the conditional sentence and a design pointer | The follow-up is discoverable from the item that would own it. |

## 27. Cross-feature architecture

### Order

1. **TUR-026** (a, b, c, d): closes verified gaps in shipped code, including
   safe tools outside today's automation allowlists; independent.
2. **UX-002** (a, b), with F18's Dismiss: owner value now, and the surface
   TUR-026 blocks and PRO-001 proposals need.
3. **TUR-027**: independent; required before TUR-028 and before any PRO-001
   slice that spends a model call.
4. **TUR-028a**: the quiet switch, after TUR-027; before any watch polls a
   live token.
5. **Any time, independent:** TUR-016a (F15), EVAL-001a (F11), INT-009a and
   b (F12), F8a, and the acceptance text of F7, F14, F19, F21 and F22, which
   lands with this document.
6. **PRO-001a (a1 to a6) → PRO-001w → PRO-001b**: deterministic intake,
   the withdrawal reconciler (w) and rules, carrying F10, F20's
   write-ahead and `integration_uncertain` notice and TUR-028b's poll gate
   (a), and F17 (b); PRO-001a is a1 to a6 in that order, after INT-009a,
   whose streamed decoder its poller reuses, and PRO-001w is w1 then w2.
7. **PRO-001c**: the assessor, after TUR-027 and EVAL-001 scaffolding.
8. **PRO-001d**: `auto`, last, after EVAL-001 evidence from recorded `observe`
   decisions.
9. **TUR-016c**: after TUR-016's backup and restore verification and TUR-016a.
10. **EVAL-001 scenarios** for F24 (delegation fidelity) and F13's
    replacement (ambiguous brief) when EVAL-001's harness exists; F24's frame
    and F13's template sentence only after a recorded failure.

### Shared building blocks

- **Outcome codes, stated once.** `NormalizeFailure` stores any (origin,
  code) pair outside `failureReasons` as `unknown`
  (`orch/runoutcome/outcome.go:410,472-481`), and
  `TestFailureReasonsHoldsExactlyTheApprovedMappings` requires the
  hand-written `approvedFailureMappings` to hold the same pairs
  (`orch/runoutcome/outcome_test.go:45,190-219`). So each new failure code
  is added to both, and a runtime-reported one to `runtimeProducerPairs`
  (`orch/runoutcome/producer_pairs_test.go:36`), in the slice that first
  writes it, whose tests assert the stored `error_code` and reason:
  `automation_grant_revoked`, `watch_grant_revoked` and
  `unattended_repeat_blocked` under the automation-policy origin map to
  `ReasonPolicyDenied`, as the existing `automation_tool_not_allowlisted`
  does (line 437), which this design reuses unchanged; and the tool-guard
  `assessor_tool_call` maps to `ReasonToolFailure`. Each new cancellation
  code (`owner_quiet`, `assessment_preempted`, `assessment_stale`,
  `profile_grant_revoked`) gets a constructor beside
  `ParentEndedCancellation` (`orch/runoutcome/outcome.go:259-261`), with
  the orchestrator-internal origin and `ReasonAbandoned`.
- **The maintenance tick, stated once.** Every background duty this design
  adds (PRO-001's lifecycle sweep, F20's `integration_writes` retention,
  TUR-026b's cancellation batches and TUR-028's drain resume) runs on one
  maintenance goroutine, not on the job reaper's: at
  `TURING_JOB_REAPER_INTERVAL_MS` or one minute, whichever is shorter, and
  at `defaultDeletionReconcileInterval` (one minute) when that is 0, as
  the session-deletion reconciler does (`orch/app/app.go:43,488-512`), but
  never longer, because the interval has no upper bound
  (`orch/config/config.go:317`) and the retention bounds below assume a
  tick of a minute at most. The session-deletion reconciler's interval is
  capped the same way, and each of its passes runs the lease-fenced stale
  recovery for its pending sessions' runs before advancing them, because
  it finishes the withdrawals the sweep begins, and only its final cascading `DELETE`
  (`orch/repository/session_delete.go:563`) removes a thread session's
  `watch_event` rows, job `userText`, answers, tool-call arguments and
  approval records. So none of them depends
  on the reaper being on, none delays orphaned-assignment recovery on the
  reaper's goroutine (`orch/service/runtime/service.go:1574-1587`), and each
  runs whether or not a watch, a delegation or quiet exists. Each duty's
  transactions keep the budgets their sections state. Whichever of
  TUR-026b and TUR-028a ships first adds the goroutine, since the roadmap
  orders neither before the other; each slice's row says so, and later
  slices add their duties to it.
- **Frozen grant, narrow-only revalidation, allowlist as ceiling (TUR-026).**
  Used by automations, delegation children and PRO-001 `auto` runs, checked
  at every tool call of an unattended run (for a PRO-001 run, with its
  event's current watch scope and rule predicate, `watchEventEligible`, and
  with `mayFireUnattended` still true for its rule), at the beacon and at
  registered-MCP dispatch, at entry, before any approval is consumed,
  and again in the read transaction that holds the dispatch fence
  (section 4, point 5), and
  again in the transaction that commits an unattended approval. Memory and
  team tools are refused without failing the run, as today; every other
  refusal, an integration tool's included, fails it visibly; the beacon's
  and registered-MCP dispatch's refusals share one repository call that
  fails the run and writes its audit row in one transaction, the row only
  when the run moved, and the runtime publishes the returned events. This is Turing's
  equivalent of DeepSeek's admission and Mandate store; there is no separate
  store.
- **One unattended predicate (TUR-027).** An `automation_runs` row or a
  `watch` session. It is written once into `jobs.unattended` at insert by the
  callers that create unattended runs and tested against it; TUR-027's order
  and TUR-028's cancellation read that flag. The gates read no session kind:
  TUR-026's `unattendedGrantCheck` and the memory service probe the
  `automation_runs` row, an assessor run's enforced empty set refuses its
  tools and its delegation at every gate (section 5, point 3), and PRO-001's pending-fire hook probes the
  `automation_runs` row itself (section 6, Performance), so "attended" keeps
  one definition (section 9, point 1).
- **The owner's quiet switch (TUR-028).** The one global gate on new
  unattended work, checked inside every claim, fire, enqueue and requeue
  transaction that creates or returns it, before every poll request and in
  each poll page's commit, where it is a stop that rolls the page back;
  turning it on also cancels a poll in flight (section 3, point 2). Turning it
  on is one write that fences all of them until a bounded drain has
  cancelled queued unattended work and pending fires; an unattended run
  that would be requeued while it is on is cancelled instead, by the one
  helper every path back to pending goes through (`releaseJobToPendingTx`,
  which `requeueAssignment`'s aborts now use too). Per-automation and
  per-watch switches stay.
- **The enforced selected set.** The specialist-job contract's
  `enforceSelectedTools`, already checked server-side before the policy, now
  also bounds every thread-session turn (Start-confirmed, owner-sent and
  `auto`). Scheduled automations do not get one: TUR-026's check bounds them
  (section 4, point 5) and keeps their failures visible.
- **One withdrawal path (PRO-001, F1 point 8).** Excerpts, proposal fields,
  assessor sessions and thread sessions are removed by one sweep over the
  existing session deletion lifecycle, on each excerpt's own clock: a thread
  session, with every answer in it, lives until its oldest excerpt expires; a
  session the owner detached has its excerpt rows cut and keeps the rest of
  its run history, tool-call and approval arguments included, the one
  exception, which the detach dialog discloses; and assessor sessions use
  `BeginWatchSessionDeletion`; a withdrawing session's pending fire is
  dropped rather than fired, and the next event opens a new thread session
  even while the withdrawal is still quiescing (section 6, point 1). An edit Turing reads replaces an excerpt
  without restarting its clock (F10; an edit inside an interval the backlog
  clamp skips is not read), and a dismissal stops storage at once while
  the sweep clears the thread's unused excerpts in bounded batches (F17).
- **Ids, not names.** A watched repository and a listed actor are each
  bound to the numeric id GitHub assigns, resolved once when the owner
  saves them; every request, gate and revalidation uses the id, and a name
  is display text, a repository's its last known name, refreshed by its id
  when polled and shown as last seen (section 3, points 2 and 3).
- **One search and recall predicate.** `watch_event` rows are never
  full-text indexed and thread sessions never match the predicate, so
  stranger text never reaches the owner's other chats.
- **`integration_writes`.** One ledger serves the gate's Turing-authored
  refusal (by id, or by body hash for the crash window) and is the
  write-ahead record of every comment; an uncertain outcome is never read
  back by Turing, and its notice links the issue by the `owner/repo` the
  owner approved (F20).
- **One failure threshold.** Three consecutive failures before a
  problem is surfaced, the same constant for watch streams (F10, whose
  `failed_polls` and `access_failures` streaks section 3's failure
  classifier defines), health
  entries (F19) and ordered channel delivery (F21); one success clears it
  (for a watch stream, a poll answered `200` or `304` with no failure, as
  section 3's classifier defines; a cut or unreached stream clears
  nothing). A
  definitive failure, such as a watch's `401` or a redirect on an
  id-addressed stream request (section 3's classifier), is surfaced at once.
- **The attention projection (UX-002).** The one place where approvals,
  consents, revoked grants, repeat and ceiling blocks, suspended watches and
  proposals become visible; a row leaves when its condition ends or when the
  owner dismisses that occurrence, never by age (F18).
- **The specialist-job contract.** Reused for the assessor, whose payload
  section 5, point 3 states; the only additions to the contract are a
  narrow-only `max_output_tokens` and `single_completion`, both behind
  team protocol 2. Its withheld pinned-memory
  snapshot, and the assessor's empty skill snapshot, are reused for every
  thread-session turn, so no stranger-steered turn carries the owner's
  persona, profile or skills.
- **`sessions.kind`.** A `watch` kind, added by a `sessions` rebuild, inherits
  every chat-only guard and is withdrawn through its own begin. Thread
  sessions stay `chat`, so the owner can see and manage them.
- **The `settings` table.** It holds TUR-028's switch beside `memory_enabled`
  until OPS-002's store, whose groups gain F22's revisions, takes both; the
  switch is compare-and-set on its own revision from the start (section 18,
  point 1).

### How Phase 2 composes with Phase 1

- **PRO-001** absorbs three Phase 2 features as acceptance: F10 settles its
  event identity, edits and failure handling; F17 makes a Dismiss stick; F20
  closes its loop guard's crash window. TUR-028 gates its poller, assessor and
  `auto` fires, and section 7's delayed-read rule means no event first read
  more than 15 minutes after its creation, after quiet, a suspension or a
  backlog, ever starts unattended work.
- **UX-002** gains F18's exits and never lists the quiet switch, which is the
  owner's own state.
- **TUR-016** gains a refusal (F15, before any of its backup work) and a
  rehearsal (F23, after it); the rehearsal reuses F15's check.
- **EVAL-001** gains a deterministic tier (F11) that every later prompt or
  schema change, including F24's frame, must pass.
- **Delivery** items (TUR-024, CHN-002, PRS-001) gain F14's eligibility and
  F21's ledger semantics as acceptance only; nothing is built before a
  consumer.

### Composition with existing work

- **Agent team.** TUR-026b makes the claim, the dispatch and egress consent
  honor today's grant and profile file. Unattended status alone does not
  refuse delegation; the absent roster does. `auto` runs and thread-session
  turns get no roster, and an assessor run gets no roster and an enforced
  empty set, which `CallTeamTool` checks before it reads the roster
  (section 5, point 3), so none can delegate.
  Delegation children and continuations are attended, so the quiet switch
  never touches them.
- **Automations.** Event automations reuse the row, allowlist, history, fire
  tail and dispatcher, but fire inside the poller's page transaction and run
  in their thread's current session (section 6, point 1); the
  save-time refusals of integration, memory and team tools stay, and Start
  and Confirm also refuse any tool that is not confined (section 5,
  point 2). TUR-026d
  changes every automation: its allowlist now bounds safe tools too
  (section 4, point 5), with its payload, routing and skills unchanged, and revoking it
  stops its reads as well as its mutations.
- **Channels (CHN-000 to CHN-002).** Unchanged gates; CHN-002 gains F4's rule
  that a later message is a new run with a new disclosure, and F14's and
  F21's acceptance.
- **GitHub integration.** INT-009 changes only what the two read tools ask
  GitHub for and return, and how far they can page; the write tool gains
  the write-ahead row.
- **EVAL-001.** Adds the deterministic tier EVAL-001a and these scenarios:
  proactive precision and abstention on recorded `observe` decisions;
  injection through event text yields no tool call outside the unattended
  read set; an outsider's detectable edit never becomes unattended work, and
  an undetectable one starts at most a run under that read set's ceiling; "revoked
  yesterday, event today" sends and mutates nothing; the assessor's
  small-model `unavailable` rate; an ambiguous brief is answered with a named
  assumption or question, never a guessed success (F13); delegation fidelity
  (F24); and, once an outbound consumer exists, a quiet daily report sends
  nothing (F14).
- **TUR-014.** Gains F19's health contract, including section 21's one
  status rule for a watch (`paused` under quiet or when disabled, unless
  suspended, which is `error` with `needs_person`).
- **OPS-002.** Gains F22's compare-and-set and becomes the home of TUR-028's
  key.
- **AGT-000.** Records the attended reserve slot as a measured hypothesis.
- **AGT-002 and AGT-003.** AGT-002 may gain F24's frame after a failed
  scenario. AGT-003's roadmap entry is unchanged; this design records when a
  typed handoff should be reconsidered (F13, section 28).
- **AGT-004.** Applies TUR-026's narrow-only rule to `team.ask`; a request
  never revives a grant. Recorded here as an input, not yet an acceptance
  change.
- **MCPP-005.** Records releasing the slot during an approval wait as an input
  to its custody design.

## 28. Rejected and out of scope

| Item | Reason |
| --- | --- |
| `danger-full-access` workers as the operator's OS user (`dsh:src/app/sessions.ts:152`; `dsh:docs/006-deployment.md:19`) | Owner decision 7 and `docs/NORTH_STAR.md:303` bar unsandboxed host shell. |
| A router model with shell tools that also assesses (`dsh:cordis.patch.yml:139-167`; `dsh:src/app/assessment.ts:85-89`) | Turing's assessor is tool-less; routing is deterministic Go. |
| PATH shims and process-wide environment mutation (`dsh:src/infra/environment.ts:79-125`) | Not a boundary by DeepSeek's own statement; replaced by XTOOL-001 fixtures. |
| Mandate table, `work_grants`, free-text scope | Duplicates frozen grants; scope text is not enforceable. |
| Subscription table, ungated fan-out, resume under an old Mandate, context into live runs | See section 6. |
| DeepSeek's default rules `copy` and `visual` (`dsh:src/policy.ts:62-65`) | Repository-coding specific; Turing ships no enabled rule. |
| `ignoredFromCard` shortcut | Exists because DeepSeek routes with a model; Turing's predicate is deterministic. |
| Rule overrides of process and communication | Path to self-escalation; tools and approvals stay code-enforced. |
| Unlisted-addressed `explicit` admission cell; cross-source identity mapping | Single-user hub; no stranger can make an explicit request. |
| Agent-saved prompt overrides (`dsh:docs/004-prompts.md:38`); per-request prompt reload | Self-modification of instructions; runs stay frozen. |
| Prompt version history in the database | Derived copies of user files; Git and backups cover history. |
| 30-minute background grace, 5-second backgrounding, handoff-exempt jobs | They serve full-access shells; MCPP-005 owns long waits. |
| `maxWorkers` default 3 or an extra inference slot now | Unmeasured on the reference machine; AGT-000 decides. |
| Six-tab supervision panel, activity counts, executor-load gauges | UX-002 shows obligations only; health stays with TUR-014. |
| DeepSeek's SQLite store as "memory" | `dsh:src/core/memory.ts:1-10` is an in-memory test repository, not personal memory. |
| An evaluation harness to copy | None exists: the test script is `vitest run` (`dsh:package.json:47-51`); EVAL-001 stays Turing's. |
| Porting the TypeScript Engine or its `Pick` roles | Section 12. |
| New IDs ADM-001, MND-001, RULE-001, SUB-001, SUP-001, CAP-001, ENV-001, CFG-001 (mai draft) | Covered by PRO-001, TUR-026, TUR-027, UX-002 or existing items. |
| GitHub notifications as the event source, with per-subject hydration (`dsh:src/sources/github.ts:297-374`) | The endpoint refuses the fine-grained token Turing recommends, and each notification needs a hydration request for actor and labels; per-repository streams replace it (section 3). |
| Self-origin by login (`dsh:src/sources/github.ts:227`) | Turing writes with the owner's token, so it would refuse the owner's own activity; Turing-written platform ids are refused instead. |
| A policy-version hash on watches and runs (`dsh:src/policy.ts:111-118`) | Direct re-reads of the current watch and automation rows answer the same question. |
| Template hashes in the `team-seeded` receipt | The past-template-hash list classifies every case on its own. |
| Reordering the routing-work page by attendance | It claims nothing, and its keyset cursor depends on its order. |
| An `off` accept mode (`dsh:src/policy.ts:84`) | A watch's enable switch already means it (section 3). |
| Label changes, edits and state changes as events attributed from an issue snapshot | The snapshot names the author, not who changed it; DeepSeek's issue intake is creation-only for the same reason (`dsh:src/sources/github.ts:352-353`). |
| **F13** Typed "needs an owner decision" specialist handoff (`dsh:src/app/tools/executor.ts:76-80`; `dsh:prompts/handoff-worker.md:1-5`) | Rejected now, after a 2–2 split (grok and opus reject; sol and mai would adapt it as a local, terminal, one-shot handoff answered by a new owner turn). No consumer needs it: a specialist runs only inside an attended turn, and its continuation already relays a missing fact to the owner in chat, where the answer is a new turn, which is the behaviour the adaptation would build, with no new state. Unattended runs have no team tools, so there is no unattended consumer either. A model-written question in a host-styled card beside approvals is a social-engineering surface ("paste your API key to continue"), and visual distinction does not remove it. The real risk, a guess reported as success, is measured instead: EVAL-001 gains an "ambiguous brief" scenario in which the child must name its assumption or the missing fact and the continuation must surface it. Only if that fails do the three seeded templates gain one sentence ("if a missing fact changes the result, stop and return `Needs your decision:` with one question"), shipped to existing installs through F8a. Revisit a typed handoff only when AGT-004 or unattended tool access creates a consumer that cannot ask in chat, and then only as a host-enumerated choice answered by a new owner turn. |
| DeepSeek's processing pause as specified: a held queue, moved deadlines, continued source reads, settable by a tool (`dsh:docs/003-execution.md:192-200`; `dsh:src/core/evals.ts:30-45`; `dsh:src/app/settings.ts:202-209`) | Held work can expire or flood the one slot on resume, moved deadlines would extend consent, and source reads are traffic. TUR-028 keeps the owner's switch and none of these (section 18). |
| Time-based expiry of attention rows (`dsh:docs/002-authorization.md:155`) | A failure that is still current must not vanish on a clock; rows leave on their condition or on the owner's occurrence-bound Dismiss (section 20). |
| A hidden HTML marker in Turing's GitHub comments (`dsh:src/sources/github-channel.ts:34-39`) | It would tag every comment the owner posts through Turing, in public; a write-ahead body hash gives the same match (section 22). |
| List-then-post as "exactly once" (`dsh:src/sources/github-channel.ts:37-39`) | It races; Turing never repeats a possibly-applied mutation, and an uncertain comment stays uncertain, with a link the owner can check (section 22). |
| A per-session cache of fetched GitHub text (`dsh:src/app/resources.ts:103-110`) | A second copy of stranger text to withdraw; a refetch bound to a `body_revision` hash replaces it (section 15). |
| The database's applied version in `Version` and `./turing status` | Equal to the binary's once F15's check passes; a refused orchestrator cannot answer (section 17). |
| A second running Compose stack for upgrade rehearsal | A one-off check container with no network runs no service loop at all (section 25). |
| DeepSeek's character budgets and English word lints (`dsh:tests/model-surface.spec.ts:36-40`) | Characters are not tokens and the numbers are DeepSeek's; F11 records exact bytes per configuration. |
| A client three-way settings merge now (`dsh:src/client/draft.tsx`) | One writer today; revisit with MOB-001 (section 24). |
| Steering a running turn with a new message, and delivered/read/handled input tracking (`dsh:src/app/delivery.ts:66-80`) | Both gap reviewers rejected it: each owner message is a new run in turn order, and live steering would need durable mid-turn checkpoints and new lease and recovery semantics. Cancel plus a new message covers the intent. |
| Agent-run setup and autonomous repair, an external watchdog, a stop marker and a control lock (`dsh:SETUP.md:8-14`; `dsh:scripts/dsh-superagent:219-229`) | Both gap reviewers rejected it: OPS-001 already ships guided `./turing setup`; an agent that installs, holds credentials or repairs its own authority violates owner decision 7; Compose already restarts services. |
| Pull-request workflow rules: yielding to a duplicate human branch, review-round caps, history rewriting for reviewer credit (`dsh:prompts/team/delivery.md:16-28`) | Both gap reviewers rejected it: Turing has no PR-authoring role, its GitHub tools are issue, file and comment only, and closing, merging or rewriting are external mutations that need argument-bound approval. |
| A second UI locale, locale-parity tests, and documentation-shape tests (`dsh:tests/locales.spec.ts`; `dsh:tests/docs.spec.ts`; `dsh:tests/class-docs.spec.ts`) | Both gap reviewers rejected it: no second locale is asked for, and `tools/docs` already guards Turing's documentation. Add a key-parity test the day a second `.arb` file lands. |
| A state-directory single-instance lock (`dsh:src/app/runtime.ts:93`) | Rejected by Sonnet: Turing's cross-process state is row-fenced (leases, fencing tokens, compare-and-set on `next_due_at`), and F15 plus the image label contain an older second orchestrator. |
| Built-in conduct text in prompts (DeepSeek's `prompts/team/*.md`) | Rejected by Sonnet: pinned tokens on every 7B turn, and the persona stays human-authored; F11 guards framings instead, and EVAL-001 measures honesty. |

## 29. Provenance

- **Drafting models (Phase 1, independent):** grok-4.7 (knights-grok),
  mai-code-2-flash (knights-mai), gpt-6.1-sol (knights-sol) and
  claude-opus-5.5 (knights-opus). Each read the DeepSeek repository at
  `6337fc8` and Turing at `310dbb36` and drafted a plan for F1 to F9 without
  seeing the others.
- **Phase 2 gap reviewers:** GPT 6 Astra and Claude Sonnet 5.5 each read the
  DeepSeek repository in detail, independently, against Turing and the Phase 1
  plan, and proposed further candidates. Their fifteen proposals became F10 to
  F24; F13 and F16 were disputed between them (one proposing, the other
  rejecting). Four candidates both reviewers rejected, and two Sonnet
  rejected, are recorded in section 28 (corrected in round 3,
  provenance-both-rejected-count-wrong).
- **Drafting models (Phase 2, independent):** the same four models, grok-4.7,
  mai-code-2-flash, gpt-6.1-sol and claude-opus-5.5, each drafted a plan for
  F10 to F24 against a frozen copy of this document's Phase 1 plan, with
  Turing at `228b4d04`, without seeing the others.
- **Synthesis:** one synthesizer agent dispatched by the Knights host unified
  the Phase 1 drafts, and one unified the Phase 2 drafts into this document,
  integrating the Phase 2 amendments into the Phase 1 text they change
  (sections 3, 5, 6, 7, 8 and 12) so that no rule is stated twice. Every
  DeepSeek and Turing citation kept here was re-checked against the files,
  and the two GitHub behaviours relied on (comment editing by write-access
  users, and secondary rate limits without headers) against GitHub's
  documentation; claims that could not be verified were dropped. The two
  Turing gaps reported by the opus Phase 1 draft were confirmed in code and
  became TUR-026a and TUR-026b. The Phase 2 synthesis found one more defect in
  the Phase 1 text, issue resource ids colliding across a watch's
  repositories, and fixed it in section 3.
- **Review panel:** the same four models review this document until all
  agree. **Round 1** returned 32 findings, **round 2** returned 25 (four of
  them groups of duplicates), **round 3**, the first over the whole
  document including Phase 2, returned 25 (three groups of duplicates),
  **round 4** returned 20 (four groups of duplicates) and **round 5**
  returned 9 (one group of duplicates), each from all four reviewers, and
  **round 6** returned 10 (one group of four duplicates, on F20's
  reconcile) from three reviewers, the fourth reporting none, and **round
  7** returned 14 (two groups of duplicates, five findings on F20's Check
  GitHub and two on the poller's `since`) from all four, and **round 8**
  returned 11 (three groups of duplicates: three findings on the poller's
  cursor, two on the label gate and two on F20's decision log) from all
  four, and **round 9** returned 6 (one pair of duplicates, on the poller's
  progress rule) from three reviewers, the fourth reporting none, and
  **round 10** returned 13 (a group of six on the poller's clamp and paging,
  and a group of three on TUR-014's roadmap threshold) from three
  reviewers, the fourth reporting none, and **round 11** returned 9 (one
  pair of duplicates, on DeepSeek's floor citation) from all four, and
  **round 12** returned 4 from two reviewers, the other two reporting none,
  and **round 13** returned 6 from three reviewers, the fourth reporting
  none, and **round 14** returned 7 from three reviewers, the fourth
  reporting none, and **round 15** returned 2 from two reviewers, the other
  two reporting none, and **round 16** returned 3 from two reviewers, the
  other two reporting none, and **round 17** returned 8 (a pair of
  duplicates on a poll in flight and a group of three on poll slots) from
  three reviewers, the fourth reporting none, and **round 18** returned 4
  (a pair of duplicates on the poll deadline starving later streams) from
  three reviewers, the fourth reporting none, and **round 19** returned 2
  from two reviewers, the other two reporting none, and **round 20**
  returned 5 (a pair of duplicates on the current thread session) from all
  four, and **round 21** returned 6 (a pair of duplicates on a connection
  revoked between polls) from three reviewers, the fourth reporting none,
  and **round 22** returned 6 (a pair of duplicates on a stopped poll's
  rotation write) from two reviewers, the other two reporting none, and
  **round 23** returned 6 from three reviewers, the fourth reporting none,
  and **round 24** returned 6 (a pair of duplicates on a stopped poll's
  rotation write) from three reviewers, the fourth reporting none, and
  **round 25** returned 6 (a pair of duplicates on the suspension write's
  predicate) from three reviewers, the fourth reporting none, and
  **round 26** returned 3 from two reviewers, the other two reporting none,
  and **round 27** returned 3 from two reviewers, the other two reporting
  none, and **round 28** returned 6 (a pair of duplicates on an old poll
  across a repository's removal and re-add) from three reviewers, the
  fourth reporting none, and **round 29** returned 2 (a pair of duplicates
  on poll requests addressed by a reusable repository name) from two
  reviewers, the other two reporting none, and **round 30** returned 5
  from three reviewers, the fourth reporting none, and **round 31**
  returned 5 (a pair of duplicates on the display name behind `304`s)
  from all four reviewers, and **round 32** returned 5 from three
  reviewers, the fourth reporting none, and **round 33** returned 5
  (three of them, from grok and opus, on the display
  name's stale window) from all four reviewers, and **round 34** returned 2
  from one reviewer, the other three reporting none, and **round 35**
  returned 1 from one reviewer, the other three reporting none, and **round 36**
  returned 1 from one reviewer, the other three reporting none, and
  **round 37** returned 5, all on the round-36 dispatch refusal, from
  all four reviewers, and **round 38** returned 8, all on TUR-026's
  dispatch refusal, from three reviewers, mai reporting none, and **round 39**
  returned 1 (the attended beacon's cost) from one reviewer, the other three
  reporting none, and **round 40** returned 2 from two reviewers, the other
  two reporting none, and **round 41** returned 1 from one reviewer, the
  other three reporting none, and **round 42** returned 5 from three
  reviewers, sol reporting none, and **round 43** returned 4 (three
  duplicates on the attention page token) from three reviewers, mai
  reporting none, and **round 44** returned 4, all one finding on that
  page token, from all four reviewers, and **round 45** returned 4 from
  three reviewers, grok reporting none, and **round 46** returned 4 from
  two reviewers, grok and mai reporting none, and **round 47** returned 4
  (two duplicates on the summary's memory figure) from three reviewers,
  mai reporting none, and **round 48** returned 2 from two reviewers, grok
  and mai reporting none, and **round 49** returned 3 from two reviewers,
  grok and mai reporting none, and **round 50** returned 6, all on round
  49's deletion batches, from three reviewers, mai reporting none, and
  **round 51** returned 5 (three duplicates on an already-indexed column)
  from three reviewers, mai reporting none, and **round 52** returned 6
  from two reviewers, grok and sol reporting none, and **round 53**
  returned 4 from three reviewers, mai reporting none, and **round 54**
  returned 6 (one duplicate on the reaper wording) from three reviewers,
  mai reporting none, and **round 55** returned 3, one finding on the
  quiet release, from three reviewers, mai reporting none, and **round 56**
  returned 2 from two reviewers, grok and mai reporting none, and **round
  57** returned 2 from one reviewer, the other three reporting none, and
  **round 58** returned 3 from two reviewers, grok and mai reporting none,
  and **round 59** returned 4 (one duplicate on `singleCompletion`) from
  two reviewers, mai and sol reporting none, and **round 60** returned 5
  from all four reviewers, and **round 61** returned 2 from two reviewers,
  grok and mai reporting none, and **round 62** returned 4 from three
  reviewers, mai reporting none, and **round 63** returned 4 (one
  duplicate on the receipt ceiling) from two reviewers, mai and sol
  reporting none, and **round 64** returned 5 from two reviewers, mai
  and sol reporting none, and **round 65** returned 6 (two pairs of
  duplicates) from three reviewers, mai reporting none, and **round 66**
  returned 4 from two reviewers, grok and mai reporting none, and **round
  67** returned 5 (two issues) from three reviewers, mai reporting none,
  and **round 68** returned 3 from one reviewer, the other three reporting
  none, and **round 69** returned 7 (two pairs of duplicates) from all
  four reviewers, and **round 70** returned 7 from three reviewers, mai
  reporting none, and **round 71** returned 4 from three reviewers, mai
  reporting none, and **round 72** returned 6 from three reviewers, sol
  reporting none, and **round 73** returned 5 (three issues) from all
  four reviewers, and **round 74** returned 5 from two reviewers, mai and
  sol reporting none, and **round 75** returned 6 from two reviewers, grok
  and mai reporting none, and **round 76** returned 3 (one duplicate)
  from two reviewers, mai and sol reporting none, and **round 77** returned
  5 (one duplicate) from three reviewers, mai reporting none, and **round
  78** returned 2 from one reviewer, the other three reporting none, and
  **round 79** returned 3, one finding, from three reviewers, mai
  reporting none, and **round 80** returned 2, one finding, from two
  reviewers, mai and sol reporting none, and **round 81** returned 2 from
  two reviewers, mai and sol reporting none, and **round 82** returned 3
  from two reviewers, grok and mai reporting none, and **round 83**
  returned 3 from two reviewers, grok and mai reporting none, and **round 84**
  returned 8, six findings, from all four reviewers, and **round 85**
  returned 2 from two reviewers, mai and sol reporting none, and **round 86**
  returned 3, two findings, from two reviewers, mai and sol reporting none, and **round 87**
  returned 1, from grok alone, and **round 88** returned 1, from mai
  alone, and **round 89** returned 4 from three reviewers, mai reporting
  none, and **round 90** returned 2 from two reviewers, mai and sol
  reporting none, and **round 91** returned 2 from two reviewers, grok
  and mai reporting none, and **round 92** returned 2, from opus alone,
  and **round 93** returned 5 from three reviewers, grok reporting none,
  and **round 94** returned 3 from two reviewers, mai and sol reporting
  none, and **round 95** returned 4, three findings, from two reviewers,
  mai and sol reporting none, and **round 96** returned none from all four
  reviewers. All 509 were verified against Turing's and DeepSeek's code and resolved: 30 of round
  1's were fixed directly, its other two (the Phase 2 incorporation and the
  `docs/NORTH_STAR.md` pointers) were deferred within that run and done in
  the next revision, and all 25 of round 2's, all 25 of round 3's, all 20
  of round 4's, all 9 of round 5's, all 10 of round 6's, all 14 of
  round 7's, all 11 of round 8's, all 6 of round 9's, all 13 of round
  10's and all 9 of round 11's were fixed, 3 of round 12's were fixed
  and its fourth rejected, all 6 of round 13's, all 7 of round 14's,
  both of round 15's, all 3 of round 16's and all 8 of round 17's were
  fixed, 3 of round 18's were fixed and its fourth rejected, both of
  round 19's, all 5 of round 20's, all 6 of round 21's, all 6 of
  round 22's, all 6 of round 23's, all 6 of round 24's, all 6 of
  round 25's, all 3 of round 26's, all 3 of round 27's, all 6 of
  round 28's, both of round 29's and all 5 of round 30's were fixed, 4
  of round 31's were fixed and one rejected, and all 5 of round 32's,
  all 5 of round 33's, both of round 34's, round 35's one, round 36's one, all 5 of round 37's, all 8 of round 38's and round 39's one were fixed, one of round 40's was fixed and its other rejected as already covered, and round 41's one, all 5 of round 42's, all 4 of round 43's, all 4 of round 44's, all 4 of round 45's, all 4 of round 46's, all 4 of round 47's, both of round 48's, all 3 of round 49's and all 6 of round 50's (by withdrawing round 49's batches) all 5 of round 51's, all 6 of round 52's, all 4 of round 53's, all 6 of round 54's, all 3 of round 55's, both of round 56's, both of round 57's, all 3 of round 58's, all 4 of round 59's, all 5 of round 60's, both of round 61's, all 4 of round 62's, all 4 of round 63's, all 5 of round 64's, all 6 of round 65's, all 4 of round 66's, all 5 of round 67's, all 3 of round 68's, all 7 of round 69's, all 7 of round 70's and all 4 of round 71's were fixed, 5 of round 72's were fixed and its sixth rejected as round 46 had settled it, all 5 of round 73's, all 5 of round 74's, all 6 of round 75's, all 3 of round 76's, all 5 of round 77's, both of round 78's, all 3 of round 79's, both of round 80's, both of round 81's, all 3 of round 82's and all 3 of round 83's were fixed, 7 of round 84's were fixed and its eighth rejected, both of round 85's, all 3 of round 86's, round 87's one, round 88's one, all 4 of round 89's, both of round 90's, both of round 91's and both of round 92's were fixed, 4 of round 93's were fixed and its fifth rejected, 2 of round 94's were fixed and its third rejected, and all 4 of round 95's were fixed; round 7's F20 group was
  fixed by removing the owner's Check GitHub (INT-009c) it concerned, and
  round 8's cursor group by adopting DeepSeek's `since`-only cursor. Each
  fix is a "Round N review" row, naming its finding ids where it had them,
  in the decision log of the feature it changed; the one round 3 finding
  about this section is noted above, and round 6's Goal correction
  (spec-goal-round-count-stale) is in section 1, which now defers to the
  Status line and this section. Eight findings were rejected: round 12's
  citation-prefix finding, because every cited path resolves under the
  citation conventions, round 18's DeepSeek abort-citation finding,
  because line 154 is the stopped predicate it names, and round 31's
  repository-route finding, because GitHub's own rename redirect targets
  `/repositories/{id}` and that route answers, and round 40's cross-watch
  rate-budget finding, because the per-connection reserve already reads
  GitHub's account-wide figure, and round 72's preflight finding, because
  round 46 had settled it, and round 84's body-revision finding, because
  only the connection's own credential is redacted, to as many newlines,
  so two bodies that hash alike after redaction give byte-identical slices,
  and a hash taken before redaction would hand the model a digest of the
  token, and round 93's cross-repository loop-guard finding, because
  round 13 chose that over-match as the safe direction, and round 94's
  proposal to drop `watch_event_expiries`, because nothing else links an
  event to its message; each is recorded with that evidence in the review
  dispositions appendix, as are the three fixes that depart from a
  reviewer's recommended remedy, round 11's cursor bound, round 16's
  uncounted timeout and round 22's hook gate, and the two round 42 fixes
  that depart from a detail of the panel's disposition (the event fire's
  slice and the paging watermark), and round 45's ledger refusal, built
  without the checksum witness it also proposed, and round 87's replaced
  credential, fixed by narrowing the claim rather than keeping old tokens. The next round's
  state is on the Status line.
- **Phases:** Phase 1 covers the nine candidates from the research report.
  Phase 2 covers the fifteen from the gap analysis: fourteen adopted or
  adapted, one rejected. The `docs/NORTH_STAR.md` pointers for every new and
  amended roadmap item land in this same change.
- **How disagreements were resolved:** on the merits, with performance and
  security/privacy first and Turing's invariants binding; reuse of an existing
  Turing mechanism won over a new one; content was weighed, not draft length
  or vote count. The material disagreements and their resolutions are in each
  feature's decision log. The hardest in Phase 1 were F3 (a local model judge
  or owner-written deterministic rules), F4 (a subscription mechanism or
  none) and F2 (a new grant entity or narrow-only revalidation of the grants
  Turing already freezes). The hardest in Phase 2 were F13 (a typed
  owner-decision handoff, split two to two and rejected for now), F16 (a
  global quiet switch, and whether it holds or cancels queued unattended
  work) and F20 (a published marker or a private body hash).

## Appendix: Review dispositions

| Finding | Recommendation | Disposition | Evidence |
| --- | --- | --- | --- |
| Round 11, opus, pro001a-full-page-paging-makes-walk-shift-loss-routine | After a walk of several pages, `next = max(since, min(N − 5 s, updated_at(first item of the last page read) − 1 s))` | The finding is right and is fixed; the recommended bound is replaced by page 1's last item, `next = max(since, min(N − 5 s, P − 1 s))` (section 3, point 2) | In a 250-item walk with one-second spacing, update a page-1 item between the requests for pages 1 and 2: item 101 shifts to position 100, page 2 returns items 102 to 201, and page 3 starts at item 202. The recommended bound stores `updated_at(202) − 1 s = updated_at(201)`, and GitHub's exclusive `since` then never returns item 101, which sorts before item 202, so the fixture the round asked for would fail. Any item a removal shifts across any page boundary sorts after the last item of a page already read, and page 1's last item is the earliest of those, so `P − 1 s` is the largest bound the walk can prove loses nothing. It costs throughput behind a budget (under 100 new items a poll, which `backlog` shows) and holds `since` before a page 1 of 100 items sharing one second, which section 3, point 2 states. |
| Round 12, mai, incorrect-turing-citation-prefixes | Replace shortened Turing paths such as `orch/config/config.go` with repository-relative paths and re-check every cited range | Rejected; no change | The "Citation conventions" paragraph at the top of this document defines `orch/` as `turing-backend/orchestrator-go/internal/` and `agent-runtime-go/` as `turing-backend/agent-runtime-go/`. A host script resolved all 94 backticked Turing paths under those conventions, and every cited file exists. The only strings it did not resolve are prose names (`init.sh`, `persona.md`, `profile.md`, DeepSeek's `cordis.patch.yml`) and the proposed new file `testdata/model_surface_bytes.json`. |
| Round 16, grok, pro001a-above-floor-timeout-still-counts-as-failure | State once in the classifier that a timeout above the floor counts nothing, with a fixture asserting the count stays 0 across halving polls | The finding is right and is fixed by deleting what it concerned: following opus's pro001a-per-page-reset-repeats-timeout-cycle, `per_page` is fixed at 100, the halving and the floor are gone, and every missed request deadline is a transient failure (section 3, point 2 and the failure classifier) | With the whole-request timeout replaced by header, idle-read and 5-minute deadlines, a slow page that keeps arriving completes, so no uncounted timeout remains to exempt; a request that misses a deadline is retried at the same size and counts, as every other transient failure does. The orchestrator's integration client sets both a 30-second `ResponseHeaderTimeout` and a 30-second whole-request `Timeout` (`orch/service/integrations/github.go:34,43`); the poller keeps the first and drops the second. |
| Round 18, grok, pro001a-dsh-abort-citation-points-at-retry-type | Replace 154 with 153 in `dsh:src/sources/github.ts:154,183,214,226`, because 154 is `export interface Retry` and the stopped predicate is 153 | Rejected; no change. The citation already points at the stopped predicate | At `6337fc8`, `grep -n` on `src/sources/github.ts` (459 lines, LF endings) gives line 154 as `const stopped = (ctx: SourceContext, round: Round): boolean => !!round.signal?.aborted \|\| !ctx.config(SOURCE)` and line 155 as `export interface Retry`; line 153 is the last line of `newRound`. Lines 183, 214 and 226 are the stream check, the signal-bound request and the per-item check, as cited. Changing 154 to 153 would cite a non-check. |
| Round 22, opus, pro001-hook-cost-claim-ordinary-turn | Restate the hook's cost, or gate the hook on a field the transition already holds, such as TUR-027's `jobs.unattended` | The finding is right and is fixed by restating the cost (section 6, Performance): one primary-key probe of `automation_runs` per terminal transition. The `jobs.unattended` gate is not adopted | The transition reads only the run and its assistant message (`runRowQuery`, `orch/repository/run_state.go:416-426`), not the job, so the flag is not already held. `automation_runs` is keyed by `run_id` (`orch/db/schema/0009_automations.sql:71`), so the hook's probe is itself one primary-key read, while the job is found by its run only through the non-primary `idx_jobs_run` (`orch/db/schema/0025_jobs_run_index.sql:4`). The gate would replace that probe with an indexed read for every run and add the probe back for every unattended run, costing more, not less. The `automation_runs` row is also the unattended predicate's own definition (section 27). |
| Round 31, mai, pro001-numeric-repository-api-route | Address requests by a source-verified route, or resolve the id to `owner/repo` before each request, because Turing's request builder uses `/repos/{owner}/{repo}/…` (`orch/service/integrations/github.go:128-198`) | Rejected; no route change. Section 3, point 2 now cites GitHub's rename redirect as the id route's basis | The host checked GitHub's public API live, unauthenticated, in November 2026: `GET https://api.github.com/repos/github/linguist` answers `301` with `Location: https://api.github.com/repositories/1725199`, so GitHub's own rename redirect targets the id route, and `GET https://api.github.com/repositories/1725199/issues?per_page=1` answers `200`. Turing's builder serves attended tools that name `owner/repo`; it does not limit which routes GitHub serves. Resolving the id to a name before each request would reintroduce the reused-name substitution round 29 removed. |
| Round 40, mai, pro001-cross-watch-rate-budget | Add one orchestrator-level GitHub API budget shared by every watch and connection, gating polls on remaining primary-limit capacity | Rejected as a new mechanism; one clarifying sentence added (section 3, Performance, "The owner's token keeps headroom") | The existing per-connection reserve already gates every watch's polls on GitHub's own `x-ratelimit-remaining`, which is the account's shared figure for all of its tokens, so it already acts as the account-level budget across watches and connections; between two responses at most 4 polls run (the slots) of at most 80 requests each, 320 in all, under the 1,000-request reserve, so concurrent watches cannot overshoot it. A second budget would duplicate GitHub's own counter. Round 62 found that the primary counter does not see `304`s against the secondary limit and added a process-local request pacer (section 3, Performance), still no table, service or shared budget. |
| Round 42, opus, tur027-assessor-acceptance-in-slice-before-assessor-exists (disposition detail) | Put the `jobs.unattended == RunUnattended` check for the event fire's enqueue path in PRO-001b | The finding is applied; only the slice is corrected: the event fire's check is in PRO-001d (section 7) | PRO-001a refuses `auto` until PRO-001d (section 3, Delivery slices), and an event match on an `observe` watch only records a proposal (section 7, Which matches may fire). PRO-001b's only new enqueue path is the Start-confirmed turn, which is attended, so the event fire first creates an unattended run in PRO-001d. |
| Round 42, mai, ux002-keyset-cursor-unspecified (disposition detail) | Make the watermark the maximum event sequence the first page observed | The finding is applied with a time watermark: the first page's read time, against each row's `since` (section 8, point 2) | `events.sequence` is unique only within a session (`UNIQUE(session_id, sequence)`, `orch/db/schema/0001_initial.sql:87-91`), so there is no global sequence to take a maximum of, and approvals, delegation consents, automation occurrences, deletion receipts, watches and proposals are rows of their own tables, not events. Every row already carries `since`, so the same exclusion needs no new column. |
| Round 45, mai, f15-missing-migration-ledger-fail-open (remedy detail) | Refuse a nonempty database with no `schema_migrations`, and require an intact ledger or checksum witness | The refusal is applied (section 17); the checksum witness is not built | Deleting a ledger row or table is already stated not to be a recovery, the database is the single owner's own file, and a witness would be a second ledger that the same hand could edit; the one `sqlite_master` read refuses every database no Turing build wrote. |
| Round 72, mai, f15-preflight-after-db-open | Add a read-only `schema_migrations` preflight before `db.Open`, with no chmod or WAL switch, and assert file bytes and sidecars unchanged | Rejected; the summary row's 'before any write' is narrowed to 'before any row, schema or skills write' | Round 46 settled this: a newer Turing build's database is already WAL and 0600 (`orch/db/connection.go:34-58`), so `db.Open` changes nothing on it; only a foreign file's mode or journal can change, which touches no row or schema; the refusal promises exactly that, and TUR-016a asserts it. |
| Round 84, mai, github-body-revision-after-redaction | Hash the decoded body before redaction for `body_revision`, keeping the redacted text for slices, with a credential-only edit fixture | Rejected; no change | `redactCredential` replaces only the connection's own credential, with as many newlines (`orch/service/integrations/github.go:97-113`), so two bodies hash alike after redaction only when their redacted bytes are identical, and slices spliced from them then reassemble a body the model could have read whole; offsets already align because redaction keeps length. Hashing before redaction would return to the model a digest computed over the token beside text it can see, which section 15's redaction exists to prevent |
| Round 87, grok, WATCH_ROTATED_CREDENTIAL_RESTORES_REDACTED_TOKEN (remedy detail) | Keep each replaced connection credential, sealed, for the 30-day excerpt window and redact every kept value, with a rotate-and-re-list fixture | The finding is applied by narrowing the claim (section 3, point 2, Bounded decoding): only the token the connection holds is redacted, a replaced one is stored as written on a later read, and replacing a credential does not revoke it. Replaced credentials are not kept | `redactCredential` takes the one credential in hand (`orch/service/integrations/github.go:97-113`), and section 15's tools already draw that line. A sealed copy of every retired token kept only to hide it is one more secret at rest, a larger exposure than a token the owner has already replaced; a leaked token is revoked at GitHub, where it can be abused |
| Round 93, mai, f20-cross-repository-loop-guard-match | Bind the ledger and the loop guard to GitHub's numeric repository id plus issue number and body hash, with a different-repository fixture | Rejected; no change | Round 13 and round 14 settled the key: `(issue_number, body_sha256)` is the smallest key a rename or a second connection cannot change, and it needs no request; `github.create_comment` addresses `owner/repo` (`orch/service/integrations/github.go:128-198`), so a repository id at POST time would cost a lookup before every comment. The only effect of the over-match is that a byte-identical body on the same issue number in another repository, within 30 days, is refused as Turing-authored: Turing starts less, never more, which section 3, point 4 states as the safe direction |
| Round 94, opus, PRO001_WATCH_EVENT_EXPIRIES_TABLE_YAGNI | Drop `watch_event_expiries` and have the watch-deletion sweep cut each detached copy in the batch that deletes its `watch_events` row, keeping the 30-day cut through `watch_events` while the watch exists | Rejected; the table stays, and the reviewer's companion finding on its scope is applied | No `watch_events` column names the message an event became: `started_run_at` is a time, and `automation_runs.watch_event_id` exists only for an `auto` fire (section 3, point 6), so cutting through `watch_events` needs a new linking column and index as well, and two cut paths, one at expiry and one at deletion. The table is that link, written once with the message and cascading with it, and it gives the cut one path with the 30-day timing the detach dialog states |
