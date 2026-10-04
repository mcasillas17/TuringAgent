import 'package:fixnum/fixnum.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/generated/turing/v1/team.pb.dart' as teampb;
import 'package:turing_flutter_app/models/agent_profile.dart';
import 'package:turing_flutter_app/models/grpc_mappers.dart';

void main() {
  test('agent profile mapper carries every declared and resolved field', () {
    final model = GrpcMappers.agentProfileToModel(
      teampb.AgentProfile(
        profileId: 'research',
        name: 'Research',
        emoji: '🔬',
        description: 'Researches topics',
        version: '1',
        model: 'qwen2.5:7b',
        resolvedModel: 'qwen2.5:7b',
        tools: ['memory.read', 'files.*'],
        skills: ['research/*'],
        memory:
            teampb.AgentProfileMemoryAccess.AGENT_PROFILE_MEMORY_ACCESS_READ,
        requires: ['memory.read'],
        maxToolCalls: 12,
        revision: 'rev-2',
        enabled: true,
        grantedRevision: 'rev-1',
        state: teampb.AgentProfileState.AGENT_PROFILE_STATE_NEEDS_GRANT,
        resolvedTools: ['memory/read', 'files/read_file'],
        unavailableReasons: ['no live worker serves model qwen2.5:7b'],
        excludedTools: [
          teampb.AgentProfileToolExclusion(
            tool: 'files/write_file',
            reason: 'disabled by policy',
          ),
        ],
        parseError: '',
      ),
    );

    expect(model.profileId, 'research');
    expect(model.name, 'Research');
    expect(model.emoji, '🔬');
    expect(model.description, 'Researches topics');
    expect(model.version, '1');
    expect(model.model, 'qwen2.5:7b');
    expect(model.resolvedModel, 'qwen2.5:7b');
    expect(model.tools, ['memory.read', 'files.*']);
    expect(model.skills, ['research/*']);
    expect(model.memory, AgentProfileMemoryAccess.read);
    expect(model.requires, ['memory.read']);
    expect(model.maxToolCalls, 12);
    expect(model.revision, 'rev-2');
    expect(model.enabled, isTrue);
    expect(model.grantedRevision, 'rev-1');
    expect(model.grantsCurrentRevision, isFalse);
    expect(model.state, AgentProfileState.needsGrant);
    expect(model.resolvedTools, ['memory/read', 'files/read_file']);
    expect(model.unavailableReasons, [
      'no live worker serves model qwen2.5:7b',
    ]);
    expect(model.excludedTools.single.tool, 'files/write_file');
    expect(model.excludedTools.single.reason, 'disabled by policy');
  });

  test('every wire state and memory level has its own model value', () {
    final states = {
      teampb.AgentProfileState.AGENT_PROFILE_STATE_ACTIVE:
          AgentProfileState.active,
      teampb.AgentProfileState.AGENT_PROFILE_STATE_DISABLED:
          AgentProfileState.disabled,
      teampb.AgentProfileState.AGENT_PROFILE_STATE_NEEDS_GRANT:
          AgentProfileState.needsGrant,
      teampb.AgentProfileState.AGENT_PROFILE_STATE_UNAVAILABLE:
          AgentProfileState.unavailable,
      teampb.AgentProfileState.AGENT_PROFILE_STATE_PARSE_ERROR:
          AgentProfileState.parseError,
      teampb.AgentProfileState.AGENT_PROFILE_STATE_UNSPECIFIED:
          AgentProfileState.unknown,
    };
    for (final entry in states.entries) {
      final model = GrpcMappers.agentProfileToModel(
        teampb.AgentProfile(profileId: 'dev', state: entry.key),
      );
      expect(model.state, entry.value, reason: '${entry.key}');
    }
    final levels = {
      teampb.AgentProfileMemoryAccess.AGENT_PROFILE_MEMORY_ACCESS_NONE:
          AgentProfileMemoryAccess.none,
      teampb.AgentProfileMemoryAccess.AGENT_PROFILE_MEMORY_ACCESS_READ:
          AgentProfileMemoryAccess.read,
      teampb.AgentProfileMemoryAccess.AGENT_PROFILE_MEMORY_ACCESS_PROPOSE:
          AgentProfileMemoryAccess.propose,
      teampb.AgentProfileMemoryAccess.AGENT_PROFILE_MEMORY_ACCESS_UNSPECIFIED:
          AgentProfileMemoryAccess.unknown,
    };
    for (final entry in levels.entries) {
      final model = GrpcMappers.agentProfileToModel(
        teampb.AgentProfile(profileId: 'dev', memory: entry.key),
      );
      expect(model.memory, entry.value, reason: '${entry.key}');
    }
  });

  // A newer backend may add a state. This build must never read one it does
  // not know as active, or as any state that would let the user skip a grant.
  test('enum values this build does not know become unknown', () {
    final profile = teampb.AgentProfile(profileId: 'future');
    profile.unknownFields.mergeVarintField(10, Int64(4242));
    profile.unknownFields.mergeVarintField(16, Int64(4242));

    final model = GrpcMappers.agentProfileToModel(profile);

    expect(model.state, AgentProfileState.unknown);
    expect(model.memory, AgentProfileMemoryAccess.unknown);
  });

  test('a profile that never parsed is labelled by its folder', () {
    final model = GrpcMappers.agentProfileToModel(
      teampb.AgentProfile(
        profileId: 'broken',
        state: teampb.AgentProfileState.AGENT_PROFILE_STATE_PARSE_ERROR,
        parseError: 'frontmatter is not valid YAML',
      ),
    );

    expect(model.label, 'broken');
    expect(model.grantsCurrentRevision, isFalse);
  });
}
