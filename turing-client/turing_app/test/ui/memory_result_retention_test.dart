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

/// Who a half-written result belongs to, and what ends it.
///
/// The page owns one resulting-profile editor per profile proposal so a re-read
/// does not throw away what the user typed into it. Which of those editors it
/// keeps is a question about the proposal's *row*, not about what this build
/// can offer for it this frame: a vault that stopped being readable, bytes that
/// stopped parsing, memory switched off — each takes the Apply button away, and
/// none of them is a decision. Letting any of them forget the editor loses
/// words the user typed to a condition that clears on the next read.
///
/// What does end a result is the row leaving the listing, or the server
/// deciding the proposal. Those results describe a file that is gone, and
/// holding one would hand it to whatever arrives under the same id next.
void main() {
  group('a result the user has typed into', () {
    testWidgets('survives a vault that stops being readable and comes back', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      await tester.enterText(_resultEditorFinder(), 'Mine, not composed.\n');
      await tester.pumpAndSettle();

      // The vault closes under the page. The row is still listed — the server
      // keeps its own record of what it wrote — but it cannot speak for the
      // file, so the proposal arrives with its content withheld.
      api
        ..state = _stateWithProfileEdit(
          candidate: _profileEditCandidate(
            content: '',
            unavailableReason: MemoryUnavailableReason.vaultUnreadable,
          ),
          vaultReason: MemoryUnavailableReason.vaultUnreadable,
        )
        ..listStateDelay = const Duration(milliseconds: 50);
      await _tapNoSettle(
        tester,
        find.byKey(const Key('memory-persona-reread')),
      );
      // The read is still in flight: the page is on its loading frame, and
      // nothing about a frame with no listing in it may decide the editor's
      // fate.
      await tester.pump(const Duration(milliseconds: 10));
      await tester.pumpAndSettle();

      expect(
        find.text('Apply'),
        findsNothing,
        reason: 'nobody may accept a proposal whose words are not on screen',
      );
      expect(_resultEditorFinder(), findsNothing);

      // The vault comes back, and both the profile and the proposal moved
      // while it was shut.
      api
        ..state = _stateWithProfileEdit(
          profileContent: '# Profile\n\nMoved on.\n',
          profileHash: 'sha256:profile-moved',
          candidate: _profileEditCandidate(
            content: 'Cycles to work, actually.',
            contentHash: 'sha256:proposed-moved',
          ),
        )
        ..listStateDelay = null;
      await _tap(tester, find.byKey(const Key('memory-persona-reread')));

      expect(
        _resultEditor(tester).controller!.text,
        'Mine, not composed.\n',
        reason:
            'a vault that shut for a moment is not permission to discard '
            'what the user typed',
      );

      await _tap(tester, find.text('Apply'));

      expect(api.profileApplies, hasLength(1));
      final (_, content, profileHash, candidateHash) =
          api.profileApplies.single;
      expect(content, 'Mine, not composed.\n');
      expect(
        profileHash,
        'sha256:profile',
        reason:
            'these words were composed over the older profile, so the '
            'server has to be the one that refuses them',
      );
      expect(
        candidateHash,
        'sha256:proposed',
        reason:
            'and they accept the older claim, not the one that replaced it '
            'while nobody could read it',
      );
    });

    testWidgets('survives memory being turned off and on again', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      await tester.enterText(_resultEditorFinder(), 'Mine, not composed.\n');
      await tester.pumpAndSettle();

      api.state = _stateWithProfileEdit(
        enabled: false,
        vaultReason: MemoryUnavailableReason.disabled,
        candidate: _profileEditCandidate(
          content: '',
          unavailableReason: MemoryUnavailableReason.disabled,
        ),
      );
      await _tap(tester, find.byKey(const Key('memory-enabled-toggle')));

      expect(_resultEditorFinder(), findsNothing);
      expect(find.text('Apply'), findsNothing);

      api.state = _stateWithProfileEdit();
      await _tap(tester, find.byKey(const Key('memory-enabled-toggle')));

      expect(
        _resultEditor(tester).controller!.text,
        'Mine, not composed.\n',
        reason: 'a toggle is not a decision about this proposal',
      );
    });

    testWidgets('survives an apply claim the server hands back', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      await tester.enterText(_resultEditorFinder(), 'Mine, not composed.\n');
      await tester.pumpAndSettle();

      // A claimed apply is not the end of the row: the server hands the
      // proposal back to pending when the write turned out to change nothing.
      api.state = _stateWithProfileEdit(
        candidate: _profileEditCandidate(
          state: MemoryCandidateState.profileApplying,
        ),
      );
      await _tap(tester, find.byKey(const Key('memory-persona-reread')));
      expect(_resultEditorFinder(), findsNothing);

      api.state = _stateWithProfileEdit();
      await _tap(tester, find.byKey(const Key('memory-persona-reread')));

      expect(_resultEditor(tester).controller!.text, 'Mine, not composed.\n');
    });

    testWidgets('survives a proposal state this build cannot name', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      await tester.enterText(_resultEditorFinder(), 'Mine, not composed.\n');
      await tester.pumpAndSettle();

      // A newer server answering with a state this build has never heard of is
      // not a server saying the proposal was decided. Reading it as one would
      // end somebody's draft over a word nobody here can read.
      api.state = _stateWithProfileEdit(
        candidate: _profileEditCandidate(
          state: MemoryCandidateState.unspecified,
        ),
      );
      await _tap(tester, find.byKey(const Key('memory-persona-reread')));
      expect(_resultEditorFinder(), findsNothing);

      api.state = _stateWithProfileEdit();
      await _tap(tester, find.byKey(const Key('memory-persona-reread')));

      expect(_resultEditor(tester).controller!.text, 'Mine, not composed.\n');
    });
  });

  group('a proposal that never had a result', () {
    testWidgets('gets no editor while it cannot be read', (tester) async {
      final api = _Api()
        ..state = _stateWithProfileEdit(
          candidate: _profileEditCandidate(
            content: '',
            unavailableReason: MemoryUnavailableReason.vaultUnreadable,
          ),
          vaultReason: MemoryUnavailableReason.vaultUnreadable,
        );
      await _pump(tester, api);

      expect(_resultEditorFinder(), findsNothing);
      expect(find.text('Apply'), findsNothing);

      // And when it becomes readable, what it gets is composed from what is
      // there now — not from anything the page invented while nobody could
      // read the file.
      api.state = _stateWithProfileEdit();
      await _tap(tester, find.byKey(const Key('memory-persona-reread')));

      expect(
        _resultEditor(tester).controller!.text,
        '# Profile\n\nWrites Go and lives in Guadalajara.\n\n'
        'Bikes to work every day.\n',
      );

      await _tap(tester, find.text('Apply'));

      expect(api.profileApplies, hasLength(1));
      expect(api.profileApplies.single.$3, 'sha256:profile');
      expect(api.profileApplies.single.$4, 'sha256:proposed');
    });
  });

  group('a result whose proposal is over', () {
    testWidgets('is forgotten when the server decides the proposal', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      await tester.enterText(_resultEditorFinder(), 'Mine, not composed.\n');
      await tester.pumpAndSettle();

      // Decided somewhere else — another window, or the reconcile pass. The
      // row is still listed, and the result the user was composing is now
      // about a file nobody will apply it to.
      api.state = _stateWithProfileEdit(
        candidate: _profileEditCandidate(state: MemoryCandidateState.promoted),
      );
      await _tap(tester, find.byKey(const Key('memory-persona-reread')));

      // The id comes back around: the inbox slot is reused by a new proposal.
      api.state = _stateWithProfileEdit(
        candidate: _profileEditCandidate(
          content: 'Cycles to work, actually.',
          contentHash: 'sha256:proposed-again',
        ),
      );
      await _tap(tester, find.byKey(const Key('memory-persona-reread')));

      expect(
        _resultEditor(tester).controller!.text,
        '# Profile\n\nWrites Go and lives in Guadalajara.\n\n'
        'Cycles to work, actually.\n',
        reason: 'a decided proposal takes its half-written result with it',
      );

      await _tap(tester, find.text('Apply'));

      expect(
        api.profileApplies.single.$4,
        'sha256:proposed-again',
        reason: 'and the tokens it was composed against go with it too',
      );
    });

    for (final decided in const {
      'rejected': MemoryCandidateState.rejected,
      'withdrawn': MemoryCandidateState.withdrawn,
    }.entries) {
      testWidgets('is forgotten when the proposal is ${decided.key}', (
        tester,
      ) async {
        final api = _Api()..state = _stateWithProfileEdit();
        await _pump(tester, api);

        await tester.enterText(_resultEditorFinder(), 'Mine, not composed.\n');
        await tester.pumpAndSettle();

        api.state = _stateWithProfileEdit(
          candidate: _profileEditCandidate(state: decided.value),
        );
        await _tap(tester, find.byKey(const Key('memory-persona-reread')));
        expect(_resultEditorFinder(), findsNothing);

        api.state = _stateWithProfileEdit(
          candidate: _profileEditCandidate(
            content: 'Cycles to work, actually.',
            contentHash: 'sha256:proposed-again',
          ),
        );
        await _tap(tester, find.byKey(const Key('memory-persona-reread')));

        expect(
          _resultEditor(tester).controller!.text,
          '# Profile\n\nWrites Go and lives in Guadalajara.\n\n'
          'Cycles to work, actually.\n',
          reason: 'a ${decided.key} proposal is over, and so is its result',
        );

        await _tap(tester, find.text('Apply'));

        expect(api.profileApplies.single.$4, 'sha256:proposed-again');
      });
    }

    testWidgets('is forgotten when the proposal leaves the listing', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      await tester.enterText(_resultEditorFinder(), 'Mine, not composed.\n');
      await tester.pumpAndSettle();

      api.state = _stateWithoutCandidates();
      await _tap(tester, find.byKey(const Key('memory-persona-reread')));
      expect(_resultEditorFinder(), findsNothing);

      api.state = _stateWithProfileEdit(
        candidate: _profileEditCandidate(
          content: 'Cycles to work, actually.',
          contentHash: 'sha256:proposed-again',
        ),
      );
      await _tap(tester, find.byKey(const Key('memory-persona-reread')));

      expect(
        _resultEditor(tester).controller!.text,
        '# Profile\n\nWrites Go and lives in Guadalajara.\n\n'
        'Cycles to work, actually.\n',
        reason:
            'nothing of the old result may be handed to the id that '
            'replaced it',
      );

      await _tap(tester, find.text('Apply'));

      expect(api.profileApplies.single.$4, 'sha256:proposed-again');
      expect(tester.takeException(), isNull);
    });
  });
}

Finder _resultEditorFinder() =>
    find.byKey(const Key('memory-profile-result-cand-profile'));

TextField _resultEditor(WidgetTester tester) =>
    tester.widget<TextField>(_resultEditorFinder());

Future<void> _tap(WidgetTester tester, Finder finder) async {
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  await tester.tap(finder);
  await tester.pumpAndSettle();
}

Future<void> _tapNoSettle(WidgetTester tester, Finder finder) async {
  await tester.ensureVisible(finder);
  await tester.pump();
  await tester.tap(finder);
  await tester.pump();
}

Future<void> _pump(WidgetTester tester, _Api api) async {
  tester.view.physicalSize = const Size(1200, 1600);
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

MemoryCandidate _profileEditCandidate({
  MemoryCandidateState state = MemoryCandidateState.pending,
  String content = 'Bikes to work every day.',
  String contentHash = 'sha256:proposed',
  MemoryUnavailableReason unavailableReason = MemoryUnavailableReason.none,
}) {
  return MemoryCandidate(
    candidateId: 'cand-profile',
    kind: MemoryCandidateKind.profileEdit,
    inboxPath: 'inbox/01-bikes.md',
    content: content,
    contentHash: contentHash,
    state: state,
    managed: true,
    unavailableReason: unavailableReason,
    provenance: const [
      MemoryProvenance(
        kind: MemoryProvenanceKind.promotedFromCandidate,
        sourceSessionId: 'sess-1',
        evidenceCount: 1,
      ),
    ],
  );
}

MemoryState _stateWithProfileEdit({
  MemoryCandidate? candidate,
  String profileContent = '# Profile\n\nWrites Go and lives in Guadalajara.\n',
  String profileHash = 'sha256:profile',
  bool enabled = true,
  MemoryUnavailableReason vaultReason = MemoryUnavailableReason.none,
}) {
  return MemoryState(
    settings: MemorySettings(
      enabled: enabled,
      vaultRoot: '/memory',
      vaultWritable: true,
      unavailableReason: vaultReason,
    ),
    persona: const MemoryDocument(
      content: '# Persona\n\nBe direct.\n',
      contentHash: 'sha256:persona',
      status: MemoryNoteStatus.unmanaged,
      unavailableReason: MemoryUnavailableReason.none,
    ),
    profile: MemoryDocument(
      content: profileContent,
      contentHash: profileHash,
      status: MemoryNoteStatus.managed,
      unavailableReason: MemoryUnavailableReason.none,
    ),
    candidates: [candidate ?? _profileEditCandidate()],
  );
}

MemoryState _stateWithoutCandidates() {
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
      content: '# Profile\n\nWrites Go and lives in Guadalajara.\n',
      contentHash: 'sha256:profile',
      status: MemoryNoteStatus.managed,
      unavailableReason: MemoryUnavailableReason.none,
    ),
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
  MemoryState state = _stateWithoutCandidates();
  Duration? listStateDelay;
  final List<(String, String, String, String)> profileApplies = [];

  @override
  Future<MemoryState> listMemoryState() async {
    if (listStateDelay case final delay?) await Future<void>.delayed(delay);
    return state;
  }

  @override
  Future<MemorySettings> setMemoryEnabled({required bool enabled}) async =>
      state.settings;

  @override
  Future<MemoryCandidate> promoteMemoryCandidate({
    required String candidateId,
    required String expectedCandidateHash,
  }) async => _profileEditCandidate();

  @override
  Future<MemoryCandidate> rejectMemoryCandidate({
    required String candidateId,
    String expectedCandidateHash = '',
    String reason = '',
  }) async => _profileEditCandidate();

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
  }) async => MemoryDocument(
    content: content,
    contentHash: 'sha256:persona-saved',
    status: MemoryNoteStatus.unmanaged,
    unavailableReason: MemoryUnavailableReason.none,
  );

  @override
  Future<MemoryDocument> saveMemoryProfile({
    required String content,
    required String expectedContentHash,
  }) async => MemoryDocument(
    content: content,
    contentHash: 'sha256:profile-saved',
    status: MemoryNoteStatus.managed,
    unavailableReason: MemoryUnavailableReason.none,
  );

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
