import 'package:flutter/material.dart';

/// Shared presentational chrome for a terminal run outcome — failure or
/// cancellation. Neither is routine progress (a retry, a step, giving up
/// after N attempts), so both get the same error-styled card rather than the
/// neutral [RunNoticeCard]. What differs between the two outcomes is only the
/// wording: callers supply their own truthful [outcomeLabel] so a
/// cancellation is never rendered — or announced to assistive technology —
/// as a failure.
class TerminalRunCard extends StatelessWidget {
  const TerminalRunCard({
    super.key,
    required this.outcomeLabel,
    required this.message,
  });

  /// The truthful, human-readable name of the outcome this card reports
  /// (e.g. `Run failed`, `Run cancelled`). Rendered visibly above [message]
  /// so sighted users can tell the two outcomes apart on screen, and
  /// prefixed onto [message] to form the semantics label assistive
  /// technology announces.
  final String outcomeLabel;

  final String message;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;

    return Semantics(
      container: true,
      liveRegion: true,
      label: '$outcomeLabel: $message',
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
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          // Sighted users read the widget tree, not the
                          // accessibility tree: the outcome must be visible
                          // on screen too, so "Run failed" and "Run
                          // cancelled" stay distinguishable without
                          // assistive technology.
                          Text(
                            outcomeLabel,
                            style: theme.textTheme.labelLarge?.copyWith(
                              color: colorScheme.onErrorContainer,
                              fontWeight: FontWeight.bold,
                            ),
                          ),
                          Text(
                            message,
                            style: theme.textTheme.bodyMedium?.copyWith(
                              color: colorScheme.onErrorContainer,
                            ),
                          ),
                        ],
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
