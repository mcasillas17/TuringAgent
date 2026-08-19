import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/models/audit.dart';

void main() {
  test('AuditPage copies entries and exposes an unmodifiable list', () {
    final sourceEntries = [_entry('audit-1')];

    final page = AuditPage(entries: sourceEntries, nextCursor: 'cursor-1');

    sourceEntries.add(_entry('audit-2'));

    expect(page.entries, hasLength(1));
    expect(page.entries.single.auditId, 'audit-1');
    expect(page.nextCursor, 'cursor-1');
    expect(() => page.entries[0] = _entry('audit-3'), throwsUnsupportedError);
  });

  // TUR-002 records what the person typed when they approved or denied; the
  // model has to be able to hold that answer exactly, including the two
  // answers that are easy to lose: "they typed nothing" and "no human field
  // was ever recorded". Those are an empty string and null, never the same
  // thing.
  test('AuditPayload holds an approval rationale, including an empty one', () {
    const withRationale = AuditPayload(
      state: AuditPayloadState.present,
      decisionComment: 'looked at the diff, fine',
      decisionCommentTruncated: true,
      denialReason: 'path is outside the sandbox',
      denialReasonTruncated: false,
    );

    expect(withRationale.decisionComment, 'looked at the diff, fine');
    expect(withRationale.decisionCommentTruncated, isTrue);
    expect(withRationale.denialReason, 'path is outside the sandbox');
    expect(withRationale.denialReasonTruncated, isFalse);

    const typedEmpty = AuditPayload(
      state: AuditPayloadState.present,
      decisionComment: '',
      denialReason: '',
    );

    expect(typedEmpty.decisionComment, '');
    expect(typedEmpty.decisionCommentTruncated, isNull);
    expect(typedEmpty.denialReason, '');
    expect(typedEmpty.denialReasonTruncated, isNull);

    const noHumanField = AuditPayload(state: AuditPayloadState.present);

    expect(noHumanField.decisionComment, isNull);
    expect(noHumanField.decisionCommentTruncated, isNull);
    expect(noHumanField.denialReason, isNull);
    expect(noHumanField.denialReasonTruncated, isNull);
  });
}

AuditEntry _entry(String auditId) {
  return AuditEntry(
    auditId: auditId,
    correlationId: null,
    actorType: 'user',
    actorId: null,
    action: 'action',
    target: null,
    payload: AuditPayload(state: AuditPayloadState.absent),
    createdAt: DateTime.utc(2026, 8, 19),
  );
}
