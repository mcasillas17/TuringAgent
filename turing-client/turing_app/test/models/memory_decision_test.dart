import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/models/memory.dart';

/// What the page may offer for one proposal.
///
/// Three of these are round-3 findings, and all three are about a card that
/// tells the user the truth about a file in their own vault: a proposal nobody
/// can read is still theirs to throw away, an inbox file Turing lost the record
/// of is not a draft they wrote, and an apply the server has already claimed is
/// not a decision waiting on them.
void main() {
  MemoryCandidate candidate({
    String candidateId = 'cand-1',
    MemoryCandidateKind kind = MemoryCandidateKind.belief,
    MemoryCandidateState state = MemoryCandidateState.pending,
    bool managed = true,
    bool untracked = false,
    String parseError = '',
    MemoryUnavailableReason unavailableReason = MemoryUnavailableReason.none,
  }) {
    return MemoryCandidate(
      candidateId: candidateId,
      kind: kind,
      inboxPath: 'inbox/proposal.md',
      content: 'They bike to work.',
      contentHash: 'sha256:file',
      state: state,
      managed: managed,
      untracked: untracked,
      parseError: parseError,
      unavailableReason: unavailableReason,
    );
  }

  group('a proposal the page could not read', () {
    test('may still be rejected, and nothing else', () {
      final unreadable = candidate(
        parseError: 'frontmatter is not terminated',
        unavailableReason: MemoryUnavailableReason.contentParseFailed,
      );

      expect(unreadable.decision, MemoryCandidateDecision.rejectOnly);
      expect(unreadable.isDecidable, isTrue);
    });

    test('is reject-only whatever kind the row remembers', () {
      final profileEdit = candidate(
        kind: MemoryCandidateKind.profileEdit,
        unavailableReason: MemoryUnavailableReason.contentParseFailed,
      );

      expect(profileEdit.decision, MemoryCandidateDecision.rejectOnly);
    });

    test('makes no compare-and-set claim about bytes it never saw', () {
      final unreadable = candidate(
        parseError: 'frontmatter is not terminated',
        unavailableReason: MemoryUnavailableReason.contentParseFailed,
      );

      // The hash the listing served is of bytes the page could not display.
      // Sending it would be answering a question this client cannot answer, and
      // the server refuses a claim it cannot check against an unreadable file.
      expect(unreadable.rejectionHash, '');
    });

    test('still sends the hash when the proposal was shown whole', () {
      expect(candidate().rejectionHash, 'sha256:file');
    });

    test('is not decidable once it has been decided', () {
      final decided = candidate(
        state: MemoryCandidateState.rejected,
        unavailableReason: MemoryUnavailableReason.contentParseFailed,
      );

      expect(decided.decision, MemoryCandidateDecision.none);
    });
  });

  group('an inbox file with no row', () {
    test('offers no decision and is marked untracked', () {
      final orphan = candidate(
        candidateId: '',
        kind: MemoryCandidateKind.unspecified,
        state: MemoryCandidateState.unspecified,
        managed: false,
        untracked: true,
      );

      expect(orphan.decision, MemoryCandidateDecision.none);
      expect(orphan.untracked, isTrue);
    });

    test('is not the same thing as a draft the user wrote', () {
      final draft = candidate(
        candidateId: '',
        kind: MemoryCandidateKind.unspecified,
        state: MemoryCandidateState.unspecified,
        managed: false,
      );

      expect(draft.untracked, isFalse);
    });
  });

  group('an apply the server has claimed', () {
    test('offers nothing, because the decision was already taken', () {
      final claimed = candidate(
        kind: MemoryCandidateKind.profileEdit,
        state: MemoryCandidateState.profileApplying,
      );

      expect(claimed.decision, MemoryCandidateDecision.none);
      expect(claimed.isDecidable, isFalse);
    });
  });
}
