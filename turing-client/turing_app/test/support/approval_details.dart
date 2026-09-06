import 'package:turing_flutter_app/models/approval.dart';

ApprovalDetails approvalDetails({
  String approvalId = 'appr_1',
  String sessionId = 'sess_1',
  String runId = 'run_1',
  String toolName = 'files.update',
  String previewHash = 'sha256:preview',
  ApprovalPreviewState state = ApprovalPreviewState.ready,
  String status = 'pending',
  DateTime? expiresAt,
  bool canApprove = true,
  String unifiedDiff = '--- before\n+++ after\n@@ -1 +1 @@\n-old\n+new',
}) => ApprovalDetails(
  approvalId: approvalId,
  sessionId: sessionId,
  runId: runId,
  toolCallId: 'call_1',
  toolName: toolName,
  serverName: toolName.startsWith('files.') ? 'files' : 'shell',
  argsHash: 'sha256:arguments',
  previewHash: previewHash,
  expiresAt: expiresAt ?? DateTime.now().add(const Duration(minutes: 1)),
  status: status,
  previewState: state,
  argumentsJson: '{"path":"note.txt","content":"new"}',
  canApprove: canApprove,
  canDeny: status == 'pending',
  filePreview: toolName.startsWith('files.')
      ? FileMutationPreview(
          logicalPath: 'note.txt',
          physicalPath: 'sessions/sess_1/runs/run_1/files/note.txt',
          operation: 'files.update',
          beforeExists: true,
          beforeHash: 'sha256:before',
          afterHash: 'sha256:after',
          beforeText: 'old',
          afterText: 'new',
          unifiedDiff: unifiedDiff,
        )
      : null,
);
