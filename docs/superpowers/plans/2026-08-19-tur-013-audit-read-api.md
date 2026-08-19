# TUR-013 Redacted Audit Read API Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose authenticated, bounded audit reads that let the local user inspect recorded decisions without returning credentials, approval capabilities, raw tool arguments, or deleted-session content.

**Architecture:** Add a public-only `AuditService.ListAuditEntries` gRPC contract backed by a keyset-paginated SQLite query ordered by `(created_at, rowid)`. The repository returns bounded raw payloads plus explicit storage state; the service projects those rows through an action-specific typed allowlist, so unknown fields and future actions are redacted by default while absent, present-but-redacted, and deletion-scrubbed payloads remain distinguishable.

**Tech Stack:** Protocol Buffers, generated Go/Dart gRPC, Go 1.23, SQLite, `database/sql`, Flutter/Dart models and networking, existing bearer-token interceptors.

---

## Source of truth and scope

- Approved roadmap design: `docs/architecture/2026-08-18-personal-agent-audit.md:349-355`.
- Existing audit writer: `turing-backend/orchestrator-go/internal/service/audit/service.go`.
- Existing audit storage: `turing-backend/orchestrator-go/internal/repository/audit.go`.
- Existing deletion tombstone: `turing-backend/orchestrator-go/internal/repository/session_delete.go:20-23`.
- Existing public authentication boundary: `turing-backend/orchestrator-go/internal/app/app.go:140-176`.
- This task does not add memory decisions, retry recording, or a large Flutter audit viewer. It makes every current row retrievable and gives future writers a safe, default-deny read boundary.

## Trust boundaries and cost bounds

- `ListAuditEntriesRequest` is untrusted client input. Validate exact filter lengths, timestamp validity/order, order enum, limit `1..100` (with `0` meaning the documented default of `50`), cursor size/shape/version, and cursor/filter fingerprint before any query.
- All filter values and cursor anchors reach SQLite only as bound parameters. The order direction is selected from a two-value server allowlist and is never interpolated from client text.
- `audit_logs.payload_json` is untrusted stored data. Read at most `limit + 1` rows and at most 16 KiB of payload JSON per row. Parse only JSON objects and copy only action-specific, typed, length/range-bounded fields into the response.
- Authentication comes from the existing public gRPC interceptor. Register no audit service on the internal runtime server.
- One request costs one indexed query, at most 101 rows, and at most about 1.6 MiB of payload text inspected. The extra row determines `next_cursor`; it is not returned.

## Target file structure

```text
proto/turing/v1/audit.proto
gen/turing/v1/go/turing/v1/audit.pb.go
gen/turing/v1/go/turing/v1/audit_grpc.pb.go
turing-client/turing_app/lib/generated/turing/v1/audit.pb.dart
turing-client/turing_app/lib/generated/turing/v1/audit.pbenum.dart
turing-client/turing_app/lib/generated/turing/v1/audit.pbgrpc.dart
turing-client/turing_app/lib/generated/turing/v1/audit.pbjson.dart
turing-backend/orchestrator-go/internal/db/schema/0012_audit_read.sql
turing-backend/orchestrator-go/internal/repository/audit.go
turing-backend/orchestrator-go/internal/repository/audit_test.go
turing-backend/orchestrator-go/internal/service/audit/service.go
turing-backend/orchestrator-go/internal/service/audit/service_test.go
turing-backend/orchestrator-go/internal/app/app.go
turing-backend/orchestrator-go/internal/app/app_test.go
turing-client/turing_app/lib/models/audit.dart
turing-client/turing_app/lib/models/grpc_mappers.dart
turing-client/turing_app/lib/networking/api_client.dart
turing-client/turing_app/lib/networking/grpc_client.dart
turing-client/turing_app/test/networking/grpc_client_test.dart
turing-client/turing_app/test/support/no_audit_api.dart
docs/architecture/audit-read-api.md
docs/architecture/tech-stack.md
docs/VISION.md
README.md
```

### Task 1: Lock the protobuf contract with failing tests

**Files:**
- Create: `proto/turing/v1/audit.proto`
- Regenerate: `gen/turing/v1/go/turing/v1/audit*.go`
- Regenerate: `turing-client/turing_app/lib/generated/turing/v1/audit*.dart`
- Test: `turing-backend/orchestrator-go/internal/service/audit/service_test.go`

- [x] **Step 1: Write a service test that references the contract before it exists**

Add a compile-time test fixture using:

```go
request := &turingv1.ListAuditEntriesRequest{
	CorrelationId: "run_1",
	Action:        "tool.call.before",
	Order:         turingv1.AuditOrder_AUDIT_ORDER_DESCENDING,
	Page:          &turingv1.PageRequest{Limit: 25},
}
_ = request
```

- [x] **Step 2: Run the focused test and verify the contract is missing**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/audit -run TestListAuditEntriesContract -count=1
```

Expected: compile failure because `ListAuditEntriesRequest` and `AuditOrder` do not exist.

- [x] **Step 3: Add the strict public contract**

Create `proto/turing/v1/audit.proto` with these public types:

```proto
syntax = "proto3";

package turing.v1;

option go_package = "github.com/mcasillas17/TuringAgent/gen/turing/v1/go;turingv1";

import "google/protobuf/timestamp.proto";
import "turing/v1/common.proto";

enum AuditOrder {
  AUDIT_ORDER_UNSPECIFIED = 0;
  AUDIT_ORDER_DESCENDING = 1;
  AUDIT_ORDER_ASCENDING = 2;
}

enum AuditPayloadState {
  AUDIT_PAYLOAD_STATE_UNSPECIFIED = 0;
  AUDIT_PAYLOAD_STATE_ABSENT = 1;
  AUDIT_PAYLOAD_STATE_PRESENT = 2;
  AUDIT_PAYLOAD_STATE_SCRUBBED = 3;
}

message AuditPayload {
  AuditPayloadState state = 1;
  optional string tool_name = 2;
  optional string server_name = 3;
  optional string phase = 4;
  optional string status = 5;
  optional string reason = 6;
  optional int64 duration_ms = 7;
  optional string error_code = 8;
  optional string provider = 9;
  optional string display_name = 10;
  optional bool unattended = 11;
  optional string automation_id = 12;
  optional string automation_name = 13;
  optional string method = 14;
  optional string request_id = 15;
  optional int64 deleted_runs = 16;
  optional int64 deleted_messages = 17;
}

message AuditEntry {
  string audit_id = 1;
  optional string correlation_id = 2;
  string actor_type = 3;
  optional string actor_id = 4;
  string action = 5;
  optional string target = 6;
  AuditPayload payload = 7;
  google.protobuf.Timestamp created_at = 8;
}

message ListAuditEntriesRequest {
  optional string correlation_id = 1;
  optional string action = 2;
  optional google.protobuf.Timestamp created_at_start = 3;
  optional google.protobuf.Timestamp created_at_end = 4;
  AuditOrder order = 5;
  PageRequest page = 6;
}

message ListAuditEntriesResponse {
  repeated AuditEntry entries = 1;
  PageResponse page = 2;
}

service AuditService {
  rpc ListAuditEntries(ListAuditEntriesRequest) returns (ListAuditEntriesResponse);
}
```

`created_at_start` is inclusive and `created_at_end` is exclusive. `AUDIT_ORDER_UNSPECIFIED` means descending.

- [x] **Step 4: Regenerate checked-in Go and Dart code**

Run:

```bash
tools/proto/generate.sh
```

Expected: generated `audit.pb.go`, `audit_grpc.pb.go`, and the four Dart audit files.

- [x] **Step 5: Run the proto determinism check**

Run:

```bash
tools/proto/check.sh
```

Expected: PASS with no generated diff.

### Task 2: Add bounded repository filtering and stable keyset pagination

**Files:**
- Create: `turing-backend/orchestrator-go/internal/db/schema/0012_audit_read.sql`
- Modify: `turing-backend/orchestrator-go/internal/repository/audit.go`
- Create: `turing-backend/orchestrator-go/internal/repository/audit_test.go`
- Test: `turing-backend/orchestrator-go/internal/db/migrations_test.go`

- [x] **Step 1: Write failing repository tests**

Cover all of these cases:

```go
func TestListAuditEntriesFiltersAndOrdersByCreatedAtThenRowID(t *testing.T)
func TestListAuditEntriesPaginatesWithoutDuplicatesWhenTimestampsMatch(t *testing.T)
func TestListAuditEntriesReadsOnlyBoundedPayloadJSON(t *testing.T)
func TestAuditReadMigrationAddsCreatedAtIndex(t *testing.T)
```

Seed rows with equal timestamps, different actions/correlations, an absent payload, the exact scrub tombstone, a normal payload, and a payload larger than 16 KiB. Assert ascending and descending order are exact reverses, time bounds are start-inclusive/end-exclusive, and `limit + 1` is the only over-fetch.

- [x] **Step 2: Run the repository tests and verify they fail**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository ./turing-backend/orchestrator-go/internal/db -run 'Test(ListAudit|AuditRead)' -count=1
```

Expected: compile/test failure because the query types and migration do not exist.

- [x] **Step 3: Add the query index**

Create:

```sql
CREATE INDEX IF NOT EXISTS idx_audit_created_at ON audit_logs(created_at);
```

SQLite indexes retain rowid as the deterministic tie-breaker, so `(created_at, rowid)` keyset reads remain indexed without rebuilding the table.

- [x] **Step 4: Add repository query types and implementation**

Add:

```go
const maxAuditPayloadReadBytes = 16 * 1024

type AuditOrder int

const (
	AuditOrderDescending AuditOrder = iota
	AuditOrderAscending
)

type AuditCursor struct {
	CreatedAt string
	RowID     int64
}

type AuditQuery struct {
	CorrelationID sql.NullString
	Action        sql.NullString
	CreatedAtStart sql.NullString
	CreatedAtEnd   sql.NullString
	Order          AuditOrder
	After          *AuditCursor
	Limit          int
}

type AuditRecord struct {
	RowID            int64
	AuditID          string
	CorrelationID    sql.NullString
	ActorType        string
	ActorID          sql.NullString
	Action           string
	Target           sql.NullString
	PayloadPresent   bool
	PayloadScrubbed  bool
	PayloadJSON      sql.NullString
	CreatedAt        string
}
```

Build SQL only from fixed fragments. Append every client value to the bound-argument slice. Select:

```sql
payload_json IS NOT NULL,
payload_json = '{"scrubbed":true}',
CASE WHEN length(payload_json) <= 16384 THEN payload_json END
```

Order by `created_at, rowid` in the allowlisted direction and query `Limit + 1`. Return the extra record to the service only as evidence that another page exists.

- [x] **Step 5: Run focused repository and migration tests**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository ./turing-backend/orchestrator-go/internal/db -run 'Test(ListAudit|AuditRead)' -count=1
```

Expected: PASS.

### Task 3: Implement cursor validation and default-deny payload projection

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/service/audit/service.go`
- Modify: `turing-backend/orchestrator-go/internal/service/audit/service_test.go`

- [x] **Step 1: Write failing service tests for validation, redaction, and pagination**

Add table-driven coverage for:

```go
func TestListAuditEntriesReturnsEveryCurrentActionUnderExplicitPolicy(t *testing.T)
func TestListAuditEntriesDistinguishesAbsentPresentAndScrubbedPayloads(t *testing.T)
func TestListAuditEntriesNeverReturnsSecretsOrUnboundedToolContent(t *testing.T)
func TestListAuditEntriesUsesStableFilterBoundCursors(t *testing.T)
func TestListAuditEntriesRejectsInvalidLimitsCursorsFiltersAndTimes(t *testing.T)
func TestUnknownAuditActionReturnsMetadataAndPresentStateWithoutPayloadFields(t *testing.T)
```

The secret fixture must place distinct sentinel values under `args`, `resultSummary`, nested error `message`, `approvalToken`, `approvalJti`, `jti`, `authorization`, `apiKey`, `credential`, `password`, and an unknown field. Marshal the protobuf response and assert no sentinel occurs.

- [x] **Step 2: Run the service tests and verify they fail**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/audit -count=1
```

Expected: failures because `ListAuditEntries`, cursor handling, and projection do not exist.

- [x] **Step 3: Implement request validation and opaque cursors**

Use defaults and limits:

```go
const (
	defaultAuditPageLimit = 50
	maxAuditPageLimit     = 100
	maxAuditCursorBytes   = 2048
	maxAuditCorrelationID = 256
	maxAuditAction        = 128
)
```

The cursor is an opaque token, not a documented or supported public format —
its JSON shape is an implementation detail a caller must not parse or
construct. The raw-URL-base64 body encodes version `1`, anchor `created_at`,
anchor `rowid`, a SHA-256 fingerprint of the normalized filters and order, and
a `mac` field: an HMAC-SHA256 digest over the version, anchor, and fingerprint,
keyed by a server-side secret. The key is derived, not random-per-request:
when a secret is configured (`app.go` passes `cfg.ApprovalJWTSecret`, the
server-side `TURING_APPROVAL_JWT_SECRET` signing secret shared with
components that verify approval capabilities, including `mcp-files`, and never
the public client bearer key), the key is `SHA-256("turing.audit.cursor.v1" +
secret)` — a separate, domain-separated key, not a reuse of the approval token
key — so a cursor minted before a restart still verifies after one as long as
the approval secret is unchanged. A public bearer holder cannot derive or forge
the MAC from the client API key; rotating the client bearer key alone leaves
cursors valid, while changing to a different approval secret invalidates them.
Identical approval secrets yield the same derived cursor key, so cursors
survive restarts or installs that keep the secret unchanged. With no secret
configured, each `audit.New` call without a supplied secret draws a fresh
random key instead, not one per process, so cursors remain unforgeable but do
not survive a restart; that fallback is for direct constructor use in isolated
tests and `Record`-only callers, including some production audit instances, but
the publicly registered app audit service always receives the required approval
secret so public cursor validation persists across restart. Decode with
`json.Decoder.DisallowUnknownFields`, require EOF, validate every field,
recompute the MAC and reject (gRPC `InvalidArgument`, no cursor/filter values
echoed) unless it matches the decoded body under a constant-time comparison —
this is what rejects a tampered anchor, not just a malformed one. A cursor
used with different filters or order is separately invalid because the
embedded fingerprint must also constant-time-match the current request's
filters.

- [x] **Step 4: Implement typed action policies**

Start every stored payload as `AUDIT_PAYLOAD_STATE_PRESENT`; use `ABSENT` only for SQL `NULL` and `SCRUBBED` only for the exact repository tombstone. Parse normal payloads with `UseNumber` and copy only:

| Action | Allowed response fields |
| --- | --- |
| `tool.call.before`, `tool.call.after` | `tool_name`, `server_name`, `phase`, `status`, `reason`, `duration_ms`, nested `error.code` |
| `approval.requested`, `approval.expired`, `approval.consumed` | `tool_name`, `unattended`, `automation_id`, `automation_name` |
| `approval.approved` | the above, plus `decision_comment` and `decision_comment_truncated` |
| `approval.denied` | the above common fields, plus `denial_reason` and `denial_reason_truncated` |
| `automation.tool.blocked` | `tool_name`, `server_name`, `automation_id`, `automation_name` |
| `integration.connected`, `integration.revoked`, `integration.deleted` | `provider`, `display_name` |
| `auth.failed` | `method`, `request_id` |
| `session.deleted` | non-negative `deleted_runs`, `deleted_messages` mapped from stored `runs`, `messages` |
| every other action, including future `memory.*` actions until their writer adds a reviewed rule | no payload fields; state remains `PRESENT` |

String readers require the exact JSON string type and action-specific length bounds. Numeric readers require exact integral JSON numbers in `int64` range and reject negative durations/counts. Do not truncate an invalid value into acceptance.

The two approval rationale fields carry human free text (TUR-002), so they use a dedicated reader rather than the machine-label string reader: an explicit empty value stays present (empty means "the person typed nothing", absent means "no human field was recorded"), newline/carriage return/tab are preserved while every other Unicode control character omits the value, and the 512-byte bound is re-checked but never applied by truncating — the writer already truncated and flagged. See [Audit read API](../../architecture/audit-read-api.md#approval-decision-rationale).

- [x] **Step 5: Map repository failures without leaking row contents**

Malformed stored JSON and oversized payloads produce a metadata-only `PRESENT` payload, not an RPC failure and not a raw fallback. Database failures return `codes.Internal` with `"list audit entries failed"`.

- [x] **Step 6: Run focused service tests**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/audit -count=1
```

Expected: PASS.

### Task 4: Register the authenticated public-only service and prove deletion semantics end to end

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/app/app.go`
- Modify: `turing-backend/orchestrator-go/internal/app/app_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/session_delete_test.go`

- [x] **Step 1: Write failing app tests**

Add:

```go
func TestAuditServiceIsAuthenticatedAndPublicOnly(t *testing.T)
func TestDeletedSessionAuditIsListableOnlyAsScrubbedEvidence(t *testing.T)
```

The first test calls `AuditService.ListAuditEntries` without metadata and expects `Unauthenticated`, succeeds with the client token, and asserts `turing.v1.AuditService` is absent from `InternalServer.GetServiceInfo()`. The second creates and completes a session run, records tool content containing a sentinel, deletes the session, lists by the run correlation ID, and asserts the row survives with `SCRUBBED` state and no sentinel.

- [x] **Step 2: Run app and deletion tests and verify they fail**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/app ./turing-backend/orchestrator-go/internal/repository -run 'Test(AuditService|DeletedSessionAudit)' -count=1
```

Expected: missing registration and/or missing public method failure.

- [x] **Step 3: Register `AuditService` only on `publicServer`**

Add:

```go
turingv1.RegisterAuditServiceServer(publicServer, auditService)
```

Do not register it on `internalServer`. The existing unary interceptor enforces the configured client API key on every call.

- [x] **Step 4: Run focused app and deletion tests**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/app ./turing-backend/orchestrator-go/internal/repository -run 'Test(AuditService|DeletedSessionAudit)' -count=1
```

Expected: PASS.

### Task 5: Add the thin Flutter access surface

**Files:**
- Create: `turing-client/turing_app/lib/models/audit.dart`
- Modify: `turing-client/turing_app/lib/models/grpc_mappers.dart`
- Modify: `turing-client/turing_app/lib/networking/api_client.dart`
- Modify: `turing-client/turing_app/lib/networking/grpc_client.dart`
- Create: `turing-client/turing_app/test/support/no_audit_api.dart`
- Modify: Flutter test fakes that implement `TuringApi`
- Modify: `turing-client/turing_app/test/networking/grpc_client_test.dart`

- [x] **Step 1: Write a failing gRPC client test**

Use a capturing `AuditServiceBase` and assert:

```dart
final page = await api.listAuditEntries(
  correlationId: 'run_1',
  action: 'tool.call.before',
  createdAtStart: DateTime.utc(2026, 8, 18),
  createdAtEnd: DateTime.utc(2026, 8, 19),
  order: AuditOrder.ascending,
  limit: 25,
  cursor: 'cursor-1',
);
```

The request must preserve all filters, use a bounded unary deadline, map optional payload fields without inventing values, and return `nextCursor`.

- [x] **Step 2: Run the focused Flutter test and verify it fails**

Run:

```bash
( cd turing-client/turing_app && flutter test test/networking/grpc_client_test.dart )
```

Expected: compile failure because the audit model and client method do not exist.

- [x] **Step 3: Add pure Dart audit models**

Create immutable `AuditPage`, `AuditEntry`, `AuditPayload`, `AuditOrder`, and `AuditPayloadState`. Every optional protobuf field maps to a nullable Dart field; absent values must not become empty strings or zeroes.

- [x] **Step 4: Add `TuringApi.listAuditEntries` and the gRPC implementation**

The method accepts optional exact correlation/action filters, optional UTC times, order, limit, and cursor. Construct `auditpb.ListAuditEntriesRequest`, call `AuditServiceClient.listAuditEntries` with `_startupUnaryTimeout`, map through `GrpcMappers`, and return the server cursor.

- [x] **Step 5: Keep unrelated test fakes explicit**

Add `NoAuditApi`, returning:

```dart
Future<AuditPage> listAuditEntries({
  String? correlationId,
  String? action,
  DateTime? createdAtStart,
  DateTime? createdAtEnd,
  AuditOrder order = AuditOrder.descending,
  int limit = 50,
  String? cursor,
}) async => const AuditPage(entries: [], nextCursor: null);
```

Mix it into existing fakes that are not testing audit. Do not add a navigation destination or audit page in TUR-013.

- [x] **Step 6: Run focused Flutter model/network tests**

Run:

```bash
( cd turing-client/turing_app && flutter test test/networking/grpc_client_test.dart )
```

Expected: PASS.

### Task 6: Document the privacy contract and current-state change

**Files:**
- Create: `docs/architecture/audit-read-api.md`
- Modify: `docs/architecture/tech-stack.md`
- Modify: `docs/VISION.md`
- Modify: `README.md`
- Modify: `docs/architecture/2026-08-18-personal-agent-audit.md`

- [x] **Step 1: Write the architecture document**

Document:

- public-only bearer authentication;
- exact filters, inclusive/exclusive time bounds, default/max limits, stable cursor and order semantics;
- required structural metadata (always returned, `[redacted]` when unsafe) versus optional structural metadata (returned only when stored and structurally safe, otherwise omitted) and their bounds, plus the action-scoped omissions layered on top of structural safety: `target` is never returned for any `approval.*` action because it is the approval JWT `jti`, and `actor_id` is never returned for `auth.failed` because it is the caller's peer address. The `approval.*` `target` omission cannot be reopened by metadata bounding: the repository carries a bounded approval-prefix disclosure bit (`AuditRecord.ActionHasApprovalPrefix`), derived in the same SQL query from only the first 9 bytes of the original action (`substr(CAST(action AS BLOB), 1, 9) = CAST('approval.' AS BLOB)`, NULL-safe, fixed literals), so an oversized `approval.*` action that collapses the bounded `action` column to `""` still drops its JTI target; the service also checks the raw mapped action's prefix directly as defense for records built without the repository;
- the action-to-field allowlist table from Task 3;
- absent vs present-but-redacted vs scrubbed payload states;
- the 16 KiB stored-payload inspection cap and `limit + 1` query bound;
- why approval tokens, JTIs, auth headers, API keys, credentials, tool args, result summaries, and error messages can never enter the response;
- why future actions are metadata-only until their typed rule is reviewed;
- why deletion keeps shape but replaces withdrawn content with the exact scrub tombstone.

- [x] **Step 2: Update current-state docs**

Change `docs/VISION.md` from “audit is not inspectable” to the exact shipped limitation: audit is inspectable through a redacted public API but has no large viewer. Add the audit read API to `docs/architecture/tech-stack.md` and README architecture links. Mark TUR-013 implemented in the roadmap without changing other task scope.

- [x] **Step 3: Run formatting and focused suites**

Run:

```bash
gofmt -w turing-backend/orchestrator-go/internal/repository/audit.go \
  turing-backend/orchestrator-go/internal/repository/audit_test.go \
  turing-backend/orchestrator-go/internal/service/audit/service.go \
  turing-backend/orchestrator-go/internal/service/audit/service_test.go \
  turing-backend/orchestrator-go/internal/app/app.go \
  turing-backend/orchestrator-go/internal/app/app_test.go
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository \
  ./turing-backend/orchestrator-go/internal/service/audit \
  ./turing-backend/orchestrator-go/internal/app -count=1
( cd turing-client/turing_app && dart format lib/models/audit.dart lib/models/grpc_mappers.dart lib/networking/api_client.dart lib/networking/grpc_client.dart test/networking/grpc_client_test.dart test/support/no_audit_api.dart )
( cd turing-client/turing_app && flutter analyze && flutter test )
tools/proto/check.sh
```

Expected: all commands PASS.

### Task 7: Run mandatory review loops, full verification, and PR delivery

**Files:**
- Review: every changed source, generated, test, plan, and architecture file

- [x] **Step 1: Fetch and merge current `origin/main`, then rerun focused tests**

Fetch `origin/main` and perform a normal (non-rebase) merge of it into the current feature branch. `origin/main` has moved since this plan was written and now includes TUR-002 (`#56`, approval decision rationale) and TUR-007 (`#55`, stable session titles from first turns) — both landed independently of this task. Resolve any conflicts by preserving both sides: this task's audit read API changes and the incoming TUR-002/TUR-007 changes must both remain intact, not one dropped in favor of the other. After the merge, rerun the focused test suites for any package touched by the merge (at minimum the packages this task already changed, plus any package the merge itself modified) to confirm the merged tree still passes before proceeding to the full reviews below.

- [x] **Step 2: Run the two independent full-diff reviews in parallel**

Dispatch:

1. Claude Opus 5, `xhigh`, `long_context`: spec coverage, architecture, privacy, protobuf compatibility, deletion semantics, docs, and tests.
2. GPT-5.6 Luna, `xhigh`, `long_context`: correctness, stable pagination, authentication, redaction, edge cases, regressions, docs, and test coverage.

Both reviewers must inspect the complete diff against `main`, not a summary.

- [x] **Step 3: Fix every valid finding with TDD and rerun focused tests**

For each finding, first add or strengthen a test that fails on the reviewed defect, then implement the fix and rerun the smallest affected suite. Record rejected findings with a concrete technical reason.

- [x] **Step 4: Repeat both full-diff reviews**

Rerun the same two reviewers in parallel after fixes. Repeat Steps 3-4 until both explicitly say there is no remaining feedback.

- [x] **Step 5: Run the repository-required Opus 4.8 full-diff review**

Use Claude Opus 4.8 over the complete diff and request correctness, edge cases, intent gaps, reuse/simplification/naming improvements, and explicit unit-test coverage gaps. Resolve every valid finding and rerun affected tests.

- [x] **Step 6: Run the complete repository verification matrix**

Invoke `/verify`, which must run root tagged tests/race/build, both standalone MCP modules, Flutter analyze/tests, proto determinism, and all three golangci-lint invocations.

Expected: every command PASS after the latest change.

- [x] **Step 7: Run the final security and naming sweeps**

Trace:

```text
ListAuditEntriesRequest -> service validation -> bound repository query
audit_logs.payload_json -> bounded read -> typed action policy -> protobuf/Dart model
public metadata token -> existing gRPC interceptor -> AuditService authorization
```

Search the diff and touched files for `token`, `jti`, `authorization`, `api[_-]?key`, `credential`, `password`, `secret`, and the sentinel fixtures. Confirm occurrences are policy/tests/documentation only and no response assignment can carry their values. Check every introduced name against its body and correct mismatches.

- [ ] **Step 8: Commit, push, and open one PR into `main`**

Commit with the configured co-author trailer, push the current feature branch, open one non-draft PR into `main`, and add the `turing-roadmap` label. Do not merge.

- [ ] **Step 9: Re-check live mergeability and CI**

Inspect the live PR merge state and all checks. If conflicts or CI failures require a code change, fix with TDD, rerun both full-diff reviewers until clean, rerun Opus 4.8 review, rerun `/verify`, push, and re-check.

- [ ] **Step 10: Message the creator**

Send the creator the PR URL, final commit SHA, `/verify` result, documentation changed, number of dual-review rounds plus Opus 4.8 result, and that TUR-021, MEM-006, MEM-012, and TUR-017 are unblocked. Do not claim any dependency was implemented.

## Self-review

- **Spec coverage:** authenticated API (Tasks 1, 4); correlation/action/time/order filters (Tasks 1-3); stable bounded cursor pagination (Tasks 2-3); explicit typed default-deny redaction and secret exclusion (Task 3); every current row action retrievable (Task 3); deletion rows listable and visibly scrubbed (Task 4); absent/present/scrubbed distinction (Tasks 1, 3); predictable invalid arguments (Task 3); thin Flutter contract without a large UI (Task 5); directly related docs (Task 6); required reviews, verification, PR, label, CI, creator handoff (Task 7).
- **Placeholder scan:** no deferred implementation markers or unspecified “handle errors/tests” steps remain.
- **Type consistency:** protobuf `AuditOrder`, `AuditPayloadState`, and `AuditEntry` map one-for-one to service and Dart names; protobuf `PageResponse` (the shared paging message, `proto/turing/v1/common.proto`) maps to the Dart `AuditPage` model; cursor anchors are `(created_at, rowid)` everywhere; deletion counts map from stored `runs/messages` to public `deleted_runs/deleted_messages`.
