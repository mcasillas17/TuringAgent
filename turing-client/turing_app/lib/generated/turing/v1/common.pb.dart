// This is a generated file - do not edit.
//
// Generated from turing/v1/common.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;

import '../../google/protobuf/struct.pb.dart' as $1;
import '../../google/protobuf/timestamp.pb.dart' as $0;
import 'common.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'common.pbenum.dart';

class RunState extends $pb.GeneratedMessage {
  factory RunState({
    $core.String? runId,
    $core.String? userMessageId,
    $core.String? assistantMessageId,
    RunLifecycle? lifecycle,
    RunOutcomeReason? outcomeReason,
    $fixnum.Int64? stateVersion,
    $0.Timestamp? stateUpdatedAt,
    $0.Timestamp? finishedAt,
    $core.bool? hasDisplayableContent,
  }) {
    final result = create();
    if (runId != null) result.runId = runId;
    if (userMessageId != null) result.userMessageId = userMessageId;
    if (assistantMessageId != null)
      result.assistantMessageId = assistantMessageId;
    if (lifecycle != null) result.lifecycle = lifecycle;
    if (outcomeReason != null) result.outcomeReason = outcomeReason;
    if (stateVersion != null) result.stateVersion = stateVersion;
    if (stateUpdatedAt != null) result.stateUpdatedAt = stateUpdatedAt;
    if (finishedAt != null) result.finishedAt = finishedAt;
    if (hasDisplayableContent != null)
      result.hasDisplayableContent = hasDisplayableContent;
    return result;
  }

  RunState._();

  factory RunState.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RunState.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RunState',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'runId')
    ..aOS(2, _omitFieldNames ? '' : 'userMessageId')
    ..aOS(3, _omitFieldNames ? '' : 'assistantMessageId')
    ..e<RunLifecycle>(4, _omitFieldNames ? '' : 'lifecycle', $pb.PbFieldType.OE,
        defaultOrMaker: RunLifecycle.RUN_LIFECYCLE_UNSPECIFIED,
        valueOf: RunLifecycle.valueOf,
        enumValues: RunLifecycle.values)
    ..e<RunOutcomeReason>(
        5, _omitFieldNames ? '' : 'outcomeReason', $pb.PbFieldType.OE,
        defaultOrMaker: RunOutcomeReason.RUN_OUTCOME_REASON_UNSPECIFIED,
        valueOf: RunOutcomeReason.valueOf,
        enumValues: RunOutcomeReason.values)
    ..aInt64(6, _omitFieldNames ? '' : 'stateVersion')
    ..aOM<$0.Timestamp>(7, _omitFieldNames ? '' : 'stateUpdatedAt',
        subBuilder: $0.Timestamp.create)
    ..aOM<$0.Timestamp>(8, _omitFieldNames ? '' : 'finishedAt',
        subBuilder: $0.Timestamp.create)
    ..aOB(9, _omitFieldNames ? '' : 'hasDisplayableContent')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunState clone() => RunState()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunState copyWith(void Function(RunState) updates) =>
      super.copyWith((message) => updates(message as RunState)) as RunState;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RunState create() => RunState._();
  @$core.override
  RunState createEmptyInstance() => create();
  static $pb.PbList<RunState> createRepeated() => $pb.PbList<RunState>();
  @$core.pragma('dart2js:noInline')
  static RunState getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<RunState>(create);
  static RunState? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get runId => $_getSZ(0);
  @$pb.TagNumber(1)
  set runId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRunId() => $_has(0);
  @$pb.TagNumber(1)
  void clearRunId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get userMessageId => $_getSZ(1);
  @$pb.TagNumber(2)
  set userMessageId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasUserMessageId() => $_has(1);
  @$pb.TagNumber(2)
  void clearUserMessageId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get assistantMessageId => $_getSZ(2);
  @$pb.TagNumber(3)
  set assistantMessageId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasAssistantMessageId() => $_has(2);
  @$pb.TagNumber(3)
  void clearAssistantMessageId() => $_clearField(3);

  @$pb.TagNumber(4)
  RunLifecycle get lifecycle => $_getN(3);
  @$pb.TagNumber(4)
  set lifecycle(RunLifecycle value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasLifecycle() => $_has(3);
  @$pb.TagNumber(4)
  void clearLifecycle() => $_clearField(4);

  @$pb.TagNumber(5)
  RunOutcomeReason get outcomeReason => $_getN(4);
  @$pb.TagNumber(5)
  set outcomeReason(RunOutcomeReason value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasOutcomeReason() => $_has(4);
  @$pb.TagNumber(5)
  void clearOutcomeReason() => $_clearField(5);

  @$pb.TagNumber(6)
  $fixnum.Int64 get stateVersion => $_getI64(5);
  @$pb.TagNumber(6)
  set stateVersion($fixnum.Int64 value) => $_setInt64(5, value);
  @$pb.TagNumber(6)
  $core.bool hasStateVersion() => $_has(5);
  @$pb.TagNumber(6)
  void clearStateVersion() => $_clearField(6);

  @$pb.TagNumber(7)
  $0.Timestamp get stateUpdatedAt => $_getN(6);
  @$pb.TagNumber(7)
  set stateUpdatedAt($0.Timestamp value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasStateUpdatedAt() => $_has(6);
  @$pb.TagNumber(7)
  void clearStateUpdatedAt() => $_clearField(7);
  @$pb.TagNumber(7)
  $0.Timestamp ensureStateUpdatedAt() => $_ensure(6);

  @$pb.TagNumber(8)
  $0.Timestamp get finishedAt => $_getN(7);
  @$pb.TagNumber(8)
  set finishedAt($0.Timestamp value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasFinishedAt() => $_has(7);
  @$pb.TagNumber(8)
  void clearFinishedAt() => $_clearField(8);
  @$pb.TagNumber(8)
  $0.Timestamp ensureFinishedAt() => $_ensure(7);

  @$pb.TagNumber(9)
  $core.bool get hasDisplayableContent => $_getBF(8);
  @$pb.TagNumber(9)
  set hasDisplayableContent($core.bool value) => $_setBool(8, value);
  @$pb.TagNumber(9)
  $core.bool hasHasDisplayableContent() => $_has(8);
  @$pb.TagNumber(9)
  void clearHasDisplayableContent() => $_clearField(9);
}

class RequestMetadata extends $pb.GeneratedMessage {
  factory RequestMetadata({
    $core.String? requestId,
  }) {
    final result = create();
    if (requestId != null) result.requestId = requestId;
    return result;
  }

  RequestMetadata._();

  factory RequestMetadata.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RequestMetadata.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RequestMetadata',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'requestId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RequestMetadata clone() => RequestMetadata()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RequestMetadata copyWith(void Function(RequestMetadata) updates) =>
      super.copyWith((message) => updates(message as RequestMetadata))
          as RequestMetadata;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RequestMetadata create() => RequestMetadata._();
  @$core.override
  RequestMetadata createEmptyInstance() => create();
  static $pb.PbList<RequestMetadata> createRepeated() =>
      $pb.PbList<RequestMetadata>();
  @$core.pragma('dart2js:noInline')
  static RequestMetadata getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RequestMetadata>(create);
  static RequestMetadata? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get requestId => $_getSZ(0);
  @$pb.TagNumber(1)
  set requestId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasRequestId() => $_has(0);
  @$pb.TagNumber(1)
  void clearRequestId() => $_clearField(1);
}

class PageRequest extends $pb.GeneratedMessage {
  factory PageRequest({
    $core.int? limit,
    $core.String? cursor,
  }) {
    final result = create();
    if (limit != null) result.limit = limit;
    if (cursor != null) result.cursor = cursor;
    return result;
  }

  PageRequest._();

  factory PageRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory PageRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'PageRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..a<$core.int>(1, _omitFieldNames ? '' : 'limit', $pb.PbFieldType.O3)
    ..aOS(2, _omitFieldNames ? '' : 'cursor')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PageRequest clone() => PageRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PageRequest copyWith(void Function(PageRequest) updates) =>
      super.copyWith((message) => updates(message as PageRequest))
          as PageRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static PageRequest create() => PageRequest._();
  @$core.override
  PageRequest createEmptyInstance() => create();
  static $pb.PbList<PageRequest> createRepeated() => $pb.PbList<PageRequest>();
  @$core.pragma('dart2js:noInline')
  static PageRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<PageRequest>(create);
  static PageRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.int get limit => $_getIZ(0);
  @$pb.TagNumber(1)
  set limit($core.int value) => $_setSignedInt32(0, value);
  @$pb.TagNumber(1)
  $core.bool hasLimit() => $_has(0);
  @$pb.TagNumber(1)
  void clearLimit() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get cursor => $_getSZ(1);
  @$pb.TagNumber(2)
  set cursor($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasCursor() => $_has(1);
  @$pb.TagNumber(2)
  void clearCursor() => $_clearField(2);
}

class PageResponse extends $pb.GeneratedMessage {
  factory PageResponse({
    $core.String? nextCursor,
  }) {
    final result = create();
    if (nextCursor != null) result.nextCursor = nextCursor;
    return result;
  }

  PageResponse._();

  factory PageResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory PageResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'PageResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'nextCursor')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PageResponse clone() => PageResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  PageResponse copyWith(void Function(PageResponse) updates) =>
      super.copyWith((message) => updates(message as PageResponse))
          as PageResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static PageResponse create() => PageResponse._();
  @$core.override
  PageResponse createEmptyInstance() => create();
  static $pb.PbList<PageResponse> createRepeated() =>
      $pb.PbList<PageResponse>();
  @$core.pragma('dart2js:noInline')
  static PageResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<PageResponse>(create);
  static PageResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get nextCursor => $_getSZ(0);
  @$pb.TagNumber(1)
  set nextCursor($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasNextCursor() => $_has(0);
  @$pb.TagNumber(1)
  void clearNextCursor() => $_clearField(1);
}

class ErrorDetail extends $pb.GeneratedMessage {
  factory ErrorDetail({
    $core.String? code,
    $core.String? message,
    $core.String? requestId,
    $1.Struct? details,
  }) {
    final result = create();
    if (code != null) result.code = code;
    if (message != null) result.message = message;
    if (requestId != null) result.requestId = requestId;
    if (details != null) result.details = details;
    return result;
  }

  ErrorDetail._();

  factory ErrorDetail.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ErrorDetail.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ErrorDetail',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'code')
    ..aOS(2, _omitFieldNames ? '' : 'message')
    ..aOS(3, _omitFieldNames ? '' : 'requestId')
    ..aOM<$1.Struct>(4, _omitFieldNames ? '' : 'details',
        subBuilder: $1.Struct.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ErrorDetail clone() => ErrorDetail()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ErrorDetail copyWith(void Function(ErrorDetail) updates) =>
      super.copyWith((message) => updates(message as ErrorDetail))
          as ErrorDetail;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ErrorDetail create() => ErrorDetail._();
  @$core.override
  ErrorDetail createEmptyInstance() => create();
  static $pb.PbList<ErrorDetail> createRepeated() => $pb.PbList<ErrorDetail>();
  @$core.pragma('dart2js:noInline')
  static ErrorDetail getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ErrorDetail>(create);
  static ErrorDetail? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get code => $_getSZ(0);
  @$pb.TagNumber(1)
  set code($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasCode() => $_has(0);
  @$pb.TagNumber(1)
  void clearCode() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get message => $_getSZ(1);
  @$pb.TagNumber(2)
  set message($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasMessage() => $_has(1);
  @$pb.TagNumber(2)
  void clearMessage() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get requestId => $_getSZ(2);
  @$pb.TagNumber(3)
  set requestId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasRequestId() => $_has(2);
  @$pb.TagNumber(3)
  void clearRequestId() => $_clearField(3);

  @$pb.TagNumber(4)
  $1.Struct get details => $_getN(3);
  @$pb.TagNumber(4)
  set details($1.Struct value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasDetails() => $_has(3);
  @$pb.TagNumber(4)
  void clearDetails() => $_clearField(4);
  @$pb.TagNumber(4)
  $1.Struct ensureDetails() => $_ensure(3);
}

class RoutingUnavailableDetail extends $pb.GeneratedMessage {
  factory RoutingUnavailableDetail({
    RoutingRequirementKind? kind,
    $core.String? requested,
    $core.Iterable<$core.String>? available,
  }) {
    final result = create();
    if (kind != null) result.kind = kind;
    if (requested != null) result.requested = requested;
    if (available != null) result.available.addAll(available);
    return result;
  }

  RoutingUnavailableDetail._();

  factory RoutingUnavailableDetail.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RoutingUnavailableDetail.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RoutingUnavailableDetail',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..e<RoutingRequirementKind>(
        1, _omitFieldNames ? '' : 'kind', $pb.PbFieldType.OE,
        defaultOrMaker:
            RoutingRequirementKind.ROUTING_REQUIREMENT_KIND_UNSPECIFIED,
        valueOf: RoutingRequirementKind.valueOf,
        enumValues: RoutingRequirementKind.values)
    ..aOS(2, _omitFieldNames ? '' : 'requested')
    ..pPS(3, _omitFieldNames ? '' : 'available')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RoutingUnavailableDetail clone() =>
      RoutingUnavailableDetail()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RoutingUnavailableDetail copyWith(
          void Function(RoutingUnavailableDetail) updates) =>
      super.copyWith((message) => updates(message as RoutingUnavailableDetail))
          as RoutingUnavailableDetail;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RoutingUnavailableDetail create() => RoutingUnavailableDetail._();
  @$core.override
  RoutingUnavailableDetail createEmptyInstance() => create();
  static $pb.PbList<RoutingUnavailableDetail> createRepeated() =>
      $pb.PbList<RoutingUnavailableDetail>();
  @$core.pragma('dart2js:noInline')
  static RoutingUnavailableDetail getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RoutingUnavailableDetail>(create);
  static RoutingUnavailableDetail? _defaultInstance;

  @$pb.TagNumber(1)
  RoutingRequirementKind get kind => $_getN(0);
  @$pb.TagNumber(1)
  set kind(RoutingRequirementKind value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasKind() => $_has(0);
  @$pb.TagNumber(1)
  void clearKind() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get requested => $_getSZ(1);
  @$pb.TagNumber(2)
  set requested($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRequested() => $_has(1);
  @$pb.TagNumber(2)
  void clearRequested() => $_clearField(2);

  @$pb.TagNumber(3)
  $pb.PbList<$core.String> get available => $_getList(2);
}

class ModelCapability extends $pb.GeneratedMessage {
  factory ModelCapability({
    ModelProvider? provider,
    $core.String? model,
    $core.int? maxContextTokens,
  }) {
    final result = create();
    if (provider != null) result.provider = provider;
    if (model != null) result.model = model;
    if (maxContextTokens != null) result.maxContextTokens = maxContextTokens;
    return result;
  }

  ModelCapability._();

  factory ModelCapability.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ModelCapability.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ModelCapability',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..e<ModelProvider>(1, _omitFieldNames ? '' : 'provider', $pb.PbFieldType.OE,
        defaultOrMaker: ModelProvider.MODEL_PROVIDER_UNSPECIFIED,
        valueOf: ModelProvider.valueOf,
        enumValues: ModelProvider.values)
    ..aOS(2, _omitFieldNames ? '' : 'model')
    ..a<$core.int>(
        3, _omitFieldNames ? '' : 'maxContextTokens', $pb.PbFieldType.O3)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ModelCapability clone() => ModelCapability()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ModelCapability copyWith(void Function(ModelCapability) updates) =>
      super.copyWith((message) => updates(message as ModelCapability))
          as ModelCapability;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ModelCapability create() => ModelCapability._();
  @$core.override
  ModelCapability createEmptyInstance() => create();
  static $pb.PbList<ModelCapability> createRepeated() =>
      $pb.PbList<ModelCapability>();
  @$core.pragma('dart2js:noInline')
  static ModelCapability getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ModelCapability>(create);
  static ModelCapability? _defaultInstance;

  @$pb.TagNumber(1)
  ModelProvider get provider => $_getN(0);
  @$pb.TagNumber(1)
  set provider(ModelProvider value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProvider() => $_has(0);
  @$pb.TagNumber(1)
  void clearProvider() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get model => $_getSZ(1);
  @$pb.TagNumber(2)
  set model($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasModel() => $_has(1);
  @$pb.TagNumber(2)
  void clearModel() => $_clearField(2);

  /// An operator-configured routing guarantee, not a value inferred from a
  /// provider label or model name.
  @$pb.TagNumber(3)
  $core.int get maxContextTokens => $_getIZ(2);
  @$pb.TagNumber(3)
  set maxContextTokens($core.int value) => $_setSignedInt32(2, value);
  @$pb.TagNumber(3)
  $core.bool hasMaxContextTokens() => $_has(2);
  @$pb.TagNumber(3)
  void clearMaxContextTokens() => $_clearField(3);
}

class ProviderConfig extends $pb.GeneratedMessage {
  factory ProviderConfig({
    ModelProvider? provider,
    $core.bool? enabled,
    $core.String? defaultModel,
    $core.Iterable<ModelCapability>? models,
  }) {
    final result = create();
    if (provider != null) result.provider = provider;
    if (enabled != null) result.enabled = enabled;
    if (defaultModel != null) result.defaultModel = defaultModel;
    if (models != null) result.models.addAll(models);
    return result;
  }

  ProviderConfig._();

  factory ProviderConfig.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ProviderConfig.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ProviderConfig',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..e<ModelProvider>(1, _omitFieldNames ? '' : 'provider', $pb.PbFieldType.OE,
        defaultOrMaker: ModelProvider.MODEL_PROVIDER_UNSPECIFIED,
        valueOf: ModelProvider.valueOf,
        enumValues: ModelProvider.values)
    ..aOB(2, _omitFieldNames ? '' : 'enabled')
    ..aOS(3, _omitFieldNames ? '' : 'defaultModel')
    ..pc<ModelCapability>(
        4, _omitFieldNames ? '' : 'models', $pb.PbFieldType.PM,
        subBuilder: ModelCapability.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ProviderConfig clone() => ProviderConfig()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ProviderConfig copyWith(void Function(ProviderConfig) updates) =>
      super.copyWith((message) => updates(message as ProviderConfig))
          as ProviderConfig;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ProviderConfig create() => ProviderConfig._();
  @$core.override
  ProviderConfig createEmptyInstance() => create();
  static $pb.PbList<ProviderConfig> createRepeated() =>
      $pb.PbList<ProviderConfig>();
  @$core.pragma('dart2js:noInline')
  static ProviderConfig getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ProviderConfig>(create);
  static ProviderConfig? _defaultInstance;

  @$pb.TagNumber(1)
  ModelProvider get provider => $_getN(0);
  @$pb.TagNumber(1)
  set provider(ModelProvider value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasProvider() => $_has(0);
  @$pb.TagNumber(1)
  void clearProvider() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.bool get enabled => $_getBF(1);
  @$pb.TagNumber(2)
  set enabled($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasEnabled() => $_has(1);
  @$pb.TagNumber(2)
  void clearEnabled() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get defaultModel => $_getSZ(2);
  @$pb.TagNumber(3)
  set defaultModel($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasDefaultModel() => $_has(2);
  @$pb.TagNumber(3)
  void clearDefaultModel() => $_clearField(3);

  @$pb.TagNumber(4)
  $pb.PbList<ModelCapability> get models => $_getList(3);
}

class AgentDescriptor extends $pb.GeneratedMessage {
  factory AgentDescriptor({
    AgentId? id,
    $core.String? displayName,
    $core.bool? available,
  }) {
    final result = create();
    if (id != null) result.id = id;
    if (displayName != null) result.displayName = displayName;
    if (available != null) result.available = available;
    return result;
  }

  AgentDescriptor._();

  factory AgentDescriptor.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory AgentDescriptor.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'AgentDescriptor',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..e<AgentId>(1, _omitFieldNames ? '' : 'id', $pb.PbFieldType.OE,
        defaultOrMaker: AgentId.AGENT_ID_UNSPECIFIED,
        valueOf: AgentId.valueOf,
        enumValues: AgentId.values)
    ..aOS(2, _omitFieldNames ? '' : 'displayName')
    ..aOB(3, _omitFieldNames ? '' : 'available')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AgentDescriptor clone() => AgentDescriptor()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AgentDescriptor copyWith(void Function(AgentDescriptor) updates) =>
      super.copyWith((message) => updates(message as AgentDescriptor))
          as AgentDescriptor;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static AgentDescriptor create() => AgentDescriptor._();
  @$core.override
  AgentDescriptor createEmptyInstance() => create();
  static $pb.PbList<AgentDescriptor> createRepeated() =>
      $pb.PbList<AgentDescriptor>();
  @$core.pragma('dart2js:noInline')
  static AgentDescriptor getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<AgentDescriptor>(create);
  static AgentDescriptor? _defaultInstance;

  @$pb.TagNumber(1)
  AgentId get id => $_getN(0);
  @$pb.TagNumber(1)
  set id(AgentId value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasId() => $_has(0);
  @$pb.TagNumber(1)
  void clearId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get displayName => $_getSZ(1);
  @$pb.TagNumber(2)
  set displayName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasDisplayName() => $_has(1);
  @$pb.TagNumber(2)
  void clearDisplayName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.bool get available => $_getBF(2);
  @$pb.TagNumber(3)
  set available($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasAvailable() => $_has(2);
  @$pb.TagNumber(3)
  void clearAvailable() => $_clearField(3);
}

class Message extends $pb.GeneratedMessage {
  factory Message({
    $core.String? messageId,
    $core.String? sessionId,
    $core.String? runId,
    MessageRole? role,
    $core.String? content,
    $core.String? contentType,
    $fixnum.Int64? sequence,
    $0.Timestamp? createdAt,
    RunState? runState,
  }) {
    final result = create();
    if (messageId != null) result.messageId = messageId;
    if (sessionId != null) result.sessionId = sessionId;
    if (runId != null) result.runId = runId;
    if (role != null) result.role = role;
    if (content != null) result.content = content;
    if (contentType != null) result.contentType = contentType;
    if (sequence != null) result.sequence = sequence;
    if (createdAt != null) result.createdAt = createdAt;
    if (runState != null) result.runState = runState;
    return result;
  }

  Message._();

  factory Message.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Message.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Message',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'messageId')
    ..aOS(2, _omitFieldNames ? '' : 'sessionId')
    ..aOS(3, _omitFieldNames ? '' : 'runId')
    ..e<MessageRole>(4, _omitFieldNames ? '' : 'role', $pb.PbFieldType.OE,
        defaultOrMaker: MessageRole.MESSAGE_ROLE_UNSPECIFIED,
        valueOf: MessageRole.valueOf,
        enumValues: MessageRole.values)
    ..aOS(5, _omitFieldNames ? '' : 'content')
    ..aOS(6, _omitFieldNames ? '' : 'contentType')
    ..aInt64(7, _omitFieldNames ? '' : 'sequence')
    ..aOM<$0.Timestamp>(8, _omitFieldNames ? '' : 'createdAt',
        subBuilder: $0.Timestamp.create)
    ..aOM<RunState>(9, _omitFieldNames ? '' : 'runState',
        subBuilder: RunState.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Message clone() => Message()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Message copyWith(void Function(Message) updates) =>
      super.copyWith((message) => updates(message as Message)) as Message;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Message create() => Message._();
  @$core.override
  Message createEmptyInstance() => create();
  static $pb.PbList<Message> createRepeated() => $pb.PbList<Message>();
  @$core.pragma('dart2js:noInline')
  static Message getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Message>(create);
  static Message? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get messageId => $_getSZ(0);
  @$pb.TagNumber(1)
  set messageId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasMessageId() => $_has(0);
  @$pb.TagNumber(1)
  void clearMessageId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get sessionId => $_getSZ(1);
  @$pb.TagNumber(2)
  set sessionId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasSessionId() => $_has(1);
  @$pb.TagNumber(2)
  void clearSessionId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get runId => $_getSZ(2);
  @$pb.TagNumber(3)
  set runId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasRunId() => $_has(2);
  @$pb.TagNumber(3)
  void clearRunId() => $_clearField(3);

  @$pb.TagNumber(4)
  MessageRole get role => $_getN(3);
  @$pb.TagNumber(4)
  set role(MessageRole value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasRole() => $_has(3);
  @$pb.TagNumber(4)
  void clearRole() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get content => $_getSZ(4);
  @$pb.TagNumber(5)
  set content($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasContent() => $_has(4);
  @$pb.TagNumber(5)
  void clearContent() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get contentType => $_getSZ(5);
  @$pb.TagNumber(6)
  set contentType($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasContentType() => $_has(5);
  @$pb.TagNumber(6)
  void clearContentType() => $_clearField(6);

  @$pb.TagNumber(7)
  $fixnum.Int64 get sequence => $_getI64(6);
  @$pb.TagNumber(7)
  set sequence($fixnum.Int64 value) => $_setInt64(6, value);
  @$pb.TagNumber(7)
  $core.bool hasSequence() => $_has(6);
  @$pb.TagNumber(7)
  void clearSequence() => $_clearField(7);

  @$pb.TagNumber(8)
  $0.Timestamp get createdAt => $_getN(7);
  @$pb.TagNumber(8)
  set createdAt($0.Timestamp value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasCreatedAt() => $_has(7);
  @$pb.TagNumber(8)
  void clearCreatedAt() => $_clearField(8);
  @$pb.TagNumber(8)
  $0.Timestamp ensureCreatedAt() => $_ensure(7);

  @$pb.TagNumber(9)
  RunState get runState => $_getN(8);
  @$pb.TagNumber(9)
  set runState(RunState value) => $_setField(9, value);
  @$pb.TagNumber(9)
  $core.bool hasRunState() => $_has(8);
  @$pb.TagNumber(9)
  void clearRunState() => $_clearField(9);
  @$pb.TagNumber(9)
  RunState ensureRunState() => $_ensure(8);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
