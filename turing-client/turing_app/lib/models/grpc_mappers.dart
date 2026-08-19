import '../generated/google/protobuf/struct.pb.dart' as structpb;
import '../generated/google/protobuf/timestamp.pb.dart' as timestamppb;

import '../generated/turing/v1/agents.pb.dart' as agentpb;
import '../generated/turing/v1/approvals.pb.dart' as approvalpb;
import '../generated/turing/v1/chat.pb.dart' as chatpb;
import '../generated/turing/v1/common.pb.dart' as commonpb;
import '../generated/turing/v1/events.pb.dart' as eventpb;
import '../generated/turing/v1/sessions.pb.dart' as sessionpb;
import '../generated/turing/v1/skills.pb.dart' as skillpb;
import 'agent_descriptor.dart' as model_agent;
import 'external_agent.dart' as model_external_agent;
import 'message.dart' as model_message;
import 'search_hit.dart' as model_search_hit;
import 'session.dart' as model_session;
import 'skill.dart' as model_skill;
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

  static model_skill.Skill skillToModel(skillpb.Skill skill) {
    return model_skill.Skill(
      skillId: skill.skillId,
      name: skill.name,
      instructions: skill.instructions,
    );
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
    );
  }

  static model_message.Message messageToModel(commonpb.Message message) {
    return model_message.Message(
      messageId: message.messageId,
      runId: message.runId.isEmpty ? null : message.runId,
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
    );
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
          'retryable': event.runFailed.retryable,
        };
      case chatpb.ChatStreamEvent_Event.runCancelled:
        return {
          'runId': event.runCancelled.runId,
          'reason': event.runCancelled.reason,
        };
      case chatpb.ChatStreamEvent_Event.persistedEvent:
        return structToMap(event.persistedEvent.payload);
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
}
