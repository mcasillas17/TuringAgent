import 'package:flutter_test/flutter_test.dart';
import 'package:grpc/grpc.dart' as grpc;
import 'package:turing_flutter_app/generated/turing/v1/approvals.pbgrpc.dart'
    as pb;
import 'package:turing_flutter_app/models/approval.dart';
import 'package:turing_flutter_app/networking/grpc_client.dart';

void main() {
  late _ApprovalService service;
  late TuringGrpcApi api;

  setUp(() async {
    service = _ApprovalService();
    final server = grpc.Server.create(services: [service]);
    await server.serve(address: '127.0.0.1', port: 0);
    final channel = grpc.ClientChannel(
      '127.0.0.1',
      port: server.port!,
      options: const grpc.ChannelOptions(
        credentials: grpc.ChannelCredentials.insecure(),
      ),
    );
    api = TuringGrpcApi(
      baseUrl: 'http://127.0.0.1:${server.port}',
      apiKey: 'synthetic-client-key',
      channel: channel,
    );
    addTearDown(() async {
      await channel.shutdown();
      await server.shutdown();
    });
  });

  test(
    'authenticated bounded detail read maps exact reviewed content',
    () async {
      final details = await api.getApprovalDetails(
        'appr_1',
        refreshPreview: true,
      );
      expect(service.detailRequest?.approvalId, 'appr_1');
      expect(service.detailRequest?.refreshPreview, isTrue);
      expect(
        service.metadata?['authorization'],
        GrpcAuthMetadata(
          apiKey: 'synthetic-client-key',
        ).headers()['authorization'],
      );
      expect(service.deadline, isNotNull);
      expect(details.sessionId, 'sess_1');
      expect(details.runId, 'run_1');
      expect(details.toolCallId, 'call_1');
      expect(details.argsHash, 'sha256:arguments');
      expect(details.previewHash, 'sha256:preview');
      expect(details.previewState, ApprovalPreviewState.ready);
      expect(
        details.filePreview?.physicalPath,
        'sessions/sess_1/runs/run_1/files/note.txt',
      );
      expect(details.filePreview?.beforeExists, isFalse);
      expect(details.filePreview?.afterText, 'reviewed bytes');
      expect(service.approveRequest, isNull);
      expect(service.consumeCalls, 0);
    },
  );

  test(
    'decision echoes only stored review identity, never replacement args',
    () async {
      final details = await api.getApprovalDetails('appr_1');
      final started = DateTime.now();
      await api.approveReviewedApproval(details, comment: 'reviewed');
      final request = service.approveRequest!;
      expect(request.approvalId, details.approvalId);
      expect(request.previewHash, details.previewHash);
      expect(request.argsHash, details.argsHash);
      expect(request.comment, 'reviewed');
      expect(service.approveDeadline, isNotNull);
      expect(
        service.approveDeadline!.difference(started),
        lessThanOrEqualTo(const Duration(seconds: 11)),
      );
      expect(service.consumeCalls, 0);
    },
  );

  test('unknown wire state fails closed instead of inheriting ready', () async {
    service.details.mergeFromBuffer([88, 99]);
    final details = await api.getApprovalDetails('appr_1');
    expect(details.previewState, ApprovalPreviewState.unknown);
    expect(details.canApproveAt(DateTime.now()), isFalse);
  });

  test('legacy decision sends no fabricated review binding', () async {
    final started = DateTime.now();
    await api.approveApproval('appr_1');
    expect(service.approveRequest?.previewHash, isEmpty);
    expect(service.approveRequest?.argsHash, isEmpty);
    expect(service.approveDeadline, isNotNull);
    expect(
      service.approveDeadline!.difference(started),
      lessThanOrEqualTo(const Duration(seconds: 11)),
    );
  });

  test('denial preserves rationale and has a bounded deadline', () async {
    final started = DateTime.now();
    final result = await api.denyApproval(
      'appr_1',
      reason: 'Do not change this file',
    );
    expect(service.denyRequest?.approvalId, 'appr_1');
    expect(service.denyRequest?.reason, 'Do not change this file');
    expect(result['status'], 'denied');
    expect(service.denyDeadline, isNotNull);
    expect(service.denyDeadline!.isAfter(started), isTrue);
    expect(
      service.denyDeadline!.difference(started),
      lessThanOrEqualTo(const Duration(seconds: 11)),
    );
  });
}

class _ApprovalService extends pb.ApprovalServiceBase {
  final details = pb.ApprovalDetails(
    approvalId: 'appr_1',
    sessionId: 'sess_1',
    runId: 'run_1',
    toolCallId: 'call_1',
    toolName: 'files.create',
    serverName: 'files',
    argsHash: 'sha256:arguments',
    previewHash: 'sha256:preview',
    expiresAt: DateTime.now()
        .add(const Duration(minutes: 1))
        .toUtc()
        .toIso8601String(),
    status: pb.ApprovalStatus.APPROVAL_STATUS_PENDING,
    previewState: pb.ApprovalPreviewState.APPROVAL_PREVIEW_STATE_READY,
    argumentsJson: '{"path":"note.txt","content":"reviewed bytes"}',
    canApprove: true,
    canDeny: true,
    filePreview: pb.FileMutationPreview(
      logicalPath: 'note.txt',
      physicalPath: 'sessions/sess_1/runs/run_1/files/note.txt',
      operation: 'files.create',
      beforeExists: false,
      afterHash: 'sha256:after',
      afterText: 'reviewed bytes',
      unifiedDiff: '--- absent\n+++ after\n+reviewed bytes',
    ),
  );
  pb.GetApprovalDetailsRequest? detailRequest;
  pb.ApproveApprovalRequest? approveRequest;
  pb.DenyApprovalRequest? denyRequest;
  Map<String, String>? metadata;
  DateTime? deadline;
  DateTime? approveDeadline;
  DateTime? denyDeadline;
  int consumeCalls = 0;

  @override
  Future<pb.ApprovalDetails> getApprovalDetails(
    grpc.ServiceCall call,
    pb.GetApprovalDetailsRequest request,
  ) async {
    detailRequest = request;
    metadata = call.clientMetadata;
    deadline = call.deadline;
    return details;
  }

  @override
  Future<pb.ApprovalResponse> approveApproval(
    grpc.ServiceCall call,
    pb.ApproveApprovalRequest request,
  ) async {
    approveRequest = request;
    approveDeadline = call.deadline;
    return pb.ApprovalResponse(
      approvalId: request.approvalId,
      status: pb.ApprovalStatus.APPROVAL_STATUS_APPROVED,
    );
  }

  @override
  Future<pb.ApprovalResponse> denyApproval(
    grpc.ServiceCall call,
    pb.DenyApprovalRequest request,
  ) async {
    denyRequest = request;
    denyDeadline = call.deadline;
    return pb.ApprovalResponse(
      approvalId: request.approvalId,
      status: pb.ApprovalStatus.APPROVAL_STATUS_DENIED,
    );
  }

  @override
  Future<pb.RuntimeApprovalState> getApprovalForRuntime(
    grpc.ServiceCall call,
    pb.GetApprovalForRuntimeRequest request,
  ) async => throw const grpc.GrpcError.unimplemented();

  @override
  Future<pb.ApprovalResponse> consumeApproval(
    grpc.ServiceCall call,
    pb.ConsumeApprovalRequest request,
  ) async {
    consumeCalls++;
    throw const grpc.GrpcError.permissionDenied();
  }

  @override
  Future<pb.FinalizeSandboxArtifactResponse> finalizeSandboxArtifact(
    grpc.ServiceCall call,
    pb.FinalizeSandboxArtifactRequest request,
  ) async => throw const grpc.GrpcError.unimplemented();

  @override
  Future<pb.SessionCapabilityState> checkSessionCapability(
    grpc.ServiceCall call,
    pb.CheckSessionCapabilityRequest request,
  ) async => throw const grpc.GrpcError.unimplemented();
}
