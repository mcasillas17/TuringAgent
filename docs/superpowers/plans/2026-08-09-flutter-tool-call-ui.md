# Flutter Tool-Call UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render tool-call activity in the chat client — show a "🔧 running `system.time`…" indicator inline in the conversation when the agent calls a tool, resolving to a completed / failed / denied state — so the model-driven tool loop (Plan #1) is visible to the user instead of silent.

**Architecture:** The live chat screen (`lib/features/chat/chat_screen.dart`) already switches on dotted event-type strings and renders `message.delta` (per-message `ValueNotifier<String>`) and `approval.requested` (`ApprovalCard`). The generated `TuringEventType` enum already maps `TOOL_CALL_*` to the strings `tool.call.started|completed|failed|denied` via `GrpcMappers.eventTypeToString` — those events currently arrive and are silently ignored (the switch has no default). This plan adds cases to that switch, an inline `_ToolCallEntry` (mirroring `_MessageEntry`'s `ValueNotifier` idiom), and a presentational `ToolCallCard` widget (mirroring `ApprovalCard`).

**Tech Stack:** Flutter/Dart (SDK ^3.10.4, so Dart 3 `sealed class` is available), `flutter_test` widget tests, no state-management framework (StatefulWidget + setState + ValueNotifier is the house idiom).

**Parallelism:** Fully isolated in `turing-client/turing_app/lib` + `test` — touches no Go module and does not regenerate `gen/`. Safe to run alongside Plan #1 (`agent-runtime-go`) and Plan #3 (`orchestrator-go`).

---

## ⚠️ Trap: there are TWO `ChatScreen` files — edit the right one

- **EDIT:** `lib/features/chat/chat_screen.dart` — the live, gRPC-wired screen (opened by `SessionListScreen._openChat`).
- **DO NOT TOUCH:** `lib/ui/chat/chat_screen.dart` (+ `lib/ui/chat/widgets/chat_bubble.dart`, `lib/models/chat_message.dart`) — dead legacy dummy code, imported by nobody.

## Cross-plan contract (frozen)

This plan renders events emitted by **Plan #1's** `Execute` loop. The payload keys are frozen there and this plan consumes them:

| Event string | payload keys the client reads |
|---|---|
| `tool.call.started` | `toolCallId` (string), `toolName` (string), `serverName` (string, **optional** — may be absent) |
| `tool.call.completed` | `toolCallId`, `toolName` |
| `tool.call.failed` | `toolCallId`, `toolName`, `error` (string) |
| `tool.call.denied` | `toolCallId`, `toolName` |

The client keys tool activity on `toolCallId` to correlate `started → completed/failed/denied`. **No dependency on Plan #1 being merged** — widget tests feed synthetic `TuringEvent`s, so this plan is built and tested standalone. (End-to-end visual confirmation needs Plan #1 live; that's a post-merge check, not a task blocker.)

> Recommendation for Plan #1 (optional, non-blocking): include `serverName` in the `TOOL_CALL_STARTED` emit payload so the card can show `system / system.time`. This plan degrades gracefully if it's absent.

## Design decisions (locked)

1. **Inline entries, not a separate strip.** Tool activity renders in chronological order within the message flow (between bubbles), so a user sees "text → 🔧 tool → answer". This is better UX than the `_approvals` strip and is achieved by making the render list hold a small **sealed hierarchy** (`_ChatEntry` → `_MessageEntry` | `_ToolCallEntry`).
2. **`ToolCallCard` is purely presentational** (StatelessWidget taking plain values + status), mirroring `ApprovalCard`. No gRPC access — tool cards are display-only (there are no buttons; approvals remain the interactive surface).
3. **Live status via `ValueNotifier<ToolCallStatus>`** on the entry (mirroring `_MessageEntry.content`), so `completed`/`failed` updates rebuild only that one card. Disposed in `dispose()`.
4. **Defensive on ordering / missing `started`.** If `completed`/`failed`/`denied` arrives with no prior `started` (replay gap), create the entry in that terminal state rather than dropping it.
5. **No proto/mapper change needed.** `eventTypeToString` already maps the `TOOL_CALL_*` enums. A mapper test is added to lock that, but no code changes there.

## File structure

- Create: `lib/features/chat/tool_call_card.dart` — `ToolCallCard` widget + `ToolCallStatus` enum.
- Modify: `lib/features/chat/chat_screen.dart` — sealed `_ChatEntry`; rename current entry to `_MessageEntry`; add `_ToolCallEntry`; `Map<String,_ToolCallEntry> _toolEntries`; new switch cases; `_ChatMessageTile` switches on entry type; dispose updates.
- Test: create `test/features/tool_call_card_test.dart`; extend `test/features/chat_screen_test.dart`; extend `test/models/grpc_mappers_test.dart`.

Run tests: `cd turing-client/turing_app && flutter test`. Lint: `flutter analyze`.

---

## Phase 0 — The `ToolCallCard` widget (isolated, no screen changes)

### Task 0: Presentational tool-call card

**Files:**
- Create: `turing-client/turing_app/lib/features/chat/tool_call_card.dart`
- Test: `turing-client/turing_app/test/features/tool_call_card_test.dart`

- [ ] **Step 1: Write the failing test** (mirror `approval_card_test.dart`)

```dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/tool_call_card.dart';

void main() {
  testWidgets('running card shows tool name and a progress indicator', (tester) async {
    await tester.pumpWidget(const MaterialApp(
      home: ToolCallCard(toolName: 'system.time', serverName: 'system', status: ToolCallStatus.running),
    ));
    expect(find.text('system.time'), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
  });

  testWidgets('completed card shows a check and no spinner', (tester) async {
    await tester.pumpWidget(const MaterialApp(
      home: ToolCallCard(toolName: 'system.time', status: ToolCallStatus.completed),
    ));
    expect(find.byIcon(Icons.check_circle_outline), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);
  });

  testWidgets('failed card shows the error text', (tester) async {
    await tester.pumpWidget(const MaterialApp(
      home: ToolCallCard(toolName: 'files.create', status: ToolCallStatus.failed, error: 'mcp_call_failed'),
    ));
    expect(find.textContaining('mcp_call_failed'), findsOneWidget);
    expect(find.byIcon(Icons.error_outline), findsOneWidget);
  });
}
```

- [ ] **Step 2: Run, confirm failure**

Run: `cd turing-client/turing_app && flutter test test/features/tool_call_card_test.dart`
Expected: FAIL — `tool_call_card.dart` / `ToolCallCard` / `ToolCallStatus` don't exist.

- [ ] **Step 3: Implement the widget**

```dart
import 'package:flutter/material.dart';

enum ToolCallStatus { running, completed, failed, denied }

class ToolCallCard extends StatelessWidget {
  const ToolCallCard({
    super.key,
    required this.toolName,
    required this.status,
    this.serverName,
    this.error,
  });

  final String toolName;
  final ToolCallStatus status;
  final String? serverName;
  final String? error;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final label = (serverName == null || serverName!.isEmpty) ? toolName : '$serverName / $toolName';

    return Align(
      alignment: Alignment.centerLeft,
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 640),
        child: Card(
          margin: const EdgeInsets.symmetric(vertical: 4),
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Row(mainAxisSize: MainAxisSize.min, children: [
                  _leading(theme),
                  const SizedBox(width: 10),
                  Flexible(child: Text(toolName, style: theme.textTheme.bodyMedium)),
                ]),
                if (serverName != null && serverName!.isNotEmpty)
                  Padding(
                    padding: const EdgeInsets.only(left: 28, top: 2),
                    child: Text(label, style: theme.textTheme.bodySmall),
                  ),
                if (status == ToolCallStatus.failed && (error?.isNotEmpty ?? false))
                  Padding(
                    padding: const EdgeInsets.only(left: 28, top: 4),
                    child: Text(error!, style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.error)),
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  Widget _leading(ThemeData theme) {
    switch (status) {
      case ToolCallStatus.running:
        return const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2));
      case ToolCallStatus.completed:
        return Icon(Icons.check_circle_outline, size: 18, color: theme.colorScheme.primary);
      case ToolCallStatus.failed:
        return Icon(Icons.error_outline, size: 18, color: theme.colorScheme.error);
      case ToolCallStatus.denied:
        return Icon(Icons.block, size: 18, color: theme.colorScheme.error);
    }
  }
}
```

- [ ] **Step 4: Run, confirm pass**

Run: `cd turing-client/turing_app && flutter test test/features/tool_call_card_test.dart`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add turing-client/turing_app/lib/features/chat/tool_call_card.dart turing-client/turing_app/test/features/tool_call_card_test.dart
git commit -m "feat(client): presentational ToolCallCard widget with status states"
```

---

## Phase 1 — Sealed entry hierarchy in the chat screen

Refactor the private `_ChatEntry` into a sealed base so the render list can hold both message bubbles and tool cards. Pure refactor — no behavior change yet; the existing `chat_screen_test.dart` must stay green.

### Task 1: Introduce `_MessageEntry` + `_ToolCallEntry`

**Files:**
- Modify: `turing-client/turing_app/lib/features/chat/chat_screen.dart`
- Test: existing `turing-client/turing_app/test/features/chat_screen_test.dart` (must remain green)

- [ ] **Step 1: Run the existing tests to establish a green baseline**

Run: `cd turing-client/turing_app && flutter test test/features/chat_screen_test.dart`
Expected: PASS (baseline before refactor).

- [ ] **Step 2: Refactor the entry model** (in `chat_screen.dart`)

Replace the single `_ChatEntry` class (bottom of the file) with:

```dart
import 'tool_call_card.dart'; // add to imports

sealed class _ChatEntry {
  void dispose();
}

class _MessageEntry extends _ChatEntry {
  _MessageEntry({required this.messageId, required this.isUser, required String content})
      : content = ValueNotifier(content);

  factory _MessageEntry.user({required String messageId, required String content}) =>
      _MessageEntry(messageId: messageId, isUser: true, content: content);
  factory _MessageEntry.assistant({required String messageId, required String content}) =>
      _MessageEntry(messageId: messageId, isUser: false, content: content);
  factory _MessageEntry.fromMessage(Message message) => _MessageEntry(
      messageId: message.messageId, isUser: message.role == 'user', content: message.content);

  final String messageId;
  final bool isUser;
  final ValueNotifier<String> content;

  @override
  void dispose() => content.dispose();
}

class _ToolCallEntry extends _ChatEntry {
  _ToolCallEntry({
    required this.toolCallId,
    required this.toolName,
    required ToolCallStatus status,
    this.serverName,
    this.error,
  }) : status = ValueNotifier(status);

  final String toolCallId;
  final String toolName;
  final String? serverName;
  String? error;
  final ValueNotifier<ToolCallStatus> status;

  @override
  void dispose() => status.dispose();
}
```

Then update every reference from the old class:
- `final List<_ChatEntry> _messages = [];` stays (now holds the sealed base).
- `final Map<String, _ChatEntry> _assistantEntries` → `final Map<String, _MessageEntry> _assistantEntries = {};`
- `_ChatEntry.fromMessage` → `_MessageEntry.fromMessage`
- `_ChatEntry.user(...)` in `_sendMessage` → `_MessageEntry.user(...)`
- `_ChatEntry.assistant(...)` in `_applyMessageDelta` → `_MessageEntry.assistant(...)`
- dispose loop `for (final message in _messages) { message.content.dispose(); }` → `for (final entry in _messages) { entry.dispose(); }`

- [ ] **Step 3: Update `_ChatMessageTile` to switch on the sealed type**

```dart
class _ChatMessageTile extends StatelessWidget {
  const _ChatMessageTile({required this.entry});
  final _ChatEntry entry;

  @override
  Widget build(BuildContext context) {
    switch (entry) {
      case _MessageEntry m:
        return _MessageBubble(entry: m);
      case _ToolCallEntry t:
        return ValueListenableBuilder<ToolCallStatus>(
          valueListenable: t.status,
          builder: (context, status, _) =>
              ToolCallCard(toolName: t.toolName, serverName: t.serverName, status: status, error: t.error),
        );
    }
  }
}
```

Extract the existing bubble body into `_MessageBubble` (same widget tree as the current `_ChatMessageTile.build`, but typed to `_MessageEntry`). The `itemBuilder` stays `_ChatMessageTile(entry: _messages[index])`.

- [ ] **Step 4: Run the existing chat tests — still green (no behavior change)**

Run: `cd turing-client/turing_app && flutter test test/features/chat_screen_test.dart && flutter analyze`
Expected: PASS, analyzer clean (exhaustive `switch` on a sealed type needs no default).

- [ ] **Step 5: Commit**

```bash
git add turing-client/turing_app/lib/features/chat/chat_screen.dart
git commit -m "refactor(client): sealed _ChatEntry hierarchy (message + tool-call entries)"
```

---

## Phase 2 — Handle tool-call events

### Task 2: New switch cases + entry management

**Files:**
- Modify: `turing-client/turing_app/lib/features/chat/chat_screen.dart`
- Test: extend `turing-client/turing_app/test/features/chat_screen_test.dart`

- [ ] **Step 1: Write the failing tests** (append to the existing file; reuse its `_FakeApiClient`, `_FakeEventSource`, `_event` helpers)

```dart
testWidgets('tool.call.started renders a running tool card', (tester) async {
  final events = StreamController<TuringEvent>(sync: true);
  await tester.pumpWidget(MaterialApp(home: ChatScreen(
    sessionId: 'sess_1', apiClient: _FakeApiClient(), eventSource: _FakeEventSource(events.stream))));
  await tester.pump();

  events.add(_event(type: 'tool.call.started', sequence: 1,
      payload: {'toolCallId': 'call_1', 'toolName': 'system.time', 'serverName': 'system'}));
  await tester.pump();

  expect(find.text('system.time'), findsOneWidget);
  expect(find.byType(CircularProgressIndicator), findsOneWidget);

  await tester.pumpWidget(const SizedBox.shrink());
  unawaited(events.close());
});

testWidgets('tool.call.completed updates the same card to completed', (tester) async {
  final events = StreamController<TuringEvent>(sync: true);
  await tester.pumpWidget(MaterialApp(home: ChatScreen(
    sessionId: 'sess_1', apiClient: _FakeApiClient(), eventSource: _FakeEventSource(events.stream))));
  await tester.pump();

  events.add(_event(type: 'tool.call.started', sequence: 1,
      payload: {'toolCallId': 'call_1', 'toolName': 'system.time'}));
  await tester.pump();
  events.add(_event(type: 'tool.call.completed', sequence: 2,
      payload: {'toolCallId': 'call_1', 'toolName': 'system.time'}));
  await tester.pump();

  expect(find.byIcon(Icons.check_circle_outline), findsOneWidget);
  expect(find.byType(CircularProgressIndicator), findsNothing);
  expect(find.text('system.time'), findsOneWidget); // still exactly one card, not two

  await tester.pumpWidget(const SizedBox.shrink());
  unawaited(events.close());
});

testWidgets('tool.call.failed shows the error', (tester) async {
  final events = StreamController<TuringEvent>(sync: true);
  await tester.pumpWidget(MaterialApp(home: ChatScreen(
    sessionId: 'sess_1', apiClient: _FakeApiClient(), eventSource: _FakeEventSource(events.stream))));
  await tester.pump();

  events.add(_event(type: 'tool.call.failed', sequence: 1,
      payload: {'toolCallId': 'call_9', 'toolName': 'files.create', 'error': 'mcp_call_failed'}));
  await tester.pump();

  expect(find.textContaining('mcp_call_failed'), findsOneWidget); // created defensively w/o prior started
  await tester.pumpWidget(const SizedBox.shrink());
  unawaited(events.close());
});
```

- [ ] **Step 2: Run, confirm failure**

Run: `cd turing-client/turing_app && flutter test test/features/chat_screen_test.dart`
Expected: FAIL — tool events are ignored, no cards rendered.

- [ ] **Step 3: Implement the handlers**

Add the map field near the other state (line ~32):
```dart
final Map<String, _ToolCallEntry> _toolEntries = {};
```

Add cases to `_applyEvent`'s switch:
```dart
case 'tool.call.started':
  _applyToolCall(event, ToolCallStatus.running);
  break;
case 'tool.call.completed':
  _applyToolCall(event, ToolCallStatus.completed);
  break;
case 'tool.call.failed':
  _applyToolCall(event, ToolCallStatus.failed);
  break;
case 'tool.call.denied':
  _applyToolCall(event, ToolCallStatus.denied);
  break;
```

Add the handler:
```dart
void _applyToolCall(TuringEvent event, ToolCallStatus status) {
  final toolCallId = event.payload['toolCallId'] as String?;
  final toolName = event.payload['toolName'] as String? ?? 'tool';
  if (toolCallId == null) return;
  final error = event.payload['error'] as String?;

  var entry = _toolEntries[toolCallId];
  if (entry == null) {
    // create (either on 'started', or defensively on a terminal event with no prior start)
    entry = _ToolCallEntry(
      toolCallId: toolCallId,
      toolName: toolName,
      serverName: event.payload['serverName'] as String?,
      status: status,
      error: error,
    );
    _toolEntries[toolCallId] = entry;
    setState(() => _messages.add(entry!));
    _scrollToBottom();
    return;
  }
  // update existing card in place (ValueNotifier drives the rebuild; no setState needed)
  if (error != null) entry.error = error;
  entry.status.value = status;
  _scrollToBottom();
}
```

Note the terminal-first path sets `entry.error` at construction; the update path assigns `entry.error` before flipping `status` so the `ValueListenableBuilder` rebuild reads the new error.

- [ ] **Step 4: Run, confirm pass**

Run: `cd turing-client/turing_app && flutter test test/features/chat_screen_test.dart && flutter analyze`
Expected: PASS, analyzer clean.

- [ ] **Step 5: Commit**

```bash
git add turing-client/turing_app/lib/features/chat/chat_screen.dart turing-client/turing_app/test/features/chat_screen_test.dart
git commit -m "feat(client): render tool-call events inline in chat"
```

---

## Phase 3 — Lock the enum→string mapping + ordering

### Task 3a: Mapper regression test

**Files:**
- Test: extend `turing-client/turing_app/test/models/grpc_mappers_test.dart`

- [ ] **Step 1: Add a test** asserting `GrpcMappers.eventTypeToString` maps each `TOOL_CALL_*` enum to the expected string (`tool.call.started|completed|failed|denied`). This guards against a future codegen/mapping change silently dropping tool events (the screen switch has no default, so an unmapped type is invisible).
- [ ] **Step 2: Run** — Expected: PASS immediately (mapping already exists). If it FAILS, the mapping regressed — fix `grpc_mappers.dart` before proceeding.
- [ ] **Step 3: Commit.**
```bash
git commit -am "test(client): lock TOOL_CALL_* event-type string mapping"
```

### Task 3b: Chronological ordering test

**Files:**
- Test: extend `turing-client/turing_app/test/features/chat_screen_test.dart`

- [ ] **Step 1: Add a test** that interleaves a `message.delta`, a `tool.call.started`+`completed`, and a second `message.delta` (different `messageId`), then asserts the render order is: first bubble, tool card, second bubble — by checking the vertical positions via `tester.getTopLeft(find.text(...)).dy` ordering. This proves tool activity appears inline between messages.
- [ ] **Step 2: Run → PASS** (entries are appended to `_messages` in arrival order, so this should pass; if not, fix ordering).
- [ ] **Step 3: Commit.**
```bash
git commit -am "test(client): tool cards render chronologically between messages"
```

---

## Phase 4 — Verify

### Task 4: Full client test suite + analyzer

- [ ] Run: `cd turing-client/turing_app && flutter test` — Expected: ALL green (new + existing).
- [ ] Run: `cd turing-client/turing_app && flutter analyze` — Expected: no issues.
- [ ] Post-merge visual check (needs Plan #1 live, NOT a task blocker): run the stack, send a prompt that triggers a tool call, confirm the running→completed card appears inline. Document in the PR description.
- [ ] Commit any analyzer fixups.

---

## Self-review checklist

- **Spec coverage:** card widget with all 4 states (Task 0) ✓; inline rendering via sealed entries (Task 1) ✓; started/completed/failed/denied handled + defensive create (Task 2) ✓; mapping locked + ordering proven (Task 3) ✓.
- **Type consistency:** `ToolCallStatus` used identically in `tool_call_card.dart`, `_ToolCallEntry`, and `_applyToolCall` ✓. Entries keyed by `toolCallId` in `_toolEntries`, mirroring `_assistantEntries` keyed by `messageId` ✓.
- **House idioms honored:** StatelessWidget card (like `ApprovalCard`); `ValueNotifier` for live status (like `_MessageEntry.content`); constructor-injected fakes in tests; every notifier disposed ✓.
- **Isolation:** no Go files, no `gen/` regeneration, no proto edits — parallel-safe with Plans #1/#3 ✓.
- **Trap avoided:** all edits in `lib/features/chat/*`, none in the dead `lib/ui/chat/*` ✓.
- **Deferred/flagged:** `serverName` enrichment depends on Plan #1's emit (graceful if absent); a "denied" tool and the approval card may both surface for the same tool — acceptable, they convey different stages.
```
