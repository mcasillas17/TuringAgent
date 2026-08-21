import 'package:flutter/material.dart';

import '../../l10n/generated/app_localizations.dart';
import '../../l10n/run_state_localizations.dart';
import '../../models/run_lifecycle.dart';
import '../../models/run_state.dart';
import 'run_cancelled_card.dart';
import 'run_failure_card.dart';

/// Turns one canonical, reconciled [RunState] into the exact adjacent card
/// the design's timeline-rendering matrix calls for.
///
/// This is the single place the matrix lives for anything that is not a
/// bare completed bubble:
///
///  - failed/cancelled: delegates to [RunFailureCard]/[RunCancelledCard],
///    which resolve their own truthful label and detail from
///    [state.outcomeReason] — never from raw backend text;
///  - completed without displayable content: a neutral completion notice,
///    never a blank success card;
///  - queued/running/waiting approval/recovering: the matching nonterminal
///    status notice;
///  - unspecified/unknown (an old client reading a lifecycle it cannot
///    name, or a defensively decoded unrecognized value): a truthful
///    "status unavailable" notice, never the raw numeric enum.
///
/// Never rendered for a completed run WITH displayable content — that case
/// is the assistant bubble alone, with no redundant card, and callers
/// (`ChatScreen`) must not construct this widget for it.
class RunStateCard extends StatelessWidget {
  const RunStateCard({
    super.key,
    required this.state,
    this.responseContentUnavailable = false,
  });

  final RunState state;
  final bool responseContentUnavailable;

  @override
  Widget build(BuildContext context) {
    switch (state.lifecycle) {
      case RunLifecycle.failed:
        return RunFailureCard(reason: state.outcomeReason);
      case RunLifecycle.cancelled:
        return RunCancelledCard(reason: state.outcomeReason);
      case RunLifecycle.queued:
      case RunLifecycle.running:
      case RunLifecycle.waitingApproval:
      case RunLifecycle.recovering:
      case RunLifecycle.completed:
      case RunLifecycle.unspecified:
      case RunLifecycle.unknown:
        final l10n = AppLocalizations.of(context);
        final copy =
            state.lifecycle == RunLifecycle.completed &&
                responseContentUnavailable
            ? localizedCompletedContentUnavailableCopy(l10n)
            : localizedRunStateCopy(l10n, state);
        return _NeutralRunStatusCard(title: copy.title, detail: copy.detail);
    }
  }
}

/// The terminal-only neutral legacy-correlation fallback: shown beside an
/// assistant row that has no usable [RunState] at all (a pre-TUR-009
/// legacy message, or one whose run state failed to decode) and no
/// displayable content either. Distinct from [RunStateCard]'s own
/// completed-no-content case — this one has no run state to reason about
/// at all, so its copy is fixed rather than derived from any lifecycle or
/// outcome.
class NoResponseCard extends StatelessWidget {
  const NoResponseCard({super.key});

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final copy = localizedNoResponseCopy(l10n);
    return _NeutralRunStatusCard(title: copy.title, detail: copy.detail);
  }
}

/// Shared neutral (non-error) chrome for [RunStateCard]'s nonterminal/
/// no-content branches and [NoResponseCard]. Deliberately distinct from
/// [TerminalOutcomeCard]'s error styling: none of these represent a
/// failure or cancellation, so none should look like one.
class _NeutralRunStatusCard extends StatelessWidget {
  const _NeutralRunStatusCard({required this.title, required this.detail});

  final String title;
  final String detail;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colorScheme = theme.colorScheme;

    return Semantics(
      container: true,
      liveRegion: true,
      label: '$title: $detail',
      child: ExcludeSemantics(
        child: Align(
          alignment: Alignment.centerLeft,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 640),
            child: Card(
              color: colorScheme.surfaceContainerHighest,
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
                      Icons.info_outline,
                      size: 18,
                      color: colorScheme.onSurfaceVariant,
                    ),
                    const SizedBox(width: 10),
                    Flexible(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Text(
                            title,
                            style: theme.textTheme.labelLarge?.copyWith(
                              color: colorScheme.onSurfaceVariant,
                              fontWeight: FontWeight.bold,
                            ),
                          ),
                          Text(
                            detail,
                            style: theme.textTheme.bodyMedium?.copyWith(
                              color: colorScheme.onSurfaceVariant,
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
