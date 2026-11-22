import 'package:material_ui/material_ui.dart';

import '../../constants/app_colors.dart';
import '../../models/automation.dart';
import '../../networking/api_client.dart';

/// The owner's quiet switch: one control that stops every automation at once.
///
/// It only ever shows a setting the backend answered with. A tap sends a
/// write naming the revision this panel last read and moves nothing until the
/// answer arrives, because the backend may refuse it as stale — and a switch
/// showing what was asked for, rather than what is held, would say quiet is
/// off while it is on.
class UnattendedQuietSwitch extends StatefulWidget {
  const UnattendedQuietSwitch({super.key, required this.apiClient});

  final TuringApi apiClient;

  @override
  State<UnattendedQuietSwitch> createState() => _UnattendedQuietSwitchState();
}

class _UnattendedQuietSwitchState extends State<UnattendedQuietSwitch> {
  /// Null while reading, and after a read that failed.
  UnattendedQuiet? _setting;
  bool _readFailed = false;
  bool _writing = false;

  @override
  void initState() {
    super.initState();
    _read();
  }

  Future<void> _read() async {
    try {
      final setting = await widget.apiClient.getUnattendedQuiet();
      if (!mounted) return;
      setState(() {
        _setting = setting;
        _readFailed = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _readFailed = true);
    }
  }

  void _readAgain() {
    setState(() {
      _setting = null;
      _readFailed = false;
    });
    _read();
  }

  Future<void> _write({required bool quiet, required int revision}) async {
    if (_writing) return;
    setState(() => _writing = true);
    try {
      final result = await widget.apiClient.setUnattendedQuiet(
        quiet: quiet,
        revision: revision,
      );
      if (!mounted) return;
      setState(() {
        _setting = result.setting;
        _writing = false;
      });
      if (result.stale) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text(
              'Quiet was changed somewhere else, so nothing changed here. '
              'This is its current setting.',
            ),
          ),
        );
      }
    } catch (error) {
      if (!mounted) return;
      // Read again as well as reported: the write may have landed before its
      // answer was lost, so neither the old setting nor the one asked for is
      // known to be the one the backend holds.
      setState(() {
        _writing = false;
        _setting = null;
        _readFailed = false;
      });
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text('Could not change quiet: $error')));
      await _read();
    }
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: palette.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: palette.border),
      ),
      child: _body(palette),
    );
  }

  Widget _body(AppPalette palette) {
    final setting = _setting;
    if (setting == null) {
      if (!_readFailed) {
        return _row(
          palette,
          trailing: const SizedBox.square(
            dimension: 18,
            child: CircularProgressIndicator(strokeWidth: 2),
          ),
        );
      }
      return Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          _row(palette, explanation: 'Could not read the quiet setting.'),
          const SizedBox(height: 8),
          TextButton.icon(
            onPressed: _readAgain,
            icon: const Icon(Icons.refresh, size: 16),
            label: const Text('Read it again'),
          ),
        ],
      );
    }

    final unavailable = setting.unavailable;
    final row = _row(
      palette,
      explanation: _meaning(setting),
      trailing: Switch(
        key: const Key('unattended-quiet-switch'),
        // An unreadable setting counts as on. It cannot be flipped, because
        // which way it points is exactly what is unknown; the two buttons
        // below repair it instead.
        value: unavailable || setting.quiet,
        onChanged: unavailable || _writing
            ? null
            : (quiet) => _write(quiet: quiet, revision: setting.revision),
      ),
    );
    if (!unavailable) return row;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        row,
        const SizedBox(height: 12),
        // A Wrap, so the pair stacks on a phone rather than overflowing.
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            FilledButton.tonal(
              onPressed: _writing
                  ? null
                  : () => _write(quiet: true, revision: setting.revision),
              child: const Text('Keep quiet on'),
            ),
            OutlinedButton(
              onPressed: _writing
                  ? null
                  : () => _write(quiet: false, revision: setting.revision),
              child: const Text('Turn quiet off'),
            ),
          ],
        ),
      ],
    );
  }

  /// What the setting means for unattended work, in the state it is in.
  ///
  /// A run already going finishes either way, so no sentence here may claim
  /// that quiet stops it.
  static String _meaning(UnattendedQuiet setting) {
    if (setting.unavailable) {
      return "The saved setting can't be read, so nothing unattended starts. "
          'Choose one to repair it.';
    }
    if (setting.quiet) {
      return 'No automation starts, and queued automation runs are cancelled. '
          'A run already going finishes. When you turn quiet off, nothing '
          'queued before it starts: each automation that came due runs once, '
          'soon after, then keeps its schedule.';
    }
    return 'Turn on to stop unattended work: no automation starts, and queued '
        'automation runs are cancelled. A run already going finishes.';
  }

  /// The title, what the setting means, and the control, merged so a screen
  /// reader announces the switch by what it does.
  Widget _row(AppPalette palette, {String? explanation, Widget? trailing}) {
    return MergeSemantics(
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'Quiet unattended work',
                  style: TextStyle(
                    fontSize: 14.5,
                    fontWeight: FontWeight.w600,
                    color: palette.text,
                  ),
                ),
                if (explanation != null) ...[
                  const SizedBox(height: 6),
                  Text(
                    explanation,
                    style: TextStyle(
                      fontSize: 13,
                      height: 1.5,
                      color: palette.textMuted,
                    ),
                  ),
                ],
              ],
            ),
          ),
          if (trailing != null) ...[const SizedBox(width: 12), trailing],
        ],
      ),
    );
  }
}
