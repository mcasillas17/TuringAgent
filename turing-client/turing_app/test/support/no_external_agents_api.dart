import 'package:turing_flutter_app/models/external_agent.dart';

/// Fills in the external-agent surface for fakes belonging to tests that are
/// not about agents.
///
/// Reads answer "nothing configured, staying local"; every mutation throws. A
/// test that unexpectedly reaches one of these should fail loudly rather than
/// quietly succeed against a stub that records nothing — silent success is how
/// a broken call site survives a green suite.
mixin NoExternalAgentsApi {
  Future<List<ExternalAgent>> listExternalAgents() async => const [];

  /// Null is the local assistant, which is what a conversation nobody routed
  /// actually does.
  Future<ExternalAgent?> getSessionAgent({required String sessionId}) async =>
      null;

  Future<ExternalAgent> createExternalAgent({
    required String displayName,
    required ExternalAgentProvider provider,
    required String baseUrl,
    required String model,
    required String credentialRef,
  }) async => throw UnimplementedError('this test does not exercise agents');

  Future<ExternalAgent> updateExternalAgent({
    required String agentId,
    required String displayName,
    required ExternalAgentProvider provider,
    required String baseUrl,
    required String model,
    required String credentialRef,
  }) async => throw UnimplementedError('this test does not exercise agents');

  Future<void> deleteExternalAgent({required String agentId}) async =>
      throw UnimplementedError('this test does not exercise agents');

  Future<ExternalAgent?> setSessionAgent({
    required String sessionId,
    required String agentId,
  }) async => throw UnimplementedError('this test does not exercise agents');

  Future<ExternalAgent?> clearSessionAgent({required String sessionId}) async =>
      throw UnimplementedError('this test does not exercise agents');
}
