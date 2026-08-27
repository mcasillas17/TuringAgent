import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/workspace/memory_page.dart';
import 'package:turing_flutter_app/generated/turing/v1/common.pb.dart'
    as commonpb;
import 'package:turing_flutter_app/generated/turing/v1/memory.pb.dart'
    as memorypb;
import 'package:turing_flutter_app/l10n/generated/app_localizations.dart';
import 'package:turing_flutter_app/models/grpc_mappers.dart';
import 'package:turing_flutter_app/models/agent_descriptor.dart';
import 'package:turing_flutter_app/models/memory.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/models/search_hit.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/session_deletion.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';
import 'package:turing_flutter_app/models/turing_event.dart';
import 'package:turing_flutter_app/networking/api_client.dart';

import '../support/no_audit_api.dart';
import '../support/no_automations_api.dart';
import '../support/no_external_agents_api.dart';
import '../support/no_integrations_api.dart';
import '../support/no_session_lifecycle_api.dart';
import '../support/no_skills_api.dart';
import '../support/no_telemetry_api.dart';

/// The Memory page and the wire, joined.
///
/// The unit tests either hand the page a hand-built model or hand the mapper a
/// hand-built message. Neither notices when the two disagree about what the
/// server actually sends — which is exactly where "evidence_count is always 1"
/// hid. These pump the page from real protobuf messages.
void main() {
  testWidgets('a belief the server sends with no evidence renders as such', (
    tester,
  ) async {
    final api = _WireMemoryApi(
      memorypb.ListMemoryStateResponse(
        settings: memorypb.MemorySettings(
          enabled: true,
          vaultRoot: '/memory',
          vaultWritable: true,
          unavailableReason:
              memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_NONE,
        ),
        notes: [
          memorypb.MemoryNote(
            noteId: 'note-1',
            path: 'beliefs/people/ada.md',
            title: 'Ada',
            content: 'They take their coffee black.',
            contentHash: 'sha256:note-1',
            status: memorypb.MemoryNoteStatus.MEMORY_NOTE_STATUS_MANAGED,
            tier: commonpb.MemoryTier.MEMORY_TIER_BELIEF,
            unavailableReason:
                memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_NONE,
          ),
        ],
      ),
    );

    await _pump(tester, api);

    expect(find.text('Ada'), findsOneWidget);
    expect(find.textContaining('no evidence'), findsOneWidget);
  });

  testWidgets('a withdrawn belief the server sends counts zero evidence', (
    tester,
  ) async {
    final api = _WireMemoryApi(
      memorypb.ListMemoryStateResponse(
        settings: memorypb.MemorySettings(
          enabled: true,
          vaultRoot: '/memory',
          vaultWritable: true,
          unavailableReason:
              memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_NONE,
        ),
        notes: [
          memorypb.MemoryNote(
            noteId: 'note-1',
            path: 'beliefs/people/ada.md',
            title: 'Ada',
            content: 'They take their coffee black.',
            contentHash: 'sha256:note-1',
            status: memorypb.MemoryNoteStatus.MEMORY_NOTE_STATUS_WITHDRAWN,
            tier: commonpb.MemoryTier.MEMORY_TIER_BELIEF,
            unavailableReason:
                memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_NONE,
            provenance: [
              memorypb.MemoryProvenance(
                kind: memorypb
                    .MemoryProvenanceKind
                    .MEMORY_PROVENANCE_KIND_PROMOTED_FROM_CANDIDATE,
                withdrawn: true,
              ),
            ],
          ),
        ],
      ),
    );

    await _pump(tester, api);

    final note = api.state.notes.single;
    expect(note.provenance.single.evidenceCount, 0);
    expect(note.provenance.single.sourceSessionId, isEmpty);
    expect(find.textContaining('withdrawn'), findsWidgets);
    expect(
      find.textContaining('From '),
      findsNothing,
      reason: 'a deleted conversation has no name left to show',
    );
  });

  testWidgets('a belief with two citations says two, not one', (tester) async {
    final api = _WireMemoryApi(
      memorypb.ListMemoryStateResponse(
        settings: memorypb.MemorySettings(
          enabled: true,
          vaultRoot: '/memory',
          vaultWritable: true,
          unavailableReason:
              memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_NONE,
        ),
        notes: [
          memorypb.MemoryNote(
            noteId: 'note-1',
            path: 'beliefs/people/ada.md',
            title: 'Ada',
            content: 'They take their coffee black.',
            contentHash: 'sha256:note-1',
            status: memorypb.MemoryNoteStatus.MEMORY_NOTE_STATUS_MANAGED,
            tier: commonpb.MemoryTier.MEMORY_TIER_BELIEF,
            unavailableReason:
                memorypb.MemoryUnavailableReason.MEMORY_UNAVAILABLE_REASON_NONE,
            provenance: [
              memorypb.MemoryProvenance(
                kind: memorypb
                    .MemoryProvenanceKind
                    .MEMORY_PROVENANCE_KIND_PROMOTED_FROM_CANDIDATE,
                sourceSessionId: 'sess-1',
                sourceSessionTitle: 'Coffee talk',
                evidenceCount: 2,
              ),
            ],
          ),
        ],
      ),
    );

    await _pump(tester, api);

    expect(find.textContaining('2 pieces of evidence'), findsOneWidget);
    expect(find.textContaining('Coffee talk'), findsOneWidget);
  });
}

Future<void> _pump(WidgetTester tester, _WireMemoryApi api) async {
  tester.view.physicalSize = const Size(1200, 900);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(
    MaterialApp(
      localizationsDelegates: AppLocalizations.localizationsDelegates,
      supportedLocales: AppLocalizations.supportedLocales,
      home: Scaffold(body: MemoryPage(apiClient: api)),
    ),
  );
  await tester.pumpAndSettle();
}

class _WireMemoryApi extends TuringApi
    with
        NoAuditApi,
        NoAutomationsApi,
        NoExternalAgentsApi,
        NoIntegrationsApi,
        NoSessionLifecycleApi,
        NoSkillsApi,
        NoTelemetryApi {
  _WireMemoryApi(this.wire) : state = GrpcMappers.memoryStateToModel(wire);

  final memorypb.ListMemoryStateResponse wire;
  final MemoryState state;

  @override
  Future<MemoryState> listMemoryState() async => state;

  @override
  Future<Map<String, dynamic>> getConfig() async => const {};

  @override
  Future<Map<String, dynamic>> createSession({String? title}) async => const {};

  @override
  Future<List<Session>> listSessions({int limit = 50, String? after}) async =>
      const [];

  @override
  Future<Session> getSession({required String sessionId}) async =>
      throw UnimplementedError();

  @override
  Future<SessionDeletionReceipt> deleteSession({
    required String sessionId,
  }) async => const SessionDeletionReceipt.completed();

  @override
  Future<List<Message>> listMessages({
    required String sessionId,
    int limit = 50,
    String? before,
  }) async => const [];

  @override
  Future<List<SearchHit>> searchMessages({
    required String query,
    int limit = 50,
  }) async => const [];

  @override
  Future<TuringEventPage> listEvents({
    required String sessionId,
    int? after,
    int limit = 500,
  }) async => const TuringEventPage(events: [], latestSequence: 0);

  @override
  Future<Map<String, dynamic>> sendMessage({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
    String? idempotencyKey,
  }) async => const {};

  @override
  Future<Map<String, dynamic>> approveApproval(
    String approvalId, {
    String? comment,
  }) async => const {};

  @override
  Future<Map<String, dynamic>> denyApproval(
    String approvalId, {
    String? reason,
  }) async => const {};

  @override
  Future<List<ToolDescriptor>> listTools() async => const [];

  @override
  Future<List<AgentDescriptor>> listAgents() async => const [];
}
