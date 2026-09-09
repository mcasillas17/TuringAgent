import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grpc/grpc.dart' show GrpcError;
import 'package:turing_flutter_app/features/chat/run_stop_control.dart';
import 'package:turing_flutter_app/features/chat/run_cancellation_controller.dart';
import 'package:turing_flutter_app/l10n/generated/app_localizations.dart';
import 'package:turing_flutter_app/models/run_cancellation.dart';
import 'package:turing_flutter_app/models/run_lifecycle.dart';
import 'package:turing_flutter_app/models/run_state.dart';
import 'package:turing_flutter_app/networking/api_client.dart';

final _time = DateTime.utc(2026, 9, 9);

RunState _state({
  String runId = 'run_1',
  RunLifecycle lifecycle = RunLifecycle.running,
  int version = 2,
}) => RunState(
  runId: runId,
  userMessageId: 'user_$runId',
  assistantMessageId: 'assistant_$runId',
  lifecycle: lifecycle,
  outcomeReason: lifecycle == RunLifecycle.cancelled
      ? RunOutcomeReason.userCancelled
      : RunOutcomeReason.none,
  stateVersion: version,
  stateUpdatedAt: _time,
  finishedAt:
      lifecycle == RunLifecycle.cancelled || lifecycle == RunLifecycle.completed
      ? _time
      : null,
  hasDisplayableContent: false,
);

class _Api extends TuringApi {
  RunCancellationStatus status = RunCancellationStatus(
    available: true,
    runState: _state(),
    progress: CancellationProgress.notCancelled,
  );
  Exception? readError;
  Completer<RunCancellationStatus>? readGate;
  Exception? cancelError;
  Completer<CancelRunReceipt>? gate;
  final calls = <({String sessionId, String runId, String key})>[];

  @override
  Future<RunCancellationStatus> getRunCancellation({
    required String sessionId,
    required String runId,
  }) async {
    if (readError case final error?) throw error;
    if (readGate case final gate?) return gate.future;
    return status;
  }

  @override
  Future<CancelRunReceipt> cancelRun({
    required String sessionId,
    required String runId,
    required String idempotencyKey,
  }) async {
    calls.add((sessionId: sessionId, runId: runId, key: idempotencyKey));
    if (cancelError case final error?) throw error;
    if (gate case final pending?) return pending.future;
    return CancelRunReceipt(
      result: CancelRunResult.accepted,
      runState: _state(lifecycle: RunLifecycle.cancelled, version: 3),
      progress: CancellationProgress.stopping,
    );
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}

Widget _host(
  _Api api, {
  RunState? state,
  ValueChanged<RunState>? onState,
  bool connected = true,
}) => MaterialApp(
  localizationsDelegates: AppLocalizations.localizationsDelegates,
  supportedLocales: AppLocalizations.supportedLocales,
  home: Scaffold(
    body: _ControlHost(
      key: const ValueKey('control'),
      api: api,
      sessionId: 'session_1',
      state: state ?? _state(),
      connected: connected,
      onRunState: onState ?? (_) {},
    ),
  ),
);

class _ControlHost extends StatefulWidget {
  const _ControlHost({
    super.key,
    required this.api,
    required this.sessionId,
    required this.state,
    required this.connected,
    required this.onRunState,
  });

  final TuringApi api;
  final String sessionId;
  final RunState state;
  final bool connected;
  final ValueChanged<RunState> onRunState;

  @override
  State<_ControlHost> createState() => _ControlHostState();
}

class _ControlHostState extends State<_ControlHost> {
  late RunCancellationController controller = _create();

  RunCancellationController _create() => RunCancellationController(
    api: widget.api,
    sessionId: widget.sessionId,
    state: widget.state,
    onRunState: (state) => widget.onRunState(state),
  );

  @override
  void didUpdateWidget(_ControlHost oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.api != widget.api ||
        oldWidget.state.runId != widget.state.runId ||
        oldWidget.sessionId != widget.sessionId) {
      controller.dispose();
      controller = _create();
    } else {
      controller.updateState(widget.state);
      if (!oldWidget.connected && widget.connected) controller.reconnect();
    }
  }

  @override
  Widget build(BuildContext context) => RunStopControl(controller: controller);

  @override
  void dispose() {
    controller.dispose();
    super.dispose();
  }
}

void main() {
  testWidgets('status probe reconciles an unseen approval and resume cycle', (
    tester,
  ) async {
    final api = _Api()
      ..status = RunCancellationStatus(
        available: true,
        runState: _state(version: 4),
        progress: CancellationProgress.notCancelled,
      );
    final states = <RunState>[];
    await tester.pumpWidget(
      _host(api, state: _state(version: 2), onState: states.add),
    );
    await tester.pumpAndSettle();
    expect(find.text('Stop'), findsOneWidget);
    expect(states.single.stateVersion, 4);
  });

  testWidgets('a stale probe preserves Stop and the newer live state', (
    tester,
  ) async {
    final api = _Api()..readGate = Completer<RunCancellationStatus>();
    final states = <RunState>[];
    await tester.pumpWidget(
      _host(
        api,
        state: _state(lifecycle: RunLifecycle.queued, version: 2),
        onState: states.add,
      ),
    );
    await tester.pumpWidget(
      _host(api, state: _state(version: 3), onState: states.add),
    );
    api.readGate!.complete(
      RunCancellationStatus(
        available: true,
        runState: _state(lifecycle: RunLifecycle.queued, version: 2),
        progress: CancellationProgress.notCancelled,
      ),
    );
    await tester.pumpAndSettle();
    expect(find.text('Stop'), findsOneWidget);
    expect(
      find.text('Cancellation status could not be confirmed.'),
      findsNothing,
    );
    expect(states.where((state) => state.stateVersion == 2), isEmpty);
  });

  testWidgets('Stop targets exact run and suppresses repeated taps', (
    tester,
  ) async {
    final api = _Api()..gate = Completer<CancelRunReceipt>();
    final states = <RunState>[];
    await tester.pumpWidget(_host(api, onState: states.add));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Stop'));
    await tester.pump();
    await tester.tap(find.byKey(const ValueKey('stop-run_1')));
    expect(api.calls, [
      (sessionId: 'session_1', runId: 'run_1', key: 'cancel:run_1'),
    ]);
    expect(find.text('Cancellation not yet confirmed.'), findsOneWidget);
    expect(states.where((state) => state.isTerminal), isEmpty);
    api.gate!.complete(
      CancelRunReceipt(
        result: CancelRunResult.accepted,
        runState: _state(lifecycle: RunLifecycle.cancelled, version: 3),
        progress: CancellationProgress.stopping,
      ),
    );
    await tester.pumpAndSettle();
    expect(states.last.lifecycle, RunLifecycle.cancelled);
    expect(
      find.textContaining('Worker shutdown is not yet confirmed'),
      findsOneWidget,
    );
    expect(find.text('Stop'), findsNothing);
  });

  testWidgets(
    'lost response retries same identity without inventing cancellation',
    (tester) async {
      final api = _Api()
        ..cancelError = const GrpcError.unavailable('private diagnostic');
      final states = <RunState>[];
      await tester.pumpWidget(_host(api, onState: states.add));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Stop'));
      await tester.pumpAndSettle();
      expect(find.text('Cancellation not yet confirmed.'), findsOneWidget);
      expect(find.textContaining('private diagnostic'), findsNothing);
      expect(states.where((state) => state.isTerminal), isEmpty);
      api.cancelError = null;
      await tester.tap(find.text('Retry cancellation'));
      await tester.pumpAndSettle();
      expect(api.calls.map((call) => call.key).toSet(), {'cancel:run_1'});
      expect(api.calls, hasLength(2));
    },
  );

  testWidgets('Stop is an accessible button and supports keyboard activation', (
    tester,
  ) async {
    final handle = tester.ensureSemantics();
    final api = _Api();
    await tester.pumpWidget(_host(api));
    await tester.pumpAndSettle();
    final button = find.byKey(const ValueKey('stop-run_1'));
    expect(
      tester.getSemantics(button),
      matchesSemantics(
        label: 'Stop',
        isButton: true,
        hasEnabledState: true,
        isEnabled: true,
        isFocusable: true,
        hasTapAction: true,
        hasFocusAction: true,
      ),
    );
    await tester.sendKeyEvent(LogicalKeyboardKey.tab);
    await tester.sendKeyEvent(LogicalKeyboardKey.enter);
    await tester.pumpAndSettle();
    expect(api.calls, hasLength(1));
    handle.dispose();
  });

  testWidgets(
    'cancel RPC unsupported after a successful probe stays explicit',
    (tester) async {
      final api = _Api()
        ..cancelError = const GrpcError.unimplemented('old server');
      await tester.pumpWidget(_host(api));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Stop'));
      await tester.pumpAndSettle();
      expect(
        find.text('Stopping runs is not supported by this backend.'),
        findsOneWidget,
      );
      expect(find.text('Retry cancellation'), findsNothing);
    },
  );

  testWidgets('permission rejection is not displayed as accepted or lost', (
    tester,
  ) async {
    final api = _Api()
      ..cancelError = const GrpcError.permissionDenied('private');
    await tester.pumpWidget(_host(api));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Stop'));
    await tester.pumpAndSettle();
    expect(
      find.text(
        'Cancellation was not accepted. Check status before trying again.',
      ),
      findsOneWidget,
    );
    expect(find.text('Cancellation not yet confirmed.'), findsNothing);
    expect(find.text('Stop'), findsNothing);
  });

  testWidgets('navigation without Stop never sends a cancellation', (
    tester,
  ) async {
    final api = _Api();
    await tester.pumpWidget(_host(api));
    await tester.pumpAndSettle();
    await tester.pumpWidget(const SizedBox.shrink());
    expect(api.calls, isEmpty);
  });

  for (final error in <Exception>[
    const GrpcError.unimplemented('unknown method'),
    const TuringApiException(code: 'run_cancel_unsupported', message: 'legacy'),
  ]) {
    testWidgets(
      'unsupported backend is explicit and never sends a cancel: $error',
      (tester) async {
        final api = _Api()..readError = error;
        await tester.pumpWidget(_host(api));
        await tester.pumpAndSettle();
        expect(
          find.text('Stopping runs is not supported by this backend.'),
          findsOneWidget,
        );
        expect(find.text('Stop'), findsNothing);
        expect(api.calls, isEmpty);
      },
    );
  }

  testWidgets('read errors can be retried without fabricating unsupported', (
    tester,
  ) async {
    final api = _Api()..readError = const GrpcError.unavailable('private');
    await tester.pumpWidget(_host(api));
    await tester.pumpAndSettle();
    expect(
      find.text('Cancellation status could not be confirmed.'),
      findsOneWidget,
    );
    expect(find.text('Stop'), findsNothing);
    api.readError = null;
    await tester.tap(find.text('Check status'));
    await tester.pumpAndSettle();
    expect(find.text('Stop'), findsOneWidget);
  });

  testWidgets('completion winning cancellation preserves completion', (
    tester,
  ) async {
    final api = _Api()..gate = Completer<CancelRunReceipt>();
    final states = <RunState>[];
    await tester.pumpWidget(_host(api, onState: states.add));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Stop'));
    api.gate!.complete(
      CancelRunReceipt(
        result: CancelRunResult.alreadyTerminal,
        runState: _state(lifecycle: RunLifecycle.completed, version: 3),
        progress: CancellationProgress.notCancelled,
      ),
    );
    await tester.pumpAndSettle();
    expect(states.last.lifecycle, RunLifecycle.completed);
    expect(find.text('The run had already ended.'), findsOneWidget);
    expect(find.text('Stop'), findsNothing);
  });

  testWidgets(
    'terminal event cannot be overwritten by a late cancel response',
    (tester) async {
      final api = _Api()..gate = Completer<CancelRunReceipt>();
      final states = <RunState>[];
      await tester.pumpWidget(_host(api, onState: states.add));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Stop'));
      final completed = _state(lifecycle: RunLifecycle.completed, version: 3);
      await tester.pumpWidget(
        _host(api, state: completed, onState: states.add),
      );
      api.gate!.complete(
        CancelRunReceipt(
          result: CancelRunResult.accepted,
          runState: _state(lifecycle: RunLifecycle.cancelled, version: 3),
          progress: CancellationProgress.stopping,
        ),
      );
      await tester.pumpAndSettle();
      expect(
        states.where((state) => state.lifecycle == RunLifecycle.cancelled),
        isEmpty,
      );
      expect(find.textContaining('Worker shutdown'), findsNothing);
    },
  );

  testWidgets('changing client rechecks cancellation support', (tester) async {
    final api = _Api()
      ..readError = const GrpcError.unimplemented('older backend');
    await tester.pumpWidget(_host(api));
    await tester.pumpAndSettle();
    expect(find.text('Stop'), findsNothing);
    final upgraded = _Api();
    await tester.pumpWidget(_host(upgraded));
    await tester.pumpAndSettle();
    expect(find.text('Stop'), findsOneWidget);
  });

  testWidgets(
    'reopen reads stopping then reconciliation without a user action',
    (tester) async {
      final cancelled = _state(lifecycle: RunLifecycle.cancelled, version: 3);
      final api = _Api()
        ..status = RunCancellationStatus(
          available: true,
          runState: cancelled,
          progress: CancellationProgress.stopping,
        );
      await tester.pumpWidget(_host(api, state: cancelled));
      await tester.pumpAndSettle();
      expect(
        find.textContaining('Worker shutdown is not yet confirmed'),
        findsOneWidget,
      );
      api.status = RunCancellationStatus(
        available: true,
        runState: cancelled,
        progress: CancellationProgress.reconciled,
      );
      await tester.tap(find.text('Check status'));
      await tester.pumpAndSettle();
      expect(
        find.textContaining('Execution is no longer held'),
        findsOneWidget,
      );
      expect(api.calls, isEmpty);
    },
  );

  testWidgets(
    'abandoned execution does not claim an accepted user cancellation',
    (tester) async {
      final abandoned = _state(
        lifecycle: RunLifecycle.cancelled,
        version: 3,
      ).copyWith(outcomeReason: RunOutcomeReason.abandoned);
      final api = _Api()
        ..status = RunCancellationStatus(
          available: true,
          runState: abandoned,
          progress: CancellationProgress.stopping,
        );
      await tester.pumpWidget(_host(api, state: abandoned));
      await tester.pumpAndSettle();
      expect(find.textContaining('Cancellation accepted'), findsNothing);
      expect(
        find.textContaining('Worker shutdown is not yet confirmed'),
        findsOneWidget,
      );
      expect(api.calls, isEmpty);
    },
  );

  testWidgets('unavailable target has no stop action or inferred outcome', (
    tester,
  ) async {
    final api = _Api()..status = const RunCancellationStatus(available: false);
    await tester.pumpWidget(_host(api));
    await tester.pumpAndSettle();
    expect(find.text('This run is unavailable.'), findsOneWidget);
    expect(find.text('Stop'), findsNothing);
    expect(api.calls, isEmpty);
  });

  testWidgets('dispose does not send cancellation and ignores late responses', (
    tester,
  ) async {
    final api = _Api()..gate = Completer<CancelRunReceipt>();
    final states = <RunState>[];
    await tester.pumpWidget(_host(api, onState: states.add));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Stop'));
    await tester.pumpWidget(const SizedBox.shrink());
    api.gate!.complete(
      CancelRunReceipt(
        result: CancelRunResult.accepted,
        runState: _state(lifecycle: RunLifecycle.cancelled, version: 3),
        progress: CancellationProgress.reconciled,
      ),
    );
    await tester.pumpAndSettle();
    expect(api.calls, hasLength(1));
    expect(states.where((state) => state.isTerminal), isEmpty);
    expect(tester.takeException(), isNull);
  });

  testWidgets(
    'reconnect re-reads committed cancellation without a model or send',
    (tester) async {
      final api = _Api()..readError = const GrpcError.unavailable('offline');
      await tester.pumpWidget(_host(api, connected: false));
      await tester.pumpAndSettle();
      api.readError = null;
      api.status = RunCancellationStatus(
        available: true,
        runState: _state(lifecycle: RunLifecycle.cancelled, version: 3),
        progress: CancellationProgress.reconciled,
      );
      await tester.pumpWidget(_host(api));
      await tester.pumpAndSettle();
      expect(
        find.textContaining('Execution is no longer held'),
        findsOneWidget,
      );
      expect(api.calls, isEmpty);
    },
  );
}
