import 'package:flutter/material.dart';

/// Presentational card for a terminal `agent.run.failed` event.
///
/// Deliberately distinct from [RunNoticeCard]: a failure is not routine
/// progress (a retry, a step, giving up after N attempts) and must not be
/// mistaken for one, so it gets its own error-styled card rather than
/// reusing the neutral notice.
class RunFailureCard extends StatelessWidget {
  const RunFailureCard({super.key, required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;

    return Semantics(
      container: true,
      liveRegion: true,
      label: 'Run failed: $message',
      child: ExcludeSemantics(
        child: Align(
          alignment: Alignment.centerLeft,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 640),
            child: Card(
              color: colorScheme.errorContainer,
              margin: const EdgeInsets.symmetric(vertical: 4),
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(8),
              ),
              child: Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 14,
                  vertical: 10,
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Icon(
                      Icons.error_outline,
                      size: 18,
                      color: colorScheme.onErrorContainer,
                    ),
                    const SizedBox(width: 10),
                    Flexible(
                      child: Text(
                        message,
                        style: theme.textTheme.bodyMedium?.copyWith(
                          color: colorScheme.onErrorContainer,
                        ),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}
