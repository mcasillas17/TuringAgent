import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/session_page.dart';

mixin NoSessionLifecycleApi {
  Future<List<Session>> listSessions({int limit = 50, String? after});

  Future<SessionPage> listSessionPage({
    int limit = 50,
    String? cursor,
    SessionListFilter filter = SessionListFilter.active,
  }) async {
    return SessionPage(
      sessions: await listSessions(limit: limit, after: cursor),
    );
  }

  Future<Session> renameSession({
    required String sessionId,
    required String title,
  }) {
    throw UnsupportedError('session rename is not used by this test');
  }

  Future<Session> archiveSession({required String sessionId}) {
    throw UnsupportedError('session archive is not used by this test');
  }

  Future<Session> restoreSession({required String sessionId}) {
    throw UnsupportedError('session restore is not used by this test');
  }
}
