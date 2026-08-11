import 'package:flutter/material.dart';

/// Lifecycle status of a model-driven tool call, mirrored from the
/// `tool.call.*` stream events emitted by the agent runtime.
enum ToolCallStatus { running, completed, failed, denied }

/// Presentational card that renders a single tool call's status inline in the
/// chat. Display-only: it takes plain values and holds no gRPC state (mirrors
/// [ApprovalCard], which keeps its interactive callbacks in the parent screen).
class ToolCallCard extends StatelessWidget {
  const ToolCallCard({
    super.key,
    required this.toolName,
    required this.status,
    this.serverName,
    this.error,
  });

  final String toolName;
  final ToolCallStatus status;
  final String? serverName;
  final String? error;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final hasServer = serverName != null && serverName!.isNotEmpty;
    final hasError =
        status == ToolCallStatus.failed && (error?.isNotEmpty ?? false);
    final qualifiedName = hasServer ? '$serverName / $toolName' : toolName;
    final semanticsLabel =
        'Tool call $qualifiedName: $_statusLabel'
        '${hasError ? '. Error: $error' : ''}';

    return Semantics(
      container: true,
      // The card is relabelled in place when the call resolves (running ->
      // completed/failed/denied) with no structural list change, which screen
      // readers do not announce on their own. Marking it live means a user who
      // has already moved focus past the card still hears the outcome.
      liveRegion: true,
      label: semanticsLabel,
      child: ExcludeSemantics(
        child: Align(
          alignment: Alignment.centerLeft,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 640),
            child: Card(
              margin: const EdgeInsets.symmetric(vertical: 4),
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(8),
              ),
              child: Padding(
                padding: const EdgeInsets.symmetric(
                  horizontal: 14,
                  vertical: 10,
                ),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        _leading(theme),
                        const SizedBox(width: 10),
                        Flexible(
                          child: Text(
                            toolName,
                            style: theme.textTheme.bodyMedium,
                          ),
                        ),
                        const SizedBox(width: 8),
                        Text(
                          _statusLabel,
                          style: theme.textTheme.labelSmall?.copyWith(
                            color: _statusColor(theme),
                          ),
                        ),
                      ],
                    ),
                    if (hasServer)
                      Padding(
                        padding: const EdgeInsets.only(left: 28, top: 2),
                        child: Text(
                          serverName!,
                          style: theme.textTheme.bodySmall,
                        ),
                      ),
                    if (hasError)
                      Padding(
                        padding: const EdgeInsets.only(left: 28, top: 4),
                        child: Text(
                          error!,
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: theme.colorScheme.error,
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

  String get _statusLabel {
    switch (status) {
      case ToolCallStatus.running:
        return 'Running';
      case ToolCallStatus.completed:
        return 'Completed';
      case ToolCallStatus.failed:
        return 'Failed';
      case ToolCallStatus.denied:
        return 'Denied';
    }
  }

  Color _statusColor(ThemeData theme) {
    switch (status) {
      case ToolCallStatus.running:
      case ToolCallStatus.completed:
        return theme.colorScheme.primary;
      case ToolCallStatus.failed:
      case ToolCallStatus.denied:
        return theme.colorScheme.error;
    }
  }

  Widget _leading(ThemeData theme) {
    switch (status) {
      case ToolCallStatus.running:
        return const SizedBox(
          width: 18,
          height: 18,
          child: CircularProgressIndicator(strokeWidth: 2),
        );
      case ToolCallStatus.completed:
        return Icon(
          Icons.check_circle_outline,
          size: 18,
          color: theme.colorScheme.primary,
        );
      case ToolCallStatus.failed:
        return Icon(
          Icons.error_outline,
          size: 18,
          color: theme.colorScheme.error,
        );
      case ToolCallStatus.denied:
        return Icon(Icons.block, size: 18, color: theme.colorScheme.error);
    }
  }
}
