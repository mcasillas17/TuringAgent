import 'package:flutter/material.dart';

import 'terminal_outcome_card.dart';

/// Presentational card for a terminal `agent.run.failed` event.
///
/// Deliberately distinct from [RunNoticeCard]: a failure is not routine
/// progress (a retry, a step, giving up after N attempts) and must not be
/// mistaken for one, so it gets its own error-styled card rather than
/// reusing the neutral notice. Shares its visual chrome with
/// [RunCancelledCard], [MessageSendUnconfirmedCard], and
/// [MessageSendFailureCard] via [TerminalOutcomeCard] — the same
/// non-routine, error-styled treatment — but keeps its own truthful "Run
/// failed" wording.
class RunFailureCard extends StatelessWidget {
  const RunFailureCard({super.key, required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    return TerminalOutcomeCard(outcomeLabel: 'Run failed', message: message);
  }
}
