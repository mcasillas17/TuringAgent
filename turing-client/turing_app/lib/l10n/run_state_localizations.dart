import '../models/run_lifecycle.dart';
import '../models/run_state.dart';
import 'generated/app_localizations.dart';

class LocalizedRunCopy {
  const LocalizedRunCopy({required this.title, required this.detail});

  final String title;
  final String detail;
}

/// Whether a [RunOutcomeReason] must render as the generic "outcome
/// unavailable" copy instead of a specific, nameable explanation.
///
/// This is presentation policy, not a model-level fact, so it lives beside
/// the localized copy it drives rather than on the model itself:
///
/// - [RunOutcomeReason.unknown] is a *live* path, reachable today: the
///   backend's own projection deliberately maps a persisted reason string it
///   does not recognize to the explicit `RUN_OUTCOME_REASON_UNKNOWN` wire
///   value, and it allows that even paired with a `completed` lifecycle — it
///   is not restricted to `failed`/`cancelled` the way the named reasons
///   are. On top of that, a future backend can introduce outcome reason
///   values this client's generated proto does not yet know about, and this
///   client also decodes an absent/`UNSPECIFIED` value the same way, even
///   though the backend does not normally emit it. All three collapse to
///   `unknown` on the wire today. This client must still render something
///   truthful for them right now, without waiting for a client update.
/// - [RunOutcomeReason.legacyUnknown] is *defensive*, not currently
///   reachable in practice: the backend's normative lifecycle/outcome
///   matrix only legally pairs `legacyUnknown` with the `unknown`
///   lifecycle, never with `completed`, `failed`, or `cancelled`. This
///   client still guards every lifecycle against it so a future relaxation
///   of that constraint, or an upstream bug, cannot silently fabricate a
///   specific-sounding outcome.
///
/// This is intentionally implemented with an exhaustive switch (no
/// `default`) rather than an `==` chain: adding a new [RunOutcomeReason]
/// member forces a deliberate `true`/`false` decision here, so future
/// additions cannot silently drift into or out of the "unavailable" bucket.
extension RunOutcomeReasonCopyAvailability on RunOutcomeReason {
  bool get usesOutcomeUnavailableCopy {
    switch (this) {
      case RunOutcomeReason.unknown:
      case RunOutcomeReason.legacyUnknown:
        return true;
      case RunOutcomeReason.none:
      case RunOutcomeReason.completedNoContent:
      case RunOutcomeReason.userCancelled:
      case RunOutcomeReason.abandoned:
      case RunOutcomeReason.expired:
      case RunOutcomeReason.contextLimit:
      case RunOutcomeReason.providerFailure:
      case RunOutcomeReason.toolFailure:
      case RunOutcomeReason.policyDenied:
      case RunOutcomeReason.retriesExhausted:
      case RunOutcomeReason.recoveryInterrupted:
      case RunOutcomeReason.sideEffectUncertain:
      case RunOutcomeReason.approvalDeliveryFailed:
      case RunOutcomeReason.internalFailure:
        return false;
    }
  }
}

/// The single construction site for the generic "outcome unavailable"
/// copy, shared by every caller that needs it so the localized strings are
/// built in exactly one place instead of duplicated at each call site.
LocalizedRunCopy _outcomeUnavailableCopy(AppLocalizations l10n) {
  return LocalizedRunCopy(
    title: l10n.runOutcomeUnavailableTitle,
    detail: l10n.runOutcomeUnavailableDetail,
  );
}

LocalizedRunCopy localizedNoResponseCopy(AppLocalizations l10n) {
  return LocalizedRunCopy(
    title: l10n.runNoResponseTitle,
    detail: l10n.runNoResponseDetail,
  );
}

LocalizedRunCopy localizedCompletedContentUnavailableCopy(
  AppLocalizations l10n,
) {
  return LocalizedRunCopy(
    title: l10n.runCompletedContentUnavailableTitle,
    detail: l10n.runCompletedContentUnavailableDetail,
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
      // A genuinely unknown/unrecognized outcome reason (a future backend
      // value this client cannot name) must not be reinterpreted as the
      // specific completedNoContent outcome just because there is no
      // displayable content — that would fabricate a truthful-sounding
      // explanation this client does not actually have. Consult
      // outcomeReason.usesOutcomeUnavailableCopy for that case before
      // inferring completedNoContent.
      if (!state.hasDisplayableContent &&
          state.outcomeReason.usesOutcomeUnavailableCopy) {
        return _outcomeUnavailableCopy(l10n);
      }
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
  // Route every outcome this client cannot name to the same generic
  // "unavailable" copy up front, keyed off the shared
  // usesOutcomeUnavailableCopy predicate so this grouping and the
  // completed-outcome guard in localizedRunStateCopy can never drift apart.
  if (outcome.usesOutcomeUnavailableCopy) {
    return _outcomeUnavailableCopy(l10n);
  }
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
      // Unreachable: usesOutcomeUnavailableCopy routes these above. Kept
      // as an explicit case (not a `default`) so the switch stays
      // exhaustive — a future RunOutcomeReason member still forces a
      // deliberate case here, and in
      // RunOutcomeReasonCopyAvailability.usesOutcomeUnavailableCopy,
      // instead of silently falling through.
      return _outcomeUnavailableCopy(l10n);
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
