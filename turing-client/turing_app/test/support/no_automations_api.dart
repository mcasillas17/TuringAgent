import 'package:turing_flutter_app/models/automation.dart';

/// Fills in the automation surface for fakes belonging to tests that are not
/// about automations.
///
/// Reads answer empty; every mutation throws. A test that unexpectedly reaches
/// one of these should fail loudly rather than quietly succeed against a stub
/// that records nothing — silent success is how a broken call site survives a
/// green suite. This mirrors [NoSkillsApi] in the neighbouring file.
mixin NoAutomationsApi {
  Future<List<Automation>> listAutomations() async => const [];

  Future<Automation> createAutomation({
    required String name,
    required String prompt,
    required AutomationSchedule schedule,
    required bool enabled,
    required List<AutomationTool> allowedTools,
  }) async =>
      throw UnimplementedError('this test does not exercise automations');

  Future<Automation> updateAutomation({
    required String automationId,
    required String name,
    required String prompt,
    required AutomationSchedule schedule,
    required List<AutomationTool> allowedTools,
  }) async =>
      throw UnimplementedError('this test does not exercise automations');

  Future<Automation> setAutomationEnabled({
    required String automationId,
    required bool enabled,
  }) async =>
      throw UnimplementedError('this test does not exercise automations');

  Future<void> deleteAutomation({required String automationId}) async =>
      throw UnimplementedError('this test does not exercise automations');
}
