import 'package:flutter/material.dart';

import '../../models/session.dart';
import '../../networking/api_client.dart';
import '../../networking/auth_storage.dart';
import '../../networking/event_source.dart';
import '../chat/chat_screen.dart';
import '../search/search_screen.dart';
import '../settings/settings_screen.dart';

class SessionListScreen extends StatefulWidget {
  const SessionListScreen({
    super.key,
    required this.apiClient,
    required this.eventSourceFactory,
    this.authStorage,
    this.onSettingsChanged,
    this.initialBackendUrl = 'http://localhost:3000',
    this.initialApiKey = '',
    this.embedded = false,
  });

  final TuringApi apiClient;
  final TuringEventSource Function() eventSourceFactory;
  final ClientAuthStorage? authStorage;
  final VoidCallback? onSettingsChanged;
  final String initialBackendUrl;
  final String initialApiKey;
  final bool embedded;

  @override
  State<SessionListScreen> createState() => _SessionListScreenState();
}

class _SessionListScreenState extends State<SessionListScreen> {
  late Future<List<Session>> _sessionsFuture;
  bool _creating = false;

  @override
  void initState() {
    super.initState();
    _sessionsFuture = widget.apiClient.listSessions();
  }

  void _refreshSessions() {
    setState(() => _sessionsFuture = widget.apiClient.listSessions());
  }

  Future<void> _createSession() async {
    setState(() => _creating = true);
    try {
      final result = await widget.apiClient.createSession(title: 'New chat');
      if (!mounted) return;
      setState(() => _creating = false);
      await _openChat(result['sessionId'] as String);
      _refreshSessions();
    } catch (error) {
      if (!mounted) return;
      setState(() => _creating = false);
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(error.toString())));
    }
  }

  Future<void> _openChat(String sessionId) async {
    await Navigator.of(context).push(
      MaterialPageRoute(
        builder: (_) => ChatScreen(
          sessionId: sessionId,
          apiClient: widget.apiClient,
          eventSource: widget.eventSourceFactory(),
        ),
      ),
    );
  }

  Future<void> _openSearch() async {
    await Navigator.of(context).push(
      MaterialPageRoute(
        builder: (_) =>
            SearchScreen(apiClient: widget.apiClient, onOpenSession: _openChat),
      ),
    );
  }

  Future<void> _openSettings() async {
    final authStorage = widget.authStorage;
    if (authStorage == null) return;
    await Navigator.of(context).push(
      MaterialPageRoute(
        builder: (_) => SettingsScreen(
          authStorage: authStorage,
          initialBackendUrl: widget.initialBackendUrl,
          initialApiKey: widget.initialApiKey,
          onSaved: widget.onSettingsChanged,
        ),
      ),
    );
    widget.onSettingsChanged?.call();
  }

  @override
  Widget build(BuildContext context) {
    final body = _buildSessionsBody();
    final searchButton = IconButton(
      tooltip: 'Search conversations',
      icon: const Icon(Icons.search),
      onPressed: _openSearch,
    );
    final newChatButton = FloatingActionButton.extended(
      onPressed: _creating ? null : _createSession,
      icon: _creating
          ? const SizedBox.square(
              dimension: 18,
              child: CircularProgressIndicator(strokeWidth: 2),
            )
          : const Icon(Icons.add),
      label: Text(_creating ? 'Creating...' : 'New chat'),
    );

    if (widget.embedded) {
      return Stack(
        children: [
          Positioned.fill(
            child: Material(
              color: Colors.transparent,
              child: Column(
                children: [
                  Padding(
                    padding: const EdgeInsets.fromLTRB(16, 16, 8, 8),
                    child: Row(
                      children: [
                        Expanded(
                          child: Text(
                            'Sessions',
                            style: Theme.of(context).textTheme.titleLarge,
                          ),
                        ),
                        searchButton,
                      ],
                    ),
                  ),
                  Expanded(child: body),
                ],
              ),
            ),
          ),
          Positioned(right: 24, bottom: 24, child: newChatButton),
        ],
      );
    }

    return Scaffold(
      appBar: AppBar(
        title: const Text('Project Turing Sessions'),
        actions: [
          searchButton,
          if (widget.authStorage != null)
            IconButton(
              tooltip: 'Settings',
              icon: const Icon(Icons.settings),
              onPressed: _openSettings,
            ),
        ],
      ),
      body: body,
      floatingActionButton: newChatButton,
    );
  }

  Widget _buildSessionsBody() {
    return FutureBuilder<List<Session>>(
      future: _sessionsFuture,
      builder: (context, snapshot) {
        if (snapshot.connectionState != ConnectionState.done) {
          return const Center(child: CircularProgressIndicator());
        }
        if (snapshot.hasError) {
          return Center(child: Text(snapshot.error.toString()));
        }
        final sessions = snapshot.data ?? const [];
        if (sessions.isEmpty) {
          return const Center(child: Text('No sessions yet.'));
        }
        return ListView.separated(
          itemCount: sessions.length,
          separatorBuilder: (_, _) => const Divider(height: 1),
          itemBuilder: (context, index) {
            final session = sessions[index];
            return ListTile(
              leading: const Icon(Icons.chat_bubble_outline),
              title: Text(
                session.title?.isNotEmpty == true
                    ? session.title!
                    : 'Untitled chat',
              ),
              subtitle: Text(session.updatedAt.toLocal().toString()),
              onTap: () => _openChat(session.sessionId),
            );
          },
        );
      },
    );
  }
}
