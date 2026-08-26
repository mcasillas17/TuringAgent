import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/models/run_lifecycle.dart';

/// The client sees committed projections, never the reason one was committed.
///
/// That is the whole shape of this graph. The backend distinguishes a run whose
/// owner handed it back in person from a run whose owner vanished, because it
/// can read the assignment attempt, the worker identity, and the execution
/// state inside the transaction that releases the run. None of those cross the
/// wire. What arrives here is `queued` at one version past `running`, and the
/// only honest thing the client can do with that pair is accept it.
///
/// So this helper is deliberately not the backend's rulebook. It rejects what
/// is impossible in public — anything leaving a terminal state, and the
/// backward edges no committed projection can produce — and accepts everything
/// the backend is allowed to commit, without pretending to know which private
/// trigger produced it.
void main() {
  group('canTransitionTo accepts every publicly committed pair', () {
    const accepted = <(RunLifecycle, RunLifecycle, String)>[
      (RunLifecycle.queued, RunLifecycle.running, 'a worker picked the run up'),
      (
        RunLifecycle.running,
        RunLifecycle.waitingApproval,
        'the run asked a human a question',
      ),
      (
        RunLifecycle.waitingApproval,
        RunLifecycle.running,
        'the owning attempt proved it was ready and resumed',
      ),
      (
        RunLifecycle.running,
        RunLifecycle.recovering,
        'ownership became uncertain mid-run',
      ),
      (
        RunLifecycle.waitingApproval,
        RunLifecycle.recovering,
        'ownership became uncertain while a decision was pending',
      ),
      (
        RunLifecycle.recovering,
        RunLifecycle.running,
        'the same owned attempt came back',
      ),
      (
        RunLifecycle.recovering,
        RunLifecycle.queued,
        'recovery gave the work back to the queue',
      ),
      (
        RunLifecycle.running,
        RunLifecycle.queued,
        'a confirmed release requeued the run without an uncertain phase',
      ),
    ];

    for (final (from, to, why) in accepted) {
      test('${from.name} -> ${to.name} ($why)', () {
        expect(from.canTransitionTo(to), isTrue);
      });
    }
  });

  group(
    'canTransitionTo accepts every terminal outcome from every live state',
    () {
      const live = <RunLifecycle>[
        RunLifecycle.queued,
        RunLifecycle.running,
        RunLifecycle.waitingApproval,
        RunLifecycle.recovering,
      ];
      const terminal = <RunLifecycle>[
        RunLifecycle.completed,
        RunLifecycle.failed,
        RunLifecycle.cancelled,
      ];

      for (final from in live) {
        for (final to in terminal) {
          test('${from.name} -> ${to.name}', () {
            // Completion is the one terminal a queued run cannot reach: nothing
            // ran, so there is no successful report to commit.
            final reachable =
                !(from == RunLifecycle.queued && to == RunLifecycle.completed);
            expect(from.canTransitionTo(to), reachable);
          });
        }
      }
    },
  );

  group('terminal states have no outgoing transitions', () {
    const terminal = <RunLifecycle>[
      RunLifecycle.completed,
      RunLifecycle.failed,
      RunLifecycle.cancelled,
    ];

    for (final from in terminal) {
      for (final to in RunLifecycle.values) {
        test('${from.name} -> ${to.name} is rejected', () {
          expect(from.canTransitionTo(to), isFalse);
        });
      }
    }
  });

  group('rejected pairs stay rejected', () {
    const rejected = <(RunLifecycle, RunLifecycle, String)>[
      (
        RunLifecycle.queued,
        RunLifecycle.waitingApproval,
        'nothing is running, so nothing can be asking',
      ),
      (
        RunLifecycle.queued,
        RunLifecycle.recovering,
        'a run nobody claimed has no owner to lose',
      ),
      (
        RunLifecycle.waitingApproval,
        RunLifecycle.queued,
        'an unanswered approval is not requeued behind the user\'s back',
      ),
      (
        RunLifecycle.running,
        RunLifecycle.running,
        'a repeat is the same state, not a transition',
      ),
      (
        RunLifecycle.queued,
        RunLifecycle.queued,
        'a repeat is the same state, not a transition',
      ),
      (
        RunLifecycle.recovering,
        RunLifecycle.waitingApproval,
        'recovery resumes to running; the approval handshake is its own edge',
      ),
      (
        RunLifecycle.recovering,
        RunLifecycle.recovering,
        'a repeat is the same state, not a transition',
      ),
    ];

    for (final (from, to, why) in rejected) {
      test('${from.name} -> ${to.name} ($why)', () {
        expect(from.canTransitionTo(to), isFalse);
      });
    }
  });

  // A total function is what lets a reconciler ask about any pair it decoded,
  // including the two reserved values it may be handed by an older or newer
  // backend. Neither of those describes a state, so nothing transitions into
  // or out of them.
  group('reserved values are inert rather than exceptional', () {
    for (final reserved in <RunLifecycle>[
      RunLifecycle.unspecified,
      RunLifecycle.unknown,
    ]) {
      test('${reserved.name} has no outgoing transitions', () {
        for (final to in RunLifecycle.values) {
          expect(reserved.canTransitionTo(to), isFalse);
        }
      });

      test('${reserved.name} has no incoming transitions', () {
        for (final from in RunLifecycle.values) {
          expect(from.canTransitionTo(reserved), isFalse);
        }
      });
    }
  });
}
