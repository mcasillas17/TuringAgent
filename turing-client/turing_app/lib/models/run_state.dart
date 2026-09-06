import 'run_lifecycle.dart';

enum RunOutcomeReason {
  unknown,
  none,
  completedNoContent,
  userCancelled,
  abandoned,
  expired,
  contextLimit,
  providerFailure,
  toolFailure,
  policyDenied,
  retriesExhausted,
  recoveryInterrupted,
  sideEffectUncertain,
  approvalDeliveryFailed,
  internalFailure,
  legacyUnknown,
}

/// Why a queued run is still waiting, or which queue bound ended one that
/// stopped waiting.
///
/// [none] is the normal queue and needs no explanation of its own: something is
/// able to run this, it just has not yet. [noCompatibleWorker] is the state a
/// user can act on — nothing connected can serve this run's route.
/// [queueTimeout] only ever appears on a terminal run whose overall queue-age
/// bound ran out, and is what separates that from an approval that expired,
/// since both report [RunOutcomeReason.expired].
///
/// [unknown] covers an absent value and one a newer backend introduced, on the
/// same terms the other reserved values use: it describes no state, so it is
/// rendered as the plain lifecycle rather than as a guess.
enum QueueWaitReason { unknown, none, noCompatibleWorker, queueTimeout }

enum RunStepNoticeCategory { dispatchRetry, recoveryRetry, recoveryExhausted }

/// Mirrors the backend's public retry-notice counter bound.
const maxRunStepNoticeAttempts = 1000;

class RunState {
  const RunState({
    required this.runId,
    required this.userMessageId,
    required this.assistantMessageId,
    required this.lifecycle,
    required this.outcomeReason,
    required this.stateVersion,
    required this.stateUpdatedAt,
    required this.finishedAt,
    required this.hasDisplayableContent,
    this.queueWaitReason = QueueWaitReason.none,
  });

  final String runId;
  final String userMessageId;
  final String assistantMessageId;
  final RunLifecycle lifecycle;
  final RunOutcomeReason outcomeReason;
  final int stateVersion;
  final DateTime stateUpdatedAt;
  final DateTime? finishedAt;
  final bool hasDisplayableContent;
  final QueueWaitReason queueWaitReason;

  bool get isTerminal =>
      lifecycle == RunLifecycle.completed ||
      lifecycle == RunLifecycle.failed ||
      lifecycle == RunLifecycle.cancelled;

  RunState copyWith({
    String? runId,
    String? userMessageId,
    String? assistantMessageId,
    RunLifecycle? lifecycle,
    RunOutcomeReason? outcomeReason,
    int? stateVersion,
    DateTime? stateUpdatedAt,
    DateTime? finishedAt,
    bool? hasDisplayableContent,
    QueueWaitReason? queueWaitReason,
  }) {
    return RunState(
      runId: runId ?? this.runId,
      userMessageId: userMessageId ?? this.userMessageId,
      assistantMessageId: assistantMessageId ?? this.assistantMessageId,
      lifecycle: lifecycle ?? this.lifecycle,
      outcomeReason: outcomeReason ?? this.outcomeReason,
      stateVersion: stateVersion ?? this.stateVersion,
      stateUpdatedAt: stateUpdatedAt ?? this.stateUpdatedAt,
      finishedAt: finishedAt ?? this.finishedAt,
      hasDisplayableContent:
          hasDisplayableContent ?? this.hasDisplayableContent,
      queueWaitReason: queueWaitReason ?? this.queueWaitReason,
    );
  }

  @override
  bool operator ==(Object other) =>
      identical(this, other) ||
      other is RunState &&
          runId == other.runId &&
          userMessageId == other.userMessageId &&
          assistantMessageId == other.assistantMessageId &&
          lifecycle == other.lifecycle &&
          outcomeReason == other.outcomeReason &&
          stateVersion == other.stateVersion &&
          stateUpdatedAt == other.stateUpdatedAt &&
          finishedAt == other.finishedAt &&
          hasDisplayableContent == other.hasDisplayableContent &&
          queueWaitReason == other.queueWaitReason;

  @override
  int get hashCode => Object.hash(
    runId,
    userMessageId,
    assistantMessageId,
    lifecycle,
    outcomeReason,
    stateVersion,
    stateUpdatedAt,
    finishedAt,
    hasDisplayableContent,
    queueWaitReason,
  );
}
