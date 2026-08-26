import 'package:fixnum/fixnum.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grpc/grpc.dart' as grpc;
import 'package:turing_flutter_app/generated/google/protobuf/timestamp.pb.dart'
    as timestamppb;
import 'package:turing_flutter_app/generated/turing/v1/chat.pb.dart' as chatpb;
import 'package:turing_flutter_app/generated/turing/v1/chat.pbgrpc.dart'
    as chatgrpc;
import 'package:turing_flutter_app/generated/turing/v1/common.pb.dart'
    as commonpb;
import 'package:turing_flutter_app/generated/turing/v1/memory.pb.dart'
    as memorypb;
import 'package:turing_flutter_app/generated/turing/v1/memory.pbgrpc.dart'
    as memorygrpc;
import 'package:turing_flutter_app/models/memory.dart';
import 'package:turing_flutter_app/models/remote_egress.dart';
import 'package:turing_flutter_app/networking/api_client.dart';
import 'package:turing_flutter_app/networking/grpc_client.dart';

void main() {
  Future<TuringGrpcApi> connect(grpc.Service service) async {
    final server = grpc.Server.create(services: [service]);
    await server.serve(address: '127.0.0.1', port: 0);
    final channel = grpc.ClientChannel(
      '127.0.0.1',
      port: server.port!,
      options: const grpc.ChannelOptions(
        credentials: grpc.ChannelCredentials.insecure(),
      ),
    );
    addTearDown(() async {
      await channel.shutdown();
      await server.shutdown();
    });
    return TuringGrpcApi(
      baseUrl: 'http://127.0.0.1:${server.port}',
      apiKey: 'client-key',
      channel: channel,
    );
  }

  test('listMemoryState maps settings, documents, tiers and proposals', () async {
    final api = await connect(_MemoryService());

    final state = await api.listMemoryState();

    expect(state.settings.enabled, isTrue);
    expect(state.settings.vaultRoot, '/memory');
    expect(state.settings.vaultWritable, isTrue);
    expect(state.persona.content, '# Persona\n\nBe direct.\n');
    expect(state.persona.contentHash, 'sha256:persona');
    expect(state.persona.status, MemoryNoteStatus.unmanaged);
    expect(state.profile.contentHash, 'sha256:profile');
    expect(state.profile.unavailableReason, MemoryUnavailableReason.none);
    expect(state.tiers.map((tier) => tier.tier), [
      MemoryTier.persona,
      MemoryTier.profile,
      MemoryTier.belief,
    ]);
    expect(state.notes.single.title, 'Ada');
    expect(state.notes.single.provenance.single.withdrawn, isTrue);
    expect(state.candidates.single.candidateId, 'cand-1');
    expect(state.candidates.single.managed, isTrue);
    expect(state.candidates.single.content, 'They bike to work.');
    expect(state.candidates.single.createdAt, isNotNull);
  });

  test('the pinned documents are saved under compare-and-set', () async {
    final service = _MemoryService();
    final api = await connect(service);

    final persona = await api.saveMemoryPersona(
      content: '# Persona\n\nBe warmer.\n',
      expectedContentHash: 'sha256:persona',
    );
    final profile = await api.saveMemoryProfile(
      content: '# Profile\n\nBikes to work.\n',
      expectedContentHash: 'sha256:profile',
    );

    expect(service.personaSaves.single.content, '# Persona\n\nBe warmer.\n');
    expect(service.personaSaves.single.expectedContentHash, 'sha256:persona');
    expect(service.profileSaves.single.content, '# Profile\n\nBikes to work.\n');
    expect(service.profileSaves.single.expectedContentHash, 'sha256:profile');
    expect(persona.content, '# Persona\n\nBe warmer.\n');
    expect(profile.content, '# Profile\n\nBikes to work.\n');
  });

  test('decisions forward the hash the user read', () async {
    final service = _MemoryService();
    final api = await connect(service);

    await api.promoteMemoryCandidate(
      candidateId: 'cand-1',
      expectedContentHash: 'sha256:cand',
      expectedCandidateHash: 'sha256:file-1',
    );
    await api.rejectMemoryCandidate(
      candidateId: 'cand-2',
      expectedContentHash: 'sha256:cand-2',
      expectedCandidateHash: 'sha256:file-2',
    );
    await api.applyMemoryProfile(
      candidateId: 'cand-3',
      content: '# Profile\n\nApplied.\n',
      expectedContentHash: 'sha256:profile',
      expectedCandidateHash: 'sha256:file-3',
    );

    expect(service.promotions.single.candidateId, 'cand-1');
    expect(service.promotions.single.expectedContentHash, 'sha256:cand');
    expect(service.rejections.single.expectedContentHash, 'sha256:cand-2');
    expect(service.applies.single.candidateId, 'cand-3');
    expect(service.applies.single.expectedContentHash, 'sha256:profile');
    // The second compare-and-set: the exact inbox bytes the decision was made
    // against. A client that dropped it would have the server accept a
    // decision about a proposal that had since been rewritten in the vault.
    expect(service.promotions.single.expectedCandidateHash, 'sha256:file-1');
    expect(service.rejections.single.expectedCandidateHash, 'sha256:file-2');
    expect(service.applies.single.expectedCandidateHash, 'sha256:file-3');
  });

  test('setMemoryEnabled toggles memory as a whole', () async {
    final service = _MemoryService();
    final api = await connect(service);

    final settings = await api.setMemoryEnabled(enabled: false);

    expect(service.toggles.single.enabled, isFalse);
    expect(
      service.toggles.single.tier,
      commonpb.MemoryTier.MEMORY_TIER_UNSPECIFIED,
      reason: 'this client does not ask for a per-tier toggle',
    );
    expect(settings.enabled, isFalse);
    expect(settings.unavailableReason, MemoryUnavailableReason.disabled);
  });

  test('a refused save arrives as a typed exception, not a raw gRPC error', () async {
    final api = await connect(_MemoryService()..refuseSaves = true);

    await expectLater(
      api.saveMemoryPersona(content: 'text', expectedContentHash: 'stale'),
      throwsA(
        isA<TuringApiException>()
            .having((error) => error.code, 'code', 'failed_precondition')
            .having(
              (error) => error.message,
              'message',
              contains('re-read'),
            ),
      ),
    );
  });

  test('prepareRemoteEgress maps every disclosed memory field', () async {
    final api = await connect(_MemoryDisclosureChatService());

    final disclosure = await api.prepareRemoteEgress(
      sessionId: 'session-1',
      content: 'hello',
      modelProvider: 'openai_compatible',
      idempotencyKey: 'idem-1',
    );

    expect(disclosure, isNotNull);
    expect(disclosure!.memoryProfileMayBeSent, isTrue);
    expect(disclosure.memoryNotes, hasLength(2));
    expect(disclosure.memoryNotes[0].title, 'persona.md');
    expect(disclosure.memoryNotes[0].vaultPath, 'persona.md');
    expect(disclosure.memoryNotes[0].tier, MemoryEgressTier.persona);
    expect(disclosure.memoryNotes[0].bodyMayBeSent, isTrue);
    expect(disclosure.memoryNotes[1].tier, MemoryEgressTier.belief);
    expect(disclosure.memoryNotes[1].bodyMayBeSent, isFalse);
  });
}

class _MemoryService extends memorygrpc.MemoryServiceBase {
  final List<memorypb.SaveMemoryPersonaRequest> personaSaves = [];
  final List<memorypb.SaveMemoryProfileRequest> profileSaves = [];
  final List<memorypb.PromoteMemoryCandidateRequest> promotions = [];
  final List<memorypb.RejectMemoryCandidateRequest> rejections = [];
  final List<memorypb.ApplyMemoryProfileRequest> applies = [];
  final List<memorypb.SetMemoryEnabledRequest> toggles = [];
  bool refuseSaves = false;

  @override
  Future<memorypb.ListMemoryStateResponse> listMemoryState(
    grpc.ServiceCall call,
    memorypb.ListMemoryStateRequest request,
  ) async {
    return memorypb.ListMemoryStateResponse(
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
      ),
      profile: memorypb.MemoryProfile(
        content: '# Profile\n\nBikes to work.\n',
        contentHash: 'sha256:profile',
        status: memorypb.MemoryNoteStatus.MEMORY_NOTE_STATUS_MANAGED,
        unavailableReason:
            memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_NONE,
      ),
      tiers: [
        memorypb.MemoryTierState(
          tier: commonpb.MemoryTier.MEMORY_TIER_PERSONA,
          enabled: true,
          noteCount: 1,
        ),
        memorypb.MemoryTierState(
          tier: commonpb.MemoryTier.MEMORY_TIER_PROFILE,
          enabled: true,
          noteCount: 1,
        ),
        memorypb.MemoryTierState(
          tier: commonpb.MemoryTier.MEMORY_TIER_BELIEF,
          enabled: true,
          noteCount: 1,
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
              withdrawn: true,
            ),
          ],
        ),
      ],
      candidates: [
        memorypb.MemoryCandidate(
          candidateId: 'cand-1',
          kind: memorypb.MemoryCandidateKind.MEMORY_CANDIDATE_KIND_BELIEF,
          inboxPath: 'inbox/01-bikes.md',
          content: 'They bike to work.',
          contentHash: 'sha256:cand',
          state: memorypb.MemoryCandidateState.MEMORY_CANDIDATE_STATE_PENDING,
          managed: true,
          createdAt: timestamppb.Timestamp(seconds: Int64(1766000000)),
        ),
      ],
    );
  }

  @override
  Future<memorypb.MemorySettings> setMemoryEnabled(
    grpc.ServiceCall call,
    memorypb.SetMemoryEnabledRequest request,
  ) async {
    toggles.add(request);
    return memorypb.MemorySettings(
      enabled: request.enabled,
      vaultRoot: '/memory',
      vaultWritable: true,
      unavailableReason: request.enabled
          ? memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_NONE
          : memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_DISABLED,
    );
  }

  @override
  Future<memorypb.SaveMemoryPersonaResponse> saveMemoryPersona(
    grpc.ServiceCall call,
    memorypb.SaveMemoryPersonaRequest request,
  ) async {
    if (refuseSaves) {
      throw grpc.GrpcError.failedPrecondition(
        'the file changed on disk since this editor read it; finish and close '
        'the memory editor, re-read the document, and save again',
      );
    }
    personaSaves.add(request);
    return memorypb.SaveMemoryPersonaResponse(
      persona: memorypb.MemoryPersona(
        content: request.content,
        contentHash: 'sha256:persona-2',
        status: memorypb.MemoryNoteStatus.MEMORY_NOTE_STATUS_UNMANAGED,
        unavailableReason:
            memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_NONE,
      ),
    );
  }

  @override
  Future<memorypb.SaveMemoryProfileResponse> saveMemoryProfile(
    grpc.ServiceCall call,
    memorypb.SaveMemoryProfileRequest request,
  ) async {
    profileSaves.add(request);
    return memorypb.SaveMemoryProfileResponse(
      profile: memorypb.MemoryProfile(
        content: request.content,
        contentHash: 'sha256:profile-2',
        status: memorypb.MemoryNoteStatus.MEMORY_NOTE_STATUS_MANAGED,
        unavailableReason:
            memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_NONE,
      ),
    );
  }

  @override
  Future<memorypb.PromoteMemoryCandidateResponse> promoteMemoryCandidate(
    grpc.ServiceCall call,
    memorypb.PromoteMemoryCandidateRequest request,
  ) async {
    promotions.add(request);
    return memorypb.PromoteMemoryCandidateResponse(
      candidate: memorypb.MemoryCandidate(
        candidateId: request.candidateId,
        state: memorypb.MemoryCandidateState.MEMORY_CANDIDATE_STATE_PROMOTED,
        managed: true,
      ),
    );
  }

  @override
  Future<memorypb.RejectMemoryCandidateResponse> rejectMemoryCandidate(
    grpc.ServiceCall call,
    memorypb.RejectMemoryCandidateRequest request,
  ) async {
    rejections.add(request);
    return memorypb.RejectMemoryCandidateResponse(
      candidate: memorypb.MemoryCandidate(
        candidateId: request.candidateId,
        state: memorypb.MemoryCandidateState.MEMORY_CANDIDATE_STATE_REJECTED,
        managed: true,
      ),
    );
  }

  @override
  Future<memorypb.ApplyMemoryProfileResponse> applyMemoryProfile(
    grpc.ServiceCall call,
    memorypb.ApplyMemoryProfileRequest request,
  ) async {
    applies.add(request);
    return memorypb.ApplyMemoryProfileResponse(
      profile: memorypb.MemoryProfile(
        content: request.content,
        contentHash: 'sha256:profile-3',
        status: memorypb.MemoryNoteStatus.MEMORY_NOTE_STATUS_MANAGED,
        unavailableReason:
            memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_NONE,
      ),
    );
  }

  @override
  Future<memorypb.MemorySettings> getMemorySettings(
    grpc.ServiceCall call,
    memorypb.GetMemorySettingsRequest request,
  ) async => throw grpc.GrpcError.unimplemented('not exercised by this test');

  @override
  Future<memorypb.ListMemoryCandidatesResponse> listMemoryCandidates(
    grpc.ServiceCall call,
    memorypb.ListMemoryCandidatesRequest request,
  ) async => throw grpc.GrpcError.unimplemented('not exercised by this test');

  @override
  Future<memorypb.MemoryCandidate> getMemoryCandidate(
    grpc.ServiceCall call,
    memorypb.GetMemoryCandidateRequest request,
  ) async => throw grpc.GrpcError.unimplemented('not exercised by this test');

  @override
  Future<memorypb.MemoryProfile> getMemoryProfile(
    grpc.ServiceCall call,
    memorypb.GetMemoryProfileRequest request,
  ) async => throw grpc.GrpcError.unimplemented('not exercised by this test');

  @override
  Future<memorypb.MemoryPersona> getMemoryPersona(
    grpc.ServiceCall call,
    memorypb.GetMemoryPersonaRequest request,
  ) async => throw grpc.GrpcError.unimplemented('not exercised by this test');

  // The internal facet. A public client must never reach these, and this fake
  // refuses them the way the real server's public facet does.
  @override
  Future<memorypb.ListMemoryToolsResponse> listMemoryTools(
    grpc.ServiceCall call,
    memorypb.ListMemoryToolsRequest request,
  ) async => throw grpc.GrpcError.permissionDenied('memory tool discovery is internal');

  @override
  Future<memorypb.CallMemoryToolResponse> callMemoryTool(
    grpc.ServiceCall call,
    memorypb.CallMemoryToolRequest request,
  ) async => throw grpc.GrpcError.permissionDenied('memory tool dispatch is internal');
}

class _MemoryDisclosureChatService extends chatgrpc.ChatServiceBase {
  @override
  Future<chatpb.PrepareRemoteEgressResponse> prepareRemoteEgress(
    grpc.ServiceCall call,
    chatpb.PrepareRemoteEgressRequest request,
  ) async {
    return chatpb.PrepareRemoteEgressResponse(
      disclosure: commonpb.RemoteEgressDisclosure(
        challenge: 'challenge',
        provider: commonpb.ModelProvider.MODEL_PROVIDER_OPENAI_COMPATIBLE,
        model: 'remote-model',
        endpoint: 'https://models.example/v1',
        endpointHost: 'models.example',
        dataCategories: [
          commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_MEMORY_PROFILE,
        ],
        expiresAt: timestamppb.Timestamp.fromDateTime(
          DateTime.utc(2026, 8, 24, 12),
        ),
        memoryProfileMayBeSent: true,
        memoryNotes: [
          commonpb.MemoryEgressDisclosure(
            title: 'persona.md',
            vaultPath: 'persona.md',
            tier: commonpb.MemoryTier.MEMORY_TIER_PERSONA,
            bodyMayBeSent: true,
          ),
          commonpb.MemoryEgressDisclosure(
            noteId: 'note-1',
            title: 'Ada',
            vaultPath: 'beliefs/people/ada.md',
            tier: commonpb.MemoryTier.MEMORY_TIER_BELIEF,
            bodyMayBeSent: false,
          ),
        ],
      ),
    );
  }

  @override
  Stream<chatpb.ChatStreamEvent> sendMessage(
    grpc.ServiceCall call,
    chatpb.SendMessageRequest request,
  ) async* {
    throw grpc.GrpcError.unimplemented('not exercised by this test');
  }
}
