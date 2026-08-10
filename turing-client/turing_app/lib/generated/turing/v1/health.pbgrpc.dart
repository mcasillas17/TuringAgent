// This is a generated file - do not edit.
//
// Generated from turing/v1/health.proto.

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

import 'health.pb.dart' as $0;

export 'health.pb.dart';

@$pb.GrpcServiceName('turing.v1.HealthService')
class HealthServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  HealthServiceClient(super.channel, {super.options, super.interceptors});

  $grpc.ResponseFuture<$0.HealthCheckResponse> check(
    $0.HealthCheckRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$check, request, options: options);
  }

  $grpc.ResponseFuture<$0.VersionResponse> version(
    $0.VersionRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$version, request, options: options);
  }

  // method descriptors

  static final _$check =
      $grpc.ClientMethod<$0.HealthCheckRequest, $0.HealthCheckResponse>(
          '/turing.v1.HealthService/Check',
          ($0.HealthCheckRequest value) => value.writeToBuffer(),
          $0.HealthCheckResponse.fromBuffer);
  static final _$version =
      $grpc.ClientMethod<$0.VersionRequest, $0.VersionResponse>(
          '/turing.v1.HealthService/Version',
          ($0.VersionRequest value) => value.writeToBuffer(),
          $0.VersionResponse.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.HealthService')
abstract class HealthServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.HealthService';

  HealthServiceBase() {
    $addMethod(
        $grpc.ServiceMethod<$0.HealthCheckRequest, $0.HealthCheckResponse>(
            'Check',
            check_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.HealthCheckRequest.fromBuffer(value),
            ($0.HealthCheckResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.VersionRequest, $0.VersionResponse>(
        'Version',
        version_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.VersionRequest.fromBuffer(value),
        ($0.VersionResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.HealthCheckResponse> check_Pre($grpc.ServiceCall $call,
      $async.Future<$0.HealthCheckRequest> $request) async {
    return check($call, await $request);
  }

  $async.Future<$0.HealthCheckResponse> check(
      $grpc.ServiceCall call, $0.HealthCheckRequest request);

  $async.Future<$0.VersionResponse> version_Pre($grpc.ServiceCall $call,
      $async.Future<$0.VersionRequest> $request) async {
    return version($call, await $request);
  }

  $async.Future<$0.VersionResponse> version(
      $grpc.ServiceCall call, $0.VersionRequest request);
}
