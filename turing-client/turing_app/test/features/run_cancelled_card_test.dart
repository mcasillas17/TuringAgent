import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/run_cancelled_card.dart';
import 'package:turing_flutter_app/features/chat/run_failure_card.dart';

void main() {
  testWidgets('renders the cancellation message text', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(body: RunCancelledCard(message: 'client_cancelled')),
      ),
    );

    expect(find.textContaining('client_cancelled'), findsOneWidget);
  });

  testWidgets('exposes the exact "Run cancelled: ..." semantics label as a '
      'live region — never "Run failed"', (tester) async {
    final handle = tester.ensureSemantics();
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(body: RunCancelledCard(message: 'client_cancelled')),
      ),
    );

    // Assert against the actual rendered semantics tree, not just the
    // `Semantics` widget's constructor arguments: a widget can be built with
    // a `liveRegion: true` argument and still fail to reach the rendered
    // `SemanticsNode` if it is merged away, excluded by an ancestor, or the
    // render object never attaches it. `bySemanticsLabel` only matches a
    // node that assistive technology would actually see.
    expect(
      find.bySemanticsLabel('Run cancelled: client_cancelled'),
      findsOneWidget,
    );
    expect(
      tester.getSemantics(find.byType(RunCancelledCard)),
      matchesSemantics(
        label: 'Run cancelled: client_cancelled',
        isLiveRegion: true,
      ),
    );
    // The truthfulness requirement: a cancellation is not a failure, so the
    // rendered semantics must never say so.
    expect(find.bySemanticsLabel('Run failed: client_cancelled'), findsNothing);
    handle.dispose();
  });

  testWidgets('uses an error-style card, distinct from ordinary content, '
      'same terminal-outcome treatment as RunFailureCard', (tester) async {
    late ColorScheme colorScheme;
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) {
            colorScheme = Theme.of(context).colorScheme;
            return const Scaffold(
              body: RunCancelledCard(message: 'client_cancelled'),
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
        const MaterialApp(
          home: Scaffold(
            body: Column(
              children: [
                RunFailureCard(message: 'boom'),
                RunCancelledCard(message: 'client_cancelled'),
              ],
            ),
          ),
        ),
      );

      expect(find.byType(RunFailureCard), findsOneWidget);
      expect(find.byType(RunCancelledCard), findsOneWidget);
    },
  );
}
