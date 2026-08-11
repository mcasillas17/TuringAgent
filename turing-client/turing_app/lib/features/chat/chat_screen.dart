import 'dart:async';

import 'package:flutter/material.dart';

import '../../models/message.dart';
import '../../models/turing_event.dart';
import '../../networking/api_client.dart';
import '../../networking/event_source.dart';
import '../approvals/approval_card.dart';
import 'model_provider_selector.dart';
import 'tool_call_card.dart';

class ChatScreen extends StatefulWidget {
  const ChatScreen({
    super.key,
    required this.sessionId,
    required this.apiClient,
    required this.eventSource,
  });

  final String sessionId;
  final TuringApi apiClient;
  final TuringEventSource eventSource;

  @override
  State<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends State<ChatScreen> {
  final _controller = TextEditingController();
  final _scrollController = ScrollController();
  final List<_ChatEntry> _messages = [];
  final Map<String, _MessageEntry> _assistantEntries = {};
  final Map<String, _ToolCallEntry> _toolEntries = {};
  final List<_PendingApproval> _approvals = [];

  /// Ids of history messages whose text is already complete on screen. The
  /// stream replays the deltas that produced them, and those must not be
  /// applied a second time.
  final Set<String> _completedHistoryMessageIds = {};

  /// Sequence of the newest event already persisted when this screen opened.
  /// The subscription replays the log from its start, so every event at or
  /// below this sequence is a REPLAY of business that finished before the
  /// screen existed — see [_applyToolCall]. Null when the tail could not be
  /// read (fresh session, or the call failed), which suppresses nothing.
  int? _replayWatermarkSequence;
  StreamSubscription<TuringEvent>? _subscription;
  String _modelProvider = 'ollama';
  bool _streamEnded = false;
  bool _historyLoadFailed = false;

  @override
  void initState() {
    super.initState();
    unawaited(_start());
  }

  /// History is seeded BEFORE the subscription opens, never concurrently with
  /// it. `SubscribeSessionEvents` replays the session's persisted events from
  /// the requested sequence and only then goes live, so every `message.delta`
  /// of every earlier turn is re-delivered. Applying those while `listMessages`
  /// is still in flight would render a second copy of the conversation with no
  /// way to tell which bubbles history already covers.
  ///
  /// The load is awaited but never allowed to fail the startup: `listMessages`
  /// hits a backend that routinely is not up (gRPC unavailable, stale token,
  /// deadline). Letting that propagate would skip `connect()` entirely and
  /// leave a screen that receives no deltas, no tool cards and — because the
  /// drop notice is raised from the subscription — no signal at all. Degrade to
  /// "no history" instead, and say so.
  Future<void> _start() async {
    // Snapshot the persisted event boundary before history starts loading.
    // Events committed during that RPC remain above the cursor and replay as
    // live after history is seeded.
    await _loadReplayWatermark();
    if (!mounted) return;
    try {
      await _loadInitialMessages();
    } catch (_) {
      // The dedup sets stay empty, so replayed deltas may re-render text the
      // server already persisted. A duplicated transcript beats a silent one.
      if (mounted) setState(() => _historyLoadFailed = true);
    }
    if (!mounted) return;
    _subscription = widget.eventSource
        .connect(
          sessionId: widget.sessionId,
          lastSequence: _replayWatermarkSequence,
        )
        .listen(
          _applyEvent,
          onError: _handleStreamEnded,
          onDone: _handleStreamEnded,
        );
  }

  static const _streamEndedNotice =
      'Connection to the session lost. Reopen the session to continue.';

  static const _historyFailedNotice =
      'Earlier messages could not be loaded. This session is live from here on.';

  /// The event stream is the only source of terminal `tool.call.*` events, so
  /// once it errors (gRPC disconnect, deadline, auth failure) or closes, any
  /// card still in [ToolCallStatus.running] can never resolve. Left alone it
  /// would spin forever and tell the user a tool is still executing. Resolve
  /// those cards instead; already-terminal cards are untouched.
  ///
  /// Resolving cards is not enough on its own: usually no tool call is in
  /// flight when the stream drops, and [TuringEventSource] never reconnects, so
  /// the screen would go permanently silent with no user-visible signal. Raise
  /// a persistent notice too.
  void _handleStreamEnded([Object? error, StackTrace? stackTrace]) {
    if (!mounted) return;
    for (final entry in _toolEntries.values) {
      final current = entry.state.value;
      if (current.status != ToolCallStatus.running) continue;
      entry.state.value = (
        toolName: current.toolName,
        status: ToolCallStatus.failed,
        error: 'connection lost',
        serverName: current.serverName,
      );
    }
    setState(() => _streamEnded = true);
  }

  /// Payloads arrive as a `Map<String, dynamic>` decoded from a proto Struct,
  /// so a producer bug can put any type behind a contract key. An `as String?`
  /// cast would throw a `TypeError` out of the listener and take the whole
  /// subscription (and therefore every later event) down with it.
  static String? _asString(Object? value) => value is String ? value : null;

  /// Fallback label for a card whose events have not yet carried a usable
  /// `toolName`. Doubles as the "still unresolved" marker so a later event can
  /// heal the card (see [_applyToolCall]).
  static const _placeholderToolName = 'tool';

  /// Reads the already-persisted event page and its authoritative latest
  /// sequence so [_applyToolCall] can tell a replayed event from a live one.
  /// Sequence is exact and free of clock skew; message timestamps are not
  /// usable for this — the backend stamps a turn's rows at enqueue time and
  /// never restamps them, so a finished turn's messages are stamped BEFORE its
  /// own tool events.
  ///
  /// Fails open (watermark left unset, nothing suppressed) on any error: this
  /// screen must survive an unreachable backend, and re-rendering a stale card
  /// is a far smaller harm than silently hiding every live tool call.
  Future<void> _loadReplayWatermark() async {
    final TuringEventPage page;
    try {
      page = await widget.apiClient.listEvents(sessionId: widget.sessionId);
    } catch (_) {
      return;
    }
    if (!mounted) return;
    // `latestSequence` covers persisted rows beyond the 500-event response
    // page. Taking the max also keeps alternate API implementations honest.
    var watermark = page.latestSequence;
    for (final event in page.events) {
      if (event.sequence > watermark) watermark = event.sequence;
    }
    _replayWatermarkSequence = watermark;
  }

  Future<void> _loadInitialMessages() async {
    final messages = await widget.apiClient.listMessages(
      sessionId: widget.sessionId,
    );
    if (!mounted || messages.isEmpty) return;
    final entries = <_MessageEntry>[];
    for (final message in messages) {
      final entry = _MessageEntry.fromMessage(message);
      entries.add(entry);
      if (!entry.isUser && message.content.isEmpty) {
        // An assistant row with no content is a turn that is still streaming:
        // the row is inserted empty when the job is created. Adopt it as the
        // live bubble so replayed and live deltas land IN it rather than
        // opening a duplicate bubble underneath.
        _assistantEntries[message.messageId] = entry;
      } else {
        _completedHistoryMessageIds.add(message.messageId);
      }
    }
    // Seed history non-destructively: prepend it above any live entries. A
    // destructive clear+addAll here would leak their notifiers and orphan the
    // correlation maps (`_toolEntries` / `_assistantEntries`) so later terminal
    // events would mutate cards no widget listens to.
    setState(() => _messages.insertAll(0, entries));
    _scrollToBottom();
  }

  void _applyEvent(TuringEvent event) {
    // `dispose` cancels the subscription, but cancellation of a gRPC-backed
    // stream is asynchronous: an event already scheduled for delivery can still
    // land here after the State is gone, and every handler below calls
    // `setState`. Guard once at the entry point — it covers all four of them,
    // and mirrors [_handleStreamEnded].
    if (!mounted) return;
    // Events are arriving, so any earlier drop notice is stale: an error does
    // not cancel the subscription (`cancelOnError` defaults to false), and the
    // stream can keep delivering after one.
    if (_streamEnded) setState(() => _streamEnded = false);
    switch (event.type) {
      case 'message.delta':
        _applyMessageDelta(event);
        break;
      case 'approval.requested':
        _addApproval(event);
        break;
      case 'approval.approved':
      case 'approval.denied':
      case 'approval.expired':
      case 'approval.consumed':
        _clearApproval(event);
        break;
      case 'tool.call.started':
        _applyToolCall(event, ToolCallStatus.running);
        break;
      case 'tool.call.completed':
        _applyToolCall(event, ToolCallStatus.completed);
        break;
      case 'tool.call.failed':
        _applyToolCall(event, ToolCallStatus.failed);
        break;
      case 'tool.call.denied':
        _applyToolCall(event, ToolCallStatus.denied);
        break;
    }
  }

  void _applyToolCall(TuringEvent event, ToolCallStatus status) {
    // The subscription replays the whole persisted log (see [_start]), so every
    // tool call that happened before this screen opened is re-delivered. Deltas
    // are de-duplicated by messageId; tool events have no equivalent identity
    // in history, so they are filtered by the event sequence captured in
    // [_loadReplayWatermark] instead. Recreating a card for one of them would
    // append it BELOW every later message (history is prepended, cards are
    // appended) and — because the create path seals live bubbles via
    // `_assistantEntries.clear()` — would orphan an adopted, still-streaming
    // assistant row into an empty pill with a duplicate bubble under it.
    //
    // Consequence, accepted deliberately: tool cards are not reconstructed for
    // calls the log already holds. There is no ordering key that could place
    // them correctly anyway — `Message` carries no event sequence to interleave
    // against — so the past renders as text only.
    final watermark = _replayWatermarkSequence;
    if (watermark != null && event.sequence <= watermark) return;
    final toolCallId = _asString(event.payload['toolCallId']);
    if (toolCallId == null || toolCallId.isEmpty) return;
    // The frozen proto contract carries `toolName` as a non-nullable scalar, so
    // an unset value arrives as '' (not null) on the live stream. Treat empty as
    // missing so the card never renders a blank label.
    final rawToolName = _asString(event.payload['toolName']);
    final error = _asString(event.payload['error']);

    // The contract only guarantees `toolCallId` correlates started -> terminal
    // WITHIN a run, and a runtime that mints ids itself (call_0, call_1, ...
    // when the model supplies none) restarts the numbering every turn. Scope
    // the correlation key by the event's own runId so turn 2's `call_1` cannot
    // hijack turn 1's card — the payload contract is untouched, and
    // `toolCallId` stays the entry's own field.
    final runId = event.runId;
    final key = (runId == null || runId.isEmpty)
        ? toolCallId
        : '$runId:$toolCallId';
    var entry = _toolEntries[key];
    if (entry == null) {
      // Create on 'started', or defensively on a terminal event that arrives
      // without a prior start (e.g. an event-replay gap).
      entry = _ToolCallEntry(
        toolCallId: toolCallId,
        toolName: (rawToolName == null || rawToolName.isEmpty)
            ? _placeholderToolName
            : rawToolName,
        serverName: _asString(event.payload['serverName']),
        status: status,
        error: error,
      );
      _toolEntries[key] = entry;
      // Seal any in-progress assistant bubbles: the runtime reuses one
      // assistantMessageId across the whole turn, so text streamed AFTER this
      // tool call must start a fresh bubble below the card rather than append
      // to the pre-tool bubble above it.
      _assistantEntries.clear();
      setState(() => _messages.add(entry!));
      _scrollToBottom();
      return;
    }
    final current = entry.state.value;
    // Only `tool.call.started` is guaranteed to carry `serverName`, and it can
    // arrive after a terminal event (replay gap). Adopt the metadata whenever
    // the card is still missing it, even on an event that is otherwise ignored.
    final incomingServer = _asString(event.payload['serverName']);
    final serverName =
        (current.serverName == null || current.serverName!.isEmpty)
        ? incomingServer
        : current.serverName;
    // Same self-healing for the name: if the first event the client saw carried
    // a malformed/empty `toolName` the card is stuck on the placeholder, so take
    // the first real name any later event supplies. A good name is never
    // downgraded back to the placeholder by a later malformed event.
    final toolName =
        (current.toolName == _placeholderToolName &&
            rawToolName != null &&
            rawToolName.isNotEmpty)
        ? rawToolName
        : current.toolName;
    // Terminal states are final: ignore a stale/duplicate 'started' replayed
    // (at-least-once redelivery, reconnect, replay overlap) after the call has
    // already resolved, so a finished card never regresses to a spinner.
    if (status == ToolCallStatus.running &&
        current.status != ToolCallStatus.running) {
      entry.state.value = (
        toolName: toolName,
        status: current.status,
        error: current.error,
        serverName: serverName,
      );
      return;
    }
    // Update the existing card in place with a single write: everything the
    // card renders lives in one notifier, so a changed error (or a newly
    // adopted serverName) still rebuilds even when `status` is unchanged.
    entry.state.value = (
      toolName: toolName,
      status: status,
      error: error ?? current.error,
      serverName: serverName,
    );
    _scrollToBottom();
  }

  void _applyMessageDelta(TuringEvent event) {
    final messageId =
        _asString(event.payload['messageId']) ?? 'active_assistant';
    // The replayed deltas that produced an already-complete history message
    // would otherwise render its text a second time below the history block.
    if (_completedHistoryMessageIds.contains(messageId)) return;
    final delta = _asString(event.payload['delta']) ?? '';
    var entry = _assistantEntries[messageId];
    if (entry == null) {
      entry = _MessageEntry.assistant(messageId: messageId, content: '');
      _assistantEntries[messageId] = entry;
      setState(() => _messages.add(entry!));
    }
    entry.content.value = '${entry.content.value}$delta';
    _scrollToBottom();
  }

  void _addApproval(TuringEvent event) {
    final approvalId = _asString(event.payload['approvalId']);
    final toolName = _asString(event.payload['toolName']);
    if (approvalId == null || toolName == null) return;
    setState(() {
      _approvals.removeWhere((approval) => approval.approvalId == approvalId);
      _approvals.add(
        _PendingApproval(
          approvalId: approvalId,
          toolName: toolName,
          argsSummary: _asString(event.payload['argsSummary']) ?? '',
        ),
      );
    });
  }

  void _clearApproval(TuringEvent event) {
    final approvalId = _asString(event.payload['approvalId']);
    if (approvalId == null) return;
    setState(
      () => _approvals.removeWhere(
        (approval) => approval.approvalId == approvalId,
      ),
    );
  }

  Future<void> _sendMessage() async {
    final text = _controller.text.trim();
    if (text.isEmpty) return;
    setState(
      () => _messages.add(
        _MessageEntry.user(
          messageId: 'local_${DateTime.now().microsecondsSinceEpoch}',
          content: text,
        ),
      ),
    );
    _controller.clear();
    _scrollToBottom();
    await widget.apiClient.sendMessage(
      sessionId: widget.sessionId,
      content: text,
      modelProvider: _modelProvider,
    );
  }

  void _scrollToBottom() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scrollController.hasClients) return;
      _scrollController.animateTo(
        _scrollController.position.maxScrollExtent,
        duration: const Duration(milliseconds: 160),
        curve: Curves.easeOut,
      );
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Project Turing')),
      body: Column(
        children: [
          Expanded(
            child: ListView.builder(
              controller: _scrollController,
              padding: const EdgeInsets.all(12),
              itemCount: _messages.length,
              // Key on the entry itself: `_loadInitialMessages` prepends
              // history with `insertAll(0, ...)`, shifting every live entry's
              // index. Without a key Flutter re-associates Elements by
              // position, tearing down and re-subscribing each
              // ValueListenableBuilder and resetting a running card's spinner.
              itemBuilder: (context, index) => _ChatMessageTile(
                key: ObjectKey(_messages[index]),
                entry: _messages[index],
              ),
            ),
          ),
          if (_historyLoadFailed) _SessionNotice(message: _historyFailedNotice),
          if (_streamEnded) _SessionNotice(message: _streamEndedNotice),
          for (final approval in _approvals)
            ApprovalCard(
              toolName: approval.toolName,
              argsSummary: approval.argsSummary,
              onApprove: () => _approve(approval),
              onDeny: () => _deny(approval),
            ),
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 4, 12, 0),
            child: Row(
              children: [
                Text(
                  'Model provider',
                  style: Theme.of(context).textTheme.labelLarge,
                ),
                const SizedBox(width: 12),
                ModelProviderSelector(
                  value: _modelProvider,
                  onChanged: (value) => setState(() => _modelProvider = value),
                ),
              ],
            ),
          ),
          SafeArea(
            top: false,
            child: Padding(
              padding: const EdgeInsets.fromLTRB(12, 4, 12, 12),
              child: Row(
                children: [
                  Expanded(
                    child: TextField(
                      controller: _controller,
                      onSubmitted: (_) => _sendMessage(),
                      decoration: const InputDecoration(
                        hintText: 'Ask Turing...',
                      ),
                    ),
                  ),
                  IconButton(
                    tooltip: 'Send',
                    icon: const Icon(Icons.send),
                    onPressed: _sendMessage,
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  Future<void> _approve(_PendingApproval approval) async {
    await widget.apiClient.approveApproval(approval.approvalId);
    if (!mounted) return;
    setState(
      () => _approvals.removeWhere(
        (item) => item.approvalId == approval.approvalId,
      ),
    );
  }

  Future<void> _deny(_PendingApproval approval) async {
    await widget.apiClient.denyApproval(approval.approvalId);
    if (!mounted) return;
    setState(
      () => _approvals.removeWhere(
        (item) => item.approvalId == approval.approvalId,
      ),
    );
  }

  @override
  void dispose() {
    _subscription?.cancel();
    widget.eventSource.close();
    for (final entry in _messages) {
      entry.dispose();
    }
    _controller.dispose();
    _scrollController.dispose();
    super.dispose();
  }
}

class _ChatMessageTile extends StatelessWidget {
  const _ChatMessageTile({super.key, required this.entry});

  final _ChatEntry entry;

  @override
  Widget build(BuildContext context) {
    switch (entry) {
      case _MessageEntry message:
        return _MessageBubble(entry: message);
      case _ToolCallEntry tool:
        return ValueListenableBuilder<_ToolCallState>(
          valueListenable: tool.state,
          builder: (context, state, _) => ToolCallCard(
            toolName: state.toolName,
            serverName: state.serverName,
            status: state.status,
            error: state.error,
          ),
        );
    }
  }
}

/// Persistent, screen-reader-announced banner for a session whose event stream
/// is gone. Presentational only: the parent owns when it is shown.
class _SessionNotice extends StatelessWidget {
  const _SessionNotice({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return Semantics(
      container: true,
      liveRegion: true,
      child: Container(
        width: double.infinity,
        color: colorScheme.errorContainer,
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
        child: Row(
          children: [
            Icon(
              Icons.cloud_off,
              size: 18,
              color: colorScheme.onErrorContainer,
            ),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                message,
                style: TextStyle(color: colorScheme.onErrorContainer),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _MessageBubble extends StatelessWidget {
  const _MessageBubble({required this.entry});

  final _MessageEntry entry;

  @override
  Widget build(BuildContext context) {
    final alignment = entry.isUser
        ? Alignment.centerRight
        : Alignment.centerLeft;
    final colorScheme = Theme.of(context).colorScheme;
    final background = entry.isUser
        ? colorScheme.primaryContainer
        : colorScheme.surfaceContainerHighest;
    final foreground = entry.isUser
        ? colorScheme.onPrimaryContainer
        : colorScheme.onSurface;
    // The builder wraps the chrome, not just the text: an entry can legitimately
    // hold no content — an assistant row adopted from a mid-run reopen before
    // its first delta, or one sealed by a tool call that arrived before any text
    // — and a decorated Container with an empty Text paints as a stray pill that
    // screen readers announce as an empty node. The entry (and its ObjectKey
    // identity) is deliberately left in place so late deltas can still fill it.
    return ValueListenableBuilder<String>(
      valueListenable: entry.content,
      builder: (context, content, _) {
        if (content.isEmpty) return const SizedBox.shrink();
        return Align(
          alignment: alignment,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 640),
            child: Container(
              margin: const EdgeInsets.symmetric(vertical: 4),
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
              decoration: BoxDecoration(
                color: background,
                borderRadius: BorderRadius.circular(8),
              ),
              child: Text(content, style: TextStyle(color: foreground)),
            ),
          ),
        );
      },
    );
  }
}

sealed class _ChatEntry {
  void dispose();
}

class _MessageEntry extends _ChatEntry {
  _MessageEntry({
    required this.messageId,
    required this.isUser,
    required String content,
  }) : content = ValueNotifier(content);

  factory _MessageEntry.user({
    required String messageId,
    required String content,
  }) {
    return _MessageEntry(messageId: messageId, isUser: true, content: content);
  }

  factory _MessageEntry.assistant({
    required String messageId,
    required String content,
  }) {
    return _MessageEntry(messageId: messageId, isUser: false, content: content);
  }

  factory _MessageEntry.fromMessage(Message message) {
    return _MessageEntry(
      messageId: message.messageId,
      isUser: message.role == 'user',
      content: message.content,
    );
  }

  final String messageId;
  final bool isUser;
  final ValueNotifier<String> content;

  @override
  void dispose() => content.dispose();
}

/// Everything a [ToolCallCard] renders that can change after creation, held in
/// ONE notifier. Records have value equality, so any changed member (a
/// corrected `error`, a late `serverName` or `toolName`) notifies even when
/// `status` is the same — a plain field read during build would silently go
/// stale instead.
typedef _ToolCallState = ({
  String toolName,
  ToolCallStatus status,
  String? error,
  String? serverName,
});

class _ToolCallEntry extends _ChatEntry {
  _ToolCallEntry({
    required this.toolCallId,
    required String toolName,
    required ToolCallStatus status,
    String? serverName,
    String? error,
  }) : state = ValueNotifier((
         toolName: toolName,
         status: status,
         error: error,
         serverName: serverName,
       ));

  final String toolCallId;
  final ValueNotifier<_ToolCallState> state;

  @override
  void dispose() => state.dispose();
}

class _PendingApproval {
  const _PendingApproval({
    required this.approvalId,
    required this.toolName,
    required this.argsSummary,
  });

  final String approvalId;
  final String toolName;
  final String argsSummary;
}
