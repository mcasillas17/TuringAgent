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
  StreamSubscription<TuringEvent>? _subscription;
  String _modelProvider = 'ollama';
  int? _lastSequence;

  @override
  void initState() {
    super.initState();
    _loadInitialMessages();
    _subscription = widget.eventSource
        .connect(sessionId: widget.sessionId, lastSequence: _lastSequence)
        .listen(
          _applyEvent,
          onError: _handleStreamEnded,
          onDone: _handleStreamEnded,
        );
  }

  /// The event stream is the only source of terminal `tool.call.*` events, so
  /// once it errors (gRPC disconnect, deadline, auth failure) or closes, any
  /// card still in [ToolCallStatus.running] can never resolve. Left alone it
  /// would spin forever and tell the user a tool is still executing. Resolve
  /// those cards instead; already-terminal cards are untouched.
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

  Future<void> _loadInitialMessages() async {
    final messages = await widget.apiClient.listMessages(
      sessionId: widget.sessionId,
    );
    if (!mounted || messages.isEmpty) return;
    // Seed history non-destructively: prepend it above any live entries (tool
    // cards, streaming bubbles) that the event stream may have already created
    // while `listMessages` was in flight. A destructive clear+addAll here would
    // wipe those live entries from the UI, leak their notifiers, and orphan the
    // correlation maps (`_toolEntries` / `_assistantEntries`) so later terminal
    // events would mutate cards no widget listens to.
    setState(() {
      _messages.insertAll(0, messages.map(_MessageEntry.fromMessage));
    });
    _scrollToBottom();
  }

  void _applyEvent(TuringEvent event) {
    _lastSequence = event.sequence;
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
    final toolCallId = _asString(event.payload['toolCallId']);
    if (toolCallId == null || toolCallId.isEmpty) return;
    // The frozen proto contract carries `toolName` as a non-nullable scalar, so
    // an unset value arrives as '' (not null) on the live stream. Treat empty as
    // missing so the card never renders a blank label.
    final rawToolName = _asString(event.payload['toolName']);
    final error = _asString(event.payload['error']);

    var entry = _toolEntries[toolCallId];
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
      _toolEntries[toolCallId] = entry;
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
          child: ValueListenableBuilder<String>(
            valueListenable: entry.content,
            builder: (context, content, _) {
              return Text(content, style: TextStyle(color: foreground));
            },
          ),
        ),
      ),
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
