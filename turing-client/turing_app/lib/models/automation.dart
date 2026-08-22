/// A saved prompt the backend sends on a schedule, without you starting it.
///
/// Every other run exists because someone typed something. This one does not,
/// which is why it carries [allowedTools]: a run nobody is watching cannot
/// stop and ask.
class Automation {
  const Automation({
    required this.automationId,
    required this.name,
    required this.prompt,
    required this.schedule,
    required this.enabled,
    required this.allowedTools,
    this.lastRunAt,
    this.nextRunAt,
    this.sessionId = '',
    this.lastRunId = '',
    this.lastRunStatus = '',
    this.lastRunError = '',
    this.lastOccurrenceFailureCode = '',
    this.lastOccurrenceFailedAt,
  });

  final String automationId;
  final String name;
  final String prompt;
  final AutomationSchedule schedule;
  final bool enabled;

  /// The tools this automation may run without asking, named one by one.
  /// Empty means it may run none of them — a tool that needs approval stops
  /// the run instead.
  final List<AutomationTool> allowedTools;

  final DateTime? lastRunAt;

  /// Null while disabled. A disabled automation has no next run, and showing
  /// one would be a claim about the future that is false.
  final DateTime? nextRunAt;

  /// The conversation this automation's runs land in. Empty until it has run
  /// once.
  final String sessionId;
  final String lastRunId;
  final String lastRunStatus;
  final String lastRunError;
  final String lastOccurrenceFailureCode;
  final DateTime? lastOccurrenceFailedAt;

  bool get hasRun => lastRunAt != null;
  bool get lastRunFailed => lastRunStatus == 'failed';
}

enum AutomationScheduleKind { interval, daily }

class AutomationSchedule {
  const AutomationSchedule.interval(this.intervalMinutes)
    : kind = AutomationScheduleKind.interval,
      dailyMinuteUtc = 0;

  const AutomationSchedule.daily(this.dailyMinuteUtc)
    : kind = AutomationScheduleKind.daily,
      intervalMinutes = 0;

  final AutomationScheduleKind kind;
  final int intervalMinutes;

  /// Minutes past midnight, UTC. Stored in UTC because the backend has no
  /// reliable notion of your zone; this client converts for display.
  final int dailyMinuteUtc;
}

class AutomationTool {
  const AutomationTool({required this.serverName, required this.toolName});

  final String serverName;
  final String toolName;

  @override
  bool operator ==(Object other) =>
      other is AutomationTool &&
      other.serverName == serverName &&
      other.toolName == toolName;

  @override
  int get hashCode => Object.hash(serverName, toolName);
}
