import 'package:fixnum/fixnum.dart';
import 'package:flutter_test/flutter_test.dart';
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

    final cases = <String, sessionpb.SearchHit>{
      'missing message': sessionpb.SearchHit(
        score: 0.5,
        snippet: 'SENTINEL_SNIPPET',
      ),
      'NaN score': sessionpb.SearchHit(
        message: sentinelMessage(),
        score: double.nan,
        snippet: 'SENTINEL_SNIPPET',
      ),
      'positive infinite score': sessionpb.SearchHit(
        message: sentinelMessage(),
        score: double.infinity,
        snippet: 'SENTINEL_SNIPPET',
      ),
      'negative infinite score': sessionpb.SearchHit(
        message: sentinelMessage(),
        score: double.negativeInfinity,
        snippet: 'SENTINEL_SNIPPET',
      ),
      'negative score': sessionpb.SearchHit(
        message: sentinelMessage(),
        score: -0.25,
        snippet: 'SENTINEL_SNIPPET',
      ),
      'empty snippet': sessionpb.SearchHit(
        message: sentinelMessage(),
        score: 0.5,
        snippet: '',
      ),
    };

    cases.forEach((name, hit) {
      test(name, () {
        Object? thrown;
        try {
          GrpcMappers.searchHitToModel(hit);
        } catch (error) {
          thrown = error;
        }

        expect(thrown, isA<FormatException>(), reason: name);
        final failure = thrown! as FormatException;
        expect(
          failure.message,
          anyOf(
            'search hit message is missing',
            'search hit score is invalid',
            'search hit snippet is invalid',
          ),
          reason: name,
        );
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
