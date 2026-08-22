import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter_markdown/flutter_markdown.dart';
import 'package:grpc/grpc.dart' show GrpcError, StatusCode;

import '../../constants/app_colors.dart';
import '../../models/message.dart';
import '../../models/remote_egress.dart';
import '../../models/run_lifecycle.dart';
import '../../models/run_state.dart';
import '../../models/turing_event.dart';
import '../../networking/api_client.dart';
import '../../networking/event_source.dart';
import '../../utils/content_presence.dart';
import '../approvals/approval_card.dart';
import 'message_send_failure_card.dart';
import 'message_send_unconfirmed_card.dart';
import 'run_cancelled_card.dart';
import 'run_failure_card.dart';
import 'run_notice_card.dart';
import 'remote_egress_dialog.dart';
import 'run_state_card.dart';
import 'run_state_reconciler.dart';
import 'tool_call_card.dart';

final Random _sendIdempotencyRandom = Random.secure();

String _newSendIdempotencyKey() {
  final bytes = List<int>.generate(
    16,
    (_) => _sendIdempotencyRandom.nextInt(256),
  );
  return 'send-${bytes.map((byte) => byte.toRadixString(16).padLeft(2, '0')).join()}';
}

class ChatScreen extends StatefulWidget {
  const ChatScreen({
    super.key,
    required this.sessionId,
    required this.apiClient,
    required this.eventSource,
    this.embedded = false,
    this.modelProvider = 'ollama',
    this.onSessionUpdated,
    this.onSessionDeleted,
  });

  final String sessionId;
  final TuringApi apiClient;
  final TuringEventSource eventSource;

  /// When hosted inside the shell there is already a Scaffold and the
  /// conversation is the whole right-hand pane, so it must not nest another
  /// Scaffold or repeat an app bar.
  final bool embedded;

  /// Chosen once in Settings rather than shown above every conversation.
  final String modelProvider;

  /// Delivers durable session metadata changes to the shell that owns the
  /// conversation list. The event payload is authoritative, so the shell does
  /// not need to poll after a send.
  final ValueChanged<TuringEvent>? onSessionUpdated;
  final ValueChanged<String>? onSessionDeleted;

  @override
  State<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends State<ChatScreen> {
  /// The event source this screen actually subscribed to, captured once so
  /// that connect and close always refer to the same instance.
  late final TuringEventSource _eventSource = widget.eventSource;

  final _controller = TextEditingController();
  final _scrollController = ScrollController();
  final List<_ChatEntry> _messages = [];
  final Map<String, _MessageEntry> _assistantEntries = {};
  final Map<String, _ToolCallEntry> _toolEntries = {};
  final List<_PendingApproval> _approvals = [];
  final Set<String> _completedHistoryRunIds = {};

  /// One accepted [RunState] per run ID, reconciled from every message page
  /// and every live/replayed event alike — the design's "history and live
  /// events use this same path" requirement. See `run_state_reconciler.dart`.
  final RunStateReconciler _runStateReconciler = RunStateReconciler();

  /// The adjacent [_RunStateCardEntry] currently rendered for each run ID
  /// that wants one, per [_wantsAdjacentCard]. Removed once a run no longer
  /// wants a card (e.g. it completed with displayable content).
  final Map<String, _RunStateCardEntry> _runStateEntries = {};

  /// The neutral [_NoResponseCardEntry] currently rendered beside an
  /// EMPTY, TUR-009-unaware historical assistant row (no [RunState] at
  /// all) — keyed by that row's own message ID. Cleared the moment a delta
  /// actually fills that row with content; see [_applyMessageDelta].
  final Map<String, _NoResponseCardEntry> _noResponseEntries = {};

  /// Bounded holding area for a [RunState] snapshot that arrives before
  /// this screen's own startup ([_initializing]) has settled. Drained, in
  /// one batch, the moment startup finishes.
  final RunStateLoadBuffer _runStateLoadBuffer = RunStateLoadBuffer();

  /// True while one coalesced newest-page resync pass is in flight.
  bool _resyncScheduled = false;

  /// Records that at least one unloaded snapshot arrived after the current
  /// resync request began. Any number of such arrivals costs one follow-up
  /// page request, so a terminal transition cannot be lost behind a stale
  /// in-flight response without degrading into one request per event.
  bool _resyncPending = false;

  /// Approval ids with an `approveApproval`/`denyApproval` RPC currently in
  /// flight — see [_approve]/[_deny]. Drives each [ApprovalCard.busy] so a
  /// second decision for the SAME approval (a duplicate tap, or Approve and
  /// Deny racing each other) is refused while the first is still
  /// unresolved, rather than firing a second, possibly contradictory RPC.
  /// Membership is keyed by approvalId, not by identity, because a
  /// [_PendingApproval] is rebuilt (not mutated in place) whenever
  /// [_addApproval] replaces one.
  final Set<String> _approvalsInFlight = {};

  /// Approval ids whose most recent `approveApproval`/`denyApproval` RPC
  /// rejected AND whose approval is still pending — see [_approve]/[_deny]'s
  /// `catch`, and [_isApprovalPending]. Unlike a run's own terminal outcome,
  /// an approval DECISION failing is not part of the message transcript, so
  /// it is surfaced as a screen-level banner (like [_streamEndedNotice])
  /// rather than an inline card: `build()` shows that banner, with the
  /// fixed, generic [_approvalActionFailedNotice] copy, whenever this set is
  /// non-empty. The copy itself is never stored per id — every failure
  /// shares the exact same safe, generic text — so this set only needs to
  /// track WHICH approvals are affected, never what to say about them.
  ///
  /// A `Set`, not a single id (round 12, finding 2): more than one
  /// [ApprovalCard] can be on screen at once, each with its own,
  /// independent decision RPC. Resolving or retrying ONE approval must
  /// never hide a notice still owed to a DIFFERENT approval also pending on
  /// screen, so an id is removed only for the approval it belongs to, by
  /// [_clearApprovalActionFailureFor] — never by clearing this whole set at
  /// once. The banner disappears only once every affected id has been
  /// removed, i.e. once this set is empty again.
  final Set<String> _approvalActionFailedApprovalIds = {};

  /// Ids of history messages whose text is already complete on screen. The
  /// stream replays the deltas that produced them, and those must not be
  /// applied a second time.
  final Set<String> _completedHistoryMessageIds = {};

  /// Sequence of the newest event already persisted when this screen opened.
  /// The subscription replays the log from its start, so every event at or
  /// below this sequence is a REPLAY of business that finished before the
  /// screen existed — see [_applyToolCall]. Null until the boundary is loaded.
  int? _replayWatermarkSequence;
  StreamSubscription<TuringEvent>? _subscription;
  String get _modelProvider => widget.modelProvider;

  /// True whenever the live subscription is not currently able to deliver —
  /// set by [_handleStreamEnded], whether reached from an `onError`, an
  /// `onDone`, or a synchronous setup failure in [_openSubscription]. This
  /// alone does NOT mean the subscription is permanently gone: `cancelOnError`
  /// is false (see [_openSubscription]), so an `onError` never ends the
  /// stream, and [_applyEvent] clears this flag the moment a further event
  /// proves the stream is still alive. [_startupFailed] is the separate,
  /// permanent flag for when nothing ever will follow.
  ///
  /// Gates the composer alongside [_initializing] and [_startupFailed] (see
  /// [_composerDisabled]) — a send while this is true could never reach the
  /// user, whether or not the drop turns out to be recoverable.
  bool _streamEnded = false;
  bool _historyLoadFailed = false;
  bool _sessionDeleted = false;

  /// True while startup is still IN PROGRESS — not yet ready, but also not
  /// yet known to have failed. The composer stays disabled, with a spinner
  /// in place of the send icon, for as long as this holds. See
  /// [_startupFailed] for the terminal counterpart: the two flags are never
  /// true at the same time (see its doc), so `build()` can tell "still
  /// starting" apart from "will never start" and show truthful copy for
  /// each, instead of showing a spinner or "Loading session..." for a
  /// failure that will never resolve.
  ///
  /// Startup only clears this flag — reaching either readiness or terminal
  /// failure — once every one of the following holds:
  ///  - the replay watermark has loaded, so [_isHistoricalRunEvent] has a
  ///    boundary to filter replay against. A message sent before this is
  ///    fixed could terminally fail or cancel fast enough that the eventual
  ///    watermark covers that very run, replay-suppressing its own live
  ///    terminal event.
  ///  - the initial history load has settled — either it succeeded, or it
  ///    failed and that failure was given its own visible handling (see
  ///    [_historyLoadFailed], which — unlike [_startupFailed] — does not
  ///    stop startup from reaching readiness). [_loadInitialMessages] seeds
  ///    history by PREPENDING (`_messages.insertAll(0, ...)`) once
  ///    `listMessages` resolves, so a live send while it is still in flight
  ///    risks a duplicate or misordered bubble: the same turn appended live,
  ///    then ALSO returned by that very `listMessages` call.
  ///  - the event subscription is open and was not already dead on arrival —
  ///    genuinely closed (`onDone`) or never opened at all, not merely
  ///    erroring once (see [_openSubscription] and [_streamEnded]) — nothing
  ///    else on this screen can carry a send's results back to the user once
  ///    that happens.
  ///
  /// A failure in either of the first or third condition above is terminal:
  /// this screen has no retry path, so it is reported by setting
  /// [_startupFailed] together with clearing this flag, never on its own.
  bool _initializing = true;

  /// True once startup has reached a TERMINAL failure: the replay watermark
  /// could not be loaded, or the event subscription itself could never be
  /// opened — either `connect()`/`listen()` threw synchronously, or the
  /// returned subscription signalled `onDone` (not merely `onError`)
  /// synchronously, before `listen()` even returned (see
  /// [_openSubscription]). A synchronous `onError` alone never sets this:
  /// `cancelOnError` is false, so an error does not end the stream, and a
  /// later event can still arrive on that very same subscription — see
  /// [_streamEnded], which covers that recoverable case instead. This screen
  /// has no retry path for a genuine terminal failure, so unlike
  /// [_historyLoadFailed] or [_streamEnded] there is no later event that
  /// clears this once set.
  ///
  /// Always set in the same update that clears [_initializing], and always
  /// alongside a call to [_handleStreamEnded], so the composer's disabled,
  /// truthful copy and the connection-lost notice ([_streamEnded] /
  /// [_streamEndedNotice]) always appear together as one consistent
  /// explanation, not two different ones for the same cause. The converse
  /// does not hold: [_handleStreamEnded] alone (a recoverable `onError`, or
  /// an async drop after startup already succeeded) sets only
  /// [_streamEnded], never this flag.
  bool _startupFailed = false;

  /// True from the moment [_sendMessage] accepts the composer's text for the
  /// CURRENT attempt until that attempt's `sendMessage` RPC resolves or
  /// rejects. Included in [_composerDisabled] so a second, overlapping
  /// attempt — a fast double-tap, or a queued `onSubmitted` racing the RPC —
  /// is refused rather than firing a second concurrent send: without this,
  /// nothing would stop two attempts' optimistic bubbles and outcomes from
  /// interleaving in an order neither [_sendMessage] nor a reader could
  /// disentangle. Cleared in both the success and failure branches of
  /// [_sendMessage] alike — whether the composer is ready to accept a NEW
  /// attempt does not depend on how the last one ended, only on whether one
  /// is still outstanding.
  bool _sending = false;
  _RetryableSend? _retryableSend;

  /// True whenever a send could never reach the user — still starting up,
  /// terminally failed to start, a live subscription that has since dropped
  /// (recoverably or not), or a previous attempt still in flight
  /// ([_sending]). Gates both the composer in `build()` and
  /// [_sendMessage]'s own defensive guard, so a queued `onSubmitted` call
  /// that bypasses the widget's `enabled`/`onPressed` gating is refused for
  /// exactly the same reasons.
  bool get _composerDisabled =>
      _initializing || _startupFailed || _streamEnded || _sending;

  @override
  void initState() {
    super.initState();
    _controller.addListener(_discardRetryForEditedText);
    unawaited(_start());
  }

  @override
  void didUpdateWidget(ChatScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.modelProvider != widget.modelProvider) {
      final retry = _retryableSend;
      if (retry != null) {
        _retryableSend = _RetryableSend(
          retry.content,
          retry.modelProvider,
          retry.idempotencyKey,
          retry.attempt,
          null,
        );
      }
    }
  }

  /// History is seeded BEFORE the subscription opens, never concurrently with
  /// it. `SubscribeSessionEvents` replays the session's persisted events from
  /// the requested sequence and only then goes live, so every `message.delta`
  /// of every earlier turn is re-delivered. Applying those while `listMessages`
  /// is still in flight would render a second copy of the conversation with no
  /// way to tell which bubbles history already covers.
  ///
  /// The load is awaited but never allowed to fail startup on its own:
  /// `listMessages` hits a backend that routinely is not up (gRPC
  /// unavailable, stale token, deadline). Letting that propagate would skip
  /// `connect()` entirely and leave a screen that receives no deltas, no tool
  /// cards and — because the drop notice is raised from the subscription —
  /// no signal at all. Degrade to "no history" instead, and say so. That
  /// holds even when startup has already failed terminally for the unrelated
  /// reason below: a transcript the backend can still serve is better than
  /// none, even though the session can never go live.
  Future<void> _start() async {
    // Snapshot the persisted event boundary before history starts loading.
    // Events committed during that RPC remain above the cursor and replay as
    // live after history is seeded.
    final replayBoundaryLoaded = await _loadReplayWatermark();
    if (!mounted) return;
    if (!replayBoundaryLoaded) {
      // No boundary means [_isHistoricalRunEvent] has nothing to filter
      // replay against, so the subscription must never open (enforced below,
      // after the history attempt this shares with the success path). Raise
      // the visible, TERMINAL failure right now instead of after
      // `_loadInitialMessages`: that unrelated, best-effort read-only load
      // must not hold the notice or the composer's truthful disabled state
      // hostage to however long — or whether ever — it resolves.
      _handleStreamEnded();
      if (!mounted) return;
      setState(() {
        _initializing = false;
        _startupFailed = true;
      });
    }
    try {
      await _loadInitialMessages();
    } catch (_) {
      // The dedup sets stay empty, so replayed deltas may re-render text the
      // server already persisted. A duplicated transcript beats a silent
      // one — true whether or not the watermark above already failed.
      if (mounted) setState(() => _historyLoadFailed = true);
    }
    if (!mounted || !replayBoundaryLoaded) return;
    if (!_openSubscription()) {
      setState(() {
        _initializing = false;
        _startupFailed = true;
      });
      return;
    }
    // Every condition in [_initializing]'s doc is met: the watermark is
    // fixed, history has settled (loaded or visibly failed) so a send cannot
    // race its own prepend, and the subscription that would carry that
    // send's events is open and was not dead on arrival.
    //
    // Drain whatever the bounded run-state buffer collected during this
    // exact window BEFORE flipping `_initializing` — a snapshot for a run
    // whose message this batch itself loaded reconciles and renders right
    // now; one for a run this screen still has no local row for (or the
    // buffer's own 65th-distinct-run overflow) is deferred to exactly one
    // coalesced resync, requested only once startup itself is done.
    final bufferOverflowed = _runStateLoadBuffer.resyncRequired;
    final buffered = _runStateLoadBuffer.drainAndClear();
    var needsResync = bufferOverflowed;
    setState(() {
      for (final state in buffered) {
        final assistantIndex = _assistantEntryIndexForRun(state.runId);
        final result = _runStateReconciler.reconcile(
          state,
          isLoaded: assistantIndex != null,
        );
        if (result.outcome == RunStateReconciliationOutcome.unloaded) {
          needsResync = true;
          continue;
        }
        if (assistantIndex == null) continue;
        if (result.isAccepted) {
          // `true`, not `false`: by this point `_loadInitialMessages` has
          // already fully seeded `_messages`, so this is exactly the same
          // "reconstructing a row already placed as part of a batch" shape
          // `_ingestMessagePage` handles — the buffered run's own row can
          // sit anywhere in that batch, not only at its current end.
          _upsertRunStateCard(
            result.current!,
            _messages[assistantIndex] as _MessageEntry,
            insertAdjacent: true,
          );
        }
      }
      _initializing = false;
    });
    if (needsResync) _requireCoalescedResync();
  }

  /// Opens the live event subscription and reports whether it is actually,
  /// permanently usable — not just whether `listen()` itself returned
  /// without throwing.
  ///
  /// [TuringEventSource] is an interface over an arbitrary `Stream`, and
  /// nothing pinned in this repo proves any particular real implementation's
  /// exact timing — only what `Stream.listen`'s own documented contract
  /// already permits. That contract allows both of the following, and this
  /// method's `try`/`catch` plus the `onDone` closure below exist to turn
  /// each into a handled, visible failure instead of an unhandled Future
  /// rejection (`_start` is fire-and-forget, `unawaited` from `initState`):
  ///  - `connect()` or `listen()` itself can throw SYNCHRONOUSLY rather than
  ///    ever producing a subscription.
  ///  - a source that is already closed can invoke `onDone` SYNCHRONOUSLY,
  ///    within `listen()` itself, before `listen()` has even returned a
  ///    subscription for this method to assign.
  ///
  /// Both are genuinely terminal — nothing further can ever arrive — so this
  /// method returns `false` for either: the throw does so directly from its
  /// `catch` below, and the sync `onDone` does so via `subscriptionClosed`,
  /// read by the final `return` once `listen()` itself has returned. A
  /// synchronous `onError` is deliberately NOT one of them:
  /// `cancelOnError` is false (see the `listen` call below), so an error
  /// never itself ends the stream, and a later event can still arrive on
  /// the very same subscription. `onError` still routes through
  /// [_handleStreamEnded] — the caller must still show the drop notice and
  /// disable the composer — but it must not set `subscriptionClosed`;
  /// [_applyEvent] clears that notice itself once a further event proves
  /// the stream is still alive, which a genuinely closed subscription never
  /// could.
  bool _openSubscription() {
    // Local, not a field: only the synchronous window inside this method's
    // own `try` matters here. An `onDone` invoked later, asynchronously,
    // still reaches `_handleStreamEnded` below, but by then this method has
    // already returned and used its result — mutating a field at that point
    // would have no reader left to observe it.
    var subscriptionClosed = false;
    try {
      _subscription = _eventSource
          .connect(
            sessionId: widget.sessionId,
            // Replay approval lifecycle events so an unresolved request is
            // reconstructed after reopening. Historical tool events remain
            // suppressed by the independently captured watermark.
            lastSequence: 0,
          )
          .listen(
            _applyEvent,
            onError: _handleStreamEnded,
            onDone: () {
              subscriptionClosed = true;
              _handleStreamEnded();
            },
          );
    } catch (error, stackTrace) {
      _handleStreamEnded(error, stackTrace);
      return false;
    }
    return !subscriptionClosed;
  }

  static const _streamEndedNotice =
      'Connection to the session lost. Reopen the session to continue.';

  static const _historyFailedNotice =
      'Earlier messages could not be loaded. This session is live from here on.';

  /// Shown instead of [_historyFailedNotice] when [_startupFailed] or
  /// [_streamEnded] holds: that copy's "This session is live from here on"
  /// claim is false whenever the stream is not CURRENTLY able to deliver,
  /// whether because no subscription will ever exist ([_startupFailed] —
  /// the replay watermark failed to load, or [_openSubscription] itself
  /// could never open one) or because one opened fine but has since gone
  /// dark, recoverably or not ([_streamEnded], set asynchronously by
  /// [_handleStreamEnded] from an `onError`/`onDone` reached after this
  /// banner may already be showing the live copy). Nothing about this
  /// session is "live" in any of those cases.
  static const _historyFailedNoSubscriptionNotice =
      'Earlier messages could not be loaded.';

  static const _loadingNotice = 'Loading session...';

  /// Shown while [_sending] holds — a `sendMessage` RPC for the current
  /// attempt is outstanding. Distinct from [_loadingNotice]: this is a
  /// per-send, transient state on an otherwise ready composer, not the
  /// screen's own startup.
  static const _sendingNotice = 'Sending...';

  /// `GrpcError` codes CONFIRMED — by the backend's own `SendMessage`
  /// handler (orchestrator-go internal/service/chat/service.go) — to occur
  /// only before it ever reaches `EnqueueUserMessage`, never after:
  ///
  ///   * [StatusCode.unauthenticated] — the stream's own auth interceptor
  ///     (internal/auth/interceptor.go, wired via `grpc.StreamInterceptor`
  ///     in internal/app/app.go) rejects the RPC before `SendMessage`'s
  ///     handler body ever runs, unconditionally.
  ///   * [StatusCode.invalidArgument] — every one of the handler's own
  ///     upfront request-validation checks (nil request; empty session_id
  ///     or content; unrecognized content_type, agent_id, or
  ///     model_provider) returns this code, and each precedes
  ///     `EnqueueUserMessage`. No other call site in the handler returns
  ///     it.
  ///   * [StatusCode.notFound] — the session lookup's `mapSessionError`
  ///     translation of `sql.ErrNoRows` ("session not found"); its only
  ///     call site, the `GetSession` check, precedes `EnqueueUserMessage`.
  ///     No other call site in the handler returns it.
  ///   * [StatusCode.alreadyExists] — an idempotency-key conflict is detected
  ///     before the enqueue transaction creates anything for this request.
  ///   * [StatusCode.failedPrecondition] — remote-egress and routing validation
  ///     either finishes before enqueue or rolls its enqueue transaction back.
  ///
  /// Deliberately excludes `StatusCode.canceled` and `StatusCode.internal`:
  /// the very same handler ALSO returns both from several points AFTER
  /// `EnqueueUserMessage`, so either one proves nothing about ordering. See
  /// [_isConfirmedPreEnqueueSendFailure].
  static const _confirmedPreEnqueueSendFailureCodes = {
    StatusCode.unauthenticated,
    StatusCode.invalidArgument,
    StatusCode.notFound,
    StatusCode.alreadyExists,
    StatusCode.failedPrecondition,
  };

  /// Whether [error] — whatever `sendMessage` rejected with, in
  /// [_sendMessage]'s own `catch` — is CONCLUSIVELY proven to have occurred
  /// before the backend's `SendMessage` handler ever reached
  /// `EnqueueUserMessage`, i.e. before the message or its run could exist
  /// server-side at all. Only a `GrpcError` whose `code` is in
  /// [_confirmedPreEnqueueSendFailureCodes] qualifies — see that constant
  /// for the exact allowlist and why each code belongs there.
  ///
  /// Everything else returns `false`, the safe default whenever this proof
  /// cannot be made: any OTHER `GrpcError` code (including
  /// `StatusCode.canceled`/`StatusCode.internal`, which the same handler
  /// also returns from points after `EnqueueUserMessage`, and any code this
  /// classifier does not recognize at all), and every `TuringApiException`
  /// unconditionally — its `code` (`api_client.dart`) is an ad-hoc,
  /// app-defined string with no contractual tie to a gRPC status, so it can
  /// never satisfy this proof, not even the real `code: 'empty_stream'`
  /// case (which only means the stream ended with no `RunQueued` seen, not
  /// that nothing was persisted). `false` here routes to
  /// [MessageSendUnconfirmedCard]; `true` routes to [MessageSendFailureCard]
  /// — see [_sendMessage]'s own `catch`.
  static bool _isConfirmedPreEnqueueSendFailure(Object error) {
    return error is GrpcError &&
        _confirmedPreEnqueueSendFailureCodes.contains(error.code);
  }

  /// Shown when `sendMessage` rejects with anything OTHER than
  /// [_isConfirmedPreEnqueueSendFailure]'s allowlist — see [_sendMessage]'s
  /// `catch`. Deliberately fixed and generic rather than derived from the
  /// thrown error: unlike [_applyRunFailed]/[_applyRunCancelled]'s own
  /// legacy fallback, which resolves only a fixed, safe `RunOutcomeReason`
  /// rather than any payload text, here there is no server-authored payload
  /// to read from at all — the thrown value is a raw
  /// `GrpcError`/[TuringApiException] never meant for display, and may
  /// carry request ids or other diagnostic detail unsafe to echo.
  ///
  /// Deliberately does NOT say "not sent" or "failed": the backend persists
  /// the enqueued message and its run BEFORE attempting to acknowledge it
  /// with a `RunQueued` event on this same stream (`SendMessage`,
  /// orchestrator-go internal/service/chat/service.go), so a rejection here
  /// can happen after the message was already durably accepted — this
  /// client simply never saw the acknowledgement. The honest state is
  /// UNKNOWN, so the copy says so and tells the user how to find out
  /// (check the conversation) rather than asserting a failure that may not
  /// have happened. For the narrow allowlist where the outcome IS known,
  /// see [_messageSendFailedNotice] instead.
  static const _messageSendUnconfirmedNotice =
      "We couldn't confirm whether this message was sent. Check the "
      'conversation before sending it again.';

  /// Shown when `sendMessage` rejects with a status
  /// [_isConfirmedPreEnqueueSendFailure] confirms occurred before
  /// `EnqueueUserMessage` — see [_sendMessage]'s `catch`. Unlike
  /// [_messageSendUnconfirmedNotice], the outcome genuinely IS known here:
  /// the message was never accepted, so this copy may truthfully say so.
  /// Still deliberately fixed and generic rather than derived from the
  /// thrown error, for the same reason as [_messageSendUnconfirmedNotice] —
  /// the value is a raw `GrpcError` never meant for display — and stays
  /// safe and actionable: it never echoes the request's own content,
  /// session id, or any other diagnostic detail, and only suggests a
  /// generic remedy that holds across all three allowlisted codes
  /// (expired/invalid auth, a rejected argument, or a missing session)
  /// rather than naming any one of them specifically.
  static const _messageSendFailedNotice =
      "Your message wasn't sent. Check your session and try again.";

  /// Shown when `approveApproval`/`denyApproval` itself rejects — see
  /// [_approve]/[_deny]'s `catch`. Deliberately fixed and generic for the
  /// same reason as [_messageSendUnconfirmedNotice]: the thrown value is a
  /// raw `GrpcError`/[TuringApiException] never meant for display. Unlike
  /// that notice, this one is framed as a plain failure rather than an
  /// unconfirmed outcome: retrying a decision for the same approval can
  /// never corrupt state or apply a second, contradictory decision. The
  /// backend (orchestrator-go internal/service/approvals/service.go and
  /// internal/repository/approvals.go) guards every decision by the
  /// approval's CURRENT status: repeating the exact same decision on an
  /// approval that is still pending (or already matches, and unexpired) is
  /// a safe no-op, while a conflicting or stale decision — denying one
  /// already approved, or deciding one already expired or consumed — fails
  /// outright instead of silently applying. So a retry is never a
  /// duplicated action the way resending a chat message could be, even
  /// though not every retry is guaranteed to be a silent no-op.
  ///
  /// Exactly one, fixed string, shared by every approval currently in
  /// [_approvalActionFailedApprovalIds] (round 12, finding 2): the banner in
  /// `build()` never varies this copy per id, so two DIFFERENT approvals
  /// failing at once still show this SAME text, just for longer — until
  /// every affected id clears.
  static const _approvalActionFailedNotice =
      'Could not send your decision. Please try again.';

  /// The event stream is the only source of terminal `tool.call.*` events. On
  /// `onDone`, or a subscription that never opened, no further event can ever
  /// arrive on it, so any card still in [ToolCallStatus.running] genuinely can
  /// never resolve. On `onError` that is not guaranteed — `cancelOnError` is
  /// false (see [_openSubscription]), so the stream can keep delivering after
  /// an error — but a card may still never see a real terminal event, so mark
  /// it pessimistically with the same 'connection lost' placeholder; a later
  /// terminal event for that call replaces it via [_applyToolCall] if the
  /// stream recovers. Either way, left alone the card would spin forever and
  /// tell the user a tool is still executing. Already-terminal cards are
  /// untouched.
  ///
  /// Resolving cards is not enough on its own: usually no tool call is in
  /// flight when the stream drops, and [TuringEventSource] never reconnects, so
  /// the screen would go permanently silent with no user-visible signal. Raise
  /// a persistent notice too.
  void _handleStreamEnded([Object? error, StackTrace? stackTrace]) {
    if (!mounted) return;
    for (final entry in _toolEntries.values) {
      final current = entry.state.value;
      if (current.status != ToolCallStatus.running) continue;
      entry.state.value = (
        toolName: current.toolName,
        status: ToolCallStatus.failed,
        error: 'connection lost',
        serverName: current.serverName,
      );
    }
    setState(() => _streamEnded = true);
  }

  /// Payloads arrive as a `Map<String, dynamic>` decoded from a proto Struct,
  /// so a producer bug can put any type behind a contract key. An `as String?`
  /// cast would throw a `TypeError` out of the listener and take the whole
  /// subscription (and therefore every later event) down with it.
  static String? _asString(Object? value) => value is String ? value : null;

  /// Fallback label for a card whose events have not yet carried a usable
  /// `toolName`. Doubles as the "still unresolved" marker so a later event can
  /// heal the card (see [_applyToolCall]).
  static const _placeholderToolName = 'tool';

  /// Reads the already-persisted event page and its authoritative latest
  /// sequence so [_applyToolCall] can tell a replayed event from a live one.
  /// Sequence is exact and free of clock skew; message timestamps are not
  /// usable for this — the backend stamps a turn's rows at enqueue time and
  /// never restamps them, so a finished turn's messages are stamped BEFORE its
  /// own tool events.
  ///
  /// Without this boundary a replayed tool event is indistinguishable from a
  /// live one, so startup must not open the event stream on failure.
  Future<bool> _loadReplayWatermark() async {
    final TuringEventPage page;
    try {
      page = await widget.apiClient.listEvents(sessionId: widget.sessionId);
    } catch (_) {
      return false;
    }
    if (!mounted) return false;
    // `latestSequence` covers persisted rows beyond the 500-event response
    // page. Taking the max also keeps alternate API implementations honest.
    var watermark = page.latestSequence;
    for (final event in page.events) {
      if (event.sequence > watermark) watermark = event.sequence;
    }
    _replayWatermarkSequence = watermark;
    return true;
  }

  Future<void> _loadInitialMessages() async {
    final messages = await widget.apiClient.listMessages(
      sessionId: widget.sessionId,
    );
    if (!mounted || messages.isEmpty) return;
    setState(() => _ingestMessagePage(messages, isInitialLoad: true));
    _scrollToBottom();
  }

  /// Applies one page of message rows — either the very first history load
  /// ([isInitialLoad] true, prepended above any live entries as one block)
  /// or a later coalesced newest-page resync ([isInitialLoad] false,
  /// appended) — as a single unit: every row's message ID is deduplicated
  /// against what is already on screen, and every row's own [RunState] (if
  /// any) is independently reconciled and rendered beside that row.
  ///
  /// Must be called from inside a `setState`.
  void _ingestMessagePage(
    List<Message> messages, {
    required bool isInitialLoad,
  }) {
    if (messages.isEmpty) return;
    _adoptPersistedUserMessageIdentities(messages);
    final alreadyLoadedMessageIds = _messages
        .whereType<_MessageEntry>()
        .map((entry) => entry.messageId)
        .toSet();
    final loadedRunIds = {
      ..._messages
          .whereType<_MessageEntry>()
          .where((entry) => !entry.isUser && entry.runId != null)
          .map((entry) => entry.runId!),
      ...messages
          .where(
            (message) => message.role == 'assistant' && message.runId != null,
          )
          .map((message) => message.runId!),
    };
    final pageResults = reconcilePage(
      _runStateReconciler,
      messages.map(
        (message) => MessagePageEntry(
          messageId: message.messageId,
          runState: message.runState,
        ),
      ),
      alreadyLoadedMessageIds,
      loadedRunIds: loadedRunIds,
    );
    final prepend = <_MessageEntry>[];
    final pendingNoResponse = <_MessageEntry>[];
    for (var i = 0; i < messages.length; i++) {
      final message = messages[i];
      final pageResult = pageResults[i];
      if (pageResult.isDuplicateMessage) {
        // Already rendered by an earlier page/live delta — dedup by message
        // ID: never a second bubble for the same row. Its own run state, if
        // this page carries one, is still reconciled below regardless — an
        // overlapping page load must not suppress a real state advance just
        // because the ROW itself is a repeat. The row's CONTENT can still
        // legitimately change across an overlapping load though: a run
        // that was still streaming (adopted empty) on an earlier page can
        // have finished by the time a later page reports it, with no live
        // `message.delta` for this exact client to have caught that on its
        // own (e.g. the run advanced entirely between page loads). Sync
        // the authoritative text in place rather than leaving a stale
        // empty/partial bubble beside a now-terminal card.
        final existingEntries = _messageEntries(message.messageId);
        final existing = existingEntries.first;
        if (message.role == 'assistant' && message.runId != null) {
          for (final entry in existingEntries) {
            entry.runId ??= message.runId;
          }
        }
        final stateOutcome = pageResult.stateResult?.outcome;
        final canApplyPersistedContent =
            stateOutcome != RunStateReconciliationOutcome.stale &&
            stateOutcome != RunStateReconciliationOutcome.inconsistent;
        if (!existing.isUser && canApplyPersistedContent) {
          // Persisted assistant content is authoritative once it is
          // displayable. Before terminalization, however, the durable row is
          // still the empty enqueue placeholder while live deltas may already
          // have filled the on-screen entry. Never let that stale placeholder
          // erase partial text the user has already seen.
          if (hasDisplayableContent(message.content)) {
            existing.content.value = message.content;
            for (final trailing in existingEntries.skip(1)) {
              trailing.content.value = '';
            }
          } else if (!existingEntries.any(
            (entry) => hasDisplayableContent(entry.content.value),
          )) {
            existing.content.value = message.content;
          }
          if (hasDisplayableContent(message.content)) {
            final noResponseCard = _noResponseEntries.remove(message.messageId);
            if (noResponseCard != null) {
              _messages.remove(noResponseCard);
              noResponseCard.dispose();
            }
            _assistantEntries.remove(message.messageId);
            _completedHistoryMessageIds.add(message.messageId);
            final runId = message.runId;
            if (message.role == 'assistant' && runId != null) {
              _completedHistoryRunIds.add(runId);
            }
          }
        }
        final runId = message.runId;
        if (runId != null && pageResult.stateResult?.isAccepted != true) {
          final state = _runStateReconciler.stateFor(runId);
          if (state != null) {
            _syncRunStateCardPresenceForContent(state, insertAdjacent: true);
          }
        }
        continue;
      }
      final entry = _MessageEntry.fromMessage(message);
      if (isInitialLoad) {
        prepend.add(entry);
      } else if (_historyLoadFailed) {
        _insertRecoveredHistoryEntry(entry, messages, i);
      } else {
        _messages.add(entry);
      }
      if (!entry.isUser && !hasDisplayableContent(message.content)) {
        // An assistant row with no content is a turn that is still
        // streaming: the row is inserted empty when the job is created.
        // Adopt it as the live bubble so replayed and live deltas land IN
        // it rather than opening a duplicate bubble underneath.
        _assistantEntries[message.messageId] = entry;
        if (message.runState == null) {
          // No canonical state at all for this empty row: this app cannot
          // tell running from abandoned from anything else, so it must not
          // guess. Show the neutral, terminal-only legacy fallback instead
          // of silence — cleared the moment real content actually arrives
          // (see `_applyMessageDelta`).
          pendingNoResponse.add(entry);
        }
      } else {
        _completedHistoryMessageIds.add(message.messageId);
        final runId = message.runId;
        if (message.role == 'assistant' && runId != null) {
          _completedHistoryRunIds.add(runId);
        }
      }
    }
    // Seed history non-destructively: prepend it above any live entries. A
    // destructive clear+addAll here would leak their notifiers and orphan
    // the correlation maps (`_toolEntries` / `_assistantEntries`) so later
    // terminal events would mutate cards no widget listens to.
    if (prepend.isNotEmpty) _messages.insertAll(0, prepend);
    for (final entry in pendingNoResponse) {
      final card = _NoResponseCardEntry();
      _noResponseEntries[entry.messageId] = card;
      _messages.insert(_messages.indexOf(entry) + 1, card);
    }
    for (final result in pageResults) {
      final stateResult = result.stateResult;
      if (stateResult == null || !stateResult.isAccepted) continue;
      final state = stateResult.current!;
      final assistantIndex = _assistantEntryIndexForRun(state.runId);
      if (assistantIndex == null) continue;
      _upsertRunStateCard(
        state,
        _messages[assistantIndex] as _MessageEntry,
        insertAdjacent: true,
      );
    }
  }

  /// Inserts one newly recovered row according to the durable page order
  /// without replacing live rows, send attempts, tool cards, or state truth.
  ///
  /// The next page row that is already present is a stable insertion anchor.
  /// If none exists, this row belongs after everything currently visible.
  void _insertRecoveredHistoryEntry(
    _MessageEntry entry,
    List<Message> page,
    int pageIndex,
  ) {
    for (var i = pageIndex + 1; i < page.length; i++) {
      final nextIndex = _messageEntryIndex(page[i].messageId);
      if (nextIndex != null) {
        _messages.insert(nextIndex, entry);
        return;
      }
    }
    _messages.add(entry);
  }

  /// Replaces a pending optimistic user's local identity with the durable
  /// message/run identity returned by a newest-page resync.
  ///
  /// A queued run-state event can reach the session subscription before the
  /// independent SendMessage RPC future resolves. That event has no loaded
  /// assistant row yet, so it correctly triggers a resync. The page's user
  /// row is the durable form of the optimistic bubble already on screen; it
  /// must adopt that bubble rather than render a second copy.
  void _adoptPersistedUserMessageIdentities(List<Message> messages) {
    final runIdByUserMessageId = <String, String>{};
    for (final message in messages) {
      final state = message.runState;
      if (state != null && state.userMessageId.isNotEmpty) {
        runIdByUserMessageId[state.userMessageId] = state.runId;
      }
    }
    for (final message in messages) {
      final runId = runIdByUserMessageId[message.messageId];
      if (message.role != 'user' ||
          runId == null ||
          _messageEntryIndex(message.messageId) != null) {
        continue;
      }
      final candidates = _messages.whereType<_MessageEntry>().where(
        (entry) =>
            entry.isUser &&
            entry.canAdoptPersistedIdentity &&
            entry.content.value == message.content &&
            (entry.runId == null || entry.runId == runId),
      );
      _MessageEntry? candidate;
      for (final entry in candidates) {
        if (entry.runId == runId) {
          candidate = entry;
          break;
        }
        candidate ??= entry;
      }
      if (candidate == null) continue;
      candidate.messageId = message.messageId;
      candidate.runId = runId;
      candidate.canAdoptPersistedIdentity = false;
      candidate.hasAdoptedPersistedIdentity = true;
      // Adopting a durable row disproves this attempt's unconfirmed-send
      // warning even if the user has since edited the retry draft (which
      // independently clears `_retryableSend`).
      _removeSendOutcomeAfter(candidate);
      final retry = _retryableSend;
      if (retry != null && identical(retry.attempt, candidate)) {
        // The page has now supplied the durable identity/correlation that the
        // rejected RPC could not confirm. The warning and retry draft are
        // stale; keep only the one adopted timeline bubble.
        _retryableSend = null;
        if (_controller.text.trim() == retry.content) _controller.clear();
      }
    }
  }

  /// Handles a [RunState] snapshot from a live or replayed event —
  /// reconciled at the very top of [_applyEvent], before its type-specific
  /// switch. During the narrow startup window before [_initializing]
  /// settles, snapshots are held in [_runStateLoadBuffer] instead of acted
  /// on immediately (see [_start]'s drain).
  void _handleIncomingRunState(
    RunState incoming, {
    required bool insertAdjacent,
  }) {
    if (_initializing) {
      _runStateLoadBuffer.offer(incoming);
      return;
    }
    final assistantIndex = _assistantEntryIndexForRun(incoming.runId);
    final result = _runStateReconciler.reconcile(
      incoming,
      isLoaded: assistantIndex != null,
    );
    if (result.outcome == RunStateReconciliationOutcome.unloaded) {
      // No local row for this run at all: reconciling it now would let the
      // reconciler accept a state this screen can never actually SHOW,
      // silently desynchronizing it from what is on screen. Discard the
      // event and ask for exactly one coalesced resync instead — the next
      // newest-page load either brings this run's row along (and its
      // state reconciles normally then) or still doesn't, in which case
      // nothing was lost by not guessing here.
      _requireCoalescedResync();
      return;
    }
    if (assistantIndex == null) return;
    if (!result.isAccepted) return;
    _upsertRunStateCard(
      result.current!,
      _messages[assistantIndex] as _MessageEntry,
      insertAdjacent: insertAdjacent,
    );
  }

  /// Requests exactly one coalesced newest-page reload if none is already
  /// scheduled or in flight — see [_resyncScheduled]'s own doc.
  void _requireCoalescedResync() {
    if (_resyncScheduled) {
      _resyncPending = true;
      return;
    }
    _resyncScheduled = true;
    unawaited(_runCoalescedResync());
  }

  Future<void> _runCoalescedResync() async {
    try {
      do {
        _resyncPending = false;
        await _reloadNewestPage();
      } while (mounted && _resyncPending);
    } finally {
      _resyncScheduled = false;
      _resyncPending = false;
    }
  }

  /// Best-effort: reuses the same `listMessages` call [_loadInitialMessages]
  /// makes for its own first load, rather than adding a second pagination
  /// mechanism. A failure here is silent and non-fatal — nothing was ever
  /// promised to the user for a run this screen could not otherwise show,
  /// so the composer/notice state this screen already has is untouched;
  /// a later resync attempt (or a live event that DOES have a local row)
  /// can still succeed independently.
  Future<void> _reloadNewestPage() async {
    final List<Message> messages;
    try {
      messages = await widget.apiClient.listMessages(
        sessionId: widget.sessionId,
      );
    } catch (_) {
      return;
    }
    if (!mounted) return;
    // An empty successful response cannot prove that the failed initial
    // projection was empty. Keep replayed content and the warning intact.
    if (messages.isEmpty) return;
    setState(() {
      _ingestMessagePage(messages, isInitialLoad: false);
      _historyLoadFailed = false;
    });
    _scrollToBottom();
  }

  /// The index of the [_MessageEntry] for [messageId], or null if no such
  /// row is currently on screen.
  int? _messageEntryIndex(String messageId) {
    for (var i = 0; i < _messages.length; i++) {
      final entry = _messages[i];
      if (entry is _MessageEntry && entry.messageId == messageId) return i;
    }
    return null;
  }

  List<_MessageEntry> _messageEntries(String messageId) => _messages
      .whereType<_MessageEntry>()
      .where((entry) => entry.messageId == messageId)
      .toList(growable: false);

  /// The index of the ASSISTANT [_MessageEntry] belonging to [runId], or
  /// null if this screen has no local row for that run at all. This is the
  /// one place a run-state card's position, and its very eligibility to be
  /// rendered at all, is decided — never beside a user bubble, and never
  /// detached with no row at all.
  int? _assistantEntryIndexForRun(String runId) {
    for (var i = _messages.length - 1; i >= 0; i--) {
      final entry = _messages[i];
      if (entry is _MessageEntry && !entry.isUser && entry.runId == runId) {
        return i;
      }
    }
    return null;
  }

  /// Whether [state] should show its own adjacent card right now, per the
  /// design's timeline-rendering matrix:
  ///
  ///  - failed/cancelled: always — the matching card renders regardless of
  ///    whether displayable content also survived (the bubble itself
  ///    already renders or not, independently, via [_MessageBubble]'s own
  ///    empty-content guard);
  ///  - completed: when canonical content does not exist, or while content
  ///    that canonically exists has not reached this screen yet;
  ///  - queued/running/waiting approval/recovering/unspecified/unknown:
  ///    only while [liveHasContent] is false — computed from what this
  ///    screen can currently see in the row itself, since a nonterminal
  ///    `RunState` carries no completed-message content of its own to
  ///    trust either way.
  bool _wantsAdjacentCard(RunState state, {required bool liveHasContent}) {
    switch (state.lifecycle) {
      case RunLifecycle.failed:
      case RunLifecycle.cancelled:
        return true;
      case RunLifecycle.completed:
        return !state.hasDisplayableContent || !liveHasContent;
      case RunLifecycle.queued:
      case RunLifecycle.running:
      case RunLifecycle.waitingApproval:
      case RunLifecycle.recovering:
      case RunLifecycle.unspecified:
      case RunLifecycle.unknown:
        return !liveHasContent;
    }
  }

  /// Creates, updates, or removes [state.runId]'s ONE adjacent
  /// [_RunStateCardEntry] beside [assistantEntry] — never a second one, and
  /// never a detached one. [insertAdjacent] places a page-sourced card
  /// immediately beside the page row. A live card instead follows any
  /// contiguous tool/notice artifacts for that run, but never crosses into a
  /// later turn merely because its state update arrived late.
  ///
  /// Must be called from inside a `setState`.
  void _upsertRunStateCard(
    RunState state,
    _MessageEntry assistantEntry, {
    required bool insertAdjacent,
  }) {
    // A genuine, reconciled `RunState` is now known for this exact row —
    // any neutral "no response recorded" fallback beside it (rendered
    // because the row was originally loaded with no `RunState` at all; see
    // `_ingestMessagePage`) would now misstate what this app actually
    // knows, whether or not the reconciled state itself wants its own
    // adjacent card below.
    final staleNoResponseCard = _noResponseEntries.remove(
      assistantEntry.messageId,
    );
    if (staleNoResponseCard != null) {
      _messages.remove(staleNoResponseCard);
      staleNoResponseCard.dispose();
    }
    final liveHasContent = _runHasDisplayableContent(state.runId);
    final wantsCard = _wantsAdjacentCard(state, liveHasContent: liveHasContent);
    final responseContentUnavailable =
        state.lifecycle == RunLifecycle.completed &&
        state.hasDisplayableContent &&
        !liveHasContent;
    if (responseContentUnavailable) {
      // The terminal state promises durable answer bytes this screen does not
      // have. Keep a truthful temporary completion card and coalesce a newest-
      // page reload rather than collapsing the turn to unexplained blankness.
      _requireCoalescedResync();
    }
    final existing = _runStateEntries[state.runId];
    if (!wantsCard) {
      if (existing != null) {
        _messages.remove(existing);
        _runStateEntries.remove(state.runId);
        existing.dispose();
      }
      return;
    }
    if (existing != null) {
      // Updated in place: the same one card tracks this run's state for as
      // long as it wants one, rather than a new card being created per
      // update.
      existing.state.value = state;
      existing.responseContentUnavailable = responseContentUnavailable;
      _messages.remove(existing);
      _messages.insert(
        _runStateCardInsertionIndex(
          state.runId,
          assistantEntry,
          includeRunArtifacts: !insertAdjacent,
        ),
        existing,
      );
      return;
    }
    final entry = _RunStateCardEntry(
      state,
      responseContentUnavailable: responseContentUnavailable,
    );
    _runStateEntries[state.runId] = entry;
    _messages.insert(
      _runStateCardInsertionIndex(
        state.runId,
        assistantEntry,
        includeRunArtifacts: !insertAdjacent,
      ),
      entry,
    );
  }

  int _runStateCardInsertionIndex(
    String runId,
    _MessageEntry assistantEntry, {
    required bool includeRunArtifacts,
  }) {
    var index = _messages.indexOf(assistantEntry) + 1;
    if (!includeRunArtifacts) return index;
    while (index < _messages.length) {
      final entry = _messages[index];
      final belongsToRun =
          entry is _ToolCallEntry && entry.runId == runId ||
          entry is _RunNoticeEntry && entry.runId == runId;
      if (!belongsToRun) break;
      index++;
    }
    return index;
  }

  bool _runHasDisplayableContent(String runId) =>
      _messages.whereType<_MessageEntry>().any(
        (entry) =>
            !entry.isUser &&
            entry.runId == runId &&
            hasDisplayableContent(entry.content.value),
      );

  /// Content can arrive independently of an already-reconciled state. Only
  /// mutate the timeline when that changes whether the run needs a card;
  /// otherwise streaming remains notifier-only and does not rebuild/reposition
  /// the list for every token.
  void _syncRunStateCardPresenceForContent(
    RunState state, {
    required bool insertAdjacent,
  }) {
    if (_runStateCardPresenceMatchesContent(state)) return;
    final assistantIndex = _assistantEntryIndexForRun(state.runId);
    if (assistantIndex == null) return;
    _upsertRunStateCard(
      state,
      _messages[assistantIndex] as _MessageEntry,
      insertAdjacent: insertAdjacent,
    );
  }

  bool _runStateCardPresenceMatchesContent(RunState state) {
    final wantsCard = _wantsAdjacentCard(
      state,
      liveHasContent: _runHasDisplayableContent(state.runId),
    );
    return (_runStateEntries[state.runId] != null) == wantsCard;
  }

  void _applyEvent(TuringEvent event) {
    // `dispose` cancels the subscription, but cancellation of a gRPC-backed
    // stream is asynchronous: an event already scheduled for delivery can still
    // land here after the State is gone, and every handler below calls
    // `setState`. Guard once at the entry point — it covers all four of them,
    // and mirrors [_handleStreamEnded].
    if (!mounted) return;
    if (_sessionDeleted) return;
    // Events are arriving, so any earlier drop notice is stale: an error does
    // not cancel the subscription (`cancelOnError` defaults to false), and the
    // stream can keep delivering after one.
    if (_streamEnded) setState(() => _streamEnded = false);
    // Reconcile any canonical `RunState` this event carries BEFORE the
    // type-specific switch below, regardless of the event's own type —
    // `queued`, `started`, an approval lifecycle event, `state_changed`,
    // `completed`, `failed`, and `cancelled` alike. A pre-TUR-009 (or
    // otherwise malformed) event carries no `RunState` at all and is
    // untouched by this step; its type-specific handler below falls back to
    // its own safe legacy mapping instead.
    final runState = event.runState;
    if (runState != null) {
      setState(
        () => _handleIncomingRunState(
          runState,
          insertAdjacent: _isHistoricalRunEvent(event),
        ),
      );
    }
    switch (event.type) {
      case 'message.delta':
        _applyMessageDelta(event);
        break;
      case 'session.updated':
        widget.onSessionUpdated?.call(event);
        break;
      case 'session.deleted':
        _sessionDeleted = true;
        widget.onSessionDeleted?.call(event.sessionId);
        unawaited(_subscription?.cancel());
        _eventSource.close();
        return;
      case 'agent.run.step':
        _applyRunStep(event);
        break;
      case 'agent.run.failed':
        _applyRunFailed(event);
        break;
      case 'agent.run.cancelled':
        _applyRunCancelled(event);
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
      case 'agent.run.state_changed':
        // No extra effect beyond the unconditional reconciliation above:
        // this event type carries nothing but the state snapshot itself.
        break;
      // Everything below has no case above and so is deliberately left
      // unhandled — it falls out of this switch untouched, not implicitly
      // grouped with any handled case above (in particular, not with the
      // `agent.run.failed` / `agent.run.cancelled` handling just above):
      //  - `agent.run.started` / `agent.run.queued`: surfacing them beyond
      //    the reconciliation step above would just add noise ahead of the
      //    real content.
      //  - `agent.run.completed`: its completion is already evidenced by
      //    the assistant's own answer arriving via `message.delta`, and any
      //    no-content/nonterminal-status card it needs is already handled
      //    by the reconciliation step above.
    }
  }

  /// Shown for an `agent.run.step` event whose payload carries neither a
  /// usable `note` nor a recognized `category` — a producer-neutral
  /// governed fallback for genuinely uninformative payloads. Kept
  /// unchanged from before TUR-009: this is the ONE piece of `note` text
  /// that was already fixed, safe app copy rather than backend prose.
  static const _runStepFallbackNotice =
      'The run reported a step with no description';

  void _applyRunStep(TuringEvent event) {
    if (_isHistoricalRunEvent(event)) return;

    final category = _runStepNoticeCategoryFromPayload(event.payload);
    final _ChatEntry entry;
    if (category != null) {
      // One of the three allowlisted failure-adjacent notice categories:
      // localized, bounded copy only — the payload's own `note` (if any) is
      // never read for these, so it cannot leak backend prose.
      final attempt = _runStepAttemptValue(event.payload['attempt']);
      final maxAttempts = _runStepAttemptValue(event.payload['maxAttempts']);
      if (attempt == null || maxAttempts == null || attempt > maxAttempts) {
        entry = _RunNoticeEntry.freeform(
          _runStepFallbackNotice,
          runId: event.runId,
        );
      } else {
        entry = _RunNoticeEntry.localized(
          category: category,
          attempt: attempt,
          maxAttempts: maxAttempts,
          runId: event.runId,
        );
      }
    } else {
      // Preserved governed nonfailure notice: the backend's own `note` text
      // for redacted egress/model-limit and similar approved payloads
      // continues to render verbatim, exactly as before.
      final rawNote = _asString(event.payload['note'])?.trim();
      final note = (rawNote == null || rawNote.isEmpty)
          ? _runStepFallbackNotice
          : rawNote;
      entry = _RunNoticeEntry.freeform(note, runId: event.runId);
    }
    setState(() => _messages.add(entry));
    _scrollToBottom();
  }

  /// Safely classifies an `agent.run.step` payload's own `category`
  /// field into one of the three allowlisted, LOCALIZED failure-adjacent
  /// notice categories — never trusting the raw string itself as display
  /// copy. Absent, unrecognized, or malformed values all return null,
  /// routing to [_applyRunStep]'s governed nonfailure fallback instead.
  static RunStepNoticeCategory? _runStepNoticeCategoryFromPayload(
    Map<String, dynamic> payload,
  ) {
    switch (_asString(payload['category'])) {
      case 'dispatch_retry':
        return RunStepNoticeCategory.dispatchRetry;
      case 'recovery_retry':
        return RunStepNoticeCategory.recoveryRetry;
      case 'recovery_exhausted':
        return RunStepNoticeCategory.recoveryExhausted;
      default:
        return null;
    }
  }

  /// Accepts only the backend notice contract's bounded positive integers.
  /// Protobuf `Struct` numbers arrive as doubles, so exact integral doubles
  /// are valid; fractional, non-finite, string, and out-of-range values fail
  /// closed to the governed fallback in [_applyRunStep].
  static int? _runStepAttemptValue(Object? raw) {
    final int value;
    if (raw is int) {
      value = raw;
    } else if (raw is double && raw.isFinite && raw == raw.truncateToDouble()) {
      value = raw.toInt();
    } else {
      return null;
    }
    if (value < 1 || value > maxRunStepNoticeAttempts) return null;
    return value;
  }

  void _applyRunFailed(TuringEvent event) {
    if (event.runState != null) {
      // A canonical `RunState` already fully handled this run's outcome via
      // the unconditional reconciliation step at the top of [_applyEvent]
      // — its adjacent `RunStateCard` already reflects it (or the event
      // was correctly discarded/deferred as unloaded). Nothing further to
      // do for a modern event.
      return;
    }
    if (_isHistoricalRunEvent(event)) return;

    // Pre-TUR-009 legacy event with no canonical state at all: the
    // payload's own `message`/`code` are machine metadata this app must
    // never echo, and there is no safe table mapping arbitrary legacy
    // codes to a more specific outcome, so every legacy failure resolves
    // to the same truthful, generic "Run failed" copy.
    final entry = _RunFailureEntry(RunOutcomeReason.none);
    setState(() => _messages.add(entry));
    _scrollToBottom();
  }

  /// The only `reason` value the backend currently emits with
  /// `agent.run.cancelled` (`cancelRun`, orchestrator-go
  /// internal/service/chat/service.go). It fires on exactly two conditions:
  /// `SendMessage`'s own context being cancelled — checked at four
  /// checkpoints (the initial `RunQueued` send, the dispatch loop's
  /// teardown, a replay-events error, and a relayed event send) — or a
  /// `DispatchPending` failure, which cancels unconditionally regardless of
  /// context state. A bare `stream.Send` failure never triggers this by
  /// itself; it only does at those checkpoints once the context is already
  /// cancelled.
  static const _clientCancelledReason = 'client_cancelled';

  /// The event stream is the only channel `agent.run.cancelled` arrives on,
  /// and `cancelRun` fires it on exactly two conditions (see
  /// [_clientCancelledReason]): `SendMessage`'s own context is cancelled, or
  /// `DispatchPending` fails. Left unhandled, the event would fall through
  /// [_applyEvent]'s switch and leave a silent, unexplained turn on screen.
  void _applyRunCancelled(TuringEvent event) {
    if (event.runState != null) {
      // See [_applyRunFailed]'s own doc: a modern event is already fully
      // handled by the reconciliation step at the top of [_applyEvent].
      return;
    }
    if (_isHistoricalRunEvent(event)) return;

    // `reason` is machine metadata, not display copy (see
    // [_clientCancelledReason]'s own doc), so it is only ever used to
    // select a semantic `RunOutcomeReason` — never rendered verbatim. The
    // design's own ambiguous-`client_cancelled` mapping is the one legacy
    // reason this app can classify more specifically than "unknown";
    // anything else (absent, unrecognized, non-string) falls back to the
    // same generic, truthful cancellation copy.
    final rawReason = _asString(event.payload['reason'])?.trim();
    final reason = rawReason == _clientCancelledReason
        ? RunOutcomeReason.abandoned
        : RunOutcomeReason.none;
    final entry = _RunCancelledEntry(reason);
    setState(() => _messages.add(entry));
    _scrollToBottom();
  }

  /// Whether an inline run artifact belongs to history that is already on
  /// screen. Approvals deliberately replay to rebuild pending state, while
  /// tool cards, run notices, and the LEGACY (no-`RunState`) terminal
  /// failure/cancellation fallback ([_applyRunFailed], [_applyRunCancelled])
  /// cannot be interleaved into message history and therefore stay hidden
  /// once that history is complete. A modern, `RunState`-bearing terminal
  /// event is unaffected by this guard — its own adjacent card is governed
  /// entirely by [RunStateReconciler] instead, which is inherently replay-
  /// safe (a replayed report reconciles as a duplicate/stale no-op).
  bool _isHistoricalRunEvent(TuringEvent event) {
    final watermark = _replayWatermarkSequence;
    if (watermark != null && event.sequence <= watermark) return true;
    final runId = event.runId;
    return runId != null && _completedHistoryRunIds.contains(runId);
  }

  void _applyToolCall(TuringEvent event, ToolCallStatus status) {
    // The subscription replays the whole persisted log (see [_start]), so every
    // tool call that happened before this screen opened is re-delivered. Deltas
    // are de-duplicated by messageId; tool events are filtered by the event
    // sequence captured in [_loadReplayWatermark]. A run that completed while
    // history loaded is also filtered by its immutable runId, because its tool
    // events are newer than that earlier watermark even though its final answer
    // is already present in history. Recreating either card would append it
    // BELOW every later message (history is prepended, cards are appended) and
    // — because the create path seals live bubbles via
    // `_assistantEntries.clear()` — would orphan an adopted, still-streaming
    // assistant row into an empty pill with a duplicate bubble under it.
    //
    // Consequence, accepted deliberately: tool cards are not reconstructed for
    // calls the log already holds. There is no ordering key that could place
    // them correctly anyway — `Message` carries no event sequence to interleave
    // against — so the past renders as text only.
    if (_isHistoricalRunEvent(event)) return;
    final runId = event.runId;
    final toolCallId = _asString(event.payload['toolCallId']);
    if (toolCallId == null || toolCallId.isEmpty) return;
    // The frozen proto contract carries `toolName` as a non-nullable scalar, so
    // an unset value arrives as '' (not null) on the live stream. Treat empty as
    // missing so the card never renders a blank label.
    final rawToolName = _asString(event.payload['toolName']);
    final error = _asString(event.payload['error']);

    // The contract only guarantees `toolCallId` correlates started -> terminal
    // WITHIN a run, and a runtime that mints ids itself (call_0, call_1, ...
    // when the model supplies none) restarts the numbering every turn. Scope
    // the correlation key by the event's own runId so turn 2's `call_1` cannot
    // hijack turn 1's card — the payload contract is untouched, and
    // `toolCallId` stays the entry's own field.
    final key = (runId == null || runId.isEmpty)
        ? toolCallId
        : '$runId:$toolCallId';
    var entry = _toolEntries[key];
    if (entry == null) {
      // Create on 'started', or defensively on a terminal event that arrives
      // without a prior start (e.g. an event-replay gap).
      entry = _ToolCallEntry(
        toolCallId: toolCallId,
        runId: runId,
        toolName: (rawToolName == null || rawToolName.isEmpty)
            ? _placeholderToolName
            : rawToolName,
        serverName: _asString(event.payload['serverName']),
        status: status,
        error: error,
      );
      _toolEntries[key] = entry;
      // Seal any in-progress assistant bubbles: the runtime reuses one
      // assistantMessageId across the whole turn, so text streamed AFTER this
      // tool call must start a fresh bubble below the card rather than append
      // to the pre-tool bubble above it.
      _assistantEntries.clear();
      setState(() => _messages.add(entry!));
      _scrollToBottom();
      return;
    }
    final current = entry.state.value;
    // Only `tool.call.started` is guaranteed to carry `serverName`, and it can
    // arrive after a terminal event (replay gap). Adopt the metadata whenever
    // the card is still missing it, even on an event that is otherwise ignored.
    final incomingServer = _asString(event.payload['serverName']);
    final serverName =
        (current.serverName == null || current.serverName!.isEmpty)
        ? incomingServer
        : current.serverName;
    // Same self-healing for the name: if the first event the client saw carried
    // a malformed/empty `toolName` the card is stuck on the placeholder, so take
    // the first real name any later event supplies. A good name is never
    // downgraded back to the placeholder by a later malformed event.
    final toolName =
        (current.toolName == _placeholderToolName &&
            rawToolName != null &&
            rawToolName.isNotEmpty)
        ? rawToolName
        : current.toolName;
    // Terminal states are final: ignore a stale/duplicate 'started' replayed
    // (at-least-once redelivery, reconnect, replay overlap) after the call has
    // already resolved, so a finished card never regresses to a spinner.
    if (status == ToolCallStatus.running &&
        current.status != ToolCallStatus.running) {
      entry.state.value = (
        toolName: toolName,
        status: current.status,
        error: current.error,
        serverName: serverName,
      );
      return;
    }
    // Update the existing card in place with a single write: everything the
    // card renders lives in one notifier, so a changed error (or a newly
    // adopted serverName) still rebuilds even when `status` is unchanged.
    entry.state.value = (
      toolName: toolName,
      status: status,
      error: error ?? current.error,
      serverName: serverName,
    );
    _scrollToBottom();
  }

  void _applyMessageDelta(TuringEvent event) {
    final messageId =
        _asString(event.payload['messageId']) ?? 'active_assistant';
    // The replayed deltas that produced an already-complete history message
    // would otherwise render its text a second time below the history block.
    if (_completedHistoryMessageIds.contains(messageId)) return;
    final delta = _asString(event.payload['delta']) ?? '';
    var entry = _assistantEntries[messageId];
    if (entry == null) {
      entry = _MessageEntry.assistant(
        messageId: messageId,
        content: '',
        runId: event.runId,
      );
      _assistantEntries[messageId] = entry;
      setState(() => _messages.add(entry!));
    }
    entry.content.value = '${entry.content.value}$delta';
    // The row was adopted (empty, no `RunState` at all) with a neutral
    // no-response fallback beside it — see `_ingestMessagePage`. Real
    // content arriving proves that fallback was never the truth; clear it
    // the moment there is something displayable to show instead.
    final noResponseCard = _noResponseEntries[messageId];
    if (noResponseCard != null && hasDisplayableContent(entry.content.value)) {
      _noResponseEntries.remove(messageId);
      setState(() {
        _messages.remove(noResponseCard);
        noResponseCard.dispose();
      });
    }
    final runId = entry.runId;
    final state = runId == null ? null : _runStateReconciler.stateFor(runId);
    if (state != null &&
        hasDisplayableContent(entry.content.value) &&
        !_runStateCardPresenceMatchesContent(state)) {
      setState(
        () => _syncRunStateCardPresenceForContent(state, insertAdjacent: false),
      );
    }
    _scrollToBottom();
  }

  void _addApproval(TuringEvent event) {
    final approvalId = _asString(event.payload['approvalId']);
    final toolName = _asString(event.payload['toolName']);
    if (approvalId == null || toolName == null) return;
    setState(() {
      _approvals.removeWhere((approval) => approval.approvalId == approvalId);
      _approvals.add(
        _PendingApproval(
          approvalId: approvalId,
          toolName: toolName,
          argsSummary: _asString(event.payload['argsSummary']) ?? '',
        ),
      );
    });
  }

  void _clearApproval(TuringEvent event) {
    final approvalId = _asString(event.payload['approvalId']);
    if (approvalId == null) return;
    setState(() {
      _approvals.removeWhere((approval) => approval.approvalId == approvalId);
      // The backend can resolve an approval by itself (e.g. expiry) while a
      // decision RPC for it is in flight or already failed — either way,
      // that approval's own card is gone now, so neither the in-flight
      // guard nor its own membership in the decision-failure set should
      // outlive it. This stream event is authoritative over that RPC's own
      // outcome (round 12, finding 1): see [_approve]/[_deny]'s `catch`.
      _approvalsInFlight.remove(approvalId);
      _clearApprovalActionFailureFor(approvalId);
    });
  }

  Future<void> _sendMessage() async {
    // The composer's `enabled`/`onPressed` gating in `build()` is the
    // primary defence, but `TextField.onSubmitted` stays wired to this
    // method regardless of `enabled` — that flag only affects Flutter's own
    // focus/input routing, not whether this callback itself runs. A queued
    // IME commit (captured before the field became disabled) or any future
    // programmatic caller could still invoke it directly, so this method
    // must refuse on its own rather than trust the UI to have blocked it.
    // [_sending] is one of the flags [_composerDisabled] combines, so this
    // same guard also refuses a second, overlapping attempt while one is
    // already in flight — see [_sending]'s own doc for why that matters.
    if (_composerDisabled) return;
    final text = _controller.text.trim();
    if (text.isEmpty) return;
    final modelProvider = _modelProvider;
    final retry = _retryableSend;
    final isMatchingRetry =
        retry != null &&
        retry.content == text &&
        retry.modelProvider == modelProvider;
    final idempotencyKey = isMatchingRetry
        ? retry.idempotencyKey
        : _newSendIdempotencyKey();
    RemoteEgressConsent? remoteEgressConsent = isMatchingRetry
        ? retry.remoteEgressConsent
        : null;
    final remoteEgressApi = widget.apiClient;
    if (remoteEgressConsent == null) {
      setState(() => _sending = true);
      try {
        final disclosure = await remoteEgressApi.prepareRemoteEgress(
          sessionId: widget.sessionId,
          content: text,
          modelProvider: modelProvider,
          idempotencyKey: idempotencyKey,
        );
        if (!mounted) return;
        if (disclosure != null) {
          final confirmed = await showRemoteEgressDialog(context, disclosure);
          if (!mounted) return;
          if (!confirmed) {
            setState(() => _sending = false);
            return;
          }
          remoteEgressConsent = RemoteEgressConsent(
            challenge: disclosure.challenge,
            acknowledgedDataCategories: disclosure.dataCategories,
          );
        }
      } on Exception {
        if (!mounted) return;
        if (modelProvider != 'ollama') {
          setState(() => _sending = false);
          ScaffoldMessenger.of(context).showSnackBar(
            const SnackBar(content: Text('Could not prepare remote send')),
          );
          return;
        }
        // A local Ollama send must not depend on the disclosure preflight.
        // If this session is actually routed away, SendMessage still rejects
        // it server-side because no consent accompanies the request.
      }
    }
    _retryableSend = null;
    // Captured by reference (not looked up again later): `_messages` may
    // grow while this RPC is in flight (an unrelated run's own stream
    // events), so finding "this attempt's bubble" later means finding THIS
    // object, not re-deriving it from state that has since moved on.
    final attempt = isMatchingRetry
        ? retry.attempt
        : _MessageEntry.user(
            messageId: 'local_${DateTime.now().microsecondsSinceEpoch}',
            content: text,
            canAdoptPersistedIdentity: true,
          );
    setState(() {
      if (isMatchingRetry) {
        _removeSendOutcomeAfter(attempt);
        attempt.canAdoptPersistedIdentity = true;
      } else {
        _messages.add(attempt);
      }
      _sending = true;
    });
    _controller.clear();
    _scrollToBottom();
    // `sendMessage` (`TuringApi`, `networking/api_client.dart`) resolves once
    // a run is queued and rejects otherwise — the real `TuringGrpcApi`
    // implementation (`networking/grpc_client.dart`) rejects with a
    // `GrpcError` from the underlying call, or a `TuringApiException` if the
    // stream ends having never queued one. Both `implements Exception`
    // (unlike `Error` and its subtypes), so `on Exception` catches exactly
    // this RPC failure boundary without masking a programming bug elsewhere
    // in this method. Left unguarded, this `await` rejecting becomes an
    // unhandled Future rejection — the bubble above is added, then silence.
    try {
      final queued = remoteEgressConsent == null
          ? await widget.apiClient.sendMessage(
              sessionId: widget.sessionId,
              content: text,
              modelProvider: modelProvider,
              idempotencyKey: idempotencyKey,
            )
          : await remoteEgressApi.sendMessageWithRemoteEgressConsent(
              sessionId: widget.sessionId,
              content: text,
              modelProvider: modelProvider,
              idempotencyKey: idempotencyKey,
              consent: remoteEgressConsent,
            );
      if (!mounted) return;
      setState(() {
        final runId = _asString(queued['runId']);
        if (runId != null && runId.isNotEmpty) attempt.runId = runId;
        _sending = false;
      });
    } on Exception catch (error) {
      if (!mounted) return;
      setState(() {
        // Whether the composer actually re-enables still depends on
        // [_composerDisabled]'s OTHER flags: if the stream ended or startup
        // failed while this RPC was pending, clearing [_sending] alone
        // will not unblock it, which is exactly the desired behaviour —
        // this send's own outcome must never override a startup/stream
        // state that arrived independently of it.
        _sending = false;
        if (attempt.hasAdoptedPersistedIdentity) {
          // A resync/live page already replaced this attempt's local
          // identity with a durable message/run id WHILE this same RPC was
          // still pending (see `_adoptPersistedUserMessageIdentities`).
          // Durable identity is authoritative proof the send was accepted
          // server-side; this rejection is just this client never seeing
          // its own acknowledgement. Re-arming retry/warning state for an
          // attempt already proven durable would contradict what is
          // already correctly on screen, so this stale outcome changes
          // nothing else.
          return;
        }
        final confirmedPreEnqueue = _isConfirmedPreEnqueueSendFailure(error);
        // An unconfirmed rejection may have persisted successfully. Keep its
        // optimistic row eligible to adopt the durable identity if a resync
        // proves that exact content/run correlation. A confirmed pre-enqueue
        // failure cannot have created a row and must never adopt an older one.
        attempt.canAdoptPersistedIdentity = !confirmedPreEnqueue;
        // The user's own text is restored either way, confirmed failure or
        // not — they can retry or edit it instead of retyping it from
        // memory. Only the CARD differs: [_isConfirmedPreEnqueueSendFailure]
        // picks between the two truthfully, per its own doc — everything
        // else about recovering from this rejection (draft restoration,
        // anchoring, the mounted guard above) is identical for both.
        _controller.text = text;
        if (_isConfirmedPreEnqueueSendFailure(error)) {
          _retryableSend = null;
        } else {
          _retryableSend = _RetryableSend(
            text,
            modelProvider,
            idempotencyKey,
            attempt,
            remoteEgressConsent,
          );
        }
        // Anchored immediately after `attempt`, never appended to the
        // list's end: an unrelated stream event for another in-flight run
        // may have already been appended while this RPC was pending, and
        // this outcome must still read as belonging to ITS OWN bubble, not
        // to whatever happens to be last.
        _insertAfterEntry(
          attempt,
          confirmedPreEnqueue
              ? _MessageSendFailureEntry(_messageSendFailedNotice)
              : _MessageSendUnconfirmedEntry(_messageSendUnconfirmedNotice),
        );
      });
      _scrollToBottom();
    }
  }

  void _discardRetryForEditedText() {
    final retry = _retryableSend;
    if (retry != null && _controller.text.trim() != retry.content) {
      _retryableSend = null;
    }
  }

  void _removeSendOutcomeAfter(_MessageEntry attempt) {
    final index = _messages.indexOf(attempt);
    if (index == -1 || index + 1 >= _messages.length) return;
    final outcome = _messages[index + 1];
    if (outcome is _MessageSendUnconfirmedEntry ||
        outcome is _MessageSendFailureEntry) {
      _messages.removeAt(index + 1).dispose();
    }
  }

  /// Inserts [entry] immediately after [origin] in [_messages] rather than
  /// at the list's end. [origin] must already be present; falls back to
  /// appending if it somehow is not (defensive — entries are never removed
  /// from [_messages] once added, so this should not happen in practice).
  /// Must be called from inside a `setState` — it mutates [_messages]
  /// directly and does not schedule a rebuild itself.
  void _insertAfterEntry(_ChatEntry origin, _ChatEntry entry) {
    final index = _messages.indexOf(origin);
    if (index == -1) {
      _messages.add(entry);
      return;
    }
    _messages.insert(index + 1, entry);
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

  /// Truthful copy for the composer's hint text and send-button tooltip in
  /// every state `build()` can be in. Checked in this order because a
  /// still-loading composer must say so specifically, never "Send" — even
  /// though [_initializing] is also one of the four flags [_composerDisabled]
  /// combines, so checking that first would collapse the distinct loading
  /// case into the same generic unavailable copy used for a real drop.
  /// [_startupFailed]/[_streamEnded] are checked before [_sending] for the
  /// same reason in reverse: they describe this SESSION as broken, which
  /// stays true regardless of how the current send resolves, so that copy
  /// must win over a merely transient "Sending..." if both were ever true
  /// at once (an event-stream drop landing while a `sendMessage` RPC — a
  /// separate stream — is still outstanding).
  String _composerCopy(String readyCopy) {
    if (_initializing) return _loadingNotice;
    if (_startupFailed || _streamEnded) return _streamEndedNotice;
    if (_sending) return _sendingNotice;
    return readyCopy;
  }

  @override
  Widget build(BuildContext context) {
    final body = Column(
      children: [
        Expanded(
          // A selected-but-empty conversation rendered as a tall black void
          // with the composer stranded at the bottom. An empty room should say
          // what to do in it.
          child: _messages.isEmpty && !_initializing
              ? const _EmptyConversation()
              : ListView.builder(
                  controller: _scrollController,
                  padding: const EdgeInsets.symmetric(
                    horizontal: 28,
                    vertical: 20,
                  ),
                  itemCount: _messages.length,
                  // Key on the entry itself: `_loadInitialMessages` prepends
                  // history with `insertAll(0, ...)`, shifting every live entry's
                  // index. Without a key Flutter re-associates Elements by
                  // position, tearing down and re-subscribing each
                  // ValueListenableBuilder and resetting a running card's spinner.
                  itemBuilder: (context, index) => _ChatMessageTile(
                    key: ObjectKey(_messages[index]),
                    entry: _messages[index],
                  ),
                ),
        ),
        if (_historyLoadFailed)
          _SessionNotice(
            // `_historyFailedNotice`'s "This session is live from here on"
            // is only true while a subscription is CURRENTLY able to
            // deliver. That fails on [_startupFailed] (no subscription
            // will ever exist, whether from the watermark or from
            // [_openSubscription] itself) just as surely as on
            // [_streamEnded] (a subscription that opened fine has since
            // gone dark, recoverably or not — see [_handleStreamEnded]):
            // either one alone already makes the claim false, and
            // [_streamEnded] is set ASYNCHRONOUSLY, after this banner may
            // already be showing the live copy, so it must be checked
            // here too, not just at first render. Both flags share the
            // same corrected copy because the distinction between them —
            // permanent vs. recoverable — is already carried by whether
            // the connection-lost notice below persists or later clears.
            message: _startupFailed || _streamEnded
                ? _historyFailedNoSubscriptionNotice
                : _historyFailedNotice,
          ),
        if (_streamEnded) _SessionNotice(message: _streamEndedNotice),
        if (_approvalActionFailedApprovalIds.isNotEmpty)
          _SessionNotice(
            message: _approvalActionFailedNotice,
            // Distinct from both the connection-lost banner's icon AND
            // `TerminalOutcomeCard`'s (used by every card in the
            // transcript, including this same screen's own
            // `MessageSendUnconfirmedCard`): this banner must be
            // findable and readable as its own, unambiguous thing, not
            // visually or programmatically conflated with either.
            icon: Icons.warning_amber_rounded,
          ),
        for (final approval in _approvals)
          ApprovalCard(
            toolName: approval.toolName,
            argsSummary: approval.argsSummary,
            onApprove: () => _approve(approval),
            onDeny: () => _deny(approval),
            busy: _approvalsInFlight.contains(approval.approvalId),
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
                    enabled: !_composerDisabled,
                    onSubmitted: (_) => _sendMessage(),
                    decoration: InputDecoration(
                      hintText: _composerCopy('Ask Turing...'),
                    ),
                  ),
                ),
                IconButton(
                  tooltip: _composerCopy('Send'),
                  icon: (_initializing || _sending)
                      ? const SizedBox.square(
                          dimension: 18,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Icon(Icons.send),
                  onPressed: _composerDisabled ? null : _sendMessage,
                ),
              ],
            ),
          ),
        ),
      ],
    );
    // Standalone (tests, deep links) still gets its own Scaffold; embedded
    // in the shell it is just the right-hand pane.
    if (widget.embedded) return body;
    return Scaffold(
      appBar: AppBar(title: const Text('Project Turing')),
      body: body,
    );
  }

  /// Whether [approvalId] is still present in [_approvals] — i.e. still
  /// pending a decision, as opposed to already resolved by ANY means (an
  /// approve, a deny, an expiry, or a consumption) via the event stream.
  /// Read by [_approve]/[_deny]'s own `catch` (round 12, finding 1): the
  /// event stream is authoritative over a decision RPC's own outcome, so a
  /// rejection for an approval that the stream already resolved while the
  /// RPC was in flight must be ignored rather than recorded as a fresh
  /// failure.
  bool _isApprovalPending(String approvalId) =>
      _approvals.any((item) => item.approvalId == approvalId);

  /// Removes [approvalId] from [_approvalActionFailedApprovalIds] — a no-op
  /// if it is not a member. Scoped to exactly this one id (round 12,
  /// finding 2) so resolving or retrying one approval never hides a
  /// decision-failure banner still owed to a DIFFERENT approval also
  /// pending on screen — see that field's own doc. Called both when a NEW
  /// decision attempt starts for this approval ([_approve]/[_deny],
  /// clearing their own stale failure right away, even before the new
  /// attempt itself resolves) and when the approval is resolved by ANY
  /// means via the event stream ([_clearApproval]). Must be called from
  /// inside a `setState`.
  void _clearApprovalActionFailureFor(String approvalId) {
    _approvalActionFailedApprovalIds.remove(approvalId);
  }

  Future<void> _approve(_PendingApproval approval) async {
    // Approve and Deny are mutually exclusive outcomes for the SAME
    // approval, so a duplicate tap or the other action racing an
    // already-in-flight decision for this one must be refused here, not
    // just at the button's own disabled state — the same reasoning
    // `_sendMessage` applies to its own `_composerDisabled` guard.
    if (_approvalsInFlight.contains(approval.approvalId)) return;
    setState(() {
      _approvalsInFlight.add(approval.approvalId);
      _clearApprovalActionFailureFor(approval.approvalId);
    });
    // Left unguarded, this `await` rejecting is an unhandled Future error —
    // the same hazard `_sendMessage` guards against, and just as reachable:
    // the backend can reject this decision (an already-resolved approval, a
    // dropped connection, ...) for reasons this client cannot always
    // prevent. `GrpcError`/[TuringApiException] both `implements Exception`,
    // so `on Exception` catches exactly this RPC failure boundary.
    try {
      await widget.apiClient.approveApproval(approval.approvalId);
      if (!mounted) return;
      setState(() {
        _approvals.removeWhere(
          (item) => item.approvalId == approval.approvalId,
        );
        _approvalsInFlight.remove(approval.approvalId);
        // Provably a no-op today: a fresh attempt always clears its own id
        // right away (above, before this `await`), and `_approvalsInFlight`
        // blocks any OTHER concurrent attempt for the same id from
        // re-adding it before this one finishes. Kept anyway so this
        // success path stays explicitly self-consistent — mirroring
        // `_clearApproval` — rather than relying on that invariant holding
        // across future changes.
        _clearApprovalActionFailureFor(approval.approvalId);
      });
    } on Exception catch (_) {
      if (!mounted) return;
      setState(() {
        _approvalsInFlight.remove(approval.approvalId);
        // A stream lifecycle event may have ALREADY resolved (or otherwise
        // removed) this very approval — e.g. it expired, or another client
        // decided it — WHILE the `await` above was still outstanding; see
        // `_clearApproval`, which is authoritative and runs the moment that
        // event arrives, regardless of this RPC's own eventual outcome
        // (round 12, finding 1). This rejection landing only now, for an
        // approval no longer even pending, must not resurrect a notice for
        // it. Only when the approval is STILL pending does the failure
        // belong on screen: its card stays in `_approvals` so it remains
        // actionable — the decision was never confirmed, so removing it
        // here would silently drop the user's request instead of letting
        // them retry it. The notice is generic and never echoes the raw
        // error — see `_approvalActionFailedNotice`'s own doc.
        if (_isApprovalPending(approval.approvalId)) {
          _approvalActionFailedApprovalIds.add(approval.approvalId);
        }
      });
    }
  }

  Future<void> _deny(_PendingApproval approval) async {
    if (_approvalsInFlight.contains(approval.approvalId)) return;
    setState(() {
      _approvalsInFlight.add(approval.approvalId);
      _clearApprovalActionFailureFor(approval.approvalId);
    });
    try {
      await widget.apiClient.denyApproval(approval.approvalId);
      if (!mounted) return;
      setState(() {
        _approvals.removeWhere(
          (item) => item.approvalId == approval.approvalId,
        );
        _approvalsInFlight.remove(approval.approvalId);
        // See `_approve`'s own success path for why this is a provable
        // no-op, kept only for explicit self-consistency.
        _clearApprovalActionFailureFor(approval.approvalId);
      });
    } on Exception catch (_) {
      if (!mounted) return;
      setState(() {
        _approvalsInFlight.remove(approval.approvalId);
        // See `_approve`'s own `catch` for why this is conditional on the
        // approval still being pending (round 12, finding 1).
        if (_isApprovalPending(approval.approvalId)) {
          _approvalActionFailedApprovalIds.add(approval.approvalId);
        }
      });
    }
  }

  @override
  void dispose() {
    _subscription?.cancel();
    // The instance captured at mount, not widget.eventSource: if the host
    // rebuilt with a different source, widget.eventSource is one this screen
    // never connected to, and closing THAT would leave the live channel open
    // forever while shutting down an unused one.
    _eventSource.close();
    for (final entry in _messages) {
      entry.dispose();
    }
    _controller.removeListener(_discardRetryForEditedText);
    _controller.dispose();
    _scrollController.dispose();
    super.dispose();
  }
}

class _RetryableSend {
  const _RetryableSend(
    this.content,
    this.modelProvider,
    this.idempotencyKey,
    this.attempt,
    this.remoteEgressConsent,
  );

  final String content;
  final String modelProvider;
  final String idempotencyKey;
  final _MessageEntry attempt;
  final RemoteEgressConsent? remoteEgressConsent;
}

class _ChatMessageTile extends StatelessWidget {
  const _ChatMessageTile({super.key, required this.entry});

  final _ChatEntry entry;

  @override
  Widget build(BuildContext context) {
    switch (entry) {
      case _MessageEntry message:
        return _MessageBubble(entry: message);
      case _ToolCallEntry tool:
        return ValueListenableBuilder<_ToolCallState>(
          valueListenable: tool.state,
          builder: (context, state, _) => ToolCallCard(
            toolName: state.toolName,
            serverName: state.serverName,
            status: state.status,
            error: state.error,
          ),
        );
      case _RunNoticeEntry notice:
        final category = notice.category;
        return category != null
            ? RunNoticeCard.localized(
                category: category,
                attempt: notice.attempt,
                maxAttempts: notice.maxAttempts,
              )
            : RunNoticeCard(note: notice.note!);
      case _RunFailureEntry failure:
        return RunFailureCard(reason: failure.reason);
      case _RunCancelledEntry cancelled:
        return RunCancelledCard(reason: cancelled.reason);
      case _RunStateCardEntry stateCard:
        return ValueListenableBuilder<RunState>(
          valueListenable: stateCard.state,
          builder: (context, state, _) => RunStateCard(
            state: state,
            responseContentUnavailable: stateCard.responseContentUnavailable,
          ),
        );
      case _NoResponseCardEntry _:
        return const NoResponseCard();
      case _MessageSendUnconfirmedEntry sendUnconfirmed:
        return MessageSendUnconfirmedCard(message: sendUnconfirmed.message);
      case _MessageSendFailureEntry sendFailed:
        return MessageSendFailureCard(message: sendFailed.message);
    }
  }
}

/// Persistent, screen-reader-announced banner for a screen-level notice —
/// an event stream that is gone, history that failed to load, or an
/// approval decision the backend rejected. Presentational only: the parent
/// owns when it is shown and which [icon] fits its cause.
class _SessionNotice extends StatelessWidget {
  const _SessionNotice({required this.message, this.icon = Icons.cloud_off});

  final String message;

  /// Defaults to the original connection-lost glyph so every pre-existing
  /// caller keeps its exact appearance unchanged.
  final IconData icon;

  @override
  Widget build(BuildContext context) {
    final colorScheme = Theme.of(context).colorScheme;
    return Semantics(
      container: true,
      liveRegion: true,
      child: Container(
        width: double.infinity,
        color: colorScheme.errorContainer,
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        child: Row(
          children: [
            Icon(icon, size: 18, color: colorScheme.onErrorContainer),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                message,
                style: TextStyle(color: colorScheme.onErrorContainer),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Markdown styling for assistant turns. Code and inline code sit on the
/// raised surface so they read as quoted machine output rather than prose,
/// and headings stay close to body size — an answer inside a conversation
/// should not shout like a document title.
MarkdownStyleSheet _assistantMarkdown(
  BuildContext context,
  AppPalette palette,
) {
  final body = TextStyle(fontSize: 15, height: 1.6, color: palette.text);
  final mono = TextStyle(
    fontFamily: 'Menlo',
    fontSize: 13,
    height: 1.5,
    color: palette.text,
  );
  return MarkdownStyleSheet(
    p: body,
    listBullet: body,
    a: const TextStyle(color: AppColors.brand),
    strong: body.copyWith(fontWeight: FontWeight.w600),
    em: body.copyWith(fontStyle: FontStyle.italic),
    h1: body.copyWith(fontSize: 19, fontWeight: FontWeight.w700),
    h2: body.copyWith(fontSize: 17, fontWeight: FontWeight.w700),
    h3: body.copyWith(fontSize: 15.5, fontWeight: FontWeight.w700),
    code: mono.copyWith(backgroundColor: Colors.transparent),
    codeblockDecoration: BoxDecoration(
      color: palette.raised,
      borderRadius: BorderRadius.circular(8),
      border: Border.all(color: palette.border),
    ),
    codeblockPadding: const EdgeInsets.all(12),
    blockquoteDecoration: BoxDecoration(
      border: Border(left: BorderSide(color: palette.border, width: 3)),
    ),
    blockquotePadding: const EdgeInsets.only(left: 12),
    tableBorder: TableBorder.all(color: palette.border),
    tableCellsPadding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
    horizontalRuleDecoration: BoxDecoration(
      border: Border(top: BorderSide(color: palette.border)),
    ),
    blockSpacing: 10,
  );
}

class _EmptyConversation extends StatelessWidget {
  const _EmptyConversation();

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 380),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.auto_awesome,
              size: 26,
              color: AppColors.brand.withValues(alpha: 0.7),
            ),
            const SizedBox(height: 14),
            Text(
              'What can I help with?',
              style: TextStyle(
                fontSize: 16.5,
                fontWeight: FontWeight.w600,
                color: palette.text,
              ),
            ),
            const SizedBox(height: 6),
            Text(
              'Runs on this machine, can use tools, and remembers earlier '
              'conversations.',
              textAlign: TextAlign.center,
              style: TextStyle(
                fontSize: 13,
                height: 1.5,
                color: palette.textMuted,
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _MessageBubble extends StatelessWidget {
  const _MessageBubble({required this.entry});

  final _MessageEntry entry;

  /// A conversation should read as a conversation, not a column of equal-weight
  /// boxes. So the two roles are rendered differently on purpose:
  ///
  /// The user's turn is a compact tinted block, right-aligned — it is short and
  /// it is an instruction, so it should look like one. The assistant's turn is
  /// plain text on the page at a comfortable measure, because it is the content
  /// you actually came to read; wrapping it in a bubble made it compete with
  /// the tool and notice cards around it.
  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    // The builder wraps the chrome, not just the text: an entry can legitimately
    // hold no content — an assistant row adopted from a mid-run reopen before
    // its first delta, or one sealed by a tool call that arrived before any text
    // — and a decorated Container with an empty Text paints as a stray pill that
    // screen readers announce as an empty node. The entry (and its ObjectKey
    // identity) is deliberately left in place so late deltas can still fill it.
    return ValueListenableBuilder<String>(
      valueListenable: entry.content,
      builder: (context, content, _) {
        if (!hasDisplayableContent(content)) return const SizedBox.shrink();
        if (entry.isUser) {
          return Align(
            alignment: Alignment.centerRight,
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 560),
              child: Container(
                margin: const EdgeInsets.only(top: 14, bottom: 6, left: 40),
                padding: const EdgeInsets.symmetric(
                  horizontal: 14,
                  vertical: 10,
                ),
                decoration: BoxDecoration(
                  color: AppColors.brand.withValues(alpha: 0.16),
                  borderRadius: const BorderRadius.only(
                    topLeft: Radius.circular(12),
                    topRight: Radius.circular(12),
                    bottomLeft: Radius.circular(12),
                    bottomRight: Radius.circular(4),
                  ),
                ),
                child: SelectableText(
                  content,
                  style: TextStyle(
                    fontSize: 14.5,
                    height: 1.5,
                    color: palette.text,
                  ),
                ),
              ),
            ),
          );
        }
        // Rendered as markdown, not plain text: the assistant writes lists,
        // code and tables, and a wall of unformatted characters is the
        // difference between a transcript you can read and one you have to
        // decode. Selectable so answers can be copied out.
        return Align(
          alignment: Alignment.centerLeft,
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 760),
            child: Padding(
              padding: const EdgeInsets.only(top: 6, bottom: 14, right: 40),
              child: MarkdownBody(
                data: content,
                selectable: true,
                styleSheet: _assistantMarkdown(context, palette),
              ),
            ),
          ),
        );
      },
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
    this.runId,
    this.canAdoptPersistedIdentity = false,
  }) : content = ValueNotifier(content);

  factory _MessageEntry.user({
    required String messageId,
    required String content,
    String? runId,
    bool canAdoptPersistedIdentity = false,
  }) {
    return _MessageEntry(
      messageId: messageId,
      isUser: true,
      content: content,
      runId: runId,
      canAdoptPersistedIdentity: canAdoptPersistedIdentity,
    );
  }

  factory _MessageEntry.assistant({
    required String messageId,
    required String content,
    String? runId,
  }) {
    return _MessageEntry(
      messageId: messageId,
      isUser: false,
      content: content,
      runId: runId,
    );
  }

  factory _MessageEntry.fromMessage(Message message) {
    return _MessageEntry(
      messageId: message.messageId,
      isUser: message.role == 'user',
      content: message.content,
      runId: message.runId,
    );
  }

  String messageId;
  final bool isUser;

  /// The run this row belongs to, if any — used by
  /// [_ChatScreenState._assistantEntryIndexForRun] to place (and find) the
  /// one adjacent [_RunStateCardEntry] a run's reconciled state wants.
  String? runId;
  bool canAdoptPersistedIdentity;

  /// True once [_ChatScreenState._adoptPersistedUserMessageIdentities] has
  /// replaced this row's local identity with a durable message/run id —
  /// distinct from [canAdoptPersistedIdentity] going `false`, which ALSO
  /// happens for an attempt that never becomes eligible to adopt at all
  /// (a confirmed pre-enqueue failure in [_ChatScreenState._sendMessage]'s
  /// `catch`). That other `false` means "not eligible"; this one means
  /// "already durably proven" — [_ChatScreenState._sendMessage]'s `catch`
  /// needs the latter specifically to recognise its own pending RPC as
  /// stale once a resync has adopted this row out from under it, without
  /// being fooled by the former.
  bool hasAdoptedPersistedIdentity = false;
  final ValueNotifier<String> content;

  @override
  void dispose() => content.dispose();
}

/// Everything a [ToolCallCard] renders that can change after creation, held in
/// ONE notifier. Records have value equality, so any changed member (a
/// corrected `error`, a late `serverName` or `toolName`) notifies even when
/// `status` is the same — a plain field read during build would silently go
/// stale instead.
typedef _ToolCallState = ({
  String toolName,
  ToolCallStatus status,
  String? error,
  String? serverName,
});

class _ToolCallEntry extends _ChatEntry {
  _ToolCallEntry({
    required this.toolCallId,
    required this.runId,
    required String toolName,
    required ToolCallStatus status,
    String? serverName,
    String? error,
  }) : state = ValueNotifier((
         toolName: toolName,
         status: status,
         error: error,
         serverName: serverName,
       ));

  final String toolCallId;
  final String? runId;
  final ValueNotifier<_ToolCallState> state;

  @override
  void dispose() => state.dispose();
}

class _RunNoticeEntry extends _ChatEntry {
  _RunNoticeEntry.freeform(this.note, {required this.runId})
    : category = null,
      attempt = 0,
      maxAttempts = 0;

  _RunNoticeEntry.localized({
    required RunStepNoticeCategory this.category,
    required this.attempt,
    required this.maxAttempts,
    required this.runId,
  }) : note = null;

  /// Governed nonfailure notice text, verbatim — null for a
  /// [RunNoticeEntry.localized] entry, which never carries free text at all.
  final String? note;

  /// Set only for one of the three allowlisted failure-adjacent notice
  /// categories; null for a governed nonfailure [note].
  final RunStepNoticeCategory? category;
  final int attempt;
  final int maxAttempts;
  final String? runId;

  @override
  void dispose() {}
}

class _RunFailureEntry extends _ChatEntry {
  _RunFailureEntry(this.reason);

  final RunOutcomeReason reason;

  @override
  void dispose() {}
}

class _RunCancelledEntry extends _ChatEntry {
  _RunCancelledEntry(this.reason);

  final RunOutcomeReason reason;

  @override
  void dispose() {}
}

/// The one adjacent card [_ChatScreenState._upsertRunStateCard] maintains
/// per run ID for a [RunState] that wants one — see
/// [_ChatScreenState._wantsAdjacentCard]. A [ValueNotifier] so an in-place
/// update (a new, still-card-worthy state for the same run) re-renders
/// without recreating or repositioning the entry.
class _RunStateCardEntry extends _ChatEntry {
  _RunStateCardEntry(RunState state, {required this.responseContentUnavailable})
    : state = ValueNotifier(state);

  final ValueNotifier<RunState> state;
  bool responseContentUnavailable;

  @override
  void dispose() => state.dispose();
}

/// The terminal-only neutral legacy-correlation fallback beside an EMPTY
/// historical assistant row that carries no [RunState] at all — see
/// [_ChatScreenState._ingestMessagePage] (creation) and
/// [_ChatScreenState._applyMessageDelta] (clearing it once real content
/// arrives). Stateless: [NoResponseCard] itself has no parameters.
class _NoResponseCardEntry extends _ChatEntry {
  @override
  void dispose() {}
}

/// A `sendMessage` RPC rejected before any `RunQueued` event ever arrived,
/// with a status NOT in [_ChatScreenState._isConfirmedPreEnqueueSendFailure]'s
/// allowlist — see [MessageSendUnconfirmedCard] for why the true outcome is
/// unknown, not a confirmed failure, and must never be conflated with
/// [_RunFailureEntry] or [_MessageSendFailureEntry]. Inserted immediately
/// after the optimistic [_MessageEntry.user] bubble for the same attempt via
/// [_ChatScreenState._insertAfterEntry] in [_ChatScreenState._sendMessage]'s
/// own `catch` — never appended to the list's end, and never from the event
/// stream, so it stays anchored to its own attempt even if an unrelated
/// stream event for another in-flight run arrives first.
class _MessageSendUnconfirmedEntry extends _ChatEntry {
  _MessageSendUnconfirmedEntry(this.message);

  final String message;

  @override
  void dispose() {}
}

/// A `sendMessage` RPC rejected before any `RunQueued` event ever arrived,
/// with a status
/// [_ChatScreenState._isConfirmedPreEnqueueSendFailure] confirms occurred
/// before `EnqueueUserMessage` — see [MessageSendFailureCard] for the exact
/// allowlist and why each entry qualifies, and why this must never be
/// conflated with [_MessageSendUnconfirmedEntry] or [_RunFailureEntry].
/// Inserted immediately after the optimistic [_MessageEntry.user] bubble for
/// the same attempt via [_ChatScreenState._insertAfterEntry] in
/// [_ChatScreenState._sendMessage]'s own `catch` — never appended to the
/// list's end, and never from the event stream, so it stays anchored to its
/// own attempt even if an unrelated stream event for another in-flight run
/// arrives first.
class _MessageSendFailureEntry extends _ChatEntry {
  _MessageSendFailureEntry(this.message);

  final String message;

  @override
  void dispose() {}
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
