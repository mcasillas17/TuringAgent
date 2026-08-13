import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
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
      find.text('The agent reported a step it could not describe.'),
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
      find.text('The agent reported a step it could not describe.'),
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

    expect(
      find.textContaining('Connection to the session lost'),
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

    expect(find.textContaining('Connection to the session lost'), findsNothing);
    expect(find.text('still here'), findsOneWidget);

    await tester.pumpWidget(const SizedBox.shrink());
    unawaited(events.close());
  });

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
      find.text(
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

  /// The event log already persisted when the screen opens. The screen reads
  /// its latest sequence to learn which events the subscription will REPLAY.
  List<TuringEvent> initialEvents = const [];
  int? latestEventSequence;

  /// When set, `listEvents` fails with it.
  Object? eventsError;

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
