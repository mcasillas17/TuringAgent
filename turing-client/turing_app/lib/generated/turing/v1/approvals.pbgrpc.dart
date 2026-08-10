// This is a generated file - do not edit.
//
// Generated from turing/v1/approvals.proto.

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

import 'approvals.pb.dart' as $0;

export 'approvals.pb.dart';

@$pb.GrpcServiceName('turing.v1.ApprovalService')
class ApprovalServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  ApprovalServiceClient(super.channel, {super.options, super.interceptors});

  $grpc.ResponseFuture<$0.ApprovalResponse> approveApproval(
    $0.ApproveApprovalRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$approveApproval, request, options: options);
  }

  $grpc.ResponseFuture<$0.ApprovalResponse> denyApproval(
    $0.DenyApprovalRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$denyApproval, request, options: options);
  }

  $grpc.ResponseFuture<$0.RuntimeApprovalState> getApprovalForRuntime(
    $0.GetApprovalForRuntimeRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getApprovalForRuntime, request, options: options);
  }

  $grpc.ResponseFuture<$0.ApprovalResponse> consumeApproval(
    $0.ConsumeApprovalRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$consumeApproval, request, options: options);
  }

  // method descriptors

  static final _$approveApproval =
      $grpc.ClientMethod<$0.ApproveApprovalRequest, $0.ApprovalResponse>(
          '/turing.v1.ApprovalService/ApproveApproval',
          ($0.ApproveApprovalRequest value) => value.writeToBuffer(),
          $0.ApprovalResponse.fromBuffer);
  static final _$denyApproval =
      $grpc.ClientMethod<$0.DenyApprovalRequest, $0.ApprovalResponse>(
          '/turing.v1.ApprovalService/DenyApproval',
          ($0.DenyApprovalRequest value) => value.writeToBuffer(),
          $0.ApprovalResponse.fromBuffer);
  static final _$getApprovalForRuntime = $grpc.ClientMethod<
          $0.GetApprovalForRuntimeRequest, $0.RuntimeApprovalState>(
      '/turing.v1.ApprovalService/GetApprovalForRuntime',
      ($0.GetApprovalForRuntimeRequest value) => value.writeToBuffer(),
      $0.RuntimeApprovalState.fromBuffer);
  static final _$consumeApproval =
      $grpc.ClientMethod<$0.ConsumeApprovalRequest, $0.ApprovalResponse>(
          '/turing.v1.ApprovalService/ConsumeApproval',
          ($0.ConsumeApprovalRequest value) => value.writeToBuffer(),
          $0.ApprovalResponse.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.ApprovalService')
abstract class ApprovalServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.ApprovalService';

  ApprovalServiceBase() {
    $addMethod(
        $grpc.ServiceMethod<$0.ApproveApprovalRequest, $0.ApprovalResponse>(
            'ApproveApproval',
            approveApproval_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.ApproveApprovalRequest.fromBuffer(value),
            ($0.ApprovalResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.DenyApprovalRequest, $0.ApprovalResponse>(
        'DenyApproval',
        denyApproval_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.DenyApprovalRequest.fromBuffer(value),
        ($0.ApprovalResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.GetApprovalForRuntimeRequest,
            $0.RuntimeApprovalState>(
        'GetApprovalForRuntime',
        getApprovalForRuntime_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.GetApprovalForRuntimeRequest.fromBuffer(value),
        ($0.RuntimeApprovalState value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.ConsumeApprovalRequest, $0.ApprovalResponse>(
            'ConsumeApproval',
            consumeApproval_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.ConsumeApprovalRequest.fromBuffer(value),
            ($0.ApprovalResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.ApprovalResponse> approveApproval_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ApproveApprovalRequest> $request) async {
    return approveApproval($call, await $request);
  }

  $async.Future<$0.ApprovalResponse> approveApproval(
      $grpc.ServiceCall call, $0.ApproveApprovalRequest request);

  $async.Future<$0.ApprovalResponse> denyApproval_Pre($grpc.ServiceCall $call,
      $async.Future<$0.DenyApprovalRequest> $request) async {
    return denyApproval($call, await $request);
  }

  $async.Future<$0.ApprovalResponse> denyApproval(
      $grpc.ServiceCall call, $0.DenyApprovalRequest request);

  $async.Future<$0.RuntimeApprovalState> getApprovalForRuntime_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.GetApprovalForRuntimeRequest> $request) async {
    return getApprovalForRuntime($call, await $request);
  }

  $async.Future<$0.RuntimeApprovalState> getApprovalForRuntime(
      $grpc.ServiceCall call, $0.GetApprovalForRuntimeRequest request);

  $async.Future<$0.ApprovalResponse> consumeApproval_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ConsumeApprovalRequest> $request) async {
    return consumeApproval($call, await $request);
  }

  $async.Future<$0.ApprovalResponse> consumeApproval(
      $grpc.ServiceCall call, $0.ConsumeApprovalRequest request);
}
