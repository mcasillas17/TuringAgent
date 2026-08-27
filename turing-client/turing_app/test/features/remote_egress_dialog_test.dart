import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/remote_egress_dialog.dart';
import 'package:turing_flutter_app/l10n/generated/app_localizations.dart';
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
    await _open(tester, disclosure);

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
    await _open(tester, disclosure);

    expect(find.text('Must Not Render'), findsNothing);
  });

  testWidgets('skill header is not rendered when the skill list is empty', (
    tester,
  ) async {
    final disclosure = RemoteEgressDisclosure(
      challenge: 'challenge',
      provider: 'openai_compatible',
      model: 'remote',
      endpoint: 'https://models.example/v1',
      endpointHost: 'models.example',
      dataCategories: const [EgressDataCategory.skillContent],
      expiresAt: DateTime.utc(2026, 8, 26),
      skills: const [],
    );
    await _open(tester, disclosure);

    expect(find.text('Skills that may be sent:'), findsNothing);
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
    await _open(tester, disclosure);

    expect(find.text('Send data off this machine?'), findsOneWidget);
    expect(
      find.text('vendor · https://vendor.example/team-a/mcp'),
      findsOneWidget,
    );
    expect(find.text('vendor/vendor.lookup'), findsOneWidget);
  });

  group('memory disclosure', () {
    testWidgets('pinned memory names the tiers that would leave', (
      tester,
    ) async {
      final disclosure = RemoteEgressDisclosure(
        challenge: 'challenge',
        provider: 'openai_compatible',
        model: 'remote',
        endpoint: 'https://models.example/v1',
        endpointHost: 'models.example',
        dataCategories: const [EgressDataCategory.memoryProfile],
        expiresAt: DateTime.utc(2026, 8, 24),
        memoryProfileMayBeSent: true,
        memoryNotes: const [
          MemoryEgressDisclosure(
            noteId: '',
            title: 'persona.md',
            vaultPath: 'persona.md',
            tier: MemoryEgressTier.persona,
            bodyMayBeSent: true,
          ),
          MemoryEgressDisclosure(
            noteId: '',
            title: 'profile.md',
            vaultPath: 'profile.md',
            tier: MemoryEgressTier.profile,
            bodyMayBeSent: true,
          ),
        ],
      );

      await _open(tester, disclosure);

      expect(find.text('Memory and profile'), findsOneWidget);
      expect(find.textContaining('pinned into this run'), findsOneWidget);
      expect(find.textContaining('persona.md'), findsWidgets);
      expect(find.textContaining('profile.md'), findsWidgets);
      expect(find.textContaining('full content may be sent'), findsWidgets);
    });

    testWidgets('memory tools with nothing pinned say exactly that', (
      tester,
    ) async {
      final disclosure = RemoteEgressDisclosure(
        challenge: 'challenge',
        provider: 'openai_compatible',
        model: 'remote',
        endpoint: 'https://models.example/v1',
        endpointHost: 'models.example',
        dataCategories: const [EgressDataCategory.memoryProfile],
        expiresAt: DateTime.utc(2026, 8, 24),
        memoryProfileMayBeSent: true,
        selectedTools: const ['memory/memory.search', 'memory/memory.read'],
      );

      await _open(tester, disclosure);

      expect(find.text('Memory and profile'), findsOneWidget);
      expect(
        find.textContaining('memory tools this run may call'),
        findsOneWidget,
      );
      expect(find.text('memory/memory.search'), findsOneWidget);
      expect(find.text('memory/memory.read'), findsOneWidget);
      expect(
        find.textContaining('pinned into this run'),
        findsNothing,
        reason: 'nothing is pinned, so nothing may claim to be',
      );
    });

    testWidgets('the dialog never shows the snapshot fingerprint or a hash', (
      tester,
    ) async {
      final disclosure = RemoteEgressDisclosure(
        challenge: 'challenge-value',
        provider: 'openai_compatible',
        model: 'remote',
        endpoint: 'https://models.example/v1',
        endpointHost: 'models.example',
        dataCategories: const [EgressDataCategory.memoryProfile],
        expiresAt: DateTime.utc(2026, 8, 24),
        memoryProfileMayBeSent: true,
        memoryNotes: const [
          MemoryEgressDisclosure(
            noteId: 'note-1',
            title: 'Ada',
            vaultPath: 'beliefs/people/ada.md',
            tier: MemoryEgressTier.belief,
            bodyMayBeSent: false,
          ),
        ],
      );

      await _open(tester, disclosure);

      expect(find.textContaining('sha256:'), findsNothing);
      expect(find.textContaining('challenge-value'), findsNothing);
      expect(find.textContaining('note-1'), findsNothing);
      expect(find.textContaining('beliefs/people/ada.md'), findsOneWidget);
      expect(find.textContaining('name and location only'), findsOneWidget);
    });

    testWidgets('an external agent gets the same memory line', (tester) async {
      final disclosure = RemoteEgressDisclosure(
        challenge: 'challenge',
        provider: 'openai_compatible',
        model: 'remote',
        endpoint: 'https://agent.example/v1',
        endpointHost: 'agent.example',
        externalAgentId: 'agent-1',
        dataCategories: const [EgressDataCategory.memoryProfile],
        expiresAt: DateTime.utc(2026, 8, 24),
        memoryProfileMayBeSent: true,
        memoryNotes: const [
          MemoryEgressDisclosure(
            noteId: '',
            title: 'persona.md',
            vaultPath: 'persona.md',
            tier: MemoryEgressTier.persona,
            bodyMayBeSent: true,
          ),
        ],
      );

      await _open(tester, disclosure);

      expect(find.text('Memory and profile'), findsOneWidget);
      expect(find.textContaining('persona.md'), findsWidgets);
    });

    testWidgets('a run with no memory says nothing about memory', (
      tester,
    ) async {
      final disclosure = RemoteEgressDisclosure(
        challenge: 'challenge',
        provider: 'openai_compatible',
        model: 'remote',
        endpoint: 'https://models.example/v1',
        endpointHost: 'models.example',
        dataCategories: const [EgressDataCategory.currentMessage],
        expiresAt: DateTime.utc(2026, 8, 24),
        memoryNotes: const [
          MemoryEgressDisclosure(
            noteId: '',
            title: 'persona.md',
            vaultPath: 'persona.md',
            tier: MemoryEgressTier.persona,
            bodyMayBeSent: true,
          ),
        ],
      );

      await _open(tester, disclosure);

      expect(find.text('Memory and profile'), findsNothing);
      expect(find.textContaining('persona.md'), findsNothing);
      expect(find.textContaining('pinned into this run'), findsNothing);
    });

    // The server sends this shape for every run that has the memory tools
    // selected and nothing pinned: a single row naming the beliefs folder the
    // tools can reach. Reading that row as "pinned into this run" would tell
    // the user their accepted memory is already in the prompt when it is not.
    testWidgets('a reachable beliefs folder is never called pinned', (
      tester,
    ) async {
      final disclosure = RemoteEgressDisclosure(
        challenge: 'challenge',
        provider: 'openai_compatible',
        model: 'remote',
        endpoint: 'https://models.example/v1',
        endpointHost: 'models.example',
        dataCategories: const [EgressDataCategory.memoryProfile],
        expiresAt: DateTime.utc(2026, 8, 24),
        memoryProfileMayBeSent: true,
        selectedTools: const ['memory/memory.search'],
        memoryNotes: const [
          MemoryEgressDisclosure(
            noteId: 'beliefs',
            title: 'Accepted memory reachable by the memory tools',
            vaultPath: 'beliefs',
            tier: MemoryEgressTier.belief,
            bodyMayBeSent: false,
          ),
        ],
      );

      await _open(tester, disclosure);

      expect(
        find.textContaining('pinned into this run'),
        findsNothing,
        reason: 'nothing is pinned, so nothing may claim to be',
      );
      expect(find.textContaining('the memory tools can reach'), findsOneWidget);
      expect(find.textContaining('beliefs'), findsWidgets);
      expect(find.text('memory/memory.search'), findsOneWidget);
    });

    testWidgets('pinned documents and reachable memory get separate headings', (
      tester,
    ) async {
      final disclosure = RemoteEgressDisclosure(
        challenge: 'challenge',
        provider: 'openai_compatible',
        model: 'remote',
        endpoint: 'https://models.example/v1',
        endpointHost: 'models.example',
        dataCategories: const [EgressDataCategory.memoryProfile],
        expiresAt: DateTime.utc(2026, 8, 24),
        memoryProfileMayBeSent: true,
        selectedTools: const ['memory/memory.remember'],
        memoryNotes: const [
          MemoryEgressDisclosure(
            noteId: '',
            title: 'persona.md',
            vaultPath: 'persona.md',
            tier: MemoryEgressTier.persona,
            bodyMayBeSent: true,
          ),
          MemoryEgressDisclosure(
            noteId: 'beliefs',
            title: 'Accepted memory reachable by the memory tools',
            vaultPath: 'beliefs',
            tier: MemoryEgressTier.belief,
            bodyMayBeSent: false,
          ),
        ],
      );

      await _open(tester, disclosure);

      expect(find.textContaining('pinned into this run'), findsOneWidget);
      expect(find.textContaining('the memory tools can reach'), findsOneWidget);
      expect(
        find.textContaining('memory tools this run may call'),
        findsOneWidget,
      );
      expect(find.textContaining('persona.md'), findsWidgets);
    });

    // A tier a newer server names and this build does not is still memory that
    // leaves the machine. It is disclosed on the honest side of the line.
    testWidgets('an unknown tier is disclosed without being called pinned', (
      tester,
    ) async {
      final disclosure = RemoteEgressDisclosure(
        challenge: 'challenge',
        provider: 'openai_compatible',
        model: 'remote',
        endpoint: 'https://models.example/v1',
        endpointHost: 'models.example',
        dataCategories: const [EgressDataCategory.memoryProfile],
        expiresAt: DateTime.utc(2026, 8, 24),
        memoryProfileMayBeSent: true,
        memoryNotes: const [
          MemoryEgressDisclosure(
            noteId: '',
            title: 'Something newer',
            vaultPath: 'somewhere/new.md',
            tier: MemoryEgressTier.unspecified,
            bodyMayBeSent: false,
          ),
        ],
      );

      await _open(tester, disclosure);

      expect(find.textContaining('pinned into this run'), findsNothing);
      expect(find.textContaining('somewhere/new.md'), findsWidgets);
    });

    testWidgets('an unknown tier is labelled as memory, not as a tier', (
      tester,
    ) async {
      final disclosure = RemoteEgressDisclosure(
        challenge: 'challenge',
        provider: 'openai_compatible',
        model: 'remote',
        endpoint: 'https://models.example/v1',
        endpointHost: 'models.example',
        dataCategories: const [EgressDataCategory.memoryProfile],
        expiresAt: DateTime.utc(2026, 8, 24),
        memoryProfileMayBeSent: true,
        memoryNotes: const [
          MemoryEgressDisclosure(
            noteId: '',
            title: 'Something newer',
            vaultPath: 'somewhere/new.md',
            tier: MemoryEgressTier.unspecified,
            bodyMayBeSent: true,
          ),
        ],
      );

      await _open(tester, disclosure);

      // The row is named and its words are declared sendable — both true, both
      // the server's own claims. What is withheld is the tier, because nothing
      // here knows it. "Persona" would be a promise about which of the user's
      // documents this is, made up by a client that has never heard of it.
      expect(find.text('Memory · Something newer'), findsOneWidget);
      expect(find.textContaining('Persona'), findsNothing);
      expect(find.textContaining('Profile'), findsNothing);
      expect(find.textContaining('Belief'), findsNothing);
      expect(
        find.textContaining('the memory tools can reach'),
        findsOneWidget,
        reason:
            'an unnamed tier is disclosed under the weaker of the two '
            'headings, because the stronger one is a claim about the prompt',
      );
    });

    testWidgets('a bare memory server name is not offered as a tool', (
      tester,
    ) async {
      final disclosure = RemoteEgressDisclosure(
        challenge: 'challenge',
        provider: 'openai_compatible',
        model: 'remote',
        endpoint: 'https://models.example/v1',
        endpointHost: 'models.example',
        dataCategories: const [EgressDataCategory.memoryProfile],
        expiresAt: DateTime.utc(2026, 8, 24),
        memoryProfileMayBeSent: true,
        selectedTools: const ['memory', 'memoryx/tool'],
      );

      await _open(tester, disclosure);

      expect(
        find.textContaining('memory tools this run may call'),
        findsNothing,
        reason: 'neither name is a tool the memory server exposes',
      );
    });

    testWidgets(
      'two beliefs whose titles read the same are still two separate rows, '
      'each keyed by the note the server named',
      (tester) async {
        final disclosure = RemoteEgressDisclosure(
          challenge: 'challenge',
          provider: 'openai_compatible',
          model: 'remote',
          endpoint: 'https://models.example/v1',
          endpointHost: 'models.example',
          dataCategories: const [EgressDataCategory.memoryProfile],
          expiresAt: DateTime.utc(2026, 8, 24),
          selectedTools: const ['memory/memory.search'],
          memoryNotes: const [
            MemoryEgressDisclosure(
              noteId: 'note-1',
              title: 'Ada',
              vaultPath: 'beliefs/note-1.md',
              tier: MemoryEgressTier.belief,
              bodyMayBeSent: false,
            ),
            MemoryEgressDisclosure(
              noteId: 'note-2',
              title: 'Ada',
              vaultPath: 'beliefs/note-2.md',
              tier: MemoryEgressTier.belief,
              bodyMayBeSent: false,
            ),
          ],
        );

        await _open(tester, disclosure);

        expect(
          find.byKey(const ValueKey('egress-memory-note-1')),
          findsOneWidget,
          reason:
              'the row has to be addressable by the note the server named, '
              'not by copy two beliefs can share',
        );
        expect(
          find.byKey(const ValueKey('egress-memory-note-2')),
          findsOneWidget,
        );
      },
    );
  });
}

Future<void> _open(
  WidgetTester tester,
  RemoteEgressDisclosure disclosure,
) async {
  await tester.pumpWidget(
    MaterialApp(
      localizationsDelegates: AppLocalizations.localizationsDelegates,
      supportedLocales: AppLocalizations.supportedLocales,
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
}
