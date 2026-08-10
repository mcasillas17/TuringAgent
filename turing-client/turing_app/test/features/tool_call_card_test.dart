import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/tool_call_card.dart';

void main() {
  testWidgets('running card shows tool name and a progress indicator', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: ToolCallCard(
            toolName: 'system.time',
            serverName: 'system',
            status: ToolCallStatus.running,
          ),
        ),
      ),
    );

    expect(find.text('system.time'), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);
  });

  testWidgets('completed card shows a check and no spinner', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: ToolCallCard(
            toolName: 'system.time',
            status: ToolCallStatus.completed,
          ),
        ),
      ),
    );

    expect(find.byIcon(Icons.check_circle_outline), findsOneWidget);
    expect(find.byType(CircularProgressIndicator), findsNothing);
  });

  testWidgets('failed card shows the error text and an error icon', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: ToolCallCard(
            toolName: 'files.create',
            status: ToolCallStatus.failed,
            error: 'mcp_call_failed',
          ),
        ),
      ),
    );

    expect(find.textContaining('mcp_call_failed'), findsOneWidget);
    expect(find.byIcon(Icons.error_outline), findsOneWidget);
  });

  testWidgets('denied card shows a block icon', (tester) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: ToolCallCard(
            toolName: 'files.create',
            status: ToolCallStatus.denied,
          ),
        ),
      ),
    );

    expect(find.byIcon(Icons.block), findsOneWidget);
  });

  testWidgets('exposes a distinct status label per status to screen readers', (
    tester,
  ) async {
    final handle = tester.ensureSemantics();
    const cases = {
      ToolCallStatus.running: 'Running',
      ToolCallStatus.completed: 'Completed',
      ToolCallStatus.failed: 'Failed',
      ToolCallStatus.denied: 'Denied',
    };
    for (final entry in cases.entries) {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ToolCallCard(
              toolName: 'system.time',
              status: entry.key,
            ),
          ),
        ),
      );
      // Status is announced both visibly and semantically, not by glyph alone.
      expect(find.text(entry.value), findsOneWidget);
      expect(
        find.bySemanticsLabel(RegExp(entry.value)),
        findsOneWidget,
        reason: 'status ${entry.key} must reach the semantics tree',
      );
    }
    handle.dispose();
  });

  testWidgets('failed card frames the error in its semantics label', (
    tester,
  ) async {
    final handle = tester.ensureSemantics();
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: ToolCallCard(
            toolName: 'files.create',
            status: ToolCallStatus.failed,
            error: 'mcp_call_failed',
          ),
        ),
      ),
    );

    expect(
      find.bySemanticsLabel(RegExp('Error: mcp_call_failed')),
      findsOneWidget,
    );
    handle.dispose();
  });

  testWidgets('renders the tool name once when a server is present', (
    tester,
  ) async {
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(
          body: ToolCallCard(
            toolName: 'system.time',
            serverName: 'system',
            status: ToolCallStatus.running,
          ),
        ),
      ),
    );

    // Server line shows the server only; the tool name is not duplicated.
    expect(find.text('system.time'), findsOneWidget);
    expect(find.text('system'), findsOneWidget);
    expect(find.text('system / system.time'), findsNothing);
  });
}
