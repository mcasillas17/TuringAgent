import 'package:material_ui/material_ui.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/delegation_card.dart';
import 'package:turing_flutter_app/features/chat/delegation_transcript_screen.dart';
import 'package:turing_flutter_app/models/delegation.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/networking/api_client.dart';

const _task = Delegation(
  delegationId: 'dlg_1',
  parentRunId: 'run_parent',
  childSessionId: 'sess_child',
  childRunId: 'run_child',
  displayName: 'Research',
  emoji: '🔬',
  task: 'Gather the notes',
  state: 'running',
  summary: '🔬 Research is working',
);

void main() {
  testWidgets('a transcript that failed to load says so and can be retried', (
    tester,
  ) async {
    final api = _TranscriptApi()..fail = true;
    await tester.pumpWidget(
      MaterialApp(
        home: DelegationTranscriptScreen(delegation: _task, apiClient: api),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.textContaining('could not be loaded'), findsOneWidget);

    api
      ..fail = false
      ..messages = [
        Message(
          messageId: 'msg_result',
          runId: 'run_child',
          role: 'assistant',
          content: 'Found three notes.',
          sequence: 2,
          createdAt: DateTime.utc(2026, 10, 1),
        ),
      ];
    await tester.tap(find.byTooltip('Refresh'));
    await tester.pumpAndSettle();

    expect(api.requested, ['sess_child', 'sess_child']);
    expect(find.text('Found three notes.'), findsOneWidget);
    expect(find.textContaining('could not be loaded'), findsNothing);
  });

  testWidgets('a task still working has nothing to show yet', (tester) async {
    await tester.pumpWidget(
      MaterialApp(
        home: DelegationTranscriptScreen(
          delegation: _task,
          apiClient: _TranscriptApi(),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Nothing to show yet.'), findsOneWidget);
  });

  testWidgets('a transcript draws its markdown in the theme text color', (
    tester,
  ) async {
    // flutter_markdown cannot see material_ui's theme, so without a sheet
    // built from it the text comes from the default light theme: dark text on
    // a dark surface.
    final api = _TranscriptApi()
      ..messages = [
        Message(
          messageId: 'msg_result',
          runId: 'run_child',
          role: 'assistant',
          content:
              '# Notes\n\nFound three notes.\n\n#### Detail\n\n'
              '> quoted\n\n- listed\n\n| name |\n|---|\n| cell |\n',
          sequence: 2,
          createdAt: DateTime.utc(2026, 10, 1),
        ),
      ];
    Widget host(Brightness brightness) => MaterialApp(
      theme: ThemeData(brightness: brightness),
      home: DelegationTranscriptScreen(delegation: _task, apiClient: api),
    );

    for (final brightness in [Brightness.dark, Brightness.light]) {
      await tester.pumpWidget(host(brightness));
      await tester.pumpAndSettle();

      final theme = Theme.of(tester.element(find.byType(MarkdownBody)));
      final texts = tester.widgetList<EditableText>(
        find.descendant(
          of: find.byType(MarkdownBody),
          matching: find.byType(EditableText),
        ),
      );
      expect(
        texts.map((text) => text.controller.text),
        containsAll(['Notes', 'Detail', 'quoted', 'listed', 'cell']),
      );
      for (final text in texts) {
        expect(
          text.style.color,
          theme.textTheme.bodyMedium!.color,
          reason: '${text.controller.text} in $brightness',
        );
      }
    }
  });

  testWidgets('a card for a task with no recorded task names only who', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: DelegationCard(
            delegation: Delegation(
              delegationId: 'dlg_1',
              parentRunId: 'run_parent',
              displayName: 'Research',
              emoji: '🔬',
              state: 'queued',
            ),
          ),
        ),
      ),
    );

    expect(find.text('🔬 Research'), findsOneWidget);
    expect(find.textContaining('working on'), findsNothing);
    expect(find.text('queued'), findsOneWidget);
    expect(find.text('Cancel'), findsNothing);
    expect(find.text('View work'), findsNothing);
  });
}

class _TranscriptApi extends Fake implements TuringApi {
  bool fail = false;
  List<Message> messages = const [];
  final requested = <String>[];

  @override
  Future<List<Message>> listMessages({
    required String sessionId,
    int limit = 50,
    String? before,
  }) async {
    requested.add(sessionId);
    if (fail) {
      throw const TuringApiException(code: 'unavailable', message: 'down');
    }
    return messages;
  }
}
