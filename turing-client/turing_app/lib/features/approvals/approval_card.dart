import 'package:flutter/material.dart';

class ApprovalCard extends StatelessWidget {
  const ApprovalCard({
    super.key,
    required this.toolName,
    required this.argsSummary,
    required this.onApprove,
    required this.onDeny,
    this.busy = false,
  });

  final String toolName;
  final String argsSummary;
  final VoidCallback onApprove;
  final VoidCallback onDeny;

  /// True while a decision for this approval is already in flight — the
  /// caller has an `approveApproval`/`denyApproval` RPC outstanding for it.
  /// Disables BOTH actions, not just the one pressed: the two decisions are
  /// mutually exclusive for one approval, so a Deny tap must not race an
  /// in-flight Approve (or vice versa) for the same one. Defaults to
  /// `false` so existing callers that never pass it keep working exactly as
  /// before.
  final bool busy;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.fromLTRB(12, 8, 12, 4),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(8)),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(
              'Approval requested: $toolName',
              style: Theme.of(context).textTheme.titleMedium,
            ),
            if (argsSummary.isNotEmpty) ...[
              const SizedBox(height: 8),
              Text(argsSummary),
            ],
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 8,
              children: [
                FilledButton.icon(
                  onPressed: busy ? null : onApprove,
                  icon: const Icon(Icons.check),
                  label: const Text('Approve'),
                ),
                OutlinedButton.icon(
                  onPressed: busy ? null : onDeny,
                  icon: const Icon(Icons.close),
                  label: const Text('Deny'),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
