import 'dart:async';

import 'package:material_ui/material_ui.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/workspace/automations_page.dart';
import 'package:turing_flutter_app/models/automation.dart';
import 'package:turing_flutter_app/models/skill.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';
import 'package:turing_flutter_app/networking/api_client.dart';

import '../support/no_mcp_registry_api.dart';
import '../support/no_skills_api.dart';
import '../support/no_external_agents_api.dart';
import '../support/no_integrations_api.dart';
import '../support/no_remote_egress_api.dart';
import '../support/no_session_lifecycle_api.dart';
import '../support/no_telemetry_api.dart';

void main() {
  group('what a schedule says', () {
    test('an interval reads as a period, in the right plural', () {
      expect(
        describeSchedule(const AutomationSchedule.interval(1)),
        'Every minute',
      );
      expect(
        describeSchedule(const AutomationSchedule.interval(5)),
        'Every 5 minutes',
      );
      expect(
        describeSchedule(const AutomationSchedule.interval(60)),
        'Every hour',
      );
      expect(
        describeSchedule(const AutomationSchedule.interval(180)),
        'Every 3 hours',
      );
      expect(
        describeSchedule(const AutomationSchedule.interval(1440)),
        'Every day',
      );
      expect(
        describeSchedule(const AutomationSchedule.interval(10080)),
        'Every 7 days',
      );
      // Not "Every 1.5 hours", and not a bare minute count either.
      expect(
        describeSchedule(const AutomationSchedule.interval(90)),
        'Every 1h 30m',
      );
    });

    // Storage is a fixed minute of the UTC day; the user reads their own
    // clock. The two conversions have to be each other's inverse or a saved
    // time drifts every time the editor is reopened.
    test('a daily time survives a round trip through the local clock', () {
      // Ordinary days, and both daylight-saving transition days in the
      // northern hemisphere — the dates where a conversion pair anchored on
      // two different midnights disagrees by an hour, silently shifting a
      // saved schedule every time the editor is opened and saved.
      final references = [
        DateTime.utc(2026, 8, 18, 12),
        DateTime.utc(2026, 1, 15, 12),
        DateTime.utc(2026, 3, 8, 12),
        DateTime.utc(2026, 11, 1, 12),
      ];
      for (final reference in references) {
        for (final minute in [0, 1, 7 * 60 + 30, 12 * 60, 23 * 60 + 59]) {
          final local = localTimeFromUtcMinute(minute, reference: reference);
          expect(
            utcMinuteFromLocalTime(local, reference: reference),
            minute,
            reason: 'minute $minute did not survive $reference',
          );
        }
      }
    });

    // The backend sends nothing rather than a guess when it cannot read a
    // stored schedule. "Every 0 minutes" would read as a real schedule.
    test('a schedule the backend could not read says so', () {
      expect(
        describeSchedule(const AutomationSchedule.interval(0)),
        'Schedule cannot be read',
      );
    });

    test('a daily time is rendered as a two-digit local clock', () {
      final reference = DateTime.utc(2026, 8, 18, 12);
      final rendered = formatLocalDaily(7 * 60 + 5, reference: reference);
      expect(rendered, matches(RegExp(r'^\d{2}:\d{2}$')));
    });
  });

  group('the sentence that is the consent', () {
    test('an empty allowlist says the run stops, not that nothing happens', () {
      final sentence = describeAllowlist(const []);
      expect(sentence, contains('may not use any tool'));
      expect(sentence, contains('the run stops'));
    });

    test('a populated allowlist names every tool and closes the door', () {
      final sentence = describeAllowlist(const [
        AutomationTool(serverName: 'files', toolName: 'files.create'),
        AutomationTool(serverName: 'files', toolName: 'files.update'),
      ]);
      expect(sentence, contains('files.create'));
      expect(sentence, contains('files.update'));
      // Without this the sentence reads as a floor rather than a ceiling.
      expect(sentence, contains('Nothing else'));
      // The two exceptions to "stops the run" are named, so the sentence
      // does not claim more than the server does.
      expect(sentence, contains('memory and team tools are refused'));
      expect(sentence, contains('Skill loaders you have marked safe'));
    });

    // What is enforced is the (server, tool) pair, so a name that does not
    // carry its server has to have it spelled out — otherwise the sentence
    // describes something narrower than what was granted.
    test('a tool whose name does not carry its server names it anyway', () {
      expect(
        describeTool(
          const AutomationTool(serverName: 'files', toolName: 'files.update'),
        ),
        'files.update',
      );
      expect(
        describeTool(
          const AutomationTool(serverName: 'notes', toolName: 'files.update'),
        ),
        'files.update (from notes)',
      );
    });
  });

  group('the list', () {
    testWidgets('says what an automation is for when there are none', (
      tester,
    ) async {
      await _pumpAutomations(tester, _FakeApi());

      expect(find.text('No automations yet'), findsOneWidget);
      expect(find.text('New automation'), findsOneWidget);
    });

    testWidgets('shows when each last ran and when it runs next', (
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
            lastRunAt: DateTime(2026, 8, 18, 7, 30),
            nextRunAt: DateTime(2026, 8, 18, 8, 30),
          ),
        );
      await _pumpAutomations(tester, api);

      expect(find.text('Every hour'), findsOneWidget);
      expect(find.text('Last run 2026-08-18 07:30'), findsOneWidget);
      expect(find.text('Next 2026-08-18 08:30'), findsOneWidget);
    });

    testWidgets('an automation that has never run says so rather than lying', (
      tester,
    ) async {
      final api = _FakeApi()..automations.add(_automation());
      await _pumpAutomations(tester, api);

      expect(find.text('Last run never'), findsOneWidget);
    });

    // "Next: never" reads as broken. Paused is a state, not a failure.
    testWidgets('a disabled automation says it is paused, not never', (
      tester,
    ) async {
      final api = _FakeApi()..automations.add(_automation(enabled: false));
      await _pumpAutomations(tester, api);

      expect(find.text('Paused, no next run'), findsOneWidget);
      expect(find.textContaining('Next '), findsNothing);
    });

    testWidgets('the card states what the automation may run unattended', (
      tester,
    ) async {
      final api = _FakeApi()
        ..automations.add(
          _automation(
            allowedTools: const [
              AutomationTool(serverName: 'files', toolName: 'files.create'),
            ],
          ),
        );
      await _pumpAutomations(tester, api);

      expect(
        find.textContaining('Without asking you, this automation may run'),
        findsOneWidget,
      );
      expect(find.textContaining('files.create'), findsOneWidget);
    });

    // The whole point of the last-run status: an operator asks "what happened
    // while I was asleep" and the answer is on the card.
    testWidgets('a failed last run says so without projecting a raw reason', (
      tester,
    ) async {
      final api = _FakeApi()
        ..automations.add(
          _automation(
            lastRunStatus: 'failed',
            lastRunError:
                '"Morning digest" needs approval to run files.update, and '
                'that tool is not on this automation\'s allowlist.',
          ),
        );
      await _pumpAutomations(tester, api);

      expect(find.text('The last run failed.'), findsOneWidget);
      expect(find.textContaining('files.update'), findsNothing);
    });

    testWidgets('a failed last run still says so without a raw reason', (
      tester,
    ) async {
      final api = _FakeApi()
        ..automations.add(_automation(lastRunStatus: 'failed'));
      await _pumpAutomations(tester, api);

      expect(find.text('The last run failed.'), findsOneWidget);
    });

    testWidgets('an unknown scheduled failure code renders neutral copy', (
      tester,
    ) async {
      final api = _FakeApi()
        ..automations.add(
          _automation(
            lastOccurrenceFailureCode: 'some_future_secret_code',
            lastOccurrenceFailedAt: DateTime(2026, 8, 18, 7, 45),
          ),
        );
      await _pumpAutomations(tester, api);

      expect(
        find.textContaining('This scheduled attempt was blocked.'),
        findsOneWidget,
      );
      expect(find.textContaining('some_future_secret_code'), findsNothing);
      expect(find.textContaining('some future secret code'), findsNothing);
    });

    for (final (code, copy) in [
      (
        'remote_egress_requires_interactive_consent',
        'Remote runs require interactive consent.',
      ),
      (
        'remote_egress_configuration_invalid',
        'The remote destination configuration is invalid.',
      ),
    ]) {
      testWidgets('$code renders its specific safe copy', (tester) async {
        final api = _FakeApi()
          ..automations.add(
            _automation(
              lastOccurrenceFailureCode: code,
              lastOccurrenceFailedAt: DateTime(2026, 8, 18, 7, 45),
            ),
          );
        await _pumpAutomations(tester, api);

        expect(find.textContaining(copy), findsOneWidget);
        expect(
          find.textContaining('This scheduled attempt was blocked.'),
          findsNothing,
        );
      });
    }

    testWidgets('the conversation it produced is one tap away', (tester) async {
      final api = _FakeApi()..automations.add(_automation(sessionId: 'sess_7'));
      final opened = <String>[];
      await _pumpAutomations(tester, api, onOpenSession: opened.add);

      await tester.tap(find.text('Open conversation'));
      await tester.pumpAndSettle();

      expect(opened, ['sess_7']);
    });

    testWidgets('an automation that has never run offers no conversation', (
      tester,
    ) async {
      final api = _FakeApi()..automations.add(_automation());
      await _pumpAutomations(tester, api);

      expect(find.text('Open conversation'), findsNothing);
    });

    testWidgets('a backend failure offers a retry instead of an empty list', (
      tester,
    ) async {
      final api = _FakeApi()..automationsError = StateError('backend down');
      await _pumpAutomations(tester, api);

      expect(find.text('Could not reach the backend'), findsOneWidget);
      // Not "No automations yet": that is a confident statement about state we
      // just failed to read.
      expect(find.text('No automations yet'), findsNothing);

      api.automationsError = null;
      api.automations.add(_automation());
      await tester.tap(find.text('Try again'));
      await tester.pumpAndSettle();
      expect(find.text('Morning digest'), findsOneWidget);
    });
  });

  group('turning one off', () {
    testWidgets('the switch reaches the backend and the card follows', (
      tester,
    ) async {
      final api = _FakeApi()..automations.add(_automation());
      await _pumpAutomations(tester, api);
      expect(find.text('Paused, no next run'), findsNothing);

      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();

      expect(api.enabledCalls, [('auto_1', false)]);
      expect(find.text('Paused, no next run'), findsOneWidget);
    });

    // A switch that moves without the backend agreeing shows a state nobody
    // holds.
    testWidgets('a failed toggle snaps back and says why', (tester) async {
      final api = _FakeApi()
        ..automations.add(_automation())
        ..toggleError = StateError('backend down');
      await _pumpAutomations(tester, api);

      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();

      expect(tester.widget<Switch>(find.byType(Switch)).value, isTrue);
      expect(find.textContaining('Could not change it'), findsOneWidget);
    });
  });

  // The one switch that stops every automation at once. It only ever shows a
  // setting the backend answered with, never the one a tap asked for.
  group('the quiet switch', () {
    final quietSwitch = find.byKey(const Key('unattended-quiet-switch'));
    final offCopy = find.textContaining('Turn on to stop unattended work');
    final onCopy = find.textContaining(
      'Turning quiet off starts nothing by itself',
    );
    final unavailableCopy = find.textContaining(
      "The saved setting can't be read, so nothing unattended starts",
    );

    testWidgets('off says what turning it on stops, and a tap turns it on', (
      tester,
    ) async {
      final api = _FakeApi()
        ..quietSetting = const UnattendedQuiet(quiet: false, revision: 0);
      await _pumpAutomations(tester, api);

      expect(find.text('Quiet unattended work'), findsOneWidget);
      expect(tester.widget<Switch>(quietSwitch).value, isFalse);
      expect(offCopy, findsOneWidget);
      // A run nobody stops is not one the copy may claim to stop.
      expect(
        find.textContaining('A run already going finishes'),
        findsOneWidget,
      );
      expect(onCopy, findsNothing);

      await tester.tap(quietSwitch);
      await tester.pumpAndSettle();

      expect(api.quietCalls, [(true, 0)]);
      expect(tester.widget<Switch>(quietSwitch).value, isTrue);
      expect(onCopy, findsOneWidget);
      expect(offCopy, findsNothing);
    });

    testWidgets('turning it off names the revision it was read at', (
      tester,
    ) async {
      final api = _FakeApi()
        ..quietSetting = const UnattendedQuiet(quiet: true, revision: 3);
      await _pumpAutomations(tester, api);

      expect(tester.widget<Switch>(quietSwitch).value, isTrue);
      expect(onCopy, findsOneWidget);

      await tester.tap(quietSwitch);
      await tester.pumpAndSettle();

      expect(api.quietCalls, [(false, 3)]);
      expect(tester.widget<Switch>(quietSwitch).value, isFalse);
      expect(offCopy, findsOneWidget);
    });

    // Someone else wrote it between this screen's read and this tap. Showing
    // what the tap asked for would claim quiet is off while it is on.
    testWidgets('a stale write shows the current setting and says so', (
      tester,
    ) async {
      final api = _FakeApi()
        ..quietSetting = const UnattendedQuiet(quiet: true, revision: 1)
        ..quietChangedElsewhere = const UnattendedQuiet(
          quiet: true,
          revision: 2,
        );
      await _pumpAutomations(tester, api);

      await tester.tap(quietSwitch);
      await tester.pumpAndSettle();

      expect(api.quietCalls, [(false, 1)], reason: 'it must not retry alone');
      expect(tester.widget<Switch>(quietSwitch).value, isTrue);
      expect(
        find.text(
          'Quiet was changed somewhere else, so nothing changed here. This '
          'is its current setting.',
        ),
        findsOneWidget,
      );

      // The setting shown is the one the next write is decided against.
      await tester.tap(quietSwitch);
      await tester.pumpAndSettle();

      expect(api.quietCalls, [(false, 1), (false, 2)]);
      expect(tester.widget<Switch>(quietSwitch).value, isFalse);
    });

    // A setting nobody can read counts as on. A switch that could be flipped
    // would have to pretend to know which way it currently points.
    testWidgets('an unreadable setting shows on and is repaired to off', (
      tester,
    ) async {
      final api = _FakeApi()
        ..quietSetting = const UnattendedQuiet(
          quiet: true,
          revision: 4,
          unavailable: true,
        );
      await _pumpAutomations(tester, api);

      expect(unavailableCopy, findsOneWidget);
      final shown = tester.widget<Switch>(quietSwitch);
      expect(shown.value, isTrue);
      expect(shown.onChanged, isNull);

      await tester.tap(find.text('Turn quiet off'));
      await tester.pumpAndSettle();

      expect(api.quietCalls, [(false, 4)]);
      expect(unavailableCopy, findsNothing);
      expect(find.text('Turn quiet off'), findsNothing);
      final repaired = tester.widget<Switch>(quietSwitch);
      expect(repaired.value, isFalse);
      expect(repaired.onChanged, isNotNull);
      expect(offCopy, findsOneWidget);
    });

    testWidgets('an unreadable setting can be repaired to on as well', (
      tester,
    ) async {
      final api = _FakeApi()
        ..quietSetting = const UnattendedQuiet(
          quiet: true,
          revision: 4,
          unavailable: true,
        );
      await _pumpAutomations(tester, api);

      await tester.tap(find.text('Keep quiet on'));
      await tester.pumpAndSettle();

      expect(api.quietCalls, [(true, 4)]);
      expect(unavailableCopy, findsNothing);
      final repaired = tester.widget<Switch>(quietSwitch);
      expect(repaired.value, isTrue);
      expect(repaired.onChanged, isNotNull);
      expect(onCopy, findsOneWidget);
    });

    testWidgets('an unreadable setting fits a phone', (tester) async {
      final api = _FakeApi()
        ..quietSetting = const UnattendedQuiet(
          quiet: true,
          revision: 4,
          unavailable: true,
        );
      await _pumpAutomations(tester, api, size: const Size(300, 400));

      expect(find.text('Keep quiet on'), findsOneWidget);
      expect(find.text('Turn quiet off'), findsOneWidget);
      expect(tester.takeException(), isNull);
    });

    // A switch drawn before the answer would be a guess about whether
    // anything runs tonight.
    testWidgets('draws no switch until the backend has answered', (
      tester,
    ) async {
      final read = Completer<void>();
      final api = _FakeApi()
        ..quietSetting = const UnattendedQuiet(quiet: true, revision: 1)
        ..quietReadGate = read;
      await _pumpAutomations(tester, api, settle: false);

      expect(quietSwitch, findsNothing);

      read.complete();
      await tester.pumpAndSettle();

      expect(tester.widget<Switch>(quietSwitch).value, isTrue);
    });

    testWidgets('a failed read says so, and reading it again shows it', (
      tester,
    ) async {
      final api = _FakeApi()
        ..quietSetting = const UnattendedQuiet(quiet: true, revision: 1)
        ..quietReadError = StateError('backend down');
      await _pumpAutomations(tester, api);

      expect(find.text('Could not read the quiet setting.'), findsOneWidget);
      expect(quietSwitch, findsNothing);

      api.quietReadError = null;
      await tester.tap(find.text('Read it again'));
      await tester.pumpAndSettle();

      expect(find.text('Could not read the quiet setting.'), findsNothing);
      expect(tester.widget<Switch>(quietSwitch).value, isTrue);
    });

    testWidgets('a tap while a write is in flight sends no second write', (
      tester,
    ) async {
      final gate = Completer<void>();
      final api = _FakeApi()
        ..quietSetting = const UnattendedQuiet(quiet: false, revision: 0)
        ..quietWriteGate = gate;
      await _pumpAutomations(tester, api);

      // Held from before the tap, as a second activation landing in the same
      // frame would be: it reaches the switch before it is rebuilt disabled.
      final beforeTap = tester.widget<Switch>(quietSwitch).onChanged!;
      await tester.tap(quietSwitch);
      await tester.pump();

      final inFlight = tester.widget<Switch>(quietSwitch);
      expect(inFlight.onChanged, isNull);
      // Not yet on: the backend has not said so.
      expect(inFlight.value, isFalse);
      await tester.tap(quietSwitch, warnIfMissed: false);
      beforeTap(true);
      await tester.pump();
      expect(api.quietCalls, [(true, 0)]);

      gate.complete();
      await tester.pumpAndSettle();

      expect(api.quietCalls, [(true, 0)]);
      final settled = tester.widget<Switch>(quietSwitch);
      expect(settled.value, isTrue);
      expect(settled.onChanged, isNotNull);
    });

    // The write may have landed before its answer was lost, so neither side
    // of the tap can be trusted; only a fresh read can.
    testWidgets('a failed write says why and shows what a re-read finds', (
      tester,
    ) async {
      final api = _FakeApi()
        ..quietSetting = const UnattendedQuiet(quiet: false, revision: 0);
      await _pumpAutomations(tester, api);

      api
        ..quietSetting = const UnattendedQuiet(quiet: true, revision: 1)
        ..quietWriteError = StateError('connection reset');
      await tester.tap(quietSwitch);
      await tester.pumpAndSettle();

      expect(api.quietCalls, [(true, 0)]);
      expect(find.textContaining('Could not change quiet:'), findsOneWidget);
      final reread = tester.widget<Switch>(quietSwitch);
      expect(reread.value, isTrue);
      expect(reread.onChanged, isNotNull);
    });

    testWidgets('is there even when the automations could not be read', (
      tester,
    ) async {
      final api = _FakeApi()
        ..quietSetting = const UnattendedQuiet(quiet: true, revision: 1)
        ..automationsError = StateError('backend down');
      await _pumpAutomations(tester, api);

      expect(find.text('Could not reach the backend'), findsOneWidget);
      expect(tester.widget<Switch>(quietSwitch).value, isTrue);
      expect(onCopy, findsOneWidget);
    });
  });

  group('deleting one', () {
    testWidgets('says what survives before it removes anything', (
      tester,
    ) async {
      final api = _FakeApi()..automations.add(_automation());
      await _pumpAutomations(tester, api);

      await tester.tap(find.byTooltip('Delete automation'));
      await tester.pumpAndSettle();

      expect(find.text('Delete "Morning digest"?'), findsOneWidget);
      expect(
        find.textContaining('conversation it produced stays'),
        findsOneWidget,
      );
      expect(
        find.textContaining('A run already underway loses its permission'),
        findsOneWidget,
      );

      await tester.tap(find.text('Cancel'));
      await tester.pumpAndSettle();
      expect(api.deleted, isEmpty);

      await tester.tap(find.byTooltip('Delete automation'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Delete'));
      await tester.pumpAndSettle();
      expect(api.deleted, ['auto_1']);
      expect(find.text('Morning digest'), findsNothing);
    });
  });

  group('the editor', () {
    testWidgets('shows the ready skills an unattended run can load', (
      tester,
    ) async {
      final api = _FakeApi()..skills.add(_skill());
      await _pumpAutomations(tester, api);

      await tester.tap(find.text('New automation'));
      await tester.pumpAndSettle();

      expect(find.text('Skills available while it runs'), findsOneWidget);
      expect(find.text('Tone (writing/tone)'), findsOneWidget);
      expect(find.textContaining('does not grant those tools'), findsOneWidget);
    });

    testWidgets('offers every tool an automation may use, safe ones too', (
      tester,
    ) async {
      final api = _FakeApi();
      await _pumpAutomations(tester, api);

      await tester.tap(find.text('New automation'));
      await tester.pumpAndSettle();

      // The allowlist is the whole of what an automation may use, so
      // approval-gated tools are offered...
      expect(find.text('files.create'), findsOneWidget);
      expect(find.text('files.update'), findsOneWidget);
      // ...and so are safe ones: an automation reads with files.read only if
      // it is ticked here.
      expect(find.text('files.read'), findsOneWidget);
      // A disabled tool is not: nothing may run it, whatever is ticked.
      expect(find.text('files.delete'), findsNothing);
      // Nor is a memory, integration or team tool, safe or approval-gated:
      // the server refuses all three to automations unconditionally, so a
      // tick here could only fail at Save.
      expect(find.text('memory.remember'), findsNothing);
      expect(find.text('memory.search'), findsNothing);
      expect(find.text('github.list_issues'), findsNothing);
      expect(find.text('team.delegate'), findsNothing);
      expect(
        find.textContaining(
          'integrations, memory tools and team delegation are not available',
        ),
        findsOneWidget,
      );
    });

    testWidgets('states the whole consent before you can save it', (
      tester,
    ) async {
      final api = _FakeApi();
      await _pumpAutomations(tester, api);
      await tester.tap(find.text('New automation'));
      await tester.pumpAndSettle();

      // With nothing ticked the sentence says the run stops.
      expect(find.textContaining('may not use any tool'), findsWidgets);

      await tester.ensureVisible(find.text('files.create'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('files.create'));
      await tester.pumpAndSettle();
      await tester.ensureVisible(
        find.textContaining('Without asking you, this automation may run'),
      );
      await tester.pumpAndSettle();

      expect(
        find.textContaining('Without asking you, this automation may run'),
        findsWidgets,
      );
      expect(find.textContaining('Nothing else'), findsWidgets);
    });

    testWidgets('saves the name, prompt, schedule and allowlist together', (
      tester,
    ) async {
      final api = _FakeApi();
      await _pumpAutomations(tester, api);
      await tester.tap(find.text('New automation'));
      await tester.pumpAndSettle();

      await tester.enterText(_field(tester, 'Name'), 'Morning digest');
      await tester.enterText(
        _field(tester, 'Prompt'),
        'Summarise the sandbox.',
      );
      await tester.enterText(_field(tester, 'Minutes between runs'), '30');
      // A focused field scrolls itself back into view on every frame, which
      // would undo the scroll down to the picker's last row.
      FocusManager.instance.primaryFocus?.unfocus();
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('files.update'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('files.update'));
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.text('Save'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();

      expect(api.automations.length, 1);
      final saved = api.automations.single;
      expect(saved.name, 'Morning digest');
      expect(saved.prompt, 'Summarise the sandbox.');
      expect(saved.schedule.kind, AutomationScheduleKind.interval);
      expect(saved.schedule.intervalMinutes, 30);
      expect(saved.allowedTools, const [
        AutomationTool(serverName: 'files', toolName: 'files.update'),
      ]);
      // A new automation that has to be switched on afterwards reads as a bug
      // the first time it does not run.
      expect(saved.enabled, isTrue);
      expect(find.text('Morning digest'), findsOneWidget);
    });

    testWidgets('a daily time is saved as the matching minute of the UTC day', (
      tester,
    ) async {
      final api = _FakeApi();
      await _pumpAutomations(tester, api);
      await tester.tap(find.text('New automation'));
      await tester.pumpAndSettle();

      await tester.enterText(_field(tester, 'Name'), 'Morning digest');
      await tester.enterText(_field(tester, 'Prompt'), 'Summarise.');
      await tester.tap(find.text('Daily'));
      await tester.pumpAndSettle();
      // The editor defaults to 09:00 local; the assertion is that whatever is
      // shown is what is stored, converted rather than copied.
      final shown = find.textContaining('your time');
      expect(shown, findsOneWidget);
      final label = tester.widget<Text>(shown).data!;
      final match = RegExp(r'At (\d{2}):(\d{2})').firstMatch(label)!;
      final local = TimeOfDay(
        hour: int.parse(match.group(1)!),
        minute: int.parse(match.group(2)!),
      );

      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();

      final saved = api.automations.single;
      expect(saved.schedule.kind, AutomationScheduleKind.daily);
      expect(saved.schedule.dailyMinuteUtc, utcMinuteFromLocalTime(local));
      expect(
        localTimeFromUtcMinute(saved.schedule.dailyMinuteUtc),
        local,
        reason: 'what was stored must read back as the time that was shown',
      );
    });

    testWidgets('a daily schedule says it is anchored to UTC', (tester) async {
      await _pumpAutomations(tester, _FakeApi());
      await tester.tap(find.text('New automation'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Daily'));
      await tester.pumpAndSettle();

      // Otherwise a user whose clock shifts an hour in October reads it as a
      // bug rather than as what they were told.
      expect(find.textContaining('daylight saving'), findsOneWidget);
    });

    testWidgets('refuses an interval that cannot be kept, next to the field', (
      tester,
    ) async {
      final api = _FakeApi();
      await _pumpAutomations(tester, api);
      await tester.tap(find.text('New automation'));
      await tester.pumpAndSettle();

      await tester.enterText(_field(tester, 'Name'), 'Too fast');
      await tester.enterText(_field(tester, 'Prompt'), 'go');
      await tester.enterText(_field(tester, 'Minutes between runs'), '0');
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();

      expect(find.textContaining('at least 1'), findsWidgets);
      expect(api.automations, isEmpty);
    });

    testWidgets('refuses an automation with no prompt', (tester) async {
      final api = _FakeApi();
      await _pumpAutomations(tester, api);
      await tester.tap(find.text('New automation'));
      await tester.pumpAndSettle();

      await tester.enterText(_field(tester, 'Name'), 'Nameless work');
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();

      expect(
        find.text('An automation needs both a name and a prompt.'),
        findsOneWidget,
      );
      expect(api.automations, isEmpty);
    });

    testWidgets('a rejected save keeps the dialog open and says why', (
      tester,
    ) async {
      final api = _FakeApi()..createError = StateError('name already taken');
      await _pumpAutomations(tester, api);
      await tester.tap(find.text('New automation'));
      await tester.pumpAndSettle();

      await tester.enterText(_field(tester, 'Name'), 'Morning digest');
      await tester.enterText(_field(tester, 'Prompt'), 'go');
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();

      expect(find.textContaining('name already taken'), findsOneWidget);
      expect(find.text('Save'), findsOneWidget);
    });

    // Claiming an empty list would invite someone to save an allowlist they
    // never got to see.
    testWidgets('an unreadable tool list is not reported as no tools', (
      tester,
    ) async {
      final api = _FakeApi()..toolsError = StateError('backend down');
      await _pumpAutomations(tester, api);
      await tester.tap(find.text('New automation'));
      await tester.pumpAndSettle();

      expect(
        find.textContaining('could not be read'),
        findsOneWidget,
        reason: 'a failed read must not read as "no tool to allow"',
      );
      expect(find.textContaining('No tool is available'), findsNothing);
    });

    // A permission you cannot see is a permission you cannot withdraw. The
    // entry is still enforced if the tool comes back, and is re-sent on every
    // save, so hiding it would be the worst of both.
    testWidgets('an allowed tool the backend no longer offers is still shown', (
      tester,
    ) async {
      final api = _FakeApi()
        ..automations.add(
          _automation(
            allowedTools: const [
              AutomationTool(serverName: 'files', toolName: 'files.vanished'),
            ],
          ),
        );
      await _pumpAutomations(tester, api);
      await tester.tap(find.byTooltip('Edit automation'));
      await tester.pumpAndSettle();

      expect(find.text('files.vanished'), findsOneWidget);
      expect(find.textContaining('not offered right now'), findsOneWidget);

      // And it can be untick-ed away. ensureVisible first: a tap that misses
      // lands on the modal barrier and closes the dialog instead.
      await tester.ensureVisible(find.text('files.vanished'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('files.vanished'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
      expect(api.automations.single.allowedTools, isEmpty);
    });

    testWidgets('a stale memory tick says untick, never that it comes back', (
      tester,
    ) async {
      // A legacy allowlist can still carry memory.remember, and while it does
      // every save of the automation is refused wholesale. The row must stay
      // visible — a permission the user cannot see cannot be withdrawn — but
      // its copy has to say what unticking is for, not promise a return the
      // server rules out.
      final api = _FakeApi()
        ..automations.add(
          _automation(
            allowedTools: const [
              AutomationTool(serverName: 'memory', toolName: 'memory.remember'),
            ],
          ),
        );
      await _pumpAutomations(tester, api);
      await tester.tap(find.byTooltip('Edit automation'));
      await tester.pumpAndSettle();

      expect(find.text('memory.remember'), findsOneWidget);
      expect(
        find.textContaining('untick it to save this automation'),
        findsOneWidget,
      );
      expect(find.textContaining('if it comes back'), findsNothing);

      await tester.ensureVisible(find.text('memory.remember'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('memory.remember'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Save'));
      await tester.pumpAndSettle();
      expect(api.automations.single.allowedTools, isEmpty);
    });

    testWidgets('editing carries the existing values in', (tester) async {
      final api = _FakeApi()
        ..automations.add(
          _automation(
            allowedTools: const [
              AutomationTool(serverName: 'files', toolName: 'files.create'),
            ],
          ),
        );
      await _pumpAutomations(tester, api);

      await tester.tap(find.byTooltip('Edit automation'));
      await tester.pumpAndSettle();

      expect(find.text('Edit automation'), findsOneWidget);
      expect(
        tester.widget<TextField>(_field(tester, 'Name')).controller?.text,
        'Morning digest',
      );
      final checkbox = tester.widget<CheckboxListTile>(
        find.ancestor(
          of: find.text('files.create'),
          matching: find.byType(CheckboxListTile),
        ),
      );
      expect(checkbox.value, isTrue);
    });
  });

  // The client has a compact layout below 840 logical px. Anything added has
  // to survive a phone, including the editor dialog.
  group('small screens', () {
    for (final size in const [
      Size(320, 640),
      Size(390, 844),
      Size(568, 320),
      Size(300, 400),
    ]) {
      testWidgets(
        'the list does not overflow at ${size.width}x${size.height}',
        (tester) async {
          final api = _FakeApi()
            ..automations.add(
              _automation(
                sessionId: 'sess_7',
                lastRunStatus: 'failed',
                lastRunError:
                    'a long reason that has to wrap on a narrow screen',
                allowedTools: const [
                  AutomationTool(serverName: 'files', toolName: 'files.create'),
                  AutomationTool(serverName: 'files', toolName: 'files.update'),
                ],
              ),
            );
          await _pumpAutomations(tester, api, size: size);
          expect(tester.takeException(), isNull);
        },
      );

      testWidgets(
        'the editor does not overflow at ${size.width}x${size.height}',
        (tester) async {
          await _pumpAutomations(tester, _FakeApi(), size: size);
          // The button can sit below the fold on a short screen, and a tap
          // that misses would leave this asserting against a dialog that
          // never opened.
          await tester.ensureVisible(find.text('New automation'));
          await tester.pumpAndSettle();
          await tester.tap(find.text('New automation'));
          await tester.pumpAndSettle();

          // Something only the dialog has: "New automation" is also the
          // button's label, so matching that would pass with no dialog.
          expect(find.byType(AlertDialog), findsOneWidget);
          expect(find.text('Prompt'), findsOneWidget);
          expect(tester.takeException(), isNull);
        },
      );
    }
  });
}

Finder _field(WidgetTester tester, String label) {
  return find.ancestor(of: find.text(label), matching: find.byType(TextField));
}

Automation _automation({
  bool enabled = true,
  String sessionId = '',
  String lastRunStatus = '',
  String lastRunError = '',
  String lastOccurrenceFailureCode = '',
  DateTime? lastOccurrenceFailedAt,
  List<AutomationTool> allowedTools = const [],
}) {
  return Automation(
    automationId: 'auto_1',
    name: 'Morning digest',
    prompt: 'Summarise the sandbox.',
    schedule: const AutomationSchedule.interval(60),
    enabled: enabled,
    allowedTools: allowedTools,
    lastRunAt: lastRunStatus.isEmpty ? null : DateTime(2026, 8, 18, 7, 30),
    nextRunAt: enabled ? DateTime(2026, 8, 18, 8, 30) : null,
    sessionId: sessionId,
    lastRunStatus: lastRunStatus,
    lastRunError: lastRunError,
    lastOccurrenceFailureCode: lastOccurrenceFailureCode,
    lastOccurrenceFailedAt: lastOccurrenceFailedAt,
  );
}

Skill _skill() => const Skill(
  skillId: 'writing/tone',
  name: 'Tone',
  description: 'Keeps prose direct',
  body: 'Be brief.',
  category: 'writing',
  version: '',
  author: '',
  license: '',
  requires: [],
  grantedCapabilities: [],
  missingCapabilities: [],
  enabled: true,
  parseError: '',
  folderPath: '/skills/writing/tone',
);

Future<void> _pumpAutomations(
  WidgetTester tester,
  _FakeApi api, {
  Size size = const Size(1000, 900),
  void Function(String sessionId)? onOpenSession,
  bool settle = true,
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);

  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(
        body: AutomationsPage(
          apiClient: api,
          onOpenSession: onOpenSession ?? (_) {},
        ),
      ),
    ),
  );
  // A read held open shows a spinner that never settles.
  if (settle) {
    await tester.pumpAndSettle();
  } else {
    await tester.pump();
  }
}

/// A working in-memory automation library, so the UI is tested against
/// something that behaves like the backend rather than a stub that always says
/// yes.
class _FakeApi
    with
        NoMcpRegistryApi,
        NoSkillsApi,
        NoExternalAgentsApi,
        NoIntegrationsApi,
        NoRemoteEgressApi,
        NoSessionLifecycleApi,
        NoTelemetryApi
    implements TuringApi {
  final List<Automation> automations = [];
  final List<Skill> skills = [];
  final List<String> deleted = [];
  final List<(String, bool)> enabledCalls = [];
  Object? automationsError;
  Object? createError;
  Object? toggleError;
  Object? toolsError;
  int nextId = 1;

  /// What the backend holds for the quiet switch. Null for every test that is
  /// not about it: the read then fails, so its page draws no second Switch.
  UnattendedQuiet? quietSetting;
  Object? quietReadError;
  Object? quietWriteError;

  /// Held open until completed, so a test can look at the screen mid-read.
  Completer<void>? quietReadGate;

  /// Held open until completed, so a test can look at the screen mid-write.
  Completer<void>? quietWriteGate;

  /// Written somewhere else just before the next write is decided, which
  /// leaves that write naming a revision that is no longer current.
  UnattendedQuiet? quietChangedElsewhere;

  /// Every write, as (quiet, revision), whatever came of it.
  final List<(bool, int)> quietCalls = [];

  @override
  Future<UnattendedQuiet> getUnattendedQuiet() async {
    final gate = quietReadGate;
    if (gate != null) await gate.future;
    final error = quietReadError;
    if (error != null) throw error;
    final setting = quietSetting;
    if (setting == null) {
      throw UnimplementedError('this test does not read the quiet switch');
    }
    return setting;
  }

  /// Compare-and-set, as the backend decides it: a write naming a revision
  /// that is not the current one changes nothing and answers stale with the
  /// current setting; the value already held is a no-op; anything else is
  /// written at the next revision, which repairs an unreadable setting too.
  @override
  Future<UnattendedQuietWrite> setUnattendedQuiet({
    required bool quiet,
    required int revision,
  }) async {
    quietCalls.add((quiet, revision));
    final gate = quietWriteGate;
    if (gate != null) await gate.future;
    final error = quietWriteError;
    if (error != null) throw error;
    final elsewhere = quietChangedElsewhere;
    if (elsewhere != null) {
      quietChangedElsewhere = null;
      quietSetting = elsewhere;
    }
    final current = quietSetting!;
    if (revision != current.revision) {
      return UnattendedQuietWrite(setting: current, stale: true);
    }
    if (!current.unavailable && current.quiet == quiet) {
      return UnattendedQuietWrite(setting: current, stale: false);
    }
    final written = UnattendedQuiet(quiet: quiet, revision: revision + 1);
    quietSetting = written;
    return UnattendedQuietWrite(setting: written, stale: false);
  }

  @override
  Future<List<Skill>> listSkills() async => List.unmodifiable(skills);

  @override
  Future<List<Automation>> listAutomations() async {
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
    final error = createError;
    if (error != null) throw error;
    final automation = Automation(
      automationId: 'auto_${nextId++}',
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
    enabledCalls.add((automationId, enabled));
    final error = toggleError;
    if (error != null) throw error;
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
      nextRunAt: enabled ? previous.nextRunAt : null,
      sessionId: previous.sessionId,
      lastRunStatus: previous.lastRunStatus,
      lastRunError: previous.lastRunError,
    );
    automations[index] = updated;
    return updated;
  }

  @override
  Future<void> deleteAutomation({required String automationId}) async {
    deleted.add(automationId);
    automations.removeWhere((a) => a.automationId == automationId);
  }

  @override
  Future<List<ToolDescriptor>> listTools() async {
    final error = toolsError;
    if (error != null) throw error;
    return const [
      ToolDescriptor(
        serverName: 'files',
        toolName: 'files.read',
        policy: ToolPolicy.safe,
      ),
      ToolDescriptor(
        serverName: 'files',
        toolName: 'files.delete',
        policy: ToolPolicy.disabled,
      ),
      ToolDescriptor(
        serverName: 'files',
        toolName: 'files.create',
        policy: ToolPolicy.approvalRequired,
      ),
      ToolDescriptor(
        serverName: 'files',
        toolName: 'files.update',
        policy: ToolPolicy.approvalRequired,
      ),
      // Approval-gated like the two above, but from the pseudo-servers the
      // orchestrator refuses to automations unconditionally — the picker must
      // not offer either.
      ToolDescriptor(
        serverName: 'memory',
        toolName: 'memory.remember',
        policy: ToolPolicy.approvalRequired,
      ),
      // Safe by default, and refused to automations all the same.
      ToolDescriptor(
        serverName: 'memory',
        toolName: 'memory.search',
        policy: ToolPolicy.safe,
      ),
      ToolDescriptor(
        serverName: 'integrations',
        toolName: 'github.list_issues',
        policy: ToolPolicy.approvalRequired,
      ),
      // An unattended run never delegates, so the team tool is refused too,
      // at its default safe policy.
      ToolDescriptor(
        serverName: 'team',
        toolName: 'team.delegate',
        policy: ToolPolicy.safe,
      ),
    ];
  }

  @override
  dynamic noSuchMethod(Invocation invocation) =>
      throw UnimplementedError('${invocation.memberName} is not used here');
}
