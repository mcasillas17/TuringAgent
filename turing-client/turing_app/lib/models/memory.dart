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

  /// The user accepted this profile edit and the server claimed it before
  /// touching profile.md. The decision has been taken — what is unfinished is
  /// the write or the bookkeeping after it — so no decision RPC will accept it
  /// and this client offers none.
  profileApplying,
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

  /// Where the vault is, as a folder on the machine the user is sitting at, so
  /// they can open it in their own editor.
  ///
  /// It is display only, and it is deliberately not the path the orchestrator
  /// opens: under Docker that is a directory inside one container. Empty means
  /// the server had nothing usable to name, and the page shows nothing rather
  /// than guessing.
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

/// What an accepted profile edit left behind.
///
/// The write and the tidying after it are reported separately because they fail
/// separately, and only one of them is the decision. Once profile.md holds the
/// document the user reviewed they have been answered; a proposal file the
/// server could not remove afterwards is Turing's housekeeping, and calling
/// that a failed apply would tell them their edit did not happen while their
/// own file says it did.
class MemoryApplyResult {
  const MemoryApplyResult({required this.profile, this.cleanupPending = false});

  final MemoryDocument profile;

  /// True when the profile holds the reviewed document and the proposal it came
  /// from is still in the inbox. The proposal is not decidable any more — no
  /// RPC will take it — so a page that said nothing would leave the user
  /// looking at a card they cannot act on.
  final bool cleanupPending;
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

  /// Whether Turing can actually find this note when it searches memory.
  ///
  /// This is a whitelist, and that is the point. The server answers NONE to say
  /// "nothing is wrong", and only that answer means anything read this note.
  /// [MemoryUnavailableReason.unspecified] is not a quieter version of it: it
  /// is the server not saying, and it is also what this build decodes any
  /// reason a newer server invents into. Accepting it would render a note
  /// nobody could account for as an ordinary, findable belief — and would
  /// suppress the one line telling the user their memory is not being found,
  /// on the strength of not recognising the problem.
  ///
  /// The status is the same allowlist the server's own search predicate uses:
  /// a note Turing may rewrite and one the user has taken over are memory they
  /// have, and search answers from those two and nothing else. A withdrawn note
  /// is kept because they accepted it, and is never answered with; a status
  /// from a newer build is one this page cannot claim anything about.
  bool get isIndexable =>
      parseError.isEmpty &&
      unavailableReason == MemoryUnavailableReason.none &&
      (status == MemoryNoteStatus.managed ||
          status == MemoryNoteStatus.unmanaged);
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
    this.untracked = false,
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

  /// True for an inbox file Turing wrote and then lost the record of. It is
  /// unmanaged for the same mechanical reason a hand-dropped draft is, and it
  /// is not the same thing: this one is a model's claim about the user, and
  /// presenting it as something they wrote themselves would be a lie about who
  /// said it.
  final bool untracked;
  final List<MemoryProvenance> provenance;
  final String promotedNoteId;
  final DateTime? createdAt;
  final DateTime? updatedAt;
  final DateTime? decidedAt;
  final String parseError;
  final MemoryUnavailableReason unavailableReason;

  /// Whether the user is being shown the whole proposal. A decision about text
  /// the page could not display is not a decision.
  ///
  /// This is a whitelist, and that is the point. The server answers NONE to say
  /// "nothing is wrong", and only that answer means the bytes on this card are
  /// the bytes in the user's inbox. Listing the failures instead would let a
  /// reason a newer server invents — one this build decodes as [unspecified]
  /// because it has never heard of it — fall through as health, and the page
  /// would offer to accept text nobody read on the strength of not recognising
  /// the problem.
  bool get contentIsWhole =>
      parseError.isEmpty && unavailableReason == MemoryUnavailableReason.none;

  /// True when the reason says nothing a person can act on: the server reported
  /// health, or reported something this build cannot name. It is what lets a
  /// card say "this build cannot decide this" only where no better sentence is
  /// already on it.
  bool get reasonIsSilent =>
      unavailableReason == MemoryUnavailableReason.none ||
      unavailableReason == MemoryUnavailableReason.unspecified;

  /// Whether a proposal the page could not show whole is still one this client
  /// knows the server can throw away.
  ///
  /// A rejection removes the file, so it needs the vault. Both arms below are
  /// the same situation — the vault is open, the file is in it, and only its
  /// contents defeated the reader — which is exactly when a removal by name
  /// still works and is the only way out that proposal has.
  ///
  /// Everything else is closed, including a reason this build cannot name. The
  /// switch is exhaustive with no `default` so a value added tomorrow has to be
  /// classified deliberately: guessing that an unknown problem is a safe one is
  /// how a client offers to delete a file over a condition nobody has thought
  /// about yet.
  bool get _rejectionIsSafe {
    switch (unavailableReason) {
      case MemoryUnavailableReason.contentParseFailed:
      case MemoryUnavailableReason.contentTooLarge:
        return true;
      case MemoryUnavailableReason.none:
      case MemoryUnavailableReason.unspecified:
      case MemoryUnavailableReason.disabled:
      case MemoryUnavailableReason.vaultMissing:
      case MemoryUnavailableReason.vaultUnreadable:
        return false;
    }
  }

  /// Which decision, if any, this client may offer for this proposal.
  ///
  /// Both switches are exhaustive with no `default`, which is the point: a kind
  /// or a state this build has never heard of lands on
  /// [MemoryCandidateDecision.unsupported] and is *said*, rather than silently
  /// falling through to whichever arm happens to be last. A proposal whose kind
  /// this client cannot read is a proposal it cannot know which RPC the server
  /// would accept for — offering the wrong one is an action the server refuses,
  /// and offering nothing without a word is a card the user cannot explain.
  MemoryCandidateDecision get decision {
    // A file the user dropped in themselves has no row, so no RPC applies. It
    // is not unsupported; it is understood, and the answer is that Turing does
    // not decide it.
    if (!managed || candidateId.isEmpty) return MemoryCandidateDecision.none;
    switch (state) {
      case MemoryCandidateState.pending:
        break;
      case MemoryCandidateState.promoted:
      case MemoryCandidateState.rejected:
      case MemoryCandidateState.withdrawn:
      case MemoryCandidateState.profileApplying:
        return MemoryCandidateDecision.none;
      case MemoryCandidateState.unspecified:
        return MemoryCandidateDecision.unsupported;
    }
    // A pending proposal the page could not show whole. Accepting it is off the
    // table — nobody may accept text they were not shown, and the server needs
    // the kind, which is a question about bytes nobody can read. Throwing it
    // away is offered only where this build knows the server still can: a
    // rejection is the one thing standing between the user and a claim about
    // themselves they can neither accept nor be rid of, and offering it where
    // it would be refused replaces that with a button that does nothing.
    if (!contentIsWhole) {
      return _rejectionIsSafe
          ? MemoryCandidateDecision.rejectOnly
          : MemoryCandidateDecision.unsupported;
    }
    switch (kind) {
      case MemoryCandidateKind.belief:
        return MemoryCandidateDecision.promoteToBeliefs;
      case MemoryCandidateKind.profileEdit:
        return MemoryCandidateDecision.applyToProfile;
      case MemoryCandidateKind.unspecified:
        return MemoryCandidateDecision.unsupported;
    }
  }

  /// Whether this client may offer any decision at all: managed by Turing,
  /// still pending, and either shown in full or refusable.
  bool get isDecidable =>
      decision == MemoryCandidateDecision.promoteToBeliefs ||
      decision == MemoryCandidateDecision.applyToProfile ||
      decision == MemoryCandidateDecision.rejectOnly;

  /// The compare-and-set a rejection carries.
  ///
  /// It is empty for a proposal the page could not show whole, and that is the
  /// whole point: a hash names bytes the user read, and these are bytes nobody
  /// read. Sending the listing's hash anyway would make a claim this client
  /// cannot stand behind — and the server refuses a claim it cannot check
  /// against a file it cannot parse, which would leave the proposal
  /// undecidable in both directions.
  String get rejectionHash => contentIsWhole ? contentHash : '';
}

/// What the page may offer for one proposal.
///
/// [unsupported] is deliberately not [none]. "Turing does not decide this file"
/// and "this build cannot tell what deciding it would mean" are different
/// facts, and only the second one is something the user should be told about.
enum MemoryCandidateDecision {
  /// No decision belongs here: an unmanaged draft, an already-decided proposal,
  /// or an apply the server has already claimed.
  none,

  /// Promote into beliefs, or reject.
  promoteToBeliefs,

  /// Apply to profile.md, or reject.
  applyToProfile,

  /// Reject, and nothing else: a managed, pending proposal whose text the page
  /// could not show whole, for a reason this build knows leaves the file itself
  /// reachable. There is nothing here to accept, and the file is still the
  /// user's to throw away.
  rejectOnly,

  /// A managed, pending proposal this build cannot safely offer anything for:
  /// its kind or state has no name here, its vault is out of reach so even a
  /// rejection would be refused, or the server gave a reason this build has
  /// never heard of. No button is safe, and the card says so.
  unsupported,
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
