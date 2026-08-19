/// What this installation has been doing, aggregated by the orchestrator from
/// its own database and served only here.
///
/// Nothing in this file is sent anywhere. It is read from the local backend
/// over the same port as everything else and drawn on one page.
///
/// The type that matters most below is `int?`. A null is a number NOBODY
/// MEASURED — a provider that did not report token usage, a tool that never
/// ran and so has no latency. It is never rendered as zero, because a zero is
/// a measurement and someone would spend a decision on it.
library;

class TelemetrySummary {
  const TelemetrySummary({
    required this.window,
    required this.runs,
    required this.tokens,
    required this.tools,
    required this.models,
    required this.externalAgents,
    required this.automations,
    required this.integrations,
    required this.daily,
  });

  final TelemetryWindow window;
  final TelemetryRunTotals runs;
  final TelemetryTokenTotals tokens;
  final List<TelemetryToolUsage> tools;
  final List<TelemetryModelUsage> models;
  final List<TelemetryExternalAgentUsage> externalAgents;
  final TelemetryAutomationTotals automations;
  final TelemetryIntegrationTotals integrations;
  final List<TelemetryDailyActivity> daily;

  /// Whether anything at all happened in this window. Used to say "nothing
  /// yet" once rather than repeating an empty state in every section.
  bool get hasActivity =>
      runs.total > 0 || tools.isNotEmpty || automations.runs > 0;
}

class TelemetryWindow {
  const TelemetryWindow({
    required this.days,
    required this.start,
    required this.end,
  });

  final int days;
  final DateTime start;
  final DateTime end;
}

class TelemetryRunTotals {
  const TelemetryRunTotals({
    required this.total,
    required this.completed,
    required this.failed,
    required this.cancelled,
    required this.inFlight,
    this.averageDurationMs,
  });

  final int total;
  final int completed;
  final int failed;
  final int cancelled;

  /// Queued, running, or waiting on an approval. Not a failure.
  final int inFlight;

  /// Derived from each run's own start and finish. Null when no run in the
  /// window recorded both.
  final int? averageDurationMs;
}

class TelemetryTokenTotals {
  const TelemetryTokenTotals({
    this.inputTokens,
    this.outputTokens,
    required this.runsWithUsage,
    required this.runsWithoutUsage,
  });

  final int? inputTokens;
  final int? outputTokens;

  /// Completed runs whose provider reported counts, and those whose provider
  /// did not. The totals above describe only the first group, which is why
  /// both numbers travel with them.
  final int runsWithUsage;
  final int runsWithoutUsage;

  bool get reported => inputTokens != null || outputTokens != null;

  /// True when some runs reported and others did not, which is the case where
  /// the totals are real but incomplete and have to say so.
  bool get partial => reported && runsWithoutUsage > 0;
}

class TelemetryToolUsage {
  const TelemetryToolUsage({
    required this.serverName,
    required this.toolName,
    required this.calls,
    required this.failed,
    required this.denied,
    this.averageDurationMs,
  });

  final String serverName;
  final String toolName;
  final int calls;
  final int failed;
  final int denied;

  /// Null when no call recorded a duration — a tool whose calls were all
  /// denied never ran.
  final int? averageDurationMs;
}

class TelemetryModelUsage {
  const TelemetryModelUsage({
    required this.provider,
    required this.model,
    required this.runs,
    this.inputTokens,
    this.outputTokens,
    required this.runsWithoutUsage,
  });

  final String provider;
  final String model;
  final int runs;
  final int? inputTokens;
  final int? outputTokens;
  final int runsWithoutUsage;
}

class TelemetryExternalAgentUsage {
  const TelemetryExternalAgentUsage({
    required this.displayName,
    required this.endpointHost,
    required this.runs,
    this.inputTokens,
    this.outputTokens,
    required this.runsWithoutUsage,
  });

  final String displayName;

  /// Host only. Where it went, not how to get there.
  final String endpointHost;
  final int runs;
  final int? inputTokens;
  final int? outputTokens;
  final int runsWithoutUsage;
}

class TelemetryAutomationTotals {
  const TelemetryAutomationTotals({
    required this.runs,
    required this.completed,
    required this.failed,
    required this.unattendedApprovals,
  });

  final int runs;
  final int completed;
  final int failed;

  /// Approvals an automation's allowlist decided rather than a person.
  final int unattendedApprovals;
}

class TelemetryIntegrationTotals {
  const TelemetryIntegrationTotals({
    required this.connected,
    required this.revoked,
  });

  final int connected;
  final int revoked;
}

class TelemetryDailyActivity {
  const TelemetryDailyActivity({
    required this.date,
    required this.runs,
    required this.toolCalls,
    this.inputTokens,
    this.outputTokens,
  });

  /// YYYY-MM-DD, UTC — the day the rows are stored under.
  final String date;
  final int runs;
  final int toolCalls;
  final int? inputTokens;
  final int? outputTokens;
}
