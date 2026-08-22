import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/remote_egress_dialog.dart';
import 'package:turing_flutter_app/models/remote_egress.dart';

void main() {
  testWidgets('remote MCP consent names exact endpoint and frozen tool', (
    tester,
  ) async {
    final disclosure = RemoteEgressDisclosure(
      challenge: 'challenge',
      provider: 'ollama',
      model: 'local',
      endpoint: '',
      endpointHost: '',
      dataCategories: const [
        EgressDataCategory.toolArguments,
        EgressDataCategory.toolResults,
      ],
      expiresAt: DateTime.utc(2026, 8, 21),
      remoteMcpServers: const [
        RemoteMcpDestination(
          serverName: 'vendor',
          endpoint: 'https://vendor.example/team-a/mcp',
          endpointHost: 'vendor.example',
        ),
      ],
      selectedTools: const ['vendor/vendor.lookup'],
    );
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (context) => TextButton(
            onPressed: () => showRemoteEgressDialog(context, disclosure),
            child: const Text('Open'),
          ),
        ),
      ),
    );

    await tester.tap(find.text('Open'));
    await tester.pumpAndSettle();

    expect(find.text('Send data off this machine?'), findsOneWidget);
    expect(
      find.text('vendor · https://vendor.example/team-a/mcp'),
      findsOneWidget,
    );
    expect(find.text('vendor/vendor.lookup'), findsOneWidget);
  });
}
