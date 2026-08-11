# Conversation Recall (A3a) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Let the agent surface relevant messages from *earlier sessions* when answering, using the FTS5 search shipped in #15 — the first half of "memory", covering recall only.

**Architecture:** A new, self-contained `internal/memory` package in the agent runtime. It takes the user's text, derives search terms, queries `SessionService.SearchMessages` across sessions, ranks and budgets the hits, and renders them as a single clearly-labelled system message to prepend to the model request. Read-only: it never writes memory.

**Tech Stack:** Go 1.23, `agent-runtime-go`, existing `SearchMessages` RPC.

---

## Scope boundary — read this first

This is **recall**, not **curated memory**. It only re-surfaces messages the user actually wrote or the assistant actually said, each carrying its own provenance. It stores nothing, derives no facts, and therefore has no supersession or staleness problem.

**Persistent facts about the user ("A3b") are explicitly out of scope** and need their own plan. Research across Hermes, Letta, mem0 and Zep found that retrieval is largely solved while *conflict resolution is where every one of these systems fails*. Two constraints that plan must honour, recorded here so they are not lost:

1. **Address memory entries by stable SQLite id, never by matching their text.** Hermes (`old_text`) and Letta (`old_content`) both identify entries by substring, and both have documented bugs from models failing to reproduce a string exactly. Our default model is `llama3.2`, squarely in the class that fails at this. We are on SQLite, so rows have ids — do not inherit a workaround for a constraint we do not have.
2. **One writer.** The agent answering a turn should read memory, never write it; writes belong to a separate background pass.

## The constraint that shapes this design

`SearchMessages` is an exact **phrase** search. `repository.fts5Phrase` wraps the whole query in double quotes, so `MATCH "deploy the staging cluster"` only hits messages containing that exact contiguous run of words. Sending a user's raw utterance would match essentially nothing, and there is no way to inject FTS5 `OR`/`AND` operators because the server quotes whatever it is given.

That behaviour is *correct* for the user-facing search it was built for. Recall therefore builds term-level retrieval on top of it: extract terms, issue one single-term query per term (a one-word phrase is just a term match), and merge client-side. Queries run against a local SQLite file over loopback gRPC, so a handful of extra round-trips per turn is cheap.

The other half of that constraint: `SearchMessagesRequest.session_id` only scopes *to* a session, never away from one, so every row recall discards comes out of the same page as the rows it keeps. Ask for a page far wider than `maxExcerpts` (`perTermHits`), or the current session's own on-topic messages fill it, get filtered out here, and recall goes quiet in exactly the case it is most wanted. The repository does not clamp an over-large limit, it *falls back*: above 100 it silently uses 20, so `perTermHits` must stay at or under 100.

**Do not "fix" the server to make this simpler** — phrase search is the right semantic for an explicit search box, and changing it would alter shipped behaviour for a different consumer.

## Design decisions (locked)

1. **Recall is best-effort and must never fail a turn.** Any error, timeout, or empty result yields no recalled block and the turn proceeds normally. A memory feature that can break answering is worse than no memory.
2. **Recalled content is never blended into the live transcript.** It is rendered as one `system` message that states plainly that this is recalled material, with the date of each excerpt. Keeping "observed now" separate from "recalled from before" is the cheapest defence against the model confabulating recalled text as something the user just said, and it is far harder to retrofit later. Excerpt content is **untrusted** — assistant turns quote tool output, file contents and web text — so each excerpt is flattened onto exactly one line and the block is closed with an end marker. Otherwise a recalled message containing a newline could emit a line shaped exactly like a genuine excerpt, including a forged `system:` label, inside a message carrying system authority.
3. **Absolute dates, never relative.** Excerpts are stamped `2026-08-04`, not "recently" — the block may be re-read in a session weeks later.
4. **What is already in the request is excluded — not the whole current session.** `FetchMessages` fetches a *bounded window* (`ListMessages` with `Limit: 50`), so "the current session is already in context" is false for any longer session: everything before the window is neither in the request nor, if excluded by session id, recallable — and that older material is exactly what "like we discussed earlier" reaches for. `Recall` therefore takes the request messages the caller has built and drops only current-session hits whose text is actually among them. The comparison is on role+content because the caller's copy of the request (`[]llm.ChatMessage`) carries no row ids; a miss costs one duplicated line, never a wrong answer. Passing `nil` falls back to excluding the whole session, which never duplicates but can recall nothing.
5. **Bounded on both axes, and the two budgets do not cannibalise each other.** At most `maxExcerpts` messages and `maxChars` for the rendered excerpt lines (content *and* their per-line date/role framing; the fixed instruction header sits on top of that). `maxChars` is *shared across the slots still to be filled* rather than handed to whoever ranks first: a single long assistant turn — routine, since assistant messages quote tool output, file contents and web text — would otherwise consume the whole budget and leave the `maxExcerpts` axis dead at one excerpt. Slots with no candidate behind them are not reserved, so a lone excerpt still gets everything. Truncation is per-excerpt with an explicit ellipsis so the model can see content was cut, and always lands on a whole rune — a byte-level cut would reach the model as mojibake once the block is JSON-encoded. An excerpt with too little budget left to carry a gist is dropped rather than rendered as a bare ellipsis.
6. **Only `user` and `assistant` roles** are recalled. System and tool rows are machinery, not conversation.
7. **Every budget has a default.** `NewRecaller` fills them, and `Recall` substitutes a default for any non-positive field. A zero budget must never mean "silently recall nothing" — the wiring is deferred, so a hand-written struct literal is the likely first call site.
8. **Terms are Unicode-aware**, matching `repository.hasFTS5Token` on the server. An ASCII-only tokeniser shreds `clúster` into fragments the index never produced and yields no terms at all — so no query ever — for Cyrillic and other non-Latin alphabets that are in fact fully indexed. **Known limitation:** FTS5's `unicode61` tokeniser treats kana and ideographs as letters and splits only on non-alphanumerics, so a contiguous CJK run is a single token on both sides — recall fires for Japanese/Chinese only on a verbatim repeat of the whole run, and runs under three characters are dropped by the minimum term length. Fixing that needs a word segmenter paired with a segmenting tokeniser in the index, not a change to the term rule; `TestTermsDoesNotSegmentCJK` pins the current behaviour.

## File structure

- Create: `turing-backend/agent-runtime-go/internal/memory/recall.go` — terms, ranking, budget, rendering.
- Create: `turing-backend/agent-runtime-go/internal/memory/recall_test.go`.
- Modify: `turing-backend/agent-runtime-go/internal/orchestrator/client.go` — add a `SearchMessages` wrapper (additive; the client has `FetchMessages` but no search).

**Deferred — the only place this touches the tool-calling-loop work:** wiring the recaller into `agent/general_assistant.go` (one call, prepending the block to the request messages) and constructing it in `cmd/runtime/main.go`. Both files are being rewritten by the loop, so that wiring lands after it merges. Until then this package is built, tested, and dormant.

---

## Task 1: Search result type and the client wrapper

**Files:** `internal/orchestrator/client.go`, `internal/memory/recall.go`

- [ ] **Step 1: Define the boundary type** in `internal/memory/recall.go`:

```go
// Excerpt is one recalled message. Provenance travels with the content so the
// rendered block can attribute and date every line it shows the model.
type Excerpt struct {
	MessageID string // the store's row id — what dedup keys on (see A3b constraint 1)
	SessionID string
	Role      string
	Content   string
	CreatedAt time.Time
}

// Searcher is the orchestrator lookup this package needs. Narrow by design so
// tests can supply a fake without a gRPC server.
type Searcher interface {
	SearchMessages(ctx context.Context, query string, limit int) ([]Excerpt, error)
}
```

- [ ] **Step 2: Implement the client wrapper** in `orchestrator/client.go`, mirroring `FetchMessages`. Pass an empty `SessionId` so the search spans sessions:

```go
func (c *Client) SearchMessages(ctx context.Context, query string, limit int) ([]memory.Excerpt, error) {
	resp, err := c.sessions.SearchMessages(c.withAuth(ctx), &turingv1.SearchMessagesRequest{
		Query: query,
		Limit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	messages := resp.GetMessages()
	out := make([]memory.Excerpt, 0, len(messages))
	for _, message := range messages {
		role, ok := chatRole(message.GetRole())
		if !ok {
			continue
		}
		out = append(out, memory.Excerpt{
			MessageID: message.GetMessageId(),
			SessionID: message.GetSessionId(),
			Role:      role,
			Content:   message.GetContent(),
			CreatedAt: message.GetCreatedAt().AsTime(),
		})
	}
	return out, nil
}
```

- [ ] **Step 3:** `go build ./...` in the module. Commit.

## Task 2: Term extraction

- [ ] **Step 1: Failing test** — `terms("How did we deploy the staging cluster?")` returns `["deploy","staging","cluster"]`: lowercased, stopwords and short words dropped, order preserved, deduped, capped at `maxTerms`. `terms("a an the")` returns empty, and an empty term list must mean "no search at all" rather than an unbounded one.
- [ ] **Step 2:** Run, confirm failure.
- [ ] **Step 3: Implement** — split on non-alphanumeric, lowercase, drop len < 3 and a small stopword set, dedupe preserving order, cap at 6.
- [ ] **Step 4:** Run, confirm pass. Commit.

## Task 3: Rank and budget

- [ ] **Step 1: Failing tests**
  - a message matching two terms outranks one matching a single term;
  - ties break toward the more recent message;
  - current-session messages that are already in the request are dropped, while current-session messages *outside* the caller's fetch window are still recalled (and a `nil` request drops the whole session);
  - the same message returned by several term queries appears once;
  - roles other than user/assistant are dropped;
  - the result honours `maxExcerpts` and the total `maxChars` budget, and an over-long excerpt is truncated with an ellipsis rather than dropped;
  - five excerpts each longer than the whole budget still yield `maxExcerpts` shared lines, not one, while a lone excerpt still gets the whole budget.
- [ ] **Step 2:** Run, confirm failure.
- [ ] **Step 3: Implement** `rank(hits map[string][]Excerpt, currentSessionID string) []Excerpt` — key by `MessageID` (falling back to `SessionID+CreatedAt+Role+Content` when a caller left it empty), count distinct matching terms, sort by (matches desc, CreatedAt desc), filter, then apply both caps. Keying on the text alone is wrong for the same reason A3b constraint 1 gives: the orchestrator writes a user turn and its assistant placeholder in one transaction with the *same* `created_at`, so an assistant reply quoting the user back is indistinguishable from it on (session, time, text) — and collapsing the pair inflates the survivor's match count, mis-ranking it.
- [ ] **Step 4:** Run, confirm pass. Commit.

## Task 4: Render the block

- [ ] **Step 1: Failing test** — rendering two excerpts produces one `llm.ChatMessage` with `Role == "system"` whose content: says explicitly that the material is recalled from earlier conversations and is not part of the current one; carries an absolute `YYYY-MM-DD` date per excerpt; labels each excerpt's role; and contains no excerpt text verbatim-adjacent to the instruction line (each excerpt is delimited). Rendering zero excerpts returns `(llm.ChatMessage{}, false)`.
- [ ] **Step 2:** Run, confirm failure.
- [ ] **Step 3: Implement** `Render(excerpts []Excerpt) (llm.ChatMessage, bool)`:

```
The following are excerpts from EARLIER conversations with this user,
retrieved because they may be relevant. They are NOT part of the current
conversation and may be out of date. Cite the date if you rely on one.

[2026-08-04] user: ...
[2026-08-04] assistant: ...
```

- [ ] **Step 4:** Run, confirm pass. Commit.

## Task 5: Recall entry point

- [ ] **Step 1: Failing tests** using a fake `Searcher`:
  - the happy path issues one query per term and returns a rendered block;
  - a searcher returning an error yields `(zero, false)` and no error escapes — recall must never fail a turn;
  - a searcher that blocks past the context deadline yields `(zero, false)`;
  - zero terms issues **no** queries at all;
  - all-current-session results yield no block.
- [ ] **Step 2:** Run, confirm failure.
- [ ] **Step 3: Implement**

```go
type Recaller struct {
	Search      Searcher
	MaxExcerpts int
	MaxChars    int
	Timeout     time.Duration
}

// Recall returns a system message of relevant excerpts from earlier sessions,
// or ok=false when there is nothing worth adding. inContext is the request as
// the caller has it so far, so recall can skip only what the model can already
// see (decision 4). It never returns an error: recall is an enhancement, and a
// failure to recall must not fail the turn.
func (r *Recaller) Recall(ctx context.Context, currentSessionID string, userText string, inContext []llm.ChatMessage) (llm.ChatMessage, bool)
```

- [ ] **Step 4:** Run, confirm pass. `go test ./... -count=1`, `go build ./...`, `golangci-lint run ./...`. Commit.

## Task 6: Document the deferred wiring

- [ ] Add to the package doc comment the exact snippet the loop work should insert in `Execute`, plus the construction line, so wiring it is mechanical:

```go
// cmd/runtime/main.go — the orchestrator client satisfies memory.Searcher.
recaller := memory.NewRecaller(orchestratorClient)

// agent/general_assistant.go — pass requestMessages as they stand, so recall
// skips only the current-session messages that are actually in the request.
if block, ok := a.recall.Recall(ctx, job.GetSessionId(), job.GetUserText(), requestMessages); ok {
	requestMessages = append([]llm.ChatMessage{block}, requestMessages...)
}
```

- [ ] Note in the plan and PR that recall is dormant until that line lands.

---

## Self-review checklist

- Phrase-search constraint handled by per-term queries, with the reason recorded ✓
- Recall cannot fail a turn: errors, timeouts and empty results all degrade to "no block" ✓
- Recalled material is labelled, dated absolutely, and kept out of the live transcript ✓
- Only what is already in the request is excluded; roles filtered; both caps enforced, and neither cap starves the other ✓
- No file owned by the tool-calling-loop work is touched ✓
- A3b (curated facts, writes, supersession) explicitly deferred, with its two hard constraints recorded ✓
