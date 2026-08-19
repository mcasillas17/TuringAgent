import 'package:turing_flutter_app/models/integration.dart';

/// Fills in the integrations surface for fakes belonging to tests that are not
/// about integrations.
///
/// Reads answer empty; every mutation throws. A test that unexpectedly reaches
/// one of these should fail loudly rather than quietly succeed against a stub
/// that records nothing — and for this surface in particular, a mutation that
/// silently succeeds would be a credential going somewhere nobody looked.
mixin NoIntegrationsApi {
  Future<IntegrationCatalogue> listIntegrationProviders() async =>
      const IntegrationCatalogue(providers: [], storageConfigured: true);

  Future<List<IntegrationConnection>> listConnections() async => const [];

  Future<IntegrationConnection> connectAccount({
    required IntegrationProviderKind provider,
    required String displayName,
    required String credential,
    required bool consentAcknowledged,
    String accountLabel = '',
    String endpoint = '',
  }) async =>
      throw UnimplementedError('this test does not exercise integrations');

  Future<IntegrationConnection> revokeConnection({
    required String connectionId,
  }) async =>
      throw UnimplementedError('this test does not exercise integrations');

  Future<void> deleteConnection({required String connectionId}) async =>
      throw UnimplementedError('this test does not exercise integrations');
}
