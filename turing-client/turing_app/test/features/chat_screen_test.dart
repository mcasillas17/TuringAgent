import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
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

  testWidgets('late history load does not wipe a live tool card', (
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

    // A live tool call starts and completes WHILE listMessages is still in
    // flight (history has not resolved yet).
    events.add(
      _event(
        type: 'tool.call.started',
        sequence: 1,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    // History resolves late.
    gate.complete([
      Message(
        messageId: 'msg_hist',
        role: 'user',
        content: 'earlier question',
        sequence: 0,
        createdAt: _fixedDate,
      ),
    ]);
    await tester.pump();

    // The live card survives the history seed, and its terminal event still
    // updates it in place rather than being orphaned.
    events.add(
      _event(
        type: 'tool.call.completed',
        sequence: 2,
        payload: {'toolCallId': 'call_1', 'toolName': 'system.time'},
      ),
    );
    await tester.pump();

    expect(find.text('earlier question'), findsOneWidget);
    expect(find.byIcon(Icons.check_circle_outline), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);

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
}

final _fixedDate = DateTime.parse('2026-05-10T00:00:00.000Z');

TuringEvent _event({
  required String type,
  required int sequence,
  required Map<String, dynamic> payload,
}) {
  return TuringEvent(
    eventId: 'evt_$sequence',
    sessionId: 'sess_1',
    runId: 'run_1',
    traceId: 'trace_1',
    sequence: sequence,
    type: type,
    createdAt: DateTime.parse('2026-05-10T00:00:00.000Z'),
    payload: payload,
  );
}

class _FakeApiClient implements TuringApi {
  String? lastSentContent;
  String? lastModelProvider;

  /// When set, `listMessages` returns this future instead of resolving
  /// immediately, letting a test drive the history-load race explicitly.
  Completer<List<Message>>? messagesGate;
  List<Message> initialMessages = const [];

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
  Future<List<TuringEvent>> listEvents({
    required String sessionId,
    int? after,
    int limit = 500,
  }) async {
    return const [];
  }

  @override
  Future<List<Message>> listMessages({
    required String sessionId,
    int limit = 50,
    String? before,
  }) {
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

  @override
  Stream<TuringEvent> connect({required String sessionId, int? lastSequence}) =>
      _events;

  @override
  void close() {
    closed = true;
  }
}
