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

/// The durable, self-contained answer to "what happened to this run", carried on
/// history and on every lifecycle event so a reopened session and a live stream
/// agree without replaying the timeline.
///
/// There is deliberately no retryable field: whether the system retries is an
/// internal dispatch decision, not a promise that repeating the request is safe.
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

  /// Per-run monotonic version, starting at 1, incremented exactly once per real
  /// public transition. Semantic no-ops do not increment it. It is the only
  /// ordering authority for reconciliation: a client keeps the higher version
  /// and drops anything older, so out-of-order or duplicate delivery cannot
  /// resurrect a stale phase. Zero means absent, never "version zero".
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

  /// Set only for terminal lifecycles.
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

  /// Whether the canonical assistant message has content worth rendering, so a
  /// client can distinguish a silent success from a lost one without inspecting
  /// message bodies.
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
    $core.String? remoteEndpoint,
    $core.bool? requiresPerRunConsent,
  }) {
    final result = create();
    if (provider != null) result.provider = provider;
    if (enabled != null) result.enabled = enabled;
    if (defaultModel != null) result.defaultModel = defaultModel;
    if (models != null) result.models.addAll(models);
    if (remoteEndpoint != null) result.remoteEndpoint = remoteEndpoint;
    if (requiresPerRunConsent != null)
      result.requiresPerRunConsent = requiresPerRunConsent;
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
    ..aOS(5, _omitFieldNames ? '' : 'remoteEndpoint')
    ..aOB(6, _omitFieldNames ? '' : 'requiresPerRunConsent')
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

  /// Canonical provider base endpoint. Empty for local providers.
  @$pb.TagNumber(5)
  $core.String get remoteEndpoint => $_getSZ(4);
  @$pb.TagNumber(5)
  set remoteEndpoint($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasRemoteEndpoint() => $_has(4);
  @$pb.TagNumber(5)
  void clearRemoteEndpoint() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.bool get requiresPerRunConsent => $_getBF(5);
  @$pb.TagNumber(6)
  set requiresPerRunConsent($core.bool value) => $_setBool(5, value);
  @$pb.TagNumber(6)
  $core.bool hasRequiresPerRunConsent() => $_has(5);
  @$pb.TagNumber(6)
  void clearRequiresPerRunConsent() => $_clearField(6);
}

class RemoteEgressDisclosure extends $pb.GeneratedMessage {
  factory RemoteEgressDisclosure({
    $core.String? challenge,
    ModelProvider? provider,
    $core.String? model,
    $core.String? endpoint,
    $core.String? endpointHost,
    $core.String? externalAgentId,
    $core.Iterable<EgressDataCategory>? dataCategories,
    $0.Timestamp? expiresAt,
    $core.Iterable<RemoteMcpEgressDestination>? remoteMcpServers,
    $core.Iterable<$core.String>? selectedTools,
  }) {
    final result = create();
    if (challenge != null) result.challenge = challenge;
    if (provider != null) result.provider = provider;
    if (model != null) result.model = model;
    if (endpoint != null) result.endpoint = endpoint;
    if (endpointHost != null) result.endpointHost = endpointHost;
    if (externalAgentId != null) result.externalAgentId = externalAgentId;
    if (dataCategories != null) result.dataCategories.addAll(dataCategories);
    if (expiresAt != null) result.expiresAt = expiresAt;
    if (remoteMcpServers != null)
      result.remoteMcpServers.addAll(remoteMcpServers);
    if (selectedTools != null) result.selectedTools.addAll(selectedTools);
    return result;
  }

  RemoteEgressDisclosure._();

  factory RemoteEgressDisclosure.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RemoteEgressDisclosure.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RemoteEgressDisclosure',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'challenge')
    ..e<ModelProvider>(2, _omitFieldNames ? '' : 'provider', $pb.PbFieldType.OE,
        defaultOrMaker: ModelProvider.MODEL_PROVIDER_UNSPECIFIED,
        valueOf: ModelProvider.valueOf,
        enumValues: ModelProvider.values)
    ..aOS(3, _omitFieldNames ? '' : 'model')
    ..aOS(4, _omitFieldNames ? '' : 'endpoint')
    ..aOS(5, _omitFieldNames ? '' : 'endpointHost')
    ..aOS(6, _omitFieldNames ? '' : 'externalAgentId')
    ..pc<EgressDataCategory>(
        7, _omitFieldNames ? '' : 'dataCategories', $pb.PbFieldType.KE,
        valueOf: EgressDataCategory.valueOf,
        enumValues: EgressDataCategory.values,
        defaultEnumValue: EgressDataCategory.EGRESS_DATA_CATEGORY_UNSPECIFIED)
    ..aOM<$0.Timestamp>(8, _omitFieldNames ? '' : 'expiresAt',
        subBuilder: $0.Timestamp.create)
    ..pc<RemoteMcpEgressDestination>(
        9, _omitFieldNames ? '' : 'remoteMcpServers', $pb.PbFieldType.PM,
        subBuilder: RemoteMcpEgressDestination.create)
    ..pPS(10, _omitFieldNames ? '' : 'selectedTools')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RemoteEgressDisclosure clone() =>
      RemoteEgressDisclosure()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RemoteEgressDisclosure copyWith(
          void Function(RemoteEgressDisclosure) updates) =>
      super.copyWith((message) => updates(message as RemoteEgressDisclosure))
          as RemoteEgressDisclosure;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RemoteEgressDisclosure create() => RemoteEgressDisclosure._();
  @$core.override
  RemoteEgressDisclosure createEmptyInstance() => create();
  static $pb.PbList<RemoteEgressDisclosure> createRepeated() =>
      $pb.PbList<RemoteEgressDisclosure>();
  @$core.pragma('dart2js:noInline')
  static RemoteEgressDisclosure getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RemoteEgressDisclosure>(create);
  static RemoteEgressDisclosure? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get challenge => $_getSZ(0);
  @$pb.TagNumber(1)
  set challenge($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasChallenge() => $_has(0);
  @$pb.TagNumber(1)
  void clearChallenge() => $_clearField(1);

  @$pb.TagNumber(2)
  ModelProvider get provider => $_getN(1);
  @$pb.TagNumber(2)
  set provider(ModelProvider value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProvider() => $_has(1);
  @$pb.TagNumber(2)
  void clearProvider() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get model => $_getSZ(2);
  @$pb.TagNumber(3)
  set model($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasModel() => $_has(2);
  @$pb.TagNumber(3)
  void clearModel() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get endpoint => $_getSZ(3);
  @$pb.TagNumber(4)
  set endpoint($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasEndpoint() => $_has(3);
  @$pb.TagNumber(4)
  void clearEndpoint() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get endpointHost => $_getSZ(4);
  @$pb.TagNumber(5)
  set endpointHost($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasEndpointHost() => $_has(4);
  @$pb.TagNumber(5)
  void clearEndpointHost() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get externalAgentId => $_getSZ(5);
  @$pb.TagNumber(6)
  set externalAgentId($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasExternalAgentId() => $_has(5);
  @$pb.TagNumber(6)
  void clearExternalAgentId() => $_clearField(6);

  @$pb.TagNumber(7)
  $pb.PbList<EgressDataCategory> get dataCategories => $_getList(6);

  @$pb.TagNumber(8)
  $0.Timestamp get expiresAt => $_getN(7);
  @$pb.TagNumber(8)
  set expiresAt($0.Timestamp value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasExpiresAt() => $_has(7);
  @$pb.TagNumber(8)
  void clearExpiresAt() => $_clearField(8);
  @$pb.TagNumber(8)
  $0.Timestamp ensureExpiresAt() => $_ensure(7);

  @$pb.TagNumber(9)
  $pb.PbList<RemoteMcpEgressDestination> get remoteMcpServers => $_getList(8);

  @$pb.TagNumber(10)
  $pb.PbList<$core.String> get selectedTools => $_getList(9);
}

class RemoteMcpEgressDestination extends $pb.GeneratedMessage {
  factory RemoteMcpEgressDestination({
    $core.String? serverName,
    $core.String? endpoint,
    $core.String? endpointHost,
  }) {
    final result = create();
    if (serverName != null) result.serverName = serverName;
    if (endpoint != null) result.endpoint = endpoint;
    if (endpointHost != null) result.endpointHost = endpointHost;
    return result;
  }

  RemoteMcpEgressDestination._();

  factory RemoteMcpEgressDestination.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RemoteMcpEgressDestination.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RemoteMcpEgressDestination',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'serverName')
    ..aOS(2, _omitFieldNames ? '' : 'endpoint')
    ..aOS(3, _omitFieldNames ? '' : 'endpointHost')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RemoteMcpEgressDestination clone() =>
      RemoteMcpEgressDestination()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RemoteMcpEgressDestination copyWith(
          void Function(RemoteMcpEgressDestination) updates) =>
      super.copyWith(
              (message) => updates(message as RemoteMcpEgressDestination))
          as RemoteMcpEgressDestination;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RemoteMcpEgressDestination create() => RemoteMcpEgressDestination._();
  @$core.override
  RemoteMcpEgressDestination createEmptyInstance() => create();
  static $pb.PbList<RemoteMcpEgressDestination> createRepeated() =>
      $pb.PbList<RemoteMcpEgressDestination>();
  @$core.pragma('dart2js:noInline')
  static RemoteMcpEgressDestination getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RemoteMcpEgressDestination>(create);
  static RemoteMcpEgressDestination? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get serverName => $_getSZ(0);
  @$pb.TagNumber(1)
  set serverName($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasServerName() => $_has(0);
  @$pb.TagNumber(1)
  void clearServerName() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get endpoint => $_getSZ(1);
  @$pb.TagNumber(2)
  set endpoint($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasEndpoint() => $_has(1);
  @$pb.TagNumber(2)
  void clearEndpoint() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get endpointHost => $_getSZ(2);
  @$pb.TagNumber(3)
  set endpointHost($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasEndpointHost() => $_has(2);
  @$pb.TagNumber(3)
  void clearEndpointHost() => $_clearField(3);
}

class RemoteEgressConsent extends $pb.GeneratedMessage {
  factory RemoteEgressConsent({
    $core.String? challenge,
    $core.Iterable<EgressDataCategory>? acknowledgedDataCategories,
    $core.bool? acknowledged,
  }) {
    final result = create();
    if (challenge != null) result.challenge = challenge;
    if (acknowledgedDataCategories != null)
      result.acknowledgedDataCategories.addAll(acknowledgedDataCategories);
    if (acknowledged != null) result.acknowledged = acknowledged;
    return result;
  }

  RemoteEgressConsent._();

  factory RemoteEgressConsent.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RemoteEgressConsent.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RemoteEgressConsent',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'challenge')
    ..pc<EgressDataCategory>(2,
        _omitFieldNames ? '' : 'acknowledgedDataCategories', $pb.PbFieldType.KE,
        valueOf: EgressDataCategory.valueOf,
        enumValues: EgressDataCategory.values,
        defaultEnumValue: EgressDataCategory.EGRESS_DATA_CATEGORY_UNSPECIFIED)
    ..aOB(3, _omitFieldNames ? '' : 'acknowledged')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RemoteEgressConsent clone() => RemoteEgressConsent()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RemoteEgressConsent copyWith(void Function(RemoteEgressConsent) updates) =>
      super.copyWith((message) => updates(message as RemoteEgressConsent))
          as RemoteEgressConsent;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RemoteEgressConsent create() => RemoteEgressConsent._();
  @$core.override
  RemoteEgressConsent createEmptyInstance() => create();
  static $pb.PbList<RemoteEgressConsent> createRepeated() =>
      $pb.PbList<RemoteEgressConsent>();
  @$core.pragma('dart2js:noInline')
  static RemoteEgressConsent getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RemoteEgressConsent>(create);
  static RemoteEgressConsent? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get challenge => $_getSZ(0);
  @$pb.TagNumber(1)
  set challenge($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasChallenge() => $_has(0);
  @$pb.TagNumber(1)
  void clearChallenge() => $_clearField(1);

  @$pb.TagNumber(2)
  $pb.PbList<EgressDataCategory> get acknowledgedDataCategories => $_getList(1);

  @$pb.TagNumber(3)
  $core.bool get acknowledged => $_getBF(2);
  @$pb.TagNumber(3)
  set acknowledged($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasAcknowledged() => $_has(2);
  @$pb.TagNumber(3)
  void clearAcknowledged() => $_clearField(3);
}

/// The orchestrator-owned decision frozen for one remote run. It contains no
/// challenge, nonce, credential value/reference, or request content; one-way
/// digests bind those inputs without disclosing them.
class RunEgressDecision extends $pb.GeneratedMessage {
  factory RunEgressDecision({
    $core.String? decisionId,
    $core.int? version,
    ModelProvider? provider,
    $core.String? model,
    $core.String? endpoint,
    $core.String? endpointHost,
    $core.String? externalAgentId,
    $core.Iterable<EgressDataCategory>? dataCategories,
    $0.Timestamp? consentGrantedAt,
    $core.String? challengeFingerprint,
    $core.Iterable<$core.String>? selectedTools,
    $core.String? skillSnapshotFingerprint,
    $core.bool? recallApplicable,
    $core.bool? memoryProfileApplicable,
    $core.String? externalCredentialRefHash,
    $core.String? requestDigest,
    $core.Iterable<RemoteMcpEgressDestination>? remoteMcpServers,
  }) {
    final result = create();
    if (decisionId != null) result.decisionId = decisionId;
    if (version != null) result.version = version;
    if (provider != null) result.provider = provider;
    if (model != null) result.model = model;
    if (endpoint != null) result.endpoint = endpoint;
    if (endpointHost != null) result.endpointHost = endpointHost;
    if (externalAgentId != null) result.externalAgentId = externalAgentId;
    if (dataCategories != null) result.dataCategories.addAll(dataCategories);
    if (consentGrantedAt != null) result.consentGrantedAt = consentGrantedAt;
    if (challengeFingerprint != null)
      result.challengeFingerprint = challengeFingerprint;
    if (selectedTools != null) result.selectedTools.addAll(selectedTools);
    if (skillSnapshotFingerprint != null)
      result.skillSnapshotFingerprint = skillSnapshotFingerprint;
    if (recallApplicable != null) result.recallApplicable = recallApplicable;
    if (memoryProfileApplicable != null)
      result.memoryProfileApplicable = memoryProfileApplicable;
    if (externalCredentialRefHash != null)
      result.externalCredentialRefHash = externalCredentialRefHash;
    if (requestDigest != null) result.requestDigest = requestDigest;
    if (remoteMcpServers != null)
      result.remoteMcpServers.addAll(remoteMcpServers);
    return result;
  }

  RunEgressDecision._();

  factory RunEgressDecision.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RunEgressDecision.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RunEgressDecision',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'decisionId')
    ..a<$core.int>(2, _omitFieldNames ? '' : 'version', $pb.PbFieldType.O3)
    ..e<ModelProvider>(3, _omitFieldNames ? '' : 'provider', $pb.PbFieldType.OE,
        defaultOrMaker: ModelProvider.MODEL_PROVIDER_UNSPECIFIED,
        valueOf: ModelProvider.valueOf,
        enumValues: ModelProvider.values)
    ..aOS(4, _omitFieldNames ? '' : 'model')
    ..aOS(5, _omitFieldNames ? '' : 'endpoint')
    ..aOS(6, _omitFieldNames ? '' : 'endpointHost')
    ..aOS(7, _omitFieldNames ? '' : 'externalAgentId')
    ..pc<EgressDataCategory>(
        8, _omitFieldNames ? '' : 'dataCategories', $pb.PbFieldType.KE,
        valueOf: EgressDataCategory.valueOf,
        enumValues: EgressDataCategory.values,
        defaultEnumValue: EgressDataCategory.EGRESS_DATA_CATEGORY_UNSPECIFIED)
    ..aOM<$0.Timestamp>(9, _omitFieldNames ? '' : 'consentGrantedAt',
        subBuilder: $0.Timestamp.create)
    ..aOS(10, _omitFieldNames ? '' : 'challengeFingerprint')
    ..pPS(11, _omitFieldNames ? '' : 'selectedTools')
    ..aOS(12, _omitFieldNames ? '' : 'skillSnapshotFingerprint')
    ..aOB(13, _omitFieldNames ? '' : 'recallApplicable')
    ..aOB(14, _omitFieldNames ? '' : 'memoryProfileApplicable')
    ..aOS(15, _omitFieldNames ? '' : 'externalCredentialRefHash')
    ..aOS(16, _omitFieldNames ? '' : 'requestDigest')
    ..pc<RemoteMcpEgressDestination>(
        17, _omitFieldNames ? '' : 'remoteMcpServers', $pb.PbFieldType.PM,
        subBuilder: RemoteMcpEgressDestination.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunEgressDecision clone() => RunEgressDecision()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RunEgressDecision copyWith(void Function(RunEgressDecision) updates) =>
      super.copyWith((message) => updates(message as RunEgressDecision))
          as RunEgressDecision;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RunEgressDecision create() => RunEgressDecision._();
  @$core.override
  RunEgressDecision createEmptyInstance() => create();
  static $pb.PbList<RunEgressDecision> createRepeated() =>
      $pb.PbList<RunEgressDecision>();
  @$core.pragma('dart2js:noInline')
  static RunEgressDecision getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RunEgressDecision>(create);
  static RunEgressDecision? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get decisionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set decisionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasDecisionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearDecisionId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.int get version => $_getIZ(1);
  @$pb.TagNumber(2)
  set version($core.int value) => $_setSignedInt32(1, value);
  @$pb.TagNumber(2)
  $core.bool hasVersion() => $_has(1);
  @$pb.TagNumber(2)
  void clearVersion() => $_clearField(2);

  @$pb.TagNumber(3)
  ModelProvider get provider => $_getN(2);
  @$pb.TagNumber(3)
  set provider(ModelProvider value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasProvider() => $_has(2);
  @$pb.TagNumber(3)
  void clearProvider() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get model => $_getSZ(3);
  @$pb.TagNumber(4)
  set model($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasModel() => $_has(3);
  @$pb.TagNumber(4)
  void clearModel() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get endpoint => $_getSZ(4);
  @$pb.TagNumber(5)
  set endpoint($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasEndpoint() => $_has(4);
  @$pb.TagNumber(5)
  void clearEndpoint() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get endpointHost => $_getSZ(5);
  @$pb.TagNumber(6)
  set endpointHost($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasEndpointHost() => $_has(5);
  @$pb.TagNumber(6)
  void clearEndpointHost() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.String get externalAgentId => $_getSZ(6);
  @$pb.TagNumber(7)
  set externalAgentId($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasExternalAgentId() => $_has(6);
  @$pb.TagNumber(7)
  void clearExternalAgentId() => $_clearField(7);

  @$pb.TagNumber(8)
  $pb.PbList<EgressDataCategory> get dataCategories => $_getList(7);

  @$pb.TagNumber(9)
  $0.Timestamp get consentGrantedAt => $_getN(8);
  @$pb.TagNumber(9)
  set consentGrantedAt($0.Timestamp value) => $_setField(9, value);
  @$pb.TagNumber(9)
  $core.bool hasConsentGrantedAt() => $_has(8);
  @$pb.TagNumber(9)
  void clearConsentGrantedAt() => $_clearField(9);
  @$pb.TagNumber(9)
  $0.Timestamp ensureConsentGrantedAt() => $_ensure(8);

  @$pb.TagNumber(10)
  $core.String get challengeFingerprint => $_getSZ(9);
  @$pb.TagNumber(10)
  set challengeFingerprint($core.String value) => $_setString(9, value);
  @$pb.TagNumber(10)
  $core.bool hasChallengeFingerprint() => $_has(9);
  @$pb.TagNumber(10)
  void clearChallengeFingerprint() => $_clearField(10);

  @$pb.TagNumber(11)
  $pb.PbList<$core.String> get selectedTools => $_getList(10);

  @$pb.TagNumber(12)
  $core.String get skillSnapshotFingerprint => $_getSZ(11);
  @$pb.TagNumber(12)
  set skillSnapshotFingerprint($core.String value) => $_setString(11, value);
  @$pb.TagNumber(12)
  $core.bool hasSkillSnapshotFingerprint() => $_has(11);
  @$pb.TagNumber(12)
  void clearSkillSnapshotFingerprint() => $_clearField(12);

  @$pb.TagNumber(13)
  $core.bool get recallApplicable => $_getBF(12);
  @$pb.TagNumber(13)
  set recallApplicable($core.bool value) => $_setBool(12, value);
  @$pb.TagNumber(13)
  $core.bool hasRecallApplicable() => $_has(12);
  @$pb.TagNumber(13)
  void clearRecallApplicable() => $_clearField(13);

  @$pb.TagNumber(14)
  $core.bool get memoryProfileApplicable => $_getBF(13);
  @$pb.TagNumber(14)
  set memoryProfileApplicable($core.bool value) => $_setBool(13, value);
  @$pb.TagNumber(14)
  $core.bool hasMemoryProfileApplicable() => $_has(13);
  @$pb.TagNumber(14)
  void clearMemoryProfileApplicable() => $_clearField(14);

  @$pb.TagNumber(15)
  $core.String get externalCredentialRefHash => $_getSZ(14);
  @$pb.TagNumber(15)
  set externalCredentialRefHash($core.String value) => $_setString(14, value);
  @$pb.TagNumber(15)
  $core.bool hasExternalCredentialRefHash() => $_has(14);
  @$pb.TagNumber(15)
  void clearExternalCredentialRefHash() => $_clearField(15);

  @$pb.TagNumber(16)
  $core.String get requestDigest => $_getSZ(15);
  @$pb.TagNumber(16)
  set requestDigest($core.String value) => $_setString(15, value);
  @$pb.TagNumber(16)
  $core.bool hasRequestDigest() => $_has(15);
  @$pb.TagNumber(16)
  void clearRequestDigest() => $_clearField(16);

  @$pb.TagNumber(17)
  $pb.PbList<RemoteMcpEgressDestination> get remoteMcpServers => $_getList(16);
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

  /// The authoritative state of the run that produced this message, so reopened
  /// history needs no separate query to know the outcome. Absent when the
  /// message has no run correlation, or when legacy correlation is inconsistent.
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
