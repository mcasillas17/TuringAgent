// This is a generated file - do not edit.
//
// Generated from turing/v1/skills.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

/// A SKILL.md file and the user's decisions about whether it may load.
/// Metadata and body come from disk on every read; only enabled and capability
/// grants are persisted in SQLite.
class Skill extends $pb.GeneratedMessage {
  factory Skill({
    $core.String? skillId,
    $core.String? name,
    $core.String? description,
    $core.String? body,
    $core.String? category,
    $core.String? version,
    $core.String? author,
    $core.String? license,
    $core.Iterable<$core.String>? requires,
    $core.Iterable<$core.String>? grantedCapabilities,
    $core.Iterable<$core.String>? missingCapabilities,
    $core.bool? enabled,
    $core.String? parseError,
    $core.String? folderPath,
  }) {
    final result = create();
    if (skillId != null) result.skillId = skillId;
    if (name != null) result.name = name;
    if (description != null) result.description = description;
    if (body != null) result.body = body;
    if (category != null) result.category = category;
    if (version != null) result.version = version;
    if (author != null) result.author = author;
    if (license != null) result.license = license;
    if (requires != null) result.requires.addAll(requires);
    if (grantedCapabilities != null)
      result.grantedCapabilities.addAll(grantedCapabilities);
    if (missingCapabilities != null)
      result.missingCapabilities.addAll(missingCapabilities);
    if (enabled != null) result.enabled = enabled;
    if (parseError != null) result.parseError = parseError;
    if (folderPath != null) result.folderPath = folderPath;
    return result;
  }

  Skill._();

  factory Skill.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory Skill.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'Skill',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'skillId')
    ..aOS(2, _omitFieldNames ? '' : 'name')
    ..aOS(3, _omitFieldNames ? '' : 'description')
    ..aOS(4, _omitFieldNames ? '' : 'body')
    ..aOS(5, _omitFieldNames ? '' : 'category')
    ..aOS(6, _omitFieldNames ? '' : 'version')
    ..aOS(7, _omitFieldNames ? '' : 'author')
    ..aOS(8, _omitFieldNames ? '' : 'license')
    ..pPS(9, _omitFieldNames ? '' : 'requires')
    ..pPS(10, _omitFieldNames ? '' : 'grantedCapabilities')
    ..pPS(11, _omitFieldNames ? '' : 'missingCapabilities')
    ..aOB(12, _omitFieldNames ? '' : 'enabled')
    ..aOS(13, _omitFieldNames ? '' : 'parseError')
    ..aOS(14, _omitFieldNames ? '' : 'folderPath')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Skill clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  Skill copyWith(void Function(Skill) updates) =>
      super.copyWith((message) => updates(message as Skill)) as Skill;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static Skill create() => Skill._();
  @$core.override
  Skill createEmptyInstance() => create();
  static $pb.PbList<Skill> createRepeated() => $pb.PbList<Skill>();
  @$core.pragma('dart2js:noInline')
  static Skill getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<Skill>(create);
  static Skill? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get skillId => $_getSZ(0);
  @$pb.TagNumber(1)
  set skillId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSkillId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSkillId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get name => $_getSZ(1);
  @$pb.TagNumber(2)
  set name($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasName() => $_has(1);
  @$pb.TagNumber(2)
  void clearName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get description => $_getSZ(2);
  @$pb.TagNumber(3)
  set description($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasDescription() => $_has(2);
  @$pb.TagNumber(3)
  void clearDescription() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get body => $_getSZ(3);
  @$pb.TagNumber(4)
  set body($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasBody() => $_has(3);
  @$pb.TagNumber(4)
  void clearBody() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get category => $_getSZ(4);
  @$pb.TagNumber(5)
  set category($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasCategory() => $_has(4);
  @$pb.TagNumber(5)
  void clearCategory() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.String get version => $_getSZ(5);
  @$pb.TagNumber(6)
  set version($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasVersion() => $_has(5);
  @$pb.TagNumber(6)
  void clearVersion() => $_clearField(6);

  @$pb.TagNumber(7)
  $core.String get author => $_getSZ(6);
  @$pb.TagNumber(7)
  set author($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasAuthor() => $_has(6);
  @$pb.TagNumber(7)
  void clearAuthor() => $_clearField(7);

  @$pb.TagNumber(8)
  $core.String get license => $_getSZ(7);
  @$pb.TagNumber(8)
  set license($core.String value) => $_setString(7, value);
  @$pb.TagNumber(8)
  $core.bool hasLicense() => $_has(7);
  @$pb.TagNumber(8)
  void clearLicense() => $_clearField(8);

  @$pb.TagNumber(9)
  $pb.PbList<$core.String> get requires => $_getList(8);

  @$pb.TagNumber(10)
  $pb.PbList<$core.String> get grantedCapabilities => $_getList(9);

  @$pb.TagNumber(11)
  $pb.PbList<$core.String> get missingCapabilities => $_getList(10);

  @$pb.TagNumber(12)
  $core.bool get enabled => $_getBF(11);
  @$pb.TagNumber(12)
  set enabled($core.bool value) => $_setBool(11, value);
  @$pb.TagNumber(12)
  $core.bool hasEnabled() => $_has(11);
  @$pb.TagNumber(12)
  void clearEnabled() => $_clearField(12);

  @$pb.TagNumber(13)
  $core.String get parseError => $_getSZ(12);
  @$pb.TagNumber(13)
  set parseError($core.String value) => $_setString(12, value);
  @$pb.TagNumber(13)
  $core.bool hasParseError() => $_has(12);
  @$pb.TagNumber(13)
  void clearParseError() => $_clearField(13);

  @$pb.TagNumber(14)
  $core.String get folderPath => $_getSZ(13);
  @$pb.TagNumber(14)
  set folderPath($core.String value) => $_setString(13, value);
  @$pb.TagNumber(14)
  $core.bool hasFolderPath() => $_has(13);
  @$pb.TagNumber(14)
  void clearFolderPath() => $_clearField(14);
}

class ListSkillsRequest extends $pb.GeneratedMessage {
  factory ListSkillsRequest() => create();

  ListSkillsRequest._();

  factory ListSkillsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListSkillsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListSkillsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListSkillsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListSkillsRequest copyWith(void Function(ListSkillsRequest) updates) =>
      super.copyWith((message) => updates(message as ListSkillsRequest))
          as ListSkillsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListSkillsRequest create() => ListSkillsRequest._();
  @$core.override
  ListSkillsRequest createEmptyInstance() => create();
  static $pb.PbList<ListSkillsRequest> createRepeated() =>
      $pb.PbList<ListSkillsRequest>();
  @$core.pragma('dart2js:noInline')
  static ListSkillsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListSkillsRequest>(create);
  static ListSkillsRequest? _defaultInstance;
}

class ListSkillsResponse extends $pb.GeneratedMessage {
  factory ListSkillsResponse({
    $core.Iterable<Skill>? skills,
  }) {
    final result = create();
    if (skills != null) result.skills.addAll(skills);
    return result;
  }

  ListSkillsResponse._();

  factory ListSkillsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListSkillsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListSkillsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..pPM<Skill>(1, _omitFieldNames ? '' : 'skills', subBuilder: Skill.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListSkillsResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListSkillsResponse copyWith(void Function(ListSkillsResponse) updates) =>
      super.copyWith((message) => updates(message as ListSkillsResponse))
          as ListSkillsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListSkillsResponse create() => ListSkillsResponse._();
  @$core.override
  ListSkillsResponse createEmptyInstance() => create();
  static $pb.PbList<ListSkillsResponse> createRepeated() =>
      $pb.PbList<ListSkillsResponse>();
  @$core.pragma('dart2js:noInline')
  static ListSkillsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListSkillsResponse>(create);
  static ListSkillsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<Skill> get skills => $_getList(0);
}

class GetSkillRequest extends $pb.GeneratedMessage {
  factory GetSkillRequest({
    $core.String? skillId,
  }) {
    final result = create();
    if (skillId != null) result.skillId = skillId;
    return result;
  }

  GetSkillRequest._();

  factory GetSkillRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory GetSkillRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GetSkillRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'skillId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetSkillRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GetSkillRequest copyWith(void Function(GetSkillRequest) updates) =>
      super.copyWith((message) => updates(message as GetSkillRequest))
          as GetSkillRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static GetSkillRequest create() => GetSkillRequest._();
  @$core.override
  GetSkillRequest createEmptyInstance() => create();
  static $pb.PbList<GetSkillRequest> createRepeated() =>
      $pb.PbList<GetSkillRequest>();
  @$core.pragma('dart2js:noInline')
  static GetSkillRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GetSkillRequest>(create);
  static GetSkillRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get skillId => $_getSZ(0);
  @$pb.TagNumber(1)
  set skillId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSkillId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSkillId() => $_clearField(1);
}

class SetSkillEnabledRequest extends $pb.GeneratedMessage {
  factory SetSkillEnabledRequest({
    $core.String? skillId,
    $core.bool? enabled,
  }) {
    final result = create();
    if (skillId != null) result.skillId = skillId;
    if (enabled != null) result.enabled = enabled;
    return result;
  }

  SetSkillEnabledRequest._();

  factory SetSkillEnabledRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SetSkillEnabledRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetSkillEnabledRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'skillId')
    ..aOB(2, _omitFieldNames ? '' : 'enabled')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetSkillEnabledRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetSkillEnabledRequest copyWith(
          void Function(SetSkillEnabledRequest) updates) =>
      super.copyWith((message) => updates(message as SetSkillEnabledRequest))
          as SetSkillEnabledRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetSkillEnabledRequest create() => SetSkillEnabledRequest._();
  @$core.override
  SetSkillEnabledRequest createEmptyInstance() => create();
  static $pb.PbList<SetSkillEnabledRequest> createRepeated() =>
      $pb.PbList<SetSkillEnabledRequest>();
  @$core.pragma('dart2js:noInline')
  static SetSkillEnabledRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetSkillEnabledRequest>(create);
  static SetSkillEnabledRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get skillId => $_getSZ(0);
  @$pb.TagNumber(1)
  set skillId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSkillId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSkillId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.bool get enabled => $_getBF(1);
  @$pb.TagNumber(2)
  set enabled($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasEnabled() => $_has(1);
  @$pb.TagNumber(2)
  void clearEnabled() => $_clearField(2);
}

class SetSkillCapabilityGrantRequest extends $pb.GeneratedMessage {
  factory SetSkillCapabilityGrantRequest({
    $core.String? skillId,
    $core.String? capability,
    $core.bool? granted,
  }) {
    final result = create();
    if (skillId != null) result.skillId = skillId;
    if (capability != null) result.capability = capability;
    if (granted != null) result.granted = granted;
    return result;
  }

  SetSkillCapabilityGrantRequest._();

  factory SetSkillCapabilityGrantRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SetSkillCapabilityGrantRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetSkillCapabilityGrantRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'skillId')
    ..aOS(2, _omitFieldNames ? '' : 'capability')
    ..aOB(3, _omitFieldNames ? '' : 'granted')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetSkillCapabilityGrantRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetSkillCapabilityGrantRequest copyWith(
          void Function(SetSkillCapabilityGrantRequest) updates) =>
      super.copyWith(
              (message) => updates(message as SetSkillCapabilityGrantRequest))
          as SetSkillCapabilityGrantRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetSkillCapabilityGrantRequest create() =>
      SetSkillCapabilityGrantRequest._();
  @$core.override
  SetSkillCapabilityGrantRequest createEmptyInstance() => create();
  static $pb.PbList<SetSkillCapabilityGrantRequest> createRepeated() =>
      $pb.PbList<SetSkillCapabilityGrantRequest>();
  @$core.pragma('dart2js:noInline')
  static SetSkillCapabilityGrantRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetSkillCapabilityGrantRequest>(create);
  static SetSkillCapabilityGrantRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get skillId => $_getSZ(0);
  @$pb.TagNumber(1)
  set skillId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasSkillId() => $_has(0);
  @$pb.TagNumber(1)
  void clearSkillId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get capability => $_getSZ(1);
  @$pb.TagNumber(2)
  set capability($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasCapability() => $_has(1);
  @$pb.TagNumber(2)
  void clearCapability() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.bool get granted => $_getBF(2);
  @$pb.TagNumber(3)
  set granted($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasGranted() => $_has(2);
  @$pb.TagNumber(3)
  void clearGranted() => $_clearField(3);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
