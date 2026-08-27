enum EgressDataCategory {
  currentMessage('current_message'),
  conversationHistory('conversation_history'),
  crossSessionRecall('cross_session_recall'),
  memoryProfile('memory_profile'),
  skillContent('skill_content'),
  toolSchemas('tool_schemas'),
  toolArguments('tool_arguments'),
  toolResults('tool_results'),
  attachments('attachments');

  const EgressDataCategory(this.wireName);

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
  ///
  /// The rule is the backend's `IsMemoryToolName`, character for character: the
  /// separator must be there and something must follow it. A bare `memory` is
  /// not a callable tool, and a third-party server called `memoryx` does not
  /// get to borrow the category.
  List<String> get memoryTools => List.unmodifiable(
    selectedTools.where(
      (tool) =>
          tool.startsWith(_memoryToolPrefix) &&
          tool.length > _memoryToolPrefix.length,
    ),
  );

  /// The documents that are already in the prompt, word for word.
  ///
  /// Only the two pinned documents qualify. Everything else the server
  /// discloses is memory a tool could go and read, which is a different promise
  /// and gets a different sentence.
  List<MemoryEgressDisclosure> get pinnedMemory => List.unmodifiable(
    memoryNotes.where(
      (note) =>
          note.tier == MemoryEgressTier.persona ||
          note.tier == MemoryEgressTier.profile,
    ),
  );

  /// The memory a tool or the graph could reach during this run.
  ///
  /// A tier this build cannot name lands here rather than among the pinned
  /// documents: disclosing it is honest, and calling it pinned would not be.
  List<MemoryEgressDisclosure> get toolReachableMemory => List.unmodifiable(
    memoryNotes.where(
      (note) =>
          note.tier != MemoryEgressTier.persona &&
          note.tier != MemoryEgressTier.profile,
    ),
  );

  /// Whether this run touches memory at all, and so whether the dialog says
  /// anything about it.
  ///
  /// The disclosed category and the applicability flag are the server's answer
  /// arriving by two fields, and either one on its own is enough to speak up.
  /// A named vault entry is deliberately *not* enough: the server derives both
  /// fields from the same applicability decision that decides whether to name
  /// entries at all, so an entry arriving without either one is a contradiction
  /// — and resolving it towards "memory is in play" would tell the user their
  /// persona is being sent on a run that the server just said does not send it.
  bool get mentionsMemory =>
      memoryProfileMayBeSent ||
      dataCategories.contains(EgressDataCategory.memoryProfile);
}

const String _memoryToolPrefix = 'memory/';

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
