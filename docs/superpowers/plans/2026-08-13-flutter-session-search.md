# Flutter Session Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> `superpowers:subagent-driven-development` (recommended) or
> `superpowers:executing-plans` to implement this plan task-by-task. Follow
> test-driven development for every behavior change.

**Goal:** Let users search their conversation history from the Flutter client
and open the matching session.

**Architecture:** Add search and point-session lookup methods to the Flutter
API abstraction, preserve each result's `session_id` in a focused `SearchHit`
model, and add a route-local search screen opened from the session list. Search
hits render immediately, grouped by session and sorted by the newest matching
message. Session titles enrich those groups asynchronously without blocking
results.

**Tech Stack:** Flutter/Dart under `turing-client/turing_app`. No backend,
proto, generated-code, or chat-feature changes.

---

## Existing backend behavior

- `SessionService.SearchMessages` accepts
  `SearchMessagesRequest{query, session_id, limit}` and returns standard proto
  `Message` values, including `session_id`.
- Search spans all sessions when `session_id` is empty.
- The repository treats the entire query as an exact FTS5 phrase, orders
  results by BM25 relevance rather than time, returns no rows for punctuation-
  only input, and falls back to 20 when `limit` is outside `1..100`.
- `SessionService.GetSession` is a reliable point lookup for session titles.
  `ListSessions` is not a complete substitute because its cursor is ignored and
  its result set can omit older sessions.
- Generated Dart clients already expose both RPCs.

## Design decisions

1. **Search remains client-only.** Do not change the backend, proto, or
   generated code.
2. **Use a focused result model.** Add
   `SearchHit{required sessionId, required Message message}` and a
   search-specific mapper. Do not add an optional session ID to every chat
   `Message`.
3. **Search all sessions.** Send an empty `session_id` and a fixed limit of 50.
4. **Preserve phrase semantics.** Trim only outer whitespace. Do not split
   terms, collapse internal whitespace, interpret FTS operators, or duplicate
   the backend's Unicode token predicate.
5. **Keep navigation route-local.** `SessionListScreen` opens `SearchScreen`
   and passes its existing `_openChat` callback. It remains the sole owner of
   event-source creation and chat navigation.
6. **Render before title lookup.** Group hits by session immediately. Show a
   cached title when known, `Untitled chat` only after a successful lookup
   returns an empty title, and `Session <id>` while unresolved or after lookup
   failure.
7. **Bound title enrichment.** Cache titles for the search screen's lifetime,
   call `getSession` once per unresolved distinct ID, and use at most four
   concurrent lookups. Title failures never replace valid hits with a global
   error.
8. **Sort explicitly.** Sort messages within each group newest-first and sort
   groups by their newest matching message, also newest-first. Never rely on
   the backend's relevance order for display order.
9. **Guard every async path.** Debounce changed input by 350 ms. Use a
   monotonically increasing generation for search and title resolution so
   stale successes, failures, and loading completions cannot alter a newer
   query. Clearing the field invalidates in-flight work.
10. **Make failure recoverable.** Search failures show an announced error and
    Retry action. Retry reruns the current query and unresolved title lookups.
11. **Use absolute local dates.** Format result dates with Flutter's material
    localizations; do not add `intl`.
12. **Keep search accessible.** Label the field, provide search tooltips,
    support keyboard submission, expose meaningful group/row semantics, and
    announce loading/error state changes.

## Files

- Create `lib/models/search_hit.dart`.
- Modify `lib/models/grpc_mappers.dart`.
- Modify `lib/networking/api_client.dart`.
- Modify `lib/networking/grpc_client.dart`.
- Create `lib/features/search/search_screen.dart`.
- Modify `lib/features/sessions/session_list_screen.dart`.
- Create `test/features/search/search_screen_test.dart`.
- Create or modify session-list navigation tests.
- Modify `test/networking/grpc_client_test.dart`.
- Update every hand-written `TuringApi` fake in existing tests.

Do not modify `lib/features/chat/**`.

---

## Task 1: Search and session API methods

- [ ] Add failing mapper and in-process gRPC tests that prove:
  - `searchMessages` forwards its query unchanged, an empty session ID, and
    limit 50;
  - the RPC has the existing bounded unary deadline;
  - every proto message field used by `Message` is mapped;
  - `session_id` is preserved in `SearchHit`;
  - `getSession` forwards the ID, has a bounded deadline, and maps the response.
- [ ] Run the targeted tests and confirm they fail for the missing methods.
- [ ] Add `SearchHit`, its mapper, `TuringApi.searchMessages`,
  `TuringApi.getSession`, and both `TuringGrpcApi` implementations.
- [ ] Update all existing hand-written `TuringApi` fakes.
- [ ] Run the targeted tests and confirm they pass.

## Task 2: Search screen state and rendering

- [ ] Add failing widget tests for:
  - initial guidance and exact-phrase hint;
  - a blank query issuing no request;
  - 350 ms debouncing;
  - outer whitespace trimming without internal whitespace changes;
  - loading, grouped results, newest-first group/hit ordering, roles, content,
    and absolute dates;
  - punctuation-only input reaching the API and using the normal empty state;
  - zero results suggesting a shorter exact phrase;
  - search failure, Retry, and successful recovery;
  - clearing input invalidating in-flight work;
  - stale success and stale error responses being ignored;
  - immediate hit rendering while title lookup is pending;
  - one title lookup per distinct session;
  - successful title replacement, empty-title fallback, failed-title fallback,
    bounded lookup concurrency, and stale title responses being ignored;
  - tapping a hit calling `onOpenSession` with the exact session ID;
  - keyboard submission and essential semantics.
- [ ] Run the targeted widget tests and confirm they fail.
- [ ] Implement `SearchScreen` as a constructor-injected `StatefulWidget` using
  existing Flutter state patterns. Dispose its controller, focus node, and
  debounce timer.
- [ ] Run the targeted widget tests and confirm they pass.

## Task 3: Reachability and navigation

- [ ] Add failing tests that prove search is reachable from:
  - standalone `SessionListScreen` through its app-bar action;
  - embedded `SessionListScreen` through a compact “Sessions” header action;
  - the desktop `ResponsiveShell`.
- [ ] Assert that choosing a result uses the session list's existing chat
  callback/event-source path.
- [ ] Implement `_openSearch` in `SessionListScreen`, passing `_openChat` to the
  search screen. Do not add a shell destination or duplicate chat navigation.
- [ ] Run the targeted tests and then the full Flutter unit-test suite.

## Task 4: Validation and documentation

- [ ] Run `flutter analyze`.
- [ ] Run the repository's required verification matrix.
- [ ] Update relevant client documentation to describe exact-phrase search,
  where it is reached, and the title fallback behavior.
- [ ] Treat live Docker/macOS smoke testing and screenshots as optional
  maintainer validation, not a completion gate; those require local secrets,
  Docker/Ollama, and GUI access.

---

## Review loop

After implementation and unit-test validation:

1. Dispatch independent Opus 5 and GPT-5.6 Luna reviewers over the full diff.
   Each must assess correctness, edge cases, intent gaps, simplification, and
   missing unit tests.
2. Implement all valid feedback and document the reason for any rejected item.
3. Rerun affected tests.
4. Repeat both reviews until both reviewers report no remaining feedback.
5. Run the repository-required Opus 4.8 pre-push review, final verification,
   push the feature branch, and open a PR to `main`.

## Self-review checklist

- Search results retain `session_id`.
- Exact-phrase behavior is explained without client-side query rewriting.
- Empty input performs no RPC.
- Search and title responses cannot update stale queries.
- Search limit is within `1..100`; unary RPCs have deadlines.
- Results render before metadata enrichment.
- Title lookup is cached, deduplicated, bounded, and non-fatal.
- Display ordering is explicit and time-based.
- Errors are recoverable and accessible.
- Both embedded and standalone session lists expose search.
- Search opens the correct session through the existing navigation owner.
- No backend, proto, generated-code, or chat-feature changes.
