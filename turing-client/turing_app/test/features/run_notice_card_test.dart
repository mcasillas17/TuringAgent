import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/run_notice_card.dart';

void main() {
  testWidgets('renders the note text', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: RunNoticeCard(note: 'maximum tool iterations reached'),
        ),
      ),
    );

    expect(
      find.textContaining('maximum tool iterations reached'),
      findsOneWidget,
    );
  });

  testWidgets('reaches the semantics tree', (tester) async {
    final handle = tester.ensureSemantics();
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: RunNoticeCard(note: 'maximum tool iterations reached'),
        ),
      ),
    );

    expect(
      find.bySemanticsLabel(RegExp('maximum tool iterations reached')),
      findsOneWidget,
    );
    handle.dispose();
  });
}
