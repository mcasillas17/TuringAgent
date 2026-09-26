// This is a generated file - do not edit.
//
// Generated from turing/v1/health.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

class HealthCheckRequest extends $pb.GeneratedMessage {
  factory HealthCheckRequest() => HealthCheckRequest._();

  HealthCheckRequest._();

  factory HealthCheckRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      HealthCheckRequest()..mergeFromBuffer(data, registry);
  factory HealthCheckRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      HealthCheckRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'HealthCheckRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: HealthCheckRequest.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  HealthCheckRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  HealthCheckRequest copyWith(void Function(HealthCheckRequest) updates) =>
      super.copyWith((message) => updates(message as HealthCheckRequest))
          as HealthCheckRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use HealthCheckRequest() / HealthCheckRequest.new instead')
  static HealthCheckRequest create() => HealthCheckRequest._();
  static $pb.GeneratedMessage $_createMessage() => HealthCheckRequest._();
  @$core.override
  HealthCheckRequest createEmptyInstance() => HealthCheckRequest._();
  @$core.pragma('dart2js:noInline')
  static HealthCheckRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<HealthCheckRequest>(
          HealthCheckRequest.$_createMessage);
  static HealthCheckRequest? _defaultInstance;
}

class HealthCheckResponse extends $pb.GeneratedMessage {
  factory HealthCheckResponse({
    $core.bool? ok,
  }) {
    final result = HealthCheckResponse._();
    if (ok != null) result.ok = ok;
    return result;
  }

  HealthCheckResponse._();

  factory HealthCheckResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      HealthCheckResponse()..mergeFromBuffer(data, registry);
  factory HealthCheckResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      HealthCheckResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'HealthCheckResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: HealthCheckResponse.$_createMessage)
    ..aOB(1, _omitFieldNames ? '' : 'ok')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  HealthCheckResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  HealthCheckResponse copyWith(void Function(HealthCheckResponse) updates) =>
      super.copyWith((message) => updates(message as HealthCheckResponse))
          as HealthCheckResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core
      .Deprecated('Use HealthCheckResponse() / HealthCheckResponse.new instead')
  static HealthCheckResponse create() => HealthCheckResponse._();
  static $pb.GeneratedMessage $_createMessage() => HealthCheckResponse._();
  @$core.override
  HealthCheckResponse createEmptyInstance() => HealthCheckResponse._();
  @$core.pragma('dart2js:noInline')
  static HealthCheckResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<HealthCheckResponse>(
          HealthCheckResponse.$_createMessage);
  static HealthCheckResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $core.bool get ok => $_getBF(0);
  @$pb.TagNumber(1)
  set ok($core.bool value) => $_setBool(0, value);
  @$pb.TagNumber(1)
  $core.bool hasOk() => $_has(0);
  @$pb.TagNumber(1)
  void clearOk() => $_clearField(1);
}

class VersionRequest extends $pb.GeneratedMessage {
  factory VersionRequest() => VersionRequest._();

  VersionRequest._();

  factory VersionRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      VersionRequest()..mergeFromBuffer(data, registry);
  factory VersionRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      VersionRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'VersionRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: VersionRequest.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  VersionRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  VersionRequest copyWith(void Function(VersionRequest) updates) =>
      super.copyWith((message) => updates(message as VersionRequest))
          as VersionRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use VersionRequest() / VersionRequest.new instead')
  static VersionRequest create() => VersionRequest._();
  static $pb.GeneratedMessage $_createMessage() => VersionRequest._();
  @$core.override
  VersionRequest createEmptyInstance() => VersionRequest._();
  @$core.pragma('dart2js:noInline')
  static VersionRequest getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<VersionRequest>(
          VersionRequest.$_createMessage);
  static VersionRequest? _defaultInstance;
}

class VersionResponse extends $pb.GeneratedMessage {
  factory VersionResponse({
    $core.String? version,
    $core.String? schemaVersion,
  }) {
    final result = VersionResponse._();
    if (version != null) result.version = version;
    if (schemaVersion != null) result.schemaVersion = schemaVersion;
    return result;
  }

  VersionResponse._();

  factory VersionResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      VersionResponse()..mergeFromBuffer(data, registry);
  factory VersionResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      VersionResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'VersionResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: VersionResponse.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'version')
    ..aOS(2, _omitFieldNames ? '' : 'schemaVersion')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  VersionResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  VersionResponse copyWith(void Function(VersionResponse) updates) =>
      super.copyWith((message) => updates(message as VersionResponse))
          as VersionResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use VersionResponse() / VersionResponse.new instead')
  static VersionResponse create() => VersionResponse._();
  static $pb.GeneratedMessage $_createMessage() => VersionResponse._();
  @$core.override
  VersionResponse createEmptyInstance() => VersionResponse._();
  @$core.pragma('dart2js:noInline')
  static VersionResponse getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<VersionResponse>(
          VersionResponse.$_createMessage);
  static VersionResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get version => $_getSZ(0);
  @$pb.TagNumber(1)
  set version($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasVersion() => $_has(0);
  @$pb.TagNumber(1)
  void clearVersion() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get schemaVersion => $_getSZ(1);
  @$pb.TagNumber(2)
  set schemaVersion($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasSchemaVersion() => $_has(1);
  @$pb.TagNumber(2)
  void clearSchemaVersion() => $_clearField(2);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
