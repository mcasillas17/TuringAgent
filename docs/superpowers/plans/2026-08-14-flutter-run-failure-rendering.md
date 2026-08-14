# Flutter Run-Failure Rendering Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The client ignores `agent.run.failed` entirely, so most run failures are completely silent — the user sends a message and gets nothing, forever. Render them.

**Architecture:** Flutter-only. **No backend change, no proto change, no regeneration.** The event already reaches the client as a string; it simply has no case.

**Tech Stack:** Flutter, `turing-client/turing_app`.

---

## Why this, and why now

`_applyEvent`'s switch (`chat_screen.dart:225-251`) has exactly one `agent.run.*` case — `agent.run.step`. `agent.run.failed`, `.started`, `.completed` and `.queued` all fall through and do nothing.

That means every failure that is **not** retry exhaustion is invisible: `model_provider_unavailable`, `tool_discovery_failed`, `message_fetch_failed`, `job_timeout`, and any terminal failure from reconciliation. The user sees their own message and then silence, indefinitely.

This is also the honest repair for a workaround. #33 added a "Gave up after N attempts" *notice* specifically because the client cannot render failures — a `agent.run.step` standing in for the failure event we were unable to show. This plan fixes the underlying gap.

By `docs/VISION.md`'s decision order this is **rule 3** ("something that silently does nothing"), which outranks the queued session-search work at rule 5.

## What is already true (verified — do not re-derive)

- **The event reaches the client.** `grpc_mappers.dart:121-122` maps `TURING_EVENT_TYPE_AGENT_RUN_FAILED` → `'agent.run.failed'`. Nothing upstream is missing.
- **The payload is** `{runId, code, message, retryable}` (`repository/jobs.go:136`, `runs.go`). `code` is machine-readable (`retries_exhausted`, `job_timeout`, `model_provider_unavailable`); `message` is human-ish (`"Job timed out"`) but **may be empty**.
- **Two display patterns already exist**, and choosing between them is Task 1:
  - `_RunNoticeEntry` — a `_ChatEntry` appended into the message list, ordered inline with text and tool cards (`chat_screen.dart:749`, applied at `:255-265`).
  - `_SessionNotice` — a screen-level persistent banner, used for `_streamEndedNotice` and `_historyFailedNotice` (`:477-478`).
- **`_asString` exists and must be used** (`:148`). Payloads cross the wire as a protobuf `Struct`, so a producer bug can put any type behind a key; a raw `as String` throws out of the listener and kills the whole subscription.
- **`_isHistoricalRunEvent`** (`:271`) suppresses events at or below the replay watermark.

---

## Task 1: Decide how a failure renders, then render it

**Files:** `turing-client/turing_app/lib/features/chat/chat_screen.dart`

**The decision:** do **not** reuse `_RunNoticeEntry`. A failure rendered identically to "Retrying (attempt 2 of 3)" reads as routine progress. Add a distinct `_RunFailureEntry` to the sealed `_ChatEntry` hierarchy (`:675`) with its own card, visually distinguishable as an error.

Use the **list-entry** pattern, not the screen banner: a failure belongs at the point in the transcript where it happened, below any partially-streamed text and tool cards, so the user can see how far the run got. `_streamEndedNotice` stays what it is — a *connection* problem, not a run outcome — and must not be conflated with this.

- [ ] **Step 1: Write the failing tests.**
  - an `agent.run.failed` event renders a failure card;
  - it renders **below** partially-streamed assistant text from the same run (send deltas, then fail);
  - a payload with a non-string `message` (e.g. a number) does not throw and does not kill the stream — the subscription still processes a later event;
  - an empty or missing `message` still renders something meaningful rather than a blank card.
- [ ] **Step 2: Run, confirm failure.**
- [ ] **Step 3: Implement.** Mirror `_applyRunStep` (`:255-265`): guard with `_isHistoricalRunEvent`, read via `_asString`, build the entry, `setState` append, `_scrollToBottom`.

  **Wording:** lead with `message` when present; fall back to something derived from `code` when it is not; never render a bare code like `tool_discovery_failed` as the whole message. The user should learn *what happened*, not read an enum.
- [ ] **Step 4: Run, confirm pass. Prove it discriminates** — remove the switch case and confirm the tests fail.
- [ ] **Step 5: Commit.**

## Task 2: Resolve the double-report on retry exhaustion

**This is the part most likely to be missed.** Retry exhaustion emits **both** events in the same `RetryDecision.Events` (`repository/jobs.go:128` and `:141`): a `agent.run.step` carrying "Gave up after 3 attempts", **and** the `agent.run.failed` this plan starts rendering. After Task 1, one failure produces two things on screen.

- [ ] **Step 1:** Decide, and write the test that pins the decision:
  - **(a) Show both, worded so they do not repeat each other.** The notice says we stopped retrying; the card says the run failed and why. Simplest, no new state, but reads redundantly.
  - **(b) Suppress the failure card when a give-up notice already rendered for that `runId`.** Cleaner on screen; costs a small amount of per-run state in the widget.

  Either is defensible. **(a) is recommended** — (b) introduces state that has to stay correct across replay and reconnect, for a cosmetic gain.
- [ ] **Step 2:** Add a test that drives *both* events for one run and asserts exactly what the user sees. Without it, a later change to either producer silently regresses this.
- [ ] **Step 3: Commit.**

> Out of scope: changing the backend to stop emitting the give-up notice. That is a separate decision and not a Flutter change.

## Task 3: Say what is still not covered

- [ ] Add a short comment near the new case recording that `agent.run.started`, `.queued` and `.completed` remain unhandled **deliberately** — a completed run is evidenced by its own answer, and start/queue would add noise. This stops the next reader assuming it was an oversight, which is exactly what happened with `agent.run.failed`.

## Task 4: Documentation

- [ ] `docs/VISION.md`: the "Known gaps" paragraph states *"the client ignores `agent.run.failed` entirely, so run failures are silent unless a notice covers them."* Update it to reflect what is now rendered, and keep whatever remains true.
- [ ] Do not claim failures survive a session reopen — they do not; see below.

---

## Known limits (state them, do not fix them here)

- **Failures are suppressed on reopen.** `_isHistoricalRunEvent` filters anything at or below the replay watermark, so reopening a session will not show a past failure. This is consistent with tool cards and run notices, and `docs/VISION.md` already records it as a known limit. Changing replay semantics affects all three and is a separate piece of work — **do not change it here.**
- Consequence worth knowing: a user who reopens a session containing a failed run still sees an unexplained empty turn. That is a real gap; it is just not this one.

## Verification

```bash
( cd turing-client/turing_app && flutter test )
( cd turing-client/turing_app && flutter analyze )
```

The Go matrix is unaffected — this change touches no Go and no `.proto` — but run it anyway before opening the PR if anything else drifted:

```bash
go test -tags sqlite_fts5 ./... -count=1
```

Plus CLAUDE.md's required pre-push subagent review, covering unit-test coverage explicitly.

## Merge-skew warning

Two other pieces of work touch nearby files:

- `2026-08-14-session-deletion.md` — adds a delete action to the **session list**, and updates the same "Known gaps" paragraph in `docs/VISION.md`.
- `2026-08-13-flutter-session-search.md` — still unimplemented, also touches the **session list**.

This plan stays inside `chat_screen.dart`'s event handling and its tests, so the only likely collision is the VISION.md paragraph. Keep that edit surgical.

## Self-review checklist

- No backend, proto, or codegen change ✓
- Failures render distinctly from routine notices, not as another `_RunNoticeEntry` ✓
- Ordered inline so the user sees how far the run got ✓
- `_asString` used; a malformed payload cannot kill the subscription ✓
- The double-report on retry exhaustion is decided deliberately and pinned by a test ✓
- The deliberately-unhandled event types are documented so the next reader does not repeat this bug ✓
