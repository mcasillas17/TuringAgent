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
}
