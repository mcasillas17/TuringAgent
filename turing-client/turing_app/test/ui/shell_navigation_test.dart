import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/chat_screen.dart';
import 'package:turing_flutter_app/features/workspace/session_skills_bar.dart';
import 'package:turing_flutter_app/features/workspace/skills_page.dart';
import 'package:turing_flutter_app/features/workspace/workspace_pages.dart';
import 'package:turing_flutter_app/models/agent_descriptor.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/models/search_hit.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/skill.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';
import 'package:turing_flutter_app/models/turing_event.dart';
import 'package:turing_flutter_app/networking/api_client.dart';
import 'package:turing_flutter_app/networking/auth_storage.dart';
import 'package:turing_flutter_app/networking/event_source.dart';
import 'package:turing_flutter_app/ui/shell/responsive_shell.dart';
import 'package:turing_flutter_app/ui/shell/shell_destination.dart';

/// Wide enough to keep the sidebar beside the conversation.
const Size _desktop = Size(1400, 900);

/// Narrower than [ResponsiveShell.compactBreakpoint], so the sidebar has to
/// become a drawer.
const Size _phone = Size(420, 900);

void main() {
  group('destinations', () {
    testWidgets('an unimplemented destination says so and says why', (
      tester,
    ) async {
      await _pumpShell(tester, api: _FakeApi(), size: _desktop);

      await tester.tap(find.text('Automations'));
      await tester.pumpAndSettle();

      expect(find.byType(PlannedDestinationPage), findsOneWidget);
      expect(find.text('Not built yet'), findsOneWidget);
      // The specific obstacle, not a generic "coming soon" — this is the
      // sentence that stops the gap reading as a bug.
      expect(
        find.textContaining('nobody is watching'),
        findsOneWidget,
        reason: 'the page explains what actually blocks it',
      );
    });

    testWidgets('MCPs lists discovered tools grouped by server', (
      tester,
    ) async {
      final api = _FakeApi()
        ..tools = const [
          ToolDescriptor(
            serverName: 'files',
            toolName: 'write_file',
            policy: ToolPolicy.approvalRequired,
          ),
          ToolDescriptor(
            serverName: 'files',
            toolName: 'read_file',
            policy: ToolPolicy.safe,
          ),
          ToolDescriptor(
            serverName: 'system',
            toolName: 'get_time',
            policy: ToolPolicy.safe,
          ),
        ];
      await _pumpShell(tester, api: api, size: _desktop);

      await tester.tap(find.text('MCPs'));
      await tester.pumpAndSettle();

      expect(find.text('files'), findsOneWidget);
      expect(find.text('system'), findsOneWidget);
      expect(find.text('2 tools'), findsOneWidget);
      // Singular, not "1 tools".
      expect(find.text('1 tool'), findsOneWidget);

      // The policy is the operationally important part: which tools stop to
      // ask before they run.
      expect(find.text('Asks first'), findsOneWidget);
      expect(find.text('Runs freely'), findsNWidgets(2));
    });

    testWidgets('a tool policy this build does not know is not called safe', (
      tester,
    ) async {
      final api = _FakeApi()
        ..tools = const [
          ToolDescriptor(
            serverName: 'files',
            toolName: 'mystery',
            policy: ToolPolicy.unspecified,
          ),
        ];
      await _pumpShell(tester, api: api, size: _desktop);

      await tester.tap(find.text('MCPs'));
      await tester.pumpAndSettle();

      expect(find.text('Unknown policy'), findsOneWidget);
      expect(find.text('Runs freely'), findsNothing);
    });

    testWidgets('Agents does not imply routing that does not exist', (
      tester,
    ) async {
      final api = _FakeApi()
        ..agents = const [
          AgentDescriptor(
            id: 'AGENT_ID_GENERAL_ASSISTANT',
            displayName: 'General Assistant',
          ),
        ];
      await _pumpShell(tester, api: api, size: _desktop);

      await tester.tap(find.text('Agents'));
      await tester.pumpAndSettle();

      expect(find.text('General Assistant'), findsOneWidget);
      // Without this the single row reads as a list that failed to load.
      expect(
        find.textContaining('Routing to other agents is not built yet'),
        findsOneWidget,
      );
    });

    testWidgets('a backend failure offers a retry instead of an empty page', (
      tester,
    ) async {
      final api = _FakeApi()..toolsError = StateError('backend down');
      await _pumpShell(tester, api: api, size: _desktop);

      await tester.tap(find.text('MCPs'));
      await tester.pumpAndSettle();

      expect(find.text('Could not reach the backend'), findsOneWidget);
      expect(find.text('Try again'), findsOneWidget);
    });
  });

  group('skills in the shell', () {
    testWidgets('Skills opens the real library, not a placeholder', (
      tester,
    ) async {
      await _pumpShell(tester, api: _FakeApi(), size: _desktop);

      await tester.tap(find.text('Skills'));
      await tester.pumpAndSettle();

      expect(find.byType(SkillsPage), findsOneWidget);
      expect(find.byType(PlannedDestinationPage), findsNothing);
    });

    testWidgets('the attached skills sit above the conversation', (
      tester,
    ) async {
      final api = _FakeApi()
        ..skills.add(
          const Skill(
            skillId: 'skill_1',
            name: 'Tone',
            instructions: 'Be brief.',
          ),
        )
        ..attached['sess_existing'] = ['skill_1'];
      await _pumpShell(tester, api: api, size: _desktop);

      await tester.tap(find.text('Existing chat'));
      await tester.pumpAndSettle();

      expect(find.byType(SessionSkillsBar), findsOneWidget);
      // Above the transcript, not below it: standing instructions that change
      // the answers should be visible while you read them.
      final bar = tester.getTopLeft(find.byType(SessionSkillsBar));
      final chat = tester.getTopLeft(find.byType(ChatScreen));
      expect(bar.dy, lessThan(chat.dy));
    });

    testWidgets('switching conversations never shows the previous set', (
      tester,
    ) async {
      final api = _FakeApi()
        ..sessions = [
          Session(
            sessionId: 'sess_a',
            title: 'Chat A',
            updatedAt: DateTime.utc(2026, 5, 11),
          ),
          Session(
            sessionId: 'sess_b',
            title: 'Chat B',
            updatedAt: DateTime.utc(2026, 5, 10),
          ),
        ]
        ..skills.add(
          const Skill(
            skillId: 'skill_1',
            name: 'Tone',
            instructions: 'Be brief.',
          ),
        )
        ..attached['sess_a'] = ['skill_1'];
      await _pumpShell(tester, api: api, size: _desktop);

      await tester.tap(find.text('Chat A'));
      await tester.pumpAndSettle();
      expect(find.text('Tone'), findsOneWidget);

      await tester.tap(find.text('Chat B'));
      await tester.pumpAndSettle();

      // B has none attached; showing A's would misstate what B is following.
      expect(find.text('Tone'), findsNothing);
      expect(find.text('No skills attached'), findsOneWidget);
    });
  });

  group('conversation naming', () {
    testWidgets('a new conversation is created without a title', (
      tester,
    ) async {
      final api = _FakeApi();
      await _pumpShell(tester, api: api, size: _desktop);

      // The sidebar's button is the first of the two "New chat" affordances;
      // the empty state's is the other.
      await tester.tap(find.text('New chat').first);
      await tester.pumpAndSettle();

      // Sending any title here would permanently defeat the backend's
      // auto-naming, which only fills a title that is still empty.
      expect(api.createSessionTitles, [null]);
    });

    testWidgets('an untitled conversation reads as "New chat"', (tester) async {
      final api = _FakeApi()
        ..sessions = [
          Session(
            sessionId: 'sess_fresh',
            title: null,
            updatedAt: DateTime.utc(2026, 5, 10),
          ),
        ];
      await _pumpShell(tester, api: api, size: _desktop);

      // The sidebar button, the empty state's button, and the row itself.
      expect(find.text('New chat'), findsNWidgets(3));
      expect(find.text('Untitled chat'), findsNothing);
    });

    testWidgets('sending a message re-reads the renamed conversation list', (
      tester,
    ) async {
      final api = _FakeApi()
        ..sessions = [
          Session(
            sessionId: 'sess_fresh',
            title: null,
            updatedAt: DateTime.utc(2026, 5, 10),
          ),
        ];
      await _pumpShell(tester, api: api, size: _desktop);

      // Open the untitled conversation. Two "New chat" labels now: the
      // sidebar's button and the untitled row itself.
      await tester.tap(find.text('New chat').last);
      await tester.pumpAndSettle();
      expect(find.text('New chat'), findsNWidgets(2));

      // The backend names the session inside the same transaction that queues
      // the run, so by the time sendMessage resolves the list is stale.
      api.sessions = [
        Session(
          sessionId: 'sess_fresh',
          title: 'What is in the sandbox?',
          updatedAt: DateTime.utc(2026, 5, 11),
        ),
      ];
      final callsBefore = api.listSessionsCalls;

      await tester.enterText(
        find.byType(TextField).last,
        'What is in the sandbox?',
      );
      await tester.testTextInput.receiveAction(TextInputAction.done);
      await tester.pumpAndSettle();

      expect(
        api.listSessionsCalls,
        greaterThan(callsBefore),
        reason: 'sending re-reads the list the backend has just renamed',
      );
      // The count, not the text: the message bubble renders the same string,
      // so asserting the title appears would pass even if the sidebar never
      // refreshed. Only the row losing its placeholder proves it did.
      expect(
        find.text('New chat'),
        findsOneWidget,
        reason: 'only the sidebar button is left; the row now has a name',
      );
    });
  });

  group('compact layout', () {
    testWidgets('below the breakpoint the sidebar is a drawer, not a column', (
      tester,
    ) async {
      await _pumpShell(tester, api: _FakeApi(), size: _phone);

      // Hidden until asked for: the conversation gets the whole screen.
      expect(find.text('Existing chat'), findsNothing);
      expect(find.byTooltip('Open navigation menu'), findsOneWidget);

      await tester.tap(find.byTooltip('Open navigation menu'));
      await tester.pumpAndSettle();
      expect(find.text('Existing chat'), findsOneWidget);
    });

    testWidgets('picking a conversation closes the drawer covering it', (
      tester,
    ) async {
      await _pumpShell(tester, api: _FakeApi(), size: _phone);

      await tester.tap(find.byTooltip('Open navigation menu'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Existing chat'));
      await tester.pumpAndSettle();

      // Still open, the user would be staring at the sidebar instead of the
      // conversation they just chose.
      expect(find.byType(Drawer), findsNothing);
    });

    testWidgets('picking a destination closes the drawer too', (tester) async {
      await _pumpShell(tester, api: _FakeApi(), size: _phone);

      await tester.tap(find.byTooltip('Open navigation menu'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Integrations'));
      await tester.pumpAndSettle();

      expect(find.byType(Drawer), findsNothing);
      expect(find.byType(PlannedDestinationPage), findsOneWidget);
    });

    testWidgets('above the breakpoint the sidebar is always present', (
      tester,
    ) async {
      await _pumpShell(tester, api: _FakeApi(), size: _desktop);

      expect(find.byTooltip('Open navigation menu'), findsNothing);
      expect(find.text('Existing chat'), findsOneWidget);
    });
  });

  group('layout changes must not destroy live state', () {
    testWidgets('crossing the breakpoint keeps the conversation alive', (
      tester,
    ) async {
      final api = _FakeApi();
      await _pumpShell(tester, api: api, size: _desktop);
      await tester.tap(find.text('Existing chat'));
      await tester.pumpAndSettle();

      final messagesBefore = api.listMessagesCalls;
      final sourcesBefore = _FakeEventSource.created;
      expect(messagesBefore, greaterThan(0), reason: 'the chat did load');

      // An iPhone is 844 logical px wide in landscape, so a single rotation
      // crosses the 840 breakpoint. If the layout rebuilds the subtree the
      // user loses history, scroll position, their draft, and the event
      // subscription is torn down and reopened.
      tester.view.physicalSize = _phone;
      await tester.pumpAndSettle();

      expect(
        api.listMessagesCalls,
        messagesBefore,
        reason: 'history must not be re-fetched on rotation',
      );
      expect(
        _FakeEventSource.created - sourcesBefore,
        0,
        reason: 'the event subscription must survive the rotation',
      );
    });

    testWidgets('a destination page does not re-query on rotation', (
      tester,
    ) async {
      final api = _FakeApi();
      await _pumpShell(tester, api: api, size: _desktop);
      await tester.tap(find.text('MCPs'));
      await tester.pumpAndSettle();
      expect(api.listToolsCalls, 1);

      tester.view.physicalSize = _phone;
      await tester.pumpAndSettle();

      expect(api.listToolsCalls, 1);
    });
  });

  group('the sidebar fits small screens', () {
    // The nav destinations added ~200px of fixed height to a column that also
    // holds a header, a button, the chat list and a footer. On a short screen
    // the part that overflowed was the footer — where Settings lives.
    for (final size in const [
      Size(568, 320), // iPhone SE, landscape
      Size(740, 360), // common Android, landscape
      Size(844, 390), // iPhone 14 landscape — above the breakpoint, no drawer
      Size(300, 400),
    ]) {
      testWidgets('no overflow at ${size.width}x${size.height}', (
        tester,
      ) async {
        await _pumpShell(tester, api: _FakeApi(), size: size);
        if (size.width < 840) {
          await tester.tap(find.byTooltip('Open navigation menu'));
          await tester.pumpAndSettle();
        }
        expect(tester.takeException(), isNull);
      });
    }

    testWidgets('Settings stays reachable on a short wide window', (
      tester,
    ) async {
      await _pumpShell(tester, api: _FakeApi(), size: const Size(844, 390));

      // No drawer at this width, so a clipped footer would be unrecoverable.
      expect(find.byTooltip('Settings'), findsOneWidget);
      await tester.tap(find.byTooltip('Settings'));
      await tester.pumpAndSettle();
      expect(find.text('Backend URL'), findsOneWidget);
    });
  });

  group('returning to the conversation list', () {
    testWidgets('the Chats header navigates back with no sessions', (
      tester,
    ) async {
      final api = _FakeApi()..sessions = [];
      await _pumpShell(tester, api: api, size: _desktop);

      await tester.tap(find.text('Integrations'));
      await tester.pumpAndSettle();
      expect(find.byType(PlannedDestinationPage), findsOneWidget);

      // With no conversations to tap, this header is the only way back that
      // does not create something.
      await tester.tap(find.text('Chats'));
      await tester.pumpAndSettle();

      expect(find.byType(PlannedDestinationPage), findsNothing);
      expect(find.text('Ask Turing anything'), findsOneWidget);
      expect(api.createSessionTitles, isEmpty, reason: 'nothing was created');
    });
  });

  group('retrying a failed load', () {
    testWidgets('Try again re-queries and renders what comes back', (
      tester,
    ) async {
      final api = _FakeApi()..toolsError = StateError('backend down');
      await _pumpShell(tester, api: api, size: _desktop);
      await tester.tap(find.text('MCPs'));
      await tester.pumpAndSettle();
      expect(api.listToolsCalls, 1);

      api
        ..toolsError = null
        ..tools = const [
          ToolDescriptor(
            serverName: 'files',
            toolName: 'read_file',
            policy: ToolPolicy.safe,
          ),
        ];
      await tester.tap(find.text('Try again'));
      await tester.pumpAndSettle();

      expect(api.listToolsCalls, 2);
      expect(find.text('read_file'), findsOneWidget);
      expect(find.text('Could not reach the backend'), findsNothing);
    });

    testWidgets('the Agents page surfaces its own failure', (tester) async {
      final api = _FakeApi()..agentsError = StateError('backend down');
      await _pumpShell(tester, api: api, size: _desktop);

      await tester.tap(find.text('Agents'));
      await tester.pumpAndSettle();

      expect(find.text('Could not reach the backend'), findsOneWidget);
    });

    testWidgets('no discovered tools says so rather than showing nothing', (
      tester,
    ) async {
      await _pumpShell(tester, api: _FakeApi(), size: _desktop);

      await tester.tap(find.text('MCPs'));
      await tester.pumpAndSettle();

      expect(find.text('No tools discovered'), findsOneWidget);
    });
  });

  group('the compact app bar', () {
    testWidgets('names the conversation you are in, not the app', (
      tester,
    ) async {
      await _pumpShell(tester, api: _FakeApi(), size: _phone);
      expect(find.text('Turing'), findsOneWidget);

      await tester.tap(find.byTooltip('Open navigation menu'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Existing chat'));
      await tester.pumpAndSettle();

      // The sidebar is hidden here, so the bar is the only thing that can say
      // which conversation is open.
      expect(find.text('Existing chat'), findsOneWidget);
      expect(find.text('Turing'), findsNothing);
    });

    testWidgets('shows the destination name and drops the new-chat action', (
      tester,
    ) async {
      await _pumpShell(tester, api: _FakeApi(), size: _phone);
      expect(find.byTooltip('New chat'), findsOneWidget);

      await tester.tap(find.byTooltip('Open navigation menu'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('MCPs'));
      await tester.pumpAndSettle();

      expect(find.text('MCPs'), findsWidgets);
      expect(
        find.byTooltip('New chat'),
        findsNothing,
        reason: 'creating a chat from the MCPs page is not what + means',
      );
    });
  });

  test('every destination either works or explains itself', () {
    for (final destination in ShellDestination.values) {
      if (destination.implemented) continue;
      expect(
        destination.summary,
        isNotNull,
        reason: '${destination.label} must say what it is for',
      );
      expect(
        destination.blockedOn,
        isNotNull,
        reason: '${destination.label} must say what blocks it',
      );
    }
  });
}

Future<void> _pumpShell(
  WidgetTester tester, {
  required _FakeApi api,
  required Size size,
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);

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
}

class _FakeApi implements TuringApi {
  List<Session> sessions = [
    Session(
      sessionId: 'sess_existing',
      title: 'Existing chat',
      updatedAt: DateTime.utc(2026, 5, 10),
    ),
  ];
  List<ToolDescriptor> tools = const [];
  List<AgentDescriptor> agents = const [];
  Object? toolsError;
  Object? agentsError;
  int listToolsCalls = 0;
  int listMessagesCalls = 0;
  final List<String?> createSessionTitles = [];
  final List<String> sentMessages = [];

  int listSessionsCalls = 0;

  @override
  Future<List<Session>> listSessions({int limit = 50, String? after}) async {
    listSessionsCalls++;
    return sessions;
  }

  @override
  Future<List<ToolDescriptor>> listTools() async {
    listToolsCalls++;
    final error = toolsError;
    if (error != null) throw error;
    return tools;
  }

  /// A working in-memory library, so the Skills UI is tested against
  /// something that behaves like the backend rather than a stub that always
  /// says yes.
  final List<Skill> skills = [];
  final Map<String, List<String>> attached = {};
  Object? skillsError;
  int nextSkillId = 1;

  @override
  Future<List<Skill>> listSkills() async {
    final error = skillsError;
    if (error != null) throw error;
    return List.unmodifiable(skills);
  }

  @override
  Future<Skill> createSkill({
    required String name,
    required String instructions,
  }) async {
    if (skills.any((s) => s.name.toLowerCase() == name.toLowerCase())) {
      throw StateError('a skill with that name already exists');
    }
    final skill = Skill(
      skillId: 'skill_${nextSkillId++}',
      name: name,
      instructions: instructions,
    );
    skills.add(skill);
    return skill;
  }

  @override
  Future<Skill> updateSkill({
    required String skillId,
    required String name,
    required String instructions,
  }) async {
    final index = skills.indexWhere((s) => s.skillId == skillId);
    if (index < 0) throw StateError('skill not found');
    final updated = Skill(
      skillId: skillId,
      name: name,
      instructions: instructions,
    );
    skills[index] = updated;
    return updated;
  }

  @override
  Future<void> deleteSkill({required String skillId}) async {
    skills.removeWhere((s) => s.skillId == skillId);
    for (final ids in attached.values) {
      ids.remove(skillId);
    }
  }

  @override
  Future<List<Skill>> attachSkill({
    required String sessionId,
    required String skillId,
  }) async {
    final ids = attached.putIfAbsent(sessionId, () => []);
    if (!ids.contains(skillId)) ids.add(skillId);
    return listSessionSkills(sessionId: sessionId);
  }

  @override
  Future<List<Skill>> detachSkill({
    required String sessionId,
    required String skillId,
  }) async {
    attached[sessionId]?.remove(skillId);
    return listSessionSkills(sessionId: sessionId);
  }

  @override
  Future<List<Skill>> listSessionSkills({required String sessionId}) async {
    final ids = attached[sessionId] ?? const <String>[];
    return skills.where((s) => ids.contains(s.skillId)).toList();
  }

  @override
  Future<List<AgentDescriptor>> listAgents() async {
    final error = agentsError;
    if (error != null) throw error;
    return agents;
  }

  @override
  Future<Map<String, dynamic>> createSession({String? title}) async {
    createSessionTitles.add(title);
    return {'sessionId': 'sess_new', 'createdAt': '2026-05-10T00:00:00.000Z'};
  }

  @override
  Future<Map<String, dynamic>> sendMessage({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
  }) async {
    sentMessages.add(content);
    return {'sessionId': sessionId, 'runId': 'run_1', 'status': 'queued'};
  }

  @override
  Future<void> deleteSession({required String sessionId}) async {}

  @override
  Future<Session> getSession({required String sessionId}) async => Session(
    sessionId: sessionId,
    title: null,
    updatedAt: DateTime.utc(2026, 5, 10),
  );

  @override
  Future<List<Message>> listMessages({
    required String sessionId,
    int limit = 50,
    String? before,
  }) async {
    listMessagesCalls++;
    return const [];
  }

  @override
  Future<TuringEventPage> listEvents({
    required String sessionId,
    int? after,
    int limit = 500,
  }) async => const TuringEventPage(events: [], latestSequence: 0);

  @override
  Future<List<SearchHit>> searchMessages({
    required String query,
    int limit = 50,
  }) async => const [];

  @override
  Future<Map<String, dynamic>> getConfig() async => {
    'enabledProviders': ['ollama'],
  };

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

class _FakeEventSource implements TuringEventSource {
  /// Counts constructions across the test, so a test can prove the shell did
  /// NOT tear down and rebuild a live subscription.
  static int created = 0;

  _FakeEventSource() {
    created++;
  }

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
