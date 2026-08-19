# Send-message idempotency

`ChatService.SendMessage` accepts the optional `idempotency_key` field. Clients
that may retry an unconfirmed send must generate one opaque, unpredictable key
for the operation and retain it until the operation either queues or the user
edits the draft. The desktop client does this automatically: retrying an
unconfirmed send reuses its key; changing the draft starts a new operation.

The key is globally scoped. The server binds it to a SHA-256 fingerprint of the
canonical request: session ID, exact content, normalized content type, agent,
provider, and resolved model. Thus omitted values that normalize to the same
effective request replay successfully, while changing any of those values
(including the session) returns gRPC `ALREADY_EXISTS`. The server never stores
the request text in the idempotency record.

On the first request, the messages, run, job, durable events, and idempotency
record commit in one bounded SQLite transaction. Only that request publishes
events or dispatches runtime work. A matching replay returns the original
run/job/trace IDs, emits the original `RunQueued` event, and continues from the
persisted event sequence. This record survives an orchestrator restart; model
and tool work never run inside the transaction. Once a keyed operation commits,
losing or closing its delivery stream does not cancel the run; transport loss
only affects event delivery, so a replay can resume it.

Calls without a key retain the existing non-idempotent behavior for backwards
compatibility and must not be retried as if they were idempotent.
