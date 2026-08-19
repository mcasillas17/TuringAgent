// This is a generated file - do not edit.
//
// Generated from turing/v1/agents.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

import '../../google/protobuf/timestamp.pb.dart' as $1;
import 'agents.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'agents.pbenum.dart';

/// An assistant that does NOT run on this machine.
///
/// Turing's own local assistant is not one of these: it is the default and it
/// cannot be configured away. Everything described here is somewhere a
/// conversation can be sent instead, which means the transcript leaves the
/// machine — the thing this project's first commitment is about. Nothing routes
/// to one of these unless a person picked it for a specific conversation.
class ExternalAgent extends $pb.GeneratedMessage {
  factory ExternalAgent({
    $core.String? agentId,
    $core.String? displayName,
    ExternalAgentProvider? provider,
    $core.String? baseUrl,
    $core.String? model,
    $core.String? credentialRef,
    $core.bool? credentialAvailable,
    $1.Timestamp? createdAt,
    $1.Timestamp? updatedAt,
  }) {
    final result = create();
    if (agentId != null) result.agentId = agentId;
    if (displayName != null) result.displayName = displayName;
    if (provider != null) result.provider = provider;
    if (baseUrl != null) result.baseUrl = baseUrl;
    if (model != null) result.model = model;
    if (credentialRef != null) result.credentialRef = credentialRef;
    if (credentialAvailable != null)
      result.credentialAvailable = credentialAvailable;
    if (createdAt != null) result.createdAt = createdAt;
    if (updatedAt != null) result.updatedAt = updatedAt;
    return result;
  }

  ExternalAgent._();

  factory ExternalAgent.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ExternalAgent.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ExternalAgent',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'agentId')
    ..aOS(2, _omitFieldNames ? '' : 'displayName')
    ..e<ExternalAgentProvider>(
        3, _omitFieldNames ? '' : 'provider', $pb.PbFieldType.OE,
        defaultOrMaker:
            ExternalAgentProvider.EXTERNAL_AGENT_PROVIDER_UNSPECIFIED,
        valueOf: ExternalAgentProvider.valueOf,
        enumValues: ExternalAgentProvider.values)
    ..aOS(4, _omitFieldNames ? '' : 'baseUrl')
    ..aOS(5, _omitFieldNames ? '' : 'model')
    ..aOS(6, _omitFieldNames ? '' : 'credentialRef')
    ..aOB(7, _omitFieldNames ? '' : 'credentialAvailable')
    ..aOM<$1.Timestamp>(8, _omitFieldNames ? '' : 'createdAt',
        subBuilder: $1.Timestamp.create)
    ..aOM<$1.Timestamp>(9, _omitFieldNames ? '' : 'updatedAt',
        subBuilder: $1.Timestamp.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ExternalAgent clone() => ExternalAgent()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ExternalAgent copyWith(void Function(ExternalAgent) updates) =>
      super.copyWith((message) => updates(message as ExternalAgent))
          as ExternalAgent;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ExternalAgent create() => ExternalAgent._();
  @$core.override
  ExternalAgent createEmptyInstance() => create();
  static $pb.PbList<ExternalAgent> createRepeated() =>
      $pb.PbList<ExternalAgent>();
  @$core.pragma('dart2js:noInline')
  static ExternalAgent getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ExternalAgent>(create);
  static ExternalAgent? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get agentId => $_getSZ(0);
  @$pb.TagNumber(1)
  set agentId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAgentId() => $_has(0);
  @$pb.TagNumber(1)
  void clearAgentId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get displayName => $_getSZ(1);
  @$pb.TagNumber(2)
  set displayName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasDisplayName() => $_has(1);
  @$pb.TagNumber(2)
  void clearDisplayName() => $_clearField(2);

  @$pb.TagNumber(3)
  ExternalAgentProvider get provider => $_getN(2);
  @$pb.TagNumber(3)
  set provider(ExternalAgentProvider value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasProvider() => $_has(2);
  @$pb.TagNumber(3)
  void clearProvider() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get baseUrl => $_getSZ(3);
  @$pb.TagNumber(4)
  set baseUrl($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasBaseUrl() => $_has(3);
  @$pb.TagNumber(4)
  void clearBaseUrl() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get model => $_getSZ(4);
  @$pb.TagNumber(5)
  set model($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasModel() => $_has(4);
  @$pb.TagNumber(5)
  void clearModel() => $_clearField(5);

  /// The NAME of a credential, never the credential itself. The API key lives
  /// in the backend's environment (TURING_AGENT_API_KEYS in
  /// turing-backend/.env) and is resolved there; it is never stored in SQLite,
  /// never sent to a client, and never accepted from one.
  @$pb.TagNumber(6)
  $core.String get credentialRef => $_getSZ(5);
  @$pb.TagNumber(6)
  set credentialRef($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasCredentialRef() => $_has(5);
  @$pb.TagNumber(6)
  void clearCredentialRef() => $_clearField(6);

  /// Whether the backend can currently resolve credential_ref. Reported so the
  /// client can say "this will not work yet" instead of letting the user find
  /// out by sending a message that fails.
  @$pb.TagNumber(7)
  $core.bool get credentialAvailable => $_getBF(6);
  @$pb.TagNumber(7)
  set credentialAvailable($core.bool value) => $_setBool(6, value);
  @$pb.TagNumber(7)
  $core.bool hasCredentialAvailable() => $_has(6);
  @$pb.TagNumber(7)
  void clearCredentialAvailable() => $_clearField(7);

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

  @$pb.TagNumber(9)
  $1.Timestamp get updatedAt => $_getN(8);
  @$pb.TagNumber(9)
  set updatedAt($1.Timestamp value) => $_setField(9, value);
  @$pb.TagNumber(9)
  $core.bool hasUpdatedAt() => $_has(8);
  @$pb.TagNumber(9)
  void clearUpdatedAt() => $_clearField(9);
  @$pb.TagNumber(9)
  $1.Timestamp ensureUpdatedAt() => $_ensure(8);
}

class CreateExternalAgentRequest extends $pb.GeneratedMessage {
  factory CreateExternalAgentRequest({
    $core.String? displayName,
    ExternalAgentProvider? provider,
    $core.String? baseUrl,
    $core.String? model,
    $core.String? credentialRef,
  }) {
    final result = create();
    if (displayName != null) result.displayName = displayName;
    if (provider != null) result.provider = provider;
    if (baseUrl != null) result.baseUrl = baseUrl;
    if (model != null) result.model = model;
    if (credentialRef != null) result.credentialRef = credentialRef;
    return result;
  }

  CreateExternalAgentRequest._();

  factory CreateExternalAgentRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory CreateExternalAgentRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CreateExternalAgentRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'displayName')
    ..e<ExternalAgentProvider>(
        2, _omitFieldNames ? '' : 'provider', $pb.PbFieldType.OE,
        defaultOrMaker:
            ExternalAgentProvider.EXTERNAL_AGENT_PROVIDER_UNSPECIFIED,
        valueOf: ExternalAgentProvider.valueOf,
        enumValues: ExternalAgentProvider.values)
    ..aOS(3, _omitFieldNames ? '' : 'baseUrl')
    ..aOS(4, _omitFieldNames ? '' : 'model')
    ..aOS(5, _omitFieldNames ? '' : 'credentialRef')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CreateExternalAgentRequest clone() =>
      CreateExternalAgentRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CreateExternalAgentRequest copyWith(
          void Function(CreateExternalAgentRequest) updates) =>
      super.copyWith(
              (message) => updates(message as CreateExternalAgentRequest))
          as CreateExternalAgentRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static CreateExternalAgentRequest create() => CreateExternalAgentRequest._();
  @$core.override
  CreateExternalAgentRequest createEmptyInstance() => create();
  static $pb.PbList<CreateExternalAgentRequest> createRepeated() =>
      $pb.PbList<CreateExternalAgentRequest>();
  @$core.pragma('dart2js:noInline')
  static CreateExternalAgentRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CreateExternalAgentRequest>(create);
  static CreateExternalAgentRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get displayName => $_getSZ(0);
  @$pb.TagNumber(1)
  set displayName($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasDisplayName() => $_has(0);
  @$pb.TagNumber(1)
  void clearDisplayName() => $_clearField(1);

  @$pb.TagNumber(2)
  ExternalAgentProvider get provider => $_getN(1);
  @$pb.TagNumber(2)
  set provider(ExternalAgentProvider value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasProvider() => $_has(1);
  @$pb.TagNumber(2)
  void clearProvider() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get baseUrl => $_getSZ(2);
  @$pb.TagNumber(3)
  set baseUrl($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasBaseUrl() => $_has(2);
  @$pb.TagNumber(3)
  void clearBaseUrl() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get model => $_getSZ(3);
  @$pb.TagNumber(4)
  set model($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasModel() => $_has(3);
  @$pb.TagNumber(4)
  void clearModel() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get credentialRef => $_getSZ(4);
  @$pb.TagNumber(5)
  set credentialRef($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasCredentialRef() => $_has(4);
  @$pb.TagNumber(5)
  void clearCredentialRef() => $_clearField(5);
}

class UpdateExternalAgentRequest extends $pb.GeneratedMessage {
  factory UpdateExternalAgentRequest({
    $core.String? agentId,
    $core.String? displayName,
    ExternalAgentProvider? provider,
    $core.String? baseUrl,
    $core.String? model,
    $core.String? credentialRef,
  }) {
    final result = create();
    if (agentId != null) result.agentId = agentId;
    if (displayName != null) result.displayName = displayName;
    if (provider != null) result.provider = provider;
    if (baseUrl != null) result.baseUrl = baseUrl;
    if (model != null) result.model = model;
    if (credentialRef != null) result.credentialRef = credentialRef;
    return result;
  }

  UpdateExternalAgentRequest._();

  factory UpdateExternalAgentRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory UpdateExternalAgentRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'UpdateExternalAgentRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'agentId')
    ..aOS(2, _omitFieldNames ? '' : 'displayName')
    ..e<ExternalAgentProvider>(
        3, _omitFieldNames ? '' : 'provider', $pb.PbFieldType.OE,
        defaultOrMaker:
            ExternalAgentProvider.EXTERNAL_AGENT_PROVIDER_UNSPECIFIED,
        valueOf: ExternalAgentProvider.valueOf,
        enumValues: ExternalAgentProvider.values)
    ..aOS(4, _omitFieldNames ? '' : 'baseUrl')
    ..aOS(5, _omitFieldNames ? '' : 'model')
    ..aOS(6, _omitFieldNames ? '' : 'credentialRef')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UpdateExternalAgentRequest clone() =>
      UpdateExternalAgentRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UpdateExternalAgentRequest copyWith(
          void Function(UpdateExternalAgentRequest) updates) =>
      super.copyWith(
              (message) => updates(message as UpdateExternalAgentRequest))
          as UpdateExternalAgentRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static UpdateExternalAgentRequest create() => UpdateExternalAgentRequest._();
  @$core.override
  UpdateExternalAgentRequest createEmptyInstance() => create();
  static $pb.PbList<UpdateExternalAgentRequest> createRepeated() =>
      $pb.PbList<UpdateExternalAgentRequest>();
  @$core.pragma('dart2js:noInline')
  static UpdateExternalAgentRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<UpdateExternalAgentRequest>(create);
  static UpdateExternalAgentRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get agentId => $_getSZ(0);
  @$pb.TagNumber(1)
  set agentId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAgentId() => $_has(0);
  @$pb.TagNumber(1)
  void clearAgentId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get displayName => $_getSZ(1);
  @$pb.TagNumber(2)
  set displayName($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasDisplayName() => $_has(1);
  @$pb.TagNumber(2)
  void clearDisplayName() => $_clearField(2);

  @$pb.TagNumber(3)
  ExternalAgentProvider get provider => $_getN(2);
  @$pb.TagNumber(3)
  set provider(ExternalAgentProvider value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasProvider() => $_has(2);
  @$pb.TagNumber(3)
  void clearProvider() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get baseUrl => $_getSZ(3);
  @$pb.TagNumber(4)
  set baseUrl($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasBaseUrl() => $_has(3);
  @$pb.TagNumber(4)
  void clearBaseUrl() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get model => $_getSZ(4);
  @$pb.TagNumber(5)
  set model($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasModel() => $_has(4);
  @$pb.TagNumber(5)
  void clearModel() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get credentialRef => $_getSZ(5);
  @$pb.TagNumber(6)
  set credentialRef($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasCredentialRef() => $_has(5);
  @$pb.TagNumber(6)
  void clearCredentialRef() => $_clearField(6);
}

class DeleteExternalAgentRequest extends $pb.GeneratedMessage {
  factory DeleteExternalAgentRequest({
    $core.String? agentId,
  }) {
    final result = create();
    if (agentId != null) result.agentId = agentId;
    return result;
  }

  DeleteExternalAgentRequest._();

  factory DeleteExternalAgentRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DeleteExternalAgentRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DeleteExternalAgentRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'agentId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteExternalAgentRequest clone() =>
      DeleteExternalAgentRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteExternalAgentRequest copyWith(
          void Function(DeleteExternalAgentRequest) updates) =>
      super.copyWith(
              (message) => updates(message as DeleteExternalAgentRequest))
          as DeleteExternalAgentRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DeleteExternalAgentRequest create() => DeleteExternalAgentRequest._();
  @$core.override
  DeleteExternalAgentRequest createEmptyInstance() => create();
  static $pb.PbList<DeleteExternalAgentRequest> createRepeated() =>
      $pb.PbList<DeleteExternalAgentRequest>();
  @$core.pragma('dart2js:noInline')
  static DeleteExternalAgentRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DeleteExternalAgentRequest>(create);
  static DeleteExternalAgentRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get agentId => $_getSZ(0);
  @$pb.TagNumber(1)
  set agentId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasAgentId() => $_has(0);
  @$pb.TagNumber(1)
  void clearAgentId() => $_clearField(1);
}

class DeleteExternalAgentResponse extends $pb.GeneratedMessage {
  factory DeleteExternalAgentResponse() => create();

  DeleteExternalAgentResponse._();

  factory DeleteExternalAgentResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DeleteExternalAgentResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DeleteExternalAgentResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteExternalAgentResponse clone() =>
      DeleteExternalAgentResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteExternalAgentResponse copyWith(
          void Function(DeleteExternalAgentResponse) updates) =>
      super.copyWith(
              (message) => updates(message as DeleteExternalAgentResponse))
          as DeleteExternalAgentResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DeleteExternalAgentResponse create() =>
      DeleteExternalAgentResponse._();
  @$core.override
  DeleteExternalAgentResponse createEmptyInstance() => create();
  static $pb.PbList<DeleteExternalAgentResponse> createRepeated() =>
      $pb.PbList<DeleteExternalAgentResponse>();
  @$core.pragma('dart2js:noInline')
  static DeleteExternalAgentResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DeleteExternalAgentResponse>(create);
  static DeleteExternalAgentResponse? _defaultInstance;
}

class ListExternalAgentsRequest extends $pb.GeneratedMessage {
  factory ListExternalAgentsRequest() => create();

  ListExternalAgentsRequest._();

  factory ListExternalAgentsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListExternalAgentsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListExternalAgentsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListExternalAgentsRequest clone() =>
      ListExternalAgentsRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListExternalAgentsRequest copyWith(
          void Function(ListExternalAgentsRequest) updates) =>
      super.copyWith((message) => updates(message as ListExternalAgentsRequest))
          as ListExternalAgentsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListExternalAgentsRequest create() => ListExternalAgentsRequest._();
  @$core.override
  ListExternalAgentsRequest createEmptyInstance() => create();
  static $pb.PbList<ListExternalAgentsRequest> createRepeated() =>
      $pb.PbList<ListExternalAgentsRequest>();
  @$core.pragma('dart2js:noInline')
  static ListExternalAgentsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListExternalAgentsRequest>(create);
  static ListExternalAgentsRequest? _defaultInstance;
}

class ListExternalAgentsResponse extends $pb.GeneratedMessage {
  factory ListExternalAgentsResponse({
    $core.Iterable<ExternalAgent>? agents,
  }) {
    final result = create();
    if (agents != null) result.agents.addAll(agents);
    return result;
  }

  ListExternalAgentsResponse._();

  factory ListExternalAgentsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListExternalAgentsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListExternalAgentsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..pc<ExternalAgent>(1, _omitFieldNames ? '' : 'agents', $pb.PbFieldType.PM,
        subBuilder: ExternalAgent.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListExternalAgentsResponse clone() =>
      ListExternalAgentsResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListExternalAgentsResponse copyWith(
          void Function(ListExternalAgentsResponse) updates) =>
      super.copyWith(
              (message) => updates(message as ListExternalAgentsResponse))
          as ListExternalAgentsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListExternalAgentsResponse create() => ListExternalAgentsResponse._();
  @$core.override
  ListExternalAgentsResponse createEmptyInstance() => create();
  static $pb.PbList<ListExternalAgentsResponse> createRepeated() =>
      $pb.PbList<ListExternalAgentsResponse>();
  @$core.pragma('dart2js:noInline')
  static ListExternalAgentsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListExternalAgentsResponse>(create);
  static ListExternalAgentsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<ExternalAgent> get agents => $_getList(0);
}

class GetSessionAgentRequest extends $pb.GeneratedMessage {
  factory GetSessionAgentRequest({
    $core.String? sessionId,
  }) {
    final result = create();
    if (sessionId != null) result.sessionId = sessionId;
    return result;
  }

  GetSessionAgentRequest._();

  factory GetSessionAgentRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetSessionAgentRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetSessionAgentRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetSessionAgentRequest clone() =>
      GetSessionAgentRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetSessionAgentRequest copyWith(
          void Function(GetSessionAgentRequest) updates) =>
      super.copyWith((message) => updates(message as GetSessionAgentRequest))
          as GetSessionAgentRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetSessionAgentRequest create() => GetSessionAgentRequest._();
  @$core.override
  GetSessionAgentRequest createEmptyInstance() => create();
  static $pb.PbList<GetSessionAgentRequest> createRepeated() =>
      $pb.PbList<GetSessionAgentRequest>();
  @$core.pragma('dart2js:noInline')
  static GetSessionAgentRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetSessionAgentRequest>(create);
  static GetSessionAgentRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);
}

class SetSessionAgentRequest extends $pb.GeneratedMessage {
  factory SetSessionAgentRequest({
    $core.String? sessionId,
    $core.String? agentId,
  }) {
    final result = create();
    if (sessionId != null) result.sessionId = sessionId;
    if (agentId != null) result.agentId = agentId;
    return result;
  }

  SetSessionAgentRequest._();

  factory SetSessionAgentRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SetSessionAgentRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetSessionAgentRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..aOS(2, _omitFieldNames ? '' : 'agentId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetSessionAgentRequest clone() =>
      SetSessionAgentRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetSessionAgentRequest copyWith(
          void Function(SetSessionAgentRequest) updates) =>
      super.copyWith((message) => updates(message as SetSessionAgentRequest))
          as SetSessionAgentRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetSessionAgentRequest create() => SetSessionAgentRequest._();
  @$core.override
  SetSessionAgentRequest createEmptyInstance() => create();
  static $pb.PbList<SetSessionAgentRequest> createRepeated() =>
      $pb.PbList<SetSessionAgentRequest>();
  @$core.pragma('dart2js:noInline')
  static SetSessionAgentRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetSessionAgentRequest>(create);
  static SetSessionAgentRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get agentId => $_getSZ(1);
  @$pb.TagNumber(2)
  set agentId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasAgentId() => $_has(1);
  @$pb.TagNumber(2)
  void clearAgentId() => $_clearField(2);
}

class ClearSessionAgentRequest extends $pb.GeneratedMessage {
  factory ClearSessionAgentRequest({
    $core.String? sessionId,
  }) {
    final result = create();
    if (sessionId != null) result.sessionId = sessionId;
    return result;
  }

  ClearSessionAgentRequest._();

  factory ClearSessionAgentRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ClearSessionAgentRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ClearSessionAgentRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'sessionId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ClearSessionAgentRequest clone() =>
      ClearSessionAgentRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ClearSessionAgentRequest copyWith(
          void Function(ClearSessionAgentRequest) updates) =>
      super.copyWith((message) => updates(message as ClearSessionAgentRequest))
          as ClearSessionAgentRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ClearSessionAgentRequest create() => ClearSessionAgentRequest._();
  @$core.override
  ClearSessionAgentRequest createEmptyInstance() => create();
  static $pb.PbList<ClearSessionAgentRequest> createRepeated() =>
      $pb.PbList<ClearSessionAgentRequest>();
  @$core.pragma('dart2js:noInline')
  static ClearSessionAgentRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ClearSessionAgentRequest>(create);
  static ClearSessionAgentRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get sessionId => $_getSZ(0);
  @$pb.TagNumber(1)
  set sessionId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSessionId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSessionId() => $_clearField(1);
}

/// Where a conversation's messages go. An unset agent means the local
/// assistant, which is the default and the only destination that keeps the
/// conversation on this machine.
class SessionAgentResponse extends $pb.GeneratedMessage {
  factory SessionAgentResponse({
    ExternalAgent? agent,
  }) {
    final result = create();
    if (agent != null) result.agent = agent;
    return result;
  }

  SessionAgentResponse._();

  factory SessionAgentResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SessionAgentResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SessionAgentResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOM<ExternalAgent>(1, _omitFieldNames ? '' : 'agent',
        subBuilder: ExternalAgent.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SessionAgentResponse clone() =>
      SessionAgentResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SessionAgentResponse copyWith(void Function(SessionAgentResponse) updates) =>
      super.copyWith((message) => updates(message as SessionAgentResponse))
          as SessionAgentResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SessionAgentResponse create() => SessionAgentResponse._();
  @$core.override
  SessionAgentResponse createEmptyInstance() => create();
  static $pb.PbList<SessionAgentResponse> createRepeated() =>
      $pb.PbList<SessionAgentResponse>();
  @$core.pragma('dart2js:noInline')
  static SessionAgentResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SessionAgentResponse>(create);
  static SessionAgentResponse? _defaultInstance;

  @$pb.TagNumber(1)
  ExternalAgent get agent => $_getN(0);
  @$pb.TagNumber(1)
  set agent(ExternalAgent value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasAgent() => $_has(0);
  @$pb.TagNumber(1)
  void clearAgent() => $_clearField(1);
  @$pb.TagNumber(1)
  ExternalAgent ensureAgent() => $_ensure(0);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
