# Run Visibility Notices Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop the client going silent during things that genuinely happen — a run being retried (three separate paths), retries being exhausted, and the agent answering from recalled context. All are invisible today.

**Architecture:** Emit `AGENT_RUN_STEP` events carrying a `note` payload. The Flutter client already renders that event type generically as a `RunNoticeCard`, so **no proto change and no regeneration are required**. One small client change *is* required — see "Client coupling" below.

**Tech Stack:** Go 1.23, `orchestrator-go` (requeue paths) and `agent-runtime-go` (recall).

**Revision note (post-design-review):** this plan was revised after a code-grounded review found four wrong claims in v1 — the "no client work" claim, an attempt off-by-one, a best-effort decision the code contradicts, and a missing requeue path that matters more than the one that was in scope. Corrections are inline.

---

## Why this, and why now

**Requeue.** `RequeueOrFailRetryableRun` returns `RetryDecision{Requeued: true}` with no events (`jobs.go:103`), and `reconcileAssignment` returns `AssignmentReconciliation{Requeued: true}` with none either (`assignments.go:233-239`). Before #30 the first did not matter, because nothing was ever requeued. Now it is. The client sees nothing between the user pressing send and the first `message.delta` — indistinguishable from the permanent hang #24 and #30 were written to eliminate. We would have replaced a silent hang with a silent retry.

**Recall.** `general_assistant.go:169-173` prepends the recalled block to the request and emits nothing. So the agent can answer using something the user said weeks ago in a different session, and the UI gives no hint why it knew. That is a trust problem, not a cosmetic one: the memory research was explicit that unattributed recall is what makes recalled material read as confabulation. The block already carries dates and an "excerpts from EARLIER conversations" header for the *model*; the user gets none of it.

**Accuracy note:** it is not true that "the client sees `agent.run.started`, then nothing." The client's switch (`chat_screen.dart:220-248`) has no case for `agent.run.started`, `queued`, `completed`, `failed`, or `message.started`, and there is no spinner anywhere on the screen. The user sees *nothing at all* until the first delta. That strengthens the motivation, and it has a consequence — see Task 1c.

## The event contract (frozen — do not invent a new type)

Reuse `TURING_EVENT_TYPE_AGENT_RUN_STEP` with payload key **`note` (string)**.

Verified, not assumed: the type exists (`events.proto:17`), is accepted from the runtime (`service.go:1230`), maps both ways to `agent.run.step` (`grpc_mappers.dart:117-118`), and `_applyRunStep` (`chat_screen.dart:251-261`) reads **only** `note`, rendering `RunNoticeCard(note:)`. The existing tool-iteration-cap notice already uses this exact shape (`general_assistant.go:317-320`).

Extra payload keys are ignored by the client — the cap notice already ships a dead `maxToolIterations`. Add them where they help an operator reading the event log, but **`note` must always carry the whole human-readable meaning on its own.**

**Do not add a new `TuringEventType`.** It would mean a proto change, Go and Dart regeneration, a `grpc_mappers` case, and a client switch case — for something the existing type already renders.

> **If a future client ever reads the numeric extras:** payloads cross the wire as a protobuf `Struct`, so `attempt`, `maxAttempts` and friends arrive in Dart as **`double`, never `int`**. `_asString` (`chat_screen.dart:144`) has no numeric sibling; an `as int` in the listener will throw. Nothing in this plan depends on that, because every number is baked into the `note` string.

## Client coupling (v1 got this wrong)

`_runStepFallbackNotice` (`chat_screen.dart:112-113`) is the string shown when a run-step arrives with a missing or empty `note`:

> "This run stopped after reaching its tool iteration limit."

That is only correct while the cap is the *sole* producer of `agent.run.step`. This plan adds four more producers. A note-less event is reachable — `messageEvent` swallows marshalling failure and sends a nil payload (`general_assistant.go:679-682`), and `appendRunEventTx` substitutes `"{}"` for an empty payload (`runs.go:552-554`) — so a retrying run could tell the user it hit the tool-iteration limit. That is a flatly wrong and alarming message.

The string is pinned by `test/features/chat_screen_test.dart:131-161`, which asserts it twice. Generalising it is therefore a deliberate, tested client change, and it is **Task 0**.

## Design decisions (locked)

1. **A notice introduces no new failure mode; existing error handling stands.** *(v1 said "emitting is best-effort — log and carry on." That contradicts both code paths: the cap notice returns the emit error and aborts (`general_assistant.go:317-322`), and in the requeue path the "emit" is an `INSERT` inside the transaction, where every other error aborts. Swallowing an emit error in the runtime means streaming an answer into a broken stream; swallowing the INSERT error means committing a requeue whose notice silently vanished. Consistency with the surrounding code wins over a special case for notices.)*
2. **Say what happened in plain language.** "Retrying (attempt 2 of 3)" is useful; "requeued" is jargon the user cannot act on.
3. **Recall notices state that material came from earlier conversations** — not the content, and not a count. The excerpts already go to the model; repeating them on screen would double the context and duplicate what the answer says.
4. **No notice when nothing happened.** Recall that returns no block emits nothing. This is the common case and must stay silent.

## Known limits (state these; do not claim otherwise)

- **Notices do not survive a session reopen.** `_isHistoricalRunEvent` (`chat_screen.dart:267-272`) suppresses any `agent.run.step` at or below the replay watermark loaded at screen open, and `TuringEventSource` never reconnects (`:122-124`). So "the client stops going silent" is true for a client that was already watching, not for one that reopens mid-retry. This is consistent with the deliberate tool-card decision.
- **The recall notice can render below the answer** if the screen is opened mid-run: `_loadInitialMessages` *prepends* history to the adopted assistant bubble (`:187-205`) while notices are *appended* (`:259`). On a live send the ordering is correct.

---

## Task 0: Make the run-step fallback producer-neutral

**Files:** `turing-client/turing_app/lib/features/chat/chat_screen.dart`, `test/features/chat_screen_test.dart`

- [ ] **Step 1:** Change `_runStepFallbackNotice` to a string true for any producer, e.g. *"The agent reported a step it could not describe."*
- [ ] **Step 2:** Update the two assertions at `chat_screen_test.dart:131-161`. Keep them asserting the fallback *behaviour* (empty/missing note → fallback), just with the new string.
- [ ] **Step 3:** `( cd turing-client/turing_app && flutter test )`.

> **Merge-skew warning:** Copilot is concurrently implementing `2026-08-13-flutter-session-search.md`, which also touches `chat_screen.dart`. This change is one constant plus its test — keep it surgical so the conflict stays trivial.

## Task 1: Notice when a run is requeued after a retryable failure

**Files:** `turing-backend/orchestrator-go/internal/repository/jobs.go`; tests in `retry_requeue_test.go` and alongside `retryable_failure_test.go`.

- [ ] **Step 1: Convert the existing test, don't be surprised by it.** `retry_requeue_test.go:44-46` currently asserts `len(decision.Events) != 0` → *"requeue emitted %d events, want none"*. That assertion is now wrong by design: change it to assert exactly one `agent.run.step` whose `note` is the expected string. Add a service-level test next to `retryable_failure_test.go` asserting `handleRunFailed` **publishes** it — the repository test can only observe `decision.Events`, and the point is that a client sees it.

- [ ] **Step 2: Run, confirm failure.**

- [ ] **Step 3: Implement.** Mechanical details, all verified:
  - Widen the `SELECT` at `jobs.go:86` to `SELECT status, session_id, trace_id FROM agent_runs WHERE id = ?`. `appendRunEventTx` needs both (`runs.go:551`) and the requeue branch has neither today. Do **not** add a second query — `failRunWithEventTx` fetches them separately at `runs.go:242` and copying that inside the same tx is a duplicate round-trip.
  - Take a single `now()` at the top of the branch for `createdAt`, matching `runs.go:240`.
  - Build the event with `appendRunEventTx` inside the tx, and return it in `RetryDecision{Requeued: true, Events: events}`. The event is created in the tx but returned after `tx.Commit()` (`jobs.go:100-103`), so there is no phantom-event-on-rollback concern.
  - **`service.go` needs no change** — `handleRunFailed` already loops `decision.Events` unconditionally before checking `decision.Requeued` (`service.go:1446-1451`). v1's "if the publish path needs it" resolves to "it doesn't".

  **The attempt number is off by one if you use it raw.** `attempt` is read from the *in-progress* job (`jobs.go:89-90`) and the column defaults to 1 (`0001_initial.sql:71`); `requeueRunForRetryTx` increments it afterwards (`jobs.go:138`), which `retry_requeue_test.go:61-62` pins as `attempt == 2`. So at this branch `attempt` is the attempt that just **failed**. Render `attempt+1`:

  ```go
  map[string]any{
      "note":        fmt.Sprintf("Retrying (attempt %d of %d)", attempt+1, maxAttempts),
      "attempt":     attempt + 1,
      "maxAttempts": maxAttempts,
      "reason":      code, // e.g. worker_busy
  }
  ```
  Assert the exact rendered string in the test so the off-by-one cannot silently return.

- [ ] **Step 4: Run, confirm pass. Prove it discriminates** — drop the event from the requeue path and confirm the test fails.

- [ ] **Step 5: Commit.**

## Task 1b: Notice when an assignment is requeued by reconciliation

**This is the path users actually hit.** `worker_busy` (Task 1) is a reconnect race the orchestrator's own capacity gate mostly prevents. `reconcileAssignment` (`assignments.go:233-239`) requeues on **worker disconnect, lease expiry, and startup recovery** — reached via `ReconcileAssignmentWithLimit` / `RecoverAssignmentWithLimit` / `RecoverAssignmentAtCutoffWithLimit` / `RecoverStaleAssignmentsWithLimit` (`assignments.go:93-118, 723-739`), wired at `service.go:576, 743, 812` and `app.go:64`. Shipping a notice only on the un-triggerable path would largely defeat the goal.

**Files:** `turing-backend/orchestrator-go/internal/repository/assignments.go`; tests alongside the existing reconciliation tests.

- [ ] **Step 1: Write the failing test** — a reconciliation that requeues produces one `agent.run.step` event whose `note` says the run is being retried after losing its worker.
- [ ] **Step 2: Run, confirm failure.**
- [ ] **Step 3: Implement.** `AssignmentReconciliation` already has `Events []Event` (`assignments.go:16`), so the mechanism is identical to Task 1. Verify **every** caller publishes `Events` — if any drops them on the floor, the notice never reaches the client and the test above must be written at that caller instead.
- [ ] **Step 4: Run, confirm pass; prove it discriminates.**
- [ ] **Step 5: Commit.**

Wording should not promise a specific attempt number unless it is as reliably available here as in Task 1 — check before using one. Prefer honest and vague over precise and wrong.

## Task 1c: Notice when retries are exhausted

Without this, Task 1 makes things **worse**: the user sees "Retrying (attempt 2 of 3)", "Retrying (attempt 3 of 3)", then permanent silence — more confusing than silence throughout, because the client has no `agent.run.failed` case at all (`chat_screen.dart:220-248`).

**Files:** `turing-backend/orchestrator-go/internal/repository/jobs.go` (the exhaustion branch, `jobs.go:106-123`).

- [ ] **Step 1: Write the failing test** — once attempts are exhausted, the decision carries an `agent.run.step` notice saying the run gave up, alongside the existing terminal events.
- [ ] **Step 2: Run, confirm failure.**
- [ ] **Step 3: Implement.** One more `appendRunEventTx` in a tx that already fetches `sessionID`/`traceID` for `failRunWithEventTx`. Order it before the terminal events.
- [ ] **Step 4: Run, confirm pass; prove it discriminates.**
- [ ] **Step 5: Commit.**

**Follow-up worth filing, not doing here:** the client ignoring `agent.run.failed` entirely means *every* run failure is silent, not just this one. Fixing that properly is a client change that overlaps Copilot's current work; note it in the PR.

## Task 2: Notice when recall contributes context

**Files:** `turing-backend/agent-runtime-go/internal/agent/general_assistant.go` (the `a.recall.Recall` call site, `:169-173`); test in `general_assistant_test.go`.

- [ ] **Step 1: Write the failing tests.**
  - recall returns a block → an `AGENT_RUN_STEP` whose `note` says material was recalled from earlier conversations;
  - recall returns nothing → **no** such event (the common case must stay silent);
  - no recaller configured → no such event.

- [ ] **Step 2: Run, confirm failure.**

- [ ] **Step 3: Implement.** Mirror the cap notice (`general_assistant.go:317`) using `messageEvent(job, ..., map[string]any{...})` (`:678-684`). Emit *after* recall returns a block and *before* the model request.

  **Do not widen `Recall` to expose a count.** v1 suggested this; the cost is the `ContextRecaller` interface (`:31-33`), the concrete method (`recall.go:142`), `fakeRecaller` (`general_assistant_test.go:3114-3128`) and ~10 call sites in `memory/recall_test.go` — real churn for a number no client reads, and "excerpt(s)" is jargon by decision 2 anyway. Per decision 3 the note carries the meaning without it:

  ```go
  map[string]any{
      "note": "Answered using material recalled from earlier conversations",
  }
  ```
  If a count is later genuinely wanted, return one struct (`memory.Recalled{Message, Excerpts, OK}`) rather than a third positional return, so the next addition doesn't churn 13 call sites again.

- [ ] **Step 4: Run, confirm pass**, including that the no-block case stays silent.

- [ ] **Step 5: Commit.**

## Task 3: See it in the real client

- [ ] **Step 1:** Bring the stack up (`cd turing-backend && ./scripts/compose.sh up -d --build`) with Ollama running, and run the Flutter client.
- [ ] **Step 2 (recall):** In one session state a distinctive fact; in a **new** session ask about it. The answer should reference it *and* a notice should appear. Screenshot for the PR.
- [ ] **Step 3 (requeue):** Now genuinely stageable via Task 1b — kill the agent-runtime container mid-run and let lease recovery requeue the assignment. Screenshot the notice.
- [ ] **Step 4 (worker_busy, Task 1):** Still a race that the capacity gate mostly prevents. If you cannot trigger it, **say so in the PR** and rely on the tests. Do not imply you saw it.
- [ ] **Step 5:** Tear the stack down.

---

## Verification

```bash
go test -tags sqlite_fts5 ./... -count=1
go build -tags sqlite_fts5 ./...
( cd turing-backend/mcp-files  && go test ./... -count=1 && go build ./cmd/server )
( cd turing-backend/mcp-system && go test ./... -count=1 && go build ./... )
( cd turing-client/turing_app  && flutter test )
tools/proto/check.sh
golangci-lint cache clean
golangci-lint run --build-tags sqlite_fts5 ./... ./.github/workflows
( cd turing-backend/mcp-files  && golangci-lint run ./... )
( cd turing-backend/mcp-system && golangci-lint run ./... )
```

`golangci-lint cache clean` is not in CLAUDE.md's matrix but is required here: a stale cache has twice produced false positives citing sibling worktrees.

Plus CLAUDE.md's required pre-push subagent review, covering unit-test coverage explicitly.

## Self-review checklist

- Reuses `AGENT_RUN_STEP` + `note`; no proto change, no regeneration ✓
- The one client change (fallback string) is scoped, tested, and flagged for merge skew ✓
- `note` carries the full meaning alone; extra keys are additive only ✓
- Notices introduce no new failure mode; existing error handling stands ✓
- Silence is preserved when nothing happened ✓
- Requeue events carry the retried `run_id` so a watching client correlates them ✓
- The attempt number is `attempt+1` and the rendered string is pinned by a test ✓
- Both requeue paths are covered, and exhaustion does not end in silence ✓
- Visibility limits (replay watermark, mid-run ordering) are stated, not glossed ✓
