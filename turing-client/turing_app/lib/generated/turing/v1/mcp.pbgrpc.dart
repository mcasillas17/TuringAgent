// This is a generated file - do not edit.
//
// Generated from turing/v1/mcp.proto.

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

import 'mcp.pb.dart' as $0;

export 'mcp.pb.dart';

@$pb.GrpcServiceName('turing.v1.McpRegistryService')
class McpRegistryServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  McpRegistryServiceClient(super.channel, {super.options, super.interceptors});

  $grpc.ResponseFuture<$0.ListMcpServersResponse> listMcpServers(
    $0.ListMcpServersRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listMcpServers, request, options: options);
  }

  $grpc.ResponseFuture<$0.McpServerDescriptor> setMcpServerEnabled(
    $0.SetMcpServerEnabledRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$setMcpServerEnabled, request, options: options);
  }

  $grpc.ResponseFuture<$0.McpToolDescriptor> updateMcpToolPolicy(
    $0.UpdateMcpToolPolicyRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$updateMcpToolPolicy, request, options: options);
  }

  $grpc.ResponseFuture<$0.McpToolDescriptor> updateToolPolicyByName(
    $0.UpdateToolPolicyByNameRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$updateToolPolicyByName, request,
        options: options);
  }

  $grpc.ResponseFuture<$0.ListPseudoServerToolsResponse> listPseudoServerTools(
    $0.ListPseudoServerToolsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listPseudoServerTools, request, options: options);
  }

  $grpc.ResponseFuture<$0.DeleteMcpServerResponse> deleteMcpServer(
    $0.DeleteMcpServerRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$deleteMcpServer, request, options: options);
  }

  $grpc.ResponseFuture<$0.CallRegisteredMcpToolResponse> callRegisteredMcpTool(
    $0.CallRegisteredMcpToolRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$callRegisteredMcpTool, request, options: options);
  }

  // method descriptors

  static final _$listMcpServers =
      $grpc.ClientMethod<$0.ListMcpServersRequest, $0.ListMcpServersResponse>(
          '/turing.v1.McpRegistryService/ListMcpServers',
          ($0.ListMcpServersRequest value) => value.writeToBuffer(),
          $0.ListMcpServersResponse.fromBuffer);
  static final _$setMcpServerEnabled =
      $grpc.ClientMethod<$0.SetMcpServerEnabledRequest, $0.McpServerDescriptor>(
          '/turing.v1.McpRegistryService/SetMcpServerEnabled',
          ($0.SetMcpServerEnabledRequest value) => value.writeToBuffer(),
          $0.McpServerDescriptor.fromBuffer);
  static final _$updateMcpToolPolicy =
      $grpc.ClientMethod<$0.UpdateMcpToolPolicyRequest, $0.McpToolDescriptor>(
          '/turing.v1.McpRegistryService/UpdateMcpToolPolicy',
          ($0.UpdateMcpToolPolicyRequest value) => value.writeToBuffer(),
          $0.McpToolDescriptor.fromBuffer);
  static final _$updateToolPolicyByName = $grpc.ClientMethod<
          $0.UpdateToolPolicyByNameRequest, $0.McpToolDescriptor>(
      '/turing.v1.McpRegistryService/UpdateToolPolicyByName',
      ($0.UpdateToolPolicyByNameRequest value) => value.writeToBuffer(),
      $0.McpToolDescriptor.fromBuffer);
  static final _$listPseudoServerTools = $grpc.ClientMethod<
          $0.ListPseudoServerToolsRequest, $0.ListPseudoServerToolsResponse>(
      '/turing.v1.McpRegistryService/ListPseudoServerTools',
      ($0.ListPseudoServerToolsRequest value) => value.writeToBuffer(),
      $0.ListPseudoServerToolsResponse.fromBuffer);
  static final _$deleteMcpServer =
      $grpc.ClientMethod<$0.DeleteMcpServerRequest, $0.DeleteMcpServerResponse>(
          '/turing.v1.McpRegistryService/DeleteMcpServer',
          ($0.DeleteMcpServerRequest value) => value.writeToBuffer(),
          $0.DeleteMcpServerResponse.fromBuffer);
  static final _$callRegisteredMcpTool = $grpc.ClientMethod<
          $0.CallRegisteredMcpToolRequest, $0.CallRegisteredMcpToolResponse>(
      '/turing.v1.McpRegistryService/CallRegisteredMcpTool',
      ($0.CallRegisteredMcpToolRequest value) => value.writeToBuffer(),
      $0.CallRegisteredMcpToolResponse.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.McpRegistryService')
abstract class McpRegistryServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.McpRegistryService';

  McpRegistryServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.ListMcpServersRequest,
            $0.ListMcpServersResponse>(
        'ListMcpServers',
        listMcpServers_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListMcpServersRequest.fromBuffer(value),
        ($0.ListMcpServersResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.SetMcpServerEnabledRequest,
            $0.McpServerDescriptor>(
        'SetMcpServerEnabled',
        setMcpServerEnabled_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.SetMcpServerEnabledRequest.fromBuffer(value),
        ($0.McpServerDescriptor value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.UpdateMcpToolPolicyRequest,
            $0.McpToolDescriptor>(
        'UpdateMcpToolPolicy',
        updateMcpToolPolicy_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.UpdateMcpToolPolicyRequest.fromBuffer(value),
        ($0.McpToolDescriptor value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.UpdateToolPolicyByNameRequest,
            $0.McpToolDescriptor>(
        'UpdateToolPolicyByName',
        updateToolPolicyByName_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.UpdateToolPolicyByNameRequest.fromBuffer(value),
        ($0.McpToolDescriptor value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListPseudoServerToolsRequest,
            $0.ListPseudoServerToolsResponse>(
        'ListPseudoServerTools',
        listPseudoServerTools_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListPseudoServerToolsRequest.fromBuffer(value),
        ($0.ListPseudoServerToolsResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.DeleteMcpServerRequest,
            $0.DeleteMcpServerResponse>(
        'DeleteMcpServer',
        deleteMcpServer_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.DeleteMcpServerRequest.fromBuffer(value),
        ($0.DeleteMcpServerResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.CallRegisteredMcpToolRequest,
            $0.CallRegisteredMcpToolResponse>(
        'CallRegisteredMcpTool',
        callRegisteredMcpTool_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.CallRegisteredMcpToolRequest.fromBuffer(value),
        ($0.CallRegisteredMcpToolResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.ListMcpServersResponse> listMcpServers_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListMcpServersRequest> $request) async {
    return listMcpServers($call, await $request);
  }

  $async.Future<$0.ListMcpServersResponse> listMcpServers(
      $grpc.ServiceCall call, $0.ListMcpServersRequest request);

  $async.Future<$0.McpServerDescriptor> setMcpServerEnabled_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.SetMcpServerEnabledRequest> $request) async {
    return setMcpServerEnabled($call, await $request);
  }

  $async.Future<$0.McpServerDescriptor> setMcpServerEnabled(
      $grpc.ServiceCall call, $0.SetMcpServerEnabledRequest request);

  $async.Future<$0.McpToolDescriptor> updateMcpToolPolicy_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.UpdateMcpToolPolicyRequest> $request) async {
    return updateMcpToolPolicy($call, await $request);
  }

  $async.Future<$0.McpToolDescriptor> updateMcpToolPolicy(
      $grpc.ServiceCall call, $0.UpdateMcpToolPolicyRequest request);

  $async.Future<$0.McpToolDescriptor> updateToolPolicyByName_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.UpdateToolPolicyByNameRequest> $request) async {
    return updateToolPolicyByName($call, await $request);
  }

  $async.Future<$0.McpToolDescriptor> updateToolPolicyByName(
      $grpc.ServiceCall call, $0.UpdateToolPolicyByNameRequest request);

  $async.Future<$0.ListPseudoServerToolsResponse> listPseudoServerTools_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListPseudoServerToolsRequest> $request) async {
    return listPseudoServerTools($call, await $request);
  }

  $async.Future<$0.ListPseudoServerToolsResponse> listPseudoServerTools(
      $grpc.ServiceCall call, $0.ListPseudoServerToolsRequest request);

  $async.Future<$0.DeleteMcpServerResponse> deleteMcpServer_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.DeleteMcpServerRequest> $request) async {
    return deleteMcpServer($call, await $request);
  }

  $async.Future<$0.DeleteMcpServerResponse> deleteMcpServer(
      $grpc.ServiceCall call, $0.DeleteMcpServerRequest request);

  $async.Future<$0.CallRegisteredMcpToolResponse> callRegisteredMcpTool_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.CallRegisteredMcpToolRequest> $request) async {
    return callRegisteredMcpTool($call, await $request);
  }

  $async.Future<$0.CallRegisteredMcpToolResponse> callRegisteredMcpTool(
      $grpc.ServiceCall call, $0.CallRegisteredMcpToolRequest request);
}
