# TuringAgent Flutter Client

This is the v1.0 Flutter client for TuringAgent. It is a thin, protocol-driven UI for the local Go gRPC orchestrator: the backend owns sessions, messages, model routing, approvals, tool execution, persistence, and audit state.

The client preserves the existing polished `ResponsiveShell` experience. Backend-connected chat, sessions, and settings are integrated into that shell instead of replacing it with a plain debug root.

## Current Status

Implemented in the client:

- Existing TuringAgent app shell with desktop navigation rail and mobile drawer.
- Backend URL and API key settings stored through secure client storage.
- gRPC client for config, sessions, message search, event replay, streaming session events, and approval actions.
- Chat tab wired to backend sessions and streamed message deltas.
- Exact-phrase conversation search across all sessions, grouped by conversation
  and linked back to the matching chat.
- Inline tool-call status cards for live `tool.call.*` events.
- Localized lifecycle/outcome cards reconstructed from the same versioned
  `RunState` used by live events and persisted message history.
- Inline safe notices for live run limits, retries, and recovery.
- Approval cards for `approval.requested` events, cleared by approval terminal events.
- Model provider selector for `ollama` or `openai_compatible` per sent message.

Provisional until the full local stack is running:

- End-to-end chat responses require the Go orchestrator, Go agent runtime, model provider, and event stream.
- Approval cards require the backend/runtime to emit approval events.
- Devices, Stats, and Integrations remain placeholders.

## Run Locally

From the repository root:

```bash
cd turing-client/turing_app
flutter pub get
flutter run -d macos
```

Use `flutter devices` to choose another target, such as Chrome or a connected Android device. For physical devices, the backend URL usually needs the host machine's LAN or Tailscale address rather than `localhost`.

Run client verification:

```bash
cd turing-client/turing_app
flutter analyze
flutter test
```

## Backend Settings Flow

On first launch, or when saved credentials are missing, the app opens `SettingsScreen`.

Enter:

- **Backend URL**: typically `http://localhost:3000` on the development machine.
- **API key**: the client API key printed by the backend initialization flow.

After saving, `TuringApp` reloads stored settings and opens the existing `ResponsiveShell`. The Settings tab remains available inside the shell so backend URL or API key can be updated later.

The current client sends authenticated gRPC metadata using:

```text
authorization: Bearer <api-key>
```

## Shell Integration

`ResponsiveShell` remains the primary app surface:

- **Chat** renders `SessionListScreen`, which lists sessions once the backend is
  available, opens backend-connected `ChatScreen` instances, and exposes
  **Search conversations** in both embedded and standalone layouts.
- **Devices** is a placeholder: `IoT Devices Dashboard`.
- **Stats** is a placeholder: `Stats & Usage`.
- **Integrations** is a placeholder: `Integrations Status`.
- **Settings** renders the real backend URL/API key settings screen.

This keeps theme logic, app colors, desktop rail behavior, mobile drawer behavior, and placeholder tabs intact while adding backend-connected client surfaces.

## Chat And Sessions

The Chat tab uses the generated gRPC services for commands, queries, and streamed events:

- `SessionService.GetConfig` for backend capabilities and model providers.
- `SessionService.ListSessions`, `SessionService.GetSession`, and
  `SessionService.CreateSession` for chat sessions and search-result headings.
- `SessionService.ListMessages` to load persisted messages.
- `SessionService.SearchMessages` to search one exact phrase across all
  sessions. Search results appear immediately; unresolved session titles use a
  session-ID fallback and update when metadata arrives.
- `ChatService.SendMessage` to enqueue a user message and selected model provider.
- `EventService.ListEvents` and `EventService.SubscribeSessionEvents` for replay and live updates.
- `ApprovalService.ApproveApproval` and `ApprovalService.DenyApproval` for approval cards.

When a session opens, `ChatScreen` loads persisted messages and subscribes to the session event stream. Incoming `message.delta` events update the active assistant message locally rather than making the client own model execution. Live tool calls render in order between message bubbles. Safe `agent.run.step` categories render localized notices; backend-provided failure prose is never displayed.

The assistant message's embedded `RunState` is authoritative after reopen.
`ChatScreen` reconciles each run by monotonic state version, drops stale or
conflicting events, and renders recovery or terminal cards adjacent to the
correlated assistant content. Completed content has no redundant card; empty
success, failed, cancelled, missing-content, unknown, and neutral legacy states
remain explicit. Initial buffering retains at most 64 run states and overflow
causes one coalesced newest-page resync.

Historical tool cards and nonterminal run notices are suppressed during event replay because persisted messages do not carry event sequence values that could place those artifacts back into the transcript correctly. Live events committed after the screen's startup watermark still render normally, and the durable run outcome itself always comes from message history.

Approval cards appear from `approval.requested` and are removed on `approval.approved`, `approval.denied`, `approval.expired`, or `approval.consumed`.

## Important Files

- `lib/app.dart`: loads saved client config and chooses Settings or `ResponsiveShell`.
- `lib/ui/shell/responsive_shell.dart`: polished TuringAgent shell and tab integration.
- `lib/features/settings/settings_screen.dart`: backend URL/API key form.
- `lib/features/sessions/session_list_screen.dart`: backend session list and new-chat flow.
- `lib/features/search/search_screen.dart`: debounced, accessible exact-phrase
  conversation search and grouped result navigation.
- `lib/features/chat/chat_screen.dart`: active backend-connected chat screen for message loading, sending, streaming deltas, inline run/tool activity, and approvals.
- `lib/features/chat/run_state_reconciler.dart`: pure run-ID/version acceptance
  rules shared by history and live state.
- `lib/features/chat/run_state_card.dart`: localized durable lifecycle/outcome
  presentation, including legacy and missing-content fallbacks.
- `lib/features/chat/tool_call_card.dart`: inline live tool-call lifecycle UI.
- `lib/features/chat/run_notice_card.dart`: localized accessible metadata for
  allowlisted run notices.
- `lib/features/approvals/approval_card.dart`: approve/deny UI.
- `lib/features/chat/model_provider_selector.dart`: provider selection control.
- `lib/models/`: typed client models for sessions, messages, approvals, config,
  versioned run state, and streamed Turing events.
- `lib/networking/api_client.dart`: typed API interface shared by widgets and gRPC implementation.
- `lib/networking/grpc_client.dart`: gRPC protocol client.
- `lib/networking/grpc_event_source.dart`: gRPC event stream client.
- `lib/networking/event_source.dart`: event stream abstraction used by widgets and tests.
- `lib/networking/auth_storage.dart`: secure storage abstraction.
- `test/ui/responsive_shell_backend_test.dart`: shell regression test proving the polished shell still wraps backend chat.

## Developer Notes

- Keep the Flutter client thin. Do not move orchestration, memory, routing, tool policy, approval decisions, or persistence into Flutter.
- Preserve `ResponsiveShell` as the main authenticated app surface. Add new client views as tabs or shell-integrated screens rather than replacing the root.
- Prefer the `TuringApi` and `TuringEventSource` interfaces in widgets so tests can use fakes without network access.
- Keep Devices, Stats, and Integrations visibly present but placeholder-only until their backend contracts are defined.
- Avoid claiming full end-to-end chat readiness in UI or docs until the orchestrator/runtime pipeline is available and verified.
