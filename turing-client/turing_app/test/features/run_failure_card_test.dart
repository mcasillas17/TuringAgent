import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/run_failure_card.dart';

void main() {
  testWidgets('renders the failure message text', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(body: RunFailureCard(message: 'connection lost')),
      ),
    );

    expect(find.textContaining('connection lost'), findsOneWidget);
  });

  testWidgets('exposes the exact "Run failed: ..." semantics label as a live '
      'region', (tester) async {
    final handle = tester.ensureSemantics();
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(body: RunFailureCard(message: 'connection lost')),
      ),
    );

    final semantics = tester.widget<Semantics>(
      find
          .descendant(
            of: find.byType(RunFailureCard),
            matching: find.byType(Semantics),
          )
          .first,
    );
    expect(semantics.properties.label, 'Run failed: connection lost');
    expect(semantics.properties.liveRegion, isTrue);
    handle.dispose();
  });
}
