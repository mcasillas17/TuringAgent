// This is a generated file - do not edit.
//
// Generated from turing/v1/telemetry.proto.

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

import 'telemetry.pb.dart' as $0;

export 'telemetry.pb.dart';

@$pb.GrpcServiceName('turing.v1.TelemetryService')
class TelemetryServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  TelemetryServiceClient(super.channel, {super.options, super.interceptors});

  /// Read-only, and the only RPC here. There is deliberately no write side:
  /// telemetry is derived from what the rest of the system already records, so
  /// nothing can report a number that no other subsystem produced.
  $grpc.ResponseFuture<$0.GetTelemetrySummaryResponse> getTelemetrySummary(
    $0.GetTelemetrySummaryRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getTelemetrySummary, request, options: options);
  }

  // method descriptors

  static final _$getTelemetrySummary = $grpc.ClientMethod<
          $0.GetTelemetrySummaryRequest, $0.GetTelemetrySummaryResponse>(
      '/turing.v1.TelemetryService/GetTelemetrySummary',
      ($0.GetTelemetrySummaryRequest value) => value.writeToBuffer(),
      $0.GetTelemetrySummaryResponse.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.TelemetryService')
abstract class TelemetryServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.TelemetryService';

  TelemetryServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.GetTelemetrySummaryRequest,
            $0.GetTelemetrySummaryResponse>(
        'GetTelemetrySummary',
        getTelemetrySummary_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.GetTelemetrySummaryRequest.fromBuffer(value),
        ($0.GetTelemetrySummaryResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.GetTelemetrySummaryResponse> getTelemetrySummary_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.GetTelemetrySummaryRequest> $request) async {
    return getTelemetrySummary($call, await $request);
  }

  $async.Future<$0.GetTelemetrySummaryResponse> getTelemetrySummary(
      $grpc.ServiceCall call, $0.GetTelemetrySummaryRequest request);
}
