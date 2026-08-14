import 'package:flutter/material.dart';

import 'terminal_run_card.dart';

/// Presentational card for a terminal `agent.run.cancelled` event.
///
/// The backend cancels a run server-side (`cancelRun`, orchestrator-go
/// internal/service/chat/service.go) on exactly two conditions:
/// `SendMessage`'s own context is cancelled (checked at four checkpoints —
/// initial send, dispatch loop teardown, replay error, relay send) or
/// `DispatchPending` fails unconditionally. A bare `stream.Send` failure does
/// not cancel a run unless the context is already cancelled — even though
/// this screen exposes no cancel affordance of its own. Deliberately
/// distinct from [RunFailureCard]: a cancellation is not a failure and must
/// never be announced as one. Shares its visual chrome with [RunFailureCard]
/// via [TerminalRunCard] — both are terminal, non-routine outcomes and get
/// the same error-styled treatment — but keeps its own truthful "Run
/// cancelled" wording, in the rendered text and in the accessibility label
/// alike.
class RunCancelledCard extends StatelessWidget {
  const RunCancelledCard({super.key, required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    return TerminalRunCard(outcomeLabel: 'Run cancelled', message: message);
  }
}
