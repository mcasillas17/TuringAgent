import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:turing_flutter_app/features/search/search_screen.dart';
import 'package:turing_flutter_app/models/message.dart';
import 'package:turing_flutter_app/models/search_hit.dart';
import 'package:turing_flutter_app/models/session.dart';
import 'package:turing_flutter_app/models/turing_event.dart';
import 'package:turing_flutter_app/networking/api_client.dart';

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

        await tester.enterText(
          find.byKey(const Key('search-field')),
          'deploy',
        );
        await tester.testTextInput.receiveAction(TextInputAction.search);
        await tester.pump();

        api.searchCalls.single.completer.complete(const []);
        await tester.pump();

        expect(find.byKey(const Key('search-empty')), findsOneWidget);
        expect(find.textContaining('exact phrase'), findsWidgets);
        expect(
          find.textContaining(RegExp('fewer|shorter')),
          findsOneWidget,
        );
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

    testWidgets('clearing input invalidates in-flight search', (
      tester,
    ) async {
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

    testWidgets('formats hit dates as an absolute local date', (
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

      final context = tester.element(find.byKey(const ValueKey('hit-msg-1')));
      final expectedDate = MaterialLocalizations.of(
        context,
      ).formatMediumDate(createdAt.toLocal());

      expect(find.textContaining(expectedDate), findsWidgets);
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

    testWidgets('falls back to Untitled chat for an empty resolved title', (
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

      expect(find.text('Untitled chat'), findsOneWidget);
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

    testWidgets('ignores a stale title response after a newer query', (
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
      // First (stale-to-be) title lookup is now in flight.
      expect(api.sessionCalls.length, 1);

      // Re-submitting the same query still bumps the generation, starting a
      // second, independent title lookup for the same session ID.
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
      expect(api.sessionCalls.length, 2);

      // Complete the stale (first-generation) lookup: must not update.
      api.sessionCalls[0].completer.complete(
        Session(
          sessionId: 'session-1',
          title: 'Stale Title',
          updatedAt: DateTime.utc(2026, 8, 13),
        ),
      );
      await tester.pump();
      expect(find.text('Stale Title'), findsNothing);
      expect(find.text('Session session-1'), findsOneWidget);

      // Complete the fresh (current-generation) lookup: must update.
      api.sessionCalls[1].completer.complete(
        Session(
          sessionId: 'session-1',
          title: 'Fresh Title',
          updatedAt: DateTime.utc(2026, 8, 13),
        ),
      );
      await tester.pump();
      expect(find.text('Fresh Title'), findsOneWidget);
      expect(find.text('Stale Title'), findsNothing);
    });

    testWidgets('tapping a result opens the exact session ID', (
      tester,
    ) async {
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

    testWidgets(
      'reuses a cached title across a later, unrelated query',
      (tester) async {
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

        await tester.enterText(
          find.byKey(const Key('search-field')),
          'second',
        );
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
      },
    );

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

      expect(
        api.sessionRequests.where((id) => id == 'session-1').length,
        2,
      );

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
        await tester.enterText(
          find.byKey(const Key('search-field')),
          'second',
        );
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
      'ignores a stale debounced search success after newer typed input',
      (tester) async {
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        await tester.enterText(find.byKey(const Key('search-field')), 'first');
        await tester.pump(const Duration(milliseconds: 350));
        expect(api.queries, ['first']);

        await tester.enterText(
          find.byKey(const Key('search-field')),
          'second',
        );
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

    testWidgets(
      'exposes semantics for the search field and the Retry action',
      (tester) async {
        final handle = tester.ensureSemantics();
        final api = _FakeSearchApi();
        await _pumpScreen(tester, api);

        final fieldFinder = find.bySemanticsLabel(
          RegExp('Search conversations'),
        );
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
      },
    );

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
  });
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
  required DateTime createdAt,
}) {
  return SearchHit(
    sessionId: sessionId,
    message: Message(
      messageId: id,
      role: role,
      content: content,
      sequence: 1,
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
  Future<Session> getSession({required String sessionId}) async {
    sessionRequests.add(sessionId);
    final call = _SessionCall(sessionId);
    sessionCalls.add(call);
    activeSessionRequests++;
    maxActiveSessionRequests = max(maxActiveSessionRequests, activeSessionRequests);
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
