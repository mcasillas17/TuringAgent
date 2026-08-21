import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/generated/turing/v1/common.pb.dart'
    as commonpb;
import 'package:turing_flutter_app/generated/turing/v1/runtime.pb.dart'
    as runtimepb;
import 'package:turing_flutter_app/l10n/generated/app_localizations.dart';
import 'package:turing_flutter_app/l10n/run_state_localizations.dart';
import 'package:turing_flutter_app/models/grpc_mappers.dart';
import 'package:turing_flutter_app/models/run_lifecycle.dart';
import 'package:turing_flutter_app/models/run_state.dart';
import 'package:turing_flutter_app/utils/protobuf_enum.dart';

void main() {
  test('raw wire unknown lifecycle maps to semantic unknown', () {
    final proto = commonpb.RunState.fromBuffer(const [
      0x0a, 0x05, 0x72, 0x75, 0x6e, 0x5f, 0x31, // run_id "run_1"
      0x20, 0x02, // lifecycle field 4, recognized QUEUED
      0x20, 0x7f, // lifecycle field 4, unknown enum value 127
      0x30, 0x01, // state_version field 6
    ]);

    expect(proto.lifecycle, commonpb.RunLifecycle.RUN_LIFECYCLE_QUEUED);
    expect(GrpcMappers.runStateToModel(proto)?.lifecycle, RunLifecycle.unknown);
  });

  test('raw wire unknown outcome maps to semantic unknown', () {
    final proto = commonpb.RunState.fromBuffer(const [
      0x0a, 0x05, 0x72, 0x75, 0x6e, 0x5f, 0x31, // run_id "run_1"
      0x28, 0x02, // outcome_reason field 5, recognized NONE
      0x28, 0x7f, // outcome_reason field 5, unknown enum value 127
      0x30, 0x01, // state_version field 6
    ]);

    expect(
      proto.outcomeReason,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_NONE,
    );
    expect(
      GrpcMappers.runStateToModel(proto)?.outcomeReason,
      RunOutcomeReason.unknown,
    );
  });

  test('raw wire unknown failure origin uses the shared unknown decoder', () {
    final proto = runtimepb.RuntimeRunFailed.fromBuffer(const [
      0x28, 0x02, // failure_origin field 5, recognized CONTEXT_ASSEMBLY
      0x28, 0x7f, // failure_origin field 5, unknown enum value 127
    ]);

    expect(
      proto.failureOrigin,
      runtimepb.FailureOrigin.FAILURE_ORIGIN_CONTEXT_ASSEMBLY,
    );
    expect(
      decodeClosedEnum(
        message: proto,
        fieldNumber: 5,
        readValue: () => proto.failureOrigin,
        unknownValue: runtimepb.FailureOrigin.FAILURE_ORIGIN_UNKNOWN,
      ),
      runtimepb.FailureOrigin.FAILURE_ORIGIN_UNKNOWN,
    );
  });

  test('raw wire values do not panic or render their integer', () async {
    final proto = commonpb.RunState.fromBuffer(const [
      0x0a,
      0x05,
      0x72,
      0x75,
      0x6e,
      0x5f,
      0x31,
      0x20,
      0x02,
      0x20,
      0x7f,
      0x28,
      0x02,
      0x28,
      0x7e,
      0x30,
      0x01,
    ]);

    final state = GrpcMappers.runStateToModel(proto);

    expect(state?.lifecycle, RunLifecycle.unknown);
    expect(state?.outcomeReason, RunOutcomeReason.unknown);
    final l10n = await AppLocalizations.delegate.load(const Locale('en'));
    final copy = localizedRunStateCopy(l10n, state!);
    final rendered = '${copy.title} ${copy.detail}';
    expect(rendered, contains('unavailable'));
    expect(rendered, isNot(contains('127')));
    expect(rendered, isNot(contains('126')));
  });
}
