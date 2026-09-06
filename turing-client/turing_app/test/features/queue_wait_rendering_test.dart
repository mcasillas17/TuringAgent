import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/run_state_card.dart';
import 'package:turing_flutter_app/features/chat/run_state_reconciler.dart';
import 'package:turing_flutter_app/l10n/generated/app_localizations.dart';
import 'package:turing_flutter_app/models/run_lifecycle.dart';
import 'package:turing_flutter_app/models/run_state.dart';

/// TUR-010's client half: a run that is waiting on something the user can act
/// on says so, a run that stopped waiting says which bound ended it, and both
/// answers are the same whether they arrived on a live stream or came back with
/// a reopened conversation.
void main() {
  final updatedAt = DateTime.utc(2026, 9, 5, 12);

  RunState state({
    RunLifecycle lifecycle = RunLifecycle.queued,
    RunOutcomeReason outcomeReason = RunOutcomeReason.none,
    QueueWaitReason queueWaitReason = QueueWaitReason.none,
    int stateVersion = 1,
    bool hasDisplayableContent = false,
  }) {
    return RunState(
      runId: 'run_queue',
      userMessageId: 'msg_user',
      assistantMessageId: 'msg_assistant',
      lifecycle: lifecycle,
      outcomeReason: outcomeReason,
      stateVersion: stateVersion,
      stateUpdatedAt: updatedAt,
      finishedAt:
          lifecycle == RunLifecycle.failed ||
              lifecycle == RunLifecycle.cancelled
          ? updatedAt
          : null,
      hasDisplayableContent: hasDisplayableContent,
      queueWaitReason: queueWaitReason,
    );
  }

  Widget host(Widget child) {
    return MaterialApp(
      localizationsDelegates: AppLocalizations.localizationsDelegates,
      supportedLocales: AppLocalizations.supportedLocales,
      home: Scaffold(body: child),
    );
  }

  testWidgets('a queued run with no compatible worker explains itself', (
    tester,
  ) async {
    await tester.pumpWidget(
      host(
        RunStateCard(
          state: state(queueWaitReason: QueueWaitReason.noCompatibleWorker),
        ),
      ),
    );

    expect(find.text('Waiting for an assistant'), findsOneWidget);
    // The plain queued copy would say nothing about why this one is stuck.
    expect(find.text('Queued'), findsNothing);
  });

  testWidgets('an ordinary queued run keeps the plain queued card', (
    tester,
  ) async {
    await tester.pumpWidget(host(RunStateCard(state: state())));

    expect(find.text('Queued'), findsOneWidget);
    expect(find.text('Waiting for an assistant'), findsNothing);
  });

  testWidgets('a queue reason this build cannot name renders the plain card', (
    tester,
  ) async {
    await tester.pumpWidget(
      host(
        RunStateCard(state: state(queueWaitReason: QueueWaitReason.unknown)),
      ),
    );

    expect(find.text('Queued'), findsOneWidget);
  });

  testWidgets('a run ended by the no-worker bound says so, not "expired"', (
    tester,
  ) async {
    await tester.pumpWidget(
      host(
        RunStateCard(
          state: state(
            lifecycle: RunLifecycle.failed,
            outcomeReason: RunOutcomeReason.expired,
            queueWaitReason: QueueWaitReason.noCompatibleWorker,
          ),
        ),
      ),
    );

    expect(find.text('No assistant became available'), findsOneWidget);
    expect(find.text('Run expired'), findsNothing);
  });

  testWidgets('a run ended by the overall queue bound says so', (tester) async {
    await tester.pumpWidget(
      host(
        RunStateCard(
          state: state(
            lifecycle: RunLifecycle.failed,
            outcomeReason: RunOutcomeReason.expired,
            queueWaitReason: QueueWaitReason.queueTimeout,
          ),
        ),
      ),
    );

    expect(find.text('Waited too long to start'), findsOneWidget);
  });

  testWidgets('the cancel policy renders the same queue truth', (tester) async {
    await tester.pumpWidget(
      host(
        RunStateCard(
          state: state(
            lifecycle: RunLifecycle.cancelled,
            outcomeReason: RunOutcomeReason.abandoned,
            queueWaitReason: QueueWaitReason.queueTimeout,
          ),
        ),
      ),
    );

    expect(find.text('Waited too long to start'), findsOneWidget);
    // The generic abandonment copy would blame the connection for a run the
    // orchestrator deliberately stopped waiting for.
    expect(find.text('Run interrupted'), findsNothing);
  });

  testWidgets('a run abandoned by its client keeps its own copy', (
    tester,
  ) async {
    // The backend clears the queue reason when a run leaves the queue, so a run
    // that waited, started, and was then abandoned arrives here as `none`. The
    // client must not describe it as one that never started — `abandoned` is
    // also the cancel policy's outcome, and only the queue reason separates
    // them.
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

    expect(find.text('No assistant became available'), findsNothing);
    expect(find.text('Waited too long to start'), findsNothing);
  });

  testWidgets('an approval expiry keeps its own copy', (tester) async {
    await tester.pumpWidget(
      host(
        RunStateCard(
          state: state(
            lifecycle: RunLifecycle.failed,
            outcomeReason: RunOutcomeReason.expired,
          ),
        ),
      ),
    );

    expect(find.text('Run expired'), findsOneWidget);
    expect(find.text('Waited too long to start'), findsNothing);
  });

  group('reconciliation', () {
    test('a queue observation is accepted as a higher-version self edge', () {
      final reconciler = RunStateReconciler();
      final queued = state();
      expect(reconciler.reconcile(queued).isAccepted, isTrue);

      final observed = state(
        stateVersion: 2,
        queueWaitReason: QueueWaitReason.noCompatibleWorker,
      );
      final result = reconciler.reconcile(observed);
      expect(result.outcome, RunStateReconciliationOutcome.accepted);
      expect(
        result.current?.queueWaitReason,
        QueueWaitReason.noCompatibleWorker,
      );
    });

    test('an older queue observation cannot undo a newer one', () {
      final reconciler = RunStateReconciler();
      reconciler.reconcile(
        state(
          stateVersion: 3,
          queueWaitReason: QueueWaitReason.noCompatibleWorker,
        ),
      );
      final result = reconciler.reconcile(state(stateVersion: 2));
      expect(result.outcome, RunStateReconciliationOutcome.stale);
      expect(
        result.current?.queueWaitReason,
        QueueWaitReason.noCompatibleWorker,
      );
    });

    test('two snapshots at one version disagreeing on the queue conflict', () {
      final reconciler = RunStateReconciler();
      reconciler.reconcile(
        state(
          stateVersion: 2,
          queueWaitReason: QueueWaitReason.noCompatibleWorker,
        ),
      );
      final result = reconciler.reconcile(state(stateVersion: 2));
      expect(result.outcome, RunStateReconciliationOutcome.inconsistent);
      expect(
        result.current?.queueWaitReason,
        QueueWaitReason.noCompatibleWorker,
      );
    });

    test('live and reopened paths reach the same accepted state', () {
      // The reopened path replays history rows through the same reconcile
      // call a live event uses, so the only thing that can make them differ is
      // arrival order — which version ordering already settles.
      final live = RunStateReconciler();
      live.reconcile(state());
      live.reconcile(
        state(
          stateVersion: 2,
          queueWaitReason: QueueWaitReason.noCompatibleWorker,
        ),
      );

      final reopened = RunStateReconciler();
      reconcilePage(reopened, [
        MessagePageEntry(
          messageId: 'msg_assistant',
          runState: state(
            stateVersion: 2,
            queueWaitReason: QueueWaitReason.noCompatibleWorker,
          ),
        ),
      ], <String>{});

      expect(reopened.stateFor('run_queue'), live.stateFor('run_queue'));
    });

    test('nothing follows a terminal queue outcome', () {
      final reconciler = RunStateReconciler();
      reconciler.reconcile(
        state(
          lifecycle: RunLifecycle.failed,
          outcomeReason: RunOutcomeReason.expired,
          queueWaitReason: QueueWaitReason.queueTimeout,
          stateVersion: 4,
        ),
      );
      final result = reconciler.reconcile(
        state(
          stateVersion: 5,
          queueWaitReason: QueueWaitReason.noCompatibleWorker,
        ),
      );
      expect(result.outcome, RunStateReconciliationOutcome.inconsistent);
      expect(result.current?.lifecycle, RunLifecycle.failed);
    });
  });
}
