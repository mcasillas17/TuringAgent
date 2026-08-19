# TUR-002 Approval Rationale Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist human approval comments and denial reasons with their decisions, and include a bounded copy in the corresponding scrub-capable audit record.

**Architecture:** Add nullable `approval_comment` and `denial_reason` columns to `approvals`. Human RPC decisions write a valid string, including an explicitly empty string, in the same transaction as the state transition; automated approvals, expirations, and delivery failures leave both columns `NULL`. The current proto3 scalar fields do not expose presence, so omitted and explicitly empty human input share the documented representation `""`; audit payloads copy only the decision field and `toolName`, truncate the decision field on a UTF-8 boundary, and remain subject to whole-session audit scrubbing.

**Tech Stack:** Go 1.23, SQLite migrations and transactions, gRPC/proto3, Go `testing`, canonical JSON audit payloads, Flutter/Go generated protobuf code.

---

## File map

- Create `turing-backend/orchestrator-go/internal/db/schema/0011_approval_rationale.sql`: add nullable approval-comment and denial-reason storage without rebuilding or rewriting existing approvals.
- Modify `turing-backend/orchestrator-go/internal/db/migrations_test.go`: prove the migration preserves existing rows, initializes both new fields to `NULL`, and advances the schema version.
- Modify `turing-backend/orchestrator-go/internal/repository/approvals.go`: carry nullable rationale in `ApprovalRecord`, accept it at human transition boundaries, and write/read it atomically without changing token consumption.
- Modify `turing-backend/orchestrator-go/internal/repository/repository_test.go`: prove approve and deny transition storage, idempotency, and existing single-use consumption behavior.
- Modify `turing-backend/orchestrator-go/internal/repository/terminal_dependents_test.go`, `approval_consumption_regression_test.go`, `late_safe_after_regression_test.go`, `stale_approval_recovery_regression_test.go`, and runtime/service callers: pass an invalid `sql.NullString` for non-human transitions.
- Modify `turing-backend/orchestrator-go/internal/service/approvals/service.go`: validate human rationale, pass it to the repository, construct an allowlisted bounded audit payload, and keep automated decisions rationale-free.
- Modify `turing-backend/orchestrator-go/internal/service/approvals/service_test.go`: prove RPC field consumption, durable restart behavior, absent/empty contract, audit bounds/allowlist, and session-deletion scrubbing.
- Modify `turing-backend/orchestrator-go/internal/service/approvals/atomicity_regression_test.go`: prove required-event failure rolls rationale and decision back together before a successful retry.
- Modify `proto/turing/v1/approvals.proto`: document the proto3 absent/empty contract at the two existing scalar fields.
- Regenerate `gen/turing/v1/go/turing/v1/approvals.pb.go` and `turing-client/turing_app/lib/generated/turing/v1/approvals.pb.dart`/`.pbjson.dart`: keep committed generated API documentation in sync.
- Modify `docs/mcp-security-and-integration.md`, `docs/architecture/tech-stack.md`, and `docs/architecture/2026-08-18-personal-agent-audit.md`: document durable rationale, null/empty semantics, audit truncation, deletion scrubbing, and TUR-002 completion without adding a public audit API or preview UX.

### Task 1: Add the approval-rationale migration

**Files:**
- Create: `turing-backend/orchestrator-go/internal/db/schema/0011_approval_rationale.sql`
- Modify: `turing-backend/orchestrator-go/internal/db/migrations_test.go`

- [ ] **Step 1: Write the failing migration test**

Add a test that applies `0001_initial.sql`, inserts a complete existing session/run/tool-call/approval graph, then applies `0011_approval_rationale.sql` and reads:

```go
var status string
var approvalComment, denialReason sql.NullString
if err := database.QueryRowContext(ctx, `
	SELECT status, approval_comment, denial_reason
	FROM approvals
	WHERE id = 'approval_before_rationale'
`).Scan(&status, &approvalComment, &denialReason); err != nil {
	t.Fatal(err)
}
if status != "pending" || approvalComment.Valid || denialReason.Valid {
	t.Fatalf("upgraded approval = status %q comment %#v reason %#v, want pending and both NULL",
		status, approvalComment, denialReason)
}
```

Update the expected embedded migration list to include `"0011_approval_rationale"` and change `TestCurrentSchemaVersionUsesLatestEmbeddedMigrationPrefix` to expect `"0011"`.

- [ ] **Step 2: Run the migration tests to verify RED**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/db -run 'TestApprovalRationaleMigrationPreservesExistingApprovals|TestApplyMigrationsRecordsEmbeddedMigrationsInLexicalOrder|TestCurrentSchemaVersionUsesLatestEmbeddedMigrationPrefix' -count=1
```

Expected: FAIL because `0011_approval_rationale.sql` and the two columns do not exist and the latest version is still `0010`.

- [ ] **Step 3: Add the minimal migration**

Create:

```sql
ALTER TABLE approvals ADD COLUMN approval_comment TEXT;
ALTER TABLE approvals ADD COLUMN denial_reason TEXT;
```

Both columns stay nullable: `NULL` means that decision path never carried that human field; a valid empty string means the human RPC field resolved to `""`.

- [ ] **Step 4: Run the migration tests to verify GREEN**

Run the command from Step 2.

Expected: PASS.

### Task 2: Persist approval comments atomically

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/repository/approvals.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/repository_test.go`
- Modify: direct repository callers of `ApproveApproval` and `ApproveApprovalWithEvent`

- [ ] **Step 1: Write the failing repository test**

Extend `TestApprovalLifecycleRecordsTokenAndUpdatesRun` to approve with:

```go
comment := sql.NullString{String: "Reviewed the exact note update", Valid: true}
approved, err := repo.ApproveApproval(
	ctx,
	approval.ApprovalID,
	"approval_token_1",
	comment,
	"2026-05-15T00:00:00Z",
)
```

Assert the returned record and database both preserve the value:

```go
if approved.ApprovalComment != comment || approved.DenialReason.Valid {
	t.Fatalf("approval rationale = comment %#v reason %#v", approved.ApprovalComment, approved.DenialReason)
}
var storedComment, storedReason sql.NullString
if err := database.QueryRowContext(ctx, `
	SELECT approval_comment, denial_reason FROM approvals WHERE id = ?
`, approval.ApprovalID).Scan(&storedComment, &storedReason); err != nil {
	t.Fatal(err)
}
if storedComment != comment || storedReason.Valid {
	t.Fatalf("stored rationale = comment %#v reason %#v", storedComment, storedReason)
}
```

Repeat approval with a different comment and assert the first committed value remains unchanged, proving idempotent retries cannot rewrite decision history.

Also add `TestApprovedApprovalEventFailureRollsBackCommentAndDecision` in
`service/approvals/atomicity_regression_test.go` before changing repository
code. Its required-event trigger must leave both `status = 'pending'` and
`approval_comment IS NULL`.

- [ ] **Step 2: Run the repository test to verify RED**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository -run TestApprovalLifecycleRecordsTokenAndUpdatesRun -count=1
```

Expected: FAIL to compile because `ApprovalRecord` and `ApproveApproval` do not expose approval comments.

- [ ] **Step 3: Implement the minimal approval plumbing**

Add exact nullable fields:

```go
type ApprovalRecord struct {
	// existing fields...
	ApprovalComment sql.NullString
	DenialReason    sql.NullString
}
```

Change the approval signatures to take `approvalComment sql.NullString` before `decidedAt`, and bind the value in the existing state transition:

```go
result, err := tx.ExecContext(ctx, `
	UPDATE approvals
	SET status = 'approved',
		approval_jti = ?,
		approval_token = ?,
		approval_comment = ?,
		decided_at = ?
	WHERE id = ? AND status = 'pending'
`, approvalID, approvalToken, nullableString(approvalComment), decidedAt, approvalID)
```

Extend `approvalByID` to scan both columns as `sql.NullString`. Update non-human repository callers to pass `sql.NullString{}`; do not change JWT creation, expiry, or consumption SQL.

- [ ] **Step 4: Run the repository test to verify GREEN**

Run the command from Step 2.

Expected: PASS, including the existing second-consumption rejection.

### Task 3: Persist denial reasons atomically

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/repository/approvals.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/repository_test.go`
- Modify: direct repository callers of `DenyApproval` and `DenyApprovalWithEvent`

- [ ] **Step 1: Write the failing denial test**

In `TestDenyApprovalAtomicallyTerminalizesToolRunJobAndEvent`, deny with:

```go
reason := sql.NullString{String: "The destination is not the one I intended", Valid: true}
if _, err := repo.DenyApproval(ctx, approval.ApprovalID, reason, "2026-05-15T00:00:00Z"); err != nil {
	t.Fatal(err)
}
```

Assert `denial_reason` equals `reason`, `approval_comment` remains `NULL`, and a repeated denial with a different reason leaves the first committed reason unchanged.

Before changing repository code, strengthen
`TestDeniedApprovalEventFailureRollsBackAndRetryCommitsOnce` to submit a reason,
assert rollback leaves `denial_reason IS NULL`, and assert the successful retry
commits the reason with the terminal state.

- [ ] **Step 2: Run the denial test to verify RED**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository -run TestDenyApprovalAtomicallyTerminalizesToolRunJobAndEvent -count=1
```

Expected: FAIL to compile because denial methods do not accept a reason.

- [ ] **Step 3: Implement the minimal denial plumbing**

Change `DenyApproval` and `DenyApprovalWithEvent` to accept `denialReason sql.NullString`. Pass it into `terminalizeApproval`; expiration and delivery-failure callers pass `sql.NullString{}`.

After the expiry-at-decision branch has finalized `approvalStatus`, select the value honestly:

```go
storedDenialReason := sql.NullString{}
if approvalStatus == "denied" {
	storedDenialReason = denialReason
}
```

Add `denial_reason = ?` to the existing terminal transition with `nullableString(storedDenialReason)`. This keeps a late deny that becomes `expired` from falsely recording a human denial reason.

- [ ] **Step 4: Run the denial test to verify GREEN**

Run the command from Step 2.

Expected: PASS with one terminal run event and the first reason retained.

### Task 4: Consume RPC fields and define empty/absent semantics

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/service/approvals/service.go`
- Modify: `turing-backend/orchestrator-go/internal/service/approvals/service_test.go`

- [ ] **Step 1: Write failing service tests for both fields**

Add table-driven subtests that invoke the real service with a non-empty comment/reason and then call `repo.GetApproval`:

```go
if got.ApprovalComment != (sql.NullString{String: "Approved after checking the path", Valid: true}) {
	t.Fatalf("approval comment = %#v", got.ApprovalComment)
}
if got.DenialReason != (sql.NullString{String: "Wrong destination", Valid: true}) {
	t.Fatalf("denial reason = %#v", got.DenialReason)
}
```

Add human empty-input subtests for `{ApprovalId: id}` and `{ApprovalId: id, Comment: ""}` (and their denial equivalents). Assert both produce a valid empty `sql.NullString`, documenting the unavoidable proto3 scalar collapse.

Before changing service code, add approve and deny restart subtests. Each opens
`filepath.Join(t.TempDir(), "turing.db")`, applies migrations, creates the real
approval graph, decides through `Server`, closes the database, reopens it, and
asserts `repository.New(reopened).GetApproval` returns the exact
`sql.NullString`.

- [ ] **Step 2: Write a failing boundary-validation test**

Call both handlers with `strings.Repeat("x", maxDecisionRationaleBytes+1)` and assert gRPC `InvalidArgument`. Call with `""` and assert success.

- [ ] **Step 3: Run the service tests to verify RED**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/approvals -run 'TestHumanApprovalRationalePersists|TestHumanApprovalRationaleEmptyInputContract|TestApprovalRationaleRejectsOversizedInput' -count=1
```

Expected: FAIL because the handlers discard both fields and no rationale bound exists.

- [ ] **Step 4: Implement the RPC boundary**

Add:

```go
const maxDecisionRationaleBytes = 4096

func validateDecisionRationale(fieldName string, value string) error {
	if !utf8.ValidString(value) {
		return status.Errorf(codes.InvalidArgument, "%s must be valid UTF-8", fieldName)
	}
	if len(value) > maxDecisionRationaleBytes {
		return status.Errorf(codes.InvalidArgument, "%s must be at most %d bytes", fieldName, maxDecisionRationaleBytes)
	}
	return nil
}
```

In `ApproveApproval`, validate `comment`, then pass `sql.NullString{String: req.Comment, Valid: true}`. In `DenyApproval`, do the same for `reason`. Existing auth remains server-derived, and SQL remains parameterized.

- [ ] **Step 5: Run the service tests to verify GREEN**

Run the command from Step 3.

Expected: PASS.

### Task 5: Prove rollback, restart durability, and single-use stability

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/service/approvals/atomicity_regression_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/approvals/service_test.go`

- [ ] **Step 1: Write the failing approval rollback test**

Create an `events` trigger that aborts `approval.approved`, approve with a comment, and assert after the error:

```go
if approval.Status != "pending" || approval.ApprovalComment.Valid {
	t.Fatalf("failed approval leaked state: %+v", approval)
}
```

Drop the trigger, retry, and assert status/comment commit together.

- [ ] **Step 2: Strengthen the existing denial rollback test**

Pass a reason to the failing first denial. Assert the pending row has `DenialReason.Valid == false`; after dropping the trigger, retry and assert the reason commits with `denied`.

- [ ] **Step 3: Run atomicity tests to verify RED**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/approvals -run 'TestApprovedApprovalEventFailureRollsBackCommentAndDecision|TestDeniedApprovalEventFailureRollsBackAndRetryCommitsOnce' -count=1
```

Expected: FAIL until rationale is part of the same repository transaction.

- [ ] **Step 4: Rerun the restart durability tests written before Task 4 implementation**

The approve and deny subtests open `filepath.Join(t.TempDir(), "turing.db")`,
apply migrations, create the real approval graph, decide through `Server`, close
the database, reopen it, and assert `repository.New(reopened).GetApproval`
returns the exact `sql.NullString`.

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/approvals -run TestApprovalRationaleSurvivesDatabaseRestart -count=1
```

Expected after Tasks 1-4: PASS.

- [ ] **Step 5: Run consumption regressions**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository ./turing-backend/orchestrator-go/internal/service/approvals -run 'Approval|Consume' -count=1
```

Expected: PASS; approval tokens remain argument-bound and consumable only once.

### Task 6: Add bounded allowlisted audit rationale and deletion scrubbing coverage

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/service/approvals/service.go`
- Modify: `turing-backend/orchestrator-go/internal/service/approvals/service_test.go`

- [ ] **Step 1: Write failing audit tests**

For human approval and denial, decode the corresponding `audit_logs.payload_json` and assert:

```go
if payload["comment"] != "Approved after checking the path" {
	t.Fatalf("approval audit payload = %#v", payload)
}
if payload["reason"] != "Wrong destination" {
	t.Fatalf("denial audit payload = %#v", payload)
}
```

Use a rationale longer than `maxAuditRationaleBytes`, assert the stored approval retains the complete value, and assert the audit string is at most the configured byte bound, valid UTF-8, and accompanied by `commentTruncated: true` or `reasonTruncated: true`.

Seed oversized tool arguments containing a sentinel secret and assert the decision audit JSON does not contain that sentinel and has none of `approvalToken`, `approvalJti`, `args`, `argsHash`, or `toolArgs`.

- [ ] **Step 2: Run audit tests to verify RED**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/approvals -run 'TestHumanApprovalRationaleAuditPayload|TestApprovalRationaleAuditIsBoundedAndAllowlisted' -count=1
```

Expected: FAIL because decision audit payloads currently contain only `toolName`.

- [ ] **Step 3: Implement a UTF-8-safe bounded allowlist**

Add:

```go
const maxAuditRationaleBytes = 512

func boundedAuditRationale(value string) (string, bool) {
	if len(value) <= maxAuditRationaleBytes {
		return value, false
	}
	prefix := value[:maxAuditRationaleBytes-3]
	for !utf8.ValidString(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix + "...", true
}
```

Replace the inline `toolName` map in `finishPostCommit` with an allowlisted helper:

```go
func approvalAuditPayload(approval repository.ApprovalRecord, action string) map[string]any {
	payload := map[string]any{"toolName": approval.ToolName}
	switch action {
	case "approval.approved":
		if approval.ApprovalComment.Valid {
			comment, truncated := boundedAuditRationale(approval.ApprovalComment.String)
			payload["comment"] = comment
			if truncated {
				payload["commentTruncated"] = true
			}
		}
	case "approval.denied":
		if approval.DenialReason.Valid {
			reason, truncated := boundedAuditRationale(approval.DenialReason.String)
			payload["reason"] = reason
			if truncated {
				payload["reasonTruncated"] = true
			}
		}
	}
	return payload
}
```

Only add the `*Truncated` key when truncation occurred. Never marshal `ApprovalRecord` wholesale.

- [ ] **Step 4: Run audit tests to verify GREEN**

Run the command from Step 2.

Expected: PASS.

- [ ] **Step 5: Write and run the session-deletion test**

Deny an approval with a unique rationale, delete its now-terminal session, then assert the approval row cascaded away and its surviving `approval.denied` audit row has exactly `{"scrubbed":true}` without the rationale.

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/approvals -run TestDeleteSessionScrubsApprovalRationaleAudit -count=1
```

Expected: PASS using the existing correlation-based scrub.

### Task 7: Document the wire contract and regenerate protobufs

**Files:**
- Modify: `proto/turing/v1/approvals.proto`
- Regenerate: `gen/turing/v1/go/turing/v1/approvals.pb.go`
- Regenerate: `turing-client/turing_app/lib/generated/turing/v1/approvals.pb.dart`
- Regenerate: `turing-client/turing_app/lib/generated/turing/v1/approvals.pbjson.dart`
- Modify: `docs/mcp-security-and-integration.md`
- Modify: `docs/architecture/tech-stack.md`
- Modify: `docs/architecture/2026-08-18-personal-agent-audit.md`

- [ ] **Step 1: Add exact proto field comments**

Document both existing fields without changing field numbers or presence:

```proto
// Optional by convention. Proto3 scalar presence is not enabled, so omission
// and an explicitly empty value are both persisted as an empty human comment.
string comment = 2;

// Optional by convention. Proto3 scalar presence is not enabled, so omission
// and an explicitly empty value are both persisted as an empty human reason.
string reason = 2;
```

- [ ] **Step 2: Regenerate committed protobuf outputs**

Run:

```bash
tools/proto/generate.sh
```

Expected: generated Go and Dart comments/descriptors update with no API shape change.

- [ ] **Step 3: Update architecture/security documentation**

Document these exact guarantees:

- human approval comments and denial reasons are stored in separate nullable columns in the same transaction as the decision;
- a human omitted/empty proto3 scalar persists as valid `""`, while `NULL` means a non-human path never carried that field;
- rationale accepts at most 4096 UTF-8 bytes;
- human decision audit payloads include only `toolName` plus a 512-byte UTF-8-safe rationale copy and a truncation flag;
- tokens, JTIs, hashes, and tool arguments remain outside decision audit payloads;
- session deletion cascades the approval row and replaces correlated audit payloads with `{"scrubbed":true}`;
- TUR-013 remains responsible for a public audit read API and TUR-021 remains responsible for approval previews/diffs.

Mark TUR-002 implemented in the roadmap entry while preserving those follow-up dependencies.

- [ ] **Step 4: Run proto and focused documentation-adjacent checks**

Run:

```bash
tools/proto/check.sh
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/db ./turing-backend/orchestrator-go/internal/repository ./turing-backend/orchestrator-go/internal/service/approvals -count=1
```

Expected: PASS.

### Task 8: Independent review loops

**Files:**
- Review: full `git diff main...HEAD` plus unstaged changes, including this plan, docs, generated code, and tests.

- [ ] **Step 1: Run required parallel round**

Dispatch in parallel:

1. Claude Opus 5, `xhigh`, `long_context`: spec/architecture/privacy review against the TUR-002 prompt and `docs/VISION.md`.
2. GPT-5.6 Luna, `xhigh`, `long_context`: correctness, atomicity, edge cases, security, regressions, and test coverage.

Both reviewers must inspect the full diff and explicitly return either concrete findings or “no remaining feedback.”

- [ ] **Step 2: Return every valid item to the implementation loop**

For every valid finding: write a failing regression test first, run it to confirm RED, implement the smallest fix, run focused tests to GREEN, and update docs if the contract changed.

- [ ] **Step 3: Repeat both reviewers**

Rerun both reviewers in parallel on the new full diff. Repeat Steps 2-3 until both explicitly report no remaining feedback in the same round. Record the round count for the creator handoff.

- [ ] **Step 4: Run the repository-required Opus 4.8 review**

Dispatch Claude Opus 4.8 on the full final diff with the repository checklist: correctness, edge cases, stated intent, reuse/simplification/naming, and explicit unit-test gaps. Resolve every valid item with TDD and rerun Opus 4.8 until clean.

### Task 9: Full verification, commit, push, and PR

**Files:**
- Verify and publish all TUR-002 changes only.

- [ ] **Step 1: Run the full repository matrix**

Invoke `/verify`, which must run:

```bash
go test -tags sqlite_fts5 ./... -count=1
go test -tags sqlite_fts5 -race ./... -count=1
go build -tags sqlite_fts5 ./...
( cd turing-backend/mcp-files  && go test ./... -count=1 && go test -race ./... -count=1 && go build ./cmd/server )
( cd turing-backend/mcp-system && go test ./... -count=1 && go test -race ./... -count=1 && go build ./... )
( cd turing-client/turing_app  && flutter analyze && flutter test )
tools/proto/check.sh
golangci-lint run --build-tags sqlite_fts5 ./... ./.github/workflows
( cd turing-backend/mcp-files  && golangci-lint run ./... )
( cd turing-backend/mcp-system && golangci-lint run ./... )
```

Expected: every command exits 0.

- [ ] **Step 2: Perform final hygiene and secret sweep**

Run:

```bash
git diff --check
git status --short
git diff --name-only main...HEAD
git diff -- . ':(exclude)gen/**' ':(exclude)turing-client/turing_app/lib/generated/**' |
  rg -n '(?i)(api[_-]?key|authorization|bearer|password|secret|approval[_-]?token|approval[_-]?jti)'
```

Inspect every hit as code/documentation terminology or a redacted test fixture; no live secret may remain.

- [ ] **Step 3: Commit once all gates are clean**

Stage only TUR-002 files and commit:

```text
feat(approvals): persist decision rationale

Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>
```

- [ ] **Step 4: Push and open one PR into `main`**

Push `mcasillas17-tur-002-approval-rationale`, then use `create_pull_request` with a focused title/body covering storage semantics, audit privacy/bounds, deletion behavior, tests, review rounds, and verification. Do not merge.

- [ ] **Step 5: Message the creator**

Send project session `62cf9513-4bf4-4de3-a427-bc9d705157c3` the PR URL, head SHA, fresh `/verify` evidence, changed documentation, independent reviewer round count, Opus 4.8 result, and unchanged follow-up dependencies on TUR-013 and TUR-021.
