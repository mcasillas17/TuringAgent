import 'package:flutter_test/flutter_test.dart';
import 'package:fixnum/fixnum.dart';
import 'package:grpc/grpc.dart' as grpc;
import 'package:turing_flutter_app/generated/google/protobuf/timestamp.pb.dart'
    as timestamppb;
import 'package:turing_flutter_app/generated/turing/v1/common.pb.dart'
    as commonpb;
import 'package:turing_flutter_app/generated/turing/v1/events.pb.dart'
    as eventpb;
import 'package:turing_flutter_app/generated/turing/v1/events.pbgrpc.dart'
    as eventgrpc;
import 'package:turing_flutter_app/generated/turing/v1/sessions.pb.dart'
    as sessionpb;
import 'package:turing_flutter_app/generated/turing/v1/sessions.pbgrpc.dart'
    as sessiongrpc;
import 'package:turing_flutter_app/networking/grpc_client.dart';

void main() {
  test('adds bearer token metadata', () {
    final metadata = GrpcAuthMetadata(apiKey: 'client-key').headers();

    expect(metadata['authorization'], 'Bearer client-key');
  });

  test(
    'listMessages forwards the before anchor with a bounded deadline',
    () async {
      final service = _CapturingSessionService();
      final server = grpc.Server.create(services: [service]);
      await server.serve(address: '127.0.0.1', port: 0);
      final channel = grpc.ClientChannel(
        '127.0.0.1',
        port: server.port!,
        options: const grpc.ChannelOptions(
          credentials: grpc.ChannelCredentials.insecure(),
        ),
      );
      addTearDown(() async {
        await channel.shutdown();
        await server.shutdown();
      });
      final api = TuringGrpcApi(
        baseUrl: 'http://127.0.0.1:${server.port}',
        apiKey: 'client-key',
        channel: channel,
      );

      final startedAt = DateTime.now();
      await api.listMessages(
        sessionId: 'session-1',
        limit: 25,
        before: 'message-anchor',
      );

      expect(service.listMessagesRequest?.sessionId, 'session-1');
      expect(service.listMessagesRequest?.limit, 25);
      expect(service.listMessagesRequest?.beforeMessageId, 'message-anchor');
      expect(service.listMessagesDeadline, isNotNull);
      expect(service.listMessagesDeadline!.isAfter(startedAt), isTrue);
      expect(
        service.listMessagesDeadline!.difference(startedAt),
        lessThanOrEqualTo(const Duration(seconds: 11)),
      );
    },
  );

  test('getSession forwards the session id with a bounded deadline', () async {
    final service = _CapturingSessionService();
    final server = grpc.Server.create(services: [service]);
    await server.serve(address: '127.0.0.1', port: 0);
    final channel = grpc.ClientChannel(
      '127.0.0.1',
      port: server.port!,
      options: const grpc.ChannelOptions(
        credentials: grpc.ChannelCredentials.insecure(),
      ),
    );
    addTearDown(() async {
      await channel.shutdown();
      await server.shutdown();
    });
    final api = TuringGrpcApi(
      baseUrl: 'http://127.0.0.1:${server.port}',
      apiKey: 'client-key',
      channel: channel,
    );

    final startedAt = DateTime.now();
    final session = await api.getSession(sessionId: 'session-42');

    expect(service.getSessionRequest?.sessionId, 'session-42');
    expect(service.getSessionDeadline, isNotNull);
    expect(service.getSessionDeadline!.isAfter(startedAt), isTrue);
    expect(
      service.getSessionDeadline!.difference(startedAt),
      lessThanOrEqualTo(const Duration(seconds: 11)),
    );
    expect(session.sessionId, 'session-42');
    expect(session.title, 'Search target');
    expect(session.updatedAt, DateTime.utc(2026, 8, 13, 12, 34, 56));
  });

  test(
    'searchMessages preserves the raw query, empty session filter, limit, and bounded deadline',
    () async {
      final service = _CapturingSessionService();
      final server = grpc.Server.create(services: [service]);
      await server.serve(address: '127.0.0.1', port: 0);
      final channel = grpc.ClientChannel(
        '127.0.0.1',
        port: server.port!,
        options: const grpc.ChannelOptions(
          credentials: grpc.ChannelCredentials.insecure(),
        ),
      );
      addTearDown(() async {
        await channel.shutdown();
        await server.shutdown();
      });
      final api = TuringGrpcApi(
        baseUrl: 'http://127.0.0.1:${server.port}',
        apiKey: 'client-key',
        channel: channel,
      );

      final startedAt = DateTime.now();
      final hits = await api.searchMessages(query: 'hello  there', limit: 25);

      expect(service.searchMessagesRequest?.query, 'hello  there');
      expect(service.searchMessagesRequest?.sessionId, '');
      expect(service.searchMessagesRequest?.limit, 25);
      expect(service.searchMessagesDeadline, isNotNull);
      expect(service.searchMessagesDeadline!.isAfter(startedAt), isTrue);
      expect(
        service.searchMessagesDeadline!.difference(startedAt),
        lessThanOrEqualTo(const Duration(seconds: 11)),
      );
      expect(hits, hasLength(1));
      expect(hits.single.sessionId, 'session-42');
      expect(hits.single.message.messageId, 'message-42');
      expect(hits.single.message.runId, 'run-42');
      expect(hits.single.message.role, 'assistant');
      expect(hits.single.message.content, 'hello  there');
      expect(hits.single.message.sequence, 99);
      expect(
        hits.single.message.createdAt,
        DateTime.utc(2026, 8, 13, 12, 35, 56),
      );
    },
  );

  test(
    'listEvents preserves latest sequence with a bounded deadline',
    () async {
      final service = _LatestSequenceEventService();
      final server = grpc.Server.create(services: [service]);
      await server.serve(address: '127.0.0.1', port: 0);
      final channel = grpc.ClientChannel(
        '127.0.0.1',
        port: server.port!,
        options: const grpc.ChannelOptions(
          credentials: grpc.ChannelCredentials.insecure(),
        ),
      );
      addTearDown(() async {
        await channel.shutdown();
        await server.shutdown();
      });
      final api = TuringGrpcApi(
        baseUrl: 'http://127.0.0.1:${server.port}',
        apiKey: 'client-key',
        channel: channel,
      );

      final startedAt = DateTime.now();
      final page = await api.listEvents(sessionId: 'session-1');

      expect(page.latestSequence, 700);
      expect(service.deadline, isNotNull);
      expect(service.deadline!.isAfter(startedAt), isTrue);
      expect(
        service.deadline!.difference(startedAt),
        lessThanOrEqualTo(const Duration(seconds: 11)),
      );
    },
  );
}

class _CapturingSessionService extends sessiongrpc.SessionServiceBase {
  sessionpb.GetSessionRequest? getSessionRequest;
  DateTime? getSessionDeadline;
  sessionpb.ListMessagesRequest? listMessagesRequest;
  DateTime? listMessagesDeadline;
  sessionpb.SearchMessagesRequest? searchMessagesRequest;
  DateTime? searchMessagesDeadline;

  @override
  Future<sessionpb.Session> getSession(
    grpc.ServiceCall call,
    sessionpb.GetSessionRequest request,
  ) async {
    getSessionRequest = request;
    getSessionDeadline = call.deadline;
    return sessionpb.Session(
      sessionId: 'session-42',
      title: 'Search target',
      updatedAt: timestamppb.Timestamp.fromDateTime(
        DateTime.utc(2026, 8, 13, 12, 34, 56),
      ),
    );
  }

  @override
  Future<sessionpb.ListMessagesResponse> listMessages(
    grpc.ServiceCall call,
    sessionpb.ListMessagesRequest request,
  ) async {
    listMessagesRequest = request;
    listMessagesDeadline = call.deadline;
    return sessionpb.ListMessagesResponse();
  }

  @override
  Future<sessionpb.SearchMessagesResponse> searchMessages(
    grpc.ServiceCall call,
    sessionpb.SearchMessagesRequest request,
  ) async {
    searchMessagesRequest = request;
    searchMessagesDeadline = call.deadline;
    return sessionpb.SearchMessagesResponse(
      messages: [
        commonpb.Message(
          messageId: 'message-42',
          sessionId: 'session-42',
          runId: 'run-42',
          role: commonpb.MessageRole.MESSAGE_ROLE_ASSISTANT,
          content: 'hello  there',
          sequence: Int64(99),
          createdAt: timestamppb.Timestamp.fromDateTime(
            DateTime.utc(2026, 8, 13, 12, 35, 56),
          ),
        ),
      ],
    );
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

class _LatestSequenceEventService extends eventgrpc.EventServiceBase {
  DateTime? deadline;

  @override
  Future<eventpb.ListEventsResponse> listEvents(
    grpc.ServiceCall call,
    eventpb.ListEventsRequest request,
  ) async {
    deadline = call.deadline;
    return eventpb.ListEventsResponse(latestSequence: Int64(700));
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}
