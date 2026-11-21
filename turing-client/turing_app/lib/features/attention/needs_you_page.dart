import 'dart:async';

import 'package:material_ui/material_ui.dart';

import '../../constants/app_colors.dart';
import '../../models/approval.dart';
import '../../models/attention.dart';
import '../../models/delegation.dart';
import '../../models/remote_egress.dart';
import '../../models/session_deletion.dart';
import '../../networking/api_client.dart';
import '../approvals/approval_card.dart';
import '../chat/delegation_card.dart';
import '../chat/remote_egress_dialog.dart';
import '../workspace/automations_page.dart'
    show describeOccurrenceFailure, formatWhen;
import '../workspace/workspace_pages.dart';
import 'attention_queue.dart';

/// Everything waiting on the owner, across conversations.
///
/// Each row is a summary built only from what `ListAttention` returned:
/// opening the list reads nothing else. Expanding a row is what mounts the
/// existing card and decision flow ([ApprovalCard], or a task's
/// [DelegationCard] with its consent dialog), and at most one row is expanded
/// at a time, because an approval's card reads its details, and prepares a
/// file preview, as soon as it is built. Every action goes through the RPC
/// the chat uses, which re-checks state, so a stale row's action is refused
/// rather than replayed; after any action the list is read again.
class NeedsYouPage extends StatefulWidget {
  const NeedsYouPage({
    super.key,
    required this.queue,
    required this.apiClient,
    required this.onOpenSession,
    required this.onOpenAutomations,
    required this.onSessionDeleted,
  });

  final AttentionQueue queue;
  final TuringApi apiClient;
  final ValueChanged<String> onOpenSession;
  final VoidCallback onOpenAutomations;

  /// A retried deletion that completed, so the shell can drop the
  /// conversation without waiting for its event.
  final ValueChanged<String> onSessionDeleted;

  @override
  State<NeedsYouPage> createState() => _NeedsYouPageState();
}

class _NeedsYouPageState extends State<NeedsYouPage> {
  String? _expanded;
  final Set<String> _acting = {};

  @override
  void initState() {
    super.initState();
    widget.queue.addListener(_onQueueChanged);
    // Opening the list is a reason to read it.
    widget.queue.requestRefresh();
  }

  @override
  void didUpdateWidget(NeedsYouPage oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(oldWidget.queue, widget.queue)) {
      oldWidget.queue.removeListener(_onQueueChanged);
      widget.queue.addListener(_onQueueChanged);
    }
  }

  @override
  void dispose() {
    widget.queue.removeListener(_onQueueChanged);
    super.dispose();
  }

  /// A row the fresh list no longer holds, or holds at a stage with nothing
  /// to review, loses its expansion; one it still holds keeps its open card.
  void _onQueueChanged() {
    if (!mounted) return;
    setState(() {
      final expanded = _expanded;
      if (expanded != null &&
          !_keyed(
            widget.queue.rows,
          ).any((entry) => entry.key == expanded && _reviewable(entry.row))) {
        _expanded = null;
      }
    });
  }

  /// Each row with a key unique in this list. A subject is one row, but a
  /// kind this build cannot identify (a future source's) could repeat a key,
  /// and a repeated key would break the list rather than one row.
  static List<({String key, AttentionRow row})> _keyed(
    List<AttentionRow> rows,
  ) {
    final seen = <String, int>{};
    return [
      for (final row in rows)
        (
          key: switch (seen.update(
            row.key,
            (count) => count + 1,
            ifAbsent: () => 0,
          )) {
            0 => row.key,
            final repeat => '${row.key}#$repeat',
          },
          row: row,
        ),
    ];
  }

  static bool _reviewable(AttentionRow row) =>
      row.stage == AttentionStage.approval ||
      row.stage == AttentionStage.consent;

  void _toggle(String key) {
    setState(() => _expanded = _expanded == key ? null : key);
  }

  /// The decision was recorded: the row is the server's to drop now.
  void _resolved(String key) {
    if (mounted && _expanded == key) setState(() => _expanded = null);
    widget.queue.requestRefresh();
  }

  void _report(String message) {
    if (!mounted) return;
    ScaffoldMessenger.maybeOf(context)
        ?.showSnackBar(SnackBar(content: Text(message)));
  }

  Future<void> _act(
    String key,
    Future<void> Function() action, {
    required String failure,
  }) async {
    if (!_acting.add(key)) return;
    setState(() {});
    try {
      await action();
    } on Exception catch (error) {
      _report(_attentionProblem(failure, error));
    } finally {
      _acting.remove(key);
      if (mounted) setState(() {});
      widget.queue.requestRefresh();
    }
  }

  Future<void> _dismiss(String key, AttentionRow row) => _act(
    key,
    () => widget.apiClient.dismissAttention(
      automationId: row.automationId,
      occurrenceKey: row.occurrenceKey,
    ),
    failure: 'Could not dismiss it',
  );

  Future<void> _retryDeletion(String key, AttentionRow row) =>
      _act(key, () async {
        final receipt = await widget.apiClient.deleteSession(
          sessionId: row.sessionId,
        );
        if (receipt.state == SessionDeletionState.completed) {
          widget.onSessionDeleted(row.sessionId);
          return;
        }
        _report(
          receipt.state == SessionDeletionState.failedExternal
              ? 'This chat could not be fully withdrawn yet. Retry deletion.'
              : 'This chat is still being withdrawn. Retry deletion shortly.',
        );
      }, failure: 'Could not retry the deletion');

  @override
  Widget build(BuildContext context) {
    return WorkspacePage(
      title: 'Needs you',
      subtitle:
          'Everything waiting on you, across conversations: approvals, tools '
          'a task wants to use off this machine, blocked automations and '
          'deletions to retry. It updates when something changes.',
      child: _content(),
    );
  }

  Widget _content() {
    final queue = widget.queue;
    if (queue.unsupported) {
      return const WorkspaceNotice(
        icon: Icons.info_outline,
        title: 'Not available',
        body: 'This backend cannot list what needs you.',
      );
    }
    if (!queue.loaded) {
      if (!queue.failed) return const WorkspaceLoading();
      return WorkspaceNotice(
        icon: Icons.error_outline,
        title: 'Could not check what needs you.',
        body: 'Nothing is known about what waits on you until it loads.',
        onRetry: queue.requestRefresh,
        tone: AppColors.danger,
      );
    }
    final rows = queue.rows;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (queue.failed)
          Padding(
            padding: const EdgeInsets.only(bottom: 12),
            child: WorkspaceNotice(
              icon: Icons.error_outline,
              title: 'Could not refresh',
              body: 'This list may be out of date.',
              onRetry: queue.requestRefresh,
              tone: AppColors.danger,
              compact: true,
            ),
          ),
        if (rows.isEmpty) const Text('Nothing needs you right now.'),
        for (final (:key, :row) in _keyed(rows))
          KeyedSubtree(
            key: ValueKey(key),
            child: Padding(
              padding: const EdgeInsets.only(bottom: 10),
              child: _AttentionRowCard(
                row: row,
                expanded: _expanded == key && _reviewable(row),
                acting: _acting.contains(key),
                onToggle: () => _toggle(key),
                onDismiss: () => _dismiss(key, row),
                onRetryDeletion: () => _retryDeletion(key, row),
                onOpenSession: () => widget.onOpenSession(row.sessionId),
                onOpenAutomations: widget.onOpenAutomations,
                review: _review(key, row),
              ),
            ),
          ),
        if (queue.nextCursor != null)
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton(
              onPressed: queue.reading ? null : queue.loadMore,
              child: Text(queue.loadingMore ? 'Loading...' : 'Load more'),
            ),
          ),
      ],
    );
  }

  Widget? _review(String key, AttentionRow row) {
    if (_expanded != key) return null;
    return switch (row.stage) {
      AttentionStage.approval => _ApprovalReview(
        key: ValueKey('review-$key'),
        row: row,
        apiClient: widget.apiClient,
        onResolved: () => _resolved(key),
        onStale: widget.queue.requestRefresh,
        onProblem: _report,
      ),
      AttentionStage.consent => _ConsentReview(
        key: ValueKey('review-$key'),
        row: row,
        apiClient: widget.apiClient,
        onResolved: () => _resolved(key),
        onStale: widget.queue.requestRefresh,
        onProblem: _report,
      ),
      _ => null,
    };
  }
}

/// A refusal the orchestrator explains (the item changed, expired or is
/// gone) is shown in its words; anything else is not echoed.
String _attentionProblem(String lead, Exception error) =>
    error is TuringApiException &&
        (error.code == 'failed_precondition' || error.code == 'not_found')
    ? '$lead: ${error.message}'
    : '$lead. Try again.';

class _AttentionRowCard extends StatelessWidget {
  const _AttentionRowCard({
    required this.row,
    required this.expanded,
    required this.acting,
    required this.onToggle,
    required this.onDismiss,
    required this.onRetryDeletion,
    required this.onOpenSession,
    required this.onOpenAutomations,
    required this.review,
  });

  final AttentionRow row;
  final bool expanded;
  final bool acting;
  final VoidCallback onToggle;
  final VoidCallback onDismiss;
  final VoidCallback onRetryDeletion;
  final VoidCallback onOpenSession;
  final VoidCallback onOpenAutomations;
  final Widget? review;

  bool get _isAutomation =>
      row.stage == AttentionStage.blocked &&
      row.kind == AttentionSourceKind.automation;

  String get _headline => switch (row.stage) {
    AttentionStage.approval =>
      row.delegationId.isEmpty
          ? 'Approval needed'
          : 'A delegated task needs approval',
    AttentionStage.consent => 'Consent needed',
    AttentionStage.blocked =>
      _isAutomation ? 'Automation blocked' : 'Watch suspended',
    AttentionStage.deletion => 'Deletion did not finish',
    AttentionStage.proposal => 'Watch proposal',
    AttentionStage.unavailable => "Turing couldn't read this item",
  };

  /// Untrusted: the owner's or a model's words, shown as plain text only.
  String get _name {
    if (row.kind == AttentionSourceKind.automation &&
        row.automationName.isNotEmpty) {
      return row.automationName;
    }
    if (row.stage == AttentionStage.deletion) {
      return 'A conversation you deleted';
    }
    return row.sessionTitle.isEmpty ? 'New chat' : row.sessionTitle;
  }

  String get _detail {
    final since = row.since;
    return [
      if (since != null) 'Since ${formatWhen(since.toLocal())}',
      ?_cause,
    ].join(' · ');
  }

  String? get _cause => switch (row.stage) {
    AttentionStage.blocked when _isAutomation => _describeAttentionCause(
      row.cause,
    ),
    AttentionStage.consent => 'A task wants tools that leave this machine.',
    _ when row.cause.isNotEmpty => 'Reason: ${row.cause}',
    _ => null,
  };

  IconData get _icon => switch (row.stage) {
    AttentionStage.approval => Icons.pending_actions,
    AttentionStage.consent => Icons.cloud_upload_outlined,
    AttentionStage.blocked =>
      _isAutomation ? Icons.event_busy_outlined : Icons.visibility_off_outlined,
    AttentionStage.deletion => Icons.delete_sweep_outlined,
    AttentionStage.proposal => Icons.lightbulb_outline,
    AttentionStage.unavailable => Icons.help_outline,
  };

  List<Widget> _actions() {
    final busy = acting;
    return [
      if (row.stage == AttentionStage.approval ||
          row.stage == AttentionStage.consent)
        FilledButton.tonal(
          onPressed: onToggle,
          child: Text(expanded ? 'Hide' : 'Review'),
        ),
      if (_isAutomation)
        FilledButton.tonal(
          onPressed: busy ? null : onDismiss,
          child: const Text('Dismiss'),
        ),
      if (row.stage == AttentionStage.deletion &&
          row.kind == AttentionSourceKind.deletion)
        FilledButton.tonal(
          onPressed: busy ? null : onRetryDeletion,
          child: const Text('Retry deletion'),
        ),
      if (row.stage != AttentionStage.deletion && row.sessionId.isNotEmpty)
        TextButton(onPressed: onOpenSession, child: const Text('Open chat'))
      else if (_isAutomation)
        TextButton(
          onPressed: onOpenAutomations,
          child: const Text('Automations'),
        ),
    ];
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    final detail = _detail;
    final tone = row.stage == AttentionStage.unavailable
        ? palette.textMuted
        : AppColors.brand;
    return Container(
      key: const ValueKey('attention-row'),
      padding: const EdgeInsets.fromLTRB(16, 14, 16, 10),
      decoration: BoxDecoration(
        color: palette.surface,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: palette.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Icon(_icon, size: 18, color: tone),
              const SizedBox(width: 10),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      _headline,
                      style: TextStyle(
                        fontSize: 12,
                        fontWeight: FontWeight.w600,
                        letterSpacing: 0.3,
                        color: palette.textMuted,
                      ),
                    ),
                    const SizedBox(height: 3),
                    Text(
                      _name,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 14.5,
                        fontWeight: FontWeight.w600,
                        color: palette.text,
                      ),
                    ),
                    if (detail.isNotEmpty) ...[
                      const SizedBox(height: 3),
                      Text(
                        detail,
                        style: TextStyle(
                          fontSize: 12.5,
                          height: 1.45,
                          color: palette.textMuted,
                        ),
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Wrap(spacing: 8, runSpacing: 4, children: _actions()),
          ?review,
        ],
      ),
    );
  }
}

/// What an automation's closed failure code means, in words.
String _describeAttentionCause(String code) => switch (code) {
  '' => 'Its latest run failed.',
  'automation_tool_not_allowlisted' =>
    'It tried a tool its allowlist does not include.',
  'automation_grant_revoked' => 'A tool it relies on is no longer allowed.',
  'remote_egress_requires_interactive_consent' ||
  'remote_egress_configuration_invalid' => describeOccurrenceFailure(code),
  _ => 'Reason: $code',
};

/// An approval row's review: one `GetApprovalDetails` read, handed to the
/// [ApprovalCard] the chat uses, so mounting the card does not read twice.
/// Its Retry and Refresh read again, as they do in the chat.
class _ApprovalReview extends StatefulWidget {
  const _ApprovalReview({
    super.key,
    required this.row,
    required this.apiClient,
    required this.onResolved,
    required this.onStale,
    required this.onProblem,
  });

  final AttentionRow row;
  final TuringApi apiClient;
  final VoidCallback onResolved;

  /// The row may no longer be the server's: read the list again.
  final VoidCallback onStale;
  final ValueChanged<String> onProblem;

  @override
  State<_ApprovalReview> createState() => _ApprovalReviewState();
}

class _ApprovalReviewState extends State<_ApprovalReview> {
  ApprovalDetails? _first;
  bool _handedOver = false;
  bool _failed = false;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    unawaited(_read());
  }

  /// A delegated task's approval is on the task's own conversation, while
  /// its row is listed under the parent's, so only the approval and its run
  /// are compared for one.
  bool _matches(ApprovalDetails details) =>
      details.approvalId == widget.row.approvalId &&
      details.runId == widget.row.runId &&
      (widget.row.delegationId.isNotEmpty ||
          details.sessionId == widget.row.sessionId);

  Future<ApprovalDetails> _details(bool refresh) async {
    final details = await widget.apiClient.getApprovalDetails(
      widget.row.approvalId,
      refreshPreview: refresh,
    );
    if (!_matches(details)) {
      throw const TuringApiException(
        code: 'approval_unavailable',
        message: 'This approval is no longer available',
      );
    }
    return details;
  }

  Future<void> _read() async {
    setState(() {
      _failed = false;
      _first = null;
      _handedOver = false;
    });
    try {
      final details = await _details(false);
      if (!mounted) return;
      setState(() => _first = details);
      if (details.status != 'pending') widget.onStale();
    } on Exception {
      if (!mounted) return;
      setState(() => _failed = true);
      widget.onStale();
    }
  }

  Future<ApprovalDetails> _load(bool refresh) {
    final first = _first;
    if (!refresh && !_handedOver && first != null) {
      _handedOver = true;
      return Future.value(first);
    }
    return _details(refresh);
  }

  Future<void> _decide(Future<void> Function() decision, String lead) async {
    if (_busy) return;
    setState(() => _busy = true);
    try {
      await decision();
      widget.onResolved();
    } on Exception catch (error) {
      widget.onProblem(_attentionProblem(lead, error));
      widget.onStale();
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  void _approve(ApprovalDetails details) {
    if (!_matches(details) || !details.canApproveAt(DateTime.now())) return;
    unawaited(
      _decide(
        () => widget.apiClient.approveReviewedApproval(details),
        'Could not record your approval',
      ),
    );
  }

  void _deny() => unawaited(
    _decide(
      () => widget.apiClient.denyApproval(widget.row.approvalId),
      'Could not record your denial',
    ),
  );

  @override
  Widget build(BuildContext context) {
    final first = _first;
    if (_failed) {
      return _ReviewProblem(
        message: 'This approval could not be loaded.',
        onRetry: _read,
      );
    }
    if (first == null) return const _ReviewLoading();
    return ApprovalCard(
      approvalId: widget.row.approvalId,
      toolName: first.toolName,
      loadDetails: _load,
      onApprove: _approve,
      onDeny: _deny,
      busy: _busy,
    );
  }
}

/// A consent row's review: one `GetDelegation` read for the task's card,
/// whose Allow… opens the consent dialog the chat uses.
class _ConsentReview extends StatefulWidget {
  const _ConsentReview({
    super.key,
    required this.row,
    required this.apiClient,
    required this.onResolved,
    required this.onStale,
    required this.onProblem,
  });

  final AttentionRow row;
  final TuringApi apiClient;
  final VoidCallback onResolved;
  final VoidCallback onStale;
  final ValueChanged<String> onProblem;

  @override
  State<_ConsentReview> createState() => _ConsentReviewState();
}

class _ConsentReviewState extends State<_ConsentReview> {
  Delegation? _delegation;
  bool _failed = false;
  bool _deciding = false;

  @override
  void initState() {
    super.initState();
    unawaited(_read());
  }

  Future<void> _read() async {
    setState(() {
      _failed = false;
      _delegation = null;
    });
    try {
      final delegation = await widget.apiClient.getDelegation(
        delegationId: widget.row.delegationId,
      );
      if (delegation.delegationId != widget.row.delegationId ||
          (widget.row.runId.isNotEmpty &&
              delegation.childRunId != widget.row.runId)) {
        throw const TuringApiException(
          code: 'delegation_unavailable',
          message: 'This task is no longer available',
        );
      }
      if (!mounted) return;
      setState(() => _delegation = delegation);
      if (!delegation.waitingForConsent) widget.onStale();
    } on Exception {
      if (!mounted) return;
      setState(() => _failed = true);
      widget.onStale();
    }
  }

  Future<void> _decide(Future<void> Function(Delegation) decide) async {
    final delegation = _delegation;
    if (delegation == null || _deciding) return;
    setState(() => _deciding = true);
    try {
      await decide(delegation);
    } finally {
      if (mounted) setState(() => _deciding = false);
    }
  }

  Future<void> _record(
    Delegation delegation,
    RemoteEgressConsent? consent,
  ) async {
    try {
      await widget.apiClient.decideDelegationEgress(
        delegationId: delegation.delegationId,
        consent: consent,
      );
      widget.onResolved();
    } on Exception catch (error) {
      widget.onProblem(
        _attentionProblem(
          "Could not record your decision for ${delegation.specialist}'s task",
          error,
        ),
      );
      widget.onStale();
    }
  }

  Future<void> _allow() => _decide((delegation) async {
    final RemoteEgressDisclosure disclosure;
    try {
      disclosure = await widget.apiClient.prepareDelegationEgress(
        delegationId: delegation.delegationId,
      );
    } on Exception catch (error) {
      widget.onProblem(
        _attentionProblem(
          "Could not open ${delegation.specialist}'s request",
          error,
        ),
      );
      widget.onStale();
      return;
    }
    if (!mounted) return;
    final allowed = await showRemoteEgressDialog(
      context,
      disclosure,
      requester: delegation.specialist,
    );
    if (!allowed || !mounted) return;
    await _record(
      delegation,
      RemoteEgressConsent(
        challenge: disclosure.challenge,
        acknowledgedDataCategories: disclosure.dataCategories,
      ),
    );
  });

  Future<void> _decline() => _decide((delegation) => _record(delegation, null));

  @override
  Widget build(BuildContext context) {
    final delegation = _delegation;
    if (_failed) {
      return _ReviewProblem(
        message: 'This task could not be loaded.',
        onRetry: _read,
      );
    }
    if (delegation == null) return const _ReviewLoading();
    return DelegationCard(
      delegation: delegation,
      deciding: _deciding,
      onAllowEgress: _allow,
      onDeclineEgress: _decline,
    );
  }
}

class _ReviewLoading extends StatelessWidget {
  const _ReviewLoading();

  @override
  Widget build(BuildContext context) => const Padding(
    padding: EdgeInsets.symmetric(vertical: 12),
    child: SizedBox.square(
      dimension: 20,
      child: CircularProgressIndicator(strokeWidth: 2),
    ),
  );
}

class _ReviewProblem extends StatelessWidget {
  const _ReviewProblem({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(top: 8),
    child: Row(
      children: [
        Expanded(child: Text(message)),
        TextButton(onPressed: onRetry, child: const Text('Try again')),
      ],
    ),
  );
}
