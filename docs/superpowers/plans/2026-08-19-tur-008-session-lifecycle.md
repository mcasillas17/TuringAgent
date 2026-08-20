# TUR-008 Session Lifecycle and Pagination Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver stable bounded session pagination, monotonic activity ordering, explicit rename/archive/restore operations, and status-aware Flutter lifecycle actions.

**Architecture:** Keep persistence and lifecycle invariants in short repository transactions, expose append-only protobuf operations through the existing sessions service, and use a canonical HMAC-authenticated keyset cursor at the service boundary. A lower-level persisted-time package is shared by the migration runner and repository; Flutter consumes authoritative session pages and durable status-aware snapshots while retaining archive guards against stale data.

**Tech Stack:** Go 1.23, SQLite, gRPC/protobuf, HMAC-SHA256, Flutter/Dart, `flutter_test`, repository migration tooling, GitHub Actions.

---

## File structure

### New files

- `turing-backend/orchestrator-go/internal/persisttime/timestamps.go` — canonical and legacy persisted timestamp parsing/formatting.
- `turing-backend/orchestrator-go/internal/persisttime/timestamps_test.go` — fixed-width and rejection coverage.
- `turing-backend/orchestrator-go/internal/db/session_timestamp_migration.go` — bounded in-transaction session timestamp normalization hook.
- `turing-backend/orchestrator-go/internal/db/schema/0014_session_lifecycle.sql` — session ordering indexes; rename to the next available migration number if TUR-004 lands first.
- `turing-backend/orchestrator-go/internal/service/sessions/cursor.go` — canonical binary session-list cursor codec.
- `turing-backend/orchestrator-go/internal/service/sessions/cursor_test.go` — cursor structure, authentication, and malformed-input coverage.
- `turing-backend/orchestrator-go/internal/repository/session_lifecycle.go` — rename/archive/restore transactions and durable snapshots.
- `turing-client/turing_app/lib/models/session_page.dart` — sessions plus next cursor.
- `turing-client/turing_app/lib/ui/shell/archived_sessions_dialog.dart` — paginated archived-session management.
- `turing-client/turing_app/test/ui/archived_sessions_dialog_test.dart` — archived pagination and actions.
- `docs/architecture/session-lifecycle.md` — public lifecycle, pagination, cursor invalidation, and archive semantics.

### Existing files changed by responsibility

- `turing-backend/orchestrator-go/internal/config/config.go` and tests — required cursor-only secret.
- `turing-backend/scripts/init.sh`, `init_test.go`, `.env.example`, and `infra/docker-compose.yml` — generate and distribute the cursor secret only to the orchestrator.
- `turing-backend/orchestrator-go/internal/db/migrations.go`, `migrations_test.go`, and `schema_invariants_test.go` — run the timestamp hook inside the migration transaction and verify both indexes.
- `turing-backend/orchestrator-go/internal/repository/timestamps.go` — delegate to `persisttime`.
- `turing-backend/orchestrator-go/internal/repository/sessions.go` and `sessions_test.go` — filtered keyset pages, strict status mapping, and query-plan assertions.
- `turing-backend/orchestrator-go/internal/repository/jobs.go` and `session_title_test.go` — monotonic accepted-message recency and status-aware snapshots.
- `proto/turing/v1/sessions.proto` plus committed generated Go/Dart files — append filters and lifecycle RPCs.
- `turing-backend/orchestrator-go/internal/service/sessions/service.go`, `lifecycle.go`, and `service_test.go` — validation, cursor pagination, lifecycle mapping, and post-commit publication.
- `turing-backend/orchestrator-go/internal/service/events/repository_event.go` — one repository-to-bus event conversion shared by chat and sessions services.
- `turing-backend/orchestrator-go/internal/app/app.go` and tests — wire the event bus and cursor key.
- `turing-client/turing_app/lib/models/session.dart` and `grpc_mappers.dart` — status and exact timestamp mapping.
- `turing-client/turing_app/lib/networking/api_client.dart` and `grpc_client.dart` — page/filter and lifecycle methods.
- `turing-client/turing_app/lib/ui/shell/responsive_shell.dart` and shell tests — active load-more, overflow actions, archive guards, and stale-response reconciliation.
- Directly relevant READMEs, the audit, and `docs/architecture/session-titles.md` — shipped behavior only.

## Task 1: Persisted time, cursor secret, and atomic migration

**Files:**
- Create: `turing-backend/orchestrator-go/internal/persisttime/timestamps.go`
- Create: `turing-backend/orchestrator-go/internal/persisttime/timestamps_test.go`
- Create: `turing-backend/orchestrator-go/internal/db/session_timestamp_migration.go`
- Create: `turing-backend/orchestrator-go/internal/db/schema/0014_session_lifecycle.sql`
- Modify: `turing-backend/orchestrator-go/internal/repository/timestamps.go`
- Modify: `turing-backend/orchestrator-go/internal/config/config.go`
- Modify: `turing-backend/orchestrator-go/internal/config/config_test.go`
- Modify: `turing-backend/orchestrator-go/internal/db/migrations.go`
- Modify: `turing-backend/orchestrator-go/internal/db/migrations_test.go`
- Modify: `turing-backend/orchestrator-go/internal/db/schema_invariants_test.go`
- Modify: `turing-backend/scripts/init.sh`
- Modify: `turing-backend/scripts/init_test.go`
- Modify: `turing-backend/.env.example`
- Modify: `turing-backend/infra/docker-compose.yml`

- [ ] **Step 1: Write failing persisted-time and config tests**

Add table-driven tests proving canonical parsing requires exactly nine fractional
digits and `Z`, while migration parsing accepts RFC3339Nano values and rewrites
offsets to UTC:

```go
func TestParseCanonicalRejectsAlternateRepresentations(t *testing.T) {
	for _, value := range []string{
		"2026-08-20T04:00:00Z",
		"2026-08-20T04:00:00.1Z",
		"2026-08-20T04:00:00.000000000+00:00",
		"not-a-time",
	} {
		if _, err := ParseCanonical(value); err == nil {
			t.Fatalf("ParseCanonical(%q) succeeded", value)
		}
	}
}

func TestLoadFromMapRequiresCursorSecret(t *testing.T) {
	env := requiredEnv()
	delete(env, "TURING_CURSOR_HMAC_SECRET")
	_, err := LoadFromMap(env)
	if err == nil || !strings.Contains(err.Error(), "TURING_CURSOR_HMAC_SECRET") {
		t.Fatalf("LoadFromMap error = %v", err)
	}
}
```

Add init tests asserting a 64-character lowercase hex value is generated once
and preserved, and a Compose test/assertion that only `turing-orchestrator`
receives the variable.

- [ ] **Step 2: Run the focused tests and verify they fail**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/persisttime \
  ./turing-backend/orchestrator-go/internal/config -count=1
go test -tags sqlite_fts5 ./turing-backend/scripts -run 'Cursor|Secret' -count=1
```

Expected: compilation fails because `persisttime` and the cursor config field do
not exist.

- [ ] **Step 3: Implement canonical persisted time and required secret parsing**

Create the shared package:

```go
package persisttime

const Layout = "2006-01-02T15:04:05.000000000Z"

func Format(value time.Time) string {
	return value.UTC().Format(Layout)
}

func ParseCanonical(value string) (time.Time, error) {
	parsed, err := time.Parse(Layout, value)
	if err != nil || Format(parsed) != value {
		return time.Time{}, ErrInvalidTimestamp
	}
	return parsed, nil
}

func ParseLegacy(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, ErrInvalidTimestamp
	}
	return parsed, nil
}
```

Validate `TURING_CURSOR_HMAC_SECRET` as exactly 64 lowercase hex characters,
decode it into `[32]byte` on `Config`, and add it to `init.sh`,
`.env.example`, and only the orchestrator Compose environment.

- [ ] **Step 4: Write failing atomic migration tests**

Seed a pre-migration database with canonical, trimmed-fraction, whole-second,
and offset timestamps. Add a malformed row after a valid row and assert:

```go
if err := ApplyMigrations(ctx, database); err == nil {
	t.Fatal("ApplyMigrations accepted malformed session timestamp")
}
assertSessionTimestamp(t, database, "valid-before-bad", originalLegacyValue)
assertMigrationMissing(t, database, "0014_session_lifecycle")
assertIndexMissing(t, database, "idx_sessions_status_updated")
assertIndexMissing(t, database, "idx_sessions_updated")
```

Also add a successful migration test that checks canonical output and the exact
two index column/order definitions through `PRAGMA index_xinfo`.

- [ ] **Step 5: Run migration tests and verify they fail**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/db \
  -run 'SessionTimestamp|SessionLifecycle|SessionIndexes' -count=1
```

Expected: failures show the hook and indexes are absent.

- [ ] **Step 6: Implement the in-transaction migration hook**

In `ApplyMigrationsWithSkillsRoot`, call the hook after `BeginTx` and before the
SQL file for the exact migration version:

```go
if version == sessionLifecycleMigrationVersion {
	if err := normalizeSessionTimestamps(ctx, tx); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("%s: normalize session timestamps: %w", name, err)
	}
}
```

Implement 256-row ID-keyed batches, close rows before guarded updates, parse
both timestamp columns through `persisttime.ParseLegacy`, and format through
`persisttime.Format`. The SQL migration replaces the legacy index and creates:

```sql
DROP INDEX IF EXISTS idx_sessions_updated;
CREATE INDEX idx_sessions_updated
  ON sessions(updated_at DESC, id DESC);
CREATE INDEX idx_sessions_status_updated
  ON sessions(status, updated_at DESC, id DESC);
```

- [ ] **Step 7: Run focused tests and commit**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/persisttime \
  ./turing-backend/orchestrator-go/internal/config \
  ./turing-backend/orchestrator-go/internal/db \
  ./turing-backend/scripts -count=1
```

Expected: all focused tests pass.

Commit:

```bash
git add turing-backend
git commit -m "feat: add session lifecycle persistence foundations" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

## Task 2: Append the protobuf lifecycle contract

**Files:**
- Modify: `proto/turing/v1/sessions.proto`
- Modify generated: `gen/turing/v1/go/turing/v1/sessions.pb.go`
- Modify generated: `gen/turing/v1/go/turing/v1/sessions_grpc.pb.go`
- Modify generated: `turing-client/turing_app/lib/generated/turing/v1/sessions.pbenum.dart`
- Modify generated: `turing-client/turing_app/lib/generated/turing/v1/sessions.pb.dart`
- Modify generated: `turing-client/turing_app/lib/generated/turing/v1/sessions.pbgrpc.dart`
- Modify generated: `turing-client/turing_app/lib/generated/turing/v1/sessions.pbjson.dart`
- Test: `tools/proto/breaking_test.go`

- [ ] **Step 1: Add a failing compatibility assertion**

Extend the proto guard so existing fields retain numbers and the new contract
has exact names:

```go
assertFieldNumber(t, sessions, "ListSessionsRequest", "page", 1)
assertFieldNumber(t, sessions, "ListSessionsRequest", "filter", 2)
assertRPC(t, sessions, "SessionService", "RenameSession")
assertRPC(t, sessions, "SessionService", "ArchiveSession")
assertRPC(t, sessions, "SessionService", "RestoreSession")
```

- [ ] **Step 2: Run the proto test and verify it fails**

Run:

```bash
go test -tags sqlite_fts5 ./tools/proto -count=1
```

Expected: missing enum, field, messages, and RPCs.

- [ ] **Step 3: Append protocol declarations**

Add `SessionListFilter`, append `filter = 2`, and add request/response wrappers
whose responses contain `Session session = 1`. Add RPCs after existing methods
without renumbering or reusing any field.

- [ ] **Step 4: Regenerate and verify deterministic output**

Run:

```bash
tools/proto/generate.sh
tools/proto/check.sh
go test -tags sqlite_fts5 ./tools/proto -count=1
```

Expected: generation check and proto tests pass.

- [ ] **Step 5: Commit**

```bash
git add proto gen turing-client/turing_app/lib/generated tools/proto
git commit -m "feat: define session lifecycle protocol" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

## Task 3: Canonical authenticated session cursor

**Files:**
- Create: `turing-backend/orchestrator-go/internal/service/sessions/cursor.go`
- Create: `turing-backend/orchestrator-go/internal/service/sessions/cursor_test.go`

- [ ] **Step 1: Write failing round-trip and malformed-cursor tests**

Define a fixed test key and cover round-trip, changed page size, wrong filter,
wrong key, tag tamper, padding, noncanonical base64 tail bits, truncation,
trailing bytes, bad magic/version/timestamp/ID, and oversize input:

```go
func TestSessionCursorRoundTrip(t *testing.T) {
	codec := newSessionCursorCodec([32]byte{1})
	want := sessionCursor{
		Filter:    sessionFilterActive,
		UpdatedAt: "2026-08-20T04:00:00.000000001Z",
		SessionID: "sess_01K34EXAMPLE",
	}
	encoded := codec.encode(want)
	got, err := codec.decode(encoded, sessionFilterActive)
	if err != nil || got != want {
		t.Fatalf("decode = %+v, %v", got, err)
	}
}
```

Every invalid case must compare to the same `errInvalidSessionCursor`.

- [ ] **Step 2: Run and verify failure**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/sessions \
  -run 'Cursor' -count=1
```

Expected: cursor codec symbols are undefined.

- [ ] **Step 3: Implement the fixed binary codec**

Use:

```go
const sessionCursorDomain = "turing.session-list.cursor.v1\x00"
const maxSessionCursorBytes = 2048
```

Encode magic, version, filter, 30 timestamp bytes, big-endian uint16 ID length,
ID bytes, then HMAC-SHA256. Decode with `base64.RawURLEncoding.Strict()`, require
exact re-encoding, perform only bounded length work before constant-time tag
verification, then validate semantics and request-filter equality.

- [ ] **Step 4: Run tests and commit**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/sessions \
  -run 'Cursor' -count=1
```

Expected: all cursor tests pass.

Commit:

```bash
git add turing-backend/orchestrator-go/internal/service/sessions
git commit -m "feat: authenticate session page cursors" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

## Task 4: Repository keyset pages and public status mapping

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/repository/sessions.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/sessions_test.go`
- Modify after the required main merge: `turing-backend/orchestrator-go/internal/repository/session_delete.go`

- [ ] **Step 1: Write failing repository page tests**

Add `SessionListFilter`, `SessionCursor`, and page tests for active, archived,
all, same-timestamp IDs, overfetch, deleted visibility, and insertion between
pages:

```go
first, err := repo.ListSessionsPage(ctx, ListSessionsInput{
	Filter: SessionListActive,
	Limit:  2,
})
insertNewestSession(t, database, "sess_inserted")
second, err := repo.ListSessionsPage(ctx, ListSessionsInput{
	Filter: SessionListActive,
	After:  &SessionCursor{UpdatedAt: first[1].UpdatedAt, SessionID: first[1].SessionID},
	Limit:  2,
})
assertSessionIDs(t, second, []string{"sess_older_2", "sess_older_1"})
```

Seed an unsupported persisted status by temporarily bypassing or using the
post-TUR-004 schema shape and assert a typed invalid-status error rather than a
returned row.

- [ ] **Step 2: Add failing query-plan tests**

At realistic cardinality, use the exact production SQL builders and assert
`EXPLAIN QUERY PLAN` mentions `idx_sessions_status_updated` for filtered pages
and `idx_sessions_updated` for `ALL`, with no `SCAN sessions`.

- [ ] **Step 3: Run and verify failure**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository \
  -run 'ListSessions|SessionQueryPlan|SessionStatus' -count=1
```

Expected: new page input/filter types and query builders are absent.

- [ ] **Step 4: Implement filtered keyset queries**

Add exact repository types:

```go
type SessionListFilter string
const (
	SessionListActive   SessionListFilter = "active"
	SessionListArchived SessionListFilter = "archived"
	SessionListAll      SessionListFilter = "all"
)

type SessionCursor struct {
	UpdatedAt string
	SessionID string
}

type ListSessionsInput struct {
	Filter SessionListFilter
	After  *SessionCursor
	Limit  int
}
```

Use canonical raw-text comparisons, order by both keys descending, apply the
central deletion visibility predicate, and return at most the requested
repository limit. Validate scanned public status values before returning.

- [ ] **Step 5: Run tests and commit**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository \
  -run 'ListSessions|SessionQueryPlan|SessionStatus' -count=1
```

Expected: repository pagination and query-plan tests pass.

Commit:

```bash
git add turing-backend/orchestrator-go/internal/repository
git commit -m "feat: add stable filtered session pages" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

## Task 5: Harden accepted-message activity ordering

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/repository/jobs.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/session_title_test.go`
- Modify after the required main merge: `turing-backend/orchestrator-go/internal/repository/session_delete_test.go`

- [ ] **Step 1: Write failing monotonic and replay tests**

Set `sessions.updated_at` later than wall-clock/latest message, enqueue, and
assert the accepted timestamp is exactly one nanosecond later. Also capture
`updated_at` before exact replay, conflicting replay, and a forced transaction
failure and assert it is unchanged.

```go
before := mustSession(t, repo, sessionID).UpdatedAt
replayed, err := repo.EnqueueUserMessage(ctx, sameIdempotentInput)
if err != nil || !replayed.Replayed {
	t.Fatalf("replay = %+v, %v", replayed, err)
}
after := mustSession(t, repo, sessionID).UpdatedAt
if after != before {
	t.Fatalf("replay updated_at = %q, want %q", after, before)
}
```

- [ ] **Step 2: Run and verify failure**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository \
  -run 'SessionUpdatedAt|Idempotent.*Touch|Monotonic' -count=1
```

Expected: the future session timestamp is overwritten backward.

- [ ] **Step 3: Implement the monotonic transaction helper**

Read current canonical session timestamp through `persisttime.ParseCanonical`,
compare it and the latest message timestamp with the candidate, and advance one
nanosecond when needed. Extend accepted-message payloads:

```go
sessionUpdatedPayload, err := json.Marshal(map[string]string{
	"title":     sessionTitle,
	"status":    sessionStatus,
	"updatedAt": createdAt,
})
```

Keep idempotency lookup before this code and require the TUR-004 activity gate
before writes after TUR-004 is merged in Task 12.

- [ ] **Step 4: Run tests and commit**

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository \
  -run 'SessionTitle|SessionUpdatedAt|Idempotent|Monotonic' -count=1
git add turing-backend/orchestrator-go/internal/repository
git commit -m "fix: keep session activity timestamps monotonic" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

## Task 6: Atomic rename, archive, restore, and snapshots

**Files:**
- Create: `turing-backend/orchestrator-go/internal/repository/session_lifecycle.go`
- Create or modify test: `turing-backend/orchestrator-go/internal/repository/session_lifecycle_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/sessions.go`

- [ ] **Step 1: Write failing lifecycle transaction tests**

Cover trimmed title, 120/121 runes, empty rename, auto-title promotion,
already-explicit no-op, archive/restore true changes and no-ops, archived active
run behavior, monotonic timestamp, durable payload, missing session, deletion
gate, and concurrent serialized changes.

```go
result, err := repo.RenameSession(ctx, sessionID, derivedTitle)
if err != nil || !result.Changed || result.Session.TitleOrigin != "explicit" {
	t.Fatalf("RenameSession = %+v, %v", result, err)
}
payload := decodeSessionUpdatedPayload(t, result.Event)
if payload.Status != "active" || payload.Title != derivedTitle {
	t.Fatalf("payload = %+v", payload)
}
```

- [ ] **Step 2: Run and verify failure**

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository \
  -run 'RenameSession|ArchiveSession|RestoreSession|Lifecycle' -count=1
```

Expected: lifecycle methods are undefined.

- [ ] **Step 3: Implement typed lifecycle operations**

Define:

```go
type SessionMutationResult struct {
	Session Session
	Event   Event
	Changed bool
}
```

Use one helper parameterized by the requested real mutation only where it keeps
validation explicit. Rename stores normalized text and explicit origin.
Archive/restore update status only if needed. Real mutations choose a monotonic
timestamp, update, read the authoritative row, append one `session.updated`,
and commit. No-ops return the current row without an event.

- [ ] **Step 4: Run tests and commit**

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository \
  -run 'RenameSession|ArchiveSession|RestoreSession|Lifecycle' -count=1
git add turing-backend/orchestrator-go/internal/repository
git commit -m "feat: persist session lifecycle mutations" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

## Task 7: Public gRPC pagination and lifecycle service

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/service/sessions/service.go`
- Create: `turing-backend/orchestrator-go/internal/service/sessions/lifecycle.go`
- Create: `turing-backend/orchestrator-go/internal/service/events/repository_event.go`
- Modify: `turing-backend/orchestrator-go/internal/service/chat/service.go`
- Modify: `turing-backend/orchestrator-go/internal/service/sessions/service_test.go`
- Modify: `turing-backend/orchestrator-go/internal/app/app.go`
- Modify: `turing-backend/orchestrator-go/internal/app/app_test.go`

- [ ] **Step 1: Expand the bufconn harness with an explicit cursor key and bus**

Construct the service with `[32]byte{1}` and `events.NewBus()`, subscribe to the
bus, and retain the production constructor path in an app wiring test.

- [ ] **Step 2: Write failing public pagination tests**

Traverse all pages, change page size mid-traversal, insert a new first-row
session between requests, and assert no duplicate/gap. Verify the final
`PageResponse` is non-nil with an empty cursor.

Add a table with limits `-1`, `101`, default `0`, and all cursor failure cases
from the spec. Every cursor failure must be:

```go
if status.Code(err) != codes.InvalidArgument ||
	status.Convert(err).Message() != "page.cursor is invalid" {
	t.Fatalf("cursor error = %v", err)
}
```

- [ ] **Step 3: Write failing lifecycle and validation tests**

Cover ID UTF-8/control/length, create title origin/bound, rename outcomes,
archive/restore idempotence, archived get/delete, unknown IDs, deleted-state
precedence, strict stored status/timestamp mapping, and exactly one post-commit
bus event for each real mutation.

- [ ] **Step 4: Run and verify failure**

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/sessions \
  ./turing-backend/orchestrator-go/internal/app \
  -run 'ListSessions|RenameSession|ArchiveSession|RestoreSession|CursorSecret' -count=1
```

Expected: service ignores cursor/filter and lifecycle RPCs are unimplemented.

- [ ] **Step 5: Implement service validation and page response**

Resolve proto filter to repository filter, validate the limit before decoding,
decode the optional cursor, request `limit + 1`, trim the overfetch, and encode
the last emitted row:

```go
hasMore := len(rows) > limit
if hasMore {
	rows = rows[:limit]
}
page := &turingv1.PageResponse{}
if hasMore {
	last := rows[len(rows)-1]
	page.NextCursor = s.cursors.encode(sessionCursor{
		Filter: filter, UpdatedAt: last.UpdatedAt, SessionID: last.SessionID,
	})
}
```

Make session mapping return `(*turingv1.Session, error)` with strict canonical
timestamp and public-status checks.

- [ ] **Step 6: Implement lifecycle RPCs and post-commit publication**

Validate IDs/titles, map typed errors, call repository operations, and publish
only nonempty committed events:

```go
if result.Changed {
	s.bus.Publish(events.FromRepositoryEvent(result.Event))
}
```

Add `events.FromRepositoryEvent` now and replace chat's private
`busEventFromRepository`, so a later TUR-004 merge resolves to one conversion
rather than retaining duplicates.

- [ ] **Step 7: Run tests and commit**

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/sessions \
  ./turing-backend/orchestrator-go/internal/app -count=1
git add turing-backend/orchestrator-go
git commit -m "feat: expose session lifecycle and pagination" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

## Task 8: Flutter page, status, and lifecycle networking

**Files:**
- Modify: `turing-client/turing_app/lib/models/session.dart`
- Create: `turing-client/turing_app/lib/models/session_page.dart`
- Modify: `turing-client/turing_app/lib/models/grpc_mappers.dart`
- Modify: `turing-client/turing_app/lib/networking/api_client.dart`
- Modify: `turing-client/turing_app/lib/networking/grpc_client.dart`
- Modify: `turing-client/turing_app/test/models/grpc_mappers_test.dart`
- Modify: `turing-client/turing_app/test/networking/grpc_client_test.dart`

- [ ] **Step 1: Write failing model and mapper tests**

Assert active/archived mapping, exact nanoseconds, and page cursor preservation:

```dart
expect(page.sessions.single.status, SessionStatus.archived);
expect(page.nextCursor, 'cursor-next');
expect(page.sessions.single.updatedAtNanoseconds, 1770000000000000001);
```

- [ ] **Step 2: Run and verify failure**

```bash
( cd turing-client/turing_app && \
  flutter test test/models/grpc_mappers_test.dart )
```

Expected: `SessionStatus` and `SessionPage` are undefined.

- [ ] **Step 3: Implement models and production client methods**

Define:

```dart
enum SessionStatus { active, archived }

class SessionPage {
  const SessionPage({required this.sessions, required this.nextCursor});
  final List<Session> sessions;
  final String? nextCursor;
}
```

Add `listSessionPage`, `renameSession`, `archiveSession`, and `restoreSession` to
`TuringApi`, with default compatibility methods only where existing unrelated
fakes need them. Production gRPC calls the new protocol and maps authoritative
sessions.

- [ ] **Step 4: Run tests and commit**

```bash
( cd turing-client/turing_app && \
  flutter test test/models/grpc_mappers_test.dart )
git add turing-client/turing_app
git commit -m "feat: add Flutter session lifecycle API" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

## Task 9: Active sidebar pagination, rename, and archive guards

**Files:**
- Modify: `turing-client/turing_app/lib/ui/shell/responsive_shell.dart`
- Modify: `turing-client/turing_app/test/ui/responsive_shell_backend_test.dart`
- Modify: `turing-client/turing_app/test/ui/shell_navigation_test.dart`
- Modify affected fake APIs in existing Flutter tests

- [ ] **Step 1: Write failing active-list pagination tests**

Return page one with a cursor, tap `Load more`, complete page two, and assert
backend order and duplicate suppression. Add loading and failure assertions so
the first page remains visible when load-more fails.

- [ ] **Step 2: Write failing rename/archive interaction tests**

Open the row overflow menu, rename with trimmed input, and assert the
authoritative response replaces the row. Archive and assert the row/active
selection disappear only after success; on failure they remain with an error
toast.

- [ ] **Step 3: Write failing process-lifetime archive-guard regressions**

After an archive response:

1. complete a stale active refresh containing the session;
2. complete a later active refresh omitting it;
3. deliver a delayed legacy status-less event; and
4. assert the row remains absent.

Then deliver a strictly newer status-aware active restore event and assert it
returns in correct recency order. Keep existing deletion tombstone assertions.

- [ ] **Step 4: Run and verify failure**

```bash
( cd turing-client/turing_app && flutter test \
  test/ui/responsive_shell_backend_test.dart \
  test/ui/shell_navigation_test.dart )
```

Expected: no load-more/menu lifecycle UI and stale data resurrects archives.

- [ ] **Step 5: Implement active page state and authoritative guards**

Track first-page request generation, `nextCursor`, load-more state, and a map of
latest status-aware snapshots. Merge only snapshots newer by exact nanoseconds;
for list ordering use session ID descending on equal timestamps. Never remove
an archived guard from active-page omission.

Replace the delete-only icon with an overflow menu and add the rename dialog.
Use `.runes.length` for the UI title count and rely on server errors as final
authority.

- [ ] **Step 6: Run tests and commit**

```bash
( cd turing-client/turing_app && flutter test \
  test/ui/responsive_shell_backend_test.dart \
  test/ui/shell_navigation_test.dart )
git add turing-client/turing_app
git commit -m "feat: manage active session lifecycle in Flutter" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

## Task 10: Archived sessions surface

**Files:**
- Create: `turing-client/turing_app/lib/ui/shell/archived_sessions_dialog.dart`
- Create: `turing-client/turing_app/test/ui/archived_sessions_dialog_test.dart`
- Modify: `turing-client/turing_app/lib/ui/shell/responsive_shell.dart`

- [ ] **Step 1: Write failing archived-dialog tests**

Cover initial archived filter, pagination, rename, restore, delete, load-more
failure retention, and lifecycle error toasts:

```dart
expect(api.pageRequests.single.filter, SessionListFilter.archived);
await tester.tap(find.text('Restore'));
await tester.pumpAndSettle();
expect(api.restoredSessionIds, ['sess_archived']);
expect(find.text('Archived chat'), findsNothing);
```

- [ ] **Step 2: Run and verify failure**

```bash
( cd turing-client/turing_app && \
  flutter test test/ui/archived_sessions_dialog_test.dart )
```

Expected: archived dialog does not exist.

- [ ] **Step 3: Implement the focused archived surface**

Add an archive icon to the Chats header. The dialog owns only archived paging
and actions, reuses the same session display title and lifecycle API, and
returns whether the active list should refresh. Restore/remove only after
authoritative success.

- [ ] **Step 4: Run tests and commit**

```bash
( cd turing-client/turing_app && flutter test \
  test/ui/archived_sessions_dialog_test.dart \
  test/ui/responsive_shell_backend_test.dart \
  test/ui/shell_navigation_test.dart )
git add turing-client/turing_app
git commit -m "feat: add archived conversation management" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

## Task 11: Documentation and integrated targeted validation

**Files:**
- Create: `docs/architecture/session-lifecycle.md`
- Modify: `docs/architecture/2026-08-18-personal-agent-audit.md`
- Modify: `docs/architecture/session-titles.md`
- Modify: `README.md`
- Modify: `turing-client/turing_app/README.md`
- Modify: `turing-backend/.env.example`

- [ ] **Step 1: Write documentation from implemented behavior**

Document:

- active default and archived/all filters;
- limit defaults/max and cursor error behavior;
- cursor opacity, dedicated key, key-rotation invalidation, and page-size
  independence;
- stable-under-insert guarantee and mutable-row omission trade-off;
- accepted-message/lifecycle monotonic activity;
- archive during runs/automations;
- archive versus TUR-004 deletion;
- Flutter load-more, rename, archive, restore, and deletion actions.

Mark TUR-008 implemented in the audit only after all corresponding tests pass.

- [ ] **Step 2: Run integrated targeted suites**

```bash
go test -tags sqlite_fts5 \
  ./turing-backend/orchestrator-go/internal/persisttime \
  ./turing-backend/orchestrator-go/internal/config \
  ./turing-backend/orchestrator-go/internal/db \
  ./turing-backend/orchestrator-go/internal/repository \
  ./turing-backend/orchestrator-go/internal/service/sessions \
  ./turing-backend/orchestrator-go/internal/app -count=1
( cd turing-client/turing_app && flutter analyze && flutter test \
  test/models/grpc_mappers_test.dart \
  test/ui/responsive_shell_backend_test.dart \
  test/ui/shell_navigation_test.dart \
  test/ui/archived_sessions_dialog_test.dart )
tools/proto/check.sh
```

Expected: all targeted Go, Flutter, and proto checks pass.

- [ ] **Step 3: Commit**

```bash
git add README.md docs turing-client/turing_app/README.md turing-backend/.env.example
git commit -m "docs: document complete session lifecycle" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

## Task 12: Mainline integration, iterative reviews, full verification, and PR

**Files:** Entire branch diff.

- [ ] **Step 1: Merge current `origin/main` normally**

```bash
git fetch origin main
git merge --no-edit origin/main
```

If TUR-004 landed, preserve its deletion lifecycle, terminal event, tombstone,
artifact cleanup, and Flutter deletion behavior. Rename TUR-008's migration to
the next available number and update its hook/tests. Preserve any TUR-003 or
TUR-006 contracts. Never rebase or force-push.

- [ ] **Step 2: Rerun integrated targeted validation after conflict resolution**

Run the commands from Task 11 Step 2.

Expected: all pass on the merged tree.

- [ ] **Step 3: Run independent full-diff review with Claude Opus 5**

Dispatch a read-only full-diff review using model `claude-opus-5`, covering
correctness, edge cases, intent gaps, reuse/simplification/naming, and test
coverage. Fix every accepted finding with a failing regression first. Record a
technical reason for each rejected finding. Repeat until the reviewer says
there is no remaining feedback.

- [ ] **Step 4: Run independent full-diff review with GPT-5.6 Luna**

Repeat the same loop using `gpt-5.6-luna` until it explicitly reports no
remaining feedback.

- [ ] **Step 5: Run the repository-required final Claude Opus 4.8 review**

Dispatch `claude-opus-4.8` against the complete diff with the repository-required
correctness, improvement, and unit-test-coverage prompt. Resolve accepted
findings with regression tests and record rigorous rejection reasons.

- [ ] **Step 6: Run the complete verification matrix**

Invoke the project `/verify` skill. It must run:

```bash
go test -tags sqlite_fts5 ./... -count=1
go test -tags sqlite_fts5 -race ./... -count=1
go build -tags sqlite_fts5 ./...
( cd turing-backend/mcp-files && go test ./... -count=1 && go test -race ./... -count=1 && go build ./cmd/server )
( cd turing-backend/mcp-system && go test ./... -count=1 && go test -race ./... -count=1 && go build ./... )
( cd turing-client/turing_app && flutter analyze && flutter test )
tools/proto/check.sh
golangci-lint run --build-tags sqlite_fts5 ./... ./.github/workflows
( cd turing-backend/mcp-files && golangci-lint run ./... )
( cd turing-backend/mcp-system && golangci-lint run ./... )
```

Expected: every command succeeds.

- [ ] **Step 7: Commit review fixes and confirm a clean branch**

```bash
git status --short
git --no-pager log --oneline origin/main..HEAD
```

Expected: no uncommitted files and only TUR-008 commits.

- [ ] **Step 8: Push normally and open one PR**

```bash
git push -u origin mcasillas17-tur-008-session-lifecycle
```

Open one PR into `main`, apply label `turing-roadmap`, and do not merge it.

- [ ] **Step 9: Confirm live mergeability and all six CI jobs**

Use `gh pr view` and `gh pr checks --watch` until these jobs are successful:

1. Go tests and build
2. MCP system module
3. Proto and script checks
4. Flutter tests
5. MCP files module
6. Lint

Report the PR URL, head SHA, both iterative review outcomes, final Opus 4.8
review, local `/verify` result, live mergeability, all six CI states, and any
retained mutable-row/cursor-rotation risk to the coordinator.
