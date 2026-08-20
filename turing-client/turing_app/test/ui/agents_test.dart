import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/workspace/agents_page.dart';
import 'package:turing_flutter_app/features/workspace/session_agent_bar.dart';
import 'package:turing_flutter_app/models/agent_descriptor.dart';
import 'package:turing_flutter_app/models/external_agent.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/models/search_hit.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';
import 'package:turing_flutter_app/models/turing_event.dart';
import 'package:turing_flutter_app/networking/api_client.dart';

import '../support/no_audit_api.dart';
import '../support/no_skills_api.dart';
import '../support/no_integrations_api.dart';
import '../support/no_automations_api.dart';
import '../support/no_telemetry_api.dart';

void main() {
  group('the agents page', () {
    testWidgets('says nothing leaves the machine when none are configured', (
      tester,
    ) async {
      await _pumpAgents(tester, _AgentApi());

      expect(find.text('No conversation leaves this machine'), findsOneWidget);
      // The local assistant is always there and is not one of the removable
      // rows, so the page must show it even with nothing else configured.
      expect(find.text('General Assistant'), findsOneWidget);
      expect(find.textContaining('cannot be removed'), findsOneWidget);
    });

    testWidgets('a configured agent shows who receives the conversation', (
      tester,
    ) async {
      final api = _AgentApi()..agents.add(_claude());
      await _pumpAgents(tester, api);

      expect(find.text('Claude'), findsOneWidget);
      expect(
        find.textContaining('api.anthropic.com'),
        findsOneWidget,
        reason: 'the endpoint names the company that receives the transcript',
      );
      // The standing warning has to be there once an agent exists, or the page
      // reads as a neutral list of equivalent options.
      expect(find.text('These receive whatever you send them'), findsOneWidget);
    });

    // An agent whose key the backend cannot find is configuration that looks
    // complete and is not. Saying so here beats discovering it on a send.
    testWidgets('an agent with no key says so before it is ever used', (
      tester,
    ) async {
      final api = _AgentApi()..agents.add(_claude(credentialAvailable: false));
      await _pumpAgents(tester, api);

      expect(find.textContaining('No API key named "claude"'), findsOneWidget);
      // Naming the fix matters: the badge is computed at backend startup, so
      // adding the key without a restart leaves it saying this forever.
      expect(find.textContaining('restart the backend'), findsOneWidget);
      expect(find.text('API key "claude" found'), findsNothing);
    });

    testWidgets('an agent with a key present says that instead', (
      tester,
    ) async {
      final api = _AgentApi()..agents.add(_claude());
      await _pumpAgents(tester, api);

      expect(find.text('API key "claude" found'), findsOneWidget);
    });

    testWidgets('a backend failure offers a retry instead of an empty page', (
      tester,
    ) async {
      final api = _AgentApi()..listError = _Offline();
      await _pumpAgents(tester, api);

      expect(find.text('Could not reach the backend'), findsOneWidget);
      // Never "No conversation leaves this machine": that is a confident claim
      // about state the page has just failed to read.
      expect(find.text('No conversation leaves this machine'), findsNothing);

      api.listError = null;
      api.agents.add(_claude());
      await tester.tap(find.text('Try again'));
      await tester.pumpAndSettle();

      expect(find.text('Claude'), findsOneWidget);
    });

    testWidgets('adding an agent sends every field and never a key', (
      tester,
    ) async {
      final api = _AgentApi();
      await _pumpAgents(tester, api);

      await _openEditor(tester);
      await tester.enterText(find.byType(TextField).at(0), 'Claude');
      await tester.enterText(find.byType(TextField).at(2), 'claude-sonnet-4-5');
      await tester.enterText(find.byType(TextField).at(3), 'claude');
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();

      expect(api.created.length, 1);
      final created = api.created.single;
      expect(created.displayName, 'Claude');
      expect(created.model, 'claude-sonnet-4-5');
      expect(created.credentialRef, 'claude');
      // Prefilled from the provider, so the user does not have to know it.
      expect(created.baseUrl, 'https://api.anthropic.com/v1');
      expect(find.text('Claude'), findsOneWidget);
    });

    testWidgets('the editor says the key does not pass through the app', (
      tester,
    ) async {
      await _pumpAgents(tester, _AgentApi());

      await _openEditor(tester);

      expect(find.textContaining('A name, not the key'), findsOneWidget);
      expect(
        find.textContaining('never stored in the database'),
        findsOneWidget,
      );
    });

    testWidgets('an incomplete agent is refused before the round trip', (
      tester,
    ) async {
      final api = _AgentApi();
      await _pumpAgents(tester, api);

      await _openEditor(tester);
      await tester.enterText(find.byType(TextField).at(0), 'Claude');
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();

      expect(find.text('Every field is required.'), findsOneWidget);
      expect(api.created, isEmpty, reason: 'nothing was sent to the backend');
    });

    testWidgets('a rejected save is shown next to the fields', (tester) async {
      final api = _AgentApi()..createError = _Offline();
      await _pumpAgents(tester, api);

      await _openEditor(tester);
      await tester.enterText(find.byType(TextField).at(0), 'Claude');
      await tester.enterText(find.byType(TextField).at(2), 'claude-sonnet-4-5');
      await tester.enterText(find.byType(TextField).at(3), 'claude');
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();

      expect(find.textContaining('offline'), findsOneWidget);
      // The dialog stays open, so the typed fields are not lost.
      expect(find.text('Add an agent'), findsOneWidget);
    });

    testWidgets('removing an agent explains what happens to its chats', (
      tester,
    ) async {
      final api = _AgentApi()..agents.add(_claude());
      await _pumpAgents(tester, api);

      await tester.tap(find.byTooltip('Remove agent'));
      await tester.pumpAndSettle();

      expect(
        find.textContaining('goes back to Turing on this machine'),
        findsOneWidget,
      );

      await tester.tap(find.text('Remove'));
      await tester.pumpAndSettle();

      expect(api.deleted, ['agent_1']);
      expect(find.text('No conversation leaves this machine'), findsOneWidget);
    });

    testWidgets('cancelling the removal removes nothing', (tester) async {
      final api = _AgentApi()..agents.add(_claude());
      await _pumpAgents(tester, api);

      await tester.tap(find.byTooltip('Remove agent'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();

      expect(api.deleted, isEmpty);
      expect(find.text('Claude'), findsOneWidget);
    });

    // A row stored by a newer backend must still be editable. A dropdown whose
    // current value is absent from its items throws, so this would crash the
    // whole page rather than degrade.
    testWidgets('an agent with a provider this build does not know opens', (
      tester,
    ) async {
      final api = _AgentApi()
        ..agents.add(
          const ExternalAgent(
            agentId: 'agent_1',
            displayName: 'Something new',
            provider: ExternalAgentProvider.unknown,
            baseUrl: 'https://example.com/v1',
            model: 'model-x',
            credentialRef: 'x',
            credentialAvailable: true,
          ),
        );
      await _pumpAgents(tester, api);

      await tester.tap(find.byTooltip('Edit agent'));
      await tester.pumpAndSettle();

      expect(tester.takeException(), isNull);
      // And saving it unchanged is refused rather than silently relabelling it
      // as a vendor the user did not pick.
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();

      expect(find.textContaining('Pick one before saving'), findsOneWidget);
      expect(api.updated, isEmpty);
    });

    testWidgets('editing an agent starts from what it already is', (
      tester,
    ) async {
      final api = _AgentApi()..agents.add(_claude());
      await _pumpAgents(tester, api);

      await tester.tap(find.byTooltip('Edit agent'));
      await tester.pumpAndSettle();
      await tester.enterText(find.byType(TextField).at(2), 'claude-opus-4-5');
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();

      expect(api.updated.length, 1);
      expect(api.updated.single.agentId, 'agent_1');
      expect(api.updated.single.model, 'claude-opus-4-5');
      // Untouched fields survive the edit rather than being blanked.
      expect(api.updated.single.credentialRef, 'claude');
    });
  });

  group('the bar above the conversation', () {
    testWidgets('a local conversation says it stays here', (tester) async {
      await _pumpBar(tester, _AgentApi());

      expect(
        find.text('Turing — this conversation stays on your machine'),
        findsOneWidget,
      );
    });

    // The point of use. A settings screen the user visited once is not where
    // this belongs.
    testWidgets('a routed conversation says the messages leave', (
      tester,
    ) async {
      final api = _AgentApi()
        ..agents.add(_claude())
        ..routes['sess_1'] = 'agent_1';
      await _pumpBar(tester, api);

      expect(
        find.text('Goes to Claude — messages leave your machine'),
        findsOneWidget,
      );
    });

    // The composer is usable while this loads. A strip that is simply absent
    // during that window reads as "nothing to say here", which is the same
    // reassurance-by-omission the failure state refuses to give.
    testWidgets('while loading it says it is checking, not nothing', (
      tester,
    ) async {
      final api = _AgentApi()..holdSessionAgent = true;
      tester.view.physicalSize = const Size(1200, 900);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: SessionAgentBar(apiClient: api, sessionId: 'sess_1'),
          ),
        ),
      );
      await tester.pump();

      expect(
        find.text('Checking where this conversation goes'),
        findsOneWidget,
      );
      expect(find.textContaining('stays on your machine'), findsNothing);

      api.releaseSessionAgent();
      await tester.pumpAndSettle();
      expect(
        find.text('Turing — this conversation stays on your machine'),
        findsOneWidget,
      );
    });

    testWidgets('a failed read never claims the conversation is local', (
      tester,
    ) async {
      final api = _AgentApi()..sessionAgentError = _Offline();
      await _pumpBar(tester, api);

      expect(
        find.text('Could not tell where this conversation goes'),
        findsOneWidget,
      );
      expect(
        find.textContaining('stays on your machine'),
        findsNothing,
        reason: 'reassurance after a failed read is the one lie this forbids',
      );

      api.sessionAgentError = null;
      await tester.tap(find.text('Retry'));
      await tester.pumpAndSettle();

      expect(
        find.text('Turing — this conversation stays on your machine'),
        findsOneWidget,
      );
    });

    testWidgets('choosing an agent routes the conversation and shows it', (
      tester,
    ) async {
      final api = _AgentApi()..agents.add(_claude());
      await _pumpBar(tester, api);

      await tester.tap(find.text('Change'));
      await tester.pumpAndSettle();
      expect(
        find.textContaining('Material recalled from your other conversations'),
        findsOneWidget,
        reason: 'the sheet states what routing does and does not send',
      );
      expect(
        find.textContaining('skill text the agent loads'),
        findsOneWidget,
        reason: 'routing disclosure includes file-backed prompt material',
      );

      await tester.tap(find.text('Claude'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Done'));
      await tester.pumpAndSettle();

      expect(api.routes['sess_1'], 'agent_1');
      expect(
        find.text('Goes to Claude — messages leave your machine'),
        findsOneWidget,
      );
    });

    testWidgets('choosing Turing brings the conversation back', (tester) async {
      final api = _AgentApi()
        ..agents.add(_claude())
        ..routes['sess_1'] = 'agent_1';
      await _pumpBar(tester, api);

      await tester.tap(find.text('Change'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Turing, on this machine'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Done'));
      await tester.pumpAndSettle();

      expect(api.routes.containsKey('sess_1'), isFalse);
      expect(
        find.text('Turing — this conversation stays on your machine'),
        findsOneWidget,
      );
    });

    testWidgets('a keyless agent warns inside the picker too', (tester) async {
      final api = _AgentApi()..agents.add(_claude(credentialAvailable: false));
      await _pumpBar(tester, api);

      await tester.tap(find.text('Change'));
      await tester.pumpAndSettle();

      expect(
        find.textContaining('this will fail until one is configured'),
        findsOneWidget,
      );
    });

    testWidgets('a failed route is reported inside the sheet', (tester) async {
      final api = _AgentApi()
        ..agents.add(_claude())
        ..setError = _Offline();
      await _pumpBar(tester, api);

      await tester.tap(find.text('Change'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Claude'));
      await tester.pumpAndSettle();

      // Inside the sheet, not in a snackbar underneath it, or the user sees
      // the selection fail to move and is told nothing.
      expect(find.textContaining('Could not change that'), findsOneWidget);
    });

    testWidgets('a failed agent list is not reported as an empty list', (
      tester,
    ) async {
      final api = _AgentApi()..listError = _Offline();
      await _pumpBar(tester, api);

      await tester.tap(find.text('Change'));
      await tester.pumpAndSettle();

      expect(find.textContaining('Could not load your agents'), findsOneWidget);
      expect(
        find.textContaining('have not added any other agents'),
        findsNothing,
      );
      // Going back to local must stay possible even when the list failed.
      expect(find.text('Turing, on this machine'), findsOneWidget);
    });
  });

  group('the agent surfaces fit a phone', () {
    for (final size in const [Size(320, 640), Size(360, 640), Size(568, 320)]) {
      testWidgets(
        'the page does not overflow at ${size.width}x${size.height}',
        (tester) async {
          final api = _AgentApi()
            ..agents.add(_claude())
            ..agents.add(
              _claude(
                agentId: 'agent_2',
                displayName:
                    'A deliberately very long agent name that will not fit',
                credentialAvailable: false,
              ),
            );
          await _pumpAgents(tester, api, size: size);

          expect(tester.takeException(), isNull);
        },
      );

      testWidgets('the bar does not overflow at ${size.width}x${size.height}', (
        tester,
      ) async {
        final api = _AgentApi()
          ..agents.add(
            _claude(
              displayName:
                  'A deliberately very long agent name that will not fit',
            ),
          )
          ..routes['sess_1'] = 'agent_1';
        await _pumpBar(tester, api, size: size);

        expect(tester.takeException(), isNull);
      });

      // The sheet carries the longest strings in the feature — the two-line
      // "no API key named …" subtitle — so it belongs in this matrix too.
      testWidgets(
        'the picker does not overflow at ${size.width}x${size.height}',
        (tester) async {
          final api = _AgentApi()
            ..agents.add(
              _claude(
                displayName:
                    'A deliberately very long agent name that will not fit',
                credentialAvailable: false,
              ),
            );
          await _pumpBar(tester, api, size: size);

          await tester.tap(find.text('Change'));
          await tester.pumpAndSettle();

          expect(tester.takeException(), isNull);
        },
      );

      testWidgets(
        'the editor does not overflow at ${size.width}x${size.height}',
        (tester) async {
          await _pumpAgents(tester, _AgentApi(), size: size);

          await _openEditor(tester);

          expect(tester.takeException(), isNull);
        },
      );
    }
  });
}

ExternalAgent _claude({
  String agentId = 'agent_1',
  String displayName = 'Claude',
  bool credentialAvailable = true,
}) => ExternalAgent(
  agentId: agentId,
  displayName: displayName,
  provider: ExternalAgentProvider.anthropic,
  baseUrl: 'https://api.anthropic.com/v1',
  model: 'claude-sonnet-4-5',
  credentialRef: 'claude',
  credentialAvailable: credentialAvailable,
);

/// Scrolls the button into view first: below the breakpoint the page is a
/// scroll view, and tapping a widget that is off screen silently misses.
Future<void> _openEditor(WidgetTester tester) async {
  await tester.ensureVisible(find.text('New agent'));
  await tester.pumpAndSettle();
  await tester.tap(find.text('New agent'));
  await tester.pumpAndSettle();
}

Future<void> _pumpAgents(
  WidgetTester tester,
  _AgentApi api, {
  Size size = const Size(1200, 900),
}) async {
  await _pump(tester, size, Scaffold(body: AgentsPage(apiClient: api)));
}

Future<void> _pumpBar(
  WidgetTester tester,
  _AgentApi api, {
  Size size = const Size(1200, 900),
}) async {
  await _pump(
    tester,
    size,
    Scaffold(
      body: SessionAgentBar(apiClient: api, sessionId: 'sess_1'),
    ),
  );
}

Future<void> _pump(WidgetTester tester, Size size, Widget home) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(MaterialApp(home: home));
  await tester.pumpAndSettle();
}

class _Offline implements Exception {
  const _Offline();

  @override
  String toString() => 'offline';
}

/// A working in-memory backend, so the UI is exercised against something that
/// behaves like the real one rather than a stub that always says yes.
class _AgentApi extends TuringApi
    with
        NoAuditApi,
        NoSkillsApi,
        NoIntegrationsApi,
        NoAutomationsApi,
        NoTelemetryApi {
  final List<ExternalAgent> agents = [];
  final Map<String, String> routes = {};
  final List<ExternalAgent> created = [];
  final List<ExternalAgent> updated = [];
  final List<String> deleted = [];
  Object? listError;
  Object? createError;
  Object? setError;
  Object? sessionAgentError;
  bool holdSessionAgent = false;
  Completer<void>? _held;
  int nextId = 2;

  void releaseSessionAgent() {
    holdSessionAgent = false;
    _held?.complete();
    _held = null;
  }

  @override
  Future<List<ExternalAgent>> listExternalAgents() async {
    final error = listError;
    if (error != null) throw error;
    return List.unmodifiable(agents);
  }

  @override
  Future<ExternalAgent> createExternalAgent({
    required String displayName,
    required ExternalAgentProvider provider,
    required String baseUrl,
    required String model,
    required String credentialRef,
  }) async {
    final error = createError;
    if (error != null) throw error;
    final agent = ExternalAgent(
      agentId: 'agent_${nextId++}',
      displayName: displayName,
      provider: provider,
      baseUrl: baseUrl,
      model: model,
      credentialRef: credentialRef,
      credentialAvailable: true,
    );
    created.add(agent);
    agents.add(agent);
    return agent;
  }

  @override
  Future<ExternalAgent> updateExternalAgent({
    required String agentId,
    required String displayName,
    required ExternalAgentProvider provider,
    required String baseUrl,
    required String model,
    required String credentialRef,
  }) async {
    final agent = ExternalAgent(
      agentId: agentId,
      displayName: displayName,
      provider: provider,
      baseUrl: baseUrl,
      model: model,
      credentialRef: credentialRef,
      credentialAvailable: true,
    );
    updated.add(agent);
    final index = agents.indexWhere((a) => a.agentId == agentId);
    if (index >= 0) agents[index] = agent;
    return agent;
  }

  @override
  Future<void> deleteExternalAgent({required String agentId}) async {
    deleted.add(agentId);
    agents.removeWhere((a) => a.agentId == agentId);
    routes.removeWhere((_, id) => id == agentId);
  }

  @override
  Future<ExternalAgent?> getSessionAgent({required String sessionId}) async {
    if (holdSessionAgent) {
      final gate = _held ??= Completer<void>();
      await gate.future;
    }
    final error = sessionAgentError;
    if (error != null) throw error;
    return _routed(sessionId);
  }

  @override
  Future<ExternalAgent?> setSessionAgent({
    required String sessionId,
    required String agentId,
  }) async {
    final error = setError;
    if (error != null) throw error;
    routes[sessionId] = agentId;
    return _routed(sessionId);
  }

  @override
  Future<ExternalAgent?> clearSessionAgent({required String sessionId}) async {
    routes.remove(sessionId);
    return null;
  }

  ExternalAgent? _routed(String sessionId) {
    final agentId = routes[sessionId];
    if (agentId == null) return null;
    for (final agent in agents) {
      if (agent.agentId == agentId) return agent;
    }
    return null;
  }

  @override
  Future<List<AgentDescriptor>> listAgents() async {
    final error = listError;
    if (error != null) throw error;
    return const [
      AgentDescriptor(
        id: 'AGENT_ID_GENERAL_ASSISTANT',
        displayName: 'General Assistant',
      ),
    ];
  }

  @override
  Future<List<ToolDescriptor>> listTools() async => const [];

  @override
  Future<Map<String, dynamic>> getConfig() async => const {
    'enabledProviders': ['ollama'],
  };

  @override
  Future<Map<String, dynamic>> createSession({String? title}) async => {
    'sessionId': 'sess_1',
  };

  @override
  Future<List<Session>> listSessions({int limit = 50, String? after}) async =>
      const [];

  @override
  Future<Session> getSession({required String sessionId}) async => Session(
    sessionId: sessionId,
    title: null,
    updatedAt: DateTime.utc(2026, 5, 10),
  );

  @override
  Future<void> deleteSession({required String sessionId}) async {}

  @override
  Future<List<Message>> listMessages({
    required String sessionId,
    int limit = 50,
    String? before,
  }) async => const [];

  @override
  Future<List<SearchHit>> searchMessages({
    required String query,
    int limit = 50,
  }) async => const [];

  @override
  Future<TuringEventPage> listEvents({
    required String sessionId,
    int? after,
    int limit = 500,
  }) async => const TuringEventPage(events: [], latestSequence: 0);

  @override
  Future<Map<String, dynamic>> sendMessage({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
    String? idempotencyKey,
  }) async => {'runId': 'run_1'};

  @override
  Future<Map<String, dynamic>> approveApproval(
    String approvalId, {
    String? comment,
  }) async => {'approvalId': approvalId, 'status': 'approved'};

  @override
  Future<Map<String, dynamic>> denyApproval(
    String approvalId, {
    String? reason,
  }) async => {'approvalId': approvalId, 'status': 'denied'};
}
