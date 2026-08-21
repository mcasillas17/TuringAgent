# MCP Registry Implementation Plan

Register MCP servers — ours and other people's — instead of hardcoding two of
them in `docker-compose.yml`. Import from `mcp.json`.

The interesting part is not the registry. It is that **three of VISION's
invariants currently hold because we wrote both tool servers**, and a
third-party server does not inherit them. This plan says exactly which
guarantee changes, and refuses the tier where the change would be a lie.

## The problem, stated precisely

`docs/mcp-security-and-integration.md` describes a closed boundary: two
servers, fixed endpoints, internal Docker networks only, non-root, read-only
root filesystems, capabilities dropped, bounded request and response sizes.
Every one of those is a property of **our compose file**, not of MCP.

Worse, the approval invariant is *cooperative*:

> **Every mutation is approved, argument-bound, and single-use.** New mutating
> capability inherits the existing approval flow; it does not get its own
> weaker one.

It holds because `mcp-files` verifies the HS256 token against the actual call
and consumes it through `ApprovalService.ConsumeApproval`. **A third-party
server does none of that.** It receives a JSON-RPC call and runs it.

And since #66 that cooperation needs a specific identity —
`TURING_APPROVAL_CONSUMER_TOKEN`, deliberately split from the runtime's so a
compromised consumer does not gain the runtime's privileges. Handing that token
to someone else's server would let it consume approvals for calls it never
made. **No third-party server ever receives it.**

## Design decisions (locked)

**A third-party MCP server is a boundary crossing, not another tool server.**
The same category as routing a conversation to an external agent: opt in,
labelled where it is used, never a default path.

**Enforcement moves from callee to caller for servers that cannot verify.**
For ours, the server checks the token — unchanged. For a registered
third-party server, the *orchestrator* refuses to dispatch a mutating call
without a valid, unconsumed approval, and consumes it itself.

This is a different enforcement point, and the guarantee is narrower: it holds
because the orchestrator is the only path to that server, not because the
server would refuse a forged call. Written here rather than substituted
silently. `docs/mcp-security-and-integration.md` gains a section saying so.

**Four tiers, and one of them is refused:**

| Tier | Egress | Sandbox-confined | Approval enforced by |
|---|---|---|---|
| Bundled (`files`, `system`) | no | yes | the server, as today |
| Local container, third-party | no | **no** | the orchestrator |
| Remote URL | **yes** | no | the orchestrator, and labelled as egress |
| stdio / `npx` | — | — | **refused, see below** |

**stdio is refused for now.** Spawning `npx some-mcp-server` is VISION's
deferral 3 — "shell and native automation, under approval" — wearing a
different hat, and it puts a Node runtime in the backend, which the
single-language invariant exists to prevent. Refusing it is not a capability
judgement; it is that the honest version of it is a different, larger piece of
work with its own gate. The UI says this when an `mcp.json` entry uses
`command`, rather than importing it silently as broken.

**Importing is not enabling.** This is the rule already settled for skills, and
it applies unchanged. An imported server arrives **disabled**. Its tools arrive
`approval_required` — which `DefaultPolicyFor` already does for anything not in
its seed map, so no new defaulting logic is needed. A remote server additionally
requires acknowledging egress before it can be enabled.

**`mcp.json` is an import format, not the source of truth.** Read from a
mounted folder the way `skills/` is. Registered servers live in the database
because enabled/acknowledged are user decisions, exactly as with skills.

**Tokens are sealed with `internal/secretbox`.** That package exists from
Integrations, under `TURING_INTEGRATION_KEY`. A second scheme for the same
problem would be a second thing to get wrong.

## What is built

1. **Migration `0015`** — `mcp_servers` (id, name, transport, url, sealed
   token, tier, enabled, egress_acknowledged_at, created_at). Tools keep their
   existing table and gain a foreign key to it.
2. **Registry repository** — CRUD, plus reconciliation with `mcp.json`.
3. **Discovery against a registered server** — `tools/list` over JSON-RPC 2.0,
   the same shape the bundled servers speak. Failure is recorded and shown, not
   swallowed: a server that will not answer is visible as down rather than
   absent.
4. **Caller-side approval gate** — for non-bundled tiers, refuse dispatch of a
   `approval_required` tool without a valid unconsumed approval, then consume.
5. **Client** — the MCPs section grows from a read-only tool list to servers
   with their tools, liveness, tier, and the egress acknowledgement. Tool
   policy becomes editable.

## What is deliberately not built

- **stdio transport.** Above.
- **Sandbox confinement for third-party servers.** We cannot confine a process
  we did not write. The UI says a non-bundled server is not sandbox-confined
  rather than implying it is.
- **Publishing our servers to the host.** The boundary that says MCP ports are
  internal-only stays.

## The tests that pin the risky parts

Names may change; every assertion must survive.

```go
// 1. The invariant that a stranger's server must never be able to consume
//    approvals it did not earn.
func TestThirdPartyServerNeverReceivesTheApprovalConsumerIdentity(t *testing.T) {
	server := registerServer(t, tierLocalContainer, "http://vendor:9000/mcp")
	call := dispatchTo(t, server, "vendor.write", approvedArgs(t))

	if headerOf(call, "authorization") == approvalConsumerToken(t) {
		t.Fatal("the approval-consumer identity must never leave the orchestrator")
	}
}

// 2. Enforcement moved, not removed. A mutating call to a server that cannot
//    verify our token is refused by the orchestrator when unapproved.
func TestMutatingCallToANonCooperatingServerIsRefusedWithoutApproval(t *testing.T) {
	server := registerServer(t, tierLocalContainer, "http://vendor:9000/mcp")
	setPolicy(t, server, "vendor.write", PolicyApprovalRequired)

	_, err := dispatch(t, server, "vendor.write", map[string]any{"path": "x"})

	if err == nil {
		t.Fatal("an approval_required tool must not dispatch without an approval")
	}
	if reached(server) {
		t.Fatal("the call must be refused before it reaches the server")
	}
}

// 3. And the approval is single-use even though the server never consumed it.
func TestTheOrchestratorConsumesTheApprovalItselfExactlyOnce(t *testing.T) {
	server := registerServer(t, tierLocalContainer, "http://vendor:9000/mcp")
	approval := approve(t, server, "vendor.write", map[string]any{"path": "x"})

	mustDispatch(t, server, "vendor.write", map[string]any{"path": "x"})
	_, err := dispatch(t, server, "vendor.write", map[string]any{"path": "x"})

	if err == nil {
		t.Fatal("the same approval must not authorise a second call")
	}
	if statusOf(t, approval) != "consumed" {
		t.Fatal("the orchestrator must consume what the server cannot")
	}
}

// 4. Importing is not enabling, and egress is never acknowledged by import.
func TestImportingMcpJsonLeavesEverythingOff(t *testing.T) {
	importMcpJSON(t, `{"mcpServers":{"vendor":{"url":"https://vendor.example/mcp"}}}`)

	server := mustFindServer(t, "vendor")
	if server.Enabled {
		t.Fatal("an imported server must arrive disabled")
	}
	if server.EgressAcknowledgedAt != nil {
		t.Fatal("importing must not acknowledge egress on the user's behalf")
	}
	for _, tool := range toolsOf(t, server) {
		if tool.Policy == PolicySafe {
			t.Fatalf("%s arrived safe; an unknown tool must be approval_required", tool.Name)
		}
	}
}

// 5. A remote server cannot be switched on without the egress acknowledgement.
func TestRemoteServerCannotBeEnabledWithoutAcknowledgingEgress(t *testing.T) {
	server := importRemote(t, "https://vendor.example/mcp")

	if err := enable(t, server); err == nil {
		t.Fatal("enabling a remote server without acknowledging egress must fail")
	}
	acknowledgeEgress(t, server)
	if err := enable(t, server); err != nil {
		t.Fatalf("enable after acknowledgement: %v", err)
	}
}

// 6. A stdio entry is reported, not silently dropped or half-imported.
func TestStdioEntriesAreReportedAsUnsupported(t *testing.T) {
	report := importMcpJSON(t, `{"mcpServers":{"local":{"command":"npx","args":["x"]}}}`)

	if len(serversNamed(t, "local")) != 0 {
		t.Fatal("a stdio entry must not be registered")
	}
	if !strings.Contains(report.Unsupported["local"], "stdio") {
		t.Fatalf("report = %q, want it to say why stdio is unsupported", report.Unsupported["local"])
	}
}
```

## Documentation this closes

`docs/mcp-security-and-integration.md` is currently accurate and will stop
being so the moment a third server can exist. It needs:

- the deployment-boundary table to describe tiers rather than two fixed rows
- a section on caller-side enforcement, saying plainly what is guaranteed and
  what is not for a server we did not write
- the sandbox-confinement claim scoped to bundled servers

`docs/VISION.md` needs the approval invariant to gain a second qualification
beside the automations one — same shape, same honesty: what changes is *who*
enforces, not *whether*.

## Verification

The full matrix in `CLAUDE.md`. Beyond it: a registered server that is
unreachable, one that returns malformed `tools/list`, one that returns a tool
whose name collides with a bundled tool, and one that disappears between
discovery and dispatch.
