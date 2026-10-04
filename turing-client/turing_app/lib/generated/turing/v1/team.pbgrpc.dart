// This is a generated file - do not edit.
//
// Generated from turing/v1/team.proto.

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

import 'team.pb.dart' as $0;

export 'team.pb.dart';

@$pb.GrpcServiceName('turing.v1.TeamService')
class TeamServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  TeamServiceClient(super.channel, {super.options, super.interceptors});

  $grpc.ResponseFuture<$0.ListAgentProfilesResponse> listAgentProfiles(
    $0.ListAgentProfilesRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listAgentProfiles, request, options: options);
  }

  $grpc.ResponseFuture<$0.AgentProfile> setAgentProfileEnabled(
    $0.SetAgentProfileEnabledRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$setAgentProfileEnabled, request,
        options: options);
  }

  $grpc.ResponseFuture<$0.AgentProfile> grantAgentProfile(
    $0.GrantAgentProfileRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$grantAgentProfile, request, options: options);
  }

  // method descriptors

  static final _$listAgentProfiles = $grpc.ClientMethod<
          $0.ListAgentProfilesRequest, $0.ListAgentProfilesResponse>(
      '/turing.v1.TeamService/ListAgentProfiles',
      ($0.ListAgentProfilesRequest value) => value.writeToBuffer(),
      $0.ListAgentProfilesResponse.fromBuffer);
  static final _$setAgentProfileEnabled =
      $grpc.ClientMethod<$0.SetAgentProfileEnabledRequest, $0.AgentProfile>(
          '/turing.v1.TeamService/SetAgentProfileEnabled',
          ($0.SetAgentProfileEnabledRequest value) => value.writeToBuffer(),
          $0.AgentProfile.fromBuffer);
  static final _$grantAgentProfile =
      $grpc.ClientMethod<$0.GrantAgentProfileRequest, $0.AgentProfile>(
          '/turing.v1.TeamService/GrantAgentProfile',
          ($0.GrantAgentProfileRequest value) => value.writeToBuffer(),
          $0.AgentProfile.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.TeamService')
abstract class TeamServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.TeamService';

  TeamServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.ListAgentProfilesRequest,
            $0.ListAgentProfilesResponse>(
        'ListAgentProfiles',
        listAgentProfiles_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListAgentProfilesRequest.fromBuffer(value),
        ($0.ListAgentProfilesResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.SetAgentProfileEnabledRequest, $0.AgentProfile>(
            'SetAgentProfileEnabled',
            setAgentProfileEnabled_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.SetAgentProfileEnabledRequest.fromBuffer(value),
            ($0.AgentProfile value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.GrantAgentProfileRequest, $0.AgentProfile>(
            'GrantAgentProfile',
            grantAgentProfile_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.GrantAgentProfileRequest.fromBuffer(value),
            ($0.AgentProfile value) => value.writeToBuffer()));
  }

  $async.Future<$0.ListAgentProfilesResponse> listAgentProfiles_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListAgentProfilesRequest> $request) async {
    return listAgentProfiles($call, await $request);
  }

  $async.Future<$0.ListAgentProfilesResponse> listAgentProfiles(
      $grpc.ServiceCall call, $0.ListAgentProfilesRequest request);

  $async.Future<$0.AgentProfile> setAgentProfileEnabled_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.SetAgentProfileEnabledRequest> $request) async {
    return setAgentProfileEnabled($call, await $request);
  }

  $async.Future<$0.AgentProfile> setAgentProfileEnabled(
      $grpc.ServiceCall call, $0.SetAgentProfileEnabledRequest request);

  $async.Future<$0.AgentProfile> grantAgentProfile_Pre($grpc.ServiceCall $call,
      $async.Future<$0.GrantAgentProfileRequest> $request) async {
    return grantAgentProfile($call, await $request);
  }

  $async.Future<$0.AgentProfile> grantAgentProfile(
      $grpc.ServiceCall call, $0.GrantAgentProfileRequest request);
}
