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

/// A queued [_Semaphore.acquire] call, paired with the predicate that
/// determines whether it's still worth granting a permit to. [completer]
/// resolves to whether a permit was actually handed to this waiter: `true`
/// once granted (the caller must eventually call [_Semaphore.release]),
/// `false` if it was discarded as stale before a permit was ever consumed
/// (the caller must not release, since it never held one).
class _SemaphoreWaiter {
  _SemaphoreWaiter(this.isValid);

  final bool Function() isValid;
  final completer = Completer<bool>();
}

/// Caps concurrent access to a resource across the screen's whole lifetime,
/// not just within a single search generation. Overlapping generations
/// (e.g. a query typed before the previous one's title lookups finished)
/// share the same pool of [_maxConcurrentTitleLookups] slots instead of each
/// getting their own, so stale and current work never combine into more
/// than the intended cap of in-flight requests.
///
/// Queued waiters carry a validity predicate so a freed permit always goes
/// to the first still-current waiter. [release] discards any stale queued
/// waiters itself (completing them with `false`, consuming no permit)
/// instead of handing them the permit and relying on each one to notice
/// it's stale and release again in turn: with plain FIFO handoff, a stale
/// query with many queued lookups would occupy that release-then-hand-off
/// chain ahead of a newer generation's own queued lookup, delaying it
/// behind every stale entry instead of being scheduled directly.
class _Semaphore {
  _Semaphore(this._availablePermits);

  int _availablePermits;
  final _waiters = <_SemaphoreWaiter>[];

  /// Resolves to whether a permit was granted. Returns `false` immediately,
  /// without ever queueing, if [isValid] already fails at call time.
  Future<bool> acquire({required bool Function() isValid}) {
    if (!isValid()) return Future.value(false);
    if (_availablePermits > 0) {
      _availablePermits--;
      return Future.value(true);
    }
    final waiter = _SemaphoreWaiter(isValid);
    _waiters.add(waiter);
    return waiter.completer.future;
  }

  /// Frees a permit. Scans queued waiters oldest-first, discarding any
  /// whose [_SemaphoreWaiter.isValid] now fails (completing them with
  /// `false`, no permit consumed) until it finds one that's still current
  /// and hands it the freed permit, or the queue is exhausted and the
  /// permit becomes available for a future [acquire].
  void release() {
    while (_waiters.isNotEmpty) {
      final waiter = _waiters.removeAt(0);
      if (waiter.isValid()) {
        waiter.completer.complete(true);
        return;
      }
      waiter.completer.complete(false);
    }
    _availablePermits++;
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

  /// Whether a search has actually completed successfully for the *current*
  /// [_query]. Distinguishes "ran and truly found nothing" from "hasn't run
  /// yet" (still debouncing, or invalidated by a newer/edited query), so the
  /// zero-results copy is only ever shown for a real, current, empty result
  /// set. Reset whenever the query changes/submits or the screen resets to
  /// initial; set only when [_runSearch] completes successfully for the
  /// still-current generation.
  bool _hasCompletedSearch = false;

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
    // fire; prior results stay visible until fresh ones replace them. No
    // search has completed for this newly edited query yet, so also clear
    // that flag: otherwise a still-empty `_groups` left over from before
    // would be mistaken for a real, current zero-hit result during the
    // debounce window.
    setState(() {
      _loading = false;
      _error = null;
      _hasCompletedSearch = false;
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
    _hasCompletedSearch = false;
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
      _hasCompletedSearch = false;
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
      _hasCompletedSearch = true;
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
  /// discovered before queueing for a slot, while waiting (in which case
  /// [_Semaphore.release] discards it directly, without ever granting it
  /// the slot), or after being granted a slot, so queued stale work never
  /// occupies or delays a slot that a current lookup could use instead.
  Future<void> _lookupTitle(String sessionId, int generation) async {
    bool isValid() => mounted && generation == _generation;
    if (!isValid()) return;
    final acquired = await _titleLookupLimiter.acquire(isValid: isValid);
    if (!acquired) return;
    try {
      if (!isValid()) return;
      final session = await widget.apiClient.getSession(
        sessionId: sessionId,
      );
      if (!isValid()) return;
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
      if (!_hasCompletedSearch) {
        // No search has completed for the current query yet (still
        // debouncing after an edit, or a prior error/results were just
        // invalidated). There's nothing to report either way yet, so render
        // nothing rather than falsely claiming a completed, zero-hit search.
        return const SizedBox.shrink(key: Key('search-pending'));
      }
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
