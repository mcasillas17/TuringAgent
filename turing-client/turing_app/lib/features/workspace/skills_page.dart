import 'package:flutter/material.dart';

import '../../constants/app_colors.dart';
import '../../models/skill.dart';
import '../../networking/api_client.dart';
import 'workspace_pages.dart';

/// A read-only browser for the SKILL.md files mounted into the backend.
/// TuringAgent owns only enablement and per-capability consent.
class SkillsPage extends StatefulWidget {
  const SkillsPage({super.key, required this.apiClient});

  final TuringApi apiClient;

  @override
  State<SkillsPage> createState() => _SkillsPageState();
}

class _SkillsPageState extends State<SkillsPage> {
  late Future<List<Skill>> _skills;
  final Set<String> _busy = {};

  @override
  void initState() {
    super.initState();
    _skills = widget.apiClient.listSkills();
  }

  void _reload() {
    setState(() {
      _skills = widget.apiClient.listSkills();
    });
  }

  Future<void> _setEnabled(Skill skill, bool enabled) async {
    await _mutate(
      skill,
      () => widget.apiClient.setSkillEnabled(
        skillId: skill.skillId,
        enabled: enabled,
      ),
      'change enablement',
    );
  }

  Future<void> _setCapability(
    Skill skill,
    String capability,
    bool granted,
  ) async {
    await _mutate(
      skill,
      () => widget.apiClient.setSkillCapabilityGrant(
        skillId: skill.skillId,
        capability: capability,
        granted: granted,
      ),
      'change capability consent',
    );
  }

  Future<void> _mutate(
    Skill skill,
    Future<Skill> Function() request,
    String operation,
  ) async {
    if (_busy.contains(skill.skillId)) return;
    setState(() => _busy.add(skill.skillId));
    try {
      final updated = await request();
      final current = await _skills;
      final next = [...current];
      final index = next.indexWhere((item) => item.skillId == skill.skillId);
      if (index >= 0) next[index] = updated;
      if (!mounted) return;
      setState(() => _skills = Future.value(List.unmodifiable(next)));
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text('Could not $operation for ${skill.skillId}: $error'),
        ),
      );
    } finally {
      if (mounted) setState(() => _busy.remove(skill.skillId));
    }
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return WorkspacePage(
      title: 'Skills',
      subtitle:
          'Skills are files in turing-backend/skills. Enabled skill metadata '
          'is available on every request; the agent reads a body only when it '
          'selects that skill or you name its path explicitly. Editing a skill '
          'can require fresh capability consent so a remove-and-restore cannot '
          'silently reuse an old grant.',
      child: FutureBuilder<List<Skill>>(
        future: _skills,
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
          final skills = snapshot.data ?? const <Skill>[];
          if (skills.isEmpty) {
            return WorkspaceNotice(
              icon: Icons.description_outlined,
              title: 'No skill files found',
              body:
                  'Create turing-backend/skills/<category>/<skill>/SKILL.md, '
                  'then refresh this page. New folders start disabled.',
              onRetry: _reload,
            );
          }
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              for (final skill in skills)
                Padding(
                  padding: const EdgeInsets.only(bottom: 12),
                  child: _SkillCard(
                    skill: skill,
                    palette: palette,
                    busy: _busy.contains(skill.skillId),
                    onEnabled: (value) => _setEnabled(skill, value),
                    onCapability: (capability, granted) =>
                        _setCapability(skill, capability, granted),
                  ),
                ),
            ],
          );
        },
      ),
    );
  }
}

class _SkillCard extends StatelessWidget {
  const _SkillCard({
    required this.skill,
    required this.palette,
    required this.busy,
    required this.onEnabled,
    required this.onCapability,
  });

  final Skill skill;
  final AppPalette palette;
  final bool busy;
  final ValueChanged<bool> onEnabled;
  final void Function(String capability, bool granted) onCapability;

  @override
  Widget build(BuildContext context) {
    final displayPath = skill.folderPath.startsWith('/skills')
        ? 'turing-backend/skills${skill.folderPath.substring('/skills'.length)}'
        : skill.folderPath;
    final metadata = <String>[
      if (skill.version.isNotEmpty) 'Version ${skill.version}',
      if (skill.author.isNotEmpty) 'Author ${skill.author}',
      if (skill.license.isNotEmpty) 'License ${skill.license}',
    ];
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
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      skill.name.isEmpty ? skill.skillId : skill.name,
                      style: TextStyle(
                        fontSize: 15,
                        fontWeight: FontWeight.w700,
                        color: palette.text,
                      ),
                    ),
                    const SizedBox(height: 3),
                    SelectableText(
                      skill.skillId,
                      style: TextStyle(
                        fontSize: 12.5,
                        color: palette.textMuted,
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(width: 12),
              Switch(value: skill.enabled, onChanged: busy ? null : onEnabled),
            ],
          ),
          const SizedBox(height: 8),
          SelectableText(
            displayPath,
            style: TextStyle(fontSize: 12, color: palette.textMuted),
          ),
          if (metadata.isNotEmpty) ...[
            const SizedBox(height: 6),
            Text(
              metadata.join(' · '),
              style: TextStyle(fontSize: 12.5, color: palette.textMuted),
            ),
          ],
          if (skill.parseError.isNotEmpty) ...[
            const SizedBox(height: 12),
            _StatusBox(
              icon: Icons.warning_amber_rounded,
              text: 'SKILL.md could not be parsed: ${skill.parseError}',
              color: AppColors.danger,
            ),
          ] else ...[
            if (skill.description.isNotEmpty) ...[
              const SizedBox(height: 12),
              Text(
                skill.description,
                style: TextStyle(fontSize: 13.5, color: palette.textMuted),
              ),
            ],
            if (skill.body.isNotEmpty) ...[
              const SizedBox(height: 12),
              SelectableText(
                skill.body,
                style: TextStyle(
                  fontSize: 13.5,
                  height: 1.5,
                  color: palette.text,
                ),
              ),
            ],
            const SizedBox(height: 12),
            _StatusBox(
              icon: _statusIcon(skill),
              text: _statusText(skill),
              color: _statusColor(skill),
            ),
            if (skill.requires.isNotEmpty) ...[
              const SizedBox(height: 10),
              Text(
                'Capability consent',
                style: TextStyle(
                  fontSize: 12.5,
                  fontWeight: FontWeight.w600,
                  color: palette.text,
                ),
              ),
              for (final capability in skill.requires)
                CheckboxListTile(
                  dense: true,
                  contentPadding: EdgeInsets.zero,
                  controlAffinity: ListTileControlAffinity.leading,
                  title: Text(capability),
                  value: skill.grantedCapabilities.contains(capability),
                  onChanged: busy
                      ? null
                      : (value) => onCapability(capability, value ?? false),
                ),
            ],
          ],
        ],
      ),
    );
  }

  static String _statusText(Skill skill) {
    if (!skill.enabled) return 'Disabled';
    if (skill.missingCapabilities.isNotEmpty) {
      return 'Withheld until every capability is granted: '
          '${skill.missingCapabilities.join(', ')}';
    }
    return 'Ready to load';
  }

  static IconData _statusIcon(Skill skill) {
    if (!skill.enabled) return Icons.pause_circle_outline;
    if (skill.missingCapabilities.isNotEmpty) return Icons.lock_outline;
    return Icons.check_circle_outline;
  }

  static Color _statusColor(Skill skill) {
    if (!skill.enabled || skill.missingCapabilities.isNotEmpty) {
      return AppColors.warning;
    }
    return AppColors.success;
  }
}

class _StatusBox extends StatelessWidget {
  const _StatusBox({
    required this.icon,
    required this.text,
    required this.color,
  });

  final IconData icon;
  final String text;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, size: 16, color: color),
        const SizedBox(width: 7),
        Expanded(
          child: Text(
            text,
            style: TextStyle(fontSize: 12.5, height: 1.35, color: color),
          ),
        ),
      ],
    );
  }
}
