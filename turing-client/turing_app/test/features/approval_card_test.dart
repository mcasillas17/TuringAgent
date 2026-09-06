import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/approvals/approval_card.dart';
import 'package:turing_flutter_app/l10n/generated/app_localizations.dart';
import 'package:turing_flutter_app/models/approval.dart';

import '../support/approval_details.dart';

void main() {
  Widget card({
    String approvalId = 'appr_1',
    Future<ApprovalDetails> Function(bool refresh)? load,
    ValueChanged<ApprovalDetails>? approve,
    VoidCallback? deny,
    bool busy = false,
    double textScale = 1,
  }) => MaterialApp(
    localizationsDelegates: AppLocalizations.localizationsDelegates,
    supportedLocales: AppLocalizations.supportedLocales,
    builder: (context, child) => MediaQuery(
      data: MediaQuery.of(
        context,
      ).copyWith(textScaler: TextScaler.linear(textScale)),
      child: child!,
    ),
    home: Scaffold(
      body: ApprovalCard(
        approvalId: approvalId,
        toolName: 'files.update',
        loadDetails: load,
        onApprove: approve ?? (_) {},
        onDeny: deny ?? () {},
        busy: busy,
      ),
    ),
  );

  FilledButton approveButton(WidgetTester tester) =>
      tester.widget<FilledButton>(
        find.byWidgetPredicate((widget) => widget is FilledButton),
      );

  testWidgets(
    'review uses one spacious scroll surface with visible decisions',
    (tester) async {
      await tester.pumpWidget(
        card(
          load: (_) async => approvalDetails(
            unifiedDiff: List.generate(
              200,
              (index) => '+reviewed line $index',
            ).join('\n'),
          ),
        ),
      );
      await tester.pumpAndSettle();
      final scroll = find.byType(SingleChildScrollView);
      expect(scroll, findsOneWidget);
      expect(tester.getSize(scroll).height, greaterThan(180));
      final deny = tester.getRect(find.text('Deny'));
      expect(deny.top, greaterThan(0));
      expect(deny.bottom, lessThan(600));
      expect(approveButton(tester).onPressed, isNotNull);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );

  testWidgets('unreviewed arguments cannot authorize a mutation', (
    tester,
  ) async {
    await tester.pumpWidget(card());
    expect(approveButton(tester).onPressed, isNull);
    expect(
      find.text('Preview unavailable. Retry or deny this request.'),
      findsOneWidget,
    );
    expect(
      tester
          .widget<OutlinedButton>(
            find.byWidgetPredicate((widget) => widget is OutlinedButton),
          )
          .onPressed,
      isNotNull,
    );
  });

  testWidgets('loading leaves denial available and never enables approval', (
    tester,
  ) async {
    final pending = Completer<ApprovalDetails>();
    var denied = false;
    await tester.pumpWidget(
      card(load: (_) => pending.future, deny: () => denied = true),
    );
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
    expect(approveButton(tester).onPressed, isNull);
    await tester.tap(find.text('Deny'));
    expect(denied, isTrue);
    await tester.pumpWidget(const SizedBox.shrink());
    pending.complete(approvalDetails());
    await tester.pump();
    expect(tester.takeException(), isNull);
  });

  testWidgets('displays exact server diff and submits the reviewed identity', (
    tester,
  ) async {
    final details = approvalDetails();
    ApprovalDetails? approved;
    await tester.pumpWidget(
      card(load: (_) async => details, approve: (value) => approved = value),
    );
    await tester.pumpAndSettle();
    expect(find.text(details.filePreview!.unifiedDiff), findsOneWidget);
    expect(
      find.textContaining(details.filePreview!.physicalPath),
      findsOneWidget,
    );
    await tester.tap(find.text('Approve'));
    expect(identical(approved, details), isTrue);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('failure is safe and retry performs only a read', (tester) async {
    var reads = 0;
    var approvals = 0;
    await tester.pumpWidget(
      card(
        load: (_) async {
          if (++reads == 1) {
            throw Exception('secret backend path and credential');
          }
          return approvalDetails();
        },
        approve: (_) => approvals++,
      ),
    );
    await tester.pumpAndSettle();
    expect(find.textContaining('secret backend'), findsNothing);
    expect(approveButton(tester).onPressed, isNull);
    await tester.tap(find.text('Retry preview'));
    await tester.pumpAndSettle();
    expect(reads, 2);
    expect(approvals, 0);
    expect(approveButton(tester).onPressed, isNotNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('stale requires explicit refresh and a separate approve tap', (
    tester,
  ) async {
    final refreshes = <bool>[];
    var approvals = 0;
    await tester.pumpWidget(
      card(
        load: (refresh) async {
          refreshes.add(refresh);
          return refresh
              ? approvalDetails(previewHash: 'sha256:renewed')
              : approvalDetails(
                  state: ApprovalPreviewState.stale,
                  canApprove: false,
                );
        },
        approve: (_) => approvals++,
      ),
    );
    await tester.pumpAndSettle();
    expect(approveButton(tester).onPressed, isNull);
    await tester.tap(find.text('Refresh preview'));
    await tester.pumpAndSettle();
    expect(refreshes, [false, true]);
    expect(approvals, 0);
    await tester.tap(find.text('Approve'));
    expect(approvals, 1);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets(
    'retry refreshes a stored unavailable preview without approving',
    (tester) async {
      final requests = <bool>[];
      var approvals = 0;
      await tester.pumpWidget(
        card(
          load: (refresh) async {
            requests.add(refresh);
            return refresh
                ? approvalDetails(previewHash: 'sha256:recovered')
                : approvalDetails(
                    state: ApprovalPreviewState.unavailable,
                    canApprove: false,
                  );
          },
          approve: (_) => approvals++,
        ),
      );
      await tester.pumpAndSettle();
      expect(approveButton(tester).onPressed, isNull);
      await tester.tap(find.text('Retry preview'));
      await tester.pumpAndSettle();
      expect(requests, [false, true]);
      expect(approvals, 0);
      expect(approveButton(tester).onPressed, isNotNull);
      await tester.tap(find.text('Approve'));
      expect(approvals, 1);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );

  testWidgets('expiry and busy state disable unsafe decisions', (tester) async {
    await tester.pumpWidget(
      card(load: (_) async => approvalDetails(expiresAt: DateTime.now())),
    );
    await tester.pumpAndSettle();
    expect(approveButton(tester).onPressed, isNull);
    expect(find.text('This approval has expired.'), findsOneWidget);
    await tester.pumpWidget(
      card(load: (_) async => approvalDetails(), busy: true),
    );
    await tester.pump();
    expect(approveButton(tester).onPressed, isNull);
    expect(
      tester
          .widget<OutlinedButton>(
            find.byWidgetPredicate((widget) => widget is OutlinedButton),
          )
          .onPressed,
      isNull,
    );
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('mismatched details cannot become a reviewed approval', (
    tester,
  ) async {
    await tester.pumpWidget(
      card(load: (_) async => approvalDetails(approvalId: 'another-approval')),
    );
    await tester.pumpAndSettle();
    expect(approveButton(tester).onPressed, isNull);
    expect(find.textContaining('another-approval'), findsNothing);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('expiry while open disables approval without polling', (
    tester,
  ) async {
    await tester.pumpWidget(
      card(
        load: (_) async => approvalDetails(
          expiresAt: DateTime.now().add(const Duration(seconds: 2)),
        ),
      ),
    );
    await tester.pump();
    expect(approveButton(tester).onPressed, isNotNull);
    await tester.pump(const Duration(seconds: 3));
    expect(approveButton(tester).onPressed, isNull);
    expect(find.text('This approval has expired.'), findsOneWidget);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('identity replacement fences an older detail read', (
    tester,
  ) async {
    final old = Completer<ApprovalDetails>();
    await tester.pumpWidget(card(load: (_) => old.future));
    await tester.pumpWidget(
      card(
        approvalId: 'appr_2',
        load: (_) async => approvalDetails(approvalId: 'appr_2'),
      ),
    );
    await tester.pump();
    expect(approveButton(tester).onPressed, isNotNull);
    old.complete(approvalDetails());
    await tester.pump();
    expect(approveButton(tester).onPressed, isNotNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('review controls stay labeled and usable with large text', (
    tester,
  ) async {
    tester.view.devicePixelRatio = 1;
    tester.view.physicalSize = const Size(360, 800);
    addTearDown(tester.view.resetDevicePixelRatio);
    addTearDown(tester.view.resetPhysicalSize);
    final semantics = tester.ensureSemantics();
    await tester.pumpWidget(
      card(load: (_) async => approvalDetails(), textScale: 2),
    );
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    expect(find.text('Deny'), findsOneWidget);
    await expectLater(tester, meetsGuideline(labeledTapTargetGuideline));
    await expectLater(tester, meetsGuideline(androidTapTargetGuideline));
    semantics.dispose();
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
