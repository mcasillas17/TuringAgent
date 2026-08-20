# Remote-provider egress policy

**Status:** TUR-003 pending merge.

Turing's default execution path is local Ollama. A run reaches an
OpenAI-compatible endpoint only after the client asks the orchestrator for a
disclosure and the user confirms that exact prepared send.

## One run, one decision

`ChatService.PrepareRemoteEgress` is read-only. It resolves the effective
provider, model, external-agent identity, canonical endpoint, selected tool
names, eligible skill snapshot, recall/memory flags, and conservative maximum
data categories for the request. It returns a short-lived challenge signed by
the orchestrator-only `TURING_EGRESS_SIGNING_SECRET`.

The challenge binds the session, idempotency key, request payload digest,
effective route, endpoint, a non-secret credential-reference digest, tools,
skills, context flags, categories, nonce, and expiry. `SendMessage` requires
explicit acknowledgment and revalidates the context. The enqueue transaction
writes the run-owned decision, job, events, idempotency record, and redacted
audit row together.

An exact idempotent replay returns the original run and decision before expiry
or nonce checks. A changed request or route needs a new preparation. A nonce
cannot create a second run. Assignment retry of the same frozen job keeps the
original decision.

The frozen tool set is also a worker-claim requirement. If a runtime reconnects
with a smaller registry, queued remote runs that named a missing tool wait for
a compatible worker rather than silently running with a different set. Routing
notices make the wait and later restoration visible.

## Categories

The typed category set covers current message, conversation history,
cross-session recall, memory/profile, skill content, tool schemas, tool
arguments, tool results, and attachments.

The recorded set is the maximum that may leave for the prepared run; actual
runtime use may be smaller, never larger. Memory/profile and attachments are
currently unsupported and therefore absent. Routed external agents also omit
cross-session recall. Direct OpenAI-compatible runs may include recall.

## Endpoint and fallback rules

Credentialed non-loopback endpoints require HTTPS. Plain HTTP is accepted only
for exact `localhost` or loopback IP literals (`127.0.0.0/8` and `::1`);
`host.docker.internal`, private addresses, and arbitrary DNS names are not
loopback exceptions. Userinfo, query strings, fragments, relative URLs, and
non-HTTP schemes are rejected.

Credentialed provider clients reject all redirects before a redirected request
is sent. Provider selection is exact: local failure cannot consult or fall back
to a remote provider, model, endpoint, or external agent.

Ollama is the local provider identity, so its endpoint is restricted to
`localhost`, `host.docker.internal`, or a loopback IP literal. Pointing the
Ollama configuration at a remote host is refused instead of bypassing consent.

Runs already queued for a remote provider before TUR-003 have no run-owned
egress decision. Migration 0014 terminalizes them with
`egress_decision_required` rather than leaving them stranded behind the
egress-aware worker gate; resend them from the client to review and record a
fresh disclosure.

## Background work and audit

Automations cannot inherit interactive consent. A remote effective route
advances the schedule but creates no run; it records the typed durable failure
`remote_egress_requires_interactive_consent` and a redacted audit row.

`egress.consent.recorded` exposes only provider, endpoint host, typed
categories, decision version, and consent timestamp. It never stores or returns
the challenge, nonce, raw fingerprint, credentials, request content, recalled
text, skill bodies, tool payloads, or attachments. Session deletion cascades
the decision and applies the existing audit scrub contract.
