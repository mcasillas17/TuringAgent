import 'package:fixnum/fixnum.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/generated/google/protobuf/timestamp.pb.dart'
    as timestamppb;
import 'package:turing_flutter_app/generated/turing/v1/telemetry.pb.dart'
    as telemetrypb;
import 'package:turing_flutter_app/models/grpc_mappers.dart';

void main() {
  // The mapper's whole job on this message is to preserve a distinction
  // protobuf makes very easy to lose: an unset int64 reads as 0 through the
  // ordinary getter. If that collapse happens here, every "not reported" on
  // the page silently becomes a measured zero, and no widget test would catch
  // it because the widget would be told the truth it was given.
  test('an unset count arrives as null, not as protobuf zero', () {
    final summary = GrpcMappers.telemetrySummaryToModel(
      telemetrypb.GetTelemetrySummaryResponse(
        window: telemetrypb.TelemetryWindow(
          days: 7,
          start: timestamppb.Timestamp(seconds: Int64(1772000000)),
          end: timestamppb.Timestamp(seconds: Int64(1772604800)),
        ),
        runs: telemetrypb.RunTotals(total: Int64(3), completed: Int64(3)),
        tokens: telemetrypb.TokenTotals(runsWithoutUsage: Int64(3)),
        tools: [
          telemetrypb.ToolUsage(
            serverName: 'files',
            toolName: 'files.create',
            calls: Int64(1),
            denied: Int64(1),
          ),
        ],
        models: [
          telemetrypb.ModelUsage(
            provider: 'ollama',
            model: 'qwen2.5:7b',
            runs: Int64(3),
            runsWithoutUsage: Int64(3),
          ),
        ],
        daily: [telemetrypb.DailyActivity(date: '2026-03-08', runs: Int64(1))],
      ),
    );

    expect(summary.tokens.inputTokens, isNull);
    expect(summary.tokens.outputTokens, isNull);
    expect(summary.tokens.reported, isFalse);
    expect(summary.tokens.runsWithoutUsage, 3);
    expect(summary.runs.averageDurationMs, isNull);
    expect(summary.tools.single.averageDurationMs, isNull);
    expect(summary.models.single.inputTokens, isNull);
    expect(summary.daily.single.inputTokens, isNull);
  });

  test('a reported zero survives as a reported zero', () {
    final summary = GrpcMappers.telemetrySummaryToModel(
      telemetrypb.GetTelemetrySummaryResponse(
        window: telemetrypb.TelemetryWindow(days: 7),
        tokens: telemetrypb.TokenTotals(
          inputTokens: Int64(0),
          outputTokens: Int64(0),
          runsWithUsage: Int64(1),
        ),
      ),
    );

    // Set-to-zero and never-set are the same bytes on the wire only if
    // presence is dropped, which is exactly what the proto's `optional` is
    // there to prevent.
    expect(summary.tokens.inputTokens, 0);
    expect(summary.tokens.outputTokens, 0);
    expect(summary.tokens.reported, isTrue);
  });

  test('measured counts and the window come through intact', () {
    final summary = GrpcMappers.telemetrySummaryToModel(
      telemetrypb.GetTelemetrySummaryResponse(
        window: telemetrypb.TelemetryWindow(
          days: 30,
          start: timestamppb.Timestamp(seconds: Int64(1772000000)),
          end: timestamppb.Timestamp(seconds: Int64(1774592000)),
        ),
        runs: telemetrypb.RunTotals(
          total: Int64(10),
          completed: Int64(7),
          failed: Int64(2),
          cancelled: Int64(1),
          inFlight: Int64(0),
          averageDurationMs: Int64(1500),
        ),
        tokens: telemetrypb.TokenTotals(
          inputTokens: Int64(4000),
          outputTokens: Int64(500),
          runsWithUsage: Int64(7),
        ),
        externalAgents: [
          telemetrypb.ExternalAgentUsage(
            displayName: 'Claude',
            endpointHost: 'api.anthropic.com',
            runs: Int64(4),
            inputTokens: Int64(3000),
          ),
        ],
        automations: telemetrypb.AutomationTotals(
          runs: Int64(2),
          completed: Int64(2),
          unattendedApprovals: Int64(5),
        ),
        integrations: telemetrypb.IntegrationTotals(
          connected: Int64(1),
          revoked: Int64(2),
        ),
      ),
    );

    expect(summary.window.days, 30);
    expect(summary.window.start.isUtc, isTrue);
    expect(summary.runs.averageDurationMs, 1500);
    expect(summary.tokens.inputTokens, 4000);
    expect(summary.tokens.partial, isFalse);
    expect(summary.externalAgents.single.endpointHost, 'api.anthropic.com');
    expect(summary.externalAgents.single.outputTokens, isNull);
    expect(summary.automations.unattendedApprovals, 5);
    expect(summary.integrations.revoked, 2);
    expect(summary.hasActivity, isTrue);
  });
}
