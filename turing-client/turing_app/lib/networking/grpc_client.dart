import 'dart:async';

import 'package:fixnum/fixnum.dart';
import 'package:grpc/grpc.dart' as grpc;
import 'package:grpc/service_api.dart' as grpc_api;

import '../generated/turing/v1/approvals.pb.dart' as approvalpb;
import '../generated/turing/v1/approvals.pbgrpc.dart' as approvalgrpc;
import '../generated/turing/v1/chat.pb.dart' as chatpb;
import '../generated/turing/v1/chat.pbgrpc.dart' as chatgrpc;
import '../generated/turing/v1/common.pb.dart' as commonpb;
import '../generated/turing/v1/events.pb.dart' as eventpb;
import '../generated/turing/v1/events.pbgrpc.dart' as eventgrpc;
import '../generated/turing/v1/sessions.pb.dart' as sessionpb;
import '../generated/turing/v1/sessions.pbgrpc.dart' as sessiongrpc;
import '../models/grpc_mappers.dart';
import '../models/message.dart';
import '../models/session.dart';
import '../models/turing_event.dart';
import 'api_client.dart';

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

abstract interface class ClosableTuringApi implements TuringApi {
  Future<void> close();
}

class TuringGrpcApi implements ClosableTuringApi {
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
  }

  final String baseUrl;
  final String apiKey;
  final grpc.ClientChannel _channel;
  final bool _ownsChannel;
  late final sessiongrpc.SessionServiceClient _sessions;
  late final eventgrpc.EventServiceClient _events;
  late final chatgrpc.ChatServiceClient _chat;
  late final approvalgrpc.ApprovalServiceClient _approvals;

  GrpcAuthMetadata get _metadata => GrpcAuthMetadata(apiKey: apiKey);

  @override
  Future<Map<String, dynamic>> getConfig() async {
    final response = await _sessions.getConfig(sessionpb.GetConfigRequest());
    final providers = <String, Map<String, dynamic>>{};
    for (final provider in response.providers) {
      providers[GrpcMappers.modelProviderToString(provider.provider)] = {
        'enabled': provider.enabled,
        'defaultModel': provider.defaultModel,
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
    );
    return response.messages.map(GrpcMappers.messageToModel).toList();
  }

  @override
  Future<List<TuringEvent>> listEvents({
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
    );
    return response.events.map(GrpcMappers.turingEventToTuringEvent).toList();
  }

  @override
  Future<Map<String, dynamic>> sendMessage({
    required String sessionId,
    required String content,
    String modelProvider = 'ollama',
  }) {
    final stream = _chat.sendMessage(
      chatpb.SendMessageRequest(
        sessionId: sessionId,
        content: content,
        contentType: 'text',
        agentId: commonpb.AgentId.AGENT_ID_GENERAL_ASSISTANT,
        modelProvider: GrpcMappers.modelProviderFromString(modelProvider),
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
