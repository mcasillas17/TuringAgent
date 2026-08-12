# Flutter: Surface Truncated Runs (`agent.run.step`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When the agent hits its tool-iteration cap, tell the user. Today the run is marked *completed*, the answer is whatever text happened to accumulate, and there is no indication anything was cut short.

**Architecture:** Handle the already-mapped `agent.run.step` event in the live chat screen and render it as an inline notice, using the same entry/rendering pattern the tool-call cards use.

**Tech Stack:** Flutter/Dart, `turing-client/turing_app`. No backend or proto changes.

---

## The gap

The runtime emits `TURING_EVENT_TYPE_AGENT_RUN_STEP` when the loop gives up (`turing-backend/agent-runtime-go/internal/agent/general_assistant.go:271-274`):

```go
if toolIteration >= maxToolIterations {
    emit(messageEvent(job, turingv1.TuringEventType_TURING_EVENT_TYPE_AGENT_RUN_STEP, map[string]any{
        "note":              "maximum tool iterations reached",
        "maxToolIterations": maxToolIterations,
    }))
```

The client maps it — `lib/models/grpc_mappers.dart:118` returns the string `agent.run.step` — and then **nothing consumes it**. `lib/features/chat/chat_screen.dart`'s `_applyEvent` switch handles `message.delta`, `approval.*` and `tool.call.*` only, and its `switch` has no `default`, so the event is silently discarded.

What the user sees instead: a run marked completed whose content is the concatenated preamble the model produced alongside each tool call — e.g. `"Let me check that for you.Let me check that for you..."` — with no separator and no explanation. The truncation signal exists and is thrown away.

**Payload contract** (frozen by the emit site above): `note` (string) and `maxToolIterations` (number). Note `maxToolIterations` arrives as a **`double`**, not an `int` — the payload crosses a `google.protobuf.Struct`, whose only numeric type is `number_value`, and `GrpcMappers._valueToDart` returns `value.numberValue` (a Dart `double`). Casting it with `as int` throws.

## Design decisions (locked)

1. **Render it inline, in run order** — not as a banner or a toast. It refers to a specific point in the conversation, and the chat already renders tool activity inline via the sealed `_ChatEntry` hierarchy. Reuse that.
2. **It is a notice, not a message.** Do not render it as an assistant bubble; the model did not say it. Style it as system/meta text, visually distinct from both bubbles and tool cards.
3. **Read the `note` from the payload, do not hardcode the string.** The runtime owns the wording. Fall back to a generic sentence only when `note` is missing or empty.
4. **Tolerate any payload shape.** Follow the existing `_asString` discipline in `chat_screen.dart`: a producer bug must never throw out of the stream listener, because that kills the subscription and with it every later event. In particular, **never** cast `maxToolIterations` with `as int`.
5. **Do not de-duplicate across replays.** The subscription replays the persisted log, and `_applyEvent` already filters replayed material by sequence watermark for tool calls. Follow whatever the surrounding code does for its own events rather than inventing a second mechanism — check `_replayWatermarkSequence` usage and match it.
6. **Accessibility parity with the tool cards.** The notice must reach the semantics tree with its text, the same way `ToolCallCard` does — this is exactly the kind of meta-information a screen-reader user would otherwise miss entirely.

## File structure

- Modify: `turing-client/turing_app/lib/features/chat/chat_screen.dart` — new case in `_applyEvent`, a `_RunNoticeEntry` in the sealed `_ChatEntry` hierarchy, and rendering in `_ChatMessageTile`.
- Create: `turing-client/turing_app/lib/features/chat/run_notice_card.dart` — the presentational widget.
- Create: `turing-client/turing_app/test/features/run_notice_card_test.dart`.
- Modify: `turing-client/turing_app/test/features/chat_screen_test.dart`.
- Modify: `turing-client/turing_app/test/models/grpc_mappers_test.dart` — lock the enum→string mapping.

**Do not touch** anything outside `turing-client/turing_app`. Backend work is in flight elsewhere; this plan needs no backend change.

Verification:
```bash
( cd turing-client/turing_app && flutter test && flutter analyze )
```

---

## Task 1: The notice widget

**Files:** `lib/features/chat/run_notice_card.dart`, `test/features/run_notice_card_test.dart`

Mirror `lib/features/chat/tool_call_card.dart` — a `StatelessWidget` taking plain values, no gRPC, no state.

- [ ] **Step 1: Failing test**

```dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/run_notice_card.dart';

void main() {
  testWidgets('renders the note text', (tester) async {
    await tester.pumpWidget(const MaterialApp(
      home: Scaffold(body: RunNoticeCard(note: 'maximum tool iterations reached')),
    ));
    expect(find.textContaining('maximum tool iterations reached'), findsOneWidget);
  });

  testWidgets('reaches the semantics tree', (tester) async {
    final handle = tester.ensureSemantics();
    await tester.pumpWidget(const MaterialApp(
      home: Scaffold(body: RunNoticeCard(note: 'maximum tool iterations reached')),
    ));
    expect(find.bySemanticsLabel(RegExp('maximum tool iterations reached')), findsOneWidget);
    handle.dispose();
  });
}
```

- [ ] **Step 2: Run, confirm failure.** `flutter test test/features/run_notice_card_test.dart`
- [ ] **Step 3: Implement** — a centred, muted, full-width row (visually unlike both the left/right bubbles and the tool cards), with an `Icons.info_outline`, wrapped so its text reaches semantics. Constrain to `maxWidth: 640` like the neighbouring widgets.
- [ ] **Step 4: Run, confirm pass. Commit.**

## Task 2: Handle the event

**Files:** `lib/features/chat/chat_screen.dart`, `test/features/chat_screen_test.dart`

- [ ] **Step 1: Failing tests** in `chat_screen_test.dart`, reusing its `_FakeApiClient` / `_FakeEventSource` / `_event` helpers:

```dart
testWidgets('agent.run.step renders a truncation notice', (tester) async {
  // ... pump ChatScreen with a fake event source ...
  events.add(_event(type: 'agent.run.step', sequence: 1, payload: {
    'note': 'maximum tool iterations reached',
    'maxToolIterations': 5.0, // a Struct number arrives as double
  }));
  await tester.pump();
  expect(find.textContaining('maximum tool iterations reached'), findsOneWidget);
});

// A producer bug must not kill the subscription and with it every later event.
testWidgets('a malformed agent.run.step does not break the stream', (tester) async {
  events.add(_event(type: 'agent.run.step', sequence: 1, payload: {
    'note': 42,                       // wrong type
    'maxToolIterations': 'five',      // wrong type
  }));
  await tester.pump();
  events.add(_event(type: 'message.delta', sequence: 2,
      payload: {'messageId': 'm1', 'delta': 'still alive'}));
  await tester.pump();
  expect(find.text('still alive'), findsOneWidget);
});

testWidgets('the notice renders in run order, after earlier text', (tester) async {
  // message.delta, then agent.run.step; assert the notice sits BELOW the bubble
  // via tester.getTopLeft(...).dy ordering.
});
```

- [ ] **Step 2: Run, confirm failure** — the event is currently discarded, so nothing renders.

- [ ] **Step 3: Implement.**
  - Add `_RunNoticeEntry` to the sealed `_ChatEntry` hierarchy (it needs a `dispose()` like its siblings; it holds no notifier, so the body is empty).
  - Add a `case 'agent.run.step':` to `_applyEvent`, reading `note` via the existing `_asString` helper and falling back to a generic sentence when absent or empty. **Do not read `maxToolIterations` with `as int`** — if you display it at all, read it as `num?` and format it.
  - Append the entry inside `setState` and scroll, matching how `_applyToolCall` appends its card.
  - Render via `_ChatMessageTile`'s switch — the sealed type makes the analyzer demand the new case, which is the point.
  - Apply the same replay-suppression rule the surrounding code uses for its own events; check `_replayWatermarkSequence` and match it rather than inventing a second scheme.

- [ ] **Step 4: Run, confirm pass.** `flutter test && flutter analyze`. Commit.

## Task 3: Lock the mapping

**Files:** `test/models/grpc_mappers_test.dart`

- [ ] **Step 1: Add** an assertion that `GrpcMappers.eventTypeToString(TuringEventType.TURING_EVENT_TYPE_AGENT_RUN_STEP)` is `'agent.run.step'`, alongside the existing `TOOL_CALL_*` assertions.

Rationale: the chat screen's switch has no `default`, so if the mapping ever changes the event is silently dropped again with no test failure anywhere — exactly the state this plan is fixing.

- [ ] **Step 2: Run, confirm it passes** (the mapping already exists; this pins it). **If it fails, the mapping regressed** — fix that first. Commit.

## Task 4: See it for real

The truncation path is hard to trigger deliberately, so this is best-effort and must not block the PR.

- [ ] **Step 1:** With the stack up and Ollama running, ask something that makes the model keep calling tools — e.g. a vague multi-step request, or temporarily lower `maxToolIterations` in a local build to force it.
- [ ] **Step 2: Confirm** the notice appears inline after the last tool card, and the run still completes.
- [ ] **Step 3: Screenshot for the PR** if you get it to fire. **If you cannot trigger it, say so explicitly in the PR** rather than implying it was seen — the widget tests are the real evidence either way.

---

## Self-review checklist

- The notice renders inline and in order, not as a banner ✓
- Styled as meta/system, never as an assistant bubble ✓
- `note` comes from the payload; the hardcoded string is only a fallback ✓
- `maxToolIterations` is never cast `as int` — a Struct number is a Dart `double` ✓
- A malformed payload cannot throw out of the listener and kill the subscription, and there is a test for it ✓
- Reaches the semantics tree, like the tool cards ✓
- Replay handling matches the surrounding code rather than inventing a second scheme ✓
- The enum→string mapping is pinned, since the switch has no `default` to catch a regression ✓
