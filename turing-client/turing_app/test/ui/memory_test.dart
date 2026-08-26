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

void main() {
  group('the toggle', () {
    testWidgets('says what memory is doing and changes it on the server', (
      tester,
    ) async {
      final api = _MemoryApi();
      await _pumpMemory(tester, api);

      expect(find.text('Memory is on'), findsOneWidget);

      await _tap(tester, find.byType(Switch).first);

      expect(api.enabledCalls, [false]);
      expect(find.text('Memory is off'), findsOneWidget);
      expect(
        find.textContaining('restart'),
        findsNothing,
        reason: 'the toggle takes effect immediately; nothing restarts',
      );
    });

    testWidgets('leaves the vault readable while memory is off', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          settings: const MemorySettings(
            enabled: false,
            vaultRoot: '/memory',
            vaultWritable: true,
            unavailableReason: MemoryUnavailableReason.disabled,
          ),
          persona: _document(
            content: '# Persona\n\nStill here.\n',
            unavailableReason: MemoryUnavailableReason.disabled,
            status: MemoryNoteStatus.unmanaged,
          ),
        );
      await _pumpMemory(tester, api);

      expect(find.text('Memory is off'), findsOneWidget);
      expect(find.textContaining('# Persona'), findsWidgets);
      expect(
        find.textContaining('Memory tools are unavailable while memory is off'),
        findsWidgets,
      );
    });

    testWidgets('a refused toggle is visible and changes nothing', (
      tester,
    ) async {
      final api = _MemoryApi()..enableError = StateError('backend refused');
      await _pumpMemory(tester, api);

      await _tap(tester, find.byType(Switch).first);

      expect(find.textContaining('backend refused'), findsWidgets);
      expect(find.text('Memory is on'), findsOneWidget);
    });
  });

  group('tier rows', () {
    testWidgets('name every unreadable tier instead of showing it empty', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          persona: _document(
            content: '',
            unavailableReason: MemoryUnavailableReason.vaultUnreadable,
            parseError: 'persona.md is a symlink',
            status: MemoryNoteStatus.unmanaged,
          ),
          profile: _document(
            content: '',
            unavailableReason: MemoryUnavailableReason.contentParseFailed,
            parseError: 'frontmatter is malformed',
          ),
          tiers: const [
            MemoryTierState(
              tier: MemoryTier.belief,
              enabled: true,
              unavailableReason: MemoryUnavailableReason.contentTooLarge,
              parseError: 'the vault holds more than 4096 indexed files',
            ),
          ],
        );
      await _pumpMemory(tester, api);

      expect(find.textContaining('could not be read'), findsWidgets);
      expect(find.textContaining('persona.md is a symlink'), findsOneWidget);
      expect(find.textContaining('frontmatter is malformed'), findsOneWidget);
      expect(find.textContaining('could not be parsed'), findsWidgets);
      expect(find.textContaining('too large'), findsWidgets);
      expect(
        find.textContaining('4096 indexed files'),
        findsOneWidget,
        reason: 'the server said why; the page must not swallow it',
      );
    });

    testWidgets('show beliefs with their status and provenance', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          notes: [
            MemoryNote(
              noteId: 'note-1',
              path: 'beliefs/people/ada.md',
              title: 'Ada',
              content: 'They take their coffee black.',
              contentHash: 'sha256:note',
              status: MemoryNoteStatus.managed,
              tier: MemoryTier.belief,
              provenance: const [
                MemoryProvenance(
                  kind: MemoryProvenanceKind.promotedFromCandidate,
                  sourceSessionId: 'sess-1',
                  sourceSessionTitle: 'Coffee talk',
                  evidenceCount: 2,
                ),
              ],
            ),
            MemoryNote(
              noteId: 'note-2',
              path: 'beliefs/people/bo.md',
              title: 'Bo',
              content: 'They bike to work.',
              contentHash: 'sha256:note-2',
              status: MemoryNoteStatus.unmanaged,
              tier: MemoryTier.belief,
              provenance: const [
                MemoryProvenance(
                  kind: MemoryProvenanceKind.promotedFromCandidate,
                  sourceSessionId: 'sess-2',
                  withdrawn: true,
                  evidenceCount: 0,
                ),
              ],
            ),
            MemoryNote(
              noteId: 'note-3',
              path: 'beliefs/broken.md',
              title: 'Broken',
              content: '',
              contentHash: '',
              status: MemoryNoteStatus.managed,
              tier: MemoryTier.belief,
              parseError: 'duplicate note id',
              unavailableReason: MemoryUnavailableReason.contentParseFailed,
            ),
          ],
        );
      await _pumpMemory(tester, api);

      expect(find.text('Ada'), findsOneWidget);
      expect(find.textContaining('coffee black'), findsOneWidget);
      expect(find.textContaining('Coffee talk'), findsOneWidget);
      expect(find.textContaining('2 pieces of evidence'), findsOneWidget);
      expect(find.textContaining('Turing may rewrite'), findsWidgets);
      expect(
        find.textContaining('You have taken this note over'),
        findsWidgets,
      );
      expect(
        find.textContaining('evidence behind this was withdrawn'),
        findsOneWidget,
      );
      expect(find.textContaining('no evidence'), findsWidgets);
      expect(find.textContaining('duplicate note id'), findsOneWidget);
      expect(
        find.textContaining('not searchable'),
        findsWidgets,
        reason: 'a note that failed to parse is not in the index',
      );
    });

    testWidgets('says a note nobody accounted for is not being found', (
      tester,
    ) async {
      // UNSPECIFIED is what this build decodes any reason a newer server
      // invents into, and it is also the server literally not answering. Either
      // way nothing has said this note is in the index — so the page must not
      // render it as an ordinary belief Turing can find, and it must put the
      // unanswered read into words rather than leaving the line blank.
      final api = _MemoryApi()
        ..state = _state(
          notes: [
            MemoryNote(
              noteId: 'note-unknown',
              path: 'beliefs/unknown.md',
              title: 'Unaccounted',
              content: 'They keep bees.',
              contentHash: 'sha256:unknown',
              status: MemoryNoteStatus.managed,
              tier: MemoryTier.belief,
              unavailableReason: MemoryUnavailableReason.unspecified,
            ),
          ],
        );
      await _pumpMemory(tester, api);

      expect(find.text('Unaccounted'), findsOneWidget);
      expect(
        find.textContaining('not searchable'),
        findsWidgets,
        reason: 'nothing said this note is in the index',
      );
      expect(
        find.textContaining('The server did not say'),
        findsWidgets,
        reason: 'silence about the reason reads as a healthy note',
      );
    });

    testWidgets('a withdrawn note is not offered as something Turing finds', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          notes: [
            MemoryNote(
              noteId: 'note-withdrawn',
              path: 'beliefs/withdrawn.md',
              title: 'Withdrawn',
              content: 'They keep bees.',
              contentHash: 'sha256:withdrawn',
              status: MemoryNoteStatus.withdrawn,
              tier: MemoryTier.belief,
              unavailableReason: MemoryUnavailableReason.none,
            ),
          ],
        );
      await _pumpMemory(tester, api);

      expect(find.text('Withdrawn'), findsOneWidget);
      expect(
        find.textContaining('not searchable'),
        findsWidgets,
        reason: 'search does not answer from a withdrawn note',
      );
    });
  });

  group('the persona and profile editors', () {
    testWidgets(
      'persona is named as the only place the user instructs Turing',
      (tester) async {
        await _pumpMemory(tester, _MemoryApi());

        expect(find.text('persona.md'), findsOneWidget);
        expect(find.text('profile.md'), findsOneWidget);
        expect(find.textContaining('You are its only author'), findsOneWidget);
        expect(find.textContaining('Turing never writes it'), findsOneWidget);
      },
    );

    testWidgets('the editors say which version they hold and when it changed', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          persona: MemoryDocument(
            content: '# Persona\n\nBe direct.\n',
            contentHash: 'sha256:persona',
            status: MemoryNoteStatus.unmanaged,
            unavailableReason: MemoryUnavailableReason.none,
            updatedAt: DateTime.utc(2026, 8, 24, 12),
          ),
        );
      await _pumpMemory(tester, api);

      expect(
        find.textContaining('Editing version sha256:persona'),
        findsOneWidget,
      );
      expect(find.textContaining('Last changed'), findsWidgets);
    });

    testWidgets('saving the persona sends the hash it was read at', (
      tester,
    ) async {
      final api = _MemoryApi();
      await _pumpMemory(tester, api);

      await tester.enterText(
        find.byKey(const Key('memory-persona-editor')),
        '# Persona\n\nBe warmer.\n',
      );
      await _tap(tester, find.byKey(const Key('memory-persona-save')));

      expect(api.personaSaves, [
        ('# Persona\n\nBe warmer.\n', 'sha256:persona'),
      ]);
      expect(api.stateReads, 2, reason: 'a save refreshes what the page shows');
    });

    testWidgets('saving the profile goes through the hand-save, not an apply', (
      tester,
    ) async {
      final api = _MemoryApi();
      await _pumpMemory(tester, api);

      await tester.ensureVisible(
        find.byKey(const Key('memory-profile-editor')),
      );
      await tester.pumpAndSettle();
      await tester.enterText(
        find.byKey(const Key('memory-profile-editor')),
        '# Profile\n\nI bike to work.\n',
      );
      await _tap(tester, find.byKey(const Key('memory-profile-save')));

      expect(api.profileSaves, [
        ('# Profile\n\nI bike to work.\n', 'sha256:profile'),
      ]);
      expect(api.profileApplies, isEmpty);
    });

    testWidgets('a lost compare-and-set tells the user what to do about it', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..personaSaveError = const TuringApiException(
          code: 'failed_precondition',
          message:
              'the file changed on disk since this editor read it; finish and '
              'close the memory editor, re-read the document, and save again',
        );
      await _pumpMemory(tester, api);

      await tester.enterText(
        find.byKey(const Key('memory-persona-editor')),
        'new text',
      );
      await _tap(tester, find.byKey(const Key('memory-persona-save')));

      expect(find.textContaining('changed on disk'), findsWidgets);
      expect(find.textContaining('close the memory editor'), findsWidgets);
      expect(find.textContaining('re-read'), findsWidgets);
      expect(
        find.byKey(const Key('memory-persona-reread')),
        findsOneWidget,
        reason: 'the user re-reads deliberately; nothing re-prepares itself',
      );
      expect(
        api.stateReads,
        1,
        reason: 'a refused save must not silently reload and overwrite',
      );

      await _tap(tester, find.byKey(const Key('memory-persona-reread')));

      expect(api.stateReads, 2);
      expect(api.personaSaves, hasLength(1));
    });
  });

  group('inbox review', () {
    testWidgets('shows the whole proposal with where it came from', (
      tester,
    ) async {
      final api = _MemoryApi()..state = _state(candidates: [_candidate()]);
      await _pumpMemory(tester, api);

      expect(
        find.textContaining('They bike to work every day.'),
        findsOneWidget,
      );
      expect(find.text('inbox/01-bikes.md'), findsOneWidget);
      expect(find.textContaining('Commute chat'), findsOneWidget);
      expect(find.textContaining('sess-1'), findsOneWidget);
      expect(find.textContaining('Waiting for you'), findsWidgets);
      expect(find.text('Promote'), findsOneWidget);
      expect(find.text('Reject'), findsOneWidget);
      expect(find.text('Apply'), findsNothing);
    });

    testWidgets('a profile edit is applied, not promoted, and shows the text', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          candidates: [
            _candidate(
              candidateId: 'cand-profile',
              kind: MemoryCandidateKind.profileEdit,
              content: 'Bikes to work.',
              contentHash: 'sha256:proposed',
            ),
          ],
        );
      await _pumpMemory(tester, api);

      expect(find.text('Apply'), findsOneWidget);
      expect(find.text('Promote'), findsNothing);
      expect(find.textContaining('Bikes to work.'), findsWidgets);
      expect(
        find.textContaining('sha256:profile'),
        findsWidgets,
        reason: 'the apply is compare-and-set against the profile it read',
      );
      expect(
        find.textContaining('sha256:proposed'),
        findsWidgets,
        reason: 'and against the proposal the result was composed from',
      );

      await _tap(tester, find.text('Apply'));

      // The whole resulting document, not the fragment: the server replaces
      // profile.md with exactly what is sent, so sending only the proposal
      // would erase everything the user had written about themselves.
      expect(api.profileApplies, [
        (
          'cand-profile',
          '# Profile\n\nNothing yet.\n\nBikes to work.\n',
          'sha256:profile',
          'sha256:proposed',
        ),
      ]);
      expect(api.stateReads, 2);
    });

    testWidgets('promoting and rejecting reach the server and refresh', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          candidates: [
            _candidate(),
            _candidate(candidateId: 'cand-2', inboxPath: 'inbox/02-tea.md'),
          ],
        );
      await _pumpMemory(tester, api);

      await _tap(tester, find.text('Promote').first);
      expect(api.promotions, [('cand-1', 'sha256:cand')]);
      expect(api.stateReads, 2);

      await _tap(tester, find.text('Reject').last);
      expect(api.rejections, [('cand-2', 'sha256:cand')]);
      expect(api.stateReads, 3);
    });

    testWidgets('a failed decision is shown and the proposal stays', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(candidates: [_candidate()])
        ..promoteError = const TuringApiException(
          code: 'aborted',
          message: 'this proposal changed since it was read',
        );
      await _pumpMemory(tester, api);

      await _tap(tester, find.text('Promote'));

      expect(find.textContaining('changed since it was read'), findsWidgets);
      expect(
        find.textContaining('They bike to work every day.'),
        findsOneWidget,
      );
    });

    testWidgets('an unmanaged draft is readable and has no buttons', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          candidates: [
            _candidate(
              candidateId: '',
              inboxPath: 'inbox/my-own-note.md',
              content: 'I dropped this here myself.',
              contentHash: '',
              managed: false,
              provenance: const [],
            ),
          ],
        );
      await _pumpMemory(tester, api);

      expect(
        find.textContaining('I dropped this here myself.'),
        findsOneWidget,
      );
      expect(find.textContaining('Your own draft'), findsOneWidget);
      expect(find.text('Promote'), findsNothing);
      expect(find.text('Reject'), findsNothing);
      expect(find.text('Apply'), findsNothing);
      expect(
        find.textContaining('move the file'),
        findsOneWidget,
        reason: 'the only way in for a draft Turing never created',
      );
    });

    testWidgets('a withdrawn proposal says its session is gone', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          candidates: [
            _candidate(
              state: MemoryCandidateState.withdrawn,
              provenance: const [
                MemoryProvenance(
                  kind: MemoryProvenanceKind.promotedFromCandidate,
                  sourceSessionId: 'sess-1',
                  withdrawn: true,
                ),
              ],
            ),
          ],
        );
      await _pumpMemory(tester, api);

      expect(
        find.textContaining('conversation behind this was deleted'),
        findsWidgets,
      );
      expect(find.text('Promote'), findsNothing);
      expect(find.text('Reject'), findsNothing);
    });

    testWidgets('a proposal that could not be read says so, with detail', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          candidates: [
            _candidate(
              content: '',
              parseError: 'the inbox file is 20 KiB, over the 16 KiB limit',
              unavailableReason: MemoryUnavailableReason.contentTooLarge,
            ),
          ],
        );
      await _pumpMemory(tester, api);

      expect(find.textContaining('over the 16 KiB limit'), findsOneWidget);
      expect(
        find.text('Promote'),
        findsNothing,
        reason: 'nobody accepts text they were never shown',
      );
      expect(
        find.text('Reject'),
        findsOneWidget,
        reason: 'the file is in the vault, and throwing it away is the way out',
      );
    });

    // Round 18. A file Turing cannot open is one it can neither show nor
    // remove: proving which entry an unlink would take needs a descriptor, and
    // there is none. So this card offers no button at all — and a card with no
    // button and no way forward is a dead end unless it says what to do.
    testWidgets('a proposal Turing cannot open says what would make it '
        'decidable', (tester) async {
      final api = _MemoryApi()
        ..state = _state(
          candidates: [
            _candidate(
              content: '',
              contentHash: '',
              unavailableReason: MemoryUnavailableReason.vaultUnreadable,
            ),
          ],
        );
      await _pumpMemory(tester, api);

      expect(find.text('Promote'), findsNothing);
      expect(
        find.text('Reject'),
        findsNothing,
        reason: 'the server cannot prove what a removal would remove',
      );
      expect(
        find.textContaining('readable'),
        findsWidgets,
        reason: 'a file Turing cannot open has to be opened before it can go',
      );
    });

    // Round 5. The server stopped serving the database's copy of a proposal
    // whose file it could not open, so this is what the page is handed: the
    // proposal is still there, and its words are not.
    testWidgets('a proposal with no vault behind it shows no body and no '
        'buttons', (tester) async {
      final api = _MemoryApi()
        ..state = _state(
          candidates: [
            _candidate(
              content: '',
              contentHash: '',
              unavailableReason: MemoryUnavailableReason.vaultMissing,
            ),
          ],
        );
      await _pumpMemory(tester, api);

      expect(find.text('inbox/01-bikes.md'), findsOneWidget);
      expect(
        find.textContaining('They bike to work every day.'),
        findsNothing,
        reason: 'the words live in a vault nobody can currently open',
      );
      // Every decision RPC needs the vault — a rejection removes the file —
      // so no button here is one the server could honour.
      expect(find.text('Promote'), findsNothing);
      expect(find.text('Apply'), findsNothing);
      expect(find.text('Reject'), findsNothing);
      expect(
        find.textContaining('nothing here to accept'),
        findsOneWidget,
        reason: 'a card with no actions and no word reads as nothing to decide',
      );
      expect(
        find.textContaining('throw it away'),
        findsNothing,
        reason: 'a rejection needs the vault this card is missing',
      );
      expect(
        find.textContaining('does not understand'),
        findsNothing,
        reason: 'the shape is fine; it is the vault that is gone',
      );
    });

    // A reason a newer server invented. The card must fail closed and say so,
    // rather than treating "not one of the four problems I know" as health.
    testWidgets('a proposal whose trouble this build cannot name offers '
        'nothing', (tester) async {
      final api = _MemoryApi()
        ..state = _state(
          candidates: [
            _candidate(
              content: '',
              contentHash: '',
              unavailableReason: MemoryUnavailableReason.unspecified,
            ),
          ],
        );
      await _pumpMemory(tester, api);

      expect(find.text('Promote'), findsNothing);
      expect(find.text('Apply'), findsNothing);
      expect(find.text('Reject'), findsNothing);
      expect(find.textContaining('does not understand'), findsOneWidget);
    });
  });

  group('evidence a belief no longer has', () {
    testWidgets('a belief with nothing behind it says so, not nothing', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          notes: [
            MemoryNote(
              noteId: 'note-1',
              path: 'beliefs/people/ada.md',
              title: 'Ada',
              content: 'They take their coffee black.',
              contentHash: 'sha256:note-1',
              status: MemoryNoteStatus.managed,
              tier: MemoryTier.belief,
              unavailableReason: MemoryUnavailableReason.none,
              provenance: const [],
            ),
          ],
        );
      await _pumpMemory(tester, api);

      expect(
        find.textContaining('no evidence'),
        findsOneWidget,
        reason: 'an empty provenance list is a fact, not an absence of one',
      );
    });

    testWidgets('a withdrawn belief says its evidence is gone', (tester) async {
      final api = _MemoryApi()
        ..state = _state(
          notes: [
            MemoryNote(
              noteId: 'note-1',
              path: 'beliefs/people/ada.md',
              title: 'Ada',
              content: 'They take their coffee black.',
              contentHash: 'sha256:note-1',
              status: MemoryNoteStatus.withdrawn,
              tier: MemoryTier.belief,
              unavailableReason: MemoryUnavailableReason.none,
              provenance: const [],
            ),
          ],
        );
      await _pumpMemory(tester, api);

      expect(find.textContaining('withdrawn'), findsWidgets);
      expect(
        find.textContaining('never had any evidence'),
        findsNothing,
        reason: 'withdrawn and never-evidenced are different histories',
      );
    });

    testWidgets('a withdrawn provenance row shows no conversation to open', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          notes: [
            MemoryNote(
              noteId: 'note-1',
              path: 'beliefs/people/ada.md',
              title: 'Ada',
              content: 'They take their coffee black.',
              contentHash: 'sha256:note-1',
              status: MemoryNoteStatus.withdrawn,
              tier: MemoryTier.belief,
              unavailableReason: MemoryUnavailableReason.none,
              provenance: const [
                MemoryProvenance(
                  kind: MemoryProvenanceKind.promotedFromCandidate,
                  withdrawn: true,
                ),
              ],
            ),
          ],
        );
      await _pumpMemory(tester, api);

      expect(find.textContaining('withdrawn'), findsWidgets);
      expect(
        find.textContaining('From '),
        findsNothing,
        reason: 'there is no conversation left to name',
      );
    });
  });

  group('tier counts and settings', () {
    testWidgets('the vault says whether it could be read, not only why not', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          settings: const MemorySettings(
            enabled: true,
            vaultRoot: '/memory',
            vaultWritable: false,
            unavailableReason: MemoryUnavailableReason.vaultUnreadable,
          ),
        );
      await _pumpMemory(tester, api);

      expect(
        find.textContaining('could not be read'),
        findsWidgets,
        reason: 'an unreadable vault is not rendered as a healthy one',
      );
    });

    testWidgets('each tier says how much it holds and what is waiting', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          tiers: const [
            MemoryTierState(
              tier: MemoryTier.persona,
              enabled: true,
              noteCount: 1,
            ),
            MemoryTierState(
              tier: MemoryTier.profile,
              enabled: true,
              noteCount: 1,
              pendingCandidateCount: 2,
            ),
            MemoryTierState(
              tier: MemoryTier.belief,
              enabled: true,
              noteCount: 3,
              pendingCandidateCount: 4,
            ),
          ],
        );
      await _pumpMemory(tester, api);

      expect(find.textContaining('3 items'), findsOneWidget);
      expect(find.textContaining('4 proposals'), findsOneWidget);
      expect(find.textContaining('2 proposals'), findsOneWidget);
    });
  });

  group('unsaved text the user typed', () {
    testWidgets('turning memory off does not throw away a draft persona', (
      tester,
    ) async {
      final api = _MemoryApi();
      await _pumpMemory(tester, api);

      await tester.enterText(
        find.byKey(const Key('memory-persona-editor')),
        'half a thought',
      );
      await _tap(tester, find.byKey(const Key('memory-enabled-toggle')));

      expect(
        find.text('half a thought'),
        findsOneWidget,
        reason: 'a refresh is not permission to discard what the user typed',
      );
    });

    testWidgets('deciding a proposal does not throw away a draft profile', (
      tester,
    ) async {
      final api = _MemoryApi()..state = _state(candidates: [_candidate()]);
      await _pumpMemory(tester, api);

      await tester.enterText(
        find.byKey(const Key('memory-profile-editor')),
        'half a profile',
      );
      await _tap(tester, find.text('Promote'));

      expect(find.text('half a profile'), findsOneWidget);
    });

    testWidgets('saving the persona leaves a draft profile alone', (
      tester,
    ) async {
      final api = _MemoryApi();
      await _pumpMemory(tester, api);

      await tester.enterText(
        find.byKey(const Key('memory-profile-editor')),
        'half a profile',
      );
      await tester.enterText(
        find.byKey(const Key('memory-persona-editor')),
        '# Persona\n\nBe warmer.\n',
      );
      await _tap(tester, find.byKey(const Key('memory-persona-save')));

      expect(find.text('half a profile'), findsOneWidget);
      expect(
        find.text('# Persona\n\nBe warmer.\n'),
        findsNothing,
        reason: 'the saved editor takes what the vault now holds',
      );
    });

    testWidgets('re-reading one document leaves the other draft alone', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..personaSaveError = const TuringApiException(
          code: 'failed_precondition',
          message: 'the file changed on disk since this editor read it',
        );
      await _pumpMemory(tester, api);

      await tester.enterText(
        find.byKey(const Key('memory-profile-editor')),
        'half a profile',
      );
      await tester.enterText(
        find.byKey(const Key('memory-persona-editor')),
        'a persona draft',
      );
      await _tap(tester, find.byKey(const Key('memory-persona-save')));

      expect(find.text('a persona draft'), findsOneWidget);
      expect(find.text('half a profile'), findsOneWidget);

      await _tap(tester, find.byKey(const Key('memory-persona-reread')));

      expect(
        find.text('a persona draft'),
        findsNothing,
        reason: 'the user asked for this one document to be re-read',
      );
      expect(find.text('half a profile'), findsOneWidget);
    });
  });

  group('clearing a document on purpose', () {
    testWidgets('an empty persona can be saved, because emptying is a choice', (
      tester,
    ) async {
      final api = _MemoryApi();
      await _pumpMemory(tester, api);

      await tester.enterText(
        find.byKey(const Key('memory-persona-editor')),
        '   ',
      );
      await _tap(tester, find.byKey(const Key('memory-persona-save')));

      expect(api.personaSaves, [('   ', 'sha256:persona')]);
    });

    testWidgets('a document that could not be read offers no save', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          persona: _document(
            content: '',
            contentHash: '',
            status: MemoryNoteStatus.unmanaged,
            unavailableReason: MemoryUnavailableReason.vaultUnreadable,
            parseError: 'persona.md is a symlink',
          ),
        );
      await _pumpMemory(tester, api);

      final save = tester.widget<FilledButton>(
        find.byKey(const Key('memory-persona-save')),
      );
      expect(
        save.onPressed,
        isNull,
        reason: 'a save the server would refuse is not offered',
      );
      expect(find.textContaining('symlink'), findsOneWidget);
    });
  });

  group('a vault nobody has written into yet', () {
    testWidgets('offers the first persona save instead of refusing it', (
      tester,
    ) async {
      // persona.md and profile.md do not exist until somebody writes them, so
      // a fresh install opens this page with both documents missing. Treating
      // that as unwritable makes the page permanently read-only: the one
      // action that would fix it is the action being refused.
      final api = _MemoryApi()
        ..state = _state(
          persona: _document(
            content: '',
            contentHash: '',
            status: MemoryNoteStatus.unmanaged,
            unavailableReason: MemoryUnavailableReason.vaultMissing,
          ),
          profile: _document(
            content: '',
            contentHash: '',
            unavailableReason: MemoryUnavailableReason.vaultMissing,
          ),
          tiers: const [],
        );
      await _pumpMemory(tester, api);

      final personaSave = tester.widget<FilledButton>(
        find.byKey(const Key('memory-persona-save')),
      );
      final profileSave = tester.widget<FilledButton>(
        find.byKey(const Key('memory-profile-save')),
      );
      expect(personaSave.onPressed, isNotNull);
      expect(profileSave.onPressed, isNotNull);
      expect(find.textContaining('not in the vault yet'), findsWidgets);
    });

    testWidgets('writes both documents for the first time with no version', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          persona: _document(
            content: '',
            contentHash: '',
            status: MemoryNoteStatus.unmanaged,
            unavailableReason: MemoryUnavailableReason.vaultMissing,
          ),
          profile: _document(
            content: '',
            contentHash: '',
            unavailableReason: MemoryUnavailableReason.vaultMissing,
          ),
          tiers: const [],
        );
      await _pumpMemory(tester, api);

      await tester.enterText(
        find.byKey(const Key('memory-persona-editor')),
        '# Persona\n\nBe direct.\n',
      );
      await _tap(tester, find.byKey(const Key('memory-persona-save')));
      await tester.enterText(
        find.byKey(const Key('memory-profile-editor')),
        '# Profile\n\nI bike to work.\n',
      );
      await _tap(tester, find.byKey(const Key('memory-profile-save')));

      expect(api.personaSaves, [('# Persona\n\nBe direct.\n', '')]);
      expect(api.profileSaves, [('# Profile\n\nI bike to work.\n', '')]);
    });
  });

  group('no vault open at all', () {
    // The server reports VAULT_MISSING on a document both when the vault is
    // open and simply has not been written yet, and when there is no vault
    // open to write into — the document's own reason cannot tell those apart.
    // Only settings.vaultRoot says which one this is, so the page must look
    // there before offering to create a file with nowhere to land.
    testWidgets(
      'disables save and explains why instead of offering to create a file',
      (tester) async {
        final api = _MemoryApi()
          ..state = _state(
            settings: const MemorySettings(
              enabled: true,
              vaultRoot: '',
              vaultWritable: false,
              unavailableReason: MemoryUnavailableReason.vaultMissing,
            ),
            persona: _document(
              content: '',
              contentHash: '',
              status: MemoryNoteStatus.unmanaged,
              unavailableReason: MemoryUnavailableReason.vaultMissing,
            ),
            profile: _document(
              content: '',
              contentHash: '',
              unavailableReason: MemoryUnavailableReason.vaultMissing,
            ),
            tiers: const [],
          );
        await _pumpMemory(tester, api);

        final personaSave = tester.widget<FilledButton>(
          find.byKey(const Key('memory-persona-save')),
        );
        final profileSave = tester.widget<FilledButton>(
          find.byKey(const Key('memory-profile-save')),
        );
        expect(personaSave.onPressed, isNull);
        expect(profileSave.onPressed, isNull);
        expect(
          find.textContaining('Open or configure a vault'),
          findsNWidgets(2),
          reason: 'both documents share the same missing vault',
        );
      },
    );

    testWidgets('stays refused even while memory is switched off too', (
      tester,
    ) async {
      // Memory being off is its own, separate reason a save is refused
      // elsewhere on this page — but with no vault open, the document reason
      // is still VAULT_MISSING (the server reports that before it ever looks
      // at the toggle), so this is the same "no vault" case, not a new one.
      final api = _MemoryApi()
        ..state = _state(
          settings: const MemorySettings(
            enabled: false,
            vaultRoot: '',
            vaultWritable: false,
            unavailableReason: MemoryUnavailableReason.disabled,
          ),
          persona: _document(
            content: '',
            contentHash: '',
            status: MemoryNoteStatus.unmanaged,
            unavailableReason: MemoryUnavailableReason.vaultMissing,
          ),
          profile: _document(
            content: '',
            contentHash: '',
            unavailableReason: MemoryUnavailableReason.vaultMissing,
          ),
          tiers: const [],
        );
      await _pumpMemory(tester, api);

      final personaSave = tester.widget<FilledButton>(
        find.byKey(const Key('memory-persona-save')),
      );
      final profileSave = tester.widget<FilledButton>(
        find.byKey(const Key('memory-profile-save')),
      );
      expect(personaSave.onPressed, isNull);
      expect(profileSave.onPressed, isNull);
      expect(find.textContaining('Open or configure a vault'), findsNWidgets(2));
    });
  });

  group('a document longer than a run carries', () {
    testWidgets('shows the whole document, never the runtime notice', (
      tester,
    ) async {
      // The editor is over the file. The truncation notice is something the
      // runtime writes into the pin so a model knows it is holding a fragment;
      // showing it here would put words in the editor the user never typed and
      // would save them back into their own persona.
      final api = _MemoryApi()
        ..state = _state(
          persona: _document(
            content: '# Persona\n\nEvery byte of it, all the way down.\n',
            contentHash: 'sha256:whole-document',
            status: MemoryNoteStatus.unmanaged,
            pinnedTruncated: true,
            pinnedBytes: 4096,
          ),
        );
      await _pumpMemory(tester, api);

      final editor = tester.widget<TextField>(
        find.byKey(const Key('memory-persona-editor')),
      );
      expect(
        editor.controller?.text,
        '# Persona\n\nEvery byte of it, all the way down.\n',
      );
      expect(find.textContaining('Open the vault to read the rest'), findsNothing);
      expect(find.textContaining('4096'), findsWidgets);
      expect(find.textContaining('reach'), findsWidgets);
    });

    testWidgets('saves the whole document against the document version', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          persona: _document(
            content: '# Persona\n\nA long one.\n',
            contentHash: 'sha256:whole-document',
            status: MemoryNoteStatus.unmanaged,
            pinnedTruncated: true,
            pinnedBytes: 4096,
          ),
        );
      await _pumpMemory(tester, api);

      await tester.enterText(
        find.byKey(const Key('memory-persona-editor')),
        '# Persona\n\nA long one, edited.\n',
      );
      await _tap(tester, find.byKey(const Key('memory-persona-save')));

      expect(api.personaSaves, [
        ('# Persona\n\nA long one, edited.\n', 'sha256:whole-document'),
      ]);
    });

    testWidgets('says nothing about truncation when nothing is cut', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          persona: _document(
            content: '# Persona\n\nShort.\n',
            contentHash: 'sha256:persona',
            status: MemoryNoteStatus.unmanaged,
            pinnedBytes: 20,
          ),
        );
      await _pumpMemory(tester, api);

      expect(find.textContaining('only the first'), findsNothing);
    });
  });

  group('the page as a whole', () {
    testWidgets('a backend failure is not rendered as an empty vault', (
      tester,
    ) async {
      final api = _MemoryApi()..listError = StateError('offline');
      await _pumpMemory(tester, api);

      expect(find.text('Could not reach the backend'), findsOneWidget);
      expect(find.text('Memory is on'), findsNothing);
      expect(find.text('Try again'), findsOneWidget);
    });

    testWidgets('renders memory as plain text, never as markup', (
      tester,
    ) async {
      final api = _MemoryApi()
        ..state = _state(
          persona: _document(
            content: '<b>not bold</b> [link](https://example.test)',
            contentHash: 'sha256:persona',
            status: MemoryNoteStatus.unmanaged,
          ),
        );
      await _pumpMemory(tester, api);

      expect(
        find.textContaining('<b>not bold</b>'),
        findsWidgets,
        reason: 'untrusted memory text is shown as written, not interpreted',
      );
    });

    testWidgets('does not overflow a compact landscape layout', (tester) async {
      final api = _MemoryApi()
        ..state = _state(
          candidates: [
            for (var index = 0; index < 6; index++)
              _candidate(
                candidateId: 'cand-$index',
                inboxPath: 'inbox/0$index-note.md',
                content:
                    'A proposal long enough to wrap in a narrow window. ' * 3,
              ),
          ],
          notes: [
            for (var index = 0; index < 6; index++)
              MemoryNote(
                noteId: 'note-$index',
                path: 'beliefs/people/person-$index.md',
                title: 'Person $index',
                content:
                    'A belief long enough to wrap in a narrow window. ' * 3,
                contentHash: 'sha256:note-$index',
                status: MemoryNoteStatus.managed,
                tier: MemoryTier.belief,
              ),
          ],
        );
      await _pumpMemory(tester, api, size: const Size(740, 360));

      expect(tester.takeException(), isNull);
    });
  });
}

/// Scrolls a control into view before tapping it. The Memory page is a single
/// scrolling column, so a button further down is rendered but not hit-testable
/// until it is on screen.
Future<void> _tap(WidgetTester tester, Finder finder) async {
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  await tester.tap(finder);
  await tester.pumpAndSettle();
}

Future<void> _pumpMemory(
  WidgetTester tester,
  _MemoryApi api, {
  Size size = const Size(1200, 900),
}) async {
  tester.view.physicalSize = size;
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

MemoryDocument _document({
  String content = '',
  String contentHash = '',
  MemoryNoteStatus status = MemoryNoteStatus.managed,
  MemoryUnavailableReason unavailableReason = MemoryUnavailableReason.none,
  String parseError = '',
  bool pinnedTruncated = false,
  int pinnedBytes = 0,
}) {
  return MemoryDocument(
    content: content,
    contentHash: contentHash,
    status: status,
    unavailableReason: unavailableReason,
    parseError: parseError,
    pinnedTruncated: pinnedTruncated,
    pinnedBytes: pinnedBytes,
  );
}

MemoryCandidate _candidate({
  String candidateId = 'cand-1',
  MemoryCandidateKind kind = MemoryCandidateKind.belief,
  String inboxPath = 'inbox/01-bikes.md',
  String content = 'They bike to work every day.',
  String contentHash = 'sha256:cand',
  MemoryCandidateState state = MemoryCandidateState.pending,
  bool managed = true,
  String parseError = '',
  MemoryUnavailableReason unavailableReason = MemoryUnavailableReason.none,
  List<MemoryProvenance> provenance = const [
    MemoryProvenance(
      kind: MemoryProvenanceKind.promotedFromCandidate,
      sourceSessionId: 'sess-1',
      sourceSessionTitle: 'Commute chat',
      evidenceCount: 1,
    ),
  ],
}) {
  return MemoryCandidate(
    candidateId: candidateId,
    kind: kind,
    inboxPath: inboxPath,
    content: content,
    contentHash: contentHash,
    state: state,
    managed: managed,
    parseError: parseError,
    unavailableReason: unavailableReason,
    provenance: provenance,
  );
}

MemoryState _state({
  MemorySettings settings = const MemorySettings(
    enabled: true,
    vaultRoot: '/memory',
    vaultWritable: true,
    unavailableReason: MemoryUnavailableReason.none,
  ),
  MemoryDocument? persona,
  MemoryDocument? profile,
  List<MemoryTierState> tiers = const [
    MemoryTierState(tier: MemoryTier.persona, enabled: true, noteCount: 1),
    MemoryTierState(tier: MemoryTier.profile, enabled: true, noteCount: 1),
    MemoryTierState(tier: MemoryTier.belief, enabled: true),
  ],
  List<MemoryNote> notes = const [],
  List<MemoryCandidate> candidates = const [],
}) {
  return MemoryState(
    settings: settings,
    persona:
        persona ??
        _document(
          content: '# Persona\n\nBe direct.\n',
          contentHash: 'sha256:persona',
          status: MemoryNoteStatus.unmanaged,
        ),
    profile:
        profile ??
        _document(
          content: '# Profile\n\nNothing yet.\n',
          contentHash: 'sha256:profile',
        ),
    tiers: tiers,
    notes: notes,
    candidates: candidates,
  );
}

class _MemoryApi extends TuringApi
    with
        NoAuditApi,
        NoAutomationsApi,
        NoExternalAgentsApi,
        NoIntegrationsApi,
        NoSessionLifecycleApi,
        NoSkillsApi,
        NoTelemetryApi {
  MemoryState state = _state();
  Object? listError;
  Object? enableError;
  Object? promoteError;
  Object? personaSaveError;
  int stateReads = 0;
  final List<bool> enabledCalls = [];
  final List<(String, String)> promotions = [];
  final List<(String, String)> rejections = [];
  final List<(String, String, String, String)> profileApplies = [];
  final List<(String, String)> personaSaves = [];
  final List<(String, String)> profileSaves = [];

  @override
  Future<MemoryState> listMemoryState() async {
    stateReads++;
    if (listError case final error?) throw error;
    return state;
  }

  @override
  Future<MemorySettings> setMemoryEnabled({required bool enabled}) async {
    if (enableError case final error?) throw error;
    enabledCalls.add(enabled);
    final settings = MemorySettings(
      enabled: enabled,
      vaultRoot: state.settings.vaultRoot,
      vaultWritable: state.settings.vaultWritable,
      unavailableReason: enabled
          ? MemoryUnavailableReason.none
          : MemoryUnavailableReason.disabled,
    );
    state = MemoryState(
      settings: settings,
      persona: state.persona,
      profile: state.profile,
      tiers: state.tiers,
      notes: state.notes,
      candidates: state.candidates,
    );
    return settings;
  }

  @override
  Future<MemoryCandidate> promoteMemoryCandidate({
    required String candidateId,
    required String expectedCandidateHash,
  }) async {
    if (promoteError case final error?) throw error;
    promotions.add((candidateId, expectedCandidateHash));
    return state.candidates.firstWhere(
      (candidate) => candidate.candidateId == candidateId,
    );
  }

  @override
  Future<MemoryCandidate> rejectMemoryCandidate({
    required String candidateId,
    String expectedCandidateHash = '',
    String reason = '',
  }) async {
    rejections.add((candidateId, expectedCandidateHash));
    return state.candidates.firstWhere(
      (candidate) => candidate.candidateId == candidateId,
    );
  }

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
    // Recorded before the refusal, so a test can assert the attempt happened
    // exactly once — a re-read that quietly retried the save would show up here
    // as a second entry.
    personaSaves.add((content, expectedContentHash));
    if (personaSaveError case final error?) throw error;
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
