/// One row of the Needs-you queue's closed stage table, read top to bottom.
/// Every stage is an obligation on the owner; healthy states are not rows.
enum AttentionStage {
  approval,
  consent,
  blocked,
  deletion,
  proposal,

  /// The orchestrator could not classify the row, or this build cannot act on
  /// what it says. It is still listed, so an empty queue is always a fact.
  unavailable,
}

/// Where a row came from, which together with its stage decides its action.
enum AttentionSourceKind {
  approval,
  delegation,
  automation,
  deletion,
  watch,
  proposal,
  unknown,
}

/// One owner obligation, exactly as `ListAttention` returned it: the stage,
/// the conversation, when it happened and the ids its action needs. It never
/// holds tool arguments, message content or transcripts.
///
/// [sessionTitle] and [automationName] are untrusted text, shown as plain text.
class AttentionRow {
  const AttentionRow({
    required this.stage,
    required this.kind,
    this.since,
    this.sessionId = '',
    this.sessionTitle = '',
    this.approvalId = '',
    this.runId = '',
    this.delegationId = '',
    this.automationId = '',
    this.automationName = '',
    this.occurrenceKey = '',
    this.cause = '',
  });

  final AttentionStage stage;
  final AttentionSourceKind kind;

  /// When the occurrence the row reports happened, or null when the server
  /// sent no time.
  final DateTime? since;
  final String sessionId;
  final String sessionTitle;
  final String approvalId;
  final String runId;
  final String delegationId;
  final String automationId;
  final String automationName;

  /// The key of the automation occurrence the row shows. Dismiss sends it back
  /// unchanged, so a newer failure is never hidden by a stale screen.
  final String occurrenceKey;

  /// A closed, content-free code: an automation's failure or block, or a
  /// deletion's failure class.
  final String cause;

  /// The row's subject. One subject is one row, so this identifies a row
  /// across refetches even when its stage or position changes.
  String get key => switch (kind) {
    AttentionSourceKind.approval => 'approval:$approvalId',
    AttentionSourceKind.delegation => 'delegation:$delegationId',
    AttentionSourceKind.automation => 'automation:$automationId',
    AttentionSourceKind.deletion => 'deletion:$sessionId',
    AttentionSourceKind.watch ||
    AttentionSourceKind.proposal ||
    AttentionSourceKind.unknown =>
      '${kind.name}:$sessionId:$runId:$automationId:$occurrenceKey',
  };
}

/// One page of the queue. [nextCursor] is set exactly when a later page
/// exists, and carries the first page's watermark.
class AttentionPage {
  const AttentionPage({required this.rows, this.nextCursor});

  final List<AttentionRow> rows;
  final String? nextCursor;
}
