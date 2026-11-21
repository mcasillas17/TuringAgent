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

  /// The only tools this automation may use, safe reads included, named one
  /// by one; it runs them without asking. Empty means it may use none — any
  /// tool it calls stops the run instead.
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

/// The owner's quiet switch for unattended work.
///
/// While it is on, no automation fires and no unattended run starts: queued
/// automation runs are cancelled, and one that would go back to the queue is
/// cancelled instead. A run already going finishes. Turning it off starts
/// nothing by itself; each automation runs next at its scheduled time.
class UnattendedQuiet {
  const UnattendedQuiet({
    required this.quiet,
    required this.revision,
    this.unavailable = false,
  });

  /// True while no unattended work starts, which includes while
  /// [unavailable]. A setting never written is off.
  final bool quiet;

  /// The revision a write must name. Every change advances it, and the backend
  /// refuses a write naming an older one, so a stale screen cannot undo a
  /// change it never saw.
  final int revision;

  /// The stored value cannot be read, which counts as on. [revision] is still
  /// real: a write naming it repairs the setting, to either value.
  final bool unavailable;
}

/// The backend's answer to a quiet write.
class UnattendedQuietWrite {
  const UnattendedQuietWrite({required this.setting, required this.stale});

  /// The setting as it now stands: the one written, or, when [stale], the
  /// current one, unchanged.
  final UnattendedQuiet setting;

  /// The revision the write named was no longer current, so nothing was
  /// written.
  final bool stale;
}
