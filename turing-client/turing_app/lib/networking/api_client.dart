import 'dart:async';

import '../models/agent_descriptor.dart';
import '../models/audit.dart';
import '../models/external_agent.dart';
import '../models/integration.dart';
import '../models/automation.dart';
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

abstract class TuringApi implements RemoteEgressApi {
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

  /// Starts or advances an idempotent session withdrawal. The caller removes
  /// local state only after a completed receipt; an in-progress or
  /// failed-external receipt remains visible for retry.
  Future<SessionDeletionReceipt> deleteSession({required String sessionId});

  /// Content-free receipts for withdrawals that have not completed yet. This
  /// lets the client preserve a retryable placeholder after restart without
  /// redisclosing the session title or transcript.
  Future<List<SessionDeletionReceipt>> listSessionDeletionReceipts() async =>
      const [];

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

  @override
  Future<RemoteEgressDisclosure?> prepareRemoteEgress({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
    required String idempotencyKey,
  }) async {
    if (modelProvider == 'ollama') return null;
    throw const TuringApiException(
      code: 'remote_egress_unsupported',
      message: 'This client cannot prepare remote egress consent',
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
    throw const TuringApiException(
      code: 'remote_egress_unsupported',
      message: 'This client cannot send remote egress consent',
    );
  }

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

  Future<McpRegistrySnapshot> listMcpServers() async {
    final tools = await listTools();
    final grouped = <String, List<ToolDescriptor>>{};
    for (final tool in tools) {
      grouped.putIfAbsent(tool.serverName, () => []).add(tool);
    }
    return McpRegistrySnapshot(
      servers: [
        for (final entry in grouped.entries)
          McpServer(
            serverId: entry.key,
            name: entry.key,
            transport: 'http',
            url: '',
            tier: McpServerTier.unspecified,
            enabled: true,
            liveness: McpServerLiveness.unknown,
            statusMessage: '',
            sandboxConfined: false,
            tools: entry.value,
          ),
      ],
      unsupported: const [],
    );
  }

  Future<McpServer> setMcpServerEnabled({
    required String serverId,
    required bool enabled,
  }) {
    throw const TuringApiException(
      code: 'mcp_registry_unsupported',
      message: 'This client cannot update MCP servers',
    );
  }

  Future<ToolDescriptor> updateMcpToolPolicy({
    required String serverId,
    required String toolName,
    required ToolPolicy policy,
  }) {
    throw const TuringApiException(
      code: 'mcp_registry_unsupported',
      message: 'This client cannot update MCP tool policy',
    );
  }

  Future<void> deleteMcpServer({required String serverId}) {
    throw const TuringApiException(
      code: 'mcp_registry_unsupported',
      message: 'This client cannot delete MCP servers',
    );
  }

  /// Registers a new MCP server. [bearerToken] is sent to the backend as
  /// plaintext for this one call only: it is never echoed back, stored on
  /// [McpServer], or otherwise retained by the client.
  Future<McpServer> registerMcpServer({
    required String name,
    required String url,
    required McpServerTier tier,
    String bearerToken = '',
  }) {
    throw const TuringApiException(
      code: 'mcp_registry_unsupported',
      message: 'This client cannot register MCP servers',
    );
  }

  /// Reimports the backend's mcp.json configuration, registering any server
  /// not already known to it.
  Future<McpImportReport> reimportMcpJson() {
    throw const TuringApiException(
      code: 'mcp_registry_unsupported',
      message: 'This client cannot reimport MCP JSON configuration',
    );
  }

  /// Replaces the bearer token stored for an existing MCP server.
  /// [bearerToken] is sent as plaintext for this one call only and is never
  /// retained by the client.
  Future<McpServer> rotateMcpServerToken({
    required String serverId,
    required String bearerToken,
  }) {
    throw const TuringApiException(
      code: 'mcp_registry_unsupported',
      message: 'This client cannot rotate MCP server tokens',
    );
  }

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

  /// The whole vault as the backend sees it: the toggle, the tier rows, the
  /// two pinned documents, accepted beliefs, and everything sitting in the
  /// inbox — including the reasons anything could not be read.
  ///
  /// This client holds no memory state of its own. It renders what this
  /// returns and re-reads after every decision, because the vault is files the
  /// user can also edit in Obsidian and a cached copy would be a guess.
  Future<MemoryState> listMemoryState() {
    throw const TuringApiException(
      code: 'memory_unsupported',
      message: 'This client cannot read memory state',
    );
  }

  /// Turns memory as a whole on or off. Returns the settings the server now
  /// holds, so the page never has to assume the write landed.
  Future<MemorySettings> setMemoryEnabled({required bool enabled}) {
    throw const TuringApiException(
      code: 'memory_unsupported',
      message: 'This client cannot change memory settings',
    );
  }

  /// Accepts a proposal as a belief. [expectedContentHash] is compare-and-set
  /// against the row the server holds; [expectedCandidateHash] is
  /// compare-and-set against the inbox file's own bytes, which is what the user
  /// was actually shown and what they may have rewritten in their editor.
  /// Accepts a belief.
  ///
  /// [expectedCandidateHash] is the compare-and-set over the candidate file as
  /// the listing served it. There is only one candidate compare-and-set: the
  /// request's older `expected_content_hash` named the same file and is
  /// deprecated on the wire, so nothing here sends it.
  Future<MemoryCandidate> promoteMemoryCandidate({
    required String candidateId,
    required String expectedCandidateHash,
  }) {
    throw const TuringApiException(
      code: 'memory_unsupported',
      message: 'This client cannot promote memory candidates',
    );
  }

  /// Refuses a proposal. See [promoteMemoryCandidate] for the token.
  Future<MemoryCandidate> rejectMemoryCandidate({
    required String candidateId,
    required String expectedCandidateHash,
    String reason = '',
  }) {
    throw const TuringApiException(
      code: 'memory_unsupported',
      message: 'This client cannot reject memory candidates',
    );
  }

  /// Applies an accepted `profile_edit` proposal.
  ///
  /// [content] is the WHOLE resulting profile document as the user reviewed it,
  /// never the proposal on its own: the server replaces profile.md with exactly
  /// these bytes, so sending the candidate's fragment would delete everything
  /// the user has written about themselves.
  ///
  /// [expectedContentHash] is compare-and-set against the profile document —
  /// does profile.md still say what the result was composed over — and
  /// [expectedCandidateHash] is compare-and-set against the proposal, so the
  /// result cannot be applied on the authority of a proposal that has since
  /// changed.
  Future<MemoryDocument> applyMemoryProfile({
    required String candidateId,
    required String content,
    required String expectedContentHash,
    String expectedCandidateHash = '',
  }) {
    throw const TuringApiException(
      code: 'memory_unsupported',
      message: 'This client cannot apply memory profile edits',
    );
  }

  /// Saves persona.md as the user typed it. This is the only write path to the
  /// persona in the whole system, and it exists for the user alone — no agent
  /// surface reaches it.
  Future<MemoryDocument> saveMemoryPersona({
    required String content,
    required String expectedContentHash,
  }) {
    throw const TuringApiException(
      code: 'memory_unsupported',
      message: 'This client cannot save the memory persona',
    );
  }

  /// Saves profile.md as the user typed it, with no proposal involved.
  Future<MemoryDocument> saveMemoryProfile({
    required String content,
    required String expectedContentHash,
  }) {
    throw const TuringApiException(
      code: 'memory_unsupported',
      message: 'This client cannot save the memory profile',
    );
  }

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

abstract interface class RemoteEgressApi {
  Future<RemoteEgressDisclosure?> prepareRemoteEgress({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
    required String idempotencyKey,
  });

  Future<Map<String, dynamic>> sendMessageWithRemoteEgressConsent({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
    required String idempotencyKey,
    required RemoteEgressConsent consent,
  });
}

abstract interface class PseudoServerPolicyApi {
  Future<List<ToolDescriptor>> listPseudoServerTools({
    required String serverName,
  });

  Future<ToolDescriptor> updateToolPolicyByName({
    required String serverName,
    required String toolName,
    required ToolPolicy policy,
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
