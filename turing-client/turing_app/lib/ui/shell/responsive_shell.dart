import 'dart:async';

import 'package:flutter/material.dart';

import '../../constants/app_colors.dart';
import '../../features/chat/chat_screen.dart';
import '../../features/search/search_screen.dart';
import '../../features/settings/settings_screen.dart';
import '../../logic/theme_logic.dart';
import '../../models/session.dart';
import '../../networking/api_client.dart';
import '../../networking/auth_storage.dart';
import '../../networking/event_source.dart';

/// The app is one surface: a conversation, with a quiet rail of past ones
/// beside it.
///
/// The previous shell was a five-item nav rail — Chat, Devices, Stats,
/// Integrations, Settings — where three destinations were placeholders for
/// features that do not exist and that `docs/VISION.md` explicitly refuses
/// (IoT among them). Opening a conversation pushed a new route you then had to
/// back out of, so switching threads meant navigating twice.
///
/// Now selecting a conversation swaps it in place. Nothing is pushed, so there
/// is nothing to back out of, and every destination leads somewhere real.
class ResponsiveShell extends StatefulWidget {
  const ResponsiveShell({
    super.key,
    required this.apiClient,
    required this.eventSourceFactory,
    this.authStorage,
    this.initialBackendUrl = 'http://localhost:3000',
    this.initialApiKey = '',
    this.onSettingsChanged,
  });

  final TuringApi apiClient;
  final TuringEventSource Function() eventSourceFactory;
  final ClientAuthStorage? authStorage;
  final String initialBackendUrl;
  final String initialApiKey;
  final VoidCallback? onSettingsChanged;

  @override
  State<ResponsiveShell> createState() => _ResponsiveShellState();
}

class _ResponsiveShellState extends State<ResponsiveShell> {
  static const double _sidebarWidth = 268;

  late Future<List<Session>> _sessionsFuture;
  String? _activeSessionId;
  String _modelProvider = 'ollama';
  bool _creating = false;
  final Set<String> _deleting = {};

  @override
  void initState() {
    super.initState();
    _sessionsFuture = widget.apiClient.listSessions();
    unawaited(_loadModelProvider());
  }

  /// Chosen once in Settings; re-read whenever settings change.
  Future<void> _loadModelProvider() async {
    final stored = await widget.authStorage?.readModelProvider();
    if (!mounted || stored == null || stored.isEmpty) return;
    setState(() => _modelProvider = stored);
  }

  void _refreshSessions() {
    // Braces, not an arrow: an arrow body returns the assigned Future and
    // setState rejects that.
    setState(() {
      _sessionsFuture = widget.apiClient.listSessions();
    });
  }

  Future<void> _newConversation() async {
    if (_creating) return;
    setState(() => _creating = true);
    try {
      final result = await widget.apiClient.createSession(title: 'New chat');
      if (!mounted) return;
      setState(() {
        _activeSessionId = result['sessionId'] as String?;
        _creating = false;
      });
      _refreshSessions();
    } catch (error) {
      if (!mounted) return;
      setState(() => _creating = false);
      _toast('Could not start a new chat: $error');
    }
  }

  Future<void> _deleteConversation(Session session) async {
    if (!_deleting.add(session.sessionId)) return;
    try {
      final title = session.title?.isNotEmpty == true
          ? session.title!
          : 'Untitled chat';
      final confirmed = await showDialog<bool>(
        context: context,
        builder: (dialogContext) => AlertDialog(
          title: Text('Delete "$title"?'),
          content: const Text(
            'This permanently removes the conversation, its messages and its '
            'run history, and it will no longer appear in search. Files '
            'written into the sandbox are not removed. This cannot be undone.',
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(dialogContext).pop(false),
              child: const Text('Cancel'),
            ),
            TextButton(
              onPressed: () => Navigator.of(dialogContext).pop(true),
              child: const Text('Delete'),
            ),
          ],
        ),
      );
      if (confirmed != true) return;
      await widget.apiClient.deleteSession(sessionId: session.sessionId);
      if (!mounted) return;
      if (_activeSessionId == session.sessionId) {
        setState(() => _activeSessionId = null);
      }
      _refreshSessions();
    } catch (error) {
      if (!mounted) return;
      _toast(
        _isRunInProgress(error)
            ? 'That chat has a run in progress. Wait for it to finish, then try again.'
            : 'Could not delete this chat: $error',
      );
    } finally {
      _deleting.remove(session.sessionId);
    }
  }

  static bool _isRunInProgress(Object error) =>
      error.toString().toLowerCase().contains('run in progress');

  void _toast(String message) => ScaffoldMessenger.of(
    context,
  ).showSnackBar(SnackBar(content: Text(message)));

  Future<void> _openSearch() async {
    await Navigator.of(context).push(
      MaterialPageRoute(
        builder: (_) => SearchScreen(
          apiClient: widget.apiClient,
          onOpenSession: (sessionId) async {
            if (mounted) setState(() => _activeSessionId = sessionId);
          },
        ),
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
    unawaited(_loadModelProvider());
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return Scaffold(
      backgroundColor: palette.background,
      body: Row(
        children: [
          _Sidebar(
            width: _sidebarWidth,
            palette: palette,
            sessionsFuture: _sessionsFuture,
            activeSessionId: _activeSessionId,
            creating: _creating,
            onNewConversation: _newConversation,
            onSelect: (id) => setState(() => _activeSessionId = id),
            onDelete: _deleteConversation,
            onSearch: _openSearch,
            onSettings: widget.authStorage == null ? null : _openSettings,
          ),
          VerticalDivider(width: 1, color: palette.border),
          Expanded(child: _conversation(palette)),
        ],
      ),
    );
  }

  Widget _conversation(AppPalette palette) {
    final sessionId = _activeSessionId;
    if (sessionId == null) {
      return _EmptyState(
        palette: palette,
        creating: _creating,
        onNewConversation: _newConversation,
      );
    }
    return ChatScreen(
      // Keyed by session so switching conversations rebuilds the chat state
      // instead of leaking the previous transcript into the new one.
      key: ValueKey(sessionId),
      sessionId: sessionId,
      apiClient: widget.apiClient,
      eventSource: widget.eventSourceFactory(),
      embedded: true,
      modelProvider: _modelProvider,
    );
  }
}

class _Sidebar extends StatelessWidget {
  const _Sidebar({
    required this.width,
    required this.palette,
    required this.sessionsFuture,
    required this.activeSessionId,
    required this.creating,
    required this.onNewConversation,
    required this.onSelect,
    required this.onDelete,
    required this.onSearch,
    required this.onSettings,
  });

  final double width;
  final AppPalette palette;
  final Future<List<Session>> sessionsFuture;
  final String? activeSessionId;
  final bool creating;
  final VoidCallback onNewConversation;
  final ValueChanged<String> onSelect;
  final ValueChanged<Session> onDelete;
  final VoidCallback onSearch;
  final VoidCallback? onSettings;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: width,
      color: palette.surface,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 18, 16, 10),
            child: Row(
              children: [
                Icon(Icons.auto_awesome, size: 18, color: AppColors.brand),
                const SizedBox(width: 9),
                Text(
                  'Turing',
                  style: TextStyle(
                    fontSize: 15,
                    fontWeight: FontWeight.w700,
                    letterSpacing: 0.2,
                    color: palette.text,
                  ),
                ),
                const Spacer(),
                _IconAction(
                  icon: Icons.search,
                  tooltip: 'Search conversations',
                  onPressed: onSearch,
                  palette: palette,
                ),
              ],
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
            child: FilledButton.icon(
              onPressed: creating ? null : onNewConversation,
              icon: creating
                  ? const SizedBox.square(
                      dimension: 15,
                      child: CircularProgressIndicator(
                        strokeWidth: 2,
                        color: Colors.white,
                      ),
                    )
                  : const Icon(Icons.add, size: 18),
              label: Text(creating ? 'Starting...' : 'New chat'),
            ),
          ),
          Expanded(
            child: FutureBuilder<List<Session>>(
              future: sessionsFuture,
              builder: (context, snapshot) {
                if (snapshot.connectionState != ConnectionState.done) {
                  return const Center(
                    child: SizedBox.square(
                      dimension: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    ),
                  );
                }
                final sessions = snapshot.data ?? const <Session>[];
                if (sessions.isEmpty) {
                  return Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 20),
                    child: Center(
                      child: Text(
                        'No conversations yet.',
                        textAlign: TextAlign.center,
                        style: TextStyle(
                          fontSize: 13,
                          color: palette.textMuted,
                        ),
                      ),
                    ),
                  );
                }
                return ListView.builder(
                  padding: const EdgeInsets.symmetric(horizontal: 8),
                  itemCount: sessions.length,
                  itemBuilder: (context, index) {
                    final session = sessions[index];
                    return _SessionTile(
                      session: session,
                      palette: palette,
                      selected: session.sessionId == activeSessionId,
                      onTap: () => onSelect(session.sessionId),
                      onDelete: () => onDelete(session),
                    );
                  },
                );
              },
            ),
          ),
          Divider(height: 1, color: palette.border),
          _SidebarFooter(palette: palette, onSettings: onSettings),
        ],
      ),
    );
  }
}

class _SessionTile extends StatefulWidget {
  const _SessionTile({
    required this.session,
    required this.palette,
    required this.selected,
    required this.onTap,
    required this.onDelete,
  });

  final Session session;
  final AppPalette palette;
  final bool selected;
  final VoidCallback onTap;
  final VoidCallback onDelete;

  @override
  State<_SessionTile> createState() => _SessionTileState();
}

class _SessionTileState extends State<_SessionTile> {
  bool _hovered = false;

  @override
  Widget build(BuildContext context) {
    final palette = widget.palette;
    final title = widget.session.title?.isNotEmpty == true
        ? widget.session.title!
        : 'Untitled chat';
    return Padding(
      padding: const EdgeInsets.only(bottom: 2),
      child: MouseRegion(
        onEnter: (_) => setState(() => _hovered = true),
        onExit: (_) => setState(() => _hovered = false),
        child: Material(
          color: widget.selected
              ? AppColors.brand.withValues(alpha: 0.14)
              : Colors.transparent,
          borderRadius: BorderRadius.circular(8),
          child: InkWell(
            onTap: widget.onTap,
            borderRadius: BorderRadius.circular(8),
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 9),
              child: Row(
                children: [
                  Expanded(
                    child: Text(
                      title,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 13.5,
                        fontWeight: widget.selected
                            ? FontWeight.w600
                            : FontWeight.w400,
                        color: widget.selected
                            ? palette.text
                            : palette.textMuted,
                      ),
                    ),
                  ),
                  // Destructive actions stay out of the way until you reach for
                  // them; a delete icon on every row invites accidents.
                  if (_hovered || widget.selected)
                    _IconAction(
                      icon: Icons.delete_outline,
                      tooltip: 'Delete chat',
                      onPressed: widget.onDelete,
                      palette: palette,
                      size: 16,
                    ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _SidebarFooter extends StatelessWidget {
  const _SidebarFooter({required this.palette, required this.onSettings});

  final AppPalette palette;
  final VoidCallback? onSettings;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      child: Row(
        children: [
          ValueListenableBuilder<ThemeMode>(
            valueListenable: ThemeLogic().mode,
            builder: (context, mode, _) {
              final isDark = mode == ThemeMode.dark;
              return _IconAction(
                icon: isDark
                    ? Icons.light_mode_outlined
                    : Icons.dark_mode_outlined,
                tooltip: isDark ? 'Light theme' : 'Dark theme',
                palette: palette,
                onPressed: () => ThemeLogic().mode.value = isDark
                    ? ThemeMode.light
                    : ThemeMode.dark,
              );
            },
          ),
          const Spacer(),
          if (onSettings != null)
            _IconAction(
              icon: Icons.settings_outlined,
              tooltip: 'Settings',
              palette: palette,
              onPressed: onSettings!,
            ),
        ],
      ),
    );
  }
}

class _IconAction extends StatelessWidget {
  const _IconAction({
    required this.icon,
    required this.tooltip,
    required this.onPressed,
    required this.palette,
    this.size = 18,
  });

  final IconData icon;
  final String tooltip;
  final VoidCallback onPressed;
  final AppPalette palette;
  final double size;

  @override
  Widget build(BuildContext context) {
    return IconButton(
      icon: Icon(icon, size: size),
      tooltip: tooltip,
      onPressed: onPressed,
      color: palette.textMuted,
      hoverColor: palette.raised,
      constraints: const BoxConstraints(minWidth: 32, minHeight: 32),
      padding: EdgeInsets.zero,
      visualDensity: VisualDensity.compact,
    );
  }
}

class _EmptyState extends StatelessWidget {
  const _EmptyState({
    required this.palette,
    required this.creating,
    required this.onNewConversation,
  });

  final AppPalette palette;
  final bool creating;
  final VoidCallback onNewConversation;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 420),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.auto_awesome, size: 34, color: AppColors.brand),
            const SizedBox(height: 18),
            Text(
              'Ask Turing anything',
              style: TextStyle(
                fontSize: 19,
                fontWeight: FontWeight.w600,
                color: palette.text,
              ),
            ),
            const SizedBox(height: 8),
            Text(
              'Runs on your machine. It can use tools, and it remembers what '
              'you told it in earlier conversations.',
              textAlign: TextAlign.center,
              style: TextStyle(
                fontSize: 13.5,
                height: 1.5,
                color: palette.textMuted,
              ),
            ),
            const SizedBox(height: 22),
            FilledButton.icon(
              onPressed: creating ? null : onNewConversation,
              icon: const Icon(Icons.add, size: 18),
              label: Text(creating ? 'Starting...' : 'New chat'),
            ),
          ],
        ),
      ),
    );
  }
}
