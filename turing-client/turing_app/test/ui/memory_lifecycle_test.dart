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

/// Two things a page that talks to a server over a slow link owes the user.
///
/// The first is that a result they have edited keeps the *whole* of what it was
/// composed against, not just the words. A resulting profile document is
/// composed over a specific profile; if that profile moves underneath while
/// their edit is on screen, the edit is kept — but pairing those older words
/// with the newer document's compare-and-set token would tell the server "this
/// is an edit of what you have now", and it would be accepted, silently
/// throwing away whatever the other writer put there.
///
/// The second is that a page the user has left is a page that must not still be
/// writing into text fields. Every await here is a window in which the widget
/// can be disposed.
void main() {
  group('a result the user has edited', () {
    testWidgets('applies against the profile it was composed from', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      await tester.enterText(_resultEditorFinder(), 'Mine, not composed.\n');
      await tester.pumpAndSettle();

      // Somebody else writes the profile while the edit is on screen. The
      // page re-reads, keeps the user's words, and must keep the token those
      // words were composed against with them.
      api.state = _stateWithProfileEdit(
        profileContent: '# Profile\n\nMoved on.\n',
        profileHash: 'sha256:profile-moved',
      );
      await _tap(tester, find.text('Reject'));

      expect(
        _resultEditor(tester).controller!.text,
        'Mine, not composed.\n',
        reason: 'a re-read is not permission to discard what the user typed',
      );

      await _tap(tester, find.text('Apply'));

      expect(api.profileApplies, hasLength(1));
      final (_, content, profileHash, _) = api.profileApplies.single;
      expect(content, 'Mine, not composed.\n');
      expect(
        profileHash,
        'sha256:profile',
        reason:
            'these words were composed over the older profile, so the server '
            'has to be the one that refuses them',
      );
    });

    testWidgets('an untouched result follows the profile, token and all', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      api.state = _stateWithProfileEdit(
        profileContent: '# Profile\n\nMoved on.\n',
        profileHash: 'sha256:profile-moved',
      );
      await _tap(tester, find.text('Reject'));

      expect(_resultEditor(tester).controller!.text, contains('Moved on.'));

      await _tap(tester, find.text('Apply'));

      expect(api.profileApplies, hasLength(1));
      expect(
        api.profileApplies.single.$3,
        'sha256:profile-moved',
        reason: 'a re-seeded result is an edit of the profile it was re-seeded '
            'from',
      );
    });
  });

  group('leaving the page while it is still talking to the server', () {
    // A read that lands after the page is gone is normally swallowed by the
    // FutureBuilder that was listening for it, which is exactly what makes this
    // class of bug so easy to leave in: the page really does write into
    // disposed controllers, and nothing says so. debugRethrowError is the
    // framework's own seam for that, and it is what these tests watch through.
    setUp(() => FutureBuilder.debugRethrowError = true);
    tearDown(() => FutureBuilder.debugRethrowError = false);

    testWidgets('a read that lands after the page is gone throws nothing', (
      tester,
    ) async {
      final api = _Api()
        ..state = _stateWithProfileEdit()
        ..listStateDelay = const Duration(milliseconds: 50);
      await tester.pumpWidget(_host(api));
      await tester.pump();

      // The user navigates away before the first read comes back.
      await tester.pumpWidget(
        const MaterialApp(home: Scaffold(body: SizedBox.shrink())),
      );
      await tester.pump(const Duration(milliseconds: 100));
      await tester.pumpAndSettle();

      expect(tester.takeException(), isNull);
    });

    testWidgets('the re-read after a decision throws nothing either', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      // The decision itself lands while the page is still there; the re-read
      // it triggers is the one still in flight when the user leaves.
      api.listStateDelay = const Duration(milliseconds: 50);
      await tester.ensureVisible(find.text('Reject'));
      await tester.pump();
      await tester.tap(find.text('Reject'));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 10));

      await tester.pumpWidget(
        const MaterialApp(home: Scaffold(body: SizedBox.shrink())),
      );
      await tester.pump(const Duration(milliseconds: 200));
      await tester.pumpAndSettle();

      expect(tester.takeException(), isNull);
    });

    testWidgets('the re-read after a save throws nothing either', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      await tester.enterText(
        find.byKey(const Key('memory-persona-editor')),
        '# Persona\n\nBe direct and brief.\n',
      );
      await tester.pump();
      api.listStateDelay = const Duration(milliseconds: 50);
      await tester.tap(find.byKey(const Key('memory-persona-save')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 10));

      await tester.pumpWidget(
        const MaterialApp(home: Scaffold(body: SizedBox.shrink())),
      );
      await tester.pump(const Duration(milliseconds: 200));
      await tester.pumpAndSettle();

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

Widget _host(_Api api) => MaterialApp(
  localizationsDelegates: AppLocalizations.localizationsDelegates,
  supportedLocales: AppLocalizations.supportedLocales,
  home: Scaffold(body: MemoryPage(apiClient: api)),
);

Future<void> _pump(WidgetTester tester, _Api api) async {
  tester.view.physicalSize = const Size(1200, 1600);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(_host(api));
  await tester.pumpAndSettle();
}

MemoryCandidate _profileEditCandidate() {
  return MemoryCandidate(
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
  );
}

MemoryState _stateWithProfileEdit({
  String profileContent = '# Profile\n\nWrites Go and lives in Guadalajara.\n',
  String profileHash = 'sha256:profile',
}) {
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
    profile: MemoryDocument(
      content: profileContent,
      contentHash: profileHash,
      status: MemoryNoteStatus.managed,
      unavailableReason: MemoryUnavailableReason.none,
    ),
    candidates: [_profileEditCandidate()],
  );
}

MemoryState _defaultState() {
  return MemoryState(
    settings: const MemorySettings(
      enabled: true,
      vaultRoot: '/memory',
      vaultWritable: true,
      unavailableReason: MemoryUnavailableReason.none,
    ),
    persona: const MemoryDocument(
      content: '',
      contentHash: 'sha256:persona',
      status: MemoryNoteStatus.unmanaged,
      unavailableReason: MemoryUnavailableReason.none,
    ),
    profile: const MemoryDocument(
      content: '',
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
  MemoryState state = _defaultState();
  Duration? listStateDelay;
  Duration? rejectDelay;
  Duration? personaSaveDelay;
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
    required String expectedContentHash,
    String expectedCandidateHash = '',
  }) async => _profileEditCandidate();

  @override
  Future<MemoryCandidate> rejectMemoryCandidate({
    required String candidateId,
    required String expectedContentHash,
    String reason = '',
    String expectedCandidateHash = '',
  }) async {
    if (rejectDelay case final delay?) await Future<void>.delayed(delay);
    return _profileEditCandidate();
  }

  @override
  Future<MemoryDocument> applyMemoryProfile({
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
    return state.profile;
  }

  @override
  Future<MemoryDocument> saveMemoryPersona({
    required String content,
    required String expectedContentHash,
  }) async {
    if (personaSaveDelay case final delay?) await Future<void>.delayed(delay);
    return MemoryDocument(
      content: content,
      contentHash: 'sha256:persona-saved',
      status: MemoryNoteStatus.unmanaged,
      unavailableReason: MemoryUnavailableReason.none,
    );
  }

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
