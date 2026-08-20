enum SessionDeletionState { inProgress, failedExternal, completed }

class SessionDeletionReceipt {
  const SessionDeletionReceipt({
    required this.sessionId,
    required this.state,
    required this.retryable,
    this.errorCode,
    this.lifecycleVersion = 0,
    this.terminalSequence = 0,
    this.runCount = 0,
    this.messageCount = 0,
    this.retainedLegacyArtifactCount = 0,
  });

  const SessionDeletionReceipt.inProgress()
    : sessionId = '',
      state = SessionDeletionState.inProgress,
      retryable = true,
      errorCode = null,
      lifecycleVersion = 0,
      terminalSequence = 0,
      runCount = 0,
      messageCount = 0,
      retainedLegacyArtifactCount = 0;

  const SessionDeletionReceipt.completed()
    : sessionId = '',
      state = SessionDeletionState.completed,
      retryable = false,
      errorCode = null,
      lifecycleVersion = 0,
      terminalSequence = 0,
      runCount = 0,
      messageCount = 0,
      retainedLegacyArtifactCount = 0;

  final String sessionId;
  final SessionDeletionState state;
  final bool retryable;
  final String? errorCode;
  final int lifecycleVersion;
  final int terminalSequence;
  final int runCount;
  final int messageCount;
  final int retainedLegacyArtifactCount;
}
