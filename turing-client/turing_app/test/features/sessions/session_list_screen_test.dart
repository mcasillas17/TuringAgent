import 'dart:async';

import 'package:flutter/material.dart';
import 'package:grpc/grpc.dart' as grpc;
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/search/search_screen.dart';
import 'package:turing_flutter_app/features/sessions/session_list_screen.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/models/search_hit.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/turing_event.dart';
import 'package:turing_flutter_app/networking/api_client.dart';
import 'package:turing_flutter_app/networking/event_source.dart';

void main() {
  group('SessionListScreen search', () {
    testWidgets('standalone search action opens SearchScreen', (tester) async {
      final api = _FakeTuringApi(
        sessions: [_session(id: 'session-list-1', title: 'Existing chat')],
      );
      final eventSourceFactory = _TrackingEventSourceFactory();

      await tester.pumpWidget(
        MaterialApp(
          home: SessionListScreen(
            apiClient: api,
            eventSourceFactory: eventSourceFactory.create,
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byTooltip('Search conversations'), findsOneWidget);

      await tester.tap(find.byTooltip('Search conversations'));
      await tester.pumpAndSettle();

      expect(find.byType(SearchScreen), findsOneWidget);
      expect(find.text('Search conversations'), findsWidgets);
    });

    testWidgets('embedded search action opens SearchScreen', (tester) async {
      final api = _FakeTuringApi(
        sessions: [_session(id: 'session-list-1', title: 'Existing chat')],
      );
      final eventSourceFactory = _TrackingEventSourceFactory();

      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SessionListScreen(
              apiClient: api,
              eventSourceFactory: eventSourceFactory.create,
              embedded: true,
            ),
          ),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('Sessions'), findsOneWidget);
      expect(find.byTooltip('Search conversations'), findsOneWidget);

      await tester.tap(find.byTooltip('Search conversations'));
      await tester.pumpAndSettle();

      expect(find.byType(SearchScreen), findsOneWidget);
    });

    testWidgets('search result opens chat through existing session path', (
      tester,
    ) async {
      final api = _FakeTuringApi(
        sessions: [_session(id: 'session-list-1', title: 'Existing chat')],
      );
      final eventSourceFactory = _TrackingEventSourceFactory();

      await tester.pumpWidget(
        MaterialApp(
          home: SessionListScreen(
            apiClient: api,
            eventSourceFactory: eventSourceFactory.create,
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byTooltip('Search conversations'));
      await tester.pumpAndSettle();

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.pump(const Duration(milliseconds: 349));
      expect(api.searchQueries, isEmpty);

      await tester.pump(const Duration(milliseconds: 1));
      expect(api.searchQueries, ['deploy']);

      api.searchCalls.single.completer.complete([
        _hit(
          id: 'hit-1',
          sessionId: 'search-session-42',
          content: 'deploy staging',
          createdAt: DateTime.utc(2026, 8, 13, 18),
        ),
      ]);
      await tester.pump();

      expect(api.sessionRequests, ['search-session-42']);
      api.sessionCalls.single.completer.complete(
        _session(id: 'search-session-42', title: 'Deployment notes'),
      );
      await tester.pump();

      expect(eventSourceFactory.createdSources, isEmpty);

      await tester.tap(find.text('deploy staging'));
      await tester.pumpAndSettle();

      expect(eventSourceFactory.createdSources, hasLength(1));
      expect(eventSourceFactory.createdSources.single.connectedSessionIds, [
        'search-session-42',
      ]);
      expect(api.listEventsSessionIds, ['search-session-42']);
      expect(api.listMessagesSessionIds, ['search-session-42']);
    });
  });

  group('SessionListScreen delete', () {
    testWidgets('confirming deletes the session and refreshes the list', (
      tester,
    ) async {
      final api = _FakeTuringApi(
        sessions: [_session(id: 'session-doomed', title: 'Doomed chat')],
      );
      await tester.pumpWidget(
        MaterialApp(
          home: SessionListScreen(
            apiClient: api,
            eventSourceFactory: _TrackingEventSourceFactory().create,
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byTooltip('Delete chat'));
      await tester.pumpAndSettle();

      // The dialog must say the deletion is permanent, not just "are you sure".
      expect(find.textContaining('cannot be undone'), findsOneWidget);
      expect(find.textContaining('Doomed chat'), findsWidgets);

      await tester.tap(find.widgetWithText(TextButton, 'Delete'));
      await tester.pumpAndSettle();

      expect(api.deletedSessionIds, ['session-doomed']);
    });

    testWidgets('cancelling deletes nothing', (tester) async {
      final api = _FakeTuringApi(
        sessions: [_session(id: 'session-safe', title: 'Safe chat')],
      );
      await tester.pumpWidget(
        MaterialApp(
          home: SessionListScreen(
            apiClient: api,
            eventSourceFactory: _TrackingEventSourceFactory().create,
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byTooltip('Delete chat'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(TextButton, 'Cancel'));
      await tester.pumpAndSettle();

      expect(api.deletedSessionIds, isEmpty);
      expect(find.text('Safe chat'), findsOneWidget);
    });

    testWidgets('a run in progress is explained, not reported as a bug', (
      tester,
    ) async {
      final api = _FakeTuringApi(
        sessions: [_session(id: 'session-busy', title: 'Busy chat')],
        deleteError: grpc.GrpcError.failedPrecondition(
          'session has a run in progress',
        ),
      );
      await tester.pumpWidget(
        MaterialApp(
          home: SessionListScreen(
            apiClient: api,
            eventSourceFactory: _TrackingEventSourceFactory().create,
          ),
        ),
      );
      await tester.pumpAndSettle();

      await tester.tap(find.byTooltip('Delete chat'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(TextButton, 'Delete'));
      await tester.pumpAndSettle();

      // Assert on wording ONLY the friendly branch produces: the raw GrpcError
      // message also contains "run in progress", so matching that would pass
      // even with the classifier dead.
      expect(find.textContaining('Wait for it to finish'), findsOneWidget);
      expect(find.textContaining('Could not delete'), findsNothing);
      // The session survives a refused delete.
      expect(find.text('Busy chat'), findsOneWidget);
    });
  });
}

Session _session({required String id, required String title}) {
  return Session(
    sessionId: id,
    title: title,
    updatedAt: DateTime.utc(2026, 8, 13, 12),
  );
}

SearchHit _hit({
  required String id,
  required String sessionId,
  required String content,
  required DateTime createdAt,
}) {
  return SearchHit(
    sessionId: sessionId,
    message: Message(
      messageId: id,
      role: 'user',
      content: content,
      sequence: 1,
      createdAt: createdAt,
    ),
  );
}

class _SearchCall {
  _SearchCall(this.query) : completer = Completer<List<SearchHit>>();

  final String query;
  final Completer<List<SearchHit>> completer;
}

class _SessionCall {
  _SessionCall(this.sessionId) : completer = Completer<Session>();

  final String sessionId;
  final Completer<Session> completer;
}

class _FakeTuringApi implements TuringApi {
  _FakeTuringApi({required this.sessions, this.deleteError});

  final List<Session> sessions;
  final Object? deleteError;
  final List<_SearchCall> searchCalls = [];
  final List<_SessionCall> sessionCalls = [];
  final List<String> sessionRequests = [];
  final List<String> listEventsSessionIds = [];
  final List<String> listMessagesSessionIds = [];

  List<String> get searchQueries =>
      searchCalls.map((call) => call.query).toList();

  @override
  Future<Map<String, dynamic>> approveApproval(
    String approvalId, {
    String? comment,
  }) async {
    return {'approvalId': approvalId, 'status': 'approved'};
  }

  @override
  Future<Map<String, dynamic>> createSession({String? title}) async {
    return {'sessionId': 'sess_1', 'createdAt': '2026-05-10T00:00:00.000Z'};
  }

  @override
  Future<Map<String, dynamic>> denyApproval(
    String approvalId, {
    String? reason,
  }) async {
    return {'approvalId': approvalId, 'status': 'denied'};
  }

  @override
  Future<Map<String, dynamic>> getConfig() async {
    return {
      'enabledProviders': ['ollama'],
    };
  }

  final List<String> deletedSessionIds = [];

  @override
  Future<void> deleteSession({required String sessionId}) async {
    if (deleteError != null) throw deleteError!;
    deletedSessionIds.add(sessionId);
    sessions.removeWhere((session) => session.sessionId == sessionId);
  }

  @override
  Future<Session> getSession({required String sessionId}) {
    sessionRequests.add(sessionId);
    final call = _SessionCall(sessionId);
    sessionCalls.add(call);
    return call.completer.future;
  }

  @override
  Future<TuringEventPage> listEvents({
    required String sessionId,
    int? after,
    int limit = 500,
  }) async {
    listEventsSessionIds.add(sessionId);
    return const TuringEventPage(events: [], latestSequence: 0);
  }

  @override
  Future<List<Message>> listMessages({
    required String sessionId,
    int limit = 50,
    String? before,
  }) async {
    listMessagesSessionIds.add(sessionId);
    return const [];
  }

  @override
  Future<List<SearchHit>> searchMessages({
    required String query,
    int limit = 50,
  }) {
    final call = _SearchCall(query);
    searchCalls.add(call);
    return call.completer.future;
  }

  @override
  Future<List<Session>> listSessions({int limit = 50, String? after}) async {
    return sessions;
  }

  @override
  Future<Map<String, dynamic>> sendMessage({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
  }) async {
    return {
      'sessionId': sessionId,
      'userMessageId': 'msg_user',
      'assistantMessageId': 'msg_asst',
      'runId': 'run_1',
      'jobId': 'job_1',
      'traceId': 'trace_1',
      'status': 'queued',
    };
  }
}

class _TrackingEventSourceFactory {
  final List<_TrackingEventSource> createdSources = [];

  TuringEventSource create() {
    final source = _TrackingEventSource();
    createdSources.add(source);
    return source;
  }
}

class _TrackingEventSource implements TuringEventSource {
  final List<String> connectedSessionIds = [];
  final _events = StreamController<TuringEvent>.broadcast();

  @override
  Stream<TuringEvent> connect({required String sessionId, int? lastSequence}) {
    connectedSessionIds.add(sessionId);
    return _events.stream;
  }

  @override
  void close() {
    unawaited(_events.close());
  }
}
