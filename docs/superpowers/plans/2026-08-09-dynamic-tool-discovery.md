# Dynamic Tool Discovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the two hardcoded sources of truth for tools (`SessionService.ListTools`'s static list and `service/tools/policy.go`'s static map) with a single DB-backed registry populated by the runtime reporting the tools it actually discovered from its MCP servers.

**Architecture:** The runtime already builds a tool registry from MCP `tools/list` (Plan #1, Phase 3). Extend the `ConnectWorker` handshake so the runtime reports those tools to the orchestrator on connect. The orchestrator upserts them into the existing (currently unused) `tools` table via a new `repository/tools.go`. `ListTools` reads from the table; `tools.GetPolicy` reads the `policy` column instead of the hardcoded map. Policy defaults are seeded from a small default-policy function so a newly discovered tool is never accidentally `safe`.

**Tech Stack:** Go 1.23, orchestrator-go, proto (`proto/turing/v1/runtime.proto`, `sessions.proto`), generated code committed (`tools/proto/generate.sh`), SQLite `tools` table (already in `0001_initial.sql`).

**Depends on:** Plan #1 (the runtime tool registry). Ships value only once the runtime can enumerate tools; can be built in parallel but tested together.

---

## Design decisions (locked)

1. **Runtime-reported, not orchestrator-introspected.** The orchestrator has no MCP client and shouldn't gain one (that would breach the network-isolation model where only the runtime reaches MCP servers). The runtime is the only component that can call `tools/list`, so it reports.
2. **Transport: extend `RuntimeWorkerReady`** with `repeated DiscoveredTool tools` and `ToolDiscoveryStatus tool_discovery_status`. This rides the existing first-message handshake — no new RPC, no new `RuntimeUpdate` variant. A runtime MUST send `COMPLETE` after a successful discovery attempt, including when it discovered zero tools, and `FAILED` when an attempted discovery fails. `UNSPECIFIED` is reserved for legacy runtimes that cannot report capabilities. The orchestrator rejects `FAILED` workers so a discovery failure can never activate compatibility defaults. (`ConnectWorker` already validates `RuntimeWorkerReady` first.)
3. **Policy stays authoritative in the orchestrator.** The runtime reports `server_name`, `tool_name`, and the tool's JSON schema — NOT its policy. The orchestrator assigns policy via a `defaultPolicyFor(toolName)` function on first discovery (default-deny bias: unknown → `approval_required`, never `safe`), and thereafter the DB row is the source of truth. This preserves "the orchestrator owns security decisions."
4. **Upsert semantics.** `(server_name, tool_name)` is unique; re-reporting updates `schema_json`/`discovered_at`/`enabled` but never downgrades an operator-set policy. Tools no longer reported are marked `enabled = 0`, not deleted (audit trail).
5. **`AgentId` stays a closed enum.** Dynamic *agent* discovery is explicitly out of scope — `AGENT_ID_GENERAL_ASSISTANT` remains the only agent (the plan defers multi-agent). `ListAgents` stays as-is. This plan is tools-only despite the "discovery" name.

## File structure

- Modify: `proto/turing/v1/runtime.proto` — add `DiscoveredTool` message + `repeated DiscoveredTool tools` to `RuntimeWorkerReady`.
- Regenerate: `tools/proto/generate.sh` → commit `gen/` + Dart output.
- Create: `turing-backend/orchestrator-go/internal/repository/tools.go` — `UpsertTools`, `ListEnabledTools`, `GetToolPolicy`.
- Create: `turing-backend/orchestrator-go/internal/service/tools/defaults.go` — `defaultPolicyFor`.
- Modify: `turing-backend/orchestrator-go/internal/service/tools/policy.go` — `GetPolicy` reads the repo (with the static map as a fallback seed only).
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/service.go` — on `RuntimeWorkerReady`, upsert reported tools.
- Modify: `turing-backend/orchestrator-go/internal/service/sessions/service.go` — `ListTools` reads the repo.
- Modify: `turing-backend/agent-runtime-go/internal/worker/worker.go` (+ `cmd/runtime/main.go`) — populate `RuntimeWorkerReady.Tools` from the registry.

Verification: root `go test ./... -count=1 && go build ./...`, `tools/proto/check.sh`, plus the runtime module.

---

## Phase 0 — Proto: report tools on connect

### Task 0: Add `DiscoveredTool` to the handshake

**Files:**
- Modify: `proto/turing/v1/runtime.proto`
- Test: `turing-backend/tests/proto_contract_test.go`

- [ ] **Step 1: Extend the proto**

In `runtime.proto`, add near `RuntimeWorkerReady`:

```proto
message DiscoveredTool {
  string server_name = 1;
  string tool_name = 2;
  google.protobuf.Struct schema = 3; // JSON Schema for the tool's arguments
}

enum ToolDiscoveryStatus {
  TOOL_DISCOVERY_STATUS_UNSPECIFIED = 0; // Legacy runtime without discovery support
  TOOL_DISCOVERY_STATUS_COMPLETE = 1;    // Authoritative snapshot, including empty
  TOOL_DISCOVERY_STATUS_FAILED = 2;      // Discovery attempted but failed
}

message RuntimeWorkerReady {
  string worker_id = 1;
  AgentId agent_id = 2;
  int32 max_concurrent_runs = 3;
  repeated DiscoveredTool tools = 4; // NEW: tools the runtime discovered from its MCP servers
  ToolDiscoveryStatus tool_discovery_status = 5; // NEW: discovery outcome and snapshot authority
}
```

Ensure `import "google/protobuf/struct.proto";` is present (it is used elsewhere; confirm at top of file).

- [ ] **Step 2: Add a contract assertion** in `proto_contract_test.go` that `RuntimeWorkerReady` has a `tools` field and `DiscoveredTool` exists (mirror the existing "required messages exist" assertions). Run it and confirm it FAILS before regeneration.

Run: `cd turing-backend && go test ./tests/ -run ProtoContract -v` → FAIL.

- [ ] **Step 3: Regenerate + verify determinism**

```bash
tools/proto/generate.sh
tools/proto/check.sh   # must exit clean
```

- [ ] **Step 4: Contract test passes**

Run: `cd turing-backend && go test ./tests/ -run ProtoContract -v` → PASS.

- [ ] **Step 5: Commit** (include regenerated `gen/` and Dart)

```bash
git add proto/turing/v1/runtime.proto gen/ turing-client/turing_app/lib/gen/ turing-backend/tests/proto_contract_test.go
git commit -m "feat(proto): report discovered tools in RuntimeWorkerReady handshake"
```

---

## Phase 1 — Tools repository (the missing `tools` table code)

### Task 1: `repository/tools.go`

**Files:**
- Create: `turing-backend/orchestrator-go/internal/repository/tools.go`
- Test: `turing-backend/orchestrator-go/internal/repository/tools_test.go`

- [ ] **Step 1: Write the failing test** (uses the existing in-memory/temp-file DB test helper pattern from `sessions_test.go`)

```go
func TestUpsertAndListTools(t *testing.T) {
	repo := newTestRepo(t) // applies migrations to a temp sqlite file

	err := repo.UpsertTools(context.Background(), []repository.DiscoveredTool{
		{ServerName: "system", ToolName: "system.time", SchemaJSON: `{"type":"object"}`, Policy: "safe"},
		{ServerName: "files", ToolName: "files.create", SchemaJSON: `{"type":"object"}`, Policy: "approval_required"},
	})
	if err != nil { t.Fatal(err) }

	tools, err := repo.ListEnabledTools(context.Background())
	if err != nil { t.Fatal(err) }
	if len(tools) != 2 { t.Fatalf("want 2, got %d", len(tools)) }

	// re-upsert updates schema but a policy already set is preserved when caller passes empty
	pol, ok, err := repo.GetToolPolicy(context.Background(), "files.create")
	if err != nil || !ok || pol != "approval_required" {
		t.Fatalf("policy lookup: %q ok=%v err=%v", pol, ok, err)
	}
	if _, ok, _ := repo.GetToolPolicy(context.Background(), "nope"); ok {
		t.Fatal("unknown tool should not resolve")
	}
}
```

- [ ] **Step 2: Run, confirm failure.**
Run: `cd turing-backend/orchestrator-go && go test ./internal/repository/ -run UpsertAndListTools -v` → FAIL (undefined).

- [ ] **Step 3: Implement** against the existing `tools` schema (`id, server_name, tool_name, policy, schema_json, enabled, discovered_at, UNIQUE(server_name,tool_name)`):

```go
package repository

import (
	"context"
	"database/sql"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

type DiscoveredTool struct {
	ServerName string
	ToolName   string
	SchemaJSON string
	Policy     string // "safe" | "approval_required" | "disabled"
}

func (r *Repository) UpsertTools(ctx context.Context, tools []DiscoveredTool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil { return err }
	defer func() { _ = tx.Rollback() }()

	for _, t := range tools {
		// ON CONFLICT: refresh schema/discovered_at/enabled, keep existing policy.
		_, err := tx.ExecContext(ctx, `
			INSERT INTO tools (id, server_name, tool_name, policy, schema_json, enabled, discovered_at)
			VALUES (?, ?, ?, ?, ?, 1, datetime('now'))
			ON CONFLICT(server_name, tool_name) DO UPDATE SET
				schema_json = excluded.schema_json,
				enabled = 1,
				discovered_at = excluded.discovered_at`,
			ids.New("tool"), t.ServerName, t.ToolName, t.Policy, t.SchemaJSON)
		if err != nil { return err }
	}
	return tx.Commit()
}

func (r *Repository) ListEnabledTools(ctx context.Context) ([]DiscoveredTool, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT server_name, tool_name, policy, schema_json FROM tools WHERE enabled = 1 ORDER BY server_name, tool_name`)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []DiscoveredTool
	for rows.Next() {
		var t DiscoveredTool
		if err := rows.Scan(&t.ServerName, &t.ToolName, &t.Policy, &t.SchemaJSON); err != nil { return nil, err }
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repository) GetToolPolicy(ctx context.Context, toolName string) (string, bool, error) {
	var policy string
	err := r.db.QueryRowContext(ctx,
		`SELECT policy FROM tools WHERE tool_name = ? AND enabled = 1`, toolName).Scan(&policy)
	if err == sql.ErrNoRows { return "", false, nil }
	if err != nil { return "", false, err }
	return policy, true, nil
}
```

(Confirm the `ids.New` prefix helper's actual signature; the `messages` table uses `msg`, `tools` should use `tool`.)

- [ ] **Step 4: Run, confirm pass.** `go test ./internal/repository/ -run UpsertAndListTools -v` → PASS.

- [ ] **Step 5: Commit.**
```bash
git add turing-backend/orchestrator-go/internal/repository/tools.go turing-backend/orchestrator-go/internal/repository/tools_test.go
git commit -m "feat(orchestrator): tools repository (upsert/list/policy) over the tools table"
```

---

## Phase 2 — Default policy for newly discovered tools

### Task 2: `defaultPolicyFor`

**Files:**
- Create: `turing-backend/orchestrator-go/internal/service/tools/defaults.go`
- Test: `turing-backend/orchestrator-go/internal/service/tools/defaults_test.go`

- [ ] **Step 1: Failing test**

```go
func TestDefaultPolicyIsDenyBiased(t *testing.T) {
	if defaultPolicyFor("system.time") != PolicySafe { t.Fatal("known-safe tool should be safe") }
	if defaultPolicyFor("files.create") != PolicyApprovalRequired { t.Fatal() }
	if defaultPolicyFor("files.delete") != PolicyDisabled { t.Fatal("delete/move stay disabled") }
	if defaultPolicyFor("brand.new.tool") != PolicyApprovalRequired { t.Fatal("unknown must NOT default to safe") }
}
```

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement** — reuse the current static map as the *seed* of known policies; everything else is `approval_required`; `files.delete`/`files.move` are permanently `disabled` (matches the "cannot be enabled without a code change" invariant):

```go
package tools

func defaultPolicyFor(toolName string) Policy {
	switch toolName {
	case "files.delete", "files.move":
		return PolicyDisabled
	}
	if p, ok := seedPolicies[toolName]; ok {
		return p
	}
	return PolicyApprovalRequired // default-deny bias
}

var seedPolicies = map[string]Policy{
	"system.time":   PolicySafe,
	"system.health": PolicySafe,
	"system.echo":   PolicySafe,
	"system.info":   PolicySafe,
	"files.list":    PolicySafe,
	"files.search":  PolicySafe,
	"files.read":    PolicySafe,
	"files.create":  PolicyApprovalRequired,
	"files.update":  PolicyApprovalRequired,
}
```

- [ ] **Step 4: Run → PASS.**
- [ ] **Step 5: Commit.**
```bash
git add turing-backend/orchestrator-go/internal/service/tools/defaults.go turing-backend/orchestrator-go/internal/service/tools/defaults_test.go
git commit -m "feat(orchestrator): default-deny policy seeding for discovered tools"
```

---

## Phase 3 — Persist reported tools on connect

### Task 3: Upsert in `ConnectWorker`

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/service.go` (handshake at lines ~68-102)
- Test: `turing-backend/orchestrator-go/internal/service/runtime/service_test.go`

- [ ] **Step 1: Failing test** — a fake worker sends `RuntimeWorkerReady{AgentId: GENERAL_ASSISTANT, Tools: [...system.time, files.create...]}`; assert the tools table now lists them with policies from `defaultPolicyFor`. (Extend the existing ConnectWorker test.)

- [ ] **Step 2: Run → FAIL.**

- [ ] **Step 3: Implement** — after the existing `ready.AgentId` validation, map `ready.GetTools()` into `[]repository.DiscoveredTool` (schema `structpb.Struct` → JSON string via `protojson` or `safejson`), assign `Policy: string(defaultPolicyFor(t.GetToolName()))`, and call `repo.UpsertTools(ctx, ...)`. Log-and-continue on upsert error (don't fail the worker connection over discovery). Only persist policy on *first* insert — the `ON CONFLICT` clause already preserves existing policy, so re-report is safe.

- [ ] **Step 4: Run → PASS.**
- [ ] **Step 5: Commit.**
```bash
git commit -am "feat(orchestrator): persist runtime-reported tools on ConnectWorker"
```

---

## Phase 4 — Read from the DB (retire the hardcoded lists)

### Task 4a: `tools.GetPolicy` reads the repo

**Files:** modify `service/tools/policy.go`, and its caller `service/runtime/service.go:590` (`handleToolBefore`).

- [ ] **Step 1: Failing test** — insert a tool with `approval_required`, assert enforcement uses the DB value; insert nothing, assert an unknown tool is denied (`ok == false`).
- [ ] **Step 2: Run → FAIL.**
- [ ] **Step 3: Implement** — change `GetPolicy` to a method that takes a repo (or inject the repo into the runtime service and call `repo.GetToolPolicy`). Keep the `Policy` string constants. Fallback: if the DB has no row (e.g. discovery hasn't run yet), fall back to `defaultPolicyFor` so the system is never wide-open. Preserve the existing deny reasons (`unknown_tool`, `approval_args_missing`, `tool_disabled`).
- [ ] **Step 4: Run → PASS.**
- [ ] **Step 5: Commit.**

### Task 4b: `SessionService.ListTools` reads the repo

**Files:** modify `service/sessions/service.go:105-111`.

- [ ] **Step 1: Failing test** — after upserting two tools, `ListTools` returns exactly those two `ToolDescriptor`s with the DB policies (not the hardcoded pair).
- [ ] **Step 2: Run → FAIL.**
- [ ] **Step 3: Implement** — `repo.ListEnabledTools(ctx)` → map each to `&turingv1.ToolDescriptor{ServerName, ToolName, Policy: toProtoPolicy(t.Policy)}` where `toProtoPolicy` maps the string to `ToolPolicy_TOOL_POLICY_*`. Remove the hardcoded slice.
- [ ] **Step 4: Run → PASS.**
- [ ] **Step 5: Commit.**
```bash
git commit -am "feat(orchestrator): ListTools and policy enforcement read the tools table"
```

---

## Phase 5 — Runtime populates the handshake

### Task 5: Report the registry on connect

**Files:** modify `agent-runtime-go/internal/worker/worker.go` (the `RuntimeWorkerReady` it sends in `Run`) and its wiring in `cmd/runtime/main.go`.

- [ ] **Step 1: Failing test** — worker unit test asserting the `RuntimeWorkerReady` sent on connect carries the tools from an injected registry.
- [ ] **Step 2: Run → FAIL.**
- [ ] **Step 3: Implement** — pass the Plan #1 `ToolRegistry` (or a `[]DiscoveredTool` snapshot) into `worker.Options`; when building `RuntimeWorkerReady`, populate `Tools` from registry entries (`server_name`, `tool_name`, `schema` as `structpb.Struct` from the tool's `Parameters`) and set `ToolDiscoveryStatus: TOOL_DISCOVERY_STATUS_COMPLETE` after discovery succeeds. `COMPLETE` is required when the resulting snapshot is empty. If discovery fails, send `TOOL_DISCOVERY_STATUS_FAILED` (or do not connect and retry); never leave the status `UNSPECIFIED`, because that is reserved for legacy compatibility defaults. Build the registry once at startup in `main.go` (it already exists post-Plan #1) and hand it to the worker.
- [ ] **Step 4: Run → PASS**, then `cd turing-backend/agent-runtime-go && go test ./... -count=1 && go build ./...`.
- [ ] **Step 5: Commit.**
```bash
git commit -am "feat(runtime): report discovered tools to orchestrator on connect"
```

---

## Phase 6 — End-to-end + full matrix

### Task 6: Discovery e2e

**Files:** modify `turing-backend/tests/grpc_harness_test.go`.

- [ ] Add `TestDiscoveredToolsAppearInListTools`: bring up the stack with a fake MCP advertising `system.time` + `files.create`; after the worker connects, call `SessionService.ListTools` and assert both appear with correct policies; call an approval-required tool and assert enforcement used the DB policy.
- [ ] Run the full verification matrix via the `/verify` skill (root, mcp-files, mcp-system, flutter, proto check).
- [ ] Commit.

---

## Self-review checklist

- **Spec coverage:** report-on-connect (Task 0,5) ✓; persist (Task 1,3) ✓; default-deny for unknown tools (Task 2) ✓; DB-backed enforcement + listing (Task 4a,4b) ✓; e2e (Task 6) ✓.
- **Two-sources-of-truth eliminated:** both `ListTools` and `GetPolicy` now read the same `tools` table ✓.
- **Security preserved:** runtime reports schema only, orchestrator assigns policy; unknown → `approval_required`; `files.delete`/`move` stay `disabled` ✓.
- **Out of scope (documented):** dynamic *agent* discovery (`AgentId` stays a closed enum); operator UI to edit policies (DB is editable directly for now).
- **Proto discipline:** `generate.sh` run, `check.sh` clean, `gen/` + Dart committed, `proto_contract_test.go` updated ✓.
```
