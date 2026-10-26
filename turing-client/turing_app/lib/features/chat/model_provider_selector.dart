import 'package:flutter/material.dart';

import '../../models/model_provider.dart';

class ModelProviderSelector extends StatelessWidget {
  const ModelProviderSelector({
    super.key,
    required this.value,
    required this.onChanged,
    this.localEngineLabel,
  });

  final String value;
  final ValueChanged<String> onChanged;

  /// The engine serving the local route, when the backend has said; the
  /// local entry reads "Local" without it.
  final String? localEngineLabel;

  @override
  Widget build(BuildContext context) {
    return DropdownButton<String>(
      value: value,
      isExpanded: true,
      items: [
        DropdownMenuItem(
          value: localModelProvider,
          child: Text(
            localEngineLabel == null || localEngineLabel!.isEmpty
                ? 'Local'
                : 'Local · $localEngineLabel',
            overflow: TextOverflow.ellipsis,
          ),
        ),
        const DropdownMenuItem(
          value: 'openai_compatible',
          child: Text('OpenAI-compatible'),
        ),
      ],
      onChanged: (value) {
        if (value != null) onChanged(value);
      },
    );
  }
}
