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

/// The number on the screen has to be the number in the request.
///
/// A compare-and-set token is the whole explanation of a refusal: the user is
/// told "this applies only while the document still matches X", the server
/// refuses because it does not, and the only way that sentence helps is if X is
/// what was actually sent. The page tracks the token each editor and each
/// composed result was loaded against — precisely because a re-read must not
/// silently re-aim an unsaved edit at a newer document — and then displayed the
/// newest one it had read instead. Refusal explained by a number nobody sent.
void main() {
  group('the token the page shows', () {
    testWidgets('is the one an apply of an edited result actually sends', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      // The user rewrites the composed result. Those words describe the
      // profile as it read a moment ago, so they stay pinned to its token.
      await tester.enterText(
        _resultEditorFinder(),
        '# Profile\n\nMine, not composed.\n',
      );
      await tester.pumpAndSettle();

      // Somebody else writes profile.md, and a decision on a second proposal
      // makes the page re-read.
      api.state = _stateWithProfileEdit(
        profileContent: '# Profile\n\nMoved on.\n',
        profileHash: 'sha256:profile-moved',
      );
      await _tap(tester, find.text('Reject'));

      expect(
        find.textContaining('profile.md still matches sha256:profile\n'),
        findsNothing,
        reason: 'no such literal; guards the finder below against a typo',
      );
      expect(
        find.textContaining('profile.md still matches sha256:profile-moved'),
        findsNothing,
        reason:
            'the newest token is not the one this result was composed against, '
            'and showing it would explain a refusal with a number nobody sent',
      );
      expect(
        find.textContaining('profile.md still matches sha256:profile'),
        findsOneWidget,
        reason: 'the card names the token the apply will carry',
      );

      await _tap(tester, find.text('Apply'));
      expect(api.profileApplies, hasLength(1));
      expect(
        api.profileApplies.single.$3,
        'sha256:profile',
        reason: 'and the request carries exactly the token the card named',
      );
    });

    testWidgets('is the version the persona editor is holding, not the newest', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      await tester.enterText(
        find.byKey(const Key('memory-persona-editor')),
        '# Persona\n\nBe warmer.\n',
      );
      await tester.pumpAndSettle();

      // The vault moved under the unsaved edit, and the page re-read.
      api.state = _stateWithProfileEdit(personaHash: 'sha256:persona-moved');
      await _tap(tester, find.text('Reject'));

      expect(
        find.textContaining('Editing version sha256:persona-moved'),
        findsNothing,
        reason:
            'the editor still holds the older version; a save will name that '
            'one and be refused, and this line is what explains why',
      );
      expect(
        find.textContaining('Editing version sha256:persona'),
        findsOneWidget,
      );

      await _tap(tester, find.byKey(const Key('memory-persona-save')));
      expect(api.personaSaves, hasLength(1));
      expect(api.personaSaves.single.$2, 'sha256:persona');
    });

    testWidgets('is the version the profile editor is holding, not the newest', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      await tester.enterText(
        find.byKey(const Key('memory-profile-editor')),
        '# Profile\n\nMy own words.\n',
      );
      await tester.pumpAndSettle();

      api.state = _stateWithProfileEdit(profileHash: 'sha256:profile-moved');
      await _tap(tester, find.text('Reject'));

      expect(
        find.textContaining('Editing version sha256:profile-moved'),
        findsNothing,
      );
      expect(
        find.textContaining('Editing version sha256:profile'),
        findsOneWidget,
      );

      await _tap(tester, find.byKey(const Key('memory-profile-save')));
      expect(api.profileSaves, hasLength(1));
      expect(api.profileSaves.single.$2, 'sha256:profile');
    });

    testWidgets('follows an untouched editor when the document moves', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      // Nothing was typed, so the re-read adopts the server's text — and the
      // token has to move with it, or the next save names a version the editor
      // is no longer showing.
      api.state = _stateWithProfileEdit(personaHash: 'sha256:persona-moved');
      await _tap(tester, find.text('Reject'));

      expect(
        find.textContaining('Editing version sha256:persona-moved'),
        findsOneWidget,
      );
    });
  });
}

Finder _resultEditorFinder() =>
    find.byKey(const Key('memory-profile-result-cand-profile'));

Future<void> _pump(WidgetTester tester, _Api api) async {
  tester.view.physicalSize = const Size(1200, 2400);
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

Future<void> _tap(WidgetTester tester, Finder finder) async {
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  await tester.tap(finder);
  await tester.pumpAndSettle();
}

MemoryState _stateWithProfileEdit({
  String profileContent = '# Profile\n\nWrites Go and lives in Guadalajara.\n',
  String profileHash = 'sha256:profile',
  String personaHash = 'sha256:persona',
}) {
  return MemoryState(
    settings: const MemorySettings(
      enabled: true,
      vaultRoot: '/memory',
      vaultWritable: true,
      unavailableReason: MemoryUnavailableReason.none,
    ),
    persona: MemoryDocument(
      content: '# Persona\n\nBe direct.\n',
      contentHash: personaHash,
      status: MemoryNoteStatus.unmanaged,
      unavailableReason: MemoryUnavailableReason.none,
    ),
    profile: MemoryDocument(
      content: profileContent,
      contentHash: profileHash,
      status: MemoryNoteStatus.managed,
      unavailableReason: MemoryUnavailableReason.none,
    ),
    candidates: [
      MemoryCandidate(
        candidateId: 'cand-profile',
        kind: MemoryCandidateKind.profileEdit,
        inboxPath: 'inbox/01-bikes.md',
        content: 'Bikes to work every day.',
        contentHash: 'sha256:proposed',
        state: MemoryCandidateState.pending,
        managed: true,
        unavailableReason: MemoryUnavailableReason.none,
        provenance: const [
          MemoryProvenance(
            kind: MemoryProvenanceKind.promotedFromCandidate,
            sourceSessionId: 'sess-1',
            evidenceCount: 1,
          ),
        ],
      ),
    ],
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
  MemoryState state = _stateWithProfileEdit();
  final List<(String, String, String, String)> profileApplies = [];
  final List<(String, String)> personaSaves = [];
  final List<(String, String)> profileSaves = [];

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
  }) async => state.candidates.single;

  @override
  Future<MemoryApplyResult> applyMemoryProfile({
    required String candidateId,
    required String content,
    required String expectedContentHash,
    String expectedCandidateHash = '',
  }) async {
    profileApplies.add((
      candidateId,
      content,
      expectedContentHash,
      expectedCandidateHash,
    ));
    return MemoryApplyResult(profile: state.profile);
  }

  @override
  Future<MemoryDocument> saveMemoryPersona({
    required String content,
    required String expectedContentHash,
  }) async {
    personaSaves.add((content, expectedContentHash));
    return state.persona;
  }

  @override
  Future<MemoryDocument> saveMemoryProfile({
    required String content,
    required String expectedContentHash,
  }) async {
    profileSaves.add((content, expectedContentHash));
    return state.profile;
  }

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
