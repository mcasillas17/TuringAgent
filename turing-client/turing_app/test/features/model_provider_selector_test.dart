import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/chat/model_provider_selector.dart';
import 'package:turing_flutter_app/models/model_provider.dart';

void main() {
  testWidgets(
    'model provider selector changes between Ollama and OpenAI-compatible',
    (tester) async {
      var selected = 'ollama';
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(
            body: ModelProviderSelector(
              value: selected,
              onChanged: (value) => selected = value,
            ),
          ),
        ),
      );

      await tester.tap(find.byType(DropdownButton<String>));
      await tester.pumpAndSettle();
      await tester.tap(find.text('OpenAI-compatible').last);

      expect(selected, 'openai_compatible');
    },
  );

  testWidgets('the local route is named by the engine serving it', (
    tester,
  ) async {
    Future<void> pump(String? engine) => tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: ModelProviderSelector(
            value: localModelProvider,
            localEngineLabel: engine,
            onChanged: (_) {},
          ),
        ),
      ),
    );
    await pump('ds4');
    expect(find.text('Local · ds4'), findsOneWidget);
    await pump(null);
    expect(find.text('Local'), findsOneWidget);
    await pump('');
    expect(find.text('Local'), findsOneWidget);
  });
}
