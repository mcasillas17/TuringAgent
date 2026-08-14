import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/approvals/approval_card.dart';

void main() {
  testWidgets('approval card exposes approve and deny actions', (tester) async {
    var approved = false;
    var denied = false;

    await tester.pumpWidget(
      MaterialApp(
        home: ApprovalCard(
          toolName: 'files.update',
          argsSummary: 'Update note.txt',
          onApprove: () => approved = true,
          onDeny: () => denied = true,
        ),
      ),
    );

    await tester.tap(find.text('Approve'));
    expect(approved, true);

    await tester.tap(find.text('Deny'));
    expect(denied, true);
  });

  testWidgets(
    'busy disables both actions so a second decision cannot be fired while '
    'the first is still in flight',
    (tester) async {
      var approveCalls = 0;
      var denyCalls = 0;

      await tester.pumpWidget(
        MaterialApp(
          home: ApprovalCard(
            toolName: 'files.update',
            argsSummary: 'Update note.txt',
            onApprove: () => approveCalls++,
            onDeny: () => denyCalls++,
            busy: true,
          ),
        ),
      );

      // Pinned directly on `onPressed` (not just "tapping does nothing"):
      // Flutter's own disabled-button semantics rely on `onPressed` being
      // `null`, so this is the actual contract `busy` must uphold.
      // `byWidgetPredicate` with an `is` check (not `find.byType`) because
      // `FilledButton.icon` builds a private `_FilledButtonWithIcon`
      // subclass — `find.byType(FilledButton)` matches on exact
      // `runtimeType` and would find nothing.
      expect(
        tester
            .widget<FilledButton>(
              find.byWidgetPredicate((widget) => widget is FilledButton),
            )
            .onPressed,
        isNull,
      );
      expect(
        tester
            .widget<OutlinedButton>(
              find.byWidgetPredicate((widget) => widget is OutlinedButton),
            )
            .onPressed,
        isNull,
      );

      await tester.tap(find.text('Approve'));
      await tester.tap(find.text('Deny'));
      expect(approveCalls, 0);
      expect(denyCalls, 0);
    },
  );

  testWidgets('defaults to not busy so existing callers are unaffected', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: ApprovalCard(
          toolName: 'files.update',
          argsSummary: 'Update note.txt',
          onApprove: () {},
          onDeny: () {},
        ),
      ),
    );

    expect(
      tester
          .widget<FilledButton>(
            find.byWidgetPredicate((widget) => widget is FilledButton),
          )
          .onPressed,
      isNotNull,
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
}
