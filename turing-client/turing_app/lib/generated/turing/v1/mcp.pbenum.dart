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

class McpServerTier extends $pb.ProtobufEnum {
  static const McpServerTier MCP_SERVER_TIER_UNSPECIFIED =
      McpServerTier._(0, _omitEnumNames ? '' : 'MCP_SERVER_TIER_UNSPECIFIED');
  static const McpServerTier MCP_SERVER_TIER_BUNDLED =
      McpServerTier._(1, _omitEnumNames ? '' : 'MCP_SERVER_TIER_BUNDLED');
  static const McpServerTier MCP_SERVER_TIER_LOCAL_CONTAINER = McpServerTier._(
      2, _omitEnumNames ? '' : 'MCP_SERVER_TIER_LOCAL_CONTAINER');
  static const McpServerTier MCP_SERVER_TIER_REMOTE_URL =
      McpServerTier._(3, _omitEnumNames ? '' : 'MCP_SERVER_TIER_REMOTE_URL');

  static const $core.List<McpServerTier> values = <McpServerTier>[
    MCP_SERVER_TIER_UNSPECIFIED,
    MCP_SERVER_TIER_BUNDLED,
    MCP_SERVER_TIER_LOCAL_CONTAINER,
    MCP_SERVER_TIER_REMOTE_URL,
  ];

  static final $core.List<McpServerTier?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static McpServerTier? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const McpServerTier._(super.value, super.name);
}

class McpServerLiveness extends $pb.ProtobufEnum {
  static const McpServerLiveness MCP_SERVER_LIVENESS_UNSPECIFIED =
      McpServerLiveness._(
          0, _omitEnumNames ? '' : 'MCP_SERVER_LIVENESS_UNSPECIFIED');
  static const McpServerLiveness MCP_SERVER_LIVENESS_UNKNOWN =
      McpServerLiveness._(
          1, _omitEnumNames ? '' : 'MCP_SERVER_LIVENESS_UNKNOWN');
  static const McpServerLiveness MCP_SERVER_LIVENESS_UP =
      McpServerLiveness._(2, _omitEnumNames ? '' : 'MCP_SERVER_LIVENESS_UP');
  static const McpServerLiveness MCP_SERVER_LIVENESS_DOWN =
      McpServerLiveness._(3, _omitEnumNames ? '' : 'MCP_SERVER_LIVENESS_DOWN');

  static const $core.List<McpServerLiveness> values = <McpServerLiveness>[
    MCP_SERVER_LIVENESS_UNSPECIFIED,
    MCP_SERVER_LIVENESS_UNKNOWN,
    MCP_SERVER_LIVENESS_UP,
    MCP_SERVER_LIVENESS_DOWN,
  ];

  static final $core.List<McpServerLiveness?> _byValue =
      $pb.ProtobufEnum.$_initByValueList(values, 3);
  static McpServerLiveness? valueOf($core.int value) =>
      value < 0 || value >= _byValue.length ? null : _byValue[value];

  const McpServerLiveness._(super.value, super.name);
}

const $core.bool _omitEnumNames =
    $core.bool.fromEnvironment('protobuf.omit_enum_names');
