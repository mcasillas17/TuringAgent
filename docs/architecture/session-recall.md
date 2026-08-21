# Session Recall Scope

This release ships the durable retrieval capability for session messages:
SQLite FTS5 indexing kept current by database triggers, ranked and
session-scoped search, and the public `SessionService.SearchMessages` RPC. The
Flutter Sessions screen exposes that RPC through **Search conversations**.
Search spans all sessions, treats the query as one exact phrase, groups matches
by conversation, and opens the selected conversation. Group headings use the
same orchestrator-owned title that the session list receives through
`session.updated`, and otherwise retain a session-ID fallback.

Two follow-up layers were deferred at the time. **Layer 1 has since shipped** (#18, #25): the runtime recalls relevant excerpts from earlier sessions and prepends them to the model context before answering, and #33 added the notice that tells the user when it did. See `agent-runtime-go/internal/memory/recall.go` and the call site in `general_assistant.go`.

1. ~~Before answering, the agent must recall the top-K relevant messages and inject them into the model context.~~ **Shipped.**
2. An LLM must summarize recalled results before use or presentation for Hermes parity. **Still deferred** — recall injects raw excerpts, not summaries.

Search remains available to API callers directly via
`SessionService.SearchMessages` and to users in the Flutter client. Automatic
runtime recall injects raw excerpts; the summary layer remains deferred.

## Scored search hits

`SearchMessages` now answers in one of two negotiated shapes, selected by
`SearchMessagesRequest.response_format`.

### Negotiated response formats

- `SEARCH_MESSAGES_RESPONSE_FORMAT_UNSPECIFIED` and
  `SEARCH_MESSAGES_RESPONSE_FORMAT_LEGACY_MESSAGES` return
  `SearchMessagesResponse.messages` only; `hits` stays empty.
- `SEARCH_MESSAGES_RESPONSE_FORMAT_HITS` returns
  `SearchMessagesResponse.hits` only; `messages` stays empty.
- A numeric value the server does not recognize is rejected with
  `InvalidArgument`. There is no default and no downgrade guess.

The two lists are never populated together, so a response never carries the
same message bodies twice. Both formats share one predicate and one ordering,
so they can only differ in projection, never in which rows are visible.

A new client asking for `HITS` against an older server that has no such field
gets that server's message-shaped response back. The client reads `hits` when
they are present and otherwise maps `messages` through its legacy path, in the
same single response. That mixed-version case is a successful legacy fallback,
not an error path: there is one RPC per search, a failed call is never retried
in the other format, and the two lists are never concatenated or merged.

### Score semantics

`SearchHit.score` is `-bm25(messages_fts)`. FTS5 already negates the
conventional BM25 formula so that ascending order is best-first; the public
score negates it back so that larger is better. The value is finite and
non-negative, and an unexpected non-finite or positive raw value fails the
query instead of being clamped into a plausible-looking number.

The ordering the score expresses is meaningful **only within one response**:
one query against one database snapshot. BM25 is a corpus-derived aggregate,
so its document frequencies and average lengths shift as messages are written
and deleted. A score is therefore not a probability, a confidence, a
percentage, or a cross-query, cross-session, or cross-snapshot metric, and no
threshold may be derived from it.

Results are ordered by `bm25(messages_fts)` then `m.id`, so equal scores are
broken deterministically by `message_id ASC` and repeated identical queries
return identical order. MEM-003 will consume that relative order; nothing
consumes the magnitude.

### Snippet safety

`SearchHit.snippet` is built by the server from the matched message's own
`content` and from nothing else — never from a neighboring message, another
session, or the query text.

The excerpt is selected by FTS5 first, before any of the post-processing bounds
below apply. `snippet(messages_fts, 0, ?, ?, '…', 32)` — where the two bound
parameters carry the per-query match markers — returns a match-centered
fragment of **at most 32 FTS5 tokens**, marking omitted edges with the `…`
literal passed to that call. That token bound is independent of the 200-scalar
and 800-byte caps, so a snippet is *not* guaranteed to be the whole message
even when the message is comfortably under both post-processing caps: a
message of more than 32 tokens is cut by FTS5, and the leading or trailing edge
of its source text can be missing.

The window is also what the *phrase* has to fit into. A phrase of more than 32
tokens has no occurrence that fits, so FTS5 returns a fragment of the matched
message with no match markers at all. That is a legitimate result, not a
failure: the search still returns the message, and its snippet is a bounded,
unhighlighted excerpt of that same message rather than a quotation of the
phrase. So the guarantee is narrower than "a buried phrase is always quoted" —
a phrase that fits the window and is buried deep in a long body is quoted
instead of being replaced by the body's opening, while a phrase wider than the
window yields an excerpt that may not contain it.

The published snippet is single-line literal plain text:

- FTS5 wraps matched phrases in per-query, high-entropy ASCII markers that are
  stripped inside the repository; a structurally broken marker sequence, or
  message content that impersonates a marker, fails the whole query rather than
  producing an ambiguous snippet. A fragment with no markers at all is not
  broken — it is what an over-window phrase produces — and is published as a
  plain excerpt. No server-added markup of any kind reaches the caller, and
  matched text carries no emphasis in either case.
- Any HTML-, Markdown-, ANSI-, or otherwise control-sequence-*looking* text in
  the stored message is left exactly as stored: those are ordinary printable
  source characters and stay literal, so `<b>`, `**bold**`, and a written-out
  `\x1b[31m` survive verbatim rather than being escaped, stripped, or
  interpreted. The replacement rule applies only to *actual* control code
  points — real C0, C1, and DEL characters. Those that are whitespace, such as
  tab and newline, normalize to a space first (see below); every remaining
  non-whitespace control becomes U+FFFD. Clients must render the snippet as
  plain text and must never parse or interpret it.
- Invalid UTF-8 becomes U+FFFD. Unicode whitespace and line separators — which
  include the whitespace C0 controls — collapse to single ASCII spaces and are
  trimmed, so the value is always one line. The C0, C1, and DEL controls that
  remain after that step are non-whitespace, and they, together with the
  explicit bidi formatting controls (U+061C, U+200E–U+200F, U+202A–U+202E,
  U+2066–U+2069), become U+FFFD, so a result cannot display something other
  than what was stored. Natural right-to-left letters and joiners are content
  and are preserved.
- After FTS5's 32-token fragment bound, the result is bounded twice more: at
  most 200 Unicode scalar values and at most 800 UTF-8 bytes, with every cut
  landing on a scalar boundary. A single matched token larger than the caps is
  truncated rather than allowed to grow the response.
- A cut edge is marked with an inserted U+2026, which is charged against both
  caps like any other content. The complete match outranks those indicators: a
  sanitized match that fits both caps on its own is published whole, and the
  indicators are added only when every edge that needs one fits beside it. When
  they cannot, all of them are omitted rather than shortening a match that fits,
  so a match of exactly 200 scalars or exactly 800 bytes is returned unmarked
  even when the message continues on both sides. Surrounding context is added
  only after the whole match is secured, and pays for its own indicators.
- Producing that snippet costs a fixed amount of working memory per row. FTS5's
  32-token bound does not bound the length of one token, so a single unbroken
  multi-megabyte token can arrive as one fragment; sanitization keeps only the
  scalars a publishable window could reach — two caps' worth around the match —
  and counts the rest, so a hostile or merely unusual message cannot make the
  server allocate in proportion to it.

A hit whose score or snippet cannot be proven safe fails the entire query with
an opaque `Internal` error; the server never emits a partial, defaulted, or
guessed hit, and never puts message, snippet, or query text in an error or log.

### Visibility, storage, and egress

Because both projections share the same predicate, hits and legacy messages
agree on lifecycle: messages in `active` and `archived` sessions remain
visible, while sessions that are `deleting` or already deleted are excluded
from both. Scope, exclusion, the limit domain, and literal-phrase handling are
unchanged: a limit of 1–100 is used as given, while a limit that is `<= 0` or
`> 100` is not an error and falls back to 20. A query with no FTS5 token is
still a successful empty response.

No score, snippet, cache, side table, or derived row is persisted. Both values
exist only inside the response that computed them. Scoring and snippet
construction are local SQLite work in the orchestrator process, so this
capability performs no remote egress and sends no message content anywhere.

### Runtime recall is unchanged

The runtime's orchestrator client deliberately leaves `response_format`
unspecified and keeps reading `messages`, so recall still injects its own raw
excerpts with its existing term extraction, deduplication, and re-ranking.
Migrating recall to `hits` belongs to MEM-004; this layer does not partially
change recall ranking. The Flutter client is the consumer that requests `HITS`
today and renders the server snippet verbatim, keeping its own opening-excerpt
cut only as the mixed-version fallback.
