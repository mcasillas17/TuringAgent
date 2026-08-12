import 'package:flutter_test/flutter_test.dart';
import 'package:fixnum/fixnum.dart';
import 'package:grpc/grpc.dart' as grpc;
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

      expect(service.request?.sessionId, 'session-1');
      expect(service.request?.limit, 25);
      expect(service.request?.beforeMessageId, 'message-anchor');
      expect(service.deadline, isNotNull);
      expect(service.deadline!.isAfter(startedAt), isTrue);
      expect(
        service.deadline!.difference(startedAt),
        lessThanOrEqualTo(const Duration(seconds: 11)),
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
  sessionpb.ListMessagesRequest? request;
  DateTime? deadline;

  @override
  Future<sessionpb.ListMessagesResponse> listMessages(
    grpc.ServiceCall call,
    sessionpb.ListMessagesRequest request,
  ) async {
    this.request = request;
    deadline = call.deadline;
    return sessionpb.ListMessagesResponse();
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
