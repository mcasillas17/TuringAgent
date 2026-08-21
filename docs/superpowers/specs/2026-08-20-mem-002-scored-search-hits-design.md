# MEM-002 Scored Search Hits Design

**Status:** Approved by the coordinator on 2026-08-20 at design commit
`6f46448efb846fef26a7951e44238504b3f51179`. Implementation remains blocked
until PR #68 (`TUR-008`, reviewed at head
`18669085f642bf81614438b48a65ffb1bf9a439e`) lands on `main`.

## Goal

`SessionService.SearchMessages` returns the relevance signal and bounded
excerpt that SQLite FTS5 already computes. New consumers explicitly request an
additive, canonical `SearchHit`; existing consumers continue receiving
protobuf-equivalent `Message` values in the same order through the legacy
`messages` field.

The score has one portable public direction: higher is better. The snippet is
bounded, valid UTF-8, single-line plain text derived only from the matched
message. Neither value is a confidence estimate, an authorization grant, or a
new memory representation.

## Current and imminent boundary

Current search:

- replaces NUL with a delimiter, rejects tokenless input as an empty result,
  and quotes the whole query as one literal FTS5 phrase;
- joins `messages_fts` back to its external-content `messages` source;
- excludes sessions whose TUR-004 `deletion_state` is not `active`;
- orders by `bm25(messages_fts) ASC, message_id ASC`;
- defaults an absent, non-positive, or greater-than-100 limit to 20; and
- returns only complete `Message` values, discarding BM25 and forcing Flutter to
  construct an opening excerpt from the unbounded body.

PR #68 retains that search RPC and ordering while adding public `active` and
`archived` session states, strict list filters, keyset pagination for session
lists, and lifecycle mutations. It intentionally does not add search
pagination. MEM-002 is designed against that final shape, not the pre-PR
surfaces.

## Non-goals

- No repeated terms, match modes, structured query, prefix search, or RPC
  collapse; those belong to MEM-004.
- No evaluation fixtures, thresholds, Recall@k, MRR, nDCG, or latency harness;
  those belong to MEM-003.
- No tokenizer migration or CJK retrieval improvement; MEM-016 owns that work.
- No semantic/vector retrieval, curated memory schema, confidence model, or
  ranking-algorithm replacement.
- No search cursor, result-page snapshot, unrelated session-list change, or UI
  redesign.
- No runtime recall re-ranking change. The runtime remains a legacy
  `messages` consumer until MEM-004.

## Considered API and score approaches

| Approach | Advantages | Problems | Decision |
| --- | --- | --- | --- |
| Negate SQLite BM25 and return `score = -raw_bm25` | Exactly preserves the current order; higher is better; no result-set dependency; cheap and explainable | Values are small, corpus-dependent, and not calibrated across queries | **Selected** |
| Min-max normalize the returned page to `[0, 1]` | Familiar bounded range | A hit's value changes with `limit` and competing rows; one-result and all-tie pages need arbitrary rules; later pagination would redefine earlier scores | Rejected |
| Apply a monotonic reciprocal/logistic transform | Bounded and higher-is-better while preserving order | The transform is arbitrary, obscures SQLite's signal, and compresses the small negative values FTS5 commonly returns into nearly indistinguishable numbers | Rejected |

Returning SQLite's raw negative sign was also rejected. SQLite deliberately
makes better matches numerically smaller so ascending SQL order works; exposing
that implementation convention as a higher-is-better API would be misleading.

The public API does not add a score enum. There is one algorithm and one
documented interpretation in this version, so an enum with a single useful value
would repeat metadata without resolving comparison scope. A future ranking
algorithm must not silently reinterpret this field; it must preserve these
semantics or add an explicit versioned field.

## Protobuf and wire contract

`sessions.proto` adds one top-level message, one response-format enum, one
request field, and one response field:

```proto
message SearchHit {
  Message message = 1;

  // Finite and non-negative. Higher means a more relevant match within the
  // same SearchMessages response. Not comparable across queries or snapshots.
  double score = 2;

  // Bounded, single-line plain text selected from message.content around the
  // match. It contains no server-added markup and must not be treated as HTML.
  string snippet = 3;
}

enum SearchMessagesResponseFormat {
  SEARCH_MESSAGES_RESPONSE_FORMAT_UNSPECIFIED = 0;
  SEARCH_MESSAGES_RESPONSE_FORMAT_LEGACY_MESSAGES = 1;
  SEARCH_MESSAGES_RESPONSE_FORMAT_HITS = 2;
}

message SearchMessagesRequest {
  string query = 1;
  string session_id = 2; // optional scope; empty = all sessions
  int32 limit = 3;
  string exclude_session_id = 4; // optional exclusion
  SearchMessagesResponseFormat response_format = 5;
}

message SearchMessagesResponse {
  // Legacy compatibility field. New consumers request HITS.
  repeated Message messages = 1;
  repeated SearchHit hits = 2;
}
```

Field numbers 1-3 inside the new message are canonical and must not be reused.
The existing request fields remain numbers 1-4 and `response_format` uses the
unused number 5. The existing response field remains number 1; `hits` uses the
unused number 2. No existing number, RPC signature, or existing type changes.
The checked-in Go and Dart generated files are regenerated from the merged
`sessions.proto`.

The response format is explicit:

- `UNSPECIFIED` resolves to `LEGACY_MESSAGES`, preserving every old caller;
- `LEGACY_MESSAGES` returns `messages` only;
- `HITS` returns `hits` only; and
- any unrecognized numeric enum value is `InvalidArgument`.

For the same trigger-consistent stable database and request filters, successful
legacy- and hit-format calls return messages that are protobuf-equal in count,
value, order, and effective limit. No-match and tokenless non-whitespace
searches leave both response arrays empty. A canonical hit always has its
message, finite score, and snippet.

The server intentionally does **not** duplicate full messages inside one
response. The gRPC server has a 4 MiB send ceiling, message content is
unbounded, and search can return 100 rows. Unconditional duplication would make
a response that succeeds today fail once its legacy payload exceeds roughly
half the ceiling. Format negotiation keeps the bounded cost:

- a legacy request performs one FTS query, computes no unused snippet, and sends
  the same at-most-100 messages as today;
- a hit request performs one FTS query and sends each message body once, plus at
  most 800 snippet bytes and one double per returned row; and
- runtime recall's current worst-case 12 local searches per run remain legacy
  searches and do not compute discarded snippets.

At the 100-row maximum, hit metadata adds at most approximately 81 KiB plus
protobuf tags; Flutter's 50-row request adds at most approximately 41 KiB plus
tags. MEM-002 does not raise global transport limits. Aggregate message bodies
that leave less than that headroom can make hit format fail with the transport's
existing `ResourceExhausted` behavior while legacy format still fits. The legacy
format's ceiling and payload are unchanged.

An old client sends no field 5 and receives legacy field 1 exactly as before. A
new client connected to an old server sends unknown request field 5, which the
old server ignores, then sees no `hits` and falls back to `messages`. A new
client connected to a new server requests `HITS`. If a nonconforming server
returns both arrays, the new client prefers `hits` and does not concatenate
them. The direct gRPC path does not depend on an intermediary retaining unknown
fields.

The schema does not set protobuf's `[deprecated = true]` option on `messages`
while supported Go and Dart code must still read that field. The option causes
generated deprecation annotations that fail the repository's static analysis.
The proto comment and this migration section carry the deprecation intent until
all supported consumers migrate.

Adding a field with a fresh number is a binary wire-safe protobuf evolution;
existing field numbers must never change or be reused. The governing protobuf
reference is [Proto3: Updating a Message
Type](https://protobuf.dev/programming-guides/proto3/#updating).

## Repository result and query

The repository keeps the existing `SearchMessages` legacy method and adds a
search-specific `SearchMessageHits` method rather than attaching ranking
metadata to every message:

```go
type SearchHit struct {
    Message Message
    Score   float64
    Snippet string
}
```

Both methods share one builder for the lifecycle, phrase, scope, exclusion, and
limit predicate fragment. `SearchMessages` retains its current message-only
projection. `SearchMessageHits` selects the same message columns plus:

```sql
bm25(messages_fts) AS raw_bm25,
snippet(messages_fts, 0, ?, ?, '…', 32) AS marked_snippet
```

The two bound marker arguments are per-query internal markers described below;
they are never returned. Because their SELECT placeholders appear before the
MATCH placeholder, the hit query's argument order is markers, phrase,
scope/exclusion values, then limit. The legacy query starts with the phrase.
The shared helper returns the predicate fragment and its predicate arguments;
each projection prepends its own arguments before executing.

Both methods keep the literal phrase builder and existing scope/exclusion
predicates. Their lifecycle join admits only public, non-deleting sessions:

```sql
JOIN sessions s
  ON s.id = m.session_id
 AND s.deletion_state = 'active'
 AND s.status IN ('active', 'archived')
```

Archived sessions therefore remain searchable globally and by explicit
`session_id`; archive is reversible visibility state, not withdrawal. Deleting
and deleted rows cannot leak through search. The current schema constrains
status to `active` or `archived`, and the explicit predicate makes any future
status opt-in rather than silently searchable. A schema contract test pins that
public status domain so widening it forces an explicit search-visibility
decision. This aligns search with TUR-008's public `ALL` union without changing
valid legacy results.

Ordering is:

```sql
ORDER BY bm25(messages_fts) ASC, m.id ASC
LIMIT ?
```

Both projections use that expression rather than a SELECT alias that exists only
in the hit projection. The message ID tie-break remains ascending and makes
every equal-score result
set deterministic. Search remains one bounded result set, not a paginated API.
The effective limit behavior remains exactly 1-100 as supplied and 20 for zero,
negative, or greater-than-100 repository inputs. The Flutter client continues
requesting 50.

An exact identifier is searchable only when its tokens occur in
`message.content`, as today. MEM-002 does not add message-ID or session-ID
lookup semantics. FTS operators, quotes, angle brackets, and other
operator-looking input remain data inside the server-built quoted phrase.

## Score semantics

For each row:

```text
raw_bm25 = SQLite bm25(messages_fts)
score    = -raw_bm25
```

SQLite documents that FTS5 BM25 returns a real where better matches are
numerically smaller and that its implementation multiplies the conventional
formula by `-1` to support ascending order. See [SQLite FTS5, the `bm25()`
function](https://sqlite.org/fts5.html#the_bm25_function).

The design was also reproduced locally with SQLite CLI 3.51.0: three documents
returned raw values `-1.419354838709677e-06`,
`-1.257142857142857e-06`, and `-8.301886792452831e-07` in best-to-worst order;
negating them produced the same order under a higher-is-better comparison.
Two identical documents both returned `-1e-06`, after which the row/message ID
tie-break determined order. The repository's Go dependency at design time is
`github.com/mattn/go-sqlite3 v1.14.24`.

This is direction normalization, not range normalization:

- every returned score is finite and non-negative;
- larger values rank before smaller values;
- equality is broken only by `message_id ASC`;
- `-0.0` is canonicalized to positive `0.0`;
- scores may be compared only among hits for the same query and database
  snapshot;
- a score is not a probability, percentage, confidence, distance, or quality
  threshold; and
- corpus size, document frequency, tokenizer behavior, and document length can
  change the numeric value between queries or snapshots.

The pure `normalizeSearchScore(raw float64) (float64, error)` function verifies
`!NaN`, `!Inf`, and `raw <= 0`, negates it, canonicalizes zero, and verifies the
public result again. An unexpected positive or non-finite value fails the whole
query. It is not clamped or emitted as a success-shaped default.

Non-positivity is a pinned assumption of the SQLite FTS5 implementation used by
this repository, not a portable promise made by every BM25 implementation. A
database canary with a term present in most documents asserts that the linked
driver still returns a finite non-positive value. A driver upgrade that changes
that behavior therefore fails CI before the runtime guard can turn searches
into internal errors.

MEM-003 uses response order for Recall@k, MRR, and nDCG. It may record scores
for diagnostics, tie analysis, and within-query rank deltas, but must not
average raw score magnitudes across unrelated queries or set a global relevance
threshold.

## Safe snippet contract

### Provenance and match selection

`marked_snippet` comes from FTS5 column 0, the external-content projection of
the same `messages.content` row carried by the hit. The query never reads
adjacent messages, a session title, another result, or text outside that source
message.
The SQL join and lifecycle predicates that authorize the message also govern
the snippet in the same statement.

SQLite's `snippet()` chooses a fragment that maximizes distinct query-term
coverage, favoring the start of a value and text after `.` or `:`. Its fifth
parameter is a token count from 1 through 64. MEM-002 chooses 32 tokens. See
[SQLite FTS5, the `snippet()`
function](https://sqlite.org/fts5.html#the_snippet_function).

Before the hit query, the repository reads exactly 16 bytes (128 bits) from
`crypto/rand` and encodes them as exactly 32 lowercase ASCII hexadecimal
characters (`[0-9a-f]{32}`). The same nonce is placed into these two exact,
domain-separated marker grammars:

```text
start-marker = "[[TURING-FTS5-SNIPPET-START:v1:" nonce "]]"
end-marker   = "[[TURING-FTS5-SNIPPET-END:v1:" nonce "]]"
```

Every framing character is the displayed single-byte ASCII code point:
`[`/`]` are U+005B/U+005D, `:` is U+003A, the labels and `v` use U+0041-U+005A
or U+0061-U+007A, all literal and nonce digits use U+0030-U+0039, and `-` is
U+002D. With the 32-character nonce, the start marker is exactly 65 bytes and
the end marker exactly 63 bytes. The grammar contains no U+0000, C0/C1 control,
non-ASCII code point, or locale-dependent transformation. UTF-8 therefore
encodes every marker code point as the same one byte. `START` versus `END` makes
the two values distinct for every nonce and prevents either complete marker
from being accepted in the other parser state.

The complete start and end strings are bound SQL TEXT values, never query text.
The linked `go-sqlite3` driver must return each bound value byte-for-byte when
selected as TEXT and must preserve those exact byte sequences when FTS5 inserts
them into `snippet()` output. Any driver conversion, replacement, truncation, or
NUL introduction is an invariant failure. A random-source failure fails the
request before opening a reader.

FTS5 inserts those markers around each matched phrase. For every row, the
repository first compares raw Go string bytes and verifies that neither complete
marker occurs in source `message.content`; a collision fails the query instead
of guessing which bytes FTS5 inserted.

The parser then operates on raw `marked_snippet` bytes before rune decoding or
UTF-8 repair. Its two states are `text` and `match`: only the exact start marker
may transition `text -> match`, and only the exact end marker may transition
`match -> text`. An end marker in `text`, a start marker in `match`, nested or
reversed markers, a missing partner, or trailing `match` state is an internal
failure. Sequential complete pairs are valid. The parser removes both complete
marker byte strings and records each enclosed byte range as a match span. Every
remaining non-marker byte, including matched text and surrounding context, is
snippet payload.

A snippet carrying *no* complete start marker and *no* complete end marker is a
distinct, valid outcome rather than a structural failure. `snippet()` can only
wrap a phrase that fits inside its 32-token window, so an exact phrase longer
than that window has no in-window occurrence to mark and SQLite legitimately
returns an unmarked fragment of the same matched row. The legacy projection
returns that row, so the hit projection must return it too. The parser hands
back the payload unchanged with zero match spans; only a completely empty
snippet still fails, because FTS5 returns some text for every row it matches.
Partial marker text is never a marker-free fragment: any occurrence of a
complete start or end marker enters the state machine, so the structural
failures above still apply.

When the repository receives a fragment with zero match spans, it supplies one
implicit whole-fragment byte span covering the entire payload before
sanitizing. That span exists only so the bounding pass below has a window to
open around; it is never published, adds no emphasis, and cannot widen or merge
a match FTS5 did report, because it is applied only when there is no span at
all. The result is a bounded, unhighlighted excerpt drawn from the same
authorized message row.

All snippet payload bytes are then repaired and sanitized. Because marker
recognition and stripping finish first and the marker grammar is valid
single-byte ASCII, invalid source UTF-8 cannot be repaired into a marker, cannot
turn a start into an end, and cannot leave marker bytes in public output.
Markers never cross the repository boundary.

The public snippet has no matched-term emphasis representation: no markup, ANSI
sequence, Markdown delimiter, private-use sentinel, or embedded HTML. Internal
markers exist only to retain a match while enforcing hard bounds. Structured
public match spans can be a future additive field if a UI need justifies them.

The ellipsis parameter is U+2026. SQLite may place it at either edge when the
fragment omits source text. A source-authored ellipsis is intentionally
indistinguishable because it has no control meaning.

### Sanitization and hard bounds

The repository sanitizes parsed snippet text while retaining the internal match
span:

1. Invalid UTF-8 subsequences become U+FFFD.
2. Unicode whitespace and line separators become one ASCII space; adjacent
   spaces collapse and outer space is trimmed.
3. C0/C1 control characters and DEL become U+FFFD unless already normalized as
   whitespace.
4. Explicit bidi controls U+061C, U+200E, U+200F, U+202A-U+202E, and
   U+2066-U+2069 become U+FFFD. Natural RTL characters and language-significant
   joiners remain intact.
5. If the sanitized fragment exceeds either 200 Unicode scalar values or 800
   UTF-8 bytes, the repository selects a window around the first marked match,
   or around the implicit whole-fragment span when the fragment carried no
   markers. It reserves one scalar and three bytes for U+2026 at each cut edge,
   never splits an encoded rune, and trims space adjacent to an inserted
   ellipsis. A complete sanitized match remains in the window whenever that
   match itself fits both caps. If one matched token alone exceeds a cap, bounds
   take precedence and the largest match prefix that fits remains visible. A
   marker-free fragment is windowed from its own start, so only its tail is cut.

For a trigger-consistent projection, the final snippet is valid UTF-8,
non-empty, single-line plain text, at most 200 scalars, at most 800 bytes, and
contains FTS5-marked match text whenever FTS5 marked any. When the phrase is
wider than the 32-token window there is no marked text to contain, and the
snippet is a bounded unhighlighted excerpt of the same message instead. The dual
limit matters because FTS5's token limit does not bound one oversized unbroken
token.

HTML metacharacters remain literal source text; they are not entity-escaped in
the data contract. Consumers must render the field as text, not HTML, Markdown,
ANSI, or another executable format. Flutter's `Text` widget satisfies that
contract. Escape-at-the-rendering-sink remains mandatory for any future web
consumer.

An empty sanitized snippet, structurally invalid internal markers, or a marker
collision is an internal search failure, not a hit with invented text. A
trigger-consistent external-content projection is a database invariant:
application writes update `messages` and `messages_fts` in the same transaction.
If a database is manually mutated around those triggers, SQLite may return zero
markers or balanced markers around stale offsets without a statement error.
Neither case is distinguishable from a legitimate result: zero markers is
exactly what an over-window phrase produces, and balanced-but-stale offsets
cannot be told from valid FTS output without reimplementing the tokenizer. Both
are therefore outside MEM-002's match-correctness guarantee and yield an excerpt
rather than an error. What still holds in both cases is confinement: the snippet
contains text only from the same returned message, never an adjacent or
unauthorized row.

A linked-driver canary binds both exact marker values, selects them back as
TEXT, and requires byte-for-byte equality, distinctness, and absence of NUL.
The same canary exercises start, middle, and end matches through the real
external-content schema, requires the exact marker bytes around the queried
phrase, and proves that parsing strips all marker bytes before public output.
It catches a driver or SQLite upgrade that changes TEXT or marker behavior
before production.

Legacy-format responses preserve unbounded source messages exactly as today.
Hit-format responses carry each source message once and add the bounded snippet
used by the new UI. Removing the legacy field is a later compatibility decision.

CJK and other non-ASCII text receives exactly the tokenizer behavior already
configured by `messages_fts`; the sanitizer preserves it and truncates on Unicode
scalar and UTF-8 boundaries. MEM-002 records that baseline but does not claim to
improve CJK tokenization.

## Lifecycle, deletion, and reader lifetime

TUR-004 deletion precedence remains stronger than archive and search:

- after the deletion-state transition commits, no new search hit, snippet, or
  legacy message can return that session's row or content;
- final deletion removes `messages`, and the existing `messages_fts_ad` trigger
  removes the projection in the same transaction;
- no score, snippet, cache, side table, or derived row is persisted by MEM-002;
  and
- a tokenless or no-match query returns an empty success without opening a
  second content path.

BM25 is a corpus-derived aggregate. Its document count, average length, and
document frequency can still include a deleting session's FTS row until final
deletion removes that projection. A returned score therefore may reflect
aggregate statistics from non-result rows, but it cannot identify or return
their content. This is another reason scores are not comparable across database
snapshots.

The orchestrator configures one SQLite connection with
`SetMaxOpenConns(1)`. Search therefore uses one `QueryContext`, scans every
selected message, score, and snippet from that statement, checks `rows.Err`,
and closes the reader before returning. It performs no per-row query, title
lookup, sanitizer database call, or nested repository operation while the
reader is open. This avoids self-deadlock and bounds how long deletion and
lifecycle writers wait for the connection.

The statement is the consistency boundary. A search that acquired the
connection before deletion may return the pre-transition row; once the
deletion-state transition commits, later searches cannot. The result limit of
at most 100 and prompt row draining keep that read lifetime bounded. MEM-002
does not hold a transaction or cursor open across RPCs.

## Service, clients, and UI

### Go service

The service resolves `UNSPECIFIED` to `LEGACY_MESSAGES`. Legacy format calls the
existing repository method and populates only `messages`. Hit format calls
`SearchMessageHits`, maps each repository hit once, and populates only `hits`.
Unknown format values are `InvalidArgument`. Repository and mapping errors
remain the value-free `Internal: search messages failed`; stored content, SQL,
scores, and snippets never enter the public error.

Nil, empty, or whitespace-only requests remain `InvalidArgument`. Punctuation-
only, emoji-only, NUL-only, and otherwise tokenless non-whitespace queries
remain successful empty responses. Context cancellation continues propagating
through the repository; the gRPC layer maps operational failures without
inventing partial hits.

### Runtime recall

The runtime orchestrator client leaves `response_format` unspecified and
continues reading `response.messages`. Its
`memory.Excerpt`, multi-query term extraction, deduplication, and re-ranking are
unchanged. Legacy-format parity guarantees identical input during this
migration without computing snippets the runtime discards.
MEM-004 will explicitly migrate recall to `hits`; MEM-002 does not partially
change its ranking.

### Dart model and mapper

The existing Flutter-domain `SearchHit` gains source-compatible optional
metadata:

```dart
const SearchHit({
  required this.sessionId,
  required this.message,
  this.score,
  this.snippet,
});
```

The proto `SearchHit` derives its session ID from nested `message.session_id`.
The existing Dart domain type intentionally retains a top-level `sessionId` for
its grouping API. Generated and domain types are always imported with explicit
aliases (`sessionpb.SearchHit` and `model_search_hit.SearchHit`); generated
descriptor/contract tests do not add an unaliased import that would make the
name ambiguous.

`score` and `snippet` are non-null for canonical proto hits and null only for
the mixed-version fallback built from a legacy `Message`. The proto-hit mapper
requires `message`, a finite non-negative score, and a non-empty snippet.
Malformed canonical hits throw a constant, value-free mapping error such as
`search hit score is invalid`; the exception never interpolates the message,
snippet, score, session ID, query, or serialized proto. They do not silently
fall back to a legacy array. This matters because the current search screen
renders and announces the caught exception text.

The gRPC client:

- requests `SEARCH_MESSAGES_RESPONSE_FORMAT_HITS`;
- maps `response.hits` when it is non-empty;
- otherwise maps `response.messages` to legacy domain hits with null metadata;
- returns empty when both fields are empty; and
- never concatenates fields or creates duplicate rows.

Fallback is for a successful old-server response, not an error retry. Hit format
has additional score, marker, sanitizer, and envelope failure modes, and sits
closer to the aggregate transport ceiling than legacy format. The Dart client
does not automatically retry `Internal`, `ResourceExhausted`, timeout, or any
other RPC failure as a legacy request: doing so would hide a canonical-hit
failure behind unscored success and double the query work. The existing search
error state remains visible to the user.

### Flutter search screen

The screen renders and announces `hit.snippet` when present. Legacy fallback
hits continue using the existing rune-safe 200-scalar opening excerpt.
Canonical snippets do not receive a second truncation or markup interpretation.

Existing product behavior remains:

- hits are grouped by session;
- messages within a group and groups themselves are sorted chronologically with
  current deterministic tie-breaks;
- active and archived hits remain visible, and tapping either invokes the
  existing `onOpenSession` callback;
- session-title enrichment remains independently bounded; and
- score is not displayed and does not reorder the current UI.

The score is still available to API and model consumers. Preserving the
chronological grouped UI avoids an unrelated relevance-layout redesign while
the backend's `hits` array retains canonical relevance order. Any later
reconciliation of an archived selection remains TUR-008-owned; MEM-002 neither
weakens nor redesigns that shell lifecycle.

## Observability and privacy

MEM-002 adds no durable telemetry, score threshold, query log, snippet log, or
content-bearing audit row. That keeps the task within the existing privacy and
MEM-001 derived-state contract. The orchestrator has no general RPC
status/latency instrumentation today, and MEM-002 does not claim or introduce a
broad interceptor.

New fail-closed metadata invariants use typed internal classes for marker
entropy, marker collision/structure, snippet sanitization, and score
normalization. Before returning the existing generic
`Internal: search messages failed`, the service writes one standard
content-free process log containing only the operation and invariant class. It
never logs the wrapped error, query, message, snippet, score, marker, session
title/ID, or raw SQL row. Tests capture the log and prove sentinel content is
absent. Ordinary database/RPC failures retain their existing behavior.

MEM-003 owns checked-in evaluation metrics; production search does not start
collecting recall-quality data in this task.

## Legacy deprecation

`SearchMessagesResponse.messages` is documented as the legacy format but is not
yet marked with protobuf's deprecation option. It remains fully populated for
`UNSPECIFIED` and `LEGACY_MESSAGES` requests and empty for explicit `HITS`
requests. It cannot be removed when MEM-002 lands.

Removal requires all in-repository consumers, including runtime recall, to use
`hits`; a documented compatibility window; evidence that supported clients no
longer depend on field 1; and a versioned breaking API decision. If removal is
ever approved, both the field number and name are reserved. Until then,
messages/hits parity is a tested server invariant.

## Documentation changes with implementation

The implementation updates:

- `docs/architecture/session-recall.md` with score direction, comparison scope,
  tie-break, snippet safety/bounds, archived visibility, and negotiated
  dual-field migration;
- the MEM-002 roadmap status in
  `docs/architecture/2026-08-18-personal-agent-audit.md` without claiming
  MEM-003 or MEM-004 behavior; and
- proto comments so generated Go/Dart API documentation carries the contract.

No memory-governance exception or schema migration is needed because scores and
snippets are transient projections of the returned source message.

## RED to GREEN test matrix

Every behavior change starts with a failing test that demonstrates the missing
contract.

### Repository

- Ranking fixture returns higher normalized scores first and preserves the
  existing BM25 winner.
- Equal BM25 rows order by `message_id ASC` and expose equal scores.
- Scores are finite, non-negative, and exactly the negation of SQLite's raw
  result; zero is positive zero.
- `normalizeSearchScore` directly rejects NaN, both infinities, and positive
  raw values rather than clamping or emitting a partial result.
- A majority-document-frequency canary pins the linked FTS5 implementation's
  finite non-positive result.
- Marker construction produces the exact versioned ASCII START/END grammar from
  16 fixed entropy bytes: one shared 32-character lowercase-hex nonce, no NUL or
  control byte, exact 65/63-byte lengths, and distinct domain-separated values.
- A linked-driver canary selects both bound markers back as TEXT and requires
  byte-for-byte equality, then uses the real external-content schema to require
  those exact bytes around start, middle, and end matches.
- Match near the beginning, middle, and end yields a fragment containing the
  matched phrase with SQLite edge ellipses where applicable.
- Source text containing fixed marker-like ASCII cannot forge the per-query
  nonce-bearing markers; forced complete start/end collisions fail closed.
- Raw-byte parser tests cover valid sequential pairs, missing partners, nesting,
  reversed order, a start marker inside `match`, an end marker inside `text`,
  and an empty snippet; a snippet with zero complete markers is a valid
  marker-free fragment whose payload and empty match list are asserted, and
  partial marker text does not turn a structurally broken snippet into one.
- A phrase longer than the 32-token snippet window returns one hit whose snippet
  is a bounded, unhighlighted fragment of that same message: the repository test
  first proves the marked projection really carries no markers, then requires
  legacy/hit parity on the message, a valid public score, both caps, and that
  the snippet minus edge ellipses is a substring of the hit's own content.
- The implicit whole-fragment span is applied only when no span was recorded: a
  unit test proves it leaves recorded spans, including empty ones, untouched,
  and that a marker-free fragment which sanitizes to nothing still fails closed.
- Invalid UTF-8 immediately adjacent to markers is repaired only after parsing;
  tests prove it cannot confuse START with END and that neither complete
  generated marker survives in the public snippet.
- A neighboring message containing a sentinel secret never contributes text to
  the hit snippet.
- HTML/Markdown/ANSI-looking source remains inert plain text; no match markers
  are inserted.
- Newline, tab, C0/C1, DEL, and explicit bidi-control fixtures produce one-line
  output with visible replacements and no directional override.
- ASCII, emoji, combining text, RTL text, and CJK remain valid UTF-8 and are not
  split.
- A normal match remains complete after match-centered truncation near either
  edge.
- One oversized matched token and a many-token body satisfy both the 200-scalar
  and 800-byte caps, retain match text, and use only the required edge
  ellipses.
- Invalid UTF-8 external-content data is repaired with U+FFFD; an empty
  sanitized snippet fails closed.
- A deliberately shortened divergent external-content row produces no markers
  and returns an excerpt of its own current content, the same way an over-window
  phrase does.
- A balanced-but-stale divergence fixture documents SQLite's out-of-band
  corruption boundary: returned snippet text remains confined to that message,
  but match correctness is not claimed.
- Exact identifiers in message content match and center the snippet; an ID that
  exists only in `messages.id` does not gain new lookup semantics.
- Quotes, FTS operators, NUL delimiters, punctuation-only input, and malformed
  operator-looking strings preserve literal-query and injection-resistance
  behavior.
- Active and archived sessions are returned; deleting/deleted sessions are not.
- A schema contract test pins the `active`/`archived` status domain so any new
  status requires an explicit search-visibility decision.
- Explicit scope, explicit exclusion, no-match, valid limit, defaulted invalid
  limit, maximum limit, cancellation, row-scan error, and `rows.Err` paths keep
  their existing behavior and close the reader.

### Service and wire

- Descriptor tests pin `SearchHit.message = 1`, `score = 2`, `snippet = 3`,
  response-format enum values 0-2, request `response_format = 5`,
  `SearchMessagesResponse.messages = 1`, and `hits = 2`.
- Proto generation and breaking checks accept the additive schema against
  `main`.
- An old client request decoded by the new server resolves unspecified format
  to legacy messages.
- A new hit-format request decoded by the pre-MEM-002 server ignores unknown
  request field 5 and returns legacy messages.
- A new hit response decoded with the pre-MEM-002 response descriptor ignores
  unknown field 2.
- An old-shaped response decoded by new bindings has messages and no hits.
- For ranked, tied, scoped, excluded, archived, limited, and empty searches,
  separate legacy- and hit-format calls return protobuf-equal
  `messages`/`hits.map(message)` in count, value, and order when both calls
  succeed against a trigger-consistent database.
- A phrase wider than the 32-token snippet window keeps that parity over the
  wire: both formats return the same message and the hit carries a non-empty
  bounded snippet instead of `Internal`.
- Service hits carry the repository score/snippet without recomputation.
- Each response populates only its requested array, and a near-4 MiB legacy
  fixture remains within the current transport behavior instead of being
  duplicated.
- Nil/blank queries and unknown response formats are `InvalidArgument`;
  tokenless queries remain empty success; database, random-marker, score,
  snippet, and mapping failures remain value-free `Internal`.
- Each typed metadata-invariant failure emits one operation/class-only process
  log; captured logs contain no query, message, snippet, score, marker, session
  identifier, or wrapped database text.
- TUR-004 deletion tests assert that neither response field can return the
  withdrawn sentinel before or after restart.

### Go runtime compatibility

- The orchestrator client leaves response format unspecified and receives the
  same legacy `messages` payload and `memory.Excerpt` sequence as before.
- A capturing service proves runtime recall never opts into hit-format snippet
  work ahead of MEM-004.
- Existing recall deduplication, current-session suppression, term ranking,
  timeout, and rendered-injection tests remain unchanged and green.

### Dart mapping and networking

- Canonical proto hits map message, session ID, score, and snippet.
- Missing message, NaN, infinity, negative score, and empty snippet fail mapping
  explicitly with constant value-free errors.
- The client requests hit format.
- A nonconforming response with hits and duplicate messages returns hits once.
- An old-server response with only messages uses nullable legacy metadata and
  retains every message value and order.
- An empty response returns an empty list rather than selecting an ambiguous
  fallback.
- `Internal`, `ResourceExhausted`, and timeout failures propagate without an
  automatic legacy retry.
- Malicious message/snippet/query sentinels never appear in mapper exceptions,
  visible error copy, or the live-region semantics label.
- Query bytes, empty global scope, request field 5, limit 50/defaults, and unary
  deadline are pinned.
- Generated and domain `SearchHit` imports remain explicitly aliased.

### Flutter UI

- Canonical rows render and announce the server snippet, including a match near
  the end of an oversized body, without rendering or announcing the full body.
- Legacy fallback rows retain the existing 200-scalar rune-safe excerpt.
- `<script>`, Markdown, ANSI-looking text, bidi fixtures, emoji, RTL, and CJK are
  rendered through `Text` as inert, bounded text.
- Score does not change chronological within-session order or group order.
- Relevance-order input, active/archived hit visibility and tap callback, title
  lookup limits, loading, empty, error, stale-generation, semantics, and the
  50-result request remain compatible. TUR-008 shell reconciliation tests remain
  unchanged rather than asserting a new archived-selection lifecycle.

## Approval and implementation gate

This document is the complete MEM-002 behavior design. No implementation,
generated-code update, or implementation plan starts from the current branch.
After explicit coordinator approval and PR #68 landing on `main`, the worktree
must fetch and normally merge current `main`, then implement against the landed
TUR-008 surfaces while preserving this contract.
