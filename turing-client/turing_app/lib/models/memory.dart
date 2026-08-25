/// The vault as this client understands it.
///
/// Memory is Markdown files on the user's disk, so every message here reports
/// what was read *and* the reason something could not be. A field that is empty
/// because the vault is broken is never the same thing as a field that is empty
/// because there is nothing there, and these types keep the two apart.
library;

enum MemoryTier { unspecified, persona, profile, belief, note }

enum MemoryCandidateKind { unspecified, belief, profileEdit }

enum MemoryCandidateState {
  unspecified,
  pending,
  promoted,
  rejected,

  /// The conversation that produced the proposal was deleted. The row survives
  /// so the record does not vanish, and it can no longer be decided.
  withdrawn,
}

/// Whether Turing may rewrite the file.
enum MemoryNoteStatus { unspecified, managed, unmanaged, withdrawn }

enum MemoryProvenanceKind {
  unspecified,
  promotedFromCandidate,
  userAuthored,
  imported,
}

/// Why a read produced nothing.
///
/// [none] is deliberately distinct from [unspecified]: the first is the server
/// saying nothing is wrong, the second is the server not saying. Collapsing
/// them would render a vault nobody could read as a healthy empty one.
enum MemoryUnavailableReason {
  unspecified,
  none,
  disabled,
  vaultMissing,
  vaultUnreadable,
  contentParseFailed,
  contentTooLarge,
}

class MemorySettings {
  const MemorySettings({
    required this.enabled,
    this.vaultRoot = '',
    this.vaultWritable = false,
    this.unavailableReason = MemoryUnavailableReason.unspecified,
    this.parseError = '',
  });

  final bool enabled;

  /// Absolute path, so the user can open the vault in their own editor.
  final String vaultRoot;
  final bool vaultWritable;
  final MemoryUnavailableReason unavailableReason;
  final String parseError;
}

class MemoryTierState {
  const MemoryTierState({
    required this.tier,
    required this.enabled,
    this.noteCount = 0,
    this.pendingCandidateCount = 0,
    this.updatedAt,
    this.unavailableReason = MemoryUnavailableReason.unspecified,
    this.parseError = '',
  });

  final MemoryTier tier;
  final bool enabled;
  final int noteCount;
  final int pendingCandidateCount;
  final DateTime? updatedAt;
  final MemoryUnavailableReason unavailableReason;
  final String parseError;
}

/// One of the two authored documents — `persona.md` or `profile.md`.
///
/// [content] is the whole document as it sits on disk, and [contentHash] is the
/// compare-and-set token taken over exactly those bytes: an editor saves against
/// the hash it read, so a save composed against text the vault has since moved
/// on from is refused instead of overwriting words the user never saw.
///
/// A run does not necessarily carry all of it. [pinnedTruncated] says whether
/// the model sees less than what is shown here, and [pinnedBytes] says how much
/// of the document reaches a conversation. Both describe the runtime's reading,
/// not the editor's — nothing here is ever trimmed, and the truncation notice
/// the runtime appends for the model is not part of [content].
class MemoryDocument {
  const MemoryDocument({
    this.content = '',
    this.contentHash = '',
    this.status = MemoryNoteStatus.unspecified,
    this.updatedAt,
    this.parseError = '',
    this.unavailableReason = MemoryUnavailableReason.unspecified,
    this.pinnedTruncated = false,
    this.pinnedBytes = 0,
  });

  final String content;
  final String contentHash;
  final MemoryNoteStatus status;
  final DateTime? updatedAt;
  final String parseError;
  final MemoryUnavailableReason unavailableReason;

  /// Whether a run sees less of this document than the editor shows.
  final bool pinnedTruncated;

  /// How many of the document's own bytes reach a conversation.
  final int pinnedBytes;

  /// Whether this client may offer to write the document at all.
  ///
  /// This is about the file, never about what the user typed into it: an empty
  /// persona is a save the user is entitled to make, and refusing it would
  /// leave "take back what I told the model" as something only reachable by
  /// leaving Turing. What is refused is a save the server would refuse anyway,
  /// because the document could not be read — unreadable, a symlink, too large,
  /// or a state this build cannot name.
  ///
  /// Two reasons look like refusals and are not. Memory being switched off
  /// leaves the vault on disk and still the user's. And a document that is
  /// simply not there yet is the ordinary first-run state: every vault starts
  /// without a persona, and the way one comes to exist is that someone writes
  /// it from here. Refusing that save would make the Memory page permanently
  /// read-only on a fresh install — the file cannot appear until it is written,
  /// and it cannot be written until it appears.
  ///
  /// A save into a missing document carries an empty [contentHash], which is
  /// the same token the server already reads as "I am creating this" — so a
  /// second writer that got there first is still caught rather than overwritten.
  bool get isWritable =>
      unavailableReason == MemoryUnavailableReason.none ||
      unavailableReason == MemoryUnavailableReason.disabled ||
      unavailableReason == MemoryUnavailableReason.vaultMissing;
}

/// Where a claim came from, and whether it still stands.
class MemoryProvenance {
  const MemoryProvenance({
    required this.kind,
    this.sourceSessionId = '',
    this.sourceSessionTitle = '',
    this.observedAt,
    this.withdrawn = false,
    this.withdrawnAt,
    this.evidenceCount = 0,
  });

  final MemoryProvenanceKind kind;
  final String sourceSessionId;
  final String sourceSessionTitle;
  final DateTime? observedAt;

  /// Set once the originating session is gone. The note survives deletion, so
  /// this is how a client says the evidence behind it no longer exists rather
  /// than presenting an unsupported claim as supported.
  final bool withdrawn;
  final DateTime? withdrawnAt;

  /// Count only. Excerpts never leave the vault.
  final int evidenceCount;
}

/// An accepted memory: a Markdown file in the user's vault.
class MemoryNote {
  MemoryNote({
    required this.noteId,
    required this.path,
    required this.title,
    required this.content,
    required this.contentHash,
    required this.status,
    required this.tier,
    List<MemoryProvenance> provenance = const [],
    this.createdAt,
    this.updatedAt,
    this.parseError = '',
    this.unavailableReason = MemoryUnavailableReason.unspecified,
  }) : provenance = List.unmodifiable(provenance);

  final String noteId;
  final String path;
  final String title;
  final String content;
  final String contentHash;
  final MemoryNoteStatus status;
  final MemoryTier tier;
  final List<MemoryProvenance> provenance;
  final DateTime? createdAt;
  final DateTime? updatedAt;
  final String parseError;
  final MemoryUnavailableReason unavailableReason;

  /// A note the vault could not parse is not in the search index, and saying so
  /// is the difference between "Turing has not used this" and silence.
  bool get isIndexable =>
      parseError.isEmpty &&
      (unavailableReason == MemoryUnavailableReason.none ||
          unavailableReason == MemoryUnavailableReason.unspecified);
}

/// A proposed memory awaiting the user's decision.
class MemoryCandidate {
  MemoryCandidate({
    required this.candidateId,
    required this.kind,
    required this.inboxPath,
    required this.content,
    required this.contentHash,
    required this.state,
    required this.managed,
    List<MemoryProvenance> provenance = const [],
    this.promotedNoteId = '',
    this.createdAt,
    this.updatedAt,
    this.decidedAt,
    this.parseError = '',
    this.unavailableReason = MemoryUnavailableReason.unspecified,
  }) : provenance = List.unmodifiable(provenance);

  final String candidateId;
  final MemoryCandidateKind kind;
  final String inboxPath;

  /// The complete proposed text, never a preview: the user is being asked to
  /// accept exactly this.
  final String content;
  final String contentHash;
  final MemoryCandidateState state;

  /// False for a draft the user dropped into the inbox themselves. Turing has
  /// no row for it and will not move it, so there is no RPC that promotes it —
  /// rendering one would offer an action the server refuses.
  final bool managed;
  final List<MemoryProvenance> provenance;
  final String promotedNoteId;
  final DateTime? createdAt;
  final DateTime? updatedAt;
  final DateTime? decidedAt;
  final String parseError;
  final MemoryUnavailableReason unavailableReason;

  /// Whether the user is being shown the whole proposal. A decision about text
  /// the page could not display is not a decision.
  bool get contentIsWhole =>
      parseError.isEmpty &&
      unavailableReason != MemoryUnavailableReason.contentParseFailed &&
      unavailableReason != MemoryUnavailableReason.contentTooLarge &&
      unavailableReason != MemoryUnavailableReason.vaultMissing &&
      unavailableReason != MemoryUnavailableReason.vaultUnreadable;

  /// Whether this client may offer a decision at all: managed by Turing, still
  /// pending, and shown in full.
  bool get isDecidable =>
      managed &&
      candidateId.isNotEmpty &&
      state == MemoryCandidateState.pending &&
      contentIsWhole;
}

class MemoryState {
  MemoryState({
    required this.settings,
    required this.persona,
    required this.profile,
    List<MemoryTierState> tiers = const [],
    List<MemoryNote> notes = const [],
    List<MemoryCandidate> candidates = const [],
  }) : tiers = List.unmodifiable(tiers),
       notes = List.unmodifiable(notes),
       candidates = List.unmodifiable(candidates);

  final MemorySettings settings;
  final MemoryDocument persona;
  final MemoryDocument profile;
  final List<MemoryTierState> tiers;
  final List<MemoryNote> notes;
  final List<MemoryCandidate> candidates;
}
