import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/workspace/workspace_pages.dart';
import 'package:turing_flutter_app/models/mcp_server.dart';
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
      'a reimport failure shows the backend error and does not reload',
      (tester) async {
        final api = _McpApi()..importError = StateError('mcp.json is invalid');
        await _pumpMcps(tester, api);
        final listCallsBefore = api.listCalls;

        await tester.tap(find.byKey(const Key('mcpsReimportButton')));
        await tester.pumpAndSettle();

        // No report dialog on failure — the error surfaces instead.
        expect(find.text('mcp.json re-imported'), findsNothing);
        expect(find.textContaining('mcp.json is invalid'), findsOneWidget);
        expect(api.listCalls, listCallsBefore);
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

    testWidgets('choosing Remove from the popup calls deleteMcpServer with the '
        'exact id, then reloads', (tester) async {
      final api = _McpApi()..servers.add(_localServer());
      await _pumpMcps(tester, api);
      final listCallsBefore = api.listCalls;

      await tester.tap(find.byTooltip('Actions for vendor'));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Remove'));
      await tester.pumpAndSettle();

      expect(api.deleteCalls, ['mcp_vendor']);
      expect(api.listCalls, greaterThan(listCallsBefore));
      expect(find.text('vendor'), findsNothing);
    });

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
      await tester.tap(find.text('Rotate token').last);
      await tester.pumpAndSettle();

      expect(api.rotateCalls, hasLength(1));
      expect(api.deleteCalls, isEmpty);
    });
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
      await tester.tap(find.text('Rotate token').last);
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

      await tester.tap(find.text('Rotate token').last);
      await tester.pumpAndSettle();

      expect(api.rotateCalls, hasLength(2));
      expect(api.rotateCalls.last['token'], '');
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
      'a rotate failure keeps the dialog open with the error and does not '
      'reload',
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
        await tester.tap(find.text('Rotate token').last);
        await tester.pumpAndSettle();

        expect(api.rotateCalls, hasLength(1));
        // The dialog stays open with the error, rather than closing as if it
        // had succeeded.
        expect(find.byKey(const Key('mcpsRotateToken')), findsOneWidget);
        expect(
          find.textContaining('server rejected the token'),
          findsOneWidget,
        );
        expect(api.listCalls, listCallsBefore);
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
        await tester.tap(find.text('Rotate token').last);
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
        await tester.tap(find.text('Rotate token').last);
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

  group('no overflow at compact widths', () {
    for (final size in const [Size(390, 780), Size(300, 700), Size(320, 400)]) {
      testWidgets('the add-server form fits at ${size.width}x${size.height}', (
        tester,
      ) async {
        await _pumpMcps(tester, _McpApi(), size: size);
        expect(tester.takeException(), isNull);
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
    }
  });
}

Future<void> _selectTier(WidgetTester tester, String label) async {
  await tester.tap(find.byKey(const Key('mcpsAddTier')));
  await tester.pumpAndSettle();
  await tester.tap(find.text(label).last);
  await tester.pumpAndSettle();
}

McpServer _localServer({
  String serverId = 'mcp_vendor',
  String name = 'vendor',
}) => McpServer(
  serverId: serverId,
  name: name,
  transport: 'http',
  url: 'https://vendor.example/mcp',
  tier: McpServerTier.localContainer,
  enabled: false,
  liveness: McpServerLiveness.unknown,
  statusMessage: '',
  sandboxConfined: true,
  tools: const [],
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
  Object? listError;
  Completer<McpRegistrySnapshot>? listGate;
  int listCalls = 0;

  final List<Map<String, Object?>> registerCalls = [];
  int registerCallCount = 0;
  Object? registerError;
  Completer<void>? registerGate;
  int _nextId = 1;

  final List<Map<String, Object?>> enabledCalls = [];
  final List<String> deleteCalls = [];

  McpImportReport? importReport;
  Object? importError;
  Completer<McpImportReport>? reimportGate;
  int reimportCalls = 0;

  final List<Map<String, String>> rotateCalls = [];
  Object? rotateError;
  Completer<McpServer>? rotateGate;

  @override
  Future<McpRegistrySnapshot> listMcpServers() async {
    listCalls++;
    final gate = listGate;
    if (gate != null) return gate.future;
    final error = listError;
    if (error != null) throw error;
    return McpRegistrySnapshot(servers: servers, unsupported: unsupported);
  }

  @override
  Future<McpServer> setMcpServerEnabled({
    required String serverId,
    required bool enabled,
  }) async {
    enabledCalls.add({'serverId': serverId, 'enabled': enabled});
    final index = servers.indexWhere((s) => s.serverId == serverId);
    final updated = _withEnabled(servers[index], enabled);
    servers[index] = updated;
    return updated;
  }

  @override
  Future<void> deleteMcpServer({required String serverId}) async {
    deleteCalls.add(serverId);
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
    final gate = registerGate;
    if (gate != null) await gate.future;
    final error = registerError;
    if (error != null) throw error;
    registerCalls.add({
      'name': name,
      'url': url,
      'tier': tier,
      'token': bearerToken,
    });
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
    servers.add(server);
    return server;
  }

  @override
  Future<McpImportReport> reimportMcpJson() async {
    reimportCalls++;
    final gate = reimportGate;
    if (gate != null) return gate.future;
    final error = importError;
    if (error != null) throw error;
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
    if (error != null) throw error;
    final index = servers.indexWhere((s) => s.serverId == serverId);
    return servers[index];
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

  @override
  dynamic noSuchMethod(Invocation invocation) =>
      throw UnimplementedError('${invocation.memberName} is not used here');
}
