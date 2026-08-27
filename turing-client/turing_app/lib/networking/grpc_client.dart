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
import '../generated/turing/v1/mcp.pb.dart' as mcppb;
import '../generated/turing/v1/mcp.pbgrpc.dart' as mcpgrpc;
import '../generated/turing/v1/memory.pb.dart' as memorypb;
import '../generated/turing/v1/memory.pbgrpc.dart' as memorygrpc;
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
import '../models/memory.dart';
import '../models/message.dart';
import '../models/mcp_server.dart';
import '../models/remote_egress.dart';
import '../models/search_hit.dart';
import '../models/session.dart';
import '../models/session_page.dart';
import '../models/session_deletion.dart';
import '../models/skill.dart';
import '../models/telemetry.dart';
import '../models/tool_descriptor.dart';
import '../models/turing_event.dart';
import '../utils/protobuf_enum.dart';
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

class TuringGrpcApi
    implements ClosableTuringApi, RemoteEgressApi, PseudoServerPolicyApi {
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
    _mcpRegistry = mcpgrpc.McpRegistryServiceClient(_channel, options: options);
    _memory = memorygrpc.MemoryServiceClient(_channel, options: options);
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
  late final mcpgrpc.McpRegistryServiceClient _mcpRegistry;
  late final memorygrpc.MemoryServiceClient _memory;

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
    final page = await listSessionPage(limit: limit, cursor: after);
    return page.sessions;
  }

  @override
  Future<SessionPage> listSessionPage({
    int limit = 50,
    String? cursor,
    SessionListFilter filter = SessionListFilter.active,
  }) async {
    final response = await _sessions.listSessions(
      sessionpb.ListSessionsRequest(
        page: commonpb.PageRequest(limit: limit, cursor: cursor ?? ''),
        filter: switch (filter) {
          SessionListFilter.active =>
            sessionpb.SessionListFilter.SESSION_LIST_FILTER_ACTIVE,
          SessionListFilter.archived =>
            sessionpb.SessionListFilter.SESSION_LIST_FILTER_ARCHIVED,
          SessionListFilter.all =>
            sessionpb.SessionListFilter.SESSION_LIST_FILTER_ALL,
        },
      ),
      options: grpc.CallOptions(timeout: _startupUnaryTimeout),
    );
    return GrpcMappers.sessionPageToModel(response);
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
  Future<Session> renameSession({
    required String sessionId,
    required String title,
  }) async {
    final response = await _sessions.renameSession(
      sessionpb.RenameSessionRequest(sessionId: sessionId, title: title),
      options: grpc.CallOptions(timeout: _startupUnaryTimeout),
    );
    return GrpcMappers.sessionToModel(response.session);
  }

  @override
  Future<Session> archiveSession({required String sessionId}) async {
    final response = await _sessions.archiveSession(
      sessionpb.ArchiveSessionRequest(sessionId: sessionId),
      options: grpc.CallOptions(timeout: _startupUnaryTimeout),
    );
    return GrpcMappers.sessionToModel(response.session);
  }

  @override
  Future<Session> restoreSession({required String sessionId}) async {
    final response = await _sessions.restoreSession(
      sessionpb.RestoreSessionRequest(sessionId: sessionId),
      options: grpc.CallOptions(timeout: _startupUnaryTimeout),
    );
    return GrpcMappers.sessionToModel(response.session);
  }

  @override
  Future<SessionDeletionReceipt> deleteSession({
    required String sessionId,
  }) async {
    final response = await _sessions.deleteSession(
      sessionpb.DeleteSessionRequest(sessionId: sessionId),
      options: grpc.CallOptions(timeout: _startupUnaryTimeout),
    );
    switch (response.deletion.state) {
      case sessionpb.SessionDeletionState.SESSION_DELETION_STATE_COMPLETED:
        return _sessionDeletionReceiptToModel(response.deletion);
      case sessionpb
          .SessionDeletionState
          .SESSION_DELETION_STATE_FAILED_EXTERNAL:
        return _sessionDeletionReceiptToModel(response.deletion);
      case sessionpb.SessionDeletionState.SESSION_DELETION_STATE_IN_PROGRESS:
      case sessionpb.SessionDeletionState.SESSION_DELETION_STATE_UNSPECIFIED:
      default:
        return _sessionDeletionReceiptToModel(response.deletion);
    }
  }

  @override
  Future<List<SessionDeletionReceipt>> listSessionDeletionReceipts() async {
    final response = await _sessions.listSessionDeletionReceipts(
      sessionpb.ListSessionDeletionReceiptsRequest(),
      options: grpc.CallOptions(timeout: _startupUnaryTimeout),
    );
    return response.deletions
        .map(_sessionDeletionReceiptToModel)
        .toList(growable: false);
  }

  SessionDeletionReceipt _sessionDeletionReceiptToModel(
    sessionpb.SessionDeletionReceipt receipt,
  ) {
    final state = switch (receipt.state) {
      sessionpb.SessionDeletionState.SESSION_DELETION_STATE_COMPLETED =>
        SessionDeletionState.completed,
      sessionpb.SessionDeletionState.SESSION_DELETION_STATE_FAILED_EXTERNAL =>
        SessionDeletionState.failedExternal,
      _ => SessionDeletionState.inProgress,
    };
    return SessionDeletionReceipt(
      sessionId: receipt.sessionId,
      state: state,
      retryable: receipt.retryable,
      errorCode: receipt.errorCode.isEmpty ? null : receipt.errorCode,
      lifecycleVersion: receipt.lifecycleVersion.toInt(),
      terminalSequence: receipt.terminalSequence.toInt(),
      runCount: receipt.runCount,
      messageCount: receipt.messageCount,
      retainedLegacyArtifactCount: receipt.retainedLegacyArtifactCount,
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
        responseFormat: sessionpb
            .SearchMessagesResponseFormat
            .SEARCH_MESSAGES_RESPONSE_FORMAT_HITS,
      ),
      options: grpc.CallOptions(timeout: _startupUnaryTimeout),
    );
    if (response.hits.isNotEmpty) {
      return response.hits
          .map(GrpcMappers.searchHitToModel)
          .toList(growable: false);
    }
    return response.messages
        .map(GrpcMappers.legacySearchHitToModel)
        .toList(growable: false);
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
      remoteMcpServers: disclosure.remoteMcpServers
          .map(
            (server) => RemoteMcpDestination(
              serverName: server.serverName,
              endpoint: server.endpoint,
              endpointHost: server.endpointHost,
            ),
          )
          .toList(growable: false),
      integrationEndpoints: disclosure.integrationEndpoints
          .map(
            (entry) => IntegrationEgressDestination(
              endpoint: entry.endpoint,
              endpointHost: entry.endpointHost,
              connectionId: entry.connectionId,
              displayName: entry.displayName,
              tools: List.unmodifiable(entry.tools),
            ),
          )
          .toList(growable: false),
      skills: disclosure.skills
          .map(
            (skill) => SkillEgressDisclosure(
              skillId: skill.skillId,
              displayName: skill.displayName,
              bodyMayBeSent: skill.bodyMayBeSent,
            ),
          )
          .toList(growable: false),
      selectedTools: List.unmodifiable(disclosure.selectedTools),
      memoryNotes: disclosure.memoryNotes
          .map(
            (note) => MemoryEgressDisclosure(
              noteId: note.noteId,
              title: note.title,
              vaultPath: note.vaultPath,
              tier: _memoryEgressTierFromProto(note),
              bodyMayBeSent: note.bodyMayBeSent,
            ),
          )
          .toList(growable: false),
      memoryProfileMayBeSent: disclosure.memoryProfileMayBeSent,
      expiresAt: disclosure.expiresAt.toDateTime().toUtc(),
    );
  }

  /// A tier this build does not recognise is reported as unspecified rather
  /// than guessed at. The dialog says "memory" for it, which is true, instead
  /// of naming a tier the server never claimed.
  ///
  /// Decoded from the whole message, not from the field alone. A closed enum
  /// keeps the last value the parser *recognised*, so a newer tier arriving
  /// after a known one leaves the known one sitting in the field while the
  /// value the server actually meant is filed away as unknown. Reading the
  /// field would put "Persona — already in this prompt" on a consent dialog
  /// over a row the server never described that way, which is the one sentence
  /// this screen must never invent.
  static MemoryEgressTier _memoryEgressTierFromProto(
    commonpb.MemoryEgressDisclosure note,
  ) {
    final tier = decodeClosedEnum(
      message: note,
      fieldNumber: 4,
      readValue: () => note.tier,
      unknownValue: commonpb.MemoryTier.MEMORY_TIER_UNSPECIFIED,
    );
    switch (GrpcMappers.memoryTierToModel(tier)) {
      case MemoryTier.persona:
        return MemoryEgressTier.persona;
      case MemoryTier.profile:
        return MemoryEgressTier.profile;
      case MemoryTier.belief:
        return MemoryEgressTier.belief;
      case MemoryTier.note:
        return MemoryEgressTier.note;
      case MemoryTier.unspecified:
        return MemoryEgressTier.unspecified;
    }
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
  Future<McpRegistrySnapshot> listMcpServers() async {
    final response = await _mcpRegistry.listMcpServers(
      mcppb.ListMcpServersRequest(),
    );
    return McpRegistrySnapshot(
      servers: response.servers.map(_mcpServerToModel).toList(),
      unsupported: response.unsupported
          .map(
            (entry) =>
                UnsupportedMcpServer(name: entry.name, reason: entry.reason),
          )
          .toList(),
      registryDegraded: response.registryDegraded,
      registryDegradationReason: response.registryDegradationReason,
    );
  }

  @override
  Future<McpServer> setMcpServerEnabled({
    required String serverId,
    required bool enabled,
  }) async {
    final response = await _mcpRegistry.setMcpServerEnabled(
      mcppb.SetMcpServerEnabledRequest(serverId: serverId, enabled: enabled),
    );
    return _mcpServerToModel(response);
  }

  @override
  Future<ToolDescriptor> updateMcpToolPolicy({
    required String serverId,
    required String toolName,
    required ToolPolicy policy,
  }) async {
    final response = await _mcpRegistry.updateMcpToolPolicy(
      mcppb.UpdateMcpToolPolicyRequest(
        serverId: serverId,
        toolName: toolName,
        policy: switch (policy) {
          ToolPolicy.safe => commonpb.ToolPolicy.TOOL_POLICY_SAFE,
          ToolPolicy.approvalRequired =>
            commonpb.ToolPolicy.TOOL_POLICY_APPROVAL_REQUIRED,
          ToolPolicy.disabled => commonpb.ToolPolicy.TOOL_POLICY_DISABLED,
          ToolPolicy.unspecified => commonpb.ToolPolicy.TOOL_POLICY_UNSPECIFIED,
        },
      ),
    );
    return ToolDescriptor(
      serverName: '',
      toolName: response.toolName,
      policy: GrpcMappers.toolPolicyToModel(response.policy),
    );
  }

  @override
  Future<List<ToolDescriptor>> listPseudoServerTools({
    required String serverName,
  }) async {
    final response = await _mcpRegistry.listPseudoServerTools(
      mcppb.ListPseudoServerToolsRequest(serverName: serverName),
    );
    return response.tools
        .map(
          (tool) => ToolDescriptor(
            serverName: serverName,
            toolName: tool.toolName,
            policy: GrpcMappers.toolPolicyToModel(tool.policy),
            enabled: tool.enabled,
            present: tool.present,
          ),
        )
        .toList(growable: false);
  }

  @override
  Future<ToolDescriptor> updateToolPolicyByName({
    required String serverName,
    required String toolName,
    required ToolPolicy policy,
  }) async {
    final response = await _mcpRegistry.updateToolPolicyByName(
      mcppb.UpdateToolPolicyByNameRequest(
        serverName: serverName,
        toolName: toolName,
        policy: switch (policy) {
          ToolPolicy.safe => commonpb.ToolPolicy.TOOL_POLICY_SAFE,
          ToolPolicy.approvalRequired =>
            commonpb.ToolPolicy.TOOL_POLICY_APPROVAL_REQUIRED,
          ToolPolicy.disabled => commonpb.ToolPolicy.TOOL_POLICY_DISABLED,
          ToolPolicy.unspecified => commonpb.ToolPolicy.TOOL_POLICY_UNSPECIFIED,
        },
      ),
    );
    return ToolDescriptor(
      serverName: serverName,
      toolName: response.toolName,
      policy: GrpcMappers.toolPolicyToModel(response.policy),
      enabled: response.enabled,
      present: response.present,
    );
  }

  @override
  Future<void> deleteMcpServer({required String serverId}) async {
    await _mcpRegistry.deleteMcpServer(
      mcppb.DeleteMcpServerRequest(serverId: serverId),
    );
  }

  @override
  Future<McpServer> registerMcpServer({
    required String name,
    required String url,
    required McpServerTier tier,
    String bearerToken = '',
  }) async {
    final protoTier = switch (tier) {
      McpServerTier.localContainer =>
        mcppb.McpServerTier.MCP_SERVER_TIER_LOCAL_CONTAINER,
      McpServerTier.remoteUrl => mcppb.McpServerTier.MCP_SERVER_TIER_REMOTE_URL,
      McpServerTier.bundled => throw const TuringApiException(
        code: 'mcp_server_tier_unsupported',
        message:
            'Only local-container and remote-url servers can be registered '
            'from this client',
      ),
      McpServerTier.unspecified => throw const TuringApiException(
        code: 'mcp_server_tier_unspecified',
        message: 'A tier must be chosen to register an MCP server',
      ),
    };
    final response = await _mcpRegistry.registerMcpServer(
      mcppb.RegisterMcpServerRequest(
        name: name,
        url: url,
        tier: protoTier,
        bearerToken: bearerToken,
      ),
    );
    return _mcpServerToModel(response);
  }

  @override
  Future<McpImportReport> reimportMcpJson() async {
    final response = await _mcpRegistry.reimportMcpJson(
      mcppb.ReimportMcpJsonRequest(),
    );
    return McpImportReport(
      imported: response.imported,
      skipped: response.skipped,
      refused: response.unsupported
          .map(
            (entry) =>
                UnsupportedMcpServer(name: entry.name, reason: entry.reason),
          )
          .toList(),
    );
  }

  @override
  Future<McpServer> rotateMcpServerToken({
    required String serverId,
    required String bearerToken,
  }) async {
    final response = await _mcpRegistry.rotateMcpServerToken(
      mcppb.RotateMcpServerTokenRequest(
        serverId: serverId,
        bearerToken: bearerToken,
      ),
    );
    return _mcpServerToModel(response);
  }

  McpServer _mcpServerToModel(mcppb.McpServerDescriptor server) {
    return McpServer(
      serverId: server.serverId,
      name: server.name,
      transport: server.transport,
      url: server.url,
      tier: switch (server.tier) {
        mcppb.McpServerTier.MCP_SERVER_TIER_BUNDLED => McpServerTier.bundled,
        mcppb.McpServerTier.MCP_SERVER_TIER_LOCAL_CONTAINER =>
          McpServerTier.localContainer,
        mcppb.McpServerTier.MCP_SERVER_TIER_REMOTE_URL =>
          McpServerTier.remoteUrl,
        _ => McpServerTier.unspecified,
      },
      enabled: server.enabled,
      liveness: switch (server.liveness) {
        mcppb.McpServerLiveness.MCP_SERVER_LIVENESS_UNKNOWN =>
          McpServerLiveness.unknown,
        mcppb.McpServerLiveness.MCP_SERVER_LIVENESS_UP => McpServerLiveness.up,
        mcppb.McpServerLiveness.MCP_SERVER_LIVENESS_DOWN =>
          McpServerLiveness.down,
        _ => McpServerLiveness.unspecified,
      },
      statusMessage: server.statusMessage,
      sandboxConfined: server.sandboxConfined,
      tools: server.tools
          .map(
            (tool) => ToolDescriptor(
              serverName: server.name,
              toolName: tool.toolName,
              policy: GrpcMappers.toolPolicyToModel(tool.policy),
              enabled: tool.enabled,
              present: tool.present,
            ),
          )
          .toList(),
    );
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

  // -------------------------------------------------------------------
  // Memory
  //
  // This client is a reader and a messenger: it holds no vault state, decides
  // nothing locally, and re-reads the whole state after every write. Failures
  // arrive as [TuringApiException] so the page can show the server's own
  // sentence — the compare-and-set refusals in particular are written for the
  // person holding the file open, and are not this client's to paraphrase.
  // -------------------------------------------------------------------

  @override
  Future<MemoryState> listMemoryState() {
    return _memoryCall(() async {
      final response = await _memory.listMemoryState(
        memorypb.ListMemoryStateRequest(),
      );
      return GrpcMappers.memoryStateToModel(response);
    });
  }

  @override
  Future<MemorySettings> setMemoryEnabled({required bool enabled}) {
    return _memoryCall(() async {
      // No tier is named: this client only ever toggles memory as a whole, and
      // the server refuses a per-tier toggle it cannot honour.
      final response = await _memory.setMemoryEnabled(
        memorypb.SetMemoryEnabledRequest(enabled: enabled),
      );
      return GrpcMappers.memorySettingsToModel(response);
    });
  }

  @override
  Future<MemoryCandidate> promoteMemoryCandidate({
    required String candidateId,
    required String expectedCandidateHash,
  }) {
    return _memoryCall(() async {
      final response = await _memory.promoteMemoryCandidate(
        memorypb.PromoteMemoryCandidateRequest(
          candidateId: candidateId,
          expectedCandidateHash: expectedCandidateHash,
        ),
      );
      return GrpcMappers.memoryCandidateToModel(response.candidate);
    });
  }

  @override
  Future<MemoryCandidate> rejectMemoryCandidate({
    required String candidateId,
    String expectedCandidateHash = '',
    String reason = '',
  }) {
    return _memoryCall(() async {
      final response = await _memory.rejectMemoryCandidate(
        memorypb.RejectMemoryCandidateRequest(
          candidateId: candidateId,
          reason: reason,
          expectedCandidateHash: expectedCandidateHash,
        ),
      );
      return GrpcMappers.memoryCandidateToModel(response.candidate);
    });
  }

  @override
  Future<MemoryApplyResult> applyMemoryProfile({
    required String candidateId,
    required String content,
    required String expectedContentHash,
    String expectedCandidateHash = '',
  }) {
    return _memoryCall(() async {
      final response = await _memory.applyMemoryProfile(
        memorypb.ApplyMemoryProfileRequest(
          candidateId: candidateId,
          content: content,
          expectedContentHash: expectedContentHash,
          expectedCandidateHash: expectedCandidateHash,
        ),
      );
      return MemoryApplyResult(
        profile: GrpcMappers.memoryProfileToModel(response.profile),
        cleanupPending: response.cleanupPending,
      );
    });
  }

  @override
  Future<MemoryDocument> saveMemoryPersona({
    required String content,
    required String expectedContentHash,
  }) {
    return _memoryCall(() async {
      final response = await _memory.saveMemoryPersona(
        memorypb.SaveMemoryPersonaRequest(
          content: content,
          expectedContentHash: expectedContentHash,
        ),
      );
      return GrpcMappers.memoryPersonaToModel(response.persona);
    });
  }

  @override
  Future<MemoryDocument> saveMemoryProfile({
    required String content,
    required String expectedContentHash,
  }) {
    return _memoryCall(() async {
      final response = await _memory.saveMemoryProfile(
        memorypb.SaveMemoryProfileRequest(
          content: content,
          expectedContentHash: expectedContentHash,
        ),
      );
      return GrpcMappers.memoryProfileToModel(response.profile);
    });
  }

  /// Turns a transport failure into something the page can render without
  /// knowing what gRPC is, keeping the server's message verbatim.
  static Future<T> _memoryCall<T>(Future<T> Function() request) async {
    try {
      return await request();
    } on grpc.GrpcError catch (error) {
      throw TuringApiException(
        code: _memoryErrorCode(error.code),
        message: error.message ?? 'the memory request failed',
      );
    }
  }

  static String _memoryErrorCode(int code) {
    switch (code) {
      case grpc.StatusCode.invalidArgument:
        return 'invalid_argument';
      case grpc.StatusCode.notFound:
        return 'not_found';
      case grpc.StatusCode.permissionDenied:
        return 'permission_denied';
      case grpc.StatusCode.failedPrecondition:
        return 'failed_precondition';
      case grpc.StatusCode.aborted:
        return 'aborted';
      case grpc.StatusCode.unimplemented:
        return 'unimplemented';
      case grpc.StatusCode.unavailable:
        return 'unavailable';
      default:
        return 'memory_error';
    }
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
