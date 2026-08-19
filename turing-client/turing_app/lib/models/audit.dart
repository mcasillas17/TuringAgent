/// A redacted, paginated read of the audit log the backend already keeps.
///
/// This is a read surface, not a mirror: the server decides what a caller may
/// see per action, and this client never receives raw tool arguments, result
/// summaries, credentials, approval tokens, or other secrets. A payload
/// field's absence here means the server did not send it, not that this
/// model guessed a default — every optional stays null until something
/// upstream actually reported it.
///
/// One field is different in kind rather than in shape: an approval's
/// [AuditPayload.decisionComment] / [AuditPayload.denialReason] is free text a
/// person typed. The server discloses it on purpose, so it is the one place a
/// caller can read words no allowlist wrote.
library;

/// Sort direction for [AuditEntry.createdAt]. Mirrors
/// `turing.v1.AuditOrder`; unspecified is never a valid value here because
/// the client always sends an explicit order.
enum AuditOrder { descending, ascending }

/// Whether a row's structured payload was ever stored, is present (subject to
/// the server's action-typed field allowlist), or was scrubbed after the
/// fact. Mirrors `turing.v1.AuditPayloadState`. There is deliberately no
/// "unknown" member: an unrecognized value on the wire is a contract break,
/// not a state this client can safely render, so mapping it throws instead of
/// picking one of these three by default.
enum AuditPayloadState { absent, present, scrubbed }

/// One page of audit entries plus the cursor for the next one, or null when
/// this is the last page.
class AuditPage {
  AuditPage({required List<AuditEntry> entries, required this.nextCursor})
    : entries = List<AuditEntry>.unmodifiable(entries);

  final List<AuditEntry> entries;
  final String? nextCursor;
}

/// A single recorded audit row.
class AuditEntry {
  const AuditEntry({
    required this.auditId,
    required this.correlationId,
    required this.actorType,
    required this.actorId,
    required this.action,
    required this.target,
    required this.payload,
    required this.createdAt,
  });

  final String auditId;
  final String? correlationId;
  final String actorType;
  final String? actorId;
  final String action;
  final String? target;
  final AuditPayload payload;
  final DateTime createdAt;
}

/// The structured, per-action typed fields the server chose to disclose for
/// one entry's payload.
///
/// Every field besides [state] is nullable and each null means "the server
/// did not report this field for this action," never "the value was empty or
/// zero." A field that is legitimately an empty string, `false`, or `0` is
/// preserved as that value rather than collapsed to null — this is why the
/// mapping that builds these from protobuf checks each field's `has*`
/// presence bit instead of comparing against a default.
class AuditPayload {
  const AuditPayload({
    required this.state,
    this.toolName,
    this.serverName,
    this.phase,
    this.status,
    this.reason,
    this.durationMs,
    this.errorCode,
    this.provider,
    this.displayName,
    this.unattended,
    this.automationId,
    this.automationName,
    this.method,
    this.requestId,
    this.deletedRuns,
    this.deletedMessages,
    this.decisionComment,
    this.decisionCommentTruncated,
    this.denialReason,
    this.denialReasonTruncated,
  });

  final AuditPayloadState state;
  final String? toolName;
  final String? serverName;
  final String? phase;
  final String? status;
  final String? reason;
  final int? durationMs;
  final String? errorCode;
  final String? provider;
  final String? displayName;
  final bool? unattended;
  final String? automationId;
  final String? automationName;
  final String? method;
  final String? requestId;
  final int? deletedRuns;
  final int? deletedMessages;

  /// What the person typed when they approved (`approval.approved`) or denied
  /// (`approval.denied`) a tool call — their own words, bounded to 512 bytes
  /// by the backend before storage.
  ///
  /// An empty string means they decided and typed nothing; null means no
  /// human rationale exists for this row at all — an unattended automation
  /// grant, an expiry, a consumption, or an action that never records one.
  /// The two are deliberately not the same answer.
  final String? decisionComment;

  /// True when the backend had to shorten [decisionComment] to fit its
  /// 512-byte audit bound. Null means the stored record made no such claim,
  /// which is the normal case for a rationale that fit.
  final bool? decisionCommentTruncated;

  /// The denial counterpart of [decisionComment]. This is the person's
  /// sentence, not the tool-policy [reason] that explains why a call needed
  /// approval in the first place — they are separate fields on purpose.
  final String? denialReason;

  /// True when the backend had to shorten [denialReason]; see
  /// [decisionCommentTruncated].
  final bool? denialReasonTruncated;
}
