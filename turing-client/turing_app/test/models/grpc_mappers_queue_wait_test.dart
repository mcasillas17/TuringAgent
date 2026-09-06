import 'package:fixnum/fixnum.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:protobuf/protobuf.dart';
import 'package:turing_flutter_app/generated/turing/v1/common.pb.dart'
    as commonpb;
import 'package:turing_flutter_app/models/grpc_mappers.dart';
import 'package:turing_flutter_app/generated/google/protobuf/timestamp.pb.dart'
    as timestamppb;
import 'package:turing_flutter_app/models/run_state.dart';

/// TUR-010's decode boundary. The queue reason is a closed vocabulary, so the
/// three answers a client can honestly give — none, a value it knows, and "a
/// newer server said something I cannot name" — have to stay distinct.
void main() {
  commonpb.RunState baseState() {
    return commonpb.RunState(
      runId: 'run_queue',
      userMessageId: 'msg_user',
      assistantMessageId: 'msg_assistant',
      lifecycle: commonpb.RunLifecycle.RUN_LIFECYCLE_QUEUED,
      outcomeReason: commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_NONE,
      stateVersion: Int64(2),
      stateUpdatedAt: timestamppb.Timestamp(seconds: Int64(1788000000)),
    );
  }

  test('an absent queue reason decodes as none, not as unknown', () {
    final decoded = GrpcMappers.runStateToModel(baseState());

    expect(decoded, isNotNull);
    // Every snapshot written before TUR-010 looks exactly like this one.
    // Reading it as unknown would put "status unavailable" on all of history.
    expect(decoded!.queueWaitReason, QueueWaitReason.none);
  });

  test('the named queue reasons round trip', () {
    for (final (wire, model) in <(commonpb.QueueWaitReason, QueueWaitReason)>[
      (commonpb.QueueWaitReason.QUEUE_WAIT_REASON_NONE, QueueWaitReason.none),
      (
        commonpb.QueueWaitReason.QUEUE_WAIT_REASON_NO_COMPATIBLE_WORKER,
        QueueWaitReason.noCompatibleWorker,
      ),
      (
        commonpb.QueueWaitReason.QUEUE_WAIT_REASON_QUEUE_TIMEOUT,
        QueueWaitReason.queueTimeout,
      ),
      (
        commonpb.QueueWaitReason.QUEUE_WAIT_REASON_UNKNOWN,
        QueueWaitReason.unknown,
      ),
    ]) {
      final decoded = GrpcMappers.runStateToModel(
        baseState()..queueWaitReason = wire,
      );
      expect(decoded?.queueWaitReason, model, reason: '$wire');
    }
  });

  test('a queue reason a newer backend invented decodes as unknown', () {
    // Field 10, varint 99: a value this build's generated enum has no name
    // for, which arrives in unknownFields rather than as a readable value.
    final unknown = UnknownFieldSet()
      ..addField(10, UnknownFieldSetField()..varints.add(Int64(99)));
    final message = baseState()..mergeUnknownFields(unknown);

    final decoded = GrpcMappers.runStateToModel(message);
    expect(decoded, isNotNull);
    expect(decoded!.queueWaitReason, QueueWaitReason.unknown);
  });
}
