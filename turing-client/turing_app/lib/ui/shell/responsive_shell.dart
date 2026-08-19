import 'dart:async';

import 'package:flutter/material.dart';

import '../../constants/app_colors.dart';
import '../../features/chat/chat_screen.dart';
import '../../features/search/search_screen.dart';
import '../../features/settings/settings_screen.dart';
import '../../features/workspace/agents_page.dart';
import '../../features/workspace/automations_page.dart';
import '../../features/workspace/session_agent_bar.dart';
import '../../features/workspace/session_skills_bar.dart';
import '../../features/workspace/integrations_page.dart';
import '../../features/workspace/skills_page.dart';
import '../../features/workspace/workspace_pages.dart';
import '../../logic/theme_logic.dart';
import '../../models/session.dart';
import '../../models/session_title.dart';
import '../../models/turing_event.dart';
import '../../networking/api_client.dart';
import '../../networking/auth_storage.dart';
import '../../networking/event_source.dart';
import 'shell_destination.dart';

/// A conversation, with a rail of past ones and the app's other surfaces
/// beside it.
///
/// An earlier shell here had a nav rail whose destinations — Devices, Stats,
/// IoT — stood for features `docs/VISION.md` explicitly refuses to build, so
/// they were removed. The destinations in [ShellDestination] are a different
/// case: they are wanted, and each view either shows real backend state or
/// says in plain words that it is not built and what is in the way. The rule
/// is not "no destination without an implementation", it is "no destination
/// that pretends".
///
/// Selecting anything swaps it in place rather than pushing a route, so there
/// is never a back stack to unwind to reach the conversation again.
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

  /// Below this the sidebar cannot sit beside the conversation without
  /// squeezing both, so it becomes a drawer. Phones are always below it;
  /// a narrow desktop window lands here too, which is the point — the
  /// layout follows the space available, not the platform.
  static const double compactBreakpoint = 840;

  final GlobalKey<ScaffoldState> _scaffoldKey = GlobalKey<ScaffoldState>();

  /// Keeps the content pane's element alive across a breakpoint crossing.
  ///
  /// The compact and wide layouts put the body at different depths (a Scaffold
  /// body directly, versus inside a Row), so without a GlobalKey Flutter
  /// discards the subtree and builds a new one: history re-fetched, the event
  /// subscription torn down and reopened, a half-typed message lost. That is
  /// not a rare edge — an iPhone is 844 logical pixels wide in landscape, so
  /// one rotation crosses the 840 boundary.
  final GlobalKey _bodyKey = GlobalKey();

  // One event source per mounted ChatScreen, not one per rebuild.
  //
  // build() runs on every setState — including the one after each sent
  // message — and calling the factory there minted a fresh gRPC channel each
  // time. ChatScreen keeps the source it captured at mount, so those extras
  // were never connected and never shut down. Held here and released when the
  // chat is about to unmount, since ChatScreen closes what it was given.
  TuringEventSource? _chatEventSource;
  String? _chatEventSourceSessionId;

  // The resolved list is held rather than a Future so the shell can answer
  // questions about it — chiefly "what is the active conversation called",
  // which the compact app bar needs and a FutureBuilder cannot be asked.
  List<Session> _sessions = const [];
  bool _sessionsLoading = true;
  bool _sessionsFailed = false;
  ShellDestination _destination = ShellDestination.chats;
  String? _activeSessionId;
  String _modelProvider = 'ollama';
  bool _creating = false;
  final Set<String> _deleting = {};
  final Set<String> _locallyDeletedSessionIds = {};
  final Map<String, _SessionSnapshot> _sessionSnapshots = {};
  int _sessionStateRevision = 0;
  int _sessionRefreshRequest = 0;

  @override
  void initState() {
    super.initState();
    unawaited(_refreshSessions());
    unawaited(_loadModelProvider());
  }

  /// Chosen once in Settings; re-read whenever settings change.
  Future<void> _loadModelProvider() async {
    final stored = await widget.authStorage?.readModelProvider();
    if (!mounted || stored == null || stored.isEmpty) return;
    setState(() => _modelProvider = stored);
  }

  Future<void> _refreshSessions() async {
    final request = ++_sessionRefreshRequest;
    final startingRevision = _sessionStateRevision;
    try {
      final sessions = await widget.apiClient.listSessions();
      if (!mounted || request != _sessionRefreshRequest) return;
      setState(() {
        _sessions = _reconcileSessionRefresh(sessions, startingRevision);
        _sessionsLoading = false;
        _sessionsFailed = false;
      });
    } on Exception {
      if (!mounted || request != _sessionRefreshRequest) return;
      // The list is left as it was: a failed refresh should not blank out
      // conversations that are still perfectly valid on screen.
      setState(() {
        _sessionsLoading = false;
        _sessionsFailed = true;
      });
    }
  }

  void _applySessionUpdated(TuringEvent event) {
    if (_locallyDeletedSessionIds.contains(event.sessionId)) return;
    final title = event.payload['title'];
    final updatedAtValue = event.payload['updatedAt'];
    if (title is! String || updatedAtValue is! String) return;
    final updatedAt = DateTime.tryParse(updatedAtValue);
    if (updatedAt == null) return;
    final index = _sessions.indexWhere(
      (session) => session.sessionId == event.sessionId,
    );
    if (index >= 0 && _sessions[index].updatedAt.isAfter(updatedAt)) return;
    final updated = Session(
      sessionId: event.sessionId,
      title: title.isEmpty ? null : title,
      updatedAt: updatedAt.toUtc(),
    );
    setState(() {
      final next = List<Session>.of(_sessions);
      if (index >= 0) {
        final previous = next.removeAt(index);
        if (previous.updatedAt.isAtSameMomentAs(updated.updatedAt)) {
          next.insert(index, updated);
        } else {
          _insertSessionByRecency(next, updated);
        }
      } else {
        _insertSessionByRecency(next, updated);
      }
      _sessions = next;
      _recordSessionSnapshot(updated);
    });
  }

  static void _insertSessionByRecency(
    List<Session> sessions,
    Session inserted,
  ) {
    final index = sessions.indexWhere(
      (session) => session.updatedAt.isBefore(inserted.updatedAt),
    );
    if (index < 0) {
      sessions.add(inserted);
    } else {
      sessions.insert(index, inserted);
    }
  }

  List<Session> _reconcileSessionRefresh(
    Iterable<Session> refreshed,
    int startingRevision,
  ) {
    final refreshedSessions = refreshed.toList();
    final merged = refreshedSessions
        .where(
          (session) => !_locallyDeletedSessionIds.contains(session.sessionId),
        )
        .toList();
    final concurrentSnapshots = _sessionSnapshots.values
        .where((snapshot) => snapshot.revision > startingRevision)
        .toList();
    _sessionSnapshots.removeWhere(
      (_, snapshot) => snapshot.revision <= startingRevision,
    );
    for (final snapshot in concurrentSnapshots) {
      final current = snapshot.session;
      final index = merged.indexWhere(
        (session) => session.sessionId == current.sessionId,
      );
      if (index < 0) {
        _insertSessionByRecency(merged, current);
        continue;
      }
      final replacement = merged[index];
      if (current.updatedAt.isAfter(replacement.updatedAt)) {
        merged.removeAt(index);
        _insertSessionByRecency(merged, current);
      } else if (current.updatedAt.isAtSameMomentAs(replacement.updatedAt)) {
        merged[index] = current;
      }
    }
    return merged;
  }

  void _recordSessionSnapshot(Session session) {
    _sessionStateRevision++;
    _sessionSnapshots[session.sessionId] = _SessionSnapshot(
      session: session,
      revision: _sessionStateRevision,
    );
  }

  /// Null when the active conversation has not been named yet, or is not in
  /// the list the shell last loaded.
  String? get _activeSessionTitle {
    for (final session in _sessions) {
      if (session.sessionId == _activeSessionId) {
        return session.title?.isNotEmpty == true ? session.title : null;
      }
    }
    return null;
  }

  TuringEventSource _eventSourceForChat(String sessionId) {
    if (_chatEventSource == null || _chatEventSourceSessionId != sessionId) {
      _chatEventSourceSessionId = sessionId;
      _chatEventSource = widget.eventSourceFactory();
    }
    return _chatEventSource!;
  }

  /// Forgets the current source because the ChatScreen holding it is about to
  /// go away. Deliberately does not close it — ChatScreen does that in its own
  /// dispose, and closing it here would shut the channel out from under a
  /// widget that is still mounted.
  void _releaseChatEventSource() {
    _chatEventSource = null;
    _chatEventSourceSessionId = null;
  }

  Future<void> _newConversation() async {
    if (_creating) return;
    setState(() => _creating = true);
    try {
      // Deliberately untitled. Sending a title here would look harmless but
      // would permanently defeat the backend's auto-naming, which only fills
      // in a title that is still empty.
      final result = await widget.apiClient.createSession();
      if (!mounted) return;
      final session = Session(
        sessionId: result['sessionId'] as String,
        title: null,
        updatedAt: DateTime.parse(result['createdAt'] as String).toUtc(),
      );
      setState(() {
        final sessions = List<Session>.of(_sessions);
        _insertSessionByRecency(sessions, session);
        _sessions = sessions;
        _recordSessionSnapshot(session);
        _activeSessionId = session.sessionId;
        _destination = ShellDestination.chats;
        _creating = false;
      });
      unawaited(_refreshSessions());
    } catch (error) {
      if (!mounted) return;
      setState(() => _creating = false);
      _toast('Could not start a new chat: $error');
    }
  }

  Future<void> _deleteConversation(Session session) async {
    if (!_deleting.add(session.sessionId)) return;
    try {
      final confirmed = await showDialog<bool>(
        context: context,
        builder: (dialogContext) => AlertDialog(
          title: Text('Delete "${sessionDisplayTitle(session)}"?'),
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
      setState(() {
        _locallyDeletedSessionIds.add(session.sessionId);
        _sessionSnapshots.remove(session.sessionId);
        _sessions = _sessions
            .where((candidate) => candidate.sessionId != session.sessionId)
            .toList();
        if (_activeSessionId == session.sessionId) {
          _activeSessionId = null;
        }
      });
      unawaited(_refreshSessions());
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

  /// On the compact layout every navigation happens inside the drawer, so it
  /// has to close itself — otherwise you pick a chat and stare at the sidebar
  /// that is covering it.
  void _closeDrawerIfOpen() {
    // closeDrawer() rather than Navigator.pop(): popping happens to work
    // (the drawer registers a local history entry) but it pops whatever is
    // topmost, so it would be one pushed route away from closing the wrong
    // thing. closeDrawer is a documented no-op when nothing is open.
    _scaffoldKey.currentState?.closeDrawer();
  }

  void _selectSession(String sessionId) {
    if (sessionId != _activeSessionId) _releaseChatEventSource();
    setState(() {
      _activeSessionId = sessionId;
      _destination = ShellDestination.chats;
    });
    _closeDrawerIfOpen();
  }

  void _selectDestination(ShellDestination destination) {
    // Leaving Chats unmounts the conversation, which closes its source.
    if (destination != ShellDestination.chats) _releaseChatEventSource();
    setState(() => _destination = destination);
    _closeDrawerIfOpen();
  }

  Future<void> _openSearch() async {
    _closeDrawerIfOpen();
    await Navigator.of(context).push(
      MaterialPageRoute(
        builder: (_) => SearchScreen(
          apiClient: widget.apiClient,
          onOpenSession: (sessionId) async {
            if (mounted) {
              setState(() {
                _activeSessionId = sessionId;
                _destination = ShellDestination.chats;
              });
            }
          },
        ),
      ),
    );
  }

  Future<void> _openSettings() async {
    final authStorage = widget.authStorage;
    if (authStorage == null) return;
    _closeDrawerIfOpen();
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
    return LayoutBuilder(
      builder: (context, constraints) {
        final compact = constraints.maxWidth < compactBreakpoint;
        final sidebar = _Sidebar(
          width: _sidebarWidth,
          palette: palette,
          sessions: _sessions,
          sessionsLoading: _sessionsLoading,
          sessionsFailed: _sessionsFailed,
          activeSessionId: _activeSessionId,
          destination: _destination,
          creating: _creating,
          onNewConversation: _newConversation,
          onSelect: _selectSession,
          onSelectDestination: _selectDestination,
          onDelete: _deleteConversation,
          onSearch: _openSearch,
          onSettings: widget.authStorage == null ? null : _openSettings,
        );

        // Built once and reparented by its GlobalKey, so crossing the
        // breakpoint moves the live conversation between layouts instead of
        // rebuilding it.
        final body = KeyedSubtree(key: _bodyKey, child: _body(palette));

        return Scaffold(
          key: _scaffoldKey,
          backgroundColor: palette.background,
          appBar: compact ? _compactAppBar(palette) : null,
          drawer: compact
              ? Drawer(
                  width: _sidebarWidth,
                  backgroundColor: palette.surface,
                  child: sidebar,
                )
              : null,
          body: compact
              ? body
              : Row(
                  children: [
                    sidebar,
                    VerticalDivider(width: 1, color: palette.border),
                    Expanded(child: body),
                  ],
                ),
        );
      },
    );
  }

  PreferredSizeWidget _compactAppBar(AppPalette palette) {
    return AppBar(
      backgroundColor: palette.surface,
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      scrolledUnderElevation: 0,
      shape: Border(bottom: BorderSide(color: palette.border)),
      title: Text(
        _compactTitle,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
        style: TextStyle(
          fontSize: 15,
          fontWeight: FontWeight.w600,
          color: palette.text,
        ),
      ),
      actions: [
        if (_destination == ShellDestination.chats)
          IconButton(
            icon: const Icon(Icons.add, size: 20),
            tooltip: 'New chat',
            color: palette.text,
            onPressed: _creating ? null : _newConversation,
          ),
      ],
    );
  }

  /// On a phone the sidebar is hidden, so the bar is the only thing that can
  /// say which conversation you are in.
  String get _compactTitle {
    if (_destination != ShellDestination.chats) return _destination.label;
    final sessionId = _activeSessionId;
    if (sessionId == null) return 'Turing';
    return _activeSessionTitle ?? 'New chat';
  }

  Widget _body(AppPalette palette) {
    switch (_destination) {
      case ShellDestination.chats:
        return _conversation(palette);
      case ShellDestination.mcps:
        return McpsPage(apiClient: widget.apiClient);
      case ShellDestination.agents:
        return AgentsPage(apiClient: widget.apiClient);
      case ShellDestination.skills:
        return SkillsPage(apiClient: widget.apiClient);
      case ShellDestination.integrations:
        return IntegrationsPage(apiClient: widget.apiClient);
      case ShellDestination.automations:
        return AutomationsPage(
          apiClient: widget.apiClient,
          onOpenSession: _selectSession,
        );
    }
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
    return Column(
      children: [
        // First, above even the skills: where a conversation goes is more
        // fundamental than how it is told to answer, and it is the one
        // property that decides whether anything typed below leaves the
        // machine. Keyed like the rest so the previous conversation's
        // destination can never linger on screen while the new one loads —
        // which, for this particular strip, would be a lie about egress.
        SessionAgentBar(
          key: ValueKey('agent-$sessionId'),
          apiClient: widget.apiClient,
          sessionId: sessionId,
        ),
        // Above the transcript rather than inside it: standing instructions
        // that change the answers should be visible while you read them.
        // Keyed like the chat below it: a fresh State per conversation is what
        // guarantees the previous conversation's skills can never linger on
        // screen while the new set loads.
        SessionSkillsBar(
          key: ValueKey('skills-$sessionId'),
          apiClient: widget.apiClient,
          sessionId: sessionId,
        ),
        Expanded(
          child: ChatScreen(
            // Keyed by session so switching conversations rebuilds the chat
            // state instead of leaking the previous transcript into the new one.
            key: ValueKey(sessionId),
            sessionId: sessionId,
            apiClient: widget.apiClient,
            eventSource: _eventSourceForChat(sessionId),
            embedded: true,
            modelProvider: _modelProvider,
            onSessionUpdated: _applySessionUpdated,
          ),
        ),
      ],
    );
  }
}

class _Sidebar extends StatelessWidget {
  const _Sidebar({
    required this.width,
    required this.palette,
    required this.sessions,
    required this.sessionsLoading,
    required this.sessionsFailed,
    required this.activeSessionId,
    required this.destination,
    required this.creating,
    required this.onNewConversation,
    required this.onSelect,
    required this.onSelectDestination,
    required this.onDelete,
    required this.onSearch,
    required this.onSettings,
  });

  final double width;
  final AppPalette palette;
  final List<Session> sessions;
  final bool sessionsLoading;
  final bool sessionsFailed;
  final String? activeSessionId;
  final ShellDestination destination;
  final bool creating;
  final VoidCallback onNewConversation;
  final ValueChanged<String> onSelect;
  final ValueChanged<ShellDestination> onSelectDestination;
  final ValueChanged<Session> onDelete;
  final VoidCallback onSearch;
  final VoidCallback? onSettings;

  /// Below this the sidebar cannot hold the destinations, the conversation
  /// list and the footer at once, so the destinations join the scroll. Above
  /// it they stay pinned, because on a desktop the destinations are the app's
  /// primary navigation and scrolling a long chat list must never take them
  /// off screen.
  static const double _pinnedNavMinHeight = 460;

  @override
  Widget build(BuildContext context) {
    final showingChats = destination == ShellDestination.chats;
    return Container(
      width: width,
      color: palette.surface,
      child: SafeArea(
        right: false,
        child: LayoutBuilder(
          builder: (context, constraints) {
            final pinNav = constraints.maxHeight >= _pinnedNavMinHeight;
            return _content(pinNav: pinNav, showingChats: showingChats);
          },
        ),
      ),
    );
  }

  Widget _content({required bool pinNav, required bool showingChats}) {
    final navItems = [
      for (final item in ShellDestination.navigation)
        _NavItem(
          destination: item,
          palette: palette,
          selected: destination == item,
          onTap: () => onSelectDestination(item),
        ),
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 16, 16, 10),
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
        // Pinned when there is room, so a long conversation list never
        // scrolls the app's navigation away. Only when the sidebar is too
        // short to hold everything do the destinations join the scroll —
        // otherwise five fixed rows plus a fixed header and footer clip
        // the footer, which is where Settings lives.
        if (pinNav) ...navItems,
        Expanded(
          child: ListView(
            padding: const EdgeInsets.only(bottom: 8),
            children: [
              if (!pinNav) ...navItems,
              const SizedBox(height: 14),
              _ChatsHeader(
                palette: palette,
                selected: showingChats,
                onTap: () => onSelectDestination(ShellDestination.chats),
                onSearch: onSearch,
              ),
              if (sessionsLoading)
                const Padding(
                  padding: EdgeInsets.symmetric(vertical: 18),
                  child: Center(
                    child: SizedBox.square(
                      dimension: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    ),
                  ),
                )
              else if (sessions.isEmpty)
                Padding(
                  padding: const EdgeInsets.fromLTRB(20, 10, 20, 10),
                  child: Text(
                    sessionsFailed
                        ? 'Could not load conversations.'
                        : 'No conversations yet.',
                    textAlign: TextAlign.center,
                    style: TextStyle(fontSize: 13, color: palette.textMuted),
                  ),
                )
              else
                for (final session in sessions)
                  Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 8),
                    child: _SessionTile(
                      session: session,
                      palette: palette,
                      // A conversation is only "current" while the chat
                      // view is what you are looking at; highlighting it
                      // from the MCPs page would claim a selection that is
                      // not on screen.
                      selected:
                          showingChats && session.sessionId == activeSessionId,
                      onTap: () => onSelect(session.sessionId),
                      onDelete: () => onDelete(session),
                    ),
                  ),
            ],
          ),
        ),
        Divider(height: 1, color: palette.border),
        _SidebarFooter(palette: palette, onSettings: onSettings),
      ],
    );
  }
}

/// The "Chats" section label doubles as the way back to the conversation
/// list. Without it, a compact user who opened Skills could only return by
/// tapping a conversation — impossible before they have one — or by creating
/// a new one, which is a side effect, not navigation.
class _ChatsHeader extends StatelessWidget {
  const _ChatsHeader({
    required this.palette,
    required this.selected,
    required this.onTap,
    required this.onSearch,
  });

  final AppPalette palette;
  final bool selected;
  final VoidCallback onTap;
  final VoidCallback onSearch;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(8, 0, 10, 4),
      child: Row(
        children: [
          Expanded(
            child: InkWell(
              onTap: onTap,
              borderRadius: BorderRadius.circular(6),
              child: Padding(
                padding: const EdgeInsets.fromLTRB(10, 6, 10, 6),
                child: Text(
                  'Chats',
                  style: TextStyle(
                    fontSize: 11.5,
                    fontWeight: FontWeight.w600,
                    letterSpacing: 0.5,
                    color: selected ? palette.text : palette.textMuted,
                  ),
                ),
              ),
            ),
          ),
          _IconAction(
            icon: Icons.search,
            tooltip: 'Search conversations',
            onPressed: onSearch,
            palette: palette,
            size: 16,
          ),
        ],
      ),
    );
  }
}

class _NavItem extends StatelessWidget {
  const _NavItem({
    required this.destination,
    required this.palette,
    required this.selected,
    required this.onTap,
  });

  final ShellDestination destination;
  final AppPalette palette;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 1),
      child: Material(
        color: selected
            ? AppColors.brand.withValues(alpha: 0.14)
            : Colors.transparent,
        borderRadius: BorderRadius.circular(8),
        child: InkWell(
          onTap: onTap,
          borderRadius: BorderRadius.circular(8),
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 9),
            child: Row(
              children: [
                Icon(
                  selected ? destination.selectedIcon : destination.icon,
                  size: 17,
                  color: selected ? AppColors.brand : palette.textMuted,
                ),
                const SizedBox(width: 11),
                Expanded(
                  child: Text(
                    destination.label,
                    style: TextStyle(
                      fontSize: 13.5,
                      fontWeight: selected ? FontWeight.w600 : FontWeight.w400,
                      color: selected ? palette.text : palette.textMuted,
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
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
    final title = sessionDisplayTitle(widget.session);
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
      child: SingleChildScrollView(
        padding: const EdgeInsets.all(24),
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
      ),
    );
  }
}

class _SessionSnapshot {
  const _SessionSnapshot({required this.session, required this.revision});

  final Session session;
  final int revision;
}
