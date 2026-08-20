import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/chat_screen.dart';
import 'package:turing_flutter_app/features/workspace/agents_page.dart';
import 'package:turing_flutter_app/features/workspace/session_agent_bar.dart';
import 'package:turing_flutter_app/features/workspace/integrations_page.dart';
import 'package:turing_flutter_app/features/workspace/automations_page.dart';
import 'package:turing_flutter_app/features/workspace/skills_page.dart';
import 'package:turing_flutter_app/features/workspace/telemetry_page.dart';
import 'package:turing_flutter_app/models/agent_descriptor.dart';
import 'package:turing_flutter_app/models/external_agent.dart';
import 'package:turing_flutter_app/models/automation.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/models/search_hit.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/session_page.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';
import 'package:turing_flutter_app/models/turing_event.dart';
import 'package:turing_flutter_app/networking/api_client.dart';
import 'package:turing_flutter_app/networking/auth_storage.dart';
import 'package:turing_flutter_app/networking/event_source.dart';
import 'package:turing_flutter_app/ui/shell/responsive_shell.dart';
import 'package:turing_flutter_app/ui/shell/shell_destination.dart';

import '../support/no_audit_api.dart';
import '../support/no_integrations_api.dart';
import '../support/no_automations_api.dart';
import '../support/no_telemetry_api.dart';
import '../support/no_skills_api.dart';

/// Wide enough to keep the sidebar beside the conversation.
const Size _desktop = Size(1400, 900);

/// Narrower than [ResponsiveShell.compactBreakpoint], so the sidebar has to
/// become a drawer.
const Size _phone = Size(420, 900);

void main() {
  group('destinations', () {
    // Integrations was this test's example of an unbuilt destination until it
    // shipped. With every destination now implemented, the claim worth pinning
    // is the inverse: nothing in the rail leads to a placeholder.
    testWidgets('every destination opens something real', (tester) async {
      // Derived from the enum rather than hand-listed: a destination added to
      // the rail but forgotten here would go unchecked, and nothing would
      // fail.
      for (final label in ShellDestination.navigation.map((d) => d.label)) {
        await _pumpShell(tester, api: _FakeApi(), size: _desktop);
        await tester.tap(find.text(label));
        await tester.pumpAndSettle();

        expect(
          find.text('Not built yet'),
          findsNothing,
          reason: '$label still says it is unbuilt',
        );
      }
    });

    testWidgets('Automations opens the real page, not a placeholder', (
      tester,
    ) async {
      await _pumpShell(tester, api: _FakeApi(), size: _desktop);

      await tester.tap(find.text('Automations'));
      await tester.pumpAndSettle();

      expect(find.byType(AutomationsPage), findsOneWidget);
      // The "not built yet" card is gone, because it is.
      expect(find.text('Not built yet'), findsNothing);
      expect(find.text('New automation'), findsOneWidget);
    });

    testWidgets('Telemetry opens the real page, not a placeholder', (
      tester,
    ) async {
      await _pumpShell(tester, api: _FakeApi(), size: _desktop);

      await tester.tap(find.text('Telemetry'));
      await tester.pumpAndSettle();

      expect(find.byType(TelemetryPage), findsOneWidget);
      expect(find.text('Not built yet'), findsNothing);
      // Something only the real page draws.
      expect(find.text('7 days'), findsOneWidget);
      expect(find.text('Tokens'), findsOneWidget);
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

    testWidgets('Agents lists the local assistant and what you added', (
      tester,
    ) async {
      final api = _FakeApi()
        ..agents = const [
          AgentDescriptor(
            id: 'AGENT_ID_GENERAL_ASSISTANT',
            displayName: 'General Assistant',
          ),
        ]
        ..externalAgents.add(
          const ExternalAgent(
            agentId: 'agent_1',
            displayName: 'Claude',
            provider: ExternalAgentProvider.anthropic,
            baseUrl: 'https://api.anthropic.com/v1',
            model: 'claude-sonnet-4-5',
            credentialRef: 'claude',
            credentialAvailable: true,
          ),
        );
      await _pumpShell(tester, api: api, size: _desktop);

      await tester.tap(find.text('Agents'));
      await tester.pumpAndSettle();

      expect(find.byType(AgentsPage), findsOneWidget);
      expect(find.text('General Assistant'), findsOneWidget);
      expect(find.text('Claude'), findsOneWidget);
      // The two kinds are not interchangeable rows in one list, and the page
      // has to say which is which.
      expect(find.text('These receive whatever you send them'), findsOneWidget);
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
    });
  });

  group('where a conversation goes', () {
    testWidgets('the destination sits above everything else in the chat', (
      tester,
    ) async {
      await _pumpShell(tester, api: _FakeApi(), size: _desktop);

      await tester.tap(find.text('Existing chat'));
      await tester.pumpAndSettle();

      expect(find.byType(SessionAgentBar), findsOneWidget);
      // Above the transcript: routing decides whether anything typed below it
      // leaves the machine.
      final agentBar = tester.getTopLeft(find.byType(SessionAgentBar));
      final chat = tester.getTopLeft(find.byType(ChatScreen));
      expect(agentBar.dy, lessThan(chat.dy));
    });

    testWidgets('a routed conversation says so on the conversation itself', (
      tester,
    ) async {
      final api = _FakeApi()
        ..externalAgents.add(
          const ExternalAgent(
            agentId: 'agent_1',
            displayName: 'Claude',
            provider: ExternalAgentProvider.anthropic,
            baseUrl: 'https://api.anthropic.com/v1',
            model: 'claude-sonnet-4-5',
            credentialRef: 'claude',
            credentialAvailable: true,
          ),
        )
        ..routes['sess_existing'] = 'agent_1';
      await _pumpShell(tester, api: api, size: _desktop);

      await tester.tap(find.text('Existing chat'));
      await tester.pumpAndSettle();

      expect(
        find.text('Goes to Claude — messages leave your machine'),
        findsOneWidget,
      );
    });

    testWidgets(
      'switching conversations never shows the previous destination',
      (tester) async {
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
          ..externalAgents.add(
            const ExternalAgent(
              agentId: 'agent_1',
              displayName: 'Claude',
              provider: ExternalAgentProvider.anthropic,
              baseUrl: 'https://api.anthropic.com/v1',
              model: 'claude-sonnet-4-5',
              credentialRef: 'claude',
              credentialAvailable: true,
            ),
          )
          ..routes['sess_a'] = 'agent_1';
        await _pumpShell(tester, api: api, size: _desktop);

        await tester.tap(find.text('Chat A'));
        await tester.pumpAndSettle();
        expect(
          find.text('Goes to Claude — messages leave your machine'),
          findsOneWidget,
        );

        await tester.tap(find.text('Chat B'));
        await tester.pumpAndSettle();

        // B is local; carrying A's warning over would be a lie about egress in
        // the one place the user is told about it.
        expect(
          find.text('Goes to Claude — messages leave your machine'),
          findsNothing,
        );
        expect(
          find.text('Turing — this conversation stays on your machine'),
          findsOneWidget,
        );
      },
    );
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

    testWidgets('sending a message does not poll the conversation list', (
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
      final callsBefore = api.listSessionsCalls;

      await tester.enterText(
        find.byType(TextField).last,
        'What is in the sandbox?',
      );
      await tester.testTextInput.receiveAction(TextInputAction.done);
      await tester.pumpAndSettle();

      expect(
        api.listSessionsCalls,
        callsBefore,
        reason: 'the durable session.updated event owns list refreshes',
      );
    });

    testWidgets('a session update event renames and reorders the row locally', (
      tester,
    ) async {
      final source = _FakeEventSource();
      final api = _FakeApi()
        ..sessions = [
          Session(
            sessionId: 'sess_other',
            title: 'Other chat',
            updatedAt: DateTime.utc(2026, 5, 11),
          ),
          Session(
            sessionId: 'sess_fresh',
            title: null,
            updatedAt: DateTime.utc(2026, 5, 10),
          ),
        ];
      await _pumpShell(
        tester,
        api: api,
        size: _desktop,
        eventSourceFactory: () => source,
      );
      await tester.tap(find.text('New chat').last);
      await tester.pumpAndSettle();
      final callsBefore = api.listSessionsCalls;

      source.add(
        TuringEvent(
          eventId: 'evt_session_updated',
          sessionId: 'sess_fresh',
          traceId: 'trace_1',
          sequence: 1,
          type: 'session.updated',
          createdAt: DateTime.utc(2026, 8, 18, 20),
          payload: const {
            'title': 'What is in the sandbox?',
            'updatedAt': '2026-08-18T20:00:00Z',
          },
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('What is in the sandbox?'), findsOneWidget);
      expect(
        tester.getTopLeft(find.text('What is in the sandbox?')).dy,
        lessThan(tester.getTopLeft(find.text('Other chat')).dy),
        reason: 'the updated conversation becomes the most recent row',
      );
      expect(
        api.listSessionsCalls,
        callsBefore,
        reason: 'the event payload is authoritative; no list poll is needed',
      );
    });

    testWidgets('a replayed older session update does not reorder the list', (
      tester,
    ) async {
      final source = _FakeEventSource();
      final api = _FakeApi()
        ..sessions = [
          Session(
            sessionId: 'sess_recent',
            title: 'Recent chat',
            updatedAt: DateTime.utc(2026, 5, 11),
          ),
          Session(
            sessionId: 'sess_old',
            title: 'Old chat',
            updatedAt: DateTime.utc(2026, 5, 10),
          ),
        ];
      await _pumpShell(
        tester,
        api: api,
        size: _desktop,
        eventSourceFactory: () => source,
      );
      await tester.tap(find.text('Old chat'));
      await tester.pumpAndSettle();

      source.add(
        TuringEvent(
          eventId: 'evt_replayed_session_updated',
          sessionId: 'sess_old',
          traceId: 'trace_old',
          sequence: 1,
          type: 'session.updated',
          createdAt: DateTime.utc(2026, 5, 10),
          payload: const {
            'title': 'Old chat',
            'updatedAt': '2026-05-10T00:00:00Z',
          },
        ),
      );
      await tester.pumpAndSettle();

      expect(
        tester.getTopLeft(find.text('Recent chat')).dy,
        lessThan(tester.getTopLeft(find.text('Old chat')).dy),
        reason: 'durable activity time, not replay delivery, owns ordering',
      );
    });

    testWidgets('load more appends the next stable cursor page', (
      tester,
    ) async {
      final api = _FakeApi()
        ..nextSessionCursor = 'cursor-2'
        ..sessionPages['cursor-2'] = SessionPage(
          sessions: [
            Session(
              sessionId: 'sess_older',
              title: 'Older chat',
              updatedAt: DateTime.utc(2026, 5, 9),
            ),
          ],
        );
      await _pumpShell(tester, api: api, size: _desktop);

      await tester.tap(find.widgetWithText(TextButton, 'Load more'));
      await tester.pumpAndSettle();

      expect(find.text('Existing chat'), findsOneWidget);
      expect(find.text('Older chat'), findsOneWidget);
      expect(api.sessionPageCursors, [null, 'cursor-2']);
      expect(find.widgetWithText(TextButton, 'Load more'), findsNothing);
    });

    testWidgets('rename uses the authoritative returned session snapshot', (
      tester,
    ) async {
      final api = _FakeApi();
      await _pumpShell(tester, api: api, size: _desktop);
      await tester.tap(find.text('Existing chat'));
      await tester.pumpAndSettle();

      await tester.tap(find.byTooltip('Rename chat'));
      await tester.pumpAndSettle();
      await tester.enterText(
        find.byType(TextFormField).last,
        '  Renamed chat  ',
      );
      await tester.tap(find.widgetWithText(FilledButton, 'Rename'));
      await tester.pumpAndSettle();

      expect(api.renamedTitles, ['  Renamed chat  ']);
      expect(find.text('Server normalized title'), findsOneWidget);
      expect(find.text('Existing chat'), findsNothing);
    });

    testWidgets(
      'archive guard survives refresh and rejects a delayed status-less event',
      (tester) async {
        final globalUpdates = _FakeSessionUpdateSource();
        final api = _FakeApi();
        await _pumpShell(
          tester,
          api: api,
          size: _desktop,
          sessionUpdateSourceFactory: () => globalUpdates,
        );
        await tester.tap(find.text('Existing chat'));
        await tester.pumpAndSettle();

        await tester.tap(find.byTooltip('Archive chat'));
        await tester.pumpAndSettle();
        expect(find.text('Existing chat'), findsNothing);

        api
          ..sessions = [
            Session(
              sessionId: 'sess_existing',
              title: 'Stale active title',
              updatedAt: DateTime.utc(2026, 5, 10),
            ),
          ]
          ..addCreatedSessionToList = true;
        await tester.tap(find.text('New chat').first);
        await tester.pumpAndSettle();
        expect(find.text('Stale active title'), findsNothing);

        globalUpdates.add(
          TuringEvent(
            eventId: 'evt_delayed_legacy',
            sessionId: 'sess_existing',
            traceId: 'trace_legacy',
            sequence: 1,
            type: 'session.updated',
            createdAt: DateTime.utc(2026, 8, 20),
            payload: const {
              'title': 'Delayed legacy title',
              'updatedAt': '2026-08-20T00:00:00.000000000Z',
            },
          ),
        );
        await tester.pumpAndSettle();

        expect(find.text('Delayed legacy title'), findsNothing);
      },
    );

    testWidgets('archived conversations paginate and restore authoritatively', (
      tester,
    ) async {
      final api = _FakeApi()
        ..archivedSessions = [
          Session(
            sessionId: 'sess_archived_1',
            title: 'Archived one',
            updatedAt: DateTime.utc(2026, 5, 9),
            status: SessionStatus.archived,
          ),
        ]
        ..nextArchivedCursor = 'archived-2'
        ..archivedPages['archived-2'] = SessionPage(
          sessions: [
            Session(
              sessionId: 'sess_archived_2',
              title: 'Archived two',
              updatedAt: DateTime.utc(2026, 5, 8),
              status: SessionStatus.archived,
            ),
          ],
        );
      await _pumpShell(tester, api: api, size: _desktop);

      await tester.tap(find.byTooltip('Archived conversations'));
      await tester.pumpAndSettle();
      expect(find.text('Archived one'), findsOneWidget);

      await tester.tap(find.widgetWithText(TextButton, 'Load more'));
      await tester.pumpAndSettle();
      expect(find.text('Archived two'), findsOneWidget);
      expect(api.archivedPageCursors, [null, 'archived-2']);

      await tester.tap(
        find.descendant(
          of: find.widgetWithText(ListTile, 'Archived one'),
          matching: find.byTooltip('Restore chat'),
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('Restored chat'), findsOneWidget);
      expect(find.text('Archived one'), findsNothing);
    });

    testWidgets('an archived conversation can be permanently deleted', (
      tester,
    ) async {
      final api = _FakeApi()
        ..archivedSessions = [
          Session(
            sessionId: 'sess_archived',
            title: 'Archived delete target',
            updatedAt: DateTime.utc(2026, 5, 9),
            status: SessionStatus.archived,
          ),
        ];
      await _pumpShell(tester, api: api, size: _desktop);

      await tester.tap(find.byTooltip('Archived conversations'));
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('Delete archived chat'));
      await tester.pumpAndSettle();
      expect(find.textContaining('cannot be undone'), findsOneWidget);
      await tester.tap(find.widgetWithText(TextButton, 'Delete'));
      await tester.pumpAndSettle();

      expect(api.deletedSessionIds, ['sess_archived']);
      expect(find.text('Archived delete target'), findsNothing);
    });

    testWidgets('a session update survives an older list response', (
      tester,
    ) async {
      final api = _FakeApi();
      final sources = <_FakeEventSource>[];
      await _pumpShell(
        tester,
        api: api,
        size: _desktop,
        eventSourceFactory: () {
          final source = _FakeEventSource();
          sources.add(source);
          return source;
        },
      );
      final delayedList = Completer<List<Session>>();
      api.nextListSessions = delayedList;

      await tester.tap(find.text('New chat').first);
      for (var i = 0; i < 5; i++) {
        await tester.pump();
      }
      expect(sources, isNotEmpty);
      sources.last.add(
        TuringEvent(
          eventId: 'evt_new_session_updated',
          sessionId: 'sess_new',
          traceId: 'trace_new',
          sequence: 1,
          type: 'session.updated',
          createdAt: DateTime.utc(2026, 8, 18, 20),
          payload: const {
            'title': 'Brand new conversation',
            'updatedAt': '2026-08-18T20:00:00Z',
          },
        ),
      );
      await tester.pump();

      delayedList.complete(api.sessions);
      await tester.pumpAndSettle();

      expect(find.text('Brand new conversation'), findsOneWidget);
      expect(
        find.text('Existing chat'),
        findsOneWidget,
        reason: 'the older response is merged rather than discarded wholesale',
      );
    });

    testWidgets('a new untitled session survives an older list response', (
      tester,
    ) async {
      final api = _FakeApi()..addCreatedSessionToList = true;
      final staleSessions = List<Session>.of(api.sessions);
      final delayedList = Completer<List<Session>>();
      api.nextListSessions = delayedList;
      tester.view.physicalSize = _desktop;
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
      for (var i = 0; i < 5; i++) {
        await tester.pump();
      }

      await tester.tap(find.text('New chat').first);
      for (var i = 0; i < 5; i++) {
        await tester.pump();
      }
      delayedList.complete(staleSessions);
      await tester.pumpAndSettle();

      expect(
        find.byTooltip('Delete chat'),
        findsOneWidget,
        reason: 'the active new session remains present in the sidebar',
      );
    });

    testWidgets('a local session survives a limited refresh page', (
      tester,
    ) async {
      final api = _FakeApi();
      await _pumpShell(tester, api: api, size: _desktop);
      final limitedPage = Completer<List<Session>>();
      api.nextListSessions = limitedPage;

      await tester.tap(find.text('New chat').first);
      for (var i = 0; i < 5; i++) {
        await tester.pump();
      }
      limitedPage.complete(api.sessions);
      await tester.pumpAndSettle();

      expect(
        find.byTooltip('Delete chat'),
        findsOneWidget,
        reason: 'page omission does not discard a retained local snapshot',
      );
    });

    testWidgets('equal timestamps use the backend session id tie-breaker', (
      tester,
    ) async {
      final api = _FakeApi();
      await _pumpShell(tester, api: api, size: _desktop);

      await tester.tap(find.text('New chat').first);
      await tester.pumpAndSettle();

      expect(
        tester.getTopLeft(find.text('New chat').last).dy,
        lessThan(tester.getTopLeft(find.text('Existing chat')).dy),
        reason: 'sess_new sorts before sess_existing when timestamps tie',
      );
    });

    testWidgets('nanoseconds outrank the session id tie-breaker', (
      tester,
    ) async {
      final api = _FakeApi()
        ..sessions = [
          Session(
            sessionId: 'sess_z',
            title: 'Earlier nanoseconds',
            updatedAt: DateTime.fromMicrosecondsSinceEpoch(
              1000000,
              isUtc: true,
            ),
            updatedAtNanoseconds: 1000000100,
          ),
        ]
        ..createdSessionId = 'sess_a'
        ..createdSessionTimestamp = '1970-01-01T00:00:01.000000900Z';
      await _pumpShell(tester, api: api, size: _desktop);

      await tester.tap(find.text('New chat').first);
      await tester.pumpAndSettle();

      expect(
        tester.getTopLeft(find.text('New chat').last).dy,
        lessThan(tester.getTopLeft(find.text('Earlier nanoseconds')).dy),
        reason: '900ns is newer than 100ns despite sess_a sorting below sess_z',
      );
    });

    testWidgets('a refresh can remove an externally deleted listed session', (
      tester,
    ) async {
      final source = _FakeEventSource();
      final api = _FakeApi();
      await _pumpShell(
        tester,
        api: api,
        size: _desktop,
        eventSourceFactory: () => source,
      );
      await tester.tap(find.text('Existing chat'));
      await tester.pumpAndSettle();
      source.add(
        TuringEvent(
          eventId: 'evt_existing_updated',
          sessionId: 'sess_existing',
          traceId: 'trace_existing',
          sequence: 1,
          type: 'session.updated',
          createdAt: DateTime.utc(2026, 5, 11),
          payload: const {
            'title': 'Updated existing chat',
            'updatedAt': '2026-05-11T00:00:00.000000000Z',
          },
        ),
      );
      await tester.pumpAndSettle();

      api
        ..sessions = []
        ..addCreatedSessionToList = true;
      await tester.tap(find.text('New chat').first);
      await tester.pumpAndSettle();

      expect(
        find.text('Updated existing chat'),
        findsNothing,
        reason:
            'an observed session snapshot expires when a later page omits it',
      );
    });

    testWidgets('a global update adds a non-active session without polling', (
      tester,
    ) async {
      final globalUpdates = _FakeSessionUpdateSource();
      final api = _FakeApi();
      await _pumpShell(
        tester,
        api: api,
        size: _desktop,
        sessionUpdateSourceFactory: () => globalUpdates,
      );
      await tester.tap(find.text('Existing chat'));
      await tester.pumpAndSettle();
      final callsBefore = api.listSessionsCalls;

      globalUpdates.add(
        TuringEvent(
          eventId: 'evt_automation_session',
          sessionId: 'sess_automation',
          traceId: 'trace_automation',
          sequence: 1,
          type: 'session.updated',
          createdAt: DateTime.utc(2026, 8, 18, 20),
          payload: const {
            'title': 'Morning digest',
            'updatedAt': '2026-08-18T20:00:00.000000000Z',
          },
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('Morning digest'), findsOneWidget);
      expect(
        tester.getTopLeft(find.text('Morning digest')).dy,
        lessThan(tester.getTopLeft(find.text('Existing chat')).dy),
      );
      expect(api.listSessionsCalls, callsBefore);
    });

    testWidgets(
      'an unlisted global update expires after a later page omits it',
      (tester) async {
        final globalUpdates = _FakeSessionUpdateSource();
        final api = _FakeApi();
        await _pumpShell(
          tester,
          api: api,
          size: _desktop,
          sessionUpdateSourceFactory: () => globalUpdates,
        );
        globalUpdates.add(
          TuringEvent(
            eventId: 'evt_off_page',
            sessionId: 'sess_off_page',
            traceId: 'trace_off_page',
            sequence: 1,
            type: 'session.updated',
            createdAt: DateTime.utc(2026, 8, 18, 20),
            payload: const {
              'title': 'Off-page update',
              'updatedAt': '2026-08-18T20:00:00.000000000Z',
            },
          ),
        );
        await tester.pumpAndSettle();
        expect(find.text('Off-page update'), findsOneWidget);

        api.addCreatedSessionToList = true;
        await tester.tap(find.text('New chat').first);
        await tester.pumpAndSettle();

        expect(find.text('Off-page update'), findsNothing);
      },
    );

    testWidgets('a queued global update after teardown is ignored', (
      tester,
    ) async {
      final events = StreamController<TuringEvent>();
      addTearDown(events.close);
      await _pumpShell(
        tester,
        api: _FakeApi(),
        size: _desktop,
        sessionUpdateSourceFactory: () =>
            _UncancellableSessionUpdateSource(events.stream),
      );

      await tester.pumpWidget(const SizedBox());
      events.add(
        TuringEvent(
          eventId: 'evt_after_dispose',
          sessionId: 'sess_after_dispose',
          traceId: 'trace_after_dispose',
          sequence: 1,
          type: 'session.updated',
          createdAt: DateTime.utc(2026, 8, 18, 20),
          payload: const {
            'title': 'Too late',
            'updatedAt': '2026-08-18T20:00:00.000000000Z',
          },
        ),
      );
      await tester.pump();

      expect(tester.takeException(), isNull);
    });

    testWidgets('a failed global stream reconnects and resumes updates', (
      tester,
    ) async {
      final sources = <_FakeSessionUpdateSource>[];
      await _pumpShell(
        tester,
        api: _FakeApi(),
        size: _desktop,
        sessionUpdateSourceFactory: () {
          final source = _FakeSessionUpdateSource();
          sources.add(source);
          return source;
        },
      );
      expect(sources, hasLength(1));

      sources.single.addError(StateError('connection lost'));
      await tester.pump(const Duration(seconds: 1));
      expect(sources, hasLength(2));

      sources.last.add(
        TuringEvent(
          eventId: 'evt_after_reconnect',
          sessionId: 'sess_after_reconnect',
          traceId: 'trace_after_reconnect',
          sequence: 1,
          type: 'session.updated',
          createdAt: DateTime.utc(2026, 8, 18, 20),
          payload: const {
            'title': 'After reconnect',
            'updatedAt': '2026-08-18T20:00:00.000000000Z',
          },
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('After reconnect'), findsOneWidget);
    });

    testWidgets('chat updates do not reset global reconnect backoff', (
      tester,
    ) async {
      final chatSource = _FakeEventSource();
      final globalSources = <_FakeSessionUpdateSource>[];
      await _pumpShell(
        tester,
        api: _FakeApi(),
        size: _desktop,
        eventSourceFactory: () => chatSource,
        sessionUpdateSourceFactory: () {
          final source = _FakeSessionUpdateSource();
          globalSources.add(source);
          return source;
        },
      );
      await tester.tap(find.text('Existing chat'));
      await tester.pumpAndSettle();

      globalSources[0].addError(StateError('first failure'));
      await tester.pump(const Duration(seconds: 1));
      expect(globalSources, hasLength(2));
      globalSources[1].addError(StateError('second failure'));
      await tester.pump(const Duration(seconds: 2));
      expect(globalSources, hasLength(3));

      chatSource.add(
        TuringEvent(
          eventId: 'evt_chat_update',
          sessionId: 'sess_existing',
          traceId: 'trace_chat_update',
          sequence: 1,
          type: 'session.updated',
          createdAt: DateTime.utc(2026, 8, 19),
          payload: const {
            'title': 'Existing chat',
            'updatedAt': '2026-08-19T00:00:00.000000000Z',
          },
        ),
      );
      await tester.pump();

      globalSources[2].addError(StateError('third failure'));
      await tester.pump(const Duration(seconds: 1));
      expect(
        globalSources,
        hasLength(3),
        reason: 'the third global retry remains on the four-second backoff',
      );
      await tester.pump(const Duration(seconds: 3));
      expect(globalSources, hasLength(4));
    });

    testWidgets('replayed updates do not reset global reconnect backoff', (
      tester,
    ) async {
      final globalSources = <_FakeSessionUpdateSource>[];
      await _pumpShell(
        tester,
        api: _FakeApi(),
        size: _desktop,
        sessionUpdateSourceFactory: () {
          final source = _FakeSessionUpdateSource();
          globalSources.add(source);
          return source;
        },
      );

      globalSources[0].addError(StateError('first failure'));
      await tester.pump(const Duration(seconds: 1));
      expect(globalSources, hasLength(2));
      globalSources[1].add(
        TuringEvent(
          eventId: 'evt_replayed_after_reconnect',
          sessionId: 'sess_existing',
          traceId: 'trace_replayed_after_reconnect',
          sequence: 1,
          type: 'session.updated',
          createdAt: DateTime.utc(2026, 8, 19),
          payload: const {
            'title': 'Existing chat',
            'updatedAt': '2026-08-19T00:00:00.000000000Z',
          },
        ),
      );
      await tester.pump();

      globalSources[1].addError(StateError('second failure'));
      await tester.pump(const Duration(seconds: 1));
      expect(
        globalSources,
        hasLength(2),
        reason: 'a replay snapshot does not restart the backoff at one second',
      );
      await tester.pump(const Duration(seconds: 1));
      expect(globalSources, hasLength(3));
    });

    testWidgets('a stable global stream resets reconnect backoff', (
      tester,
    ) async {
      final globalSources = <_FakeSessionUpdateSource>[];
      await _pumpShell(
        tester,
        api: _FakeApi(),
        size: _desktop,
        sessionUpdateSourceFactory: () {
          final source = _FakeSessionUpdateSource();
          globalSources.add(source);
          return source;
        },
      );

      globalSources[0].addError(StateError('first failure'));
      await tester.pump(const Duration(seconds: 1));
      expect(globalSources, hasLength(2));
      await tester.pump(const Duration(seconds: 30));

      globalSources[1].addError(StateError('failure after stable connection'));
      await tester.pump(const Duration(seconds: 1));
      expect(
        globalSources,
        hasLength(3),
        reason: 'thirty stable seconds reset the retry delay to one second',
      );
    });

    testWidgets('stream stability does not hide a conversation load failure', (
      tester,
    ) async {
      final api = _FakeApi()..sessionsError = Exception('backend down');
      await _pumpShell(
        tester,
        api: api,
        size: _desktop,
        sessionUpdateSourceFactory: _FakeSessionUpdateSource.new,
      );

      expect(find.text('Could not load conversations.'), findsOneWidget);
      await tester.pump(const Duration(seconds: 31));

      expect(find.text('Could not load conversations.'), findsOneWidget);
      expect(find.text('No conversations yet.'), findsNothing);
    });

    testWidgets('a synchronous source error reconnects and resumes updates', (
      tester,
    ) async {
      var calls = 0;
      final replacement = _FakeSessionUpdateSource();
      await _pumpShell(
        tester,
        api: _FakeApi(),
        size: _desktop,
        sessionUpdateSourceFactory: () {
          calls++;
          if (calls == 1) throw StateError('setup failed');
          return replacement;
        },
      );
      await tester.pump(const Duration(seconds: 1));
      expect(calls, 2);

      replacement.add(
        TuringEvent(
          eventId: 'evt_after_setup_retry',
          sessionId: 'sess_after_setup_retry',
          traceId: 'trace_after_setup_retry',
          sequence: 1,
          type: 'session.updated',
          createdAt: DateTime.utc(2026, 8, 18, 20),
          payload: const {
            'title': 'After setup retry',
            'updatedAt': '2026-08-18T20:00:00.000000000Z',
          },
        ),
      );
      await tester.pumpAndSettle();

      expect(find.text('After setup retry'), findsOneWidget);
    });

    testWidgets('a later stale page does not resurrect a deleted session', (
      tester,
    ) async {
      final deleted = Session(
        sessionId: 'sess_deleted',
        title: 'Delete me',
        updatedAt: DateTime.utc(2026, 5, 10),
      );
      final api = _FakeApi()
        ..sessions = [deleted]
        ..removeDeletedSessionFromList = true;
      await _pumpShell(tester, api: api, size: _desktop);

      await tester.tap(find.text('Delete me'));
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('Delete chat'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(TextButton, 'Delete'));
      await tester.pumpAndSettle();
      expect(find.text('Delete me'), findsNothing);

      api
        ..sessions = [deleted]
        ..addCreatedSessionToList = true;
      await tester.tap(find.text('New chat').first);
      await tester.pumpAndSettle();

      expect(
        find.text('Delete me'),
        findsNothing,
        reason: 'page omission does not expire a local deletion tombstone',
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
      expect(find.byType(IntegrationsPage), findsOneWidget);
    });

    testWidgets('Automations is reachable and usable through the drawer', (
      tester,
    ) async {
      final api = _FakeApi()
        ..automations.add(
          Automation(
            automationId: 'auto_1',
            name: 'Morning digest',
            prompt: 'Summarise the sandbox.',
            schedule: const AutomationSchedule.interval(60),
            enabled: true,
            allowedTools: const [],
            nextRunAt: DateTime(2026, 8, 18, 8, 30),
          ),
        );
      await _pumpShell(tester, api: api, size: _phone);

      await tester.tap(find.byTooltip('Open navigation menu'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Automations'));
      await tester.pumpAndSettle();

      expect(find.byType(Drawer), findsNothing);
      expect(find.byType(AutomationsPage), findsOneWidget);
      expect(find.text('Morning digest'), findsOneWidget);
      expect(
        tester.takeException(),
        isNull,
        reason: 'the page has to fit a phone',
      );
    });

    testWidgets('Telemetry is reachable and usable through the drawer', (
      tester,
    ) async {
      await _pumpShell(tester, api: _FakeApi(), size: _phone);

      await tester.tap(find.byTooltip('Open navigation menu'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Telemetry'));
      await tester.pumpAndSettle();

      expect(find.byType(Drawer), findsNothing);
      expect(find.byType(TelemetryPage), findsOneWidget);
      expect(
        tester.takeException(),
        isNull,
        reason: 'the page has to fit a phone',
      );
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

  group('the destinations stay put on desktop', () {
    testWidgets('scrolling a long conversation list keeps them visible', (
      tester,
    ) async {
      final api = _FakeApi()
        ..sessions = [
          for (var i = 0; i < 60; i++)
            Session(
              sessionId: 'sess_$i',
              title: 'Conversation number $i',
              updatedAt: DateTime.utc(2026, 5, 10),
            ),
        ];
      await _pumpShell(tester, api: api, size: _desktop);
      expect(find.text('Skills'), findsOneWidget);

      // Enough to run the list well past where the nav rows sit.
      await tester.drag(find.byType(ListView).first, const Offset(0, -1200));
      await tester.pumpAndSettle();

      // The destinations are this app's primary navigation on desktop; a long
      // chat list must never take them off screen.
      for (final label in ShellDestination.navigation.map((d) => d.label)) {
        expect(
          find.text(label),
          findsOneWidget,
          reason: '$label scrolled away',
        );
      }
      // And the list really did move, or the assertion above proves nothing.
      expect(find.text('Conversation number 0'), findsNothing);
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
      expect(find.byType(IntegrationsPage), findsOneWidget);

      // With no conversations to tap, this header is the only way back that
      // does not create something.
      await tester.tap(find.text('Chats'));
      await tester.pumpAndSettle();

      expect(find.byType(IntegrationsPage), findsNothing);
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

  // The rule the removed placeholder machinery used to enforce, kept as the
  // assertion that actually matters: nothing reaches the rail before it works.
  // Adding an unbuilt destination fails here, which is the moment to decide
  // whether to finish it or restore the honest placeholder from git history.
  test('every destination is backed by something real', () {
    for (final destination in ShellDestination.values) {
      expect(
        destination.implemented,
        isTrue,
        reason:
            '${destination.label} is in the rail but not implemented — build '
            'it, or give it a view that says what is missing and why',
      );
    }
  });
}

Future<void> _pumpShell(
  WidgetTester tester, {
  required _FakeApi api,
  required Size size,
  TuringEventSource Function()? eventSourceFactory,
  TuringSessionUpdateSource? Function()? sessionUpdateSourceFactory,
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);

  await tester.pumpWidget(
    MaterialApp(
      home: ResponsiveShell(
        apiClient: api,
        eventSourceFactory: eventSourceFactory ?? () => _FakeEventSource(),
        sessionUpdateSourceFactory: sessionUpdateSourceFactory,
        authStorage: _FakeAuthStorage(),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

class _FakeApi
    with
        NoAuditApi,
        NoIntegrationsApi,
        NoAutomationsApi,
        NoTelemetryApi,
        NoSkillsApi
    implements TuringApi {
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
  Object? sessionsError;
  int listToolsCalls = 0;
  int listMessagesCalls = 0;
  final List<String?> createSessionTitles = [];
  final List<String> sentMessages = [];
  Completer<List<Session>>? nextListSessions;
  bool addCreatedSessionToList = false;
  bool removeDeletedSessionFromList = false;
  String createdSessionId = 'sess_new';
  String createdSessionTimestamp = '2026-05-10T00:00:00.000Z';
  String? nextSessionCursor;
  final Map<String, SessionPage> sessionPages = {};
  final List<String?> sessionPageCursors = [];
  final List<String> renamedTitles = [];
  List<Session> archivedSessions = [];
  String? nextArchivedCursor;
  final Map<String, SessionPage> archivedPages = {};
  final List<String?> archivedPageCursors = [];
  final List<String> deletedSessionIds = [];

  int listSessionsCalls = 0;

  @override
  Future<List<Session>> listSessions({int limit = 50, String? after}) async {
    listSessionsCalls++;
    final error = sessionsError;
    if (error != null) throw error;
    final next = nextListSessions;
    if (next != null) {
      nextListSessions = null;
      return next.future;
    }
    return sessions;
  }

  @override
  Future<SessionPage> listSessionPage({
    int limit = 50,
    String? cursor,
    SessionListFilter filter = SessionListFilter.active,
  }) async {
    if (filter == SessionListFilter.archived) {
      archivedPageCursors.add(cursor);
      if (cursor != null) {
        return archivedPages[cursor] ?? const SessionPage(sessions: []);
      }
      return SessionPage(
        sessions: archivedSessions,
        nextCursor: nextArchivedCursor,
      );
    }
    sessionPageCursors.add(cursor);
    if (cursor != null) {
      return sessionPages[cursor] ?? const SessionPage(sessions: []);
    }
    return SessionPage(
      sessions: await listSessions(limit: limit),
      nextCursor: nextSessionCursor,
    );
  }

  @override
  Future<List<ToolDescriptor>> listTools() async {
    listToolsCalls++;
    final error = toolsError;
    if (error != null) throw error;
    return tools;
  }

  /// A working in-memory automation library, so the Automations UI is tested
  /// against something that behaves like the backend rather than a stub that
  /// always says yes.
  final List<Automation> automations = [];
  Object? automationsError;
  int nextAutomationId = 1;
  int listAutomationsCalls = 0;

  @override
  Future<List<Automation>> listAutomations() async {
    listAutomationsCalls++;
    final error = automationsError;
    if (error != null) throw error;
    return List.unmodifiable(automations);
  }

  @override
  Future<Automation> createAutomation({
    required String name,
    required String prompt,
    required AutomationSchedule schedule,
    required bool enabled,
    required List<AutomationTool> allowedTools,
  }) async {
    if (automations.any((a) => a.name.toLowerCase() == name.toLowerCase())) {
      throw StateError('an automation with that name already exists');
    }
    final automation = Automation(
      automationId: 'auto_${nextAutomationId++}',
      name: name,
      prompt: prompt,
      schedule: schedule,
      enabled: enabled,
      allowedTools: allowedTools,
    );
    automations.add(automation);
    return automation;
  }

  @override
  Future<Automation> updateAutomation({
    required String automationId,
    required String name,
    required String prompt,
    required AutomationSchedule schedule,
    required List<AutomationTool> allowedTools,
  }) async {
    final index = automations.indexWhere((a) => a.automationId == automationId);
    if (index < 0) throw StateError('automation not found');
    final previous = automations[index];
    final updated = Automation(
      automationId: automationId,
      name: name,
      prompt: prompt,
      schedule: schedule,
      enabled: previous.enabled,
      allowedTools: allowedTools,
      lastRunAt: previous.lastRunAt,
      nextRunAt: previous.nextRunAt,
      sessionId: previous.sessionId,
    );
    automations[index] = updated;
    return updated;
  }

  @override
  Future<Automation> setAutomationEnabled({
    required String automationId,
    required bool enabled,
  }) async {
    final index = automations.indexWhere((a) => a.automationId == automationId);
    if (index < 0) throw StateError('automation not found');
    final previous = automations[index];
    final updated = Automation(
      automationId: automationId,
      name: previous.name,
      prompt: previous.prompt,
      schedule: previous.schedule,
      enabled: enabled,
      allowedTools: previous.allowedTools,
      lastRunAt: previous.lastRunAt,
      // A disabled automation has no next run, which is what the backend does
      // too — the card must not keep showing one.
      nextRunAt: enabled ? previous.nextRunAt : null,
      sessionId: previous.sessionId,
    );
    automations[index] = updated;
    return updated;
  }

  @override
  Future<void> deleteAutomation({required String automationId}) async {
    automations.removeWhere((a) => a.automationId == automationId);
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
    if (addCreatedSessionToList) {
      sessions = [
        Session(
          sessionId: createdSessionId,
          title: title,
          updatedAt: DateTime.parse(createdSessionTimestamp),
        ),
        ...sessions,
      ];
    }
    return {
      'sessionId': createdSessionId,
      'createdAt': createdSessionTimestamp,
    };
  }

  @override
  Future<Map<String, dynamic>> sendMessage({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
    String? idempotencyKey,
  }) async {
    sentMessages.add(content);
    return {'sessionId': sessionId, 'runId': 'run_1', 'status': 'queued'};
  }

  @override
  Future<void> deleteSession({required String sessionId}) async {
    deletedSessionIds.add(sessionId);
    archivedSessions = archivedSessions
        .where((session) => session.sessionId != sessionId)
        .toList();
    if (removeDeletedSessionFromList) {
      sessions = sessions
          .where((session) => session.sessionId != sessionId)
          .toList();
    }
  }

  @override
  Future<Session> renameSession({
    required String sessionId,
    required String title,
  }) async {
    renamedTitles.add(title);
    final renamed = Session(
      sessionId: sessionId,
      title: 'Server normalized title',
      updatedAt: DateTime.utc(2026, 5, 11),
    );
    sessions = [
      renamed,
      ...sessions.where((session) => session.sessionId != sessionId),
    ];
    return renamed;
  }

  @override
  Future<Session> archiveSession({required String sessionId}) async {
    final existing = sessions.firstWhere(
      (session) => session.sessionId == sessionId,
    );
    sessions = sessions
        .where((session) => session.sessionId != sessionId)
        .toList();
    return Session(
      sessionId: sessionId,
      title: existing.title,
      updatedAt: DateTime.utc(2026, 5, 12),
      status: SessionStatus.archived,
    );
  }

  @override
  Future<Session> restoreSession({required String sessionId}) async {
    final restored = Session(
      sessionId: sessionId,
      title: 'Restored chat',
      updatedAt: DateTime.utc(2026, 5, 13),
    );
    archivedSessions = archivedSessions
        .where((session) => session.sessionId != sessionId)
        .toList();
    sessions = [restored, ...sessions];
    return restored;
  }

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

  /// A working in-memory set of external agents plus per-conversation routing,
  /// so the UI is exercised against something that behaves like the backend
  /// rather than a stub that always says yes.
  final List<ExternalAgent> externalAgents = [];
  final Map<String, String> routes = {};
  Object? externalAgentsError;
  Object? sessionAgentError;
  int listExternalAgentsCalls = 0;
  int nextExternalAgentId = 1;

  @override
  Future<List<ExternalAgent>> listExternalAgents() async {
    listExternalAgentsCalls++;
    final error = externalAgentsError;
    if (error != null) throw error;
    return List.unmodifiable(externalAgents);
  }

  @override
  Future<ExternalAgent> createExternalAgent({
    required String displayName,
    required ExternalAgentProvider provider,
    required String baseUrl,
    required String model,
    required String credentialRef,
  }) async {
    if (externalAgents.any(
      (a) => a.displayName.toLowerCase() == displayName.toLowerCase(),
    )) {
      throw StateError('an agent with that name already exists');
    }
    final agent = ExternalAgent(
      agentId: 'agent_${nextExternalAgentId++}',
      displayName: displayName,
      provider: provider,
      baseUrl: baseUrl,
      model: model,
      credentialRef: credentialRef,
      credentialAvailable: true,
    );
    externalAgents.add(agent);
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
    final index = externalAgents.indexWhere((a) => a.agentId == agentId);
    if (index < 0) throw StateError('agent not found');
    final updated = ExternalAgent(
      agentId: agentId,
      displayName: displayName,
      provider: provider,
      baseUrl: baseUrl,
      model: model,
      credentialRef: credentialRef,
      credentialAvailable: true,
    );
    externalAgents[index] = updated;
    return updated;
  }

  @override
  Future<void> deleteExternalAgent({required String agentId}) async {
    externalAgents.removeWhere((a) => a.agentId == agentId);
    routes.removeWhere((_, id) => id == agentId);
  }

  @override
  Future<ExternalAgent?> getSessionAgent({required String sessionId}) async {
    final error = sessionAgentError;
    if (error != null) throw error;
    return _routedAgent(sessionId);
  }

  @override
  Future<ExternalAgent?> setSessionAgent({
    required String sessionId,
    required String agentId,
  }) async {
    routes[sessionId] = agentId;
    return _routedAgent(sessionId);
  }

  @override
  Future<ExternalAgent?> clearSessionAgent({required String sessionId}) async {
    routes.remove(sessionId);
    return null;
  }

  ExternalAgent? _routedAgent(String sessionId) {
    final agentId = routes[sessionId];
    if (agentId == null) return null;
    for (final agent in externalAgents) {
      if (agent.agentId == agentId) return agent;
    }
    return null;
  }
}

class _FakeEventSource implements TuringEventSource {
  /// Counts constructions across the test, so a test can prove the shell did
  /// NOT tear down and rebuild a live subscription.
  static int created = 0;

  _FakeEventSource() {
    created++;
  }

  final _events = StreamController<TuringEvent>();

  void add(TuringEvent event) => _events.add(event);

  @override
  Stream<TuringEvent> connect({required String sessionId, int? lastSequence}) {
    return _events.stream;
  }

  @override
  void close() {
    unawaited(_events.close());
  }
}

class _FakeSessionUpdateSource implements TuringSessionUpdateSource {
  final _events = StreamController<TuringEvent>();

  void add(TuringEvent event) => _events.add(event);

  void addError(Object error) => _events.addError(error);

  @override
  Stream<TuringEvent> connectSessionUpdates() => _events.stream;

  @override
  void close() {
    unawaited(_events.close());
  }
}

class _UncancellableSessionUpdateSource implements TuringSessionUpdateSource {
  _UncancellableSessionUpdateSource(this._events);

  final Stream<TuringEvent> _events;

  @override
  Stream<TuringEvent> connectSessionUpdates() =>
      _UncancellableSessionUpdateStream(_events);

  @override
  void close() {}
}

class _UncancellableSessionUpdateStream extends Stream<TuringEvent> {
  _UncancellableSessionUpdateStream(this._source);

  final Stream<TuringEvent> _source;

  @override
  StreamSubscription<TuringEvent> listen(
    void Function(TuringEvent event)? onData, {
    Function? onError,
    void Function()? onDone,
    bool? cancelOnError,
  }) {
    return _UncancellableSessionUpdateSubscription(
      _source.listen(
        onData,
        onError: onError,
        onDone: onDone,
        cancelOnError: cancelOnError,
      ),
    );
  }
}

class _UncancellableSessionUpdateSubscription
    implements StreamSubscription<TuringEvent> {
  _UncancellableSessionUpdateSubscription(this._inner);

  final StreamSubscription<TuringEvent> _inner;

  @override
  Future<void> cancel() async {}

  @override
  Future<E> asFuture<E>([E? futureValue]) => _inner.asFuture(futureValue);

  @override
  bool get isPaused => _inner.isPaused;

  @override
  void onData(void Function(TuringEvent event)? handleData) =>
      _inner.onData(handleData);

  @override
  void onDone(void Function()? handleDone) => _inner.onDone(handleDone);

  @override
  void onError(Function? handleError) => _inner.onError(handleError);

  @override
  void pause([Future<void>? resumeSignal]) => _inner.pause(resumeSignal);

  @override
  void resume() => _inner.resume();
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
