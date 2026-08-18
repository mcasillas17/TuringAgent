import 'dart:async';

import '../models/agent_descriptor.dart';
import '../models/message.dart';
import '../models/search_hit.dart';
import '../models/session.dart';
import '../models/skill.dart';
import '../models/tool_descriptor.dart';
import '../models/turing_event.dart';

abstract class TuringApi {
  Future<Map<String, dynamic>> getConfig();

  Future<Map<String, dynamic>> createSession({String? title});

  Future<List<Session>> listSessions({int limit = 50, String? after});

  Future<Session> getSession({required String sessionId});

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

  /// The user's whole skill library.
  Future<List<Skill>> listSkills();

  Future<Skill> createSkill({
    required String name,
    required String instructions,
  });

  Future<Skill> updateSkill({
    required String skillId,
    required String name,
    required String instructions,
  });

  /// Removes a skill from the library, which also detaches it from every
  /// conversation. Runs already queued are unaffected — they carry a snapshot
  /// of the instructions taken when the message was sent.
  Future<void> deleteSkill({required String skillId});

  /// Attach and detach return the conversation's full skill set, so the client
  /// never has to guess what the server now believes is attached.
  Future<List<Skill>> attachSkill({
    required String sessionId,
    required String skillId,
  });

  Future<List<Skill>> detachSkill({
    required String sessionId,
    required String skillId,
  });

  Future<List<Skill>> listSessionSkills({required String sessionId});
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
