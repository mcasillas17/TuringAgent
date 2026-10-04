// This is a generated file - do not edit.
//
// Generated from turing/v1/team.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

import 'team.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'team.pbenum.dart';

/// A registered tool the profile's patterns match that its runs would still
/// not receive, and why.
class AgentProfileToolExclusion extends $pb.GeneratedMessage {
  factory AgentProfileToolExclusion({
    $core.String? tool,
    $core.String? reason,
  }) {
    final result = AgentProfileToolExclusion._();
    if (tool != null) result.tool = tool;
    if (reason != null) result.reason = reason;
    return result;
  }

  AgentProfileToolExclusion._();

  factory AgentProfileToolExclusion.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      AgentProfileToolExclusion()..mergeFromBuffer(data, registry);
  factory AgentProfileToolExclusion.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      AgentProfileToolExclusion()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'AgentProfileToolExclusion',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: AgentProfileToolExclusion.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'tool')
    ..aOS(2, _omitFieldNames ? '' : 'reason')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AgentProfileToolExclusion clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AgentProfileToolExclusion copyWith(
          void Function(AgentProfileToolExclusion) updates) =>
      super.copyWith((message) => updates(message as AgentProfileToolExclusion))
          as AgentProfileToolExclusion;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use AgentProfileToolExclusion() / AgentProfileToolExclusion.new instead')
  static AgentProfileToolExclusion create() => AgentProfileToolExclusion._();
  static $pb.GeneratedMessage $_createMessage() =>
      AgentProfileToolExclusion._();
  @$core.override
  AgentProfileToolExclusion createEmptyInstance() =>
      AgentProfileToolExclusion._();
  @$core.pragma('dart2js:noInline')
  static AgentProfileToolExclusion getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<AgentProfileToolExclusion>(
          AgentProfileToolExclusion.$_createMessage);
  static AgentProfileToolExclusion? _defaultInstance;

  /// Qualified as server/tool.
  @$pb.TagNumber(1)
  $core.String get tool => $_getSZ(0);
  @$pb.TagNumber(1)
  set tool($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasTool() => $_has(0);
  @$pb.TagNumber(1)
  void clearTool() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get reason => $_getSZ(1);
  @$pb.TagNumber(2)
  set reason($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasReason() => $_has(1);
  @$pb.TagNumber(2)
  void clearReason() => $_clearField(2);
}

/// A team/{id}/AGENT.md file and the user's decisions about it. Everything but
/// enabled and granted_revision is read from disk on every call.
class AgentProfile extends $pb.GeneratedMessage {
  factory AgentProfile({
    $core.String? profileId,
    $core.String? name,
    $core.String? emoji,
    $core.String? description,
    $core.String? version,
    $core.String? model,
    $core.String? resolvedModel,
    $core.Iterable<$core.String>? tools,
    $core.Iterable<$core.String>? skills,
    AgentProfileMemoryAccess? memory,
    $core.Iterable<$core.String>? requires,
    $core.int? maxToolCalls,
    $core.String? revision,
    $core.bool? enabled,
    $core.String? grantedRevision,
    AgentProfileState? state,
    $core.Iterable<$core.String>? resolvedTools,
    $core.Iterable<$core.String>? unavailableReasons,
    $core.Iterable<AgentProfileToolExclusion>? excludedTools,
    $core.String? parseError,
  }) {
    final result = AgentProfile._();
    if (profileId != null) result.profileId = profileId;
    if (name != null) result.name = name;
    if (emoji != null) result.emoji = emoji;
    if (description != null) result.description = description;
    if (version != null) result.version = version;
    if (model != null) result.model = model;
    if (resolvedModel != null) result.resolvedModel = resolvedModel;
    if (tools != null) result.tools.addAll(tools);
    if (skills != null) result.skills.addAll(skills);
    if (memory != null) result.memory = memory;
    if (requires != null) result.requires.addAll(requires);
    if (maxToolCalls != null) result.maxToolCalls = maxToolCalls;
    if (revision != null) result.revision = revision;
    if (enabled != null) result.enabled = enabled;
    if (grantedRevision != null) result.grantedRevision = grantedRevision;
    if (state != null) result.state = state;
    if (resolvedTools != null) result.resolvedTools.addAll(resolvedTools);
    if (unavailableReasons != null)
      result.unavailableReasons.addAll(unavailableReasons);
    if (excludedTools != null) result.excludedTools.addAll(excludedTools);
    if (parseError != null) result.parseError = parseError;
    return result;
  }

  AgentProfile._();

  factory AgentProfile.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      AgentProfile()..mergeFromBuffer(data, registry);
  factory AgentProfile.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      AgentProfile()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'AgentProfile',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: AgentProfile.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'profileId')
    ..aOS(2, _omitFieldNames ? '' : 'name')
    ..aOS(3, _omitFieldNames ? '' : 'emoji')
    ..aOS(4, _omitFieldNames ? '' : 'description')
    ..aOS(5, _omitFieldNames ? '' : 'version')
    ..aOS(6, _omitFieldNames ? '' : 'model')
    ..aOS(7, _omitFieldNames ? '' : 'resolvedModel')
    ..pPS(8, _omitFieldNames ? '' : 'tools')
    ..pPS(9, _omitFieldNames ? '' : 'skills')
    ..aE<AgentProfileMemoryAccess>(10, _omitFieldNames ? '' : 'memory',
        enumValues: AgentProfileMemoryAccess.values)
    ..pPS(11, _omitFieldNames ? '' : 'requires')
    ..aI(12, _omitFieldNames ? '' : 'maxToolCalls')
    ..aOS(13, _omitFieldNames ? '' : 'revision')
    ..aOB(14, _omitFieldNames ? '' : 'enabled')
    ..aOS(15, _omitFieldNames ? '' : 'grantedRevision')
    ..aE<AgentProfileState>(16, _omitFieldNames ? '' : 'state',
        enumValues: AgentProfileState.values)
    ..pPS(17, _omitFieldNames ? '' : 'resolvedTools')
    ..pPS(18, _omitFieldNames ? '' : 'unavailableReasons')
    ..pPM<AgentProfileToolExclusion>(19, _omitFieldNames ? '' : 'excludedTools',
        subBuilder: AgentProfileToolExclusion.$_createMessage)
    ..aOS(20, _omitFieldNames ? '' : 'parseError')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AgentProfile clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  AgentProfile copyWith(void Function(AgentProfile) updates) =>
      super.copyWith((message) => updates(message as AgentProfile))
          as AgentProfile;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use AgentProfile() / AgentProfile.new instead')
  static AgentProfile create() => AgentProfile._();
  static $pb.GeneratedMessage $_createMessage() => AgentProfile._();
  @$core.override
  AgentProfile createEmptyInstance() => AgentProfile._();
  @$core.pragma('dart2js:noInline')
  static AgentProfile getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<AgentProfile>(
          AgentProfile.$_createMessage);
  static AgentProfile? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get profileId => $_getSZ(0);
  @$pb.TagNumber(1)
  set profileId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasProfileId() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfileId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get name => $_getSZ(1);
  @$pb.TagNumber(2)
  set name($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasName() => $_has(1);
  @$pb.TagNumber(2)
  void clearName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get emoji => $_getSZ(2);
  @$pb.TagNumber(3)
  set emoji($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasEmoji() => $_has(2);
  @$pb.TagNumber(3)
  void clearEmoji() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get description => $_getSZ(3);
  @$pb.TagNumber(4)
  set description($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasDescription() => $_has(3);
  @$pb.TagNumber(4)
  void clearDescription() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.String get version => $_getSZ(4);
  @$pb.TagNumber(5)
  set version($core.String value) => $_setString(4, value);
  @$pb.TagNumber(5)
  $core.bool hasVersion() => $_has(4);
  @$pb.TagNumber(5)
  void clearVersion() => $_clearField(5);

  /// The declared local model. Empty means Turing's default model.
  @$pb.TagNumber(6)
  $core.String get model => $_getSZ(5);
  @$pb.TagNumber(6)
  set model($core.String value) => $_setString(5, value);
  @$pb.TagNumber(6)
  $core.bool hasModel() => $_has(5);
  @$pb.TagNumber(6)
  void clearModel() => $_clearField(6);

  /// The model a delegated run would use now. Empty when no worker serves one.
  @$pb.TagNumber(7)
  $core.String get resolvedModel => $_getSZ(6);
  @$pb.TagNumber(7)
  set resolvedModel($core.String value) => $_setString(6, value);
  @$pb.TagNumber(7)
  $core.bool hasResolvedModel() => $_has(6);
  @$pb.TagNumber(7)
  void clearResolvedModel() => $_clearField(7);

  /// The declared tool patterns.
  @$pb.TagNumber(8)
  $pb.PbList<$core.String> get tools => $_getList(7);

  /// The declared skill patterns.
  @$pb.TagNumber(9)
  $pb.PbList<$core.String> get skills => $_getList(8);

  @$pb.TagNumber(10)
  AgentProfileMemoryAccess get memory => $_getN(9);
  @$pb.TagNumber(10)
  set memory(AgentProfileMemoryAccess value) => $_setField(10, value);
  @$pb.TagNumber(10)
  $core.bool hasMemory() => $_has(9);
  @$pb.TagNumber(10)
  void clearMemory() => $_clearField(10);

  /// Tool patterns that must resolve for the profile to be available.
  @$pb.TagNumber(11)
  $pb.PbList<$core.String> get requires => $_getList(10);

  /// The declared limit. Zero means the worker's own per-run limit.
  @$pb.TagNumber(12)
  $core.int get maxToolCalls => $_getIZ(11);
  @$pb.TagNumber(12)
  set maxToolCalls($core.int value) => $_setSignedInt32(11, value);
  @$pb.TagNumber(12)
  $core.bool hasMaxToolCalls() => $_has(11);
  @$pb.TagNumber(12)
  void clearMaxToolCalls() => $_clearField(12);

  /// Hash of the authority fields. A grant is bound to it.
  @$pb.TagNumber(13)
  $core.String get revision => $_getSZ(12);
  @$pb.TagNumber(13)
  set revision($core.String value) => $_setString(12, value);
  @$pb.TagNumber(13)
  $core.bool hasRevision() => $_has(12);
  @$pb.TagNumber(13)
  void clearRevision() => $_clearField(13);

  @$pb.TagNumber(14)
  $core.bool get enabled => $_getBF(13);
  @$pb.TagNumber(14)
  set enabled($core.bool value) => $_setBool(13, value);
  @$pb.TagNumber(14)
  $core.bool hasEnabled() => $_has(13);
  @$pb.TagNumber(14)
  void clearEnabled() => $_clearField(14);

  /// The revision the user granted, or empty.
  @$pb.TagNumber(15)
  $core.String get grantedRevision => $_getSZ(14);
  @$pb.TagNumber(15)
  set grantedRevision($core.String value) => $_setString(14, value);
  @$pb.TagNumber(15)
  $core.bool hasGrantedRevision() => $_has(14);
  @$pb.TagNumber(15)
  void clearGrantedRevision() => $_clearField(15);

  @$pb.TagNumber(16)
  AgentProfileState get state => $_getN(15);
  @$pb.TagNumber(16)
  set state(AgentProfileState value) => $_setField(16, value);
  @$pb.TagNumber(16)
  $core.bool hasState() => $_has(15);
  @$pb.TagNumber(16)
  void clearState() => $_clearField(16);

  /// The tools a delegated run would receive now, qualified as server/tool.
  @$pb.TagNumber(17)
  $pb.PbList<$core.String> get resolvedTools => $_getList(16);

  /// Why the profile cannot be active even when enabled and granted.
  @$pb.TagNumber(18)
  $pb.PbList<$core.String> get unavailableReasons => $_getList(17);

  @$pb.TagNumber(19)
  $pb.PbList<AgentProfileToolExclusion> get excludedTools => $_getList(18);

  @$pb.TagNumber(20)
  $core.String get parseError => $_getSZ(19);
  @$pb.TagNumber(20)
  set parseError($core.String value) => $_setString(19, value);
  @$pb.TagNumber(20)
  $core.bool hasParseError() => $_has(19);
  @$pb.TagNumber(20)
  void clearParseError() => $_clearField(20);
}

class ListAgentProfilesRequest extends $pb.GeneratedMessage {
  factory ListAgentProfilesRequest() => ListAgentProfilesRequest._();

  ListAgentProfilesRequest._();

  factory ListAgentProfilesRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListAgentProfilesRequest()..mergeFromBuffer(data, registry);
  factory ListAgentProfilesRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListAgentProfilesRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListAgentProfilesRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListAgentProfilesRequest.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAgentProfilesRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAgentProfilesRequest copyWith(
          void Function(ListAgentProfilesRequest) updates) =>
      super.copyWith((message) => updates(message as ListAgentProfilesRequest))
          as ListAgentProfilesRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use ListAgentProfilesRequest() / ListAgentProfilesRequest.new instead')
  static ListAgentProfilesRequest create() => ListAgentProfilesRequest._();
  static $pb.GeneratedMessage $_createMessage() => ListAgentProfilesRequest._();
  @$core.override
  ListAgentProfilesRequest createEmptyInstance() =>
      ListAgentProfilesRequest._();
  @$core.pragma('dart2js:noInline')
  static ListAgentProfilesRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListAgentProfilesRequest>(
          ListAgentProfilesRequest.$_createMessage);
  static ListAgentProfilesRequest? _defaultInstance;
}

class ListAgentProfilesResponse extends $pb.GeneratedMessage {
  factory ListAgentProfilesResponse({
    $core.Iterable<AgentProfile>? profiles,
  }) {
    final result = ListAgentProfilesResponse._();
    if (profiles != null) result.profiles.addAll(profiles);
    return result;
  }

  ListAgentProfilesResponse._();

  factory ListAgentProfilesResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListAgentProfilesResponse()..mergeFromBuffer(data, registry);
  factory ListAgentProfilesResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListAgentProfilesResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListAgentProfilesResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListAgentProfilesResponse.$_createMessage)
    ..pPM<AgentProfile>(1, _omitFieldNames ? '' : 'profiles',
        subBuilder: AgentProfile.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAgentProfilesResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListAgentProfilesResponse copyWith(
          void Function(ListAgentProfilesResponse) updates) =>
      super.copyWith((message) => updates(message as ListAgentProfilesResponse))
          as ListAgentProfilesResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use ListAgentProfilesResponse() / ListAgentProfilesResponse.new instead')
  static ListAgentProfilesResponse create() => ListAgentProfilesResponse._();
  static $pb.GeneratedMessage $_createMessage() =>
      ListAgentProfilesResponse._();
  @$core.override
  ListAgentProfilesResponse createEmptyInstance() =>
      ListAgentProfilesResponse._();
  @$core.pragma('dart2js:noInline')
  static ListAgentProfilesResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListAgentProfilesResponse>(
          ListAgentProfilesResponse.$_createMessage);
  static ListAgentProfilesResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<AgentProfile> get profiles => $_getList(0);
}

class SetAgentProfileEnabledRequest extends $pb.GeneratedMessage {
  factory SetAgentProfileEnabledRequest({
    $core.String? profileId,
    $core.bool? enabled,
  }) {
    final result = SetAgentProfileEnabledRequest._();
    if (profileId != null) result.profileId = profileId;
    if (enabled != null) result.enabled = enabled;
    return result;
  }

  SetAgentProfileEnabledRequest._();

  factory SetAgentProfileEnabledRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      SetAgentProfileEnabledRequest()..mergeFromBuffer(data, registry);
  factory SetAgentProfileEnabledRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      SetAgentProfileEnabledRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetAgentProfileEnabledRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: SetAgentProfileEnabledRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'profileId')
    ..aOB(2, _omitFieldNames ? '' : 'enabled')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetAgentProfileEnabledRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetAgentProfileEnabledRequest copyWith(
          void Function(SetAgentProfileEnabledRequest) updates) =>
      super.copyWith(
              (message) => updates(message as SetAgentProfileEnabledRequest))
          as SetAgentProfileEnabledRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use SetAgentProfileEnabledRequest() / SetAgentProfileEnabledRequest.new instead')
  static SetAgentProfileEnabledRequest create() =>
      SetAgentProfileEnabledRequest._();
  static $pb.GeneratedMessage $_createMessage() =>
      SetAgentProfileEnabledRequest._();
  @$core.override
  SetAgentProfileEnabledRequest createEmptyInstance() =>
      SetAgentProfileEnabledRequest._();
  @$core.pragma('dart2js:noInline')
  static SetAgentProfileEnabledRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetAgentProfileEnabledRequest>(
          SetAgentProfileEnabledRequest.$_createMessage);
  static SetAgentProfileEnabledRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get profileId => $_getSZ(0);
  @$pb.TagNumber(1)
  set profileId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasProfileId() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfileId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.bool get enabled => $_getBF(1);
  @$pb.TagNumber(2)
  set enabled($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasEnabled() => $_has(1);
  @$pb.TagNumber(2)
  void clearEnabled() => $_clearField(2);
}

class GrantAgentProfileRequest extends $pb.GeneratedMessage {
  factory GrantAgentProfileRequest({
    $core.String? profileId,
    $core.String? revision,
  }) {
    final result = GrantAgentProfileRequest._();
    if (profileId != null) result.profileId = profileId;
    if (revision != null) result.revision = revision;
    return result;
  }

  GrantAgentProfileRequest._();

  factory GrantAgentProfileRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      GrantAgentProfileRequest()..mergeFromBuffer(data, registry);
  factory GrantAgentProfileRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      GrantAgentProfileRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'GrantAgentProfileRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: GrantAgentProfileRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'profileId')
    ..aOS(2, _omitFieldNames ? '' : 'revision')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GrantAgentProfileRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  GrantAgentProfileRequest copyWith(
          void Function(GrantAgentProfileRequest) updates) =>
      super.copyWith((message) => updates(message as GrantAgentProfileRequest))
          as GrantAgentProfileRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use GrantAgentProfileRequest() / GrantAgentProfileRequest.new instead')
  static GrantAgentProfileRequest create() => GrantAgentProfileRequest._();
  static $pb.GeneratedMessage $_createMessage() => GrantAgentProfileRequest._();
  @$core.override
  GrantAgentProfileRequest createEmptyInstance() =>
      GrantAgentProfileRequest._();
  @$core.pragma('dart2js:noInline')
  static GrantAgentProfileRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<GrantAgentProfileRequest>(
          GrantAgentProfileRequest.$_createMessage);
  static GrantAgentProfileRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get profileId => $_getSZ(0);
  @$pb.TagNumber(1)
  set profileId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasProfileId() => $_has(0);
  @$pb.TagNumber(1)
  void clearProfileId() => $_clearField(1);

  /// The revision the user reviewed. It is refused once an authority field
  /// (model, tools, skills, memory, requires, max_tool_calls) has changed since;
  /// edits to the instructions or display fields keep the revision.
  @$pb.TagNumber(2)
  $core.String get revision => $_getSZ(1);
  @$pb.TagNumber(2)
  set revision($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRevision() => $_has(1);
  @$pb.TagNumber(2)
  void clearRevision() => $_clearField(2);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
