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

  test('AuditPayload holds only typed remote egress metadata', () {
    final consentedAt = DateTime.utc(2026, 8, 20, 1, 2, 3);
    final payload = AuditPayload(
      state: AuditPayloadState.present,
      provider: 'openai_compatible',
      endpointHost: 'api.example.com',
      egressDataCategories: const ['current_message', 'conversation_history'],
      egressDecisionVersion: 1,
      egressConsentGrantedAt: consentedAt,
    );

    expect(payload.endpointHost, 'api.example.com');
    expect(payload.egressDataCategories, [
      'current_message',
      'conversation_history',
    ]);
    expect(payload.egressDecisionVersion, 1);
    expect(payload.egressConsentGrantedAt, consentedAt);
    expect(
      () => payload.egressDataCategories.add('tool_results'),
      throwsUnsupportedError,
    );
  });

  // The MCP registry read API projects ten action-typed fields (see
  // audit/service.go's applyAuditActionPolicy): the model has to be able to
  // hold every one of them, including their falsy-but-present values, the
  // same "absence is the answer" rule every other optional in this class
  // already follows.
  test(
    'AuditPayload holds typed MCP registry fields, including falsy ones',
    () {
      const populated = AuditPayload(
        state: AuditPayloadState.present,
        serverName: 'vendor',
        mcpServerTier: 'remote_url',
        mcpServerUrl: 'https://vendor.example/mcp',
        adopted: false,
        tokenConfigured: false,
        remoteDiscoveryAttempted: false,
        discoverySucceeded: false,
        importedServers: 0,
        skippedServers: 0,
        refusedServers: 0,
        toolName: 'vendor.write',
        toolPolicy: 'disabled',
      );

      expect(populated.mcpServerTier, 'remote_url');
      expect(populated.mcpServerUrl, 'https://vendor.example/mcp');
      expect(populated.adopted, isFalse);
      expect(populated.tokenConfigured, isFalse);
      expect(populated.remoteDiscoveryAttempted, isFalse);
      expect(populated.discoverySucceeded, isFalse);
      expect(populated.importedServers, 0);
      expect(populated.skippedServers, 0);
      expect(populated.refusedServers, 0);
      expect(populated.toolPolicy, 'disabled');

      const none = AuditPayload(state: AuditPayloadState.present);
      expect(none.mcpServerTier, isNull);
      expect(none.mcpServerUrl, isNull);
      expect(none.adopted, isNull);
      expect(none.tokenConfigured, isNull);
      expect(none.remoteDiscoveryAttempted, isNull);
      expect(none.discoverySucceeded, isNull);
      expect(none.importedServers, isNull);
      expect(none.skippedServers, isNull);
      expect(none.refusedServers, isNull);
      expect(none.toolPolicy, isNull);
    },
  );
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
