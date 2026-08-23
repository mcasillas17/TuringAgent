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

import '../../google/protobuf/struct.pb.dart' as $1;
import 'common.pbenum.dart' as $2;
import 'mcp.pbenum.dart';

export 'package:protobuf/protobuf.dart' show GeneratedMessageGenericExtensions;

export 'mcp.pbenum.dart';

class McpToolDescriptor extends $pb.GeneratedMessage {
  factory McpToolDescriptor({
    $core.String? toolName,
    $2.ToolPolicy? policy,
    $1.Struct? schema,
    $core.bool? enabled,
    $core.bool? present,
  }) {
    final result = create();
    if (toolName != null) result.toolName = toolName;
    if (policy != null) result.policy = policy;
    if (schema != null) result.schema = schema;
    if (enabled != null) result.enabled = enabled;
    if (present != null) result.present = present;
    return result;
  }

  McpToolDescriptor._();

  factory McpToolDescriptor.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory McpToolDescriptor.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'McpToolDescriptor',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'toolName')
    ..e<$2.ToolPolicy>(2, _omitFieldNames ? '' : 'policy', $pb.PbFieldType.OE,
        defaultOrMaker: $2.ToolPolicy.TOOL_POLICY_UNSPECIFIED,
        valueOf: $2.ToolPolicy.valueOf,
        enumValues: $2.ToolPolicy.values)
    ..aOM<$1.Struct>(3, _omitFieldNames ? '' : 'schema',
        subBuilder: $1.Struct.create)
    ..aOB(4, _omitFieldNames ? '' : 'enabled')
    ..aOB(5, _omitFieldNames ? '' : 'present')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  McpToolDescriptor clone() => McpToolDescriptor()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  McpToolDescriptor copyWith(void Function(McpToolDescriptor) updates) =>
      super.copyWith((message) => updates(message as McpToolDescriptor))
          as McpToolDescriptor;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static McpToolDescriptor create() => McpToolDescriptor._();
  @$core.override
  McpToolDescriptor createEmptyInstance() => create();
  static $pb.PbList<McpToolDescriptor> createRepeated() =>
      $pb.PbList<McpToolDescriptor>();
  @$core.pragma('dart2js:noInline')
  static McpToolDescriptor getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<McpToolDescriptor>(create);
  static McpToolDescriptor? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get toolName => $_getSZ(0);
  @$pb.TagNumber(1)
  set toolName($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasToolName() => $_has(0);
  @$pb.TagNumber(1)
  void clearToolName() => $_clearField(1);

  @$pb.TagNumber(2)
  $2.ToolPolicy get policy => $_getN(1);
  @$pb.TagNumber(2)
  set policy($2.ToolPolicy value) => $_setField(2, value);
  @$pb.TagNumber(2)
  $core.bool hasPolicy() => $_has(1);
  @$pb.TagNumber(2)
  void clearPolicy() => $_clearField(2);

  @$pb.TagNumber(3)
  $1.Struct get schema => $_getN(2);
  @$pb.TagNumber(3)
  set schema($1.Struct value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasSchema() => $_has(2);
  @$pb.TagNumber(3)
  void clearSchema() => $_clearField(3);
  @$pb.TagNumber(3)
  $1.Struct ensureSchema() => $_ensure(2);

  @$pb.TagNumber(4)
  $core.bool get enabled => $_getBF(3);
  @$pb.TagNumber(4)
  set enabled($core.bool value) => $_setBool(3, value);
  @$pb.TagNumber(4)
  $core.bool hasEnabled() => $_has(3);
  @$pb.TagNumber(4)
  void clearEnabled() => $_clearField(4);

  @$pb.TagNumber(5)
  $core.bool get present => $_getBF(4);
  @$pb.TagNumber(5)
  set present($core.bool value) => $_setBool(4, value);
  @$pb.TagNumber(5)
  $core.bool hasPresent() => $_has(4);
  @$pb.TagNumber(5)
  void clearPresent() => $_clearField(5);
}

class McpServerDescriptor extends $pb.GeneratedMessage {
  factory McpServerDescriptor({
    $core.String? serverId,
    $core.String? name,
    $core.String? transport,
    $core.String? url,
    McpServerTier? tier,
    $core.bool? enabled,
    McpServerLiveness? liveness,
    $core.String? statusMessage,
    $core.bool? sandboxConfined,
    $core.Iterable<McpToolDescriptor>? tools,
  }) {
    final result = create();
    if (serverId != null) result.serverId = serverId;
    if (name != null) result.name = name;
    if (transport != null) result.transport = transport;
    if (url != null) result.url = url;
    if (tier != null) result.tier = tier;
    if (enabled != null) result.enabled = enabled;
    if (liveness != null) result.liveness = liveness;
    if (statusMessage != null) result.statusMessage = statusMessage;
    if (sandboxConfined != null) result.sandboxConfined = sandboxConfined;
    if (tools != null) result.tools.addAll(tools);
    return result;
  }

  McpServerDescriptor._();

  factory McpServerDescriptor.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory McpServerDescriptor.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'McpServerDescriptor',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'serverId')
    ..aOS(2, _omitFieldNames ? '' : 'name')
    ..aOS(3, _omitFieldNames ? '' : 'transport')
    ..aOS(4, _omitFieldNames ? '' : 'url')
    ..e<McpServerTier>(5, _omitFieldNames ? '' : 'tier', $pb.PbFieldType.OE,
        defaultOrMaker: McpServerTier.MCP_SERVER_TIER_UNSPECIFIED,
        valueOf: McpServerTier.valueOf,
        enumValues: McpServerTier.values)
    ..aOB(6, _omitFieldNames ? '' : 'enabled')
    ..e<McpServerLiveness>(
        7, _omitFieldNames ? '' : 'liveness', $pb.PbFieldType.OE,
        defaultOrMaker: McpServerLiveness.MCP_SERVER_LIVENESS_UNSPECIFIED,
        valueOf: McpServerLiveness.valueOf,
        enumValues: McpServerLiveness.values)
    ..aOS(8, _omitFieldNames ? '' : 'statusMessage')
    ..aOB(9, _omitFieldNames ? '' : 'sandboxConfined')
    ..pc<McpToolDescriptor>(
        10, _omitFieldNames ? '' : 'tools', $pb.PbFieldType.PM,
        subBuilder: McpToolDescriptor.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  McpServerDescriptor clone() => McpServerDescriptor()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  McpServerDescriptor copyWith(void Function(McpServerDescriptor) updates) =>
      super.copyWith((message) => updates(message as McpServerDescriptor))
          as McpServerDescriptor;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static McpServerDescriptor create() => McpServerDescriptor._();
  @$core.override
  McpServerDescriptor createEmptyInstance() => create();
  static $pb.PbList<McpServerDescriptor> createRepeated() =>
      $pb.PbList<McpServerDescriptor>();
  @$core.pragma('dart2js:noInline')
  static McpServerDescriptor getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<McpServerDescriptor>(create);
  static McpServerDescriptor? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get serverId => $_getSZ(0);
  @$pb.TagNumber(1)
  set serverId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasServerId() => $_has(0);
  @$pb.TagNumber(1)
  void clearServerId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get name => $_getSZ(1);
  @$pb.TagNumber(2)
  set name($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasName() => $_has(1);
  @$pb.TagNumber(2)
  void clearName() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get transport => $_getSZ(2);
  @$pb.TagNumber(3)
  set transport($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasTransport() => $_has(2);
  @$pb.TagNumber(3)
  void clearTransport() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get url => $_getSZ(3);
  @$pb.TagNumber(4)
  set url($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasUrl() => $_has(3);
  @$pb.TagNumber(4)
  void clearUrl() => $_clearField(4);

  @$pb.TagNumber(5)
  McpServerTier get tier => $_getN(4);
  @$pb.TagNumber(5)
  set tier(McpServerTier value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasTier() => $_has(4);
  @$pb.TagNumber(5)
  void clearTier() => $_clearField(5);

  @$pb.TagNumber(6)
  $core.bool get enabled => $_getBF(5);
  @$pb.TagNumber(6)
  set enabled($core.bool value) => $_setBool(5, value);
  @$pb.TagNumber(6)
  $core.bool hasEnabled() => $_has(5);
  @$pb.TagNumber(6)
  void clearEnabled() => $_clearField(6);

  @$pb.TagNumber(7)
  McpServerLiveness get liveness => $_getN(6);
  @$pb.TagNumber(7)
  set liveness(McpServerLiveness value) => $_setField(7, value);
  @$pb.TagNumber(7)
  $core.bool hasLiveness() => $_has(6);
  @$pb.TagNumber(7)
  void clearLiveness() => $_clearField(7);

  @$pb.TagNumber(8)
  $core.String get statusMessage => $_getSZ(7);
  @$pb.TagNumber(8)
  set statusMessage($core.String value) => $_setString(7, value);
  @$pb.TagNumber(8)
  $core.bool hasStatusMessage() => $_has(7);
  @$pb.TagNumber(8)
  void clearStatusMessage() => $_clearField(8);

  @$pb.TagNumber(9)
  $core.bool get sandboxConfined => $_getBF(8);
  @$pb.TagNumber(9)
  set sandboxConfined($core.bool value) => $_setBool(8, value);
  @$pb.TagNumber(9)
  $core.bool hasSandboxConfined() => $_has(8);
  @$pb.TagNumber(9)
  void clearSandboxConfined() => $_clearField(9);

  @$pb.TagNumber(10)
  $pb.PbList<McpToolDescriptor> get tools => $_getList(9);
}

class UnsupportedMcpServer extends $pb.GeneratedMessage {
  factory UnsupportedMcpServer({
    $core.String? name,
    $core.String? reason,
  }) {
    final result = create();
    if (name != null) result.name = name;
    if (reason != null) result.reason = reason;
    return result;
  }

  UnsupportedMcpServer._();

  factory UnsupportedMcpServer.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory UnsupportedMcpServer.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'UnsupportedMcpServer',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'name')
    ..aOS(2, _omitFieldNames ? '' : 'reason')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UnsupportedMcpServer clone() =>
      UnsupportedMcpServer()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UnsupportedMcpServer copyWith(void Function(UnsupportedMcpServer) updates) =>
      super.copyWith((message) => updates(message as UnsupportedMcpServer))
          as UnsupportedMcpServer;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static UnsupportedMcpServer create() => UnsupportedMcpServer._();
  @$core.override
  UnsupportedMcpServer createEmptyInstance() => create();
  static $pb.PbList<UnsupportedMcpServer> createRepeated() =>
      $pb.PbList<UnsupportedMcpServer>();
  @$core.pragma('dart2js:noInline')
  static UnsupportedMcpServer getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<UnsupportedMcpServer>(create);
  static UnsupportedMcpServer? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get name => $_getSZ(0);
  @$pb.TagNumber(1)
  set name($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasName() => $_has(0);
  @$pb.TagNumber(1)
  void clearName() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get reason => $_getSZ(1);
  @$pb.TagNumber(2)
  set reason($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasReason() => $_has(1);
  @$pb.TagNumber(2)
  void clearReason() => $_clearField(2);
}

class ListMcpServersRequest extends $pb.GeneratedMessage {
  factory ListMcpServersRequest() => create();

  ListMcpServersRequest._();

  factory ListMcpServersRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListMcpServersRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListMcpServersRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMcpServersRequest clone() =>
      ListMcpServersRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMcpServersRequest copyWith(
          void Function(ListMcpServersRequest) updates) =>
      super.copyWith((message) => updates(message as ListMcpServersRequest))
          as ListMcpServersRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListMcpServersRequest create() => ListMcpServersRequest._();
  @$core.override
  ListMcpServersRequest createEmptyInstance() => create();
  static $pb.PbList<ListMcpServersRequest> createRepeated() =>
      $pb.PbList<ListMcpServersRequest>();
  @$core.pragma('dart2js:noInline')
  static ListMcpServersRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListMcpServersRequest>(create);
  static ListMcpServersRequest? _defaultInstance;
}

class ListMcpServersResponse extends $pb.GeneratedMessage {
  factory ListMcpServersResponse({
    $core.Iterable<McpServerDescriptor>? servers,
    $core.Iterable<UnsupportedMcpServer>? unsupported,
  }) {
    final result = create();
    if (servers != null) result.servers.addAll(servers);
    if (unsupported != null) result.unsupported.addAll(unsupported);
    return result;
  }

  ListMcpServersResponse._();

  factory ListMcpServersResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListMcpServersResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListMcpServersResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..pc<McpServerDescriptor>(
        1, _omitFieldNames ? '' : 'servers', $pb.PbFieldType.PM,
        subBuilder: McpServerDescriptor.create)
    ..pc<UnsupportedMcpServer>(
        2, _omitFieldNames ? '' : 'unsupported', $pb.PbFieldType.PM,
        subBuilder: UnsupportedMcpServer.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMcpServersResponse clone() =>
      ListMcpServersResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMcpServersResponse copyWith(
          void Function(ListMcpServersResponse) updates) =>
      super.copyWith((message) => updates(message as ListMcpServersResponse))
          as ListMcpServersResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListMcpServersResponse create() => ListMcpServersResponse._();
  @$core.override
  ListMcpServersResponse createEmptyInstance() => create();
  static $pb.PbList<ListMcpServersResponse> createRepeated() =>
      $pb.PbList<ListMcpServersResponse>();
  @$core.pragma('dart2js:noInline')
  static ListMcpServersResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListMcpServersResponse>(create);
  static ListMcpServersResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<McpServerDescriptor> get servers => $_getList(0);

  @$pb.TagNumber(2)
  $pb.PbList<UnsupportedMcpServer> get unsupported => $_getList(1);
}

class SetMcpServerEnabledRequest extends $pb.GeneratedMessage {
  factory SetMcpServerEnabledRequest({
    $core.String? serverId,
    $core.bool? enabled,
  }) {
    final result = create();
    if (serverId != null) result.serverId = serverId;
    if (enabled != null) result.enabled = enabled;
    return result;
  }

  SetMcpServerEnabledRequest._();

  factory SetMcpServerEnabledRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory SetMcpServerEnabledRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetMcpServerEnabledRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'serverId')
    ..aOB(2, _omitFieldNames ? '' : 'enabled')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetMcpServerEnabledRequest clone() =>
      SetMcpServerEnabledRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetMcpServerEnabledRequest copyWith(
          void Function(SetMcpServerEnabledRequest) updates) =>
      super.copyWith(
              (message) => updates(message as SetMcpServerEnabledRequest))
          as SetMcpServerEnabledRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static SetMcpServerEnabledRequest create() => SetMcpServerEnabledRequest._();
  @$core.override
  SetMcpServerEnabledRequest createEmptyInstance() => create();
  static $pb.PbList<SetMcpServerEnabledRequest> createRepeated() =>
      $pb.PbList<SetMcpServerEnabledRequest>();
  @$core.pragma('dart2js:noInline')
  static SetMcpServerEnabledRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetMcpServerEnabledRequest>(create);
  static SetMcpServerEnabledRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get serverId => $_getSZ(0);
  @$pb.TagNumber(1)
  set serverId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasServerId() => $_has(0);
  @$pb.TagNumber(1)
  void clearServerId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.bool get enabled => $_getBF(1);
  @$pb.TagNumber(2)
  set enabled($core.bool value) => $_setBool(1, value);
  @$pb.TagNumber(2)
  $core.bool hasEnabled() => $_has(1);
  @$pb.TagNumber(2)
  void clearEnabled() => $_clearField(2);
}

class UpdateMcpToolPolicyRequest extends $pb.GeneratedMessage {
  factory UpdateMcpToolPolicyRequest({
    $core.String? serverId,
    $core.String? toolName,
    $2.ToolPolicy? policy,
  }) {
    final result = create();
    if (serverId != null) result.serverId = serverId;
    if (toolName != null) result.toolName = toolName;
    if (policy != null) result.policy = policy;
    return result;
  }

  UpdateMcpToolPolicyRequest._();

  factory UpdateMcpToolPolicyRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory UpdateMcpToolPolicyRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'UpdateMcpToolPolicyRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'serverId')
    ..aOS(2, _omitFieldNames ? '' : 'toolName')
    ..e<$2.ToolPolicy>(3, _omitFieldNames ? '' : 'policy', $pb.PbFieldType.OE,
        defaultOrMaker: $2.ToolPolicy.TOOL_POLICY_UNSPECIFIED,
        valueOf: $2.ToolPolicy.valueOf,
        enumValues: $2.ToolPolicy.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UpdateMcpToolPolicyRequest clone() =>
      UpdateMcpToolPolicyRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UpdateMcpToolPolicyRequest copyWith(
          void Function(UpdateMcpToolPolicyRequest) updates) =>
      super.copyWith(
              (message) => updates(message as UpdateMcpToolPolicyRequest))
          as UpdateMcpToolPolicyRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static UpdateMcpToolPolicyRequest create() => UpdateMcpToolPolicyRequest._();
  @$core.override
  UpdateMcpToolPolicyRequest createEmptyInstance() => create();
  static $pb.PbList<UpdateMcpToolPolicyRequest> createRepeated() =>
      $pb.PbList<UpdateMcpToolPolicyRequest>();
  @$core.pragma('dart2js:noInline')
  static UpdateMcpToolPolicyRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<UpdateMcpToolPolicyRequest>(create);
  static UpdateMcpToolPolicyRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get serverId => $_getSZ(0);
  @$pb.TagNumber(1)
  set serverId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasServerId() => $_has(0);
  @$pb.TagNumber(1)
  void clearServerId() => $_clearField(1);

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

class UpdateToolPolicyByNameRequest extends $pb.GeneratedMessage {
  factory UpdateToolPolicyByNameRequest({
    $core.String? serverName,
    $core.String? toolName,
    $2.ToolPolicy? policy,
  }) {
    final result = create();
    if (serverName != null) result.serverName = serverName;
    if (toolName != null) result.toolName = toolName;
    if (policy != null) result.policy = policy;
    return result;
  }

  UpdateToolPolicyByNameRequest._();

  factory UpdateToolPolicyByNameRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory UpdateToolPolicyByNameRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'UpdateToolPolicyByNameRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'serverName')
    ..aOS(2, _omitFieldNames ? '' : 'toolName')
    ..e<$2.ToolPolicy>(3, _omitFieldNames ? '' : 'policy', $pb.PbFieldType.OE,
        defaultOrMaker: $2.ToolPolicy.TOOL_POLICY_UNSPECIFIED,
        valueOf: $2.ToolPolicy.valueOf,
        enumValues: $2.ToolPolicy.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UpdateToolPolicyByNameRequest clone() =>
      UpdateToolPolicyByNameRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UpdateToolPolicyByNameRequest copyWith(
          void Function(UpdateToolPolicyByNameRequest) updates) =>
      super.copyWith(
              (message) => updates(message as UpdateToolPolicyByNameRequest))
          as UpdateToolPolicyByNameRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static UpdateToolPolicyByNameRequest create() =>
      UpdateToolPolicyByNameRequest._();
  @$core.override
  UpdateToolPolicyByNameRequest createEmptyInstance() => create();
  static $pb.PbList<UpdateToolPolicyByNameRequest> createRepeated() =>
      $pb.PbList<UpdateToolPolicyByNameRequest>();
  @$core.pragma('dart2js:noInline')
  static UpdateToolPolicyByNameRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<UpdateToolPolicyByNameRequest>(create);
  static UpdateToolPolicyByNameRequest? _defaultInstance;

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

class ListPseudoServerToolsRequest extends $pb.GeneratedMessage {
  factory ListPseudoServerToolsRequest({
    $core.String? serverName,
  }) {
    final result = create();
    if (serverName != null) result.serverName = serverName;
    return result;
  }

  ListPseudoServerToolsRequest._();

  factory ListPseudoServerToolsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListPseudoServerToolsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListPseudoServerToolsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'serverName')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListPseudoServerToolsRequest clone() =>
      ListPseudoServerToolsRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListPseudoServerToolsRequest copyWith(
          void Function(ListPseudoServerToolsRequest) updates) =>
      super.copyWith(
              (message) => updates(message as ListPseudoServerToolsRequest))
          as ListPseudoServerToolsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListPseudoServerToolsRequest create() =>
      ListPseudoServerToolsRequest._();
  @$core.override
  ListPseudoServerToolsRequest createEmptyInstance() => create();
  static $pb.PbList<ListPseudoServerToolsRequest> createRepeated() =>
      $pb.PbList<ListPseudoServerToolsRequest>();
  @$core.pragma('dart2js:noInline')
  static ListPseudoServerToolsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListPseudoServerToolsRequest>(create);
  static ListPseudoServerToolsRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get serverName => $_getSZ(0);
  @$pb.TagNumber(1)
  set serverName($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasServerName() => $_has(0);
  @$pb.TagNumber(1)
  void clearServerName() => $_clearField(1);
}

class ListPseudoServerToolsResponse extends $pb.GeneratedMessage {
  factory ListPseudoServerToolsResponse({
    $core.Iterable<McpToolDescriptor>? tools,
  }) {
    final result = create();
    if (tools != null) result.tools.addAll(tools);
    return result;
  }

  ListPseudoServerToolsResponse._();

  factory ListPseudoServerToolsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ListPseudoServerToolsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListPseudoServerToolsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..pc<McpToolDescriptor>(
        1, _omitFieldNames ? '' : 'tools', $pb.PbFieldType.PM,
        subBuilder: McpToolDescriptor.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListPseudoServerToolsResponse clone() =>
      ListPseudoServerToolsResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListPseudoServerToolsResponse copyWith(
          void Function(ListPseudoServerToolsResponse) updates) =>
      super.copyWith(
              (message) => updates(message as ListPseudoServerToolsResponse))
          as ListPseudoServerToolsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ListPseudoServerToolsResponse create() =>
      ListPseudoServerToolsResponse._();
  @$core.override
  ListPseudoServerToolsResponse createEmptyInstance() => create();
  static $pb.PbList<ListPseudoServerToolsResponse> createRepeated() =>
      $pb.PbList<ListPseudoServerToolsResponse>();
  @$core.pragma('dart2js:noInline')
  static ListPseudoServerToolsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListPseudoServerToolsResponse>(create);
  static ListPseudoServerToolsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<McpToolDescriptor> get tools => $_getList(0);
}

class DeleteMcpServerRequest extends $pb.GeneratedMessage {
  factory DeleteMcpServerRequest({
    $core.String? serverId,
  }) {
    final result = create();
    if (serverId != null) result.serverId = serverId;
    return result;
  }

  DeleteMcpServerRequest._();

  factory DeleteMcpServerRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DeleteMcpServerRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DeleteMcpServerRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'serverId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteMcpServerRequest clone() =>
      DeleteMcpServerRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteMcpServerRequest copyWith(
          void Function(DeleteMcpServerRequest) updates) =>
      super.copyWith((message) => updates(message as DeleteMcpServerRequest))
          as DeleteMcpServerRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DeleteMcpServerRequest create() => DeleteMcpServerRequest._();
  @$core.override
  DeleteMcpServerRequest createEmptyInstance() => create();
  static $pb.PbList<DeleteMcpServerRequest> createRepeated() =>
      $pb.PbList<DeleteMcpServerRequest>();
  @$core.pragma('dart2js:noInline')
  static DeleteMcpServerRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DeleteMcpServerRequest>(create);
  static DeleteMcpServerRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get serverId => $_getSZ(0);
  @$pb.TagNumber(1)
  set serverId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasServerId() => $_has(0);
  @$pb.TagNumber(1)
  void clearServerId() => $_clearField(1);
}

class DeleteMcpServerResponse extends $pb.GeneratedMessage {
  factory DeleteMcpServerResponse() => create();

  DeleteMcpServerResponse._();

  factory DeleteMcpServerResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory DeleteMcpServerResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DeleteMcpServerResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteMcpServerResponse clone() =>
      DeleteMcpServerResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteMcpServerResponse copyWith(
          void Function(DeleteMcpServerResponse) updates) =>
      super.copyWith((message) => updates(message as DeleteMcpServerResponse))
          as DeleteMcpServerResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static DeleteMcpServerResponse create() => DeleteMcpServerResponse._();
  @$core.override
  DeleteMcpServerResponse createEmptyInstance() => create();
  static $pb.PbList<DeleteMcpServerResponse> createRepeated() =>
      $pb.PbList<DeleteMcpServerResponse>();
  @$core.pragma('dart2js:noInline')
  static DeleteMcpServerResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DeleteMcpServerResponse>(create);
  static DeleteMcpServerResponse? _defaultInstance;
}

class RegisterMcpServerRequest extends $pb.GeneratedMessage {
  factory RegisterMcpServerRequest({
    $core.String? name,
    $core.String? url,
    $core.String? bearerToken,
    McpServerTier? tier,
  }) {
    final result = create();
    if (name != null) result.name = name;
    if (url != null) result.url = url;
    if (bearerToken != null) result.bearerToken = bearerToken;
    if (tier != null) result.tier = tier;
    return result;
  }

  RegisterMcpServerRequest._();

  factory RegisterMcpServerRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RegisterMcpServerRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RegisterMcpServerRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'name')
    ..aOS(2, _omitFieldNames ? '' : 'url')
    ..aOS(3, _omitFieldNames ? '' : 'bearerToken')
    ..e<McpServerTier>(4, _omitFieldNames ? '' : 'tier', $pb.PbFieldType.OE,
        defaultOrMaker: McpServerTier.MCP_SERVER_TIER_UNSPECIFIED,
        valueOf: McpServerTier.valueOf,
        enumValues: McpServerTier.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RegisterMcpServerRequest clone() =>
      RegisterMcpServerRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RegisterMcpServerRequest copyWith(
          void Function(RegisterMcpServerRequest) updates) =>
      super.copyWith((message) => updates(message as RegisterMcpServerRequest))
          as RegisterMcpServerRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RegisterMcpServerRequest create() => RegisterMcpServerRequest._();
  @$core.override
  RegisterMcpServerRequest createEmptyInstance() => create();
  static $pb.PbList<RegisterMcpServerRequest> createRepeated() =>
      $pb.PbList<RegisterMcpServerRequest>();
  @$core.pragma('dart2js:noInline')
  static RegisterMcpServerRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RegisterMcpServerRequest>(create);
  static RegisterMcpServerRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get name => $_getSZ(0);
  @$pb.TagNumber(1)
  set name($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasName() => $_has(0);
  @$pb.TagNumber(1)
  void clearName() => $_clearField(1);

  /// Absolute HTTP(S) endpoint. The tier is always derived from the hardened
  /// URL exactly as mcp.json import derives it; `tier` below is only a caller
  /// assertion that must agree with that derivation.
  @$pb.TagNumber(2)
  $core.String get url => $_getSZ(1);
  @$pb.TagNumber(2)
  set url($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasUrl() => $_has(1);
  @$pb.TagNumber(2)
  void clearUrl() => $_clearField(2);

  /// Optional bearer token. Write-only: sealed at rest, never echoed by any
  /// response, and absent from McpServerDescriptor by construction.
  @$pb.TagNumber(3)
  $core.String get bearerToken => $_getSZ(2);
  @$pb.TagNumber(3)
  set bearerToken($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasBearerToken() => $_has(2);
  @$pb.TagNumber(3)
  void clearBearerToken() => $_clearField(3);

  /// Optional caller-declared tier. MCP_SERVER_TIER_UNSPECIFIED accepts the
  /// tier derived from `url`; any other value must match that derivation or the
  /// request is refused. MCP_SERVER_TIER_BUNDLED is never accepted.
  @$pb.TagNumber(4)
  McpServerTier get tier => $_getN(3);
  @$pb.TagNumber(4)
  set tier(McpServerTier value) => $_setField(4, value);
  @$pb.TagNumber(4)
  $core.bool hasTier() => $_has(3);
  @$pb.TagNumber(4)
  void clearTier() => $_clearField(4);
}

class ReimportMcpJsonRequest extends $pb.GeneratedMessage {
  factory ReimportMcpJsonRequest() => create();

  ReimportMcpJsonRequest._();

  factory ReimportMcpJsonRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ReimportMcpJsonRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ReimportMcpJsonRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReimportMcpJsonRequest clone() =>
      ReimportMcpJsonRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReimportMcpJsonRequest copyWith(
          void Function(ReimportMcpJsonRequest) updates) =>
      super.copyWith((message) => updates(message as ReimportMcpJsonRequest))
          as ReimportMcpJsonRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ReimportMcpJsonRequest create() => ReimportMcpJsonRequest._();
  @$core.override
  ReimportMcpJsonRequest createEmptyInstance() => create();
  static $pb.PbList<ReimportMcpJsonRequest> createRepeated() =>
      $pb.PbList<ReimportMcpJsonRequest>();
  @$core.pragma('dart2js:noInline')
  static ReimportMcpJsonRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ReimportMcpJsonRequest>(create);
  static ReimportMcpJsonRequest? _defaultInstance;
}

class ReimportMcpJsonResponse extends $pb.GeneratedMessage {
  factory ReimportMcpJsonResponse({
    $core.Iterable<$core.String>? imported,
    $core.Iterable<UnsupportedMcpServer>? unsupported,
    $core.Iterable<$core.String>? skipped,
  }) {
    final result = create();
    if (imported != null) result.imported.addAll(imported);
    if (unsupported != null) result.unsupported.addAll(unsupported);
    if (skipped != null) result.skipped.addAll(skipped);
    return result;
  }

  ReimportMcpJsonResponse._();

  factory ReimportMcpJsonResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory ReimportMcpJsonResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ReimportMcpJsonResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..pPS(1, _omitFieldNames ? '' : 'imported')
    ..pc<UnsupportedMcpServer>(
        2, _omitFieldNames ? '' : 'unsupported', $pb.PbFieldType.PM,
        subBuilder: UnsupportedMcpServer.create)
    ..pPS(3, _omitFieldNames ? '' : 'skipped')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReimportMcpJsonResponse clone() =>
      ReimportMcpJsonResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReimportMcpJsonResponse copyWith(
          void Function(ReimportMcpJsonResponse) updates) =>
      super.copyWith((message) => updates(message as ReimportMcpJsonResponse))
          as ReimportMcpJsonResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static ReimportMcpJsonResponse create() => ReimportMcpJsonResponse._();
  @$core.override
  ReimportMcpJsonResponse createEmptyInstance() => create();
  static $pb.PbList<ReimportMcpJsonResponse> createRepeated() =>
      $pb.PbList<ReimportMcpJsonResponse>();
  @$core.pragma('dart2js:noInline')
  static ReimportMcpJsonResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ReimportMcpJsonResponse>(create);
  static ReimportMcpJsonResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<$core.String> get imported => $_getList(0);

  /// Entries the registry refused to import, with a redacted reason.
  @$pb.TagNumber(2)
  $pb.PbList<UnsupportedMcpServer> get unsupported => $_getList(1);

  /// Entries left untouched because a server with that identity already exists.
  @$pb.TagNumber(3)
  $pb.PbList<$core.String> get skipped => $_getList(2);
}

class RotateMcpServerTokenRequest extends $pb.GeneratedMessage {
  factory RotateMcpServerTokenRequest({
    $core.String? serverId,
    $core.String? bearerToken,
  }) {
    final result = create();
    if (serverId != null) result.serverId = serverId;
    if (bearerToken != null) result.bearerToken = bearerToken;
    return result;
  }

  RotateMcpServerTokenRequest._();

  factory RotateMcpServerTokenRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory RotateMcpServerTokenRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RotateMcpServerTokenRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'serverId')
    ..aOS(2, _omitFieldNames ? '' : 'bearerToken')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RotateMcpServerTokenRequest clone() =>
      RotateMcpServerTokenRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RotateMcpServerTokenRequest copyWith(
          void Function(RotateMcpServerTokenRequest) updates) =>
      super.copyWith(
              (message) => updates(message as RotateMcpServerTokenRequest))
          as RotateMcpServerTokenRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static RotateMcpServerTokenRequest create() =>
      RotateMcpServerTokenRequest._();
  @$core.override
  RotateMcpServerTokenRequest createEmptyInstance() => create();
  static $pb.PbList<RotateMcpServerTokenRequest> createRepeated() =>
      $pb.PbList<RotateMcpServerTokenRequest>();
  @$core.pragma('dart2js:noInline')
  static RotateMcpServerTokenRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RotateMcpServerTokenRequest>(create);
  static RotateMcpServerTokenRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get serverId => $_getSZ(0);
  @$pb.TagNumber(1)
  set serverId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasServerId() => $_has(0);
  @$pb.TagNumber(1)
  void clearServerId() => $_clearField(1);

  /// The replacement bearer token. Empty clears the stored token. Write-only,
  /// like RegisterMcpServerRequest.bearer_token.
  @$pb.TagNumber(2)
  $core.String get bearerToken => $_getSZ(1);
  @$pb.TagNumber(2)
  set bearerToken($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasBearerToken() => $_has(1);
  @$pb.TagNumber(2)
  void clearBearerToken() => $_clearField(2);
}

class CallRegisteredMcpToolRequest extends $pb.GeneratedMessage {
  factory CallRegisteredMcpToolRequest({
    $core.String? serverId,
    $core.String? runId,
    $core.String? approvalId,
    $core.String? toolName,
    $1.Struct? args,
  }) {
    final result = create();
    if (serverId != null) result.serverId = serverId;
    if (runId != null) result.runId = runId;
    if (approvalId != null) result.approvalId = approvalId;
    if (toolName != null) result.toolName = toolName;
    if (args != null) result.args = args;
    return result;
  }

  CallRegisteredMcpToolRequest._();

  factory CallRegisteredMcpToolRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory CallRegisteredMcpToolRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CallRegisteredMcpToolRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOS(1, _omitFieldNames ? '' : 'serverId')
    ..aOS(2, _omitFieldNames ? '' : 'runId')
    ..aOS(3, _omitFieldNames ? '' : 'approvalId')
    ..aOS(4, _omitFieldNames ? '' : 'toolName')
    ..aOM<$1.Struct>(5, _omitFieldNames ? '' : 'args',
        subBuilder: $1.Struct.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CallRegisteredMcpToolRequest clone() =>
      CallRegisteredMcpToolRequest()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CallRegisteredMcpToolRequest copyWith(
          void Function(CallRegisteredMcpToolRequest) updates) =>
      super.copyWith(
              (message) => updates(message as CallRegisteredMcpToolRequest))
          as CallRegisteredMcpToolRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static CallRegisteredMcpToolRequest create() =>
      CallRegisteredMcpToolRequest._();
  @$core.override
  CallRegisteredMcpToolRequest createEmptyInstance() => create();
  static $pb.PbList<CallRegisteredMcpToolRequest> createRepeated() =>
      $pb.PbList<CallRegisteredMcpToolRequest>();
  @$core.pragma('dart2js:noInline')
  static CallRegisteredMcpToolRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CallRegisteredMcpToolRequest>(create);
  static CallRegisteredMcpToolRequest? _defaultInstance;

  @$pb.TagNumber(1)
  $core.String get serverId => $_getSZ(0);
  @$pb.TagNumber(1)
  set serverId($core.String value) => $_setString(0, value);
  @$pb.TagNumber(1)
  $core.bool hasServerId() => $_has(0);
  @$pb.TagNumber(1)
  void clearServerId() => $_clearField(1);

  @$pb.TagNumber(2)
  $core.String get runId => $_getSZ(1);
  @$pb.TagNumber(2)
  set runId($core.String value) => $_setString(1, value);
  @$pb.TagNumber(2)
  $core.bool hasRunId() => $_has(1);
  @$pb.TagNumber(2)
  void clearRunId() => $_clearField(2);

  @$pb.TagNumber(3)
  $core.String get approvalId => $_getSZ(2);
  @$pb.TagNumber(3)
  set approvalId($core.String value) => $_setString(2, value);
  @$pb.TagNumber(3)
  $core.bool hasApprovalId() => $_has(2);
  @$pb.TagNumber(3)
  void clearApprovalId() => $_clearField(3);

  @$pb.TagNumber(4)
  $core.String get toolName => $_getSZ(3);
  @$pb.TagNumber(4)
  set toolName($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasToolName() => $_has(3);
  @$pb.TagNumber(4)
  void clearToolName() => $_clearField(4);

  @$pb.TagNumber(5)
  $1.Struct get args => $_getN(4);
  @$pb.TagNumber(5)
  set args($1.Struct value) => $_setField(5, value);
  @$pb.TagNumber(5)
  $core.bool hasArgs() => $_has(4);
  @$pb.TagNumber(5)
  void clearArgs() => $_clearField(5);
  @$pb.TagNumber(5)
  $1.Struct ensureArgs() => $_ensure(4);
}

class CallRegisteredMcpToolResponse extends $pb.GeneratedMessage {
  factory CallRegisteredMcpToolResponse({
    $1.Struct? result,
  }) {
    final result$ = create();
    if (result != null) result$.result = result;
    return result$;
  }

  CallRegisteredMcpToolResponse._();

  factory CallRegisteredMcpToolResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromBuffer(data, registry);
  factory CallRegisteredMcpToolResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      create()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CallRegisteredMcpToolResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: create)
    ..aOM<$1.Struct>(1, _omitFieldNames ? '' : 'result',
        subBuilder: $1.Struct.create)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CallRegisteredMcpToolResponse clone() =>
      CallRegisteredMcpToolResponse()..mergeFromMessage(this);
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CallRegisteredMcpToolResponse copyWith(
          void Function(CallRegisteredMcpToolResponse) updates) =>
      super.copyWith(
              (message) => updates(message as CallRegisteredMcpToolResponse))
          as CallRegisteredMcpToolResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  static CallRegisteredMcpToolResponse create() =>
      CallRegisteredMcpToolResponse._();
  @$core.override
  CallRegisteredMcpToolResponse createEmptyInstance() => create();
  static $pb.PbList<CallRegisteredMcpToolResponse> createRepeated() =>
      $pb.PbList<CallRegisteredMcpToolResponse>();
  @$core.pragma('dart2js:noInline')
  static CallRegisteredMcpToolResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CallRegisteredMcpToolResponse>(create);
  static CallRegisteredMcpToolResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $1.Struct get result => $_getN(0);
  @$pb.TagNumber(1)
  set result($1.Struct value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasResult() => $_has(0);
  @$pb.TagNumber(1)
  void clearResult() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Struct ensureResult() => $_ensure(0);
}

class McpRequest extends $pb.GeneratedMessage {
  factory McpRequest({
    $core.String? serverName,
    $core.String? method,
    $1.Struct? params,
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
    ..aOM<$1.Struct>(3, _omitFieldNames ? '' : 'params',
        subBuilder: $1.Struct.create)
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
  $1.Struct get params => $_getN(2);
  @$pb.TagNumber(3)
  set params($1.Struct value) => $_setField(3, value);
  @$pb.TagNumber(3)
  $core.bool hasParams() => $_has(2);
  @$pb.TagNumber(3)
  void clearParams() => $_clearField(3);
  @$pb.TagNumber(3)
  $1.Struct ensureParams() => $_ensure(2);
}

class McpResult extends $pb.GeneratedMessage {
  factory McpResult({
    $1.Struct? result,
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
    ..aOM<$1.Struct>(1, _omitFieldNames ? '' : 'result',
        subBuilder: $1.Struct.create)
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
  $1.Struct get result => $_getN(0);
  @$pb.TagNumber(1)
  set result($1.Struct value) => $_setField(1, value);
  @$pb.TagNumber(1)
  $core.bool hasResult() => $_has(0);
  @$pb.TagNumber(1)
  void clearResult() => $_clearField(1);
  @$pb.TagNumber(1)
  $1.Struct ensureResult() => $_ensure(0);
}

const $core.bool _omitFieldNames =
    $core.bool.fromEnvironment('protobuf.omit_field_names');
const $core.bool _omitMessageNames =
    $core.bool.fromEnvironment('protobuf.omit_message_names');
