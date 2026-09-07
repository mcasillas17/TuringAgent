class Approval {
  const Approval({
    required this.approvalId,
    required this.toolName,
    required this.argsSummary,
    required this.status,
  });

  final String approvalId;
  final String toolName;
  final String argsSummary;
  final String status;

  factory Approval.fromJson(Map<String, dynamic> json) {
    return Approval(
      approvalId: json['approvalId'] as String,
      toolName: json['toolName'] as String,
      argsSummary: json['argsSummary'] as String? ?? '',
      status: json['status'] as String,
    );
  }
}

enum ApprovalPreviewState {
  unknown,
  ready,
  unavailable,
  unsupported,
  redacted,
  oversized,
  binary,
  expired,
  stale,
  terminal,
}

class FileMutationPreview {
  const FileMutationPreview({
    required this.logicalPath,
    required this.physicalPath,
    required this.operation,
    required this.beforeExists,
    required this.beforeHash,
    required this.afterHash,
    required this.beforeText,
    required this.afterText,
    required this.unifiedDiff,
  });

  final String logicalPath;
  final String physicalPath;
  final String operation;
  final bool beforeExists;
  final String beforeHash;
  final String afterHash;
  final String beforeText;
  final String afterText;
  final String unifiedDiff;
}

class ApprovalDetails {
  const ApprovalDetails({
    required this.approvalId,
    required this.sessionId,
    required this.runId,
    required this.toolCallId,
    required this.toolName,
    required this.serverName,
    required this.argsHash,
    required this.previewHash,
    required this.expiresAt,
    required this.status,
    required this.previewState,
    required this.argumentsJson,
    required this.canApprove,
    required this.canDeny,
    this.filePreview,
  });

  final String approvalId;
  final String sessionId;
  final String runId;
  final String toolCallId;
  final String toolName;
  final String serverName;
  final String argsHash;
  final String previewHash;
  final DateTime? expiresAt;
  final String status;
  final ApprovalPreviewState previewState;
  final String argumentsJson;
  final bool canApprove;
  final bool canDeny;
  final FileMutationPreview? filePreview;

  bool canApproveAt(DateTime now) {
    final expiry = expiresAt;
    final file = filePreview;
    if (serverName == 'files' &&
        (file == null ||
            file.physicalPath.isEmpty ||
            file.afterHash.isEmpty ||
            (file.beforeExists && file.beforeHash.isEmpty))) {
      return false;
    }
    return canApprove &&
        status == 'pending' &&
        approvalId.isNotEmpty &&
        sessionId.isNotEmpty &&
        runId.isNotEmpty &&
        toolCallId.isNotEmpty &&
        argsHash.isNotEmpty &&
        previewHash.isNotEmpty &&
        expiry != null &&
        now.isBefore(expiry) &&
        (previewState == ApprovalPreviewState.ready ||
            (previewState == ApprovalPreviewState.unsupported &&
                serverName != 'files'));
  }
}
