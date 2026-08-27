import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/workspace/memory_page.dart';
import 'package:turing_flutter_app/l10n/generated/app_localizations.dart';
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

/// What the inbox says about files the user can open themselves.
///
/// Three shapes the page used to get wrong. A proposal nobody could parse got
/// no buttons at all, which left a claim about the user sitting in their vault
/// with no way out of it. A file Turing wrote and lost the record of was left
/// off the page entirely. And an apply the server had already claimed was
/// indistinguishable from one still waiting on them.
void main() {
  group('a proposal the page could not read', () {
    testWidgets('offers Reject and nothing else', (tester) async {
      final api = _Api()..state = _stateWith(_unreadableProposal());
      await _pump(tester, api);

      expect(find.text('Reject'), findsOneWidget);
      expect(
        find.text('Promote'),
        findsNothing,
        reason: 'nobody may accept text the page could not show them',
      );
      expect(find.text('Apply'), findsNothing);
    });

    testWidgets('rejects it without claiming to have read it', (tester) async {
      final api = _Api()..state = _stateWith(_unreadableProposal());
      await _pump(tester, api);

      await tester.tap(find.text('Reject'));
      await tester.pumpAndSettle();

      expect(api.rejections.single.$1, 'cand-unreadable');
      expect(
        api.rejections.single.$2,
        '',
        reason: 'the hash names bytes the page never displayed',
      );
    });

    testWidgets('a proposal shown whole still carries its hash', (
      tester,
    ) async {
      final api = _Api()..state = _stateWith(_readableProposal());
      await _pump(tester, api);

      await tester.tap(find.text('Reject'));
      await tester.pumpAndSettle();

      expect(api.rejections.single.$2, 'sha256:file');
    });
  });

  group('an inbox file Turing lost the record of', () {
    testWidgets('is on the page, marked as neither the user\'s nor decidable', (
      tester,
    ) async {
      final api = _Api()..state = _stateWith(_untrackedFile());
      await _pump(tester, api);

      expect(find.text('inbox/orphan.md'), findsOneWidget);
      expect(find.text('Reject'), findsNothing);
      expect(find.text('Promote'), findsNothing);
      // Not "your own draft": Turing wrote this claim about them.
      expect(find.text('Your own draft'), findsNothing);
      expect(
        find.textContaining('Turing wrote this file'),
        findsOneWidget,
        reason: 'the user is owed the truth about who wrote it',
      );
      expect(
        find.textContaining('delete it'),
        findsOneWidget,
        reason: 'a card with no action needs to say what the user can do',
      );
    });
  });

  group('an apply the server has already claimed', () {
    testWidgets('says so, and offers no decision', (tester) async {
      final api = _Api()..state = _stateWith(_claimedApply());
      await _pump(tester, api);

      expect(find.text('Reject'), findsNothing);
      expect(find.text('Apply'), findsNothing);
      expect(
        find.textContaining('being applied'),
        findsOneWidget,
        reason: 'a claimed apply is not a proposal waiting on the user',
      );
    });
  });

  group('an apply whose tidying is outstanding', () {
    testWidgets('says the edit landed and the proposal is still there', (
      tester,
    ) async {
      final api = _Api()
        ..state = _stateWith(_readableProfileEdit())
        ..cleanupPending = true;
      await _pump(tester, api);

      await tester.tap(find.text('Apply'));
      await tester.pumpAndSettle();

      expect(
        find.textContaining('Your profile was updated'),
        findsOneWidget,
        reason: 'reporting nothing would leave a card the user cannot act on',
      );
    });
  });
}

Future<void> _pump(WidgetTester tester, _Api api) async {
  tester.view.physicalSize = const Size(1400, 2400);
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

MemoryCandidate _unreadableProposal() {
  return MemoryCandidate(
    candidateId: 'cand-unreadable',
    kind: MemoryCandidateKind.belief,
    inboxPath: 'inbox/broken.md',
    content: '',
    contentHash: 'sha256:file',
    state: MemoryCandidateState.pending,
    managed: true,
    parseError: 'frontmatter is not terminated',
    unavailableReason: MemoryUnavailableReason.contentParseFailed,
  );
}

MemoryCandidate _readableProposal() {
  return MemoryCandidate(
    candidateId: 'cand-readable',
    kind: MemoryCandidateKind.belief,
    inboxPath: 'inbox/bikes.md',
    content: 'They bike to work.',
    contentHash: 'sha256:file',
    state: MemoryCandidateState.pending,
    managed: true,
    unavailableReason: MemoryUnavailableReason.none,
  );
}

MemoryCandidate _readableProfileEdit() {
  return MemoryCandidate(
    candidateId: 'cand-profile',
    kind: MemoryCandidateKind.profileEdit,
    inboxPath: 'inbox/profile.md',
    content: 'They bike to work.',
    contentHash: 'sha256:file',
    state: MemoryCandidateState.pending,
    managed: true,
    unavailableReason: MemoryUnavailableReason.none,
  );
}

MemoryCandidate _untrackedFile() {
  return MemoryCandidate(
    candidateId: '',
    kind: MemoryCandidateKind.unspecified,
    inboxPath: 'inbox/orphan.md',
    content: 'They bike to work.',
    contentHash: 'sha256:orphan',
    state: MemoryCandidateState.unspecified,
    managed: false,
    untracked: true,
    unavailableReason: MemoryUnavailableReason.none,
  );
}

MemoryCandidate _claimedApply() {
  return MemoryCandidate(
    candidateId: 'cand-applying',
    kind: MemoryCandidateKind.profileEdit,
    inboxPath: 'inbox/profile.md',
    content: 'They bike to work.',
    contentHash: 'sha256:file',
    state: MemoryCandidateState.profileApplying,
    managed: true,
    unavailableReason: MemoryUnavailableReason.none,
  );
}

MemoryState _stateWith(MemoryCandidate candidate) {
  return MemoryState(
    settings: const MemorySettings(
      enabled: true,
      vaultRoot: '/memory',
      vaultWritable: true,
      unavailableReason: MemoryUnavailableReason.none,
    ),
    persona: const MemoryDocument(
      content: '# Persona\n\nBe direct.\n',
      contentHash: 'sha256:persona',
      status: MemoryNoteStatus.unmanaged,
      unavailableReason: MemoryUnavailableReason.none,
    ),
    profile: const MemoryDocument(
      content: '# Profile\n\nWrites Go.\n',
      contentHash: 'sha256:profile',
      status: MemoryNoteStatus.managed,
      unavailableReason: MemoryUnavailableReason.none,
    ),
    candidates: [candidate],
  );
}

class _Api extends TuringApi
    with
        NoAuditApi,
        NoAutomationsApi,
        NoExternalAgentsApi,
        NoIntegrationsApi,
        NoSessionLifecycleApi,
        NoSkillsApi,
        NoTelemetryApi {
  late MemoryState state;
  bool cleanupPending = false;
  final List<(String, String)> rejections = [];

  @override
  Future<MemoryState> listMemoryState() async => state;

  @override
  Future<MemorySettings> setMemoryEnabled({required bool enabled}) async =>
      state.settings;

  @override
  Future<MemoryCandidate> promoteMemoryCandidate({
    required String candidateId,
    required String expectedCandidateHash,
  }) async => state.candidates.single;

  @override
  Future<MemoryCandidate> rejectMemoryCandidate({
    required String candidateId,
    String expectedCandidateHash = '',
    String reason = '',
  }) async {
    rejections.add((candidateId, expectedCandidateHash));
    return state.candidates.single;
  }

  @override
  Future<MemoryApplyResult> applyMemoryProfile({
    required String candidateId,
    required String content,
    required String expectedContentHash,
    String expectedCandidateHash = '',
  }) async {
    return MemoryApplyResult(
      profile: state.profile,
      cleanupPending: cleanupPending,
    );
  }

  @override
  Future<MemoryDocument> saveMemoryPersona({
    required String content,
    required String expectedContentHash,
  }) async => state.persona;

  @override
  Future<MemoryDocument> saveMemoryProfile({
    required String content,
    required String expectedContentHash,
  }) async => state.profile;

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
