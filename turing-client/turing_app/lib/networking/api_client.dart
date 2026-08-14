import 'dart:async';

import '../models/message.dart';
import '../models/search_hit.dart';
import '../models/session.dart';
import '../models/turing_event.dart';

abstract class TuringApi {
  Future<Map<String, dynamic>> getConfig();

  Future<Map<String, dynamic>> createSession({String? title});

  Future<List<Session>> listSessions({int limit = 50, String? after});

  Future<Session> getSession({required String sessionId});

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
