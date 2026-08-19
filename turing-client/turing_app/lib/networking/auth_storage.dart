import 'package:flutter_secure_storage/flutter_secure_storage.dart';

abstract class ClientAuthStorage {
  Future<void> save({required String backendUrl, required String apiKey});

  Future<String?> readBackendUrl();

  Future<String?> readApiKey();

  /// Which model provider to send with. A preference, not a per-conversation
  /// decision — it lived above the composer on every chat, which put a setting
  /// you change once in the way of the thing you do constantly.
  Future<String?> readModelProvider();

  Future<void> saveModelProvider(String provider);
}

class AuthStorage implements ClientAuthStorage {
  const AuthStorage([this._storage = _defaultStorage]);

  /// macOS defaults to the data-protection keychain, which only works for an
  /// app signed with a keychain-access-group entitlement. A locally built
  /// (adhoc-signed) app has no such entitlement, so every write fails at the OS
  /// layer and the Future never completes — the Settings screen sits on
  /// "Saving..." forever with no error. The legacy file-based keychain works
  /// without a signing identity, which is what a local-first desktop app needs.
  static const _defaultStorage = FlutterSecureStorage(
    mOptions: MacOsOptions(useDataProtectionKeyChain: false),
  );

  final FlutterSecureStorage _storage;
  static const _backendUrlKey = 'turing_backend_url';
  static const _apiKeyKey = 'turing_api_key';
  static const _modelProviderKey = 'turing_model_provider';

  @override
  Future<void> save({
    required String backendUrl,
    required String apiKey,
  }) async {
    await _storage.write(key: _backendUrlKey, value: backendUrl.trim());
    await _storage.write(key: _apiKeyKey, value: apiKey.trim());
  }

  @override
  Future<String?> readModelProvider() => _storage.read(key: _modelProviderKey);

  @override
  Future<void> saveModelProvider(String provider) =>
      _storage.write(key: _modelProviderKey, value: provider.trim());

  @override
  Future<String?> readBackendUrl() => _storage.read(key: _backendUrlKey);

  @override
  Future<String?> readApiKey() => _storage.read(key: _apiKeyKey);
}
