import 'package:flutter_test/flutter_test.dart';
import 'package:turing_flutter_app/generated/turing/v1/common.pb.dart'
    as commonpb;
import 'package:turing_flutter_app/generated/turing/v1/sessions.pb.dart'
    as sessionpb;
import 'package:turing_flutter_app/models/grpc_mappers.dart';
import 'package:turing_flutter_app/models/tool_descriptor.dart';

/// The widget tests build [ToolDescriptor]s directly, so without these the
/// enum mapping is never exercised against a real proto value and a
/// mis-mapped policy would ship green.
void main() {
  group('toolPolicyToModel', () {
    test('maps every policy the proto defines today', () {
      expect(
        GrpcMappers.toolPolicyToModel(commonpb.ToolPolicy.TOOL_POLICY_SAFE),
        ToolPolicy.safe,
      );
      expect(
        GrpcMappers.toolPolicyToModel(
          commonpb.ToolPolicy.TOOL_POLICY_APPROVAL_REQUIRED,
        ),
        ToolPolicy.approvalRequired,
      );
      expect(
        GrpcMappers.toolPolicyToModel(commonpb.ToolPolicy.TOOL_POLICY_DISABLED),
        ToolPolicy.disabled,
      );
      expect(
        GrpcMappers.toolPolicyToModel(
          commonpb.ToolPolicy.TOOL_POLICY_UNSPECIFIED,
        ),
        ToolPolicy.unspecified,
      );
    });

    test('an unrecognised policy is never assumed safe', () {
      // What an older client receives from a newer backend. Defaulting this
      // to "safe" would tell the user a gated tool runs without asking.
      final future = commonpb.ToolPolicy.valueOf(9999);
      expect(
        GrpcMappers.toolPolicyToModel(
          future ?? commonpb.ToolPolicy.TOOL_POLICY_UNSPECIFIED,
        ),
        ToolPolicy.unspecified,
      );
    });

    test('every declared proto policy has a mapping', () {
      // Guards the switch's default: a policy added to the proto and left
      // unmapped would silently fall through to "unknown" everywhere.
      final mapped = commonpb.ToolPolicy.values
          .where((p) => p != commonpb.ToolPolicy.TOOL_POLICY_UNSPECIFIED)
          .map(GrpcMappers.toolPolicyToModel);
      expect(mapped, everyElement(isNot(ToolPolicy.unspecified)));
    });
  });

  test('toolToModel carries server, tool and policy through', () {
    final model = GrpcMappers.toolToModel(
      sessionpb.ToolDescriptor(
        serverName: 'files',
        toolName: 'write_file',
        policy: commonpb.ToolPolicy.TOOL_POLICY_APPROVAL_REQUIRED,
      ),
    );

    expect(model.serverName, 'files');
    expect(model.toolName, 'write_file');
    expect(model.policy, ToolPolicy.approvalRequired);
  });

  test('agentToModel uses the enum name as the id', () {
    final model = GrpcMappers.agentToModel(
      commonpb.AgentDescriptor(
        id: commonpb.AgentId.AGENT_ID_GENERAL_ASSISTANT,
        displayName: 'General Assistant',
      ),
    );

    expect(model.id, 'AGENT_ID_GENERAL_ASSISTANT');
    expect(model.displayName, 'General Assistant');
  });
}
