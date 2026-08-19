import 'package:turing_flutter_app/models/skill.dart';

/// Fills in the skill surface for fakes belonging to tests that are not about
/// skills.
///
/// Reads answer empty; every mutation throws. A test that unexpectedly reaches
/// one of these should fail loudly rather than quietly succeed against a stub
/// that records nothing — silent success is how a broken call site survives a
/// green suite.
mixin NoSkillsApi {
  Future<List<Skill>> listSkills() async => const [];

  Future<List<Skill>> listSessionSkills({required String sessionId}) async =>
      const [];

  Future<Skill> createSkill({
    required String name,
    required String instructions,
  }) async => throw UnimplementedError('this test does not exercise skills');

  Future<Skill> updateSkill({
    required String skillId,
    required String name,
    required String instructions,
  }) async => throw UnimplementedError('this test does not exercise skills');

  Future<void> deleteSkill({required String skillId}) async =>
      throw UnimplementedError('this test does not exercise skills');

  Future<List<Skill>> attachSkill({
    required String sessionId,
    required String skillId,
  }) async => throw UnimplementedError('this test does not exercise skills');

  Future<List<Skill>> detachSkill({
    required String sessionId,
    required String skillId,
  }) async => throw UnimplementedError('this test does not exercise skills');
}
