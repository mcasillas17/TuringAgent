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

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;

import 'approvals.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'approvals.pbenum.dart';

class ApproveApprovalRequest extends $pb.GeneratedMessage {
  factory ApproveApprovalRequest({
    $core.String? approvalId,
    $core.String? comment,
    $core.String? previewHash,
    $core.String? argsHash,
  }) {
    final result = create();
    if (approvalId != null) result.approvalId = approvalId;
    if (comment != null) result.comment = comment;
    if (previewHash != null) result.previewHash = previewHash;
    if (argsHash != null) result.argsHash = argsHash;
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
    ..aOS(3, _omitFieldNames ? '' : 'previewHash')
    ..aOS(4, _omitFieldNames ? '' : 'argsHash')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApproveApprovalRequest clone() => deepCopy();
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

  @$pb.TagNumber(3)
  $core.String get previewHash => $_getSZ(2);
  @$pb.TagNumber(3)
  set previewHash($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasPreviewHash() => $_has(2);
  @$pb.TagNumber(3)
  void clearPreviewHash() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get argsHash => $_getSZ(3);
  @$pb.TagNumber(4)
  set argsHash($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasArgsHash() => $_has(3);
  @$pb.TagNumber(4)
  void clearArgsHash() => $_clearField(4);
}

class GetApprovalDetailsRequest extends $pb.GeneratedMessage {
  factory GetApprovalDetailsRequest({
    $core.String? approvalId,
    $core.bool? refreshPreview,
  }) {
    final result = create();
    if (approvalId != null) result.approvalId = approvalId;
    if (refreshPreview != null) result.refreshPreview = refreshPreview;
    return result;
  }

  GetApprovalDetailsRequest._();

  factory GetApprovalDetailsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetApprovalDetailsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetApprovalDetailsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'approvalId')
    ..aOB(2, _omitFieldNames ? '' : 'refreshPreview')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetApprovalDetailsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetApprovalDetailsRequest copyWith(
          void Function(GetApprovalDetailsRequest) updates) =>
      super.copyWith((message) => updates(message as GetApprovalDetailsRequest))
          as GetApprovalDetailsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetApprovalDetailsRequest create() => GetApprovalDetailsRequest._();
  @$core.override
  GetApprovalDetailsRequest createEmptyInstance() => create();
  static $pb.PbList<GetApprovalDetailsRequest> createRepeated() =>
      $pb.PbList<GetApprovalDetailsRequest>();
  @$core.pragma('dart2js:noInline')
  static GetApprovalDetailsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetApprovalDetailsRequest>(create);
  static GetApprovalDetailsRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get approvalId => $_getSZ(0);
  @$pb.TagNumber(1)
  set approvalId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasApprovalId() => $_has(0);
  @$pb.TagNumber(1)
  void clearApprovalId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.bool get refreshPreview => $_getBF(1);
  @$pb.TagNumber(2)
  set refreshPreview($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRefreshPreview() => $_has(1);
  @$pb.TagNumber(2)
  void clearRefreshPreview() => $_clearField(2);
}

class ApprovalDetails extends $pb.GeneratedMessage {
  factory ApprovalDetails({
    $core.String? approvalId,
    $core.String? sessionId,
    $core.String? runId,
    $core.String? toolCallId,
    $core.String? toolName,
    $core.String? serverName,
    $core.String? argsHash,
    $core.String? previewHash,
    $core.String? expiresAt,
    ApprovalStatus? status,
    ApprovalPreviewState? previewState,
    $core.String? argumentsJson,
    FileMutationPreview? filePreview,
    $core.bool? canApprove,
    $core.bool? canDeny,
  }) {
    final result = create();
    if (approvalId != null) result.approvalId = approvalId;
    if (sessionId != null) result.sessionId = sessionId;
    if (runId != null) result.runId = runId;
    if (toolCallId != null) result.toolCallId = toolCallId;
    if (toolName != null) result.toolName = toolName;
    if (serverName != null) result.serverName = serverName;
    if (argsHash != null) result.argsHash = argsHash;
    if (previewHash != null) result.previewHash = previewHash;
    if (expiresAt != null) result.expiresAt = expiresAt;
    if (status != null) result.status = status;
    if (previewState != null) result.previewState = previewState;
    if (argumentsJson != null) result.argumentsJson = argumentsJson;
    if (filePreview != null) result.filePreview = filePreview;
    if (canApprove != null) result.canApprove = canApprove;
    if (canDeny != null) result.canDeny = canDeny;
    return result;
  }

  ApprovalDetails._();

  factory ApprovalDetails.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ApprovalDetails.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ApprovalDetails',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'approvalId')
    ..aOS(2, _omitFieldNames ? '' : 'sessionId')
    ..aOS(3, _omitFieldNames ? '' : 'runId')
    ..aOS(4, _omitFieldNames ? '' : 'toolCallId')
    ..aOS(5, _omitFieldNames ? '' : 'toolName')
    ..aOS(6, _omitFieldNames ? '' : 'serverName')
    ..aOS(7, _omitFieldNames ? '' : 'argsHash')
    ..aOS(8, _omitFieldNames ? '' : 'previewHash')
    ..aOS(9, _omitFieldNames ? '' : 'expiresAt')
    ..aE<ApprovalStatus>(10, _omitFieldNames ? '' : 'status',
        enumValues: ApprovalStatus.values)
    ..aE<ApprovalPreviewState>(11, _omitFieldNames ? '' : 'previewState',
        enumValues: ApprovalPreviewState.values)
    ..aOS(12, _omitFieldNames ? '' : 'argumentsJson')
    ..aOM<FileMutationPreview>(13, _omitFieldNames ? '' : 'filePreview',
        subBuilder: FileMutationPreview.create)
    ..aOB(14, _omitFieldNames ? '' : 'canApprove')
    ..aOB(15, _omitFieldNames ? '' : 'canDeny')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApprovalDetails clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApprovalDetails copyWith(void Function(ApprovalDetails) updates) =>
      super.copyWith((message) => updates(message as ApprovalDetails))
          as ApprovalDetails;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ApprovalDetails create() => ApprovalDetails._();
  @$core.override
  ApprovalDetails createEmptyInstance() => create();
  static $pb.PbList<ApprovalDetails> createRepeated() =>
      $pb.PbList<ApprovalDetails>();
  @$core.pragma('dart2js:noInline')
  static ApprovalDetails getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ApprovalDetails>(create);
  static ApprovalDetails? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get approvalId => $_getSZ(0);
  @$pb.TagNumber(1)
  set approvalId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasApprovalId() => $_has(0);
  @$pb.TagNumber(1)
  void clearApprovalId() => $_clearField(1);

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
  $core.String get toolCallId => $_getSZ(3);
  @$pb.TagNumber(4)
  set toolCallId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasToolCallId() => $_has(3);
  @$pb.TagNumber(4)
  void clearToolCallId() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get toolName => $_getSZ(4);
  @$pb.TagNumber(5)
  set toolName($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasToolName() => $_has(4);
  @$pb.TagNumber(5)
  void clearToolName() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get serverName => $_getSZ(5);
  @$pb.TagNumber(6)
  set serverName($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasServerName() => $_has(5);
  @$pb.TagNumber(6)
  void clearServerName() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.String get argsHash => $_getSZ(6);
  @$pb.TagNumber(7)
  set argsHash($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasArgsHash() => $_has(6);
  @$pb.TagNumber(7)
  void clearArgsHash() => $_clearField(7);

  @$pb.TagNumber(8)
  $core.String get previewHash => $_getSZ(7);
  @$pb.TagNumber(8)
  set previewHash($core.String value) => $_setString(7, value);
  @$pb.TagNumber(8)
  $core.bool hasPreviewHash() => $_has(7);
  @$pb.TagNumber(8)
  void clearPreviewHash() => $_clearField(8);

  @$pb.TagNumber(9)
  $core.String get expiresAt => $_getSZ(8);
  @$pb.TagNumber(9)
  set expiresAt($core.String value) => $_setString(8, value);
  @$pb.TagNumber(9)
  $core.bool hasExpiresAt() => $_has(8);
  @$pb.TagNumber(9)
  void clearExpiresAt() => $_clearField(9);

  @$pb.TagNumber(10)
  ApprovalStatus get status => $_getN(9);
  @$pb.TagNumber(10)
  set status(ApprovalStatus value) => $_setField(10, value);
  @$pb.TagNumber(10)
  $core.bool hasStatus() => $_has(9);
  @$pb.TagNumber(10)
  void clearStatus() => $_clearField(10);

  @$pb.TagNumber(11)
  ApprovalPreviewState get previewState => $_getN(10);
  @$pb.TagNumber(11)
  set previewState(ApprovalPreviewState value) => $_setField(11, value);
  @$pb.TagNumber(11)
  $core.bool hasPreviewState() => $_has(10);
  @$pb.TagNumber(11)
  void clearPreviewState() => $_clearField(11);

  @$pb.TagNumber(12)
  $core.String get argumentsJson => $_getSZ(11);
  @$pb.TagNumber(12)
  set argumentsJson($core.String value) => $_setString(11, value);
  @$pb.TagNumber(12)
  $core.bool hasArgumentsJson() => $_has(11);
  @$pb.TagNumber(12)
  void clearArgumentsJson() => $_clearField(12);

  @$pb.TagNumber(13)
  FileMutationPreview get filePreview => $_getN(12);
  @$pb.TagNumber(13)
  set filePreview(FileMutationPreview value) => $_setField(13, value);
  @$pb.TagNumber(13)
  $core.bool hasFilePreview() => $_has(12);
  @$pb.TagNumber(13)
  void clearFilePreview() => $_clearField(13);
  @$pb.TagNumber(13)
  FileMutationPreview ensureFilePreview() => $_ensure(12);

  @$pb.TagNumber(14)
  $core.bool get canApprove => $_getBF(13);
  @$pb.TagNumber(14)
  set canApprove($core.bool value) => $_setBool(13, value);
  @$pb.TagNumber(14)
  $core.bool hasCanApprove() => $_has(13);
  @$pb.TagNumber(14)
  void clearCanApprove() => $_clearField(14);

  @$pb.TagNumber(15)
  $core.bool get canDeny => $_getBF(14);
  @$pb.TagNumber(15)
  set canDeny($core.bool value) => $_setBool(14, value);
  @$pb.TagNumber(15)
  $core.bool hasCanDeny() => $_has(14);
  @$pb.TagNumber(15)
  void clearCanDeny() => $_clearField(15);
}

class FileMutationPreview extends $pb.GeneratedMessage {
  factory FileMutationPreview({
    $core.String? logicalPath,
    $core.String? physicalPath,
    $core.String? operation,
    $core.bool? beforeExists,
    $core.String? beforeHash,
    $core.String? afterHash,
    $core.String? beforeText,
    $core.String? afterText,
    $core.String? unifiedDiff,
  }) {
    final result = create();
    if (logicalPath != null) result.logicalPath = logicalPath;
    if (physicalPath != null) result.physicalPath = physicalPath;
    if (operation != null) result.operation = operation;
    if (beforeExists != null) result.beforeExists = beforeExists;
    if (beforeHash != null) result.beforeHash = beforeHash;
    if (afterHash != null) result.afterHash = afterHash;
    if (beforeText != null) result.beforeText = beforeText;
    if (afterText != null) result.afterText = afterText;
    if (unifiedDiff != null) result.unifiedDiff = unifiedDiff;
    return result;
  }

  FileMutationPreview._();

  factory FileMutationPreview.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory FileMutationPreview.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'FileMutationPreview',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'logicalPath')
    ..aOS(2, _omitFieldNames ? '' : 'physicalPath')
    ..aOS(3, _omitFieldNames ? '' : 'operation')
    ..aOB(4, _omitFieldNames ? '' : 'beforeExists')
    ..aOS(5, _omitFieldNames ? '' : 'beforeHash')
    ..aOS(6, _omitFieldNames ? '' : 'afterHash')
    ..aOS(7, _omitFieldNames ? '' : 'beforeText')
    ..aOS(8, _omitFieldNames ? '' : 'afterText')
    ..aOS(9, _omitFieldNames ? '' : 'unifiedDiff')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  FileMutationPreview clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  FileMutationPreview copyWith(void Function(FileMutationPreview) updates) =>
      super.copyWith((message) => updates(message as FileMutationPreview))
          as FileMutationPreview;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static FileMutationPreview create() => FileMutationPreview._();
  @$core.override
  FileMutationPreview createEmptyInstance() => create();
  static $pb.PbList<FileMutationPreview> createRepeated() =>
      $pb.PbList<FileMutationPreview>();
  @$core.pragma('dart2js:noInline')
  static FileMutationPreview getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<FileMutationPreview>(create);
  static FileMutationPreview? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get logicalPath => $_getSZ(0);
  @$pb.TagNumber(1)
  set logicalPath($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasLogicalPath() => $_has(0);
  @$pb.TagNumber(1)
  void clearLogicalPath() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get physicalPath => $_getSZ(1);
  @$pb.TagNumber(2)
  set physicalPath($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasPhysicalPath() => $_has(1);
  @$pb.TagNumber(2)
  void clearPhysicalPath() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get operation => $_getSZ(2);
  @$pb.TagNumber(3)
  set operation($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasOperation() => $_has(2);
  @$pb.TagNumber(3)
  void clearOperation() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.bool get beforeExists => $_getBF(3);
  @$pb.TagNumber(4)
  set beforeExists($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasBeforeExists() => $_has(3);
  @$pb.TagNumber(4)
  void clearBeforeExists() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get beforeHash => $_getSZ(4);
  @$pb.TagNumber(5)
  set beforeHash($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasBeforeHash() => $_has(4);
  @$pb.TagNumber(5)
  void clearBeforeHash() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get afterHash => $_getSZ(5);
  @$pb.TagNumber(6)
  set afterHash($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasAfterHash() => $_has(5);
  @$pb.TagNumber(6)
  void clearAfterHash() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.String get beforeText => $_getSZ(6);
  @$pb.TagNumber(7)
  set beforeText($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasBeforeText() => $_has(6);
  @$pb.TagNumber(7)
  void clearBeforeText() => $_clearField(7);

  @$pb.TagNumber(8)
  $core.String get afterText => $_getSZ(7);
  @$pb.TagNumber(8)
  set afterText($core.String value) => $_setString(7, value);
  @$pb.TagNumber(8)
  $core.bool hasAfterText() => $_has(7);
  @$pb.TagNumber(8)
  void clearAfterText() => $_clearField(8);

  @$pb.TagNumber(9)
  $core.String get unifiedDiff => $_getSZ(8);
  @$pb.TagNumber(9)
  set unifiedDiff($core.String value) => $_setString(8, value);
  @$pb.TagNumber(9)
  $core.bool hasUnifiedDiff() => $_has(8);
  @$pb.TagNumber(9)
  void clearUnifiedDiff() => $_clearField(9);
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
  DenyApprovalRequest clone() => deepCopy();
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
    SandboxArtifactReservation? reservation,
  }) {
    final result = create();
    if (approvalId != null) result.approvalId = approvalId;
    if (status != null) result.status = status;
    if (reservation != null) result.reservation = reservation;
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
    ..aE<ApprovalStatus>(2, _omitFieldNames ? '' : 'status',
        enumValues: ApprovalStatus.values)
    ..aOM<SandboxArtifactReservation>(3, _omitFieldNames ? '' : 'reservation',
        subBuilder: SandboxArtifactReservation.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ApprovalResponse clone() => deepCopy();
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

  /// Present only for a consume that reserved a sandbox artifact, which is the
  /// manifest row the write is allowed to land in.
  @$pb.TagNumber(3)
  SandboxArtifactReservation get reservation => $_getN(2);
  @$pb.TagNumber(3)
  set reservation(SandboxArtifactReservation value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasReservation() => $_has(2);
  @$pb.TagNumber(3)
  void clearReservation() => $_clearField(3);
  @$pb.TagNumber(3)
  SandboxArtifactReservation ensureReservation() => $_ensure(2);
}

/// SandboxArtifactReservation is the orchestrator's durable promise that one
/// file write is accounted for before any bytes exist.
class SandboxArtifactReservation extends $pb.GeneratedMessage {
  factory SandboxArtifactReservation({
    $core.String? artifactId,
    $core.String? physicalPath,
    $core.String? policy,
    $fixnum.Int64? deletionGeneration,
  }) {
    final result = create();
    if (artifactId != null) result.artifactId = artifactId;
    if (physicalPath != null) result.physicalPath = physicalPath;
    if (policy != null) result.policy = policy;
    if (deletionGeneration != null)
      result.deletionGeneration = deletionGeneration;
    return result;
  }

  SandboxArtifactReservation._();

  factory SandboxArtifactReservation.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SandboxArtifactReservation.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SandboxArtifactReservation',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'artifactId')
    ..aOS(2, _omitFieldNames ? '' : 'physicalPath')
    ..aOS(3, _omitFieldNames ? '' : 'policy')
    ..aInt64(4, _omitFieldNames ? '' : 'deletionGeneration')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SandboxArtifactReservation clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SandboxArtifactReservation copyWith(
          void Function(SandboxArtifactReservation) updates) =>
      super.copyWith(
              (message) => updates(message as SandboxArtifactReservation))
          as SandboxArtifactReservation;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SandboxArtifactReservation create() => SandboxArtifactReservation._();
  @$core.override
  SandboxArtifactReservation createEmptyInstance() => create();
  static $pb.PbList<SandboxArtifactReservation> createRepeated() =>
      $pb.PbList<SandboxArtifactReservation>();
  @$core.pragma('dart2js:noInline')
  static SandboxArtifactReservation getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SandboxArtifactReservation>(create);
  static SandboxArtifactReservation? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get artifactId => $_getSZ(0);
  @$pb.TagNumber(1)
  set artifactId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasArtifactId() => $_has(0);
  @$pb.TagNumber(1)
  void clearArtifactId() => $_clearField(1);

  /// Server-derived location the write must land in; mcp-files computes the same
  /// path independently and refuses to proceed if the two disagree.
  @$pb.TagNumber(2)
  $core.String get physicalPath => $_getSZ(1);
  @$pb.TagNumber(2)
  set physicalPath($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasPhysicalPath() => $_has(1);
  @$pb.TagNumber(2)
  void clearPhysicalPath() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get policy => $_getSZ(2);
  @$pb.TagNumber(3)
  set policy($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasPolicy() => $_has(2);
  @$pb.TagNumber(3)
  void clearPolicy() => $_clearField(3);

  @$pb.TagNumber(4)
  $fixnum.Int64 get deletionGeneration => $_getI64(3);
  @$pb.TagNumber(4)
  set deletionGeneration($fixnum.Int64 value) => $_setInt64(3, value);
  @$pb.TagNumber(4)
  $core.bool hasDeletionGeneration() => $_has(3);
  @$pb.TagNumber(4)
  void clearDeletionGeneration() => $_clearField(4);
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
  GetApprovalForRuntimeRequest clone() => deepCopy();
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
    ..aE<ApprovalStatus>(2, _omitFieldNames ? '' : 'status',
        enumValues: ApprovalStatus.values)
    ..aOS(3, _omitFieldNames ? '' : 'approvalToken')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RuntimeApprovalState clone() => deepCopy();
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
    $core.String? provenanceToken,
    $core.String? physicalPath,
  }) {
    final result = create();
    if (approvalId != null) result.approvalId = approvalId;
    if (provenanceToken != null) result.provenanceToken = provenanceToken;
    if (physicalPath != null) result.physicalPath = physicalPath;
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
    ..aOS(2, _omitFieldNames ? '' : 'provenanceToken')
    ..aOS(3, _omitFieldNames ? '' : 'physicalPath')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ConsumeApprovalRequest clone() => deepCopy();
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

  /// The server-issued provenance capability for the same tool call. It is what
  /// ties the consume to a session, run and path, so the reservation cannot be
  /// taken for work nobody authorised.
  @$pb.TagNumber(2)
  $core.String get provenanceToken => $_getSZ(1);
  @$pb.TagNumber(2)
  set provenanceToken($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasProvenanceToken() => $_has(1);
  @$pb.TagNumber(2)
  void clearProvenanceToken() => $_clearField(2);

  /// Where the caller resolved the write to. It must equal either the run-scoped
  /// location the server derives or the legacy root path the capability already
  /// names; anything else is refused.
  @$pb.TagNumber(3)
  $core.String get physicalPath => $_getSZ(2);
  @$pb.TagNumber(3)
  set physicalPath($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasPhysicalPath() => $_has(2);
  @$pb.TagNumber(3)
  void clearPhysicalPath() => $_clearField(3);
}

/// FinalizeSandboxArtifactRequest reports the outcome of a reserved write over
/// the same authenticated internal channel that consumed the approval.
class FinalizeSandboxArtifactRequest extends $pb.GeneratedMessage {
  factory FinalizeSandboxArtifactRequest({
    $core.String? artifactId,
    $core.String? provenanceToken,
    $core.bool? committed,
  }) {
    final result = create();
    if (artifactId != null) result.artifactId = artifactId;
    if (provenanceToken != null) result.provenanceToken = provenanceToken;
    if (committed != null) result.committed = committed;
    return result;
  }

  FinalizeSandboxArtifactRequest._();

  factory FinalizeSandboxArtifactRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory FinalizeSandboxArtifactRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'FinalizeSandboxArtifactRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'artifactId')
    ..aOS(2, _omitFieldNames ? '' : 'provenanceToken')
    ..aOB(3, _omitFieldNames ? '' : 'committed')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  FinalizeSandboxArtifactRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  FinalizeSandboxArtifactRequest copyWith(
          void Function(FinalizeSandboxArtifactRequest) updates) =>
      super.copyWith(
              (message) => updates(message as FinalizeSandboxArtifactRequest))
          as FinalizeSandboxArtifactRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static FinalizeSandboxArtifactRequest create() =>
      FinalizeSandboxArtifactRequest._();
  @$core.override
  FinalizeSandboxArtifactRequest createEmptyInstance() => create();
  static $pb.PbList<FinalizeSandboxArtifactRequest> createRepeated() =>
      $pb.PbList<FinalizeSandboxArtifactRequest>();
  @$core.pragma('dart2js:noInline')
  static FinalizeSandboxArtifactRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<FinalizeSandboxArtifactRequest>(create);
  static FinalizeSandboxArtifactRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get artifactId => $_getSZ(0);
  @$pb.TagNumber(1)
  set artifactId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasArtifactId() => $_has(0);
  @$pb.TagNumber(1)
  void clearArtifactId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get provenanceToken => $_getSZ(1);
  @$pb.TagNumber(2)
  set provenanceToken($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasProvenanceToken() => $_has(1);
  @$pb.TagNumber(2)
  void clearProvenanceToken() => $_clearField(2);

  /// True once the bytes are durably on the file system. False withdraws a
  /// reservation whose write never happened; it never removes a finalized
  /// artifact.
  @$pb.TagNumber(3)
  $core.bool get committed => $_getBF(2);
  @$pb.TagNumber(3)
  set committed($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasCommitted() => $_has(2);
  @$pb.TagNumber(3)
  void clearCommitted() => $_clearField(3);
}

class FinalizeSandboxArtifactResponse extends $pb.GeneratedMessage {
  factory FinalizeSandboxArtifactResponse({
    $core.String? artifactId,
    $core.String? state,
  }) {
    final result = create();
    if (artifactId != null) result.artifactId = artifactId;
    if (state != null) result.state = state;
    return result;
  }

  FinalizeSandboxArtifactResponse._();

  factory FinalizeSandboxArtifactResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory FinalizeSandboxArtifactResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'FinalizeSandboxArtifactResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'artifactId')
    ..aOS(2, _omitFieldNames ? '' : 'state')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  FinalizeSandboxArtifactResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  FinalizeSandboxArtifactResponse copyWith(
          void Function(FinalizeSandboxArtifactResponse) updates) =>
      super.copyWith(
              (message) => updates(message as FinalizeSandboxArtifactResponse))
          as FinalizeSandboxArtifactResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static FinalizeSandboxArtifactResponse create() =>
      FinalizeSandboxArtifactResponse._();
  @$core.override
  FinalizeSandboxArtifactResponse createEmptyInstance() => create();
  static $pb.PbList<FinalizeSandboxArtifactResponse> createRepeated() =>
      $pb.PbList<FinalizeSandboxArtifactResponse>();
  @$core.pragma('dart2js:noInline')
  static FinalizeSandboxArtifactResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<FinalizeSandboxArtifactResponse>(
          create);
  static FinalizeSandboxArtifactResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get artifactId => $_getSZ(0);
  @$pb.TagNumber(1)
  set artifactId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasArtifactId() => $_has(0);
  @$pb.TagNumber(1)
  void clearArtifactId() => $_clearField(1);

  /// "ready" when the artifact is recorded, "released" when an unwritten
  /// reservation was withdrawn.
  @$pb.TagNumber(2)
  $core.String get state => $_getSZ(1);
  @$pb.TagNumber(2)
  set state($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasState() => $_has(1);
  @$pb.TagNumber(2)
  void clearState() => $_clearField(2);
}

/// CheckSessionCapabilityRequest asks whether a capability's session is still
/// accepting work. It is how a read-only tool gets a server-side answer, since
/// nothing about a read touches the artifact manifest.
class CheckSessionCapabilityRequest extends $pb.GeneratedMessage {
  factory CheckSessionCapabilityRequest({
    $core.String? provenanceToken,
  }) {
    final result = create();
    if (provenanceToken != null) result.provenanceToken = provenanceToken;
    return result;
  }

  CheckSessionCapabilityRequest._();

  factory CheckSessionCapabilityRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory CheckSessionCapabilityRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CheckSessionCapabilityRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'provenanceToken')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CheckSessionCapabilityRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CheckSessionCapabilityRequest copyWith(
          void Function(CheckSessionCapabilityRequest) updates) =>
      super.copyWith(
              (message) => updates(message as CheckSessionCapabilityRequest))
          as CheckSessionCapabilityRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static CheckSessionCapabilityRequest create() =>
      CheckSessionCapabilityRequest._();
  @$core.override
  CheckSessionCapabilityRequest createEmptyInstance() => create();
  static $pb.PbList<CheckSessionCapabilityRequest> createRepeated() =>
      $pb.PbList<CheckSessionCapabilityRequest>();
  @$core.pragma('dart2js:noInline')
  static CheckSessionCapabilityRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CheckSessionCapabilityRequest>(create);
  static CheckSessionCapabilityRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get provenanceToken => $_getSZ(0);
  @$pb.TagNumber(1)
  set provenanceToken($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasProvenanceToken() => $_has(0);
  @$pb.TagNumber(1)
  void clearProvenanceToken() => $_clearField(1);
}

class SessionCapabilityState extends $pb.GeneratedMessage {
  factory SessionCapabilityState({
    $core.bool? active,
    $fixnum.Int64? deletionGeneration,
  }) {
    final result = create();
    if (active != null) result.active = active;
    if (deletionGeneration != null)
      result.deletionGeneration = deletionGeneration;
    return result;
  }

  SessionCapabilityState._();

  factory SessionCapabilityState.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SessionCapabilityState.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SessionCapabilityState',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOB(1, _omitFieldNames ? '' : 'active')
    ..aInt64(2, _omitFieldNames ? '' : 'deletionGeneration')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SessionCapabilityState clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SessionCapabilityState copyWith(
          void Function(SessionCapabilityState) updates) =>
      super.copyWith((message) => updates(message as SessionCapabilityState))
          as SessionCapabilityState;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SessionCapabilityState create() => SessionCapabilityState._();
  @$core.override
  SessionCapabilityState createEmptyInstance() => create();
  static $pb.PbList<SessionCapabilityState> createRepeated() =>
      $pb.PbList<SessionCapabilityState>();
  @$core.pragma('dart2js:noInline')
  static SessionCapabilityState getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SessionCapabilityState>(create);
  static SessionCapabilityState? _defaultInstance;

  /// True only when the session exists, is not being withdrawn, and is still on
  /// the withdrawal generation the capability was issued against.
  @$pb.TagNumber(1)
  $core.bool get active => $_getBF(0);
  @$pb.TagNumber(1)
  set active($core.bool value) => $_setBool(0, value);
  @$pb.TagNumber(1)
  $core.bool hasActive() => $_has(0);
  @$pb.TagNumber(1)
  void clearActive() => $_clearField(1);

  @$pb.TagNumber(2)
  $fixnum.Int64 get deletionGeneration => $_getI64(1);
  @$pb.TagNumber(2)
  set deletionGeneration($fixnum.Int64 value) => $_setInt64(1, value);
  @$pb.TagNumber(2)
  $core.bool hasDeletionGeneration() => $_has(1);
  @$pb.TagNumber(2)
  void clearDeletionGeneration() => $_clearField(2);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
