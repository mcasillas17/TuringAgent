import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/run_cancelled_card.dart';
import 'package:turing_flutter_app/features/chat/run_failure_card.dart';
import 'package:turing_flutter_app/features/chat/run_state_card.dart';
import 'package:turing_flutter_app/l10n/generated/app_localizations.dart';
import 'package:turing_flutter_app/models/run_lifecycle.dart';
import 'package:turing_flutter_app/models/run_state.dart';

/// Widget-level coverage for [RunStateCard] and [NoResponseCard] — the
/// adjacent, non-bubble cards the design's rendering matrix calls for
/// whenever a run's canonical state is not a plain completed bubble with
/// displayable content.
void main() {
  final updatedAt = DateTime.utc(2026, 8, 20, 12);

  RunState state({
    RunLifecycle lifecycle = RunLifecycle.queued,
    RunOutcomeReason outcomeReason = RunOutcomeReason.none,
    bool hasDisplayableContent = false,
  }) {
    return RunState(
      runId: 'run_1',
      userMessageId: 'msg_user',
      assistantMessageId: 'msg_assistant',
      lifecycle: lifecycle,
      outcomeReason: outcomeReason,
      stateVersion: 1,
      stateUpdatedAt: updatedAt,
      finishedAt: null,
      hasDisplayableContent: hasDisplayableContent,
    );
  }

  Widget host(Widget child) {
    return MaterialApp(
      localizationsDelegates: AppLocalizations.localizationsDelegates,
      supportedLocales: AppLocalizations.supportedLocales,
      home: Scaffold(body: child),
    );
  }

  testWidgets('queued state shows the localized queued status card', (
    tester,
  ) async {
    await tester.pumpWidget(
      host(RunStateCard(state: state(lifecycle: RunLifecycle.queued))),
    );

    expect(find.text('Queued'), findsOneWidget);
    expect(find.text('The run is waiting to start.'), findsOneWidget);
  });

  testWidgets('running state shows the localized working status card', (
    tester,
  ) async {
    await tester.pumpWidget(
      host(RunStateCard(state: state(lifecycle: RunLifecycle.running))),
    );

    expect(find.text('Working'), findsOneWidget);
  });

  testWidgets(
    'waiting-approval state shows the localized waiting status card',
    (tester) async {
      await tester.pumpWidget(
        host(
          RunStateCard(state: state(lifecycle: RunLifecycle.waitingApproval)),
        ),
      );

      expect(find.text('Waiting for approval'), findsOneWidget);
    },
  );

  testWidgets('recovering state shows the localized recovering status card', (
    tester,
  ) async {
    await tester.pumpWidget(
      host(RunStateCard(state: state(lifecycle: RunLifecycle.recovering))),
    );

    expect(find.text('Recovering'), findsOneWidget);
  });

  testWidgets(
    'completed no content suppresses blank bubble and shows completion card',
    (tester) async {
      await tester.pumpWidget(
        host(
          RunStateCard(
            state: state(
              lifecycle: RunLifecycle.completed,
              hasDisplayableContent: false,
            ),
          ),
        ),
      );

      // A neutral, truthful completion notice — never a blank success card.
      expect(find.text('Completed'), findsOneWidget);
      expect(find.text('No assistant response was recorded.'), findsOneWidget);
    },
  );

  testWidgets(
    'completed unknown outcome without content shows generic '
    'outcome-unavailable copy, not the completed-no-content copy',
    (tester) async {
      await tester.pumpWidget(
        host(
          RunStateCard(
            state: state(
              lifecycle: RunLifecycle.completed,
              outcomeReason: RunOutcomeReason.unknown,
              hasDisplayableContent: false,
            ),
          ),
        ),
      );

      expect(find.text('Outcome unavailable'), findsOneWidget);
      expect(
        find.text('This app cannot identify why the run ended.'),
        findsOneWidget,
      );
      expect(find.text('Completed'), findsNothing);
      expect(find.text('No assistant response was recorded.'), findsNothing);
    },
  );

  testWidgets(
    'completed legacyUnknown outcome without content shows generic '
    'outcome-unavailable copy, not the completed-no-content copy',
    (tester) async {
      await tester.pumpWidget(
        host(
          RunStateCard(
            state: state(
              lifecycle: RunLifecycle.completed,
              outcomeReason: RunOutcomeReason.legacyUnknown,
              hasDisplayableContent: false,
            ),
          ),
        ),
      );

      expect(find.text('Outcome unavailable'), findsOneWidget);
      expect(
        find.text('This app cannot identify why the run ended.'),
        findsOneWidget,
      );
      expect(find.text('Completed'), findsNothing);
      expect(find.text('No assistant response was recorded.'), findsNothing);
    },
  );

  testWidgets('failed lifecycle delegates to RunFailureCard', (tester) async {
    await tester.pumpWidget(
      host(
        RunStateCard(
          state: state(
            lifecycle: RunLifecycle.failed,
            outcomeReason: RunOutcomeReason.toolFailure,
          ),
        ),
      ),
    );

    expect(find.byType(RunFailureCard), findsOneWidget);
    expect(find.byType(RunCancelledCard), findsNothing);
    expect(find.text('Tool failed'), findsOneWidget);
  });

  testWidgets('cancelled lifecycle delegates to RunCancelledCard', (
    tester,
  ) async {
    await tester.pumpWidget(
      host(
        RunStateCard(
          state: state(
            lifecycle: RunLifecycle.cancelled,
            outcomeReason: RunOutcomeReason.abandoned,
          ),
        ),
      ),
    );

    expect(find.byType(RunCancelledCard), findsOneWidget);
    expect(find.byType(RunFailureCard), findsNothing);
  });

  testWidgets('abandoned run uses localized abandonment card', (tester) async {
    await tester.pumpWidget(
      host(
        RunStateCard(
          state: state(
            lifecycle: RunLifecycle.cancelled,
            outcomeReason: RunOutcomeReason.abandoned,
          ),
        ),
      ),
    );

    expect(find.text('Run interrupted'), findsOneWidget);
    expect(find.text('The run ended before it could finish.'), findsOneWidget);
  });

  testWidgets('unknown state shows unavailable copy without raw backend text', (
    tester,
  ) async {
    await tester.pumpWidget(
      host(RunStateCard(state: state(lifecycle: RunLifecycle.unknown))),
    );

    expect(find.text('Run status unavailable'), findsOneWidget);
    expect(
      find.text("This app cannot identify the run's current status."),
      findsOneWidget,
    );
    // No raw numeric/backend enum value ever leaks into this card.
    expect(find.textContaining('RUN_LIFECYCLE'), findsNothing);
  });

  testWidgets('NoResponseCard shows the neutral legacy fallback copy', (
    tester,
  ) async {
    await tester.pumpWidget(host(const NoResponseCard()));

    expect(find.text('No response recorded'), findsOneWidget);
    expect(
      find.text('No assistant response was recorded for this run.'),
      findsOneWidget,
    );
  });

  testWidgets('RunStateCard reaches the semantics tree as a live region', (
    tester,
  ) async {
    final handle = tester.ensureSemantics();
    await tester.pumpWidget(
      host(RunStateCard(state: state(lifecycle: RunLifecycle.queued))),
    );

    expect(
      find.bySemanticsLabel('Queued: The run is waiting to start.'),
      findsOneWidget,
    );
    handle.dispose();
  });
}
