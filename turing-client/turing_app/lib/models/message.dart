import 'run_state.dart';

class Message {
  const Message({
    required this.messageId,
    this.runId,
    this.runState,
    required this.role,
    required this.content,
    this.contentType = 'text',
    required this.sequence,
    required this.createdAt,
  });

  final String messageId;
  final String? runId;
  final RunState? runState;
  final String role;
  final String content;

  /// What kind of row this is beyond its role — `delegation_results` (a join)
  /// and `delegation_notice` render as their own rows, not as answers.
  final String contentType;
  final int sequence;
  final DateTime createdAt;

  factory Message.fromJson(Map<String, dynamic> json) {
    final rawRunId = json['runId'] ?? json['run_id'];
    return Message(
      messageId: (json['messageId'] ?? json['id']) as String,
      runId: rawRunId is String && rawRunId.isNotEmpty ? rawRunId : null,
      runState: null,
      role: json['role'] as String,
      content: json['content'] as String,
      contentType: json['contentType'] as String? ?? 'text',
      sequence: (json['sequence'] as num).toInt(),
      createdAt: DateTime.parse(json['createdAt'] as String),
    );
  }
}
