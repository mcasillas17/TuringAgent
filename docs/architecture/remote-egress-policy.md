# Remote egress policy

**Status:** implemented.

Turing's default execution path is local Ollama. A run reaches an
OpenAI-compatible endpoint only after the client asks the orchestrator for a
disclosure and the user confirms that exact prepared send. A registered remote
MCP server is the second egress path and uses the same decision.

## One run, one decision

`ChatService.PrepareRemoteEgress` is read-only. It resolves the effective
provider, model, external-agent identity, canonical model endpoint, every
remote MCP server and endpoint, integration endpoint and connection, selected tool
names, eligible skill snapshot, recall/memory flags, and conservative maximum
data categories for the request. It returns a short-lived challenge signed by
the orchestrator-only `TURING_EGRESS_SIGNING_SECRET`.

The challenge binds the session, idempotency key, request payload digest,
effective route, all endpoints, a non-secret credential-reference digest, tools,
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

Skill content appears only when the model provider is remote and the frozen,
parseable enabled-skill snapshot is non-empty. The disclosure names every
skill in that snapshot and distinguishes skills whose full content may be sent
from those limited to name and description. This is a ceiling: the runtime
always injects the skill index, while a permitted body and its references are
sent only when the user invokes the skill or the model calls `skill_view`.
The existing skill-snapshot fingerprint binds each displayed skill id, name,
and content ceiling; the names are the legible face of that signed binding,
not a second signature surface.

A local-model run that may call a remote MCP server discloses only the
categories that can cross that boundary: tool arguments and tool results. A
mixed run takes the conservative union. Enabling a remote server is not consent
to contact it, and there is no per-server acknowledgement flag. Because the
local model is offered every enabled tool, an enabled remote tool makes every
such run a run that may call it; each send therefore asks for its own decision.
Disabling all tools from that remote server restores the no-consent local path.
The confirmation names each exact remote MCP endpoint and the frozen tools
available at that destination, not merely the endpoint host.

## Endpoint and fallback rules

Credentialed non-loopback endpoints require HTTPS. Plain HTTP is accepted only
for exact `localhost` or loopback IP literals (`127.0.0.0/8` and `::1`);
`host.docker.internal`, private addresses, and arbitrary DNS names are not
loopback exceptions. Userinfo, query strings, fragments, relative URLs, and
non-HTTP schemes are rejected.

Credentialed provider clients reject all redirects before a redirected request
is sent. Provider selection is exact: local failure cannot consult or fall back
to a remote provider, model, endpoint, or external agent.

Remote MCP URLs require HTTPS, reject redirects, and resolve through a dialer
that refuses loopback, private, link-local, multicast and unspecified
addresses. Local third-party URLs require an explicit port and a single Docker
service name; redirects, reserved names, host aliases and IP literals are
refused. At dispatch the name must resolve entirely inside the fixed
`172.31.254.0/24` subnet of the internal-only `net-mcp-registry` network.

Connected-account integrations are the third egress path beside remote model
providers and remote MCP servers. Their signed entries bind the canonical
endpoint, connection id, display name, and frozen tools. The consent is
endpoint-granular, not repository-granular: consent to `api.github.com` for a
GitHub connection covers any repository that credential can reach. Because
enabled integration tools may be called by a local run, connecting an account
makes every local send ask until those tools are disabled.

Ollama is the local provider identity, so its endpoint is restricted to
`localhost`, `host.docker.internal`, or a loopback IP literal. Pointing the
Ollama configuration at a remote host is refused instead of bypassing consent.

Runs already queued for a remote provider before TUR-003 have no run-owned
egress decision. Migration 0014 terminalizes them with
`egress_decision_required` rather than leaving them stranded behind the
egress-aware worker gate; resend them from the client to review and record a
fresh disclosure. The later run-outcome migration preserves populated
`run_egress_decisions` exactly and projects this legacy terminal code (and the
legacy `egress_decision_invalid` code) as bounded `policy_denied`, with no raw
diagnostic text in public history.

## Background work and audit

Automations cannot inherit interactive consent. A remote effective route
advances the schedule but creates no run; it records the typed durable failure
`remote_egress_requires_interactive_consent` and a redacted audit row.
Automations receive no run-owned decision for a remote MCP destination either;
the proxy refuses the remote call rather than inheriting interactive consent.

`egress.consent.recorded` exposes only provider, endpoint host, typed
categories, decision version, and consent timestamp. It never stores or returns
the challenge, nonce, raw fingerprint, credentials, request content, recalled
text, skill bodies, tool payloads, or attachments. Session deletion cascades
the decision and applies the existing audit scrub contract.
