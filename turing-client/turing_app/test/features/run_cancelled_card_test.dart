import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/run_cancelled_card.dart';
import 'package:turing_flutter_app/features/chat/run_failure_card.dart';
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
  testWidgets('renders the localized cancellation copy for a specific '
      'outcome reason', (tester) async {
    await tester.pumpWidget(
      _host(const RunCancelledCard(reason: RunOutcomeReason.userCancelled)),
    );

    expect(find.text('Run cancelled'), findsOneWidget);
    expect(find.text('You cancelled this run.'), findsOneWidget);
  });

  testWidgets('abandoned run uses localized abandonment card', (tester) async {
    await tester.pumpWidget(
      _host(const RunCancelledCard(reason: RunOutcomeReason.abandoned)),
    );

    // Abandonment (an ambiguous `client_cancelled`/tool-cleanup outcome) is
    // rendered as a truthful interruption, never a false "you cancelled
    // this" claim.
    expect(find.text('Run interrupted'), findsOneWidget);
    expect(find.text('The run ended before it could finish.'), findsOneWidget);
    expect(find.text('You cancelled this run.'), findsNothing);
  });

  testWidgets('renders the fallback outcome label visibly for an '
      'unclassified reason, not just in the semantics label, and never '
      'renders "Run failed"', (tester) async {
    await tester.pumpWidget(
      _host(const RunCancelledCard(reason: RunOutcomeReason.none)),
    );

    expect(find.text('Run interrupted'), findsOneWidget);
    expect(find.text('Run failed'), findsNothing);
  });

  testWidgets(
    'exposes the exact "Run cancelled: ..." semantics label as a live '
    'region — never "Run failed"',
    (tester) async {
      final handle = tester.ensureSemantics();
      await tester.pumpWidget(
        _host(const RunCancelledCard(reason: RunOutcomeReason.userCancelled)),
      );

      expect(
        find.bySemanticsLabel('Run cancelled: You cancelled this run.'),
        findsOneWidget,
      );
      expect(
        tester.getSemantics(find.byType(RunCancelledCard)),
        matchesSemantics(
          label: 'Run cancelled: You cancelled this run.',
          isLiveRegion: true,
        ),
      );
      expect(
        find.bySemanticsLabel('Run failed: You cancelled this run.'),
        findsNothing,
      );
      handle.dispose();
    },
  );

  testWidgets('uses an error-style card, distinct from ordinary content, '
      'same terminal-outcome treatment as RunFailureCard', (tester) async {
    late ColorScheme colorScheme;
    await tester.pumpWidget(
      MaterialApp(
        localizationsDelegates: AppLocalizations.localizationsDelegates,
        supportedLocales: AppLocalizations.supportedLocales,
        home: Builder(
          builder: (context) {
            colorScheme = Theme.of(context).colorScheme;
            return const Scaffold(
              body: RunCancelledCard(reason: RunOutcomeReason.userCancelled),
            );
          },
        ),
      ),
    );

    final icon = tester.widget<Icon>(
      find.descendant(
        of: find.byType(RunCancelledCard),
        matching: find.byIcon(Icons.error_outline),
      ),
    );
    expect(icon.color, colorScheme.onErrorContainer);

    final card = tester.widget<Card>(
      find.descendant(
        of: find.byType(RunCancelledCard),
        matching: find.byType(Card),
      ),
    );
    expect(card.color, colorScheme.errorContainer);

    expect(colorScheme.errorContainer, isNot(colorScheme.surface));
  });

  testWidgets(
    'RunCancelledCard and RunFailureCard are visually siblings but never '
    'the same widget type',
    (tester) async {
      await tester.pumpWidget(
        _host(
          const Column(
            children: [
              RunFailureCard(reason: RunOutcomeReason.none),
              RunCancelledCard(reason: RunOutcomeReason.userCancelled),
            ],
          ),
        ),
      );

      expect(find.byType(RunFailureCard), findsOneWidget);
      expect(find.byType(RunCancelledCard), findsOneWidget);
    },
  );
}
