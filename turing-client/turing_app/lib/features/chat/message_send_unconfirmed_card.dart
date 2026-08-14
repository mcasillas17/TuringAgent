import 'package:flutter/material.dart';

import 'terminal_outcome_card.dart';

/// Presentational card for a `sendMessage` RPC that rejected before any
/// `RunQueued` event ever reached this client.
///
/// `TuringApi.sendMessage` (`networking/api_client.dart`) resolves once a
/// run is queued and rejects otherwise. The real `TuringGrpcApi.sendMessage`
/// (`networking/grpc_client.dart`) listens to the `ChatService.SendMessage`
/// stream and rejects with whatever it reports before a `RunQueued` event
/// ever arrives — a stream `onError` (the underlying gRPC call itself
/// failing), or the stream reaching `onDone` having never queued a run
/// (mapped there to `TuringApiException(code: 'empty_stream', ...)`).
///
/// Neither case proves the message was never sent. The backend's
/// `SendMessage` handler (orchestrator-go
/// internal/service/chat/service.go) persists the enqueued message and its
/// run FIRST, and only afterwards attempts `stream.Send` of the `RunQueued`
/// acknowledgement back over this same stream — a network drop or reconnect
/// between those two steps rejects this RPC with no `RunQueued` ever
/// observed, even though the run may already exist server-side. So the
/// honest outcome here is UNKNOWN, not failure: labelling this
/// [RunFailureCard]'s "Run failed" (or worse, a prior version of this very
/// card, "Message not sent") would assert a certainty this client does not
/// have.
///
/// Deliberately its own card with truthful "Message send unconfirmed"
/// wording, not a relabeled [RunFailureCard]: shares [TerminalOutcomeCard]'s
/// visual chrome with [RunFailureCard] and [RunCancelledCard] — every
/// non-routine outcome gets the same error-styled treatment — but is never
/// interchangeable with either: both of those report a definite, known
/// terminal state; this one deliberately does not.
class MessageSendUnconfirmedCard extends StatelessWidget {
  const MessageSendUnconfirmedCard({super.key, required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    return TerminalOutcomeCard(
      outcomeLabel: 'Message send unconfirmed',
      message: message,
    );
  }
}
