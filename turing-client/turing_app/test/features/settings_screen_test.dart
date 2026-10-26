import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/features/settings/settings_screen.dart';
import 'package:turing_flutter_app/models/model_provider.dart';
import 'package:turing_flutter_app/networking/auth_storage.dart';

class _AuthStorage implements ClientAuthStorage {
  @override
  Future<String?> readModelProvider() async => localModelProvider;

  @override
  Future<void> saveModelProvider(String provider) async {}

  @override
  Future<String?> readApiKey() async => 'key';

  @override
  Future<String?> readBackendUrl() async => 'http://localhost:3000';

  @override
  Future<void> save({
    required String backendUrl,
    required String apiKey,
  }) async {}
}

void main() {
  testWidgets('settings names the engine and model serving the local route', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: SettingsScreen(
          authStorage: _AuthStorage(),
          loadLocalEngine: () async =>
              const LocalEngine(label: 'ds4', model: 'deepseek-chat'),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Local · ds4'), findsOneWidget);
    expect(
      find.textContaining('runs on this machine: ds4 serving deepseek-chat'),
      findsOneWidget,
    );
  });

  testWidgets('without a backend to ask, settings says only Local', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(home: SettingsScreen(authStorage: _AuthStorage())),
    );
    await tester.pumpAndSettle();

    expect(find.text('Local'), findsOneWidget);
    expect(find.textContaining('serving'), findsNothing);
  });

  testWidgets('a failed read leaves settings usable and says only Local', (
    tester,
  ) async {
    await tester.pumpWidget(
      MaterialApp(
        home: SettingsScreen(
          authStorage: _AuthStorage(),
          loadLocalEngine: () async => throw Exception('offline'),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Local'), findsOneWidget);
    expect(find.text('Save'), findsOneWidget);
  });

  testWidgets('a longest engine label fits a phone-width settings screen', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(360, 800);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final label = 'W' * 64;
    var saved = '';
    await tester.pumpWidget(
      MaterialApp(
        home: SettingsScreen(
          authStorage: _RecordingAuthStorage((provider) => saved = provider),
          loadLocalEngine: () async =>
              LocalEngine(label: label, model: 'deepseek-chat'),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);

    await tester.tap(find.byType(DropdownButton<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('OpenAI-compatible').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Save'));
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    expect(saved, 'openai_compatible');
  });
}

class _RecordingAuthStorage extends _AuthStorage {
  _RecordingAuthStorage(this.onSave);

  final void Function(String provider) onSave;

  @override
  Future<void> saveModelProvider(String provider) async => onSave(provider);
}
