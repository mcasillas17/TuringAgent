# FTS5 Session Recall Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the agent cross-session memory recall via full-text search over past messages — the cheapest large step toward Hermes-style "search its own past conversations," using SQLite's built-in FTS5 with zero new infrastructure.

**Architecture:** Enable FTS5 in the SQLite build (a build-tag change), add a migration that creates an FTS5 virtual table mirroring `messages.content` kept in sync by triggers, add `repository.SearchMessages`, and expose a `SessionService.SearchMessages` RPC. The agent (and later the tool loop) can query it; the Flutter client can offer a search box. This plan delivers the backend search capability; wiring it into the model's context is a documented follow-up.

**Tech Stack:** Go 1.23, `mattn/go-sqlite3` (needs `-tags sqlite_fts5`), orchestrator-go, proto `sessions.proto`, SQLite FTS5 + triggers.

**Independent of Plans #1/#2** — can ship anytime.

---

## Design decisions (locked)

1. **FTS5 must be compiled in.** `mattn/go-sqlite3` only includes FTS5 with the `sqlite_fts5` build tag. Today no build uses it, so `CREATE VIRTUAL TABLE ... USING fts5` fails at runtime with "no such module: fts5." This plan adds `-tags sqlite_fts5` to **every** build/test/Docker invocation for the root module. This is the riskiest step — do it first and prove it, because forgetting the tag anywhere silently breaks migrations at startup.
2. **External-content FTS5 table + triggers**, not manual index maintenance. Assistant messages are inserted empty in `jobs.go` and their `content` is filled later on run completion (`CompleteRunWithEvent`). A trigger on `messages` (INSERT/UPDATE/DELETE) is the only place that reliably captures both the initial insert and the later content fill — application-side indexing would miss the update path.
3. **Content search only, ranked by `bm25`.** MVP returns the matching messages (reusing the existing `Message` proto), most-relevant first, optionally scoped to a session. No summarization/LLM step in this plan (that's the Hermes "LLM summarization for cross-session recall" layer — a follow-up).
4. **CI must run the tagged build.** `.github/workflows/ci.yml` and its self-guard `.github/workflows/ci_test.go` both change — the CI test asserts specific commands, so the tag addition must be reflected there or CI's self-check fails.

## File structure

- Modify: build/test invocations — `.github/workflows/ci.yml`, `.github/workflows/ci_test.go`, `turing-backend/infra/docker-compose.yml` (orchestrator build), `turing-backend/scripts/*.sh` as needed, and the `/verify` skill's Go steps. Add `-tags sqlite_fts5`.
- Create: `turing-backend/orchestrator-go/internal/db/schema/0003_messages_fts.sql` — FTS5 vtable + triggers.
- Modify: `turing-backend/orchestrator-go/internal/repository/sessions.go` — add `SearchMessages`.
- Modify: `proto/turing/v1/sessions.proto` — `SearchMessages` RPC + request/response. Regenerate.
- Modify: `turing-backend/orchestrator-go/internal/service/sessions/service.go` — implement the RPC.

Verification: `cd turing-backend/orchestrator-go && go test -tags sqlite_fts5 ./... -count=1` plus root `go build -tags sqlite_fts5 ./...`, `tools/proto/check.sh`, and the full matrix.

---

## Phase 0 — Enable FTS5 in the build (prove it before anything else)

### Task 0: Add the `sqlite_fts5` build tag everywhere

**Files:** `.github/workflows/ci.yml`, `.github/workflows/ci_test.go`, `turing-backend/infra/docker-compose.yml`, any build scripts, `.claude/skills/verify/SKILL.md`.

- [ ] **Step 1: Write a failing proof test** — a package test that creates an FTS5 table:

Create `turing-backend/orchestrator-go/internal/db/fts5_test.go`:

```go
package db

import (
	"context"
	"testing"
)

func TestFTS5IsCompiledIn(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil { t.Fatal(err) }
	defer database.Close()
	_, err = database.ExecContext(context.Background(),
		`CREATE VIRTUAL TABLE probe USING fts5(content)`)
	if err != nil {
		t.Fatalf("FTS5 not available (missing -tags sqlite_fts5?): %v", err)
	}
}
```

- [ ] **Step 2: Run WITHOUT the tag, confirm failure** (this proves the tag is required)

Run: `cd turing-backend/orchestrator-go && go test ./internal/db/ -run FTS5IsCompiledIn -v`
Expected: FAIL — "no such module: fts5".

- [ ] **Step 3: Run WITH the tag, confirm pass**

Run: `cd turing-backend/orchestrator-go && go test -tags sqlite_fts5 ./internal/db/ -run FTS5IsCompiledIn -v`
Expected: PASS.

- [ ] **Step 4: Propagate the tag** to every build/test entry point:
  - `.github/workflows/ci.yml`: change the `go` job's `go test ./... -count=1` → `go test -tags sqlite_fts5 ./... -count=1` and `go build ./...` → `go build -tags sqlite_fts5 ./...`.
  - `.github/workflows/ci_test.go`: update the asserted command strings to match (this self-guard will otherwise fail CI).
  - `turing-backend/infra/docker-compose.yml`: the orchestrator image build must compile with the tag — set it via a build arg or `GOFLAGS=-tags=sqlite_fts5` in the build stage (confirm how the Dockerfile invokes `go build`).
  - `.claude/skills/verify/SKILL.md`: update the root Go steps to include `-tags sqlite_fts5`.
  - Consider a repo-wide default: add `GOFLAGS=-tags=sqlite_fts5` guidance to `CLAUDE.md` so local `go test`/`go build` don't silently drop it.

- [ ] **Step 5: Verify CI self-guard passes locally**

Run from the repository root: `go test ./.github/workflows/ -count=1` (the CI-asserting test) — Expected: PASS after updating both `ci.yml` and `ci_test.go`.

- [ ] **Step 6: Commit**

```bash
git add .github/workflows/ci.yml .github/workflows/ci_test.go turing-backend/infra/docker-compose.yml .claude/skills/verify/SKILL.md CLAUDE.md turing-backend/orchestrator-go/internal/db/fts5_test.go
git commit -m "build: enable sqlite_fts5 build tag across builds/tests/CI"
```

---

## Phase 1 — FTS5 migration with sync triggers

### Task 1: `0003_messages_fts.sql`

**Files:**
- Create: `turing-backend/orchestrator-go/internal/db/schema/0003_messages_fts.sql`
- Test: `turing-backend/orchestrator-go/internal/db/migrations_test.go` (extend)

- [ ] **Step 1: Write the failing test** (migration applies + triggers sync on insert and on the later content UPDATE)

```go
func TestMessagesFTSStaysInSync(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil { t.Fatal(err) }
	if err := ApplyMigrations(context.Background(), database); err != nil { t.Fatal(err) }

	ctx := context.Background()
	// minimal parent rows to satisfy FKs
	_, _ = database.ExecContext(ctx, `INSERT INTO sessions (id, ...) VALUES ('s1', ...)`) // fill per sessions schema
	// insert empty assistant message, then fill content (mirrors jobs.go + CompleteRunWithEvent)
	_, err = database.ExecContext(ctx, `INSERT INTO messages (id, session_id, role, content, content_type, sequence, created_at) VALUES ('m1','s1','assistant','','text',1,datetime('now'))`)
	if err != nil { t.Fatal(err) }
	_, err = database.ExecContext(ctx, `UPDATE messages SET content='the mitochondria is the powerhouse' WHERE id='m1'`)
	if err != nil { t.Fatal(err) }

	var count int
	err = database.QueryRowContext(ctx,
		`SELECT count(*) FROM messages_fts WHERE messages_fts MATCH 'mitochondria'`).Scan(&count)
	if err != nil { t.Fatal(err) }
	if count != 1 { t.Fatalf("FTS did not index updated content, count=%d", count) }
}
```

- [ ] **Step 2: Run (with tag) → FAIL** (no `messages_fts`).
Run: `cd turing-backend/orchestrator-go && go test -tags sqlite_fts5 ./internal/db/ -run MessagesFTSStaysInSync -v`

- [ ] **Step 3: Implement the migration.** External-content FTS5 keyed to the `messages` rowid, with triggers covering insert/update/delete:

```sql
-- 0003_messages_fts.sql
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
  content,
  content='messages',
  content_rowid='rowid'
);

-- Keep the FTS index in sync with messages (content is filled AFTER insert for assistant turns).
CREATE TRIGGER IF NOT EXISTS messages_fts_ai AFTER INSERT ON messages BEGIN
  INSERT INTO messages_fts(rowid, content) VALUES (new.rowid, new.content);
END;

CREATE TRIGGER IF NOT EXISTS messages_fts_ad AFTER DELETE ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
END;

CREATE TRIGGER IF NOT EXISTS messages_fts_au AFTER UPDATE ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.rowid, old.content);
  INSERT INTO messages_fts(rowid, content) VALUES (new.rowid, new.content);
END;
```

Note: `messages.id` is a TEXT id, but FTS5 external-content tables key on the implicit integer `rowid`. The triggers use `new.rowid`/`old.rowid`, and the search query (Task 2) joins back on `messages.rowid`. Confirm the `messages` table is not `WITHOUT ROWID` (it is a normal table with a TEXT PK, so an implicit rowid exists — good).

- [ ] **Step 4: Run (with tag) → PASS.**

- [ ] **Step 5: Commit.**
```bash
git add turing-backend/orchestrator-go/internal/db/schema/0003_messages_fts.sql turing-backend/orchestrator-go/internal/db/migrations_test.go
git commit -m "feat(db): FTS5 virtual table + sync triggers for message search"
```

---

## Phase 2 — `SearchMessages` repository method

### Task 2: `repository.SearchMessages`

**Files:**
- Modify: `turing-backend/orchestrator-go/internal/repository/sessions.go`
- Test: `turing-backend/orchestrator-go/internal/repository/sessions_test.go`

- [ ] **Step 1: Failing test** — insert several messages across two sessions, search a term, assert only matching messages return, ranked, and that a `sessionID` scope filters correctly.

```go
func TestSearchMessagesRanksAndScopes(t *testing.T) {
	repo := newTestRepo(t)
	// seed sessions s1,s2 and messages (fill content)...
	res, err := repo.SearchMessages(context.Background(), "", "powerhouse", 10) // global
	if err != nil { t.Fatal(err) }
	if len(res) == 0 { t.Fatal("expected a match") }
	scoped, _ := repo.SearchMessages(context.Background(), "s2", "powerhouse", 10)
	// assert every result belongs to s2
}
```

- [ ] **Step 2: Run (with tag) → FAIL.**

- [ ] **Step 3: Implement** — reuse the existing `Message` struct (`sessions.go:28-35`). Join FTS back to `messages`; `bm25()` orders best-first (lower = better):

```go
func (r *Repository) SearchMessages(ctx context.Context, sessionID, query string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 100 { limit = 20 }
	sql := `
		SELECT m.id, m.role, m.content, m.content_type, m.sequence, m.created_at
		FROM messages_fts f
		JOIN messages m ON m.rowid = f.rowid
		WHERE messages_fts MATCH ?`
	args := []any{query}
	if sessionID != "" {
		sql += ` AND m.session_id = ?`
		args = append(args, sessionID)
	}
	sql += ` ORDER BY bm25(messages_fts) LIMIT ?`
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, sql, args...)
	if err != nil { return nil, err }
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.MessageID, &m.Role, &m.Content, &m.ContentType, &m.Sequence, &m.CreatedAt); err != nil { return nil, err }
		out = append(out, m)
	}
	return out, rows.Err()
}
```

Guard: FTS5 `MATCH` throws on malformed query syntax (e.g. unbalanced quotes). Sanitize or wrap user input as a quoted phrase for MVP (`"` + escaped query + `"`) so arbitrary text is treated as a literal phrase, not FTS operators.

- [ ] **Step 4: Run (with tag) → PASS.**
- [ ] **Step 5: Commit.**
```bash
git commit -am "feat(orchestrator): SearchMessages repository method over FTS5"
```

---

## Phase 3 — `SearchMessages` RPC

### Task 3: Proto + service

**Files:** `proto/turing/v1/sessions.proto`, regenerate, `service/sessions/service.go`.

- [ ] **Step 1: Extend the proto**

```proto
message SearchMessagesRequest {
  string query = 1;
  string session_id = 2; // optional scope; empty = all sessions
  int32 limit = 3;
}
message SearchMessagesResponse {
  repeated Message messages = 1;
}
// add to SessionService:
rpc SearchMessages(SearchMessagesRequest) returns (SearchMessagesResponse);
```

- [ ] **Step 2: Add a `proto_contract_test.go` assertion** for the new RPC/messages; run → FAIL before regen.
- [ ] **Step 3: Regenerate + determinism check**
```bash
tools/proto/generate.sh && tools/proto/check.sh
```
- [ ] **Step 4: Implement the RPC** in `service/sessions/service.go` — validate non-empty `query`, call `s.repo.SearchMessages`, map via the existing `mapMessage` helper (service.go:127-137), return `&turingv1.SearchMessagesResponse{Messages: ...}`. Add a service-level test.
- [ ] **Step 5: Run (with tag) → PASS**, then `tools/proto/check.sh` clean.
- [ ] **Step 6: Commit** (include regenerated `gen/` + Dart).
```bash
git add proto/ gen/ turing-client/turing_app/lib/generated/ turing-backend/orchestrator-go/internal/service/sessions/ turing-backend/tests/proto_contract_test.go
git commit -m "feat(orchestrator): SearchMessages RPC for cross-session recall"
```

---

## Phase 4 — Verify + follow-up hooks

### Task 4: Full matrix + document the memory-into-context follow-up

- [ ] Run the full verification matrix via `/verify` (now tag-aware). Confirm the orchestrator container built with FTS5 starts and migrations apply (run `turing-backend/scripts/smoke-grpc.sh`).
- [ ] Add a short note to `docs/architecture/` describing the two deferred layers this enables next: (a) the agent injecting top-K recalled messages into the model context before answering (the actual "memory" behavior), and (b) LLM summarization of recalled results (Hermes parity). Neither is in this plan.
- [ ] Commit.

---

## Self-review checklist

- **Spec coverage:** FTS5 enabled + proven (Task 0) ✓; indexed & trigger-synced incl. the post-insert content fill (Task 1) ✓; ranked, scopeable search (Task 2) ✓; RPC surface (Task 3) ✓.
- **The build-tag trap is handled first and everywhere** (CI, Docker, verify skill, CLAUDE.md) — the single most likely cause of a silent prod failure ✓.
- **Trigger covers the mutate-after-insert path** (assistant content filled on completion) — application-side indexing would have missed it ✓.
- **Injection into model context is explicitly deferred** — this plan ships the *capability*, not the behavior. Flagged for the user as the next step.
- **Query-injection guard** on FTS `MATCH` noted ✓.
```
