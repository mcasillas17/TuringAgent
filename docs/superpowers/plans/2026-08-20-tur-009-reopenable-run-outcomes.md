# TUR-009 Reopenable Run Outcomes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persist one authoritative, versioned run outcome so live streaming and reopened conversation history show the same terminal truth and no blank assistant turn is ambiguous.

**Architecture:** `agent_runs` remains the canonical lifecycle row. Every real transition atomically increments its state version and appends the matching event snapshot; message history embeds that snapshot through the correlated assistant message. Typed failure normalization closes every durable ingestion path, while Flutter reconciles history and events by `(run_id, state_version)` and renders localized cards adjacent to the assistant entry.

**Tech Stack:** Go 1.23, SQLite, gRPC/protobuf, Flutter/Dart, `protoc` 34.1, `protoc-gen-go` 1.36.11, `protoc-gen-go-grpc` 1.6.2, Dart `protoc_plugin` 22.5.0, Go `testing`, Flutter `flutter_test`.

**Approved spec:** `docs/superpowers/specs/2026-08-20-tur-009-reopenable-run-outcomes-design.md`

---

## Execution Rules

- Keep this branch limited to TUR-009. Do not implement TUR-004, TUR-008, TUR-003, or TUR-010 behavior that has not landed on `main`.
- Use strict RED → minimal GREEN → refactor for every behavior below. Record the named RED failure before changing production code.
- Run Go commands from the repository root with `-tags sqlite_fts5`.
- Use channels, barriers, synchronous fake streams, and Dart `Completer`s for races. Do not add sleeps.
- Keep every SQLite transaction bounded and free of model, tool, filesystem, or network work.
- Do not manually edit generated protobuf files.
- Do not persist or expose provider messages, raw errors, tool arguments/results, approval tokens, paths, IDs in migration errors, or human approval rationale in run-state/failure projections.
- Do not create success-shaped assistant text for a failed, cancelled, abandoned, or interrupted run.
- Terminal lifecycle rows are immutable. A duplicate is successful only when the complete canonical identity matches.

## Planned Domain Signatures

Keep these names consistent across tasks so later steps do not invent a second state model.

```go
// turing-backend/orchestrator-go/internal/repository/runs.go
type RunState struct {
	RunID                string
	UserMessageID        string
	AssistantMessageID   string
	Lifecycle            string
	OutcomeReason        string
	StateVersion         int64
	StateUpdatedAt       string
	FinishedAt           sql.NullString
	HasDisplayableContent bool
	ContentSHA256        string // internal only; never map to protobuf
}

type RunTransitionResult struct {
	State     RunState
	Events    []Event
	Duplicate bool
}

type CompleteRunInput struct {
	RunID               string
	AssistantMessageID  string
	Content             string
	ExpectedStateVersion int64
	Usage               *RunTokenUsage
}

type FailRunInput struct {
	RunID                string
	AssistantMessageID   string
	ExpectedStateVersion int64
	Failure              runoutcome.Failure
	PreserveExecution    bool
}

type CancelRunInput struct {
	RunID                string
	AssistantMessageID   string
	ExpectedStateVersion int64
	Cancellation         runoutcome.Cancellation
	PreserveExecution    bool
}

func (r *Repository) CompleteRunCanonical(context.Context, CompleteRunInput) (RunTransitionResult, error)
func (r *Repository) FailRunCanonical(context.Context, FailRunInput) (RunTransitionResult, error)
func (r *Repository) CancelRunCanonical(context.Context, CancelRunInput) (RunTransitionResult, error)
```

```go
// turing-backend/orchestrator-go/internal/runoutcome/outcome.go
type Origin uint8
type RetryClass uint8
type Reason string

type Failure struct { /* private canonical fields */ }
type Cancellation struct { /* private canonical fields */ }

func NormalizeFailure(origin Origin, code string, retry RetryClass) Failure
func NormalizeRuntimeFailure(
	origin turingv1.FailureOrigin,
	code string,
	retry turingv1.AutomaticRetryClass,
) Failure
func AbandonedCancellation() Cancellation
```

`repository.RunState` is the persistence domain object. Add
`turing-backend/orchestrator-go/internal/service/runstate/projection.go` as the
only repository-to-protobuf mapper used by SessionService, ChatService, and
EventService. Do not put protobuf types into repository method signatures.

Add `turing-backend/orchestrator-go/internal/runcorrelation/correlation.go` as
the dependency-neutral owner of one-link validation:

```go
type Link struct {
	RunID, RunSessionID, RunAssistantMessageID string
	MessageID, MessageSessionID, MessageRunID  string
	MessageRole                                string
}

func Validate(Link) error
```

The migration preflight, atomic enqueue, and joined history reader all call
this validator after their own query-level uniqueness checks. Neither
repository nor migration code defines a second correlation rule.

---

### Task 0: Merge the Current Base and Freeze Allocations

**Files:**
- Inspect: `turing-backend/orchestrator-go/internal/db/schema/*.sql`
- Inspect: `proto/turing/v1/common.proto`
- Inspect: `proto/turing/v1/chat.proto`
- Inspect: `proto/turing/v1/events.proto`
- Inspect: `proto/turing/v1/runtime.proto`
- Inspect: `turing-backend/tests/proto_contract_test.go`
- Inspect: TUR-004/TUR-008/TUR-003 files that arrive through the merge

- [ ] **Step 1: Refresh and merge, never rebase**

Run:

```bash
git fetch origin
git merge --no-edit origin/main
git status --short
```

Expected: a normal merge or “Already up to date”; no rebase and no force-push.

- [ ] **Step 2: Classify landed roadmap scope before editing**

Run:

```bash
git --no-pager log --oneline --decorate -20
ls turing-backend/orchestrator-go/internal/db/schema | sort
rg -n "SESSION_DELETED|DeleteSession|tombstone" proto turing-backend turing-client docs
rg -n "archive|archived|page_token|next_page|persisttime" proto turing-backend turing-client docs
rg -n "egress|remote provider|external agent" proto turing-backend turing-client docs/architecture
```

Record in the implementation checkpoint:

- TUR-004 landed or not; if landed, preserve deletion NotFound/tombstone/terminal-event precedence.
- TUR-008 landed or not; if landed, reuse its pagination, archive/status reconciliation, and `persisttime` package.
- TUR-003 landed or not; if landed, preserve run-owned egress decisions and redacted notices.

Do not copy code from an unmerged PR.

- [ ] **Step 3: Select the migration number after the merge**

The final selected migration is
`turing-backend/orchestrator-go/internal/db/schema/0017_run_outcomes.sql`, selected
after the final main merge because main already owned prefix `0016`. After the merge:

1. If the highest merged prefix is below `0016`, keep `0016_run_outcomes.sql`.
2. If `0016` or a higher prefix already exists, select the first unused prefix
   greater than the merged maximum.
3. Set the same exact version string in the version-keyed Go hook and migration
   tests.

Run:

```bash
ls turing-backend/orchestrator-go/internal/db/schema/*.sql | sort | tail -5
```

Expected: one unambiguous selected filename, with no collision.

- [ ] **Step 4: Recheck all protobuf allocations**

The merged descriptors must leave these additions intact:

| Contract | Required allocation |
|---|---:|
| `Message.run_state` | 9 |
| `RunState` fields | 1 through 9 as approved |
| `RunQueued.run_state` | 4 |
| `RunStarted.run_state` | 4 |
| `ApprovalEvent.run_state` | 4 |
| `RunCompleted.run_state` | 3 |
| `RunFailed.run_state` | 5; field 4 remains deprecated |
| `RunCancelled.run_state` | 3 |
| `RunStateChanged.run_state` | 1 |
| `ChatStreamEvent.run_state_changed` | 27 |
| `TuringEvent.run_state` | 9 |
| `TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED` | 23 |
| `AgentJob.expected_state_version` | 19; merged egress fields retain 17 and 18 |
| `AgentJob.assignment_attempt_id` | 20 |
| `RuntimeRunCompleted.expected_state_version` | 6 |
| `RuntimeRunFailed.failure_origin` | 5 |
| `RuntimeRunFailed.automatic_retry_class` | 6 |
| `RuntimeRunFailed.expected_state_version` | 7 |
| `RuntimeCancelledAck.observed_state_version` | 2 |
| `RuntimeApprovalResumeReady` fields | 1 through 4 as approved |
| `RuntimeUpdate.approval_resume_ready` | 9; value 8 stays worker capabilities |
| `RuntimeApprovalResumeAccepted` fields | 1 through 4 as approved |
| `RuntimeCommand.approval_resume_accepted` | 8; merged MCP registry change retains 7 |
| `RuntimeRunCancelled.state_version` | 3 |
| `RuntimeApprovalUpdated.state_version` | 4 |
| `ToolPolicyDecision.run_state_version` | 8; merged provenance token retains 7 |

If a merged contract occupies any required number other than event value 22,
stop and return to the design gate. Do not renumber an existing field.

---

### Task 1: Add the Additive Protobuf Contract

**Files:**
- Modify: `turing-backend/tests/proto_contract_test.go`
- Modify: `proto/turing/v1/common.proto`
- Modify: `proto/turing/v1/chat.proto`
- Modify: `proto/turing/v1/events.proto`
- Modify: `proto/turing/v1/runtime.proto`
- Modify: `proto/turing/v1/tools.proto`
- Generate: `gen/turing/v1/go/turing/v1/*.pb.go`
- Generate: `gen/turing/v1/go/turing/v1/*_grpc.pb.go`
- Generate: `turing-client/turing_app/lib/generated/turing/v1/*.pb.dart`
- Generate: `turing-client/turing_app/lib/generated/turing/v1/*.pbenum.dart`
- Generate: `turing-client/turing_app/lib/generated/turing/v1/*.pbgrpc.dart`
- Modify: `turing-client/turing_app/lib/models/grpc_mappers.dart`
- Modify: `turing-client/turing_app/test/models/grpc_mappers_test.dart`

- [ ] **Step 1: Write descriptor-only RED tests**

Add:

- `TestRunOutcomeProtoContractUsesApprovedAllocations`
- `TestRunOutcomeEnumsHaveUnspecifiedAndUnknownValues`
- `TestRuntimeApprovalResumeProtoContractUsesApprovedAllocations`
- `TestRunStateChangedReservesEventTypeTwentyThree`
- `TestRunFailedRetryableRemainsDeprecatedAtFieldFour`

Use `protoreflect` name/number lookup so the tests compile before generated
symbols exist.

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/tests \
  -run 'TestRunOutcomeProtoContract|TestRunOutcomeEnums|TestRuntimeApprovalResumeProtoContract|TestRunStateChanged|TestRunFailedRetryable' \
  -count=1
```

Expected RED: missing `RunState`, missing enum values, and missing additive fields;
the failure must identify the first absent descriptor rather than fail to
compile.

- [ ] **Step 2: Add the minimal wire-compatible definitions**

In `common.proto`:

- add `RunLifecycle` with `UNSPECIFIED=0`, `UNKNOWN=1`, then queued, running,
  waiting approval, recovering, completed, failed, cancelled;
- add `RunOutcomeReason` with `UNSPECIFIED=0`, `UNKNOWN=1`, `NONE=2`, and every
  approved terminal reason through `LEGACY_UNKNOWN`;
- add `RunState` with the approved fields and numbers;
- add `Message.run_state = 9`.

In `chat.proto`:

- add the approved `run_state` fields;
- mark `RunFailed.retryable = 4` deprecated without removing it;
- add `RunStateChanged` and oneof field 27.

In `events.proto`:

- import `turing/v1/common.proto` for `RunState`;
- reserve value 22 for TUR-004 when it is not yet present;
- add `AGENT_RUN_STATE_CHANGED = 23`;
- add `TuringEvent.run_state = 9`.

In `runtime.proto`:

- add `FailureOrigin` with explicit unspecified/unknown and all approved origins;
- add `AutomaticRetryClass` with unspecified, unknown, never, and
  same-run-transient;
- add all approved assignment, expected-version, Ready, Accepted, and state
  version fields without changing existing numbers.

In `tools.proto`:

- add `ToolPolicyDecision.run_state_version = 8`; retain merged fields 1 through
  7, including the provenance token.

- [ ] **Step 3: Generate with the pinned toolchain**

Run:

```bash
tools/proto/generate.sh
```

Expected GREEN prerequisite: generation succeeds with the pinned versions.

- [ ] **Step 4: Write the generated-oneof mapper RED test**

After generation, add:

- `maps run state changed chat event type and structural payload`;
- `maps persisted event type twenty three to agent run state changed`;
- `generated chat event oneof remains exhaustively mapped`.

Run:

```bash
( cd turing-client/turing_app && \
  flutter test test/models/grpc_mappers_test.dart \
    -n 'run state changed|exhaustively mapped' && \
  flutter analyze )
```

Expected RED: the newly generated `runStateChanged` oneof member makes
`_chatStreamEventType` and `_chatStreamPayload` non-exhaustive and has no
mapping.

- [ ] **Step 5: Add the minimal generated-oneof arms**

Map `runStateChanged` to durable type `agent.run.state_changed`, and add
`TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED` to `eventTypeToString` so EventService
replay and ChatStream `persistedEvent` do not collapse to `system`. Until Task 9
adds the semantic `RunState` model, expose only safe structural `runId` and
`stateVersion` payload values; never copy enum names, numbers as user-facing
text, or diagnostics.

- [ ] **Step 6: Run the contract tests and deterministic check**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/tests \
  -run 'TestRunOutcomeProtoContract|TestRunOutcomeEnums|TestRuntimeApprovalResumeProtoContract|TestRunStateChanged|TestRunFailedRetryable' \
  -count=1
tools/proto/check.sh
go build -tags sqlite_fts5 ./...
( cd turing-client/turing_app && \
  flutter test test/models/grpc_mappers_test.dart && \
  flutter analyze )
```

Expected GREEN: exact allocations match and a second generation changes no
tracked bytes.

- [ ] **Step 7: Refactor the tests for exhaustive enum membership**

Assert every approved enum member, not only count/min/max. Assert field 22 is
either TUR-004’s `SESSION_DELETED` or reserved in source and that field 23 is
TUR-009’s state-changed event.

- [ ] **Step 8: Commit the contract**

```bash
git add proto gen turing-client/turing_app/lib/generated \
  turing-client/turing_app/lib/models/grpc_mappers.dart \
  turing-client/turing_app/test/models/grpc_mappers_test.dart \
  turing-backend/tests/proto_contract_test.go
git commit -m "feat: add durable run outcome contracts" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 2d56d516-9e29-4b22-95f1-c851589f6ad3"
```

---

### Task 2: Build Typed Outcome, Content, and Time Primitives

**Files:**
- Create: `turing-backend/orchestrator-go/internal/runoutcome/outcome.go`
- Create: `turing-backend/orchestrator-go/internal/runoutcome/outcome_test.go`
- Create: `turing-backend/orchestrator-go/internal/runoutcome/content.go`
- Create: `turing-backend/orchestrator-go/internal/runoutcome/content_test.go`
- Create: `turing-backend/orchestrator-go/internal/runcorrelation/correlation.go`
- Create: `turing-backend/orchestrator-go/internal/runcorrelation/correlation_test.go`
- Conditionally create if TUR-008 has not landed:
  `turing-backend/orchestrator-go/internal/persisttime/time.go`
- Conditionally create if TUR-008 has not landed:
  `turing-backend/orchestrator-go/internal/persisttime/time_test.go`
- Otherwise modify the exact landed `persisttime` files without creating a
  second parser package
- Modify: `turing-backend/orchestrator-go/internal/repository/timestamps.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/timestamp_ordering_test.go`

- [ ] **Step 1: Write the failure mapping RED table**

Add `TestNormalizeFailureMapsEveryExistingCode` with one subtest for every row
in both approved mapping tables:

- run-terminal codes from `message_fetch_failed` through
  `automation_tool_not_allowlisted`;
- nonterminal `worker_busy` and `worker_unavailable`;
- subsidiary codes from `tool_policy_decision_failed` through current tool
  cleanup `cancelled`;
- typed provider/tool unknown-code fallbacks;
- unknown/unspecified origin fallback to internal failure;
- unknown/unspecified retry class treated as never.

Add:

- `TestAbandonedCancellationNeverClaimsUserIntent`
- `TestNormalizedFailuresExposeNoRawMessage`
- `TestRuntimeUnknownFailureOriginFailsClosed`
- `TestNormalizeRuntimeFailureRawWireUnknownEnumsFailClosed`
- `TestNormalizeFailureRunStepNoticeUsesOnlyAllowlistedCategoryAndAttempts`

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/runoutcome -count=1
```

Expected RED: package does not exist.

- [ ] **Step 2: Write shared correlation-validator RED tests**

Add:

- `TestValidateAcceptsOneBidirectionalAssistantLink`
- `TestValidateRejectsMismatchedRunMessageSessionRoleOrIdentity`
- `TestValidateReturnsOnlyTheValueFreeCorrelationSentinel`

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/runcorrelation -count=1
```

Expected RED: package does not exist.

- [ ] **Step 3: Implement private normalized values and correlation validation**

Implement private fields plus read-only accessors for allowlisted code, origin,
public reason, and automatic retry class. Do not accept or store an error
message. `NormalizeRuntimeFailure` must switch on generated enum values and map
every unrecognized numeric to semantic unknown/internal failure without calling
an enum name helper. For the raw-wire test, unmarshal a `RuntimeRunFailed`
containing unknown numeric origin/retry fields and pass its generated getters to
`NormalizeRuntimeFailure`; this exercises the real Go protobuf consumer.

Define one internal allowlisted notice category type for rewritten failure-like
`agent.run.step` events (`dispatch_retry`, `recovery_retry`, and
`recovery_exhausted`) plus bounded numeric attempt metadata. It accepts no
display string. Nonfailure redacted egress/model-limit notices stay on their
existing governed notice path.

Implement `AbandonedCancellation()` only. Do not add a user-cancel constructor
until a future typed cancel-intent RPC exists.

Implement `runcorrelation.Validate` without database or protobuf dependencies.
It returns only `run/message correlation conflict` and never formats input
values.

- [ ] **Step 4: Write exact content-presence RED vectors**

Add `TestHasDisplayableContentUsesTheApprovedUnicodeTable` covering:

| Input | Expected |
|---|---:|
| empty | false |
| U+0009 through U+000D | false |
| U+0020, U+0085, U+00A0, U+1680 | false |
| U+2000 through U+200A | false |
| U+2028, U+2029, U+202F, U+205F, U+3000 | false |
| `a` | true |
| U+200B | true |
| U+FFFD | true |
| whitespace around text | true |

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/runoutcome \
  -run TestHasDisplayableContent -count=1
```

Expected RED: helper is missing.

- [ ] **Step 5: Implement scalar-based content presence**

Implement the explicit Unicode table. Preserve original bytes; the helper only
returns a boolean. Add `ContentSHA256(string) string` for lowercase exact-byte
identity and test the empty-content digest plus a non-ASCII byte sequence.

- [ ] **Step 6: Write canonical time RED tests**

Whether extending landed TUR-008 code or creating the fallback package, add:

- `TestParseLegacyAcceptsApprovedRFC3339NanoShapes`
- `TestParseLegacyRejectsVariableOrInvalidShapesValueFree`
- `TestFormatUsesFixedWidthUTCNanoseconds`
- `TestNextStateTimeUsesNowOrPriorPlusOneNanosecond`
- `TestNextStateTimeRejectsUpperBoundOverflow`

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/persisttime -count=1
```

Expected RED: missing functions or missing package.

- [ ] **Step 7: Implement the minimal time API**

Required signatures:

```go
func ParseLegacy(string) (time.Time, error)
func Format(time.Time) string
func NextStateTime(now time.Time, prior string) (string, error)
```

Accept only the approved `Z` and `±HH:MM` RFC3339Nano forms, normalize to UTC,
format `2006-01-02T15:04:05.000000000Z`, and return value-free sentinel classes.
Make the existing `repository.FormatTimestamp` delegate to `persisttime.Format`
and retain its current tests/API, so migration and repository code cannot drift
between duplicate layout implementations.

- [ ] **Step 8: Run and commit the primitives**

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/runoutcome \
  ./turing-backend/orchestrator-go/internal/runcorrelation \
  ./turing-backend/orchestrator-go/internal/persisttime -count=1
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository \
  -run TestFormatTimestamp -count=1
go build -tags sqlite_fts5 ./...
git add turing-backend/orchestrator-go/internal/runoutcome \
  turing-backend/orchestrator-go/internal/runcorrelation \
  turing-backend/orchestrator-go/internal/persisttime \
  turing-backend/orchestrator-go/internal/repository/timestamps.go \
  turing-backend/orchestrator-go/internal/repository/timestamp_ordering_test.go
git commit -m "feat: normalize durable run outcomes" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 2d56d516-9e29-4b22-95f1-c851589f6ad3"
```

---

### Task 3: Add the Transactional, Bounded Migration

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/db/migrations.go`
- Create: `turing-backend/orchestrator-go/internal/db/run_outcomes_migration.go`
- Create: `turing-backend/orchestrator-go/internal/db/run_outcomes_migration_test.go`
- Create using Task 0’s selected number:
  `turing-backend/orchestrator-go/internal/db/schema/0017_run_outcomes.sql`
- Modify: `turing-backend/orchestrator-go/internal/db/migrations_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/jobs.go`
- Create: `turing-backend/orchestrator-go/internal/repository/run_outcome_migration_enqueue_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/telemetry_test.go`

- [ ] **Step 1: Write and run the embedded-migration RED test first**

Update `TestApplyMigrationsRecordsEmbeddedMigrationsInLexicalOrder` with the
selected exact migration name (`0017_run_outcomes`) and update
`TestCurrentSchemaVersionUsesLatestEmbeddedMigrationPrefix` from hardcoded
`0013` to Task 0's selected prefix. Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/db \
  -run 'TestApplyMigrationsRecordsEmbeddedMigrationsInLexicalOrder|TestCurrentSchemaVersionUsesLatestEmbeddedMigrationPrefix' \
  -count=1
```

Expected RED: the selected embedded migration is absent and the latest prefix
is still the prior merged-main value.

- [ ] **Step 2: Add only the marker-bearing migration stub**

Create the selected SQL file with the exact ordered `after-rebuild`,
`after-scrub`, and `after-indexes` marker comments and no schema mutation yet.
Re-run Step 1's selector. Expected minimal GREEN: both migration inventory tests
pass, while no hook or schema behavior exists yet.

- [ ] **Step 3: Write migration-hook ordering RED tests**

Add:

- `TestRunOutcomeMigrationRunsBeforeSQLAfterSQLRecordAndCommitInOrder`
- `TestOrdinaryMigrationsKeepExistingExecutionPath`
- `TestRunOutcomeMigrationRecordIsAtomicWithHooksAndSQL`

Use a test-only phase callback and an in-memory ordered slice. The production
hook shape is:

```go
type migrationHook struct {
	Before func(context.Context, *sql.Tx) error
	After  func(context.Context, *sql.Tx) error
}
```

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/db \
  -run 'TestRunOutcomeMigrationRunsBefore|TestOrdinaryMigrations|TestRunOutcomeMigrationRecord' \
  -count=1
```

Expected RED: no version-keyed hook mechanism.

- [ ] **Step 4: Implement hooks without changing ordinary migrations**

Register only the selected TUR-009 version. Execute:

1. `Before`;
2. SQL;
3. `After`;
4. migration-record insert;
5. commit.

Add a test-only callback after the migration-record insert and before commit.
For TUR-009 SQL only, split the SQL file on exact named marker comments for
`after-rebuild`, `after-scrub`, and `after-indexes`; execute each section inside
the same transaction and invoke the test callback after each marker. Reject a
missing, duplicate, or reordered marker. Ordinary SQL files still use one
`ExecContext`.

The TUR-009 parent-table rebuild requires a pinned `*sql.Conn` because every
run-owned child table references `agent_runs(id) ON DELETE CASCADE`. For this
migration only:

1. acquire one dedicated connection before beginning the transaction;
2. assert foreign keys are on;
3. set `PRAGMA foreign_keys=OFF` before `BeginTx`;
4. execute the complete migration transaction on that same connection;
5. run `PRAGMA foreign_key_check` after the rebuilt table and child rows are in
   place, before commit;
6. commit or roll back;
7. restore `PRAGMA foreign_keys=ON` on the pinned connection and verify it;
8. close the database and fail startup if restoration cannot be proven.

The existing single-connection limit prevents another migration/query from
using a different physical connection while the pinned one is held. Ordinary
migrations retain the current foreign-key-on path.
While the pinned connection is held, Before/After hooks and SQL-section
executors use only the passed `*sql.Tx`; any `database.Query*` or
`database.Exec*` call would wait forever for the one occupied connection.

- [ ] **Step 5: Write correlation and byte-bound RED tests**

Add:

- `TestRunOutcomeMigrationRejectsDuplicateRunAssistantCorrelationValueFree`
- `TestRunOutcomeMigrationRejectsDuplicateMessageRunCorrelationValueFree`
- `TestRunOutcomeMigrationAllowsNullAndSingleMismatchedLegacyCorrelation`
- `TestRunOutcomeMigrationAllowsExactlySixteenMiBSelectedData`
- `TestRunOutcomeMigrationSplitsAtOneHundredTwentyEightRows`
- `TestRunOutcomeMigrationSplitsBeforeExceedingSixteenMiB`
- `TestRunOutcomeMigrationRejectsOneOversizedRowValueFree`
- `TestRunOutcomeMigrationPreservesEveryRunOwnedChildRow`
- `TestRunOutcomeMigrationPreservesEveryExistingRunColumnAndForeignKey`
- `TestRunOutcomeMigrationRestoresForeignKeysAfterSuccessAndRollback`

Assert the complete error strings contain only:

- `run/message correlation conflict`; or
- `run outcome migration row exceeds byte limit`.

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/db \
  -run 'TestRunOutcomeMigrationRejectsDuplicate|TestRunOutcomeMigrationAllowsNull|TestRunOutcomeMigrationAllowsExactly|TestRunOutcomeMigrationSplits|TestRunOutcomeMigrationRejectsOneOversized|TestRunOutcomeMigrationPreservesEveryRunOwned|TestRunOutcomeMigrationPreservesEveryExisting|TestRunOutcomeMigrationRestoresForeignKeys|TestCurrentSchemaVersionUsesLatest' \
  -count=1
```

Expected RED: no preflight, no length pass, and no bounded batcher.

- [ ] **Step 6: Implement the bounded Before hook**

The hook must:

1. run value-free duplicate existence queries;
2. create a temporary backfill table;
3. scan runs by stable `rowid > ? ORDER BY rowid LIMIT 128`;
4. first select only `length(CAST(column AS BLOB))` for every variable-width
   field the data pass will read;
5. reject one row above 16 MiB before selecting its values;
6. stop a batch before its accumulated selected bytes exceed 16 MiB;
7. close every `Rows` cursor before any insert/update;
8. parse `finished_at`, then `started_at`, then `created_at`;
9. normalize and fixed-width-format the chosen timestamp;
10. derive lifecycle/outcome/content presence/content SHA-256;
11. insert only canonical data into the temporary table.

No transaction may perform file, model, tool, or network work.
Call `runcorrelation.Validate` for each scanned bidirectional link after the
value-free duplicate existence queries.

- [ ] **Step 7: Write semantic backfill RED tests**

Add:

- `TestRunOutcomeMigrationBackfillsEveryLifecycleAndOutcome`
- `TestRunOutcomeMigrationBackfillsUncertainOwnershipAsRecovering`
- `TestRunOutcomeMigrationMapsLegacyClientCancelledToAbandoned`
- `TestRunOutcomeMigrationBackfillsContentIdentityAndPresence`
- `TestRunOutcomeMigrationCanonicalizesOffsetAndVariableFractionTimestamps`
- `TestRunOutcomeMigrationRejectsInvalidTimestampValueFree`
- `TestRunOutcomeMigrationCreatesBidirectionalPartialUniqueIndexes`
- `TestRunOutcomeMigrationRecreatesEveryExistingRunIndex`
- `TestRunOutcomeMigrationPreservesTerminalRowsWithoutTerminalEvents`
- `TestPostMigrationEnqueueWritesCanonicalVersionOneFields`

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/db \
  -run 'TestRunOutcomeMigrationBackfills|TestRunOutcomeMigrationMapsLegacy|TestRunOutcomeMigrationCanonicalizes|TestRunOutcomeMigrationRejectsInvalid|TestRunOutcomeMigrationCreates|TestRunOutcomeMigrationRecreates|TestRunOutcomeMigrationPreservesTerminal' \
  -count=1
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository \
  -run TestPostMigrationEnqueueWritesCanonical \
  -count=1
```

Expected RED: canonical columns, recovering status, indexes, and backfill data
are absent and post-migration enqueue cannot yet supply them.

- [ ] **Step 8: Write the SQL rebuild**

The selected SQL file must:

- rebuild `agent_runs` with `recovering` allowed in `status`;
- preserve every merged-main `agent_runs` column, default, nullability,
  uniqueness rule, check constraint, and foreign key unless this approved
  contract explicitly changes it;
- require `state_version BETWEEN 1 AND 9223372036854775807`;
- require non-null canonical state fields;
- copy values only from the temporary backfill table;
- swap the table within the transaction;
- null `agent_runs.error_message`, `jobs.error_message`, and
  `tool_calls.error_message`;
- replace unknown diagnostic codes with canonical safe codes selected from
  trusted row context;
- create unique partial indexes for non-null
  `agent_runs.assistant_message_id` and assistant `messages.run_id`;
- recreate `idx_runs_session_created`, `idx_runs_status`,
  `idx_runs_session_execution_active`, `idx_runs_execution_recovery`, and
  `idx_runs_execution_recovery_ns` after the parent-table swap;
- leave approval comments, denial reasons, argument JSON, tokens, and audit
  rationale untouched.

In the same GREEN step, update atomic enqueue's `agent_runs` insert to supply
`state_version=1`, canonical `state_updated_at=created_at`,
`outcome_reason=none`, and the exact empty-content SHA-256. Do not yet add the
queued event snapshot; Task 4's enqueue RED remains red on that missing
projection. Update direct latest-schema run fixtures in
`repository/telemetry_test.go` and applicable migration tests to supply the same
required canonical fields. Preserve the existing single enqueue transaction and
SendMessage idempotency identity.

The child-survival test seeds and verifies rows in `agent_run_steps`, `jobs`,
`events`, `tool_calls`, `approvals`, `automation_runs`, and
`send_message_idempotency`. It also runs `PRAGMA foreign_key_check` after
reopen. The schema-preservation test snapshots `PRAGMA table_info`,
`foreign_key_list`, and index metadata before migration and proves that only the
approved additions/constraint widening differ afterward.

- [ ] **Step 9: Write public-event rewrite and rationale RED tests**

Add one table-driven test,
`TestRunOutcomeMigrationRewritesThePublicFailureInventory`, with rows for:

| Event type | Required safe projection |
|---|---|
| `agent.run.failed` | canonical run state only |
| `agent.run.cancelled` | canonical run state only |
| retry/give-up/recovery `agent.run.step` | version, allowlisted category, numeric attempts |
| `approval.denied` | intended IDs plus allowlisted category |
| `approval.expired` | intended IDs plus allowlisted category |
| `tool.call.failed` | intended tool-call identity plus category |
| `tool.call.denied` | intended tool-call identity plus category |

Also add:

- `TestRunOutcomeMigrationPreservesNonfailureRunStepNotices`
- `TestRunOutcomeMigrationPreservesApprovalRationaleOnlyInGovernedStorage`
- `TestRunOutcomeMigrationNeverCopiesRationaleIntoRunStateOrEvents`
- `TestRunOutcomeMigrationScrubsAllRawDiagnosticColumns`

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/db \
  -run 'TestRunOutcomeMigrationRewrites|TestRunOutcomeMigrationPreservesNonfailure|TestRunOutcomeMigrationPreservesApproval|TestRunOutcomeMigrationNeverCopies|TestRunOutcomeMigrationScrubs' \
  -count=1
```

Expected RED: legacy payloads still contain message/note/reason/error fields and
raw diagnostic columns remain populated.

- [ ] **Step 10: Implement the bounded After hook**

Scan event rows with the same 128-row and 16 MiB limits, close readers before
writes, encode canonical JSON with Go’s JSON encoder, validate canonical fields
and bidirectional correlations, then drop the temporary table.

Sanitize only the failure inventory. Preserve `message.completed`, redacted
egress/model-limit notices, `approvals.approval_comment`,
`approvals.denial_reason`, and governed bounded audit rationale.

- [ ] **Step 11: Write rollback RED tests for every boundary**

Add `TestRunOutcomeMigrationRollsBackEveryPhase` with subtests:

- before SQL;
- after table rebuild;
- after raw-field scrub;
- after index creation;
- after event rewrite;
- before migration-record insert;
- after migration-record insert, before commit.

Each subtest must inject an error, close and reopen the database, then prove the
old table shape, old data, old event JSON, absent indexes, absent migration row,
unchanged child rows, unchanged rationale, `PRAGMA foreign_keys=1`, and an empty
`PRAGMA foreign_key_check`.

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/db \
  -run TestRunOutcomeMigrationRollsBackEveryPhase -count=1
```

Expected RED: the requested phase seam is missing or partial changes survive.

- [ ] **Step 12: Make all migration tests GREEN and refactor shared fixtures**

Move legacy-schema fixture creation and schema/index assertions into test
helpers. Keep failure values and expected errors literal and value-free.

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/db -count=1
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository -count=1
go test -tags sqlite_fts5 ./... -count=1
go build -tags sqlite_fts5 ./...
```

- [ ] **Step 13: Commit the migration**

```bash
git add turing-backend/orchestrator-go/internal/db \
  turing-backend/orchestrator-go/internal/repository/jobs.go \
  turing-backend/orchestrator-go/internal/repository/run_outcome_migration_enqueue_test.go \
  turing-backend/orchestrator-go/internal/repository/telemetry_test.go
git commit -m "feat: migrate durable run outcomes" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 2d56d516-9e29-4b22-95f1-c851589f6ad3"
```

---

### Task 4: Make Repository Lifecycle Transitions Versioned and Idempotent

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/repository/runs.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/jobs.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/assignments.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/approvals.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/events.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/toolcalls.go`
- Create: `turing-backend/orchestrator-go/internal/repository/run_state_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/cancellation_lifecycle_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/retry_requeue_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/assignment_recovery_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/stale_approval_recovery_regression_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/terminal_dependents_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/approval_consumption_regression_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/session_delete_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/assignment_requeue_notice_test.go`

- [ ] **Step 1: Write enqueue and transition RED tests**

Add:

- `TestEnqueueCreatesQueuedVersionOneWithMatchingEventSnapshot`
- `TestEnqueueValidatesBothDirectionsOfRunMessageCorrelation`
- `TestRealLifecycleTransitionIncrementsVersionAndAppendsOneProjection`
- `TestSemanticDuplicatePreservesVersionTimestampAndEventCount`
- `TestConflictingNonterminalDuplicateIsFenced`
- `TestTerminalRowsRejectEveryLaterTransition`
- `TestTransitionRejectsZeroNegativeAndMaxInt64Version`
- `TestTransitionAtMaxInt64ReturnsRunStateVersionExhausted`
- `TestTransitionTimeAdvancesWhenClockRegresses`
- `TestTransitionTimeOverflowRollsBackRowAndEvent`

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository \
  -run 'TestEnqueueCreatesQueued|TestEnqueueValidatesBoth|TestRealLifecycle|TestSemanticDuplicate|TestConflictingNonterminal|TestTerminalRowsReject|TestTransitionRejects|TestTransitionTime' \
  -count=1
```

Expected RED: enqueue has version-1 row fields from Task 3 but its queued event
lacks the matching snapshot, and transitions do not use expected version/event
projections.

- [ ] **Step 2: Add one guarded transition core**

Create an internal transaction helper that:

- reads lifecycle, version, canonical time, correlation, current assignment, and
  content identity;
- validates allowed from/to lifecycle and reason matrix;
- detects exact nonterminal semantic duplicates using run/message IDs plus
  approval ID, authenticated worker ID, assignment attempt ID, or recovery
  trigger identity where applicable;
- computes `version+1` and monotonic time;
- executes `UPDATE ... WHERE id=? AND status=? AND state_version=?`;
- appends exactly one canonical event snapshot;
- returns the committed `RunState`.

Keep terminal wrappers separate for completion, failure, and cancellation
because they have different identity requirements.
The version-exhaustion test must assert the exact value-free sentinel
`run state version exhausted`.
Atomic enqueue calls `runcorrelation.Validate` after creating both rows and
before inserting the queued event or job.

- [ ] **Step 3: Write the recovering-predicate RED audit, then convert writers**

Before changing predicates, run:

```bash
rg -n "running|waiting_approval|queued|recovering" \
  turing-backend/orchestrator-go/internal --glob '*.go'
rg -n "status.*running|runStatus.*running|isActiveRunStatus|== \"running\"|case \"running\"" \
  turing-backend/orchestrator-go/internal --glob '*.go'
```

Classify every SQL and Go predicate in repository and service code rather than
mechanically adding `recovering`:

- lease recovery scans, terminal eligibility, cancellation, execution-exit
  gating, and stale-assignment cleanup must continue to see recovering runs;
- lease renewal and approval creation must not treat uncertain ownership as
  proven running ownership;
- resume and requeue require their explicit expected-version/attempt guards.

Add:

- `TestRecoveringRunRemainsVisibleToRecoveryScan`
- `TestRecoveringRunCanCompleteFailOrCancelAtExpectedVersion`
- `TestRecoveringRunDoesNotRenewUnprovenOwnership`
- `TestRecoveringRunCannotCreateASecondApproval`
- `TestRecoveringRunPreservesExecutionExitGating`
- `TestRecoveringRunOnlyResumesOrRequeuesThroughGuardedTransitions`
- `TestRecoveringRunCanTerminalizeApproval`
- `TestRecoveringRunBlocksSessionDeletionAsActive`

Run these tests before changing a status predicate:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository \
  -run 'TestRecoveringRun' -count=1
```

Expected RED: recovering is absent from required recovery/terminal paths or is
incorrectly accepted as proven running ownership.

Then update:

- atomic enqueue's queued event to project the already-written version-1 state;
- assignment start to queued → running;
- approval required to running → waiting approval;
- worker uncertainty/fencing to running or waiting approval → recovering;
- a current assignment whose command is proven unsent, or whose authenticated
  current attempt explicitly reports a same-run-transient failure, to running →
  queued in the same transaction that releases the job;
- proof by the same still-owned attempt to recovering → running;
- lease/retry requeue to recovering → queued.

The running → queued edge is not a general retry shortcut. The repository accepts
it only through a confirmed-release input carrying the current version and
assignment identity, or through the exact pre-delivery job/attempt guards. A
stream loss, unresolved send, stale attempt, lease expiry, or other uncertain
owner must commit active → recovering and recovering → queued as two real
transitions in one short transaction with two incremented versions and two
ordered event projections.

- [ ] **Step 4: Write terminal identity RED tests**

Add:

- `TestCompleteRunPersistsExactContentIdentityAndDisplayability`
- `TestWhitespaceOnlyExplicitSuccessCompletesWithoutContent`
- `TestFailedAndCancelledRunsPreserveExistingAssistantContent`
- `TestExactDuplicateTerminalReportIsAWriteFreeSuccess`
- `TestDuplicateCompletionWithDifferentBytesIsFenced`
- `TestDuplicateTerminalWithDifferentReasonAssistantOrVersionIsFenced`
- `TestCompletionCancellationAndRecoveryRacesLinearize`
- `TestTerminalTransitionAppendsExactlyOneTerminalEvent`
- `TestAllSevenTemporaryRawTerminalAdaptersDelegateToCanonicalWriters`

Use barriers so competing goroutines reach the guarded update together.

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository \
  -run 'TestCompleteRunPersists|TestWhitespaceOnly|TestFailedAndCancelled|TestExactDuplicateTerminal|TestDuplicateCompletion|TestDuplicateTerminal|TestCompletionCancellation|TestTerminalTransitionAppends|TestAllSevenTemporaryRawTerminalAdapters' \
  -count=1
```

Expected RED: duplicate matching is based on incomplete status/payload checks,
empty completion is rejected, and content identity is absent.

- [ ] **Step 5: Replace raw terminal writer signatures**

Implement the planned `CompleteRunCanonical`, `FailRunCanonical`, and
`CancelRunCanonical` methods with typed inputs. The distinct canonical names
avoid colliding with the current raw bare methods while the compatibility
boundary exists. Event builders accept committed state or normalized subsidiary
categories only.
Remove or keep unexported any link-update helper; repository APIs must not
expose an operation that mutates `messages.run_id` or
`agent_runs.assistant_message_id` after enqueue.

Keep all seven existing raw-signature methods — bare `CompleteRun`, `FailRun`,
and `CancelRun`, plus the four `*WithEvent` variants — as explicitly temporary
adapters so untouched ChatService/RuntimeService/tests compile at this commit
boundary. Each adapter delegates terminal mutation to the corresponding
canonical method, while preserving its old raw ingestion behavior long enough
for Task 6's persistence/origin tests to observe RED. The delegation RED test
covers every adapter. Mark all seven for Task 6 removal; no new caller may use
them.
Because raw signatures carry no expected version, each adapter resolves the
current version inside the same short transaction as the guarded canonical
transition through an unexported transaction-local path; it never passes public
version zero and never performs a read-then-write TOCTOU. The seven-adapter test
uses a barrier to prove concurrent transition/version changes fence correctly.

Completion:

- requires an explicit terminal report;
- permits empty/approved-whitespace content;
- persists the exact bytes;
- computes SHA-256 and displayability in the same transaction.

Failure/cancellation:

- preserve already durable assistant content and its identity;
- never write success fallback text;
- terminalize from queued, running, waiting approval, or recovering only at the
  expected version.

- [ ] **Step 6: Convert repository-owned transition and subsidiary writers**

Do not convert external ChatService, RuntimeService, telemetry, or test callers
yet. Task 6 must first observe its typed-ingestion RED tests through the
temporary adapters.

Update repository-owned paths so:

- exhausted retries use normalized `retries_exhausted`;
- approval expiry/delivery failure use typed outcomes;
- retry/give-up/recovery events store safe categories and numbers, not notes;
- tool failure/denial writers accept normalized subsidiary categories;
- `worker_busy` and `worker_unavailable` stay nonterminal with reason `none`.

Update `assignment_requeue_notice_test.go` in this same step: replace exact
retry/give-up prose assertions with exact allowlisted category and bounded
attempt metadata assertions. Nonfailure notice tests retain their governed copy.

- [ ] **Step 7: Prove the Task 4 commit boundary compiles**

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository -count=1
go test -tags sqlite_fts5 -race ./turing-backend/orchestrator-go/internal/repository -count=1
go test -tags sqlite_fts5 ./... -count=1
go build -tags sqlite_fts5 ./...
```

Expected: repository tests and the root build pass through the temporary
adapters. The adapter test proves this boundary is deliberate and delegates
terminal mutation to the typed implementation.

- [ ] **Step 8: Commit the repository state machine**

```bash
git add turing-backend/orchestrator-go/internal/repository
git commit -m "feat: version durable run lifecycle transitions" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 2d56d516-9e29-4b22-95f1-c851589f6ad3"
```

---

### Task 5: Join Authoritative Run State into Message History

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/repository/sessions.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/sessions_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/history_anchor_regression_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/sessions/service.go`
- Modify: `turing-backend/orchestrator-go/internal/service/sessions/service_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/sessions/history_anchor_regression_test.go`
- Create: `turing-backend/orchestrator-go/internal/service/runstate/projection.go`
- Create: `turing-backend/orchestrator-go/internal/service/runstate/projection_test.go`

- [ ] **Step 1: Write zero-or-one join RED tests**

Add:

- `TestListMessagesEmbedsMatchingRunStateWithoutChangingCardinality`
- `TestListMessagesBeforeAppliesBoundaryBeforeRunProjection`
- `TestListMessagesOmitsStateForNullOrSingleMismatchedLegacyCorrelation`
- `TestListMessagesRejectsValueFreeDuplicateCorrelation`
- `TestOverlappingMessagePagesKeepOneMessageAndOneRunVersion`
- `TestRunProjectionDoesNotIssuePerMessageQueries`

Use a test-only counting `database/sql/driver` wrapper around the repository's
registered SQLite driver for the last test; count `QueryContext` calls and
assert one history query, not one query per row. Do not depend on an unavailable
SQLite trace API.

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository \
  -run 'TestListMessagesEmbeds|TestListMessagesBeforeApplies|TestListMessagesOmits|TestListMessagesRejects|TestOverlappingMessagePages|TestRunProjectionDoesNot' \
  -count=1
```

Expected RED: `Message` has no state and history selects only message columns.

- [ ] **Step 2: Add the guarded primary-key join**

Extend repository `Message` with `RunState *RunState`. Both newest and
before-anchor queries must:

- apply the existing message boundary/limit to message rows;
- left-join `messages.run_id` to the run primary key;
- return state only when assistant role, session ID, run ID, and mirrored
  assistant message ID all agree;
- preserve ordering and cardinality;
- omit state for null/single mismatch;
- fail value-free if duplicate ownership is detected despite migration guards.

If TUR-008 has landed, retain its cursor and archive/status predicates exactly.
Call `runcorrelation.Validate` for every non-null candidate link; do not copy its
field checks into the history mapper.

- [ ] **Step 3: Write protobuf projection RED tests**

Add:

- `TestRunStateProjectionMapsEveryKnownLifecycleAndReason`
- `TestRunStateProjectionMapsUnknownStoredStringsToSemanticUnknown`
- `TestRunStateProjectionOmitsContentHashAndInternalExecution`
- `TestListMessagesRoundTripsRunStateAfterDatabaseReopen`

This mapper consumes repository strings, not decoded protobuf. Its unknown test
therefore seeds unknown stored lifecycle/reason strings. Raw-wire numeric
compatibility is exercised at the real generated-enum consumers in Task 2
(`NormalizeRuntimeFailure`), Task 8 (public Chat/Event decode), and Task 9
(Flutter mapping); do not invent a protobuf-to-protobuf projection boundary.

Run:

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/service/runstate \
  ./turing-backend/orchestrator-go/internal/service/sessions \
  -run 'TestRunStateProjection|TestListMessagesRoundTrips' -count=1
```

Expected RED: common mapper and `Message.run_state` population are missing.

- [ ] **Step 4: Implement one shared safe mapper**

Map known repository strings exhaustively. Default/unrecognized lifecycle or
reason maps to explicit protobuf `UNKNOWN`; invalid canonical pair omits state
and returns the neutral legacy path. Never map raw numbers, diagnostic fields,
content hash, worker ID, assignment attempt, arguments, or tokens.

- [ ] **Step 5: Run and commit history projection**

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/repository \
  ./turing-backend/orchestrator-go/internal/service/runstate \
  ./turing-backend/orchestrator-go/internal/service/sessions -count=1
go test -tags sqlite_fts5 ./... -count=1
go build -tags sqlite_fts5 ./...
git add turing-backend/orchestrator-go/internal/repository/sessions.go \
  turing-backend/orchestrator-go/internal/repository/*sessions*test.go \
  turing-backend/orchestrator-go/internal/repository/history_anchor_regression_test.go \
  turing-backend/orchestrator-go/internal/service/runstate \
  turing-backend/orchestrator-go/internal/service/sessions
git commit -m "feat: embed run outcomes in message history" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 2d56d516-9e29-4b22-95f1-c851589f6ad3"
```

---

### Task 6: Normalize Runtime Failures and Terminal Reports at Ingestion

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/repository/runs.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/run_state_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/repository_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/timestamp_ordering_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/telemetry_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/late_safe_after_regression_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/global_capacity_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/terminal_dependents_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/cancellation_lifecycle_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/session_delete_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/chat/service.go`
- Modify: `turing-backend/orchestrator-go/internal/service/chat/service_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/service.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/service_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/terminal_idempotency_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/late_terminal_identity_regression_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/retryable_failure_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/recovery_lifecycle_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/capability_lifecycle_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/lifecycle_regression_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/command_liveness_regression_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/automation_approval_test.go`
- Modify: `turing-backend/agent-runtime-go/internal/agent/general_assistant.go`
- Modify: `turing-backend/agent-runtime-go/internal/agent/general_assistant_test.go`
- Modify: `turing-backend/agent-runtime-go/internal/llm/ollama.go`
- Modify: `turing-backend/agent-runtime-go/internal/llm/ollama_test.go`
- Modify: `turing-backend/agent-runtime-go/internal/llm/openai_compatible.go`
- Modify: `turing-backend/agent-runtime-go/internal/llm/openai_compatible_test.go`
- Modify: `turing-backend/agent-runtime-go/internal/tools/runner.go`
- Modify: `turing-backend/agent-runtime-go/internal/tools/runner_test.go`
- Modify: `turing-backend/agent-runtime-go/internal/worker/worker.go`
- Modify: `turing-backend/agent-runtime-go/internal/worker/worker_test.go`
- Modify: `turing-backend/agent-runtime-go/internal/worker/terminal_race_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/telemetry/service_test.go`

- [ ] **Step 1: Write assignment/version RED tests**

Add:

- `TestAssignedJobCarriesExpectedStateVersionAndDurableAttemptID`
- `TestWorkerEchoesExpectedVersionOnCompletionAndFailure`
- `TestTerminalReportWithWrongExpectedVersionIsFenced`
- `TestWorkerKeepsHighestAcceptedVersionPerAssignment`
- `TestWorkerPausesOutboundRunUpdatesUntilSameAttemptRefresh`
- `TestGenericRuntimeEventCannotResumeRecoveringWithoutVersionReply`
- `TestSameAttemptAssignmentRefreshResumesRecoveringAndUpdatesWorkerVersion`
- `TestToolBeaconProofReturnsResultingVersionBeforeContinuation`
- `TestTerminalAfterFenceAndVersionedProofCommitsExactlyOnce`
- `TestUnownedUpdateCannotBypassRecoveringFence`
- `TestIsActiveRunStatusRejectsUnprovenRecoveringOwnership`

Run:

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/service/runtime \
  ./turing-backend/agent-runtime-go/internal/worker \
  -run 'TestAssignedJobCarries|TestWorkerEchoes|TestTerminalReportWithWrong|TestWorkerKeepsHighest|TestWorkerPausesOutbound|TestGenericRuntimeEvent|TestSameAttemptAssignmentRefresh|TestToolBeaconProof|TestTerminalAfterFence|TestUnownedUpdate|TestIsActiveRunStatus' \
  -count=1
```

Expected RED: assignments and terminal updates omit version/attempt identity.

- [ ] **Step 2: Thread version and attempt identity**

Populate `AgentJob.expected_state_version` and
`AgentJob.assignment_attempt_id`. The worker stores both on the active
assignment, rejects lower/conflicting command versions, and echoes the expected
pre-terminal version on terminal reports.

No lifecycle transition may advance beyond the worker's highest known version
without returning that resulting version on an orchestrator command:

- a generic runtime event has no response command, so it cannot itself prove
  recovering → running and remains fenced;
- reconnect/heartbeat ownership proof commits recovering → running, then sends
  a same-attempt `RunAssigned` refresh carrying the unchanged assignment attempt
  and resulting `expected_state_version`; the worker recognizes it as a refresh,
  updates its version, and does not start a second executor;
- after stream loss, the worker pauses outbound run updates until that
  same-attempt refresh arrives;
- a matching tool beacon may prove ownership because its
  `ToolPolicyDecision.run_state_version` response returns the committed version
  before tool/model work continues;
- delivery failure of either version-bearing response uses the common ownership
  fence and leaves recovering truth durable.

An unowned or conflicting update remains fenced. Do not make the generic
`isActiveRunStatus` helper treat uncertain recovering ownership as running.
In RuntimeService's tool-beacon handler, insert the authenticated
same-attempt recovery-proof branch before the existing
`!isActiveRunStatus(run.Status)` rejection; leave the generic helper rejecting
recovering. The proof branch must validate ownership, transition, and return the
resulting version in the decision before processing the beacon.
The composed RED test must prove a same-attempt terminal report after
fence → recovering → versioned proof commits exactly once rather than being
rejected as stale.

- [ ] **Step 3: Write typed-failure RED tests at every origin**

Add table-driven cases proving call sites supply typed origin rather than infer
from text:

- context/history assembly;
- external provider and provider configuration;
- provider protocol, transport, auth/quota/status, malformed chunk, and output
  guard;
- tool infrastructure, execution, guard, and policy;
- approval transport and expiry;
- automation policy;
- worker runtime;
- dispatch and recovery;
- orchestrator internal;
- client lifecycle.

Add:

- `TestProviderControlledTextCannotChangePublicOutcome`
- `TestRuntimeFailureMessageIsNeverPersisted`
- `TestUnknownOriginAndRetryClassFailClosed`
- `TestChatDisconnectNormalizesToAbandonedWithoutRawReason`
- `TestApprovalAndToolCallersSupplyTypedOriginBeforePersistence`

Run:

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/agent-runtime-go/internal/agent \
  ./turing-backend/agent-runtime-go/internal/llm \
  ./turing-backend/agent-runtime-go/internal/tools \
  ./turing-backend/agent-runtime-go/internal/worker \
  ./turing-backend/orchestrator-go/internal/service/chat \
  ./turing-backend/orchestrator-go/internal/service/runtime \
  -run 'Test.*TypedFailure|TestProviderControlledText|TestRuntimeFailureMessage|TestUnknownOrigin|TestChatDisconnectNormalizes|TestApprovalAndToolCallers' \
  -count=1
```

Expected RED: raw `err.Error()` and caller booleans still cross the runtime
boundary.

- [ ] **Step 4: Supply typed origins and normalize before writes**

Runtime producers set only:

- typed `FailureOrigin`;
- allowlisted code;
- typed automatic retry class;
- expected version.

Legacy message is empty or a fixed generic compatibility value. The
orchestrator calls `NormalizeRuntimeFailure` before any run, job, tool-call,
approval, or event writer. Ignore legacy `retryable` for unknown origin/code
pairs and serialize public `RunFailed.retryable` as false.

After every Step 3 test has been observed RED, convert every production and test
caller of all seven temporary raw terminal adapters: ChatService disconnect,
RuntimeService completion/failure/tool-beacon/approval-delivery paths, all
repository/runtime regression fixtures, and telemetry's completion fixture.
Use `AbandonedCancellation` for the ambiguous disconnect. Remove the temporary
adapters and their boundary test only after the last caller is converted. Keep
the explicit `*Canonical` names permanently; the ambiguous bare names are
retired and the obsolete-definition guard reserves them from reintroduction.
Update `automation_approval_test.go` to assert the typed policy outcome and
safe projection rather than requiring automation/tool names in
`agent_runs.error_message`; that raw diagnostic column must remain null.

- [ ] **Step 5: Write explicit-success and EOF RED tests**

Add:

- `TestExplicitEmptySuccessCompletesWithoutSynthesizedText`
- `TestToolIterationLimitEmptySuccessDoesNotSynthesizeText`
- `TestProviderEOFWithoutExplicitFinishNeverCompletes`
- `TestDisconnectWithoutTerminalReportMovesThroughRecovery`
- `TestDurablePartialContentSurvivesFailureButLiveDeltaIsNotPromised`

Run:

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/agent-runtime-go/internal/agent \
  ./turing-backend/orchestrator-go/internal/service/runtime \
  -run 'TestExplicitEmptySuccess|TestToolIterationLimitEmpty|TestProviderEOF|TestDisconnectWithout|TestDurablePartialContent' \
  -count=1
```

Expected RED: empty successes are replaced with fallback text or rejected, and
EOF can be confused with success.

- [ ] **Step 6: Remove success fallbacks and require explicit finish**

Remove both empty-success fallback strings. An explicit terminal completion may
carry empty/whitespace content; EOF, disconnect, or context loss without an
explicit successful report must return an interruption/failure signal and let
the ownership fence enter recovery.

- [ ] **Step 7: Strengthen terminal duplicate/race tests**

Update existing terminal idempotency tests to compare:

- expected and resulting version;
- lifecycle and outcome;
- assistant message ID;
- exact content SHA-256 and displayability;
- exact bytes for completion.

Keep late matching terminal updates as execution-exit acknowledgements only
when the complete identity and owned attempt match.

- [ ] **Step 8: Run runtime and worker packages with race detection**

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/service/runtime \
  ./turing-backend/agent-runtime-go/internal/agent \
  ./turing-backend/agent-runtime-go/internal/llm \
  ./turing-backend/agent-runtime-go/internal/tools \
  ./turing-backend/agent-runtime-go/internal/worker -count=1
go test -tags sqlite_fts5 -race \
  ./turing-backend/orchestrator-go/internal/service/runtime \
  ./turing-backend/agent-runtime-go/internal/worker -count=1
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/repository \
  ./turing-backend/orchestrator-go/internal/service/chat \
  ./turing-backend/orchestrator-go/internal/service/telemetry -count=1
bare_status=0
rg -n -F \
  -e 'func (r *Repository) CompleteRun(' \
  -e 'func (r *Repository) FailRun(' \
  -e 'func (r *Repository) CancelRun(' \
  turing-backend/orchestrator-go/internal/repository --glob '*.go' || bare_status=$?
if [ "$bare_status" -eq 0 ]; then
  echo "obsolete bare terminal writer remains" >&2
  exit 1
elif [ "$bare_status" -ne 1 ]; then
  exit "$bare_status"
fi
event_status=0
rg -n -F \
  -e 'CompleteRunWithEvent(' \
  -e 'FailRunWithEvent(' \
  -e 'FailRunWithEventPreservingExecution(' \
  -e 'CancelRunWithEvent(' \
  turing-backend --glob '*.go' || event_status=$?
if [ "$event_status" -eq 0 ]; then
  echo "obsolete event terminal writer remains" >&2
  exit 1
elif [ "$event_status" -ne 1 ]; then
  exit "$event_status"
fi
go build -tags sqlite_fts5 ./...
```

- [ ] **Step 9: Commit typed runtime outcomes**

```bash
git add turing-backend/orchestrator-go/internal/repository \
  turing-backend/orchestrator-go/internal/service/chat \
  turing-backend/orchestrator-go/internal/service/runtime \
  turing-backend/orchestrator-go/internal/service/telemetry/service_test.go \
  turing-backend/agent-runtime-go/internal
git commit -m "feat: ingest typed runtime outcomes" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 2d56d516-9e29-4b22-95f1-c851589f6ad3"
```

---

### Task 7: Implement the Approval Ready/Accepted Fencing Protocol

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/service.go`
- Create: `turing-backend/orchestrator-go/internal/service/runtime/approval_resume_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/recovery_lifecycle_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/approvals.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/assignments.go`
- Modify: `turing-backend/agent-runtime-go/internal/tools/runner.go`
- Modify: `turing-backend/agent-runtime-go/internal/tools/runner_test.go`
- Modify: `turing-backend/agent-runtime-go/internal/worker/worker.go`
- Create: `turing-backend/agent-runtime-go/internal/worker/approval_resume_test.go`
- Modify: `turing-backend/agent-runtime-go/internal/worker/outbound_lifecycle_test.go`

- [ ] **Step 1: Write the complete Ready/Accepted RED matrix**

Add deterministic tests:

| Test | Required durable result |
|---|---|
| `TestApprovalDecisionDeliveryFailureBeforeReadyKeepsWaitingWhileOwned` | waiting approval, same version |
| `TestApprovalDecisionDeliveryFailureWithUncertainOwnerEntersRecovering` | recovering, version +1 |
| `TestApprovalReadyCommitsRunningBeforeAccepted` | running, version +1, one event |
| `TestLostAcceptedSameAttemptReadyReplaysExactResponse` | same Accepted/version, no write/event |
| `TestReadyWithConflictingApprovalIsFenced` | no mutation |
| `TestReadyWithConflictingWorkerIsFenced` | no mutation |
| `TestReadyWithConflictingAttemptIsFenced` | no mutation |
| `TestReadyWithConflictingExpectedVersionIsFenced` | no mutation |
| `TestDetectedAcceptedDeliveryFailureMovesRunningToRecovering` | recovering, another version/event |
| `TestUnobservedAcceptedThenOwnershipLossMovesRunningToRecovering` | recovering through common fence |
| `TestWorkerCannotContinueToolOrModelBeforeAccepted` | no side effect before matching command |
| `TestReadyWaitUsesRemainingApprovalDeadline` | bounded by existing approval wait deadline |
| `TestNeverAnsweredReadyFailsOrRecoversWithoutHoldingWorkerSlot` | typed approval-delivery failure while owned/waiting, otherwise recovery |
| `TestMismatchedAcceptedDoesNotExtendReadyDeadline` | same original deadline |
| `TestConflictingReadyClosesStreamFencesRecoveryAndReleasesSlot` | no Ready transition; common ownership fence and worker exit |

Use fake sender gates and explicit channels:

1. block command delivery;
2. observe repository commit;
3. release or fail delivery;
4. assert no tool/model continuation channel fired early.

Run:

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/service/runtime \
  ./turing-backend/agent-runtime-go/internal/worker \
  ./turing-backend/agent-runtime-go/internal/tools \
  -run 'TestApprovalDecisionDelivery|TestApprovalReadyCommits|TestLostAccepted|TestReadyWithConflicting|TestDetectedAccepted|TestUnobservedAccepted|TestWorkerCannotContinue|TestReadyWaitUses|TestNeverAnsweredReady|TestMismatchedAccepted|TestConflictingReadyCloses' \
  -count=1
```

Expected RED: no Ready/Accepted branches or owned-attempt replay identity exist.

- [ ] **Step 2: Pause the worker at the approval boundary**

When an approval-required tool call is created:

- preserve the approval ID and `ToolPolicyDecision.run_state_version`;
- require the matching approved `RuntimeApprovalUpdated` command before Ready;
- allow token polling to continue, but do not treat token availability or
  consumption as lifecycle resume;
- emit `RuntimeApprovalResumeReady` with run, approval, expected waiting version,
  and assignment attempt;
- have the worker’s emit path send Ready and block the executor until the
  matching Accepted command is delivered to the active run;
- reject a mismatched Accepted and keep the executor paused without resetting
  the deadline;
- bound the Ready → Accepted wait by the remaining existing approval-wait
  deadline and the run context, whichever ends first;
- if the deadline expires while the row is still waiting approval and ownership
  is proven, emit typed `approval_delivery_failed` at the known expected version;
- if Ready already committed or ownership is uncertain, close the stream so the
  approved running state follows the required ownership-loss fence to
  recovering;
- always cancel the active executor and release its worker concurrency slot on
  this exit path.

- [ ] **Step 3: Commit/replay Ready in the orchestrator**

On an authenticated worker stream, validate:

- run ID;
- approval ID;
- server-authenticated worker ID;
- durable assignment attempt ID;
- expected waiting-approval version;
- live assignment ownership.

First Ready commits waiting approval → running and appends one state event.
An identical Ready when the row is running at exactly `expected+1` reconstructs
the exact Accepted response from durable run/job/approval identity, with no
write or event. Any differing trigger identity is fenced.
An invalid/conflicting Ready returns `FailedPrecondition`, closes the offending
worker stream, and invokes the common ownership fence. The Ready handler itself
does not commit its requested transition; the stream fence may honestly move
waiting approval to recovering. It never leaves a paused worker slot alive.

- [ ] **Step 4: Fence post-commit Accepted delivery failure**

The command sender must report whether Accepted was delivered. If send fails
after Ready committed running:

1. invoke the same repository ownership-loss fence;
2. commit running → recovering with another version/event;
3. publish that event;
4. close/fail the worker stream.

Never revert to or report waiting approval after the Ready commit.

- [ ] **Step 5: Run focused race tests repeatedly**

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/service/runtime \
  ./turing-backend/agent-runtime-go/internal/worker \
  ./turing-backend/agent-runtime-go/internal/tools \
  -run 'TestApprovalDecisionDelivery|TestApprovalReadyCommits|TestLostAccepted|TestReadyWithConflicting|TestDetectedAccepted|TestUnobservedAccepted|TestWorkerCannotContinue|TestReadyWaitUses|TestNeverAnsweredReady|TestMismatchedAccepted|TestConflictingReadyCloses' \
  -count=20
go test -tags sqlite_fts5 -race \
  ./turing-backend/orchestrator-go/internal/service/runtime \
  ./turing-backend/agent-runtime-go/internal/worker -count=1
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/repository -count=1
go build -tags sqlite_fts5 ./...
```

- [ ] **Step 6: Commit the handshake**

```bash
git add turing-backend/orchestrator-go/internal/service/runtime \
  turing-backend/orchestrator-go/internal/repository/approvals.go \
  turing-backend/orchestrator-go/internal/repository/assignments.go \
  turing-backend/agent-runtime-go/internal/tools \
  turing-backend/agent-runtime-go/internal/worker
git commit -m "feat: fence approval resume delivery" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 2d56d516-9e29-4b22-95f1-c851589f6ad3"
```

---

### Task 8: Make Public History and Events Safe and Equivalent

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/service/chat/service.go`
- Modify: `turing-backend/orchestrator-go/internal/service/chat/service_test.go`
- Create: `turing-backend/orchestrator-go/internal/service/chat/run_outcome_restart_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/events/service.go`
- Modify: `turing-backend/orchestrator-go/internal/service/events/service_test.go`
- Create: `turing-backend/orchestrator-go/internal/service/events/run_outcome_sanitization_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/approvals/service.go`
- Modify: `turing-backend/orchestrator-go/internal/service/approvals/service_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/tool_event_contract_test.go`

- [ ] **Step 1: Write public restart/parity RED tests**

Add:

- `TestEveryLifecycleAndTerminalReasonRoundTripsAfterDatabaseRestart`
- `TestCompletedWithoutContentRoundTripsWithoutAssistantText`
- `TestRecoveringAndWaitingStatesRoundTripAfterRestart`
- `TestTerminalRowWithoutTerminalEventStillReopensFromCanonicalState`
- `TestChatLiveAndReopenedHistoryCarryIdenticalRunState`
- `TestEventReplayAndLiveBusCarryIdenticalVersionedRunState`
- `TestSendMessageIdempotentReplayKeepsOriginalRunAndStateIdentity`
- `TestChatAndEventTypeMappersMapRunStateChangedToTwentyThree`
- `TestRunStateChangedIsNotTerminal`
- `TestChatDirectRunQueuedCarriesVersionOneRunState`
- `TestChatPersistedEventCarriesRunStateForApprovalAndStateChanged`

Make the first test table-driven across queued, running, waiting approval,
recovering, completed, failed, and cancelled. Its terminal rows cover completed
with content, completed-no-content; cancelled abandoned plus a seeded
`USER_CANCELLED` reservation row that proves projection compatibility while
asserting no current producer emits it; and every failed reason: expired,
context limit, provider failure, tool failure, policy denied, retries exhausted,
recovery interrupted, side-effect uncertain, approval delivery failed, and
internal failure.

Close the DB, reopen it, recreate services, and call public gRPC methods rather
than inspecting repository structs.

Run:

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/service/chat \
  ./turing-backend/orchestrator-go/internal/service/events \
  ./turing-backend/orchestrator-go/internal/service/sessions \
  -run 'TestEveryLifecycleAndTerminalReason|TestCompletedWithoutContent|TestRecoveringAndWaiting|TestTerminalRowWithout|TestChatLiveAndReopened|TestEventReplayAndLive|TestSendMessageIdempotentReplayKeeps|TestChatAndEventTypeMappers|TestRunStateChangedIsNotTerminal|TestChatDirectRunQueued|TestChatPersistedEventCarries' \
  -count=1
```

Expected RED: live events and history lack a common snapshot.

- [ ] **Step 2: Project committed state on every lifecycle event**

Existing lifecycle events carry the committed `RunState`. Transitions without
an existing lifecycle event use `agent.run.state_changed`. Terminal transitions
emit only their existing terminal event, never an extra state-changed event.
Add explicit branches for durable `agent.run.state_changed` in both
`chat.mapEventType` and `events.mapEventType`. Both functions normalize
underscores to dots before switching, so their case literal must be
`"agent.run.state.changed"` even though the durable event type remains
`"agent.run.state_changed"`. Map the ChatStream union to `run_state_changed`,
populate `TuringEvent.run_state` in both `events.mapEvent` and
`events.mapBusEvent` and ChatService's `persistedEvent` builder, and keep
`isTerminalEvent("agent.run.state_changed")` false. Populate the additive
run-state field on the direct initial `RunQueued` send and direct
started/completed/failed/cancelled ChatStream messages as well. The initiating
client suppresses the bus copy of queued, so this direct send is required for
live/reopen version-1 parity. Approval and state-changed events use the
persisted-event arm and must carry the identical snapshot there.

Map legacy ChatStream fields to fixed generic values; new clients use
`RunState`. Keep `RunFailed.retryable=false`.

- [ ] **Step 3: Write defense-in-depth redaction RED tests**

Add one subtest per public inventory event plus malformed JSON:

- `TestChatServiceSanitizesMalformedLegacyFailureEvents`
- `TestEventServiceSanitizesMalformedLegacyFailureEvents`
- `TestPublicFailureEventsNeverExposeRawDiagnostics`
- `TestApprovalDenialRationaleStaysOutOfRunStateAndFailureEvents`
- `TestUnknownStoredLifecycleAndOutcomeMapToSemanticUnknown`

Seed rows directly with raw provider error text, paths, arguments, results,
approval tokens, and denial rationale. Assert none appears in ChatService or
EventService responses. Assert denial rationale remains available only through
the existing governed approval/audit storage.

Run:

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/service/chat \
  ./turing-backend/orchestrator-go/internal/service/events \
  ./turing-backend/orchestrator-go/internal/service/approvals \
  -run 'Test.*SanitizesMalformed|TestPublicFailureEventsNever|TestApprovalDenialRationale|TestUnknownStoredLifecycle' \
  -count=1
```

Expected RED: raw payload strings or parser errors are still mapped publicly.

- [ ] **Step 4: Centralize safe event decoding**

Use one allowlisted decoder for ChatService and EventService. On malformed or
unmigrated legacy rows:

- return semantic unknown/internal state or category;
- never return parser text;
- never copy unknown code/message/note/reason fields;
- never panic on unknown numeric enums;
- preserve intended structural IDs only.

- [ ] **Step 5: Preserve cancellation honesty**

Using Task 6's already-tested typed disconnect/tool-cleanup ingestion, add an
end-to-end assertion across migration, live stream, event replay, and reopen
that every ambiguous current cancellation is abandoned and no current path
emits `USER_CANCELLED`. This is parity coverage, not a deferred production
conversion.

- [ ] **Step 6: Run service regressions and commit**

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/service/chat \
  ./turing-backend/orchestrator-go/internal/service/events \
  ./turing-backend/orchestrator-go/internal/service/sessions \
  ./turing-backend/orchestrator-go/internal/service/approvals \
  ./turing-backend/orchestrator-go/internal/service/runtime -count=1
go build -tags sqlite_fts5 ./...
git add turing-backend/orchestrator-go/internal/service
git commit -m "feat: expose safe versioned run outcomes" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 2d56d516-9e29-4b22-95f1-c851589f6ad3"
```

---

### Task 8A: Reconcile Confirmed-Release and Uncertain-Ownership Requeues

This blocker task records the post-Task-8 correction to the transition graph.
Current main had direct running-to-queued writers for every retry and recovery;
the first TUR-009 implementation replaced all of them with
running-to-recovering-to-queued. Neither universal rule is honest. A proven
release has no uncertain phase, while a lost owner must expose one.

**Files:**
- Modify: `docs/superpowers/specs/2026-08-20-tur-009-reopenable-run-outcomes-design.md`
- Modify: `docs/superpowers/plans/2026-08-20-tur-009-reopenable-run-outcomes.md`
- Modify: `turing-backend/orchestrator-go/internal/repository/run_state.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/jobs.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/assignments.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/retry_requeue_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/recovering_lifecycle_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/repository_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/service.go`
- Modify: `turing-backend/orchestrator-go/internal/service/runtime/retryable_failure_test.go`
- Create: `turing-client/turing_app/lib/models/run_lifecycle.dart`
- Create: `turing-client/turing_app/test/models/run_lifecycle_test.dart`

- [ ] **Step 1: Write repository graph RED tests**

Add:

- `TestConfirmedReleaseRetryRequeuesRunningDirectlyToQueued`
- `TestConfirmedUnsentAssignmentsRequeueRunningDirectlyToQueued`
- `TestUncertainOwnershipRequeueStillCommitsRecoveringThenQueued`
- `TestConfirmedReleaseRejectsStaleVersionWorkerAndAttempt`
- `TestWaitingApprovalCannotUseConfirmedReleaseRequeue`

The first two tests assert one version increment and one ordered queued
projection. The uncertain test asserts recovering at `version+1` and queued at
`version+2`. The fencing tests assert unchanged rows and event counts.

Run:

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/repository \
  -run 'TestConfirmedRelease|TestConfirmedUnsent|TestUncertainOwnershipRequeue|TestWaitingApprovalCannotUse' \
  -count=1
```

Expected RED: all current retry and pre-delivery writers still route through
recovering, and there is no guarded confirmed-release transition.

- [ ] **Step 2: Implement separate confirmed-release and uncertain-owner paths**

Add a typed repository input:

```go
type RetryableRunFailureInput struct {
	RunID                string
	ExpectedStateVersion int64
	WorkerID             string
	AssignmentAttemptID  string
	Failure              runoutcome.Failure
	MaxAttempts          int
}
```

Change `RequeueOrFailRetryableRun` to accept that input. Its requeue branch
updates the exact in-progress job and applies a transaction-local
running-to-queued transition guarded by version, worker, and attempt. The
transition clears ownership, preserves `outcome_reason=none`, increments once,
and appends one queued snapshot.

Route `AbortPendingAssignment`, `AbortUnsentAssignment`, and stale
`pending_send` reconciliation through the confirmed-release lifecycle helper
with their existing job/execution/attempt guards. `RequeueClaimedJob` branches
on the execution state observed inside its transaction: `pending_send` uses
confirmed release, while delivered or any other allowed post-send state uses
the uncertain recovery helper. Keep lease loss, stream loss, `uncertain`, and
already-fenced reconciliation on
`running|waiting_approval → recovering → queued`. Do not weaken terminal or
approval guards.

- [ ] **Step 3: Write the runtime-boundary RED test**

Add `TestRuntimeRetryableFailureUsesCurrentAttemptConfirmedRelease`. Drive a
retryable failure through the authenticated worker stream and assert the
published/durable lifecycle sequence contains queued at `version+1`, no
recovering projection, and no raw diagnostic. Replay a stale prior-attempt
report and assert it is fenced without changing the row or event count.

Run:

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/service/runtime \
  -run 'TestRuntimeRetryableFailureUsesCurrentAttemptConfirmedRelease' \
  -count=1
```

Expected RED: `applyUpdateForWorker` does not pass the connected worker's
durable assignment identity into the retry repository input.

- [ ] **Step 4: Carry authenticated assignment identity to the repository**

Change `applyUpdateForWorker` and `handleRunFailed` to receive the connected
worker ID and assignment snapshot already protected by `worker.beginUpdate`.
Populate `RetryableRunFailureInput` with that worker ID, attempt ID, the
worker-reported expected version after legacy-zero resolution, normalized
failure, and configured attempt cap. Keep terminal failure handling unchanged.

- [ ] **Step 5: Write the Dart graph RED test**

Create `run_lifecycle_test.dart` with a table asserting:

- running-to-queued is accepted because the client receives only the committed
  confirmed-release projection, not the private trigger;
- running/waiting-to-recovering and recovering-to-running/queued are accepted;
- queued-to-running and approval wait/resume are accepted;
- terminal states reject every outgoing transition;
- unrelated backward edges remain rejected.

Run:

```bash
( cd turing-client/turing_app && \
  flutter test test/models/run_lifecycle_test.dart )
```

Expected RED: no semantic lifecycle graph exists in Flutter.

- [ ] **Step 6: Add the minimal reusable Dart lifecycle graph**

Create `run_lifecycle.dart` with the semantic lifecycle enum and a total
`canTransitionTo` helper. The helper accepts only the public pairs in the
approved design; it does not attempt to infer private backend trigger identity.
Task 9 reuses this enum from `run_state.dart` rather than declaring a competing
one.

- [ ] **Step 7: Run the correction boundary**

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/repository \
  ./turing-backend/orchestrator-go/internal/service/runtime -count=1
go test -tags sqlite_fts5 -race \
  ./turing-backend/orchestrator-go/internal/repository \
  ./turing-backend/orchestrator-go/internal/service/runtime -count=1
go build -tags sqlite_fts5 ./...
( cd turing-client/turing_app && \
  flutter test test/models/run_lifecycle_test.dart && flutter analyze )
```

Do not commit yet. Task 8B is a second lifecycle blocker discovered before this
checkpoint closed; both corrections receive one fresh combined spec/quality
review and one clean commit after Task 8B.

---

### Task 8B: Fail Startup for Unprogressable Nonterminal Legacy Correlations

The original fallback accepted any single broken legacy correlation. That is
safe only for immutable terminal history. A nonterminal row accepted that way
cannot be claimed or transitioned safely, and an invalid assistant link can
remove it from the same-session ordering subquery so later work leapfrogs it.

**Files:**
- Modify: `docs/superpowers/specs/2026-08-20-tur-009-reopenable-run-outcomes-design.md`
- Modify: `docs/superpowers/plans/2026-08-20-tur-009-reopenable-run-outcomes.md`
- Modify: `turing-backend/orchestrator-go/internal/db/run_outcomes_migration.go`
- Modify: `turing-backend/orchestrator-go/internal/db/run_outcomes_migration_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/run_state.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/run_state_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/jobs.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/repository_test.go`

- [ ] **Step 1: Write exhaustive nonterminal migration RED tests**

Add table-driven
`TestRunOutcomeMigrationRejectsEveryNonterminalBrokenCorrelationValueFree`.
Cross every derived nonterminal lifecycle — queued, running, waiting approval,
and recovering (legacy running/waiting with active uncertain or fenced
execution) — with:

- null run and message directions;
- run-to-message only;
- message-to-run only;
- non-assistant role;
- cross-session pairing.

For every row, assert `errors.Is(err, runcorrelation.ErrConflict)`, exact error
text `run/message correlation conflict`, no `0017_run_outcomes.sql` migration
record, no canonical columns or indexes, no temporary backfill table, unchanged
legacy run/message/job rows, and the same value-free failure after close/reopen.

Keep and rename only if useful
`TestRunOutcomeMigrationAllowsNullAndSingleMismatchedLegacyCorrelation`; its
failed/completed fixtures remain the terminal neutral-fallback proof.

Run:

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/db \
  -run 'TestRunOutcomeMigrationRejectsEveryNonterminalBrokenCorrelationValueFree|TestRunOutcomeMigrationAllowsNullAndSingleMismatchedLegacyCorrelation' \
  -count=1
```

Expected RED: `deriveRunOutcome` ignores correlation failure for every lifecycle,
writes neutral content identity, and completes migration.

- [ ] **Step 2: Validate correlation after deriving lifecycle**

In `deriveRunOutcome`, derive lifecycle first and call the shared
`runcorrelation.Validate` once. If validation fails and lifecycle is queued,
running, waiting approval, or recovering, return the shared sentinel
immediately. If lifecycle is terminal, preserve the current neutral behavior:
adopt no message bytes, keep the empty-content digest, and do not rewrite either
side. Do not log or wrap row identifiers, content, sessions, or paths.

- [ ] **Step 3: Write transition and session-order RED tests**

Add:

- `TestCanonicalTransitionRejectsAbsentNonterminalCorrelationValueFree`
- `TestCorruptActiveRunCannotBeTransitionedOrLeapfrogged`

The transition test removes both assistant-link directions from a queued run and
asserts completion, failure, cancellation, assignment start, and requeue all
return `runcorrelation.ErrConflict` without changing the row, job, or event
count.

The ordering test enqueues two same-session runs, claims the first, removes both
assistant-link directions from that active run, and then asserts:

- a canonical cancellation of the first run fails with the value-free sentinel
  and writes nothing;
- `ClaimNextJob` returns no later job;
- the second job remains pending and queued;
- no started event exists for the second run.

Run:

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/repository \
  -run 'TestCanonicalTransitionRejectsAbsentNonterminalCorrelationValueFree|TestCorruptActiveRunCannotBeTransitionedOrLeapfrogged' \
  -count=1
```

Expected RED: the transition core treats two empty directions as absence rather
than conflict, and the claim-order subquery's assistant-message joins omit the
corrupt active blocker.

- [ ] **Step 4: Make transition and ordering boundaries fail closed**

Always call the shared correlation validator in `applyRunTransitionTx`; do not
skip validation when both assistant-link directions are empty. Terminal legacy
fallback remains a read-only history rule, not a writer exception.

Change same-session claim ordering to compare the initiating user-message
sequence for earlier and candidate runs. Those IDs are non-null run-owned
ordering anchors and do not depend on the assistant correlation being usable.
Keep job age/routing order and session serialization unchanged.

- [ ] **Step 5: Run the combined blocker gate, review, and commit**

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/db \
  ./turing-backend/orchestrator-go/internal/repository \
  ./turing-backend/orchestrator-go/internal/service/runtime -count=1
go test -tags sqlite_fts5 -race \
  ./turing-backend/orchestrator-go/internal/db \
  ./turing-backend/orchestrator-go/internal/repository \
  ./turing-backend/orchestrator-go/internal/service/runtime -count=1
go build -tags sqlite_fts5 ./...
( cd turing-client/turing_app && \
  flutter test test/models/run_lifecycle_test.dart && flutter analyze )
```

Run a fresh combined Opus spec-compliance review first and a separate Opus
code-quality review second. Resolve and rereview until both explicitly report no
remaining feedback.

```bash
git add docs/superpowers/specs/2026-08-20-tur-009-reopenable-run-outcomes-design.md \
  docs/superpowers/plans/2026-08-20-tur-009-reopenable-run-outcomes.md \
  turing-backend/orchestrator-go/internal/db \
  turing-backend/orchestrator-go/internal/repository \
  turing-backend/orchestrator-go/internal/service/runtime \
  turing-client/turing_app/lib/models/run_lifecycle.dart \
  turing-client/turing_app/test/models/run_lifecycle_test.dart
git commit -m "fix: preserve honest run transitions" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 2d56d516-9e29-4b22-95f1-c851589f6ad3"
```

---

### Task 9: Add Flutter Run-State Models, Raw-Wire Safety, and Localization

**Files:**
- Modify: `turing-client/turing_app/lib/models/run_lifecycle.dart`
- Create: `turing-client/turing_app/lib/models/run_state.dart`
- Modify: `turing-client/turing_app/lib/models/message.dart`
- Modify: `turing-client/turing_app/lib/models/turing_event.dart`
- Modify: `turing-client/turing_app/lib/models/grpc_mappers.dart`
- Create: `turing-client/turing_app/lib/utils/content_presence.dart`
- Create: `turing-client/turing_app/test/models/run_state_test.dart`
- Modify: `turing-client/turing_app/test/models/grpc_mappers_test.dart`
- Create: `turing-client/turing_app/test/models/run_state_wire_compatibility_test.dart`
- Create: `turing-client/turing_app/test/utils/content_presence_test.dart`
- Modify: `turing-client/turing_app/pubspec.yaml`
- Modify after dependency resolution: `turing-client/turing_app/pubspec.lock`
- Create: `turing-client/turing_app/l10n.yaml`
- Create: `turing-client/turing_app/lib/l10n/app_en.arb`
- Generate: `turing-client/turing_app/lib/l10n/generated/*.dart`
- Modify: `turing-client/turing_app/lib/app.dart`

- [ ] **Step 1: Write Dart model and enum RED tests**

Add:

- `maps message run state without internal fields`
- `maps every known lifecycle and outcome`
- `present unspecified lifecycle maps to semantic unknown`
- `present unspecified outcome maps to semantic unknown`
- `absent run state remains neutral legacy absence`
- `unknown event type never becomes a raw numeric label`
- `event service state changed maps type and semantic run state`
- `chat persisted state changed maps type and semantic run state`

Run:

```bash
( cd turing-client/turing_app && \
  flutter test test/models/run_state_test.dart test/models/grpc_mappers_test.dart )
```

Expected RED: the lifecycle graph exists from Task 8A, but no immutable domain
run state, outcome mapping, or protobuf mapper exists.

- [ ] **Step 2: Add immutable domain enums and state**

`run_lifecycle.dart` retains the semantic lifecycle enum and transition helper
from Task 8A. `run_state.dart` defines the outcome enum, terminal helper,
structural equality, and immutable `RunState`. Store version as Dart `int` after
checked conversion from protobuf `Int64`; reject zero, negative, and values
outside signed 64-bit range.

`Message` receives nullable `runState`. `TuringEvent` receives nullable
`runState`; do not hide it inside the untyped payload map.

- [ ] **Step 3: Write raw-wire enum RED tests**

Construct generated protobuf messages from raw bytes containing unrecognized
numeric values:

- `RunState.lifecycle` field 4;
- `RunState.outcome_reason` field 5;
- `RuntimeRunFailed.failure_origin` field 5.

Add:

- `raw wire unknown lifecycle maps to semantic unknown`
- `raw wire unknown outcome maps to semantic unknown`
- `raw wire unknown failure origin uses the shared unknown decoder`
- `raw wire values do not panic or render their integer`

Run:

```bash
( cd turing-client/turing_app && \
  flutter test test/models/run_state_wire_compatibility_test.dart )
```

Expected RED: generated getters collapse unknown enum numerics to the default.

- [ ] **Step 4: Decode unknown enum wire values operationally**

Before reading a generated enum getter, inspect
`GeneratedMessage.unknownFields.getField(fieldNumber)?.varints`. If an unknown
varint exists for that enum field, map it to semantic unknown. Otherwise map
the generated enum exhaustively. A present `RunState` with unspecified values
also maps to semantic unknown; an absent `RunState` remains legacy absence.

Use this same helper for the raw `FailureOrigin` compatibility test, even though
runtime failures are not rendered by Flutter. Never render `.name` or `.value`
for unknown values.

- [ ] **Step 5: Write and implement Dart content-presence parity**

Mirror every Go vector in `content_presence_test.dart`. Implement the exact
scalar table in `content_presence.dart`; do not use `trim()`. Historical cards
use the canonical backend boolean, while live bubble visibility uses this
helper.

- [ ] **Step 6: Write localization RED tests**

Add:

- `run state copy resolves through English localization resources`
- `every lifecycle and outcome has localized safe copy`
- `localized cards ignore backend message note reason and code`

Run:

```bash
( cd turing-client/turing_app && \
  flutter test test/models/run_state_test.dart -n 'localization|localized' )
```

Expected RED: no localization resource/delegate exists.

- [ ] **Step 7: Add only the required localization plumbing**

Add SDK `flutter_localizations`, enable Flutter generation, configure
`l10n.yaml` to emit committed files under `lib/l10n/generated`, and add English
copy for:

- queued, running, waiting approval, recovering;
- completed without content;
- user cancelled, abandoned, expired, context limit, provider failure, tool
  failure, policy denied, retries exhausted, recovery interrupted, side-effect
  uncertain, approval delivery failed, internal failure;
- status unavailable, outcome unavailable, and no response recorded;
- dispatch retry, recovery retry, and recovery exhausted run-step notices with
  bounded attempt interpolation.

Add delegates and supported locales to `app.dart`. Do not migrate unrelated
existing UI strings.

Run:

```bash
( cd turing-client/turing_app && flutter pub get && flutter gen-l10n )
```

- [ ] **Step 8: Run and commit model/localization work**

```bash
( cd turing-client/turing_app && \
  flutter test test/models/run_state_test.dart \
    test/models/grpc_mappers_test.dart \
    test/models/run_state_wire_compatibility_test.dart \
    test/utils/content_presence_test.dart && \
  flutter analyze )
git add turing-client/turing_app/lib/models \
  turing-client/turing_app/lib/utils \
  turing-client/turing_app/lib/l10n \
  turing-client/turing_app/lib/app.dart \
  turing-client/turing_app/l10n.yaml \
  turing-client/turing_app/pubspec.yaml \
  turing-client/turing_app/pubspec.lock \
  turing-client/turing_app/test/models \
  turing-client/turing_app/test/utils
git commit -m "feat: map localized run outcomes in Flutter" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 2d56d516-9e29-4b22-95f1-c851589f6ad3"
```

---

### Task 10: Reconstruct and Reconcile the Flutter Timeline

**Files:**
- Create: `turing-client/turing_app/lib/features/chat/run_state_reconciler.dart`
- Create: `turing-client/turing_app/lib/features/chat/run_state_card.dart`
- Modify: `turing-client/turing_app/lib/features/chat/run_failure_card.dart`
- Modify: `turing-client/turing_app/lib/features/chat/run_cancelled_card.dart`
- Modify: `turing-client/turing_app/lib/features/chat/terminal_outcome_card.dart`
- Modify: `turing-client/turing_app/lib/features/chat/run_notice_card.dart`
- Modify: `turing-client/turing_app/lib/features/chat/chat_screen.dart`
- Create: `turing-client/turing_app/test/features/run_state_reconciler_test.dart`
- Create: `turing-client/turing_app/test/features/run_state_card_test.dart`
- Modify: `turing-client/turing_app/test/features/chat_screen_test.dart`
- Modify: `turing-client/turing_app/test/features/run_failure_card_test.dart`
- Modify: `turing-client/turing_app/test/features/run_cancelled_card_test.dart`
- Modify: `turing-client/turing_app/test/features/run_notice_card_test.dart`

- [ ] **Step 1: Write reconciliation RED tests**

Add:

- `accepts first valid nonzero version`
- `ignores lower version`
- `treats equal identical state as no-op`
- `rejects equal conflicting state`
- `rejects higher version after terminal`
- `accepts only valid higher nonterminal transition`
- `history and live events use the same reconciliation path`
- `overlapping pages deduplicate by message id and run id version`
- `state bearing queued started approval and state changed events reconcile before type handling`

Run:

```bash
( cd turing-client/turing_app && \
  flutter test test/features/run_state_reconciler_test.dart )
```

Expected RED: no run-ID/version reconciler exists.

- [ ] **Step 2: Implement a pure reconciler**

The reconciler owns one accepted state per run ID and returns explicit results:
accepted, stale, duplicate, inconsistent, or unloaded. It does not use event
sequence, finished time, lifecycle rank, or arrival order to choose truth.

Page application accepts message rows as one unit, deduplicates by message ID,
and creates state/card data only beside that assistant row.

- [ ] **Step 3: Write rendering RED tests**

Add widget tests:

- `completed content has bubble and no redundant terminal card`
- `completed no content suppresses blank bubble and shows completion card`
- `failed content renders content before adjacent failure card`
- `abandoned run uses localized abandonment card`
- `nonterminal empty run shows adjacent status card`
- `missing state empty assistant shows neutral no-response card`
- `unknown state shows unavailable copy without raw backend text`
- `partial live content remains before later terminal card`
- `legacy failure and cancellation cards accept enums not backend strings`
- `failure run step uses localized category and bounded attempts without note`
- `nonfailure redacted run step preserves governed notice copy`

Run:

```bash
( cd turing-client/turing_app && \
  flutter test test/features/run_state_card_test.dart \
    test/features/chat_screen_test.dart \
    test/features/run_failure_card_test.dart \
    test/features/run_cancelled_card_test.dart \
    test/features/run_notice_card_test.dart \
    -n 'completed content|completed no content|failed content|abandoned run|nonterminal empty|missing state|unknown state|partial live content|failure run step|nonfailure redacted' )
```

Expected RED: history suppresses terminal truth or renders an empty assistant
bubble.

- [ ] **Step 4: Render one adjacent state entry**

Extend `_MessageEntry` with run ID and state version. Maintain one
run-ID-keyed state-card entry adjacent to the assistant row.

Rendering matrix:

| Canonical state | Bubble | Card |
|---|---:|---:|
| completed + displayable | yes | no |
| completed + non-displayable | no | neutral completion |
| failed/cancelled + displayable | yes | matching card after bubble |
| failed/cancelled + non-displayable | no | matching card |
| queued/running/waiting/recovering + non-displayable | no | status card |
| absent unusable legacy state + non-displayable | no | neutral no-response |

For run-state cards and rewritten failure-like run steps, do not render backend
message, note, reason, code, or numeric enum text. Continue rendering the
existing governed safe copy for nonfailure redacted egress/model-limit
`agent.run.step` notices; those payloads are explicitly preserved by the
approved migration.
Change `RunFailureCard` and `RunCancelledCard` to accept a semantic
`RunOutcomeReason` (or the canonical domain `RunState`) instead of an arbitrary
`message` string. They resolve both label and detail through generated
localization. Live legacy events first pass through the safe enum mapper, so
these constructors cannot receive backend prose.

At the start of `_applyEvent`, reconcile any non-null `event.runState` before
the type-specific switch. Add a no-extra-effect
`agent.run.state_changed` switch arm; queued, started, approval, completed,
failed, and cancelled events still perform their existing type-specific work
after their snapshot is reconciled. Update `_applyRunStep` and `RunNoticeCard`
so only the three allowlisted failure-notice categories use localized copy and
bounded attempts, while preserved nonfailure notices keep their governed text.

- [ ] **Step 5: Write bounded initial-load race RED tests**

Add:

- `buffers only highest version for each run during initial page load`
- `holds at most sixty four distinct run states`
- `ten thousand unloaded events clear buffer and coalesce one resync`
- `events after overflow are not retained`
- `event for unloaded historical message is discarded`
- `post-load unloaded live event coalesces one newest-page resync`
- `resync page snapshot wins through normal version reconciliation`
- `unloaded events never create detached cards`

Use a pending `Completer<List<Message>>`, synchronous event controller, and an
API fake that counts newest-page reads. Feed exactly 10,000 events without
delays.

Run:

```bash
( cd turing-client/turing_app && \
  flutter test test/features/run_state_reconciler_test.dart \
    test/features/chat_screen_test.dart \
    -n 'buffers only|sixty four|ten thousand|after overflow|unloaded historical|post-load unloaded|resync page|detached cards' )
```

Expected RED: no bounded buffer/resync mechanism exists.

- [ ] **Step 6: Implement the bounded race buffer**

Before the initial newest page commits:

- key by run ID;
- retain only the highest version;
- cap at 64 distinct runs;
- on the 65th distinct run, clear the map, set one resync-required flag, and
  retain no later events;
- after initial page application, issue one newest-page reload.

After initial load:

- discard an event for an unloaded message;
- coalesce at most one newest-page resync;
- never retain events waiting for older pages.

If TUR-008 landed, invoke its newest-page loader and retain its cursor/archive
logic. If it did not land, reuse `_loadInitialMessages` for the coalesced newest
page and do not add older-page pagination.

- [ ] **Step 7: Prove stale replay cannot overwrite history**

Add widget tests where:

- history version N arrives before replay N-1;
- replay N equals history N;
- live N+1 arrives while history is committing;
- terminal history N is followed by invalid N+1;
- a state card lies exactly at a backend page boundary.

Assert no duplicate card, detached card, or blank bubble.

- [ ] **Step 8: Run Flutter tests/analyze and commit**

```bash
( cd turing-client/turing_app && \
  flutter test test/features/run_state_reconciler_test.dart \
    test/features/run_state_card_test.dart \
    test/features/chat_screen_test.dart \
    test/features/run_failure_card_test.dart \
    test/features/run_cancelled_card_test.dart \
    test/features/run_notice_card_test.dart && \
  flutter analyze )
git add turing-client/turing_app/lib/features/chat \
  turing-client/turing_app/test/features
git commit -m "feat: rebuild conversation run outcomes" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 2d56d516-9e29-4b22-95f1-c851589f6ad3"
```

---

### Task 11: Document the Durable Contract and Preserve Landed Roadmap Work

**Files:**
- Create: `docs/architecture/run-outcomes.md`
- Modify: `docs/architecture/2026-08-18-personal-agent-audit.md`
- Modify: `docs/architecture/tech-stack.md`
- Modify: `docs/architecture/send-message-idempotency.md`
- Modify: `docs/architecture/worker-capabilities.md`
- Modify: `docs/VISION.md`
- Modify: `README.md`
- Modify: `turing-client/turing_app/README.md`
- Modify: `.claude/skills/verify/SKILL.md`
- Modify if TUR-004 landed: directly relevant deletion architecture document
- Modify if TUR-008 landed: directly relevant session lifecycle/pagination architecture document
- Modify if TUR-003 landed: directly relevant egress architecture document

- [ ] **Step 1: Run compatibility regressions before documentation**

Run the exact landed tests plus:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository \
  -run 'Test.*DeleteSession|Test.*Idempotency|Test.*HistoryAnchor|Test.*Archive|Test.*Egress' \
  -count=1
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/... \
  -run 'Test.*NotFound|Test.*Tombstone|Test.*Pagination|Test.*Archive|Test.*Egress|Test.*Idempotent' \
  -count=1
( cd turing-client/turing_app && \
  flutter test test/features/chat_screen_test.dart \
    test/ui/shell_navigation_test.dart )
```

Expected GREEN. If a selector matches no test because the corresponding roadmap
task did not land, record that fact; do not create substitute behavior.

- [ ] **Step 2: Write the architecture contract**

`run-outcomes.md` must document:

- `agent_runs` as sole authority;
- canonical columns and reason matrix;
- monotonic version/time semantics and bounds;
- event projection ordering;
- run/message correlation and neutral legacy fallback;
- explicit success/content-presence rules;
- typed failure normalization and redaction inventory;
- approval Ready/Accepted failure matrix;
- duplicate/retry fencing;
- live/reopen Flutter reconciliation and 64-entry buffer;
- migration byte/row bounds and value-free failures;
- retained limitations.

- [ ] **Step 3: Update status and directly relevant docs**

Mark TUR-009 implemented in the central audit only after all behavior tests pass.
Replace the VISION/README statement that reopen suppresses terminal truth with
the new versioned contract. Document that SendMessage replay still returns the
original run/message identity. Document assignment expected version and attempt
ID in worker capabilities/runtime docs. Document Flutter localized terminal
cards and neutral legacy fallback.
Bring the project `verify` skill into exact agreement with the repository's
required matrix by adding the root and both submodule race tests plus
`flutter analyze`; keep the existing builds, tests, proto check, and all three
lint commands. This makes Task 12's `/verify` invocation authoritative rather
than relying on undocumented follow-up commands.

Retain these limits verbatim in substance:

- no queue/no-worker timeout policy;
- no historical tool-card reconstruction;
- no explicit user-cancel intent API;
- no guarantee partial live deltas survive reopen;
- live tool-separated segments collapse on reopen;
- only new run-state copy is localized.

- [ ] **Step 4: Scan documentation for stale contradictions**

Run:

```bash
rg -n "unexplained empty|suppressed on.*reopen|client_cancelled|USER_CANCELLED|retryable|raw error|run outcome|state_version|RECOVERING" \
  README.md turing-client/turing_app/README.md docs
```

Expected: no active documentation claims historical failed/cancelled run truth
is unavailable; retained limitations remain honest.

- [ ] **Step 5: Commit documentation**

```bash
git add README.md turing-client/turing_app/README.md docs \
  .claude/skills/verify/SKILL.md
git commit -m "docs: explain reopenable run outcomes" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 2d56d516-9e29-4b22-95f1-c851589f6ad3"
```

---

### Task 12: Merge Latest Main, Review Until Clean, Verify, and Deliver

**Files:**
- Review all files in: `git diff origin/main...HEAD`
- Update only TUR-009 files needed to resolve merge/review findings

- [ ] **Step 1: Merge the latest main normally before review**

```bash
git fetch origin
git merge --no-edit origin/main
```

Re-run Task 0’s migration/allocation checks. Resolve conflicts by preserving
landed TUR-004 deletion precedence, TUR-008 pagination/archive/status logic,
TUR-003 run-owned egress decisions, and TUR-009’s versioned run truth. Never
rebase or force-push.

- [ ] **Step 2: Run the focused TUR-009 regression set**

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/db \
  ./turing-backend/orchestrator-go/internal/runoutcome \
  ./turing-backend/orchestrator-go/internal/persisttime \
  ./turing-backend/orchestrator-go/internal/repository \
  ./turing-backend/orchestrator-go/internal/service/runstate \
  ./turing-backend/orchestrator-go/internal/service/sessions \
  ./turing-backend/orchestrator-go/internal/service/chat \
  ./turing-backend/orchestrator-go/internal/service/events \
  ./turing-backend/orchestrator-go/internal/service/runtime \
  ./turing-backend/agent-runtime-go/internal/agent \
  ./turing-backend/agent-runtime-go/internal/llm \
  ./turing-backend/agent-runtime-go/internal/tools \
  ./turing-backend/agent-runtime-go/internal/worker -count=1
( cd turing-client/turing_app && \
  flutter test test/models/run_state_test.dart \
    test/models/run_state_wire_compatibility_test.dart \
    test/utils/content_presence_test.dart \
    test/features/run_state_reconciler_test.dart \
    test/features/run_state_card_test.dart \
    test/features/chat_screen_test.dart )
tools/proto/check.sh
```

- [ ] **Step 3: Audit the final failure-ingestion inventory**

Run:

```bash
rg -n 'err\.Error\(\)|Error\(\)|error_message|agent\.run\.failed|agent\.run\.cancelled|agent\.run\.step|approval\.denied|approval\.expired|tool\.call\.failed|tool\.call\.denied' \
  turing-backend/orchestrator-go turing-backend/agent-runtime-go
```

For every match, prove it is one of:

- nondurable internal logging with no sensitive value;
- normalized before any durable write;
- governed approval/audit rationale;
- a safe allowlisted event projection.

Remove any raw-string durable writer bypass.

- [ ] **Step 4: Audit deterministic tests and task scope**

```bash
git diff --check origin/main...HEAD
git diff origin/main...HEAD -- '*_test.go' '*.dart' | rg 'time\.Sleep|Future\.delayed'
git status --short
git diff --stat origin/main...HEAD
```

Expected: no whitespace errors, no new sleep-based synchronization, and only
TUR-009/spec/plan commits plus merge conflict resolutions.

- [ ] **Step 5: Run independent full-diff review rounds**

Dispatch both reviewers independently against the complete
`origin/main...HEAD` diff:

1. Claude Opus 5;
2. GPT-5.6 Luna.

Prompt each to report:

- correctness bugs, races, edge cases, and spec gaps;
- migration atomicity and byte-bound failures;
- lifecycle/version/event ordering and duplicate fencing;
- approval Ready/Accepted delivery races;
- public redaction and unknown-enum compatibility;
- Flutter history/live parity, pagination boundaries, bounded memory, and
  localization;
- untested new behavior or fixed bugs;
- compatibility regressions against landed roadmap work.

Address every finding or record a rigorous rejection tied to code/spec evidence.
Re-run affected tests. Repeat both full-diff reviews after changes until each
reviewer explicitly reports no remaining feedback.

- [ ] **Step 6: Run the repository-required final Opus 4.8 review**

Dispatch Claude Opus 4.8 over the full final diff with the repository-required
prompt: correctness, edge cases, intent gaps, reuse/simplification/naming, and
unit test coverage for every behavior/fix. Address findings. If code changes,
rerun affected tests and repeat the Opus 4.8 review until no actionable finding
remains.

- [ ] **Step 7: Invoke `/verify` and run the complete matrix**

Use the project `verify` skill, which must execute:

```bash
go test -tags sqlite_fts5 ./... -count=1
go test -tags sqlite_fts5 -race ./... -count=1
go build -tags sqlite_fts5 ./...
( cd turing-backend/mcp-files && \
  go test ./... -count=1 && \
  go test -race ./... -count=1 && \
  go build ./cmd/server )
( cd turing-backend/mcp-system && \
  go test ./... -count=1 && \
  go test -race ./... -count=1 && \
  go build ./... )
( cd turing-client/turing_app && flutter analyze && flutter test )
tools/proto/check.sh
golangci-lint run --build-tags sqlite_fts5 ./... ./.github/workflows
( cd turing-backend/mcp-files && golangci-lint run ./... )
( cd turing-backend/mcp-system && golangci-lint run ./... )
```

Expected: every command exits zero. Do not claim completion from a partial
matrix.

- [ ] **Step 8: Commit any final merge/review fixes**

```bash
git status --short
git add --all
git commit -m "fix: complete reopenable run outcomes" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 2d56d516-9e29-4b22-95f1-c851589f6ad3"
```

Skip this commit only if the tree is already clean.

- [ ] **Step 9: Push and open exactly one PR**

```bash
git push -u origin HEAD
pr_body="$(printf '%s\n' \
  '## Summary' \
  '- Persist authoritative versioned run lifecycle/outcomes on message history and lifecycle events.' \
  '- Normalize and redact failure ingestion, including bounded atomic legacy migration.' \
  '- Reconstruct localized Flutter state cards with run/version reconciliation and bounded resync.' \
  '' \
  '## Validation' \
  '- Strict RED/GREEN coverage includes migration rollback, runtime races, public restart parity, and Flutter replay bounds.' \
  '- Claude Opus 5 and GPT-5.6 Luna independently reported no remaining feedback; the final Claude Opus 4.8 review has no unresolved finding.' \
  '- The complete repository /verify matrix passes.' \
  '' \
  '## Retained limits' \
  '- TUR-010 still owns queue timeout; historical tool cards and partial-delta persistence remain out of scope; transport cancellation remains abandonment.')"
gh pr create --base main \
  --title "TUR-009: Persist reopenable run outcomes" \
  --body "$pr_body"
gh pr edit --add-label turing-roadmap
```

The PR body must summarize the durable contract, TDD coverage, redaction,
retained limits, both iterative review results, Opus 4.8 result, and `/verify`.
Do not merge the PR.

- [ ] **Step 10: Confirm live mergeability and all six CI jobs**

```bash
gh pr view --json url,headRefOid,mergeable,mergeStateStatus,statusCheckRollup
gh pr checks --watch --fail-fast=false
```

Expected: live mergeability is not conflicting and all six required GitHub CI
jobs succeed. Report the PR URL, head SHA, final contract, review results, local
verification, live CI, and retained risks to the coordinator.

---

## Commit-Boundary Buildability Audit

| Task commit | Required pre-commit proof |
|---:|---|
| 1 | descriptor tests, deterministic proto check, root Go build, Flutter analyze |
| 2 | primitive package tests and root Go build |
| 3 | complete DB migration plus repository enqueue/fixture tests, full root tests, root Go build |
| 4 | repository race tests, temporary-adapter delegation test, full root tests, root Go build |
| 5 | repository/runstate/session tests, full root tests, root Go build |
| 6 | runtime/agent/LLM/tool/worker plus repository/chat/telemetry tests, obsolete-call zero scan, runtime/worker race tests, root Go build |
| 7 | repeated Ready/Accepted race matrix, runtime/worker race tests, full repository tests, root Go build |
| 8 | chat/event/session/approval/runtime tests and root Go build |
| 8A | repository/runtime Go tests and race tests, root Go build, Dart graph test, Flutter analyze |
| 8B | migration/repository/runtime tests and race tests, root Go build, Dart graph test, Flutter analyze |
| 9 | focused Flutter model/wire/content tests and Flutter analyze |
| 10 | focused timeline/card tests and Flutter analyze |
| 11 | roadmap compatibility regressions before the documentation-only commit |
| 12 | focused cross-surface regressions, clean independent reviews, and full `/verify` before delivery |

No task may create a commit when its row fails; repair that task's boundary
before proceeding.

## Approval Ready/Accepted Race Coverage Map

| Boundary | RED test | Implementation task |
|---|---|---:|
| Decision command fails before Ready, owner proven | `TestApprovalDecisionDeliveryFailureBeforeReadyKeepsWaitingWhileOwned` | 7 |
| Decision command fails and ownership uncertain | `TestApprovalDecisionDeliveryFailureWithUncertainOwnerEntersRecovering` | 7 |
| Ready commits before Accepted send | `TestApprovalReadyCommitsRunningBeforeAccepted` | 7 |
| Accepted unobserved, identical Ready retry | `TestLostAcceptedSameAttemptReadyReplaysExactResponse` | 7 |
| Conflicting approval/worker/attempt/version | four `TestReadyWithConflicting...` tests | 7 |
| Detected Accepted send failure | `TestDetectedAcceptedDeliveryFailureMovesRunningToRecovering` | 7 |
| Accepted send succeeded, later ownership loss | `TestUnobservedAcceptedThenOwnershipLossMovesRunningToRecovering` | 7 |
| No tool/model work before Accepted | `TestWorkerCannotContinueToolOrModelBeforeAccepted` | 7 |
| Ready wait bounded by original approval deadline | `TestReadyWaitUsesRemainingApprovalDeadline` | 7 |
| Never-answered Ready cannot hold worker slot | `TestNeverAnsweredReadyFailsOrRecoversWithoutHoldingWorkerSlot` | 7 |
| Mismatched Accepted cannot extend deadline | `TestMismatchedAcceptedDoesNotExtendReadyDeadline` | 7 |
| Conflicting Ready closes/fences stream | `TestConflictingReadyClosesStreamFencesRecoveryAndReleasesSlot` | 7 |

## Public Failure Redaction Coverage Map

| Durable public path | Migration test | Live/read test |
|---|---|---|
| `agent.run.failed` | inventory subtest | Chat/Event sanitization |
| `agent.run.cancelled` | inventory subtest | abandonment + sanitization |
| retry/give-up/recovery `agent.run.step` | inventory subtest | runtime event contract |
| `approval.denied` | inventory + rationale preservation | approval/event sanitization |
| `approval.expired` | inventory subtest | approval/event sanitization |
| `tool.call.failed` | inventory subtest | runtime tool event contract |
| `tool.call.denied` | inventory subtest | runtime tool event contract |
| malformed legacy JSON | rollback-safe migration rejection | semantic unknown fallback |

## Normative Spec Coverage Map

| Approved spec section | Plan tasks |
|---|---|
| Goal and Scope | 0, 8, 10, 11, 12 |
| Existing Failure | 8, 10 |
| Selected Approach | 4, 5, 8 |
| Canonical Durable Model | 2, 3, 4 |
| Public Protobuf Contract | 0, 1, 5, 8, 9 |
| Lifecycle and Version Transitions | 4, 6, 7, 8A, 9 |
| Correlation and Query Invariants | 3, 4, 5, 8B, 10 |
| Typed Failure Normalization | 2, 4, 6, 8 |
| Existing Run-Terminal Code Mapping | 2, 3, 6 |
| Subsidiary Failure Code Mapping | 2, 3, 4, 8 |
| Public Failure Event Inventory | 3, 4, 6, 8, 12 |
| Migration and Legacy Scrubbing | 3 |
| Content Presence | 2, 4, 6, 9, 10 |
| Terminalization and Duplicate Semantics | 4, 6, 7, 8 |
| Flutter Reconstruction and Reconciliation | 9, 10 |
| Testing Strategy | every task; final matrix in 12 |
| Merge Compatibility and Retained Limits | 0, 10, 11, 12 |
