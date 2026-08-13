# Flutter Session Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let the user search their own conversation history. The backend has had full-text search since #15 and **not one line of Dart calls it** — the feature exists and is invisible.

**Architecture:** Add `searchMessages` to the client's `TuringApi` + gRPC implementation, and a search surface in the Flutter app that lists matches grouped by session and opens the session on tap.

**Tech Stack:** Flutter/Dart, `turing-client/turing_app`. **No backend, proto, or generated-code changes.**

---

## What already exists

- `SessionService.SearchMessages` RPC — `SearchMessagesRequest{query, session_id, limit}` → `SearchMessagesResponse{messages}`, returning the standard `Message` (`message_id`, `session_id`, `role`, `content`, `created_at`, …).
- The generated Dart client is already committed under `lib/generated/` — you do **not** need to regenerate anything.
- `TuringApi` (`lib/networking/api_client.dart`) has `listSessions`, `listMessages`, `listEvents`, `sendMessage`, `approveApproval`, `denyApproval` — and **no** `searchMessages`. That gap is the whole task.

## The constraint that shapes the UX

**Search is an exact PHRASE match.** `repository.fts5Phrase` wraps the entire query in double quotes, so `MATCH "deploy the staging cluster"` only hits messages containing that exact contiguous run of words. There is no way to inject FTS5 `OR`/`AND` — the server quotes whatever it is given.

Consequences you must design for, not paper over:
- **Multi-word queries usually return nothing** unless the user typed a phrase that literally appears. This is correct behaviour for an explicit search box, but a user typing a sentence will see "no results" and conclude search is broken.
- **Say so in the UI.** A hint like *"finds messages containing this exact phrase"* near the field, and an empty state that suggests trying fewer words, is worth more here than any amount of result styling.
- Do **not** try to fix this by splitting the query into terms and firing N requests. The `memory` package does that deliberately for *recall*, where the caller is a model and latency is invisible. For a user-facing box it would make one keystroke a burst of RPCs and change the meaning of the result set. If phrase search proves wrong for this UX, that is a backend conversation, not a client workaround.
- The repository **silently returns nothing** for a query with no alphanumeric token (`hasFTS5Token`), and a `limit` above 100 **falls back to 20** rather than clamping. Keep `limit` at or under 100.

## Design decisions (locked)

1. **Search across all sessions by default** — send an empty `session_id`. Cross-session recall is the point; scoping to the current session is a filter to offer later, not the default.
2. **Results are grouped by session, newest first**, each row showing the role, an absolute date, and the matching text. Absolute dates, not "3 days ago" — the same reason the recall block uses them: a result list is read long after the fact.
3. **Tapping a result opens that session.** Anything less makes search a dead end. Scrolling to the exact message is a bonus, not a requirement — say so in the PR if you skip it.
4. **Debounce input**; do not fire a request per keystroke. Each one hits SQLite through gRPC.
5. **A failed search must not break the screen.** Render an error state and let the user retry — mirror how `chat_screen.dart` treats a dead event stream rather than throwing.
6. **Empty query does nothing.** No request, no error, no spinner.

## File structure

- Modify: `lib/networking/api_client.dart` — add `searchMessages` to the `TuringApi` interface.
- Modify: `lib/networking/grpc_client.dart` — implement it against the generated `SessionServiceClient`, mapping to the existing `Message` model via `GrpcMappers`.
- Create: `lib/features/search/search_screen.dart` (+ any small widgets it needs).
- Modify: wherever navigation lives (`lib/ui/shell/responsive_shell.dart` and/or `lib/features/sessions/session_list_screen.dart`) to reach it.
- Create: `test/features/search/search_screen_test.dart`.
- Modify: `test/networking/grpc_client_test.dart` for the new method.

**Do not touch** `lib/features/chat/**` beyond what navigation strictly requires — backend work is landing notices that render there, and a conflict in that file is the one collision worth avoiding tonight.

---

## Task 1: Client API method

- [ ] **Step 1: Failing test** in `test/networking/grpc_client_test.dart`, mirroring how the existing methods are tested: assert `searchMessages` sends the query, passes an empty `session_id`, respects `limit`, and maps results into `Message`.
- [ ] **Step 2: Run, confirm failure.**
- [ ] **Step 3: Implement** on the interface and the gRPC client. Follow `listMessages` for the auth-metadata and mapping idiom; reuse `GrpcMappers.messageToModel` rather than hand-mapping fields.
- [ ] **Step 4: Run, confirm pass. Commit.**

## Task 2: The search screen

- [ ] **Step 1: Failing widget tests**, using the fake-`TuringApi` pattern already established in `test/features/chat_screen_test.dart` (hand-written fakes, no mockito):
  - typing a query and settling shows matching results;
  - an empty query issues **no** request;
  - a searcher that throws renders an error state and does not crash the screen;
  - zero results renders an empty state that mentions the exact-phrase behaviour;
  - results are grouped by session with an absolute date visible;
  - tapping a result navigates to that session.
- [ ] **Step 2: Run, confirm failure.**
- [ ] **Step 3: Implement.** `StatefulWidget` + `setState`, constructor-injected `TuringApi` — the house idiom; no state-management package. Debounce with a `Timer`, and cancel it in `dispose` along with any controllers.
- [ ] **Step 4: Run, confirm pass.** `flutter test && flutter analyze`. Commit.

## Task 3: Make it reachable

- [ ] **Step 1:** Add navigation — a search entry in the responsive shell's navigation, and/or a search icon on the session list. Follow the existing shell pattern rather than inventing a new navigation concept.
- [ ] **Step 2:** Update `test/ui/responsive_shell_backend_test.dart` if the navigation surface changes.
- [ ] **Step 3:** Run the full Flutter suite. Commit.

## Task 4: Use it for real

- [ ] **Step 1:** Bring the stack up (`cd turing-backend && ./scripts/compose.sh up -d --build`), run the client, hold a couple of conversations so there is something to find.
- [ ] **Step 2:** Search for a **single distinctive word** from an earlier conversation — confirm it is found and opens the session.
- [ ] **Step 3:** Search a full sentence — confirm you get the empty state, and that its wording actually helps. This is the case real users will hit first; if the copy does not help here, rewrite it.
- [ ] **Step 4:** Screenshot both for the PR. Tear the stack down.

---

## Verification

```bash
( cd turing-client/turing_app && flutter test && flutter analyze )
```
Flutter tests gate CI; `flutter analyze` does not, so run it yourself — the repo is currently clean.

Plus CLAUDE.md's required **Opus 4.8 pre-push review**, covering unit-test coverage explicitly. If you cannot dispatch a reviewer subagent, say so plainly in the PR rather than implying it happened.

## Self-review checklist

- No backend, proto, or generated-code changes ✓
- Phrase-search behaviour surfaced in the UI rather than hidden ✓
- No per-term request fan-out ✓
- `limit` ≤ 100 ✓
- Debounced; timers and controllers disposed ✓
- A failed search degrades to an error state, never a broken screen ✓
- Results open their session ✓
- Absolute dates ✓
- `lib/features/chat/**` left alone apart from navigation ✓
