import 'package:flutter/material.dart';

import '../../constants/app_colors.dart';
import '../../models/automation.dart';
import '../../models/tool_descriptor.dart';
import '../../networking/api_client.dart';
import 'workspace_pages.dart';

/// Renders a schedule as a sentence, in the reader's own time zone.
///
/// Daily times are stored as a fixed minute of the UTC day, so the local time
/// they land at can shift by an hour across daylight saving. The editor says
/// so; here the job is only to state what it currently is.
String describeSchedule(AutomationSchedule schedule) {
  switch (schedule.kind) {
    case AutomationScheduleKind.interval:
      // A backend that cannot read a stored schedule sends nothing rather
      // than a guess, and "Every 0 minutes" would be a schedule that reads as
      // real. The backend switches such an automation off when it meets it.
      if (schedule.intervalMinutes < 1) return 'Schedule cannot be read';
      return 'Every ${describeMinutes(schedule.intervalMinutes)}';
    case AutomationScheduleKind.daily:
      return 'Every day at ${formatLocalDaily(schedule.dailyMinuteUtc)}';
  }
}

String describeMinutes(int minutes) {
  if (minutes < 60) {
    return minutes == 1 ? 'minute' : '$minutes minutes';
  }
  if (minutes % (60 * 24) == 0) {
    final days = minutes ~/ (60 * 24);
    return days == 1 ? 'day' : '$days days';
  }
  if (minutes % 60 == 0) {
    final hours = minutes ~/ 60;
    return hours == 1 ? 'hour' : '$hours hours';
  }
  final hours = minutes ~/ 60;
  return '${hours}h ${minutes % 60}m';
}

/// The local clock's offset from UTC on the day [reference] falls in.
///
/// Both conversions below take their offset from this one anchor. Anchoring
/// them separately — one on UTC midnight, one on local midnight — makes them
/// disagree by an hour on a daylight-saving transition day, and a schedule
/// that shifts every time the editor is opened and saved is worse than one
/// that is simply an hour off.
int _localOffsetMinutes(DateTime? reference) {
  final day = (reference ?? DateTime.now()).toUtc();
  return DateTime.utc(
    day.year,
    day.month,
    day.day,
  ).toLocal().timeZoneOffset.inMinutes;
}

int _wrapMinuteOfDay(int minutes) => ((minutes % 1440) + 1440) % 1440;

/// Converts a minute of the UTC day into the local wall clock. [reference]
/// exists so a test can pin the day rather than depending on when it runs.
TimeOfDay localTimeFromUtcMinute(int minuteUtc, {DateTime? reference}) {
  final local = _wrapMinuteOfDay(minuteUtc + _localOffsetMinutes(reference));
  return TimeOfDay(hour: local ~/ 60, minute: local % 60);
}

int utcMinuteFromLocalTime(TimeOfDay time, {DateTime? reference}) {
  return _wrapMinuteOfDay(
    time.hour * 60 + time.minute - _localOffsetMinutes(reference),
  );
}

String formatLocalDaily(int minuteUtc, {DateTime? reference}) {
  final time = localTimeFromUtcMinute(minuteUtc, reference: reference);
  final hour = time.hour.toString().padLeft(2, '0');
  final minute = time.minute.toString().padLeft(2, '0');
  return '$hour:$minute';
}

/// The sentence that IS the consent: exactly what this automation may do
/// without stopping to ask you.
String describeAllowlist(List<AutomationTool> tools) {
  if (tools.isEmpty) {
    return 'This automation may not run any tool that needs approval. If it '
        'tries, the run stops and says which tool it wanted.';
  }
  final names = tools.map(describeTool).join(', ');
  return 'Without asking you, this automation may run: $names. Nothing else — '
      'any other tool that needs approval stops the run.';
}

/// What is enforced is the (server, tool) pair, so the sentence has to name
/// both. Tool names are server-prefixed by convention, and repeating the
/// server for `files.update` would read as noise — so it is only spelled out
/// when the name does not already carry it.
String describeTool(AutomationTool tool) {
  if (tool.toolName.startsWith('${tool.serverName}.')) return tool.toolName;
  return '${tool.toolName} (from ${tool.serverName})';
}

String formatWhen(DateTime? when) {
  if (when == null) return 'never';
  final date =
      '${when.year}-${when.month.toString().padLeft(2, '0')}-'
      '${when.day.toString().padLeft(2, '0')}';
  final time =
      '${when.hour.toString().padLeft(2, '0')}:'
      '${when.minute.toString().padLeft(2, '0')}';
  return '$date $time';
}

/// Work that runs without you starting it.
class AutomationsPage extends StatefulWidget {
  const AutomationsPage({
    super.key,
    required this.apiClient,
    required this.onOpenSession,
  });

  final TuringApi apiClient;

  /// Reaching the conversation an automation produced is the whole point of
  /// the "Open conversation" affordance, so the shell hands the page a way to
  /// get there rather than the page pushing a route the user has to unwind.
  final void Function(String sessionId) onOpenSession;

  @override
  State<AutomationsPage> createState() => _AutomationsPageState();
}

class _AutomationsPageState extends State<AutomationsPage> {
  late Future<List<Automation>> _automations;

  @override
  void initState() {
    super.initState();
    _automations = widget.apiClient.listAutomations();
  }

  void _reload() {
    setState(() {
      _automations = widget.apiClient.listAutomations();
    });
  }

  Future<void> _edit({Automation? existing}) async {
    final saved = await showDialog<bool>(
      context: context,
      builder: (_) =>
          _AutomationEditor(apiClient: widget.apiClient, existing: existing),
    );
    if (saved == true) _reload();
  }

  Future<void> _toggle(Automation automation, bool enabled) async {
    try {
      await widget.apiClient.setAutomationEnabled(
        automationId: automation.automationId,
        enabled: enabled,
      );
      if (!mounted) return;
      _reload();
    } catch (error) {
      if (!mounted) return;
      // Reloaded as well as reported: the switch has already moved, and
      // leaving it where the user put it would show a state the backend does
      // not hold.
      _reload();
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('Could not change it: $error')));
    }
  }

  Future<void> _delete(Automation automation) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text('Delete "${automation.name}"?'),
        content: const Text(
          'It stops running from now on. The conversation it produced stays '
          'where it is, and a run already underway finishes with the '
          'permission it was given when it started.',
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(false),
            child: const Text('Cancel'),
          ),
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (confirmed != true) return;
    try {
      await widget.apiClient.deleteAutomation(
        automationId: automation.automationId,
      );
      if (!mounted) return;
      _reload();
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('Could not delete it: $error')));
    }
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return WorkspacePage(
      title: 'Automations',
      subtitle:
          'Work that runs without you starting it. Each one sends a prompt of '
          'yours on a schedule, into its own conversation. Because nobody is '
          'watching when it runs, an automation can only use the tools you '
          'name for it — anything else stops the run and says so.',
      child: FutureBuilder<List<Automation>>(
        future: _automations,
        builder: (context, snapshot) {
          if (snapshot.connectionState != ConnectionState.done) {
            return const WorkspaceLoading();
          }
          if (snapshot.hasError) {
            return WorkspaceNotice(
              icon: Icons.error_outline,
              title: 'Could not reach the backend',
              body: '${snapshot.error}',
              onRetry: _reload,
              tone: AppColors.danger,
            );
          }
          final automations = snapshot.data ?? const <Automation>[];
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Align(
                alignment: Alignment.centerLeft,
                child: FilledButton.icon(
                  onPressed: () => _edit(),
                  icon: const Icon(Icons.add, size: 18),
                  label: const Text('New automation'),
                ),
              ),
              const SizedBox(height: 18),
              if (automations.isEmpty)
                WorkspaceNotice(
                  icon: Icons.schedule_outlined,
                  title: 'No automations yet',
                  body:
                      'An automation is a prompt you save once and let run on '
                      'its own: a morning summary of what changed in the '
                      'sandbox, an hourly check of something you care about. '
                      'You choose what it may do unattended before it ever '
                      'runs.',
                )
              else
                for (final automation in automations)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 10),
                    child: _AutomationCard(
                      automation: automation,
                      palette: palette,
                      onEdit: () => _edit(existing: automation),
                      onDelete: () => _delete(automation),
                      onToggle: (enabled) => _toggle(automation, enabled),
                      onOpenSession: widget.onOpenSession,
                    ),
                  ),
            ],
          );
        },
      ),
    );
  }
}

class _AutomationCard extends StatelessWidget {
  const _AutomationCard({
    required this.automation,
    required this.palette,
    required this.onEdit,
    required this.onDelete,
    required this.onToggle,
    required this.onOpenSession,
  });

  final Automation automation;
  final AppPalette palette;
  final VoidCallback onEdit;
  final VoidCallback onDelete;
  final void Function(bool enabled) onToggle;
  final void Function(String sessionId) onOpenSession;

  /// The name and the controls, side by side when there is room and stacked
  /// when there is not.
  ///
  /// A switch plus two icon buttons is about 130 logical pixels, which leaves
  /// nothing for a name inside a card on a 320px phone. Ellipsising the name
  /// instead would hide the one thing that says which automation the delete
  /// button belongs to.
  Widget _header(BuildContext context) {
    final name = Text(
      automation.name,
      style: TextStyle(
        fontSize: 14.5,
        fontWeight: FontWeight.w600,
        color: palette.text,
      ),
    );
    final controls = Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Switch(
          value: automation.enabled,
          onChanged: onToggle,
          thumbIcon: WidgetStateProperty.all(
            Icon(
              automation.enabled ? Icons.play_arrow : Icons.pause,
              size: 14,
            ),
          ),
        ),
        IconButton(
          icon: const Icon(Icons.edit_outlined, size: 17),
          tooltip: 'Edit automation',
          color: palette.textMuted,
          visualDensity: VisualDensity.compact,
          onPressed: onEdit,
        ),
        IconButton(
          icon: const Icon(Icons.delete_outline, size: 17),
          tooltip: 'Delete automation',
          color: palette.textMuted,
          visualDensity: VisualDensity.compact,
          onPressed: onDelete,
        ),
      ],
    );
    return LayoutBuilder(
      builder: (context, constraints) {
        if (constraints.maxWidth < 280) {
          return Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [name, controls],
          );
        }
        return Row(children: [Expanded(child: name), controls]);
      },
    );
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: palette.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: palette.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _header(context),
          const SizedBox(height: 6),
          Text(
            automation.prompt,
            maxLines: 3,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(
              fontSize: 13.5,
              height: 1.55,
              color: palette.textMuted,
            ),
          ),
          const SizedBox(height: 12),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              _Fact(
                icon: Icons.repeat,
                label: describeSchedule(automation.schedule),
                palette: palette,
              ),
              _Fact(
                icon: Icons.history,
                label: 'Last run ${formatWhen(automation.lastRunAt)}',
                palette: palette,
              ),
              _Fact(
                icon: Icons.event_outlined,
                // A disabled automation is not "next: never", it is paused —
                // and saying "never" would read as broken.
                label: automation.enabled
                    ? 'Next ${formatWhen(automation.nextRunAt)}'
                    : 'Paused, no next run',
                palette: palette,
              ),
            ],
          ),
          const SizedBox(height: 12),
          Text(
            describeAllowlist(automation.allowedTools),
            style: TextStyle(
              fontSize: 12.5,
              height: 1.5,
              color: palette.textMuted,
            ),
          ),
          if (automation.lastRunFailed && automation.lastRunError.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 12),
              child: Container(
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  color: AppColors.danger.withValues(alpha: 0.1),
                  borderRadius: BorderRadius.circular(9),
                ),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Icon(
                      Icons.error_outline,
                      size: 15,
                      color: AppColors.danger,
                    ),
                    const SizedBox(width: 9),
                    Expanded(
                      child: Text(
                        'The last run failed. ${automation.lastRunError}',
                        style: TextStyle(
                          fontSize: 12.5,
                          height: 1.5,
                          color: palette.text,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          if (automation.sessionId.isNotEmpty)
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton.icon(
                onPressed: () => onOpenSession(automation.sessionId),
                icon: const Icon(Icons.forum_outlined, size: 16),
                label: const Text('Open conversation'),
              ),
            ),
        ],
      ),
    );
  }
}

class _Fact extends StatelessWidget {
  const _Fact({required this.icon, required this.label, required this.palette});

  final IconData icon;
  final String label;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 5),
      decoration: BoxDecoration(
        color: palette.raised,
        borderRadius: BorderRadius.circular(20),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 13, color: palette.textMuted),
          const SizedBox(width: 6),
          // Flexible, because a Wrap hands its children the full line width
          // and these labels carry timestamps: on a phone a single one is
          // wider than the card, and a fixed Text would overflow rather than
          // wrap.
          Flexible(
            child: Text(
              label,
              style: TextStyle(fontSize: 11.5, color: palette.textMuted),
            ),
          ),
        ],
      ),
    );
  }
}

class _AutomationEditor extends StatefulWidget {
  const _AutomationEditor({required this.apiClient, this.existing});

  final TuringApi apiClient;
  final Automation? existing;

  @override
  State<_AutomationEditor> createState() => _AutomationEditorState();
}

class _AutomationEditorState extends State<_AutomationEditor> {
  late final TextEditingController _name = TextEditingController(
    text: widget.existing?.name ?? '',
  );
  late final TextEditingController _prompt = TextEditingController(
    text: widget.existing?.prompt ?? '',
  );
  late final TextEditingController _interval = TextEditingController(
    text:
        (widget.existing?.schedule.kind == AutomationScheduleKind.interval
                ? widget.existing!.schedule.intervalMinutes
                : 60)
            .toString(),
  );
  late AutomationScheduleKind _kind =
      widget.existing?.schedule.kind ?? AutomationScheduleKind.interval;
  late TimeOfDay _dailyTime = localTimeFromUtcMinute(
    widget.existing?.schedule.kind == AutomationScheduleKind.daily
        ? widget.existing!.schedule.dailyMinuteUtc
        : 9 * 60,
  );
  late final Set<AutomationTool> _allowed = {...?widget.existing?.allowedTools};
  late Future<List<ToolDescriptor>> _tools;
  bool _saving = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _tools = widget.apiClient.listTools();
  }

  @override
  void dispose() {
    _name.dispose();
    _prompt.dispose();
    _interval.dispose();
    super.dispose();
  }

  AutomationSchedule? _schedule() {
    switch (_kind) {
      case AutomationScheduleKind.interval:
        final minutes = int.tryParse(_interval.text.trim());
        // Bounded here as well as on the server so the reason appears next to
        // the field rather than as a failed round trip.
        if (minutes == null || minutes < 1 || minutes > 7 * 24 * 60) {
          return null;
        }
        return AutomationSchedule.interval(minutes);
      case AutomationScheduleKind.daily:
        return AutomationSchedule.daily(utcMinuteFromLocalTime(_dailyTime));
    }
  }

  Future<void> _save() async {
    if (_saving) return;
    final name = _name.text.trim();
    final prompt = _prompt.text.trim();
    if (name.isEmpty || prompt.isEmpty) {
      setState(() => _error = 'An automation needs both a name and a prompt.');
      return;
    }
    final schedule = _schedule();
    if (schedule == null) {
      setState(
        () => _error =
            'An interval must be a whole number of minutes, at least 1 and at '
            'most 10080 (a week).',
      );
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final existing = widget.existing;
      if (existing == null) {
        await widget.apiClient.createAutomation(
          name: name,
          prompt: prompt,
          schedule: schedule,
          // New automations start enabled: saving one you then have to switch
          // on reads as a bug the first time it does not run.
          enabled: true,
          allowedTools: _allowed.toList(growable: false),
        );
      } else {
        await widget.apiClient.updateAutomation(
          automationId: existing.automationId,
          name: name,
          prompt: prompt,
          schedule: schedule,
          allowedTools: _allowed.toList(growable: false),
        );
      }
      if (!mounted) return;
      Navigator.of(context).pop(true);
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _saving = false;
        _error = '$error';
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    // The dialog has to fit a phone as well as a desktop window; a fixed width
    // would overflow below about 540px.
    final width = MediaQuery.sizeOf(context).width;
    return AlertDialog(
      title: Text(
        widget.existing == null ? 'New automation' : 'Edit automation',
      ),
      content: SizedBox(
        width: width < 540 ? width - 80 : 460,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              TextField(
                controller: _name,
                autofocus: true,
                decoration: const InputDecoration(
                  labelText: 'Name',
                  helperText: 'How you will recognise it in a list',
                ),
              ),
              const SizedBox(height: 16),
              TextField(
                controller: _prompt,
                minLines: 3,
                maxLines: 8,
                decoration: const InputDecoration(
                  labelText: 'Prompt',
                  alignLabelWithHint: true,
                  helperText: 'Sent as if you had typed it, every time it runs',
                ),
              ),
              const SizedBox(height: 20),
              Text(
                'When it runs',
                style: TextStyle(
                  fontSize: 13,
                  fontWeight: FontWeight.w600,
                  color: palette.text,
                ),
              ),
              const SizedBox(height: 8),
              SegmentedButton<AutomationScheduleKind>(
                segments: const [
                  ButtonSegment(
                    value: AutomationScheduleKind.interval,
                    label: Text('Every'),
                  ),
                  ButtonSegment(
                    value: AutomationScheduleKind.daily,
                    label: Text('Daily'),
                  ),
                ],
                selected: {_kind},
                onSelectionChanged: (selection) =>
                    setState(() => _kind = selection.first),
              ),
              const SizedBox(height: 12),
              if (_kind == AutomationScheduleKind.interval)
                TextField(
                  controller: _interval,
                  keyboardType: TextInputType.number,
                  decoration: const InputDecoration(
                    labelText: 'Minutes between runs',
                    helperText: 'At least 1, at most 10080 (a week)',
                  ),
                )
              else
                Row(
                  children: [
                    Expanded(
                      child: Text(
                        'At ${_dailyTime.hour.toString().padLeft(2, '0')}:'
                        '${_dailyTime.minute.toString().padLeft(2, '0')} '
                        'your time',
                        style: TextStyle(fontSize: 13.5, color: palette.text),
                      ),
                    ),
                    TextButton(
                      onPressed: () async {
                        final picked = await showTimePicker(
                          context: context,
                          initialTime: _dailyTime,
                        );
                        if (picked != null) {
                          setState(() => _dailyTime = picked);
                        }
                      },
                      child: const Text('Change'),
                    ),
                  ],
                ),
              if (_kind == AutomationScheduleKind.daily) ...[
                const SizedBox(height: 6),
                Text(
                  'Kept as a fixed time in UTC, so daylight saving can shift '
                  'what that is locally by an hour.',
                  style: TextStyle(fontSize: 12, color: palette.textMuted),
                ),
              ],
              const SizedBox(height: 20),
              Text(
                'What it may do without asking',
                style: TextStyle(
                  fontSize: 13,
                  fontWeight: FontWeight.w600,
                  color: palette.text,
                ),
              ),
              const SizedBox(height: 6),
              Text(
                'These are the tools that would normally stop and ask you. '
                'Nobody is there to answer when an automation runs, so tick '
                'only the ones you are willing to have run on their own. '
                'Ticking one here does not affect any conversation you drive '
                'by hand.',
                style: TextStyle(
                  fontSize: 12.5,
                  height: 1.5,
                  color: palette.textMuted,
                ),
              ),
              const SizedBox(height: 10),
              _AllowlistPicker(
                tools: _tools,
                selected: _allowed,
                onChanged: (tool, checked) => setState(() {
                  if (checked) {
                    _allowed.add(tool);
                  } else {
                    _allowed.remove(tool);
                  }
                }),
              ),
              const SizedBox(height: 14),
              // The whole consent, restated as one sentence, right above Save.
              Container(
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  color: palette.raised,
                  borderRadius: BorderRadius.circular(9),
                  border: Border.all(color: palette.border),
                ),
                child: Text(
                  describeAllowlist(_allowed.toList(growable: false)),
                  style: TextStyle(
                    fontSize: 12.5,
                    height: 1.5,
                    color: palette.text,
                  ),
                ),
              ),
              if (_error != null) ...[
                const SizedBox(height: 14),
                Text(
                  _error!,
                  style: TextStyle(fontSize: 13, color: AppColors.danger),
                ),
              ],
            ],
          ),
        ),
      ),
      actions: [
        TextButton(
          onPressed: _saving ? null : () => Navigator.of(context).pop(false),
          child: const Text('Cancel'),
        ),
        FilledButton(
          onPressed: _saving ? null : _save,
          child: Text(_saving ? 'Saving...' : 'Save'),
        ),
      ],
    );
  }
}

/// The tools that would otherwise stop and ask, with a tick against each.
///
/// Only approval-gated tools appear: a safe tool needs no permission, and
/// listing it would invite the user to grant something that was never
/// withheld.
class _AllowlistPicker extends StatelessWidget {
  const _AllowlistPicker({
    required this.tools,
    required this.selected,
    required this.onChanged,
  });

  final Future<List<ToolDescriptor>> tools;
  final Set<AutomationTool> selected;
  final void Function(AutomationTool tool, bool checked) onChanged;

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return FutureBuilder<List<ToolDescriptor>>(
      future: tools,
      builder: (context, snapshot) {
        if (snapshot.connectionState != ConnectionState.done) {
          return const Padding(
            padding: EdgeInsets.symmetric(vertical: 12),
            child: SizedBox.square(
              dimension: 18,
              child: CircularProgressIndicator(strokeWidth: 2),
            ),
          );
        }
        if (snapshot.hasError) {
          // Not "no tools": claiming an empty list here would invite someone
          // to save an allowlist they never got to see.
          return Text(
            'The list of tools could not be read, so this cannot say what is '
            'available to allow. Existing ticks are unchanged.',
            style: TextStyle(fontSize: 12.5, color: AppColors.danger),
          );
        }
        final gated =
            (snapshot.data ?? const <ToolDescriptor>[])
                .where((tool) => tool.policy == ToolPolicy.approvalRequired)
                .map(
                  (tool) => AutomationTool(
                    serverName: tool.serverName,
                    toolName: tool.toolName,
                  ),
                )
                .toList();
        // An entry for a tool no server currently offers is still enforced if
        // that tool comes back, and is still sent on every save. Hiding it
        // would make it un-untickable — a permission the user cannot withdraw
        // because they cannot see it.
        final stale = selected.where((tool) => !gated.contains(tool)).toList();
        final rows = [...gated, ...stale]
          ..sort((a, b) => a.toolName.compareTo(b.toolName));
        if (rows.isEmpty) {
          return Text(
            'No tool currently needs approval, so there is nothing to allow '
            'in advance.',
            style: TextStyle(fontSize: 12.5, color: palette.textMuted),
          );
        }
        return Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            for (final tool in rows)
              CheckboxListTile(
                dense: true,
                contentPadding: EdgeInsets.zero,
                controlAffinity: ListTileControlAffinity.leading,
                value: selected.contains(tool),
                onChanged: (checked) => onChanged(tool, checked ?? false),
                title: Text(
                  tool.toolName,
                  style: TextStyle(
                    fontSize: 13,
                    fontFamily: 'monospace',
                    color: palette.text,
                  ),
                ),
                subtitle: Text(
                  stale.contains(tool)
                      ? 'from ${tool.serverName} — not offered right now, but '
                            'still allowed if it comes back'
                      : 'from ${tool.serverName}',
                  style: TextStyle(fontSize: 11.5, color: palette.textMuted),
                ),
              ),
          ],
        );
      },
    );
  }
}
