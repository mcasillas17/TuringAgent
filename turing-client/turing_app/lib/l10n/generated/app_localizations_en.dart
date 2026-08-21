// ignore: unused_import
import 'package:intl/intl.dart' as intl;
import 'app_localizations.dart';

// ignore_for_file: type=lint

/// The translations for English (`en`).
class AppLocalizationsEn extends AppLocalizations {
  AppLocalizationsEn([String locale = 'en']) : super(locale);

  @override
  String get runStatusUnavailableTitle => 'Run status unavailable';

  @override
  String get runStatusUnavailableDetail =>
      'This app cannot identify the run\'s current status.';

  @override
  String get runQueuedTitle => 'Queued';

  @override
  String get runQueuedDetail => 'The run is waiting to start.';

  @override
  String get runRunningTitle => 'Working';

  @override
  String get runRunningDetail => 'The assistant is working on this run.';

  @override
  String get runWaitingApprovalTitle => 'Waiting for approval';

  @override
  String get runWaitingApprovalDetail =>
      'The run is paused until an approval is decided.';

  @override
  String get runRecoveringTitle => 'Recovering';

  @override
  String get runRecoveringDetail =>
      'The app is recovering this run after an interruption.';

  @override
  String get runCompletedTitle => 'Completed';

  @override
  String get runCompletedDetail => 'The assistant response is complete.';

  @override
  String get runCompletedNoContentTitle => 'Completed';

  @override
  String get runCompletedNoContentDetail =>
      'No assistant response was recorded.';

  @override
  String get runFailedTitle => 'Run failed';

  @override
  String get runFailedDetail => 'The run ended before it could complete.';

  @override
  String get runCancelledTitle => 'Run interrupted';

  @override
  String get runCancelledDetail => 'The run ended before it could finish.';

  @override
  String get runOutcomeUnavailableTitle => 'Outcome unavailable';

  @override
  String get runOutcomeUnavailableDetail =>
      'This app cannot identify why the run ended.';

  @override
  String get runNoResponseTitle => 'No response recorded';

  @override
  String get runNoResponseDetail =>
      'No assistant response was recorded for this run.';

  @override
  String get runOutcomeNoneTitle => 'No terminal outcome';

  @override
  String get runOutcomeNoneDetail => 'The run has no terminal outcome.';

  @override
  String get runUserCancelledTitle => 'Run cancelled';

  @override
  String get runUserCancelledDetail => 'You cancelled this run.';

  @override
  String get runAbandonedTitle => 'Run interrupted';

  @override
  String get runAbandonedDetail => 'The run ended before it could finish.';

  @override
  String get runExpiredTitle => 'Run expired';

  @override
  String get runExpiredDetail => 'The run expired before it could finish.';

  @override
  String get runContextLimitTitle => 'Context limit reached';

  @override
  String get runContextLimitDetail =>
      'The run could not continue within its context limit.';

  @override
  String get runProviderFailureTitle => 'Provider unavailable';

  @override
  String get runProviderFailureDetail =>
      'The model provider could not complete this run.';

  @override
  String get runToolFailureTitle => 'Tool failed';

  @override
  String get runToolFailureDetail => 'A tool could not complete this run.';

  @override
  String get runPolicyDeniedTitle => 'Action not allowed';

  @override
  String get runPolicyDeniedDetail =>
      'A policy prevented this run from continuing.';

  @override
  String get runRetriesExhaustedTitle => 'Retries exhausted';

  @override
  String get runRetriesExhaustedDetail =>
      'The run could not recover after its retry attempts.';

  @override
  String get runRecoveryInterruptedTitle => 'Recovery interrupted';

  @override
  String get runRecoveryInterruptedDetail =>
      'The run ended while the app was recovering it.';

  @override
  String get runSideEffectUncertainTitle => 'Result uncertain';

  @override
  String get runSideEffectUncertainDetail =>
      'The app could not confirm whether an action completed.';

  @override
  String get runApprovalDeliveryFailedTitle => 'Approval could not resume';

  @override
  String get runApprovalDeliveryFailedDetail =>
      'The approved action could not be delivered safely.';

  @override
  String get runInternalFailureTitle => 'Internal failure';

  @override
  String get runInternalFailureDetail => 'The app could not complete this run.';

  @override
  String runDispatchRetryNotice(int attempt, int maxAttempts) {
    return 'Starting attempt $attempt of $maxAttempts.';
  }

  @override
  String runRecoveryRetryNotice(int attempt, int maxAttempts) {
    return 'Recovering with attempt $attempt of $maxAttempts.';
  }

  @override
  String runRecoveryExhaustedNotice(int attempt, int maxAttempts) {
    return 'Recovery stopped after attempt $attempt of $maxAttempts.';
  }
}
