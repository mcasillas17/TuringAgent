// This is a generated file - do not edit.
//
// Generated from turing/v1/sessions.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:fixnum/fixnum.dart' as $fixnum;
import 'package:protobuf/protobuf.dart' as $pb;
import 'package:protobuf/well_known_types/google/protobuf/timestamp.pb.dart'
    as $1;

import 'common.pb.dart' as $2;
import 'sessions.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'sessions.pbenum.dart';

class Session extends $pb.GeneratedMessage {
  factory Session({
    $core.String? sessionId,
    $core.String? title,
    $core.String? status,
    $1.Timestamp? createdAt,
    $1.Timestamp? updatedAt,
  }) {
    final result = Session._();
    if (sessionId != null) result.sessionId = sessionId;
    if (title != null) result.title = title;
    if (status != null) result.status = status;
    if (createdAt != null) result.createdAt = createdAt;
    if (updatedAt != null) result.updatedAt = updatedAt;
    return result;
  }

  Session._();

  factory Session.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Session()..mergeFromBuffer(data, registry);
  factory Session.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      Session()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Session',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: Session.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..aOS(2, _omitFieldNames ? '' : 'title')
    ..aOS(3, _omitFieldNames ? '' : 'status')
    ..aOM<$1.Timestamp>(4, _omitFieldNames ? '' : 'createdAt',
        subBuilder: $1.Timestamp.$_createMessage)
    ..aOM<$1.Timestamp>(5, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $1.Timestamp.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Session clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Session copyWith(void Function(Session) updates) =>
      super.copyWith((message) => updates(message as Session)) as Session;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use Session() / Session.new instead')
  static Session create() => Session._();
  static $pb.GeneratedMessage $_createMessage() => Session._();
  @$core.override
  Session createEmptyInstance() => Session._();
  @$core.pragma('dart2js:noInline')
  static Session getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<Session>(Session.$_createMessage);
  static Session? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get title => $_getSZ(1);
  @$pb.TagNumber(2)
  set title($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasTitle() => $_has(1);
  @$pb.TagNumber(2)
  void clearTitle() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get status => $_getSZ(2);
  @$pb.TagNumber(3)
  set status($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasStatus() => $_has(2);
  @$pb.TagNumber(3)
  void clearStatus() => $_clearField(3);

  @$pb.TagNumber(4)
  $1.Timestamp get createdAt => $_getN(3);
  @$pb.TagNumber(4)
  set createdAt($1.Timestamp value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasCreatedAt() => $_has(3);
  @$pb.TagNumber(4)
  void clearCreatedAt() => $_clearField(4);
  @$pb.TagNumber(4)
  $1.Timestamp ensureCreatedAt() => $_ensure(3);

  @$pb.TagNumber(5)
  $1.Timestamp get updatedAt => $_getN(4);
  @$pb.TagNumber(5)
  set updatedAt($1.Timestamp value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasUpdatedAt() => $_has(4);
  @$pb.TagNumber(5)
  void clearUpdatedAt() => $_clearField(5);
  @$pb.TagNumber(5)
  $1.Timestamp ensureUpdatedAt() => $_ensure(4);
}

class CreateSessionRequest extends $pb.GeneratedMessage {
  factory CreateSessionRequest({
    $core.String? title,
  }) {
    final result = CreateSessionRequest._();
    if (title != null) result.title = title;
    return result;
  }

  CreateSessionRequest._();

  factory CreateSessionRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      CreateSessionRequest()..mergeFromBuffer(data, registry);
  factory CreateSessionRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      CreateSessionRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CreateSessionRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: CreateSessionRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'title')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CreateSessionRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CreateSessionRequest copyWith(void Function(CreateSessionRequest) updates) =>
      super.copyWith((message) => updates(message as CreateSessionRequest))
          as CreateSessionRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use CreateSessionRequest() / CreateSessionRequest.new instead')
  static CreateSessionRequest create() => CreateSessionRequest._();
  static $pb.GeneratedMessage $_createMessage() => CreateSessionRequest._();
  @$core.override
  CreateSessionRequest createEmptyInstance() => CreateSessionRequest._();
  @$core.pragma('dart2js:noInline')
  static CreateSessionRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CreateSessionRequest>(
          CreateSessionRequest.$_createMessage);
  static CreateSessionRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get title => $_getSZ(0);
  @$pb.TagNumber(1)
  set title($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasTitle() => $_has(0);
  @$pb.TagNumber(1)
  void clearTitle() => $_clearField(1);
}

class CreateSessionResponse extends $pb.GeneratedMessage {
  factory CreateSessionResponse({
    $core.String? sessionId,
    $1.Timestamp? createdAt,
  }) {
    final result = CreateSessionResponse._();
    if (sessionId != null) result.sessionId = sessionId;
    if (createdAt != null) result.createdAt = createdAt;
    return result;
  }

  CreateSessionResponse._();

  factory CreateSessionResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      CreateSessionResponse()..mergeFromBuffer(data, registry);
  factory CreateSessionResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      CreateSessionResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CreateSessionResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: CreateSessionResponse.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..aOM<$1.Timestamp>(2, _omitFieldNames ? '' : 'createdAt',
        subBuilder: $1.Timestamp.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CreateSessionResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CreateSessionResponse copyWith(
          void Function(CreateSessionResponse) updates) =>
      super.copyWith((message) => updates(message as CreateSessionResponse))
          as CreateSessionResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use CreateSessionResponse() / CreateSessionResponse.new instead')
  static CreateSessionResponse create() => CreateSessionResponse._();
  static $pb.GeneratedMessage $_createMessage() => CreateSessionResponse._();
  @$core.override
  CreateSessionResponse createEmptyInstance() => CreateSessionResponse._();
  @$core.pragma('dart2js:noInline')
  static CreateSessionResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CreateSessionResponse>(
          CreateSessionResponse.$_createMessage);
  static CreateSessionResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);

  @$pb.TagNumber(2)
  $1.Timestamp get createdAt => $_getN(1);
  @$pb.TagNumber(2)
  set createdAt($1.Timestamp value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasCreatedAt() => $_has(1);
  @$pb.TagNumber(2)
  void clearCreatedAt() => $_clearField(2);
  @$pb.TagNumber(2)
  $1.Timestamp ensureCreatedAt() => $_ensure(1);
}

class ListSessionsRequest extends $pb.GeneratedMessage {
  factory ListSessionsRequest({
    $2.PageRequest? page,
    SessionListFilter? filter,
  }) {
    final result = ListSessionsRequest._();
    if (page != null) result.page = page;
    if (filter != null) result.filter = filter;
    return result;
  }

  ListSessionsRequest._();

  factory ListSessionsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListSessionsRequest()..mergeFromBuffer(data, registry);
  factory ListSessionsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListSessionsRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListSessionsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListSessionsRequest.$_createMessage)
    ..aOM<$2.PageRequest>(1, _omitFieldNames ? '' : 'page',
        subBuilder: $2.PageRequest.$_createMessage)
    ..aE<SessionListFilter>(2, _omitFieldNames ? '' : 'filter',
        enumValues: SessionListFilter.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListSessionsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListSessionsRequest copyWith(void Function(ListSessionsRequest) updates) =>
      super.copyWith((message) => updates(message as ListSessionsRequest))
          as ListSessionsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core
      .Deprecated('Use ListSessionsRequest() / ListSessionsRequest.new instead')
  static ListSessionsRequest create() => ListSessionsRequest._();
  static $pb.GeneratedMessage $_createMessage() => ListSessionsRequest._();
  @$core.override
  ListSessionsRequest createEmptyInstance() => ListSessionsRequest._();
  @$core.pragma('dart2js:noInline')
  static ListSessionsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListSessionsRequest>(
          ListSessionsRequest.$_createMessage);
  static ListSessionsRequest? _defaultInstance;

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

  @$pb.TagNumber(2)
  SessionListFilter get filter => $_getN(1);
  @$pb.TagNumber(2)
  set filter(SessionListFilter value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasFilter() => $_has(1);
  @$pb.TagNumber(2)
  void clearFilter() => $_clearField(2);
}

class ListSessionsResponse extends $pb.GeneratedMessage {
  factory ListSessionsResponse({
    $core.Iterable<Session>? sessions,
    $2.PageResponse? page,
  }) {
    final result = ListSessionsResponse._();
    if (sessions != null) result.sessions.addAll(sessions);
    if (page != null) result.page = page;
    return result;
  }

  ListSessionsResponse._();

  factory ListSessionsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListSessionsResponse()..mergeFromBuffer(data, registry);
  factory ListSessionsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListSessionsResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListSessionsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListSessionsResponse.$_createMessage)
    ..pPM<Session>(1, _omitFieldNames ? '' : 'sessions',
        subBuilder: Session.$_createMessage)
    ..aOM<$2.PageResponse>(2, _omitFieldNames ? '' : 'page',
        subBuilder: $2.PageResponse.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListSessionsResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListSessionsResponse copyWith(void Function(ListSessionsResponse) updates) =>
      super.copyWith((message) => updates(message as ListSessionsResponse))
          as ListSessionsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use ListSessionsResponse() / ListSessionsResponse.new instead')
  static ListSessionsResponse create() => ListSessionsResponse._();
  static $pb.GeneratedMessage $_createMessage() => ListSessionsResponse._();
  @$core.override
  ListSessionsResponse createEmptyInstance() => ListSessionsResponse._();
  @$core.pragma('dart2js:noInline')
  static ListSessionsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListSessionsResponse>(
          ListSessionsResponse.$_createMessage);
  static ListSessionsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<Session> get sessions => $_getList(0);

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

class GetSessionRequest extends $pb.GeneratedMessage {
  factory GetSessionRequest({
    $core.String? sessionId,
  }) {
    final result = GetSessionRequest._();
    if (sessionId != null) result.sessionId = sessionId;
    return result;
  }

  GetSessionRequest._();

  factory GetSessionRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      GetSessionRequest()..mergeFromBuffer(data, registry);
  factory GetSessionRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      GetSessionRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetSessionRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: GetSessionRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetSessionRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetSessionRequest copyWith(void Function(GetSessionRequest) updates) =>
      super.copyWith((message) => updates(message as GetSessionRequest))
          as GetSessionRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use GetSessionRequest() / GetSessionRequest.new instead')
  static GetSessionRequest create() => GetSessionRequest._();
  static $pb.GeneratedMessage $_createMessage() => GetSessionRequest._();
  @$core.override
  GetSessionRequest createEmptyInstance() => GetSessionRequest._();
  @$core.pragma('dart2js:noInline')
  static GetSessionRequest getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<GetSessionRequest>(
          GetSessionRequest.$_createMessage);
  static GetSessionRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);
}

class DeleteSessionRequest extends $pb.GeneratedMessage {
  factory DeleteSessionRequest({
    $core.String? sessionId,
  }) {
    final result = DeleteSessionRequest._();
    if (sessionId != null) result.sessionId = sessionId;
    return result;
  }

  DeleteSessionRequest._();

  factory DeleteSessionRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      DeleteSessionRequest()..mergeFromBuffer(data, registry);
  factory DeleteSessionRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      DeleteSessionRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DeleteSessionRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: DeleteSessionRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteSessionRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteSessionRequest copyWith(void Function(DeleteSessionRequest) updates) =>
      super.copyWith((message) => updates(message as DeleteSessionRequest))
          as DeleteSessionRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use DeleteSessionRequest() / DeleteSessionRequest.new instead')
  static DeleteSessionRequest create() => DeleteSessionRequest._();
  static $pb.GeneratedMessage $_createMessage() => DeleteSessionRequest._();
  @$core.override
  DeleteSessionRequest createEmptyInstance() => DeleteSessionRequest._();
  @$core.pragma('dart2js:noInline')
  static DeleteSessionRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DeleteSessionRequest>(
          DeleteSessionRequest.$_createMessage);
  static DeleteSessionRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);
}

class SessionDeletionReceipt extends $pb.GeneratedMessage {
  factory SessionDeletionReceipt({
    $core.String? sessionId,
    SessionDeletionState? state,
    $fixnum.Int64? lifecycleVersion,
    $core.bool? retryable,
    $core.String? errorCode,
    $fixnum.Int64? terminalSequence,
    $core.int? runCount,
    $core.int? messageCount,
    $core.int? retainedLegacyArtifactCount,
  }) {
    final result = SessionDeletionReceipt._();
    if (sessionId != null) result.sessionId = sessionId;
    if (state != null) result.state = state;
    if (lifecycleVersion != null) result.lifecycleVersion = lifecycleVersion;
    if (retryable != null) result.retryable = retryable;
    if (errorCode != null) result.errorCode = errorCode;
    if (terminalSequence != null) result.terminalSequence = terminalSequence;
    if (runCount != null) result.runCount = runCount;
    if (messageCount != null) result.messageCount = messageCount;
    if (retainedLegacyArtifactCount != null)
      result.retainedLegacyArtifactCount = retainedLegacyArtifactCount;
    return result;
  }

  SessionDeletionReceipt._();

  factory SessionDeletionReceipt.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      SessionDeletionReceipt()..mergeFromBuffer(data, registry);
  factory SessionDeletionReceipt.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      SessionDeletionReceipt()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SessionDeletionReceipt',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: SessionDeletionReceipt.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..aE<SessionDeletionState>(2, _omitFieldNames ? '' : 'state',
        enumValues: SessionDeletionState.values)
    ..aInt64(3, _omitFieldNames ? '' : 'lifecycleVersion')
    ..aOB(4, _omitFieldNames ? '' : 'retryable')
    ..aOS(5, _omitFieldNames ? '' : 'errorCode')
    ..aInt64(6, _omitFieldNames ? '' : 'terminalSequence')
    ..aI(7, _omitFieldNames ? '' : 'runCount')
    ..aI(8, _omitFieldNames ? '' : 'messageCount')
    ..aI(9, _omitFieldNames ? '' : 'retainedLegacyArtifactCount')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SessionDeletionReceipt clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SessionDeletionReceipt copyWith(
          void Function(SessionDeletionReceipt) updates) =>
      super.copyWith((message) => updates(message as SessionDeletionReceipt))
          as SessionDeletionReceipt;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use SessionDeletionReceipt() / SessionDeletionReceipt.new instead')
  static SessionDeletionReceipt create() => SessionDeletionReceipt._();
  static $pb.GeneratedMessage $_createMessage() => SessionDeletionReceipt._();
  @$core.override
  SessionDeletionReceipt createEmptyInstance() => SessionDeletionReceipt._();
  @$core.pragma('dart2js:noInline')
  static SessionDeletionReceipt getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SessionDeletionReceipt>(
          SessionDeletionReceipt.$_createMessage);
  static SessionDeletionReceipt? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);

  @$pb.TagNumber(2)
  SessionDeletionState get state => $_getN(1);
  @$pb.TagNumber(2)
  set state(SessionDeletionState value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasState() => $_has(1);
  @$pb.TagNumber(2)
  void clearState() => $_clearField(2);

  @$pb.TagNumber(3)
  $fixnum.Int64 get lifecycleVersion => $_getI64(2);
  @$pb.TagNumber(3)
  set lifecycleVersion($fixnum.Int64 value) => $_setInt64(2, value);
  @$pb.TagNumber(3)
  $core.bool hasLifecycleVersion() => $_has(2);
  @$pb.TagNumber(3)
  void clearLifecycleVersion() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.bool get retryable => $_getBF(3);
  @$pb.TagNumber(4)
  set retryable($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasRetryable() => $_has(3);
  @$pb.TagNumber(4)
  void clearRetryable() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get errorCode => $_getSZ(4);
  @$pb.TagNumber(5)
  set errorCode($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasErrorCode() => $_has(4);
  @$pb.TagNumber(5)
  void clearErrorCode() => $_clearField(5);

  @$pb.TagNumber(6)
  $fixnum.Int64 get terminalSequence => $_getI64(5);
  @$pb.TagNumber(6)
  set terminalSequence($fixnum.Int64 value) => $_setInt64(5, value);
  @$pb.TagNumber(6)
  $core.bool hasTerminalSequence() => $_has(5);
  @$pb.TagNumber(6)
  void clearTerminalSequence() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.int get runCount => $_getIZ(6);
  @$pb.TagNumber(7)
  set runCount($core.int value) => $_setSignedInt32(6, value);
  @$pb.TagNumber(7)
  $core.bool hasRunCount() => $_has(6);
  @$pb.TagNumber(7)
  void clearRunCount() => $_clearField(7);

  @$pb.TagNumber(8)
  $core.int get messageCount => $_getIZ(7);
  @$pb.TagNumber(8)
  set messageCount($core.int value) => $_setSignedInt32(7, value);
  @$pb.TagNumber(8)
  $core.bool hasMessageCount() => $_has(7);
  @$pb.TagNumber(8)
  void clearMessageCount() => $_clearField(8);

  @$pb.TagNumber(9)
  $core.int get retainedLegacyArtifactCount => $_getIZ(8);
  @$pb.TagNumber(9)
  set retainedLegacyArtifactCount($core.int value) =>
      $_setSignedInt32(8, value);
  @$pb.TagNumber(9)
  $core.bool hasRetainedLegacyArtifactCount() => $_has(8);
  @$pb.TagNumber(9)
  void clearRetainedLegacyArtifactCount() => $_clearField(9);
}

class ListSessionDeletionReceiptsRequest extends $pb.GeneratedMessage {
  factory ListSessionDeletionReceiptsRequest() =>
      ListSessionDeletionReceiptsRequest._();

  ListSessionDeletionReceiptsRequest._();

  factory ListSessionDeletionReceiptsRequest.fromBuffer(
          $core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListSessionDeletionReceiptsRequest()..mergeFromBuffer(data, registry);
  factory ListSessionDeletionReceiptsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListSessionDeletionReceiptsRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListSessionDeletionReceiptsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListSessionDeletionReceiptsRequest.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListSessionDeletionReceiptsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListSessionDeletionReceiptsRequest copyWith(
          void Function(ListSessionDeletionReceiptsRequest) updates) =>
      super.copyWith((message) =>
              updates(message as ListSessionDeletionReceiptsRequest))
          as ListSessionDeletionReceiptsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use ListSessionDeletionReceiptsRequest() / ListSessionDeletionReceiptsRequest.new instead')
  static ListSessionDeletionReceiptsRequest create() =>
      ListSessionDeletionReceiptsRequest._();
  static $pb.GeneratedMessage $_createMessage() =>
      ListSessionDeletionReceiptsRequest._();
  @$core.override
  ListSessionDeletionReceiptsRequest createEmptyInstance() =>
      ListSessionDeletionReceiptsRequest._();
  @$core.pragma('dart2js:noInline')
  static ListSessionDeletionReceiptsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListSessionDeletionReceiptsRequest>(
          ListSessionDeletionReceiptsRequest.$_createMessage);
  static ListSessionDeletionReceiptsRequest? _defaultInstance;
}

class ListSessionDeletionReceiptsResponse extends $pb.GeneratedMessage {
  factory ListSessionDeletionReceiptsResponse({
    $core.Iterable<SessionDeletionReceipt>? deletions,
  }) {
    final result = ListSessionDeletionReceiptsResponse._();
    if (deletions != null) result.deletions.addAll(deletions);
    return result;
  }

  ListSessionDeletionReceiptsResponse._();

  factory ListSessionDeletionReceiptsResponse.fromBuffer(
          $core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListSessionDeletionReceiptsResponse()..mergeFromBuffer(data, registry);
  factory ListSessionDeletionReceiptsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListSessionDeletionReceiptsResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListSessionDeletionReceiptsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListSessionDeletionReceiptsResponse.$_createMessage)
    ..pPM<SessionDeletionReceipt>(1, _omitFieldNames ? '' : 'deletions',
        subBuilder: SessionDeletionReceipt.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListSessionDeletionReceiptsResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListSessionDeletionReceiptsResponse copyWith(
          void Function(ListSessionDeletionReceiptsResponse) updates) =>
      super.copyWith((message) =>
              updates(message as ListSessionDeletionReceiptsResponse))
          as ListSessionDeletionReceiptsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use ListSessionDeletionReceiptsResponse() / ListSessionDeletionReceiptsResponse.new instead')
  static ListSessionDeletionReceiptsResponse create() =>
      ListSessionDeletionReceiptsResponse._();
  static $pb.GeneratedMessage $_createMessage() =>
      ListSessionDeletionReceiptsResponse._();
  @$core.override
  ListSessionDeletionReceiptsResponse createEmptyInstance() =>
      ListSessionDeletionReceiptsResponse._();
  @$core.pragma('dart2js:noInline')
  static ListSessionDeletionReceiptsResponse getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<
              ListSessionDeletionReceiptsResponse>(
          ListSessionDeletionReceiptsResponse.$_createMessage);
  static ListSessionDeletionReceiptsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<SessionDeletionReceipt> get deletions => $_getList(0);
}

class DeleteSessionResponse extends $pb.GeneratedMessage {
  factory DeleteSessionResponse({
    $core.String? sessionId,
    SessionDeletionReceipt? deletion,
  }) {
    final result = DeleteSessionResponse._();
    if (sessionId != null) result.sessionId = sessionId;
    if (deletion != null) result.deletion = deletion;
    return result;
  }

  DeleteSessionResponse._();

  factory DeleteSessionResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      DeleteSessionResponse()..mergeFromBuffer(data, registry);
  factory DeleteSessionResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      DeleteSessionResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DeleteSessionResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: DeleteSessionResponse.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..aOM<SessionDeletionReceipt>(2, _omitFieldNames ? '' : 'deletion',
        subBuilder: SessionDeletionReceipt.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteSessionResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteSessionResponse copyWith(
          void Function(DeleteSessionResponse) updates) =>
      super.copyWith((message) => updates(message as DeleteSessionResponse))
          as DeleteSessionResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use DeleteSessionResponse() / DeleteSessionResponse.new instead')
  static DeleteSessionResponse create() => DeleteSessionResponse._();
  static $pb.GeneratedMessage $_createMessage() => DeleteSessionResponse._();
  @$core.override
  DeleteSessionResponse createEmptyInstance() => DeleteSessionResponse._();
  @$core.pragma('dart2js:noInline')
  static DeleteSessionResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DeleteSessionResponse>(
          DeleteSessionResponse.$_createMessage);
  static DeleteSessionResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);

  @$pb.TagNumber(2)
  SessionDeletionReceipt get deletion => $_getN(1);
  @$pb.TagNumber(2)
  set deletion(SessionDeletionReceipt value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasDeletion() => $_has(1);
  @$pb.TagNumber(2)
  void clearDeletion() => $_clearField(2);
  @$pb.TagNumber(2)
  SessionDeletionReceipt ensureDeletion() => $_ensure(1);
}

class RenameSessionRequest extends $pb.GeneratedMessage {
  factory RenameSessionRequest({
    $core.String? sessionId,
    $core.String? title,
  }) {
    final result = RenameSessionRequest._();
    if (sessionId != null) result.sessionId = sessionId;
    if (title != null) result.title = title;
    return result;
  }

  RenameSessionRequest._();

  factory RenameSessionRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      RenameSessionRequest()..mergeFromBuffer(data, registry);
  factory RenameSessionRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      RenameSessionRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RenameSessionRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: RenameSessionRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..aOS(2, _omitFieldNames ? '' : 'title')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RenameSessionRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RenameSessionRequest copyWith(void Function(RenameSessionRequest) updates) =>
      super.copyWith((message) => updates(message as RenameSessionRequest))
          as RenameSessionRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use RenameSessionRequest() / RenameSessionRequest.new instead')
  static RenameSessionRequest create() => RenameSessionRequest._();
  static $pb.GeneratedMessage $_createMessage() => RenameSessionRequest._();
  @$core.override
  RenameSessionRequest createEmptyInstance() => RenameSessionRequest._();
  @$core.pragma('dart2js:noInline')
  static RenameSessionRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RenameSessionRequest>(
          RenameSessionRequest.$_createMessage);
  static RenameSessionRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get title => $_getSZ(1);
  @$pb.TagNumber(2)
  set title($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasTitle() => $_has(1);
  @$pb.TagNumber(2)
  void clearTitle() => $_clearField(2);
}

class RenameSessionResponse extends $pb.GeneratedMessage {
  factory RenameSessionResponse({
    Session? session,
  }) {
    final result = RenameSessionResponse._();
    if (session != null) result.session = session;
    return result;
  }

  RenameSessionResponse._();

  factory RenameSessionResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      RenameSessionResponse()..mergeFromBuffer(data, registry);
  factory RenameSessionResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      RenameSessionResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RenameSessionResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: RenameSessionResponse.$_createMessage)
    ..aOM<Session>(1, _omitFieldNames ? '' : 'session',
        subBuilder: Session.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RenameSessionResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RenameSessionResponse copyWith(
          void Function(RenameSessionResponse) updates) =>
      super.copyWith((message) => updates(message as RenameSessionResponse))
          as RenameSessionResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use RenameSessionResponse() / RenameSessionResponse.new instead')
  static RenameSessionResponse create() => RenameSessionResponse._();
  static $pb.GeneratedMessage $_createMessage() => RenameSessionResponse._();
  @$core.override
  RenameSessionResponse createEmptyInstance() => RenameSessionResponse._();
  @$core.pragma('dart2js:noInline')
  static RenameSessionResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RenameSessionResponse>(
          RenameSessionResponse.$_createMessage);
  static RenameSessionResponse? _defaultInstance;

  @$pb.TagNumber(1)
  Session get session => $_getN(0);
  @$pb.TagNumber(1)
  set session(Session value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasSession() => $_has(0);
  @$pb.TagNumber(1)
  void clearSession() => $_clearField(1);
  @$pb.TagNumber(1)
  Session ensureSession() => $_ensure(0);
}

class ArchiveSessionRequest extends $pb.GeneratedMessage {
  factory ArchiveSessionRequest({
    $core.String? sessionId,
  }) {
    final result = ArchiveSessionRequest._();
    if (sessionId != null) result.sessionId = sessionId;
    return result;
  }

  ArchiveSessionRequest._();

  factory ArchiveSessionRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ArchiveSessionRequest()..mergeFromBuffer(data, registry);
  factory ArchiveSessionRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ArchiveSessionRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ArchiveSessionRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ArchiveSessionRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ArchiveSessionRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ArchiveSessionRequest copyWith(
          void Function(ArchiveSessionRequest) updates) =>
      super.copyWith((message) => updates(message as ArchiveSessionRequest))
          as ArchiveSessionRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use ArchiveSessionRequest() / ArchiveSessionRequest.new instead')
  static ArchiveSessionRequest create() => ArchiveSessionRequest._();
  static $pb.GeneratedMessage $_createMessage() => ArchiveSessionRequest._();
  @$core.override
  ArchiveSessionRequest createEmptyInstance() => ArchiveSessionRequest._();
  @$core.pragma('dart2js:noInline')
  static ArchiveSessionRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ArchiveSessionRequest>(
          ArchiveSessionRequest.$_createMessage);
  static ArchiveSessionRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);
}

class ArchiveSessionResponse extends $pb.GeneratedMessage {
  factory ArchiveSessionResponse({
    Session? session,
  }) {
    final result = ArchiveSessionResponse._();
    if (session != null) result.session = session;
    return result;
  }

  ArchiveSessionResponse._();

  factory ArchiveSessionResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ArchiveSessionResponse()..mergeFromBuffer(data, registry);
  factory ArchiveSessionResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ArchiveSessionResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ArchiveSessionResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ArchiveSessionResponse.$_createMessage)
    ..aOM<Session>(1, _omitFieldNames ? '' : 'session',
        subBuilder: Session.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ArchiveSessionResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ArchiveSessionResponse copyWith(
          void Function(ArchiveSessionResponse) updates) =>
      super.copyWith((message) => updates(message as ArchiveSessionResponse))
          as ArchiveSessionResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use ArchiveSessionResponse() / ArchiveSessionResponse.new instead')
  static ArchiveSessionResponse create() => ArchiveSessionResponse._();
  static $pb.GeneratedMessage $_createMessage() => ArchiveSessionResponse._();
  @$core.override
  ArchiveSessionResponse createEmptyInstance() => ArchiveSessionResponse._();
  @$core.pragma('dart2js:noInline')
  static ArchiveSessionResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ArchiveSessionResponse>(
          ArchiveSessionResponse.$_createMessage);
  static ArchiveSessionResponse? _defaultInstance;

  @$pb.TagNumber(1)
  Session get session => $_getN(0);
  @$pb.TagNumber(1)
  set session(Session value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasSession() => $_has(0);
  @$pb.TagNumber(1)
  void clearSession() => $_clearField(1);
  @$pb.TagNumber(1)
  Session ensureSession() => $_ensure(0);
}

class RestoreSessionRequest extends $pb.GeneratedMessage {
  factory RestoreSessionRequest({
    $core.String? sessionId,
  }) {
    final result = RestoreSessionRequest._();
    if (sessionId != null) result.sessionId = sessionId;
    return result;
  }

  RestoreSessionRequest._();

  factory RestoreSessionRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      RestoreSessionRequest()..mergeFromBuffer(data, registry);
  factory RestoreSessionRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      RestoreSessionRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RestoreSessionRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: RestoreSessionRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RestoreSessionRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RestoreSessionRequest copyWith(
          void Function(RestoreSessionRequest) updates) =>
      super.copyWith((message) => updates(message as RestoreSessionRequest))
          as RestoreSessionRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use RestoreSessionRequest() / RestoreSessionRequest.new instead')
  static RestoreSessionRequest create() => RestoreSessionRequest._();
  static $pb.GeneratedMessage $_createMessage() => RestoreSessionRequest._();
  @$core.override
  RestoreSessionRequest createEmptyInstance() => RestoreSessionRequest._();
  @$core.pragma('dart2js:noInline')
  static RestoreSessionRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RestoreSessionRequest>(
          RestoreSessionRequest.$_createMessage);
  static RestoreSessionRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);
}

class RestoreSessionResponse extends $pb.GeneratedMessage {
  factory RestoreSessionResponse({
    Session? session,
  }) {
    final result = RestoreSessionResponse._();
    if (session != null) result.session = session;
    return result;
  }

  RestoreSessionResponse._();

  factory RestoreSessionResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      RestoreSessionResponse()..mergeFromBuffer(data, registry);
  factory RestoreSessionResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      RestoreSessionResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RestoreSessionResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: RestoreSessionResponse.$_createMessage)
    ..aOM<Session>(1, _omitFieldNames ? '' : 'session',
        subBuilder: Session.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RestoreSessionResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RestoreSessionResponse copyWith(
          void Function(RestoreSessionResponse) updates) =>
      super.copyWith((message) => updates(message as RestoreSessionResponse))
          as RestoreSessionResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use RestoreSessionResponse() / RestoreSessionResponse.new instead')
  static RestoreSessionResponse create() => RestoreSessionResponse._();
  static $pb.GeneratedMessage $_createMessage() => RestoreSessionResponse._();
  @$core.override
  RestoreSessionResponse createEmptyInstance() => RestoreSessionResponse._();
  @$core.pragma('dart2js:noInline')
  static RestoreSessionResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RestoreSessionResponse>(
          RestoreSessionResponse.$_createMessage);
  static RestoreSessionResponse? _defaultInstance;

  @$pb.TagNumber(1)
  Session get session => $_getN(0);
  @$pb.TagNumber(1)
  set session(Session value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasSession() => $_has(0);
  @$pb.TagNumber(1)
  void clearSession() => $_clearField(1);
  @$pb.TagNumber(1)
  Session ensureSession() => $_ensure(0);
}

class ListMessagesRequest extends $pb.GeneratedMessage {
  factory ListMessagesRequest({
    $core.String? sessionId,
    $core.int? limit,
    $core.String? beforeMessageId,
  }) {
    final result = ListMessagesRequest._();
    if (sessionId != null) result.sessionId = sessionId;
    if (limit != null) result.limit = limit;
    if (beforeMessageId != null) result.beforeMessageId = beforeMessageId;
    return result;
  }

  ListMessagesRequest._();

  factory ListMessagesRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListMessagesRequest()..mergeFromBuffer(data, registry);
  factory ListMessagesRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListMessagesRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListMessagesRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListMessagesRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..aI(2, _omitFieldNames ? '' : 'limit')
    ..aOS(3, _omitFieldNames ? '' : 'beforeMessageId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMessagesRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMessagesRequest copyWith(void Function(ListMessagesRequest) updates) =>
      super.copyWith((message) => updates(message as ListMessagesRequest))
          as ListMessagesRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core
      .Deprecated('Use ListMessagesRequest() / ListMessagesRequest.new instead')
  static ListMessagesRequest create() => ListMessagesRequest._();
  static $pb.GeneratedMessage $_createMessage() => ListMessagesRequest._();
  @$core.override
  ListMessagesRequest createEmptyInstance() => ListMessagesRequest._();
  @$core.pragma('dart2js:noInline')
  static ListMessagesRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListMessagesRequest>(
          ListMessagesRequest.$_createMessage);
  static ListMessagesRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.int get limit => $_getIZ(1);
  @$pb.TagNumber(2)
  set limit($core.int value) => $_setSignedInt32(1, value);
  @$pb.TagNumber(2)
  $core.bool hasLimit() => $_has(1);
  @$pb.TagNumber(2)
  void clearLimit() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get beforeMessageId => $_getSZ(2);
  @$pb.TagNumber(3)
  set beforeMessageId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasBeforeMessageId() => $_has(2);
  @$pb.TagNumber(3)
  void clearBeforeMessageId() => $_clearField(3);
}

class ListMessagesResponse extends $pb.GeneratedMessage {
  factory ListMessagesResponse({
    $core.Iterable<$2.Message>? messages,
  }) {
    final result = ListMessagesResponse._();
    if (messages != null) result.messages.addAll(messages);
    return result;
  }

  ListMessagesResponse._();

  factory ListMessagesResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListMessagesResponse()..mergeFromBuffer(data, registry);
  factory ListMessagesResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListMessagesResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListMessagesResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListMessagesResponse.$_createMessage)
    ..pPM<$2.Message>(1, _omitFieldNames ? '' : 'messages',
        subBuilder: $2.Message.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMessagesResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMessagesResponse copyWith(void Function(ListMessagesResponse) updates) =>
      super.copyWith((message) => updates(message as ListMessagesResponse))
          as ListMessagesResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use ListMessagesResponse() / ListMessagesResponse.new instead')
  static ListMessagesResponse create() => ListMessagesResponse._();
  static $pb.GeneratedMessage $_createMessage() => ListMessagesResponse._();
  @$core.override
  ListMessagesResponse createEmptyInstance() => ListMessagesResponse._();
  @$core.pragma('dart2js:noInline')
  static ListMessagesResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListMessagesResponse>(
          ListMessagesResponse.$_createMessage);
  static ListMessagesResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<$2.Message> get messages => $_getList(0);
}

class SearchHit extends $pb.GeneratedMessage {
  factory SearchHit({
    $2.Message? message,
    $core.double? score,
    $core.String? snippet,
  }) {
    final result = SearchHit._();
    if (message != null) result.message = message;
    if (score != null) result.score = score;
    if (snippet != null) result.snippet = snippet;
    return result;
  }

  SearchHit._();

  factory SearchHit.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      SearchHit()..mergeFromBuffer(data, registry);
  factory SearchHit.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      SearchHit()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SearchHit',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: SearchHit.$_createMessage)
    ..aOM<$2.Message>(1, _omitFieldNames ? '' : 'message',
        subBuilder: $2.Message.$_createMessage)
    ..aD(2, _omitFieldNames ? '' : 'score')
    ..aOS(3, _omitFieldNames ? '' : 'snippet')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SearchHit clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SearchHit copyWith(void Function(SearchHit) updates) =>
      super.copyWith((message) => updates(message as SearchHit)) as SearchHit;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use SearchHit() / SearchHit.new instead')
  static SearchHit create() => SearchHit._();
  static $pb.GeneratedMessage $_createMessage() => SearchHit._();
  @$core.override
  SearchHit createEmptyInstance() => SearchHit._();
  @$core.pragma('dart2js:noInline')
  static SearchHit getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SearchHit>(SearchHit.$_createMessage);
  static SearchHit? _defaultInstance;

  @$pb.TagNumber(1)
  $2.Message get message => $_getN(0);
  @$pb.TagNumber(1)
  set message($2.Message value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasMessage() => $_has(0);
  @$pb.TagNumber(1)
  void clearMessage() => $_clearField(1);
  @$pb.TagNumber(1)
  $2.Message ensureMessage() => $_ensure(0);

  /// Finite and non-negative. Higher means a more relevant match within the
  /// same SearchMessages response. Not comparable across queries or snapshots.
  @$pb.TagNumber(2)
  $core.double get score => $_getN(1);
  @$pb.TagNumber(2)
  set score($core.double value) => $_setDouble(1, value);
  @$pb.TagNumber(2)
  $core.bool hasScore() => $_has(1);
  @$pb.TagNumber(2)
  void clearScore() => $_clearField(2);

  /// Bounded single-line plain text selected from message.content. It is
  /// centered on the match when one fits FTS5's snippet window, and is
  /// otherwise a bounded unhighlighted excerpt of the same message. Contains
  /// no server-added markup and must not be treated as HTML.
  @$pb.TagNumber(3)
  $core.String get snippet => $_getSZ(2);
  @$pb.TagNumber(3)
  set snippet($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasSnippet() => $_has(2);
  @$pb.TagNumber(3)
  void clearSnippet() => $_clearField(3);
}

class SearchMessagesRequest extends $pb.GeneratedMessage {
  factory SearchMessagesRequest({
    $core.String? query,
    $core.String? sessionId,
    $core.int? limit,
    $core.String? excludeSessionId,
    SearchMessagesResponseFormat? responseFormat,
  }) {
    final result = SearchMessagesRequest._();
    if (query != null) result.query = query;
    if (sessionId != null) result.sessionId = sessionId;
    if (limit != null) result.limit = limit;
    if (excludeSessionId != null) result.excludeSessionId = excludeSessionId;
    if (responseFormat != null) result.responseFormat = responseFormat;
    return result;
  }

  SearchMessagesRequest._();

  factory SearchMessagesRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      SearchMessagesRequest()..mergeFromBuffer(data, registry);
  factory SearchMessagesRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      SearchMessagesRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SearchMessagesRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: SearchMessagesRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'query')
    ..aOS(2, _omitFieldNames ? '' : 'sessionId')
    ..aI(3, _omitFieldNames ? '' : 'limit')
    ..aOS(4, _omitFieldNames ? '' : 'excludeSessionId')
    ..aE<SearchMessagesResponseFormat>(
        5, _omitFieldNames ? '' : 'responseFormat',
        enumValues: SearchMessagesResponseFormat.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SearchMessagesRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SearchMessagesRequest copyWith(
          void Function(SearchMessagesRequest) updates) =>
      super.copyWith((message) => updates(message as SearchMessagesRequest))
          as SearchMessagesRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use SearchMessagesRequest() / SearchMessagesRequest.new instead')
  static SearchMessagesRequest create() => SearchMessagesRequest._();
  static $pb.GeneratedMessage $_createMessage() => SearchMessagesRequest._();
  @$core.override
  SearchMessagesRequest createEmptyInstance() => SearchMessagesRequest._();
  @$core.pragma('dart2js:noInline')
  static SearchMessagesRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SearchMessagesRequest>(
          SearchMessagesRequest.$_createMessage);
  static SearchMessagesRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get query => $_getSZ(0);
  @$pb.TagNumber(1)
  set query($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasQuery() => $_has(0);
  @$pb.TagNumber(1)
  void clearQuery() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get sessionId => $_getSZ(1);
  @$pb.TagNumber(2)
  set sessionId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasSessionId() => $_has(1);
  @$pb.TagNumber(2)
  void clearSessionId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.int get limit => $_getIZ(2);
  @$pb.TagNumber(3)
  set limit($core.int value) => $_setSignedInt32(2, value);
  @$pb.TagNumber(3)
  $core.bool hasLimit() => $_has(2);
  @$pb.TagNumber(3)
  void clearLimit() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get excludeSessionId => $_getSZ(3);
  @$pb.TagNumber(4)
  set excludeSessionId($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasExcludeSessionId() => $_has(3);
  @$pb.TagNumber(4)
  void clearExcludeSessionId() => $_clearField(4);

  /// Unsupported numeric values are rejected with InvalidArgument.
  @$pb.TagNumber(5)
  SearchMessagesResponseFormat get responseFormat => $_getN(4);
  @$pb.TagNumber(5)
  set responseFormat(SearchMessagesResponseFormat value) =>
      $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasResponseFormat() => $_has(4);
  @$pb.TagNumber(5)
  void clearResponseFormat() => $_clearField(5);
}

class SearchMessagesResponse extends $pb.GeneratedMessage {
  factory SearchMessagesResponse({
    $core.Iterable<$2.Message>? messages,
    $core.Iterable<SearchHit>? hits,
  }) {
    final result = SearchMessagesResponse._();
    if (messages != null) result.messages.addAll(messages);
    if (hits != null) result.hits.addAll(hits);
    return result;
  }

  SearchMessagesResponse._();

  factory SearchMessagesResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      SearchMessagesResponse()..mergeFromBuffer(data, registry);
  factory SearchMessagesResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      SearchMessagesResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SearchMessagesResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: SearchMessagesResponse.$_createMessage)
    ..pPM<$2.Message>(1, _omitFieldNames ? '' : 'messages',
        subBuilder: $2.Message.$_createMessage)
    ..pPM<SearchHit>(2, _omitFieldNames ? '' : 'hits',
        subBuilder: SearchHit.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SearchMessagesResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SearchMessagesResponse copyWith(
          void Function(SearchMessagesResponse) updates) =>
      super.copyWith((message) => updates(message as SearchMessagesResponse))
          as SearchMessagesResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use SearchMessagesResponse() / SearchMessagesResponse.new instead')
  static SearchMessagesResponse create() => SearchMessagesResponse._();
  static $pb.GeneratedMessage $_createMessage() => SearchMessagesResponse._();
  @$core.override
  SearchMessagesResponse createEmptyInstance() => SearchMessagesResponse._();
  @$core.pragma('dart2js:noInline')
  static SearchMessagesResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SearchMessagesResponse>(
          SearchMessagesResponse.$_createMessage);
  static SearchMessagesResponse? _defaultInstance;

  /// Legacy compatibility field. New consumers request HITS.
  @$pb.TagNumber(1)
  $pb.PbList<$2.Message> get messages => $_getList(0);

  @$pb.TagNumber(2)
  $pb.PbList<SearchHit> get hits => $_getList(1);
}

class GetConfigRequest extends $pb.GeneratedMessage {
  factory GetConfigRequest() => GetConfigRequest._();

  GetConfigRequest._();

  factory GetConfigRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      GetConfigRequest()..mergeFromBuffer(data, registry);
  factory GetConfigRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      GetConfigRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetConfigRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: GetConfigRequest.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetConfigRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetConfigRequest copyWith(void Function(GetConfigRequest) updates) =>
      super.copyWith((message) => updates(message as GetConfigRequest))
          as GetConfigRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use GetConfigRequest() / GetConfigRequest.new instead')
  static GetConfigRequest create() => GetConfigRequest._();
  static $pb.GeneratedMessage $_createMessage() => GetConfigRequest._();
  @$core.override
  GetConfigRequest createEmptyInstance() => GetConfigRequest._();
  @$core.pragma('dart2js:noInline')
  static GetConfigRequest getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<GetConfigRequest>(
          GetConfigRequest.$_createMessage);
  static GetConfigRequest? _defaultInstance;
}

class GetConfigResponse extends $pb.GeneratedMessage {
  factory GetConfigResponse({
    $core.Iterable<$2.ProviderConfig>? providers,
    $core.bool? approvalsEnabled,
    $core.bool? filesMcpEnabled,
  }) {
    final result = GetConfigResponse._();
    if (providers != null) result.providers.addAll(providers);
    if (approvalsEnabled != null) result.approvalsEnabled = approvalsEnabled;
    if (filesMcpEnabled != null) result.filesMcpEnabled = filesMcpEnabled;
    return result;
  }

  GetConfigResponse._();

  factory GetConfigResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      GetConfigResponse()..mergeFromBuffer(data, registry);
  factory GetConfigResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      GetConfigResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetConfigResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: GetConfigResponse.$_createMessage)
    ..pPM<$2.ProviderConfig>(1, _omitFieldNames ? '' : 'providers',
        subBuilder: $2.ProviderConfig.$_createMessage)
    ..aOB(2, _omitFieldNames ? '' : 'approvalsEnabled')
    ..aOB(3, _omitFieldNames ? '' : 'filesMcpEnabled')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetConfigResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetConfigResponse copyWith(void Function(GetConfigResponse) updates) =>
      super.copyWith((message) => updates(message as GetConfigResponse))
          as GetConfigResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use GetConfigResponse() / GetConfigResponse.new instead')
  static GetConfigResponse create() => GetConfigResponse._();
  static $pb.GeneratedMessage $_createMessage() => GetConfigResponse._();
  @$core.override
  GetConfigResponse createEmptyInstance() => GetConfigResponse._();
  @$core.pragma('dart2js:noInline')
  static GetConfigResponse getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<GetConfigResponse>(
          GetConfigResponse.$_createMessage);
  static GetConfigResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<$2.ProviderConfig> get providers => $_getList(0);

  @$pb.TagNumber(2)
  $core.bool get approvalsEnabled => $_getBF(1);
  @$pb.TagNumber(2)
  set approvalsEnabled($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasApprovalsEnabled() => $_has(1);
  @$pb.TagNumber(2)
  void clearApprovalsEnabled() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.bool get filesMcpEnabled => $_getBF(2);
  @$pb.TagNumber(3)
  set filesMcpEnabled($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasFilesMcpEnabled() => $_has(2);
  @$pb.TagNumber(3)
  void clearFilesMcpEnabled() => $_clearField(3);
}

class ListAgentsRequest extends $pb.GeneratedMessage {
  factory ListAgentsRequest() => ListAgentsRequest._();

  ListAgentsRequest._();

  factory ListAgentsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListAgentsRequest()..mergeFromBuffer(data, registry);
  factory ListAgentsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListAgentsRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListAgentsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListAgentsRequest.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAgentsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAgentsRequest copyWith(void Function(ListAgentsRequest) updates) =>
      super.copyWith((message) => updates(message as ListAgentsRequest))
          as ListAgentsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use ListAgentsRequest() / ListAgentsRequest.new instead')
  static ListAgentsRequest create() => ListAgentsRequest._();
  static $pb.GeneratedMessage $_createMessage() => ListAgentsRequest._();
  @$core.override
  ListAgentsRequest createEmptyInstance() => ListAgentsRequest._();
  @$core.pragma('dart2js:noInline')
  static ListAgentsRequest getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<ListAgentsRequest>(
          ListAgentsRequest.$_createMessage);
  static ListAgentsRequest? _defaultInstance;
}

class ListAgentsResponse extends $pb.GeneratedMessage {
  factory ListAgentsResponse({
    $core.Iterable<$2.AgentDescriptor>? agents,
  }) {
    final result = ListAgentsResponse._();
    if (agents != null) result.agents.addAll(agents);
    return result;
  }

  ListAgentsResponse._();

  factory ListAgentsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListAgentsResponse()..mergeFromBuffer(data, registry);
  factory ListAgentsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListAgentsResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListAgentsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListAgentsResponse.$_createMessage)
    ..pPM<$2.AgentDescriptor>(1, _omitFieldNames ? '' : 'agents',
        subBuilder: $2.AgentDescriptor.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAgentsResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAgentsResponse copyWith(void Function(ListAgentsResponse) updates) =>
      super.copyWith((message) => updates(message as ListAgentsResponse))
          as ListAgentsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use ListAgentsResponse() / ListAgentsResponse.new instead')
  static ListAgentsResponse create() => ListAgentsResponse._();
  static $pb.GeneratedMessage $_createMessage() => ListAgentsResponse._();
  @$core.override
  ListAgentsResponse createEmptyInstance() => ListAgentsResponse._();
  @$core.pragma('dart2js:noInline')
  static ListAgentsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListAgentsResponse>(
          ListAgentsResponse.$_createMessage);
  static ListAgentsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<$2.AgentDescriptor> get agents => $_getList(0);
}

class ToolDescriptor extends $pb.GeneratedMessage {
  factory ToolDescriptor({
    $core.String? serverName,
    $core.String? toolName,
    $2.ToolPolicy? policy,
  }) {
    final result = ToolDescriptor._();
    if (serverName != null) result.serverName = serverName;
    if (toolName != null) result.toolName = toolName;
    if (policy != null) result.policy = policy;
    return result;
  }

  ToolDescriptor._();

  factory ToolDescriptor.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ToolDescriptor()..mergeFromBuffer(data, registry);
  factory ToolDescriptor.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ToolDescriptor()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ToolDescriptor',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ToolDescriptor.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'serverName')
    ..aOS(2, _omitFieldNames ? '' : 'toolName')
    ..aE<$2.ToolPolicy>(3, _omitFieldNames ? '' : 'policy',
        enumValues: $2.ToolPolicy.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ToolDescriptor clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ToolDescriptor copyWith(void Function(ToolDescriptor) updates) =>
      super.copyWith((message) => updates(message as ToolDescriptor))
          as ToolDescriptor;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use ToolDescriptor() / ToolDescriptor.new instead')
  static ToolDescriptor create() => ToolDescriptor._();
  static $pb.GeneratedMessage $_createMessage() => ToolDescriptor._();
  @$core.override
  ToolDescriptor createEmptyInstance() => ToolDescriptor._();
  @$core.pragma('dart2js:noInline')
  static ToolDescriptor getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<ToolDescriptor>(
          ToolDescriptor.$_createMessage);
  static ToolDescriptor? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get serverName => $_getSZ(0);
  @$pb.TagNumber(1)
  set serverName($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasServerName() => $_has(0);
  @$pb.TagNumber(1)
  void clearServerName() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get toolName => $_getSZ(1);
  @$pb.TagNumber(2)
  set toolName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasToolName() => $_has(1);
  @$pb.TagNumber(2)
  void clearToolName() => $_clearField(2);

  @$pb.TagNumber(3)
  $2.ToolPolicy get policy => $_getN(2);
  @$pb.TagNumber(3)
  set policy($2.ToolPolicy value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasPolicy() => $_has(2);
  @$pb.TagNumber(3)
  void clearPolicy() => $_clearField(3);
}

class ListToolsRequest extends $pb.GeneratedMessage {
  factory ListToolsRequest() => ListToolsRequest._();

  ListToolsRequest._();

  factory ListToolsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListToolsRequest()..mergeFromBuffer(data, registry);
  factory ListToolsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListToolsRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListToolsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListToolsRequest.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListToolsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListToolsRequest copyWith(void Function(ListToolsRequest) updates) =>
      super.copyWith((message) => updates(message as ListToolsRequest))
          as ListToolsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use ListToolsRequest() / ListToolsRequest.new instead')
  static ListToolsRequest create() => ListToolsRequest._();
  static $pb.GeneratedMessage $_createMessage() => ListToolsRequest._();
  @$core.override
  ListToolsRequest createEmptyInstance() => ListToolsRequest._();
  @$core.pragma('dart2js:noInline')
  static ListToolsRequest getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<ListToolsRequest>(
          ListToolsRequest.$_createMessage);
  static ListToolsRequest? _defaultInstance;
}

class ListToolsResponse extends $pb.GeneratedMessage {
  factory ListToolsResponse({
    $core.Iterable<ToolDescriptor>? tools,
  }) {
    final result = ListToolsResponse._();
    if (tools != null) result.tools.addAll(tools);
    return result;
  }

  ListToolsResponse._();

  factory ListToolsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListToolsResponse()..mergeFromBuffer(data, registry);
  factory ListToolsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListToolsResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListToolsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListToolsResponse.$_createMessage)
    ..pPM<ToolDescriptor>(1, _omitFieldNames ? '' : 'tools',
        subBuilder: ToolDescriptor.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListToolsResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListToolsResponse copyWith(void Function(ListToolsResponse) updates) =>
      super.copyWith((message) => updates(message as ListToolsResponse))
          as ListToolsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use ListToolsResponse() / ListToolsResponse.new instead')
  static ListToolsResponse create() => ListToolsResponse._();
  static $pb.GeneratedMessage $_createMessage() => ListToolsResponse._();
  @$core.override
  ListToolsResponse createEmptyInstance() => ListToolsResponse._();
  @$core.pragma('dart2js:noInline')
  static ListToolsResponse getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<ListToolsResponse>(
          ListToolsResponse.$_createMessage);
  static ListToolsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<ToolDescriptor> get tools => $_getList(0);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
