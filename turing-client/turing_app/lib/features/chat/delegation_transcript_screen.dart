import 'package:flutter/material.dart';
import 'package:flutter_markdown/flutter_markdown.dart';

import '../../models/delegation.dart';
import '../../models/message.dart';
import '../../networking/api_client.dart';
import '../../utils/content_presence.dart';

/// What a specialist did on a task, read-only: the task as Turing asked it,
/// then the specialist's own conversation. The framed brief the specialist
/// received is left out; its task is the header.
class DelegationTranscriptScreen extends StatefulWidget {
  const DelegationTranscriptScreen({
    super.key,
    required this.delegation,
    required this.apiClient,
  });

  final Delegation delegation;
  final TuringApi apiClient;

  @override
  State<DelegationTranscriptScreen> createState() =>
      _DelegationTranscriptScreenState();
}

class _DelegationTranscriptScreenState
    extends State<DelegationTranscriptScreen> {
  List<Message>? _messages;
  bool _failed = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  void _reload() {
    setState(() {
      _messages = null;
      _failed = false;
    });
    _load();
  }

  Future<void> _load() async {
    try {
      final messages = await widget.apiClient.listMessages(
        sessionId: widget.delegation.childSessionId,
      );
      if (mounted) setState(() => _messages = messages);
    } on Exception {
      if (mounted) setState(() => _failed = true);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final delegation = widget.delegation;
    final messages = _messages
        ?.where(
          (message) =>
              message.contentType != 'delegation_brief' &&
              hasDisplayableContent(message.content),
        )
        .toList();
    return Scaffold(
      appBar: AppBar(
        title: Text(delegation.specialist),
        actions: [
          IconButton(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: _messages == null && !_failed ? null : _reload,
          ),
        ],
      ),
      body: ListView(
        padding: const EdgeInsets.all(20),
        children: [
          Text('Task', style: theme.textTheme.labelMedium),
          const SizedBox(height: 4),
          Text(
            delegation.task.isEmpty ? 'Not recorded' : delegation.task,
            style: theme.textTheme.titleMedium,
          ),
          const SizedBox(height: 4),
          Text(delegation.summary, style: theme.textTheme.bodySmall),
          const Divider(height: 28),
          if (_failed)
            const Text(
              "This task's work could not be loaded. Refresh to retry.",
            )
          else if (messages == null)
            const Center(child: CircularProgressIndicator())
          else if (messages.isEmpty)
            const Text('Nothing to show yet.')
          else
            for (final message in messages)
              Padding(
                padding: const EdgeInsets.only(bottom: 14),
                child: MarkdownBody(data: message.content, selectable: true),
              ),
        ],
      ),
    );
  }
}
