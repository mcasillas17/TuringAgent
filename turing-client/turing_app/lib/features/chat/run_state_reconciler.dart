import '../../models/run_lifecycle.dart';
import '../../models/run_state.dart';

/// What happened when a [RunState] snapshot was offered to a
/// [RunStateReconciler].
///
/// These mirror, exactly, the six reconciliation rules from the design
/// document (`docs/superpowers/specs/2026-08-20-tur-009-reopenable-run-
/// outcomes-design.md`, "Flutter Reconstruction and Reconciliation"),
/// evaluated in this fixed order — never by event arrival order,
/// `finishedAt`, or lifecycle "rank":
///
///  1. no existing state -> [accepted] for any (already-validated, nonzero)
///     version;
///  2. a lower version than the one already held -> [stale];
///  3. an equal version carrying byte-for-byte identical state (including
///     an identical TERMINAL state) -> [duplicate], a semantic no-op;
///  4. an equal version carrying different state -> [inconsistent];
///  5. a higher version arriving after an existing terminal state ->
///     [inconsistent] — nothing may ever follow a terminal state, not even
///     one that looks like a structurally valid transition;
///  6. a higher version from a nonterminal state -> [accepted] only if
///     [RunLifecycle.canTransitionTo] allows that exact edge, otherwise
///     [inconsistent].
enum RunStateReconciliationOutcome {
  /// The incoming snapshot is now this run's accepted, current state.
  accepted,

  /// The incoming snapshot's version is lower than the one already held.
  /// Ignored: the existing state is left untouched.
  stale,

  /// The incoming snapshot exactly matches the state already held, at the
  /// same version. A safe no-op, not an error.
  duplicate,

  /// The incoming snapshot cannot be reconciled with the state already
  /// held — same version with different content, a version arriving after
  /// a terminal state, or a version bump the backend's own lifecycle graph
  /// could never have committed. The existing state is left untouched.
  inconsistent,

  /// The screen has no loaded assistant row to which this run's state can
  /// honestly attach. The snapshot is not retained; callers may coalesce a
  /// newest-page resync instead.
  unloaded,
}

/// The result of one [RunStateReconciler.reconcile] call: what happened,
/// and the state to treat as this run's current truth afterward.
class RunStateReconciliationResult {
  const RunStateReconciliationResult({
    required this.outcome,
    required this.current,
  });

  final RunStateReconciliationOutcome outcome;

  /// The state to render for this run after this call: the incoming state
  /// for [RunStateReconciliationOutcome.accepted] or
  /// [RunStateReconciliationOutcome.duplicate]; the previously held state,
  /// unchanged, for [RunStateReconciliationOutcome.stale] or
  /// [RunStateReconciliationOutcome.inconsistent], or null when an unloaded
  /// or invalid first snapshot was not retained.
  final RunState? current;

  /// Whether this call actually moved this run's accepted state. A duplicate
  /// is a semantic no-op and must not trigger rendering side effects.
  bool get isAccepted => outcome == RunStateReconciliationOutcome.accepted;
}

/// Owns exactly one accepted [RunState] per run ID, and decides whether an
/// incoming snapshot may replace it.
///
/// Pure and side-effect free: no widget, stream, timer, or API dependency,
/// so it is fully unit-testable without pumping a single frame. History
/// (message-page) snapshots and live/replayed event snapshots are
/// reconciled through this exact same [reconcile] method — the design's
/// "history and live events use this same path" requirement — so nothing
/// about a snapshot's ORIGIN, only its `runId`/`stateVersion`/content, ever
/// affects the outcome.
class RunStateReconciler {
  final Map<String, RunState> _accepted = {};

  /// The currently accepted state for [runId], or null if none has been
  /// accepted yet.
  RunState? stateFor(String runId) => _accepted[runId];

  /// Every run ID this reconciler currently holds an accepted state for.
  Iterable<String> get runIds => _accepted.keys;

  RunStateReconciliationResult reconcile(
    RunState incoming, {
    bool isLoaded = true,
  }) {
    final existing = _accepted[incoming.runId];
    if (incoming.stateVersion <= 0) {
      return RunStateReconciliationResult(
        outcome: RunStateReconciliationOutcome.inconsistent,
        current: existing,
      );
    }
    if (!isLoaded) {
      return RunStateReconciliationResult(
        outcome: RunStateReconciliationOutcome.unloaded,
        current: existing,
      );
    }
    if (existing == null) {
      // Rule 1. Accept only after the validity and loaded-row guards above.
      _accepted[incoming.runId] = incoming;
      return RunStateReconciliationResult(
        outcome: RunStateReconciliationOutcome.accepted,
        current: incoming,
      );
    }
    if (incoming.stateVersion < existing.stateVersion) {
      return RunStateReconciliationResult(
        outcome: RunStateReconciliationOutcome.stale,
        current: existing,
      );
    }
    if (incoming.stateVersion == existing.stateVersion) {
      // Rules 3/4: only byte-for-byte identical state at the same version
      // is a no-op; anything else at that version is a conflict, even a
      // terminal state that matches everywhere except (say) its outcome
      // reason.
      if (incoming == existing) {
        return RunStateReconciliationResult(
          outcome: RunStateReconciliationOutcome.duplicate,
          current: existing,
        );
      }
      return RunStateReconciliationResult(
        outcome: RunStateReconciliationOutcome.inconsistent,
        current: existing,
      );
    }
    // incoming.stateVersion > existing.stateVersion.
    if (existing.isTerminal) {
      // Rule 5: nothing may follow a terminal state, regardless of what the
      // lifecycle graph would otherwise allow.
      return RunStateReconciliationResult(
        outcome: RunStateReconciliationOutcome.inconsistent,
        current: existing,
      );
    }
    // Rule 6: a higher version from a nonterminal state is accepted only
    // if it is a lifecycle transition the backend could actually have
    // committed — decided purely from the lifecycle graph, never inferred
    // from an event's own type, arrival order, or `finishedAt`.
    if (!existing.lifecycle.canTransitionTo(incoming.lifecycle)) {
      return RunStateReconciliationResult(
        outcome: RunStateReconciliationOutcome.inconsistent,
        current: existing,
      );
    }
    _accepted[incoming.runId] = incoming;
    return RunStateReconciliationResult(
      outcome: RunStateReconciliationOutcome.accepted,
      current: incoming,
    );
  }
}

/// One row from a loaded or reloaded message page: enough to dedupe by
/// message ID and reconcile that row's run state, without needing the
/// wider chat screen's own bookkeeping (assistant bubbles, tool cards,
/// scroll position, ...).
class MessagePageEntry {
  const MessagePageEntry({required this.messageId, this.runState});

  final String messageId;
  final RunState? runState;
}

/// The result of applying one [MessagePageEntry] via [reconcilePage].
class MessagePageEntryResult {
  const MessagePageEntryResult({
    required this.messageId,
    required this.isDuplicateMessage,
    this.stateResult,
  });

  final String messageId;

  /// Whether [messageId] was already present in the caller's own
  /// already-loaded set — i.e. this exact message row was already rendered
  /// by an earlier page application, live delta, or history load.
  final bool isDuplicateMessage;

  /// The reconciliation outcome for this entry's [MessagePageEntry.runState],
  /// or null when the row carries none.
  final RunStateReconciliationResult? stateResult;
}

/// Applies one page of message rows — freshly loaded history, or a
/// coalesced newest-page resync — as one unit: every row's message ID is
/// checked against [alreadyLoadedMessageIds] for dedup, and every row's
/// [MessagePageEntry.runState] (if any) is reconciled through [reconciler]
/// independently of that dedup check.
///
/// The independence matters: two overlapping page loads can legitimately
/// report the SAME message twice while its run has since advanced to a
/// higher, still-valid version (e.g. a run that was still `running` on the
/// first load and has reached a terminal state by the time a resync reloads
/// the same row) — the message-id dedup must not suppress that state
/// update, only the (already-rendered) message content/bubble itself.
List<MessagePageEntryResult> reconcilePage(
  RunStateReconciler reconciler,
  Iterable<MessagePageEntry> page,
  Set<String> alreadyLoadedMessageIds, {
  Set<String>? loadedRunIds,
}) {
  final results = <MessagePageEntryResult>[];
  for (final entry in page) {
    final runState = entry.runState;
    results.add(
      MessagePageEntryResult(
        messageId: entry.messageId,
        isDuplicateMessage: alreadyLoadedMessageIds.contains(entry.messageId),
        stateResult: runState == null
            ? null
            : reconciler.reconcile(
                runState,
                isLoaded: loadedRunIds?.contains(runState.runId) ?? true,
              ),
      ),
    );
  }
  return results;
}

/// Bounded buffer for [RunState] snapshots that arrive — from live or
/// replayed events — WHILE the initial newest-message page is still
/// loading.
///
/// Pure and side-effect free, like [RunStateReconciler]: it never calls an
/// API itself. The owner ([RunStateReconciler]'s caller) drains it — via
/// [drainAndClear] — once the initial page has committed, then replays each
/// buffered snapshot through that same [RunStateReconciler.reconcile] path
/// a live event would use, so a state that raced the initial load is never
/// treated any differently from one that arrived after it.
class RunStateLoadBuffer {
  /// The most distinct run IDs this buffer retains before it gives up on
  /// buffering individually and instead asks its owner to resync the
  /// newest page wholesale. A conversation with more than 64 concurrently
  /// in-flight runs racing the single initial-load window is far outside
  /// normal use; treating that as "just reload" rather than growing this
  /// buffer without bound keeps its memory use fixed no matter how many
  /// events arrive.
  static const maxDistinctRuns = 64;

  final Map<String, RunState> _buffered = {};

  /// Set once the 65th distinct run ID is offered. While true, every
  /// further [offer] is a no-op: the eventual resync re-fetches
  /// authoritative state for every run anyway, so retaining anything more
  /// here would only ever be discarded unread.
  bool _resyncRequired = false;

  /// Whether the distinct-run cap was exceeded and a coalesced resync is
  /// now owed.
  bool get resyncRequired => _resyncRequired;

  /// Number of distinct run IDs currently buffered.
  int get length => _buffered.length;

  /// Offers [state] to the buffer. Retains only the highest
  /// [RunState.stateVersion] seen so far for `state.runId` — an earlier
  /// buffered version for the same run is superseded, never queued
  /// alongside it, because only the highest version could ever survive
  /// reconciliation once drained anyway.
  void offer(RunState state) {
    if (_resyncRequired) return;
    final existing = _buffered[state.runId];
    if (existing == null && _buffered.length >= maxDistinctRuns) {
      // The 65th distinct run: stop buffering individually for the rest of
      // this race window and ask the owner for one coalesced resync
      // instead of growing further.
      _buffered.clear();
      _resyncRequired = true;
      return;
    }
    if (existing == null || state.stateVersion > existing.stateVersion) {
      _buffered[state.runId] = state;
    }
  }

  /// Returns every buffered state (highest version per run) and resets the
  /// buffer — including [resyncRequired] — back to empty. Called exactly
  /// once, right after the initial page commits.
  List<RunState> drainAndClear() {
    final drained = _buffered.values.toList(growable: false);
    _buffered.clear();
    _resyncRequired = false;
    return drained;
  }
}
