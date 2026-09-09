import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:grpc/grpc.dart' show GrpcError, StatusCode;

import '../../models/run_cancellation.dart';
import '../../models/run_lifecycle.dart';
import '../../models/run_state.dart';
import '../../networking/api_client.dart';
import 'run_state_reconciler.dart';

enum CancellationNotice {
  none,
  unsupported,
  unavailable,
  statusFailed,
  unconfirmed,
  rejected,
  alreadyTerminal,
}

/// Screen-owned request state. Moving or recycling a message bubble must not
/// discard an in-flight cancellation or its authoritative response.
class RunCancellationController extends ChangeNotifier {
  RunCancellationController({
    required this.api,
    required this.sessionId,
    required RunState state,
    required this.onRunState,
  }) : runId = state.runId {
    _states.reconcile(state);
  }

  final TuringApi api;
  final String sessionId;
  final String runId;
  final ValueChanged<RunState> onRunState;
  final _states = RunStateReconciler();
  bool _disposed = false;
  bool _initialized = false;
  bool _busy = false;
  bool _supported = false;
  bool _refreshAfter = false;
  CancellationNotice _notice = CancellationNotice.none;
  CancellationProgress _progress = CancellationProgress.unknown;

  RunState get state => _states.stateFor(runId)!;
  bool get busy => _busy;
  CancellationNotice get notice => _notice;
  CancellationProgress get progress => _progress;
  bool get cancellable => switch (state.lifecycle) {
    RunLifecycle.queued ||
    RunLifecycle.running ||
    RunLifecycle.waitingApproval ||
    RunLifecycle.recovering => true,
    _ => false,
  };
  bool get canStop =>
      cancellable &&
      _supported &&
      (_notice == CancellationNotice.none ||
          _notice == CancellationNotice.unconfirmed);
  bool get canRefresh =>
      _notice != CancellationNotice.unsupported &&
      _notice != CancellationNotice.unavailable &&
      (_notice == CancellationNotice.statusFailed ||
          _notice == CancellationNotice.unconfirmed ||
          _notice == CancellationNotice.rejected ||
          state.lifecycle == RunLifecycle.cancelled &&
              _progress != CancellationProgress.reconciled);

  void initialize() {
    if (_initialized || _disposed) return;
    _initialized = true;
    if (cancellable || state.lifecycle == RunLifecycle.cancelled) {
      unawaited(refresh());
    }
  }

  void updateState(RunState incoming) {
    if (_disposed || incoming.runId != runId) return;
    final wasCancelled = state.lifecycle == RunLifecycle.cancelled;
    final result = _states.reconcile(incoming);
    if (!result.isAccepted) return;
    if (state.isTerminal) _notice = CancellationNotice.none;
    notifyListeners();
    if (!wasCancelled && state.lifecycle == RunLifecycle.cancelled) reconnect();
  }

  void reconnect() {
    if (!_initialized ||
        _disposed ||
        (!cancellable && state.lifecycle != RunLifecycle.cancelled)) {
      return;
    }
    if (_busy) {
      _refreshAfter = true;
    } else {
      unawaited(refresh());
    }
  }

  RunStateReconciliationOutcome _adopt(RunState? incoming) {
    if (incoming == null ||
        incoming.runId != runId ||
        incoming.assistantMessageId != state.assistantMessageId ||
        incoming.userMessageId != state.userMessageId) {
      return RunStateReconciliationOutcome.inconsistent;
    }
    final result = _states.reconcile(incoming);
    if (result.isAccepted) onRunState(incoming);
    return result.outcome;
  }

  void _finish() {
    if (_disposed) return;
    _busy = false;
    notifyListeners();
    if (_refreshAfter) {
      _refreshAfter = false;
      unawaited(refresh());
    }
  }

  Future<void> refresh() async {
    if (_busy || _disposed) return;
    _busy = true;
    final wasUnconfirmed = _notice == CancellationNotice.unconfirmed;
    notifyListeners();
    try {
      final status = await api.getRunCancellation(
        sessionId: sessionId,
        runId: runId,
      );
      if (_disposed) return;
      _supported = true;
      if (!status.available) {
        _notice = CancellationNotice.unavailable;
      } else {
        final outcome = _adopt(status.runState);
        switch (outcome) {
          case RunStateReconciliationOutcome.accepted:
          case RunStateReconciliationOutcome.duplicate:
            _progress = status.progress;
            _notice = wasUnconfirmed && !state.isTerminal
                ? CancellationNotice.unconfirmed
                : CancellationNotice.none;
          case RunStateReconciliationOutcome.stale:
            // A valid older snapshot proves support, not the current phase or
            // shutdown progress. Keep the newer live observation.
            _notice = wasUnconfirmed && !state.isTerminal
                ? CancellationNotice.unconfirmed
                : CancellationNotice.none;
          case RunStateReconciliationOutcome.inconsistent:
          case RunStateReconciliationOutcome.unloaded:
            _notice = CancellationNotice.statusFailed;
        }
      }
    } on Exception catch (error) {
      if (_disposed) return;
      _notice = _isUnsupported(error)
          ? CancellationNotice.unsupported
          : CancellationNotice.statusFailed;
    } finally {
      _finish();
    }
  }

  Future<void> stop() async {
    if (_busy || _disposed || !canStop) return;
    _busy = true;
    _notice = CancellationNotice.unconfirmed;
    notifyListeners();
    try {
      final receipt = await api.cancelRun(
        sessionId: sessionId,
        runId: runId,
        // Stable across retries and reopening; a run can be stopped only once.
        idempotencyKey: 'cancel:$runId',
      );
      if (_disposed) return;
      if (receipt.result == CancelRunResult.unavailable) {
        _notice = CancellationNotice.unavailable;
      } else if (receipt.result != CancelRunResult.unknown) {
        final outcome = _adopt(receipt.runState);
        if (outcome == RunStateReconciliationOutcome.accepted ||
            outcome == RunStateReconciliationOutcome.duplicate) {
          _progress = receipt.progress;
          _notice = receipt.result == CancelRunResult.alreadyTerminal
              ? CancellationNotice.alreadyTerminal
              : CancellationNotice.none;
        } else if (state.isTerminal) {
          _notice = CancellationNotice.none;
        }
      }
    } on Exception catch (error) {
      if (_disposed) return;
      if (_isUnsupported(error)) {
        _notice = CancellationNotice.unsupported;
      } else if (error is GrpcError &&
          const {
            StatusCode.invalidArgument,
            StatusCode.unauthenticated,
            StatusCode.permissionDenied,
            StatusCode.alreadyExists,
            StatusCode.failedPrecondition,
          }.contains(error.code)) {
        _notice = CancellationNotice.rejected;
      }
    } finally {
      _finish();
    }
  }

  bool _isUnsupported(Exception error) =>
      error is GrpcError && error.code == StatusCode.unimplemented ||
      error is TuringApiException && error.code == 'run_cancel_unsupported';

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }
}
