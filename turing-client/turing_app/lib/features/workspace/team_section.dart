import 'package:flutter/material.dart';

import '../../constants/app_colors.dart';
import '../../models/agent_profile.dart';
import '../../networking/api_client.dart';
import 'workspace_pages.dart';

/// The specialists Turing can hand work to, read from `team/<id>/AGENT.md`.
///
/// It loads on its own, so a team the backend cannot read never hides the
/// agents listed beside it, and the reverse. Nothing here delegates: this build
/// records which profiles the user enabled and granted, and says so.
class TeamSection extends StatefulWidget {
  const TeamSection({super.key, required this.apiClient});

  final TuringApi apiClient;

  @override
  State<TeamSection> createState() => _TeamSectionState();
}

class _TeamSectionState extends State<TeamSection> {
  List<AgentProfile>? _profiles;
  Object? _error;
  bool _loading = true;

  /// A change is in flight. The section runs one request at a time: a list
  /// read before a change lands after it would put the old state back on
  /// screen, and two changes could each overwrite the other's result.
  bool _changing = false;

  bool get _locked => _loading || _changing;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final profiles = await widget.apiClient.listAgentProfiles();
      if (!mounted) return;
      setState(() {
        _profiles = profiles;
        _loading = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _error = error;
        _loading = false;
      });
    }
  }

  void _replace(AgentProfile updated) {
    final profiles = _profiles;
    if (profiles == null) return;
    setState(() {
      _profiles = [
        for (final profile in profiles)
          profile.profileId == updated.profileId ? updated : profile,
      ];
    });
  }

  Future<void> _onToggle(AgentProfile profile, bool enabled) async {
    if (!enabled) {
      await _run(
        profile,
        () => widget.apiClient.setAgentProfileEnabled(
          profileId: profile.profileId,
          enabled: false,
        ),
      );
      return;
    }
    if (profile.grantsCurrentRevision) {
      await _run(
        profile,
        () => widget.apiClient.setAgentProfileEnabled(
          profileId: profile.profileId,
          enabled: true,
        ),
      );
      return;
    }
    await _review(profile);
  }

  /// Shows what the current revision asks for and grants exactly that
  /// revision. Turning the profile on comes second, so a grant the backend
  /// refuses leaves it off.
  Future<void> _review(AgentProfile profile) async {
    if (_locked) return;
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (_) => AgentProfileGrantSheet(profile: profile),
    );
    if (confirmed != true || !mounted) return;
    await _run(profile, () async {
      final AgentProfile granted;
      try {
        granted = await widget.apiClient.grantAgentProfile(
          profileId: profile.profileId,
          revision: profile.revision,
        );
      } on TuringApiException catch (error) {
        if (error.code == 'failed_precondition') {
          throw _GrantRefused(error.message);
        }
        rethrow;
      }
      if (granted.enabled) return granted;
      return widget.apiClient.setAgentProfileEnabled(
        profileId: profile.profileId,
        enabled: true,
      );
    });
  }

  Future<void> _run(
    AgentProfile profile,
    Future<AgentProfile> Function() request,
  ) async {
    if (_locked) return;
    setState(() => _changing = true);
    try {
      final updated = await request();
      if (!mounted) return;
      _replace(updated);
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(_failureText(profile, error))));
      // Whatever the backend now holds is the truth, including a grant that
      // landed before the enable failed.
      await _load();
    } finally {
      if (mounted) setState(() => _changing = false);
    }
  }

  static String _failureText(AgentProfile profile, Object error) {
    final path = 'team/${profile.profileId}/AGENT.md';
    if (error is _GrantRefused) {
      return '$path changed while you were reviewing it, or cannot be used. '
          'Nothing was granted; review the new version.';
    }
    if (error is TuringApiException) {
      switch (error.code) {
        case 'failed_precondition':
          return '${profile.label} cannot be turned on: ${error.message}';
        case 'not_found':
          return '$path is gone. The list has been refreshed.';
      }
      return 'Could not update ${profile.label}: ${error.message}';
    }
    return 'Could not update ${profile.label}: $error';
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Expanded(
              child: Text(
                'Turing\'s team',
                style: TextStyle(
                  fontSize: 16,
                  fontWeight: FontWeight.w700,
                  color: palette.text,
                ),
              ),
            ),
            IconButton(
              icon: const Icon(Icons.refresh, size: 18),
              tooltip: 'Read the team folder again',
              color: palette.textMuted,
              visualDensity: VisualDensity.compact,
              onPressed: _locked ? null : _load,
            ),
          ],
        ),
        const SizedBox(height: 4),
        Text(
          'Specialists Turing can hand work to. Each one is a file, '
          'team/<id>/AGENT.md, beside your backend. Turning one on asks you '
          'to grant the tools, skills, memory and model that file lists, '
          'unless you already granted them; turning it off keeps the grant, '
          'and changing any of those asks again. Delegation is not switched on '
          'in this build, so the team does not take part in conversations yet.',
          style: TextStyle(
            fontSize: 13,
            height: 1.55,
            color: palette.textMuted,
          ),
        ),
        const SizedBox(height: 12),
        ..._body(palette),
      ],
    );
  }

  List<Widget> _body(AppPalette palette) {
    final profiles = _profiles;
    if (_loading && profiles == null) {
      return const [WorkspaceLoading()];
    }
    final error = _error;
    if (error != null) {
      return [
        WorkspaceNotice(
          icon: Icons.error_outline,
          title: 'Could not load your team',
          body: error is TuringApiException ? error.message : '$error',
          onRetry: _load,
          tone: AppColors.danger,
        ),
      ];
    }
    if (profiles == null || profiles.isEmpty) {
      return const [
        WorkspaceNotice(
          icon: Icons.groups_outlined,
          title: 'No specialists yet',
          body:
              'Add a folder under team/ with an AGENT.md in it, then read the '
              'folder again. To bring back Dev, Inbox or Research, copy its '
              'folder from scripts/team-templates/ into team/. It returns '
              'with the setting and grant it had when you removed it.',
        ),
      ];
    }
    return [
      for (final profile in profiles)
        Padding(
          padding: const EdgeInsets.only(bottom: 10),
          child: _ProfileCard(
            profile: profile,
            palette: palette,
            busy: _locked,
            onToggle: (enabled) => _onToggle(profile, enabled),
            onReview: () => _review(profile),
          ),
        ),
    ];
  }
}

/// The backend refused the grant itself: the reviewed revision is no longer
/// the file's, or the file no longer parses. Nothing was recorded.
class _GrantRefused implements Exception {
  const _GrantRefused(this.message);

  final String message;

  @override
  String toString() => message;
}

class _ProfileCard extends StatelessWidget {
  const _ProfileCard({
    required this.profile,
    required this.palette,
    required this.busy,
    required this.onToggle,
    required this.onReview,
  });

  final AgentProfile profile;
  final AppPalette palette;
  final bool busy;
  final ValueChanged<bool> onToggle;
  final VoidCallback onReview;

  @override
  Widget build(BuildContext context) {
    final state = _stateLabel(profile);
    // A file that does not parse cannot be turned on, but one that was on
    // before it broke can still be turned off.
    final canToggle =
        !busy &&
        !(profile.state == AgentProfileState.parseError && !profile.enabled);
    return Container(
      key: ValueKey('team-profile-${profile.profileId}'),
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: palette.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: palette.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              if (profile.emoji.isNotEmpty)
                // The loader allows up to 16 characters; clip rather than let
                // them push the switch off the card.
                ConstrainedBox(
                  constraints: const BoxConstraints(maxWidth: 48),
                  child: Text(
                    profile.emoji,
                    maxLines: 1,
                    softWrap: false,
                    overflow: TextOverflow.clip,
                    style: const TextStyle(fontSize: 18),
                  ),
                )
              else
                Icon(Icons.person_outline, size: 18, color: palette.textMuted),
              const SizedBox(width: 10),
              Expanded(
                child: Text(
                  profile.label,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                    fontSize: 14.5,
                    fontWeight: FontWeight.w600,
                    color: palette.text,
                  ),
                ),
              ),
              const SizedBox(width: 6),
              Semantics(
                label: 'Use ${profile.label}',
                child: Switch(
                  value: profile.enabled,
                  onChanged: canToggle ? onToggle : null,
                ),
              ),
            ],
          ),
          // On its own line: the longest status is wider than a phone leaves
          // beside the name and the switch.
          const SizedBox(height: 4),
          _StateChip(label: state.$1, color: state.$2 ?? palette.textMuted),
          if (profile.description.isNotEmpty) ...[
            const SizedBox(height: 4),
            Text(
              profile.description,
              style: TextStyle(fontSize: 12.5, color: palette.textMuted),
            ),
          ],
          if (profile.state == AgentProfileState.parseError) ...[
            const SizedBox(height: 8),
            Text(
              'team/${profile.profileId}/AGENT.md could not be read: '
              '${profile.parseError}',
              style: const TextStyle(
                fontSize: 12.5,
                height: 1.5,
                color: AppColors.danger,
              ),
            ),
          ] else ...[
            const SizedBox(height: 8),
            Text(
              profile.resolvedTools.isEmpty
                  ? 'No tools resolve for it right now.'
                  : 'Tools: ${profile.resolvedTools.join(', ')}',
              style: TextStyle(
                fontSize: 12.5,
                height: 1.5,
                color: palette.textMuted,
              ),
            ),
            // Still listed once granted: the sheet that showed them is not
            // shown again until the declaration changes.
            if (profile.excludedTools.isNotEmpty)
              Text(
                'Withheld: '
                '${profile.excludedTools.map((e) => '${e.tool} (${e.reason})').join(', ')}',
                style: TextStyle(
                  fontSize: 12.5,
                  height: 1.5,
                  color: palette.textMuted,
                ),
              ),
          ],
          for (final reason in profile.unavailableReasons)
            Padding(
              padding: const EdgeInsets.only(top: 4),
              child: Text(
                '• $reason',
                style: const TextStyle(
                  fontSize: 12.5,
                  height: 1.5,
                  color: AppColors.warning,
                ),
              ),
            ),
          if (profile.state == AgentProfileState.needsGrant) ...[
            const SizedBox(height: 8),
            TextButton.icon(
              onPressed: busy ? null : onReview,
              icon: const Icon(Icons.fact_check_outlined, size: 16),
              label: const Text('Review and grant'),
            ),
          ],
        ],
      ),
    );
  }

  static (String, Color?) _stateLabel(AgentProfile profile) {
    switch (profile.state) {
      case AgentProfileState.active:
        return ('Active', AppColors.success);
      case AgentProfileState.disabled:
        return ('Off', null);
      case AgentProfileState.needsGrant:
        return (
          profile.grantedRevision.isEmpty
              ? 'Needs your grant'
              : 'Changed since you granted it',
          AppColors.warning,
        );
      case AgentProfileState.unavailable:
        return ('Unavailable', AppColors.warning);
      case AgentProfileState.parseError:
        return ('Cannot be read', AppColors.danger);
      case AgentProfileState.unknown:
        return ('Unknown state', null);
    }
  }
}

class _StateChip extends StatelessWidget {
  const _StateChip({required this.label, required this.color});

  final String label;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 4),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.13),
        borderRadius: BorderRadius.circular(20),
      ),
      child: Text(
        label,
        style: TextStyle(
          fontSize: 11.5,
          fontWeight: FontWeight.w600,
          color: color,
        ),
      ),
    );
  }
}

/// What granting a profile's current revision would allow, shown before the
/// grant is sent. Public so the widget test can pump it directly.
class AgentProfileGrantSheet extends StatelessWidget {
  const AgentProfileGrantSheet({super.key, required this.profile});

  final AgentProfile profile;

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    final heading = profile.emoji.isEmpty
        ? profile.label
        : '${profile.emoji} ${profile.label}';
    // Title and content scroll together: a valid name can be many lines
    // tall, and the buttons must stay in reach.
    return AlertDialog(
      scrollable: true,
      title: Text('Grant $heading?'),
      content: SizedBox(
        width: 520,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              'This grants revision ${_shortRevision(profile.revision)} of '
              'team/${profile.profileId}/AGENT.md. If what it asks for '
              'changes, the grant stops applying until you review it again. '
              'Delegation is not switched on in this build, so this changes no '
              'conversation yet.',
              style: TextStyle(
                fontSize: 13,
                height: 1.55,
                color: palette.textMuted,
              ),
            ),
            _GrantRow(label: 'Model', value: _modelText(profile)),
            _GrantRow(label: 'Memory', value: _memoryText(profile)),
            _GrantRow(
              label: 'Tools it asks for',
              value: _listOrNone(profile.tools),
            ),
            _GrantRow(
              label: 'Tools it requires',
              value: _listOrNone(profile.requires),
            ),
            _GrantRow(
              label: 'Tools it would get now',
              value: _listOrNone(profile.resolvedTools),
            ),
            if (profile.excludedTools.isNotEmpty)
              _GrantRow(
                label: 'Tools it would not get',
                value: profile.excludedTools
                    .map(
                      (exclusion) => '${exclusion.tool} (${exclusion.reason})',
                    )
                    .join('\n'),
              ),
            _GrantRow(label: 'Skills', value: _listOrNone(profile.skills)),
            _GrantRow(
              label: 'Tool calls per task',
              value: profile.maxToolCalls > 0
                  ? 'Asks for up to ${profile.maxToolCalls}; the worker\'s '
                        'own limit still applies'
                  : 'The worker\'s own limit',
            ),
            if (profile.unavailableReasons.isNotEmpty)
              _GrantRow(
                label: 'Not available yet',
                value: profile.unavailableReasons.join('\n'),
              ),
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('Cancel'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: Text(profile.enabled ? 'Grant' : 'Grant and turn on'),
        ),
      ],
    );
  }

  static String _shortRevision(String revision) =>
      revision.length > 12 ? revision.substring(0, 12) : revision;

  static String _listOrNone(List<String> values) =>
      values.isEmpty ? 'None' : values.join(', ');

  static String _modelText(AgentProfile profile) {
    if (profile.model.isEmpty) {
      return profile.resolvedModel.isEmpty
          ? 'Turing\'s default (no worker serves one right now)'
          : 'Turing\'s default (${profile.resolvedModel} right now)';
    }
    return profile.resolvedModel.isEmpty
        ? '${profile.model} (no worker serves it right now)'
        : profile.model;
  }

  /// The level only caps the memory tools the profile lists and grants none
  /// by itself, so it reads as a ceiling. The tools a run would get say what
  /// it can actually do.
  static String _memoryText(AgentProfile profile) {
    final reaches = profile.resolvedTools.any(
      (tool) => tool.startsWith('memory/'),
    );
    final String ceiling;
    switch (profile.memory) {
      case AgentProfileMemoryAccess.none:
        return 'None';
      case AgentProfileMemoryAccess.read:
        ceiling = 'Up to search and read';
      case AgentProfileMemoryAccess.propose:
        ceiling =
            'Up to reading and proposing notes (each proposal still asks you)';
      case AgentProfileMemoryAccess.unknown:
        return 'A level this app does not recognise';
    }
    return reaches
        ? '$ceiling; what it gets is under Tools it would get now'
        : '$ceiling, but none of its tools reach your memory now';
  }
}

class _GrantRow extends StatelessWidget {
  const _GrantRow({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return Padding(
      padding: const EdgeInsets.only(top: 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            label,
            style: TextStyle(
              fontSize: 12,
              fontWeight: FontWeight.w600,
              color: palette.text,
            ),
          ),
          const SizedBox(height: 3),
          SelectableText(
            value,
            style: TextStyle(
              fontSize: 12.5,
              height: 1.5,
              color: palette.textMuted,
            ),
          ),
        ],
      ),
    );
  }
}
