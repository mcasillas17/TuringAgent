import 'dart:async';

import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:material_ui/material_ui.dart';
import 'package:turing_flutter_app/features/approvals/approval_card.dart';
import 'package:turing_flutter_app/features/chat/delegation_card.dart';
import 'package:turing_flutter_app/l10n/app_localizations_delegates.dart';
import 'package:turing_flutter_app/l10n/generated/app_localizations.dart';
import 'package:turing_flutter_app/models/agent_descriptor.dart';
import 'package:turing_flutter_app/models/approval.dart';
import 'package:turing_flutter_app/models/attention.dart';
import 'package:turing_flutter_app/models/delegation.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/models/remote_egress.dart';
import 'package:turing_flutter_app/models/search_hit.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/session_deletion.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';
import 'package:turing_flutter_app/models/turing_event.dart';
import 'package:turing_flutter_app/networking/api_client.dart';
import 'package:turing_flutter_app/networking/auth_storage.dart';
import 'package:turing_flutter_app/networking/event_source.dart';
import 'package:turing_flutter_app/ui/shell/responsive_shell.dart';

import '../support/approval_details.dart';
import '../support/no_automations_api.dart';
import '../support/no_external_agents_api.dart';
import '../support/no_integrations_api.dart';
import '../support/no_mcp_registry_api.dart';
import '../support/no_memory_api.dart';
import '../support/no_remote_egress_api.dart';
import '../support/no_session_lifecycle_api.dart';
import '../support/no_skills_api.dart';
import '../support/no_team_api.dart';
import '../support/no_telemetry_api.dart';

const Size _desktop = Size(1400, 900);
const Size _phone = Size(420, 900);
const String _needsYou = 'Needs you';

void main() {
  group('the badge', () {
    testWidgets("equals the first page's row count", (tester) async {
      final api = _QueueApi()
        ..rows = [_approvalRow(1), _approvalRow(2), _consentRow()];
      await _pumpShell(tester, api);

      expect(find.text(_needsYou), findsOneWidget);
      expect(_badgeText(tester), '3');
      expect(api.attentionCursors, [null], reason: 'no second count query');
    });

    testWidgets('reads "100+" with 101 obligations', (tester) async {
      final api = _QueueApi()
        ..rows = [for (var i = 1; i <= 101; i++) _approvalRow(i)];
      await _pumpShell(tester, api);

      expect(_badgeText(tester), '100+');
      expect(api.attentionCursors, [null]);
    });

    testWidgets('the empty state takes no space', (tester) async {
      final api = _QueueApi();
      await _pumpShell(tester, api);

      expect(api.attentionCursors, [null], reason: 'the queue was read');
      expect(find.text(_needsYou), findsNothing);
      expect(find.byKey(const ValueKey('needs-you-badge')), findsNothing);
    });

    testWidgets('a server without the queue offers no destination', (
      tester,
    ) async {
      final api = _QueueApi()
        ..attentionError = const TuringApiException(
          code: 'unimplemented',
          message: 'unknown service turing.v1.AttentionService',
        );
      await _pumpShell(tester, api);

      expect(find.text(_needsYou), findsNothing);
    });

    testWidgets('a failed read is not shown as nothing needing you', (
      tester,
    ) async {
      final api = _QueueApi()
        ..attentionError = const TuringApiException(
          code: 'unavailable',
          message: 'backend down',
        );
      await _pumpShell(tester, api);

      expect(find.text(_needsYou), findsOneWidget);
      expect(_badgeText(tester), '!');

      await _openQueue(tester);
      expect(find.text('Could not check what needs you.'), findsOneWidget);
    });

    testWidgets('marks the menu button on a phone, where the rail is hidden', (
      tester,
    ) async {
      final api = _QueueApi()..rows = [_approvalRow(1), _approvalRow(2)];
      await _pumpShell(tester, api, size: _phone);

      expect(
        find.descendant(
          of: find.byType(AppBar),
          matching: find.byKey(const ValueKey('needs-you-menu-badge')),
        ),
        findsOneWidget,
      );
      expect(find.byTooltip('Open navigation menu'), findsOneWidget);
      await tester.tap(find.byTooltip('Open navigation menu'));
      await tester.pumpAndSettle();
      expect(find.text(_needsYou), findsOneWidget);
    });
  });

  group('refetching', () {
    testWidgets(
      'follows the attention.changed hint, at most once a second, and '
      'never polls',
      (tester) async {
        final updates = _FakeSessionUpdateSource();
        final api = _QueueApi()..rows = [_approvalRow(1)];
        await _pumpShell(tester, api, updates: updates);
        expect(api.attentionCursors, hasLength(1));

        await tester.pump(const Duration(seconds: 2));
        expect(api.attentionCursors, hasLength(1), reason: 'no polling');

        api.rows = [_approvalRow(1), _approvalRow(2)];
        updates
          ..add(_hint())
          ..add(_hint())
          ..add(_hint());
        await tester.pump();
        expect(api.attentionCursors, hasLength(2));
        await tester.pump();
        expect(_badgeText(tester), '2');

        await tester.pump(const Duration(milliseconds: 900));
        expect(
          api.attentionCursors,
          hasLength(2),
          reason: 'the hints after the first wait out the second',
        );
        await tester.pump(const Duration(milliseconds: 200));
        expect(api.attentionCursors, hasLength(3));

        await tester.pump(const Duration(seconds: 10));
        expect(api.attentionCursors, hasLength(3), reason: 'no polling');
      },
    );

    testWidgets('reads again when the app resumes', (tester) async {
      final api = _QueueApi()..rows = [_approvalRow(1)];
      await _pumpShell(tester, api);
      await tester.pump(const Duration(seconds: 2));
      expect(api.attentionCursors, hasLength(1));

      api.rows = const [];
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.inactive);
      tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
      await tester.pumpAndSettle();

      expect(api.attentionCursors, hasLength(2));
      expect(find.text(_needsYou), findsNothing);
    });
  });

  group('each stage', () {
    testWidgets('an approval offers Review, never Dismiss', (tester) async {
      final api = _QueueApi()..rows = [_approvalRow(1)];
      await _pumpShell(tester, api);
      await _openQueue(tester);

      expect(find.text('Approval needed'), findsOneWidget);
      expect(find.text('Chat 1'), findsOneWidget);
      expect(find.textContaining('2026-11-20 09:01'), findsOneWidget);
      expect(find.text('Review'), findsOneWidget);
      expect(find.text('Dismiss'), findsNothing);
    });

    testWidgets("a delegated task's approval says who is waiting", (
      tester,
    ) async {
      final api = _QueueApi()
        ..rows = [_approvalRow(1, delegationId: 'del_1', title: 'Parent')];
      await _pumpShell(tester, api);
      await _openQueue(tester);

      expect(find.text('A delegated task needs approval'), findsOneWidget);
      expect(find.text('Parent'), findsOneWidget);
      expect(find.text('Dismiss'), findsNothing);
    });

    testWidgets('a consent offers Review, never Dismiss', (tester) async {
      final api = _QueueApi()..rows = [_consentRow()];
      await _pumpShell(tester, api);
      await _openQueue(tester);

      expect(find.text('Consent needed'), findsOneWidget);
      expect(find.text('Research chat'), findsOneWidget);
      expect(find.text('Review'), findsOneWidget);
      expect(find.text('Dismiss'), findsNothing);
    });

    testWidgets('a blocked automation offers Dismiss and its conversation', (
      tester,
    ) async {
      final api = _QueueApi()..rows = [_automationRow()];
      await _pumpShell(tester, api);
      await _openQueue(tester);

      expect(find.text('Automation blocked'), findsOneWidget);
      expect(find.text('Morning digest'), findsOneWidget);
      expect(
        find.textContaining('a tool its allowlist does not include'),
        findsOneWidget,
      );
      expect(find.text('Dismiss'), findsOneWidget);
      expect(find.text('Open chat'), findsOneWidget);
      expect(find.text('Review'), findsNothing);
    });

    testWidgets('a deletion offers Retry deletion, never Dismiss', (
      tester,
    ) async {
      final api = _QueueApi()..rows = [_deletionRow()];
      await _pumpShell(tester, api);
      await _openQueue(tester);

      expect(find.text('Deletion did not finish'), findsOneWidget);
      expect(find.textContaining('artifact_cleanup_failed'), findsOneWidget);
      expect(find.text('Retry deletion'), findsOneWidget);
      expect(find.text('Dismiss'), findsNothing);
      expect(find.text('Open chat'), findsNothing);
    });

    testWidgets('a proposal is listed with nothing it cannot do yet', (
      tester,
    ) async {
      final api = _QueueApi()
        ..rows = [
          AttentionRow(
            stage: AttentionStage.proposal,
            kind: AttentionSourceKind.proposal,
            since: DateTime(2026, 11, 20, 9, 4),
            sessionId: 'sess_watch',
            sessionTitle: 'Watch chat',
          ),
        ];
      await _pumpShell(tester, api);
      await _openQueue(tester);

      expect(find.text('Watch proposal'), findsOneWidget);
      expect(find.text('Watch chat'), findsOneWidget);
      expect(find.text('Open chat'), findsOneWidget);
      expect(find.text('Dismiss'), findsNothing);
      expect(find.text('Review'), findsNothing);
    });

    testWidgets('a suspended watch cannot be dismissed', (tester) async {
      final api = _QueueApi()
        ..rows = [
          AttentionRow(
            stage: AttentionStage.blocked,
            kind: AttentionSourceKind.watch,
            since: DateTime(2026, 11, 20, 9, 4),
            sessionId: 'sess_watch',
            sessionTitle: 'Watch chat',
            cause: 'connection_unavailable',
          ),
        ];
      await _pumpShell(tester, api);
      await _openQueue(tester);

      expect(find.text('Watch suspended'), findsOneWidget);
      expect(find.text('Dismiss'), findsNothing);
    });

    testWidgets('unavailable is listed, never dropped, and offers no action', (
      tester,
    ) async {
      final api = _QueueApi()
        ..rows = [
          AttentionRow(
            stage: AttentionStage.unavailable,
            kind: AttentionSourceKind.approval,
            since: DateTime(2026, 11, 20, 9, 5),
            sessionId: 'sess_odd',
            sessionTitle: 'Odd chat',
            approvalId: 'appr_odd',
            runId: 'run_odd',
          ),
        ];
      await _pumpShell(tester, api);
      await _openQueue(tester);

      expect(find.text("Turing couldn't read this item"), findsOneWidget);
      expect(find.text('Odd chat'), findsOneWidget);
      expect(_badgeText(tester), '1');
      expect(find.text('Review'), findsNothing);
      expect(find.text('Dismiss'), findsNothing);
      expect(find.text('Open chat'), findsOneWidget);
    });

    testWidgets('two rows this build cannot tell apart are both listed', (
      tester,
    ) async {
      final proposal = AttentionRow(
        stage: AttentionStage.proposal,
        kind: AttentionSourceKind.proposal,
        since: DateTime(2026, 11, 20, 9, 4),
        sessionId: 'sess_watch',
        sessionTitle: 'Watch chat',
      );
      final api = _QueueApi()..rows = [proposal, proposal];
      await _pumpShell(tester, api);
      await _openQueue(tester);

      expect(tester.takeException(), isNull);
      expect(find.text('Watch proposal'), findsNWidgets(2));
      expect(_badgeText(tester), '2');
    });

    testWidgets('titles and names render as plain text', (tester) async {
      const title = '**Approve me** [now](https://evil.example)';
      const name = '# Urgent <b>digest</b>';
      final api = _QueueApi()
        ..rows = [_approvalRow(1, title: title), _automationRow(name: name)];
      await _pumpShell(tester, api);
      await _openQueue(tester);

      expect(find.text(title), findsOneWidget);
      expect(find.text(name), findsOneWidget);
      expect(find.byType(MarkdownBody), findsNothing);
    });
  });

  group('expanding', () {
    testWidgets(
      'opening 100 approvals reads no details; expanding one reads exactly '
      'one and collapses any other',
      (tester) async {
        final api = _QueueApi()
          ..rows = [for (var i = 1; i <= 100; i++) _approvalRow(i)];
        await _pumpShell(tester, api);
        await _openQueue(tester);

        expect(find.text('Chat 1'), findsOneWidget);
        expect(api.detailReads, isEmpty);
        expect(find.byType(ApprovalCard), findsNothing);

        await tester.tap(_reviewFor('Chat 1'));
        await tester.pumpAndSettle();
        expect(api.detailReads, ['appr_1']);
        expect(find.byType(ApprovalCard), findsOneWidget);

        await tester.tap(_reviewFor('Chat 2'));
        await tester.pumpAndSettle();
        expect(api.detailReads, ['appr_1', 'appr_2']);
        expect(find.byType(ApprovalCard), findsOneWidget);
        final card = tester.widget<ApprovalCard>(find.byType(ApprovalCard));
        expect(card.approvalId, 'appr_2');

        await tester.tap(find.text('Hide'));
        await tester.pumpAndSettle();
        expect(find.byType(ApprovalCard), findsNothing);
        expect(api.detailReads, ['appr_1', 'appr_2']);
      },
    );

    testWidgets('a refetch that still lists the row keeps its review open', (
      tester,
    ) async {
      final updates = _FakeSessionUpdateSource();
      final api = _QueueApi()..rows = [_approvalRow(1), _approvalRow(2)];
      await _pumpShell(tester, api, updates: updates);
      await _openQueue(tester);
      await tester.tap(_reviewFor('Chat 2'));
      await tester.pumpAndSettle();
      expect(api.detailReads, ['appr_2']);

      await tester.pump(const Duration(seconds: 2));
      api.rows = [_approvalRow(0), _approvalRow(2)];
      updates.add(_hint());
      await tester.pumpAndSettle();

      expect(find.text('Chat 0'), findsOneWidget);
      expect(find.byType(ApprovalCard), findsOneWidget);
      expect(api.detailReads, ['appr_2'], reason: 'the open card stayed put');
    });

    testWidgets('approving from the list resolves the same approval id the '
        'chat would', (tester) async {
      final api = _QueueApi()..rows = [_approvalRow(7)];
      await _pumpShell(tester, api);
      await _openQueue(tester);
      await tester.tap(_reviewFor('Chat 7'));
      await tester.pumpAndSettle();

      // The chat approves the details GetApprovalDetails returned for the
      // approval id its approval.requested event named; the list's row names
      // the same id.
      api.rows = const [];
      await tester.tap(find.text('Approve'));
      await tester.pumpAndSettle();

      expect(api.approved, hasLength(1));
      final approved = api.approved.single;
      expect(approved.approvalId, 'appr_7');
      expect(approved.runId, 'run_7');
      expect(approved.previewHash, 'sha256:preview-appr_7');
      expect(api.detailReads, ['appr_7']);

      await tester.pump(const Duration(seconds: 1));
      await tester.pumpAndSettle();
      expect(find.text('Chat 7'), findsNothing);
    });

    testWidgets('denying from the list sends the row approval id', (
      tester,
    ) async {
      final api = _QueueApi()..rows = [_approvalRow(3)];
      await _pumpShell(tester, api);
      await _openQueue(tester);
      await tester.tap(_reviewFor('Chat 3'));
      await tester.pumpAndSettle();

      await tester.tap(find.text('Deny'));
      await tester.pumpAndSettle();

      expect(api.denied, ['appr_3']);
    });

    testWidgets(
      "an expired row's action is refused and the row disappears on refetch",
      (tester) async {
        final api = _QueueApi()..rows = [_approvalRow(1)];
        await _pumpShell(tester, api);
        await _openQueue(tester);
        await tester.tap(_reviewFor('Chat 1'));
        await tester.pumpAndSettle();

        // It expired on the server after the list and the review were read.
        api
          ..rows = const []
          ..approveError = const TuringApiException(
            code: 'failed_precondition',
            message: 'approval has expired',
          );
        await tester.tap(find.text('Approve'));
        await tester.pump();

        expect(api.approveAttempts, 1);
        expect(find.textContaining('approval has expired'), findsOneWidget);

        await tester.pump(const Duration(seconds: 1));
        await tester.pumpAndSettle();
        expect(find.text('Chat 1'), findsNothing);
        expect(find.byType(ApprovalCard), findsNothing);
        expect(api.approveAttempts, 1, reason: 'the refusal is not replayed');
      },
    );

    testWidgets('a review whose approval is gone asks for a fresh list', (
      tester,
    ) async {
      final api = _QueueApi()
        ..rows = [_approvalRow(1)]
        ..detailsError = const TuringApiException(
          code: 'not_found',
          message: 'approval not found',
        );
      await _pumpShell(tester, api);
      await _openQueue(tester);
      final reads = api.attentionCursors.length;

      api.rows = const [];
      await tester.tap(_reviewFor('Chat 1'));
      await tester.pump();
      await tester.pump();
      expect(find.text('This approval could not be loaded.'), findsOneWidget);

      await tester.pump(const Duration(seconds: 1));
      await tester.pumpAndSettle();
      expect(api.attentionCursors.length, greaterThan(reads));
      expect(find.text('Chat 1'), findsNothing);
    });

    testWidgets('a consent mounts its task card and the consent dialog', (
      tester,
    ) async {
      final api = _QueueApi()
        ..rows = [_consentRow()]
        ..delegation = _waitingDelegation();
      await _pumpShell(tester, api);
      await _openQueue(tester);
      expect(api.delegationReads, isEmpty);

      await tester.tap(_reviewFor('Research chat'));
      await tester.pumpAndSettle();
      expect(api.delegationReads, ['del_1']);
      expect(find.byType(DelegationCard), findsOneWidget);

      await tester.tap(find.text('Allow…'));
      await tester.pumpAndSettle();
      expect(api.prepared, ['del_1']);
      await tester.tap(find.widgetWithText(FilledButton, 'Allow'));
      await tester.pumpAndSettle();

      expect(api.decided, hasLength(1));
      expect(api.decided.single.delegationId, 'del_1');
      expect(api.decided.single.consent?.challenge, 'signed-challenge');
    });

    testWidgets('Run without declines through the same decision', (
      tester,
    ) async {
      final api = _QueueApi()
        ..rows = [_consentRow()]
        ..delegation = _waitingDelegation();
      await _pumpShell(tester, api);
      await _openQueue(tester);
      await tester.tap(_reviewFor('Research chat'));
      await tester.pumpAndSettle();

      await tester.tap(find.text('Run without'));
      await tester.pumpAndSettle();

      expect(api.decided.single.delegationId, 'del_1');
      expect(api.decided.single.consent, isNull);
    });
  });

  group('acting on a row', () {
    testWidgets('Dismiss sends the occurrence key unchanged and the badge '
        'drops by one', (tester) async {
      final api = _QueueApi()..rows = [_approvalRow(1), _automationRow()];
      await _pumpShell(tester, api);
      expect(_badgeText(tester), '2');
      await _openQueue(tester);

      api.rows = [_approvalRow(1)];
      await tester.tap(find.text('Dismiss'));
      await tester.pump();
      expect(api.dismissed, [
        (automationId: 'auto_1', occurrenceKey: 'audit_01J9Z'),
      ]);

      await tester.pump(const Duration(seconds: 1));
      await tester.pumpAndSettle();
      expect(find.text('Morning digest'), findsNothing);
      expect(_badgeText(tester), '1');
    });

    testWidgets('a stale Dismiss is refused and the newer failure stays', (
      tester,
    ) async {
      final api = _QueueApi()
        ..rows = [_automationRow()]
        ..dismissError = const TuringApiException(
          code: 'failed_precondition',
          message: 'the automation has a newer occurrence',
        );
      await _pumpShell(tester, api);
      await _openQueue(tester);

      api.rows = [_automationRow(occurrenceKey: 'run_newer')];
      await tester.tap(find.text('Dismiss'));
      await tester.pump();
      expect(find.textContaining('newer occurrence'), findsOneWidget);

      await tester.pump(const Duration(seconds: 1));
      await tester.pumpAndSettle();
      expect(find.text('Morning digest'), findsOneWidget);

      api.dismissError = null;
      await tester.tap(find.text('Dismiss'));
      await tester.pump();
      expect(api.dismissed.last.occurrenceKey, 'run_newer');
    });

    testWidgets('Retry deletion advances the conversation deletion', (
      tester,
    ) async {
      final api = _QueueApi()..rows = [_deletionRow()];
      await _pumpShell(tester, api);
      await _openQueue(tester);

      api.rows = const [];
      await tester.tap(find.text('Retry deletion'));
      await tester.pump();
      expect(api.deleted, ['sess_gone']);

      await tester.pump(const Duration(seconds: 1));
      await tester.pumpAndSettle();
      expect(find.text('Deletion did not finish'), findsNothing);
    });

    testWidgets('Load more reads the next page with its cursor', (
      tester,
    ) async {
      final api = _QueueApi()
        ..rows = [for (var i = 1; i <= 101; i++) _approvalRow(i)];
      await _pumpShell(tester, api);
      await _openQueue(tester);

      await tester.scrollUntilVisible(
        find.text('Load more'),
        600,
        scrollable: find.byType(Scrollable).last,
      );
      await tester.tap(find.text('Load more'));
      await tester.pumpAndSettle();

      expect(api.attentionCursors.last, '100');
      await tester.scrollUntilVisible(
        find.text('Chat 101'),
        600,
        scrollable: find.byType(Scrollable).last,
      );
      expect(find.text('Chat 101'), findsOneWidget);
      expect(find.text('Load more'), findsNothing);
      expect(_badgeText(tester), '100+', reason: 'the badge is page one');
    });

    testWidgets('Open chat goes to the row conversation', (tester) async {
      final api = _QueueApi()..rows = [_automationRow()];
      await _pumpShell(tester, api);
      await _openQueue(tester);

      await tester.tap(find.text('Open chat'));
      await tester.pumpAndSettle();

      expect(api.messageReads, ['sess_auto']);
    });
  });
}

AttentionRow _approvalRow(int i, {String? title, String delegationId = ''}) =>
    AttentionRow(
      stage: AttentionStage.approval,
      kind: AttentionSourceKind.approval,
      since: DateTime(2026, 11, 20, 9, 1),
      sessionId: 'sess_$i',
      sessionTitle: title ?? 'Chat $i',
      approvalId: 'appr_$i',
      runId: 'run_$i',
      delegationId: delegationId,
    );

AttentionRow _consentRow() => AttentionRow(
  stage: AttentionStage.consent,
  kind: AttentionSourceKind.delegation,
  since: DateTime(2026, 11, 20, 9, 2),
  sessionId: 'sess_parent',
  sessionTitle: 'Research chat',
  runId: 'run_child',
  delegationId: 'del_1',
);

AttentionRow _automationRow({
  String name = 'Morning digest',
  String occurrenceKey = 'audit_01J9Z',
}) => AttentionRow(
  stage: AttentionStage.blocked,
  kind: AttentionSourceKind.automation,
  since: DateTime(2026, 11, 20, 9, 3),
  sessionId: 'sess_auto',
  sessionTitle: 'Digest chat',
  runId: 'run_auto',
  automationId: 'auto_1',
  automationName: name,
  occurrenceKey: occurrenceKey,
  cause: 'automation_tool_not_allowlisted',
);

AttentionRow _deletionRow() => AttentionRow(
  stage: AttentionStage.deletion,
  kind: AttentionSourceKind.deletion,
  since: DateTime(2026, 11, 20, 9, 4),
  sessionId: 'sess_gone',
  cause: 'artifact_cleanup_failed',
);

Delegation _waitingDelegation() => const Delegation(
  delegationId: 'del_1',
  parentRunId: 'run_parent',
  childSessionId: 'sess_child',
  childRunId: 'run_child',
  displayName: 'Research',
  emoji: '🔬',
  task: 'Find the open issues',
  state: 'waiting_consent',
  egressTools: ['integrations/github.get_issue'],
  egressConsent: 'pending',
);

TuringEvent _hint() => TuringEvent(
  eventId: '',
  sessionId: '',
  traceId: '',
  sequence: 0,
  type: 'attention.changed',
  createdAt: DateTime.utc(2026, 11, 20),
  payload: const {},
);

String? _badgeText(WidgetTester tester) {
  final badge = find.byKey(const ValueKey('needs-you-badge'));
  if (badge.evaluate().isEmpty) return null;
  return tester
      .widget<Text>(find.descendant(of: badge, matching: find.byType(Text)))
      .data;
}

Finder _reviewFor(String title) => find.descendant(
  of: find.ancestor(
    of: find.text(title),
    matching: find.byKey(const ValueKey('attention-row')),
  ),
  matching: find.text('Review'),
);

Future<void> _openQueue(WidgetTester tester) async {
  await tester.tap(find.text(_needsYou));
  await tester.pumpAndSettle();
}

Future<void> _pumpShell(
  WidgetTester tester,
  _QueueApi api, {
  Size size = _desktop,
  _FakeSessionUpdateSource? updates,
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);

  await tester.pumpWidget(
    MaterialApp(
      localizationsDelegates: appLocalizationsDelegates,
      supportedLocales: AppLocalizations.supportedLocales,
      home: ResponsiveShell(
        apiClient: api,
        eventSourceFactory: _FakeEventSource.new,
        sessionUpdateSourceFactory: updates == null ? null : () => updates,
        authStorage: _FakeAuthStorage(),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

class _QueueApi extends TuringApi
    with
        NoIntegrationsApi,
        NoAutomationsApi,
        NoTelemetryApi,
        NoSkillsApi,
        NoTeamApi,
        NoMcpRegistryApi,
        NoMemoryApi,
        NoExternalAgentsApi,
        NoRemoteEgressApi,
        NoSessionLifecycleApi {
  List<AttentionRow> rows = const [];
  Object? attentionError;
  final List<String?> attentionCursors = [];

  final List<String> detailReads = [];
  Object? detailsError;
  int approveAttempts = 0;
  Object? approveError;
  final List<ApprovalDetails> approved = [];
  final List<String> denied = [];

  final List<({String automationId, String occurrenceKey})> dismissed = [];
  Object? dismissError;

  Delegation? delegation;
  final List<String> delegationReads = [];
  final List<String> prepared = [];
  final List<({String delegationId, RemoteEgressConsent? consent})> decided =
      [];

  final List<String> deleted = [];
  final List<String> messageReads = [];

  @override
  Future<AttentionPage> listAttention({String? cursor, int limit = 100}) async {
    attentionCursors.add(cursor);
    final error = attentionError;
    if (error != null) throw error;
    final offset = cursor == null ? 0 : int.parse(cursor);
    final page = rows.skip(offset).take(limit).toList();
    final next = offset + page.length;
    return AttentionPage(
      rows: page,
      nextCursor: next < rows.length ? '$next' : null,
    );
  }

  @override
  Future<void> dismissAttention({
    required String automationId,
    required String occurrenceKey,
  }) async {
    dismissed.add((automationId: automationId, occurrenceKey: occurrenceKey));
    final error = dismissError;
    if (error != null) throw error;
  }

  @override
  Future<ApprovalDetails> getApprovalDetails(
    String approvalId, {
    bool refreshPreview = false,
  }) async {
    detailReads.add(approvalId);
    final error = detailsError;
    if (error != null) throw error;
    final suffix = approvalId.substring('appr_'.length);
    return approvalDetails(
      approvalId: approvalId,
      sessionId: 'sess_$suffix',
      runId: 'run_$suffix',
      previewHash: 'sha256:preview-$approvalId',
    );
  }

  @override
  Future<Map<String, dynamic>> approveReviewedApproval(
    ApprovalDetails details, {
    String? comment,
  }) async {
    approveAttempts++;
    final error = approveError;
    if (error != null) throw error;
    approved.add(details);
    return {'approvalId': details.approvalId, 'status': 'approved'};
  }

  @override
  Future<Map<String, dynamic>> denyApproval(
    String approvalId, {
    String? reason,
  }) async {
    denied.add(approvalId);
    return {'approvalId': approvalId, 'status': 'denied'};
  }

  @override
  Future<Delegation> getDelegation({required String delegationId}) async {
    delegationReads.add(delegationId);
    final found = delegation;
    if (found == null) {
      throw const TuringApiException(code: 'not_found', message: 'gone');
    }
    return found;
  }

  @override
  Future<RemoteEgressDisclosure> prepareDelegationEgress({
    required String delegationId,
  }) async {
    prepared.add(delegationId);
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
  }) async {
    decided.add((delegationId: delegationId, consent: consent));
    return _waitingDelegation();
  }

  @override
  Future<SessionDeletionReceipt> deleteSession({
    required String sessionId,
  }) async {
    deleted.add(sessionId);
    return const SessionDeletionReceipt.completed();
  }

  @override
  Future<List<Session>> listSessions({int limit = 50, String? after}) async =>
      const [];

  @override
  Future<Map<String, dynamic>> createSession({String? title}) async => {
    'sessionId': 'sess_new',
    'createdAt': '2026-11-20T00:00:00.000Z',
  };

  @override
  Future<Session> getSession({required String sessionId}) async =>
      Session(sessionId: sessionId, title: null, updatedAt: DateTime.utc(2026));

  @override
  Future<List<Message>> listMessages({
    required String sessionId,
    int limit = 50,
    String? before,
  }) async {
    messageReads.add(sessionId);
    return const [];
  }

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
  }) async => {'sessionId': sessionId, 'runId': 'run_x', 'status': 'queued'};

  @override
  Future<List<ToolDescriptor>> listTools() async => const [];

  @override
  Future<List<AgentDescriptor>> listAgents() async => const [];
}

class _FakeEventSource implements TuringEventSource {
  final _events = StreamController<TuringEvent>();

  @override
  Stream<TuringEvent> connect({required String sessionId, int? lastSequence}) =>
      _events.stream;

  @override
  void close() {
    unawaited(_events.close());
  }
}

class _FakeSessionUpdateSource implements TuringSessionUpdateSource {
  final _events = StreamController<TuringEvent>();

  void add(TuringEvent event) => _events.add(event);

  @override
  Stream<TuringEvent> connectSessionUpdates() => _events.stream;

  @override
  void close() {
    unawaited(_events.close());
  }
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
