import 'dart:async';

import 'package:flutter/material.dart';

import '../../models/search_hit.dart';
import '../../models/session_title.dart';
import '../../networking/api_client.dart';

/// Shown and announced when a search completes with no matches.
const _emptyResultsCopy =
    'No messages match this exact phrase. Try fewer or shorter words.';

/// How much of a message body a result row shows and announces.
///
/// Bodies are unbounded — a pasted log or a whole file is a normal message —
/// but a row is a summary someone skims, or hears, while deciding which
/// conversation to open. This keeps the body's opening: enough to tell one
/// hit from another, and enough to carry the matched phrase whenever it
/// appears near the start. Centring the window on the match itself isn't
/// possible here, since [SearchHit] carries the message but no match offset,
/// so a phrase buried deep in a long body is identified by its conversation,
/// date and opening rather than quoted.
const _maxExcerptRunes = 200;

/// How many lines of that excerpt a row renders before clipping.
const _maxExcerptLines = 3;

/// A bounded excerpt of [content], ellipsised when it had to be cut.
///
/// Cuts on runes rather than code units: `substring` splits surrogate pairs,
/// so an emoji (or any non-BMP character) straddling the cut would leave a
/// lone half that renders as a replacement glyph and reads as garbage. Only
/// the first [_maxExcerptRunes] + 1 runes are ever decoded, so an enormous
/// body isn't walked end to end just to discover it is too long.
String _excerpt(String content) {
  final head = content.runes.take(_maxExcerptRunes + 1).toList();
  if (head.length <= _maxExcerptRunes) return content;
  final kept = String.fromCharCodes(head.take(_maxExcerptRunes)).trimRight();
  return '$kept…';
}

/// Route-local conversation search. Searches all sessions for an exact
/// phrase, renders matching messages grouped by session and sorted newest
/// first, announces how the search ended, and enriches each group with the
/// session's title without blocking on that lookup.
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

  /// When this session's newest matching message was written, i.e. the key
  /// the group itself is ordered by.
  DateTime get newestCreatedAt => hits.first.message.createdAt;
}

/// Orders two hits from the same session newest first.
///
/// `createdAt` alone can't decide this. Backend timestamps are protobuf
/// `Timestamp`s carrying nanoseconds, but they arrive here as Dart
/// [DateTime]s, which only keep microseconds — so a user turn and the
/// assistant reply it triggered routinely collapse onto the same instant.
/// Returning 0 there would leave the pair in whatever order the backend's
/// BM25 ranking produced, making the rendered order both non-chronological
/// and dependent on the query. The session-local `sequence` is the
/// authoritative write order, so it breaks the tie; `messageId` is a last
/// resort that keeps the ordering total even for duplicate sequences.
int _compareHitsNewestFirst(SearchHit a, SearchHit b) {
  final byCreatedAt = b.message.createdAt.compareTo(a.message.createdAt);
  if (byCreatedAt != 0) return byCreatedAt;
  final bySequence = b.message.sequence.compareTo(a.message.sequence);
  if (bySequence != 0) return bySequence;
  return a.message.messageId.compareTo(b.message.messageId);
}

/// Orders two session groups by their newest matching message, newest first.
///
/// Groups can tie for the same truncation reason as [_compareHitsNewestFirst],
/// but `sequence` is session-local and says nothing about which of two
/// different sessions is newer. The session ID breaks the tie instead:
/// arbitrary, but stable, so the same result set always renders the same way
/// rather than inheriting the backend's relevance order.
int _compareGroupsNewestFirst(_SessionGroup a, _SessionGroup b) {
  final byCreatedAt = b.newestCreatedAt.compareTo(a.newestCreatedAt);
  if (byCreatedAt != 0) return byCreatedAt;
  return a.sessionId.compareTo(b.sessionId);
}

/// Caps concurrent access to a resource across the screen's whole lifetime,
/// not just within a single search generation. Overlapping generations
/// (e.g. a query typed before the previous one's title lookups finished)
/// share the same pool of [_maxConcurrentTitleLookups] permits instead of
/// each getting their own, so their work never combines into more than the
/// intended cap of in-flight requests.
///
/// Waiters are served first-in, first-out. Nothing needs to be prioritized
/// or discarded by search generation: title lookups are deduped and cached
/// per session for the screen's lifetime, so a lookup an older generation
/// started still serves whatever query is current when it finishes.
class _Semaphore {
  _Semaphore(this._availablePermits);

  int _availablePermits;
  final _waiters = <Completer<void>>[];

  /// Resolves once a permit is held. The caller must [release] exactly once.
  Future<void> acquire() {
    if (_availablePermits > 0) {
      _availablePermits--;
      return Future.value();
    }
    final waiter = Completer<void>();
    _waiters.add(waiter);
    return waiter.future;
  }

  /// Frees a permit, handing it to the longest-waiting queued caller if any,
  /// otherwise returning it to the pool for a future [acquire].
  void release() {
    if (_waiters.isNotEmpty) {
      _waiters.removeAt(0).complete();
      return;
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

  /// Incremented on every changed/submitted/retried/cleared query so a stale
  /// search completion cannot mutate newer state. Title lookups are
  /// deliberately not gated on this: they're keyed by session, not by query.
  int _generation = 0;

  /// The current trimmed query as typed, adopted as soon as the user edits
  /// the field even though its request may still be waiting out the
  /// debounce. Empty only when the field is (effectively) empty, which is
  /// what puts the screen back into its initial-guidance state.
  String _query = '';
  bool _loading = false;
  Object? _error;
  List<_SessionGroup> _groups = const [];

  /// Whether a search has actually completed successfully for the *current*
  /// [_query]. Distinguishes "ran and truly found nothing" from "hasn't run
  /// yet" (still debouncing, or invalidated by a newer/edited query), so the
  /// zero-results copy is only ever shown for a real, current, empty result
  /// set, and so the results summary only announces itself when it describes
  /// a completion rather than results left over from an earlier query. Reset
  /// whenever the query changes/submits or the screen resets to initial; set
  /// only when [_runSearch] completes successfully for the still-current
  /// generation.
  bool _hasCompletedSearch = false;

  /// How many searches have completed successfully for their still-current
  /// query. Keyed into the results/no-results live regions so each
  /// completion builds a *new* semantics node rather than reusing the
  /// previous one.
  ///
  /// Both platforms only speak a live region when its text changed or when
  /// the node is one they haven't seen before. The summary is counts-only
  /// and the no-results copy is fixed, so two searches in a row routinely
  /// produce byte-identical text; the loading state usually tears the region
  /// down in between, but a response that arrives before that frame renders
  /// (a warm cache, a local backend) leaves the node untouched and the
  /// completion unannounced. A per-completion key removes that dependency on
  /// timing. Deliberately not bumped when late title metadata arrives: that
  /// rebuild is not a new result and must stay silent.
  int _completedSearches = 0;

  /// Cached successful session titles, kept for the screen's lifetime. A
  /// title belongs to a session, not to the query that happened to surface
  /// it, so a cached entry is valid for every later query too.
  final Map<String, String> _titles = {};

  /// Lookups that are running or queued, keyed by session ID, so overlapping
  /// generations join the same request instead of issuing a duplicate one.
  /// Entries are removed once settled: successes live on in [_titles], and
  /// failures become retryable again.
  final Map<String, Future<void>> _inFlightTitleLookups = {};

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
    // query. Adopt the newly typed query as the current one right away —
    // without starting its request, which still waits out the debounce —
    // so the screen stops describing the state of a query the user has
    // already moved past. Otherwise the untouched-screen guidance would sit
    // there through the whole debounce window of the very first query.
    // Clear the old query's loading/error for the same reason; prior
    // results stay visible until fresh ones replace them. No search has
    // completed for this newly edited query yet, so also clear that flag:
    // otherwise a still-empty `_groups` left over from before would be
    // mistaken for a real, current zero-hit result during the debounce
    // window.
    setState(() {
      _query = query;
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
      _completedSearches++;
    });
    // Fire-and-forget: results already rendered must not wait on titles.
    _resolveTitles(groups);
  }

  List<_SessionGroup> _groupHits(List<SearchHit> hits) {
    final bySession = <String, List<SearchHit>>{};
    for (final hit in hits) {
      bySession.putIfAbsent(hit.sessionId, () => []).add(hit);
    }
    final groups = bySession.entries.map((entry) {
      final sorted = [...entry.value]..sort(_compareHitsNewestFirst);
      return _SessionGroup(sessionId: entry.key, hits: sorted);
    }).toList();
    groups.sort(_compareGroupsNewestFirst);
    return groups;
  }

  /// Starts (or joins) a title lookup for every session in [groups] that
  /// isn't cached yet. Deliberately not generation-scoped: a title is a
  /// property of the session, so the work is worth doing and keeping no
  /// matter which query first asked for it.
  void _resolveTitles(List<_SessionGroup> groups) {
    final sessionIds = groups
        .map((group) => group.sessionId)
        .where((id) => !_titles.containsKey(id))
        .toSet();
    for (final sessionId in sessionIds) {
      unawaited(_lookupTitle(sessionId));
    }
  }

  /// Returns the lookup already running or queued for [sessionId] when there
  /// is one, so overlapping generations share a single `getSession` call per
  /// session instead of racing duplicates against the global cap.
  Future<void> _lookupTitle(String sessionId) {
    final inFlight = _inFlightTitleLookups[sessionId];
    if (inFlight != null) return inFlight;
    final lookup = _fetchTitle(sessionId);
    _inFlightTitleLookups[sessionId] = lookup;
    return lookup;
  }

  /// Fetches one session's title, gated by the screen-lifetime
  /// [_titleLookupLimiter] so every generation's lookups share the global
  /// cap. Only mount state gates the rebuild: whichever generation started
  /// this lookup, the resolved title is the correct heading for any group
  /// currently rendering that session, and [build] reads it out of the
  /// cache by session ID.
  Future<void> _fetchTitle(String sessionId) async {
    await _titleLookupLimiter.acquire();
    try {
      // A lookup can sit in the limiter's queue for a while; if the screen
      // is gone by the time it gets a permit, there's nothing left to title,
      // so skip the request entirely.
      if (!mounted) return;
      final session = await widget.apiClient.getSession(sessionId: sessionId);
      final title = sessionDisplayTitle(session);
      if (!mounted) return;
      setState(() {
        _titles[sessionId] = title;
      });
    } catch (_) {
      // Metadata failure is non-fatal: the ID fallback stays visible, and
      // nothing is cached, so a later search or Retry can try again.
    } finally {
      _inFlightTitleLookups.remove(sessionId);
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
        // The caption below says exactly what the label announces, so the
        // whole subtree is excluded: left as semantics of its own it would
        // be traversed and spoken a second time.
        child: Semantics(
          key: const Key('search-loading'),
          liveRegion: true,
          container: true,
          excludeSemantics: true,
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
      final message = 'Search failed: $_error';
      return Center(
        child: Semantics(
          key: const Key('search-error'),
          liveRegion: true,
          container: true,
          label: message,
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                // Spoken from the label above; excluded here so the same
                // sentence isn't read twice. Excluding the whole container
                // instead would take Retry's semantics with it, leaving the
                // only way out of this state unreachable, so the exclusion
                // is scoped to the duplicated text alone.
                ExcludeSemantics(
                  child: Text(message, textAlign: TextAlign.center),
                ),
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
      // Announced like the loading and error states are: a search that ran
      // and found nothing is a completion a screen reader user has to hear
      // about, otherwise the screen just goes quiet after the "Searching"
      // announcement. The copy is spoken from the label, so the identical
      // visible text is excluded to keep it from being read twice.
      return KeyedSubtree(
        // Rebuilt per completed search: this copy is fixed, so back-to-back
        // empty searches would otherwise leave an untouched live region the
        // platform stays silent about. See [_completedSearches].
        key: ValueKey('search-empty-$_completedSearches'),
        child: Semantics(
          key: const Key('search-empty'),
          liveRegion: true,
          container: true,
          excludeSemantics: true,
          label: 'No results. $_emptyResultsCopy',
          child: const Center(
            child: Padding(
              padding: EdgeInsets.all(24),
              child: Text(_emptyResultsCopy, textAlign: TextAlign.center),
            ),
          ),
        ),
      );
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _buildResultsStatus(context),
        Expanded(
          child: ListView.builder(
            itemCount: _groups.length,
            itemBuilder: (context, index) =>
                _buildGroup(context, _groups[index]),
          ),
        ),
      ],
    );
  }

  /// The counts-only summary that both heads the result list and announces a
  /// successful search.
  ///
  /// Deliberately derived from nothing but the group and hit counts. Session
  /// titles resolve after the results render, and each arrival rebuilds this
  /// screen; a summary that mentioned them would change then, and a changed
  /// live region is re-announced — reporting late metadata as if it were a
  /// whole new search result. Counts only change when results actually do.
  String get _resultsSummary {
    final results = _groups.fold<int>(
      0,
      (total, group) => total + group.hits.length,
    );
    final conversations = _groups.length;
    return '$results ${results == 1 ? 'result' : 'results'} in '
        '$conversations '
        '${conversations == 1 ? 'conversation' : 'conversations'}';
  }

  Widget _buildResultsStatus(BuildContext context) {
    final summary = _resultsSummary;
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 0),
      child: KeyedSubtree(
        // Rebuilt per completed search: counts repeat, so two searches in a
        // row often produce the same summary and only a node the platform
        // hasn't seen before gets spoken. See [_completedSearches].
        key: ValueKey('search-results-announcement-$_completedSearches'),
        child: Semantics(
          key: const Key('search-results-status'),
          // Live only while these counts describe a search that actually
          // completed for the query as it now stands. Results outlive the
          // query they came from — they stay on screen through the next
          // query's debounce so the screen isn't blanked mid-typing — and a
          // keystroke landing while that query is in flight drops the loading
          // state, putting them back on screen. That rebuilds this node from
          // scratch (loading had torn it down), and a live region the
          // platform hasn't seen before is spoken: the previous query's
          // counts, announced as though a search had just finished, while
          // none has. Staying silent costs nothing, because the completion
          // that does arrive bumps [_completedSearches] and so is announced
          // by a freshly keyed node of its own.
          liveRegion: _hasCompletedSearch,
          container: true,
          excludeSemantics: true,
          label: summary,
          child: Text(summary, style: Theme.of(context).textTheme.bodySmall),
        ),
      ),
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
            child: Text(title, style: Theme.of(context).textTheme.titleMedium),
          ),
        ),
        for (final hit in group.hits) _buildHit(context, group.sessionId, hit),
        const Divider(height: 1),
      ],
    );
  }

  Widget _buildHit(BuildContext context, String sessionId, SearchHit hit) {
    final localDate = hit.message.createdAt.toLocal();
    // Short date rather than medium: `MaterialLocalizations.formatMediumDate`
    // omits the year (e.g. "Thu, Aug 13"), which makes hits from the same
    // day of different years indistinguishable.
    final date = MaterialLocalizations.of(context).formatShortDate(localDate);
    final role = hit.message.role;
    // Bodies are unbounded, and a row is a summary of one: announce and
    // render an excerpt, not the whole message. Whatever fits stays verbatim.
    final excerpt = _excerpt(hit.message.content);
    return Semantics(
      key: ValueKey('hit-${hit.message.messageId}'),
      label: '$role message from $date: $excerpt',
      button: true,
      excludeSemantics: true,
      onTap: () => widget.onOpenSession(sessionId),
      child: ListTile(
        title: Text(
          excerpt,
          maxLines: _maxExcerptLines,
          overflow: TextOverflow.ellipsis,
        ),
        subtitle: Text('$role · $date'),
        onTap: () => widget.onOpenSession(sessionId),
      ),
    );
  }
}
