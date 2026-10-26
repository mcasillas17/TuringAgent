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

/// Like MemoryService, one service with two facets split by method name: the
/// public facet manages profiles and reads delegations, and the internal facet,
/// which only the runtime identity reaches, serves the team tool.
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

  $grpc.ResponseFuture<$0.ListTeamToolsResponse> listTeamTools(
    $0.ListTeamToolsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listTeamTools, request, options: options);
  }

  $grpc.ResponseFuture<$0.CallTeamToolResponse> callTeamTool(
    $0.CallTeamToolRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$callTeamTool, request, options: options);
  }

  /// Every task a conversation delegated. NotFound for a conversation that
  /// does not exist, is being deleted, or is a specialist's own.
  $grpc.ResponseFuture<$0.ListDelegationsResponse> listDelegations(
    $0.ListDelegationsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listDelegations, request, options: options);
  }

  /// One delegation. NotFound once its conversation is being deleted.
  $grpc.ResponseFuture<$0.Delegation> getDelegation(
    $0.GetDelegationRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getDelegation, request, options: options);
  }

  /// The disclosure for a task waiting for consent to tools that leave the
  /// machine. FailedPrecondition once it is not waiting.
  $grpc.ResponseFuture<$0.PrepareDelegationEgressResponse>
      prepareDelegationEgress(
    $0.PrepareDelegationEgressRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$prepareDelegationEgress, request,
        options: options);
  }

  /// The user's decision for that one task: a decision bound to the task's
  /// own run, never its turn's.
  $grpc.ResponseFuture<$0.Delegation> decideDelegationEgress(
    $0.DecideDelegationEgressRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$decideDelegationEgress, request,
        options: options);
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
  static final _$listTeamTools =
      $grpc.ClientMethod<$0.ListTeamToolsRequest, $0.ListTeamToolsResponse>(
          '/turing.v1.TeamService/ListTeamTools',
          ($0.ListTeamToolsRequest value) => value.writeToBuffer(),
          $0.ListTeamToolsResponse.fromBuffer);
  static final _$callTeamTool =
      $grpc.ClientMethod<$0.CallTeamToolRequest, $0.CallTeamToolResponse>(
          '/turing.v1.TeamService/CallTeamTool',
          ($0.CallTeamToolRequest value) => value.writeToBuffer(),
          $0.CallTeamToolResponse.fromBuffer);
  static final _$listDelegations =
      $grpc.ClientMethod<$0.ListDelegationsRequest, $0.ListDelegationsResponse>(
          '/turing.v1.TeamService/ListDelegations',
          ($0.ListDelegationsRequest value) => value.writeToBuffer(),
          $0.ListDelegationsResponse.fromBuffer);
  static final _$getDelegation =
      $grpc.ClientMethod<$0.GetDelegationRequest, $0.Delegation>(
          '/turing.v1.TeamService/GetDelegation',
          ($0.GetDelegationRequest value) => value.writeToBuffer(),
          $0.Delegation.fromBuffer);
  static final _$prepareDelegationEgress = $grpc.ClientMethod<
          $0.PrepareDelegationEgressRequest,
          $0.PrepareDelegationEgressResponse>(
      '/turing.v1.TeamService/PrepareDelegationEgress',
      ($0.PrepareDelegationEgressRequest value) => value.writeToBuffer(),
      $0.PrepareDelegationEgressResponse.fromBuffer);
  static final _$decideDelegationEgress =
      $grpc.ClientMethod<$0.DecideDelegationEgressRequest, $0.Delegation>(
          '/turing.v1.TeamService/DecideDelegationEgress',
          ($0.DecideDelegationEgressRequest value) => value.writeToBuffer(),
          $0.Delegation.fromBuffer);
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
    $addMethod(
        $grpc.ServiceMethod<$0.ListTeamToolsRequest, $0.ListTeamToolsResponse>(
            'ListTeamTools',
            listTeamTools_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.ListTeamToolsRequest.fromBuffer(value),
            ($0.ListTeamToolsResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.CallTeamToolRequest, $0.CallTeamToolResponse>(
            'CallTeamTool',
            callTeamTool_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.CallTeamToolRequest.fromBuffer(value),
            ($0.CallTeamToolResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListDelegationsRequest,
            $0.ListDelegationsResponse>(
        'ListDelegations',
        listDelegations_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListDelegationsRequest.fromBuffer(value),
        ($0.ListDelegationsResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.GetDelegationRequest, $0.Delegation>(
        'GetDelegation',
        getDelegation_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.GetDelegationRequest.fromBuffer(value),
        ($0.Delegation value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.PrepareDelegationEgressRequest,
            $0.PrepareDelegationEgressResponse>(
        'PrepareDelegationEgress',
        prepareDelegationEgress_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.PrepareDelegationEgressRequest.fromBuffer(value),
        ($0.PrepareDelegationEgressResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.DecideDelegationEgressRequest, $0.Delegation>(
            'DecideDelegationEgress',
            decideDelegationEgress_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.DecideDelegationEgressRequest.fromBuffer(value),
            ($0.Delegation value) => value.writeToBuffer()));
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

  $async.Future<$0.ListTeamToolsResponse> listTeamTools_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListTeamToolsRequest> $request) async {
    return listTeamTools($call, await $request);
  }

  $async.Future<$0.ListTeamToolsResponse> listTeamTools(
      $grpc.ServiceCall call, $0.ListTeamToolsRequest request);

  $async.Future<$0.CallTeamToolResponse> callTeamTool_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.CallTeamToolRequest> $request) async {
    return callTeamTool($call, await $request);
  }

  $async.Future<$0.CallTeamToolResponse> callTeamTool(
      $grpc.ServiceCall call, $0.CallTeamToolRequest request);

  $async.Future<$0.ListDelegationsResponse> listDelegations_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListDelegationsRequest> $request) async {
    return listDelegations($call, await $request);
  }

  $async.Future<$0.ListDelegationsResponse> listDelegations(
      $grpc.ServiceCall call, $0.ListDelegationsRequest request);

  $async.Future<$0.Delegation> getDelegation_Pre($grpc.ServiceCall $call,
      $async.Future<$0.GetDelegationRequest> $request) async {
    return getDelegation($call, await $request);
  }

  $async.Future<$0.Delegation> getDelegation(
      $grpc.ServiceCall call, $0.GetDelegationRequest request);

  $async.Future<$0.PrepareDelegationEgressResponse> prepareDelegationEgress_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.PrepareDelegationEgressRequest> $request) async {
    return prepareDelegationEgress($call, await $request);
  }

  $async.Future<$0.PrepareDelegationEgressResponse> prepareDelegationEgress(
      $grpc.ServiceCall call, $0.PrepareDelegationEgressRequest request);

  $async.Future<$0.Delegation> decideDelegationEgress_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.DecideDelegationEgressRequest> $request) async {
    return decideDelegationEgress($call, await $request);
  }

  $async.Future<$0.Delegation> decideDelegationEgress(
      $grpc.ServiceCall call, $0.DecideDelegationEgressRequest request);
}
