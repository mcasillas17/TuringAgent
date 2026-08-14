import 'package:flutter/material.dart';

import 'terminal_outcome_card.dart';

/// Presentational card for a `sendMessage` RPC rejection CONCLUSIVELY proven
/// to have happened before the backend's `SendMessage` handler
/// (orchestrator-go internal/service/chat/service.go) ever reached
/// `EnqueueUserMessage` — i.e. before the message and its run could exist
/// server-side at all.
///
/// Unlike [MessageSendUnconfirmedCard], which covers every OTHER rejection
/// (the true outcome unknown because the backend already persists the
/// message before acknowledging it), this card is reserved for a narrow,
/// contract-backed allowlist of statuses that can only occur pre-enqueue: an
/// unauthenticated request (rejected by the stream's auth interceptor before
/// the handler body ever runs), an invalid argument (the handler's own
/// upfront validation), or a not-found session (the handler's session
/// lookup, which happens before enqueueing). None of these has any other
/// call site in `SendMessage` that could fire after enqueueing, so a
/// rejection carrying one of them proves the message was never accepted —
/// see the classifier that selects this card over
/// [MessageSendUnconfirmedCard] for the exact allowlist and why each entry
/// qualifies.
///
/// Every other rejection — any other `GrpcError` status (including ones
/// this client does not recognize), and every `TuringApiException` (whose
/// `code` is an app-defined string with no contractual tie to a gRPC
/// status) — stays on [MessageSendUnconfirmedCard]. Deliberately its own
/// card with truthful "Message not sent" wording, not a relabeled
/// [MessageSendUnconfirmedCard]: shares [TerminalOutcomeCard]'s visual
/// chrome with [MessageSendUnconfirmedCard], [RunFailureCard], and
/// [RunCancelledCard] — every non-routine outcome gets the same
/// error-styled treatment — but is never interchangeable with any of them:
/// this is the only one of the four that reports a certainty proven before
/// any server-side effect, not merely a known terminal outcome after one.
class MessageSendFailureCard extends StatelessWidget {
  const MessageSendFailureCard({super.key, required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    return TerminalOutcomeCard(
      outcomeLabel: 'Message not sent',
      message: message,
    );
  }
}
