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
runtime use may be smaller, never larger. Attachments are currently unsupported
and therefore absent. Routed external agents omit cross-session recall. Direct
OpenAI-compatible runs may include recall.

Memory/profile is live. It appears when **both** conjuncts hold: the model
provider is remote, **and** either the frozen pinned snapshot has content that
survives whitespace trimming after truncation, or a memory tool is in the frozen
selected-tool set. The second conjunct closes an honesty gap: with nothing
pinned, a belief the model searches for still leaves as tool arguments and
results, and the disclosure would otherwise stay silent about it. Both conjuncts
are re-derivable from the frozen job, because `selected_tools` is an exact frozen
set and the pinned snapshot rides on the job.

The disclosure names what is reachable, by tier and vault path: `persona.md` and
`profile.md` when they are pinned, and the `beliefs/` folder when a memory tool
is selected — the folder rather than its notes, because which note is read
depends on what the model asks for after the disclosure is written. `inbox/` is
never named because no tool argument can reach it. The pinned bytes themselves
are never in the disclosure; it is a list of what may leave, not a second copy
of it.

A `memory_snapshot_fingerprint` binds the pinned tiers to the run, beside the
skill-snapshot fingerprint and computed the same way: over the post-truncation
pinned bytes of both tiers, notices included, in one read that also serves the
applicability decision and the disclosure. It rides the signed challenge, the
enqueue fingerprint, and the frozen decision. The enqueue transaction recomputes
it against the frozen decision, so an editor autosave between consent and send
is refused legibly — prepare the send again — rather than shipping bytes the
user never saw disclosed.

Before a provider is contacted, the runtime re-derives both halves from the
frozen job and compares in both directions: the fingerprint it computes must
equal the decision's *and* the job's own copy, and the applicability flag is an
equality mirror, not a must-be-false gate. Claiming memory on a run that would
send none, or denying it on one that would, ends the run.

Memory rides to **routed external agents**, unlike cross-session recall. The
divergence is deliberate: recall is withheld there because the transcript belongs
to a conversation the user pointed elsewhere, while the persona is how the user
asked to be spoken to, and they asked it of this conversation.

Pinned tiers are consented over **whole**, per run. Per-item selection and
sensitivity filtering are deferred, not implemented: nothing inspects a pinned
document for sensitive content. A whitespace-only `persona.md` therefore
attaches no category on either side, and a missing, unreadable, over-limit or
symlinked pinned document pins nothing, claims nothing, and surfaces as a
visible unavailable row instead of failing silently.

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

A decision recorded under an *older* decision version is a different case, and
it is deliberately not rescued. Migration 0019 keeps such a row exactly as it
was written, with an empty `memory_snapshot_fingerprint`, because a consent
given before memory existed disclosed no memory and must never be
retroactively credited with any. A job still queued under it is refused at
dispatch by the runtime's shape check — a typed, terminal
`egress_decision_invalid` that is never retried — and the way forward is the
person sending the message again under a disclosure they actually read.
Executing it would run against a disclosure nobody saw; rewriting its version
would forge that consent with this server's own signature. Failing closed is
the specified behaviour, pinned by
`agent-runtime-go/internal/agent/egress_version_skew_test.go` against the
literal pre-bump number.

## Background work and audit

Automations cannot inherit interactive consent. A remote effective route
advances the schedule but creates no run; it records the typed durable failure
`remote_egress_requires_interactive_consent` and a redacted audit row.
Automations receive no run-owned decision for a remote MCP destination either;
the proxy refuses the remote call rather than inheriting interactive consent.
Memory is refused on an unattended run at dispatch regardless of tool policy, so
an automation cannot read or propose memory even where a decision exists.

`egress.consent.recorded` exposes only provider, endpoint host, typed
categories, decision version, and consent timestamp. It never stores or returns
the challenge, nonce, raw fingerprint — including the memory snapshot
fingerprint — credentials, request content, recalled text, pinned persona or
profile bytes, belief note text, skill bodies, tool payloads, or attachments.
The transcript's routing notice is held to the same rule. Session deletion
cascades the decision and applies the existing audit scrub contract.
