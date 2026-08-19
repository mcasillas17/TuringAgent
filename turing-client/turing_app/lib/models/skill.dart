/// A file-backed skill and the local decisions that control whether it loads.
///
/// The descriptive fields and body are read from `SKILL.md`. Only [enabled]
/// and the capability grants are stored by TuringAgent.
class Skill {
  const Skill({
    required this.skillId,
    required this.name,
    required this.description,
    required this.body,
    required this.category,
    required this.version,
    required this.author,
    required this.license,
    required this.requires,
    required this.grantedCapabilities,
    required this.missingCapabilities,
    required this.enabled,
    required this.parseError,
    required this.folderPath,
  });

  final String skillId;
  final String name;
  final String description;
  final String body;
  final String category;
  final String version;
  final String author;
  final String license;
  final List<String> requires;
  final List<String> grantedCapabilities;
  final List<String> missingCapabilities;
  final bool enabled;
  final String parseError;
  final String folderPath;

  Skill copyWith({
    String? skillId,
    String? name,
    String? description,
    String? body,
    String? category,
    String? version,
    String? author,
    String? license,
    List<String>? requires,
    List<String>? grantedCapabilities,
    List<String>? missingCapabilities,
    bool? enabled,
    String? parseError,
    String? folderPath,
  }) => Skill(
    skillId: skillId ?? this.skillId,
    name: name ?? this.name,
    description: description ?? this.description,
    body: body ?? this.body,
    category: category ?? this.category,
    version: version ?? this.version,
    author: author ?? this.author,
    license: license ?? this.license,
    requires: requires ?? this.requires,
    grantedCapabilities: grantedCapabilities ?? this.grantedCapabilities,
    missingCapabilities: missingCapabilities ?? this.missingCapabilities,
    enabled: enabled ?? this.enabled,
    parseError: parseError ?? this.parseError,
    folderPath: folderPath ?? this.folderPath,
  );
}
