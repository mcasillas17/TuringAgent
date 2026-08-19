import 'package:flutter/material.dart';

import '../../constants/app_colors.dart';
import '../../models/skill.dart';
import '../../networking/api_client.dart';
import 'workspace_pages.dart';

/// The library of named instructions, and where they are written.
///
/// Attaching happens in a conversation rather than here: a skill is a thing
/// you own, and which conversations use it is a property of those
/// conversations.
class SkillsPage extends StatefulWidget {
  const SkillsPage({super.key, required this.apiClient});

  final TuringApi apiClient;

  @override
  State<SkillsPage> createState() => _SkillsPageState();
}

class _SkillsPageState extends State<SkillsPage> {
  late Future<List<Skill>> _skills;

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

  Future<void> _edit({Skill? existing}) async {
    final saved = await showDialog<bool>(
      context: context,
      builder: (_) =>
          _SkillEditor(apiClient: widget.apiClient, existing: existing),
    );
    if (saved == true) _reload();
  }

  Future<void> _delete(Skill skill) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: Text('Delete "${skill.name}"?'),
        content: const Text(
          'This removes the skill from every conversation using it. Replies '
          'already being generated are unaffected — they were given a copy of '
          'the instructions when you sent the message.',
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
      await widget.apiClient.deleteSkill(skillId: skill.skillId);
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
      title: 'Skills',
      subtitle:
          'Instructions you write once and attach to the conversations where '
          'they apply. Nothing is chosen for you — a conversation follows '
          'exactly the skills you attach to it, and no others.',
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
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Align(
                alignment: Alignment.centerLeft,
                child: FilledButton.icon(
                  onPressed: () => _edit(),
                  icon: const Icon(Icons.add, size: 18),
                  label: const Text('New skill'),
                ),
              ),
              const SizedBox(height: 18),
              if (skills.isEmpty)
                WorkspaceNotice(
                  icon: Icons.auto_awesome_outlined,
                  title: 'No skills yet',
                  body:
                      'A skill is a standing instruction: how you want answers '
                      'written, a procedure to follow every time, a place to '
                      'always check first. Write one, then attach it to a '
                      'conversation from the bar above the messages.',
                )
              else
                for (final skill in skills)
                  Padding(
                    padding: const EdgeInsets.only(bottom: 10),
                    child: _SkillCard(
                      skill: skill,
                      palette: palette,
                      onEdit: () => _edit(existing: skill),
                      onDelete: () => _delete(skill),
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
    required this.onEdit,
    required this.onDelete,
  });

  final Skill skill;
  final AppPalette palette;
  final VoidCallback onEdit;
  final VoidCallback onDelete;

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
          Row(
            children: [
              Expanded(
                child: Text(
                  skill.name,
                  style: TextStyle(
                    fontSize: 14.5,
                    fontWeight: FontWeight.w600,
                    color: palette.text,
                  ),
                ),
              ),
              IconButton(
                icon: const Icon(Icons.edit_outlined, size: 17),
                tooltip: 'Edit skill',
                color: palette.textMuted,
                visualDensity: VisualDensity.compact,
                onPressed: onEdit,
              ),
              IconButton(
                icon: const Icon(Icons.delete_outline, size: 17),
                tooltip: 'Delete skill',
                color: palette.textMuted,
                visualDensity: VisualDensity.compact,
                onPressed: onDelete,
              ),
            ],
          ),
          const SizedBox(height: 6),
          Text(
            skill.instructions,
            maxLines: 4,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(
              fontSize: 13.5,
              height: 1.55,
              color: palette.textMuted,
            ),
          ),
        ],
      ),
    );
  }
}

class _SkillEditor extends StatefulWidget {
  const _SkillEditor({required this.apiClient, this.existing});

  final TuringApi apiClient;
  final Skill? existing;

  @override
  State<_SkillEditor> createState() => _SkillEditorState();
}

class _SkillEditorState extends State<_SkillEditor> {
  late final TextEditingController _name = TextEditingController(
    text: widget.existing?.name ?? '',
  );
  late final TextEditingController _instructions = TextEditingController(
    text: widget.existing?.instructions ?? '',
  );
  bool _saving = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _instructions.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    if (_saving) return;
    final name = _name.text.trim();
    final instructions = _instructions.text.trim();
    // Checked here as well as on the server so the reason appears next to the
    // field rather than as a failed round trip.
    if (name.isEmpty || instructions.isEmpty) {
      setState(() => _error = 'A skill needs both a name and instructions.');
      return;
    }
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final existing = widget.existing;
      if (existing == null) {
        await widget.apiClient.createSkill(
          name: name,
          instructions: instructions,
        );
      } else {
        await widget.apiClient.updateSkill(
          skillId: existing.skillId,
          name: name,
          instructions: instructions,
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
    return AlertDialog(
      title: Text(widget.existing == null ? 'New skill' : 'Edit skill'),
      content: SizedBox(
        width: 460,
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
                controller: _instructions,
                minLines: 5,
                maxLines: 12,
                decoration: const InputDecoration(
                  labelText: 'Instructions',
                  alignLabelWithHint: true,
                  helperText: 'Written to the agent, in your own words',
                ),
              ),
              if (_error != null) ...[
                const SizedBox(height: 14),
                Text(
                  _error!,
                  style: TextStyle(fontSize: 13, color: AppColors.danger),
                ),
              ],
              const SizedBox(height: 14),
              Text(
                'Attached skills are sent with every message in the '
                'conversations you attach them to.',
                style: TextStyle(fontSize: 12.5, color: palette.textMuted),
              ),
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
