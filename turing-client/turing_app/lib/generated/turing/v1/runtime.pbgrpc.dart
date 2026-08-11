// This is a generated file - do not edit.
//
// Generated from turing/v1/runtime.proto.

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

import 'runtime.pb.dart' as $0;

export 'runtime.pb.dart';

@$pb.GrpcServiceName('turing.v1.RuntimeService')
class RuntimeServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  RuntimeServiceClient(super.channel, {super.options, super.interceptors});

  $grpc.ResponseStream<$0.RuntimeCommand> connectWorker(
    $async.Stream<$0.RuntimeUpdate> request, {
    $grpc.CallOptions? options,
  }) {
    return $createStreamingCall(_$connectWorker, request, options: options);
  }

  // method descriptors

  static final _$connectWorker =
      $grpc.ClientMethod<$0.RuntimeUpdate, $0.RuntimeCommand>(
          '/turing.v1.RuntimeService/ConnectWorker',
          ($0.RuntimeUpdate value) => value.writeToBuffer(),
          $0.RuntimeCommand.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.RuntimeService')
abstract class RuntimeServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.RuntimeService';

  RuntimeServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.RuntimeUpdate, $0.RuntimeCommand>(
        'ConnectWorker',
        connectWorker,
        true,
        true,
        ($core.List<$core.int> value) => $0.RuntimeUpdate.fromBuffer(value),
        ($0.RuntimeCommand value) => value.writeToBuffer()));
  }

  $async.Stream<$0.RuntimeCommand> connectWorker(
      $grpc.ServiceCall call, $async.Stream<$0.RuntimeUpdate> request);
}
