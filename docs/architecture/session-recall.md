# Session Recall Scope

This release ships the durable retrieval capability for session messages:
SQLite FTS5 indexing kept current by database triggers, ranked and
session-scoped search, and the public `SessionService.SearchMessages` RPC. The
Flutter Sessions screen exposes that RPC through **Search conversations**.
Search spans all sessions, treats the query as one exact phrase, groups matches
by conversation, and opens the selected conversation. Group headings use the
session title when it can be loaded and otherwise retain a session-ID fallback.

Two follow-up layers are deliberately deferred:

1. Before answering, the agent must recall the top-K relevant messages and inject them into the model context. This is the layer that turns retrieval into user-visible conversational memory.
2. An LLM must summarize recalled results before use or presentation for Hermes parity.

Neither layer is part of this plan. Users and API callers may search messages
now, but the runtime does not automatically add recalled messages to model
prompts and does not summarize recall results.
