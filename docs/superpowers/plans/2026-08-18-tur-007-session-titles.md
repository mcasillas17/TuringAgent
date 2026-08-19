# TUR-007 Stable Session Titles Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the orchestrator publish every committed session metadata update so Flutter can show the deterministic first-turn title and current session ordering without polling.

**Architecture:** Keep title derivation and ownership in the Go repository transaction that inserts the user message and queues its run. Explicit `title_origin` provenance distinguishes legacy placeholders from valid titles whose text is `New chat`. The enqueue transaction persists a `session.updated` event containing the authoritative title and `updatedAt` snapshot; services publish the committed event, the protocol maps it explicitly, and Flutter merges it into its local session list by durable recency rather than delivery order. Existing startup backfill remains the compatibility path for pre-feature conversations, while live clients use only the durable event path.

**Tech Stack:** Go 1.23, SQLite transactions, gRPC/protobuf, Flutter/Dart, Go `testing`, Flutter widget tests.

---

## File map

- `turing-backend/orchestrator-go/internal/repository/jobs.go`: update session metadata in the enqueue transaction, persist the authoritative event, and return it to publishers.
- `turing-backend/orchestrator-go/internal/repository/runs.go`: share one transaction-safe event insertion helper between run-scoped and session-scoped events.
- `turing-backend/orchestrator-go/internal/repository/session_title_test.go`: prove event durability, title stability, whitespace handling, payload shape, and replay.
- `turing-backend/orchestrator-go/internal/db/schema/0010_session_title_origin.sql`: distinguish unset/legacy titles from explicit and derived titles.
- `turing-backend/orchestrator-go/internal/db/migrations_test.go`: prove legacy placeholder classification and schema-version guards.
- `turing-backend/orchestrator-go/internal/repository/automations.go`: carry the session update event out of automation enqueue transactions.
- `turing-backend/orchestrator-go/internal/service/chat/service.go`: publish `session.updated` before later event sequences.
- `turing-backend/orchestrator-go/internal/service/chat/service_test.go`: prove a second subscriber receives the committed update without polling.
- `turing-backend/orchestrator-go/internal/service/automations/scheduler.go`: publish the session update for automation-owned conversations too.
- `proto/turing/v1/events.proto`: assign the additive `TURING_EVENT_TYPE_SESSION_UPDATED` wire value.
- `gen/turing/v1/go/turing/v1/events.pb.go`: generated Go event enum.
- `turing-client/turing_app/lib/generated/turing/v1/events.pbenum.dart`: generated Dart event enum.
- `turing-client/turing_app/lib/generated/turing/v1/events.pb.dart`: generated Dart event message bindings.
- `turing-client/turing_app/lib/generated/turing/v1/events.pbjson.dart`: generated Dart event descriptors.
- `turing-backend/orchestrator-go/internal/service/events/service.go`: map the persisted event name to the protocol enum.
- `turing-backend/orchestrator-go/internal/service/events/service_test.go`: prove replay maps the new event type and payload.
- `turing-backend/orchestrator-go/internal/service/chat/service.go`: map the same event for the generic persisted-event chat fallback.
- `turing-client/turing_app/lib/models/grpc_mappers.dart`: map the protocol enum to `session.updated`.
- `turing-client/turing_app/test/models/grpc_mappers_test.dart`: pin the Dart enum mapping.
- `turing-client/turing_app/lib/features/chat/chat_screen.dart`: forward live session metadata events to its shell host and stop requesting a post-send refresh.
- `turing-client/turing_app/lib/ui/shell/responsive_shell.dart`: apply the title and timestamp snapshot locally and reorder the list.
- `turing-client/turing_app/test/ui/shell_navigation_test.dart`: prove a live event updates the row without another `ListSessions` call.
- `turing-client/turing_app/test/ui/responsive_shell_backend_test.dart`: prove refresh merging cannot resurrect a locally deleted session.
- `README.md`: describe user-visible automatic naming and the relevant focused verification command.
- `docs/architecture/session-titles.md`: document derivation, persistence, event delivery, compatibility, limits, deletion, configuration, and testing.
- `docs/architecture/session-recall.md`: state that search group headings consume the same orchestrator-owned title.
- `docs/VISION.md`: record stable session naming as shipped platform state.
- `docs/architecture/2026-08-18-personal-agent-audit.md`: mark TUR-007 implemented and point to the durable event contract.

### Task 1: Persist an authoritative session metadata event

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/repository/session_title_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/jobs.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/runs.go`

- [ ] **Step 1: Write the failing repository tests**

Add assertions that the enqueue result carries one durable session event whose type is `session.updated`, whose `RunID.Valid` is false, and whose JSON payload is the committed title plus RFC3339Nano `updatedAt`. Replay it from sequence zero and assert it precedes `agent.run.queued`.

Extend the existing later-message and whitespace-only tests so the returned payload reports the stored title rather than the latest message:

```go
type sessionUpdatedPayload struct {
	Title     string `json:"title"`
	UpdatedAt string `json:"updatedAt"`
}

func decodeSessionUpdatedPayload(t *testing.T, event Event) sessionUpdatedPayload {
	t.Helper()
	var payload sessionUpdatedPayload
	if err := json.Unmarshal([]byte(event.PayloadJSON), &payload); err != nil {
		t.Fatalf("decode session.updated payload: %v", err)
	}
	return payload
}
```

For an untitled session enqueued with `"What is in the sandbox?"`, require `payload.Title == "What is in the sandbox?"`. For the second message in an already titled session, require the same first title. For whitespace-only content, require an empty title so the client retains its honest placeholder.

- [ ] **Step 2: Run the focused repository tests and observe failure**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository -run 'TestEnqueueUserMessage|TestDeriveSessionTitle' -count=1
```

Expected: FAIL because `EnqueueUserMessageResult` has no session metadata event and enqueue persists only `agent.run.queued`.

- [ ] **Step 3: Add one shared transaction event helper**

Refactor `appendRunEventTx` into a thin wrapper over a helper that accepts `sql.NullString`, then add the session-scoped wrapper:

```go
func appendSessionEventTx(ctx context.Context, tx *sql.Tx, sessionID string, traceID string, eventType string, payloadJSON string, createdAt string) (Event, error) {
	return appendEventTx(ctx, tx, sessionID, sql.NullString{}, traceID, eventType, payloadJSON, createdAt)
}

func appendRunEventTx(ctx context.Context, tx *sql.Tx, sessionID string, runID string, traceID string, eventType string, payloadJSON string, createdAt string) (Event, error) {
	return appendEventTx(ctx, tx, sessionID, sql.NullString{String: runID, Valid: true}, traceID, eventType, payloadJSON, createdAt)
}
```

The shared helper must insert `nullableString(runID)` so a session event is persisted with SQL `NULL`, not an empty run identifier.

- [ ] **Step 4: Persist and return the committed session snapshot**

Add `SessionUpdatedEvent Event` to `EnqueueUserMessageResult`. Keep the existing atomic `CASE` title guard, then read `COALESCE(title, '')` in the same transaction, marshal:

```go
sessionUpdatedPayload, err := json.Marshal(map[string]string{
	"title":     sessionTitle,
	"updatedAt": createdAt,
})
```

Append `session.updated` before `agent.run.queued`, with the enqueue trace ID and no run ID. Return it as `SessionUpdatedEvent`. This event is written for every accepted message because `updated_at` changes every time; the title changes only while `title_origin = 'unset'` and the derived title is non-empty. Set the origin to `derived` in that same update so an explicit or derived title equal to `New chat` is immutable.

- [ ] **Step 5: Run the focused repository tests**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/repository -run 'TestEnqueueUserMessage|TestDeriveSessionTitle|TestDeleteSessionRemovesEverythingItProduced' -count=1
```

Expected: PASS. The deletion test also proves the title row and its durable update event disappear with the session.

- [ ] **Step 6: Commit the repository transaction**

```bash
git add turing-backend/orchestrator-go/internal/repository/jobs.go \
  turing-backend/orchestrator-go/internal/repository/runs.go \
  turing-backend/orchestrator-go/internal/repository/session_title_test.go
git commit -m "feat(orchestrator): persist session update events" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

### Task 2: Expose and publish the event through gRPC

**Files:**
- Modify: `proto/turing/v1/events.proto`
- Regenerate: `gen/turing/v1/go/turing/v1/events.pb.go`
- Regenerate: `turing-client/turing_app/lib/generated/turing/v1/events.pbenum.dart`
- Regenerate: `turing-client/turing_app/lib/generated/turing/v1/events.pb.dart`
- Regenerate: `turing-client/turing_app/lib/generated/turing/v1/events.pbjson.dart`
- Modify: `turing-backend/orchestrator-go/internal/service/events/service.go`
- Modify: `turing-backend/orchestrator-go/internal/service/events/service_test.go`
- Modify: `turing-backend/orchestrator-go/internal/service/chat/service.go`
- Modify: `turing-backend/orchestrator-go/internal/service/chat/service_test.go`
- Modify: `turing-backend/orchestrator-go/internal/repository/automations.go`
- Modify: `turing-backend/orchestrator-go/internal/service/automations/scheduler.go`

- [ ] **Step 1: Write failing protocol and publish tests**

In the event service test, append a repository event with type `session.updated` and payload `{"title":"First turn","updatedAt":"2026-08-18T20:00:00Z"}`. Assert `ListEvents` returns `TURING_EVENT_TYPE_SESSION_UPDATED` and preserves both payload values.

In the chat service test, subscribe directly to the bus before sending the first message to an untitled session. Require the first published event to be `session.updated`, to have no run ID, and to carry the derived title. Require the next published event to be `agent.run.queued`.

Extend the automation result test so `AutomationFire.SessionUpdatedEvent` is populated and sequenced before `QueuedEvent`.

- [ ] **Step 2: Run the focused service tests and observe failure**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/events \
  ./turing-backend/orchestrator-go/internal/service/chat \
  ./turing-backend/orchestrator-go/internal/service/automations -count=1
```

Expected: FAIL because the enum and mapping do not exist, chat publishes `agent.run.queued` first, and automation drops the returned session event.

- [ ] **Step 3: Add the additive event enum and regenerate bindings**

Append without renumbering existing values:

```proto
TURING_EVENT_TYPE_SESSION_UPDATED = 21;
```

Run:

```bash
tools/proto/generate.sh
```

Expected: generated Go and Dart event enum files change; unrelated generated files remain byte-identical.

- [ ] **Step 4: Map and publish events in sequence order**

Map normalized `"session.updated"` in both Go `mapEventType` functions and Dart `GrpcMappers.eventTypeToString`.

In `chat.Server.SendMessage`, publish:

```go
s.bus.Publish(busEventFromRepository(enqueued.SessionUpdatedEvent))
s.bus.Publish(busEventFromRepository(enqueued.QueuedEvent))
```

before routing notices. Carry `SessionUpdatedEvent` through `AutomationFire` and publish it before `QueuedEvent` in the scheduler. Publishing in persisted sequence order avoids forcing live subscribers through gap replay.

- [ ] **Step 5: Run protocol and service tests**

Run:

```bash
go test -tags sqlite_fts5 ./turing-backend/orchestrator-go/internal/service/events \
  ./turing-backend/orchestrator-go/internal/service/chat \
  ./turing-backend/orchestrator-go/internal/service/automations -count=1
tools/proto/check.sh
```

Expected: PASS.

- [ ] **Step 6: Commit the protocol path**

```bash
git add proto/turing/v1/events.proto gen/turing/v1/go/turing/v1/events.pb.go \
  turing-client/turing_app/lib/generated/turing/v1/events.pbenum.dart \
  turing-client/turing_app/lib/generated/turing/v1/events.pb.dart \
  turing-client/turing_app/lib/generated/turing/v1/events.pbjson.dart \
  turing-backend/orchestrator-go/internal/repository/automations.go \
  turing-backend/orchestrator-go/internal/service/automations/scheduler.go \
  turing-backend/orchestrator-go/internal/service/events/service.go \
  turing-backend/orchestrator-go/internal/service/events/service_test.go \
  turing-backend/orchestrator-go/internal/service/chat/service.go \
  turing-backend/orchestrator-go/internal/service/chat/service_test.go
git commit -m "feat(protocol): stream session metadata updates" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

### Task 3: Apply session updates in Flutter without polling

**Files:**
- Modify: `turing-client/turing_app/test/models/grpc_mappers_test.dart`
- Modify: `turing-client/turing_app/test/ui/shell_navigation_test.dart`
- Modify: `turing-client/turing_app/lib/models/grpc_mappers.dart`
- Modify: `turing-client/turing_app/lib/features/chat/chat_screen.dart`
- Modify: `turing-client/turing_app/lib/ui/shell/responsive_shell.dart`

- [ ] **Step 1: Write the failing Dart mapping and widget tests**

Pin:

```dart
expect(
  GrpcMappers.eventTypeToString(
    eventpb.TuringEventType.TURING_EVENT_TYPE_SESSION_UPDATED,
  ),
  'session.updated',
);
```

Replace the polling naming test with a controlled `_FakeEventSource`. After opening an untitled session and sending its first message, assert `listSessionsCalls` did not increase. Then push:

```dart
TuringEvent(
  eventId: 'evt_session_updated',
  sessionId: 'sess_fresh',
  traceId: 'trace_1',
  sequence: 1,
  type: 'session.updated',
  createdAt: DateTime.utc(2026, 8, 18, 20),
  payload: const {
    'title': 'What is in the sandbox?',
    'updatedAt': '2026-08-18T20:00:00Z',
  },
)
```

Assert the sidebar row changes from `New chat` to the derived title, recency follows `updatedAt`, and `listSessionsCalls` is still unchanged. Add replay and delayed-refresh cases proving an older event cannot reorder the list and an event for a newly created or unpaged session survives a stale list response.

- [ ] **Step 2: Run the focused Flutter tests and observe failure**

Run:

```bash
( cd turing-client/turing_app && flutter test \
  test/models/grpc_mappers_test.dart \
  test/ui/shell_navigation_test.dart )
```

Expected: FAIL because the mapper defaults to `system`, `ChatScreen` ignores `session.updated`, and the shell still polls from `onMessageSent`.

- [ ] **Step 3: Forward the live event from ChatScreen**

Replace `VoidCallback? onMessageSent` with:

```dart
final ValueChanged<TuringEvent>? onSessionUpdated;
```

Handle the event explicitly in `_applyEvent`:

```dart
case 'session.updated':
  widget.onSessionUpdated?.call(event);
  break;
```

Delete the post-`sendMessage` callback. Successful sends wait for the authoritative durable event rather than issuing `ListSessions`.

- [ ] **Step 4: Apply the authoritative snapshot in ResponsiveShell**

Replace `_onMessageSent` with `_applySessionUpdated(TuringEvent event)`. Validate `title` and `updatedAt`, ignore an event older than the held snapshot, and update or insert the immutable `Session` by durable recency. Merge later list responses with newer event snapshots instead of replacing the list:

```dart
void _applySessionUpdated(TuringEvent event) {
  final title = event.payload['title'];
  final updatedAtValue = event.payload['updatedAt'];
  if (title is! String || updatedAtValue is! String) return;
  final updatedAt = DateTime.tryParse(updatedAtValue);
  if (updatedAt == null) return;
  final index = _sessions.indexWhere(
    (session) => session.sessionId == event.sessionId,
  );
  if (index >= 0 && _sessions[index].updatedAt.isAfter(updatedAt)) return;
  final updated = Session(
    sessionId: event.sessionId,
    title: title.isEmpty ? null : title,
    updatedAt: updatedAt.toUtc(),
  );
  setState(() {
    final next = List<Session>.of(_sessions);
    if (index >= 0) next.removeAt(index);
    _insertSessionByRecency(next, updated);
    _sessions = next;
  });
}
```

Pass it as `onSessionUpdated: _applySessionUpdated`.

- [ ] **Step 5: Run the focused Flutter tests**

Run:

```bash
( cd turing-client/turing_app && flutter test \
  test/models/grpc_mappers_test.dart \
  test/ui/shell_navigation_test.dart \
  test/features/search/search_screen_test.dart )
```

Expected: PASS. The search test confirms grouped results continue using the same session title.

- [ ] **Step 6: Commit the no-poll Flutter path**

```bash
git add turing-client/turing_app/lib/models/grpc_mappers.dart \
  turing-client/turing_app/lib/features/chat/chat_screen.dart \
  turing-client/turing_app/lib/ui/shell/responsive_shell.dart \
  turing-client/turing_app/test/models/grpc_mappers_test.dart \
  turing-client/turing_app/test/ui/shell_navigation_test.dart
git commit -m "feat(client): apply streamed session titles" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

### Task 4: Document shipped behavior and limitations

**Files:**
- Create: `docs/architecture/session-titles.md`
- Modify: `README.md`
- Modify: `docs/architecture/session-recall.md`
- Modify: `docs/VISION.md`
- Modify: `docs/architecture/2026-08-18-personal-agent-audit.md`

- [ ] **Step 1: Write the architecture guide**

Document these exact contracts:

- New Flutter sessions are created with no stored title; `New chat` is display-only.
- `DeriveSessionTitle` collapses Unicode whitespace to one line, uses at most 60 runes plus an ellipsis, prefers a word boundary after 30 runes, and makes no model call.
- Empty or whitespace-only input leaves the title unset; the next usable user message may set it.
- Migration `0010_session_title_origin.sql` classifies old null, empty, and placeholder rows as `unset`; new explicit and derived titles record their provenance, so even a valid title equal to `New chat` is preserved.
- The repository guards assignment by `title_origin` in the enqueue transaction, so later messages and explicit caller titles are preserved.
- Every accepted message updates `sessions.updated_at` and persists `session.updated` with `title` and RFC3339Nano `updatedAt`; subscribed clients apply it without polling.
- Startup backfill repairs legacy null, empty, and literal `New chat` rows from their first usable stored user message, skipping whitespace-only turns. It is idempotent and emits no live event because it runs before servers accept subscriptions.
- Automation-created conversations set `title_origin = 'explicit'`, preserving their configured name rather than replacing it with the scheduled prompt.
- Migration classifies sessions already linked from automations as `explicit` before evaluating the legacy `New chat` sentinel.
- Flutter records locally created sessions in the same retained snapshot journal, ignores older refresh generations, preserves snapshots omitted from limited pages, orders equal timestamps by session ID descending, and retains local deletion tombstones for the shell lifetime because page omission cannot prove deletion.
- Session deletion removes the session row, title, messages, and title events through existing cascades.
- There is no title-related configuration knob. The 60-rune policy is a code-level UX contract.
- The rune boundary is not a grapheme-cluster boundary; a combining sequence or emoji ZWJ sequence exactly at the cutoff can end oddly.
- Focused tests and the full `/verify` matrix commands.

- [ ] **Step 2: Update repository entry points**

Add automatic first-turn naming to README’s Flutter capability description and link the new architecture guide. Update session recall to say search headings use the same durable title. Add a “Stable session titles” row to VISION’s current-state table.

Mark TUR-007 implemented in the audit and describe the completed durable event/client path rather than leaving it as unshipped roadmap text.

- [ ] **Step 3: Review documentation against code**

Check each number, event name, payload field, limitation, command, and startup behavior against the implementation. Remove any claim that depends on a poll or a model-generated summary.

- [ ] **Step 4: Commit documentation**

```bash
git add README.md docs/VISION.md docs/architecture/session-recall.md \
  docs/architecture/session-titles.md \
  docs/architecture/2026-08-18-personal-agent-audit.md
git commit -m "docs: describe stable session titles" \
  -m "Co-authored-by: Copilot App <223556219+Copilot@users.noreply.github.com>"
```

### Task 5: Review, fix, verify, and open the PR

**Files:**
- Review: full `git diff main...HEAD`
- Modify: every directly affected file required by valid findings

- [ ] **Step 1: Run the first independent review pair in parallel**

Dispatch:

1. Claude Opus 5, `xhigh`, `long_context`: compare the full diff, this plan, the TUR-007 prompt, and `docs/VISION.md`; report every spec or architecture mismatch, including documentation.
2. GPT-5.6 Luna, `xhigh`, `long_context`: inspect the full diff for correctness, SQLite/event ordering, Unicode and whitespace edges, Flutter state regressions, deletion, compatibility, and missing tests/docs.

- [ ] **Step 2: Fix every valid finding with tests first**

For each valid behavior finding, add or tighten a failing focused test, run it to observe failure, implement the fix, and rerun the focused test. Record rejected findings with concrete code evidence.

- [ ] **Step 3: Repeat both reviewers until both report zero feedback**

Rerun both reviewers in parallel on the new full diff after every fix round. Stop only when both explicitly say there is no remaining feedback, then inspect the final diff independently.

- [ ] **Step 4: Run the repository-required Opus 4.8 final review**

Dispatch Claude Opus 4.8 on the final full diff for correctness, intent gaps, simplification/reuse, naming, and unit-test coverage. Resolve every valid finding and rerun the reviewer if any code or documentation changes.

- [ ] **Step 5: Run the full verification matrix**

Invoke `/verify`, which must pass:

```bash
go test -tags sqlite_fts5 ./... -count=1
go test -tags sqlite_fts5 -race ./... -count=1
go build -tags sqlite_fts5 ./...
( cd turing-backend/mcp-files  && go test ./... -count=1 && go test -race ./... -count=1 && go build ./cmd/server )
( cd turing-backend/mcp-system && go test ./... -count=1 && go test -race ./... -count=1 && go build ./... )
( cd turing-client/turing_app  && flutter test )
tools/proto/check.sh
golangci-lint run --build-tags sqlite_fts5 ./... ./.github/workflows
( cd turing-backend/mcp-files  && golangci-lint run ./... )
( cd turing-backend/mcp-system && golangci-lint run ./... )
```

Expected: every command exits zero.

- [ ] **Step 6: Push and open the focused PR**

Confirm `git status --short` is clean and the full diff contains only TUR-007 code, generated bindings, tests, and directly related documentation. Push `mcasillas17-derive-session-titles`, then use the app-native `create_pull_request` tool with base `main`. Do not merge.

- [ ] **Step 7: Report to the creator session**

Send the PR URL, full verification evidence, the number of Opus 5/Luna review rounds, the Opus 4.8 result, and any real follow-up dependency to project session `62cf9513-4bf4-4de3-a427-bc9d705157c3`.
