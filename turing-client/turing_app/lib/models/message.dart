class Message {
  const Message({
    required this.messageId,
    this.runId,
    required this.role,
    required this.content,
    required this.sequence,
    required this.createdAt,
  });

  final String messageId;
  final String? runId;
  final String role;
  final String content;
  final int sequence;
  final DateTime createdAt;

  factory Message.fromJson(Map<String, dynamic> json) {
    final rawRunId = json['runId'] ?? json['run_id'];
    return Message(
      messageId: (json['messageId'] ?? json['id']) as String,
      runId: rawRunId is String && rawRunId.isNotEmpty ? rawRunId : null,
      role: json['role'] as String,
      content: json['content'] as String,
      sequence: (json['sequence'] as num).toInt(),
      createdAt: DateTime.parse(json['createdAt'] as String),
    );
  }
}
