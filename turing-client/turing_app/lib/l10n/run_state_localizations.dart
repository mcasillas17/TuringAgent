import '../models/run_lifecycle.dart';
import '../models/run_state.dart';
import 'generated/app_localizations.dart';

class LocalizedRunCopy {
  const LocalizedRunCopy({required this.title, required this.detail});

  final String title;
  final String detail;
}

LocalizedRunCopy localizedNoResponseCopy(AppLocalizations l10n) {
  return LocalizedRunCopy(
    title: l10n.runNoResponseTitle,
    detail: l10n.runNoResponseDetail,
  );
}

LocalizedRunCopy localizedRunStateCopy(AppLocalizations l10n, RunState state) {
  switch (state.lifecycle) {
    case RunLifecycle.queued:
    case RunLifecycle.running:
    case RunLifecycle.waitingApproval:
    case RunLifecycle.recovering:
      return localizedRunLifecycleCopy(l10n, state.lifecycle);
    case RunLifecycle.completed:
      if (!state.hasDisplayableContent ||
          state.outcomeReason == RunOutcomeReason.completedNoContent) {
        return localizedRunOutcomeCopy(
          l10n,
          RunOutcomeReason.completedNoContent,
        );
      }
      return localizedRunLifecycleCopy(l10n, state.lifecycle);
    case RunLifecycle.failed:
    case RunLifecycle.cancelled:
      if (state.outcomeReason == RunOutcomeReason.none) {
        return localizedRunLifecycleCopy(l10n, state.lifecycle);
      }
      return localizedRunOutcomeCopy(l10n, state.outcomeReason);
    case RunLifecycle.unspecified:
    case RunLifecycle.unknown:
      return localizedRunLifecycleCopy(l10n, RunLifecycle.unknown);
  }
}

LocalizedRunCopy localizedRunLifecycleCopy(
  AppLocalizations l10n,
  RunLifecycle lifecycle,
) {
  switch (lifecycle) {
    case RunLifecycle.queued:
      return LocalizedRunCopy(
        title: l10n.runQueuedTitle,
        detail: l10n.runQueuedDetail,
      );
    case RunLifecycle.running:
      return LocalizedRunCopy(
        title: l10n.runRunningTitle,
        detail: l10n.runRunningDetail,
      );
    case RunLifecycle.waitingApproval:
      return LocalizedRunCopy(
        title: l10n.runWaitingApprovalTitle,
        detail: l10n.runWaitingApprovalDetail,
      );
    case RunLifecycle.recovering:
      return LocalizedRunCopy(
        title: l10n.runRecoveringTitle,
        detail: l10n.runRecoveringDetail,
      );
    case RunLifecycle.completed:
      return LocalizedRunCopy(
        title: l10n.runCompletedTitle,
        detail: l10n.runCompletedDetail,
      );
    case RunLifecycle.failed:
      return LocalizedRunCopy(
        title: l10n.runFailedTitle,
        detail: l10n.runFailedDetail,
      );
    case RunLifecycle.cancelled:
      return LocalizedRunCopy(
        title: l10n.runCancelledTitle,
        detail: l10n.runCancelledDetail,
      );
    case RunLifecycle.unspecified:
    case RunLifecycle.unknown:
      return LocalizedRunCopy(
        title: l10n.runStatusUnavailableTitle,
        detail: l10n.runStatusUnavailableDetail,
      );
  }
}

LocalizedRunCopy localizedRunOutcomeCopy(
  AppLocalizations l10n,
  RunOutcomeReason outcome,
) {
  switch (outcome) {
    case RunOutcomeReason.none:
      return LocalizedRunCopy(
        title: l10n.runOutcomeNoneTitle,
        detail: l10n.runOutcomeNoneDetail,
      );
    case RunOutcomeReason.completedNoContent:
      return LocalizedRunCopy(
        title: l10n.runCompletedNoContentTitle,
        detail: l10n.runCompletedNoContentDetail,
      );
    case RunOutcomeReason.userCancelled:
      return LocalizedRunCopy(
        title: l10n.runUserCancelledTitle,
        detail: l10n.runUserCancelledDetail,
      );
    case RunOutcomeReason.abandoned:
      return LocalizedRunCopy(
        title: l10n.runAbandonedTitle,
        detail: l10n.runAbandonedDetail,
      );
    case RunOutcomeReason.expired:
      return LocalizedRunCopy(
        title: l10n.runExpiredTitle,
        detail: l10n.runExpiredDetail,
      );
    case RunOutcomeReason.contextLimit:
      return LocalizedRunCopy(
        title: l10n.runContextLimitTitle,
        detail: l10n.runContextLimitDetail,
      );
    case RunOutcomeReason.providerFailure:
      return LocalizedRunCopy(
        title: l10n.runProviderFailureTitle,
        detail: l10n.runProviderFailureDetail,
      );
    case RunOutcomeReason.toolFailure:
      return LocalizedRunCopy(
        title: l10n.runToolFailureTitle,
        detail: l10n.runToolFailureDetail,
      );
    case RunOutcomeReason.policyDenied:
      return LocalizedRunCopy(
        title: l10n.runPolicyDeniedTitle,
        detail: l10n.runPolicyDeniedDetail,
      );
    case RunOutcomeReason.retriesExhausted:
      return LocalizedRunCopy(
        title: l10n.runRetriesExhaustedTitle,
        detail: l10n.runRetriesExhaustedDetail,
      );
    case RunOutcomeReason.recoveryInterrupted:
      return LocalizedRunCopy(
        title: l10n.runRecoveryInterruptedTitle,
        detail: l10n.runRecoveryInterruptedDetail,
      );
    case RunOutcomeReason.sideEffectUncertain:
      return LocalizedRunCopy(
        title: l10n.runSideEffectUncertainTitle,
        detail: l10n.runSideEffectUncertainDetail,
      );
    case RunOutcomeReason.approvalDeliveryFailed:
      return LocalizedRunCopy(
        title: l10n.runApprovalDeliveryFailedTitle,
        detail: l10n.runApprovalDeliveryFailedDetail,
      );
    case RunOutcomeReason.internalFailure:
      return LocalizedRunCopy(
        title: l10n.runInternalFailureTitle,
        detail: l10n.runInternalFailureDetail,
      );
    case RunOutcomeReason.unknown:
    case RunOutcomeReason.legacyUnknown:
      return LocalizedRunCopy(
        title: l10n.runOutcomeUnavailableTitle,
        detail: l10n.runOutcomeUnavailableDetail,
      );
  }
}

String localizedRunStepNotice(
  AppLocalizations l10n,
  RunStepNoticeCategory category, {
  required int attempt,
  required int maxAttempts,
}) {
  switch (category) {
    case RunStepNoticeCategory.dispatchRetry:
      return l10n.runDispatchRetryNotice(attempt, maxAttempts);
    case RunStepNoticeCategory.recoveryRetry:
      return l10n.runRecoveryRetryNotice(attempt, maxAttempts);
    case RunStepNoticeCategory.recoveryExhausted:
      return l10n.runRecoveryExhaustedNotice(attempt, maxAttempts);
  }
}
