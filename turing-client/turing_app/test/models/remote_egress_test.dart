import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/models/remote_egress.dart';

/// The consent dialog decides what to say about memory from the disclosure
/// alone. These tests pin the two judgements it makes before it renders a
/// single word: which frozen tools belong to the memory server, and which
/// disclosed rows are pinned into the prompt rather than merely reachable.
void main() {
  group('memory tool names', () {
    // Mirrors turing-backend/internal/egress.IsMemoryToolName and its test
    // table exactly. A client that draws the line somewhere else would either
    // promise the user a memory tool the run cannot call, or stay quiet about
    // one it can.
    const accepted = [
      'memory/memory.search',
      'memory/memory.read',
      'memory/memory.remember',
    ];
    const rejected = [
      '',
      'memory',
      'memory/',
      'memoryx/tool',
      'files/read',
      'skills/skill_view',
      'notmemory/memory.read',
    ];

    test('accepts exactly the names the server calls memory tools', () {
      final disclosure = _disclosure(selectedTools: [...accepted, ...rejected]);

      expect(disclosure.memoryTools, accepted);
    });

    test('a bare server name is not a tool the run can call', () {
      expect(_disclosure(selectedTools: const ['memory']).memoryTools, isEmpty);
      expect(
        _disclosure(selectedTools: const ['memory/']).memoryTools,
        isEmpty,
      );
    });

    test('a third-party server cannot borrow the memory category', () {
      expect(
        _disclosure(selectedTools: const ['memoryx/tool']).memoryTools,
        isEmpty,
      );
    });
  });

  group('pinned versus reachable', () {
    test('only the two pinned documents count as pinned', () {
      final disclosure = _disclosure(
        memoryNotes: const [
          MemoryEgressDisclosure(
            noteId: 'persona',
            title: 'persona.md',
            vaultPath: 'persona.md',
            tier: MemoryEgressTier.persona,
            bodyMayBeSent: true,
          ),
          MemoryEgressDisclosure(
            noteId: 'profile',
            title: 'profile.md',
            vaultPath: 'profile.md',
            tier: MemoryEgressTier.profile,
            bodyMayBeSent: true,
          ),
          MemoryEgressDisclosure(
            noteId: 'beliefs',
            title: 'Accepted memory reachable by the memory tools',
            vaultPath: 'beliefs',
            tier: MemoryEgressTier.belief,
            bodyMayBeSent: false,
          ),
          MemoryEgressDisclosure(
            noteId: 'unknown',
            title: 'A tier this client does not know',
            vaultPath: 'somewhere',
            tier: MemoryEgressTier.unspecified,
            bodyMayBeSent: false,
          ),
        ],
      );

      expect(disclosure.pinnedMemory.map((note) => note.vaultPath), const [
        'persona.md',
        'profile.md',
      ]);
      // A tier this build cannot name is never upgraded into a pinned claim.
      expect(
        disclosure.toolReachableMemory.map((note) => note.vaultPath),
        const ['beliefs', 'somewhere'],
      );
    });

    test(
      'a run with the tools but nothing pinned claims nothing is pinned',
      () {
        final disclosure = _disclosure(
          selectedTools: const ['memory/memory.search'],
          memoryProfileMayBeSent: true,
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

        expect(disclosure.pinnedMemory, isEmpty);
        expect(disclosure.toolReachableMemory, hasLength(1));
        expect(disclosure.mentionsMemory, isTrue);
      },
    );

    test('a run that never touches memory says nothing about it', () {
      expect(_disclosure().mentionsMemory, isFalse);
    });
  });
}

RemoteEgressDisclosure _disclosure({
  List<String> selectedTools = const [],
  List<MemoryEgressDisclosure> memoryNotes = const [],
  bool memoryProfileMayBeSent = false,
  List<EgressDataCategory> dataCategories = const [],
}) {
  return RemoteEgressDisclosure(
    challenge: 'challenge',
    provider: 'openai_compatible',
    model: 'remote',
    endpoint: 'https://models.example/v1',
    endpointHost: 'models.example',
    dataCategories: dataCategories,
    selectedTools: selectedTools,
    memoryNotes: memoryNotes,
    memoryProfileMayBeSent: memoryProfileMayBeSent,
    expiresAt: DateTime.utc(2026, 1, 1),
  );
}
