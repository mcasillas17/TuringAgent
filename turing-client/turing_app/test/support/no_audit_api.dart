import 'package:turing_flutter_app/models/audit.dart';

/// Fills in the audit surface for fakes belonging to tests that are not
/// about audit reads.
///
/// The read answers empty rather than throwing: a page under test may
/// legitimately never open the audit surface, so a fake that reaches it
/// anyway gets an honestly empty page instead of a crash. This mirrors
/// [NoTelemetryApi] in the neighbouring file.
mixin NoAuditApi {
  Future<AuditPage> listAuditEntries({
    String? correlationId,
    String? action,
    DateTime? createdAtStart,
    DateTime? createdAtEnd,
    AuditOrder order = AuditOrder.descending,
    int limit = 50,
    String? cursor,
  }) async => AuditPage(entries: const [], nextCursor: null);
}
