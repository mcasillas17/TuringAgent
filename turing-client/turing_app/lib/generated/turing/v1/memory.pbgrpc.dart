// This is a generated file - do not edit.
//
// Generated from turing/v1/memory.proto.

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

import 'memory.pb.dart' as $0;

export 'memory.pb.dart';

/// The public facet is everything a client needs to read and decide; the
/// internal facet is ListMemoryTools and CallMemoryTool, which the runtime
/// calls over the internal channel to wire and run memory tools dynamically.
/// The split is enforced by method name at the identity layer, so all of these
/// names must stay stable.
@$pb.GrpcServiceName('turing.v1.MemoryService')
class MemoryServiceClient extends $grpc.Client {
  /// The hostname for this service.
  static const $core.String defaultHost = '';

  /// OAuth scopes needed for the client.
  static const $core.List<$core.String> oauthScopes = [
    '',
  ];

  MemoryServiceClient(super.channel, {super.options, super.interceptors});

  $grpc.ResponseFuture<$0.ListMemoryStateResponse> listMemoryState(
    $0.ListMemoryStateRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listMemoryState, request, options: options);
  }

  $grpc.ResponseFuture<$0.MemorySettings> getMemorySettings(
    $0.GetMemorySettingsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getMemorySettings, request, options: options);
  }

  $grpc.ResponseFuture<$0.MemorySettings> setMemoryEnabled(
    $0.SetMemoryEnabledRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$setMemoryEnabled, request, options: options);
  }

  $grpc.ResponseFuture<$0.ListMemoryCandidatesResponse> listMemoryCandidates(
    $0.ListMemoryCandidatesRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listMemoryCandidates, request, options: options);
  }

  $grpc.ResponseFuture<$0.MemoryCandidate> getMemoryCandidate(
    $0.GetMemoryCandidateRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getMemoryCandidate, request, options: options);
  }

  $grpc.ResponseFuture<$0.PromoteMemoryCandidateResponse>
      promoteMemoryCandidate(
    $0.PromoteMemoryCandidateRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$promoteMemoryCandidate, request,
        options: options);
  }

  $grpc.ResponseFuture<$0.RejectMemoryCandidateResponse> rejectMemoryCandidate(
    $0.RejectMemoryCandidateRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$rejectMemoryCandidate, request, options: options);
  }

  $grpc.ResponseFuture<$0.MemoryProfile> getMemoryProfile(
    $0.GetMemoryProfileRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$getMemoryProfile, request, options: options);
  }

  $grpc.ResponseFuture<$0.ApplyMemoryProfileResponse> applyMemoryProfile(
    $0.ApplyMemoryProfileRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$applyMemoryProfile, request, options: options);
  }

  /// Internal facet only.
  $grpc.ResponseFuture<$0.ListMemoryToolsResponse> listMemoryTools(
    $0.ListMemoryToolsRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$listMemoryTools, request, options: options);
  }

  $grpc.ResponseFuture<$0.CallMemoryToolResponse> callMemoryTool(
    $0.CallMemoryToolRequest request, {
    $grpc.CallOptions? options,
  }) {
    return $createUnaryCall(_$callMemoryTool, request, options: options);
  }

  // method descriptors

  static final _$listMemoryState =
      $grpc.ClientMethod<$0.ListMemoryStateRequest, $0.ListMemoryStateResponse>(
          '/turing.v1.MemoryService/ListMemoryState',
          ($0.ListMemoryStateRequest value) => value.writeToBuffer(),
          $0.ListMemoryStateResponse.fromBuffer);
  static final _$getMemorySettings =
      $grpc.ClientMethod<$0.GetMemorySettingsRequest, $0.MemorySettings>(
          '/turing.v1.MemoryService/GetMemorySettings',
          ($0.GetMemorySettingsRequest value) => value.writeToBuffer(),
          $0.MemorySettings.fromBuffer);
  static final _$setMemoryEnabled =
      $grpc.ClientMethod<$0.SetMemoryEnabledRequest, $0.MemorySettings>(
          '/turing.v1.MemoryService/SetMemoryEnabled',
          ($0.SetMemoryEnabledRequest value) => value.writeToBuffer(),
          $0.MemorySettings.fromBuffer);
  static final _$listMemoryCandidates = $grpc.ClientMethod<
          $0.ListMemoryCandidatesRequest, $0.ListMemoryCandidatesResponse>(
      '/turing.v1.MemoryService/ListMemoryCandidates',
      ($0.ListMemoryCandidatesRequest value) => value.writeToBuffer(),
      $0.ListMemoryCandidatesResponse.fromBuffer);
  static final _$getMemoryCandidate =
      $grpc.ClientMethod<$0.GetMemoryCandidateRequest, $0.MemoryCandidate>(
          '/turing.v1.MemoryService/GetMemoryCandidate',
          ($0.GetMemoryCandidateRequest value) => value.writeToBuffer(),
          $0.MemoryCandidate.fromBuffer);
  static final _$promoteMemoryCandidate = $grpc.ClientMethod<
          $0.PromoteMemoryCandidateRequest, $0.PromoteMemoryCandidateResponse>(
      '/turing.v1.MemoryService/PromoteMemoryCandidate',
      ($0.PromoteMemoryCandidateRequest value) => value.writeToBuffer(),
      $0.PromoteMemoryCandidateResponse.fromBuffer);
  static final _$rejectMemoryCandidate = $grpc.ClientMethod<
          $0.RejectMemoryCandidateRequest, $0.RejectMemoryCandidateResponse>(
      '/turing.v1.MemoryService/RejectMemoryCandidate',
      ($0.RejectMemoryCandidateRequest value) => value.writeToBuffer(),
      $0.RejectMemoryCandidateResponse.fromBuffer);
  static final _$getMemoryProfile =
      $grpc.ClientMethod<$0.GetMemoryProfileRequest, $0.MemoryProfile>(
          '/turing.v1.MemoryService/GetMemoryProfile',
          ($0.GetMemoryProfileRequest value) => value.writeToBuffer(),
          $0.MemoryProfile.fromBuffer);
  static final _$applyMemoryProfile = $grpc.ClientMethod<
          $0.ApplyMemoryProfileRequest, $0.ApplyMemoryProfileResponse>(
      '/turing.v1.MemoryService/ApplyMemoryProfile',
      ($0.ApplyMemoryProfileRequest value) => value.writeToBuffer(),
      $0.ApplyMemoryProfileResponse.fromBuffer);
  static final _$listMemoryTools =
      $grpc.ClientMethod<$0.ListMemoryToolsRequest, $0.ListMemoryToolsResponse>(
          '/turing.v1.MemoryService/ListMemoryTools',
          ($0.ListMemoryToolsRequest value) => value.writeToBuffer(),
          $0.ListMemoryToolsResponse.fromBuffer);
  static final _$callMemoryTool =
      $grpc.ClientMethod<$0.CallMemoryToolRequest, $0.CallMemoryToolResponse>(
          '/turing.v1.MemoryService/CallMemoryTool',
          ($0.CallMemoryToolRequest value) => value.writeToBuffer(),
          $0.CallMemoryToolResponse.fromBuffer);
}

@$pb.GrpcServiceName('turing.v1.MemoryService')
abstract class MemoryServiceBase extends $grpc.Service {
  $core.String get $name => 'turing.v1.MemoryService';

  MemoryServiceBase() {
    $addMethod($grpc.ServiceMethod<$0.ListMemoryStateRequest,
            $0.ListMemoryStateResponse>(
        'ListMemoryState',
        listMemoryState_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListMemoryStateRequest.fromBuffer(value),
        ($0.ListMemoryStateResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.GetMemorySettingsRequest, $0.MemorySettings>(
            'GetMemorySettings',
            getMemorySettings_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.GetMemorySettingsRequest.fromBuffer(value),
            ($0.MemorySettings value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.SetMemoryEnabledRequest, $0.MemorySettings>(
            'SetMemoryEnabled',
            setMemoryEnabled_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.SetMemoryEnabledRequest.fromBuffer(value),
            ($0.MemorySettings value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListMemoryCandidatesRequest,
            $0.ListMemoryCandidatesResponse>(
        'ListMemoryCandidates',
        listMemoryCandidates_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListMemoryCandidatesRequest.fromBuffer(value),
        ($0.ListMemoryCandidatesResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.GetMemoryCandidateRequest, $0.MemoryCandidate>(
            'GetMemoryCandidate',
            getMemoryCandidate_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.GetMemoryCandidateRequest.fromBuffer(value),
            ($0.MemoryCandidate value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.PromoteMemoryCandidateRequest,
            $0.PromoteMemoryCandidateResponse>(
        'PromoteMemoryCandidate',
        promoteMemoryCandidate_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.PromoteMemoryCandidateRequest.fromBuffer(value),
        ($0.PromoteMemoryCandidateResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.RejectMemoryCandidateRequest,
            $0.RejectMemoryCandidateResponse>(
        'RejectMemoryCandidate',
        rejectMemoryCandidate_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.RejectMemoryCandidateRequest.fromBuffer(value),
        ($0.RejectMemoryCandidateResponse value) => value.writeToBuffer()));
    $addMethod(
        $grpc.ServiceMethod<$0.GetMemoryProfileRequest, $0.MemoryProfile>(
            'GetMemoryProfile',
            getMemoryProfile_Pre,
            false,
            false,
            ($core.List<$core.int> value) =>
                $0.GetMemoryProfileRequest.fromBuffer(value),
            ($0.MemoryProfile value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ApplyMemoryProfileRequest,
            $0.ApplyMemoryProfileResponse>(
        'ApplyMemoryProfile',
        applyMemoryProfile_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ApplyMemoryProfileRequest.fromBuffer(value),
        ($0.ApplyMemoryProfileResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.ListMemoryToolsRequest,
            $0.ListMemoryToolsResponse>(
        'ListMemoryTools',
        listMemoryTools_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.ListMemoryToolsRequest.fromBuffer(value),
        ($0.ListMemoryToolsResponse value) => value.writeToBuffer()));
    $addMethod($grpc.ServiceMethod<$0.CallMemoryToolRequest,
            $0.CallMemoryToolResponse>(
        'CallMemoryTool',
        callMemoryTool_Pre,
        false,
        false,
        ($core.List<$core.int> value) =>
            $0.CallMemoryToolRequest.fromBuffer(value),
        ($0.CallMemoryToolResponse value) => value.writeToBuffer()));
  }

  $async.Future<$0.ListMemoryStateResponse> listMemoryState_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListMemoryStateRequest> $request) async {
    return listMemoryState($call, await $request);
  }

  $async.Future<$0.ListMemoryStateResponse> listMemoryState(
      $grpc.ServiceCall call, $0.ListMemoryStateRequest request);

  $async.Future<$0.MemorySettings> getMemorySettings_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.GetMemorySettingsRequest> $request) async {
    return getMemorySettings($call, await $request);
  }

  $async.Future<$0.MemorySettings> getMemorySettings(
      $grpc.ServiceCall call, $0.GetMemorySettingsRequest request);

  $async.Future<$0.MemorySettings> setMemoryEnabled_Pre($grpc.ServiceCall $call,
      $async.Future<$0.SetMemoryEnabledRequest> $request) async {
    return setMemoryEnabled($call, await $request);
  }

  $async.Future<$0.MemorySettings> setMemoryEnabled(
      $grpc.ServiceCall call, $0.SetMemoryEnabledRequest request);

  $async.Future<$0.ListMemoryCandidatesResponse> listMemoryCandidates_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListMemoryCandidatesRequest> $request) async {
    return listMemoryCandidates($call, await $request);
  }

  $async.Future<$0.ListMemoryCandidatesResponse> listMemoryCandidates(
      $grpc.ServiceCall call, $0.ListMemoryCandidatesRequest request);

  $async.Future<$0.MemoryCandidate> getMemoryCandidate_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.GetMemoryCandidateRequest> $request) async {
    return getMemoryCandidate($call, await $request);
  }

  $async.Future<$0.MemoryCandidate> getMemoryCandidate(
      $grpc.ServiceCall call, $0.GetMemoryCandidateRequest request);

  $async.Future<$0.PromoteMemoryCandidateResponse> promoteMemoryCandidate_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.PromoteMemoryCandidateRequest> $request) async {
    return promoteMemoryCandidate($call, await $request);
  }

  $async.Future<$0.PromoteMemoryCandidateResponse> promoteMemoryCandidate(
      $grpc.ServiceCall call, $0.PromoteMemoryCandidateRequest request);

  $async.Future<$0.RejectMemoryCandidateResponse> rejectMemoryCandidate_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.RejectMemoryCandidateRequest> $request) async {
    return rejectMemoryCandidate($call, await $request);
  }

  $async.Future<$0.RejectMemoryCandidateResponse> rejectMemoryCandidate(
      $grpc.ServiceCall call, $0.RejectMemoryCandidateRequest request);

  $async.Future<$0.MemoryProfile> getMemoryProfile_Pre($grpc.ServiceCall $call,
      $async.Future<$0.GetMemoryProfileRequest> $request) async {
    return getMemoryProfile($call, await $request);
  }

  $async.Future<$0.MemoryProfile> getMemoryProfile(
      $grpc.ServiceCall call, $0.GetMemoryProfileRequest request);

  $async.Future<$0.ApplyMemoryProfileResponse> applyMemoryProfile_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ApplyMemoryProfileRequest> $request) async {
    return applyMemoryProfile($call, await $request);
  }

  $async.Future<$0.ApplyMemoryProfileResponse> applyMemoryProfile(
      $grpc.ServiceCall call, $0.ApplyMemoryProfileRequest request);

  $async.Future<$0.ListMemoryToolsResponse> listMemoryTools_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.ListMemoryToolsRequest> $request) async {
    return listMemoryTools($call, await $request);
  }

  $async.Future<$0.ListMemoryToolsResponse> listMemoryTools(
      $grpc.ServiceCall call, $0.ListMemoryToolsRequest request);

  $async.Future<$0.CallMemoryToolResponse> callMemoryTool_Pre(
      $grpc.ServiceCall $call,
      $async.Future<$0.CallMemoryToolRequest> $request) async {
    return callMemoryTool($call, await $request);
  }

  $async.Future<$0.CallMemoryToolResponse> callMemoryTool(
      $grpc.ServiceCall call, $0.CallMemoryToolRequest request);
}
