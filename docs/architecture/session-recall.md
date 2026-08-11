# Session Recall Scope

This release ships the durable retrieval capability for session messages: SQLite FTS5 indexing kept current by database triggers, ranked and session-scoped search, and the public `SessionService.SearchMessages` RPC. It makes prior conversation turns available to callers without changing how the agent answers.

Two follow-up layers are deliberately deferred:

1. Before answering, the agent must recall the top-K relevant messages and inject them into the model context. This is the layer that turns retrieval into user-visible conversational memory.
2. An LLM must summarize recalled results before use or presentation for Hermes parity.

Neither layer is part of this plan. Callers may search messages now, but the runtime does not automatically add recalled messages to model prompts and does not summarize recall results.
