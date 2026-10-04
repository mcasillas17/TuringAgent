import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/workspace/agents_page.dart';
import 'package:turing_flutter_app/features/workspace/session_agent_bar.dart';
import 'package:turing_flutter_app/features/workspace/team_section.dart';
import 'package:turing_flutter_app/features/workspace/workspace_pages.dart';
import 'package:turing_flutter_app/models/agent_descriptor.dart';
import 'package:turing_flutter_app/models/agent_profile.dart';
import 'package:turing_flutter_app/models/external_agent.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/models/search_hit.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/session_deletion.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';
import 'package:turing_flutter_app/models/turing_event.dart';
import 'package:turing_flutter_app/networking/api_client.dart';

import '../support/no_skills_api.dart';
import '../support/no_integrations_api.dart';
import '../support/no_session_lifecycle_api.dart';
import '../support/no_automations_api.dart';
import '../support/no_team_api.dart';
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

      await _tapVisible(tester, find.byTooltip('Remove agent'));
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

      await _tapVisible(tester, find.byTooltip('Remove agent'));
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

      await _tapVisible(tester, find.byTooltip('Edit agent'));
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

      await _tapVisible(tester, find.byTooltip('Edit agent'));
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

      testWidgets('a tall name keeps the grant buttons in reach at '
          '${size.width}x${size.height}', (tester) async {
        // Valid: under 120 characters, but sixty lines tall.
        final name = List.filled(60, 'A').join('\n');
        await _pumpAgents(
          tester,
          _TeamAgentApi([_profile('research', name: name)]),
          size: size,
        );

        await _toggle(tester, 'research');

        expect(tester.takeException(), isNull);
        for (final label in ['Cancel', 'Grant and turn on']) {
          final button = tester.getRect(find.text(label));
          expect(
            button.bottom <= size.height && button.top >= 0,
            isTrue,
            reason: '$label is off screen at $button',
          );
        }
      });

      testWidgets(
        'a long emoji leaves the switch in reach at ${size.width}x${size.height}',
        (tester) async {
          // Sixteen characters is the most the loader accepts.
          await _pumpAgents(
            tester,
            _TeamAgentApi([
              _profile(
                'research',
                name: 'Research',
                emoji: '🔬📚🧪🧠🗂📝🔎💡📌🔬📚🧪🧠🗂📝🔎',
              ),
            ]),
            size: size,
          );

          expect(tester.takeException(), isNull);
          final toggle = find.descendant(
            of: _card('research'),
            matching: find.byType(Switch),
          );
          final card = tester.getRect(_card('research'));
          final switchRect = tester.getRect(toggle);
          expect(card.contains(switchRect.center), isTrue);
          expect(_switchOf(tester, 'research').onChanged, isNotNull);
        },
      );

      testWidgets(
        'every team status fits its card at ${size.width}x${size.height}',
        (tester) async {
          await _pumpAgents(
            tester,
            _TeamAgentApi([
              _profile(
                'research',
                name: 'Research',
                emoji: '🔬',
                enabled: true,
                grantedRevision: 'an-older-revision',
              ),
              _profile('broken', name: 'Broken', parseError: 'bad yaml'),
              _profile(
                'inbox',
                name: 'Inbox',
                emoji: '📨',
                granted: true,
                enabled: true,
                unavailable: const ['requires gmail.*'],
              ),
            ]),
            size: size,
          );

          expect(tester.takeException(), isNull);
          expect(
            _chipOf('research', 'Changed since you granted it'),
            findsOneWidget,
          );
          expect(_switchOf(tester, 'research').onChanged, isNotNull);
        },
      );

      testWidgets(
        'the grant review does not overflow at ${size.width}x${size.height}',
        (tester) async {
          await _pumpAgents(
            tester,
            _TeamAgentApi([_profile('research', name: 'Research')]),
            size: size,
          );

          await _toggle(tester, 'research');

          expect(find.byType(AgentProfileGrantSheet), findsOneWidget);
          expect(tester.takeException(), isNull);
        },
      );
    }
  });

  group('Turing\'s team', () {
    testWidgets('lists each specialist with what it is and where it stands', (
      tester,
    ) async {
      final api = _TeamAgentApi([
        _profile('dev', name: 'Dev', emoji: '💻', granted: true, enabled: true),
        _profile('inbox', name: 'Inbox', emoji: '📨'),
        _profile('research', name: 'Research', emoji: '🔬', enabled: true),
      ]);
      await _pumpAgents(tester, api);

      expect(find.text('Turing\'s team'), findsOneWidget);
      expect(find.text('💻'), findsOneWidget);
      expect(find.text('Dev'), findsOneWidget);
      expect(_chipOf('dev', 'Active'), findsOneWidget);
      expect(_chipOf('inbox', 'Off'), findsOneWidget);
      expect(_chipOf('research', 'Needs your grant'), findsOneWidget);
      expect(
        find.descendant(
          of: _card('dev'),
          matching: find.text('Tools: files/files.read'),
        ),
        findsOneWidget,
      );
      // The page must not suggest the team already takes part in chats.
      expect(
        find.textContaining('Delegation is not switched on'),
        findsOneWidget,
      );
      expect(
        find.textContaining('unless you already granted them'),
        findsOneWidget,
        reason: 'turning a granted profile on again does not ask',
      );
    });

    testWidgets('turning one on shows exactly what the grant covers', (
      tester,
    ) async {
      final api = _TeamAgentApi([
        _profile(
          'research',
          name: 'Research',
          emoji: '🔬',
          model: 'qwen2.5:14b',
          resolvedModel: '',
          memory: AgentProfileMemoryAccess.propose,
          tools: const ['files.*', 'memory.*', 'web.*'],
          resolvedTools: const ['files/files.read', 'memory/memory.search'],
          excluded: const [
            AgentProfileToolExclusion(
              tool: 'files/files.write',
              reason: 'changes files',
            ),
          ],
          skills: const ['research/*'],
          maxToolCalls: 12,
          requires: const ['web.search'],
        ),
      ]);
      await _pumpAgents(tester, api);

      await _toggle(tester, 'research');

      expect(find.text('Grant 🔬 Research?'), findsOneWidget);
      final sheet = find.byType(AgentProfileGrantSheet);
      String body() => tester
          .widgetList<SelectableText>(
            find.descendant(of: sheet, matching: find.byType(SelectableText)),
          )
          .map((text) => text.data)
          .join('\n');
      expect(body(), contains('qwen2.5:14b (no worker serves it right now)'));
      // Only memory.search resolves, so the level is shown as a ceiling and
      // the tools it actually gets carry the detail.
      expect(
        body(),
        contains(
          'Up to reading and proposing notes (each proposal still asks you); '
          'what it gets is under Tools it would get now',
        ),
      );
      expect(body(), isNot(contains('Can read your memory')));
      expect(body(), contains('files.*, memory.*, web.*'));
      expect(body(), contains('files/files.read'));
      expect(body(), contains('files/files.write (changes files)'));
      expect(body(), contains('research/*'));
      expect(
        find.descendant(of: sheet, matching: find.text('Tools it requires')),
        findsOneWidget,
      );
      expect(body(), contains('web.search'));
      expect(body(), contains('Asks for up to 12'));
      expect(
        find.descendant(
          of: sheet,
          matching: find.textContaining('revision 0123456789ab of'),
        ),
        findsOneWidget,
        reason: 'the sheet names the revision the grant is bound to',
      );
      expect(api.grants, isEmpty, reason: 'nothing is sent before confirming');
    });

    testWidgets('a granted profile that cannot run yet says why', (
      tester,
    ) async {
      const reason = 'requires gmail.* but no connected tool matches it';
      final api = _TeamAgentApi([
        _profile(
          'inbox',
          name: 'Inbox',
          tools: const ['gmail.*'],
          resolvedTools: const [],
          requires: const ['gmail.*'],
          unavailable: const [reason],
        ),
      ]);
      await _pumpAgents(tester, api);

      await _toggle(tester, 'inbox');
      final sheet = find.byType(AgentProfileGrantSheet);
      expect(
        find.descendant(of: sheet, matching: find.text('Not available yet')),
        findsOneWidget,
        reason: 'the review says before the grant that it will not run',
      );
      expect(
        find.descendant(of: sheet, matching: find.text(reason)),
        findsOneWidget,
      );
      await tester.tap(find.widgetWithText(FilledButton, 'Grant and turn on'));
      await tester.pumpAndSettle();

      expect(api.grants, [('inbox', _revision)]);
      expect(_chipOf('inbox', 'Unavailable'), findsOneWidget);
      expect(_chipOf('inbox', 'Active'), findsNothing);
      expect(
        find.descendant(of: _card('inbox'), matching: find.text('• $reason')),
        findsOneWidget,
      );
    });

    // Spec 6.3: a declared egressing tool stays listed as withheld on the
    // card, not only in the sheet that is no longer shown once it is granted.
    testWidgets('an active profile still lists the tools it is denied', (
      tester,
    ) async {
      final api = _TeamAgentApi([
        _profile(
          'dev',
          name: 'Dev',
          granted: true,
          enabled: true,
          tools: const ['files.*', 'github.*'],
          resolvedTools: const ['files/files.read'],
          excluded: const [
            AgentProfileToolExclusion(
              tool: 'integrations/github.get_issue',
              reason: 'declared, needs per-delegation consent',
            ),
          ],
        ),
      ]);
      await _pumpAgents(tester, api);

      expect(_chipOf('dev', 'Active'), findsOneWidget);
      expect(
        find.descendant(
          of: _card('dev'),
          matching: find.textContaining(
            'integrations/github.get_issue (declared, needs per-delegation '
            'consent)',
          ),
        ),
        findsOneWidget,
      );
    });

    // The memory level is a ceiling on memory tools the profile lists, not a
    // grant of its own, so a profile listing none is not told it can read.
    testWidgets('a memory level with no memory tool says it gives nothing', (
      tester,
    ) async {
      final api = _TeamAgentApi([
        _profile(
          'inbox',
          name: 'Inbox',
          memory: AgentProfileMemoryAccess.read,
          tools: const ['gmail.*'],
          resolvedTools: const [],
          requires: const ['gmail.*'],
        ),
      ]);
      await _pumpAgents(tester, api);

      await _toggle(tester, 'inbox');
      final sheet = find.byType(AgentProfileGrantSheet);
      expect(
        find.descendant(of: sheet, matching: find.textContaining('Can search')),
        findsNothing,
      );
      expect(
        find.descendant(
          of: sheet,
          matching: find.text(
            'Up to search and read, but none of its tools reach your memory '
            'now',
          ),
        ),
        findsOneWidget,
      );
    });

    testWidgets('confirming grants the reviewed revision and turns it on', (
      tester,
    ) async {
      final api = _TeamAgentApi([
        _profile('research', name: 'Research', emoji: '🔬'),
      ]);
      await _pumpAgents(tester, api);

      await _toggle(tester, 'research');
      await tester.tap(find.text('Grant and turn on'));
      await tester.pumpAndSettle();

      expect(api.grants, [('research', _revision)]);
      expect(api.enables, [('research', true)]);
      expect(_chipOf('research', 'Active'), findsOneWidget);
    });

    testWidgets('cancelling the review changes nothing', (tester) async {
      final api = _TeamAgentApi([_profile('inbox', name: 'Inbox')]);
      await _pumpAgents(tester, api);

      await _toggle(tester, 'inbox');
      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();

      expect(api.grants, isEmpty);
      expect(api.enables, isEmpty);
      expect(_chipOf('inbox', 'Off'), findsOneWidget);
      expect(_switchOf(tester, 'inbox').value, isFalse);
    });

    // The file changed between listing and confirming. The backend refuses the
    // stale revision, so the app must not go on to turn the profile on.
    testWidgets('a grant for an edited file is refused and nothing turns on', (
      tester,
    ) async {
      final api = _TeamAgentApi([_profile('inbox', name: 'Inbox')]);
      await _pumpAgents(tester, api);

      await _toggle(tester, 'inbox');
      api.editFile('inbox', 'fedcba9876543210');
      await tester.tap(find.text('Grant and turn on'));
      await tester.pumpAndSettle();

      expect(api.grants, isEmpty);
      expect(api.enables, isEmpty);
      expect(
        find.textContaining('changed while you were reviewing it'),
        findsOneWidget,
      );
      expect(api.listCalls, 2, reason: 'the new revision is read back');
      expect(_switchOf(tester, 'inbox').value, isFalse);
    });

    testWidgets('a profile already granted turns on without asking again', (
      tester,
    ) async {
      final api = _TeamAgentApi([_profile('dev', name: 'Dev', granted: true)]);
      await _pumpAgents(tester, api);

      await _toggle(tester, 'dev');

      expect(find.byType(AgentProfileGrantSheet), findsNothing);
      expect(api.grants, isEmpty);
      expect(api.enables, [('dev', true)]);
      expect(_chipOf('dev', 'Active'), findsOneWidget);
    });

    testWidgets('turning one off sends that and keeps the grant', (
      tester,
    ) async {
      final api = _TeamAgentApi([
        _profile('dev', name: 'Dev', granted: true, enabled: true),
      ]);
      await _pumpAgents(tester, api);

      await _toggle(tester, 'dev');

      expect(api.enables, [('dev', false)]);
      expect(api.grants, isEmpty);
      expect(_chipOf('dev', 'Off'), findsOneWidget);
    });

    testWidgets('an edited file that was on asks for review, not a toggle', (
      tester,
    ) async {
      final api = _TeamAgentApi([
        _profile(
          'dev',
          name: 'Dev',
          enabled: true,
          grantedRevision: 'aaaaaaaaaaaaaaaa',
        ),
      ]);
      await _pumpAgents(tester, api);

      expect(_chipOf('dev', 'Changed since you granted it'), findsOneWidget);
      await tester.ensureVisible(find.text('Review and grant'));
      await tester.tap(find.text('Review and grant'));
      await tester.pumpAndSettle();
      // Already on, so the action is a grant alone.
      await tester.tap(find.widgetWithText(FilledButton, 'Grant'));
      await tester.pumpAndSettle();

      expect(api.grants, [('dev', _revision)]);
      expect(api.enables, isEmpty);
      expect(_chipOf('dev', 'Active'), findsOneWidget);
    });

    testWidgets('a file that cannot be read says why and cannot be turned on', (
      tester,
    ) async {
      final api = _TeamAgentApi([
        _profile('broken', parseError: 'line 3: tools must be a list'),
        _profile(
          'was-on',
          enabled: true,
          parseError: 'line 1: missing front matter',
        ),
      ]);
      await _pumpAgents(tester, api);

      expect(_chipOf('broken', 'Cannot be read'), findsOneWidget);
      expect(
        find.text(
          'team/broken/AGENT.md could not be read: '
          'line 3: tools must be a list',
        ),
        findsOneWidget,
      );
      expect(_switchOf(tester, 'broken').onChanged, isNull);
      // One that broke while on can still be switched off.
      expect(_switchOf(tester, 'was-on').onChanged, isNotNull);
      await _toggle(tester, 'was-on');
      expect(api.enables, [('was-on', false)]);
    });

    testWidgets('a refused enable after a grant says so and shows the result', (
      tester,
    ) async {
      final api = _TeamAgentApi([_profile('inbox', name: 'Inbox')])
        ..enableError = const TuringApiException(
          code: 'failed_precondition',
          message: 'team/inbox/AGENT.md does not parse',
        );
      await _pumpAgents(tester, api);

      await _toggle(tester, 'inbox');
      await tester.tap(find.text('Grant and turn on'));
      await tester.pumpAndSettle();

      expect(api.grants, [('inbox', _revision)]);
      expect(
        find.text(
          'Inbox cannot be turned on: team/inbox/AGENT.md does not parse',
        ),
        findsOneWidget,
      );
      // The grant landed before the enable failed; the reload shows that.
      expect(api.listCalls, 2);
      expect(_switchOf(tester, 'inbox').value, isFalse);
    });

    testWidgets('a team the backend cannot read leaves the agents in place', (
      tester,
    ) async {
      final api = _TeamAgentApi(const [])
        ..teamError = const TuringApiException(
          code: 'unavailable',
          message: 'team folder unreadable',
        )
        ..agents.add(_claude());
      await _pumpAgents(tester, api);

      expect(find.text('Could not load your team'), findsOneWidget);
      expect(find.text('team folder unreadable'), findsOneWidget);
      expect(find.text('Claude'), findsOneWidget);

      api.teamError = null;
      api.profiles.add(_profile('dev', name: 'Dev'));
      await tester.tap(
        find.descendant(
          of: find.ancestor(
            of: find.text('Could not load your team'),
            matching: find.byType(WorkspaceNotice),
          ),
          matching: find.text('Try again'),
        ),
      );
      await tester.pumpAndSettle();
      expect(find.text('Could not load your team'), findsNothing);
      expect(_card('dev'), findsOneWidget);
    });

    testWidgets('agents the backend cannot list leave the team in place', (
      tester,
    ) async {
      final api = _TeamAgentApi([_profile('dev', name: 'Dev')])
        ..listError = const _Offline();
      await _pumpAgents(tester, api);

      expect(find.text('Could not reach the backend'), findsOneWidget);
      expect(_card('dev'), findsOneWidget);
    });

    // A list read before a change lands after it would put the old state
    // back on screen, so the section runs one request at a time.
    testWidgets('a refresh in flight holds every toggle and review', (
      tester,
    ) async {
      final api = _TeamAgentApi([
        _profile('dev', name: 'Dev', granted: true, enabled: true),
        _profile('research', name: 'Research', enabled: true),
      ]);
      await _pumpAgents(tester, api);

      final held = api.holdList = Completer<void>();
      await _tapVisible(tester, _teamRefresh());

      expect(_switchOf(tester, 'dev').onChanged, isNull);
      expect(_switchOf(tester, 'research').onChanged, isNull);
      expect(_reviewOf(tester, 'research').onPressed, isNull);

      held.complete();
      await tester.pumpAndSettle();
      expect(_switchOf(tester, 'dev').onChanged, isNotNull);
      expect(_reviewOf(tester, 'research').onPressed, isNotNull);
    });

    testWidgets('a change in flight holds the refresh and the other toggles', (
      tester,
    ) async {
      final api = _TeamAgentApi([
        _profile('dev', name: 'Dev', granted: true, enabled: true),
        _profile('research', name: 'Research', granted: true, enabled: true),
      ]);
      await _pumpAgents(tester, api);

      final held = api.holdEnable = Completer<void>();
      await _toggle(tester, 'dev');

      expect(tester.widget<IconButton>(_teamRefresh()).onPressed, isNull);
      expect(_switchOf(tester, 'research').onChanged, isNull);

      held.complete();
      await tester.pumpAndSettle();
      expect(api.enables, [('dev', false)]);
      expect(_chipOf('dev', 'Off'), findsOneWidget);
      expect(tester.widget<IconButton>(_teamRefresh()).onPressed, isNotNull);
      expect(_switchOf(tester, 'research').onChanged, isNotNull);
    });

    // The team loads on its own, so reloading the external agents must
    // neither read it again nor throw away a change that is still in flight.
    testWidgets('changing the external agents leaves the team alone', (
      tester,
    ) async {
      final api = _TeamAgentApi([
        _profile('dev', name: 'Dev', granted: true, enabled: true),
      ])..agents.add(_claude());
      await _pumpAgents(tester, api);
      expect(api.listCalls, 1);

      final held = api.holdEnable = Completer<void>();
      await _toggle(tester, 'dev');
      await _tapVisible(tester, find.byTooltip('Remove agent'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Remove'));
      await tester.pumpAndSettle();
      expect(api.deleted, ['agent_1']);

      held.complete();
      await tester.pumpAndSettle();
      expect(api.listCalls, 1, reason: 'the team was read again');
      expect(_chipOf('dev', 'Off'), findsOneWidget);
    });

    testWidgets('a profile deleted after the list says so and drops out', (
      tester,
    ) async {
      final api = _TeamAgentApi([
        _profile('dev', name: 'Dev', granted: true, enabled: true),
        _profile('research', name: 'Research'),
      ]);
      await _pumpAgents(tester, api);
      api.profiles.removeWhere((p) => p.profileId == 'dev');

      await _toggle(tester, 'dev');

      expect(
        find.text('team/dev/AGENT.md is gone. The list has been refreshed.'),
        findsOneWidget,
      );
      expect(api.listCalls, 2);
      expect(_card('dev'), findsNothing);
      expect(_card('research'), findsOneWidget);
    });

    testWidgets('each switch is named for the specialist it controls', (
      tester,
    ) async {
      final semantics = tester.ensureSemantics();
      final api = _TeamAgentApi([
        _profile('dev', name: 'Dev', granted: true, enabled: true),
        _profile('research', name: 'Research'),
      ]);
      await _pumpAgents(tester, api);

      expect(
        tester.getSemantics(
          find.descendant(of: _card('dev'), matching: find.byType(Switch)),
        ),
        matchesSemantics(
          label: 'Use Dev',
          hasToggledState: true,
          isToggled: true,
          hasEnabledState: true,
          isEnabled: true,
          isFocusable: true,
          hasTapAction: true,
          hasFocusAction: true,
        ),
      );
      expect(
        tester.getSemantics(
          find.descendant(of: _card('research'), matching: find.byType(Switch)),
        ),
        matchesSemantics(
          label: 'Use Research',
          hasToggledState: true,
          isToggled: false,
          hasEnabledState: true,
          isEnabled: true,
          isFocusable: true,
          hasTapAction: true,
          hasFocusAction: true,
        ),
      );
      semantics.dispose();
    });

    testWidgets('an empty team says how to add one', (tester) async {
      await _pumpAgents(tester, _TeamAgentApi(const []));

      expect(find.text('No specialists yet'), findsOneWidget);
      expect(find.textContaining('team/ with an AGENT.md'), findsOneWidget);
      expect(find.textContaining('scripts/team-templates/'), findsOneWidget);
      expect(
        find.textContaining('with the setting and grant it had'),
        findsOneWidget,
      );
    });
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

/// Scrolls the target into view first: the page is a scroll view, and
/// tapping a widget that is off screen silently misses.
Future<void> _tapVisible(WidgetTester tester, Finder target) async {
  await tester.ensureVisible(target);
  await tester.pumpAndSettle();
  await tester.tap(target);
  await tester.pumpAndSettle();
}

Future<void> _openEditor(WidgetTester tester) =>
    _tapVisible(tester, find.text('New agent'));

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
        NoSkillsApi,
        NoIntegrationsApi,
        NoSessionLifecycleApi,
        NoAutomationsApi,
        NoTeamApi,
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
  Future<SessionDeletionReceipt> deleteSession({
    required String sessionId,
  }) async => const SessionDeletionReceipt.completed();

  @override
  Future<List<SessionDeletionReceipt>> listSessionDeletionReceipts() async =>
      const [];

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
  Future<Map<String, dynamic>> denyApproval(
    String approvalId, {
    String? reason,
  }) async => {'approvalId': approvalId, 'status': 'denied'};
}

const _revision = '0123456789abcdef';

AgentProfile _profile(
  String id, {
  String name = '',
  String emoji = '',
  bool enabled = false,
  bool granted = false,
  String? grantedRevision,
  String model = '',
  String resolvedModel = 'qwen2.5:7b',
  AgentProfileMemoryAccess memory = AgentProfileMemoryAccess.none,
  List<String> tools = const ['files.read'],
  List<String> resolvedTools = const ['files/files.read'],
  List<AgentProfileToolExclusion> excluded = const [],
  List<String> skills = const [],
  int maxToolCalls = 0,
  String parseError = '',
  String revision = _revision,
  List<String> requires = const [],
  List<String> unavailable = const [],
}) => _TeamAgentApi.withState(
  AgentProfile(
    profileId: id,
    name: name,
    emoji: emoji,
    description: name.isEmpty ? '' : '$name helps.',
    version: '1',
    model: model,
    resolvedModel: resolvedModel,
    tools: tools,
    skills: skills,
    memory: memory,
    requires: requires,
    maxToolCalls: maxToolCalls,
    revision: revision,
    enabled: enabled,
    grantedRevision: grantedRevision ?? (granted ? revision : ''),
    state: AgentProfileState.unknown,
    resolvedTools: resolvedTools,
    unavailableReasons: unavailable,
    excludedTools: excluded,
    parseError: parseError,
  ),
);

Finder _card(String id) => find.byKey(ValueKey('team-profile-$id'));

Finder _chipOf(String id, String label) =>
    find.descendant(of: _card(id), matching: find.text(label));

Switch _switchOf(WidgetTester tester, String id) => tester.widget<Switch>(
  find.descendant(of: _card(id), matching: find.byType(Switch)),
);

Finder _teamRefresh() => find.ancestor(
  of: find.byTooltip('Read the team folder again'),
  matching: find.byType(IconButton),
);

ButtonStyleButton _reviewOf(WidgetTester tester, String id) =>
    tester.widget<ButtonStyleButton>(
      find.descendant(
        of: _card(id),
        matching: find.byWidgetPredicate((w) => w is ButtonStyleButton),
      ),
    );

Future<void> _toggle(WidgetTester tester, String id) async {
  final toggle = find.descendant(of: _card(id), matching: find.byType(Switch));
  await tester.ensureVisible(toggle);
  await tester.tap(toggle);
  await tester.pumpAndSettle();
}

/// The team half of the in-memory backend. It keeps the backend's rules that
/// the page depends on: a grant names a revision and is refused when the file
/// moved on or does not parse, and the state is recomputed from the file and
/// the user's decisions after every change.
class _TeamAgentApi extends _AgentApi {
  _TeamAgentApi(List<AgentProfile> profiles) : profiles = [...profiles];

  final List<AgentProfile> profiles;
  final List<(String, String)> grants = [];
  final List<(String, bool)> enables = [];
  Object? teamError;
  Object? enableError;
  int listCalls = 0;

  /// While set, the next list or enable waits for it to complete.
  Completer<void>? holdList;
  Completer<void>? holdEnable;

  static AgentProfile withState(
    AgentProfile p, {
    String? revision,
    bool? enabled,
    String? grantedRevision,
  }) {
    final rev = revision ?? p.revision;
    final on = enabled ?? p.enabled;
    final grant = grantedRevision ?? p.grantedRevision;
    final AgentProfileState state;
    if (p.parseError.isNotEmpty) {
      state = AgentProfileState.parseError;
    } else if (!on) {
      state = AgentProfileState.disabled;
    } else if (grant != rev) {
      state = AgentProfileState.needsGrant;
    } else if (p.unavailableReasons.isNotEmpty) {
      state = AgentProfileState.unavailable;
    } else {
      state = AgentProfileState.active;
    }
    return AgentProfile(
      profileId: p.profileId,
      name: p.name,
      emoji: p.emoji,
      description: p.description,
      version: p.version,
      model: p.model,
      resolvedModel: p.resolvedModel,
      tools: p.tools,
      skills: p.skills,
      memory: p.memory,
      requires: p.requires,
      maxToolCalls: p.maxToolCalls,
      revision: rev,
      enabled: on,
      grantedRevision: grant,
      state: state,
      resolvedTools: p.resolvedTools,
      unavailableReasons: p.unavailableReasons,
      excludedTools: p.excludedTools,
      parseError: p.parseError,
    );
  }

  int _indexOf(String id) {
    final index = profiles.indexWhere((p) => p.profileId == id);
    if (index < 0) {
      throw TuringApiException(code: 'not_found', message: '$id not found');
    }
    return index;
  }

  void editFile(String id, String revision) {
    final index = _indexOf(id);
    profiles[index] = withState(profiles[index], revision: revision);
  }

  @override
  Future<List<AgentProfile>> listAgentProfiles() async {
    listCalls++;
    final hold = holdList;
    holdList = null;
    if (hold != null) await hold.future;
    final error = teamError;
    if (error != null) throw error;
    return List.unmodifiable(profiles);
  }

  @override
  Future<AgentProfile> grantAgentProfile({
    required String profileId,
    required String revision,
  }) async {
    final index = _indexOf(profileId);
    final current = profiles[index];
    if (current.parseError.isNotEmpty || current.revision != revision) {
      throw const TuringApiException(
        code: 'failed_precondition',
        message: 'stale revision',
      );
    }
    grants.add((profileId, revision));
    return profiles[index] = withState(current, grantedRevision: revision);
  }

  @override
  Future<AgentProfile> setAgentProfileEnabled({
    required String profileId,
    required bool enabled,
  }) async {
    final hold = holdEnable;
    holdEnable = null;
    if (hold != null) await hold.future;
    final error = enableError;
    if (error != null) throw error;
    final index = _indexOf(profileId);
    final current = profiles[index];
    if (enabled && current.parseError.isNotEmpty) {
      throw const TuringApiException(
        code: 'failed_precondition',
        message: 'does not parse',
      );
    }
    enables.add((profileId, enabled));
    return profiles[index] = withState(current, enabled: enabled);
  }
}
