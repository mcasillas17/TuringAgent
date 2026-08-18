import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:turing_flutter_app/app.dart';
import 'package:turing_flutter_app/models/agent_descriptor.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/models/search_hit.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';
import 'package:turing_flutter_app/models/turing_event.dart';
import 'package:turing_flutter_app/networking/auth_storage.dart';
import 'package:turing_flutter_app/networking/grpc_client.dart';
import 'package:turing_flutter_app/networking/event_source.dart';

import 'support/no_skills_api.dart';

void main() {
  testWidgets('Turing app renders settings when credentials are missing', (
    tester,
  ) async {
    await tester.pumpWidget(TuringApp(authStorage: _FakeAuthStorage()));
    await tester.pumpAndSettle();

    expect(find.text('Project Turing Settings'), findsOneWidget);
  });

  testWidgets('Turing app closes the configured API when disposed', (
    tester,
  ) async {
    final apiClient = _ClosableFakeApiClient();

    await tester.pumpWidget(
      TuringApp(
        authStorage: _FakeAuthStorage(
          backendUrl: 'http://localhost:3000',
          apiKey: 'client-key',
        ),
        apiFactory: ({required baseUrl, required apiKey}) {
          expect(baseUrl, 'http://localhost:3000');
          expect(apiKey, 'client-key');
          return apiClient;
        },
        eventSourceFactory: ({required baseUrl, required apiKey}) =>
            _FakeEventSource(),
      ),
    );
    await tester.pumpAndSettle();

    expect(apiClient.closed, isFalse);

    await tester.pumpWidget(const SizedBox.shrink());
    await tester.pump();

    expect(apiClient.closed, isTrue);
  });
}

class _FakeAuthStorage implements ClientAuthStorage {
  const _FakeAuthStorage({this.backendUrl, this.apiKey});

  final String? backendUrl;
  final String? apiKey;

  @override
  Future<String?> readModelProvider() async => 'ollama';

  @override
  Future<void> saveModelProvider(String provider) async {}

  @override
  Future<String?> readApiKey() async => apiKey;

  @override
  Future<String?> readBackendUrl() async => backendUrl;

  @override
  Future<void> save({
    required String backendUrl,
    required String apiKey,
  }) async {}
}

class _ClosableFakeApiClient with NoSkillsApi implements ClosableTuringApi {
  bool closed = false;

  @override
  Future<void> close() async {
    closed = true;
  }

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

  @override
  Future<List<ToolDescriptor>> listTools() async => const [];

  @override
  Future<List<AgentDescriptor>> listAgents() async => const [];

  @override
  Future<void> deleteSession({required String sessionId}) async {}

  @override
  Future<Session> getSession({required String sessionId}) async {
    return Session(
      sessionId: sessionId,
      title: 'Session',
      updatedAt: DateTime.utc(2026, 5, 10),
    );
  }

  @override
  Future<TuringEventPage> listEvents({
    required String sessionId,
    int? after,
    int limit = 500,
  }) async {
    return const TuringEventPage(events: [], latestSequence: 0);
  }

  @override
  Future<List<Message>> listMessages({
    required String sessionId,
    int limit = 50,
    String? before,
  }) async {
    return const [];
  }

  @override
  Future<List<SearchHit>> searchMessages({
    required String query,
    int limit = 50,
  }) async {
    return const [];
  }

  @override
  Future<List<Session>> listSessions({int limit = 50, String? after}) async {
    return const [];
  }

  @override
  Future<Map<String, dynamic>> sendMessage({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
  }) async {
    return {
      'sessionId': sessionId,
      'runId': 'run_1',
      'jobId': 'job_1',
      'traceId': 'trace_1',
      'status': 'queued',
    };
  }
}

class _FakeEventSource implements TuringEventSource {
  @override
  Stream<TuringEvent> connect({required String sessionId, int? lastSequence}) {
    return const Stream.empty();
  }

  @override
  void close() {}
}
