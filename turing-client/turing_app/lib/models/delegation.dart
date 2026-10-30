/// A task Turing delegated to a specialist, as its card in the conversation
/// shows it.
class Delegation {
  const Delegation({
    required this.delegationId,
    required this.parentRunId,
    this.childSessionId = '',
    this.childRunId = '',
    this.displayName = '',
    this.emoji = '',
    this.task = '',
    this.state = 'queued',
    this.approvalId = '',
    this.summary = '',
    this.continuationRunId = '',
    this.egressTools = const [],
    this.egressConsent = '',
    this.egressRequired = false,
  });

  final String delegationId;

  /// The Turing turn that asked; its card sits under that turn's answer.
  final String parentRunId;

  /// The specialist's own hidden conversation and run, which View work reads
  /// and Cancel stops.
  final String childSessionId;
  final String childRunId;
  final String displayName;
  final String emoji;

  /// What Turing asked for, as the model wrote it.
  final String task;

  /// queued, running, waiting_approval, completed, failed or cancelled.
  final String state;

  /// The approval the task waits on, while it waits.
  final String approvalId;

  /// One line written by the orchestrator, never by a model.
  final String summary;

  /// The run that answered from this task's result once it was joined.
  final String continuationRunId;

  /// Tools that leave the machine which the task asked the user's consent
  /// for, where that consent stands (pending, granted, declined, or empty
  /// when none was needed), and whether declining ends the task.
  final List<String> egressTools;
  final String egressConsent;
  final bool egressRequired;

  /// The task waits, unclaimed, for the user to allow or decline its tools.
  bool get waitingForConsent => state == 'waiting_consent';

  String get specialist => '$emoji $displayName'.trim();

  bool get isTerminal =>
      state == 'completed' || state == 'failed' || state == 'cancelled';

  /// The card after a `delegation.*` event about it. Every event carries the
  /// card's whole displayed state, so its state, summary and approval replace
  /// what an earlier one said; the task, names and continuation are kept when
  /// an event leaves them out. An event about another delegation changes
  /// nothing, and one that names no delegation or no turn makes no card.
  static Delegation? fromEvent(
    Delegation? current,
    Map<String, dynamic> payload,
  ) {
    String read(String key) {
      final value = payload[key];
      return value is String ? value : '';
    }

    String keep(String key, String old) {
      final value = read(key);
      return value.isEmpty ? old : value;
    }

    final tools = payload['egressTools'];
    final required = payload['egressRequired'];

    final id = read('delegationId');
    if (current != null && current.delegationId != id) return current;
    final parentRunId = keep('parentRunId', current?.parentRunId ?? '');
    if (id.isEmpty || parentRunId.isEmpty) return current;
    return Delegation(
      delegationId: id,
      parentRunId: parentRunId,
      childSessionId: keep('childSessionId', current?.childSessionId ?? ''),
      childRunId: keep('childRunId', current?.childRunId ?? ''),
      displayName: keep('displayName', current?.displayName ?? ''),
      emoji: keep('emoji', current?.emoji ?? ''),
      task: keep('task', current?.task ?? ''),
      state: keep('state', current?.state ?? 'queued'),
      approvalId: read('approvalId'),
      summary: keep('summary', current?.summary ?? ''),
      continuationRunId: keep(
        'continuationRunId',
        current?.continuationRunId ?? '',
      ),
      egressTools: tools is List && tools.isNotEmpty
          ? List.unmodifiable(tools.whereType<String>())
          : current?.egressTools ?? const [],
      egressConsent: keep('egressConsent', current?.egressConsent ?? ''),
      egressRequired: required is bool
          ? required
          : current?.egressRequired ?? false,
    );
  }
}
