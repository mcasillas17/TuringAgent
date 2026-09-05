# TuringAgent Flutter Client

This is the Flutter client for TuringAgent. It is a thin, protocol-driven UI for the local Go gRPC orchestrator: the backend owns sessions, messages, model routing, approvals, tool execution, persistence, and audit state.

The client preserves the existing polished `ResponsiveShell` experience. Backend-connected chat, sessions, and settings are integrated into that shell instead of replacing it with a plain debug root.

## Current Status

The [canonical roadmap](../../docs/NORTH_STAR.md) separates current behavior
from future work. The scoped status cells below are checked by the
[offline documentation guard](../../tools/docs/README.md); they do not certify
live providers, MCP conformance, or production mobile support.

<!-- status-guard:begin -->
| Claim | Status | Scope |
| --- | --- | --- |
| flutter-search | shipped | Exact-phrase conversation search is wired from the shell through gRPC to backend search. |
| flutter-workspace | shipped | The named destinations described below load real backend state, not placeholder pages. |
| mcp-registry | shipped | The MCPs page manages registrations, imports, enablement, tokens and tool policies. |
| mcp-lifecycle | pending | Registry management and the HTTP JSON-RPC tools subset do not implement initialization/capability negotiation (CON-001). |
| remote-model-routing | shipped | Agents manages endpoint records; the conversation's destination bar selects the route through ExternalAgentService, and the runtime calls the model under per-run disclosure. |
| agent-delegation | pending | ExternalAgentService is model routing, not A2A or access to an existing vendor-product conversation. |
| github-tools | shipped | Connected GitHub credentials have issue/file read tools and approval-gated issue comments. |
| other-integration-tools | pending | IMAP, CalDAV and Notion currently store credentials without functional tools; do not connect them expecting mail, calendar or notes operations. |
| mobile-client | pending | Responsive layouts and iOS/Android scaffolding are not a production mobile client; the main Android manifest lacks INTERNET permission while debug/profile grant it. |
| mobile-reachability | pending | The default host API is loopback-only; LAN/tailnet URLs alone cannot make a phone reach it securely. |
<!-- status-guard:end -->

Implemented in the client:

- Existing TuringAgent app shell with desktop sidebar and compact drawer.
- Backend URL and API key settings stored through secure client storage.
- gRPC client for config, sessions, message search, event replay, streaming session events, and approval actions.
- Chats destination wired to backend sessions and streamed message deltas.
- Active conversations are cursor-paginated and expose rename, archive, and
  permanent delete actions; an archived-conversations dialog paginates,
  renames, restores, or permanently deletes archived rows.
- Exact-phrase conversation search across all sessions, grouped by conversation
  with result selection setting the underlying chat. Dismiss search to see it;
  selecting a result does not itself close the search route. There is no date filter.
- Inline tool-call status cards for live `tool.call.*` events.
- Localized lifecycle/outcome cards reconstructed from the same versioned
  `RunState` used by live events and persisted message history.
- Inline safe notices for live run limits, retries, and recovery.
- Approval cards for `approval.requested` events, cleared by approval terminal events.
- Model provider preference for `ollama` or `openai_compatible`; every effective
  remote send separately confirms its exact endpoint and disclosed categories.
- Typed session-withdrawal receipts and terminal `session.deleted` events. The
  shell removes a conversation only after a completed receipt or its terminal
  event; an in-progress or failed-external receipt remains visible for retry.

Runtime prerequisites and limits:

- End-to-end chat responses require the Go orchestrator, Go agent runtime, model provider, and event stream.
- Approval cards require the backend/runtime to emit approval events.
- A functional management page does not imply every connector or protocol it
  names works. GitHub has tool consumers; IMAP, CalDAV and Notion currently
  only accept credentials. Google, Microsoft and Slack account connections
  are not implemented. INT-001 will change powerless-credential acceptance;
  this documentation change does not.

## Run Locally

From the repository root:

```bash
cd turing-client/turing_app
flutter pub get
flutter run -d macos
```

Use `flutter devices` to inspect available development targets, not as a list
of supported deployments. The current client uses native `ClientChannel`
gRPC; this guide does not provide a working Chrome/gRPC-Web deployment.

The supported local setup is the desktop client on the backend host. Compose
publishes the public gRPC API on **127.0.0.1** (default port `3000`). Merely
setting a phone's backend URL to the host's LAN or Tailscale address does not
make that loopback listener reachable. Do not expose it on `0.0.0.0` or put
the shared desktop bearer behind a public tunnel as a mobile workaround.

iOS/Android scaffolding and phone-sized layouts are present, but production
mobile support is pending: the Android `src/main/AndroidManifest.xml` has no
`INTERNET` permission (debug/profile manifests do), and device pairing,
revocation and an authenticated non-loopback TLS transport are not implemented.
See SEC-001 and MOB-001 in the canonical roadmap. A future private overlay
transport is a design target, not a setup step that works today.

Run client verification:

```bash
cd turing-client/turing_app
flutter analyze
flutter test
```

## Backend Settings Flow

On first launch, or when saved credentials are missing, the app opens `SettingsScreen`.

Enter:

- **Backend URL**: `http://localhost:3000` on the backend host (or the configured
  loopback port).
- **API key**: the client API key printed by the backend initialization flow.

After saving, `TuringApp` reloads stored settings and opens `ResponsiveShell`.
Settings remains available from the sidebar, rather than as a workspace
destination, so the backend URL or API key can be updated later.

The current client sends authenticated gRPC metadata using:

```text
authorization: Bearer <api-key>
```

## Shell Integration

`ResponsiveShell` remains the primary app surface:

- **Chats** lists active sessions in `ResponsiveShell`, loads additional cursor
  pages, opens backend-connected `ChatScreen` instances, and exposes search and
  archived-conversation management.
- **Skills** lists file-backed skills and manages enablement and capability grants.
- **Memory** displays the file-backed vault, pinned documents and proposal
  decisions. This does not imply automatic learning from conversation.
- **Integrations** lists providers and connections with connect/revoke/delete
  actions and GitHub tool policies; credential storage is not functional
  IMAP/CalDAV/Notion support.
- **MCPs** manages the tool-server registry and policies. The current transport
  is an HTTP JSON-RPC subset, not full MCP lifecycle conformance.
- **Automations** manages interval/daily runs and their explicit tool allowlists;
  it does not provide mobile/channel delivery.
- **Agents** manages remote model endpoint records. The conversation's
  `SessionAgentBar` selects, reads and clears its route through
  `ExternalAgentService`; model execution stays in the runtime. Vendor labels
  do not select native Anthropic/Gemini adapters or delegate to a user's
  existing Claude/Copilot/Gemini/ChatGPT session.
- **Telemetry** shows backend local usage aggregates, not a user-facing audit log.

The destination list is defined in `ShellDestination`; `ResponsiveShell`
wires each page to `TuringApi`, and `TuringGrpcApi` forwards calls to generated
clients. The older Devices/Stats placeholder descriptions do not describe these
pages. Responsive sidebar/drawer behavior is a layout capability, not proof of
mobile networking.

## Chat And Sessions

The Chats destination uses the generated gRPC services for commands, queries, and streamed events:

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
- `ChatService.PrepareRemoteEgress` to obtain a side-effect-free, exact-request
  disclosure before a remote send.
- `ChatService.SendMessage` to enqueue a user message and selected model
  provider, carrying one-time consent when the effective route is remote.
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

The shell preserves session timestamp nanoseconds and reconciles list pages,
lifecycle RPC responses, and `session.updated` events by authoritative snapshot.
An archived row cannot be restored by omission from an active page or by an
older status-less event.

Historical tool cards and nonterminal run notices are suppressed during event
replay because persisted messages do not carry event sequence values that could
place those artifacts back into the transcript correctly. Live events committed
after the screen's startup watermark still render normally, and the durable run
outcome itself always comes from message history.

Approval cards appear from `approval.requested` and are removed on `approval.approved`, `approval.denied`, `approval.expired`, or `approval.consumed`.

`session.deleted` is terminal. `ChatScreen` closes its source, ignores stale
events, and tells `ResponsiveShell` to remove the session. A reconnect or
event replay for a deleted session is `NotFound`, not an empty history.

## Important Files

- `lib/app.dart`: loads saved client config and chooses Settings or `ResponsiveShell`.
- `lib/ui/shell/responsive_shell.dart`: TuringAgent sidebar, drawer and destination integration.
- `lib/ui/shell/shell_destination.dart`: current workspace destinations.
- `lib/features/workspace/`: backend-connected workspace pages, including
  `workspace_pages.dart` for the MCPs registry surface.
- `lib/features/settings/settings_screen.dart`: backend URL/API key form.
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
- Preserve `ResponsiveShell` as the main authenticated app surface. Add new client views as destinations or shell-integrated screens rather than replacing the root.
- Prefer the `TuringApi` and `TuringEventSource` interfaces in widgets so tests can use fakes without network access.
- Keep status descriptions scoped to the real page, RPC and consumer behavior.
  A provider descriptor or an `implemented` flag is not end-to-end evidence.
- Run `go test ./tools/docs -count=1` from the repository root after updating
  guarded statuses. Update evidence and behavioral coverage with a genuine
  capability change rather than relabeling missing evidence as pending.
