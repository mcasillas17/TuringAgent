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
