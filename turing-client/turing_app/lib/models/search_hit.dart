import 'message.dart';

class SearchHit {
  const SearchHit({required this.sessionId, required this.message});

  final String sessionId;
  final Message message;
}
