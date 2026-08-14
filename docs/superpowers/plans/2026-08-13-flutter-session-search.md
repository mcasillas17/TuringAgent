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

**Files:**

- Create: `turing-client/turing_app/lib/models/search_hit.dart`
- Modify: `turing-client/turing_app/lib/models/grpc_mappers.dart`
- Modify: `turing-client/turing_app/lib/networking/api_client.dart`
- Modify: `turing-client/turing_app/lib/networking/grpc_client.dart`
- Modify: `turing-client/turing_app/test/models/grpc_mappers_test.dart`
- Modify: `turing-client/turing_app/test/networking/grpc_client_test.dart`
- Modify: `turing-client/turing_app/test/widget_test.dart`
- Modify: `turing-client/turing_app/test/features/chat_screen_test.dart`
- Modify: `turing-client/turing_app/test/ui/responsive_shell_backend_test.dart`

- [ ] **Step 1: Write the failing mapper test**

Add a proto message with a non-empty `sessionId`, map it through the
search-specific mapper, and assert both the grouping key and nested message:

```dart
final hit = GrpcMappers.searchHitToModel(
  commonpb.Message(
    messageId: 'message-1',
    sessionId: 'session-1',
    role: commonpb.MessageRole.MESSAGE_ROLE_USER,
    content: 'deploy staging',
    sequence: Int64(7),
    createdAt: timestamppb.Timestamp.fromDateTime(
      DateTime.utc(2026, 8, 13, 12),
    ),
  ),
);

expect(hit.sessionId, 'session-1');
expect(hit.message.messageId, 'message-1');
expect(hit.message.content, 'deploy staging');
expect(hit.message.sequence, 7);
expect(hit.message.createdAt, DateTime.utc(2026, 8, 13, 12));
```

- [ ] **Step 2: Write failing in-process gRPC tests**

Extend the existing capturing `SessionServiceBase` with `searchMessages` and
`getSession`. Assert:

```dart
final hits = await api.searchMessages(query: 'deploy  staging', limit: 50);
final session = await api.getSession(sessionId: 'session-1');

expect(service.searchRequest?.query, 'deploy  staging');
expect(service.searchRequest?.sessionId, '');
expect(service.searchRequest?.limit, 50);
expect(service.searchDeadline, isNotNull);
expect(hits.single.sessionId, 'session-1');
expect(service.getRequest?.sessionId, 'session-1');
expect(service.getDeadline, isNotNull);
expect(session.title, 'Release work');
```

Return a complete proto message from `searchMessages` and a `Session` from
`getSession` so mapping is exercised rather than merely request capture.

- [ ] **Step 3: Run the targeted tests and verify failure**

Run:

```bash
( cd turing-client/turing_app && \
  flutter test test/models/grpc_mappers_test.dart \
    test/networking/grpc_client_test.dart )
```

Expected: compilation fails because `SearchHit`, `searchHitToModel`,
`searchMessages`, and `getSession` do not exist.

- [ ] **Step 4: Add the focused result model**

Create:

```dart
import 'message.dart';

class SearchHit {
  const SearchHit({required this.sessionId, required this.message});

  final String sessionId;
  final Message message;
}
```

Add to `GrpcMappers`:

```dart
static model_search.SearchHit searchHitToModel(commonpb.Message message) {
  return model_search.SearchHit(
    sessionId: message.sessionId,
    message: messageToModel(message),
  );
}
```

- [ ] **Step 5: Add API signatures**

Import `search_hit.dart` and add:

```dart
Future<Session> getSession({required String sessionId});

Future<List<SearchHit>> searchMessages({
  required String query,
  int limit = 50,
});
```

- [ ] **Step 6: Implement both gRPC methods**

Use the same bounded deadline as startup list calls:

```dart
@override
Future<Session> getSession({required String sessionId}) async {
  final response = await _sessions.getSession(
    sessionpb.GetSessionRequest(sessionId: sessionId),
    options: grpc.CallOptions(timeout: _startupUnaryTimeout),
  );
  return GrpcMappers.sessionToModel(response);
}

@override
Future<List<SearchHit>> searchMessages({
  required String query,
  int limit = 50,
}) async {
  final response = await _sessions.searchMessages(
    sessionpb.SearchMessagesRequest(
      query: query,
      sessionId: '',
      limit: limit,
    ),
    options: grpc.CallOptions(timeout: _startupUnaryTimeout),
  );
  return response.messages.map(GrpcMappers.searchHitToModel).toList();
}
```

- [ ] **Step 7: Update every existing test fake**

Each fake implementing `TuringApi` must compile with explicit methods. Use
fixture-appropriate values, with this empty default where search is irrelevant:

```dart
@override
Future<Session> getSession({required String sessionId}) async {
  return Session(
    sessionId: sessionId,
    title: null,
    updatedAt: DateTime.utc(2026, 8, 13),
  );
}

@override
Future<List<SearchHit>> searchMessages({
  required String query,
  int limit = 50,
}) async {
  return const [];
}
```

- [ ] **Step 8: Run targeted tests and verify pass**

Run the Step 3 command. Expected: all mapper and gRPC tests pass.

- [ ] **Step 9: Commit**

```bash
git add turing-client/turing_app/lib/models \
  turing-client/turing_app/lib/networking \
  turing-client/turing_app/test
git commit -m "feat(flutter): expose session message search"
```

## Task 2: Search screen state and rendering

**Files:**

- Create: `turing-client/turing_app/lib/features/search/search_screen.dart`
- Create: `turing-client/turing_app/test/features/search/search_screen_test.dart`

- [ ] **Step 1: Create a controllable fake and widget harness**

The fake records queries and title lookup concurrency and exposes completers:

```dart
class _FakeSearchApi implements TuringApi {
  final queries = <String>[];
  final sessionRequests = <String>[];
  final searchResponses = <Completer<List<SearchHit>>>[];
  final sessions = <String, Future<Session>>{};
  int activeSessionRequests = 0;
  int maxActiveSessionRequests = 0;

  @override
  Future<List<SearchHit>> searchMessages({
    required String query,
    int limit = 50,
  }) {
    queries.add(query);
    final response = Completer<List<SearchHit>>();
    searchResponses.add(response);
    return response.future;
  }

  @override
  Future<Session> getSession({required String sessionId}) async {
    sessionRequests.add(sessionId);
    activeSessionRequests++;
    maxActiveSessionRequests = max(
      maxActiveSessionRequests,
      activeSessionRequests,
    );
    try {
      return await sessions[sessionId]!;
    } finally {
      activeSessionRequests--;
    }
  }

  // Implement the remaining TuringApi methods with inert test defaults.
}
```

Pump `MaterialApp(home: SearchScreen(...))` and capture opened session IDs.

- [ ] **Step 2: Add failing query lifecycle tests**

Use `tester.enterText`, `pump(const Duration(milliseconds: 349))`, and a final
1 ms pump to prove debounce timing. Assert:

```dart
expect(api.queries, isEmpty);
await tester.pump(const Duration(milliseconds: 1));
expect(api.queries, ['deploy  staging']);
```

Cover blank input, outer-only trimming, punctuation-only input, keyboard submit,
loading, empty exact-phrase guidance, error with Retry, retry recovery, clearing
input, and out-of-order success/error completions.

- [ ] **Step 3: Add failing result and title tests**

Create hits spanning multiple sessions and deliberately return them in relevance
order. Assert groups and rows use newest matching timestamps instead. Keep title
futures incomplete first and verify `Session session-1` is visible immediately.
Then complete title futures and assert named/untitled replacement, one lookup per
ID, failure fallback, stale title suppression, and
`api.maxActiveSessionRequests <= 4`.

Tap a result and assert:

```dart
expect(openedSessionIds, ['session-1']);
```

Use `tester.ensureSemantics()` to verify the field label, live error region,
group headers, row labels, and Retry action.

- [ ] **Step 4: Run the new widget test and verify failure**

Run:

```bash
( cd turing-client/turing_app && \
  flutter test test/features/search/search_screen_test.dart )
```

Expected: compilation fails because `SearchScreen` does not exist.

- [ ] **Step 5: Implement lifecycle state**

`SearchScreen` has:

```dart
const SearchScreen({
  super.key,
  required this.apiClient,
  required this.onOpenSession,
});

final TuringApi apiClient;
final Future<void> Function(String sessionId) onOpenSession;
```

Its state owns a `TextEditingController`, `FocusNode`, nullable debounce
`Timer`, generation integer, current normalized query, loading flag, error,
hits, and successful title cache. Input change increments the generation
immediately, cancels the prior timer, clears state for an empty trimmed query,
or starts a 350 ms timer. Keyboard submit cancels the timer and searches
immediately.

`_runSearch(query, generation)` sets loading state, awaits
`apiClient.searchMessages(query: query, limit: 50)`, ignores stale completion,
sorts/groups hits, renders them, and starts title resolution without awaiting
it. Retry increments the generation and calls `_runSearch` with the current
query.

- [ ] **Step 6: Implement bounded title resolution**

Build unresolved distinct IDs, share an integer queue index across at most four
worker futures, and update the title cache only when mounted and the generation
still matches:

```dart
final workerCount = min(4, sessionIds.length);
var nextIndex = 0;

Future<void> worker() async {
  while (nextIndex < sessionIds.length) {
    final sessionId = sessionIds[nextIndex++];
    try {
      final session = await widget.apiClient.getSession(sessionId: sessionId);
      if (!mounted || generation != _generation) continue;
      setState(() {
        _titles[sessionId] = session.title?.isNotEmpty == true
            ? session.title!
            : 'Untitled chat';
      });
    } catch (_) {
      // A metadata failure retains the visible ID fallback.
    }
  }
}

await Future.wait(List.generate(workerCount, (_) => worker()));
```

Do not catch the search RPC broadly; its error is stored and rendered with
Retry. The narrow title catch is intentional because metadata is non-critical
and the ID fallback is already visible.

- [ ] **Step 7: Implement accessible rendering**

Use a labeled `TextField` with search icon, exact-phrase helper text, clear
button, `TextInputAction.search`, and submit handler. Render:

- initial guidance when the normalized query is empty;
- a live-region progress state while searching;
- a live-region error plus Retry button;
- an exact-phrase empty state;
- grouped result sections otherwise.

Format each hit date with:

```dart
final localDate = hit.message.createdAt.toLocal();
final date = MaterialLocalizations.of(context).formatMediumDate(localDate);
```

Wrap group headings in `Semantics(header: true)` and give each tappable row a
label containing role, absolute date, and content.

- [ ] **Step 8: Dispose owned resources**

```dart
@override
void dispose() {
  _debounce?.cancel();
  _controller.dispose();
  _focusNode.dispose();
  super.dispose();
}
```

- [ ] **Step 9: Run widget tests and verify pass**

Run the Step 4 command. Expected: all search-screen tests pass.

- [ ] **Step 10: Commit**

```bash
git add turing-client/turing_app/lib/features/search \
  turing-client/turing_app/test/features/search
git commit -m "feat(flutter): add conversation search screen"
```

## Task 3: Reachability and navigation

**Files:**

- Modify:
  `turing-client/turing_app/lib/features/sessions/session_list_screen.dart`
- Create:
  `turing-client/turing_app/test/features/sessions/session_list_screen_test.dart`
- Modify:
  `turing-client/turing_app/test/ui/responsive_shell_backend_test.dart`

- [ ] **Step 1: Write failing reachability tests**

Pump standalone and embedded `SessionListScreen` variants. Find the
`Search conversations` tooltip, tap it, settle, and assert the search field is
visible. In the responsive-shell test, assert the embedded header contains
`Sessions` and the same search action.

Use a fake search hit, tap it, and assert the created event source receives the
matching session ID after `ChatScreen` opens.

- [ ] **Step 2: Run navigation tests and verify failure**

Run:

```bash
( cd turing-client/turing_app && \
  flutter test test/features/sessions/session_list_screen_test.dart \
    test/ui/responsive_shell_backend_test.dart )
```

Expected: the session-list test file or search action is missing.

- [ ] **Step 3: Add route-local navigation**

Import `SearchScreen` and add:

```dart
Future<void> _openSearch() async {
  await Navigator.of(context).push(
    MaterialPageRoute(
      builder: (_) => SearchScreen(
        apiClient: widget.apiClient,
        onOpenSession: _openChat,
      ),
    ),
  );
}
```

The standalone app bar gets:

```dart
IconButton(
  tooltip: 'Search conversations',
  icon: const Icon(Icons.search),
  onPressed: _openSearch,
)
```

The embedded body becomes a column with a compact `Sessions` header row and the
same tooltip/action above an expanded sessions list. Keep the existing new-chat
button and `_openChat` implementation unchanged.

- [ ] **Step 4: Run navigation tests and verify pass**

Run the Step 2 command. Expected: all session-list and responsive-shell tests
pass.

- [ ] **Step 5: Run the full Flutter unit suite**

```bash
( cd turing-client/turing_app && flutter test )
```

Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add turing-client/turing_app/lib/features/sessions \
  turing-client/turing_app/test/features/sessions \
  turing-client/turing_app/test/ui
git commit -m "feat(flutter): expose conversation search"
```

## Task 4: Validation and documentation

**Files:**

- Modify: `README.md` or the existing client documentation that describes the
  Flutter session list

- [ ] **Step 1: Run Flutter static analysis**

```bash
( cd turing-client/turing_app && flutter analyze )
```

Expected: `No issues found!`

- [ ] **Step 2: Run the repository verification skill**

Invoke `/verify`, which runs the root Go module, both MCP Go modules, Flutter
tests, proto drift check, and all configured Go linters. Expected: every matrix
entry passes.

- [ ] **Step 3: Update client documentation**

Document that Search is reached from the Sessions screen, matches exact phrases
across all sessions, opens the selected conversation, and may temporarily show
a session ID if title metadata cannot be loaded.

- [ ] **Step 4: Review and commit documentation**

```bash
git diff --check
git add README.md docs turing-client
git commit -m "docs: describe Flutter conversation search"
```

Live Docker/macOS smoke testing and screenshots are optional maintainer
validation because they require local secrets, Docker/Ollama, and GUI access.

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
