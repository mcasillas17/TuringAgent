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
