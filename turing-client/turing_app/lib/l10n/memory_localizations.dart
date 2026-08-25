import '../models/memory.dart';
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
