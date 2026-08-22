# MCP Registry UX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users register, re-import, and rotate credentials for MCP servers from the Flutter app without restarting the backend, while preserving registry intent and never disclosing bearer tokens.

**Architecture:** Keep `mcpregistry.Server` as the only validation and sealing boundary. File import and direct registration share one normalized server-definition validator, while repository transactions distinguish create-only import, explicit tombstone-clearing registration, and token replacement. Public gRPC handlers expose token-free descriptors/reports; Flutter adds compact forms and dialogs over those RPCs.

**Tech Stack:** Go 1.23, SQLite, gRPC/protobuf, `internal/secretbox`, Flutter/Dart, existing audit service, Go `testing`, Flutter widget tests.

---

## File map

- `proto/turing/v1/mcp.proto`: public request/response contracts for registration, re-import, and rotation.
- `gen/turing/v1/go/turing/v1/*mcp*`: pinned generated Go protobuf and gRPC code.
- `turing-client/turing_app/lib/generated/turing/v1/mcp.*.dart`: pinned generated Dart protobuf and gRPC code.
- `turing-backend/orchestrator-go/internal/repository/mcp_servers.go`: create-only import, explicit registration, and sealed-token replacement transactions.
- `turing-backend/orchestrator-go/internal/service/mcpregistry/import.go`: shared name, URL/tier, bearer, sealing, and file-import validation.
- `turing-backend/orchestrator-go/internal/service/mcpregistry/service.go`: public RPC handlers, import-file runner, fixed gRPC errors, registry notifications, and token-free audit records.
- `turing-backend/orchestrator-go/internal/app/app.go`: configure the registry with `MCPConfigRoot` and the audit recorder, then invoke the same import-file runner at startup.
- `turing-backend/orchestrator-go/internal/service/mcpregistry/*_test.go`: discriminating repository/service/security tests.
- `turing-backend/orchestrator-go/internal/app/*mcp*_test.go`: startup/on-demand parity and public/internal authorization tests.
- `turing-client/turing_app/lib/models/mcp_server.dart`: immutable import-report model beside the existing registry snapshot.
- `turing-client/turing_app/lib/networking/api_client.dart`: abstract management methods with unsupported defaults for lightweight test clients.
- `turing-client/turing_app/lib/networking/grpc_client.dart`: protobuf request construction and token-free response mapping.
- `turing-client/turing_app/lib/features/workspace/workspace_pages.dart`: compact registration, re-import, and token-rotation controls.
- `turing-client/turing_app/test/ui/mcps_test.dart`: focused desktop/mobile widget behavior.
- `turing-client/turing_app/test/support/no_mcp_registry_api.dart`: default test mixin implementations for the new API methods.
- `docs/mcp-security-and-integration.md`: in-app and file import behavior, no-restart workflow, create-only re-import semantics, and credential rotation.

### Task 1: Lock and generate the gRPC contract

**Files:**
- Modify: `proto/turing/v1/mcp.proto`
- Modify: `gen/turing/v1/go/turing/v1/mcp.pb.go`
- Modify: `gen/turing/v1/go/turing/v1/mcp_grpc.pb.go`
- Modify: `turing-client/turing_app/lib/generated/turing/v1/mcp.pb.dart`
- Modify: `turing-client/turing_app/lib/generated/turing/v1/mcp.pbgrpc.dart`
- Modify: `turing-client/turing_app/lib/generated/turing/v1/mcp.pbjson.dart`
- Test: `turing-backend/orchestrator-go/internal/service/mcpregistry/service_test.go`
- Test: `turing-client/turing_app/test/generated/protobuf_contract_test.dart`

- [ ] **Step 1: Write compile-time contract tests**

Add Go and Dart tests that instantiate the exact request/response fields:

```go
register := &turingv1.RegisterMcpServerRequest{
	Name: "vendor", Url: "https://vendor.example/mcp",
	Tier: turingv1.McpServerTier_MCP_SERVER_TIER_REMOTE_URL,
	BearerToken: "write-only",
}
rotate := &turingv1.RotateMcpServerTokenRequest{
	ServerId: "mcp_vendor", BearerToken: "",
}
report := &turingv1.ReimportMcpJsonResponse{
	Imported: []string{"new"},
	Skipped: []string{"existing"},
	Refused: []*turingv1.UnsupportedMcpServer{{Name: "bad", Reason: "refused"}},
}
```

```dart
final request = RegisterMcpServerRequest(
  name: 'vendor',
  url: 'https://vendor.example/mcp',
  tier: McpServerTier.MCP_SERVER_TIER_REMOTE_URL,
  bearerToken: 'write-only',
);
expect(request.bearerToken, 'write-only');
expect(RotateMcpServerTokenRequest(serverId: 'mcp_vendor').serverId, 'mcp_vendor');
expect(ReimportMcpJsonResponse(imported: ['new']).imported, ['new']);
```

- [ ] **Step 2: Run the contract tests and verify they fail to compile**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/mcpregistry -run TestMcpManagementProtoContract -count=1
( cd turing-client/turing_app && flutter test test/generated/protobuf_contract_test.dart )
```

Expected: missing protobuf messages/RPC methods.

- [ ] **Step 3: Add token-free response contracts**

Add these messages and public methods:

```proto
message RegisterMcpServerRequest {
  string name = 1;
  string url = 2;
  McpServerTier tier = 3;
  string bearer_token = 4;
}

message ReimportMcpJsonRequest {}

message ReimportMcpJsonResponse {
  repeated string imported = 1;
  repeated string skipped = 2;
  repeated UnsupportedMcpServer refused = 3;
}

message RotateMcpServerTokenRequest {
  string server_id = 1;
  string bearer_token = 2;
}
```

Return `McpServerDescriptor` from register and rotate. Neither response type has a token or ciphertext field.

- [ ] **Step 4: Regenerate with the pinned toolchain**

Run:

```bash
tools/proto/generate.sh
tools/proto/check.sh
```

Expected: generated Go and Dart files change, and the check exits zero.

- [ ] **Step 5: Run both contract tests**

Run the Step 2 commands. Expected: PASS.

### Task 2: Make import and registration preserve user intent

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/repository/mcp_servers.go`
- Test: `turing-backend/orchestrator-go/internal/service/mcpregistry/import_test.go`
- Test: `turing-backend/orchestrator-go/internal/service/mcpregistry/rediscovery_policy_test.go`
- Test: `turing-backend/orchestrator-go/internal/service/mcpregistry/delete_test.go`

- [ ] **Step 1: Write failing intent-preservation tests**

Cover these transitions:

```go
func TestReimportSkipsExistingDisabledServer(t *testing.T)
func TestReimportPreservesEditedToolPolicy(t *testing.T)
func TestReimportPreservesRotatedToken(t *testing.T)
func TestReimportRefusesTombstonedServer(t *testing.T)
func TestExplicitRegistrationClearsTombstone(t *testing.T)
func TestExplicitRegistrationRefusesExistingName(t *testing.T)
func TestExplicitRegistrationRefusesBundledName(t *testing.T)
```

For the policy test, import a tool, set its policy to `disabled`, re-import a file that declares it `approval_required` by omission, and assert the stored policy remains `disabled`. For the token test, open the sealed value after rotation and after re-import and assert it remains the rotated value.

- [ ] **Step 2: Run the focused tests and verify the current upsert behavior fails**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/mcpregistry \
  -run 'Test(Reimport|ExplicitRegistration)' -count=1
```

Expected: re-import overwrites at least one existing field and explicit tombstone clearing is unavailable.

- [ ] **Step 3: Add repository operations with explicit outcomes**

Use named results and errors:

```go
var ErrMCPServerNameTaken = errors.New("MCP server name is already registered")

type MCPImportResult struct {
	Server  MCPServerRecord
	Created bool
}

func (r *Repository) ImportMCPServer(
	ctx context.Context,
	input ImportedMCPServer,
) (MCPImportResult, error)

func (r *Repository) RegisterMCPServer(
	ctx context.Context,
	input ImportedMCPServer,
) (MCPServerRecord, error)

func (r *Repository) ReplaceMCPServerToken(
	ctx context.Context,
	serverID string,
	sealedToken []byte,
) (MCPServerRecord, error)
```

`ImportMCPServer` checks tombstones first, returns `Created:false` without updating an existing non-bundled row, and inserts new rows disabled. `RegisterMCPServer` refuses existing rows, deletes a matching tombstone and inserts the new disabled row in one transaction. `ReplaceMCPServerToken` refuses bundled rows and stores SQL `NULL` for an empty token.

- [ ] **Step 4: Route file import through create-only import**

Populate:

```go
type ImportReport struct {
	Imported    []string
	Skipped     []string
	Unsupported map[string]string
}
```

Append new names to `Imported`, existing names to `Skipped`, and tombstoned/bundled/invalid names to `Unsupported`. Sort all report names before returning so UI and tests are deterministic.

- [ ] **Step 5: Run registry tests**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/mcpregistry -count=1
```

Expected: PASS.

### Task 3: Add shared validation, management RPCs, and token-free auditing

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/service/mcpregistry/import.go`
- Modify: `turing-backend/orchestrator-go/internal/service/mcpregistry/service.go`
- Test: `turing-backend/orchestrator-go/internal/service/mcpregistry/management_test.go`
- Test: `turing-backend/orchestrator-go/internal/service/mcpregistry/token_security_test.go`

- [ ] **Step 1: Write failing RPC behavior tests**

Test:

```go
func TestRegisterMcpServerUsesImportValidationAndStartsDisabled(t *testing.T)
func TestRegisterMcpServerRefusesTierURLMismatch(t *testing.T)
func TestRegisterMcpServerRefusesStdioShapedURL(t *testing.T)
func TestRegisterMcpServerRefusesBundledName(t *testing.T)
func TestRotateMcpServerTokenReplacesAndClearsSealedToken(t *testing.T)
func TestRotateMcpServerTokenRefusesBundledServer(t *testing.T)
func TestManagementResponsesAndAuditNeverContainToken(t *testing.T)
```

The security test must call both registration and rotation with a unique sentinel, marshal every response with `protojson.Marshal`, query every `audit_logs.payload_json`, and fail if either contains the sentinel or a substantial substring. It must also assert audit rows exist so the audit check is not vacuous.

- [ ] **Step 2: Run focused tests and verify they fail**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/mcpregistry \
  -run 'Test(RegisterMcpServer|RotateMcpServerToken|ManagementResponses)' -count=1
```

Expected: missing handlers and audit records.

- [ ] **Step 3: Extract one normalized validation path**

Both file import and registration call:

```go
type validatedMCPServer struct {
	Name  string
	URL   string
	Tier  repository.MCPServerTier
	Token string
}

func (s *Server) validateServer(
	name string,
	rawURL string,
	requestedTier repository.MCPServerTier,
	token string,
) (validatedMCPServer, error)

func (s *Server) sealToken(name, token string) ([]byte, error)
```

The validator applies `mcpServerNamePattern`, bundled-name refusal, `classifyImportedURL`, requested-tier equality when supplied, and `bearerFromHeaders`-equivalent control-character checks. `sealToken` maps `secretbox.ErrNoKey` to one fixed message and never formats the token or sealing error.

- [ ] **Step 4: Implement public-only RPC handlers**

`PublicServer` delegates all three methods. `InternalServer` returns `PermissionDenied` with “MCP server management is public.” The service methods:

```go
func (s *Server) RegisterMcpServer(
	ctx context.Context,
	req *turingv1.RegisterMcpServerRequest,
) (*turingv1.McpServerDescriptor, error)

func (s *Server) ReimportMcpJson(
	ctx context.Context,
	req *turingv1.ReimportMcpJsonRequest,
) (*turingv1.ReimportMcpJsonResponse, error)

func (s *Server) RotateMcpServerToken(
	ctx context.Context,
	req *turingv1.RotateMcpServerTokenRequest,
) (*turingv1.McpServerDescriptor, error)
```

Registration infers/canonicalizes the URL, verifies the requested non-bundled tier, seals by server name, inserts disabled, returns a field-by-field descriptor, audits only name/tier, notifies the runtime, and does not contact the endpoint. Rotation reads the server first, rejects bundled, seals or clears, updates, audits only server name plus `tokenConfigured: bool`, and returns a descriptor.

- [ ] **Step 5: Add narrow audit and import-source dependencies**

Add:

```go
type AuditRecorder interface {
	Record(context.Context, string, string, string, string, string, map[string]any) error
}

func (s *Server) SetAuditRecorder(audit AuditRecorder)
func (s *Server) SetMCPConfigRoot(root string)
```

Audit actions are `mcp.server.registered`, `mcp.server.token_rotated`, and `mcp.server.token_cleared`. Logging identifies only action and server ID.

> **Superseded by the 2026-08-21 discovery follow-up (below):** the shipped audit surface also records `mcp.server.enabled`, `mcp.server.disabled`, and `mcp.server.reimported`, added when enable-time discovery landed after this task was first executed.

- [ ] **Step 6: Run the registry package tests**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/mcpregistry -count=1
```

Expected: PASS.

### Task 4: Reuse the startup importer on demand

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/app/app.go`
- Test: `turing-backend/orchestrator-go/internal/app/mcp_import_test.go`
- Test: `turing-backend/orchestrator-go/internal/app/mcp_delete_restart_test.go`
- Test: `turing-backend/orchestrator-go/internal/app/app_test.go`

- [ ] **Step 1: Write failing shared-path tests**

Add:

```go
func TestReimportMcpJsonReadsConfiguredFileWithoutRestart(t *testing.T)
func TestReimportMcpJsonReturnsImportedSkippedAndRefused(t *testing.T)
func TestReimportMcpJsonPreservesDisabledServerAndPolicy(t *testing.T)
func TestDeletedMCPServerStaysDeletedThroughOnDemandReimport(t *testing.T)
func TestPublicCanManageMCPRegistryAndInternalCannot(t *testing.T)
```

The first test starts the app without `mcp.json`, writes the file after startup, calls the RPC, and observes the new disabled server without constructing a second app.

- [ ] **Step 2: Run focused app tests and verify they fail**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/app \
  -run 'Test(ReimportMcpJson|DeletedMCPServerStaysDeletedThrough|PublicCanManageMCP)' -count=1
```

Expected: no configured on-demand importer.

- [ ] **Step 3: Move file reading behind the registry service**

Implement:

```go
func (s *Server) ReimportConfiguredJSON(ctx context.Context) (ImportReport, error)
```

It reads `<MCPConfigRoot>/mcp.json`, clears issues and returns an empty report when the file is absent, bounds document-level diagnostics under `_document`, and returns fixed errors without file contents. `app.New` sets the root and audit recorder before invoking this method once for startup import.

- [ ] **Step 4: Preserve the internal identity allowlist**

Do not add management methods to `runtime` or `approval-consumer`. Keep only `ListMcpServers` and `CallRegisteredMcpTool` for runtime. Extend authorization tests to prove public success and internal `PermissionDenied`.

- [ ] **Step 5: Run app and third-party identity regression tests**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/app -count=1
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/mcpregistry \
  -run TestThirdPartyServerNeverReceivesTheApprovalConsumerIdentity -count=1
```

Expected: PASS.

### Task 5: Expose the RPCs through the Dart API

**Files:**
- Modify: `turing-client/turing_app/lib/models/mcp_server.dart`
- Modify: `turing-client/turing_app/lib/networking/api_client.dart`
- Modify: `turing-client/turing_app/lib/networking/grpc_client.dart`
- Modify: `turing-client/turing_app/test/support/no_mcp_registry_api.dart`
- Test: `turing-client/turing_app/test/networking/grpc_client_test.dart`

- [ ] **Step 1: Write failing mapping tests**

Capture outgoing protobuf requests with the existing fake gRPC server and assert:

```dart
expect(registerRequest.name, 'vendor');
expect(registerRequest.tier, McpServerTier.MCP_SERVER_TIER_REMOTE_URL);
expect(registerRequest.bearerToken, sentinel);
expect(rotateRequest.bearerToken, '');
expect(report.refused.single.reason, contains('stdio'));
```

Also marshal returned Dart models to strings and assert the sentinel is absent.

- [ ] **Step 2: Run the networking test and verify it fails**

Run:

```bash
( cd turing-client/turing_app && flutter test test/networking/grpc_client_test.dart )
```

Expected: missing Dart API methods.

- [ ] **Step 3: Add immutable API models and methods**

Add:

```dart
class McpImportReport {
  McpImportReport({
    required List<String> imported,
    required List<String> skipped,
    required List<UnsupportedMcpServer> refused,
  }) : imported = List.unmodifiable(imported),
       skipped = List.unmodifiable(skipped),
       refused = List.unmodifiable(refused);

  final List<String> imported;
  final List<String> skipped;
  final List<UnsupportedMcpServer> refused;
}
```

Add `registerMcpServer`, `reimportMcpJson`, and `rotateMcpServerToken` to `TuringApi`, with unsupported defaults. Override them in `GrpcApiClient`, mapping only token-free response fields.

- [ ] **Step 4: Run the networking and generated contract tests**

Run:

```bash
( cd turing-client/turing_app && flutter test test/networking/grpc_client_test.dart test/generated/protobuf_contract_test.dart )
```

Expected: PASS.

### Task 6: Build the compact MCP management UX

**Files:**
- Modify: `turing-client/turing_app/lib/features/workspace/workspace_pages.dart`
- Create: `turing-client/turing_app/test/ui/mcps_test.dart`
- Modify: `turing-client/turing_app/test/ui/shell_navigation_test.dart`

- [ ] **Step 1: Write failing widget tests**

Cover:

```dart
testWidgets('registers a remote server with a write-only token', ...)
testWidgets('add server form fits a compact phone', ...)
testWidgets('re-import shows imported skipped and refused entries', ...)
testWidgets('rotates and clears a non-bundled token without redisplaying it', ...)
testWidgets('bundled servers have no rotate action', ...)
testWidgets('empty state says app registration needs no restart', ...)
```

The fake API records the submitted token but never places it in a returned model. After submission and after reopening the rotate dialog, assert `find.text(sentinel)` finds nothing.

- [ ] **Step 2: Run the focused widget tests and verify they fail**

Run:

```bash
( cd turing-client/turing_app && flutter test test/ui/mcps_test.dart )
```

Expected: missing controls.

- [ ] **Step 3: Add the registration form**

Use `TextFormField` controls for name/URL/token, a dropdown limited to local-container and remote-URL tiers, `obscureText: true` for the token, and a `Wrap`/responsive column so 300–390 logical pixel widths do not overflow. Clear all controllers after success and always dispose them.

- [ ] **Step 4: Add re-import and rotation actions**

The re-import button calls the API, reloads the registry, and shows a dialog with separate Imported, Skipped, and Refused sections including reasons. Each non-bundled server card has a “Rotate token” action opening a fresh obscure field; empty submission means clear. Dispose and clear the dialog controller on every exit.

- [ ] **Step 5: Rewrite the empty state**

Use:

```text
Add a server here to register it immediately. For bulk setup, edit the mounted
mcp.json file and choose Re-import mcp.json. Neither path needs a backend restart.
```

Keep new servers described as disabled until explicitly enabled.

- [ ] **Step 6: Run focused and existing MCP widget tests**

Run:

```bash
( cd turing-client/turing_app && flutter test test/ui/mcps_test.dart test/ui/shell_navigation_test.dart )
```

Expected: PASS with no overflow exception at compact sizes.

### Task 7: Update the security documentation

**Files:**
- Modify: `docs/mcp-security-and-integration.md`
- Modify only if stale MCP-specific copy is found: `docs/**/*.md`

- [ ] **Step 1: Update implemented behavior**

Document that:

- in-app registration and `mcp.json` use the same validation, URL hardening, bundled-name refusal, and sealing;
- direct registration explicitly clears a matching tombstone and creates a disabled server;
- re-import is create-only for existing names, preserving enablement, policies, endpoints, and rotated credentials;
- on-demand re-import and in-app registration require no backend restart;
- rotation is write-only, empty clears, bundled servers refuse it, and responses/audit/events never contain token material;
- registration (in-app or via `mcp.json`) never contacts the endpoint by itself — a server stays silent until explicitly enabled;
- explicitly enabling a non-bundled server, including a remote-url one, performs a bounded liveness/tool discovery request against its endpoint at that moment, separately from and in addition to the per-run egress consent a later tool call still requires.

- [ ] **Step 2: Sweep stale MCP restart-only copy**

Run:

```bash
rg -n -i 'mcp\.json|restart.*backend|backend.*restart|startup-only' docs turing-client/turing_app/lib
```

Expected: no MCP copy claims a backend restart is required.

### Task 8: Prove the required tests discriminate

**Files:**
- Temporarily modify and restore production lines in:
  - `turing-backend/orchestrator-go/internal/repository/mcp_servers.go`
  - `turing-backend/orchestrator-go/internal/service/mcpregistry/import.go`
  - `turing-backend/orchestrator-go/internal/service/mcpregistry/service.go`

- [ ] **Step 1: Break disabled-server preservation**

Temporarily set an existing imported row enabled during re-import. Run `TestReimportSkipsExistingDisabledServer`; expected FAIL. Restore and rerun; expected PASS.

- [ ] **Step 2: Break policy preservation**

Temporarily replace the conflict policy with `excluded.policy`. Run `TestReimportPreservesEditedToolPolicy`; expected FAIL. Restore and rerun; expected PASS.

- [ ] **Step 3: Break tombstone survival**

Temporarily ignore import suppression. Run `TestReimportRefusesTombstonedServer`; expected FAIL. Restore and rerun; expected PASS.

- [ ] **Step 4: Break explicit tombstone clearing**

Temporarily omit the tombstone delete from explicit registration. Run `TestExplicitRegistrationClearsTombstone`; expected FAIL. Restore and rerun; expected PASS.

- [ ] **Step 5: Break bundled-name refusal**

Temporarily bypass the reserved-name validation. Run `TestRegisterMcpServerRefusesBundledName`; expected FAIL. Restore and rerun; expected PASS.

- [ ] **Step 6: Break token response secrecy**

Temporarily add the request token to a returned status field or audit payload. Run `TestManagementResponsesAndAuditNeverContainToken`; expected FAIL. Restore and rerun; expected PASS.

### Task 9: Verify, review, commit, and publish

**Files:**
- Review all changed files.

- [ ] **Step 1: Run the complete repository verification matrix**

Run the project `/verify` skill, which executes the root Go tests/build/race, both MCP modules, Flutter analyze/tests, proto check, and all three golangci-lint commands. Expected: every command exits zero.

- [ ] **Step 2: Run two independent complete-diff reviews**

Dispatch one Opus 5 reviewer and one GPT-5.6 Terra reviewer. Give each the complete brief and full changed-file list. Require correctness, security, token-leak analysis, import-intent analysis, bundled/tool-name collision analysis, repeated rotation analysis, test gaps, and simplifications.

- [ ] **Step 3: Fix every accepted finding and repeat reviews**

After any change, rerun targeted tests, then both reviewers on the updated complete diff. Repeat until both report no issues. Record the technical reason for any rejected finding.

- [ ] **Step 4: Re-run the full verification matrix**

Run `/verify` again after the final review-driven edit. Expected: every command exits zero.

- [ ] **Step 5: Commit**

```bash
git add proto gen turing-backend turing-client docs
git commit -m "feat: manage MCP registry from the app" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

- [ ] **Step 6: Open the pull request**

Push the feature branch and open a PR into `main` with a concise security-focused summary and the full verification matrix in the test plan.

- [ ] **Step 7: Watch required CI checks until green**

Use `gh pr checks --watch` and inspect/fix failures for Go tests and build, MCP files, MCP system, Lint, Proto and script checks, and Flutter tests. Stop only when all required checks pass.

## Addendum: enable-time discovery follow-up

This plan shipped registration, re-import, and rotation with enablement left
as a purely local state flip for a remote-url server — an imported or
registered remote entry stayed visible with no callable tools until an
`mcp.json` entry happened to carry its own `tools` snapshot. That gap was
closed in the same feature branch by a follow-up ("feat: discover remote MCP
tools on enable" and its audit/idempotency fix-ups): `SetMcpServerEnabled`
now runs the same bounded `tools/list` discovery for a remote-url server that
a local-container server always got, reconciling live tools/schemas while
preserving edited policy and seeding new tools through `DefaultPolicyFor`. A
failed discovery marks the server down with a redacted, bounded status
message and leaves any prior snapshot in place; the enabled state itself is
never rolled back by a failed discovery. Enable and disable are now audited
(`mcp.server.enabled` / `mcp.server.disabled`, carrying only name, tier, and
whether discovery was attempted/succeeded — never a URL, token, or status
string), alongside `mcp.server.reimported`.

This does not change the per-run egress boundary Task 7 above describes:
dispatching a remote tool during a run still requires the caller-prepared,
run-acknowledged signed egress decision naming the endpoint and the
tool-argument/tool-result categories. Enable-time discovery is a one-time
liveness contact at the moment of the operator's own explicit action, not a
substitute for that per-run consent. This document remains a record of how
the feature was implemented; see `docs/mcp-security-and-integration.md` for
the current, authoritative description of shipped behavior.
