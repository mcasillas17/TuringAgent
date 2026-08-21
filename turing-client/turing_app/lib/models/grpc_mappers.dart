import 'package:fixnum/fixnum.dart';

import '../generated/google/protobuf/struct.pb.dart' as structpb;
import '../generated/google/protobuf/timestamp.pb.dart' as timestamppb;

import '../generated/turing/v1/agents.pb.dart' as agentpb;
import '../generated/turing/v1/approvals.pb.dart' as approvalpb;
import '../generated/turing/v1/audit.pb.dart' as auditpb;
import '../generated/turing/v1/automations.pb.dart' as automationpb;
import '../generated/turing/v1/chat.pb.dart' as chatpb;
import '../generated/turing/v1/common.pb.dart' as commonpb;
import '../generated/turing/v1/events.pb.dart' as eventpb;
import '../generated/turing/v1/integrations.pb.dart' as integrationpb;
import '../generated/turing/v1/sessions.pb.dart' as sessionpb;
import '../generated/turing/v1/skills.pb.dart' as skillpb;
import '../generated/turing/v1/telemetry.pb.dart' as telemetrypb;
import '../utils/protobuf_enum.dart';
import 'agent_descriptor.dart' as model_agent;
import 'audit.dart' as model_audit;
import 'external_agent.dart' as model_external_agent;
import 'integration.dart' as model_integration;
import 'automation.dart' as model_automation;
import 'message.dart' as model_message;
import 'run_lifecycle.dart' as model_run_lifecycle;
import 'run_state.dart' as model_run_state;
import 'search_hit.dart' as model_search_hit;
import 'session.dart' as model_session;
import 'skill.dart' as model_skill;
import 'telemetry.dart' as model_telemetry;
import 'tool_descriptor.dart' as model_tool;
import 'turing_event.dart' as model_event;

class GrpcMappers {
  static model_external_agent.ExternalAgent externalAgentToModel(
    agentpb.ExternalAgent agent,
  ) {
    return model_external_agent.ExternalAgent(
      agentId: agent.agentId,
      displayName: agent.displayName,
      provider: externalAgentProviderToModel(agent.provider),
      baseUrl: agent.baseUrl,
      model: agent.model,
      credentialRef: agent.credentialRef,
      credentialAvailable: agent.credentialAvailable,
    );
  }

  static model_external_agent.ExternalAgentProvider
  externalAgentProviderToModel(agentpb.ExternalAgentProvider provider) {
    switch (provider) {
      case agentpb.ExternalAgentProvider.EXTERNAL_AGENT_PROVIDER_ANTHROPIC:
        return model_external_agent.ExternalAgentProvider.anthropic;
      case agentpb.ExternalAgentProvider.EXTERNAL_AGENT_PROVIDER_OPENAI:
        return model_external_agent.ExternalAgentProvider.openai;
      case agentpb.ExternalAgentProvider.EXTERNAL_AGENT_PROVIDER_GOOGLE:
        return model_external_agent.ExternalAgentProvider.google;
      case agentpb.ExternalAgentProvider.EXTERNAL_AGENT_PROVIDER_XAI:
        return model_external_agent.ExternalAgentProvider.xai;
      case agentpb.ExternalAgentProvider.EXTERNAL_AGENT_PROVIDER_OTHER:
        return model_external_agent.ExternalAgentProvider.other;
      default:
        // Includes UNSPECIFIED and anything added to the proto after this
        // build. Reported as unknown rather than guessed at: this label names
        // the company that receives the conversation.
        return model_external_agent.ExternalAgentProvider.unknown;
    }
  }

  static agentpb.ExternalAgentProvider externalAgentProviderToProto(
    model_external_agent.ExternalAgentProvider provider,
  ) {
    switch (provider) {
      case model_external_agent.ExternalAgentProvider.anthropic:
        return agentpb.ExternalAgentProvider.EXTERNAL_AGENT_PROVIDER_ANTHROPIC;
      case model_external_agent.ExternalAgentProvider.openai:
        return agentpb.ExternalAgentProvider.EXTERNAL_AGENT_PROVIDER_OPENAI;
      case model_external_agent.ExternalAgentProvider.google:
        return agentpb.ExternalAgentProvider.EXTERNAL_AGENT_PROVIDER_GOOGLE;
      case model_external_agent.ExternalAgentProvider.xai:
        return agentpb.ExternalAgentProvider.EXTERNAL_AGENT_PROVIDER_XAI;
      case model_external_agent.ExternalAgentProvider.other:
        return agentpb.ExternalAgentProvider.EXTERNAL_AGENT_PROVIDER_OTHER;
      case model_external_agent.ExternalAgentProvider.unknown:
        // Sent back as unspecified, which the backend rejects. Silently
        // rewriting it to a real vendor would save an unrelated edit by
        // changing where the conversation goes.
        return agentpb
            .ExternalAgentProvider
            .EXTERNAL_AGENT_PROVIDER_UNSPECIFIED;
    }
  }

  static model_automation.Automation automationToModel(
    automationpb.Automation automation,
  ) {
    return model_automation.Automation(
      automationId: automation.automationId,
      name: automation.name,
      prompt: automation.prompt,
      schedule: automationScheduleToModel(automation.schedule),
      enabled: automation.enabled,
      allowedTools: automation.allowedTools
          .map(
            (tool) => model_automation.AutomationTool(
              serverName: tool.serverName,
              toolName: tool.toolName,
            ),
          )
          .toList(growable: false),
      lastRunAt: automation.hasLastRunAt()
          ? automation.lastRunAt.toDateTime().toLocal()
          : null,
      // Absent rather than epoch-zero: a disabled automation has no next run,
      // and 1970 would render as a date that means something.
      nextRunAt: automation.hasNextRunAt()
          ? automation.nextRunAt.toDateTime().toLocal()
          : null,
      sessionId: automation.sessionId,
      lastRunId: automation.lastRunId,
      lastRunStatus: automation.lastRunStatus,
      lastRunError: automation.lastRunError,
    );
  }

  static model_automation.AutomationSchedule automationScheduleToModel(
    automationpb.AutomationSchedule schedule,
  ) {
    switch (schedule.kind) {
      case automationpb.AutomationScheduleKind.AUTOMATION_SCHEDULE_KIND_DAILY:
        return model_automation.AutomationSchedule.daily(
          schedule.dailyMinuteUtc,
        );
      default:
        // Includes INTERVAL, UNSPECIFIED, and anything added to the proto
        // after this build. An unknown kind reads as an interval of the
        // minutes it carries, which is inert rather than wrong: the editor
        // shows what the server sent and the user can correct it.
        return model_automation.AutomationSchedule.interval(
          schedule.intervalMinutes,
        );
    }
  }

  static automationpb.AutomationSchedule automationScheduleToProto(
    model_automation.AutomationSchedule schedule,
  ) {
    switch (schedule.kind) {
      case model_automation.AutomationScheduleKind.daily:
        return automationpb.AutomationSchedule(
          kind: automationpb
              .AutomationScheduleKind
              .AUTOMATION_SCHEDULE_KIND_DAILY,
          dailyMinuteUtc: schedule.dailyMinuteUtc,
        );
      case model_automation.AutomationScheduleKind.interval:
        return automationpb.AutomationSchedule(
          kind: automationpb
              .AutomationScheduleKind
              .AUTOMATION_SCHEDULE_KIND_INTERVAL,
          intervalMinutes: schedule.intervalMinutes,
        );
    }
  }

  static automationpb.AutomationTool automationToolToProto(
    model_automation.AutomationTool tool,
  ) {
    return automationpb.AutomationTool(
      serverName: tool.serverName,
      toolName: tool.toolName,
    );
  }

  static model_skill.Skill skillToModel(skillpb.Skill skill) {
    return model_skill.Skill(
      skillId: skill.skillId,
      name: skill.name,
      description: skill.description,
      body: skill.body,
      category: skill.category,
      version: skill.version,
      author: skill.author,
      license: skill.license,
      requires: List.unmodifiable(skill.requires),
      grantedCapabilities: List.unmodifiable(skill.grantedCapabilities),
      missingCapabilities: List.unmodifiable(skill.missingCapabilities),
      enabled: skill.enabled,
      parseError: skill.parseError,
      folderPath: skill.folderPath,
    );
  }

  static model_integration.IntegrationConnection connectionToModel(
    integrationpb.Connection connection,
  ) {
    return model_integration.IntegrationConnection(
      connectionId: connection.connectionId,
      provider: integrationProviderToModel(connection.provider),
      displayName: connection.displayName,
      accountLabel: connection.accountLabel,
      endpoint: connection.endpoint,
      credentialHint: connection.credentialHint,
      state: connectionStateToModel(connection.status),
      grantedScopes: List.unmodifiable(connection.grantedScopes),
      credentialUnreadable: connection.credentialUnreadable,
      connectedAt: connection.hasConnectedAt()
          ? _timestampToDateTime(connection.connectedAt)
          : null,
      revokedAt: connection.hasRevokedAt()
          ? _timestampToDateTime(connection.revokedAt)
          : null,
    );
  }

  static model_integration.IntegrationCatalogue catalogueToModel(
    integrationpb.ListProvidersResponse response,
  ) {
    return model_integration.IntegrationCatalogue(
      providers: response.providers.map(providerToModel).toList(),
      storageConfigured: response.credentialStorageConfigured,
      storageUnconfiguredReason: response.storageUnconfiguredReason,
    );
  }

  static model_integration.IntegrationProviderInfo providerToModel(
    integrationpb.ProviderDescriptor descriptor,
  ) {
    return model_integration.IntegrationProviderInfo(
      kind: integrationProviderToModel(descriptor.provider),
      displayName: descriptor.displayName,
      category: descriptor.category,
      supported: descriptor.supported,
      unsupportedReason: descriptor.unsupportedReason,
      secretLabel: descriptor.secretLabel,
      secretHelp: descriptor.secretHelp,
      accountLabel: descriptor.accountLabel,
      requiresEndpoint: descriptor.requiresEndpoint,
      endpointLabel: descriptor.endpointLabel,
      grants: List.unmodifiable(descriptor.grants),
    );
  }

  static model_integration.IntegrationProviderKind integrationProviderToModel(
    integrationpb.IntegrationProvider provider,
  ) {
    switch (provider) {
      case integrationpb.IntegrationProvider.INTEGRATION_PROVIDER_IMAP:
        return model_integration.IntegrationProviderKind.imap;
      case integrationpb.IntegrationProvider.INTEGRATION_PROVIDER_CALDAV:
        return model_integration.IntegrationProviderKind.caldav;
      case integrationpb.IntegrationProvider.INTEGRATION_PROVIDER_NOTION:
        return model_integration.IntegrationProviderKind.notion;
      case integrationpb.IntegrationProvider.INTEGRATION_PROVIDER_GITHUB:
        return model_integration.IntegrationProviderKind.github;
      case integrationpb
          .IntegrationProvider
          .INTEGRATION_PROVIDER_GOOGLE_WORKSPACE:
        return model_integration.IntegrationProviderKind.googleWorkspace;
      case integrationpb.IntegrationProvider.INTEGRATION_PROVIDER_MICROSOFT_365:
        return model_integration.IntegrationProviderKind.microsoft365;
      case integrationpb.IntegrationProvider.INTEGRATION_PROVIDER_SLACK:
        return model_integration.IntegrationProviderKind.slack;
      default:
        // Includes UNSPECIFIED and anything added to the proto after this
        // build.
        return model_integration.IntegrationProviderKind.unknown;
    }
  }

  static integrationpb.IntegrationProvider integrationProviderFromModel(
    model_integration.IntegrationProviderKind kind,
  ) {
    switch (kind) {
      case model_integration.IntegrationProviderKind.imap:
        return integrationpb.IntegrationProvider.INTEGRATION_PROVIDER_IMAP;
      case model_integration.IntegrationProviderKind.caldav:
        return integrationpb.IntegrationProvider.INTEGRATION_PROVIDER_CALDAV;
      case model_integration.IntegrationProviderKind.notion:
        return integrationpb.IntegrationProvider.INTEGRATION_PROVIDER_NOTION;
      case model_integration.IntegrationProviderKind.github:
        return integrationpb.IntegrationProvider.INTEGRATION_PROVIDER_GITHUB;
      case model_integration.IntegrationProviderKind.googleWorkspace:
        return integrationpb
            .IntegrationProvider
            .INTEGRATION_PROVIDER_GOOGLE_WORKSPACE;
      case model_integration.IntegrationProviderKind.microsoft365:
        return integrationpb
            .IntegrationProvider
            .INTEGRATION_PROVIDER_MICROSOFT_365;
      case model_integration.IntegrationProviderKind.slack:
        return integrationpb.IntegrationProvider.INTEGRATION_PROVIDER_SLACK;
      case model_integration.IntegrationProviderKind.unknown:
        return integrationpb
            .IntegrationProvider
            .INTEGRATION_PROVIDER_UNSPECIFIED;
    }
  }

  static model_integration.IntegrationConnectionState connectionStateToModel(
    integrationpb.ConnectionStatus status,
  ) {
    switch (status) {
      case integrationpb.ConnectionStatus.CONNECTION_STATUS_CONNECTED:
        return model_integration.IntegrationConnectionState.connected;
      case integrationpb.ConnectionStatus.CONNECTION_STATUS_REVOKED:
        return model_integration.IntegrationConnectionState.revoked;
      default:
        // Never "connected": saying an account still has access when this
        // build cannot tell would misstate what someone has given away.
        return model_integration.IntegrationConnectionState.unknown;
    }
  }

  static model_tool.ToolDescriptor toolToModel(sessionpb.ToolDescriptor tool) {
    return model_tool.ToolDescriptor(
      serverName: tool.serverName,
      toolName: tool.toolName,
      policy: toolPolicyToModel(tool.policy),
    );
  }

  static model_tool.ToolPolicy toolPolicyToModel(commonpb.ToolPolicy policy) {
    switch (policy) {
      case commonpb.ToolPolicy.TOOL_POLICY_SAFE:
        return model_tool.ToolPolicy.safe;
      case commonpb.ToolPolicy.TOOL_POLICY_APPROVAL_REQUIRED:
        return model_tool.ToolPolicy.approvalRequired;
      case commonpb.ToolPolicy.TOOL_POLICY_DISABLED:
        return model_tool.ToolPolicy.disabled;
      default:
        // Includes UNSPECIFIED and any value added to the proto after this
        // build. Reported as unknown rather than defaulted to safe, because
        // guessing "safe" for something the backend gates would misdescribe
        // what the agent can do without asking.
        return model_tool.ToolPolicy.unspecified;
    }
  }

  static model_agent.AgentDescriptor agentToModel(
    commonpb.AgentDescriptor agent,
  ) {
    return model_agent.AgentDescriptor(
      id: agent.id.name,
      displayName: agent.displayName,
    );
  }

  static model_session.Session sessionToModel(sessionpb.Session session) {
    return model_session.Session(
      sessionId: session.sessionId,
      title: session.title.isEmpty ? null : session.title,
      updatedAt: _timestampToDateTime(session.updatedAt),
      updatedAtNanoseconds:
          session.updatedAt.seconds.toInt() * 1000000000 +
          session.updatedAt.nanos,
    );
  }

  static model_message.Message messageToModel(commonpb.Message message) {
    return model_message.Message(
      messageId: message.messageId,
      runId: message.runId.isEmpty ? null : message.runId,
      runState: message.hasRunState()
          ? runStateToModel(message.runState)
          : null,
      role: messageRoleToString(message.role),
      content: message.content,
      sequence: message.sequence.toInt(),
      createdAt: _timestampToDateTime(message.createdAt),
    );
  }

  static model_search_hit.SearchHit searchHitToModel(commonpb.Message message) {
    return model_search_hit.SearchHit(
      sessionId: message.sessionId,
      message: messageToModel(message),
    );
  }

  static model_event.TuringEvent turingEventToTuringEvent(
    eventpb.TuringEvent event,
  ) {
    return model_event.TuringEvent(
      eventId: event.eventId,
      sessionId: event.sessionId,
      runId: event.runId.isEmpty ? null : event.runId,
      traceId: event.traceId,
      sequence: event.sequence.toInt(),
      type: eventTypeToString(event.type),
      createdAt: _timestampToDateTime(event.createdAt),
      payload: structToMap(event.payload),
      runState: event.hasRunState() ? runStateToModel(event.runState) : null,
    );
  }

  static model_event.TuringEvent chatStreamEventToTuringEvent(
    chatpb.ChatStreamEvent event,
  ) {
    if (event.hasPersistedEvent()) {
      return turingEventToTuringEvent(event.persistedEvent);
    }

    final type = _chatStreamEventType(event);
    return model_event.TuringEvent(
      eventId: 'stream:${event.runId}:${event.sequence}',
      sessionId: event.sessionId,
      runId: event.runId.isEmpty ? null : event.runId,
      traceId: event.traceId,
      sequence: event.sequence.toInt(),
      type: type,
      createdAt: DateTime.now().toUtc(),
      payload: _chatStreamPayload(event),
      runState: _chatStreamRunState(event),
    );
  }

  static model_run_state.RunState? runStateToModel(commonpb.RunState runState) {
    final version = runState.stateVersion.toInt();
    if (runState.runId.isEmpty ||
        version < 1 ||
        Int64(version) != runState.stateVersion) {
      return null;
    }
    return model_run_state.RunState(
      runId: runState.runId,
      userMessageId: runState.userMessageId,
      assistantMessageId: runState.assistantMessageId,
      lifecycle: runLifecycleToModel(
        decodeClosedEnum(
          message: runState,
          fieldNumber: 4,
          readValue: () => runState.lifecycle,
          unknownValue: commonpb.RunLifecycle.RUN_LIFECYCLE_UNKNOWN,
        ),
      ),
      outcomeReason: runOutcomeReasonToModel(
        decodeClosedEnum(
          message: runState,
          fieldNumber: 5,
          readValue: () => runState.outcomeReason,
          unknownValue: commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_UNKNOWN,
        ),
      ),
      stateVersion: version,
      stateUpdatedAt: _timestampToDateTime(runState.stateUpdatedAt),
      finishedAt: runState.hasFinishedAt()
          ? _timestampToDateTime(runState.finishedAt)
          : null,
      hasDisplayableContent: runState.hasDisplayableContent,
    );
  }

  static model_run_lifecycle.RunLifecycle runLifecycleToModel(
    commonpb.RunLifecycle lifecycle,
  ) {
    switch (lifecycle) {
      case commonpb.RunLifecycle.RUN_LIFECYCLE_QUEUED:
        return model_run_lifecycle.RunLifecycle.queued;
      case commonpb.RunLifecycle.RUN_LIFECYCLE_RUNNING:
        return model_run_lifecycle.RunLifecycle.running;
      case commonpb.RunLifecycle.RUN_LIFECYCLE_WAITING_APPROVAL:
        return model_run_lifecycle.RunLifecycle.waitingApproval;
      case commonpb.RunLifecycle.RUN_LIFECYCLE_RECOVERING:
        return model_run_lifecycle.RunLifecycle.recovering;
      case commonpb.RunLifecycle.RUN_LIFECYCLE_COMPLETED:
        return model_run_lifecycle.RunLifecycle.completed;
      case commonpb.RunLifecycle.RUN_LIFECYCLE_FAILED:
        return model_run_lifecycle.RunLifecycle.failed;
      case commonpb.RunLifecycle.RUN_LIFECYCLE_CANCELLED:
        return model_run_lifecycle.RunLifecycle.cancelled;
      case commonpb.RunLifecycle.RUN_LIFECYCLE_UNSPECIFIED:
      case commonpb.RunLifecycle.RUN_LIFECYCLE_UNKNOWN:
      default:
        return model_run_lifecycle.RunLifecycle.unknown;
    }
  }

  static model_run_state.RunOutcomeReason runOutcomeReasonToModel(
    commonpb.RunOutcomeReason reason,
  ) {
    switch (reason) {
      case commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_NONE:
        return model_run_state.RunOutcomeReason.none;
      case commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_COMPLETED_NO_CONTENT:
        return model_run_state.RunOutcomeReason.completedNoContent;
      case commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_USER_CANCELLED:
        return model_run_state.RunOutcomeReason.userCancelled;
      case commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_ABANDONED:
        return model_run_state.RunOutcomeReason.abandoned;
      case commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_EXPIRED:
        return model_run_state.RunOutcomeReason.expired;
      case commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_CONTEXT_LIMIT:
        return model_run_state.RunOutcomeReason.contextLimit;
      case commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_PROVIDER_FAILURE:
        return model_run_state.RunOutcomeReason.providerFailure;
      case commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_TOOL_FAILURE:
        return model_run_state.RunOutcomeReason.toolFailure;
      case commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_POLICY_DENIED:
        return model_run_state.RunOutcomeReason.policyDenied;
      case commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_RETRIES_EXHAUSTED:
        return model_run_state.RunOutcomeReason.retriesExhausted;
      case commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_RECOVERY_INTERRUPTED:
        return model_run_state.RunOutcomeReason.recoveryInterrupted;
      case commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_SIDE_EFFECT_UNCERTAIN:
        return model_run_state.RunOutcomeReason.sideEffectUncertain;
      case commonpb
          .RunOutcomeReason
          .RUN_OUTCOME_REASON_APPROVAL_DELIVERY_FAILED:
        return model_run_state.RunOutcomeReason.approvalDeliveryFailed;
      case commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_INTERNAL_FAILURE:
        return model_run_state.RunOutcomeReason.internalFailure;
      case commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_LEGACY_UNKNOWN:
        return model_run_state.RunOutcomeReason.legacyUnknown;
      case commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_UNSPECIFIED:
      case commonpb.RunOutcomeReason.RUN_OUTCOME_REASON_UNKNOWN:
      default:
        return model_run_state.RunOutcomeReason.unknown;
    }
  }

  static String modelProviderToString(commonpb.ModelProvider provider) {
    switch (provider) {
      case commonpb.ModelProvider.MODEL_PROVIDER_OPENAI_COMPATIBLE:
        return 'openai_compatible';
      case commonpb.ModelProvider.MODEL_PROVIDER_OLLAMA:
      case commonpb.ModelProvider.MODEL_PROVIDER_UNSPECIFIED:
      default:
        return 'ollama';
    }
  }

  static commonpb.ModelProvider modelProviderFromString(String provider) {
    switch (provider) {
      case 'openai_compatible':
        return commonpb.ModelProvider.MODEL_PROVIDER_OPENAI_COMPATIBLE;
      case 'ollama':
      default:
        return commonpb.ModelProvider.MODEL_PROVIDER_OLLAMA;
    }
  }

  static String messageRoleToString(commonpb.MessageRole role) {
    switch (role) {
      case commonpb.MessageRole.MESSAGE_ROLE_SYSTEM:
        return 'system';
      case commonpb.MessageRole.MESSAGE_ROLE_USER:
        return 'user';
      case commonpb.MessageRole.MESSAGE_ROLE_ASSISTANT:
        return 'assistant';
      case commonpb.MessageRole.MESSAGE_ROLE_TOOL:
        return 'tool';
      case commonpb.MessageRole.MESSAGE_ROLE_UNSPECIFIED:
      default:
        return 'assistant';
    }
  }

  static String eventTypeToString(eventpb.TuringEventType type) {
    switch (type) {
      case eventpb.TuringEventType.TURING_EVENT_TYPE_MESSAGE_STARTED:
        return 'message.started';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_MESSAGE_DELTA:
        return 'message.delta';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_MESSAGE_COMPLETED:
        return 'message.completed';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_AGENT_RUN_QUEUED:
        return 'agent.run.queued';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_AGENT_RUN_STARTED:
        return 'agent.run.started';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_AGENT_RUN_STEP:
        return 'agent.run.step';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_AGENT_RUN_COMPLETED:
        return 'agent.run.completed';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_AGENT_RUN_FAILED:
        return 'agent.run.failed';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_AGENT_RUN_CANCELLED:
        return 'agent.run.cancelled';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_TOOL_CALL_STARTED:
        return 'tool.call.started';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_TOOL_CALL_COMPLETED:
        return 'tool.call.completed';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_TOOL_CALL_FAILED:
        return 'tool.call.failed';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_TOOL_CALL_DENIED:
        return 'tool.call.denied';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_APPROVAL_REQUESTED:
        return 'approval.requested';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_APPROVAL_APPROVED:
        return 'approval.approved';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_APPROVAL_DENIED:
        return 'approval.denied';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_APPROVAL_EXPIRED:
        return 'approval.expired';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_APPROVAL_CONSUMED:
        return 'approval.consumed';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_ERROR:
        return 'error';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_SYSTEM:
        return 'system';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_SESSION_UPDATED:
        return 'session.updated';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_AGENT_RUN_STATE_CHANGED:
        return 'agent.run.state_changed';
      case eventpb.TuringEventType.TURING_EVENT_TYPE_UNSPECIFIED:
      default:
        return 'system';
    }
  }

  static String approvalStatusToString(approvalpb.ApprovalStatus status) {
    switch (status) {
      case approvalpb.ApprovalStatus.APPROVAL_STATUS_PENDING:
        return 'pending';
      case approvalpb.ApprovalStatus.APPROVAL_STATUS_APPROVED:
        return 'approved';
      case approvalpb.ApprovalStatus.APPROVAL_STATUS_DENIED:
        return 'denied';
      case approvalpb.ApprovalStatus.APPROVAL_STATUS_EXPIRED:
        return 'expired';
      case approvalpb.ApprovalStatus.APPROVAL_STATUS_CONSUMED:
        return 'consumed';
      case approvalpb.ApprovalStatus.APPROVAL_STATUS_UNSPECIFIED:
      default:
        return 'unspecified';
    }
  }

  static Map<String, dynamic> structToMap(structpb.Struct struct) {
    return struct.fields.map(
      (key, value) => MapEntry(key, _valueToDart(value)),
    );
  }

  static DateTime _timestampToDateTime(timestamppb.Timestamp timestamp) {
    if (timestamp.seconds.toInt() == 0 && timestamp.nanos == 0) {
      return DateTime.fromMillisecondsSinceEpoch(0, isUtc: true);
    }
    return timestamp.toDateTime().toUtc();
  }

  static String _chatStreamEventType(chatpb.ChatStreamEvent event) {
    switch (event.whichEvent()) {
      case chatpb.ChatStreamEvent_Event.runQueued:
        return 'agent.run.queued';
      case chatpb.ChatStreamEvent_Event.runStarted:
        return 'agent.run.started';
      case chatpb.ChatStreamEvent_Event.messageStarted:
        return 'message.started';
      case chatpb.ChatStreamEvent_Event.tokenDelta:
        return 'message.delta';
      case chatpb.ChatStreamEvent_Event.toolCallStarted:
        return 'tool.call.started';
      case chatpb.ChatStreamEvent_Event.toolCallCompleted:
        return 'tool.call.completed';
      case chatpb.ChatStreamEvent_Event.toolCallFailed:
        return 'tool.call.failed';
      case chatpb.ChatStreamEvent_Event.approvalRequested:
        return 'approval.requested';
      case chatpb.ChatStreamEvent_Event.approvalApproved:
        return 'approval.approved';
      case chatpb.ChatStreamEvent_Event.approvalDenied:
        return 'approval.denied';
      case chatpb.ChatStreamEvent_Event.approvalExpired:
        return 'approval.expired';
      case chatpb.ChatStreamEvent_Event.approvalConsumed:
        return 'approval.consumed';
      case chatpb.ChatStreamEvent_Event.messageCompleted:
        return 'message.completed';
      case chatpb.ChatStreamEvent_Event.runCompleted:
        return 'agent.run.completed';
      case chatpb.ChatStreamEvent_Event.runFailed:
        return 'agent.run.failed';
      case chatpb.ChatStreamEvent_Event.runCancelled:
        return 'agent.run.cancelled';
      case chatpb.ChatStreamEvent_Event.persistedEvent:
        return eventTypeToString(event.persistedEvent.type);
      case chatpb.ChatStreamEvent_Event.runStateChanged:
        return 'agent.run.state_changed';
      case chatpb.ChatStreamEvent_Event.notSet:
        return 'system';
    }
  }

  static Map<String, dynamic> _chatStreamPayload(chatpb.ChatStreamEvent event) {
    switch (event.whichEvent()) {
      case chatpb.ChatStreamEvent_Event.runQueued:
        return {
          'runId': event.runQueued.runId,
          'jobId': event.runQueued.jobId,
          'traceId': event.runQueued.traceId,
          'status': 'queued',
        };
      case chatpb.ChatStreamEvent_Event.runStarted:
        return {
          'runId': event.runStarted.runId,
          'jobId': event.runStarted.jobId,
          'attempt': event.runStarted.attempt,
        };
      case chatpb.ChatStreamEvent_Event.messageStarted:
        return {
          'messageId': event.messageStarted.messageId,
          'role': messageRoleToString(event.messageStarted.role),
        };
      case chatpb.ChatStreamEvent_Event.tokenDelta:
        return {
          'messageId': event.tokenDelta.messageId,
          'delta': event.tokenDelta.delta,
        };
      case chatpb.ChatStreamEvent_Event.toolCallStarted:
        return _toolPayload(event.toolCallStarted);
      case chatpb.ChatStreamEvent_Event.toolCallCompleted:
        return _toolPayload(event.toolCallCompleted);
      case chatpb.ChatStreamEvent_Event.toolCallFailed:
        return _toolPayload(event.toolCallFailed);
      case chatpb.ChatStreamEvent_Event.approvalRequested:
        return _approvalPayload(event.approvalRequested);
      case chatpb.ChatStreamEvent_Event.approvalApproved:
        return _approvalPayload(event.approvalApproved);
      case chatpb.ChatStreamEvent_Event.approvalDenied:
        return _approvalPayload(event.approvalDenied);
      case chatpb.ChatStreamEvent_Event.approvalExpired:
        return _approvalPayload(event.approvalExpired);
      case chatpb.ChatStreamEvent_Event.approvalConsumed:
        return _approvalPayload(event.approvalConsumed);
      case chatpb.ChatStreamEvent_Event.messageCompleted:
        return {
          'messageId': event.messageCompleted.messageId,
          'content': event.messageCompleted.content,
        };
      case chatpb.ChatStreamEvent_Event.runCompleted:
        return {
          'runId': event.runCompleted.runId,
          'assistantMessageId': event.runCompleted.assistantMessageId,
        };
      case chatpb.ChatStreamEvent_Event.runFailed:
        return {
          'runId': event.runFailed.runId,
          'code': event.runFailed.code,
          'message': event.runFailed.message,
          // Deprecated on the wire but retained while older servers emit it.
          // ignore: deprecated_member_use_from_same_package
          'retryable': event.runFailed.retryable,
        };
      case chatpb.ChatStreamEvent_Event.runCancelled:
        return {
          'runId': event.runCancelled.runId,
          'reason': event.runCancelled.reason,
        };
      case chatpb.ChatStreamEvent_Event.persistedEvent:
        return structToMap(event.persistedEvent.payload);
      case chatpb.ChatStreamEvent_Event.runStateChanged:
        return const {};
      case chatpb.ChatStreamEvent_Event.notSet:
        return const {};
    }
  }

  static Map<String, dynamic> _toolPayload(chatpb.ToolEvent event) {
    return {
      'toolCallId': event.toolCallId,
      'serverName': event.serverName,
      'toolName': event.toolName,
      ...structToMap(event.payload),
    };
  }

  static Map<String, dynamic> _approvalPayload(chatpb.ApprovalEvent event) {
    return {
      'approvalId': event.approvalId,
      'toolName': event.toolName,
      'argsSummary': event.argsSummary,
    };
  }

  static model_run_state.RunState? _chatStreamRunState(
    chatpb.ChatStreamEvent event,
  ) {
    commonpb.RunState? state;
    switch (event.whichEvent()) {
      case chatpb.ChatStreamEvent_Event.runQueued:
        state = event.runQueued.hasRunState() ? event.runQueued.runState : null;
      case chatpb.ChatStreamEvent_Event.runStarted:
        state = event.runStarted.hasRunState()
            ? event.runStarted.runState
            : null;
      case chatpb.ChatStreamEvent_Event.approvalRequested:
        state = event.approvalRequested.hasRunState()
            ? event.approvalRequested.runState
            : null;
      case chatpb.ChatStreamEvent_Event.approvalApproved:
        state = event.approvalApproved.hasRunState()
            ? event.approvalApproved.runState
            : null;
      case chatpb.ChatStreamEvent_Event.approvalDenied:
        state = event.approvalDenied.hasRunState()
            ? event.approvalDenied.runState
            : null;
      case chatpb.ChatStreamEvent_Event.approvalExpired:
        state = event.approvalExpired.hasRunState()
            ? event.approvalExpired.runState
            : null;
      case chatpb.ChatStreamEvent_Event.approvalConsumed:
        state = event.approvalConsumed.hasRunState()
            ? event.approvalConsumed.runState
            : null;
      case chatpb.ChatStreamEvent_Event.runCompleted:
        state = event.runCompleted.hasRunState()
            ? event.runCompleted.runState
            : null;
      case chatpb.ChatStreamEvent_Event.runFailed:
        state = event.runFailed.hasRunState() ? event.runFailed.runState : null;
      case chatpb.ChatStreamEvent_Event.runCancelled:
        state = event.runCancelled.hasRunState()
            ? event.runCancelled.runState
            : null;
      case chatpb.ChatStreamEvent_Event.runStateChanged:
        state = event.runStateChanged.hasRunState()
            ? event.runStateChanged.runState
            : null;
      case chatpb.ChatStreamEvent_Event.messageStarted:
      case chatpb.ChatStreamEvent_Event.tokenDelta:
      case chatpb.ChatStreamEvent_Event.toolCallStarted:
      case chatpb.ChatStreamEvent_Event.toolCallCompleted:
      case chatpb.ChatStreamEvent_Event.toolCallFailed:
      case chatpb.ChatStreamEvent_Event.messageCompleted:
      case chatpb.ChatStreamEvent_Event.persistedEvent:
      case chatpb.ChatStreamEvent_Event.notSet:
        state = null;
    }
    return state == null ? null : runStateToModel(state);
  }

  /// Telemetry, where the interesting part of the mapping is what does NOT
  /// happen: an unset int64 stays null all the way to the widget instead of
  /// collapsing to protobuf's zero default. `hasX()` is the whole difference
  /// between "no provider reported this" and "this was zero", and the page
  /// draws them differently on purpose.
  static model_telemetry.TelemetrySummary telemetrySummaryToModel(
    telemetrypb.GetTelemetrySummaryResponse response,
  ) {
    return model_telemetry.TelemetrySummary(
      window: model_telemetry.TelemetryWindow(
        days: response.window.days,
        start: response.window.start.toDateTime().toUtc(),
        end: response.window.end.toDateTime().toUtc(),
      ),
      runs: model_telemetry.TelemetryRunTotals(
        total: response.runs.total.toInt(),
        completed: response.runs.completed.toInt(),
        failed: response.runs.failed.toInt(),
        cancelled: response.runs.cancelled.toInt(),
        inFlight: response.runs.inFlight.toInt(),
        averageDurationMs: response.runs.hasAverageDurationMs()
            ? response.runs.averageDurationMs.toInt()
            : null,
      ),
      tokens: model_telemetry.TelemetryTokenTotals(
        inputTokens: response.tokens.hasInputTokens()
            ? response.tokens.inputTokens.toInt()
            : null,
        outputTokens: response.tokens.hasOutputTokens()
            ? response.tokens.outputTokens.toInt()
            : null,
        runsWithUsage: response.tokens.runsWithUsage.toInt(),
        runsWithoutUsage: response.tokens.runsWithoutUsage.toInt(),
      ),
      tools: response.tools
          .map(
            (tool) => model_telemetry.TelemetryToolUsage(
              serverName: tool.serverName,
              toolName: tool.toolName,
              calls: tool.calls.toInt(),
              failed: tool.failed.toInt(),
              denied: tool.denied.toInt(),
              averageDurationMs: tool.hasAverageDurationMs()
                  ? tool.averageDurationMs.toInt()
                  : null,
            ),
          )
          .toList(growable: false),
      models: response.models
          .map(
            (model) => model_telemetry.TelemetryModelUsage(
              provider: model.provider,
              model: model.model,
              runs: model.runs.toInt(),
              inputTokens: model.hasInputTokens()
                  ? model.inputTokens.toInt()
                  : null,
              outputTokens: model.hasOutputTokens()
                  ? model.outputTokens.toInt()
                  : null,
              runsWithoutUsage: model.runsWithoutUsage.toInt(),
            ),
          )
          .toList(growable: false),
      externalAgents: response.externalAgents
          .map(
            (agent) => model_telemetry.TelemetryExternalAgentUsage(
              displayName: agent.displayName,
              endpointHost: agent.endpointHost,
              runs: agent.runs.toInt(),
              inputTokens: agent.hasInputTokens()
                  ? agent.inputTokens.toInt()
                  : null,
              outputTokens: agent.hasOutputTokens()
                  ? agent.outputTokens.toInt()
                  : null,
              runsWithoutUsage: agent.runsWithoutUsage.toInt(),
            ),
          )
          .toList(growable: false),
      automations: model_telemetry.TelemetryAutomationTotals(
        runs: response.automations.runs.toInt(),
        completed: response.automations.completed.toInt(),
        failed: response.automations.failed.toInt(),
        unattendedApprovals: response.automations.unattendedApprovals.toInt(),
      ),
      integrations: model_telemetry.TelemetryIntegrationTotals(
        connected: response.integrations.connected.toInt(),
        revoked: response.integrations.revoked.toInt(),
      ),
      daily: response.daily
          .map(
            (day) => model_telemetry.TelemetryDailyActivity(
              date: day.date,
              runs: day.runs.toInt(),
              toolCalls: day.toolCalls.toInt(),
              inputTokens: day.hasInputTokens()
                  ? day.inputTokens.toInt()
                  : null,
              outputTokens: day.hasOutputTokens()
                  ? day.outputTokens.toInt()
                  : null,
            ),
          )
          .toList(growable: false),
    );
  }

  static dynamic _valueToDart(structpb.Value value) {
    switch (value.whichKind()) {
      case structpb.Value_Kind.nullValue:
        return null;
      case structpb.Value_Kind.numberValue:
        return value.numberValue;
      case structpb.Value_Kind.stringValue:
        return value.stringValue;
      case structpb.Value_Kind.boolValue:
        return value.boolValue;
      case structpb.Value_Kind.structValue:
        return structToMap(value.structValue);
      case structpb.Value_Kind.listValue:
        return value.listValue.values.map(_valueToDart).toList();
      case structpb.Value_Kind.notSet:
        return null;
    }
  }

  /// Audit reads, where every optional field is mapped from protobuf's
  /// `has*` presence bit rather than compared against a default. A falsy or
  /// empty value the server explicitly set (an empty string, `false`, `0`)
  /// must survive as that value, not collapse to null because it looks like
  /// "unset" — only the absence of the field itself means null here.
  static model_audit.AuditPage auditPageToModel(
    auditpb.ListAuditEntriesResponse response,
  ) {
    final nextCursor = response.page.nextCursor;
    return model_audit.AuditPage(
      entries: response.entries.map(auditEntryToModel).toList(growable: false),
      nextCursor: nextCursor.isEmpty ? null : nextCursor,
    );
  }

  static model_audit.AuditEntry auditEntryToModel(auditpb.AuditEntry entry) {
    return model_audit.AuditEntry(
      auditId: entry.auditId,
      correlationId: entry.hasCorrelationId() ? entry.correlationId : null,
      actorType: entry.actorType,
      actorId: entry.hasActorId() ? entry.actorId : null,
      action: entry.action,
      target: entry.hasTarget() ? entry.target : null,
      payload: auditPayloadToModel(entry.payload),
      createdAt: entry.createdAt.toDateTime().toUtc(),
    );
  }

  static model_audit.AuditPayload auditPayloadToModel(
    auditpb.AuditPayload payload,
  ) {
    return model_audit.AuditPayload(
      state: auditPayloadStateToModel(payload.state),
      toolName: payload.hasToolName() ? payload.toolName : null,
      serverName: payload.hasServerName() ? payload.serverName : null,
      phase: payload.hasPhase() ? payload.phase : null,
      status: payload.hasStatus() ? payload.status : null,
      reason: payload.hasReason() ? payload.reason : null,
      durationMs: payload.hasDurationMs() ? payload.durationMs.toInt() : null,
      errorCode: payload.hasErrorCode() ? payload.errorCode : null,
      provider: payload.hasProvider() ? payload.provider : null,
      displayName: payload.hasDisplayName() ? payload.displayName : null,
      unattended: payload.hasUnattended() ? payload.unattended : null,
      automationId: payload.hasAutomationId() ? payload.automationId : null,
      automationName: payload.hasAutomationName()
          ? payload.automationName
          : null,
      method: payload.hasMethod() ? payload.method : null,
      requestId: payload.hasRequestId() ? payload.requestId : null,
      deletedRuns: payload.hasDeletedRuns()
          ? payload.deletedRuns.toInt()
          : null,
      deletedMessages: payload.hasDeletedMessages()
          ? payload.deletedMessages.toInt()
          : null,
      decisionComment: payload.hasDecisionComment()
          ? payload.decisionComment
          : null,
      decisionCommentTruncated: payload.hasDecisionCommentTruncated()
          ? payload.decisionCommentTruncated
          : null,
      denialReason: payload.hasDenialReason() ? payload.denialReason : null,
      denialReasonTruncated: payload.hasDenialReasonTruncated()
          ? payload.denialReasonTruncated
          : null,
    );
  }

  /// Unspecified is not a fourth state this client can safely render: it
  /// means the server sent something this contract does not define, so this
  /// throws rather than guessing among absent, present, and scrubbed.
  static model_audit.AuditPayloadState auditPayloadStateToModel(
    auditpb.AuditPayloadState state,
  ) {
    switch (state) {
      case auditpb.AuditPayloadState.AUDIT_PAYLOAD_STATE_ABSENT:
        return model_audit.AuditPayloadState.absent;
      case auditpb.AuditPayloadState.AUDIT_PAYLOAD_STATE_PRESENT:
        return model_audit.AuditPayloadState.present;
      case auditpb.AuditPayloadState.AUDIT_PAYLOAD_STATE_SCRUBBED:
        return model_audit.AuditPayloadState.scrubbed;
      case auditpb.AuditPayloadState.AUDIT_PAYLOAD_STATE_UNSPECIFIED:
      default:
        throw FormatException(
          'audit payload state is unspecified: the server sent no state '
          'for this row, which this client refuses to render as absent, '
          'present, or scrubbed',
        );
    }
  }
}
