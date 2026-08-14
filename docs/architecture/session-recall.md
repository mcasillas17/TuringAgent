# Session Recall Scope

This release ships the durable retrieval capability for session messages: SQLite FTS5 indexing kept current by database triggers, ranked and session-scoped search, and the public `SessionService.SearchMessages` RPC. It makes prior conversation turns available to callers without changing how the agent answers.

Two follow-up layers were deferred at the time. **Layer 1 has since shipped** (#18, #25): the runtime recalls relevant excerpts from earlier sessions and prepends them to the model context before answering, and #33 added the notice that tells the user when it did. See `agent-runtime-go/internal/memory/recall.go` and the call site in `general_assistant.go`.

1. ~~Before answering, the agent must recall the top-K relevant messages and inject them into the model context.~~ **Shipped.**
2. An LLM must summarize recalled results before use or presentation for Hermes parity. **Still deferred** — recall injects raw excerpts, not summaries.

Search remains available to callers directly via `SessionService.SearchMessages`.
