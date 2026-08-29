import 'package:flutter/material.dart';

import '../../l10n/generated/app_localizations.dart';
import '../../l10n/memory_localizations.dart';
import '../../models/remote_egress.dart';

Future<bool> showRemoteEgressDialog(
  BuildContext context,
  RemoteEgressDisclosure disclosure,
) async {
  return await showDialog<bool>(
        context: context,
        barrierDismissible: false,
        builder: (context) {
          final l10n = AppLocalizations.of(context);
          final small = Theme.of(context).textTheme.bodySmall;
          return AlertDialog(
            title: Text(
              disclosure.endpointHost.isEmpty
                  ? l10n.egressDialogTitleUnknownHost
                  : l10n.egressDialogTitle(disclosure.endpointHost),
            ),
            content: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 520),
              child: SingleChildScrollView(
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      '${disclosure.provider} / ${disclosure.model}\n'
                      '${disclosure.endpoint}',
                    ),
                    const SizedBox(height: 16),
                    _Heading(l10n.egressDialogMaySendHeading),
                    const SizedBox(height: 8),
                    if (disclosure.remoteMcpServers.isNotEmpty) ...[
                      _Heading(l10n.egressDialogMcpHeading),
                      const SizedBox(height: 8),
                      for (final server in disclosure.remoteMcpServers)
                        Padding(
                          padding: const EdgeInsets.only(bottom: 6),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text('${server.serverName} · ${server.endpoint}'),
                              for (final tool in disclosure.selectedTools.where(
                                (tool) =>
                                    tool.startsWith('${server.serverName}/'),
                              ))
                                Padding(
                                  padding: const EdgeInsets.only(
                                    left: 12,
                                    top: 2,
                                  ),
                                  child: Text(tool),
                                ),
                            ],
                          ),
                        ),
                      const SizedBox(height: 8),
                    ],
                    if (disclosure.integrationEndpoints.isNotEmpty) ...[
                      _Heading(l10n.egressDialogIntegrationHeading),
                      const SizedBox(height: 8),
                      for (final destination in disclosure.integrationEndpoints)
                        Padding(
                          padding: const EdgeInsets.only(bottom: 6),
                          child: Text(
                            '${destination.displayName} · '
                            '${destination.endpointHost}\n'
                            '${destination.connectionId}',
                          ),
                        ),
                      const SizedBox(height: 8),
                    ],
                    for (final category in disclosure.dataCategories)
                      Padding(
                        padding: const EdgeInsets.only(bottom: 6),
                        child: Row(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            const Text('• '),
                            Expanded(
                              child: Text(
                                localizedEgressCategoryCopy(l10n, category),
                              ),
                            ),
                          ],
                        ),
                      ),
                    if (disclosure.dataCategories.contains(
                          EgressDataCategory.skillContent,
                        ) &&
                        disclosure.skills.isNotEmpty) ...[
                      const SizedBox(height: 8),
                      _Heading(l10n.egressDialogSkillsHeading),
                      const SizedBox(height: 6),
                      for (final skill in disclosure.skills)
                        Padding(
                          padding: const EdgeInsets.only(bottom: 8),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(skill.displayName),
                              Text(
                                skill.bodyMayBeSent
                                    ? l10n.egressSkillBodyMayBeSent
                                    : l10n.egressSkillNameOnly,
                                style: small,
                              ),
                            ],
                          ),
                        ),
                    ],
                    if (disclosure.mentionsMemory)
                      _MemorySection(disclosure: disclosure),
                    const SizedBox(height: 8),
                    Text(l10n.egressDialogSingleRunNotice),
                    const SizedBox(height: 4),
                    Text(
                      l10n.egressDialogExpiry(
                        disclosure.expiresAt.toLocal(),
                        disclosure.expiresAt.toLocal(),
                      ),
                      style: small,
                    ),
                  ],
                ),
              ),
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.of(context).pop(false),
                child: Text(l10n.egressDialogCancel),
              ),
              FilledButton(
                onPressed: () => Navigator.of(context).pop(true),
                child: Text(l10n.egressDialogSend),
              ),
            ],
          );
        },
      ) ??
      false;
}

/// What this run does with memory, in the two shapes memory actually takes.
///
/// Pinned means the words are already in the prompt. Reachable means a tool
/// call would have to go and get them, and might never. Collapsing the two into
/// one "pinned into this run" heading — which is what a single list of every
/// disclosed row amounts to — tells a person their accepted beliefs are already
/// on their way to a remote model when they are not, and that is the exact
/// claim this dialog exists to get right.
///
/// Neither the snapshot fingerprint nor any content hash appears here: a person
/// cannot check a digest, and showing one only teaches them to click past it.
class _MemorySection extends StatelessWidget {
  const _MemorySection({required this.disclosure});

  final RemoteEgressDisclosure disclosure;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final small = Theme.of(context).textTheme.bodySmall;
    final pinned = disclosure.pinnedMemory;
    final reachable = disclosure.toolReachableMemory;
    final tools = disclosure.memoryTools;

    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SizedBox(height: 8),
        if (pinned.isNotEmpty) ...[
          _Heading(l10n.memoryEgressPinnedHeading),
          const SizedBox(height: 4),
          Text(l10n.memoryEgressPinnedDetail, style: small),
          const SizedBox(height: 6),
          for (final note in pinned) _MemoryRow(note: note),
        ],
        if (reachable.isNotEmpty) ...[
          _Heading(l10n.memoryEgressReachableHeading),
          const SizedBox(height: 4),
          Text(l10n.memoryEgressReachableDetail, style: small),
          const SizedBox(height: 6),
          for (final note in reachable) _MemoryRow(note: note),
        ],
        if (tools.isNotEmpty) ...[
          _Heading(l10n.memoryEgressToolsHeading),
          const SizedBox(height: 6),
          for (final tool in tools)
            Padding(
              padding: const EdgeInsets.only(bottom: 4),
              child: Text(tool),
            ),
          Text(l10n.memoryEgressToolsDetail, style: small),
        ],
        // The category was disclosed and nothing was named under it. Saying
        // that out loud beats an empty space, which reads as "nothing".
        if (pinned.isEmpty && reachable.isEmpty && tools.isEmpty)
          Text(l10n.memoryEgressUnnamed, style: small),
      ],
    );
  }
}

class _MemoryRow extends StatelessWidget {
  /// Two beliefs can carry the same title, so a title is not an identity.
  /// Keying on the id the server named keeps each row its own thing rather
  /// than one Flutter is free to recycle into another. The shipped server
  /// always sets an id — pinned documents carry their vault path in it — so
  /// the vault-path fallback is defence against a server that leaves the
  /// field empty, not a case any current wire produces.
  _MemoryRow({required this.note})
    : super(
        key: ValueKey(
          'egress-memory-${note.noteId.isEmpty ? note.vaultPath : note.noteId}',
        ),
      );

  final MemoryEgressDisclosure note;

  @override
  Widget build(BuildContext context) {
    final l10n = AppLocalizations.of(context);
    final small = Theme.of(context).textTheme.bodySmall;
    final tierCopy = localizedEgressMemoryTierCopy(l10n, note.tier);
    final title = note.title.isEmpty ? note.vaultPath : note.title;
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // A pinned document's server title is its tier said again —
          // "Persona" under MEMORY_TIER_PERSONA — and "Persona · Persona"
          // reads as a glitch in the one dialog that must read as
          // deliberate. When the title repeats the tier, say it once; the
          // vault path below still names the file.
          Text(title == tierCopy ? tierCopy : '$tierCopy · $title'),
          Text(note.vaultPath, style: small),
          Text(
            note.bodyMayBeSent
                ? l10n.memoryEgressBodyMayBeSent
                : l10n.memoryEgressNameOnly,
            style: small,
          ),
        ],
      ),
    );
  }
}

class _Heading extends StatelessWidget {
  const _Heading(this.text);

  final String text;

  @override
  Widget build(BuildContext context) {
    return Text(text, style: const TextStyle(fontWeight: FontWeight.bold));
  }
}
