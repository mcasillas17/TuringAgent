class TuringEvent {
  const TuringEvent({
    required this.eventId,
    required this.sessionId,
    this.runId,
    required this.traceId,
    required this.sequence,
    required this.type,
    required this.createdAt,
    required this.payload,
  });

  final String eventId;
  final String sessionId;
  final String? runId;
  final String traceId;
  final int sequence;
  final String type;
  final DateTime createdAt;
  final Map<String, dynamic> payload;

  factory TuringEvent.fromJson(Map<String, dynamic> json) {
    return TuringEvent(
      eventId: json['eventId'] as String,
      sessionId: json['sessionId'] as String,
      runId: json['runId'] as String?,
      traceId: json['traceId'] as String,
      sequence: (json['sequence'] as num).toInt(),
      type: json['type'] as String,
      createdAt: DateTime.parse(json['createdAt'] as String),
      payload: Map<String, dynamic>.from(json['payload'] as Map),
    );
  }
}

class TuringEventPage {
  const TuringEventPage({required this.events, required this.latestSequence});

  final List<TuringEvent> events;
  final int latestSequence;
}
