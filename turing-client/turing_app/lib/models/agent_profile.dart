/// Where a specialist profile stands. Exactly one applies.
enum AgentProfileState {
  active,
  disabled,
  needsGrant,
  unavailable,
  parseError,

  /// A state this build does not know. Never treated as active.
  unknown,
}

/// How much of the memory vault a specialist's runs may touch.
enum AgentProfileMemoryAccess {
  none,

  /// memory.search and memory.read.
  read,

  /// Also memory.remember, which still needs an approval per call.
  propose,

  /// A level this build does not know.
  unknown,
}

/// A registered tool the profile's patterns match that its runs would still
/// not receive, and why.
class AgentProfileToolExclusion {
  const AgentProfileToolExclusion({required this.tool, required this.reason});

  final String tool;
  final String reason;
}

/// A `team/<id>/AGENT.md` file and the user's decisions about it.
///
/// Everything except [enabled] and [grantedRevision] is read from the file by
/// the backend on every call. A grant is bound to [revision], so granting
/// sends back exactly the revision the user reviewed.
class AgentProfile {
  const AgentProfile({
    required this.profileId,
    required this.name,
    required this.emoji,
    required this.description,
    required this.version,
    required this.model,
    required this.resolvedModel,
    required this.tools,
    required this.skills,
    required this.memory,
    required this.requires,
    required this.maxToolCalls,
    required this.revision,
    required this.enabled,
    required this.grantedRevision,
    required this.state,
    required this.resolvedTools,
    required this.unavailableReasons,
    required this.excludedTools,
    required this.parseError,
  });

  final String profileId;
  final String name;
  final String emoji;
  final String description;
  final String version;

  /// The declared local model. Empty means Turing's default model.
  final String model;

  /// The model a delegated run would use now. Empty when no worker serves one.
  final String resolvedModel;

  /// The declared tool patterns.
  final List<String> tools;

  /// The declared skill patterns.
  final List<String> skills;
  final AgentProfileMemoryAccess memory;

  /// Tool patterns that must resolve for the profile to be available.
  final List<String> requires;

  /// The declared limit. Zero means the worker's own per-run limit.
  final int maxToolCalls;
  final String revision;
  final bool enabled;

  /// The revision the user granted, or empty.
  final String grantedRevision;
  final AgentProfileState state;

  /// The tools a delegated run would receive now, qualified as server/tool.
  final List<String> resolvedTools;
  final List<String> unavailableReasons;
  final List<AgentProfileToolExclusion> excludedTools;
  final String parseError;

  bool get grantsCurrentRevision =>
      revision.isNotEmpty && grantedRevision == revision;

  /// The name as the user knows it, falling back to the folder name for a
  /// file that did not parse far enough to have one.
  String get label => name.isEmpty ? profileId : name;
}
