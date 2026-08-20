// This is a generated file - do not edit.
//
// Generated from turing/v1/sessions.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:async' as $async;
import 'dart:core' as $core;

import 'package:grpc/service_api.dart' as $grpc;
import 'package:protobuf/protobuf.dart' as $pb;

import 'sessions.pb.dart' as $0;

export 'sessions.pb.dart';

@$pb.GrpcServiceName('turing.v1.SessionService')
class SessionServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  SessionServiceClient(super.channel, {super.options, super.interceptors});

  $grpc.ResponseFuture<$0.CreateSessionResponse> createSession(
    $0.CreateSessionRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$createSession, request, options: options);
  }

  $grpc.ResponseFuture<$0.ListSessionsResponse> listSessions(
    $0.ListSessionsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listSessions, request, options: options);
  }

  $grpc.ResponseFuture<$0.Session> getSession(
    $0.GetSessionRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getSession, request, options: options);
  }

  $grpc.ResponseFuture<$0.DeleteSessionResponse> deleteSession(
    $0.DeleteSessionRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$deleteSession, request, options: options);
  }

  $grpc.ResponseFuture<$0.ListSessionDeletionReceiptsResponse>
      listSessionDeletionReceipts(
    $0.ListSessionDeletionReceiptsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listSessionDeletionReceipts, request,
        options: options);
  }

  $grpc.ResponseFuture<$0.ListMessagesResponse> listMessages(
    $0.ListMessagesRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listMessages, request, options: options);
  }

  $grpc.ResponseFuture<$0.SearchMessagesResponse> searchMessages(
    $0.SearchMessagesRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$searchMessages, request, options: options);
  }

  $grpc.ResponseFuture<$0.GetConfigResponse> getConfig(
    $0.GetConfigRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getConfig, request, options: options);
  }

  $grpc.ResponseFuture<$0.ListAgentsResponse> listAgents(
    $0.ListAgentsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listAgents, request, options: options);
  }

  $grpc.ResponseFuture<$0.ListToolsResponse> listTools(
    $0.ListToolsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listTools, request, options: options);
  }

  // method descriptors

  static final _$createSession =
      $grpc.ClientMethod<$0.CreateSessionRequest, $0.CreateSessionResponse>(
          '/turing.v1.SessionService/CreateSession',
          ($0.CreateSessionRequest value) => value.writeToBuffer(),
          $0.CreateSessionResponse.fromBuffer);
  static final _$listSessions =
      $grpc.ClientMethod<$0.ListSessionsRequest, $0.ListSessionsResponse>(
          '/turing.v1.SessionService/ListSessions',
          ($0.ListSessionsRequest value) => value.writeToBuffer(),
          $0.ListSessionsResponse.fromBuffer);
  static final _$getSession =
      $grpc.ClientMethod<$0.GetSessionRequest, $0.Session>(
          '/turing.v1.SessionService/GetSession',
          ($0.GetSessionRequest value) => value.writeToBuffer(),
          $0.Session.fromBuffer);
  static final _$deleteSession =
      $grpc.ClientMethod<$0.DeleteSessionRequest, $0.DeleteSessionResponse>(
          '/turing.v1.SessionService/DeleteSession',
          ($0.DeleteSessionRequest value) => value.writeToBuffer(),
          $0.DeleteSessionResponse.fromBuffer);
  static final _$listSessionDeletionReceipts = $grpc.ClientMethod<
          $0.ListSessionDeletionReceiptsRequest,
          $0.ListSessionDeletionReceiptsResponse>(
      '/turing.v1.SessionService/ListSessionDeletionReceipts',
      ($0.ListSessionDeletionReceiptsRequest value) => value.writeToBuffer(),
      $0.ListSessionDeletionReceiptsResponse.fromBuffer);
  static final _$listMessages =
      $grpc.ClientMethod<$0.ListMessagesRequest, $0.ListMessagesResponse>(
          '/turing.v1.SessionService/ListMessages',
          ($0.ListMessagesRequest value) => value.writeToBuffer(),
          $0.ListMessagesResponse.fromBuffer);
  static final _$searchMessages =
      $grpc.ClientMethod<$0.SearchMessagesRequest, $0.SearchMessagesResponse>(
          '/turing.v1.SessionService/SearchMessages',
          ($0.SearchMessagesRequest value) => value.writeToBuffer(),
          $0.SearchMessagesResponse.fromBuffer);
  static final _$getConfig =
      $grpc.ClientMethod<$0.GetConfigRequest, $0.GetConfigResponse>(
          '/turing.v1.SessionService/GetConfig',
          ($0.GetConfigRequest value) => value.writeToBuffer(),
          $0.GetConfigResponse.fromBuffer);
  static final _$listAgents =
      $grpc.ClientMethod<$0.ListAgentsRequest, $0.ListAgentsResponse>(
          '/turing.v1.SessionService/ListAgents',
          ($0.ListAgentsRequest value) => value.writeToBuffer(),
          $0.ListAgentsResponse.fromBuffer);
  static final _$listTools =
      $grpc.ClientMethod<$0.ListToolsRequest, $0.ListToolsResponse>(
          '/turing.v1.SessionService/ListTools',
          ($0.ListToolsRequest value) => value.writeToBuffer(),
          $0.ListToolsResponse.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.SessionService')
abstract class SessionServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.SessionService';

  SessionServiceBase() {
    $addMethod(
        $grpc.ServiceMethod<$0.CreateSessionRequest, $0.CreateSessionResponse>(
            'CreateSession',
            createSession_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.CreateSessionRequest.fromBuffer(value),
            ($0.CreateSessionResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.ListSessionsRequest, $0.ListSessionsResponse>(
            'ListSessions',
            listSessions_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.ListSessionsRequest.fromBuffer(value),
            ($0.ListSessionsResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.GetSessionRequest, $0.Session>(
        'GetSession',
        getSession_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.GetSessionRequest.fromBuffer(value),
        ($0.Session value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.DeleteSessionRequest, $0.DeleteSessionResponse>(
            'DeleteSession',
            deleteSession_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.DeleteSessionRequest.fromBuffer(value),
            ($0.DeleteSessionResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListSessionDeletionReceiptsRequest,
            $0.ListSessionDeletionReceiptsResponse>(
        'ListSessionDeletionReceipts',
        listSessionDeletionReceipts_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListSessionDeletionReceiptsRequest.fromBuffer(value),
        ($0.ListSessionDeletionReceiptsResponse value) =>
            value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.ListMessagesRequest, $0.ListMessagesResponse>(
            'ListMessages',
            listMessages_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.ListMessagesRequest.fromBuffer(value),
            ($0.ListMessagesResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.SearchMessagesRequest,
            $0.SearchMessagesResponse>(
        'SearchMessages',
        searchMessages_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.SearchMessagesRequest.fromBuffer(value),
        ($0.SearchMessagesResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.GetConfigRequest, $0.GetConfigResponse>(
        'GetConfig',
        getConfig_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.GetConfigRequest.fromBuffer(value),
        ($0.GetConfigResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListAgentsRequest, $0.ListAgentsResponse>(
        'ListAgents',
        listAgents_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.ListAgentsRequest.fromBuffer(value),
        ($0.ListAgentsResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListToolsRequest, $0.ListToolsResponse>(
        'ListTools',
        listTools_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.ListToolsRequest.fromBuffer(value),
        ($0.ListToolsResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.CreateSessionResponse> createSession_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.CreateSessionRequest> $request) async {
    return createSession($call, await $request);
  }

  $async.Future<$0.CreateSessionResponse> createSession(
      $grpc.ServiceCall call, $0.CreateSessionRequest request);

  $async.Future<$0.ListSessionsResponse> listSessions_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListSessionsRequest> $request) async {
    return listSessions($call, await $request);
  }

  $async.Future<$0.ListSessionsResponse> listSessions(
      $grpc.ServiceCall call, $0.ListSessionsRequest request);

  $async.Future<$0.Session> getSession_Pre($grpc.ServiceCall $call,
      $async.Future<$0.GetSessionRequest> $request) async {
    return getSession($call, await $request);
  }

  $async.Future<$0.Session> getSession(
      $grpc.ServiceCall call, $0.GetSessionRequest request);

  $async.Future<$0.DeleteSessionResponse> deleteSession_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.DeleteSessionRequest> $request) async {
    return deleteSession($call, await $request);
  }

  $async.Future<$0.DeleteSessionResponse> deleteSession(
      $grpc.ServiceCall call, $0.DeleteSessionRequest request);

  $async.Future<$0.ListSessionDeletionReceiptsResponse>
      listSessionDeletionReceipts_Pre($grpc.ServiceCall $call,
          $async.Future<$0.ListSessionDeletionReceiptsRequest> $request) async {
    return listSessionDeletionReceipts($call, await $request);
  }

  $async.Future<$0.ListSessionDeletionReceiptsResponse>
      listSessionDeletionReceipts($grpc.ServiceCall call,
          $0.ListSessionDeletionReceiptsRequest request);

  $async.Future<$0.ListMessagesResponse> listMessages_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListMessagesRequest> $request) async {
    return listMessages($call, await $request);
  }

  $async.Future<$0.ListMessagesResponse> listMessages(
      $grpc.ServiceCall call, $0.ListMessagesRequest request);

  $async.Future<$0.SearchMessagesResponse> searchMessages_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.SearchMessagesRequest> $request) async {
    return searchMessages($call, await $request);
  }

  $async.Future<$0.SearchMessagesResponse> searchMessages(
      $grpc.ServiceCall call, $0.SearchMessagesRequest request);

  $async.Future<$0.GetConfigResponse> getConfig_Pre($grpc.ServiceCall $call,
      $async.Future<$0.GetConfigRequest> $request) async {
    return getConfig($call, await $request);
  }

  $async.Future<$0.GetConfigResponse> getConfig(
      $grpc.ServiceCall call, $0.GetConfigRequest request);

  $async.Future<$0.ListAgentsResponse> listAgents_Pre($grpc.ServiceCall $call,
      $async.Future<$0.ListAgentsRequest> $request) async {
    return listAgents($call, await $request);
  }

  $async.Future<$0.ListAgentsResponse> listAgents(
      $grpc.ServiceCall call, $0.ListAgentsRequest request);

  $async.Future<$0.ListToolsResponse> listTools_Pre($grpc.ServiceCall $call,
      $async.Future<$0.ListToolsRequest> $request) async {
    return listTools($call, await $request);
  }

  $async.Future<$0.ListToolsResponse> listTools(
      $grpc.ServiceCall call, $0.ListToolsRequest request);
}
