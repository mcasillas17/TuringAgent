/// Which company an external agent belongs to.
///
/// Descriptive only: every one of these is reached over the same
/// OpenAI-compatible endpoint, so the choice changes the label and the
/// suggested URL, not how the request is made.
enum ExternalAgentProvider {
  anthropic('Anthropic', 'https://api.anthropic.com/v1'),
  openai('OpenAI', 'https://api.openai.com/v1'),
  google('Google', 'https://generativelanguage.googleapis.com/v1beta/openai'),
  xai('xAI', 'https://api.x.ai/v1'),
  other('Other OpenAI-compatible', ''),

  /// A value this build does not know, sent by a newer backend. Named rather
  /// than folded into [other], because the label says who receives the
  /// conversation and guessing that wrong is the one mistake this section
  /// cannot afford.
  unknown('Unrecognised provider', '');

  const ExternalAgentProvider(this.label, this.suggestedBaseUrl);

  final String label;

  /// A starting point for the endpoint field, not a guarantee. Vendors move
  /// these, so it is prefilled and editable rather than fixed.
  final String suggestedBaseUrl;

  /// The providers a person can pick. [unknown] is excluded: it describes
  /// something the backend sent, never something to choose.
  static List<ExternalAgentProvider> get selectable =>
      values.where((p) => p != unknown).toList(growable: false);
}

/// An assistant that does not run on this machine.
///
/// Turing's own assistant is not one of these — it is the default, it cannot
/// be removed, and it is the only destination that keeps a conversation local.
class ExternalAgent {
  const ExternalAgent({
    required this.agentId,
    required this.displayName,
    required this.provider,
    required this.baseUrl,
    required this.model,
    required this.credentialRef,
    required this.credentialAvailable,
  });

  final String agentId;
  final String displayName;
  final ExternalAgentProvider provider;
  final String baseUrl;
  final String model;

  /// The NAME of an API key, never the key. The key lives in the backend's
  /// environment; the client neither sends nor receives one.
  final String credentialRef;

  /// Whether the backend can currently find a key by that name. False means
  /// this agent will fail the moment it is used, which is worth saying before
  /// someone sends a message rather than after.
  final bool credentialAvailable;

  /// The part of the endpoint worth showing: who receives the conversation.
  String get endpointHost => Uri.tryParse(baseUrl)?.host.isNotEmpty == true
      ? Uri.parse(baseUrl).host
      : baseUrl;
}
