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
  String get runCompletedContentUnavailableTitle => 'Response unavailable';

  @override
  String get runCompletedContentUnavailableDetail =>
      'The run completed, but the saved assistant response could not be loaded.';

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

  @override
  String get memoryPageTitle => 'Memory';

  @override
  String get memoryPageSubtitle =>
      'Memory is a vault of Markdown files you can open and edit yourself. Turing reads it, proposes additions into the inbox, and writes nothing you have not accepted. persona.md is yours alone.';

  @override
  String get memoryEnabledTitle => 'Memory is on';

  @override
  String get memoryDisabledTitle => 'Memory is off';

  @override
  String get memoryEnabledDetail =>
      'Turing pins persona.md and profile.md into each run and can search accepted beliefs.';

  @override
  String get memoryDisabledDetail =>
      'Memory tools are unavailable while memory is off, and nothing is pinned into a run. The vault stays on disk and stays readable here.';

  @override
  String get memoryVaultLocation => 'The vault is a folder on this computer:';

  @override
  String get memoryReasonUnspecified =>
      'The server did not say whether this could be read.';

  @override
  String get memoryReasonNone => 'Readable.';

  @override
  String get memoryReasonDisabled => 'Memory is off, so this is not in use.';

  @override
  String get memoryReasonVaultMissing => 'This is not in the vault yet.';

  @override
  String get memoryReasonVaultUnreadable =>
      'This could not be read from the vault.';

  @override
  String get memoryReasonContentParseFailed => 'This could not be parsed.';

  @override
  String get memoryReasonContentTooLarge => 'This is too large to read.';

  @override
  String memoryPinnedTruncated(int bytes) {
    return 'Longer than a run carries: only the first $bytes bytes reach a conversation. The whole document is here, and saving keeps all of it.';
  }

  @override
  String get memoryStatusUnspecified =>
      'The server did not say who may write this.';

  @override
  String get memoryStatusManaged => 'Turing may rewrite this file.';

  @override
  String get memoryStatusUnmanaged =>
      'You have taken this note over; Turing reads it and never writes it.';

  @override
  String get memoryStatusWithdrawn => 'Withdrawn.';

  @override
  String get memoryCandidateStateUnspecified =>
      'The server did not say where this proposal stands.';

  @override
  String get memoryCandidateStatePending => 'Waiting for you.';

  @override
  String get memoryCandidateStatePromoted => 'Accepted.';

  @override
  String get memoryCandidateStateRejected => 'Rejected.';

  @override
  String get memoryCandidateStateWithdrawn =>
      'The conversation behind this was deleted, so it can no longer be accepted.';

  @override
  String get memoryCandidateStateProfileApplying =>
      'You accepted this and it is being applied to profile.md. Turing is finishing it; there is nothing to decide here.';

  @override
  String get memoryCandidateKindUnspecified => 'Proposal';

  @override
  String get memoryCandidateKindBelief => 'Proposed belief';

  @override
  String get memoryCandidateKindProfileEdit => 'Proposed change to profile.md';

  @override
  String get memoryTierUnspecified => 'Memory';

  @override
  String get memoryTierPersona => 'Persona';

  @override
  String get memoryTierProfile => 'Profile';

  @override
  String get memoryTierBelief => 'Beliefs';

  @override
  String get memoryTierNote => 'Notes';

  @override
  String get memoryPersonaHeading => 'persona.md';

  @override
  String get memoryPersonaDescription =>
      'Who Turing is. You are its only author: Turing never writes it, and no proposal can reach it. It is pinned into every run as written.';

  @override
  String get memoryProfileHeading => 'profile.md';

  @override
  String get memoryProfileDescription =>
      'What Turing knows about you. You write it here; Turing can only propose changes into the inbox for you to accept.';

  @override
  String get memorySaveAction => 'Save';

  @override
  String get memoryRereadAction => 'Re-read from the vault';

  @override
  String get memoryUnmanagedDraftTitle => 'Your own draft';

  @override
  String get memoryUnmanagedDraftDetail =>
      'Turing did not create this file and will not move it. To accept it, move the file into beliefs/ in your vault.';

  @override
  String get memoryUntrackedInboxTitle =>
      'Turing wrote this file and lost track of it';

  @override
  String get memoryUntrackedInboxDetail =>
      'Turing wrote this proposal but has no record of it, so it cannot be accepted or rejected here. Read it in your vault and either move it into beliefs/ to keep it, or delete it.';

  @override
  String get memoryProposalDiscardOnly =>
      'This proposal could not be read in full, so there is nothing here to accept. You can still throw it away.';

  @override
  String get memoryProfileAppliedCleanupPending =>
      'Your profile was updated. The proposal it came from could not be removed from the inbox yet, so it is still listed below; Turing will clear it.';

  @override
  String get memoryNoteUnsearchable =>
      'This note is not searchable until the problem above is fixed.';

  @override
  String get memoryNoteWithdrawnEvidence =>
      'The evidence behind this was withdrawn with its conversation.';

  @override
  String get memoryNoteNoEvidence => 'Kept with no evidence behind it.';

  @override
  String memoryEvidenceCount(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count pieces of evidence',
      one: '1 piece of evidence',
    );
    return '$_temp0';
  }

  @override
  String get memoryInboxHeading => 'Inbox';

  @override
  String get memoryInboxEmpty => 'Nothing is waiting for you.';

  @override
  String get memoryBeliefsHeading => 'Beliefs';

  @override
  String get memoryBeliefsEmpty => 'No accepted beliefs yet.';

  @override
  String get memoryPromoteAction => 'Promote';

  @override
  String get memoryRejectAction => 'Reject';

  @override
  String get memoryApplyAction => 'Apply';

  @override
  String get memoryProposalUnreadable =>
      'This proposal could not be read in full, so there is nothing here to accept.';

  @override
  String get memoryProposalUnopenable =>
      'Turing could not read this file, so there is nothing here to decide. If it cannot open the file at all it cannot throw it away for you either: removing a file safely means proving which file is going, and that needs one Turing can open. Make it readable in your vault — or delete it there — and Turing will catch up.';

  @override
  String get memoryProposalUndecidable =>
      'This proposal is in a shape this version of Turing does not understand, so there is nothing safe to offer here. Open it in your vault, or update Turing.';

  @override
  String get memoryProfileResultHeading => 'The profile after applying this';

  @override
  String get memoryProfileResultDescription =>
      'This is what profile.md will say. It starts as your profile with the proposal added; edit it here and Apply writes exactly what you see.';

  @override
  String get memoryProfileResultEmpty =>
      'Add the text you want profile.md to hold before applying.';

  @override
  String memoryExpectedProposalHash(String hash) {
    return 'Applies only while the proposal still matches $hash.';
  }

  @override
  String memoryExpectedProfileHash(String hash) {
    return 'Applies only while profile.md still matches $hash.';
  }

  @override
  String get memoryBackendUnreachable => 'Could not reach the backend';

  @override
  String memoryEditingVersion(String hash) {
    return 'Editing version $hash';
  }

  @override
  String memoryLastChanged(DateTime date, DateTime time) {
    final intl.DateFormat dateDateFormat = intl.DateFormat.yMMMd(localeName);
    final String dateString = dateDateFormat.format(date);
    final intl.DateFormat timeDateFormat = intl.DateFormat.Hm(localeName);
    final String timeString = timeDateFormat.format(time);

    return 'Last changed $dateString at $timeString';
  }

  @override
  String memoryProvenanceFrom(String source) {
    return 'From $source';
  }

  @override
  String get memoryNoProfileYet => '(no profile yet)';

  @override
  String get memorySaveUnavailable =>
      'This document cannot be saved from here until the problem above is fixed.';

  @override
  String get memorySaveNeedsVault =>
      'The memory vault is not writable right now, so this cannot be saved.';

  @override
  String memoryTierItemCount(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count items in this tier.',
      one: '1 item in this tier.',
      zero: 'Nothing is in this tier.',
    );
    return '$_temp0';
  }

  @override
  String memoryTierPendingCount(int count) {
    String _temp0 = intl.Intl.pluralLogic(
      count,
      locale: localeName,
      other: '$count proposals are waiting for this tier.',
      one: '1 proposal is waiting for this tier.',
      zero: 'No proposals are waiting for this tier.',
    );
    return '$_temp0';
  }

  @override
  String get egressDialogTitleUnknownHost => 'Send data off this machine?';

  @override
  String egressDialogTitle(String host) {
    return 'Send data to $host?';
  }

  @override
  String get egressDialogMaySendHeading => 'This run may send:';

  @override
  String get egressDialogMcpHeading => 'Remote MCP destinations:';

  @override
  String get egressDialogIntegrationHeading =>
      'Connected-account destinations:';

  @override
  String get egressDialogSkillsHeading => 'Skills that may be sent:';

  @override
  String get egressSkillBodyMayBeSent => 'full content may be sent';

  @override
  String get egressSkillNameOnly => 'name and description only';

  @override
  String get egressDialogSingleRunNotice =>
      'This consent applies only to this exact run.';

  @override
  String egressDialogExpiry(DateTime date, DateTime time) {
    final intl.DateFormat dateDateFormat = intl.DateFormat.yMMMd(localeName);
    final String dateString = dateDateFormat.format(date);
    final intl.DateFormat timeDateFormat = intl.DateFormat.Hm(localeName);
    final String timeString = timeDateFormat.format(time);

    return 'Confirm before $dateString at $timeString.';
  }

  @override
  String get egressDialogCancel => 'Cancel';

  @override
  String get egressDialogSend => 'Send';

  @override
  String get egressCategoryCurrentMessage => 'Current message';

  @override
  String get egressCategoryConversationHistory => 'Conversation history';

  @override
  String get egressCategoryCrossSessionRecall => 'Cross-session recall';

  @override
  String get egressCategoryMemoryProfile => 'Memory and profile';

  @override
  String get egressCategorySkillContent => 'Enabled skill content';

  @override
  String get egressCategoryToolSchemas => 'Tool schemas';

  @override
  String get egressCategoryToolArguments => 'Tool arguments';

  @override
  String get egressCategoryToolResults => 'Tool results';

  @override
  String get egressCategoryAttachments => 'Attachments';

  @override
  String get egressMemoryTierUnspecified => 'Memory';

  @override
  String get egressMemoryTierPersona => 'Persona';

  @override
  String get egressMemoryTierProfile => 'Profile';

  @override
  String get egressMemoryTierBelief => 'Belief';

  @override
  String get egressMemoryTierNote => 'Note';

  @override
  String get memoryEgressPinnedHeading => 'Memory pinned into this run:';

  @override
  String get memoryEgressPinnedDetail =>
      'These documents are in the prompt as written.';

  @override
  String get memoryEgressReachableHeading =>
      'Memory the memory tools can reach:';

  @override
  String get memoryEgressReachableDetail =>
      'Nothing here is in the prompt. A tool call would have to go and read it, and whatever it read would then be part of this run.';

  @override
  String get memoryEgressToolsHeading => 'The memory tools this run may call:';

  @override
  String get memoryEgressToolsDetail =>
      'What those tools return is part of this run and may be sent with it.';

  @override
  String get memoryEgressUnnamed =>
      'The server did not name which memory this run may send.';

  @override
  String get memoryEgressBodyMayBeSent => 'full content may be sent';

  @override
  String get memoryEgressNameOnly => 'name and location only';
}
