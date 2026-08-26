import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:intl/intl.dart' as intl;

import 'app_localizations_en.dart';

// ignore_for_file: type=lint

/// Callers can lookup localized strings with an instance of AppLocalizations
/// returned by `AppLocalizations.of(context)`.
///
/// Applications need to include `AppLocalizations.delegate()` in their app's
/// `localizationDelegates` list, and the locales they support in the app's
/// `supportedLocales` list. For example:
///
/// ```dart
/// import 'generated/app_localizations.dart';
///
/// return MaterialApp(
///   localizationsDelegates: AppLocalizations.localizationsDelegates,
///   supportedLocales: AppLocalizations.supportedLocales,
///   home: MyApplicationHome(),
/// );
/// ```
///
/// ## Update pubspec.yaml
///
/// Please make sure to update your pubspec.yaml to include the following
/// packages:
///
/// ```yaml
/// dependencies:
///   # Internationalization support.
///   flutter_localizations:
///     sdk: flutter
///   intl: any # Use the pinned version from flutter_localizations
///
///   # Rest of dependencies
/// ```
///
/// ## iOS Applications
///
/// iOS applications define key application metadata, including supported
/// locales, in an Info.plist file that is built into the application bundle.
/// To configure the locales supported by your app, you’ll need to edit this
/// file.
///
/// First, open your project’s ios/Runner.xcworkspace Xcode workspace file.
/// Then, in the Project Navigator, open the Info.plist file under the Runner
/// project’s Runner folder.
///
/// Next, select the Information Property List item, select Add Item from the
/// Editor menu, then select Localizations from the pop-up menu.
///
/// Select and expand the newly-created Localizations item then, for each
/// locale your application supports, add a new item and select the locale
/// you wish to add from the pop-up menu in the Value field. This list should
/// be consistent with the languages listed in the AppLocalizations.supportedLocales
/// property.
abstract class AppLocalizations {
  AppLocalizations(String locale)
    : localeName = intl.Intl.canonicalizedLocale(locale.toString());

  final String localeName;

  static AppLocalizations of(BuildContext context) {
    return Localizations.of<AppLocalizations>(context, AppLocalizations)!;
  }

  static const LocalizationsDelegate<AppLocalizations> delegate =
      _AppLocalizationsDelegate();

  /// A list of this localizations delegate along with the default localizations
  /// delegates.
  ///
  /// Returns a list of localizations delegates containing this delegate along with
  /// GlobalMaterialLocalizations.delegate, GlobalCupertinoLocalizations.delegate,
  /// and GlobalWidgetsLocalizations.delegate.
  ///
  /// Additional delegates can be added by appending to this list in
  /// MaterialApp. This list does not have to be used at all if a custom list
  /// of delegates is preferred or required.
  static const List<LocalizationsDelegate<dynamic>> localizationsDelegates =
      <LocalizationsDelegate<dynamic>>[
        delegate,
        GlobalMaterialLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
      ];

  /// A list of this localizations delegate's supported locales.
  static const List<Locale> supportedLocales = <Locale>[Locale('en')];

  /// No description provided for @runStatusUnavailableTitle.
  ///
  /// In en, this message translates to:
  /// **'Run status unavailable'**
  String get runStatusUnavailableTitle;

  /// No description provided for @runStatusUnavailableDetail.
  ///
  /// In en, this message translates to:
  /// **'This app cannot identify the run\'s current status.'**
  String get runStatusUnavailableDetail;

  /// No description provided for @runQueuedTitle.
  ///
  /// In en, this message translates to:
  /// **'Queued'**
  String get runQueuedTitle;

  /// No description provided for @runQueuedDetail.
  ///
  /// In en, this message translates to:
  /// **'The run is waiting to start.'**
  String get runQueuedDetail;

  /// No description provided for @runRunningTitle.
  ///
  /// In en, this message translates to:
  /// **'Working'**
  String get runRunningTitle;

  /// No description provided for @runRunningDetail.
  ///
  /// In en, this message translates to:
  /// **'The assistant is working on this run.'**
  String get runRunningDetail;

  /// No description provided for @runWaitingApprovalTitle.
  ///
  /// In en, this message translates to:
  /// **'Waiting for approval'**
  String get runWaitingApprovalTitle;

  /// No description provided for @runWaitingApprovalDetail.
  ///
  /// In en, this message translates to:
  /// **'The run is paused until an approval is decided.'**
  String get runWaitingApprovalDetail;

  /// No description provided for @runRecoveringTitle.
  ///
  /// In en, this message translates to:
  /// **'Recovering'**
  String get runRecoveringTitle;

  /// No description provided for @runRecoveringDetail.
  ///
  /// In en, this message translates to:
  /// **'The app is recovering this run after an interruption.'**
  String get runRecoveringDetail;

  /// No description provided for @runCompletedTitle.
  ///
  /// In en, this message translates to:
  /// **'Completed'**
  String get runCompletedTitle;

  /// No description provided for @runCompletedDetail.
  ///
  /// In en, this message translates to:
  /// **'The assistant response is complete.'**
  String get runCompletedDetail;

  /// No description provided for @runCompletedContentUnavailableTitle.
  ///
  /// In en, this message translates to:
  /// **'Response unavailable'**
  String get runCompletedContentUnavailableTitle;

  /// No description provided for @runCompletedContentUnavailableDetail.
  ///
  /// In en, this message translates to:
  /// **'The run completed, but the saved assistant response could not be loaded.'**
  String get runCompletedContentUnavailableDetail;

  /// No description provided for @runCompletedNoContentTitle.
  ///
  /// In en, this message translates to:
  /// **'Completed'**
  String get runCompletedNoContentTitle;

  /// No description provided for @runCompletedNoContentDetail.
  ///
  /// In en, this message translates to:
  /// **'No assistant response was recorded.'**
  String get runCompletedNoContentDetail;

  /// No description provided for @runFailedTitle.
  ///
  /// In en, this message translates to:
  /// **'Run failed'**
  String get runFailedTitle;

  /// No description provided for @runFailedDetail.
  ///
  /// In en, this message translates to:
  /// **'The run ended before it could complete.'**
  String get runFailedDetail;

  /// No description provided for @runCancelledTitle.
  ///
  /// In en, this message translates to:
  /// **'Run interrupted'**
  String get runCancelledTitle;

  /// No description provided for @runCancelledDetail.
  ///
  /// In en, this message translates to:
  /// **'The run ended before it could finish.'**
  String get runCancelledDetail;

  /// No description provided for @runOutcomeUnavailableTitle.
  ///
  /// In en, this message translates to:
  /// **'Outcome unavailable'**
  String get runOutcomeUnavailableTitle;

  /// No description provided for @runOutcomeUnavailableDetail.
  ///
  /// In en, this message translates to:
  /// **'This app cannot identify why the run ended.'**
  String get runOutcomeUnavailableDetail;

  /// No description provided for @runNoResponseTitle.
  ///
  /// In en, this message translates to:
  /// **'No response recorded'**
  String get runNoResponseTitle;

  /// No description provided for @runNoResponseDetail.
  ///
  /// In en, this message translates to:
  /// **'No assistant response was recorded for this run.'**
  String get runNoResponseDetail;

  /// No description provided for @runOutcomeNoneTitle.
  ///
  /// In en, this message translates to:
  /// **'No terminal outcome'**
  String get runOutcomeNoneTitle;

  /// No description provided for @runOutcomeNoneDetail.
  ///
  /// In en, this message translates to:
  /// **'The run has no terminal outcome.'**
  String get runOutcomeNoneDetail;

  /// No description provided for @runUserCancelledTitle.
  ///
  /// In en, this message translates to:
  /// **'Run cancelled'**
  String get runUserCancelledTitle;

  /// No description provided for @runUserCancelledDetail.
  ///
  /// In en, this message translates to:
  /// **'You cancelled this run.'**
  String get runUserCancelledDetail;

  /// No description provided for @runAbandonedTitle.
  ///
  /// In en, this message translates to:
  /// **'Run interrupted'**
  String get runAbandonedTitle;

  /// No description provided for @runAbandonedDetail.
  ///
  /// In en, this message translates to:
  /// **'The run ended before it could finish.'**
  String get runAbandonedDetail;

  /// No description provided for @runExpiredTitle.
  ///
  /// In en, this message translates to:
  /// **'Run expired'**
  String get runExpiredTitle;

  /// No description provided for @runExpiredDetail.
  ///
  /// In en, this message translates to:
  /// **'The run expired before it could finish.'**
  String get runExpiredDetail;

  /// No description provided for @runContextLimitTitle.
  ///
  /// In en, this message translates to:
  /// **'Context limit reached'**
  String get runContextLimitTitle;

  /// No description provided for @runContextLimitDetail.
  ///
  /// In en, this message translates to:
  /// **'The run could not continue within its context limit.'**
  String get runContextLimitDetail;

  /// No description provided for @runProviderFailureTitle.
  ///
  /// In en, this message translates to:
  /// **'Provider unavailable'**
  String get runProviderFailureTitle;

  /// No description provided for @runProviderFailureDetail.
  ///
  /// In en, this message translates to:
  /// **'The model provider could not complete this run.'**
  String get runProviderFailureDetail;

  /// No description provided for @runToolFailureTitle.
  ///
  /// In en, this message translates to:
  /// **'Tool failed'**
  String get runToolFailureTitle;

  /// No description provided for @runToolFailureDetail.
  ///
  /// In en, this message translates to:
  /// **'A tool could not complete this run.'**
  String get runToolFailureDetail;

  /// No description provided for @runPolicyDeniedTitle.
  ///
  /// In en, this message translates to:
  /// **'Action not allowed'**
  String get runPolicyDeniedTitle;

  /// No description provided for @runPolicyDeniedDetail.
  ///
  /// In en, this message translates to:
  /// **'A policy prevented this run from continuing.'**
  String get runPolicyDeniedDetail;

  /// No description provided for @runRetriesExhaustedTitle.
  ///
  /// In en, this message translates to:
  /// **'Retries exhausted'**
  String get runRetriesExhaustedTitle;

  /// No description provided for @runRetriesExhaustedDetail.
  ///
  /// In en, this message translates to:
  /// **'The run could not recover after its retry attempts.'**
  String get runRetriesExhaustedDetail;

  /// No description provided for @runRecoveryInterruptedTitle.
  ///
  /// In en, this message translates to:
  /// **'Recovery interrupted'**
  String get runRecoveryInterruptedTitle;

  /// No description provided for @runRecoveryInterruptedDetail.
  ///
  /// In en, this message translates to:
  /// **'The run ended while the app was recovering it.'**
  String get runRecoveryInterruptedDetail;

  /// No description provided for @runSideEffectUncertainTitle.
  ///
  /// In en, this message translates to:
  /// **'Result uncertain'**
  String get runSideEffectUncertainTitle;

  /// No description provided for @runSideEffectUncertainDetail.
  ///
  /// In en, this message translates to:
  /// **'The app could not confirm whether an action completed.'**
  String get runSideEffectUncertainDetail;

  /// No description provided for @runApprovalDeliveryFailedTitle.
  ///
  /// In en, this message translates to:
  /// **'Approval could not resume'**
  String get runApprovalDeliveryFailedTitle;

  /// No description provided for @runApprovalDeliveryFailedDetail.
  ///
  /// In en, this message translates to:
  /// **'The approved action could not be delivered safely.'**
  String get runApprovalDeliveryFailedDetail;

  /// No description provided for @runInternalFailureTitle.
  ///
  /// In en, this message translates to:
  /// **'Internal failure'**
  String get runInternalFailureTitle;

  /// No description provided for @runInternalFailureDetail.
  ///
  /// In en, this message translates to:
  /// **'The app could not complete this run.'**
  String get runInternalFailureDetail;

  /// No description provided for @runDispatchRetryNotice.
  ///
  /// In en, this message translates to:
  /// **'Starting attempt {attempt} of {maxAttempts}.'**
  String runDispatchRetryNotice(int attempt, int maxAttempts);

  /// No description provided for @runRecoveryRetryNotice.
  ///
  /// In en, this message translates to:
  /// **'Recovering with attempt {attempt} of {maxAttempts}.'**
  String runRecoveryRetryNotice(int attempt, int maxAttempts);

  /// No description provided for @runRecoveryExhaustedNotice.
  ///
  /// In en, this message translates to:
  /// **'Recovery stopped after attempt {attempt} of {maxAttempts}.'**
  String runRecoveryExhaustedNotice(int attempt, int maxAttempts);

  /// No description provided for @memoryPageTitle.
  ///
  /// In en, this message translates to:
  /// **'Memory'**
  String get memoryPageTitle;

  /// No description provided for @memoryPageSubtitle.
  ///
  /// In en, this message translates to:
  /// **'Memory is a vault of Markdown files you can open and edit yourself. Turing reads it, proposes additions into the inbox, and writes nothing you have not accepted. persona.md is yours alone.'**
  String get memoryPageSubtitle;

  /// No description provided for @memoryEnabledTitle.
  ///
  /// In en, this message translates to:
  /// **'Memory is on'**
  String get memoryEnabledTitle;

  /// No description provided for @memoryDisabledTitle.
  ///
  /// In en, this message translates to:
  /// **'Memory is off'**
  String get memoryDisabledTitle;

  /// No description provided for @memoryEnabledDetail.
  ///
  /// In en, this message translates to:
  /// **'Turing pins persona.md and profile.md into each run and can search accepted beliefs.'**
  String get memoryEnabledDetail;

  /// No description provided for @memoryDisabledDetail.
  ///
  /// In en, this message translates to:
  /// **'Memory tools are unavailable while memory is off, and nothing is pinned into a run. The vault stays on disk and stays readable here.'**
  String get memoryDisabledDetail;

  /// No description provided for @memoryReasonUnspecified.
  ///
  /// In en, this message translates to:
  /// **'The server did not say whether this could be read.'**
  String get memoryReasonUnspecified;

  /// No description provided for @memoryReasonNone.
  ///
  /// In en, this message translates to:
  /// **'Readable.'**
  String get memoryReasonNone;

  /// No description provided for @memoryReasonDisabled.
  ///
  /// In en, this message translates to:
  /// **'Memory is off, so this is not in use.'**
  String get memoryReasonDisabled;

  /// No description provided for @memoryReasonVaultMissing.
  ///
  /// In en, this message translates to:
  /// **'This is not in the vault yet.'**
  String get memoryReasonVaultMissing;

  /// No description provided for @memoryReasonVaultUnreadable.
  ///
  /// In en, this message translates to:
  /// **'This could not be read from the vault.'**
  String get memoryReasonVaultUnreadable;

  /// No description provided for @memoryReasonContentParseFailed.
  ///
  /// In en, this message translates to:
  /// **'This could not be parsed.'**
  String get memoryReasonContentParseFailed;

  /// No description provided for @memoryReasonContentTooLarge.
  ///
  /// In en, this message translates to:
  /// **'This is too large to read.'**
  String get memoryReasonContentTooLarge;

  /// Shown above a persona.md or profile.md editor whose document is longer than the runtime pin budget. The editor holds the whole file; this says how much of it a model actually sees.
  ///
  /// In en, this message translates to:
  /// **'Longer than a run carries: only the first {bytes} bytes reach a conversation. The whole document is here, and saving keeps all of it.'**
  String memoryPinnedTruncated(int bytes);

  /// No description provided for @memoryStatusUnspecified.
  ///
  /// In en, this message translates to:
  /// **'The server did not say who may write this.'**
  String get memoryStatusUnspecified;

  /// No description provided for @memoryStatusManaged.
  ///
  /// In en, this message translates to:
  /// **'Turing may rewrite this file.'**
  String get memoryStatusManaged;

  /// No description provided for @memoryStatusUnmanaged.
  ///
  /// In en, this message translates to:
  /// **'You have taken this note over; Turing reads it and never writes it.'**
  String get memoryStatusUnmanaged;

  /// No description provided for @memoryStatusWithdrawn.
  ///
  /// In en, this message translates to:
  /// **'Withdrawn.'**
  String get memoryStatusWithdrawn;

  /// No description provided for @memoryCandidateStateUnspecified.
  ///
  /// In en, this message translates to:
  /// **'The server did not say where this proposal stands.'**
  String get memoryCandidateStateUnspecified;

  /// No description provided for @memoryCandidateStatePending.
  ///
  /// In en, this message translates to:
  /// **'Waiting for you.'**
  String get memoryCandidateStatePending;

  /// No description provided for @memoryCandidateStatePromoted.
  ///
  /// In en, this message translates to:
  /// **'Accepted.'**
  String get memoryCandidateStatePromoted;

  /// No description provided for @memoryCandidateStateRejected.
  ///
  /// In en, this message translates to:
  /// **'Rejected.'**
  String get memoryCandidateStateRejected;

  /// No description provided for @memoryCandidateStateWithdrawn.
  ///
  /// In en, this message translates to:
  /// **'The conversation behind this was deleted, so it can no longer be accepted.'**
  String get memoryCandidateStateWithdrawn;

  /// No description provided for @memoryCandidateKindUnspecified.
  ///
  /// In en, this message translates to:
  /// **'Proposal'**
  String get memoryCandidateKindUnspecified;

  /// No description provided for @memoryCandidateKindBelief.
  ///
  /// In en, this message translates to:
  /// **'Proposed belief'**
  String get memoryCandidateKindBelief;

  /// No description provided for @memoryCandidateKindProfileEdit.
  ///
  /// In en, this message translates to:
  /// **'Proposed change to profile.md'**
  String get memoryCandidateKindProfileEdit;

  /// No description provided for @memoryTierUnspecified.
  ///
  /// In en, this message translates to:
  /// **'Memory'**
  String get memoryTierUnspecified;

  /// No description provided for @memoryTierPersona.
  ///
  /// In en, this message translates to:
  /// **'Persona'**
  String get memoryTierPersona;

  /// No description provided for @memoryTierProfile.
  ///
  /// In en, this message translates to:
  /// **'Profile'**
  String get memoryTierProfile;

  /// No description provided for @memoryTierBelief.
  ///
  /// In en, this message translates to:
  /// **'Beliefs'**
  String get memoryTierBelief;

  /// No description provided for @memoryTierNote.
  ///
  /// In en, this message translates to:
  /// **'Notes'**
  String get memoryTierNote;

  /// No description provided for @memoryPersonaHeading.
  ///
  /// In en, this message translates to:
  /// **'persona.md'**
  String get memoryPersonaHeading;

  /// No description provided for @memoryPersonaDescription.
  ///
  /// In en, this message translates to:
  /// **'Who Turing is. You are its only author: Turing never writes it, and no proposal can reach it. It is pinned into every run as written.'**
  String get memoryPersonaDescription;

  /// No description provided for @memoryProfileHeading.
  ///
  /// In en, this message translates to:
  /// **'profile.md'**
  String get memoryProfileHeading;

  /// No description provided for @memoryProfileDescription.
  ///
  /// In en, this message translates to:
  /// **'What Turing knows about you. You write it here; Turing can only propose changes into the inbox for you to accept.'**
  String get memoryProfileDescription;

  /// No description provided for @memorySaveAction.
  ///
  /// In en, this message translates to:
  /// **'Save'**
  String get memorySaveAction;

  /// No description provided for @memoryRereadAction.
  ///
  /// In en, this message translates to:
  /// **'Re-read from the vault'**
  String get memoryRereadAction;

  /// No description provided for @memoryUnmanagedDraftTitle.
  ///
  /// In en, this message translates to:
  /// **'Your own draft'**
  String get memoryUnmanagedDraftTitle;

  /// No description provided for @memoryUnmanagedDraftDetail.
  ///
  /// In en, this message translates to:
  /// **'Turing did not create this file and will not move it. To accept it, move the file into beliefs/ in your vault.'**
  String get memoryUnmanagedDraftDetail;

  /// No description provided for @memoryNoteUnsearchable.
  ///
  /// In en, this message translates to:
  /// **'This note is not searchable until the problem above is fixed.'**
  String get memoryNoteUnsearchable;

  /// No description provided for @memoryNoteWithdrawnEvidence.
  ///
  /// In en, this message translates to:
  /// **'The evidence behind this was withdrawn with its conversation.'**
  String get memoryNoteWithdrawnEvidence;

  /// No description provided for @memoryNoteNoEvidence.
  ///
  /// In en, this message translates to:
  /// **'Kept with no evidence behind it.'**
  String get memoryNoteNoEvidence;

  /// No description provided for @memoryEvidenceCount.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =1{1 piece of evidence} other{{count} pieces of evidence}}'**
  String memoryEvidenceCount(int count);

  /// No description provided for @memoryInboxHeading.
  ///
  /// In en, this message translates to:
  /// **'Inbox'**
  String get memoryInboxHeading;

  /// No description provided for @memoryInboxEmpty.
  ///
  /// In en, this message translates to:
  /// **'Nothing is waiting for you.'**
  String get memoryInboxEmpty;

  /// No description provided for @memoryBeliefsHeading.
  ///
  /// In en, this message translates to:
  /// **'Beliefs'**
  String get memoryBeliefsHeading;

  /// No description provided for @memoryBeliefsEmpty.
  ///
  /// In en, this message translates to:
  /// **'No accepted beliefs yet.'**
  String get memoryBeliefsEmpty;

  /// No description provided for @memoryPromoteAction.
  ///
  /// In en, this message translates to:
  /// **'Promote'**
  String get memoryPromoteAction;

  /// No description provided for @memoryRejectAction.
  ///
  /// In en, this message translates to:
  /// **'Reject'**
  String get memoryRejectAction;

  /// No description provided for @memoryApplyAction.
  ///
  /// In en, this message translates to:
  /// **'Apply'**
  String get memoryApplyAction;

  /// No description provided for @memoryProposalUnreadable.
  ///
  /// In en, this message translates to:
  /// **'This proposal could not be read in full, so there is nothing here to accept.'**
  String get memoryProposalUnreadable;

  /// No description provided for @memoryProposalUndecidable.
  ///
  /// In en, this message translates to:
  /// **'This proposal is in a shape this version of Turing does not understand, so there is nothing safe to offer here. Open it in your vault, or update Turing.'**
  String get memoryProposalUndecidable;

  /// No description provided for @memoryProfileResultHeading.
  ///
  /// In en, this message translates to:
  /// **'The profile after applying this'**
  String get memoryProfileResultHeading;

  /// No description provided for @memoryProfileResultDescription.
  ///
  /// In en, this message translates to:
  /// **'This is what profile.md will say. It starts as your profile with the proposal added; edit it here and Apply writes exactly what you see.'**
  String get memoryProfileResultDescription;

  /// No description provided for @memoryProfileResultEmpty.
  ///
  /// In en, this message translates to:
  /// **'Add the text you want profile.md to hold before applying.'**
  String get memoryProfileResultEmpty;

  /// No description provided for @memoryExpectedProposalHash.
  ///
  /// In en, this message translates to:
  /// **'Applies only while the proposal still matches {hash}.'**
  String memoryExpectedProposalHash(String hash);

  /// No description provided for @memoryExpectedProfileHash.
  ///
  /// In en, this message translates to:
  /// **'Applies only while profile.md still matches {hash}.'**
  String memoryExpectedProfileHash(String hash);

  /// No description provided for @memoryBackendUnreachable.
  ///
  /// In en, this message translates to:
  /// **'Could not reach the backend'**
  String get memoryBackendUnreachable;

  /// No description provided for @memoryEditingVersion.
  ///
  /// In en, this message translates to:
  /// **'Editing version {hash}'**
  String memoryEditingVersion(String hash);

  /// No description provided for @memoryLastChanged.
  ///
  /// In en, this message translates to:
  /// **'Last changed {date} at {time}'**
  String memoryLastChanged(DateTime date, DateTime time);

  /// No description provided for @memoryProvenanceFrom.
  ///
  /// In en, this message translates to:
  /// **'From {source}'**
  String memoryProvenanceFrom(String source);

  /// No description provided for @memoryNoProfileYet.
  ///
  /// In en, this message translates to:
  /// **'(no profile yet)'**
  String get memoryNoProfileYet;

  /// No description provided for @memorySaveUnavailable.
  ///
  /// In en, this message translates to:
  /// **'This document cannot be saved from here until the problem above is fixed.'**
  String get memorySaveUnavailable;

  /// No description provided for @memorySaveNeedsVault.
  ///
  /// In en, this message translates to:
  /// **'Open or configure a vault before saving here.'**
  String get memorySaveNeedsVault;

  /// No description provided for @memoryTierItemCount.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =0{Nothing is in this tier.} =1{1 item in this tier.} other{{count} items in this tier.}}'**
  String memoryTierItemCount(int count);

  /// No description provided for @memoryTierPendingCount.
  ///
  /// In en, this message translates to:
  /// **'{count, plural, =0{No proposals are waiting for this tier.} =1{1 proposal is waiting for this tier.} other{{count} proposals are waiting for this tier.}}'**
  String memoryTierPendingCount(int count);

  /// No description provided for @egressDialogTitleUnknownHost.
  ///
  /// In en, this message translates to:
  /// **'Send data off this machine?'**
  String get egressDialogTitleUnknownHost;

  /// No description provided for @egressDialogTitle.
  ///
  /// In en, this message translates to:
  /// **'Send data to {host}?'**
  String egressDialogTitle(String host);

  /// No description provided for @egressDialogMaySendHeading.
  ///
  /// In en, this message translates to:
  /// **'This run may send:'**
  String get egressDialogMaySendHeading;

  /// No description provided for @egressDialogMcpHeading.
  ///
  /// In en, this message translates to:
  /// **'Remote MCP destinations:'**
  String get egressDialogMcpHeading;

  /// No description provided for @egressDialogIntegrationHeading.
  ///
  /// In en, this message translates to:
  /// **'Connected-account destinations:'**
  String get egressDialogIntegrationHeading;

  /// No description provided for @egressDialogSkillsHeading.
  ///
  /// In en, this message translates to:
  /// **'Skills that may be sent:'**
  String get egressDialogSkillsHeading;

  /// No description provided for @egressSkillBodyMayBeSent.
  ///
  /// In en, this message translates to:
  /// **'full content may be sent'**
  String get egressSkillBodyMayBeSent;

  /// No description provided for @egressSkillNameOnly.
  ///
  /// In en, this message translates to:
  /// **'name and description only'**
  String get egressSkillNameOnly;

  /// No description provided for @egressDialogSingleRunNotice.
  ///
  /// In en, this message translates to:
  /// **'This consent applies only to this exact run.'**
  String get egressDialogSingleRunNotice;

  /// No description provided for @egressDialogExpiry.
  ///
  /// In en, this message translates to:
  /// **'Confirm before {date} at {time}.'**
  String egressDialogExpiry(DateTime date, DateTime time);

  /// No description provided for @egressDialogCancel.
  ///
  /// In en, this message translates to:
  /// **'Cancel'**
  String get egressDialogCancel;

  /// No description provided for @egressDialogSend.
  ///
  /// In en, this message translates to:
  /// **'Send'**
  String get egressDialogSend;

  /// No description provided for @egressCategoryCurrentMessage.
  ///
  /// In en, this message translates to:
  /// **'Current message'**
  String get egressCategoryCurrentMessage;

  /// No description provided for @egressCategoryConversationHistory.
  ///
  /// In en, this message translates to:
  /// **'Conversation history'**
  String get egressCategoryConversationHistory;

  /// No description provided for @egressCategoryCrossSessionRecall.
  ///
  /// In en, this message translates to:
  /// **'Cross-session recall'**
  String get egressCategoryCrossSessionRecall;

  /// No description provided for @egressCategoryMemoryProfile.
  ///
  /// In en, this message translates to:
  /// **'Memory and profile'**
  String get egressCategoryMemoryProfile;

  /// No description provided for @egressCategorySkillContent.
  ///
  /// In en, this message translates to:
  /// **'Enabled skill content'**
  String get egressCategorySkillContent;

  /// No description provided for @egressCategoryToolSchemas.
  ///
  /// In en, this message translates to:
  /// **'Tool schemas'**
  String get egressCategoryToolSchemas;

  /// No description provided for @egressCategoryToolArguments.
  ///
  /// In en, this message translates to:
  /// **'Tool arguments'**
  String get egressCategoryToolArguments;

  /// No description provided for @egressCategoryToolResults.
  ///
  /// In en, this message translates to:
  /// **'Tool results'**
  String get egressCategoryToolResults;

  /// No description provided for @egressCategoryAttachments.
  ///
  /// In en, this message translates to:
  /// **'Attachments'**
  String get egressCategoryAttachments;

  /// No description provided for @egressMemoryTierUnspecified.
  ///
  /// In en, this message translates to:
  /// **'Memory'**
  String get egressMemoryTierUnspecified;

  /// No description provided for @egressMemoryTierPersona.
  ///
  /// In en, this message translates to:
  /// **'Persona'**
  String get egressMemoryTierPersona;

  /// No description provided for @egressMemoryTierProfile.
  ///
  /// In en, this message translates to:
  /// **'Profile'**
  String get egressMemoryTierProfile;

  /// No description provided for @egressMemoryTierBelief.
  ///
  /// In en, this message translates to:
  /// **'Belief'**
  String get egressMemoryTierBelief;

  /// No description provided for @egressMemoryTierNote.
  ///
  /// In en, this message translates to:
  /// **'Note'**
  String get egressMemoryTierNote;

  /// No description provided for @memoryEgressPinnedHeading.
  ///
  /// In en, this message translates to:
  /// **'Memory pinned into this run:'**
  String get memoryEgressPinnedHeading;

  /// No description provided for @memoryEgressPinnedDetail.
  ///
  /// In en, this message translates to:
  /// **'These documents are in the prompt as written.'**
  String get memoryEgressPinnedDetail;

  /// No description provided for @memoryEgressReachableHeading.
  ///
  /// In en, this message translates to:
  /// **'Memory the memory tools can reach:'**
  String get memoryEgressReachableHeading;

  /// No description provided for @memoryEgressReachableDetail.
  ///
  /// In en, this message translates to:
  /// **'Nothing here is in the prompt. A tool call would have to go and read it, and whatever it read would then be part of this run.'**
  String get memoryEgressReachableDetail;

  /// No description provided for @memoryEgressToolsHeading.
  ///
  /// In en, this message translates to:
  /// **'The memory tools this run may call:'**
  String get memoryEgressToolsHeading;

  /// No description provided for @memoryEgressToolsDetail.
  ///
  /// In en, this message translates to:
  /// **'What those tools return is part of this run and may be sent with it.'**
  String get memoryEgressToolsDetail;

  /// No description provided for @memoryEgressUnnamed.
  ///
  /// In en, this message translates to:
  /// **'The server did not name which memory this run may send.'**
  String get memoryEgressUnnamed;

  /// No description provided for @memoryEgressBodyMayBeSent.
  ///
  /// In en, this message translates to:
  /// **'full content may be sent'**
  String get memoryEgressBodyMayBeSent;

  /// No description provided for @memoryEgressNameOnly.
  ///
  /// In en, this message translates to:
  /// **'name and location only'**
  String get memoryEgressNameOnly;
}

class _AppLocalizationsDelegate
    extends LocalizationsDelegate<AppLocalizations> {
  const _AppLocalizationsDelegate();

  @override
  Future<AppLocalizations> load(Locale locale) {
    return SynchronousFuture<AppLocalizations>(lookupAppLocalizations(locale));
  }

  @override
  bool isSupported(Locale locale) =>
      <String>['en'].contains(locale.languageCode);

  @override
  bool shouldReload(_AppLocalizationsDelegate old) => false;
}

AppLocalizations lookupAppLocalizations(Locale locale) {
  // Lookup logic when only language code is specified.
  switch (locale.languageCode) {
    case 'en':
      return AppLocalizationsEn();
  }

  throw FlutterError(
    'AppLocalizations.delegate failed to load unsupported locale "$locale". This is likely '
    'an issue with the localizations generation tool. Please file an issue '
    'on GitHub with a reproducible sample app and the gen-l10n configuration '
    'that was used.',
  );
}
