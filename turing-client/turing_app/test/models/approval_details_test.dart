import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/models/approval.dart';

import '../support/approval_details.dart';

void main() {
  ApprovalDetails details({
    ApprovalPreviewState state = ApprovalPreviewState.ready,
    String argsHash = 'sha256:arguments',
    String previewHash = 'sha256:preview',
    String status = 'pending',
    bool canApprove = true,
    bool includeFile = true,
    DateTime? expiresAt,
  }) => ApprovalDetails(
    approvalId: 'appr_1',
    sessionId: 'sess_1',
    runId: 'run_1',
    toolCallId: 'call_1',
    toolName: 'files.update',
    serverName: 'files',
    argsHash: argsHash,
    previewHash: previewHash,
    expiresAt: expiresAt ?? DateTime.utc(2030),
    status: status,
    previewState: state,
    argumentsJson: '{"path":"note.txt"}',
    canApprove: canApprove,
    canDeny: true,
    filePreview: includeFile ? approvalDetails().filePreview : null,
  );

  test('approval requires an unexpired complete server binding', () {
    final now = DateTime.utc(2026);
    expect(details().canApproveAt(now), isTrue);
    expect(details(argsHash: '').canApproveAt(now), isFalse);
    expect(details(previewHash: '').canApproveAt(now), isFalse);
    expect(details(status: 'approved').canApproveAt(now), isFalse);
    expect(details(canApprove: false).canApproveAt(now), isFalse);
    expect(details(includeFile: false).canApproveAt(now), isFalse);
    expect(details(expiresAt: now).canApproveAt(now), isFalse);
  });

  test('unavailable or unknown file effects never enable approval', () {
    for (final state in ApprovalPreviewState.values) {
      if (state == ApprovalPreviewState.ready) continue;
      expect(
        details(state: state).canApproveAt(DateTime.utc(2026)),
        isFalse,
        reason: '$state is not a complete file preview',
      );
    }
  });

  test('third-party effects are explicitly unsupported, not invented', () {
    final value = approvalDetails(
      toolName: 'shell.exec',
      state: ApprovalPreviewState.unsupported,
    );
    expect(value.filePreview, isNull);
    expect(value.canApproveAt(DateTime.now()), isTrue);
  });
}
