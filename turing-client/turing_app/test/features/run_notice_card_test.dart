import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/run_notice_card.dart';
import 'package:turing_flutter_app/l10n/generated/app_localizations.dart';
import 'package:turing_flutter_app/models/run_state.dart';

Widget _host(Widget child) {
  return MaterialApp(
    localizationsDelegates: AppLocalizations.localizationsDelegates,
    supportedLocales: AppLocalizations.supportedLocales,
    home: Scaffold(body: child),
  );
}

void main() {
  // Preserved: `agent.run.step` payloads outside the three allowlisted
  // failure-notice categories (an approved, governed redacted
  // egress/model-limit notice, "maximum tool iterations reached", etc.)
  // keep rendering their EXACT backend-provided text unchanged. Nothing
  // about wiring the three localized categories in may alter this path.
  testWidgets('nonfailure redacted run step preserves governed notice copy', (
    tester,
  ) async {
    await tester.pumpWidget(
      _host(const RunNoticeCard(note: 'maximum tool iterations reached')),
    );

    expect(
      find.textContaining('maximum tool iterations reached'),
      findsOneWidget,
    );
  });

  testWidgets('reaches the semantics tree', (tester) async {
    final handle = tester.ensureSemantics();
    await tester.pumpWidget(
      _host(const RunNoticeCard(note: 'maximum tool iterations reached')),
    );

    expect(
      find.bySemanticsLabel(RegExp('maximum tool iterations reached')),
      findsOneWidget,
    );
    handle.dispose();
  });

  testWidgets(
    'failure run step uses localized category and bounded attempts without '
    'note',
    (tester) async {
      await tester.pumpWidget(
        _host(
          const RunNoticeCard.localized(
            category: RunStepNoticeCategory.dispatchRetry,
            attempt: 2,
            maxAttempts: 5,
          ),
        ),
      );

      expect(find.text('Starting attempt 2 of 5.'), findsOneWidget);
    },
  );

  testWidgets('recovery retry category renders its own localized copy', (
    tester,
  ) async {
    await tester.pumpWidget(
      _host(
        const RunNoticeCard.localized(
          category: RunStepNoticeCategory.recoveryRetry,
          attempt: 1,
          maxAttempts: 3,
        ),
      ),
    );

    expect(find.text('Recovering with attempt 1 of 3.'), findsOneWidget);
  });

  testWidgets('recovery exhausted category renders its own localized copy', (
    tester,
  ) async {
    await tester.pumpWidget(
      _host(
        const RunNoticeCard.localized(
          category: RunStepNoticeCategory.recoveryExhausted,
          attempt: 3,
          maxAttempts: 3,
        ),
      ),
    );

    expect(find.text('Recovery stopped after attempt 3 of 3.'), findsOneWidget);
  });

  testWidgets(
    'a negative attempt or maxAttempts is bounded to zero, never rendered '
    'as a raw negative number',
    (tester) async {
      await tester.pumpWidget(
        _host(
          const RunNoticeCard.localized(
            category: RunStepNoticeCategory.dispatchRetry,
            attempt: -4,
            maxAttempts: -1,
          ),
        ),
      );

      expect(find.text('Starting attempt 0 of 0.'), findsOneWidget);
      expect(find.textContaining('-'), findsNothing);
    },
  );

  testWidgets('attempt interpolation is defensively bounded above', (
    tester,
  ) async {
    await tester.pumpWidget(
      _host(
        const RunNoticeCard.localized(
          category: RunStepNoticeCategory.dispatchRetry,
          attempt: 5000,
          maxAttempts: 6000,
        ),
      ),
    );

    expect(find.text('Starting attempt 1000 of 1000.'), findsOneWidget);
    expect(find.textContaining('5000'), findsNothing);
    expect(find.textContaining('6000'), findsNothing);
  });
}
