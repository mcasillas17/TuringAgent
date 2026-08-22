import 'package:fixnum/fixnum.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/generated/google/protobuf/timestamp.pb.dart'
    as timestamppb;
import 'package:turing_flutter_app/generated/turing/v1/common.pb.dart'
    as commonpb;
import 'package:turing_flutter_app/l10n/generated/app_localizations.dart';
import 'package:turing_flutter_app/l10n/run_state_localizations.dart';
import 'package:turing_flutter_app/models/grpc_mappers.dart';
import 'package:turing_flutter_app/models/run_lifecycle.dart';
import 'package:turing_flutter_app/models/run_state.dart';

void main() {
  final updatedAt = DateTime.utc(2026, 8, 21, 1, 2, 3);
  final finishedAt = DateTime.utc(2026, 8, 21, 1, 2, 4);

  commonpb.RunState protoState({
    commonpb.RunLifecycle lifecycle =
        commonpb.RunLifecycle.RUN_LIFECYCLE_FAILED,
    commonpb.RunOutcomeReason outcome =
        commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_PROVIDER_FAILURE,
    Int64? stateVersion,
  }) {
    return commonpb.RunState(
      runId: 'run_1',
      userMessageId: 'msg_user',
      assistantMessageId: 'msg_assistant',
      lifecycle: lifecycle,
      outcomeReason: outcome,
      stateVersion: stateVersion ?? Int64(7),
      stateUpdatedAt: timestamppb.Timestamp.fromDateTime(updatedAt),
      finishedAt: timestamppb.Timestamp.fromDateTime(finishedAt),
      hasDisplayableContent: true,
    );
  }

  test('maps message run state without internal fields', () {
    final message = GrpcMappers.messageToModel(
      commonpb.Message(
        messageId: 'msg_assistant',
        runId: 'run_1',
        role: commonpb.MessageRole.MESSAGE_ROLE_ASSISTANT,
        content: 'partial answer',
        runState: protoState(),
      ),
    );

    expect(
      message.runState,
      RunState(
        runId: 'run_1',
        userMessageId: 'msg_user',
        assistantMessageId: 'msg_assistant',
        lifecycle: RunLifecycle.failed,
        outcomeReason: RunOutcomeReason.providerFailure,
        stateVersion: 7,
        stateUpdatedAt: updatedAt,
        finishedAt: finishedAt,
        hasDisplayableContent: true,
      ),
    );
  });

  test('maps every known lifecycle and outcome', () {
    final lifecycles = {
      commonpb.RunLifecycle.RUN_LIFECYCLE_UNKNOWN: RunLifecycle.unknown,
      commonpb.RunLifecycle.RUN_LIFECYCLE_QUEUED: RunLifecycle.queued,
      commonpb.RunLifecycle.RUN_LIFECYCLE_RUNNING: RunLifecycle.running,
      commonpb.RunLifecycle.RUN_LIFECYCLE_WAITING_APPROVAL:
          RunLifecycle.waitingApproval,
      commonpb.RunLifecycle.RUN_LIFECYCLE_RECOVERING: RunLifecycle.recovering,
      commonpb.RunLifecycle.RUN_LIFECYCLE_COMPLETED: RunLifecycle.completed,
      commonpb.RunLifecycle.RUN_LIFECYCLE_FAILED: RunLifecycle.failed,
      commonpb.RunLifecycle.RUN_LIFECYCLE_CANCELLED: RunLifecycle.cancelled,
    };
    expect({
      ...lifecycles.keys,
      commonpb.RunLifecycle.RUN_LIFECYCLE_UNSPECIFIED,
    }, commonpb.RunLifecycle.values.toSet());
    for (final entry in lifecycles.entries) {
      expect(GrpcMappers.runLifecycleToModel(entry.key), entry.value);
    }

    final outcomes = {
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_UNKNOWN:
          RunOutcomeReason.unknown,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_NONE: RunOutcomeReason.none,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_COMPLETED_NO_CONTENT:
          RunOutcomeReason.completedNoContent,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_USER_CANCELLED:
          RunOutcomeReason.userCancelled,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_ABANDONED:
          RunOutcomeReason.abandoned,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_EXPIRED:
          RunOutcomeReason.expired,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_CONTEXT_LIMIT:
          RunOutcomeReason.contextLimit,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_PROVIDER_FAILURE:
          RunOutcomeReason.providerFailure,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_TOOL_FAILURE:
          RunOutcomeReason.toolFailure,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_POLICY_DENIED:
          RunOutcomeReason.policyDenied,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_RETRIES_EXHAUSTED:
          RunOutcomeReason.retriesExhausted,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_RECOVERY_INTERRUPTED:
          RunOutcomeReason.recoveryInterrupted,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_SIDE_EFFECT_UNCERTAIN:
          RunOutcomeReason.sideEffectUncertain,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_APPROVAL_DELIVERY_FAILED:
          RunOutcomeReason.approvalDeliveryFailed,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_INTERNAL_FAILURE:
          RunOutcomeReason.internalFailure,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_LEGACY_UNKNOWN:
          RunOutcomeReason.legacyUnknown,
    };
    expect({
      ...outcomes.keys,
      commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_UNSPECIFIED,
    }, commonpb.RunOutcomeReason.values.toSet());
    for (final entry in outcomes.entries) {
      expect(GrpcMappers.runOutcomeReasonToModel(entry.key), entry.value);
    }
  });

  test('present unspecified lifecycle maps to semantic unknown', () {
    final state = GrpcMappers.runStateToModel(
      protoState(lifecycle: commonpb.RunLifecycle.RUN_LIFECYCLE_UNSPECIFIED),
    );

    expect(state?.lifecycle, RunLifecycle.unknown);
  });

  test('present unspecified outcome maps to semantic unknown', () {
    final state = GrpcMappers.runStateToModel(
      protoState(
        outcome: commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_UNSPECIFIED,
      ),
    );

    expect(state?.outcomeReason, RunOutcomeReason.unknown);
  });

  test('rejects absent or nonpositive state versions', () {
    expect(
      GrpcMappers.runStateToModel(protoState(stateVersion: Int64.ZERO)),
      isNull,
    );
    expect(
      GrpcMappers.runStateToModel(protoState(stateVersion: Int64(-1))),
      isNull,
    );
  });

  test('rejects run state without its reconciliation identity', () {
    final state = protoState()..clearRunId();

    expect(GrpcMappers.runStateToModel(state), isNull);
  });

  test(
    'rejects a run state with no state_updated_at rather than fabricate epoch',
    () {
      final state = protoState()..clearStateUpdatedAt();

      expect(GrpcMappers.runStateToModel(state), isNull);
    },
  );

  test(
    'rejects a run state whose state_updated_at nanos escape the valid range',
    () {
      final state = protoState();
      state.stateUpdatedAt.nanos = 1000000000;

      expect(GrpcMappers.runStateToModel(state), isNull);
    },
  );

  test('rejects a run state whose state_updated_at nanos are negative', () {
    final state = protoState();
    state.stateUpdatedAt.nanos = -1;

    expect(GrpcMappers.runStateToModel(state), isNull);
  });

  test(
    'accepts a run state whose state_updated_at nanos sit exactly at the '
    'documented upper bound',
    () {
      final state = protoState();
      state.stateUpdatedAt.nanos = 999999999;

      final mapped = GrpcMappers.runStateToModel(state);

      expect(mapped, isNotNull);
      // protobuf's Timestamp.toDateTime() (and DateTime itself) only resolve
      // to microseconds, so the maximum valid nanos value truncates rather
      // than rounds: ...999999999ns becomes ...999999us, one microsecond
      // short of the next second, not the next second itself.
      expect(
        mapped?.stateUpdatedAt,
        DateTime.utc(2026, 8, 21, 1, 2, 3, 999, 999),
      );
      // The nanos edit changes nothing else about the snapshot.
      expect(mapped?.runId, 'run_1');
      expect(mapped?.userMessageId, 'msg_user');
      expect(mapped?.assistantMessageId, 'msg_assistant');
      expect(mapped?.lifecycle, RunLifecycle.failed);
      expect(mapped?.outcomeReason, RunOutcomeReason.providerFailure);
      expect(mapped?.stateVersion, 7);
      expect(mapped?.finishedAt, finishedAt);
      expect(mapped?.hasDisplayableContent, isTrue);
    },
  );

  test(
    'rejects a run state whose state_updated_at seconds exceed 9999-12-31T23:59:59Z',
    () {
      final state = protoState();
      // One second past the documented maximum (253402300799).
      state.stateUpdatedAt.seconds = Int64(253402300800);

      expect(GrpcMappers.runStateToModel(state), isNull);
    },
  );

  test(
    'rejects a run state whose state_updated_at seconds precede 0001-01-01T00:00:00Z',
    () {
      final state = protoState();
      // One second before the documented minimum (-62135596800).
      state.stateUpdatedAt.seconds = Int64(-62135596801);

      expect(GrpcMappers.runStateToModel(state), isNull);
    },
  );

  test(
    'accepts a run state whose state_updated_at seconds sit exactly at the documented bounds',
    () {
      final min = protoState();
      min.stateUpdatedAt.seconds = Int64(-62135596800);
      final max = protoState();
      max.stateUpdatedAt.seconds = Int64(253402300799);

      expect(GrpcMappers.runStateToModel(min), isNotNull);
      expect(GrpcMappers.runStateToModel(max), isNotNull);
    },
  );

  test(
    'accepts an explicitly present zero-valued state_updated_at as the real epoch',
    () {
      final state = protoState();
      state.stateUpdatedAt = timestamppb.Timestamp();

      final mapped = GrpcMappers.runStateToModel(state);

      expect(
        mapped?.stateUpdatedAt,
        DateTime.fromMillisecondsSinceEpoch(0, isUtc: true),
      );
    },
  );

  test('accepts a run state with no finished_at as an unfinished run', () {
    final state = protoState()..clearFinishedAt();

    final mapped = GrpcMappers.runStateToModel(state);

    expect(mapped, isNotNull);
    expect(mapped?.finishedAt, isNull);
  });

  // Mirrors the state_updated_at epoch test above: an explicitly present,
  // all-zero finished_at Timestamp (a run that genuinely finished exactly at
  // the Unix epoch) must still be distinguished from an absent finished_at
  // (a still-running run). hasFinishedAt() is what draws that line — a
  // seconds/nanos-nonzero check would instead treat this real, present
  // zero-valued Timestamp the same as "not yet finished".
  test(
    'accepts an explicitly present zero-valued finished_at as the real epoch',
    () {
      final state = protoState();
      state.finishedAt = timestamppb.Timestamp();

      final mapped = GrpcMappers.runStateToModel(state);

      expect(mapped, isNotNull);
      expect(
        mapped?.finishedAt,
        DateTime.fromMillisecondsSinceEpoch(0, isUtc: true),
      );
    },
  );

  test(
    'rejects a run state whose finished_at nanos escape the valid range',
    () {
      final state = protoState();
      state.finishedAt.nanos = 1000000000;

      expect(GrpcMappers.runStateToModel(state), isNull);
    },
  );

  test('rejects a run state whose finished_at nanos are negative', () {
    final state = protoState();
    state.finishedAt.nanos = -1;

    expect(GrpcMappers.runStateToModel(state), isNull);
  });

  test(
    'accepts a run state whose present finished_at nanos sit exactly at the '
    'documented upper bound',
    () {
      final state = protoState();
      state.finishedAt.nanos = 999999999;

      final mapped = GrpcMappers.runStateToModel(state);

      expect(mapped, isNotNull);
      // Same truncation as state_updated_at above: ...999999999ns becomes
      // ...999999us, not the next second, because DateTime has no
      // nanosecond field to round into.
      expect(
        mapped?.finishedAt,
        DateTime.utc(2026, 8, 21, 1, 2, 4, 999, 999),
      );
      // The nanos edit changes nothing else about the snapshot.
      expect(mapped?.runId, 'run_1');
      expect(mapped?.userMessageId, 'msg_user');
      expect(mapped?.assistantMessageId, 'msg_assistant');
      expect(mapped?.lifecycle, RunLifecycle.failed);
      expect(mapped?.outcomeReason, RunOutcomeReason.providerFailure);
      expect(mapped?.stateVersion, 7);
      expect(mapped?.stateUpdatedAt, updatedAt);
      expect(mapped?.hasDisplayableContent, isTrue);
    },
  );

  test(
    'rejects a run state whose finished_at seconds exceed 9999-12-31T23:59:59Z',
    () {
      final state = protoState();
      // One second past the documented maximum (253402300799).
      state.finishedAt.seconds = Int64(253402300800);

      expect(GrpcMappers.runStateToModel(state), isNull);
    },
  );

  test(
    'rejects a run state whose finished_at seconds precede 0001-01-01T00:00:00Z',
    () {
      final state = protoState();
      // One second before the documented minimum (-62135596800).
      state.finishedAt.seconds = Int64(-62135596801);

      expect(GrpcMappers.runStateToModel(state), isNull);
    },
  );

  // Seconds this large sit far outside the documented Timestamp range but are
  // still small enough that seconds * 1_000_000 (microsecond conversion)
  // does not itself wrap the signed 64-bit product; DateTime's own supported
  // range (roughly +/-273,790 years) is narrower still, so an unvalidated
  // conversion throws a RangeError instead of merely returning a wrong date.
  // The mapper must reject this before ever calling toDateTime(), not let the
  // conversion throw out of runStateToModel.
  test('rejects a run state whose finished_at seconds are so large that '
      'conversion would throw, without throwing itself', () {
    final state = protoState();
    state.finishedAt.seconds = Int64(9000000000000);

    expect(() => GrpcMappers.runStateToModel(state), returnsNormally);
    expect(GrpcMappers.runStateToModel(state), isNull);
  });

  test(
    'accepts a run state whose finished_at seconds sit exactly at the documented bounds',
    () {
      final min = protoState();
      min.finishedAt.seconds = Int64(-62135596800);
      final max = protoState();
      max.finishedAt.seconds = Int64(253402300799);

      expect(GrpcMappers.runStateToModel(min), isNotNull);
      expect(GrpcMappers.runStateToModel(max), isNotNull);
    },
  );

  test('accepts the maximum signed 64-bit state version', () {
    final state = GrpcMappers.runStateToModel(
      protoState(stateVersion: Int64.MAX_VALUE),
    );

    expect(state?.stateVersion, 9223372036854775807);
  });

  test('terminal helper and structural equality cover the full state', () {
    final state = RunState(
      runId: 'run_1',
      userMessageId: 'msg_user',
      assistantMessageId: 'msg_assistant',
      lifecycle: RunLifecycle.failed,
      outcomeReason: RunOutcomeReason.providerFailure,
      stateVersion: 7,
      stateUpdatedAt: updatedAt,
      finishedAt: finishedAt,
      hasDisplayableContent: true,
    );

    expect(state.isTerminal, isTrue);
    expect(state, state.copyWith());
    expect(
      state,
      isNot(state.copyWith(outcomeReason: RunOutcomeReason.toolFailure)),
    );
  });

  test(
    'run state copy resolves through English localization resources',
    () async {
      final l10n = await AppLocalizations.delegate.load(const Locale('en'));
      final copy = localizedRunStateCopy(
        l10n,
        RunState(
          runId: 'run_1',
          userMessageId: 'msg_user',
          assistantMessageId: 'msg_assistant',
          lifecycle: RunLifecycle.failed,
          outcomeReason: RunOutcomeReason.providerFailure,
          stateVersion: 7,
          stateUpdatedAt: updatedAt,
          finishedAt: finishedAt,
          hasDisplayableContent: false,
        ),
      );

      expect(copy.title, 'Provider unavailable');
      expect(copy.detail, 'The model provider could not complete this run.');
    },
  );

  // A completed run with no displayable content and a genuinely unknown
  // outcome reason (a future/unrecognized backend value this client cannot
  // name) must not be reinterpreted as the specific completed_no_content
  // outcome — that would fabricate a truthful-sounding explanation for a
  // reason this client cannot actually identify. It must render the same
  // generic outcome-unavailable copy any other lifecycle uses for an
  // unknown outcome.
  test(
    'completed run with unknown outcome and no content renders generic '
    'outcome-unavailable copy, not completed-no-content',
    () async {
      final l10n = await AppLocalizations.delegate.load(const Locale('en'));
      final copy = localizedRunStateCopy(
        l10n,
        RunState(
          runId: 'run_1',
          userMessageId: 'msg_user',
          assistantMessageId: 'msg_assistant',
          lifecycle: RunLifecycle.completed,
          outcomeReason: RunOutcomeReason.unknown,
          stateVersion: 1,
          stateUpdatedAt: updatedAt,
          finishedAt: finishedAt,
          hasDisplayableContent: false,
        ),
      );

      expect(copy.title, 'Outcome unavailable');
      expect(copy.detail, 'This app cannot identify why the run ended.');
    },
  );

  test(
    'completed run with legacyUnknown outcome and no content renders '
    'generic outcome-unavailable copy, not completed-no-content',
    () async {
      final l10n = await AppLocalizations.delegate.load(const Locale('en'));
      final copy = localizedRunStateCopy(
        l10n,
        RunState(
          runId: 'run_1',
          userMessageId: 'msg_user',
          assistantMessageId: 'msg_assistant',
          lifecycle: RunLifecycle.completed,
          outcomeReason: RunOutcomeReason.legacyUnknown,
          stateVersion: 1,
          stateUpdatedAt: updatedAt,
          finishedAt: finishedAt,
          hasDisplayableContent: false,
        ),
      );

      expect(copy.title, 'Outcome unavailable');
      expect(copy.detail, 'This app cannot identify why the run ended.');
    },
  );

  // Controls: the known no-content outcomes keep their existing canonical
  // completed copy exactly as before — only the unknown/legacyUnknown
  // branch order changes.
  test(
    'completed run with explicit completed-no-content outcome keeps '
    'canonical completed-no-content copy',
    () async {
      final l10n = await AppLocalizations.delegate.load(const Locale('en'));
      final copy = localizedRunStateCopy(
        l10n,
        RunState(
          runId: 'run_1',
          userMessageId: 'msg_user',
          assistantMessageId: 'msg_assistant',
          lifecycle: RunLifecycle.completed,
          outcomeReason: RunOutcomeReason.completedNoContent,
          stateVersion: 1,
          stateUpdatedAt: updatedAt,
          finishedAt: finishedAt,
          hasDisplayableContent: false,
        ),
      );

      expect(copy.title, 'Completed');
      expect(copy.detail, 'No assistant response was recorded.');
    },
  );

  test(
    'completed run with no outcome reason (none) and no content keeps '
    'canonical completed-no-content copy',
    () async {
      final l10n = await AppLocalizations.delegate.load(const Locale('en'));
      final copy = localizedRunStateCopy(
        l10n,
        RunState(
          runId: 'run_1',
          userMessageId: 'msg_user',
          assistantMessageId: 'msg_assistant',
          lifecycle: RunLifecycle.completed,
          outcomeReason: RunOutcomeReason.none,
          stateVersion: 1,
          stateUpdatedAt: updatedAt,
          finishedAt: finishedAt,
          hasDisplayableContent: false,
        ),
      );

      expect(copy.title, 'Completed');
      expect(copy.detail, 'No assistant response was recorded.');
    },
  );

  // Control: actual displayable content present alongside an unknown
  // outcome still keeps the canonical completed copy — the unknown-outcome
  // branch only applies while no displayable content backs it, so this
  // never suppresses or replaces a real assistant bubble's copy.
  test(
    'completed run with unknown outcome but displayable content keeps '
    'canonical completed copy',
    () async {
      final l10n = await AppLocalizations.delegate.load(const Locale('en'));
      final copy = localizedRunStateCopy(
        l10n,
        RunState(
          runId: 'run_1',
          userMessageId: 'msg_user',
          assistantMessageId: 'msg_assistant',
          lifecycle: RunLifecycle.completed,
          outcomeReason: RunOutcomeReason.unknown,
          stateVersion: 1,
          stateUpdatedAt: updatedAt,
          finishedAt: finishedAt,
          hasDisplayableContent: true,
        ),
      );

      expect(copy.title, 'Completed');
      expect(copy.detail, 'The assistant response is complete.');
    },
  );

  test('every lifecycle and outcome has localized safe copy', () async {
    final l10n = await AppLocalizations.delegate.load(const Locale('en'));

    for (final lifecycle in RunLifecycle.values) {
      final copy = localizedRunLifecycleCopy(l10n, lifecycle);
      expect(copy.title, isNotEmpty, reason: lifecycle.name);
      expect(copy.detail, isNotEmpty, reason: lifecycle.name);
    }
    for (final outcome in RunOutcomeReason.values) {
      final copy = localizedRunOutcomeCopy(l10n, outcome);
      expect(copy.title, isNotEmpty, reason: outcome.name);
      expect(copy.detail, isNotEmpty, reason: outcome.name);
    }
    for (final category in RunStepNoticeCategory.values) {
      final copy = localizedRunStepNotice(
        l10n,
        category,
        attempt: 2,
        maxAttempts: 3,
      );
      expect(copy, isNotEmpty, reason: category.name);
      expect(copy, contains('2'));
      expect(copy, contains('3'));
    }
  });

  test('absent legacy state has neutral no-response copy', () async {
    final l10n = await AppLocalizations.delegate.load(const Locale('en'));

    final copy = localizedNoResponseCopy(l10n);

    expect(copy.title, 'No response recorded');
    expect(copy.detail, 'No assistant response was recorded for this run.');
  });
}
