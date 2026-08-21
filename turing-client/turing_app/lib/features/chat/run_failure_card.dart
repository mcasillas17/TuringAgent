import 'package:flutter/material.dart';

import '../../l10n/generated/app_localizations.dart';
import '../../l10n/run_state_localizations.dart';
import '../../models/run_lifecycle.dart';
import '../../models/run_state.dart';
import 'terminal_outcome_card.dart';

/// Presentational card for a terminal, failed run.
///
/// Deliberately distinct from [RunNoticeCard]: a failure is not routine
/// progress (a retry, a step, giving up after N attempts) and must not be
/// mistaken for one, so it gets its own error-styled card rather than
/// reusing the neutral notice. Shares its visual chrome with
/// [RunCancelledCard], [MessageSendUnconfirmedCard], and
/// [MessageSendFailureCard] via [TerminalOutcomeCard] — the same
/// non-routine, error-styled treatment.
///
/// Takes a semantic [RunOutcomeReason], never a raw message string: every
/// live event this app decodes passes through the safe backend-enum mapper
/// (`GrpcMappers.runOutcomeReasonToModel`) or, for a defensively-handled
/// legacy event with no [RunState] at all, this app's own safe payload
/// mapper — neither can ever hand this widget arbitrary backend prose.
/// Both label and detail are resolved through generated localization, so
/// this card can never render a raw backend `message`, `note`, `reason`,
/// `code`, or numeric enum value.
class RunFailureCard extends StatelessWidget {
  const RunFailureCard({super.key, required this.reason});

  final RunOutcomeReason reason;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    // `none` mirrors `localizedRunStateCopy`'s own fallback: a failure with
    // no more specific classified reason still needs truthful "Run failed"
    // copy, not the generic cross-lifecycle "outcome unavailable" wording
    // `localizedRunOutcomeCopy` would otherwise give every unclassified
    // reason alike.
    final copy = reason == RunOutcomeReason.none
        ? localizedRunLifecycleCopy(l10n, RunLifecycle.failed)
        : localizedRunOutcomeCopy(l10n, reason);
    return TerminalOutcomeCard(outcomeLabel: copy.title, message: copy.detail);
  }
}
