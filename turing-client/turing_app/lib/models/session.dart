class Session {
  Session({
    required this.sessionId,
    required this.title,
    required this.updatedAt,
    int? updatedAtNanoseconds,
  }) : updatedAtNanoseconds =
           updatedAtNanoseconds ?? updatedAt.microsecondsSinceEpoch * 1000;

  final String sessionId;
  final String? title;
  final DateTime updatedAt;
  final int updatedAtNanoseconds;

  factory Session.fromJson(Map<String, dynamic> json) {
    return Session(
      sessionId: (json['sessionId'] ?? json['id']) as String,
      title: json['title'] as String?,
      updatedAt: DateTime.parse(
        (json['updatedAt'] ?? json['createdAt']) as String,
      ),
    );
  }
}
