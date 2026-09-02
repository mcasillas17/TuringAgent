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
