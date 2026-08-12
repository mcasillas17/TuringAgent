import 'package:flutter/material.dart';

/// Presentational notice for metadata emitted while an agent run is active.
///
/// The chat screen owns event handling and ordering; this widget only renders
/// the runtime-provided note as inline, screen-reader-announced meta text.
class RunNoticeCard extends StatelessWidget {
  const RunNoticeCard({super.key, required this.note});

  final String note;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final foreground = theme.colorScheme.onSurfaceVariant;

    return Semantics(
      container: true,
      liveRegion: true,
      label: note,
      child: ExcludeSemantics(
        child: Align(
          alignment: Alignment.center,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 640),
            child: Padding(
              padding: const EdgeInsets.symmetric(vertical: 8),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  Icon(Icons.info_outline, size: 18, color: foreground),
                  const SizedBox(width: 8),
                  Flexible(
                    child: Text(
                      note,
                      textAlign: TextAlign.center,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: foreground,
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
