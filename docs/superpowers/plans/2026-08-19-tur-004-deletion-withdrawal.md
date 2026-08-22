# TUR-004 Session Deletion Withdrawal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make session deletion a durable, idempotent withdrawal operation that
quiesces live work, withdraws queryable state and sandbox artifacts, and
terminates existing subscribers without allowing replay or reconnect.

**Architecture:** The repository owns a `deleting` session lifecycle,
generation, bounded drain leases, and content-free deletion receipt. The event
bus delivers a non-persisted terminal event after the final cascade commits.
The orchestrator authorizes and records all sandbox artifacts before
`mcp-files` writes a session/run namespace, so deletion can reconcile
filesystem work before deleting database ownership.

**Tech Stack:** Go 1.23, SQLite/FTS5, gRPC/protobuf, Go HTTP JSON-RPC MCP
server, Flutter/Dart, existing GitHub Actions verification.

---

### Task 1: Define protocol and schema ownership

**Files:**
- Modify: `proto/turing/v1/events.proto`
- Modify: `proto/turing/v1/approvals.proto`
- Modify: `proto/turing/v1/runtime.proto`
- Create: `turing-backend/orchestrator-go/internal/db/schema/0014_session_deletion_withdrawal.sql`
- Modify: `turing-backend/orchestrator-go/internal/db/schema_invariants_test.go`
- Test: `turing-backend/orchestrator-go/internal/db/schema_invariants_test.go`

- [ ] **Step 1: Write the failing schema-policy and proto contract tests**

```go
func TestDerivedStateSchemaPoliciesRequireDeletionReceiptAndSandboxArtifacts(t *testing.T) {
    policies := policyByTable(currentSchemaTablePolicies)
    if policies["session_deletions"].kind != schemaTableScrubbedException {
        t.Fatal("session_deletions must be a content-free scrubbed exception")
    }
    if policies["sandbox_artifacts"].kind != schemaTableCascadeOwned ||
        policies["sandbox_artifacts"].sourceTable != "sessions" {
        t.Fatal("sandbox artifacts must cascade from their session owner")
    }
}
```

```go
func TestSessionDeletedEventEnumIsMapped(t *testing.T) {
    if mapEventType("session.deleted") != turingv1.TuringEventType_TURING_EVENT_TYPE_SESSION_DELETED {
        t.Fatal("session.deleted must map to SESSION_DELETED")
    }
}
```

- [ ] **Step 2: Run the focused tests and confirm the missing schema/event contract fails**

Run:
`go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/db ./turing-backend/orchestrator-go/internal/service/events -run 'Test(DerivedStateSchemaPoliciesRequireDeletionReceiptAndSandboxArtifacts|SessionDeletedEventEnumIsMapped)' -count=1`

Expected: FAIL because the policy and enum do not exist.

- [ ] **Step 3: Add additive protobuf fields and migration**

```proto
enum TuringEventType {
  // Existing values stay unchanged.
  TURING_EVENT_TYPE_SESSION_DELETED = 22;
}
```

```sql
ALTER TABLE sessions
  ADD COLUMN deletion_state TEXT NOT NULL DEFAULT 'active'
  CHECK (deletion_state IN ('active','deleting'));

CREATE TABLE session_deletions (
  session_id TEXT PRIMARY KEY,
  lifecycle_version INTEGER NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('quiescing','artifacts','failed_external','completed')),
  terminal_sequence INTEGER NOT NULL,
  terminal_at TEXT,
  deleted_at TEXT,
  error_code TEXT,
  retryable INTEGER NOT NULL,
  run_count INTEGER NOT NULL DEFAULT 0,
  message_count INTEGER NOT NULL DEFAULT 0,
  retained_legacy_artifact_count INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE sandbox_artifacts (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  run_id TEXT NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  logical_path_hash TEXT NOT NULL,
  physical_path TEXT NOT NULL,
  state TEXT NOT NULL CHECK (state IN ('writing','ready','delete_failed')),
  policy TEXT NOT NULL CHECK (policy IN ('delete_on_session_delete','retain_legacy_unowned')),
  deletion_generation INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  finalized_at TEXT,
  UNIQUE(session_id, run_id, physical_path)
);
```

Add additive `SessionDeletionState` and `SessionDeletionReceipt` response
messages to the Session API. The receipt contains only session id, lifecycle
version, state, retryability, opaque error code, counters, and terminal time.
Add additive internal artifact authorization/finalization messages to the
existing authenticated internal approval/runtime channel; do not add a
listener. Add the receipt to `approvedScrubbedExceptionTables` and
`currentSchemaTablePolicies` with a rationale that it holds only an operational
receipt; add `sandbox_artifacts` as a cascade-owned session table. Extend the
event mapper with `session.deleted`.

- [ ] **Step 4: Regenerate protobuf output**

Run: `tools/proto/generate.sh`

Expected: generated Go and Dart protocol bindings include
`SESSION_DELETED` and the additive internal artifact authorization messages.

- [ ] **Step 5: Run focused tests and proto validation**

Run:
`go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/db ./turing-backend/orchestrator-go/internal/service/events -run 'Test(DerivedStateSchemaPoliciesRequireDeletionReceiptAndSandboxArtifacts|SessionDeletedEventEnumIsMapped)' -count=1 && tools/proto/check.sh`

Expected: PASS.

### Task 2: Make deleting sessions fail closed

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/repository/session_delete.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/jobs.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/approvals.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/sessions.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/events.go`
- Test: `turing-backend/orchestrator-go/internal/repository/session_delete_test.go`
- Test: `turing-backend/orchestrator-go/internal/repository/repository_test.go`

- [ ] **Step 1: Write failing repository race and visibility tests**

```go
func TestBeginSessionDeletionRejectsConcurrentEnqueueAndIdempotencyReplay(t *testing.T) {
    repo, session := newDeletionFixture(t)
    if _, err := repo.BeginSessionDeletion(context.Background(), session.SessionID); err != nil {
        t.Fatal(err)
    }
    _, err := repo.EnqueueUserMessage(context.Background(), deletionInput(session.SessionID))
    if !errors.Is(err, ErrSessionDeleting) {
        t.Fatalf("enqueue error = %v, want ErrSessionDeleting", err)
    }
    if _, err := repo.SearchMessages(context.Background(), "", "", "withdrawal-sentinel", 10); err != nil || got != 0 {
        t.Fatalf("deleting content must be hidden: err=%v count=%d", err, got)
    }
}
```

```go
func TestDeletionReceiptIsIdempotentAndContainsNoSessionContent(t *testing.T) {
    receipt := beginAndReadDeletion(t)
    if receipt.State != "quiescing" || receipt.ErrorCode != "" {
        t.Fatalf("receipt = %+v", receipt)
    }
    if strings.Contains(fmt.Sprintf("%+v", receipt), "withdrawal-sentinel") {
        t.Fatal("deletion receipt retained content")
    }
}
```

```go
func TestDeletingSessionIsInvisibleToEveryPublicRead(t *testing.T) {
    repo, session := newDeletionFixture(t)
    beginDeletion(t, repo, session.SessionID)
    assertSessionReadNotFound(t, repo, session.SessionID)
    assertMessagesReadNotFound(t, repo, session.SessionID)
    assertEventReplayNotFound(t, repo, session.SessionID)
    assertSearchCannotReturn(t, repo, "withdrawal-sentinel")
}
```

- [ ] **Step 2: Run the focused repository tests and confirm the lifecycle is absent**

Run:
`go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository -run 'Test(BeginSessionDeletionRejectsConcurrentEnqueueAndIdempotencyReplay|DeletionReceiptIsIdempotentAndContainsNoSessionContent)' -count=1`

Expected: FAIL because `BeginSessionDeletion` and `ErrSessionDeleting` do not
exist.

- [ ] **Step 3: Implement the state transition and active-session predicates**

```go
var ErrSessionDeleting = errors.New("session deletion is in progress")

func (r *Repository) BeginSessionDeletion(ctx context.Context, sessionID string) (SessionDeletionReceipt, error) {
    // Begin one transaction; return an existing receipt if present.
    // Change only active/archived rows to deleting, count owned rows, fence
    // queued work and approvals, and insert the content-free receipt.
}
```

Require `deletion_state = 'active'` in every public session-owned read and
mutation query through a centralized active-session predicate. Replace
idempotency replay's blind return with the same predicate. `ReplayEvents` must
return `ErrSessionNotFound` for a deleting or deleted session, rather than a
valid empty history. Keep direct FTS trigger deletion as the finalization
mechanism.

- [ ] **Step 4: Run focused repository tests and the existing deletion suite**

Run:
`go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository -run 'Test(DeleteSession|BeginSessionDeletion|DeletionReceipt)' -count=1`

Expected: PASS.

### Task 3: Implement quiescence and final cascade

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/repository/session_delete.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/runs.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/assignments.go`
- Test: `turing-backend/orchestrator-go/internal/repository/session_delete_test.go`
- Test: `turing-backend/orchestrator-go/internal/repository/cancellation_lifecycle_test.go`

- [ ] **Step 1: Write failing quiescence tests**

```go
func TestFinalizeSessionDeletionWaitsForExecutionExit(t *testing.T) {
    repo, run := seededLiveRun(t)
    if _, err := repo.BeginSessionDeletion(context.Background(), run.SessionID); err != nil {
        t.Fatal(err)
    }
    if _, err := repo.FinalizeSessionDeletion(context.Background(), run.SessionID); !errors.Is(err, ErrSessionDeletionQuiescing) {
        t.Fatalf("finalize = %v, want ErrSessionDeletionQuiescing", err)
    }
    acknowledgeExit(t, repo, run.RunID)
    if _, err := repo.FinalizeSessionDeletion(context.Background(), run.SessionID); err != nil {
        t.Fatal(err)
    }
}
```

```go
func TestFinalizeSessionDeletionReturnsRetryableReceiptAfterDrainLeaseExpires(t *testing.T) {
    repo, run := seededLiveRun(t)
    beginDeletion(t, repo, run.SessionID)
    expireDeletionDrainLease(t, repo, run.SessionID)
    receipt, err := repo.AdvanceSessionDeletion(context.Background(), run.SessionID)
    if err != nil || receipt.State != "failed_external" || receipt.ErrorCode != "execution_unreconciled" || !receipt.Retryable {
        t.Fatalf("receipt = %+v err=%v", receipt, err)
    }
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run:
`go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository -run TestFinalizeSessionDeletionWaitsForExecutionExit -count=1`

Expected: FAIL because finalization does not exist.

- [ ] **Step 3: Implement cancellation fencing and finalization**

```go
func (r *Repository) AdvanceSessionDeletion(ctx context.Context, sessionID string) (SessionDeletionReceipt, error) {
    // Never wait indefinitely. Require a quiescing/artifacts receipt and no
    // unexpired execution or writing-artifact lease. On expiry retain
    // failed_external with an opaque retryable error. On completion scrub audit,
    // delete the root, then mark the receipt completed in one transaction.
}
```

Reuse the existing `CancelRunWithEvent` lifecycle helpers inside the deletion
transaction so queued jobs, pending and approved approvals, and tool calls
become terminal before a live worker can act. Preserve `execution_active`
until the worker's acknowledged exit, then reject late reports by ownership
predicate instead of recreating a deleted row. Drive cancellation through the
existing authenticated runtime stream; no new listener is permitted.

- [ ] **Step 4: Run quiescence and cancellation regression tests**

Run:
`go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository -run 'Test(FinalizeSessionDeletion|CancelRun|DeleteSession)' -count=1`

Expected: PASS.

### Task 4: Deliver terminal event and fence replay

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/service/events/bus.go`
- Modify: `turing-backend/orchestrator-go/internal/service/events/service.go`
- Modify: `turing-backend/orchestrator-go/internal/service/sessions/service.go`
- Modify: `turing-backend/orchestrator-go/internal/app/app.go`
- Test: `turing-backend/orchestrator-go/internal/service/events/service_test.go`
- Test: `turing-backend/orchestrator-go/internal/service/sessions/service_test.go`

- [ ] **Step 1: Write failing terminal-delivery tests**

```go
func TestDeleteSessionDeliversOneTerminalEventThenCloses(t *testing.T) {
    h, session, stream := subscribedDeletionHarness(t)
    deleteSession(t, h, session.SessionID)
    event := recvEvent(t, stream)
    if event.Type != turingv1.TuringEventType_TURING_EVENT_TYPE_SESSION_DELETED {
        t.Fatalf("terminal type = %v", event.Type)
    }
    if _, err := stream.Recv(); err != io.EOF {
        t.Fatalf("stream after deletion = %v, want EOF", err)
    }
}

func TestDeletedSessionCannotReplayOrReconnect(t *testing.T) {
    h, session := finalizedDeletionHarness(t)
    for _, call := range []func() error{listEventsCall(h, session.SessionID), subscribeCall(h, session.SessionID)} {
        if status.Code(call()) != codes.NotFound {
            t.Fatal("deleted session must not reopen an event stream")
        }
    }
}
```

- [ ] **Step 2: Run focused event tests and confirm the terminal behavior fails**

Run:
`go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/events ./turing-backend/orchestrator-go/internal/service/sessions -run 'Test(DeleteSessionDeliversOneTerminalEventThenCloses|DeletedSessionCannotReplayOrReconnect)' -count=1`

Expected: FAIL because no terminal type or bus close exists.

- [ ] **Step 3: Add terminal subscription delivery**

```go
func (b *Bus) TerminateSession(event Event, generation int64) {
    // Under the bus lock, select subscriptions authorized and attached when
    // deletion started, record one dedicated non-droppable terminal slot per
    // subscription, fence later ordinary publication for this generation, wake
    // blocked subscribers, and close with the typed terminal reason only after
    // each stream consumes its slot.
}
```

`SubscribeSessionEvents` must map terminal events, skip ordinary replay gap
repair for them, and return cleanly after sending one. `ListEvents` and a new
subscription must verify active session visibility first. Wire
`sessions.Server` to the event bus and call `TerminateSession` only after
repository finalization commits.

- [ ] **Step 4: Add overflow and delete/send race tests**

```go
func TestTerminalDeletionSurvivesOverflowAndRejectsLatePublish(t *testing.T) {
    // Fill a slow subscriber's ordinary buffer using a deterministic send fence,
    // terminate, attempt Publish, and assert only ordered pre-terminal replay
    // plus exactly one terminal.
}
```

- [ ] **Step 5: Run event and session packages**

Run:
`go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/events ./turing-backend/orchestrator-go/internal/service/sessions -count=1`

Expected: PASS.

### Task 5: Add artifact authorization and durable manifest

**Files:**
- Modify: `proto/turing/v1/approvals.proto`
- Modify: `proto/turing/v1/runtime.proto`
- Modify: `turing-backend/orchestrator-go/internal/repository/approvals.go`
- Create: `turing-backend/orchestrator-go/internal/repository/sandbox_artifacts.go`
- Modify: `turing-backend/orchestrator-go/internal/service/approvals/service.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/service.go`
- Modify: `turing-backend/agent-runtime-go/internal/tools/runner.go`
- Modify: `turing-backend/agent-runtime-go/internal/mcp/client.go`
- Modify: `turing-backend/mcp-files/internal/approval/jwt.go`
- Modify: `turing-backend/mcp-files/cmd/server/main.go`
- Modify: `turing-backend/mcp-files/internal/tools/files.go`
- Test: `turing-backend/orchestrator-go/internal/service/approvals/service_test.go`
- Test: `turing-backend/agent-runtime-go/internal/tools/runner_test.go`
- Test: `turing-backend/agent-runtime-go/internal/mcp/client_test.go`
- Test: `turing-backend/mcp-files/internal/approval/jwt_test.go`
- Test: `turing-backend/mcp-files/internal/tools/files_test.go`

- [ ] **Step 1: Write failing provenance and race tests**

```go
func TestArtifactAuthorizationReservesSessionOwnedWrite(t *testing.T) {
    grant, err := approvals.AuthorizeSandboxMutation(ctx, approvalID)
    if err != nil { t.Fatal(err) }
    artifact := artifactByID(t, repo, grant.ArtifactID)
    if artifact.State != "writing" || artifact.Policy != "delete_on_session_delete" {
        t.Fatalf("artifact = %+v", artifact)
    }
}

func TestDeletionDuringArtifactWriteFailsToolAndReconcilesArtifact(t *testing.T) {
    grant := reserveArtifactWrite(t)
    beginDeletion(t, grant.SessionID)
    if err := finalizeArtifactWrite(t, grant); !errors.Is(err, ErrSessionDeleting) {
        t.Fatalf("finalize = %v, want ErrSessionDeleting", err)
    }
    retryDeletionToCompletion(t, grant.SessionID)
    assertNoArtifactOrNamespace(t, grant.SessionID)
}
```

```go
func TestLegacyUnownedArtifactIsRetainedAndCounted(t *testing.T) {
    legacy := seedLegacySandboxFile(t, "legacy.txt")
    writeRetainedArtifact(t, legacy)
    receipt := deleteSessionToReceipt(t, legacy.SessionID)
    if receipt.RetainedLegacyArtifactCount != 1 || !fileExists(legacy.Path) {
        t.Fatalf("legacy artifact was claimed or deleted: %+v", receipt)
    }
}
```

- [ ] **Step 2: Run the cross-module focused tests and confirm they fail**

Run:
`go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/approvals ./turing-backend/agent-runtime-go/internal/tools && ( cd turing-backend/mcp-files && go test ./internal/approval ./internal/tools )`

Expected: FAIL because no provenance grant or artifact reservation exists.

- [ ] **Step 3: Implement signed provenance and reserve-before-write**

```go
type SandboxGrant struct {
    ArtifactID string
    SessionID  string
    RunID      string
    Token      string
}

func (r *Repository) ReserveSandboxArtifactTx(ctx context.Context, tx *sql.Tx, input ReserveSandboxArtifactInput) (SandboxArtifact, error) {
    // Require active session and matching deletion generation, insert a bounded
    // writing lease, and derive only the opaque namespace destination from
    // server-owned ids. Pre-existing paths retain retain_legacy_unowned.
}
```

Extend the existing signed approval claims with run/session provenance for
mutation. Add an additive signed provenance token for safe file calls. Pass
both only in `_meta`; validate agent/tool/argument hash/session/run/deletion
generation/operation/path scope/expiry and reject unknown metadata. `mcp-files`
must perform a before-I/O generation check, derive the physical namespace
itself, call the existing authenticated internal finalizer after `fsync`,
perform the after-I/O generation check, and return a deletion-in-progress error
when finalization detects the race.

- [ ] **Step 4: Implement artifact deletion and durable external failure**

```go
func (r *Repository) MarkSessionArtifactDeleteFailure(ctx context.Context, sessionID, code string) error {
    // Store only a fixed error code on the receipt and keep state
    // failed_external; never store a filesystem path or OS error text.
}
```

Create a deletion collaborator that deletes every ready/writing manifest path
and scans only the owned session namespace. Treat missing files as an
idempotent removal. It advances to database finalization only after every
removal succeeds; offline MCP/runtime, lease expiration, stale grants, or
manifest/filesystem divergence retain `failed_external` for the next
idempotent request.

- [ ] **Step 5: Run all focused artifact tests**

Run:
`go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository ./turing-backend/orchestrator-go/internal/service/approvals ./turing-backend/agent-runtime-go/internal/tools ./turing-backend/agent-runtime-go/internal/mcp -count=1 && ( cd turing-backend/mcp-files && go test ./... -count=1 )`

Expected: PASS.

### Task 6: Make the Flutter client terminal-aware

**Files:**
- Modify: `turing-client/turing_app/lib/models/turing_event.dart`
- Modify: `turing-client/turing_app/lib/models/grpc_mappers.dart`
- Modify: `turing-client/turing_app/lib/features/chat/chat_screen.dart`
- Modify: `turing-client/turing_app/lib/ui/shell/responsive_shell.dart`
- Test: `turing-client/turing_app/test/features/chat_screen_test.dart`
- Test: `turing-client/turing_app/test/ui/responsive_shell_backend_test.dart`

- [ ] **Step 1: Write failing Flutter terminal-event tests**

```dart
testWidgets('session deletion closes chat without reconnecting', (tester) async {
  await pumpChat(tester);
  events.add(TuringEvent(type: TuringEventType.sessionDeleted, sessionId: 's1'));
  await tester.pump();
  expect(find.text('This conversation was deleted.'), findsOneWidget);
  expect(fakeSource.connectCalls, 1);
});
```

- [ ] **Step 2: Run the focused Flutter tests and confirm failure**

Run:
`cd turing-client/turing_app && flutter test test/features/chat_screen_test.dart test/ui/responsive_shell_backend_test.dart`

Expected: FAIL because the enum and terminal UI handling do not exist.

- [ ] **Step 3: Map and handle the terminal event**

```dart
if (event.type == TuringEventType.sessionDeleted) {
  widget.onSessionDeleted?.call(event.sessionId);
  await _subscription?.cancel();
  _eventSource.close();
  setState(() => _sessionDeleted = true);
  return;
}
```

Remove terminally deleted ids from session state, suppress global stale updates,
and show an unsuccessful deletion message when the server reports durable
external cleanup failure. Do not convert a deletion-in-progress response into
an optimistic local removal.

- [ ] **Step 4: Run focused Flutter tests**

Run:
`cd turing-client/turing_app && flutter test test/features/chat_screen_test.dart test/ui/responsive_shell_backend_test.dart`

Expected: PASS.

### Task 7: Document policy, limits, and roadmap status

**Files:**
- Modify: `docs/architecture/2026-08-18-personal-agent-audit.md`
- Modify: `docs/architecture/memory-governance.md`
- Modify: `docs/VISION.md`
- Modify: `README.md`
- Modify: `turing-client/turing_app/README.md`
- Modify: `docs/mcp-security-and-integration.md`

- [ ] **Step 1: Write documentation assertions or focused text tests where the repository has them**

```go
func TestVisionLinksDeletionWithdrawalContract(t *testing.T) {
    contents := mustReadRepoFile(t, "docs/VISION.md")
    if !strings.Contains(contents, "logical withdrawal") ||
        !strings.Contains(contents, "physical erasure") {
        t.Fatal("VISION must distinguish the deletion guarantees")
    }
}
```

- [ ] **Step 2: Run the documentation guard and confirm the new assertion fails**

Run:
`go test -tags sqlite_fts5 ./turing-backend/tests -run TestVisionLinksDeletionWithdrawalContract -count=1`

Expected: FAIL until the contract is documented.

- [ ] **Step 3: Document exactly what ships**

Document the deletion state machine, terminal event/reconnect behavior,
delete-on-session-delete plus retain-legacy-unowned artifact policy, namespace
provenance, durable external failure and retry semantics, bounded
execution/artifact leases, and the fact that no supported query surface returns
withdrawn content. Mark TUR-004 **Implementation pending merge** in the
central roadmap. State that WAL, backups, snapshots and SSD remapping prevent
a forensic-erasure promise. Link SQLite WAL and `secure_delete` documentation
and record the encryption/key-destruction evaluation: whole-database key
destruction is an all-database retirement strategy, requires destruction of
all key wrappers/backups, and is not a per-session guarantee. Do not add a
SQLCipher dependency, encryption migration, or new listener.

- [ ] **Step 4: Run the documentation guard**

Run:
`go test -tags sqlite_fts5 ./turing-backend/tests -count=1`

Expected: PASS.

### Task 8: Perform staged validation and review

**Files:**
- Modify: files identified by the preceding tasks only
- Test: full repository verification matrix

- [ ] **Step 1: Run targeted race tests repeatedly**

Run:
`go test -tags sqlite_fts5 -race ./turing-backend/orchestrator-go/internal/repository ./turing-backend/orchestrator-go/internal/service/events ./turing-backend/orchestrator-go/internal/service/approvals -run 'Test.*(Delete|Deletion|Artifact|Terminal)' -count=20`

Expected: PASS for all repetitions.

- [ ] **Step 2: Run required independent reviews**

Dispatch parallel full-diff reviews with Claude Opus 5 and GPT-5.6 Luna at
xhigh/long-context. Fix every valid finding, then repeat until both report no
remaining feedback. Dispatch the separate mandated Opus 4.8 review after
those rounds and address valid findings.

- [ ] **Step 3: Run the full verification matrix**

Run: `/verify`

Expected: root Go tests/build (including race), mcp-files tests/build, mcp-system
tests/build, Flutter analysis/tests, proto check, and all lint commands PASS.

- [ ] **Step 4: Commit, push, create one PR, and confirm live status**

```bash
git add <changed-files>
git commit -m "feat: withdraw deleted session artifacts" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
git push -u origin HEAD
```

Create one PR into `main`, apply `turing-roadmap`, do not merge, and confirm
its live GitHub state is `MERGEABLE`/`CLEAN` with all six visible CI jobs
successful before reporting the URL and head SHA.
