import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/models/memory.dart';

void main() {
  group('MemoryDocument.isWritable', () {
    test('a document that is not in the vault yet is the first run', () {
      // persona.md and profile.md do not exist until somebody writes them.
      // Reporting "not in the vault yet" as unwritable is how a fresh install
      // ends up with a Memory page that can only ever be read: the one action
      // that would fix it — writing the file — is the action being refused.
      const document = MemoryDocument(
        unavailableReason: MemoryUnavailableReason.vaultMissing,
      );

      expect(document.isWritable, isTrue);
      expect(
        document.contentHash,
        isEmpty,
        reason:
            'a save for a file that does not exist expects no version, and '
            'the server creates it',
      );
    });

    test('a readable document is writable, memory on or off', () {
      for (final reason in [
        MemoryUnavailableReason.none,
        MemoryUnavailableReason.disabled,
      ]) {
        expect(
          MemoryDocument(unavailableReason: reason).isWritable,
          isTrue,
          reason: '$reason describes the vault, not a broken file',
        );
      }
    });

    test('a document that could not be read offers no save', () {
      // These are the states where the server would refuse the write anyway,
      // and where a save composed against a partial read could truncate the
      // user's own file down to whatever this page happened to hold.
      for (final reason in [
        MemoryUnavailableReason.vaultUnreadable,
        MemoryUnavailableReason.contentParseFailed,
        MemoryUnavailableReason.contentTooLarge,
        MemoryUnavailableReason.unspecified,
      ]) {
        expect(
          MemoryDocument(unavailableReason: reason).isWritable,
          isFalse,
          reason: '$reason means this client never saw the whole document',
        );
      }
    });
  });

  group('MemoryDocument pin metadata', () {
    test('says nothing is cut until the server says so', () {
      const document = MemoryDocument(content: 'short');

      expect(document.pinnedTruncated, isFalse);
      expect(document.pinnedBytes, 0);
    });

    test('carries what a run will actually see', () {
      const document = MemoryDocument(
        content: 'a very long document',
        contentHash: 'sha256:document',
        pinnedTruncated: true,
        pinnedBytes: 4096,
      );

      expect(document.pinnedTruncated, isTrue);
      expect(document.pinnedBytes, 4096);
      expect(
        document.contentHash,
        'sha256:document',
        reason:
            'the compare-and-set token is about the document, never about '
            'the bounded pin a run carries',
      );
    });
  });
}
