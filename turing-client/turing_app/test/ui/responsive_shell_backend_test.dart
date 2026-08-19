import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/models/agent_descriptor.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/models/search_hit.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';
import 'package:turing_flutter_app/models/turing_event.dart';
import 'package:turing_flutter_app/networking/api_client.dart';
import 'package:turing_flutter_app/networking/auth_storage.dart';
import 'package:turing_flutter_app/networking/event_source.dart';
import 'package:turing_flutter_app/ui/shell/responsive_shell.dart';

import '../support/no_external_agents_api.dart';
import '../support/no_integrations_api.dart';
import '../support/no_automations_api.dart';
import '../support/no_skills_api.dart';

void main() {
  testWidgets('the shell is one surface: conversations beside a chat', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1200, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(
        home: ResponsiveShell(
          apiClient: _FakeApiClient(),
          eventSourceFactory: () => _FakeEventSource(),
          authStorage: _FakeAuthStorage(),
          initialBackendUrl: 'http://localhost:3000',
          initialApiKey: 'tk_test',
        ),
      ),
    );
    await tester.pumpAndSettle();

    // The old rail carried Devices/Stats/IoT: destinations for features
    // docs/VISION.md refuses outright, rendering mock dashboards that implied
    // the feature existed. Those stay gone.
    expect(find.text('Devices'), findsNothing);
    expect(find.text('Stats'), findsNothing);
    expect(find.text('IoT Devices Dashboard'), findsNothing);

    // Destinations that ARE on the roadmap do appear — the rule is not "only
    // implemented destinations" but "no destination that pretends", and each
    // unimplemented one says so on arrival (asserted below).
    for (final label in ['Skills', 'Integrations', 'MCPs', 'Automations']) {
      expect(find.text(label), findsOneWidget, reason: '$label is navigable');
    }

    // What is there instead: start a conversation, find an old one, settings.
    // Two while the pane is empty: the sidebar button and the empty state's.
    expect(find.text('New chat'), findsWidgets);
    expect(find.byTooltip('Search conversations'), findsOneWidget);
    expect(find.byTooltip('Settings'), findsOneWidget);
    expect(find.text('Existing chat'), findsOneWidget);

    // With nothing selected the pane invites a first message rather than
    // showing an empty frame.
    expect(find.text('Ask Turing anything'), findsOneWidget);

    // Selecting a conversation swaps it IN PLACE — nothing is pushed, so the
    // sidebar stays put and there is nothing to back out of.
    await tester.tap(find.text('Existing chat'));
    await tester.pumpAndSettle();
    expect(find.text('Ask Turing anything'), findsNothing);
    expect(find.text('New chat'), findsOneWidget, reason: 'sidebar persists');
  });

  testWidgets('deleting a conversation confirms, then removes it', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1200, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final api = _FakeApiClient();
    await tester.pumpWidget(
      MaterialApp(
        home: ResponsiveShell(
          apiClient: api,
          eventSourceFactory: () => _FakeEventSource(),
          authStorage: _FakeAuthStorage(),
        ),
      ),
    );
    await tester.pumpAndSettle();

    // The delete affordance is revealed by selecting the row rather than shown
    // on every row, so a destructive action is never one stray click away.
    await tester.tap(find.text('Existing chat'));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('Delete chat'));
    await tester.pumpAndSettle();

    // The dialog must say it is permanent, and must not overclaim: sandbox
    // files outlive the conversation.
    expect(find.textContaining('cannot be undone'), findsOneWidget);
    expect(find.textContaining('sandbox are not removed'), findsOneWidget);

    await tester.tap(find.widgetWithText(TextButton, 'Delete'));
    await tester.pumpAndSettle();
    expect(api.deletedSessionIds, ['sess_existing']);
  });

  testWidgets('cancelling a delete removes nothing', (tester) async {
    tester.view.physicalSize = const Size(1200, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final api = _FakeApiClient();
    await tester.pumpWidget(
      MaterialApp(
        home: ResponsiveShell(
          apiClient: api,
          eventSourceFactory: () => _FakeEventSource(),
          authStorage: _FakeAuthStorage(),
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.text('Existing chat'));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('Delete chat'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(TextButton, 'Cancel'));
    await tester.pumpAndSettle();

    expect(api.deletedSessionIds, isEmpty);
    expect(find.text('Existing chat'), findsOneWidget);
  });

  testWidgets('settings opens from the sidebar', (tester) async {
    tester.view.physicalSize = const Size(1200, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      MaterialApp(
        home: ResponsiveShell(
          apiClient: _FakeApiClient(),
          eventSourceFactory: () => _FakeEventSource(),
          authStorage: _FakeAuthStorage(),
          initialBackendUrl: 'http://localhost:3000',
          initialApiKey: 'tk_test',
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byTooltip('Settings'));
    await tester.pumpAndSettle();
    expect(find.text('Backend URL'), findsOneWidget);
    expect(find.text('API key'), findsOneWidget);
  });
}

class _FakeApiClient
    with NoSkillsApi, NoExternalAgentsApi, NoIntegrationsApi, NoAutomationsApi
    implements TuringApi {
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
    deletedSessionIds.add(sessionId);
  }

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

  /// Mutable so a test can change what the backend reports between calls —
  /// which is exactly what happens when the first message renames a session.
  List<Session> sessions = [
    Session(
      sessionId: 'sess_existing',
      title: 'Existing chat',
      updatedAt: DateTime.utc(2026, 5, 10),
    ),
  ];
  @override
  Future<List<Session>> listSessions({int limit = 50, String? after}) async =>
      sessions;

  List<ToolDescriptor> tools = const [];
  List<AgentDescriptor> agents = const [];

  @override
  Future<List<ToolDescriptor>> listTools() async => tools;

  @override
  Future<List<AgentDescriptor>> listAgents() async => agents;

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

class _FakeAuthStorage implements ClientAuthStorage {
  @override
  Future<String?> readModelProvider() async => 'ollama';

  @override
  Future<void> saveModelProvider(String provider) async {}

  @override
  Future<String?> readApiKey() async => 'tk_test';

  @override
  Future<String?> readBackendUrl() async => 'http://localhost:3000';

  @override
  Future<void> save({
    required String backendUrl,
    required String apiKey,
  }) async {}
}

class _FakeEventSource implements TuringEventSource {
  final _events = StreamController<TuringEvent>();

  @override
  Stream<TuringEvent> connect({required String sessionId, int? lastSequence}) {
    return _events.stream;
  }

  @override
  void close() {
    unawaited(_events.close());
  }
}
