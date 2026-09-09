import 'run_state.dart';

enum CancelRunResult { unknown, accepted, alreadyTerminal, unavailable }

enum CancellationProgress { unknown, notCancelled, stopping, reconciled }

class CancelRunReceipt {
  const CancelRunReceipt({
    required this.result,
    this.runState,
    this.progress = CancellationProgress.unknown,
  });

  final CancelRunResult result;
  final RunState? runState;
  final CancellationProgress progress;
}

class RunCancellationStatus {
  const RunCancellationStatus({
    required this.available,
    this.runState,
    this.progress = CancellationProgress.unknown,
  });

  final bool available;
  final RunState? runState;
  final CancellationProgress progress;
}
