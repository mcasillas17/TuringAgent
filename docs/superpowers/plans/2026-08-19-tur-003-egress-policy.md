# TUR-003 Explicit Remote Egress Policy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Require a one-time, exact-context consent decision for every interactive remote run, persist its disclosed data categories, reject insecure or redirected keyed endpoints, and keep local failures and background work local.

**Architecture:** The orchestrator resolves the effective provider and endpoint before enqueue, issues a short-lived disclosure challenge signed with dedicated server-only key material and bound to the normalized request, and atomically consumes that challenge into a run-owned egress decision. Exact existing idempotent replay is resolved before expiry and nonce-use checks. The frozen decision travels with the job and is revalidated by the runtime before model I/O; a shared endpoint package enforces one canonical keyed-endpoint policy in both processes, while the Flutter client confirms every remote send without persisting consent.

**Tech Stack:** Go 1.23, SQLite, gRPC/protobuf, Flutter/Dart, `net/url`, `net/netip`, HMAC-SHA256, existing audit/event/idempotency infrastructure.

---

### Task 1: Add typed egress protocol contracts

**Files:**
- Modify: `proto/turing/v1/common.proto`
- Modify: `proto/turing/v1/chat.proto`
- Modify: `proto/turing/v1/runtime.proto`
- Modify: `proto/turing/v1/sessions.proto`
- Modify: `turing-backend/tests/proto_contract_test.go`
- Generate: `gen/turing/v1/go/turing/v1/*.pb.go`
- Generate: `turing-client/turing_app/lib/generated/turing/v1/*.pb*.dart`

- [ ] **Step 1: Write failing proto contract tests**

Assert that `SendMessageRequest` has a `remote_egress_consent` field, `ChatService` exposes `PrepareRemoteEgress`, `AgentJob` carries `RunEgressDecision`, and every required category enum is present.

- [ ] **Step 2: Run the contract test and verify it fails**

Run: `go test -tags sqlite_fts5 ./turing-backend/tests -run 'TestProto.*Egress' -count=1`

Expected: FAIL because the messages, RPC, and enum do not exist.

- [ ] **Step 3: Add the typed messages**

Define:

```proto
enum EgressDataCategory {
  EGRESS_DATA_CATEGORY_UNSPECIFIED = 0;
  EGRESS_DATA_CATEGORY_CURRENT_MESSAGE = 1;
  EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY = 2;
  EGRESS_DATA_CATEGORY_CROSS_SESSION_RECALL = 3;
  EGRESS_DATA_CATEGORY_MEMORY_PROFILE = 4;
  EGRESS_DATA_CATEGORY_SKILL_CONTENT = 5;
  EGRESS_DATA_CATEGORY_TOOL_SCHEMAS = 6;
  EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS = 7;
  EGRESS_DATA_CATEGORY_TOOL_RESULTS = 8;
  EGRESS_DATA_CATEGORY_ATTACHMENTS = 9;
}

message RemoteEgressDisclosure {
  string challenge = 1;
  ModelProvider provider = 2;
  string model = 3;
  string endpoint = 4;
  repeated EgressDataCategory data_categories = 5;
  google.protobuf.Timestamp expires_at = 6;
}

message RemoteEgressConsent {
  string challenge = 1;
  repeated EgressDataCategory acknowledged_data_categories = 2;
  bool acknowledged = 3;
}

message RunEgressDecision {
  string decision_id = 1;
  ModelProvider provider = 2;
  string model = 3;
  string endpoint = 4;
  repeated EgressDataCategory data_categories = 5;
  google.protobuf.Timestamp consent_granted_at = 6;
  string request_fingerprint = 7;
}
```

Add `PrepareRemoteEgressRequest` with every routing and payload-shaping field used by `SendMessageRequest`, including the idempotency key, add `PrepareRemoteEgress` to `ChatService`, add consent to `SendMessageRequest`, add the frozen decision and selected tool names to `AgentJob`, and expose remote endpoint metadata on `ProviderConfig` without credential references.

- [ ] **Step 4: Generate and verify the contract**

Run: `tools/proto/generate.sh && go test -tags sqlite_fts5 ./turing-backend/tests -run 'TestProto.*Egress' -count=1`

Expected: PASS.

### Task 2: Enforce canonical keyed endpoint security

**Files:**
- Create: `turing-backend/internal/egress/endpoint.go`
- Create: `turing-backend/internal/egress/endpoint_test.go`
- Modify: `turing-backend/orchestrator-go/internal/config/config.go`
- Modify: `turing-backend/agent-runtime-go/internal/config/config.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/external_agents.go`
- Modify: `turing-backend/agent-runtime-go/internal/llm/openai_compatible.go`
- Modify: related config, repository, and provider tests

- [ ] **Step 1: Write failing table tests**

Cover HTTPS remote endpoints, HTTP `localhost`, HTTP `127.0.0.1`, HTTP `[::1]`, rejection of HTTP `host.docker.internal`, arbitrary hostnames and non-loopback IPs, and rejection of userinfo, query, fragment, relative URLs, and non-HTTP schemes.

- [ ] **Step 2: Run endpoint tests and verify they fail**

Run: `go test -tags sqlite_fts5 ./turing-backend/internal/egress ./turing-backend/orchestrator-go/internal/config ./turing-backend/agent-runtime-go/internal/config ./turing-backend/orchestrator-go/internal/repository -run 'Endpoint|BaseURL' -count=1`

Expected: FAIL because the shared validator does not exist and keyed `host.docker.internal` is currently allowed.

- [ ] **Step 3: Implement one typed endpoint classifier**

Use a closed security class:

```go
type EndpointClass int

const (
	EndpointHTTPS EndpointClass = iota + 1
	EndpointLoopbackHTTP
)

type Endpoint struct {
	Canonical string
	Host      string
	Class     EndpointClass
}

func ParseKeyedEndpoint(raw string) (Endpoint, error)
```

Only exact `localhost`, `netip.Addr.IsLoopback()` IP literals, or HTTPS pass. Normalize scheme/host/default ports and trailing slashes without DNS resolution.

- [ ] **Step 4: Reject all remote-provider redirects**

Copy the supplied `http.Client`, set `CheckRedirect` to return a typed `RedirectError`, and use that client for direct OpenAI-compatible and external-agent providers. Add an `httptest` redirect regression proving no second request reaches the redirect target.

- [ ] **Step 5: Run endpoint/provider tests**

Run: `go test -tags sqlite_fts5 ./turing-backend/internal/egress ./turing-backend/orchestrator-go/internal/config ./turing-backend/agent-runtime-go/internal/config ./turing-backend/agent-runtime-go/internal/llm ./turing-backend/orchestrator-go/internal/repository -count=1`

Expected: PASS.

### Task 3: Persist one immutable egress decision per run

**Files:**
- Create: `turing-backend/orchestrator-go/internal/db/schema/0014_run_egress_decisions.sql`
- Modify: `turing-backend/orchestrator-go/internal/db/schema_invariants_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/jobs.go`
- Create: `turing-backend/orchestrator-go/internal/repository/egress.go`
- Create: `turing-backend/orchestrator-go/internal/repository/egress_test.go`
- Modify: migration and restart tests

- [ ] **Step 1: Write failing migration and repository tests**

Prove the table is run-cascade-owned, one run has at most one decision, a challenge nonce can create at most one run under concurrent attempts, session deletion removes the decision, restart preserves the frozen job decision, exact replay succeeds after expiry and route changes, and changed payload/provider/categories conflict with TUR-001 idempotency.

- [ ] **Step 2: Run repository tests and verify they fail**

Run: `go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/db ./turing-backend/orchestrator-go/internal/repository -run 'Egress|Migration|Idempot' -count=1`

Expected: FAIL because no decision table or enqueue input exists.

- [ ] **Step 3: Add bounded schema and typed repository models**

Create `run_egress_decisions` with:

```sql
decision_id TEXT PRIMARY KEY,
decision_version INTEGER NOT NULL,
run_id TEXT NOT NULL UNIQUE REFERENCES agent_runs(id) ON DELETE CASCADE,
challenge_nonce TEXT NOT NULL UNIQUE,
request_fingerprint TEXT NOT NULL,
provider TEXT NOT NULL,
model_name TEXT NOT NULL,
external_agent_id TEXT,
endpoint TEXT NOT NULL,
endpoint_host TEXT NOT NULL,
data_categories_json TEXT NOT NULL,
consent_granted_at TEXT NOT NULL
```

Add `RunEgressDecision` and `PendingEgressDecision` Go types, canonical sorted category encoding, and include session, idempotency key, exact request payload digest, effective provider/model/agent/endpoint, frozen tool names, eligible-skill snapshot digest, attachment selection, recall/memory flags, categories, expiry, and nonce in enqueue fingerprint version 3.

- [ ] **Step 4: Insert decision, job snapshot, event, and audit atomically**

Resolve the effective route before writes. For remote routes require `PendingEgressDecision`, insert it after the run row, put it in `jobs.payload_json`, append a run notice naming endpoint host and categories, and record `egress.consent.recorded` with metadata only. Local routes reject a supplied decision.

- [ ] **Step 5: Run migration/repository tests**

Run: `go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/db ./turing-backend/orchestrator-go/internal/repository -count=1`

Expected: PASS.

### Task 4: Prepare and validate one-time interactive consent

**Files:**
- Create: `turing-backend/orchestrator-go/internal/service/chat/egress.go`
- Create: `turing-backend/orchestrator-go/internal/service/chat/egress_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/chat/service.go`
- Modify: `turing-backend/orchestrator-go/internal/app/app.go`
- Modify: `turing-backend/orchestrator-go/internal/service/sessions/service.go`
- Modify: chat/session service tests

- [ ] **Step 1: Write failing service tests**

Cover local requests returning no disclosure, direct OpenAI and routed-agent disclosures, missing/false/stale/malformed consent, changed content/provider/model/endpoint/categories, duplicate category rejection, challenge reuse for a second run, exact idempotent replay, and no persistence on validation failure.

- [ ] **Step 2: Run chat tests and verify they fail**

Run: `go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/chat ./turing-backend/orchestrator-go/internal/service/sessions -run 'Egress|Consent|Disclosure' -count=1`

Expected: FAIL because the RPC and validator do not exist.

- [ ] **Step 3: Implement signed short-lived disclosures**

Use HMAC-SHA256 with dedicated `TURING_EGRESS_SIGNING_SECRET` material held only by the orchestrator and the fixed domain `turing.remote-egress.challenge.v1`. The signed payload contains a random nonce, expiry, normalized request fingerprint, effective provider/model/agent/canonical endpoint, frozen tool names, eligible-skill snapshot digest, attachment selection, recall/memory flags, and sorted categories. Verification checks signature before decoding, rejects noncanonical or oversized inputs, requires acknowledgment and exact category equality, and recomputes the current context.

For `SendMessage`, decode and authenticate the challenge first without applying expiry or nonce-use checks, compute the enqueue fingerprint, and resolve the idempotency record. Return an exact existing run even after expiry or route/config drift; reject conflicting key reuse. Only a new operation proceeds to expiry, current-context, and nonce validation.

- [ ] **Step 4: Compute honest categories**

Direct OpenAI disclosures include the conservative maximum of current message, conversation history, cross-session recall, skill content, tool schemas, tool arguments, and tool results. Routed external agents omit recall because runtime already suppresses it. Runtime use may be a subset, never a superset. Memory/profile and attachments remain defined but are not disclosed until a code path can send them.

- [ ] **Step 5: Run chat/session tests**

Run: `go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/chat ./turing-backend/orchestrator-go/internal/service/sessions -count=1`

Expected: PASS.

### Task 5: Revalidate the frozen decision before provider I/O

**Files:**
- Modify: `turing-backend/agent-runtime-go/internal/llm/provider.go`
- Modify: `turing-backend/agent-runtime-go/internal/llm/openai_compatible.go`
- Modify: `turing-backend/agent-runtime-go/internal/llm/ollama.go`
- Modify: `turing-backend/agent-runtime-go/internal/agent/general_assistant.go`
- Create: `turing-backend/agent-runtime-go/internal/agent/egress_test.go`
- Modify: runtime mapping tests

- [ ] **Step 1: Write failing runtime tests**

Prove remote jobs fail before `StreamChat` on absent decisions, provider mismatch, endpoint mismatch, or missing categories; local jobs reject remote decisions; local provider failure never invokes the external resolver; and exact retry of the same job remains valid.

- [ ] **Step 2: Run runtime tests and verify they fail**

Run: `go test -tags sqlite_fts5 ./turing-backend/agent-runtime-go/internal/agent ./turing-backend/orchestrator-go/internal/service/runtime -run 'Egress|Fallback|MapJob' -count=1`

Expected: FAIL because runtime does not inspect an egress decision.

- [ ] **Step 3: Add provider endpoint identity and runtime gate**

Add an optional typed interface:

```go
type EgressProvider interface {
	Provider
	EgressEndpoint() string
}
```

OpenAI-compatible providers return their canonical endpoint; Ollama remains local. Before context assembly/model dispatch, require the frozen decision for remote providers, compare provider/model/endpoint, and require every category the runtime may emit. Return `egress_decision_invalid` without retry or fallback.

- [ ] **Step 4: Run runtime tests**

Run: `go test -tags sqlite_fts5 ./turing-backend/agent-runtime-go/internal/agent ./turing-backend/agent-runtime-go/internal/llm ./turing-backend/orchestrator-go/internal/service/runtime -count=1`

Expected: PASS.

### Task 6: Block background inheritance and expose redacted audit metadata

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/repository/automations.go`
- Modify: automation repository/service tests
- Modify: `proto/turing/v1/audit.proto`
- Modify: `turing-backend/orchestrator-go/internal/service/audit/service.go`
- Modify: audit tests and Flutter audit model/mappers

- [ ] **Step 1: Write failing automation and audit tests**

Prove a due automation whose session resolves remote creates no run/job/decision, enters a typed durable failed state, and records a bounded blocked reason, while local automation still queues. Prove audit reads expose provider, endpoint host, typed category enums, decision version, and consent timestamp only, never challenge, nonce, raw fingerprint, credential reference, prompt, history, recall, tool payloads, or full endpoint.

- [ ] **Step 2: Run tests and verify they fail**

Run: `go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository ./turing-backend/orchestrator-go/internal/service/automations ./turing-backend/orchestrator-go/internal/service/audit -run 'Automation.*Egress|Egress.*Audit' -count=1`

Expected: FAIL because remote automation currently inherits routing and audit has no egress policy.

- [ ] **Step 3: Fail background remote routing closed**

After resolving a due automation route but before enqueue, persist a failed automation occurrence with `remote_egress_requires_interactive_consent`. Do not silently skip and do not consult any interactive challenge or prior run decision.

- [ ] **Step 4: Add audit allowlist fields**

Add `endpoint_host` and repeated `egress_data_categories` to `AuditPayload`. Only `egress.consent.recorded` may populate them, parsed through bounded closed-enum mapping.

- [ ] **Step 5: Run automation/audit tests**

Run: `go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository ./turing-backend/orchestrator-go/internal/service/automations ./turing-backend/orchestrator-go/internal/service/audit -count=1`

Expected: PASS.

### Task 7: Add per-send Flutter disclosure and consent

**Files:**
- Create: `turing-client/turing_app/lib/models/remote_egress.dart`
- Create: `turing-client/turing_app/lib/features/chat/remote_egress_dialog.dart`
- Modify: `turing-client/turing_app/lib/networking/api_client.dart`
- Modify: `turing-client/turing_app/lib/networking/grpc_client.dart`
- Modify: `turing-client/turing_app/lib/features/chat/chat_screen.dart`
- Modify: provider selector/settings copy
- Create/modify: focused Flutter tests

- [ ] **Step 1: Write failing widget/client tests**

Cover an OpenAI send showing endpoint and every disclosed category, cancel producing no send, confirmation forwarding the exact challenge/categories, a second run prompting again, changed draft/provider re-preparing, exact unconfirmed enqueue retry reusing its idempotency key and consent, and local Ollama sending without a dialog.

- [ ] **Step 2: Run Flutter tests and verify they fail**

Run: `( cd turing-client/turing_app && flutter test test/features/remote_egress_dialog_test.dart test/features/chat_screen_test.dart test/networking/grpc_client_test.dart )`

Expected: FAIL because no disclosure API or dialog exists.

- [ ] **Step 3: Implement typed client and dialog**

Add `prepareRemoteEgress` to `TuringApi`; map proto categories to stable user-facing labels. In `_sendMessage`, prepare after capturing the exact draft/provider, show a modal only when a disclosure is returned, and call `sendMessage` with the confirmed consent. Store consent only in `_RetryableSend` for the same unconfirmed enqueue attempt; never in `ClientAuthStorage`.

- [ ] **Step 4: Run focused Flutter tests**

Run: `( cd turing-client/turing_app && flutter test test/features/remote_egress_dialog_test.dart test/features/chat_screen_test.dart test/networking/grpc_client_test.dart )`

Expected: PASS.

### Task 8: Update architecture, privacy, security, README, and roadmap

**Files:**
- Create: `docs/architecture/remote-egress-policy.md`
- Modify: `docs/architecture/2026-08-18-personal-agent-audit.md`
- Modify: `docs/architecture/memory-governance.md`
- Modify: `docs/VISION.md`
- Modify: `docs/mcp-security-and-integration.md`
- Modify: `README.md`
- Modify: `turing-client/turing_app/README.md`
- Modify: `turing-backend/.env.example`

- [ ] **Step 1: Document exact guarantees and limits**

State the one-time challenge lifecycle, category meanings, direct OpenAI versus routed-agent differences, unsupported memory/attachments, loopback-only plaintext exception, redirect refusal, automation prohibition, idempotent replay semantics, audit redaction, SQLite/backup lifecycle, and absence of local-to-remote fallback.

- [ ] **Step 2: Mark TUR-003 pending merge**

Add a pending-merge artifact and coverage summary under TUR-003 without claiming it has landed.

- [ ] **Step 3: Check documentation references**

Run: `rg -n 'TUR-003|remote egress|OPENAI_BASE_URL|host.docker.internal' README.md docs turing-client/turing_app/README.md turing-backend/.env.example`

Expected: every configuration and UX surface states the same endpoint and consent rules.

### Task 9: Review, verify, commit, push, and open the PR

**Files:**
- All changed files

- [ ] **Step 1: Run fresh parallel full-diff reviews**

Dispatch Claude Opus 5 and GPT-5.6 Luna with xhigh effort and long context. Fix every valid finding and repeat fresh reviews until both explicitly report no remaining feedback.

- [ ] **Step 2: Run the required Claude Opus 4.8 review**

Review the final full diff for correctness, simplification, naming, and missing unit tests. Address every valid finding.

- [ ] **Step 3: Run the full verification skill**

Invoke `/verify` and require every root Go, race, build, nested module, Flutter, proto, and golangci-lint command to pass.

- [ ] **Step 4: Commit once with required trailer**

```text
feat: enforce explicit remote egress consent

Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>
```

- [ ] **Step 5: Push normally and open one PR into main**

Apply `turing-roadmap`, do not merge, and wait until GitHub reports `MERGEABLE`/`CLEAN` with all six visible CI jobs successful.

- [ ] **Step 6: Report evidence**

Send the coordinator the PR URL, commit SHA, consent/category model, endpoint/fallback rules, tests/docs, review rounds, full verification evidence, and live PR status.
