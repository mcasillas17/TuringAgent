import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/constants/app_colors.dart';
import 'package:turing_flutter_app/features/workspace/workspace_pages.dart';
import 'package:turing_flutter_app/models/mcp_server.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';
import 'package:turing_flutter_app/networking/api_client.dart';

import '../support/no_audit_api.dart';
import '../support/no_external_agents_api.dart';
import '../support/no_integrations_api.dart';
import '../support/no_remote_egress_api.dart';
import '../support/no_session_lifecycle_api.dart';
import '../support/no_skills_api.dart';
import '../support/no_telemetry_api.dart';

void main() {
  group('registering a server', () {
    testWidgets('submits exact name/url/tier/token and stays disabled', (
      tester,
    ) async {
      final api = _McpApi();
      await _pumpMcps(tester, api);

      await tester.enterText(find.byKey(const Key('mcpsAddName')), 'Vendor');
      await tester.enterText(
        find.byKey(const Key('mcpsAddUrl')),
        'https://vendor.example/mcp',
      );
      await _selectTier(tester, 'Remote URL');
      await tester.enterText(
        find.byKey(const Key('mcpsAddToken')),
        'sekret-token-1',
      );
      await tester.tap(find.byKey(const Key('mcpsAddSubmit')));
      await tester.pumpAndSettle();

      expect(api.registerCalls, hasLength(1));
      final call = api.registerCalls.single;
      expect(call['name'], 'Vendor');
      expect(call['url'], 'https://vendor.example/mcp');
      expect(call['tier'], McpServerTier.remoteUrl);
      expect(call['token'], 'sekret-token-1');

      // The registered server is listed and disabled — registering never
      // enables it.
      expect(find.text('Vendor'), findsOneWidget);
      expect(find.text('Disabled'), findsWidgets);

      // The token must never be rendered or re-shown after success. If the
      // production code ever prefilled the controller from the response or
      // echoed it into a Text widget, this would fail because find.text also
      // matches EditableText controller content.
      expect(find.text('sekret-token-1'), findsNothing);
      // Fields are cleared, including the token.
      expect(
        tester
            .widget<TextField>(find.byKey(const Key('mcpsAddName')))
            .controller!
            .text,
        '',
      );
      expect(
        tester
            .widget<TextField>(find.byKey(const Key('mcpsAddUrl')))
            .controller!
            .text,
        '',
      );
      expect(
        tester
            .widget<TextField>(find.byKey(const Key('mcpsAddToken')))
            .controller!
            .text,
        '',
      );
    });

    testWidgets('announces the server was added and is disabled', (
      tester,
    ) async {
      final api = _McpApi();
      await _pumpMcps(tester, api);

      await tester.enterText(find.byKey(const Key('mcpsAddName')), 'Vendor');
      await tester.enterText(
        find.byKey(const Key('mcpsAddUrl')),
        'https://vendor.example/mcp',
      );
      await tester.tap(find.byKey(const Key('mcpsAddSubmit')));
      await tester.pumpAndSettle();

      expect(
        find.textContaining('added'),
        findsWidgets,
        reason: 'a confirmation announces the addition',
      );
      expect(find.textContaining('disabled'), findsWidgets);
    });

    testWidgets(
      'the token field is obscured with autocorrect/suggestions off',
      (tester) async {
        await _pumpMcps(tester, _McpApi());

        final field = tester.widget<TextField>(
          find.byKey(const Key('mcpsAddToken')),
        );
        expect(field.obscureText, isTrue);
        expect(field.autocorrect, isFalse);
        expect(field.enableSuggestions, isFalse);
        // Never prefilled.
        expect(field.controller!.text, '');
      },
    );

    testWidgets('the token field has an honest accessible label and no masked '
        'placeholder while empty', (tester) async {
      await _pumpMcps(tester, _McpApi());

      // The label describes what the field is for in plain language —
      // no bullet/asterisk masking stands in for a real label. Checked
      // both as rendered text and as the field's accessible semantics.
      expect(find.text('Bearer token (optional)'), findsOneWidget);
      expect(
        find.bySemanticsLabel(RegExp(r'Bearer token \(optional\)')),
        findsOneWidget,
      );

      // The old masked-placeholder label must not appear anywhere, and
      // the (empty) field renders no masking characters as text either.
      expect(find.textContaining('******'), findsNothing);
      expect(find.textContaining('••••••'), findsNothing);
    });

    testWidgets('defaults to local container and never offers bundled', (
      tester,
    ) async {
      await _pumpMcps(tester, _McpApi());

      // Default selection, shown in the closed field.
      expect(find.text('Local container'), findsOneWidget);

      await tester.tap(find.byKey(const Key('mcpsAddTier')));
      await tester.pumpAndSettle();

      expect(find.text('Local container'), findsWidgets);
      expect(find.text('Remote URL'), findsOneWidget);
      expect(find.text('Bundled'), findsNothing);

      // Close the menu.
      await tester.tap(find.text('Remote URL'));
      await tester.pumpAndSettle();
    });

    testWidgets(
      'typing an https:// URL auto-selects Remote URL, so a first '
      'submission succeeds without a manual tier correction',
      (tester) async {
        final api = _McpApi();
        await _pumpMcps(tester, api);

        await tester.enterText(
          find.byKey(const Key('mcpsAddName')),
          'Vendor',
        );
        await tester.enterText(
          find.byKey(const Key('mcpsAddUrl')),
          'https://vendor.example/mcp',
        );
        await tester.pumpAndSettle();

        // The closed dropdown already shows the auto-selected tier —
        // no tap on it was needed.
        expect(find.text('Remote URL'), findsOneWidget);
        expect(find.text('Local container'), findsNothing);

        await tester.tap(find.byKey(const Key('mcpsAddSubmit')));
        await tester.pumpAndSettle();

        expect(api.registerCalls, hasLength(1));
        expect(api.registerCalls.single['tier'], McpServerTier.remoteUrl);
      },
    );

    testWidgets(
      'typing an http:// URL auto-selects Local container',
      (tester) async {
        final api = _McpApi();
        await _pumpMcps(tester, api);

        // Start from an auto-selected Remote URL (via typing, not a
        // manual dropdown pick, which would instead disable further
        // auto-detection — see the "manually choosing a tier" test
        // below), so this proves the field actively re-selects Local
        // container when the scheme changes, not merely that it
        // already defaulted there.
        await tester.enterText(
          find.byKey(const Key('mcpsAddUrl')),
          'https://vendor.example/mcp',
        );
        await tester.pumpAndSettle();
        expect(find.text('Remote URL'), findsOneWidget);

        await tester.enterText(
          find.byKey(const Key('mcpsAddUrl')),
          'http://vendor.internal:9000/mcp',
        );
        await tester.pumpAndSettle();

        expect(find.text('Local container'), findsOneWidget);
        expect(find.text('Remote URL'), findsNothing);
      },
    );

    testWidgets(
      'a URL with leading/trailing whitespace and an uppercase HTTPS '
      'scheme still auto-selects Remote URL, so a first submission '
      'succeeds without a manual tier correction',
      (tester) async {
        final api = _McpApi();
        await _pumpMcps(tester, api);

        await tester.enterText(
          find.byKey(const Key('mcpsAddName')),
          'Vendor',
        );
        await tester.enterText(
          find.byKey(const Key('mcpsAddUrl')),
          '  HTTPS://vendor.example/mcp  ',
        );
        await tester.pumpAndSettle();

        expect(find.text('Remote URL'), findsOneWidget);
        expect(find.text('Local container'), findsNothing);

        await tester.tap(find.byKey(const Key('mcpsAddSubmit')));
        await tester.pumpAndSettle();

        expect(api.registerCalls, hasLength(1));
        expect(api.registerCalls.single['tier'], McpServerTier.remoteUrl);
      },
    );

    testWidgets(
      'a URL with leading/trailing whitespace and an uppercase HTTP '
      'scheme still auto-selects Local container, so a first submission '
      'succeeds without a manual tier correction',
      (tester) async {
        final api = _McpApi();
        await _pumpMcps(tester, api);

        await tester.enterText(
          find.byKey(const Key('mcpsAddName')),
          'Vendor',
        );
        // Start from an auto-selected Remote URL (via typing, not a
        // manual dropdown pick), so this proves the whitespace/uppercase
        // http:// URL below actively re-selects Local container, not
        // merely that the field already defaulted there.
        await tester.enterText(
          find.byKey(const Key('mcpsAddUrl')),
          'https://vendor.example/mcp',
        );
        await tester.pumpAndSettle();
        expect(find.text('Remote URL'), findsOneWidget);

        await tester.enterText(
          find.byKey(const Key('mcpsAddUrl')),
          '\tHTTP://vendor.internal:9000/mcp\n',
        );
        await tester.pumpAndSettle();

        expect(find.text('Local container'), findsOneWidget);
        expect(find.text('Remote URL'), findsNothing);

        await tester.tap(find.byKey(const Key('mcpsAddSubmit')));
        await tester.pumpAndSettle();

        expect(api.registerCalls, hasLength(1));
        expect(api.registerCalls.single['tier'], McpServerTier.localContainer);
      },
    );

    testWidgets(
      'a manually chosen tier stays sticky even against a whitespace/'
      'uppercase URL that would otherwise auto-select a different one',
      (tester) async {
        final api = _McpApi();
        await _pumpMcps(tester, api);

        // The user explicitly picks Remote URL themselves...
        await _selectTier(tester, 'Remote URL');
        // ...then types a whitespace-padded, uppercase http:// URL, which
        // (per the two tests above) would otherwise auto-select Local
        // container.
        await tester.enterText(
          find.byKey(const Key('mcpsAddName')),
          'Vendor',
        );
        await tester.enterText(
          find.byKey(const Key('mcpsAddUrl')),
          '  HTTP://vendor.internal:9000/mcp  ',
        );
        await tester.pumpAndSettle();

        // The user's explicit choice survives.
        expect(find.text('Remote URL'), findsOneWidget);
        expect(find.text('Local container'), findsNothing);

        await tester.tap(find.byKey(const Key('mcpsAddSubmit')));
        await tester.pumpAndSettle();

        expect(api.registerCalls, hasLength(1));
        expect(api.registerCalls.single['tier'], McpServerTier.remoteUrl);
      },
    );

    testWidgets(
      'manually choosing a tier stops the URL field from overriding it',
      (tester) async {
        final api = _McpApi();
        await _pumpMcps(tester, api);

        // The user explicitly picks Remote URL themselves...
        await _selectTier(tester, 'Remote URL');
        // ...then types an http:// URL, which would otherwise
        // auto-select Local container.
        await tester.enterText(
          find.byKey(const Key('mcpsAddName')),
          'Vendor',
        );
        await tester.enterText(
          find.byKey(const Key('mcpsAddUrl')),
          'http://vendor.internal:9000/mcp',
        );
        await tester.pumpAndSettle();

        // The user's explicit choice survives.
        expect(find.text('Remote URL'), findsOneWidget);
        expect(find.text('Local container'), findsNothing);

        await tester.tap(find.byKey(const Key('mcpsAddSubmit')));
        await tester.pumpAndSettle();

        // The backend still independently validates the pair — this
        // proves only that the UI itself respects the user's override,
        // not that a mismatch would be accepted.
        expect(api.registerCalls, hasLength(1));
        expect(api.registerCalls.single['tier'], McpServerTier.remoteUrl);
      },
    );

    testWidgets(
      'the tier auto-selects again for the next server after a successful '
      'submission resets the form',
      (tester) async {
        final api = _McpApi();
        await _pumpMcps(tester, api);

        await tester.enterText(
          find.byKey(const Key('mcpsAddName')),
          'Vendor',
        );
        await tester.enterText(
          find.byKey(const Key('mcpsAddUrl')),
          'https://vendor.example/mcp',
        );
        await tester.tap(find.byKey(const Key('mcpsAddSubmit')));
        await tester.pumpAndSettle();
        expect(api.registerCalls, hasLength(1));

        // The form reset to its own default tier...
        expect(find.text('Local container'), findsOneWidget);

        // ...and auto-detection still runs for the next entry.
        await tester.enterText(
          find.byKey(const Key('mcpsAddUrl')),
          'https://vendor-two.example/mcp',
        );
        await tester.pumpAndSettle();
        expect(find.text('Remote URL'), findsOneWidget);
      },
    );

    testWidgets(
      'the add-form token field explains the token is sealed and never '
      'shown again',
      (tester) async {
        await _pumpMcps(tester, _McpApi());

        expect(
          find.textContaining(
            'Stored sealed. Never shown again \u2014 rotate to replace.',
          ),
          findsOneWidget,
        );

        // Still obscured, no autocorrect/suggestions — the new helper
        // text does not relax any of that.
        final field = tester.widget<TextField>(
          find.byKey(const Key('mcpsAddToken')),
        );
        expect(field.obscureText, isTrue);
        expect(field.autocorrect, isFalse);
        expect(field.enableSuggestions, isFalse);
      },
    );

    testWidgets('a missing name or url is refused before any call is made', (
      tester,
    ) async {
      final api = _McpApi();
      await _pumpMcps(tester, api);

      await tester.tap(find.byKey(const Key('mcpsAddSubmit')));
      await tester.pumpAndSettle();

      expect(api.registerCalls, isEmpty);
      expect(find.textContaining('required'), findsOneWidget);
    });

    testWidgets('a backend error keeps the form and never shows the token', (
      tester,
    ) async {
      final api = _McpApi()
        ..registerError = const TuringApiException(
          code: 'mcp_server_conflict',
          message: 'a server with that name already exists',
        );
      await _pumpMcps(tester, api);

      await tester.enterText(find.byKey(const Key('mcpsAddName')), 'Vendor');
      await tester.enterText(
        find.byKey(const Key('mcpsAddUrl')),
        'https://vendor.example/mcp',
      );
      await _selectTier(tester, 'Remote URL');
      await tester.enterText(
        find.byKey(const Key('mcpsAddToken')),
        'super-secret-value',
      );
      await tester.tap(find.byKey(const Key('mcpsAddSubmit')));
      await tester.pumpAndSettle();

      expect(
        find.textContaining('a server with that name already exists'),
        findsOneWidget,
      );
      // The call is recorded even though it failed, so a failure test can
      // assert on exactly what was sent — the fake records args before
      // consulting its gate/error, mirroring what a real API call sends
      // over the wire before the response comes back.
      expect(api.registerCalls, hasLength(1));
      expect(api.registerCalls.single, {
        'name': 'Vendor',
        'url': 'https://vendor.example/mcp',
        'tier': McpServerTier.remoteUrl,
        'token': 'super-secret-value',
      });
      // Non-secret form state is retained, so the user is not forced to
      // retype it to correct and retry.
      expect(
        tester
            .widget<TextField>(find.byKey(const Key('mcpsAddName')))
            .controller!
            .text,
        'Vendor',
      );
      expect(
        tester
            .widget<TextField>(find.byKey(const Key('mcpsAddUrl')))
            .controller!
            .text,
        'https://vendor.example/mcp',
      );
      // The previously selected tier is still shown, not reset to the
      // default.
      expect(find.text('Remote URL'), findsOneWidget);
      expect(find.text('Local container'), findsNothing);
      // Only the token is cleared — it is never retained or resubmitted
      // silently, and the actual sentinel/value never renders anywhere.
      expect(
        tester
            .widget<TextField>(find.byKey(const Key('mcpsAddToken')))
            .controller!
            .text,
        '',
      );
      expect(find.textContaining('super-secret-value'), findsNothing);
      expect(find.textContaining('******'), findsNothing);
    });

    testWidgets(
      'a backend error still reloads the registry through the callback',
      (tester) async {
        final api = _McpApi()
          ..registerError = const TuringApiException(
            code: 'mcp_server_conflict',
            message: 'a server with that name already exists',
          );
        await _pumpMcps(tester, api);
        final listCallsBefore = api.listCalls;

        await tester.enterText(find.byKey(const Key('mcpsAddName')), 'Vendor');
        await tester.enterText(
          find.byKey(const Key('mcpsAddUrl')),
          'https://vendor.example/mcp',
        );
        await tester.tap(find.byKey(const Key('mcpsAddSubmit')));
        await tester.pumpAndSettle();

        expect(
          find.textContaining('a server with that name already exists'),
          findsOneWidget,
        );
        // A registration can commit on the backend and still have its RPC
        // response fail, so the displayed state can no longer be trusted
        // as-is: the catch handler must reload through the same callback
        // the success path uses, the same way enable/disable and policy
        // changes already reload before showing their own error.
        expect(api.listCalls, greaterThan(listCallsBefore));
      },
    );

    testWidgets(
      'a backend error that actually committed on the backend still shows '
      'the registered server once reloaded',
      (tester) async {
        // Simulates a post-commit Internal error: the registration's
        // mutation lands (a server is actually created) but the response
        // itself still errors. Without reloading through the callback,
        // the UI would never display the server that was, in fact,
        // registered.
        final api = _McpApi()
          ..registerError = StateError('mcp.server.registered audit failed')
          ..registerCommitsBeforeThrowing = true;
        await _pumpMcps(tester, api);

        await tester.enterText(find.byKey(const Key('mcpsAddName')), 'Vendor');
        await tester.enterText(
          find.byKey(const Key('mcpsAddUrl')),
          'https://vendor.example/mcp',
        );
        await tester.tap(find.byKey(const Key('mcpsAddSubmit')));
        await tester.pumpAndSettle();

        expect(
          find.textContaining('mcp.server.registered audit failed'),
          findsOneWidget,
        );
        // The form's own Name field also still displays "Vendor" (retained
        // on failure), so asserting on the "Disabled" badge — which only
        // ever renders on a server card, never inside the form — is what
        // actually proves the reload surfaced a new card for it, rather
        // than merely finding the retained form text.
        expect(
          find.text('Disabled'),
          findsWidgets,
          reason:
              'the reload must surface the backend\'s authoritative '
              '(already-committed) state despite the RPC returning an error',
        );
      },
    );

    testWidgets('a backend error is announced as a semantic live region', (
      tester,
    ) async {
      final api = _McpApi()
        ..registerError = const TuringApiException(
          code: 'mcp_server_conflict',
          message: 'a server with that name already exists',
        );
      await _pumpMcps(tester, api);

      await tester.enterText(find.byKey(const Key('mcpsAddName')), 'Vendor');
      await tester.enterText(
        find.byKey(const Key('mcpsAddUrl')),
        'https://vendor.example/mcp',
      );
      await tester.tap(find.byKey(const Key('mcpsAddSubmit')));
      await tester.pumpAndSettle();

      expect(
        _isAnnouncedAsLiveRegion(
          tester,
          find.textContaining('a server with that name already exists'),
        ),
        isTrue,
      );
    });

    testWidgets('a busy submission cannot be duplicated', (tester) async {
      final gate = Completer<void>();
      final api = _McpApi()..registerGate = gate;
      await _pumpMcps(tester, api);

      await tester.enterText(find.byKey(const Key('mcpsAddName')), 'Vendor');
      await tester.enterText(
        find.byKey(const Key('mcpsAddUrl')),
        'https://vendor.example/mcp',
      );
      await tester.tap(find.byKey(const Key('mcpsAddSubmit')));
      await tester.pump();

      // While pending, a second tap must not start a second call.
      await tester.tap(find.byKey(const Key('mcpsAddSubmit')));
      await tester.pump();
      expect(api.registerCallCount, 1);

      gate.complete();
      await tester.pumpAndSettle();
      expect(api.registerCallCount, 1);
    });
  });

  group('reimporting mcp.json', () {
    testWidgets('the action is visible while loading, empty, and errored', (
      tester,
    ) async {
      final loadingApi = _McpApi()..listGate = Completer<McpRegistrySnapshot>();
      tester.view.physicalSize = const Size(1200, 900);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(body: McpsPage(apiClient: loadingApi)),
        ),
      );
      await tester.pump();
      expect(find.text('Re-import mcp.json'), findsOneWidget);
      loadingApi.listGate!.complete(
        McpRegistrySnapshot(servers: const [], unsupported: const []),
      );
      await tester.pumpAndSettle();

      final emptyApi = _McpApi();
      await _pumpMcps(tester, emptyApi);
      expect(find.text('Re-import mcp.json'), findsOneWidget);

      final erroredApi = _McpApi()..listError = StateError('offline');
      await _pumpMcps(tester, erroredApi);
      expect(find.text('Re-import mcp.json'), findsOneWidget);
    });

    testWidgets('shows imported, skipped, and refused with reasons', (
      tester,
    ) async {
      final api = _McpApi()
        ..importReport = McpImportReport(
          imported: const ['newly-added'],
          skipped: const ['already-known'],
          refused: const [
            UnsupportedMcpServer(
              name: 'stdio-vendor',
              reason: 'stdio/command MCP servers are unsupported',
            ),
          ],
        );
      await _pumpMcps(tester, api);

      await tester.tap(find.text('Re-import mcp.json'));
      await tester.pumpAndSettle();

      expect(find.text('Imported'), findsOneWidget);
      expect(find.text('Skipped'), findsOneWidget);
      expect(find.text('Refused'), findsOneWidget);
      expect(find.textContaining('newly-added'), findsOneWidget);
      expect(find.textContaining('already-known'), findsOneWidget);
      expect(find.textContaining('stdio-vendor'), findsOneWidget);
      expect(
        find.textContaining('stdio/command MCP servers are unsupported'),
        findsOneWidget,
      );
      expect(api.reimportCalls, 1);
    });

    testWidgets(
      'explains a skipped entry as already registered with settings kept, '
      'by its exact name',
      (tester) async {
        final api = _McpApi()
          ..importReport = McpImportReport(
            imported: const [],
            skipped: const ['vendor'],
            refused: const [],
          );
        await _pumpMcps(tester, api);

        await tester.tap(find.text('Re-import mcp.json'));
        await tester.pumpAndSettle();

        // A bare name is not enough context to know settings were kept, not
        // overwritten by whatever mcp.json now says — the reason must
        // accompany the exact name.
        expect(
          find.text('vendor — already registered; existing settings were kept'),
          findsOneWidget,
        );
      },
    );

    testWidgets('explains how to repoint a skipped server to a new endpoint', (
      tester,
    ) async {
      final api = _McpApi()
        ..importReport = McpImportReport(
          imported: const [],
          skipped: const ['vendor'],
          refused: const [],
        );
      await _pumpMcps(tester, api);

      await tester.tap(find.text('Re-import mcp.json'));
      await tester.pumpAndSettle();

      expect(
        find.textContaining('remove it, then add it again'),
        findsOneWidget,
        reason:
            'an operator must be told how to point a skipped server at a '
            'new endpoint, since a plain mcp.json edit is silently kept '
            'as-is',
      );
    });

    testWidgets(
      'does not show the repoint explanation when nothing was skipped',
      (tester) async {
        final api = _McpApi()
          ..importReport = McpImportReport(
            imported: const ['vendor'],
            skipped: const [],
            refused: const [],
          );
        await _pumpMcps(tester, api);

        await tester.tap(find.text('Re-import mcp.json'));
        await tester.pumpAndSettle();

        expect(
          find.textContaining('remove it, then add it again'),
          findsNothing,
        );
      },
    );

    testWidgets('shows None for every empty section', (tester) async {
      final api = _McpApi()
        ..importReport = McpImportReport(
          imported: const [],
          skipped: const [],
          refused: const [],
        );
      await _pumpMcps(tester, api);

      await tester.tap(find.text('Re-import mcp.json'));
      await tester.pumpAndSettle();

      expect(find.text('None'), findsNWidgets(3));
    });

    testWidgets('the reimport report itself has no restart language', (
      tester,
    ) async {
      final api = _McpApi()
        ..importReport = McpImportReport(
          imported: const [],
          skipped: const [],
          refused: const [],
        );
      await _pumpMcps(tester, api);

      await tester.tap(find.text('Re-import mcp.json'));
      await tester.pumpAndSettle();

      // The dialog itself never asks for a restart — that copy belongs only
      // to the page's own explanatory text, not the reimport result.
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.textContaining('restart'),
        ),
        findsNothing,
      );
    });

    testWidgets('a busy reimport cannot be duplicated', (tester) async {
      final gate = Completer<McpImportReport>();
      final api = _McpApi()..reimportGate = gate;
      await _pumpMcps(tester, api);

      await tester.tap(find.byKey(const Key('mcpsReimportButton')));
      await tester.pump();
      await tester.tap(
        find.byKey(const Key('mcpsReimportButton')),
        warnIfMissed: false,
      );
      await tester.pump();
      expect(api.reimportCalls, 1);

      gate.complete(
        McpImportReport(
          imported: const [],
          skipped: const [],
          refused: const [],
        ),
      );
      await tester.pumpAndSettle();
      expect(api.reimportCalls, 1);
    });

    testWidgets(
      'a reimport failure shows the backend error and reloads the registry',
      (tester) async {
        final api = _McpApi()..importError = StateError('mcp.json is invalid');
        await _pumpMcps(tester, api);
        final listCallsBefore = api.listCalls;

        await tester.tap(find.byKey(const Key('mcpsReimportButton')));
        await tester.pumpAndSettle();

        // No report dialog on failure — the error surfaces instead.
        expect(find.text('mcp.json re-imported'), findsNothing);
        expect(find.textContaining('mcp.json is invalid'), findsOneWidget);
        // A reimport can commit some entries on the backend and still have
        // the RPC response itself fail, so the displayed state can no
        // longer be trusted as-is: the catch handler must reload before
        // showing the error, the same way enable/disable and policy
        // changes already do.
        expect(api.listCalls, greaterThan(listCallsBefore));
      },
    );

    testWidgets(
      'a reimport failure that actually committed on the backend still '
      'shows the imported server once reloaded',
      (tester) async {
        // Simulates a post-commit Internal error: the reimport's mutation
        // lands (a server is actually imported) but the response itself
        // still errors. Without reloading before showing the error, the UI
        // would never display the server that was, in fact, imported.
        final api = _McpApi()
          ..importError = StateError('mcp.server.reimported audit failed')
          ..importCommitsBeforeThrowing = true;
        await _pumpMcps(tester, api);

        await tester.tap(find.byKey(const Key('mcpsReimportButton')));
        await tester.pumpAndSettle();

        expect(
          find.textContaining('mcp.server.reimported audit failed'),
          findsOneWidget,
        );
        expect(
          find.text('reimported-before-failure'),
          findsOneWidget,
          reason:
              'the reload must surface the backend\'s authoritative '
              '(already-committed) state despite the RPC returning an error',
        );
      },
    );
  });

  group('managing an existing server in the list', () {
    testWidgets(
      'toggling a non-bundled server calls setMcpServerEnabled with the '
      'exact id and value, then reloads',
      (tester) async {
        final api = _McpApi()..servers.add(_localServer());
        await _pumpMcps(tester, api);
        final listCallsBefore = api.listCalls;

        await tester.tap(find.byType(Switch));
        await tester.pumpAndSettle();

        expect(api.enabledCalls, hasLength(1));
        expect(api.enabledCalls.single, {
          'serverId': 'mcp_vendor',
          'enabled': true,
        });
        expect(api.listCalls, greaterThan(listCallsBefore));
      },
    );

    testWidgets(
      'the enable switch is disabled for a non-bundled server with no '
      'configured endpoint, and never calls setMcpServerEnabled if tapped',
      (tester) async {
        final api = _McpApi()..servers.add(_localServer(url: ''));
        await _pumpMcps(tester, api);

        final toggle = tester.widget<Switch>(find.byType(Switch));
        expect(
          toggle.onChanged,
          isNull,
          reason: 'a placeholder with no endpoint must never be enable-able',
        );

        await tester.tap(find.byType(Switch), warnIfMissed: false);
        await tester.pumpAndSettle();
        expect(api.enabledCalls, isEmpty);
      },
    );

    testWidgets(
      'the disabled switch for an endpoint-less placeholder explains why '
      'via tooltip/semantics',
      (tester) async {
        final api = _McpApi()..servers.add(_localServer(url: ''));
        await _pumpMcps(tester, api);

        final tooltip = tester.widget<Tooltip>(
          find.ancestor(
            of: find.byType(Switch),
            matching: find.byType(Tooltip),
          ),
        );
        expect(tooltip.message, isNotNull);
        expect(tooltip.message!.toLowerCase(), contains('endpoint'));
      },
    );

    testWidgets(
      'a bundled server (which also has no url in this model) is disabled '
      'for the bundled reason, not rendered with the placeholder tooltip',
      (tester) async {
        final api = _McpApi()..servers.add(_bundledServer());
        await _pumpMcps(tester, api);

        final toggle = tester.widget<Switch>(find.byType(Switch));
        expect(toggle.onChanged, isNull);
        expect(
          find.ancestor(
            of: find.byType(Switch),
            matching: find.byType(Tooltip),
          ),
          findsNothing,
          reason:
              'the placeholder-specific tooltip must be scoped to '
              'non-bundled servers only',
        );
      },
    );

    testWidgets(
      'once a placeholder is registered with a real endpoint, its switch '
      'becomes enable-able again',
      (tester) async {
        final api = _McpApi()..servers.add(_localServer());
        await _pumpMcps(tester, api);

        final toggle = tester.widget<Switch>(find.byType(Switch));
        expect(toggle.onChanged, isNotNull);
      },
    );

    testWidgets(
      'a busy enable/disable toggle disables the switch and cannot be '
      'duplicated by rapid taps',
      (tester) async {
        final gate = Completer<void>();
        final api = _McpApi()
          ..servers.add(_localServer())
          ..enabledGates['mcp_vendor'] = gate;
        await _pumpMcps(tester, api);
        final listCallsBefore = api.listCalls;

        await tester.tap(find.byType(Switch));
        await tester.pump();

        // Second rapid tap while the first request is still pending must
        // not fire a second call — the switch itself should be disabled.
        final toggle = tester.widget<Switch>(find.byType(Switch));
        expect(toggle.onChanged, isNull);
        await tester.tap(find.byType(Switch), warnIfMissed: false);
        await tester.pump();

        expect(api.enabledCalls, hasLength(1));

        gate.complete();
        await tester.pumpAndSettle();

        expect(api.enabledCalls, hasLength(1));
        expect(api.listCalls, greaterThan(listCallsBefore));
      },
    );

    testWidgets(
      'a busy mutation on one server does not disable another server\'s '
      'switch or popup',
      (tester) async {
        final gate = Completer<void>();
        final api = _McpApi()
          ..servers.add(_localServer(serverId: 'mcp_a', name: 'alpha'))
          ..servers.add(_localServer(serverId: 'mcp_b', name: 'beta'))
          ..enabledGates['mcp_a'] = gate;
        await _pumpMcps(tester, api);

        await tester.tap(find.byType(Switch).first);
        await tester.pump();

        // alpha sorts before beta, so the first switch is alpha's — busy —
        // and the second is beta's, which must remain interactive.
        final switches = tester.widgetList<Switch>(find.byType(Switch));
        expect(switches.first.onChanged, isNull);
        expect(switches.last.onChanged, isNotNull);

        await tester.ensureVisible(find.byType(Switch).last);
        await tester.pump();
        await tester.tap(find.byType(Switch).last);
        await tester.pump();
        expect(api.enabledCalls, hasLength(2));
        expect(
          api.enabledCalls.map((c) => c['serverId']),
          containsAll(['mcp_a', 'mcp_b']),
        );

        gate.complete();
        await tester.pumpAndSettle();
      },
    );

    testWidgets(
      'a failed enable/disable toggle re-enables the switch and surfaces '
      'the error, after reloading the registry',
      (tester) async {
        final api = _McpApi()
          ..servers.add(_localServer())
          ..enabledError = StateError('vendor is unreachable');
        await _pumpMcps(tester, api);
        final listCallsBefore = api.listCalls;

        await tester.tap(find.byType(Switch));
        await tester.pumpAndSettle();

        expect(api.enabledCalls, hasLength(1));
        expect(find.textContaining('vendor is unreachable'), findsOneWidget);
        // The busy flag must be cleared on failure too, or the switch would
        // stay disabled forever after a transient error.
        final toggle = tester.widget<Switch>(find.byType(Switch));
        expect(toggle.onChanged, isNotNull);
        // The catch handler must reload the registry before showing the
        // error: a mutation can commit on the backend and still return an
        // error (e.g. a post-commit audit failure), so only a reload can
        // tell whether the displayed state is still authoritative.
        expect(api.listCalls, greaterThan(listCallsBefore));

        // The switch works again on the next attempt.
        await tester.tap(find.byType(Switch));
        await tester.pump();
        expect(api.enabledCalls, hasLength(2));
      },
    );

    testWidgets('a failed enable/disable toggle that actually committed on the '
        'backend still shows the committed value once reloaded', (
      tester,
    ) async {
      // Simulates a post-commit Internal error: the RPC's mutation lands
      // (enabled flips to true) but the response itself still errors
      // (e.g. an audit write failing after commit). Without reloading
      // before showing the error, the UI would keep displaying the
      // pre-mutation value forever.
      final api = _McpApi()
        ..servers.add(_localServer())
        ..enabledError = StateError('mcp.server.enabled audit failed')
        ..enabledCommitsBeforeThrowing = true;
      await _pumpMcps(tester, api);

      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();

      expect(api.enabledCalls, hasLength(1));
      expect(
        find.textContaining('mcp.server.enabled audit failed'),
        findsOneWidget,
      );
      final toggle = tester.widget<Switch>(find.byType(Switch));
      expect(
        toggle.value,
        isTrue,
        reason:
            'the reload must surface the backend\'s authoritative '
            '(already-committed) state despite the RPC returning an error',
      );
    });

    testWidgets(
      'a busy enable/disable renders a small progress indicator and keeps '
      'the popup disabled',
      (tester) async {
        final gate = Completer<void>();
        final api = _McpApi()
          ..servers.add(_localServer())
          ..enabledGates['mcp_vendor'] = gate;
        await _pumpMcps(tester, api);

        expect(find.byType(CircularProgressIndicator), findsNothing);

        await tester.tap(find.byType(Switch));
        await tester.pump();

        // A remote enable can involve a tools/list round trip; a small
        // progress indicator proves the tap wasn't ignored while it's in
        // flight.
        expect(find.byType(CircularProgressIndicator), findsOneWidget);
        final popup = tester.widget<PopupMenuButton<String>>(
          find.byType(PopupMenuButton<String>),
        );
        expect(popup.enabled, isFalse);

        // The progress indicator must carry a descriptive semantic label
        // (e.g. "Updating vendor") so assistive tech has something to
        // announce while busy — it does not need to be a liveRegion for
        // that, since the label is already present in the tree the moment
        // the state changes to busy (see `_isAnnouncedAsLiveRegion`, which
        // this deliberately does not use).
        final semanticsAncestors = find
            .ancestor(
              of: find.byType(CircularProgressIndicator),
              matching: find.byType(Semantics),
            )
            .evaluate()
            .map((element) => (element.widget as Semantics).properties.label);
        expect(semanticsAncestors, contains('Updating vendor'));

        gate.complete();
        await tester.pumpAndSettle();

        expect(find.byType(CircularProgressIndicator), findsNothing);
      },
    );

    testWidgets(
      'a busy delete disables the popup menu and cannot be duplicated by '
      'rapid taps',
      (tester) async {
        final gate = Completer<void>();
        final api = _McpApi()
          ..servers.add(_localServer())
          ..deleteGates['mcp_vendor'] = gate;
        await _pumpMcps(tester, api);
        final listCallsBefore = api.listCalls;

        await tester.tap(find.byTooltip('Actions for vendor'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Remove'));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('mcpsConfirmRemove')));
        await tester.pump();

        expect(api.deleteCalls, hasLength(1));

        // The popup itself must be disabled while the delete is pending —
        // a second rapid open-and-remove must not fire a duplicate call.
        final popup = tester.widget<PopupMenuButton<String>>(
          find.byType(PopupMenuButton<String>),
        );
        expect(popup.enabled, isFalse);

        gate.complete();
        await tester.pumpAndSettle();

        expect(api.deleteCalls, hasLength(1));
        expect(api.listCalls, greaterThan(listCallsBefore));
      },
    );

    testWidgets(
      'rapidly tapping "Remove server" twice in the confirmation dialog '
      'only calls deleteMcpServer once',
      (tester) async {
        // The dialog itself has no async work of its own — Navigator.pop
        // happens synchronously on the very first tap, so this proves a
        // regression (e.g. a future change adding awaited work before the
        // pop) could never let a second tap on the same still-visible
        // button dispatch a second delete: the gate here holds the
        // *downstream* _deleteServer call open, standing in for whatever
        // that future awaited work might be.
        final gate = Completer<void>();
        final api = _McpApi()
          ..servers.add(_localServer())
          ..deleteGates['mcp_vendor'] = gate;
        await _pumpMcps(tester, api);

        await tester.tap(find.byTooltip('Actions for vendor'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Remove'));
        await tester.pumpAndSettle();

        await tester.tap(find.byKey(const Key('mcpsConfirmRemove')));
        await tester.pump();
        await tester.tap(
          find.byKey(const Key('mcpsConfirmRemove')),
          warnIfMissed: false,
        );
        await tester.pump();

        expect(api.deleteCalls, hasLength(1));

        gate.complete();
        await tester.pumpAndSettle();
        expect(api.deleteCalls, hasLength(1));
      },
    );

    testWidgets(
      'a failed delete re-enables the popup menu and surfaces the error, '
      'after reloading the registry',
      (tester) async {
        final api = _McpApi()
          ..servers.add(_localServer())
          ..deleteError = StateError('vendor cannot be removed right now');
        await _pumpMcps(tester, api);
        final listCallsBefore = api.listCalls;

        await tester.tap(find.byTooltip('Actions for vendor'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Remove'));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('mcpsConfirmRemove')));
        await tester.pumpAndSettle();

        expect(api.deleteCalls, hasLength(1));
        expect(
          find.textContaining('vendor cannot be removed right now'),
          findsOneWidget,
        );
        // The busy flag must be cleared on failure too, or the popup would
        // stay disabled forever after a transient error.
        final popup = tester.widget<PopupMenuButton<String>>(
          find.byType(PopupMenuButton<String>),
        );
        expect(popup.enabled, isTrue);
        // A delete can commit on the backend and still have its RPC
        // response fail, so the displayed state can no longer be trusted
        // as-is: the catch handler must reload before showing the error,
        // the same way enable/disable and policy changes already do.
        expect(api.listCalls, greaterThan(listCallsBefore));
        expect(find.text('vendor'), findsOneWidget);
      },
    );

    testWidgets(
      'a failed delete that actually committed on the backend no longer '
      'shows the server once reloaded',
      (tester) async {
        // Simulates a post-commit Internal error: the delete's mutation
        // lands (the row is actually removed) but the response itself
        // still errors. Without reloading before showing the error, the UI
        // would keep showing a server that no longer exists.
        final api = _McpApi()
          ..servers.add(_localServer())
          ..deleteError = StateError('mcp.server.deleted audit failed')
          ..deleteCommitsBeforeThrowing = true;
        await _pumpMcps(tester, api);

        await tester.tap(find.byTooltip('Actions for vendor'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Remove'));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('mcpsConfirmRemove')));
        await tester.pumpAndSettle();

        expect(
          find.textContaining('mcp.server.deleted audit failed'),
          findsOneWidget,
        );
        expect(
          find.text('vendor'),
          findsNothing,
          reason:
              'the reload must surface the backend\'s authoritative '
              '(already-committed) state despite the RPC returning an error',
        );
      },
    );

    testWidgets(
      'choosing Remove opens a confirmation dialog naming the server and '
      'stating what removing it deletes',
      (tester) async {
        final api = _McpApi()..servers.add(_localServer());
        await _pumpMcps(tester, api);

        await tester.tap(find.byTooltip('Actions for vendor'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Remove'));
        await tester.pumpAndSettle();

        // Nothing is dispatched merely by opening the confirmation —
        // only an explicit confirm below may call deleteMcpServer.
        expect(api.deleteCalls, isEmpty);
        expect(find.textContaining('vendor'), findsWidgets);
        expect(find.textContaining('token'), findsWidgets);
        expect(find.textContaining('per-tool polic'), findsWidgets);
        expect(find.textContaining('suppressed'), findsWidgets);
        expect(find.widgetWithText(TextButton, 'Cancel'), findsOneWidget);
        expect(
          find.descendant(
            of: find.byKey(const Key('mcpsConfirmRemove')),
            matching: find.text('Remove server'),
          ),
          findsOneWidget,
        );
      },
    );

    testWidgets(
      'the confirmation dialog names the exact server chosen — never a '
      'different one — when multiple servers are registered',
      (tester) async {
        final api = _McpApi()
          ..servers.add(_localServer(serverId: 'mcp_alpha', name: 'alpha'))
          ..servers.add(_localServer(serverId: 'mcp_beta', name: 'beta'));
        await _pumpMcps(tester, api);

        await tester.ensureVisible(find.byTooltip('Actions for beta'));
        await tester.pumpAndSettle();
        await tester.tap(find.byTooltip('Actions for beta'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Remove'));
        await tester.pumpAndSettle();

        expect(find.text('Remove beta?'), findsOneWidget);
        expect(find.text('Remove alpha?'), findsNothing);
        expect(find.textContaining('"beta"'), findsWidgets);
        expect(find.textContaining('"alpha"'), findsNothing);

        await tester.tap(find.byKey(const Key('mcpsConfirmRemove')));
        await tester.pumpAndSettle();

        expect(api.deleteCalls, ['mcp_beta']);
        expect(find.text('alpha'), findsOneWidget);
        expect(find.text('beta'), findsNothing);
      },
    );

    testWidgets('canceling the removal confirmation dialog does not call '
        'deleteMcpServer and leaves the server in place', (tester) async {
      final api = _McpApi()..servers.add(_localServer());
      await _pumpMcps(tester, api);

      await tester.tap(find.byTooltip('Actions for vendor'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Remove'));
      await tester.pumpAndSettle();
      await tester.tap(find.widgetWithText(TextButton, 'Cancel'));
      await tester.pumpAndSettle();

      expect(api.deleteCalls, isEmpty);
      expect(find.text('vendor'), findsOneWidget);
      expect(find.byKey(const Key('mcpsConfirmRemove')), findsNothing);
    });

    testWidgets(
      'tapping the barrier on the removal confirmation dialog does not '
      'call deleteMcpServer',
      (tester) async {
        final api = _McpApi()..servers.add(_localServer());
        await _pumpMcps(tester, api);

        await tester.tap(find.byTooltip('Actions for vendor'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Remove'));
        await tester.pumpAndSettle();

        await tester.tapAt(const Offset(5, 5));
        await tester.pumpAndSettle();

        expect(api.deleteCalls, isEmpty);
        expect(find.text('vendor'), findsOneWidget);
        expect(find.byKey(const Key('mcpsConfirmRemove')), findsNothing);
      },
    );

    testWidgets(
      'the platform back gesture on the removal confirmation dialog does '
      'not call deleteMcpServer',
      (tester) async {
        final api = _McpApi()..servers.add(_localServer());
        await _pumpMcps(tester, api);

        await tester.tap(find.byTooltip('Actions for vendor'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Remove'));
        await tester.pumpAndSettle();

        final dynamic backResult = await tester.binding.handlePopRoute();
        await tester.pumpAndSettle();

        expect(backResult, isTrue);
        expect(api.deleteCalls, isEmpty);
        expect(find.text('vendor'), findsOneWidget);
        expect(find.byKey(const Key('mcpsConfirmRemove')), findsNothing);
      },
    );

    testWidgets(
      'confirming Remove calls deleteMcpServer with the exact id, then '
      'reloads',
      (tester) async {
        final api = _McpApi()..servers.add(_localServer());
        await _pumpMcps(tester, api);
        final listCallsBefore = api.listCalls;

        await tester.tap(find.byTooltip('Actions for vendor'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Remove'));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('mcpsConfirmRemove')));
        await tester.pumpAndSettle();

        expect(api.deleteCalls, ['mcp_vendor']);
        expect(api.listCalls, greaterThan(listCallsBefore));
        expect(find.text('vendor'), findsNothing);
      },
    );

    testWidgets(
      'a bundled server has no actions popup, no rotate, no delete, and '
      'its switch is disabled',
      (tester) async {
        final api = _McpApi()..servers.add(_bundledServer());
        await _pumpMcps(tester, api);

        expect(find.byTooltip('Actions for files'), findsNothing);
        expect(find.byType(PopupMenuButton<String>), findsNothing);
        expect(find.text('Rotate token'), findsNothing);
        expect(find.text('Remove'), findsNothing);

        final toggle = tester.widget<Switch>(find.byType(Switch));
        expect(toggle.onChanged, isNull);
      },
    );

    testWidgets('choosing Remove from the popup never invokes rotate', (
      tester,
    ) async {
      final api = _McpApi()..servers.add(_localServer());
      await _pumpMcps(tester, api);

      await tester.tap(find.byTooltip('Actions for vendor'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Remove'));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('mcpsConfirmRemove')));
      await tester.pumpAndSettle();

      expect(api.deleteCalls, hasLength(1));
      expect(api.rotateCalls, isEmpty);
    });

    testWidgets('choosing Rotate token from the popup never invokes delete', (
      tester,
    ) async {
      final api = _McpApi()..servers.add(_localServer());
      await _pumpMcps(tester, api);

      await tester.tap(find.byTooltip('Actions for vendor'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Rotate token'));
      await tester.pumpAndSettle();
      await tester.enterText(
        find.byKey(const Key('mcpsRotateToken')),
        'new-token',
      );
      await tester.tap(find.byKey(const Key('mcpsRotateSubmit')));
      await tester.pumpAndSettle();

      expect(api.rotateCalls, hasLength(1));
      expect(api.deleteCalls, isEmpty);
    });
  });

  group('confirming before enabling a remote server', () {
    testWidgets('flipping a local-container server on needs no confirmation', (
      tester,
    ) async {
      final api = _McpApi()..servers.add(_localServer(enabled: false));
      await _pumpMcps(tester, api);

      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsNothing);
      expect(api.enabledCalls, hasLength(1));
      expect(api.enabledCalls.single, {
        'serverId': 'mcp_vendor',
        'enabled': true,
      });
    });

    testWidgets('flipping any server off needs no confirmation', (
      tester,
    ) async {
      final api = _McpApi()
        ..servers.add(_remoteServer(serverId: 'mcp_remote', enabled: true));
      await _pumpMcps(tester, api);

      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();

      expect(find.byType(AlertDialog), findsNothing);
      expect(api.enabledCalls, hasLength(1));
      expect(api.enabledCalls.single, {
        'serverId': 'mcp_remote',
        'enabled': false,
      });
    });

    testWidgets('flipping a remote server on shows a confirmation naming its '
        'endpoint and host before making any call', (tester) async {
      final api = _McpApi()
        ..servers.add(
          _remoteServer(
            serverId: 'mcp_remote',
            name: 'remote-vendor',
            url: 'https://mcp.remote-vendor.example/api',
            enabled: false,
          ),
        );
      await _pumpMcps(tester, api);

      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();

      expect(find.text('Enable remote-vendor?'), findsOneWidget);
      final withinDialog = find.descendant(
        of: find.byType(AlertDialog),
        matching: find.textContaining('https://mcp.remote-vendor.example/api'),
      );
      expect(withinDialog, findsOneWidget);
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.textContaining('mcp.remote-vendor.example'),
        ),
        findsWidgets,
      );
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.textContaining('sent with that request'),
        ),
        findsOneWidget,
      );
      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.textContaining('Each run still asks separately'),
        ),
        findsOneWidget,
      );
      // No call at all until the confirmation is answered.
      expect(api.enabledCalls, isEmpty);
    });

    testWidgets(
      'cancelling the confirmation makes no call and leaves the switch off',
      (tester) async {
        final api = _McpApi()..servers.add(_remoteServer(enabled: false));
        await _pumpMcps(tester, api);

        await tester.tap(find.byType(Switch));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Cancel'));
        await tester.pumpAndSettle();

        expect(find.byType(AlertDialog), findsNothing);
        expect(api.enabledCalls, isEmpty);
        final toggle = tester.widget<Switch>(find.byType(Switch));
        expect(toggle.value, isFalse);
      },
    );

    testWidgets(
      'dismissing the confirmation via the barrier makes no call, the '
      'same as Cancel',
      (tester) async {
        final api = _McpApi()..servers.add(_remoteServer(enabled: false));
        await _pumpMcps(tester, api);

        await tester.tap(find.byType(Switch));
        await tester.pumpAndSettle();
        // Tap the modal barrier, well away from the dialog's own content.
        await tester.tapAt(const Offset(5, 5));
        await tester.pumpAndSettle();

        expect(find.byType(AlertDialog), findsNothing);
        expect(api.enabledCalls, isEmpty);
      },
    );

    testWidgets(
      'the platform back gesture dismisses the confirmation safely, the '
      'same as Cancel',
      (tester) async {
        final api = _McpApi()..servers.add(_remoteServer(enabled: false));
        await _pumpMcps(tester, api);

        await tester.tap(find.byType(Switch));
        await tester.pumpAndSettle();
        final dynamic backResult = await tester.binding.handlePopRoute();
        await tester.pumpAndSettle();

        expect(backResult, isTrue);
        expect(find.byType(AlertDialog), findsNothing);
        expect(api.enabledCalls, isEmpty);
      },
    );

    testWidgets(
      'confirming with "Enable and discover" calls setMcpServerEnabled '
      'and reloads',
      (tester) async {
        final api = _McpApi()
          ..servers.add(_remoteServer(serverId: 'mcp_remote', enabled: false));
        await _pumpMcps(tester, api);
        final listCallsBefore = api.listCalls;

        await tester.tap(find.byType(Switch));
        await tester.pumpAndSettle();
        await tester.tap(find.byKey(const Key('mcpsConfirmEnableRemote')));
        await tester.pumpAndSettle();

        expect(find.byType(AlertDialog), findsNothing);
        expect(api.enabledCalls, hasLength(1));
        expect(api.enabledCalls.single, {
          'serverId': 'mcp_remote',
          'enabled': true,
        });
        expect(api.listCalls, greaterThan(listCallsBefore));
      },
    );

    testWidgets('the confirmation never shows a token: no text field, and no '
        'sentinel token value present in its rendered text', (tester) async {
      final api = _McpApi()..servers.add(_remoteServer(enabled: false));
      await _pumpMcps(tester, api);

      await tester.tap(find.byType(Switch));
      await tester.pumpAndSettle();

      expect(
        find.descendant(
          of: find.byType(AlertDialog),
          matching: find.byType(TextField),
        ),
        findsNothing,
        reason:
            'this confirmation is informational only; it must never '
            'offer to enter or display a token',
      );
      expect(find.textContaining('super-secret-value'), findsNothing);
    });
  });

  group('changing a tool policy', () {
    testWidgets(
      'choosing a different policy calls updateMcpToolPolicy with the exact '
      'serverId/toolName/policy, disables the picker while busy, and '
      'reloads on success',
      (tester) async {
        final gate = Completer<void>();
        final api = _McpApi()
          ..servers.add(
            _localServer(
              tools: const [
                ToolDescriptor(
                  serverName: 'vendor',
                  toolName: 'search',
                  policy: ToolPolicy.safe,
                ),
              ],
            ),
          )
          ..policyGate = gate;
        await _pumpMcps(tester, api);
        final listCallsBefore = api.listCalls;

        await tester.ensureVisible(find.byType(DropdownButton<ToolPolicy>));
        await tester.pumpAndSettle();
        await tester.tap(find.byType(DropdownButton<ToolPolicy>));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Disabled').last);
        await tester.pump();

        expect(api.policyCalls, hasLength(1));
        expect(api.policyCalls.single, {
          'serverId': 'mcp_vendor',
          'toolName': 'search',
          'policy': ToolPolicy.disabled,
        });

        // While the request is in flight, the picker must be disabled so a
        // second selection cannot fire a duplicate call.
        final picker = tester.widget<DropdownButton<ToolPolicy>>(
          find.byType(DropdownButton<ToolPolicy>),
        );
        expect(picker.onChanged, isNull);

        gate.complete();
        await tester.pumpAndSettle();

        expect(api.policyCalls, hasLength(1));
        expect(api.listCalls, greaterThan(listCallsBefore));
      },
    );

    testWidgets(
      'a busy policy change for one tool does not disable a different '
      "tool's picker on the same server",
      (tester) async {
        final gate = Completer<void>();
        final api = _McpApi()
          ..servers.add(
            _localServer(
              tools: const [
                ToolDescriptor(
                  serverName: 'vendor',
                  toolName: 'ls',
                  policy: ToolPolicy.safe,
                ),
                ToolDescriptor(
                  serverName: 'vendor',
                  toolName: 'search',
                  policy: ToolPolicy.safe,
                ),
              ],
            ),
          )
          ..policyGate = gate;
        await _pumpMcps(tester, api);

        await tester.ensureVisible(
          find.byType(DropdownButton<ToolPolicy>).first,
        );
        await tester.pumpAndSettle();
        await tester.tap(find.byType(DropdownButton<ToolPolicy>).first);
        await tester.pumpAndSettle();
        await tester.tap(find.text('Disabled').last);
        await tester.pump();

        final pickers = tester.widgetList<DropdownButton<ToolPolicy>>(
          find.byType(DropdownButton<ToolPolicy>),
        );
        // ls comes before search alphabetically, so the busy picker is the
        // first one — the second tool's picker must stay enabled.
        expect(pickers.first.onChanged, isNull);
        expect(pickers.last.onChanged, isNotNull);

        gate.complete();
        await tester.pumpAndSettle();
      },
    );

    testWidgets(
      'a failed policy change re-enables the picker, surfaces the error, '
      'and reloads the registry',
      (tester) async {
        final api = _McpApi()
          ..servers.add(
            _localServer(
              tools: const [
                ToolDescriptor(
                  serverName: 'vendor',
                  toolName: 'search',
                  policy: ToolPolicy.safe,
                ),
              ],
            ),
          )
          ..policyError = StateError('policy update rejected');
        await _pumpMcps(tester, api);
        final listCallsBefore = api.listCalls;

        await tester.ensureVisible(find.byType(DropdownButton<ToolPolicy>));
        await tester.pumpAndSettle();
        await tester.tap(find.byType(DropdownButton<ToolPolicy>));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Disabled').last);
        await tester.pumpAndSettle();

        expect(api.policyCalls, hasLength(1));
        expect(find.textContaining('policy update rejected'), findsOneWidget);
        // The busy flag must be cleared on failure too, or the picker would
        // stay disabled forever after a transient error.
        final picker = tester.widget<DropdownButton<ToolPolicy>>(
          find.byType(DropdownButton<ToolPolicy>),
        );
        expect(picker.onChanged, isNotNull);
        // The catch handler must reload the registry before showing the
        // error: a policy update can commit on the backend and still return
        // an error (e.g. a post-commit audit failure), so only a reload can
        // tell whether the displayed policy is still authoritative.
        expect(api.listCalls, greaterThan(listCallsBefore));
      },
    );

    testWidgets(
      'a failed policy change that actually committed on the backend still '
      'shows the committed policy once reloaded',
      (tester) async {
        // Simulates a post-commit Internal error: the RPC's mutation lands
        // (the tool's policy flips to safe) but the response itself still
        // errors (e.g. an audit write failing after commit). Without
        // reloading before showing the error, the UI would keep displaying
        // the pre-mutation policy forever.
        final api = _McpApi()
          ..servers.add(
            _localServer(
              tools: const [
                ToolDescriptor(
                  serverName: 'vendor',
                  toolName: 'search',
                  policy: ToolPolicy.approvalRequired,
                ),
              ],
            ),
          )
          ..policyError = StateError('mcp.tool.policy_changed audit failed')
          ..policyCommitsBeforeThrowing = true;
        await _pumpMcps(tester, api);

        await tester.ensureVisible(find.byType(DropdownButton<ToolPolicy>));
        await tester.pumpAndSettle();
        await tester.tap(find.byType(DropdownButton<ToolPolicy>));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Runs freely').last);
        await tester.pumpAndSettle();

        expect(api.policyCalls, hasLength(1));
        expect(
          find.textContaining('mcp.tool.policy_changed audit failed'),
          findsOneWidget,
        );
        expect(
          find.text('Runs freely'),
          findsWidgets,
          reason:
              'the reload must surface the backend\'s authoritative '
              '(already-committed) policy despite the RPC returning an '
              'error',
        );
        // The busy flag must be cleared on failure too, or the picker would
        // stay disabled forever after a transient error.
        final picker = tester.widget<DropdownButton<ToolPolicy>>(
          find.byType(DropdownButton<ToolPolicy>),
        );
        expect(picker.onChanged, isNotNull);
        // No duplicate call: exactly one mutation attempt despite the
        // catch handler's own reload.
        expect(api.policyCalls, hasLength(1));
      },
    );
  });

  group('rotating a server token', () {
    testWidgets('rotate sends the entered token, and clear sends empty', (
      tester,
    ) async {
      final api = _McpApi()..servers.add(_localServer());
      await _pumpMcps(tester, api);
      await tester.pumpAndSettle();

      await tester.tap(find.byTooltip('Actions for vendor'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Rotate token'));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const Key('mcpsRotateToken')),
        'new-rotated-token',
      );
      await tester.tap(find.byKey(const Key('mcpsRotateSubmit')));
      await tester.pumpAndSettle();

      expect(api.rotateCalls, hasLength(1));
      expect(api.rotateCalls.single['serverId'], 'mcp_vendor');
      expect(api.rotateCalls.single['token'], 'new-rotated-token');
      // Never rendered anywhere once the dialog closes.
      expect(find.text('new-rotated-token'), findsNothing);

      // Reopen and rotate again leaving the field empty — this must send an
      // empty token, which is how a token is cleared.
      await tester.tap(find.byTooltip('Actions for vendor'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Rotate token'));
      await tester.pumpAndSettle();

      // Reopening is always empty, never the sentinel from the previous
      // rotation.
      expect(
        tester
            .widget<TextField>(find.byKey(const Key('mcpsRotateToken')))
            .controller!
            .text,
        '',
      );
      expect(find.text('new-rotated-token'), findsNothing);

      await tester.tap(find.byKey(const Key('mcpsRotateSubmit')));
      await tester.pumpAndSettle();

      expect(api.rotateCalls, hasLength(2));
      expect(api.rotateCalls.last['token'], '');
    });

    testWidgets('rotating a token reloads with liveness reset to Not checked, '
        'matching the backend resetting it to unknown', (tester) async {
      // The backend resets liveness to unknown/empty in the same
      // transaction as a token rotation (a prior reading was made using
      // the credential being replaced, so it says nothing about the new
      // one). No token is ever stored client-side; this only asserts the
      // liveness the fake's updated state reports once reloaded.
      final api = _McpApi()
        ..servers.add(
          _localServer(enabled: true, liveness: McpServerLiveness.up),
        );
      await _pumpMcps(tester, api);
      expect(find.text('Up'), findsOneWidget);

      await tester.tap(find.byTooltip('Actions for vendor'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Rotate token'));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const Key('mcpsRotateToken')),
        'new-rotated-token',
      );
      await tester.tap(find.byKey(const Key('mcpsRotateSubmit')));
      await tester.pumpAndSettle();

      expect(find.text('Up'), findsNothing);
      expect(find.text('Not checked'), findsOneWidget);
    });

    testWidgets('the token field is obscured and never prefilled', (
      tester,
    ) async {
      final api = _McpApi()..servers.add(_localServer());
      await _pumpMcps(tester, api);
      await tester.pumpAndSettle();

      await tester.tap(find.byTooltip('Actions for vendor'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Rotate token'));
      await tester.pumpAndSettle();

      final field = tester.widget<TextField>(
        find.byKey(const Key('mcpsRotateToken')),
      );
      expect(field.obscureText, isTrue);
      expect(field.autocorrect, isFalse);
      expect(field.enableSuggestions, isFalse);
      expect(field.controller!.text, '');
    });

    testWidgets('bundled servers have no rotate-token action', (tester) async {
      final api = _McpApi()..servers.add(_bundledServer());
      await _pumpMcps(tester, api);
      await tester.pumpAndSettle();

      expect(find.byTooltip('Actions for files'), findsNothing);
      expect(find.text('Rotate token'), findsNothing);
    });

    testWidgets(
      'a rotate failure keeps the dialog open with the error and reloads '
      'the registry',
      (tester) async {
        final api = _McpApi()
          ..servers.add(_localServer())
          ..rotateError = StateError('server rejected the token');
        await _pumpMcps(tester, api);
        final listCallsBefore = api.listCalls;

        await tester.tap(find.byTooltip('Actions for vendor'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Rotate token'));
        await tester.pumpAndSettle();

        await tester.enterText(
          find.byKey(const Key('mcpsRotateToken')),
          'will-fail',
        );
        await tester.tap(find.byKey(const Key('mcpsRotateSubmit')));
        await tester.pumpAndSettle();

        expect(api.rotateCalls, hasLength(1));
        // The dialog stays open with the error, rather than closing as if it
        // had succeeded.
        expect(find.byKey(const Key('mcpsRotateToken')), findsOneWidget);
        expect(
          find.textContaining('server rejected the token'),
          findsOneWidget,
        );
        // A rotation can commit on the backend and still have its RPC
        // response fail, so the displayed state can no longer be trusted
        // as-is: the catch handler must ask the parent to reload — while
        // this dialog stays open with the error — the same way
        // enable/disable and policy changes already reload before showing
        // their own error.
        expect(api.listCalls, greaterThan(listCallsBefore));
        expect(
          _isAnnouncedAsLiveRegion(
            tester,
            find.textContaining('server rejected the token'),
          ),
          isTrue,
        );
      },
    );

    testWidgets(
      'a rotate failure that actually committed on the backend still shows '
      'the reset liveness once reloaded, while the dialog stays open',
      (tester) async {
        // Simulates a post-commit Internal error: the rotation's mutation
        // lands (liveness resets to unknown, mirroring the backend) but
        // the response itself still errors. Without reloading before
        // showing the error, the page behind the still-open dialog would
        // keep displaying the pre-rotation liveness.
        final api = _McpApi()
          ..servers.add(
            _localServer(enabled: true, liveness: McpServerLiveness.up),
          )
          ..rotateError = StateError('mcp.server.token_rotated audit failed')
          ..rotateCommitsBeforeThrowing = true;
        await _pumpMcps(tester, api);
        expect(find.text('Up'), findsOneWidget);

        await tester.tap(find.byTooltip('Actions for vendor'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Rotate token'));
        await tester.pumpAndSettle();

        await tester.enterText(
          find.byKey(const Key('mcpsRotateToken')),
          'will-fail-post-commit',
        );
        await tester.tap(find.byKey(const Key('mcpsRotateSubmit')));
        await tester.pumpAndSettle();

        // The dialog stays open with the error.
        expect(find.byKey(const Key('mcpsRotateToken')), findsOneWidget);
        expect(
          find.textContaining('mcp.server.token_rotated audit failed'),
          findsOneWidget,
        );
        // The token field is cleared even on this post-commit failure,
        // matching every other rotate-failure path.
        expect(
          tester
              .widget<TextField>(find.byKey(const Key('mcpsRotateToken')))
              .controller!
              .text,
          '',
        );
        // The page behind the still-open dialog must reflect the reload —
        // liveness reset from "Up" to "Not checked" — not the stale
        // pre-rotation snapshot. The underlying route's widgets remain in
        // the tree (just visually behind the modal barrier), so both
        // texts are still queryable here.
        expect(find.text('Up'), findsNothing);
        expect(find.text('Not checked'), findsOneWidget);
      },
    );

    testWidgets(
      'a rotate failure clears the token field so the failed value is '
      'never retained, matching the add form, and the user can retype',
      (tester) async {
        final api = _McpApi()
          ..servers.add(_localServer())
          ..rotateError = StateError('server rejected the token');
        await _pumpMcps(tester, api);

        await tester.tap(find.byTooltip('Actions for vendor'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Rotate token'));
        await tester.pumpAndSettle();

        await tester.enterText(
          find.byKey(const Key('mcpsRotateToken')),
          'will-fail',
        );
        await tester.tap(find.byKey(const Key('mcpsRotateSubmit')));
        await tester.pumpAndSettle();

        // Write-only/minimal-retention: the rejected token is not left in
        // the controller, is never rendered, and no masked sentinel stands
        // in for it either.
        expect(
          tester
              .widget<TextField>(find.byKey(const Key('mcpsRotateToken')))
              .controller!
              .text,
          '',
        );
        expect(find.textContaining('will-fail'), findsNothing);
        expect(find.textContaining('******'), findsNothing);

        // The user can retype and retry without reopening the dialog.
        api.rotateError = null;
        await tester.enterText(
          find.byKey(const Key('mcpsRotateToken')),
          'second-attempt',
        );
        await tester.tap(find.byKey(const Key('mcpsRotateSubmit')));
        await tester.pumpAndSettle();

        expect(api.rotateCalls, hasLength(2));
        expect(api.rotateCalls.last['token'], 'second-attempt');
      },
    );

    testWidgets('a busy rotate submission cannot be duplicated', (
      tester,
    ) async {
      final gate = Completer<McpServer>();
      final api = _McpApi()
        ..servers.add(_localServer())
        ..rotateGate = gate;
      await _pumpMcps(tester, api);

      await tester.tap(find.byTooltip('Actions for vendor'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Rotate token'));
      await tester.pumpAndSettle();

      await tester.enterText(
        find.byKey(const Key('mcpsRotateToken')),
        'still-in-flight',
      );
      await tester.tap(find.byKey(const Key('mcpsRotateSubmit')));
      await tester.pump();
      await tester.tap(
        find.byKey(const Key('mcpsRotateSubmit')),
        warnIfMissed: false,
      );
      await tester.pump();

      expect(api.rotateCalls, hasLength(1));

      gate.complete(_localServer());
      await tester.pumpAndSettle();
      expect(api.rotateCalls, hasLength(1));
    });

    testWidgets(
      'the barrier and back navigation cannot dismiss the dialog while a '
      'rotation is pending, but it closes and reloads once it completes',
      (tester) async {
        final gate = Completer<McpServer>();
        final api = _McpApi()
          ..servers.add(_localServer())
          ..rotateGate = gate;
        await _pumpMcps(tester, api);
        final listCallsBefore = api.listCalls;

        await tester.tap(find.byTooltip('Actions for vendor'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Rotate token'));
        await tester.pumpAndSettle();

        await tester.enterText(
          find.byKey(const Key('mcpsRotateToken')),
          'still-in-flight',
        );
        await tester.tap(find.byKey(const Key('mcpsRotateSubmit')));
        await tester.pump();

        // Tapping the modal barrier must not dismiss the dialog while the
        // rotation is in flight. `pumpAndSettle` lets any dismiss
        // transition fully play out so a would-be dismissal is not missed.
        await tester.tapAt(const Offset(5, 5));
        await tester.pumpAndSettle();
        expect(find.byKey(const Key('mcpsRotateToken')), findsOneWidget);

        // Nor can the platform back gesture/button. `handlePopRoute`
        // resolves true because PopScope reports the request as handled —
        // the important thing is it did not actually close the dialog.
        final dynamic backResult = await tester.binding.handlePopRoute();
        await tester.pumpAndSettle();
        expect(backResult, isTrue);
        expect(find.byKey(const Key('mcpsRotateToken')), findsOneWidget);

        // Cancel must still be disabled while pending — the only way out
        // is for the request to finish.
        expect(
          tester
              .widget<TextButton>(find.widgetWithText(TextButton, 'Cancel'))
              .onPressed,
          isNull,
        );

        gate.complete(_localServer());
        await tester.pumpAndSettle();

        expect(find.byKey(const Key('mcpsRotateToken')), findsNothing);
        expect(api.listCalls, greaterThan(listCallsBefore));
      },
    );
  });

  group('the page subtitle', () {
    testWidgets('says enabling a remote server contacts it to discover tools, '
        'separately from the per-run consent for tool arguments/results', (
      tester,
    ) async {
      await _pumpMcps(tester, _McpApi());

      expect(
        find.textContaining('contacts its endpoint to discover its tools'),
        findsOneWidget,
        reason:
            'enabling a remote server is a real network contact for '
            'discovery, not a no-op — the copy must say so honestly',
      );
      expect(
        find.textContaining('every run still asks before sending'),
        findsOneWidget,
        reason:
            'discovery happening on enable must not be confused with '
            'the separate, still-required per-run consent before a '
            'tool call actually sends arguments/results',
      );
    });
  });

  group('showing the server endpoint', () {
    testWidgets('renders the canonical registered URL on the card', (
      tester,
    ) async {
      final api = _McpApi()
        ..servers.add(_localServer(url: 'https://vendor.example/mcp'));
      await _pumpMcps(tester, api);

      expect(find.text('https://vendor.example/mcp'), findsOneWidget);
    });

    testWidgets(
      'renders an honest "Endpoint not configured" warning for a legacy '
      'placeholder with no url',
      (tester) async {
        final api = _McpApi()..servers.add(_localServer(url: ''));
        await _pumpMcps(tester, api);

        expect(find.text('Endpoint not configured'), findsOneWidget);
      },
    );

    testWidgets(
      'the endpoint text is selectable so it can be copied/verified',
      (tester) async {
        final api = _McpApi()
          ..servers.add(_localServer(url: 'https://vendor.example/mcp'));
        await _pumpMcps(tester, api);

        // A SelectionArea makes its descendant Text selectable without the
        // clipping (no ellipsis support) that SelectableText imposes.
        expect(find.byType(SelectionArea), findsOneWidget);
        expect(find.text('https://vendor.example/mcp'), findsOneWidget);
      },
    );

    testWidgets(
      'a long endpoint truncates with an ellipsis while the full value '
      'stays available via tooltip and selection text',
      (tester) async {
        const longUrl =
            'https://a-fairly-long-vendor-hostname.example.com/mcp/v1/'
            'endpoint/that/keeps/going/and/going/until/it/would/overflow';
        final api = _McpApi()..servers.add(_localServer(url: longUrl));
        await _pumpMcps(tester, api, size: const Size(320, 700));

        // The compact layout must not overflow: the Text is one line with
        // an ellipsis, not clipped raw text extending past its bounds.
        expect(tester.takeException(), isNull);
        final text = tester.widget<Text>(
          find.descendant(
            of: find.byType(SelectionArea),
            matching: find.byType(Text),
          ),
        );
        expect(text.maxLines, 1);
        expect(text.overflow, TextOverflow.ellipsis);
        // The full value is still available: verbatim in the Text's own
        // data (so it can be selected/copied in full even though only a
        // truncated portion is drawn) and in the Tooltip's message (so it
        // can be read on hover/long-press).
        expect(text.data, longUrl);
        final tooltip = tester.widget<Tooltip>(
          find.ancestor(
            of: find.byType(SelectionArea),
            matching: find.byType(Tooltip),
          ),
        );
        expect(tooltip.message, longUrl);
      },
    );
  });

  group('the empty state', () {
    testWidgets(
      'says a server can be added here, mcp.json is bulk, no restart',
      (tester) async {
        await _pumpMcps(tester, _McpApi());

        expect(find.text('No MCP servers registered'), findsOneWidget);
        expect(find.text('No tools discovered'), findsNothing);
        expect(find.textContaining('Add a server here'), findsOneWidget);
        expect(find.textContaining('mcp.json'), findsWidgets);
        expect(find.textContaining('Re-import mcp.json'), findsWidgets);
        expect(
          find.textContaining('Neither path needs a backend restart'),
          findsOneWidget,
        );
        expect(
          find.textContaining('restart the backend to import'),
          findsNothing,
        );
      },
    );
  });

  group('the degraded registry state notice', () {
    // The backend reports a systemic, registry-wide degraded condition
    // through ListMcpServersResponse's own explicit registry_degraded/
    // registry_degradation_reason fields (see
    // internal/service/mcpregistry.ListMcpServers) — never a synthetic
    // "_registry"-named entry mixed into `unsupported`, which describes
    // only ordinary per-entry mcp.json import refusals. This is a
    // systemic status, not a per-entry import failure, so McpsPage gives
    // it its own separate notice and title rather than the generic
    // "`<name>` was not imported" framing any `unsupported` entry gets.
    testWidgets('renders a separate notice from the explicit fields', (
      tester,
    ) async {
      final api = _McpApi()
        ..servers = [_bundledServer()]
        ..registryDegraded = true
        ..registryDegradationReason =
            'MCP registry aggregate tool budget is exhausted; tool '
            'schemas are hidden until an oversized or excess server '
            'is deleted';
      await _pumpMcps(tester, api);

      expect(
        find.text('MCP registry is running in a degraded state'),
        findsOneWidget,
      );
      expect(
        find.textContaining('MCP registry aggregate tool budget is exhausted'),
        findsOneWidget,
      );
      // Never the generic "<name> was not imported" framing an ordinary
      // refused entry gets: a systemic degraded notice was never an
      // import attempt at all.
      expect(find.textContaining('was not imported'), findsNothing);

      final notice = tester.widget<WorkspaceNotice>(
        find.byKey(const Key('mcpRegistryDegradedNotice')),
      );
      expect(notice.tone, AppColors.warning);
      expect(notice.compact, isTrue);
    });

    testWidgets(
      'a real invalid mcp.json entry literally named "_registry" is an '
      'ordinary refusal, never a collision with the degraded notice',
      (tester) async {
        // Finding #1 means an mcp.json entry whose key is literally
        // "_registry" is refused through the ordinary synthetic
        // invalid-entry path (its leading "_" fails the server-name
        // pattern) — never recorded under that literal name — but this
        // proves the UI itself no longer special-cases that literal
        // string either, should it ever appear in `unsupported` for any
        // other reason.
        final api = _McpApi()
          ..unsupported = const [
            UnsupportedMcpServer(
              name: '_registry',
              reason: 'server name is invalid or reserved',
            ),
          ];
        await _pumpMcps(tester, api);

        expect(find.text('_registry was not imported'), findsOneWidget);
        expect(
          find.text('MCP registry is running in a degraded state'),
          findsNothing,
        );
        final notice = tester.widget<WorkspaceNotice>(
          find.byKey(const Key('mcpUnsupportedNotice-_registry')),
        );
        expect(notice.tone, AppColors.warning);
        expect(notice.compact, isFalse);
      },
    );

    testWidgets('the degraded notice and an ordinary refusal coexist with no '
        'duplicate keys', (tester) async {
      final api = _McpApi()
        ..servers = [_bundledServer()]
        ..registryDegraded = true
        ..registryDegradationReason =
            'MCP registry server count exceeds its operating limit; '
            'only a bounded subset is listed until excess servers are '
            'deleted'
        ..unsupported = const [
          UnsupportedMcpServer(
            name: 'stdio-vendor',
            reason: 'stdio/command MCP servers are unsupported',
          ),
        ];
      await _pumpMcps(tester, api);

      expect(
        find.byKey(const Key('mcpRegistryDegradedNotice')),
        findsOneWidget,
      );
      expect(
        find.byKey(const Key('mcpUnsupportedNotice-stdio-vendor')),
        findsOneWidget,
      );
      expect(find.text('stdio-vendor was not imported'), findsOneWidget);
    });

    testWidgets('clears once the backend reports the registry back to normal '
        '(delete/recover)', (tester) async {
      final api = _McpApi()
        ..servers = [_bundledServer()]
        ..registryDegraded = true
        ..registryDegradationReason =
            'MCP registry aggregate tool '
            'budget is exhausted';
      await _pumpMcps(tester, api);
      expect(
        find.byKey(const Key('mcpRegistryDegradedNotice')),
        findsOneWidget,
      );

      // Recovery: the backend now reports a healthy snapshot (e.g.
      // after the offending server was deleted) — reloaded here via
      // the same Re-import mcp.json action _reimport()'s own success
      // path already reloads through (see the "reimporting mcp.json"
      // group above), rather than only asserting against the fake
      // api's own field directly.
      api
        ..registryDegraded = false
        ..registryDegradationReason = '';
      await tester.tap(find.byKey(const Key('mcpsReimportButton')));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('mcpRegistryDegradedNotice')), findsNothing);
      expect(
        find.text('MCP registry is running in a degraded state'),
        findsNothing,
      );
    });

    testWidgets('an ordinary refused name keeps the unchanged framing', (
      tester,
    ) async {
      final api = _McpApi()
        ..unsupported = const [
          UnsupportedMcpServer(
            name: 'stdio-vendor',
            reason: 'stdio/command MCP servers are unsupported',
          ),
        ];
      await _pumpMcps(tester, api);

      expect(find.text('stdio-vendor was not imported'), findsOneWidget);
      expect(
        find.textContaining('stdio/command MCP servers are unsupported'),
        findsOneWidget,
      );
      expect(
        find.text('MCP registry is running in a degraded state'),
        findsNothing,
      );

      final notice = tester.widget<WorkspaceNotice>(
        find.byKey(const Key('mcpUnsupportedNotice-stdio-vendor')),
      );
      expect(notice.tone, AppColors.warning);
      expect(notice.compact, isFalse);
    });
  });

  group('no overflow at compact widths', () {
    for (final size in const [Size(390, 780), Size(300, 700), Size(320, 400)]) {
      testWidgets('the add-server form fits at ${size.width}x${size.height}', (
        tester,
      ) async {
        await _pumpMcps(tester, _McpApi(), size: size);
        expect(tester.takeException(), isNull);
      });

      testWidgets(
        'a remote server tier badge fits at ${size.width}x${size.height}',
        (tester) async {
          final api = _McpApi()..servers = [_remoteServer()];
          await _pumpMcps(tester, api, size: size);

          expect(find.text('Remote · enable + per-run egress'), findsOneWidget);
          expect(tester.takeException(), isNull);
        },
      );

      testWidgets(
        'a long registered endpoint fits at ${size.width}x${size.height} '
        'without overflowing',
        (tester) async {
          final api = _McpApi()
            ..servers.add(
              _localServer(
                url:
                    'https://a-fairly-long-vendor-hostname.example.com/mcp/v1/endpoint',
              ),
            );
          await _pumpMcps(tester, api, size: size);

          expect(tester.takeException(), isNull);
        },
      );

      testWidgets('an empty legacy placeholder endpoint warning fits at '
          '${size.width}x${size.height}', (tester) async {
        final api = _McpApi()..servers.add(_localServer(url: ''));
        await _pumpMcps(tester, api, size: size);

        expect(find.text('Endpoint not configured'), findsOneWidget);
        expect(tester.takeException(), isNull);
      });

      testWidgets('a busy enable/disable progress indicator fits at '
          '${size.width}x${size.height}', (tester) async {
        final gate = Completer<void>();
        final api = _McpApi()
          ..servers.add(_localServer())
          ..enabledGates['mcp_vendor'] = gate;
        await _pumpMcps(tester, api, size: size);

        await tester.ensureVisible(find.byType(Switch));
        await tester.pumpAndSettle();
        await tester.tap(find.byType(Switch));
        await tester.pump();

        expect(find.byType(CircularProgressIndicator), findsOneWidget);
        expect(tester.takeException(), isNull);

        gate.complete();
        await tester.pumpAndSettle();
      });

      testWidgets(
        'the reimport report dialog fits at ${size.width}x${size.height}',
        (tester) async {
          final api = _McpApi()
            ..importReport = McpImportReport(
              imported: const ['a-fairly-long-server-name-imported'],
              skipped: const ['another-fairly-long-server-name'],
              refused: const [
                UnsupportedMcpServer(
                  name: 'stdio-vendor-with-a-long-name',
                  reason:
                      'stdio/command MCP servers are unsupported by this '
                      'client and cannot be registered from here',
                ),
              ],
            );
          await _pumpMcps(tester, api, size: size);

          await tester.ensureVisible(find.text('Re-import mcp.json'));
          await tester.pumpAndSettle();
          await tester.tap(find.text('Re-import mcp.json'));
          await tester.pumpAndSettle();

          expect(tester.takeException(), isNull);
        },
      );

      testWidgets(
        'the rotate-token dialog fits at ${size.width}x${size.height}',
        (tester) async {
          final api = _McpApi()..servers.add(_localServer());
          await _pumpMcps(tester, api, size: size);
          await tester.pumpAndSettle();

          await tester.ensureVisible(find.byTooltip('Actions for vendor'));
          await tester.pumpAndSettle();
          await tester.tap(find.byTooltip('Actions for vendor'));
          await tester.pumpAndSettle();
          await tester.tap(find.text('Rotate token'));
          await tester.pumpAndSettle();

          expect(tester.takeException(), isNull);
        },
      );

      testWidgets('the remove-server confirmation dialog fits at '
          '${size.width}x${size.height}', (tester) async {
        final api = _McpApi()..servers.add(_localServer());
        await _pumpMcps(tester, api, size: size);
        await tester.pumpAndSettle();

        await tester.ensureVisible(find.byTooltip('Actions for vendor'));
        await tester.pumpAndSettle();
        await tester.tap(find.byTooltip('Actions for vendor'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Remove'));
        await tester.pumpAndSettle();

        expect(find.byKey(const Key('mcpsConfirmRemove')), findsOneWidget);
        expect(tester.takeException(), isNull);
      });

      testWidgets('the enable-remote-server confirmation dialog fits at '
          '${size.width}x${size.height}', (tester) async {
        final api = _McpApi()
          ..servers.add(
            _remoteServer(
              url:
                  'https://mcp.a-fairly-long-remote-vendor-hostname.example/api/mcp',
              enabled: false,
            ),
          );
        await _pumpMcps(tester, api, size: size);
        await tester.pumpAndSettle();

        await tester.ensureVisible(find.byType(Switch));
        await tester.pumpAndSettle();
        await tester.tap(find.byType(Switch));
        await tester.pumpAndSettle();

        expect(
          find.byKey(const Key('mcpsConfirmEnableRemote')),
          findsOneWidget,
        );
        expect(tester.takeException(), isNull);
      });

      testWidgets(
        'a server card with short and long tool names/policies fits at '
        '${size.width}x${size.height} with a working policy dropdown',
        (tester) async {
          final api = _McpApi()
            ..servers.add(
              _localServer(
                tools: const [
                  ToolDescriptor(
                    serverName: 'vendor',
                    toolName: 'ls',
                    policy: ToolPolicy.safe,
                  ),
                  ToolDescriptor(
                    serverName: 'vendor',
                    toolName:
                        'a_very_long_tool_name_that_could_overflow_a_narrow_row',
                    policy: ToolPolicy.approvalRequired,
                  ),
                ],
              ),
            );
          await _pumpMcps(tester, api, size: size);

          expect(tester.takeException(), isNull);

          // The dropdown must still open and show readable policy options.
          await tester.ensureVisible(
            find.byType(DropdownButton<ToolPolicy>).first,
          );
          await tester.pumpAndSettle();
          await tester.tap(find.byType(DropdownButton<ToolPolicy>).first);
          await tester.pumpAndSettle();

          expect(tester.takeException(), isNull);
          expect(find.text('Runs freely'), findsWidgets);
          expect(find.text('Asks first'), findsWidgets);
          expect(find.text('Disabled'), findsWidgets);
        },
      );
    }
  });
}

Future<void> _selectTier(WidgetTester tester, String label) async {
  await tester.tap(find.byKey(const Key('mcpsAddTier')));
  await tester.pumpAndSettle();
  await tester.tap(find.text(label).last);
  await tester.pumpAndSettle();
}

/// Whether a `Semantics(liveRegion: true)` ancestor wraps the widget found
/// by [finder], meaning assistive tech announces it without focus moving —
/// as opposed to merely being present as visible text.
bool _isAnnouncedAsLiveRegion(WidgetTester tester, Finder finder) {
  final ancestors = find.ancestor(of: finder, matching: find.byType(Semantics));
  for (final element in ancestors.evaluate()) {
    final widget = element.widget;
    if (widget is Semantics && widget.properties.liveRegion == true) {
      return true;
    }
  }
  return false;
}

McpServer _localServer({
  String serverId = 'mcp_vendor',
  String name = 'vendor',
  String url = 'https://vendor.example/mcp',
  bool enabled = false,
  McpServerLiveness liveness = McpServerLiveness.unknown,
  List<ToolDescriptor> tools = const [],
}) => McpServer(
  serverId: serverId,
  name: name,
  transport: 'http',
  url: url,
  tier: McpServerTier.localContainer,
  enabled: enabled,
  liveness: liveness,
  statusMessage: '',
  sandboxConfined: true,
  tools: tools,
);

McpServer _remoteServer({
  String serverId = 'mcp_remote_vendor',
  String name = 'remote-vendor',
  String url = 'https://remote-vendor.example/mcp',
  bool enabled = false,
  List<ToolDescriptor> tools = const [],
}) => McpServer(
  serverId: serverId,
  name: name,
  transport: 'http',
  url: url,
  tier: McpServerTier.remoteUrl,
  enabled: enabled,
  liveness: McpServerLiveness.unknown,
  statusMessage: '',
  sandboxConfined: false,
  tools: tools,
);

McpServer _bundledServer() => McpServer(
  serverId: 'mcp_files',
  name: 'files',
  transport: 'http',
  url: '',
  tier: McpServerTier.bundled,
  enabled: true,
  liveness: McpServerLiveness.up,
  statusMessage: '',
  sandboxConfined: true,
  tools: [],
);

Future<void> _pumpMcps(
  WidgetTester tester,
  _McpApi api, {
  Size size = const Size(1200, 900),
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);
  await tester.pumpWidget(
    MaterialApp(
      home: Scaffold(body: McpsPage(apiClient: api)),
    ),
  );
  await tester.pumpAndSettle();
}

/// A working in-memory MCP registry, so the UI is exercised against something
/// that behaves like the backend rather than a stub that always says yes.
class _McpApi
    with
        NoAuditApi,
        NoSkillsApi,
        NoExternalAgentsApi,
        NoIntegrationsApi,
        NoRemoteEgressApi,
        NoSessionLifecycleApi,
        NoTelemetryApi
    implements TuringApi {
  List<McpServer> servers = [];
  List<UnsupportedMcpServer> unsupported = [];
  bool registryDegraded = false;
  String registryDegradationReason = '';
  Object? listError;
  Completer<McpRegistrySnapshot>? listGate;
  int listCalls = 0;

  final List<Map<String, Object?>> registerCalls = [];
  int registerCallCount = 0;
  Object? registerError;
  Completer<void>? registerGate;
  int _nextId = 1;
  // When true, a new server row is added to `servers` before registerError
  // is thrown — simulating a post-commit Internal error (the backend
  // mutation committed, but the RPC response itself still failed). Mirrors
  // `enabledCommitsBeforeThrowing`.
  bool registerCommitsBeforeThrowing = false;

  final List<Map<String, Object?>> enabledCalls = [];
  Object? enabledError;
  // When true, the mutation is applied to `servers` before `enabledError` is
  // thrown — simulating a post-commit Internal error (the backend mutation
  // committed, but the RPC response itself still failed).
  bool enabledCommitsBeforeThrowing = false;
  // Keyed by serverId so tests can gate one server's mutation without
  // blocking another's — mirrors the production dedupe, which must not be
  // global across servers.
  final Map<String, Completer<void>> enabledGates = {};

  final List<String> deleteCalls = [];
  Object? deleteError;
  final Map<String, Completer<void>> deleteGates = {};
  // When true, the server is removed from `servers` before deleteError is
  // thrown — simulating a post-commit Internal error. Mirrors
  // `enabledCommitsBeforeThrowing`.
  bool deleteCommitsBeforeThrowing = false;

  McpImportReport? importReport;
  Object? importError;
  Completer<McpImportReport>? reimportGate;
  int reimportCalls = 0;
  // When true, a new server row is added to `servers` before importError is
  // thrown — simulating a reimport whose mutation committed (a server was
  // actually imported) but whose RPC response itself still failed. Mirrors
  // `enabledCommitsBeforeThrowing`.
  bool importCommitsBeforeThrowing = false;

  final List<Map<String, String>> rotateCalls = [];
  Object? rotateError;
  Completer<McpServer>? rotateGate;
  // When true, the server's liveness is reset (mirroring the backend's own
  // post-rotation liveness reset) before rotateError is thrown — simulating
  // a rotation whose mutation committed but whose RPC response itself
  // still failed. Mirrors `enabledCommitsBeforeThrowing`.
  bool rotateCommitsBeforeThrowing = false;

  final List<Map<String, Object?>> policyCalls = [];
  Object? policyError;
  Completer<void>? policyGate;
  // When true, the mutation is applied to `servers` before `policyError` is
  // thrown — simulating a post-commit Internal error (the backend mutation
  // committed, but the RPC response itself still failed). Mirrors
  // `enabledCommitsBeforeThrowing`.
  bool policyCommitsBeforeThrowing = false;

  @override
  Future<McpRegistrySnapshot> listMcpServers() async {
    listCalls++;
    final gate = listGate;
    if (gate != null) return gate.future;
    final error = listError;
    if (error != null) throw error;
    return McpRegistrySnapshot(
      servers: servers,
      unsupported: unsupported,
      registryDegraded: registryDegraded,
      registryDegradationReason: registryDegradationReason,
    );
  }

  @override
  Future<McpServer> setMcpServerEnabled({
    required String serverId,
    required bool enabled,
  }) async {
    enabledCalls.add({'serverId': serverId, 'enabled': enabled});
    final gate = enabledGates[serverId];
    if (gate != null) await gate.future;
    final error = enabledError;
    if (error != null) {
      if (enabledCommitsBeforeThrowing) {
        final index = servers.indexWhere((s) => s.serverId == serverId);
        // A missing index here means the test misconfigured `servers` for
        // this serverId — silently no-op-ing (or letting `servers[-1]`
        // throw an opaque RangeError) would leave a real bug in the fake
        // masquerading as a passing test. Fail loudly and specifically.
        if (index == -1) {
          throw StateError(
            'enabledCommitsBeforeThrowing: no server with serverId '
            '"$serverId" in servers; cannot simulate a post-commit failure '
            'for a server the fake does not know about',
          );
        }
        servers[index] = _withEnabled(servers[index], enabled);
      }
      throw error;
    }
    final index = servers.indexWhere((s) => s.serverId == serverId);
    final updated = _withEnabled(servers[index], enabled);
    servers[index] = updated;
    return updated;
  }

  @override
  Future<void> deleteMcpServer({required String serverId}) async {
    deleteCalls.add(serverId);
    final gate = deleteGates[serverId];
    if (gate != null) await gate.future;
    final error = deleteError;
    if (error != null) {
      if (deleteCommitsBeforeThrowing) {
        servers.removeWhere((s) => s.serverId == serverId);
      }
      throw error;
    }
    servers.removeWhere((s) => s.serverId == serverId);
  }

  @override
  Future<McpServer> registerMcpServer({
    required String name,
    required String url,
    required McpServerTier tier,
    String bearerToken = '',
  }) async {
    registerCallCount++;
    // Recorded before the gate/error so failure and busy-dedupe tests can
    // assert on exactly what was sent, even when the call never resolves
    // successfully.
    registerCalls.add({
      'name': name,
      'url': url,
      'tier': tier,
      'token': bearerToken,
    });
    final gate = registerGate;
    if (gate != null) await gate.future;
    final server = McpServer(
      serverId: 'mcp_new_${_nextId++}',
      name: name,
      transport: 'http',
      url: url,
      tier: tier,
      enabled: false,
      liveness: McpServerLiveness.unknown,
      statusMessage: '',
      sandboxConfined: tier == McpServerTier.localContainer,
      tools: const [],
    );
    final error = registerError;
    if (error != null) {
      if (registerCommitsBeforeThrowing) {
        servers.add(server);
      }
      throw error;
    }
    servers.add(server);
    return server;
  }

  @override
  Future<McpImportReport> reimportMcpJson() async {
    reimportCalls++;
    final gate = reimportGate;
    if (gate != null) return gate.future;
    final error = importError;
    if (error != null) {
      if (importCommitsBeforeThrowing) {
        servers.add(
          McpServer(
            serverId: 'mcp_new_${_nextId++}',
            name: 'reimported-before-failure',
            transport: 'http',
            url: 'https://reimported-before-failure.example/mcp',
            tier: McpServerTier.remoteUrl,
            enabled: false,
            liveness: McpServerLiveness.unknown,
            statusMessage: '',
            sandboxConfined: false,
            tools: const [],
          ),
        );
      }
      throw error;
    }
    return importReport ??
        McpImportReport(
          imported: const [],
          skipped: const [],
          refused: const [],
        );
  }

  @override
  Future<McpServer> rotateMcpServerToken({
    required String serverId,
    required String bearerToken,
  }) async {
    rotateCalls.add({'serverId': serverId, 'token': bearerToken});
    final gate = rotateGate;
    if (gate != null) await gate.future;
    final error = rotateError;
    if (error != null) {
      if (rotateCommitsBeforeThrowing) {
        final index = servers.indexWhere((s) => s.serverId == serverId);
        if (index == -1) {
          throw StateError(
            'rotateCommitsBeforeThrowing: no server with serverId '
            '"$serverId" in servers; cannot simulate a post-commit failure '
            'for a server the fake does not know about',
          );
        }
        servers[index] = _withLiveness(
          servers[index],
          McpServerLiveness.unknown,
        );
      }
      throw error;
    }
    final index = servers.indexWhere((s) => s.serverId == serverId);
    // Mirrors the backend: rotating a token resets liveness to
    // unknown/empty in the same transaction, since a prior reading was made
    // using the credential being replaced.
    final updated = _withLiveness(servers[index], McpServerLiveness.unknown);
    servers[index] = updated;
    return updated;
  }

  @override
  Future<ToolDescriptor> updateMcpToolPolicy({
    required String serverId,
    required String toolName,
    required ToolPolicy policy,
  }) async {
    policyCalls.add({
      'serverId': serverId,
      'toolName': toolName,
      'policy': policy,
    });
    final gate = policyGate;
    if (gate != null) await gate.future;
    final error = policyError;
    if (error != null) {
      if (policyCommitsBeforeThrowing) {
        _applyToolPolicy(
          serverId: serverId,
          toolName: toolName,
          policy: policy,
        );
      }
      throw error;
    }
    return _applyToolPolicy(
      serverId: serverId,
      toolName: toolName,
      policy: policy,
    );
  }

  /// Applies a policy change to the in-memory `servers` list and returns the
  /// updated [ToolDescriptor]. Shared by the success path and the
  /// `policyCommitsBeforeThrowing` post-commit-failure simulation, so both
  /// mutate `servers` identically.
  ToolDescriptor _applyToolPolicy({
    required String serverId,
    required String toolName,
    required ToolPolicy policy,
  }) {
    final serverIndex = servers.indexWhere((s) => s.serverId == serverId);
    // A missing index means the test misconfigured `servers` for this
    // serverId — fail loudly and specifically rather than letting
    // `servers[-1]` throw an opaque RangeError.
    if (serverIndex == -1) {
      throw StateError(
        'updateMcpToolPolicy: no server with serverId "$serverId" in '
        'servers; cannot apply a policy change to a server the fake does '
        'not know about',
      );
    }
    final server = servers[serverIndex];
    if (!server.tools.any((tool) => tool.toolName == toolName)) {
      throw StateError(
        'updateMcpToolPolicy: server "$serverId" has no tool named '
        '"$toolName"; cannot apply a policy change to an unknown tool',
      );
    }
    final tools = [
      for (final tool in server.tools)
        if (tool.toolName == toolName)
          ToolDescriptor(
            serverName: tool.serverName,
            toolName: tool.toolName,
            policy: policy,
            enabled: tool.enabled,
            present: tool.present,
          )
        else
          tool,
    ];
    servers[serverIndex] = _withTools(server, tools);
    return tools.firstWhere((tool) => tool.toolName == toolName);
  }

  McpServer _withEnabled(McpServer server, bool enabled) => McpServer(
    serverId: server.serverId,
    name: server.name,
    transport: server.transport,
    url: server.url,
    tier: server.tier,
    enabled: enabled,
    liveness: server.liveness,
    statusMessage: server.statusMessage,
    sandboxConfined: server.sandboxConfined,
    tools: server.tools,
  );

  McpServer _withLiveness(McpServer server, McpServerLiveness liveness) =>
      McpServer(
        serverId: server.serverId,
        name: server.name,
        transport: server.transport,
        url: server.url,
        tier: server.tier,
        enabled: server.enabled,
        liveness: liveness,
        statusMessage: '',
        sandboxConfined: server.sandboxConfined,
        tools: server.tools,
      );

  McpServer _withTools(McpServer server, List<ToolDescriptor> tools) =>
      McpServer(
        serverId: server.serverId,
        name: server.name,
        transport: server.transport,
        url: server.url,
        tier: server.tier,
        enabled: server.enabled,
        liveness: server.liveness,
        statusMessage: server.statusMessage,
        sandboxConfined: server.sandboxConfined,
        tools: tools,
      );

  @override
  dynamic noSuchMethod(Invocation invocation) =>
      throw UnimplementedError('${invocation.memberName} is not used here');
}
