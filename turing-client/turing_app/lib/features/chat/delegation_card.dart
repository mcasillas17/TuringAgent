import 'package:flutter/material.dart';

import '../../models/delegation.dart';

/// One task Turing delegated, under the turn that asked for it: who is doing
/// what, the orchestrator's one-line summary, and — while the task waits on
/// the user — the approval it is waiting on.
class DelegationCard extends StatelessWidget {
  const DelegationCard({
    super.key,
    required this.delegation,
    this.onCancel,
    this.onViewWork,
    this.cancelling = false,
    this.approval,
    this.onAllowEgress,
    this.onDeclineEgress,
    this.deciding = false,
  });

  final Delegation delegation;

  /// Null once the task cannot be cancelled: it finished, or its
  /// conversation is gone.
  final VoidCallback? onCancel;
  final VoidCallback? onViewWork;
  final bool cancelling;

  /// The existing approval card for the approval this task waits on.
  final Widget? approval;

  /// While the task waits for consent to tools that leave the machine:
  /// review and allow them, or decline them. [deciding] holds both while a
  /// decision is open.
  final VoidCallback? onAllowEgress;
  final VoidCallback? onDeclineEgress;
  final bool deciding;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final muted = theme.colorScheme.onSurfaceVariant;
    final title = delegation.task.isEmpty
        ? delegation.specialist
        : '${delegation.specialist} · working on: ${delegation.task}';
    return Card(
      margin: const EdgeInsets.symmetric(vertical: 6),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(14, 12, 14, 6),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(_stateIcon(delegation.state), size: 18, color: muted),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    title,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.titleSmall,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 4),
            Semantics(
              liveRegion: true,
              child: Text(
                delegation.summary.isEmpty
                    ? delegation.state
                    : delegation.summary,
                style: theme.textTheme.bodySmall?.copyWith(color: muted),
              ),
            ),
            ?approval,
            if (delegation.waitingForConsent) ...[
              const SizedBox(height: 6),
              Text(
                'Wants to use: ${delegation.egressTools.join(', ')}',
                style: theme.textTheme.bodySmall,
              ),
            ],
            Wrap(
              spacing: 4,
              children: [
                if (delegation.waitingForConsent && onAllowEgress != null)
                  TextButton(
                    onPressed: deciding ? null : onAllowEgress,
                    child: const Text('Allow…'),
                  ),
                if (delegation.waitingForConsent && onDeclineEgress != null)
                  TextButton(
                    onPressed: deciding ? null : onDeclineEgress,
                    child: Text(
                      delegation.egressRequired
                          ? 'Decline (stops the task)'
                          : 'Run without',
                    ),
                  ),
                if (onCancel != null)
                  TextButton(
                    onPressed: cancelling ? null : onCancel,
                    child: const Text('Cancel'),
                  ),
                if (onViewWork != null)
                  TextButton(
                    onPressed: onViewWork,
                    child: const Text('View work'),
                  ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  static IconData _stateIcon(String state) => switch (state) {
    'queued' => Icons.schedule,
    'running' => Icons.autorenew,
    'waiting_approval' => Icons.pending_actions,
    'completed' => Icons.check_circle_outline,
    'failed' => Icons.error_outline,
    'cancelled' => Icons.block,
    _ => Icons.help_outline,
  };
}

/// Under an answer Turing wrote from its team's results: who helped. Tapping
/// it shows their cards.
class ContinuationChip extends StatelessWidget {
  const ContinuationChip({
    super.key,
    required this.delegations,
    required this.onPressed,
  });

  final List<Delegation> delegations;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return Align(
      alignment: Alignment.centerLeft,
      child: Padding(
        padding: const EdgeInsets.only(bottom: 10),
        child: ActionChip(
          avatar: const Icon(Icons.groups_outlined, size: 16),
          label: Text(
            'with ${delegations.map((delegation) => delegation.specialist).join(', ')}',
          ),
          onPressed: onPressed,
        ),
      ),
    );
  }
}
