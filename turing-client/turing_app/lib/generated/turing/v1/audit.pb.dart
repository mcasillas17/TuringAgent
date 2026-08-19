// This is a generated file - do not edit.
//
// Generated from turing/v1/audit.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;

import '../../google/protobuf/timestamp.pb.dart' as $1;
import 'audit.pbenum.dart';
import 'common.pb.dart' as $2;

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'audit.pbenum.dart';

class AuditPayload extends $pb.GeneratedMessage {
  factory AuditPayload({
    AuditPayloadState? state,
    $core.String? toolName,
    $core.String? serverName,
    $core.String? phase,
    $core.String? status,
    $core.String? reason,
    $fixnum.Int64? durationMs,
    $core.String? errorCode,
    $core.String? provider,
    $core.String? displayName,
    $core.bool? unattended,
    $core.String? automationId,
    $core.String? automationName,
    $core.String? method,
    $core.String? requestId,
    $fixnum.Int64? deletedRuns,
    $fixnum.Int64? deletedMessages,
    $core.String? decisionComment,
    $core.bool? decisionCommentTruncated,
    $core.String? denialReason,
    $core.bool? denialReasonTruncated,
  }) {
    final result = create();
    if (state != null) result.state = state;
    if (toolName != null) result.toolName = toolName;
    if (serverName != null) result.serverName = serverName;
    if (phase != null) result.phase = phase;
    if (status != null) result.status = status;
    if (reason != null) result.reason = reason;
    if (durationMs != null) result.durationMs = durationMs;
    if (errorCode != null) result.errorCode = errorCode;
    if (provider != null) result.provider = provider;
    if (displayName != null) result.displayName = displayName;
    if (unattended != null) result.unattended = unattended;
    if (automationId != null) result.automationId = automationId;
    if (automationName != null) result.automationName = automationName;
    if (method != null) result.method = method;
    if (requestId != null) result.requestId = requestId;
    if (deletedRuns != null) result.deletedRuns = deletedRuns;
    if (deletedMessages != null) result.deletedMessages = deletedMessages;
    if (decisionComment != null) result.decisionComment = decisionComment;
    if (decisionCommentTruncated != null)
      result.decisionCommentTruncated = decisionCommentTruncated;
    if (denialReason != null) result.denialReason = denialReason;
    if (denialReasonTruncated != null)
      result.denialReasonTruncated = denialReasonTruncated;
    return result;
  }

  AuditPayload._();

  factory AuditPayload.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory AuditPayload.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'AuditPayload',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..e<AuditPayloadState>(
        1, _omitFieldNames ? '' : 'state', $pb.PbFieldType.OE,
        defaultOrMaker: AuditPayloadState.AUDIT_PAYLOAD_STATE_UNSPECIFIED,
        valueOf: AuditPayloadState.valueOf,
        enumValues: AuditPayloadState.values)
    ..aOS(2, _omitFieldNames ? '' : 'toolName')
    ..aOS(3, _omitFieldNames ? '' : 'serverName')
    ..aOS(4, _omitFieldNames ? '' : 'phase')
    ..aOS(5, _omitFieldNames ? '' : 'status')
    ..aOS(6, _omitFieldNames ? '' : 'reason')
    ..aInt64(7, _omitFieldNames ? '' : 'durationMs')
    ..aOS(8, _omitFieldNames ? '' : 'errorCode')
    ..aOS(9, _omitFieldNames ? '' : 'provider')
    ..aOS(10, _omitFieldNames ? '' : 'displayName')
    ..aOB(11, _omitFieldNames ? '' : 'unattended')
    ..aOS(12, _omitFieldNames ? '' : 'automationId')
    ..aOS(13, _omitFieldNames ? '' : 'automationName')
    ..aOS(14, _omitFieldNames ? '' : 'method')
    ..aOS(15, _omitFieldNames ? '' : 'requestId')
    ..aInt64(16, _omitFieldNames ? '' : 'deletedRuns')
    ..aInt64(17, _omitFieldNames ? '' : 'deletedMessages')
    ..aOS(18, _omitFieldNames ? '' : 'decisionComment')
    ..aOB(19, _omitFieldNames ? '' : 'decisionCommentTruncated')
    ..aOS(20, _omitFieldNames ? '' : 'denialReason')
    ..aOB(21, _omitFieldNames ? '' : 'denialReasonTruncated')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AuditPayload clone() => AuditPayload()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AuditPayload copyWith(void Function(AuditPayload) updates) =>
      super.copyWith((message) => updates(message as AuditPayload))
          as AuditPayload;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static AuditPayload create() => AuditPayload._();
  @$core.override
  AuditPayload createEmptyInstance() => create();
  static $pb.PbList<AuditPayload> createRepeated() =>
      $pb.PbList<AuditPayload>();
  @$core.pragma('dart2js:noInline')
  static AuditPayload getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<AuditPayload>(create);
  static AuditPayload? _defaultInstance;

  @$pb.TagNumber(1)
  AuditPayloadState get state => $_getN(0);
  @$pb.TagNumber(1)
  set state(AuditPayloadState value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasState() => $_has(0);
  @$pb.TagNumber(1)
  void clearState() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get toolName => $_getSZ(1);
  @$pb.TagNumber(2)
  set toolName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasToolName() => $_has(1);
  @$pb.TagNumber(2)
  void clearToolName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get serverName => $_getSZ(2);
  @$pb.TagNumber(3)
  set serverName($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasServerName() => $_has(2);
  @$pb.TagNumber(3)
  void clearServerName() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get phase => $_getSZ(3);
  @$pb.TagNumber(4)
  set phase($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasPhase() => $_has(3);
  @$pb.TagNumber(4)
  void clearPhase() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get status => $_getSZ(4);
  @$pb.TagNumber(5)
  set status($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasStatus() => $_has(4);
  @$pb.TagNumber(5)
  void clearStatus() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get reason => $_getSZ(5);
  @$pb.TagNumber(6)
  set reason($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasReason() => $_has(5);
  @$pb.TagNumber(6)
  void clearReason() => $_clearField(6);

  @$pb.TagNumber(7)
  $fixnum.Int64 get durationMs => $_getI64(6);
  @$pb.TagNumber(7)
  set durationMs($fixnum.Int64 value) => $_setInt64(6, value);
  @$pb.TagNumber(7)
  $core.bool hasDurationMs() => $_has(6);
  @$pb.TagNumber(7)
  void clearDurationMs() => $_clearField(7);

  @$pb.TagNumber(8)
  $core.String get errorCode => $_getSZ(7);
  @$pb.TagNumber(8)
  set errorCode($core.String value) => $_setString(7, value);
  @$pb.TagNumber(8)
  $core.bool hasErrorCode() => $_has(7);
  @$pb.TagNumber(8)
  void clearErrorCode() => $_clearField(8);

  @$pb.TagNumber(9)
  $core.String get provider => $_getSZ(8);
  @$pb.TagNumber(9)
  set provider($core.String value) => $_setString(8, value);
  @$pb.TagNumber(9)
  $core.bool hasProvider() => $_has(8);
  @$pb.TagNumber(9)
  void clearProvider() => $_clearField(9);

  @$pb.TagNumber(10)
  $core.String get displayName => $_getSZ(9);
  @$pb.TagNumber(10)
  set displayName($core.String value) => $_setString(9, value);
  @$pb.TagNumber(10)
  $core.bool hasDisplayName() => $_has(9);
  @$pb.TagNumber(10)
  void clearDisplayName() => $_clearField(10);

  @$pb.TagNumber(11)
  $core.bool get unattended => $_getBF(10);
  @$pb.TagNumber(11)
  set unattended($core.bool value) => $_setBool(10, value);
  @$pb.TagNumber(11)
  $core.bool hasUnattended() => $_has(10);
  @$pb.TagNumber(11)
  void clearUnattended() => $_clearField(11);

  @$pb.TagNumber(12)
  $core.String get automationId => $_getSZ(11);
  @$pb.TagNumber(12)
  set automationId($core.String value) => $_setString(11, value);
  @$pb.TagNumber(12)
  $core.bool hasAutomationId() => $_has(11);
  @$pb.TagNumber(12)
  void clearAutomationId() => $_clearField(12);

  @$pb.TagNumber(13)
  $core.String get automationName => $_getSZ(12);
  @$pb.TagNumber(13)
  set automationName($core.String value) => $_setString(12, value);
  @$pb.TagNumber(13)
  $core.bool hasAutomationName() => $_has(12);
  @$pb.TagNumber(13)
  void clearAutomationName() => $_clearField(13);

  @$pb.TagNumber(14)
  $core.String get method => $_getSZ(13);
  @$pb.TagNumber(14)
  set method($core.String value) => $_setString(13, value);
  @$pb.TagNumber(14)
  $core.bool hasMethod() => $_has(13);
  @$pb.TagNumber(14)
  void clearMethod() => $_clearField(14);

  @$pb.TagNumber(15)
  $core.String get requestId => $_getSZ(14);
  @$pb.TagNumber(15)
  set requestId($core.String value) => $_setString(14, value);
  @$pb.TagNumber(15)
  $core.bool hasRequestId() => $_has(14);
  @$pb.TagNumber(15)
  void clearRequestId() => $_clearField(15);

  @$pb.TagNumber(16)
  $fixnum.Int64 get deletedRuns => $_getI64(15);
  @$pb.TagNumber(16)
  set deletedRuns($fixnum.Int64 value) => $_setInt64(15, value);
  @$pb.TagNumber(16)
  $core.bool hasDeletedRuns() => $_has(15);
  @$pb.TagNumber(16)
  void clearDeletedRuns() => $_clearField(16);

  @$pb.TagNumber(17)
  $fixnum.Int64 get deletedMessages => $_getI64(16);
  @$pb.TagNumber(17)
  set deletedMessages($fixnum.Int64 value) => $_setInt64(16, value);
  @$pb.TagNumber(17)
  $core.bool hasDeletedMessages() => $_has(16);
  @$pb.TagNumber(17)
  void clearDeletedMessages() => $_clearField(17);

  /// Human-authored approval rationale, disclosed only for the two decision
  /// actions a person takes: decision_comment for approval.approved and
  /// denial_reason for approval.denied. Each carries the bounded (<= 512 byte)
  /// copy the approvals writer stored, never raw payload JSON and never the
  /// tool-policy `reason` above.
  ///
  /// Presence is the answer, not the value: a present empty string means the
  /// person decided and typed nothing, while an absent field means no human
  /// rationale was ever recorded for the row (an unattended automation grant,
  /// an expiry, a consumption) or that the stored value failed the read API's
  /// own checks.
  ///
  /// The *_truncated flags are present only when the stored audit payload
  /// itself carried that exact boolean; they report the writer's truncation,
  /// and this read path never truncates or repairs a value of its own.
  @$pb.TagNumber(18)
  $core.String get decisionComment => $_getSZ(17);
  @$pb.TagNumber(18)
  set decisionComment($core.String value) => $_setString(17, value);
  @$pb.TagNumber(18)
  $core.bool hasDecisionComment() => $_has(17);
  @$pb.TagNumber(18)
  void clearDecisionComment() => $_clearField(18);

  @$pb.TagNumber(19)
  $core.bool get decisionCommentTruncated => $_getBF(18);
  @$pb.TagNumber(19)
  set decisionCommentTruncated($core.bool value) => $_setBool(18, value);
  @$pb.TagNumber(19)
  $core.bool hasDecisionCommentTruncated() => $_has(18);
  @$pb.TagNumber(19)
  void clearDecisionCommentTruncated() => $_clearField(19);

  @$pb.TagNumber(20)
  $core.String get denialReason => $_getSZ(19);
  @$pb.TagNumber(20)
  set denialReason($core.String value) => $_setString(19, value);
  @$pb.TagNumber(20)
  $core.bool hasDenialReason() => $_has(19);
  @$pb.TagNumber(20)
  void clearDenialReason() => $_clearField(20);

  @$pb.TagNumber(21)
  $core.bool get denialReasonTruncated => $_getBF(20);
  @$pb.TagNumber(21)
  set denialReasonTruncated($core.bool value) => $_setBool(20, value);
  @$pb.TagNumber(21)
  $core.bool hasDenialReasonTruncated() => $_has(20);
  @$pb.TagNumber(21)
  void clearDenialReasonTruncated() => $_clearField(21);
}

class AuditEntry extends $pb.GeneratedMessage {
  factory AuditEntry({
    $core.String? auditId,
    $core.String? correlationId,
    $core.String? actorType,
    $core.String? actorId,
    $core.String? action,
    $core.String? target,
    AuditPayload? payload,
    $1.Timestamp? createdAt,
  }) {
    final result = create();
    if (auditId != null) result.auditId = auditId;
    if (correlationId != null) result.correlationId = correlationId;
    if (actorType != null) result.actorType = actorType;
    if (actorId != null) result.actorId = actorId;
    if (action != null) result.action = action;
    if (target != null) result.target = target;
    if (payload != null) result.payload = payload;
    if (createdAt != null) result.createdAt = createdAt;
    return result;
  }

  AuditEntry._();

  factory AuditEntry.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory AuditEntry.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'AuditEntry',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'auditId')
    ..aOS(2, _omitFieldNames ? '' : 'correlationId')
    ..aOS(3, _omitFieldNames ? '' : 'actorType')
    ..aOS(4, _omitFieldNames ? '' : 'actorId')
    ..aOS(5, _omitFieldNames ? '' : 'action')
    ..aOS(6, _omitFieldNames ? '' : 'target')
    ..aOM<AuditPayload>(7, _omitFieldNames ? '' : 'payload',
        subBuilder: AuditPayload.create)
    ..aOM<$1.Timestamp>(8, _omitFieldNames ? '' : 'createdAt',
        subBuilder: $1.Timestamp.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AuditEntry clone() => AuditEntry()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AuditEntry copyWith(void Function(AuditEntry) updates) =>
      super.copyWith((message) => updates(message as AuditEntry)) as AuditEntry;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static AuditEntry create() => AuditEntry._();
  @$core.override
  AuditEntry createEmptyInstance() => create();
  static $pb.PbList<AuditEntry> createRepeated() => $pb.PbList<AuditEntry>();
  @$core.pragma('dart2js:noInline')
  static AuditEntry getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<AuditEntry>(create);
  static AuditEntry? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get auditId => $_getSZ(0);
  @$pb.TagNumber(1)
  set auditId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAuditId() => $_has(0);
  @$pb.TagNumber(1)
  void clearAuditId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get correlationId => $_getSZ(1);
  @$pb.TagNumber(2)
  set correlationId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasCorrelationId() => $_has(1);
  @$pb.TagNumber(2)
  void clearCorrelationId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get actorType => $_getSZ(2);
  @$pb.TagNumber(3)
  set actorType($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasActorType() => $_has(2);
  @$pb.TagNumber(3)
  void clearActorType() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get actorId => $_getSZ(3);
  @$pb.TagNumber(4)
  set actorId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasActorId() => $_has(3);
  @$pb.TagNumber(4)
  void clearActorId() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get action => $_getSZ(4);
  @$pb.TagNumber(5)
  set action($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasAction() => $_has(4);
  @$pb.TagNumber(5)
  void clearAction() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get target => $_getSZ(5);
  @$pb.TagNumber(6)
  set target($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasTarget() => $_has(5);
  @$pb.TagNumber(6)
  void clearTarget() => $_clearField(6);

  @$pb.TagNumber(7)
  AuditPayload get payload => $_getN(6);
  @$pb.TagNumber(7)
  set payload(AuditPayload value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasPayload() => $_has(6);
  @$pb.TagNumber(7)
  void clearPayload() => $_clearField(7);
  @$pb.TagNumber(7)
  AuditPayload ensurePayload() => $_ensure(6);

  @$pb.TagNumber(8)
  $1.Timestamp get createdAt => $_getN(7);
  @$pb.TagNumber(8)
  set createdAt($1.Timestamp value) => $_setField(8, value);
  @$pb.TagNumber(8)
  $core.bool hasCreatedAt() => $_has(7);
  @$pb.TagNumber(8)
  void clearCreatedAt() => $_clearField(8);
  @$pb.TagNumber(8)
  $1.Timestamp ensureCreatedAt() => $_ensure(7);
}

class ListAuditEntriesRequest extends $pb.GeneratedMessage {
  factory ListAuditEntriesRequest({
    $core.String? correlationId,
    $core.String? action,
    $1.Timestamp? createdAtStart,
    $1.Timestamp? createdAtEnd,
    AuditOrder? order,
    $2.PageRequest? page,
  }) {
    final result = create();
    if (correlationId != null) result.correlationId = correlationId;
    if (action != null) result.action = action;
    if (createdAtStart != null) result.createdAtStart = createdAtStart;
    if (createdAtEnd != null) result.createdAtEnd = createdAtEnd;
    if (order != null) result.order = order;
    if (page != null) result.page = page;
    return result;
  }

  ListAuditEntriesRequest._();

  factory ListAuditEntriesRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListAuditEntriesRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListAuditEntriesRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'correlationId')
    ..aOS(2, _omitFieldNames ? '' : 'action')
    ..aOM<$1.Timestamp>(3, _omitFieldNames ? '' : 'createdAtStart',
        subBuilder: $1.Timestamp.create)
    ..aOM<$1.Timestamp>(4, _omitFieldNames ? '' : 'createdAtEnd',
        subBuilder: $1.Timestamp.create)
    ..e<AuditOrder>(5, _omitFieldNames ? '' : 'order', $pb.PbFieldType.OE,
        defaultOrMaker: AuditOrder.AUDIT_ORDER_UNSPECIFIED,
        valueOf: AuditOrder.valueOf,
        enumValues: AuditOrder.values)
    ..aOM<$2.PageRequest>(6, _omitFieldNames ? '' : 'page',
        subBuilder: $2.PageRequest.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAuditEntriesRequest clone() =>
      ListAuditEntriesRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAuditEntriesRequest copyWith(
          void Function(ListAuditEntriesRequest) updates) =>
      super.copyWith((message) => updates(message as ListAuditEntriesRequest))
          as ListAuditEntriesRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListAuditEntriesRequest create() => ListAuditEntriesRequest._();
  @$core.override
  ListAuditEntriesRequest createEmptyInstance() => create();
  static $pb.PbList<ListAuditEntriesRequest> createRepeated() =>
      $pb.PbList<ListAuditEntriesRequest>();
  @$core.pragma('dart2js:noInline')
  static ListAuditEntriesRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListAuditEntriesRequest>(create);
  static ListAuditEntriesRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get correlationId => $_getSZ(0);
  @$pb.TagNumber(1)
  set correlationId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasCorrelationId() => $_has(0);
  @$pb.TagNumber(1)
  void clearCorrelationId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get action => $_getSZ(1);
  @$pb.TagNumber(2)
  set action($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasAction() => $_has(1);
  @$pb.TagNumber(2)
  void clearAction() => $_clearField(2);

  /// Inclusive lower bound on created_at.
  @$pb.TagNumber(3)
  $1.Timestamp get createdAtStart => $_getN(2);
  @$pb.TagNumber(3)
  set createdAtStart($1.Timestamp value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasCreatedAtStart() => $_has(2);
  @$pb.TagNumber(3)
  void clearCreatedAtStart() => $_clearField(3);
  @$pb.TagNumber(3)
  $1.Timestamp ensureCreatedAtStart() => $_ensure(2);

  /// Exclusive upper bound on created_at.
  @$pb.TagNumber(4)
  $1.Timestamp get createdAtEnd => $_getN(3);
  @$pb.TagNumber(4)
  set createdAtEnd($1.Timestamp value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasCreatedAtEnd() => $_has(3);
  @$pb.TagNumber(4)
  void clearCreatedAtEnd() => $_clearField(4);
  @$pb.TagNumber(4)
  $1.Timestamp ensureCreatedAtEnd() => $_ensure(3);

  /// Unspecified means descending.
  @$pb.TagNumber(5)
  AuditOrder get order => $_getN(4);
  @$pb.TagNumber(5)
  set order(AuditOrder value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasOrder() => $_has(4);
  @$pb.TagNumber(5)
  void clearOrder() => $_clearField(5);

  @$pb.TagNumber(6)
  $2.PageRequest get page => $_getN(5);
  @$pb.TagNumber(6)
  set page($2.PageRequest value) => $_setField(6, value);
  @$pb.TagNumber(6)
  $core.bool hasPage() => $_has(5);
  @$pb.TagNumber(6)
  void clearPage() => $_clearField(6);
  @$pb.TagNumber(6)
  $2.PageRequest ensurePage() => $_ensure(5);
}

class ListAuditEntriesResponse extends $pb.GeneratedMessage {
  factory ListAuditEntriesResponse({
    $core.Iterable<AuditEntry>? entries,
    $2.PageResponse? page,
  }) {
    final result = create();
    if (entries != null) result.entries.addAll(entries);
    if (page != null) result.page = page;
    return result;
  }

  ListAuditEntriesResponse._();

  factory ListAuditEntriesResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListAuditEntriesResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListAuditEntriesResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..pc<AuditEntry>(1, _omitFieldNames ? '' : 'entries', $pb.PbFieldType.PM,
        subBuilder: AuditEntry.create)
    ..aOM<$2.PageResponse>(2, _omitFieldNames ? '' : 'page',
        subBuilder: $2.PageResponse.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAuditEntriesResponse clone() =>
      ListAuditEntriesResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAuditEntriesResponse copyWith(
          void Function(ListAuditEntriesResponse) updates) =>
      super.copyWith((message) => updates(message as ListAuditEntriesResponse))
          as ListAuditEntriesResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListAuditEntriesResponse create() => ListAuditEntriesResponse._();
  @$core.override
  ListAuditEntriesResponse createEmptyInstance() => create();
  static $pb.PbList<ListAuditEntriesResponse> createRepeated() =>
      $pb.PbList<ListAuditEntriesResponse>();
  @$core.pragma('dart2js:noInline')
  static ListAuditEntriesResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListAuditEntriesResponse>(create);
  static ListAuditEntriesResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<AuditEntry> get entries => $_getList(0);

  @$pb.TagNumber(2)
  $2.PageResponse get page => $_getN(1);
  @$pb.TagNumber(2)
  set page($2.PageResponse value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasPage() => $_has(1);
  @$pb.TagNumber(2)
  void clearPage() => $_clearField(2);
  @$pb.TagNumber(2)
  $2.PageResponse ensurePage() => $_ensure(1);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
