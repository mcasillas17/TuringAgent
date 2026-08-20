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

  test('maps a search hit from a complete proto message', () {
    final mapped = GrpcMappers.searchHitToModel(
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
    expect(mapped.message.messageId, 'message_42');
    expect(mapped.message.runId, 'run_42');
    expect(mapped.message.role, 'user');
    expect(mapped.message.content, 'find  this text');
    expect(mapped.message.sequence, 99);
    expect(mapped.message.createdAt, DateTime.utc(2026, 8, 13, 12, 34, 56));
  });
}
