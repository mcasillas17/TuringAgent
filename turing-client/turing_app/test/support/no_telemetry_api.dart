import 'package:turing_flutter_app/models/telemetry.dart';

/// Fills in the telemetry surface for fakes belonging to tests that are not
/// about telemetry.
///
/// Unlike the other mixins here, the read does not throw: a page under test
/// may legitimately never open Telemetry, and the shell builds destinations
/// lazily. It returns a summary that is honestly empty — every count zero,
/// every unmeasurable number null — so a test that does reach it renders the
/// "nothing happened" state rather than fabricated activity. This mirrors
/// [NoAutomationsApi] in the neighbouring file.
mixin NoTelemetryApi {
  Future<TelemetrySummary> getTelemetrySummary({
    required int windowDays,
  }) async => TelemetrySummary(
    window: TelemetryWindow(
      days: windowDays,
      start: DateTime.utc(2026, 3, 8),
      end: DateTime.utc(2026, 3, 15),
    ),
    runs: const TelemetryRunTotals(
      total: 0,
      completed: 0,
      failed: 0,
      cancelled: 0,
      inFlight: 0,
    ),
    tokens: const TelemetryTokenTotals(runsWithUsage: 0, runsWithoutUsage: 0),
    tools: const [],
    models: const [],
    externalAgents: const [],
    automations: const TelemetryAutomationTotals(
      runs: 0,
      completed: 0,
      failed: 0,
      unattendedApprovals: 0,
    ),
    integrations: const TelemetryIntegrationTotals(connected: 0, revoked: 0),
    daily: const [],
  );
}
