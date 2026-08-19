// This is a generated file - do not edit.
//
// Generated from turing/v1/agents.proto.

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

import 'agents.pb.dart' as $0;

export 'agents.pb.dart';

@$pb.GrpcServiceName('turing.v1.ExternalAgentService')
class ExternalAgentServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  ExternalAgentServiceClient(super.channel,
      {super.options, super.interceptors});

  $grpc.ResponseFuture<$0.ExternalAgent> createExternalAgent(
    $0.CreateExternalAgentRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$createExternalAgent, request, options: options);
  }

  $grpc.ResponseFuture<$0.ExternalAgent> updateExternalAgent(
    $0.UpdateExternalAgentRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$updateExternalAgent, request, options: options);
  }

  $grpc.ResponseFuture<$0.DeleteExternalAgentResponse> deleteExternalAgent(
    $0.DeleteExternalAgentRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$deleteExternalAgent, request, options: options);
  }

  $grpc.ResponseFuture<$0.ListExternalAgentsResponse> listExternalAgents(
    $0.ListExternalAgentsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listExternalAgents, request, options: options);
  }

  /// Set, clear and get all return the conversation's current destination, so a
  /// client never has to guess what the server now believes it is.
  $grpc.ResponseFuture<$0.SessionAgentResponse> getSessionAgent(
    $0.GetSessionAgentRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getSessionAgent, request, options: options);
  }

  $grpc.ResponseFuture<$0.SessionAgentResponse> setSessionAgent(
    $0.SetSessionAgentRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$setSessionAgent, request, options: options);
  }

  $grpc.ResponseFuture<$0.SessionAgentResponse> clearSessionAgent(
    $0.ClearSessionAgentRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$clearSessionAgent, request, options: options);
  }

  // method descriptors

  static final _$createExternalAgent =
      $grpc.ClientMethod<$0.CreateExternalAgentRequest, $0.ExternalAgent>(
          '/turing.v1.ExternalAgentService/CreateExternalAgent',
          ($0.CreateExternalAgentRequest value) => value.writeToBuffer(),
          $0.ExternalAgent.fromBuffer);
  static final _$updateExternalAgent =
      $grpc.ClientMethod<$0.UpdateExternalAgentRequest, $0.ExternalAgent>(
          '/turing.v1.ExternalAgentService/UpdateExternalAgent',
          ($0.UpdateExternalAgentRequest value) => value.writeToBuffer(),
          $0.ExternalAgent.fromBuffer);
  static final _$deleteExternalAgent = $grpc.ClientMethod<
          $0.DeleteExternalAgentRequest, $0.DeleteExternalAgentResponse>(
      '/turing.v1.ExternalAgentService/DeleteExternalAgent',
      ($0.DeleteExternalAgentRequest value) => value.writeToBuffer(),
      $0.DeleteExternalAgentResponse.fromBuffer);
  static final _$listExternalAgents = $grpc.ClientMethod<
          $0.ListExternalAgentsRequest, $0.ListExternalAgentsResponse>(
      '/turing.v1.ExternalAgentService/ListExternalAgents',
      ($0.ListExternalAgentsRequest value) => value.writeToBuffer(),
      $0.ListExternalAgentsResponse.fromBuffer);
  static final _$getSessionAgent =
      $grpc.ClientMethod<$0.GetSessionAgentRequest, $0.SessionAgentResponse>(
          '/turing.v1.ExternalAgentService/GetSessionAgent',
          ($0.GetSessionAgentRequest value) => value.writeToBuffer(),
          $0.SessionAgentResponse.fromBuffer);
  static final _$setSessionAgent =
      $grpc.ClientMethod<$0.SetSessionAgentRequest, $0.SessionAgentResponse>(
          '/turing.v1.ExternalAgentService/SetSessionAgent',
          ($0.SetSessionAgentRequest value) => value.writeToBuffer(),
          $0.SessionAgentResponse.fromBuffer);
  static final _$clearSessionAgent =
      $grpc.ClientMethod<$0.ClearSessionAgentRequest, $0.SessionAgentResponse>(
          '/turing.v1.ExternalAgentService/ClearSessionAgent',
          ($0.ClearSessionAgentRequest value) => value.writeToBuffer(),
          $0.SessionAgentResponse.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.ExternalAgentService')
abstract class ExternalAgentServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.ExternalAgentService';

  ExternalAgentServiceBase() {
    $addMethod(
        $grpc.ServiceMethod<$0.CreateExternalAgentRequest, $0.ExternalAgent>(
            'CreateExternalAgent',
            createExternalAgent_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.CreateExternalAgentRequest.fromBuffer(value),
            ($0.ExternalAgent value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.UpdateExternalAgentRequest, $0.ExternalAgent>(
            'UpdateExternalAgent',
            updateExternalAgent_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.UpdateExternalAgentRequest.fromBuffer(value),
            ($0.ExternalAgent value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.DeleteExternalAgentRequest,
            $0.DeleteExternalAgentResponse>(
        'DeleteExternalAgent',
        deleteExternalAgent_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.DeleteExternalAgentRequest.fromBuffer(value),
        ($0.DeleteExternalAgentResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListExternalAgentsRequest,
            $0.ListExternalAgentsResponse>(
        'ListExternalAgents',
        listExternalAgents_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListExternalAgentsRequest.fromBuffer(value),
        ($0.ListExternalAgentsResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.GetSessionAgentRequest, $0.SessionAgentResponse>(
            'GetSessionAgent',
            getSessionAgent_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.GetSessionAgentRequest.fromBuffer(value),
            ($0.SessionAgentResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.SetSessionAgentRequest, $0.SessionAgentResponse>(
            'SetSessionAgent',
            setSessionAgent_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.SetSessionAgentRequest.fromBuffer(value),
            ($0.SessionAgentResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ClearSessionAgentRequest,
            $0.SessionAgentResponse>(
        'ClearSessionAgent',
        clearSessionAgent_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ClearSessionAgentRequest.fromBuffer(value),
        ($0.SessionAgentResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.ExternalAgent> createExternalAgent_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.CreateExternalAgentRequest> $request) async {
    return createExternalAgent($call, await $request);
  }

  $async.Future<$0.ExternalAgent> createExternalAgent(
      $grpc.ServiceCall call, $0.CreateExternalAgentRequest request);

  $async.Future<$0.ExternalAgent> updateExternalAgent_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.UpdateExternalAgentRequest> $request) async {
    return updateExternalAgent($call, await $request);
  }

  $async.Future<$0.ExternalAgent> updateExternalAgent(
      $grpc.ServiceCall call, $0.UpdateExternalAgentRequest request);

  $async.Future<$0.DeleteExternalAgentResponse> deleteExternalAgent_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.DeleteExternalAgentRequest> $request) async {
    return deleteExternalAgent($call, await $request);
  }

  $async.Future<$0.DeleteExternalAgentResponse> deleteExternalAgent(
      $grpc.ServiceCall call, $0.DeleteExternalAgentRequest request);

  $async.Future<$0.ListExternalAgentsResponse> listExternalAgents_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListExternalAgentsRequest> $request) async {
    return listExternalAgents($call, await $request);
  }

  $async.Future<$0.ListExternalAgentsResponse> listExternalAgents(
      $grpc.ServiceCall call, $0.ListExternalAgentsRequest request);

  $async.Future<$0.SessionAgentResponse> getSessionAgent_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.GetSessionAgentRequest> $request) async {
    return getSessionAgent($call, await $request);
  }

  $async.Future<$0.SessionAgentResponse> getSessionAgent(
      $grpc.ServiceCall call, $0.GetSessionAgentRequest request);

  $async.Future<$0.SessionAgentResponse> setSessionAgent_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.SetSessionAgentRequest> $request) async {
    return setSessionAgent($call, await $request);
  }

  $async.Future<$0.SessionAgentResponse> setSessionAgent(
      $grpc.ServiceCall call, $0.SetSessionAgentRequest request);

  $async.Future<$0.SessionAgentResponse> clearSessionAgent_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ClearSessionAgentRequest> $request) async {
    return clearSessionAgent($call, await $request);
  }

  $async.Future<$0.SessionAgentResponse> clearSessionAgent(
      $grpc.ServiceCall call, $0.ClearSessionAgentRequest request);
}
