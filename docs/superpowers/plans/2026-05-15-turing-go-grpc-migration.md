# Turing Go gRPC Migration Implementation Plan

**Historical plan:** This record is not current implementation or status.
Use the [canonical roadmap](../../NORTH_STAR.md) for current truth.

**Goal:** Replace the TypeScript Turing orchestrator and agent-runtime with Go services using public and internal gRPC, server-streamed AI token responses, safe dynamic JSON handling, and end-to-end cancellation.

**Architecture:** Implement this as a contract-first side-by-side migration. Add stable `proto/turing/v1` contracts and checked-in generated stubs, then build a Go orchestrator that preserves the current SQLite behavior, a Go agent-runtime that connects over internal gRPC, and a Flutter gRPC client that replaces REST/WebSocket networking before deleting the TypeScript backend runtime.

**Tech Stack:** Go 1.23, gRPC, Protocol Buffers, `database/sql`, `github.com/mattn/go-sqlite3`, `golang.org/x/sync/errgroup`, SQLite WAL, Dart/Flutter gRPC, existing Go MCP JSON-RPC/HTTP servers, Docker Compose.

---

## Source of truth

- Design spec: `docs/superpowers/specs/2026-05-15-turing-go-grpc-migration-design.md`
- Existing TypeScript behavior to preserve until cutover:
  - `turing-backend/orchestrator/src/api/routes.ts`
  - `turing-backend/orchestrator/src/internal/routes.ts`
  - `turing-backend/orchestrator/src/ws/gateway.ts`
  - `turing-backend/orchestrator/src/events/service.ts`
  - `turing-backend/orchestrator/src/jobs/service.ts`
  - `turing-backend/orchestrator/src/sessions/service.ts`
  - `turing-backend/orchestrator/src/approvals/service.ts`
  - `turing-backend/agent-runtime/src/main.ts`
  - `turing-backend/agent-runtime/src/agents/generalAssistant.ts`
  - `turing-backend/agent-runtime/src/llm/ollama.ts`
  - `turing-backend/agent-runtime/src/llm/openaiCompatible.ts`
  - `turing-backend/agent-runtime/src/mcp/client.ts`
  - `turing-backend/agent-runtime/src/tools/toolRunner.ts`
- Existing SQLite schema: `turing-backend/orchestrator/migrations/0001_initial.sql`
- Current Flutter networking to replace:
  - `turing-client/turing_app/lib/networking/api_client.dart`
  - `turing-client/turing_app/lib/networking/ws_client.dart`

## Scope check

The spec covers proto contracts, a Go orchestrator, a Go runtime, generated stubs for multiple client platforms, Flutter integration, Docker cutover, and deletion of the TypeScript backend runtime. This plan keeps them in one ordered sequence because the transport contract is cross-cutting and each task produces a testable slice. If execution needs to split branches, split after Task 4 for backend protocol/state work and after Task 9 for client/runtime cutover work.

## Target file structure

```text
proto/turing/v1/
  common.proto
  sessions.proto
  events.proto
  chat.proto
  approvals.proto
  tools.proto
  runtime.proto
  mcp.proto
  health.proto

tools/proto/
  generate.sh
  check.sh
  README.md

gen/turing/v1/
  go/                  # generated Go pb/grpc code
  dart/                # generated Dart/Flutter pb/grpc code
  swift/               # generated macOS future-client stubs
  csharp/              # generated Windows future-client stubs
  kotlin/              # generated Android future-client stubs

go.mod                 # root Go module for generated stubs and Go services
go.sum
turing-backend/orchestrator-go/
  cmd/server/main.go
  internal/app/app.go
  internal/auth/interceptor.go
  internal/config/config.go
  internal/db/connection.go
  internal/db/migrations.go
  internal/db/schema/0001_initial.sql
  internal/db/schema/0002_go_runtime.sql
  internal/repository/sessions.go
  internal/repository/events.go
  internal/repository/runs.go
  internal/repository/jobs.go
  internal/repository/approvals.go
  internal/repository/toolcalls.go
  internal/repository/audit.go
  internal/service/events/bus.go
  internal/service/sessions/service.go
  internal/service/chat/service.go
  internal/service/runtime/service.go
  internal/service/approvals/service.go
  internal/service/tools/policy.go
  internal/service/audit/service.go
  internal/safejson/safejson.go
  internal/ids/ids.go
  Dockerfile

turing-backend/agent-runtime-go/
  cmd/runtime/main.go
  internal/config/config.go
  internal/orchestrator/client.go
  internal/worker/worker.go
  internal/agent/general_assistant.go
  internal/llm/provider.go
  internal/llm/ollama.go
  internal/llm/openai_compatible.go
  internal/mcp/client.go
  internal/tools/runner.go
  internal/safejson/safejson.go
  Dockerfile

turing-backend/tests/
  grpc_harness_test.go
  parity_test.go
  cancellation_test.go

turing-client/turing_app/lib/generated/turing/v1/   # copied or referenced generated Dart stubs
turing-client/turing_app/lib/networking/grpc_client.dart
turing-client/turing_app/lib/networking/grpc_event_source.dart
turing-client/turing_app/lib/models/grpc_mappers.dart
```

## Commit strategy

Commit after each task that passes its verification command. Use the commit messages shown in each task. Every commit must include:

```text
Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>
```

## Task 1: Proto contracts and generation tooling

**Files:**
- Create: `proto/turing/v1/common.proto`
- Create: `proto/turing/v1/sessions.proto`
- Create: `proto/turing/v1/events.proto`
- Create: `proto/turing/v1/chat.proto`
- Create: `proto/turing/v1/approvals.proto`
- Create: `proto/turing/v1/tools.proto`
- Create: `proto/turing/v1/runtime.proto`
- Create: `proto/turing/v1/mcp.proto`
- Create: `proto/turing/v1/health.proto`
- Create: `tools/proto/generate.sh`
- Create: `tools/proto/check.sh`
- Create: `tools/proto/README.md`
- Create: `turing-backend/tests/proto_contract_test.go`
- Generated: `gen/turing/v1/go/**`
- Generated: `gen/turing/v1/dart/**`
- Generated: `gen/turing/v1/swift/**`
- Generated: `gen/turing/v1/csharp/**`
- Generated: `gen/turing/v1/kotlin/**`
- Create: `go.mod`
- Create: `go.sum`

- [ ] **Step 1: Write the proto contract test before adding protos**

Create `turing-backend/tests/proto_contract_test.go`:

```go
package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProtoContractsDefineRequiredServices(t *testing.T) {
	root := filepath.Join("..", "..", "proto", "turing", "v1")
	required := map[string][]string{
		"chat.proto":      {"service ChatService", "rpc SendMessage", "returns (stream ChatStreamEvent)", "message TokenDelta"},
		"events.proto":    {"service EventService", "rpc ListEvents", "rpc SubscribeSessionEvents", "message TuringEvent"},
		"runtime.proto":   {"service RuntimeService", "rpc ConnectWorker", "returns (stream RuntimeCommand)", "stream RuntimeUpdate"},
		"sessions.proto":  {"service SessionService", "rpc CreateSession", "rpc ListMessages", "rpc ListTools"},
		"approvals.proto": {"service ApprovalService", "rpc ApproveApproval", "rpc DenyApproval"},
		"tools.proto":     {"message ToolCallBeacon", "message ToolPolicyDecision"},
		"mcp.proto":       {"message McpRequest", "message McpResult"},
		"health.proto":    {"service HealthService", "rpc Check", "rpc Version"},
	}
	for file, snippets := range required {
		data, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		text := string(data)
		for _, snippet := range snippets {
			if !strings.Contains(text, snippet) {
				t.Fatalf("%s missing %q", file, snippet)
			}
		}
	}
}

func TestDynamicFieldsUseStructNotRawJsonStrings(t *testing.T) {
	root := filepath.Join("..", "..", "proto", "turing", "v1")
	files, err := filepath.Glob(filepath.Join(root, "*.proto"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		if strings.Contains(text, "bytes raw_json") || strings.Contains(text, "string raw_json") {
			t.Fatalf("%s uses raw_json instead of google.protobuf.Struct", filepath.Base(file))
		}
	}
}
```

- [ ] **Step 2: Run the proto contract test to verify it fails**

Run:

```bash
cd turing-backend
go test ./tests -run TestProtoContractsDefineRequiredServices
```

Expected: FAIL because `proto/turing/v1/*.proto` does not exist yet.

- [ ] **Step 3: Add the shared proto definitions**

Create `proto/turing/v1/common.proto`:

```proto
syntax = "proto3";

package turing.v1;

option go_package = "github.com/mcasillas17/TuringAgent/gen/turing/v1/go;turingv1";

import "google/protobuf/struct.proto";
import "google/protobuf/timestamp.proto";

enum AgentId {
  AGENT_ID_UNSPECIFIED = 0;
  AGENT_ID_GENERAL_ASSISTANT = 1;
}

enum ModelProvider {
  MODEL_PROVIDER_UNSPECIFIED = 0;
  MODEL_PROVIDER_OLLAMA = 1;
  MODEL_PROVIDER_OPENAI_COMPATIBLE = 2;
}

enum MessageRole {
  MESSAGE_ROLE_UNSPECIFIED = 0;
  MESSAGE_ROLE_SYSTEM = 1;
  MESSAGE_ROLE_USER = 2;
  MESSAGE_ROLE_ASSISTANT = 3;
  MESSAGE_ROLE_TOOL = 4;
}

enum ToolPolicy {
  TOOL_POLICY_UNSPECIFIED = 0;
  TOOL_POLICY_SAFE = 1;
  TOOL_POLICY_APPROVAL_REQUIRED = 2;
  TOOL_POLICY_DISABLED = 3;
}

enum RunStatus {
  RUN_STATUS_UNSPECIFIED = 0;
  RUN_STATUS_QUEUED = 1;
  RUN_STATUS_RUNNING = 2;
  RUN_STATUS_WAITING_APPROVAL = 3;
  RUN_STATUS_COMPLETED = 4;
  RUN_STATUS_FAILED = 5;
  RUN_STATUS_CANCELLED = 6;
}

message RequestMetadata {
  string request_id = 1;
}

message PageRequest {
  int32 limit = 1;
  string cursor = 2;
}

message PageResponse {
  string next_cursor = 1;
}

message ErrorDetail {
  string code = 1;
  string message = 2;
  string request_id = 3;
  google.protobuf.Struct details = 4;
}

message ProviderConfig {
  ModelProvider provider = 1;
  bool enabled = 2;
  string default_model = 3;
}

message AgentDescriptor {
  AgentId id = 1;
  string display_name = 2;
}

message Message {
  string message_id = 1;
  string session_id = 2;
  string run_id = 3;
  MessageRole role = 4;
  string content = 5;
  string content_type = 6;
  int64 sequence = 7;
  google.protobuf.Timestamp created_at = 8;
}
```

- [ ] **Step 4: Add session, event, chat, approval, tool, runtime, MCP, and health protos**

Create `proto/turing/v1/sessions.proto`:

```proto
syntax = "proto3";

package turing.v1;

option go_package = "github.com/mcasillas17/TuringAgent/gen/turing/v1/go;turingv1";

import "google/protobuf/timestamp.proto";
import "turing/v1/common.proto";

message Session {
  string session_id = 1;
  string title = 2;
  string status = 3;
  google.protobuf.Timestamp created_at = 4;
  google.protobuf.Timestamp updated_at = 5;
}

message CreateSessionRequest {
  string title = 1;
}

message CreateSessionResponse {
  string session_id = 1;
  google.protobuf.Timestamp created_at = 2;
}

message ListSessionsRequest {
  PageRequest page = 1;
}

message ListSessionsResponse {
  repeated Session sessions = 1;
  PageResponse page = 2;
}

message GetSessionRequest {
  string session_id = 1;
}

message ListMessagesRequest {
  string session_id = 1;
  int32 limit = 2;
}

message ListMessagesResponse {
  repeated Message messages = 1;
}

message GetConfigRequest {}

message GetConfigResponse {
  repeated ProviderConfig providers = 1;
  bool approvals_enabled = 2;
  bool files_mcp_enabled = 3;
}

message ListAgentsRequest {}

message ListAgentsResponse {
  repeated AgentDescriptor agents = 1;
}

message ToolDescriptor {
  string server_name = 1;
  string tool_name = 2;
  ToolPolicy policy = 3;
}

message ListToolsRequest {}

message ListToolsResponse {
  repeated ToolDescriptor tools = 1;
}

service SessionService {
  rpc CreateSession(CreateSessionRequest) returns (CreateSessionResponse);
  rpc ListSessions(ListSessionsRequest) returns (ListSessionsResponse);
  rpc GetSession(GetSessionRequest) returns (Session);
  rpc ListMessages(ListMessagesRequest) returns (ListMessagesResponse);
  rpc GetConfig(GetConfigRequest) returns (GetConfigResponse);
  rpc ListAgents(ListAgentsRequest) returns (ListAgentsResponse);
  rpc ListTools(ListToolsRequest) returns (ListToolsResponse);
}
```

Create `proto/turing/v1/events.proto`:

```proto
syntax = "proto3";

package turing.v1;

option go_package = "github.com/mcasillas17/TuringAgent/gen/turing/v1/go;turingv1";

import "google/protobuf/struct.proto";
import "google/protobuf/timestamp.proto";

enum TuringEventType {
  TURING_EVENT_TYPE_UNSPECIFIED = 0;
  TURING_EVENT_TYPE_MESSAGE_STARTED = 1;
  TURING_EVENT_TYPE_MESSAGE_DELTA = 2;
  TURING_EVENT_TYPE_MESSAGE_COMPLETED = 3;
  TURING_EVENT_TYPE_AGENT_RUN_QUEUED = 4;
  TURING_EVENT_TYPE_AGENT_RUN_STARTED = 5;
  TURING_EVENT_TYPE_AGENT_RUN_STEP = 6;
  TURING_EVENT_TYPE_AGENT_RUN_COMPLETED = 7;
  TURING_EVENT_TYPE_AGENT_RUN_FAILED = 8;
  TURING_EVENT_TYPE_AGENT_RUN_CANCELLED = 9;
  TURING_EVENT_TYPE_TOOL_CALL_STARTED = 10;
  TURING_EVENT_TYPE_TOOL_CALL_COMPLETED = 11;
  TURING_EVENT_TYPE_TOOL_CALL_FAILED = 12;
  TURING_EVENT_TYPE_TOOL_CALL_DENIED = 13;
  TURING_EVENT_TYPE_APPROVAL_REQUESTED = 14;
  TURING_EVENT_TYPE_APPROVAL_APPROVED = 15;
  TURING_EVENT_TYPE_APPROVAL_DENIED = 16;
  TURING_EVENT_TYPE_APPROVAL_EXPIRED = 17;
  TURING_EVENT_TYPE_APPROVAL_CONSUMED = 18;
  TURING_EVENT_TYPE_ERROR = 19;
  TURING_EVENT_TYPE_SYSTEM = 20;
}

message TuringEvent {
  string event_id = 1;
  string session_id = 2;
  string run_id = 3;
  string trace_id = 4;
  int64 sequence = 5;
  TuringEventType type = 6;
  google.protobuf.Timestamp created_at = 7;
  google.protobuf.Struct payload = 8;
}

message ListEventsRequest {
  string session_id = 1;
  int64 after_sequence = 2;
  int32 limit = 3;
}

message ListEventsResponse {
  repeated TuringEvent events = 1;
  int64 latest_sequence = 2;
  bool resync_required = 3;
}

message SubscribeSessionEventsRequest {
  string session_id = 1;
  int64 after_sequence = 2;
}

service EventService {
  rpc ListEvents(ListEventsRequest) returns (ListEventsResponse);
  rpc SubscribeSessionEvents(SubscribeSessionEventsRequest) returns (stream TuringEvent);
}
```

Create `proto/turing/v1/chat.proto`:

```proto
syntax = "proto3";

package turing.v1;

option go_package = "github.com/mcasillas17/TuringAgent/gen/turing/v1/go;turingv1";

import "google/protobuf/struct.proto";
import "turing/v1/common.proto";
import "turing/v1/events.proto";

message SendMessageRequest {
  string session_id = 1;
  string content = 2;
  string content_type = 3;
  AgentId agent_id = 4;
  ModelProvider model_provider = 5;
  string model = 6;
  string idempotency_key = 7;
}

message RunQueued {
  string run_id = 1;
  string job_id = 2;
  string trace_id = 3;
}

message RunStarted {
  string run_id = 1;
  string job_id = 2;
  int32 attempt = 3;
}

message MessageStarted {
  string message_id = 1;
  MessageRole role = 2;
}

message TokenDelta {
  string message_id = 1;
  string delta = 2;
}

message ToolEvent {
  string tool_call_id = 1;
  string server_name = 2;
  string tool_name = 3;
  google.protobuf.Struct payload = 4;
}

message ApprovalEvent {
  string approval_id = 1;
  string tool_name = 2;
  string args_summary = 3;
}

message MessageCompleted {
  string message_id = 1;
  string content = 2;
}

message RunCompleted {
  string run_id = 1;
  string assistant_message_id = 2;
}

message RunFailed {
  string run_id = 1;
  string code = 2;
  string message = 3;
  bool retryable = 4;
}

message RunCancelled {
  string run_id = 1;
  string reason = 2;
}

message ChatStreamEvent {
  string session_id = 1;
  string run_id = 2;
  string trace_id = 3;
  int64 sequence = 4;
  oneof event {
    RunQueued run_queued = 10;
    RunStarted run_started = 11;
    MessageStarted message_started = 12;
    TokenDelta token_delta = 13;
    ToolEvent tool_call_started = 14;
    ToolEvent tool_call_completed = 15;
    ToolEvent tool_call_failed = 16;
    ApprovalEvent approval_requested = 17;
    ApprovalEvent approval_approved = 18;
    ApprovalEvent approval_denied = 19;
    ApprovalEvent approval_expired = 20;
    ApprovalEvent approval_consumed = 21;
    MessageCompleted message_completed = 22;
    RunCompleted run_completed = 23;
    RunFailed run_failed = 24;
    RunCancelled run_cancelled = 25;
    TuringEvent persisted_event = 26;
  }
}

service ChatService {
  rpc SendMessage(SendMessageRequest) returns (stream ChatStreamEvent);
}
```

Create `proto/turing/v1/approvals.proto`:

```proto
syntax = "proto3";

package turing.v1;

option go_package = "github.com/mcasillas17/TuringAgent/gen/turing/v1/go;turingv1";

enum ApprovalStatus {
  APPROVAL_STATUS_UNSPECIFIED = 0;
  APPROVAL_STATUS_PENDING = 1;
  APPROVAL_STATUS_APPROVED = 2;
  APPROVAL_STATUS_DENIED = 3;
  APPROVAL_STATUS_EXPIRED = 4;
  APPROVAL_STATUS_CONSUMED = 5;
}

message ApproveApprovalRequest {
  string approval_id = 1;
  string comment = 2;
}

message DenyApprovalRequest {
  string approval_id = 1;
  string reason = 2;
}

message ApprovalResponse {
  string approval_id = 1;
  ApprovalStatus status = 2;
}

service ApprovalService {
  rpc ApproveApproval(ApproveApprovalRequest) returns (ApprovalResponse);
  rpc DenyApproval(DenyApprovalRequest) returns (ApprovalResponse);
}
```

Create `proto/turing/v1/tools.proto`:

```proto
syntax = "proto3";

package turing.v1;

option go_package = "github.com/mcasillas17/TuringAgent/gen/turing/v1/go;turingv1";

import "google/protobuf/struct.proto";
import "turing/v1/common.proto";

enum ToolCallPhase {
  TOOL_CALL_PHASE_UNSPECIFIED = 0;
  TOOL_CALL_PHASE_BEFORE = 1;
  TOOL_CALL_PHASE_AFTER = 2;
}

enum ToolCallStatus {
  TOOL_CALL_STATUS_UNSPECIFIED = 0;
  TOOL_CALL_STATUS_COMPLETED = 1;
  TOOL_CALL_STATUS_FAILED = 2;
  TOOL_CALL_STATUS_DENIED = 3;
}

message ToolCallError {
  string code = 1;
  string message = 2;
}

message ToolCallBeacon {
  ToolCallPhase phase = 1;
  string tool_call_id = 2;
  AgentId agent_id = 3;
  string server_name = 4;
  string tool_name = 5;
  google.protobuf.Struct args = 6;
  ToolCallStatus status = 7;
  string result_summary = 8;
  int64 duration_ms = 9;
  ToolCallError error = 10;
  string run_id = 11;
  string trace_id = 12;
}

message ToolPolicyDecision {
  enum Decision {
    DECISION_UNSPECIFIED = 0;
    DECISION_ALLOW = 1;
    DECISION_DENY = 2;
    DECISION_APPROVAL_REQUIRED = 3;
  }
  Decision decision = 1;
  string tool_call_id = 2;
  string approval_id = 3;
  string reason = 4;
}
```

Create `proto/turing/v1/runtime.proto`:

```proto
syntax = "proto3";

package turing.v1;

option go_package = "github.com/mcasillas17/TuringAgent/gen/turing/v1/go;turingv1";

import "google/protobuf/struct.proto";
import "turing/v1/common.proto";
import "turing/v1/events.proto";
import "turing/v1/tools.proto";

message AgentJob {
  string job_id = 1;
  string run_id = 2;
  string session_id = 3;
  string user_message_id = 4;
  string assistant_message_id = 5;
  AgentId agent_id = 6;
  string trace_id = 7;
  ModelProvider model_provider = 8;
  string model = 9;
  string user_text = 10;
  repeated string requested_tools = 11;
  int32 attempt = 12;
}

message RuntimeWorkerReady {
  string worker_id = 1;
  AgentId agent_id = 2;
  int32 max_concurrent_runs = 3;
}

message RuntimeHeartbeat {
  string worker_id = 1;
}

message RuntimeRunCompleted {
  string run_id = 1;
  string assistant_message_id = 2;
  string content = 3;
  google.protobuf.Struct usage = 4;
}

message RuntimeRunFailed {
  string run_id = 1;
  string code = 2;
  string message = 3;
  bool retryable = 4;
}

message RuntimeCancelledAck {
  string run_id = 1;
}

message RuntimeUpdate {
  oneof update {
    RuntimeWorkerReady worker_ready = 1;
    RuntimeHeartbeat heartbeat = 2;
    TuringEvent event = 3;
    ToolCallBeacon tool_beacon = 4;
    RuntimeRunCompleted run_completed = 5;
    RuntimeRunFailed run_failed = 6;
    RuntimeCancelledAck run_cancelled_ack = 7;
  }
}

message RuntimeWorkerAccepted {
  string worker_id = 1;
}

message RuntimeRunCancelled {
  string run_id = 1;
  string reason = 2;
}

message RuntimeApprovalUpdated {
  string approval_id = 1;
}

message RuntimeShutdownRequested {
  string reason = 1;
}

message RuntimeCommand {
  oneof command {
    RuntimeWorkerAccepted worker_accepted = 1;
    AgentJob run_assigned = 2;
    RuntimeRunCancelled run_cancelled = 3;
    RuntimeApprovalUpdated approval_updated = 4;
    RuntimeShutdownRequested shutdown_requested = 5;
  }
}

service RuntimeService {
  rpc ConnectWorker(stream RuntimeUpdate) returns (stream RuntimeCommand);
}
```

Create `proto/turing/v1/mcp.proto`:

```proto
syntax = "proto3";

package turing.v1;

option go_package = "github.com/mcasillas17/TuringAgent/gen/turing/v1/go;turingv1";

import "google/protobuf/struct.proto";

message McpRequest {
  string server_name = 1;
  string method = 2;
  google.protobuf.Struct params = 3;
}

message McpResult {
  google.protobuf.Struct result = 1;
}
```

Create `proto/turing/v1/health.proto`:

```proto
syntax = "proto3";

package turing.v1;

option go_package = "github.com/mcasillas17/TuringAgent/gen/turing/v1/go;turingv1";

message HealthCheckRequest {}

message HealthCheckResponse {
  bool ok = 1;
}

message VersionRequest {}

message VersionResponse {
  string version = 1;
  string schema_version = 2;
}

service HealthService {
  rpc Check(HealthCheckRequest) returns (HealthCheckResponse);
  rpc Version(VersionRequest) returns (VersionResponse);
}
```

- [ ] **Step 5: Add deterministic generation scripts**

Create `tools/proto/generate.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PROTO_DIR="$ROOT/proto"
OUT_DIR="$ROOT/gen/turing/v1"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required tool: $1" >&2
    exit 127
  fi
}

require protoc
require protoc-gen-go
require protoc-gen-go-grpc

mkdir -p "$OUT_DIR/go" "$OUT_DIR/dart" "$OUT_DIR/swift" "$OUT_DIR/csharp" "$OUT_DIR/kotlin"

protoc -I "$PROTO_DIR" \
  --go_out="$OUT_DIR/go" --go_opt=paths=source_relative \
  --go-grpc_out="$OUT_DIR/go" --go-grpc_opt=paths=source_relative \
  "$PROTO_DIR"/turing/v1/*.proto

if command -v protoc-gen-dart >/dev/null 2>&1; then
  protoc -I "$PROTO_DIR" --dart_out=grpc:"$OUT_DIR/dart" "$PROTO_DIR"/turing/v1/*.proto
else
  echo "protoc-gen-dart not installed; skipping Dart generation" >&2
fi

if command -v protoc-gen-swift >/dev/null 2>&1 && command -v protoc-gen-grpc-swift >/dev/null 2>&1; then
  protoc -I "$PROTO_DIR" --swift_out="$OUT_DIR/swift" --grpc-swift_out="$OUT_DIR/swift" "$PROTO_DIR"/turing/v1/*.proto
else
  echo "Swift protoc plugins not installed; skipping Swift generation" >&2
fi

if command -v grpc_csharp_plugin >/dev/null 2>&1; then
  protoc -I "$PROTO_DIR" --csharp_out="$OUT_DIR/csharp" --grpc_out="$OUT_DIR/csharp" --plugin=protoc-gen-grpc="$(command -v grpc_csharp_plugin)" "$PROTO_DIR"/turing/v1/*.proto
else
  echo "grpc_csharp_plugin not installed; skipping C# generation" >&2
fi

if command -v protoc-gen-grpc-java >/dev/null 2>&1; then
  protoc -I "$PROTO_DIR" --java_out="$OUT_DIR/kotlin" --grpc-java_out="$OUT_DIR/kotlin" "$PROTO_DIR"/turing/v1/*.proto
else
  echo "protoc-gen-grpc-java not installed; skipping Android Java/Kotlin-compatible generation" >&2
fi
```

Create `tools/proto/check.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
before="$(git -C "$ROOT" status --porcelain -- gen proto)"
"$ROOT/tools/proto/generate.sh"
after="$(git -C "$ROOT" status --porcelain -- gen proto)"

if [[ "$before" != "$after" ]]; then
  echo "generated proto output is not deterministic or not committed" >&2
  git -C "$ROOT" --no-pager status --short -- gen proto >&2
  exit 1
fi
```

Create `tools/proto/README.md`:

```markdown
# Proto generation

`proto/turing/v1` is the source of truth for Turing gRPC contracts.

Normal backend builds use checked-in generated code and do not require code generation.

To regenerate Go stubs:

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
tools/proto/generate.sh
```

Optional client generators are used when installed:

- `protoc-gen-dart` for Flutter
- `protoc-gen-swift` and `protoc-gen-grpc-swift` for macOS
- `grpc_csharp_plugin` for Windows
- `protoc-gen-grpc-java` for Android-compatible stubs

When optional generators are not installed, `gen/turing/v1/dart`, `gen/turing/v1/swift`, `gen/turing/v1/csharp`, and `gen/turing/v1/kotlin` may contain only `.gitkeep` placeholders. These directories are reserved for future checked-in client stubs.
```

Create root `go.mod` for generated Go stubs and future Go services:

```go
module github.com/mcasillas17/TuringAgent

go 1.23

require (
	google.golang.org/grpc v1.69.2
	google.golang.org/protobuf v1.36.1
)
```

Run:

```bash
chmod +x tools/proto/generate.sh tools/proto/check.sh
```

- [ ] **Step 6: Generate Go stubs and run the proto contract test**

Run:

```bash
tools/proto/generate.sh
go mod tidy
cd turing-backend
go test ./tests -run TestProtoContracts
```

Expected: PASS for both proto contract tests.

- [ ] **Step 7: Commit proto contracts**

Run:

```bash
git add proto tools/proto gen/turing/v1 turing-backend/tests/proto_contract_test.go go.mod go.sum
git commit -m "feat: add Turing gRPC proto contracts" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

## Task 2: Go backend module and shared foundations

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `turing-backend/orchestrator-go/internal/config/config.go`
- Create: `turing-backend/orchestrator-go/internal/auth/interceptor.go`
- Create: `turing-backend/orchestrator-go/internal/ids/ids.go`
- Create: `turing-backend/orchestrator-go/internal/safejson/safejson.go`
- Create: `turing-backend/orchestrator-go/internal/config/config_test.go`
- Create: `turing-backend/orchestrator-go/internal/auth/interceptor_test.go`
- Create: `turing-backend/orchestrator-go/internal/ids/ids_test.go`
- Create: `turing-backend/orchestrator-go/internal/safejson/safejson_test.go`
- Create: `turing-backend/agent-runtime-go/internal/safejson/safejson.go`

- [ ] **Step 1: Write failing tests for config, auth, IDs, and safe JSON**

Create `turing-backend/orchestrator-go/internal/config/config_test.go`:

```go
package config

import "testing"

func TestLoadFromEnvRequiresSecretsAndDefaultsPorts(t *testing.T) {
	env := map[string]string{
		"TURING_CLIENT_API_KEY":     "client-key",
		"TURING_INTERNAL_TOKEN":     "internal-token",
		"MCP_SYSTEM_TOKEN_GENERAL":  "system-token",
		"MCP_FILES_TOKEN_GENERAL":   "files-token",
		"TURING_APPROVAL_JWT_SECRET": "approval-secret",
	}
	cfg, err := LoadFromMap(env)
	if err != nil {
		t.Fatalf("LoadFromMap returned error: %v", err)
	}
	if cfg.PublicPort != 3000 || cfg.InternalPort != 3001 {
		t.Fatalf("ports = %d/%d, want 3000/3001", cfg.PublicPort, cfg.InternalPort)
	}
	if cfg.OllamaModel != "llama3.2" {
		t.Fatalf("OllamaModel = %q", cfg.OllamaModel)
	}
}

func TestLoadFromEnvRejectsInvalidInteger(t *testing.T) {
	env := map[string]string{
		"TURING_CLIENT_API_KEY":     "client-key",
		"TURING_INTERNAL_TOKEN":     "internal-token",
		"MCP_SYSTEM_TOKEN_GENERAL":  "system-token",
		"MCP_FILES_TOKEN_GENERAL":   "files-token",
		"TURING_APPROVAL_JWT_SECRET": "approval-secret",
		"ORCHESTRATOR_PUBLIC_PORT":  "abc",
	}
	_, err := LoadFromMap(env)
	if err == nil {
		t.Fatal("expected invalid integer error")
	}
}
```

Create `turing-backend/orchestrator-go/internal/auth/interceptor_test.go`:

```go
package auth

import (
	"context"
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestTokenFromMetadata(t *testing.T) {
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer secret"))
	got, ok := TokenFromMetadata(ctx)
	if !ok || got != "secret" {
		t.Fatalf("TokenFromMetadata = %q/%v", got, ok)
	}
}

func TestConstantTimeTokenMatch(t *testing.T) {
	if !TokenMatches("secret", "secret") {
		t.Fatal("same token did not match")
	}
	if TokenMatches("secret", "different") {
		t.Fatal("different tokens matched")
	}
}
```

Create `turing-backend/orchestrator-go/internal/ids/ids_test.go`:

```go
package ids

import (
	"strings"
	"testing"
)

func TestNewPrefixedID(t *testing.T) {
	got := New("run")
	if !strings.HasPrefix(got, "run_") {
		t.Fatalf("id %q missing prefix", got)
	}
	if len(got) <= len("run_") {
		t.Fatalf("id %q too short", got)
	}
}
```

Create `turing-backend/orchestrator-go/internal/safejson/safejson_test.go`:

```go
package safejson

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestDecodeObjectUsesNumber(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(`{"count":9007199254740993}`))
	got, err := DecodeObject(dec)
	if err != nil {
		t.Fatalf("DecodeObject returned error: %v", err)
	}
	if _, ok := got["count"].(json.Number); !ok {
		t.Fatalf("count type = %T, want json.Number", got["count"])
	}
}

func TestNormalizeRejectsNaN(t *testing.T) {
	_, err := Normalize(map[string]any{"bad": math.NaN()})
	if err == nil {
		t.Fatal("expected NaN rejection")
	}
}

func TestToStructConvertsObject(t *testing.T) {
	got, err := ToStruct(map[string]any{"ok": true, "nested": map[string]any{"value": "x"}})
	if err != nil {
		t.Fatalf("ToStruct returned error: %v", err)
	}
	if !got.Fields["ok"].GetBoolValue() {
		t.Fatal("ok field was not true")
	}
}

func TestSummaryLimitsBytes(t *testing.T) {
	got := Summary(map[string]any{"value": strings.Repeat("a", 100)}, 20)
	if len(got) > 20 {
		t.Fatalf("summary length = %d, want <= 20", len(got))
	}
}
```

- [ ] **Step 2: Run the foundation tests to verify they fail**

Run:

```bash
cd turing-backend
go test ./orchestrator-go/internal/config ./orchestrator-go/internal/auth ./orchestrator-go/internal/ids ./orchestrator-go/internal/safejson
```

Expected: FAIL with missing module or undefined package errors.

- [ ] **Step 3: Update the root Go module and add foundation code**

Update root `go.mod` to include only the dependencies imported by Task 1/2 code:

```go
module github.com/mcasillas17/TuringAgent

go 1.23

require (
	github.com/oklog/ulid/v2 v2.1.0
	google.golang.org/grpc v1.69.2
	google.golang.org/protobuf v1.36.1
)
```

Do not add SQLite or errgroup dependencies in Task 2; Task 3 and Task 9 add them when their code first imports them.

Create `turing-backend/orchestrator-go/internal/config/config.go`:

```go
package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	ClientAPIKey            string
	InternalToken           string
	MCPSystemTokenGeneral   string
	MCPFilesTokenGeneral    string
	ApprovalJWTSecret       string
	PublicPort              int
	InternalPort            int
	DatabasePath            string
	OllamaBaseURL           string
	OllamaModel             string
	OpenAIBaseURL           string
	OpenAIAPIKey            string
	OpenAIModel             string
	JobTimeoutMS            int
	JobReaperIntervalMS     int
	JobMaxAttempts          int
	MaxConcurrentRunsGeneral int
	MaxToolCallsPerRun      int
	ModelTimeoutMS          int
	ToolTimeoutMS           int
	LogLevel                string
}

func Load() (Config, error) {
	env := map[string]string{}
	for _, item := range os.Environ() {
		for i := 0; i < len(item); i++ {
			if item[i] == '=' {
				env[item[:i]] = item[i+1:]
				break
			}
		}
	}
	return LoadFromMap(env)
}

func LoadFromMap(env map[string]string) (Config, error) {
	required := func(name string) (string, error) {
		if env[name] == "" {
			return "", fmt.Errorf("missing required env var %s", name)
		}
		return env[name], nil
	}
	intValue := func(name string, fallback int) (int, error) {
		raw := env[name]
		if raw == "" {
			return fallback, nil
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("invalid integer env var %s", name)
		}
		return n, nil
	}
	stringValue := func(name, fallback string) string {
		if env[name] != "" {
			return env[name]
		}
		return fallback
	}

	clientKey, err := required("TURING_CLIENT_API_KEY")
	if err != nil {
		return Config{}, err
	}
	internalToken, err := required("TURING_INTERNAL_TOKEN")
	if err != nil {
		return Config{}, err
	}
	systemToken, err := required("MCP_SYSTEM_TOKEN_GENERAL")
	if err != nil {
		return Config{}, err
	}
	filesToken, err := required("MCP_FILES_TOKEN_GENERAL")
	if err != nil {
		return Config{}, err
	}
	approvalSecret, err := required("TURING_APPROVAL_JWT_SECRET")
	if err != nil {
		return Config{}, err
	}
	publicPort, err := intValue("ORCHESTRATOR_PUBLIC_PORT", 3000)
	if err != nil {
		return Config{}, err
	}
	internalPort, err := intValue("ORCHESTRATOR_INTERNAL_PORT", 3001)
	if err != nil {
		return Config{}, err
	}
	jobTimeout, err := intValue("TURING_JOB_TIMEOUT_MS", 300000)
	if err != nil {
		return Config{}, err
	}
	reaperInterval, err := intValue("TURING_JOB_REAPER_INTERVAL_MS", 60000)
	if err != nil {
		return Config{}, err
	}
	maxAttempts, err := intValue("TURING_JOB_MAX_ATTEMPTS", 3)
	if err != nil {
		return Config{}, err
	}
	maxRuns, err := intValue("TURING_MAX_CONCURRENT_RUNS_GENERAL", 1)
	if err != nil {
		return Config{}, err
	}
	maxTools, err := intValue("TURING_MAX_TOOL_CALLS_PER_RUN", 10)
	if err != nil {
		return Config{}, err
	}
	modelTimeout, err := intValue("TURING_MODEL_TIMEOUT_MS", 120000)
	if err != nil {
		return Config{}, err
	}
	toolTimeout, err := intValue("TURING_TOOL_TIMEOUT_MS", 30000)
	if err != nil {
		return Config{}, err
	}

	return Config{
		ClientAPIKey:            clientKey,
		InternalToken:           internalToken,
		MCPSystemTokenGeneral:   systemToken,
		MCPFilesTokenGeneral:    filesToken,
		ApprovalJWTSecret:       approvalSecret,
		PublicPort:              publicPort,
		InternalPort:            internalPort,
		DatabasePath:            stringValue("DATABASE_PATH", "/app/data/turing.db"),
		OllamaBaseURL:           stringValue("OLLAMA_BASE_URL", "http://host.docker.internal:11434"),
		OllamaModel:             stringValue("OLLAMA_MODEL", "llama3.2"),
		OpenAIBaseURL:           stringValue("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		OpenAIAPIKey:            env["OPENAI_API_KEY"],
		OpenAIModel:             stringValue("OPENAI_MODEL", "gpt-4o-mini"),
		JobTimeoutMS:            jobTimeout,
		JobReaperIntervalMS:     reaperInterval,
		JobMaxAttempts:          maxAttempts,
		MaxConcurrentRunsGeneral: maxRuns,
		MaxToolCallsPerRun:      maxTools,
		ModelTimeoutMS:          modelTimeout,
		ToolTimeoutMS:           toolTimeout,
		LogLevel:                stringValue("LOG_LEVEL", "info"),
	}, nil
}
```

Create `turing-backend/orchestrator-go/internal/auth/interceptor.go`:

```go
package auth

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TokenFromMetadata(ctx context.Context) (string, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", false
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return "", false
	}
	raw := values[0]
	if !strings.HasPrefix(raw, "Bearer ") {
		return "", false
	}
	token := strings.TrimPrefix(raw, "Bearer ")
	return token, token != ""
}

func TokenMatches(got, want string) bool {
	if got == "" || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func UnaryInterceptor(requiredToken string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		token, ok := TokenFromMetadata(ctx)
		if !ok || !TokenMatches(token, requiredToken) {
			return nil, status.Error(codes.Unauthenticated, "invalid bearer token")
		}
		return handler(ctx, req)
	}
}

func StreamInterceptor(requiredToken string) grpc.StreamServerInterceptor {
	return func(srv any, stream grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		token, ok := TokenFromMetadata(stream.Context())
		if !ok || !TokenMatches(token, requiredToken) {
			return status.Error(codes.Unauthenticated, "invalid bearer token")
		}
		return handler(srv, stream)
	}
}
```

Create `turing-backend/orchestrator-go/internal/ids/ids.go`:

```go
package ids

import (
	"crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

func New(prefix string) string {
	return prefix + "_" + ulid.MustNew(ulid.Timestamp(time.Now()), rand.Reader).String()
}
```

Create `turing-backend/orchestrator-go/internal/safejson/safejson.go`:

```go
package safejson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"

	"google.golang.org/protobuf/types/known/structpb"
)

func DecodeObject(decoder *json.Decoder) (map[string]any, error) {
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("expected JSON object")
	}
	return obj, nil
}

func DecodeLimitedObject(reader io.Reader, maxBytes int64) (map[string]any, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, maxBytes))
	return DecodeObject(decoder)
}

func Normalize(value any) (any, error) {
	switch v := value.(type) {
	case nil, bool, string:
		return v, nil
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i, nil
		}
		f, err := v.Float64()
		if err != nil {
			return nil, err
		}
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, errors.New("unsupported non-finite number")
		}
		return f, nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, errors.New("unsupported non-finite number")
		}
		return v, nil
	case float32:
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, errors.New("unsupported non-finite number")
		}
		return f, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32:
		return v, nil
	case uint64:
		if v > math.MaxInt64 {
			return nil, errors.New("uint64 exceeds supported range")
		}
		return v, nil
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			normalized, err := Normalize(item)
			if err != nil {
				return nil, err
			}
			out = append(out, normalized)
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			normalized, err := Normalize(item)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			out[key] = normalized
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported JSON value %T", value)
	}
}

func ToStruct(value map[string]any) (*structpb.Struct, error) {
	normalized, err := Normalize(value)
	if err != nil {
		return nil, err
	}
	obj, ok := normalized.(map[string]any)
	if !ok {
		return nil, errors.New("expected normalized object")
	}
	return structpb.NewStruct(obj)
}

func Summary(value any, maxBytes int) string {
	normalized, err := Normalize(value)
	if err != nil {
		return `{"error":"unserializable"}`
	}
	data, err := json.Marshal(canonical(normalized))
	if err != nil {
		return `{"error":"unserializable"}`
	}
	if len(data) <= maxBytes {
		return string(data)
	}
	if maxBytes <= 3 {
		return string(data[:maxBytes])
	}
	return string(data[:maxBytes-3]) + "..."
}

func canonical(value any) any {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			keyBytes, _ := json.Marshal(key)
			valueBytes, _ := json.Marshal(canonical(v[key]))
			buf.Write(keyBytes)
			buf.WriteByte(':')
			buf.Write(valueBytes)
		}
		buf.WriteByte('}')
		var out any
		_ = json.Unmarshal(buf.Bytes(), &out)
		return out
	case []any:
		out := make([]any, 0, len(v))
		for _, item := range v {
			out = append(out, canonical(item))
		}
		return out
	default:
		return v
	}
}
```

Copy the same safe JSON implementation into `turing-backend/agent-runtime-go/internal/safejson/safejson.go` with package name `safejson`. Do not import orchestrator internals from runtime packages.

- [ ] **Step 4: Run foundation tests**

Run:

```bash
go mod tidy
go test ./turing-backend/orchestrator-go/internal/config ./turing-backend/orchestrator-go/internal/auth ./turing-backend/orchestrator-go/internal/ids ./turing-backend/orchestrator-go/internal/safejson
```

Expected: PASS.

- [ ] **Step 5: Commit foundation packages**

Run:

```bash
git add go.mod go.sum turing-backend/orchestrator-go turing-backend/agent-runtime-go/internal/safejson
git commit -m "feat: add Go backend foundation packages" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

## Task 3: SQLite migrations and repositories

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `turing-backend/orchestrator-go/internal/db/schema/0001_initial.sql`
- Create: `turing-backend/orchestrator-go/internal/db/schema/0002_go_runtime.sql`
- Create: `turing-backend/orchestrator-go/internal/db/connection.go`
- Create: `turing-backend/orchestrator-go/internal/db/migrations.go`
- Create: `turing-backend/orchestrator-go/internal/repository/sessions.go`
- Create: `turing-backend/orchestrator-go/internal/repository/events.go`
- Create: `turing-backend/orchestrator-go/internal/repository/runs.go`
- Create: `turing-backend/orchestrator-go/internal/repository/jobs.go`
- Create: `turing-backend/orchestrator-go/internal/repository/approvals.go`
- Create: `turing-backend/orchestrator-go/internal/repository/toolcalls.go`
- Create: `turing-backend/orchestrator-go/internal/repository/audit.go`
- Create: `turing-backend/orchestrator-go/internal/repository/repository_test.go`

- [ ] **Step 1: Write repository tests for current schema behavior and Go runtime additions**

Create `turing-backend/orchestrator-go/internal/repository/repository_test.go`:

```go
package repository

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "turing.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := db.ApplyMigrations(context.Background(), database); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return database
}

func TestSessionMessageRunJobTransaction(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Test chat")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	result, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID:     session.SessionID,
		Content:       "hello",
		AgentID:       "general_assistant",
		ModelProvider: "ollama",
		Model:         "llama3.2",
	})
	if err != nil {
		t.Fatalf("EnqueueUserMessage: %v", err)
	}
	messages, err := repo.ListMessages(ctx, session.SessionID, 50)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(messages))
	}
	if result.Status != "queued" || result.RunID == "" || result.JobID == "" || result.TraceID == "" {
		t.Fatalf("bad enqueue result: %+v", result)
	}
}

func TestEventsAreSequencedPerSession(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Events")
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.AppendEvent(ctx, AppendEventInput{SessionID: session.SessionID, TraceID: "trace_1", Type: "system", PayloadJSON: `{"a":1}`})
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.AppendEvent(ctx, AppendEventInput{SessionID: session.SessionID, TraceID: "trace_1", Type: "system", PayloadJSON: `{"b":2}`})
	if err != nil {
		t.Fatal(err)
	}
	if first.Sequence != 1 || second.Sequence != 2 {
		t.Fatalf("sequences = %d/%d", first.Sequence, second.Sequence)
	}
	replayed, latest, err := repo.ReplayEvents(ctx, session.SessionID, 1, 500)
	if err != nil {
		t.Fatal(err)
	}
	if latest != 2 || len(replayed) != 1 || replayed[0].Sequence != 2 {
		t.Fatalf("replay latest=%d events=%+v", latest, replayed)
	}
}

func TestCancelRunUpdatesRunAndJob(t *testing.T) {
	database := openTestDB(t)
	repo := New(database)
	ctx := context.Background()
	session, err := repo.CreateSession(ctx, "Cancel")
	if err != nil {
		t.Fatal(err)
	}
	enqueued, err := repo.EnqueueUserMessage(ctx, EnqueueUserMessageInput{
		SessionID: session.SessionID, Content: "cancel me", AgentID: "general_assistant", ModelProvider: "ollama", Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.MarkRunRunning(ctx, enqueued.RunID); err != nil {
		t.Fatal(err)
	}
	if err := repo.CancelRun(ctx, enqueued.RunID, "client_cancelled"); err != nil {
		t.Fatal(err)
	}
	run, err := repo.GetRun(ctx, enqueued.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "cancelled" {
		t.Fatalf("run status = %q", run.Status)
	}
}
```

- [ ] **Step 2: Run repository tests to verify they fail**

Run:

```bash
cd turing-backend
go test ./orchestrator-go/internal/repository
```

Expected: FAIL because `db` and repository packages do not exist.

- [ ] **Step 3: Add the SQLite driver dependency**

Run from the repository root before adding DB code:

```bash
go get github.com/mattn/go-sqlite3@v1.14.24
go mod tidy
```

Expected: root `go.mod` includes `github.com/mattn/go-sqlite3 v1.14.24` because `connection.go` imports `_ "github.com/mattn/go-sqlite3"` in this task.

- [ ] **Step 4: Copy the current initial schema and add Go runtime migration**

Copy `turing-backend/orchestrator/migrations/0001_initial.sql` to `turing-backend/orchestrator-go/internal/db/schema/0001_initial.sql`.

Create `turing-backend/orchestrator-go/internal/db/schema/0002_go_runtime.sql`:

```sql
ALTER TABLE agent_runs ADD COLUMN cancellation_reason TEXT;
ALTER TABLE agent_runs ADD COLUMN worker_id TEXT;
ALTER TABLE jobs ADD COLUMN lease_owner TEXT;
ALTER TABLE jobs ADD COLUMN lease_expires_at TEXT;

CREATE INDEX IF NOT EXISTS idx_jobs_lease ON jobs(status, lease_expires_at);
```

- [ ] **Step 5: Add DB connection and migration runner**

Create `turing-backend/orchestrator-go/internal/db/connection.go`:

```go
package db

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	*sql.DB
}

func Open(path string) (*DB, error) {
	database, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?_foreign_keys=on&_journal_mode=WAL", path))
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, err
	}
	return &DB{DB: database}, nil
}
```

Create `turing-backend/orchestrator-go/internal/db/migrations.go`:

```go
package db

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed schema/*.sql
var migrationFS embed.FS

func ApplyMigrations(ctx context.Context, database *DB) error {
	if _, err := database.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	entries, err := migrationFS.ReadDir("schema")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		var exists int
		if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		sqlText, err := migrationFS.ReadFile("schema/" + name)
		if err != nil {
			return err
		}
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(sqlText)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("%s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, applied_at) VALUES (?, datetime('now'))`, version); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 6: Add repository methods used by services**

Create `turing-backend/orchestrator-go/internal/repository/sessions.go`, `events.go`, `runs.go`, and `jobs.go` with one `Repository` type. Include these exact exported types and methods:

```go
package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/db"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/ids"
)

type Repository struct {
	db *db.DB
}

func New(database *db.DB) *Repository {
	return &Repository{db: database}
}

type Session struct {
	SessionID string
	Title     sql.NullString
	Status    string
	CreatedAt string
	UpdatedAt string
}

type Message struct {
	MessageID string
	Role      string
	Content   string
	ContentType string
	Sequence  int64
	CreatedAt string
}

type EnqueueUserMessageInput struct {
	SessionID     string
	Content       string
	AgentID       string
	ModelProvider string
	Model         string
}

type EnqueueUserMessageResult struct {
	SessionID          string
	UserMessageID     string
	AssistantMessageID string
	RunID              string
	JobID              string
	TraceID            string
	Status             string
}

type Event struct {
	EventID     string
	SessionID   string
	RunID       sql.NullString
	TraceID     string
	Sequence    int64
	Type        string
	PayloadJSON string
	CreatedAt   string
}

type AppendEventInput struct {
	SessionID   string
	RunID       string
	TraceID     string
	Type        string
	PayloadJSON string
}

type Run struct {
	RunID   string
	Status  string
	TraceID string
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func (r *Repository) CreateSession(ctx context.Context, title string) (Session, error) {
	createdAt := now()
	session := Session{SessionID: ids.New("sess"), Status: "active", CreatedAt: createdAt, UpdatedAt: createdAt}
	if title != "" {
		session.Title = sql.NullString{String: title, Valid: true}
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO sessions (id, title, created_at, updated_at) VALUES (?, ?, ?, ?)`, session.SessionID, nullableString(session.Title), createdAt, createdAt)
	return session, err
}

func (r *Repository) ListMessages(ctx context.Context, sessionID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, role, content, content_type, sequence, created_at FROM messages WHERE session_id = ? ORDER BY sequence DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reversed []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.MessageID, &msg.Role, &msg.Content, &msg.ContentType, &msg.Sequence, &msg.CreatedAt); err != nil {
			return nil, err
		}
		reversed = append(reversed, msg)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed, nil
}

func (r *Repository) EnqueueUserMessage(ctx context.Context, input EnqueueUserMessageInput) (EnqueueUserMessageResult, error) {
	createdAt := now()
	userMessageID := ids.New("msg")
	assistantMessageID := ids.New("msg")
	runID := ids.New("run")
	jobID := ids.New("job")
	traceID := ids.New("trace")
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	defer tx.Rollback()
	var next int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM messages WHERE session_id = ?`, input.SessionID).Scan(&next); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at) VALUES (?, ?, 'user', ?, 'text', ?, ?)`, userMessageID, input.SessionID, input.Content, next, createdAt); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages (id, session_id, run_id, role, content, content_type, sequence, created_at) VALUES (?, ?, ?, 'assistant', '', 'text', ?, ?)`, assistantMessageID, input.SessionID, runID, next+1, createdAt); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_runs (id, session_id, user_message_id, assistant_message_id, agent_id, trace_id, status, model_provider, model_name, created_at) VALUES (?, ?, ?, ?, ?, ?, 'queued', ?, ?, ?)`, runID, input.SessionID, userMessageID, assistantMessageID, input.AgentID, traceID, input.ModelProvider, input.Model, createdAt); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	payload, err := json.Marshal(map[string]any{
		"userText": input.Content, "sessionId": input.SessionID, "userMessageId": userMessageID,
		"assistantMessageId": assistantMessageID, "traceId": traceID, "modelProvider": input.ModelProvider, "model": input.Model,
	})
	if err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO jobs (id, run_id, agent_id, status, payload_json, created_at) VALUES (?, ?, ?, 'pending', ?, ?)`, jobID, runID, input.AgentID, string(payload), createdAt); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return EnqueueUserMessageResult{}, err
	}
	return EnqueueUserMessageResult{SessionID: input.SessionID, UserMessageID: userMessageID, AssistantMessageID: assistantMessageID, RunID: runID, JobID: jobID, TraceID: traceID, Status: "queued"}, nil
}

func (r *Repository) AppendEvent(ctx context.Context, input AppendEventInput) (Event, error) {
	createdAt := now()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback()
	var next int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM events WHERE session_id = ?`, input.SessionID).Scan(&next); err != nil {
		return Event{}, err
	}
	event := Event{EventID: ids.New("evt"), SessionID: input.SessionID, TraceID: input.TraceID, Sequence: next, Type: input.Type, PayloadJSON: input.PayloadJSON, CreatedAt: createdAt}
	var runID any
	if input.RunID != "" {
		event.RunID = sql.NullString{String: input.RunID, Valid: true}
		runID = input.RunID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO events (id, session_id, run_id, trace_id, sequence, type, payload_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, event.EventID, event.SessionID, runID, event.TraceID, event.Sequence, event.Type, event.PayloadJSON, event.CreatedAt); err != nil {
		return Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return Event{}, err
	}
	return event, nil
}

func (r *Repository) ReplayEvents(ctx context.Context, sessionID string, afterSequence int64, limit int) ([]Event, int64, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	var latest int64
	if err := r.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) FROM events WHERE session_id = ?`, sessionID).Scan(&latest); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, session_id, run_id, trace_id, sequence, type, payload_json, created_at FROM events WHERE session_id = ? AND sequence > ? ORDER BY sequence LIMIT ?`, sessionID, afterSequence, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.EventID, &event.SessionID, &event.RunID, &event.TraceID, &event.Sequence, &event.Type, &event.PayloadJSON, &event.CreatedAt); err != nil {
			return nil, 0, err
		}
		events = append(events, event)
	}
	return events, latest, rows.Err()
}

func (r *Repository) MarkRunRunning(ctx context.Context, runID string) error {
	result, err := r.db.ExecContext(ctx, `UPDATE agent_runs SET status = 'running', started_at = ? WHERE id = ? AND status = 'queued'`, now(), runID)
	if err != nil {
		return err
	}
	changed, _ := result.RowsAffected()
	if changed != 1 {
		return errors.New("run is not queued")
	}
	_, err = r.db.ExecContext(ctx, `UPDATE jobs SET status = 'in_progress', picked_up_at = ? WHERE run_id = ? AND status = 'pending'`, now(), runID)
	return err
}

func (r *Repository) CancelRun(ctx context.Context, runID string, reason string) error {
	finishedAt := now()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE agent_runs SET status = 'cancelled', cancellation_reason = ?, finished_at = ? WHERE id = ? AND status IN ('queued','running','waiting_approval')`, reason, finishedAt, runID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET status = 'cancelled', finished_at = ?, error_code = 'cancelled', error_message = ? WHERE run_id = ? AND status IN ('pending','in_progress')`, finishedAt, reason, runID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) GetRun(ctx context.Context, runID string) (Run, error) {
	var run Run
	err := r.db.QueryRowContext(ctx, `SELECT id, status, trace_id FROM agent_runs WHERE id = ?`, runID).Scan(&run.RunID, &run.Status, &run.TraceID)
	return run, err
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}
```

Add these exact exported repository method signatures in `approvals.go`, `toolcalls.go`, and `audit.go`; keep their implementations transactional and backed by the existing `approvals`, `tool_calls`, and `audit_logs` tables:

```go
type ApprovalRecord struct {
	ApprovalID string
	RunID string
	ToolCallID string
	AgentID string
	ToolName string
	ArgsJSON string
	ArgsHash string
	Status string
	ApprovalToken string
	ExpiresAt string
}

func (r *Repository) CreateApproval(ctx context.Context, runID string, toolCallID string, agentID string, toolName string, argsJSON string, argsHash string, expiresAt string) (ApprovalRecord, error)
func (r *Repository) ApproveApproval(ctx context.Context, approvalID string, approvalToken string, decidedAt string) (ApprovalRecord, error)
func (r *Repository) DenyApproval(ctx context.Context, approvalID string, decidedAt string) (ApprovalRecord, error)
func (r *Repository) ConsumeApproval(ctx context.Context, approvalID string, consumedAt string) (ApprovalRecord, error)

type ToolCallRecord struct {
	ToolCallID string
	RunID string
	Status string
	ApprovalID string
}

func (r *Repository) RecordToolCallBefore(ctx context.Context, record ToolCallRecord, agentID string, serverName string, toolName string, argsJSON string, argsHash string) error
func (r *Repository) RecordToolCallAfter(ctx context.Context, toolCallID string, runID string, status string, resultSummary string, errorCode string, errorMessage string, durationMS int64) error

func (r *Repository) RecordAudit(ctx context.Context, correlationID string, actorType string, actorID string, action string, target string, payloadJSON string) error
```

- [ ] **Step 7: Run repository tests**

Run:

```bash
cd turing-backend
go test ./orchestrator-go/internal/db ./orchestrator-go/internal/repository
```

Expected: PASS.

- [ ] **Step 8: Commit persistence foundation**

Run:

```bash
git add go.mod go.sum turing-backend/orchestrator-go/internal/db turing-backend/orchestrator-go/internal/repository
git commit -m "feat: add Go orchestrator SQLite repositories" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

## Task 4: Event bus and public session/event gRPC services

**Files:**
- Create: `turing-backend/orchestrator-go/internal/service/events/bus.go`
- Create: `turing-backend/orchestrator-go/internal/service/events/bus_test.go`
- Create: `turing-backend/orchestrator-go/internal/service/sessions/service.go`
- Create: `turing-backend/orchestrator-go/internal/service/sessions/service_test.go`
- Create: `turing-backend/orchestrator-go/internal/service/events/service.go`
- Create: `turing-backend/orchestrator-go/internal/service/events/service_test.go`

- [ ] **Step 1: Write failing tests for event bus unsubscribe and session service**

Create `turing-backend/orchestrator-go/internal/service/events/bus_test.go`:

```go
package events

import (
	"testing"
	"time"
)

func TestBusPublishesOnlyMatchingSessionAndUnsubscribes(t *testing.T) {
	bus := NewBus(8)
	ch, unsubscribe := bus.Subscribe("sess_1")
	bus.Publish(Event{SessionID: "sess_2", Sequence: 1})
	select {
	case got := <-ch:
		t.Fatalf("unexpected event: %+v", got)
	default:
	}
	bus.Publish(Event{SessionID: "sess_1", Sequence: 2})
	select {
	case got := <-ch:
		if got.Sequence != 2 {
			t.Fatalf("sequence = %d", got.Sequence)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
	unsubscribe()
	bus.Publish(Event{SessionID: "sess_1", Sequence: 3})
	select {
	case got, ok := <-ch:
		if ok {
			t.Fatalf("received after unsubscribe: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("channel did not close")
	}
}
```

Create `turing-backend/orchestrator-go/internal/service/sessions/service_test.go` with a bufconn gRPC server that calls `CreateSession`, `ListMessages`, `GetConfig`, `ListAgents`, and `ListTools`. Assert `ListAgents` returns `AGENT_ID_GENERAL_ASSISTANT` and `ListTools` returns `system.time` and `files.create`.

- [ ] **Step 2: Run service tests to verify they fail**

Run:

```bash
cd turing-backend
go test ./orchestrator-go/internal/service/events ./orchestrator-go/internal/service/sessions
```

Expected: FAIL because services do not exist.

- [ ] **Step 3: Implement focused event bus**

Create `turing-backend/orchestrator-go/internal/service/events/bus.go`:

```go
package events

import "sync"

type Event struct {
	SessionID   string
	RunID       string
	TraceID     string
	Sequence    int64
	Type        string
	PayloadJSON string
}

type Bus struct {
	mu         sync.Mutex
	bufferSize int
	nextID     int64
	subs       map[int64]subscription
}

type subscription struct {
	sessionID string
	ch        chan Event
}

func NewBus(bufferSize int) *Bus {
	return &Bus{bufferSize: bufferSize, subs: map[int64]subscription{}}
}

func (b *Bus) Subscribe(sessionID string) (<-chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nextID++
	id := b.nextID
	ch := make(chan Event, b.bufferSize)
	b.subs[id] = subscription{sessionID: sessionID, ch: ch}
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		sub, ok := b.subs[id]
		if !ok {
			return
		}
		delete(b.subs, id)
		close(sub.ch)
	}
}

func (b *Bus) Publish(event Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, sub := range b.subs {
		if sub.sessionID != event.SessionID {
			continue
		}
		select {
		case sub.ch <- event:
		default:
		}
	}
}
```

- [ ] **Step 4: Implement session and event gRPC services**

Create `turing-backend/orchestrator-go/internal/service/sessions/service.go` with a `Server` struct that embeds `turingv1.UnimplementedSessionServiceServer`, accepts a repository and config, validates request fields, and maps repository rows to generated protobuf messages. Use these exact static outputs:

```go
agents := []*turingv1.AgentDescriptor{{Id: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, DisplayName: "General Assistant"}}
tools := []*turingv1.ToolDescriptor{
	{ServerName: "system", ToolName: "system.time", Policy: turingv1.ToolPolicy_TOOL_POLICY_SAFE},
	{ServerName: "files", ToolName: "files.create", Policy: turingv1.ToolPolicy_TOOL_POLICY_APPROVAL_REQUIRED},
}
```

Create `turing-backend/orchestrator-go/internal/service/events/service.go` with `ListEvents` and `SubscribeSessionEvents`. `SubscribeSessionEvents` must:

```go
events, _, err := s.repo.ReplayEvents(ctx, req.SessionId, req.AfterSequence, 500)
for _, event := range events {
	if err := stream.Send(mapEvent(event)); err != nil {
		return err
	}
}
ch, unsubscribe := s.bus.Subscribe(req.SessionId)
defer unsubscribe()
for {
	select {
	case <-ctx.Done():
		return status.Error(codes.Canceled, "client cancelled event stream")
	case event, ok := <-ch:
		if !ok {
			return nil
		}
		if event.Sequence <= req.AfterSequence {
			continue
		}
		if err := stream.Send(mapBusEvent(event)); err != nil {
			return err
		}
	}
}
```

- [ ] **Step 5: Run session/event service tests**

Run:

```bash
cd turing-backend
go test ./orchestrator-go/internal/service/events ./orchestrator-go/internal/service/sessions
```

Expected: PASS.

- [ ] **Step 6: Commit public read/query services**

Run:

```bash
git add turing-backend/orchestrator-go/internal/service/events turing-backend/orchestrator-go/internal/service/sessions
git commit -m "feat: add Go session and event gRPC services" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

## Task 5: Runtime worker stream and cancellation command path

**Files:**
- Create: `turing-backend/orchestrator-go/internal/service/runtime/service.go`
- Create: `turing-backend/orchestrator-go/internal/service/runtime/service_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/jobs.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/runs.go`

- [ ] **Step 1: Write failing worker stream tests**

Create `turing-backend/orchestrator-go/internal/service/runtime/service_test.go`:

```go
package runtime

import (
	"context"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
)

func TestAssignsPendingJobToReadyWorker(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSessionAndRun(t, "hello")
	_ = sessionID
	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-1", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	cmd, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if cmd.GetRunAssigned() == nil {
		t.Fatalf("command = %T, want run_assigned", cmd.Command)
	}
}

func TestCancelRunSendsRuntimeCommand(t *testing.T) {
	h := newHarness(t)
	runID := h.createRunningRun(t, "cancel me")
	client := h.runtimeClient(t)
	stream, err := client.ConnectWorker(h.internalContext())
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&turingv1.RuntimeUpdate{Update: &turingv1.RuntimeUpdate_WorkerReady{WorkerReady: &turingv1.RuntimeWorkerReady{WorkerId: "worker-1", AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, MaxConcurrentRuns: 1}}}); err != nil {
		t.Fatal(err)
	}
	h.service.CancelRun(context.Background(), runID, "client_cancelled")
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for cancellation command")
		default:
			cmd, err := stream.Recv()
			if err != nil {
				t.Fatal(err)
			}
			if cancel := cmd.GetRunCancelled(); cancel != nil && cancel.RunId == runID {
				return
			}
		}
	}
}
```

The helper `newHarness` must create an in-memory gRPC server with internal-token metadata and a temp SQLite database. Keep it in this test file so the runtime service tests are self-contained.

- [ ] **Step 2: Run worker stream tests to verify they fail**

Run:

```bash
cd turing-backend
go test ./orchestrator-go/internal/service/runtime
```

Expected: FAIL because runtime service does not exist.

- [ ] **Step 3: Implement runtime service**

Create `turing-backend/orchestrator-go/internal/service/runtime/service.go` with:

```go
package runtime

import (
	"context"
	"sync"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	turingv1.UnimplementedRuntimeServiceServer
	repo *repository.Repository
	mu sync.Mutex
	workers map[string]chan *turingv1.RuntimeCommand
}

func New(repo *repository.Repository) *Server {
	return &Server{repo: repo, workers: map[string]chan *turingv1.RuntimeCommand{}}
}

func (s *Server) ConnectWorker(stream turingv1.RuntimeService_ConnectWorkerServer) error {
	ctx := stream.Context()
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	ready := first.GetWorkerReady()
	if ready == nil || ready.WorkerId == "" || ready.AgentId != turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT {
		return status.Error(codes.InvalidArgument, "worker_ready is required")
	}
	commands := make(chan *turingv1.RuntimeCommand, 8)
	s.mu.Lock()
	s.workers[ready.WorkerId] = commands
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.workers, ready.WorkerId)
		close(commands)
		s.mu.Unlock()
	}()
	if err := stream.Send(&turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_WorkerAccepted{WorkerAccepted: &turingv1.RuntimeWorkerAccepted{WorkerId: ready.WorkerId}}}); err != nil {
		return err
	}
	if job, err := s.repo.ClaimNextJob(ctx, "general_assistant", ready.WorkerId); err == nil && job.JobID != "" {
		commands <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunAssigned{RunAssigned: mapJob(job)}}
	}
	recvErr := make(chan error, 1)
	go func() {
		for {
			update, err := stream.Recv()
			if err != nil {
				recvErr <- err
				return
			}
			if err := s.applyUpdate(ctx, update); err != nil {
				recvErr <- err
				return
			}
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return status.Error(codes.Canceled, "worker stream cancelled")
		case err := <-recvErr:
			return err
		case cmd := <-commands:
			if err := stream.Send(cmd); err != nil {
				return err
			}
		}
	}
}

func (s *Server) CancelRun(ctx context.Context, runID string, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, commands := range s.workers {
		select {
		case commands <- &turingv1.RuntimeCommand{Command: &turingv1.RuntimeCommand_RunCancelled{RunCancelled: &turingv1.RuntimeRunCancelled{RunId: runID, Reason: reason}}}:
		default:
		}
	}
}
```

Add these private helpers in the same file:

```go
func (s *Server) applyUpdate(ctx context.Context, update *turingv1.RuntimeUpdate) error {
	switch value := update.Update.(type) {
	case *turingv1.RuntimeUpdate_Event:
		return s.repo.AppendRuntimeEvent(ctx, value.Event)
	case *turingv1.RuntimeUpdate_ToolBeacon:
		_, err := s.handleToolBeacon(ctx, value.ToolBeacon)
		return err
	case *turingv1.RuntimeUpdate_RunCompleted:
		return s.repo.CompleteRun(ctx, value.RunCompleted.RunId, value.RunCompleted.AssistantMessageId, value.RunCompleted.Content)
	case *turingv1.RuntimeUpdate_RunFailed:
		return s.repo.FailRun(ctx, value.RunFailed.RunId, value.RunFailed.Code, value.RunFailed.Message)
	case *turingv1.RuntimeUpdate_RunCancelledAck:
		return nil
	default:
		return status.Error(codes.InvalidArgument, "unsupported runtime update")
	}
}

func mapJob(job repository.Job) *turingv1.AgentJob {
	provider := turingv1.ModelProvider_MODEL_PROVIDER_UNSPECIFIED
	if job.ModelProvider == "ollama" {
		provider = turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA
	}
	if job.ModelProvider == "openai_compatible" {
		provider = turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE
	}
	return &turingv1.AgentJob{
		JobId: job.JobID, RunId: job.RunID, SessionId: job.SessionID,
		UserMessageId: job.UserMessageID, AssistantMessageId: job.AssistantMessageID,
		AgentId: turingv1.AgentId_AGENT_ID_GENERAL_ASSISTANT, TraceId: job.TraceID,
		ModelProvider: provider, Model: job.Model, UserText: job.UserText, Attempt: int32(job.Attempt),
	}
}
```

Add `ClaimNextJob` and `Job` to `repository/jobs.go`. It must atomically update `jobs.status` to `in_progress`, set `lease_owner`, set `picked_up_at`, update `agent_runs.status` to `running`, and return one pending job ordered by `jobs.created_at`.

- [ ] **Step 4: Run worker stream tests**

Run:

```bash
cd turing-backend
go test ./orchestrator-go/internal/service/runtime ./orchestrator-go/internal/repository
```

Expected: PASS.

- [ ] **Step 5: Commit runtime service**

Run:

```bash
git add turing-backend/orchestrator-go/internal/service/runtime turing-backend/orchestrator-go/internal/repository
git commit -m "feat: add internal runtime worker stream" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

## Task 6: ChatService SendMessage server-streaming

**Files:**
- Create: `turing-backend/orchestrator-go/internal/service/chat/service.go`
- Create: `turing-backend/orchestrator-go/internal/service/chat/service_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/events/bus.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/service.go`

- [ ] **Step 1: Write failing SendMessage streaming and cancellation tests**

Create `turing-backend/orchestrator-go/internal/service/chat/service_test.go`:

```go
package chat

import (
	"context"
	"io"
	"testing"
	"time"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSendMessageStreamsQueuedEvent(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	stream, err := h.chatClient.SendMessage(h.clientContext(), &turingv1.SendMessageRequest{
		SessionId: sessionID,
		Content: "hello",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	event, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if event.GetRunQueued() == nil {
		t.Fatalf("first event = %T, want run_queued", event.Event)
	}
}

func TestSendMessageCancellationCancelsRun(t *testing.T) {
	h := newHarness(t)
	sessionID := h.createSession(t)
	ctx, cancel := context.WithCancel(h.clientContext())
	stream, err := h.chatClient.SendMessage(ctx, &turingv1.SendMessageRequest{
		SessionId: sessionID,
		Content: "cancel this",
		ModelProvider: turingv1.ModelProvider_MODEL_PROVIDER_OLLAMA,
		Model: "llama3.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := stream.Recv()
	if err != nil {
		t.Fatal(err)
	}
	runID := first.GetRunQueued().RunId
	cancel()
	_, err = stream.Recv()
	if status.Code(err) != codes.Canceled && err != io.EOF {
		t.Fatalf("Recv after cancel = %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		run, err := h.repo.GetRun(context.Background(), runID)
		if err == nil && run.Status == "cancelled" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("run was not cancelled")
}
```

- [ ] **Step 2: Run chat tests to verify they fail**

Run:

```bash
cd turing-backend
go test ./orchestrator-go/internal/service/chat
```

Expected: FAIL because `ChatService` does not exist.

- [ ] **Step 3: Implement SendMessage with stream context cleanup**

Create `turing-backend/orchestrator-go/internal/service/chat/service.go`:

```go
package chat

import (
	"context"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/repository"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/events"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/service/runtime"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	turingv1.UnimplementedChatServiceServer
	repo *repository.Repository
	bus *events.Bus
	runtime *runtime.Server
	ollamaModel string
	openAIModel string
}

func New(repo *repository.Repository, bus *events.Bus, runtimeServer *runtime.Server, ollamaModel string, openAIModel string) *Server {
	return &Server{repo: repo, bus: bus, runtime: runtimeServer, ollamaModel: ollamaModel, openAIModel: openAIModel}
}

func (s *Server) SendMessage(req *turingv1.SendMessageRequest, stream turingv1.ChatService_SendMessageServer) error {
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()
	if req.SessionId == "" {
		return status.Error(codes.InvalidArgument, "session_id is required")
	}
	if req.Content == "" {
		return status.Error(codes.InvalidArgument, "content is required")
	}
	modelProvider := "ollama"
	if req.ModelProvider == turingv1.ModelProvider_MODEL_PROVIDER_OPENAI_COMPATIBLE {
		modelProvider = "openai_compatible"
	}
	model := req.Model
	if model == "" && modelProvider == "ollama" {
		model = s.ollamaModel
	}
	if model == "" && modelProvider == "openai_compatible" {
		model = s.openAIModel
	}
	enqueued, err := s.repo.EnqueueUserMessage(ctx, repository.EnqueueUserMessageInput{
		SessionID: req.SessionId, Content: req.Content, AgentID: "general_assistant", ModelProvider: modelProvider, Model: model,
	})
	if err != nil {
		return status.Error(codes.NotFound, "session not found")
	}
	event, err := s.repo.AppendEvent(ctx, repository.AppendEventInput{
		SessionID: req.SessionId, RunID: enqueued.RunID, TraceID: enqueued.TraceID, Type: "agent.run.queued",
		PayloadJSON: `{"status":"queued"}`,
	})
	if err != nil {
		return status.Error(codes.Internal, "append queued event failed")
	}
	s.bus.Publish(events.Event{SessionID: event.SessionID, RunID: enqueued.RunID, TraceID: event.TraceID, Sequence: event.Sequence, Type: event.Type, PayloadJSON: event.PayloadJSON})
	ch, unsubscribe := s.bus.Subscribe(req.SessionId)
	defer unsubscribe()
	if err := stream.Send(&turingv1.ChatStreamEvent{
		SessionId: req.SessionId, RunId: enqueued.RunID, TraceId: enqueued.TraceID, Sequence: event.Sequence,
		Event: &turingv1.ChatStreamEvent_RunQueued{RunQueued: &turingv1.RunQueued{RunId: enqueued.RunID, JobId: enqueued.JobID, TraceId: enqueued.TraceID}},
	}); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			_ = s.repo.CancelRun(context.Background(), enqueued.RunID, "client_cancelled")
			s.runtime.CancelRun(context.Background(), enqueued.RunID, "client_cancelled")
			return status.Error(codes.Canceled, "client cancelled stream")
		case event, ok := <-ch:
			if !ok {
				return nil
			}
			if event.RunID != enqueued.RunID {
				continue
			}
			converted := mapChatEvent(event)
			if err := stream.Send(converted); err != nil {
				return err
			}
			switch event.Type {
			case "message.completed", "agent.run.completed", "agent.run.failed", "agent.run.cancelled":
				return nil
			}
		}
	}
}
```

Implement `mapChatEvent` in the same file. It must convert:

- `message.delta` with payload field `delta` to `TokenDelta`
- `message.completed` with payload field `content` to `MessageCompleted`
- `agent.run.completed` to `RunCompleted`
- `agent.run.failed` to `RunFailed`
- `agent.run.cancelled` to `RunCancelled`
- unknown persisted events to `persisted_event`

Use `safejson.DecodeObject` to parse payload JSON and return a `RunFailed` event if payload parsing fails.

- [ ] **Step 4: Run chat tests**

Run:

```bash
cd turing-backend
go test ./orchestrator-go/internal/service/chat ./orchestrator-go/internal/service/runtime
```

Expected: PASS.

- [ ] **Step 5: Commit ChatService**

Run:

```bash
git add turing-backend/orchestrator-go/internal/service/chat turing-backend/orchestrator-go/internal/service/events turing-backend/orchestrator-go/internal/service/runtime
git commit -m "feat: add streaming chat gRPC service" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

## Task 7: Approvals, tool policy, audit, and dynamic payload handling

**Files:**
- Create: `turing-backend/orchestrator-go/internal/service/tools/policy.go`
- Create: `turing-backend/orchestrator-go/internal/service/tools/policy_test.go`
- Create: `turing-backend/orchestrator-go/internal/service/approvals/service.go`
- Create: `turing-backend/orchestrator-go/internal/service/approvals/service_test.go`
- Create: `turing-backend/orchestrator-go/internal/service/audit/service.go`
- Create: `turing-backend/orchestrator-go/internal/service/audit/service_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/service.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/approvals.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/toolcalls.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/audit.go`

- [ ] **Step 1: Write failing tests for policy and approval transitions**

Create `turing-backend/orchestrator-go/internal/service/tools/policy_test.go`:

```go
package tools

import "testing"

func TestPolicyForKnownTools(t *testing.T) {
	cases := map[string]Policy{
		"system.time": PolicySafe,
		"files.create": PolicyApprovalRequired,
		"files.update": PolicyApprovalRequired,
	}
	for name, want := range cases {
		got, ok := GetPolicy(name)
		if !ok || got != want {
			t.Fatalf("GetPolicy(%q) = %q/%v, want %q/true", name, got, ok, want)
		}
	}
}

func TestUnknownToolIsDenied(t *testing.T) {
	if _, ok := GetPolicy("system.shell"); ok {
		t.Fatal("unknown tool should not have a policy")
	}
}
```

Create `turing-backend/orchestrator-go/internal/service/approvals/service_test.go` that covers:

- creating approval-required beacon returns `DECISION_APPROVAL_REQUIRED`
- approving a pending approval returns `APPROVAL_STATUS_APPROVED`
- denying a pending approval returns `APPROVAL_STATUS_DENIED`
- approving an expired approval returns `codes.FailedPrecondition`

- [ ] **Step 2: Run approval/tool tests to verify they fail**

Run:

```bash
cd turing-backend
go test ./orchestrator-go/internal/service/tools ./orchestrator-go/internal/service/approvals ./orchestrator-go/internal/service/audit
```

Expected: FAIL because services do not exist.

- [ ] **Step 3: Implement tool policy**

Create `turing-backend/orchestrator-go/internal/service/tools/policy.go`:

```go
package tools

type Policy string

const (
	PolicySafe Policy = "safe"
	PolicyApprovalRequired Policy = "approval_required"
	PolicyDisabled Policy = "disabled"
)

var policies = map[string]Policy{
	"system.time":  PolicySafe,
	"system.health": PolicySafe,
	"system.echo": PolicySafe,
	"files.create": PolicyApprovalRequired,
	"files.update": PolicyApprovalRequired,
}

func GetPolicy(toolName string) (Policy, bool) {
	policy, ok := policies[toolName]
	return policy, ok
}
```

- [ ] **Step 4: Implement approval and audit services**

Create `turing-backend/orchestrator-go/internal/service/approvals/service.go` with a public gRPC `ApprovalService` and internal helper methods:

```go
func (s *Server) ApproveApproval(ctx context.Context, req *turingv1.ApproveApprovalRequest) (*turingv1.ApprovalResponse, error)
func (s *Server) DenyApproval(ctx context.Context, req *turingv1.DenyApprovalRequest) (*turingv1.ApprovalResponse, error)
func (s *Server) CreateApprovalForTool(ctx context.Context, runID string, toolCallID string, agentID string, toolName string, args map[string]any) (approvalID string, err error)
```

Use HMAC SHA-256 JWT signing for approval tokens with:

- issuer `turing.orchestrator`
- audience `mcp-files`
- subject `general_assistant`
- `tool`
- `args_hash`
- 60 second expiration

Create `turing-backend/orchestrator-go/internal/service/audit/service.go` with:

```go
func (s *Server) Record(ctx context.Context, correlationID string, actorType string, actorID string, action string, target string, payload map[string]any) error
```

All payloads must pass through `safejson.Normalize` and store canonical JSON.

- [ ] **Step 5: Wire runtime tool beacons through policy, approvals, and audit**

Modify `turing-backend/orchestrator-go/internal/service/runtime/service.go` so `applyUpdate` handles `ToolCallBeacon`:

```go
func (s *Server) handleToolBeacon(ctx context.Context, beacon *turingv1.ToolCallBeacon) (*turingv1.ToolPolicyDecision, error) {
	policy, ok := tools.GetPolicy(beacon.ToolName)
	if !ok {
		return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_DENY, ToolCallId: beacon.ToolCallId, Reason: "unknown_tool"}, nil
	}
	if beacon.Phase == turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE && policy == tools.PolicySafe {
		return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW, ToolCallId: beacon.ToolCallId}, nil
	}
	if beacon.Phase == turingv1.ToolCallPhase_TOOL_CALL_PHASE_BEFORE && policy == tools.PolicyApprovalRequired {
		approvalID, err := s.approvals.CreateApprovalForTool(ctx, beacon.RunId, beacon.ToolCallId, "general_assistant", beacon.ToolName, beacon.Args.AsMap())
		if err != nil {
			return nil, err
		}
		return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_APPROVAL_REQUIRED, ToolCallId: beacon.ToolCallId, ApprovalId: approvalID}, nil
	}
	return &turingv1.ToolPolicyDecision{Decision: turingv1.ToolPolicyDecision_DECISION_ALLOW, ToolCallId: beacon.ToolCallId}, nil
}
```

Store before/after tool-call records in `tool_calls`, append corresponding canonical events, publish them on the bus, and record audit entries.

- [ ] **Step 6: Run approval/tool/audit tests**

Run:

```bash
cd turing-backend
go test ./orchestrator-go/internal/service/tools ./orchestrator-go/internal/service/approvals ./orchestrator-go/internal/service/audit ./orchestrator-go/internal/service/runtime
```

Expected: PASS.

- [ ] **Step 7: Commit approvals and tool policy**

Run:

```bash
git add turing-backend/orchestrator-go/internal/service/tools turing-backend/orchestrator-go/internal/service/approvals turing-backend/orchestrator-go/internal/service/audit turing-backend/orchestrator-go/internal/service/runtime turing-backend/orchestrator-go/internal/repository
git commit -m "feat: add Go approvals and tool policy flow" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

## Task 8: Go orchestrator server, interceptors, and Docker image

**Files:**
- Create: `turing-backend/orchestrator-go/internal/app/app.go`
- Create: `turing-backend/orchestrator-go/internal/app/app_test.go`
- Create: `turing-backend/orchestrator-go/cmd/server/main.go`
- Create: `turing-backend/orchestrator-go/Dockerfile`
- Modify: `turing-backend/.env.example`

- [ ] **Step 1: Write failing app/server tests**

Create `turing-backend/orchestrator-go/internal/app/app_test.go`:

```go
package app

import (
	"context"
	"net"
	"testing"

	turingv1 "github.com/mcasillas17/TuringAgent/gen/turing/v1/go/turing/v1"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

func TestPublicServerRequiresClientToken(t *testing.T) {
	cfg := config.Config{ClientAPIKey: "client", InternalToken: "internal", DatabasePath: t.TempDir() + "/turing.db", OllamaModel: "llama3.2", OpenAIModel: "gpt-4o-mini"}
	app, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1024 * 1024)
	go func() { _ = app.PublicServer.Serve(lis) }()
	t.Cleanup(app.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	client := turingv1.NewHealthServiceClient(conn)
	if _, err := client.Check(context.Background(), &turingv1.HealthCheckRequest{}); err == nil {
		t.Fatal("expected unauthenticated error")
	}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer client"))
	res, err := client.Check(ctx, &turingv1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Ok {
		t.Fatal("health check was not ok")
	}
}
```

- [ ] **Step 2: Run app tests to verify they fail**

Run:

```bash
cd turing-backend
go test ./orchestrator-go/internal/app
```

Expected: FAIL because app package does not exist.

- [ ] **Step 3: Implement app wiring**

Create `turing-backend/orchestrator-go/internal/app/app.go` that:

- opens SQLite and applies migrations;
- creates repositories, event bus, runtime service, session service, event service, chat service, approval service, audit service, and health service;
- creates one public gRPC server with client API key interceptors;
- creates one internal gRPC server with internal token interceptors;
- registers public services on the public server;
- registers `RuntimeService` on the internal server;
- sets max receive/send message sizes to 4 MiB;
- exposes `Stop()` that gracefully stops both servers and closes the DB.

Create a health service in this file or a small `internal/service/health/service.go`:

```go
func (s *HealthServer) Check(context.Context, *turingv1.HealthCheckRequest) (*turingv1.HealthCheckResponse, error) {
	return &turingv1.HealthCheckResponse{Ok: true}, nil
}

func (s *HealthServer) Version(context.Context, *turingv1.VersionRequest) (*turingv1.VersionResponse, error) {
	return &turingv1.VersionResponse{Version: "1.0.0-go", SchemaVersion: "0002"}, nil
}
```

- [ ] **Step 4: Implement server main and Dockerfile**

Create `turing-backend/orchestrator-go/cmd/server/main.go`:

```go
package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/app"
	"github.com/mcasillas17/TuringAgent/turing-backend/orchestrator-go/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	application, err := app.New(cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer application.Stop()

	publicListener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.PublicPort))
	if err != nil {
		log.Fatal(err)
	}
	internalListener, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.InternalPort))
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		log.Printf("public gRPC listening on %s", publicListener.Addr())
		if err := application.PublicServer.Serve(publicListener); err != nil {
			log.Printf("public server stopped: %v", err)
		}
	}()
	go func() {
		log.Printf("internal gRPC listening on %s", internalListener.Addr())
		if err := application.InternalServer.Serve(internalListener); err != nil {
			log.Printf("internal server stopped: %v", err)
		}
	}()

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	<-done
}
```

Create `turing-backend/orchestrator-go/Dockerfile`:

```Dockerfile
FROM golang:1.23-bookworm AS build
WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends gcc libc6-dev sqlite3 libsqlite3-dev && rm -rf /var/lib/apt/lists/*
COPY go.mod go.sum ./
RUN go mod download
COPY gen ./gen
COPY turing-backend ./turing-backend
RUN CGO_ENABLED=1 go build -o /out/turing-orchestrator-go ./turing-backend/orchestrator-go/cmd/server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates sqlite3 libsqlite3-0 && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/turing-orchestrator-go /app/turing-orchestrator-go
EXPOSE 3000 3001
ENTRYPOINT ["/app/turing-orchestrator-go"]
```

- [ ] **Step 5: Run app tests and build orchestrator**

Run:

```bash
go test ./turing-backend/orchestrator-go/...
go build ./turing-backend/orchestrator-go/cmd/server
```

Expected: PASS and successful build.

- [ ] **Step 6: Commit orchestrator server**

Run:

```bash
git add turing-backend/orchestrator-go turing-backend/.env.example
git commit -m "feat: wire Go orchestrator gRPC server" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

## Task 9: Go agent-runtime model and MCP execution

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `turing-backend/agent-runtime-go/internal/config/config.go`
- Create: `turing-backend/agent-runtime-go/internal/orchestrator/client.go`
- Create: `turing-backend/agent-runtime-go/internal/worker/worker.go`
- Create: `turing-backend/agent-runtime-go/internal/agent/general_assistant.go`
- Create: `turing-backend/agent-runtime-go/internal/llm/provider.go`
- Create: `turing-backend/agent-runtime-go/internal/llm/ollama.go`
- Create: `turing-backend/agent-runtime-go/internal/llm/openai_compatible.go`
- Create: `turing-backend/agent-runtime-go/internal/mcp/client.go`
- Create: `turing-backend/agent-runtime-go/internal/tools/runner.go`
- Create: `turing-backend/agent-runtime-go/cmd/runtime/main.go`
- Create: `turing-backend/agent-runtime-go/Dockerfile`
- Create: `turing-backend/agent-runtime-go/internal/llm/ollama_test.go`
- Create: `turing-backend/agent-runtime-go/internal/llm/openai_compatible_test.go`
- Create: `turing-backend/agent-runtime-go/internal/mcp/client_test.go`
- Create: `turing-backend/agent-runtime-go/internal/worker/worker_test.go`
- Create: `turing-backend/agent-runtime-go/internal/agent/general_assistant_test.go`

- [ ] **Step 1: Write failing runtime tests**

Create `turing-backend/agent-runtime-go/internal/llm/ollama_test.go` with:

```go
func TestOllamaStreamChatParsesDeltaAndCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":{"content":"Hel"},"done":false}` + "\n"))
		w.Write([]byte(`{"done":true,"done_reason":"stop"}` + "\n"))
	}))
	t.Cleanup(server.Close)
	provider := NewOllama(server.URL, server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "llama3.2", Messages: []ChatMessage{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if got[0].Text != "Hel" || got[1].Type != "completed" {
		t.Fatalf("events = %+v", got)
	}
}

func TestOllamaStreamChatMalformedJSONReturnsErrorEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":` + "\n"))
	}))
	t.Cleanup(server.Close)
	provider := NewOllama(server.URL, server.Client())
	events, err := provider.StreamChat(context.Background(), ChatRequest{Model: "llama3.2"})
	if err != nil {
		t.Fatal(err)
	}
	got := collectEvents(events)
	if got[0].Code != "model_bad_chunk" {
		t.Fatalf("code = %q", got[0].Code)
	}
}
```

Create `turing-backend/agent-runtime-go/internal/llm/openai_compatible_test.go` with an SSE server that writes `data: {"choices":[{"delta":{"content":"Hi"}}]}` and `data: [DONE]`, then asserts a delta followed by completion.

Create `turing-backend/agent-runtime-go/internal/mcp/client_test.go` with a JSON-RPC fake server that returns `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"denied"}}`; assert `CallTool` returns an error containing `denied`.

Create `turing-backend/agent-runtime-go/internal/worker/worker_test.go` with the cancellation fake provider shown below.

The cancellation test must use a fake provider:

```go
type blockingProvider struct {
	started chan struct{}
	cancelled chan struct{}
}

func (p *blockingProvider) StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	close(p.started)
	out := make(chan llm.StreamEvent)
	go func() {
		defer close(out)
		<-ctx.Done()
		close(p.cancelled)
	}()
	return out, nil
}
```

- [ ] **Step 2: Run runtime tests to verify they fail**

Run:

```bash
cd turing-backend
go test ./agent-runtime-go/...
```

Expected: FAIL because runtime packages do not exist.

- [ ] **Step 3: Implement LLM providers with safe streaming parsers**

Create `provider.go`:

```go
package llm

import "context"

type ChatMessage struct {
	Role string
	Content string
}

type ChatRequest struct {
	Model string
	Messages []ChatMessage
	Temperature float64
	MaxTokens int
}

type StreamEvent struct {
	Type string
	Text string
	FinishReason string
	Code string
	Message string
}

type Provider interface {
	ID() string
	StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
}
```

In `ollama.go`, read response bodies with `bufio.Scanner`, increase the scanner buffer to 1 MiB, decode each line with `json.Decoder.UseNumber`, and validate:

```go
message, _ := obj["message"].(map[string]any)
content, _ := message["content"].(string)
done, _ := obj["done"].(bool)
```

Never call chained unchecked assertions such as `obj["message"].(map[string]any)["content"].(string)`.

In `openai_compatible.go`, parse `data:` SSE lines, handle `[DONE]`, decode JSON into a narrow struct with `json.RawMessage`, and validate `choices[0].delta.content` with explicit length checks.

- [ ] **Step 4: Add the errgroup dependency**

Run from the repository root before creating the tool runner:

```bash
go get golang.org/x/sync@v0.10.0
go mod tidy
```

Expected: root `go.mod` includes `golang.org/x/sync v0.10.0` because `tools/runner.go` imports `golang.org/x/sync/errgroup` in this task.

- [ ] **Step 5: Implement MCP client and authorized tool runner**

Create `mcp/client.go` that:

- accepts context for every call;
- sends JSON-RPC 2.0 HTTP POST;
- applies a max response size;
- decodes with `safejson.DecodeObject`;
- validates `error.message` as string before returning;
- returns result as `map[string]any` or an empty map.

Create `tools/runner.go` that:

- creates a `call_` ID;
- sends before beacon over internal gRPC;
- handles allow, deny, approval-required;
- includes approval token under `_meta.approvalToken`;
- sends after beacon with `completed`, `failed`, or `denied`;
- uses `errgroup.WithContext` for any concurrent tool metadata fetches.

- [ ] **Step 6: Implement worker and agent executor**

Create `worker/worker.go` that:

- dials orchestrator internal gRPC with authorization metadata;
- opens `ConnectWorker`;
- sends `worker_ready`;
- stores active run cancel functions in `map[string]context.CancelFunc` guarded by a mutex;
- starts one goroutine per assigned run;
- on `run_cancelled`, calls the matching cancel function;
- waits for run goroutine exit before deleting map state;
- sends `run_completed`, `run_failed`, and `run_cancelled_ack`.

Create `agent/general_assistant.go` that:

- fetches messages from orchestrator through a typed runtime client method;
- emits `message.started`;
- appends provider delta text to final content;
- emits `message.delta` for each token;
- emits `message.completed` and `run_completed`;
- emits `run_failed` on provider or tool errors.

Create `turing-backend/agent-runtime-go/Dockerfile` for repository-root build context:

```Dockerfile
FROM golang:1.23-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY gen ./gen
COPY turing-backend ./turing-backend
RUN CGO_ENABLED=0 go build -o /out/turing-agent-runtime-go ./turing-backend/agent-runtime-go/cmd/runtime

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/turing-agent-runtime-go /app/turing-agent-runtime-go
ENTRYPOINT ["/app/turing-agent-runtime-go"]
```

- [ ] **Step 7: Run runtime package tests**

Run:

```bash
go test ./turing-backend/agent-runtime-go/...
go build ./turing-backend/agent-runtime-go/cmd/runtime
```

Expected: PASS and successful build.

- [ ] **Step 8: Commit Go agent runtime**

Run:

```bash
git add go.mod go.sum turing-backend/agent-runtime-go
git commit -m "feat: add Go agent runtime worker" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

## Task 10: End-to-end Go gRPC integration tests

**Files:**
- Create: `turing-backend/tests/grpc_harness_test.go`
- Create: `turing-backend/tests/cancellation_test.go`
- Create: `turing-backend/tests/parity_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Write failing integration tests**

Create tests with fake model and fake MCP HTTP servers:

- `TestSendMessageStreamsTokensToCompletion`
- `TestApprovalRequiredToolFlow`
- `TestSubscribeSessionEventsReplaysAfterSequence`
- `TestClientCancellationStopsRuntimeAndModel`
- `TestParityForSessionMessageEventShapes`

The cancellation assertion must include:

```go
select {
case <-fakeModel.cancelled:
case <-time.After(2 * time.Second):
	t.Fatal("model request was not cancelled")
}
run, err := harness.repo.GetRun(context.Background(), runID)
if err != nil {
	t.Fatal(err)
}
if run.Status != "cancelled" {
	t.Fatalf("run status = %q, want cancelled", run.Status)
}
```

- [ ] **Step 2: Run integration tests to verify failures**

Run:

```bash
cd turing-backend
go test ./tests -run 'TestSendMessageStreamsTokensToCompletion|TestApprovalRequiredToolFlow|TestSubscribeSessionEventsReplaysAfterSequence|TestClientCancellationStopsRuntimeAndModel|TestParityForSessionMessageEventShapes'
```

Expected: FAIL until harness wiring is implemented.

- [ ] **Step 3: Implement the integration harness**

Create `grpc_harness_test.go` that:

- starts orchestrator public and internal gRPC servers with `bufconn`;
- starts the runtime worker against the internal `bufconn`;
- uses fake model provider channels to emit `"Hel"` and `"lo"`;
- starts fake MCP JSON-RPC handlers for `system.time` and `files.create`;
- provides `clientContext()` with `authorization: Bearer client-key`;
- provides `internalContext()` with `authorization: Bearer internal-token`.

- [ ] **Step 4: Run integration tests**

Run:

```bash
cd turing-backend
go test ./tests
```

Expected: PASS.

- [ ] **Step 5: Commit integration tests**

Run:

```bash
git add turing-backend/tests go.mod go.sum
git commit -m "test: add Go gRPC integration coverage" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

## Task 11: Flutter gRPC networking migration

**Files:**
- Modify: `turing-client/turing_app/pubspec.yaml`
- Create: `turing-client/turing_app/lib/generated/turing/v1/**`
- Create: `turing-client/turing_app/lib/networking/grpc_client.dart`
- Create: `turing-client/turing_app/lib/networking/grpc_event_source.dart`
- Create: `turing-client/turing_app/lib/models/grpc_mappers.dart`
- Modify: `turing-client/turing_app/lib/app.dart`
- Modify: `turing-client/turing_app/lib/features/chat/chat_screen.dart`
- Modify: `turing-client/turing_app/lib/features/sessions/session_list_screen.dart`
- Modify: `turing-client/turing_app/lib/ui/shell/responsive_shell.dart`
- Create: `turing-client/turing_app/test/networking/grpc_client_test.dart`
- Create: `turing-client/turing_app/test/models/grpc_mappers_test.dart`

- [ ] **Step 1: Write failing Flutter networking tests**

Add a test that feeds synthetic `ChatStreamEvent` messages into the chat screen model and asserts token deltas append to the active assistant message:

```dart
test('maps token deltas into assistant message content', () {
  final event = ChatStreamEvent(
    sessionId: 'sess_1',
    runId: 'run_1',
    traceId: 'trace_1',
    tokenDelta: TokenDelta(messageId: 'msg_2', delta: 'Hel'),
  );
  final mapped = GrpcMappers.chatStreamEventToTuringEvent(event);
  expect(mapped.type, 'message.delta');
  expect(mapped.payload['delta'], 'Hel');
});
```

Add a test for `GrpcMetadataInterceptor`:

```dart
test('adds bearer token metadata', () {
  final metadata = GrpcAuthMetadata(apiKey: 'client-key').headers();
  expect(metadata['authorization'], 'Bearer client-key');
});
```

- [ ] **Step 2: Run Flutter tests to verify they fail**

Run:

```bash
cd turing-client/turing_app
flutter test test/networking/grpc_client_test.dart
```

Expected: FAIL because gRPC client and mappers do not exist.

- [ ] **Step 3: Add Flutter gRPC dependencies and generated stubs**

Modify `pubspec.yaml` dependencies:

```yaml
dependencies:
  grpc: ^4.0.1
  protobuf: ^4.0.0
```

Copy generated Dart files from `gen/turing/v1/dart` into `turing-client/turing_app/lib/generated/turing/v1/` or configure imports to reference the checked-in generated Dart location. Use one approach consistently; do not duplicate generated files in two Flutter import roots.

- [ ] **Step 4: Implement gRPC client and event source**

Create `grpc_client.dart`:

```dart
class GrpcAuthMetadata {
  const GrpcAuthMetadata({required this.apiKey});
  final String apiKey;
  Map<String, String> headers() => {'authorization': 'Bearer $apiKey'};
}
```

Implement `TuringGrpcApi` with methods matching the existing `TuringApi` interface: `getConfig`, `createSession`, `listSessions`, `listMessages`, `listEvents`, `sendMessage`, `approveApproval`, and `denyApproval`. `sendMessage` returns the initial run metadata after consuming the first `runQueued` event from `ChatService.SendMessage`.

Create `grpc_event_source.dart` implementing `TuringEventSource` with `EventService.subscribeSessionEvents`.

Create `grpc_mappers.dart` that maps protobuf messages into existing `Session`, `Message`, and `TuringEvent` models. Token deltas must map to:

```dart
TuringEvent(
  eventId: 'stream:${event.runId}:${event.sequence}',
  sessionId: event.sessionId,
  runId: event.runId,
  traceId: event.traceId,
  sequence: event.sequence.toInt(),
  type: 'message.delta',
  createdAt: DateTime.now().toUtc(),
  payload: {'messageId': event.tokenDelta.messageId, 'delta': event.tokenDelta.delta},
)
```

- [ ] **Step 5: Wire app to use gRPC classes**

Modify `app.dart`, session list, chat screen, and shell constructor wiring to instantiate:

```dart
final api = TuringGrpcApi(baseUrl: baseUrl, apiKey: apiKey);
final eventSource = TuringGrpcEventSource(baseUrl: baseUrl, apiKey: apiKey);
```

Remove direct use of `TuringApiClient` and `TuringWsClient` from production app wiring. Keep old files until Task 13 deletion so tests can compare behavior during migration.

- [ ] **Step 6: Run Flutter tests**

Run:

```bash
cd turing-client/turing_app
flutter test
```

Expected: PASS.

- [ ] **Step 7: Commit Flutter gRPC migration**

Run:

```bash
git add turing-client/turing_app/pubspec.yaml turing-client/turing_app/pubspec.lock turing-client/turing_app/lib turing-client/turing_app/test
git commit -m "feat: migrate Flutter networking to gRPC" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

## Task 12: Docker Compose Go cutover and smoke script

**Files:**
- Modify: `turing-backend/infra/docker-compose.yml`
- Create: `turing-backend/scripts/smoke-grpc.sh`
- Create: `turing-backend/scripts/grpc-smoke-client.go`
- Modify: `README.md`
- Modify: `turing-backend/.env.example`

- [ ] **Step 1: Write smoke script before changing Compose**

Create `turing-backend/scripts/grpc-smoke-client.go` that:

- dials `localhost:${ORCHESTRATOR_PUBLIC_PORT:-3000}` with insecure local credentials;
- sends bearer metadata using `TURING_CLIENT_API_KEY`;
- calls `HealthService.Check`;
- creates a session;
- calls `ChatService.SendMessage`;
- reads until a `run_completed` or `run_failed` event;
- calls `EventService.ListEvents` with `after_sequence = 0`;
- exits non-zero if no token delta or terminal event is observed.

Create `turing-backend/scripts/smoke-grpc.sh`:

```bash
#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
./scripts/init.sh
docker compose -f infra/docker-compose.yml up --build -d
trap 'docker compose -f infra/docker-compose.yml down' EXIT

for _ in $(seq 1 60); do
  if go run ./scripts/grpc-smoke-client.go -health-only; then
    break
  fi
  sleep 1
done

go run ./scripts/grpc-smoke-client.go
```

- [ ] **Step 2: Run smoke script to verify it fails against old Compose**

Run:

```bash
cd turing-backend
bash scripts/smoke-grpc.sh
```

Expected: FAIL because Compose still starts the TypeScript orchestrator without gRPC.

- [ ] **Step 3: Switch Compose to Go services**

Modify `turing-backend/infra/docker-compose.yml`:

- set Go service build contexts to repository root (`../..` from `turing-backend/infra/docker-compose.yml`);
- change `turing-orchestrator` Dockerfile to `turing-backend/orchestrator-go/Dockerfile`;
- change `turing-agent-runtime-general` Dockerfile to `turing-backend/agent-runtime-go/Dockerfile`;
- keep the same networks and internal MCP service names;
- expose public gRPC on `${ORCHESTRATOR_PUBLIC_PORT:-3000}:3000`;
- expose internal gRPC port `3001` only inside Docker networks;
- keep `host.docker.internal` for Ollama.

- [ ] **Step 4: Update docs for gRPC runtime**

Modify `README.md`:

- replace REST/WebSocket wording with public gRPC API;
- replace WebSocket smoke references with `scripts/smoke-grpc.sh`;
- document metadata header `authorization: Bearer <TURING_CLIENT_API_KEY>`;
- keep local-first security notes.

- [ ] **Step 5: Run Go and smoke checks**

Run:

```bash
cd turing-backend
go test ./...
bash scripts/smoke-grpc.sh
```

Expected: PASS.

- [ ] **Step 6: Commit Docker cutover**

Run:

```bash
git add README.md turing-backend/infra/docker-compose.yml turing-backend/scripts/smoke-grpc.sh turing-backend/scripts/grpc-smoke-client.go turing-backend/.env.example
git commit -m "feat: switch local stack to Go gRPC services" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

## Task 13: Remove TypeScript backend runtime and WebSocket/REST surfaces

**Files:**
- Delete: `turing-backend/orchestrator/src/**`
- Delete: `turing-backend/orchestrator/tests/**`
- Delete: `turing-backend/orchestrator/package.json`
- Delete: `turing-backend/orchestrator/tsconfig.json`
- Delete: `turing-backend/orchestrator/Dockerfile`
- Delete: `turing-backend/agent-runtime/src/**`
- Delete: `turing-backend/agent-runtime/tests/**`
- Delete: `turing-backend/agent-runtime/package.json`
- Delete: `turing-backend/agent-runtime/tsconfig.json`
- Delete: `turing-backend/agent-runtime/Dockerfile`
- Delete: `turing-backend/shared-types/**`
- Modify: `turing-backend/package.json`
- Delete: `turing-backend/package-lock.json`
- Delete: `turing-backend/scripts/smoke-ws.mjs`
- Modify: `.gitignore`
- Modify: `README.md`

- [ ] **Step 1: Verify Go/Flutter parity before deletion**

Run:

```bash
cd turing-backend
go test ./...
cd ../turing-client/turing_app
flutter test
```

Expected: PASS before deleting TypeScript code.

- [ ] **Step 2: Remove TypeScript backend runtime files**

Delete the TypeScript orchestrator, TypeScript agent-runtime, shared TypeScript types, package lock, and WebSocket smoke script. Keep Go MCP server directories.

Modify `turing-backend/package.json` to either remove it entirely if no backend npm scripts remain, or reduce it to:

```json
{
  "name": "project-turing-backend",
  "private": true,
  "scripts": {
    "build": "go build ./...",
    "test": "go test ./...",
    "lint": "go test ./..."
  }
}
```

If this package file remains, do not add npm dependencies.

- [ ] **Step 3: Update ignore rules and docs**

Modify `.gitignore`:

- remove Node/TypeScript backend-specific comments that imply orchestrator is Node;
- keep generic Node ignores for Flutter tooling if still needed;
- keep Go binary ignores.

Modify `README.md` to remove:

- WebSocket connection instructions;
- REST endpoint examples;
- Node.js backend prerequisite as a runtime requirement.

- [ ] **Step 4: Run final deletion checks**

Run:

```bash
rg "@fastify|websocket|WebSocket|ws_client|smoke-ws|agent-runtime/src|orchestrator/src|shared-types" .
cd turing-backend
go test ./...
go build ./...
cd ../turing-client/turing_app
flutter test
```

Expected:

- `rg` finds no TypeScript backend runtime or WebSocket client references except historical docs under `docs/superpowers/`.
- Go tests pass.
- Go builds pass.
- Flutter tests pass.

- [ ] **Step 5: Commit TypeScript backend removal**

Run:

```bash
git add -A
git commit -m "refactor: remove TypeScript backend runtime" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

## Task 14: Final verification and handoff

**Files:**
- No planned file edits. This task records final verification and commits corrections only when a verification command exposes a concrete mismatch.

- [ ] **Step 1: Run complete backend verification**

Run:

```bash
cd turing-backend
go test ./...
go build ./...
bash scripts/smoke-grpc.sh
```

Expected: PASS.

- [ ] **Step 2: Run complete Flutter verification**

Run:

```bash
cd turing-client/turing_app
flutter test
```

Expected: PASS.

- [ ] **Step 3: Run repository-level search checks**

Run:

```bash
rg "WebSocket|/ws|smoke-ws|@fastify|better-sqlite3|Promise\\.all|node dist/server|tsx src/server" README.md turing-backend turing-client/turing_app/lib
```

Expected: no matches except migration history in committed design/plan docs if those paths are included manually.

- [ ] **Step 4: Confirm generated code is deterministic**

Run:

```bash
tools/proto/check.sh
```

Expected: PASS. If optional generators are not installed, the script prints skip messages for those languages and leaves committed generated output unchanged.

- [ ] **Step 5: Commit final docs or verification fixes**

When Step 1 through Step 4 changed documentation or verification scripts, run:

```bash
git add README.md docs/superpowers/specs/2026-05-15-turing-go-grpc-migration-design.md docs/superpowers/plans/2026-05-15-turing-go-grpc-migration.md
git commit -m "docs: finalize Go gRPC migration handoff" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

When Step 1 through Step 4 leave the worktree clean, do not create an empty commit.

## Self-review checklist

- Spec coverage:
  - Go orchestrator: Tasks 2 through 8.
  - Go runtime: Task 9.
  - gRPC public API and server-streamed token responses: Tasks 1, 6, 8, 10.
  - internal gRPC worker stream: Tasks 1, 5, 9, 10.
  - safe dynamic JSON: Tasks 2, 7, 9, 10.
  - cancellation propagation: Tasks 5, 6, 9, 10.
  - Promise.all to Go concurrency: Task 9.
  - SQLite preservation and migrations: Task 3.
  - Flutter migration: Task 11.
  - future stubs: Task 1.
  - Docker cutover and TypeScript removal: Tasks 12 and 13.
- Placeholder scan: no red-flag placeholder text remains, and deferred choices from the design are fixed in the plan.
- Type consistency:
  - Proto service names match task code: `ChatService`, `EventService`, `SessionService`, `ApprovalService`, `RuntimeService`, `HealthService`.
  - Cancellation state uses `cancelled` in database status and `RUN_STATUS_CANCELLED` in proto.
  - Runtime worker stream uses `ConnectWorker(stream RuntimeUpdate) returns (stream RuntimeCommand)` consistently.
