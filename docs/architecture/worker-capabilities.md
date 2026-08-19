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
maximum concurrent runs, and the exact external-agent credential names the runtime
can resolve. Credential names are routing metadata, never API keys. The coarse
`supports_external_agents` bit remains only for older orchestrators; current routing
authorization requires an exact credential-ref match. A modern worker reports both
the snapshot and the legacy ready fields so an older orchestrator can still accept it.
If a modern worker has no discovery callback, it reports `COMPLETE` with an
authoritative empty tool set so an older orchestrator cannot synthesize legacy tools.

`RuntimeWorkerCapabilitiesUpdated` replaces the complete snapshot for the current
registration. It never patches individual fields. Complete replacement makes tool or
model removal unambiguous and lets a capacity reduction take effect without merging
stale values. Updates with a different worker or registration identity fail.

Workers that predate the snapshot are accepted only when the orchestrator was created
with an explicit `LegacyCapabilityProfile`. The profile supplies exact configured
models, context ceilings, agent IDs, exact external-agent credential refs, and any
rollout-only fallback tool list; the ready message still supplies its tool snapshot and capacity.
The ready agent ID must be recognized and included in the profile. A completed empty
tool discovery is authoritative. A legacy ready message without tools uses only the
profile's explicit fallback list, and an empty list means no tool capability. There is
no "unknown means supported" path.

## Registry lifecycle

The registry is process-local because a capability is true only while its owning
runtime stream is live. Each entry is keyed by stable `worker_id` and owned by the
stream's `registration_id` plus its connection object.

- Ready inserts one entry. A second live registration for the same worker ID fails.
- A complete capability update persists its discovered-tool snapshot before replacing
  only the matching registration's live capability snapshot. Persistence failure
  leaves both views unchanged.
- Heartbeats refresh the connection timestamp but do not mutate capabilities.
- Dispatch and public configuration views ignore entries past the heartbeat lease.
- If registration persistence or initial queue reconciliation fails after the worker
  becomes visible, normal teardown fences the registration and requeues every claim
  made during that window before the handshake returns an error.
- Recovery ticks publish capability-loss notices for queued routes whose worker
  heartbeat lease expires; a later heartbeat publishes restoration before dispatch.
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
6. exact external-agent credential ref when applicable.

Failure returns `FailedPrecondition` with a typed `RoutingUnavailableDetail` naming
the failed capability and requested value. A failed validation commits nothing.
Legacy workers are validated against their explicit profile rather than bypassing the
check. A legacy external-support boolean without an explicit credential-ref set
authorizes nothing. Credential-specific errors name only the requested ref and do not
enumerate other configured credential names. External-agent support carries no model
context guarantee, so any positive context requirement on an external route fails
closed.

The accepted requirements are frozen into the job payload. Dispatch claims only jobs
that match the selected worker's current snapshot. A route that was valid when
accepted therefore cannot be handed to an incompatible worker after capabilities
change. Coarse provider/model/context/tool/capacity predicates run in SQLite before
the final typed matcher, and the indexed query claims at most one compatible row, so
an incompatible backlog is not decoded and rescanned for every worker. Dispatch
reserves worker capacity without holding the worker lock while waiting for SQLite.
Immediately before assignment delivery, the sender revalidates the frozen route
against the committed live snapshot; an incompatible claim is requeued instead of
being sent. Capability fencing occurs before execution and therefore does not consume
the run's execution retry budget.

Scheduled runs use the same validator before creating a session, message, run, or job.
An unavailable occurrence advances its schedule and records `routing_unavailable`
as a durable automation audit occurrence instead of creating work that cannot
currently execute. Successful external automation runs publish their already-durable
routing notices live after the queued event, matching interactive enqueue behavior.

## Capability loss and queue notices

After every insert, replacement, or removal, the runtime compares queued routes
against the registry before and after the transition.

- Supported to unsupported appends an `agent.run.step` notice that names the
  unavailable capability and records `routing_capability_unavailable`.
- Unsupported to supported appends a restoration notice and immediately retries
  dispatch.

Only pending jobs with queued runs are considered. Notice insertion repeats those
conditions atomically at the SQLite write boundary, so work claimed after a scan does
not receive a stale loss or restoration notice. An already delivered run keeps its
frozen assignment; reducing capacity or removing a model does not cancel work already
executing. Stream disconnect reconciliation continues to own notices and retries for
assigned runs.

The before/after comparison tracks whether each loss was actually published, so a
restart seed cannot suppress the first actionable notice. Enqueue callers recheck
pending routes after commit to close the capability-loss race between validation and
persistence. Deduplication advances after each committed notice, even if a later notice
in the same refresh fails. Queue refreshes use an indexed keyset scan in bounded pages
and a five-second deadline, so retries neither duplicate transitions nor monopolize the
registry lock indefinitely. Idempotent snapshots do not duplicate notices.
After an enqueue commits, a refresh failure is logged but does not cancel the durable
run or prevent dispatch; later lifecycle and recovery passes retry notice state.
On orchestrator restart the empty registry is restored by worker reconnects; queued
routes receive restoration notices and become dispatchable again.

## Configuration APIs

`SessionService.GetConfig` and `ListAgents` combine configured defaults with live
registry snapshots:

- providers remain listed but are enabled only when a live worker advertises them;
- provider entries expose the currently advertised exact models and context ceilings;
- each provider default is the configured model when live, otherwise the first
  deterministic live model (or empty when the provider is unavailable);
- chat requests and scheduled automations that omit a model resolve through that same
  live default before validation and enqueue;
- known agents remain listed with an explicit availability flag;
- tools come from the union of live worker tool snapshots; persisted discovery rows
  are filtered against that union so disconnect or lease expiry cannot expose stale
  tools.

The context ceilings are routing guarantees configured on the runtime. They are not
inferred from provider marketing metadata or guessed from a model name.

## Test contract

Tests must fail without the implementation for:

- unsupported provider, model, tool, agent, context ceiling, and minimum concurrency
  before any enqueue persistence;
- exact external credential refs across enqueue validation, SQL claim filtering, and
  final delivery fencing, without exposing the worker's other refs;
- exact typed error details;
- legacy-profile and ready-agent validation without an allow-all or implicit tool
  fallback outside the profile;
- multi-worker dispatch selecting only a compatible worker;
- capacity reduction and model/tool loss while work is queued;
- pending-only queue-notice insertion when a scan races with assignment;
- disconnect loss notices and reconnect restoration notices;
- duplicate live registration rejection and owner-safe replacement;
- authoritative capability replacement and mismatched-registration rejection;
- tool-persistence failure preserving the previous live snapshot and assignment-send
  revalidation against the committed replacement;
- worker reconnect advertising a fresh registration and complete snapshot;
- modern no-discovery workers reporting an authoritative empty tool set;
- capability-fence requeues preserving execution attempts;
- populated pre-capability migration backfill and stable nanosecond keyset ordering;
- automation routing-notice publication and durable routing-unavailable audit
  occurrences;
- concurrent validation, snapshot replacement, dispatch, and disconnect under the Go
  race detector;
- live provider/model/agent configuration responses;
- additive Go and Dart protobuf generation.
