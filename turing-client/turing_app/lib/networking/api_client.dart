import 'dart:async';

import '../models/agent_descriptor.dart';
import '../models/audit.dart';
import '../models/external_agent.dart';
import '../models/integration.dart';
import '../models/automation.dart';
import '../models/message.dart';
import '../models/search_hit.dart';
import '../models/session.dart';
import '../models/session_page.dart';
import '../models/skill.dart';
import '../models/telemetry.dart';
import '../models/tool_descriptor.dart';
import '../models/turing_event.dart';

abstract class TuringApi {
  Future<Map<String, dynamic>> getConfig();

  Future<Map<String, dynamic>> createSession({String? title});

  Future<List<Session>> listSessions({int limit = 50, String? after});

  Future<SessionPage> listSessionPage({
    int limit = 50,
    String? cursor,
    SessionListFilter filter = SessionListFilter.active,
  });

  Future<Session> getSession({required String sessionId});

  Future<Session> renameSession({
    required String sessionId,
    required String title,
  });

  Future<Session> archiveSession({required String sessionId});

  Future<Session> restoreSession({required String sessionId});

  /// Removes a session, its messages and its run history. Permanent: there is
  /// no undo, and the content also leaves the search index. Note this does NOT
  /// remove files the session wrote into the sandbox — mcp-files has no notion
  /// of a session, so those outlive it.
  Future<void> deleteSession({required String sessionId});

  Future<List<Message>> listMessages({
    required String sessionId,
    int limit = 50,
    String? before,
  });

  Future<List<SearchHit>> searchMessages({
    required String query,
    int limit = 50,
  });

  Future<TuringEventPage> listEvents({
    required String sessionId,
    int? after,
    int limit = 500,
  });

  Future<Map<String, dynamic>> sendMessage({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
    String? idempotencyKey,
  });

  Future<Map<String, dynamic>> approveApproval(
    String approvalId, {
    String? comment,
  });

  Future<Map<String, dynamic>> denyApproval(
    String approvalId, {
    String? reason,
  });

  /// Every tool the backend has discovered from its MCP servers, with the
  /// approval policy currently attached to each.
  Future<List<ToolDescriptor>> listTools();

  /// The agents the backend can route a run to.
  Future<List<AgentDescriptor>> listAgents();

  /// Every SKILL.md discovered under the backend's skills directory.
  Future<List<Skill>> listSkills();

  Future<Skill> getSkill({required String skillId});

  /// Enabling a skill does not implicitly approve any capability.
  Future<Skill> setSkillEnabled({
    required String skillId,
    required bool enabled,
  });

  Future<Skill> setSkillCapabilityGrant({
    required String skillId,
    required String capability,
    required bool granted,
  });

  /// The assistants the user has configured that do NOT run on this machine.
  /// Turing's own assistant is not among them: it is the default, and it is
  /// the only destination that keeps a conversation local.
  Future<List<ExternalAgent>> listExternalAgents();

  Future<ExternalAgent> createExternalAgent({
    required String displayName,
    required ExternalAgentProvider provider,
    required String baseUrl,
    required String model,
    required String credentialRef,
  });

  Future<ExternalAgent> updateExternalAgent({
    required String agentId,
    required String displayName,
    required ExternalAgentProvider provider,
    required String baseUrl,
    required String model,
    required String credentialRef,
  });

  /// Removes an agent, which also returns every conversation routed to it to
  /// the local assistant. Failing towards "stays on this machine" is the only
  /// safe direction for this to fail in.
  Future<void> deleteExternalAgent({required String agentId});

  /// Where a conversation's messages go. Null means the local assistant.
  Future<ExternalAgent?> getSessionAgent({required String sessionId});

  /// Routes a conversation to an agent, replacing wherever it went before.
  /// Returns the destination the server now believes in, so the client never
  /// has to guess.
  Future<ExternalAgent?> setSessionAgent({
    required String sessionId,
    required String agentId,
  });

  /// Returns a conversation to the local assistant.
  Future<ExternalAgent?> clearSessionAgent({required String sessionId});

  /// What can be connected, what cannot, what each kind of credential grants,
  /// and whether this backend is set up to store one at all. Served by the
  /// backend so the client does not have its own idea of what connecting
  /// gives away.
  Future<IntegrationCatalogue> listIntegrationProviders();

  /// Every connected account, live and revoked. No response carries a stored
  /// credential — only a redacted hint.
  Future<List<IntegrationConnection>> listConnections();

  /// Stores a credential the user created at the provider.
  ///
  /// [consentAcknowledged] is the user's agreement to the provider's grants.
  /// The backend refuses the call without it, so this is not a formality the
  /// client can skip.
  Future<IntegrationConnection> connectAccount({
    required IntegrationProviderKind provider,
    required String displayName,
    required String credential,
    required bool consentAcknowledged,
    String accountLabel,
    String endpoint,
  });

  /// Destroys the stored credential. The record of the connection survives so
  /// the user can still see the account once had access. This cannot
  /// invalidate the credential at the provider — only the provider can.
  Future<IntegrationConnection> revokeConnection({
    required String connectionId,
  });

  /// Removes the connection and its history. Deleting a live one destroys its
  /// credential too.
  Future<void> deleteConnection({required String connectionId});

  /// Every automation, whether enabled or not.
  Future<List<Automation>> listAutomations();

  Future<Automation> createAutomation({
    required String name,
    required String prompt,
    required AutomationSchedule schedule,
    required bool enabled,
    required List<AutomationTool> allowedTools,
  });

  /// Enabling is deliberately NOT part of this: a save must not require
  /// resending a schedule you did not intend to change, and a toggle must not
  /// require resending a prompt.
  Future<Automation> updateAutomation({
    required String automationId,
    required String name,
    required String prompt,
    required AutomationSchedule schedule,
    required List<AutomationTool> allowedTools,
  });

  Future<Automation> setAutomationEnabled({
    required String automationId,
    required bool enabled,
  });

  /// Stops future runs. The conversation it produced, and any run already in
  /// flight, are left alone — the record of what it did outlives the schedule
  /// that caused it.
  Future<void> deleteAutomation({required String automationId});

  /// What this installation has been doing over the last [windowDays] days.
  ///
  /// Read-only, and computed by the backend from its own database — no counter
  /// is kept here, and nothing is sent anywhere. There is no write side, which
  /// is why this is the only telemetry method: every number it returns was
  /// recorded by some other part of the system doing its actual work.
  ///
  /// The backend REFUSES a window it cannot answer rather than narrowing it,
  /// so the returned window always describes the returned numbers.
  Future<TelemetrySummary> getTelemetrySummary({required int windowDays});

  /// An authenticated, local-only read of the audit log the backend already
  /// keeps. Every row is redacted server-side before it ever reaches this
  /// client: the response never carries raw tool arguments, result
  /// summaries, credentials, approval tokens, or other secrets, only the
  /// typed fields the server's own per-action allowlist chose to disclose.
  ///
  /// [correlationId] and [action] are exact-match filters. [createdAtStart]
  /// is an inclusive lower bound and [createdAtEnd] an exclusive upper bound
  /// on [AuditEntry.createdAt]; both are sent in UTC. [order] controls
  /// whether the newest or oldest matching entry comes first, [limit] bounds
  /// the page size, and [cursor] resumes from a previous [AuditPage].
  Future<AuditPage> listAuditEntries({
    String? correlationId,
    String? action,
    DateTime? createdAtStart,
    DateTime? createdAtEnd,
    AuditOrder order = AuditOrder.descending,
    int limit = 50,
    String? cursor,
  });
}

class TuringApiException implements Exception {
  const TuringApiException({
    required this.code,
    required this.message,
    this.requestId,
  });

  final String code;
  final String message;
  final String? requestId;

  @override
  String toString() {
    final suffix = requestId == null ? '' : ' ($requestId)';
    return 'TuringApiException: $message$suffix';
  }
}
