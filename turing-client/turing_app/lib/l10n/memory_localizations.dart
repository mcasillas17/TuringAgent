import '../models/memory.dart';
import '../models/remote_egress.dart';
import 'generated/app_localizations.dart';

/// The one place a memory status becomes a sentence.
///
/// Every function here is an exhaustive switch with no `default`: a status this
/// client learns to decode tomorrow forces a deliberate sentence rather than
/// falling into whichever arm happens to be last. That matters more here than
/// elsewhere, because the whole point of these values is to say what could not
/// be read — a silent fallback would turn a broken vault into a blank line.
String localizedMemoryUnavailableCopy(
  AppLocalizations l10n,
  MemoryUnavailableReason reason,
) {
  switch (reason) {
    // "The server did not say" is its own answer, and it is not "fine": a
    // client that collapsed it into NONE would render an unanswered read as a
    // healthy one.
    case MemoryUnavailableReason.unspecified:
      return l10n.memoryReasonUnspecified;
    case MemoryUnavailableReason.none:
      return l10n.memoryReasonNone;
    case MemoryUnavailableReason.disabled:
      return l10n.memoryReasonDisabled;
    case MemoryUnavailableReason.vaultMissing:
      return l10n.memoryReasonVaultMissing;
    case MemoryUnavailableReason.vaultUnreadable:
      return l10n.memoryReasonVaultUnreadable;
    case MemoryUnavailableReason.contentParseFailed:
      return l10n.memoryReasonContentParseFailed;
    case MemoryUnavailableReason.contentTooLarge:
      return l10n.memoryReasonContentTooLarge;
  }
}

String localizedMemoryNoteStatusCopy(
  AppLocalizations l10n,
  MemoryNoteStatus status,
) {
  switch (status) {
    case MemoryNoteStatus.unspecified:
      return l10n.memoryStatusUnspecified;
    case MemoryNoteStatus.managed:
      return l10n.memoryStatusManaged;
    case MemoryNoteStatus.unmanaged:
      return l10n.memoryStatusUnmanaged;
    case MemoryNoteStatus.withdrawn:
      return l10n.memoryStatusWithdrawn;
  }
}

String localizedMemoryCandidateStateCopy(
  AppLocalizations l10n,
  MemoryCandidateState state,
) {
  switch (state) {
    case MemoryCandidateState.unspecified:
      return l10n.memoryCandidateStateUnspecified;
    case MemoryCandidateState.pending:
      return l10n.memoryCandidateStatePending;
    case MemoryCandidateState.promoted:
      return l10n.memoryCandidateStatePromoted;
    case MemoryCandidateState.rejected:
      return l10n.memoryCandidateStateRejected;
    case MemoryCandidateState.withdrawn:
      return l10n.memoryCandidateStateWithdrawn;
  }
}

String localizedMemoryCandidateKindCopy(
  AppLocalizations l10n,
  MemoryCandidateKind kind,
) {
  switch (kind) {
    case MemoryCandidateKind.unspecified:
      return l10n.memoryCandidateKindUnspecified;
    case MemoryCandidateKind.belief:
      return l10n.memoryCandidateKindBelief;
    case MemoryCandidateKind.profileEdit:
      return l10n.memoryCandidateKindProfileEdit;
  }
}

String localizedMemoryTierCopy(AppLocalizations l10n, MemoryTier tier) {
  switch (tier) {
    case MemoryTier.unspecified:
      return l10n.memoryTierUnspecified;
    case MemoryTier.persona:
      return l10n.memoryTierPersona;
    case MemoryTier.profile:
      return l10n.memoryTierProfile;
    case MemoryTier.belief:
      return l10n.memoryTierBelief;
    case MemoryTier.note:
      return l10n.memoryTierNote;
  }
}

/// The categories the consent dialog lists, in the user's words.
///
/// Exhaustive for the same reason as the rest of this file: a category added
/// to the wire and to this client, but never given a sentence, would show the
/// user a blank bullet in the one dialog that exists to tell them what leaves
/// the machine.
String localizedEgressCategoryCopy(
  AppLocalizations l10n,
  EgressDataCategory category,
) {
  switch (category) {
    case EgressDataCategory.currentMessage:
      return l10n.egressCategoryCurrentMessage;
    case EgressDataCategory.conversationHistory:
      return l10n.egressCategoryConversationHistory;
    case EgressDataCategory.crossSessionRecall:
      return l10n.egressCategoryCrossSessionRecall;
    case EgressDataCategory.memoryProfile:
      return l10n.egressCategoryMemoryProfile;
    case EgressDataCategory.skillContent:
      return l10n.egressCategorySkillContent;
    case EgressDataCategory.toolSchemas:
      return l10n.egressCategoryToolSchemas;
    case EgressDataCategory.toolArguments:
      return l10n.egressCategoryToolArguments;
    case EgressDataCategory.toolResults:
      return l10n.egressCategoryToolResults;
    case EgressDataCategory.attachments:
      return l10n.egressCategoryAttachments;
  }
}

/// The tier a disclosed memory belongs to, in the user's words.
///
/// An unrecognised tier is called "Memory" rather than guessed at: saying
/// something true and vague beats naming a tier the server never claimed.
String localizedEgressMemoryTierCopy(
  AppLocalizations l10n,
  MemoryEgressTier tier,
) {
  switch (tier) {
    case MemoryEgressTier.persona:
      return l10n.egressMemoryTierPersona;
    case MemoryEgressTier.profile:
      return l10n.egressMemoryTierProfile;
    case MemoryEgressTier.belief:
      return l10n.egressMemoryTierBelief;
    case MemoryEgressTier.note:
      return l10n.egressMemoryTierNote;
    case MemoryEgressTier.unspecified:
      return l10n.egressMemoryTierUnspecified;
  }
}
