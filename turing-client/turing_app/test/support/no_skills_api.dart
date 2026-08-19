import 'package:turing_flutter_app/models/skill.dart';

/// Fills in the skill surface for fakes belonging to tests that are not about
/// skills. Unexpected mutations fail loudly.
mixin NoSkillsApi {
  Future<List<Skill>> listSkills() async => const [];

  Future<Skill> getSkill({required String skillId}) async =>
      throw UnimplementedError('this test does not exercise skills');

  Future<Skill> setSkillEnabled({
    required String skillId,
    required bool enabled,
  }) async => throw UnimplementedError('this test does not exercise skills');

  Future<Skill> setSkillCapabilityGrant({
    required String skillId,
    required String capability,
    required bool granted,
  }) async => throw UnimplementedError('this test does not exercise skills');
}
