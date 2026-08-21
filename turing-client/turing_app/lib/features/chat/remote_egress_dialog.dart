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
          title: Text('Send data to ${disclosure.endpointHost}?'),
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
