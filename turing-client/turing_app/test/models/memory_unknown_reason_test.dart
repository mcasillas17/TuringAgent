import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/generated/turing/v1/memory.pb.dart'
    as memorypb;
import 'package:turing_flutter_app/models/grpc_mappers.dart';
import 'package:turing_flutter_app/models/memory.dart';

/// A reason this build has never heard of.
///
/// Round 5: the whole-content test was a blacklist of the four reasons this
/// build knows are bad, so anything a newer server invents fell through it as
/// "readable" — and a proposal nobody had read was offered with a Promote
/// button and a compare-and-set token made of bytes the page never displayed.
/// The rule is a whitelist now: only a server that says, in a value this build
/// recognises, that nothing is wrong.
void main() {
  /// A candidate whose unavailable_reason is a value no released server sends,
  /// which is exactly what a client one version behind a new backend sees. The
  /// recognised PENDING/BELIEF pair is on the wire first so the unknown value
  /// is isolated to the one field under test.
  memorypb.MemoryCandidate unknownReasonCandidate() {
    return memorypb.MemoryCandidate.fromBuffer(const [
      0x0a, 0x06, 0x63, 0x61, 0x6e, 0x64, 0x5f, 0x31, // candidate_id "cand_1"
      0x10, 0x01, // kind field 2, BELIEF
      // inbox_path field 3, "inbox/01.md"
      0x1a, 0x0b, 0x69, 0x6e, 0x62, 0x6f, 0x78, 0x2f, 0x30, 0x31, 0x2e, 0x6d,
      0x64,
      0x22, 0x04, 0x74, 0x65, 0x78, 0x74, // content field 4, "text"
      0x2a, 0x04, 0x68, 0x61, 0x73, 0x68, // content_hash field 5, "hash"
      0x30, 0x01, // state field 6, PENDING
      0x68, 0x01, // unavailable_reason field 13, recognised NONE
      0x68, 0x7f, // unavailable_reason field 13, unknown value 127
      0x70, 0x01, // managed field 14, true
    ]);
  }

  group('an unavailable reason this build cannot name', () {
    test('is decoded as unspecified rather than the value beside it', () {
      final proto = unknownReasonCandidate();

      // protobuf keeps the last recognised value it parsed, which is why the
      // raw field cannot be trusted on its own.
      expect(
        proto.unavailableReason,
        memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_NONE,
      );
      expect(
        GrpcMappers.memoryCandidateToModel(proto).unavailableReason,
        MemoryUnavailableReason.unspecified,
      );
    });

    test('is not a proposal this page may say it showed whole', () {
      final candidate = GrpcMappers.memoryCandidateToModel(
        unknownReasonCandidate(),
      );

      expect(candidate.contentIsWhole, isFalse);
    });

    test('offers no decision at all, not even a rejection', () {
      final candidate = GrpcMappers.memoryCandidateToModel(
        unknownReasonCandidate(),
      );

      // A rejection is only safe where this build knows what went wrong: the
      // file is there and its contents are the problem. An unnamed reason
      // could be anything, including one where throwing the file away is the
      // wrong answer — so the card says it cannot decide instead of guessing.
      expect(candidate.decision, MemoryCandidateDecision.unsupported);
      expect(candidate.isDecidable, isFalse);
    });

    test('makes no compare-and-set claim about bytes it never saw', () {
      final candidate = GrpcMappers.memoryCandidateToModel(
        unknownReasonCandidate(),
      );

      expect(candidate.rejectionHash, '');
    });
  });

  group('a reason this build does know', () {
    MemoryCandidate candidate(MemoryUnavailableReason reason) {
      return MemoryCandidate(
        candidateId: 'cand-1',
        kind: MemoryCandidateKind.belief,
        inboxPath: 'inbox/proposal.md',
        content: '',
        contentHash: '',
        state: MemoryCandidateState.pending,
        managed: true,
        unavailableReason: reason,
      );
    }

    test('still offers a rejection when the file itself is reachable', () {
      // The vault is open and the file is sitting in it; only its contents
      // defeated the reader. Removing it by name is something the server can
      // still do, and it is the only way out that proposal has.
      expect(
        candidate(MemoryUnavailableReason.contentParseFailed).decision,
        MemoryCandidateDecision.rejectOnly,
      );
      expect(
        candidate(MemoryUnavailableReason.contentTooLarge).decision,
        MemoryCandidateDecision.rejectOnly,
      );
    });

    test('offers nothing when the vault itself is out of reach', () {
      // No vault means no rejection either: the server needs the vault to
      // remove the file, so a Reject button here is an action it refuses.
      expect(
        candidate(MemoryUnavailableReason.vaultMissing).decision,
        MemoryCandidateDecision.unsupported,
      );
      expect(
        candidate(MemoryUnavailableReason.vaultUnreadable).decision,
        MemoryCandidateDecision.unsupported,
      );
    });

    test('offers the accept it always did when nothing is wrong', () {
      final whole = MemoryCandidate(
        candidateId: 'cand-1',
        kind: MemoryCandidateKind.belief,
        inboxPath: 'inbox/proposal.md',
        content: 'They bike to work.',
        contentHash: 'sha256:file',
        state: MemoryCandidateState.pending,
        managed: true,
        unavailableReason: MemoryUnavailableReason.none,
      );

      expect(whole.contentIsWhole, isTrue);
      expect(whole.decision, MemoryCandidateDecision.promoteToBeliefs);
      expect(whole.rejectionHash, 'sha256:file');
    });
  });
}
