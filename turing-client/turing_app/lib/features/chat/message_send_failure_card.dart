import 'package:flutter/material.dart';

import 'terminal_run_card.dart';

/// Presentational card for a `sendMessage` RPC rejected before any run was
/// ever queued.
///
/// `TuringApi.sendMessage` (`networking/api_client.dart`) resolves once a
/// run is queued and rejects otherwise. The real `TuringGrpcApi.sendMessage`
/// (`networking/grpc_client.dart`) listens to the `ChatService.SendMessage`
/// stream and rejects with whatever it reports before a `RunQueued` event
/// ever arrives — a stream `onError` (the underlying gRPC call itself
/// failing), or the stream reaching `onDone` having never queued a run
/// (mapped there to `TuringApiException(code: 'empty_stream', ...)`).
/// Either way NO run was ever created for that attempt, so labelling this
/// [RunFailureCard]'s "Run failed" would falsely claim one existed and then
/// failed — the truth is the send itself never reached, or was rejected by,
/// the backend before any run could be dispatched.
///
/// Deliberately its own card with truthful "Message not sent" wording, not
/// a relabeled [RunFailureCard]: shares [TerminalRunCard]'s visual chrome
/// with [RunFailureCard] and [RunCancelledCard] — every terminal, non-
/// routine outcome gets the same error-styled treatment — but is never
/// interchangeable with either.
class MessageSendFailureCard extends StatelessWidget {
  const MessageSendFailureCard({super.key, required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    return TerminalRunCard(outcomeLabel: 'Message not sent', message: message);
  }
}
