# TuringAgent Flutter Client

This is the v1.0 Flutter client for TuringAgent. It is a thin, protocol-driven UI for the local Go gRPC orchestrator: the backend owns sessions, messages, model routing, approvals, tool execution, persistence, and audit state.

The client preserves the existing polished `ResponsiveShell` experience. Backend-connected chat, sessions, and settings are integrated into that shell instead of replacing it with a plain debug root.

## Current Status

Implemented in the client:

- Existing TuringAgent app shell with desktop navigation rail and mobile drawer.
- Backend URL and API key settings stored through secure client storage.
- gRPC client for config, sessions, message search, event replay, streaming session events, and approval actions.
- Chat tab wired to backend sessions and streamed message deltas.
- Active conversations are cursor-paginated and expose rename, archive, and
  permanent delete actions; an archived-conversations dialog paginates,
  renames, restores, or permanently deletes archived rows.
- Exact-phrase conversation search across all sessions, grouped by conversation
  and linked back to the matching chat.
- Inline tool-call status cards for live `tool.call.*` events.
- Inline notices when a live agent run reaches its tool-iteration limit.
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

- **Chat** lists active sessions in `ResponsiveShell`, loads additional cursor
  pages, opens backend-connected `ChatScreen` instances, and exposes search and
  archived-conversation management.
- **Devices** is a placeholder: `IoT Devices Dashboard`.
- **Stats** is a placeholder: `Stats & Usage`.
- **Integrations** is a placeholder: `Integrations Status`.
- **Settings** renders the real backend URL/API key settings screen.

This keeps theme logic, app colors, desktop rail behavior, mobile drawer behavior, and placeholder tabs intact while adding backend-connected client surfaces.

## Chat And Sessions

The Chat tab uses the generated gRPC services for commands, queries, and streamed events:

- `SessionService.GetConfig` for backend capabilities and model providers.
- `SessionService.ListSessions`, `SessionService.GetSession`, and
  `SessionService.CreateSession` for paginated chat sessions and search-result
  headings.
- `SessionService.RenameSession`, `SessionService.ArchiveSession`, and
  `SessionService.RestoreSession` for explicit lifecycle actions. Returned
  session snapshots remain authoritative.
- `SessionService.ListMessages` to load persisted messages.
- `SessionService.SearchMessages` to search one exact phrase across all
  sessions. Search results appear immediately; unresolved session titles use a
  session-ID fallback and update when metadata arrives.
- `ChatService.SendMessage` to enqueue a user message and selected model provider.
- `EventService.ListEvents` and `EventService.SubscribeSessionEvents` for replay and live updates.
- `ApprovalService.ApproveApproval` and `ApprovalService.DenyApproval` for approval cards.

When a session opens, `ChatScreen` loads persisted messages and subscribes to the session event stream. Incoming `message.delta` events update the active assistant message locally rather than making the client own model execution. Live tool calls render in order between message bubbles, and an `agent.run.step` event renders the runtime-provided note as accessible meta text when the tool-iteration limit cuts a run short.

The shell preserves session timestamp nanoseconds and reconciles list pages,
lifecycle RPC responses, and `session.updated` events by authoritative snapshot.
An archived row cannot be restored by omission from an active page or by an
older status-less event.

Historical tool cards and run notices are suppressed during event replay because persisted messages do not carry event sequence values that could place those artifacts back into the transcript correctly. Live events committed after the screen's startup watermark still render normally.

Approval cards appear from `approval.requested` and are removed on `approval.approved`, `approval.denied`, `approval.expired`, or `approval.consumed`.

## Important Files

- `lib/app.dart`: loads saved client config and chooses Settings or `ResponsiveShell`.
- `lib/ui/shell/responsive_shell.dart`: polished TuringAgent shell and tab integration.
- `lib/features/settings/settings_screen.dart`: backend URL/API key form.
- `lib/features/sessions/session_list_screen.dart`: backend session list and new-chat flow.
- `lib/features/search/search_screen.dart`: debounced, accessible exact-phrase
  conversation search and grouped result navigation.
- `lib/features/chat/chat_screen.dart`: active backend-connected chat screen for message loading, sending, streaming deltas, inline run/tool activity, and approvals.
- `lib/features/chat/tool_call_card.dart`: inline live tool-call lifecycle UI.
- `lib/features/chat/run_notice_card.dart`: accessible inline metadata for a truncated live run.
- `lib/features/approvals/approval_card.dart`: approve/deny UI.
- `lib/features/chat/model_provider_selector.dart`: provider selection control.
- `lib/models/`: typed client models for sessions, messages, approvals, config, and streamed Turing events.
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
