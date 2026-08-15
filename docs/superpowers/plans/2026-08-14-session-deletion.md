# Session Deletion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** Let a user delete a session and everything it produced. This is deferral #1 in `docs/VISION.md` and the outstanding failure of commitment #1 — a system that remembers across sessions and cannot forget is not keeping its first promise.

**Scope, decided 2026-08-14:** whole-session deletion only (not individual messages), and audit rows **survive with their content scrubbed**.

**Status: implemented 2026-08-15.** One correction found during implementation: this plan says the audit scrub must happen *before* the cascade. Mutation testing showed that is not the constraint — `audit_logs` has no foreign key, so nothing cascades into it and the `UPDATE` works either side of the `DELETE`. What is genuinely order-sensitive is **capturing the run ids**, since `SELECT id FROM agent_runs WHERE session_id = ?` returns nothing once the cascade has run.

**Tech Stack:** Go 1.23, `orchestrator-go`, proto change + regeneration, Flutter client affordance.

---

## What the schema already does for us

Verified, not assumed:

- **Foreign keys are genuinely enforced.** `_foreign_keys=on` is in the DSN (`db/connection.go:19,24`), so the `ON DELETE CASCADE` declarations are real, not decorative. `sessions` → `messages`, `agent_runs`; `agent_runs` → `jobs`, `events`, `tool_calls`, `approvals` (`0001_initial.sql:23,36,55,68,84,85,109,129`).
- **FTS stays consistent for free — but only on a real DELETE.** `messages_fts_ad` is an `AFTER DELETE` trigger that removes the row from the index (`0003_messages_fts.sql:15-17`).

## The two traps

**1. Soft delete would leave deleted content recallable.** A `deleted_at` flag never fires `messages_fts_ad`, so `SearchMessages` — and therefore cross-session recall — would keep surfacing content the user deleted. The system would "remember" something you erased, which is the worst version of the confabulation problem the recall notices (#33) exist to prevent. **Use a real `DELETE`.**

**2. Scrubbing audit AFTER the cascade is impossible.** `audit_logs` has **no foreign keys** (`0001_initial.sql:146-155`) — no `session_id`, no `run_id`. Its only link to a session is `correlation_id`, which every call site sets to the **run id** (`runtime/service.go:1601,1689,1765`; `approvals/service.go:125,292,332`). That link resolves only through `agent_runs.session_id`, and the cascade deletes `agent_runs`. So the scrub **must happen before the delete, inside the same transaction**, or the rows become permanently unscrubbable orphans.

The scrub is not cosmetic: `toolAuditPayload` puts `args` (tool arguments — paths, content) and `resultSummary` into `payload_json` (`runtime/service.go:1864-1882`).

## Design decisions (locked)

1. **Hard delete, one transaction.** Scrub audit, then delete the session; either both happen or neither does.
2. **Audit keeps its shape, loses its content.** `action`, `actor_type`, `actor_id`, `correlation_id`, `target`, `created_at` survive. `payload_json` is replaced with a tombstone (e.g. `{"scrubbed":true}`) rather than nulled, so a reader can tell scrubbed-by-deletion from never-had-a-payload.
3. **Deletion is not silent.** It is a user action with consequences; it emits an audit row of its own (`session.deleted`), which is itself not scrubbed.
4. **An in-flight run must not be orphaned.** See Task 2 — decide and enforce, do not leave it to chance.

---

## Task 1: `DeleteSession` in the repository

**Files:** `turing-backend/orchestrator-go/internal/repository/sessions.go` (or a new `session_delete.go`); tests alongside.

- [x] **Step 1: Write the failing tests.**
  - a session with messages, runs, jobs, events, tool calls and approvals is fully removed — assert each table has zero rows for it;
  - its messages are gone from **FTS**: `SearchMessages` for a distinctive term in the deleted session returns nothing (this is the test that catches a soft-delete regression);
  - audit rows for the session's runs **survive**, with `payload_json` replaced and every other column intact;
  - audit rows belonging to **other** sessions are untouched (the scrub must be correlated, not global);
  - deleting an unknown session id returns a not-found error, not a silent success.

- [x] **Step 2: Run, confirm failure.**

- [x] **Step 3: Implement.** In one transaction, in this order:
  1. `SELECT id FROM agent_runs WHERE session_id = ?` — capture the run ids **first**; after step 3 they are gone.
  2. `UPDATE audit_logs SET payload_json = ? WHERE correlation_id IN (<those run ids>)`.
  3. `DELETE FROM sessions WHERE id = ?` and let the cascade do the rest.
  4. Append the `session.deleted` audit row.

  Do not hand-delete the child tables — that duplicates the FK graph in Go and will drift. If a table turns out **not** to cascade, fix the schema rather than the Go.

- [x] **Step 4: Run, confirm pass. Prove it discriminates** — swap the real `DELETE` for a `deleted_at` flag and confirm the FTS test fails.

- [x] **Step 5: Commit.**

## Task 2: Refuse to delete a session with a live run

Deleting rows out from under a worker mid-execution means the runtime finishes a run whose rows no longer exist, and reconciliation then operates on nothing. The cheapest correct answer is to refuse.

- [x] **Step 1: Write the failing test** — deleting a session whose run is `running`/`waiting_approval` returns `FailedPrecondition` and deletes nothing.
- [x] **Step 2: Run, confirm failure.**
- [x] **Step 3: Implement** the guard in the same transaction as Task 1, before any mutation.
- [x] **Step 4: Run, confirm pass; assert nothing was deleted on the refusal path.**
- [x] **Step 5: Commit.**

> `CancelRun`/`CancelRunWithEvent` already exist (`runs.go:299,357`). Cancel-then-delete is a reasonable follow-up, but it is a **second** decision (what does the user see when their delete cancels work?) and should not be smuggled in here. Refuse first; make it convenient later.

## Task 3: Expose it over gRPC

**Files:** `proto/turing/v1/sessions.proto`, orchestrator `sessions` service, regenerated `gen/` + `turing-client/turing_app/lib/generated/`.

- [x] **Step 1:** Add `rpc DeleteSession(DeleteSessionRequest) returns (DeleteSessionResponse);` to `SessionService` (`sessions.proto:86-94`).
- [x] **Step 2:** Regenerate with the pinned toolchain and **commit the generated output** — `tools/proto/generate.sh`, then `tools/proto/check.sh` must pass. CI compares bytes.
- [x] **Step 3:** Implement the service method: not-found → `NotFound`, live run → `FailedPrecondition`, empty id → `InvalidArgument`.
- [x] **Step 4:** Service-level test asserting the status codes, not just the happy path.
- [x] **Step 5: Commit.**

## Task 4: Client affordance

**Files:** `turing-client/turing_app/lib/features/sessions/`, `networking/`.

- [x] **Step 1:** Add `deleteSession` to the API client and a delete action in the session list.
- [x] **Step 2:** Confirm before deleting, and say what goes: the conversation and everything it produced, permanently, with no undo.
- [x] **Step 3:** Handle `FailedPrecondition` with a real message ("this session has a run in progress"), not a generic failure.
- [x] **Step 4:** Widget tests for confirm-then-delete, cancel-does-nothing, and the in-progress refusal.
- [x] **Step 5: Commit.**

> **Merge-skew:** `2026-08-13-flutter-session-search.md` is still unimplemented and also touches the session list. Whoever lands second should expect a conflict there.

## Task 5: Update the documentation this closes

- [x] `docs/VISION.md`: remove session deletion from "Known gaps" and from deferral #1; note in "How we would know this is working" that commitment #1's deletion check now has an implementation. *(All three done — the third was initially missed and caught in review; the falsifiability bullet also records that sandbox files outlive a deleted session.)*
- [x] Do **not** claim message-level deletion or "forget this fact" — both remain out of scope.

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

`tools/proto/check.sh` matters here — this is the first proto change in a while, and CI compares generated bytes.

Plus CLAUDE.md's required pre-push subagent review, covering unit-test coverage explicitly.

## Self-review checklist

- Real `DELETE`, so `messages_fts_ad` fires and recall cannot surface deleted content ✓
- Audit scrubbed **before** the cascade, while `correlation_id` can still be resolved to a session ✓
- Audit keeps its shape; only `payload_json` is replaced, with a tombstone rather than NULL ✓
- Other sessions' audit rows untouched ✓
- Cascade does the deleting; the FK graph is not duplicated in Go ✓
- A live run refuses the delete and mutates nothing ✓
- Proto regenerated with the pinned toolchain and committed ✓
- The client says plainly that deletion is permanent ✓
