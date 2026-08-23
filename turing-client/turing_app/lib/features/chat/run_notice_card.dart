import 'package:flutter/material.dart';

import '../../l10n/generated/app_localizations.dart';
import '../../l10n/run_state_localizations.dart';
import '../../models/run_state.dart';

/// Presentational notice for metadata emitted while an agent run is active.
///
/// Two distinct kinds of `agent.run.step` notice reach this widget:
///
///  - governed nonfailure notices (a redacted egress notice, a model
///    tool-iteration limit notice, ...) whose text the backend already
///    produces safely for display. These use the default [RunNoticeCard]
///    constructor and render [note] verbatim, exactly as before.
///  - the three allowlisted FAILURE-adjacent notice categories — a
///    dispatch retry, a recovery retry, or recovery giving up — use
///    [RunNoticeCard.localized] instead: no free-text `note` parameter
///    exists on that constructor at all, so it is structurally impossible
///    to hand it raw backend prose. Its label and bounded attempt counts are
///    always resolved through generated localization
///    ([localizedRunStepNotice]).
class RunNoticeCard extends StatelessWidget {
  const RunNoticeCard({super.key, required String note})
    : _note = note,
      _category = null,
      _attempt = 0,
      _maxAttempts = 0;

  const RunNoticeCard.localized({
    super.key,
    required RunStepNoticeCategory category,
    required int attempt,
    required int maxAttempts,
  }) : _note = null,
       _category = category,
       _attempt = attempt,
       _maxAttempts = maxAttempts;

  final String? _note;
  final RunStepNoticeCategory? _category;
  final int _attempt;
  final int _maxAttempts;

  /// Defensive, belt-and-suspenders bound: even a malformed/negative
  /// attempt count from the caller never reaches the localized copy as a
  /// raw negative number.
  static int _bounded(int value) => value.clamp(0, maxRunStepNoticeAttempts);

  @override
  Widget build(BuildContext context) {
    final category = _category;
    final text = category == null
        ? _note!
        : localizedRunStepNotice(
            AppLocalizations.of(context),
            category,
            attempt: _bounded(_attempt),
            maxAttempts: _bounded(_maxAttempts),
          );
    final theme = Theme.of(context);
    final foreground = theme.colorScheme.onSurfaceVariant;

    return Semantics(
      container: true,
      liveRegion: true,
      label: text,
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
                      text,
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
