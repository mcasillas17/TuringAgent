enum SessionStatus { active, archived }

class Session {
  Session({
    required this.sessionId,
    required this.title,
    required this.updatedAt,
    int? updatedAtNanoseconds,
    this.status = SessionStatus.active,
  }) : updatedAtNanoseconds =
           updatedAtNanoseconds ?? updatedAt.microsecondsSinceEpoch * 1000;

  final String sessionId;
  final String? title;
  final DateTime updatedAt;
  final int updatedAtNanoseconds;
  final SessionStatus status;

  factory Session.fromJson(Map<String, dynamic> json) {
    return Session(
      sessionId: (json['sessionId'] ?? json['id']) as String,
      title: json['title'] as String?,
      updatedAt: DateTime.parse(
        (json['updatedAt'] ?? json['createdAt']) as String,
      ),
      status: switch (json['status'] as String?) {
        null || 'active' => SessionStatus.active,
        'archived' => SessionStatus.archived,
        final value => throw FormatException('invalid session status: $value'),
      },
    );
  }
}
