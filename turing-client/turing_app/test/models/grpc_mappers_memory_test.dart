import 'package:fixnum/fixnum.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/generated/google/protobuf/timestamp.pb.dart'
    as timestamppb;
import 'package:turing_flutter_app/generated/turing/v1/common.pb.dart'
    as commonpb;
import 'package:turing_flutter_app/generated/turing/v1/memory.pb.dart'
    as memorypb;
import 'package:turing_flutter_app/models/grpc_mappers.dart';
import 'package:turing_flutter_app/models/memory.dart';

void main() {
  test('memory state mapper carries every tier the page renders', () {
    final state = GrpcMappers.memoryStateToModel(
      memorypb.ListMemoryStateResponse(
        settings: memorypb.MemorySettings(
          enabled: true,
          vaultRoot: '/memory',
          vaultWritable: true,
          unavailableReason:
              memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_NONE,
        ),
        persona: memorypb.MemoryPersona(
          content: '# Persona\n\nBe direct.\n',
          contentHash: 'sha256:persona',
          status: memorypb.MemoryNoteStatus.MEMORY_NOTE_STATUS_UNMANAGED,
          unavailableReason:
              memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_NONE,
          updatedAt: timestamppb.Timestamp(seconds: Int64(1766000000)),
        ),
        profile: memorypb.MemoryProfile(
          content: '# Profile\n\nBikes to work.\n',
          contentHash: 'sha256:profile',
          status: memorypb.MemoryNoteStatus.MEMORY_NOTE_STATUS_MANAGED,
          unavailableReason: memorypb
              .MemoryUnavailableReason
              .MEMORY_UNAVAILABLE_REASON_CONTENT_PARSE_FAILED,
          parseError: 'frontmatter is malformed',
        ),
        tiers: [
          memorypb.MemoryTierState(
            tier: commonpb.MemoryTier.MEMORY_TIER_BELIEF,
            enabled: true,
            noteCount: 2,
            pendingCandidateCount: 1,
          ),
        ],
        notes: [
          memorypb.MemoryNote(
            noteId: 'note-1',
            path: 'beliefs/people/ada.md',
            title: 'Ada',
            content: 'They take their coffee black.',
            contentHash: 'sha256:note',
            status: memorypb.MemoryNoteStatus.MEMORY_NOTE_STATUS_MANAGED,
            tier: commonpb.MemoryTier.MEMORY_TIER_BELIEF,
            provenance: [
              memorypb.MemoryProvenance(
                kind: memorypb
                    .MemoryProvenanceKind
                    .MEMORY_PROVENANCE_KIND_PROMOTED_FROM_CANDIDATE,
                sourceSessionId: 'sess-1',
                sourceSessionTitle: 'Coffee talk',
                withdrawn: true,
                evidenceCount: 0,
              ),
            ],
          ),
        ],
        candidates: [
          memorypb.MemoryCandidate(
            candidateId: 'cand-1',
            kind: memorypb.MemoryCandidateKind.MEMORY_CANDIDATE_KIND_BELIEF,
            inboxPath: 'inbox/01-ada.md',
            content: 'They bike to work.',
            contentHash: 'sha256:cand',
            state: memorypb.MemoryCandidateState.MEMORY_CANDIDATE_STATE_PENDING,
            managed: true,
          ),
        ],
      ),
    );

    expect(state.settings.enabled, isTrue);
    expect(state.settings.vaultRoot, '/memory');
    expect(state.persona.content, '# Persona\n\nBe direct.\n');
    expect(state.persona.contentHash, 'sha256:persona');
    expect(state.persona.status, MemoryNoteStatus.unmanaged);
    expect(state.persona.updatedAt, isNotNull);
    expect(
      state.profile.unavailableReason,
      MemoryUnavailableReason.contentParseFailed,
    );
    expect(state.profile.parseError, 'frontmatter is malformed');
    expect(state.profile.updatedAt, isNull);
    expect(state.tiers.single.tier, MemoryTier.belief);
    expect(state.tiers.single.noteCount, 2);
    expect(state.tiers.single.pendingCandidateCount, 1);
    expect(state.notes.single.noteId, 'note-1');
    expect(state.notes.single.provenance.single.withdrawn, isTrue);
    expect(state.notes.single.provenance.single.evidenceCount, 0);
    expect(state.candidates.single.candidateId, 'cand-1');
    expect(state.candidates.single.kind, MemoryCandidateKind.belief);
    expect(state.candidates.single.managed, isTrue);
    expect(state.candidates.single.content, 'They bike to work.');
  });

  test('an unset document is reported as absent, never as healthy', () {
    final state = GrpcMappers.memoryStateToModel(
      memorypb.ListMemoryStateResponse(),
    );

    expect(state.persona.content, isEmpty);
    expect(
      state.persona.unavailableReason,
      MemoryUnavailableReason.unspecified,
      reason: 'a server that said nothing must not be rendered as saying NONE',
    );
    expect(
      state.profile.unavailableReason,
      MemoryUnavailableReason.unspecified,
    );
    expect(state.settings.enabled, isFalse);
    expect(state.tiers, isEmpty);
    expect(state.candidates, isEmpty);
  });

  test('enum values this build does not know become unspecified', () {
    // A newer backend sending an enum member this client was built before.
    final candidate = memorypb.MemoryCandidate(
      candidateId: 'cand-future',
      content: 'a proposal from a newer server',
    );
    candidate.unknownFields.mergeVarintField(2, Int64(4242));
    candidate.unknownFields.mergeVarintField(6, Int64(4242));
    candidate.unknownFields.mergeVarintField(13, Int64(4242));

    final model = GrpcMappers.memoryCandidateToModel(candidate);

    expect(model.kind, MemoryCandidateKind.unspecified);
    expect(model.state, MemoryCandidateState.unspecified);
    expect(model.unavailableReason, MemoryUnavailableReason.unspecified);
    expect(model.content, 'a proposal from a newer server');
  });

  test('withdrawn provenance keeps its timestamp and its counted evidence', () {
    final note = GrpcMappers.memoryNoteToModel(
      memorypb.MemoryNote(
        noteId: 'note-2',
        provenance: [
          memorypb.MemoryProvenance(
            kind: memorypb.MemoryProvenanceKind.MEMORY_PROVENANCE_KIND_IMPORTED,
            withdrawn: true,
            withdrawnAt: timestamppb.Timestamp(seconds: Int64(1766000000)),
            evidenceCount: 3,
          ),
        ],
      ),
    );

    final provenance = note.provenance.single;
    expect(provenance.kind, MemoryProvenanceKind.imported);
    expect(provenance.withdrawn, isTrue);
    expect(provenance.withdrawnAt, isNotNull);
    expect(provenance.evidenceCount, 3);
    expect(provenance.observedAt, isNull);
  });

  test('a pinned document carries the document and the pin separately', () {
    // content is the file, content_hash is a hash of the file, and the two
    // pinned_* fields are the separate statement about what a run carries.
    // Mapping the pin's bytes into content — or its hash into content_hash —
    // is what made a long document unsaveable: every compare-and-set the page
    // sent was a hash of text that is nowhere on disk.
    final document = GrpcMappers.memoryPersonaToModel(
      memorypb.MemoryPersona(
        content: '# Persona\n\nThe whole document, all of it.\n',
        contentHash: 'sha256:whole-document',
        status: memorypb.MemoryNoteStatus.MEMORY_NOTE_STATUS_UNMANAGED,
        unavailableReason:
            memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_NONE,
        pinnedTruncated: true,
        pinnedBytes: 4096,
      ),
    );

    expect(document.content, '# Persona\n\nThe whole document, all of it.\n');
    expect(document.contentHash, 'sha256:whole-document');
    expect(document.pinnedTruncated, isTrue);
    expect(document.pinnedBytes, 4096);
    expect(document.isWritable, isTrue);
  });

  test('a profile a run carries whole says so', () {
    final document = GrpcMappers.memoryProfileToModel(
      memorypb.MemoryProfile(
        content: '# Profile\n\nShort.\n',
        contentHash: 'sha256:profile',
        pinnedBytes: 19,
      ),
    );

    expect(document.pinnedTruncated, isFalse);
    expect(document.pinnedBytes, 19);
  });

  test('a document missing from the vault is offered as a first save', () {
    final document = GrpcMappers.memoryPersonaToModel(
      memorypb.MemoryPersona(
        unavailableReason: memorypb
            .MemoryUnavailableReason
            .MEMORY_UNAVAILABLE_REASON_VAULT_MISSING,
      ),
    );

    expect(document.unavailableReason, MemoryUnavailableReason.vaultMissing);
    expect(document.contentHash, isEmpty);
    expect(
      document.isWritable,
      isTrue,
      reason: 'a persona nobody has written yet is written by writing it',
    );
  });
}
