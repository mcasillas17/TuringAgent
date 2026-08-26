import 'package:turing_flutter_app/models/memory.dart';

/// Fills in the memory surface for fakes belonging to tests that are not about
/// memory. Reads answer with an empty, healthy-looking vault; every write fails
/// loudly, because a test that silently writes the user's persona is a test
/// that is lying about what it exercises.
mixin NoMemoryApi {
  Future<MemoryState> listMemoryState() async => MemoryState(
    settings: const MemorySettings(
      enabled: false,
      unavailableReason: MemoryUnavailableReason.disabled,
    ),
    persona: const MemoryDocument(
      status: MemoryNoteStatus.unmanaged,
      unavailableReason: MemoryUnavailableReason.disabled,
    ),
    profile: const MemoryDocument(
      unavailableReason: MemoryUnavailableReason.disabled,
    ),
  );

  Future<MemorySettings> setMemoryEnabled({required bool enabled}) async =>
      throw UnimplementedError('this test does not exercise memory');

  Future<MemoryCandidate> promoteMemoryCandidate({
    required String candidateId,
    required String expectedCandidateHash,
  }) async => throw UnimplementedError('this test does not exercise memory');

  Future<MemoryCandidate> rejectMemoryCandidate({
    required String candidateId,
    String expectedCandidateHash = '',
    String reason = '',
  }) async => throw UnimplementedError('this test does not exercise memory');

  Future<MemoryApplyResult> applyMemoryProfile({
    required String candidateId,
    required String content,
    required String expectedContentHash,
    String expectedCandidateHash = '',
  }) async => throw UnimplementedError('this test does not exercise memory');

  Future<MemoryDocument> saveMemoryPersona({
    required String content,
    required String expectedContentHash,
  }) async => throw UnimplementedError('this test does not exercise memory');

  Future<MemoryDocument> saveMemoryProfile({
    required String content,
    required String expectedContentHash,
  }) async => throw UnimplementedError('this test does not exercise memory');
}
