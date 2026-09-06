/// The semantic lifecycle a run publishes, and the transitions a client may
/// believe.
///
/// This is the public half of the backend's state machine, and deliberately
/// only that half. The orchestrator distinguishes a run whose owner handed it
/// back from a run whose owner vanished by reading the assignment attempt, the
/// worker identity, and the execution state inside the transaction that
/// releases the run. None of that crosses the wire, and none of it should: a
/// client has no business knowing which process was executing its request.
///
/// So the rules here answer a narrower question than the backend's: is this
/// pair something a committed projection could ever describe? Anything leaving
/// a terminal state is impossible, as is a backward edge no writer can commit.
/// Everything else the backend is allowed to commit is accepted, without
/// guessing which private trigger produced it.
enum RunLifecycle {
  /// Reserved for a lifecycle an older client cannot name. It describes no
  /// state, so nothing transitions into or out of it.
  unspecified,

  /// Waiting for a worker to pick the run up.
  queued,

  /// Owned by a worker and executing.
  running,

  /// Holding, because the run asked a human to authorize something.
  waitingApproval,

  /// Ownership became uncertain: the run may or may not still be executing
  /// somewhere, and nobody can currently say which.
  recovering,

  /// Finished with a report of its own.
  completed,

  /// Ended without producing its answer.
  failed,

  /// Ended because it was cancelled or abandoned.
  cancelled,

  /// Reserved for a lifecycle a newer backend introduced. Like [unspecified],
  /// it describes no state.
  unknown;

  /// Whether a projection moving this run to [next] is one the backend could
  /// have committed.
  ///
  /// Total by construction: every pair has an answer, including the two
  /// reserved values, so a reconciler can ask about anything it decoded without
  /// having to guard the call.
  bool canTransitionTo(RunLifecycle next) =>
      _publiclyCommittedEdges[this]?.contains(next) ?? false;
}

/// The transitions a committed projection can describe.
///
/// running to queued is here because the backend can prove a release that had
/// no uncertain phase — an assignment that never reached a worker, or an
/// authenticated attempt handing its own run back — and commits it as a single
/// version. The client sees only that committed pair. Rejecting it because the
/// more common requeue passes through recovering would make the client discard
/// a state the run really is in.
///
/// queued to completed is absent for the opposite reason: nothing ran, so there
/// is no successful report to commit. A queued run can still end, but only by
/// failing or being cancelled.
///
/// queued to queued is the one self-edge, and it is here because the backend
/// really does commit one: TUR-010's queue observer records that a queued run
/// has no worker able to serve it — or that one came back — as a versioned
/// transition, so the fact reaches a reopened conversation and a live stream
/// through the same snapshot as every other change. The run has not moved
/// phase; what changed is [RunState.queueWaitReason], and rejecting the edge
/// would make the client discard the only explanation it will ever get for a
/// run that is sitting still. No other lifecycle has a self-edge, and this one
/// is still subject to every rule around it: a higher version, never after a
/// terminal state, and identical state at the same version is still a no-op.
const Map<RunLifecycle, Set<RunLifecycle>> _publiclyCommittedEdges = {
  RunLifecycle.queued: {
    RunLifecycle.queued,
    RunLifecycle.running,
    RunLifecycle.failed,
    RunLifecycle.cancelled,
  },
  RunLifecycle.running: {
    RunLifecycle.queued,
    RunLifecycle.waitingApproval,
    RunLifecycle.recovering,
    RunLifecycle.completed,
    RunLifecycle.failed,
    RunLifecycle.cancelled,
  },
  RunLifecycle.waitingApproval: {
    RunLifecycle.running,
    RunLifecycle.recovering,
    RunLifecycle.completed,
    RunLifecycle.failed,
    RunLifecycle.cancelled,
  },
  RunLifecycle.recovering: {
    RunLifecycle.running,
    RunLifecycle.queued,
    RunLifecycle.completed,
    RunLifecycle.failed,
    RunLifecycle.cancelled,
  },
};
