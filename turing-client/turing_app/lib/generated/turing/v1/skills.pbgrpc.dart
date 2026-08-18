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

  $grpc.ResponseFuture<$0.Skill> createSkill(
    $0.CreateSkillRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$createSkill, request, options: options);
  }

  $grpc.ResponseFuture<$0.Skill> updateSkill(
    $0.UpdateSkillRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$updateSkill, request, options: options);
  }

  $grpc.ResponseFuture<$0.DeleteSkillResponse> deleteSkill(
    $0.DeleteSkillRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$deleteSkill, request, options: options);
  }

  $grpc.ResponseFuture<$0.ListSkillsResponse> listSkills(
    $0.ListSkillsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listSkills, request, options: options);
  }

  /// Attach and detach return the conversation's full skill set so a client
  /// never has to guess what the server now believes is attached.
  $grpc.ResponseFuture<$0.SessionSkillsResponse> attachSkill(
    $0.AttachSkillRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$attachSkill, request, options: options);
  }

  $grpc.ResponseFuture<$0.SessionSkillsResponse> detachSkill(
    $0.DetachSkillRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$detachSkill, request, options: options);
  }

  $grpc.ResponseFuture<$0.SessionSkillsResponse> listSessionSkills(
    $0.ListSessionSkillsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listSessionSkills, request, options: options);
  }

  // method descriptors

  static final _$createSkill =
      $grpc.ClientMethod<$0.CreateSkillRequest, $0.Skill>(
          '/turing.v1.SkillService/CreateSkill',
          ($0.CreateSkillRequest value) => value.writeToBuffer(),
          $0.Skill.fromBuffer);
  static final _$updateSkill =
      $grpc.ClientMethod<$0.UpdateSkillRequest, $0.Skill>(
          '/turing.v1.SkillService/UpdateSkill',
          ($0.UpdateSkillRequest value) => value.writeToBuffer(),
          $0.Skill.fromBuffer);
  static final _$deleteSkill =
      $grpc.ClientMethod<$0.DeleteSkillRequest, $0.DeleteSkillResponse>(
          '/turing.v1.SkillService/DeleteSkill',
          ($0.DeleteSkillRequest value) => value.writeToBuffer(),
          $0.DeleteSkillResponse.fromBuffer);
  static final _$listSkills =
      $grpc.ClientMethod<$0.ListSkillsRequest, $0.ListSkillsResponse>(
          '/turing.v1.SkillService/ListSkills',
          ($0.ListSkillsRequest value) => value.writeToBuffer(),
          $0.ListSkillsResponse.fromBuffer);
  static final _$attachSkill =
      $grpc.ClientMethod<$0.AttachSkillRequest, $0.SessionSkillsResponse>(
          '/turing.v1.SkillService/AttachSkill',
          ($0.AttachSkillRequest value) => value.writeToBuffer(),
          $0.SessionSkillsResponse.fromBuffer);
  static final _$detachSkill =
      $grpc.ClientMethod<$0.DetachSkillRequest, $0.SessionSkillsResponse>(
          '/turing.v1.SkillService/DetachSkill',
          ($0.DetachSkillRequest value) => value.writeToBuffer(),
          $0.SessionSkillsResponse.fromBuffer);
  static final _$listSessionSkills =
      $grpc.ClientMethod<$0.ListSessionSkillsRequest, $0.SessionSkillsResponse>(
          '/turing.v1.SkillService/ListSessionSkills',
          ($0.ListSessionSkillsRequest value) => value.writeToBuffer(),
          $0.SessionSkillsResponse.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.SkillService')
abstract class SkillServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.SkillService';

  SkillServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.CreateSkillRequest, $0.Skill>(
        'CreateSkill',
        createSkill_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.CreateSkillRequest.fromBuffer(value),
        ($0.Skill value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.UpdateSkillRequest, $0.Skill>(
        'UpdateSkill',
        updateSkill_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.UpdateSkillRequest.fromBuffer(value),
        ($0.Skill value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.DeleteSkillRequest, $0.DeleteSkillResponse>(
            'DeleteSkill',
            deleteSkill_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.DeleteSkillRequest.fromBuffer(value),
            ($0.DeleteSkillResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListSkillsRequest, $0.ListSkillsResponse>(
        'ListSkills',
        listSkills_Pre,
        false,
        false,
        ($core.List<$core.int> value) => $0.ListSkillsRequest.fromBuffer(value),
        ($0.ListSkillsResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.AttachSkillRequest, $0.SessionSkillsResponse>(
            'AttachSkill',
            attachSkill_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.AttachSkillRequest.fromBuffer(value),
            ($0.SessionSkillsResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.DetachSkillRequest, $0.SessionSkillsResponse>(
            'DetachSkill',
            detachSkill_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.DetachSkillRequest.fromBuffer(value),
            ($0.SessionSkillsResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListSessionSkillsRequest,
            $0.SessionSkillsResponse>(
        'ListSessionSkills',
        listSessionSkills_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListSessionSkillsRequest.fromBuffer(value),
        ($0.SessionSkillsResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.Skill> createSkill_Pre($grpc.ServiceCall $call,
      $async.Future<$0.CreateSkillRequest> $request) async {
    return createSkill($call, await $request);
  }

  $async.Future<$0.Skill> createSkill(
      $grpc.ServiceCall call, $0.CreateSkillRequest request);

  $async.Future<$0.Skill> updateSkill_Pre($grpc.ServiceCall $call,
      $async.Future<$0.UpdateSkillRequest> $request) async {
    return updateSkill($call, await $request);
  }

  $async.Future<$0.Skill> updateSkill(
      $grpc.ServiceCall call, $0.UpdateSkillRequest request);

  $async.Future<$0.DeleteSkillResponse> deleteSkill_Pre($grpc.ServiceCall $call,
      $async.Future<$0.DeleteSkillRequest> $request) async {
    return deleteSkill($call, await $request);
  }

  $async.Future<$0.DeleteSkillResponse> deleteSkill(
      $grpc.ServiceCall call, $0.DeleteSkillRequest request);

  $async.Future<$0.ListSkillsResponse> listSkills_Pre($grpc.ServiceCall $call,
      $async.Future<$0.ListSkillsRequest> $request) async {
    return listSkills($call, await $request);
  }

  $async.Future<$0.ListSkillsResponse> listSkills(
      $grpc.ServiceCall call, $0.ListSkillsRequest request);

  $async.Future<$0.SessionSkillsResponse> attachSkill_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.AttachSkillRequest> $request) async {
    return attachSkill($call, await $request);
  }

  $async.Future<$0.SessionSkillsResponse> attachSkill(
      $grpc.ServiceCall call, $0.AttachSkillRequest request);

  $async.Future<$0.SessionSkillsResponse> detachSkill_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.DetachSkillRequest> $request) async {
    return detachSkill($call, await $request);
  }

  $async.Future<$0.SessionSkillsResponse> detachSkill(
      $grpc.ServiceCall call, $0.DetachSkillRequest request);

  $async.Future<$0.SessionSkillsResponse> listSessionSkills_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListSessionSkillsRequest> $request) async {
    return listSessionSkills($call, await $request);
  }

  $async.Future<$0.SessionSkillsResponse> listSessionSkills(
      $grpc.ServiceCall call, $0.ListSessionSkillsRequest request);
}
