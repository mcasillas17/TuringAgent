import 'message.dart';

class SearchHit {
  const SearchHit({
    required this.sessionId,
    required this.message,
    this.score,
    this.snippet,
  });

  final String sessionId;
  final Message message;

  /// Server relevance score, null only for mixed-version legacy responses that
  /// carry plain messages without hit metadata.
  final double? score;

  /// Server-built bounded plain-text excerpt of the message, centered on the
  /// match when one fits the server's snippet window and otherwise an
  /// unhighlighted excerpt of the same message. Null only for legacy
  /// responses.
  final String? snippet;
}
