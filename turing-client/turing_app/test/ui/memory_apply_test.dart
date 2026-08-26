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

/// What an apply actually is.
///
/// `ApplyMemoryProfile.content` is the whole resulting profile document, not
/// the proposal. A client that sent the candidate's fragment there would be
/// asking the server to replace everything the user has ever written about
/// themselves with one paragraph a model proposed — and the server would
/// accept it, because the fragment is a perfectly valid document.
///
/// So the page composes the result, shows it, and lets the user edit it before
/// it is sent.
void main() {
  group('applying a profile proposal', () {
    testWidgets('composes the result from the profile and the proposal', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      final result = _resultEditor(tester);
      expect(
        result.controller!.text,
        contains('Writes Go and lives in Guadalajara.'),
        reason: 'the profile the user already wrote must survive the apply',
      );
      expect(
        result.controller!.text,
        contains('Bikes to work every day.'),
        reason: 'the proposal is what the apply is adding',
      );
      expect(
        result.controller!.text.indexOf('Writes Go'),
        lessThan(result.controller!.text.indexOf('Bikes to work')),
        reason: 'the composition is deterministic: the profile, then the edit',
      );
      // The proposal is still shown on its own, whole, beside the result.
      expect(find.textContaining('Bikes to work every day.'), findsWidgets);
    });

    testWidgets('sends the reviewed result, not the proposal', (tester) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      await _tap(tester, find.text('Apply'));

      expect(api.profileApplies, hasLength(1));
      final (candidateId, content, profileHash, candidateHash) =
          api.profileApplies.single;
      expect(candidateId, 'cand-profile');
      expect(
        content,
        contains('Writes Go and lives in Guadalajara.'),
        reason: 'the apply must not drop the profile it was composed over',
      );
      expect(content, contains('Bikes to work every day.'));
      expect(profileHash, 'sha256:profile');
      expect(
        candidateHash,
        'sha256:proposed',
        reason: 'the decision names the exact proposal it was composed from',
      );
    });

    testWidgets('sends what the user edited, not what was composed', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      await tester.enterText(
        _resultEditorFinder(),
        '# Profile\n\nWrites Go.\n\nCycles to work, actually.\n',
      );
      await tester.pumpAndSettle();
      await _tap(tester, find.text('Apply'));

      expect(api.profileApplies, hasLength(1));
      expect(
        api.profileApplies.single.$2,
        '# Profile\n\nWrites Go.\n\nCycles to work, actually.\n',
      );
    });

    testWidgets('offers no apply until there is a result to send', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      await tester.enterText(_resultEditorFinder(), '   ');
      await tester.pumpAndSettle();

      final apply = tester.widget<FilledButton>(
        find.ancestor(
          of: find.text('Apply'),
          matching: find.byType(FilledButton),
        ),
      );
      expect(
        apply.onPressed,
        isNull,
        reason: 'an apply with nothing in it would empty the profile',
      );
      await _tap(tester, find.text('Apply'));
      expect(api.profileApplies, isEmpty);
    });

    testWidgets('re-seeds an untouched result when the profile moves under it', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      // Somebody else wrote the profile. The page re-reads after a decision on
      // another proposal, and the result the user has not touched has to catch
      // up — a result composed over a profile that no longer exists would be
      // applied over words it never saw.
      api.state = _stateWithProfileEdit(profileContent: '# Profile\n\nMoved on.\n');
      await _tap(tester, find.text('Reject'));

      expect(
        _resultEditor(tester).controller!.text,
        contains('Moved on.'),
        reason: 'an untouched result follows the profile it is composed over',
      );
    });

    testWidgets('keeps an edited result when the profile moves under it', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);

      await tester.enterText(_resultEditorFinder(), 'Mine, not composed.\n');
      await tester.pumpAndSettle();
      api.state = _stateWithProfileEdit(profileContent: '# Profile\n\nMoved on.\n');
      await _tap(tester, find.text('Reject'));

      expect(
        _resultEditor(tester).controller!.text,
        'Mine, not composed.\n',
        reason: 'a re-read is not permission to discard what the user typed',
      );
    });

    testWidgets('forgets the result editor once the proposal is decided', (
      tester,
    ) async {
      final api = _Api()..state = _stateWithProfileEdit();
      await _pump(tester, api);
      expect(_resultEditorFinder(), findsOneWidget);

      // A decided proposal leaves the vault, so its half-composed result must
      // leave the page with it rather than waiting to be handed to whatever
      // reuses the id.
      api.state = _stateWithoutCandidates();
      await _tap(tester, find.text('Reject'));

      expect(_resultEditorFinder(), findsNothing);
      expect(tester.takeException(), isNull);
    });

    testWidgets('a proposal that could not be read offers no result editor', (
      tester,
    ) async {
      final api = _Api()
        ..state = _stateWithProfileEdit(
          candidate: _profileEditCandidate(
            content: '',
            unavailableReason: MemoryUnavailableReason.contentTooLarge,
          ),
        );
      await _pump(tester, api);

      expect(_resultEditorFinder(), findsNothing);
      expect(find.text('Apply'), findsNothing);
    });
  });

  group('a proposal this build cannot read', () {
    testWidgets('offers no decision for an unknown kind', (tester) async {
      final api = _Api()
        ..state = _stateWithProfileEdit(
          candidate: _profileEditCandidate(
            kind: MemoryCandidateKind.unspecified,
          ),
        );
      await _pump(tester, api);

      expect(find.text('Apply'), findsNothing);
      expect(find.text('Promote'), findsNothing);
      expect(find.text('Reject'), findsNothing);
      expect(
        find.textContaining('this version of Turing does not understand'),
        findsOneWidget,
        reason: 'silence would read as a proposal with nothing to decide',
      );
    });

    testWidgets('offers no decision for a state it cannot name', (
      tester,
    ) async {
      final api = _Api()
        ..state = _stateWithProfileEdit(
          candidate: _profileEditCandidate(
            kind: MemoryCandidateKind.belief,
            state: MemoryCandidateState.unspecified,
          ),
        );
      await _pump(tester, api);

      expect(find.text('Apply'), findsNothing);
      expect(find.text('Promote'), findsNothing);
      expect(find.text('Reject'), findsNothing);
      expect(
        find.textContaining('this version of Turing does not understand'),
        findsOneWidget,
      );
    });
  });

  group('typing while an apply is in flight', () {
    testWidgets('takes no words the apply will not carry', (tester) async {
      final api = _Api()
        ..state = _stateWithProfileEdit()
        ..profileApplyDelay = const Duration(milliseconds: 50);
      await _pump(tester, api);

      await tester.enterText(_resultEditorFinder(), 'What I reviewed.\n');
      await tester.pump();
      await _tapNoSettle(tester, find.text('Apply'));

      // The request is in flight and the whole page is busy. An authored
      // document may keep taking keystrokes mid-save, because it is still
      // there afterwards to hold them — this editor is not. A successful apply
      // decides the proposal, the card leaves the page, and anything typed
      // into it after the button was pressed goes with it: not sent, not
      // saved, and nowhere to be found.
      final duringApply = _resultEditor(tester);
      expect(
        duringApply.enabled,
        isFalse,
        reason: 'a field that cannot keep what it takes must not take it',
      );
      expect(
        _resultEditable(tester).readOnly,
        isTrue,
        reason: 'read-only is what actually keeps a platform keyboard out',
      );
      expect(
        tester
            .widget<FilledButton>(find.widgetWithText(FilledButton, 'Apply'))
            .onPressed,
        isNull,
        reason: 'and the decision is not offered twice',
      );

      await tester.enterText(_resultEditorFinder(), 'Typed after the apply.\n');
      await tester.pump();
      expect(
        _resultEditor(tester).controller!.text,
        'What I reviewed.\n',
        reason: 'nothing typed after the button reached the editor',
      );

      await tester.pump(const Duration(milliseconds: 100));
      await tester.pumpAndSettle();

      expect(api.profileApplies, hasLength(1));
      expect(
        api.profileApplies.single.$2,
        'What I reviewed.\n',
        reason: 'the document that was reviewed is the document that was sent',
      );
    });

    testWidgets('takes them again once the apply has answered', (tester) async {
      final api = _Api()
        ..state = _stateWithProfileEdit()
        ..profileApplyDelay = const Duration(milliseconds: 50);
      await _pump(tester, api);

      await _tapNoSettle(tester, find.text('Apply'));
      await tester.pump(const Duration(milliseconds: 100));
      await tester.pumpAndSettle();

      // The proposal is still listed — this apply changed nothing about the
      // listing — so the editor is back, and it is an editor again.
      expect(
        _resultEditor(tester).enabled,
        isNot(isFalse),
        reason: 'the page is not busy any more, so nothing is disabled',
      );
    });
  });

  group('typing while a save is in flight', () {
    testWidgets('keeps the keystrokes the user added mid-save', (tester) async {
      final api = _Api()..personaSaveDelay = const Duration(milliseconds: 50);
      await _pump(tester, api);

      await tester.enterText(
        find.byKey(const Key('memory-persona-editor')),
        '# Persona\n\nBe direct.\n',
      );
      await tester.pump();
      await _tapNoSettle(tester, find.byKey(const Key('memory-persona-save')));

      // The save is in flight. The user keeps typing.
      await tester.enterText(
        find.byKey(const Key('memory-persona-editor')),
        '# Persona\n\nBe direct. And brief.\n',
      );
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 100));
      await tester.pumpAndSettle();

      expect(api.personaSaves, [('# Persona\n\nBe direct.\n', 'sha256:persona')]);
      final editor = tester.widget<TextField>(
        find.byKey(const Key('memory-persona-editor')),
      );
      expect(
        editor.controller!.text,
        '# Persona\n\nBe direct. And brief.\n',
        reason: 'a successful save must not swallow what the user typed after it',
      );

      // The next save carries the hash the accepted save returned, because the
      // text on screen is those words plus more.
      await _tap(tester, find.byKey(const Key('memory-persona-save')));
      expect(api.personaSaves, [
        ('# Persona\n\nBe direct.\n', 'sha256:persona'),
        ('# Persona\n\nBe direct. And brief.\n', 'sha256:persona-saved'),
      ]);
    });

    testWidgets('adopts the server text when nothing was typed after it', (
      tester,
    ) async {
      final api = _Api()..profileSaveDelay = const Duration(milliseconds: 50);
      await _pump(tester, api);

      await tester.enterText(
        find.byKey(const Key('memory-profile-editor')),
        '# Profile\n\nWrites Go.\n',
      );
      await tester.pump();
      await _tapNoSettle(tester, find.byKey(const Key('memory-profile-save')));
      await tester.pump(const Duration(milliseconds: 100));
      await tester.pumpAndSettle();

      final editor = tester.widget<TextField>(
        find.byKey(const Key('memory-profile-editor')),
      );
      expect(editor.controller!.text, '# Profile\n\nWrites Go.\n');
    });
  });
}

Finder _resultEditorFinder() =>
    find.byKey(const Key('memory-profile-result-cand-profile'));

TextField _resultEditor(WidgetTester tester) =>
    tester.widget<TextField>(_resultEditorFinder());

EditableText _resultEditable(WidgetTester tester) => tester.widget<EditableText>(
  find.descendant(
    of: _resultEditorFinder(),
    matching: find.byType(EditableText),
  ),
);


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
  MemoryCandidateKind kind = MemoryCandidateKind.profileEdit,
  MemoryCandidateState state = MemoryCandidateState.pending,
  String content = 'Bikes to work every day.',
  String contentHash = 'sha256:proposed',
  MemoryUnavailableReason unavailableReason = MemoryUnavailableReason.none,
}) {
  return MemoryCandidate(
    candidateId: 'cand-profile',
    kind: kind,
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
      contentHash: 'sha256:profile',
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
  Duration? personaSaveDelay;
  Duration? profileSaveDelay;
  Duration? profileApplyDelay;
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
    if (profileApplyDelay case final delay?) await Future<void>.delayed(delay);
    return MemoryApplyResult(profile: state.profile);
  }

  @override
  Future<MemoryDocument> saveMemoryPersona({
    required String content,
    required String expectedContentHash,
  }) async {
    personaSaves.add((content, expectedContentHash));
    if (personaSaveDelay case final delay?) await Future<void>.delayed(delay);
    final saved = MemoryDocument(
      content: content,
      contentHash: 'sha256:persona-saved',
      status: MemoryNoteStatus.unmanaged,
      unavailableReason: MemoryUnavailableReason.none,
    );
    state = MemoryState(
      settings: state.settings,
      persona: saved,
      profile: state.profile,
      tiers: state.tiers,
      notes: state.notes,
      candidates: state.candidates,
    );
    return saved;
  }

  @override
  Future<MemoryDocument> saveMemoryProfile({
    required String content,
    required String expectedContentHash,
  }) async {
    profileSaves.add((content, expectedContentHash));
    if (profileSaveDelay case final delay?) await Future<void>.delayed(delay);
    final saved = MemoryDocument(
      content: content,
      contentHash: 'sha256:profile-saved',
      status: MemoryNoteStatus.managed,
      unavailableReason: MemoryUnavailableReason.none,
    );
    state = MemoryState(
      settings: state.settings,
      persona: state.persona,
      profile: saved,
      tiers: state.tiers,
      notes: state.notes,
      candidates: state.candidates,
    );
    return saved;
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
