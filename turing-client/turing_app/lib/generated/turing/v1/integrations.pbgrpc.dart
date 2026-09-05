// This is a generated file - do not edit.
//
// Generated from turing/v1/integrations.proto.

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

import 'integrations.pb.dart' as $0;

export 'integrations.pb.dart';

@$pb.GrpcServiceName('turing.v1.IntegrationService')
class IntegrationServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  IntegrationServiceClient(super.channel, {super.options, super.interceptors});

  /// The catalogue: what can be connected, what cannot, and what each kind of
  /// credential grants.
  $grpc.ResponseFuture<$0.ListProvidersResponse> listProviders(
    $0.ListProvidersRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listProviders, request, options: options);
  }

  /// Refuses unsupported providers before credential validation, sealing or
  /// storage. For a supported provider, stores a credential the user minted
  /// after explicit consent to its grants in the same request.
  $grpc.ResponseFuture<$0.Connection> connectAccount(
    $0.ConnectAccountRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$connectAccount, request, options: options);
  }

  $grpc.ResponseFuture<$0.ListConnectionsResponse> listConnections(
    $0.ListConnectionsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listConnections, request, options: options);
  }

  $grpc.ResponseFuture<$0.Connection> getConnection(
    $0.GetConnectionRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getConnection, request, options: options);
  }

  /// Destroys the stored credential and marks the connection revoked. The
  /// record of the connection survives; the secret does not. This cannot
  /// invalidate the credential at the provider — only the provider can do
  /// that — and a client must say so.
  $grpc.ResponseFuture<$0.Connection> revokeConnection(
    $0.RevokeConnectionRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$revokeConnection, request, options: options);
  }

  /// Removes the saved connection row and its credential. Audit records remain.
  /// Revoking first is not required. Neither cleanup RPC revokes vendor copies.
  $grpc.ResponseFuture<$0.DeleteConnectionResponse> deleteConnection(
    $0.DeleteConnectionRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$deleteConnection, request, options: options);
  }

  $grpc.ResponseFuture<$0.ListIntegrationToolsResponse> listIntegrationTools(
    $0.ListIntegrationToolsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listIntegrationTools, request, options: options);
  }

  $grpc.ResponseFuture<$0.CallIntegrationToolResponse> callIntegrationTool(
    $0.CallIntegrationToolRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$callIntegrationTool, request, options: options);
  }

  // method descriptors

  static final _$listProviders =
      $grpc.ClientMethod<$0.ListProvidersRequest, $0.ListProvidersResponse>(
          '/turing.v1.IntegrationService/ListProviders',
          ($0.ListProvidersRequest value) => value.writeToBuffer(),
          $0.ListProvidersResponse.fromBuffer);
  static final _$connectAccount =
      $grpc.ClientMethod<$0.ConnectAccountRequest, $0.Connection>(
          '/turing.v1.IntegrationService/ConnectAccount',
          ($0.ConnectAccountRequest value) => value.writeToBuffer(),
          $0.Connection.fromBuffer);
  static final _$listConnections =
      $grpc.ClientMethod<$0.ListConnectionsRequest, $0.ListConnectionsResponse>(
          '/turing.v1.IntegrationService/ListConnections',
          ($0.ListConnectionsRequest value) => value.writeToBuffer(),
          $0.ListConnectionsResponse.fromBuffer);
  static final _$getConnection =
      $grpc.ClientMethod<$0.GetConnectionRequest, $0.Connection>(
          '/turing.v1.IntegrationService/GetConnection',
          ($0.GetConnectionRequest value) => value.writeToBuffer(),
          $0.Connection.fromBuffer);
  static final _$revokeConnection =
      $grpc.ClientMethod<$0.RevokeConnectionRequest, $0.Connection>(
          '/turing.v1.IntegrationService/RevokeConnection',
          ($0.RevokeConnectionRequest value) => value.writeToBuffer(),
          $0.Connection.fromBuffer);
  static final _$deleteConnection = $grpc.ClientMethod<
          $0.DeleteConnectionRequest, $0.DeleteConnectionResponse>(
      '/turing.v1.IntegrationService/DeleteConnection',
      ($0.DeleteConnectionRequest value) => value.writeToBuffer(),
      $0.DeleteConnectionResponse.fromBuffer);
  static final _$listIntegrationTools = $grpc.ClientMethod<
          $0.ListIntegrationToolsRequest, $0.ListIntegrationToolsResponse>(
      '/turing.v1.IntegrationService/ListIntegrationTools',
      ($0.ListIntegrationToolsRequest value) => value.writeToBuffer(),
      $0.ListIntegrationToolsResponse.fromBuffer);
  static final _$callIntegrationTool = $grpc.ClientMethod<
          $0.CallIntegrationToolRequest, $0.CallIntegrationToolResponse>(
      '/turing.v1.IntegrationService/CallIntegrationTool',
      ($0.CallIntegrationToolRequest value) => value.writeToBuffer(),
      $0.CallIntegrationToolResponse.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.IntegrationService')
abstract class IntegrationServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.IntegrationService';

  IntegrationServiceBase() {
    $addMethod(
        $grpc.ServiceMethod<$0.ListProvidersRequest, $0.ListProvidersResponse>(
            'ListProviders',
            listProviders_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.ListProvidersRequest.fromBuffer(value),
            ($0.ListProvidersResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ConnectAccountRequest, $0.Connection>(
        'ConnectAccount',
        connectAccount_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ConnectAccountRequest.fromBuffer(value),
        ($0.Connection value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListConnectionsRequest,
            $0.ListConnectionsResponse>(
        'ListConnections',
        listConnections_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListConnectionsRequest.fromBuffer(value),
        ($0.ListConnectionsResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.GetConnectionRequest, $0.Connection>(
        'GetConnection',
        getConnection_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.GetConnectionRequest.fromBuffer(value),
        ($0.Connection value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.RevokeConnectionRequest, $0.Connection>(
        'RevokeConnection',
        revokeConnection_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.RevokeConnectionRequest.fromBuffer(value),
        ($0.Connection value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.DeleteConnectionRequest,
            $0.DeleteConnectionResponse>(
        'DeleteConnection',
        deleteConnection_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.DeleteConnectionRequest.fromBuffer(value),
        ($0.DeleteConnectionResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListIntegrationToolsRequest,
            $0.ListIntegrationToolsResponse>(
        'ListIntegrationTools',
        listIntegrationTools_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListIntegrationToolsRequest.fromBuffer(value),
        ($0.ListIntegrationToolsResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.CallIntegrationToolRequest,
            $0.CallIntegrationToolResponse>(
        'CallIntegrationTool',
        callIntegrationTool_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.CallIntegrationToolRequest.fromBuffer(value),
        ($0.CallIntegrationToolResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.ListProvidersResponse> listProviders_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListProvidersRequest> $request) async {
    return listProviders($call, await $request);
  }

  $async.Future<$0.ListProvidersResponse> listProviders(
      $grpc.ServiceCall call, $0.ListProvidersRequest request);

  $async.Future<$0.Connection> connectAccount_Pre($grpc.ServiceCall $call,
      $async.Future<$0.ConnectAccountRequest> $request) async {
    return connectAccount($call, await $request);
  }

  $async.Future<$0.Connection> connectAccount(
      $grpc.ServiceCall call, $0.ConnectAccountRequest request);

  $async.Future<$0.ListConnectionsResponse> listConnections_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListConnectionsRequest> $request) async {
    return listConnections($call, await $request);
  }

  $async.Future<$0.ListConnectionsResponse> listConnections(
      $grpc.ServiceCall call, $0.ListConnectionsRequest request);

  $async.Future<$0.Connection> getConnection_Pre($grpc.ServiceCall $call,
      $async.Future<$0.GetConnectionRequest> $request) async {
    return getConnection($call, await $request);
  }

  $async.Future<$0.Connection> getConnection(
      $grpc.ServiceCall call, $0.GetConnectionRequest request);

  $async.Future<$0.Connection> revokeConnection_Pre($grpc.ServiceCall $call,
      $async.Future<$0.RevokeConnectionRequest> $request) async {
    return revokeConnection($call, await $request);
  }

  $async.Future<$0.Connection> revokeConnection(
      $grpc.ServiceCall call, $0.RevokeConnectionRequest request);

  $async.Future<$0.DeleteConnectionResponse> deleteConnection_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.DeleteConnectionRequest> $request) async {
    return deleteConnection($call, await $request);
  }

  $async.Future<$0.DeleteConnectionResponse> deleteConnection(
      $grpc.ServiceCall call, $0.DeleteConnectionRequest request);

  $async.Future<$0.ListIntegrationToolsResponse> listIntegrationTools_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListIntegrationToolsRequest> $request) async {
    return listIntegrationTools($call, await $request);
  }

  $async.Future<$0.ListIntegrationToolsResponse> listIntegrationTools(
      $grpc.ServiceCall call, $0.ListIntegrationToolsRequest request);

  $async.Future<$0.CallIntegrationToolResponse> callIntegrationTool_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.CallIntegrationToolRequest> $request) async {
    return callIntegrationTool($call, await $request);
  }

  $async.Future<$0.CallIntegrationToolResponse> callIntegrationTool(
      $grpc.ServiceCall call, $0.CallIntegrationToolRequest request);
}
