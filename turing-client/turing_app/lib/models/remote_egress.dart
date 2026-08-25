enum EgressDataCategory {
  currentMessage('Current message', 'current_message'),
  conversationHistory('Conversation history', 'conversation_history'),
  crossSessionRecall('Cross-session recall', 'cross_session_recall'),
  memoryProfile('Memory and profile', 'memory_profile'),
  skillContent('Enabled skill content', 'skill_content'),
  toolSchemas('Tool schemas', 'tool_schemas'),
  toolArguments('Tool arguments', 'tool_arguments'),
  toolResults('Tool results', 'tool_results'),
  attachments('Attachments', 'attachments');

  const EgressDataCategory(this.label, this.wireName);

  final String label;

  /// The backend's own name for this category. Carried so a test can pin this
  /// list against the wire enum: a category the server can disclose but this
  /// client cannot name would make the consent dialog quietly shorter than the
  /// run it describes.
  final String wireName;
}

/// Which part of the vault a disclosed memory belongs to. Mirrors the wire
/// enum, with [unspecified] kept as its own member so a tier this build does
/// not recognise is never displayed as one it does.
enum MemoryEgressTier { unspecified, persona, profile, belief, note }

/// One memory that would leave the machine, named so the user can go and read
/// it before consenting.
///
/// It carries a title and a vault path and never the signed snapshot
/// fingerprint the run-owned decision uses internally — the fingerprint is a
/// binding token, not something a person can check, and showing it would only
/// teach the user to click past a hex string.
class MemoryEgressDisclosure {
  const MemoryEgressDisclosure({
    required this.noteId,
    required this.title,
    required this.vaultPath,
    required this.tier,
    required this.bodyMayBeSent,
  });

  final String noteId;
  final String title;
  final String vaultPath;
  final MemoryEgressTier tier;
  final bool bodyMayBeSent;
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
    List<MemoryEgressDisclosure> memoryNotes = const [],
    this.memoryProfileMayBeSent = false,
  }) : dataCategories = List.unmodifiable(dataCategories),
       remoteMcpServers = List.unmodifiable(remoteMcpServers),
       selectedTools = List.unmodifiable(selectedTools),
       integrationEndpoints = List.unmodifiable(integrationEndpoints),
       skills = List.unmodifiable(skills),
       memoryNotes = List.unmodifiable(memoryNotes);

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

  /// The pinned documents and beliefs this run may carry, note by note.
  final List<MemoryEgressDisclosure> memoryNotes;

  /// The server's own answer to "does this run touch memory at all" — true when
  /// something is pinned or memory tools are selected. The client renders the
  /// memory section on the disclosed category, and uses this to tell a run with
  /// pinned content apart from one that only has the tools available.
  final bool memoryProfileMayBeSent;
  final DateTime expiresAt;

  /// The memory tools frozen into this run, if any. Derived from the selected
  /// tool set rather than a separate field so it cannot disagree with it.
  List<String> get memoryTools => List.unmodifiable(
    selectedTools.where(
      (tool) => tool == 'memory' || tool.startsWith('memory/'),
    ),
  );
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
