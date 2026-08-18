import 'package:flutter/material.dart';

import '../../constants/app_colors.dart';
import '../../models/skill.dart';
import '../../networking/api_client.dart';

/// A slim strip above the conversation naming the skills it is following.
///
/// It sits here rather than in Settings or the library because attachment is a
/// property of the conversation, and because standing instructions that change
/// the answers ought to be visible while you read them. When nothing is
/// attached it stays quiet but present, so the affordance is discoverable
/// without shouting.
class SessionSkillsBar extends StatefulWidget {
  const SessionSkillsBar({
    super.key,
    required this.apiClient,
    required this.sessionId,
  });

  final TuringApi apiClient;
  final String sessionId;

  @override
  State<SessionSkillsBar> createState() => _SessionSkillsBarState();
}

class _SessionSkillsBarState extends State<SessionSkillsBar> {
  List<Skill> _attached = const [];
  bool _loading = true;
  bool _failed = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final skills = await widget.apiClient.listSessionSkills(
        sessionId: widget.sessionId,
      );
      if (!mounted) return;
      setState(() {
        _attached = skills;
        _loading = false;
        _failed = false;
      });
    } on Exception {
      if (!mounted) return;
      // Not "No skills attached" — that would be a confident claim about
      // state we just failed to read. The strip stays, but says it does not
      // know and offers a retry: hiding it outright would leave a
      // conversation applying standing instructions with no way to see or
      // change them.
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
      builder: (_) => _SkillPicker(
        apiClient: widget.apiClient,
        sessionId: widget.sessionId,
      ),
    );
    // Any outcome except an explicit "nothing changed" reloads: dismissing
    // the sheet by swiping returns null, and the toggles already wrote to the
    // server before that happened.
    if (changed != false) await _load();
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    if (_loading) return const SizedBox.shrink();
    if (_failed) {
      return _BarShell(
        palette: palette,
        onTap: _load,
        icon: Icons.sync_problem_outlined,
        iconColor: palette.textMuted,
        label: 'Could not load this conversation\'s skills',
        labelColor: palette.textMuted,
        action: 'Retry',
      );
    }
    final empty = _attached.isEmpty;
    return _BarShell(
      palette: palette,
      onTap: _openPicker,
      icon: Icons.auto_awesome_outlined,
      iconColor: empty ? palette.textMuted : AppColors.brand,
      label: empty
          ? 'No skills attached'
          : _attached.map((skill) => skill.name).join(', '),
      labelColor: empty ? palette.textMuted : palette.text,
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
  });

  final AppPalette palette;
  final VoidCallback onTap;
  final IconData icon;
  final Color iconColor;
  final String label;
  final Color labelColor;
  final String action;

  @override
  Widget build(BuildContext context) {
    return Material(
      color: palette.surface,
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

class _SkillPicker extends StatefulWidget {
  const _SkillPicker({required this.apiClient, required this.sessionId});

  final TuringApi apiClient;
  final String sessionId;

  @override
  State<_SkillPicker> createState() => _SkillPickerState();
}

class _SkillPickerState extends State<_SkillPicker> {
  List<Skill> _all = const [];
  Set<String> _attachedIds = {};
  bool _loading = true;
  bool _failed = false;
  bool _changed = false;
  String? _error;
  final Set<String> _busy = {};

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      // Both at once: the sheet cannot render until it has each, so serialising
      // them only doubles how long the user waits on an empty panel.
      final results = await Future.wait([
        widget.apiClient.listSkills(),
        widget.apiClient.listSessionSkills(sessionId: widget.sessionId),
      ]);
      final all = results[0];
      final attached = results[1];
      if (!mounted) return;
      setState(() {
        _all = all;
        _attachedIds = attached.map((skill) => skill.skillId).toSet();
        _loading = false;
      });
    } on Exception {
      if (!mounted) return;
      // Distinguished from an empty library: telling someone they have written
      // no skills when the read failed sends them to write one they already
      // have.
      setState(() {
        _loading = false;
        _failed = true;
      });
    }
  }

  Future<void> _toggle(Skill skill, bool attach) async {
    if (!_busy.add(skill.skillId)) return;
    try {
      final result = attach
          ? await widget.apiClient.attachSkill(
              sessionId: widget.sessionId,
              skillId: skill.skillId,
            )
          : await widget.apiClient.detachSkill(
              sessionId: widget.sessionId,
              skillId: skill.skillId,
            );
      if (!mounted) return;
      setState(() {
        // The server returns the whole set, so the checkboxes follow what it
        // now believes rather than what this widget assumed.
        _attachedIds = result.map((skill) => skill.skillId).toSet();
        _changed = true;
        _error = null;
      });
    } on Exception catch (error) {
      if (!mounted) return;
      // Shown inside the sheet, not via ScaffoldMessenger: a snackbar renders
      // in the Scaffold underneath this modal, so the user would see the tick
      // fail to move and be told nothing.
      setState(() => _error = 'Could not change that: $error');
    } finally {
      _busy.remove(skill.skillId);
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
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 18, 20, 6),
              child: Text(
                'Skills for this conversation',
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
                'Attached skills are sent with every message you send here.',
                style: TextStyle(fontSize: 12.5, color: palette.textMuted),
              ),
            ),
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
            else if (_failed)
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 10, 20, 24),
                child: Text(
                  'Could not load your skills. The backend may not be running.',
                  style: TextStyle(
                    fontSize: 13.5,
                    height: 1.55,
                    color: AppColors.danger,
                  ),
                ),
              )
            else if (_all.isEmpty)
              Padding(
                padding: const EdgeInsets.fromLTRB(20, 10, 20, 24),
                child: Text(
                  'You have not written any skills yet. The Skills section in '
                  'the sidebar is where they live.',
                  style: TextStyle(
                    fontSize: 13.5,
                    height: 1.55,
                    color: palette.textMuted,
                  ),
                ),
              )
            else
              Flexible(
                child: ListView(
                  shrinkWrap: true,
                  children: [
                    for (final skill in _all)
                      CheckboxListTile(
                        value: _attachedIds.contains(skill.skillId),
                        onChanged: (value) => _toggle(skill, value ?? false),
                        title: Text(skill.name),
                        subtitle: Text(
                          skill.instructions,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                        ),
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
