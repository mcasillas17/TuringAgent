import 'package:flutter/material.dart';

import '../../models/remote_egress.dart';

Future<bool> showRemoteEgressDialog(
  BuildContext context,
  RemoteEgressDisclosure disclosure,
) async {
  return await showDialog<bool>(
        context: context,
        barrierDismissible: false,
        builder: (context) => AlertDialog(
          title: Text(
            disclosure.endpointHost.isEmpty
                ? 'Send data off this machine?'
                : 'Send data to ${disclosure.endpointHost}?',
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
                  const Text(
                    'This run may send:',
                    style: TextStyle(fontWeight: FontWeight.bold),
                  ),
                  const SizedBox(height: 8),
                  if (disclosure.remoteMcpServers.isNotEmpty) ...[
                    const Text(
                      'Remote MCP destinations:',
                      style: TextStyle(fontWeight: FontWeight.bold),
                    ),
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
                    const Text(
                      'Connected-account destinations:',
                      style: TextStyle(fontWeight: FontWeight.bold),
                    ),
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
                          Expanded(child: Text(category.label)),
                        ],
                      ),
                    ),
                  if (disclosure.dataCategories.contains(
                    EgressDataCategory.skillContent,
                  )) ...[
                    const SizedBox(height: 8),
                    const Text(
                      'Skills that may be sent:',
                      style: TextStyle(fontWeight: FontWeight.bold),
                    ),
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
                                  ? 'full content may be sent'
                                  : 'name and description only',
                              style: Theme.of(context).textTheme.bodySmall,
                            ),
                          ],
                        ),
                      ),
                  ],
                  if (disclosure.dataCategories.contains(
                    EgressDataCategory.memoryProfile,
                  )) ...[
                    const SizedBox(height: 8),
                    // Memory leaves the machine in two shapes, and the dialog
                    // names whichever ones apply: the pinned documents that
                    // ride along with the prompt, and the memory tools this run
                    // may call. Neither the snapshot fingerprint nor any
                    // content hash appears here — a person cannot check a
                    // digest, and showing one only teaches them to click past
                    // it.
                    if (disclosure.memoryNotes.isNotEmpty) ...[
                      const Text(
                        'Memory pinned into this run:',
                        style: TextStyle(fontWeight: FontWeight.bold),
                      ),
                      const SizedBox(height: 6),
                      for (final note in disclosure.memoryNotes)
                        Padding(
                          padding: const EdgeInsets.only(bottom: 8),
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                '${_memoryTierLabel(note.tier)} · '
                                '${note.title.isEmpty ? note.vaultPath : note.title}',
                              ),
                              Text(
                                note.vaultPath,
                                style: Theme.of(context).textTheme.bodySmall,
                              ),
                              Text(
                                note.bodyMayBeSent
                                    ? 'full content may be sent'
                                    : 'name and location only',
                                style: Theme.of(context).textTheme.bodySmall,
                              ),
                            ],
                          ),
                        ),
                    ],
                    if (disclosure.memoryTools.isNotEmpty) ...[
                      const Text(
                        'The memory tools this run may call:',
                        style: TextStyle(fontWeight: FontWeight.bold),
                      ),
                      const SizedBox(height: 6),
                      for (final tool in disclosure.memoryTools)
                        Padding(
                          padding: const EdgeInsets.only(bottom: 4),
                          child: Text(tool),
                        ),
                      Text(
                        'What those tools return is part of this run and may '
                        'be sent with it.',
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                    ],
                  ],
                  const SizedBox(height: 8),
                  const Text('This consent applies only to this exact run.'),
                  const SizedBox(height: 4),
                  Text(
                    'Confirm before ${disclosure.expiresAt.toLocal()}.',
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                ],
              ),
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.of(context).pop(false),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: () => Navigator.of(context).pop(true),
              child: const Text('Send'),
            ),
          ],
        ),
      ) ??
      false;
}

/// The tier a disclosed memory belongs to, in the user's words.
///
/// An unrecognised tier is called "Memory" rather than guessed at: saying
/// something true and vague beats naming a tier the server never claimed.
String _memoryTierLabel(MemoryEgressTier tier) {
  switch (tier) {
    case MemoryEgressTier.persona:
      return 'Persona';
    case MemoryEgressTier.profile:
      return 'Profile';
    case MemoryEgressTier.belief:
      return 'Belief';
    case MemoryEgressTier.note:
      return 'Note';
    case MemoryEgressTier.unspecified:
      return 'Memory';
  }
}
