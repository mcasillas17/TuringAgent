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
}
