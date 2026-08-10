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
        .listen(_applyEvent);
  }

  Future<void> _loadInitialMessages() async {
    final messages = await widget.apiClient.listMessages(
      sessionId: widget.sessionId,
    );
    if (!mounted || messages.isEmpty) return;
    setState(() {
      _messages
        ..clear()
        ..addAll(messages.map(_MessageEntry.fromMessage));
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
    final toolCallId = event.payload['toolCallId'] as String?;
    if (toolCallId == null) return;
    final toolName = event.payload['toolName'] as String? ?? 'tool';
    final error = event.payload['error'] as String?;

    var entry = _toolEntries[toolCallId];
    if (entry == null) {
      // Create on 'started', or defensively on a terminal event that arrives
      // without a prior start (e.g. an event-replay gap).
      entry = _ToolCallEntry(
        toolCallId: toolCallId,
        toolName: toolName,
        serverName: event.payload['serverName'] as String?,
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
    // Update the existing card in place; the ValueNotifier drives the rebuild.
    if (error != null) entry.error = error;
    entry.status.value = status;
    _scrollToBottom();
  }

  void _applyMessageDelta(TuringEvent event) {
    final messageId =
        event.payload['messageId'] as String? ?? 'active_assistant';
    final delta = event.payload['delta'] as String? ?? '';
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
    final approvalId = event.payload['approvalId'] as String?;
    final toolName = event.payload['toolName'] as String?;
    if (approvalId == null || toolName == null) return;
    setState(() {
      _approvals.removeWhere((approval) => approval.approvalId == approvalId);
      _approvals.add(
        _PendingApproval(
          approvalId: approvalId,
          toolName: toolName,
          argsSummary: event.payload['argsSummary'] as String? ?? '',
        ),
      );
    });
  }

  void _clearApproval(TuringEvent event) {
    final approvalId = event.payload['approvalId'] as String?;
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
              itemBuilder: (context, index) =>
                  _ChatMessageTile(entry: _messages[index]),
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
  const _ChatMessageTile({required this.entry});

  final _ChatEntry entry;

  @override
  Widget build(BuildContext context) {
    switch (entry) {
      case _MessageEntry message:
        return _MessageBubble(entry: message);
      case _ToolCallEntry tool:
        return ValueListenableBuilder<ToolCallStatus>(
          valueListenable: tool.status,
          builder: (context, status, _) => ToolCallCard(
            toolName: tool.toolName,
            serverName: tool.serverName,
            status: status,
            error: tool.error,
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

class _ToolCallEntry extends _ChatEntry {
  _ToolCallEntry({
    required this.toolCallId,
    required this.toolName,
    required ToolCallStatus status,
    this.serverName,
    this.error,
  }) : status = ValueNotifier(status);

  final String toolCallId;
  final String toolName;
  final String? serverName;
  String? error;
  final ValueNotifier<ToolCallStatus> status;

  @override
  void dispose() => status.dispose();
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
