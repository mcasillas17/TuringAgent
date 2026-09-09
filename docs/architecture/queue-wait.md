# Bounded queue waiting

TUR-010 answers one question a queued run could not previously answer: *why is
this still waiting, and how long will it wait?* Before it, a run accepted while
a worker was live and then stranded when that worker went away sat in the queue
with no explanation and no end.

This is a waiting policy, and only a waiting policy. It is not a model timeout,
an approval TTL, a worker heartbeat lease, or an execution retry budget. Those
four measure work, authorization, liveness and attempts, and each keeps its own
setting; a run that has never been dispatched has consumed none of them.

## What the orchestrator distinguishes

A queued run is in exactly one of two states, and they are not the same problem:

| Observed condition | Public queue reason | Which bound applies |
|---|---|---|
| At least one live worker satisfies the run's frozen route — it is busy, at its advertised capacity, or working through earlier turns in the session | `none` | the overall queue-age bound only |
| No live worker satisfies the route: none connected, every candidate past its heartbeat lease, or the live ones advertise capabilities the route needs and does not have | `no_compatible_worker` | the no-worker bound, and the overall queue-age bound |

"Live" means the same thing dispatch means by it: a registered worker whose last
heartbeat is inside its lease. A worker that has stopped answering is absent for
this purpose even while its stream is open.

A **busy** compatible worker never starts the no-worker clock and never produces
a notice. Waiting your turn is normal, and a card that announced it on every
queued run would train the user to ignore the one that matters.

New work with no compatible worker never reaches this policy at all: routing
validation refuses the enqueue before anything is persisted, and a scheduled
occurrence records `routing_unavailable` in its automation audit instead of
queueing work nothing can run. TUR-010 is entirely about work that was correctly
accepted and lost its workers afterwards, or was requeued after a disconnect.

## Configuration

| Variable | Default | Meaning |
|---|---|---|
| `TURING_QUEUE_NO_WORKER_TIMEOUT_MS` | `900000` (15 min) | How long a run may stay queued while no live worker satisfies its route. The clock starts the first time the orchestrator *observes* that condition — not at enqueue — and is cleared the moment a compatible worker returns. `0` turns the bound off. |
| `TURING_QUEUE_MAX_WAIT_MS` | `43200000` (12 h) | Total time a run may spend queued, accumulated across every queued interval. Starts at enqueue. This is the bound that covers a compatible worker that is present but never gets to this run. `0` turns the bound off. |
| `TURING_QUEUE_TIMEOUT_POLICY` | `fail` | What happens when either bound is reached: `fail` or `cancel`. |

Both bounds are checked on the existing recovery tick
(`TURING_JOB_REAPER_INTERVAL_MS`), so that interval is the granularity with
which a deadline is noticed. Values above thirty days are refused; a negative
value is refused as an invalid integer rather than read as "off".

**`TURING_JOB_REAPER_INTERVAL_MS=0` turns both queue bounds off**, whatever they
are set to. Zero stops the recovery loop, and that loop is the only thing that
checks a deadline on an idle stack: the sweep also runs on an enqueue and on a
worker connecting, disconnecting or changing capabilities, so a busy install
still enforces the bounds, but a run left waiting on a stack where nothing else
happens is never looked at again. An operator who disables the reaper is
choosing that.

The defaults suit a home hub. Fifteen minutes with nothing able to run a message
survives a restart, a model swap or a closed laptop lid, and still answers the
same session rather than staying silent. Twelve hours of total queue age is
deliberately far longer, because a slow queue is not a broken one.

### The policy, and why there is no pause

| Policy | Terminal lifecycle | Outcome reason | Asserted queue reason |
|---|---|---|---|
| `fail` | `failed` | `expired` | `no_compatible_worker` or `queue_timeout` |
| `cancel` | `cancelled` | `abandoned` | `no_compatible_worker` or `queue_timeout` |

Which of the two reasons a terminal run carries follows the bound that actually
fired, not the condition it was waiting under. The no-worker bound is checked
first and reports `no_compatible_worker`; anything else that reaches the overall
bound reports `queue_timeout`. So a run whose worker vanished eleven hours into a
twelve-hour queue age reports `queue_timeout`, and so does every run when
`TURING_QUEUE_NO_WORKER_TIMEOUT_MS` is `0` — the overall bound is the only one
left to fire.

Both outcomes are terminal. **Pausing is deliberately not offered.** A durable
paused queue is not a configuration value: it would need a public lifecycle of
its own, a resume RPC, and client behavior for a run that is neither waiting nor
finished. The one existing state that resembles a pause — `waiting_approval` —
belongs to an approval a human was actually asked for, and repurposing it as a
generic hold would make "Turing is waiting for you" mean two unrelated things.

Under `cancel` the reason is `abandoned`, not `user_cancelled`. Nobody asked for
this; the orchestrator gave up on the run's behalf. `user_cancelled` is written
only by the [explicit cancel-intent API](run-outcomes.md#explicit-cancellation).
Flutter **Stop** can end a queued run even when no compatible worker or model
is available. It does not change the queue-timeout policy or enqueue a replacement.

The public outcome vocabulary is unchanged: a queue bound reports the existing
`expired` (or `abandoned`) reason, and the `queue_wait_reason` the bound asserts
is what separates it from an approval that expired. That keeps `agent_runs` out
of a table rebuild and keeps the closed outcome vocabulary from growing a
synonym.

The assertion matters as much as the value. Leaving the queue clears the reason,
so a run that waited, was picked up, ran, and was then abandoned by its client
reports `none` and gets its own copy — only a run the bound itself ended is
described as one that never started. `expired` and `abandoned` both reach runs
that did start (an approval that ran out, a closed app), and without that rule
they would all borrow the queue's explanation.

## Durable state

Migration `0020_queue_wait` appends four columns to `agent_runs`:

| Column | Meaning |
|---|---|
| `queued_since_ns` | Start of the current queued interval; null when the run is not queued. Opened by enqueue and by every transition back into `queued`. |
| `queue_waited_ns` | Nanoseconds already banked from previous queued intervals. |
| `queue_unroutable_since_ns` | Start of the current no-compatible-worker interval, or null. Only ever set by an actual observation; never inferred from the absence of a dispatch. |
| `queue_wait_reason` | The public value above, plus the terminal-only `queue_timeout`. Cleared whenever a run crosses the queue boundary in either direction; a terminal run carries one only because the bound that ended it asserted it. |

The accumulator is the point. Measuring only the current interval would let a
run that flaps between `recovering` and `queued` — a worker that keeps
disconnecting, a reconnect storm, a restart loop — restart its own deadline
forever. Banking each interval on the way out means the overall bound converges
however many times the run is requeued. That maintenance lives in the single
guarded transition writer every lifecycle change already passes through, so no
writer can forget it and no two writers can disagree about when a transition
happened.

Existing queued rows are backfilled from `created_at`: a run that was queued when
this migration ran has been queued since it was created, and giving it a fresh
clock would have handed every waiting run an unbounded wait at the exact moment
bounded waiting shipped.

### What survives a restart

Everything the bounds are measured from is a column, so a restart changes none
of it:

- accumulated queue age keeps accruing; a run already past its overall bound is
  ended on the first sweep after startup;
- an already-unavailable queue keeps the no-worker interval it had, so a
  restart is not a way to reset that clock either;
- a run whose route was fine before shutdown has no open no-worker interval, so
  the empty registry a restart starts with opens a fresh one on the first sweep
  rather than back-dating one — the workers have not had a chance to reconnect
  yet, and the sweep does not pretend otherwise.

The startup case is handled by the same sweep as every other case. There is no
separate "already unavailable at startup" path to keep in step with the live
capability-change path.

## Notices, versions and races

A change in queue truth commits as a guarded `queued -> queued` transition: the
run's `state_version` increments once, `agent.run.state_changed` carries the new
snapshot, and both a live stream and a reopened conversation read the same
`RunState`. It is the *only* self-edge in the lifecycle graph, and the client's
transition rules accept it for exactly that reason.

Deduplication is structural rather than a cache: a run whose stored reason
already matches what the sweep observed is not written to at all — no version, no
event, no row write — so reconciling an unchanged queue is free however often the
reaper ticks. The advisory `agent.run.step` capability-loss and restoration
notices TUR-018 already emits are unchanged and still ride their own publish
rules; the durable observation is recorded in both directions regardless, because
it is authoritative state rather than an interruption.

Expiry is atomic against assignment. It runs in one transaction that requires the
run to still be `queued` *and* its job to still be `pending`, and resolves the
expected state version from the row under that guard rather than from the page
the scan read. So:

- a claim that won the race makes expiry a no-op instead of failing a run a
  worker is executing;
- a cancelled or deleted run is not queued, and is left exactly as it is;
- a terminal run cannot be revived;
- the same work cannot be dispatched twice, because expiry never returns work to
  the queue.

The scan itself is the existing paged, keyset-ordered pending-routing scan with
its five-second deadline, so nothing here holds SQLite's single connection open
across a growing backlog.

Ending an over-waiting run also marks its job terminal. Leaving it pending would
let a later sweep pick the run up again and would block the session's next turn
behind a run that is never going to start.

## What a user sees

| State | Card |
|---|---|
| Queued, `none` | "Queued — The run is waiting to start." |
| Queued, `no_compatible_worker` | "Waiting for an assistant — Nothing connected right now can run this. It will start on its own if one becomes available." |
| Ended by the no-worker bound | "No assistant became available" |
| Ended by the overall bound | "Waited too long to start" |

The waiting card deliberately stops at "it will start on its own if one becomes
available" and does not promise that it will stop waiting otherwise. Both bounds
can be switched off, and `RunState` carries no deadline, so the client has no way
to know whether one is configured — a card that promised an end the install had
disabled would be the same kind of confident, unverifiable claim this design
refuses everywhere else.

All four render identically from a live event and from reopened history, because
both paths reconcile the same versioned snapshot. A queue reason a newer backend
introduces renders as the plain queued card rather than as a guess.

## Limitations

- The client is told *why* a run is waiting, not *until when*. A countdown would
  mean publishing a deadline computed from live configuration, and a deadline
  frozen into an event payload goes stale the moment an operator changes a
  bound. The bound is enforced from the durable clock either way.
- A deadline is noticed on the recovery tick, so a run can be up to one tick
  past its bound before it ends.
- `workers_busy` is not persisted as a distinct value. A queued run with a live
  compatible worker is described by its lifecycle alone; only the actionable
  condition gets a name.
- There is no pause, hold or resume — see the policy table above.
