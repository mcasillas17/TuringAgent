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

  /// Server-built match excerpt, null only for legacy responses.
  final String? snippet;
}
