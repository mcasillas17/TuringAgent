import 'dart:async';

import 'package:flutter/material.dart';

import '../../constants/app_colors.dart';
import '../../features/chat/chat_screen.dart';
import '../../features/search/search_screen.dart';
import '../../features/settings/settings_screen.dart';
import '../../features/workspace/agents_page.dart';
import '../../features/workspace/automations_page.dart';
import '../../features/workspace/session_agent_bar.dart';
import '../../features/workspace/integrations_page.dart';
import '../../features/workspace/skills_page.dart';
import '../../features/workspace/telemetry_page.dart';
import '../../features/workspace/workspace_pages.dart';
import '../../logic/theme_logic.dart';
import '../../models/session.dart';
import '../../models/session_page.dart';
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
    this.sessionUpdateSourceFactory,
    this.authStorage,
    this.initialBackendUrl = 'http://localhost:3000',
    this.initialApiKey = '',
    this.onSettingsChanged,
  });

  final TuringApi apiClient;
  final TuringEventSource Function() eventSourceFactory;
  final TuringSessionUpdateSource? Function()? sessionUpdateSourceFactory;
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
  TuringSessionUpdateSource? _sessionUpdateSource;
  StreamSubscription<TuringEvent>? _sessionUpdateSubscription;
  Timer? _sessionUpdateReconnectTimer;
  Timer? _sessionUpdateStabilityTimer;
  int _sessionUpdateReconnectAttempts = 0;

  // The resolved list is held rather than a Future so the shell can answer
  // questions about it — chiefly "what is the active conversation called",
  // which the compact app bar needs and a FutureBuilder cannot be asked.
  List<Session> _sessions = const [];
  bool _sessionsLoading = true;
  bool _sessionsLoadingMore = false;
  bool _sessionsFailed = false;
  String? _nextSessionsCursor;
  bool _hasLoadedTailPages = false;
  Set<String> _firstPageSessionIds = const {};
  final Set<String> _eventOnlySessionIds = {};
  ShellDestination _destination = ShellDestination.chats;
  String? _activeSessionId;
  String _modelProvider = 'ollama';
  bool _creating = false;
  final Set<String> _deleting = {};
  final Set<String> _lifecycleMutating = {};
  final Set<String> _locallyDeletedSessionIds = {};
  final Map<String, _SessionSnapshot> _sessionSnapshots = {};
  int _sessionStateRevision = 0;
  int _sessionRefreshRequest = 0;

  @override
  void initState() {
    super.initState();
    _openSessionUpdateSubscription();
    unawaited(_refreshSessions());
    unawaited(_loadModelProvider());
  }

  void _openSessionUpdateSubscription() {
    TuringSessionUpdateSource? source;
    try {
      source = widget.sessionUpdateSourceFactory?.call();
      if (source == null) return;
      _sessionUpdateSource = source;
      _sessionUpdateSubscription = source.connectSessionUpdates().listen(
        _applyGlobalSessionUpdated,
        onError: (_, _) => _handleSessionUpdateStreamEnded(),
        onDone: _handleSessionUpdateStreamEnded,
        cancelOnError: true,
      );
      _sessionUpdateStabilityTimer?.cancel();
      final openedSource = source;
      _sessionUpdateStabilityTimer = Timer(const Duration(seconds: 30), () {
        _sessionUpdateStabilityTimer = null;
        if (!mounted || !identical(_sessionUpdateSource, openedSource)) return;
        _sessionUpdateReconnectAttempts = 0;
      });
    } catch (_) {
      source?.close();
      _sessionUpdateSource = null;
      _scheduleSessionUpdateReconnect();
    }
  }

  void _handleSessionUpdateStreamEnded() {
    if (!mounted) return;
    _sessionUpdateStabilityTimer?.cancel();
    _sessionUpdateStabilityTimer = null;
    unawaited(_sessionUpdateSubscription?.cancel());
    _sessionUpdateSubscription = null;
    _sessionUpdateSource?.close();
    _sessionUpdateSource = null;
    _scheduleSessionUpdateReconnect();
  }

  void _scheduleSessionUpdateReconnect() {
    if (!mounted || _sessionUpdateReconnectTimer != null) return;
    final exponent = _sessionUpdateReconnectAttempts.clamp(0, 5);
    final delay = Duration(seconds: 1 << exponent);
    _sessionUpdateReconnectAttempts++;
    _sessionUpdateReconnectTimer = Timer(delay, () {
      _sessionUpdateReconnectTimer = null;
      if (!mounted) return;
      _openSessionUpdateSubscription();
      unawaited(_refreshSessions());
    });
  }

  @override
  void dispose() {
    _sessionUpdateReconnectTimer?.cancel();
    _sessionUpdateStabilityTimer?.cancel();
    unawaited(_sessionUpdateSubscription?.cancel());
    _sessionUpdateSource?.close();
    super.dispose();
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
      final page = await widget.apiClient.listSessionPage();
      if (!mounted || request != _sessionRefreshRequest) return;
      setState(() {
        _sessions = _reconcileSessionRefresh(page.sessions, startingRevision);
        _firstPageSessionIds = page.sessions
            .map((session) => session.sessionId)
            .toSet();
        if (!_hasLoadedTailPages) {
          _nextSessionsCursor = page.nextCursor;
        }
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

  Future<void> _loadMoreSessions() async {
    final cursor = _nextSessionsCursor;
    if (cursor == null || _sessionsLoadingMore) return;
    setState(() => _sessionsLoadingMore = true);
    try {
      final page = await widget.apiClient.listSessionPage(cursor: cursor);
      if (!mounted || _nextSessionsCursor != cursor) return;
      setState(() {
        final merged = List<Session>.of(_sessions);
        for (final session in page.sessions) {
          final authoritative = _resolveSessionSnapshot(
            session,
            observedByList: true,
          );
          _eventOnlySessionIds.remove(authoritative.sessionId);
          merged.removeWhere(
            (candidate) => candidate.sessionId == authoritative.sessionId,
          );
          if (authoritative.status == SessionStatus.active &&
              !_locallyDeletedSessionIds.contains(authoritative.sessionId)) {
            _insertSessionByRecency(merged, authoritative);
          }
        }
        _removeArchivedSessions(merged);
        _sessions = merged;
        _nextSessionsCursor = page.nextCursor;
        _hasLoadedTailPages = true;
        _sessionsFailed = false;
      });
    } on Exception {
      if (!mounted) return;
      setState(() => _sessionsFailed = true);
    } finally {
      if (mounted) setState(() => _sessionsLoadingMore = false);
    }
  }

  void _applyGlobalSessionUpdated(TuringEvent event) {
    _applySessionUpdated(event);
  }

  void _applySessionUpdated(TuringEvent event) {
    if (!mounted) return;
    if (_locallyDeletedSessionIds.contains(event.sessionId)) return;
    final title = event.payload['title'];
    final updatedAtValue = event.payload['updatedAt'];
    if (title is! String || updatedAtValue is! String) return;
    final updatedAt = DateTime.tryParse(updatedAtValue);
    final updatedAtNanoseconds = _parseTimestampNanoseconds(updatedAtValue);
    if (updatedAt == null || updatedAtNanoseconds == null) return;
    final previousSnapshot = _sessionSnapshots[event.sessionId];
    final statusValue = event.payload['status'];
    final status = switch (statusValue) {
      'active' => SessionStatus.active,
      'archived' => SessionStatus.archived,
      null when previousSnapshot?.session.status == SessionStatus.archived =>
        null,
      null => SessionStatus.active,
      _ => null,
    };
    if (status == null) return;
    if (previousSnapshot != null &&
        previousSnapshot.session.updatedAtNanoseconds >= updatedAtNanoseconds) {
      return;
    }
    final index = _sessions.indexWhere(
      (session) => session.sessionId == event.sessionId,
    );
    if (index >= 0 &&
        _sessions[index].updatedAtNanoseconds > updatedAtNanoseconds) {
      return;
    }
    final updated = Session(
      sessionId: event.sessionId,
      title: title.isEmpty ? null : title,
      updatedAt: updatedAt.toUtc(),
      updatedAtNanoseconds: updatedAtNanoseconds,
      status: status,
    );
    if (updated.status == SessionStatus.archived &&
        _activeSessionId == updated.sessionId) {
      _releaseChatEventSource();
    }
    setState(() {
      final next = List<Session>.of(_sessions);
      if (index >= 0) {
        final previous = next.removeAt(index);
        if (updated.status == SessionStatus.archived) {
          if (_activeSessionId == updated.sessionId) {
            _activeSessionId = null;
          }
        } else if (previous.updatedAtNanoseconds ==
            updated.updatedAtNanoseconds) {
          next.insert(index, updated);
        } else {
          _insertSessionByRecency(next, updated);
        }
      } else if (updated.status == SessionStatus.active) {
        _insertSessionByRecency(next, updated);
        _eventOnlySessionIds.add(updated.sessionId);
      }
      if (updated.status == SessionStatus.archived &&
          _activeSessionId == updated.sessionId) {
        _activeSessionId = null;
      }
      _sessions = next;
      _recordSessionSnapshot(
        updated,
        retainUntilObserved:
            _sessionSnapshots[event.sessionId]?.retainUntilObserved ?? false,
      );
    });
  }

  static void _insertSessionByRecency(
    List<Session> sessions,
    Session inserted,
  ) {
    final index = sessions.indexWhere(
      (session) =>
          session.updatedAtNanoseconds < inserted.updatedAtNanoseconds ||
          (session.updatedAtNanoseconds == inserted.updatedAtNanoseconds &&
              session.sessionId.compareTo(inserted.sessionId) < 0),
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
    final merged = _sessions
        .where(
          (session) =>
              !_firstPageSessionIds.contains(session.sessionId) &&
              !_eventOnlySessionIds.contains(session.sessionId) &&
              !_locallyDeletedSessionIds.contains(session.sessionId),
        )
        .toList();
    for (final session in refreshedSessions) {
      final authoritative = _resolveSessionSnapshot(
        session,
        observedByList: true,
      );
      _eventOnlySessionIds.remove(authoritative.sessionId);
      merged.removeWhere(
        (candidate) => candidate.sessionId == authoritative.sessionId,
      );
      if (authoritative.status == SessionStatus.active &&
          !_locallyDeletedSessionIds.contains(authoritative.sessionId)) {
        merged.add(authoritative);
      }
    }
    for (final entry in _sessionSnapshots.entries.toList()) {
      final snapshot = entry.value;
      final current = snapshot.session;
      if (_locallyDeletedSessionIds.contains(current.sessionId)) continue;
      final index = merged.indexWhere(
        (session) => session.sessionId == current.sessionId,
      );
      if (index < 0) {
        if (current.status == SessionStatus.active &&
            (snapshot.retainUntilObserved ||
                snapshot.revision > startingRevision)) {
          _insertSessionByRecency(merged, current);
        }
        continue;
      }
      final replacement = merged[index];
      if (current.updatedAtNanoseconds > replacement.updatedAtNanoseconds) {
        merged.removeAt(index);
        _insertSessionByRecency(merged, current);
      } else if (current.updatedAtNanoseconds ==
          replacement.updatedAtNanoseconds) {
        merged[index] = current;
      }
    }
    _removeArchivedSessions(merged);
    merged.sort(_compareSessionsByRecency);
    return merged;
  }

  Session _resolveSessionSnapshot(
    Session session, {
    required bool observedByList,
  }) {
    final previous = _sessionSnapshots[session.sessionId];
    if (previous == null ||
        session.updatedAtNanoseconds > previous.session.updatedAtNanoseconds) {
      _recordSessionSnapshot(
        session,
        retainUntilObserved: observedByList
            ? false
            : previous?.retainUntilObserved ?? false,
      );
      return session;
    }
    if (observedByList &&
        session.updatedAtNanoseconds == previous.session.updatedAtNanoseconds &&
        previous.retainUntilObserved) {
      _sessionSnapshots[session.sessionId] = _SessionSnapshot(
        session: previous.session,
        revision: previous.revision,
        retainUntilObserved: false,
      );
    }
    return previous.session;
  }

  void _removeArchivedSessions(List<Session> sessions) {
    sessions.removeWhere(
      (session) =>
          _sessionSnapshots[session.sessionId]?.session.status ==
          SessionStatus.archived,
    );
  }

  static int _compareSessionsByRecency(Session left, Session right) {
    final timestampOrder = right.updatedAtNanoseconds.compareTo(
      left.updatedAtNanoseconds,
    );
    if (timestampOrder != 0) return timestampOrder;
    return right.sessionId.compareTo(left.sessionId);
  }

  void _recordSessionSnapshot(
    Session session, {
    required bool retainUntilObserved,
  }) {
    _sessionStateRevision++;
    _sessionSnapshots[session.sessionId] = _SessionSnapshot(
      session: session,
      revision: _sessionStateRevision,
      retainUntilObserved: retainUntilObserved,
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
      final createdAtValue = result['createdAt'] as String;
      final session = Session(
        sessionId: result['sessionId'] as String,
        title: null,
        updatedAt: DateTime.parse(createdAtValue).toUtc(),
        updatedAtNanoseconds:
            result['createdAtNanoseconds'] as int? ??
            _parseTimestampNanoseconds(createdAtValue),
      );
      setState(() {
        final sessions = List<Session>.of(_sessions);
        _insertSessionByRecency(sessions, session);
        _sessions = sessions;
        _recordSessionSnapshot(session, retainUntilObserved: true);
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

  static int? _parseTimestampNanoseconds(String value) {
    final timestamp = DateTime.tryParse(value);
    if (timestamp == null) return null;
    final match = RegExp(r'\.(\d+)(?:Z|[+-]\d{2}:\d{2})$').firstMatch(value);
    final fraction = match?.group(1) ?? '';
    final nanos = int.parse(fraction.padRight(9, '0').substring(0, 9));
    final seconds = timestamp.toUtc().millisecondsSinceEpoch ~/ 1000;
    return seconds * 1000000000 + nanos;
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

  Future<void> _renameConversation(Session session) async {
    if (!_lifecycleMutating.add(session.sessionId)) return;
    try {
      final title = await _showRenameSessionDialog(context, session);
      if (title == null) return;
      final updated = await widget.apiClient.renameSession(
        sessionId: session.sessionId,
        title: title,
      );
      _applyAuthoritativeSession(updated);
      unawaited(_refreshSessions());
    } catch (error) {
      if (mounted) _toast('Could not rename this chat: $error');
    } finally {
      _lifecycleMutating.remove(session.sessionId);
    }
  }

  Future<void> _archiveConversation(Session session) async {
    if (!_lifecycleMutating.add(session.sessionId)) return;
    try {
      final updated = await widget.apiClient.archiveSession(
        sessionId: session.sessionId,
      );
      _applyAuthoritativeSession(updated);
      unawaited(_refreshSessions());
    } catch (error) {
      if (mounted) _toast('Could not archive this chat: $error');
    } finally {
      _lifecycleMutating.remove(session.sessionId);
    }
  }

  void _applyAuthoritativeSession(Session session) {
    if (!mounted || _locallyDeletedSessionIds.contains(session.sessionId)) {
      return;
    }
    final previous = _sessionSnapshots[session.sessionId];
    if (previous != null &&
        previous.session.updatedAtNanoseconds > session.updatedAtNanoseconds) {
      return;
    }
    if (session.status == SessionStatus.archived &&
        _activeSessionId == session.sessionId) {
      _releaseChatEventSource();
    }
    setState(() {
      _recordSessionSnapshot(session, retainUntilObserved: false);
      final next = List<Session>.of(_sessions)
        ..removeWhere((candidate) => candidate.sessionId == session.sessionId);
      if (session.status == SessionStatus.active) {
        _insertSessionByRecency(next, session);
      } else if (_activeSessionId == session.sessionId) {
        _activeSessionId = null;
      }
      _sessions = next;
    });
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

  Future<void> _openArchivedSessions() async {
    _closeDrawerIfOpen();
    await showDialog<void>(
      context: context,
      builder: (dialogContext) => AlertDialog(
        title: const Text('Archived conversations'),
        content: SizedBox(
          width: 480,
          height: 440,
          child: _ArchivedSessionsList(
            apiClient: widget.apiClient,
            onRestored: _applyAuthoritativeSession,
            onDeleted: (sessionId) {
              if (!mounted) return;
              setState(() {
                _locallyDeletedSessionIds.add(sessionId);
                _sessionSnapshots.remove(sessionId);
                _sessions = _sessions
                    .where((session) => session.sessionId != sessionId)
                    .toList();
              });
            },
          ),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(),
            child: const Text('Close'),
          ),
        ],
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
          nextCursor: _nextSessionsCursor,
          loadingMore: _sessionsLoadingMore,
          activeSessionId: _activeSessionId,
          destination: _destination,
          creating: _creating,
          onNewConversation: _newConversation,
          onSelect: _selectSession,
          onSelectDestination: _selectDestination,
          onDelete: _deleteConversation,
          onRename: _renameConversation,
          onArchive: _archiveConversation,
          onLoadMore: _loadMoreSessions,
          onSearch: _openSearch,
          onArchived: _openArchivedSessions,
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
      case ShellDestination.telemetry:
        return TelemetryPage(apiClient: widget.apiClient);
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
        // The one conversation property that decides whether anything typed
        // below leaves the machine. Keyed like the rest so the previous one's
        // destination can never linger on screen while the new one loads —
        // which, for this particular strip, would be a lie about egress.
        SessionAgentBar(
          key: ValueKey('agent-$sessionId'),
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
            onSessionUpdated: (event) => _applySessionUpdated(event),
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
    required this.nextCursor,
    required this.loadingMore,
    required this.activeSessionId,
    required this.destination,
    required this.creating,
    required this.onNewConversation,
    required this.onSelect,
    required this.onSelectDestination,
    required this.onDelete,
    required this.onRename,
    required this.onArchive,
    required this.onLoadMore,
    required this.onSearch,
    required this.onArchived,
    required this.onSettings,
  });

  final double width;
  final AppPalette palette;
  final List<Session> sessions;
  final bool sessionsLoading;
  final bool sessionsFailed;
  final String? nextCursor;
  final bool loadingMore;
  final String? activeSessionId;
  final ShellDestination destination;
  final bool creating;
  final VoidCallback onNewConversation;
  final ValueChanged<String> onSelect;
  final ValueChanged<ShellDestination> onSelectDestination;
  final ValueChanged<Session> onDelete;
  final ValueChanged<Session> onRename;
  final ValueChanged<Session> onArchive;
  final VoidCallback onLoadMore;
  final VoidCallback onSearch;
  final VoidCallback onArchived;
  final VoidCallback? onSettings;

  /// Below this the sidebar cannot hold the destinations, the conversation
  /// list and the footer at once, so the destinations join the scroll. Above
  /// it they stay pinned, because on a desktop the destinations are the app's
  /// primary navigation and scrolling a long chat list must never take them
  /// off screen.
  ///
  /// Raised with each destination added: every nav row is about 37 logical
  /// pixels of FIXED height, and the thing that overflows first when there is
  /// not enough room is the footer, where Settings lives.
  static const double _pinnedNavMinHeight = 500;

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
                onArchived: onArchived,
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
                      onRename: () => onRename(session),
                      onArchive: () => onArchive(session),
                    ),
                  ),
              if (nextCursor != null)
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 16),
                  child: TextButton(
                    onPressed: loadingMore ? null : onLoadMore,
                    child: Text(loadingMore ? 'Loading...' : 'Load more'),
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
    required this.onArchived,
  });

  final AppPalette palette;
  final bool selected;
  final VoidCallback onTap;
  final VoidCallback onSearch;
  final VoidCallback onArchived;

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
            icon: Icons.archive_outlined,
            tooltip: 'Archived conversations',
            onPressed: onArchived,
            palette: palette,
            size: 16,
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
    required this.onRename,
    required this.onArchive,
  });

  final Session session;
  final AppPalette palette;
  final bool selected;
  final VoidCallback onTap;
  final VoidCallback onDelete;
  final VoidCallback onRename;
  final VoidCallback onArchive;

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
                      icon: Icons.edit_outlined,
                      tooltip: 'Rename chat',
                      onPressed: widget.onRename,
                      palette: palette,
                      size: 16,
                    ),
                  if (_hovered || widget.selected)
                    _IconAction(
                      icon: Icons.archive_outlined,
                      tooltip: 'Archive chat',
                      onPressed: widget.onArchive,
                      palette: palette,
                      size: 16,
                    ),
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

String? _validateSessionTitle(String value) {
  final normalized = value.trim();
  if (normalized.isEmpty) return 'Enter a title.';
  if (normalized.runes.length > 120) {
    return 'Use 120 characters or fewer.';
  }
  return null;
}

Future<String?> _showRenameSessionDialog(
  BuildContext context,
  Session session,
) {
  var pendingTitle = session.title ?? '';
  var validation = _validateSessionTitle(pendingTitle);
  return showDialog<String>(
    context: context,
    builder: (dialogContext) => StatefulBuilder(
      builder: (context, setDialogState) => AlertDialog(
        title: const Text('Rename chat'),
        content: TextFormField(
          autofocus: true,
          initialValue: pendingTitle,
          decoration: InputDecoration(
            labelText: 'Title',
            errorText: validation,
          ),
          onChanged: (value) {
            setDialogState(() {
              pendingTitle = value;
              validation = _validateSessionTitle(value);
            });
          },
          onFieldSubmitted: (value) {
            if (_validateSessionTitle(value) == null) {
              Navigator.of(dialogContext).pop(value.trim());
            }
          },
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(dialogContext).pop(),
            child: const Text('Cancel'),
          ),
          FilledButton(
            onPressed: validation == null
                ? () => Navigator.of(dialogContext).pop(pendingTitle.trim())
                : null,
            child: const Text('Rename'),
          ),
        ],
      ),
    ),
  );
}

class _ArchivedSessionsList extends StatefulWidget {
  const _ArchivedSessionsList({
    required this.apiClient,
    required this.onRestored,
    required this.onDeleted,
  });

  final TuringApi apiClient;
  final ValueChanged<Session> onRestored;
  final ValueChanged<String> onDeleted;

  @override
  State<_ArchivedSessionsList> createState() => _ArchivedSessionsListState();
}

class _ArchivedSessionsListState extends State<_ArchivedSessionsList> {
  List<Session> _sessions = const [];
  String? _nextCursor;
  bool _loading = true;
  bool _loadingMore = false;
  Object? _error;
  final Set<String> _mutating = {};

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load({String? cursor}) async {
    if (cursor != null) {
      if (_loadingMore) return;
      setState(() => _loadingMore = true);
    }
    try {
      final page = await widget.apiClient.listSessionPage(
        cursor: cursor,
        filter: SessionListFilter.archived,
      );
      if (!mounted) return;
      setState(() {
        if (cursor == null) {
          _sessions = List<Session>.of(page.sessions);
        } else {
          final merged = <String, Session>{
            for (final session in _sessions) session.sessionId: session,
            for (final session in page.sessions) session.sessionId: session,
          };
          _sessions = merged.values.toList()
            ..sort(_ResponsiveShellState._compareSessionsByRecency);
        }
        _nextCursor = page.nextCursor;
        _loading = false;
        _error = null;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = error;
      });
    } finally {
      if (mounted) setState(() => _loadingMore = false);
    }
  }

  Future<void> _restore(Session session) async {
    if (!_mutating.add(session.sessionId)) return;
    setState(() {});
    try {
      final restored = await widget.apiClient.restoreSession(
        sessionId: session.sessionId,
      );
      if (!mounted) return;
      setState(() {
        _sessions = _sessions
            .where((candidate) => candidate.sessionId != session.sessionId)
            .toList();
      });
      widget.onRestored(restored);
    } catch (error) {
      if (mounted) _showError('Could not restore this chat: $error');
    } finally {
      _mutating.remove(session.sessionId);
      if (mounted) setState(() {});
    }
  }

  Future<void> _rename(Session session) async {
    if (!_mutating.add(session.sessionId)) return;
    setState(() {});
    try {
      final title = await _showRenameSessionDialog(context, session);
      if (title == null) return;
      final renamed = await widget.apiClient.renameSession(
        sessionId: session.sessionId,
        title: title,
      );
      if (!mounted) return;
      setState(() {
        final index = _sessions.indexWhere(
          (candidate) => candidate.sessionId == session.sessionId,
        );
        if (index >= 0) {
          if (renamed.status == SessionStatus.archived) {
            _sessions[index] = renamed;
          } else {
            _sessions.removeAt(index);
          }
        }
        _sessions.sort(_ResponsiveShellState._compareSessionsByRecency);
      });
    } catch (error) {
      if (mounted) _showError('Could not rename this chat: $error');
    } finally {
      _mutating.remove(session.sessionId);
      if (mounted) setState(() {});
    }
  }

  Future<void> _delete(Session session) async {
    if (!_mutating.add(session.sessionId)) return;
    setState(() {});
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
        _sessions = _sessions
            .where((candidate) => candidate.sessionId != session.sessionId)
            .toList();
      });
      widget.onDeleted(session.sessionId);
    } catch (error) {
      if (mounted) _showError('Could not delete this chat: $error');
    } finally {
      _mutating.remove(session.sessionId);
      if (mounted) setState(() {});
    }
  }

  void _showError(String message) {
    ScaffoldMessenger.of(
      context,
    ).showSnackBar(SnackBar(content: Text(message)));
  }

  @override
  Widget build(BuildContext context) {
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_error != null && _sessions.isEmpty) {
      return Center(
        child: FilledButton(onPressed: _load, child: const Text('Retry')),
      );
    }
    if (_sessions.isEmpty) {
      return const Center(child: Text('No archived conversations.'));
    }
    return ListView(
      children: [
        for (final session in _sessions)
          ListTile(
            title: Text(sessionDisplayTitle(session)),
            trailing: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                IconButton(
                  tooltip: 'Rename archived chat',
                  onPressed: _mutating.contains(session.sessionId)
                      ? null
                      : () => _rename(session),
                  icon: const Icon(Icons.edit_outlined),
                ),
                IconButton(
                  tooltip: 'Restore chat',
                  onPressed: _mutating.contains(session.sessionId)
                      ? null
                      : () => _restore(session),
                  icon: const Icon(Icons.unarchive_outlined),
                ),
                IconButton(
                  tooltip: 'Delete archived chat',
                  onPressed: _mutating.contains(session.sessionId)
                      ? null
                      : () => _delete(session),
                  icon: const Icon(Icons.delete_outline),
                ),
              ],
            ),
          ),
        if (_nextCursor != null)
          TextButton(
            onPressed: _loadingMore ? null : () => _load(cursor: _nextCursor),
            child: Text(_loadingMore ? 'Loading...' : 'Load more'),
          ),
      ],
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
  const _SessionSnapshot({
    required this.session,
    required this.revision,
    required this.retainUntilObserved,
  });

  final Session session;
  final int revision;
  final bool retainUntilObserved;
}
