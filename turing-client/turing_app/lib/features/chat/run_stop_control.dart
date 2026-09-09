import 'package:flutter/material.dart';

import '../../l10n/generated/app_localizations.dart';
import '../../models/run_cancellation.dart';
import '../../models/run_lifecycle.dart';
import 'run_cancellation_controller.dart';

class RunStopControl extends StatefulWidget {
  const RunStopControl({super.key, required this.controller});

  final RunCancellationController controller;

  @override
  State<RunStopControl> createState() => _RunStopControlState();
}

class _RunStopControlState extends State<RunStopControl> {
  @override
  void initState() {
    super.initState();
    widget.controller.initialize();
  }

  @override
  void didUpdateWidget(RunStopControl oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.controller != widget.controller) {
      widget.controller.initialize();
    }
  }

  @override
  Widget build(BuildContext context) => ListenableBuilder(
    listenable: widget.controller,
    builder: (context, _) {
      final control = widget.controller;
      final l10n = AppLocalizations.of(context);
      final cancelled = control.state.lifecycle == RunLifecycle.cancelled;
      if (!control.cancellable &&
          !cancelled &&
          control.notice != CancellationNotice.alreadyTerminal) {
        return const SizedBox.shrink();
      }
      final String? notice = switch (control.notice) {
        CancellationNotice.unsupported => l10n.runCancelUnsupported,
        CancellationNotice.unavailable => l10n.runCancelUnavailable,
        CancellationNotice.statusFailed => l10n.runCancelStatusFailed,
        _ when cancelled => switch (control.progress) {
          CancellationProgress.stopping => l10n.runCancelStopping,
          CancellationProgress.reconciled => l10n.runCancelReconciled,
          _ => l10n.runCancelShutdownUnknown,
        },
        CancellationNotice.alreadyTerminal => l10n.runCancelAlreadyTerminal,
        CancellationNotice.rejected => l10n.runCancelRejected,
        CancellationNotice.unconfirmed => l10n.runCancelUnconfirmed,
        CancellationNotice.none => null,
      };
      return Align(
        alignment: Alignment.centerLeft,
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 4),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (notice != null)
                Semantics(liveRegion: true, child: Text(notice)),
              Wrap(
                spacing: 8,
                children: [
                  if (control.canStop)
                    TextButton.icon(
                      key: ValueKey('stop-${control.runId}'),
                      onPressed: control.busy ? null : control.stop,
                      icon: const Icon(Icons.stop_circle_outlined),
                      label: Text(
                        control.notice == CancellationNotice.unconfirmed &&
                                !control.busy
                            ? l10n.runCancelRetry
                            : l10n.runStop,
                      ),
                    ),
                  if (control.canRefresh)
                    TextButton(
                      onPressed: control.busy ? null : control.refresh,
                      child: Text(l10n.runCancelCheck),
                    ),
                  if (control.busy)
                    Semantics(
                      label: l10n.runCancelChecking,
                      child: const Padding(
                        padding: EdgeInsets.all(12),
                        child: SizedBox.square(
                          dimension: 16,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        ),
                      ),
                    ),
                ],
              ),
            ],
          ),
        ),
      );
    },
  );
}
