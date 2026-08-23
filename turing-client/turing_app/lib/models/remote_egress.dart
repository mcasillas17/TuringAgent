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
    List<RemoteMcpDestination> remoteMcpServers = const [],
    List<String> selectedTools = const [],
    List<IntegrationEgressDestination> integrationEndpoints = const [],
    List<SkillEgressDisclosure> skills = const [],
  }) : dataCategories = List.unmodifiable(dataCategories),
       remoteMcpServers = List.unmodifiable(remoteMcpServers),
       selectedTools = List.unmodifiable(selectedTools),
       integrationEndpoints = List.unmodifiable(integrationEndpoints),
       skills = List.unmodifiable(skills);

  final String challenge;
  final String provider;
  final String model;
  final String endpoint;
  final String endpointHost;
  final String externalAgentId;
  final List<EgressDataCategory> dataCategories;
  final List<RemoteMcpDestination> remoteMcpServers;
  final List<String> selectedTools;
  final List<IntegrationEgressDestination> integrationEndpoints;
  final List<SkillEgressDisclosure> skills;
  final DateTime expiresAt;
}

class SkillEgressDisclosure {
  const SkillEgressDisclosure({
    required this.skillId,
    required this.displayName,
    required this.bodyMayBeSent,
  });

  final String skillId;
  final String displayName;
  final bool bodyMayBeSent;
}

class IntegrationEgressDestination {
  const IntegrationEgressDestination({
    required this.endpoint,
    required this.endpointHost,
    required this.connectionId,
    required this.displayName,
    this.tools = const [],
  });

  final String endpoint;
  final String endpointHost;
  final String connectionId;
  final String displayName;
  final List<String> tools;
}

class RemoteMcpDestination {
  const RemoteMcpDestination({
    required this.serverName,
    required this.endpoint,
    required this.endpointHost,
  });

  final String serverName;
  final String endpoint;
  final String endpointHost;
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
