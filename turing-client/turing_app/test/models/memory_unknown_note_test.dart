import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/generated/turing/v1/memory.pb.dart'
    as memorypb;
import 'package:turing_flutter_app/models/grpc_mappers.dart';
import 'package:turing_flutter_app/models/memory.dart';

/// Whether a note turns up in a search over the user's own memory.
///
/// Round 7: `isIndexable` was a blacklist wearing a whitelist's clothes. It
/// accepted [MemoryUnavailableReason.none] — the server saying nothing is
/// wrong — *and* [MemoryUnavailableReason.unspecified], which is the server not
/// saying, and which is also what this build decodes any reason a newer server
/// invents into. So a note nobody could account for rendered as an ordinary,
/// searchable belief, and the one line that would have told the user their
/// memory is not being found was suppressed by the very condition that made it
/// unfindable.
///
/// The server's own predicate is the reference: search answers from notes whose
/// status is managed or unmanaged, and from nothing else.
void main() {
  /// A note whose unavailable_reason is a value no released server sends.
  /// The recognised NONE is on the wire first so the unknown value is isolated
  /// to the one field under test — protobuf keeps the last *recognised* value
  /// it parsed, which is exactly why the raw field cannot be trusted.
  memorypb.MemoryNote unknownReasonNote() {
    return memorypb.MemoryNote.fromBuffer(const [
      0x0a, 0x06, 0x6e, 0x6f, 0x74, 0x65, 0x5f, 0x31, // note_id "note_1"
      // path field 2, "beliefs/01.md"
      0x12, 0x0d, 0x62, 0x65, 0x6c, 0x69, 0x65, 0x66, 0x73, 0x2f, 0x30, 0x31,
      0x2e, 0x6d, 0x64,
      0x22, 0x04, 0x74, 0x65, 0x78, 0x74, // content field 4, "text"
      0x2a, 0x04, 0x68, 0x61, 0x73, 0x68, // content_hash field 5, "hash"
      0x30, 0x01, // status field 6, MANAGED
      0x38, 0x03, // tier field 7, BELIEF
      0x60, 0x01, // unavailable_reason field 12, recognised NONE
      0x60, 0x7f, // unavailable_reason field 12, unknown value 127
    ]);
  }

  /// The same note with a *status* no released server sends. A note the index
  /// holds in a state this build cannot name is not one search answers from
  /// either — the server's predicate is an allowlist of two.
  memorypb.MemoryNote unknownStatusNote() {
    return memorypb.MemoryNote.fromBuffer(const [
      0x0a, 0x06, 0x6e, 0x6f, 0x74, 0x65, 0x5f, 0x31, // note_id "note_1"
      // path field 2, "beliefs/01.md"
      0x12, 0x0d, 0x62, 0x65, 0x6c, 0x69, 0x65, 0x66, 0x73, 0x2f, 0x30, 0x31,
      0x2e, 0x6d, 0x64,
      0x22, 0x04, 0x74, 0x65, 0x78, 0x74, // content field 4, "text"
      0x2a, 0x04, 0x68, 0x61, 0x73, 0x68, // content_hash field 5, "hash"
      0x30, 0x01, // status field 6, recognised MANAGED
      0x30, 0x7f, // status field 6, unknown value 127
      0x38, 0x03, // tier field 7, BELIEF
      0x60, 0x01, // unavailable_reason field 12, NONE
    ]);
  }

  MemoryNote note({
    MemoryUnavailableReason unavailableReason = MemoryUnavailableReason.none,
    MemoryNoteStatus status = MemoryNoteStatus.managed,
    String parseError = '',
  }) {
    return MemoryNote(
      noteId: 'note_1',
      path: 'beliefs/01.md',
      title: 'Bees',
      content: 'The user keeps bees.',
      contentHash: 'sha256:note',
      status: status,
      tier: MemoryTier.belief,
      parseError: parseError,
      unavailableReason: unavailableReason,
    );
  }

  group('a note availability this build cannot name', () {
    test('decodes as unspecified rather than the value beside it', () {
      final proto = unknownReasonNote();

      expect(
        proto.unavailableReason,
        memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_NONE,
      );
      expect(
        GrpcMappers.memoryNoteToModel(proto).unavailableReason,
        MemoryUnavailableReason.unspecified,
      );
    });

    test('is not a note this page may say Turing can find', () {
      expect(GrpcMappers.memoryNoteToModel(unknownReasonNote()).isIndexable,
          isFalse);
      expect(GrpcMappers.memoryNoteToModel(unknownStatusNote()).isIndexable,
          isFalse);
    });
  });

  group('isIndexable is a whitelist', () {
    test('only the server saying nothing is wrong', () {
      expect(note().isIndexable, isTrue);
      for (final reason in MemoryUnavailableReason.values) {
        if (reason == MemoryUnavailableReason.none) continue;
        expect(
          note(unavailableReason: reason).isIndexable,
          isFalse,
          reason: '$reason is not the server reporting health',
        );
      }
    });

    test('only a status a search answers from', () {
      expect(note(status: MemoryNoteStatus.managed).isIndexable, isTrue);
      expect(note(status: MemoryNoteStatus.unmanaged).isIndexable, isTrue);
      // A withdrawn note is kept — the user accepted it — and search does not
      // answer with it, so the page must not imply it is findable.
      expect(note(status: MemoryNoteStatus.withdrawn).isIndexable, isFalse);
      expect(note(status: MemoryNoteStatus.unspecified).isIndexable, isFalse);
    });

    test('and never a note whose file could not be parsed', () {
      expect(note(parseError: 'frontmatter is malformed').isIndexable, isFalse);
    });
  });
}
