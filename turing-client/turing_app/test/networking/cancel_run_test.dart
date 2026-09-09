import 'package:fixnum/fixnum.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grpc/grpc.dart' as grpc;
import 'package:turing_flutter_app/generated/google/protobuf/timestamp.pb.dart';
import 'package:turing_flutter_app/generated/turing/v1/chat.pbgrpc.dart' as pb;
import 'package:turing_flutter_app/generated/turing/v1/common.pb.dart'
    as common;
import 'package:turing_flutter_app/models/run_cancellation.dart';
import 'package:turing_flutter_app/models/run_lifecycle.dart';
import 'package:turing_flutter_app/networking/api_client.dart';
import 'package:turing_flutter_app/networking/grpc_client.dart';

common.RunState _state({bool cancelled = true}) => common.RunState(
  runId: 'run_1',
  userMessageId: 'user_1',
  assistantMessageId: 'assistant_1',
  lifecycle: cancelled
      ? common.RunLifecycle.RUN_LIFECYCLE_CANCELLED
      : common.RunLifecycle.RUN_LIFECYCLE_RUNNING,
  outcomeReason: cancelled
      ? common.RunOutcomeReason.RUN_OUTCOME_REASON_USER_CANCELLED
      : common.RunOutcomeReason.RUN_OUTCOME_REASON_NONE,
  stateVersion: Int64(3),
  stateUpdatedAt: Timestamp.fromDateTime(DateTime.utc(2026, 9, 9)),
  finishedAt: cancelled
      ? Timestamp.fromDateTime(DateTime.utc(2026, 9, 9))
      : null,
);

class _Service extends pb.ChatServiceBase {
  pb.CancelRunRequest? request;
  pb.GetRunCancellationRequest? readRequest;
  DateTime? deadline;
  Map<String, String>? metadata;
  grpc.GrpcError? error;
  pb.CancelRunResponse receipt = pb.CancelRunResponse(
    result: pb.CancelRunResult.CANCEL_RUN_RESULT_ACCEPTED,
    runState: _state(),
    progress: pb.CancellationProgress.CANCELLATION_PROGRESS_STOPPING,
  );
  pb.GetRunCancellationResponse status = pb.GetRunCancellationResponse(
    available: true,
    runState: _state(),
    progress: pb.CancellationProgress.CANCELLATION_PROGRESS_RECONCILED,
  );

  @override
  Future<pb.CancelRunResponse> cancelRun(
    grpc.ServiceCall call,
    pb.CancelRunRequest request,
  ) async {
    this.request = request;
    deadline = call.deadline;
    metadata = call.clientMetadata;
    if (error case final failure?) throw failure;
    return receipt;
  }

  @override
  Future<pb.GetRunCancellationResponse> getRunCancellation(
    grpc.ServiceCall call,
    pb.GetRunCancellationRequest request,
  ) async {
    readRequest = request;
    deadline = call.deadline;
    if (error case final failure?) throw failure;
    return status;
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

Future<TuringGrpcApi> _api(_Service service) async {
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
    apiKey: 'synthetic-client',
    channel: channel,
  );
}

void main() {
  test(
    'cancel sends exact identity with authentication and bounded deadline',
    () async {
      final service = _Service();
      final api = await _api(service);
      final result = await api.cancelRun(
        sessionId: 'session_1',
        runId: 'run_1',
        idempotencyKey: 'cancel:run_1',
      );
      expect(service.request!.sessionId, 'session_1');
      expect(service.request!.runId, 'run_1');
      expect(service.request!.idempotencyKey, 'cancel:run_1');
      expect(service.metadata, contains('authorization'));
      expect(
        service.deadline!.difference(DateTime.now()),
        lessThan(const Duration(seconds: 11)),
      );
      expect(result.result, CancelRunResult.accepted);
      expect(result.runState!.lifecycle, RunLifecycle.cancelled);
      expect(result.progress, CancellationProgress.stopping);
    },
  );

  test(
    'status read targets exact run and distinguishes released execution',
    () async {
      final service = _Service();
      final api = await _api(service);
      final result = await api.getRunCancellation(
        sessionId: 'session_1',
        runId: 'run_1',
      );
      expect(service.readRequest!.sessionId, 'session_1');
      expect(service.readRequest!.runId, 'run_1');
      expect(service.deadline, isNotNull);
      expect(result.available, isTrue);
      expect(result.progress, CancellationProgress.reconciled);
    },
  );

  test(
    'unavailable is metadata-free even if malformed backend includes state',
    () async {
      final service = _Service()
        ..receipt = pb.CancelRunResponse(
          result: pb.CancelRunResult.CANCEL_RUN_RESULT_UNAVAILABLE,
          runState: _state(),
        )
        ..status = pb.GetRunCancellationResponse(
          available: false,
          runState: _state(),
        );
      final api = await _api(service);
      final receipt = await api.cancelRun(
        sessionId: 'session_1',
        runId: 'run_1',
        idempotencyKey: 'key',
      );
      final status = await api.getRunCancellation(
        sessionId: 'session_1',
        runId: 'run_1',
      );
      expect(receipt.result, CancelRunResult.unavailable);
      expect(receipt.runState, isNull);
      expect(status.available, isFalse);
      expect(status.runState, isNull);
    },
  );

  for (final code in [
    grpc.StatusCode.unimplemented,
    grpc.StatusCode.unavailable,
    grpc.StatusCode.alreadyExists,
  ]) {
    test(
      'RPC error $code is preserved, never stream-cancel fallback',
      () async {
        final service = _Service()..error = grpc.GrpcError.custom(code);
        final api = await _api(service);
        await expectLater(
          api.cancelRun(
            sessionId: 'session_1',
            runId: 'run_1',
            idempotencyKey: 'key',
          ),
          throwsA(
            isA<grpc.GrpcError>().having((error) => error.code, 'code', code),
          ),
        );
      },
    );
  }

  for (final malformed in [
    pb.CancelRunResponse(result: pb.CancelRunResult.CANCEL_RUN_RESULT_ACCEPTED),
    pb.CancelRunResponse(
      result: pb.CancelRunResult.CANCEL_RUN_RESULT_ACCEPTED,
      runState: _state(cancelled: false),
    ),
    pb.CancelRunResponse(
      result: pb.CancelRunResult.CANCEL_RUN_RESULT_ACCEPTED,
      runState: _state()..runId = 'another_run',
    ),
    pb.CancelRunResponse(
      result: pb.CancelRunResult.CANCEL_RUN_RESULT_ALREADY_TERMINAL,
      runState: _state(cancelled: false),
    ),
  ]) {
    test(
      'malformed receipt is unconfirmed rather than accepted: $malformed',
      () async {
        final service = _Service()..receipt = malformed;
        final api = await _api(service);
        await expectLater(
          api.cancelRun(
            sessionId: 'session_1',
            runId: 'run_1',
            idempotencyKey: 'key',
          ),
          throwsA(isA<TuringApiException>()),
        );
      },
    );
  }

  test('unknown wire result is never accepted', () async {
    final response = pb.CancelRunResponse(
      runState: _state(),
      progress: pb.CancellationProgress.CANCELLATION_PROGRESS_STOPPING,
    );
    response.unknownFields.mergeVarintField(1, Int64(99));
    final service = _Service()..receipt = response;
    final api = await _api(service);
    await expectLater(
      api.cancelRun(
        sessionId: 'session_1',
        runId: 'run_1',
        idempotencyKey: 'key',
      ),
      throwsA(isA<TuringApiException>()),
    );
  });
}
