import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/workspace/telemetry_page.dart';
import 'package:turing_flutter_app/models/telemetry.dart';
import 'package:turing_flutter_app/networking/api_client.dart';

import '../support/no_audit_api.dart';
import '../support/no_automations_api.dart';
import '../support/no_external_agents_api.dart';
import '../support/no_integrations_api.dart';
import '../support/no_remote_egress_api.dart';
import '../support/no_session_lifecycle_api.dart';
import '../support/no_skills_api.dart';

void main() {
  group('saying what a number is worth', () {
    test('a count that was never measured never renders as zero', () {
      expect(formatTelemetryTokens(null), telemetryUnknownLabel);
      expect(formatTelemetryDuration(null), telemetryUnknownLabel);
      // And a real zero still renders as a real zero. Collapsing the two is
      // the failure this whole page is arranged around.
      expect(formatTelemetryTokens(0), '0');
      expect(formatTelemetryDuration(0), '0 ms');
    });

    test('large counts stay readable', () {
      expect(formatTelemetryCount(0), '0');
      expect(formatTelemetryCount(999), '999');
      expect(formatTelemetryCount(1000), '1,000');
      expect(formatTelemetryCount(1234567), '1,234,567');
    });

    test('durations change unit rather than growing digits', () {
      expect(formatTelemetryDuration(999), '999 ms');
      expect(formatTelemetryDuration(1500), '1.5 s');
      expect(formatTelemetryDuration(42000), '42 s');
      expect(formatTelemetryDuration(150000), '2.5 min');
    });
  });

  group('the sentence under the token totals', () {
    test('says so when nothing finished', () {
      final text = describeTokenProvenance(
        const TelemetryTokenTotals(runsWithUsage: 0, runsWithoutUsage: 0),
      );
      expect(text, contains('No conversation completed'));
      expect(text, contains('nothing to measure'));
    });

    test('says the counts are unknown when no provider reported', () {
      final text = describeTokenProvenance(
        const TelemetryTokenTotals(runsWithUsage: 0, runsWithoutUsage: 3),
      );
      expect(text, contains('None of the 3'));
      expect(text, contains('will not guess'));
    });

    test('says how much of the work a partial total covers', () {
      final text = describeTokenProvenance(
        const TelemetryTokenTotals(
          inputTokens: 100,
          outputTokens: 20,
          runsWithUsage: 2,
          runsWithoutUsage: 5,
        ),
      );
      expect(text, contains('Measured from 2 of 7 completed'));
      // Without this the reader would take an understated total as complete.
      expect(text, contains('higher by an unknown amount'));
    });

    test('claims completeness only when every finished run reported', () {
      final text = describeTokenProvenance(
        const TelemetryTokenTotals(
          inputTokens: 100,
          outputTokens: 20,
          runsWithUsage: 4,
          runsWithoutUsage: 0,
        ),
      );
      expect(text, 'Measured from all 4 completed conversations.');
    });
  });

  test('the window is spelled out with the numbers it describes', () {
    final text = describeTelemetryWindow(
      TelemetryWindow(
        days: 7,
        start: DateTime.utc(2026, 3, 8, 12),
        end: DateTime.utc(2026, 3, 15, 12),
      ),
    );
    expect(text, 'Last 7 days — 2026-03-08 to 2026-03-15 UTC');
  });

  group('the page', () {
    testWidgets('asks for a week first', (tester) async {
      final api = _FakeApi();
      await _pumpTelemetry(tester, api);

      expect(api.requestedWindows, [7]);
    });

    testWidgets('draws measured token totals', (tester) async {
      final api = _FakeApi()
        ..summary = _summary(
          tokens: const TelemetryTokenTotals(
            inputTokens: 12345,
            outputTokens: 678,
            runsWithUsage: 4,
            runsWithoutUsage: 0,
          ),
        );
      await _pumpTelemetry(tester, api);

      expect(find.text('12,345'), findsOneWidget);
      expect(find.text('678'), findsOneWidget);
      expect(
        find.text('Measured from all 4 completed conversations.'),
        findsOneWidget,
      );
    });

    testWidgets('says "not reported" instead of drawing a zero', (
      tester,
    ) async {
      final api = _FakeApi()
        ..summary = _summary(
          tokens: const TelemetryTokenTotals(
            runsWithUsage: 0,
            runsWithoutUsage: 2,
          ),
        );
      await _pumpTelemetry(tester, api);

      // A zero in either of these would be read as "this cost nothing", which
      // is a different claim from "nobody counted". Read by label rather than
      // by scanning the page, which is full of counts that legitimately are
      // zero.
      expect(_statValue(tester, 'Sent to models'), telemetryUnknownLabel);
      expect(_statValue(tester, 'Generated'), telemetryUnknownLabel);
    });

    testWidgets('ranks tools and reports a tool that never ran honestly', (
      tester,
    ) async {
      final api = _FakeApi()
        ..summary = _summary(
          tools: const [
            TelemetryToolUsage(
              serverName: 'files',
              toolName: 'files.read',
              calls: 9,
              failed: 2,
              denied: 0,
              averageDurationMs: 24,
            ),
            TelemetryToolUsage(
              serverName: 'files',
              toolName: 'files.create',
              calls: 1,
              failed: 0,
              denied: 1,
            ),
          ],
        );
      await _pumpTelemetry(tester, api);

      expect(find.text('files.read'), findsOneWidget);
      expect(find.text('9 calls'), findsOneWidget);
      expect(find.text('2 failed'), findsOneWidget);
      expect(find.text('avg 24 ms'), findsOneWidget);
      // A denied call never ran, so it has no latency and must not be given
      // one.
      expect(find.text('avg $telemetryUnknownLabel'), findsOneWidget);
      expect(find.text('1 denied'), findsOneWidget);
    });

    testWidgets('names what left the machine, and where it went', (
      tester,
    ) async {
      final api = _FakeApi()
        ..summary = _summary(
          externalAgents: const [
            TelemetryExternalAgentUsage(
              displayName: 'Claude',
              endpointHost: 'api.anthropic.com',
              runs: 3,
              inputTokens: 900,
              outputTokens: 120,
              runsWithoutUsage: 0,
            ),
          ],
        );
      await _pumpTelemetry(tester, api);

      expect(find.text('Claude'), findsOneWidget);
      expect(find.text('api.anthropic.com'), findsOneWidget);
      expect(find.text('3 runs sent'), findsOneWidget);
    });

    // The panel that most needs provenance is the one about what left the
    // machine. A token total there without its caveat reads as a complete
    // account of what was sent.
    testWidgets('says how many of the sent runs went unmeasured', (
      tester,
    ) async {
      final api = _FakeApi()
        ..summary = _summary(
          externalAgents: const [
            TelemetryExternalAgentUsage(
              displayName: 'Claude',
              endpointHost: 'api.anthropic.com',
              runs: 40,
              inputTokens: 5000,
              outputTokens: 600,
              runsWithoutUsage: 12,
            ),
          ],
          models: const [
            TelemetryModelUsage(
              provider: 'ollama',
              model: 'qwen2.5:7b',
              runs: 1200,
              runsWithoutUsage: 1200,
            ),
          ],
        );
      await _pumpTelemetry(tester, api);

      expect(find.text('12 runs reported no usage'), findsOneWidget);
      // And the same caveat on a model group, with the thousands separator
      // every other count on the page uses.
      expect(find.text('1,200 runs reported no usage'), findsOneWidget);
    });

    testWidgets('says plainly when nothing left the machine', (tester) async {
      await _pumpTelemetry(tester, _FakeApi());

      expect(
        find.textContaining('answered by a model running here'),
        findsOneWidget,
      );
    });

    testWidgets('flags approvals the user did not give', (tester) async {
      final api = _FakeApi()
        ..summary = _summary(
          automations: const TelemetryAutomationTotals(
            runs: 4,
            completed: 3,
            failed: 1,
            unattendedApprovals: 2,
          ),
        );
      await _pumpTelemetry(tester, api);

      expect(find.text('Approvals you did not give'), findsOneWidget);
      expect(find.textContaining('without asking at the time'), findsOneWidget);
    });

    testWidgets('does not let connection counts read as activity', (
      tester,
    ) async {
      final api = _FakeApi()
        ..summary = _summary(
          integrations: const TelemetryIntegrationTotals(
            connected: 2,
            revoked: 1,
          ),
        );
      await _pumpTelemetry(tester, api);

      expect(
        find.textContaining('not activity in this window'),
        findsOneWidget,
      );
      expect(find.textContaining('no usage to measure'), findsOneWidget);
    });

    testWidgets('says an empty window is empty, not broken', (tester) async {
      await _pumpTelemetry(tester, _FakeApi());

      expect(find.text('Nothing happened in this window'), findsOneWidget);
      expect(
        find.textContaining('not because nothing was recorded'),
        findsOneWidget,
      );
    });

    testWidgets('offers a retry when the backend cannot be reached', (
      tester,
    ) async {
      final api = _FakeApi()..error = StateError('backend is down');
      await _pumpTelemetry(tester, api);

      expect(find.text('Could not reach the backend'), findsOneWidget);
      expect(find.textContaining('backend is down'), findsOneWidget);

      api.error = null;
      await tester.tap(find.text('Try again'));
      await tester.pumpAndSettle();

      expect(find.text('Could not reach the backend'), findsNothing);
      expect(find.text('Tokens'), findsOneWidget);
    });

    testWidgets('changing the window asks the backend again', (tester) async {
      final api = _FakeApi();
      await _pumpTelemetry(tester, api);

      await tester.tap(find.text('30 days'));
      await tester.pumpAndSettle();

      expect(api.requestedWindows, [7, 30]);

      // Tapping the window already shown must not re-fetch: a page that
      // reloads on every tap looks broken on a slow backend.
      await tester.tap(find.text('30 days'));
      await tester.pumpAndSettle();
      expect(api.requestedWindows, [7, 30]);
    });

    testWidgets('shows a loading state before the first answer arrives', (
      tester,
    ) async {
      final api = _FakeApi()..hold = true;
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(body: TelemetryPage(apiClient: api)),
        ),
      );
      await tester.pump();

      expect(find.byType(CircularProgressIndicator), findsOneWidget);

      api.release();
      await tester.pumpAndSettle();
      expect(find.byType(CircularProgressIndicator), findsNothing);
    });

    testWidgets('draws the busiest day rather than an empty chart', (
      tester,
    ) async {
      final api = _FakeApi()
        ..summary = _summary(
          runs: const TelemetryRunTotals(
            total: 5,
            completed: 5,
            failed: 0,
            cancelled: 0,
            inFlight: 0,
            averageDurationMs: 1200,
          ),
          daily: const [
            TelemetryDailyActivity(date: '2026-03-08', runs: 0, toolCalls: 0),
            TelemetryDailyActivity(
              date: '2026-03-09',
              runs: 4,
              toolCalls: 2,
              inputTokens: 40,
            ),
            TelemetryDailyActivity(date: '2026-03-10', runs: 1, toolCalls: 0),
          ],
        );
      await _pumpTelemetry(tester, api);

      expect(find.text('2026-03-08'), findsOneWidget);
      expect(find.text('2026-03-10'), findsOneWidget);
      expect(find.text('busiest day: 4 runs'), findsOneWidget);
      expect(find.text('1.2 s'), findsOneWidget);

      // The bars are the only place data becomes pixels, so the mapping is
      // asserted rather than assumed: proportional to the busiest day, and a
      // day with no runs draws nothing at all rather than a misleading stub.
      final heights = tester
          .widgetList<FractionallySizedBox>(find.byType(FractionallySizedBox))
          .map((box) => box.heightFactor)
          .toList();
      expect(heights, [0.0, 1.0, 0.25]);
    });

    testWidgets('says the chart is empty rather than drawing a flat one', (
      tester,
    ) async {
      final api = _FakeApi()
        ..summary = _summary(
          daily: const [
            TelemetryDailyActivity(date: '2026-03-08', runs: 0, toolCalls: 0),
            TelemetryDailyActivity(date: '2026-03-09', runs: 0, toolCalls: 0),
          ],
        );
      await _pumpTelemetry(tester, api);

      expect(
        find.text('No conversation ran on any day in this window.'),
        findsOneWidget,
      );
    });
  });

  // The client has a compact layout below 840 logical px, so every number on
  // this page has to survive a phone.
  group('small screens', () {
    for (final size in const [
      Size(320, 640),
      Size(390, 844),
      Size(568, 320),
      Size(300, 400),
    ]) {
      testWidgets('does not overflow at ${size.width}x${size.height}', (
        tester,
      ) async {
        final api = _FakeApi()..summary = _busySummary();
        await _pumpTelemetry(tester, api, size: size);

        expect(tester.takeException(), isNull);
      });

      // The widest window the picker offers is 90 days, which is 90 bars
      // sharing whatever width the phone has.
      testWidgets(
        'the longest window still fits at ${size.width}x${size.height}',
        (tester) async {
          final api = _FakeApi()
            ..summary = _summary(
              daily: [
                for (var day = 1; day <= 90; day++)
                  TelemetryDailyActivity(
                    date: '2026-01-${day.toString().padLeft(2, '0')}',
                    runs: day % 7,
                    toolCalls: day,
                  ),
              ],
            );
          await _pumpTelemetry(tester, api, size: size);

          expect(tester.takeException(), isNull);
          expect(find.text('busiest day: 6 runs'), findsOneWidget);
        },
      );
    }

    testWidgets('the numbers are still legible in the light theme', (
      tester,
    ) async {
      final api = _FakeApi()..summary = _busySummary();
      tester.view.physicalSize = const Size(390, 844);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);

      await tester.pumpWidget(
        MaterialApp(
          theme: ThemeData(brightness: Brightness.light),
          home: Scaffold(body: TelemetryPage(apiClient: api)),
        ),
      );
      await tester.pumpAndSettle();

      expect(tester.takeException(), isNull);
      expect(find.text('Tokens'), findsOneWidget);
    });
  });
}

/// Reads the value a `_Stat` tile shows under [label]. The tile is a Column of
/// value-then-label, so the innermost Column around the label holds both.
String _statValue(WidgetTester tester, String label) {
  final tile = find
      .ancestor(of: find.text(label), matching: find.byType(Column))
      .first;
  final texts = tester
      .widgetList<Text>(find.descendant(of: tile, matching: find.byType(Text)))
      .toList();
  return texts.first.data ?? '';
}

Future<void> _pumpTelemetry(
  WidgetTester tester,
  _FakeApi api, {
  Size size = const Size(1000, 900),
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);

  await tester.pumpWidget(
    MaterialApp(
      theme: ThemeData(brightness: Brightness.dark),
      home: Scaffold(body: TelemetryPage(apiClient: api)),
    ),
  );
  await tester.pumpAndSettle();
}

TelemetrySummary _summary({
  TelemetryRunTotals? runs,
  TelemetryTokenTotals? tokens,
  List<TelemetryToolUsage> tools = const [],
  List<TelemetryModelUsage> models = const [],
  List<TelemetryExternalAgentUsage> externalAgents = const [],
  TelemetryAutomationTotals? automations,
  TelemetryIntegrationTotals? integrations,
  List<TelemetryDailyActivity> daily = const [],
}) {
  return TelemetrySummary(
    window: TelemetryWindow(
      days: 7,
      start: DateTime.utc(2026, 3, 8, 12),
      end: DateTime.utc(2026, 3, 15, 12),
    ),
    runs:
        runs ??
        const TelemetryRunTotals(
          total: 0,
          completed: 0,
          failed: 0,
          cancelled: 0,
          inFlight: 0,
        ),
    tokens:
        tokens ??
        const TelemetryTokenTotals(runsWithUsage: 0, runsWithoutUsage: 0),
    tools: tools,
    models: models,
    externalAgents: externalAgents,
    automations:
        automations ??
        const TelemetryAutomationTotals(
          runs: 0,
          completed: 0,
          failed: 0,
          unattendedApprovals: 0,
        ),
    integrations:
        integrations ??
        const TelemetryIntegrationTotals(connected: 0, revoked: 0),
    daily: daily,
  );
}

/// Every section populated at once, with the longest strings each can hold.
/// A layout test against an empty page proves nothing.
TelemetrySummary _busySummary() => _summary(
  runs: const TelemetryRunTotals(
    total: 128,
    completed: 120,
    failed: 6,
    cancelled: 1,
    inFlight: 1,
    averageDurationMs: 184000,
  ),
  tokens: const TelemetryTokenTotals(
    inputTokens: 1234567,
    outputTokens: 98765,
    runsWithUsage: 90,
    runsWithoutUsage: 30,
  ),
  tools: const [
    TelemetryToolUsage(
      serverName: 'files',
      toolName: 'files.update_a_file_with_a_very_long_name',
      calls: 4210,
      failed: 12,
      denied: 3,
      averageDurationMs: 1240,
    ),
  ],
  models: const [
    TelemetryModelUsage(
      provider: 'openai_compatible',
      model: 'claude-sonnet-4-20250514-with-a-long-suffix',
      runs: 40,
      runsWithoutUsage: 12,
    ),
  ],
  externalAgents: const [
    TelemetryExternalAgentUsage(
      displayName: 'A destination with a rather long display name',
      endpointHost: 'api.a-very-long-hostname.example.com',
      runs: 40,
      inputTokens: 900000,
      outputTokens: 120000,
      runsWithoutUsage: 5,
    ),
  ],
  automations: const TelemetryAutomationTotals(
    runs: 14,
    completed: 12,
    failed: 2,
    unattendedApprovals: 9,
  ),
  integrations: const TelemetryIntegrationTotals(connected: 3, revoked: 2),
  daily: const [
    TelemetryDailyActivity(date: '2026-03-08', runs: 3, toolCalls: 1),
    TelemetryDailyActivity(date: '2026-03-09', runs: 40, toolCalls: 90),
    TelemetryDailyActivity(date: '2026-03-10', runs: 0, toolCalls: 0),
    TelemetryDailyActivity(date: '2026-03-11', runs: 12, toolCalls: 4),
  ],
);

class _FakeApi
    with
        NoAuditApi,
        NoSkillsApi,
        NoExternalAgentsApi,
        NoIntegrationsApi,
        NoRemoteEgressApi,
        NoSessionLifecycleApi,
        NoAutomationsApi
    implements TuringApi {
  TelemetrySummary? summary;
  Object? error;
  final List<int> requestedWindows = [];

  /// Parks the next call so the loading state can be observed rather than
  /// raced past.
  bool hold = false;
  Completer<void>? _gate;

  void release() {
    hold = false;
    _gate?.complete();
    _gate = null;
  }

  @override
  Future<TelemetrySummary> getTelemetrySummary({
    required int windowDays,
  }) async {
    requestedWindows.add(windowDays);
    if (hold) {
      final gate = _gate ??= Completer<void>();
      await gate.future;
    }
    if (error != null) throw error!;
    return summary ?? _summary();
  }

  @override
  dynamic noSuchMethod(Invocation invocation) =>
      throw UnimplementedError('${invocation.memberName} is not used here');
}
