import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/generated/turing/v1/team.pb.dart' as teampb;
import 'package:turing_flutter_app/models/delegation.dart';
import 'package:turing_flutter_app/models/grpc_mappers.dart';

Map<String, dynamic> _payload(Map<String, dynamic> overrides) => {
  'delegationId': 'dlg_1',
  'parentRunId': 'run_parent',
  'childRunId': 'run_child',
  'childSessionId': 'sess_child',
  'profileId': 'research',
  'displayName': 'Research',
  'emoji': '🔬',
  ...overrides,
};

void main() {
  test('a delegation maps from the read API with everything its card shows', () {
    final delegation = GrpcMappers.delegationToModel(
      teampb.Delegation(
        delegationId: 'dlg_1',
        parentSessionId: 'sess_parent',
        parentRunId: 'run_parent',
        childSessionId: 'sess_child',
        childRunId: 'run_child',
        profileId: 'research',
        displayName: 'Research',
        emoji: '🔬',
        task: 'Gather the notes',
        state: 'waiting_approval',
        approvalId: 'appr_1',
        continuationRunId: 'run_continuation',
        summary: '🔬 Research is waiting for your approval',
      ),
    );

    expect(delegation.delegationId, 'dlg_1');
    expect(delegation.parentRunId, 'run_parent');
    expect(delegation.childSessionId, 'sess_child');
    expect(delegation.childRunId, 'run_child');
    expect(delegation.specialist, '🔬 Research');
    expect(delegation.task, 'Gather the notes');
    expect(delegation.state, 'waiting_approval');
    expect(delegation.approvalId, 'appr_1');
    expect(delegation.continuationRunId, 'run_continuation');
    expect(delegation.summary, '🔬 Research is waiting for your approval');
    expect(delegation.isTerminal, isFalse);
  });

  test('a started event makes a card, and later events replace its state', () {
    final started = Delegation.fromEvent(
      null,
      _payload({
        'state': 'queued',
        'summary': 'Asked 🔬 Research',
        'task': 'Gather the notes',
      }),
    )!;
    expect(started.state, 'queued');
    expect(started.task, 'Gather the notes');
    expect(started.specialist, '🔬 Research');

    final waiting = Delegation.fromEvent(
      started,
      _payload({
        'state': 'waiting_approval',
        'approvalId': 'appr_1',
        'summary': '🔬 Research is waiting for your approval',
      }),
    )!;
    expect(waiting.state, 'waiting_approval');
    expect(waiting.approvalId, 'appr_1');
    // An update names no task; the card keeps the one it was given.
    expect(waiting.task, 'Gather the notes');

    final approved = Delegation.fromEvent(
      waiting,
      _payload({'state': 'waiting_approval', 'summary': 'still waiting'}),
    )!;
    expect(approved.approvalId, isEmpty);

    final finished = Delegation.fromEvent(
      approved,
      _payload({
        'state': 'completed',
        'summary': '🔬 Research completed',
        'continuationRunId': 'run_continuation',
      }),
    )!;
    expect(finished.isTerminal, isTrue);
    expect(finished.continuationRunId, 'run_continuation');
    expect(finished.summary, '🔬 Research completed');
  });

  test('an event about a different or unnamed delegation changes nothing', () {
    final card = Delegation.fromEvent(null, _payload({'state': 'running'}))!;
    expect(
      Delegation.fromEvent(card, _payload({'delegationId': 'dlg_2'})),
      same(card),
    );
    expect(Delegation.fromEvent(null, {'state': 'running'}), isNull);
    expect(
      Delegation.fromEvent(null, _payload({'parentRunId': ''})),
      isNull,
      reason: 'a card that cannot be placed under its turn is not made',
    );
  });

  test('only completed, failed and cancelled are terminal', () {
    for (final state in ['queued', 'running', 'waiting_approval']) {
      expect(
        Delegation.fromEvent(null, _payload({'state': state}))!.isTerminal,
        isFalse,
      );
    }
    for (final state in ['completed', 'failed', 'cancelled']) {
      expect(
        Delegation.fromEvent(null, _payload({'state': state}))!.isTerminal,
        isTrue,
      );
    }
  });
}
