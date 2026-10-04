import 'package:turing_flutter_app/models/agent_profile.dart';

/// Fills in the team surface for fakes belonging to tests that are not about
/// the team: the read answers "no specialists". Every mutation keeps
/// [TuringApi]'s own body, which throws `team_unsupported`.
mixin NoTeamApi {
  Future<List<AgentProfile>> listAgentProfiles() async => const [];
}
