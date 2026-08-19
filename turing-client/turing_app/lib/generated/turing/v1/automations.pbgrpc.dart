// This is a generated file - do not edit.
//
// Generated from turing/v1/automations.proto.

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

import 'automations.pb.dart' as $0;

export 'automations.pb.dart';

@$pb.GrpcServiceName('turing.v1.AutomationService')
class AutomationServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  AutomationServiceClient(super.channel, {super.options, super.interceptors});

  $grpc.ResponseFuture<$0.Automation> createAutomation(
    $0.CreateAutomationRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$createAutomation, request, options: options);
  }

  $grpc.ResponseFuture<$0.Automation> updateAutomation(
    $0.UpdateAutomationRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$updateAutomation, request, options: options);
  }

  /// Enabling and disabling is its own call rather than a field on update,
  /// because a toggle must not require the caller to resend a prompt and a
  /// schedule it did not intend to change.
  $grpc.ResponseFuture<$0.Automation> setAutomationEnabled(
    $0.SetAutomationEnabledRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$setAutomationEnabled, request, options: options);
  }

  $grpc.ResponseFuture<$0.DeleteAutomationResponse> deleteAutomation(
    $0.DeleteAutomationRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$deleteAutomation, request, options: options);
  }

  $grpc.ResponseFuture<$0.ListAutomationsResponse> listAutomations(
    $0.ListAutomationsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listAutomations, request, options: options);
  }

  // method descriptors

  static final _$createAutomation =
      $grpc.ClientMethod<$0.CreateAutomationRequest, $0.Automation>(
          '/turing.v1.AutomationService/CreateAutomation',
          ($0.CreateAutomationRequest value) => value.writeToBuffer(),
          $0.Automation.fromBuffer);
  static final _$updateAutomation =
      $grpc.ClientMethod<$0.UpdateAutomationRequest, $0.Automation>(
          '/turing.v1.AutomationService/UpdateAutomation',
          ($0.UpdateAutomationRequest value) => value.writeToBuffer(),
          $0.Automation.fromBuffer);
  static final _$setAutomationEnabled =
      $grpc.ClientMethod<$0.SetAutomationEnabledRequest, $0.Automation>(
          '/turing.v1.AutomationService/SetAutomationEnabled',
          ($0.SetAutomationEnabledRequest value) => value.writeToBuffer(),
          $0.Automation.fromBuffer);
  static final _$deleteAutomation = $grpc.ClientMethod<
          $0.DeleteAutomationRequest, $0.DeleteAutomationResponse>(
      '/turing.v1.AutomationService/DeleteAutomation',
      ($0.DeleteAutomationRequest value) => value.writeToBuffer(),
      $0.DeleteAutomationResponse.fromBuffer);
  static final _$listAutomations =
      $grpc.ClientMethod<$0.ListAutomationsRequest, $0.ListAutomationsResponse>(
          '/turing.v1.AutomationService/ListAutomations',
          ($0.ListAutomationsRequest value) => value.writeToBuffer(),
          $0.ListAutomationsResponse.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.AutomationService')
abstract class AutomationServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.AutomationService';

  AutomationServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.CreateAutomationRequest, $0.Automation>(
        'CreateAutomation',
        createAutomation_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.CreateAutomationRequest.fromBuffer(value),
        ($0.Automation value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.UpdateAutomationRequest, $0.Automation>(
        'UpdateAutomation',
        updateAutomation_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.UpdateAutomationRequest.fromBuffer(value),
        ($0.Automation value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.SetAutomationEnabledRequest, $0.Automation>(
            'SetAutomationEnabled',
            setAutomationEnabled_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.SetAutomationEnabledRequest.fromBuffer(value),
            ($0.Automation value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.DeleteAutomationRequest,
            $0.DeleteAutomationResponse>(
        'DeleteAutomation',
        deleteAutomation_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.DeleteAutomationRequest.fromBuffer(value),
        ($0.DeleteAutomationResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListAutomationsRequest,
            $0.ListAutomationsResponse>(
        'ListAutomations',
        listAutomations_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListAutomationsRequest.fromBuffer(value),
        ($0.ListAutomationsResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.Automation> createAutomation_Pre($grpc.ServiceCall $call,
      $async.Future<$0.CreateAutomationRequest> $request) async {
    return createAutomation($call, await $request);
  }

  $async.Future<$0.Automation> createAutomation(
      $grpc.ServiceCall call, $0.CreateAutomationRequest request);

  $async.Future<$0.Automation> updateAutomation_Pre($grpc.ServiceCall $call,
      $async.Future<$0.UpdateAutomationRequest> $request) async {
    return updateAutomation($call, await $request);
  }

  $async.Future<$0.Automation> updateAutomation(
      $grpc.ServiceCall call, $0.UpdateAutomationRequest request);

  $async.Future<$0.Automation> setAutomationEnabled_Pre($grpc.ServiceCall $call,
      $async.Future<$0.SetAutomationEnabledRequest> $request) async {
    return setAutomationEnabled($call, await $request);
  }

  $async.Future<$0.Automation> setAutomationEnabled(
      $grpc.ServiceCall call, $0.SetAutomationEnabledRequest request);

  $async.Future<$0.DeleteAutomationResponse> deleteAutomation_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.DeleteAutomationRequest> $request) async {
    return deleteAutomation($call, await $request);
  }

  $async.Future<$0.DeleteAutomationResponse> deleteAutomation(
      $grpc.ServiceCall call, $0.DeleteAutomationRequest request);

  $async.Future<$0.ListAutomationsResponse> listAutomations_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListAutomationsRequest> $request) async {
    return listAutomations($call, await $request);
  }

  $async.Future<$0.ListAutomationsResponse> listAutomations(
      $grpc.ServiceCall call, $0.ListAutomationsRequest request);
}
