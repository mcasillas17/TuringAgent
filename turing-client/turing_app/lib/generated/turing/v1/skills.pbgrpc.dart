// This is a generated file - do not edit.
//
// Generated from turing/v1/skills.proto.

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

import 'skills.pb.dart' as $0;

export 'skills.pb.dart';

@$pb.GrpcServiceName('turing.v1.SkillService')
class SkillServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  SkillServiceClient(super.channel, {super.options, super.interceptors});

  $grpc.ResponseFuture<$0.ListSkillsResponse> listSkills(
    $0.ListSkillsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listSkills, request, options: options);
  }

  $grpc.ResponseFuture<$0.Skill> getSkill(
    $0.GetSkillRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getSkill, request, options: options);
  }

  $grpc.ResponseFuture<$0.Skill> setSkillEnabled(
    $0.SetSkillEnabledRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$setSkillEnabled, request, options: options);
  }

  $grpc.ResponseFuture<$0.Skill> setSkillCapabilityGrant(
    $0.SetSkillCapabilityGrantRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$setSkillCapabilityGrant, request,
        options: options);
  }

  // method descriptors

  static final _$listSkills =
      $grpc.ClientMethod<$0.ListSkillsRequest, $0.ListSkillsResponse>(
          '/turing.v1.SkillService/ListSkills',
          ($0.ListSkillsRequest value) => value.writeToBuffer(),
          $0.ListSkillsResponse.fromBuffer);
  static final _$getSkill = $grpc.ClientMethod<$0.GetSkillRequest, $0.Skill>(
      '/turing.v1.SkillService/GetSkill',
      ($0.GetSkillRequest value) => value.writeToBuffer(),
      $0.Skill.fromBuffer);
  static final _$setSkillEnabled =
      $grpc.ClientMethod<$0.SetSkillEnabledRequest, $0.Skill>(
          '/turing.v1.SkillService/SetSkillEnabled',
          ($0.SetSkillEnabledRequest value) => value.writeToBuffer(),
          $0.Skill.fromBuffer);
  static final _$setSkillCapabilityGrant =
      $grpc.ClientMethod<$0.SetSkillCapabilityGrantRequest, $0.Skill>(
          '/turing.v1.SkillService/SetSkillCapabilityGrant',
          ($0.SetSkillCapabilityGrantRequest value) => value.writeToBuffer(),
          $0.Skill.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.SkillService')
abstract class SkillServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.SkillService';

  SkillServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.ListSkillsRequest, $0.ListSkillsResponse>(
        'ListSkills',
        listSkills_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.ListSkillsRequest.fromBuffer(value),
        ($0.ListSkillsResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.GetSkillRequest, $0.Skill>(
        'GetSkill',
        getSkill_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.GetSkillRequest.fromBuffer(value),
        ($0.Skill value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.SetSkillEnabledRequest, $0.Skill>(
        'SetSkillEnabled',
        setSkillEnabled_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.SetSkillEnabledRequest.fromBuffer(value),
        ($0.Skill value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.SetSkillCapabilityGrantRequest, $0.Skill>(
        'SetSkillCapabilityGrant',
        setSkillCapabilityGrant_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.SetSkillCapabilityGrantRequest.fromBuffer(value),
        ($0.Skill value) => value.writeToBuffer()));
  }

  $async.Future<$0.ListSkillsResponse> listSkills_Pre($grpc.ServiceCall $call,
      $async.Future<$0.ListSkillsRequest> $request) async {
    return listSkills($call, await $request);
  }

  $async.Future<$0.ListSkillsResponse> listSkills(
      $grpc.ServiceCall call, $0.ListSkillsRequest request);

  $async.Future<$0.Skill> getSkill_Pre($grpc.ServiceCall $call,
      $async.Future<$0.GetSkillRequest> $request) async {
    return getSkill($call, await $request);
  }

  $async.Future<$0.Skill> getSkill(
      $grpc.ServiceCall call, $0.GetSkillRequest request);

  $async.Future<$0.Skill> setSkillEnabled_Pre($grpc.ServiceCall $call,
      $async.Future<$0.SetSkillEnabledRequest> $request) async {
    return setSkillEnabled($call, await $request);
  }

  $async.Future<$0.Skill> setSkillEnabled(
      $grpc.ServiceCall call, $0.SetSkillEnabledRequest request);

  $async.Future<$0.Skill> setSkillCapabilityGrant_Pre($grpc.ServiceCall $call,
      $async.Future<$0.SetSkillCapabilityGrantRequest> $request) async {
    return setSkillCapabilityGrant($call, await $request);
  }

  $async.Future<$0.Skill> setSkillCapabilityGrant(
      $grpc.ServiceCall call, $0.SetSkillCapabilityGrantRequest request);
}
