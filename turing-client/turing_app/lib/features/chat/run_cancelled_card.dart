import 'package:flutter/material.dart';

import '../../l10n/generated/app_localizations.dart';
import '../../l10n/run_state_localizations.dart';
import '../../models/run_lifecycle.dart';
import '../../models/run_state.dart';
import 'terminal_outcome_card.dart';

/// Presentational card for a terminal, cancelled/interrupted run.
///
/// The backend cancels or abandons a run server-side for a variety of
/// reasons this app classifies through [RunOutcomeReason] — a user's own
/// cancellation, an ambiguous `client_cancelled`/tool-cleanup abandonment,
/// approval expiry, and so on. Deliberately distinct from [RunFailureCard]:
/// a cancellation is not a failure and must never be announced as one.
/// Shares its visual chrome with [RunFailureCard],
/// [MessageSendUnconfirmedCard], and [MessageSendFailureCard] via
/// [TerminalOutcomeCard] — every non-routine outcome this screen reports
/// gets the same error-styled treatment.
///
/// Takes a semantic [RunOutcomeReason], never a raw message string — see
/// [RunFailureCard]'s own doc for why that matters. Live legacy events
/// first pass through the safe enum mapper, so this constructor can never
/// receive backend prose.
class RunCancelledCard extends StatelessWidget {
  const RunCancelledCard({super.key, required this.reason});

  final RunOutcomeReason reason;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    // `none` mirrors `localizedRunStateCopy`'s own fallback: an
    // unclassified cancellation still needs truthful lifecycle-level
    // copy, not the generic cross-lifecycle "outcome unavailable" wording.
    final copy = reason == RunOutcomeReason.none
        ? localizedRunLifecycleCopy(l10n, RunLifecycle.cancelled)
        : localizedRunOutcomeCopy(l10n, reason);
    return TerminalOutcomeCard(outcomeLabel: copy.title, message: copy.detail);
  }
}
