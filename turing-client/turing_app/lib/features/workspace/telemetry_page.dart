import 'package:flutter/material.dart';

import '../../constants/app_colors.dart';
import '../../models/telemetry.dart';
import '../../networking/api_client.dart';
import 'workspace_pages.dart';

/// The windows this page offers. Kept short on purpose: the question is
/// "what has this been doing lately", and a menu of twelve spans answers it
/// worse than three.
const List<int> telemetryWindowChoices = [7, 30, 90];

/// What is written wherever a number was never measured.
///
/// Deliberately not "0" and deliberately not blank. A zero is a measurement
/// and someone will act on it; a blank reads as a rendering bug. This says the
/// thing that is actually true.
const String telemetryUnknownLabel = 'not reported';

/// Formats a count with thousands separators, by hand — the app carries no
/// internationalisation package and every other date and number in it is
/// assembled the same way.
String formatTelemetryCount(int value) {
  final digits = value.abs().toString();
  final buffer = StringBuffer();
  for (var index = 0; index < digits.length; index++) {
    if (index > 0 && (digits.length - index) % 3 == 0) buffer.write(',');
    buffer.write(digits[index]);
  }
  return value < 0 ? '-$buffer' : buffer.toString();
}

/// A measured duration, or [telemetryUnknownLabel] when there was nothing to
/// measure. Sub-second values stay in milliseconds because that is the scale a
/// local tool call actually runs at.
String formatTelemetryDuration(int? milliseconds) {
  if (milliseconds == null) return telemetryUnknownLabel;
  if (milliseconds < 1000) return '$milliseconds ms';
  final seconds = milliseconds / 1000;
  if (seconds < 10) return '${seconds.toStringAsFixed(1)} s';
  if (seconds < 60) return '${seconds.round()} s';
  final minutes = seconds / 60;
  return '${minutes.toStringAsFixed(1)} min';
}

/// A token count, or the honest absence of one.
String formatTelemetryTokens(int? tokens) {
  if (tokens == null) return telemetryUnknownLabel;
  return formatTelemetryCount(tokens);
}

/// The sentence under the token totals. This is the whole provenance story in
/// one line, and it is the reason the page can be trusted: the totals never
/// appear without saying how much of the work they cover.
String describeTokenProvenance(TelemetryTokenTotals tokens) {
  // "Completed", not "finished": a run that failed also finished, and the
  // counters behind this sentence cover completed runs only. The sentence
  // whose entire job is precision cannot be the loose one on the page.
  final completed = tokens.runsWithUsage + tokens.runsWithoutUsage;
  if (completed == 0) {
    return 'No conversation completed in this window, so there was nothing to '
        'measure.';
  }
  if (!tokens.reported) {
    return 'None of the $completed completed ${_runWord(completed)} reported '
        'token usage. The counts are unknown, and this page will not guess '
        'them.';
  }
  if (!tokens.partial) {
    return 'Measured from all $completed completed ${_runWord(completed)}.';
  }
  return 'Measured from ${tokens.runsWithUsage} of $completed completed '
      '${_runWord(completed)}. The other ${tokens.runsWithoutUsage} reported '
      'no usage, so the real totals are higher by an unknown amount.';
}

String _runWord(int count) => count == 1 ? 'conversation' : 'conversations';

/// The window, spelled out under the heading so a screenshot of these numbers
/// still says what they cover.
String describeTelemetryWindow(TelemetryWindow window) {
  return 'Last ${window.days} days — '
      '${_formatDate(window.start)} to ${_formatDate(window.end)} UTC';
}

String _formatDate(DateTime when) {
  final utc = when.toUtc();
  return '${utc.year}-${utc.month.toString().padLeft(2, '0')}-'
      '${utc.day.toString().padLeft(2, '0')}';
}

/// What this assistant has been doing, measured on this machine.
class TelemetryPage extends StatefulWidget {
  const TelemetryPage({super.key, required this.apiClient});

  final TuringApi apiClient;

  @override
  State<TelemetryPage> createState() => _TelemetryPageState();
}

class _TelemetryPageState extends State<TelemetryPage> {
  late Future<TelemetrySummary> _summary;
  int _windowDays = telemetryWindowChoices.first;

  @override
  void initState() {
    super.initState();
    _summary = widget.apiClient.getTelemetrySummary(windowDays: _windowDays);
  }

  void _reload() {
    setState(() {
      _summary = widget.apiClient.getTelemetrySummary(windowDays: _windowDays);
    });
  }

  void _selectWindow(int days) {
    if (days == _windowDays) return;
    setState(() {
      _windowDays = days;
      _summary = widget.apiClient.getTelemetrySummary(windowDays: days);
    });
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return WorkspacePage(
      title: 'Telemetry',
      subtitle:
          'What this assistant has been doing, counted on this machine from '
          'its own records. Nothing here is sent anywhere, and nothing here '
          'is content — no message, no prompt, no tool argument.',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Align(
            alignment: Alignment.centerLeft,
            child: _WindowPicker(
              selected: _windowDays,
              onSelect: _selectWindow,
              palette: palette,
            ),
          ),
          const SizedBox(height: 18),
          FutureBuilder<TelemetrySummary>(
            future: _summary,
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
              final summary = snapshot.data;
              if (summary == null) {
                return WorkspaceNotice(
                  icon: Icons.error_outline,
                  title: 'Could not reach the backend',
                  body: 'The backend returned no summary.',
                  onRetry: _reload,
                  tone: AppColors.danger,
                );
              }
              return _Summary(summary: summary, palette: palette);
            },
          ),
        ],
      ),
    );
  }
}

class _WindowPicker extends StatelessWidget {
  const _WindowPicker({
    required this.selected,
    required this.onSelect,
    required this.palette,
  });

  final int selected;
  final void Function(int days) onSelect;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
    // A Wrap rather than a SegmentedButton: three segments plus their labels
    // do not fit beside each other at 300 logical pixels, and a segmented
    // control has no way to wrap.
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        for (final days in telemetryWindowChoices)
          _WindowChoice(
            days: days,
            selected: days == selected,
            onTap: () => onSelect(days),
            palette: palette,
          ),
      ],
    );
  }
}

class _WindowChoice extends StatelessWidget {
  const _WindowChoice({
    required this.days,
    required this.selected,
    required this.onTap,
    required this.palette,
  });

  final int days;
  final bool selected;
  final VoidCallback onTap;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: selected
          ? AppColors.brand.withValues(alpha: 0.14)
          : palette.raised,
      borderRadius: BorderRadius.circular(8),
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(8),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
          child: Text(
            '$days days',
            style: TextStyle(
              fontSize: 13,
              fontWeight: selected ? FontWeight.w600 : FontWeight.w400,
              color: selected ? palette.text : palette.textMuted,
            ),
          ),
        ),
      ),
    );
  }
}

class _Summary extends StatelessWidget {
  const _Summary({required this.summary, required this.palette});

  final TelemetrySummary summary;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          describeTelemetryWindow(summary.window),
          style: TextStyle(fontSize: 12.5, color: palette.textMuted),
        ),
        const SizedBox(height: 16),
        if (!summary.hasActivity) ...[
          const WorkspaceNotice(
            icon: Icons.insights_outlined,
            title: 'Nothing happened in this window',
            body:
                'No conversation ran and no tool was called. The counts below '
                'are zero because nothing happened, not because nothing was '
                'recorded.',
          ),
          const SizedBox(height: 18),
        ],
        _TokenCard(tokens: summary.tokens, palette: palette),
        const SizedBox(height: 18),
        _Section(
          title: 'Conversations',
          palette: palette,
          child: Wrap(
            spacing: 10,
            runSpacing: 10,
            children: [
              _Stat(
                label: 'Runs',
                value: formatTelemetryCount(summary.runs.total),
                palette: palette,
              ),
              _Stat(
                label: 'Completed',
                value: formatTelemetryCount(summary.runs.completed),
                palette: palette,
              ),
              _Stat(
                label: 'Failed',
                value: formatTelemetryCount(summary.runs.failed),
                palette: palette,
                tone: summary.runs.failed > 0 ? AppColors.danger : null,
              ),
              _Stat(
                label: 'Cancelled',
                value: formatTelemetryCount(summary.runs.cancelled),
                palette: palette,
              ),
              _Stat(
                label: 'Still running',
                value: formatTelemetryCount(summary.runs.inFlight),
                palette: palette,
              ),
              _Stat(
                label: 'Average length',
                value: formatTelemetryDuration(summary.runs.averageDurationMs),
                palette: palette,
                derived: true,
                unknown: summary.runs.averageDurationMs == null,
              ),
            ],
          ),
        ),
        const SizedBox(height: 18),
        _Section(
          title: 'Activity by day',
          palette: palette,
          child: _DailyChart(daily: summary.daily, palette: palette),
        ),
        const SizedBox(height: 18),
        _Section(
          title: 'Tools used most',
          palette: palette,
          child: summary.tools.isEmpty
              ? _Empty(
                  text: 'No tool was called in this window.',
                  palette: palette,
                )
              : Column(
                  children: [
                    for (final tool in summary.tools)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 10),
                        child: _ToolRow(tool: tool, palette: palette),
                      ),
                  ],
                ),
        ),
        const SizedBox(height: 18),
        _Section(
          title: 'Models',
          palette: palette,
          child: summary.models.isEmpty
              ? _Empty(
                  text: 'No conversation ran in this window.',
                  palette: palette,
                )
              : Column(
                  children: [
                    for (final model in summary.models)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 10),
                        child: _ModelRow(model: model, palette: palette),
                      ),
                  ],
                ),
        ),
        const SizedBox(height: 18),
        _Section(
          title: 'What left this machine',
          palette: palette,
          child: summary.externalAgents.isEmpty
              ? _Empty(
                  text:
                      'Nothing. Every conversation in this window was answered '
                      'by a model running here.',
                  palette: palette,
                )
              : Column(
                  children: [
                    for (final agent in summary.externalAgents)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 10),
                        child: _ExternalAgentRow(
                          agent: agent,
                          palette: palette,
                        ),
                      ),
                  ],
                ),
        ),
        const SizedBox(height: 18),
        _Section(
          title: 'Automations',
          palette: palette,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Wrap(
                spacing: 10,
                runSpacing: 10,
                children: [
                  _Stat(
                    label: 'Unattended runs',
                    value: formatTelemetryCount(summary.automations.runs),
                    palette: palette,
                  ),
                  _Stat(
                    label: 'Completed',
                    value: formatTelemetryCount(summary.automations.completed),
                    palette: palette,
                  ),
                  _Stat(
                    label: 'Failed',
                    value: formatTelemetryCount(summary.automations.failed),
                    palette: palette,
                    tone: summary.automations.failed > 0
                        ? AppColors.danger
                        : null,
                  ),
                  _Stat(
                    label: 'Approvals you did not give',
                    value: formatTelemetryCount(
                      summary.automations.unattendedApprovals,
                    ),
                    palette: palette,
                    tone: summary.automations.unattendedApprovals > 0
                        ? AppColors.warning
                        : null,
                  ),
                ],
              ),
              if (summary.automations.unattendedApprovals > 0) ...[
                const SizedBox(height: 12),
                _Note(
                  text:
                      'An automation approved its own tool calls from the '
                      'allowlist you gave it, without asking at the time.',
                  palette: palette,
                ),
              ],
            ],
          ),
        ),
        const SizedBox(height: 18),
        _Section(
          title: 'Connected accounts',
          palette: palette,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Wrap(
                spacing: 10,
                runSpacing: 10,
                children: [
                  _Stat(
                    label: 'Connected',
                    value: formatTelemetryCount(summary.integrations.connected),
                    palette: palette,
                  ),
                  _Stat(
                    label: 'Revoked',
                    value: formatTelemetryCount(summary.integrations.revoked),
                    palette: palette,
                  ),
                ],
              ),
              const SizedBox(height: 12),
              _Note(
                text:
                    'These are counts of accounts held right now, not activity '
                    'in this window. Nothing in a conversation reads a '
                    'connection yet, so there is no usage to measure.',
                palette: palette,
              ),
            ],
          ),
        ),
      ],
    );
  }
}

/// Tokens, given a card of their own because they are the number the user
/// asked for and the number most easily misread.
class _TokenCard extends StatelessWidget {
  const _TokenCard({required this.tokens, required this.palette});

  final TelemetryTokenTotals tokens;
  final AppPalette palette;

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
          Text(
            'Tokens',
            style: TextStyle(
              fontSize: 14.5,
              fontWeight: FontWeight.w600,
              color: palette.text,
            ),
          ),
          const SizedBox(height: 12),
          Wrap(
            spacing: 10,
            runSpacing: 10,
            children: [
              _Stat(
                label: 'Sent to models',
                value: formatTelemetryTokens(tokens.inputTokens),
                palette: palette,
                unknown: tokens.inputTokens == null,
              ),
              _Stat(
                label: 'Generated',
                value: formatTelemetryTokens(tokens.outputTokens),
                palette: palette,
                unknown: tokens.outputTokens == null,
              ),
            ],
          ),
          const SizedBox(height: 12),
          _Note(text: describeTokenProvenance(tokens), palette: palette),
        ],
      ),
    );
  }
}

/// The one data mark on the page.
///
/// Drawn with laid-out boxes rather than a painter, so it inherits the same
/// overflow behaviour as everything else and survives a 300-pixel window.
/// Coloured with the brand accent, which is the palette's only non-semantic
/// one: `success` or `danger` here would put a judgement on a bar that is
/// only reporting how busy a day was.
class _DailyChart extends StatelessWidget {
  const _DailyChart({required this.daily, required this.palette});

  final List<TelemetryDailyActivity> daily;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
    if (daily.isEmpty) {
      return _Empty(text: 'No days to show.', palette: palette);
    }
    var busiest = 0;
    for (final day in daily) {
      if (day.runs > busiest) busiest = day.runs;
    }
    if (busiest == 0) {
      return _Empty(
        text: 'No conversation ran on any day in this window.',
        palette: palette,
      );
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SizedBox(
          height: 90,
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              for (final day in daily)
                Expanded(
                  child: Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 1.5),
                    child: Align(
                      alignment: Alignment.bottomCenter,
                      child: FractionallySizedBox(
                        // A day with no runs draws nothing at all. A minimum
                        // stub would make an empty day look like a small one.
                        heightFactor: day.runs / busiest,
                        child: Container(
                          decoration: BoxDecoration(
                            color: AppColors.brand,
                            borderRadius: const BorderRadius.vertical(
                              top: Radius.circular(2),
                            ),
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
            ],
          ),
        ),
        const SizedBox(height: 6),
        Container(height: 1, color: palette.border),
        const SizedBox(height: 8),
        // Only the ends are labelled. One label per day is unreadable at any
        // width this app has to survive.
        Row(
          mainAxisAlignment: MainAxisAlignment.spaceBetween,
          children: [
            Flexible(
              child: Text(
                daily.first.date,
                style: TextStyle(fontSize: 11.5, color: palette.textMuted),
              ),
            ),
            Flexible(
              child: Text(
                'busiest day: ${formatTelemetryCount(busiest)} runs',
                textAlign: TextAlign.center,
                style: TextStyle(fontSize: 11.5, color: palette.textMuted),
              ),
            ),
            Flexible(
              child: Text(
                daily.last.date,
                textAlign: TextAlign.end,
                style: TextStyle(fontSize: 11.5, color: palette.textMuted),
              ),
            ),
          ],
        ),
      ],
    );
  }
}

class _ToolRow extends StatelessWidget {
  const _ToolRow({required this.tool, required this.palette});

  final TelemetryToolUsage tool;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
    return _Row(
      palette: palette,
      title: tool.toolName,
      titleIsCode: true,
      subtitle: 'from ${tool.serverName}',
      facts: [
        _Fact(
          icon: Icons.play_arrow_outlined,
          text: '${formatTelemetryCount(tool.calls)} calls',
          palette: palette,
        ),
        if (tool.failed > 0)
          _Fact(
            icon: Icons.error_outline,
            text: '${formatTelemetryCount(tool.failed)} failed',
            palette: palette,
            tone: AppColors.danger,
          ),
        if (tool.denied > 0)
          _Fact(
            icon: Icons.block_outlined,
            text: '${formatTelemetryCount(tool.denied)} denied',
            palette: palette,
          ),
        _Fact(
          icon: Icons.timer_outlined,
          text: 'avg ${formatTelemetryDuration(tool.averageDurationMs)}',
          palette: palette,
          unknown: tool.averageDurationMs == null,
        ),
      ],
    );
  }
}

class _ModelRow extends StatelessWidget {
  const _ModelRow({required this.model, required this.palette});

  final TelemetryModelUsage model;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
    return _Row(
      palette: palette,
      title: model.model,
      titleIsCode: true,
      subtitle: model.provider,
      facts: [
        _Fact(
          icon: Icons.forum_outlined,
          text: '${formatTelemetryCount(model.runs)} runs',
          palette: palette,
        ),
        ..._tokenFacts(
          palette: palette,
          inputTokens: model.inputTokens,
          outputTokens: model.outputTokens,
          runsWithoutUsage: model.runsWithoutUsage,
        ),
      ],
    );
  }
}

/// The in/out pair and the caveat that has to travel with them.
///
/// Shared so the two places that show a token total cannot drift apart: a
/// panel that prints a total without saying how many runs it left out is
/// exactly the dishonesty the top of this page is careful to avoid, and
/// "What left this machine" is the last panel that should be allowed it.
List<Widget> _tokenFacts({
  required AppPalette palette,
  required int? inputTokens,
  required int? outputTokens,
  required int runsWithoutUsage,
}) {
  return [
    _Fact(
      icon: Icons.south_west,
      text: 'in ${formatTelemetryTokens(inputTokens)}',
      palette: palette,
      unknown: inputTokens == null,
    ),
    _Fact(
      icon: Icons.north_east,
      text: 'out ${formatTelemetryTokens(outputTokens)}',
      palette: palette,
      unknown: outputTokens == null,
    ),
    if (runsWithoutUsage > 0)
      _Fact(
        icon: Icons.help_outline,
        text:
            '${formatTelemetryCount(runsWithoutUsage)} runs reported no usage',
        palette: palette,
        unknown: true,
      ),
  ];
}

class _ExternalAgentRow extends StatelessWidget {
  const _ExternalAgentRow({required this.agent, required this.palette});

  final TelemetryExternalAgentUsage agent;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
    return _Row(
      palette: palette,
      title: agent.displayName,
      subtitle: agent.endpointHost.isEmpty
          ? 'endpoint not recorded'
          : agent.endpointHost,
      facts: [
        _Fact(
          icon: Icons.outbound_outlined,
          text: '${formatTelemetryCount(agent.runs)} runs sent',
          palette: palette,
        ),
        ..._tokenFacts(
          palette: palette,
          inputTokens: agent.inputTokens,
          outputTokens: agent.outputTokens,
          runsWithoutUsage: agent.runsWithoutUsage,
        ),
      ],
    );
  }
}

class _Row extends StatelessWidget {
  const _Row({
    required this.palette,
    required this.title,
    required this.subtitle,
    required this.facts,
    this.titleIsCode = false,
  });

  final AppPalette palette;
  final String title;
  final String subtitle;
  final List<Widget> facts;
  final bool titleIsCode;

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
          Text(
            title,
            style: TextStyle(
              fontSize: 14.5,
              fontWeight: FontWeight.w600,
              color: palette.text,
              fontFamily: titleIsCode ? 'monospace' : null,
            ),
          ),
          const SizedBox(height: 4),
          Text(
            subtitle,
            style: TextStyle(fontSize: 12.5, color: palette.textMuted),
          ),
          const SizedBox(height: 12),
          // A Wrap hands each child the full line width, so every fact has to
          // be able to shrink or it overflows instead of wrapping.
          Wrap(spacing: 8, runSpacing: 8, children: facts),
        ],
      ),
    );
  }
}

class _Section extends StatelessWidget {
  const _Section({
    required this.title,
    required this.child,
    required this.palette,
  });

  final String title;
  final Widget child;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          title,
          style: TextStyle(
            fontSize: 13,
            fontWeight: FontWeight.w600,
            color: palette.text,
          ),
        ),
        const SizedBox(height: 10),
        child,
      ],
    );
  }
}

/// One number and what it is.
///
/// [unknown] means the number was never measured, and it changes what is drawn
/// rather than only how: the value renders in muted text so an absent figure
/// never carries the visual weight of a measured one. [derived] marks a figure
/// computed from other rows rather than recorded directly.
class _Stat extends StatelessWidget {
  const _Stat({
    required this.label,
    required this.value,
    required this.palette,
    this.tone,
    this.unknown = false,
    this.derived = false,
  });

  final String label;
  final String value;
  final AppPalette palette;
  final Color? tone;
  final bool unknown;
  final bool derived;

  @override
  Widget build(BuildContext context) {
    return ConstrainedBox(
      constraints: const BoxConstraints(minWidth: 120),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
        decoration: BoxDecoration(
          color: palette.raised,
          borderRadius: BorderRadius.circular(9),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              value,
              style: TextStyle(
                fontSize: unknown ? 13.5 : 18,
                fontWeight: unknown ? FontWeight.w400 : FontWeight.w700,
                color: unknown ? palette.textMuted : (tone ?? palette.text),
              ),
            ),
            const SizedBox(height: 2),
            Text(
              derived ? '$label (derived)' : label,
              style: TextStyle(fontSize: 11.5, color: palette.textMuted),
            ),
          ],
        ),
      ),
    );
  }
}

class _Fact extends StatelessWidget {
  const _Fact({
    required this.icon,
    required this.text,
    required this.palette,
    this.tone,
    this.unknown = false,
  });

  final IconData icon;
  final String text;
  final AppPalette palette;
  final Color? tone;
  final bool unknown;

  @override
  Widget build(BuildContext context) {
    final color = tone ?? palette.textMuted;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 5),
      decoration: BoxDecoration(
        color: palette.raised,
        borderRadius: BorderRadius.circular(20),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 13, color: color),
          const SizedBox(width: 6),
          Flexible(
            child: Text(
              text,
              style: TextStyle(
                fontSize: 11.5,
                color: color,
                fontStyle: unknown ? FontStyle.italic : FontStyle.normal,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _Note extends StatelessWidget {
  const _Note({required this.text, required this.palette});

  final String text;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
    return Text(
      text,
      style: TextStyle(fontSize: 12.5, height: 1.5, color: palette.textMuted),
    );
  }
}

class _Empty extends StatelessWidget {
  const _Empty({required this.text, required this.palette});

  final String text;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: palette.raised,
        borderRadius: BorderRadius.circular(10),
      ),
      child: Text(
        text,
        style: TextStyle(fontSize: 12.5, height: 1.5, color: palette.textMuted),
      ),
    );
  }
}
