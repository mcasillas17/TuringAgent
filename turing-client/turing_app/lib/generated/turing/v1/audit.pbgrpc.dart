// This is a generated file - do not edit.
//
// Generated from turing/v1/audit.proto.

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

import 'audit.pb.dart' as $0;

export 'audit.pb.dart';

@$pb.GrpcServiceName('turing.v1.AuditService')
class AuditServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  AuditServiceClient(super.channel, {super.options, super.interceptors});

  $grpc.ResponseFuture<$0.ListAuditEntriesResponse> listAuditEntries(
    $0.ListAuditEntriesRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listAuditEntries, request, options: options);
  }

  // method descriptors

  static final _$listAuditEntries = $grpc.ClientMethod<
          $0.ListAuditEntriesRequest, $0.ListAuditEntriesResponse>(
      '/turing.v1.AuditService/ListAuditEntries',
      ($0.ListAuditEntriesRequest value) => value.writeToBuffer(),
      $0.ListAuditEntriesResponse.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.AuditService')
abstract class AuditServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.AuditService';

  AuditServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.ListAuditEntriesRequest,
            $0.ListAuditEntriesResponse>(
        'ListAuditEntries',
        listAuditEntries_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListAuditEntriesRequest.fromBuffer(value),
        ($0.ListAuditEntriesResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.ListAuditEntriesResponse> listAuditEntries_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListAuditEntriesRequest> $request) async {
    return listAuditEntries($call, await $request);
  }

  $async.Future<$0.ListAuditEntriesResponse> listAuditEntries(
      $grpc.ServiceCall call, $0.ListAuditEntriesRequest request);
}
