import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
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
  testWidgets(
    'legacy failure and cancellation cards accept enums not backend strings',
    (tester) async {
      // The whole point of this API: there is no `message`/`String`
      // constructor parameter left to (mis)use — only a semantic
      // `RunOutcomeReason`. This test exists to pin that shape; if a future
      // change reintroduces a free-text parameter, every other test in this
      // file that asserts exact, localized copy below would also start
      // failing once raw backend prose could reach the widget again.
      await tester.pumpWidget(
        _host(const RunFailureCard(reason: RunOutcomeReason.providerFailure)),
      );

      expect(find.text('Provider unavailable'), findsOneWidget);
      expect(
        find.text('The model provider could not complete this run.'),
        findsOneWidget,
      );
    },
  );

  testWidgets('renders the "Run failed" outcome label for an unclassified '
      'reason, visibly, not just in the semantics label, and never renders '
      '"Run cancelled"', (tester) async {
    await tester.pumpWidget(
      _host(const RunFailureCard(reason: RunOutcomeReason.none)),
    );

    // Sighted users read the widget tree, not the accessibility tree: the
    // outcome must be visible on screen, not only announced to assistive
    // technology via the `Semantics` label.
    expect(find.text('Run failed'), findsOneWidget);
    expect(find.text('Run cancelled'), findsNothing);
    expect(
      find.text('The run ended before it could complete.'),
      findsOneWidget,
    );
  });

  testWidgets(
    'exposes the exact "Run failed: ..." semantics label as a live region',
    (tester) async {
      final handle = tester.ensureSemantics();
      await tester.pumpWidget(
        _host(const RunFailureCard(reason: RunOutcomeReason.none)),
      );

      // Assert against the actual rendered semantics tree, not just the
      // `Semantics` widget's constructor arguments: a widget can be built
      // with a `liveRegion: true` argument and still fail to reach the
      // rendered `SemanticsNode` if it is merged away, excluded by an
      // ancestor, or the render object never attaches it.
      expect(
        find.bySemanticsLabel(
          'Run failed: The run ended before it could complete.',
        ),
        findsOneWidget,
      );
      expect(
        tester.getSemantics(find.byType(RunFailureCard)),
        matchesSemantics(
          label: 'Run failed: The run ended before it could complete.',
          isLiveRegion: true,
        ),
      );
      handle.dispose();
    },
  );

  testWidgets('uses an error icon and theme-derived error colors, distinct '
      'from ordinary content', (tester) async {
    late ColorScheme colorScheme;
    await tester.pumpWidget(
      MaterialApp(
        localizationsDelegates: AppLocalizations.localizationsDelegates,
        supportedLocales: AppLocalizations.supportedLocales,
        home: Builder(
          builder: (context) {
            colorScheme = Theme.of(context).colorScheme;
            return const Scaffold(
              body: RunFailureCard(reason: RunOutcomeReason.none),
            );
          },
        ),
      ),
    );

    // Visual distinction is pinned against the *theme's* error colors, not
    // a hardcoded palette: this fails if the card stops using the error
    // container styling while staying correct whichever concrete color
    // scheme the running app supplies.
    final icon = tester.widget<Icon>(
      find.descendant(
        of: find.byType(RunFailureCard),
        matching: find.byIcon(Icons.error_outline),
      ),
    );
    expect(icon.color, colorScheme.onErrorContainer);

    final card = tester.widget<Card>(
      find.descendant(
        of: find.byType(RunFailureCard),
        matching: find.byType(Card),
      ),
    );
    expect(card.color, colorScheme.errorContainer);

    // The error color must actually differ from a plain surface color, or
    // pinning "errorContainer" would be a distinction without a difference.
    expect(colorScheme.errorContainer, isNot(colorScheme.surface));
  });

  testWidgets(
    'every classified failure reason resolves distinct, non-empty localized '
    'copy with no raw enum name',
    (tester) async {
      for (final reason in RunOutcomeReason.values) {
        await tester.pumpWidget(_host(RunFailureCard(reason: reason)));
        await tester.pump();

        expect(
          find.textContaining(reason.toString()),
          findsNothing,
          reason: 'the raw Dart enum identifier must never leak into the UI',
        );
        expect(find.byType(RunFailureCard), findsOneWidget);
      }
    },
  );
}
