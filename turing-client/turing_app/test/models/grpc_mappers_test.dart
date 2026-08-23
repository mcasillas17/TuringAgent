import 'package:fixnum/fixnum.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/generated/turing/v1/automations.pb.dart'
    as automationpb;
import 'package:turing_flutter_app/generated/turing/v1/chat.pb.dart';
import 'package:turing_flutter_app/generated/turing/v1/common.pb.dart'
    as commonpb;
import 'package:turing_flutter_app/generated/turing/v1/events.pb.dart'
    as eventpb;
import 'package:turing_flutter_app/generated/turing/v1/sessions.pb.dart'
    as sessionpb;
import 'package:turing_flutter_app/generated/google/protobuf/timestamp.pb.dart'
    as timestamppb;
import 'package:turing_flutter_app/models/grpc_mappers.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/session_page.dart';
import 'package:turing_flutter_app/generated/google/protobuf/struct.pb.dart'
    as structpb;
import 'package:turing_flutter_app/models/run_lifecycle.dart';
import 'package:turing_flutter_app/models/run_state.dart';

void main() {
  // Guards the tool-call UI: the chat screen switches on these dotted strings,
  // and its switch has no default, so a mapping regression would silently drop
  // every tool-call event instead of failing loudly.
  test('maps TOOL_CALL_* event types to their dotted strings', () {
    expect(
      GrpcMappers.eventTypeToString(
        eventpb.TuringEventType.TURING_EVENT_TYPE_TOOL_CALL_STARTED,
      ),
      'tool.call.started',
    );
    expect(
      GrpcMappers.eventTypeToString(
        eventpb.TuringEventType.TURING_EVENT_TYPE_TOOL_CALL_COMPLETED,
      ),
      'tool.call.completed',
    );
    expect(
      GrpcMappers.eventTypeToString(
        eventpb.TuringEventType.TURING_EVENT_TYPE_TOOL_CALL_FAILED,
      ),
      'tool.call.failed',
    );
    expect(
      GrpcMappers.eventTypeToString(
        eventpb.TuringEventType.TURING_EVENT_TYPE_TOOL_CALL_DENIED,
      ),
      'tool.call.denied',
    );
  });

  test('maps AGENT_RUN_STEP to the chat event string', () {
    expect(
      GrpcMappers.eventTypeToString(
        eventpb.TuringEventType.TURING_EVENT_TYPE_AGENT_RUN_STEP,
      ),
      'agent.run.step',
    );
  });

  test('maps SESSION_UPDATED to the session event string', () {
    expect(
      GrpcMappers.eventTypeToString(
        eventpb.TuringEventType.TURING_EVENT_TYPE_SESSION_UPDATED,
      ),
      'session.updated',
    );
  });

  test('maps SESSION_DELETED to the terminal session event string', () {
    expect(
      GrpcMappers.eventTypeToString(
        eventpb.TuringEventType.TURING_EVENT_TYPE_SESSION_DELETED,
      ),
      'session.deleted',
    );
  });

  test('maps automation occurrence failure fields', () {
    final model = GrpcMappers.automationToModel(
      automationpb.Automation(
        lastOccurrenceFailureCode: 'remote_egress_configuration_invalid',
        lastOccurrenceFailedAt: timestamppb.Timestamp(
          seconds: Int64(1770000000),
          nanos: 123,
        ),
      ),
    );

    expect(
      model.lastOccurrenceFailureCode,
      'remote_egress_configuration_invalid',
    );
    expect(
      model.lastOccurrenceFailedAt,
      DateTime.fromMillisecondsSinceEpoch(1770000000000, isUtc: true).toLocal(),
    );
  });

  test('preserves session timestamp nanoseconds for ordering', () {
    final earlier = GrpcMappers.sessionToModel(
      sessionpb.Session(
        sessionId: 'sess_z',
        status: 'active',
        updatedAt: timestamppb.Timestamp(seconds: Int64(1), nanos: 100),
      ),
    );
    final later = GrpcMappers.sessionToModel(
      sessionpb.Session(
        sessionId: 'sess_a',
        status: 'active',
        updatedAt: timestamppb.Timestamp(seconds: Int64(1), nanos: 900),
      ),
    );

    expect(earlier.updatedAt, later.updatedAt);
    expect(earlier.updatedAtNanoseconds, 1000000100);
    expect(later.updatedAtNanoseconds, 1000000900);
  });

  test('maps archived session pages with exact cursor and nanoseconds', () {
    final page = GrpcMappers.sessionPageToModel(
      sessionpb.ListSessionsResponse(
        sessions: [
          sessionpb.Session(
            sessionId: 'sess_archived',
            title: 'Archived',
            status: 'archived',
            updatedAt: timestamppb.Timestamp(
              seconds: Int64(1770000000),
              nanos: 1,
            ),
          ),
        ],
        page: commonpb.PageResponse(nextCursor: 'cursor-next'),
      ),
    );

    expect(page, isA<SessionPage>());
    expect(page.sessions.single.status, SessionStatus.archived);
    expect(page.nextCursor, 'cursor-next');
    expect(page.sessions.single.updatedAtNanoseconds, 1770000000000000001);
  });

  test('maps token deltas into assistant message content', () {
    final event = ChatStreamEvent(
      sessionId: 'sess_1',
      runId: 'run_1',
      traceId: 'trace_1',
      sequence: Int64(42),
      tokenDelta: TokenDelta(messageId: 'msg_2', delta: 'Hel'),
    );

    final mapped = GrpcMappers.chatStreamEventToTuringEvent(event);

    expect(mapped.type, 'message.delta');
    expect(mapped.eventId, 'stream:run_1:42');
    expect(mapped.sequence, 42);
    expect(mapped.payload['messageId'], 'msg_2');
    expect(mapped.payload['delta'], 'Hel');
  });

  test('maps persisted live tool events with the frozen payload keys', () {
    final event = ChatStreamEvent(
      sessionId: 'sess_1',
      runId: 'run_1',
      traceId: 'trace_1',
      sequence: Int64(43),
      persistedEvent: eventpb.TuringEvent(
        type: eventpb.TuringEventType.TURING_EVENT_TYPE_TOOL_CALL_STARTED,
        payload: structpb.Struct(
          fields: <String, structpb.Value>{
            'toolCallId': structpb.Value(stringValue: 'call_1'),
            'toolName': structpb.Value(stringValue: 'system.time'),
            'serverName': structpb.Value(stringValue: 'system'),
          }.entries,
        ),
      ),
    );

    final mapped = GrpcMappers.chatStreamEventToTuringEvent(event);

    expect(mapped.type, 'tool.call.started');
    expect(mapped.payload['toolCallId'], 'call_1');
    expect(mapped.payload['toolName'], 'system.time');
    expect(mapped.payload['serverName'], 'system');
  });

  test('chat persisted state changed maps type and semantic run state', () {
    final mapped = GrpcMappers.chatStreamEventToTuringEvent(
      ChatStreamEvent(
        sessionId: 'sess_1',
        runId: 'run_1',
        sequence: Int64(44),
        runStateChanged: RunStateChanged(
          runState: commonpb.RunState(
            runId: 'run_1',
            stateVersion: Int64(7),
            stateUpdatedAt: timestamppb.Timestamp(),
          ),
        ),
      ),
    );

    expect(mapped.type, 'agent.run.state_changed');
    expect(mapped.runState?.runId, 'run_1');
    expect(mapped.runState?.stateVersion, 7);
    expect(mapped.runState?.lifecycle, RunLifecycle.unknown);
    expect(mapped.runState?.outcomeReason, RunOutcomeReason.unknown);
    expect(mapped.payload, isEmpty);
  });

  test('event service state changed maps type and semantic run state', () {
    final mapped = GrpcMappers.turingEventToTuringEvent(
      eventpb.TuringEvent(
        type: eventpb.TuringEventType.TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED,
        runState: commonpb.RunState(
          runId: 'run_1',
          stateVersion: Int64(7),
          stateUpdatedAt: timestamppb.Timestamp(),
        ),
        payload: structpb.Struct(
          fields: <String, structpb.Value>{
            'outcomeReason': structpb.Value(stringValue: 'provider_failure'),
          }.entries,
        ),
      ),
    );

    expect(mapped.type, 'agent.run.state_changed');
    expect(mapped.runState?.runId, 'run_1');
    expect(mapped.runState?.stateVersion, 7);
    expect(mapped.runState?.lifecycle, RunLifecycle.unknown);
    expect(mapped.runState?.outcomeReason, RunOutcomeReason.unknown);
    expect(mapped.payload, {'outcomeReason': 'provider_failure'});
  });

  // A server that types an event as state-changed without filling the typed
  // RunState still carries meaning in its persisted Struct. Fabricating
  // {'runId': '', 'stateVersion': 0} would invent an authoritative-looking run
  // identity and version that nobody reported, and discard the real payload.
  test('preserves the persisted payload when run state is absent', () {
    final mapped = GrpcMappers.turingEventToTuringEvent(
      eventpb.TuringEvent(
        type: eventpb.TuringEventType.TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED,
        payload: structpb.Struct(
          fields: <String, structpb.Value>{
            'runId': structpb.Value(stringValue: 'run_1'),
            'note': structpb.Value(stringValue: 'legacy projection'),
          }.entries,
        ),
      ),
    );

    expect(mapped.type, 'agent.run.state_changed');
    expect(mapped.runState, isNull);
    expect(mapped.payload, {'runId': 'run_1', 'note': 'legacy projection'});
  });

  // The RunState allowlist is scoped to state-changed events by type, not just
  // by presence. A failure event may carry a RunState alongside its own
  // persisted Struct; collapsing it to {runId, stateVersion} would erase the
  // error detail the failure view renders.
  test('preserves the persisted payload for a non state-changed event', () {
    final mapped = GrpcMappers.turingEventToTuringEvent(
      eventpb.TuringEvent(
        type: eventpb.TuringEventType.TURING_EVENT_TYPE_AGENT_RUN_FAILED,
        runState: commonpb.RunState(
          runId: 'run_1',
          stateVersion: Int64(7),
          stateUpdatedAt: timestamppb.Timestamp(),
        ),
        payload: structpb.Struct(
          fields: <String, structpb.Value>{
            'errorCode': structpb.Value(stringValue: 'provider_failure'),
            'message': structpb.Value(stringValue: 'upstream timeout'),
          }.entries,
        ),
      ),
    );

    expect(mapped.type, 'agent.run.failed');
    expect(mapped.runState?.stateVersion, 7);
    expect(mapped.payload, {
      'errorCode': 'provider_failure',
      'message': 'upstream timeout',
    });
  });

  test('maps a run state changed chat event with no run state to empty', () {
    final mapped = GrpcMappers.chatStreamEventToTuringEvent(
      ChatStreamEvent(
        sessionId: 'sess_1',
        runId: 'run_1',
        sequence: Int64(45),
        runStateChanged: RunStateChanged(),
      ),
    );

    expect(mapped.type, 'agent.run.state_changed');
    expect(mapped.runState, isNull);
    expect(mapped.payload, isEmpty);
  });

  test('absent message run state remains neutral legacy absence', () {
    final mapped = GrpcMappers.messageToModel(
      commonpb.Message(
        messageId: 'msg_assistant',
        role: commonpb.MessageRole.MESSAGE_ROLE_ASSISTANT,
      ),
    );

    expect(mapped.runState, isNull);
  });

  test('unknown event type never becomes a raw numeric label', () {
    final mapped = GrpcMappers.turingEventToTuringEvent(
      eventpb.TuringEvent.fromBuffer(const [0x30, 0x7f]),
    );

    expect(mapped.type, 'system');
    expect(mapped.type, isNot(contains('127')));
  });

  test('generated chat event oneof remains exhaustively mapped', () {
    final cases = <ChatStreamEvent_Event, ChatStreamEvent>{
      ChatStreamEvent_Event.runQueued: ChatStreamEvent(runQueued: RunQueued()),
      ChatStreamEvent_Event.runStarted: ChatStreamEvent(
        runStarted: RunStarted(),
      ),
      ChatStreamEvent_Event.messageStarted: ChatStreamEvent(
        messageStarted: MessageStarted(),
      ),
      ChatStreamEvent_Event.tokenDelta: ChatStreamEvent(
        tokenDelta: TokenDelta(),
      ),
      ChatStreamEvent_Event.toolCallStarted: ChatStreamEvent(
        toolCallStarted: ToolEvent(),
      ),
      ChatStreamEvent_Event.toolCallCompleted: ChatStreamEvent(
        toolCallCompleted: ToolEvent(),
      ),
      ChatStreamEvent_Event.toolCallFailed: ChatStreamEvent(
        toolCallFailed: ToolEvent(),
      ),
      ChatStreamEvent_Event.approvalRequested: ChatStreamEvent(
        approvalRequested: ApprovalEvent(),
      ),
      ChatStreamEvent_Event.approvalApproved: ChatStreamEvent(
        approvalApproved: ApprovalEvent(),
      ),
      ChatStreamEvent_Event.approvalDenied: ChatStreamEvent(
        approvalDenied: ApprovalEvent(),
      ),
      ChatStreamEvent_Event.approvalExpired: ChatStreamEvent(
        approvalExpired: ApprovalEvent(),
      ),
      ChatStreamEvent_Event.approvalConsumed: ChatStreamEvent(
        approvalConsumed: ApprovalEvent(),
      ),
      ChatStreamEvent_Event.messageCompleted: ChatStreamEvent(
        messageCompleted: MessageCompleted(),
      ),
      ChatStreamEvent_Event.runCompleted: ChatStreamEvent(
        runCompleted: RunCompleted(),
      ),
      ChatStreamEvent_Event.runFailed: ChatStreamEvent(runFailed: RunFailed()),
      ChatStreamEvent_Event.runCancelled: ChatStreamEvent(
        runCancelled: RunCancelled(),
      ),
      ChatStreamEvent_Event.persistedEvent: ChatStreamEvent(
        persistedEvent: eventpb.TuringEvent(),
      ),
      ChatStreamEvent_Event.runStateChanged: ChatStreamEvent(
        runStateChanged: RunStateChanged(),
      ),
      ChatStreamEvent_Event.notSet: ChatStreamEvent(),
    };

    expect(cases.keys.toSet(), ChatStreamEvent_Event.values.toSet());
    cases.forEach((expectedCase, streamEvent) {
      expect(streamEvent.whichEvent(), expectedCase);
      expect(
        GrpcMappers.chatStreamEventToTuringEvent(streamEvent).type,
        isNotEmpty,
      );
    });
    expect(
      GrpcMappers.chatStreamEventToTuringEvent(
        cases[ChatStreamEvent_Event.runStateChanged]!,
      ).type,
      'agent.run.state_changed',
    );
  });

  test('maps a message run id for history correlation', () {
    final mapped = GrpcMappers.messageToModel(
      commonpb.Message(
        messageId: 'message_1',
        runId: 'run_1',
        role: commonpb.MessageRole.MESSAGE_ROLE_ASSISTANT,
        content: 'done',
      ),
    );

    expect(mapped.runId, 'run_1');
  });

  test('maps a canonical scored search hit', () {
    final mapped = GrpcMappers.searchHitToModel(
      sessionpb.SearchHit(
        message: commonpb.Message(
          messageId: 'message_42',
          sessionId: 'session_42',
          runId: 'run_42',
          role: commonpb.MessageRole.MESSAGE_ROLE_USER,
          content: 'prefix needle suffix',
          sequence: Int64(99),
          createdAt: timestamppb.Timestamp.fromDateTime(
            DateTime.utc(2026, 8, 13, 12, 34, 56),
          ),
        ),
        score: 0.75,
        snippet: 'prefix needle suffix',
      ),
    );

    expect(mapped.sessionId, 'session_42');
    expect(mapped.score, 0.75);
    expect(mapped.snippet, 'prefix needle suffix');
    expect(mapped.message.messageId, 'message_42');
    expect(mapped.message.runId, 'run_42');
    expect(mapped.message.role, 'user');
    expect(mapped.message.content, 'prefix needle suffix');
    expect(mapped.message.sequence, 99);
    expect(mapped.message.createdAt, DateTime.utc(2026, 8, 13, 12, 34, 56));
  });

  // A zero relevance score is a legitimate ranking value, so the strict mapper
  // must not confuse it with the malformed cases below.
  test('maps a canonical hit whose score is zero', () {
    final mapped = GrpcMappers.searchHitToModel(
      sessionpb.SearchHit(
        message: commonpb.Message(
          messageId: 'message_zero',
          sessionId: 'session_zero',
          role: commonpb.MessageRole.MESSAGE_ROLE_ASSISTANT,
          content: 'needle',
          sequence: Int64(1),
          createdAt: timestamppb.Timestamp.fromDateTime(
            DateTime.utc(2026, 8, 13, 12, 34, 56),
          ),
        ),
        score: 0,
        snippet: 'needle',
      ),
    );

    expect(mapped.score, 0);
    expect(mapped.snippet, 'needle');
    expect(mapped.message.runId, isNull);
  });

  // The search screen renders and announces caught mapping errors, so a
  // malformed hit must fail with a fixed class string that cannot smuggle
  // attacker-controlled transcript, snippet, or query bytes into the UI.
  group('rejects malformed canonical hits without leaking values', () {
    const sentinels = <String>[
      'SENTINEL_CONTENT',
      'SENTINEL_SNIPPET',
      'SENTINEL_SESSION',
      'SENTINEL_MESSAGE_ID',
      'SENTINEL_RUN',
    ];

    commonpb.Message sentinelMessage() {
      return commonpb.Message(
        messageId: 'SENTINEL_MESSAGE_ID',
        sessionId: 'SENTINEL_SESSION',
        runId: 'SENTINEL_RUN',
        role: commonpb.MessageRole.MESSAGE_ROLE_USER,
        content: 'SENTINEL_CONTENT',
        sequence: Int64(7),
        createdAt: timestamppb.Timestamp.fromDateTime(
          DateTime.utc(2026, 8, 13, 12, 34, 56),
        ),
      );
    }

    final cases = <String, ({sessionpb.SearchHit hit, String message})>{
      'missing message': (
        hit: sessionpb.SearchHit(score: 0.5, snippet: 'SENTINEL_SNIPPET'),
        message: 'search hit message is missing',
      ),
      'NaN score': (
        hit: sessionpb.SearchHit(
          message: sentinelMessage(),
          score: double.nan,
          snippet: 'SENTINEL_SNIPPET',
        ),
        message: 'search hit score is invalid',
      ),
      'positive infinite score': (
        hit: sessionpb.SearchHit(
          message: sentinelMessage(),
          score: double.infinity,
          snippet: 'SENTINEL_SNIPPET',
        ),
        message: 'search hit score is invalid',
      ),
      'negative infinite score': (
        hit: sessionpb.SearchHit(
          message: sentinelMessage(),
          score: double.negativeInfinity,
          snippet: 'SENTINEL_SNIPPET',
        ),
        message: 'search hit score is invalid',
      ),
      'negative score': (
        hit: sessionpb.SearchHit(
          message: sentinelMessage(),
          score: -0.25,
          snippet: 'SENTINEL_SNIPPET',
        ),
        message: 'search hit score is invalid',
      ),
      'empty snippet': (
        hit: sessionpb.SearchHit(
          message: sentinelMessage(),
          score: 0.5,
          snippet: '',
        ),
        message: 'search hit snippet is invalid',
      ),
    };

    cases.forEach((name, expected) {
      test(name, () {
        Object? thrown;
        try {
          GrpcMappers.searchHitToModel(expected.hit);
        } catch (error) {
          thrown = error;
        }

        expect(thrown, isA<FormatException>(), reason: name);
        final failure = thrown! as FormatException;
        expect(failure.message, expected.message, reason: name);
        expect(failure.source, isNull, reason: name);
        expect(failure.offset, isNull, reason: name);
        for (final sentinel in sentinels) {
          expect(failure.toString(), isNot(contains(sentinel)), reason: name);
        }
      });
    });
  });

  test('maps a legacy search hit with null score and snippet', () {
    final mapped = GrpcMappers.legacySearchHitToModel(
      commonpb.Message(
        messageId: 'message_42',
        sessionId: 'session_42',
        runId: 'run_42',
        role: commonpb.MessageRole.MESSAGE_ROLE_USER,
        content: 'find  this text',
        sequence: Int64(99),
        createdAt: timestamppb.Timestamp.fromDateTime(
          DateTime.utc(2026, 8, 13, 12, 34, 56),
        ),
      ),
    );

    expect(mapped.sessionId, 'session_42');
    expect(mapped.score, isNull);
    expect(mapped.snippet, isNull);
    expect(mapped.message.messageId, 'message_42');
    expect(mapped.message.runId, 'run_42');
    expect(mapped.message.role, 'user');
    expect(mapped.message.content, 'find  this text');
    expect(mapped.message.sequence, 99);
    expect(mapped.message.createdAt, DateTime.utc(2026, 8, 13, 12, 34, 56));
  });
}
