import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/remote_egress_dialog.dart';
import 'package:turing_flutter_app/models/remote_egress.dart';

void main() {
  testWidgets('skill disclosure names content and metadata ceilings', (
    tester,
  ) async {
    final disclosure = RemoteEgressDisclosure(
      challenge: 'challenge',
      provider: 'openai_compatible',
      model: 'remote',
      endpoint: 'https://models.example/v1',
      endpointHost: 'models.example',
      dataCategories: const [EgressDataCategory.skillContent],
      expiresAt: DateTime.utc(2026, 8, 22),
      skills: const [
        SkillEgressDisclosure(
          skillId: 'writing/brief',
          displayName: 'Brief Writer',
          bodyMayBeSent: true,
        ),
        SkillEgressDisclosure(
          skillId: 'ops/locked',
          displayName: 'Locked Skill',
          bodyMayBeSent: false,
        ),
      ],
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

    expect(find.text('Brief Writer'), findsOneWidget);
    expect(find.text('full content may be sent'), findsOneWidget);
    expect(find.text('Locked Skill'), findsOneWidget);
    expect(find.text('name and description only'), findsOneWidget);
  });

  testWidgets('skills are not rendered without the skill-content category', (
    tester,
  ) async {
    final disclosure = RemoteEgressDisclosure(
      challenge: 'challenge',
      provider: 'ollama',
      model: 'local',
      endpoint: '',
      endpointHost: '',
      dataCategories: const [EgressDataCategory.toolArguments],
      expiresAt: DateTime.utc(2026, 8, 22),
      skills: const [
        SkillEgressDisclosure(
          skillId: 'writing/brief',
          displayName: 'Must Not Render',
          bodyMayBeSent: true,
        ),
      ],
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

    expect(find.text('Must Not Render'), findsNothing);
  });

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
