# Worker capability routing

**Status:** Pending merge for TUR-018.

## Purpose

The orchestrator must know whether connected workers can execute a requested route
before it persists a message or job. The same information must guide dispatch after
the job is queued and explain when a previously available route disappears.

TUR-018 is limited to capability advertisement, registry lifecycle, validation,
dispatch filtering, and queue notices. It does not add delegation, agent handoff,
connectors, or durable worker membership.

## Protocol

`RuntimeWorkerReady` keeps its existing fields for worker-first rolling upgrades and
adds:

- a per-stream `registration_id`;
- one authoritative `WorkerCapabilities` snapshot.

The snapshot contains exact provider/model pairs, an operator-configured maximum
context-token ceiling for each model, supported local agent IDs, discovered tools,
maximum concurrent runs, and whether the runtime can execute an existing external
agent route. A modern worker reports both the snapshot and the legacy ready fields so
an older orchestrator can still accept it.

`RuntimeWorkerCapabilitiesUpdated` replaces the complete snapshot for the current
registration. It never patches individual fields. Complete replacement makes tool or
model removal unambiguous and lets a capacity reduction take effect without merging
stale values. Updates with a different worker or registration identity fail.

Workers that predate the snapshot are accepted only when the orchestrator was created
with an explicit `LegacyCapabilityProfile`. The profile supplies exact configured
models, context ceilings, agent IDs, and external-agent support; the ready message
still supplies its tool snapshot and capacity. There is no "unknown means supported"
path.

## Registry lifecycle

The registry is process-local because a capability is true only while its owning
runtime stream is live. Each entry is keyed by stable `worker_id` and owned by the
stream's `registration_id` plus its connection object.

- Ready inserts one entry. A second live registration for the same worker ID fails.
- A complete capability update replaces only the matching registration's snapshot.
- Heartbeats refresh the connection timestamp but do not mutate capabilities.
- Dispatch and public configuration views ignore entries past the heartbeat lease.
- Disconnect or lease recovery removes only the matching owner, so teardown from an
  old stream cannot erase a replacement.
- Reconnect with a fresh registration restores the entry from the new ready snapshot.

Registry transitions are serialized. Readers take immutable snapshots, so routing
validation, dispatch, configuration APIs, and race-enabled tests never observe a
partially replaced capability set.

## Routing requirements and validation

`SendMessageRequest` adds optional exact tool requirements, a minimum context-token
ceiling, and a minimum advertised worker concurrency. Provider, model, and agent
remain the existing request fields. Defaults require no tools, no stated context
ceiling, and a worker whose maximum concurrency is at least one.

The repository resolves the session's effective route before writing any message,
run, job, or event. It invokes the runtime validator inside the enqueue transaction.
The validator requires one live worker whose single snapshot satisfies the complete
route:

1. local agent ID;
2. exact provider/model pair, unless this is an explicit external-agent route;
3. model context ceiling;
4. every requested `server/tool`;
5. minimum advertised concurrency;
6. external-agent execution support when applicable.

Failure returns `FailedPrecondition` with a typed `RoutingUnavailableDetail` naming
the failed capability and requested value. A failed validation commits nothing.
Legacy workers are validated against their explicit profile rather than bypassing the
check.

The accepted requirements are frozen into the job payload. Dispatch claims only jobs
that match the selected worker's current snapshot. A route that was valid when
accepted therefore cannot be handed to an incompatible worker after capabilities
change.

## Capability loss and queue notices

After every insert, replacement, or removal, the runtime compares queued routes
against the registry before and after the transition.

- Supported to unsupported appends an `agent.run.step` notice that names the
  unavailable capability and records `routing_capability_unavailable`.
- Unsupported to supported appends a restoration notice and immediately retries
  dispatch.

Only pending jobs are considered. An already assigned run keeps its frozen assignment;
reducing capacity or removing a model does not cancel work already executing.
Stream disconnect reconciliation continues to own notices and retries for assigned
runs.

The before/after comparison prevents duplicate notices from idempotent snapshots.
On orchestrator restart the empty registry is restored by worker reconnects; queued
routes receive restoration notices and become dispatchable again.

## Configuration APIs

`SessionService.GetConfig` and `ListAgents` combine configured defaults with live
registry snapshots:

- providers remain listed but are enabled only when a live worker advertises them;
- provider entries expose the currently advertised exact models and context ceilings;
- known agents remain listed with an explicit availability flag;
- tools continue to come from the union of live worker tool snapshots.

The context ceilings are routing guarantees configured on the runtime. They are not
inferred from provider marketing metadata or guessed from a model name.

## Test contract

Tests must fail without the implementation for:

- unsupported provider, model, tool, agent, context ceiling, and minimum concurrency
  before any enqueue persistence;
- exact typed error details;
- legacy-profile validation without an allow-all fallback;
- multi-worker dispatch selecting only a compatible worker;
- capacity reduction and model/tool loss while work is queued;
- disconnect loss notices and reconnect restoration notices;
- duplicate live registration rejection and owner-safe replacement;
- authoritative capability replacement and mismatched-registration rejection;
- worker reconnect advertising a fresh registration and complete snapshot;
- concurrent validation, snapshot replacement, dispatch, and disconnect under the Go
  race detector;
- live provider/model/agent configuration responses;
- additive Go and Dart protobuf generation.

