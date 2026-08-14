import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/run_cancelled_card.dart';
import 'package:turing_flutter_app/features/chat/run_failure_card.dart';
import 'package:turing_flutter_app/features/chat/run_notice_card.dart';
import 'package:turing_flutter_app/features/chat/chat_screen.dart';
import 'package:turing_flutter_app/features/chat/tool_call_card.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/turing_event.dart';
import 'package:turing_flutter_app/networking/api_client.dart';
import 'package:turing_flutter_app/networking/event_source.dart';

void main() {
  testWidgets('chat streams message deltas into one assistant bubble', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final apiClient = _FakeApiClient();

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'message.delta',
        sequence: 1,
        payload: {'messageId': 'msg_asst', 'delta': 'Hel'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'message.delta',
        sequence: 2,
        payload: {'messageId': 'msg_asst', 'delta': 'lo'},
      ),
    );
    await tester.pump();

    expect(find.text('Hello'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('agent.run.step renders the runtime note', (tester) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'agent.run.step',
        sequence: 1,
        payload: {
          'note': 'maximum tool iterations reached',
          'maxToolIterations': 5.0,
        },
      ),
    );
    await tester.pump();

    expect(find.byType(RunNoticeCard), findsOneWidget);
    expect(find.text('maximum tool iterations reached'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a malformed agent.run.step does not break the stream', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'agent.run.step',
        sequence: 1,
        payload: {'note': 42, 'maxToolIterations': 'five'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'message.delta',
        sequence: 2,
        payload: {'messageId': 'm1', 'delta': 'still alive'},
      ),
    );
    await tester.pump();

    expect(
      find.text('The run reported a step with no description'),
      findsOneWidget,
    );
    expect(find.text('still alive'), findsOneWidget);
    expect(tester.takeException(), isNull);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('agent.run.step falls back for absent and empty notes', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(_event(type: 'agent.run.step', sequence: 1, payload: const {}));
    await tester.pump();
    events.add(
      _event(type: 'agent.run.step', sequence: 2, payload: const {'note': ''}),
    );
    await tester.pump();

    expect(
      find.text('The run reported a step with no description'),
      findsNWidgets(2),
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('the run notice renders below earlier assistant text', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'message.delta',
        sequence: 1,
        payload: {'messageId': 'm1', 'delta': 'Let me check.'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'agent.run.step',
        sequence: 2,
        payload: {'note': 'maximum tool iterations reached'},
      ),
    );
    await tester.pump();

    expect(
      tester.getTopLeft(find.byType(RunNoticeCard)).dy,
      greaterThan(tester.getTopLeft(find.text('Let me check.')).dy),
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('the run notice renders below the last tool card', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.completed',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'agent.run.step',
        sequence: 2,
        payload: {'note': 'maximum tool iterations reached'},
      ),
    );
    await tester.pump();

    expect(
      tester.getTopLeft(find.byType(RunNoticeCard)).dy,
      greaterThan(tester.getTopLeft(find.byType(ToolCallCard)).dy),
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a replayed agent.run.step is not appended to history', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final persistedNotice = _event(
      type: 'agent.run.step',
      sequence: 7,
      payload: {'note': 'maximum tool iterations reached'},
    );
    final apiClient = _FakeApiClient()..initialEvents = [persistedNotice];

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(persistedNotice);
    await tester.pump();

    expect(find.byType(RunNoticeCard), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a run notice for completed history stays hidden', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final apiClient = _FakeApiClient()
      ..initialEvents = [
        _event(
          type: 'message.delta',
          sequence: 1,
          payload: {'messageId': 'msg_a1', 'delta': 'finished'},
        ),
      ]
      ..initialMessages = [
        Message(
          messageId: 'msg_a1',
          runId: 'run_1',
          role: 'assistant',
          content: 'finished',
          sequence: 1,
          createdAt: _fixedDate,
        ),
      ];

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'agent.run.step',
        sequence: 2,
        payload: {'note': 'maximum tool iterations reached'},
      ),
    );
    await tester.pump();

    expect(
      find.byType(RunNoticeCard),
      findsNothing,
      reason:
          'a notice for a run already represented by complete history would '
          'be appended below newer conversation content',
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('agent.run.failed renders a distinct failure card', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'agent.run.failed',
        sequence: 1,
        payload: {
          'code': 'job_timeout',
          'message': 'Job timed out',
          'retryable': false,
        },
      ),
    );
    await tester.pump();

    expect(find.byType(RunFailureCard), findsOneWidget);
    expect(find.text('Job timed out'), findsOneWidget);
    // A failure must not be indistinguishable from routine retry progress.
    expect(find.byType(RunNoticeCard), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('the run failure renders below earlier assistant text', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'message.delta',
        sequence: 1,
        payload: {'messageId': 'm1', 'delta': 'Let me check.'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'agent.run.failed',
        sequence: 2,
        payload: {'code': 'job_timeout', 'message': 'Job timed out'},
      ),
    );
    await tester.pump();

    expect(
      tester.getTopLeft(find.byType(RunFailureCard)).dy,
      greaterThan(tester.getTopLeft(find.text('Let me check.')).dy),
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('the run failure renders below the last tool card', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.completed',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'agent.run.failed',
        sequence: 2,
        payload: {'code': 'job_timeout', 'message': 'Job timed out'},
      ),
    );
    await tester.pump();

    expect(
      tester.getTopLeft(find.byType(RunFailureCard)).dy,
      greaterThan(tester.getTopLeft(find.byType(ToolCallCard)).dy),
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a malformed agent.run.failed does not break the stream', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'agent.run.failed',
        sequence: 1,
        payload: {'code': 42, 'message': 42, 'retryable': 'nope'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'message.delta',
        sequence: 2,
        payload: {'messageId': 'm1', 'delta': 'still alive'},
      ),
    );
    await tester.pump();

    expect(find.byType(RunFailureCard), findsOneWidget);
    expect(find.text('still alive'), findsOneWidget);
    expect(tester.takeException(), isNull);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets(
    'agent.run.failed falls back to a humanized code when the message is '
    'absent',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: _FakeApiClient(),
            eventSource: _FakeEventSource(events.stream),
          ),
        ),
      );
      await tester.pump();

      events.add(
        _event(
          type: 'agent.run.failed',
          sequence: 1,
          payload: const {'code': 'tool_discovery_failed', 'message': ''},
        ),
      );
      await tester.pump();

      // Never the bare machine code as the whole message.
      expect(find.text('tool_discovery_failed'), findsNothing);
      expect(find.text('Tool discovery failed'), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      unawaited(events.close());
    },
  );

  testWidgets('agent.run.failed falls back to the code when the message is '
      'whitespace-only', (tester) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'agent.run.failed',
        sequence: 1,
        payload: const {'code': 'tool_discovery_failed', 'message': '   '},
      ),
    );
    await tester.pump();

    // A blank-but-present message must not win over a usable code.
    expect(find.text('   '), findsNothing);
    expect(find.text('Tool discovery failed'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  // `_humanizeFailureCode` strips underscores before deriving a sentence
  // fragment from the code, so a code that is nothing BUT underscores (a
  // producer bug, or a not-yet-classified code stored as a literal `_`)
  // strips down to an empty string. Guard against that collapsing to a blank
  // card: assert the generic fallback renders instead, and that the stream
  // keeps delivering afterwards. Removing the empty-after-strip guard would
  // index into that empty string and throw, taking the whole subscription
  // (and every later event) down with it — the later `message.delta` in this
  // test is what would fail to appear if that regressed.
  testWidgets(
    'agent.run.failed falls back to generic text for an underscore-only '
    'code, and the stream keeps delivering afterwards',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: _FakeApiClient(),
            eventSource: _FakeEventSource(events.stream),
          ),
        ),
      );
      await tester.pump();

      events.add(
        _event(
          type: 'agent.run.failed',
          sequence: 1,
          payload: const {'code': '_'},
        ),
      );
      await tester.pump();

      expect(find.byType(RunFailureCard), findsOneWidget);
      // Narrow to the message text specifically: `RunFailureCard` now also
      // renders its outcome label ("Run failed") as a second, sibling `Text`
      // descendant, so grabbing "the" `Text` under this card is no longer
      // unambiguous.
      expect(
        find.descendant(
          of: find.byType(RunFailureCard),
          matching: find.text('The run failed with no further details'),
        ),
        findsOneWidget,
      );
      expect(find.text('_'), findsNothing);

      events.add(
        _event(
          type: 'message.delta',
          sequence: 2,
          payload: {'messageId': 'm1', 'delta': 'still alive'},
        ),
      );
      await tester.pump();

      expect(find.text('still alive'), findsOneWidget);
      expect(tester.takeException(), isNull);

      await tester.pumpWidget(const SizedBox.shrink());
      unawaited(events.close());
    },
  );

  testWidgets(
    'agent.run.failed prefers a valid message over a non-string code',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: _FakeApiClient(),
            eventSource: _FakeEventSource(events.stream),
          ),
        ),
      );
      await tester.pump();

      events.add(
        _event(
          type: 'agent.run.failed',
          sequence: 1,
          payload: const {'code': 42, 'message': 'Job timed out'},
        ),
      );
      await tester.pump();

      expect(find.text('Job timed out'), findsOneWidget);
      expect(tester.takeException(), isNull);

      await tester.pumpWidget(const SizedBox.shrink());
      unawaited(events.close());
    },
  );

  testWidgets(
    'agent.run.failed falls back to generic text when message and code are '
    'both absent',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: _FakeApiClient(),
            eventSource: _FakeEventSource(events.stream),
          ),
        ),
      );
      await tester.pump();

      events.add(
        _event(type: 'agent.run.failed', sequence: 1, payload: const {}),
      );
      await tester.pump();

      expect(find.byType(RunFailureCard), findsOneWidget);
      // Narrow to the message text specifically: `RunFailureCard` now also
      // renders its outcome label ("Run failed") as a second, sibling `Text`
      // descendant, so grabbing "the" `Text` under this card is no longer
      // unambiguous.
      expect(
        find.descendant(
          of: find.byType(RunFailureCard),
          matching: find.text('The run failed with no further details'),
        ),
        findsOneWidget,
      );
      await tester.pumpWidget(const SizedBox.shrink());
      unawaited(events.close());
    },
  );

  testWidgets('a replayed agent.run.failed is not appended to history', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final persistedFailure = _event(
      type: 'agent.run.failed',
      sequence: 7,
      payload: {'code': 'job_timeout', 'message': 'Job timed out'},
    );
    final apiClient = _FakeApiClient()..initialEvents = [persistedFailure];

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(persistedFailure);
    await tester.pump();

    expect(find.byType(RunFailureCard), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  // Unlike the `agent.run.step` case above, the backend cannot actually
  // produce an `agent.run.failed` for a run whose `agent.run.completed` is
  // already in history: completion and failure are mutually exclusive
  // terminal outcomes for the same run, so this specific interleaving is not
  // one the backend can produce. `_applyRunFailed` shares the exact same
  // `_isHistoricalRunEvent` guard as `_applyRunStep` regardless — purely for
  // defensive parity with the run-notice path, not because this scenario is
  // real. This test pins that shared guard, not a producible interleaving.
  testWidgets('a failure for a run already represented by completed history '
      'stays hidden', (tester) async {
    final events = StreamController<TuringEvent>(sync: true);
    final apiClient = _FakeApiClient()
      ..initialEvents = [
        _event(
          type: 'message.delta',
          sequence: 1,
          payload: {'messageId': 'msg_a1', 'delta': 'finished'},
        ),
      ]
      ..initialMessages = [
        Message(
          messageId: 'msg_a1',
          runId: 'run_1',
          role: 'assistant',
          content: 'finished',
          sequence: 1,
          createdAt: _fixedDate,
        ),
      ];

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    // Sequence 2 is newer than the watermark (1), so only the runId match
    // suppresses it.
    events.add(
      _event(
        type: 'agent.run.failed',
        sequence: 2,
        payload: {'code': 'job_timeout', 'message': 'Job timed out'},
      ),
    );
    await tester.pump();

    expect(
      find.byType(RunFailureCard),
      findsNothing,
      reason:
          'a failure card for a run already represented by complete history '
          'would be appended below newer conversation content',
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  // A server-initiated cancellation is a live signal even though this screen
  // has no cancel affordance: `cancelRun` (orchestrator-go
  // internal/service/chat/service.go) fires this event on exactly two
  // conditions — SendMessage's own context being cancelled (checked at four
  // checkpoints: initial send, dispatch loop teardown, replay error, relay
  // send) or DispatchPending failing unconditionally. A bare `stream.Send`
  // failure does not cancel a run on its own. Without a dedicated handler
  // this event would fall through the switch's unhandled default (see the
  // comment above that default) and leave a silent, unexplained turn.
  testWidgets('agent.run.cancelled renders a distinct terminal error-style '
      'card with truthful, human-readable cancellation wording — never the '
      'raw machine `reason` enum', (tester) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'agent.run.cancelled',
        sequence: 1,
        payload: {'reason': 'client_cancelled'},
      ),
    );
    await tester.pump();

    expect(find.byType(RunCancelledCard), findsOneWidget);
    // `client_cancelled` is machine metadata (see `cancelRun` above), not
    // display copy, and it is truthful across both of its triggers — so the
    // card must show human wording valid for both, never the bare enum
    // value.
    expect(
      find.text('The run was cancelled before it could finish'),
      findsOneWidget,
    );
    expect(find.text('client_cancelled'), findsNothing);
    // The outcome title itself must also be visible on screen, not only in
    // the accessibility tree, and must never say "failed".
    expect(find.text('Run cancelled'), findsOneWidget);
    expect(find.text('Run failed'), findsNothing);
    // A cancellation must not be indistinguishable from a failure or from
    // routine retry progress.
    expect(find.byType(RunFailureCard), findsNothing);
    expect(find.byType(RunNoticeCard), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets(
    'the rendered agent.run.cancelled semantics label is truthful — "Run '
    'cancelled", never "Run failed"',
    (tester) async {
      final handle = tester.ensureSemantics();
      final events = StreamController<TuringEvent>(sync: true);

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: _FakeApiClient(),
            eventSource: _FakeEventSource(events.stream),
          ),
        ),
      );
      await tester.pump();

      events.add(
        _event(
          type: 'agent.run.cancelled',
          sequence: 1,
          payload: {'reason': 'client_cancelled'},
        ),
      );
      await tester.pump();

      expect(
        find.bySemanticsLabel(
          'Run cancelled: The run was cancelled before it could finish',
        ),
        findsOneWidget,
      );
      expect(
        find.bySemanticsLabel(
          'Run failed: The run was cancelled before it could finish',
        ),
        findsNothing,
      );

      await tester.pumpWidget(const SizedBox.shrink());
      unawaited(events.close());
      handle.dispose();
    },
  );

  testWidgets('agent.run.cancelled falls back to generic text when the '
      'reason is absent', (tester) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(type: 'agent.run.cancelled', sequence: 1, payload: const {}),
    );
    await tester.pump();

    expect(find.byType(RunCancelledCard), findsOneWidget);
    // Narrow to the message text specifically: `RunCancelledCard` now also
    // renders its outcome label ("Run cancelled") as a second, sibling
    // `Text` descendant, so grabbing "the" `Text` under this card is no
    // longer unambiguous.
    expect(
      find.descendant(
        of: find.byType(RunCancelledCard),
        matching: find.text('The run was cancelled with no further details'),
      ),
      findsOneWidget,
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('agent.run.cancelled falls back to generic text when the '
      'reason is whitespace-only', (tester) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'agent.run.cancelled',
        sequence: 1,
        payload: const {'reason': '   '},
      ),
    );
    await tester.pump();

    expect(find.text('   '), findsNothing);
    expect(
      find.text('The run was cancelled with no further details'),
      findsOneWidget,
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('agent.run.cancelled falls back to generic text when the '
      'reason is an unrecognized, non-empty value', (tester) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'agent.run.cancelled',
        sequence: 1,
        // `client_cancelled` is currently the only reason the backend ever
        // emits (`cancelRun`, orchestrator-go internal/service/chat/
        // service.go). An unrecognized value is not a real producer today,
        // but the client must not surface it verbatim if one ever appears —
        // that would leak a bare enum straight to the user.
        payload: const {'reason': 'some_future_reason'},
      ),
    );
    await tester.pump();

    expect(find.byType(RunCancelledCard), findsOneWidget);
    expect(find.text('some_future_reason'), findsNothing);
    expect(
      find.text('The run was cancelled with no further details'),
      findsOneWidget,
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets(
    'a non-String agent.run.cancelled reason does not break the stream, and '
    'a later event is still processed',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: _FakeApiClient(),
            eventSource: _FakeEventSource(events.stream),
          ),
        ),
      );
      await tester.pump();

      events.add(
        _event(
          type: 'agent.run.cancelled',
          sequence: 1,
          payload: const {'reason': 42},
        ),
      );
      await tester.pump();

      expect(find.byType(RunCancelledCard), findsOneWidget);
      expect(
        find.text('The run was cancelled with no further details'),
        findsOneWidget,
      );
      expect(tester.takeException(), isNull);

      events.add(
        _event(
          type: 'message.delta',
          sequence: 2,
          payload: {'messageId': 'm1', 'delta': 'still alive'},
        ),
      );
      await tester.pump();

      expect(find.text('still alive'), findsOneWidget);
      expect(tester.takeException(), isNull);

      await tester.pumpWidget(const SizedBox.shrink());
      unawaited(events.close());
    },
  );

  testWidgets('the run cancellation renders below earlier assistant text', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'message.delta',
        sequence: 1,
        payload: {'messageId': 'm1', 'delta': 'Let me check.'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'agent.run.cancelled',
        sequence: 2,
        payload: {'reason': 'client_cancelled'},
      ),
    );
    await tester.pump();

    expect(
      tester.getTopLeft(find.byType(RunCancelledCard)).dy,
      greaterThan(tester.getTopLeft(find.text('Let me check.')).dy),
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a replayed agent.run.cancelled is not appended to history', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final persistedCancellation = _event(
      type: 'agent.run.cancelled',
      sequence: 7,
      payload: {'reason': 'client_cancelled'},
    );
    final apiClient = _FakeApiClient()..initialEvents = [persistedCancellation];

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(persistedCancellation);
    await tester.pump();

    expect(find.byType(RunCancelledCard), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  // Mirrors "a failure for a run already represented by completed history
  // stays hidden" above: completion and cancellation are also mutually
  // exclusive terminal outcomes for the same run, so the backend cannot
  // actually produce this interleaving either. `_applyRunCancelled` shares
  // the same `_isHistoricalRunEvent` guard as `_applyRunFailed` and
  // `_applyRunStep` purely for defensive parity, not because this scenario
  // is real. This test pins that shared guard, not a producible
  // interleaving.
  testWidgets('a cancellation for a run already represented by completed '
      'history stays hidden', (tester) async {
    final events = StreamController<TuringEvent>(sync: true);
    final apiClient = _FakeApiClient()
      ..initialEvents = [
        _event(
          type: 'message.delta',
          sequence: 1,
          payload: {'messageId': 'msg_a1', 'delta': 'finished'},
        ),
      ]
      ..initialMessages = [
        Message(
          messageId: 'msg_a1',
          runId: 'run_1',
          role: 'assistant',
          content: 'finished',
          sequence: 1,
          createdAt: _fixedDate,
        ),
      ];

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    // Sequence 2 is newer than the watermark (1), so only the runId match
    // suppresses it.
    events.add(
      _event(
        type: 'agent.run.cancelled',
        sequence: 2,
        payload: {'reason': 'client_cancelled'},
      ),
    );
    await tester.pump();

    expect(
      find.byType(RunCancelledCard),
      findsNothing,
      reason:
          'a cancellation card for a run already represented by complete '
          'history would be appended below newer conversation content',
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  // Retry exhaustion is a documented, decided double-report (Task 2 of
  // docs/superpowers/plans/2026-08-14-flutter-run-failure-rendering.md):
  // the backend emits both an `agent.run.step` give-up notice ("Gave up
  // after N attempts") and the terminal `agent.run.failed` for the same
  // run (`repository/jobs.go:120-133`). The chosen resolution is option
  // (a) — show both, worded so neither repeats the other — rather than
  // suppressing the card with per-run dedup state. This test pins that
  // decision: it fails if a future change collapses the two into one
  // card, silently drops either one, or lets their wording collide.
  testWidgets(
    'retry exhaustion shows one give-up notice and one distinct failure '
    'card, worded so neither repeats the other',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: _FakeApiClient(),
            eventSource: _FakeEventSource(events.stream),
          ),
        ),
      );
      await tester.pump();

      // Mirrors repository.RetryDecision for the exhausted-retries path
      // (`repository/jobs.go:120-133`, `giveUpNote`): the give-up
      // `agent.run.step` is ordered first (it explains why retrying
      // stopped), the terminal `agent.run.failed` follows. Once attempts
      // are exhausted, `RequeueOrFailRetryableRun` overwrites the failure
      // code to `RetriesExhaustedCode` ("retries_exhausted",
      // `repository/jobs.go:122`) but passes the *original* retryable
      // failure's message straight through unchanged (`failCode, failMessage
      // := code, message` at jobs.go:119). "worker cannot accept the run" is
      // that original message verbatim for the `worker_busy` producer
      // (`agent-runtime-go/internal/worker/worker.go:522-527`) — a real,
      // non-empty message reaching this path, distinct from the give-up
      // wording. Both events share one runId.
      const giveUpNote = 'Gave up after 3 attempts';
      const failureMessage = 'worker cannot accept the run';
      events.add(
        _event(
          type: 'agent.run.step',
          sequence: 1,
          payload: {'attempts': 3, 'maxAttempts': 3, 'note': giveUpNote},
        ),
      );
      await tester.pump();
      events.add(
        _event(
          type: 'agent.run.failed',
          sequence: 2,
          payload: {
            'code': 'retries_exhausted',
            'message': failureMessage,
            'retryable': false,
          },
        ),
      );
      await tester.pump();

      // Exactly one of each: no duplication and no suppression.
      expect(find.byType(RunNoticeCard), findsOneWidget);
      expect(find.byType(RunFailureCard), findsOneWidget);

      // The give-up wording, verbatim from the backend's `giveUpNote`, is
      // shown only inside the notice card.
      expect(
        find.descendant(
          of: find.byType(RunNoticeCard),
          matching: find.text(giveUpNote),
        ),
        findsOneWidget,
      );
      // The passed-through failure message, distinct wording, is shown only
      // inside the failure card.
      expect(
        find.descendant(
          of: find.byType(RunFailureCard),
          matching: find.text(failureMessage),
        ),
        findsOneWidget,
      );

      // Non-duplicative: neither card renders the other's text anywhere
      // inside it.
      expect(
        find.descendant(
          of: find.byType(RunNoticeCard),
          matching: find.text(failureMessage),
        ),
        findsNothing,
      );
      expect(
        find.descendant(
          of: find.byType(RunFailureCard),
          matching: find.text(giveUpNote),
        ),
        findsNothing,
      );

      // Event order preserved: the notice (why we stopped retrying) reads
      // above the failure card (what actually happened) in the transcript.
      expect(
        tester.getTopLeft(find.byType(RunNoticeCard)).dy,
        lessThan(tester.getTopLeft(find.byType(RunFailureCard)).dy),
      );

      await tester.pumpWidget(const SizedBox.shrink());
      unawaited(events.close());
    },
  );

  // The `message` the runtime passes into `RequeueOrFailRetryableRun` is an
  // ordinary proto string field (`RuntimeRunFailed.Message`), so nothing in
  // the contract guarantees it is non-empty by the time it reaches this
  // `code: "retries_exhausted"` terminal payload. Naively humanizing the code
  // here ("retries_exhausted" -> "Retries exhausted") would just restate the
  // give-up notice a second time with no new information — the actual cause
  // is still unknown at this point, so `_applyRunFailed` special-cases this
  // one code and renders the same cause-free generic fallback it uses when
  // no code or message is present at all (`chat_screen.dart`'s
  // `_runFailureFallbackNotice`). Pin that exact, non-repetitive wording so
  // the double-report never collapses into two cards that say the same
  // thing in different words.
  testWidgets(
    'retry exhaustion with an empty failure message falls back to the '
    'generic notice, not a restated "Retries exhausted"',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: _FakeApiClient(),
            eventSource: _FakeEventSource(events.stream),
          ),
        ),
      );
      await tester.pump();

      const giveUpNote = 'Gave up after 1 attempt';
      events.add(
        _event(
          type: 'agent.run.step',
          sequence: 1,
          payload: {'attempts': 1, 'maxAttempts': 1, 'note': giveUpNote},
        ),
      );
      await tester.pump();
      events.add(
        _event(
          type: 'agent.run.failed',
          sequence: 2,
          payload: {
            'code': 'retries_exhausted',
            'message': '',
            'retryable': false,
          },
        ),
      );
      await tester.pump();

      expect(find.byType(RunNoticeCard), findsOneWidget);
      expect(find.byType(RunFailureCard), findsOneWidget);

      // Exact, cause-free copy: not the humanized code, not the give-up
      // wording restated. Narrow to the message text specifically:
      // `RunFailureCard` now also renders its outcome label ("Run failed")
      // as a second, sibling `Text` descendant, so grabbing "the" `Text`
      // under this card is no longer unambiguous.
      expect(
        find.descendant(
          of: find.byType(RunFailureCard),
          matching: find.text('The run failed with no further details'),
        ),
        findsOneWidget,
      );

      // Never the humanized code anywhere: that would just repeat the
      // give-up notice's meaning without adding anything.
      expect(find.text('Retries exhausted'), findsNothing);
      expect(
        find.descendant(
          of: find.byType(RunFailureCard),
          matching: find.text(giveUpNote),
        ),
        findsNothing,
      );

      await tester.pumpWidget(const SizedBox.shrink());
      unawaited(events.close());
    },
  );

  testWidgets('chat sends selected provider through API client', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final apiClient = _FakeApiClient();

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    await tester.tap(find.byType(DropdownButton<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('OpenAI-compatible').last);
    await tester.pump();

    await tester.enterText(find.byType(TextField), 'Use cloud model');
    await tester.tap(find.byIcon(Icons.send));
    await tester.pump();

    expect(apiClient.lastSentContent, 'Use cloud model');
    expect(apiClient.lastModelProvider, 'openai_compatible');
    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('approval cards appear and clear from approval events', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final apiClient = _FakeApiClient();

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'approval.requested',
        sequence: 1,
        payload: {
          'approvalId': 'appr_1',
          'toolName': 'files.update',
          'argsSummary': 'Update note.txt',
        },
      ),
    );
    await tester.pump();

    expect(find.text('Approval requested: files.update'), findsOneWidget);
    expect(find.text('Update note.txt'), findsOneWidget);

    events.add(
      _event(
        type: 'approval.consumed',
        sequence: 2,
        payload: {'approvalId': 'appr_1'},
      ),
    );
    await tester.pump();

    expect(find.text('Approval requested: files.update'), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('persisted pending approvals are replayed when reopening', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final requested = _event(
      type: 'approval.requested',
      sequence: 7,
      payload: {
        'approvalId': 'appr_pending',
        'toolName': 'files.update',
        'argsSummary': 'Update note.txt',
      },
    );
    final apiClient = _FakeApiClient()..initialEvents = [requested];
    final eventSource = _FakeEventSource(events.stream);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: eventSource,
        ),
      ),
    );
    await tester.pump();

    expect(
      eventSource.lastSequence,
      0,
      reason:
          'approval lifecycle replay must start at the beginning even though '
          'historical tool cards are bounded by the startup watermark',
    );
    events.add(requested);
    await tester.pump();

    expect(find.text('Approval requested: files.update'), findsOneWidget);
    expect(find.text('Update note.txt'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('tool.call.started renders a running tool card', (tester) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {
          'toolCallId': 'call_1',
          'toolName': 'system.time',
          'serverName': 'system',
        },
      ),
    );
    await tester.pump();

    expect(find.text('system.time'), findsOneWidget);
    // Locks the optional `serverName` payload key all the way through to the
    // card; without this the card renders identically if the key is dropped.
    expect(find.text('system'), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a tool event without a toolCallId is ignored, not rendered', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    // Malformed/partial event: no correlation id, so no card can be tracked.
    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {'toolName': 'system.time'},
      ),
    );
    await tester.pump();

    expect(find.byType(ToolCallCard), findsNothing);

    // The stream survives it: a well-formed event still renders.
    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 2,
        payload: {'toolCallId': 'call_1', 'toolName': 'files.create'},
      ),
    );
    await tester.pump();

    expect(find.byType(ToolCallCard), findsOneWidget);
    expect(find.text('files.create'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a non-String payload value does not kill the subscription', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    // The payload is a Struct-decoded Map<String, dynamic>; a producer bug can
    // put any type behind a contract key. A hard cast here would throw and take
    // every subsequent event down with it.
    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {'toolCallId': 42, 'toolName': 'system.time'},
      ),
    );
    await tester.pump();

    expect(find.byType(ToolCallCard), findsNothing);

    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 2,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();

    expect(find.byType(ToolCallCard), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a stream error resolves cards still running', (tester) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'tool.call.completed',
        sequence: 2,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 3,
        payload: {'toolCallId': 'call_2', 'toolName': 'files.create'},
      ),
    );
    await tester.pump();
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    // The gRPC stream drops (disconnect / deadline / auth failure). No terminal
    // event can ever arrive for call_2 now.
    events.addError(StateError('stream dropped'));
    await tester.pump();

    expect(
      find.byType(CircularProgressIndicator),
      findsNothing,
      reason: 'no card may keep spinning once the stream is gone',
    );
    expect(find.byIcon(Icons.error_outline), findsOneWidget);
    expect(find.textContaining('connection lost'), findsOneWidget);
    // The already-resolved card is untouched.
    expect(find.byIcon(Icons.check_circle_outline), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a stream close resolves cards still running', (tester) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    // Not awaited: the close future only settles once the test pumps, so
    // awaiting it here would deadlock under fake async. The controller is sync,
    // so onDone has already been delivered.
    unawaited(events.close());
    await tester.pump();

    expect(find.byType(CircularProgressIndicator), findsNothing);
    expect(find.byIcon(Icons.error_outline), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('tool.call.completed updates the same card in place', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'tool.call.completed',
        sequence: 2,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();

    expect(find.byIcon(Icons.check_circle_outline), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);
    // Still exactly one card — updated in place, not duplicated.
    expect(find.text('system.time'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('tool.call.failed shows the error even without a prior start', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.failed',
        sequence: 1,
        payload: {
          'toolCallId': 'call_9',
          'toolName': 'files.create',
          'error': 'mcp_call_failed',
        },
      ),
    );
    await tester.pump();

    expect(find.textContaining('mcp_call_failed'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('assistant text after a tool call renders below the tool card '
      '(same messageId across the turn)', (tester) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    // The runtime loop reuses ONE assistantMessageId for pre-tool preamble
    // and the post-tool answer.
    events.add(
      _event(
        type: 'message.delta',
        sequence: 1,
        payload: {'messageId': 'msg_x', 'delta': 'Checking the time.'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 2,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'tool.call.completed',
        sequence: 3,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'message.delta',
        sequence: 4,
        payload: {'messageId': 'msg_x', 'delta': 'It is noon.'},
      ),
    );
    await tester.pump();

    final toolY = tester.getTopLeft(find.text('system.time')).dy;
    final answerY = tester.getTopLeft(find.text('It is noon.')).dy;
    expect(
      answerY,
      greaterThan(toolY),
      reason: 'the post-tool answer must render below the tool card',
    );
    // The preamble and the answer are distinct bubbles, not merged.
    expect(find.text('Checking the time.'), findsOneWidget);
    expect(find.text('It is noon.'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('tool cards render chronologically between message bubbles', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'message.delta',
        sequence: 1,
        payload: {'messageId': 'msg_a', 'delta': 'before'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 2,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'message.delta',
        sequence: 3,
        payload: {'messageId': 'msg_b', 'delta': 'after'},
      ),
    );
    await tester.pump();

    final firstBubbleY = tester.getTopLeft(find.text('before')).dy;
    final toolCardY = tester.getTopLeft(find.text('system.time')).dy;
    final secondBubbleY = tester.getTopLeft(find.text('after')).dy;
    expect(firstBubbleY, lessThan(toolCardY));
    expect(toolCardY, lessThan(secondBubbleY));

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('the event subscription opens only after history has loaded', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final gate = Completer<List<Message>>();
    final apiClient = _FakeApiClient()..messagesGate = gate;

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    // The stream replays the session from sequence 0, so nothing may be applied
    // before the client knows which messageIds history already covers.
    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();
    expect(find.byType(ToolCallCard), findsNothing);

    gate.complete([
      Message(
        messageId: 'msg_hist',
        role: 'user',
        content: 'earlier question',
        sequence: 1,
        createdAt: _fixedDate,
      ),
    ]);
    await tester.pump();
    await tester.pump();

    // Nothing buffered while history loaded is lost: the card appears below the
    // seeded history, and its terminal event still updates it in place.
    events.add(
      _event(
        type: 'tool.call.completed',
        sequence: 2,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();

    expect(find.text('earlier question'), findsOneWidget);
    expect(
      tester.getTopLeft(find.text('earlier question')).dy,
      lessThan(tester.getTopLeft(find.text('system.time')).dy),
    );
    expect(find.byIcon(Icons.check_circle_outline), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('tool events committed while history loads remain live', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final gate = Completer<List<Message>>();
    final apiClient = _FakeApiClient()
      ..messagesGate = gate
      ..latestEventSequence = 100;
    final eventSource = _FakeEventSource(events.stream);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: eventSource,
        ),
      ),
    );
    await tester.pump();

    apiClient.latestEventSequence = 101;
    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 101,
        payload: {'toolCallId': 'call_live', 'toolName': 'system.time'},
      ),
    );
    gate.complete(const []);
    await tester.pump();
    await tester.pump();

    expect(eventSource.lastSequence, 0);
    expect(find.byType(ToolCallCard), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets(
    'tool events for a completed run loaded after the watermark stay hidden',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);
      final gate = Completer<List<Message>>();
      final apiClient = _FakeApiClient()
        ..messagesGate = gate
        ..latestEventSequence = 100;

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: apiClient,
            eventSource: _FakeEventSource(events.stream),
          ),
        ),
      );
      await tester.pump();

      gate.complete([
        Message.fromJson({
          'id': 'msg_complete',
          'runId': 'run_complete',
          'role': 'assistant',
          'content': 'Finished answer',
          'sequence': 2,
          'createdAt': _fixedDate.toIso8601String(),
        }),
      ]);
      await tester.pump();
      await tester.pump();

      events.add(
        _event(
          type: 'tool.call.started',
          sequence: 101,
          runId: 'run_complete',
          payload: {'toolCallId': 'call_old', 'toolName': 'system.time'},
        ),
      );
      events.add(
        _event(
          type: 'tool.call.completed',
          sequence: 102,
          runId: 'run_complete',
          payload: {'toolCallId': 'call_old', 'toolName': 'system.time'},
        ),
      );
      await tester.pump();

      expect(find.text('Finished answer'), findsOneWidget);
      expect(
        find.byType(ToolCallCard),
        findsNothing,
        reason:
            'history and replay must not render two representations of the '
            'same completed run',
      );

      await tester.pumpWidget(const SizedBox.shrink());
      unawaited(events.close());
    },
  );

  testWidgets('a failed history load still opens the event subscription', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    // Routine for a local-first stack: the backend may be down, the token
    // stale, the deadline blown. History is the expendable surface here — the
    // live session is not.
    final apiClient = _FakeApiClient()
      ..messagesError = StateError('grpc unavailable');

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'message.delta',
        sequence: 2,
        payload: {'messageId': 'msg_a', 'delta': 'still streaming'},
      ),
    );
    await tester.pump();

    expect(
      find.byType(ToolCallCard),
      findsOneWidget,
      reason: 'a history failure must not suppress the live event stream',
    );
    expect(find.text('still streaming'), findsOneWidget);
    expect(
      find.textContaining('Earlier messages could not be loaded'),
      findsOneWidget,
      reason: 'a silently empty transcript looks identical to a fresh session',
    );
    // Readiness (round 5): a history failure is handled, not fatal — the
    // subscription above proves it — so it must free the composer exactly
    // like a clean load would. Refusing to send here would treat a
    // recoverable, already-signalled failure as a permanent dead end.
    final field = tester.widget<TextField>(find.byType(TextField));
    final button = tester.widget<IconButton>(find.byType(IconButton));
    expect(field.enabled, isTrue);
    expect(button.onPressed, isNotNull);
    expect(field.decoration?.hintText, 'Ask Turing...');

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a live tool call leaves no empty bubble above the card', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final apiClient = _FakeApiClient()
      ..initialMessages = [
        Message(
          messageId: 'msg_user',
          role: 'user',
          content: 'what time is it',
          sequence: 1,
          createdAt: _fixedDate,
        ),
        // Reopened mid-run: the assistant row exists but is still empty, so it
        // is adopted as the live bubble.
        Message(
          messageId: 'msg_live',
          role: 'assistant',
          content: '',
          sequence: 2,
          createdAt: _fixedDate,
        ),
      ];

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    // The turn's FIRST output is a tool call, not text: the create path seals
    // the adopted (still empty) bubble, orphaning it above the card.
    for (final type in ['tool.call.started', 'tool.call.completed']) {
      events.add(
        _event(
          type: type,
          sequence: 1,
          payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
        ),
      );
      await tester.pump();
    }
    events.add(
      _event(
        type: 'message.delta',
        sequence: 2,
        payload: {'messageId': 'msg_live', 'delta': 'It is noon.'},
      ),
    );
    await tester.pump();

    expect(find.text('It is noon.'), findsOneWidget);
    expect(
      // Scoped to the transcript: the composer's empty TextField also renders
      // an empty text node, and it is not what this asserts about.
      find.descendant(of: find.byType(ListView), matching: find.text('')),
      findsNothing,
      reason:
          'a sealed, never-filled bubble must not paint as an empty pill (or '
          'be announced as an empty text node) between the question and card',
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('replayed deltas do not duplicate completed history text', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final apiClient = _FakeApiClient()
      ..initialMessages = [
        Message(
          messageId: 'msg_user',
          role: 'user',
          content: 'what time is it',
          sequence: 1,
          createdAt: _fixedDate,
        ),
        Message(
          messageId: 'msg_asst',
          role: 'assistant',
          content: 'It is noon.',
          sequence: 2,
          createdAt: _fixedDate,
        ),
      ];

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();
    expect(find.text('It is noon.'), findsOneWidget);

    // Opening a session replays the whole persisted event log, including every
    // delta that produced the history above.
    events.add(
      _event(
        type: 'message.delta',
        sequence: 1,
        payload: {'messageId': 'msg_asst', 'delta': 'It is noon.'},
      ),
    );
    await tester.pump();

    expect(
      find.text('It is noon.'),
      findsOneWidget,
      reason: 'history must not be rendered a second time from its own replay',
    );

    // A genuinely new turn still streams normally.
    events.add(
      _event(
        type: 'message.delta',
        sequence: 2,
        payload: {'messageId': 'msg_next', 'delta': 'Fresh answer'},
      ),
    );
    await tester.pump();
    expect(find.text('Fresh answer'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('an unfinished assistant row keeps streaming into its bubble', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final apiClient = _FakeApiClient()
      ..initialMessages = [
        Message(
          messageId: 'msg_user',
          role: 'user',
          content: 'what time is it',
          sequence: 1,
          createdAt: _fixedDate,
        ),
        // The assistant row is inserted empty when the job is created, so a
        // session reopened mid-run carries it in history with no content.
        Message(
          messageId: 'msg_live',
          role: 'assistant',
          content: '',
          sequence: 2,
          createdAt: _fixedDate,
        ),
      ];

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    for (final delta in ['It is ', 'noon.']) {
      events.add(
        _event(
          type: 'message.delta',
          sequence: 1,
          payload: {'messageId': 'msg_live', 'delta': delta},
        ),
      );
      await tester.pump();
    }

    expect(
      find.text('It is noon.'),
      findsOneWidget,
      reason: 'deltas for an unfinished history row belong IN that bubble',
    );
    expect(
      // One bubble per history row (the question and the answer) and no third:
      // the empty row was filled, not left sitting above a duplicate.
      find.byType(ValueListenableBuilder<String>),
      findsNWidgets(2),
      reason: 'streaming into a history row must not open a second bubble',
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a dead stream is announced even with no tool card running', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    // No tool call is in flight, so resolving running cards signals nothing:
    // without a notice the user just sees prompts that never answer.
    events.addError(StateError('stream dropped'));
    await tester.pump();

    // Round 7, finding 4 extends the same notice text to the composer's own
    // hint/tooltip while `_streamEnded` holds (previously only
    // `_startupFailed` did this — see `_sessionNoticeText`'s doc), so an
    // unscoped `find.textContaining` now ambiguously matches both the
    // banner and the composer. Scope to the banner specifically, exactly as
    // the terminal-startup-failure tests already do.
    expect(
      _sessionNoticeText(
        'Connection to the session lost. Reopen the session to continue.',
      ),
      findsOneWidget,
    );

    // The subscription is not cancelled on error, so a later event proves the
    // stream is alive again and the notice must not linger.
    events.add(
      _event(
        type: 'message.delta',
        sequence: 1,
        payload: {'messageId': 'msg_a', 'delta': 'still here'},
      ),
    );
    await tester.pump();

    expect(
      _sessionNoticeText(
        'Connection to the session lost. Reopen the session to continue.',
      ),
      findsNothing,
    );
    expect(find.text('still here'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  // Composer guard (round 7, finding 1): the notice above already proves it
  // recovers, but nothing proved the *composer* also recovers with it. A
  // send while nothing is listening could never reach the user — so
  // `_streamEnded` must gate the composer exactly like `_startupFailed`
  // already does, and (unlike `_startupFailed`, which is permanent) must
  // stop gating it once a later event proves the stream is alive again.
  testWidgets(
    'a dropped stream disables the composer and a later event re-enables it',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: _FakeApiClient(),
            eventSource: _FakeEventSource(events.stream),
          ),
        ),
      );
      await tester.pump();

      final readyField = tester.widget<TextField>(find.byType(TextField));
      final readyButton = tester.widget<IconButton>(find.byType(IconButton));
      expect(
        readyField.enabled,
        isTrue,
        reason: 'sanity check: composer is ready before the drop',
      );
      expect(readyButton.onPressed, isNotNull);

      events.addError(StateError('stream dropped'));
      await tester.pump();

      // Round 7, finding 4 makes the composer's own hint/tooltip show this
      // same notice text while `_streamEnded` holds, so an unscoped
      // `find.textContaining` would ambiguously match both the banner and
      // the composer (see `_sessionNoticeText`'s doc) — scope to the banner.
      expect(
        _sessionNoticeText(
          'Connection to the session lost. Reopen the session to continue.',
        ),
        findsOneWidget,
      );
      final droppedField = tester.widget<TextField>(find.byType(TextField));
      final droppedButton = tester.widget<IconButton>(find.byType(IconButton));
      expect(
        droppedField.enabled,
        isFalse,
        reason:
            'sending now could never reach the user — nothing is '
            'currently listening to carry a reply back',
      );
      expect(droppedButton.onPressed, isNull);
      expect(
        droppedButton.tooltip,
        'Connection to the session lost. Reopen the session to continue.',
      );

      // The subscription is not cancelled on error, so a later event proves
      // the stream is alive again and the composer must re-enable with it.
      events.add(
        _event(
          type: 'message.delta',
          sequence: 1,
          payload: {'messageId': 'msg_a', 'delta': 'still here'},
        ),
      );
      await tester.pump();

      final recoveredField = tester.widget<TextField>(find.byType(TextField));
      final recoveredButton = tester.widget<IconButton>(
        find.byType(IconButton),
      );
      expect(
        recoveredField.enabled,
        isTrue,
        reason:
            'a screen unable to receive results must become able to again '
            'once a later event proves it is, exactly like the notice above',
      );
      expect(recoveredButton.onPressed, isNotNull);
      expect(recoveredButton.tooltip, 'Send');

      await tester.pumpWidget(const SizedBox.shrink());
      unawaited(events.close());
    },
  );

  testWidgets('tool.call.denied renders a denied card', (tester) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.denied',
        sequence: 1,
        payload: {'toolCallId': 'c1', 'toolName': 'files.create'},
      ),
    );
    await tester.pump();

    expect(find.byIcon(Icons.block), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('tool.call.started then failed updates the error in place', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': 'files.create'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'tool.call.failed',
        sequence: 2,
        payload: {
          'toolCallId': 'call_1',
          'toolName': 'files.create',
          'error': 'boom',
        },
      ),
    );
    await tester.pump();

    // The error is written before status notifies, so it appears on rebuild.
    expect(find.textContaining('boom'), findsOneWidget);
    expect(find.byIcon(Icons.error_outline), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a real failure replaces the connection-lost placeholder error', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': 'files.create'},
      ),
    );
    await tester.pump();

    // The stream errors: the card is resolved with the placeholder error. The
    // subscription is NOT cancelled (cancelOnError defaults to false), so the
    // real terminal event can still arrive afterwards.
    events.addError(StateError('stream dropped'));
    await tester.pump();
    expect(find.textContaining('connection lost'), findsOneWidget);

    events.add(
      _event(
        type: 'tool.call.failed',
        sequence: 2,
        payload: {
          'toolCallId': 'call_1',
          'toolName': 'files.create',
          'error': 'mcp_call_failed',
        },
      ),
    );
    await tester.pump();

    // The status is unchanged (failed -> failed), so only an error-carrying
    // notification can refresh the card.
    expect(find.textContaining('mcp_call_failed'), findsOneWidget);
    expect(
      find.textContaining('connection lost'),
      findsNothing,
      reason: 'the placeholder must not outlive the real failure reason',
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a late started adopts serverName without regressing the card', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    // Replay gap: the terminal event lands first, and only `tool.call.started`
    // is guaranteed to carry `serverName`.
    events.add(
      _event(
        type: 'tool.call.completed',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();
    expect(find.text('system'), findsNothing);

    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 2,
        payload: {
          'toolCallId': 'call_1',
          'toolName': 'system.time',
          'serverName': 'system',
        },
      ),
    );
    await tester.pump();

    expect(
      find.text('system'),
      findsOneWidget,
      reason: 'late metadata the client received must not be dropped',
    );
    // ...and the card still does not regress to a spinner.
    expect(find.byIcon(Icons.check_circle_outline), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a non-String message.delta payload does not kill the stream', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    // Same Struct-decoding hazard as the tool payloads: a hard cast here would
    // throw an uncaught zone error and drop the event.
    events.add(
      _event(
        type: 'message.delta',
        sequence: 1,
        payload: {'messageId': 42, 'delta': 7},
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'message.delta',
        sequence: 2,
        payload: {'messageId': 'msg_a', 'delta': 'still here'},
      ),
    );
    await tester.pump();

    expect(find.text('still here'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a non-String approvalId does not kill the stream', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'approval.requested',
        sequence: 1,
        payload: {'approvalId': 1, 'toolName': 'files.update'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'approval.consumed',
        sequence: 2,
        payload: {'approvalId': 1},
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'approval.requested',
        sequence: 3,
        payload: {
          'approvalId': 'appr_1',
          'toolName': 'files.update',
          'argsSummary': 'Update note.txt',
        },
      ),
    );
    await tester.pump();

    expect(find.text('Approval requested: files.update'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a stale replayed started does not regress a completed card', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    for (final type in ['tool.call.started', 'tool.call.completed']) {
      events.add(
        _event(
          type: type,
          sequence: 1,
          payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
        ),
      );
      await tester.pump();
    }
    // A duplicate/out-of-order 'started' arrives after the terminal state.
    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 3,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();

    expect(find.byIcon(Icons.check_circle_outline), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('an empty toolName still renders a non-empty label', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {'toolCallId': 'c1', 'toolName': ''},
      ),
    );
    await tester.pump();

    expect(find.text('tool'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a later real toolName replaces the placeholder label', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    // The first event the client sees carries no usable name, so the card is
    // created with the placeholder label.
    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': ''},
      ),
    );
    await tester.pump();
    expect(find.text('tool'), findsOneWidget);

    // A later well-formed event carries the real name: adopt it rather than
    // leaving the card (and its screen-reader label) permanently wrong.
    events.add(
      _event(
        type: 'tool.call.completed',
        sequence: 2,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();

    expect(find.text('system.time'), findsOneWidget);
    expect(find.text('tool'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a real toolName is never overwritten by the placeholder', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();

    // A terminal event omits the name (or carries a malformed one): the good
    // name already on the card wins.
    events.add(
      _event(
        type: 'tool.call.completed',
        sequence: 2,
        payload: {'toolCallId': 'call_1', 'toolName': 42},
      ),
    );
    await tester.pump();

    expect(find.text('system.time'), findsOneWidget);
    expect(find.text('tool'), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a late started adopts the real toolName after a terminal', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.completed',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': ''},
      ),
    );
    await tester.pump();
    expect(find.text('tool'), findsOneWidget);

    // The ignored (terminal states are final) late 'started' still carries the
    // only good metadata the client will ever see.
    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 2,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();

    expect(find.text('system.time'), findsOneWidget);
    expect(find.text('tool'), findsNothing);
    expect(find.byIcon(Icons.check_circle_outline), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('distinct toolCallIds render two independent cards', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': 'a.one'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 2,
        payload: {'toolCallId': 'call_2', 'toolName': 'b.two'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'tool.call.completed',
        sequence: 3,
        payload: {'toolCallId': 'call_1', 'toolName': 'a.one'},
      ),
    );
    await tester.pump();

    expect(find.text('a.one'), findsOneWidget);
    expect(find.text('b.two'), findsOneWidget);
    expect(find.byIcon(Icons.check_circle_outline), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets(
    'replayed tool calls of the newest finished turn are not re-rendered',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);
      // The backend stamps a turn's user AND assistant rows at enqueue time and
      // never restamps them, so a finished turn's messages are stamped BEFORE
      // its own tool events. History here is ONLY that one finished turn: any
      // filter that compares wall-clock times lets its replayed cards through.
      final turnStart = DateTime.parse('2026-05-10T00:00:00.000Z');
      final afterTurn = DateTime.parse('2026-05-10T00:00:09.000Z');
      final persisted = [
        _event(
          type: 'tool.call.started',
          sequence: 7,
          createdAt: afterTurn,
          payload: {'toolCallId': 'call_old', 'toolName': 'system.time'},
        ),
        _event(
          type: 'tool.call.completed',
          sequence: 8,
          createdAt: afterTurn,
          payload: {'toolCallId': 'call_old', 'toolName': 'system.time'},
        ),
      ];
      final apiClient = _FakeApiClient()
        ..initialEvents = persisted
        ..initialMessages = [
          Message(
            messageId: 'msg_u1',
            role: 'user',
            content: 'q1',
            sequence: 1,
            createdAt: turnStart,
          ),
          Message(
            messageId: 'msg_a1',
            role: 'assistant',
            content: 'answer1',
            sequence: 2,
            createdAt: turnStart,
          ),
        ];

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: apiClient,
            eventSource: _FakeEventSource(events.stream),
          ),
        ),
      );
      await tester.pump();

      // Reopening replays the WHOLE persisted log.
      for (final event in persisted) {
        events.add(event);
        await tester.pump();
      }

      expect(
        find.byType(ToolCallCard),
        findsNothing,
        reason:
            'a tool call the persisted log already holds is finished business; '
            'appending its card would place it below the answer it belongs to',
      );
      expect(find.text('answer1'), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      unawaited(events.close());
    },
  );

  testWidgets('replayed tool calls do not disturb a still-streaming turn', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final earlier = DateTime.parse('2026-05-10T00:00:00.000Z');
    final later = DateTime.parse('2026-05-10T00:05:00.000Z');
    final apiClient = _FakeApiClient()
      ..initialEvents = [
        _event(
          type: 'tool.call.started',
          sequence: 1,
          createdAt: earlier,
          payload: {'toolCallId': 'call_old', 'toolName': 'system.time'},
        ),
        _event(
          type: 'tool.call.completed',
          sequence: 2,
          createdAt: earlier,
          payload: {'toolCallId': 'call_old', 'toolName': 'system.time'},
        ),
      ]
      ..initialMessages = [
        Message(
          messageId: 'msg_u1',
          role: 'user',
          content: 'q1',
          sequence: 1,
          createdAt: earlier,
        ),
        // The runtime reuses one assistantMessageId for a turn's preamble AND
        // its post-tool answer, so a finished tool-using turn persists as a
        // single, complete assistant message.
        Message(
          messageId: 'msg_a1',
          role: 'assistant',
          content: 'answer1',
          sequence: 2,
          createdAt: earlier,
        ),
        Message(
          messageId: 'msg_u2',
          role: 'user',
          content: 'q2',
          sequence: 3,
          createdAt: later,
        ),
        // Reopened mid-run: the assistant row exists but is still empty.
        Message(
          messageId: 'msg_live',
          role: 'assistant',
          content: '',
          sequence: 4,
          createdAt: later,
        ),
      ];

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    // Reopening replays the WHOLE persisted log, so turn 1's tool events arrive
    // again even though its text is already fully on screen.
    for (var sequence = 1; sequence <= 2; sequence++) {
      events.add(
        _event(
          type: sequence == 1 ? 'tool.call.started' : 'tool.call.completed',
          sequence: sequence,
          createdAt: earlier,
          payload: {'toolCallId': 'call_old', 'toolName': 'system.time'},
        ),
      );
      await tester.pump();
    }

    expect(
      find.byType(ToolCallCard),
      findsNothing,
      reason:
          'a tool call from a turn history already renders is finished '
          'business; appending its card would attribute it to the newest turn',
    );

    // The live turn still streams into the adopted (empty) history row.
    events.add(
      _event(
        type: 'message.delta',
        sequence: 3,
        payload: {'messageId': 'msg_live', 'delta': 'It is noon.'},
      ),
    );
    await tester.pump();

    expect(find.text('It is noon.'), findsOneWidget);
    expect(
      find.byType(ValueListenableBuilder<String>),
      findsNWidgets(4),
      reason:
          'the replayed tool call must not orphan the adopted bubble into '
          'an empty pill with a duplicate below it',
    );
    expect(
      tester.getTopLeft(find.text('It is noon.')).dy,
      greaterThan(tester.getTopLeft(find.text('q2')).dy),
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('a tool call past the replay watermark still renders', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final apiClient = _FakeApiClient()
      ..initialEvents = [
        _event(
          type: 'message.delta',
          sequence: 4,
          payload: {'messageId': 'msg_a1', 'delta': 'hi'},
        ),
      ]
      ..initialMessages = [
        Message(
          messageId: 'msg_user',
          role: 'user',
          content: 'what time is it',
          sequence: 1,
          createdAt: _fixedDate,
        ),
      ];

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 5,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();

    expect(
      find.byType(ToolCallCard),
      findsOneWidget,
      reason: 'suppressing replayed events must not silence live tool calls',
    );
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('replayed tool calls past a truncated event page stay hidden', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final apiClient = _FakeApiClient()
      ..initialEvents = [
        _event(
          type: 'message.delta',
          sequence: 500,
          payload: {'messageId': 'msg_old', 'delta': 'old'},
        ),
      ]
      ..latestEventSequence = 1000;

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 700,
        payload: {'toolCallId': 'call_old', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();

    expect(
      find.byType(ToolCallCard),
      findsNothing,
      reason:
          'latest_sequence must suppress persisted events beyond the first page',
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('an unreadable event tail does not replay stale tool events', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final apiClient = _FakeApiClient()
      ..eventsError = const TuringApiException(
        code: 'UNAVAILABLE',
        message: 'no backend',
      )
      ..initialMessages = [
        Message(
          messageId: 'msg_user',
          role: 'user',
          content: 'what time is it',
          sequence: 1,
          createdAt: _fixedDate,
        ),
      ];
    final eventSource = _FakeEventSource(events.stream);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: eventSource,
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();

    expect(eventSource.connectCount, 0);
    expect(find.byType(ToolCallCard), findsNothing);
    expect(
      _sessionNoticeText(
        'Connection to the session lost. Reopen the session to continue.',
      ),
      findsOneWidget,
      reason:
          'without a replay boundary the client cannot distinguish stale tool '
          'events from live ones',
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  // Startup race (round 4): `_loadReplayWatermark` used to be awaited with
  // nothing stopping a submission from landing while it was still in
  // flight. A run submitted in that window can terminally fail or cancel
  // before the watermark resolves, and the eventual watermark — which
  // covers everything persisted up to that moment, including the
  // just-submitted run's own terminal event — would then make
  // `_isHistoricalRunEvent` treat that live event as replay and silently
  // swallow it. The composer must refuse input until the boundary is fixed,
  // closing the window structurally instead of exempting the run after the
  // fact.
  testWidgets(
    'the composer stays disabled while the replay watermark is loading, '
    'then enables once it resolves',
    (tester) async {
      // Semantics enabled for this test (round 7, finding 4): the loading
      // control's accessibility-tree tooltip must be checked directly, not
      // inferred from the widget property alone — `IconButton.tooltip` and
      // `Semantics.tooltip` happen to be the same value today, but only
      // reading the actual accessibility tree proves that, rather than
      // assuming it.
      final handle = tester.ensureSemantics();
      final events = StreamController<TuringEvent>(sync: true);
      final gate = Completer<TuringEventPage>();
      final apiClient = _FakeApiClient()..eventsGate = gate;

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: apiClient,
            eventSource: _FakeEventSource(events.stream),
          ),
        ),
      );
      await tester.pump();

      final loadingField = tester.widget<TextField>(find.byType(TextField));
      final loadingButton = tester.widget<IconButton>(find.byType(IconButton));
      expect(
        loadingField.enabled,
        isFalse,
        reason:
            'a submission here could complete before the boundary is fixed, '
            'and the eventual watermark would then cover its own terminal '
            'event',
      );
      expect(loadingButton.onPressed, isNull);
      expect(
        loadingField.decoration?.hintText,
        'Loading session...',
        reason:
            'the composer being unusable must also be visible, not just '
            'unresponsive',
      );
      expect(
        loadingButton.tooltip,
        isNot('Send'),
        reason:
            'a control that cannot yet send anything must not claim it '
            'can — "Send" is only truthful once startup is actually ready',
      );
      expect(
        tester.getSemantics(find.byType(Tooltip)),
        matchesSemantics(
          // Hardcoded (not `loadingButton.tooltip`) so this assertion can
          // independently fail if the loading copy regresses to some other
          // non-"Send" string — deriving the expectation from the widget
          // under test would let any such regression pass silently.
          tooltip: 'Loading session...',
          isButton: true,
          hasEnabledState: true,
        ),
        reason:
            'IconButton.tooltip drives Semantics.tooltip, so the '
            'accessibility tree must be checked directly — a screen '
            'reader user relies on that tree, not on the widget property',
      );
      expect(
        find.byType(CircularProgressIndicator),
        findsOneWidget,
        reason:
            'the send icon swaps for a spinner while disabled, matching '
            'the existing loading affordance in session_list_screen.dart',
      );
      expect(find.byIcon(Icons.send), findsNothing);

      gate.complete(const TuringEventPage(events: [], latestSequence: 0));
      await tester.pump();
      await tester.pump();

      final readyField = tester.widget<TextField>(find.byType(TextField));
      final readyButton = tester.widget<IconButton>(find.byType(IconButton));
      expect(readyField.enabled, isTrue);
      expect(readyButton.onPressed, isNotNull);
      expect(readyField.decoration?.hintText, 'Ask Turing...');
      expect(readyButton.tooltip, 'Send');
      expect(
        tester.getSemantics(find.byType(Tooltip)),
        matchesSemantics(
          tooltip: 'Send',
          isButton: true,
          hasEnabledState: true,
          isEnabled: true,
          isFocusable: true,
          hasTapAction: true,
          hasFocusAction: true,
        ),
        reason: 'once ready, the accessibility tree must say "Send" too',
      );
      expect(find.byIcon(Icons.send), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsNothing);

      await tester.pumpWidget(const SizedBox.shrink());
      unawaited(events.close());
      handle.dispose();
    },
  );

  testWidgets(
    'the widget can be disposed while the replay watermark load is still '
    'pending, before the composer is ever freed',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);
      final gate = Completer<TuringEventPage>();
      final apiClient = _FakeApiClient()..eventsGate = gate;

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: apiClient,
            eventSource: _FakeEventSource(events.stream),
          ),
        ),
      );
      await tester.pump();

      // The gate is never completed: `_start` is still suspended on
      // `await _loadReplayWatermark()` when the screen goes away. `_start`
      // must resume into a dead `State` without throwing, and the
      // now-orphaned gate must not leak into a later test.
      await tester.pumpWidget(const SizedBox.shrink());
      expect(tester.takeException(), isNull);

      gate.complete(const TuringEventPage(events: [], latestSequence: 0));
      await tester.pump();
      expect(tester.takeException(), isNull);

      unawaited(events.close());
    },
  );

  // Readiness (round 5, finding A): `_initializing` used to clear right after
  // `_loadReplayWatermark` settled, before `_loadInitialMessages` even
  // started. History is seeded by PREPENDING once `listMessages` resolves
  // (`_messages.insertAll(0, ...)` — see `_loadInitialMessages`), so a
  // message sent in that window is appended live and can then ALSO come back
  // from that very `listMessages` call: the same turn rendered twice, in the
  // wrong relative order. The composer must stay disabled until history has
  // settled AND the subscription is open, closing the window structurally
  // instead of reconciling a duplicate/misordered bubble after the fact.
  testWidgets('the composer stays disabled while history is loading after a '
      'successful watermark, then enables once the subscription opens', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);
    final gate = Completer<List<Message>>();
    final apiClient = _FakeApiClient()..messagesGate = gate;
    final eventSource = _FakeEventSource(events.stream);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: eventSource,
        ),
      ),
    );
    await tester.pump();

    final loadingField = tester.widget<TextField>(find.byType(TextField));
    final loadingButton = tester.widget<IconButton>(find.byType(IconButton));
    expect(
      loadingField.enabled,
      isFalse,
      reason:
          'history is seeded by prepending once listMessages resolves, so '
          'a send here could be duplicated by — or misordered against — '
          'the very persisted turn that same send produces',
    );
    expect(loadingButton.onPressed, isNull);
    expect(loadingField.decoration?.hintText, 'Loading session...');
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    expect(find.byIcon(Icons.send), findsNothing);
    expect(
      eventSource.connectCount,
      0,
      reason:
          'the subscription must not open before history has settled, '
          'mirroring the ordering `_loadInitialMessages`\'s own doc comment '
          'already requires',
    );

    gate.complete(const []);
    await tester.pump();
    await tester.pump();

    final readyField = tester.widget<TextField>(find.byType(TextField));
    final readyButton = tester.widget<IconButton>(find.byType(IconButton));
    expect(readyField.enabled, isTrue);
    expect(readyButton.onPressed, isNotNull);
    expect(readyField.decoration?.hintText, 'Ask Turing...');
    expect(find.byIcon(Icons.send), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);
    expect(eventSource.connectCount, 1);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  // Readiness (round 5, finding B): `_initializing` used to clear right
  // after `_loadReplayWatermark` settled — success OR failure — because
  // `_start` unconditionally freed the composer before even attempting
  // `_loadInitialMessages`. On the failure path that left an enabled
  // composer, backed by no subscription, for however long history took to
  // load next — `_handleStreamEnded` (and its visible notice) only ran
  // AFTER that history load finished. `_start` must check the watermark's
  // own outcome first and, on failure, raise the notice immediately without
  // waiting on history: there is no subscription for history to seed, or
  // for anything the composer sends to reach.
  //
  // Readiness (round 6, finding 1 + 2): this used to also assert the
  // composer stayed on its IN-PROGRESS copy ("Loading session...", a
  // spinner) forever on this path, and that `listMessages` was never even
  // called. Both were themselves bugs: a spinner and "Loading" claim work is
  // still happening when it never will again is untruthful, and skipping
  // history entirely throws away a transcript that `listMessages` could
  // still deliver even though the session can never go live. `_start` now
  // raises a TERMINAL failure state — composer disabled with copy
  // consistent with the notice below, no spinner — in the same synchronous
  // update as the notice, and only THEN makes a best-effort, read-only
  // history attempt. The never-completing gate below now proves that
  // attempt is made (`listMessagesCallCount` is 1) without either the
  // notice or the terminal composer state waiting on it to settle.
  testWidgets('a failed replay watermark load shows the connection-lost '
      'notice and a truthful terminal composer immediately, without waiting '
      'on the read-only history attempt', (tester) async {
    final events = StreamController<TuringEvent>(sync: true);
    // Never completed. If the notice or terminal composer state waited on
    // this, this test would fail (or hang) instead of passing quickly.
    final neverSettles = Completer<List<Message>>();
    final apiClient = _FakeApiClient()
      ..eventsError = const TuringApiException(
        code: 'UNAVAILABLE',
        message: 'no backend',
      )
      ..messagesGate = neverSettles;
    final eventSource = _FakeEventSource(events.stream);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: eventSource,
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    expect(
      _sessionNoticeText(
        'Connection to the session lost. Reopen the session to continue.',
      ),
      findsOneWidget,
      reason:
          'the failure notice must not wait on a history load that has not '
          'settled yet',
    );
    final field = tester.widget<TextField>(find.byType(TextField));
    final button = tester.widget<IconButton>(find.byType(IconButton));
    expect(
      field.enabled,
      isFalse,
      reason:
          'no subscription will ever open on this failure path, so an '
          'enabled composer would send into a screen that can never show '
          'the result — the notice above is the signal, a live composer '
          'would not be',
    );
    expect(button.onPressed, isNull);
    expect(
      field.decoration?.hintText,
      'Connection to the session lost. Reopen the session to continue.',
      reason:
          'this is a TERMINAL failure, not progress — the copy must match '
          'the notice above, not still claim a session is loading',
    );
    expect(
      button.tooltip,
      'Connection to the session lost. Reopen the session to continue.',
      reason:
          'IconButton.tooltip also drives its accessibility-tree semantics '
          '(Semantics.tooltip — see framework Tooltip widget), so this is '
          'what makes the disabled send affordance truthful for assistive '
          'tech too, not just sighted users',
    );
    expect(
      find.byType(CircularProgressIndicator),
      findsNothing,
      reason:
          'nothing is in progress any more, so a spinner here would be '
          'untruthful',
    );
    expect(
      find.byIcon(Icons.send),
      findsOneWidget,
      reason: 'the send icon stays visible, just disabled — not a spinner',
    );
    expect(
      eventSource.connectCount,
      0,
      reason: 'a failed watermark must never open a subscription',
    );
    expect(
      apiClient.listMessagesCallCount,
      1,
      reason:
          'a read-only history attempt must still be made even though the '
          'watermark failed — a never-completing gate proves neither the '
          'notice nor the terminal composer state waits on it to settle',
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
    // Hygiene only: complete it so the Completer does not dangle into a
    // later test.
    neverSettles.complete(const []);
  });

  // Readiness (round 6, finding 2): the read-only history attempt above
  // must not be a no-op — if it resolves, its messages belong on screen
  // exactly like a normal history load's would.
  testWidgets(
    'a failed replay watermark load still renders the transcript once the '
    'read-only history attempt succeeds',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);
      final gate = Completer<List<Message>>();
      final apiClient = _FakeApiClient()
        ..eventsError = const TuringApiException(
          code: 'UNAVAILABLE',
          message: 'no backend',
        )
        ..messagesGate = gate;
      final eventSource = _FakeEventSource(events.stream);

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: apiClient,
            eventSource: eventSource,
          ),
        ),
      );
      await tester.pump();
      await tester.pump();

      expect(
        _sessionNoticeText(
          'Connection to the session lost. Reopen the session to continue.',
        ),
        findsOneWidget,
      );
      expect(find.text('what time is it'), findsNothing);

      gate.complete([
        Message(
          messageId: 'msg_user',
          role: 'user',
          content: 'what time is it',
          sequence: 1,
          createdAt: _fixedDate,
        ),
      ]);
      await tester.pump();
      await tester.pump();

      expect(
        find.text('what time is it'),
        findsOneWidget,
        reason:
            'a usable transcript must survive a watermark failure even '
            'though the session can never go live',
      );
      expect(
        _sessionNoticeText(
          'Connection to the session lost. Reopen the session to continue.',
        ),
        findsOneWidget,
        reason:
            'a successful history load must not clear a terminal '
            'startup failure — there is still no subscription',
      );
      final field = tester.widget<TextField>(find.byType(TextField));
      expect(
        field.enabled,
        isFalse,
        reason: 'history settling is not the same as startup succeeding',
      );
      expect(eventSource.connectCount, 0);

      await tester.pumpWidget(const SizedBox.shrink());
      unawaited(events.close());
    },
  );

  // Readiness (round 6, finding 2): the existing history-load-failure
  // handling (`_historyLoadFailed`) must still fire when the read-only
  // attempt this finding adds ALSO fails, stacking with — not replacing —
  // the connection-lost notice from the watermark failure itself.
  //
  // Round 7, finding 3: the notice text itself must also be
  // context-accurate. The normal history-failure copy claims "This session
  // is live from here on" — true when a subscription is open and only
  // history was lost, but false here: the watermark failure means NO
  // subscription will ever open, so nothing about this session is "live"
  // at all. `_startupFailed` distinguishes the two cases; this test pins
  // the corrected, subscription-accurate copy.
  testWidgets(
    'a failed replay watermark load preserves the history-load-failed '
    'notice when the read-only history attempt also fails',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);
      final apiClient = _FakeApiClient()
        ..eventsError = const TuringApiException(
          code: 'UNAVAILABLE',
          message: 'no backend',
        )
        ..messagesError = StateError('grpc unavailable');
      final eventSource = _FakeEventSource(events.stream);

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: apiClient,
            eventSource: eventSource,
          ),
        ),
      );
      await tester.pump();
      await tester.pump();

      expect(
        _sessionNoticeText(
          'Connection to the session lost. Reopen the session to continue.',
        ),
        findsOneWidget,
      );
      expect(
        _sessionNoticeText('Earlier messages could not be loaded.'),
        findsOneWidget,
        reason:
            'a history failure must still get its own visible handling even '
            'on a path that starts from a watermark failure',
      );
      expect(
        find.textContaining('This session is live from here on'),
        findsNothing,
        reason:
            'no subscription will ever open on this failure path — nothing '
            'about this session is "live", so the normal history-failure '
            'copy would be false here',
      );
      expect(apiClient.listMessagesCallCount, 1);
      expect(eventSource.connectCount, 0);

      await tester.pumpWidget(const SizedBox.shrink());
      unawaited(events.close());
    },
  );

  // Round 7, finding 3: the corrected copy above must be scoped to when a
  // subscription genuinely never opens — a history failure that happens
  // to occur while the subscription itself is fine must keep the ORIGINAL
  // "live from here on" claim, because that one is still true.
  testWidgets(
    'a history load failure keeps the "live from here on" notice when the '
    'subscription is open',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);
      final apiClient = _FakeApiClient()
        ..messagesError = StateError('grpc unavailable');
      final eventSource = _FakeEventSource(events.stream);

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: apiClient,
            eventSource: eventSource,
          ),
        ),
      );
      await tester.pump();
      await tester.pump();

      expect(
        _sessionNoticeText(
          'Earlier messages could not be loaded. This session is live from '
          'here on.',
        ),
        findsOneWidget,
        reason:
            'a subscription is genuinely open here, so this claim is true '
            'and must be preserved exactly as before',
      );
      final field = tester.widget<TextField>(find.byType(TextField));
      expect(
        field.enabled,
        isTrue,
        reason: 'a history failure alone must not disable a live composer',
      );
      expect(eventSource.connectCount, 1);

      await tester.pumpWidget(const SizedBox.shrink());
      unawaited(events.close());
    },
  );

  // Round 7, finding 3: the watermark is not the only path to
  // `_startupFailed` — the subscription itself can fail to open (see the
  // synchronous-throw/synchronous-onDone tests above) with the watermark
  // otherwise fine. The corrected copy must key off "no subscription will
  // ever exist" generally, not off the watermark specifically, so it must
  // also replace the false claim on THIS path.
  testWidgets(
    'a failed subscription open preserves the history-load-failed notice '
    'with context-accurate copy, not the watermark-failure path',
    (tester) async {
      final apiClient = _FakeApiClient()
        ..messagesError = StateError('grpc unavailable');
      final eventSource = _ImmediatelyTerminalEventSource(asError: false);

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: apiClient,
            eventSource: eventSource,
          ),
        ),
      );
      await tester.pump();
      await tester.pump();

      expect(
        _sessionNoticeText(
          'Connection to the session lost. Reopen the session to continue.',
        ),
        findsOneWidget,
      );
      expect(
        _sessionNoticeText('Earlier messages could not be loaded.'),
        findsOneWidget,
        reason:
            'a history failure must still get its own visible handling on '
            'this path to `_startupFailed` too, not just the '
            'watermark-failure one',
      );
      expect(
        find.textContaining('This session is live from here on'),
        findsNothing,
        reason:
            'the subscription failed to open here just as surely as on the '
            'watermark-failure path — this claim would be exactly as false',
      );
      expect(eventSource.connectCount, 1);

      await tester.pumpWidget(const SizedBox.shrink());
    },
  );

  // Disposal safety (round 6): the read-only history attempt this finding
  // adds is a NEW await point on the watermark-failure path — `_start` must
  // resume into a dead `State` after it without throwing, exactly like the
  // pre-existing watermark-load and history-load disposal tests already
  // guarantee for their own await points.
  testWidgets(
    'the widget can be disposed while the read-only history attempt after a '
    'failed watermark is still pending',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);
      final gate = Completer<List<Message>>();
      final apiClient = _FakeApiClient()
        ..eventsError = const TuringApiException(
          code: 'UNAVAILABLE',
          message: 'no backend',
        )
        ..messagesGate = gate;

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: apiClient,
            eventSource: _FakeEventSource(events.stream),
          ),
        ),
      );
      await tester.pump();
      await tester.pump();

      await tester.pumpWidget(const SizedBox.shrink());
      expect(tester.takeException(), isNull);

      gate.complete(const []);
      await tester.pump();
      expect(tester.takeException(), isNull);

      unawaited(events.close());
    },
  );

  // Subscription setup (round 6, finding 3): `TuringEventSource` is an
  // interface, and its contract allows `connect()` to fail eagerly instead
  // of only ever arriving later as an `onError` callback — nothing here
  // asserts that any particular real implementation actually behaves this
  // way, only that `_start` must survive it if one does. `_start` is
  // fire-and-forget (`unawaited` in `initState`), so an uncaught
  // synchronous throw here becomes an unhandled Future rejection — not a
  // hypothetical: this exact shape reliably fails a `flutter test` run
  // outright (`EXCEPTION CAUGHT BY FLUTTER TEST FRAMEWORK`) rather than
  // merely failing an assertion.
  testWidgets(
    'a synchronous exception from connect() during subscription setup is '
    'handled, not left as an unhandled Future rejection',
    (tester) async {
      final handle = tester.ensureSemantics();
      final eventSource = _ThrowingEventSource();

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: _FakeApiClient(),
            eventSource: eventSource,
          ),
        ),
      );
      await tester.pump();
      await tester.pump();

      expect(
        tester.takeException(),
        isNull,
        reason:
            'a synchronous connect() failure must be caught by `_start` '
            'itself, not escape as an unhandled Future rejection',
      );
      expect(eventSource.connectCount, 1);
      expect(
        _sessionNoticeText(
          'Connection to the session lost. Reopen the session to continue.',
        ),
        findsOneWidget,
      );
      final field = tester.widget<TextField>(find.byType(TextField));
      final button = tester.widget<IconButton>(find.byType(IconButton));
      expect(field.enabled, isFalse);
      expect(button.onPressed, isNull);
      expect(
        field.decoration?.hintText,
        'Connection to the session lost. Reopen the session to continue.',
      );
      expect(
        button.tooltip,
        'Connection to the session lost. Reopen the session to continue.',
        reason:
            'the disabled send affordance must be truthful in the '
            'accessibility tree too (IconButton.tooltip drives '
            'Semantics.tooltip), not just visually',
      );
      expect(
        tester.getSemantics(find.byType(Tooltip)),
        matchesSemantics(
          tooltip:
              'Connection to the session lost. Reopen the session to '
              'continue.',
          isButton: true,
          hasEnabledState: true,
        ),
        reason:
            'a startup failure must also be truthful in the accessibility '
            'tree itself, not only in the widget property a screen reader '
            'never inspects directly',
      );
      expect(find.byType(CircularProgressIndicator), findsNothing);

      await tester.pumpWidget(const SizedBox.shrink());
      handle.dispose();
    },
  );

  // Subscription setup (round 6, finding 3): `connect()` succeeding is not
  // the only synchronous-throw risk — `_openSubscription`'s `try` wraps the
  // whole `connect().listen(...)` chain specifically because `.listen()`
  // itself can also throw synchronously (e.g. a transport that validates
  // eagerly). A fix that only guarded `connect()` (an early `try` scoped
  // just around that call, with a separate unguarded `.listen()`) would pass
  // every other test here yet still crash on this one.
  testWidgets(
    'a synchronous exception from listen() during subscription setup is '
    'handled, not left as an unhandled Future rejection',
    (tester) async {
      final eventSource = _ThrowingOnListenEventSource();

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: _FakeApiClient(),
            eventSource: eventSource,
          ),
        ),
      );
      await tester.pump();
      await tester.pump();

      expect(
        tester.takeException(),
        isNull,
        reason:
            'a synchronous listen() failure must be caught by `_start` '
            'itself, not escape as an unhandled Future rejection',
      );
      expect(eventSource.connectCount, 1);
      expect(
        _sessionNoticeText(
          'Connection to the session lost. Reopen the session to continue.',
        ),
        findsOneWidget,
      );
      final field = tester.widget<TextField>(find.byType(TextField));
      final button = tester.widget<IconButton>(find.byType(IconButton));
      expect(field.enabled, isFalse);
      expect(button.onPressed, isNull);
      expect(
        button.tooltip,
        'Connection to the session lost. Reopen the session to continue.',
      );
      expect(find.byType(CircularProgressIndicator), findsNothing);

      await tester.pumpWidget(const SizedBox.shrink());
    },
  );

  // Subscription setup (round 6, finding 3): even once `listen` returns
  // without throwing, the returned subscription can be dead on arrival — a
  // source that is already erroring fires its terminal callback
  // SYNCHRONOUSLY, within the very `listen` call, before `_start` even
  // finishes assigning `_subscription`. The pre-fix (round 6) bug: `_start`
  // unconditionally cleared `_initializing` right after `listen` returned,
  // with nothing checking whether that same `listen` call had already
  // invoked `_handleStreamEnded` — so this exact scenario showed BOTH the
  // connection-lost notice AND a composer that claimed to be ready, backed
  // by a subscription that was already gone.
  //
  // Round 6's own fix over-corrected (round 7, finding 2): it treated ANY
  // synchronous `_streamEnded` — including one set by `onError` alone — as
  // a PERMANENT startup failure. That is wrong: `cancelOnError` is false
  // (see `_openSubscription`), so an `onError` does not end the stream,
  // synchronous or not, and a real event can still follow it. The composer
  // must still refuse a send for as long as the drop notice is showing,
  // exactly as round 6 already proved, but startup itself must NOT be
  // marked as terminally failed for it — proven below by a later event on
  // the very same subscription recovering the composer, which a genuinely
  // terminal failure (see the immediate-`onDone` test right after this
  // one) never could.
  testWidgets(
    'a stream that errors synchronously the instant it is listened to '
    'disables the composer but recovers once a later event arrives',
    (tester) async {
      final handle = tester.ensureSemantics();
      final eventSource = _ImmediatelyTerminalEventSource(asError: true);

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: _FakeApiClient(),
            eventSource: eventSource,
          ),
        ),
      );
      await tester.pump();
      await tester.pump();

      expect(tester.takeException(), isNull);
      expect(eventSource.connectCount, 1);
      expect(
        _sessionNoticeText(
          'Connection to the session lost. Reopen the session to continue.',
        ),
        findsOneWidget,
      );
      final droppedField = tester.widget<TextField>(find.byType(TextField));
      final droppedButton = tester.widget<IconButton>(find.byType(IconButton));
      expect(
        droppedField.enabled,
        isFalse,
        reason:
            'a send here could never reach the user — nothing is '
            'currently listening to carry its results back, even though '
            'the drop itself may still recover',
      );
      expect(droppedButton.onPressed, isNull);
      expect(
        droppedField.decoration?.hintText,
        'Connection to the session lost. Reopen the session to continue.',
      );
      expect(
        droppedButton.tooltip,
        'Connection to the session lost. Reopen the session to continue.',
        reason:
            'the disabled send affordance must be truthful in the '
            'accessibility tree too (IconButton.tooltip drives '
            'Semantics.tooltip), not just visually',
      );
      expect(
        tester.getSemantics(find.byType(Tooltip)),
        matchesSemantics(
          tooltip:
              'Connection to the session lost. Reopen the session to '
              'continue.',
          isButton: true,
          hasEnabledState: true,
        ),
        reason:
            'a dropped stream must also be truthful in the accessibility '
            'tree itself, not only in the widget property a screen reader '
            'never inspects directly',
      );
      expect(find.byType(CircularProgressIndicator), findsNothing);

      // The subscription is NOT cancelled on error (`cancelOnError` is
      // false), so the fake can still deliver a genuine event afterwards —
      // proving this was never a terminal startup failure to begin with.
      eventSource.pushLater(
        _event(
          type: 'message.delta',
          sequence: 1,
          payload: {'messageId': 'msg_a', 'delta': 'still here'},
        ),
      );
      await tester.pump();
      // A second pump, matching the loading→ready transition test above:
      // the focus-action semantics for the now-enabled button settles one
      // frame after the rebuild that flips `enabled`.
      await tester.pump();

      expect(
        _sessionNoticeText(
          'Connection to the session lost. Reopen the session to continue.',
        ),
        findsNothing,
        reason: 'a later event proves the stream recovered',
      );
      final recoveredField = tester.widget<TextField>(find.byType(TextField));
      final recoveredButton = tester.widget<IconButton>(
        find.byType(IconButton),
      );
      expect(
        recoveredField.enabled,
        isTrue,
        reason:
            'a synchronous onError alone must never be treated as a '
            'terminal startup failure — only a genuinely closed, or '
            'never-opened, subscription may permanently disable the '
            'composer',
      );
      expect(recoveredButton.onPressed, isNotNull);
      expect(recoveredField.decoration?.hintText, 'Ask Turing...');
      expect(recoveredButton.tooltip, 'Send');
      expect(
        tester.getSemantics(find.byType(Tooltip)),
        matchesSemantics(
          tooltip: 'Send',
          isButton: true,
          hasEnabledState: true,
          isEnabled: true,
          isFocusable: true,
          hasTapAction: true,
          hasFocusAction: true,
        ),
        reason:
            'recovery must be truthful in the accessibility tree too — a '
            'screen reader user relies on that tree, not the widget '
            'property, to know sending is possible again',
      );
      expect(find.text('still here'), findsOneWidget);

      await tester.pumpWidget(const SizedBox.shrink());
      handle.dispose();
    },
  );

  // Subscription setup (round 6, finding 3): the same dead-on-arrival race
  // as above, but signalled with an immediate `onDone` instead of an
  // `onError`. Unlike `onError` above — recoverable, because
  // `cancelOnError` is false and an error does not end the stream —
  // `onDone` is the stream contract's own guarantee that nothing more will
  // EVER arrive on this subscription. That difference is exactly why only
  // this shape may fail startup permanently (round 7, finding 2): no later
  // event could ever prove otherwise.
  testWidgets(
    'a stream that closes synchronously the instant it is listened to '
    'fails startup instead of enabling the composer',
    (tester) async {
      final eventSource = _ImmediatelyTerminalEventSource(asError: false);

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: _FakeApiClient(),
            eventSource: eventSource,
          ),
        ),
      );
      await tester.pump();
      await tester.pump();

      expect(tester.takeException(), isNull);
      expect(eventSource.connectCount, 1);
      expect(
        _sessionNoticeText(
          'Connection to the session lost. Reopen the session to continue.',
        ),
        findsOneWidget,
      );
      final field = tester.widget<TextField>(find.byType(TextField));
      final button = tester.widget<IconButton>(find.byType(IconButton));
      expect(field.enabled, isFalse);
      expect(button.onPressed, isNull);
      expect(
        button.tooltip,
        'Connection to the session lost. Reopen the session to continue.',
      );
      expect(find.byType(CircularProgressIndicator), findsNothing);

      await tester.pumpWidget(const SizedBox.shrink());
    },
  );

  // Defensive readiness guard (round 6, finding 4): the composer's
  // `enabled`/`onPressed` gating in `build()` is the primary defence, but
  // `TextField.onSubmitted` stays wired to `_sendMessage` regardless of
  // `enabled` — that flag only affects Flutter's own focus/input routing,
  // not whether the Dart callback itself still short-circuits. Invoking it
  // directly here bypasses the widget layer entirely, standing in for a
  // queued IME commit (captured before the field became disabled) or any
  // future programmatic caller. `_sendMessage` must refuse on its own.
  testWidgets(
    'a queued onSubmitted invocation cannot send while startup is still in '
    'progress',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);
      final gate = Completer<TuringEventPage>();
      final apiClient = _FakeApiClient()..eventsGate = gate;

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: apiClient,
            eventSource: _FakeEventSource(events.stream),
          ),
        ),
      );
      await tester.pump();

      final field = tester.widget<TextField>(find.byType(TextField));
      expect(
        field.enabled,
        isFalse,
        reason: 'sanity check: still mid-startup at this point',
      );
      field.controller!.text = 'go';
      field.onSubmitted!('go');
      await tester.pump();

      expect(
        apiClient.lastSentContent,
        isNull,
        reason: 'a send during startup must never reach the API client',
      );
      expect(
        find.descendant(of: find.byType(ListView), matching: find.text('go')),
        findsNothing,
        reason:
            'no local echo bubble may appear in the message list for a '
            'refused send — scoped to the ListView because the composer\'s '
            'own (un-cleared) TextField still legitimately shows "go"',
      );

      gate.complete(const TuringEventPage(events: [], latestSequence: 0));
      await tester.pump();
      await tester.pump();
      unawaited(events.close());
    },
  );

  testWidgets('a queued onSubmitted invocation cannot send after startup has '
      'terminally failed', (tester) async {
    final apiClient = _FakeApiClient()
      ..eventsError = const TuringApiException(
        code: 'UNAVAILABLE',
        message: 'no backend',
      );

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(const Stream.empty()),
        ),
      ),
    );
    await tester.pump();
    await tester.pump();

    final field = tester.widget<TextField>(find.byType(TextField));
    expect(
      field.enabled,
      isFalse,
      reason: 'sanity check: startup has failed terminally at this point',
    );
    field.controller!.text = 'go';
    field.onSubmitted!('go');
    await tester.pump();

    expect(
      apiClient.lastSentContent,
      isNull,
      reason:
          'a send after a terminal startup failure must never reach '
          'the API client',
    );
    expect(
      find.descendant(of: find.byType(ListView), matching: find.text('go')),
      findsNothing,
      reason:
          'no local echo bubble may appear in the message list for a '
          'refused send — scoped to the ListView because the composer\'s '
          'own (un-cleared) TextField still legitimately shows "go"',
    );
  });

  // Send guard (round 7, finding 1): `build()`'s `enabled`/`onPressed`
  // gating is the primary defence, but `_sendMessage` must refuse on its
  // own too — this bypasses the widget layer entirely (see the two
  // `onSubmitted` tests above), standing in for a queued IME commit
  // captured just before a post-ready drop. Unlike the two tests above,
  // this one is never `_initializing` nor `_startupFailed`: startup
  // completed successfully, and the drop that follows is recoverable, not
  // terminal — proving the guard keys off `_streamEnded` itself, not off
  // either of the two states it already covers.
  testWidgets(
    'a queued onSubmitted invocation cannot send while a live stream has '
    'since dropped',
    (tester) async {
      final events = StreamController<TuringEvent>(sync: true);
      final apiClient = _FakeApiClient();

      await tester.pumpWidget(
        MaterialApp(
          home: ChatScreen(
            sessionId: 'sess_1',
            apiClient: apiClient,
            eventSource: _FakeEventSource(events.stream),
          ),
        ),
      );
      await tester.pump();

      final readyField = tester.widget<TextField>(find.byType(TextField));
      expect(
        readyField.enabled,
        isTrue,
        reason: 'sanity check: composer is ready before the drop',
      );

      events.addError(StateError('stream dropped'));
      await tester.pump();

      final field = tester.widget<TextField>(find.byType(TextField));
      expect(
        field.enabled,
        isFalse,
        reason: 'sanity check: the drop above disabled the composer',
      );
      field.controller!.text = 'go';
      field.onSubmitted!('go');
      await tester.pump();

      expect(
        apiClient.lastSentContent,
        isNull,
        reason:
            'a send while nothing is listening to carry a reply back must '
            'never reach the API client, even though the drop is '
            'recoverable rather than a terminal startup failure',
      );
      expect(
        find.descendant(of: find.byType(ListView), matching: find.text('go')),
        findsNothing,
        reason:
            'no local echo bubble may appear in the message list for a '
            'refused send — scoped to the ListView because the composer\'s '
            'own (un-cleared) TextField still legitimately shows "go"',
      );

      unawaited(events.close());
    },
  );

  testWidgets('a run submitted right after the replay watermark resolves still '
      'renders its live cancellation', (tester) async {
    final events = StreamController<TuringEvent>(sync: true);
    final gate = Completer<TuringEventPage>();
    final apiClient = _FakeApiClient()..eventsGate = gate;

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: apiClient,
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    gate.complete(const TuringEventPage(events: [], latestSequence: 0));
    await tester.pump();
    await tester.pump();

    await tester.enterText(find.byType(TextField), 'go');
    await tester.tap(find.byIcon(Icons.send));
    await tester.pump();

    expect(apiClient.lastSentContent, 'go');

    // This run's terminal event arrives with the very next sequence after
    // the boundary the screen just fixed — the closest a live event can
    // get to that boundary — and must still render.
    events.add(
      _event(
        type: 'agent.run.cancelled',
        sequence: 1,
        payload: {'reason': 'client_cancelled'},
      ),
    );
    await tester.pump();

    expect(
      find.byType(RunCancelledCard),
      findsOneWidget,
      reason:
          'a run started after readiness must never be classified as '
          'replay of history that predates this screen',
    );
    expect(
      find.text('The run was cancelled before it could finish'),
      findsOneWidget,
    );

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('the same toolCallId in two runs renders two cards', (
    tester,
  ) async {
    final events = StreamController<TuringEvent>(sync: true);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: _FakeEventSource(events.stream),
        ),
      ),
    );
    await tester.pump();

    // A runtime that mints ids itself restarts the numbering every run, so
    // `call_1` of run 2 must not update run 1's card.
    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        runId: 'run_1',
        payload: {'toolCallId': 'call_1', 'toolName': 'a.one'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'tool.call.completed',
        sequence: 2,
        runId: 'run_1',
        payload: {'toolCallId': 'call_1', 'toolName': 'a.one'},
      ),
    );
    await tester.pump();
    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 3,
        runId: 'run_2',
        payload: {'toolCallId': 'call_1', 'toolName': 'b.two'},
      ),
    );
    await tester.pump();

    expect(find.byType(ToolCallCard), findsNWidgets(2));
    expect(find.text('a.one'), findsOneWidget);
    expect(find.text('b.two'), findsOneWidget);
    expect(
      find.byIcon(Icons.check_circle_outline),
      findsOneWidget,
      reason: 'run 1 stays Completed rather than regressing to a spinner',
    );
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

  testWidgets('an event delivered after dispose is ignored', (tester) async {
    final events = StreamController<TuringEvent>(sync: true);
    // Cancelling a gRPC-backed stream is asynchronous, so an event can still be
    // delivered after `dispose`. This source reproduces that: its subscription
    // keeps delivering after `cancel()`.
    final source = _UncancellableEventSource(events.stream);

    await tester.pumpWidget(
      MaterialApp(
        home: ChatScreen(
          sessionId: 'sess_1',
          apiClient: _FakeApiClient(),
          eventSource: source,
        ),
      ),
    );
    await tester.pump();

    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();
    expect(find.byType(ToolCallCard), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    // Unguarded, this throws 'setState() called after dispose()'.
    events.add(
      _event(
        type: 'tool.call.completed',
        sequence: 2,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    events.add(
      _event(
        type: 'message.delta',
        sequence: 3,
        payload: {'messageId': 'msg_a1', 'delta': 'late'},
      ),
    );
    await tester.pump();

    expect(tester.takeException(), isNull);
    unawaited(events.close());
  });
}

/// The `_SessionNotice` banner's own rendered text, scoped to exclude the
/// composer. Once startup fails terminally (`_startupFailed`), the
/// TextField's hint text becomes this exact same notice string by design
/// (round 6, finding 1 — the copy must be consistent, not just similar), so
/// an unscoped `find.text(message)` ambiguously matches both. `Icons.cloud_off`
/// renders nowhere else on this screen, so its enclosing `Row` is reliably
/// the banner's own, never the composer's.
Finder _sessionNoticeText(String message) => find.descendant(
  of: find.ancestor(
    of: find.byIcon(Icons.cloud_off),
    matching: find.byType(Row),
  ),
  matching: find.text(message),
);

final _fixedDate = DateTime.parse('2026-05-10T00:00:00.000Z');

/// Default stamp for events a test feeds as LIVE. Nothing branches on it — the
/// screen tells replay from live by SEQUENCE — but keeping it later than
/// [_fixedDate] (the stamp of seeded history) keeps the fixtures honest.
final _liveDate = DateTime.parse('2026-05-10T01:00:00.000Z');

TuringEvent _event({
  required String type,
  required int sequence,
  required Map<String, dynamic> payload,
  DateTime? createdAt,
  String runId = 'run_1',
}) {
  return TuringEvent(
    eventId: 'evt_$sequence',
    sessionId: 'sess_1',
    runId: runId,
    traceId: 'trace_1',
    sequence: sequence,
    type: type,
    createdAt: createdAt ?? _liveDate,
    payload: payload,
  );
}

class _FakeApiClient implements TuringApi {
  String? lastSentContent;
  String? lastModelProvider;

  /// When set, `listMessages` returns this future instead of resolving
  /// immediately, letting a test drive the history-load race explicitly.
  Completer<List<Message>>? messagesGate;

  /// When set, `listMessages` fails with it — the backend being unreachable is
  /// the normal case for a local-first stack, not an exotic one.
  Object? messagesError;
  List<Message> initialMessages = const [];

  /// How many times `listMessages` has been invoked. Lets a test prove a
  /// short-circuit skips the call entirely, not just that it never awaited
  /// the call's result.
  int listMessagesCallCount = 0;

  /// The event log already persisted when the screen opens. The screen reads
  /// its latest sequence to learn which events the subscription will REPLAY.
  List<TuringEvent> initialEvents = const [];
  int? latestEventSequence;

  /// When set, `listEvents` fails with it.
  Object? eventsError;

  /// When set, `listEvents` returns this future instead of resolving
  /// immediately, letting a test drive the replay-watermark startup race
  /// explicitly.
  Completer<TuringEventPage>? eventsGate;

  @override
  Future<Map<String, dynamic>> approveApproval(
    String approvalId, {
    String? comment,
  }) async {
    return {'approvalId': approvalId, 'status': 'approved'};
  }

  @override
  Future<Map<String, dynamic>> createSession({String? title}) async {
    return {'sessionId': 'sess_1', 'createdAt': '2026-05-10T00:00:00.000Z'};
  }

  @override
  Future<Map<String, dynamic>> denyApproval(
    String approvalId, {
    String? reason,
  }) async {
    return {'approvalId': approvalId, 'status': 'denied'};
  }

  @override
  Future<Map<String, dynamic>> getConfig() async {
    return {
      'enabledProviders': ['ollama'],
    };
  }

  @override
  Future<TuringEventPage> listEvents({
    required String sessionId,
    int? after,
    int limit = 500,
  }) {
    final error = eventsError;
    if (error != null) return Future.error(error);
    final gate = eventsGate;
    if (gate != null) return gate.future;
    var latest = latestEventSequence ?? 0;
    for (final event in initialEvents) {
      if (event.sequence > latest) latest = event.sequence;
    }
    return Future.value(
      TuringEventPage(events: initialEvents, latestSequence: latest),
    );
  }

  @override
  Future<List<Message>> listMessages({
    required String sessionId,
    int limit = 50,
    String? before,
  }) {
    listMessagesCallCount++;
    final error = messagesError;
    if (error != null) return Future.error(error);
    return messagesGate?.future ?? Future.value(initialMessages);
  }

  @override
  Future<List<Session>> listSessions({int limit = 50, String? after}) async {
    return const [];
  }

  @override
  Future<Map<String, dynamic>> sendMessage({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
  }) async {
    lastSentContent = content;
    lastModelProvider = modelProvider;
    return {
      'sessionId': sessionId,
      'userMessageId': 'msg_user',
      'assistantMessageId': 'msg_asst',
      'runId': 'run_1',
      'jobId': 'job_1',
      'traceId': 'trace_1',
      'status': 'queued',
    };
  }
}

class _FakeEventSource implements TuringEventSource {
  _FakeEventSource(this._events);

  final Stream<TuringEvent> _events;
  bool closed = false;
  int connectCount = 0;
  int? lastSequence;

  @override
  Stream<TuringEvent> connect({required String sessionId, int? lastSequence}) {
    connectCount++;
    this.lastSequence = lastSequence;
    return _events;
  }

  @override
  void close() {
    closed = true;
  }
}

/// Event source whose `connect()` throws SYNCHRONOUSLY instead of ever
/// producing a stream. [TuringEventSource] is an interface, and nothing
/// pinned in this repo proves any particular real implementation actually
/// fails this way — this fake exists only to prove `_openSubscription`
/// survives a setup failure the interface's contract permits, whether or
/// not a given implementation ever exercises it.
class _ThrowingEventSource implements TuringEventSource {
  int connectCount = 0;

  @override
  Stream<TuringEvent> connect({required String sessionId, int? lastSequence}) {
    connectCount++;
    throw StateError('connect() failed synchronously');
  }

  @override
  void close() {}
}

/// Event source whose `connect()` succeeds and returns a stream, but whose
/// `listen()` call itself throws SYNCHRONOUSLY — a distinct call site from
/// [_ThrowingEventSource], which fails one step earlier, in `connect()`
/// itself. `_openSubscription`'s `try` wraps the whole `connect().listen(...)`
/// chain precisely so both call sites are caught identically; this fake
/// proves the second one actually is, not just the first.
class _ThrowingOnListenEventSource implements TuringEventSource {
  int connectCount = 0;

  @override
  Stream<TuringEvent> connect({required String sessionId, int? lastSequence}) {
    connectCount++;
    return _ThrowingOnListenStream();
  }

  @override
  void close() {}
}

class _ThrowingOnListenStream extends Stream<TuringEvent> {
  @override
  StreamSubscription<TuringEvent> listen(
    void Function(TuringEvent event)? onData, {
    Function? onError,
    void Function()? onDone,
    bool? cancelOnError,
  }) {
    throw StateError('listen() failed synchronously');
  }
}

/// Event source whose returned stream fires a terminal callback
/// SYNCHRONOUSLY — within the very `listen` call, before `listen` even
/// returns a subscription. `asError` selects which shape:
///  - `true`: an immediate `onError`, with the controller left OPEN
///    afterward. A real `Stream` does not close itself just because it
///    errored once — `cancelOnError` defaults to false, and production
///    wires it that way too (see `_openSubscription`) — so this fake does
///    not either; [pushLater] lets a test prove the subscription really is
///    still alive and can deliver a genuine event afterwards.
///  - `false`: an immediate `onDone`, with no error at all — the
///    controller closes right there, so nothing can ever arrive on it
///    again. This is the only one of the two shapes that is actually
///    terminal.
class _ImmediatelyTerminalEventSource implements TuringEventSource {
  _ImmediatelyTerminalEventSource({required this.asError});

  final bool asError;
  int connectCount = 0;
  _ImmediatelyTerminalStream? _stream;

  @override
  Stream<TuringEvent> connect({required String sessionId, int? lastSequence}) {
    connectCount++;
    final stream = _ImmediatelyTerminalStream(asError: asError);
    _stream = stream;
    return stream;
  }

  /// Delivers a live event on the same controller after the synchronous
  /// `onError` above. Only meaningful when `asError` is true — the
  /// `asError: false` shape closes its controller immediately and can
  /// never accept another event.
  void pushLater(TuringEvent event) => _stream!.controller!.add(event);

  @override
  void close() {}
}

class _ImmediatelyTerminalStream extends Stream<TuringEvent> {
  _ImmediatelyTerminalStream({required this.asError});

  final bool asError;

  /// Exposed so [_ImmediatelyTerminalEventSource.pushLater] can add a later
  /// event on the exact controller `listen()` created below.
  StreamController<TuringEvent>? controller;

  @override
  StreamSubscription<TuringEvent> listen(
    void Function(TuringEvent event)? onData, {
    Function? onError,
    void Function()? onDone,
    bool? cancelOnError,
  }) {
    // A SYNC controller delivers to an already-attached listener
    // synchronously, during the very call that adds the event/closes the
    // controller — but only once a listener is already attached; attaching
    // the inner listener first, then emitting, is what makes the callback
    // fire before this override returns a subscription to its own caller.
    final controller = StreamController<TuringEvent>(sync: true);
    this.controller = controller;
    final subscription = controller.stream.listen(
      onData,
      onError: onError,
      onDone: onDone,
      cancelOnError: cancelOnError,
    );
    if (asError) {
      // Deliberately does NOT close: an error is not itself a done signal
      // (see the class doc above), so the controller stays open.
      controller.addError(StateError('synchronous recoverable error'));
    } else {
      controller.close();
    }
    return subscription;
  }
}

/// Event source whose subscription IGNORES `cancel()`, standing in for a
/// gRPC-backed stream whose cancellation is asynchronous and therefore does not
/// stop an event already on its way to the listener.
class _UncancellableEventSource implements TuringEventSource {
  _UncancellableEventSource(this._events);

  final Stream<TuringEvent> _events;

  @override
  Stream<TuringEvent> connect({required String sessionId, int? lastSequence}) =>
      _UncancellableStream(_events);

  @override
  void close() {}
}

class _UncancellableStream extends Stream<TuringEvent> {
  _UncancellableStream(this._source);

  final Stream<TuringEvent> _source;

  @override
  StreamSubscription<TuringEvent> listen(
    void Function(TuringEvent event)? onData, {
    Function? onError,
    void Function()? onDone,
    bool? cancelOnError,
  }) {
    return _UncancellableSubscription(
      _source.listen(
        onData,
        onError: onError,
        onDone: onDone,
        cancelOnError: cancelOnError,
      ),
    );
  }
}

class _UncancellableSubscription implements StreamSubscription<TuringEvent> {
  _UncancellableSubscription(this._inner);

  final StreamSubscription<TuringEvent> _inner;

  @override
  Future<void> cancel() async {}

  @override
  Future<E> asFuture<E>([E? futureValue]) => _inner.asFuture(futureValue);

  @override
  bool get isPaused => _inner.isPaused;

  @override
  void onData(void Function(TuringEvent event)? handleData) =>
      _inner.onData(handleData);

  @override
  void onDone(void Function()? handleDone) => _inner.onDone(handleDone);

  @override
  void onError(Function? handleError) => _inner.onError(handleError);

  @override
  void pause([Future<void>? resumeSignal]) => _inner.pause(resumeSignal);

  @override
  void resume() => _inner.resume();
}
