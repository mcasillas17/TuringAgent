import 'package:flutter/material.dart';

/// Shared presentational chrome for a non-routine, error-styled outcome the
/// user must notice: a run failure, a run cancellation, or a `sendMessage`
/// attempt whose outcome could not be confirmed. None of these is routine
/// progress (a retry, a step, giving up after N attempts), so each gets this
/// same error-styled card rather than the neutral [RunNoticeCard]. Callers
/// are not limited to any fixed set — each supplies its own truthful
/// [outcomeLabel], so what is actually known (or not known) about ITS
/// outcome is never blurred into another's wording, and adding a future
/// caller never requires revisiting this chrome.
class TerminalOutcomeCard extends StatelessWidget {
  const TerminalOutcomeCard({
    super.key,
    required this.outcomeLabel,
    required this.message,
  });

  /// The truthful, human-readable name of the outcome this card reports
  /// (e.g. `Run failed`, `Run cancelled`, `Message send unconfirmed`).
  /// Rendered visibly above [message] so sighted users can tell different
  /// callers' outcomes apart on screen, and prefixed onto [message] to form
  /// the semantics label assistive technology announces.
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
                          // on screen too, so each caller's outcomeLabel
                          // stays distinguishable from every other's
                          // without assistive technology.
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
