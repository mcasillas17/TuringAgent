import 'package:flutter/material.dart';

import '../../constants/app_colors.dart';
import '../../models/external_agent.dart';
import '../../networking/api_client.dart';

/// A strip above the conversation naming where its messages go.
///
/// This is the point of use. Sending a transcript to Anthropic or OpenAI is a
/// materially different act from running a model on this machine, and the
/// difference has to be legible while you are typing — not filed away in a
/// settings screen you visited once. So the bar is always present, states the
/// destination in plain words, and changes colour when that destination is off
/// the machine.
class SessionAgentBar extends StatefulWidget {
  const SessionAgentBar({
    super.key,
    required this.apiClient,
    required this.sessionId,
  });

  final TuringApi apiClient;
  final String sessionId;

  @override
  State<SessionAgentBar> createState() => _SessionAgentBarState();
}

class _SessionAgentBarState extends State<SessionAgentBar> {
  ExternalAgent? _agent;
  bool _loading = true;
  bool _failed = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final agent = await widget.apiClient.getSessionAgent(
        sessionId: widget.sessionId,
      );
      if (!mounted) return;
      setState(() {
        _agent = agent;
        _loading = false;
        _failed = false;
      });
    } on Exception {
      if (!mounted) return;
      // Never "stays on this machine" — that is the reassuring answer, and
      // giving it after failing to read the state would be the one lie this
      // bar exists to prevent. It says it does not know, and offers a retry.
      setState(() {
        _loading = false;
        _failed = true;
      });
    }
  }

  Future<void> _openPicker() async {
    final changed = await showModalBottomSheet<bool>(
      context: context,
      isScrollControlled: true,
      builder: (_) => _DestinationPicker(
        apiClient: widget.apiClient,
        sessionId: widget.sessionId,
        current: _agent,
      ),
    );
    // Any outcome except an explicit "nothing changed" reloads: dismissing the
    // sheet by swiping returns null, and the choice already reached the server
    // before that happened.
    if (changed != false) await _load();
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    // Not SizedBox.shrink(). The composer is usable while this loads, and a
    // missing strip during that window reads exactly like "nothing to say
    // here" — which is the reassurance-by-omission the failure branch below
    // refuses to give. It occupies its own height and says what it is doing.
    if (_loading) {
      return _BarShell(
        palette: palette,
        onTap: _load,
        icon: Icons.more_horiz,
        iconColor: palette.textMuted,
        label: 'Checking where this conversation goes',
        labelColor: palette.textMuted,
        action: '',
      );
    }
    if (_failed) {
      return _BarShell(
        palette: palette,
        onTap: _load,
        icon: Icons.sync_problem_outlined,
        iconColor: palette.textMuted,
        label: 'Could not tell where this conversation goes',
        labelColor: palette.textMuted,
        action: 'Retry',
      );
    }
    final agent = _agent;
    if (agent == null) {
      return _BarShell(
        palette: palette,
        onTap: _openPicker,
        icon: Icons.computer_outlined,
        iconColor: palette.textMuted,
        label: 'Turing — this conversation stays on your machine',
        labelColor: palette.textMuted,
        action: 'Change',
      );
    }
    return _BarShell(
      palette: palette,
      onTap: _openPicker,
      background: AppColors.warning.withValues(alpha: 0.12),
      icon: Icons.cloud_upload_outlined,
      iconColor: AppColors.warning,
      label: 'Goes to ${agent.displayName} — messages leave your machine',
      labelColor: palette.text,
      action: 'Change',
    );
  }
}

class _BarShell extends StatelessWidget {
  const _BarShell({
    required this.palette,
    required this.onTap,
    required this.icon,
    required this.iconColor,
    required this.label,
    required this.labelColor,
    required this.action,
    this.background,
  });

  final AppPalette palette;
  final VoidCallback onTap;
  final IconData icon;
  final Color iconColor;
  final String label;
  final Color labelColor;
  final String action;
  final Color? background;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: background ?? palette.surface,
      child: InkWell(
        onTap: onTap,
        child: Container(
          decoration: BoxDecoration(
            border: Border(bottom: BorderSide(color: palette.border)),
          ),
          padding: const EdgeInsets.fromLTRB(16, 9, 12, 9),
          child: Row(
            children: [
              Icon(icon, size: 15, color: iconColor),
              const SizedBox(width: 9),
              Expanded(
                child: Text(
                  label,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 12.5, color: labelColor),
                ),
              ),
              Text(
                action,
                style: TextStyle(fontSize: 12.5, color: AppColors.brand),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _DestinationPicker extends StatefulWidget {
  const _DestinationPicker({
    required this.apiClient,
    required this.sessionId,
    required this.current,
  });

  final TuringApi apiClient;
  final String sessionId;
  final ExternalAgent? current;

  @override
  State<_DestinationPicker> createState() => _DestinationPickerState();
}

class _DestinationPickerState extends State<_DestinationPicker> {
  List<ExternalAgent> _agents = const [];
  String? _selectedId;
  bool _loading = true;
  bool _failed = false;
  bool _changed = false;
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _selectedId = widget.current?.agentId;
    _load();
  }

  Future<void> _load() async {
    try {
      final agents = await widget.apiClient.listExternalAgents();
      if (!mounted) return;
      setState(() {
        _agents = agents;
        _loading = false;
      });
    } on Exception {
      if (!mounted) return;
      // Distinguished from an empty list: telling someone they have configured
      // no agents when the read failed sends them to configure one they
      // already have.
      setState(() {
        _loading = false;
        _failed = true;
      });
    }
  }

  Future<void> _select(String? agentId) async {
    if (_busy || agentId == _selectedId) return;
    setState(() => _busy = true);
    try {
      final result = agentId == null
          ? await widget.apiClient.clearSessionAgent(
              sessionId: widget.sessionId,
            )
          : await widget.apiClient.setSessionAgent(
              sessionId: widget.sessionId,
              agentId: agentId,
            );
      if (!mounted) return;
      setState(() {
        // The server returns the destination it now believes in, so the
        // selection follows that rather than what this sheet assumed.
        _selectedId = result?.agentId;
        _changed = true;
        _error = null;
      });
    } on Exception catch (error) {
      if (!mounted) return;
      // Shown inside the sheet, not via ScaffoldMessenger: a snackbar renders
      // in the Scaffold underneath this modal, so the user would see the
      // selection fail to move and be told nothing.
      setState(() => _error = 'Could not change that: $error');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return SafeArea(
      child: ConstrainedBox(
        constraints: BoxConstraints(
          maxHeight: MediaQuery.of(context).size.height * 0.7,
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            // The heading and its explanation scroll WITH the options rather
            // than sitting above them at a fixed height. On a landscape phone
            // the sheet gets ~220px total, and four lines of standing text
            // plus two option rows do not fit — a fixed header there pushes
            // the choices off the bottom, which is worse than making the
            // explanation scroll.
            if (_loading)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 30),
                child: Center(
                  child: SizedBox.square(
                    dimension: 20,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  ),
                ),
              )
            else
              Flexible(
                child: ListView(
                  shrinkWrap: true,
                  children: [
                    Padding(
                      padding: const EdgeInsets.fromLTRB(20, 18, 20, 6),
                      child: Text(
                        'Where this conversation goes',
                        style: TextStyle(
                          fontSize: 16,
                          fontWeight: FontWeight.w600,
                          color: palette.text,
                        ),
                      ),
                    ),
                    Padding(
                      padding: const EdgeInsets.fromLTRB(20, 0, 20, 10),
                      child: Text(
                        'Choosing an agent below sends every message in this '
                        'conversation, and the results of any tool it runs, '
                        'to that company. Enabled skill metadata and any '
                        'skill text the agent loads are sent too. Material '
                        'recalled from your other conversations is never sent '
                        'to one.',
                        style: TextStyle(
                          fontSize: 12.5,
                          color: palette.textMuted,
                        ),
                      ),
                    ),
                    _DestinationTile(
                      selected: _selectedId == null,
                      onTap: () => _select(null),
                      title: 'Turing, on this machine',
                      subtitle: 'The default. Nothing leaves your computer.',
                    ),
                    if (_failed)
                      Padding(
                        padding: const EdgeInsets.fromLTRB(20, 10, 20, 10),
                        child: Text(
                          'Could not load your agents. The backend may not be '
                          'running, so anything you added is not listed here.',
                          style: TextStyle(
                            fontSize: 13.5,
                            height: 1.55,
                            color: AppColors.danger,
                          ),
                        ),
                      )
                    else if (_agents.isEmpty)
                      Padding(
                        padding: const EdgeInsets.fromLTRB(20, 10, 20, 10),
                        child: Text(
                          'You have not added any other agents. The Agents '
                          'section in the sidebar is where they live.',
                          style: TextStyle(
                            fontSize: 13.5,
                            height: 1.55,
                            color: palette.textMuted,
                          ),
                        ),
                      )
                    else
                      for (final agent in _agents)
                        _DestinationTile(
                          selected: _selectedId == agent.agentId,
                          onTap: () => _select(agent.agentId),
                          title: agent.displayName,
                          subtitle: agent.credentialAvailable
                              ? 'Leaves your machine · ${agent.model} · '
                                    '${agent.endpointHost}'
                              : 'Leaves your machine · no API key named '
                                    '"${agent.credentialRef}" — this will '
                                    'fail until one is configured',
                        ),
                  ],
                ),
              ),
            if (_error != null)
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 4, 20, 0),
                child: Text(
                  _error!,
                  style: TextStyle(fontSize: 12.5, color: AppColors.danger),
                ),
              ),
            Padding(
              padding: const EdgeInsets.fromLTRB(12, 4, 12, 12),
              child: Align(
                alignment: Alignment.centerRight,
                child: TextButton(
                  onPressed: () => Navigator.of(context).pop(_changed),
                  child: const Text('Done'),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// One choice in the destination sheet.
///
/// A [ListTile] rather than a radio: the two options differ in more than which
/// one is ticked — one keeps the conversation here and the other does not — so
/// each row carries a sentence, and the tick is only the reminder of which was
/// chosen.
class _DestinationTile extends StatelessWidget {
  const _DestinationTile({
    required this.selected,
    required this.onTap,
    required this.title,
    required this.subtitle,
  });

  final bool selected;
  final VoidCallback onTap;
  final String title;
  final String subtitle;

  @override
  Widget build(BuildContext context) {
    return ListTile(
      onTap: onTap,
      selected: selected,
      leading: Icon(
        selected ? Icons.radio_button_checked : Icons.radio_button_unchecked,
        size: 20,
      ),
      title: Text(title),
      subtitle: Text(subtitle),
    );
  }
}
