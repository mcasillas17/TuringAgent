import 'dart:async';

import 'package:flutter/material.dart';

import '../../models/search_hit.dart';
import '../../networking/api_client.dart';

/// Route-local conversation search. Searches all sessions for an exact
/// phrase, renders matching messages grouped by session and sorted newest
/// first, and enriches each group with the session's title without blocking
/// on that lookup.
class SearchScreen extends StatefulWidget {
  const SearchScreen({
    super.key,
    required this.apiClient,
    required this.onOpenSession,
  });

  final TuringApi apiClient;
  final Future<void> Function(String sessionId) onOpenSession;

  @override
  State<SearchScreen> createState() => _SearchScreenState();
}

/// One session's search hits, sorted newest matching message first.
class _SessionGroup {
  _SessionGroup({required this.sessionId, required this.hits});

  final String sessionId;
  final List<SearchHit> hits;
}

/// Caps concurrent access to a resource across the screen's whole lifetime,
/// not just within a single search generation. Overlapping generations
/// (e.g. a query typed before the previous one's title lookups finished)
/// share the same pool of [_maxConcurrentTitleLookups] slots instead of each
/// getting their own, so stale and current work never combine into more
/// than the intended cap of in-flight requests.
class _Semaphore {
  _Semaphore(this._availablePermits);

  int _availablePermits;
  final _waiters = <Completer<void>>[];

  Future<void> acquire() {
    if (_availablePermits > 0) {
      _availablePermits--;
      return Future.value();
    }
    final waiter = Completer<void>();
    _waiters.add(waiter);
    return waiter.future;
  }

  void release() {
    if (_waiters.isNotEmpty) {
      _waiters.removeAt(0).complete();
    } else {
      _availablePermits++;
    }
  }
}

class _SearchScreenState extends State<SearchScreen> {
  static const _debounceDuration = Duration(milliseconds: 350);
  static const _maxConcurrentTitleLookups = 4;

  final _controller = TextEditingController();
  final _focusNode = FocusNode();
  Timer? _debounce;

  /// Incremented on every changed/submitted/retried/cleared query so stale
  /// async completions (search or title) cannot mutate newer state.
  int _generation = 0;

  String _query = '';
  bool _loading = false;
  Object? _error;
  List<_SessionGroup> _groups = const [];

  /// Cached successful session titles, kept for the screen's lifetime.
  final Map<String, String> _titles = {};

  /// Global cap shared by every title lookup for the screen's lifetime, so
  /// overlapping generations can never push concurrent title RPCs past
  /// [_maxConcurrentTitleLookups] in total.
  final _titleLookupLimiter = _Semaphore(_maxConcurrentTitleLookups);

  @override
  void dispose() {
    _debounce?.cancel();
    _controller.dispose();
    _focusNode.dispose();
    super.dispose();
  }

  void _handleChanged(String value) {
    _debounce?.cancel();
    ++_generation;
    final generation = _generation;
    final query = value.trim();
    if (query.isEmpty) {
      _resetToInitial();
      return;
    }
    // The new generation invalidates whatever was in flight for the old
    // query. Clear its stale loading/error immediately so neither lingers
    // through the new debounce window while the newly typed query waits to
    // fire; prior results stay visible until fresh ones replace them.
    setState(() {
      _loading = false;
      _error = null;
    });
    _debounce = Timer(_debounceDuration, () {
      _runSearch(query, generation);
    });
  }

  void _handleSubmitted(String value) {
    _debounce?.cancel();
    final generation = ++_generation;
    final query = value.trim();
    if (query.isEmpty) {
      _resetToInitial();
      return;
    }
    _runSearch(query, generation);
  }

  void _handleClear() {
    _debounce?.cancel();
    ++_generation;
    _controller.clear();
    _resetToInitial();
  }

  void _handleRetry() {
    _debounce?.cancel();
    final generation = ++_generation;
    _runSearch(_query, generation);
  }

  void _resetToInitial() {
    setState(() {
      _query = '';
      _loading = false;
      _error = null;
      _groups = const [];
    });
  }

  Future<void> _runSearch(String query, int generation) async {
    setState(() {
      _query = query;
      _loading = true;
      _error = null;
    });

    List<SearchHit> hits;
    try {
      hits = await widget.apiClient.searchMessages(query: query, limit: 50);
    } catch (error) {
      if (!mounted || generation != _generation) return;
      setState(() {
        _loading = false;
        _error = error;
      });
      return;
    }

    if (!mounted || generation != _generation) return;
    final groups = _groupHits(hits);
    setState(() {
      _loading = false;
      _groups = groups;
    });
    // Fire-and-forget: results already rendered must not wait on titles.
    _resolveTitles(groups, generation);
  }

  List<_SessionGroup> _groupHits(List<SearchHit> hits) {
    final bySession = <String, List<SearchHit>>{};
    for (final hit in hits) {
      bySession.putIfAbsent(hit.sessionId, () => []).add(hit);
    }
    final groups = bySession.entries.map((entry) {
      final sorted = [...entry.value]
        ..sort((a, b) => b.message.createdAt.compareTo(a.message.createdAt));
      return _SessionGroup(sessionId: entry.key, hits: sorted);
    }).toList();
    groups.sort(
      (a, b) => b.hits.first.message.createdAt.compareTo(
        a.hits.first.message.createdAt,
      ),
    );
    return groups;
  }

  Future<void> _resolveTitles(
    List<_SessionGroup> groups,
    int generation,
  ) async {
    final sessionIds = groups
        .map((group) => group.sessionId)
        .where((id) => !_titles.containsKey(id))
        .toSet()
        .toList();
    if (sessionIds.isEmpty) return;

    await Future.wait(
      sessionIds.map((sessionId) => _lookupTitle(sessionId, generation)),
    );
  }

  /// Looks up a single session's title, gated by the screen-lifetime
  /// [_titleLookupLimiter] so this generation's requests share the global
  /// cap with any other generation's still-in-flight lookups. Skips the
  /// call entirely once the generation has gone stale, whether that's
  /// discovered before queueing for a slot or after (in case it went stale
  /// while waiting), so queued stale work doesn't waste a slot that a
  /// current lookup could use instead.
  Future<void> _lookupTitle(String sessionId, int generation) async {
    if (!mounted || generation != _generation) return;
    await _titleLookupLimiter.acquire();
    try {
      if (!mounted || generation != _generation) return;
      final session = await widget.apiClient.getSession(
        sessionId: sessionId,
      );
      if (!mounted || generation != _generation) return;
      setState(() {
        _titles[sessionId] = session.title?.isNotEmpty == true
            ? session.title!
            : 'Untitled chat';
      });
    } catch (_) {
      // Metadata failure is non-fatal: the ID fallback stays visible.
    } finally {
      _titleLookupLimiter.release();
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Search conversations')),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.all(16),
            child: TextField(
              key: const Key('search-field'),
              controller: _controller,
              focusNode: _focusNode,
              textInputAction: TextInputAction.search,
              onChanged: _handleChanged,
              onSubmitted: _handleSubmitted,
              decoration: InputDecoration(
                labelText: 'Search conversations',
                hintText: 'Search for an exact phrase',
                helperText:
                    'Matches the exact phrase you type, across every session.',
                prefixIcon: const Icon(Icons.search),
                border: const OutlineInputBorder(),
                suffixIcon: IconButton(
                  key: const Key('search-clear-button'),
                  tooltip: 'Clear search',
                  icon: const Icon(Icons.clear),
                  onPressed: _handleClear,
                ),
              ),
            ),
          ),
          Expanded(child: _buildBody(context)),
        ],
      ),
    );
  }

  Widget _buildBody(BuildContext context) {
    if (_query.isEmpty) {
      return const Center(
        key: Key('search-initial'),
        child: Padding(
          padding: EdgeInsets.all(24),
          child: Text(
            'Search your conversations. Type an exact phrase to find '
            'matching messages across every session.',
            textAlign: TextAlign.center,
          ),
        ),
      );
    }

    if (_loading) {
      return Center(
        child: Semantics(
          key: const Key('search-loading'),
          liveRegion: true,
          label: 'Searching conversations',
          child: const Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              CircularProgressIndicator(),
              SizedBox(height: 12),
              Text('Searching...'),
            ],
          ),
        ),
      );
    }

    if (_error != null) {
      return Center(
        child: Semantics(
          key: const Key('search-error'),
          liveRegion: true,
          label: 'Search failed: $_error',
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Text('Search failed: $_error', textAlign: TextAlign.center),
                const SizedBox(height: 12),
                ElevatedButton(
                  key: const Key('search-retry'),
                  onPressed: _handleRetry,
                  child: const Text('Retry'),
                ),
              ],
            ),
          ),
        ),
      );
    }

    if (_groups.isEmpty) {
      return const Center(
        key: Key('search-empty'),
        child: Padding(
          padding: EdgeInsets.all(24),
          child: Text(
            'No messages match this exact phrase. Try fewer or shorter words.',
            textAlign: TextAlign.center,
          ),
        ),
      );
    }

    return ListView.builder(
      itemCount: _groups.length,
      itemBuilder: (context, index) => _buildGroup(context, _groups[index]),
    );
  }

  Widget _buildGroup(BuildContext context, _SessionGroup group) {
    final title = _titles[group.sessionId] ?? 'Session ${group.sessionId}';
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 16, 16, 4),
          child: Semantics(
            key: ValueKey('group-header-${group.sessionId}'),
            header: true,
            child: Text(
              title,
              style: Theme.of(context).textTheme.titleMedium,
            ),
          ),
        ),
        for (final hit in group.hits) _buildHit(context, group.sessionId, hit),
        const Divider(height: 1),
      ],
    );
  }

  Widget _buildHit(BuildContext context, String sessionId, SearchHit hit) {
    final localDate = hit.message.createdAt.toLocal();
    final date = MaterialLocalizations.of(context).formatMediumDate(localDate);
    final role = hit.message.role;
    final content = hit.message.content;
    return Semantics(
      key: ValueKey('hit-${hit.message.messageId}'),
      label: '$role message from $date: $content',
      button: true,
      excludeSemantics: true,
      onTap: () => widget.onOpenSession(sessionId),
      child: ListTile(
        title: Text(content),
        subtitle: Text('$role · $date'),
        onTap: () => widget.onOpenSession(sessionId),
      ),
    );
  }
}
