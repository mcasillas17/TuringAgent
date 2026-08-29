import 'package:flutter/material.dart';

import '../../constants/app_colors.dart';
import '../../l10n/generated/app_localizations.dart';
import '../../l10n/memory_localizations.dart';
import '../../models/memory.dart';
import '../../networking/api_client.dart';
import 'workspace_pages.dart';

/// The vault, as a page.
///
/// It is a thin client in the strict sense: it calls MemoryService, renders
/// exactly what comes back, and re-reads after every write. No promotion rule,
/// no confinement check and no compare-and-set decision lives here — those are
/// the server's, and a second copy of them in the UI would be a second opinion
/// nobody asked for. What this page owns is honesty: every reason the vault
/// could not be read is on screen, and no button is offered for an action the
/// server would refuse.
class MemoryPage extends StatefulWidget {
  const MemoryPage({super.key, required this.apiClient});

  final TuringApi apiClient;

  @override
  State<MemoryPage> createState() => _MemoryPageState();
}

/// The one rule that turns a proposal into a document.
///
/// `ApplyMemoryProfile.content` is the whole resulting profile, so the page has
/// to compose one — and the composition has to be something the user can look
/// at and predict: their profile as it stands, then the proposal, separated by
/// a blank line. Nothing is dropped and nothing is reordered, because the
/// alternative to a rule they can see is a merge they have to trust.
///
/// It is only the starting point. The result is editable, and Apply sends what
/// the editor holds.
@visibleForTesting
String composeProfileResult(String profile, String proposal) {
  final existing = profile.trimRight();
  final addition = proposal.trim();
  if (addition.isEmpty) return profile;
  if (existing.isEmpty) return '$addition\n';
  return '$existing\n\n$addition\n';
}

/// One profile proposal's resulting document as this build resolved it: the
/// editor holding the words, and the two compare-and-set tokens its apply will
/// name.
///
/// They are handed out as one value so a caller cannot read a token from a
/// build where the editor had not been re-seeded yet. Displayed and sent are
/// the same numbers by construction, not by call order.
@immutable
class _ProfileResultBinding {
  const _ProfileResultBinding({
    required this.controller,
    required this.profileHash,
    required this.candidateHash,
  });

  final TextEditingController controller;

  /// The profile version these words were composed over, and so the document an
  /// apply of them is an edit of.
  final String profileHash;

  /// The proposal these words were composed from, and so the claim an apply of
  /// them is an acceptance of.
  final String candidateHash;
}

/// Everything the page holds for one profile proposal's result: the editor, the
/// words it was seeded with, and the two tokens those words were composed
/// against.
///
/// The seed is how an untouched result is told from an edited one. An editor
/// still holding its seed is one the user has not typed into, and may be
/// re-composed when the profile or the proposal moves underneath it; one that
/// has drifted holds their words and is left alone.
///
/// The two tokens travel with the text, not with the page. A result the user
/// has edited keeps their words when the profile moves underneath it — and it
/// has to keep these with them, because those words describe a document that is
/// no longer there. Sending them with the *newer* profile's token would tell
/// the server "this is an edit of what you have now", and it would be accepted:
/// whatever the other writer put in profile.md would be gone, silently. The
/// proposal's token is kept for the same reason: a proposal is a file the user
/// can rewrite in Obsidian between composing a result and applying it, and
/// sending these words against the newer one would say "I read this and I
/// accept it" about a claim nobody has read. Sent with the pair they were
/// actually composed against, the server refuses and the user is told to look
/// again.
class _ProfileResult {
  _ProfileResult({
    required this.controller,
    required this.seed,
    required this.profileHash,
    required this.candidateHash,
  });

  final TextEditingController controller;
  String seed;
  String profileHash;
  String candidateHash;
}

class _MemoryPageState extends State<MemoryPage> {
  late Future<MemoryState> _state;
  final TextEditingController _persona = TextEditingController();
  final TextEditingController _profile = TextEditingController();

  /// One resulting-profile editor per profile_edit proposal, kept on the page
  /// rather than inside the card so a re-read does not throw away what the user
  /// has typed into it.
  ///
  /// One map, not four parallel ones: the words, the seed they were composed
  /// as, and the two tokens they were composed against are one fact about one
  /// proposal, and forgetting a proposal has to forget all of it. Kept apart,
  /// a result could be dropped while the numbers it was composed against stayed
  /// behind to be handed to whatever arrives under the same id next.
  final Map<String, _ProfileResult> _profileResults = {};

  /// The hashes the editors were loaded at. Sent back as the compare-and-set
  /// token, so a save always answers "I am editing *this* version" rather than
  /// "whatever is there now".
  String _personaHash = '';
  String _profileHash = '';

  /// The server's text as each editor last adopted it. An editor whose content
  /// has drifted from this holds words the user typed and has not saved.
  String _personaAdopted = '';
  String _profileAdopted = '';
  bool _busy = false;
  String _settingsError = '';
  String _personaError = '';
  String _profileError = '';
  String _inboxError = '';

  /// Said after an apply whose write landed and whose proposal could not be
  /// removed. It is not an error — the user's profile holds what they accepted
  /// — and it is not nothing either: the proposal is still listed below and no
  /// button on it will work, so the page has to say why.
  String _inboxNotice = '';

  bool get _personaDirty => _persona.text != _personaAdopted;
  bool get _profileDirty => _profile.text != _profileAdopted;

  @override
  void initState() {
    super.initState();
    _state = _load(adoptPersona: true, adoptProfile: true);
  }

  @override
  void dispose() {
    _persona.dispose();
    _profile.dispose();
    for (final result in _profileResults.values) {
      result.controller.dispose();
    }
    super.dispose();
  }

  /// The editor for one profile proposal's resulting document and the token
  /// that travels with it, resolved together in one call.
  ///
  /// Together is the point. The words and the token are one fact: the editor is
  /// re-seeded here when the profile moves underneath an untouched result, and
  /// the token is re-aimed in the same step. Reading the token separately —
  /// before this ran, as the card used to — names the version the *previous*
  /// build composed against while the apply carries this one, which is the one
  /// thing a compare-and-set line on screen exists to rule out.
  _ProfileResultBinding _profileResultFor(
    MemoryCandidate candidate,
    String profile,
    String profileHash,
  ) {
    final seed = composeProfileResult(profile, candidate.content);
    final result = _profileResults.putIfAbsent(
      candidate.candidateId,
      () => _ProfileResult(
        controller: TextEditingController(text: seed),
        seed: seed,
        profileHash: profileHash,
        candidateHash: candidate.contentHash,
      ),
    );
    if (result.controller.text == result.seed) {
      // Untouched, so it follows the vault. Re-seeded means re-composed: these
      // words are the profile as it reads now plus the proposal as it reads
      // now, so they are an edit of the one and an acceptance of the other, and
      // they carry both of those tokens.
      if (result.controller.text != seed) result.controller.text = seed;
      result.seed = seed;
      result.profileHash = profileHash;
      result.candidateHash = candidate.contentHash;
    }
    return _ProfileResultBinding(
      controller: result.controller,
      // Never whichever read happened most recently: an edited result keeps the
      // pair it was composed from, so the apply is refused honestly rather than
      // silently rewriting a document nobody read or accepting a claim nobody
      // read.
      profileHash: result.profileHash,
      candidateHash: result.candidateHash,
    );
  }

  /// Forgets the results of proposals this page is no longer composing for: the
  /// ones that left the listing, and the ones the server has decided. Those
  /// results describe a file that is gone, and keeping one alive would hand it
  /// to whatever arrives under the same id next.
  ///
  /// Only a frame that actually rendered the listing may call this. A loading
  /// frame has no proposals in it and is not evidence that any of them left.
  void _retainProfileResults(Iterable<String> liveCandidateIds) {
    final live = liveCandidateIds.toSet();
    for (final candidateId in _profileResults.keys.toList()) {
      if (live.contains(candidateId)) continue;
      _profileResults.remove(candidateId)?.controller.dispose();
    }
  }

  /// Re-reads the page, and adopts the server's text into the editors the
  /// caller has the standing to overwrite.
  ///
  /// Everything on this page re-reads after a write, because a promotion moves
  /// a file and an apply rewrites a document and the page cannot truthfully
  /// patch its own copy. But a re-read is not permission to throw away words
  /// the user typed and has not saved: unless this particular action was about
  /// this particular document, a dirty editor keeps its text — and keeps the
  /// hash it composed that text against, so the save that follows still asks
  /// "is it still the version I read?" and is refused honestly if it is not.
  Future<MemoryState> _load({
    bool adoptPersona = false,
    bool adoptProfile = false,
  }) async {
    final state = await widget.apiClient.listMemoryState();
    // A read is not instantaneous, and the user may have left the page while
    // it was in flight. Everything below writes into text controllers this
    // State owns and has already disposed, which is a use-after-dispose the
    // framework normally swallows — the FutureBuilder that asked for this read
    // is gone too, so the error it throws has nowhere to be reported.
    if (!mounted) return state;
    if (adoptPersona || !_personaDirty) {
      _persona.text = state.persona.content;
      _personaAdopted = state.persona.content;
      _personaHash = state.persona.contentHash;
    }
    if (adoptProfile || !_profileDirty) {
      _profile.text = state.profile.content;
      _profileAdopted = state.profile.content;
      _profileHash = state.profile.contentHash;
    }
    return state;
  }

  void _reload({bool adoptPersona = false, bool adoptProfile = false}) {
    setState(() {
      _state = _load(adoptPersona: adoptPersona, adoptProfile: adoptProfile);
    });
  }

  /// Runs one write, then re-reads the whole page.
  ///
  /// The re-read is not a refresh detail. A promotion moves a file, an apply
  /// rewrites a document, and a rejection deletes one — the page cannot
  /// truthfully patch its own copy afterwards, so it asks again.
  Future<void> _mutate(
    Future<void> Function() request,
    void Function(String message) onError, {
    bool adoptPersona = false,
    bool adoptProfile = false,
  }) async {
    if (_busy) return;
    setState(() {
      _busy = true;
      onError('');
      // Cleared before the request, not after it: a notice about the last
      // apply has nothing to say about this decision, and the apply below sets
      // it again from what the server just answered.
      _inboxNotice = '';
    });
    try {
      await request();
      if (!mounted) return;
      setState(() {
        _busy = false;
        _state = _load(adoptPersona: adoptPersona, adoptProfile: adoptProfile);
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        onError(_describe(error));
      });
    }
  }

  /// Saves one authored document without losing the keystrokes that landed
  /// while it was in flight.
  ///
  /// A save is not instantaneous, and the editor stays live throughout: the
  /// user can — and does — keep typing between pressing Save and the server
  /// answering. The text that was sent is captured here, and on success the
  /// editor adopts it *as its baseline* rather than as its content. If nothing
  /// was typed meanwhile, the two are the same and the editor is clean; if
  /// something was, the newer words stay on screen and stay dirty.
  ///
  /// The compare-and-set token moves with the baseline, and only when the
  /// baseline this save started from is still the one in effect. Those newer
  /// words were typed on top of the document that was just written, so the next
  /// save has to name the hash the server just returned — naming the older one
  /// would have the server refuse an edit that is genuinely based on what is
  /// now on disk. A re-read landing in the same window replaces the baseline
  /// with something newer, and then this save's receipt is stale and is
  /// dropped.
  Future<void> _saveDocument({
    required TextEditingController controller,
    required Future<MemoryDocument> Function(String content, String hash)
    request,
    required void Function(String message) onError,
    required String Function() readAdopted,
    required void Function(String adopted, String hash) writeAdopted,
    required String hash,
  }) async {
    if (_busy) return;
    final submitted = controller.text;
    final submittedAgainst = readAdopted();
    setState(() {
      _busy = true;
      onError('');
    });
    try {
      final saved = await request(submitted, hash);
      if (!mounted) return;
      setState(() {
        _busy = false;
        if (readAdopted() == submittedAgainst) {
          writeAdopted(submitted, saved.contentHash);
        }
        _state = _load();
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _busy = false;
        onError(_describe(error));
      });
    }
  }

  static String _describe(Object error) {
    if (error is TuringApiException) return error.message;
    return '$error';
  }

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return WorkspacePage(
      title: l10n.memoryPageTitle,
      subtitle: l10n.memoryPageSubtitle,
      child: FutureBuilder<MemoryState>(
        future: _state,
        builder: (context, snapshot) {
          if (snapshot.connectionState != ConnectionState.done) {
            return const WorkspaceLoading();
          }
          if (snapshot.hasError) {
            return WorkspaceNotice(
              icon: Icons.error_outline,
              title: l10n.memoryBackendUnreachable,
              body: _describe(snapshot.error!),
              onRetry: () => _reload(adoptPersona: true, adoptProfile: true),
              tone: AppColors.danger,
            );
          }
          final state = snapshot.data!;
          return _MemoryBody(
            state: state,
            l10n: l10n,
            persona: _persona,
            profile: _profile,
            personaHash: _personaHash,
            profileHash: _profileHash,
            busy: _busy,
            settingsError: _settingsError,
            personaError: _personaError,
            profileError: _profileError,
            inboxError: _inboxError,
            inboxNotice: _inboxNotice,
            onToggle: (enabled) => _mutate(
              () => widget.apiClient.setMemoryEnabled(enabled: enabled),
              (message) => _settingsError = message,
            ),
            onSavePersona: () => _saveDocument(
              controller: _persona,
              hash: _personaHash,
              request: (content, hash) => widget.apiClient.saveMemoryPersona(
                content: content,
                expectedContentHash: hash,
              ),
              onError: (message) => _personaError = message,
              readAdopted: () => _personaAdopted,
              writeAdopted: (adopted, savedHash) {
                _personaAdopted = adopted;
                _personaHash = savedHash;
              },
            ),
            onSaveProfile: () => _saveDocument(
              controller: _profile,
              hash: _profileHash,
              request: (content, hash) => widget.apiClient.saveMemoryProfile(
                content: content,
                expectedContentHash: hash,
              ),
              onError: (message) => _profileError = message,
              readAdopted: () => _profileAdopted,
              writeAdopted: (adopted, savedHash) {
                _profileAdopted = adopted;
                _profileHash = savedHash;
              },
            ),
            onRereadPersona: () => _reload(adoptPersona: true),
            onRereadProfile: () => _reload(adoptProfile: true),
            onPromote: (candidate) => _mutate(
              () => widget.apiClient.promoteMemoryCandidate(
                candidateId: candidate.candidateId,
                // The listing serves the proposal as the vault holds it, so
                // this is the hash of exactly the words on screen — and the
                // only compare-and-set a decision has.
                expectedCandidateHash: candidate.contentHash,
              ),
              (message) => _inboxError = message,
            ),
            onReject: (candidate) => _mutate(
              () => widget.apiClient.rejectMemoryCandidate(
                candidateId: candidate.candidateId,
                // Empty for a proposal the page could not show whole. A hash
                // names bytes the user read, and a claim about bytes nobody
                // read is one the server refuses — which would leave them with
                // a proposal about themselves they cannot get rid of.
                expectedCandidateHash: candidate.rejectionHash,
              ),
              (message) => _inboxError = message,
            ),
            onApply: (candidate, result, profileHash, candidateHash) => _mutate(
              () async {
                final applied = await widget.apiClient.applyMemoryProfile(
                  candidateId: candidate.candidateId,
                  // The whole resulting document the user reviewed, never the
                  // proposal: the server writes exactly these bytes over
                  // profile.md.
                  content: result,
                  // Compare-and-set against the profile document, not the
                  // proposal: the question an apply asks is whether profile.md
                  // still says what the user was shown beside it. The token
                  // travels from the card that displayed it, so the request and
                  // the sentence under the button cannot name different numbers.
                  expectedContentHash: profileHash,
                  // And the second one, against the proposal this result was
                  // composed from — which is not necessarily the one listed now.
                  expectedCandidateHash: candidateHash,
                );
                _inboxNotice = applied.cleanupPending
                    ? l10n.memoryProfileAppliedCleanupPending
                    : '';
              },
              (message) => _inboxError = message,
            ),
            profileResultFor: (candidate) => _profileResultFor(
              candidate,
              state.profile.content,
              state.profile.contentHash,
            ),
            onCandidatesBuilt: _retainProfileResults,
          );
        },
      ),
    );
  }
}

class _MemoryBody extends StatelessWidget {
  const _MemoryBody({
    required this.state,
    required this.l10n,
    required this.persona,
    required this.profile,
    required this.personaHash,
    required this.profileHash,
    required this.busy,
    required this.settingsError,
    required this.personaError,
    required this.profileError,
    required this.inboxError,
    required this.inboxNotice,
    required this.onToggle,
    required this.onSavePersona,
    required this.onSaveProfile,
    required this.onRereadPersona,
    required this.onRereadProfile,
    required this.onPromote,
    required this.onReject,
    required this.onApply,
    required this.profileResultFor,
    required this.onCandidatesBuilt,
  });

  final MemoryState state;
  final AppLocalizations l10n;
  final TextEditingController persona;
  final TextEditingController profile;

  /// The compare-and-set tokens the editors were loaded at, which is what a
  /// save will name. They are not `state.persona.contentHash` and
  /// `state.profile.contentHash` the moment an editor holds unsaved words: a
  /// re-read leaves a dirty editor its text and its token, so the newest hash
  /// on the page describes a document the user is not editing.
  final String personaHash;
  final String profileHash;
  final bool busy;
  final String settingsError;
  final String personaError;
  final String profileError;
  final String inboxError;
  final String inboxNotice;
  final ValueChanged<bool> onToggle;
  final VoidCallback onSavePersona;
  final VoidCallback onSaveProfile;
  final VoidCallback onRereadPersona;
  final VoidCallback onRereadProfile;
  final ValueChanged<MemoryCandidate> onPromote;
  final ValueChanged<MemoryCandidate> onReject;

  /// The apply carries the reviewed resulting document, which is the whole of
  /// what profile.md will say — not the proposal, which is a fragment of it —
  /// and the two tokens the card displayed beside it.
  final void Function(
    MemoryCandidate candidate,
    String result,
    String profileHash,
    String candidateHash,
  )
  onApply;

  /// The editor holding that document and the token its apply will name,
  /// resolved together. The editor is owned by the page so it survives the
  /// re-read every write triggers, and the token comes back from the same call
  /// so the card cannot display one the request will not send.
  final _ProfileResultBinding Function(MemoryCandidate candidate)
  profileResultFor;

  /// Told which proposals are still the page's to compose for, so it can
  /// forget the results of the ones that are not. Every listed proposal the
  /// server has not decided is on that list, including the ones this frame can
  /// offer no button for: an editor is kept for a row, not for a button.
  final ValueChanged<List<String>> onCandidatesBuilt;

  /// The server's own row for a tier, if it sent one. A build that has not
  /// heard about a tier renders no row for it rather than an invented one.
  MemoryTierState? _tierFor(MemoryTier tier) {
    for (final row in state.tiers) {
      if (row.tier == tier) return row;
    }
    return null;
  }

  /// Whether this proposal needs a resulting-profile editor: a decidable
  /// profile edit and nothing else. A proposal the page will not offer a
  /// decision on gets no editor, because there is no apply to compose for.
  ///
  /// Every proposal this answers yes for is one [_retainsProfileResult] also
  /// answers yes for — an applicable proposal is a pending one — and it has to
  /// stay that way: an editor created in a frame that then forgets it would be
  /// disposed as it was drawn.
  static bool _needsProfileResult(MemoryCandidate candidate) =>
      candidate.decision == MemoryCandidateDecision.applyToProfile;

  /// Whether a result already composed for this proposal is still that
  /// proposal's to hold.
  ///
  /// The question is about the row, not about what this build can offer for it
  /// this frame. A vault that stopped being readable, bytes that stopped
  /// parsing, memory switched off — each of those takes the Apply button away,
  /// and none of them is a decision about the proposal. The words the user
  /// typed are still their answer to it, and forgetting them because no button
  /// can be drawn this frame loses them to a condition that clears on the next
  /// read: the page would compose a fresh result over whatever the profile and
  /// the proposal say afterwards, and the apply would carry those newer tokens
  /// — an edit of a document nobody read, and an acceptance of a claim nobody
  /// read.
  ///
  /// An apply the server has claimed is retained for the same reason: the claim
  /// is handed back to pending when the write turns out to change nothing, so
  /// it is not the end of the row either. What is the end of it is a decision —
  /// promoted, rejected, withdrawn — and a row that leaves the listing
  /// altogether, which is the other half of the same answer. The switch is
  /// exhaustive with no `default` so a state added tomorrow has to be sorted
  /// deliberately rather than silently ending somebody's draft.
  static bool _retainsProfileResult(MemoryCandidate candidate) {
    switch (candidate.state) {
      case MemoryCandidateState.pending:
      case MemoryCandidateState.profileApplying:
      // A state this build cannot name is not a decision it has been told
      // about. Keeping the words costs a text controller; guessing that an
      // unknown answer means "decided" costs the user what they wrote.
      case MemoryCandidateState.unspecified:
        return true;
      case MemoryCandidateState.promoted:
      case MemoryCandidateState.rejected:
      case MemoryCandidateState.withdrawn:
        return false;
    }
  }

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    final beliefTier = state.tiers
        .where((tier) => tier.tier == MemoryTier.belief)
        .toList();
    onCandidatesBuilt([
      for (final candidate in state.candidates)
        if (_retainsProfileResult(candidate)) candidate.candidateId,
    ]);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _SettingsCard(
          settings: state.settings,
          l10n: l10n,
          palette: palette,
          busy: busy,
          error: settingsError,
          onToggle: onToggle,
        ),
        const SizedBox(height: 12),
        _DocumentCard(
          key: const Key('memory-persona-card'),
          heading: l10n.memoryPersonaHeading,
          description: l10n.memoryPersonaDescription,
          document: state.persona,
          editingHash: personaHash,
          vaultWritable: state.settings.vaultWritable,
          controller: persona,
          editorKey: const Key('memory-persona-editor'),
          saveKey: const Key('memory-persona-save'),
          rereadKey: const Key('memory-persona-reread'),
          l10n: l10n,
          palette: palette,
          busy: busy,
          error: personaError,
          tier: _tierFor(MemoryTier.persona),
          onSave: onSavePersona,
          onReread: onRereadPersona,
        ),
        const SizedBox(height: 12),
        _DocumentCard(
          key: const Key('memory-profile-card'),
          heading: l10n.memoryProfileHeading,
          description: l10n.memoryProfileDescription,
          document: state.profile,
          editingHash: profileHash,
          vaultWritable: state.settings.vaultWritable,
          controller: profile,
          editorKey: const Key('memory-profile-editor'),
          saveKey: const Key('memory-profile-save'),
          rereadKey: const Key('memory-profile-reread'),
          l10n: l10n,
          palette: palette,
          busy: busy,
          error: profileError,
          tier: _tierFor(MemoryTier.profile),
          onSave: onSaveProfile,
          onReread: onRereadProfile,
        ),
        const SizedBox(height: 22),
        _SectionHeading(text: l10n.memoryInboxHeading, palette: palette),
        if (inboxError.isNotEmpty) ...[
          const SizedBox(height: 8),
          _ErrorLine(message: inboxError),
        ],
        if (inboxNotice.isNotEmpty) ...[
          const SizedBox(height: 8),
          _StatusLine(text: inboxNotice, tone: AppColors.warning),
        ],
        const SizedBox(height: 10),
        if (state.candidates.isEmpty)
          Text(
            l10n.memoryInboxEmpty,
            style: TextStyle(fontSize: 13, color: palette.textMuted),
          )
        else
          for (final candidate in state.candidates)
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: _CandidateCard(
                candidate: candidate,
                profileResult: _needsProfileResult(candidate)
                    ? profileResultFor(candidate)
                    : null,
                l10n: l10n,
                palette: palette,
                busy: busy,
                onPromote: () => onPromote(candidate),
                onReject: () => onReject(candidate),
                onApply: onApply,
              ),
            ),
        const SizedBox(height: 22),
        _SectionHeading(text: l10n.memoryBeliefsHeading, palette: palette),
        if (beliefTier.isNotEmpty) ...[
          const SizedBox(height: 8),
          _TierStatus(tier: beliefTier.first, l10n: l10n, palette: palette),
        ],
        const SizedBox(height: 10),
        if (state.notes.isEmpty)
          Text(
            l10n.memoryBeliefsEmpty,
            style: TextStyle(fontSize: 13, color: palette.textMuted),
          )
        else
          for (final note in state.notes)
            Padding(
              padding: const EdgeInsets.only(bottom: 12),
              child: _NoteCard(note: note, l10n: l10n, palette: palette),
            ),
      ],
    );
  }
}

class _SettingsCard extends StatelessWidget {
  const _SettingsCard({
    required this.settings,
    required this.l10n,
    required this.palette,
    required this.busy,
    required this.error,
    required this.onToggle,
  });

  final MemorySettings settings;
  final AppLocalizations l10n;
  final AppPalette palette;
  final bool busy;
  final String error;
  final ValueChanged<bool> onToggle;

  @override
  Widget build(BuildContext context) {
    return _Card(
      palette: palette,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Text(
                  settings.enabled
                      ? l10n.memoryEnabledTitle
                      : l10n.memoryDisabledTitle,
                  style: TextStyle(
                    fontSize: 15,
                    fontWeight: FontWeight.w700,
                    color: palette.text,
                  ),
                ),
              ),
              const SizedBox(width: 12),
              Switch(
                key: const Key('memory-enabled-toggle'),
                value: settings.enabled,
                onChanged: busy ? null : onToggle,
              ),
            ],
          ),
          const SizedBox(height: 8),
          Text(
            settings.enabled
                ? l10n.memoryEnabledDetail
                : l10n.memoryDisabledDetail,
            style: TextStyle(
              fontSize: 13,
              height: 1.5,
              color: palette.textMuted,
            ),
          ),
          // Shown only when the server names a folder, and labelled with the
          // machine it is on. The server sends the host directory here — the
          // one the vault is bound from — because the path it opens the vault
          // at may exist only inside a container. An unlabelled string invites
          // somebody to paste a container path into their own terminal, and a
          // label with nothing under it invites them to look for a folder
          // nobody named.
          if (settings.vaultRoot.isNotEmpty) ...[
            const SizedBox(height: 8),
            Text(
              l10n.memoryVaultLocation,
              style: TextStyle(fontSize: 12, color: palette.textMuted),
            ),
            SelectableText(
              settings.vaultRoot,
              style: TextStyle(fontSize: 12, color: palette.textMuted),
            ),
          ],
          const SizedBox(height: 8),
          // Why the vault is or is not usable, in the vault's own words. A
          // parse error alone left the "it is there but unreadable" case
          // rendering as an ordinary, healthy vault.
          _StatusLine(
            text: localizedMemoryUnavailableCopy(
              l10n,
              settings.unavailableReason,
            ),
            tone: _reasonTone(settings.unavailableReason),
          ),
          if (settings.parseError.isNotEmpty) ...[
            const SizedBox(height: 10),
            _ErrorLine(message: settings.parseError),
          ],
          if (error.isNotEmpty) ...[
            const SizedBox(height: 10),
            _ErrorLine(message: error),
          ],
        ],
      ),
    );
  }
}

/// One pinned document with its editor.
///
/// The content is rendered and edited as plain text in every state. Memory is
/// untrusted input — a model wrote some of what ends up beside it — so nothing
/// here interprets Markdown or HTML: what the file says is what the user sees.
class _DocumentCard extends StatelessWidget {
  const _DocumentCard({
    super.key,
    required this.heading,
    required this.description,
    required this.document,
    required this.editingHash,
    required this.vaultWritable,
    required this.controller,
    required this.editorKey,
    required this.saveKey,
    required this.rereadKey,
    required this.l10n,
    required this.palette,
    required this.busy,
    required this.error,
    required this.tier,
    required this.onSave,
    required this.onReread,
  });

  final String heading;
  final String description;
  final MemoryDocument document;

  /// The compare-and-set token this editor was loaded at, and the one a save
  /// will name.
  ///
  /// Deliberately not [MemoryDocument.contentHash]. A re-read leaves an editor
  /// the user has typed into both its text and the token that text was composed
  /// against, so the document beside it may already describe a newer version
  /// nobody in this editor is editing. Showing that newer number would put a
  /// sentence on screen that explains a refusal with a token the save never
  /// sent — the one thing this line exists to do.
  final String editingHash;

  /// Whether a vault is open at all, from `settings.vaultRoot`.
  ///
  /// [MemoryDocument.isWritable] is silent on this: the server reports
  /// VAULT_MISSING on a document both when the vault is open and this file
  /// has simply never been written, and when there is no vault open to write
  /// into. Those are not the same refusal, and the document alone cannot
  /// tell them apart — only the settings row can, so it is threaded down here
  /// rather than folded into [MemoryDocument.isWritable]'s per-file meaning.
  final bool vaultWritable;
  final TextEditingController controller;
  final Key editorKey;
  final Key saveKey;
  final Key rereadKey;
  final AppLocalizations l10n;
  final AppPalette palette;
  final bool busy;
  final String error;
  final MemoryTierState? tier;
  final VoidCallback onSave;
  final VoidCallback onReread;

  /// Whether this page may offer to write [document] right now.
  ///
  /// [MemoryDocument.isWritable] answers for the file alone, and a VAULT_MISSING
  /// document is writable by that reckoning — it is the ordinary shape of "the
  /// vault is open and this file has not been created yet". But the same
  /// reason is what a document reports when there is no vault open at all, and
  /// offering to create a file with nowhere to land would be a save the server
  /// can only refuse. [vaultWritable] is the one signal that tells those
  /// apart — the server's own answer about the vault it opened, never the
  /// display-only path, which is legitimately empty while the vault writes
  /// fine — so it gates VAULT_MISSING specifically without touching what
  /// [MemoryDocument.isWritable] means for every other reason.
  bool get _canSave =>
      document.isWritable &&
      (vaultWritable ||
          document.unavailableReason != MemoryUnavailableReason.vaultMissing);

  @override
  Widget build(BuildContext context) {
    return _Card(
      palette: palette,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            heading,
            style: TextStyle(
              fontSize: 15,
              fontWeight: FontWeight.w700,
              color: palette.text,
            ),
          ),
          const SizedBox(height: 6),
          Text(
            description,
            style: TextStyle(
              fontSize: 13,
              height: 1.5,
              color: palette.textMuted,
            ),
          ),
          const SizedBox(height: 10),
          _StatusLine(
            text: localizedMemoryUnavailableCopy(
              l10n,
              document.unavailableReason,
            ),
            tone: _reasonTone(document.unavailableReason),
          ),
          _StatusLine(
            text: localizedMemoryNoteStatusCopy(l10n, document.status),
            tone: palette.textMuted,
          ),
          if (document.parseError.isNotEmpty) ...[
            const SizedBox(height: 8),
            _ErrorLine(message: document.parseError),
          ],
          if (tier case final row?) _TierCounts(row: row, palette: palette),
          if (document.updatedAt case final updated?) ...[
            const SizedBox(height: 6),
            Text(
              l10n.memoryLastChanged(updated.toLocal(), updated.toLocal()),
              style: TextStyle(fontSize: 12, color: palette.textMuted),
            ),
          ],
          if (editingHash.isNotEmpty) ...[
            const SizedBox(height: 4),
            // The version this editor is holding. It is what a save is
            // compare-and-set against, so it is the one number that explains a
            // refusal — and it is a document hash, never the egress snapshot
            // fingerprint, which is a binding token and stays off screen.
            SelectableText(
              l10n.memoryEditingVersion(editingHash),
              style: TextStyle(fontSize: 12, color: palette.textMuted),
            ),
          ],
          if (document.pinnedTruncated) ...[
            const SizedBox(height: 6),
            // The editor is over the whole file; a run is not. Saying so here
            // is the only place a user can learn that the model is reading
            // less than they wrote — the runtime's own notice goes into the
            // pin, where it belongs, and never into their text.
            _StatusLine(
              text: l10n.memoryPinnedTruncated(document.pinnedBytes),
              tone: AppColors.warning,
            ),
          ],
          const SizedBox(height: 12),
          TextField(
            key: editorKey,
            controller: controller,
            maxLines: 8,
            minLines: 4,
            style: TextStyle(fontSize: 13, height: 1.45, color: palette.text),
            decoration: const InputDecoration(border: OutlineInputBorder()),
          ),
          if (error.isNotEmpty) ...[
            const SizedBox(height: 10),
            _ErrorLine(message: error),
          ],
          if (!document.isWritable) ...[
            const SizedBox(height: 8),
            _StatusLine(
              text: l10n.memorySaveUnavailable,
              tone: AppColors.warning,
            ),
          ] else if (!_canSave) ...[
            const SizedBox(height: 8),
            _StatusLine(
              text: l10n.memorySaveNeedsVault,
              tone: AppColors.warning,
            ),
          ],
          const SizedBox(height: 10),
          Wrap(
            spacing: 8,
            runSpacing: 4,
            children: [
              // Empty is a save like any other: clearing persona.md is how a
              // user takes back words they already gave a model. What is
              // refused here is a save the server would refuse anyway, because
              // the document could not be read in the first place.
              FilledButton(
                key: saveKey,
                onPressed: busy || !_canSave ? null : onSave,
                child: Text(l10n.memorySaveAction),
              ),
              // Re-reading is always the user's move, never automatic. After a
              // compare-and-set refusal in particular, silently reloading would
              // throw away what they just typed to make room for the words that
              // caused the refusal.
              TextButton(
                key: rereadKey,
                onPressed: busy ? null : onReread,
                child: Text(l10n.memoryRereadAction),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _CandidateCard extends StatelessWidget {
  const _CandidateCard({
    required this.candidate,
    required this.profileResult,
    required this.l10n,
    required this.palette,
    required this.busy,
    required this.onPromote,
    required this.onReject,
    required this.onApply,
  });

  final MemoryCandidate candidate;

  /// The resulting-profile editor and the token its apply will name, for a
  /// profile edit this page may apply, and null for every other proposal. The
  /// editor is owned by the page: a re-read rebuilds this card, and an editor
  /// rebuilt with it would lose what the user typed. The token arrives with it
  /// so the line under the button and the request name one number.
  final _ProfileResultBinding? profileResult;
  final AppLocalizations l10n;
  final AppPalette palette;
  final bool busy;
  final VoidCallback onPromote;
  final VoidCallback onReject;
  final void Function(
    MemoryCandidate candidate,
    String result,
    String profileHash,
    String candidateHash,
  )
  onApply;

  @override
  Widget build(BuildContext context) {
    final decision = candidate.decision;
    final result = profileResult;
    return _Card(
      palette: palette,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            localizedMemoryCandidateKindCopy(l10n, candidate.kind),
            style: TextStyle(
              fontSize: 14,
              fontWeight: FontWeight.w700,
              color: palette.text,
            ),
          ),
          const SizedBox(height: 4),
          SelectableText(
            candidate.inboxPath,
            style: TextStyle(fontSize: 12, color: palette.textMuted),
          ),
          const SizedBox(height: 10),
          if (candidate.content.isNotEmpty)
            // The whole proposal, never a preview: a decision about text the
            // user was not shown is not a decision. Selectable plain text, so
            // nothing in it is interpreted as markup.
            SelectableText(
              candidate.content,
              style: TextStyle(
                fontSize: 13.5,
                height: 1.5,
                color: palette.text,
              ),
            ),
          if (!candidate.contentIsWhole) ...[
            const SizedBox(height: 8),
            // Two different sentences, because they are two different facts. A
            // proposal still waiting on the user is one they can throw away,
            // and saying only "there is nothing here to accept" beside a lone
            // Reject button reads as a dead end rather than as the one action
            // left. A decided one is neither.
            _ErrorLine(
              message: decision == MemoryCandidateDecision.rejectOnly
                  ? l10n.memoryProposalDiscardOnly
                  : l10n.memoryProposalUnreadable,
            ),
          ],
          if (candidate.parseError.isNotEmpty) ...[
            const SizedBox(height: 8),
            _ErrorLine(message: candidate.parseError),
          ],
          if (candidate.unavailableReason != MemoryUnavailableReason.none) ...[
            const SizedBox(height: 8),
            _StatusLine(
              text: localizedMemoryUnavailableCopy(
                l10n,
                candidate.unavailableReason,
              ),
              tone: _reasonTone(candidate.unavailableReason),
            ),
          ],
          // The one card here with no action on it at all: nothing to show,
          // nothing to accept, and — where the server cannot open the file —
          // nothing to throw away either, because a removal has to prove which
          // entry is going and proving that needs a file something can open.
          // Saying only "this could not be read from the vault" beside no
          // buttons is a dead end; this is the way out of it, and one the user
          // can take without Turing.
          //
          // The sentence is conditional because this reason is: it also covers
          // a walk that could not finish and a read that failed for its own
          // reasons, and only some of those are a file nothing can open. What
          // is true of all of them is the instruction.
          if (candidate.managed &&
              candidate.state == MemoryCandidateState.pending &&
              candidate.unavailableReason ==
                  MemoryUnavailableReason.vaultUnreadable) ...[
            const SizedBox(height: 8),
            _StatusLine(
              text: l10n.memoryProposalUnopenable,
              tone: palette.textMuted,
            ),
          ],
          const SizedBox(height: 10),
          if (candidate.managed)
            _StatusLine(
              text: localizedMemoryCandidateStateCopy(l10n, candidate.state),
              tone: candidate.state == MemoryCandidateState.pending
                  ? palette.textMuted
                  : AppColors.warning,
            )
          else ...[
            // A file with no row behind it. Turing will not move it and every
            // decision RPC refuses it, so a Promote button here would be an
            // action the server refuses — but which file it is matters: one the
            // user dropped in is theirs, and one Turing wrote and lost the
            // record of is a model's claim about them. Calling the second the
            // first would be a lie about who said it.
            _StatusLine(
              text: candidate.untracked
                  ? l10n.memoryUntrackedInboxTitle
                  : l10n.memoryUnmanagedDraftTitle,
              tone: AppColors.warning,
            ),
            const SizedBox(height: 4),
            Text(
              candidate.untracked
                  ? l10n.memoryUntrackedInboxDetail
                  : l10n.memoryUnmanagedDraftDetail,
              style: TextStyle(
                fontSize: 12.5,
                height: 1.45,
                color: palette.textMuted,
              ),
            ),
          ],
          for (final provenance in candidate.provenance)
            _ProvenanceLine(
              provenance: provenance,
              l10n: l10n,
              palette: palette,
            ),
          // A managed, pending proposal this build cannot classify. No button
          // is offered, because the page cannot know which RPC the server
          // would accept — and the reason is said out loud, because a card
          // with prose and no actions reads as a proposal with nothing to
          // decide rather than as one this client is too old to decide.
          //
          // Only where the reason line above had nothing to say. A card that
          // already reports the vault could not be read must not also claim
          // the proposal is in a shape this build does not understand: the
          // shape is fine, the vault is gone, and only one of those sentences
          // is true.
          if (decision == MemoryCandidateDecision.unsupported &&
              candidate.reasonIsSilent) ...[
            const SizedBox(height: 10),
            _ErrorLine(message: l10n.memoryProposalUndecidable),
          ],
          if (decision == MemoryCandidateDecision.applyToProfile &&
              result != null) ...[
            const SizedBox(height: 14),
            Text(
              l10n.memoryProfileResultHeading,
              style: TextStyle(
                fontSize: 13,
                fontWeight: FontWeight.w700,
                color: palette.text,
              ),
            ),
            const SizedBox(height: 4),
            Text(
              l10n.memoryProfileResultDescription,
              style: TextStyle(
                fontSize: 12.5,
                height: 1.45,
                color: palette.textMuted,
              ),
            ),
            const SizedBox(height: 8),
            // Plain text in and plain text out. The proposal above it was
            // written by a model, and nothing on this page interprets it as
            // markup — least of all the field whose contents become the user's
            // own document.
            //
            // Closed while the page is busy, and that is not cosmetic. An
            // authored document may keep taking keystrokes mid-save because it
            // is still on screen afterwards to hold them; this editor is not.
            // A successful apply decides the proposal, the card goes, and
            // words typed into it after the button was pressed are neither
            // sent nor kept anywhere the user could find them. A field that
            // cannot keep what it takes must not take it — and it says so,
            // rather than swallowing them silently.
            TextField(
              key: Key('memory-profile-result-${candidate.candidateId}'),
              controller: result.controller,
              enabled: !busy,
              maxLines: 8,
              minLines: 4,
              keyboardType: TextInputType.multiline,
              style: const TextStyle(fontSize: 13, height: 1.5),
              decoration: const InputDecoration(
                border: OutlineInputBorder(),
                isDense: true,
              ),
            ),
            // The hint and the button both follow the editor rather than the
            // last rebuild, and they do it by listening rather than by holding
            // state: the page may reseed this controller mid-build when the
            // profile underneath it moves, and a setState in that window is a
            // build-time crash.
            ValueListenableBuilder<TextEditingValue>(
              valueListenable: result.controller,
              builder: (context, value, _) {
                if (value.text.trim().isNotEmpty) {
                  return const SizedBox.shrink();
                }
                return Padding(
                  padding: const EdgeInsets.only(top: 6),
                  child: _ErrorLine(message: l10n.memoryProfileResultEmpty),
                );
              },
            ),
            const SizedBox(height: 6),
            Text(
              l10n.memoryExpectedProfileHash(
                result.profileHash.isEmpty
                    ? l10n.memoryNoProfileYet
                    : result.profileHash,
              ),
              style: TextStyle(fontSize: 12, color: palette.textMuted),
            ),
            Text(
              l10n.memoryExpectedProposalHash(result.candidateHash),
              style: TextStyle(fontSize: 12, color: palette.textMuted),
            ),
          ],
          if (decision == MemoryCandidateDecision.applyToProfile ||
              decision == MemoryCandidateDecision.promoteToBeliefs ||
              decision == MemoryCandidateDecision.rejectOnly) ...[
            const SizedBox(height: 12),
            Wrap(
              spacing: 8,
              runSpacing: 4,
              children: [
                // Nothing to accept on a proposal the page could not show
                // whole: an acceptance is about text the user read, and this is
                // text nobody read. The rejection below stays, because a claim
                // about them that they can neither accept nor throw away is
                // worse than either.
                if (decision == MemoryCandidateDecision.applyToProfile)
                  if (result == null)
                    // Unreachable in this build — a decidable profile edit is
                    // always given an editor — and rendered disabled rather
                    // than omitted so a future caller that forgets one gets a
                    // dead button instead of an apply with no document.
                    FilledButton(
                      onPressed: null,
                      child: Text(l10n.memoryApplyAction),
                    )
                  else
                    ValueListenableBuilder<TextEditingValue>(
                      valueListenable: result.controller,
                      builder: (context, value, _) => FilledButton(
                        // Nothing to apply is not an apply. An empty document
                        // would replace everything the user has written about
                        // themselves with nothing.
                        onPressed: busy || value.text.trim().isEmpty
                            ? null
                            : () => onApply(
                                candidate,
                                result.controller.text,
                                // The tokens this card displayed, handed
                                // straight to the request: one build resolved
                                // the words and both numbers together, so the
                                // sentence and the send cannot disagree.
                                result.profileHash,
                                result.candidateHash,
                              ),
                        child: Text(l10n.memoryApplyAction),
                      ),
                    )
                else if (decision == MemoryCandidateDecision.promoteToBeliefs)
                  FilledButton(
                    onPressed: busy ? null : onPromote,
                    child: Text(l10n.memoryPromoteAction),
                  ),
                TextButton(
                  onPressed: busy ? null : onReject,
                  child: Text(l10n.memoryRejectAction),
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }
}

class _NoteCard extends StatelessWidget {
  const _NoteCard({
    required this.note,
    required this.l10n,
    required this.palette,
  });

  final MemoryNote note;
  final AppLocalizations l10n;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
    return _Card(
      palette: palette,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            note.title.isEmpty ? note.path : note.title,
            style: TextStyle(
              fontSize: 14,
              fontWeight: FontWeight.w700,
              color: palette.text,
            ),
          ),
          const SizedBox(height: 4),
          SelectableText(
            note.path,
            style: TextStyle(fontSize: 12, color: palette.textMuted),
          ),
          if (note.content.isNotEmpty) ...[
            const SizedBox(height: 10),
            SelectableText(
              note.content,
              style: TextStyle(
                fontSize: 13.5,
                height: 1.5,
                color: palette.text,
              ),
            ),
          ],
          const SizedBox(height: 10),
          _StatusLine(
            text: localizedMemoryNoteStatusCopy(l10n, note.status),
            tone: palette.textMuted,
          ),
          if (note.updatedAt case final updated?)
            _StatusLine(
              text: l10n.memoryLastChanged(
                updated.toLocal(),
                updated.toLocal(),
              ),
              tone: palette.textMuted,
            ),
          if (note.parseError.isNotEmpty) ...[
            const SizedBox(height: 8),
            _ErrorLine(message: note.parseError),
          ],
          if (note.unavailableReason != MemoryUnavailableReason.none)
            // Everything except the server saying nothing is wrong, including
            // its saying nothing at all. An unanswered read rendered as a blank
            // line is one the user reads as a healthy note, and a reason this
            // build has never heard of arrives here as exactly that silence —
            // so it gets the "the server did not say" sentence rather than
            // none.
            _StatusLine(
              text: localizedMemoryUnavailableCopy(
                l10n,
                note.unavailableReason,
              ),
              tone: _reasonTone(note.unavailableReason),
            ),
          if (!note.isIndexable) ...[
            const SizedBox(height: 6),
            _StatusLine(
              text: l10n.memoryNoteUnsearchable,
              tone: AppColors.warning,
            ),
          ],
          // An empty provenance list is a fact, not an absence of one. Left
          // to render nothing it reads as a belief whose sourcing simply was
          // not mentioned, which is exactly the impression a claim resting on
          // nothing must not give.
          if (note.provenance.isEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 6),
              child: _StatusLine(
                text: note.status == MemoryNoteStatus.withdrawn
                    ? l10n.memoryNoteWithdrawnEvidence
                    : l10n.memoryNoteNoEvidence,
                tone: AppColors.warning,
              ),
            ),
          for (final provenance in note.provenance)
            _ProvenanceLine(
              provenance: provenance,
              l10n: l10n,
              palette: palette,
            ),
        ],
      ),
    );
  }
}

/// Where a claim came from, and whether it still stands.
///
/// A withdrawn source and an unevidenced note are stated in words rather than
/// left to an absent line: the whole difference between a belief the user
/// accepted and a claim nothing supports lives here.
class _ProvenanceLine extends StatelessWidget {
  const _ProvenanceLine({
    required this.provenance,
    required this.l10n,
    required this.palette,
  });

  final MemoryProvenance provenance;
  final AppLocalizations l10n;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
    final source = provenance.sourceSessionTitle.isNotEmpty
        ? '${provenance.sourceSessionTitle} · ${provenance.sourceSessionId}'
        : provenance.sourceSessionId;
    return Padding(
      padding: const EdgeInsets.only(top: 6),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (source.isNotEmpty)
            SelectableText(
              l10n.memoryProvenanceFrom(source),
              style: TextStyle(fontSize: 12, color: palette.textMuted),
            ),
          if (provenance.withdrawn)
            _StatusLine(
              text: l10n.memoryNoteWithdrawnEvidence,
              tone: AppColors.warning,
            ),
          if (provenance.evidenceCount > 0)
            _StatusLine(
              text: l10n.memoryEvidenceCount(provenance.evidenceCount),
              tone: palette.textMuted,
            )
          else
            _StatusLine(
              text: l10n.memoryNoteNoEvidence,
              tone: AppColors.warning,
            ),
        ],
      ),
    );
  }
}

class _TierStatus extends StatelessWidget {
  const _TierStatus({
    required this.tier,
    required this.l10n,
    required this.palette,
  });

  final MemoryTierState tier;
  final AppLocalizations l10n;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _StatusLine(
          text: localizedMemoryUnavailableCopy(l10n, tier.unavailableReason),
          tone: _reasonTone(tier.unavailableReason),
        ),
        if (tier.parseError.isNotEmpty) ...[
          const SizedBox(height: 6),
          _ErrorLine(message: tier.parseError),
        ],
        _TierCounts(row: tier, palette: palette),
      ],
    );
  }
}

/// How much a tier holds, and how much is waiting on the user.
///
/// The server counts both; showing them is the difference between "the inbox
/// looks empty" and "the inbox is empty". The counts are the tier's own answer,
/// so a page that renders a shorter list than the vault holds says so.
class _TierCounts extends StatelessWidget {
  const _TierCounts({required this.row, required this.palette});

  final MemoryTierState row;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _StatusLine(
          text: l10n.memoryTierItemCount(row.noteCount),
          tone: palette.textMuted,
        ),
        _StatusLine(
          text: l10n.memoryTierPendingCount(row.pendingCandidateCount),
          tone: palette.textMuted,
        ),
      ],
    );
  }
}

class _SectionHeading extends StatelessWidget {
  const _SectionHeading({required this.text, required this.palette});

  final String text;
  final AppPalette palette;

  @override
  Widget build(BuildContext context) => Text(
    text,
    style: TextStyle(
      fontSize: 16,
      fontWeight: FontWeight.w700,
      color: palette.text,
    ),
  );
}

class _Card extends StatelessWidget {
  const _Card({required this.palette, required this.child});

  final AppPalette palette;
  final Widget child;

  @override
  Widget build(BuildContext context) => Material(
    color: palette.surface,
    shape: RoundedRectangleBorder(
      borderRadius: BorderRadius.circular(12),
      side: BorderSide(color: palette.border),
    ),
    child: Padding(padding: const EdgeInsets.all(16), child: child),
  );
}

class _StatusLine extends StatelessWidget {
  const _StatusLine({required this.text, required this.tone});

  final String text;
  final Color tone;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(top: 4),
    child: Text(
      text,
      style: TextStyle(fontSize: 12.5, height: 1.4, color: tone),
    ),
  );
}

class _ErrorLine extends StatelessWidget {
  const _ErrorLine({required this.message});

  final String message;

  @override
  Widget build(BuildContext context) => Row(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      const Icon(
        Icons.warning_amber_rounded,
        size: 16,
        color: AppColors.danger,
      ),
      const SizedBox(width: 7),
      Expanded(
        child: SelectableText(
          message,
          style: const TextStyle(
            fontSize: 12.5,
            height: 1.35,
            color: AppColors.danger,
          ),
        ),
      ),
    ],
  );
}

Color _reasonTone(MemoryUnavailableReason reason) {
  switch (reason) {
    case MemoryUnavailableReason.none:
      return AppColors.success;
    case MemoryUnavailableReason.disabled:
    case MemoryUnavailableReason.unspecified:
      return AppColors.warning;
    case MemoryUnavailableReason.vaultMissing:
      return AppColors.warning;
    case MemoryUnavailableReason.vaultUnreadable:
    case MemoryUnavailableReason.contentParseFailed:
    case MemoryUnavailableReason.contentTooLarge:
      return AppColors.danger;
  }
}
