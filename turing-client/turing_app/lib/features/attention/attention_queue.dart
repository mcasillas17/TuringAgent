import 'dart:async';

import 'package:flutter/foundation.dart';

import '../../models/attention.dart';
import '../../networking/api_client.dart';

/// What the app knows about the Needs-you queue: the rows of the pages read in
/// the current pass, and whether the latest read worked.
///
/// It holds nothing it did not just read. Every reason to read again (the
/// shell starting, the list opening, an `attention.changed` hint, the app
/// resuming, a row's action finishing) goes through [requestRefresh], which
/// starts at most one read every [minInterval] and never two at once. Nothing
/// reads on a timer of its own: without a reason, there is no read.
class AttentionQueue extends ChangeNotifier {
  AttentionQueue({
    required this.api,
    this.minInterval = const Duration(seconds: 1),
  });

  /// The server's page ceiling, so the badge counts what one page can hold.
  static const int pageSize = 100;

  final TuringApi api;
  final Duration minInterval;

  List<AttentionRow> _rows = const [];
  int _firstPageCount = 0;
  bool _firstPageHasMore = false;
  String? _nextCursor;
  int _pages = 1;
  bool _loaded = false;
  bool _failed = false;
  bool _unsupported = false;
  bool _loadingMore = false;

  bool _dirty = false;
  bool _reading = false;
  Timer? _cooldown;
  bool _disposed = false;

  /// Every row read in this pass, in the queue's order.
  List<AttentionRow> get rows => _rows;

  /// The cursor of the page after the last one read, or null on the last.
  String? get nextCursor => _nextCursor;

  /// At least one read has worked.
  bool get loaded => _loaded;

  /// The latest read failed, so the rows (if any) may be out of date.
  bool get failed => _failed;

  /// The server has no queue to read.
  bool get unsupported => _unsupported;

  bool get loadingMore => _loadingMore;

  /// A read is in flight.
  bool get reading => _reading;

  /// The first page's row count, with a "+" when a later page exists, so it
  /// always equals what the list it links to starts with, and never needs a
  /// second count query. "!" when the latest read failed and no row is known:
  /// not knowing is not the same as nothing needing you. Null when nothing
  /// needs the owner, before the first read, and when the server has no
  /// queue.
  String? get badge {
    if (_unsupported) return null;
    if (_firstPageCount > 0 || _firstPageHasMore) {
      return _firstPageHasMore ? '$_firstPageCount+' : '$_firstPageCount';
    }
    return _failed ? '!' : null;
  }

  /// Asks for a fresh first-page pass. Requests that arrive while a read is
  /// in flight, or within [minInterval] of the last read starting, are
  /// coalesced into one read as soon as both have passed.
  void requestRefresh() {
    if (_disposed) return;
    _dirty = true;
    _pump();
  }

  void _pump() {
    if (_disposed || !_dirty || _reading || _cooldown != null) return;
    _dirty = false;
    _reading = true;
    _cooldown = Timer(minInterval, () {
      _cooldown = null;
      _pump();
    });
    unawaited(_refresh());
  }

  /// Reads the same number of pages the last pass had, from a fresh first
  /// page, so a row the owner loaded more to reach does not vanish on a hint.
  /// Pages of one pass share the first page's watermark, which is what keeps
  /// them free of repeats; pages of two passes are never mixed.
  Future<void> _refresh() async {
    try {
      final first = await api.listAttention(limit: pageSize);
      final rows = [...first.rows];
      var cursor = first.nextCursor;
      var read = 1;
      while (read < _pages && cursor != null) {
        final page = await api.listAttention(cursor: cursor, limit: pageSize);
        rows.addAll(page.rows);
        cursor = page.nextCursor;
        read++;
      }
      if (_disposed) return;
      _rows = List.unmodifiable(rows);
      _firstPageCount = first.rows.length;
      _firstPageHasMore = first.nextCursor != null;
      _nextCursor = cursor;
      _pages = read;
      _loaded = true;
      _failed = false;
      _unsupported = false;
    } catch (error) {
      if (_disposed) return;
      if (_isUnsupported(error)) {
        _rows = const [];
        _firstPageCount = 0;
        _firstPageHasMore = false;
        _nextCursor = null;
        _pages = 1;
        _unsupported = true;
        _failed = false;
      } else {
        _failed = true;
      }
    } finally {
      _reading = false;
    }
    if (_disposed) return;
    notifyListeners();
    _pump();
  }

  /// Reads the page after the last one, in the same pass. It waits for no
  /// cooldown, being the owner's own request for rows not yet shown, but it
  /// still never runs beside another read.
  Future<void> loadMore() async {
    final cursor = _nextCursor;
    if (_disposed || cursor == null || _reading) return;
    _reading = true;
    _loadingMore = true;
    notifyListeners();
    try {
      final page = await api.listAttention(cursor: cursor, limit: pageSize);
      if (_disposed) return;
      _rows = List.unmodifiable([..._rows, ...page.rows]);
      _nextCursor = page.nextCursor;
      _pages++;
      _failed = false;
    } catch (_) {
      if (_disposed) return;
      _failed = true;
    } finally {
      _reading = false;
      _loadingMore = false;
    }
    if (_disposed) return;
    notifyListeners();
    _pump();
  }

  static bool _isUnsupported(Object error) =>
      error is TuringApiException &&
      (error.code == 'unimplemented' || error.code == 'attention_unsupported');

  @override
  void dispose() {
    _disposed = true;
    _cooldown?.cancel();
    _cooldown = null;
    super.dispose();
  }
}
