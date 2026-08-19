// This is a generated file - do not edit.
//
// Generated from turing/v1/approvals.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

import 'approvals.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'approvals.pbenum.dart';

class ApproveApprovalRequest extends $pb.GeneratedMessage {
  factory ApproveApprovalRequest({
    $core.String? approvalId,
    $core.String? comment,
  }) {
    final result = create();
    if (approvalId != null) result.approvalId = approvalId;
    if (comment != null) result.comment = comment;
    return result;
  }

  ApproveApprovalRequest._();

  factory ApproveApprovalRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ApproveApprovalRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ApproveApprovalRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'approvalId')
    ..aOS(2, _omitFieldNames ? '' : 'comment')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApproveApprovalRequest clone() =>
      ApproveApprovalRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApproveApprovalRequest copyWith(
          void Function(ApproveApprovalRequest) updates) =>
      super.copyWith((message) => updates(message as ApproveApprovalRequest))
          as ApproveApprovalRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ApproveApprovalRequest create() => ApproveApprovalRequest._();
  @$core.override
  ApproveApprovalRequest createEmptyInstance() => create();
  static $pb.PbList<ApproveApprovalRequest> createRepeated() =>
      $pb.PbList<ApproveApprovalRequest>();
  @$core.pragma('dart2js:noInline')
  static ApproveApprovalRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ApproveApprovalRequest>(create);
  static ApproveApprovalRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get approvalId => $_getSZ(0);
  @$pb.TagNumber(1)
  set approvalId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasApprovalId() => $_has(0);
  @$pb.TagNumber(1)
  void clearApprovalId() => $_clearField(1);

  /// Optional by convention. Proto3 scalar presence is not enabled, so omission
  /// and an explicitly empty value are both persisted as an empty human comment.
  @$pb.TagNumber(2)
  $core.String get comment => $_getSZ(1);
  @$pb.TagNumber(2)
  set comment($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasComment() => $_has(1);
  @$pb.TagNumber(2)
  void clearComment() => $_clearField(2);
}

class DenyApprovalRequest extends $pb.GeneratedMessage {
  factory DenyApprovalRequest({
    $core.String? approvalId,
    $core.String? reason,
  }) {
    final result = create();
    if (approvalId != null) result.approvalId = approvalId;
    if (reason != null) result.reason = reason;
    return result;
  }

  DenyApprovalRequest._();

  factory DenyApprovalRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DenyApprovalRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DenyApprovalRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'approvalId')
    ..aOS(2, _omitFieldNames ? '' : 'reason')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DenyApprovalRequest clone() => DenyApprovalRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DenyApprovalRequest copyWith(void Function(DenyApprovalRequest) updates) =>
      super.copyWith((message) => updates(message as DenyApprovalRequest))
          as DenyApprovalRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DenyApprovalRequest create() => DenyApprovalRequest._();
  @$core.override
  DenyApprovalRequest createEmptyInstance() => create();
  static $pb.PbList<DenyApprovalRequest> createRepeated() =>
      $pb.PbList<DenyApprovalRequest>();
  @$core.pragma('dart2js:noInline')
  static DenyApprovalRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DenyApprovalRequest>(create);
  static DenyApprovalRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get approvalId => $_getSZ(0);
  @$pb.TagNumber(1)
  set approvalId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasApprovalId() => $_has(0);
  @$pb.TagNumber(1)
  void clearApprovalId() => $_clearField(1);

  /// Optional by convention. Proto3 scalar presence is not enabled, so omission
  /// and an explicitly empty value are both persisted as an empty human reason.
  @$pb.TagNumber(2)
  $core.String get reason => $_getSZ(1);
  @$pb.TagNumber(2)
  set reason($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasReason() => $_has(1);
  @$pb.TagNumber(2)
  void clearReason() => $_clearField(2);
}

class ApprovalResponse extends $pb.GeneratedMessage {
  factory ApprovalResponse({
    $core.String? approvalId,
    ApprovalStatus? status,
  }) {
    final result = create();
    if (approvalId != null) result.approvalId = approvalId;
    if (status != null) result.status = status;
    return result;
  }

  ApprovalResponse._();

  factory ApprovalResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ApprovalResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ApprovalResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'approvalId')
    ..e<ApprovalStatus>(2, _omitFieldNames ? '' : 'status', $pb.PbFieldType.OE,
        defaultOrMaker: ApprovalStatus.APPROVAL_STATUS_UNSPECIFIED,
        valueOf: ApprovalStatus.valueOf,
        enumValues: ApprovalStatus.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApprovalResponse clone() => ApprovalResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApprovalResponse copyWith(void Function(ApprovalResponse) updates) =>
      super.copyWith((message) => updates(message as ApprovalResponse))
          as ApprovalResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ApprovalResponse create() => ApprovalResponse._();
  @$core.override
  ApprovalResponse createEmptyInstance() => create();
  static $pb.PbList<ApprovalResponse> createRepeated() =>
      $pb.PbList<ApprovalResponse>();
  @$core.pragma('dart2js:noInline')
  static ApprovalResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ApprovalResponse>(create);
  static ApprovalResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get approvalId => $_getSZ(0);
  @$pb.TagNumber(1)
  set approvalId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasApprovalId() => $_has(0);
  @$pb.TagNumber(1)
  void clearApprovalId() => $_clearField(1);

  @$pb.TagNumber(2)
  ApprovalStatus get status => $_getN(1);
  @$pb.TagNumber(2)
  set status(ApprovalStatus value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasStatus() => $_has(1);
  @$pb.TagNumber(2)
  void clearStatus() => $_clearField(2);
}

class GetApprovalForRuntimeRequest extends $pb.GeneratedMessage {
  factory GetApprovalForRuntimeRequest({
    $core.String? approvalId,
  }) {
    final result = create();
    if (approvalId != null) result.approvalId = approvalId;
    return result;
  }

  GetApprovalForRuntimeRequest._();

  factory GetApprovalForRuntimeRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetApprovalForRuntimeRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetApprovalForRuntimeRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'approvalId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetApprovalForRuntimeRequest clone() =>
      GetApprovalForRuntimeRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetApprovalForRuntimeRequest copyWith(
          void Function(GetApprovalForRuntimeRequest) updates) =>
      super.copyWith(
              (message) => updates(message as GetApprovalForRuntimeRequest))
          as GetApprovalForRuntimeRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetApprovalForRuntimeRequest create() =>
      GetApprovalForRuntimeRequest._();
  @$core.override
  GetApprovalForRuntimeRequest createEmptyInstance() => create();
  static $pb.PbList<GetApprovalForRuntimeRequest> createRepeated() =>
      $pb.PbList<GetApprovalForRuntimeRequest>();
  @$core.pragma('dart2js:noInline')
  static GetApprovalForRuntimeRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetApprovalForRuntimeRequest>(create);
  static GetApprovalForRuntimeRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get approvalId => $_getSZ(0);
  @$pb.TagNumber(1)
  set approvalId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasApprovalId() => $_has(0);
  @$pb.TagNumber(1)
  void clearApprovalId() => $_clearField(1);
}

class RuntimeApprovalState extends $pb.GeneratedMessage {
  factory RuntimeApprovalState({
    $core.String? approvalId,
    ApprovalStatus? status,
    $core.String? approvalToken,
  }) {
    final result = create();
    if (approvalId != null) result.approvalId = approvalId;
    if (status != null) result.status = status;
    if (approvalToken != null) result.approvalToken = approvalToken;
    return result;
  }

  RuntimeApprovalState._();

  factory RuntimeApprovalState.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RuntimeApprovalState.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RuntimeApprovalState',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'approvalId')
    ..e<ApprovalStatus>(2, _omitFieldNames ? '' : 'status', $pb.PbFieldType.OE,
        defaultOrMaker: ApprovalStatus.APPROVAL_STATUS_UNSPECIFIED,
        valueOf: ApprovalStatus.valueOf,
        enumValues: ApprovalStatus.values)
    ..aOS(3, _omitFieldNames ? '' : 'approvalToken')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeApprovalState clone() =>
      RuntimeApprovalState()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeApprovalState copyWith(void Function(RuntimeApprovalState) updates) =>
      super.copyWith((message) => updates(message as RuntimeApprovalState))
          as RuntimeApprovalState;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RuntimeApprovalState create() => RuntimeApprovalState._();
  @$core.override
  RuntimeApprovalState createEmptyInstance() => create();
  static $pb.PbList<RuntimeApprovalState> createRepeated() =>
      $pb.PbList<RuntimeApprovalState>();
  @$core.pragma('dart2js:noInline')
  static RuntimeApprovalState getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RuntimeApprovalState>(create);
  static RuntimeApprovalState? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get approvalId => $_getSZ(0);
  @$pb.TagNumber(1)
  set approvalId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasApprovalId() => $_has(0);
  @$pb.TagNumber(1)
  void clearApprovalId() => $_clearField(1);

  @$pb.TagNumber(2)
  ApprovalStatus get status => $_getN(1);
  @$pb.TagNumber(2)
  set status(ApprovalStatus value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasStatus() => $_has(1);
  @$pb.TagNumber(2)
  void clearStatus() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get approvalToken => $_getSZ(2);
  @$pb.TagNumber(3)
  set approvalToken($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasApprovalToken() => $_has(2);
  @$pb.TagNumber(3)
  void clearApprovalToken() => $_clearField(3);
}

class ConsumeApprovalRequest extends $pb.GeneratedMessage {
  factory ConsumeApprovalRequest({
    $core.String? approvalId,
  }) {
    final result = create();
    if (approvalId != null) result.approvalId = approvalId;
    return result;
  }

  ConsumeApprovalRequest._();

  factory ConsumeApprovalRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ConsumeApprovalRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ConsumeApprovalRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'approvalId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ConsumeApprovalRequest clone() =>
      ConsumeApprovalRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ConsumeApprovalRequest copyWith(
          void Function(ConsumeApprovalRequest) updates) =>
      super.copyWith((message) => updates(message as ConsumeApprovalRequest))
          as ConsumeApprovalRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ConsumeApprovalRequest create() => ConsumeApprovalRequest._();
  @$core.override
  ConsumeApprovalRequest createEmptyInstance() => create();
  static $pb.PbList<ConsumeApprovalRequest> createRepeated() =>
      $pb.PbList<ConsumeApprovalRequest>();
  @$core.pragma('dart2js:noInline')
  static ConsumeApprovalRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ConsumeApprovalRequest>(create);
  static ConsumeApprovalRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get approvalId => $_getSZ(0);
  @$pb.TagNumber(1)
  set approvalId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasApprovalId() => $_has(0);
  @$pb.TagNumber(1)
  void clearApprovalId() => $_clearField(1);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
