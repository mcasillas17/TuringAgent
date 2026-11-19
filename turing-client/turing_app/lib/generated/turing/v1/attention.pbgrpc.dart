// This is a generated file - do not edit.
//
// Generated from turing/v1/attention.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:async' as $async;
import 'dart:core' as $core;

import 'package:grpc/service_api.dart' as $grpc;
import 'package:protobuf/protobuf.dart' as $pb;

import 'attention.pb.dart' as $0;

export 'attention.pb.dart';

/// The owner's Needs-you queue. Public only: the runtime and every internal
/// identity are refused.
@$pb.GrpcServiceName('turing.v1.AttentionService')
class AttentionServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  AttentionServiceClient(super.channel, {super.options, super.interceptors});

  $grpc.ResponseFuture<$0.ListAttentionResponse> listAttention(
    $0.ListAttentionRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listAttention, request, options: options);
  }

  /// Hides one automation's failing occurrence until a newer one. Approvals,
  /// consents and deletions cannot be dismissed.
  $grpc.ResponseFuture<$0.DismissAttentionResponse> dismissAttention(
    $0.DismissAttentionRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$dismissAttention, request, options: options);
  }

  // method descriptors

  static final _$listAttention =
      $grpc.ClientMethod<$0.ListAttentionRequest, $0.ListAttentionResponse>(
          '/turing.v1.AttentionService/ListAttention',
          ($0.ListAttentionRequest value) => value.writeToBuffer(),
          $0.ListAttentionResponse.fromBuffer);
  static final _$dismissAttention = $grpc.ClientMethod<
          $0.DismissAttentionRequest, $0.DismissAttentionResponse>(
      '/turing.v1.AttentionService/DismissAttention',
      ($0.DismissAttentionRequest value) => value.writeToBuffer(),
      $0.DismissAttentionResponse.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.AttentionService')
abstract class AttentionServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.AttentionService';

  AttentionServiceBase() {
    $addMethod(
        $grpc.ServiceMethod<$0.ListAttentionRequest, $0.ListAttentionResponse>(
            'ListAttention',
            listAttention_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.ListAttentionRequest.fromBuffer(value),
            ($0.ListAttentionResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.DismissAttentionRequest,
            $0.DismissAttentionResponse>(
        'DismissAttention',
        dismissAttention_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.DismissAttentionRequest.fromBuffer(value),
        ($0.DismissAttentionResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.ListAttentionResponse> listAttention_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListAttentionRequest> $request) async {
    return listAttention($call, await $request);
  }

  $async.Future<$0.ListAttentionResponse> listAttention(
      $grpc.ServiceCall call, $0.ListAttentionRequest request);

  $async.Future<$0.DismissAttentionResponse> dismissAttention_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.DismissAttentionRequest> $request) async {
    return dismissAttention($call, await $request);
  }

  $async.Future<$0.DismissAttentionResponse> dismissAttention(
      $grpc.ServiceCall call, $0.DismissAttentionRequest request);
}
