import 'package:fixnum/fixnum.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grpc/grpc.dart' as grpc;
import 'package:protobuf/well_known_types/google/protobuf/timestamp.pb.dart';
import 'package:turing_flutter_app/generated/turing/v1/attention.pbgrpc.dart'
    as attentiongrpc;
import 'package:turing_flutter_app/generated/turing/v1/common.pb.dart'
    as commonpb;
import 'package:turing_flutter_app/models/attention.dart';
import 'package:turing_flutter_app/networking/api_client.dart';
import 'package:turing_flutter_app/networking/grpc_client.dart';

void main() {
  Future<TuringGrpcApi> connect(grpc.Service service) async {
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
    return TuringGrpcApi(
      baseUrl: 'http://127.0.0.1:${server.port}',
      apiKey: 'client-key',
      channel: channel,
    );
  }

  test('listAttention asks for one page of 100 and maps it', () async {
    final service = _AttentionService();
    final api = await connect(service);

    final page = await api.listAttention();

    final request = service.listed.single;
    expect(request.page.limit, 100);
    expect(request.page.cursor, isEmpty);
    expect(service.listDeadline, isNotNull);
    expect(service.metadata['authorization'], 'Bearer client-key');
    expect(page.rows.single.stage, AttentionStage.approval);
    expect(page.rows.single.approvalId, 'appr_1');
    expect(page.nextCursor, 'next-page');
  });

  test('a later page sends the cursor back unchanged', () async {
    final service = _AttentionService();
    final api = await connect(service);

    await api.listAttention(cursor: 'next-page');

    expect(service.listed.single.page.cursor, 'next-page');
  });

  test('dismissAttention sends the automation and its key unchanged', () async {
    final service = _AttentionService();
    final api = await connect(service);

    await api.dismissAttention(
      automationId: 'auto_1',
      occurrenceKey: 'audit_01J9Z',
    );

    expect(service.dismissed.single.automationId, 'auto_1');
    expect(service.dismissed.single.occurrenceKey, 'audit_01J9Z');
    expect(service.dismissDeadline, isNotNull);
  });

  test('statuses keep their codes so the page can tell them apart', () async {
    final cases = {
      grpc.StatusCode.failedPrecondition: 'failed_precondition',
      grpc.StatusCode.notFound: 'not_found',
      grpc.StatusCode.unimplemented: 'unimplemented',
      grpc.StatusCode.unavailable: 'unavailable',
      grpc.StatusCode.internal: 'attention_error',
    };
    for (final entry in cases.entries) {
      final api = await connect(_AttentionService(failure: entry.key));
      await expectLater(
        api.listAttention(),
        throwsA(
          isA<TuringApiException>().having(
            (error) => error.code,
            'code',
            entry.value,
          ),
        ),
      );
      await expectLater(
        api.dismissAttention(automationId: 'auto_1', occurrenceKey: 'run_1'),
        throwsA(
          isA<TuringApiException>().having(
            (error) => error.code,
            'code',
            entry.value,
          ),
        ),
      );
    }
  });
}

class _AttentionService extends attentiongrpc.AttentionServiceBase {
  _AttentionService({this.failure});

  final int? failure;
  final listed = <attentiongrpc.ListAttentionRequest>[];
  final dismissed = <attentiongrpc.DismissAttentionRequest>[];
  DateTime? listDeadline;
  DateTime? dismissDeadline;
  Map<String, String> metadata = const {};

  @override
  Future<attentiongrpc.ListAttentionResponse> listAttention(
    grpc.ServiceCall call,
    attentiongrpc.ListAttentionRequest request,
  ) async {
    listed.add(request);
    listDeadline = call.deadline;
    metadata = Map.of(call.clientMetadata ?? const {});
    final code = failure;
    if (code != null) throw grpc.GrpcError.custom(code, 'cannot list');
    return attentiongrpc.ListAttentionResponse(
      rows: [
        attentiongrpc.AttentionRow(
          stage: attentiongrpc.AttentionStage.ATTENTION_STAGE_PERSON_APPROVAL,
          kind:
              attentiongrpc.AttentionSourceKind.ATTENTION_SOURCE_KIND_APPROVAL,
          since: Timestamp(seconds: Int64(1795000000)),
          sessionId: 'sess_1',
          sessionTitle: 'Chat',
          approvalId: 'appr_1',
          runId: 'run_1',
        ),
      ],
      page: commonpb.PageResponse(nextCursor: 'next-page'),
    );
  }

  @override
  Future<attentiongrpc.DismissAttentionResponse> dismissAttention(
    grpc.ServiceCall call,
    attentiongrpc.DismissAttentionRequest request,
  ) async {
    dismissed.add(request);
    dismissDeadline = call.deadline;
    final code = failure;
    if (code != null) throw grpc.GrpcError.custom(code, 'cannot dismiss');
    return attentiongrpc.DismissAttentionResponse();
  }
}
