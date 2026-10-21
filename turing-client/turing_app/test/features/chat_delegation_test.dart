import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/approvals/approval_card.dart';
import 'package:turing_flutter_app/features/chat/chat_screen.dart';
import 'package:turing_flutter_app/features/chat/delegation_card.dart';
import 'package:turing_flutter_app/features/chat/run_notice_card.dart';
import 'package:turing_flutter_app/l10n/generated/app_localizations.dart';
import 'package:turing_flutter_app/models/agent_descriptor.dart';
import 'package:turing_flutter_app/models/approval.dart';
import 'package:turing_flutter_app/models/delegation.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/models/remote_egress.dart';
import 'package:turing_flutter_app/models/run_cancellation.dart';
import 'package:turing_flutter_app/models/search_hit.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/session_deletion.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';
import 'package:turing_flutter_app/models/turing_event.dart';
import 'package:turing_flutter_app/networking/api_client.dart';
import 'package:turing_flutter_app/networking/event_source.dart';

import '../support/approval_details.dart';
import '../support/no_automations_api.dart';
import '../support/no_external_agents_api.dart';
import '../support/no_integrations_api.dart';
import '../support/no_session_lifecycle_api.dart';
import '../support/no_skills_api.dart';
import '../support/no_telemetry_api.dart';

const _research = Delegation(
  delegationId: 'dlg_1',
  parentRunId: 'run_parent',
  childSessionId: 'sess_child',
  childRunId: 'run_child',
  displayName: 'Research',
  emoji: '🔬',
  task: 'Gather the notes',
  state: 'completed',
  summary: '🔬 Research completed',
  continuationRunId: 'run_continuation',
);

final _turn = [
  _message('msg_user', 'user', 'Prep me for tomorrow', sequence: 1),
  _message(
    'msg_parent',
    'assistant',
    'I asked Research to gather the notes.',
    runId: 'run_parent',
    sequence: 2,
  ),
];

Delegation _waitingForConsent({bool required = false}) => Delegation(
  delegationId: 'dlg_1',
  parentRunId: 'run_parent',
  childSessionId: 'sess_child',
  childRunId: 'run_child',
  displayName: 'Research',
  emoji: '🔬',
  task: 'Gather the notes',
  state: 'waiting_consent',
  summary: '🔬 Research is waiting for your consent',
  egressTools: const ['integrations/github.get_issue'],
  egressConsent: 'pending',
  egressRequired: required,
);

void main() {
  testWidgets('a task waiting for consent asks on its card', (tester) async {
    final api = _DelegationApi()
      ..history = _turn
      ..delegations = [_waitingForConsent()];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    expect(
      find.text('Wants to use: integrations/github.get_issue'),
      findsOneWidget,
    );
    expect(find.text('Allow…'), findsOneWidget);
    expect(find.text('Run without'), findsOneWidget);
    expect(find.text('Cancel'), findsOneWidget);
    await _close(tester, events);
  });

  testWidgets('Allow shows what may leave and records the consent', (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = _turn
      ..delegations = [_waitingForConsent()];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    await tester.tap(find.text('Allow…'));
    await tester.pumpAndSettle();
    expect(api.prepared, ['dlg_1']);
    expect(
      find.text('🔬 Research asks to use these tools for this one task.'),
      findsOneWidget,
    );
    await tester.tap(find.text('Allow'));
    await tester.pumpAndSettle();

    final decision = api.decided.single;
    expect(decision.delegationId, 'dlg_1');
    expect(decision.consent?.challenge, 'signed-challenge');
    expect(decision.consent?.acknowledgedDataCategories, [
      EgressDataCategory.toolArguments,
      EgressDataCategory.toolResults,
    ]);
    await _close(tester, events);
  });

  testWidgets('closing the disclosure decides nothing', (tester) async {
    final api = _DelegationApi()
      ..history = _turn
      ..delegations = [_waitingForConsent()];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    await tester.tap(find.text('Allow…'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(TextButton, 'Cancel').last);
    await tester.pumpAndSettle();

    expect(api.decided, isEmpty);
    await _close(tester, events);
  });

  testWidgets('Run without declines, once while it is open', (tester) async {
    final pending = Completer<Delegation>();
    final api = _DelegationApi()
      ..history = _turn
      ..decideAnswer = pending.future
      ..delegations = [_waitingForConsent()];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    TextButton button(String label) =>
        tester.widget<TextButton>(find.widgetWithText(TextButton, label));
    // Two presses in one frame, before the card can disable the button.
    final runWithout = button('Run without').onPressed!;
    runWithout();
    runWithout();
    await tester.pump();
    expect(button('Run without').onPressed, isNull);
    expect(button('Allow…').onPressed, isNull);
    pending.complete(_withState(_research, 'queued'));
    await tester.pumpAndSettle();

    expect(api.decided, hasLength(1));
    expect(api.decided.single.consent, isNull);
    await _close(tester, events);
  });

  testWidgets('a task that needs its tools says declining stops it', (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = _turn
      ..decideAnswer = Future.value(_withState(_research, 'cancelled'))
      ..delegations = [_waitingForConsent(required: true)];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);
    final reads = api.listMessagesCalls;

    expect(find.text('Run without'), findsNothing);
    await tester.tap(find.text('Decline (stops the task)'));
    await tester.pumpAndSettle();

    expect(api.decided.single.consent, isNull);
    // The task ended, so the join may be in; the screen asks for it.
    expect(api.listMessagesCalls, greaterThan(reads));
    await _close(tester, events);
  });

  testWidgets('a request that cannot be opened is reported', (tester) async {
    final api = _DelegationApi()
      ..history = _turn
      ..prepareError = const TuringApiException(
        code: 'failed_precondition',
        message: 'the tools changed',
      )
      ..delegations = [_waitingForConsent()];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    await tester.tap(find.text('Allow…'));
    await tester.pumpAndSettle();

    expect(
      find.text("Could not open 🔬 Research's request: the tools changed"),
      findsOneWidget,
    );
    expect(api.decided, isEmpty);
    await _close(tester, events);
  });

  testWidgets('a live task waiting for consent asks on its card', (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = _turn
      ..delegations = const [];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    events.add(
      _delegationEvent('delegation.started', 1, {
        'state': 'waiting_consent',
        'summary': 'Asked 🔬 Research',
        'egressTools': ['integrations/github.get_issue'],
        'egressConsent': 'pending',
        'egressRequired': false,
      }),
    );
    await tester.pump();
    expect(find.text('Allow…'), findsOneWidget);

    events.add(
      _delegationEvent('delegation.updated', 2, {
        'state': 'queued',
        'summary': '🔬 Research is queued',
        'egressConsent': 'granted',
      }),
    );
    await tester.pump();
    expect(find.text('Allow…'), findsNothing);
    expect(find.text('Run without'), findsNothing);
    expect(find.textContaining('Wants to use'), findsNothing);
    await _close(tester, events);
  });

  testWidgets('a decision shows on the card without waiting for its update', (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = _turn
      ..delegations = [_waitingForConsent()];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    await tester.tap(find.text('Run without'));
    await tester.pumpAndSettle();

    expect(find.text('Run without'), findsNothing);
    expect(find.text('🔬 Research is queued'), findsOneWidget);
    await _close(tester, events);
  });

  testWidgets('a decision older than the task\'s update does not undo it', (
    tester,
  ) async {
    final pending = Completer<Delegation>();
    final api = _DelegationApi()
      ..history = _turn
      ..decideAnswer = pending.future
      ..delegations = [_waitingForConsent()];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    await tester.tap(find.text('Run without'));
    await tester.pump();
    events.add(
      _delegationEvent('delegation.updated', 1, {
        'state': 'running',
        'summary': '🔬 Research is working',
        'egressConsent': 'declined',
      }),
    );
    await tester.pump();
    pending.complete(_withState(_research, 'queued'));
    await tester.pumpAndSettle();

    expect(find.text('🔬 Research is working'), findsOneWidget);
    expect(find.text('🔬 Research is queued'), findsNothing);
    await _close(tester, events);
  });

  testWidgets('an unexplained refusal is not echoed', (tester) async {
    final api = _DelegationApi()
      ..history = _turn
      ..decideError = const TuringApiException(
        code: 'internal',
        message: 'database is locked',
      )
      ..delegations = [_waitingForConsent()];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    await tester.tap(find.text('Run without'));
    await tester.pumpAndSettle();

    expect(
      find.text(
        "Could not record your decision for 🔬 Research's task. Try again.",
      ),
      findsOneWidget,
    );
    expect(find.textContaining('database is locked'), findsNothing);
    await _close(tester, events);
  });

  testWidgets('a deleted conversation asks for no consent', (tester) async {
    final api = _DelegationApi()
      ..history = _turn
      ..delegations = [_waitingForConsent()];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);
    expect(find.text('Allow…'), findsOneWidget);

    events.add(
      TuringEvent(
        eventId: 'evt_1',
        sessionId: 'sess_1',
        traceId: 'trace_1',
        sequence: 1,
        type: 'session.deleted',
        createdAt: DateTime.utc(2026, 10, 1),
        payload: const {},
      ),
    );
    await tester.pump();
    expect(find.text('Allow…'), findsNothing);
    expect(find.text('Run without'), findsNothing);
    await _close(tester, events);
  });

  testWidgets('reopening rebuilds each card under the turn that asked', (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = [
        ..._turn,
        _message(
          'msg_join',
          'system',
          'Results of the 1 task(s) Turing delegated.\n\n## 🔬 Research '
              '(research): completed\nThe notes say ship it.',
          contentType: 'delegation_results',
          sequence: 3,
        ),
        _message(
          'msg_answer',
          'assistant',
          'Research found that we ship tomorrow.',
          runId: 'run_continuation',
          sequence: 4,
        ),
      ]
      ..delegations = [_research];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    final card = find.byType(DelegationCard);
    expect(card, findsOneWidget);
    expect(
      find.text('🔬 Research · working on: Gather the notes'),
      findsOneWidget,
    );
    expect(find.text('🔬 Research completed'), findsOneWidget);
    expect(
      find.descendant(of: card, matching: find.text('Cancel')),
      findsNothing,
      reason: 'a finished task cannot be cancelled',
    );
    final parentAnswer = tester.getTopLeft(
      find.text('I asked Research to gather the notes.'),
    );
    final results = tester.getTopLeft(find.text('Results from the team'));
    expect(tester.getTopLeft(card).dy, greaterThan(parentAnswer.dy));
    expect(tester.getTopLeft(card).dy, lessThan(results.dy));

    // The join is a collapsed row, not raw text in the conversation.
    expect(find.textContaining('The notes say ship it.'), findsNothing);
    await tester.tap(find.text('Results from the team'));
    await tester.pumpAndSettle();
    expect(find.textContaining('The notes say ship it.'), findsOneWidget);

    // The answer that came from the results says who helped.
    expect(find.text('with 🔬 Research'), findsOneWidget);
    await tester.tap(find.text('with 🔬 Research'));
    await tester.pumpAndSettle();
    expect(find.byType(DelegationCard), findsNWidgets(2));
    await _close(tester, events);
  });

  testWidgets('the results stay above an answer that streamed in first', (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = _turn
      ..delegations = [_withState(_research, 'running')];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    final page = Completer<List<Message>>();
    api.nextPage = page.future;
    events.add(
      _delegationEvent('delegation.finished', 1, {
        'state': 'completed',
        'summary': '🔬 Research completed',
        'continuationRunId': 'run_continuation',
      }),
    );
    await tester.pump();
    // The continuation answers before the resync the task's end asked for.
    events.add(
      TuringEvent(
        eventId: 'evt_2',
        sessionId: 'sess_1',
        runId: 'run_continuation',
        traceId: 'trace_1',
        sequence: 2,
        type: 'message.delta',
        createdAt: DateTime.utc(2026, 10, 1),
        payload: const {
          'messageId': 'msg_answer',
          'delta': 'Research found that we ship tomorrow.',
        },
      ),
    );
    await tester.pump();
    page.complete([
      ..._turn,
      _message(
        'msg_join',
        'system',
        'Results of the 1 task(s) Turing delegated.',
        contentType: 'delegation_results',
        sequence: 3,
      ),
      _message(
        'msg_answer',
        'assistant',
        'Research found that we ship tomorrow.',
        runId: 'run_continuation',
        sequence: 4,
      ),
    ]);
    await tester.pumpAndSettle();

    expect(
      tester.getTopLeft(find.text('Results from the team')).dy,
      lessThan(
        tester
            .getTopLeft(find.text('Research found that we ship tomorrow.'))
            .dy,
      ),
    );
    await _close(tester, events);
  });

  testWidgets('replayed events cannot roll back what the read API returned', (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = _turn
      ..delegations = [_research]
      ..latestSequence = 10;
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    events.add(
      _delegationEvent('delegation.started', 5, {
        'state': 'queued',
        'summary': 'Asked 🔬 Research',
      }),
    );
    await tester.pump();

    expect(find.text('🔬 Research completed'), findsOneWidget);
    expect(find.text('Asked 🔬 Research'), findsNothing);
    await _close(tester, events);
  });

  testWidgets('without the read API, replayed events still build the card', (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = _turn
      ..latestSequence = 10;
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    events
      ..add(
        _delegationEvent('delegation.started', 5, {
          'state': 'queued',
          'summary': 'Asked 🔬 Research',
          'task': 'Gather the notes',
        }),
      )
      ..add(
        _delegationEvent('delegation.updated', 6, {
          'state': 'running',
          'summary': '🔬 Research is working',
        }),
      );
    await tester.pump();

    expect(find.text('🔬 Research is working'), findsOneWidget);
    expect(
      find.text('🔬 Research · working on: Gather the notes'),
      findsOneWidget,
    );
    await _close(tester, events);
  });

  testWidgets('a live task updates its card and ends with a resync', (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = _turn
      ..delegations = const [];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);
    final loads = api.listMessagesCalls;

    events.add(
      _delegationEvent('delegation.started', 1, {
        'state': 'queued',
        'summary': 'Asked 🔬 Research',
        'task': 'Gather the notes',
      }),
    );
    await tester.pump();
    expect(find.text('Asked 🔬 Research'), findsOneWidget);
    expect(find.text('Cancel'), findsOneWidget);

    events.add(
      _delegationEvent('delegation.updated', 2, {
        'state': 'running',
        'summary': '🔬 Research is working',
      }),
    );
    await tester.pump();
    expect(find.text('🔬 Research is working'), findsOneWidget);
    expect(api.listMessagesCalls, loads);

    events.add(
      _delegationEvent('delegation.updated', 3, {
        'state': 'cancelled',
        'summary': "🔬 Research was stopped because Turing's turn ended",
      }),
    );
    await tester.pump();
    expect(
      find.text("🔬 Research was stopped because Turing's turn ended"),
      findsOneWidget,
    );
    expect(find.text('Cancel'), findsNothing);
    expect(
      api.listMessagesCalls,
      loads + 1,
      reason: "a task's end can write a notice or the join; both are messages",
    );
    await _close(tester, events);
  });

  testWidgets("a card waits for its turn's answer to arrive by resync", (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = [_turn.first]
      ..delegations = const [];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    events.add(
      _delegationEvent('delegation.started', 1, {
        'state': 'queued',
        'summary': 'Asked 🔬 Research',
      }),
    );
    await tester.pump();
    expect(find.byType(DelegationCard), findsNothing);

    api.history = _turn;
    events.add(
      _delegationEvent('delegation.updated', 2, {
        'state': 'completed',
        'summary': '🔬 Research completed',
      }),
    );
    await tester.pumpAndSettle();
    expect(find.text('🔬 Research completed'), findsOneWidget);
    await _close(tester, events);
  });

  testWidgets("a card waits for its turn's answer to stream in", (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = [_turn.first]
      ..delegations = const [];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    events.add(
      _delegationEvent('delegation.started', 1, {
        'state': 'queued',
        'summary': 'Asked 🔬 Research',
      }),
    );
    await tester.pump();
    expect(find.byType(DelegationCard), findsNothing);

    events.add(
      TuringEvent(
        eventId: 'evt_2',
        sessionId: 'sess_1',
        runId: 'run_parent',
        traceId: 'trace_1',
        sequence: 2,
        type: 'message.delta',
        createdAt: DateTime.utc(2026, 10, 1),
        payload: const {'messageId': 'msg_parent', 'delta': 'Asking Research.'},
      ),
    );
    await tester.pump();
    expect(find.text('Asked 🔬 Research'), findsOneWidget);
    await _close(tester, events);
  });

  testWidgets("cards follow their turn's last answer as it streams on", (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = [_turn.first]
      ..delegations = const [];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    events
      ..add(_delta(1, 'Asking Research.'))
      ..add(
        _delegationEvent('delegation.started', 2, {
          'state': 'queued',
          'summary': 'Asked 🔬 Research',
        }),
      )
      ..add(
        TuringEvent(
          eventId: 'evt_3',
          sessionId: 'sess_1',
          runId: 'run_parent',
          traceId: 'trace_1',
          sequence: 3,
          type: 'tool.call.started',
          createdAt: DateTime.utc(2026, 10, 1),
          payload: const {
            'toolCallId': 'call_status',
            'toolName': 'system.time',
            'serverName': 'system',
          },
        ),
      )
      ..add(_delta(4, 'Research is on it.'));
    // The running tool card spins, so the screen never settles.
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 300));

    expect(
      tester.getTopLeft(find.byType(DelegationCard)).dy,
      greaterThan(tester.getTopLeft(find.text('Research is on it.')).dy),
    );
    await _close(tester, events);
  });

  testWidgets('a deleted conversation offers no Cancel', (tester) async {
    final api = _DelegationApi()
      ..history = _turn
      ..delegations = [_withState(_research, 'running')];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);
    expect(find.text('Cancel'), findsOneWidget);

    events.add(
      TuringEvent(
        eventId: 'evt_1',
        sessionId: 'sess_1',
        traceId: 'trace_1',
        sequence: 1,
        type: 'session.deleted',
        createdAt: DateTime.utc(2026, 10, 1),
        payload: const {},
      ),
    );
    await tester.pump();
    expect(find.text('Cancel'), findsNothing);
    await _close(tester, events);
  });

  testWidgets('a stop that lands between the startup reads still resyncs', (
    tester,
  ) async {
    const notice = "Turing's turn ended, so I stopped the work of 🔬 Research.";
    // History was read before the turn failed; the delegations after.
    final api = _DelegationApi()
      ..history = _turn
      ..delegations = [_withState(_research, 'cancelled')];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    api.history = [
      ..._turn,
      _message(
        'msg_notice',
        'system',
        notice,
        contentType: 'delegation_notice',
        sequence: 3,
      ),
    ];
    events.add(
      _delegationEvent('delegation.updated', 1, {
        'state': 'cancelled',
        'summary': "🔬 Research was stopped because Turing's turn ended",
      }),
    );
    await tester.pumpAndSettle();

    expect(find.widgetWithText(RunNoticeCard, notice), findsOneWidget);
    await _close(tester, events);
  });

  testWidgets('a cancellation the server could not take is reported', (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = _turn
      ..cancelAnswer = Future.value(
        const CancelRunReceipt(result: CancelRunResult.unavailable),
      )
      ..delegations = [_withState(_research, 'running')];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    await tester.tap(find.text('Cancel'));
    await tester.pumpAndSettle();

    expect(
      find.text("🔬 Research's task can no longer be cancelled."),
      findsOneWidget,
    );
    await _close(tester, events);
  });

  testWidgets("Cancel stops the specialist's run, not Turing's", (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = _turn
      ..delegations = [_withState(_research, 'running')];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    await tester.tap(find.text('Cancel'));
    await tester.pump();

    expect(api.cancels.single.sessionId, 'sess_child');
    expect(api.cancels.single.runId, 'run_child');
    expect(api.cancels.single.idempotencyKey, startsWith('cancel-'));
    await _close(tester, events);
  });

  testWidgets('Cancel waits for its answer and says when it failed', (
    tester,
  ) async {
    final pending = Completer<CancelRunReceipt>();
    final api = _DelegationApi()
      ..history = _turn
      ..cancelAnswer = pending.future
      ..delegations = [_withState(_research, 'running')];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    await tester.tap(find.text('Cancel'));
    await tester.pump();
    await tester.tap(find.text('Cancel'));
    await tester.pump();
    expect(api.cancels, hasLength(1), reason: 'one request while it is open');

    pending.completeError(
      const TuringApiException(code: 'unavailable', message: 'down'),
    );
    await tester.pumpAndSettle();
    expect(
      find.text("Could not cancel 🔬 Research's task. Try again."),
      findsOneWidget,
    );
    await tester.tap(find.text('Cancel'));
    await tester.pump();
    expect(api.cancels, hasLength(2), reason: 'it can be tried again');
    await _close(tester, events);
  });

  testWidgets("a task's approval is decided on its card", (tester) async {
    final api = _DelegationApi()
      ..history = _turn
      ..delegations = [
        _withState(_research, 'waiting_approval', approvalId: 'appr_child'),
      ];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    final relayed = find.descendant(
      of: find.byType(DelegationCard),
      matching: find.byType(ApprovalCard),
    );
    expect(relayed, findsOneWidget);
    expect(find.text('🔬 Research wants to use files.update'), findsOneWidget);

    await tester.tap(find.text('Approve'));
    await tester.pump();
    expect(api.approved.single.approvalId, 'appr_child');
    expect(api.approved.single.sessionId, 'sess_child');
    expect(find.byType(ApprovalCard), findsNothing);
    await _close(tester, events);
  });

  testWidgets('an approval whose card is not on screen waits in the dock', (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = _turn
      ..delegations = [
        Delegation(
          delegationId: 'dlg_old',
          parentRunId: 'run_older_than_the_page',
          childSessionId: 'sess_child',
          childRunId: 'run_child',
          displayName: 'Inbox',
          emoji: '📨',
          state: 'waiting_approval',
          approvalId: 'appr_child',
        ),
      ];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    expect(find.byType(DelegationCard), findsNothing);
    expect(find.text('📨 Inbox wants to use files.update'), findsOneWidget);
    await _close(tester, events);
  });

  testWidgets('an off-page approval that could not be read is retried', (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = _turn
      ..failFirstDetailsRead = true
      ..delegations = [
        Delegation(
          delegationId: 'dlg_old',
          parentRunId: 'run_older_than_the_page',
          childSessionId: 'sess_child',
          childRunId: 'run_child',
          displayName: 'Inbox',
          emoji: '📨',
          state: 'waiting_approval',
          approvalId: 'appr_child',
        ),
      ];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);
    expect(find.byType(DelegationCard), findsNothing);
    expect(
      find.text('The approval 📨 Inbox asked for could not be loaded.'),
      findsOneWidget,
    );

    await tester.tap(find.text('Load approval'));
    await tester.pumpAndSettle();

    expect(find.text('📨 Inbox wants to use files.update'), findsOneWidget);
    expect(find.textContaining('could not be loaded'), findsNothing);
    await _close(tester, events);
  });

  testWidgets('a relayed approval leaves once its task stops naming it', (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = _turn
      ..delegations = [
        _withState(_research, 'waiting_approval', approvalId: 'appr_child'),
      ];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);
    expect(find.byType(ApprovalCard), findsOneWidget);

    events.add(
      _delegationEvent('delegation.updated', 1, {
        'state': 'waiting_approval',
        'summary': '🔬 Research is waiting for your approval',
      }),
    );
    await tester.pump();

    expect(find.byType(ApprovalCard), findsNothing);
    await _close(tester, events);
  });

  testWidgets('an approval read overtaken by a newer update is dropped', (
    tester,
  ) async {
    final details = Completer<ApprovalDetails>();
    final api = _DelegationApi()
      ..history = _turn
      ..detailsGate = details.future
      ..delegations = [
        _withState(_research, 'waiting_approval', approvalId: 'appr_child'),
      ];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    events.add(
      _delegationEvent('delegation.updated', 1, {
        'state': 'running',
        'summary': '🔬 Research is working',
      }),
    );
    await tester.pump();
    details.complete(
      approvalDetails(
        approvalId: 'appr_child',
        sessionId: 'sess_child',
        runId: 'run_child',
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byType(ApprovalCard), findsNothing);
    await _close(tester, events);
  });

  testWidgets('an approval that could not be read can be read again', (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = _turn
      ..failFirstDetailsRead = true
      ..delegations = [
        _withState(_research, 'waiting_approval', approvalId: 'appr_child'),
      ];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);
    expect(find.byType(ApprovalCard), findsNothing);
    expect(
      find.text('The approval 🔬 Research asked for could not be loaded.'),
      findsOneWidget,
    );

    await tester.tap(find.text('Load approval'));
    await tester.pumpAndSettle();

    expect(find.byType(ApprovalCard), findsOneWidget);
    expect(find.textContaining('could not be loaded'), findsNothing);
    await _close(tester, events);
  });

  testWidgets("an approval from another conversation is not put on a card", (
    tester,
  ) async {
    for (final mismatch in ['session', 'run']) {
      final api = _DelegationApi()
        ..history = _turn
        ..detailsSessionId = mismatch == 'session' ? 'sess_other' : 'sess_child'
        ..detailsRunId = mismatch == 'run' ? 'run_other' : 'run_child'
        ..delegations = [
          _withState(_research, 'waiting_approval', approvalId: 'appr_child'),
        ];
      final events = StreamController<TuringEvent>(sync: true);
      await _pump(tester, api, events);

      expect(find.byType(ApprovalCard), findsNothing, reason: mismatch);
      await _close(tester, events);
    }
  });

  testWidgets('an approval no longer pending is not relayed', (tester) async {
    final api = _DelegationApi()
      ..history = _turn
      ..approvalStatus = 'approved'
      ..delegations = [
        _withState(_research, 'waiting_approval', approvalId: 'appr_child'),
      ];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    expect(find.byType(ApprovalCard), findsNothing);
    await _close(tester, events);
  });

  testWidgets('a stop notice reads as a status line', (tester) async {
    const notice = "Turing's turn ended, so I stopped the work of 🔬 Research.";
    final api = _DelegationApi()
      ..history = [
        ..._turn,
        _message(
          'msg_notice',
          'system',
          notice,
          contentType: 'delegation_notice',
          sequence: 3,
        ),
      ];
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    expect(find.widgetWithText(RunNoticeCard, notice), findsOneWidget);
    await _close(tester, events);
  });

  testWidgets("View work opens the specialist's transcript read-only", (
    tester,
  ) async {
    final api = _DelegationApi()
      ..history = _turn
      ..delegations = [_research]
      ..transcripts = {
        'sess_child': [
          _message(
            'msg_brief',
            'user',
            'BEGIN TURING_RETRIEVED_DELEGATION_BRIEF_x\nTask:\nGather the '
                'notes\nEND TURING_RETRIEVED_DELEGATION_BRIEF_x',
            contentType: 'delegation_brief',
            sequence: 1,
          ),
          _message(
            'msg_result',
            'assistant',
            'The notes say ship it.',
            runId: 'run_child',
            sequence: 2,
          ),
        ],
      };
    final events = StreamController<TuringEvent>(sync: true);
    await _pump(tester, api, events);

    await tester.tap(find.text('View work'));
    await tester.pumpAndSettle();

    expect(find.text('Gather the notes'), findsOneWidget);
    expect(find.text('The notes say ship it.'), findsOneWidget);
    expect(find.textContaining('BEGIN TURING'), findsNothing);
    expect(find.byType(TextField), findsNothing);
    await _close(tester, events);
  });
}

Delegation _withState(
  Delegation delegation,
  String state, {
  String approvalId = '',
}) => Delegation(
  delegationId: delegation.delegationId,
  parentRunId: delegation.parentRunId,
  childSessionId: delegation.childSessionId,
  childRunId: delegation.childRunId,
  displayName: delegation.displayName,
  emoji: delegation.emoji,
  task: delegation.task,
  state: state,
  approvalId: approvalId,
  summary: '${delegation.specialist} is $state',
);

Future<void> _pump(
  WidgetTester tester,
  _DelegationApi api,
  StreamController<TuringEvent> events,
) async {
  await tester.pumpWidget(
    MaterialApp(
      localizationsDelegates: AppLocalizations.localizationsDelegates,
      supportedLocales: AppLocalizations.supportedLocales,
      home: ChatScreen(
        sessionId: 'sess_1',
        apiClient: api,
        eventSource: _EventSource(events.stream),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

Future<void> _close(
  WidgetTester tester,
  StreamController<TuringEvent> events,
) async {
  await tester.pumpWidget(const SizedBox.shrink());
  unawaited(events.close());
}

Message _message(
  String id,
  String role,
  String content, {
  String? runId,
  String contentType = 'text',
  required int sequence,
}) => Message(
  messageId: id,
  runId: runId,
  role: role,
  content: content,
  contentType: contentType,
  sequence: sequence,
  createdAt: DateTime.utc(2026, 10, 1, 0, 0, sequence),
);

TuringEvent _delta(int sequence, String delta) => TuringEvent(
  eventId: 'evt_$sequence',
  sessionId: 'sess_1',
  runId: 'run_parent',
  traceId: 'trace_1',
  sequence: sequence,
  type: 'message.delta',
  createdAt: DateTime.utc(2026, 10, 1),
  payload: {'messageId': 'msg_parent', 'delta': delta},
);

TuringEvent _delegationEvent(
  String type,
  int sequence,
  Map<String, dynamic> payload,
) => TuringEvent(
  eventId: 'evt_$sequence',
  sessionId: 'sess_1',
  runId: 'run_parent',
  traceId: 'trace_1',
  sequence: sequence,
  type: type,
  createdAt: DateTime.utc(2026, 10, 1),
  payload: {
    'delegationId': 'dlg_1',
    'parentRunId': 'run_parent',
    'childRunId': 'run_child',
    'childSessionId': 'sess_child',
    'profileId': 'research',
    'displayName': 'Research',
    'emoji': '🔬',
    ...payload,
  },
);

class _EventSource implements TuringEventSource {
  _EventSource(this._events);

  final Stream<TuringEvent> _events;

  @override
  Stream<TuringEvent> connect({required String sessionId, int? lastSequence}) =>
      _events;

  @override
  void close() {}
}

class _DelegationApi extends TuringApi
    with
        NoSkillsApi,
        NoExternalAgentsApi,
        NoIntegrationsApi,
        NoSessionLifecycleApi,
        NoAutomationsApi,
        NoTelemetryApi {
  List<Message> history = const [];
  Map<String, List<Message>> transcripts = const {};

  /// When set, the next history read answers with it.
  Future<List<Message>>? nextPage;

  /// Null answers like a server without the read API.
  List<Delegation>? delegations;
  int latestSequence = 0;
  int listMessagesCalls = 0;
  String approvalStatus = 'pending';
  final cancels = <({String sessionId, String runId, String idempotencyKey})>[];
  final approved = <ApprovalDetails>[];

  /// When set, the first `getApprovalDetails` answers with it.
  Future<ApprovalDetails>? detailsGate;

  /// When set, the first `getApprovalDetails` fails.
  bool failFirstDetailsRead = false;

  /// The conversation and run the approval details say they belong to.
  String detailsSessionId = 'sess_child';
  String detailsRunId = 'run_child';

  final prepared = <String>[];
  final decided = <({String delegationId, RemoteEgressConsent? consent})>[];
  Object? prepareError;

  /// When set, `decideDelegationEgress` answers with it once.
  Future<Delegation>? decideAnswer;
  Object? decideError;

  /// When set, `cancelRun` answers with it once.
  Future<CancelRunReceipt>? cancelAnswer;

  @override
  Future<List<Message>> listMessages({
    required String sessionId,
    int limit = 50,
    String? before,
  }) async {
    if (sessionId != 'sess_1') return transcripts[sessionId] ?? const [];
    listMessagesCalls++;
    final page = nextPage;
    nextPage = null;
    return page ?? history;
  }

  @override
  Future<TuringEventPage> listEvents({
    required String sessionId,
    int? after,
    int limit = 500,
  }) async => TuringEventPage(events: const [], latestSequence: latestSequence);

  @override
  Future<List<Delegation>> listDelegations({
    required String parentSessionId,
  }) async {
    final known = delegations;
    if (known == null) {
      throw const TuringApiException(
        code: 'unimplemented',
        message: 'no delegations here',
      );
    }
    return known;
  }

  @override
  Future<ApprovalDetails> getApprovalDetails(
    String approvalId, {
    bool refreshPreview = false,
  }) {
    if (failFirstDetailsRead) {
      failFirstDetailsRead = false;
      return Future.error(
        const TuringApiException(code: 'unavailable', message: 'down'),
      );
    }
    final gate = detailsGate;
    detailsGate = null;
    return gate ??
        Future.value(
          approvalDetails(
            approvalId: approvalId,
            sessionId: detailsSessionId,
            runId: detailsRunId,
            status: approvalStatus,
          ),
        );
  }

  @override
  Future<Map<String, dynamic>> approveReviewedApproval(
    ApprovalDetails details, {
    String? comment,
  }) async {
    approved.add(details);
    return {'approvalId': details.approvalId, 'status': 'approved'};
  }

  @override
  Future<Map<String, dynamic>> denyApproval(
    String approvalId, {
    String? reason,
  }) async => {'approvalId': approvalId, 'status': 'denied'};

  @override
  Future<RemoteEgressDisclosure> prepareDelegationEgress({
    required String delegationId,
  }) async {
    prepared.add(delegationId);
    final error = prepareError;
    if (error != null) throw error;
    return RemoteEgressDisclosure(
      challenge: 'signed-challenge',
      provider: 'ollama',
      model: 'llama3.2',
      endpoint: '',
      endpointHost: '',
      dataCategories: const [
        EgressDataCategory.toolArguments,
        EgressDataCategory.toolResults,
      ],
      expiresAt: DateTime.now().add(const Duration(minutes: 5)),
      selectedTools: const ['integrations/github.get_issue'],
    );
  }

  @override
  Future<Delegation> decideDelegationEgress({
    required String delegationId,
    RemoteEgressConsent? consent,
  }) {
    decided.add((delegationId: delegationId, consent: consent));
    final error = decideError;
    if (error != null) return Future.error(error);
    final answer = decideAnswer;
    decideAnswer = null;
    return answer ?? Future.value(_withState(_research, 'queued'));
  }

  @override
  Future<CancelRunReceipt> cancelRun({
    required String sessionId,
    required String runId,
    required String idempotencyKey,
  }) async {
    cancels.add((
      sessionId: sessionId,
      runId: runId,
      idempotencyKey: idempotencyKey,
    ));
    final answer = cancelAnswer;
    cancelAnswer = null;
    return answer ?? const CancelRunReceipt(result: CancelRunResult.accepted);
  }

  @override
  Future<Map<String, dynamic>> createSession({String? title}) async => {
    'sessionId': 'sess_1',
  };

  @override
  Future<Session> getSession({required String sessionId}) =>
      throw UnimplementedError();

  @override
  Future<List<SearchHit>> searchMessages({
    required String query,
    int limit = 50,
  }) async => const [];

  @override
  Future<Map<String, dynamic>> sendMessage({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
    String? idempotencyKey,
  }) => throw UnimplementedError();

  @override
  Future<List<ToolDescriptor>> listTools() async => const [];

  @override
  Future<List<AgentDescriptor>> listAgents() async => const [];

  @override
  Future<List<Session>> listSessions({int limit = 50, String? after}) async =>
      const [];

  @override
  Future<SessionDeletionReceipt> deleteSession({
    required String sessionId,
  }) async => const SessionDeletionReceipt.completed();
}
