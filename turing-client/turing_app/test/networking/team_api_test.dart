import 'package:flutter_test/flutter_test.dart';
import 'package:grpc/grpc.dart' as grpc;
import 'package:protobuf/well_known_types/google/protobuf/timestamp.pb.dart';
import 'package:turing_flutter_app/generated/turing/v1/common.pb.dart'
    as commonpb;
import 'package:turing_flutter_app/generated/turing/v1/team.pb.dart' as teampb;
import 'package:turing_flutter_app/generated/turing/v1/team.pbgrpc.dart'
    as teamgrpc;
import 'package:turing_flutter_app/models/agent_profile.dart';
import 'package:turing_flutter_app/models/remote_egress.dart';
import 'package:turing_flutter_app/networking/api_client.dart';
import 'package:turing_flutter_app/networking/grpc_client.dart';

void main() {
  Future<TuringGrpcApi> connect(grpc.Service service) async {
    final server = grpc.Server.create(services: [service]);
    await server.serve(address: '127.0.0.1', port: 0);
    final channel = grpc.ClientChannel(
      '127.0.0.1',
      port: server.port!,
      options: const grpc.ChannelOptions(
        credentials: grpc.ChannelCredentials.insecure(),
      ),
    );
    addTearDown(() async {
      await channel.shutdown();
      await server.shutdown();
    });
    return TuringGrpcApi(
      baseUrl: 'http://127.0.0.1:${server.port}',
      apiKey: 'client-key',
      channel: channel,
    );
  }

  test('listAgentProfiles maps every profile the backend found', () async {
    final service = _TeamService();
    final api = await connect(service);

    final profiles = await api.listAgentProfiles();

    expect(profiles.map((profile) => profile.profileId), ['dev', 'broken']);
    expect(profiles.first.state, AgentProfileState.needsGrant);
    expect(profiles.first.resolvedTools, ['files/read_file']);
    expect(profiles.last.state, AgentProfileState.parseError);
    expect(profiles.last.parseError, 'name is required');
    expect(service.metadata['authorization'], 'Bearer client-key');
  });

  test('enabling and granting send exactly what the user chose', () async {
    final service = _TeamService();
    final api = await connect(service);

    final granted = await api.grantAgentProfile(
      profileId: 'dev',
      revision: 'rev-1',
    );
    final enabled = await api.setAgentProfileEnabled(
      profileId: 'dev',
      enabled: true,
    );

    expect(service.grants.single.profileId, 'dev');
    expect(service.grants.single.revision, 'rev-1');
    expect(service.enables.single.profileId, 'dev');
    expect(service.enables.single.enabled, isTrue);
    expect(granted.grantedRevision, 'rev-1');
    expect(enabled.enabled, isTrue);
  });

  test('a stale grant surfaces as failed_precondition', () async {
    final api = await connect(_TeamService(staleRevision: true));

    await expectLater(
      api.grantAgentProfile(profileId: 'dev', revision: 'rev-0'),
      throwsA(
        isA<TuringApiException>()
            .having((error) => error.code, 'code', 'failed_precondition')
            .having(
              (error) => error.message,
              'message',
              contains('changed since it was reviewed'),
            ),
      ),
    );
  });

  test(
    'listDelegations asks for one conversation and maps its cards',
    () async {
      final service = _TeamService();
      final api = await connect(service);

      final delegations = await api.listDelegations(parentSessionId: 'sess_1');

      expect(service.delegationRequests.single.parentSessionId, 'sess_1');
      expect(
        service.delegationDeadline,
        isNotNull,
        reason: 'chat startup waits on this read',
      );
      expect(delegations.map((delegation) => delegation.delegationId), [
        'dlg_1',
        'dlg_2',
      ]);
      expect(delegations.first.specialist, '🔬 Research');
      expect(delegations.first.task, 'Gather the notes');
      expect(delegations.last.state, 'waiting_approval');
      expect(delegations.last.approvalId, 'appr_1');
    },
  );

  test('preparing a task\'s consent maps the whole disclosure', () async {
    final service = _TeamService();
    final api = await connect(service);

    final disclosure = await api.prepareDelegationEgress(delegationId: 'dlg_1');

    expect(service.prepared.single.delegationId, 'dlg_1');
    expect(service.prepareDeadline, isNotNull);
    expect(disclosure.challenge, 'signed-challenge');
    expect(disclosure.provider, 'ollama');
    expect(disclosure.selectedTools, [
      'files/files.read',
      'integrations/github.get_issue',
    ]);
    expect(disclosure.dataCategories, [
      EgressDataCategory.toolArguments,
      EgressDataCategory.toolResults,
    ]);
    expect(
      disclosure.integrationEndpoints.single.endpointHost,
      'api.github.com',
    );
    expect(disclosure.integrationEndpoints.single.tools, ['github.get_issue']);
  });

  test(
    'allowing sends the acknowledged challenge, declining sends none',
    () async {
      final service = _TeamService();
      final api = await connect(service);

      final allowed = await api.decideDelegationEgress(
        delegationId: 'dlg_1',
        consent: RemoteEgressConsent(
          challenge: 'signed-challenge',
          acknowledgedDataCategories: const [
            EgressDataCategory.toolArguments,
            EgressDataCategory.toolResults,
          ],
        ),
      );
      final declined = await api.decideDelegationEgress(delegationId: 'dlg_2');

      final allow = service.decided.first;
      expect(allow.delegationId, 'dlg_1');
      expect(allow.consent.challenge, 'signed-challenge');
      expect(allow.consent.acknowledged, isTrue);
      expect(allow.consent.acknowledgedDataCategories, [
        commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
        commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_TOOL_RESULTS,
      ]);
      expect(service.decided.last.delegationId, 'dlg_2');
      expect(service.decided.last.hasConsent(), isFalse);
      expect(allowed.egressConsent, 'granted');
      expect(declined.egressConsent, 'declined');
    },
  );

  test('a server without delegations surfaces as a team error code', () async {
    final api = await connect(_TeamService(listFailure: 12));

    await expectLater(
      api.listDelegations(parentSessionId: 'sess_1'),
      throwsA(
        isA<TuringApiException>().having(
          (error) => error.code,
          'code',
          'unimplemented',
        ),
      ),
    );
  });

  test(
    'statuses keep their own codes so the page can tell them apart',
    () async {
      final cases = {
        grpc.StatusCode.invalidArgument: 'invalid_argument',
        grpc.StatusCode.notFound: 'not_found',
        grpc.StatusCode.permissionDenied: 'permission_denied',
        grpc.StatusCode.aborted: 'aborted',
        grpc.StatusCode.unimplemented: 'unimplemented',
        grpc.StatusCode.unavailable: 'unavailable',
        grpc.StatusCode.internal: 'team_error',
      };
      for (final entry in cases.entries) {
        final api = await connect(_TeamService(listFailure: entry.key));
        await expectLater(
          api.listAgentProfiles(),
          throwsA(
            isA<TuringApiException>().having(
              (error) => error.code,
              'code',
              entry.value,
            ),
          ),
          reason: 'status ${entry.key}',
        );
      }
    },
  );
}

class _TeamService extends teamgrpc.TeamServiceBase {
  _TeamService({this.staleRevision = false, this.listFailure});

  final bool staleRevision;
  final int? listFailure;
  final grants = <teampb.GrantAgentProfileRequest>[];
  final enables = <teampb.SetAgentProfileEnabledRequest>[];
  final delegationRequests = <teampb.ListDelegationsRequest>[];
  final prepared = <teampb.PrepareDelegationEgressRequest>[];
  final decided = <teampb.DecideDelegationEgressRequest>[];
  DateTime? prepareDeadline;
  DateTime? delegationDeadline;
  Map<String, String> metadata = const {};

  @override
  Future<teampb.ListAgentProfilesResponse> listAgentProfiles(
    grpc.ServiceCall call,
    teampb.ListAgentProfilesRequest request,
  ) async {
    metadata = Map.of(call.clientMetadata ?? const {});
    final failure = listFailure;
    if (failure != null) {
      throw grpc.GrpcError.custom(failure, 'the team cannot be listed');
    }
    return teampb.ListAgentProfilesResponse(
      profiles: [
        teampb.AgentProfile(
          profileId: 'dev',
          name: 'Dev',
          revision: 'rev-1',
          state: teampb.AgentProfileState.AGENT_PROFILE_STATE_NEEDS_GRANT,
          resolvedTools: ['files/read_file'],
        ),
        teampb.AgentProfile(
          profileId: 'broken',
          state: teampb.AgentProfileState.AGENT_PROFILE_STATE_PARSE_ERROR,
          parseError: 'name is required',
        ),
      ],
    );
  }

  @override
  Future<teampb.AgentProfile> setAgentProfileEnabled(
    grpc.ServiceCall call,
    teampb.SetAgentProfileEnabledRequest request,
  ) async {
    enables.add(request);
    return teampb.AgentProfile(
      profileId: request.profileId,
      enabled: request.enabled,
      revision: 'rev-1',
      grantedRevision: 'rev-1',
    );
  }

  @override
  Future<teampb.AgentProfile> grantAgentProfile(
    grpc.ServiceCall call,
    teampb.GrantAgentProfileRequest request,
  ) async {
    if (staleRevision) {
      throw const grpc.GrpcError.failedPrecondition(
        'team/dev/AGENT.md changed since it was reviewed',
      );
    }
    grants.add(request);
    return teampb.AgentProfile(
      profileId: request.profileId,
      revision: request.revision,
      grantedRevision: request.revision,
    );
  }

  // The internal facet. A public client must never reach it, and this fake
  // refuses it the way the real server's public facet does.
  @override
  Future<teampb.ListTeamToolsResponse> listTeamTools(
    grpc.ServiceCall call,
    teampb.ListTeamToolsRequest request,
  ) async =>
      throw grpc.GrpcError.permissionDenied('team tool discovery is internal');

  @override
  Future<teampb.CallTeamToolResponse> callTeamTool(
    grpc.ServiceCall call,
    teampb.CallTeamToolRequest request,
  ) async =>
      throw grpc.GrpcError.permissionDenied('team tool dispatch is internal');

  @override
  Future<teampb.ListDelegationsResponse> listDelegations(
    grpc.ServiceCall call,
    teampb.ListDelegationsRequest request,
  ) async {
    delegationRequests.add(request);
    delegationDeadline = call.deadline;
    final failure = listFailure;
    if (failure != null) {
      throw grpc.GrpcError.custom(failure, 'delegations cannot be listed');
    }
    return teampb.ListDelegationsResponse(
      delegations: [
        teampb.Delegation(
          delegationId: 'dlg_1',
          parentRunId: 'run_1',
          displayName: 'Research',
          emoji: '🔬',
          task: 'Gather the notes',
          state: 'completed',
        ),
        teampb.Delegation(
          delegationId: 'dlg_2',
          parentRunId: 'run_1',
          displayName: 'Inbox',
          emoji: '📨',
          state: 'waiting_approval',
          approvalId: 'appr_1',
        ),
      ],
    );
  }

  @override
  Future<teampb.PrepareDelegationEgressResponse> prepareDelegationEgress(
    grpc.ServiceCall call,
    teampb.PrepareDelegationEgressRequest request,
  ) async {
    prepared.add(request);
    prepareDeadline = call.deadline;
    return teampb.PrepareDelegationEgressResponse(
      disclosure: commonpb.RemoteEgressDisclosure(
        challenge: 'signed-challenge',
        provider: commonpb.ModelProvider.MODEL_PROVIDER_OLLAMA,
        model: 'llama3.2',
        dataCategories: [
          commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS,
          commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_TOOL_RESULTS,
        ],
        selectedTools: ['files/files.read', 'integrations/github.get_issue'],
        integrationEndpoints: [
          commonpb.IntegrationEgressDestination(
            endpoint: 'https://api.github.com',
            endpointHost: 'api.github.com',
            connectionId: 'conn_1',
            displayName: 'Work GitHub',
            tools: ['github.get_issue'],
          ),
        ],
        expiresAt: Timestamp.fromDateTime(DateTime.utc(2026, 10, 1)),
      ),
    );
  }

  @override
  Future<teampb.Delegation> decideDelegationEgress(
    grpc.ServiceCall call,
    teampb.DecideDelegationEgressRequest request,
  ) async {
    decided.add(request);
    return teampb.Delegation(
      delegationId: request.delegationId,
      parentRunId: 'run_1',
      state: 'queued',
      egressConsent: request.hasConsent() ? 'granted' : 'declined',
    );
  }

  @override
  Future<teampb.Delegation> getDelegation(
    grpc.ServiceCall call,
    teampb.GetDelegationRequest request,
  ) async => throw grpc.GrpcError.unimplemented('not used by these tests');
}
