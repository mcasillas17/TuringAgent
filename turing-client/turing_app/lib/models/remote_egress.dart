enum EgressDataCategory {
  currentMessage('Current message'),
  conversationHistory('Conversation history'),
  crossSessionRecall('Cross-session recall'),
  memoryProfile('Memory and profile'),
  skillContent('Enabled skill content'),
  toolSchemas('Tool schemas'),
  toolArguments('Tool arguments'),
  toolResults('Tool results'),
  attachments('Attachments');

  const EgressDataCategory(this.label);

  final String label;
}

class RemoteEgressDisclosure {
  RemoteEgressDisclosure({
    required this.challenge,
    required this.provider,
    required this.model,
    required this.endpoint,
    required this.endpointHost,
    required List<EgressDataCategory> dataCategories,
    required this.expiresAt,
    this.externalAgentId = '',
  }) : dataCategories = List.unmodifiable(dataCategories);

  final String challenge;
  final String provider;
  final String model;
  final String endpoint;
  final String endpointHost;
  final String externalAgentId;
  final List<EgressDataCategory> dataCategories;
  final DateTime expiresAt;
}

class RemoteEgressConsent {
  RemoteEgressConsent({
    required this.challenge,
    required List<EgressDataCategory> acknowledgedDataCategories,
  }) : acknowledgedDataCategories = List.unmodifiable(
         acknowledgedDataCategories,
       );

  final String challenge;
  final List<EgressDataCategory> acknowledgedDataCategories;
}
