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
