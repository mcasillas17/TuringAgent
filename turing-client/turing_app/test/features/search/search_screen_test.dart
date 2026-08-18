import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter/semantics.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:turing_flutter_app/features/search/search_screen.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/models/search_hit.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/turing_event.dart';
import 'package:turing_flutter_app/networking/api_client.dart';
import 'package:turing_flutter_app/models/agent_descriptor.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';

void main() {
  group('SearchScreen', () {
    testWidgets('shows initial guidance and an exact-phrase hint', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      expect(find.byKey(const Key('search-initial')), findsOneWidget);
      expect(find.textContaining('exact phrase'), findsWidgets);
      expect(find.byIcon(Icons.search), findsWidgets);
      expect(find.byKey(const Key('search-field')), findsOneWidget);
      final field = tester.widget<TextField>(
        find.byKey(const Key('search-field')),
      );
      expect(field.textInputAction, TextInputAction.search);
      expect(field.decoration?.labelText, isNotNull);
      expect(field.decoration?.helperText, contains('exact phrase'));
      expect(find.byKey(const Key('search-clear-button')), findsOneWidget);
    });

    testWidgets('performs no request for blank input', (tester) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), '   ');
      await tester.pump(const Duration(milliseconds: 400));

      expect(api.queries, isEmpty);
      expect(find.byKey(const Key('search-initial')), findsOneWidget);
    });

    testWidgets('debounces changed input by exactly 350ms', (tester) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(
        find.byKey(const Key('search-field')),
        'deploy  staging',
      );
      await tester.pump(const Duration(milliseconds: 349));
      expect(api.queries, isEmpty);

      await tester.pump(const Duration(milliseconds: 1));
      expect(api.queries, ['deploy  staging']);
    });

    testWidgets(
      'drops the initial guidance for a pending first query during the debounce',
      (tester) async {
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.pump(const Duration(milliseconds: 100));

        // Still debouncing, so no request has gone out yet, but the screen
        // must already reflect the query being typed instead of the
        // untouched-screen guidance, and must not claim zero results for a
        // search that has never run.
        expect(api.queries, isEmpty);
        expect(find.byKey(const Key('search-initial')), findsNothing);
        expect(find.byKey(const Key('search-pending')), findsOneWidget);
        expect(find.byKey(const Key('search-empty')), findsNothing);
        expect(find.byKey(const Key('search-loading')), findsNothing);

        // The debounce window itself is unchanged: still exactly 350ms from
        // the keystroke, not restarted or fired early.
        await tester.pump(const Duration(milliseconds: 249));
        expect(api.queries, isEmpty);
        await tester.pump(const Duration(milliseconds: 1));
        expect(api.queries, ['deploy']);
        expect(find.byKey(const Key('search-loading')), findsOneWidget);
      },
    );

    testWidgets('keeps prior results visible while a new query debounces', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();
      api.searchCalls.single.completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          createdAt: DateTime.utc(2026, 8, 13),
        ),
      ]);
      await tester.pump();
      expect(find.byKey(const ValueKey('hit-msg-1')), findsOneWidget);

      await tester.enterText(
        find.byKey(const Key('search-field')),
        'deploy staging',
      );
      await tester.pump(const Duration(milliseconds: 100));

      // Results from the previous query stay on screen until fresh ones
      // replace them, rather than blanking out mid-typing.
      expect(find.byKey(const ValueKey('hit-msg-1')), findsOneWidget);
      expect(find.byKey(const Key('search-initial')), findsNothing);
      expect(find.byKey(const Key('search-empty')), findsNothing);
    });

    testWidgets(
      'returns to the initial guidance when a pending query is emptied',
      (tester) async {
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.pump(const Duration(milliseconds: 100));
        expect(find.byKey(const Key('search-initial')), findsNothing);

        await tester.enterText(find.byKey(const Key('search-field')), '');
        await tester.pump(const Duration(milliseconds: 400));

        expect(find.byKey(const Key('search-initial')), findsOneWidget);
        expect(find.byKey(const Key('search-pending')), findsNothing);
        expect(api.queries, isEmpty);
      },
    );

    testWidgets('trims only outer whitespace, keeping internal spacing', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(
        find.byKey(const Key('search-field')),
        '  deploy  staging  ',
      );
      await tester.pump(const Duration(milliseconds: 350));

      expect(api.queries, ['deploy  staging']);
    });

    testWidgets('sends punctuation-only input to the API', (tester) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), '???');
      await tester.pump(const Duration(milliseconds: 350));

      expect(api.queries, ['???']);
    });

    testWidgets('keyboard submit searches immediately, bypassing debounce', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      expect(api.queries, ['deploy']);

      // The debounce timer must have been cancelled: waiting past 350ms must
      // not enqueue a duplicate request.
      await tester.pump(const Duration(milliseconds: 400));
      expect(api.queries, ['deploy']);
    });

    testWidgets('shows an accessible loading state while searching', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      expect(find.byKey(const Key('search-loading')), findsOneWidget);
      final data = tester.getSemantics(find.byKey(const Key('search-loading')));
      expect(data.flagsCollection.isLiveRegion, isTrue);

      handle.dispose();
    });

    testWidgets(
      'explains zero results by name, mentioning the exact phrase and a shorter/fewer-words suggestion',
      (tester) async {
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();

        api.searchCalls.single.completer.complete(const []);
        await tester.pump();

        expect(find.byKey(const Key('search-empty')), findsOneWidget);
        expect(find.textContaining('exact phrase'), findsWidgets);
        expect(find.textContaining(RegExp('fewer|shorter')), findsOneWidget);
      },
    );

    testWidgets('shows a recoverable, announced error with Retry', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      api.searchCalls.single.completer.completeError(Exception('boom'));
      await tester.pump();

      expect(find.byKey(const Key('search-error')), findsOneWidget);
      final data = tester.getSemantics(find.byKey(const Key('search-error')));
      expect(data.flagsCollection.isLiveRegion, isTrue);
      expect(find.byKey(const Key('search-retry')), findsOneWidget);

      await tester.tap(find.byKey(const Key('search-retry')));
      await tester.pump();

      expect(api.queries, ['deploy', 'deploy']);
      api.searchCalls[1].completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          content: 'deploy staging',
          createdAt: DateTime.utc(2026, 8, 13, 12),
        ),
      ]);
      await tester.pump();

      expect(find.byKey(const Key('search-error')), findsNothing);
      expect(find.byKey(const ValueKey('hit-msg-1')), findsOneWidget);

      handle.dispose();
    });

    testWidgets('clearing input invalidates in-flight search', (tester) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      await tester.tap(find.byKey(const Key('search-clear-button')));
      await tester.pump();

      expect(find.byKey(const Key('search-initial')), findsOneWidget);

      api.searchCalls.single.completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          createdAt: DateTime.utc(2026, 8, 13),
        ),
      ]);
      await tester.pump();

      // The now-stale success must not resurrect results.
      expect(find.byKey(const Key('search-initial')), findsOneWidget);
      expect(find.byKey(const ValueKey('hit-msg-1')), findsNothing);
    });

    testWidgets('ignores a stale search success after a newer query', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'first');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      await tester.enterText(find.byKey(const Key('search-field')), 'second');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      expect(api.queries, ['first', 'second']);

      api.searchCalls[0].completer.complete([
        _hit(
          id: 'stale-hit',
          sessionId: 'session-stale',
          createdAt: DateTime.utc(2026, 8, 1),
        ),
      ]);
      await tester.pump();

      expect(find.byKey(const Key('search-loading')), findsOneWidget);
      expect(find.byKey(const ValueKey('hit-stale-hit')), findsNothing);

      api.searchCalls[1].completer.complete([
        _hit(
          id: 'fresh-hit',
          sessionId: 'session-fresh',
          createdAt: DateTime.utc(2026, 8, 2),
        ),
      ]);
      await tester.pump();

      expect(find.byKey(const ValueKey('hit-fresh-hit')), findsOneWidget);
      expect(find.byKey(const ValueKey('hit-stale-hit')), findsNothing);
    });

    testWidgets('ignores a stale search error after a newer query', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'first');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      await tester.enterText(find.byKey(const Key('search-field')), 'second');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      api.searchCalls[0].completer.completeError(Exception('stale boom'));
      await tester.pump();

      expect(find.byKey(const Key('search-error')), findsNothing);
      expect(find.byKey(const Key('search-loading')), findsOneWidget);

      api.searchCalls[1].completer.complete(const []);
      await tester.pump();

      expect(find.byKey(const Key('search-empty')), findsOneWidget);
    });

    testWidgets('groups hits by session and sorts by newest first', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      // Returned in relevance order, not chronological/grouped order.
      api.searchCalls.single.completer.complete([
        _hit(
          id: 'a-old',
          sessionId: 'session-a',
          createdAt: DateTime.utc(2026, 1, 1, 10),
        ),
        _hit(
          id: 'b-only',
          sessionId: 'session-b',
          createdAt: DateTime.utc(2026, 1, 2, 10),
        ),
        _hit(
          id: 'a-new',
          sessionId: 'session-a',
          createdAt: DateTime.utc(2026, 1, 3, 10),
        ),
      ]);
      await tester.pump();

      final groupAY = tester
          .getTopLeft(find.byKey(const ValueKey('group-header-session-a')))
          .dy;
      final groupBY = tester
          .getTopLeft(find.byKey(const ValueKey('group-header-session-b')))
          .dy;
      expect(groupAY, lessThan(groupBY));

      final newAY = tester
          .getTopLeft(find.byKey(const ValueKey('hit-a-new')))
          .dy;
      final oldAY = tester
          .getTopLeft(find.byKey(const ValueKey('hit-a-old')))
          .dy;
      expect(newAY, lessThan(oldAY));
    });

    testWidgets(
      'breaks same-session timestamp ties with the message sequence, not '
      'backend order',
      (tester) async {
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();

        // A user turn and the assistant reply it triggered are written
        // nanoseconds apart, but protobuf timestamps land on Dart DateTimes
        // at microsecond resolution, so both hits carry the same instant.
        // The backend returns them in BM25 order, which here is the reverse
        // of conversation order.
        final sameInstant = DateTime.utc(2026, 8, 13, 12);
        api.searchCalls.single.completer.complete([
          _hit(
            id: 'ask',
            sessionId: 'session-1',
            role: 'user',
            sequence: 1,
            createdAt: sameInstant,
          ),
          _hit(
            id: 'reply',
            sessionId: 'session-1',
            role: 'assistant',
            sequence: 2,
            createdAt: sameInstant,
          ),
          _hit(
            id: 'older',
            sessionId: 'session-1',
            role: 'assistant',
            sequence: 9,
            createdAt: sameInstant.subtract(const Duration(days: 1)),
          ),
        ]);
        await tester.pump();

        // Newest first: the tied pair orders by descending sequence, and the
        // genuinely older hit stays last despite its higher sequence.
        expect(_hitTop(tester, 'reply'), lessThan(_hitTop(tester, 'ask')));
        expect(_hitTop(tester, 'ask'), lessThan(_hitTop(tester, 'older')));
      },
    );

    testWidgets(
      'orders sessions deterministically when their newest hits share a '
      'timestamp',
      (tester) async {
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        final sameInstant = DateTime.utc(2026, 8, 13, 12);
        final bHit = _hit(
          id: 'b-hit',
          sessionId: 'session-b',
          sequence: 7,
          createdAt: sameInstant,
        );
        final aHit = _hit(
          id: 'a-hit',
          sessionId: 'session-a',
          sequence: 1,
          createdAt: sameInstant,
        );

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();
        api.searchCalls[0].completer.complete([bHit, aHit]);
        await tester.pump();

        expect(
          _groupTop(tester, 'session-a'),
          lessThan(_groupTop(tester, 'session-b')),
        );

        // The same result set delivered in the opposite backend order must
        // render identically: group order can't inherit relevance ranking.
        await tester.enterText(find.byKey(const Key('search-field')), 'again');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();
        api.searchCalls[1].completer.complete([aHit, bHit]);
        await tester.pump();

        expect(
          _groupTop(tester, 'session-a'),
          lessThan(_groupTop(tester, 'session-b')),
        );
      },
    );

    testWidgets(
      'renders identically stamped and sequenced hits in a stable order',
      (tester) async {
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        // Degenerate data: same instant *and* same sequence. Nothing about
        // the hits themselves says which is newer, so the only requirement
        // is that the rendered order is reproducible instead of tracking
        // whatever order the backend happened to rank them in.
        final sameInstant = DateTime.utc(2026, 8, 13, 12);
        final first = _hit(
          id: 'dup-1',
          sessionId: 'session-1',
          sequence: 4,
          createdAt: sameInstant,
        );
        final second = _hit(
          id: 'dup-2',
          sessionId: 'session-1',
          sequence: 4,
          createdAt: sameInstant,
        );

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();
        api.searchCalls[0].completer.complete([first, second]);
        await tester.pump();
        final orderedFirst = _hitTop(tester, 'dup-1') < _hitTop(tester, 'dup-2')
            ? 'dup-1'
            : 'dup-2';

        await tester.enterText(find.byKey(const Key('search-field')), 'again');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();
        api.searchCalls[1].completer.complete([second, first]);
        await tester.pump();

        expect(
          _hitTop(tester, orderedFirst),
          lessThan(
            _hitTop(tester, orderedFirst == 'dup-1' ? 'dup-2' : 'dup-1'),
          ),
        );
      },
    );

    testWidgets('announces a completed search that found nothing', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      api.searchCalls.single.completer.complete(const []);
      await tester.pump();

      final data = tester.getSemantics(find.byKey(const Key('search-empty')));
      expect(data.flagsCollection.isLiveRegion, isTrue);
      expect(data.label, contains('No results'));

      handle.dispose();
    });

    testWidgets('announces result and conversation counts, pluralized', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      api.searchCalls[0].completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          createdAt: DateTime.utc(2026, 8, 13, 9),
        ),
        _hit(
          id: 'msg-2',
          sessionId: 'session-1',
          createdAt: DateTime.utc(2026, 8, 13, 10),
        ),
      ]);
      await tester.pump();

      final status = tester.getSemantics(
        find.byKey(const Key('search-results-status')),
      );
      expect(status.flagsCollection.isLiveRegion, isTrue);
      expect(status.label, '2 results in 1 conversation');

      // A single hit spread across sessions flips both plurals the other way.
      await tester.enterText(find.byKey(const Key('search-field')), 'staging');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();
      api.searchCalls[1].completer.complete([
        _hit(
          id: 'msg-3',
          sessionId: 'session-2',
          createdAt: DateTime.utc(2026, 8, 12, 9),
        ),
        _hit(
          id: 'msg-4',
          sessionId: 'session-3',
          createdAt: DateTime.utc(2026, 8, 11, 9),
        ),
        _hit(
          id: 'msg-5',
          sessionId: 'session-3',
          createdAt: DateTime.utc(2026, 8, 10, 9),
        ),
      ]);
      await tester.pump();

      expect(
        tester
            .getSemantics(find.byKey(const Key('search-results-status')))
            .label,
        '3 results in 2 conversations',
      );

      handle.dispose();
    });

    testWidgets(
      'keeps focus put and the announcement stable when a title resolves',
      (tester) async {
        final handle = tester.ensureSemantics();
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();
        final focusWhileSearching = tester.binding.focusManager.primaryFocus;

        api.searchCalls.single.completer.complete([
          _hit(
            id: 'msg-1',
            sessionId: 'session-1',
            createdAt: DateTime.utc(2026, 8, 13),
          ),
        ]);
        await tester.pump();

        // Announcing results must not steal focus from the field the user is
        // still typing in.
        expect(
          tester.binding.focusManager.primaryFocus,
          same(focusWhileSearching),
        );
        final announced = tester
            .getSemantics(find.byKey(const Key('search-results-status')))
            .label;
        expect(announced, '1 result in 1 conversation');

        api.sessionCalls.single.completer.complete(
          Session(
            sessionId: 'session-1',
            title: 'Release work',
            updatedAt: DateTime.utc(2026, 8, 13),
          ),
        );
        await tester.pump();
        expect(find.text('Release work'), findsOneWidget);

        // Late title metadata changes headings only. The status label is
        // derived from counts alone, so it stays byte-identical — which is
        // what stops the platform from re-announcing this live region as a
        // freshly completed search. (Both engines gate live-region
        // announcements on the label actually changing; the announcement
        // itself is emitted engine-side and isn't observable from a widget
        // test, so the stable label is what this asserts.)
        final afterTitle = tester
            .getSemantics(find.byKey(const Key('search-results-status')))
            .label;
        expect(afterTitle, announced);
        expect(afterTitle, isNot(contains('Release work')));
        expect(
          tester.binding.focusManager.primaryFocus,
          same(focusWhileSearching),
        );

        handle.dispose();
      },
    );

    testWidgets(
      'still announces a new search that happens to match the previous '
      'counts',
      (tester) async {
        final handle = tester.ensureSemantics();
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();
        api.searchCalls[0].completer.complete([
          _hit(
            id: 'msg-1',
            sessionId: 'session-1',
            createdAt: DateTime.utc(2026, 8, 13, 9),
          ),
          _hit(
            id: 'msg-2',
            sessionId: 'session-1',
            createdAt: DateTime.utc(2026, 8, 13, 10),
          ),
        ]);
        await tester.pump();
        expect(
          tester
              .getSemantics(find.byKey(const Key('search-results-status')))
              .label,
          '2 results in 1 conversation',
        );

        await tester.enterText(
          find.byKey(const Key('search-field')),
          'staging',
        );
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();

        // A counts-only label repeats itself whenever two searches happen to
        // find the same shape of results. That still reaches the user
        // because the status region is torn down for the loading region
        // while the new search runs, so the repeated summary arrives as a
        // label change rather than an unchanged live region the platform
        // would suppress.
        expect(find.byKey(const Key('search-results-status')), findsNothing);
        expect(find.byKey(const Key('search-loading')), findsOneWidget);

        api.searchCalls[1].completer.complete([
          _hit(
            id: 'msg-3',
            sessionId: 'session-2',
            createdAt: DateTime.utc(2026, 8, 12, 9),
          ),
          _hit(
            id: 'msg-4',
            sessionId: 'session-2',
            createdAt: DateTime.utc(2026, 8, 12, 10),
          ),
        ]);
        await tester.pump();

        final status = tester.getSemantics(
          find.byKey(const Key('search-results-status')),
        );
        expect(status.flagsCollection.isLiveRegion, isTrue);
        expect(status.label, '2 results in 1 conversation');

        handle.dispose();
      },
    );

    testWidgets('formats hit dates as an absolute local date with a year', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      final opened = await _pumpScreen(tester, api);
      addTearDown(() => opened.clear());

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      final createdAt = DateTime.utc(2026, 8, 13, 12);
      api.searchCalls.single.completer.complete([
        _hit(id: 'msg-1', sessionId: 'session-1', createdAt: createdAt),
      ]);
      await tester.pump();

      // Expectation is derived from the raw local DateTime fields, not from
      // the same formatter the screen uses, so a formatter that silently
      // drops the year (or renders the UTC day) fails here.
      final local = createdAt.toLocal();
      final expected =
          '${_shortMonths[local.month - 1]} ${local.day}, ${local.year}';
      expect(_hitSubtitle(tester, 'msg-1'), contains(expected));
    });

    testWidgets('distinguishes same-day hits from different years', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      api.searchCalls.single.completer.complete([
        _hit(
          id: 'msg-2026',
          sessionId: 'session-1',
          createdAt: DateTime.utc(2026, 8, 13, 12),
        ),
        _hit(
          id: 'msg-2024',
          sessionId: 'session-1',
          createdAt: DateTime.utc(2024, 8, 13, 12),
        ),
      ]);
      await tester.pump();

      final visible2026 = _hitSubtitle(tester, 'msg-2026');
      final visible2024 = _hitSubtitle(tester, 'msg-2024');
      expect(visible2026, contains('2026'));
      expect(visible2024, contains('2024'));
      expect(visible2026, isNot(visible2024));

      final semantics2026 = tester
          .getSemantics(find.byKey(const ValueKey('hit-msg-2026')))
          .label;
      final semantics2024 = tester
          .getSemantics(find.byKey(const ValueKey('hit-msg-2024')))
          .label;
      expect(semantics2026, contains('2026'));
      expect(semantics2024, contains('2024'));
      expect(semantics2026, isNot(semantics2024));

      handle.dispose();
    });

    testWidgets('renders hits immediately, before title metadata resolves', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      api.searchCalls.single.completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          createdAt: DateTime.utc(2026, 8, 13),
        ),
      ]);
      await tester.pump();

      expect(find.byKey(const ValueKey('hit-msg-1')), findsOneWidget);
      expect(find.text('Session session-1'), findsOneWidget);
    });

    testWidgets('looks up each distinct session ID only once per search', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      api.searchCalls.single.completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          createdAt: DateTime.utc(2026, 8, 13, 9),
        ),
        _hit(
          id: 'msg-2',
          sessionId: 'session-1',
          createdAt: DateTime.utc(2026, 8, 13, 10),
        ),
      ]);
      await tester.pump();

      expect(api.sessionRequests, ['session-1']);
    });

    testWidgets('replaces the fallback heading once the title resolves', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      api.searchCalls.single.completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          createdAt: DateTime.utc(2026, 8, 13),
        ),
      ]);
      await tester.pump();
      expect(find.text('Session session-1'), findsOneWidget);

      api.sessionCalls.single.completer.complete(
        Session(
          sessionId: 'session-1',
          title: 'Release work',
          updatedAt: DateTime.utc(2026, 8, 13),
        ),
      );
      await tester.pump();

      expect(find.text('Release work'), findsOneWidget);
      expect(find.text('Session session-1'), findsNothing);
    });

    testWidgets('falls back to the same placeholder the sidebar uses', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      api.searchCalls.single.completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          createdAt: DateTime.utc(2026, 8, 13),
        ),
      ]);
      await tester.pump();

      api.sessionCalls.single.completer.complete(
        Session(
          sessionId: 'session-1',
          title: '',
          updatedAt: DateTime.utc(2026, 8, 13),
        ),
      );
      await tester.pump();

      // Must match the sidebar exactly: one session showing two different
      // names in two places reads as two sessions.
      expect(find.text('New chat'), findsOneWidget);
      expect(find.text('Untitled chat'), findsNothing);
    });

    testWidgets('keeps the ID fallback when title lookup fails', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      api.searchCalls.single.completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          createdAt: DateTime.utc(2026, 8, 13),
        ),
      ]);
      await tester.pump();

      api.sessionCalls.single.completer.completeError(Exception('nope'));
      await tester.pump();

      expect(find.text('Session session-1'), findsOneWidget);
    });

    testWidgets('caps concurrent title lookups at four', (tester) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      final ids = List.generate(6, (i) => 'session-$i');
      api.searchCalls.single.completer.complete([
        for (final id in ids)
          _hit(
            id: 'msg-$id',
            sessionId: id,
            createdAt: DateTime.utc(2026, 8, 13),
          ),
      ]);
      await tester.pump();

      expect(api.activeSessionRequests, 4);
      expect(api.sessionCalls.length, 4);

      // Resolving one in-flight lookup frees a worker slot for the fifth ID.
      api.sessionCalls[0].completer.complete(
        Session(
          sessionId: api.sessionCalls[0].sessionId,
          title: 'T0',
          updatedAt: DateTime.utc(2026, 8, 13),
        ),
      );
      await tester.pump();
      expect(api.sessionCalls.length, 5);

      for (var i = 1; i < api.sessionCalls.length; i++) {
        api.sessionCalls[i].completer.complete(
          Session(
            sessionId: api.sessionCalls[i].sessionId,
            title: 'T$i',
            updatedAt: DateTime.utc(2026, 8, 13),
          ),
        );
        await tester.pump();
      }

      expect(api.sessionCalls.length, 6);
      expect(api.maxActiveSessionRequests, lessThanOrEqualTo(4));
    });

    testWidgets(
      'reuses one in-flight title lookup when a newer query hits the same '
      'session',
      (tester) async {
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();

        api.searchCalls[0].completer.complete([
          _hit(
            id: 'msg-1',
            sessionId: 'session-1',
            createdAt: DateTime.utc(2026, 8, 13),
          ),
        ]);
        await tester.pump();
        expect(api.sessionCalls.length, 1);

        // A newer generation matching the same session must ride the
        // already-in-flight lookup: a session's title is session-specific,
        // not query-specific, so re-requesting it is pure duplicate work.
        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();

        api.searchCalls[1].completer.complete([
          _hit(
            id: 'msg-1',
            sessionId: 'session-1',
            createdAt: DateTime.utc(2026, 8, 13),
          ),
        ]);
        await tester.pump();
        expect(api.sessionRequests, ['session-1']);
        expect(api.sessionCalls.length, 1);

        // The shared lookup was started by the older generation, but its
        // result is still the right title for the current query's group.
        api.sessionCalls[0].completer.complete(
          Session(
            sessionId: 'session-1',
            title: 'Release work',
            updatedAt: DateTime.utc(2026, 8, 13),
          ),
        );
        await tester.pump();
        expect(find.text('Release work'), findsOneWidget);
        expect(find.text('Session session-1'), findsNothing);

        // A later, unrelated query reuses the cached title with no new call.
        await tester.enterText(
          find.byKey(const Key('search-field')),
          'staging',
        );
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();

        api.searchCalls[2].completer.complete([
          _hit(
            id: 'msg-2',
            sessionId: 'session-1',
            createdAt: DateTime.utc(2026, 8, 14),
          ),
        ]);
        await tester.pump();
        expect(api.sessionRequests, ['session-1']);
        expect(find.text('Release work'), findsOneWidget);
      },
    );

    testWidgets('tapping a result opens the exact session ID', (tester) async {
      final api = _FakeSearchApi();
      final opened = await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      api.searchCalls.single.completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-42',
          createdAt: DateTime.utc(2026, 8, 13),
        ),
      ]);
      await tester.pump();

      await tester.tap(find.byKey(const ValueKey('hit-msg-1')));
      await tester.pump();

      expect(opened, ['session-42']);
    });

    testWidgets('exposes group header and row semantics', (tester) async {
      final handle = tester.ensureSemantics();
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      final createdAt = DateTime.utc(2026, 8, 13, 12);
      api.searchCalls.single.completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          role: 'user',
          content: 'deploy staging',
          createdAt: createdAt,
        ),
      ]);
      await tester.pump();

      final headerData = tester.getSemantics(
        find.byKey(const ValueKey('group-header-session-1')),
      );
      expect(headerData.flagsCollection.isHeader, isTrue);

      final rowData = tester.getSemantics(
        find.byKey(const ValueKey('hit-msg-1')),
      );
      expect(rowData.label, contains('user'));
      expect(rowData.label, contains('deploy staging'));

      handle.dispose();
    });

    testWidgets('sends a limit of 50 to searchMessages', (tester) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      expect(api.limits, [50]);
    });

    testWidgets('reuses a cached title across a later, unrelated query', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'first');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      api.searchCalls[0].completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          createdAt: DateTime.utc(2026, 8, 13),
        ),
      ]);
      await tester.pump();

      api.sessionCalls[0].completer.complete(
        Session(
          sessionId: 'session-1',
          title: 'Release work',
          updatedAt: DateTime.utc(2026, 8, 13),
        ),
      );
      await tester.pump();
      expect(find.text('Release work'), findsOneWidget);
      expect(api.sessionRequests, ['session-1']);

      await tester.enterText(find.byKey(const Key('search-field')), 'second');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      api.searchCalls[1].completer.complete([
        _hit(
          id: 'msg-2',
          sessionId: 'session-1',
          createdAt: DateTime.utc(2026, 8, 14),
        ),
      ]);
      await tester.pump();

      // The title was already cached: no second lookup is issued, and the
      // cached title renders immediately.
      expect(api.sessionRequests, ['session-1']);
      expect(find.text('Release work'), findsOneWidget);
    });

    testWidgets('Retry reruns a title lookup that previously failed', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      api.searchCalls[0].completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          createdAt: DateTime.utc(2026, 8, 13),
        ),
      ]);
      await tester.pump();
      api.sessionCalls[0].completer.completeError(Exception('nope'));
      await tester.pump();
      expect(find.text('Session session-1'), findsOneWidget);

      // Force the error UI so Retry is available, then retry the same
      // query: the still-uncached title from the failed lookup above must
      // be looked up again.
      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();
      api.searchCalls[1].completer.completeError(Exception('search boom'));
      await tester.pump();
      expect(find.byKey(const Key('search-retry')), findsOneWidget);

      await tester.tap(find.byKey(const Key('search-retry')));
      await tester.pump();

      api.searchCalls[2].completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          createdAt: DateTime.utc(2026, 8, 13),
        ),
      ]);
      await tester.pump();

      expect(api.sessionRequests.where((id) => id == 'session-1').length, 2);

      api.sessionCalls[1].completer.complete(
        Session(
          sessionId: 'session-1',
          title: 'Resolved on retry',
          updatedAt: DateTime.utc(2026, 8, 13),
        ),
      );
      await tester.pump();
      expect(find.text('Resolved on retry'), findsOneWidget);
    });

    testWidgets(
      'caps concurrent title lookups at four across overlapping generations',
      (tester) async {
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'first');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();

        final firstIds = List.generate(4, (i) => 'gen1-session-$i');
        api.searchCalls[0].completer.complete([
          for (final id in firstIds)
            _hit(
              id: 'msg-$id',
              sessionId: id,
              createdAt: DateTime.utc(2026, 8, 13),
            ),
        ]);
        await tester.pump();

        expect(api.sessionCalls.length, 4);
        expect(api.activeSessionRequests, 4);

        // A newer query starts before any of the first generation's title
        // lookups resolve. Its own session IDs must queue behind the
        // global cap instead of pushing concurrency past four.
        await tester.enterText(find.byKey(const Key('search-field')), 'second');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();

        final secondIds = List.generate(4, (i) => 'gen2-session-$i');
        api.searchCalls[1].completer.complete([
          for (final id in secondIds)
            _hit(
              id: 'msg-$id',
              sessionId: id,
              createdAt: DateTime.utc(2026, 8, 14),
            ),
        ]);
        await tester.pump();

        // Still only the first generation's four lookups have started; the
        // second generation's four IDs are queued behind the cap.
        expect(api.sessionCalls.length, 4);
        expect(api.maxActiveSessionRequests, lessThanOrEqualTo(4));

        // Resolving the stale (first-generation) lookups frees slots for
        // the current (second-generation) lookups, one at a time.
        for (var i = 0; i < 4; i++) {
          api.sessionCalls[i].completer.complete(
            Session(
              sessionId: firstIds[i],
              title: 'Stale $i',
              updatedAt: DateTime.utc(2026, 8, 13),
            ),
          );
          await tester.pump();
        }

        expect(api.sessionCalls.length, 8);
        expect(api.maxActiveSessionRequests, lessThanOrEqualTo(4));

        // The stale titles were never applied (their generation is gone),
        // and the second generation's lookups aren't starved: they do
        // resolve once slots free up.
        for (var i = 4; i < 8; i++) {
          final sessionId = api.sessionCalls[i].sessionId;
          api.sessionCalls[i].completer.complete(
            Session(
              sessionId: sessionId,
              title: 'Fresh $sessionId',
              updatedAt: DateTime.utc(2026, 8, 14),
            ),
          );
          await tester.pump();
          expect(find.text('Fresh $sessionId'), findsOneWidget);
        }

        for (var i = 0; i < 4; i++) {
          expect(find.text('Stale $i'), findsNothing);
        }
      },
    );

    // Replaces an earlier "stale queued waiters are skipped" test that could
    // not actually fail: `pump()` drains pending microtasks, so a plain FIFO
    // hand-off converged to the same end state as the priority hand-off it
    // was meant to guard. This one is behavioral: without a screen-lifetime
    // cache plus in-flight dedupe keyed by session ID, overlapping
    // generations reissue `getSession` for sessions they share, and titles
    // resolved by the older generation never reach the current query.
    testWidgets(
      'dedupes overlapping generations onto one lookup per session without '
      'starving newly matched sessions',
      (tester) async {
        tester.view.physicalSize = const Size(1200, 3000);
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.reset);

        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        // Generation A matches five sessions: four lookups take the global
        // permits and the fifth queues behind them.
        await tester.enterText(find.byKey(const Key('search-field')), 'first');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();

        final sharedIds = List.generate(5, (i) => 'session-$i');
        api.searchCalls[0].completer.complete([
          for (final id in sharedIds)
            _hit(
              id: 'msg-$id',
              sessionId: id,
              createdAt: DateTime.utc(2026, 8, 13),
            ),
        ]);
        await tester.pump();
        expect(api.sessionCalls.length, 4);

        // Generation B refines the query before anything resolves, matching
        // the same five sessions plus one the older generation never saw.
        await tester.enterText(
          find.byKey(const Key('search-field')),
          'first refined',
        );
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();

        final allIds = [...sharedIds, 'session-5'];
        api.searchCalls[1].completer.complete([
          for (final id in allIds)
            _hit(
              id: 'msg-$id',
              sessionId: id,
              createdAt: DateTime.utc(2026, 8, 14),
            ),
        ]);
        await tester.pump();

        // The shared sessions ride the existing lookups instead of being
        // requested a second time.
        expect(api.sessionCalls.length, 4);
        expect(
          api.sessionRequests.length,
          api.sessionRequests.toSet().length,
          reason: 'no session ID may be requested twice',
        );

        // A lookup started by the older generation still titles the current
        // query's identical session.
        final firstId = api.sessionCalls[0].sessionId;
        api.sessionCalls[0].completer.complete(
          Session(
            sessionId: firstId,
            title: 'Title $firstId',
            updatedAt: DateTime.utc(2026, 8, 13),
          ),
        );
        await tester.pump();
        expect(find.text('Title $firstId'), findsOneWidget);
        expect(find.text('Session $firstId'), findsNothing);

        // Drain the rest. Freed permits pick up the queued sessions,
        // including the one only the newer generation matched.
        for (var i = 1; i < api.sessionCalls.length; i++) {
          final sessionId = api.sessionCalls[i].sessionId;
          api.sessionCalls[i].completer.complete(
            Session(
              sessionId: sessionId,
              title: 'Title $sessionId',
              updatedAt: DateTime.utc(2026, 8, 14),
            ),
          );
          await tester.pump();
        }

        expect(api.sessionRequests, hasLength(allIds.length));
        expect(api.sessionRequests.toSet(), allIds.toSet());
        expect(api.maxActiveSessionRequests, lessThanOrEqualTo(4));
        for (final id in allIds) {
          expect(find.text('Title $id'), findsOneWidget);
          expect(find.text('Session $id'), findsNothing);
        }
      },
    );

    testWidgets('skips a queued title lookup once the screen is disposed', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      // Six sessions: four lookups take the permits, two queue.
      api.searchCalls.single.completer.complete([
        for (var i = 0; i < 6; i++)
          _hit(
            id: 'msg-session-$i',
            sessionId: 'session-$i',
            createdAt: DateTime.utc(2026, 8, 13),
          ),
      ]);
      await tester.pump();
      expect(api.sessionCalls.length, 4);

      await tester.pumpWidget(const SizedBox());

      // Freeing permits after disposal must not send the queued lookups:
      // nothing is left to render their titles.
      for (var i = 0; i < 4; i++) {
        api.sessionCalls[i].completer.complete(
          Session(
            sessionId: api.sessionCalls[i].sessionId,
            title: 'Late title $i',
            updatedAt: DateTime.utc(2026, 8, 13),
          ),
        );
        await tester.pump();
      }

      expect(api.sessionCalls.length, 4);
      expect(api.activeSessionRequests, 0);
      expect(tester.takeException(), isNull);
    });

    testWidgets(
      'clears stale loading immediately when input changes during the debounce window',
      (tester) async {
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();
        expect(find.byKey(const Key('search-loading')), findsOneWidget);

        // Typing again while the first search is still in flight must
        // clear the stale loading indicator immediately, before the new
        // 350ms debounce elapses.
        await tester.enterText(
          find.byKey(const Key('search-field')),
          'deploy2',
        );
        await tester.pump();

        expect(find.byKey(const Key('search-loading')), findsNothing);
        expect(find.byKey(const Key('search-initial')), findsNothing);

        await tester.pump(const Duration(milliseconds: 350));
        expect(api.queries, ['deploy', 'deploy2']);
        expect(find.byKey(const Key('search-loading')), findsOneWidget);
      },
    );

    testWidgets(
      'clears a stale error immediately when input changes during the debounce window',
      (tester) async {
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();
        api.searchCalls.single.completer.completeError(Exception('boom'));
        await tester.pump();
        expect(find.byKey(const Key('search-error')), findsOneWidget);

        await tester.enterText(
          find.byKey(const Key('search-field')),
          'deploy2',
        );
        await tester.pump();

        expect(find.byKey(const Key('search-error')), findsNothing);
        expect(find.byKey(const Key('search-retry')), findsNothing);
      },
    );

    testWidgets(
      'does not show the no-results copy while a new query is still '
      'debouncing after a prior error, since no search has completed for it',
      (tester) async {
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();
        api.searchCalls.single.completer.completeError(Exception('boom'));
        await tester.pump();
        expect(find.byKey(const Key('search-error')), findsOneWidget);

        await tester.enterText(
          find.byKey(const Key('search-field')),
          'deploy2',
        );
        await tester.pump();

        // The error and its Retry action are gone (already covered above),
        // but no successful search has ever completed for "deploy2" yet: it
        // must not be mistaken for a completed, zero-hit search.
        expect(find.byKey(const Key('search-error')), findsNothing);
        expect(find.byKey(const Key('search-retry')), findsNothing);
        expect(find.byKey(const Key('search-empty')), findsNothing);
        expect(
          find.text(
            'No messages match this exact phrase. Try fewer or shorter '
            'words.',
          ),
          findsNothing,
        );

        // Once the debounced search actually completes with zero hits, the
        // no-results copy is legitimate and must appear.
        await tester.pump(const Duration(milliseconds: 350));
        expect(api.queries, ['deploy', 'deploy2']);
        api.searchCalls[1].completer.complete(const []);
        await tester.pump();
        expect(find.byKey(const Key('search-empty')), findsOneWidget);
      },
    );

    testWidgets(
      'ignores a stale debounced search success after newer typed input',
      (tester) async {
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'first');
        await tester.pump(const Duration(milliseconds: 350));
        expect(api.queries, ['first']);

        await tester.enterText(find.byKey(const Key('search-field')), 'second');
        await tester.pump(const Duration(milliseconds: 350));
        expect(api.queries, ['first', 'second']);

        api.searchCalls[0].completer.complete([
          _hit(
            id: 'stale-hit',
            sessionId: 'session-stale',
            createdAt: DateTime.utc(2026, 8, 1),
          ),
        ]);
        await tester.pump();
        expect(find.byKey(const ValueKey('hit-stale-hit')), findsNothing);
        expect(find.byKey(const Key('search-loading')), findsOneWidget);

        api.searchCalls[1].completer.complete([
          _hit(
            id: 'fresh-hit',
            sessionId: 'session-fresh',
            createdAt: DateTime.utc(2026, 8, 2),
          ),
        ]);
        await tester.pump();
        expect(find.byKey(const ValueKey('hit-fresh-hit')), findsOneWidget);
        expect(find.byKey(const ValueKey('hit-stale-hit')), findsNothing);
      },
    );

    testWidgets(
      'ignores a Retry search completion once superseded by new input',
      (tester) async {
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();
        api.searchCalls[0].completer.completeError(Exception('boom'));
        await tester.pump();
        expect(find.byKey(const Key('search-retry')), findsOneWidget);

        await tester.tap(find.byKey(const Key('search-retry')));
        await tester.pump();
        expect(api.searchCalls.length, 2);
        expect(find.byKey(const Key('search-loading')), findsOneWidget);

        // Before the retried search resolves, the user submits a different
        // query. The retry's eventual completion must not resurrect stale
        // state for a query the user has already moved past.
        await tester.enterText(
          find.byKey(const Key('search-field')),
          'staging',
        );
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();
        expect(api.searchCalls.length, 3);

        api.searchCalls[1].completer.complete([
          _hit(
            id: 'retry-hit',
            sessionId: 'session-retry',
            createdAt: DateTime.utc(2026, 8, 1),
          ),
        ]);
        await tester.pump();
        expect(find.byKey(const ValueKey('hit-retry-hit')), findsNothing);
        expect(find.byKey(const Key('search-loading')), findsOneWidget);

        api.searchCalls[2].completer.complete([
          _hit(
            id: 'fresh-hit',
            sessionId: 'session-fresh',
            createdAt: DateTime.utc(2026, 8, 2),
          ),
        ]);
        await tester.pump();
        expect(find.byKey(const ValueKey('hit-fresh-hit')), findsOneWidget);
        expect(find.byKey(const ValueKey('hit-retry-hit')), findsNothing);
      },
    );

    testWidgets('exposes semantics for the search field and the Retry action', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      final fieldFinder = find.bySemanticsLabel(RegExp('Search conversations'));
      expect(fieldFinder, findsWidgets);
      final fieldData = tester.getSemantics(fieldFinder.first);
      expect(fieldData.flagsCollection.isTextField, isTrue);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();
      api.searchCalls.single.completer.completeError(Exception('boom'));
      await tester.pump();

      final retryData = tester.getSemantics(
        find.byKey(const Key('search-retry')),
      );
      expect(retryData.flagsCollection.isButton, isTrue);
      expect(retryData.label, contains('Retry'));

      handle.dispose();
    });

    testWidgets(
      'disposing with a pending debounce and in-flight operations causes no errors',
      (tester) async {
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        // Kick off an in-flight search.
        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();
        expect(api.searchCalls.length, 1);

        api.searchCalls[0].completer.complete([
          _hit(
            id: 'msg-1',
            sessionId: 'session-1',
            createdAt: DateTime.utc(2026, 8, 13),
          ),
        ]);
        await tester.pump();
        // An in-flight title lookup is now outstanding too.
        expect(api.sessionCalls.length, 1);

        // Type again without waiting for the debounce to fire, leaving a
        // pending timer alongside the in-flight title lookup above.
        await tester.enterText(
          find.byKey(const Key('search-field')),
          'deploy2',
        );

        // Dispose the widget while both are outstanding.
        await tester.pumpWidget(const SizedBox());

        // Let the debounce window elapse and complete the outstanding
        // operations; none of this should throw or try to update disposed
        // state.
        await tester.pump(const Duration(milliseconds: 400));
        api.sessionCalls[0].completer.complete(
          Session(
            sessionId: 'session-1',
            title: 'Late title',
            updatedAt: DateTime.utc(2026, 8, 13),
          ),
        );
        await tester.pump();

        expect(tester.takeException(), isNull);
      },
    );

    testWidgets(
      'announces the loading state once, not once per duplicate node',
      (tester) async {
        final handle = tester.ensureSemantics();
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();

        final loading = tester.getSemantics(
          find.byKey(const Key('search-loading')),
        );
        expect(loading.flagsCollection.isLiveRegion, isTrue);
        expect(loading.label, 'Searching conversations');

        // The visible "Searching..." caption says exactly what the live
        // region already announces. Left as a node of its own it is
        // traversed — and spoken — a second time, so the user hears the same
        // state twice.
        expect(
          _spokenLabels(
            tester,
          ).where((label) => label.contains('Searching')).toList(),
          ['Searching conversations'],
          reason: 'the loading state must reach a screen reader exactly once',
        );

        handle.dispose();
      },
    );

    testWidgets(
      'announces a failure once while keeping Retry independently actionable',
      (tester) async {
        final handle = tester.ensureSemantics();
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();
        api.searchCalls.single.completer.completeError(Exception('boom'));
        await tester.pump();

        const message = 'Search failed: Exception: boom';
        final error = tester.getSemantics(
          find.byKey(const Key('search-error')),
        );
        expect(error.flagsCollection.isLiveRegion, isTrue);
        expect(error.label, message);
        expect(
          _spokenLabels(
            tester,
          ).where((label) => label.contains('Search failed')).toList(),
          [message],
          reason: 'the failure must reach a screen reader exactly once',
        );

        // Silencing the duplicate copy must not silence the recovery action
        // along with it: Retry stays a node of its own, outside the live
        // region's label, and stays a button assistive technology can
        // activate.
        final retry = tester.getSemantics(
          find.byKey(const Key('search-retry')),
        );
        expect(retry.id, isNot(error.id));
        final retryData = retry.getSemanticsData();
        expect(retryData.flagsCollection.isButton, isTrue);
        expect(retryData.label, 'Retry');
        expect(retryData.hasAction(SemanticsAction.tap), isTrue);
        expect(_spokenLabels(tester), contains('Retry'));

        // Driving it the way a screen reader does — the semantics action,
        // not a synthesized pointer tap — still reruns the search.
        retry.owner!.performAction(retry.id, SemanticsAction.tap);
        await tester.pump();
        expect(api.queries, ['deploy', 'deploy']);

        handle.dispose();
      },
    );

    testWidgets(
      'announces a repeated search whose results land before any loading '
      'frame renders',
      (tester) async {
        final handle = tester.ensureSemantics();
        final api = _ImmediateSearchApi([
          [
            _hit(
              id: 'msg-1',
              sessionId: 'session-1',
              createdAt: DateTime.utc(2026, 8, 13),
            ),
          ],
          [
            _hit(
              id: 'msg-2',
              sessionId: 'session-1',
              createdAt: DateTime.utc(2026, 8, 13),
            ),
          ],
        ]);
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();

        // The API answered within the frame the search started in, so the
        // loading live region never rendered and never tore the status
        // region down in between the two searches.
        expect(find.byKey(const Key('search-loading')), findsNothing);
        final first = tester.getSemantics(
          find.byKey(const Key('search-results-status')),
        );
        expect(first.label, '1 result in 1 conversation');
        final firstId = first.id;
        final focusAfterFirst = tester.binding.focusManager.primaryFocus;

        await _submitAgain(tester);
        await tester.pump();
        expect(api.queries, ['deploy', 'deploy']);
        expect(find.byKey(const Key('search-loading')), findsNothing);
        expect(find.byKey(const ValueKey('hit-msg-2')), findsOneWidget);

        final second = tester.getSemantics(
          find.byKey(const Key('search-results-status')),
        );
        // A counts-only summary repeats itself, so the label alone cannot
        // carry this announcement...
        expect(second.label, '1 result in 1 conversation');
        // ...and a live region the platform has already seen is only spoken
        // again when the node itself is one it has not seen before.
        expect(
          second.id,
          isNot(firstId),
          reason: 'each completed search must produce a fresh live region',
        );
        final secondId = second.id;

        // Rebuilding that region from scratch must not drag focus along with
        // it, the way announcing results must not steal focus in the first
        // place.
        expect(tester.binding.focusManager.primaryFocus, same(focusAfterFirst));

        // Late title metadata is not a new result: it must reuse that node,
        // so nothing gets announced a second time for the same search.
        api.sessionCalls.single.completer.complete(
          Session(
            sessionId: 'session-1',
            title: 'Release work',
            updatedAt: DateTime.utc(2026, 8, 13),
          ),
        );
        await tester.pump();
        expect(find.text('Release work'), findsOneWidget);
        final afterTitle = tester.getSemantics(
          find.byKey(const Key('search-results-status')),
        );
        expect(afterTitle.id, secondId);
        expect(afterTitle.label, '1 result in 1 conversation');

        handle.dispose();
      },
    );

    testWidgets(
      'announces a repeated empty search that lands before any loading '
      'frame renders',
      (tester) async {
        final handle = tester.ensureSemantics();
        final api = _ImmediateSearchApi([const [], const []]);
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();

        expect(find.byKey(const Key('search-loading')), findsNothing);
        final first = tester.getSemantics(
          find.byKey(const Key('search-empty')),
        );
        expect(first.flagsCollection.isLiveRegion, isTrue);
        expect(first.label, contains('No results'));
        final firstLabel = first.label;
        final firstId = first.id;

        await _submitAgain(tester);
        await tester.pump();
        expect(api.queries, ['deploy', 'deploy']);
        expect(find.byKey(const Key('search-loading')), findsNothing);

        final second = tester.getSemantics(
          find.byKey(const Key('search-empty')),
        );
        // The zero-results copy is fixed, so a second empty search says the
        // same words; only a new node can make them heard again.
        expect(second.label, firstLabel);
        expect(
          second.id,
          isNot(firstId),
          reason:
              'each completed empty search must produce a fresh live '
              'region',
        );

        handle.dispose();
      },
    );

    testWidgets(
      'stays silent about surviving results when a keystroke interrupts an '
      'in-flight search',
      (tester) async {
        final handle = tester.ensureSemantics();
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        // A search completes and announces its counts.
        await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();
        api.searchCalls.single.completer.complete([
          _hit(
            id: 'msg-1',
            sessionId: 'session-1',
            createdAt: DateTime.utc(2026, 8, 13, 12),
          ),
          _hit(
            id: 'msg-2',
            sessionId: 'session-1',
            sequence: 2,
            createdAt: DateTime.utc(2026, 8, 13, 11),
          ),
        ]);
        await tester.pump();
        final completed = tester.getSemantics(
          find.byKey(const Key('search-results-status')),
        );
        expect(completed.flagsCollection.isLiveRegion, isTrue);
        expect(completed.label, '2 results in 1 conversation');
        final completedId = completed.id;

        // A newer query reaches its request: loading replaces the results and
        // tears their live region down.
        await tester.enterText(
          find.byKey(const Key('search-field')),
          'deploy stag',
        );
        await tester.pump(const Duration(milliseconds: 350));
        expect(api.queries, ['deploy', 'deploy stag']);
        expect(find.byKey(const Key('search-loading')), findsOneWidget);
        expect(find.byKey(const Key('search-results-status')), findsNothing);

        // The user types again before that search answers. Loading is dropped
        // for a query nothing has run for yet, so the previous query's
        // results — and their summary — come back on screen.
        await tester.enterText(
          find.byKey(const Key('search-field')),
          'deploy stagi',
        );
        await tester.pump();
        expect(find.byKey(const Key('search-loading')), findsNothing);
        expect(find.byKey(const ValueKey('hit-msg-1')), findsOneWidget);

        final resurrected = tester.getSemantics(
          find.byKey(const Key('search-results-status')),
        );
        final resurrectedId = resurrected.id;
        expect(resurrected.label, '2 results in 1 conversation');
        // The loading frame destroyed the earlier node, so this is a node the
        // platform has never seen: as a live region it would be spoken, and
        // it would report the old query's counts as though a search had just
        // finished. No search has completed for what is now typed, so it must
        // not announce.
        expect(
          resurrectedId,
          isNot(completedId),
          reason:
              'the summary node was rebuilt from scratch, so only its '
              'live-region flag can keep it quiet',
        );
        expect(
          resurrected.flagsCollection.isLiveRegion,
          isFalse,
          reason:
              'surviving results must not be announced when no search has '
              'completed for the current query',
        );

        // The next real completion is still a completion: even with an
        // identical summary it has to mint a live region and be heard.
        await tester.pump(const Duration(milliseconds: 350));
        expect(api.queries, ['deploy', 'deploy stag', 'deploy stagi']);
        api.searchCalls[2].completer.complete([
          _hit(
            id: 'msg-3',
            sessionId: 'session-2',
            createdAt: DateTime.utc(2026, 8, 14, 12),
          ),
          _hit(
            id: 'msg-4',
            sessionId: 'session-2',
            sequence: 2,
            createdAt: DateTime.utc(2026, 8, 14, 11),
          ),
        ]);
        await tester.pump();
        expect(find.byKey(const ValueKey('hit-msg-3')), findsOneWidget);
        expect(find.byKey(const ValueKey('hit-msg-1')), findsNothing);

        final announced = tester.getSemantics(
          find.byKey(const Key('search-results-status')),
        );
        expect(announced.label, '2 results in 1 conversation');
        expect(announced.flagsCollection.isLiveRegion, isTrue);
        expect(
          announced.id,
          isNot(resurrectedId),
          reason: 'a real completion must still produce a fresh live region',
        );

        handle.dispose();
      },
    );

    testWidgets('bounds the row an over-long message body renders into', (
      tester,
    ) async {
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(
        find.byKey(const Key('search-field')),
        'deploy staging',
      );
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      api.searchCalls.single.completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          content: _longBody,
          createdAt: DateTime.utc(2026, 8, 13, 12),
        ),
      ]);
      await tester.pump();

      // A single pasted file or transcript must not push every other hit off
      // the list.
      expect(
        tester.getSize(find.byKey(const ValueKey('hit-msg-1'))).height,
        lessThan(200),
      );

      final body = tester.widget<Text>(
        find.descendant(
          of: find.byKey(const ValueKey('hit-msg-1')),
          matching: find.textContaining('deploy staging'),
        ),
      );
      expect(body.maxLines, isNotNull);
      expect(body.maxLines, lessThanOrEqualTo(3));
      expect(body.overflow, TextOverflow.ellipsis);
      expect(body.data, startsWith('deploy staging'));
      expect(
        body.data!.runes.length,
        lessThan(_longBody.runes.length),
        reason: 'the row must not lay out the whole body just to clip it',
      );
    });

    testWidgets('announces a bounded excerpt of an over-long message body', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(
        find.byKey(const Key('search-field')),
        'deploy staging',
      );
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      api.searchCalls.single.completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          content: _longBody,
          createdAt: DateTime.utc(2026, 8, 13, 12),
        ),
      ]);
      await tester.pump();

      final label = tester
          .getSemantics(find.byKey(const ValueKey('hit-msg-1')))
          .label;
      expect(label, contains('user message from '));
      expect(
        label,
        contains('deploy staging'),
        reason: 'the excerpt must still identify the matched phrase',
      );
      expect(label, isNot(contains(_longBody)));
      expect(
        label.runes.length,
        lessThan(300),
        reason:
            'a row announcement must stay skimmable, not read a whole '
            'transcript',
      );
      expect(label, endsWith('…'));

      handle.dispose();
    });

    testWidgets('cuts an over-long body on rune boundaries, not code units', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'rocket');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      // Every character here is a surrogate pair, so a cut made on code
      // units lands mid-pair and leaves a lone, unrenderable half behind.
      final body = '🚀' * 1000;
      api.searchCalls.single.completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          content: body,
          createdAt: DateTime.utc(2026, 8, 13, 12),
        ),
      ]);
      await tester.pump();

      final label = tester
          .getSemantics(find.byKey(const ValueKey('hit-msg-1')))
          .label;
      expect(label.runes.length, lessThan(300));
      expect(
        _hasUnpairedSurrogate(label),
        isFalse,
        reason: 'the announced excerpt must not end mid-character',
      );

      final rendered = tester
          .widget<Text>(
            find.descendant(
              of: find.byKey(const ValueKey('hit-msg-1')),
              matching: find.textContaining('🚀'),
            ),
          )
          .data!;
      expect(rendered.runes.length, lessThan(300));
      expect(_hasUnpairedSurrogate(rendered), isFalse);

      handle.dispose();
    });

    testWidgets('leaves a body that already fits untouched', (tester) async {
      final handle = tester.ensureSemantics();
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(
        find.byKey(const Key('search-field')),
        'deploy staging',
      );
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      const body =
          'deploy staging tonight, and roll back at the first sign of trouble';
      api.searchCalls.single.completer.complete([
        _hit(
          id: 'msg-1',
          sessionId: 'session-1',
          content: body,
          createdAt: DateTime.utc(2026, 8, 13, 12),
        ),
      ]);
      await tester.pump();

      expect(find.text(body), findsOneWidget);
      final label = tester
          .getSemantics(find.byKey(const ValueKey('hit-msg-1')))
          .label;
      expect(label, endsWith(body));
      expect(label, isNot(contains('…')));

      handle.dispose();
    });

    testWidgets('renders degenerate bodies without breaking the row', (
      tester,
    ) async {
      final handle = tester.ensureSemantics();
      final api = _FakeSearchApi();
      await _pumpScreen(tester, api);

      await tester.enterText(find.byKey(const Key('search-field')), 'deploy');
      await tester.testTextInput.receiveAction(TextInputAction.search);
      await tester.pump();

      // Whitespace-only past the budget, and empty: the shapes an
      // index-arithmetic excerpt trips over.
      api.searchCalls.single.completer.complete([
        _hit(
          id: 'msg-blank',
          sessionId: 'session-1',
          content: ' ' * 300,
          createdAt: DateTime.utc(2026, 8, 13, 12),
        ),
        _hit(
          id: 'msg-empty',
          sessionId: 'session-1',
          content: '',
          sequence: 2,
          createdAt: DateTime.utc(2026, 8, 13, 11),
        ),
      ]);
      await tester.pump();

      expect(tester.takeException(), isNull);
      expect(find.byKey(const ValueKey('hit-msg-blank')), findsOneWidget);
      expect(find.byKey(const ValueKey('hit-msg-empty')), findsOneWidget);
      expect(
        tester.getSize(find.byKey(const ValueKey('hit-msg-blank'))).height,
        lessThan(200),
      );
      expect(
        tester.getSemantics(find.byKey(const ValueKey('hit-msg-empty'))).label,
        contains('user message from '),
      );

      handle.dispose();
    });
  });
}

/// A message body far longer than a result row could sensibly show — a
/// pasted log or transcript, as far as the screen is concerned.
final _longBody =
    'deploy staging '
    '${List.filled(400, 'lorem ipsum dolor sit amet consectetur').join(' ')}';

/// Submits whatever the field already holds, re-attaching the text input
/// client that the previous submit detached when it dismissed the keyboard.
Future<void> _submitAgain(WidgetTester tester) async {
  await tester.showKeyboard(find.byKey(const Key('search-field')));
  await tester.testTextInput.receiveAction(TextInputAction.search);
}

/// The labels a screen reader would encounter, in traversal order, with
/// merged-away nodes already folded into whichever node speaks for them.
List<String> _spokenLabels(WidgetTester tester) {
  return tester.semantics
      .simulatedAccessibilityTraversal()
      .map((node) => node.getSemanticsData().label)
      .where((label) => label.isNotEmpty)
      .toList();
}

/// Whether [text] contains a UTF-16 surrogate that lost its partner: the
/// telltale of a string cut on code units instead of runes.
bool _hasUnpairedSurrogate(String text) {
  final units = text.codeUnits;
  for (var i = 0; i < units.length; i++) {
    final unit = units[i];
    if (unit >= 0xDC00 && unit <= 0xDFFF) return true;
    if (unit >= 0xD800 && unit <= 0xDBFF) {
      if (i + 1 == units.length) return true;
      final next = units[i + 1];
      if (next < 0xDC00 || next > 0xDFFF) return true;
      i++;
    }
  }
  return false;
}

/// Short month names as rendered by `DefaultMaterialLocalizations`, spelled
/// out here so date expectations don't lean on the widget's own formatter.
const _shortMonths = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
];

/// The visible "role · date" subtitle rendered for a hit.
String _hitSubtitle(WidgetTester tester, String messageId) {
  return tester
      .widget<Text>(
        find.descendant(
          of: find.byKey(ValueKey('hit-$messageId')),
          matching: find.textContaining('·'),
        ),
      )
      .data!;
}

/// Vertical offset of a rendered group heading, for render-order assertions.
double _groupTop(WidgetTester tester, String sessionId) {
  return tester.getTopLeft(find.byKey(ValueKey('group-header-$sessionId'))).dy;
}

/// Vertical offset of a rendered hit row, for render-order assertions.
double _hitTop(WidgetTester tester, String messageId) {
  return tester.getTopLeft(find.byKey(ValueKey('hit-$messageId'))).dy;
}

Future<List<String>> _pumpScreen(WidgetTester tester, TuringApi api) async {
  final opened = <String>[];
  await tester.pumpWidget(
    MaterialApp(
      home: SearchScreen(
        apiClient: api,
        onOpenSession: (sessionId) async {
          opened.add(sessionId);
        },
      ),
    ),
  );
  return opened;
}

SearchHit _hit({
  required String id,
  required String sessionId,
  String role = 'user',
  String content = 'deploy staging',
  int sequence = 1,
  required DateTime createdAt,
}) {
  return SearchHit(
    sessionId: sessionId,
    message: Message(
      messageId: id,
      role: role,
      content: content,
      sequence: sequence,
      createdAt: createdAt,
    ),
  );
}

class _SearchCall {
  _SearchCall(this.query) : completer = Completer<List<SearchHit>>();

  final String query;
  final Completer<List<SearchHit>> completer;
}

class _SessionCall {
  _SessionCall(this.sessionId) : completer = Completer<Session>();

  final String sessionId;
  final Completer<Session> completer;
}

class _FakeSearchApi implements TuringApi {
  final List<_SearchCall> searchCalls = [];
  final List<_SessionCall> sessionCalls = [];
  final List<String> sessionRequests = [];
  final List<int> limits = [];
  int activeSessionRequests = 0;
  int maxActiveSessionRequests = 0;

  List<String> get queries => searchCalls.map((c) => c.query).toList();

  @override
  Future<List<SearchHit>> searchMessages({
    required String query,
    int limit = 50,
  }) {
    limits.add(limit);
    final call = _SearchCall(query);
    searchCalls.add(call);
    return call.completer.future;
  }

  @override
  Future<void> deleteSession({required String sessionId}) async {}

  @override
  Future<Session> getSession({required String sessionId}) async {
    sessionRequests.add(sessionId);
    final call = _SessionCall(sessionId);
    sessionCalls.add(call);
    activeSessionRequests++;
    maxActiveSessionRequests = max(
      maxActiveSessionRequests,
      activeSessionRequests,
    );
    try {
      return await call.completer.future;
    } finally {
      activeSessionRequests--;
    }
  }

  @override
  Future<Map<String, dynamic>> approveApproval(
    String approvalId, {
    String? comment,
  }) async {
    return {'approvalId': approvalId, 'status': 'approved'};
  }

  @override
  Future<Map<String, dynamic>> createSession({String? title}) async {
    return {'sessionId': 'sess_1', 'createdAt': '2026-05-10T00:00:00.000Z'};
  }

  @override
  Future<Map<String, dynamic>> denyApproval(
    String approvalId, {
    String? reason,
  }) async {
    return {'approvalId': approvalId, 'status': 'denied'};
  }

  @override
  Future<List<ToolDescriptor>> listTools() async => const [];

  @override
  Future<List<AgentDescriptor>> listAgents() async => const [];

  @override
  Future<Map<String, dynamic>> getConfig() async {
    return {
      'enabledProviders': ['ollama'],
    };
  }

  @override
  Future<TuringEventPage> listEvents({
    required String sessionId,
    int? after,
    int limit = 500,
  }) async {
    return const TuringEventPage(events: [], latestSequence: 0);
  }

  @override
  Future<List<Message>> listMessages({
    required String sessionId,
    int limit = 50,
    String? before,
  }) async {
    return const [];
  }

  @override
  Future<List<Session>> listSessions({int limit = 50, String? after}) async {
    return const [];
  }

  @override
  Future<Map<String, dynamic>> sendMessage({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
  }) async {
    return {
      'sessionId': sessionId,
      'runId': 'run_1',
      'jobId': 'job_1',
      'traceId': 'trace_1',
      'status': 'queued',
    };
  }
}

/// A fake whose searches are already complete by the time the screen awaits
/// them, so no loading frame ever renders between two searches — the case
/// where an unchanged live region would otherwise be silently reused.
class _ImmediateSearchApi extends _FakeSearchApi {
  _ImmediateSearchApi(this.results);

  /// One entry per search, in call order; the last entry answers every
  /// further search.
  final List<List<SearchHit>> results;
  int _completed = 0;

  @override
  Future<List<SearchHit>> searchMessages({
    required String query,
    int limit = 50,
  }) {
    final future = super.searchMessages(query: query, limit: limit);
    searchCalls.last.completer.complete(
      results[min(_completed++, results.length - 1)],
    );
    return future;
  }
}
