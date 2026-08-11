// This is a generated file - do not edit.
//
// Generated from turing/v1/mcp.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;

import '../../google/protobuf/struct.pb.dart' as $0;

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

class McpRequest extends $pb.GeneratedMessage {
  factory McpRequest({
    $core.String? serverName,
    $core.String? method,
    $0.Struct? params,
  }) {
    final result = create();
    if (serverName != null) result.serverName = serverName;
    if (method != null) result.method = method;
    if (params != null) result.params = params;
    return result;
  }

  McpRequest._();

  factory McpRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory McpRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'McpRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'serverName')
    ..aOS(2, _omitFieldNames ? '' : 'method')
    ..aOM<$0.Struct>(3, _omitFieldNames ? '' : 'params',
        subBuilder: $0.Struct.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  McpRequest clone() => McpRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  McpRequest copyWith(void Function(McpRequest) updates) =>
      super.copyWith((message) => updates(message as McpRequest)) as McpRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static McpRequest create() => McpRequest._();
  @$core.override
  McpRequest createEmptyInstance() => create();
  static $pb.PbList<McpRequest> createRepeated() => $pb.PbList<McpRequest>();
  @$core.pragma('dart2js:noInline')
  static McpRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<McpRequest>(create);
  static McpRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get serverName => $_getSZ(0);
  @$pb.TagNumber(1)
  set serverName($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasServerName() => $_has(0);
  @$pb.TagNumber(1)
  void clearServerName() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get method => $_getSZ(1);
  @$pb.TagNumber(2)
  set method($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasMethod() => $_has(1);
  @$pb.TagNumber(2)
  void clearMethod() => $_clearField(2);

  @$pb.TagNumber(3)
  $0.Struct get params => $_getN(2);
  @$pb.TagNumber(3)
  set params($0.Struct value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasParams() => $_has(2);
  @$pb.TagNumber(3)
  void clearParams() => $_clearField(3);
  @$pb.TagNumber(3)
  $0.Struct ensureParams() => $_ensure(2);
}

class McpResult extends $pb.GeneratedMessage {
  factory McpResult({
    $0.Struct? result,
  }) {
    final result$ = create();
    if (result != null) result$.result = result;
    return result$;
  }

  McpResult._();

  factory McpResult.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory McpResult.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'McpResult',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOM<$0.Struct>(1, _omitFieldNames ? '' : 'result',
        subBuilder: $0.Struct.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  McpResult clone() => McpResult()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  McpResult copyWith(void Function(McpResult) updates) =>
      super.copyWith((message) => updates(message as McpResult)) as McpResult;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static McpResult create() => McpResult._();
  @$core.override
  McpResult createEmptyInstance() => create();
  static $pb.PbList<McpResult> createRepeated() => $pb.PbList<McpResult>();
  @$core.pragma('dart2js:noInline')
  static McpResult getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<McpResult>(create);
  static McpResult? _defaultInstance;

  @$pb.TagNumber(1)
  $0.Struct get result => $_getN(0);
  @$pb.TagNumber(1)
  set result($0.Struct value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasResult() => $_has(0);
  @$pb.TagNumber(1)
  void clearResult() => $_clearField(1);
  @$pb.TagNumber(1)
  $0.Struct ensureResult() => $_ensure(0);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
