// This is a generated file - do not edit.
//
// Generated from turing/v1/mcp.proto.

// @dart = 3.3

// ignore_for_file: annotate_overrides, camel_case_types, comment_references
// ignore_for_file: constant_identifier_names
// ignore_for_file: curly_braces_in_flow_control_structures
// ignore_for_file: deprecated_member_use_from_same_package, library_prefixes
// ignore_for_file: non_constant_identifier_names, prefer_relative_imports

import 'dart:core' as $core;

import 'package:protobuf/protobuf.dart' as $pb;
import 'package:protobuf/well_known_types/google/protobuf/struct.pb.dart' as $1;

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
    final result = McpToolDescriptor._();
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
      McpToolDescriptor()..mergeFromBuffer(data, registry);
  factory McpToolDescriptor.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      McpToolDescriptor()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'McpToolDescriptor',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: McpToolDescriptor.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'toolName')
    ..aE<$2.ToolPolicy>(2, _omitFieldNames ? '' : 'policy',
        enumValues: $2.ToolPolicy.values)
    ..aOM<$1.Struct>(3, _omitFieldNames ? '' : 'schema',
        subBuilder: $1.Struct.$_createMessage)
    ..aOB(4, _omitFieldNames ? '' : 'enabled')
    ..aOB(5, _omitFieldNames ? '' : 'present')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  McpToolDescriptor clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  McpToolDescriptor copyWith(void Function(McpToolDescriptor) updates) =>
      super.copyWith((message) => updates(message as McpToolDescriptor))
          as McpToolDescriptor;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use McpToolDescriptor() / McpToolDescriptor.new instead')
  static McpToolDescriptor create() => McpToolDescriptor._();
  static $pb.GeneratedMessage $_createMessage() => McpToolDescriptor._();
  @$core.override
  McpToolDescriptor createEmptyInstance() => McpToolDescriptor._();
  @$core.pragma('dart2js:noInline')
  static McpToolDescriptor getDefault() =>
      _defaultInstance ??= $pb.GeneratedMessage.$_defaultFor<McpToolDescriptor>(
          McpToolDescriptor.$_createMessage);
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
    final result = McpServerDescriptor._();
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
      McpServerDescriptor()..mergeFromBuffer(data, registry);
  factory McpServerDescriptor.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      McpServerDescriptor()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'McpServerDescriptor',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: McpServerDescriptor.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'serverId')
    ..aOS(2, _omitFieldNames ? '' : 'name')
    ..aOS(3, _omitFieldNames ? '' : 'transport')
    ..aOS(4, _omitFieldNames ? '' : 'url')
    ..aE<McpServerTier>(5, _omitFieldNames ? '' : 'tier',
        enumValues: McpServerTier.values)
    ..aOB(6, _omitFieldNames ? '' : 'enabled')
    ..aE<McpServerLiveness>(7, _omitFieldNames ? '' : 'liveness',
        enumValues: McpServerLiveness.values)
    ..aOS(8, _omitFieldNames ? '' : 'statusMessage')
    ..aOB(9, _omitFieldNames ? '' : 'sandboxConfined')
    ..pPM<McpToolDescriptor>(10, _omitFieldNames ? '' : 'tools',
        subBuilder: McpToolDescriptor.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  McpServerDescriptor clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  McpServerDescriptor copyWith(void Function(McpServerDescriptor) updates) =>
      super.copyWith((message) => updates(message as McpServerDescriptor))
          as McpServerDescriptor;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core
      .Deprecated('Use McpServerDescriptor() / McpServerDescriptor.new instead')
  static McpServerDescriptor create() => McpServerDescriptor._();
  static $pb.GeneratedMessage $_createMessage() => McpServerDescriptor._();
  @$core.override
  McpServerDescriptor createEmptyInstance() => McpServerDescriptor._();
  @$core.pragma('dart2js:noInline')
  static McpServerDescriptor getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<McpServerDescriptor>(
          McpServerDescriptor.$_createMessage);
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
    final result = UnsupportedMcpServer._();
    if (name != null) result.name = name;
    if (reason != null) result.reason = reason;
    return result;
  }

  UnsupportedMcpServer._();

  factory UnsupportedMcpServer.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      UnsupportedMcpServer()..mergeFromBuffer(data, registry);
  factory UnsupportedMcpServer.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      UnsupportedMcpServer()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'UnsupportedMcpServer',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: UnsupportedMcpServer.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'name')
    ..aOS(2, _omitFieldNames ? '' : 'reason')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UnsupportedMcpServer clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UnsupportedMcpServer copyWith(void Function(UnsupportedMcpServer) updates) =>
      super.copyWith((message) => updates(message as UnsupportedMcpServer))
          as UnsupportedMcpServer;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use UnsupportedMcpServer() / UnsupportedMcpServer.new instead')
  static UnsupportedMcpServer create() => UnsupportedMcpServer._();
  static $pb.GeneratedMessage $_createMessage() => UnsupportedMcpServer._();
  @$core.override
  UnsupportedMcpServer createEmptyInstance() => UnsupportedMcpServer._();
  @$core.pragma('dart2js:noInline')
  static UnsupportedMcpServer getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<UnsupportedMcpServer>(
          UnsupportedMcpServer.$_createMessage);
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
  factory ListMcpServersRequest() => ListMcpServersRequest._();

  ListMcpServersRequest._();

  factory ListMcpServersRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListMcpServersRequest()..mergeFromBuffer(data, registry);
  factory ListMcpServersRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListMcpServersRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListMcpServersRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListMcpServersRequest.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMcpServersRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMcpServersRequest copyWith(
          void Function(ListMcpServersRequest) updates) =>
      super.copyWith((message) => updates(message as ListMcpServersRequest))
          as ListMcpServersRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use ListMcpServersRequest() / ListMcpServersRequest.new instead')
  static ListMcpServersRequest create() => ListMcpServersRequest._();
  static $pb.GeneratedMessage $_createMessage() => ListMcpServersRequest._();
  @$core.override
  ListMcpServersRequest createEmptyInstance() => ListMcpServersRequest._();
  @$core.pragma('dart2js:noInline')
  static ListMcpServersRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListMcpServersRequest>(
          ListMcpServersRequest.$_createMessage);
  static ListMcpServersRequest? _defaultInstance;
}

class ListMcpServersResponse extends $pb.GeneratedMessage {
  factory ListMcpServersResponse({
    $core.Iterable<McpServerDescriptor>? servers,
    $core.Iterable<UnsupportedMcpServer>? unsupported,
    $core.bool? registryDegraded,
    $core.String? registryDegradationReason,
  }) {
    final result = ListMcpServersResponse._();
    if (servers != null) result.servers.addAll(servers);
    if (unsupported != null) result.unsupported.addAll(unsupported);
    if (registryDegraded != null) result.registryDegraded = registryDegraded;
    if (registryDegradationReason != null)
      result.registryDegradationReason = registryDegradationReason;
    return result;
  }

  ListMcpServersResponse._();

  factory ListMcpServersResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListMcpServersResponse()..mergeFromBuffer(data, registry);
  factory ListMcpServersResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListMcpServersResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListMcpServersResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListMcpServersResponse.$_createMessage)
    ..pPM<McpServerDescriptor>(1, _omitFieldNames ? '' : 'servers',
        subBuilder: McpServerDescriptor.$_createMessage)
    ..pPM<UnsupportedMcpServer>(2, _omitFieldNames ? '' : 'unsupported',
        subBuilder: UnsupportedMcpServer.$_createMessage)
    ..aOB(3, _omitFieldNames ? '' : 'registryDegraded')
    ..aOS(4, _omitFieldNames ? '' : 'registryDegradationReason')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMcpServersResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListMcpServersResponse copyWith(
          void Function(ListMcpServersResponse) updates) =>
      super.copyWith((message) => updates(message as ListMcpServersResponse))
          as ListMcpServersResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use ListMcpServersResponse() / ListMcpServersResponse.new instead')
  static ListMcpServersResponse create() => ListMcpServersResponse._();
  static $pb.GeneratedMessage $_createMessage() => ListMcpServersResponse._();
  @$core.override
  ListMcpServersResponse createEmptyInstance() => ListMcpServersResponse._();
  @$core.pragma('dart2js:noInline')
  static ListMcpServersResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListMcpServersResponse>(
          ListMcpServersResponse.$_createMessage);
  static ListMcpServersResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<McpServerDescriptor> get servers => $_getList(0);

  @$pb.TagNumber(2)
  $pb.PbList<UnsupportedMcpServer> get unsupported => $_getList(1);

  /// True when the registry could not safely return complete, healthy
  /// state — more servers, more import issues, or a larger aggregate
  /// tool-byte total than the registry's own operating bounds allow (see
  /// repository.MaxMCPRegistryServers/MaxMCPRegistryToolBytes) — and
  /// returned a safe, bounded, degraded view instead: every server
  /// descriptor this response does return has its own `tools` left empty,
  /// regardless of which of those three bounds was the one exceeded. When
  /// the server *count* itself is over repository.MaxMCPRegistryServers,
  /// `servers` is additionally truncated to exactly that many
  /// descriptors, in name order, rather than every server being listed
  /// (an operator still retains enough identity — id, name, endpoint — to
  /// find and delete whichever one is responsible, from among that
  /// bounded set); for the other two over-cap reasons (too many import
  /// issues, or too large an aggregate tool-byte total), every server
  /// descriptor is listed, just with `tools` empty as above. This is an
  /// explicit, structured signal, never a synthetic entry appended to
  /// `unsupported` (which continues to describe only ordinary per-entry
  /// import refusals).
  @$pb.TagNumber(3)
  $core.bool get registryDegraded => $_getBF(2);
  @$pb.TagNumber(3)
  set registryDegraded($core.bool value) => $_setBool(2, value);
  @$pb.TagNumber(3)
  $core.bool hasRegistryDegraded() => $_has(2);
  @$pb.TagNumber(3)
  void clearRegistryDegraded() => $_clearField(3);

  /// Set only when registry_degraded is true: a fixed, non-sensitive
  /// explanation of which bound was exceeded.
  @$pb.TagNumber(4)
  $core.String get registryDegradationReason => $_getSZ(3);
  @$pb.TagNumber(4)
  set registryDegradationReason($core.String value) => $_setString(3, value);
  @$pb.TagNumber(4)
  $core.bool hasRegistryDegradationReason() => $_has(3);
  @$pb.TagNumber(4)
  void clearRegistryDegradationReason() => $_clearField(4);
}

class SetMcpServerEnabledRequest extends $pb.GeneratedMessage {
  factory SetMcpServerEnabledRequest({
    $core.String? serverId,
    $core.bool? enabled,
  }) {
    final result = SetMcpServerEnabledRequest._();
    if (serverId != null) result.serverId = serverId;
    if (enabled != null) result.enabled = enabled;
    return result;
  }

  SetMcpServerEnabledRequest._();

  factory SetMcpServerEnabledRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      SetMcpServerEnabledRequest()..mergeFromBuffer(data, registry);
  factory SetMcpServerEnabledRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      SetMcpServerEnabledRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'SetMcpServerEnabledRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: SetMcpServerEnabledRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'serverId')
    ..aOB(2, _omitFieldNames ? '' : 'enabled')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetMcpServerEnabledRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  SetMcpServerEnabledRequest copyWith(
          void Function(SetMcpServerEnabledRequest) updates) =>
      super.copyWith(
              (message) => updates(message as SetMcpServerEnabledRequest))
          as SetMcpServerEnabledRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use SetMcpServerEnabledRequest() / SetMcpServerEnabledRequest.new instead')
  static SetMcpServerEnabledRequest create() => SetMcpServerEnabledRequest._();
  static $pb.GeneratedMessage $_createMessage() =>
      SetMcpServerEnabledRequest._();
  @$core.override
  SetMcpServerEnabledRequest createEmptyInstance() =>
      SetMcpServerEnabledRequest._();
  @$core.pragma('dart2js:noInline')
  static SetMcpServerEnabledRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<SetMcpServerEnabledRequest>(
          SetMcpServerEnabledRequest.$_createMessage);
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
    final result = UpdateMcpToolPolicyRequest._();
    if (serverId != null) result.serverId = serverId;
    if (toolName != null) result.toolName = toolName;
    if (policy != null) result.policy = policy;
    return result;
  }

  UpdateMcpToolPolicyRequest._();

  factory UpdateMcpToolPolicyRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      UpdateMcpToolPolicyRequest()..mergeFromBuffer(data, registry);
  factory UpdateMcpToolPolicyRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      UpdateMcpToolPolicyRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'UpdateMcpToolPolicyRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: UpdateMcpToolPolicyRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'serverId')
    ..aOS(2, _omitFieldNames ? '' : 'toolName')
    ..aE<$2.ToolPolicy>(3, _omitFieldNames ? '' : 'policy',
        enumValues: $2.ToolPolicy.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UpdateMcpToolPolicyRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UpdateMcpToolPolicyRequest copyWith(
          void Function(UpdateMcpToolPolicyRequest) updates) =>
      super.copyWith(
              (message) => updates(message as UpdateMcpToolPolicyRequest))
          as UpdateMcpToolPolicyRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use UpdateMcpToolPolicyRequest() / UpdateMcpToolPolicyRequest.new instead')
  static UpdateMcpToolPolicyRequest create() => UpdateMcpToolPolicyRequest._();
  static $pb.GeneratedMessage $_createMessage() =>
      UpdateMcpToolPolicyRequest._();
  @$core.override
  UpdateMcpToolPolicyRequest createEmptyInstance() =>
      UpdateMcpToolPolicyRequest._();
  @$core.pragma('dart2js:noInline')
  static UpdateMcpToolPolicyRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<UpdateMcpToolPolicyRequest>(
          UpdateMcpToolPolicyRequest.$_createMessage);
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
    final result = UpdateToolPolicyByNameRequest._();
    if (serverName != null) result.serverName = serverName;
    if (toolName != null) result.toolName = toolName;
    if (policy != null) result.policy = policy;
    return result;
  }

  UpdateToolPolicyByNameRequest._();

  factory UpdateToolPolicyByNameRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      UpdateToolPolicyByNameRequest()..mergeFromBuffer(data, registry);
  factory UpdateToolPolicyByNameRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      UpdateToolPolicyByNameRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'UpdateToolPolicyByNameRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: UpdateToolPolicyByNameRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'serverName')
    ..aOS(2, _omitFieldNames ? '' : 'toolName')
    ..aE<$2.ToolPolicy>(3, _omitFieldNames ? '' : 'policy',
        enumValues: $2.ToolPolicy.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UpdateToolPolicyByNameRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  UpdateToolPolicyByNameRequest copyWith(
          void Function(UpdateToolPolicyByNameRequest) updates) =>
      super.copyWith(
              (message) => updates(message as UpdateToolPolicyByNameRequest))
          as UpdateToolPolicyByNameRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use UpdateToolPolicyByNameRequest() / UpdateToolPolicyByNameRequest.new instead')
  static UpdateToolPolicyByNameRequest create() =>
      UpdateToolPolicyByNameRequest._();
  static $pb.GeneratedMessage $_createMessage() =>
      UpdateToolPolicyByNameRequest._();
  @$core.override
  UpdateToolPolicyByNameRequest createEmptyInstance() =>
      UpdateToolPolicyByNameRequest._();
  @$core.pragma('dart2js:noInline')
  static UpdateToolPolicyByNameRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<UpdateToolPolicyByNameRequest>(
          UpdateToolPolicyByNameRequest.$_createMessage);
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
    final result = ListPseudoServerToolsRequest._();
    if (serverName != null) result.serverName = serverName;
    return result;
  }

  ListPseudoServerToolsRequest._();

  factory ListPseudoServerToolsRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListPseudoServerToolsRequest()..mergeFromBuffer(data, registry);
  factory ListPseudoServerToolsRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListPseudoServerToolsRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListPseudoServerToolsRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListPseudoServerToolsRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'serverName')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListPseudoServerToolsRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListPseudoServerToolsRequest copyWith(
          void Function(ListPseudoServerToolsRequest) updates) =>
      super.copyWith(
              (message) => updates(message as ListPseudoServerToolsRequest))
          as ListPseudoServerToolsRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use ListPseudoServerToolsRequest() / ListPseudoServerToolsRequest.new instead')
  static ListPseudoServerToolsRequest create() =>
      ListPseudoServerToolsRequest._();
  static $pb.GeneratedMessage $_createMessage() =>
      ListPseudoServerToolsRequest._();
  @$core.override
  ListPseudoServerToolsRequest createEmptyInstance() =>
      ListPseudoServerToolsRequest._();
  @$core.pragma('dart2js:noInline')
  static ListPseudoServerToolsRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListPseudoServerToolsRequest>(
          ListPseudoServerToolsRequest.$_createMessage);
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
    final result = ListPseudoServerToolsResponse._();
    if (tools != null) result.tools.addAll(tools);
    return result;
  }

  ListPseudoServerToolsResponse._();

  factory ListPseudoServerToolsResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListPseudoServerToolsResponse()..mergeFromBuffer(data, registry);
  factory ListPseudoServerToolsResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ListPseudoServerToolsResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ListPseudoServerToolsResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ListPseudoServerToolsResponse.$_createMessage)
    ..pPM<McpToolDescriptor>(1, _omitFieldNames ? '' : 'tools',
        subBuilder: McpToolDescriptor.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListPseudoServerToolsResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ListPseudoServerToolsResponse copyWith(
          void Function(ListPseudoServerToolsResponse) updates) =>
      super.copyWith(
              (message) => updates(message as ListPseudoServerToolsResponse))
          as ListPseudoServerToolsResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use ListPseudoServerToolsResponse() / ListPseudoServerToolsResponse.new instead')
  static ListPseudoServerToolsResponse create() =>
      ListPseudoServerToolsResponse._();
  static $pb.GeneratedMessage $_createMessage() =>
      ListPseudoServerToolsResponse._();
  @$core.override
  ListPseudoServerToolsResponse createEmptyInstance() =>
      ListPseudoServerToolsResponse._();
  @$core.pragma('dart2js:noInline')
  static ListPseudoServerToolsResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ListPseudoServerToolsResponse>(
          ListPseudoServerToolsResponse.$_createMessage);
  static ListPseudoServerToolsResponse? _defaultInstance;

  @$pb.TagNumber(1)
  $pb.PbList<McpToolDescriptor> get tools => $_getList(0);
}

class DeleteMcpServerRequest extends $pb.GeneratedMessage {
  factory DeleteMcpServerRequest({
    $core.String? serverId,
  }) {
    final result = DeleteMcpServerRequest._();
    if (serverId != null) result.serverId = serverId;
    return result;
  }

  DeleteMcpServerRequest._();

  factory DeleteMcpServerRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      DeleteMcpServerRequest()..mergeFromBuffer(data, registry);
  factory DeleteMcpServerRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      DeleteMcpServerRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DeleteMcpServerRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: DeleteMcpServerRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'serverId')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteMcpServerRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteMcpServerRequest copyWith(
          void Function(DeleteMcpServerRequest) updates) =>
      super.copyWith((message) => updates(message as DeleteMcpServerRequest))
          as DeleteMcpServerRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use DeleteMcpServerRequest() / DeleteMcpServerRequest.new instead')
  static DeleteMcpServerRequest create() => DeleteMcpServerRequest._();
  static $pb.GeneratedMessage $_createMessage() => DeleteMcpServerRequest._();
  @$core.override
  DeleteMcpServerRequest createEmptyInstance() => DeleteMcpServerRequest._();
  @$core.pragma('dart2js:noInline')
  static DeleteMcpServerRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DeleteMcpServerRequest>(
          DeleteMcpServerRequest.$_createMessage);
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
  factory DeleteMcpServerResponse() => DeleteMcpServerResponse._();

  DeleteMcpServerResponse._();

  factory DeleteMcpServerResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      DeleteMcpServerResponse()..mergeFromBuffer(data, registry);
  factory DeleteMcpServerResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      DeleteMcpServerResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'DeleteMcpServerResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: DeleteMcpServerResponse.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteMcpServerResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  DeleteMcpServerResponse copyWith(
          void Function(DeleteMcpServerResponse) updates) =>
      super.copyWith((message) => updates(message as DeleteMcpServerResponse))
          as DeleteMcpServerResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use DeleteMcpServerResponse() / DeleteMcpServerResponse.new instead')
  static DeleteMcpServerResponse create() => DeleteMcpServerResponse._();
  static $pb.GeneratedMessage $_createMessage() => DeleteMcpServerResponse._();
  @$core.override
  DeleteMcpServerResponse createEmptyInstance() => DeleteMcpServerResponse._();
  @$core.pragma('dart2js:noInline')
  static DeleteMcpServerResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<DeleteMcpServerResponse>(
          DeleteMcpServerResponse.$_createMessage);
  static DeleteMcpServerResponse? _defaultInstance;
}

class RegisterMcpServerRequest extends $pb.GeneratedMessage {
  factory RegisterMcpServerRequest({
    $core.String? name,
    $core.String? url,
    $core.String? bearerToken,
    McpServerTier? tier,
  }) {
    final result = RegisterMcpServerRequest._();
    if (name != null) result.name = name;
    if (url != null) result.url = url;
    if (bearerToken != null) result.bearerToken = bearerToken;
    if (tier != null) result.tier = tier;
    return result;
  }

  RegisterMcpServerRequest._();

  factory RegisterMcpServerRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      RegisterMcpServerRequest()..mergeFromBuffer(data, registry);
  factory RegisterMcpServerRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      RegisterMcpServerRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RegisterMcpServerRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: RegisterMcpServerRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'name')
    ..aOS(2, _omitFieldNames ? '' : 'url')
    ..aOS(3, _omitFieldNames ? '' : 'bearerToken')
    ..aE<McpServerTier>(4, _omitFieldNames ? '' : 'tier',
        enumValues: McpServerTier.values)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RegisterMcpServerRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RegisterMcpServerRequest copyWith(
          void Function(RegisterMcpServerRequest) updates) =>
      super.copyWith((message) => updates(message as RegisterMcpServerRequest))
          as RegisterMcpServerRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use RegisterMcpServerRequest() / RegisterMcpServerRequest.new instead')
  static RegisterMcpServerRequest create() => RegisterMcpServerRequest._();
  static $pb.GeneratedMessage $_createMessage() => RegisterMcpServerRequest._();
  @$core.override
  RegisterMcpServerRequest createEmptyInstance() =>
      RegisterMcpServerRequest._();
  @$core.pragma('dart2js:noInline')
  static RegisterMcpServerRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RegisterMcpServerRequest>(
          RegisterMcpServerRequest.$_createMessage);
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
  factory ReimportMcpJsonRequest() => ReimportMcpJsonRequest._();

  ReimportMcpJsonRequest._();

  factory ReimportMcpJsonRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ReimportMcpJsonRequest()..mergeFromBuffer(data, registry);
  factory ReimportMcpJsonRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ReimportMcpJsonRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ReimportMcpJsonRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ReimportMcpJsonRequest.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReimportMcpJsonRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReimportMcpJsonRequest copyWith(
          void Function(ReimportMcpJsonRequest) updates) =>
      super.copyWith((message) => updates(message as ReimportMcpJsonRequest))
          as ReimportMcpJsonRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use ReimportMcpJsonRequest() / ReimportMcpJsonRequest.new instead')
  static ReimportMcpJsonRequest create() => ReimportMcpJsonRequest._();
  static $pb.GeneratedMessage $_createMessage() => ReimportMcpJsonRequest._();
  @$core.override
  ReimportMcpJsonRequest createEmptyInstance() => ReimportMcpJsonRequest._();
  @$core.pragma('dart2js:noInline')
  static ReimportMcpJsonRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ReimportMcpJsonRequest>(
          ReimportMcpJsonRequest.$_createMessage);
  static ReimportMcpJsonRequest? _defaultInstance;
}

class ReimportMcpJsonResponse extends $pb.GeneratedMessage {
  factory ReimportMcpJsonResponse({
    $core.Iterable<$core.String>? imported,
    $core.Iterable<UnsupportedMcpServer>? unsupported,
    $core.Iterable<$core.String>? skipped,
  }) {
    final result = ReimportMcpJsonResponse._();
    if (imported != null) result.imported.addAll(imported);
    if (unsupported != null) result.unsupported.addAll(unsupported);
    if (skipped != null) result.skipped.addAll(skipped);
    return result;
  }

  ReimportMcpJsonResponse._();

  factory ReimportMcpJsonResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ReimportMcpJsonResponse()..mergeFromBuffer(data, registry);
  factory ReimportMcpJsonResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      ReimportMcpJsonResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'ReimportMcpJsonResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: ReimportMcpJsonResponse.$_createMessage)
    ..pPS(1, _omitFieldNames ? '' : 'imported')
    ..pPM<UnsupportedMcpServer>(2, _omitFieldNames ? '' : 'unsupported',
        subBuilder: UnsupportedMcpServer.$_createMessage)
    ..pPS(3, _omitFieldNames ? '' : 'skipped')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReimportMcpJsonResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  ReimportMcpJsonResponse copyWith(
          void Function(ReimportMcpJsonResponse) updates) =>
      super.copyWith((message) => updates(message as ReimportMcpJsonResponse))
          as ReimportMcpJsonResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use ReimportMcpJsonResponse() / ReimportMcpJsonResponse.new instead')
  static ReimportMcpJsonResponse create() => ReimportMcpJsonResponse._();
  static $pb.GeneratedMessage $_createMessage() => ReimportMcpJsonResponse._();
  @$core.override
  ReimportMcpJsonResponse createEmptyInstance() => ReimportMcpJsonResponse._();
  @$core.pragma('dart2js:noInline')
  static ReimportMcpJsonResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<ReimportMcpJsonResponse>(
          ReimportMcpJsonResponse.$_createMessage);
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
    final result = RotateMcpServerTokenRequest._();
    if (serverId != null) result.serverId = serverId;
    if (bearerToken != null) result.bearerToken = bearerToken;
    return result;
  }

  RotateMcpServerTokenRequest._();

  factory RotateMcpServerTokenRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      RotateMcpServerTokenRequest()..mergeFromBuffer(data, registry);
  factory RotateMcpServerTokenRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      RotateMcpServerTokenRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'RotateMcpServerTokenRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: RotateMcpServerTokenRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'serverId')
    ..aOS(2, _omitFieldNames ? '' : 'bearerToken')
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RotateMcpServerTokenRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  RotateMcpServerTokenRequest copyWith(
          void Function(RotateMcpServerTokenRequest) updates) =>
      super.copyWith(
              (message) => updates(message as RotateMcpServerTokenRequest))
          as RotateMcpServerTokenRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use RotateMcpServerTokenRequest() / RotateMcpServerTokenRequest.new instead')
  static RotateMcpServerTokenRequest create() =>
      RotateMcpServerTokenRequest._();
  static $pb.GeneratedMessage $_createMessage() =>
      RotateMcpServerTokenRequest._();
  @$core.override
  RotateMcpServerTokenRequest createEmptyInstance() =>
      RotateMcpServerTokenRequest._();
  @$core.pragma('dart2js:noInline')
  static RotateMcpServerTokenRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<RotateMcpServerTokenRequest>(
          RotateMcpServerTokenRequest.$_createMessage);
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
    final result = CallRegisteredMcpToolRequest._();
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
      CallRegisteredMcpToolRequest()..mergeFromBuffer(data, registry);
  factory CallRegisteredMcpToolRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      CallRegisteredMcpToolRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CallRegisteredMcpToolRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: CallRegisteredMcpToolRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'serverId')
    ..aOS(2, _omitFieldNames ? '' : 'runId')
    ..aOS(3, _omitFieldNames ? '' : 'approvalId')
    ..aOS(4, _omitFieldNames ? '' : 'toolName')
    ..aOM<$1.Struct>(5, _omitFieldNames ? '' : 'args',
        subBuilder: $1.Struct.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CallRegisteredMcpToolRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CallRegisteredMcpToolRequest copyWith(
          void Function(CallRegisteredMcpToolRequest) updates) =>
      super.copyWith(
              (message) => updates(message as CallRegisteredMcpToolRequest))
          as CallRegisteredMcpToolRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use CallRegisteredMcpToolRequest() / CallRegisteredMcpToolRequest.new instead')
  static CallRegisteredMcpToolRequest create() =>
      CallRegisteredMcpToolRequest._();
  static $pb.GeneratedMessage $_createMessage() =>
      CallRegisteredMcpToolRequest._();
  @$core.override
  CallRegisteredMcpToolRequest createEmptyInstance() =>
      CallRegisteredMcpToolRequest._();
  @$core.pragma('dart2js:noInline')
  static CallRegisteredMcpToolRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CallRegisteredMcpToolRequest>(
          CallRegisteredMcpToolRequest.$_createMessage);
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
    final result$ = CallRegisteredMcpToolResponse._();
    if (result != null) result$.result = result;
    return result$;
  }

  CallRegisteredMcpToolResponse._();

  factory CallRegisteredMcpToolResponse.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      CallRegisteredMcpToolResponse()..mergeFromBuffer(data, registry);
  factory CallRegisteredMcpToolResponse.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      CallRegisteredMcpToolResponse()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'CallRegisteredMcpToolResponse',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: CallRegisteredMcpToolResponse.$_createMessage)
    ..aOM<$1.Struct>(1, _omitFieldNames ? '' : 'result',
        subBuilder: $1.Struct.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CallRegisteredMcpToolResponse clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  CallRegisteredMcpToolResponse copyWith(
          void Function(CallRegisteredMcpToolResponse) updates) =>
      super.copyWith(
              (message) => updates(message as CallRegisteredMcpToolResponse))
          as CallRegisteredMcpToolResponse;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated(
      'Use CallRegisteredMcpToolResponse() / CallRegisteredMcpToolResponse.new instead')
  static CallRegisteredMcpToolResponse create() =>
      CallRegisteredMcpToolResponse._();
  static $pb.GeneratedMessage $_createMessage() =>
      CallRegisteredMcpToolResponse._();
  @$core.override
  CallRegisteredMcpToolResponse createEmptyInstance() =>
      CallRegisteredMcpToolResponse._();
  @$core.pragma('dart2js:noInline')
  static CallRegisteredMcpToolResponse getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<CallRegisteredMcpToolResponse>(
          CallRegisteredMcpToolResponse.$_createMessage);
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
    final result = McpRequest._();
    if (serverName != null) result.serverName = serverName;
    if (method != null) result.method = method;
    if (params != null) result.params = params;
    return result;
  }

  McpRequest._();

  factory McpRequest.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      McpRequest()..mergeFromBuffer(data, registry);
  factory McpRequest.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      McpRequest()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'McpRequest',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: McpRequest.$_createMessage)
    ..aOS(1, _omitFieldNames ? '' : 'serverName')
    ..aOS(2, _omitFieldNames ? '' : 'method')
    ..aOM<$1.Struct>(3, _omitFieldNames ? '' : 'params',
        subBuilder: $1.Struct.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  McpRequest clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  McpRequest copyWith(void Function(McpRequest) updates) =>
      super.copyWith((message) => updates(message as McpRequest)) as McpRequest;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use McpRequest() / McpRequest.new instead')
  static McpRequest create() => McpRequest._();
  static $pb.GeneratedMessage $_createMessage() => McpRequest._();
  @$core.override
  McpRequest createEmptyInstance() => McpRequest._();
  @$core.pragma('dart2js:noInline')
  static McpRequest getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<McpRequest>(McpRequest.$_createMessage);
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
    final result$ = McpResult._();
    if (result != null) result$.result = result;
    return result$;
  }

  McpResult._();

  factory McpResult.fromBuffer($core.List<$core.int> data,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      McpResult()..mergeFromBuffer(data, registry);
  factory McpResult.fromJson($core.String json,
          [$pb.ExtensionRegistry registry = $pb.ExtensionRegistry.EMPTY]) =>
      McpResult()..mergeFromJson(json, registry);

  static final $pb.BuilderInfo _i = $pb.BuilderInfo(
      _omitMessageNames ? '' : 'McpResult',
      package: const $pb.PackageName(_omitMessageNames ? '' : 'turing.v1'),
      createEmptyInstance: McpResult.$_createMessage)
    ..aOM<$1.Struct>(1, _omitFieldNames ? '' : 'result',
        subBuilder: $1.Struct.$_createMessage)
    ..hasRequiredFields = false;

  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  McpResult clone() => deepCopy();
  @$core.Deprecated('See https://github.com/google/protobuf.dart/issues/998.')
  McpResult copyWith(void Function(McpResult) updates) =>
      super.copyWith((message) => updates(message as McpResult)) as McpResult;

  @$core.override
  $pb.BuilderInfo get info_ => _i;

  @$core.pragma('dart2js:noInline')
  @$core.Deprecated('Use McpResult() / McpResult.new instead')
  static McpResult create() => McpResult._();
  static $pb.GeneratedMessage $_createMessage() => McpResult._();
  @$core.override
  McpResult createEmptyInstance() => McpResult._();
  @$core.pragma('dart2js:noInline')
  static McpResult getDefault() => _defaultInstance ??=
      $pb.GeneratedMessage.$_defaultFor<McpResult>(McpResult.$_createMessage);
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
