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

enum RunStepNoticeCategory { dispatchRetry, recoveryRetry, recoveryExhausted }

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
          hasDisplayableContent == other.hasDisplayableContent;

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
  );
}
