import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/run_state_reconciler.dart';
import 'package:turing_flutter_app/models/run_lifecycle.dart';
import 'package:turing_flutter_app/models/run_state.dart';

/// Pure, non-widget coverage for [RunStateReconciler] and
/// [RunStateLoadBuffer] — the design's reconciliation rules and bounded
/// initial-load buffer, exercised directly with no [WidgetTester] involved.
void main() {
  final baseUpdatedAt = DateTime.utc(2026, 8, 20, 12);

  RunState state({
    String runId = 'run_1',
    RunLifecycle lifecycle = RunLifecycle.queued,
    RunOutcomeReason outcomeReason = RunOutcomeReason.none,
    int stateVersion = 1,
    bool hasDisplayableContent = false,
    DateTime? finishedAt,
    DateTime? stateUpdatedAt,
  }) {
    return RunState(
      runId: runId,
      userMessageId: 'msg_user',
      assistantMessageId: 'msg_assistant',
      lifecycle: lifecycle,
      outcomeReason: outcomeReason,
      stateVersion: stateVersion,
      stateUpdatedAt: stateUpdatedAt ?? baseUpdatedAt,
      finishedAt: finishedAt,
      hasDisplayableContent: hasDisplayableContent,
    );
  }

  group('RunStateReconciler', () {
    test('accepts first valid nonzero version', () {
      final reconciler = RunStateReconciler();
      final incoming = state(stateVersion: 1);

      final result = reconciler.reconcile(incoming);

      expect(result.outcome, RunStateReconciliationOutcome.accepted);
      expect(result.current, incoming);
      expect(reconciler.stateFor('run_1'), incoming);
    });

    test('rejects a nonpositive version without retaining state', () {
      final reconciler = RunStateReconciler();
      final invalid = state(stateVersion: 0);

      final result = reconciler.reconcile(invalid);

      expect(result.outcome, RunStateReconciliationOutcome.inconsistent);
      expect(result.current, isNull);
      expect(reconciler.stateFor('run_1'), isNull);
    });

    test(
      'returns unloaded without retaining state when the row is unavailable',
      () {
        final reconciler = RunStateReconciler();
        final incoming = state(stateVersion: 1);

        final result = reconciler.reconcile(incoming, isLoaded: false);

        expect(result.outcome, RunStateReconciliationOutcome.unloaded);
        expect(result.current, isNull);
        expect(reconciler.stateFor('run_1'), isNull);
      },
    );

    test('ignores lower version', () {
      final reconciler = RunStateReconciler();
      final current = state(stateVersion: 3, lifecycle: RunLifecycle.running);
      reconciler.reconcile(current);

      final stale = state(stateVersion: 2, lifecycle: RunLifecycle.queued);
      final result = reconciler.reconcile(stale);

      expect(result.outcome, RunStateReconciliationOutcome.stale);
      // The existing, higher-version state is untouched.
      expect(result.current, current);
      expect(reconciler.stateFor('run_1'), current);
    });

    test('treats equal identical state as no-op', () {
      final reconciler = RunStateReconciler();
      final first = state(
        stateVersion: 4,
        lifecycle: RunLifecycle.failed,
        outcomeReason: RunOutcomeReason.providerFailure,
      );
      reconciler.reconcile(first);

      // A byte-for-byte identical terminal report replayed a second time.
      final duplicate = state(
        stateVersion: 4,
        lifecycle: RunLifecycle.failed,
        outcomeReason: RunOutcomeReason.providerFailure,
      );
      final result = reconciler.reconcile(duplicate);

      expect(result.outcome, RunStateReconciliationOutcome.duplicate);
      expect(result.current, first);
      expect(reconciler.stateFor('run_1'), first);
    });

    test('rejects equal conflicting state', () {
      final reconciler = RunStateReconciler();
      final first = state(
        stateVersion: 4,
        lifecycle: RunLifecycle.failed,
        outcomeReason: RunOutcomeReason.providerFailure,
      );
      reconciler.reconcile(first);

      // Same version, but a DIFFERENT outcome reason — a genuine conflict,
      // not a replay.
      final conflicting = state(
        stateVersion: 4,
        lifecycle: RunLifecycle.failed,
        outcomeReason: RunOutcomeReason.toolFailure,
      );
      final result = reconciler.reconcile(conflicting);

      expect(result.outcome, RunStateReconciliationOutcome.inconsistent);
      // The originally accepted state must survive a conflicting report.
      expect(result.current, first);
      expect(reconciler.stateFor('run_1'), first);
    });

    // state_version is the sole reconciliation ordering authority (see the
    // outcome doc comment) — state_updated_at must never override it, no
    // matter how much fresher or staler its wall-clock value claims to be.
    test(
      'ignores a lower version even when its state_updated_at is strictly '
      'newer',
      () {
        final reconciler = RunStateReconciler();
        final current = state(
          stateVersion: 3,
          lifecycle: RunLifecycle.running,
          stateUpdatedAt: baseUpdatedAt,
        );
        reconciler.reconcile(current);

        // Lower version, but its state_updated_at is strictly AFTER the
        // existing state's — a version ordered purely by the wall clock
        // would (wrongly) treat this as newer.
        final stale = state(
          stateVersion: 2,
          lifecycle: RunLifecycle.queued,
          stateUpdatedAt: baseUpdatedAt.add(const Duration(minutes: 5)),
        );
        final result = reconciler.reconcile(stale);

        expect(result.outcome, RunStateReconciliationOutcome.stale);
        expect(result.current, current);
        expect(reconciler.stateFor('run_1'), current);
      },
    );

    test(
      'accepts a higher version even when its state_updated_at is strictly '
      'older',
      () {
        final reconciler = RunStateReconciler();
        final current = state(
          stateVersion: 1,
          lifecycle: RunLifecycle.queued,
          stateUpdatedAt: baseUpdatedAt,
        );
        reconciler.reconcile(current);

        // Higher, structurally valid version (queued -> running), but its
        // state_updated_at is strictly BEFORE the one already held — e.g. a
        // delayed delivery of an earlier wall-clock write. Only
        // state_version orders snapshots, so this must still be accepted.
        final olderTimestamp = state(
          stateVersion: 2,
          lifecycle: RunLifecycle.running,
          stateUpdatedAt: baseUpdatedAt.subtract(const Duration(minutes: 5)),
        );
        final result = reconciler.reconcile(olderTimestamp);

        expect(result.outcome, RunStateReconciliationOutcome.accepted);
        expect(result.current, olderTimestamp);
        expect(reconciler.stateFor('run_1'), olderTimestamp);
      },
    );

    // RunState equality (and therefore the duplicate/inconsistent split at
    // an equal version) includes state_updated_at — two reports at the same
    // version that differ ONLY in their timestamp are not a safe replay of
    // the same fact, so this must be inconsistent, not duplicate.
    test(
      'rejects equal version differing only in state_updated_at as '
      'inconsistent, not duplicate',
      () {
        final reconciler = RunStateReconciler();
        final first = state(
          stateVersion: 4,
          lifecycle: RunLifecycle.failed,
          outcomeReason: RunOutcomeReason.providerFailure,
          stateUpdatedAt: baseUpdatedAt,
        );
        reconciler.reconcile(first);

        final differentTimestamp = state(
          stateVersion: 4,
          lifecycle: RunLifecycle.failed,
          outcomeReason: RunOutcomeReason.providerFailure,
          stateUpdatedAt: baseUpdatedAt.add(const Duration(seconds: 1)),
        );
        final result = reconciler.reconcile(differentTimestamp);

        expect(result.outcome, RunStateReconciliationOutcome.inconsistent);
        expect(result.current, first);
        expect(reconciler.stateFor('run_1'), first);
      },
    );

    test('rejects higher version after terminal', () {
      final reconciler = RunStateReconciler();
      final completed = state(
        stateVersion: 5,
        lifecycle: RunLifecycle.completed,
        hasDisplayableContent: true,
      );
      reconciler.reconcile(completed);

      // A higher version arrives claiming the run somehow kept running after
      // it was already terminal — structurally this would even be a legal
      // edge FROM `running`, but nothing may ever follow a terminal state.
      final impossible = state(
        stateVersion: 6,
        lifecycle: RunLifecycle.running,
      );
      final result = reconciler.reconcile(impossible);

      expect(result.outcome, RunStateReconciliationOutcome.inconsistent);
      expect(result.current, completed);
      expect(reconciler.stateFor('run_1'), completed);
    });

    test('accepts only valid higher nonterminal transition', () {
      final reconciler = RunStateReconciler();
      reconciler.reconcile(
        state(stateVersion: 1, lifecycle: RunLifecycle.queued),
      );

      // queued -> completed is not an edge any committed projection can
      // produce (nothing ran, so there is no successful report to commit) —
      // must be rejected even though the version bump alone looks valid.
      final invalid = state(
        stateVersion: 2,
        lifecycle: RunLifecycle.completed,
        hasDisplayableContent: true,
      );
      final rejected = reconciler.reconcile(invalid);
      expect(rejected.outcome, RunStateReconciliationOutcome.inconsistent);
      expect(
        reconciler.stateFor('run_1')!.lifecycle,
        RunLifecycle.queued,
        reason: 'the invalid transition must not have moved the run forward',
      );

      // queued -> running is a real, committable edge and must be accepted.
      final valid = state(stateVersion: 2, lifecycle: RunLifecycle.running);
      final accepted = reconciler.reconcile(valid);
      expect(accepted.outcome, RunStateReconciliationOutcome.accepted);
      expect(reconciler.stateFor('run_1'), valid);
    });

    // Simulates the sequence of RunState snapshots that would accompany a
    // realistic run: `agent.run.queued`, `agent.run.started`,
    // `approval.requested`, `approval.approved` (back to running), and
    // finally `agent.run.state_changed` moving it into recovery — proving
    // the reconciler decides purely from runId/version/lifecycle, with no
    // notion of "event type" anywhere in its API, so reconciling ahead of a
    // type-specific switch can never depend on which case fired.
    test('state bearing queued started approval and state changed events '
        'reconcile before type handling', () {
      final reconciler = RunStateReconciler();

      final queued = state(stateVersion: 1, lifecycle: RunLifecycle.queued);
      final started = state(stateVersion: 2, lifecycle: RunLifecycle.running);
      final approvalRequested = state(
        stateVersion: 3,
        lifecycle: RunLifecycle.waitingApproval,
      );
      final approvalApproved = state(
        stateVersion: 4,
        lifecycle: RunLifecycle.running,
      );
      final stateChangedToRecovering = state(
        stateVersion: 5,
        lifecycle: RunLifecycle.recovering,
      );

      for (final snapshot in [
        queued,
        started,
        approvalRequested,
        approvalApproved,
        stateChangedToRecovering,
      ]) {
        final result = reconciler.reconcile(snapshot);
        expect(
          result.outcome,
          RunStateReconciliationOutcome.accepted,
          reason:
              'every snapshot in this realistic sequence is a valid '
              'committed transition regardless of the event type that '
              'would have carried it',
        );
      }
      expect(reconciler.stateFor('run_1'), stateChangedToRecovering);
    });

    test('history and live events use the same reconciliation path', () {
      // Two independent reconcilers modeling: one fed only from a loaded
      // history page, one fed only from live/replayed events. Feeding the
      // exact same snapshots down each must produce identical outcomes and
      // identical final state — there is no separate "history" logic.
      final historyReconciler = RunStateReconciler();
      final liveReconciler = RunStateReconciler();

      final snapshots = [
        state(stateVersion: 1, lifecycle: RunLifecycle.queued),
        state(stateVersion: 2, lifecycle: RunLifecycle.running),
        state(
          stateVersion: 3,
          lifecycle: RunLifecycle.failed,
          outcomeReason: RunOutcomeReason.toolFailure,
        ),
      ];

      final historyOutcomes = snapshots
          .map((snapshot) => historyReconciler.reconcile(snapshot).outcome)
          .toList();
      final liveOutcomes = snapshots
          .map((snapshot) => liveReconciler.reconcile(snapshot).outcome)
          .toList();

      expect(historyOutcomes, liveOutcomes);
      expect(
        historyReconciler.stateFor('run_1'),
        liveReconciler.stateFor('run_1'),
      );
    });

    test('overlapping pages deduplicate by message id and run id version', () {
      final reconciler = RunStateReconciler();
      final alreadyLoaded = <String>{'msg_user', 'msg_assistant'};

      // First page: the assistant row still mid-run.
      final firstPage = [
        const MessagePageEntry(messageId: 'msg_user'),
        MessagePageEntry(
          messageId: 'msg_assistant',
          runState: state(stateVersion: 2, lifecycle: RunLifecycle.running),
        ),
      ];
      final firstResults = reconcilePage(reconciler, firstPage, alreadyLoaded);
      expect(firstResults, hasLength(2));
      expect(firstResults[0].isDuplicateMessage, isTrue);
      expect(firstResults[1].isDuplicateMessage, isTrue);
      expect(
        firstResults[1].stateResult!.outcome,
        RunStateReconciliationOutcome.accepted,
      );

      // A SECOND, overlapping page load returns the exact same two message
      // rows (both already loaded), but the run has since reached a higher,
      // valid, terminal version. The message id dedup must not suppress
      // that state update.
      final secondPage = [
        const MessagePageEntry(messageId: 'msg_user'),
        MessagePageEntry(
          messageId: 'msg_assistant',
          runState: state(
            stateVersion: 3,
            lifecycle: RunLifecycle.completed,
            hasDisplayableContent: true,
          ),
        ),
      ];
      final secondResults = reconcilePage(
        reconciler,
        secondPage,
        alreadyLoaded,
      );
      expect(secondResults[0].isDuplicateMessage, isTrue);
      expect(secondResults[1].isDuplicateMessage, isTrue);
      expect(
        secondResults[1].stateResult!.outcome,
        RunStateReconciliationOutcome.accepted,
        reason:
            'the run state must still reconcile forward even though the '
            'message row itself is a dedup-suppressed repeat',
      );
      expect(reconciler.stateFor('run_1')!.lifecycle, RunLifecycle.completed);
    });
  });

  group('RunStateLoadBuffer', () {
    test(
      'buffers only highest version for each run during initial page load',
      () {
        final buffer = RunStateLoadBuffer();

        buffer.offer(state(runId: 'run_a', stateVersion: 1));
        buffer.offer(state(runId: 'run_a', stateVersion: 3));
        buffer.offer(state(runId: 'run_a', stateVersion: 2));

        expect(buffer.length, 1);
        final drained = buffer.drainAndClear();
        expect(drained, hasLength(1));
        expect(drained.single.stateVersion, 3);
      },
    );

    test('holds at most sixty four distinct run states', () {
      final buffer = RunStateLoadBuffer();

      for (var i = 0; i < RunStateLoadBuffer.maxDistinctRuns; i++) {
        buffer.offer(state(runId: 'run_$i', stateVersion: 1));
      }

      expect(buffer.length, RunStateLoadBuffer.maxDistinctRuns);
      expect(buffer.resyncRequired, isFalse);
    });

    test(
      'ten thousand unloaded events clear buffer and coalesce one resync',
      () {
        final buffer = RunStateLoadBuffer();

        for (var i = 0; i < 10000; i++) {
          buffer.offer(state(runId: 'run_$i', stateVersion: 1));
        }

        expect(buffer.resyncRequired, isTrue);
        expect(
          buffer.length,
          0,
          reason: 'overflow clears the buffer rather than growing it',
        );
        expect(buffer.drainAndClear(), isEmpty);
      },
    );

    test('events after overflow are not retained', () {
      final buffer = RunStateLoadBuffer();
      for (var i = 0; i < RunStateLoadBuffer.maxDistinctRuns + 1; i++) {
        buffer.offer(state(runId: 'run_$i', stateVersion: 1));
      }
      expect(buffer.resyncRequired, isTrue);

      // Further offers, including for runs already seen before overflow,
      // must remain no-ops once a resync is required.
      buffer.offer(state(runId: 'run_0', stateVersion: 99));
      buffer.offer(state(runId: 'run_new', stateVersion: 1));

      expect(buffer.length, 0);
      expect(buffer.drainAndClear(), isEmpty);
    });
  });
}
