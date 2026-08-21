import 'session.dart';

enum SessionListFilter { active, archived, all }

class SessionPage {
  const SessionPage({required this.sessions, this.nextCursor});

  final List<Session> sessions;
  final String? nextCursor;
}
