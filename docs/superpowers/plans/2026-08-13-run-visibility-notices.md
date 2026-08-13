# Run Visibility Notices Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop the client going silent during two things that now genuinely happen — a run being retried, and the agent answering from recalled context. Both are invisible today.

**Architecture:** Emit `AGENT_RUN_STEP` events with a `note`. The Flutter client already renders that event type generically as a `RunNoticeCard`, so **no proto change, no regeneration, and no client work is required** for these to appear.

**Tech Stack:** Go 1.23, `orchestrator-go` (requeue) and `agent-runtime-go` (recall).

---

## Why these two, and why now

**Requeue.** `repository.RequeueOrFailRetryableRun` returns `RetryDecision{Requeued: true}` with **no events** (`jobs.go:103`). Before #30 that did not matter, because nothing was ever requeued. Now it is: the client sees `agent.run.started`, then nothing at all until a fresh run begins. That is indistinguishable from the permanent hang #24 and #30 were written to eliminate — we would have replaced a silent hang with a silent retry.

**Recall.** `general_assistant.go:170-173` prepends the recalled block to the request and emits nothing. So the agent can answer using something the user said weeks ago in a different session, and the UI gives no hint why it knew. That is a trust problem, not a cosmetic one: the memory research was explicit that unattributed recall is what makes recalled material read as confabulation. The recall block itself already carries dates and a "these are excerpts from EARLIER conversations" header for the *model*; the user gets none of it.

## The event contract (frozen — do not invent a new type)

Reuse `TURING_EVENT_TYPE_AGENT_RUN_STEP` with payload key **`note` (string)**.

That key is not a guess: `RunNoticeCard` takes exactly `note`, `chat_screen.dart:224` maps `agent.run.step` to it, and the existing tool-iteration-cap notice already uses this shape (`general_assistant.go:317-320`). Anything else silently renders nothing.

Extra payload keys are fine and ignored by the client — the cap notice already ships `maxToolIterations`. Add them where they help a future UI or an operator reading the event log, but **`note` must always carry the whole human-readable meaning on its own.**

**Do not add a new `TuringEventType`.** It would mean a proto change, Go and Dart regeneration, a `grpc_mappers` case, and a client switch case — for something the existing type already renders. If a later design wants requeue and recall styled differently, that is a client-side change keyed off the extra payload fields, not a new event.

## Design decisions (locked)

1. **A notice must never fail the thing it describes.** Emitting is best-effort: if the emit errors, log and carry on. A run must not fail because we could not tell the user it was retrying.
2. **Say what happened in plain language, and be honest about uncertainty.** "Retrying (attempt 2 of 3)" is useful; "requeued" is jargon the user cannot act on.
3. **Recall notices state the count and that the material is from earlier conversations** — not the content. The excerpts already go to the model; repeating them in the transcript would double the context on screen and duplicate what the answer will say.
4. **No notice when nothing happened.** Recall that returns no block emits nothing. This is the common case and must stay silent.

---

## Task 1: Emit a notice when a run is requeued

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/repository/jobs.go` (`RequeueOrFailRetryableRun`)
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/service.go` if the publish path needs it
- Test: `turing-backend/orchestrator-go/internal/repository/retry_requeue_test.go`, and the service-level test alongside `retryable_failure_test.go`

- [ ] **Step 1: Write the failing test.** A retryable failure that requeues must produce an event of type `agent.run.step` whose payload `note` mentions the retry and the attempt number. Assert on the persisted/published event, not on an internal struct field — the point is that a *client* can see it.

- [ ] **Step 2: Run it, confirm it fails** — today `RetryDecision{Requeued: true}` carries no events.

- [ ] **Step 3: Implement.** Follow the existing pattern in this file: the fail path already builds an event and returns it in the decision for the service to publish (`handleRunFailed` loops `decision.Events` and calls `s.publishEvent`). Do the same for the requeue path, so both go through one publish mechanism.

Note the run stays in `queued` and the *same* `run_id` is retried, so the event must carry that `run_id` — a client correlating by run must see it on the run it is already watching.

Suggested payload:
```go
map[string]any{
    "note":       fmt.Sprintf("Retrying (attempt %d of %d)", attempt, maxAttempts),
    "attempt":    attempt,
    "maxAttempts": maxAttempts,
    "reason":     code, // e.g. worker_busy
}
```

- [ ] **Step 4: Run, confirm pass. Prove it discriminates** — remove the event from the requeue path and confirm the test fails.

- [ ] **Step 5: Commit.**

## Task 2: Emit a notice when recall contributes context

**Files:**
- Modify: `turing-backend/agent-runtime-go/internal/agent/general_assistant.go` (the `a.recall.Recall` call site, ~line 170)
- Test: `turing-backend/agent-runtime-go/internal/agent/general_assistant_test.go`

- [ ] **Step 1: Write the failing tests.**
  - a run where recall returns a block emits an `AGENT_RUN_STEP` whose `note` says material was recalled from earlier conversations;
  - a run where recall returns nothing emits **no** such event (the common case must stay silent);
  - a run with no recaller configured emits no such event.

- [ ] **Step 2: Run, confirm failure.**

- [ ] **Step 3: Implement.** Mirror the cap notice at `general_assistant.go:317`, which uses the `messageEvent(job, ..., map[string]any{...})` helper. Emit *after* recall returns a block and *before* the model request, so the ordering in the transcript matches what actually happened.

The excerpt count is not currently returned by `Recall` — it returns `(llm.ChatMessage, bool)`. Prefer widening the return or adding a small accessor over parsing the rendered block text; parsing prose you also control is the kind of coupling that breaks quietly. If widening the signature, update the `ContextRecaller` interface and the fake in the tests.

Suggested payload:
```go
map[string]any{
    "note":     fmt.Sprintf("Recalled %d excerpt(s) from earlier conversations", count),
    "excerpts": count,
}
```

- [ ] **Step 4: Run, confirm pass**, including that the no-block case stays silent.

- [ ] **Step 5: Commit.**

## Task 3: See both in the real client

- [ ] **Step 1:** Bring the stack up (`cd turing-backend && ./scripts/compose.sh up -d --build`) with Ollama running, and run the Flutter client.
- [ ] **Step 2 (recall):** In one session state a distinctive fact; in a **new** session ask about it. The answer should reference it *and* a notice should appear saying material was recalled. Screenshot for the PR.
- [ ] **Step 3 (requeue):** Harder to stage deliberately — `worker_busy` is a reconnect race (the orchestrator's own capacity gate stops it over-assigning a healthy worker). Do not contort the product to force it; if you cannot trigger it, **say so in the PR** and rely on the tests. Do not imply you saw it.
- [ ] **Step 4:** Tear the stack down.

---

## Verification

```bash
go test -tags sqlite_fts5 ./... -count=1
go build -tags sqlite_fts5 ./...
( cd turing-backend/mcp-files  && go test ./... -count=1 && go build ./cmd/server )
( cd turing-backend/mcp-system && go test ./... -count=1 && go build ./... )
( cd turing-client/turing_app  && flutter test )
golangci-lint cache clean && golangci-lint run --build-tags sqlite_fts5 ./... ./.github/workflows
( cd turing-backend/mcp-files  && golangci-lint run ./... )
( cd turing-backend/mcp-system && golangci-lint run ./... )
```

Plus CLAUDE.md's required **Opus 4.8 pre-push review**, covering unit-test coverage explicitly.

## Self-review checklist

- Reuses `AGENT_RUN_STEP` + `note`; no proto change, no regeneration, no client work ✓
- `note` carries the full meaning alone; extra keys are additive only ✓
- Emitting is best-effort and cannot fail the run it describes ✓
- Silence is preserved when nothing happened ✓
- The requeue event carries the retried `run_id` so a watching client correlates it ✓
- Excerpt count comes from a real return value, not by parsing rendered prose ✓
