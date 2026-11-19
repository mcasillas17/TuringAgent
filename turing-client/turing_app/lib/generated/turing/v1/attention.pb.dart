// This is a generated file - do not edit.
//
// Generated from turing/v1/attention.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;
import 'package:protobuf/well_known_types/google/protobuf/timestamp.pb.dart'
    as $1;

import 'attention.pbenum.dart';
import 'common.pb.dart' as $2;

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'attention.pbenum.dart';

/// One owner obligation. It never carries tool arguments, message content or
/// transcripts: only what a row's summary shows and the ids its action needs.
class AttentionRow extends $pb.GeneratedMessage {
  factory AttentionRow({
    AttentionStage? stage,
    AttentionSourceKind? kind,
    $1.Timestamp? since,
    $core.String? sessionId,
    $core.String? sessionTitle,
    $core.String? approvalId,
    $core.String? runId,
    $core.String? delegationId,
    $core.String? automationId,
    $core.String? automationName,
    $core.String? occurrenceKey,
    $core.String? cause,
  }) {
    final result = AttentionRow._();
    if (stage != null) result.stage = stage;
    if (kind != null) result.kind = kind;
    if (since != null) result.since = since;
    if (sessionId != null) result.sessionId = sessionId;
    if (sessionTitle != null) result.sessionTitle = sessionTitle;
    if (approvalId != null) result.approvalId = approvalId;
    if (runId != null) result.runId = runId;
    if (delegationId != null) result.delegationId = delegationId;
    if (automationId != null) result.automationId = automationId;
    if (automationName != null) result.automationName = automationName;
    if (occurrenceKey != null) result.occurrenceKey = occurrenceKey;
    if (cause != null) result.cause = cause;
    return result;
  }

  AttentionRow._();

  factory AttentionRow.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      AttentionRow()..mergeFromBuffer(data, registry);
  factory AttentionRow.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      AttentionRow()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'AttentionRow',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: AttentionRow.$_createMessage)
    ..aE<AttentionStage>(1, _omitFieldNames ? '' : 'stage',
        enumValues: AttentionStage.values)
    ..aE<AttentionSourceKind>(2, _omitFieldNames ? '' : 'kind',
        enumValues: AttentionSourceKind.values)
    ..aOM<$1.Timestamp>(3, _omitFieldNames ? '' : 'since',
        subBuilder: $1.Timestamp.$_createMessage)
    ..aOS(4, _omitFieldNames ? '' : 'sessionId')
    ..aOS(5, _omitFieldNames ? '' : 'sessionTitle')
    ..aOS(6, _omitFieldNames ? '' : 'approvalId')
    ..aOS(7, _omitFieldNames ? '' : 'runId')
    ..aOS(8, _omitFieldNames ? '' : 'delegationId')
    ..aOS(9, _omitFieldNames ? '' : 'automationId')
    ..aOS(10, _omitFieldNames ? '' : 'automationName')
    ..aOS(11, _omitFieldNames ? '' : 'occurrenceKey')
    ..aOS(12, _omitFieldNames ? '' : 'cause')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AttentionRow clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AttentionRow copyWith(void Function(AttentionRow) updates) =>
      super.copyWith((message) => updates(message as AttentionRow))
          as AttentionRow;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use AttentionRow() / AttentionRow.new instead')
  static AttentionRow create() => AttentionRow._();
  static $pb.GeneratedMessage $_createMessage() => AttentionRow._();
  @$core.override
  AttentionRow createEmptyInstance() => AttentionRow._();
  @$core.pragma('dart2js:noInline')
  static AttentionRow getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<AttentionRow>(
          AttentionRow.$_createMessage);
  static AttentionRow? _defaultInstance;

  @$pb.TagNumber(1)
  AttentionStage get stage => $_getN(0);
  @$pb.TagNumber(1)
  set stage(AttentionStage value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasStage() => $_has(0);
  @$pb.TagNumber(1)
  void clearStage() => $_clearField(1);

  @$pb.TagNumber(2)
  AttentionSourceKind get kind => $_getN(1);
  @$pb.TagNumber(2)
  set kind(AttentionSourceKind value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasKind() => $_has(1);
  @$pb.TagNumber(2)
  void clearKind() => $_clearField(2);

  /// When the occurrence the row reports happened.
  @$pb.TagNumber(3)
  $1.Timestamp get since => $_getN(2);
  @$pb.TagNumber(3)
  set since($1.Timestamp value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasSince() => $_has(2);
  @$pb.TagNumber(3)
  void clearSince() => $_clearField(3);
  @$pb.TagNumber(3)
  $1.Timestamp ensureSince() => $_ensure(2);

  /// The conversation the row belongs to: an approval's or a consent's (a
  /// delegated task's appears under its parent), an automation's own while it
  /// exists, or the conversation a deletion withdraws.
  @$pb.TagNumber(4)
  $core.String get sessionId => $_getSZ(3);
  @$pb.TagNumber(4)
  set sessionId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasSessionId() => $_has(3);
  @$pb.TagNumber(4)
  void clearSessionId() => $_clearField(4);

  /// That conversation's title, empty for a deletion. Untrusted text: render
  /// it as plain text.
  @$pb.TagNumber(5)
  $core.String get sessionTitle => $_getSZ(4);
  @$pb.TagNumber(5)
  set sessionTitle($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasSessionTitle() => $_has(4);
  @$pb.TagNumber(5)
  void clearSessionTitle() => $_clearField(5);

  /// Set on an approval row.
  @$pb.TagNumber(6)
  $core.String get approvalId => $_getSZ(5);
  @$pb.TagNumber(6)
  set approvalId($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasApprovalId() => $_has(5);
  @$pb.TagNumber(6)
  void clearApprovalId() => $_clearField(6);

  /// The approval's run, the delegated task's run, or the automation's run
  /// whose failure the row shows.
  @$pb.TagNumber(7)
  $core.String get runId => $_getSZ(6);
  @$pb.TagNumber(7)
  set runId($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasRunId() => $_has(6);
  @$pb.TagNumber(7)
  void clearRunId() => $_clearField(7);

  /// Set on a consent row, and on an approval row of a delegated task.
  @$pb.TagNumber(8)
  $core.String get delegationId => $_getSZ(7);
  @$pb.TagNumber(8)
  set delegationId($core.String value) => $_setString(7, value);
  @$pb.TagNumber(8)
  $core.bool hasDelegationId() => $_has(7);
  @$pb.TagNumber(8)
  void clearDelegationId() => $_clearField(8);

  /// Set on an automation row, with the automation's name.
  @$pb.TagNumber(9)
  $core.String get automationId => $_getSZ(8);
  @$pb.TagNumber(9)
  set automationId($core.String value) => $_setString(8, value);
  @$pb.TagNumber(9)
  $core.bool hasAutomationId() => $_has(8);
  @$pb.TagNumber(9)
  void clearAutomationId() => $_clearField(9);

  @$pb.TagNumber(10)
  $core.String get automationName => $_getSZ(9);
  @$pb.TagNumber(10)
  set automationName($core.String value) => $_setString(9, value);
  @$pb.TagNumber(10)
  $core.bool hasAutomationName() => $_has(9);
  @$pb.TagNumber(10)
  void clearAutomationName() => $_clearField(10);

  /// Set on an automation row: the key of the occurrence it shows, a run id or
  /// an audit record id. DismissAttention takes it back unchanged.
  @$pb.TagNumber(11)
  $core.String get occurrenceKey => $_getSZ(10);
  @$pb.TagNumber(11)
  set occurrenceKey($core.String value) => $_setString(10, value);
  @$pb.TagNumber(11)
  $core.bool hasOccurrenceKey() => $_has(10);
  @$pb.TagNumber(11)
  void clearOccurrenceKey() => $_clearField(11);

  /// A closed, content-free code saying why the row needs a person: an
  /// automation's failure or block code, or a deletion's failure class.
  @$pb.TagNumber(12)
  $core.String get cause => $_getSZ(11);
  @$pb.TagNumber(12)
  set cause($core.String value) => $_setString(11, value);
  @$pb.TagNumber(12)
  $core.bool hasCause() => $_has(11);
  @$pb.TagNumber(12)
  void clearCause() => $_clearField(12);
}

class ListAttentionRequest extends $pb.GeneratedMessage {
  factory ListAttentionRequest({
    $2.PageRequest? page,
  }) {
    final result = ListAttentionRequest._();
    if (page != null) result.page = page;
    return result;
  }

  ListAttentionRequest._();

  factory ListAttentionRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListAttentionRequest()..mergeFromBuffer(data, registry);
  factory ListAttentionRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListAttentionRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListAttentionRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListAttentionRequest.$_createMessage)
    ..aOM<$2.PageRequest>(1, _omitFieldNames ? '' : 'page',
        subBuilder: $2.PageRequest.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAttentionRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAttentionRequest copyWith(void Function(ListAttentionRequest) updates) =>
      super.copyWith((message) => updates(message as ListAttentionRequest))
          as ListAttentionRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use ListAttentionRequest() / ListAttentionRequest.new instead')
  static ListAttentionRequest create() => ListAttentionRequest._();
  static $pb.GeneratedMessage $_createMessage() => ListAttentionRequest._();
  @$core.override
  ListAttentionRequest createEmptyInstance() => ListAttentionRequest._();
  @$core.pragma('dart2js:noInline')
  static ListAttentionRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListAttentionRequest>(
          ListAttentionRequest.$_createMessage);
  static ListAttentionRequest? _defaultInstance;

  /// A limit from 1 to 100; 0 means 100. The cursor is the previous page's
  /// next_cursor, which carries the first page's watermark.
  @$pb.TagNumber(1)
  $2.PageRequest get page => $_getN(0);
  @$pb.TagNumber(1)
  set page($2.PageRequest value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasPage() => $_has(0);
  @$pb.TagNumber(1)
  void clearPage() => $_clearField(1);
  @$pb.TagNumber(1)
  $2.PageRequest ensurePage() => $_ensure(0);
}

class ListAttentionResponse extends $pb.GeneratedMessage {
  factory ListAttentionResponse({
    $core.Iterable<AttentionRow>? rows,
    $2.PageResponse? page,
  }) {
    final result = ListAttentionResponse._();
    if (rows != null) result.rows.addAll(rows);
    if (page != null) result.page = page;
    return result;
  }

  ListAttentionResponse._();

  factory ListAttentionResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListAttentionResponse()..mergeFromBuffer(data, registry);
  factory ListAttentionResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListAttentionResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListAttentionResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListAttentionResponse.$_createMessage)
    ..pPM<AttentionRow>(1, _omitFieldNames ? '' : 'rows',
        subBuilder: AttentionRow.$_createMessage)
    ..aOM<$2.PageResponse>(2, _omitFieldNames ? '' : 'page',
        subBuilder: $2.PageResponse.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAttentionResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAttentionResponse copyWith(
          void Function(ListAttentionResponse) updates) =>
      super.copyWith((message) => updates(message as ListAttentionResponse))
          as ListAttentionResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use ListAttentionResponse() / ListAttentionResponse.new instead')
  static ListAttentionResponse create() => ListAttentionResponse._();
  static $pb.GeneratedMessage $_createMessage() => ListAttentionResponse._();
  @$core.override
  ListAttentionResponse createEmptyInstance() => ListAttentionResponse._();
  @$core.pragma('dart2js:noInline')
  static ListAttentionResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListAttentionResponse>(
          ListAttentionResponse.$_createMessage);
  static ListAttentionResponse? _defaultInstance;

  /// In the queue's total order: stage, then since (newest first), then
  /// source kind, then source id.
  @$pb.TagNumber(1)
  $pb.PbList<AttentionRow> get rows => $_getList(0);

  /// next_cursor is set exactly when a next page exists.
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

class DismissAttentionRequest extends $pb.GeneratedMessage {
  factory DismissAttentionRequest({
    $core.String? automationId,
    $core.String? occurrenceKey,
  }) {
    final result = DismissAttentionRequest._();
    if (automationId != null) result.automationId = automationId;
    if (occurrenceKey != null) result.occurrenceKey = occurrenceKey;
    return result;
  }

  DismissAttentionRequest._();

  factory DismissAttentionRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      DismissAttentionRequest()..mergeFromBuffer(data, registry);
  factory DismissAttentionRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      DismissAttentionRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DismissAttentionRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: DismissAttentionRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'automationId')
    ..aOS(2, _omitFieldNames ? '' : 'occurrenceKey')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DismissAttentionRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DismissAttentionRequest copyWith(
          void Function(DismissAttentionRequest) updates) =>
      super.copyWith((message) => updates(message as DismissAttentionRequest))
          as DismissAttentionRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use DismissAttentionRequest() / DismissAttentionRequest.new instead')
  static DismissAttentionRequest create() => DismissAttentionRequest._();
  static $pb.GeneratedMessage $_createMessage() => DismissAttentionRequest._();
  @$core.override
  DismissAttentionRequest createEmptyInstance() => DismissAttentionRequest._();
  @$core.pragma('dart2js:noInline')
  static DismissAttentionRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DismissAttentionRequest>(
          DismissAttentionRequest.$_createMessage);
  static DismissAttentionRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get automationId => $_getSZ(0);
  @$pb.TagNumber(1)
  set automationId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAutomationId() => $_has(0);
  @$pb.TagNumber(1)
  void clearAutomationId() => $_clearField(1);

  /// The listed row's occurrence_key, unchanged. A key that is no longer the
  /// automation's latest failing occurrence is refused.
  @$pb.TagNumber(2)
  $core.String get occurrenceKey => $_getSZ(1);
  @$pb.TagNumber(2)
  set occurrenceKey($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasOccurrenceKey() => $_has(1);
  @$pb.TagNumber(2)
  void clearOccurrenceKey() => $_clearField(2);
}

class DismissAttentionResponse extends $pb.GeneratedMessage {
  factory DismissAttentionResponse() => DismissAttentionResponse._();

  DismissAttentionResponse._();

  factory DismissAttentionResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      DismissAttentionResponse()..mergeFromBuffer(data, registry);
  factory DismissAttentionResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      DismissAttentionResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DismissAttentionResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: DismissAttentionResponse.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DismissAttentionResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DismissAttentionResponse copyWith(
          void Function(DismissAttentionResponse) updates) =>
      super.copyWith((message) => updates(message as DismissAttentionResponse))
          as DismissAttentionResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use DismissAttentionResponse() / DismissAttentionResponse.new instead')
  static DismissAttentionResponse create() => DismissAttentionResponse._();
  static $pb.GeneratedMessage $_createMessage() => DismissAttentionResponse._();
  @$core.override
  DismissAttentionResponse createEmptyInstance() =>
      DismissAttentionResponse._();
  @$core.pragma('dart2js:noInline')
  static DismissAttentionResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DismissAttentionResponse>(
          DismissAttentionResponse.$_createMessage);
  static DismissAttentionResponse? _defaultInstance;
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
