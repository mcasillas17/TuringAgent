# Send-message idempotency

`ChatService.SendMessage` accepts the optional `idempotency_key` field. Clients
that may retry an unconfirmed send must generate one opaque, unpredictable key
for the operation and retain it until the operation either queues or the user
edits the draft. The desktop client does this automatically: retrying an
unconfirmed send reuses its key; changing the draft starts a new operation.

The key is globally scoped. The server binds it to a SHA-256 fingerprint of the
canonical request: session ID, exact content, normalized content type, agent,
provider, the configured default model when the request omits one, requested
tools, required context tokens, and minimum worker concurrency. Live worker
selection may choose a different execution model, but that mutable choice does
not change the identity of an exact retry. Thus omitted values that normalize to
the same effective request replay successfully, while changing any canonical
value (including the session) returns gRPC `ALREADY_EXISTS`. The server never
stores the request text in the idempotency record.

On the first request, the messages, run, job, durable events, and idempotency
record commit in one bounded SQLite transaction. Only that request publishes
new events. A matching replay returns the original user-message,
assistant-message, run, job, and trace IDs, emits the original version-1
`RunQueued` event with its durable `RunState`, and continues from the persisted
event sequence. It never creates a second run, message pair, state version, or
event.
If a prior dispatch left its run queued, the replay re-dispatches the existing
job rather than creating one. This record survives an orchestrator restart;
model and tool work never run inside the transaction. Once a keyed operation
commits, losing or closing its delivery stream does not cancel the run;
transport loss only affects event delivery, so a replay can resume it. The
runtime recovery loop also re-dispatches still-pending jobs, so a transient
dispatch failure cannot leave a committed keyed operation stranded.

Calls without a key retain the existing non-idempotent behavior for backwards
compatibility and must not be retried as if they were idempotent.
