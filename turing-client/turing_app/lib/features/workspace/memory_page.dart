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

class _MemoryPageState extends State<MemoryPage> {
  late Future<MemoryState> _state;
  final TextEditingController _persona = TextEditingController();
  final TextEditingController _profile = TextEditingController();

  /// One resulting-profile editor per profile_edit proposal on screen, kept on
  /// the page rather than inside the card so a re-read does not throw away what
  /// the user has typed into it.
  final Map<String, TextEditingController> _profileResults = {};

  /// What each of those editors was seeded with. An editor still holding its
  /// seed is one the user has not touched, and may be re-seeded when the
  /// profile or the proposal moves underneath it; one that has drifted holds
  /// their words and is left alone.
  final Map<String, String> _profileResultSeeds = {};

  /// The profile compare-and-set token each result was composed against.
  ///
  /// It travels with the text, not with the page. A result the user has edited
  /// keeps their words when the profile moves underneath it — and it has to
  /// keep this with them, because those words describe a document that is no
  /// longer there. Sending them with the *newer* profile's token would tell the
  /// server "this is an edit of what you have now", and it would be accepted:
  /// whatever the other writer put in profile.md would be gone, silently. Sent
  /// with the token they were actually composed against, the server refuses,
  /// and the user is told to look again.
  final Map<String, String> _profileResultProfileHashes = {};

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
    for (final controller in _profileResults.values) {
      controller.dispose();
    }
    super.dispose();
  }

  /// The editor for one profile proposal's resulting document, created and
  /// seeded on first sight and re-seeded only while the user has not typed
  /// into it.
  TextEditingController _profileResultFor(
    MemoryCandidate candidate,
    String profile,
    String profileHash,
  ) {
    final seed = composeProfileResult(profile, candidate.content);
    final controller = _profileResults.putIfAbsent(candidate.candidateId, () {
      _profileResultSeeds[candidate.candidateId] = seed;
      _profileResultProfileHashes[candidate.candidateId] = profileHash;
      return TextEditingController(text: seed);
    });
    final previousSeed = _profileResultSeeds[candidate.candidateId];
    if (previousSeed != seed && controller.text == previousSeed) {
      controller.text = seed;
      _profileResultSeeds[candidate.candidateId] = seed;
      // Re-seeded means re-composed: these words describe the profile as it
      // reads now, so they are an edit of it and carry its token.
      _profileResultProfileHashes[candidate.candidateId] = profileHash;
    }
    return controller;
  }

  /// The profile token an apply of one proposal has to name: the one its result
  /// was composed against, never whichever read happened most recently.
  String _profileResultHashFor(MemoryCandidate candidate, String fallback) {
    return _profileResultProfileHashes[candidate.candidateId] ?? fallback;
  }

  /// Forgets the editors for proposals that are no longer on the page. A
  /// decided proposal is gone from the vault, and keeping its half-edited
  /// result alive would resurrect it under a reused id.
  void _retainProfileResults(Iterable<String> liveCandidateIds) {
    final live = liveCandidateIds.toSet();
    for (final candidateId in _profileResults.keys.toList()) {
      if (live.contains(candidateId)) continue;
      _profileResults.remove(candidateId)?.dispose();
      _profileResultSeeds.remove(candidateId);
      _profileResultProfileHashes.remove(candidateId);
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
            onApply: (candidate, result) => _mutate(() async {
              final applied = await widget.apiClient.applyMemoryProfile(
                candidateId: candidate.candidateId,
                // The whole resulting document the user reviewed, never the
                // proposal: the server writes exactly these bytes over
                // profile.md.
                content: result,
                // Compare-and-set against the profile document, not the
                // proposal: the question an apply asks is whether profile.md
                // still says what the user was shown beside it. The token is
                // the one this result was composed against, which is not the
                // same as the newest one read whenever the user has edited it.
                expectedContentHash: _profileResultHashFor(
                  candidate,
                  state.profile.contentHash,
                ),
                // And the second one, against the proposal this result was
                // composed from.
                expectedCandidateHash: candidate.contentHash,
              );
              _inboxNotice = applied.cleanupPending
                  ? l10n.memoryProfileAppliedCleanupPending
                  : '';
            }, (message) => _inboxError = message),
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
  /// what profile.md will say — not the proposal, which is a fragment of it.
  final void Function(MemoryCandidate candidate, String result) onApply;

  /// The editor holding that document, owned by the page so it survives the
  /// re-read every write triggers.
  final TextEditingController Function(MemoryCandidate candidate)
  profileResultFor;

  /// Told which proposals are on screen, so the page can forget the editors of
  /// the ones that are not.
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
  static bool _needsProfileResult(MemoryCandidate candidate) =>
      candidate.decision == MemoryCandidateDecision.applyToProfile;

  @override
  Widget build(BuildContext context) {
    final palette = AppColors.of(context);
    final beliefTier = state.tiers
        .where((tier) => tier.tier == MemoryTier.belief)
        .toList();
    onCandidatesBuilt([
      for (final candidate in state.candidates)
        if (_needsProfileResult(candidate)) candidate.candidateId,
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
          vaultConfigured: state.settings.vaultRoot.isNotEmpty,
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
          vaultConfigured: state.settings.vaultRoot.isNotEmpty,
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
                profileHash: state.profile.contentHash,
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
          if (settings.vaultRoot.isNotEmpty) ...[
            const SizedBox(height: 8),
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
    required this.vaultConfigured,
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

  /// Whether a vault is open at all, from `settings.vaultRoot`.
  ///
  /// [MemoryDocument.isWritable] is silent on this: the server reports
  /// VAULT_MISSING on a document both when the vault is open and this file
  /// has simply never been written, and when there is no vault open to write
  /// into. Those are not the same refusal, and the document alone cannot
  /// tell them apart — only the settings row can, so it is threaded down here
  /// rather than folded into [MemoryDocument.isWritable]'s per-file meaning.
  final bool vaultConfigured;
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
  /// can only refuse. [vaultConfigured] is the one signal that tells those
  /// apart, so it gates VAULT_MISSING specifically without touching what
  /// [MemoryDocument.isWritable] means for every other reason.
  bool get _canSave =>
      document.isWritable &&
      (vaultConfigured ||
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
          if (document.contentHash.isNotEmpty) ...[
            const SizedBox(height: 4),
            // The version this editor is holding. It is what a save is
            // compare-and-set against, so it is the one number that explains a
            // refusal — and it is a document hash, never the egress snapshot
            // fingerprint, which is a binding token and stays off screen.
            SelectableText(
              l10n.memoryEditingVersion(document.contentHash),
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
    required this.profileHash,
    required this.profileResult,
    required this.l10n,
    required this.palette,
    required this.busy,
    required this.onPromote,
    required this.onReject,
    required this.onApply,
  });

  final MemoryCandidate candidate;
  final String profileHash;

  /// The resulting-profile editor, for a profile edit this page may apply, and
  /// null for every other proposal. It is owned by the page: a re-read rebuilds
  /// this card, and an editor rebuilt with it would lose what the user typed.
  final TextEditingController? profileResult;
  final AppLocalizations l10n;
  final AppPalette palette;
  final bool busy;
  final VoidCallback onPromote;
  final VoidCallback onReject;
  final void Function(MemoryCandidate candidate, String result) onApply;

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
          if (decision == MemoryCandidateDecision.unsupported) ...[
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
            TextField(
              key: Key('memory-profile-result-${candidate.candidateId}'),
              controller: result,
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
              valueListenable: result,
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
                profileHash.isEmpty ? l10n.memoryNoProfileYet : profileHash,
              ),
              style: TextStyle(fontSize: 12, color: palette.textMuted),
            ),
            Text(
              l10n.memoryExpectedProposalHash(candidate.contentHash),
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
                      valueListenable: result,
                      builder: (context, value, _) => FilledButton(
                        // Nothing to apply is not an apply. An empty document
                        // would replace everything the user has written about
                        // themselves with nothing.
                        onPressed: busy || value.text.trim().isEmpty
                            ? null
                            : () => onApply(candidate, result.text),
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
          if (note.unavailableReason != MemoryUnavailableReason.none &&
              note.unavailableReason != MemoryUnavailableReason.unspecified)
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
