import 'dart:async';

import 'package:fixnum/fixnum.dart';
import 'package:grpc/grpc.dart' as grpc;
import 'package:grpc/service_api.dart' as grpc_api;

import '../generated/google/protobuf/timestamp.pb.dart' as timestamppb;
import '../generated/turing/v1/agents.pb.dart' as agentpb;
import '../generated/turing/v1/agents.pbgrpc.dart' as agentgrpc;
import '../generated/turing/v1/approvals.pb.dart' as approvalpb;
import '../generated/turing/v1/approvals.pbgrpc.dart' as approvalgrpc;
import '../generated/turing/v1/audit.pb.dart' as auditpb;
import '../generated/turing/v1/audit.pbgrpc.dart' as auditgrpc;
import '../generated/turing/v1/automations.pb.dart' as automationpb;
import '../generated/turing/v1/automations.pbgrpc.dart' as automationgrpc;
import '../generated/turing/v1/chat.pb.dart' as chatpb;
import '../generated/turing/v1/chat.pbgrpc.dart' as chatgrpc;
import '../generated/turing/v1/common.pb.dart' as commonpb;
import '../generated/turing/v1/events.pb.dart' as eventpb;
import '../generated/turing/v1/events.pbgrpc.dart' as eventgrpc;
import '../generated/turing/v1/integrations.pb.dart' as integrationpb;
import '../generated/turing/v1/integrations.pbgrpc.dart' as integrationgrpc;
import '../generated/turing/v1/sessions.pb.dart' as sessionpb;
import '../generated/turing/v1/sessions.pbgrpc.dart' as sessiongrpc;
import '../generated/turing/v1/skills.pb.dart' as skillpb;
import '../generated/turing/v1/skills.pbgrpc.dart' as skillgrpc;
import '../generated/turing/v1/telemetry.pb.dart' as telemetrypb;
import '../generated/turing/v1/telemetry.pbgrpc.dart' as telemetrygrpc;
import '../models/agent_descriptor.dart';
import '../models/audit.dart';
import '../models/automation.dart';
import '../models/external_agent.dart';
import '../models/grpc_mappers.dart';
import '../models/integration.dart';
import '../models/message.dart';
import '../models/remote_egress.dart';
import '../models/search_hit.dart';
import '../models/session.dart';
import '../models/skill.dart';
import '../models/telemetry.dart';
import '../models/tool_descriptor.dart';
import '../models/turing_event.dart';
import 'api_client.dart';

const _startupUnaryTimeout = Duration(seconds: 10);

class GrpcAuthMetadata {
  const GrpcAuthMetadata({required this.apiKey});

  final String apiKey;

  Map<String, String> headers() => {'authorization': 'Bearer $apiKey'};
}

class GrpcMetadataInterceptor extends grpc.ClientInterceptor {
  GrpcMetadataInterceptor(this.authMetadata);

  final GrpcAuthMetadata authMetadata;

  @override
  grpc_api.ResponseFuture<R> interceptUnary<Q, R>(
    grpc.ClientMethod<Q, R> method,
    Q request,
    grpc.CallOptions options,
    grpc_api.ClientUnaryInvoker<Q, R> invoker,
  ) {
    return invoker(method, request, _withAuth(options));
  }

  @override
  grpc_api.ResponseStream<R> interceptStreaming<Q, R>(
    grpc.ClientMethod<Q, R> method,
    Stream<Q> requests,
    grpc.CallOptions options,
    grpc_api.ClientStreamingInvoker<Q, R> invoker,
  ) {
    return invoker(method, requests, _withAuth(options));
  }

  grpc.CallOptions _withAuth(grpc.CallOptions options) {
    return options.mergedWith(
      grpc.CallOptions(metadata: authMetadata.headers()),
    );
  }
}

abstract class ClosableTuringApi extends TuringApi {
  Future<void> close();
}

class TuringGrpcApi implements ClosableTuringApi, RemoteEgressApi {
  TuringGrpcApi({
    required this.baseUrl,
    required this.apiKey,
    grpc.ClientChannel? channel,
  }) : _channel = channel ?? createTuringGrpcChannel(baseUrl),
       _ownsChannel = channel == null {
    final options = grpc.CallOptions(metadata: _metadata.headers());
    _sessions = sessiongrpc.SessionServiceClient(_channel, options: options);
    _events = eventgrpc.EventServiceClient(_channel, options: options);
    _chat = chatgrpc.ChatServiceClient(_channel, options: options);
    _approvals = approvalgrpc.ApprovalServiceClient(_channel, options: options);
    _skills = skillgrpc.SkillServiceClient(_channel, options: options);
    _externalAgents = agentgrpc.ExternalAgentServiceClient(
      _channel,
      options: options,
    );
    _integrations = integrationgrpc.IntegrationServiceClient(
      _channel,
      options: options,
    );
    _automations = automationgrpc.AutomationServiceClient(
      _channel,
      options: options,
    );
    _telemetry = telemetrygrpc.TelemetryServiceClient(
      _channel,
      options: options,
    );
    _audit = auditgrpc.AuditServiceClient(_channel, options: options);
  }

  final String baseUrl;
  final String apiKey;
  final grpc.ClientChannel _channel;
  final bool _ownsChannel;
  late final sessiongrpc.SessionServiceClient _sessions;
  late final eventgrpc.EventServiceClient _events;
  late final chatgrpc.ChatServiceClient _chat;
  late final approvalgrpc.ApprovalServiceClient _approvals;
  late final skillgrpc.SkillServiceClient _skills;
  late final agentgrpc.ExternalAgentServiceClient _externalAgents;
  late final integrationgrpc.IntegrationServiceClient _integrations;
  late final automationgrpc.AutomationServiceClient _automations;
  late final telemetrygrpc.TelemetryServiceClient _telemetry;
  late final auditgrpc.AuditServiceClient _audit;

  GrpcAuthMetadata get _metadata => GrpcAuthMetadata(apiKey: apiKey);

  @override
  Future<Map<String, dynamic>> getConfig() async {
    final response = await _sessions.getConfig(sessionpb.GetConfigRequest());
    final providers = <String, Map<String, dynamic>>{};
    for (final provider in response.providers) {
      providers[GrpcMappers.modelProviderToString(provider.provider)] = {
        'enabled': provider.enabled,
        'defaultModel': provider.defaultModel,
        'remoteEndpoint': provider.remoteEndpoint,
        'requiresPerRunConsent': provider.requiresPerRunConsent,
      };
    }
    final enabledProviders = response.providers
        .where((provider) => provider.enabled)
        .map((provider) => GrpcMappers.modelProviderToString(provider.provider))
        .toList();
    return {
      'providers': providers,
      'enabledProviders': enabledProviders,
      'approvalsEnabled': response.approvalsEnabled,
      'filesMcpEnabled': response.filesMcpEnabled,
    };
  }

  @override
  Future<Map<String, dynamic>> createSession({String? title}) async {
    final response = await _sessions.createSession(
      sessionpb.CreateSessionRequest(title: title ?? ''),
    );
    return {
      'sessionId': response.sessionId,
      'createdAt': response.createdAt.toDateTime().toUtc().toIso8601String(),
      'createdAtNanoseconds':
          response.createdAt.seconds.toInt() * 1000000000 +
          response.createdAt.nanos,
    };
  }

  @override
  Future<List<Session>> listSessions({int limit = 50, String? after}) async {
    final response = await _sessions.listSessions(
      sessionpb.ListSessionsRequest(
        page: commonpb.PageRequest(limit: limit, cursor: after ?? ''),
      ),
    );
    return response.sessions.map(GrpcMappers.sessionToModel).toList();
  }

  @override
  Future<Session> getSession({required String sessionId}) async {
    final response = await _sessions.getSession(
      sessionpb.GetSessionRequest(sessionId: sessionId),
      options: grpc.CallOptions(timeout: _startupUnaryTimeout),
    );
    return GrpcMappers.sessionToModel(response);
  }

  @override
  Future<void> deleteSession({required String sessionId}) async {
    await _sessions.deleteSession(
      sessionpb.DeleteSessionRequest(sessionId: sessionId),
      options: grpc.CallOptions(timeout: _startupUnaryTimeout),
    );
  }

  @override
  Future<List<Message>> listMessages({
    required String sessionId,
    int limit = 50,
    String? before,
  }) async {
    final response = await _sessions.listMessages(
      sessionpb.ListMessagesRequest(
        sessionId: sessionId,
        limit: limit,
        beforeMessageId: before ?? '',
      ),
      options: grpc.CallOptions(timeout: _startupUnaryTimeout),
    );
    return response.messages.map(GrpcMappers.messageToModel).toList();
  }

  @override
  Future<List<SearchHit>> searchMessages({
    required String query,
    int limit = 50,
  }) async {
    final response = await _sessions.searchMessages(
      sessionpb.SearchMessagesRequest(
        query: query,
        sessionId: '',
        limit: limit,
      ),
      options: grpc.CallOptions(timeout: _startupUnaryTimeout),
    );
    return response.messages.map(GrpcMappers.searchHitToModel).toList();
  }

  @override
  Future<TuringEventPage> listEvents({
    required String sessionId,
    int? after,
    int limit = 500,
  }) async {
    final response = await _events.listEvents(
      eventpb.ListEventsRequest(
        sessionId: sessionId,
        afterSequence: Int64(after ?? 0),
        limit: limit,
      ),
      options: grpc.CallOptions(timeout: _startupUnaryTimeout),
    );
    return TuringEventPage(
      events: response.events
          .map(GrpcMappers.turingEventToTuringEvent)
          .toList(),
      latestSequence: response.latestSequence.toInt(),
    );
  }

  @override
  Future<Map<String, dynamic>> sendMessage({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
    String? idempotencyKey,
  }) {
    return _sendMessage(
      sessionId: sessionId,
      content: content,
      modelProvider: modelProvider,
      idempotencyKey: idempotencyKey,
    );
  }

  @override
  Future<RemoteEgressDisclosure?> prepareRemoteEgress({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
    required String idempotencyKey,
  }) async {
    final response = await _chat.prepareRemoteEgress(
      chatpb.PrepareRemoteEgressRequest(
        sessionId: sessionId,
        content: content,
        contentType: 'text',
        agentId: commonpb.AgentId.AGENT_ID_GENERAL_ASSISTANT,
        modelProvider: GrpcMappers.modelProviderFromString(modelProvider),
        idempotencyKey: idempotencyKey,
      ),
      options: grpc.CallOptions(timeout: _startupUnaryTimeout),
    );
    if (!response.hasDisclosure()) return null;
    final disclosure = response.disclosure;
    return RemoteEgressDisclosure(
      challenge: disclosure.challenge,
      provider: GrpcMappers.modelProviderToString(disclosure.provider),
      model: disclosure.model,
      endpoint: disclosure.endpoint,
      endpointHost: disclosure.endpointHost,
      externalAgentId: disclosure.externalAgentId,
      dataCategories: disclosure.dataCategories
          .map(_egressCategoryFromProto)
          .toList(growable: false),
      expiresAt: disclosure.expiresAt.toDateTime().toUtc(),
    );
  }

  @override
  Future<Map<String, dynamic>> sendMessageWithRemoteEgressConsent({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
    required String idempotencyKey,
    required RemoteEgressConsent consent,
  }) {
    return _sendMessage(
      sessionId: sessionId,
      content: content,
      modelProvider: modelProvider,
      idempotencyKey: idempotencyKey,
      remoteEgressConsent: commonpb.RemoteEgressConsent(
        challenge: consent.challenge,
        acknowledged: true,
        acknowledgedDataCategories: consent.acknowledgedDataCategories.map(
          _egressCategoryToProto,
        ),
      ),
    );
  }

  Future<Map<String, dynamic>> _sendMessage({
    required String sessionId,
    required String content,
    required String modelProvider,
    String? idempotencyKey,
    commonpb.RemoteEgressConsent? remoteEgressConsent,
  }) {
    final stream = _chat.sendMessage(
      chatpb.SendMessageRequest(
        sessionId: sessionId,
        content: content,
        contentType: 'text',
        agentId: commonpb.AgentId.AGENT_ID_GENERAL_ASSISTANT,
        modelProvider: GrpcMappers.modelProviderFromString(modelProvider),
        idempotencyKey: idempotencyKey ?? '',
        remoteEgressConsent: remoteEgressConsent,
      ),
    );
    final queued = Completer<Map<String, dynamic>>();
    late final StreamSubscription<chatpb.ChatStreamEvent> subscription;
    subscription = stream.listen(
      (event) {
        if (event.hasRunQueued() && !queued.isCompleted) {
          queued.complete({
            'sessionId': event.sessionId,
            'runId': event.runQueued.runId,
            'jobId': event.runQueued.jobId,
            'traceId': event.runQueued.traceId,
            'status': 'queued',
          });
        }
        if (_isTerminalChatEvent(event)) {
          unawaited(subscription.cancel());
        }
      },
      onError: (Object error, StackTrace stackTrace) {
        if (!queued.isCompleted) {
          queued.completeError(error, stackTrace);
        }
      },
      onDone: () {
        if (!queued.isCompleted) {
          queued.completeError(
            const TuringApiException(
              code: 'empty_stream',
              message: 'SendMessage stream ended before run queued',
            ),
          );
        }
      },
      cancelOnError: false,
    );
    return queued.future;
  }

  static EgressDataCategory _egressCategoryFromProto(
    commonpb.EgressDataCategory category,
  ) {
    switch (category) {
      case commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_CURRENT_MESSAGE:
        return EgressDataCategory.currentMessage;
      case commonpb
          .EgressDataCategory
          .EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY:
        return EgressDataCategory.conversationHistory;
      case commonpb
          .EgressDataCategory
          .EGRESS_DATA_CATEGORY_CROSS_SESSION_RECALL:
        return EgressDataCategory.crossSessionRecall;
      case commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_MEMORY_PROFILE:
        return EgressDataCategory.memoryProfile;
      case commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_SKILL_CONTENT:
        return EgressDataCategory.skillContent;
      case commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_TOOL_SCHEMAS:
        return EgressDataCategory.toolSchemas;
      case commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS:
        return EgressDataCategory.toolArguments;
      case commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_TOOL_RESULTS:
        return EgressDataCategory.toolResults;
      case commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_ATTACHMENTS:
        return EgressDataCategory.attachments;
      case commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_UNSPECIFIED:
        throw const TuringApiException(
          code: 'invalid_egress_category',
          message: 'The server returned an unspecified egress category',
        );
    }
    throw TuringApiException(
      code: 'invalid_egress_category',
      message: 'The server returned an unknown egress category: $category',
    );
  }

  static commonpb.EgressDataCategory _egressCategoryToProto(
    EgressDataCategory category,
  ) {
    switch (category) {
      case EgressDataCategory.currentMessage:
        return commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_CURRENT_MESSAGE;
      case EgressDataCategory.conversationHistory:
        return commonpb
            .EgressDataCategory
            .EGRESS_DATA_CATEGORY_CONVERSATION_HISTORY;
      case EgressDataCategory.crossSessionRecall:
        return commonpb
            .EgressDataCategory
            .EGRESS_DATA_CATEGORY_CROSS_SESSION_RECALL;
      case EgressDataCategory.memoryProfile:
        return commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_MEMORY_PROFILE;
      case EgressDataCategory.skillContent:
        return commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_SKILL_CONTENT;
      case EgressDataCategory.toolSchemas:
        return commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_TOOL_SCHEMAS;
      case EgressDataCategory.toolArguments:
        return commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_TOOL_ARGUMENTS;
      case EgressDataCategory.toolResults:
        return commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_TOOL_RESULTS;
      case EgressDataCategory.attachments:
        return commonpb.EgressDataCategory.EGRESS_DATA_CATEGORY_ATTACHMENTS;
    }
  }

  @override
  Future<Map<String, dynamic>> approveApproval(
    String approvalId, {
    String? comment,
  }) async {
    final response = await _approvals.approveApproval(
      approvalpb.ApproveApprovalRequest(
        approvalId: approvalId,
        comment: comment ?? '',
      ),
    );
    return {
      'approvalId': response.approvalId,
      'status': GrpcMappers.approvalStatusToString(response.status),
    };
  }

  @override
  Future<Map<String, dynamic>> denyApproval(
    String approvalId, {
    String? reason,
  }) async {
    final response = await _approvals.denyApproval(
      approvalpb.DenyApprovalRequest(
        approvalId: approvalId,
        reason: reason ?? '',
      ),
    );
    return {
      'approvalId': response.approvalId,
      'status': GrpcMappers.approvalStatusToString(response.status),
    };
  }

  @override
  Future<List<ToolDescriptor>> listTools() async {
    final response = await _sessions.listTools(sessionpb.ListToolsRequest());
    return response.tools.map(GrpcMappers.toolToModel).toList();
  }

  @override
  Future<List<AgentDescriptor>> listAgents() async {
    final response = await _sessions.listAgents(sessionpb.ListAgentsRequest());
    return response.agents.map(GrpcMappers.agentToModel).toList();
  }

  @override
  Future<List<Skill>> listSkills() async {
    final response = await _skills.listSkills(skillpb.ListSkillsRequest());
    return response.skills.map(GrpcMappers.skillToModel).toList();
  }

  @override
  Future<Skill> getSkill({required String skillId}) async {
    final response = await _skills.getSkill(
      skillpb.GetSkillRequest(skillId: skillId),
    );
    return GrpcMappers.skillToModel(response);
  }

  @override
  Future<Skill> setSkillEnabled({
    required String skillId,
    required bool enabled,
  }) async {
    final response = await _skills.setSkillEnabled(
      skillpb.SetSkillEnabledRequest(skillId: skillId, enabled: enabled),
    );
    return GrpcMappers.skillToModel(response);
  }

  @override
  Future<Skill> setSkillCapabilityGrant({
    required String skillId,
    required String capability,
    required bool granted,
  }) async {
    final response = await _skills.setSkillCapabilityGrant(
      skillpb.SetSkillCapabilityGrantRequest(
        skillId: skillId,
        capability: capability,
        granted: granted,
      ),
    );
    return GrpcMappers.skillToModel(response);
  }

  @override
  Future<List<ExternalAgent>> listExternalAgents() async {
    final response = await _externalAgents.listExternalAgents(
      agentpb.ListExternalAgentsRequest(),
    );
    return response.agents.map(GrpcMappers.externalAgentToModel).toList();
  }

  @override
  Future<ExternalAgent> createExternalAgent({
    required String displayName,
    required ExternalAgentProvider provider,
    required String baseUrl,
    required String model,
    required String credentialRef,
  }) async {
    final response = await _externalAgents.createExternalAgent(
      agentpb.CreateExternalAgentRequest(
        displayName: displayName,
        provider: GrpcMappers.externalAgentProviderToProto(provider),
        baseUrl: baseUrl,
        model: model,
        credentialRef: credentialRef,
      ),
    );
    return GrpcMappers.externalAgentToModel(response);
  }

  @override
  Future<ExternalAgent> updateExternalAgent({
    required String agentId,
    required String displayName,
    required ExternalAgentProvider provider,
    required String baseUrl,
    required String model,
    required String credentialRef,
  }) async {
    final response = await _externalAgents.updateExternalAgent(
      agentpb.UpdateExternalAgentRequest(
        agentId: agentId,
        displayName: displayName,
        provider: GrpcMappers.externalAgentProviderToProto(provider),
        baseUrl: baseUrl,
        model: model,
        credentialRef: credentialRef,
      ),
    );
    return GrpcMappers.externalAgentToModel(response);
  }

  @override
  Future<void> deleteExternalAgent({required String agentId}) async {
    await _externalAgents.deleteExternalAgent(
      agentpb.DeleteExternalAgentRequest(agentId: agentId),
    );
  }

  @override
  Future<ExternalAgent?> getSessionAgent({required String sessionId}) async {
    final response = await _externalAgents.getSessionAgent(
      agentpb.GetSessionAgentRequest(sessionId: sessionId),
      options: grpc.CallOptions(timeout: _startupUnaryTimeout),
    );
    return _sessionAgentOrLocal(response);
  }

  @override
  Future<ExternalAgent?> setSessionAgent({
    required String sessionId,
    required String agentId,
  }) async {
    final response = await _externalAgents.setSessionAgent(
      agentpb.SetSessionAgentRequest(sessionId: sessionId, agentId: agentId),
    );
    return _sessionAgentOrLocal(response);
  }

  @override
  Future<ExternalAgent?> clearSessionAgent({required String sessionId}) async {
    final response = await _externalAgents.clearSessionAgent(
      agentpb.ClearSessionAgentRequest(sessionId: sessionId),
    );
    return _sessionAgentOrLocal(response);
  }

  /// An absent agent is not a missing field: it is the local assistant, which
  /// is what every conversation does unless someone routed it elsewhere.
  ExternalAgent? _sessionAgentOrLocal(agentpb.SessionAgentResponse response) {
    if (!response.hasAgent()) return null;
    return GrpcMappers.externalAgentToModel(response.agent);
  }

  @override
  Future<IntegrationCatalogue> listIntegrationProviders() async {
    final response = await _integrations.listProviders(
      integrationpb.ListProvidersRequest(),
    );
    return GrpcMappers.catalogueToModel(response);
  }

  @override
  Future<List<IntegrationConnection>> listConnections() async {
    final response = await _integrations.listConnections(
      integrationpb.ListConnectionsRequest(),
    );
    return response.connections.map(GrpcMappers.connectionToModel).toList();
  }

  @override
  Future<IntegrationConnection> connectAccount({
    required IntegrationProviderKind provider,
    required String displayName,
    required String credential,
    required bool consentAcknowledged,
    String accountLabel = '',
    String endpoint = '',
  }) async {
    // The only request in this client that carries a secret, and it only goes
    // one way. Nothing here logs the request.
    final response = await _integrations.connectAccount(
      integrationpb.ConnectAccountRequest(
        provider: GrpcMappers.integrationProviderFromModel(provider),
        displayName: displayName,
        accountLabel: accountLabel,
        endpoint: endpoint,
        credential: credential,
        consentAcknowledged: consentAcknowledged,
      ),
    );
    return GrpcMappers.connectionToModel(response);
  }

  @override
  Future<IntegrationConnection> revokeConnection({
    required String connectionId,
  }) async {
    final response = await _integrations.revokeConnection(
      integrationpb.RevokeConnectionRequest(connectionId: connectionId),
    );
    return GrpcMappers.connectionToModel(response);
  }

  @override
  Future<void> deleteConnection({required String connectionId}) async {
    await _integrations.deleteConnection(
      integrationpb.DeleteConnectionRequest(connectionId: connectionId),
    );
  }

  @override
  Future<List<Automation>> listAutomations() async {
    final response = await _automations.listAutomations(
      automationpb.ListAutomationsRequest(),
    );
    return response.automations.map(GrpcMappers.automationToModel).toList();
  }

  @override
  Future<Automation> createAutomation({
    required String name,
    required String prompt,
    required AutomationSchedule schedule,
    required bool enabled,
    required List<AutomationTool> allowedTools,
  }) async {
    final response = await _automations.createAutomation(
      automationpb.CreateAutomationRequest(
        name: name,
        prompt: prompt,
        schedule: GrpcMappers.automationScheduleToProto(schedule),
        enabled: enabled,
        allowedTools: allowedTools.map(GrpcMappers.automationToolToProto),
      ),
    );
    return GrpcMappers.automationToModel(response);
  }

  @override
  Future<Automation> updateAutomation({
    required String automationId,
    required String name,
    required String prompt,
    required AutomationSchedule schedule,
    required List<AutomationTool> allowedTools,
  }) async {
    final response = await _automations.updateAutomation(
      automationpb.UpdateAutomationRequest(
        automationId: automationId,
        name: name,
        prompt: prompt,
        schedule: GrpcMappers.automationScheduleToProto(schedule),
        allowedTools: allowedTools.map(GrpcMappers.automationToolToProto),
      ),
    );
    return GrpcMappers.automationToModel(response);
  }

  @override
  Future<Automation> setAutomationEnabled({
    required String automationId,
    required bool enabled,
  }) async {
    final response = await _automations.setAutomationEnabled(
      automationpb.SetAutomationEnabledRequest(
        automationId: automationId,
        enabled: enabled,
      ),
    );
    return GrpcMappers.automationToModel(response);
  }

  @override
  Future<void> deleteAutomation({required String automationId}) async {
    await _automations.deleteAutomation(
      automationpb.DeleteAutomationRequest(automationId: automationId),
    );
  }

  @override
  Future<TelemetrySummary> getTelemetrySummary({
    required int windowDays,
  }) async {
    final response = await _telemetry.getTelemetrySummary(
      telemetrypb.GetTelemetrySummaryRequest(windowDays: windowDays),
    );
    return GrpcMappers.telemetrySummaryToModel(response);
  }

  @override
  Future<AuditPage> listAuditEntries({
    String? correlationId,
    String? action,
    DateTime? createdAtStart,
    DateTime? createdAtEnd,
    AuditOrder order = AuditOrder.descending,
    int limit = 50,
    String? cursor,
  }) async {
    final response = await _audit.listAuditEntries(
      auditpb.ListAuditEntriesRequest(
        correlationId: correlationId,
        action: action,
        createdAtStart: createdAtStart == null
            ? null
            : timestamppb.Timestamp.fromDateTime(createdAtStart.toUtc()),
        createdAtEnd: createdAtEnd == null
            ? null
            : timestamppb.Timestamp.fromDateTime(createdAtEnd.toUtc()),
        order: order == AuditOrder.ascending
            ? auditpb.AuditOrder.AUDIT_ORDER_ASCENDING
            : auditpb.AuditOrder.AUDIT_ORDER_DESCENDING,
        page: commonpb.PageRequest(limit: limit, cursor: cursor ?? ''),
      ),
      options: grpc.CallOptions(timeout: _startupUnaryTimeout),
    );
    return GrpcMappers.auditPageToModel(response);
  }

  @override
  Future<void> close() async {
    if (_ownsChannel) {
      await _channel.shutdown();
    }
  }

  static grpc.ClientChannel createChannel(String baseUrl) {
    final uri = parseBaseUrl(baseUrl);
    final secure = uri.scheme == 'https';
    return grpc.ClientChannel(
      uri.host,
      port: uri.hasPort ? uri.port : (secure ? 443 : 80),
      options: grpc.ChannelOptions(
        credentials: secure
            ? const grpc.ChannelCredentials.secure()
            : const grpc.ChannelCredentials.insecure(),
      ),
    );
  }

  static Uri parseBaseUrl(String baseUrl) {
    final trimmed = baseUrl.trim().replaceFirst(RegExp(r'/+$'), '');
    final candidate = trimmed.contains('://') ? trimmed : 'http://$trimmed';
    return Uri.parse(candidate);
  }

  static bool _isTerminalChatEvent(chatpb.ChatStreamEvent event) {
    return event.hasRunCompleted() ||
        event.hasRunFailed() ||
        event.hasRunCancelled();
  }
}

grpc.ClientChannel createTuringGrpcChannel(String baseUrl) {
  return TuringGrpcApi.createChannel(baseUrl);
}
